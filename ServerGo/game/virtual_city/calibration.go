// Package virtual_city — calibration.go: 校准与验证框架(批次71 §4–§7)
//
// 本文件落地《校准与验证框架》的**可执行子集**:
//
//   - §4 七条回归不变量 I1 / I2 / I3 / I4 / I5 / I6 / I8 的逐月检查器;
//   - §6 逐月审计快照(MonthlyAudit)+ D 指标 + 运行级记录(RunRecord);
//   - §7 红线判定器(R-E3 / R-E4 / R-E5 / R-E6 / R-B1)。
//
// 硬纪律(方案 §6.1):
//   - **零 rand 消费** —— 检查器与哈希绝不调用 w.Rand,否则会移动既有随机
//     序列 ⇒ 存量对局回归偏移(§197 反例教训;B10 测试守护)。
//   - **不变量必须是真检查** —— 不得写成恒真式(如「就业率 + 失业率 == 1」
//     这类由赋值构造出来的等式);每条不变量都对照方案 §4 给出的具体口径。
//   - **不新增任何经济数值** —— 本文件只读既有状态,不改任何数值语义。
package virtual_city

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"
)

// ── 严重级常量 ─────────────────────────────────────────────────────────

const (
	// InvariantSeverityFatal 致命(触发即应中止运行)。
	InvariantSeverityFatal = "fatal"
	// InvariantSeveritySerious 严重(报告不得引用该次运行)。
	InvariantSeveritySerious = "serious"
)

// ── 不变量 id 常量(顺序即面板展示顺序,恒 7 条)────────────────────────

const (
	InvariantI1 = "I1" // 座位现金守恒
	InvariantI2 = "I2" // world 注入唯一性
	InvariantI3 = "I3" // 国库收支闭合
	InvariantI4 = "I4" // 资产负债口径契约
	InvariantI5 = "I5" // 引擎状态哈希(双跑对拍)
	InvariantI6 = "I6" // 场景注入留痕
	InvariantI8 = "I8" // 系统净流闭合
)

// ── 红线 id 常量(顺序即面板展示顺序,恒 5 条)──────────────────────────

const (
	RedlineRE1 = "R-E1" // 品类年化上限(批次72 §6)
	RedlineRE2 = "R-E2" // 品类年化下限(批次72 §6)
	RedlineRE3 = "R-E3" // 货币扩张上限
	RedlineRE4 = "R-E4" // 货币扩张下限
	RedlineRE5 = "R-E5" // 破产率上限
	RedlineRE6 = "R-E6" // 破产率下限
	RedlineRB1 = "R-B1" // 首位座位结局偏差
)

// 红线阈值(方案 §7.1)。
const (
	redlineMoneyExpansionMax = 7.0
	redlineMoneyExpansionMin = 2.0
	redlineBankruptcyMax     = 0.40
	redlineBankruptcyMin     = 0.08
	redlineFirstSeatBiasPP   = 0.15 // 15pp
	// 批次72 §6.3:品类年化红线阈值(跨局均值口径)。
	redlineCategoryReturnMax = 0.30
	redlineCategoryReturnMin = 0.03
)

// auditCap 逐月审计快照环形缓冲容量(最近 24 月)。
const auditCap = 24

// calibrationHistoryLen 契约下发的历史条数(最近 12 月)。
const calibrationHistoryLen = 12

// ── 数据结构 ───────────────────────────────────────────────────────────

// InvariantCheck 单条不变量检查结果。
//
// actual / expected 为 nil(契约 §9.2:不适用的检查 → null,不得省略键)。
type InvariantCheck struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Passed   bool     `json:"passed"`
	Severity string   `json:"severity"`
	Actual   *float64 `json:"actual"`
	Expected *float64 `json:"expected"`
	Detail   string   `json:"detail"`
	// NoSample 批次72 §7(N12):该检查无样本可判(当前仅 I6:
	// 无场景注入记录)。true 时 Passed 恒 true(无违例事实),
	// 前端渲染灰色「无样本」徽章(不是 ✔ 也不是 ✘)。
	NoSample bool `json:"no_sample"`
}

// CalibrationIndicators D 指标 + 辅助指标(方案 §6.2)。
//
// ⚠ 无值时为 null,**不得省略键**(契约 §9.2)⇒ 全部用指针。
type CalibrationIndicators struct {
	MoneyExpansion *float64 `json:"money_expansion"` // D2
	BankruptcyRate *float64 `json:"bankruptcy_rate"` // D5
	Gini           *float64 `json:"gini"`
	Unemployment   *float64 `json:"unemployment"`
	CPI            *float64 `json:"cpi"`
	TotalWealth    *int64   `json:"total_wealth"`
	AliveSeats     *int     `json:"alive_seats"`
	TotalSeats     *int     `json:"total_seats"`
	OffLedgerCNY   *int64   `json:"off_ledger_cny"`
}

// RedlineHit 红线命中记录(红线是「越界告警」,不是失败)。
//
// ⚠ Actual / Threshold 为**普通 float64**(无指针)⇒ 恒有值下发,
// **不适用时恒为 0**(不用 null)。红线的阈值是常量、实测值恒有
// (D2 / D5 有值即判定),故可接受;前端按「恒有值」口径渲染。
type RedlineHit struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Triggered bool    `json:"triggered"`
	Actual    float64 `json:"actual"`
	Threshold float64 `json:"threshold"`
	Detail    string  `json:"detail"`
}

// MonthlyAudit 逐月审计快照(挂在 World.Audits 环形缓冲,cap 24)。
type MonthlyAudit struct {
	Month      int                   `json:"month"`
	StateHash  string                `json:"state_hash"`
	Invariants []InvariantCheck      `json:"invariants"`
	Indicators CalibrationIndicators `json:"indicators"`
	Redlines   []RedlineHit          `json:"redlines"`
}

