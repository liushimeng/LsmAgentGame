// Package werewolf — prop_aoe_test.go: 2026-08-07 §20260807-04 P0-2 验证。
//
// 覆盖：
//   - AOE 道具(long_swear)命中后,所有存活 bot 座位的 propInjectQueue 均有条目
//     (修复旧 `if result.Hit && target >= 0` 对 AOE(target=-1) 永不入队的 bug)
//   - 人类反制道具(TargetCamp=="human")的 debuff 落地到目标人类座位
package werewolf

import (
	"strings"
	"testing"

	"LsmAgentGame/agent/wwtypes"
)

// newAOETestRoom 构造一个带 5 个座位的测试房间：
//   - seat 0/1/2 = 存活 bot
//   - seat 3 = 死亡 bot(不应收到 AOE 入队)
//   - seat 4 = 存活真人(不应收到 AOE 入队)
func newAOETestRoom() *WerewolfRoom {
	gs := NewGame(42)
	gs.SeatCount = 5
	for i := 0; i < 5; i++ {
		gs.Players[i].Alive = true
		gs.Players[i].IsBot = true
		gs.Roles[i] = RoleVillager
	}
	gs.Players[3].Alive = false // seat 3 死亡
	gs.Players[4].IsBot = false // seat 4 真人

	r := &WerewolfRoom{
		RoomID:      "aoe-test-room",
		State:       gs,
		propCatalog: BuildDefaultPropCatalog(),
	}
	return r
}

// TestAOEPropHit_EnqueuesAllAliveBots 验证 AOE 道具命中后所有存活 bot 均入队。
func TestAOEPropHit_EnqueuesAllAliveBots(t *testing.T) {
	r := newAOETestRoom()
	catEntry, ok := r.propCatalog.GetEnabled("long_swear")
	if !ok {
		t.Fatal("long_swear should be enabled")
	}
	if !catEntry.IsAOE {
		t.Fatal("long_swear should be AOE")
	}

	// 模拟 room_action.go / agent_runner.go 的 AOE 入队逻辑(2026-08-07 P0-2)。
	injResult := GenerateInjectByKey("long_swear", 0, -1, "", "", "")
	for seat, p := range r.State.Players {
		if !p.Alive || !p.IsBot {
			continue
		}
		twistSeat := r.computeTwistSeatLocked(catEntry.EffectSpec.TwistSeatSrc, 0, seat)
		r.enqueuePropHitLocked(seat, PropInjectEntry{
			FromSeat:     0,
			PropKey:      "long_swear",
			InjectText:   injResult.InjectText,
			EffectTypes:  catEntry.EffectSpec.EffectTypes,
			TwistSeat:    twistSeat,
			Hit:          true,
			ExpiresAfter: 1,
		})
	}

	// 断言:seat 0/1/2(存活 bot)均有条目;seat 3(死亡)/4(真人)无。
	for seat := 0; seat <= 2; seat++ {
		entries := r.propInjectQueue[seat]
		if len(entries) != 1 {
			t.Errorf("seat %d should have 1 entry, got %d", seat, len(entries))
			continue
		}
		if entries[0].EffectTypes != "attention_scatter,target_twist" {
			t.Errorf("seat %d EffectTypes = %s, want attention_scatter,target_twist", seat, entries[0].EffectTypes)
		}
	}
	if len(r.propInjectQueue[3]) != 0 {
		t.Error("dead bot seat 3 should NOT be enqueued")
	}
	if len(r.propInjectQueue[4]) != 0 {
		t.Error("human seat 4 should NOT be enqueued by AOE")
	}
}

// TestAOEPropHit_DrainAppliesEffects 验证 drain + ApplyEffects 把干扰信号落到 GameContext。
func TestAOEPropHit_DrainAppliesEffects(t *testing.T) {
	r := newAOETestRoom()
	injResult := GenerateInjectByKey("long_swear", 0, -1, "", "", "")
	for seat, p := range r.State.Players {
		if !p.Alive || !p.IsBot {
			continue
		}
		r.enqueuePropHitLocked(seat, PropInjectEntry{
			FromSeat:     0,
			PropKey:      "long_swear",
			InjectText:   injResult.InjectText,
			EffectTypes:  "attention_scatter,target_twist",
			TwistSeat:    4,
			Hit:          true,
			ExpiresAfter: 1,
		})
	}
	for seat := 0; seat <= 2; seat++ {
		gc := wwtypes.GameContext{}
		entries := r.drainPropInjectQueueLocked(seat)
		if len(entries) != 1 {
			t.Fatalf("seat %d should drain 1 entry, got %d", seat, len(entries))
		}
		ApplyEffects(&gc, seat, entries[0], EffectApplyContext{Room: r, Entry: entries[0], FromSeat: 0})
		if !gc.EffectAttentionScatter {
			t.Errorf("seat %d: attention_scatter not applied", seat)
		}
		if gc.ToolUseMaxOverride != 2 {
			t.Errorf("seat %d: ToolUseMaxOverride = %d, want 2", seat, gc.ToolUseMaxOverride)
		}
		if gc.EffectTargetTwistSeat != 4 {
			t.Errorf("seat %d: EffectTargetTwistSeat = %d, want 4", seat, gc.EffectTargetTwistSeat)
		}
	}
}

