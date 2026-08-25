package texasholdem

import (
	"context"
	"fmt"
	"sync"
	"time"

	texasherp "LsmAgentGame/agent/thpagent"
	"LsmAgentGame/errcode"
	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// ─────────────────── Room ───────────────────

// TexasHoldemRoom 持有一局德州扑克的内存状态。
type TexasHoldemRoom struct {
	mu         sync.Mutex
	RoomID     string
	Seats      [MaxPlayers]string // 座位 0..5 的 userID，空串表示空位
	State      *GameState
	Spectators map[string]struct{}

	// 2026-08-19 §德州扑克Agent — Agent 集成字段。
	// botSeats[seat]=true 表示该座位由 Agent 接管,人类玩家通过 WS 提交的动作会被拒绝。
	// BotModels[seat] 记录 bot 用的 model_key,view.go 透传给前端用于"🤖 AI"徽章。
	// 由 TexasHoldemAgentDriver.RegisterAgentsLocked 在房间创建时填充。
	BotSeats  [MaxPlayers]bool
	BotModels [MaxPlayers]string

	// 2026-08-19 §德州扑克金币 — 手牌开始时各座位筹码快照。
	// StartHand 时记录,手牌结束后用 delta 结算金币。
	HandStartStacks [MaxPlayers]int

	// 2026-08-19 §德州扑克Agent — Bot 内部状态(透传前端 BotHeartThought/BotThinking)。
	// 由 ws/game_service_texas_bot.go::recordBotThought / setBotThinking 写入,
	// view.go::BuildClientStateWithRoom 读取后填到 ClientGameState.BotHeartThought/BotThinking。
	// 锁语义:必须先持 r.mu 才能读写。
	BotHeartThought [MaxPlayers]string
	BotThinking     [MaxPlayers]bool

	// 2026-08-25 §聊天升级 — bot 最近一条公屏发言(前端座位气泡渲染)。
	// 由 ws/game_service_texas_bot.go::sendBotChat 广播成功后锁内写入;
	// view.go::BuildClientStateWithRoom 读取透传 ClientGameState.BotLastChat。
	// 公屏消息本就是公开信息,无需脱敏;前端按 AtMs 过期自动隐藏。
	BotLastChat [MaxPlayers]BotChatBubble

	// 2026-08-20 §B8 — bot 回合 watchdog 时钟。
	// 每次 gs.Turn 变更（StartHand / ApplyAction 后）由 markTurnLocked 刷新;
	// ws 层 watchdog 每 5s 检查「当前 bot 座位已思考 > timeout+10s」则强制 fold。
	// 锁语义:必须先持 r.mu 才能读写。
	TurnStartSeat int
	TurnStartedAt time.Time
}

// markTurnLocked 记录当前回合开始时间与座位（锁内变体,§92a）。
// 必须在 gs.Turn 被赋予新值之后、且已持有 r.mu 时调用。
func (r *TexasHoldemRoom) markTurnLocked() {
	if r.State == nil {
		return
	}
	r.TurnStartSeat = r.State.Turn
	r.TurnStartedAt = time.Now()
}

// SetBotHeartThoughtLocked 设置指定 bot 座位的最近内心独白(锁内变体)。
// 必须在已持有 r.mu 时调用;截断到 200 字(与 thpagent.Agent.SetInternalThought 对齐)。
func (r *TexasHoldemRoom) SetBotHeartThoughtLocked(seat int, thought string) {
	if seat < 0 || seat >= MaxPlayers {
		return
	}
	const maxLen = 200
	if len(thought) > maxLen {
		thought = thought[:maxLen]
	}
	r.BotHeartThought[seat] = thought
}

// SetBotLastChatLocked 设置指定 bot 座位的最近一条公屏发言(锁内变体,
// 2026-08-25 §聊天升级 — sendBotChat 广播成功后调用,前端座位气泡渲染)。
func (r *TexasHoldemRoom) SetBotLastChatLocked(seat int, text string, atMs int64) {
	if seat < 0 || seat >= MaxPlayers {
		return
	}
	r.BotLastChat[seat] = BotChatBubble{Text: text, AtMs: atMs}
}

// SetBotThinkingLocked 设置指定 bot 座位是否正在思考(锁内变体)。
// 必须在已持有 r.mu 时调用。
func (r *TexasHoldemRoom) SetBotThinkingLocked(seat int, thinking bool) {
	if seat < 0 || seat >= MaxPlayers {
		return
	}
	r.BotThinking[seat] = thinking
}

// ClearBotThinkingAllLocked 重置所有座位的思考标记(每轮结束 / 手牌开始时调用)。
func (r *TexasHoldemRoom) ClearBotThinkingAllLocked() {
	for i := 0; i < MaxPlayers; i++ {
		r.BotThinking[i] = false
	}
}

// SeatOf 返回 userID 所在座位，未入座返回 (-1,false)。
func (r *TexasHoldemRoom) SeatOf(userID string) (int, bool) {
	for i, u := range r.Seats {
		if u == userID {
			return i, true
		}
	}
	return -1, false
}

// Occupied 返回已入座人数。
func (r *TexasHoldemRoom) Occupied() int {
	n := 0
	for _, u := range r.Seats {
		if u != "" {
			n++
		}
	}
	return n
}

// IsReady 报告是否至少 2 人。
func (r *TexasHoldemRoom) IsReady() bool {
	return r.Occupied() >= 2
}

// allBotSeatsOccupiedLocked 报告所有已标记的 bot 座位是否都已入座(锁内变体,§92a)。
// 2026-08-20 §P0-NEW-1 / P0-3:JoinGame 自动开局守卫 —— 在任何已标记的 bot
// 座位尚未入座前禁止 StartHand,否则:
//  1. 开局后才 AddPlayer 的座位本手底牌全为零值(rank:0/suit:0,前端渲染 "? ?");
//  2. 首个 bot 回合的 IsBotSeatTurn 依赖 BotSeats 标记,时序错位会导致
//     onHandStarted → ProcessBotTurn 静默 return,对局永久卡死。
// 纯人类房间 BotSeats 全 false → 真空真,行为不变。
func (r *TexasHoldemRoom) allBotSeatsOccupiedLocked() bool {
	for i := 0; i < MaxPlayers; i++ {
		if r.BotSeats[i] && r.Seats[i] == "" {
			return false
		}
	}
	return true
}

// IsBotSeat 报告指定座位是否为 Agent(2026-08-19 §德州扑克Agent)。
// 必须在已持有 r.mu 时调用(由 Room Service 或 Manager 调用方保证)。
func (r *TexasHoldemRoom) IsBotSeat(seat int) bool {
	if seat < 0 || seat >= MaxPlayers {
		return false
	}
	return r.BotSeats[seat]
}

// RegisterBotSeatsLocked 标记 bot 座位(由 Room Service 在创建房间时调用)。
// 锁内变体(§92a):调用方必须已持有 r.mu。
//
// seatModels: 座位号 → model_key (空字符串视为人类)。
func (r *TexasHoldemRoom) RegisterBotSeatsLocked(seatModels map[int]string) {
	for seat, modelKey := range seatModels {
		if seat < 0 || seat >= MaxPlayers {
			continue
		}
		if modelKey != "" {
			r.BotSeats[seat] = true
			r.BotModels[seat] = modelKey
		}
	}
}

// SeatRestoreInfo 是 seat hydrator 返回的单座位恢复信息(§20260819-02 P0-1)。
// ModelKey 为空表示人类座位;非空表示 Agent 座位(用于 BotSeats/BotModels 标记)。
type SeatRestoreInfo struct {
	Seat     int
	UserID   string
	ModelKey string
}

// ─────────────────── Manager ───────────────────

// TexasHoldemManager 管理所有活跃的德州扑克房间。
type TexasHoldemManager struct {
	mu    sync.RWMutex
	rooms map[string]*TexasHoldemRoom
	// seedFn 提供发牌随机种子；测试可替换为固定值。
	seedFn     func() int64
	BigBlind   int
	StartStack int

	// 2026-08-19 §德州扑克Agent — 新手牌开始回调(由 WS 层注册)。
	// 在延迟 auto-start goroutine 中调用,通知 WS 层广播新状态并触发 bot 行动。
	// 注意:回调在**释放 r.mu 之后**调用（2026-08-20 §B6 修复:此前持锁调用,
	// 回调内 WithRoomLocked 再取 r.mu → sync.Mutex 不可重入自死锁,§92a）。
	onHandStarted func(roomID string)

	// 2026-08-20 §B2 — 手牌结束回调(由 WS 层注册)。
	// 在 ApplyAction/ApplyBotAction 判定 handOver 且释放 r.mu 之后调用,
	// WS 层借此把本手结果写入每个 bot 的 Memory(AppendHand / 对手画像)。
	onHandOver func(roomID string)

	// 2026-08-19 §德州扑克金币 — 钱包服务(由 main.go 注入)。
	// 手牌结束后按筹码盈亏结算金币。
	walletSvc WalletSettler

	// 2026-08-20 §B7 — 单手牌底池上限(筹码),由 main.go 从
	// cfg.TexasHoldem.MaxPotPerHand 注入(默认 100000)。结算时本手底池超过
	// 该上限则按比例封顶赢家结算(防恶意刷金币),并 logger.Warn。
	maxPotPerHand int

	// 2026-08-19 §德州扑克盲注透传 — 房间级盲注/买入配置。
	// 由 RoomService 经 SetTexasHoldemRoomConfigurer 回调在房间创建成功后、
	// 首次 JoinGame 之前写入;缺省房间回退 m.BigBlind / m.StartStack。
	roomConfigs map[string]texasRoomConfig

	// 2026-08-20 §20260819-02 P0-1 - 重启恢复 hydrator(对齐狼人杀 BUG-WEREWOLF-P0-7)。
	// seatHydrator 按 roomID 返回 DB 持久化的座位清单(人类 + Agent),由 main.go
	// 注入(读 roomSvc.SeatsForRoom)。SpectateGame 在内存房间缺失时懒创建并
	// 调此回调恢复座位,避免「DB 房间 / 内存无」导致的观战 404。
	seatHydrator func(roomID string) ([]SeatRestoreInfo, error)
}

// texasRoomConfig 2026-08-19 §德州扑克盲注透传 — 单房间盲注/买入覆盖值。
type texasRoomConfig struct {
	BigBlind   int
	StartStack int
}

// WalletSettler 是 wallet service 的精简接口(避免循环 import)。
type WalletSettler interface {
	Credit(ctx context.Context, userID, txType, refType, refID, gameKind, remark string, amount int64) error
	Debit(ctx context.Context, userID, txType, refType, refID, gameKind, remark string, amount int64) error
	// GetBalance 返回用户当前金币余额(无钱包行时返回 0 + nil,
	// 与 service.WalletService.GetBalance 的防御性约定一致)。
	GetBalance(ctx context.Context, userID string) (int64, error)
}

// NewTexasHoldemManager 创建空管理器。
func NewTexasHoldemManager() *TexasHoldemManager {
	return &TexasHoldemManager{
		rooms:       make(map[string]*TexasHoldemRoom),
		roomConfigs: make(map[string]texasRoomConfig),
		seedFn:      func() int64 { return time.Now().UnixNano() },
		BigBlind:    200,
		StartStack:  10000,
		maxPotPerHand: 100000, // §B7 默认值,与 config 兜底一致;main.go 可覆盖
	}
}

// SetRoomConfig 记录房间级盲注/买入(2026-08-19 §德州扑克盲注透传)。
// 由 RoomService 在房间创建成功后、首次 JoinGame 之前调用。
func (m *TexasHoldemManager) SetRoomConfig(roomID string, bigBlind, startStack int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.roomConfigs[roomID] = texasRoomConfig{BigBlind: bigBlind, StartStack: startStack}
}

// configForLocked 返回房间生效的盲注/买入(未配置时回退 manager 默认值)。
// 锁内变体(§92a):调用方必须已持有 m.mu。
func (m *TexasHoldemManager) configForLocked(roomID string) (bigBlind, startStack int) {
	if cfg, ok := m.roomConfigs[roomID]; ok {
		return cfg.BigBlind, cfg.StartStack
	}
	return m.BigBlind, m.StartStack
}

// SetOnHandStarted 注册新手牌开始回调(§德州扑克Agent)。
// 仅 WS 层调用一次;延迟 auto-start goroutine 触发时通知广播 + bot 行动。
func (m *TexasHoldemManager) SetOnHandStarted(cb func(roomID string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onHandStarted = cb
}

// SetOnHandOver 注册手牌结束回调(2026-08-20 §B2)。
// 仅 WS 层调用一次;handOver 判定后(锁外)通知 bot Memory 记账。
func (m *TexasHoldemManager) SetOnHandOver(cb func(roomID string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onHandOver = cb
}

// SetSeatHydrator 注册重启恢复回调(§20260819-02 P0-1)。
// 由 main.go 注入,读 roomSvc.SeatsForRoom 返回人类 + Agent 全座位。
// fn 为 nil 表示禁用恢复(回退到 7858d33 旧行为:SpectateGame 仍懒创建
// 但不恢复座位,适用单元测试与无 RoomService 的 fixture)。
func (m *TexasHoldemManager) SetSeatHydrator(fn func(roomID string) ([]SeatRestoreInfo, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seatHydrator = fn
}

// SetMaxPotPerHand 注入单手牌底池上限(2026-08-20 §B7)。
// 由 main.go 从 cfg.TexasHoldem.MaxPotPerHand 读出注入;<=0 时保持默认。
func (m *TexasHoldemManager) SetMaxPotPerHand(n int) {
	if n <= 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.maxPotPerHand = n
}

// SetWalletService 注入钱包服务(§德州扑克金币)。由 main.go 调用。
func (m *TexasHoldemManager) SetWalletService(svc WalletSettler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.walletSvc = svc
}

// getOrCreateRoomLocked 返回房间,不存在时懒创建(锁内变体,§92a:调用方
// 必须已持有 m.mu)。JoinGame / RegisterBotSeats 共用同一懒创建逻辑。
// by 仅用于创建日志(标识触发方)。
func (m *TexasHoldemManager) getOrCreateRoomLocked(roomID, by string) *TexasHoldemRoom {
	r, ok := m.rooms[roomID]
	if !ok {
		r = &TexasHoldemRoom{RoomID: roomID}
		m.rooms[roomID] = r
		logger.L().Info("texasholdem room created", zap.String("room_id", roomID), zap.String("by", by))
	}
	return r
}

func (m *TexasHoldemManager) getRoom(roomID string) *TexasHoldemRoom {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.rooms[roomID]
}

// GetRoomForBot 返回房间指针供 WS 层 Bot 驱动写入 BotHeartThought / BotThinking
// (2026-08-19 §德州扑克Agent v1.1)。调用方拿到指针后**仍需自行 r.mu.Lock** 读/写字段;
// 本方法不加锁是因为上层可能要做"快照 + 锁内写 + 释放"的原子序列。
func (m *TexasHoldemManager) GetRoomForBot(roomID string) *TexasHoldemRoom {
	return m.getRoom(roomID)
}

// WithRoomLocked 用回调方式执行任意闭包并自动持锁/释放,供 ws 层 bot handler
// 在不直接访问 unexported r.mu 字段的前提下安全修改 BotHeartThought / BotThinking。
//
// fn 返回后立即释放锁;fn 本身**禁止**再调用任何持有 r.mu 的房间方法,以免 §92a 自死锁。
func (m *TexasHoldemManager) WithRoomLocked(roomID string, fn func(r *TexasHoldemRoom)) {
	if fn == nil {
		return
	}
	r := m.getRoom(roomID)
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	fn(r)
}

// RegisterBotSeats 一次性标记房间的全部 bot 座位(BotSeats/BotModels)。
// 房间不存在时按 JoinGame 同款逻辑懒创建。
//
// 调用方:ws 层 registerTexasHoldemAgentSeats(2026-08-20 §P0-NEW-1 时序契约:
// BotSeats 标记 + driver 注册必须先于任何可能触发 StartHand 的 JoinGame,
// 否则首个 bot 回合 IsBotSeatTurn 返回 false,ProcessBotTurn 静默 return,
// 对局永久卡死)。幂等:重复调用只覆写相同座位,无副作用。
func (m *TexasHoldemManager) RegisterBotSeats(roomID string, seatModels map[int]string) {
	m.mu.Lock()
	r := m.getOrCreateRoomLocked(roomID, "bot-seats-register")
	m.mu.Unlock()

	r.mu.Lock()
	defer r.mu.Unlock()
	r.RegisterBotSeatsLocked(seatModels)
}

// UnregisterBotSeat 清除指定座位的 bot 标记(BotSeats/BotModels)。
//
// 调用方:ws 层 registerTexasHoldemAgentSeats 的 JoinGame 失败回滚路径 ——
// 若 bot 入座失败却保留 BotSeats[seat]=true,JoinGame 自动开局守卫
// allBotSeatsOccupiedLocked 会永远不满足,对局无法开始。幂等:清除空
// 标记 / 座位越界 / 房间不存在均安全返回。
func (m *TexasHoldemManager) UnregisterBotSeat(roomID string, seat int) {
	r := m.getRoom(roomID)
	if r == nil {
		return
	}
	if seat < 0 || seat >= MaxPlayers {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.BotSeats[seat] = false
	r.BotModels[seat] = ""
}

// JoinGame 让 userID 入座(first-empty 顺次找空位)。幂等。
// 达到 2 人且所有已标记的 bot 座位均已入座时自动发牌进入翻前阶段
// (2026-08-20 §P0-NEW-1:追加 allBotSeatsOccupiedLocked 守卫)。
func (m *TexasHoldemManager) JoinGame(roomID, userID string) (room *TexasHoldemRoom, started bool, joinErr *errcode.Error) {
	return m.joinInternal(roomID, userID, -1)
}

// JoinGameAtSeat 让 userID 入座到指定座位(2026-08-20 §P0-NEW-1)。
// 与 JoinGame 的差别:座位由调用方指定(DB 持久化座位 = 内存物理座位 =
// BotSeats 标记座位 = driver 座位,四者一致)。前端 agent_seats 座位号随机,
// bot 若走 first-empty 入座会导致配置/物理座位错位,allBotSeatsOccupiedLocked
// 守卫可能永不满足(永久不开局)。调用方:ws 层 registerTexasHoldemAgentSeats。
// 幂等:已入座(任意座位)直接返回;指定座位被占/越界返回错误。
func (m *TexasHoldemManager) JoinGameAtSeat(roomID, userID string, seat int) (room *TexasHoldemRoom, started bool, joinErr *errcode.Error) {
	return m.joinInternal(roomID, userID, seat)
}

// joinInternal 是 JoinGame / JoinGameAtSeat 的共用实现。
// wantSeat < 0 表示 first-empty 顺次找空位;>= 0 表示必须入座该座位。
func (m *TexasHoldemManager) joinInternal(roomID, userID string, wantSeat int) (room *TexasHoldemRoom, started bool, joinErr *errcode.Error) {
	m.mu.Lock()
	r := m.getOrCreateRoomLocked(roomID, userID)
	// 2026-08-19 §德州扑克盲注透传: 在持有 m.mu 时快照房间级盲注/买入
	// (下方 NewGame/AddPlayer 在 m.mu 释放后执行,不能再读 m.roomConfigs)。
	bigBlind, startStack := m.configForLocked(roomID)
	m.mu.Unlock()

	r.mu.Lock()

	// 幂等：已入座直接返回
	if _, seated := r.SeatOf(userID); seated {
		r.mu.Unlock()
		return r, false, nil
	}

	// 找座位:first-empty 或指定座位
	seat := wantSeat
	if seat < 0 {
		seat = -1
		for i, u := range r.Seats {
			// 2026-08-20 §P0-NEW-1:跳过已标记(即便尚未入座)的 bot 座位 ——
			// bot 由 JoinGameAtSeat 按 DB 配置座位入座,first-empty 路径(人类)
			// 不得抢占,否则 allBotSeatsOccupiedLocked 守卫永不满足(永久不开局)
			// 且「配置座位 = 物理座位 = BotSeats 标记 = driver 座位」对齐被打破。
			if u == "" && !r.BotSeats[i] {
				seat = i
				break
			}
		}
	} else if seat >= MaxPlayers || r.Seats[seat] != "" {
		// 指定座位越界或已被占
		seat = -1
	}
	if seat == -1 {
		r.mu.Unlock()
		return nil, false, errcode.Code(errcode.ErrRoomFull)
	}
	r.Seats[seat] = userID

	if r.State == nil {
		r.State = NewGame(m.seedFn(), bigBlind)
	}
	var e *errcode.Error
	if wantSeat >= 0 {
		_, e = r.State.AddPlayerAt(userID, seat, startStack)
	} else {
		_, e = r.State.AddPlayer(userID, startStack)
	}
	if e != nil {
		// 不应发生（刚找到空位）
		logger.L().Warn("texasholdem add player failed", zap.String("room_id", roomID), zap.Error(e))
	}

	// 满 2 人且当前阶段为 waiting，自动开首手
	// (2026-08-20 §P0-NEW-1:含 bot 房间须等全部已标记 bot 座位入座后再开局,
	// 否则后入座者本手底牌全零且 bot 驱动因 BotSeats 时序错位永久卡死)
	if r.IsReady() && r.State.Street == PhaseWaiting && r.allBotSeatsOccupiedLocked() {
		// 2026-08-19 §德州扑克金币: 记录各座位手牌开始时的筹码
		r.snapshotHandStartStacks()
		if e := r.State.StartHand(); e != nil {
			logger.L().Warn("texasholdem start hand failed", zap.String("room_id", roomID), zap.Error(e))
		} else {
			started = true
			r.markTurnLocked() // §B8 bot 回合 watchdog 时钟
			logger.L().Info("texasholdem hand started",
				zap.String("room_id", roomID),
				zap.Int("hand_number", r.State.HandNumber))
		}
	}
	r.mu.Unlock()

	// 2026-08-20 §P0-NEW-R: JoinGame 自动开首手后,锁外触发 onHandStarted 回调
	// (清理思考标记 + 广播新手牌 + 驱动 bot 决策),与 runHandOverEpilogue 路径对齐。
	// 必须在释放 r.mu 之后调用(§B6: 回调内会再取 r.mu,不可重入)。
	if started && m.onHandStarted != nil {
		m.onHandStarted(roomID)
	}
	return r, started, nil
}

// Action 处理玩家动作。返回 (房间, 手牌是否结束, 错误)。
// 手牌结束后，若满足开新一手条件则自动开始下一手。
func (m *TexasHoldemManager) Action(roomID, userID string, a Action) (*TexasHoldemRoom, bool, *errcode.Error) {
	r := m.getRoom(roomID)
	if r == nil {
		return nil, false, errcode.Code(errcode.ErrRoomNotFound)
	}
	r.mu.Lock()

	if r.State == nil {
		r.mu.Unlock()
		return nil, false, errcode.Code(errcode.ErrGameNotStarted)
	}
	seat, ok := r.State.GetSeat(userID)
	if !ok {
		r.mu.Unlock()
		return nil, false, errcode.Code(errcode.ErrRoomNotIn)
	}

	// 2026-08-19 §德州扑克Agent: bot 座位的行动由 driver 自动驱动,
	// 人类玩家在 bot 座位上提交动作直接拒绝(防止作弊)。
	if r.BotSeats[seat] {
		r.mu.Unlock()
		return nil, false, errcode.CodeMsg(errcode.ErrInvalidMove, "seat is bot-controlled")
	}

	handOver, e := r.State.ApplyAction(seat, a)
	if e != nil {
		r.mu.Unlock()
		return nil, false, e
	}
	r.markTurnLocked() // §B8: 回合易主,刷新 watchdog 时钟

	// handOver 后处理在锁外进行(2026-08-20 §B6: 此前 onHandStarted 回调在
	// 持锁的延迟 goroutine 里触发,回调内 WithRoomLocked 再取 r.mu 自死锁)。
	canNext := handOver && r.State.CanStartHand()
	r.mu.Unlock()

	if handOver {
		m.runHandOverEpilogue(roomID, r, canNext)
	}

	return r, handOver, nil
}

// runHandOverEpilogue 手牌结束后的统一收尾（§B2/§B6，锁外调用）：
//  1. 异步金币结算（SettleHandCoins 自行取锁）
//  2. onHandOver 回调（bot Memory 记账）
//  3. 满足条件时延迟 5s 自动开下一手，成功后（锁外）触发 onHandStarted
func (m *TexasHoldemManager) runHandOverEpilogue(roomID string, r *TexasHoldemRoom, canNext bool) {
	// 2026-08-19 §德州扑克金币: 手牌结束,异步结算金币
	//
	// 2026-08-21 §20260821-P0-1: SettleHandCoins 是异步 goroutine,内部
	// 调用 walletSvc / DB 等不可控依赖,历史无 defer recover;一旦下游
	// 抛 panic 会直接击穿进程(`pgrep LsmAgentGame` 消失)。补 recover
	// 兜底 + zap.Stack 便于定位(对齐 ProcessBotTurn 既有 recover 风格)。
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				logger.L().Error("texasholdem SettleHandCoins panic recovered",
					zap.String("room_id", roomID),
					zap.Any("panic", rec),
					zap.Stack("stack"))
			}
		}()
		m.SettleHandCoins(roomID)
	}()

	// 2026-08-20 §B2: bot Memory 记账（回调内部自行取锁,必须在锁外调）
	if m.onHandOver != nil {
		m.onHandOver(roomID)
	}

	if !canNext {
		return
	}
	// 延迟开新一手（给客户端 time 展示结果）。
	//
	// 2026-08-21 §20260821-P0-1: 此 goroutine 此前无 recover —— df6b6a7 补的
	// zap.Stack 只覆盖了 ProcessBotTurn 路径;跨手牌堆耗尽 → drawCard 触发
	// `index out of range [-1]` 时直接击穿进程,所有房间中断。
	// 现补 defer recover + zap.Stack,与 SettleHandCoins 一致。
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				logger.L().Error("texasholdem auto-next-hand goroutine panic recovered",
					zap.String("room_id", roomID),
					zap.Any("panic", rec),
					zap.Stack("stack"))
			}
		}()
		time.Sleep(5 * time.Second)
		r.mu.Lock()
		started := false
		if r.State.Street == PhaseOver || r.State.Street == PhaseShowdown {
			// 2026-08-19 §德州扑克金币: 新手牌开始前记录筹码快照
			r.snapshotHandStartStacks()
			if e := r.State.StartHand(); e != nil {
				logger.L().Warn("auto start next hand failed", zap.String("room_id", roomID), zap.Error(e))
			} else {
				started = true
				r.markTurnLocked() // §B8
				logger.L().Info("texasholdem next hand", zap.String("room_id", roomID), zap.Int("hand", r.State.HandNumber))
			}
		}
		r.mu.Unlock()
		if started {
			// 2026-08-19 §德州扑克Agent: 新手牌开始,通知 WS 层广播 + 触发 bot。
			// 必须在释放 r.mu 之后调用(§B6 死锁修复:回调内会再取 r.mu)。
			if m.onHandStarted != nil {
				m.onHandStarted(roomID)
			}
		}
	}()
}

