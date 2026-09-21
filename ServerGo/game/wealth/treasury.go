// Package wealth — treasury.go: 政府财政子系统·国库核心(阶段4)
// 2026-09-21 §城市扩张v2.12 阶段4。
//
// 职责:月度税收汇总(R4-2 聚合一次性清算)/ 转移支付派发 / 财政支出 /
// 国债簿记,以及月度财政快照(History 环形保留 24 条)。
// 纯引擎层:无锁、无 goroutine、无 IO、**零 rand 消费** —— 固定种子存量
// 对局回归零偏移(§197 反例教训:引擎新增路径不得动 w.Rand)。
//
// R4-1(禁止央行透支):本文件及配套(fiscal_policy / transfer_payment /
// sovereign_bond)从不读写 CentralBankState;国债融资只记录 BondsOutstanding,
// 国库与央行/银行/市场实体之间不存在任何 Ledger 通道(见 ledger.go 白名单)。
//
// 记账口径(阶段4 设计取舍,防双重计数):
//   - 个税/社保:座位在月结步骤4 已付至 EntityGov;本子系统只**汇总**入
//     TaxCounter,不重复扣座位现金(避免二次征税破坏既有 I1 语义)。
//   - 印花/增值税/企业所得税/消费税/交易税:宏口径课税(对经济总量计税),
//     只入国库现金,不逐笔回溯扣款(R4-2:聚合清算,避免逐笔耗时)。
//   - 财政采购(教育/医疗/基建)与国债还本付息:国库账内核算(Cash 直减 +
//     History 记录),**不写 Ledger** —— FirmScale=2.5 已含"政府+投资+外需"
//     倍数,再记 to=firms 会双重计入企业营收,污染 Labor.RevenueCNY。
//   - 转移支付/财政直发(触及座位现金):唯一走 w.Pay 的政府支出通道
//     (from=EntityGovernment → seat,category=CatWelfare),I1 守恒由 Pay 保证。
package wealth

import (
	"fmt"
)

// 国库常量(阶段4 新定)。
const (
	// TreasuryInitialCash 初始国库现金 500 万元(虚拟城市政府启动资金)。
	TreasuryInitialCash = 5_000_000
	// TreasuryHistoryLen History 环形缓冲长度(最近 24 个月)。
	TreasuryHistoryLen = 24
	// TreasuryMinMonthlyExpense 月支出下限(发行国债触发阈值用):
	// 首月 LastMonthExpense=0 时不允许"阈值=0 → 永不发行"的退化。
	TreasuryMinMonthlyExpense = 20_000
)

// 宏口径税率(阶段4 新定;对经济总量课税,不逐笔扣座位)。
const (
	VatRate            = 0.06  // 增值税 = 企业部门营收 × 6%(小规模纳税人简化档)
	CorporateTaxRate   = 0.15  // 企业所得税 = 应税利润(营收−工资)× 15%(高新简化档)
	StampTaxRate       = 0.001 // 印花税 = 当月资产买入流水 × 0.1%(股票/房产交易)
	ConsumptionTaxRate = 0.10  // 消费税 = 奢侈档(3)玩家当月生活支出 × 10%
	TradeTaxRate       = 0.001 // 交易税 = 玩家间当月成交额 × 0.1%(CatTrade)
)

