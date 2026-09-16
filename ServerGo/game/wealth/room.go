// Package wealth — room.go: WealthRoom(锁 + 月度 tick + 座位 + 广播回调)
// 2026-09-14 §财商流P0。
//
// 契约: 后端架构文档 §14(月度 tick 时序)。锁纪律 §92a:所有广播回调在
// **释放 r.mu 之后**调用(texasholdem §B6 同款「持锁结算 → 锁外回调」);
// BuildClientState 走纯函数快照,不回调房间方法。
package wealth

import (
	"context"
	"math/rand"
	"sync"
	"time"

	"LsmAgentGame/agent/wealthplayer"
	"LsmAgentGame/errcode"
	"LsmAgentGame/game/wealth/profession"
	"LsmAgentGame/logger"
	"LsmAgentGame/service"

	"go.uber.org/zap"
)

// BotTranscript 单座位 Agent 思维可见性(game.state.bot_contexts)。
type BotTranscript struct {
	LastDecisionSummary string
	LastToolInput       string
	LastToolResult      string
	HeartThought        string
}

// ChatSender bot 公屏发言通道(ws.ChatService 适配器注入;nil-safe)。
type ChatSender interface {
	SendFromBot(roomID, botUserID, botAccount, modelKey, text string) error
}

// BroadcastHooks ws 层广播钩子(全部在锁外调用)。
type BroadcastHooks struct {
	OnEvent   func(roomID string, ev EventRecord)         // game.event
	OnMonth   func(roomID string, res *SettleResult)      // game.month(BroadcastRoom)
	OnState   func(roomID string)                         // game.state 逐座位单发
	OnOver    func(roomID string, scores []FinalScore)    // game.over
	OnStarted func(roomID string, payload map[string]any) // game.started
	OnRemoved func(roomID string)                         // game.removed(终局 60s 后)
}

// SeatProfession 开局职业公开对(game.started.professions)。
type SeatProfession struct {
	Seat         int    `json:"seat"`
	ProfessionID string `json:"profession_id"`
}

// WealthRoom 单房间运行时。
type WealthRoom struct {
	mu     sync.Mutex
	RoomID string

	OwnerID string

	Seats          [MaxSeats]string
	SeatModelKeys  [MaxSeats]string
	BotSeats       [MaxSeats]bool
	SeatProfession [MaxSeats]string // 座位职业偏好(agent_seats[].profession)
	Nicknames      [MaxSeats]string
	Spectators     map[string]struct{}
	IdleSeats      map[int]bool // 人类中途离房 → 自动挂机(P0;bot 接管为 P1)

	World *World

	Status string // open | playing | over
	Phase  string // acting | settling

	MonthMs     int
	NextMonthAt time.Time
	Paused      bool

	Transcripts [MaxSeats]BotTranscript

	// 卡池与随机源(房间级;开局抽卡用)。
	pool      string // curated | docs
	seed      int64
	rng       *rand.Rand
	docLoader *profession.Loader // pool="docs" 时由 Manager 注入

	hooks       BroadcastHooks
	chatSender  ChatSender
	agents      map[int]*wealthplayer.Agent
	agentSem    chan struct{} // 房间级 LLM 并发信号量(默认 DefaultAgentConcurrency=8)
	eventsSent  int           // World.Events 已下发条数
	cardPool    []profession.Card
	cardPoolIdx int

	gameStartedAt int64 // 开局 unix s(view 下发 game_started_at 字段)

	done     chan struct{}
	settleCh chan struct{}
	closed   bool
}

// NewWealthRoom 构造空房间(不启动 loop;Start 后进入 playing)。
func NewWealthRoom(roomID string, monthMs int, pool string, seed int64, llmConcurrency int) *WealthRoom {
	if monthMs < 3000 {
		monthMs = 3000
	}
	if monthMs > 30000 {
		monthMs = 30000
	}
	if pool != "docs" {
		pool = "curated"
	}
	if llmConcurrency <= 0 {
		// 2026-09-16 §12 座扩容:默认 4 → DefaultAgentConcurrency(8),让 10+ bot
		// 同月的并发决策不再被 4 路信号量压成串行(详见 engine.go 常量注释)。
		llmConcurrency = DefaultAgentConcurrency
	}
	seedVal := seed
	if seedVal == 0 {
		seedVal = time.Now().UnixNano()
	}
	return &WealthRoom{
		RoomID:     roomID,
		Status:     StatusOpen,
		Phase:      PhaseActing,
		MonthMs:    monthMs,
		pool:       pool,
		seed:       seedVal,
		rng:        rand.New(rand.NewSource(seedVal)),
		Spectators: map[string]struct{}{},
		IdleSeats:  map[int]bool{},
		agents:     map[int]*wealthplayer.Agent{},
		agentSem:   make(chan struct{}, llmConcurrency),
		done:       make(chan struct{}),
		settleCh:   make(chan struct{}, 1),
	}
}

