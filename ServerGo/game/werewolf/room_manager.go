package werewolf

import (
	"context"
	"fmt"
	"sync"
	"time"
	"LsmWebGame/agent/wwplayer"
	"LsmWebGame/agent/core"
	"LsmWebGame/config"
	"LsmWebGame/llm"
	"LsmWebGame/logger"
	"LsmWebGame/service"
	"go.uber.org/zap"
)

type WerewolfManager struct {
	mu       sync.RWMutex
	rooms    map[string]*WerewolfRoom
	seedFn   func() int64
	registry *llm.Registry // nil-safe: game runs without agents until wired.
	chatSvc  wwplayer.BotChatSender
	// activityEmitter 暴露 EmitRoomActivity 的扩展 chat service,
	// 2026-07-09 §115 房间聊天增强。nil-safe:旧部署不接也不影响。
	activityEmitter ActivityEmitter

	// spectatorWakeLimiter 节流观战者发言触发的 Agent wake(2026-07-08 §13.6)。
	// 同一房间 15s 内最多触发 1 次 wake,防止 token 暴增。
	// 复用 agentcore.SpeakLimiter(token bucket) — 已在 ratelimit.go 验证。
	spectatorWakeLimiter *agentcore.SpeakLimiter
	// hydrator, when non-nil, is invoked from JoinGame / SpectateGame the
	// first time an empty in-memory room is created for a roomID that has
	// persisted agent seats in MySQL. It returns the (seat, userID,
	// model_key) tuples recorded at room-creation time so the manager can
	// re-create the bots in-process before any human joins.
	//
	// BUG-WEREWOLF-P0-7 FIX: without this, a server restart leaves only DB
	// rows behind; the next spectator/player join sees an empty room and
	// the engine never force-starts because ManagerAddPlayerAt /
	// SetSeatModelKey are never called for the bots.
	hydrator func(roomID string) ([]AgentSeatInfo, error)

	// onGameStarted, when non-nil, is invoked after a successful StartGame
	// so the caller (typically ws.GameService) can update the DB room status
	// from "open" to "playing". Without this, the room API returns
	// status="open" even after the game has started.
	onGameStarted func(roomID string)

	// BUG-R48-P0-4: onGameOver, when non-nil, is invoked after the engine
	// detects a winner (checkWinner returns true). Updates the DB room status
	// from "playing" to "over" so the room disappears from the active list.
	// Without this, phase="over" but status="playing" — a terminal state
	// contradiction.
	onGameOver func(roomID string)

	// §20260810-11 P1 — 终局奖励服务(可选,空时仅发空奖励)。
	rewardSvc *SettlementRewardService

	// onSheriffStreamSettle, when non-nil, is invoked after the engine settles
	// the sheriff stream at dawn (docs/狼人杀13人标准局规则.md §7.4). The callback
	// receives the settlement payload so the ws layer can broadcast
	// game.sheriff_stream_settle to the room. nil-safe:旧部署不接也不影响。
	onSheriffStreamSettle func(roomID string, payload map[string]any)
	// onIdiotRevealed, when non-nil, is invoked after the engine settles the
	// idiot reveal (docs/狼人杀13人标准局规则.md §3.5). The callback receives seat +
	// choice so the ws layer can broadcast game.idiot_revealed. nil-safe。
	onIdiotRevealed func(roomID string, seat int, choice string, revealed bool)
	// onPropUsed, when non-nil, is invoked from broadcastPropUseLocked (while
	// r.mu is held) after a prop is used. The callback receives the room id and a
	// fully-populated PropUseEvent-shaped payload so the ws layer can broadcast
	// the dedicated game.werewolf_prop_used frame that the frontend
	// (useWerewolf.ts) already parses. Without this frame the frontend case is
	// dead code and prop use stays text-only in the activity feed. nil-safe:
	// 旧部署不接也不影响(仍走 chat.activity 路径)。
	onPropUsed func(roomID string, payload map[string]any)

	// onGameOverBroadcast 2026-07-30 解决和设计方案-20260730-03 Fix-A1/A3/C:
	// 终局收编广播回调。两条路径触发:
	//   (a) 进入 §129 冷却期时(tryEnterCoolingFromGameOverLocked 成功) —
	//       冷却期会推迟 EmitGameOver,若不在此广播,在房玩家/观众拿不到
	//       phase=over,status=over 的权威终局帧,只能看到「进行中的尸体字段」
	//       (bot_contexts 残留 / 过期 phase_deadline / 旧法官语);
	//   (b) EmitGameOver 之后(冷却期结束 / 无冷却期直接终局)。
	// ws 层接到后广播 per-seat game.state + game.over 帧(payload 含 winner)。
	// 锁安全: 调用方持 r.mu,回调实现不得反向获取 r.mu(与 onPropUsed 同约束,
	// ws 层广播走 hub.BroadcastTo/BroadcastRoom,不碰引擎锁)。nil-safe。
	onGameOverBroadcast func(roomID string, winner string)

	// onPreWait 2026-07-30 BUG-FIX: §130 人类等待窗口广播回调。tryStartWithHumanWaitLocked
	// 触发时调用,payload 含 room_id / wait_sec / deadline_at / human_player / spectator_count。
	// ws 层接到后广播 game.pre_wait 帧,前端据此把"等待 13 位玩家入座…"改画为
	// "等待人类玩家… (N 秒后自动开始)"。nil-safe:未接线时静默丢弃,回退到旧行为
	// (客户端永远看到"等待 13 位玩家入座…"直到 StartGame 真正触发)。
	onPreWait func(roomID string, payload map[string]any)

	// publicStateCache mirrors the latest successful GetPublicState result
	// per room. Used by the REST API path to fall back when r.mu is held by
	// an in-progress engine op (LLM retry, auto-skip dispatch, quarantine)
	// so the REST caller never blocks on engine concurrency.
	//
	// BUG-WEREWOLF-P1-LOCK (Round 26): /api/games/werewolf/rooms and
	// /api/rooms/{id} hung for 20s+ in curl after the test bot exited a
	// room; the engine was holding r.mu during a long LLM retry, so the
	// REST r.mu.Lock() call queued behind it and the request never returned.
	// Now GetPublicState uses lockRoomBriefly() + this cache to guarantee a
	// bounded-latency REST response.
	publicStateCache sync.Map // roomID → PublicState

	// 2026-07-10 §4: 模型对局日志 hook
	// RecordLog 由 main.go 在 wire 时 SetRecordLogService 注入。
	// StartAgentsLocked 会把此引用传给每个 wwplayer.Agent,让 wwplayer.Run
	// 调 RecordChatMessage / RecordAction 时直接拿到 service。
	// nil-safe:未注入时所有 hook no-op,旧部署不受影响。
	RecordLog *agentcore.RecordLogService
	// Wallet 由 main.go 在 wire 时 SetWalletService 注入,供 EmitGameOver
	// 在对局结束时对所有 bot 玩家做金币结算(±100)。nil 时 no-op。
	Wallet *service.WalletService

	// 2026-07-15 狼人杀 13 人局金币系统 — WS 金币推送回调。
	// BalancePusher 由 main.go 在 wire 时 SetBalancePusher 注入（通常注入
	// ws.Hub.PushBalanceChange 方法值）。nil-safe:nil 时跳过推送,结算仍生效。
	BalancePusher func(userID string, balance, delta int64, reason string)

	// 2026-07-17 金池结算 UI — WS 结算明细推送回调。
	// SettlementPusher 由 main.go 在 wire 时 SetSettlementPusher 注入（通常注入
	// ws.Hub.SendToUser 派生的 per-user 方法值）。

	// 2026-07-21 道具系统 — 道具目录 + 引擎。
	// propCatalog 是运行时道具目录（从 DB 加载，空表时 seed from code）。
	// propEngine 是道具使用引擎（处理使用流程 + 中招判定 + 金币结算）。
	// 两者均由 main.go 在 wire 时注入。nil-safe:nil 时道具系统不可用。
	propCatalog *PropCatalog
	propEngine  *PropEngine
	// 结算后调用,向该人类玩家推送 game.settlement 帧（含 result/ante/netGain/finalBalance/winner）,
	// 前端 WerewolfGamePage 据此渲染 SettlementModal。
	// nil-safe:nil 时跳过推送,结算仍生效。
	SettlementPusher func(userID string, payload map[string]any)

	// §20260811-10 U1 / U2 — 道具特化单帧推送回调。
	// PropSpecialPusher 是 prop.mirror_reveal / prop.behavior_report 单推回调;
	// frameKind 区分两种道具,framePayload 是该帧载荷(map)。ws 层接到后用
	// SendToUser(buyer userID, frameKind, payload) 单独推送,不走 BroadcastRoom。
	// nil-safe:nil 时静默跳过,旧部署零感知(购买者拿不到特化帧)。
	PropSpecialPusher func(userID, frameKind string, payload map[string]any)

	// 2026-07-12 §129 增强 — 冷却期探针接口。
	// coolingHumanPresence 在冷却期被 cooling watchdog 周期调用, 判断当前房间
	// 是否仍有任何人类在线。true = 至少一名人类玩家 / 观察者还在房间里,
	// 冷却期应持续; false = 已无人类, 从此时起算 coolingEmptySince。
	// 默认实现通过 hub.IsRoomEmpty(检查 hub.rooms + hub.spectators 一套集合)
	// 取反 — 非空即有人类。
	// nil-safe:nil 时冷却 watchdog 视作"始终有人类",永不强制关门。
	coolingHumanPresence func(roomID string) bool

	// 2026-07-20 §131 新增 — Agent 持久化记忆(MEMORY.md)。
	// agentMemoryStore 由 main.go 在 wire 时 SetAgentMemoryStore 注入
	// (service.AgentMemoryService 天然实现该窄接口);nil 时整链 no-op。
	// memoryMus 是 per-model_key 的迭代单飞锁(sync.Map[string]*sync.Mutex),
	// 与 DB version 乐观锁构成并发写双保险(重开局相邻触发场景)。
	agentMemoryStore AgentMemoryStore
	memoryMus        sync.Map

	// 2026-08-11 §20260811-05 U1 新增 — Agent 玩家行为画像(PlayerProfile)。
	// playerProfileStore 由 main.go 在 wire 时 SetPlayerProfileStore 注入
	// (service.AgentPlayerProfileService 经 adapter 实现该窄接口);nil 时整链 no-op。
	// 单飞锁复用 memoryMus(同一 model_key 的记忆迭代与画像迭代串行化)。
	playerProfileStore PlayerProfileStore

	// 2026-08-11 §20260811-05 U2 新增 — 赛后复盘问答(RecallChat)。
	// recallLimiter 是每 (room, user) 的提问限流器(默认 10 次/人/房);
	// 房间 GC 时经 ResetRecallRateLimit 清理。
	recallLimiter recallRateLimiter

	// 2026-08-10 §20260810-10 U2 — 模型自画像聚合读取窄接口。
	// selfPortraitReader 由 main.go 在 wire 时 SetSelfPortraitReader 注入
	// (service.ModelLogService 天然实现);nil 时降级为通用自画像(不阻塞游戏流)。
	selfPortraitReader SelfPortraitReader

	// §20260811-10 U4 — WerewolfIQ 落库窄接口。
	// reputationSvc 由 main.go 在 wire 时 SetReputationService 注入
	// (service.ReputationService 天然实现 ReputationStoreLike)。
	// nil 时 ComputeWerewolfIQAsync 整链 no-op(测试环境 / 老部署零感知)。
	reputationSvc ReputationStoreLike
}

