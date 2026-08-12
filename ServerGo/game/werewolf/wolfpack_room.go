// Package werewolf — wolfpack_room.go: 狼人小队内部交流通道（v4 新增）。
//
// 设计动机（docs/狼人杀-道具与经济/狼人杀13人局道具系统设计.md §13.1）：
//   - v3 已经实现 30% 对局 2 只狼人 Agent 互知身份（WolfTeammateHint 注入 prompt），
//     但**狼人 Agent 之间没有结构化沟通通道**。
//   - 狼 Agent 在公屏发言必须伪装好人，无法在刀人决策前同步策略；
//   - 现有 whisper 只能对单一玩家互动，不支持小队内多狼广播。
//   - v4 引入 WolfPackRoom + wolf_whisper 工具，让配对成功的狼 Agent 能在
//     刀人/伪装阶段协调行动。
//
// 协议层隔离（与 §119 HeartThought 一致）：
//   - WolfPackRoom msgs **不**写入 chat_message 表 / chat_history 队列 /
//     BotTranscript.HeartThought（仅小队 Agent user prompt 可见）
//   - 不广播给人类玩家、不广播给观众
//   - 狼人死亡时 PurgeByDeath(deadSeats) 清理其留言（避免死人继续影响队友）
//
// 2026-07-21 狼人杀 13 人局道具系统 v4 重构（docs/狼人杀-道具与经济/狼人杀13人局道具系统设计.md §13.1）。
package werewolf

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"LsmWebGame/logger"

	"go.uber.org/zap"
)

// WolfPackMsgLenMax 是单条狼小队留言的最大字符数（防止滥用 + 降低 prompt 体积）。
const WolfPackMsgLenMax = 80

// WolfPackSnapshotMax 是 user prompt 中渲染的最近留言条数。
const WolfPackSnapshotMax = 20

// §20260810-10 U1 — 狼队战术分工（WolfRoleAssignment）。
//
// 四种战术定位(仅 GameContext 注入,§119 协议层隔离,不进 chat 表/队列/HeartThought):
//   - 悍跳位(hype):     假冒预言家争夺警徽,发言高调自信
//   - 冲锋位(charger):  为悍跳位造势、攻击真预言家,发言激进煽动
//   - 倒钩位(hook):     假装好人混入好人阵营,必要时卖队友,发言随大流温和
//   - 深水位(deep):     极端低调不被注意,留到最后,发言划水装平民
const (
	WolfRoleHype    = "hype"    // 悍跳位
	WolfRoleCharger = "charger" // 冲锋位
	WolfRoleHook    = "hook"    // 倒钩位
	WolfRoleDeep    = "deep"    // 深水位
)

// WolfRoleSystemSeat 是分工变更系统留言的虚拟发送者座位(不对应任何真实玩家)。
const WolfRoleSystemSeat = -2

// wolfRoleTemplates 按存活狼数给出分工模板(按座位升序依次指派)。
// 1 只狼不指派(独狼自由发挥)。
var wolfRoleTemplates = map[int][]string{
	4: {WolfRoleHype, WolfRoleCharger, WolfRoleHook, WolfRoleDeep},
	3: {WolfRoleHype, WolfRoleCharger, WolfRoleHook},
	2: {WolfRoleHype, WolfRoleHook},
}

// WolfRoleLabel 返回分工的中文展示名(供 prompt 渲染)。
func WolfRoleLabel(role string) string {
	switch role {
	case WolfRoleHype:
		return "悍跳位"
	case WolfRoleCharger:
		return "冲锋位"
	case WolfRoleHook:
		return "倒钩位"
	case WolfRoleDeep:
		return "深水位"
	default:
		return ""
	}
}

// wolfRoleDuty 返回分工的职责一句话(供 prompt 渲染)。
func wolfRoleDuty(role string) string {
	switch role {
	case WolfRoleHype:
		return "假冒预言家争夺警徽,发言高调自信,主动报查验"
	case WolfRoleCharger:
		return "为悍跳位造势、攻击真预言家,发言激进带节奏"
	case WolfRoleHook:
		return "假装好人混入好人阵营,必要时卖队友,发言随大流温和"
	case WolfRoleDeep:
		return "极端低调不被注意,留到最后,发言划水装平民少说话"
	default:
		return ""
	}
}

// validWolfRole 判断 role 字符串是否合法。
func validWolfRole(role string) bool {
	return WolfRoleLabel(role) != ""
}

// WolfPackMsg 是狼小队内的单条留言（v4 §13.1 数据结构）。
type WolfPackMsg struct {
	FromSeat   int       // 发送者座位（0-indexed）
	FromUserID string    // 发送者 user_id
	Text       string    // 留言内容（≤80 字）
	CreatedAt  time.Time // 创建时间
}

