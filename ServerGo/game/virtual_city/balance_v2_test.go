// Package virtual_city — balance_v2_test.go: 批次74 P0 经济平衡 v2 第一步
// 「只救命」单测(设计文档 §5.1 T1–T6 + §5.2 AB-1)。
//
// 全部断言只读引擎既有产出(账本 / Monthly / ConsumptionLevel / hazard 纯函数),
// 不新增 rand 消费点、不改任何账目语义。
package virtual_city

import (
	"math"
	"math/rand"
	"testing"

	"LsmAgentGame/game/virtual_city/profession"
)

// balanceV2Cards 确定性 12 座合成卡(与 MC harness 同源卡源,卡源 rng 与世界
// rng 独立)。返回值是 **StartGame 之前**的卡面(初始 Savings 未被写回)。
func balanceV2Cards(seed int64, seats int) [MaxSeats]profession.Card {
	var cards [MaxSeats]profession.Card
	for i, c := range profession.SyntheticCards(seats, rand.New(rand.NewSource(seed))) {
		if i >= MaxSeats {
			break
		}
		cards[i] = c
	}
	return cards
}

// balanceV2World 12 座(或 seats 座)合成卡世界(未 StartGame)。
func balanceV2World(seed int64, seats int) (*World, [MaxSeats]profession.Card) {
	cards := balanceV2Cards(seed, seats)
	return NewWorld(seed, cards), cards
}

// billCard 内联夹具卡(金额整千 ⇒ ceil_1000 不改变夹具值)。
func billCard(salary, expense, savings int64, district string) profession.Card {
	return profession.Card{
		ID: "T74", Title: "批次74夹具", Salary: salary, Expense: expense,
		Savings: savings, StartAge: 30, Energy: 8, Network: 5, Cognition: 5,
		CreditScore: 650, HomeDistrict: district, RiskPreference: "balanced",
		HealthGrade: "B", Marital: "single",
		OpeningHook: "这是一张批次74经济平衡测试用的内联夹具卡,用于固定数值断言。",
	}
}

// injectEntriesOf 统计世界→座位初始注入条目。
func injectEntriesOf(w *World, seat int) []Entry {
	var out []Entry
	for _, e := range w.Ledger.Entries {
		if e.From == EntityWorld && e.To == SeatEntity(seat) && e.Category == CatInject {
			out = append(out, e)
		}
	}
	return out
}

// familySupportAddBack 本月「赡养父母」刚性支出(批次52 代际通道,本批非目标
// §6.2)。T1 断言把它加回,使口径与 P0 v2 §3.2/§3.3 的静态推算一致。
func familySupportAddBack(p *Player) int64 {
	total := int64(0)
	for _, d := range p.Monthly.Detail {
		if d.Key == "family_support" && d.AmountCNY < 0 {
			total += -d.AmountCNY
		}
	}
	return total
}

// ── T1 生效初始现金 + 非失业座 M1 净现金流 ────────────────────────────

