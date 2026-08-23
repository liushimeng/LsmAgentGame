// chat_compress_test.go — 2026-08-23 §3.1/§3.2/§3.4 德州扑克Agent聊天系统
// 回归测试:聊天窗口注入、限流新参数、压缩四梯度、MemoryIter 纯函数与
// AgentClassName 接线(§130 防御:新字段写完必须有消费点)。
package thpagent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	agentroot "LsmAgentGame/agent"
	agentcore "LsmAgentGame/agent/core"
	"LsmAgentGame/llm/types"
)

// ─────────────────── §3.1 ChatWindow 注入 ───────────────────

func TestFormatChatWindow_RendersMessages(t *testing.T) {
	msgs := []agentcore.ChatMessage{
		{FromSeat: -1, FromID: "u1", FromAccount: "Alice", Text: "你好"},
		{FromSeat: 2, FromID: "bot:r:2", AgentName: "ModelA", FromAccount: "Bot 3号", IsBot: true, Text: "跟注"},
		{FromID: "u2", IsWhisper: true, Text: "私聊不应出现"},
		{FromID: "u3", Text: "  "}, // 空白文本跳过
	}
	out := FormatChatWindow(msgs)
	if !strings.Contains(out, "Alice: 你好") {
		t.Errorf("window missing human msg: %q", out)
	}
	if !strings.Contains(out, "Bot 3号: 跟注") {
		t.Errorf("window missing bot msg: %q", out)
	}
	if strings.Contains(out, "私聊") {
		t.Errorf("whisper must never appear in chat window: %q", out)
	}
}

func TestBuildUserPrompt_ContainsChatWindowBlock(t *testing.T) {
	ctx := &GameContextForAgent{RoomID: "r1", MySeat: 0, Street: "preflop"}
	if strings.Contains(BuildUserPrompt(ctx, NewMemory()), "牌桌闲聊") {
		t.Error("empty ChatWindow must not inject chat block")
	}
	ctx.ChatWindow = "Alice: 加注啊\n"
	got := BuildUserPrompt(ctx, NewMemory())
	if !strings.Contains(got, "【牌桌闲聊(增量)】") || !strings.Contains(got, "Alice: 加注啊") {
		t.Errorf("user prompt missing chat window block:\n%s", got)
	}
}

func TestBuildSystemPrompt_ChatGuidance(t *testing.T) {
	ctx := &GameContextForAgent{RoomID: "r1", MySeat: 0, Street: "preflop"}
	sys := BuildSystemPrompt(ctx, NewMemory())
	// §3.2:每手 ≤3 次 + 回应他人 + 不泄露底牌 + 摊牌短评。
	for _, want := range []string{"最多 3 次", "回应他人", "不要泄露自己的底牌", "情绪化短评"} {
		if !strings.Contains(sys, want) {
			t.Errorf("system prompt missing guidance %q", want)
		}
	}
}

// ─────────────────── §3.4 压缩四梯度 ───────────────────

func TestPromptTierFor(t *testing.T) {
	const budget = 1000
	cases := []struct {
		bytes int
		want  PromptTier
	}{
		{500, TierNone},
		{601, Tier60},
		{799, Tier60},
		{801, Tier80},
		{1000, Tier100},
		{5000, Tier100},
	}
	for _, c := range cases {
		if got := PromptTierFor(c.bytes, budget); got != c.want {
			t.Errorf("PromptTierFor(%d,%d)=%d want %d", c.bytes, budget, got, c.want)
		}
	}
}