// TreasuryState 政府财政状态(月度;挂 World.Treasury,纯引擎无锁)。
type TreasuryState struct {
	Cash             int64 // 国库现金(元)
	BondsOutstanding int64 // 国债余额(元;sovereign_bond.go 簿记)
	LastMonthRevenue int64 // 上月财政收入
	LastMonthExpense int64 // 上月财政支出(财政采购+转移支付+国债还本付息)
	DeficitRun       int   // 连续赤字月数(Deficit<0 累计,盈余清零)

	// MonthlyCounter 当月税收聚合器(R4-2:聚合一次性清算,避免逐笔计算)。
	// CollectMonthTax 每次调用先清零再汇总当月口径。
	MonthlyCounter *TaxCounter

	// Bonds 存量国债明细(sovereign_bond.go 维护;付息/兑付遍历用)。
	Bonds []*SovereignBond

	// 公共服务累计投入(fiscal_policy.go 维护;PublicServiceIndex 分母)。
	EduCapital    int64 // 教育累计投入
	HealthCapital int64 // 医疗累计投入
	InfraCapital  int64 // 基建累计投入

	// History 最近 TreasuryHistoryLen 个月财政记录(环形缓冲:append 后裁尾)。
	History []TreasuryMonthRecord
}

// TaxCounter 月度税收聚合器(R4-2:聚合一次性清算,避免逐笔耗时)。
type TaxCounter struct {
	IncomeTaxTotal    int64 // 个税(汇总座位已缴 MonthlyIncomeTax)
	SocialSecurityTot int64 // 社保(汇总座位已缴 SocialSecurity)
	StampTaxTotal     int64 // 印花税(股票/房产交易流水 × StampTaxRate)
	VatTotal          int64 // 增值税(企业营收 × VatRate)
	CorporateTaxTotal int64 // 企业所得税(企业利润 × CorporateTaxRate)
	ConsumptionTaxTot int64 // 消费税(奢侈档生活支出 × ConsumptionTaxRate)
	TradeTaxTotal     int64 // 交易税(玩家间成交 × TradeTaxRate)
}

// Total 七税合计。
func (c *TaxCounter) Total() int64 {
	if c == nil {
		return 0
	}
	return c.IncomeTaxTotal + c.SocialSecurityTot + c.StampTaxTotal +
		c.VatTotal + c.CorporateTaxTotal + c.ConsumptionTaxTot + c.TradeTaxTotal
}

// TreasuryMonthRecord 单月财政快照。
type TreasuryMonthRecord struct {
	Month         int
	Revenue       int64
	Expense       int64
	Deficit       int64 // Revenue − Expense(负 = 赤字)
	TransferPaid  int64 // 转移支付总额
	BondService   int64 // 国债还本付息(账内核算部分)
	Beneficiaries int   // 受益人数(当月收到任意转移支付的座位数)
}

// NewTreasury 构造国库初始状态:现金 500 万,无国债,无历史。
func NewTreasury() *TreasuryState {
	return &TreasuryState{
		Cash:           TreasuryInitialCash,
		MonthlyCounter: &TaxCounter{},
	}
}

// CollectMonthTax 月末税收汇总(R4-2):清零聚合器 → 汇总七税 → Cash += 总额。
// 返回当月总税收。个税/社保取座位月结快照(p.Monthly,步骤4 已缴至 gov);
// 印花/交易税扫描当月 Ledger;增值税/企业所得税取 Labor 部门上月口径;
// 消费税取奢侈档(ConsumptionLevel==3)座位当月生活支出。不扣任何座位现金。
func (t *TreasuryState) CollectMonthTax(w *World) int64 {
	if t == nil || w == nil {
		return 0
	}
	if t.MonthlyCounter == nil {
		t.MonthlyCounter = &TaxCounter{}
	}
	c := t.MonthlyCounter
	*c = TaxCounter{} // 月初清零(R4-2:一次清算,不逐笔累加)

	// ① 个税 + 社保 + 消费税:遍历存活座位。
	for _, p := range w.Players {
		if p == nil || !p.Alive {
			continue
		}
		c.IncomeTaxTotal += p.Monthly.Tax
		c.SocialSecurityTot += p.Monthly.Social
		if p.ConsumptionLevelSafe() == 3 {
			c.ConsumptionTaxTot += int64(float64(livingExpenseOf(p))*ConsumptionTaxRate + 0.5)
		}
	}

	// ② 印花税 + 交易税:扫描当月 Ledger(seat→market 买入 / seat↔seat 成交)。
	for _, e := range w.Ledger.MonthEntries(w.Month) {
		if _, ok := IsSeatEntity(e.From); !ok {
			continue
		}
		switch e.Category {
		case CatBuy:
			c.StampTaxTotal += int64(float64(e.AmountCNY)*StampTaxRate + 0.5)
		case CatTrade:
			c.TradeTaxTotal += int64(float64(e.AmountCNY)*TradeTaxRate + 0.5)
		}
	}

	// ③ 增值税 + 企业所得税:企业部门口径(Labor 为 nil 时跳过)。
	if w.Labor != nil {
		c.VatTotal = int64(float64(w.Labor.RevenueCNY)*VatRate + 0.5)
		profit := w.Labor.RevenueCNY - w.Labor.WageBillCNY
		if profit > 0 {
			c.CorporateTaxTotal = int64(float64(profit)*CorporateTaxRate + 0.5)
		}
	}

	total := c.Total()
	t.Cash += total
	return total
}