// TestBalanceV2_T1_StartCashAndM1NonNegative
//   - 12 座生效初始现金 = ceil_1000(max(savings, 6 × SurvivalCore)),只抬不削;
//   - 每座恰 1 条 world→seat 注入,金额 = 生效值(I2);
//   - 非失业座 M1 净现金流 ≥ 0(加回批次52 家庭赡养通道,见 familySupportAddBack)。
func TestBalanceV2_T1_StartCashAndM1NonNegative(t *testing.T) {
	seed := int64(20261022)
	w, cards := balanceV2World(seed, MaxSeats)
	// StartGame 前按公式复算期望值(市场价此刻 = M1 静态价)。
	want := [MaxSeats]int64{}
	for s := 0; s < MaxSeats; s++ {
		probe := newPlayerFromCard(s, cards[s])
		floor := int64(w.startCashMonths()) * w.survivalCoreM1(probe)
		base := cards[s].Savings
		if floor > base {
			base = floor
		}
		want[s] = ceilTo1000(base)
	}
	w.StartGame()

	nonVolitile := 0
	for s := 0; s < MaxSeats; s++ {
		p := w.Players[s]
		if !p.SalaryVolatile {
			nonVolitile++
		}
		if p.Cash != want[s] || p.Card.Savings != want[s] {
			t.Errorf("座 %d(%s)生效初始现金 = cash %d / card.Savings %d,期望 %d",
				s, p.Card.Title, p.Cash, p.Card.Savings, want[s])
		}
		entries := injectEntriesOf(w, s)
		if len(entries) != 1 {
			t.Fatalf("座 %d world→seat 注入条目 = %d,期望 1 条", s, len(entries))
		}
		if entries[0].AmountCNY != want[s] {
			t.Errorf("座 %d 注入金额 = %d,期望 %d(生效初始现金)", s, entries[0].AmountCNY, want[s])
		}
	}
	if nonVolitile != MaxSeats {
		t.Logf("注:12 座中非波动卡 %d 张(合成卡 SalaryVolatile 恒 false)", nonVolitile)
	}

	// M1 月结。
	w.SettleMonth()
	neg, employed := 0, 0
	for s := 0; s < MaxSeats; s++ {
		p := w.Players[s]
		if p.Monthly.Income <= 0 {
			continue // 当月被裁/无收入座不在本断言内(失业兜底由 T3/T5 覆盖)
		}
		employed++
		net := p.Monthly.Net + familySupportAddBack(p)
		if net < 0 {
			neg++
			t.Errorf("座 %d(%s)M1 净现金流(剔除家庭赡养)= %d < 0:income %d expense %d",
				s, p.Card.Title, net, p.Monthly.Income, p.Monthly.Expense)
		}
	}
	if employed < MaxSeats-1 {
		t.Errorf("M1 有收入座 = %d,期望 ≥ %d(样本不足)", employed, MaxSeats-1)
	}
	t.Logf("T1:M1 有收入座 %d/12,负净现金流(剔除家庭赡养) %d 座", employed, neg)
}

// ── T2 基础住房租金 ──────────────────────────────────────────────────

// TestBalanceV2_T2_BasicRent 基础租 = min(市场租, clamp(25%×家庭常规收入,
// 1800, 12000));投资房租金收入/自住房免租不受影响。
func TestBalanceV2_T2_BasicRent(t *testing.T) {
	w := NewWorld(74, [MaxSeats]profession.Card{})
	// commerce:市场租 7,040 → 25%×5,000 = 1,250 → 下限 1,800。
	low := newPlayerFromCard(0, billCard(5000, 3000, 24000, "commerce"))
	// oldtown:市场租 2,304 < clamp(3,000) ⇒ 实付 = 市场租(不抬高)。
	mid := newPlayerFromCard(1, billCard(12000, 6000, 44000, "oldtown"))
	// finance:clamp 上限 12,000 < 市场租 16,640 ⇒ 实付 12,000。
	high := newPlayerFromCard(2, billCard(60000, 30000, 190000, "finance"))
	w.Players[0], w.Players[1], w.Players[2] = low, mid, high

	if got := w.Market.HouseRent("commerce"); got != 7040 {
		t.Fatalf("commerce 市场租 = %d,期望 7040(M1 静态价)", got)
	}
	if got := w.basicRent(low); got != 1800 {
		t.Errorf("commerce 基础租 = %d,期望 1800(7,040 → 下限)", got)
	}
	if got := w.basicRent(mid); got != 2304 {
		t.Errorf("oldtown 基础租 = %d,期望 2304(= 市场租,未越 25%% 上限)", got)
	}
	if got := w.basicRent(high); got != 12000 {
		t.Errorf("finance 基础租 = %d,期望 12000(封顶)", got)
	}

	// 投资房租金收入仍按市场租:给 commerce 座位一套非自住住宅。
	low.Assets = []Asset{{
		Kind: AssetKindHouse("commerce"), Units: 1, OpenMonth: 1,
		Extra: map[string]any{"district": "commerce", "self_occupied": false},
	}}
	// 自住房座位(central_park):免租、仅物业费。
	selfCard := billCard(15000, 8000, 83000, "central_park")
	self := newPlayerFromCard(3, selfCard)
	self.Assets = []Asset{{
		Kind: AssetKindHouse("central_park"), Units: 1, OpenMonth: 1,
		Extra: map[string]any{"district": "central_park", "self_occupied": true},
	}}
	w.Players[3] = self
	w.StartGame()
	w.SettleMonth()

	// 座0:房租支出 = 基础租 1,800;租金收入 = 市场租 7,040(不收上限约束)。
	var rentPaid, rentIncome int64
	for _, d := range w.Players[0].Monthly.Detail {
		switch d.Key {
		case "rent_pay":
			rentPaid = -d.AmountCNY
		case "rent":
			rentIncome += d.AmountCNY
		}
	}
	if rentPaid != 1800 {
		t.Errorf("座0 实付房租 = %d,期望 1800(基础租)", rentPaid)
	}
	if rentIncome != 7040 {
		t.Errorf("座0 投资房租金收入 = %d,期望 7040(市场租,不受基础租上限影响)", rentIncome)
	}
	// 座3 自住:无 rent_pay;有物业费。
	var selfRent, selfProperty int64
	for _, d := range w.Players[3].Monthly.Detail {
		switch d.Key {
		case "rent_pay":
			selfRent = -d.AmountCNY
		case "property":
			selfProperty = -d.AmountCNY
		}
	}
	if selfRent != 0 {
		t.Errorf("自住房仍应免租,实付 %d", selfRent)
	}
	if selfProperty != propertyFeeCNY {
		t.Errorf("自住房物业费 = %d,期望 %d", selfProperty, propertyFeeCNY)
	}
}