// SelfPortraitReader 是模型自画像聚合的 DB 读取窄接口(§20260810-10 U2)。
// service.ModelLogService.SelfPortraits 天然实现;werewolf 包只依赖此接口,
// 不依赖 service 具体类型(便于测试桩注入,与 AgentMemoryStore 同模式)。
type SelfPortraitReader interface {
	SelfPortraits(ctx context.Context, modelKeys []string) (map[string]*SelfPortraitData, error)
}

// SelfPortraitData 是 werewolf 包内的自画像数据镜像(避免 import service)。
// 字段与 service.ModelSelfPortrait 一一对应;装配层负责 struct 拷贝。
type SelfPortraitData struct {
	ModelKey      string
	Games         int64
	WinRate       float64
	WolfGames     int64
	WolfWinRate   float64
	GoodGames     int64
	GoodWinRate   float64
	AvgWinRateAll float64
	SampleOK      bool
}

// SetSelfPortraitReader 注入自画像聚合读取层(§20260810-10 U2)。
// nil 时整链 no-op(Agent.SelfPortraitText 保持空串,system 输出与旧版一致)。
func (m *WerewolfManager) SetSelfPortraitReader(reader SelfPortraitReader) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.selfPortraitReader = reader
}

// SelfPortraitReaderAdapter 把 service.ModelLogService.SelfPortraits 的返回类型
// (map[string]*service.ModelSelfPortrait) 适配为 werewolf 包窄接口。
// main.go wire 时使用:
//
//	werewolfManager.SetSelfPortraitReader(werewolf.SelfPortraitReaderAdapter{Svc: modelLogSvc})
//
// 之所以需要 adapter 而非直接断言接口:Go 接口方法签名中的 map value 类型必须
// 完全一致,werewolf 包不能 import service(避免反向依赖),故做一层显式拷贝。
type SelfPortraitReaderAdapter struct {
	Svc *service.ModelLogService
}

