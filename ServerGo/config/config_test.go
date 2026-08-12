// 2026-07-10 §116: 验证狼人杀配置默认值契约。
//
// 关注:
//   - WerewolfConfig.FirstNightForcedSpeakRounds 默认值从 1 提到 3
//     (狼人杀 7 人局开局每人必发 3 轮)
//   - getForcedSpeakRounds() 的 clamp 行为[1,3]由 werewolf 包测试覆盖,
//     这里只验证 config 层的 applyDefaults 行为。
package config

import "testing"

// TestWerewolfConfig_DefaultFirstNightForcedSpeakRounds 验证 §116 默认值。
// 模拟一个空 WerewolfConfig 走 applyDefaults,然后断言值 = 3。
func TestWerewolfConfig_DefaultFirstNightForcedSpeakRounds(t *testing.T) {
	c := &Config{}
	applyDefaults(c)
	if c.Werewolf.FirstNightForcedSpeakRounds != 3 {
		t.Errorf("FirstNightForcedSpeakRounds default = %d, want 3 (2026-07-10 §116:狼人杀 7 人局开局每人必发 3 轮强制发言)",
			c.Werewolf.FirstNightForcedSpeakRounds)
	}
}

// TestWerewolfConfig_ExplicitZeroStillDefaultsToThree 验证"显式置零"也被兜底:
// 即使 LsmWebGame.conf 显式写 0(老用户的 conf.example 在升级前是 0 兜底为 1),
// 我们 §116 之后应该兜底为 3,而不是回到旧 1 默认。
func TestWerewolfConfig_ExplicitZeroStillDefaultsToThree(t *testing.T) {
	c := &Config{Werewolf: WerewolfConfig{FirstNightForcedSpeakRounds: 0}}
	applyDefaults(c)
	if c.Werewolf.FirstNightForcedSpeakRounds != 3 {
		t.Errorf("FirstNightForcedSpeakRounds after applyDefaults on explicit zero = %d, want 3", c.Werewolf.FirstNightForcedSpeakRounds)
	}
}

// TestWerewolfConfig_NonZeroRespected 验证非零显式配置不被覆写。
// 如果用户在 LsmWebGame.conf 里写了 2,applyDefaults 必须保留(用户的显式选择优先)。
func TestWerewolfConfig_NonZeroRespected(t *testing.T) {
	c := &Config{Werewolf: WerewolfConfig{FirstNightForcedSpeakRounds: 2}}
	applyDefaults(c)
	if c.Werewolf.FirstNightForcedSpeakRounds != 2 {
		t.Errorf("FirstNightForcedSpeakRounds after applyDefaults on explicit 2 = %d, want 2", c.Werewolf.FirstNightForcedSpeakRounds)
	}
}

// TestWerewolfConfig_AllOtherDefaultsSanity 顺手验证 §13 + §16 引入的几个
// 关键默认没被 §116 误伤。
func TestWerewolfConfig_AllOtherDefaultsSanity(t *testing.T) {
	c := &Config{}
	applyDefaults(c)
	wantPairs := map[string]int{
		"FirstNightGraceSec":              c.Werewolf.FirstNightGraceSec,
		"MinSpeaksPerMinute":              c.Werewolf.MinSpeaksPerMinute,
		"ChatHistoryBytes":                c.Werewolf.ChatHistoryBytes,
		"DeathLyricDeadlineSec":           c.Werewolf.DeathLyricDeadlineSec,
	}
	// 这些值应仍为原本的默认(不被 §116 误改)。
	if wantPairs["FirstNightGraceSec"] != 120 {
		t.Errorf("FirstNightGraceSec default = %d, want 120", wantPairs["FirstNightGraceSec"])
	}
	if wantPairs["MinSpeaksPerMinute"] != 2 {
		t.Errorf("MinSpeaksPerMinute default = %d, want 2", wantPairs["MinSpeaksPerMinute"])
	}
	if wantPairs["ChatHistoryBytes"] != 500*1024 {
		t.Errorf("ChatHistoryBytes default = %d, want %d", wantPairs["ChatHistoryBytes"], 500*1024)
	}
	if wantPairs["DeathLyricDeadlineSec"] != 30 {
		t.Errorf("DeathLyricDeadlineSec default = %d, want 30", wantPairs["DeathLyricDeadlineSec"])
	}
}