// ── T3 失业救济金 v2 ─────────────────────────────────────────────────

// TestBalanceV2_T3_UnemploymentBenefit 救济金 = clamp(0.5 × SalaryBase,
// 1800, 8000),无等待期;Ledger gov→seat + CatWelfare(不新增类目)。
func TestBalanceV2_T3_UnemploymentBenefit(t *testing.T) {
	cases := []struct {
		salary, want int64
		note         string
	}{
		{10000, 5000, "中位:50% 替代率"},
		{2000, 1800, "低薪:触下限"},
		{30000, 8000, "高薪:触上限"},
	}
	w := NewWorld(74, [MaxSeats]profession.Card{})
	for i, c := range cases {
		p := newPlayerFromCard(i, billCard(c.salary, c.salary/2, 30000, "residential"))
		w.Players[i] = p
	}
	w.StartGame() // 清账本基线(注入在 Record 0 月)
	for i, c := range cases {
		p := w.Players[i]
		p.UnemployedMonths = 6
		p.UnemployedAccumMonths = 0 // 第 1 个失业月:旧口径等待期 = 0,v2 即发
		before := w.Treasury.Cash
		got := w.Treasury.payUnemployment(w, p)
		if got != c.want {
			t.Errorf("%s:救济金 = %d,期望 %d", c.note, got, c.want)
		}
		if w.Treasury.Cash != before-got {
			t.Errorf("%s:国库扣减 = %d,期望 %d", c.note, before-w.Treasury.Cash, got)
		}
		found := false
		for _, e := range w.Ledger.Entries {
			if e.From == EntityGovernment && e.To == SeatEntity(i) &&
				e.Category == CatWelfare && e.AmountCNY == c.want {
				found = true
			}
		}
		if !found {
			t.Errorf("%s:缺 gov→seat CatWelfare 账目 %d", c.note, c.want)
		}
		if p.UnemployedAccumMonths != 1 {
			t.Errorf("%s:累计失业月数 = %d,期望 1(低保门槛依赖)", c.note, p.UnemployedAccumMonths)
		}
	}

	// 无衰减:第 13/14 个失业月仍发全额(旧口径 0.7 衰减已取消)。
	p := w.Players[0]
	p.UnemployedMonths = 6
	p.UnemployedAccumMonths = 12
	if got := w.Treasury.payUnemployment(w, p); got != 5000 {
		t.Errorf("第 13 个失业月救济金 = %d,期望 5000(v2 无衰减)", got)
	}

	// 国库不足 → 不发(不透支)。
	w2 := NewWorld(74, [MaxSeats]profession.Card{})
	p2 := newPlayerFromCard(0, billCard(10000, 5000, 30000, "residential"))
	w2.Players[0] = p2
	w2.Treasury.Cash = 100
	p2.UnemployedMonths = 3
	if got := w2.Treasury.payUnemployment(w2, p2); got != 0 || w2.Treasury.Cash != 100 {
		t.Errorf("国库不足应不发:got %d cash %d", got, w2.Treasury.Cash)
	}
}

