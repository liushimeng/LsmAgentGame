package agent

import "testing"

// TestAgentClassWerewolfMemoryCompact_Wired 校验狼人杀 MemoryCompact 的
// AgentClassName 已按 §24 登记(class_names.go 常量 + AllAgentClassNames +
// IsValidAgentClassName)。2026-08-25 §20260825-01 之前该字面量硬编码在
// wwplayer/memory_compact.go 且未登记,存在「散写字面量」合规缺口。
func TestAgentClassWerewolfMemoryCompact_Wired(t *testing.T) {
	if AgentClassWerewolfMemoryCompact == "" {
		t.Fatal("AgentClassWerewolfMemoryCompact must be non-empty (§24)")
	}
	if string(AgentClassWerewolfMemoryCompact) != "LsmAgentGame-Werewolf-MemoryCompact" {
		t.Errorf("unexpected AgentClassName: %q", AgentClassWerewolfMemoryCompact)
	}
	found := false
	for _, c := range AllAgentClassNames() {
		if c == AgentClassWerewolfMemoryCompact {
			found = true
			break
		}
	}
	if !found {
		t.Error("AgentClassWerewolfMemoryCompact must be registered in AllAgentClassNames()")
	}
	if !IsValidAgentClassName(string(AgentClassWerewolfMemoryCompact)) {
		t.Error("AgentClassWerewolfMemoryCompact must pass IsValidAgentClassName")
	}
}

// TestAgentClassCityPlayer_Wired 校验虚拟城市玩家 Bot 的 AgentClassName 已按
// §24 登记(常量 + AllAgentClassNames + IsValidAgentClassName)——防 §130
// 「声明了却从不接线」复发(2026-09-14 §财商流P0,G3 门禁;
// 2026-09-21 §虚拟城市-Agent命名City化: Wealth→City)。
func TestAgentClassCityPlayer_Wired(t *testing.T) {
	if AgentClassCityPlayer == "" {
		t.Fatal("AgentClassCityPlayer must be non-empty (§24)")
	}
	if string(AgentClassCityPlayer) != "LsmAgentGame-City-Player" {
		t.Errorf("unexpected AgentClassName: %q", AgentClassCityPlayer)
	}
	found := false
	for _, c := range AllAgentClassNames() {
		if c == AgentClassCityPlayer {
			found = true
			break
		}
	}
	if !found {
		t.Error("AgentClassCityPlayer must be registered in AllAgentClassNames()")
	}
	if !IsValidAgentClassName(string(AgentClassCityPlayer)) {
		t.Error("AgentClassCityPlayer must pass IsValidAgentClassName")
	}
}

// TestAgentClassCityVoice_Wired 校验虚拟城市「城市之声」的
// AgentClassName 已按 §24 两步登记(常量 + AllAgentClassNames +
// IsValidAgentClassName)—— 防止 voice.go 散写字面量(2026-09-21
// §虚拟城市-城市Agent规模化,契约 03 §5;同日 §虚拟城市-Agent命名City化:
// Wealth→City)。
func TestAgentClassCityVoice_Wired(t *testing.T) {
	if AgentClassCityVoice == "" {
		t.Fatal("AgentClassCityVoice must be non-empty (§24)")
	}
	if string(AgentClassCityVoice) != "LsmAgentGame-City-Voice" {
		t.Errorf("unexpected AgentClassName: %q", AgentClassCityVoice)
	}
	found := false
	for _, c := range AllAgentClassNames() {
		if c == AgentClassCityVoice {
			found = true
			break
		}
	}
	if !found {
		t.Error("AgentClassCityVoice must be registered in AllAgentClassNames()")
	}
	if !IsValidAgentClassName(string(AgentClassCityVoice)) {
		t.Error("AgentClassCityVoice must pass IsValidAgentClassName")
	}
}

// TestAllAgentClassNames_NonEmptyAndUnique 是 §24 的通用不变量:所有登记进
// AllAgentClassNames 的常量必须非空且互不重复(防将来复制粘贴出同名/空常量)。
func TestAllAgentClassNames_NonEmptyAndUnique(t *testing.T) {
	seen := make(map[AgentClassName]struct{}, len(AllAgentClassNames()))
	for _, c := range AllAgentClassNames() {
		if c == "" {
			t.Error("AllAgentClassNames() must not contain empty AgentClassName")
		}
		if _, dup := seen[c]; dup {
			t.Errorf("duplicate AgentClassName in AllAgentClassNames(): %q", c)
		}
		seen[c] = struct{}{}
	}
}
