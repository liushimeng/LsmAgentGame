// Package wwplayer — prompt_reveal_20260830_test.go: §20260830-01「死亡亮身份」
// 玩家 Agent 侧单测(设计文档 §6.2/§6.3 + §10 Agent 侧用例)。
//
// 覆盖:
//
//	P-R1  BuildSystemPrompt 双模式:关闭与旧版逐字节一致(§135 竞技文案);
//	      开启替换为「死亡即公开身份」文案,且保留「存活玩家身份保密」禁令。
//	P-R2  白痴夜间死亡规则段双模式(关闭「不翻牌身份保密」/开启「当场公开」)。
//	P-R3  BuildUserPrompt【死亡白名单】带身份:RevealedRoles 命中座位追加
//	      「(身份:X,已公开)」;关闭模式输出与现状一致;开启但 map 未命中
//	      防御性不写身份。
//	P-R4  Agent.SetRevealRoleOnDeath:同步开关并重算冻结快照(invariant I11 同源),
//	      幂等(值未变零开销);BuildSystemPromptBytes 两种模式字节不同且各自稳定。
package wwplayer

import (
	"strings"
	"testing"

	"LsmAgentGame/agent/wwtypes"
)

// TestPlayerPrompt_R1_SystemPromptDualMode system prompt §135 规则段双模式(§6.2)。
func TestPlayerPrompt_R1_SystemPromptDualMode(t *testing.T) {
	closed := BuildSystemPrompt("", PersonalityVector{}, "", "", false)
	closedText := closed[0].Text
	open := BuildSystemPrompt("", PersonalityVector{}, "", "", true)
	openText := open[0].Text

	// 关闭:现行 §135 竞技文案必须在位。
	for _, want := range []string{
		"死者身份牌全程不翻开",
		"【§135 身份公开规则 — 死者身份牌不翻开】",
		"绝不下发死者角色",
	} {
		if !strings.Contains(closedText, want) {
			t.Errorf("关闭模式 system prompt 缺少 %q", want)
		}
		if strings.Contains(openText, want) {
			t.Errorf("开启模式 system prompt 不得再出现 %q", want)
		}
	}
	// 开启:死亡亮身份文案 + 存活玩家身份保密禁令 + FactCheck 合法性注记。
	for _, want := range []string{
		"ℹ️ 本局开启【死亡亮身份】",
		"【§135 身份公开规则 — 本局开启死亡亮身份】",
		"【死亡白名单】(带角色名)",
		"严禁声称知道任何存活玩家的具体身份",
		"属于合法公开信息",
		"死者已公开的角色名属于合法公开信息",
	} {
		if !strings.Contains(openText, want) {
			t.Errorf("开启模式 system prompt 缺少 %q", want)
		}
	}
	// 关闭模式不得出现开启专属文案。
	for _, ban := range []string{"死亡亮身份", "死者已公开的角色名"} {
		if strings.Contains(closedText, ban) {
			t.Errorf("关闭模式 system prompt 不得出现 %q", ban)
		}
	}
}

// TestPlayerPrompt_R2_IdiotNightDeathRuleDualMode 白痴夜间死亡规则双模式(§6.2/E 一致性)。
func TestPlayerPrompt_R2_IdiotNightDeathRuleDualMode(t *testing.T) {
	closedText := BuildSystemPrompt("", PersonalityVector{}, "", "", false)[0].Text
	if !strings.Contains(closedText, "夜间被狼刀 / 被毒杀时正常死亡,**不翻牌**,身份保密。") {
		t.Error("关闭模式应保留「白痴夜间死亡不翻牌身份保密」")
	}
	openText := BuildSystemPrompt("", PersonalityVector{}, "", "", true)[0].Text
	if !strings.Contains(openText, "身份按本局【死亡亮身份】规则当场公开") {
		t.Error("开启模式白痴夜间死亡规则应改为「当场公开」")
	}
	if strings.Contains(openText, "**不翻牌**,身份保密") {
		t.Error("开启模式不得保留「不翻牌身份保密」(与新规则矛盾)")
	}
}

