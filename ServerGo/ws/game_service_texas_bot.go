// game_service_texas_bot.go — 德州扑克 Bot Agent WS 层集成(2026-08-19 §德州扑克Agent;
// 2026-08-20 §B2/B4/B5/B6/B8 缺口修复)。
//
// 职责:
//   1. 初始化 thpagent.Driver(注入 LLM Registry)
//   2. 提供 ProcessBotTurn 入口 — 检查当前是否 bot 行动,是则调 LLM 决策并应用
//   3. 处理连续 bot 行动(bot1 → bot2 → bot3 链式触发)
//   4. RegisterAgentSeats 的 texasholdem 分支 — 把 bot 座位注册到 in-memory 房间
//
// 设计原则(沿用 §15 + §92a):
//   - 每 bot 座位一个 thpagent.Agent + goroutine(in-process 驱动)
//   - Agent 通过 ToolRunner 接口调 TexasHoldemManager.ApplyBotAction(不走 WS)
//   - 锁语义:BotGameContext 自行获取房间锁;DecideAction 不持锁(LLM 调用可能 30s+)
//
// 2026-08-20 缺口修复:
//   - §B2 Memory 接线:OnNewHandLocked(带座位表)/RecordPlayerAction/AppendHandResult/BluffHintFor
//   - §B4 normalizeBotAction:raise < min_raise 自动抬升、> stack 改 allin、allin <90% 改 raise(金币设计 §4.3)
//   - §B5 poker_chat:限流通过后走 ChatService.SendFromBot 真正广播(此前仅拼进日志)
//   - §B6 ProcessBotTurn 异步化 + per-room 串行守卫(不再阻塞人类 WS handler)
//   - §B8 bot 回合 watchdog:5s tick,同 actingSeat 超 timeout+10s 强制 fold(§81b 复查)
package ws

import (
	"context"
	"sync"
	"time"

	"LsmAgentGame/agent/thpagent"
	"LsmAgentGame/errcode"
	"LsmAgentGame/game/texasholdem"
	"LsmAgentGame/llm"
	"LsmAgentGame/logger"
	"LsmAgentGame/service"

	"go.uber.org/zap"
)

// texasBotWatchdogHandle 是 per-room bot 回合 watchdog 的句柄(§B8)。
// 用指针装入 sync.Map 以便 CompareAndDelete 精确删除(func 不可比较)。
type texasBotWatchdogHandle struct {
	cancel context.CancelFunc
}

// ─────────────────── Bot Driver 初始化 ───────────────────

// initTexasHoldemBotDriver 初始化德扑 Bot 驱动器。由 NewGameService 调用。
// actionTimeoutSec <= 0 时使用 driver 默认 30s。
func (s *GameService) initTexasHoldemBotDriver(registry *llm.Registry, actionTimeoutSec int) {
	if s.texasHoldemMgr == nil {
		return
	}
	if actionTimeoutSec <= 0 {
		actionTimeoutSec = 30
	}
	s.thpActionTimeoutSec = actionTimeoutSec
	// 创建 driver 并注入 registry
	driver := thpagent.NewDriver()
	if registry != nil {
		driver.SetRegistry(registry)
	}
	driver.SetMaxActionTimeoutSec(actionTimeoutSec)
	// 设置 manager 的新手牌回调 → 重置 dispatcher + 清思考标记 + 广播 + 触发 bot
	s.texasHoldemMgr.SetOnHandStarted(func(roomID string) {
		// 1) 重置所有 bot 的 chat 计数 + Memory 本手状态 + 广播前的思考标记清理
		s.texasHoldemMgr.WithRoomLocked(roomID, func(r *texasholdem.TexasHoldemRoom) {
			r.ClearBotThinkingAllLocked()
		})
		// §B2: OnNewHandLocked 携带座位表,驱动 ResetCurrentHand + IncrementHandsPlayed
		if seats, ok := s.texasHoldemMgr.Seats(roomID); ok {
			s.thpDriver.OnNewHandLocked(roomID, seats)
		}
		// 2) 广播新手牌状态
		s.broadcastTexasHoldemState(roomID)
		// 3) 检查 bot 先行动(§B6: 异步 + 串行守卫)
		go s.ProcessBotTurn(roomID)
	})
	// §B2: 手牌结束回调 → 每个 bot 的 Memory 记账(AppendHand + 对手画像)
	s.texasHoldemMgr.SetOnHandOver(func(roomID string) {
		handNum, community, communityLen, winners, seats, ok := s.texasHoldemMgr.HandEndSummaries(roomID)
		if !ok {
			return
		}
		s.thpDriver.AppendHandResult(roomID, handNum, community, communityLen, winners, seats)
	})
	// 保存 driver 引用(供 RegisterAgentSeats / ProcessBotTurn 使用)
	s.thpDriver = driver
	logger.L().Info("texasholdem bot driver initialized")
}

