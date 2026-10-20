// Package virtual_city — agent_behavior_test.go: 批次62 行为金融上下文的
// **教学闭环**契约测试(§4.1:认知检定未通过 ⇒ 不得向 Agent 暴露偏差名)。
package virtual_city

import (
	"strings"
	"testing"

	"LsmAgentGame/game/virtual_city/profession"
)

// TestBehaviorBrief_NoLeakWhenUnrecognized §4.1 核心契约:
// 认知检定**未通过**的偏差,其名称与解释**绝不可**出现在 Agent 上下文里。
func TestBehaviorBrief_NoLeakWhenUnrecognized(t *testing.T) {
	w := NewWorld(62, [MaxSeats]profession.Card{})
	p := newPlayerFromCard(0, synthCard(0, 100000))
	assignBiasProfile(0, p)
	w.Players[0] = p
	// 强制画像含两个可区分的偏差;认知压到 0 ⇒ 检定必不通过。
	p.BiasProfile = []string{"loss_aversion", "overconfidence"}
	p.BiasImmunity = map[string]float64{}
	p.Cognition = 0
	brief := behaviorBriefLocked(p)
	if brief != "" {
		t.Errorf("unrecognized biases must yield an empty brief, got %q", brief)
	}
	for _, name := range []string{"损失厌恶", "过度自信", "loss_aversion", "overconfidence"} {
		if strings.Contains(brief, name) {
			t.Errorf("brief leaked bias %q: %q", name, brief)
		}
	}
	// 认知达标 ⇒ 允许出现偏差名(玩家已识破)。
	p.Cognition = 10
	brief = behaviorBriefLocked(p)
	if brief == "" {
		t.Fatal("recognized biases must produce a brief")
	}
	if !strings.Contains(brief, "损失厌恶") || !strings.Contains(brief, "过度自信") {
		t.Errorf("brief must name recognized biases: %q", brief)
	}
}

// TestBehaviorBrief_PreviewForcesRecognition 复盘预习:无论认知是否达标,
// 偏差一律视为"通过"(可被识破)⇒ 名字允许出现。
func TestBehaviorBrief_PreviewForcesRecognition(t *testing.T) {
	w := NewWorld(62, [MaxSeats]profession.Card{})
	p := newPlayerFromCard(0, synthCard(0, 100000))
	assignBiasProfile(0, p)
	w.Players[0] = p
	p.BiasProfile = []string{"loss_aversion"}
	p.BiasImmunity = map[string]float64{}
	p.Cognition = 0
	p.BiasPreviewNextTurn = true
	brief := behaviorBriefLocked(p)
	if !strings.Contains(brief, "损失厌恶") {
		t.Errorf("preview must make the bias recognizable: %q", brief)
	}
}

// TestBehaviorBrief_ResolvedAndImmuneNotLeaked 反思解除 / 免疫达标的偏差
// 不再作为"生效倾向"出现(但识破的仍可展示 —— 玩家知道自己的能力)。
func TestBehaviorBrief_ResolvedAndImmuneNotLeaked(t *testing.T) {
	w := NewWorld(62, [MaxSeats]profession.Card{})
	p := newPlayerFromCard(0, synthCard(0, 100000))
	assignBiasProfile(0, p)
	w.Players[0] = p
	p.BiasProfile = []string{"loss_aversion"}
	p.BiasImmunity = map[string]float64{"loss_aversion": 0.9} // 免疫 ≥0.80 ⇒ 不生效
	p.Cognition = 0
	brief := behaviorBriefLocked(p)
	// 免疫 ⇒ 效果不生效 ⇒ 不作为"倾向"展示。
	if strings.Contains(brief, "损失厌恶") {
		t.Errorf("immune bias must not be presented as an active tendency: %q", brief)
	}
}

// TestBubbleHerdLiquidityBriefs 四个上下文小节:内容契约 + 金额不泄漏。
func TestBubbleHerdLiquidityBriefs(t *testing.T) {
	w := NewWorld(62, [MaxSeats]profession.Card{})
	p := newPlayerFromCard(0, synthCard(0, 100000))
	assignBiasProfile(0, p)
	w.Players[0] = p
	w.StartGame()

	// 羊群小节:无历史 ⇒ 空;有历史 ⇒ 含方向与人数、**不含金额**。
	if got := herdBriefLocked(w); got != "" {
		t.Errorf("herd brief with no history must be empty: %q", got)
	}
	w.TurnSummaries = []TurnSummary{{Month: 1, Buy: 5, Sell: 1, Hold: 2, Majority: MajorityBuy}}
	hb := herdBriefLocked(w)
	for _, kw := range []string{"买入", "跟风", "逆势"} {
		if !strings.Contains(hb, kw) {
			t.Errorf("herd brief missing %q: %q", kw, hb)
		}
	}
	if strings.Contains(hb, "¥") {
		t.Errorf("herd brief must not leak amounts: %q", hb)
	}
	// 泡沫小节:状态 + 三项指标。
	bb := bubbleBriefLocked(w)
	for _, kw := range []string{"杠杆率", "换手率", "现金占比"} {
		if !strings.Contains(bb, kw) {
			t.Errorf("bubble brief missing %q: %q", kw, bb)
		}
	}
	// 流动性小节:各类资产价差。
	lb := liquidityBriefLocked(w)
	for _, kw := range []string{"股票", "债券", "黄金", "房产", "价差"} {
		if !strings.Contains(lb, kw) {
			t.Errorf("liquidity brief missing %q: %q", kw, lb)
		}
	}
}

// TestBehaviorJSON_NoLeakForUnrecognized my.behavior 快照:未识破的偏差
// 不得暴露 name_cn / effect / explanation(前端据此隐藏)。
func TestBehaviorJSON_NoLeakForUnrecognized(t *testing.T) {
	w := NewWorld(62, [MaxSeats]profession.Card{})
	p := newPlayerFromCard(0, synthCard(0, 100000))
	assignBiasProfile(0, p)
	w.Players[0] = p
	p.BiasProfile = []string{"loss_aversion", "overconfidence"}
	p.BiasImmunity = map[string]float64{}
	p.Cognition = 0
	js := behaviorJSONFor(p)
	if js == nil {
		t.Fatal("behavior snapshot must not be nil")
	}
	if len(js.BiasProfile) != 2 {
		t.Fatalf("bias profile size: got %d, want 2", len(js.BiasProfile))
	}
	for _, item := range js.BiasProfile {
		if item.Detected {
			continue
		}
		if item.NameCN != "" || item.Explanation != "" || item.Effect != "" {
			t.Errorf("undetected bias must not expose name/effect/explanation: %+v", item)
		}
	}
	// 认知达标 ⇒ 暴露名字与解释。
	p.Cognition = 10
	js = behaviorJSONFor(p)
	for _, item := range js.BiasProfile {
		if !item.Detected {
			continue
		}
		if item.NameCN == "" || item.Explanation == "" || item.Effect == "" {
			t.Errorf("detected bias must carry name/effect/explanation: %+v", item)
		}
	}
}