// Resign 认输（等同弃牌）。
func (m *TexasHoldemManager) Resign(roomID, userID string) (*TexasHoldemRoom, *errcode.Error) {
	r := m.getRoom(roomID)
	if r == nil {
		return nil, errcode.Code(errcode.ErrRoomNotFound)
	}
	r.mu.Lock()

	if r.State == nil || r.State.Status != StatusPlaying {
		r.mu.Unlock()
		return nil, errcode.Code(errcode.ErrGameAlreadyOver)
	}
	seat, ok := r.State.GetSeat(userID)
	if !ok {
		r.mu.Unlock()
		return nil, errcode.Code(errcode.ErrRoomNotIn)
	}

	// 2026-08-19 §德州扑克Agent: bot 座位不接受人类 resign(§92a 锁内守卫,
	// 防止作弊绕过 BotDriver.ApplyBotAction)。
	if r.BotSeats[seat] {
		r.mu.Unlock()
		return nil, errcode.CodeMsg(errcode.ErrInvalidMove, "seat is bot-controlled")
	}

	handOver := false
	if !r.State.Players[seat].Folded {
		r.State.Players[seat].Folded = true
		// 检查是否只剩 1 人
		if r.State.activePlayers() <= 1 {
			r.State.endHandFold()
			handOver = true
		}
	}
	canNext := handOver && r.State.CanStartHand()
	r.mu.Unlock()

	// 2026-08-24 BUG-TEXAS-DISCARD-STALL 同族修复:resign 直接结束手牌时
	// 此前不跑 runHandOverEpilogue —— 金币不结算、bot Memory 不记账、
	// 5s 自动开下一手不触发,纯 bot 房会停在 PhaseOver 直到 vacancy 删除。
	// 与 Action / ApplyBotAction / ForceRemovePlayer 对齐(§B6:锁外调用)。
	if handOver {
		m.runHandOverEpilogue(roomID, r, canNext)
	}
	return r, nil
}

