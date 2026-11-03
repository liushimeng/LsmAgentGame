// Package citysystem — agent.go: City-System 主 Agent 构造与 ReAct 主循环
// (2026-11-03 §批次81)。
//
// 契约: 批次81 方案文档 §3.2「ReAct 主循环」。
//
//	人类自然语言 → system(角色段+场景目录) → ReAct(≤MaxSteps) →
//	工具派发 → 流式事件(agent_start/text/thinking/tool_start/tool_result/
//	show_command/report/agent_end)→ 会话持久化
//
// LLM 调用走 ChatStreamAccumulate(§197 流式续命)+ AgentClassName(§24);
// 节流经注入的 acquireLLM(复用项目 llmSema 模式)。
package citysystem

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	agentroot "LsmAgentGame/agent"
	"LsmAgentGame/agent/citybus"
	"LsmAgentGame/game/virtual_city"
	llmtypes "LsmAgentGame/llm/types"
)

// Agent 是虚拟城市系统主 Agent(每房间一个实例,由 WS 层持有)。
type Agent struct {
	// RoomID / ModelKey 为静态配置。
	RoomID   string
	ModelKey string

	room     *virtual_city.VirtualCityRoom
	registry LLMRegistry
	hub      *citybus.Hub
	store    *ConversationStore
	// emit 事件流出(WS 层注入);nil 时 NewAgent 置为 no-op。
	emit func(SystemEvent)

	// cfg 运行参数(零值已由 NewAgent 归一)。
	cfg Config

	// lineMu 保护 linePolicy:建房链路写入 / Run 读取(读多写少)。
	lineMu     sync.RWMutex
	linePolicy string

	// acquireLLM 是 LLM 并发节流槽(复用 llmSema 模式);nil = 不节流。
	acquireLLM func(ctx context.Context) (release func(), ok bool)
}

// NewAgent 构造。cfg 零值归一:MaxSteps 8 / MaxMessages 40 /
// RequestTimeout 180s / ToolResultLimit 4000。
func NewAgent(room *virtual_city.VirtualCityRoom, cfg Config, reg LLMRegistry,
	hub *citybus.Hub, store *ConversationStore, emit func(SystemEvent)) *Agent {
	if cfg.MaxSteps <= 0 {
		cfg.MaxSteps = 8
	}
	if cfg.MaxMessages <= 0 {
		cfg.MaxMessages = 40
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = 180 * time.Second
	}
	if cfg.ToolResultLimit <= 0 {
		cfg.ToolResultLimit = 4000
	}
	if store == nil {
		store = NewConversationStore(cfg.MaxMessages)
	}
	if emit == nil {
		emit = func(SystemEvent) {}
	}
	roomID := ""
	if room != nil {
		roomID = room.RoomID
	}
	return &Agent{
		RoomID:   roomID,
		ModelKey: cfg.ModelKey,
		room:     room,
		registry: reg,
		hub:      hub,
		store:    store,
		emit:     emit,
		cfg:      cfg,
	}
}

// SetLLMAcquire 注入 LLM 节流槽(装配层;nil = 不节流)。
func (a *Agent) SetLLMAcquire(fn func(ctx context.Context) (release func(), ok bool)) {
	a.acquireLLM = fn
}

// Reset 清空指定用户在本房的会话(前端「清空对话」按钮)。
func (a *Agent) Reset(userID string) {
	a.store.Reset(a.RoomID + "/" + userID)
}

// NewTraceID 生成跨 Agent 调用链标识(显示指令 / 事件流共用)。
func NewTraceID() string {
	return fmt.Sprintf("tr-%d", time.Now().UnixNano())
}

