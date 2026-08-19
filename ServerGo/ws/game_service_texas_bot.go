// game_service_texas_bot.go — 德州扑克 Bot Agent WS 层集成(2026-08-19 §德州扑克Agent)。
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
package ws

import (
	"context"
	"time"

	"LsmAgentGame/agent/thpagent"
	"LsmAgentGame/errcode"
	"LsmAgentGame/game/texasholdem"
	"LsmAgentGame/llm"
	"LsmAgentGame/logger"
	"LsmAgentGame/service"

	"go.uber.org/zap"
)

// ─────────────────── Bot Driver 初始化 ───────────────────

// initTexasHoldemBotDriver 初始化德扑 Bot 驱动器。由 NewGameService 调用。
// actionTimeoutSec <= 0 时使用 driver 默认 30s。
func (s *GameService) initTexasHoldemBotDriver(registry *llm.Registry, actionTimeoutSec int) {
	if s.texasHoldemMgr == nil {
		return
	}
	// 创建 driver 并注入 registry
	driver := thpagent.NewDriver()
	if registry != nil {
		driver.SetRegistry(registry)
	}
	if actionTimeoutSec > 0 {
		driver.SetMaxActionTimeoutSec(actionTimeoutSec)
	}
	// 设置 manager 的新手牌回调 → 重置 dispatcher + 清思考标记 + 广播 + 触发 bot
	s.texasHoldemMgr.SetOnHandStarted(func(roomID string) {
		// 1) 重置所有 bot 的 chat 计数(Dispatcher.OnNewHand)+ 广播前的思考标记清理
		s.texasHoldemMgr.WithRoomLocked(roomID, func(r *texasholdem.TexasHoldemRoom) {
			r.ClearBotThinkingAllLocked()
		})
		s.thpDriver.OnNewHandLocked(roomID)
		// 2) 广播新手牌状态
		s.broadcastTexasHoldemState(roomID)
		// 3) 检查 bot 先行动
		s.ProcessBotTurn(roomID)
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

	logger.L().Info("texasholdem agent seats registered",
		zap.String("room_id", roomID),
		zap.Int("agent_count", len(seatConfigs)))
	return nil
}

// ─────────────────── Bot 行动入口 ───────────────────

// ProcessBotTurn 检查当前是否 bot 行动,是则调 LLM 决策并应用。
// 由 WS 层在以下时机调用:
//   1. handleTexasHoldemAction (人类行动后)
//   2. handleTexasHoldemJoin (游戏开始时)
//   3. onHandStarted 回调 (延迟 auto-start 新手牌后)
//
// 如果 bot 行动后下一个仍是 bot(连续 bot),自动链式触发,直到轮到人类或手牌结束。
//
// 2026-08-19 §德州扑克Agent v1.1 增强:
//   - BotThinking 在 LLM 调用期间标 true,完成后置 false(前端可观测)
//   - BotHeartThought 在 LLM 返回 thought 时写入房间(前端 PlayerSeat hover 弹全文)
//   - 每手牌开始时由 driver 调 dispatcher.OnNewHand 重置 chat 计数
func (s *GameService) ProcessBotTurn(roomID string) {
	if s.thpDriver == nil || s.texasHoldemMgr == nil {
		return
	}

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

		// 构造 thpagent 上下文
		agentCtx := buildAgentContext(ctx, s.texasHoldemMgr.BotSeatModelKey(roomID, seat))

		// 调 LLM 决策(30s 超时)
		decideCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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

		// 记录 bot 内心独白(供前端 BotThoughtPanel 渲染)
		if action.Thought != "" {
			s.recordBotThought(roomID, seat, action.Thought)
		}

		// 转换 thpagent.Action → texasholdem.Action
		engineAction := convertToEngineAction(action)

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
		}

		// 广播状态(把最新 thought/thinking 推到所有玩家)
		s.broadcastTexasHoldemState(roomID)

		if handOver {
			s.broadcastTexasHoldemOver(roomID, nil)
			return // 手牌结束,等待 onHandStarted 回调触发下一手
		}

		// 继续检查下一个行动者是否也是 bot(链式触发)
	}
}

// ─────────────────── 内部辅助 ───────────────────

// buildAgentContext 把引擎快照转为 thpagent.GameContextForAgent。
func buildAgentContext(snap *texasholdem.BotGameContextSnapshot, modelKey string) *thpagent.GameContextForAgent {
	// 计算数学信号
	handStrength, _ := thpagent.HandStrength(snap.MyHole, snap.Community, snap.CommunityLen, 0)
	_, requiredEquity := thpagent.PotOdds(snap.CallAmount, snap.Pot)
	posLabel, posLabelZh := thpagent.Position(snap.MySeat, snap.Button)

	return &thpagent.GameContextForAgent{
		RoomID:          snap.RoomID,
		HandNumber:      snap.HandNumber,
		Street:          snap.Street,
		MySeat:          snap.MySeat,
		MyHole:          snap.MyHole,
		Community:       snap.Community,
		CommunityLen:    snap.CommunityLen,
		MyStack:         snap.MyStack,
		Pot:             snap.Pot,
		CurrentBet:      snap.CurrentBet,
		CallAmount:      snap.CallAmount,
		HandStrength:    handStrength,
		RequiredEquity:  requiredEquity,
		Position:        posLabel,
		PositionLabelZh: posLabelZh,
		BluffHint:       0.15, // v1.0 固定值,v1.1 接入对手弃牌率
		OpponentsCount:  snap.Opponents,
		ActionHistory:   snap.ActionHistory,
		ModelNameField:  modelKey,
		// 2026-08-19 §德州扑克金币 — 经济档位透传(由 game 侧按房间总金币计算)
		EconTier:      snap.EconTier,
		RoomTotalCoin: snap.RoomTotalCoin,
		RakeRatePct:   snap.RakeRatePct,
	}
}

// convertToEngineAction 把 thpagent.Action 转为 texasholdem.Action。
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
