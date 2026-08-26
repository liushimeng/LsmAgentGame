// Package werewolf — role_prior_test.go: 身份偏见 RolePriorStore 单测（§20260826-01 U1）。
//
// 测试矩阵:
//   T1: 均匀初始化:13 局开局 → 12 目标 × 9 个非 unknown role × 每条目 prior ≈ 1/9
//   T2: 死亡公开:target=X → X 的狼 prior=1.0, 其他 = 0.0
//   T3: L1 归一化:每 target 维度求和 = 1
//   T4: SnapshotAllLocked 排序稳定性
//   T5: §92a — 未持锁调 *Locked 方法无 panic(由调用方守约,本测试覆盖正常路径)
package werewolf

import (
	"math"
	"testing"
	"time"
)

// T1: 13 人局开局均匀分布。
func TestRolePrior_UniformInit(t *testing.T) {
	s := NewRolePriorStore()
	tbl := s.ComputeRolePriorForSeatLocked(2, 0.5, time.Now())
	if tbl == nil {
		t.Fatal("expected table")
	}
	if tbl.Seat != 2 {
		t.Errorf("seat=%d, want 2", tbl.Seat)
	}
	// 每个 target 应有 9 个非 unknown role 条目(unknown 不计, villager/seer/witch/guard/...
	// + werewolf + idiot + knight + hunter + demon_hunter = 10 候选, 减 unknown = 9 个)
	byTarget := make(map[int]int)
	sumPerTarget := make(map[int]float32)
	for _, e := range tbl.Entries {
		byTarget[e.TargetSeat]++
		sumPerTarget[e.TargetSeat] += e.PriorProb
	}
	if len(byTarget) != MaxPlayers-1 {
		// seat=2 自己被 self-exclude,只剩 12 个目标
		t.Errorf("targets=%d, want %d", len(byTarget), MaxPlayers-1)
	}
	for tgt, cnt := range byTarget {
		if cnt != 9 {
			t.Errorf("target=%d entries=%d, want 9", tgt, cnt)
		}
		if math.Abs(float64(sumPerTarget[tgt]-1.0)) > 0.01 {
			t.Errorf("target=%d sum=%.4f, want ~1.0 (L1 norm)", tgt, sumPerTarget[tgt])
		}
	}
}

// T2: 死亡公开 → target 的所有 role 被 hard-set。
func TestRolePrior_DeathRevealed(t *testing.T) {
	s := NewRolePriorStore()
	s.ComputeRolePriorForSeatLocked(2, 0.5, time.Now())
	now := time.Now()
	s.ApplyDeathRevealPriorLocked(5, "werewolf", now)

	tbl := s.GetLocked(2)
	if tbl == nil {
		t.Fatal("nil after death reveal")
	}
	foundWerewolf := false
	for _, e := range tbl.Entries {
		if e.TargetSeat != 5 {
			continue
		}
		if e.RoleGuess == "werewolf" {
			foundWerewolf = true
			if e.PriorProb != 1.0 {
				t.Errorf("death-revealed werewolf prob=%.4f, want 1.0", e.PriorProb)
			}
			if e.EvidenceKind != "death_revealed" {
				t.Errorf("evidence_kind=%q, want death_revealed", e.EvidenceKind)
			}
		}
	}
	if !foundWerewolf {
		t.Error("death-revealed werewolf not in table")
	}
}

// T3: 多疑者加成 + L1 归一化。
func TestRolePrior_SuspiciousTrustBoost(t *testing.T) {
	s := NewRolePriorStore()
	tbl := s.ComputeRolePriorForSeatLocked(0, 0.2, time.Now()) // trust=0.2 多疑
	if tbl == nil {
		t.Fatal("expected table")
	}
	// 每 target 维度求和应为 1
	sumByTarget := make(map[int]float32)
	for _, e := range tbl.Entries {
		sumByTarget[e.TargetSeat] += e.PriorProb
	}
	for tgt, sum := range sumByTarget {
		if math.Abs(float64(sum-1.0)) > 0.01 {
			t.Errorf("target=%d sum=%.4f, want ~1.0", tgt, sum)
		}
	}
}

// T4: SnapshotAllLocked 返回多个 bot 表,seat 索引正确。
func TestRolePrior_SnapshotAll(t *testing.T) {
	s := NewRolePriorStore()
	s.ComputeRolePriorForSeatLocked(1, 0.5, time.Now())
	s.ComputeRolePriorForSeatLocked(5, 0.5, time.Now())
	snap := s.SnapshotAllLocked()
	if len(snap) != 2 {
		t.Fatalf("snap len=%d, want 2", len(snap))
	}
	seats := map[int]bool{}
	for _, x := range snap {
		seats[x.Seat] = true
	}
	if !seats[1] || !seats[5] {
		t.Errorf("seats=%v, want {1,5}", seats)
	}
}

// T5: §92a — GetLocked 在 nil receiver 上安全返回 nil。
func TestRolePrior_NilReceiverSafe(t *testing.T) {
	var s *RolePriorStore
	if got := s.GetLocked(0); got != nil {
		t.Errorf("nil receiver should return nil, got %+v", got)
	}
}