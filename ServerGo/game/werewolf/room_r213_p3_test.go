// BUG-R213-P3-01 (2026-07-31) 回归测试:守卫/猎魔人/骑士工具名与决策摘要
// 在下发给玩家/观战者的 BotTranscript 中必须抽象化 + 脱敏,杜绝
// 「工具名=身份」侧信道。
//
// 背景(自动化测试报告 2026-07-31 05:43:32 §4.3/§8.3):
//   - 观战者座位卡显示 `📤 guard_protect`,即便 target 脱敏也足以嗅探
//     "该座位是守卫",直接摧毁 §134 盲守 / §猎魔人 隐身狩猎的博弈价值;
//   - 修复:sensitiveToolNames 加入 guard_protect / demon_hunter_hunt /
//     knight_duel;sanitizeBotTranscript 对这些工具走
//     publicToolNameForWire(guard_protect → night_act 等)+ 目标脱敏。
package werewolf

import (
	"strings"
	"testing"

	"LsmAgentGame/agent/wwplayer"
)

// TestR213_SanitizeBotTranscript_NightRoleToolNameMasked 验证 guard_protect /
// demon_hunter_hunt / knight_duel 在 tool_calls / last_tool /
// last_decision_summary 三处全部抽象化,玩家/观战者看不到真实工具名。
func TestR213_SanitizeBotTranscript_NightRoleToolNameMasked(t *testing.T) {
	for _, isSpec := range []bool{false, true} {
		in := wwplayer.BotTranscript{
			Seat:                10,
			Model:               "Tencent-model",
			LastTool:            "guard_protect",
			LastDecisionSummary: "guard_protect → [已隐藏]",
			ToolCalls:           []string{"guard_protect: target=3"},
		}
		out := sanitizeBotTranscript(in, isSpec)

		if strings.Contains(out.LastTool, "guard_protect") {
			t.Errorf("isSpec=%v: LastTool 泄露真实工具名: %q", isSpec, out.LastTool)
		}
		if out.LastTool != "night_act" {
			t.Errorf("isSpec=%v: LastTool = %q, want night_act", isSpec, out.LastTool)
		}
		if strings.Contains(out.LastDecisionSummary, "guard_protect") {
			t.Errorf("isSpec=%v: LastDecisionSummary 泄露真实工具名: %q", isSpec, out.LastDecisionSummary)
		}
		if !strings.HasPrefix(out.LastDecisionSummary, "night_act") {
			t.Errorf("isSpec=%v: LastDecisionSummary = %q, want night_act 前缀", isSpec, out.LastDecisionSummary)
		}
		for _, tc := range out.ToolCalls {
			if strings.Contains(tc, "guard_protect") || strings.Contains(tc, "target=3") {
				t.Errorf("isSpec=%v: ToolCalls 泄露: %q", isSpec, tc)
			}
		}
	}
}

// TestR213_SanitizeBotTranscript_DemonHunterAndKnightMasked 验证猎魔人/骑士
// 工具同样被抽象化(demon_hunter_hunt → night_act;knight_duel → day_act)。
func TestR213_SanitizeBotTranscript_DemonHunterAndKnightMasked(t *testing.T) {
	cases := []struct {
		tool    string
		wantPub string
	}{
		{"demon_hunter_hunt", "night_act"},
		{"knight_duel", "day_act"},
	}
	for _, c := range cases {
		in := wwplayer.BotTranscript{
			Seat:                3,
			LastTool:            c.tool,
			LastDecisionSummary: c.tool + " → 5号",
			ToolCalls:           []string{c.tool + ": target=5"},
		}
		out := sanitizeBotTranscript(in, true)
		if out.LastTool != c.wantPub {
			t.Errorf("%s: LastTool = %q, want %q", c.tool, out.LastTool, c.wantPub)
		}
		if strings.Contains(out.LastDecisionSummary, c.tool) || strings.Contains(out.LastDecisionSummary, "5号") {
			t.Errorf("%s: LastDecisionSummary 泄露: %q", c.tool, out.LastDecisionSummary)
		}
		for _, tc := range out.ToolCalls {
			if strings.Contains(tc, c.tool) || strings.Contains(tc, "target=5") {
				t.Errorf("%s: ToolCalls 泄露: %q", c.tool, tc)
			}
		}
	}
}

// TestR213_PublicToolNameForWire_NonSensitiveUnchanged 验证非身份敏感工具
// (speak/vote/idle 等)工具名原样透传,不被误抽象化。
// BUG-R226-P1-01 (2026-08-01): wolf_kill/seer_check 已收口进单表,
// 必须抽象化,从"原样透传"清单中移出。
func TestR213_PublicToolNameForWire_NonSensitiveUnchanged(t *testing.T) {
	for _, name := range []string{"speak", "vote", "interject", "whisper", "idle_silent"} {
		if got := publicToolNameForWire(name); got != name {
			t.Errorf("publicToolNameForWire(%q) = %q, want 原样返回", name, got)
		}
	}
}