// SetHooks 注入 ws 广播钩子(房间创建后、Start 前调用一次)。
func (r *WealthRoom) SetHooks(h BroadcastHooks) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hooks = h
}

// SetChatSender 注入聊天通道(bot speak 走 SendFromBot)。
func (r *WealthRoom) SetChatSender(cs ChatSender) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.chatSender = cs
}

// applyOpts 应用房间级 WealthRoomOptions(由 manager.ApplyRoomOptions 调用)。
func (r *WealthRoom) applyOpts(opts *service.WealthRoomOptions) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if opts.MonthMs > 0 {
		r.MonthMs = clampInt(opts.MonthMs, 3000, 30000)
	}
	if opts.Pool == "curated" || opts.Pool == "docs" {
		r.pool = opts.Pool
	}
	if opts.Seed != 0 {
		r.seed = opts.Seed
	}
}

// SetOwner 记录房主(game.wealth_start/pause 权限,35011)。
// 首位人类入座者即创建者(CreateRoomWithAgents → SyncSeat 顺序保证)。
func (r *WealthRoom) SetOwner(userID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.OwnerID == "" {
		r.OwnerID = userID
	}
}

// SeatOf 返回 userID 所在座位。
func (r *WealthRoom) SeatOf(userID string) (int, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, u := range r.Seats {
		if u == userID && u != "" {
			return i, true
		}
	}
	return -1, false
}

// Occupied 已入座数。
func (r *WealthRoom) Occupied() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.occupiedLocked()
}

func (r *WealthRoom) occupiedLocked() int {
	n := 0
	for _, u := range r.Seats {
		if u != "" {
			n++
		}
	}
	return n
}

// JoinGame 人类入座(first-empty,跳过已标记 bot 座)。幂等。
// 返回 (座位, 是否因此满员触发自动开局)。
func (r *WealthRoom) JoinGame(userID, nickname string) (int, bool, *errcode.Error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Status == StatusOver {
		return -1, false, errcode.Code(errcode.ErrWealthNotPlaying)
	}
	// 幂等。
	for i, u := range r.Seats {
		if u == userID {
			return i, false, nil
		}
	}
	seat := -1
	for i, u := range r.Seats {
		if u == "" && !r.BotSeats[i] {
			seat = i
			break
		}
	}
	if seat == -1 {
		return -1, false, errcode.Code(errcode.ErrRoomFull)
	}
	r.Seats[seat] = userID
	r.Nicknames[seat] = nickname
	if r.OwnerID == "" {
		r.OwnerID = userID
	}
	if r.World != nil {
		// 中途加入:发卡 + 本月不参与(B7:当月不补结,下月正式参与)。
		card := r.drawCardLocked()
		p := newPlayerFromCard(seat, card)
		p.Submitted = true
		p.ActionBudget = 0
		r.World.Players[seat] = p
	}
	full := r.occupiedLocked() >= MaxSeats && r.Status == StatusOpen
	return seat, full, nil
}

