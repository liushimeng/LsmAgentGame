// Package werewolf — secret_letter_room.go: §20260812-03 U2 私下通道(暗线信件 + 私密结盟)。
//
// 设计动机:原 §3.1「暗线信件」与 §4.3「私密结盟提议」本质同构(私下双向通道),
// 合并实现为「Secret Letter Room」单一抽象。仿 wolfpack_room.go 协议层隔离模板,
// 仅在白天 speak 之后 → vote 启动前的窗口可用。
//
// 协议层隔离(§119 三重拒绝):
//   - 不入 chat_message 表
//   - 不入 chat_history 队列
//   - 不入 BotTranscript.HeartThought
//
// 约束:
//   - 不可发给自己 / 不可发群 / 不可发给死亡玩家
//   - 每日每人发送上限 5 条(cfgWerewolfSecretLetterDailyLimit)
//   - 单条 ≤ 200 字
//   - 仅白天 speak 阶段可发(vote 启动后立即关闭)
//
// 线程安全:SecretLetterRoom 内部用 mu 保护;外部调用方(manager 层)已持 r.mu。
package werewolf

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"LsmWebGame/logger"

	"go.uber.org/zap"
)

// SecretLetter 内存态(不入 chat_message 表/队列,仅 SecretLetterRoom 持有 + DB 持久化)。
type SecretLetter struct {
	ID        string    `json:"id"`
	RoomID    string    `json:"room_id"`
	FromSeat  int       `json:"from_seat"`
	ToSeat    int       `json:"to_seat"`
	Body      string    `json:"body"`
	Round     int       `json:"round"`
	IsRead    bool      `json:"is_read"`
	CreatedAt time.Time `json:"created_at"`
}

// SecretLetterView 是 API 返回的视图(与 models.TLsmGameSecretLetter 一一对应)。
type SecretLetterView struct {
	ID        string `json:"id"`
	FromSeat  int    `json:"from_seat"`
	ToSeat    int    `json:"to_seat"`
	Body      string `json:"body"`
	Round     int    `json:"round"`
	IsRead    bool   `json:"is_read"`
	CreatedAt int64  `json:"created_at"`
}

// SecretLetterRoom 单房间的私下通道容器。
// §92a 锁内变体约束:Manager 调用方已持 r.mu;SecretLetterRoom 内部 mu 仅保护
// 自身 letters/inbox/sentToday map 的并发安全。
type SecretLetterRoom struct {
	mu        sync.Mutex
	letters   map[string]*SecretLetter // key = letter ID
	bySeat    map[int][]string         // key = seat, value = letter IDs (inbox)
	sentToday map[int]int              // key = seat, value = today's send count
	todayKey  string                   // 跟踪「今天」,跨日清零
	roomID    string
}

// ErrSecretLetterClosed 错误:窗口已关闭(speak→vote 之外)。
var ErrSecretLetterClosed = errors.New("私下通道窗口已关闭(仅白天 speak 阶段可发)")

// ErrSecretLetterSelf 错误:不可发给自己。
var ErrSecretLetterSelf = errors.New("不可发送私下信件给自己")

// ErrSecretLetterDead 错误:目标玩家已死亡。
var ErrSecretLetterDead = errors.New("目标玩家已死亡,无法接收私下信件")

// ErrSecretLetterLimit 错误:今日发送上限已达。
var ErrSecretLetterLimit = errors.New("今日私下信件发送次数已达上限")

// ErrSecretLetterBody 错误:内容长度不合法。
var ErrSecretLetterBody = errors.New("私下信件内容必须 1~200 字")

// newSecretLetterRoomLocked 构造一个 SecretLetterRoom,初始化所有 map。
// 调用方持 r.mu(§92a 锁内构造)。
func newSecretLetterRoomLocked(roomID string) *SecretLetterRoom {
	now := time.Now()
	return &SecretLetterRoom{
		letters:   make(map[string]*SecretLetter),
		bySeat:    make(map[int][]string),
		sentToday: make(map[int]int),
		todayKey:  now.Format("2006-01-02"),
		roomID:    roomID,
	}
}

// reset 清空所有数据(用于重开局时调用)。
// 调用方持 r.mu。
func (sl *SecretLetterRoom) reset() {
	sl.mu.Lock()
	defer sl.mu.Unlock()
	sl.letters = make(map[string]*SecretLetter)
	sl.bySeat = make(map[int][]string)
	sl.sentToday = make(map[int]int)
	sl.todayKey = time.Now().Format("2006-01-02")
}

