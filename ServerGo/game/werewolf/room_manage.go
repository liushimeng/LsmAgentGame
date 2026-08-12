package werewolf

import (
	"context"
	"strings"
	"time"

	"LsmWebGame/agent/wwtypes"
	"LsmWebGame/errcode"
	"LsmWebGame/logger"

	"go.uber.org/zap"
)

func (m *WerewolfManager) RemoveGame(roomID string) {
	m.mu.Lock()
	r, ok := m.rooms[roomID]
	if !ok {
		m.mu.Unlock()
		return
	}
	delete(m.rooms, roomID)
	m.mu.Unlock()

	// Stop agents outside the manager-level lock to avoid deadlock with the
	// agent goroutine (which acquires r.mu when building GameContext).
	if r != nil {
		r.mu.Lock()
		m.stopAgentsLocked(r)
		r.mu.Unlock()
	}
	// 2026-08-11 §20260811-05 U2 — 房间 GC 时清理复盘提问限流计数
	// (§130 接线验证:防止 recallLimiter.counts 随房间数无限增长)。
	m.ResetRecallRateLimit(roomID)
}

func (m *WerewolfManager) WipeAllRooms() []string {
	m.mu.Lock()
	ids := make([]string, 0, len(m.rooms))
	type entry struct {
		id   string
		room *WerewolfRoom
	}
	entries := make([]entry, 0, len(m.rooms))
	for id, r := range m.rooms {
		ids = append(ids, id)
		if r != nil {
			entries = append(entries, entry{id: id, room: r})
		}
		delete(m.rooms, id)
	}
	m.mu.Unlock()

	for _, e := range entries {
		e.room.mu.Lock()
		m.stopAgentsLocked(e.room)
		e.room.mu.Unlock()
	}
	if len(ids) > 0 {
		logger.L().Warn("werewolf: WipeAllRooms cleared in-memory state",
			zap.Int("rooms", len(ids)))
	}
	return ids
}

func (m *WerewolfManager) RoomIDs() []string {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.rooms))
	for id := range m.rooms {
		out = append(out, id)
	}
	return out
}

func (m *WerewolfManager) RemovePlayer(roomID, userID string) *errcode.Error {
	r := m.getRoom(roomID)
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.State != nil {
		r.State.RemovePlayer(userID)
	}
	for i, u := range r.Seats {
		if u == userID {
			r.Seats[i] = ""
			break
		}
	}
	return nil
}

func (m *WerewolfManager) HandleDisconnect(roomID, userID string) *errcode.Error {
	r := m.getRoom(roomID)
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	// 仅在游戏已开局(phase != filling)时标记死亡。filling 阶段掉线由 LeaveRoom
	// 清理 DB 行即可,不应把座位标记为死亡(否则 AddPlayer 重新入座时会被误判)。
	if r.State == nil || r.State.Phase == PhaseFilling {
		return nil
	}
	seat := r.State.SeatOf(userID)
	if seat == NoSeat || !r.State.AliveSeat(seat) {
		return nil
	}
	// 标记为死亡(死因 = disconnected)。保留 Roles[seat] 供终局展示。
	if e := r.State.killPlayer(seat, "disconnected"); e != nil {
		return e
	}
	// 若 acting seat 指向已断线的座位,推进到下一个存活的行动者。
	//
	// BUG-R212-P1-01(2026-07-31):WitchSeat/GuardSeat 分支补 HasActorAt 守卫。
	// 历史原因:真人玩家"创建房间 → 立即 leave/spectate"路径不调用本函数
	//(只走 room_service_crud.LeaveRoom 删 DB),in-memory r.State.Seats[seat]
	// 残留 userID + Alive=true,导致女巫/守卫 leave 后仍被当作可 acting 座位,
	// phase 卡死至 watchdog 兜底。WitchSeat/GuardSeat 与 firstLiving* 不同,
	// 是单一固定席位,补 HasActorAt 即可彻底关闭这条路径。
	if r.State.TurnActingSeat == seat {
		switch r.State.Phase {
		case PhaseNightWolves:
			r.State.TurnActingSeat = firstLivingWolfLocked(r.State)
			if r.State.TurnActingSeat == NoSeat {
				r.State.endWolfPhase()
			}
		case PhaseNightSeer:
			r.State.TurnActingSeat = firstLivingSeer(r.State)
			if r.State.TurnActingSeat == NoSeat {
				r.State.endSeerPhase()
			}
		case PhaseNightWitch:
			if r.State.WitchSeat != NoSeat && r.State.HasActorAt(r.State.WitchSeat) && r.State.AliveSeat(r.State.WitchSeat) {
				r.State.TurnActingSeat = r.State.WitchSeat
			} else {
				r.State.endWitchPhase()
			}
		case PhaseNightGuard:
			if r.State.GuardSeat != NoSeat && r.State.HasActorAt(r.State.GuardSeat) && r.State.AliveSeat(r.State.GuardSeat) {
				r.State.TurnActingSeat = r.State.GuardSeat
			} else {
				r.State.endGuardPhase()
			}
		}
	}
	r.State.refreshCounts()
	r.State.checkWinner()
	// 发出死亡事件,使前端与活动流同步。
	m.EmitPlayerDied(r, int(seat), "disconnected")
	// 唤醒 acting agents,使阶段可以推进。
	m.wakeActingAgentsLocked(r, "player_disconnected")
	return nil
}