// RegisterBotSeats 标记并入住 bot 座位(建房时调用,先于人类 JoinGame)。
// 2026-09-14 §财商流P0-bugfix: 旧版只写 BotSeats/SeatModelKeys,不写
// Seats[seat] 的 bot userID,导致 Start 发卡跳过 bot 座位、EnsureAgents 因
// Seats[seat]=="" 跳过 → bot 永不上场。现扩展为同时入住 bot userID
// (seatUsers,仅写空位,不覆盖已有人类座位)。
func (r *WealthRoom) RegisterBotSeats(seatUsers map[int]string, seatModels map[int]string, professions map[int]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for seat, userID := range seatUsers {
		if seat < 0 || seat >= MaxSeats || userID == "" {
			continue
		}
		if r.Seats[seat] == "" {
			r.Seats[seat] = userID
		}
		r.BotSeats[seat] = true
		if r.Nicknames[seat] == "" {
			if mk := seatModels[seat]; mk != "" {
				r.Nicknames[seat] = "AI·" + mk
			} else {
				r.Nicknames[seat] = "AI 玩家"
			}
		}
	}
	for seat, modelKey := range seatModels {
		if seat < 0 || seat >= MaxSeats || modelKey == "" {
			continue
		}
		r.BotSeats[seat] = true
		r.SeatModelKeys[seat] = modelKey
		if prof := professions[seat]; prof != "" {
			r.SeatProfession[seat] = prof
		}
	}
}

// drawCardLocked 从房间卡池抽下一张(池尽循环 curated;确定性:房间 rng)。
func (r *WealthRoom) drawCardLocked() profession.Card {
	if len(r.cardPool) == 0 || r.cardPoolIdx >= len(r.cardPool) {
		r.cardPool = r.buildCardPoolLocked()
		r.cardPoolIdx = 0
	}
	card := r.cardPool[r.cardPoolIdx]
	r.cardPoolIdx++
	return card
}

// buildCardPoolLocked 按房间 pool 配置构建洗牌后的卡池。
func (r *WealthRoom) buildCardPoolLocked() []profession.Card {
	if r.pool == "docs" && r.docLoader != nil {
		if cards := r.docLoader.Draw(MaxSeats, r.rng); len(cards) > 0 {
			return cards
		}
	}
	cards := profession.CuratedCards()
	r.rng.Shuffle(len(cards), func(i, j int) { cards[i], cards[j] = cards[j], cards[i] })
	return cards
}

// Start 开局:发卡 → 初始注入 → 广播 → 进入月份 1。返回错误(人数不足等)。
// caller: manager(锁外;内部自行持锁)。
func (r *WealthRoom) Start(loader *profession.Loader) *errcode.Error {
	r.mu.Lock()
	if r.Status != StatusOpen {
		r.mu.Unlock()
		return errcode.Code(errcode.ErrWealthNotPlaying)
	}
	if n := r.occupiedLocked(); n < MinSeats {
		r.mu.Unlock()
		return errcode.Code(errcode.ErrWealthNotEnoughPlayers)
	}

	// 发卡:座位序抽取;有职业偏好的座位优先匹配卡 id。
	var cards [MaxSeats]profession.Card
	byID := map[string]profession.Card{}
	for _, c := range profession.CuratedCards() {
		byID[c.ID] = c
	}
	for seat := 0; seat < MaxSeats; seat++ {
		if r.Seats[seat] == "" {
			continue
		}
		if pref := r.SeatProfession[seat]; pref != "" {
			if c, ok := byID[pref]; ok && r.pool != "docs" {
				cards[seat] = c
				continue
			}
		}
		cards[seat] = r.drawCardLocked()
	}

	seed := r.seed
	r.World = NewWorld(seed, cards)
	r.World.StartGame()
	r.Status = StatusPlaying
	r.Phase = PhaseActing
	r.gameStartedAt = time.Now().Unix()
	r.MonthMs = clampInt(r.MonthMs, 3000, 30000)
	r.NextMonthAt = time.Now().Add(time.Duration(r.MonthMs) * time.Millisecond)
	r.resetMonthFlagsLocked()

	professions := make([]SeatProfession, 0, MaxSeats)
	age := r.World.Age()
	for seat, p := range r.World.Players {
		if p == nil {
			continue
		}
		professions = append(professions, SeatProfession{Seat: seat, ProfessionID: p.Card.ID})
	}
	startedPayload := map[string]any{
		"room_id": r.RoomID, "game_kind": "wealth",
		"month": 1, "age": age, "start_age": age,
		"professions": professions,
	}
	hooks := r.hooks
	r.mu.Unlock()

	if hooks.OnStarted != nil {
		hooks.OnStarted(r.RoomID, startedPayload)
	}
	if hooks.OnState != nil {
		hooks.OnState(r.RoomID)
	}
	r.wakeBots()
	logger.L().Info("wealth game started",
		zap.String("room_id", r.RoomID), zap.Int("seats", len(professions)))
	return nil
}