// ── T4 失业 hazard v2 ────────────────────────────────────────────────

// TestBalanceV2_T4_UnemploymentHazard hazard 表 0.8% / 3% / 5%,乘 Labor 倍率
// 与认知守卫后封顶 8%;同种子逐字段复现。
func TestBalanceV2_T4_UnemploymentHazard(t *testing.T) {
	w := NewWorld(74, [MaxSeats]profession.Card{})
	p := newPlayerFromCard(0, billCard(10000, 5000, 30000, "residential"))
	p.Cognition = 0 // guard = 1.0
	w.Players[0] = p
	w.EconomyEnabled = false // Labor 倍率恒 1(隔离相位数)

	w.Market.CyclePhase = PhaseRecovery
	if got := w.unemploymentHazardBase(); got != 0.008 {
		t.Errorf("recovery hazard = %v,期望 0.008", got)
	}
	w.Market.CyclePhase = PhaseBoom
	if got := w.unemploymentHazardBase(); got != 0.008 {
		t.Errorf("boom hazard = %v,期望 0.008", got)
	}
	w.Market.CyclePhase = PhaseRecession
	if got := w.unemploymentHazardBase(); got != 0.03 {
		t.Errorf("recession hazard = %v,期望 0.03", got)
	}
	w.Market.CyclePhase = PhaseDepression
	if got := w.unemploymentHazardBase(); got != 0.05 {
		t.Errorf("depression hazard = %v,期望 0.05", got)
	}

	// 认知守卫:depression 0.05 × (1 − 0.05×4) = 0.04。
	p.Cognition = 4
	if got := w.unemploymentProbability(p); math.Abs(got-0.04) > 1e-12 {
		t.Errorf("depression + 认知4 概率 = %v,期望 0.04(×0.8 守卫)", got)
	}

	// 封顶 8%:depression 0.05 × Labor 2.5 × guard 1.0 = 0.125 → 0.08。
	p.Cognition = 0
	w.EconomyEnabled = true
	w.Labor.Unemployment = UnemployNatural * 2.5
	if got := w.unemploymentProbability(p); math.Abs(got-UnemploymentHazardCapV2) > 1e-12 {
		t.Errorf("高失业 + 萧条概率 = %v,期望封顶 0.08", got)
	}
	// 旧口径不封顶(0.25 × 2.5 = 0.625)。
	w.BalanceV2Enabled = false
	if got := w.unemploymentProbability(p); math.Abs(got-0.625) > 1e-12 {
		t.Errorf("旧口径概率 = %v,期望 0.625(不封顶)", got)
	}
	w.BalanceV2Enabled = true

	// 同种子复现:两世界 12 月逐字段一致(含失业状态/现金/账本摘要)。
	a, _ := balanceV2World(4242, 6)
	a.StartGame()
	b, _ := balanceV2World(4242, 6)
	b.StartGame()
	for m := 1; m <= 12; m++ {
		a.SettleMonth()
		b.SettleMonth()
		for s := 0; s < 6; s++ {
			pa, pb := a.Players[s], b.Players[s]
			if pa.Cash != pb.Cash || pa.UnemployedMonths != pb.UnemployedMonths ||
				pa.SalaryBase != pb.SalaryBase || pa.ConsumptionLevel != pb.ConsumptionLevel {
				t.Fatalf("同种子 M%d 座 %d 复现失败:cash %d/%d unemp %d/%d sal %d/%d lvl %d/%d",
					m, s, pa.Cash, pb.Cash, pa.UnemployedMonths, pb.UnemployedMonths,
					pa.SalaryBase, pb.SalaryBase, pa.ConsumptionLevel, pb.ConsumptionLevel)
			}
		}
		if a.Ledger.Digest != b.Ledger.Digest {
			t.Fatalf("同种子 M%d 账本摘要不一致", m)
		}
	}
}