// WolfPackRoom 是狼人小队内部交流通道（v4 §13.1）。
// 线程安全：所有方法使用 mu 锁保护。
// msgs 容量上限 maxLen（默认 50），超出按 FIFO 淘汰最早留言。
type WolfPackRoom struct {
	mu      sync.Mutex
	roomID  string
	members map[int]bool // 当前存活的狼人座位集合（用于校验发送者合法性）
	msgs    []WolfPackMsg
	maxLen  int

	// §20260810-10 U1 — 战术分工与轮值狼王。
	// roles: seat -> WolfRole* 常量；仅存活狼有分工（死亡即被 PurgeByDeath 移除）。
	// kingSeat: 当前轮值狼王座位（-1 = 未指派）；狼王可用 wolfpack_assign 工具
	// 重排自己的分工。确定性约定：AutoAssignRoles 按座位升序套模板，无随机，
	// 保证同房间所有 goroutine 对同一状态得出同一结论。
	roles    map[int]string
	kingSeat int
}

// NewWolfPackRoom 构造指定房间的狼小队交流通道。
// roomID: 房间 ID；maxLen: msgs 容量上限（≤0 默认 50）。
func NewWolfPackRoom(roomID string, maxLen int) *WolfPackRoom {
	if maxLen <= 0 {
		maxLen = 50
	}
	return &WolfPackRoom{
		roomID:   roomID,
		members:  make(map[int]bool),
		msgs:     make([]WolfPackMsg, 0, maxLen),
		maxLen:   maxLen,
		roles:    make(map[int]string),
		kingSeat: -1,
	}
}

// ErrWolfPackNotMember 发送者不是狼小队成员（不在 members 中）。
var ErrWolfPackNotMember = errors.New("wolfpack: sender is not a wolf member")

// ErrWolfPackMsgTooLong 留言长度超限。
var ErrWolfPackMsgTooLong = errors.New("wolfpack: message too long")

// Append 把留言追加到小队通道。
// from: 发送者座位；uid: 发送者 user_id；text: 留言内容（≤80 字）。
// 错误：
//   - ErrWolfPackNotMember：from 不在 members 中（非狼人或已死亡未同步）。
//   - ErrWolfPackMsgTooLong：text 长度超过 WolfPackMsgLenMax。
//
// 设计：若 text 超长，**不**自动截断（强约束 LLM/人类输入合法性，避免"自动改内容"）。
func (w *WolfPackRoom) Append(from int, uid, text string) error {
	if w == nil {
		return errors.New("wolfpack: nil room")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.members[from] {
		return ErrWolfPackNotMember
	}
	text = strings.TrimSpace(text)
	if len([]rune(text)) > WolfPackMsgLenMax {
		return ErrWolfPackMsgTooLong
	}
	w.msgs = append(w.msgs, WolfPackMsg{
		FromSeat:   from,
		FromUserID: uid,
		Text:       text,
		CreatedAt:  time.Now(),
	})
	// FIFO 淘汰
	if len(w.msgs) > w.maxLen {
		drop := len(w.msgs) - w.maxLen
		w.msgs = w.msgs[drop:]
	}
	return nil
}

// Snapshot 返回最近 maxN 条留言（按时间正序）。maxN≤0 时返回所有。
// 返回**拷贝**，调用方修改不影响内部状态。
func (w *WolfPackRoom) Snapshot(maxN int) []WolfPackMsg {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if maxN <= 0 || maxN > len(w.msgs) {
		maxN = len(w.msgs)
	}
	out := make([]WolfPackMsg, maxN)
	// 取尾部 maxN 条
	copy(out, w.msgs[len(w.msgs)-maxN:])
	return out
}

// SetMembers 替换当前存活狼人座位集合（用于房间状态变化时同步）。
func (w *WolfPackRoom) SetMembers(seats []int) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.members = make(map[int]bool, len(seats))
	for _, s := range seats {
		w.members[s] = true
	}
}

