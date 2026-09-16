// Package wealthplayer — run.go: 月度决策循环(2026-09-14 §财商流P0)。
//
// 契约: Agent 设计文档 §6。月度事件驱动:OnMonthStart(ctx GameContext) 由
// game/wealth/agent_runner.go 在每 acting 广播后异步调用,经房间信号量
// (默认 4)限并发,单 Agent 决策 20s 超时。
package wealthplayer

import (
	"context"
	"fmt"
	"runtime/debug"
	"strings"
	"time"

	"LsmAgentGame/agent/wealthtypes"
	"LsmAgentGame/errcode"
	llmtypes "LsmAgentGame/llm/types"
	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// ActionCount 限制常量(从 cfg.BotMaxActionsPerMonth 注入)。
func (a *Agent) actionsLimit() int {
	if a.maxActions <= 0 {
		return 3
	}
	return a.maxActions
}

// BuildLLMRequest 构造 LLMRequest(系统块来自 SystemPromptBlocks;messages
// 累积在 caller OnMonthStart 内部循环)。
func (a *Agent) BuildLLMRequest(sysBlocks []llmtypes.SystemBlock, userText string, tools []llmtypes.ToolDef) *llmtypes.LLMRequest {
	return &llmtypes.LLMRequest{
		Model:           a.ModelKey,
		System:          sysBlocks,
		Messages:        []llmtypes.Message{{Role: "user", Content: []llmtypes.ContentBlock{{Type: "text", Text: userText}}}},
		Tools:           tools,
		MaxTokens:       4096,
		Metadata:        llmtypes.Metadata{UserID: a.MyUserID},
		AgentClassName:  string(a.AgentClass()),
	}
}

// toolResultContentBlock 构造 tool_result content-block(Anthropic wire,§14.1)。
func toolResultContentBlock(toolUseID string, text string, isErr bool) llmtypes.ContentBlock {
	return llmtypes.ContentBlock{
		Type:      "tool_result",
		ToolUseID: toolUseID,
		Content:   []llmtypes.ContentBlock{{Type: "text", Text: text}},
		IsError:   isErr,
	}
}

// toolUseBlocks 从 LLM 响应里提取所有 tool_use 块。
func toolUseBlocks(resp llmtypes.LLMResponse) []llmtypes.ContentBlock {
	var out []llmtypes.ContentBlock
	for _, b := range resp.Content {
		if b.Type == "tool_use" {
			out = append(out, b)
		}
	}
	return out
}

// textConcatenate 拼接 text 块(text 首段取前 80 字作 last_decision_summary)。
func textConcatenate(resp llmtypes.LLMResponse) string {
	var sb strings.Builder
	for _, b := range resp.Content {
		if b.Type == "text" && b.Text != "" {
			sb.WriteString(b.Text)
		}
	}
	return sb.String()
}

// maxRunes 限长截断。
func maxRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// OnMonthStart 月度决策循环入口(caller 负责房间信号量)。
// 调用方在每 acting 窗口开始后异步调用;ctx 携带 decisionTimeout。
func (a *Agent) OnMonthStart(parent context.Context, ctx *wealthtypes.GameContext) {
	// 2026-09-15 §财商流P0-bugfix: 单 bot 任何 panic 都兜底 submit + 摘要,
	// 避免 LLM provider 异常或 typed-nil 接口穿透导致整个房间锁住 / 服务崩溃。
	defer func() {
		if r := recover(); r != nil {
			logger.L().Error("wealthplayer OnMonthStart panic recovered",
				zap.Int("seat", a.MySeat),
				zap.Any("panic", r),
				zap.String("stack", string(debug.Stack())),
			)
			if a.runner != nil {
				if err := a.runner.SubmitMonth(a.MySeat); err != nil {
					logger.L().Warn("wealthplayer recover: submit_month fallback failed",
						zap.Int("seat", a.MySeat),
						zap.Error(err),
					)
				}
			}
			a.finalizeTranscript("", "", "panic recovered; 强制提交")
		}
	}()
	if a.IsCancelled() || a.runner == nil {
		return
	}
	a.mu.Lock()
	a.transcript = BotTranscript{}
	a.spokenThisMonth = false
	a.mu.Unlock()

	// context with timeout(§197 字节刷新)。
	timeout := a.decisionTimeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	cctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	// 1. 取 system blocks + 初始 user prompt。
	sysBlocks := SystemPromptBlocks(ctx.MyCard)
	userText := UserPrompt(ctx, a.memory.RenderForPrompt())
	tools := BuildTools()

	messages := []llmtypes.Message{
		{Role: "user", Content: []llmtypes.ContentBlock{{Type: "text", Text: userText}}},
	}

	actionsUsed := 0
	actionsLimit := a.actionsLimit()
	speakUsed := false
	lastSummary := ""
	lastToolInput := ""
	lastToolResult := ""

	// 2. 工具循环 ≤3 轮(或直至 submit_month / 超时)。
	for round := 0; round < 3; round++ {
		req := a.BuildLLMRequest(sysBlocks, "", tools)
		req.Messages = messages

		// 流式回调:每收到一个 SSE event 即刷新 timeout(§197)。
		progress := func(_ llmtypes.StreamEvent) error {
			// 用零开销回调唤醒 watchdog:此处简化,实际字节流由 provider 内部
			// streaming transport 处理;reader 短读不会产生 idle timeout。
			_ = cctx
			return nil
		}

		var resp llmtypes.LLMResponse
		var err error
		if cctx.Err() != nil {
			a.recordTimeoutSummary()
			return
		}
		resp, err = a.callProvider(cctx, *req, progress)
		if err != nil {
			if cctx.Err() != nil {
				a.recordTimeoutSummary()
			} else {
				logger.L().Warn("wealthplayer callProvider error",
					zap.String("room_id", a.RoomID), zap.Int("seat", a.MySeat), zap.Error(err))
				a.mu.Lock()
				a.transcript.LastDecisionSummary = "思考超时,自动提交本月"
				a.mu.Unlock()
			}
			break
		}
		// 仅保留最后一段 text 作为本轮决策摘要。
		rawText := textConcatenate(resp)
		if rawText != "" {
			lastSummary = maxRunes(rawText, 120)
		}

		// 解析 tool_use。
		tus := toolUseBlocks(resp)
		if len(tus) == 0 {
			// 无 tool_use → 跳出循环,强制 submit。
			break
		}

		// 第一轮就提交 → 不耗预算(§9)。
		if round == 0 && len(tus) == 1 && tus[0].Name == ToolSubmitMonth {
			_ = a.runner.SubmitMonth(a.MySeat)
			lastToolInput, lastToolResult = "{}", "已提交"
			break
		}

		// 处理每个 tool_use:dispatch + tool_result 回喂。
		results := make([]llmtypes.ContentBlock, 0, len(tus))
		for _, tu := range tus {
			res := a.DispatchTool(tu.Name, tu.Input)
			// 累计动作计数。
			if isBudgetAction(tu.Name) {
				actionsUsed++
			}
			if tu.Name == ToolSpeak {
				speakUsed = true
			}
			if tu.Name == ToolSubmitMonth {
				// 不计预算;结束循环。
				results = append(results, toolResultContentBlock(tu.ID, res.Text, res.IsErr))
				lastToolInput, lastToolResult = res.Input, res.Text
				a.appendMessages(&messages, resp.Content, results)
				a.finalizeTranscript(lastSummary, lastToolInput, lastToolResult)
				return
			}
			results = append(results, toolResultContentBlock(tu.ID, res.Text, res.IsErr))
			lastToolInput = res.Input
			lastToolResult = res.Text
			if actionsUsed >= actionsLimit && isBudgetAction(tu.Name) {
				// 超预算:本工具被拒;直接给 submit 提示,下一轮结束。
				a.finalizeTranscript(lastSummary, lastToolInput, lastToolResult)
				a.appendMessages(&messages, resp.Content, results)
				// 主动结束:不再加 tool_use,强制提交。
				break
			}
			if speakUsed && tu.Name == ToolSpeak {
				// speak 已用过;记下结果,继续。
			}
		}
		a.appendMessages(&messages, resp.Content, results)
		// 若本轮全为非动作(纯 speak/check_state)且已用完,仍然 break;
		// 我们用 actionsUsed 兜底,actionsUsed 不再增长则安全。
		if actionsUsed > actionsLimit {
			break
		}
		// 若本轮只有一个非 submit 的 tool_use 且已 saturate,跳出。
		if actionsUsed >= actionsLimit {
			break
		}
	}
	// 3. 默认 submit(超时 / LLM 失败 / 全程无 tool_use / 预算耗尽)。
	_ = a.runner.SubmitMonth(a.MySeat)
	a.finalizeTranscript(lastSummary, lastToolInput, lastToolResult)
}

// isBudgetAction 是否耗动作预算(check_state / submit_month / view_listings 不耗)。
func isBudgetAction(name string) bool {
	switch name {
	case ToolCheckState, ToolSubmitMonth, ToolViewListings:
		return false
	}
	return true
}

// appendMessages 把 assistant + tool_result 回合追加到消息流。
// 2026-09-14 §财商流P0-bugfix: 必须携带原始 assistant content(含 tool_use
// 块)——旧实现只追加 user 态 tool_result,assistant 段的 tool_use 从未入流,
// 下一轮请求中 tool_result.tool_use_id 在上游找不到对应 tool_use →
// Anthropic 协议 400 "tool result's tool id not found"(CLAUDE.md §14.1:
// messages 严格 user/assistant 交替 + tool_use/tool_result 成对)。
func (a *Agent) appendMessages(messages *[]llmtypes.Message, assistantContent []llmtypes.ContentBlock, results []llmtypes.ContentBlock) {
	if len(assistantContent) > 0 {
		*messages = append(*messages, llmtypes.Message{
			Role: "assistant", Content: assistantContent,
		})
	}
	if len(results) == 0 {
		return
	}
	// 把 tool_result 段放进 user 消息,与上轮 assistant 衔接。
	*messages = append(*messages, llmtypes.Message{
		Role: "user", Content: results,
	})
}

// recordTimeoutSummary 写决策摘要为"timeout"。
func (a *Agent) recordTimeoutSummary() {
	a.mu.Lock()
	a.transcript.LastDecisionSummary = "timeout"
	a.mu.Unlock()
	_ = a.runner.SubmitMonth(a.MySeat)
}

// finalizeTranscript 写入决策摘要 + 最近 tool 细节;追加 Memory。
func (a *Agent) finalizeTranscript(summary, toolInput, toolResult string) {
	a.mu.Lock()
	a.transcript.LastDecisionSummary = summary
	a.transcript.LastToolInput = toolInput
	a.transcript.LastToolResult = toolResult
	a.mu.Unlock()
	if a.memory != nil {
		a.memory.AppendDecision(MonthDecision{
			Month:       -1, // 房间侧在月结时修正
			Actions:     nil,
			Summary:     summary,
			CashAfter:   -1,
			NetAfter:    -1,
			FI:          -1,
		})
	}
}

// SuppressUnusedImport 占位(避免编译告警)。
var _ = fmt.Sprintf
var _ = errcode.ErrWealthActionBudgetExhausted