// ─────────────────── Agent 座位注册 ───────────────────

// registerTexasHoldemAgentSeats 把 agent_seats 注册到 in-memory 德扑房间。
// 由 RegisterAgentSeats 的 texasholdem 分支调用。
func (s *GameService) registerTexasHoldemAgentSeats(roomID string, seats []service.AgentSeatConfig) *errcode.Error {
	if s.texasHoldemMgr == nil || s.thpDriver == nil {
		return nil
	}

	// 1. 把 bot 座位标记到房间(BotSeats/BotModels)
	seatModels := make(map[int]string)
	seatConfigs := make([]thpagent.SeatConfig, 0, len(seats))
	for _, seatCfg := range seats {
		botUserID, err := s.botUserIDForSeat(roomID, seatCfg.Seat)
		if err != nil {
			logger.L().Warn("registerTexasHoldemAgentSeats: resolve bot user failed",
				zap.String("room_id", roomID),
				zap.Int("seat", seatCfg.Seat),
				zap.Error(err))
			continue
		}
		seatModels[seatCfg.Seat] = seatCfg.ModelKey
		seatConfigs = append(seatConfigs, thpagent.SeatConfig{
			Seat:      seatCfg.Seat,
			UserID:    botUserID,
			ModelKey:  seatCfg.ModelKey,
			ModelName: seatCfg.ModelKey, // v1.0 用 modelKey 作为展示名
		})
	}

	// 2. 把 bot 座位注册到 in-memory 房间(JoinGame 让 bot user 入座)
	for seatIdx, modelKey := range seatModels {
		_ = modelKey
		botUserID, _ := s.botUserIDForSeat(roomID, seatIdx)
		if botUserID == "" {
			continue
		}
		room, _, e := s.texasHoldemMgr.JoinGame(roomID, botUserID)
		if e != nil {
			logger.L().Warn("registerTexasHoldemAgentSeats: JoinGame failed",
				zap.String("room_id", roomID),
				zap.Int("seat", seatIdx),
				zap.Int("code", e.Code), zap.String("msg", e.Message))
			continue
		}
		// 标记 bot 座位
		room.RegisterBotSeatsLocked(map[int]string{seatIdx: seatModels[seatIdx]})
	}

	// 3. 注册到 thpagent.Driver
	if e := s.thpDriver.RegisterAgents(roomID, seatConfigs); e != nil {
		logger.L().Warn("registerTexasHoldemAgentSeats: driver registration failed",
			zap.String("room_id", roomID),
			zap.Error(e))
	}

	// 4. §B8: 启动 per-room bot 回合 watchdog
	if len(seatConfigs) > 0 {
		s.startTexasBotWatchdog(roomID)
	}

	logger.L().Info("texasholdem agent seats registered",
		zap.String("room_id", roomID),
		zap.Int("agent_count", len(seatConfigs)))
	return nil
}

// cleanupTexasHoldemBotRuntime 房间删除时的 bot 运行时清理(§B6/§B8)。
// 接线:RemoveRoomState + game.leave 的 texasholdem 分支。
func (s *GameService) cleanupTexasHoldemBotRuntime(roomID string) {
	if v, ok := s.thpWatchdogs.LoadAndDelete(roomID); ok {
		v.(*texasBotWatchdogHandle).cancel()
	}
	s.thpTurnGuards.Delete(roomID)
	if s.thpDriver != nil {
		s.thpDriver.UnregisterAgents(roomID)
	}
}

