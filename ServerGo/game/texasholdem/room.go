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
	onHandStarted func(roomID string)

	// 2026-08-19 §德州扑克金币 — 钱包服务(由 main.go 注入)。
	// 手牌结束后按筹码盈亏结算金币。
	walletSvc WalletSettler

	// 2026-08-19 §德州扑克盲注透传 — 房间级盲注/买入配置。
	// 由 RoomService 经 SetTexasHoldemRoomConfigurer 回调在房间创建成功后、
	// 首次 JoinGame 之前写入;缺省房间回退 m.BigBlind / m.StartStack。
	roomConfigs map[string]texasRoomConfig
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
}

// NewTexasHoldemManager 创建空管理器。
func NewTexasHoldemManager() *TexasHoldemManager {
	return &TexasHoldemManager{
		rooms:       make(map[string]*TexasHoldemRoom),
		roomConfigs: make(map[string]texasRoomConfig),
		seedFn:      func() int64 { return time.Now().UnixNano() },
		BigBlind:    200,
		StartStack:  10000,
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

// SetWalletService 注入钱包服务(§德州扑克金币)。由 main.go 调用。
func (m *TexasHoldemManager) SetWalletService(svc WalletSettler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.walletSvc = svc
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

// JoinGame 让 userID 入座。幂等。
// 达到 2 人时自动发牌进入翻前阶段。
func (m *TexasHoldemManager) JoinGame(roomID, userID string) (*TexasHoldemRoom, bool, *errcode.Error) {
	m.mu.Lock()
	r, ok := m.rooms[roomID]
	if !ok {
		r = &TexasHoldemRoom{RoomID: roomID}
		m.rooms[roomID] = r
		logger.L().Info("texasholdem room created", zap.String("room_id", roomID), zap.String("by", userID))
	}
	// 2026-08-19 §德州扑克盲注透传: 在持有 m.mu 时快照房间级盲注/买入
	// (下方 NewGame/AddPlayer 在 m.mu 释放后执行,不能再读 m.roomConfigs)。
	bigBlind, startStack := m.configForLocked(roomID)
	m.mu.Unlock()

	r.mu.Lock()
	defer r.mu.Unlock()

	// 幂等：已入座直接返回
	if _, seated := r.SeatOf(userID); seated {
		return r, false, nil
	}

	// 找空位
	seat := -1
	for i, u := range r.Seats {
		if u == "" {
			seat = i
			break
		}
	}
	if seat == -1 {
		return nil, false, errcode.Code(errcode.ErrRoomFull)
	}
	r.Seats[seat] = userID

	started := false
	if r.State == nil {
		r.State = NewGame(m.seedFn(), bigBlind)
	}
	if _, e := r.State.AddPlayer(userID, startStack); e != nil {
		// 不应发生（刚找到空位）
		logger.L().Warn("texasholdem add player failed", zap.String("room_id", roomID), zap.Error(e))
	}

	// 满 2 人且当前阶段为 waiting，自动开首手
	if r.IsReady() && r.State.Street == PhaseWaiting {
		// 2026-08-19 §德州扑克金币: 记录各座位手牌开始时的筹码
		r.snapshotHandStartStacks()
		if e := r.State.StartHand(); e != nil {
			logger.L().Warn("texasholdem start hand failed", zap.String("room_id", roomID), zap.Error(e))
		} else {
			started = true
			logger.L().Info("texasholdem hand started",
				zap.String("room_id", roomID),
				zap.Int("hand_number", r.State.HandNumber))
		}
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
	defer r.mu.Unlock()

	if r.State == nil {
		return nil, false, errcode.Code(errcode.ErrGameNotStarted)
	}
	seat, ok := r.State.GetSeat(userID)
	if !ok {
		return nil, false, errcode.Code(errcode.ErrRoomNotIn)
	}

	// 2026-08-19 §德州扑克Agent: bot 座位的行动由 driver 自动驱动,
	// 人类玩家在 bot 座位上提交动作直接拒绝(防止作弊)。
	if r.BotSeats[seat] {
		return nil, false, errcode.CodeMsg(errcode.ErrInvalidMove, "seat is bot-controlled")
	}

	handOver, e := r.State.ApplyAction(seat, a)
	if e != nil {
		return nil, false, e
	}

	if handOver {
		// 2026-08-19 §德州扑克金币: 手牌结束,异步结算金币
		go m.SettleHandCoins(roomID)

		// 延迟开新一手（给客户端 time 展示结果）
		if r.State.CanStartHand() {
			go func() {
				time.Sleep(5 * time.Second)
				r.mu.Lock()
				defer r.mu.Unlock()
				if r.State.Street == PhaseOver || r.State.Street == PhaseShowdown {
					// 2026-08-19 §德州扑克金币: 新手牌开始前记录筹码快照
					r.snapshotHandStartStacks()
					if e := r.State.StartHand(); e != nil {
						logger.L().Warn("auto start next hand failed", zap.String("room_id", roomID), zap.Error(e))
					} else {
						logger.L().Info("texasholdem next hand", zap.String("room_id", roomID), zap.Int("hand", r.State.HandNumber))
						m.broadcastState(roomID, r)
						// 2026-08-19 §德州扑克Agent: 新手牌开始,通知 WS 层广播 + 触发 bot
						if m.onHandStarted != nil {
							m.onHandStarted(roomID)
						}
					}
				}
			}()
		}
	}

	return r, handOver, nil
}

// broadcastState 广播房间状态（供异步开新手时调用）。
func (m *TexasHoldemManager) broadcastState(roomID string, r *TexasHoldemRoom) {
	// 此方法只在已持有房间锁时调用，外层负责 hub 广播。
	// 返回 rooms 快照供调用方获取座位信息。
}

// Resign 认输（等同弃牌）。
func (m *TexasHoldemManager) Resign(roomID, userID string) (*TexasHoldemRoom, *errcode.Error) {
	r := m.getRoom(roomID)
	if r == nil {
		return nil, errcode.Code(errcode.ErrRoomNotFound)
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State == nil || r.State.Status != StatusPlaying {
		return nil, errcode.Code(errcode.ErrGameAlreadyOver)
	}
	seat, ok := r.State.GetSeat(userID)
	if !ok {
		return nil, errcode.Code(errcode.ErrRoomNotIn)
	}

	// 2026-08-19 §德州扑克Agent: bot 座位不接受人类 resign(§92a 锁内守卫,
	// 防止作弊绕过 BotDriver.ApplyBotAction)。
	if r.BotSeats[seat] {
		return nil, errcode.CodeMsg(errcode.ErrInvalidMove, "seat is bot-controlled")
	}

	if !r.State.Players[seat].Folded {
		r.State.Players[seat].Folded = true
		// 检查是否只剩 1 人
		if r.State.activePlayers() <= 1 {
			r.State.endHandFold()
		}
	}
	return r, nil
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
	return BuildClientStateWithRoom(roomID, r.Seats, r.BotSeats, r.BotModels, seat, r.State, r.BotHeartThought, r.BotThinking), nil
}

// StateForSeat 在已持有房间引用时构造指定座位视图。
func (m *TexasHoldemManager) StateForSeat(roomID string, seat int) *ClientGameState {
	r := m.getRoom(roomID)
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return BuildClientStateWithRoom(roomID, r.Seats, r.BotSeats, r.BotModels, seat, r.State, r.BotHeartThought, r.BotThinking)
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
		MyHole:       [2]int{cardToInt(p.Hole[0]), cardToInt(p.Hole[1])},
		Community:    community,
		CommunityLen: gs.CommunityShown,
		MyStack:      p.Stack,
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
	MyHole        [2]int
	Community     [5]int
	CommunityLen  int
	MyStack       int
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
	defer r.mu.Unlock()

	if r.State == nil {
		return false, errcode.Code(errcode.ErrGameNotStarted)
	}
	if seat != r.State.Turn {
		return false, errcode.Code(errcode.ErrNotYourTurn)
	}
	if !r.BotSeats[seat] {
		return false, errcode.CodeMsg(errcode.ErrInvalidMove, "not a bot seat")
	}

	handOver, e := r.State.ApplyAction(seat, a)
	if e != nil {
		return false, e
	}

	if handOver {
		// 2026-08-19 §德州扑克金币: 手牌结束,异步结算金币
		go m.SettleHandCoins(roomID)

		// 延迟开新一手(与 Action 方法相同的逻辑)
		if r.State.CanStartHand() {
			go func() {
				time.Sleep(5 * time.Second)
				r.mu.Lock()
				defer r.mu.Unlock()
				if r.State.Street == PhaseOver || r.State.Street == PhaseShowdown {
					// 2026-08-19 §德州扑克金币: 新手牌开始前记录筹码快照
					r.snapshotHandStartStacks()
					if e := r.State.StartHand(); e != nil {
						logger.L().Warn("auto start next hand failed (bot)", zap.String("room_id", roomID), zap.Error(e))
					} else {
						logger.L().Info("texasholdem next hand (bot)", zap.String("room_id", roomID), zap.Int("hand", r.State.HandNumber))
						m.broadcastState(roomID, r)
						if m.onHandStarted != nil {
							m.onHandStarted(roomID)
						}
					}
				}
			}()
		}
	}

	return handOver, nil
}

// cardToInt 把 Card 转为 thpagent 使用的 int 编码 (rank*4+suit+1)。
// rank: 0..12 (2..A), suit: 0..3 (Spade/Heart/Club/Diamond)
// 返回 1..52; 零值 Card 返回 0。
func cardToInt(c Card) int {
	if c.Rank == 0 && c.Suit == 0 {
		return 0
	}
	return c.Rank*4 + c.Suit + 1
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
// 结算规则(v1.1,2026-08-19 §德州扑克金币 §132 §133):
//   - delta = 当前筹码 - 手牌开始筹码
//   - delta > 0 → Credit 赢得金币,但先按 EconTier 抽水(赢家份额 = delta - rake)
//   - delta < 0 → Debit 损失金币(输家不抽水)
//   - 房间总金币 = Σ存活玩家金币,按 ComputeEconTier 计算档位
//   - 抽水明细写 t_lsm_game_wallet_log(reason="texasholdem_rake")
//   - 人类 + bot 都结算(bot 关联 model 用户的金币账户)
//
// 与 §132 potReturn 区别:
//   - 狼人杀 §133 EconTier 返彩池(50%/40%/30% 给胜方)
//   - 德扑 §132 当前版本只对赢家扣抽水(无额外 potReturn),后续 v1.2 可叠加
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
			amount := int64(-s.delta)
			if err := m.walletSvc.Debit(context.Background(), s.userID, "game_lose", "texasholdem_settle", refID, "texasholdem", remark, amount); err != nil {
				logger.L().Warn("texasholdem settle debit failed",
					zap.String("room_id", roomID),
					zap.String("user_id", s.userID),
					zap.Int("delta", s.delta),
					zap.Error(err))
			}
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

// SpectateGame 注册 userID 为房间观察者；按需创建房间。不会消耗座位。幂等。
func (m *TexasHoldemManager) SpectateGame(roomID, userID string) (*TexasHoldemRoom, *errcode.Error) {
	m.mu.Lock()
	r, ok := m.rooms[roomID]
	if !ok {
		r = &TexasHoldemRoom{RoomID: roomID}
		m.rooms[roomID] = r
		logger.L().Info("texasholdem room created by spectator",
			zap.String("room_id", roomID), zap.String("user_id", userID))
	}
	m.mu.Unlock()

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Spectators == nil {
		r.Spectators = make(map[string]struct{})
	}
	r.Spectators[userID] = struct{}{}
	return r, nil
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
	return BuildClientStateWithRoom(roomID, r.Seats, r.BotSeats, r.BotModels, -1, r.State, r.BotHeartThought, r.BotThinking), nil
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
	return BuildClientStateWithRoom(roomID, r.Seats, r.BotSeats, r.BotModels, -1, r.State, r.BotHeartThought, r.BotThinking)
}
