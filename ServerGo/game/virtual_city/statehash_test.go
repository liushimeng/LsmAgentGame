// Package virtual_city — statehash_test.go: 引擎状态哈希测试(批次71 §5 / §10.1)
//
// 覆盖方案 §10.1 的 B8 / B9 / B10:
//   - B8 同 seed 双跑 36 月逐月哈希一致;
//   - B9 不同 seed 哈希不同(防「哈希函数恒定」假绿);
//   - B10 调用 StateHash()/CheckInvariants() 不消费 w.Rand(零 rand 纪律)。
//
// B10 是本批**最重要的护栏**:新增的逐月审计绝不能移动既有随机序列。
package virtual_city

import (
	"testing"
)

// stateHashRunMonths 推进 n 个月并逐月记录状态哈希。
func stateHashRunMonths(w *World, n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, w.StateHash())
		finished, _ := w.SettleMonth()
		if finished {
			break
		}
	}
	return out
}

// TestStateHash_SameSeedSameHash 同 seed 双跑 36 月逐月哈希一致
// (方案 §10.1 B8;同时是 I5 的离线判据)。
func TestStateHash_SameSeedSameHash(t *testing.T) {
	a := calibTestWorld(2024, 6)
	b := calibTestWorld(2024, 6)
	ha := stateHashRunMonths(a, 36)
	hb := stateHashRunMonths(b, 36)
	if len(ha) != len(hb) {
		t.Fatalf("两轮月数不同:%d vs %d", len(ha), len(hb))
	}
	if len(ha) < 12 {
		t.Fatalf("只推进了 %d 月,样本不足", len(ha))
	}
	for i := range ha {
		if ha[i] != hb[i] {
			t.Fatalf("月 %d 哈希不一致:%s vs %s", i+1, ha[i], hb[i])
		}
	}
	// 末态哈希也必须一致。
	if a.StateHash() != b.StateHash() {
		t.Errorf("末态哈希不一致:%s vs %s", a.StateHash(), b.StateHash())
	}
}

// TestStateHash_DifferentSeedDiffers 不同 seed 哈希不同
// (方案 §10.1 B9;防「哈希函数恒定/忽略了大部分状态」假绿)。
func TestStateHash_DifferentSeedDiffers(t *testing.T) {
	a := calibTestWorld(2024, 6)
	b := calibTestWorld(2025, 6)
	ha := stateHashRunMonths(a, 8)
	hb := stateHashRunMonths(b, 8)
	same := 0
	for i := range ha {
		if ha[i] == hb[i] {
			same++
		}
	}
	if same == len(ha) {
		t.Errorf("不同 seed 的哈希完全相同(哈希恒定?):%v", ha)
	}
	if a.StateHash() == b.StateHash() {
		t.Errorf("不同 seed 末态哈希相同")
	}
}

// TestStateHash_NoRandConsumption 调用 StateHash()/CheckInvariants() 前后
// w.Rand 序列**不偏移**(方案 §10.1 B10;零 rand 硬纪律)。
//
// 实现:同 seed 两份世界 —— A 正常推进 18 月;B 在推进前反复调用
// StateHash()/CheckInvariants()(1000 次),再推进同样 18 月 ⇒
// 若审计/哈希消费了随机数,B 的随机序列会整体后移 ⇒ 末月哈希不同。
func TestStateHash_NoRandConsumption(t *testing.T) {
	a := calibTestWorld(99, 6)
	calibSettleMonths(a, 18)
	hashA := a.StateHash()

	b := calibTestWorld(99, 6)
	for i := 0; i < 1000; i++ {
		_ = b.StateHash()
		_ = CheckInvariants(b)
		_ = collectIndicators(b)
	}
	calibSettleMonths(b, 18)
	hashB := b.StateHash()
	if hashA != hashB {
		t.Errorf("StateHash/CheckInvariants 消费了随机数:hashA=%s hashB=%s", hashA, hashB)
	}
}

// TestStateHash_AuditDoesNotShiftSequence 逐月审计(含 auditMonth)同样
// 不得移动随机序列 —— 与 B10 互补(审计接线在月结内部,而非月结之外)。
func TestStateHash_AuditDoesNotShiftSequence(t *testing.T) {
	a := calibTestWorld(1234, 6)
	calibSettleMonths(a, 12)
	hashA := a.StateHash()

	b := calibTestWorld(1234, 6)
	for i := 0; i < 12; i++ {
		// 在月结**之前**额外做一次审计(模拟「重复审计」的最坏情况)。
		b.auditMonth()
		finished, _ := b.SettleMonth()
		if finished {
			break
		}
	}
	hashB := b.StateHash()
	if hashA != hashB {
		t.Errorf("重复审计移动了随机序列:hashA=%s hashB=%s", hashA, hashB)
	}
}

// TestStateHash_Format 哈希格式:16 位小写十六进制。
func TestStateHash_Format(t *testing.T) {
	w := calibTestWorld(42, 6)
	h := w.StateHash()
	if len(h) != 16 {
		t.Fatalf("哈希长度 = %d,期望 16:%s", len(h), h)
	}
	for i := 0; i < len(h); i++ {
		c := h[i]
		if !(c >= '0' && c <= '9') && !(c >= 'a' && c <= 'f') {
			t.Fatalf("哈希含非十六进制字符 %q:%s", c, h)
		}
	}
}

// TestStateHash_NilWorld nil World → 空串(nil 安全)。
func TestStateHash_NilWorld(t *testing.T) {
	var w *World
	if got := w.StateHash(); got != "" {
		t.Errorf("nil World 哈希 = %q,期望空串", got)
	}
}

// TestStateHash_StableAcrossCalls 同状态下重复调用哈希必须一致
// (排除 map 迭代序 / 浮点格式化等不稳定来源)。
func TestStateHash_StableAcrossCalls(t *testing.T) {
	w := calibTestWorld(42, 6)
	calibSettleMonths(w, 6)
	first := w.StateHash()
	for i := 0; i < 200; i++ {
		if got := w.StateHash(); got != first {
			t.Fatalf("第 %d 次调用哈希漂移:%s vs %s", i, got, first)
		}
	}
}