// resetMonthFlagsLocked 新月 acting 开始:重置预算/发言/提交标记(锁内,§92a)。
func (r *WealthRoom) resetMonthFlagsLocked() {
	if r.World == nil {
		return
	}
	for _, p := range r.World.Players {
		if p == nil {
			continue
		}
		p.ActionBudget = monthlyActionBudget
		p.SpokenThisMonth = false
		p.Submitted = false
		p.LastActionText = ""
		p.StatusIcon = "idle"
	}
}

// RunLoop 月度主循环(单 goroutine;禁止锁内回调)。
func (r *WealthRoom) RunLoop(onFinish func(roomID string)) {
	for {
		r.mu.Lock()
		if r.closed {
			r.mu.Unlock()
			return
		}
		var wait time.Duration
		if r.Status == StatusPlaying && r.Phase == PhaseActing {
			if r.Paused {
				// 暂停:冻结窗口(顺延)。
				r.NextMonthAt = time.Now().Add(time.Duration(r.MonthMs) * time.Millisecond)
			}
			wait = time.Until(r.NextMonthAt)
		} else {
			wait = time.Hour // over / settling(由 settleCh 推进)
			if r.Status == StatusOver {
				wait = time.Hour
			}
		}
		r.mu.Unlock()
		if wait < 0 {
			wait = 0
		}

		select {
		case <-r.done:
			return
		case <-r.settleCh:
		case <-time.After(wait):
		}

		r.trySettle(onFinish)
	}
}

// trySettle 窗口结束(或全员提交)→ 月结。返回是否推进了一月。
func (r *WealthRoom) trySettle(onFinish func(roomID string)) bool {
	r.mu.Lock()
	if r.closed || r.Status != StatusPlaying || r.Phase != PhaseActing {
		r.mu.Unlock()
		return false
	}
	if r.Paused || time.Now().Before(r.NextMonthAt) {
		// 提前信号(全员提交)在暂停时忽略;未到窗口不看。
		if allSubmitted := r.allSubmittedLocked(); !(allSubmitted && !r.Paused) {
			r.mu.Unlock()
			return false
		}
	}

	// settling:停止接收动作。
	r.Phase = PhaseSettling
	for _, p := range r.World.Players {
		if p != nil && !p.Submitted {
			p.Submitted = true // 窗口强制结束(watchdog 兜底语义)
		}
	}
	prevEvents := len(r.World.Events)
	finished, res := r.World.SettleMonth()
	newEvents := append([]EventRecord(nil), r.World.Events[prevEvents:]...)
	hooks := r.hooks
	r.eventsSent = len(r.World.Events)

	var scores []FinalScore
	over := false
	if finished {
		r.Status = StatusOver
		scores = r.World.FinalScores()
		over = true
	} else {
		r.Phase = PhaseActing
		r.NextMonthAt = time.Now().Add(time.Duration(r.MonthMs) * time.Millisecond)
		r.resetMonthFlagsLocked()
	}
	roomID := r.RoomID
	r.mu.Unlock()

	// ── 锁外回调(§92a) ──
	if hooks.OnEvent != nil {
		for _, ev := range newEvents {
			hooks.OnEvent(roomID, ev)
		}
	}
	if hooks.OnMonth != nil {
		hooks.OnMonth(roomID, res)
	}
	if over {
		if hooks.OnOver != nil {
			hooks.OnOver(roomID, scores)
		}
		if onFinish != nil {
			onFinish(roomID)
		}
		return true
	}
	if hooks.OnState != nil {
		hooks.OnState(roomID)
	}
	r.wakeBots()
	return true
}

// allSubmittedLocked 全员已提交(空座不计)。
func (r *WealthRoom) allSubmittedLocked() bool {
	if r.World == nil {
		return false
	}
	n, submitted := 0, 0
	for _, p := range r.World.Players {
		if p == nil {
			continue
		}
		n++
		if p.Submitted {
			submitted++
		}
	}
	return n > 0 && n == submitted
}