// ForceRemovePlayer 强制移除断线超时(15s)被踢出的人类玩家座位
// (2026-08-24 BUG-TEXAS-DISCARD-STALL,R17 P1)。
//
// 缺陷背景:hub.handleDisconnectTimeout → RoomService.LeaveRoom 只清 DB 行,
// in-memory 对局的回合仍停在已离线的人类座位上 —— bot 回合 watchdog 只管
// bot 座位(IsBotSeatTurn),人类座位没有任何超时推进机制,手牌永久停滞
// (实测 ≥10min),底池与各方已投入筹码悬置。
//
// 语义:
//  1. 手牌进行中(Street 在 preflop..river)且该座位未弃牌:
//     - 当前正好轮到该座位 → 走 ApplyAction(fold) 正道,回合推进/街推进/
//       结算与正常 fold 完全一致;
//     - 未轮到 → 直接标记 Folded;只剩 1 名未弃牌玩家时 endHandFold 收尾。
//  2. 无论手牌是否进行中,都把该座位从引擎(RemovePlayer)与 r.Seats 摘除,
//     防止后续手牌继续给空座发牌、再次把回合停在幽灵座位上。
//  3. bot 座位不从此路径移除(bot 没有 hub 连接,不会产生断线超时)。
//
// 返回 (removed, handOver, handNum, delta):
//   - removed=false 表示房间不存在 / 该用户不在座 / 是 bot 座位(调用方 no-op);
//   - delta 为该玩家本手筹码净变化(仅手牌进行中时有意义,恒 ≤ 0),
//     供 ws 层单独结算钱包 —— 该玩家被摘除后已不在 SettleHandCoins 的
//     快照内,不单独入账会破坏「赢家 credit ↔ 输家 debit」守恒。
//
// 锁语义:runHandOverEpilogue 在锁外调用(§B6 死锁教训)。
func (m *TexasHoldemManager) ForceRemovePlayer(roomID, userID string) (removed bool, handOver bool, handNum int, delta int) {
	r := m.getRoom(roomID)
	if r == nil {
		return false, false, 0, 0
	}
	r.mu.Lock()
	if r.State == nil {
		r.mu.Unlock()
		return false, false, 0, 0
	}
	seat, ok := r.State.GetSeat(userID)
	if !ok || r.BotSeats[seat] {
		r.mu.Unlock()
		return false, false, 0, 0
	}

	handNum = r.State.HandNumber
	inHand := r.State.Status == StatusPlaying &&
		r.State.Street != PhaseWaiting && r.State.Street != PhaseOver && r.State.Street != PhaseShowdown
	if inHand {
		delta = r.State.Players[seat].Stack - r.HandStartStacks[seat]
		if !r.State.Players[seat].Folded {
			if r.State.Turn == seat {
				// 正道:fold 走 ApplyAction,回合推进/街推进/摊牌逻辑全部复用。
				if ho, e := r.State.ApplyAction(seat, Action{Type: ActFold}); e == nil {
					handOver = ho
				} else {
					// ApplyAction 对回合内玩家的 fold 不应失败;兜底直接标记,
					// 绝不让移除动作被引擎拒绝而重新卡死。
					logger.L().Warn("texasholdem force-remove fold rejected, marking folded directly",
						zap.String("room_id", roomID),
						zap.Int("seat", seat),
						zap.Int("code", e.Code))
					r.State.Players[seat].Folded = true
					if r.State.activePlayers() <= 1 {
						r.State.endHandFold()
						handOver = true
					}
				}
			} else {
				r.State.Players[seat].Folded = true
				if r.State.activePlayers() <= 1 {
					r.State.endHandFold()
					handOver = true
				}
			}
		}
	}

	// 从引擎座位表与房间座位表摘除,后续手牌不再给该座位发牌。
	r.State.RemovePlayer(userID)
	r.Seats[seat] = ""
	r.BotHeartThought[seat] = ""
	r.BotThinking[seat] = false
	r.HandStartStacks[seat] = 0
	canNext := handOver && r.State.CanStartHand()
	r.mu.Unlock()

	logger.L().Warn("texasholdem disconnected player force-removed",
		zap.String("room_id", roomID),
		zap.String("user_id", userID),
		zap.Int("seat", seat),
		zap.Bool("hand_over", handOver),
		zap.Int("hand", handNum),
		zap.Int("delta", delta))

	if handOver {
		// §B6:回调与结算必须锁外(runHandOverEpilogue 内部自行取锁)。
		m.runHandOverEpilogue(roomID, r, canNext)
	}
	return true, handOver, handNum, delta
}

