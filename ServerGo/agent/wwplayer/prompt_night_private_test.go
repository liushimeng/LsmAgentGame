package wwplayer

import (
	"strings"
	"testing"

	"LsmWebGame/agent/wwtypes"
)

// §20260812-04 U1 (P0-1) 回归测试。
//
// 缺陷:MySeerCheck / WolfTarget 由引擎填充但从未进入 prompt,AI 预言家/女巫
// 的技能对 LLM 完全不可见。这些用例断言的是**最终 prompt 文本**而非中间函数
// —— §20260811-08 教训 (5):「只测转换函数、不测转换结果,等于没测」。

// U1-01: 预言家的完整查验历史必须出现在 BuildUserPrompt 输出里。
func TestNightPrivate_U1_01_SeerHistoryReachesUserPrompt(t *testing.T) {
	ctx := wwtypes.GameContext{
		Role: "seer", MySeat: 0, Round: 3, Phase: "speak", SeatCount: 13,
		MySeerCheck:        6,
		MySeerCheckFaction: "good",
		MySeerCheckHistory: []wwtypes.SeerCheckRecord{
			{Round: 1, Seat: 3, Faction: "wolf"},
			{Round: 2, Seat: 6, Faction: "good"},
		},
	}
	got := BuildUserPrompt(ctx)

	// 座位号必须是 1-indexed 对外编号(§82a)。
	for _, want := range []string{"4号", "7号", "查杀", "金水", "私有信息"} {
		if !strings.Contains(got, want) {
			t.Fatalf("user prompt 缺少 %q\n--- prompt ---\n%s", want, got)
		}
	}
	// 绝不能出现 0-indexed 的原始座位。
	if strings.Contains(got, "查验 3号 → 🐺") {
		t.Fatalf("查验历史用了 0-indexed 座位号(应为 4号)")
	}
}

// U1-02: 女巫必须看到今晚狼刀目标 + 药剂余量。
func TestNightPrivate_U1_02_WitchSeesWolfTarget(t *testing.T) {
	ctx := wwtypes.GameContext{
		Role: "witch", MySeat: 2, Round: 1, Phase: "night_witch", SeatCount: 13,
		WolfTarget:        8,
		WitchAntidoteUsed: false,
		WitchPoisonUsed:   true,
	}
	got := BuildUserPrompt(ctx)

	if !strings.Contains(got, "9号") {
		t.Fatalf("女巫 prompt 未包含狼刀目标 9号\n--- prompt ---\n%s", got)
	}
	if !strings.Contains(got, "解药：仍可使用") {
		t.Fatalf("女巫 prompt 未正确渲染解药余量\n--- prompt ---\n%s", got)
	}
	if !strings.Contains(got, "毒药：已用完") {
		t.Fatalf("女巫 prompt 未正确渲染毒药余量\n--- prompt ---\n%s", got)
	}
}

// U1-03: 守卫是盲守 —— 即便 WolfTarget 被误填,也绝不能渲染给守卫(§134)。
func TestNightPrivate_U1_03_GuardNeverSeesWolfTarget(t *testing.T) {
	ctx := wwtypes.GameContext{
		Role: "guard", MySeat: 4, Round: 2, Phase: "night_guard", SeatCount: 13,
		GuardLastProtect: 5,
		// 故意污染:即使上游错填了狼刀目标,守卫也不该看到。
		WolfTarget: 9,
	}
	got := NightPrivateInfoBlock(&ctx)

	if !strings.Contains(got, "6号") {
		t.Fatalf("守卫 prompt 未渲染上晚守护目标 6号\n%s", got)
	}
	if strings.Contains(got, "10号") || strings.Contains(got, "狼人刀的是") {
		t.Fatalf("§134 盲守被破坏:守卫看到了狼刀目标\n%s", got)
	}
}

// U1-04: 普通角色(平民/狼人)不产出私有信息块,避免污染 prompt。
func TestNightPrivate_U1_04_NonGodRoleEmpty(t *testing.T) {
	for _, role := range []string{"villager", "werewolf", "hunter", ""} {
		ctx := wwtypes.GameContext{Role: role, MySeat: 1, SeatCount: 13}
		if got := NightPrivateInfoBlock(&ctx); got != "" {
			t.Fatalf("role=%q 不应产出私有信息块,实际:%q", role, got)
		}
	}
}

// U1-05: 尚未查验过的预言家应得到明确提示,而不是空块。
func TestNightPrivate_U1_05_SeerNoCheckYet(t *testing.T) {
	ctx := wwtypes.GameContext{Role: "seer", MySeat: 0, MySeerCheck: -1, SeatCount: 13}
	got := NightPrivateInfoBlock(&ctx)
	if !strings.Contains(got, "还没有查验过") {
		t.Fatalf("首夜预言家应得到「还没查验过」提示,实际:%q", got)
	}
}

// U1-06: 只有单条 MySeerCheck 而无 history 时的退化路径必须仍渲染结果。
func TestNightPrivate_U1_06_SeerDegradedSingleResult(t *testing.T) {
	ctx := wwtypes.GameContext{
		Role: "seer", MySeat: 0, SeatCount: 13,
		MySeerCheck:        11,
		MySeerCheckFaction: "wolf",
	}
	got := NightPrivateInfoBlock(&ctx)
	if !strings.Contains(got, "12号") || !strings.Contains(got, "查杀") {
		t.Fatalf("退化路径未渲染单条查验结果\n%s", got)
	}
}