// ── T5 新手节俭档 ────────────────────────────────────────────────────

// TestBalanceV2_T5_FrugalConsumption M1–M6 强制 0 档;条件解除且引擎强制 →
// 恢复 1;显式选择不被改写(非强制态下保留 2/3 档与显式 0 档)。
func TestBalanceV2_T5_FrugalConsumption(t *testing.T) {
	w := NewWorld(74, [MaxSeats]profession.Card{})
	p := newPlayerFromCard(0, billCard(20000, 8000, 200000, "residential"))
	p.ConsumptionByGoods = map[string]float64{"food": 1} // 已初始化 ⇒ 档位字段生效
	p.ConsumptionLevel = 1
	w.Players[0] = p
	w.StartGame()
	if p.ConsumptionLevelForced {
		t.Fatal("开局不应带 Forced 标记")
	}

	// M1:新手保护期 ⇒ 强制 0 档 + Forced 标记;生活支出 = 卡面 × infl × 0.6。
	w.SettleMonth()
	if p.ConsumptionLevel != 0 || !p.ConsumptionLevelForced {
		t.Fatalf("M1 强制节俭:level=%d forced=%v,期望 0/true", p.ConsumptionLevel, p.ConsumptionLevelForced)
	}
	infl := InflationFactorCB(w)
	wantLiving := int64(float64(p.Card.Expense)*infl*consumptionLevelMult[0] + 0.5)
	var living int64
	for _, d := range p.Monthly.Detail {
		if d.Key == "living" {
			living = -d.AmountCNY
		}
	}
	if living != wantLiving {
		t.Errorf("M1 生活支出 = %d,期望 %d(×0.6)", living, wantLiving)
	}

	// 条件解除(月数 > FrugalMonths 且储备充足)⇒ 由引擎强制过 ⇒ 恢复 1。
	w.Month = w.frugalMonths() + 1
	w.SettleMonth()
	if p.ConsumptionLevel != 1 || p.ConsumptionLevelForced {
		t.Fatalf("条件解除后:level=%d forced=%v,期望 1/false", p.ConsumptionLevel, p.ConsumptionLevelForced)
	}

	// 显式选择不被改写:非强制态显式 3 档 ⇒ 月结后仍为 3 档。
	p.ConsumptionLevel = 3
	w.SettleMonth()
	if p.ConsumptionLevel != 3 || p.ConsumptionLevelForced {
		t.Errorf("显式 3 档被改写:level=%d forced=%v", p.ConsumptionLevel, p.ConsumptionLevelForced)
	}

	// 非强制态显式 0 档:不得被「自动恢复 1」覆盖。
	p.ConsumptionLevel = 0
	w.SettleMonth()
	if p.ConsumptionLevel != 0 || p.ConsumptionLevelForced {
		t.Errorf("显式 0 档被改写:level=%d forced=%v", p.ConsumptionLevel, p.ConsumptionLevelForced)
	}

	// 储备不足 ⇒ 再次引擎强制(Forced=true);显式 0 档期间不重复标记。
	p.ConsumptionLevel = 2
	p.Cash = 0
	p.Assets = nil
	w.SettleMonth()
	if p.ConsumptionLevel != 0 || !p.ConsumptionLevelForced {
		t.Errorf("储备不足应强制 0 档:level=%d forced=%v", p.ConsumptionLevel, p.ConsumptionLevelForced)
	}

	// 显式选择优先:强制期内玩家显式改档 ⇒ 清 Forced 标记,条件解除后不自动
	// 恢复 1 档(玩家选 0 就一直 0)。
	w3 := NewWorld(76, [MaxSeats]profession.Card{})
	p3 := newPlayerFromCard(0, billCard(20000, 8000, 200000, "residential"))
	p3.ConsumptionByGoods = map[string]float64{"food": 1}
	p3.ConsumptionLevel = 1
	w3.Players[0] = p3
	w3.StartGame()
	w3.SettleMonth() // M1 强制 → Forced=true
	if !p3.ConsumptionLevelForced {
		t.Fatalf("夹具应先被引擎强制:forced=%v", p3.ConsumptionLevelForced)
	}
	p3.ActionBudget = 3
	if _, e := w3.ApplyAction(0, Action{Type: ActSetConsumption, Level: 0}); e != nil {
		t.Fatalf("set_consumption: %v", e)
	}
	if p3.ConsumptionLevelForced {
		t.Error("显式选择后 Forced 标记应清除")
	}
	w3.Month = w3.frugalMonths() + 1
	w3.SettleMonth()
	if p3.ConsumptionLevel != 0 {
		t.Errorf("显式 0 档在条件解除后被改写:level=%d", p3.ConsumptionLevel)
	}

	// 失业期 ⇒ 强制(月数已过新手期,靠失业条件单独触发)。
	w2 := NewWorld(75, [MaxSeats]profession.Card{})
	p2 := newPlayerFromCard(0, billCard(20000, 8000, 200000, "residential"))
	p2.ConsumptionByGoods = map[string]float64{"food": 1}
	p2.ConsumptionLevel = 1
	w2.Players[0] = p2
	w2.StartGame()
	w2.Month = w2.frugalMonths() + 1
	p2.UnemployedMonths = 3
	w2.SettleMonth()
	if p2.ConsumptionLevel != 0 || !p2.ConsumptionLevelForced {
		t.Errorf("失业期应强制节俭档:level=%d forced=%v", p2.ConsumptionLevel, p2.ConsumptionLevelForced)
	}
}

