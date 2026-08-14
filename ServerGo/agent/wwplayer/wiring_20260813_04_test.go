package wwplayer

import (
	"strings"
	"testing"
)

// §20260813-04 U1/U2/U3 回归测试。
//
// 三项都是 §130「声明了却从不接线」第七次复发的修复：
//
//	U1  steeringQueue      完整实现(149 行) + run.go 读取，但零 setter → 恒 nil
//	U2  toolHooks          零读取零 setter，tools.go 写死 nil → 管道从未执行
//	U3  difficultyRoundCap difficulty.go 4 赋值 + agent 侧 0 读取 → 难度档位无效
//
// 每组测试都包含「未修复时会失败」的断言 —— 即直接断言接线后的行为，
// 而非只测被接线组件自身的功能（§20260811-08 教训 5：
// 只测转换函数、不测转换结果，等于没测）。

// ---------------------------------------------------------------------------
// U1 — SteeringQueue 接线
// ---------------------------------------------------------------------------

// TestU1_SteeringQueueSetterRoundTrip 断言 setter/getter 往返可用。
// 未修复前 SetSteeringQueue / SteeringQueue 方法不存在（编译失败）。
func TestU1_SteeringQueueSetterRoundTrip(t *testing.T) {
	a := &Agent{}
	if got := a.SteeringQueue(); got != nil {
		t.Fatalf("零值 Agent 的 SteeringQueue 应为 nil，得到 %v", got)
	}

	q := NewSteeringQueue(agentSteerTestCap)
	a.SetSteeringQueue(q)
	if got := a.SteeringQueue(); got != q {
		t.Fatalf("SteeringQueue 未返回注入的队列")
	}

	// 传 nil 显式关闭
	a.SetSteeringQueue(nil)
	if got := a.SteeringQueue(); got != nil {
		t.Fatalf("SetSteeringQueue(nil) 后应返回 nil，得到 %v", got)
	}
}

// TestU1_SteeringQueueDrainFormatsByKind 断言四类事件各有独立前缀。
// 这是 run.go:685 DrainAndFormat 注入到 user prompt 的实际文本。
func TestU1_SteeringQueueDrainFormatsByKind(t *testing.T) {
	q := NewSteeringQueue(agentSteerTestCap)
	q.Enqueue(AgentSteerMsg{Kind: SteerPropHit, Content: "你被 markdown_bomb 击中"})
	q.Enqueue(AgentSteerMsg{Kind: SteerSpectatorInquiry, Content: "3号为什么改票"})
	q.Enqueue(AgentSteerMsg{Kind: SteerWhisper, Content: "队友让你别刀4号"})
	q.Enqueue(AgentSteerMsg{Kind: SteerPhaseHint, Content: "投票即将结束"})

	got := q.DrainAndFormat()
	for _, want := range []string{"【道具影响】", "【观众提问】", "【私聊到达】", "【阶段提示】"} {
		if !strings.Contains(got, want) {
			t.Errorf("DrainAndFormat 输出缺少前缀 %q，实际输出:\n%s", want, got)
		}
	}
	// drain 后队列应为空（避免同一事件被重复注入 prompt）
	if n := q.Len(); n != 0 {
		t.Errorf("DrainAndFormat 后队列应清空，仍有 %d 条", n)
	}
	if again := q.DrainAndFormat(); again != "" {
		t.Errorf("二次 DrainAndFormat 应返回空串，得到 %q", again)
	}
}

// TestU1_CloseSteeringQueueNilsBeforeClose 断言 Close 先置 nil 再 close。
//
// 顺序不可颠倒：若先 close 再置 nil，并发的 SteeringQueue() 调用方会拿到
// 已关闭的 channel，Enqueue 时向 closed channel 发送 → panic。
func TestU1_CloseSteeringQueueNilsBeforeClose(t *testing.T) {
	a := &Agent{}
	a.SetSteeringQueue(NewSteeringQueue(agentSteerTestCap))

	a.CloseSteeringQueue()

	if got := a.SteeringQueue(); got != nil {
		t.Fatalf("CloseSteeringQueue 后字段应为 nil（否则调用方可能向 closed channel 发送），得到 %v", got)
	}
	// 幂等：二次调用不 panic
	a.CloseSteeringQueue()
}

// TestU1_NilQueueIsNoOp 断言未注入队列时 drain 路径安全。
// 这是回归保护：U1 之前所有 Agent 的 steeringQueue 都是 nil，
// 修复不能让「未注入」变成 panic。
func TestU1_NilQueueIsNoOp(t *testing.T) {
	a := &Agent{}
	// 模拟 run.go:684 的守卫形状
	if q := a.SteeringQueue(); q != nil {
		t.Fatal("零值 Agent 不应有队列")
	}
	a.CloseSteeringQueue() // 不 panic
}

