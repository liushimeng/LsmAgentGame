package wwplayer

import (
	"strings"
	"testing"
)

// §20260813-04 U4 —— pre-flight 上下文预算预检测试。
//
// 对标 Hermes _compute_threshold_tokens 的三个洞察，每条一组断言：
//
//	① 必须减去 max_tokens 输出预留     （Hermes #43547）
//	② 预留吃掉窗口时按比例退化          （Hermes #14690）
//	③ max_tokens 未知时保守假设无预留

// TestU4_BudgetSubtractsOutputReserve 断言洞察 ①：预算减去输出预留。
//
// 这是 U4 的**核心断言** —— 修复前 getModelContextBudget 返回的是整个窗口，
// 完全没有「provider 从同一窗口预留输出空间」的概念。
func TestU4_BudgetSubtractsOutputReserve(t *testing.T) {
	const model = "DouBao-model" // 400KB 窗口
	window := getModelContextBudget(model)
	if window <= 0 {
		t.Fatalf("前置假设失败: %s 应有正窗口，实际 %d", model, window)
	}

	got := preflightBudgetBytes(model, llmMaxTokensPerCall)
	wantReserve := llmMaxTokensPerCall * preflightBytesPerToken
	want := window - wantReserve

	if got != want {
		t.Errorf("预算应为 窗口(%d) - 预留(%d) = %d，实际 %d",
			window, wantReserve, want, got)
	}
	if got >= window {
		t.Errorf("预算 %d 未小于窗口 %d —— 输出预留没被扣减（Hermes #43547 同类缺陷）",
			got, window)
	}
}

// TestU4_DegradesWhenReserveEatsWindow 断言洞察 ②：预留 ≥ 窗口时按比例退化。
//
// 若不做这个处理，effective 会 ≤ 0，pre-flight 要么永不触发、要么每轮都触发。
func TestU4_DegradesWhenReserveEatsWindow(t *testing.T) {
	const model = "DouBao-model"
	window := getModelContextBudget(model)

	// 构造一个能吃掉整个窗口的 max_tokens
	hugeMaxTokens := window/preflightBytesPerToken + 1000
	got := preflightBudgetBytes(model, hugeMaxTokens)

	if got <= 0 {
		t.Fatalf("退化路径返回非正预算 %d —— pre-flight 会失效", got)
	}
	// 退化值应是窗口的 85%，但会被 min 下限钳制，故只断言两个不变式：
	if got > window {
		t.Errorf("退化预算 %d 不应超过窗口 %d", got, window)
	}
	if got < preflightMinBudgetBytes {
		t.Errorf("退化预算 %d 低于下限 %d", got, preflightMinBudgetBytes)
	}
}

// TestU4_UnknownMaxTokensAssumesNoReserve 断言洞察 ③：未知 max_tokens 不扣预留。
func TestU4_UnknownMaxTokensAssumesNoReserve(t *testing.T) {
	const model = "MeiTuan-model" // 600KB
	window := getModelContextBudget(model)

	for _, mt := range []int{0, -1, -9999} {
		got := preflightBudgetBytes(model, mt)
		if got != window {
			t.Errorf("max_tokens=%d（未知）应保守用完整窗口 %d，实际 %d", mt, window, got)
		}
	}
}

// TestU4_UnknownModelFallsBackToDefault 断言未注册模型走默认预算。
//
// getModelContextBudget 是 8 键硬编码 map，第 9 个模型静默 fallback。
// pre-flight 不能因此返回 0（那会让预检整体失效）。
func TestU4_UnknownModelFallsBackToDefault(t *testing.T) {
	got := preflightBudgetBytes("Brand-New-Model-Not-In-Map", 0)
	if got != DefaultMaxPromptBytes {
		t.Errorf("未知模型应 fallback 到 DefaultMaxPromptBytes(%d)，实际 %d",
			DefaultMaxPromptBytes, got)
	}
	if got <= 0 {
		t.Fatal("未知模型返回非正预算 —— pre-flight 会被整体跳过")
	}
}

// TestU4_BudgetNeverBelowFloor 断言预算有下限（护栏宁松勿紧）。
//
// 极端配置（小窗口 + 大 max_tokens）不能把预算压到几 KB，
// 否则每轮都裁剪、把 identity 和近期上下文全裁掉（§20260812-04 教训 5）。
func TestU4_BudgetNeverBelowFloor(t *testing.T) {
	for _, mt := range []int{100000, 1000000} {
		got := preflightBudgetBytes("DeepSeek-model", mt)
		if got < preflightMinBudgetBytes {
			t.Errorf("max_tokens=%d 时预算 %d 低于下限 %d", mt, got, preflightMinBudgetBytes)
		}
	}
}