func (m *WerewolfManager) SpectateGame(roomID, userID string) (*WerewolfRoom, *errcode.Error) {
	m.mu.Lock()
	r, ok := m.rooms[roomID]
	if !ok {
		r = &WerewolfRoom{
			RoomID:         roomID,
			createdAt:      time.Now(),
			recentSpeeches: make([]wwtypes.SpeechEvent, 0, recentSpeechBufferSize),
			whisperInbox:   make(map[int][]wwtypes.WhisperEvent, MaxPlayers),
			// BUG-R242-P1-01: llmSema 由 StartAgentsLocked 懒创建(不在此处设)。
		}
		m.rooms[roomID] = r
		logger.L().Info("werewolf room created by spectator",
			zap.String("room_id", roomID), zap.String("user_id", userID))
	}
	m.mu.Unlock()

	r.mu.Lock()
	defer r.mu.Unlock()
	// BUG-WEREWOLF-P0-7 FIX: same restart-recovery hydration as JoinGame. The
	// most common path after a server crash is "spectator reloads /werewolf/:id
	// URL" — SpectateGame is the entry point and must rebuild the bot seats +
	// seatModelKeys before we even register the spectator, otherwise the
	// spectator view will forever be phase=filling.
	if m.hydrator != nil && len(r.seatModelKeys) == 0 && r.Occupied() == 0 {
		if agents, e := m.hydrator(roomID); e == nil && len(agents) > 0 {
			m.restoreBotsLocked(r, agents)
		}
	}
	// BUG-WEREWOLF-P0-7 + BUG-WEREWOLF-SPECTATE-FILLING FIX: if the freshly
	// hydrated room is already at 7/7 (full-AI room surviving a restart), or
	// if its in-memory State was lost but the seats survived hydration, force
	// a StartGame now so the spectator joining this URL sees a live phase
	// rather than a permanent filling. Previously this branch was guarded by
	// `r.State != nil && r.State.Phase == PhaseFilling`, which short-circuited
	// the post-restart case where r.State was nil — the line below then
	// created a brand-new NewGame() in PhaseFilling and the spectator was
	// stuck on "👁 观战中（等待 7 位玩家入座…）" forever (Round 24 P0
	// observation: REST rooms/{id} reported phase=filling for status=playing
	// rooms because of exactly this branch).
	needStart := false
	if r.State == nil && r.Occupied() == MaxPlayers {
		// Post-restart State loss: synthesise a fresh game and start it
		// immediately so the spectator sees the running phase (night_wolves
		// / speak / …) rather than the spinner-equivalent.
		r.State = NewGame(m.seedFn())
		needStart = true
	}
	if r.State != nil && r.State.Phase == PhaseFilling && r.IsReady() {
		needStart = true
	}
	if needStart {
		// §130 重构(2026-07-13):有 Agent + 真人(观察者)时,先进入"人类等待窗口"。
		// 等待窗口已建立 → 跳过立即 StartGame,由 watchdog 推进。
		if m.tryStartWithHumanWaitLocked(r) {
			// 等待中,不立即启动游戏。
		} else {
			// 2026-08-06 §20260806-03: 发牌前同步座位角色偏好。
			syncPreferredRolesLocked(r)
			if err := r.State.StartGame(); err != nil {
				logger.L().Warn("werewolf spectate force-start failed",
					zap.String("room_id", roomID), zap.Error(err))
			} else {
				r.gameStartedAt = time.Now().Unix()
				// 2026-08-10 §20260810-05 — 信息账本 role_deal 登记(发牌成功路径)。
				r.ledgerRegisterRoleDealLocked()
				// 2026-07-14 BUG-R116-03: 新一局开始时重置单座位发言冷却。
				r.seatLastPublicSpeak = make(map[int]time.Time)
				// 2026-07-15 BUG-R124-UI-001: 新一局开始时清零单座位每阶段发言计数。
				r.seatSpeakCountThisPhase = make(map[int]int)
				r.seatSpeakCountPhaseTag = ""
				logger.L().Info("werewolf room force-started via spectator recovery",
					zap.String("room_id", roomID))
				m.StartAgentsLocked(r)
				// 2026-07-10 §125 增强 — 启动法官 goroutine(若 JudgeMode 启用)。
				// 2026-07-30 §重构:启动条件改为 cfgWerewolfJudgeMode() != "off"
				// 且 r.JudgeDesired=true。在 StartAgentsLocked 之后跑,确保 seatModelKeys 已就位。
				m.startJudgeGoroutine(r)
				// Same wake as JoinGame / ForceStartIfReady: the freshly spawned
				// agent goroutines block on evCh until they receive a wake event.
				// Must use wakeAllAgentsLocked because we hold r.mu.
				m.wakeAllAgentsLocked(r, "state_change", wwtypes.GameContext{Phase: r.State.Phase.String()})
				// BUG-R200-P0-01 (2026-07-30): 同 ForceStartIfReady 修复 —
				// 在持锁状态同步调 DB 写会阻断所有试图抢 r.mu 的 WS handler /
				// REST snapshot 路径,表现为 WS game.spectate 帧无响应。释放锁
				// 后再回调 DB 写 + publicStateCache 种入快照。
				seedPhase := r.State.Phase.String()
				seedDay := r.State.DayNumber
				seedStatus := r.State.Status
				r.mu.Unlock()
				// Notify caller to update DB room status from "open" to "playing".
				if m.onGameStarted != nil {
					m.onGameStarted(roomID)
				}
				// publicStateCache 是 sync.Map(零值即可用),无需 nil 守卫。
				m.publicStateCache.Store(roomID, PublicState{
					Phase:  seedPhase,
					Day:    seedDay,
					Status: seedStatus,
				})
				// 重新加锁,补上 return 路径前的 Spectators 注册。
				r.mu.Lock()
			}
		}
	}
	if r.Spectators == nil {
		r.Spectators = make(map[string]struct{})
	}
	r.Spectators[userID] = struct{}{}
	if r.State == nil {
		// Fallback for rooms that are still in PhaseFilling (capacity not yet
		// reached, e.g. partial AI + partial human spectators). The spectator
		// sees a benign empty board; the standard broadcastWerewolfState will
		// start the game the moment the last human player joins.
		r.State = NewGame(time.Now().UnixNano())
	}
	for i := range r.Seats {
		if r.Seats[i] == "" && r.State.Players[i].UserID != "" {
			r.Seats[i] = r.State.Players[i].UserID
		}
	}
	return r, nil
}