// RunRecord 运行级采集记录(方案 §6.3;本批只做内存版)。
type RunRecord struct {
	RunID             string         `json:"run_id"` // "CR{n}"(Calibration Run)
	Seed              int64          `json:"seed"`
	Phase             string         `json:"phase"`              // α / β / γ
	TotalSteps        int            `json:"total_steps"`        // 推进的模拟月数
	PopulationSize    int            `json:"population_size"`    // 座位数
	TotalAssetsStart  int64          `json:"total_assets_start"` // 期初现金(世界注入总额)
	TotalAssetsEnd    int64          `json:"total_assets_end"`
	MoneyExpansion    float64        `json:"money_expansion"` // D2
	BankruptcyRate    float64        `json:"bankruptcy_rate"` // D5
	OutcomeCounts     map[string]int `json:"outcome_counts"`  // D3
	StateHash         string         `json:"state_hash"`      // D9
	InvariantsOK      []bool         `json:"invariants_ok"`   // 逐条
	InvariantFails    []string       `json:"invariant_fails"` // 失败的不变量 id
	RedlinesTriggered []string       `json:"redlines_triggered"`
	OffLedgerCNY      int64          `json:"off_ledger_cny"` // 账外净流
	// ── 批次72 扩展字段(§5.4,向后兼容追加)──
	FinishedAt         int64              `json:"finished_at"`         // unix 秒
	Strategy           string             `json:"strategy"`            // ""=真实对局 / 5 策略 id
	CategoryAnnualized map[string]float64 `json:"category_annualized"` // §6.2(月数<12 为 nil)
}

// CalibrationState 校准运行状态(挂 World.Calibration;惰性初始化,nil 安全)。
type CalibrationState struct {
	Seq     int         // run id 自增序列(CR1 / CR2…)
	Runs    []RunRecord // 已完成运行(批次72 起 finalize 时同步 JSONL 落盘)
	Current *RunRecord  // 进行中的运行(每次审计刷新)
	// Strategy 当局策略标注(批次72:MC harness 写入,真实对局恒空串;
	// updateCalibrationRun 首建 Current 时拷入 RunRecord.Strategy)。
	Strategy string
	// MarketTrack 品类年化基线追踪(§6.2:期初价 + BondRate 逐月累计;
	// StartGame 捕获,审计兜底)。内部态,不直接序列化(RunRecord 只带结果)。
	MarketTrack *calibrationMarketTrack
}

// calibrationMarketTrack 品类年化计算的内累积器(零 rand)。
type calibrationMarketTrack struct {
	StartStock    float64 // 期初 StockIndex
	StartGold     float64 // 期初 GoldPrice
	StartHouseIdx float64 // 期初 DistrictIdx 全区均值
	StartMonth    int     // 捕获月
	BondRateSum   float64 // 逐月 BondRate(新购票面年化)累计
	BondRateN     int     // 累计月数
}

// OutcomeStat 单座结局统计(R-B1 首位偏差判定输入)。
type OutcomeStat struct {
	Seat    int    // 座位号
	Outcome string // 结局分档 id
	Good    bool   // 是否「良好结局」(winner / affluent)
}

// ── 不变量检查器 ───────────────────────────────────────────────────────

// CheckInvariants 执行七条回归不变量检查(方案 §4)。
//
// 返回恒 7 条、顺序固定:I1 / I2 / I3 / I4 / I5 / I6 / I8。
// 零 rand 消费;nil World → 7 条全 false。
func CheckInvariants(w *World) []InvariantCheck {
	out := make([]InvariantCheck, 0, 7)
	if w == nil {
		for _, id := range []string{InvariantI1, InvariantI2, InvariantI3, InvariantI4, InvariantI5, InvariantI6, InvariantI8} {
			out = append(out, InvariantCheck{ID: id, Name: invariantName(id), Passed: false,
				Severity: invariantSeverity(id), Detail: "世界未初始化,无法检查"})
		}
		return out
	}
	out = append(out, checkI1(w))
	out = append(out, checkI2(w))
	out = append(out, checkI3(w))
	out = append(out, checkI4(w))
	out = append(out, checkI5(w))
	out = append(out, checkI6(w))
	out = append(out, checkI8(w))
	return out
}

// invariantName 不变量中文名(后端兜底文案;前端优先用 i18n)。
func invariantName(id string) string {
	switch id {
	case InvariantI1:
		return "座位现金守恒"
	case InvariantI2:
		return "世界注入唯一性"
	case InvariantI3:
		return "国库收支闭合"
	case InvariantI4:
		return "资产负债口径契约"
	case InvariantI5:
		return "引擎状态哈希"
	case InvariantI6:
		return "场景注入留痕"
	case InvariantI8:
		return "系统净流闭合"
	}
	return id
}

// invariantSeverity 不变量严重级(I6 为 serious,其余 fatal)。
func invariantSeverity(id string) string {
	if id == InvariantI6 {
		return InvariantSeveritySerious
	}
	return InvariantSeverityFatal
}

// f64Ptr 取 float64 指针(便于构造 InvariantCheck 的 actual/expected)。
func f64Ptr(v float64) *float64 { return &v }

// i64Ptr 取 int64 指针。
func i64Ptr(v int64) *int64 { return &v }

// intPtr 取 int 指针。
func intPtr(v int) *int { return &v }

