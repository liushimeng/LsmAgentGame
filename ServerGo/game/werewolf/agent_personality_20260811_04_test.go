// Package werewolf — agent_personality_20260811_04_test.go: §20260811-04 U2 人设倾向参数装配层单测。
//
// 验证:
//   - WerewolfRoom.SetAgentPersonality 归一化(mode/preset 非枚举值回退到默认);
//   - PersonalitySnapshotLocked 返回 (mode, preset_key, custom_vector 副本);
//   - resolvePersonalityForSeatLocked 在三种 mode 下给出确定性的 (vec, key);
//   - random 模式按 seat % len(presets) 确定性派发(无随机 → 同房间所有 goroutine 一致);
//   - 自定义向量的 Clamp 在 room 层生效。
package werewolf

import (
	"testing"

	"LsmWebGame/agent/wwplayer"
)

func newTestRoomForPersonality() *WerewolfRoom {
	return &WerewolfRoom{}
}

func TestSetAgentPersonality_NormalizesInvalidMode(t *testing.T) {
	r := newTestRoomForPersonality()
	for _, bad := range []string{"", "garbage", "Uniform" /* case sensitive */} {
		r.SetAgentPersonality(bad, "logical", nil)
		mode, preset, _ := r.PersonalitySnapshotLocked()
		if mode != PersonalityModeUniform || preset != "logical" {
			t.Fatalf("mode=%q should normalize to uniform+logical, got (%q,%q)", bad, mode, preset)
		}
	}
}

func TestSetAgentPersonality_NormalizesInvalidPreset(t *testing.T) {
	r := newTestRoomForPersonality()
	for _, bad := range []string{"", "unknown", "Logical" /* case sensitive */} {
		r.SetAgentPersonality(PersonalityModeUniform, bad, nil)
		_, preset, _ := r.PersonalitySnapshotLocked()
		if preset != "logical" {
			t.Fatalf("preset=%q should normalize to logical, got %q", bad, preset)
		}
	}
}

func TestSetAgentPersonality_CustomVectorClamped(t *testing.T) {
	r := newTestRoomForPersonality()
	raw := &wwplayer.PersonalityVector{Aggressiveness: 1.5, TrustTendency: -0.2, BluffFrequency: 0.5, CollaborationStyle: 0.3, RiskTolerance: 0.7}
	r.SetAgentPersonality(PersonalityModeCustom, "logical", raw)
	_, _, stored := r.PersonalitySnapshotLocked()
	if stored == nil {
		t.Fatal("custom mode should retain vector")
	}
	if stored.Aggressiveness != 1.0 || stored.TrustTendency != 0 {
		t.Fatalf("clamp failed: got %+v", *stored)
	}
}

func TestSetAgentPersonality_NonCustomIgnoresCustomVec(t *testing.T) {
	r := newTestRoomForPersonality()
	raw := &wwplayer.PersonalityVector{Aggressiveness: 0.9, RiskTolerance: 0.9}
	r.SetAgentPersonality(PersonalityModeUniform, "aggressive", raw)
	mode, preset, stored := r.PersonalitySnapshotLocked()
	if mode != PersonalityModeUniform || preset != "aggressive" {
		t.Fatalf("mode drift: got (%q,%q)", mode, preset)
	}
	if stored != nil {
		t.Fatalf("uniform mode should ignore custom vec, got %+v", *stored)
	}
}

func TestResolvePersonalityForSeat_UniformSameForAllSeats(t *testing.T) {
	r := newTestRoomForPersonality()
	r.SetAgentPersonality(PersonalityModeUniform, "showman", nil)
	for _, seat := range []int{0, 1, 5, 12} {
		vec, key := resolvePersonalityForSeatLocked(r, seat)
		if key != "showman" {
			t.Errorf("seat %d: expected preset showman, got %q", seat, key)
		}
		want := wwplayer.PersonalityPresets["showman"]
		if vec != want {
			t.Errorf("seat %d: vec drift", seat)
		}
	}
}

func TestResolvePersonalityForSeat_RandomDeterministic(t *testing.T) {
	r := newTestRoomForPersonality()
	r.SetAgentPersonality(PersonalityModeRandom, "", nil)
	// §92a 同房间确定性:两次调用应得到完全一致结果
	for _, seat := range []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12} {
		v1, k1 := resolvePersonalityForSeatLocked(r, seat)
		v2, k2 := resolvePersonalityForSeatLocked(r, seat)
		if v1 != v2 || k1 != k2 {
			t.Fatalf("seat %d: nondeterministic resolution (%v,%q) vs (%v,%q)", seat, v1, k1, v2, k2)
		}
	}
	// 13 个座位应至少覆盖 5 个预设中的多个(确定性 seat%5)
	seen := map[string]bool{}
	for seat := 0; seat < 13; seat++ {
		_, key := resolvePersonalityForSeatLocked(r, seat)
		seen[key] = true
	}
	if len(seen) < 3 {
		t.Errorf("random distribution too narrow: only %d distinct presets in 13 seats", len(seen))
	}
}

func TestResolvePersonalityForSeat_CustomUsesStored(t *testing.T) {
	r := newTestRoomForPersonality()
	raw := &wwplayer.PersonalityVector{Aggressiveness: 0.3, TrustTendency: 0.8, BluffFrequency: 0.4, CollaborationStyle: 0.6, RiskTolerance: 0.5}
	r.SetAgentPersonality(PersonalityModeCustom, "logical", raw)
	vec, key := resolvePersonalityForSeatLocked(r, 0)
	if key != "logical" {
		t.Errorf("custom mode should keep preset key, got %q", key)
	}
	if vec.Aggressiveness != 0.3 || vec.TrustTendency != 0.8 {
		t.Errorf("custom vec drift: %+v", vec)
	}
}

func TestPersonalitySnapshotLocked_NilRoomSafe(t *testing.T) {
	var r *WerewolfRoom
	mode, preset, vec := r.PersonalitySnapshotLocked()
	if mode != PersonalityModeUniform || preset != "logical" || vec != nil {
		t.Fatalf("nil room should return defaults, got (%q,%q,%+v)", mode, preset, vec)
	}
}

func TestPersonalitySnapshotLocked_CopyNotShared(t *testing.T) {
	r := newTestRoomForPersonality()
	raw := &wwplayer.PersonalityVector{Aggressiveness: 0.5, RiskTolerance: 0.5}
	r.SetAgentPersonality(PersonalityModeCustom, "logical", raw)
	_, _, snap1 := r.PersonalitySnapshotLocked()
	snap1.Aggressiveness = 0.99 // mutate caller copy
	_, _, snap2 := r.PersonalitySnapshotLocked()
	if snap2.Aggressiveness != 0.5 {
		t.Fatalf("snapshot should be a copy; caller mutation leaked back: %+v", *snap2)
	}
}