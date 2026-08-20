// Package thpagent — driver.go: 房间级 Agent 驱动器（2026-08-19）。
//
// 职责：
//   - 维护 roomID → agents[6]*Agent 映射
//   - 启动/停止 Agent goroutines
//   - 提供 DecideAction / RegisterAgents / UnregisterAgents 公开 API
//   - **所有** 修改 r/agents 的方法必须有 *Locked 锁内变体（§92a）
package thpagent

import (
	"context"
	"sync"
	"time"

	agentroot "LsmAgentGame/agent"
	agentcore "LsmAgentGame/agent/core"
	"LsmAgentGame/llm/types"
	"LsmAgentGame/logger"
	"go.uber.org/zap"
)

// Registry is a thin interface over llm.Registry so the thpagent package
// doesn't import llm (avoids circular dependency risk). The driver only
// uses Get(); concrete wiring is done from ws/game_service.go.
type Registry interface {
	Get(modelKey string) (types.LLMProvider, string, error)
}

// Driver 是房间级 Agent 驱动器（每个 TexasHoldemManager 一个）。
type Driver struct {
	mu sync.RWMutex

	// roomID -> roomAgents
	rooms map[string]*RoomAgents

	// 配置（注入）
	maxActionTimeoutSec int
	registry            Registry

	// wallet/profile 服务（占位 — v1.1 注入）
	walletCb  func(userID string, delta int64, reason string) error
	profileCb func(modelKey string) error
}

// RoomAgents 是单房间的 Agent 集合。
type RoomAgents struct {
	roomID   string
	agents   [6]*Agent           // 6 个座位(空座位 nil)
	dispatch [6]*Dispatcher      // 每个 Agent 一个 dispatcher
	memories [6]*Memory
	models   [6]string           // seat -> model_key
}

// NewDriver 构造一个空 Driver。
func NewDriver() *Driver {
	return &Driver{
		rooms:                make(map[string]*RoomAgents),
		maxActionTimeoutSec:   30,
	}
}

// SetRegistry 注入 LLM Registry（必须在 DecideAction 之前调用）。
func (d *Driver) SetRegistry(r Registry) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.registry = r
}

// SetMaxActionTimeoutSec 注入单 bot 决策超时上限(秒)。
// 由 main.go 在 wireRoomService 阶段从 cfg.TexasHoldem.AgentActionTimeoutSec 读出注入。
func (d *Driver) SetMaxActionTimeoutSec(sec int) {
	if sec <= 0 {
		sec = 30
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.maxActionTimeoutSec = sec
}

// RegisterAgents 注册 Agent 座位。必须在 TexasHoldemManager.StartHand 之前调用。
//
// 参数：
//   - roomID: 房间 ID
//   - seatConfigs: 座位配置（每个含 seat + model_key + agent_name）
//
// 锁内变体（§92a）：调用方必须已持有 room 锁。
func (d *Driver) RegisterAgents(roomID string, seatConfigs []SeatConfig) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	ra, ok := d.rooms[roomID]
	if !ok {
		ra = &RoomAgents{roomID: roomID}
		d.rooms[roomID] = ra
	}

	for _, cfg := range seatConfigs {
		if cfg.Seat < 0 || cfg.Seat >= 6 {
			continue
		}
		a := NewAgent(roomID, cfg.UserID, cfg.ModelKey, cfg.Seat)
		a.ModelName = cfg.ModelName
		if d.registry != nil {
			// 注入 Provider + API key（§20260813-04 U1 wiring）
			if p, apiKey, err := d.registry.Get(cfg.ModelKey); err == nil && p != nil {
				a.SetProvider(p, cfg.ModelKey)
				a.SetAPIKey(apiKey)
			}
		}
		ra.agents[cfg.Seat] = a
		ra.dispatch[cfg.Seat] = NewDispatcher()
		ra.memories[cfg.Seat] = NewMemory()
		ra.models[cfg.Seat] = cfg.ModelKey

		// 启动 Agent goroutine
		go a.Run()

		logger.L().Info("texasholdem agent registered",
			zap.String("room_id", roomID),
			zap.Int("seat", cfg.Seat),
			zap.String("model", cfg.ModelKey))
	}

	return nil
}

// SeatConfig 是单个 Agent 座位配置。
type SeatConfig struct {
	Seat      int
	UserID    string
	ModelKey  string
	ModelName string
}