func TestApplyPromptCompression_Tiers(t *testing.T) {
	lines := make([]string, 30)
	for i := range lines {
		lines[i] = "speaker: message"
	}
	window := strings.Join(lines, "\n") + "\n"

	// Tier60: 保留 20 行 + RecentHands 3 手
	ctx := &GameContextForAgent{ChatWindow: window}
	mem := NewMemory()
	for i := 0; i < 5; i++ {
		mem.AppendHand(HandRecord{HandNumber: i + 1}, 5)
	}
	ApplyPromptCompression(ctx, mem, Tier60, "")
	if got := len(strings.Split(strings.TrimSpace(ctx.ChatWindow), "\n")); got != 20 {
		t.Errorf("Tier60 chat window lines=%d want 20", got)
	}
	if len(mem.RecentHandsSnapshot()) != 3 {
		t.Errorf("Tier60 recent hands=%d want 3", len(mem.RecentHandsSnapshot()))
	}

	// Tier80 + LLM 摘要:ChatWindow 被摘要替换
	ctx2 := &GameContextForAgent{ChatWindow: window}
	ApplyPromptCompression(ctx2, NewMemory(), Tier80, " Alice 加注,Bot 3 跟注 ")
	if ctx2.ChatWindow != "Alice 加注,Bot 3 跟注" {
		t.Errorf("Tier80 llm summary not applied: %q", ctx2.ChatWindow)
	}

	// Tier80 LLM 失败(空摘要):回退规则式 20 行
	ctx3 := &GameContextForAgent{ChatWindow: window}
	ApplyPromptCompression(ctx3, NewMemory(), Tier80, "")
	if got := len(strings.Split(strings.TrimSpace(ctx3.ChatWindow), "\n")); got != 20 {
		t.Errorf("Tier80 fallback lines=%d want 20", got)
	}

	// Tier100:只留 5 行 + 1 手
	ctx4 := &GameContextForAgent{ChatWindow: window}
	mem4 := NewMemory()
	for i := 0; i < 5; i++ {
		mem4.AppendHand(HandRecord{HandNumber: i + 1}, 5)
	}
	ApplyPromptCompression(ctx4, mem4, Tier100, "")
	if got := len(strings.Split(strings.TrimSpace(ctx4.ChatWindow), "\n")); got != 5 {
		t.Errorf("Tier100 lines=%d want 5", got)
	}
	if len(mem4.RecentHandsSnapshot()) != 1 {
		t.Errorf("Tier100 recent hands=%d want 1", len(mem4.RecentHandsSnapshot()))
	}

	// Tier400:清空 ChatWindow + Optional 段抑制(BuildUserPrompt 不再含画像等)。
	ctx5 := &GameContextForAgent{ChatWindow: window, MySeat: 0, Street: "river"}
	mem5 := NewMemory()
	mem5.IncrementHandsPlayed("opp1")
	mem5.UpdateOpponentStat("opp1", 1, "fold")
	ApplyPromptCompression(ctx5, mem5, Tier400, "")
	if ctx5.ChatWindow != "" {
		t.Errorf("Tier400 chat window must be cleared, got %q", ctx5.ChatWindow)
	}
	if !ctx5.suppressOptionalBlocks {
		t.Error("Tier400 must set suppressOptionalBlocks")
	}
	up := BuildUserPrompt(ctx5, mem5)
	if strings.Contains(up, "【玩家画像】") {
		t.Error("Tier400 must drop optional reputation block")
	}
	if !strings.Contains(up, "【本手动作】") {
		t.Error("Tier400 must keep critical action history block")
	}
}

func TestIsContextExceededError(t *testing.T) {
	if !IsContextExceededError(errors.New("prompt is too long: 40000 > 8192 tokens (context length)")) {
		t.Error("context length error should be detected")
	}
	if IsContextExceededError(errors.New("connection refused")) {
		t.Error("network error must not be detected as context exceeded")
	}
	if IsContextExceededError(nil) {
		t.Error("nil error must not be detected")
	}
}

func TestCompressChatWindowLLM_FallbackOnError(t *testing.T) {
	// provider 失败 → 返回空串(ApplyPromptCompression 回退规则式)。
	p := &fakeProvider{err: errors.New("boom")}
	if got := compressChatWindowLLM(context.Background(), p, "key", "ModelA", "a: b"); got != "" {
		t.Errorf("compressChatWindowLLM on error should return empty, got %q", got)
	}
	// provider 成功 → 返回文本(resp.Text())。
	p2 := &fakeProvider{resp: types.LLMResponse{Content: []types.ContentBlock{{Type: "text", Text: " 摘要内容 "}}}}
	if got := compressChatWindowLLM(context.Background(), p2, "key", "ModelA", "a: b"); got != "摘要内容" {
		t.Errorf("compressChatWindowLLM should return trimmed text, got %q", got)
	}
	// 空窗口 → 直接空串。
	if got := compressChatWindowLLM(context.Background(), p2, "key", "ModelA", "  "); got != "" {
		t.Errorf("empty window should return empty, got %q", got)
	}
}

// ─────────────────── §3.2 HandOverChat ───────────────────

func TestHandOverChat_RateLimitedAndLLM(t *testing.T) {
	d := NewDriver()
	p := &fakeProvider{resp: types.LLMResponse{Content: []types.ContentBlock{{Type: "text", Text: "哎呀被河杀了吧"}}}}
	registerAgentWithProvider(t, d, "r1", 0, p)

	ctx := context.Background()
	got := d.HandOverChat(ctx, "r1", 0, 7, false, -300)
	if got != "哎呀被河杀了吧" {
		t.Errorf("HandOverChat should return LLM text, got %q", got)
	}
	// 立即第二次 → 限流(20s 间隔)拒绝,静默返回空串。
	if got := d.HandOverChat(ctx, "r1", 0, 8, false, -100); got != "" {
		t.Errorf("rate-limited HandOverChat should return empty, got %q", got)
	}
	// LLM 失败 → 静默空串。
	d2 := NewDriver()
	registerAgentWithProvider(t, d2, "r1", 0, &fakeProvider{err: errors.New("down")})
	if got := d2.HandOverChat(ctx, "r1", 0, 9, true, 500); got != "" {
		t.Errorf("failed LLM HandOverChat should return empty, got %q", got)
	}
	// 未注册座位 → 空串。
	if got := d.HandOverChat(ctx, "r1", 5, 1, true, 0); got != "" {
		t.Errorf("unregistered seat should return empty, got %q", got)
	}
}