// rehydrateTexasHoldemAgents §20260819-02 P0-1 -- 重启恢复后重建 bot 运行时。
//
// 触发:handleSpectate 的 texasholdem 分支看到 manager 返回 created=true
// (即内存房间原本不存在,这次是 SpectateGame 懒创建的)。manager 侧 hydrator
// 只恢复内存座位 + 视图,触达不到 ws 层的 thpDriver 与 bot watchdog。
// 这里从 DB 读 agent 座位清单,复用 registerTexasHoldemAgentSeats 重建
// driver 注册 + watchdog 启动。JoinGame / RegisterBotSeatsLocked 本身是
// 幂等的(已入座直接返回,BotSeats 覆写无副作用),重复调用安全。
func (s *GameService) rehydrateTexasHoldemAgents(roomID string) {
	if s.roomSvc == nil {
		logger.L().Debug("rehydrateTexasHoldemAgents: roomSvc is nil, skip",
			zap.String("room_id", roomID))
		return
	}
	seats, err := s.roomSvc.BotSeatsForRoom(roomID)
	if err != nil {
		logger.L().Warn("rehydrateTexasHoldemAgents: list agent seats failed",
			zap.String("room_id", roomID), zap.Error(err))
		return
	}
	if len(seats) == 0 {
		// 纯人类房间无需 bot 运行时(hydrator 恢复座位即可,无 driver 注册)。
		return
	}
	cfgs := make([]service.AgentSeatConfig, 0, len(seats))
	for _, st := range seats {
		cfgs = append(cfgs, service.AgentSeatConfig{
			Seat:     st.Seat,
			ModelKey: st.ModelKey,
		})
	}
	if e := s.registerTexasHoldemAgentSeats(roomID, cfgs); e != nil {
		logger.L().Warn("rehydrateTexasHoldemAgents: register failed",
			zap.String("room_id", roomID), zap.Error(e))
		return
	}
	logger.L().Info("rehydrated texasholdem agent runtime after restart",
		zap.String("room_id", roomID), zap.Int("agent_count", len(seats)))
}

// ─────────────────── Bot 行动入口 ───────────────────

