package wwplayer_test

import (
	"strings"
	"sync"
	"testing"

	"LsmAgentGame/agent/wwplayer"
	"LsmAgentGame/agent/wwtypes"
)

// TestEmotionConstants_AllTen 是 §124 情绪模块基础不变量测试:
// 必须恰好有 10 类情绪,且 AllEmotions 与 emotionMetas 同步。
func TestEmotionConstants_AllTen(t *testing.T) {
	if len(wwplayer.AllEmotions) != 10 {
		t.Fatalf("AllEmotions should have 10 entries, got %d", len(wwplayer.AllEmotions))
	}
	for _, e := range wwplayer.AllEmotions {
		if !wwplayer.IsValidEmotion(e) {
			t.Errorf("AllEmotions entry %q not in emotionMetas", e)
		}
	}
}

// TestEmotionMetas_HaveRequiredFields 验证每个情绪都有中文名/极性/唤醒度。
func TestEmotionMetas_HaveRequiredFields(t *testing.T) {
	requiredPolarities := map[string]bool{"positive": true, "negative": true, "neutral": true}
	requiredArousals := map[string]bool{"high": true, "medium": true, "low": true}
	for _, e := range wwplayer.AllEmotions {
		m := wwplayer.EmotionMeta(e)
		if m.Name == "" || m.Name == e {
			t.Errorf("emotion %q has empty/default name", e)
		}
		if !requiredPolarities[m.Polarity] {
			t.Errorf("emotion %q has invalid polarity %q", e, m.Polarity)
		}
		if !requiredArousals[m.Arousal] {
			t.Errorf("emotion %q has invalid arousal %q", e, m.Arousal)
		}
		if m.Emoji == "" {
			t.Errorf("emotion %q has empty emoji", e)
		}
		if m.SpeechStyle == "" {
			t.Errorf("emotion %q has empty speech style", e)
		}
		if m.DecisionStyle == "" {
			t.Errorf("emotion %q has empty decision style", e)
		}
		if m.Color == "" {
			t.Errorf("emotion %q has empty color", e)
		}
	}
}

// TestIsValidEmotion_UnknownKey 验证未知情绪 key 返 false。
func TestIsValidEmotion_UnknownKey(t *testing.T) {
	if wwplayer.IsValidEmotion("unknown_emotion") {
		t.Error("expected unknown_emotion to be invalid")
	}
	if wwplayer.IsValidEmotion("") {
		t.Error("expected empty string to be invalid")
	}
}

// TestPickRandomEmotion_ReturnsValid 验证随机抽取总是返回有效情绪。
func TestPickRandomEmotion_ReturnsValid(t *testing.T) {
	for i := 0; i < 100; i++ {
		e := wwplayer.PickRandomEmotion()
		if !wwplayer.IsValidEmotion(e) {
			t.Fatalf("PickRandomEmotion returned invalid emotion %q", e)
		}
	}
}

// TestPickInitialEmotion_Distribution 验证 pickInitialEmotion 的 1000 次
// 抽取应该覆盖所有 10 类情绪(统计意义)。
func TestPickInitialEmotion_Distribution(t *testing.T) {
	counts := make(map[string]int)
	for i := 0; i < 1000; i++ {
		counts[wwplayer.PickInitialEmotionForTest("villager")]++
	}
	for _, e := range wwplayer.AllEmotions {
		if counts[e] == 0 {
			t.Errorf("expected emotion %q to be picked at least once in 1000 trials (got %v)", e, counts)
		}
	}
}

// TestEmotionStyleBlock_HasContent 验证渲染块非空且含关键字段。
func TestEmotionStyleBlock_HasContent(t *testing.T) {
	for _, e := range wwplayer.AllEmotions {
		block := wwplayer.EmotionStyleBlock(e)
		if block == "" {
			t.Errorf("EmotionStyleBlock(%q) returned empty", e)
		}
		if !strings.Contains(block, "情绪") {
			t.Errorf("EmotionStyleBlock(%q) missing '情绪' keyword", e)
		}
	}
}

// TestEmotionStyleBlock_UnknownReturnsEmpty 验证未知 key 返空(避免在 prompt 渲染 "未知:xxx")。
func TestEmotionStyleBlock_UnknownReturnsEmpty(t *testing.T) {
	if got := wwplayer.EmotionStyleBlock("unknown"); got != "" {
		t.Errorf("expected empty block for unknown emotion, got %q", got)
	}
}