// ── T6 开关关闭 = 批次73 旧口径 ──────────────────────────────────────

// TestBalanceV2_T6_LegacyRegression BalanceV2Enabled=false 时初始现金 / 基础租 /
// 节俭档 / 失业救济 / hazard 全部走旧口径(逐字段与批次73 一致)。
func TestBalanceV2_T6_LegacyRegression(t *testing.T) {
	w := NewWorld(73, [MaxSeats]profession.Card{})
	w.BalanceV2Enabled = false
	low := newPlayerFromCard(0, billCard(5000, 3000, 1000, "commerce"))
	w.Players[0] = low
	w.StartGame()
	if low.Cash != 1000 || low.Card.Savings != 1000 {
		t.Errorf("旧口径初始现金 = %d/%d,期望 1000(卡面 Savings,不抬升)",
			low.Cash, low.Card.Savings)
	}
	if entries := injectEntriesOf(w, 0); len(entries) != 1 || entries[0].AmountCNY != 1000 {
		t.Errorf("旧口径注入条目 = %+v,期望 1 条 1000", entries)
	}

	// 房租 = 全额市场租(不做 25% 上限)。月结后会漂移 ⇒ 先取当月市场租。
	market := w.Market.HouseRent("commerce")
	low.ConsumptionByGoods = map[string]float64{"food": 1}
	low.ConsumptionLevel = 1
	w.SettleMonth()
	var rentPaid int64
	for _, d := range low.Monthly.Detail {
		if d.Key == "rent_pay" {
			rentPaid = -d.AmountCNY
		}
	}
	if rentPaid != market {
		t.Errorf("旧口径实付房租 = %d,期望市场租 %d", rentPaid, market)
	}
	// 旧口径粘滞降档:现金 < 2×基线 ⇒ 0 档。
	if low.ConsumptionLevel != 0 {
		t.Errorf("旧口径应有粘滞强制降档,level=%d", low.ConsumptionLevel)
	}

	// 失业救济旧公式:前 3 个月等待期不发。
	p := w.Players[0]
	p.UnemployedMonths = 6
	p.UnemployedAccumMonths = 0
	if got := w.Treasury.payUnemployment(w, p); got != 0 {
		t.Errorf("旧口径等待期救济 = %d,期望 0", got)
	}

	// hazard 旧表。
	w.Market.CyclePhase = PhaseRecovery
	if got := w.unemploymentHazardBase(); got != 0.05 {
		t.Errorf("旧口径 recovery hazard = %v,期望 0.05", got)
	}
	w.Market.CyclePhase = PhaseRecession
	if got := w.unemploymentHazardBase(); got != 0.15 {
		t.Errorf("旧口径 recession hazard = %v,期望 0.15", got)
	}
	w.Market.CyclePhase = PhaseDepression
	if got := w.unemploymentHazardBase(); got != 0.25 {
		t.Errorf("旧口径 depression hazard = %v,期望 0.25", got)
	}
	// 旧口径 probability 不封顶。
	p.Cognition = 0
	w.EconomyEnabled = false
	if got := w.unemploymentProbability(p); math.Abs(got-0.25) > 1e-12 {
		t.Errorf("旧口径 depression 概率 = %v,期望 0.25", got)
	}
}

