// LLM 调用超时兜底回归测试 (2026-08-01,报告 20260801_083235)。
//
// 缺陷:cfgLLMCallTimeoutSec 用匿名返回值 + `defer func(){ _ = recover() }()`,
// config.Load() panic 时静默返回零值 0;而 run.go 的调用点把 0 解读为
// "不强制超时",走 `context.WithCancel(ctx)` 分支 —— **每一次 LLM 调用都变成
// 无界调用**(既不超时、也不进重试 / quarantine,agent goroutine 永久挂住)。
// 兄弟函数 cfgStreamExtendedTimeoutSec 早有 `<=0 → default` 兜底,callTimeout
// 独独没有。
//
// 修复三处:
//  1. cfgLLMCallTimeoutSec           → 具名返回值 + recover 回填 default;
//  2. cfgLLMCallTimeoutSecWithFallback → 同源缺陷,同样修;
//  3. run.go 调用点                   → `if callTimeout <= 0 { = default }`
//     纵深防御 + 删除不可达的 `WithCancel` 无界分支。
package wwplayer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestLLMTimeout_NeverReturnsZero_MissingConfig 是**核心回归**:
// 指向一个不存在的配置文件(模拟 config.Load() 失败 / panic),
// cfgLLMCallTimeoutSec 必须返回 defaultLLMCallTimeoutSec 而不是 0。
func TestLLMTimeout_NeverReturnsZero_MissingConfig(t *testing.T) {
	orig, had := os.LookupEnv("LSM_CONF")
	defer func() {
		if had {
			os.Setenv("LSM_CONF", orig)
		} else {
			os.Unsetenv("LSM_CONF")
		}
	}()
	os.Setenv("LSM_CONF", filepath.Join(t.TempDir(), "definitely-missing.conf"))

	for _, seats := range []int{0, 7, 13, 19} {
		if got := cfgLLMCallTimeoutSec(seats); got <= 0 {
			t.Fatalf("cfgLLMCallTimeoutSec(%d) = %d —— 返回 0 会被调用点解读为"+
				"\"不强制超时\",导致 LLM 调用无界挂起", seats, got)
		}
		if got := cfgLLMCallTimeoutSecWithFallback(seats); got <= 0 {
			t.Fatalf("cfgLLMCallTimeoutSecWithFallback(%d) = %d,同样不可为 0", seats, got)
		}
	}
}

// TestLLMTimeout_DefaultConstantIsPositive:兜底常量本身必须为正。
// 若有人把 defaultLLMCallTimeoutSec 改成 0,上面所有兜底都会退化回
// "无超时",本断言是那种改动的第一道拦截。
func TestLLMTimeout_DefaultConstantIsPositive(t *testing.T) {
	if defaultLLMCallTimeoutSec <= 0 {
		t.Fatalf("defaultLLMCallTimeoutSec = %d,必须为正 —— 0 会让兜底退化为\"无超时\"",
			defaultLLMCallTimeoutSec)
	}
	if defaultStreamExtendedTimeoutSec <= 0 {
		t.Fatalf("defaultStreamExtendedTimeoutSec = %d,必须为正", defaultStreamExtendedTimeoutSec)
	}
}

// TestLLMTimeout_BudgetIsAlwaysPositive 直接驱动生产函数 llmCallBudgetSec
// (run.go 构造 parentCtx 的唯一事实来源):任何输入组合下总预算都必须为正。
// 修复前调用点是 `if callTimeout > 0 { WithTimeout } else { WithCancel }`,
// 0 输入直接产出无 deadline 的 ctx;本断言即为那条分支的墓志铭。
func TestLLMTimeout_BudgetIsAlwaysPositive(t *testing.T) {
	cases := []struct{ call, extended int }{
		{0, 0},     // 两个 cfg 读取全退化(config.Load() panic 场景)
		{0, 900},   // 仅 callTimeout 退化 —— 这是本次缺陷的真实形状
		{300, 0},   // 仅 extendedTimeout 退化
		{-1, -1},   // 负值(防御性)
		{300, 900}, // 正常
	}
	for _, c := range cases {
		got := llmCallBudgetSec(c.call, c.extended)
		if got <= 0 {
			t.Fatalf("llmCallBudgetSec(%d, %d) = %d,总预算必须为正 —— "+
				"非正值会退化为无界 LLM 调用", c.call, c.extended, got)
		}
		// 兜底后总预算至少是两个 default 里较小的那个,不应被压到几秒。
		if got < defaultStreamExtendedTimeoutSec {
			t.Fatalf("llmCallBudgetSec(%d, %d) = %d,低于 extended 兜底 %d",
				c.call, c.extended, got, defaultStreamExtendedTimeoutSec)
		}
	}
	// 正常输入不被改写。
	if got := llmCallBudgetSec(300, 900); got != 1200 {
		t.Fatalf("正常输入 (300, 900) 应为 1200,got %d", got)
	}
}

// TestLLMTimeout_ParentCtxAlwaysHasDeadline 断言用 llmCallBudgetSec 构造出的
// ctx(与 run.go 生产路径同一函数)在最坏输入下仍**带 deadline**。
func TestLLMTimeout_ParentCtxAlwaysHasDeadline(t *testing.T) {
	parentCtx, cancel := context.WithTimeout(
		context.Background(), time.Duration(llmCallBudgetSec(0, 0))*time.Second)
	defer cancel()

	dl, ok := parentCtx.Deadline()
	if !ok {
		t.Fatal("parentCtx 必须带 deadline —— 无 deadline = LLM 调用可无限挂起")
	}
	if remain := time.Until(dl); remain <= 0 || remain > 25*time.Minute {
		t.Fatalf("parentCtx 剩余预算 %v 不合理(期望 0 < x <= 25min)", remain)
	}
}

// TestLLMTimeout_ExplicitConfigStillHonoured:兜底不能盖掉显式配置。
// config.Load() 是 sync.Once 缓存的(整个测试二进制只加载一次),所以这里
// 不再注入临时 conf(会与 run_13p_lenient_test.go 抢加载顺序),改为直接
// 断言参与计算的纯函数 —— 显式 base 值必须原样透传 / 按 lenient 缩放,
// 不会被 <=0 兜底逻辑吞掉。
func TestLLMTimeout_ExplicitConfigStillHonoured(t *testing.T) {
	if got := cfgLLMCallTimeoutSecScaled(120, 7, 13, 150); got != 120 {
		t.Fatalf("显式 120s(7 人局不缩放)应原样返回,got %d", got)
	}
	if got := cfgLLMCallTimeoutSecScaled(120, 13, 13, 150); got != 180 {
		t.Fatalf("显式 120s × 150%%(13 人局 lenient)应为 180,got %d", got)
	}
	// 兜底只在 <=0 时介入:600s 这种大于 default 的显式值不应被改写。
	if got := cfgLLMCallTimeoutSecScaled(600, 7, 13, 100); got != 600 {
		t.Fatalf("显式 600s 不应被 default(%d)覆盖,got %d", defaultLLMCallTimeoutSec, got)
	}
}
