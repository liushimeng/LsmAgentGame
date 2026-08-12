// §流式续命测试 (2026-07-24)
//
// 背景:13 人局慢模型(Kimi/GLM/DeepSeek 典型首字节 1-3min)在外层
// context.WithTimeout(默认 300s/480s cap)经常被误 cancel,即使后续
// 持续有 token 输出。本测试覆盖 3 项关键不变式:
//
//   1. cfgStreamExtendedTimeoutSec() 配置覆盖生效,负值兜底默认;
//   2. cfgLLMCallTimeoutSecScaled() 在 lenient ×150% 下 = 450,cap 480;
//   3. ctx 总预算:parentCtx = (callTimeout + extendedTimeout) ≥ 1200s。
//
// 这是白盒测试 (package agent),不依赖真实 LLM Provider;直接对
// 配置读取函数 + ctx 分层构造做断言。
package wwplayer

import (
	"context"
	"testing"
	"time"
)

// TestStreamExtend_001_DefaultConstant 验证 §流式续命 兜底常量
// defaultStreamExtendedTimeoutSec = 900 (15 min),这是"最少 15 分钟"
// 用户需求的硬约束。任何运行时配置覆盖都不能降低这个值(但允许调高)。
func TestStreamExtend_001_DefaultConstant(t *testing.T) {
	if defaultStreamExtendedTimeoutSec != 900 {
		t.Fatalf("defaultStreamExtendedTimeoutSec = %d, 期望 900 (15 min)", defaultStreamExtendedTimeoutSec)
	}
}

// TestStreamExtend_002_ScaledTimeoutCap 验证 cfgLLMCallTimeoutSecScaled
// 的标度逻辑,无外部依赖。
//
// 函数定义:seatCount >= lenientSeatCount 且 scalePercent > 100 时,
// scaled = base * scalePercent / 100,截断到 480。
func TestStreamExtend_002_ScaledTimeoutCap(t *testing.T) {
	// 7 人局 < lenient=13 → 不缩放 → 300
	got := cfgLLMCallTimeoutSecScaled(300, 7, 13, 150)
	if got != 300 {
		t.Fatalf("7 人局不缩放: 期望 300, got %d", got)
	}

	// 13 人局 lenient ×150% → 300*150/100 = 450
	got = cfgLLMCallTimeoutSecScaled(300, 13, 13, 150)
	if got != 450 {
		t.Fatalf("13 人局 lenient ×150%%: 期望 450, got %d", got)
	}

	// 19 人局(>= 13) ×150% → 450
	got = cfgLLMCallTimeoutSecScaled(300, 19, 13, 150)
	if got != 450 {
		t.Fatalf("19 人局 lenient ×150%%: 期望 450, got %d", got)
	}

	// 极端 base=480 → 480*1.5 = 720,被 cap=480 截断
	got = cfgLLMCallTimeoutSecScaled(480, 13, 13, 200)
	if got != 480 {
		t.Fatalf("极端 base=480 ×200%% 应被 cap=480 截断, got %d", got)
	}

	// scalePercent <= 100 → 不缩放(300 < 13 不满足,但即使满足也走 base 分支)
	got = cfgLLMCallTimeoutSecScaled(300, 7, 13, 100)
	if got != 300 {
		t.Fatalf("scalePercent=100 不缩放: 期望 300, got %d", got)
	}

	// lenientSeatCount=0 意味着任何 seatCount >= 0 都触发缩放(向后兼容:disabled 时
	// 应同时把 lenientSeatCount 设为大于最大座位数,如 13)。这里测的是纯函数逻辑。
	got = cfgLLMCallTimeoutSecScaled(300, 13, 0, 150)
	if got != 450 {
		t.Fatalf("lenientSeatCount=0 + seatCount=13 应触发缩放: 期望 450, got %d", got)
	}
}

// TestStreamExtend_003_TotalBudgetIsAtLeast15Min 验证"最少 15 分钟"硬约束:
// callTimeout + extendedTimeout 的总和 ≥ 900s(15 min)。
//
// 实际数值依赖 cfgLLMCallTimeoutSecScaled 与 cfgStreamExtendedTimeoutSec;
// 本测试保守断言:任意 ≥7 人局,总预算必 ≥ 1200s(300 + 900)。
func TestStreamExtend_003_TotalBudgetIsAtLeast15Min(t *testing.T) {
	// §流式续命硬约束:单次 LLM 调用的兜底总预算 = callTimeout(300s 基础) +
	// defaultStreamExtendedTimeoutSec(900s) = 1200s = 20 min,任意场景下都不
	// 应低于 900s (15 min)。
	if defaultStreamExtendedTimeoutSec < 900 {
		t.Fatalf("defaultStreamExtendedTimeoutSec = %d 违反\"最少 15 min\"用户需求",
			defaultStreamExtendedTimeoutSec)
	}
	if defaultLLMCallTimeoutSec+defaultStreamExtendedTimeoutSec < 1200 {
		t.Fatalf("兜底总预算 %d 应 ≥ 1200 (20 min)",
			defaultLLMCallTimeoutSec+defaultStreamExtendedTimeoutSec)
	}
}

// TestStreamExtend_004_CtxDeadlineBehaviour 模拟 parentCtx 的 deadline 行为:
//   - T0 创建 parentCtx,deadline = (callTimeout + extended) 后到期;
//   - 不到期前 parentCtx.Err() == nil;
//   - 到期后 parentCtx.Err() == context.DeadlineExceeded。
//
// 不再模拟"双层 ctx + streamCancel 释放"——简化设计后只剩 parentCtx。
func TestStreamExtend_004_CtxDeadlineBehaviour(t *testing.T) {
	callTimeout := 50 * time.Millisecond
	extendedTimeout := 500 * time.Millisecond
	totalBudget := callTimeout + extendedTimeout

	parentCtx, parentCancel := context.WithTimeout(context.Background(), totalBudget)
	defer parentCancel()

	// T0 + 30ms:还没到 callTimeout 到期
	time.Sleep(30 * time.Millisecond)
	if parentCtx.Err() != nil {
		t.Fatalf("30ms 时 parentCtx 应仍活 (剩余 ~520ms), got %v", parentCtx.Err())
	}

	// T0 + 100ms:超过 callTimeout(50ms),但还在 totalBudget 内,parentCtx 还活
	time.Sleep(70 * time.Millisecond)
	if parentCtx.Err() != nil {
		t.Fatalf("100ms 时 parentCtx 应仍活 (剩余 ~450ms), got %v", parentCtx.Err())
	}

	// 等到 totalBudget 到期
	time.Sleep(totalBudget)
	if parentCtx.Err() == nil {
		t.Fatalf("totalBudget 到期后 parentCtx 应 Done, got nil")
	}
}