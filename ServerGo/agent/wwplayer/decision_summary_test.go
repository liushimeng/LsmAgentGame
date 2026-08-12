// Package agent — decision_summary_test.go: 单元测试覆盖
// 2026-07-09 §重构 - "Agent 思考 → Agent 交互" 的决策可观测性字段。
//
// 覆盖:
//   - BuildInputSummary: wwtypes.GameContext → 决策输入数字摘要
//   - BuildDecisionSummary: tool_use + tool_result → 1 句话总结
//   - SanitizeToolInput: 敏感字段脱敏
//   - recordTranscript: BotTranscript 填充新字段(替代旧 LastThinking 置空)
package wwplayer

import (
	"encoding/json"
	"LsmAgentGame/agent/core"
	"LsmAgentGame/agent/wwtypes"
	"strings"
	"testing"
)

// TestBuildInputSummary_EmptyPhase 测试 Phase 为空时返回空字符串。
func TestBuildInputSummary_EmptyPhase(t *testing.T) {
	got := BuildInputSummary(wwtypes.GameContext{Phase: ""})
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

// TestBuildInputSummary_AllFields 测试所有字段都被填入摘要。
func TestBuildInputSummary_AllFields(t *testing.T) {
	got := BuildInputSummary(wwtypes.GameContext{
		Phase:          "speak",
		Role:           "seer",
		Round:          3,
		AliveSeats:     []int{0, 1, 2, 3, 4},
		RecentSpeeches: make([]wwtypes.SpeechEvent, 12),
		WhisperInbox:   make([]wwtypes.WhisperEvent, 2),
		ChatHistory:    make([]agentcore.ChatMessage, 50),
		MyTurn:         true,
	})
	for _, expect := range []string{"阶段:", "角色:seer", "第3天", "存活5", "发言+12", "私聊+2", "500K+50", "轮到我"} {
		if !strings.Contains(got, expect) {
			t.Errorf("BuildInputSummary missing %q in output: %q", expect, got)
		}
	}
}

// TestBuildInputSummary_LengthLimit 测试摘要 ≤ 200 字。
func TestBuildInputSummary_LengthLimit(t *testing.T) {
	got := BuildInputSummary(wwtypes.GameContext{
		Phase:          "speak",
		Role:           "witch",
		Round:          99,
		AliveSeats:     []int{0, 1, 2, 3, 4, 5, 6},
		RecentSpeeches: make([]wwtypes.SpeechEvent, 9999),
		WhisperInbox:   make([]wwtypes.WhisperEvent, 9999),
		ChatHistory:    make([]agentcore.ChatMessage, 9999),
		MyTurn:         true,
	})
	if len([]rune(got)) > 201 { // 200 + 省略号
		t.Errorf("BuildInputSummary too long: %d runes", len([]rune(got)))
	}
}

// TestBuildDecisionSummary_Speak 测试 speak 工具生成"工具(目标+1号):文本"格式。
func TestBuildDecisionSummary_Speak(t *testing.T) {
	got := BuildDecisionSummary("speak", map[string]any{
		"target": 2,
		"text":   "我怀疑 4 号是狼人",
	}, "")
	if !strings.Contains(got, "speak(3号)") {
		t.Errorf("speak summary missing seat+1: %q", got)
	}
	if !strings.Contains(got, "我怀疑 4 号是狼人") {
		t.Errorf("speak summary missing text: %q", got)
	}
}

// TestBuildDecisionSummary_Vote 测试 vote 工具生成"vote → 目标+1号"格式。
func TestBuildDecisionSummary_Vote(t *testing.T) {
	got := BuildDecisionSummary("vote", map[string]any{"target": 4}, "")
	if got != "vote → 5号" {
		t.Errorf("vote summary wrong: %q", got)
	}
}

// TestBuildDecisionSummary_WolfKill 测试 wolf_kill 工具生成"wolf_kill → 目标+1号"格式。
func TestBuildDecisionSummary_WolfKill(t *testing.T) {
	got := BuildDecisionSummary("wolf_kill", map[string]any{"target": 6}, "")
	if got != "wolf_kill → 7号" {
		t.Errorf("wolf_kill summary wrong: %q", got)
	}
}

// TestBuildDecisionSummary_Idle 测试 idle 工具生成"idle (沉默思考)"。
func TestBuildDecisionSummary_Idle(t *testing.T) {
	got := BuildDecisionSummary("idle_think", nil, "")
	if !strings.Contains(got, "idle") {
		t.Errorf("idle summary wrong: %q", got)
	}
}

// TestBuildDecisionSummary_EmptyToolName 测试空工具名返回空字符串。
func TestBuildDecisionSummary_EmptyToolName(t *testing.T) {
	got := BuildDecisionSummary("", nil, "")
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

// TestSanitizeToolInput_WolfKillTarget 测试 wolf_kill.target 被脱敏。
func TestSanitizeToolInput_WolfKillTarget(t *testing.T) {
	got := SanitizeToolInput("wolf_kill", map[string]any{
		"target": 3,
		"reason": "他昨晚预言了 1 号",
	})
	var parsed map[string]any
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, got)
	}
	if parsed["target"] != "[已隐藏]" {
		t.Errorf("target should be [已隐藏], got %v", parsed["target"])
	}
	if parsed["reason"] != "他昨晚预言了 1 号" {
		t.Errorf("reason should not be sanitized, got %v", parsed["reason"])
	}
}

