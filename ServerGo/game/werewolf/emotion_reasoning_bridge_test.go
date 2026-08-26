// Package werewolf — emotion_reasoning_bridge_test.go: 情绪→推理桥单测（§20260826-01 U4）。
//
// 测试矩阵:
//   T1: wary 情绪 → confidence 下限钳制 60
//   T2: guilty 情绪 → confidence 上限钳制 50
//   T3: irritated → ThreatMultiplier 1.5x
//   T4: excited → TrustMultiplier 1.3x
//   T5: 默认 emotion → 不钳制, 倍率 1.0
package werewolf

import "testing"

// T1: wary floor=60。
func TestEmotionBridge_WaryFloor(t *testing.T) {
	got := ApplyEmotionToHypothesisConfidence(30, "wary")
	if got != 60 {
		t.Errorf("wary 30 → %d, want 60", got)
	}
	if got := ApplyEmotionToHypothesisConfidence(75, "wary"); got != 75 {
		t.Errorf("wary 75 should not clamp up, got %d", got)
	}
}

// T2: guilty ceil=50。
func TestEmotionBridge_GuiltyCeil(t *testing.T) {
	got := ApplyEmotionToHypothesisConfidence(80, "guilty")
	if got != 50 {
		t.Errorf("guilty 80 → %d, want 50", got)
	}
	if got := ApplyEmotionToHypothesisConfidence(30, "guilty"); got != 30 {
		t.Errorf("guilty 30 should not clamp down, got %d", got)
	}
}

// T3: irritated → Threat 1.5x。
func TestEmotionBridge_IrritatedThreatMult(t *testing.T) {
	w := weightsForEmotion("irritated")
	if w.ThreatMultiplier != 1.5 {
		t.Errorf("irritated ThreatMult=%.2f, want 1.5", w.ThreatMultiplier)
	}
}

// T4: excited → Trust 1.3x。
func TestEmotionBridge_ExcitedTrustMult(t *testing.T) {
	w := weightsForEmotion("excited")
	if w.TrustMultiplier != 1.3 {
		t.Errorf("excited TrustMult=%.2f, want 1.3", w.TrustMultiplier)
	}
}

// T5: 未知 emotion → 不影响。
func TestEmotionBridge_UnknownNoOp(t *testing.T) {
	got := ApplyEmotionToHypothesisConfidence(50, "totally_made_up")
	if got != 50 {
		t.Errorf("unknown emotion 50 → %d, want 50", got)
	}
	w := weightsForEmotion("calm")
	if w.ThreatMultiplier != 1.0 || w.TrustMultiplier != 1.0 {
		t.Errorf("calm should be 1.0/1.0, got threat=%.2f trust=%.2f", w.ThreatMultiplier, w.TrustMultiplier)
	}
}

// 附加: ApplyEmotionToHypothesisEntryLocked 批量处理。
func TestEmotionBridge_ApplyEntries(t *testing.T) {
	entries := []HypothesisEntry{
		{Confidence: 20},
		{Confidence: 90},
	}
	out := ApplyEmotionToHypothesisEntryLocked(entries, "wary")
	if out[0].Confidence != 60 || out[1].Confidence != 90 {
		t.Errorf("wary batch: got [%d,%d], want [60,90]", out[0].Confidence, out[1].Confidence)
	}
}