// ledgerInjectSummary 一次性扫描账本,汇总每个座位的 world 注入
// (金额 / 条数 / 注入月)。I1 与 I2 共用(避免两趟全扫)。
func ledgerInjectSummary(w *World) (amount [MaxSeats]int64, count [MaxSeats]int, month [MaxSeats]int) {
	if w == nil || w.Ledger == nil {
		return
	}
	for _, e := range w.Ledger.Entries {
		if e.From != EntityWorld {
			continue
		}
		seat, ok := IsSeatEntity(e.To)
		if !ok {
			continue
		}
		amount[seat] += e.AmountCNY
		count[seat]++
		month[seat] = e.Month
	}
	return
}

// checkI1 座位现金守恒:
//
//	p.Cash == injectAmount(seat) + (LedgerSeatNet(seat) − injectAmount(seat))
//	          + OffLedger.SeatNet(seat)
//
// 等价于「账本净流 + 账外净流 == 座位现金」—— 其中账本净流已含世界注入。
//
// ⚠ **出局座位口径(实测结论,方案 §4.2 要求「据实决定」)**:以 40 月 × 6 座
// 实跑(批次71 勘察)验证,破产出局座位在出局当月与出局后的差额**恒为 0**
// —— 破产清算不改 `p.Cash`(只记 BankruptCount),遗产清算的清零由
// `estate_clear` 账外登记精确对冲。因此本检查**对全部在座座位生效**
// (含已出局座位),不做任何跳过;detail 记录在座/出局覆盖数。
func checkI1(w *World) InvariantCheck {
	c := InvariantCheck{ID: InvariantI1, Name: invariantName(InvariantI1), Severity: InvariantSeverityFatal}
	inject, _, _ := ledgerInjectSummary(w)
	var totalDiff int64
	seats, out := 0, 0
	for seat, p := range w.Players {
		if p == nil {
			continue
		}
		seats++
		if !p.Alive {
			out++
		}
		ledgerNet := int64(0)
		if w.Ledger != nil {
			ledgerNet = w.Ledger.SeatNets[seat]
		}
		netExInject := ledgerNet - inject[seat]
		off := int64(0)
		if w.OffLedger != nil {
			off = w.OffLedger.SeatNetFlow(seat)
		}
		diff := p.Cash - (inject[seat] + netExInject + off)
		totalDiff += diff
	}
	c.Actual = f64Ptr(float64(totalDiff))
	c.Expected = f64Ptr(0)
	c.Passed = totalDiff == 0
	c.Detail = fmt.Sprintf("%d 座逐月现金差额合计 %d(其中 %d 座已出局,仍参与守恒断言)", seats, totalDiff, out)
	return c
}

// checkI2 world 注入唯一性:每座恰好 1 条 from=world 注入,且 month == 0。
func checkI2(w *World) InvariantCheck {
	c := InvariantCheck{ID: InvariantI2, Name: invariantName(InvariantI2), Severity: InvariantSeverityFatal}
	_, count, month := ledgerInjectSummary(w)
	bad := 0
	seats := 0
	for seat, p := range w.Players {
		if p == nil {
			continue
		}
		seats++
		if count[seat] != 1 || month[seat] != 0 {
			bad++
		}
	}
	// 注入到非座位实体的条目也是违规(world 只能注入座位)。
	stray := 0
	if w.Ledger != nil {
		for _, e := range w.Ledger.Entries {
			if e.From == EntityWorld {
				if _, ok := IsSeatEntity(e.To); !ok {
					stray++
				}
			}
		}
	}
	c.Actual = f64Ptr(float64(bad + stray))
	c.Expected = f64Ptr(0)
	c.Passed = bad == 0 && stray == 0
	c.Detail = fmt.Sprintf("%d 座注入唯一性违例 %d 条;world→非座位注入 %d 条", seats, bad, stray)
	return c
}

// checkI3 国库收支闭合:
//
//	Cash − PrevMonthCash == LastMonthRevenue − LastMonthExpense − OtherExpense
//
// 依据(方案 §4.3):国库全部现金变动都在 SettleTreasuryMonth 窗口内;
// IssueBonds 只记 BondsOutstanding 不动现金。
//
// ⚠ **OtherExpense**:第 7 处国库现金改动(市长津贴 civic_election.go)在
// ⑨G(国库月结之后)发生,不在 LastMonthExpense 口径内 —— 按方案 §4.3
// 「定位该改动点并登记」的要求单独登记,参与本恒等式。
func checkI3(w *World) InvariantCheck {
	c := InvariantCheck{ID: InvariantI3, Name: invariantName(InvariantI3), Severity: InvariantSeverityFatal}
	if w.Treasury == nil {
		c.Passed = false
		c.Detail = "未启用国库子系统,本项不适用"
		return c
	}
	if !w.EconomyEnabled {
		c.Passed = true
		c.Detail = "economy_enabled=false,国库月结未运行,本项不适用(不参与判定)"
		return c
	}
	t := w.Treasury
	diff := (t.Cash - t.PrevMonthCash) - (t.LastMonthRevenue - t.LastMonthExpense - t.OtherExpense)
	c.Actual = f64Ptr(float64(diff))
	c.Expected = f64Ptr(0)
	c.Passed = diff == 0
	c.Detail = fmt.Sprintf("Δ国库现金 %d vs 收入 %d − 支出 %d − 账外支出 %d",
		t.Cash-t.PrevMonthCash, t.LastMonthRevenue, t.LastMonthExpense, t.OtherExpense)
	return c
}