// Run 执行一轮对话:追加 user 消息 → ReAct 主循环 → 持久化 assistant 文本。
//
// 并发:不同用户可在同一 Agent 上并发 Run(WS 层每用户限流 1);
// 操作者身份 **userID 随调用链显式传递**(不在 Agent 上共享字段),
// control_simulation 的房主校验因此不会被并发轮次串号。
//
// 返回 error 仅表示**对话未能完成**(LLM 失败 / 上下文异常);工具级失败
// 已作为 isErr 结果回灌模型,不在此返回。
func (a *Agent) Run(ctx context.Context, userID, text string) error {
	if a.room == nil {
		return fmt.Errorf("citysystem: room not bound")
	}
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("citysystem: empty user message")
	}
	key := a.RoomID + "/" + userID

	// 1. 取会话上下文 + 追加 user 消息。
	a.store.Append(key, llmtypes.Message{
		Role:    "user",
		Content: []llmtypes.ContentBlock{{Type: "text", Text: text}},
	})
	messages := a.store.Get(key)

	// 2. system(角色段 + 场景目录 + 三段式标准头)。
	system := BuildSystemBlocks(a.cfg.MaxSteps)

	// 3. 事件:开始。
	a.emit(SystemEvent{Kind: EventAgentStart, Data: map[string]any{
		"user_id": userID, "text": text,
	}})

	rctx, cancel := context.WithTimeout(ctx, a.cfg.RequestTimeout)
	defer cancel()

	// 4. ReAct 主循环。
	finalText, newMsgs, err := a.runLoop(rctx, userID, system, messages)

	// 5. 持久化本轮新增消息(含最终 assistant 文本)。
	if len(newMsgs) > 0 {
		a.store.Append(key, newMsgs...)
	}
	if finalText != "" {
		a.store.Append(key, llmtypes.Message{
			Role:    "assistant",
			Content: []llmtypes.ContentBlock{{Type: "text", Text: finalText}},
		})
	}

	// 6. 事件:结束(异常时先补一条 error)。
	if err != nil {
		a.emit(SystemEvent{Kind: EventError, Data: map[string]string{"message": err.Error()}})
	}
	a.emit(SystemEvent{Kind: EventAgentEnd, Data: map[string]any{"text": finalText}})
	return err
}

// runLoop 是 ReAct 主循环:返回 (最终 assistant 文本, 本轮新增消息, 错误)。
//
// 消息交替纪律(§14.1):assistant(tool_use) 与 user(tool_result) 成对追加,
// 严格 user/assistant 交替;任一轮出错则**整体不落地**(调用方不持久化),
// 避免会话里留下没有 assistant 结尾的悬空 tool_result。
//
// 收敛纪律:每个工具结果带 `[step i/N]` 进度前缀(与 citychrome 同款);
// 步数用尽时补一条收尾说明,保证会话以 assistant 结束(下一轮 user 不与
// 悬空 tool_result 相邻)。
func (a *Agent) runLoop(ctx context.Context, userID string,
	system []llmtypes.SystemBlock, messages []llmtypes.Message) (string, []llmtypes.Message, error) {
	var (
		finalText string
		newMsgs   []llmtypes.Message
	)
	for step := 0; step < a.cfg.MaxSteps; step++ {
		if ctx.Err() != nil {
			return finalText, nil, fmt.Errorf("citysystem: context done: %w", ctx.Err())
		}
		resp, err := a.callLLM(ctx, system, messages)
		if err != nil {
			return finalText, nil, fmt.Errorf("citysystem: llm step %d: %w", step, err)
		}
		if t := resp.Text(); t != "" {
			finalText = t
		}
		tus := resp.ToolUses()
		if len(tus) == 0 {
			// 模型直接文本作答:结束循环。
			return finalText, newMsgs, nil
		}
		results := make([]llmtypes.ContentBlock, 0, len(tus))
		for _, tu := range tus {
			inputJSON, _ := json.Marshal(tu.Input)
			a.emit(SystemEvent{Kind: EventToolStart, Data: ToolCallRecord{
				Tool: tu.Name, Input: string(inputJSON), Ts: time.Now().UnixMilli(),
			}})
			text, isErr := a.Dispatch(ctx, userID, tu.Name, tu.Input)
			// [step i/N] 进度前缀(促收敛):先截断再加前缀,前缀恒完整。
			text = fmt.Sprintf("[step %d/%d] %s",
				step+1, a.cfg.MaxSteps, truncateRunes(text, a.cfg.ToolResultLimit))
			a.emit(SystemEvent{Kind: EventToolResult, Data: ToolCallRecord{
				Tool: tu.Name, Input: string(inputJSON), Text: text,
				IsErr: isErr, Ts: time.Now().UnixMilli(),
			}})
			results = append(results, llmtypes.ContentBlock{
				Type:      "tool_result",
				ToolUseID: tu.ID,
				Content:   []llmtypes.ContentBlock{{Type: "text", Text: text}},
				IsError:   isErr,
			})
		}
		// assistant(tool_use) + user(tool_result) 成对回灌。
		assistantMsg := llmtypes.Message{Role: "assistant", Content: resp.Content}
		userMsg := llmtypes.Message{Role: "user", Content: results}
		messages = append(messages, assistantMsg, userMsg)
		newMsgs = append(newMsgs, assistantMsg, userMsg)
	}
	// 步数用尽:补收尾说明,保证会话以 assistant 结束。
	if finalText == "" {
		finalText = fmt.Sprintf("已达到单轮最大步数 %d 步,工具调用停止;请基于以上结果总结,或继续追问。", a.cfg.MaxSteps)
	} else {
		finalText += fmt.Sprintf("\n\n(已达到单轮最大步数 %d 步,工具调用停止)", a.cfg.MaxSteps)
	}
	return finalText, newMsgs, nil
}

