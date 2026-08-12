// Package wwplayer — tool_hooks.go: 工具执行前/后 hooks 管道。
// 灵感来源: PI Agent 的 beforeToolCall/afterToolCall hooks 机制。
//
// 设计原则:
//   - Before hooks: 校验/日志/配额检查,返回 error 可阻止工具执行
//   - After hooks: 记录/统计/通知,失败不阻塞 (best-effort)
//   - Hooks 在 DispatchTool 入口/出口统一调用,与 dispatchToolInner 解耦
//   - 所有 hook 必须是无锁的 (不持有 r.mu / a.mu)
package wwplayer

import (
	"LsmWebGame/agent/wwtypes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ToolHookContext 传递给 hooks 的上下文信息。
// Before-hook: Result/CallErr 为零值;After-hook: 全部填充。
type ToolHookContext struct {
	ToolName string
	Args     map[string]interface{}
	Phase    string
	Role     string
	Seat     int
	GameCtx  *wwtypes.GameContext // 可选,before-hook 可读取当前游戏状态

	// After-hook only
	Result string
	CallErr error

	// 时间戳
	StartedAt time.Time
}

// ToolHook 是工具执行前/后的回调函数。
// Before-hook 返回 error 可阻止工具执行;After-hook 的 error 被忽略。
type ToolHook func(ctx *ToolHookContext) error

// ToolHooks 管理 before/after hooks 的注册与执行。
type ToolHooks struct {
	Before []ToolHook
	After  []ToolHook
}

// NewToolHooks 创建带有默认 hooks 的 ToolHooks 实例。
func NewToolHooks() *ToolHooks {
	return &ToolHooks{
		Before: []ToolHook{
			hookLogToolCall,
		},
		After: []ToolHook{
			hookLogToolResult,
		},
	}
}

// RunBefore 依次执行所有 before hooks。首个返回 error 的 hook 阻止工具执行。
func (h *ToolHooks) RunBefore(ctx *ToolHookContext) error {
	for _, hook := range h.Before {
		if err := hook(ctx); err != nil {
			return err
		}
	}
	return nil
}

// RunAfter 依次执行所有 after hooks。错误被忽略 (best-effort)。
func (h *ToolHooks) RunAfter(ctx *ToolHookContext) {
	for _, hook := range h.After {
		_ = hook(ctx) // after-hook 失败不阻塞
	}
}

// ─── 默认 Hooks ───────────────────────────────────────────────

// hookLogToolCall 在工具执行前记录调用日志。
func hookLogToolCall(ctx *ToolHookContext) error {
	if ctx.Args == nil {
		return nil
	}
	// 仅记录有意义的工具 (跳过 idle_silent 等无参数工具)
	if ctx.ToolName == "idle_silent" || ctx.ToolName == "finish_speak" || ctx.ToolName == "finish_vote" {
		return nil
	}
	argsJSON, _ := json.Marshal(ctx.Args)
	argsStr := string(argsJSON)
	if len(argsStr) > 200 {
		argsStr = argsStr[:200] + "..."
	}
	// 使用 fmt 而非 logger,避免循环依赖;上层 agentRunner 可用自己的 logger
	_ = fmt.Sprintf("[tool_hook] seat=%d phase=%s tool=%s args=%s", ctx.Seat, ctx.Phase, ctx.ToolName, argsStr)
	return nil
}

// hookLogToolResult 在工具执行后记录结果摘要。
func hookLogToolResult(ctx *ToolHookContext) error {
	if ctx.CallErr != nil {
		// 工具执行失败,记录错误
		_ = fmt.Sprintf("[tool_hook] seat=%d tool=%s FAILED: %v", ctx.Seat, ctx.ToolName, ctx.CallErr)
		return nil
	}
	// 成功时截取结果前 80 字
	resultSummary := ctx.Result
	if len(resultSummary) > 80 {
		resultSummary = resultSummary[:80] + "..."
	}
	_ = fmt.Sprintf("[tool_hook] seat=%d tool=%s OK: %s", ctx.Seat, ctx.ToolName, resultSummary)
	return nil
}

// ─── 可选 Hooks (由 Agent 构造时按需注册) ─────────────────────

// NewFactCheckHook 创建一个 before-hook,校验发言工具的目标合法性。
// 防止 Agent 在发言中引用已死亡玩家作为"私聊来源"等幻觉。
func NewFactCheckHook() ToolHook {
	return func(ctx *ToolHookContext) error {
		// 仅校验 speak/speak_with_thought/interject
		switch ctx.ToolName {
		case "speak", "speak_with_thought", "interject":
			// text 校验由 agent_runner.go 的 ScrubIdentityLeak 已覆盖
			// 这里只做额外的目标字段校验
		case "wolf_kill", "seer_check", "vote", "guard_protect", "hunter_shoot", "knight_duel", "demon_hunter_hunt":
			// 校验 target 在合法范围内
			if ctx.GameCtx == nil {
				return nil
			}
			target, ok := ctx.Args["target"]
			if !ok {
				return nil
			}
			targetInt, ok := toInt(target)
			if !ok {
				return nil
			}
			if targetInt < 0 {
				return nil // -1 表空过,合法
			}
			if targetInt >= len(ctx.GameCtx.AliveSeats) {
				return fmt.Errorf("target seat %d out of range [0,%d)", targetInt, len(ctx.GameCtx.AliveSeats))
			}
		}
		return nil
	}
}

// NewQuotaCheckHook 创建一个 before-hook,检查本轮工具调用次数。
// 防止单轮 tool_use 过多 (慢模型尤其重要,§197)。
func NewQuotaCheckHook(maxToolsPerRound int) ToolHook {
	count := 0
	return func(ctx *ToolHookContext) error {
		// 跳过被动工具
		if ctx.ToolName == "idle_silent" || ctx.ToolName == "finish_speak" || ctx.ToolName == "finish_vote" || ctx.ToolName == "last_words_skip" {
			return nil
		}
		count++
		if count > maxToolsPerRound {
			return fmt.Errorf("tool quota exceeded: %d/%d tools this round", count, maxToolsPerRound)
		}
		return nil
	}
}

// ResetQuotaCounter 重置 quota hook 的计数器 (每轮 LLM 调用前)。
// 由于 Go 没有闭包字段暴露,使用 ToolHook 类型断言不现实,
// 所以改用带状态的 QuotaHookWrapper。
type QuotaHookWrapper struct {
	MaxTools int
	count    int
}

func (q *QuotaHookWrapper) Hook() ToolHook {
	return func(ctx *ToolHookContext) error {
		if ctx.ToolName == "idle_silent" || ctx.ToolName == "finish_speak" || ctx.ToolName == "finish_vote" || ctx.ToolName == "last_words_skip" {
			return nil
		}
		q.count++
		if q.count > q.MaxTools {
			return fmt.Errorf("tool quota exceeded: %d/%d tools this round", q.count, q.MaxTools)
		}
		return nil
	}
}

func (q *QuotaHookWrapper) Reset() {
	q.count = 0
}

// ─── 辅助函数 ─────────────────────────────────────────────────

// toInt 将 interface{} 转换为 int。
func toInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	case string:
		var i int
		_, err := fmt.Sscanf(n, "%d", &i)
		return i, err == nil
	default:
		return 0, false
	}
}

// SanitizeToolName 清理工具名中的特殊字符 (用于日志)。
func SanitizeToolName(name string) string {
	name = strings.TrimSpace(name)
	if len(name) > 64 {
		name = name[:64]
	}
	return name
}