// PurgeByDeath 清理死亡狼人座位发送的所有留言（v4 §13.1 安全约束）。
// 防止死人继续影响存活的狼队友。
func (w *WolfPackRoom) PurgeByDeath(deadSeats []int) int {
	if w == nil || len(deadSeats) == 0 {
		return 0
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	deadSet := make(map[int]bool, len(deadSeats))
	for _, s := range deadSeats {
		deadSet[s] = true
		w.members[s] = false // 同时从成员集合中移除
		// §20260810-10 U1 — 同步移除死亡狼的分工;狼王死亡由调用方随后
		// 调 RotateKingLocked 顺延(EmitPlayerDied 持锁路径,§92a)。
		delete(w.roles, s)
	}
	if len(deadSet) == 0 {
		return 0
	}
	kept := w.msgs[:0]
	purged := 0
	for _, m := range w.msgs {
		if deadSet[m.FromSeat] {
			purged++
			continue
		}
		kept = append(kept, m)
	}
	w.msgs = kept
	if purged > 0 {
		logger.L().Debug("wolfpack: purged dead wolf messages",
			zap.String("room_id", w.roomID),
			zap.Ints("dead_seats", deadSeats),
			zap.Int("purged", purged))
	}
	return purged
}

// Len 返回当前留言总数（调试 + 测试用）。
func (w *WolfPackRoom) Len() int {
	if w == nil {
		return 0
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.msgs)
}

// IsMember 判断座位是否是小队成员。
func (w *WolfPackRoom) IsMember(seat int) bool {
	if w == nil {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.members[seat]
}

// ─── §20260810-10 U1 战术分工 + 轮值狼王 ───

// AutoAssignRoles 按座位升序套模板自动分工（确定性，无随机）。
// 仅对 ≥2 只存活狼指派；1 只狼或 0 只狼时清空分工。
// seats 调用方需自行保证为存活狼座位列表。
func (w *WolfPackRoom) AutoAssignRoles(seats []int) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.autoAssignLocked(seats)
}

// autoAssignLocked 是 AutoAssignRoles 的锁内变体（§92a）。
func (w *WolfPackRoom) autoAssignLocked(seats []int) {
	w.roles = make(map[int]string, len(seats))
	w.kingSeat = -1
	if len(seats) < 2 {
		return
	}
	sorted := make([]int, len(seats))
	copy(sorted, seats)
	sort.Ints(sorted)
	tmpl := wolfRoleTemplates[len(sorted)]
	if tmpl == nil {
		// >4 只狼（异常房间）→ 复用 4 狼模板前缀。
		tmpl = wolfRoleTemplates[4]
	}
	for i, seat := range sorted {
		if i < len(tmpl) {
			w.roles[seat] = tmpl[i]
		} else {
			w.roles[seat] = WolfRoleDeep // 兜底:多余狼一律深水
		}
	}
	w.kingSeat = sorted[0]
}

// AssignRole 由轮值狼王重排分工（wolfpack_assign 工具落地路径）。
// callerSeat 必须是当前 kingSeat，否则返回错误。
// 语义:把 callerSeat 自己的分工改为 newRole;若 newRole 已被其他狼占用,
// 则与被占用者**互换**分工(保证四种分工不重复、不重排无关座位)。
// 修改结果由调用方(Agent runner)以系统留言写入通道。
//
// 命名说明:虽以 Locked 结尾语义为「锁内逻辑」,但对外是公开加锁入口
// (调用方 agentRunner 持有的是 r.mu 而非 w.mu,不存在自死锁)。
func (w *WolfPackRoom) AssignRole(callerSeat int, newRole string) (string, error) {
	if w == nil {
		return "", errors.New("wolfpack: nil room")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.assignRoleLocked(callerSeat, newRole)
}

// assignRoleLocked 是 AssignRole 的锁内变体（§92a）。
func (w *WolfPackRoom) assignRoleLocked(callerSeat int, newRole string) (string, error) {
	if !w.members[callerSeat] {
		return "", ErrWolfPackNotMember
	}
	if callerSeat != w.kingSeat {
		return "", fmt.Errorf("wolfpack: only the rotating wolf king (seat %d) can reassign roles", w.kingSeat)
	}
	if !validWolfRole(newRole) {
		return "", fmt.Errorf("wolfpack: invalid role %q (want hype/charger/hook/deep)", newRole)
	}
	oldRole := w.roles[callerSeat]
	if oldRole == newRole {
		return oldRole, nil // 幂等:已是该分工
	}
	// 若 newRole 被其他狼占用 → 互换。
	for seat, role := range w.roles {
		if seat != callerSeat && role == newRole {
			w.roles[seat] = oldRole
			break
		}
	}
	w.roles[callerSeat] = newRole
	return oldRole, nil
}

// RotateKing 在狼王死亡/缺失时顺延到下一个存活狼（座位升序最小者）。
// 返回新的 kingSeat（-1 = 无存活狼）。幂等:当前 king 仍存活时不做任何事。
//
// 锁说明:本方法只锁 wolfPack 自身 mu;即便调用方已持有 r.mu(EmitPlayerDied
// 持锁路径),也不构成 §92a 自死锁(两把不同的锁,且 wolfPack 内部方法从不
// 反向获取 r.mu)。
func (w *WolfPackRoom) RotateKing() int {
	if w == nil {
		return -1
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.rotateKingLocked()
}

// rotateKingLocked 是 RotateKing 的锁内变体（§92a）。
func (w *WolfPackRoom) rotateKingLocked() int {
	if w.kingSeat >= 0 && w.members[w.kingSeat] {
		return w.kingSeat
	}
	best := -1
	for seat, alive := range w.members {
		if !alive {
			continue
		}
		if best < 0 || seat < best {
			best = seat
		}
	}
	w.kingSeat = best
	return best
}

// RoleSnapshot 返回分工表拷贝 + 当前狼王座位（供 buildAgentContextLocked 填充）。
// 非狼 / 未分工时 roles 为空 map。
func (w *WolfPackRoom) RoleSnapshot() (map[int]string, int) {
	if w == nil {
		return nil, -1
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make(map[int]string, len(w.roles))
	for seat, role := range w.roles {
		out[seat] = role
	}
	return out, w.kingSeat
}

// ResetAssignments 清空分工与狼王（restartGameLocked 重开局时调用）。
// 下一局 StartAgentsLocked 会重新 AutoAssignRoles。
func (w *WolfPackRoom) ResetAssignments() {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.roles = make(map[int]string)
	w.kingSeat = -1
}