// SelfPortraits 实现 SelfPortraitReader。
func (a SelfPortraitReaderAdapter) SelfPortraits(
	ctx context.Context, modelKeys []string,
) (map[string]*SelfPortraitData, error) {
	out := make(map[string]*SelfPortraitData, len(modelKeys))
	if a.Svc == nil {
		return out, nil
	}
	raw, err := a.Svc.SelfPortraits(ctx, modelKeys)
	if err != nil {
		return nil, err
	}
	for k, p := range raw {
		if p == nil {
			continue
		}
		out[k] = &SelfPortraitData{
			ModelKey:      p.ModelKey,
			Games:         p.Games,
			WinRate:       p.WinRate,
			WolfGames:     p.WolfGames,
			WolfWinRate:   p.WolfWinRate,
			GoodGames:     p.GoodGames,
			GoodWinRate:   p.GoodWinRate,
			AvgWinRateAll: p.AvgWinRateAll,
			SampleOK:      p.SampleOK,
		}
	}
	return out, nil
}

type AgentSeatInfo struct {
	Seat     int
	UserID   string
	ModelKey string
}

func (m *WerewolfManager) SetHydrator(h func(roomID string) ([]AgentSeatInfo, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.hydrator = h
}

func (m *WerewolfManager) SetOnSheriffStreamSettle(cb func(roomID string, payload map[string]any)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onSheriffStreamSettle = cb
}

func (m *WerewolfManager) SetOnIdiotRevealed(cb func(roomID string, seat int, choice string, revealed bool)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onIdiotRevealed = cb
}

func (m *WerewolfManager) SetOnPropUsed(cb func(roomID string, payload map[string]any)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onPropUsed = cb
}

// SetPropSpecialPusher 注入 §20260811-10 U1 / U2 道具特化单帧推送回调。
// ws 层接入方式:main.go 中调 mgr.SetPropSpecialPusher(func(uid, kind, payload) {
//   hub.SendToUser(uid, Envelope{Type: kind, Payload: payload})
// })。
func (m *WerewolfManager) SetPropSpecialPusher(cb func(userID, frameKind string, payload map[string]any)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.PropSpecialPusher = cb
}

func (m *WerewolfManager) SetOnGameStarted(fn func(roomID string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onGameStarted = fn
}

func (m *WerewolfManager) SetOnGameOver(fn func(roomID string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onGameOver = fn
}

// SetOnGameOverBroadcast 注册终局收编广播回调(2026-07-30 方案-20260730-03)。
// 由 main.go wire:回调内做 per-seat game.state + game.over 帧广播。
func (m *WerewolfManager) SetOnGameOverBroadcast(fn func(roomID string, winner string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onGameOverBroadcast = fn
}

// SetOnPreWait 注册 §130 人类等待窗口广播回调(2026-07-30 BUG-FIX)。
// 由 main.go wire:回调内广播 game.pre_wait 帧,前端据此渲染倒计时等待 UI
// 而非误导性的"等待 N 位玩家入座…"。nil-safe:未接线时静默丢弃。
func (m *WerewolfManager) SetOnPreWait(fn func(roomID string, payload map[string]any)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onPreWait = fn
}

func (m *WerewolfManager) SetRecordLogService(svc *agentcore.RecordLogService) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.RecordLog = svc
}

// Room returns the in-memory WerewolfRoom for the given roomID, or (nil,
// false) if no live room is registered. Used by the ws layer to wire
// per-room callbacks (e.g. SetOnTranscriptPublished) that the engine cannot
// register on its own. The returned reference is stable for the room's
// lifetime; callers must NOT retain it beyond the room's teardown.
func (m *WerewolfManager) Room(roomID string) (*WerewolfRoom, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rooms[roomID]
	return r, ok
}

// FactionByUserID resolves a human user to their current werewolf faction
// (wolf|good|unknown) + alive bool within an active room. §20260810-03 F1 —
// used by ChatService.Whisper to apply the same cross-faction guard that the
// Agent driver uses internally (see agent_runner.go:1372-1386). Returns
// (faction="unknown", alive=false, isSpectator=true) when the user is not a
// seated player in the room — caller can then treat them as spectator /
// non-player and apply the appropriate rule.
//
// Safe for concurrent use: takes a brief r.mu lock (~200ms budget via
// lockRoomBriefly) so we don't block the in-process game goroutines.
func (m *WerewolfManager) FactionByUserID(roomID, userID string) (faction string, alive, isSpectator bool) {
	if roomID == "" || userID == "" {
		return "unknown", false, true
	}
	m.mu.Lock()
	r, ok := m.rooms[roomID]
	m.mu.Unlock()
	if !ok || r == nil || r.State == nil {
		return "unknown", false, true
	}
	if !lockRoomBriefly(r, 200*time.Millisecond) {
		return "unknown", false, true
	}
	defer r.mu.Unlock()
	seat, seated := r.SeatOf(userID)
	if !seated {
		return "unknown", false, true
	}
	if int(seat) < 0 || int(seat) >= len(r.State.Players) {
		return "unknown", false, true
	}
	role := r.State.Roles[seat]
	alive = r.State.Players[seat].Alive
	return FactionOf(role).String(), alive, false
}

func (m *WerewolfManager) SetCoolingHumanPresence(fn func(roomID string) bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.coolingHumanPresence = fn
}

func cfgWerewolfCoolingSec() (n int) {
	// 2026-07-30 方案-20260730-03: 命名返回值 — 此前 config.Load() 在测试环境
	// panic(找不到 ./LsmWebGame.conf*),defer recover 后函数返回零值 0,
	// 冷却期被静默禁用(首次调用 false、第二次调用起 1800,行为不可重现)。
	// 默认值 1800 必须在 panic 路径也生效。
	n = 1800
	defer func() { _ = recover() }()
	c := config.Load()
	if c == nil {
		return 1800
	}
	if c.Werewolf.CoolingSec <= 0 {
		return 1800
	}
	return c.Werewolf.CoolingSec
}

func (m *WerewolfManager) SetWalletService(svc *service.WalletService) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Wallet = svc
}

func (m *WerewolfManager) SetBalancePusher(pusher func(userID string, balance, delta int64, reason string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.BalancePusher = pusher
}

func (m *WerewolfManager) SetSettlementPusher(pusher func(userID string, payload map[string]any)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SettlementPusher = pusher
}

func (m *WerewolfManager) SetChatService(cs wwplayer.BotChatSender) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.chatSvc = cs
}

func (m *WerewolfManager) SetPropCatalog(cat *PropCatalog) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.propCatalog = cat
	// 同步引用到所有房间，供 buildAgentContextLocked 填充 PropSnapshot。
	for _, r := range m.rooms {
		if r != nil {
			r.propCatalog = cat
		}
	}
}

func (m *WerewolfManager) SetPropEngine(eng *PropEngine) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.propEngine = eng
	// v3 §G3 — 同步引用到所有房间,供 buildAgentContextLocked 调 walletSvc 填充 WalletBalance。
	for _, r := range m.rooms {
		if r != nil {
			r.propEngine = eng
		}
	}
}