// ProcessBotTurn 检查当前是否 bot 行动,是则调 LLM 决策并应用。
// 由 WS 层在以下时机调用(全部以 `go s.ProcessBotTurn(...)` 异步触发,§B6):
//   1. handleTexasHoldemAction (人类行动后)
//   2. handleTexasHoldemJoin (游戏开始时)
//   3. onHandStarted 回调 (延迟 auto-start 新手牌后)
//   4. §B8 watchdog 强制 fold 后的链式推进
//
// §B6:per-room 串行守卫(thpTurnGuards)防止两个 goroutine 并发驱动同一房间;
// panic 由 defer recover 兜住并保证守卫释放(防 goroutine 泄漏 / 房间永久卡死)。
//
// 如果 bot 行动后下一个仍是 bot(连续 bot),自动链式触发,直到轮到人类或手牌结束。
func (s *GameService) ProcessBotTurn(roomID string) {
	if s.thpDriver == nil || s.texasHoldemMgr == nil {
		return
	}
	v, _ := s.thpTurnGuards.LoadOrStore(roomID, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	defer func() {
		if r := recover(); r != nil {
			logger.L().Error("texasholdem ProcessBotTurn panic recovered",
				zap.String("room_id", roomID),
				zap.Any("panic", r))
		}
		mu.Unlock()
	}()

	// 最多连续处理 6 个 bot(6-max 上限,防死循环)
	for i := 0; i < 6; i++ {
		seat, isBot := s.texasHoldemMgr.IsBotSeatTurn(roomID)
		if !isBot {
			return // 轮到人类或手牌已结束
		}

		// 获取 bot 上下文
		ctx := s.texasHoldemMgr.BotGameContext(roomID, seat)
		if ctx == nil {
			logger.L().Warn("ProcessBotTurn: nil game context",
				zap.String("room_id", roomID),
				zap.Int("seat", seat))
			return
		}

		// 标记该 bot 为思考中(锁内,前端可见)
		s.setBotThinking(roomID, seat, true)

		// 构造 thpagent 上下文(§B2: BluffHint 由 Memory 对手弃牌率驱动,不再硬编码)
		modelKey := s.texasHoldemMgr.BotSeatModelKey(roomID, seat)
		agentCtx := buildAgentContext(ctx, modelKey, s.thpDriver.BluffHintFor(roomID, seat))

		// 调 LLM 决策(配置超时,默认 30s)
		timeoutSec := s.thpActionTimeoutSec
		if timeoutSec <= 0 {
			timeoutSec = 30
		}
		decideCtx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
		action, err := s.thpDriver.DecideAction(decideCtx, roomID, seat, agentCtx)
		cancel()

		// 思考完成 — 立刻置 false,不等广播
		s.setBotThinking(roomID, seat, false)

		if err != nil {
			logger.L().Warn("ProcessBotTurn: LLM decision failed, forcing fold",
				zap.String("room_id", roomID),
				zap.Int("seat", seat),
				zap.Error(err))
			action = thpagent.Action{Type: thpagent.ActFold, Thought: "LLM error, forced fold"}
		}

		// 记录 bot 内心独白(供前端 BotThoughtPanel 渲染;§119 协议层隔离,绝不入 chat 表)
		if action.Thought != "" {
			s.recordBotThought(roomID, seat, action.Thought)
		}

		// §B5: poker_chat 公屏发言 — 已通过 driver 内部去重 + 限流,这里真正广播
		if action.ChatText != "" {
			s.sendBotChat(roomID, seat, ctx.MyUserID, modelKey, action.ChatText)
		}

		// §B4: 动作规范化(raise < min_raise 抬升 / > stack 改 allin / allin <90% 改 raise)
		engineAction := normalizeBotAction(ctx, action)

		// 应用 bot 动作
		handOver, e := s.texasHoldemMgr.ApplyBotAction(roomID, seat, engineAction)
		if e != nil {
			logger.L().Warn("ProcessBotTurn: apply bot action failed",
				zap.String("room_id", roomID),
				zap.Int("seat", seat),
				zap.String("action", action.Type),
				zap.Int("code", e.Code), zap.String("msg", e.Message))
			// bot 动作失败 → 强制 fold 兜底
			foldOver, foldErr := s.texasHoldemMgr.ApplyBotAction(roomID, seat, texasholdem.Action{Type: texasholdem.ActFold})
			if foldErr != nil {
				logger.L().Error("ProcessBotTurn: forced fold also failed",
					zap.String("room_id", roomID),
					zap.Int("seat", seat),
					zap.Int("code", foldErr.Code), zap.String("msg", foldErr.Message))
				return
			}
			handOver = foldOver
			engineAction = texasholdem.Action{Type: texasholdem.ActFold}
		}

		// §B2: 动作已应用 → 所有 bot 的 Memory 记录行动者统计
		s.thpDriver.RecordPlayerAction(roomID, seat, ctx.MyUserID, engineAction.Type.String(), engineAction.Amount, ctx.Street)

		// 广播状态(把最新 thought/thinking 推到所有玩家)
		s.broadcastTexasHoldemState(roomID)

		if handOver {
			s.broadcastTexasHoldemOver(roomID, nil)
			return // 手牌结束,等待 onHandStarted 回调触发下一手
		}

		// 继续检查下一个行动者是否也是 bot(链式触发)
	}
}

// sendBotChat 把 bot 的公屏发言写入聊天系统(§B5)。
// 失败只记日志,绝不影响已决策的动作(聊天是可选工具)。
func (s *GameService) sendBotChat(roomID string, seat int, botUserID, modelKey, text string) {
	if s.chatSvc == nil {
		return
	}
	if botUserID == "" {
		var err error
		botUserID, err = s.botUserIDForSeat(roomID, seat)
		if err != nil || botUserID == "" {
			logger.L().Warn("sendBotChat: resolve bot user failed",
				zap.String("room_id", roomID),
				zap.Int("seat", seat),
				zap.Error(err))
			return
		}
	}
	// botAccount 传空串,SendFromBot 内部 lookupAccount 兜底
	if _, err := s.chatSvc.SendFromBot(roomID, botUserID, "", modelKey, text); err != nil {
		logger.L().Warn("sendBotChat: SendFromBot failed",
			zap.String("room_id", roomID),
			zap.Int("seat", seat),
			zap.Error(err))
		return
	}
	logger.L().Debug("texasholdem bot chat sent",
		zap.String("room_id", roomID),
		zap.Int("seat", seat),
		zap.String("text", truncate(text, 80)))
}

// ─────────────────── §B8 Bot 回合 watchdog ───────────────────

// startTexasBotWatchdog 启动 per-room bot 回合 watchdog(幂等)。
// 每 5s 检查:当前回合是 bot 座位且已持续 > agent_action_timeout_sec+10s →
// 强制 fold 并继续链式触发(§81b:fold 前复查仍是该座位)。
// 房间删除时由 cleanupTexasHoldemBotRuntime 取消;房间自然消失时自退。
func (s *GameService) startTexasBotWatchdog(roomID string) {
	if _, loaded := s.thpWatchdogs.Load(roomID); loaded {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	h := &texasBotWatchdogHandle{cancel: cancel}
	if _, loaded := s.thpWatchdogs.LoadOrStore(roomID, h); loaded {
		cancel()
		return
	}
	go s.texasBotWatchdogLoop(roomID, ctx, h)
	logger.L().Debug("texasholdem bot watchdog started", zap.String("room_id", roomID))
}

func (s *GameService) texasBotWatchdogLoop(roomID string, ctx context.Context, h *texasBotWatchdogHandle) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	// 自退时仅当 map 里仍是本句柄才删除(防止误删 cleanup 后重新注册的新句柄)
	defer s.thpWatchdogs.CompareAndDelete(roomID, h)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !s.texasBotWatchdogTick(roomID) {
				return
			}
		}
	}
}