// SettlePlayerDelta 为被 ForceRemovePlayer 摘除的玩家单独结算本手净输赢
// (BUG-TEXAS-DISCARD-STALL)。该玩家已不在 SettleHandCoins 的 Players 快照
// 内,若不单独入账,赢家 credit 将没有对应的输家 debit,金币凭空增发。
//
// 手牌进行中被移除的玩家 delta 恒 ≤ 0(底池只在手牌结束时分配,进行中的
// 玩家不可能已赢池),因此只需 debit 路径;delta > 0 属异常,记录后按
// credit(不抽水 —— 无法在不重扫房间的情况下计算 tier,且该场景本不应发生)。
func (m *TexasHoldemManager) SettlePlayerDelta(roomID, userID string, handNum, delta int) {
	if m.walletSvc == nil || userID == "" || delta == 0 {
		return
	}
	refID := roomID + ":" + fmt.Sprintf("%d", handNum)
	remark := fmt.Sprintf("texasholdem hand #%d settle (disconnect removal)", handNum)
	if delta > 0 {
		logger.L().Warn("texasholdem removed player has positive delta (unexpected mid-hand)",
			zap.String("room_id", roomID),
			zap.String("user_id", userID),
			zap.Int("delta", delta))
		if err := m.walletSvc.Credit(context.Background(), userID, "game_win", "texasholdem_settle", refID, "texasholdem", remark, int64(delta)); err != nil {
			logger.L().Warn("texasholdem removed-player credit failed",
				zap.String("room_id", roomID), zap.String("user_id", userID), zap.Error(err))
		}
		return
	}
	m.debitClamped(roomID, userID, "game_lose", "texasholdem_settle", refID, "texasholdem", remark, int64(-delta), delta, handNum)
}