// ---------------------------------------------------------------------------
// U2 — ToolHooks 接线
// ---------------------------------------------------------------------------

// TestU2_ToolHooksSetterRoundTrip 断言 setter/getter 往返可用。
func TestU2_ToolHooksSetterRoundTrip(t *testing.T) {
	a := &Agent{}
	if got := a.ToolHooks(); got != nil {
		t.Fatalf("零值 Agent 的 ToolHooks 应为 nil，得到 %v", got)
	}

	h := NewToolHooks()
	a.SetToolHooks(h)
	if got := a.ToolHooks(); got != h {
		t.Fatal("ToolHooks 未返回注入的管道")
	}

	a.SetToolHooks(nil)
	if got := a.ToolHooks(); got != nil {
		t.Fatalf("SetToolHooks(nil) 后应返回 nil，得到 %v", got)
	}
}

// 注：before/after hook 阻止与忽略语义的 dispatch 级测试在
// wiring_20260813_04_dispatch_test.go（外部测试包，复用已有的 fakeRunner 桩）。

// ---------------------------------------------------------------------------
// U3 — difficultyRoundCap 接线
// ---------------------------------------------------------------------------

// TestU3_DifficultyRoundCapTightensSpeakPhase 断言 easy 档位收紧发言阶段。
//
// 这是 U3 的**核心断言**：修复前 difficulty.MaxToolUse=3 对轮次零影响，
// speak 阶段恒为 phase 基线 5 轮。
func TestU3_DifficultyRoundCapTightensSpeakPhase(t *testing.T) {
	base := maxInnerRoundsForPhase("speak")
	if base != defaultMaxInnerRounds {
		t.Fatalf("前置假设失败: speak 阶段基线应为 %d，实际 %d", defaultMaxInnerRounds, base)
	}

	a := &Agent{}
	a.SetDifficultyRoundCap(3) // easy 档位
	if got := a.maxInnerRoundsFor("speak"); got != 3 {
		t.Errorf("easy 档位(cap=3)应把 speak 从 %d 收紧到 3，实际 %d", base, got)
	}
}

// TestU3_ZeroCapUsesPhaseBaseline 断言 cap=0（normal/hell）走 phase 基线。
func TestU3_ZeroCapUsesPhaseBaseline(t *testing.T) {
	a := &Agent{}
	a.SetDifficultyRoundCap(0)

	for _, phase := range []string{"speak", "vote", "night_wolves", "hunter_shoot"} {
		want := maxInnerRoundsForPhase(phase)
		if got := a.maxInnerRoundsFor(phase); got != want {
			t.Errorf("cap=0 时 %s 应走基线 %d，实际 %d", phase, want, got)
		}
	}
}

// TestU3_CapOnlyTightensNeverWidens 断言 cap 只收紧不放宽。
//
// hard=6 / hell=8 都比夜间阶段基线(3)宽 —— 若实现写成无条件取 cap，
// 夜间就会从 3 轮放宽到 6/8 轮，破坏 §197 的慢模型预算假设。
func TestU3_CapOnlyTightensNeverWidens(t *testing.T) {
	nightBase := maxInnerRoundsForPhase("night_wolves")
	if nightBase != 3 {
		t.Fatalf("前置假设失败: night_wolves 基线应为 3，实际 %d", nightBase)
	}

	for _, cap := range []int{6, 8, 100} {
		a := &Agent{}
		a.SetDifficultyRoundCap(cap)
		if got := a.maxInnerRoundsFor("night_wolves"); got != nightBase {
			t.Errorf("cap=%d 比基线 %d 宽，应保持基线，实际放宽到 %d", cap, nightBase, got)
		}
	}
}

// TestU3_NegativeCapClampedToZero 断言负值被钳到 0（不收紧）。
func TestU3_NegativeCapClampedToZero(t *testing.T) {
	a := &Agent{}
	a.SetDifficultyRoundCap(-5)
	if got := a.DifficultyRoundCap(); got != 0 {
		t.Errorf("负 cap 应钳到 0，实际 %d", got)
	}
	if got := a.maxInnerRoundsFor("speak"); got != defaultMaxInnerRounds {
		t.Errorf("负 cap 应走基线 %d，实际 %d", defaultMaxInnerRounds, got)
	}
}

// ---------------------------------------------------------------------------
// 测试辅助
// ---------------------------------------------------------------------------

const agentSteerTestCap = 10
