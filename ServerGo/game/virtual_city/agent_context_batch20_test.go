// Package virtual_city — agent_context_batch20_test.go: 三块 GameContext 注入小节
// 的字节预算与内容契约(批次20 文档2 §4.3 / 文档3 A5/B4)。
package virtual_city

import (
	"strings"
	"testing"

	"LsmAgentGame/game/virtual_city/profession"
)

func briefWorld(t *testing.T, seats int) *World {
	t.Helper()
	w := NewWorld(77, [MaxSeats]profession.Card{})
	for s := 0; s < seats; s++ {
		w.Players[s] = newPlayerFromCard(s, synthCard(s, 100000))
	}
	w.StartGame()
	return w
}

// TestSideMarketBrief_BudgetAndContent 副业定价小节:无副业 "" / 有副业含
// 品类·档位·份额·上月实收·规则行·<0.4 策略提示;12 座位全竞争 ≤350B。
func TestSideMarketBrief_BudgetAndContent(t *testing.T) {
	w := briefWorld(t, 2)
	r := NewVirtualCityRoom("room-sm", 3000, 5, 4)
	r.World = w
	if got := sideMarketBriefLocked(r, 0); got != "" {
		t.Fatalf("no side business must be empty: %q", got)
	}
	// seat0 高价 vs seat1 低价 → 份额 0.2/0.7 ≈ 0.286 < 0.4 → 策略提示;
	// 边界核对:恰 0.4 不提示(严格小于)。
	w.Players[0].SideBusiness = &SideBusiness{Kind: "delivery", BaseIncome: 3250, OpenedMonth: 1, PriceTier: PriceTierHigh}
	w.Players[1].SideBusiness = &SideBusiness{Kind: "delivery", BaseIncome: 3250, OpenedMonth: 1, PriceTier: PriceTierLow}
	brief := sideMarketBriefLocked(r, 0)
	for _, kw := range []string{"跑腿配送", "高价", "份额", "对手", "集体低价"} {
		if !strings.Contains(brief, kw) {
			t.Errorf("brief missing %q: %q", kw, brief)
		}
	}
	if !strings.Contains(brief, "份额偏低") {
		t.Errorf("share<0.4 must carry strategy hint: %q", brief)
	}
	// 12 座位全同品类(极端对手列表)→ 预算 350B + 截断生效。
	for s := 2; s < MaxSeats; s++ {
		w.Players[s] = newPlayerFromCard(s, synthCard(s, 1000))
		w.Players[s].SideBusiness = &SideBusiness{Kind: "delivery", BaseIncome: 1000, OpenedMonth: 1, PriceTier: PriceTierLow}
	}
	wide := sideMarketBriefLocked(r, 0)
	if len(wide) > 350 {
		t.Fatalf("12-seat brief exceeds 350B: %d bytes: %q", len(wide), wide)
	}
	// 对手信息不含 BaseIncome(仅档位/份额)。
	if strings.Contains(wide, "¥3250") || strings.Contains(wide, "¥1000") {
		// 自己上月实收允许(¥0),但对手金额禁止 —— 严格:任何 4 位金额来自对手行。
		for _, line := range strings.Split(wide, ";") {
			if strings.Contains(line, "号位") && strings.Contains(line, "¥") {
				t.Errorf("rival line leaks income detail: %q", line)
			}
		}
	}
}

// TestElectionBrief 选举小节:未启用 "" / 现任市长自指 / 他人市长 /
// 票型排名行 / 人脉提示。
func TestElectionBrief(t *testing.T) {
	w := briefWorld(t, 2)
	r := NewVirtualCityRoom("room-el", 3000, 5, 4)
	r.World = w
	if got := electionBriefLocked(w, 0); got != "" {
		t.Fatalf("disabled must be empty: %q", got)
	}
	w.Election.Enabled = true
	w.Election.MayorSeat = 0
	w.Election.LastElectionMonth = 1
	brief := electionBriefLocked(w, 0)
	if !strings.Contains(brief, "你是现任市长") || !strings.Contains(brief, "第 49 月") {
		t.Errorf("mayor self brief: %q", brief)
	}
	brief1 := electionBriefLocked(w, 1)
	if !strings.Contains(brief1, "现任市长为 0 号位") {
		t.Errorf("non-mayor brief: %q", brief1)
	}
	if !strings.Contains(brief1, "socialize") {
		t.Errorf("must carry social-capital hint: %q", brief1)
	}
	// 有历史票型 → 排名行。
	w.Election.LastVotes = []ElectionVote{{Seat: 1, Score: 90}, {Seat: 0, Score: 60}}
	if got := electionBriefLocked(w, 0); !strings.Contains(got, "第 2 名") {
		t.Errorf("rank line missing: %q", got)
	}
	_ = r
}

// TestMicroPriceBrief 微观小节:无信息 "" / 持仓双价行 / T+1 冻结数 /
// 熔断行;≤120B。
func TestMicroPriceBrief(t *testing.T) {
	w := briefWorld(t, 2)
	if got := microPriceBriefLocked(w, w.Players[0]); got != "" {
		t.Fatalf("no stock no breaker must be empty: %q", got)
	}
	w.Players[0].Assets = append(w.Players[0].Assets, Asset{Kind: AssetStockIndex, Units: 800, OpenMonth: 1})
	w.Players[0].StockT1Locked = 800
	w.Market.CyclePhase = PhaseDepression // 恐慌 15bp
	brief := microPriceBriefLocked(w, w.Players[0])
	for _, kw := range []string{"买", "卖", "T+1 冻结 800", "bp"} {
		if !strings.Contains(brief, kw) {
			t.Errorf("brief missing %q: %q", kw, brief)
		}
	}
	if len(brief) > 120 {
		t.Fatalf("hold+T1 brief exceeds 120B: %d %q", len(brief), brief)
	}
	w.Market.BreakerUntilMonth = w.Month
	w.Market.StockIndex = 12345.678 // 极端数值也不得超预算(截断兜底)
	brk := microPriceBriefLocked(w, w.Players[0])
	if !strings.Contains(brk, "熔断") || len(brk) > 120 {
		t.Errorf("breaker line: len=%d %q", len(brk), brk)
	}
}

// TestBuildContextForAgent_Batch20Fields 集成:BuildContextForAgent 三字段
// 填充(定价市场 + 选举 + 微观),房间构造走零锁路径。
func TestBuildContextForAgent_Batch20Fields(t *testing.T) {
	w := briefWorld(t, 2)
	w.Election.Enabled = true
	w.Election.MayorSeat = 0
	w.Players[0].SideBusiness = &SideBusiness{Kind: "delivery", BaseIncome: 3250, OpenedMonth: 1}
	w.Players[0].Assets = append(w.Players[0].Assets, Asset{Kind: AssetStockIndex, Units: 500, OpenMonth: 1})
	w.Players[0].StockT1Locked = 500
	r := NewVirtualCityRoom("room-all", 3000, 5, 4)
	r.World = w
	ctx, ok := BuildContextForAgent(r, 0)
	if !ok || ctx == nil {
		t.Fatal("BuildContextForAgent failed")
	}
	if ctx.SideMarketBrief == "" || ctx.ElectionBrief == "" || ctx.MicroPriceBrief == "" {
		t.Errorf("briefs not injected: sm=%q el=%q mp=%q", ctx.SideMarketBrief, ctx.ElectionBrief, ctx.MicroPriceBrief)
	}
}