// OnNewHandLocked 每手牌开始时重置所有 bot 的 per-hand 状态（2026-08-20 §B2 扩展）。
// 由 WS 层 SetOnHandStarted 回调调用（engine 触发 onHandStarted 后）。
//
// 接线清单（此前全部是 §130「声明了却从不接线」死代码）：
//   1. Dispatcher.OnNewHand — 重置 chat 计数（v1.0 已接）
//   2. Memory.ResetCurrentHand — 清空本手动作记录
//   3. Memory.IncrementHandsPlayed — 对房间内所有其他入座玩家（含人类）累计手数
//
// seats: 房间座位表（空串 = 空位），由调用方从 TexasHoldemManager.Seats 快照。
func (d *Driver) OnNewHandLocked(roomID string, seats [6]string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	ra, ok := d.rooms[roomID]
	if !ok {
		return
	}
	for i := 0; i < 6; i++ {
		if ra.dispatch[i] != nil {
			ra.dispatch[i].OnNewHand()
		}
		mem := ra.memories[i]
		if mem == nil {
			continue
		}
		mem.ResetCurrentHand()
		for j := 0; j < 6; j++ {
			if j == i || seats[j] == "" {
				continue
			}
			mem.IncrementHandsPlayed(seats[j])
		}
	}
	logger.L().Debug("texasholdem driver OnNewHand", zap.String("room_id", roomID))
}

// RecordPlayerAction 把一次已应用到引擎的动作广播给所有 bot 的 Memory
// （2026-08-20 §B2：UpdateOpponentStat / RecordAction 此前零生产调用点）。
//
// 调用时机：bot 动作在 ProcessBotTurn 应用成功后 + 人类 action 在引擎成功后。
//   - 行动者本人（若是 bot）：记录到自己的 CurrentHandActions
//   - 其他所有 bot：UpdateOpponentStat(行动者) + RecordAction
func (d *Driver) RecordPlayerAction(roomID string, actorSeat int, actorUserID, actionType string, amount int, street string) {
	if actorUserID == "" {
		return
	}
	d.mu.RLock()
	ra, ok := d.rooms[roomID]
	d.mu.RUnlock()
	if !ok {
		return
	}
	rec := ActionRecordForMemory{Seat: actorSeat, ActionType: actionType, Amount: amount, Street: street}
	for i := 0; i < 6; i++ {
		mem := ra.memories[i]
		if mem == nil {
			continue
		}
		if i == actorSeat {
			mem.RecordAction(rec) // 自己的动作也入本手时间线
			continue
		}
		mem.UpdateOpponentStat(actorUserID, actorSeat, actionType)
		mem.RecordAction(rec)
	}
}

// HandSeatSummary 是单手牌结束时单个座位的结算摘要（2026-08-20 §B2）。
// 与 game/texasholdem 的结算快照平行（避免循环 import，由 ws 层转换）。
type HandSeatSummary struct {
	Seat     int
	UserID   string
	Hole     [2]int
	NetDelta int  // 本手净盈亏（筹码,结算前）
	Won      bool // 是否在 winners 中
}

// AppendHandResult 手牌结束时把结果写入所有 bot 的 Memory
// （2026-08-20 §B2：AppendHand / OpponentStat.NetChips 此前零生产调用点）。
//
//   - 每个 bot：AppendHand(HandRecord{... 自己的底牌/公共牌/赢家/净盈亏})
//   - 每个 bot 对房间内其他所有入座玩家：RecordOpponentHandResult（驱动玩家画像）
func (d *Driver) AppendHandResult(roomID string, handNumber int, community [5]int, communityLen int, winners []int, seats []HandSeatSummary) {
	d.mu.RLock()
	ra, ok := d.rooms[roomID]
	d.mu.RUnlock()
	if !ok {
		return
	}
	for i := 0; i < 6; i++ {
		mem := ra.memories[i]
		if mem == nil {
			continue
		}
		var mine *HandSeatSummary
		for k := range seats {
			if seats[k].Seat == i {
				mine = &seats[k]
				break
			}
		}
		if mine != nil {
			mem.AppendHand(HandRecord{
				HandNumber:   handNumber,
				MySeat:       i,
				MyHole:       mine.Hole,
				Community:    community,
				CommunityLen: communityLen,
				Winners:      append([]int{}, winners...),
				NetChipDelta: mine.NetDelta,
			}, 5)
		}
		for k := range seats {
			if seats[k].Seat == i || seats[k].UserID == "" {
				continue
			}
			mem.RecordOpponentHandResult(seats[k].UserID, seats[k].Seat, seats[k].NetDelta, seats[k].Won)
		}
	}
}

// BluffHintFor 返回指定 bot 的虚张频率建议（2026-08-20 §B2）。
// 取该 bot Memory 中所有有交手记录对手的弃牌率均值 → BluffFrequency 映射。
// 无数据时返回 0.15 中性默认（与旧硬编码一致,保证无回归）。
func (d *Driver) BluffHintFor(roomID string, seat int) float64 {
	d.mu.RLock()
	ra, ok := d.rooms[roomID]
	d.mu.RUnlock()
	if !ok || seat < 0 || seat >= 6 || ra.memories[seat] == nil {
		return 0.15
	}
	stats := ra.memories[seat].AllOpponentStats()
	sum := 0.0
	n := 0
	for _, st := range stats {
		if st.HandsPlayed <= 0 {
			continue
		}
		sum += st.FoldRate
		n++
	}
	if n == 0 {
		return 0.15
	}
	return BluffFrequency(sum / float64(n))
}