// checkI4 资产负债口径契约:独立重算 现金+养老金+Σ资产市值−Σ贷款余额,
// 与 p.NetWorth(market) 逐座比对(不调用 p.NetWorth 本体)。
//
// 附加断言:Loan.Balance >= 0、AssetValue >= 0。
func checkI4(w *World) InvariantCheck {
	c := InvariantCheck{ID: InvariantI4, Name: invariantName(InvariantI4), Severity: InvariantSeverityFatal}
	var diff int64
	negAssets, negLoans := 0, 0
	seats := 0
	for _, p := range w.Players {
		if p == nil {
			continue
		}
		seats++
		recomputed := recomputeNetWorth(p, w.Market)
		diff += recomputed - p.NetWorth(w.Market)
		for i := range p.Assets {
			if AssetValue(&p.Assets[i], w.Market) < 0 {
				negAssets++
			}
		}
		for i := range p.Loans {
			if p.Loans[i].Balance < 0 {
				negLoans++
			}
		}
	}
	c.Actual = f64Ptr(float64(diff))
	c.Expected = f64Ptr(0)
	c.Passed = diff == 0 && negAssets == 0 && negLoans == 0
	c.Detail = fmt.Sprintf("%d 座口径差额 %d;负资产 %d 笔;负贷款余额 %d 笔", seats, diff, negAssets, negLoans)
	return c
}

// recomputeNetWorth 独立重算净资产口径(不调用 Player.NetWorth)。
//
// 口径:现金 + 养老金 + Σ 资产市值 − Σ 贷款余额。
// 资产估值复用 AssetValue 原语(估值规则本身属既有契约,本检查守护的是
// **组合口径**:NetWorth 是否偷偷改了语义,如加入 SavingsDeposit)。
func recomputeNetWorth(p *Player, m *MarketState) int64 {
	total := p.Cash + p.PensionCNY
	for i := range p.Assets {
		total += AssetValue(&p.Assets[i], m)
	}
	for i := range p.Loans {
		total -= p.Loans[i].Balance
	}
	return total
}

// checkI5 引擎状态哈希:本项在运行时校验「哈希已生成且格式合法」
// (16 位十六进制);**同 seed 双跑逐月一致**由离线测试(方案 §10.1 B8)保证。
func checkI5(w *World) InvariantCheck {
	c := InvariantCheck{ID: InvariantI5, Name: invariantName(InvariantI5), Severity: InvariantSeverityFatal}
	hash := w.StateHash()
	valid := len(hash) == 16
	if valid {
		for i := 0; i < len(hash); i++ {
			ch := hash[i]
			if !(ch >= '0' && ch <= '9') && !(ch >= 'a' && ch <= 'f') {
				valid = false
				break
			}
		}
	}
	c.Actual = f64Ptr(float64(len(hash)))
	c.Expected = f64Ptr(16)
	c.Passed = valid
	c.Detail = fmt.Sprintf("状态哈希 %q(16 位十六进制;双跑对拍见离线测试)", hash)
	return c
}

// checkI6 场景注入留痕:每条记录含 kind / magnitude / target / duration /
// seed / snapshot,且 magnitude ∈ [1,5]、applied_month > 0、duration ≥ 1、seed ≠ 0。
func checkI6(w *World) InvariantCheck {
	c := InvariantCheck{ID: InvariantI6, Name: invariantName(InvariantI6), Severity: InvariantSeveritySerious}
	bad := 0
	total := len(w.ScenarioApplied)
	for _, rec := range w.ScenarioApplied {
		if rec.Kind == "" || rec.Magnitude < 1 || rec.Magnitude > 5 ||
			rec.AppliedMonth <= 0 || rec.DurationMonths < 1 || rec.Seed == 0 {
			bad++
		}
	}
	c.Actual = f64Ptr(float64(bad))
	c.Expected = f64Ptr(0)
	c.Passed = bad == 0
	if total == 0 {
		c.NoSample = true // 批次72 §7:无样本状态位(Passed 保持 true)
		c.Detail = "尚无场景注入记录,本项无样本"
	} else {
		c.Detail = fmt.Sprintf("%d 条注入记录中 %d 条留痕不完整", total, bad)
	}
	return c
}

// checkI8 系统净流闭合:
//
//	Σ 所有实体净流 == 0;座位组净流 + 非座位组净流 == 0。
//
// 口径声明(方案 §4.6):**国库不走 Ledger**(to=gov:treasury 恒非法),
// 故 I8 的部门集合 = seat:* / bank / market / gov / firms / insurer /
// world / family / gov:treasury;国库侧由 **I3** 单独守恒。
//
// 本检查独立重扫账本(不信任增量计数),因此能抓到绕过 Record 直接
// append 的条目(实体不在白名单 ⇒ 「来路不明」组净流 ≠ 0)。
func checkI8(w *World) InvariantCheck {
	c := InvariantCheck{ID: InvariantI8, Name: invariantName(InvariantI8), Severity: InvariantSeverityFatal}
	if w.Ledger == nil {
		c.Passed = false
		c.Detail = "账本未初始化,本项不适用"
		return c
	}
	known := map[string]bool{
		EntityBank: true, EntityMarket: true, EntityGov: true, EntityInsurer: true,
		EntityWorld: true, EntityFirms: true, EntityGovernment: true, EntityFamily: true,
	}
	var seatNet, otherNet, unknownNet int64
	negative := 0
	for _, e := range w.Ledger.Entries {
		if e.AmountCNY < 0 {
			negative++
		}
		for _, ent := range [2]string{e.To, e.From} {
			sign := int64(1)
			if ent == e.From {
				sign = -1
			}
			if _, ok := IsSeatEntity(ent); ok {
				seatNet += sign * e.AmountCNY
				continue
			}
			if known[ent] {
				otherNet += sign * e.AmountCNY
				continue
			}
			unknownNet += sign * e.AmountCNY
		}
	}
	diff := seatNet + otherNet
	c.Actual = f64Ptr(float64(diff))
	c.Expected = f64Ptr(0)
	c.Passed = diff == 0 && unknownNet == 0 && negative == 0
	c.Detail = fmt.Sprintf("座位组净流 %d + 非座位组净流 %d = %d;白名单外实体净流 %d;负额条目 %d",
		seatNet, otherNet, diff, unknownNet, negative)
	return c
}

