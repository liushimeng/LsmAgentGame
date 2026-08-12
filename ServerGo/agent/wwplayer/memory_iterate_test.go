// Package agent_test — agent_memory 纯函数单测(2026-07-20 §131)。
//
// 覆盖设计文档 §9 测试要点中的纯逻辑部分:
//   - ValidateMemorySections:4 段标题齐全校验;
//   - HardTruncateMemory:rune 边界安全(绝不切断 UTF-8 字符);
//   - FallbackMerge:LLM 输出不合格时的规则兜底(旧记忆 + 本局 note);
//   - InjectBlock:注入截断(MemoryInjectMaxRunes)与空值不注入。
package wwplayer_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"LsmWebGame/agent/wwplayer"
)

// validMemoryMD 是一份含全部 4 段标题的合法记忆。
const validMemoryMD = `# Agent 长期记忆（模型: DeepSeek-model）

## 战绩与趋势
- 近 12 局: 胜 5 / 负 7

## 我的失误与教训
- 首夜不救人优先

## 其他模型特点分析
- Qwen-model: 发言快且多

## 决策策略迭代
- 投票前先核对警徽流
`

func TestValidateMemorySections(t *testing.T) {
	if !wwplayer.ValidateMemorySections(validMemoryMD) {
		t.Fatalf("valid memory should pass validation")
	}
	// 缺任意一段 → false
	for _, title := range []string{"## 战绩与趋势", "## 我的失误与教训", "## 其他模型特点分析", "## 决策策略迭代"} {
		broken := strings.Replace(validMemoryMD, title, "## 被改掉的标题", 1)
		if wwplayer.ValidateMemorySections(broken) {
			t.Fatalf("missing %q should fail validation", title)
		}
	}
	if wwplayer.ValidateMemorySections("") {
		t.Fatalf("empty string should fail validation")
	}
	if wwplayer.ValidateMemorySections("随便一段没有标题的文本") {
		t.Fatalf("garbage should fail validation")
	}
}

func TestHardTruncateMemory(t *testing.T) {
	// 已在限内 → 原样返回
	short := "短文本"
	if got := wwplayer.HardTruncateMemory(short, 100); got != short {
		t.Fatalf("short input should be returned as-is, got %q", got)
	}
	// maxBytes <= 0 → 空
	if got := wwplayer.HardTruncateMemory("abc", 0); got != "" {
		t.Fatalf("maxBytes=0 should return empty, got %q", got)
	}
	// 中文字符(3 字节)边界:截断点不能切断一个字符。
	// 构造 10 个汉字 = 30 字节,在第 29 字节处截断(第 10 个汉字中间)。
	src := strings.Repeat("汉", 10) // 30 bytes
	if len(src) != 30 {
		t.Fatalf("setup: expect 30 bytes, got %d", len(src))
	}
	got := wwplayer.HardTruncateMemory(src, 29)
	// 头部必须是合法 UTF-8 且字节数 ≤ 29(实际应截到 27 字节 = 9 个汉字)。
	head := strings.TrimSuffix(got, "\n\n…(全文超长已截断)\n")
	if !utf8.ValidString(head) {
		t.Fatalf("truncated head is not valid UTF-8: %q", head)
	}
	if len(head) > 29 {
		t.Fatalf("truncated head exceeds maxBytes: %d > 29", len(head))
	}
	if !strings.HasPrefix(src, head) {
		t.Fatalf("truncated head must be a prefix of source")
	}
	// 大输入:105KB → 截断后 ≤ 100KB + 后缀。
	big := strings.Repeat("测", 40000) // 120000 bytes
	got = wwplayer.HardTruncateMemory(big, wwplayer.MemoryMaxBytes)
	if !utf8.ValidString(got) {
		t.Fatalf("big truncate result must be valid UTF-8")
	}
	// 后缀固定约 20 字节,宽松断言:前缀部分 ≤ MemoryMaxBytes。
	head = strings.TrimSuffix(got, "\n\n…(全文超长已截断)\n")
	if len(head) > wwplayer.MemoryMaxBytes {
		t.Fatalf("hard truncate head %d bytes > MemoryMaxBytes %d", len(head), wwplayer.MemoryMaxBytes)
	}
}