// callLLM 单步 LLM 调用:出站消息修整(sanitizeForWire,§14.1)+ 节流槽 +
// thinking 注入 + ChatStreamAccumulate(§197)。
// text_delta / thinking_delta 经 emit 流式下发(前端打字机)。
func (a *Agent) callLLM(ctx context.Context, system []llmtypes.SystemBlock,
	messages []llmtypes.Message) (llmtypes.LLMResponse, error) {
	if a.registry == nil {
		return llmtypes.LLMResponse{}, fmt.Errorf("citysystem: llm registry not bound")
	}
	messages = sanitizeForWire(messages) // 出站唯一闸口:交替 + 配对 + 块白名单
	modelKey := a.resolveLineKey()
	if a.acquireLLM != nil {
		release, ok := a.acquireLLM(ctx)
		if !ok {
			return llmtypes.LLMResponse{}, fmt.Errorf("citysystem: llm acquire timeout")
		}
		defer release()
	}
	provider, key, err := a.registry.Get(modelKey)
	if err != nil {
		return llmtypes.LLMResponse{}, fmt.Errorf("citysystem: registry.Get(%s): %w", modelKey, err)
	}
	req := llmtypes.LLMRequest{
		Model:          modelKey,
		System:         system,
		Messages:       messages,
		Tools:          BuildTools(),
		MaxTokens:      4096,
		AgentClassName: string(agentroot.AgentClassCitySystem),
	}
	if enabled, budget := a.registry.GetThinkingEnabled(modelKey); enabled {
		req.Thinking = &llmtypes.ThinkingConfig{Type: "enabled", BudgetTokens: budget}
	}
	onProgress := func(ev llmtypes.StreamEvent) error {
		if ev.Type != "content_block_delta" {
			return nil
		}
		switch ev.DeltaType {
		case "text_delta":
			a.emit(SystemEvent{Kind: EventText, Data: map[string]string{"delta": ev.Delta}})
		case "thinking_delta":
			a.emit(SystemEvent{Kind: EventThinking, Data: map[string]string{"delta": ev.Delta}})
		}
		return nil
	}
	type streamingProvider interface {
		ChatStreamAccumulate(context.Context, string, llmtypes.LLMRequest,
			func(llmtypes.StreamEvent) error) (llmtypes.LLMResponse, error)
	}
	if sp, ok := provider.(streamingProvider); ok {
		return sp.ChatStreamAccumulate(ctx, key, req, onProgress)
	}
	return provider.Chat(ctx, key, req)
}

// truncateRunes 按 rune 截断(工具结果回灌上限,防超长上下文)。
func truncateRunes(s string, limit int) string {
	if limit <= 0 || len([]rune(s)) <= limit {
		return s
	}
	return string([]rune(s)[:limit]) + "…(已截断)"
}