// SubmitMonth 座位提交本月(submit_month;不耗预算;全员提交 → 提前月结)。
func (r *WealthRoom) SubmitMonth(seat int) *errcode.Error {
	r.mu.Lock()
	if r.Status != StatusPlaying {
		r.mu.Unlock()
		return errcode.Code(errcode.ErrWealthNotPlaying)
	}
	if r.Phase != PhaseActing {
		r.mu.Unlock()
		return errcode.Code(errcode.ErrWealthWrongPhase)
	}
	p := r.World.Players[seat]
	if p == nil || !p.Alive {
		r.mu.Unlock()
		return errcode.Code(errcode.ErrWealthPlayerInactive)
	}
	if p.Submitted {
		r.mu.Unlock()
		return nil // 幂等
	}
	p.Submitted = true
	all := r.allSubmittedLocked()
	r.mu.Unlock()

	if all {
		select {
		case r.settleCh <- struct{}{}:
		default:
		}
	}
	return nil
}

// NotifySubmitted 人类/Agent submit 后由 ws 层触发一次状态刷新。
func (r *WealthRoom) NotifySubmitted() {
	r.mu.Lock()
	hooks := r.hooks
	r.mu.Unlock()
	if hooks.OnState != nil {
		hooks.OnState(r.RoomID)
	}
}

// MarkIdle 人类中途离房:P0 座位自动挂机(每月自动 submit_month,不接 LLM);
// bot 接管为 P1(见 ws/game_service_wealth.go 注释)。
func (r *WealthRoom) MarkIdle(userID string) {
	r.mu.Lock()
	seat, ok := -1, false
	for i, u := range r.Seats {
		if u == userID {
			seat, ok = i, true
			break
		}
	}
	if ok && !r.BotSeats[seat] {
		r.IdleSeats[seat] = true
	}
	r.mu.Unlock()
}

// Pause 房主暂停/恢复(月结边界生效;B8)。
func (r *WealthRoom) Pause(userID string, pause bool) *errcode.Error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.OwnerID == "" || userID != r.OwnerID {
		return errcode.Code(errcode.ErrWealthNotOwner)
	}
	if r.Status != StatusPlaying {
		return errcode.Code(errcode.ErrWealthNotPlaying)
	}
	r.Paused = pause
	if !pause {
		r.NextMonthAt = time.Now().Add(time.Duration(r.MonthMs) * time.Millisecond)
	}
	logger.L().Info("wealth room pause toggled",
		zap.String("room_id", r.RoomID), zap.Bool("paused", pause))
	return nil
}

// Close 停止 loop(房间删除/终局清理)。
func (r *WealthRoom) Close() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	close(r.done)
	r.mu.Unlock()
}

// SpecSnapshot 供 ws 层构造 game.joined 的轻量快照。
type SpecSnapshot struct {
	Status string
	Phase  string
	Month  int
}

// SnapshotStatus 返回状态快照。
func (r *WealthRoom) SnapshotStatus() SpecSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := SpecSnapshot{Status: r.Status, Phase: r.Phase, Month: 1}
	if r.World != nil {
		s.Month = r.World.Month
	}
	return s
}

// GetStatus 房间状态(锁内)。
func (r *WealthRoom) GetStatus() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Status
}

// GetPhase 阶段。
func (r *WealthRoom) GetPhase() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Phase
}

// Engine 返回引擎快照指针(锁内;调用方不得修改指针指向结构)。
func (r *WealthRoom) Engine() *World {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.World
}

// EngineLocked 返回引擎指针(不加锁;调用方必须已持 MuLock —— §92a ws 层
// apply 路径专用)。2026-09-14 §财商流P0-bugfix: 修复「持锁后调 Engine()
// 二次加锁自死锁」;wealth 包内部持锁路径同理应直接读 r.World 字段。
func (r *WealthRoom) EngineLocked() *World {
	return r.World
}

// MuLock/MuUnlock 是 ws 层 SyncSeat/apply 路径专用(§92a,锁外不允许)。
// 其他路径优先用短方法。
func (r *WealthRoom) MuLock()   { r.mu.Lock() }
func (r *WealthRoom) MuUnlock() { r.mu.Unlock() }

// SeedView 返回房间种子(锁内读)。ws 层用于 PlaceholderWorld。
func (r *WealthRoom) SeedView() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.seed
}