func (m *WerewolfManager) UnspectateGame(roomID, userID string) *errcode.Error {
	r := m.getRoom(roomID)
	if r == nil {
		return errcode.Code(errcode.ErrRoomNotFound)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.Spectators, userID)
	return nil
}

func (m *WerewolfManager) SpectatorList(roomID string) []string {
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

func (m *WerewolfManager) SpectatorState(roomID, userID string) (*ClientGameState, *errcode.Error) {
	r := m.getRoom(roomID)
	if r == nil {
		return nil, errcode.Code(errcode.ErrRoomNotFound)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	cs := BuildClientStateWithRoom(roomID, r, -1)
	r.populateBotContexts(cs)
	r.populateAgentNames(cs, m.registry)
	return cs, nil
}

func (m *WerewolfManager) SpectatorView(roomID string) *ClientGameState {
	r := m.getRoom(roomID)
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	cs := BuildClientStateWithRoom(roomID, r, -1)
	r.populateBotContexts(cs)
	r.populateAgentNames(cs, m.registry)
	return cs
}

func lockRoomBriefly(r *WerewolfRoom, d time.Duration) bool {
	// Fast path: uncontended lock — avoids the channel allocation.
	if r.mu.TryLock() {
		return true
	}
	if d <= 0 {
		return false
	}
	// Slow path: poll TryLock until either we win or the deadline expires.
	// The ticker is coarse (5ms) — lockRoomBriefly is only used by REST
	// snapshot / best-effort bookkeeping paths where a few ms of extra
	// latency is irrelevant, and by the phase watchdog where a missed tick
	// is simply retried 5s later.
	deadline := time.Now().Add(d)
	poll := time.NewTicker(5 * time.Millisecond)
	defer poll.Stop()
	for {
		select {
		case <-poll.C:
			if r.mu.TryLock() {
				return true
			}
			if time.Now().After(deadline) {
				return false
			}
		}
	}
}

func (m *WerewolfManager) shouldHumanWaitLocked(r *WerewolfRoom) bool {
	if cfgWerewolfHumanWaitSec() <= 0 {
		return false
	}
	if r == nil {
		return false
	}
	if len(r.seatModelKeys) == 0 {
		return false // 全人类房间:无需等待,直接 StartGame
	}
	if !r.humanWaitDeadlineAt.IsZero() {
		return false // 已经在等待中,避免重复启动
	}
	if r.State == nil || r.State.Phase != PhaseFilling {
		return false
	}
	// 判定真人:至少有一个 Seats[i] 的 userID 不是 bot userID。
	hasHumanPlayer := false
	for _, uid := range r.Seats {
		if uid == "" {
			continue
		}
		if !m.isBotUserIDLocked(r, uid) {
			hasHumanPlayer = true
			break
		}
	}
	hasSpectator := len(r.Spectators) > 0
	if !hasHumanPlayer && !hasSpectator {
		return false // 全 AI 房间:无人类,不等待
	}
	return true
}

func (m *WerewolfManager) isBotUserIDLocked(r *WerewolfRoom, userID string) bool {
	if userID == "" {
		return false
	}
	if m.hydrator != nil {
		if agents, err := m.hydrator(r.RoomID); err == nil {
			for _, a := range agents {
				if a.UserID == userID {
					return true
				}
			}
		}
	}
	if strings.HasPrefix(userID, "bot_") {
		return true
	}
	return false
}

func (m *WerewolfManager) tryStartWithHumanWaitLocked(r *WerewolfRoom) bool {
	if !m.shouldHumanWaitLocked(r) {
		return false
	}
	waitSec := cfgWerewolfHumanWaitSec()
	r.humanWaitDeadlineAt = time.Now().Add(time.Duration(waitSec) * time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	r.humanWaitCancel = cancel

	// 广播 game.pre_wait 帧(仅一次)。
	// 2026-07-30 BUG-FIX:此前 payload 构建后以 `_ = payload` 丢弃,前端永远收不到
	// 等待窗口通知,导致 12AI+1 人类房间客户端误渲染"等待 13 位玩家入座…"永久死锁。
	// 改为经 onPreWait 回调委托 ws 层广播 game.pre_wait 帧。
	if !r.humanWaitBroadcastSent {
		r.humanWaitBroadcastSent = true
		payload := map[string]any{
			"room_id":         r.RoomID,
			"wait_sec":        waitSec,
			"deadline_at":     r.humanWaitDeadlineAt.UnixMilli(),
			"human_player":    true,
			"spectator_count": len(r.Spectators),
		}
		logger.L().Info("human wait started; agents will boot after window expires",
			zap.String("room_id", r.RoomID),
			zap.Int("wait_sec", waitSec),
			zap.Int64("deadline_at", r.humanWaitDeadlineAt.UnixMilli()),
			zap.Int("spectator_count", len(r.Spectators)))
		if m.onPreWait != nil {
			m.onPreWait(r.RoomID, payload)
		}
	}

	go m.humanWaitWatchdog(r.RoomID, waitSec, ctx)
	return true
}

func (m *WerewolfManager) humanWaitWatchdog(roomID string, waitSec int, ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	deadline := time.Now().Add(time.Duration(waitSec) * time.Second)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r := m.getRoom(roomID)
			if r == nil {
				return
			}
			r.mu.Lock()
			if r.State == nil || r.State.Phase != PhaseFilling {
				r.mu.Unlock()
				return
			}
			// 房间突然无人类 → 取消等待,立即开始。
			hasHuman := false
			for _, uid := range r.Seats {
				if uid != "" && !m.isBotUserIDLocked(r, uid) {
					hasHuman = true
					break
				}
			}
			if !hasHuman && len(r.Spectators) == 0 {
				logger.L().Info("human wait cancelled: no human left in room, starting immediately",
					zap.String("room_id", roomID))
				r.humanWaitDeadlineAt = time.Time{}
				if r.humanWaitCancel != nil {
					r.humanWaitCancel()
					r.humanWaitCancel = nil
				}
				// 2026-08-06 §20260806-03: 发牌前同步座位角色偏好。
				syncPreferredRolesLocked(r)
				if err := r.State.StartGame(); err != nil {
					logger.L().Warn("human wait fallback StartGame failed",
						zap.String("room_id", roomID), zap.Error(err))
					r.mu.Unlock()
					return
				}
				r.gameStartedAt = time.Now().Unix()
				// 2026-08-10 §20260810-05 — 信息账本 role_deal 登记(发牌成功路径)。
				r.ledgerRegisterRoleDealLocked()
				// 2026-07-14 BUG-R116-03: 新一局开始时重置单座位发言冷却。
				r.seatLastPublicSpeak = make(map[int]time.Time)
				// 2026-07-15 BUG-R124-UI-001: 新一局开始时清零单座位每阶段发言计数。
				r.seatSpeakCountThisPhase = make(map[int]int)
				r.seatSpeakCountPhaseTag = ""
				m.StartAgentsLocked(r)
				m.startJudgeGoroutine(r)
				m.wakeAllAgentsLocked(r, "state_change", wwtypes.GameContext{Phase: r.State.Phase.String()})
				// BUG-R200-R211-P0-01 (2026-07-30): 同步种 publicStateCache 快照
				// (对应对应 ForceStartIfReady / completeHumanWaitAndStart 的同期修复)。
				seedPhase := r.State.Phase.String()
				seedDay := r.State.DayNumber
				seedStatus := r.State.Status
				r.mu.Unlock()
				if m.onGameStarted != nil {
					m.onGameStarted(roomID)
				}
				m.publicStateCache.Store(roomID, PublicState{Phase: seedPhase, Day: seedDay, Status: seedStatus})
				return
			}
			r.mu.Unlock()
			// deadline 到 → 启动游戏。
			if !time.Now().Before(deadline) {
				m.completeHumanWaitAndStart(roomID)
				return
			}
		}
	}
}

func (m *WerewolfManager) completeHumanWaitAndStart(roomID string) {
	r := m.getRoom(roomID)
	if r == nil {
		return
	}
	// BUG-R212-P0-02 (2026-07-30): 本函数**不能**用 defer 解锁 —— 末尾必须
	// 显式 Unlock 后再回调 onGameStarted(DB 写) + 种 publicStateCache
	// (R211 两段式范式)。R211 从 ForceStartIfReady(纯显式解锁,无 defer)搬来
	// 后半段范式时漏删了这里的 `defer r.mu.Unlock()`,造成末尾 Unlock 之后
	// defer 再解一次 → `fatal error: sync: unlock of unlocked mutex`
	// (不可 recover,直接杀进程);更隐蔽的是若此间隙有其他 goroutine 抢到锁,
	// defer 会静默释放**它**的临界区。范式对齐 ForceStartIfReady:全部提前
	// return 分支显式解锁。
	r.mu.Lock()
	if r.State == nil || r.State.Phase != PhaseFilling {
		r.mu.Unlock()
		return // 已被其他路径覆盖。
	}
	// 2026-08-06 §20260806-03: 发牌前同步座位角色偏好。
	syncPreferredRolesLocked(r)
	if err := r.State.StartGame(); err != nil {
		logger.L().Warn("human wait deadline StartGame failed",
			zap.String("room_id", roomID), zap.Error(err))
		r.mu.Unlock()
		return
	}
	r.gameStartedAt = time.Now().Unix()
	// 2026-08-10 §20260810-05 — 信息账本 role_deal 登记(发牌成功路径)。
	r.ledgerRegisterRoleDealLocked()
	// 2026-07-14 BUG-R116-03: 新一局开始时重置单座位发言冷却。
	r.seatLastPublicSpeak = make(map[int]time.Time)
	// 2026-07-15 BUG-R124-UI-001: 新一局开始时清零单座位每阶段发言计数。
	r.seatSpeakCountThisPhase = make(map[int]int)
	r.seatSpeakCountPhaseTag = ""
	logger.L().Info("human wait completed; starting game",
		zap.String("room_id", roomID),
		zap.Int64("seed", r.State.Seed))
	// 清空等待状态。
	r.humanWaitDeadlineAt = time.Time{}
	if r.humanWaitCancel != nil {
		r.humanWaitCancel()
		r.humanWaitCancel = nil
	}
	r.humanWaitBroadcastSent = false
	m.StartAgentsLocked(r)
	m.startJudgeGoroutine(r)
	m.wakeAllAgentsLocked(r, "state_change", wwtypes.GameContext{Phase: r.State.Phase.String()})
	// BUG-R200-R211-P0-01 (2026-07-30): 在人类等待窗口到期路径种下权威
	// publicStateCache 快照。ForceStartIfReady 的全 AI 直开分支(§92a-2/R210)在
	// 持锁状态下种;此分支必须在解锁前同步种,避免后续 REST /api/rooms/:id 在
	// 引擎持锁时(慢 LLM 调 / 自动 skip 派发 / 隔离路径)退回 publicStateCache
	// 命中却发现 cache 中无该 room,而 fallback 到 phase=filling,status=open
	// 的旧假数据 —— 表现为"房间永久卡 filling"(R211 复现)。
	seedPhase := r.State.Phase.String()
	seedDay := r.State.DayNumber
	seedStatus := r.State.Status
	r.mu.Unlock()
	if m.onGameStarted != nil {
		m.onGameStarted(roomID)
	}
	m.publicStateCache.Store(roomID, PublicState{Phase: seedPhase, Day: seedDay, Status: seedStatus})
}