// TestHumanDebuffProp_LandsOnHumanSeat 验证人类反制道具 debuff 落地到目标人类座位。
func TestHumanDebuffProp_LandsOnHumanSeat(t *testing.T) {
	r := newAOETestRoom()
	// seat 0 = bot(使用者); seat 4 = 真人(目标)
	cases := []struct {
		propKey  string
		wantType string
	}{
		{"md_bomb_human", "human_announce_prefix"},
		{"nested_maze_human", "human_vote_suggest"},
		{"char_confuse_human", "human_char_garble"},
	}
	for _, tc := range cases {
		t.Run(tc.propKey, func(t *testing.T) {
			catEntry, ok := r.propCatalog.GetEnabled(tc.propKey)
			if !ok {
				t.Fatalf("%s should be enabled", tc.propKey)
			}
			if catEntry.TargetCamp != "human" {
				t.Fatalf("%s TargetCamp = %s, want human", tc.propKey, catEntry.TargetCamp)
			}
			spec := buildHumanDebuffSpecLocked(catEntry, 0, 4)
			if spec == nil {
				t.Fatalf("buildHumanDebuffSpecLocked(%s) returned nil", tc.propKey)
			}
			if spec.Type != tc.wantType {
				t.Errorf("spec.Type = %s, want %s", spec.Type, tc.wantType)
			}
			r.setHumanDebuffLocked(4, *spec)
			if r.State.Players[4].HumanDebuff == nil {
				t.Errorf("player 4 HumanDebuff should be set after %s", tc.propKey)
			} else if r.State.Players[4].HumanDebuff.Type != tc.wantType {
				t.Errorf("player 4 HumanDebuff.Type = %s, want %s",
					r.State.Players[4].HumanDebuff.Type, tc.wantType)
			}
			// bot 座位不应有 debuff
			if r.State.Players[0].HumanDebuff != nil {
				t.Error("bot seat 0 should NOT have HumanDebuff")
			}
			// 清理
			r.State.Players[4].HumanDebuff = nil
		})
	}
}

// TestHumanDebuffVoteSuggest_HasSeat 验证 human_vote_suggest 携带 SuggestSeat。
func TestHumanDebuffVoteSuggest_HasSeat(t *testing.T) {
	r := newAOETestRoom()
	catEntry, _ := r.propCatalog.GetEnabled("nested_maze_human")
	spec := buildHumanDebuffSpecLocked(catEntry, 0, 4)
	if spec == nil {
		t.Fatal("spec nil")
	}
	if spec.Type != "human_vote_suggest" {
		t.Fatalf("Type = %s, want human_vote_suggest", spec.Type)
	}
	// TwistSeatSrc = "from_seat" → SuggestSeat = 使用者座位(0)
	if spec.SuggestSeat != 0 {
		t.Errorf("SuggestSeat = %d, want 0 (from_seat)", spec.SuggestSeat)
	}
}

// TestHumanDebuffRegistry 验证 3 个人类反制效果已注册。
func TestHumanDebuffRegistry(t *testing.T) {
	for _, key := range []string{"human_announce_prefix", "human_vote_suggest", "human_char_garble"} {
		if _, ok := EffectRegistry[key]; !ok {
			t.Errorf("EffectRegistry missing %q", key)
		}
	}
	for _, key := range []string{"md_bomb_human", "nested_maze_human", "char_confuse_human"} {
		if _, ok := InjectRegistry[key]; !ok {
			t.Errorf("InjectRegistry missing %q", key)
		}
	}
}

// TestDrainPropInjectQueue_ExpiresAfterDecrements 验证 P1-2 修复:
// ExpiresAfter>1 的条目在 drain 后正确递减(原 bug:值拷贝导致永不递减)。
func TestDrainPropInjectQueue_ExpiresAfterDecrements(t *testing.T) {
	r := &WerewolfRoom{}
	r.enqueuePropInjectLocked(2, PropInjectEntry{
		FromSeat:     0,
		PropKey:      "test_multi_round",
		InjectText:   "x",
		EffectTypes:  "expose_identity",
		Hit:          true,
		ExpiresAfter: 2,
	})
	// 第一次 drain:ExpiresAfter 2 → 递减为 1,条目有效。
	entries := r.drainPropInjectQueueLocked(2)
	if len(entries) != 1 {
		t.Fatalf("first drain should return 1 entry, got %d", len(entries))
	}
	if entries[0].ExpiresAfter != 1 {
		t.Errorf("after first drain ExpiresAfter = %d, want 1 (原 bug 为 2 永不递减)", entries[0].ExpiresAfter)
	}
}

// TestRoleSpecificInduction 验证 P1-1 按角色差异化诱导指令。
func TestRoleSpecificInduction(t *testing.T) {
	wolf := roleSpecificInduction("werewolf", "")
	if !strings.Contains(wolf, "刀人目标") {
		t.Errorf("werewolf induction should contain 刀人目标, got: %s", wolf[:minInt(60, len(wolf))])
	}
	seer := roleSpecificInduction("seer", "")
	if !strings.Contains(seer, "查验") {
		t.Errorf("seer induction should contain 查验, got: %s", seer[:minInt(60, len(seer))])
	}
	witch := roleSpecificInduction("witch", "")
	if !strings.Contains(witch, "用药") {
		t.Errorf("witch induction should contain 用药, got: %s", witch[:minInt(60, len(witch))])
	}
	other := roleSpecificInduction("villager", "")
	if !strings.Contains(other, "身份") {
		t.Errorf("villager induction should contain 身份, got: %s", other[:minInt(60, len(other))])
	}
	// payload 覆盖
	custom := roleSpecificInduction("werewolf", "自定义指令")
	if custom != "自定义指令" {
		t.Errorf("payload should override, got %s", custom)
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