// TestMyEmotionBlock_IncludesName 验证 MyEmotionBlock 含中文名 + reason。
func TestMyEmotionBlock_IncludesName(t *testing.T) {
	block := wwplayer.MyEmotionBlock("panic", "被质疑身份")
	if !strings.Contains(block, "紧张恐慌") {
		t.Errorf("expected '紧张恐慌' in block, got %q", block)
	}
	if !strings.Contains(block, "被质疑身份") {
		t.Errorf("expected reason in block, got %q", block)
	}
}

// TestOthersEmotionBlock_RendersAll 验证多座位渲染。
func TestOthersEmotionBlock_RendersAll(t *testing.T) {
	others := []wwtypes.SeatEmotionBrief{
		{Seat: 0, Emotion: "confident", Reason: "查杀顺利"},
		{Seat: 1, Emotion: "panic", Reason: "被质疑"},
		{Seat: 2, Emotion: "guilty", Reason: "撒谎心虚"},
	}
	block := wwplayer.OthersEmotionBlock(others)
	if !strings.Contains(block, "1号") || !strings.Contains(block, "2号") || !strings.Contains(block, "3号") {
		t.Errorf("expected all seat labels, got %q", block)
	}
	if !strings.Contains(block, "自信从容") || !strings.Contains(block, "紧张恐慌") || !strings.Contains(block, "心虚愧疚") {
		t.Errorf("expected all emotion names, got %q", block)
	}
}

// TestOthersEmotionBlock_EmptyList 验证空列表返空(避免空块污染 prompt)。
func TestOthersEmotionBlock_EmptyList(t *testing.T) {
	if got := wwplayer.OthersEmotionBlock(nil); got != "" {
		t.Errorf("expected empty block for nil list, got %q", got)
	}
	if got := wwplayer.OthersEmotionBlock([]wwtypes.SeatEmotionBrief{}); got != "" {
		t.Errorf("expected empty block for empty list, got %q", got)
	}
}

// TestAgentEmotion_SwitchAndRead 集成测试:模拟 Agent.SwitchEmotion 后
// getter 读出正确情绪 + reason + history 追加。
//
// 由于 NewWithRoom 需要 registry 等额外依赖,这里直接测试 SwitchEmotion 的
// 字段读写 — Agent.emotion 字段是导出给同包的私有字段,可通过 NewWithRoom
// 初始化后由 getter 读出。但 NewWithRoom 需要 registry,我们用 nil-safe 的
// SwitchEmotion 配合简单的 Agent{} 即可(emotion 是值类型,无 nil 风险)。
func TestAgentEmotion_SwitchAndRead(t *testing.T) {
	// 简化 Agent(emotion 字段可直接访问;无外部依赖)。
	a := &wwplayer.Agent{}
	a.SwitchEmotion("panic", "被质疑身份")
	if got := a.CurrentEmotion(); got != "panic" {
		t.Errorf("CurrentEmotion() = %q, want panic", got)
	}
	if got := a.CurrentEmotionReason(); got != "被质疑身份" {
		t.Errorf("CurrentEmotionReason() = %q, want 被质疑身份", got)
	}
	if got := a.EmotionUpdatedAtMs(); got <= 0 {
		t.Errorf("EmotionUpdatedAtMs() = %d, want > 0", got)
	}
	hist := a.EmotionHistoryCopy()
	if len(hist) != 1 {
		t.Fatalf("EmotionHistoryCopy() len = %d, want 1", len(hist))
	}
	if hist[0].Emotion != "panic" {
		t.Errorf("history[0].Emotion = %q, want panic", hist[0].Emotion)
	}
}

// TestAgentEmotion_HistoryFIFOLimit 验证超过 emotionHistoryMaxLen (5) 时
// 按 FIFO 淘汰。
func TestAgentEmotion_HistoryFIFOLimit(t *testing.T) {
	a := &wwplayer.Agent{}
	for i, e := range []string{"confident", "excited", "calm", "panic", "wary", "irritated", "grievance"} {
		a.SwitchEmotion(e, "switch "+itoaSimple(i))
	}
	hist := a.EmotionHistoryCopy()
	if len(hist) != 5 {
		t.Fatalf("expected history to be capped at 5, got %d", len(hist))
	}
	// 最早的两条(confident/excited)已被淘汰;首条应为 calm。
	if hist[0].Emotion != "calm" {
		t.Errorf("hist[0].Emotion = %q, want calm (FIFO 淘汰 confident/excited)", hist[0].Emotion)
	}
	if hist[4].Emotion != "grievance" {
		t.Errorf("hist[4].Emotion = %q, want grievance (latest)", hist[4].Emotion)
	}
}