// ── D 指标与结局分布 ───────────────────────────────────────────────────

// collectIndicators 采集 D 指标 + 辅助指标(方案 §6.2)。零 rand 消费。
//
// D2 口径(方案 §6.2 说明):周期末全体居民**总资产** ÷ 周期初全体**现金**;
// 期初现金 = Ledger 的 from=world 注入总额;期末总资产 =
// Σ (Cash + SavingsDeposit + PensionCNY + Σ AssetValue − Σ Loan.Balance)。
// ⚠ 该口径与央行 MoneyMultiplier(M2/基础货币)**不是同一个量**,不可混用。
func collectIndicators(w *World) CalibrationIndicators {
	var ind CalibrationIndicators
	if w == nil {
		return ind
	}
	seats, alive := 0, 0
	var assetsEnd int64
	for _, p := range w.Players {
		if p == nil {
			continue
		}
		seats++
		if p.Alive {
			alive++
		}
		assetsEnd += p.Cash + p.SavingsDeposit + p.PensionCNY
		for i := range p.Assets {
			assetsEnd += AssetValue(&p.Assets[i], w.Market)
		}
		for i := range p.Loans {
			assetsEnd -= p.Loans[i].Balance
		}
	}
	ind.AliveSeats = intPtr(alive)
	ind.TotalSeats = intPtr(seats)

	// D2 货币扩张倍数。
	injectTotal := int64(0)
	if w.Ledger != nil {
		for _, e := range w.Ledger.Entries {
			if e.From == EntityWorld {
				if _, ok := IsSeatEntity(e.To); ok {
					injectTotal += e.AmountCNY
				}
			}
		}
	}
	if injectTotal > 0 {
		ind.MoneyExpansion = f64Ptr(float64(assetsEnd) / float64(injectTotal))
	}

	// D5 破产率:BankruptCount > 0 的座位 ÷ 总座位。
	if seats > 0 {
		bankrupt := 0
		for _, p := range w.Players {
			if p != nil && p.BankruptCount > 0 {
				bankrupt++
			}
		}
		ind.BankruptcyRate = f64Ptr(float64(bankrupt) / float64(seats))
	}

	if w.Society != nil {
		ind.Gini = f64Ptr(w.Society.Gini)
		ind.TotalWealth = i64Ptr(w.Society.TotalWealth)
	}
	if w.Labor != nil {
		ind.Unemployment = f64Ptr(w.Labor.Unemployment)
	}
	if w.Goods != nil {
		ind.CPI = f64Ptr(w.Goods.CPIYoY)
	} else if w.CB != nil {
		ind.CPI = f64Ptr(w.CB.CPI)
	}
	if w.OffLedger != nil {
		ind.OffLedgerCNY = i64Ptr(w.OffLedger.Total())
	} else {
		ind.OffLedgerCNY = i64Ptr(0)
	}
	return ind
}

// outcomeStats 逐座结局分档(D3 + R-B1 输入)。
//
// 终局结局取 p.Ending;未终局(p.Ending == "")时按「存活 + 净值分档」近似:
// 高于中位净值 → winner 档;低于中位 → ordinary 档;已出局 → out 档。
func outcomeStats(w *World) []OutcomeStat {
	out := make([]OutcomeStat, 0, MaxSeats)
	if w == nil {
		return out
	}
	// 存活座位净值中位数(用于未终局分档)。
	var aliveNW []int64
	for _, p := range w.Players {
		if p == nil || !p.Alive {
			continue
		}
		aliveNW = append(aliveNW, p.NetWorth(w.Market))
	}
	median := int64(0)
	if len(aliveNW) > 0 {
		sorted := append([]int64(nil), aliveNW...)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
		median = sorted[len(sorted)/2]
	}
	for seat, p := range w.Players {
		if p == nil {
			continue
		}
		bucket := outcomeBucket(p, median, w.Market)
		out = append(out, OutcomeStat{
			Seat:    seat,
			Outcome: bucket,
			Good:    bucket == OutcomeWinner || bucket == OutcomeAffluent,
		})
	}
	return out
}

// 结局分档 id(D3 口径)。
const (
	OutcomeWinner   = "winner"   // 自由
	OutcomeAffluent = "affluent" // 小康
	OutcomeOrdinary = "ordinary" // 温饱
	OutcomeIndebted = "indebted" // 债务缠身
	OutcomeBankrupt = "bankrupt" // 破产
	OutcomeOut      = "out"      // 已出局(未标注结局)
	OutcomeUnknown  = "unknown"  // 未知
)

// outcomeBucket 结局分档。
func outcomeBucket(p *Player, aliveMedianNW int64, m *MarketState) string {
	if p == nil {
		return OutcomeUnknown
	}
	switch p.Ending {
	case EndingWinner, EndingLonelyRic:
		return OutcomeWinner
	case EndingAffluent:
		return OutcomeAffluent
	case EndingOrdinary:
		return OutcomeOrdinary
	case EndingIndebted:
		return OutcomeIndebted
	case EndingBankrupt:
		return OutcomeBankrupt
	case EndingAccidentDeath:
		return OutcomeOut
	}
	if !p.Alive {
		return OutcomeOut
	}
	// 未终局:按净值分档近似(高于中位 → 小康,否则 → 温饱)。
	if p.NetWorth(m) > aliveMedianNW {
		return OutcomeAffluent
	}
	return OutcomeOrdinary
}

