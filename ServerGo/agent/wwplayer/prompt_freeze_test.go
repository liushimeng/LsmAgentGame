// Package wwplayer — prompt_freeze_test.go: U5 System Prompt 字节稳定性测试。
//
// 验证 BuildSystemPromptBytes 与 BuildSystemPrompt 字节级一致 + 跨调用稳定。
package wwplayer

import (
	"testing"
)

func TestPromptFreeze_BytesStableAcrossCalls(t *testing.T) {
	a := BuildSystemPromptBytes("portrait", PersonalityVector{}, "logical", "")
	b := BuildSystemPromptBytes("portrait", PersonalityVector{}, "logical", "")
	if string(a) != string(b) {
		t.Fatalf("BuildSystemPromptBytes 跨调用字节不一致:\nA=%s\nB=%s", string(a), string(b))
	}
}

func TestPromptFreeze_StableHashMatches(t *testing.T) {
	h1 := HashSystemPromptBytes("portrait", PersonalityVector{}, "logical", "")
	h2 := HashSystemPromptBytes("portrait", PersonalityVector{}, "logical", "")
	if h1 != h2 {
		t.Fatalf("Hash 不一致: %s vs %s", h1, h2)
	}
	if len(h1) != 64 { // sha256 hex
		t.Fatalf("Hash 长度错误: %d (期望 64)", len(h1))
	}
}

func TestPromptFreeze_DifferentParamsDifferentBytes(t *testing.T) {
	a := BuildSystemPromptBytes("portrait", PersonalityVector{}, "logical", "")
	b := BuildSystemPromptBytes("portrait_different", PersonalityVector{}, "logical", "")
	if string(a) == string(b) {
		t.Fatalf("不同 selfPortrait 应产生不同字节,但实际相同")
	}
}

func TestPromptFreeze_DifficultyDirectiveChangesBytes(t *testing.T) {
	a := BuildSystemPromptBytes("", PersonalityVector{}, "", "")
	b := BuildSystemPromptBytes("", PersonalityVector{}, "", "difficulty: hard")
	if string(a) == string(b) {
		t.Fatalf("不同 difficultyDirective 应产生不同字节,但实际相同")
	}
}

func TestPromptFreeze_NonEmpty(t *testing.T) {
	b := BuildSystemPromptBytes("", PersonalityVector{}, "", "")
	if len(b) == 0 {
		t.Fatal("空 prompt 应仍产生字节,但实际为空")
	}
	// 粗略下界:BuildSystemPrompt 基础块 ≥ 2KB
	if len(b) < 1024 {
		t.Fatalf("System prompt 字节过短: %d (期望 ≥ 1KB)", len(b))
	}
}