// debitClamped 先查余额,把 debit 金额 clamp 到当前钱包余额后扣款
// (R18 P2 修复,2026-08-25 §20260825-P2)。此前全额 Debit 失败时整笔欠账
// (debt carried),输家 Bot 连续多手全押会让债务滚雪球且永不结算 —— 因为
// 下次结算仍全额尝试,钱包余额永远 0,坏账单调增长。改为「有多少扣多少,
// 余额为 0 则跳过」,债务被就地吸收,不再累积。桌内筹码(引擎权威)不受影响,
// 钱包只是上层投影;短缺部分记 shortfall 告警供运营审计。
func (m *TexasHoldemManager) debitClamped(roomID, userID, txType, refType, refID, gameKind, remark string, amount int64, origDelta int, handNum int) {
	ctx := context.Background()
	balance, err := m.walletSvc.GetBalance(ctx, userID)
	if err != nil {
		logger.L().Warn("texasholdem settle get balance failed, fallback to full debit",
			zap.String("room_id", roomID), zap.String("user_id", userID), zap.Error(err))
	}
	if err == nil && balance < amount {
		logger.L().Warn("texasholdem settle debit clamped to wallet balance (shortfall absorbed)",
			zap.String("room_id", roomID),
			zap.String("user_id", userID),
			zap.Int("hand", handNum),
			zap.Int("delta", origDelta),
			zap.Int64("attempted", amount),
			zap.Int64("balance", balance))
		if balance <= 0 {
			return
		}
		amount = balance
	}
	if err := m.walletSvc.Debit(ctx, userID, txType, refType, refID, gameKind, remark, amount); err != nil {
		logger.L().Warn("texasholdem settle debit failed",
			zap.String("room_id", roomID),
			zap.String("user_id", userID),
			zap.Int64("attempted", amount),
			zap.Error(err))
	}
}