// texasBotWatchdogTick 单次巡检。返回 false = 房间已不存在,watchdog 退出。
func (s *GameService) texasBotWatchdogTick(roomID string) bool {
	seat, startedAt, isBot, ok := s.texasHoldemMgr.BotTurnInfo(roomID)
	if !ok {
		logger.L().Debug("texasholdem bot watchdog: room gone, exiting",
			zap.String("room_id", roomID))
		return false
	}
	if !isBot || startedAt.IsZero() {
		return true
	}
	timeoutSec := s.thpActionTimeoutSec
	if timeoutSec <= 0 {
		timeoutSec = 30
	}
	if time.Since(startedAt) < time.Duration(timeoutSec+10)*time.Second {
		return true
	}
	// §81b: 强制 fold 前重新检查当前回合仍是该 bot 座位(防与 ProcessBotTurn 双驱动)
	seat2, isBot2 := s.texasHoldemMgr.IsBotSeatTurn(roomID)
	if !isBot2 || seat2 != seat {
		return true
	}
	logger.L().Warn("texasholdem bot turn watchdog forcing fold",
		zap.String("room_id", roomID),
		zap.Int("seat", seat),
		zap.Duration("stuck_for", time.Since(startedAt)))
	s.setBotThinking(roomID, seat, false)
	handOver, e := s.texasHoldemMgr.ApplyBotAction(roomID, seat, texasholdem.Action{Type: texasholdem.ActFold})
	if e != nil {
		// 状态已变化(如手牌刚好结束) → 下一 tick 自然恢复
		logger.L().Debug("texasholdem watchdog fold rejected",
			zap.String("room_id", roomID),
			zap.Int("seat", seat),
			zap.Int("code", e.Code))
		return true
	}
	// §B2: 强制 fold 也计入行动统计(roomSvc 缺失时跳过,如纯内存测试)
	if s.roomSvc != nil && s.thpDriver != nil {
		if uid, err := s.botUserIDForSeat(roomID, seat); err == nil && uid != "" {
			s.thpDriver.RecordPlayerAction(roomID, seat, uid, "fold", 0, "")
		}
	}
	s.broadcastTexasHoldemState(roomID)
	if handOver {
		s.broadcastTexasHoldemOver(roomID, nil)
		return true
	}
	// 链式推进:下一个可能仍是 bot(§B6: 异步 + 串行守卫防与正在运行的 ProcessBotTurn 撞车)
	go s.ProcessBotTurn(roomID)
	return true
}

// ─────────────────── 内部辅助 ───────────────────