// TestPlayerPrompt_R3_DeathWhitelistWithIdentity 【死亡白名单】带身份(§6.3)。
func TestPlayerPrompt_R3_DeathWhitelistWithIdentity(t *testing.T) {
	baseCtx := wwtypes.GameContext{
		Round:           2,
		Phase:           "dawn",
		MySeat:          0,
		AliveSeats:      []int{0, 1, 2},
		LastNightDeaths: []int{4, 7},
	}

	// 开启:死者 5 号已公开身份(hunter)、8 号(hunter_fired 白名单)已公开。
	openCtx := baseCtx
	openCtx.RevealRoleOnDeath = true
	openCtx.RevealedRoles = map[int]string{4: "villager", 7: "hunter"}
	open := BuildUserPrompt(openCtx)
	if !strings.Contains(open, "5号(身份:villager,已公开)") {
		t.Errorf("开启模式【死亡白名单】5 号应带身份:\n%s", open)
	}
	if !strings.Contains(open, "8号(身份:hunter,已公开)") {
		t.Errorf("开启模式【死亡白名单】8 号应带身份:\n%s", open)
	}
	if !strings.Contains(open, "【死亡白名单】") {
		t.Error("死亡白名单块缺失")
	}

	// 关闭(维持现状):普通死亡不在 RevealedRoles → 不写身份,输出与旧版一致。
	closedCtx := baseCtx
	closed := BuildUserPrompt(closedCtx)
	if strings.Contains(closed, "身份:") {
		t.Errorf("关闭模式死亡白名单不得出现身份:\n%s", closed)
	}
	// 关闭 + 白名单命中(如猎人开枪死者):身份已公开 → 显示(与公共信息一致)。
	closedCtx.RevealedRoles = map[int]string{7: "hunter"}
	closedWhitelist := BuildUserPrompt(closedCtx)
	if !strings.Contains(closedWhitelist, "8号(身份:hunter,已公开)") {
		t.Errorf("关闭模式白名单死者身份应显示(本就公开):\n%s", closedWhitelist)
	}

	// 防御:开启但死者不在 map(理论上不应发生)→ 不写身份。
	defensiveCtx := baseCtx
	defensiveCtx.RevealRoleOnDeath = true
	defensive := BuildUserPrompt(defensiveCtx)
	if strings.Contains(defensive, "身份:") {
		t.Errorf("开启但 map 未命中时防御性不写身份:\n%s", defensive)
	}
}

// TestPlayerPrompt_R4_AgentSetterSyncsFrozenBytes Agent 开关同步 + 冻结快照同源(§6.2)。
func TestPlayerPrompt_R4_AgentSetterSyncsFrozenBytes(t *testing.T) {
	// 模拟 NewWithRoom 构造期:按关闭模式(零值 false)冻结快照
	// (生产路径由 buildAgentContextLocked 的 seam 调用 SetRevealRoleOnDeath 同步)。
	a := &Agent{}
	a.systemPromptBytes = BuildSystemPromptBytes(a.SelfPortraitText, a.Personality, a.PersonalityPresetKey, a.DifficultyDirective, a.revealRoleOnDeath)

	// 默认(关闭):getter false,与 BuildSystemPrompt(false) 字节一致。
	if a.RevealRoleOnDeath() {
		t.Fatal("默认应为关闭模式")
	}
	closedBytes := BuildSystemPromptBytes("", PersonalityVector{}, "", "", false)
	if len(a.systemPromptBytes) != len(closedBytes) {
		t.Fatalf("默认冻结快照应与关闭模式一致: got %d bytes, want %d", len(a.systemPromptBytes), len(closedBytes))
	}

	// 切到开启:开关生效 + 冻结快照重算(invariant I11:请求路径与快照同源)。
	a.SetRevealRoleOnDeath(true)
	if !a.RevealRoleOnDeath() {
		t.Fatal("setter 后应为开启模式")
	}
	openBytes := BuildSystemPromptBytes("", PersonalityVector{}, "", "", true)
	if len(a.systemPromptBytes) != len(openBytes) {
		t.Fatalf("开启后冻结快照应与开启模式一致: got %d bytes, want %d", len(a.systemPromptBytes), len(openBytes))
	}
	if len(openBytes) == len(closedBytes) {
		t.Fatal("开/关两种模式的 system prompt 字节数应不同(文案已切换)")
	}

	// 幂等:重复同值调用零开销(快照不被破坏)。
	before := a.systemPromptBytes
	a.SetRevealRoleOnDeath(true)
	if len(a.systemPromptBytes) != len(before) {
		t.Fatal("幂等 setter 不应改变快照")
	}

	// nil-safety。
	var nilAgent *Agent
	nilAgent.SetRevealRoleOnDeath(true) // 不得 panic
	if nilAgent.RevealRoleOnDeath() {
		t.Fatal("nil Agent getter 应返回 false")
	}
}
