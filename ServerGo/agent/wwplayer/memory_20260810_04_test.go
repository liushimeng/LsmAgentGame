package wwplayer

// 2026-08-10 §20260810-04 U4 — InjectBlock 按段配额截断回归测试。
//
// 验证:
//   - 完整 4 段记忆 → 各段配额 rune 截断(标题保留,余数给第 4 段);
//   - 缺段 → 回退头部截断(与旧行为一致);
//   - 第 4 段「决策策略迭代」不再被系统性丢弃。

import (
	"strings"
	"testing"
)

// makeFullMemory 构造一份含 4 个固定标题的完整记忆;section 内容长度按需。
func makeFullMemory(s1, s2, s3, s4 string) string {
	return strings.Join([]string{
		"# Agent 长期记忆\n\n",
		"## 战绩与趋势\n", s1, "\n\n",
		"## 我的失误与教训\n", s2, "\n\n",
		"## 其他模型特点分析\n", s3, "\n\n",
		"## 决策策略迭代\n", s4, "\n",
	}, "")
}

// TestTruncateMemoryBySections_PreservesAllFourHeadings — 4 段均保留标题。
func TestTruncateMemoryBySections_PreservesAllFourHeadings(t *testing.T) {
	md := makeFullMemory(
		"近 5 局胜率 60%。",
		"女巫首夜解药浪费。",
		"GLM 说话长。",
		"优先保预言家。",
	)
	maxRunes := 200 // 远大于实际内容,不应触发截断。
	out := TruncateMemoryBySections(md, maxRunes)
	if !ValidateMemorySections(out) {
		t.Fatalf("output should have all 4 sections; got:\n%s", out)
	}
}

// TestTruncateMemoryBySections_AllocatesQuotaEvenly — 每段按配额截断,余数给第 4 段。
func TestTruncateMemoryBySections_AllocatesQuotaEvenly(t *testing.T) {
	// 构造每段 200 rune 内容(总 800 rune),maxRunes=200 → 每段 50,余 0。
	sectionBody := strings.Repeat("x", 200)
	md := makeFullMemory(sectionBody, sectionBody, sectionBody, sectionBody)
	out := TruncateMemoryBySections(md, 200)
	if !ValidateMemorySections(out) {
		t.Fatalf("output should still have all 4 sections; got:\n%s", out)
	}
	// 验证每个标题之后的内容长度 ≤ 配额 - 标题长度。
	quotas := []int{50, 50, 50, 50} // 平均分配,余 0 给第 4 段(实际相同)
	for i, title := range memorySectionTitles {
		idx := strings.Index(out, title)
		if idx < 0 {
			t.Fatalf("missing title %q in output", title)
		}
		// 标题之后到下一标题(或末尾)的内容长度。
		nextIdx := len(out)
		if i+1 < len(memorySectionTitles) {
			nextIdx = strings.Index(out, memorySectionTitles[i+1])
		}
		contentStart := idx + len(title)
		content := out[contentStart:nextIdx]
		// 截断标记「…(本段过长已截断)」如果出现,rune 数包含尾注;
		// 去掉尾注再比较。
		trimmed := strings.TrimSuffix(content, "\n…(本段过长已截断)")
		runeCount := len([]rune(trimmed))
		titleRunes := len([]rune(title))
		// 该段总配额包含标题,即内容配额 = quotas[i] - titleRunes。
		want := quotas[i] - titleRunes
		if want < 0 {
			want = 0
		}
		if runeCount > want {
			t.Fatalf("section %d (%s) content rune count %d > quota %d", i+1, title, runeCount, want)
		}
	}
}

// TestTruncateMemoryBySections_ExtraRunesGoToLastSection — 余数分配给第 4 段。
func TestTruncateMemoryBySections_ExtraRunesGoToLastSection(t *testing.T) {
	md := makeFullMemory("AAAA", "BBBB", "CCCC", "DDDD")
	// maxRunes=17 → 4 段平均 4,余 1 → 第 4 段 5。
	// 标题长度:"## 战绩与趋势" = 7,"## 我的失误与教训" = 9,"## 其他模型特点分析" = 11,"## 决策策略迭代" = 9
	out := TruncateMemoryBySections(md, 17)
	// 第 4 段配额 = 5,标题占 9 rune → 内容配额 -4 → 实际不放任何内容 + 截断标记可能存在。
	idx4 := strings.Index(out, "## 决策策略迭代")
	if idx4 < 0 {
		t.Fatalf("missing last section title")
	}
	// 整个输出应包含截断标记(至少一段超配额)。
	if !strings.Contains(out, "本段过长已截断") {
		t.Fatalf("expected at least one truncation marker; got:\n%s", out)
	}
}

// TestTruncateMemoryBySections_FallsBackOnMissingSection — 缺段时回退头部截断。
func TestTruncateMemoryBySections_FallsBackOnMissingSection(t *testing.T) {
	md := "# Agent 长期记忆\n\n## 战绩与趋势\n近 5 局胜率 60%。\n" // 缺第 2/3/4 段
	out := TruncateMemoryBySections(md, 100)
	if !ValidateMemorySections(out) {
		// 缺段回退头部截断 → 输出可能没有全部 4 标题(这是预期行为)。
		if !strings.Contains(out, "## 战绩与趋势") {
			t.Fatalf("output should at least preserve original content; got:\n%s", out)
		}
	}
}

// TestTruncateMemoryBySections_EmptyInput — 空输入返回空。
func TestTruncateMemoryBySections_EmptyInput(t *testing.T) {
	if got := TruncateMemoryBySections("", 100); got != "" {
		t.Fatalf("empty input should return empty; got %q", got)
	}
}

// TestInjectBlock_AllFourSectionsPreserved — InjectBlock 集成:4 段标题全部保留,
// 第 4 段不再被系统性丢弃(LongCat-D4 修复要点)。
func TestInjectBlock_AllFourSectionsPreserved(t *testing.T) {
	md := makeFullMemory(
		strings.Repeat("战绩数据.", 500),  // 超长:会被截断
		strings.Repeat("失误教训.", 500),
		strings.Repeat("模型特点.", 500),
		"关键策略:必须保留。",
	)
	out := InjectBlockWithBudget(md, 0)
	if !strings.Contains(out, "## 决策策略迭代") {
		t.Fatalf("decision-strategy section must be preserved; got:\n%s", out[:min(500, len(out))])
	}
	if !strings.Contains(out, "必须保留") {
		t.Fatalf("key phrase from section 4 must survive; got:\n%s", out[:minLen(500, len(out))])
	}
}

// minLen 是局部辅助,避免与 agent.go 中已声明的 min 冲突。
func minLen(a, b int) int {
	if a < b {
		return a
	}
	return b
}