// Package virtual_city — side_market_test.go: 副业竞争比价与定价战单测(批次20 文档2 §6)。
//
// 覆盖:份额公式表(n=1..4,含囚徒困境四象限锚点)/ 档位 gate 矩阵(4 品类×3 档×
// 认知门槛)/ 改档月限 / set_side_price 动作 / start_side_business tier 兼容 /
// content 高价×0.5 / delivery 高价精力额外 −1 / 竞争 event 去重。
package virtual_city

import (
	"math"
	"strings"
	"testing"

	"LsmAgentGame/errcode"
	"LsmAgentGame/game/virtual_city/profession"
)

// nearlyEq 浮点容差比较(份额公式含除法,允许 1e-9 噪声)。
func nearlyEq(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// TestSideTier_WeightsAndMultipliers 档位常量表(文档2 §1:50/30/20 × 0.75/1.0/1.25)。
func TestSideTier_WeightsAndMultipliers(t *testing.T) {
	cases := []struct {
		tier   int
		weight float64
		mult   float64
		cn     string
	}{
		{PriceTierLow, 0.50, 0.75, "低价"},
		{PriceTierMid, 0.30, 1.00, "中价"},
		{PriceTierHigh, 0.20, 1.25, "高价"},
	}
	for _, c := range cases {
		if !nearlyEq(sideTierWeight(c.tier), c.weight) {
			t.Errorf("weight(%d): got %f, want %f", c.tier, sideTierWeight(c.tier), c.weight)
		}
		if !nearlyEq(sideTierMultiplier(c.tier), c.mult) {
			t.Errorf("multiplier(%d): got %f, want %f", c.tier, sideTierMultiplier(c.tier), c.mult)
		}
		if sideTierCN(c.tier) != c.cn {
			t.Errorf("cn(%d): got %s, want %s", c.tier, sideTierCN(c.tier), c.cn)
		}
	}
	// 非法档位入口校验。
	if sideTierValid(-1) || sideTierValid(3) {
		t.Error("sideTierValid must reject -1/3")
	}
	for _, v := range []int{PriceTierMid, PriceTierLow, PriceTierHigh} {
		if !sideTierValid(v) {
			t.Errorf("sideTierValid(%d) must be true", v)
		}
	}
}

// sideWorld 定价测试世界:按 kind/tier 布置存活经营者。
func sideWorld(t *testing.T, entries ...struct {
	seat int
	kind string
	tier int
}) *World {
	t.Helper()
	cards := make([]profession.Card, MaxSeats)
	for i := range cards {
		cards[i] = profession.Card{ID: "X"}
	}
	w := NewWorld(7, [MaxSeats]profession.Card{})
	for _, e := range entries {
		p := newPlayerFromCard(e.seat, profession.Card{ID: "S", Savings: 1000})
		p.SideBusiness = &SideBusiness{Kind: e.kind, BaseIncome: 1000, OpenedMonth: 1, PriceTier: e.tier}
		w.Players[e.seat] = p
	}
	w.StartGame()
	return w
}

// TestSideMarketShares_Formula 份额公式枚举(文档2 §1:s_i = w(t_i)/Σw;独占=1)。
// n=1(任意档)→ 1.0;n=2 双中 → 0.5/0.5;n=3/4 多分布手算钉死。
func TestSideMarketShares_Formula(t *testing.T) {
	// n=1:三档独占全部 = 1.0(无竞争不惩罚)。
	for tier := 0; tier <= 2; tier++ {
		w := sideWorld(t, []struct {
			seat int
			kind string
			tier int
		}{{0, "delivery", tier}}...)
		s := sideMarketShares(w)
		if !nearlyEq(s["delivery"][0], 1.0) {
			t.Errorf("n=1 tier=%d share: got %f, want 1.0", tier, s["delivery"][0])
		}
	}
	cases := []struct {
		name   string
		seed   int64
		tiers  []int
		want   []float64
	}{
		// 双中:Σw=0.6 → 各 0.5。
		{"mid/mid", 0, []int{0, 0}, []float64{0.5, 0.5}},
		// 我低对手中(囚徒困境):Σ=0.8 → 0.625/0.375。
		{"low/mid", 0, []int{1, 0}, []float64{0.625, 0.375}},
		// 双低:Σ=1.0 → 各 0.5。
		{"low/low", 0, []int{1, 1}, []float64{0.5, 0.5}},
		// 我高对手中(§1 公式口径):Σ=0.5 → 0.4/0.6。
		{"high/mid", 0, []int{2, 0}, []float64{0.4, 0.6}},
		// n=3 低/中/高:Σ=1.0 → 0.5/0.3/0.2。
		{"3-way", 0, []int{1, 0, 2}, []float64{0.5, 0.3, 0.2}},
		// n=4 高/高/中/低:Σ=0.2+0.2+0.3+0.5=1.2 → 2/12,2/12,3/12,5/12。
		{"4-way", 0, []int{2, 2, 0, 1}, []float64{2.0 / 12.0, 2.0 / 12.0, 3.0 / 12.0, 5.0 / 12.0}},
	}
	for _, c := range cases {
		entries := make([]struct {
			seat int
			kind string
			tier int
		}, len(c.tiers))
		for i, tier := range c.tiers {
			entries[i] = struct {
				seat int
				kind string
				tier int
			}{seat: i * 2, kind: "content", tier: tier}
		}
		w := sideWorld(t, entries...)
		s := sideMarketShares(w)["content"]
		sum := 0.0
		for i, want := range c.want {
			seat := i * 2
			if !nearlyEq(s[seat], want) {
				t.Errorf("%s seat %d share: got %f, want %f", c.name, seat, s[seat], want)
			}
			sum += s[seat]
		}
		if !nearlyEq(sum, 1.0) {
			t.Errorf("%s shares must sum to 1, got %f", c.name, sum)
		}
	}
}

// TestSideMarketShares_PrisonersDilemma 囚徒困境四象限锚点(文档2 §1 表)。
// 实收系数 = m(t)×s;收入 = Base×系数×Brass×variance(此处只校验系数)。
func TestSideMarketShares_PrisonersDilemma(t *testing.T) {
	factor := func(w *World, seat int, tier int) float64 {
		s := sideMarketShares(w)
		sb := w.Players[seat].SideBusiness
		return sideTierMultiplier(sb.PriceTier) * s[sb.Kind][seat]
	}
	mk := func(myTier, oppTier int) *World {
		return sideWorld(t,
			struct {
				seat int
				kind string
				tier int
			}{0, "delivery", myTier},
			struct {
				seat int
				kind string
				tier int
			}{2, "delivery", oppTier})
	}
	type cell struct{ my, opp float64 }
	grid := map[[2]int]cell{
		{PriceTierMid, PriceTierMid}:   {0.50, 0.50},  // 各 0.50×Base
		{PriceTierLow, PriceTierMid}:   {0.46875, 0.375}, // 我 0.375→0.469 / 对方 0.375
		{PriceTierMid, PriceTierLow}:   {0.375, 0.46875},
		{PriceTierLow, PriceTierLow}:   {0.375, 0.375},  // 各 0.375×Base
		{PriceTierHigh, PriceTierMid}:  {0.5, 0.6},      // 高价利基(公式口径)
		{PriceTierMid, PriceTierHigh}:  {0.6, 0.5},
		{PriceTierHigh, PriceTierHigh}: {0.625, 0.625}, // Σ=0.4 → s=0.5 ×1.25
	}
	for key, want := range grid {
		myTier, oppTier := key[0], key[1]
		w := mk(myTier, oppTier)
		if got := factor(w, 0, myTier); !nearlyEq(got, want.my) {
			t.Errorf("quadrant (me=%d, opp=%d): my factor got %f, want %f", myTier, oppTier, got, want.my)
		}
		if got := factor(w, 2, oppTier); !nearlyEq(got, want.opp) {
			t.Errorf("quadrant (me=%d, opp=%d): opp factor got %f, want %f", myTier, oppTier, got, want.opp)
		}
	}
	// 博弈性质:对手中价时,我的最优回应是低价(0.469>0.375);双低 0.375 < 双中 0.50
	// (集体降价集体受损);单方面守中面对低价对手受损 0.375(被背刺者)。
	if !(0.75*0.625 > 1.0*0.375 && 0.75*0.5 < 1.0*0.5) {
		t.Fatal("prisoner dilemma anchor broken: 降价抢客占优且集体降价受损")
	}
}

// TestSideTierGate_Matrix 档位 gate 矩阵(文档2 §1 规则表;4 品类 × 3 档 × 认知)。
func TestSideTierGate_Matrix(t *testing.T) {
	p := &Player{Cognition: 3}
	cases := []struct {
		kind   string
		tier   int
		cog    int
		reject bool
	}{
		{"delivery", PriceTierLow, 0, false},   // 低价无门槛
		{"delivery", PriceTierMid, 0, false},
		{"delivery", PriceTierHigh, 0, false},  // 高价无动作门槛(月结精力-1)
		{"content", PriceTierHigh, 0, false},   // 高价无动作门槛(月结×0.5)
		{"content", PriceTierHigh, 2, false},
		{"tutoring", PriceTierHigh, 4, true},   // gate=4 → 高价需 ≥5
		{"tutoring", PriceTierHigh, 5, false},
		{"freelance", PriceTierHigh, 3, true},  // gate=3 → 高价需 ≥4
		{"freelance", PriceTierHigh, 4, false},
		{"tutoring", PriceTierLow, 0, false},   // 低价无门槛
	}
	for _, c := range cases {
		p.Cognition = c.cog
		def := sideBizDefs[c.kind]
		e := sideTierGate(p, def, c.tier)
		if c.reject && e == nil {
			t.Errorf("%s tier=%d cog=%d: must reject", c.kind, c.tier, c.cog)
		}
		if !c.reject && e != nil {
			t.Errorf("%s tier=%d cog=%d: must pass, got %v", c.kind, c.tier, c.cog, e)
		}
		if e != nil && e.Code != errcode.ErrVirtualCityGateFailed {
			t.Errorf("%s high-tier reject must be 35006, got %d", c.kind, e.Code)
		}
	}
}

// TestAction_StartSideBusiness_Tier start_side_business tier 参数:缺省中价
// (兼容旧客户端/旧 Agent)、显式低/高价、非法档拒绝、tutoring 高价认知 gate。
func TestAction_StartSideBusiness_Tier(t *testing.T) {
	// 缺省 tier(不带参数)→ 中价,行为与旧版逐字一致(文案无定价括号)。
	w := sideWorld(t)
	p := w.Players[0]
	if p == nil {
		p = newPlayerFromCard(0, profession.Card{ID: "S", Savings: 1000, Cognition: 6})
		w.Players[0] = p
	}
	p.Cognition = 6
	p.ActionBudget = 3
	text, e := w.ApplyAction(0, Action{Type: ActStartSide, Kind: "delivery"})
	if e != nil {
		t.Fatalf("legacy start (no tier) must succeed: %v", e)
	}
	if !strings.HasPrefix(text, "启动副业(跑腿配送),月入约 ¥2500–4000") || strings.Contains(text, "定价") {
		t.Errorf("legacy text changed: %q", text)
	}
	if p.SideBusiness.PriceTier != PriceTierMid || p.SideBusiness.TierSetMonth != 0 {
		t.Errorf("default tier: got %d/%d, want 0/0", p.SideBusiness.PriceTier, p.SideBusiness.TierSetMonth)
	}
	w.applySideStop(0)

	// 带 tier=1(低价)→ 文案含定价信息。
	text, e = w.ApplyAction(0, Action{Type: ActStartSide, Kind: "delivery", Tier: PriceTierLow})
	if e != nil {
		t.Fatalf("start tier=1: %v", e)
	}
	if p.SideBusiness.PriceTier != PriceTierLow || !strings.Contains(text, "定价低价档(客群50%)") {
		t.Errorf("tier=1: got tier=%d text=%q", p.SideBusiness.PriceTier, text)
	}
	w.applySideStop(0)

	// tutoring 高价需认知 ≥ gate+1(=5);4 拒绝、5 成功。
	p.Cognition = 4
	if _, e = w.ApplyAction(0, Action{Type: ActStartSide, Kind: "tutoring", Tier: PriceTierHigh}); e == nil {
		t.Error("tutoring high-tier at cog 4 must reject")
	}
	p.Cognition = 5
	if _, e = w.ApplyAction(0, Action{Type: ActStartSide, Kind: "tutoring", Tier: PriceTierHigh}); e != nil {
		t.Errorf("tutoring high-tier at cog 5 must pass: %v", e)
	}
	w.applySideStop(0)

	// 非法 tier → 35006 带中文原因(补预算:前面成功动作已耗尽 3 点)。
	p.ActionBudget = 3
	if _, e = w.ApplyAction(0, Action{Type: ActStartSide, Kind: "delivery", Tier: 5}); e == nil || e.Code != errcode.ErrVirtualCityGateFailed {
		t.Errorf("invalid tier must be 35006, got %v", e)
	}
}

// applySideStop 测试辅助:清副业与 side_business 资产(同 actStopSide 语义)。
func (w *World) applySideStop(seat int) {
	p := w.Players[seat]
	p.SideBusiness = nil
	w.removeAsset(p, AssetSideBusiness, 0)
}

// TestAction_SetSidePrice set_side_price:预算 1(working)/每月 1 次/无副业拒绝/
// 非法档拒绝/成功文案/次月可再改。
func TestAction_SetSidePrice(t *testing.T) {
	w := sideWorld(t)
	p := newPlayerFromCard(0, profession.Card{ID: "S", Savings: 1000, Cognition: 6})
	p.ActionBudget = 3
	w.Players[0] = p
	w.StartGame()
	if _, e := w.ApplyAction(0, Action{Type: ActStartSide, Kind: "delivery"}); e != nil {
		t.Fatalf("start side: %v", e)
	}
	budgetBefore := p.ActionBudget

	// 无副业座位拒绝。
	w.Players[1] = newPlayerFromCard(1, profession.Card{ID: "S2", Savings: 100})
	w.Players[1].ActionBudget = 3
	if _, e := w.ApplyAction(1, Action{Type: ActSetSidePrice, Tier: PriceTierLow}); e == nil ||
		e.Code != errcode.ErrVirtualCityGateFailed {
		t.Errorf("no side business must 35006, got %v", e)
	}

	// 改低价成功:文案 + TierSetMonth 记账 + 预算 −1(working)。
	text, e := w.ApplyAction(0, Action{Type: ActSetSidePrice, Tier: PriceTierLow})
	if e != nil {
		t.Fatalf("set low: %v", e)
	}
	if text != "副业改价:低价档(客群50%)" {
		t.Errorf("text: got %q", text)
	}
	if p.SideBusiness.PriceTier != PriceTierLow || p.SideBusiness.TierSetMonth != w.Month {
		t.Errorf("state: tier=%d setMonth=%d (month=%d)", p.SideBusiness.PriceTier, p.SideBusiness.TierSetMonth, w.Month)
	}
	if p.ActionBudget != budgetBefore-1 {
		t.Errorf("budget: got %d, want %d", p.ActionBudget, budgetBefore-1)
	}

	// 同月二次改价 → 35006「每月限 1 次」。
	if _, e := w.ApplyAction(0, Action{Type: ActSetSidePrice, Tier: PriceTierMid}); e == nil ||
		!strings.Contains(e.Message, "每月限 1 次") {
		t.Errorf("same-month second change must reject: %v", e)
	}
	// 非法 tier → 35006(同月限制不抢先;非法检查在前)。
	w.Month++
	if _, e := w.ApplyAction(0, Action{Type: ActSetSidePrice, Tier: 9}); e == nil || e.Code != errcode.ErrVirtualCityGateFailed {
		t.Errorf("invalid tier must 35006, got %v", e)
	}
	// 次月改中价成功。
	if _, e := w.ApplyAction(0, Action{Type: ActSetSidePrice, Tier: PriceTierMid}); e != nil {
		t.Errorf("next month change: %v", e)
	}
}

// settleSideCase 跑单座位 settlePlayer 拿副业结算(同 seed → salary/variance
// 抽样逐位一致,可跨世界精确对比倍率)。
func settleSideCase(t *testing.T, kind string, tier int, cog int) (int64, int, string) {
	t.Helper()
	w := NewWorld(99, [MaxSeats]profession.Card{})
	p := newPlayerFromCard(0, profession.Card{ID: "S", Savings: 500000, Salary: 20000, Expense: 5000, Energy: 9, Cognition: cog})
	w.Players[0] = p
	p.SideBusiness = &SideBusiness{Kind: kind, BaseIncome: 4000, OpenedMonth: 1, PriceTier: tier}
	w.StartGame()
	p.Energy = 9
	w.settlePlayer(p, 30, sideMarketShares(w))
	text := ""
	for _, d := range p.Monthly.Detail {
		if d.Key == "side" {
			text = d.Text
		}
	}
	return p.Monthly.SideIncome, p.Energy, text
}

// TestSettlement_SideTierEffects 高价档品类效果:content ×0.5(认知<3)/
// ×1.0(认知≥3)精确关系;delivery 高价额外精力 −1;倍率 ±2 容差。
func TestSettlement_SideTierEffects(t *testing.T) {
	// content 高价:认知 2 → 折半;认知 3 → 不折半。同 seed → 同 variance。
	side2, _, text2 := settleSideCase(t, "content", PriceTierHigh, 2)
	side3, _, text3 := settleSideCase(t, "content", PriceTierHigh, 3)
	if !strings.Contains(text2, "涨粉折半") {
		t.Errorf("cog2 high content must note 折半: %q", text2)
	}
	if strings.Contains(text3, "折半") {
		t.Errorf("cog3 high content must not halve: %q", text3)
	}
	if side2 != int64(float64(side3)*0.5+0.5) {
		t.Errorf("content high halve relation: got %d, want %d(=%d×0.5)", side2, int64(float64(side3)*0.5+0.5), side3)
	}
	// delivery 高价精力 = 中价 −1(额外透支;两世界其余能耗路径同 seed 一致)。
	_, energyHigh, _ := settleSideCase(t, "delivery", PriceTierHigh, 0)
	_, energyMid, _ := settleSideCase(t, "delivery", PriceTierMid, 0)
	if energyMid != 8 {
		t.Fatalf("mid delivery energy(9−2 副业+1 月度恢复): got %d, want 8", energyMid)
	}
	if energyHigh != energyMid-1 {
		t.Errorf("high delivery must cost extra −1 energy: got %d, want %d", energyHigh, energyMid-1)
	}
	// 收入乘数倍率(独占):高价 ≈ 中价×1.25,低价 ≈ 中价×0.75(±2 元舍入带)。
	mid, _, _ := settleSideCase(t, "tutoring", PriceTierMid, 6)
	high, _, _ := settleSideCase(t, "tutoring", PriceTierHigh, 6)
	low, _, _ := settleSideCase(t, "tutoring", PriceTierLow, 6)
	if d := math.Abs(float64(high) - 1.25*float64(mid)); d > 3 {
		t.Errorf("high multiplier band: high=%d mid=%d diff=%.1f", high, mid, d)
	}
	if d := math.Abs(float64(low) - 0.75*float64(mid)); d > 3 {
		t.Errorf("low multiplier band: low=%d mid=%d diff=%.1f", low, mid, d)
	}
	// 高价档文案括号(文档2 §3 示例形态)。
	if !strings.Contains(highText(text3), "高价×125%") {
		t.Errorf("high text paren missing: %q", text3)
	}
}

func highText(s string) string { return s }

// TestSideMarket_ShareCutInSettlement 双经营者切分实收:两 seat 同品类同 seed
// 世界互验 —— 份额 0.5/0.5 时收入 ≈ 独占中价的一半(±3 舍入带)。
func TestSideMarket_ShareCutInSettlement(t *testing.T) {
	w := NewWorld(5, [MaxSeats]profession.Card{})
	cards := []profession.Card{
		{ID: "A", Savings: 900000, Salary: 20000, Expense: 5000, Energy: 9, Cognition: 6},
		{ID: "B", Savings: 900000, Salary: 20000, Expense: 5000, Energy: 9, Cognition: 6},
	}
	w.Players[0] = newPlayerFromCard(0, cards[0])
	w.Players[1] = newPlayerFromCard(1, cards[1])
	w.StartGame()
	w.Players[0].SideBusiness = &SideBusiness{Kind: "delivery", BaseIncome: 3250, OpenedMonth: 1}
	w.Players[0].Energy = 9
	w.Players[1].SideBusiness = &SideBusiness{Kind: "tutoring", BaseIncome: 3250, OpenedMonth: 1}
	w.Players[1].Energy = 9
	// seat1 也是 delivery → 同品类竞争(切两个品类之一)。
	w.Players[1].SideBusiness.Kind = "delivery"
	w.settlePlayer(w.Players[0], 30, sideMarketShares(w))
	w.settlePlayer(w.Players[1], 30, sideMarketShares(w))
	a, b := w.Players[0].Monthly.SideIncome, w.Players[1].Monthly.SideIncome
	// 双中份额各 0.5:文本括号必须显示份额 50%(Key=side 项)。
	ta := w.Players[0].Monthly.Detail
	sideText := ""
	for _, d := range ta {
		if d.Key == "side" {
			sideText = d.Text
		}
	}
	if sideText == "" {
		t.Fatal("side detail missing")
	}
	if !strings.Contains(sideText, "份额50%") {
		t.Errorf("share 50%% not in text: %q", sideText)
	}
	if a <= 0 || b <= 0 {
		t.Fatalf("both must earn: %d/%d", a, b)
	}
	// Ledger 科目/实体不变(EntityMarket → seat,CatSide)。
	found := 0
	for _, e := range w.Ledger.Entries {
		if e.Category == CatSide && e.From == EntityMarket {
			found++
		}
	}
	if found < 2 {
		t.Errorf("CatSide ledger entries: got %d, want ≥2", found)
	}
}

// TestSideMarket_CompetitionEvent 竞争播报:品类首次 2+ 经营者当月一条;
// 持续月不重复;消失后再现重新播报;独占品类不播。
func TestSideMarket_CompetitionEvent(t *testing.T) {
	w := sideWorld(t,
		struct {
			seat int
			kind string
			tier int
		}{0, "delivery", 0},
		struct {
			seat int
			kind string
			tier int
		}{2, "delivery", 0})
	w.Month = 1
	w.stepSideMarketEvents(sideMarketShares(w))
	if n := countEvent(w, "市场出现价格竞争"); n != 1 {
		t.Fatalf("first competition month: got %d events, want 1", n)
	}
	// 同月重复调用不叠加。
	w.stepSideMarketEvents(sideMarketShares(w))
	if n := countEvent(w, "市场出现价格竞争"); n != 1 {
		t.Fatalf("same month re-run: got %d, want 1", n)
	}
	// 竞争持续(次月)不重复播报。
	w.Month = 2
	w.stepSideMarketEvents(sideMarketShares(w))
	if n := countEvent(w, "市场出现价格竞争"); n != 1 {
		t.Fatalf("continuing competition: got %d, want 1", n)
	}
	// 竞争消失 → 再现:重新播报。
	w.Players[2].SideBusiness = nil
	w.Month = 3
	w.stepSideMarketEvents(sideMarketShares(w))
	w.Players[2].SideBusiness = &SideBusiness{Kind: "delivery", BaseIncome: 1000, OpenedMonth: 3}
	w.Month = 4
	w.stepSideMarketEvents(sideMarketShares(w))
	if n := countEvent(w, "市场出现价格竞争"); n != 2 {
		t.Fatalf("re-emerging competition: got %d, want 2", n)
	}
	// 文案含品类名与人数。
	if !strings.Contains(w.Events[len(w.Events)-1].Text, "跑腿配送市场出现价格竞争:2 名经营者") {
		t.Errorf("event text: %q", w.Events[len(w.Events)-1].Text)
	}
	// 独占品类不播。
	w2 := sideWorld(t, struct {
		seat int
		kind string
		tier int
	}{0, "content", 0})
	w2.stepSideMarketEvents(sideMarketShares(w2))
	if n := countEvent(w2, "价格竞争"); n != 0 {
		t.Errorf("monopoly must not announce: %d", n)
	}
}

func countEvent(w *World, substr string) int {
	n := 0
	for _, e := range w.Events {
		if strings.Contains(e.Text, substr) {
			n++
		}
	}
	return n
}

// TestSidePriceTextSuffix 结算文案括号:中价+独占空串(零偏移),否则带定价信息。
func TestSidePriceTextSuffix(t *testing.T) {
	if s := sidePriceTextSuffix(PriceTierMid, 1.0); s != "" {
		t.Errorf("mid monopoly suffix must be empty, got %q", s)
	}
	if s := sidePriceTextSuffix(PriceTierHigh, 0.625); s != "(高价×125%·份额63%)" {
		t.Errorf("high 62.5%% suffix: got %q", s)
	}
	if s := sidePriceTextSuffix(PriceTierLow, 0.5); s != "(低价×75%·份额50%)" {
		t.Errorf("low suffix: got %q", s)
	}
}