// livingExpenseOf 从月结明细取当月"生活支出"(消费税税基;无明细 → 0)。
func livingExpenseOf(p *Player) int64 {
	var sum int64
	for _, it := range p.Monthly.Detail {
		if it.Key == "living" && it.AmountCNY < 0 {
			sum += -it.AmountCNY
		}
	}
	return sum
}

// SettleTreasuryMonth 国库月度总结算(SettleMonth ⑨ 钩子,阶段4 新增):
//
//	⑨A CollectMonthTax     税收汇总 → Cash
//	⑨B PayTransferPayments 转移支付(4 类,见 transfer_payment.go)
//	⑨C ExecuteFiscalSpending 财政支出(5 类预算,见 fiscal_policy.go)
//	⑨D MonthlyBondStep     国债付息 → 到期兑付 → 必要发行(sovereign_bond.go)
//	⑨E 记录 History + DeficitRun 更新(连续赤字累计,盈余清零)
//
// 调用方(SettleMonth)持房间锁;economy_enabled=false 时由调用方整体跳过。
func (t *TreasuryState) SettleTreasuryMonth(w *World) {
	if t == nil || w == nil {
		return
	}
	// ⑨A 政府税收。
	revenue := t.CollectMonthTax(w)
	// ⑨B 转移支付。
	transferPaid, beneficiaries := t.PayTransferPayments(w)
	// ⑨C 财政支出。
	fiscalSpent := t.ExecuteFiscalSpending(w)
	// ⑨D 国债月度处理(付息/兑付/发行)。
	bondService := t.MonthlyBondStep(w)

	// ⑨E 记录 + 赤字连续月数。
	expense := fiscalSpent + transferPaid + bondService
	t.LastMonthRevenue = revenue
	t.LastMonthExpense = expense
	rec := TreasuryMonthRecord{
		Month: w.Month, Revenue: revenue, Expense: expense,
		Deficit: revenue - expense, TransferPaid: transferPaid,
		BondService: bondService, Beneficiaries: beneficiaries,
	}
	t.History = append(t.History, rec)
	if len(t.History) > TreasuryHistoryLen {
		t.History = t.History[len(t.History)-TreasuryHistoryLen:]
	}
	if rec.Deficit < 0 {
		t.DeficitRun++
		if t.DeficitRun == 3 {
			w.emitEvent("policy", -1, fmt.Sprintf("财政预警:连续 %d 个月赤字(上月赤字 ¥%d),政府考虑发债融资", t.DeficitRun, -rec.Deficit))
		}
	} else {
		t.DeficitRun = 0
	}
}

// monthSpend 月支出基准(发行阈值/紧缩判定用):max(上月支出, 下限常量)。
func (t *TreasuryState) monthSpend() int64 {
	spend := t.LastMonthExpense
	if spend < TreasuryMinMonthlyExpense {
		spend = TreasuryMinMonthlyExpense
	}
	return spend
}