// TestU4_ShouldPruneOnlyWhenOverBudget 断言不超预算时**不裁剪**。
//
// 这是最重要的回归保护：pre-flight 误杀会让正常对局丢上下文。
func TestU4_ShouldPruneOnlyWhenOverBudget(t *testing.T) {
	const budget = 200 * 1024

	cases := []struct {
		name    string
		payload int
		want    bool
	}{
		{"远低于预算", 50 * 1024, false},
		{"恰好等于预算", budget, false}, // 等于不裁：能过就过
		{"刚超预算 1 字节", budget + 1, true},
		{"大幅超预算", budget * 3, true},
	}
	for _, c := range cases {
		need, target, reason := shouldPreflightPrune(c.payload, budget)
		if need != c.want {
			t.Errorf("%s: payload=%d budget=%d 期望 need=%v，实际 %v",
				c.name, c.payload, budget, c.want, need)
		}
		if need {
			if target != budget {
				t.Errorf("%s: 目标字节应等于预算 %d，实际 %d", c.name, budget, target)
			}
			if reason == "" {
				t.Errorf("%s: 裁剪必须给出原因（可观测性）", c.name)
			}
		} else {
			if target != 0 || reason != "" {
				t.Errorf("%s: 不裁剪时应返回零值，实际 target=%d reason=%q",
					c.name, target, reason)
			}
		}
	}
}

// TestU4_ZeroBudgetDisablesPreflight 断言预算为 0 时 pre-flight 整体跳过。
// 防御性：若 getModelContextBudget 未来返回 0，不能变成「payload > 0 就裁剪」。
func TestU4_ZeroBudgetDisablesPreflight(t *testing.T) {
	need, _, _ := shouldPreflightPrune(999*1024, 0)
	if need {
		t.Error("预算为 0 时不应触发裁剪（应视为 pre-flight 禁用）")
	}
}

// TestU4_NoteCarriesFourNumbers 断言可观测标记含四个关键数字。
//
// 降级必留可观测标记（§20260812-04 教训 4）：
// 标记里要能读出 payload / 预算 / 窗口 / 预留，否则无法事后诊断。
func TestU4_NoteCarriesFourNumbers(t *testing.T) {
	note := preflightNote("DouBao-model", 500*1024, 394*1024, llmMaxTokensPerCall)

	for _, want := range []string{"pre-flight", "DouBao-model", "payload", "预算", "窗口", "预留"} {
		if !strings.Contains(note, want) {
			t.Errorf("标记缺少 %q，实际:\n%s", want, note)
		}
	}
}

// TestU4_SetLastPreflightNoteRoundTrip 断言标记能写入并读回（wire 透出前提）。
func TestU4_SetLastPreflightNoteRoundTrip(t *testing.T) {
	a := &Agent{}
	if got := a.LastPreflightNote(); got != "" {
		t.Fatalf("零值 Agent 的标记应为空，得到 %q", got)
	}

	a.SetLastPreflightNote("[pre-flight 裁剪] test")
	if got := a.LastPreflightNote(); got == "" {
		t.Fatal("SetLastPreflightNote 后标记仍为空")
	}
	if a.preflightAt == 0 {
		t.Error("preflightAt 应被打上时间戳（0 表示从未裁剪，会让前端误判）")
	}
}

// TestU4_NoteIsTruncated 断言过长标记被截断（防止 wire 膨胀）。
func TestU4_NoteIsTruncated(t *testing.T) {
	a := &Agent{}
	a.SetLastPreflightNote(strings.Repeat("很长的诊断信息", 200))

	got := a.LastPreflightNote()
	if len([]rune(got)) > 200 {
		t.Errorf("标记应被截断到 ~160 rune，实际 %d rune", len([]rune(got)))
	}
}

// TestU4_MaxTokensConstantMatchesRequest 断言常量与实际请求值一致。
//
// pre-flight 的整个前提是「预留量 = 实际请求的 max_tokens」。
// 若 run.go 里的 MaxTokens 与本常量漂移，预留量算错，预检失去意义
// —— 这正是 Hermes #43547 的教训。本断言把「两处必须同源」钉死。
func TestU4_MaxTokensConstantMatchesRequest(t *testing.T) {
	srcs := packageSources(t)

	var hardcoded []string
	for name, src := range srcs {
		if !strings.HasSuffix(name, "run.go") {
			continue
		}
		for _, line := range strings.Split(src, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "MaxTokens:") &&
				!strings.Contains(trimmed, "llmMaxTokensPerCall") {
				hardcoded = append(hardcoded, trimmed)
			}
		}
	}
	if len(hardcoded) > 0 {
		t.Errorf(`run.go 仍有硬编码的 MaxTokens，会与 pre-flight 预留量漂移:

    %s

改用 llmMaxTokensPerCall 常量 —— 预留量必须与实际请求值同源（Hermes #43547）。`,
			strings.Join(hardcoded, "\n    "))
	}
}