func TestFallbackMerge(t *testing.T) {
	// 旧记忆为空 → 生成含 4 段骨架的新文档 + note 落在策略迭代段。
	got := wwplayer.FallbackMerge("", "本局(R1)迭代失败")
	if !wwplayer.ValidateMemorySections(got) {
		t.Fatalf("fallback merge from empty must contain all 4 sections:\n%s", got)
	}
	if !strings.Contains(got, "本局(R1)迭代失败") {
		t.Fatalf("fallback merge must include the game note")
	}
	// 旧记忆非空 → 保留全文 + 追加一行 note。
	got = wwplayer.FallbackMerge(validMemoryMD, "本局(R2) LLM 输出不合格")
	if !strings.Contains(got, validMemoryMD[:50]) {
		t.Fatalf("fallback merge must preserve old memory")
	}
	if !strings.Contains(got, "- 本局(R2) LLM 输出不合格") {
		t.Fatalf("fallback merge must append the note line")
	}
	// 空 note → 兜底占位文本。
	got = wwplayer.FallbackMerge(validMemoryMD, "  ")
	if !strings.Contains(got, "(本局迭代失败,仅记录局数)") {
		t.Fatalf("empty note should get placeholder text")
	}
}

func TestInjectBlockWithBudget(t *testing.T) {
	// 空输入 → 不注入
	if got := wwplayer.InjectBlockWithBudget("", 0); got != "" {
		t.Fatalf("empty memory should not inject, got %q", got)
	}
	if got := wwplayer.InjectBlockWithBudget("   \n  ", 0); got != "" {
		t.Fatalf("whitespace-only memory should not inject")
	}
	// 正常输入 → 含头尾注。
	got := wwplayer.InjectBlockWithBudget(validMemoryMD, 0)
	if !strings.Contains(got, "【你的长期记忆（跨局积累）】") {
		t.Fatalf("inject block must carry the header")
	}
	if !strings.Contains(got, "本局信息以上方实时状态为准") {
		t.Fatalf("inject block must carry the footer note")
	}
	if !strings.Contains(got, "## 战绩与趋势") {
		t.Fatalf("inject block must carry the memory body")
	}
	// 截断:> MemoryInjectMaxRunes 的输入被 rune 安全截断。
	long := strings.Repeat("忆", wwplayer.MemoryInjectMaxRunes+500)
	got = wwplayer.InjectBlockWithBudget(long, 0)
	if !strings.Contains(got, "记忆过长仅注入前部") {
		t.Fatalf("truncated inject block must carry truncation marker")
	}
	if !utf8.ValidString(got) {
		t.Fatalf("inject block must be valid UTF-8")
	}
	// 注入增量上界:头部 rune 数 ≤ MemoryInjectMaxRunes(+ 固定头尾注)。
	// 宽松断言:整体 rune 数 < MemoryInjectMaxRunes + 200(头尾注 + 截断标记)。
	if utf8.RuneCountInString(got) > wwplayer.MemoryInjectMaxRunes+200 {
		t.Fatalf("inject block rune count %d exceeds bound", utf8.RuneCountInString(got))
	}
}

func TestBuildIterationPrompt(t *testing.T) {
	// 基本形状:含 4 段标题要求 + 旧记忆 + 事实 + 法官总结。
	p := wwplayer.BuildIterationPrompt(validMemoryMD, "本局角色 女巫;胜方: 好人阵营", "【阵营胜负】好人胜", false)
	for _, title := range []string{"## 战绩与趋势", "## 我的失误与教训", "## 其他模型特点分析", "## 决策策略迭代"} {
		if !strings.Contains(p, title) {
			t.Fatalf("iteration prompt must list required section %q", title)
		}
	}
	if !strings.Contains(p, "本局角色 女巫") {
		t.Fatalf("prompt must include seat facts")
	}
	if !strings.Contains(p, "【阵营胜负】好人胜") {
		t.Fatalf("prompt must include judge summary")
	}
	if strings.Contains(p, "压缩指令") {
		t.Fatalf("compress=false must not include compression directive")
	}
	// compress=true → 追加压缩指令。
	p = wwplayer.BuildIterationPrompt(validMemoryMD, "facts", "summary", true)
	if !strings.Contains(p, "压缩指令") || !strings.Contains(p, "80K") {
		t.Fatalf("compress=true must include compression directive")
	}
	// 空旧记忆 → 明确标注"第一局"。
	p = wwplayer.BuildIterationPrompt("", "facts", "summary", false)
	if !strings.Contains(p, "第一局") {
		t.Fatalf("empty old memory should be marked as first game")
	}
}