// OnNewRoundLocked 重置指定 bot 座位的 poker_action 限流(每轮 1 次)。
// 由 engine.ApplyAction 路径后或 ProcessBotTurn 每轮开头调用。
func (d *Driver) OnNewRoundLocked(roomID string, seat int) {
	d.mu.Lock()
	defer d.mu.Unlock()

	ra, ok := d.rooms[roomID]
	if !ok || seat < 0 || seat >= 6 {
		return
	}
	if ra.dispatch[seat] != nil {
		ra.dispatch[seat].OnNewRound()
	}
}

// UnregisterAgents 注销房间的所有 Agent。
func (d *Driver) UnregisterAgents(roomID string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	ra, ok := d.rooms[roomID]
	if !ok {
		return
	}
	for i := 0; i < 6; i++ {
		if ra.agents[i] != nil {
			ra.agents[i].Cancel()
			ra.agents[i] = nil
		}
		ra.dispatch[i] = nil
		ra.memories[i] = nil
	}
	delete(d.rooms, roomID)
	logger.L().Info("texasholdem agents unregistered", zap.String("room_id", roomID))
}

// DecideAction 让指定 bot 座位做决策。返回 (action, error)。
//
// 阻塞调用，超时由 ctx 控制（默认 30s）。
// 锁内变体（§92a）：调用方必须已持有 room 锁；此函数**不**修改房间状态,仅读 + 调 LLM。
//
// 2026-08-19 §德州扑克Agent v1.1: 真实 LLM 调用链路:
//   1. 获取 Agent 的 Provider + apiKey
//   2. BuildSystemPrompt + BuildUserPrompt + BuildTools
//   3. provider.Chat(ctx, apiKey, req)
//   4. 解析 tool_use 块 → poker_action
//   5. 兜底: LLM 失败/超时/无 tool_use → fold
func (d *Driver) DecideAction(ctx context.Context, roomID string, seat int, promptContext *GameContextForAgent) (Action, error) {
	d.mu.RLock()
	ra, ok := d.rooms[roomID]
	if !ok {
		d.mu.RUnlock()
		return Action{}, ErrRoomNotRegistered
	}
	if seat < 0 || seat >= 6 || ra.agents[seat] == nil {
		d.mu.RUnlock()
		return Action{}, ErrSeatNotRegistered
	}
	a := ra.agents[seat]
	disp := ra.dispatch[seat]
	mem := ra.memories[seat]
	d.mu.RUnlock()

	if a.IsCancelled() {
		return Action{}, ErrAgentCancelled
	}

	// 获取 Provider（只读快照）
	a.mu.Lock()
	provider := a.Provider
	apiKey := a.apiKey
	a.mu.Unlock()

	if provider == nil || apiKey == "" {
		logger.L().Warn("texasholdem DecideAction: no provider/apiKey, forcing fold",
			zap.String("room_id", roomID),
			zap.Int("seat", seat))
		return Action{Type: ActFold, Thought: "no LLM provider configured"}, nil
	}

	// 构建 prompt
	systemPrompt := BuildSystemPrompt(promptContext, mem)
	userPrompt := BuildUserPrompt(promptContext, mem)
	tools := BuildTools()

	// 构造 LLM 请求
	req := a.BuildLLMRequest(systemPrompt, userPrompt, tools)

	// 调 LLM（带重试: 第 1 次失败重试 1 次）
	var resp types.LLMResponse
	var callErr error
	startMs := time.Now().UnixMilli()

	for attempt := 0; attempt < 2; attempt++ {
		resp, callErr = provider.Chat(ctx, apiKey, *req)
		if callErr == nil {
			break
		}
		logger.L().Warn("texasholdem LLM call failed, retrying",
			zap.String("room_id", roomID),
			zap.Int("seat", seat),
			zap.Int("attempt", attempt+1),
			zap.Error(callErr))
	}

	latencyMs := time.Now().UnixMilli() - startMs
	a.updateStats(latencyMs)

	if callErr != nil {
		logger.L().Warn("texasholdem LLM call failed after retry, forcing fold",
			zap.String("room_id", roomID),
			zap.Int("seat", seat),
			zap.Int64("latency_ms", latencyMs),
			zap.Error(callErr))
		return Action{Type: ActFold, Thought: "LLM call failed: " + callErr.Error()}, nil
	}

	// 解析 tool_use 块
	action, thought, chatText := parseToolUseResponse(resp, disp, promptContext)

	// 2026-08-20 §B5: poker_chat 接线 —— 去重 + 限流后挂到 Action.ChatText,
	// 由 WS 层走 ChatService.SendFromBot 真正广播。限流拒绝时只丢 chat,
	// action 照常应用（工具协议 §5）。
	if chatText != "" {
		cleaned, deduped, truncated := agentcore.DedupSpeakText(chatText)
		if deduped || truncated {
			logger.L().Debug("texasholdem bot chat deduped/truncated",
				zap.String("room_id", roomID),
				zap.Int("seat", seat),
				zap.Bool("deduped", deduped),
				zap.Bool("truncated", truncated))
		}
		if cleaned == "" {
			// 整段复读被清空 —— 丢弃 chat
		} else if err := disp.DispatchPokerChat(cleaned); err != nil {
			logger.L().Debug("texasholdem bot chat rate-limited, dropped",
				zap.String("room_id", roomID),
				zap.Int("seat", seat),
				zap.Error(err))
		} else {
			action.ChatText = cleaned
		}
	}

	// 记录决策摘要
	if thought != "" {
		a.SetInternalThought(thought)
		mem.SetLastDecisionSummary(thought)
	}
	a.recordAction()

	logger.L().Info("texasholdem bot decided",
		zap.String("room_id", roomID),
		zap.Int("seat", seat),
		zap.String("action", action.Type),
		zap.Int("amount", action.Amount),
		zap.Int64("latency_ms", latencyMs))

	return action, nil
}