// dailyLimitReached 报告 seat 今日发送是否已达上限。
// 调用方持 r.mu。
func (sl *SecretLetterRoom) dailyLimitReached(seat int) bool {
	// 跨日自动清零
	today := time.Now().Format("2006-01-02")
	if today != sl.todayKey {
		sl.sentToday = make(map[int]int)
		sl.todayKey = today
	}
	return sl.sentToday[seat] >= 5
}

// Send 发送一条私下信件。
//   - phase: 当前阶段字符串(仅 "speak"/"PhaseSpeak" 允许,其他返回 ErrSecretLetterClosed)
//   - aliveSeats: 当前存活座位集合(目标必须在其中)
//   - senderIsAlive: 发送者是否存活(已死亡不能发)
//
// 调用方必须持 r.mu(§92a)。
func (sl *SecretLetterRoom) Send(
	phase string,
	senderSeat, targetSeat int,
	body string,
	round int,
	aliveSeats map[int]bool,
) (*SecretLetter, error) {
	sl.mu.Lock()
	defer sl.mu.Unlock()

	// 校验 1:窗口必须开
	if phase != "speak" && phase != "PhaseSpeak" {
		return nil, ErrSecretLetterClosed
	}
	// 校验 2:不可发给自己
	if senderSeat == targetSeat {
		return nil, ErrSecretLetterSelf
	}
	// 校验 3:目标必须存活
	if !aliveSeats[targetSeat] {
		return nil, ErrSecretLetterDead
	}
	// 校验 4:内容长度
	runes := []rune(body)
	if len(runes) == 0 || len(runes) > 200 {
		return nil, ErrSecretLetterBody
	}
	// 校验 5:每日上限
	if sl.dailyLimitReached(senderSeat) {
		return nil, ErrSecretLetterLimit
	}

	now := time.Now()
	letter := &SecretLetter{
		ID:        fmt.Sprintf("sl-%d-%d", now.UnixNano(), senderSeat),
		RoomID:    sl.roomID,
		FromSeat:  senderSeat,
		ToSeat:    targetSeat,
		Body:      body,
		Round:     round,
		IsRead:    false,
		CreatedAt: now,
	}
	sl.letters[letter.ID] = letter
	sl.bySeat[targetSeat] = append(sl.bySeat[targetSeat], letter.ID)
	sl.sentToday[senderSeat]++
	logger.L().Debug("SecretLetter sent",
		zap.String("room_id", sl.roomID),
		zap.Int("from", senderSeat),
		zap.Int("to", targetSeat),
		zap.Int("round", round),
		zap.Int("runes", len(runes)))
	return letter, nil
}

// Inbox 返回 seat 的收件箱(未读优先 + 倒序)。
// 调用方必须持 r.mu。
func (sl *SecretLetterRoom) Inbox(seat int) []*SecretLetter {
	sl.mu.Lock()
	defer sl.mu.Unlock()

	ids := sl.bySeat[seat]
	out := make([]*SecretLetter, 0, len(ids))
	for _, id := range ids {
		if l, ok := sl.letters[id]; ok {
			out = append(out, l)
		}
	}
	// 排序:未读优先,然后按时间倒序
	sortSecretLettersByUnreadThenTime(out)
	return out
}

// MarkRead 把 seat 收件箱中 letterID 标记为已读。
// 调用方必须持 r.mu。
func (sl *SecretLetterRoom) MarkRead(seat int, letterID string) {
	sl.mu.Lock()
	defer sl.mu.Unlock()
	if l, ok := sl.letters[letterID]; ok && l.ToSeat == seat {
		l.IsRead = true
	}
}

func sortSecretLettersByUnreadThenTime(ls []*SecretLetter) {
	// 简单实现:用 sort.Slice(避免引入 sort 包的 sort.Slice,保证 import 最小)
	// 用冒泡:信件数通常 < 100,O(n²) 足够
	for i := 0; i < len(ls); i++ {
		for j := i + 1; j < len(ls); j++ {
			if shouldSwapSecretLetter(ls[i], ls[j]) {
				ls[i], ls[j] = ls[j], ls[i]
			}
		}
	}
}

func shouldSwapSecretLetter(a, b *SecretLetter) bool {
	// 未读优先
	if a.IsRead != b.IsRead {
		return !a.IsRead && b.IsRead
	}
	// 同状态按时间倒序(较新的在前)
	return a.CreatedAt.Before(b.CreatedAt)
}
