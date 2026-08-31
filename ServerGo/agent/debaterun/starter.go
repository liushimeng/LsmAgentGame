// Package debaterun — 辩方 + 裁判 + 解说 Agent 启动器(2026-08-31 §20260831-01)。
//
// 独立包(不放在 debate 或 debateplayer/debatejudge 中)以避免:
//
//	debate → agent/debateplayer → debate   (循环)
//
// 由 DebateManager.StartGame 调用,遍历房间内所有 Agent Bot 启动 goroutine。
// 裁判 Agent 由 engine_phase.runJudgingPhase 触发。
// 解说 Agent 由 DebateManager 事件钩子触发(phase_change / speech / game_over)。
package debaterun

import (
	"context"
	"sync"

	"LsmAgentGame/agent/debatecommentator"
	"LsmAgentGame/agent/debatejudge"
	"LsmAgentGame/agent/debateplayer"
	"LsmAgentGame/game/debate"
	"LsmAgentGame/llm"
	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// BotHandle 单个 Bot 句柄(用于 Stop + Stats)。
type BotHandle struct {
	TeamID int
	Seat   int
	cancel context.CancelFunc
	// §20260831-09 — Bot 实例引用(供房间级 Token 聚合读 stats)。
	// 仅在 Stop() 前非 nil;由 StartAgents 注入。
	agent *debateplayer.Agent
}

// JudgeHandle 单个裁判句柄。
type JudgeHandle struct {
	JudgeID int
	cancel  context.CancelFunc
	// §20260831-09 — 裁判实例引用(供房间级 Token 聚合读 stats)。
	judge *debatejudge.AgentJudge
}

// CommentatorHandle 解说着句柄。
type CommentatorHandle struct {
	cancel context.CancelFunc
}

// Registry 房间级 Agent 句柄集合(挂在 DebateRoom.agentRegistry 字段)。
//
// 字段名加下划线避免与 struct 内其它方法名冲突。
type Registry struct {
	mu          sync.RWMutex
	bots        []*BotHandle
	judges      []*JudgeHandle
	commentator *CommentatorHandle
	rootCtx     context.Context
	rootStop    context.CancelFunc
}

// NewRegistry 构造空 registry。
func NewRegistry() *Registry {
	ctx, cancel := context.WithCancel(context.Background())
	return &Registry{
		rootCtx:  ctx,
		rootStop: cancel,
	}
}

// Stop 取消所有 Bot + 裁判 + 解说的 ctx。
func (ar *Registry) Stop() {
	ar.mu.Lock()
	defer ar.mu.Unlock()
	for _, b := range ar.bots {
		if b.cancel != nil {
			b.cancel()
		}
	}
	for _, j := range ar.judges {
		if j.cancel != nil {
			j.cancel()
		}
	}
	if ar.commentator != nil && ar.commentator.cancel != nil {
		ar.commentator.cancel()
	}
	if ar.rootStop != nil {
		ar.rootStop()
	}
}

// BotStats 返回每个 Bot 的 Token + API 统计快照(§20260831-09)。
//
// 由 DebateRoom.AggregateAgentStats 调用;返回 AgentTokenSnapshot 列表
// (room → ws → frontend → DebateAgentStatsPanel 渲染)。
func (ar *Registry) BotStats() []debate.AgentTokenSnapshot {
	ar.mu.RLock()
	defer ar.mu.RUnlock()
	out := make([]debate.AgentTokenSnapshot, 0, len(ar.bots))
	for _, b := range ar.bots {
		if b == nil || b.agent == nil {
			continue
		}
		ts := b.agent.AgentTokenStats()
		out = append(out, debate.AgentTokenSnapshot{
			TeamID:         b.TeamID,
			Seat:           b.Seat,
			LLMCallCount:   b.agent.TotalLLMCalls(),
			InputTokens:    ts.TotalInputTokens,
			OutputTokens:   ts.TotalOutputTokens,
			APITokens:      ts.TotalAPITokens,
			APISuccessCount: ts.APISuccessCount,
			APIFailCount:    ts.APIFailCount,
		})
	}
	return out
}

// JudgeStats 返回每个裁判的 Token + API 统计快照(§20260831-09)。
func (ar *Registry) JudgeStats() []debate.JudgeTokenSnapshot {
	ar.mu.RLock()
	defer ar.mu.RUnlock()
	out := make([]debate.JudgeTokenSnapshot, 0, len(ar.judges))
	for _, j := range ar.judges {
		if j == nil || j.judge == nil {
			continue
		}
		ts := j.judge.JudgeTokenStats()
		out = append(out, debate.JudgeTokenSnapshot{
			JudgeID:         j.JudgeID,
			LLMCallCount:    j.judge.TotalLLMCalls(),
			InputTokens:     ts.TotalInputTokens,
			OutputTokens:    ts.TotalOutputTokens,
			APITokens:       ts.TotalAPITokens,
			APISuccessCount: ts.APISuccessCount,
			APIFailCount:    ts.APIFailCount,
		})
	}
	return out
}

// StartAgents 为房间启动所有辩方 Bot + 裁判 + 解说。
//
// 启动顺序:
//  1. 遍历 RoomConfig.Teams → 每队每个 Agent → 启动 debateplayer.Agent
//  2. 注册到 room.agentRegistry(供 Stop + Stats)
//  3. Bot Run goroutine 阻塞监听直到 ctx 取消
//  4. 启动解说 Agent(如有 commentatorModelKey)
func StartAgents(room *debate.DebateRoom, engine *debate.DebateEngine, registry *llm.Registry) *Registry {
	if room == nil || engine == nil {
		return nil
	}
	if registry == nil {
		logger.L().Warn("debate: llm registry is nil, agents will run as placeholders",
			zap.String("room_id", room.RoomID))
	}

	reg := NewRegistry()

	for _, team := range room.Config.Teams {
		for _, ag := range team.Agents {
			a := debateplayer.NewAgent(room, engine, team.TeamID, ag.SeatID,
				ag.Role, team.Stance, ag.ModelKey, registry)
			ctx, cancel := context.WithCancel(reg.rootCtx)
			reg.mu.Lock()
			reg.bots = append(reg.bots, &BotHandle{
				TeamID: team.TeamID, Seat: ag.SeatID, cancel: cancel, agent: a,
			})
			reg.mu.Unlock()
			go a.Run(ctx)
			logger.L().Info("debate bot started",
				zap.String("room_id", room.RoomID),
				zap.Int("team", team.TeamID),
				zap.Int("seat", ag.SeatID),
				zap.String("role", string(ag.Role)),
				zap.String("model", ag.ModelKey))
		}
	}

	// 启动裁判(裁判在 PhaseJudging 才被唤醒,这里只启动常驻)
	for _, j := range room.Config.Judges {
		judge := debatejudge.NewJudge(room, engine, j.JudgeID, j.ModelKey, registry)
		ctx, cancel := context.WithCancel(reg.rootCtx)
		reg.mu.Lock()
		reg.judges = append(reg.judges, &JudgeHandle{JudgeID: j.JudgeID, cancel: cancel, judge: judge})
		reg.mu.Unlock()
		go judge.Run(ctx)
		logger.L().Info("debate judge started",
			zap.String("room_id", room.RoomID),
			zap.Int("judge_id", j.JudgeID),
			zap.String("model", j.ModelKey))
	}

	// 启动解说 Agent(如有 commentatorModelKey)
	commentatorModelKey := room.Manager().CommentatorModelKey()
	if commentatorModelKey != "" && registry != nil {
		commentator := debatecommentator.NewCommentatorAgent(room.RoomID, "pro", commentatorModelKey)
		if p, k, err := registry.Get(commentatorModelKey); err == nil {
			commentator.SetProvider(p, k)
		}
		commentator.SetRegistry(registry)
		commentator.SetLLMSemaphore(room.Manager().LLMSemaChannel())
		commentator.SetOnBroadcast(room.Manager().CommentatorBroadcast())
		ctx, cancel := context.WithCancel(reg.rootCtx)
		reg.mu.Lock()
		reg.commentator = &CommentatorHandle{cancel: cancel}
		reg.mu.Unlock()
		go commentator.Run(ctx, room.Manager().CommentatorSnapProvider(room))
		logger.L().Info("debate commentator started",
			zap.String("room_id", room.RoomID),
			zap.String("model", commentatorModelKey))
	}

	return reg
}