// TestSanitizeToolInput_SeerCheck 测试 seer_check.target 被脱敏。
func TestSanitizeToolInput_SeerCheck(t *testing.T) {
	got := SanitizeToolInput("seer_check", map[string]any{"target": 2})
	if !strings.Contains(got, "[已隐藏]") {
		t.Errorf("seer_check target not sanitized: %s", got)
	}
}

// TestSanitizeToolInput_NonSensitive 测试非敏感工具不被脱敏。
func TestSanitizeToolInput_NonSensitive(t *testing.T) {
	got := SanitizeToolInput("speak", map[string]any{
		"text":   "我投 2 号",
		"target": 1,
	})
	var parsed map[string]any
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if parsed["text"] != "我投 2 号" {
		t.Errorf("speak text should not be sanitized, got %v", parsed["text"])
	}
	if parsed["target"] != 1.0 { // JSON 数字反序列化为 float64
		t.Errorf("speak target should not be sanitized, got %v", parsed["target"])
	}
}

// TestSanitizeToolInput_NilInput 测试 nil 入参返回空字符串。
func TestSanitizeToolInput_NilInput(t *testing.T) {
	got := SanitizeToolInput("vote", nil)
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

// TestSanitizeToolInput_Whisper 测试 whisper 的 text / to_seat 都被脱敏。
func TestSanitizeToolInput_Whisper(t *testing.T) {
	got := SanitizeToolInput("whisper", map[string]any{
		"to_seat": 2,
		"text":    "我建议今晚杀 3 号",
	})
	var parsed map[string]any
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if parsed["to_seat"] != "[已隐藏]" {
		t.Errorf("whisper to_seat should be [已隐藏], got %v", parsed["to_seat"])
	}
	if parsed["text"] != "[已隐藏]" {
		t.Errorf("whisper text should be [已隐藏], got %v", parsed["text"])
	}
}

// TestRecordDecisionState_SetAndMerge 测试 SetLastDecision + MergeLastDecision
// 的合并语义:STAGE 1 设置 DecisionInputs,STAGE 4 调 MergeLastDecision
// 设置 LastTool* 不应清空 DecisionInputs。
func TestRecordDecisionState_SetAndMerge(t *testing.T) {
	// 构造一个 Agent 实例(不通过 New,因为测试不需要 registry)
	a := &Agent{Seat: 0, ModelKey: "test-model"}

	// STAGE 1:Memory push 之后设置 DecisionInputs
	a.MergeLastDecision(RecordDecisionState{
		DecisionInputs: "阶段:speak | 角色:seer | 第3天",
	})

	// STAGE 4:dispatch 完工具后设置决策输出
	a.MergeLastDecision(RecordDecisionState{
		LastToolName:   "speak",
		LastToolInput:  map[string]any{"text": "我投 2 号"},
		LastToolResult: "ok",
		LastOutcome:    "OK",
	})

	got := a.LastDecision()
	if got.DecisionInputs != "阶段:speak | 角色:seer | 第3天" {
		t.Errorf("DecisionInputs lost after MergeLastDecision: %q", got.DecisionInputs)
	}
	if got.LastToolName != "speak" {
		t.Errorf("LastToolName wrong: %q", got.LastToolName)
	}
	if got.LastOutcome != "OK" {
		t.Errorf("LastOutcome wrong: %q", got.LastOutcome)
	}
}

// TestRecordDecisionState_DefaultEmpty 测试默认零值时字段为空。
func TestRecordDecisionState_DefaultEmpty(t *testing.T) {
	a := &Agent{Seat: 0}
	got := a.LastDecision()
	if got.LastToolName != "" || got.LastToolInput != nil || got.LastOutcome != "" {
		t.Errorf("default RecordDecisionState should be empty, got %+v", got)
	}
}

// TestRecordTranscript_OldFieldsEmpty 测试 recordTranscript 后旧 LastThinking /
// FullThinking / RecentMessages / ToolCalls 字段已置空(2026-07-09 §重构)。
func TestRecordTranscript_OldFieldsEmpty(t *testing.T) {
	a := &Agent{
		Seat:     0,
		ModelKey: "test",
		Memory:   NewMemory("villager", "good", "放逐所有狼人", 0),
	}
	// 不调 New(没有 registry),直接构造

	// 模拟 STAGE 1 + STAGE 4 已经填充决策状态
	a.MergeLastDecision(RecordDecisionState{
		LastToolName:   "speak",
		LastToolInput:  map[string]any{"text": "我投 1 号"},
		LastToolResult: "ok",
		LastOutcome:    "OK",
		DecisionInputs: "阶段:speak | 角色:villager | 第1天",
	})

	a.recordTranscript()

	bt := a.BotTranscript()
	if bt == nil {
		t.Fatalf("BotTranscript is nil")
	}
	// §128 对话即思考重构:LastThinking / FullThinking / RecentMessages 字段已物理删除。
	if bt.LastDecisionSummary == "" {
		t.Errorf("LastDecisionSummary should be filled, got empty")
	}
	if bt.LastToolInput == "" {
		t.Errorf("LastToolInput should be filled, got empty")
	}
	if bt.LastOutcome != "OK" {
		t.Errorf("LastOutcome should be OK, got %q", bt.LastOutcome)
	}
	if bt.DecisionInputs == "" {
		t.Errorf("DecisionInputs should be filled, got empty")
	}
}

// TestRecordTranscript_QuarantineLastOutcome 测试 quarantine 时 LastOutcome 字段。
func TestRecordTranscript_QuarantineLastOutcome(t *testing.T) {
	a := &Agent{
		Seat:     1,
		ModelKey: "test",
		Memory:   NewMemory("werewolf", "wolf", "屠边", 1),
	}
	// 先创建 lastTranscript,再 quarantine — 与实际路径一致
	a.MergeLastDecision(RecordDecisionState{
		LastToolName:   "wolf_kill",
		LastToolInput:  map[string]any{"target": 3},
		LastOutcome:    "OK",
		DecisionInputs: "阶段:night_wolves",
	})
	a.recordTranscript()
	a.SetQuarantined()
	a.publishQuarantineTranscript()

	bt := a.BotTranscript()
	if bt == nil {
		t.Fatalf("BotTranscript is nil")
	}
	if !bt.Quarantined {
		t.Errorf("Quarantined should be true")
	}
	if bt.LastOutcome != "quarantine" {
		t.Errorf("LastOutcome should be quarantine, got %q", bt.LastOutcome)
	}
}

// §20260812-03 U4 — ExtractThreeReasons 单测。

// TestExtractThreeReasons_Basic 验证 1./2./3. 前缀正确截取 3 条理由。
func TestExtractThreeReasons_Basic(t *testing.T) {
	thought := "1. 三号发言矛盾\n2. 投票异常\n3. 昨夜被预言家跳"
	got := ExtractThreeReasons(thought)
	for _, want := range []string{"三号发言矛盾", "投票异常", "昨夜被预言家跳"} {
		if !strings.Contains(got, want) {
			t.Errorf("ExtractThreeReasons missing %q in %q", want, got)
		}
	}
	if !strings.Contains(got, "1.") || !strings.Contains(got, "2.") || !strings.Contains(got, "3.") {
		t.Errorf("ExtractThreeReasons missing numbered prefix: %q", got)
	}
}

// TestExtractThreeReasons_OnlyTwo 验证只识别到 2 条时只输出 2 条。
func TestExtractThreeReasons_OnlyTwo(t *testing.T) {
	thought := "1. 三号发言矛盾\n2. 投票异常\n（一些无编号的闲话）"
	got := ExtractThreeReasons(thought)
	if !strings.Contains(got, "三号发言矛盾") {
		t.Errorf("missing 1st reason: %q", got)
	}
	if !strings.Contains(got, "投票异常") {
		t.Errorf("missing 2nd reason: %q", got)
	}
	if strings.Contains(got, "闲话") {
		t.Errorf("unnumbered text should be excluded: %q", got)
	}
}

// TestExtractThreeReasons_NoNumber 验证无编号时退化为原文前 50 字。
func TestExtractThreeReasons_NoNumber(t *testing.T) {
	thought := "这是一段没有编号的内心独白,只是 LLM 想到的模糊推理。"
	got := ExtractThreeReasons(thought)
	if got != thought {
		t.Errorf("fallback should return original (≤50 chars), got %q", got)
	}
}

// TestExtractThreeReasons_Empty 验证空输入返回空串。
func TestExtractThreeReasons_Empty(t *testing.T) {
	if got := ExtractThreeReasons(""); got != "" {
		t.Errorf("empty input should return empty, got %q", got)
	}
}

// TestExtractThreeReasons_AlternateDelimiters 验证 "1、" "1)" "②" 等替代分隔符。
func TestExtractThreeReasons_AlternateDelimiters(t *testing.T) {
	thought := "1、A 是狼\n2) B 异常\n3、C 发言矛盾"
	got := ExtractThreeReasons(thought)
	if !strings.Contains(got, "A 是狼") {
		t.Errorf("missing 1st reason with 、 delimiter: %q", got)
	}
	if !strings.Contains(got, "B 异常") {
		t.Errorf("missing 2nd reason with ) delimiter: %q", got)
	}
	if !strings.Contains(got, "C 发言矛盾") {
		t.Errorf("missing 3rd reason: %q", got)
	}
}

// TestBuildDecisionSummary_SpeakWithThought 验证 speak_with_thought 走 3 条理由分支。
func TestBuildDecisionSummary_SpeakWithThought(t *testing.T) {
	got := BuildDecisionSummary("speak_with_thought", map[string]any{
		"target":          2,
		"text":            "我觉得 3 号是好人",
		"internal_thought": "1. 三号发言前后一致\n2. 投票偏向好人阵营\n3. 没有理由怀疑",
	}, "")
	if !strings.Contains(got, "speak_with_thought(3号)") {
		t.Errorf("missing seat prefix: %q", got)
	}
	if !strings.Contains(got, "三号发言前后一致") {
		t.Errorf("missing 1st reason: %q", got)
	}
	if !strings.Contains(got, "没有理由怀疑") {
		t.Errorf("missing 3rd reason: %q", got)
	}
}

// TestThreeReasonsBlock_OnlyOnKeyPhases 验证 ThreeReasonsBlock 仅在关键行动阶段输出。
func TestThreeReasonsBlock_OnlyOnKeyPhases(t *testing.T) {
	// 关键阶段:有内容
	phases := []string{"vote", "PhaseVote", "night_wolves", "night_seer", "night_witch", "night_guard", "hunter_shoot", "idiot_reveal"}
	for _, p := range phases {
		ctx := &wwtypes.GameContext{Phase: p}
		got := ThreeReasonsBlock(ctx)
		if got == "" {
			t.Errorf("phase %s should produce ThreeReasonsBlock, got empty", p)
		}
	}
	// 非关键阶段:空
	nonKey := []string{"speak", "PhaseSpeak", "start_night", "dawn", "over"}
	for _, p := range nonKey {
		ctx := &wwtypes.GameContext{Phase: p}
		got := ThreeReasonsBlock(ctx)
		if got != "" {
			t.Errorf("phase %s should NOT produce ThreeReasonsBlock, got %q", p, got)
		}
	}
}
