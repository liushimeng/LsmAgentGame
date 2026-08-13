// Package wwplayer — memory_retention_test.go: §20260813-02 U4 保留率校验测试。
//
// 对齐 OpenClaw maxPriorEntryLossFraction:ValidateMemorySections 只保证 4 段
// 标题存在,无法识别「标题都在但正文被 LLM 截掉大半」的截断事故。
// ValidateMemoryRetention 补这一层:新记忆 rune 数 < 旧记忆 50%
// (旧记忆 >80K 的压缩场景放宽到 30%)→ 视为截断事故,调用方回退 FallbackMerge。
package wwplayer

import (
	"strings"
	"testing"
)

// TestMemoryRetention_U4_01_Boundary 50% 边界:49% 拒绝,51% 通过。
func TestMemoryRetention_U4_01_Boundary(t *testing.T) {
	old := strings.Repeat("记", 1000) // 1000 runes,远小于 80K
	if err := ValidateMemoryRetention(old, strings.Repeat("新", 490)); err == nil {
		t.Fatal("49% retention must be rejected as truncation accident")
	}
	if err := ValidateMemoryRetention(old, strings.Repeat("新", 510)); err != nil {
		t.Fatalf("51%% retention must pass, got: %v", err)
	}
	// 恰好 50% 通过(下限是「小于」才拒绝)。
	if err := ValidateMemoryRetention(old, strings.Repeat("新", 500)); err != nil {
		t.Fatalf("exactly 50%% retention must pass, got: %v", err)
	}
}

// TestMemoryRetention_U4_02_CompressRelaxation 旧记忆 >80K(迭代 prompt 带压缩
// 指令)→ 下限放宽到 30%:35% 通过,25% 拒绝。
func TestMemoryRetention_U4_02_CompressRelaxation(t *testing.T) {
	// 构造 > 80K 字节(81920)的旧记忆。中文 1 rune = 3 bytes,
	// 28000 runes ≈ 84000 bytes > 81920。
	old := strings.Repeat("记", 28000)
	oldRunes := len([]rune(old))
	if len(old) <= MemoryCompressThresholdBytes {
		t.Fatalf("test setup: old memory %d bytes must exceed %d", len(old), MemoryCompressThresholdBytes)
	}
	if err := ValidateMemoryRetention(old, strings.Repeat("新", int(float64(oldRunes)*0.35))); err != nil {
		t.Fatalf("35%% retention with >80K old must pass (compress relaxation), got: %v", err)
	}
	if err := ValidateMemoryRetention(old, strings.Repeat("新", int(float64(oldRunes)*0.25))); err == nil {
		t.Fatal("25% retention with >80K old must be rejected (below 30% floor)")
	}
}

// TestMemoryRetention_U4_03_EmptyOld 旧记忆为空(首局)恒通过 —— 无内容可丢。
func TestMemoryRetention_U4_03_EmptyOld(t *testing.T) {
	if err := ValidateMemoryRetention("", "任意新内容"); err != nil {
		t.Fatalf("empty old memory must always pass, got: %v", err)
	}
	if err := ValidateMemoryRetention("", ""); err != nil {
		t.Fatalf("both empty must pass, got: %v", err)
	}
}
