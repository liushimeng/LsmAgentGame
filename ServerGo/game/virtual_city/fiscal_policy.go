// Package virtual_city — fiscal_policy.go: 政府财政支出(阶段4)
// 2026-09-21 §城市扩张v2.12 阶段4。
//
// 预算五分法:教育 30% / 医疗 25% / 养老 20% / 基建 15% / 失业救济 10%。
// 量入为出:月支出 = min(国库现金 × 5%, 上月财政收入 × 110%)(允许小幅赤字);
// 紧缩:现金 < 100 万时支出减半。
//
// 记账口径(treasury.go 头注释):教育/医疗/基建为国库账内核算(Cash 直减 +
// EduCapital/HealthCapital/InfraCapital 累计,不写 Ledger —— FirmScale=2.5
// 已含政府需求,再入 to=firms 会双重计入企业营收);养老/失业两类为财政直发,
// 走 w.Pay(from=EntityGovernment → seat, CatWelfare),I1 守恒。
package virtual_city

// 财政支出常量(阶段4 新定)。
const (
	// FiscalSpendCashRate 月支出 = 国库现金 × 5%(量入为出上限①)。
	FiscalSpendCashRate = 0.05
	// FiscalSpendRevenueMult 月支出 ≤ 上月财政收入 × 110%(允许 10% 小幅赤字)。
	FiscalSpendRevenueMult = 1.10
	// FiscalAusterityCash 紧缩线:现金 < 100 万 → 月支出减半。
	FiscalAusterityCash = 1_000_000
	// PublicServiceIdxUnit 公共服务指数:每累计 10,000 元投入 +1 点(上限 100)。
	PublicServiceIdxUnit = 10_000.0
	// PublicServiceIdxMax 公共服务指数上限。
	PublicServiceIdxMax = 100.0
)

// FiscalBudget 财政支出预算分配比例(总和 = 1.0;TestFiscalBudgetAllocation 锁定)。
var FiscalBudget = struct {
	Education           float64 // 教育 30%
	Healthcare          float64 // 医疗 25%
	Pension             float64 // 养老 20%
	Infrastructure      float64 // 基建 15%
	UnemploymentBenefit float64 // 失业救济 10%
}{0.30, 0.25, 0.20, 0.15, 0.10}

// FiscalCategory 支出类别枚举。
type FiscalCategory int

const (
	FiscalEdu           FiscalCategory = iota // 教育
	FiscalHealth                              // 医疗
	FiscalPension                             // 养老(财政直发)
	FiscalInfra                               // 基建
	FiscalUnemployment                        // 失业救济(财政直发)
	fiscalCategoryCount                       // 哨兵:类别总数(测试遍历用)
)

// String 中文标签(事件/备注用)。
func (c FiscalCategory) String() string {
	switch c {
	case FiscalEdu:
		return "教育"
	case FiscalHealth:
		return "医疗"
	case FiscalPension:
		return "养老"
	case FiscalInfra:
		return "基建"
	case FiscalUnemployment:
		return "失业救济"
	}
	return "未知"
}

// fiscalShare 类别 → FiscalBudget 份额。
func fiscalShare(c FiscalCategory) float64 {
	switch c {
	case FiscalEdu:
		return FiscalBudget.Education
	case FiscalHealth:
		return FiscalBudget.Healthcare
	case FiscalPension:
		return FiscalBudget.Pension
	case FiscalInfra:
		return FiscalBudget.Infrastructure
	case FiscalUnemployment:
		return FiscalBudget.UnemploymentBenefit
	}
	return 0
}