// ── §5.2 AB-1 旧 vs 新成对对照 ───────────────────────────────────────

// TestBalanceV2_AB AB-1:base = BalanceV2=false,variant = 默认;同种子配对
// n=20,覆盖 balanced 与 aggressive_leverage。断言新口径 D5(终局正式出局率)
// 不高于旧口径(允许持平;χ² 显著性见日志)。
//
// 两个策略的 MCCompare 并行跑(各 40 局),墙钟 ≈ 单策略耗时。
func TestBalanceV2_AB(t *testing.T) {
	if testing.Short() {
		t.Skip("批次74 AB 对照(80 局)在 -short 下跳过")
	}
	off := false
	// 落盘旁路(居民级 JSONL)改到临时目录,避免污染仓库工作目录;测试结束复原。
	prevStore := defaultResidentStore
	defaultResidentStore = &ResidentStore{dir: t.TempDir()}
	defer func() { defaultResidentStore = prevStore }()
	base := MCOverride{BalanceV2: &off}
	variant := MCOverride{} // 默认 = v2 开启
	strategies := []string{MCStrategyBalanced, MCStrategyAggressiveLeverage}
	reports := make([]ABReport, len(strategies))
	var wg = make(chan int, len(strategies))
	for i, st := range strategies {
		go func(i int, st string) {
			reports[i] = MCCompare(st, base, variant, 20)
			wg <- i
		}(i, st)
	}
	for range strategies {
		<-wg
	}
	for i, rep := range reports {
		t.Logf("AB-1 %s:n=%d base_d5=%.4f variant_d5=%.4f delta_d5=%+.4f chi2=%.4f chi2_p=%.4f verdict=%s | base_d6=%.4f variant_d6=%.4f t_p=%.4f guard_breach=%v underpowered=%v",
			rep.Strategy, rep.N, rep.BaseD5, rep.VariantD5, rep.DeltaD5,
			rep.Chi2, rep.Chi2P, rep.Verdict, rep.BaseD6, rep.VariantD6, rep.TP,
			rep.GuardBreach, rep.Underpowered)
		if rep.N != 20 {
			t.Errorf("%s:n = %d,期望 20", strategies[i], rep.N)
		}
		if rep.VariantD5 > rep.BaseD5+1e-9 {
			t.Errorf("%s:新口径 D5 = %.4f 高于旧口径 %.4f(不应更差)",
				strategies[i], rep.VariantD5, rep.BaseD5)
		}
	}
}