// SubmitMonthLocked 提交单座位(锁内,§92a)。
// 别名保持:SubmitMonthLocked = SubmitMonthLocked(同上)。

// Month 返回当前月份。
func (r *WealthRoom) Month() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.World == nil {
		return 0
	}
	return r.World.Month
}

// Age 返回主时钟年龄。
func (r *WealthRoom) Age() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.World == nil {
		return 25
	}
	return r.World.Age()
}

// SignalSettle 触发提前月结信号。
func (r *WealthRoom) SignalSettle() {
	select {
	case r.settleCh <- struct{}{}:
	default:
	}
}

// AllSubmittedLocked 全员已提交(锁内调用,§92a)。
func (r *WealthRoom) AllSubmittedLocked() bool {
	return r.allSubmittedLocked()
}

// SubmitMonthLocked 提交单座位(锁内,§92a)。
func (r *WealthRoom) SubmitMonthLocked(seat int) *errcode.Error {
	if r.Status != StatusPlaying {
		return errcode.Code(errcode.ErrWealthNotPlaying)
	}
	if r.Phase != PhaseActing {
		return errcode.Code(errcode.ErrWealthWrongPhase)
	}
	p := r.World.Players[seat]
	if p == nil || !p.Alive {
		return errcode.Code(errcode.ErrWealthPlayerInactive)
	}
	p.Submitted = true
	return nil
}

// EnqueueEventForUI 把事件加入 World.Events(供 ws 层即时广播)。
func (r *WealthRoom) EnqueueEventForUI(ev EventRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.World == nil {
		return
	}
	r.World.Events = append(r.World.Events, ev)
}

// SnapshotSeats/Nicknames/BotSeats/ModelKeys/Transcripts 锁内取快照。
func (r *WealthRoom) SnapshotSeats() [MaxSeats]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Seats
}
func (r *WealthRoom) SnapshotNicknames() [MaxSeats]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Nicknames
}
func (r *WealthRoom) SnapshotBotSeats() [MaxSeats]bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.BotSeats
}
func (r *WealthRoom) SnapshotModelKeys() [MaxSeats]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.SeatModelKeys
}
func (r *WealthRoom) SnapshotTranscripts() [MaxSeats]BotTranscript {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Transcripts
}

// GameStartedAtUnix 开局 unix 时间戳(开 Start 时冻结;供 view next_month_at 等)。
func (r *WealthRoom) GameStartedAtUnix() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.gameStartedAt
}

// NextMonthAtUnix 当前月窗口结束 unix ms(若未 playing,返回 0)。
func (r *WealthRoom) NextMonthAtUnix() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Status != StatusPlaying {
		return 0
	}
	return r.NextMonthAt.UnixMilli()
}

// SetChatSender 注入聊天通道(由 ws 层装配时调用)。
func (r *WealthRoom) ChatSender() ChatSender {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.chatSender
}

// clampInt 整数夹取。
func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// wakeBots 唤醒全部 bot 座位决策(由 Start()/每轮 settle 后调用,锁外路径)。
// 房间级并发信号量(默认 4;NewWealthRoom 已设)。
func (r *WealthRoom) wakeBots() {
	r.mu.Lock()
	if r.closed || r.Status != StatusPlaying || r.Paused {
		r.mu.Unlock()
		return
	}
	agents := make(map[int]*wealthplayer.Agent, len(r.agents))
	for s, a := range r.agents {
		agents[s] = a
	}
	r.mu.Unlock()
	for seat, agent := range agents {
		go r.runOneBot(seat, agent)
	}
}

func (r *WealthRoom) runOneBot(seat int, agent *wealthplayer.Agent) {
	select {
	case r.agentSem <- struct{}{}:
		defer func() { <-r.agentSem }()
	default:
		// 满槽:稍后再 wake,避免抢锁。
		go func() {
			r.agentSem <- struct{}{}
			defer func() { <-r.agentSem }()
			r.runAgentCtx(seat, agent)
		}()
		return
	}
	r.runAgentCtx(seat, agent)
}

func (r *WealthRoom) runAgentCtx(seat int, agent *wealthplayer.Agent) {
	ctx, ok := BuildContextForAgent(r, seat)
	if !ok || ctx == nil {
		return
	}
	agent.OnMonthStart(context.Background(), ctx)
}