// GetState 返回某玩家可见的对局视图。
func (m *TexasHoldemManager) GetState(roomID, userID string) (*ClientGameState, *errcode.Error) {
	r := m.getRoom(roomID)
	if r == nil {
		return nil, errcode.Code(errcode.ErrRoomNotFound)
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	seat, ok := r.SeatOf(userID)
	if !ok {
		return nil, errcode.Code(errcode.ErrRoomNotIn)
	}
	return BuildClientStateWithRoom(roomID, r.Seats, r.BotSeats, r.BotModels, seat, r.State, r.BotHeartThought, r.BotThinking, r.BotLastChat), nil
}

// StateForSeat 在已持有房间引用时构造指定座位视图。
func (m *TexasHoldemManager) StateForSeat(roomID string, seat int) *ClientGameState {
	r := m.getRoom(roomID)
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return BuildClientStateWithRoom(roomID, r.Seats, r.BotSeats, r.BotModels, seat, r.State, r.BotHeartThought, r.BotThinking, r.BotLastChat)
}

// Seats 返回房间各座位 userID 的快照。
func (m *TexasHoldemManager) Seats(roomID string) ([MaxPlayers]string, bool) {
	r := m.getRoom(roomID)
	if r == nil {
		return [MaxPlayers]string{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Seats, true
}

// RemoveGame 从管理器移除房间。
func (m *TexasHoldemManager) RemoveGame(roomID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.rooms, roomID)
	// 2026-08-19 §德州扑克盲注透传: 同步清理房间级配置,防止 map 泄漏。
	delete(m.roomConfigs, roomID)
}

// IsRoomActive 报告 roomID 是否在内存管理器中存在且仍有玩家入座。
// 由 RoomService 的 stale-room janitor 调用(2026-08-22 §BUG-TEXAS-JANITOR-SPLITBRAIN):
// 德州扑克开手后 DB status 永不推进到 'playing',30 分钟 staleMaxAge 会把活跃对局
// 当「陈旧 open 房」强删。内存态是权威 —— 只要管理器里还有玩家,就跳过不删,
// 避免 REST 404 / DB 无记录 / WS 对局照常的三层分裂脑。
func (m *TexasHoldemManager) IsRoomActive(roomID string) bool {
	r := m.getRoom(roomID)
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Occupied() > 0
}

// ─────────────────── Spectator API ───────────────────

// ─────────────────── Bot Agent API (2026-08-19 §德州扑克Agent) ───────────────────

// IsBotSeatTurn 报告当前是否轮到 bot 座位行动。
// 由 WS 层在每次 Action/JoinGame/StartHand 后调用。
func (m *TexasHoldemManager) IsBotSeatTurn(roomID string) (seat int, isBot bool) {
	r := m.getRoom(roomID)
	if r == nil || r.State == nil {
		return -1, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.State.Street == PhaseWaiting || r.State.Street == PhaseOver || r.State.Street == PhaseShowdown {
		return -1, false
	}
	turn := r.State.Turn
	if turn < 0 || turn >= MaxPlayers {
		return -1, false
	}
	return turn, r.BotSeats[turn]
}

// BotSeatModelKey 返回指定 bot 座位的 model_key。
func (m *TexasHoldemManager) BotSeatModelKey(roomID string, seat int) string {
	r := m.getRoom(roomID)
	if r == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if seat < 0 || seat >= MaxPlayers || !r.BotSeats[seat] {
		return ""
	}
	return r.BotModels[seat]
}

// BotGameContext 构造指定 bot 座位的 GameContextForAgent 快照。
// 调用方不持有任何锁;此函数内部自行获取。
// 返回 nil 表示房间不存在或该座位不是 bot。
func (m *TexasHoldemManager) BotGameContext(roomID string, seat int) *BotGameContextSnapshot {
	r := m.getRoom(roomID)
	if r == nil || r.State == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if seat < 0 || seat >= MaxPlayers || !r.BotSeats[seat] {
		return nil
	}
	gs := r.State
	if gs.Street == PhaseWaiting || gs.Street == PhaseOver || gs.Street == PhaseShowdown {
		return nil
	}
	p := &gs.Players[seat]
	if p.UserID == "" || p.Folded {
		return nil
	}

	callAmount := gs.CurrentBet - p.RoundCommitted
	if callAmount < 0 {
		callAmount = 0
	}

	// 构建公共牌数组(转 int 编码: rank*4+suit+1, 与 thpagent 对齐)
	var community [5]int
	for i := 0; i < gs.CommunityShown && i < 5; i++ {
		community[i] = cardToInt(gs.Community[i])
	}

	// 构建动作历史摘要
	var actionHistory string
	roomTotalCoin := int64(0)
	for i := 0; i < MaxPlayers; i++ {
		pl := &gs.Players[i]
		if pl.UserID == "" {
			continue
		}
		roomTotalCoin += int64(pl.Stack)
		marker := ""
		if i == seat {
			marker = " ← 你"
		}
		if pl.Folded {
			actionHistory += fmt.Sprintf("  座位%d: 已弃牌%s\n", i+1, marker)
		} else if pl.AllIn {
			actionHistory += fmt.Sprintf("  座位%d: 全押 %d%s\n", i+1, pl.TotalCommitted, marker)
		} else {
			actionHistory += fmt.Sprintf("  座位%d: 筹码 %d, 本轮已注 %d%s\n", i+1, pl.Stack, pl.RoundCommitted, marker)
		}
	}

	// 计算经济档位(§132 §133 联动)
	tier := texasherp.ComputeEconTier(roomTotalCoin)
	econTierStr := string(tier)
	rakeRatePct := tier.RakeRatePct()

	return &BotGameContextSnapshot{
		RoomID:       roomID,
		HandNumber:   gs.HandNumber,
		Street:       gs.Street.String(),
		MySeat:       seat,
		MyUserID:     p.UserID, // §B2: Memory 记账需要行动者 userID
		MyHole:       [2]int{cardToInt(p.Hole[0]), cardToInt(p.Hole[1])},
		Community:    community,
		CommunityLen: gs.CommunityShown,
		MyStack:      p.Stack,
		MyRoundCommitted: p.RoundCommitted, // §B3: prompt 真实值(此前字面「?」占位)
		Pot:          gs.Pot,
		CurrentBet:   gs.CurrentBet,
		CallAmount:   callAmount,
		Position:     "", // 由 thpagent.Position() 填充
		Opponents:    gs.activePlayers() - 1,
		Button:       gs.Button,
		BigBlind:     gs.BigBlind,
		MinRaise:     gs.MinRaise,
		ActionHistory: actionHistory,
		RoomTotalCoin: int(roomTotalCoin),
		EconTier:      econTierStr,
		RakeRatePct:   rakeRatePct,
	}
}

// BotGameContextSnapshot 是 bot 决策所需的完整上下文快照。
// 与 thpagent.GameContextForAgent 平行(避免循环 import)。
type BotGameContextSnapshot struct {
	RoomID        string
	HandNumber    int
	Street        string
	MySeat        int
	MyUserID      string // 本座位 userID(2026-08-20 §B2: Memory 记账)
	MyHole        [2]int
	Community     [5]int
	CommunityLen  int
	MyStack       int
	MyRoundCommitted int // 本轮已下注(2026-08-20 §B3)
	Pot           int
	CurrentBet    int
	CallAmount    int
	Position      string
	Opponents     int
	Button        int
	BigBlind      int
	MinRaise      int
	ActionHistory string

	// 2026-08-19 §德州扑克金币 — 经济档位联动(§132 §133)。
	// RoomTotalCoin 是房间所有存活玩家当前 Stack 之和(不含旁观金币账户)。
	// EconTier / RakeRatePct 由 thpagent.ComputeEconTier 计算后回填。
	RoomTotalCoin int
	EconTier      string
	RakeRatePct   int
}

// ApplyBotAction 应用 bot 座位的动作。由 WS 层在 driver.DecideAction 返回后调用。
// 返回 (手牌是否结束, 错误)。
func (m *TexasHoldemManager) ApplyBotAction(roomID string, seat int, a Action) (bool, *errcode.Error) {
	r := m.getRoom(roomID)
	if r == nil {
		return false, errcode.Code(errcode.ErrRoomNotFound)
	}
	r.mu.Lock()

	if r.State == nil {
		r.mu.Unlock()
		return false, errcode.Code(errcode.ErrGameNotStarted)
	}
	if seat != r.State.Turn {
		r.mu.Unlock()
		return false, errcode.Code(errcode.ErrNotYourTurn)
	}
	if !r.BotSeats[seat] {
		r.mu.Unlock()
		return false, errcode.CodeMsg(errcode.ErrInvalidMove, "not a bot seat")
	}

	handOver, e := r.State.ApplyAction(seat, a)
	if e != nil {
		r.mu.Unlock()
		return false, e
	}
	r.markTurnLocked() // §B8: 回合易主,刷新 watchdog 时钟

	canNext := handOver && r.State.CanStartHand()
	r.mu.Unlock()

	if handOver {
		// 与 Action 走同一收尾路径(§B2 Memory 记账 + §B6 锁外回调)
		m.runHandOverEpilogue(roomID, r, canNext)
	}

	return handOver, nil
}

// BotTurnInfo 返回当前回合信息供 ws 层 bot 回合 watchdog 使用（2026-08-20 §B8）。
// 返回 (当前回合座位, 回合开始时间, 该座位是否 bot, 房间是否存在且对局进行中)。
func (m *TexasHoldemManager) BotTurnInfo(roomID string) (seat int, startedAt time.Time, isBot bool, ok bool) {
	r := m.getRoom(roomID)
	if r == nil || r.State == nil {
		return -1, time.Time{}, false, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.State.Street == PhaseWaiting || r.State.Street == PhaseOver || r.State.Street == PhaseShowdown {
		return -1, r.TurnStartedAt, false, true
	}
	turn := r.State.Turn
	if turn < 0 || turn >= MaxPlayers {
		return -1, r.TurnStartedAt, false, true
	}
	return turn, r.TurnStartedAt, r.BotSeats[turn], true
}

// HandEndSummaries 返回本手牌结束时各座位的结算摘要（2026-08-20 §B2）。
// 由 ws 层 onHandOver 回调调用（锁外），结果喂给 thpagent.Driver.AppendHandResult。
// ok=false 表示房间不存在或尚未开过任何一手。
func (m *TexasHoldemManager) HandEndSummaries(roomID string) (handNumber int, community [5]int, communityLen int, winners []int, seats []texasherp.HandSeatSummary, ok bool) {
	r := m.getRoom(roomID)
	if r == nil || r.State == nil {
		return 0, [5]int{}, 0, nil, nil, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	gs := r.State
	if gs.HandNumber == 0 {
		return 0, [5]int{}, 0, nil, nil, false
	}
	for i := 0; i < gs.CommunityShown && i < 5; i++ {
		community[i] = cardToInt(gs.Community[i])
	}
	communityLen = gs.CommunityShown
	winners = append([]int{}, gs.Winners...)
	wonSet := make(map[int]bool, len(gs.Winners))
	for _, w := range gs.Winners {
		wonSet[w] = true
	}
	for i := 0; i < MaxPlayers; i++ {
		p := &gs.Players[i]
		if p.UserID == "" {
			continue
		}
		seats = append(seats, texasherp.HandSeatSummary{
			Seat:     i,
			UserID:   p.UserID,
			Hole:     [2]int{cardToInt(p.Hole[0]), cardToInt(p.Hole[1])},
			NetDelta: p.Stack - r.HandStartStacks[i],
			Won:      wonSet[i],
		})
	}
	return gs.HandNumber, community, communityLen, winners, seats, true
}

// OverSummary 返回手牌结束广播所需的快照（winners/status/hand_number）。
// 供 ws 层在只有 roomID 的场景（bot 路径 / watchdog 强制 fold）构造 game.over 帧;
// 此前 broadcastTexasHoldemOver(roomID, nil) 因 room==nil 直接 return,game.over 从未下发。
func (m *TexasHoldemManager) OverSummary(roomID string) (winners []int, status string, handNumber int, ok bool) {
	r := m.getRoom(roomID)
	if r == nil || r.State == nil {
		return nil, "", 0, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]int{}, r.State.Winners...), r.State.Status.String(), r.State.HandNumber, true
}

// cardToInt 把 Card 转为 thpagent 使用的规范 int 编码 (Rank-2)*4+(Suit-1)+1。
// rank: 2..14 (2..A), suit: 1..4 (Spade/Heart/Club/Diamond)
// 返回 1..52; 零值 Card 返回 0（「无牌」哨兵）。
//
// 2026-08-20 §B1：旧编码 Rank*4+Suit+1（值域 10..61）与 thpagent 的 1..52
// 牌堆 + (c-1)/4 解码不一致 —— 底牌 >52 永远不会从抽样牌堆剔除（对手可被
// 发到同一张物理牌）且解码 rank 错误。现统一为唯一规范编码，由
// card_encoding_test.go 的往返测试 + 评估器交叉对照强制防漂移。
func cardToInt(c Card) int {
	if c.Rank == 0 && c.Suit == 0 {
		return 0
	}
	return texasherp.EncodeCard(c.Rank, c.Suit)
}

// ─────────────────── 金币结算 (2026-08-19 §德州扑克金币) ───────────────────

// snapshotHandStartStacks 记录各座位手牌开始时的筹码(锁内调用)。
func (r *TexasHoldemRoom) snapshotHandStartStacks() {
	for i := 0; i < MaxPlayers; i++ {
		if r.State != nil && r.State.Players[i].UserID != "" {
			r.HandStartStacks[i] = r.State.Players[i].Stack
		} else {
			r.HandStartStacks[i] = 0
		}
	}
}

// SettleHandCoins 手牌结束后按筹码盈亏结算金币。
// 由 WS 层在 handOver 时调用。异步执行,不阻塞游戏流。
//
// 结算规则(v1.2,2026-08-21 §20260821-P1-1):
//   - delta = 当前筹码 - 手牌开始筹码
//   - delta > 0 → Credit 赢得金币,但先按 EconTier 抽水(赢家份额 = delta - rake)
//   - delta < 0 → Debit 损失金币(输家不抽水)
//   - 房间总金币 = Σ存活玩家金币,按 ComputeEconTier 计算档位
//   - 抽水明细写 t_lsm_game_wallet_log(reason="texasholdem_rake")
//   - 人类 + bot 都结算(bot 关联 model 用户的金币账户)
//
// 2026-08-21 §20260821-P1-1 重大修改(对齐 R10/R11 报告):
//   历史版本(§B7)对赢家单独加 ±5000 硬顶 clamp,导致:
//     - winner +9400 → clamp +5000(少付 4400)
//     - loser -8000 → clamp -5000,然后钱包不足 → debit 失败
//     → 筹码守恒被破坏:7700 筹码凭空消失(桌面上 +9400,钱包只入账 +5000
//       + 试图扣 -5000 失败),R10/R11 报告反复复现。
//   现改为:不再做单玩家硬顶 clamp,只保留 pot 级 MaxPotPerHand 缩放
//     (防一手刷金币 + 防系统性通胀)。输家钱包不足时只 logger.Warn
//     记录 shortfall,游戏内筹码已正确流转向赢家,钱包负债留在用户
//     账上(下次入金或离座时优先结算)。这与金币设计 §4.4 边池模型
//     一致:引擎的栈增/减是权威,钱包只是「投影」,不应反向裁剪。
//
// 与 §132 potReturn 区别:
//   - 狼人杀 §133 EconTier 返彩池(50%/40%/30% 给胜方)
//   - 德扑 §132 当前版本只对赢家扣抽水(无额外 potReturn),后续 v1.3 可叠加
func (m *TexasHoldemManager) SettleHandCoins(roomID string) {
	if m.walletSvc == nil {
		return
	}
	r := m.getRoom(roomID)
	if r == nil || r.State == nil {
		return
	}
	r.mu.Lock()
	// 收集结算数据(锁内快照)
	type settlement struct {
		userID string
		delta  int
	}
	var settlements []settlement
	roomTotalCoin := int64(0)
	for i := 0; i < MaxPlayers; i++ {
		p := &r.State.Players[i]
		if p.UserID == "" {
			continue
		}
		delta := p.Stack - r.HandStartStacks[i]
		roomTotalCoin += int64(p.Stack)
		if delta != 0 {
			settlements = append(settlements, settlement{userID: p.UserID, delta: delta})
		}
	}
	handNum := r.State.HandNumber
	r.mu.Unlock()

	// 2026-08-20 §B7 — MaxPotPerHand 封顶(金币设计 §2.3):
	// 本手底池(= 赢家总进账,筹码守恒下 = 输家总付出)超过上限时,赢家 delta
	// 按比例缩放到上限,超出部分不进钱包(防恶意刷金币)。游戏内筹码不动,
	// 只影响钱包结算金额。这是唯一保留的 cap 机制 —— 单玩家 clamp 已废除
	// (2026-08-21 §20260821-P1-1,见函数级注释)。
	m.mu.RLock()
	maxPot := m.maxPotPerHand
	m.mu.RUnlock()
	if maxPot > 0 {
		pot := 0
		for _, s := range settlements {
			if s.delta > 0 {
				pot += s.delta
			}
		}
		if pot > maxPot {
			scale := float64(maxPot) / float64(pot)
			for i := range settlements {
				if settlements[i].delta > 0 {
					settlements[i].delta = int(float64(settlements[i].delta) * scale)
				}
			}
			logger.L().Warn("texasholdem hand pot exceeds MaxPotPerHand, payout capped",
				zap.String("room_id", roomID),
				zap.Int("hand", handNum),
				zap.Int("pot", pot),
				zap.Int("max_pot_per_hand", maxPot))
		}
	}

	// 按房间总金币计算抽水档位(§133 联动)
	tier := texasherp.ComputeEconTier(roomTotalCoin)
	rakeRate := tier.RakeRate()

	// 锁外异步结算(不阻塞游戏流)
	for _, s := range settlements {
		refID := roomID + ":" + fmt.Sprintf("%d", handNum)
		remark := fmt.Sprintf("texasholdem hand #%d settle", handNum)
		if s.delta > 0 {
			// 赢家:扣抽水后再 Credit
			netPayout, rake := texasherp.ApplyRake(int64(s.delta), tier)
			if netPayout > 0 {
				if err := m.walletSvc.Credit(context.Background(), s.userID, "game_win", "texasholdem_settle", refID, "texasholdem", remark, netPayout); err != nil {
					logger.L().Warn("texasholdem settle credit failed",
						zap.String("room_id", roomID),
						zap.String("user_id", s.userID),
						zap.Int64("delta", netPayout),
						zap.Error(err))
				}
			}
			// 抽水明细
			if rake > 0 {
				if err := m.walletSvc.Debit(context.Background(), s.userID, "game_lose", "texasholdem_rake", refID, "texasholdem",
					fmt.Sprintf("texasholdem rake (tier=%s rate=%.2f%%)", tier, rakeRate*100), rake); err != nil {
					logger.L().Warn("texasholdem rake debit failed",
						zap.String("room_id", roomID),
						zap.String("user_id", s.userID),
						zap.Int64("rake", rake),
						zap.Error(err))
				}
			}
		} else {
			// 输家:按钱包余额 clamp 后扣款(R18 P2 修复,§20260825-P2,
			// 见 debitClamped)。此前全额 Debit 失败时整笔欠账(debt carried),
			// 连续全押的 Bot 债务会滚雪球;现改为有多少扣多少,债务就地吸收。
			// 引擎是筹码权威,钱包是上层投影。
			m.debitClamped(roomID, s.userID, "game_lose", "texasholdem_settle", refID, "texasholdem", remark, int64(-s.delta), s.delta, handNum)
		}
	}
	if len(settlements) > 0 {
		logger.L().Info("texasholdem hand coins settled",
			zap.String("room_id", roomID),
			zap.Int("hand", handNum),
			zap.Int("players", len(settlements)),
			zap.String("econ_tier", string(tier)),
			zap.Float64("rake_rate", rakeRate))
	}
}

// ─────────────────── Spectator API ───────────────────

// SpectateGame 注册 userID 为房间观察者;按需创建房间。不会消耗座位。幂等。
//
// §20260819-02 P0-1 重启恢复修复(对齐狼人杀 BUG-WEREWOLF-P0-7 的 hydrator 模式):
// 此前(7858d33)内存房间缺失时直接返回 ErrRoomNotFound,导致「服务器重启后 DB
// 房间成为孤儿」场景:REST spectate 查 DB 成功、WS spectate 404,前端把错误帧
// 吞进 console,观战者永久卡在「观战中…」。现改为:
//  1. 内存缺失时懒创建房间(与 xiangqi/chess/junqi/doudizhu 行为一致);
//  2. 新建房间通过 seatHydrator(main.go 注入,读 t_lsm_game_player)恢复
//     全部座位(人类 + Agent),恢复后满足条件则自动开首手;
//  3. 返回 created=true 让 WS 层重建 bot 运行时(thpDriver + watchdog)。
//
// 7858d33 修复注释保留备查:凭空创建「空房间」且不恢复座位的旧行为确实会让
// 前端永远 ready=false -- 根因是缺 hydrator,而非懒创建本身。
func (m *TexasHoldemManager) SpectateGame(roomID, userID string) (*TexasHoldemRoom, bool, *errcode.Error) {
	m.mu.Lock()
	r, ok := m.rooms[roomID]
	created := false
	if !ok {
		r = &TexasHoldemRoom{RoomID: roomID}
		m.rooms[roomID] = r
		created = true
		logger.L().Info("texasholdem room created by spectator",
			zap.String("room_id", roomID), zap.String("user_id", userID))
	}
	// §20260819-02:在持有 m.mu 时快照配置与 hydrator(下方 r.mu 路径不再读 m 字段)。
	bigBlind, startStack := m.configForLocked(roomID)
	hydrator := m.seatHydrator
	m.mu.Unlock()

	r.mu.Lock()
	defer r.mu.Unlock()

	// 仅懒创建的空房间做 DB 恢复 -- 已有内存状态的房间绝不重放(幂等护栏,
	// 防止重复 StartHand 把进行中的手牌掀了)。
	if created && hydrator != nil && r.Occupied() == 0 {
		if infos, err := hydrator(roomID); err == nil && len(infos) > 0 {
			m.restoreSeatsLocked(r, infos, bigBlind, startStack)
		} else if err != nil {
			logger.L().Warn("texasholdem seat hydrate failed",
				zap.String("room_id", roomID), zap.Error(err))
		}
	}

	if r.Spectators == nil {
		r.Spectators = make(map[string]struct{})
	}
	r.Spectators[userID] = struct{}{}
	return r, created, nil
}

// restoreSeatsLocked 把 DB 恢复的座位写回内存房间(锁内变体,§92a:调用方
// 必须已持有 r.mu)。人类与 Agent 座位都恢复;Agent 座位同时标记
// BotSeats/BotModels。恢复后若满足开局条件(>=2 人且 PhaseWaiting)则自动
// 开首手 -- 纯 Agent 房间重启后观战者进来即看到活局。
func (m *TexasHoldemManager) restoreSeatsLocked(r *TexasHoldemRoom, infos []SeatRestoreInfo, bigBlind, startStack int) {
	if r.State == nil {
		r.State = NewGame(m.seedFn(), bigBlind)
	}
	for _, info := range infos {
		if info.Seat < 0 || info.Seat >= MaxPlayers || info.UserID == "" {
			continue
		}
		r.Seats[info.Seat] = info.UserID
		if info.ModelKey != "" {
			r.BotSeats[info.Seat] = true
			r.BotModels[info.Seat] = info.ModelKey
		}
		// 引擎侧按指定座位写 Players(AddPlayer 只能顺次找空位,不能指定座位)。
		if p := &r.State.Players[info.Seat]; p.UserID == "" {
			p.UserID = info.UserID
			p.Seat = info.Seat
			p.Stack = startStack
			r.State.NumSeat++
		}
	}
	if r.IsReady() && r.State.Street == PhaseWaiting {
		r.snapshotHandStartStacks()
		if e := r.State.StartHand(); e != nil {
			logger.L().Warn("texasholdem hydrate start hand failed",
				zap.String("room_id", r.RoomID), zap.Error(e))
		} else {
			r.markTurnLocked()
			logger.L().Info("texasholdem hand started (spectator hydrate)",
				zap.String("room_id", r.RoomID),
				zap.Int("hand_number", r.State.HandNumber))
		}
	}
}

// UnspectateGame 取消观察者身份。
func (m *TexasHoldemManager) UnspectateGame(roomID, userID string) *errcode.Error {
	r := m.getRoom(roomID)
	if r == nil {
		return errcode.Code(errcode.ErrRoomNotFound)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.Spectators, userID)
	return nil
}

// SpectatorList 返回当前观察者 userID。
func (m *TexasHoldemManager) SpectatorList(roomID string) []string {
	r := m.getRoom(roomID)
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.Spectators))
	for uid := range r.Spectators {
		out = append(out, uid)
	}
	return out
}

// SpectatorState 返回观察者可见的客户端视图。任何玩家的 Hole 都不填充，
// 仅留下 hole_count。摊牌阶段（亮底牌比大小）结束后隐藏信息才公开。
func (m *TexasHoldemManager) SpectatorState(roomID, userID string) (*ClientGameState, *errcode.Error) {
	r := m.getRoom(roomID)
	if r == nil {
		return nil, errcode.Code(errcode.ErrRoomNotFound)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return BuildClientStateWithRoom(roomID, r.Seats, r.BotSeats, r.BotModels, -1, r.State, r.BotHeartThought, r.BotThinking, r.BotLastChat), nil
}

// SpectatorView 同 SpectatorState 但省去 userID 参数；所有观察者共享同一
// 张重新构建的视图，构建一次即可向多个观察者广播。
func (m *TexasHoldemManager) SpectatorView(roomID string) *ClientGameState {
	r := m.getRoom(roomID)
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return BuildClientStateWithRoom(roomID, r.Seats, r.BotSeats, r.BotModels, -1, r.State, r.BotHeartThought, r.BotThinking, r.BotLastChat)
}