// TestAgentEmotion_UnknownEmotionNoOp 验证 SwitchEmotion 对未知 key 不修改状态。
func TestAgentEmotion_UnknownEmotionNoOp(t *testing.T) {
	a := &wwplayer.Agent{}
	a.SwitchEmotion("panic", "init")
	a.SwitchEmotion("unknown_emotion", "should be ignored")
	if got := a.CurrentEmotion(); got != "panic" {
		t.Errorf("CurrentEmotion() = %q, want panic (unchanged)", got)
	}
}

// TestAgentEmotion_ReasonTruncation 验证 reason 被截断到 80 字(后跟省略号)。
func TestAgentEmotion_ReasonTruncation(t *testing.T) {
	a := &wwplayer.Agent{}
	longReason := strings.Repeat("啊", 200) // 200 chars
	a.SwitchEmotion("calm", longReason)
	// wwplayer.truncate(s, n) returns up to n runes + "…" suffix,so len ≤ 81.
	if got := a.CurrentEmotionReason(); len([]rune(got)) > 81 {
		t.Errorf("reason len = %d, want ≤ 81 (80 chars + '…')", len([]rune(got)))
	}
}

// TestAgentEmotion_ConcurrentSafe 验证 SwitchEmotion + CurrentEmotion 并发安全
// (用 race detector 跑;失败时会报 race 警告)。
func TestAgentEmotion_ConcurrentSafe(t *testing.T) {
	a := &wwplayer.Agent{}
	a.SwitchEmotion("calm", "init")
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			a.SwitchEmotion(wwplayer.AllEmotions[i%len(wwplayer.AllEmotions)], "writer")
		}(i)
		go func() {
			defer wg.Done()
			_ = a.CurrentEmotion()
		}()
	}
	wg.Wait()
	if !wwplayer.IsValidEmotion(a.CurrentEmotion()) {
		t.Errorf("after concurrent writes, emotion %q is invalid", a.CurrentEmotion())
	}
}

// TestSwitchEmotionFx_DurationSecToMs 验证 root-cause-5 防御:
// fx.DurationSec(秒) 必须按 *1000 换算为 ms 写入 fxDurationMs,
// 且 clamp 后的值域对应 [8000,30000] ms。防止后续开发者把 ms 当秒传入。
func TestSwitchEmotionFx_DurationSecToMs(t *testing.T) {
	a := &wwplayer.Agent{}

	// 正常 12s → 12000ms
	a.SwitchEmotionFx("panic", "被质疑", wwplayer.EmotionFx{Effect: "sweat", DurationSec: 12})
	_, started, dur := a.CurrentEmotionFx()
	if started == 0 {
		t.Fatalf("expected fxStartedAtMs > 0")
	}
	if dur != 12000 {
		t.Errorf("DurationSec=12 应 → fxDurationMs=12000,实际 %d(单位混用!)", dur)
	}

	// clamp 上界:100s → 30000ms
	a.SwitchEmotionFx("calm", "恢复", wwplayer.EmotionFx{Effect: "pulse", DurationSec: 100})
	_, _, dur = a.CurrentEmotionFx()
	if dur != 30000 {
		t.Errorf("DurationSec=100 应 clamp 到 30000ms,实际 %d", dur)
	}

	// clamp 下界:1s → 8000ms
	a.SwitchEmotionFx("confident", "带队", wwplayer.EmotionFx{Effect: "pulse", DurationSec: 1})
	_, _, dur = a.CurrentEmotionFx()
	if dur != 8000 {
		t.Errorf("DurationSec=1 应 clamp 到 8000ms,实际 %d", dur)
	}

	// 零值:SwitchEmotion 兼容入口(SwitchEmotion → SwitchEmotionFx(_,_,EmotionFx{}))
	// 语义为「只切情绪、不带特效」,fx 时间戳清零(清除上一轮特效)。
	a.SwitchEmotionFx("excited", "冲票", wwplayer.EmotionFx{})
	_, now2, dur2 := a.CurrentEmotionFx()
	if now2 != 0 || dur2 != 0 {
		t.Errorf("零值 EmotionFx 应清除 fx 时间戳; 实际 started=%d dur=%d", now2, dur2)
	}
}

// itoaSimple 是 strconv.Itoa 的本地副本,避免 emotion_test.go 多余 import。
func itoaSimple(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	buf := make([]byte, 0, 8)
	for i > 0 {
		buf = append([]byte{byte('0' + i%10)}, buf...)
		i /= 10
	}
	if neg {
		buf = append([]byte{'-'}, buf...)
	}
	return string(buf)
}