// outcomeCounts 结局分布计数(D3)。
func outcomeCounts(outcomes []OutcomeStat) map[string]int {
	counts := map[string]int{
		OutcomeWinner: 0, OutcomeAffluent: 0, OutcomeOrdinary: 0,
		OutcomeIndebted: 0, OutcomeBankrupt: 0, OutcomeOut: 0, OutcomeUnknown: 0,
	}
	for _, o := range outcomes {
		counts[o.Outcome]++
	}
	return counts
}

// ── 红线判定 ───────────────────────────────────────────────────────────

// EvaluateRedlines 判定 7 条红线(批次71 §7.1 的 5 条 + 批次72 §6 的
// R-E1/R-E2 品类年化上下限,追加在尾部;前端按 id 渲染不受序影响)。
//
// 判定是**纯函数**:输入 D 指标 + 逐座结局 + 最近完成运行(runs,
// 仅 R-E1/R-E2 消费;nil = 无历史运行),输出恒 7 条、顺序固定。
// 红线是「越界告警」不是「失败」:触发不中止运行。
func EvaluateRedlines(ind CalibrationIndicators, outcomes []OutcomeStat, runs []RunRecord) []RedlineHit {
	out := make([]RedlineHit, 0, 7)

	// R-E3 货币扩张上限 / R-E4 货币扩张下限(D2)。
	d2 := 0.0
	hasD2 := ind.MoneyExpansion != nil
	if hasD2 {
		d2 = *ind.MoneyExpansion
	}
	out = append(out, RedlineHit{
		ID: RedlineRE3, Name: "货币扩张上限", Triggered: hasD2 && d2 > redlineMoneyExpansionMax,
		Actual: d2, Threshold: redlineMoneyExpansionMax,
		Detail: fmt.Sprintf("货币扩张倍数 %.2f ≤ %.1f", d2, redlineMoneyExpansionMax),
	})
	if hasD2 && d2 > redlineMoneyExpansionMax {
		out[len(out)-1].Detail = fmt.Sprintf("货币扩张倍数 %.2f > %.1f", d2, redlineMoneyExpansionMax)
	}
	out = append(out, RedlineHit{
		ID: RedlineRE4, Name: "货币扩张下限", Triggered: hasD2 && d2 < redlineMoneyExpansionMin,
		Actual: d2, Threshold: redlineMoneyExpansionMin,
		Detail: fmt.Sprintf("货币扩张倍数 %.2f ≥ %.1f", d2, redlineMoneyExpansionMin),
	})
	if hasD2 && d2 < redlineMoneyExpansionMin {
		out[len(out)-1].Detail = fmt.Sprintf("货币扩张倍数 %.2f < %.1f", d2, redlineMoneyExpansionMin)
	}

	// R-E5 破产率上限 / R-E6 破产率下限(D5)。
	d5 := 0.0
	hasD5 := ind.BankruptcyRate != nil
	if hasD5 {
		d5 = *ind.BankruptcyRate
	}
	out = append(out, RedlineHit{
		ID: RedlineRE5, Name: "破产率上限", Triggered: hasD5 && d5 > redlineBankruptcyMax,
		Actual: d5, Threshold: redlineBankruptcyMax,
		Detail: fmt.Sprintf("破产率 %.1f%% ≤ %.0f%%", d5*100, redlineBankruptcyMax*100),
	})
	if hasD5 && d5 > redlineBankruptcyMax {
		out[len(out)-1].Detail = fmt.Sprintf("破产率 %.1f%% > %.0f%%", d5*100, redlineBankruptcyMax*100)
	}
	out = append(out, RedlineHit{
		ID: RedlineRE6, Name: "破产率下限", Triggered: hasD5 && d5 < redlineBankruptcyMin,
		Actual: d5, Threshold: redlineBankruptcyMin,
		Detail: fmt.Sprintf("破产率 %.1f%% ≥ %.0f%%", d5*100, redlineBankruptcyMin*100),
	})
	if hasD5 && d5 < redlineBankruptcyMin {
		out[len(out)-1].Detail = fmt.Sprintf("破产率 %.1f%% < %.0f%%", d5*100, redlineBankruptcyMin*100)
	}

	// R-B1 首位座位结局偏差:首位座位(0 号位)「良好结局占比」> 其余均值 + 15pp。
	//
	// ⚠ 口径说明:单局每座只有一个结局 ⇒ 「占比」退化为 0/1 指示量;
	// 跨局聚合口径(1000 局蒙特卡洛)属批次 72。
	firstGood, others, othersCount := 0.0, 0.0, 0
	for _, o := range outcomes {
		if o.Seat == 0 {
			if o.Good {
				firstGood = 1
			}
			continue
		}
		othersCount++
		if o.Good {
			others += 1
		}
	}
	othersRate := 0.0
	if othersCount > 0 {
		othersRate = others / float64(othersCount)
	}
	rb1 := RedlineHit{
		ID: RedlineRB1, Name: "首位座位结局偏差",
		Triggered: firstGood > othersRate+redlineFirstSeatBiasPP,
		Actual:    firstGood, Threshold: othersRate + redlineFirstSeatBiasPP,
		Detail: fmt.Sprintf("首位座位良好结局 %.0f%% vs 其余均值 %.0f%%(阈值 +%.0fpp)",
			firstGood*100, othersRate*100, redlineFirstSeatBiasPP*100),
	}
	out = append(out, rb1)

	// 批次72 §6.3:R-E1 品类年化上限 / R-E2 下限(市场级口径,
	// 跨最近 categoryRunsWindow 局完成运行取均值)。
	out = append(out, evaluateCategoryRedline(runs, RedlineRE1, true))
	out = append(out, evaluateCategoryRedline(runs, RedlineRE2, false))
	return out
}

