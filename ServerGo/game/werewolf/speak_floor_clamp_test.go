package werewolf

import "testing"

// §20260812-04 U5 (P0-7) 回归测试 —— 发言下限与同座位冷却的自洽性钳制。
//
// 缺陷:min_speaks_per_minute 默认 2、same_seat_speak_cooldown_sec 默认 60,
// 后者意味着每座位每分钟最多 1 次公开发言 → `count >= 2` 对任何 bot 永远不成立
// → speak_floor watchdog 每 20s 给每个存活 bot 推一次注定失败的 wake
//(13 人局 ≈ 39 次无效 LLM 调用/分钟,还挤占 cap=4 的房间信号量)。

func TestClampMinSpeaks_U5_P07(t *testing.T) {
	cases := []struct {
		name      string
		minSpeaks int
		cooldown  int
		want      int
	}{
		// 生产默认组合:60s 冷却 → 每分钟最多 1 次 → 下限 2 必须被钳到 1。
		{"生产默认(2/60s)钳到可达值", 2, 60, 1},
		// 冷却放宽到 20s → 每分钟可达 3 次 → 下限 2 本就可满足,保持不变。
		{"冷却20s时下限2可达_不干预", 2, 20, 2},
		// 冷却 20s、下限 5 → 可达 3,钳到 3。
		{"下限高于可达值_钳到可达", 5, 20, 3},
		// 冷却超过 60s → 一分钟内一次都不保证,至少给 1。
		{"超长冷却_至少为1", 3, 90, 1},
		// 下限 0 = 显式禁用发言下限,不得被钳制改写。
		{"下限0表示禁用_原样返回", 0, 60, 0},
		// 无冷却配置 → 下限原样生效。
		{"无冷却_原样返回", 4, 0, 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := clampMinSpeaksWithCooldown(tc.minSpeaks, tc.cooldown)
			if got != tc.want {
				t.Fatalf("clamp(minSpeaks=%d, cooldown=%d) = %d, want %d",
					tc.minSpeaks, tc.cooldown, got, tc.want)
			}
		})
	}
}

// 生产默认值下,钳制后的下限必须是「物理可达」的 —— 这条是缺陷本体的断言:
// 钳制前 2 > 1(不可达),钳制后必须 ≤ 每分钟可发言次数。
func TestClampMinSpeaks_U5_P07_DefaultsAreSatisfiable(t *testing.T) {
	const cooldown = 60 // cfgWerewolfSameSeatSpeakCooldownSec 默认值
	const configured = 2 // cfgWerewolfMinSpeaks 默认值

	reachable := 60 / cooldown
	if configured <= reachable {
		t.Skip("默认值已自洽,本用例针对的缺陷组合不复存在")
	}
	got := clampMinSpeaksWithCooldown(configured, cooldown)
	if got > reachable {
		t.Fatalf("钳制后下限 %d 仍超过每分钟可达次数 %d —— "+
			"speak_floor 仍会推送注定失败的 wake", got, reachable)
	}
}