// ─────────────────── §3.4 MemoryIter 纯函数 ───────────────────

func TestTexasMemoryIterPromptAndValidation(t *testing.T) {
	p := BuildTexasMemoryIterPrompt("", "本局赢了 3 手", false)
	if !strings.Contains(p, "## 风格画像") || !strings.Contains(p, "## 对手笔记") {
		t.Error("iteration prompt must contain both section titles")
	}
	pc := BuildTexasMemoryIterPrompt(strings.Repeat("x", 90000), "facts", true)
	if !strings.Contains(pc, "压缩指令") {
		t.Error("compress prompt missing for >80K old memory")
	}

	good := "# Texas Hold'em Agent 长期记忆\n\n## 风格画像\n紧凶\n\n## 对手笔记\n暂无\n"
	if !ValidateTexasMemorySections(good) {
		t.Error("valid memory should pass validation")
	}
	if ValidateTexasMemorySections("## 风格画像\nonly one section") {
		t.Error("missing section should fail validation")
	}
}

func TestTexasFallbackMerge(t *testing.T) {
	merged := TexasFallbackMerge("", "本局净赢 5000")
	if !ValidateTexasMemorySections(merged) || !strings.Contains(merged, "本局净赢 5000") {
		t.Errorf("fallback merge on empty should build skeleton with note:\n%s", merged)
	}
	old := goodTexasMemory()
	merged2 := TexasFallbackMerge(old, "note-line")
	if !strings.HasPrefix(merged2, old) || !strings.Contains(merged2, "- note-line") {
		t.Error("fallback merge on old memory must keep old content + append note")
	}
}

func goodTexasMemory() string {
	return "# Texas Hold'em Agent 长期记忆\n\n## 风格画像\n暂无\n\n## 对手笔记\n暂无\n"
}

func TestTexasHardTruncate(t *testing.T) {
	long := strings.Repeat("记", 100000) // 300000 bytes
	out := TexasHardTruncate(long, TexasMemoryMaxBytes)
	if len(out) > TexasMemoryMaxBytes+64 { // 允许追加的截断尾注
		t.Errorf("hard truncate result too large: %d bytes", len(out))
	}
	if TexasHardTruncate("short", TexasMemoryMaxBytes) != "short" {
		t.Error("short memory must pass through unchanged")
	}
}

// ─────────────────── §3.4/§24 AgentClassName 接线(§130) ───────────────────

func TestAgentClassTexasPokerMemoryIter_Wired(t *testing.T) {
	if agentroot.AgentClassTexasPokerMemoryIter == "" {
		t.Fatal("AgentClassTexasPokerMemoryIter must be non-empty (§24)")
	}
	if string(agentroot.AgentClassTexasPokerMemoryIter) != "LsmAgentGame-TexasPoker-MemoryIter" {
		t.Errorf("unexpected AgentClassName: %q", agentroot.AgentClassTexasPokerMemoryIter)
	}
	found := false
	for _, c := range agentroot.AllAgentClassNames() {
		if c == agentroot.AgentClassTexasPokerMemoryIter {
			found = true
		}
	}
	if !found {
		t.Error("AgentClassTexasPokerMemoryIter must be registered in AllAgentClassNames()")
	}
	if !agentroot.IsValidAgentClassName(string(agentroot.AgentClassTexasPokerMemoryIter)) {
		t.Error("IsValidAgentClassName must accept the MemoryIter class")
	}
}

// RoomMemorySnapshots 快照接线(§3.4:局末 MemoryIter 的事实来源)。
func TestRoomMemorySnapshots(t *testing.T) {
	d := NewDriver()
	registerAgentWithProvider(t, d, "r1", 1, &fakeProvider{})
	d.RecordPlayerAction("r1", 1, "bot-seat", "call", 100, "flop")
	snaps := d.RoomMemorySnapshots("r1")
	if len(snaps) != 1 || snaps[0].Seat != 1 || snaps[0].ModelKey != "ModelA" {
		t.Fatalf("unexpected snapshots: %+v", snaps)
	}
	if !strings.Contains(snaps[0].Facts, "2 号位") || !strings.Contains(snaps[0].Facts, "ModelA") {
		t.Errorf("facts missing seat/model: %q", snaps[0].Facts)
	}
	if len(d.RoomMemorySnapshots("no-such-room")) != 0 {
		t.Error("unknown room must return empty snapshots")
	}
}

// 防御:time import 仅在需要时使用(保持与现有测试文件一致)。
var _ = time.Now