// PlaceSpectatorBet 处理观众押注请求(§20260812-02 U3)。
// 仅观战者可押注;PhaseVote 阶段有效。
func (m *WerewolfManager) PlaceSpectatorBet(roomID, userID string, targetSeat, amount, seatCount int) (string, error) {
	m.mu.Lock()
	r, ok := m.rooms[roomID]
	m.mu.Unlock()
	if !ok || r == nil {
		return "", errBetInvalidTarget
	}
	if !lockRoomBriefly(r, 200*time.Millisecond) {
		return "", errBetInvalidTarget
	}
	defer r.mu.Unlock()
	// 必须在投票阶段
	if r.State == nil || r.State.Phase != PhaseVote {
		return "", betError("bets can only be placed during vote phase")
	}
	return r.PlaceSpectatorBet(userID, targetSeat, amount, seatCount)
}

func (m *WerewolfManager) broadcastPropUseLocked(r *WerewolfRoom, fromSeat, toSeat int, propKey, propName string, hit bool) {
	if r == nil {
		return
	}
	hitStr := ""
	if hit {
		hitStr = " [目标中招!]"
	}
	toStr := ""
	if toSeat >= 0 {
		toStr = fmt.Sprintf("对 %d 号玩家", toSeat+1)
	} else {
		toStr = "对所有玩家"
	}
	text := fmt.Sprintf("💣 %d 号玩家使用道具「%s」%s%s", fromSeat+1, propName, toStr, hitStr)
	phase := ""
	roundN := 0
	if r.State != nil {
		phase = r.State.Phase.String()
		roundN = r.State.DayNumber
	}
	logger.L().Info("prop broadcast",
		zap.String("room_id", r.RoomID),
		zap.String("text", text))
	severity := "info"
	if hit {
		severity = "warn"
	}
	m.emitActivity(r, "prop_used", text, phase, roundN,
		severity, "💣", fromSeat, toSeat, false)

	// 2026-07-23 §道具特效:发送独立前端帧,驱动 PropUseOverlay 特效。
	// 此前端 case(game.werewolf_prop_used) 原为此帧而设,但后端长期未发送,
	// 导致前端路径为死代码;本帧让道具特效叠加 UI 成为可能。
	if m.onPropUsed != nil {
		emoji := PropKeyToEmoji(propKey)
		// from_account:agent 座位用 model_key(模型名),人类座位用 userID。
		fromAccount := ""
		if fromSeat >= 0 && fromSeat < len(r.Seats) {
			fromAccount = r.Seats[fromSeat]
		}
		if modelKey, ok := r.seatModelKeys[fromSeat]; ok && modelKey != "" {
			fromAccount = modelKey
		}
		m.onPropUsed(r.RoomID, map[string]any{
			"room_id":      r.RoomID,
			"from_seat":    fromSeat,
			"from_account": fromAccount,
			"prop_key":     propKey,
			"prop_name":    propName,
			"prop_emoji":   emoji,
			"target_seat":  toSeat,
			"price_paid":   0,
			"hit":          hit,
			"phase":        phase,
			"at":           time.Now().UnixMilli(),
		})
	}
}