// monthFiscalBudget 当月财政支出总预算(尚未分配):
// min(Cash × 5%, LastMonthRevenue × 110%);紧缩(现金 < 100 万)减半;
// 再 clamp 到 Cash(不透支,R4-1 精神:国库不为负)。
func (t *TreasuryState) monthFiscalBudget() int64 {
	if t.Cash <= 0 {
		return 0
	}
	byCash := int64(float64(t.Cash) * FiscalSpendCashRate)
	byRevenue := int64(float64(t.LastMonthRevenue) * FiscalSpendRevenueMult)
	budget := byCash
	if byRevenue < budget {
		budget = byRevenue
	}
	if t.Cash < FiscalAusterityCash {
		budget /= 2 // 紧缩:支出减半
	}
	if budget > t.Cash {
		budget = t.Cash
	}
	if budget < 0 {
		budget = 0
	}
	return budget
}

// ExecuteFiscalSpending 执行财政支出(SettleTreasuryMonth ⑨C):
// 按 FiscalBudget 比例分配当月预算;教育/医疗/基建 → 公共服务累计投入
// (账内核算);养老/失业 → 财政直发(平分给符合条件座位,w.Pay CatWelfare)。
// 返回实际支出总额(无符合条件直发对象时对应份额留存国库)。
func (t *TreasuryState) ExecuteFiscalSpending(w *World) int64 {
	if t == nil || w == nil || t.Cash <= 0 {
		return 0
	}
	budget := t.monthFiscalBudget()
	if budget <= 0 {
		return 0
	}
	spent := int64(0)

	// ① 教育 / 医疗 / 基建:公共服务采购(账内核算,不写 Ledger)。
	public := []struct {
		cat     FiscalCategory
		capital *int64
	}{
		{FiscalEdu, &t.EduCapital},
		{FiscalHealth, &t.HealthCapital},
		{FiscalInfra, &t.InfraCapital},
	}
	for _, pc := range public {
		amt := int64(float64(budget)*fiscalShare(pc.cat) + 0.5)
		if amt <= 0 || amt > t.Cash {
			continue
		}
		*pc.capital += amt
		t.Cash -= amt
		spent += amt
	}

	// ② 养老 / 失业救济:财政直发(平分给符合条件座位)。
	spent += t.payFiscalDirect(w, budget, FiscalPension, func(p *Player, w *World) bool {
		return p.Alive && w.Age() >= 60
	})
	spent += t.payFiscalDirect(w, budget, FiscalUnemployment, func(p *Player, _ *World) bool {
		return p.Alive && p.UnemployedMonths > 0
	})
	return spent
}

// payFiscalDirect 单类财政直发:份额预算平分给 eligible 座位(整除,余数留存);
// 无符合条件座位 → 0 支出(份额自动留存国库)。
func (t *TreasuryState) payFiscalDirect(w *World, budget int64, cat FiscalCategory, eligible func(p *Player, w *World) bool) int64 {
	amt := int64(float64(budget)*fiscalShare(cat) + 0.5)
	if amt <= 0 {
		return 0
	}
	var seats []int
	for seat, p := range w.Players {
		if p == nil || !eligible(p, w) {
			continue
		}
		seats = append(seats, seat)
	}
	if len(seats) == 0 {
		return 0
	}
	each := amt / int64(len(seats))
	if each <= 0 {
		return 0
	}
	paid := int64(0)
	for _, seat := range seats {
		if each > t.Cash {
			break // 国库见底:按座位序截断,不透支
		}
		w.Pay(seat, EntityGovernment, SeatEntity(seat), each, CatWelfare,
			"财政直发·"+cat.String())
		t.Cash -= each
		paid += each
	}
	return paid
}

// PublicServiceIndex 公共服务指数(0–100):教育/医疗/基建累计投入
// 每 1 万元 +1 点(阶段4 的"提升 SocietyStats 对应指标"承载: SocietyStats
// 无教育/医疗/基建字段,以国库侧累计指数代理,v2.13 可上移至 SocietyStats)。
func (t *TreasuryState) PublicServiceIndex() float64 {
	if t == nil {
		return 0
	}
	total := float64(t.EduCapital + t.HealthCapital + t.InfraCapital)
	idx := total / PublicServiceIdxUnit
	if idx > PublicServiceIdxMax {
		idx = PublicServiceIdxMax
	}
	if idx < 0 {
		idx = 0
	}
	return idx
}
