// BUG-R213-P2-02 (2026-07-31) 回归测试:法官 fallback 文本必须携带当前对局
// 快照,避免阶段推进后 marquee 仍停留在「首夜强制发言阶段」这类陈旧文案。
//
// 背景(自动化测试报告 2026-07-31 04:32:56 §5.3):
//   - 法官 LLM 常因上游 400/超时熔断,玩家看到的**唯一**宣告就是 fallback;
//   - 旧 JudgeFallbackText 对所有阶段只返回一句静态占位,不含「第 N 天 /
//     存活 / 死亡」事实 → 白天发言阶段仍显示「首夜强制发言阶段」;
//   - 修复:JudgeFallbackTextWithSnapshot 拼接「第 N 天 · 存活 X / 死亡 Y」;
//     JudgeFallbackText(无快照) 保持旧行为以兼容测试 / 兜底路径。
package wwjudge

import (
	"strings"
	"testing"
)

// TestR213_JudgeFallbackText_LegacyBehavior 验证无快照路径与旧行为完全一致,
// 防止 snapshot-aware 重构破坏既有调用方(测试 / 兜底)。
func TestR213_JudgeFallbackText_LegacyBehavior(t *testing.T) {
	cases := []struct {
		kind string
		want string
	}{
		{JudgePendingFillingWelcome, "欢迎进入狼人杀对局。"},
		{JudgePendingPreWolves, "首夜强制发言阶段:所有存活玩家依次发言。"},
		{JudgePendingDawnAnnounce, "黎明已至,请查看昨夜伤亡。"},
		{JudgePendingSpeakStart, "进入白天发言阶段,请依次发言。"},
		{JudgePendingGameOver, "对局结束。"},
		{"unknown_kind", ""},
	}
	for _, c := range cases {
		if got := JudgeFallbackText(c.kind); got != c.want {
			t.Errorf("JudgeFallbackText(%q) = %q, want %q", c.kind, got, c.want)
		}
	}
}

// TestR213_JudgeFallbackTextWithSnapshot_IncludesDayAndCounts 验证快照感知
// fallback 拼接「第 N 天 · 存活 X / 死亡 Y」事实段,解决 marquee 陈旧问题。
func TestR213_JudgeFallbackTextWithSnapshot_IncludesDayAndCounts(t *testing.T) {
	snap := GameSnapshot{
		Day:        2,
		AliveSeats: []int{0, 1, 2, 3, 4, 5, 6, 7, 8},
		DeadSeats:  []int{9, 10, 11, 12},
	}
	got := JudgeFallbackTextWithSnapshot(JudgePendingSpeakStart, snap)
	if !strings.HasPrefix(got, "进入白天发言阶段,请依次发言。") {
		t.Errorf("fallback 缺少阶段语义前缀: %q", got)
	}
	if !strings.Contains(got, "第 2 天") {
		t.Errorf("fallback 缺少「第 N 天」事实: %q", got)
	}
	if !strings.Contains(got, "存活 9") || !strings.Contains(got, "死亡 4") {
		t.Errorf("fallback 缺少「存活/死亡」计数: %q", got)
	}
}

// TestR213_JudgeFallbackTextWithSnapshot_PreWolvesNoDayPrefix 验证首夜
// (day=0) 不拼接「第 0 天」这种不自然表述,只拼接存活/死亡计数。
func TestR213_JudgeFallbackTextWithSnapshot_PreWolvesNoDayPrefix(t *testing.T) {
	snap := GameSnapshot{
		Day:        0,
		AliveSeats: []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12},
	}
	got := JudgeFallbackTextWithSnapshot(JudgePendingPreWolves, snap)
	if strings.Contains(got, "第 0 天") {
		t.Errorf("首夜 fallback 不应出现「第 0 天」: %q", got)
	}
	if !strings.Contains(got, "存活 13") {
		t.Errorf("首夜 fallback 缺少存活计数: %q", got)
	}
}

// TestR213_JudgeFallbackTextWithSnapshot_EmptySnapDegrades 验证空快照退化为
// 旧静态文案,行为与 JudgeFallbackText 完全一致。
func TestR213_JudgeFallbackTextWithSnapshot_EmptySnapDegrades(t *testing.T) {
	for _, kind := range []string{
		JudgePendingPreWolves,
		JudgePendingSpeakStart,
		JudgePendingGameOver,
	} {
		if got, want := JudgeFallbackTextWithSnapshot(kind, GameSnapshot{}), JudgeFallbackText(kind); got != want {
			t.Errorf("空快照 fallback(%q) = %q, want %q(应与无快照路径一致)", kind, got, want)
		}
	}
}

// TestR213_JudgePublicPhaseLabel 验证 judgePublicPhaseLabel 覆盖所有主要
// phase 与 judge 事件 kind,且与前端 phaseLabel.ts 语义对齐。
func TestR213_JudgePublicPhaseLabel(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"pre_wolves", "首夜强制发言阶段"},
		{"PhasePreWolves", "首夜强制发言阶段"},
		{JudgePendingPreWolves, "首夜强制发言阶段"},
		{"speak", "白天 · 轮流发言"},
		{"PhaseSpeak", "白天 · 轮流发言"},
		{JudgePendingSpeakStart, "白天 · 轮流发言"},
		{"vote", "白天 · 投票放逐"},
		{"dawn", "黎明 · 公布死亡"},
		{"over", "对局结束"},
		{JudgePendingGameOver, "对局结束"},
		{"unknown_phase_xyz", "unknown_phase_xyz"}, // 未知阶段原样返回
	}
	for _, c := range cases {
		if got := judgePublicPhaseLabel(c.in); got != c.want {
			t.Errorf("judgePublicPhaseLabel(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestR213_JudgeSystemPrompt_ContainsPhaseHardConstraint 验证 system prompt
// 已注入「阶段语义硬约束」段,防止 LLM 在阶段推进后复述旧阶段语义。
func TestR213_JudgeSystemPrompt_ContainsPhaseHardConstraint(t *testing.T) {
	blocks := BuildJudgeSystemPrompt(JudgePendingSpeakStart, GameSnapshot{Day: 2})
	if len(blocks) == 0 {
		t.Fatal("BuildJudgeSystemPrompt 返回空 system blocks")
	}
	body := blocks[0].Text
	if !strings.Contains(body, "【阶段语义硬约束】") {
		t.Errorf("system prompt 缺少【阶段语义硬约束】段: %s", body)
	}
	if !strings.Contains(body, "白天 · 轮流发言") {
		t.Errorf("system prompt 未注入当前阶段对外名称: %s", body)
	}
	if !strings.Contains(body, "禁止") || !strings.Contains(body, "首夜") {
		t.Errorf("system prompt 未注入「禁止复述旧阶段」约束: %s", body)
	}
}