// buildAgentContext 把引擎快照转为 thpagent.GameContextForAgent。
//
// 2026-08-20 §B2/§B3:bluffHint 由调用方从 driver.BluffHintFor(Memory 对手弃牌率)
// 取真实值(此前硬编码 0.15);MyRoundCommitted/MinRaise/BigBlind 全量透传(此前缺失,
// prompt 只能输出字面「?」占位)。
func buildAgentContext(snap *texasholdem.BotGameContextSnapshot, modelKey string, bluffHint float64) *thpagent.GameContextForAgent {
	// 计算数学信号
	handStrength, _ := thpagent.HandStrength(snap.MyHole, snap.Community, snap.CommunityLen, 0)
	_, requiredEquity := thpagent.PotOdds(snap.CallAmount, snap.Pot)
	posLabel, posLabelZh := thpagent.Position(snap.MySeat, snap.Button)

	return &thpagent.GameContextForAgent{
		RoomID:           snap.RoomID,
		HandNumber:       snap.HandNumber,
		Street:           snap.Street,
		MySeat:           snap.MySeat,
		MyHole:           snap.MyHole,
		Community:        snap.Community,
		CommunityLen:     snap.CommunityLen,
		MyStack:          snap.MyStack,
		MyRoundCommitted: snap.MyRoundCommitted,
		Pot:              snap.Pot,
		CurrentBet:       snap.CurrentBet,
		CallAmount:       snap.CallAmount,
		MinRaise:         snap.MinRaise,
		BigBlind:         snap.BigBlind,
		HandStrength:     handStrength,
		RequiredEquity:   requiredEquity,
		Position:         posLabel,
		PositionLabelZh:  posLabelZh,
		BluffHint:        bluffHint,
		OpponentsCount:   snap.Opponents,
		ActionHistory:    snap.ActionHistory,
		ModelNameField:   modelKey,
		// 2026-08-19 §德州扑克金币 — 经济档位透传(由 game 侧按房间总金币计算)
		EconTier:      snap.EconTier,
		RoomTotalCoin: snap.RoomTotalCoin,
		RakeRatePct:   snap.RakeRatePct,
	}
}

// convertToEngineAction 把 thpagent.Action 转为 texasholdem.Action(纯类型映射)。
// 语义规范化(最小加注/超筹码改 allin 等)在 normalizeBotAction 完成(§B4)。
func convertToEngineAction(a thpagent.Action) texasholdem.Action {
	var actionType texasholdem.ActionType
	switch a.Type {
	case thpagent.ActFold:
		actionType = texasholdem.ActFold
	case thpagent.ActCheck:
		actionType = texasholdem.ActCheck
	case thpagent.ActCall:
		actionType = texasholdem.ActCall
	case thpagent.ActBet:
		actionType = texasholdem.ActBet
	case thpagent.ActRaise:
		actionType = texasholdem.ActRaise
	case thpagent.ActAllIn:
		actionType = texasholdem.ActAllIn
	default:
		actionType = texasholdem.ActFold // 未知动作 → fold 兜底
	}
	return texasholdem.Action{Type: actionType, Amount: a.Amount}
}

// recordBotThought 把 bot 的内心独白写入 TexasHoldemRoom(锁内调),
// 下一次 broadcastTexasHoldemState 会通过 BuildClientStateWithRoom 把它透传给前端。
// 协议层隔离:thought 仅入内存态(r.BotHeartThought[seat]),**绝不**入 chat_message
// 表(同狼人杀 §119 heart-thought 协议层隔离)。
func (s *GameService) recordBotThought(roomID string, seat int, thought string) {
	if thought == "" {
		return
	}
	s.texasHoldemMgr.WithRoomLocked(roomID, func(r *texasholdem.TexasHoldemRoom) {
		r.SetBotHeartThoughtLocked(seat, thought)
	})

	logger.L().Info("texasholdem bot thought",
		zap.String("room_id", roomID),
		zap.Int("seat", seat),
		zap.String("thought", truncate(thought, 100)))
}

// setBotThinking 把 bot 思考状态写入 TexasHoldemRoom(锁内调)。
// 必须在 LLM 调用前后成对调用(true → false),前端 PlayerSeat 用此渲染 ⏳ 指示器。
func (s *GameService) setBotThinking(roomID string, seat int, thinking bool) {
	s.texasHoldemMgr.WithRoomLocked(roomID, func(r *texasholdem.TexasHoldemRoom) {
		r.SetBotThinkingLocked(seat, thinking)
	})
}

// truncate 截断字符串到最大长度。
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}