// categoryRunsWindow R-E1/R-E2 的跨局均值窗口(方案 §6.3)。
const categoryRunsWindow = 10

// mcCategoryOrder R-E1/R-E2 判定的品类顺序(与 §6.2 表一致)。
var mcCategoryOrder = []string{AssetStockIndex, AssetGold, "house", AssetBond}

// evaluateCategoryRedline 品类年化跨局均值红线。
//
//   - 窗口 = runs 尾部 min(10, len) 局(MC 局与真实局混计 —— 品类行情
//     与策略无关,但需 ≥10 局才触发);
//   - 每品类对窗口内有该品类值的局取均值;upper ⇒ 任一品类均值 > 0.30
//     触发(actual = 最差品类均值),lower ⇒ 任一品类均值 < 0.03 触发
//     (actual = 最低品类均值);
//   - 窗口不足 10 局 ⇒ 不触发,detail 标注「样本不足(n=x/10)」。
func evaluateCategoryRedline(runs []RunRecord, id string, upper bool) RedlineHit {
	name := "品类年化上限"
	threshold := redlineCategoryReturnMax
	if !upper {
		name = "品类年化下限"
		threshold = redlineCategoryReturnMin
	}
	hit := RedlineHit{ID: id, Name: name, Threshold: threshold}

	window := len(runs)
	if window > categoryRunsWindow {
		runs = runs[len(runs)-categoryRunsWindow:]
		window = categoryRunsWindow
	}
	if window < categoryRunsWindow {
		hit.Detail = fmt.Sprintf("样本不足(n=%d/%d),不触发", window, categoryRunsWindow)
		return hit
	}

	worstMean, worstCat := 0.0, ""
	for _, cat := range mcCategoryOrder {
		var vals []float64
		for i := range runs {
			if v, ok := runs[i].CategoryAnnualized[cat]; ok {
				vals = append(vals, v)
			}
		}
		if len(vals) == 0 {
			continue
		}
		mean := mcMean(vals)
		if worstCat == "" || (upper && mean > worstMean) || (!upper && mean < worstMean) {
			worstMean, worstCat = mean, cat
		}
	}
	if worstCat == "" {
		hit.Detail = fmt.Sprintf("窗口内 %d 局均无品类年化数据,不触发", window)
		return hit
	}
	mean := worstMean
	hit.Actual = mean
	if upper {
		hit.Triggered = mean > redlineCategoryReturnMax
		hit.Detail = fmt.Sprintf("品类 %s 年化均值 %.1f%%(窗口 %d 局;> %.0f%% 触发)", worstCat, mean*100, window, redlineCategoryReturnMax*100)
	} else {
		hit.Triggered = mean < redlineCategoryReturnMin
		hit.Detail = fmt.Sprintf("品类 %s 年化均值 %.1f%%(窗口 %d 局;< %.0f%% 触发)", worstCat, mean*100, window, redlineCategoryReturnMin*100)
	}
	return hit
}

// ── 逐月审计 ───────────────────────────────────────────────────────────

// auditMonth 逐月审计快照(方案 §6.1):写入点为 SettleMonth 末尾
// (w.Month++ 之前)。零 rand 消费;nil World 安全 no-op。
func (w *World) auditMonth() {
	if w == nil {
		return
	}
	hash := w.StateHash()
	invariants := CheckInvariants(w)
	indicators := collectIndicators(w)
	outcomes := outcomeStats(w)
	var pastRuns []RunRecord // R-E1/R-E2 跨局均值输入(批次72 §6.3)
	if w.Calibration != nil {
		pastRuns = w.Calibration.Runs
	}
	redlines := EvaluateRedlines(indicators, outcomes, pastRuns)

	audit := MonthlyAudit{
		Month:      w.Month,
		StateHash:  hash,
		Invariants: invariants,
		Indicators: indicators,
		Redlines:   redlines,
	}
	w.Audits = append(w.Audits, audit)
	if len(w.Audits) > auditCap {
		w.Audits = w.Audits[len(w.Audits)-auditCap:]
	}
	w.updateCalibrationRun(audit, outcomes)
}

// calibrationEnsure 惰性初始化校准状态(nil 安全)。
func (w *World) calibrationEnsure() *CalibrationState {
	if w == nil {
		return nil
	}
	if w.Calibration == nil {
		w.Calibration = &CalibrationState{}
	}
	return w.Calibration
}

