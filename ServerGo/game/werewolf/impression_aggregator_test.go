// Package werewolf — impression_aggregator_test.go: 印象自动聚合单测（§20260826-01 U3）。
//
// 测试矩阵:
//   T1: EmitImpressionOnVoteAccurateLocked → Cooperation + Competence
//   T2: EmitImpressionOnFramedLocked → Threat 大幅 +
//   T3: emotionFactor 影响累加幅度
//   T4: 已死亡玩家不更新
package werewolf

import (
	"testing"
	"time"
)

// T1: 投票命中狼 → Cooperation/Competence +。
func TestAggregator_VoteAccurate(t *testing.T) {
	r := newTestRoomWithPlayers(13)
	now := time.Now()
	r.EmitImpressionOnVoteAccurateLocked(2, 5, now)

	mem := r.impressionStoreLocked().GetLocked(2, now)
	if mem == nil || len(mem.Entries) == 0 {
		t.Fatal("expected entry for seat 5")
	}
	for _, e := range mem.Entries {
		if e.TargetSeat != 5 {
			continue
		}
		if e.Dims.Cooperation < 0.59 {
			t.Errorf("Cooperation=%.4f, want ~0.6 (0.5+0.1)", e.Dims.Cooperation)
		}
		if e.Dims.Competence < 0.59 {
			t.Errorf("Competence=%.4f, want ~0.6 (0.5+0.1)", e.Dims.Competence)
		}
	}
}

// T2: 被嫁祸 → Threat + +。
func TestAggregator_FramedByOther(t *testing.T) {
	r := newTestRoomWithPlayers(13)
	now := time.Now()
	r.EmitImpressionOnFrameLocked(3, 7, now) // target=3 被 framer=7 嫁祸

	mem := r.impressionStoreLocked().GetLocked(3, now)
	if mem == nil || len(mem.Entries) == 0 {
		t.Fatal("expected entry for seat 7")
	}
	for _, e := range mem.Entries {
		if e.TargetSeat != 7 {
			continue
		}
		// Threat + 0.15
		if e.Dims.Threat < 0.6 {
			t.Errorf("Threat=%.4f, want ~0.65 (0.5+0.15)", e.Dims.Threat)
		}
		// Sincerity - 0.10
		if e.Dims.Sincerity > 0.41 {
			t.Errorf("Sincerity=%.4f, want ~0.4 (0.5-0.10)", e.Dims.Sincerity)
		}
	}
}

// T3: emotionFactor 影响。
func TestAggregator_EmotionFactor(t *testing.T) {
	r := newTestRoomWithPlayers(13)
	now := time.Now()
	r.AggregateImpressionFromEventLocked(2, 5, ImpressionEventVoteAccurate, 0.5, now) // emotionFactor 0.5

	mem := r.impressionStoreLocked().GetLocked(2, now)
	for _, e := range mem.Entries {
		if e.TargetSeat != 5 {
			continue
		}
		// Cooperation +0.1 * 0.5 = 0.05
		if e.Dims.Cooperation < 0.54 || e.Dims.Cooperation > 0.56 {
			t.Errorf("emotionFactor=0.5 → Cooperation=%.4f, want ~0.55", e.Dims.Cooperation)
		}
	}
}

// T4: 已死亡玩家 → 不更新印象。
func TestAggregator_DeadPlayerNoUpdate(t *testing.T) {
	r := newTestRoomWithPlayers(13)
	r.State.Players[5].Alive = false
	now := time.Now()
	r.EmitImpressionOnVoteAccurateLocked(2, 5, now)

	mem := r.impressionStoreLocked().GetLocked(2, now)
	if mem == nil {
		// 没更新 → 无 entries
		return
	}
	for _, e := range mem.Entries {
		if e.TargetSeat == 5 {
			t.Errorf("dead target seat should not have entry, got %+v", e)
		}
	}
}

// helper: 构造一个最小可用的 WerewolfRoom 用于单测(避免触发 StartGame 等重型初始化)。
func newTestRoomWithPlayers(n int) *WerewolfRoom {
	r := &WerewolfRoom{
		RoomID: "test-room",
		State:  &GameState{},
	}
	for i := 0; i < n && i < len(r.State.Players); i++ {
		r.State.Players[i] = Player{Seat: Seat(i), Alive: true, IsBot: true}
	}
	return r
}