// parseToolUseResponse 从 LLM 响应中解析 poker_action / poker_chat tool_use。
// 返回 (action, thought, chatText)。未找到 poker_action 时返回 fold + reason。
//
// 2026-08-20 §B5：poker_chat 的 text 不再只拼进日志字符串,而是作为独立
// 返回值交给 DecideAction 做去重 + 限流（internal_thought 仍只进 thought,
// 协议层隔离,绝不入 chat — §119）。
func parseToolUseResponse(resp types.LLMResponse, disp *Dispatcher, ctx *GameContextForAgent) (Action, string, string) {
	var textContent string
	var action Action
	var thought string
	var chatText string
	foundAction := false

	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			textContent += block.Text
		case "tool_use":
			if block.Name == "poker_action" && !foundAction {
				foundAction = true
				action = parseActionFromInput(block.Input)
				// 提取 internal_thought
				if t, ok := block.Input["internal_thought"].(string); ok {
					thought = t
				}
			} else if block.Name == "poker_chat" && chatText == "" {
				if t, ok := block.Input["text"].(string); ok {
					chatText = t
				}
			}
		}
	}

	if !foundAction {
		// LLM 没有返回 tool_use → 用 textContent 作为 thought, fold 兜底
		if thought == "" {
			thought = textContent
		}
		return Action{Type: ActFold, Thought: thought}, thought, chatText
	}

	// 通过 dispatcher 校验(限流/合法性)
	validated, err := disp.DispatchPokerAction(action)
	if err != nil {
		// 校验失败(如本轮已行动) → fold 兜底
		return Action{Type: ActFold, Thought: "dispatch rejected: " + err.Error()}, thought, chatText
	}

	return validated, thought, chatText
}

// parseActionFromInput 从 tool_use input 中解析 Action。
func parseActionFromInput(input map[string]any) Action {
	action := Action{}

	if t, ok := input["action"].(string); ok {
		action.Type = t
	}
	if amt, ok := input["amount"].(float64); ok {
		action.Amount = int(amt)
	}
	if t, ok := input["internal_thought"].(string); ok {
		action.Thought = t
	}

	// 校验 action 类型
	switch action.Type {
	case ActFold, ActCheck, ActCall, ActBet, ActRaise, ActAllIn:
		// 合法
	default:
		action.Type = ActFold // 未知类型 → fold
	}

	return action
}

// 错误定义
var (
	ErrRoomNotRegistered   = driverError("room not registered with driver")
	ErrSeatNotRegistered   = driverError("seat not registered with driver")
	ErrAgentCancelled      = driverError("agent cancelled")
)

type driverError string

func (e driverError) Error() string { return string(e) }

// GetAgentCountForRoom 返回房间的 bot 数量（供测试 / 调试）。
func (d *Driver) GetAgentCountForRoom(roomID string) int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	ra, ok := d.rooms[roomID]
	if !ok {
		return 0
	}
	count := 0
	for _, a := range ra.agents {
		if a != nil {
			count++
		}
	}
	return count
}

// SuppressUnusedImport agentroot 用于跨包 lint（防止后续重构误删）。
var _ = agentroot.AgentClassTexasHoldemPlayer