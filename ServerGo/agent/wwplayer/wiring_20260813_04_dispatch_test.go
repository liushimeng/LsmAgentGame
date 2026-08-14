package wwplayer_test

import (
	"errors"
	"testing"

	"LsmAgentGame/agent/wwplayer"
)

// §20260813-04 U2 —— ToolHooks 在 dispatch 层的语义断言。
//
// 放在外部测试包（wwplayer_test）以复用 agent_test.go 已有的 fakeRunner
// 完整 ToolRunner 桩 —— 避免为几个断言重写 30 个方法。
//
// 三条断言对应 tool_hooks.go 文件头声明的三条设计原则：
//
//	Before hooks: 返回 error 可阻止工具执行
//	After  hooks: 失败不阻塞（best-effort）
//	nil hooks:    走原路径（回归保护 —— U2 之前 tools.go:846 写死 nil）

var errTestHookBlocked = errors.New("blocked by test hook")

// TestU2Dispatch_BeforeHookBlocksExecution 断言 before-hook 返回 error 时
// 工具**不被执行**（而非执行后丢弃结果）。
func TestU2Dispatch_BeforeHookBlocksExecution(t *testing.T) {
	hooks := &wwplayer.ToolHooks{
		Before: []wwplayer.ToolHook{
			func(ctx *wwplayer.ToolHookContext) error { return errTestHookBlocked },
		},
	}
	runner := &fakeRunner{}

	_, err := wwplayer.DispatchToolWithHooks("vote_skip", map[string]any{}, runner, hooks)
	if err == nil {
		t.Fatal("before-hook 返回 error 时应返回 error")
	}
	for _, c := range runner.calls {
		if c == "vote" {
			t.Fatal("before-hook 阻止后 Vote 仍被调用 —— 校验形同虚设")
		}
	}
}

// TestU2Dispatch_AfterHookErrorIgnored 断言 after-hook 的 error 被忽略。
// after-hook 是记录/统计/通知，失败不能回滚已执行的工具。
func TestU2Dispatch_AfterHookErrorIgnored(t *testing.T) {
	var afterRan bool
	hooks := &wwplayer.ToolHooks{
		After: []wwplayer.ToolHook{
			func(ctx *wwplayer.ToolHookContext) error {
				afterRan = true
				return errTestHookBlocked
			},
		},
	}
	runner := &fakeRunner{}

	_, err := wwplayer.DispatchToolWithHooks("vote_skip", map[string]any{}, runner, hooks)
	if err != nil {
		t.Fatalf("after-hook 的 error 应被忽略，却传播出来: %v", err)
	}
	if !afterRan {
		t.Fatal("after-hook 未被执行")
	}
}

// TestU2Dispatch_NilHooksMatchesLegacy 断言 nil hooks 与 DispatchTool 等价。
// 这是 U2 的回归保护：修复不能改变「未注入 hooks」的既有语义。
func TestU2Dispatch_NilHooksMatchesLegacy(t *testing.T) {
	r1, r2 := &fakeRunner{}, &fakeRunner{}

	withNil, errNil := wwplayer.DispatchToolWithHooks("vote_skip", map[string]any{}, r1, nil)
	viaLegacy, errLegacy := wwplayer.DispatchTool("vote_skip", map[string]any{}, r2)

	if withNil != viaLegacy {
		t.Errorf("nil hooks 结果与 DispatchTool 不一致: %q vs %q", withNil, viaLegacy)
	}
	if (errNil == nil) != (errLegacy == nil) {
		t.Errorf("nil hooks 错误态不一致: %v vs %v", errNil, errLegacy)
	}
	if len(r1.calls) != len(r2.calls) {
		t.Errorf("nil hooks 与 DispatchTool 的 runner 调用序列不一致: %v vs %v", r1.calls, r2.calls)
	}
}