// updateCalibrationRun 刷新进行中运行记录(方案 §6.3)。
func (w *World) updateCalibrationRun(audit MonthlyAudit, outcomes []OutcomeStat) {
	cs := w.calibrationEnsure()
	if cs == nil {
		return
	}
	if cs.Current == nil {
		cs.Seq++
		cs.Current = &RunRecord{
			RunID:          "CR" + strconv.Itoa(cs.Seq),
			Seed:           w.Seed,
			Phase:          "α",
			Strategy:       cs.Strategy, // 批次72:MC 局策略标注(真实对局空串)
			OutcomeCounts:  map[string]int{},
			InvariantsOK:   []bool{},
			InvariantFails: []string{},
		}
	}
	if cs.MarketTrack == nil {
		w.calibrationCaptureMarketStart() // 兜底:未经 StartGame 的世界(测试路径)
	}
	rec := cs.Current
	rec.TotalSteps = w.Month
	rec.PopulationSize = 0
	for _, p := range w.Players {
		if p != nil {
			rec.PopulationSize++
		}
	}
	rec.TotalAssetsStart = w.worldInjectTotal()
	rec.TotalAssetsEnd = totalAssetsEnd(w)
	if rec.TotalAssetsStart > 0 {
		rec.MoneyExpansion = float64(rec.TotalAssetsEnd) / float64(rec.TotalAssetsStart)
	}
	rec.BankruptcyRate = 0
	if rec.PopulationSize > 0 {
		bankrupt := 0
		for _, p := range w.Players {
			if p != nil && p.BankruptCount > 0 {
				bankrupt++
			}
		}
		rec.BankruptcyRate = float64(bankrupt) / float64(rec.PopulationSize)
	}
	rec.OutcomeCounts = outcomeCounts(outcomes)
	rec.StateHash = audit.StateHash
	rec.InvariantsOK = rec.InvariantsOK[:0]
	rec.InvariantFails = rec.InvariantFails[:0]
	for _, inv := range audit.Invariants {
		rec.InvariantsOK = append(rec.InvariantsOK, inv.Passed)
		if !inv.Passed {
			rec.InvariantFails = append(rec.InvariantFails, inv.ID)
		}
	}
	rec.RedlinesTriggered = rec.RedlinesTriggered[:0]
	for _, rl := range audit.Redlines {
		if rl.Triggered {
			rec.RedlinesTriggered = append(rec.RedlinesTriggered, rl.ID)
		}
	}
	if w.OffLedger != nil {
		rec.OffLedgerCNY = w.OffLedger.Total()
	}

	// 批次72 §6.2:品类年化 —— BondRate 逐月累计 + 期初/期末价年化
	// (月数 <12 不产出;零 rand)。
	if mt := cs.MarketTrack; mt != nil && w.Market != nil {
		mt.BondRateSum += w.Market.Params().BondRate
		mt.BondRateN++
		rec.CategoryAnnualized = computeCategoryAnnualized(w, mt, w.Month)
	}
}

// calibrationCaptureMarketStart 捕获品类年化期初基线(StartGame 调一次;
// 幂等:已有 MarketTrack 时不覆盖)。零 rand。
func (w *World) calibrationCaptureMarketStart() {
	cs := w.calibrationEnsure()
	if cs == nil || cs.MarketTrack != nil || w.Market == nil {
		return
	}
	cs.MarketTrack = &calibrationMarketTrack{
		StartStock:    w.Market.StockIndex,
		StartGold:     w.Market.GoldPrice,
		StartHouseIdx: w.houseIndexMean(),
		StartMonth:    w.Month,
	}
}

// houseIndexMean DistrictIdx 全区均值(DistrictDefs 固定序求和 ⇒ 确定性)。
func (w *World) houseIndexMean() float64 {
	if w == nil || w.Market == nil {
		return 0
	}
	var sum float64
	n := 0
	for _, d := range DistrictDefs {
		v, ok := w.Market.DistrictIdx[d.ID]
		if !ok {
			continue
		}
		sum += v
		n++
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

// computeCategoryAnnualized 品类年化(方案 §6.2):
//
//	stock_index / gold / house:(期末/期初)^(12/月数) − 1(复利年化);
//	bond:逐月 BondRate(新购票面年化)算术均值;
//	月数 < 12 ⇒ nil(数据不足)。零 rand。
func computeCategoryAnnualized(w *World, mt *calibrationMarketTrack, months int) map[string]float64 {
	if w == nil || w.Market == nil || mt == nil || months < 12 {
		return nil
	}
	out := map[string]float64{
		AssetStockIndex: mcCompoundAnnual(mt.StartStock, w.Market.StockIndex, months),
		AssetGold:       mcCompoundAnnual(mt.StartGold, w.Market.GoldPrice, months),
		"house":         mcCompoundAnnual(mt.StartHouseIdx, w.houseIndexMean(), months),
	}
	if mt.BondRateN > 0 {
		out[AssetBond] = mt.BondRateSum / float64(mt.BondRateN)
	}
	return out
}

// mcCompoundAnnual 复利年化:(end/start)^(12/months) − 1(非有限值 → 0)。
func mcCompoundAnnual(start, end float64, months int) float64 {
	if months < 12 || start <= 0 {
		return 0
	}
	r := math.Pow(end/start, 12.0/float64(months)) - 1
	if math.IsNaN(r) || math.IsInf(r, 0) {
		return 0
	}
	return r
}

// worldInjectTotal 世界注入现金总额(D2 分母:期初全体现金)。
func (w *World) worldInjectTotal() int64 {
	var total int64
	if w.Ledger == nil {
		return 0
	}
	for _, e := range w.Ledger.Entries {
		if e.From == EntityWorld {
			if _, ok := IsSeatEntity(e.To); ok {
				total += e.AmountCNY
			}
		}
	}
	return total
}

// totalAssetsEnd 期末全体居民总资产(D2 分子口径)。
func totalAssetsEnd(w *World) int64 {
	var total int64
	if w == nil {
		return 0
	}
	for _, p := range w.Players {
		if p == nil {
			continue
		}
		total += p.Cash + p.SavingsDeposit + p.PensionCNY
		for i := range p.Assets {
			total += AssetValue(&p.Assets[i], w.Market)
		}
		for i := range p.Loans {
			total -= p.Loans[i].Balance
		}
	}
	return total
}

// finalizeCalibrationRun 终局时把进行中运行收口进 Runs,并同步 JSONL 落盘
// (批次72 §5.1:仅在终局调用一次,不在月结热路径 ⇒ 同步写可接受;
// 写失败 zap warn,绝不 panic、绝不阻断对局收口)。
func (w *World) finalizeCalibrationRun() {
	cs := w.calibrationEnsure()
	if cs == nil || cs.Current == nil {
		return
	}
	rec := *cs.Current
	rec.FinishedAt = time.Now().Unix()
	cs.Runs = append(cs.Runs, rec)
	cs.Current = nil
	appendRunLogging(rec)
}
