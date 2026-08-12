// Package agent — prop_v5_test.go: 道具系统 v5 协同测试。
//
// 覆盖：
//  1. buildUsePropDynamicDescription 动态拼接(价格/中招率/经济档位销毁率)
//  2. mountFromRegistry 对 prop/wolf 工具的装配顺序 + Description fallback
//  3. v5 prop_status / prop_history 注册表挂载规则
package wwplayer

import (
	"LsmAgentGame/agent/wwtypes"
	"strings"
	"testing"
)

// TestBuildUsePropDynamicDescription_EmptySnapshot 验证无可购买道具时返回默认文案。
func TestBuildUsePropDynamicDescription_EmptySnapshot(t *testing.T) {
	if got := buildUsePropDynamicDescription(nil); got == "" {
		t.Fatalf("nil gc should return non-empty default text")
	}
	if got := buildUsePropDynamicDescription(&wwtypes.GameContext{}); !strings.Contains(got, "无可购买道具") {
		t.Fatalf("empty wwtypes.PropSnapshot should return '无可购买道具', got %s", got)
	}
}

// TestBuildUsePropDynamicDescription_IncludesEconTier 验证当前 EconTier 销毁率拼入描述。
func TestBuildUsePropDynamicDescription_IncludesEconTier(t *testing.T) {
	gc := &wwtypes.GameContext{
		EconTier: "critical",
		PropSnapshot: []wwtypes.PropSnapshot{
			{PropKey: "markdown_bomb", NameZh: "紧急公告", Price: 150, BaseHitRate: 30, IsAOE: false},
		},
	}
	got := buildUsePropDynamicDescription(gc)
	if !strings.Contains(got, "紧急公告") {
		t.Fatalf("dynamic desc should list prop name, got %s", got)
	}
	if !strings.Contains(got, "150币") {
		t.Fatalf("dynamic desc should list prop price, got %s", got)
	}
	if !strings.Contains(got, "critical") {
		t.Fatalf("dynamic desc should mention current tier 'critical', got %s", got)
	}
	if !strings.Contains(got, "60%") {
		t.Fatalf("Critical tier absorb rate should be 60%%, got %s", got)
	}
}

// TestBuildUsePropDynamicDescription_DefaultTierHealth 验证 EconTier 为空时 fallback 到 Health(30%)。
func TestBuildUsePropDynamicDescription_DefaultTierHealth(t *testing.T) {
	gc := &wwtypes.GameContext{
		EconTier: "", // 未设置
		PropSnapshot: []wwtypes.PropSnapshot{
			{PropKey: "long_swear", NameZh: "长篇废话", Price: 250, BaseHitRate: 35, IsAOE: true},
		},
	}
	got := buildUsePropDynamicDescription(gc)
	if !strings.Contains(got, "health") {
		t.Fatalf("empty tier should fallback to health, got %s", got)
	}
}

// TestPropStatusMountedWithNoPropSnapshot 验证 prop_status 即使无可购买道具也挂载
// (v4 §G2 行为保留 — 让 LLM 随时能查"现在能不能用")。
func TestPropStatusMountedWithNoPropSnapshot(t *testing.T) {
	gc := &wwtypes.GameContext{}
	specs := MountTools(ToolPhaseSpeak, gc)
	found := false
	for _, s := range specs {
		if s.Name == "prop_status" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("prop_status should mount even with empty wwtypes.PropSnapshot, got %v", namesOf(specs))
	}
}

// TestPropHistoryHiddenWhenEmpty 验证 prop_history 在 PropHistorySnapshot 为空时**不**挂载
// (v4 §G2 行为保留 — 无历史就别暴露)。
func TestPropHistoryHiddenWhenEmpty(t *testing.T) {
	gc := &wwtypes.GameContext{} // PropHistorySnapshot=nil
	specs := MountTools(ToolPhaseSpeak, gc)
	for _, s := range specs {
		if s.Name == "prop_history" {
			t.Fatalf("prop_history should not mount when PropHistorySnapshot is empty, got %v", namesOf(specs))
		}
	}
}

// TestPropHistoryShownWhenNonEmpty 验证 prop_history 在 PropHistorySnapshot 非空时挂载。
func TestPropHistoryShownWhenNonEmpty(t *testing.T) {
	gc := &wwtypes.GameContext{
		PropHistorySnapshot: []wwtypes.PropHistoryRecord{{FromSeat: 0, ToSeat: 1, PropKey: "markdown_bomb"}},
	}
	specs := MountTools(ToolPhaseSpeak, gc)
	found := false
	for _, s := range specs {
		if s.Name == "prop_history" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("prop_history should mount when PropHistorySnapshot non-empty, got %v", namesOf(specs))
	}
}

// TestWolfWhisperMountsOnlyForWolfFaction 验证 wolf_whisper 仍按 §13.1 规则挂载。
// §20260810-04 U1 — WolfTeammateSeat>=0 不再门控挂载(通道与 30% 互知解耦);
// 仅 faction=="wolf" 仍是挂载前置。
func TestWolfWhisperMountsOnlyForWolfFaction(t *testing.T) {
	good := MountTools(ToolPhaseSpeak, &wwtypes.GameContext{Faction: "good"})
	if containsName(good, "wolf_whisper") {
		t.Fatalf("wolf_whisper should not mount for good faction")
	}
	// §20260810-04 U1 — WolfTeammateSeat=-1 现在应挂载(通道对所有狼 bot 开放)。
	wolfNoTeammate := MountTools(ToolPhaseSpeak, &wwtypes.GameContext{Faction: "wolf", WolfTeammateSeat: -1})
	if !containsName(wolfNoTeammate, "wolf_whisper") {
		t.Fatalf("wolf_whisper should mount when faction=wolf regardless of WolfTeammateSeat (U1)")
	}
	wolfWithTeammate := MountTools(ToolPhaseSpeak, &wwtypes.GameContext{Faction: "wolf", WolfTeammateSeat: 3})
	if !containsName(wolfWithTeammate, "wolf_whisper") {
		t.Fatalf("wolf_whisper should mount when faction=wolf")
	}
	// §20260810-04 U1 — 同样挂载在 night 阶段(配套 mountFromRegistry call)。
	wolfNight := MountTools(ToolPhaseNight, &wwtypes.GameContext{Faction: "wolf", WolfTeammateSeat: -1})
	if !containsName(wolfNight, "wolf_whisper") {
		t.Fatalf("wolf_whisper should mount at ToolPhaseNight when faction=wolf (U1)")
	}
}