// TestR226_P1_01_CoreIdentityToolNamesMasked 验证 seer_check / wolf_kill /
// witch_act / hunter_shoot 四个核心身份工具名同样被抽象化(BUG-R226-P1-01)。
// 背景:R213 修复只覆盖守卫/猎魔人/骑士三个边缘角色,核心的预言家/狼人
// 工具名原样下发,观战者座位卡 `📤 seer_check → [已隐藏]` 可直接锁定身份。
func TestR226_P1_01_CoreIdentityToolNamesMasked(t *testing.T) {
	cases := []struct {
		tool    string
		wantPub string
	}{
		{"seer_check", "night_act"},
		{"wolf_kill", "night_act"},
		{"witch_act", "night_act"},
		{"witch_act_skip", "night_act"},
		{"hunter_shoot", "day_act"},
	}
	for _, c := range cases {
		if got := publicToolNameForWire(c.tool); got != c.wantPub {
			t.Errorf("publicToolNameForWire(%q) = %q, want %q", c.tool, got, c.wantPub)
		}
		out := sanitizeBotTranscript(wwplayer.BotTranscript{
			Seat:                1,
			LastTool:            c.tool,
			LastDecisionSummary: c.tool + " → 5号",
			ToolCalls:           []string{c.tool + ": target=5"},
		}, true)
		if strings.Contains(out.LastTool, c.tool) {
			t.Errorf("%s: LastTool 泄露真实工具名: %q", c.tool, out.LastTool)
		}
		if strings.Contains(out.LastDecisionSummary, c.tool) {
			t.Errorf("%s: LastDecisionSummary 泄露真实工具名: %q", c.tool, out.LastDecisionSummary)
		}
		for _, tc := range out.ToolCalls {
			if strings.Contains(tc, c.tool) {
				t.Errorf("%s: ToolCalls 泄露真实工具名: %q", c.tool, tc)
			}
		}
	}
}

// TestR226_P1_01_SensitiveTableFullCoverage 结构性断言:sensitiveToolNames
// 中的**每一个** key 经 publicToolNameForWire 后都 != 原名 ——
// 防止未来新增角色时「加进 A 表却忘了加进 B 表」的 BUG-R226-P1-01 复现。
func TestR226_P1_01_SensitiveTableFullCoverage(t *testing.T) {
	for name := range sensitiveToolNames {
		if got := publicToolNameForWire(name); got == name {
			t.Errorf("sensitiveToolNames[%q] 存在但 publicToolNameForWire 未抽象化(返回原名)", name)
		}
	}
}

// TestR213_BuildDecisionSummary_NightRoleTools 验证守卫/猎魔人/骑士工具在
// BuildDecisionSummary 走「tool → N号」桶(与 wolf_kill/seer_check 一致),
// 不再落到 default 的裸工具名。
func TestR213_BuildDecisionSummary_NightRoleTools(t *testing.T) {
	cases := []struct {
		tool string
		want string
	}{
		{"guard_protect", "guard_protect → 4号"},
		{"demon_hunter_hunt", "demon_hunter_hunt → 6号"},
		{"knight_duel", "knight_duel → 2号"},
	}
	for _, c := range cases {
		input := map[string]any{"target": c.want[len(c.tool)+3:]}
		_ = input // target 解析走 int;直接用 float64 模拟 JSON 解码后的形态
		var target float64
		switch c.tool {
		case "guard_protect":
			target = 3
		case "demon_hunter_hunt":
			target = 5
		case "knight_duel":
			target = 1
		}
		got := wwplayer.BuildDecisionSummary(c.tool, map[string]any{"target": target}, "")
		if got != c.want {
			t.Errorf("BuildDecisionSummary(%q) = %q, want %q", c.tool, got, c.want)
		}
	}
}

// TestR213_SanitizeToolInput_NightRoleTargetMasked 验证守卫/猎魔人/骑士的
// target 入参在 LastToolInput JSON 中被 [已隐藏] 替换。
func TestR213_SanitizeToolInput_NightRoleTargetMasked(t *testing.T) {
	for _, tool := range []string{"guard_protect", "demon_hunter_hunt", "knight_duel"} {
		got := wwplayer.SanitizeToolInput(tool, map[string]any{"target": 3})
		if strings.Contains(got, `"target":3`) {
			t.Errorf("SanitizeToolInput(%q) 泄露 target: %q", tool, got)
		}
		if !strings.Contains(got, "[已隐藏]") {
			t.Errorf("SanitizeToolInput(%q) 缺少 [已隐藏] 占位: %q", tool, got)
		}
	}
}
