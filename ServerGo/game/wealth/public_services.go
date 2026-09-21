// Package wealth — public_services.go: 公共服务五件套 + 监管聚合(阶段8)
// 2026-09-21 §城市扩张v2.12 阶段8(最终阶段)。
//
// 职责:
//   - PublicServices:教育/医疗/养老质量(财政支出占比累积)+ 住房可负担性
//     (房价收入比映射)+ 就业服务(FirmSector.Employment 直读);
//   - EducationSystem / HealthcareSystem:公共投入存量镜像 + 教育代际效应
//     (年度 Cognition 增值)/ 医疗因子查询(意外概率/保费折扣,v2.13 接线);
//   - RegulatorBundle:包装 regulators 子包 4 实例(证监会/反垄断/消协/隐私),
//     从 World(Ledger/SupplyChain/Players)适配输入,执行罚款与事件播报。
//
// 接线:settlement.go ⑨G(SettleMonth,产业集群之后)月步;教育年度步挂
// AnnualAdjust;view.go 下发 public_services 快照。
//
// 纪律(与阶段4-7 一致):纯引擎层,无锁、无 goroutine、无 IO、**零 rand
// 消费** —— 固定种子存量对局回归零偏移。资金变动全部走 w.Pay(I1 守恒):
//   - 监管罚款:seat → gov(EntityGov,新增 category=reg_fine;与个税同实体,
//     不入 gov:treasury —— Ledger 白名单禁止 to=gov:treasury,见 ledger.go);
//   - 市长津贴:civic_election.go,gov:treasury → seat(CatWelfare 唯一合法通道)。
package wealth

import (
	"fmt"
	"sort"

	"LsmAgentGame/game/wealth/regulators"
)

// 公共服务常量(阶段8 新定)。
const (
	// PublicSvcQualityGain 质量月增速系数:月增速 = 财政支出占比 × 0.01
	// (教育 30% → +0.003/月,约 28 年满格 —— 420 月对局内合理节奏)。
	PublicSvcQualityGain = 0.01
	// HousingRatioDivisor 房价收入比映射分母:afford = clamp(1 − ratio/20, 0, 1)
	// (ratio = 中位房价 ÷ 年收入;ratio ≥ 20 → 0)。
	HousingRatioDivisor = 20.0
	// EduIncomeGateCNY 教育代际效应门槛:月收入 > 15000 才能负担私立教育。
	EduIncomeGateCNY int64 = 15_000
	// EduCognitionAnnualGain 年度 Cognition 增值(0.1/年,10 年 +1 点)。
	EduCognitionAnnualGain = 0.1
	// CognitionCap 资源 K 上限(与既有 clamp 口径一致)。
	CognitionCap = 10
	// MedInjuryQualityGate 医疗质量门槛:> 0.5 → 意外受伤概率 −20%。
	MedInjuryQualityGate = 0.5
	// MedInjuryFactor 意外概率因子(0.8)。
	MedInjuryFactor = 0.8
	// MedPremiumQualityGate 医疗质量门槛:> 0.7 → 健康险保费 −10%。
	MedPremiumQualityGate = 0.7
	// MedPremiumFactor 保费因子(0.9)。
	MedPremiumFactor = 0.9
	// CatRegulatoryFine 监管罚款 category(seat → gov;to=EntityGov 无
	// category 白名单限制,ledger.go 仅限制 from=gov)。
	CatRegulatoryFine = "reg_fine"
)

// PublicServices 公共服务五件套(阶段8;挂 World.PublicSvc)。
type PublicServices struct {
	EduQuality    float64 // 教育质量 0..1(财政教育投入占比累积)
	MedQuality    float64 // 医疗质量 0..1
	PensionLevel  float64 // 养老水平 0..1
	HousingAfford float64 // 住房可负担性 0..1(房价收入比映射)
	Employment    float64 // 就业服务(取 FirmSector.Employment;Labor nil → 0)

	// Edu / Med 公共投入子系统(存量镜像 Treasury.*Capital)。
	Edu *EducationSystem
	Med *HealthcareSystem

	// MonthsRun 累计月步次数(§130 接线验证)。
	MonthsRun int
	// LastStepMonth 最近一次 MonthlyStep 的主钟月份。
	LastStepMonth int
}

// EducationSystem 教育代际效应(挂 PublicServices.Edu)。
type EducationSystem struct {
	PublicEduStock float64 // 公共教育累积投入(万元;镜像 Treasury.EduCapital)

	// accrual 座位 → Cognition 年度累积进度(0.1/年;满 1.0 进位 +1 点)。
	accrual map[int]float64
}

// HealthcareSystem 医疗保障(挂 PublicServices.Med)。
type HealthcareSystem struct {
	PublicMedStock float64 // 公共医疗累积投入(万元;镜像 Treasury.HealthCapital)
}

// NewPublicServices 构造(五指标零值;投入子系统空)。
func NewPublicServices() *PublicServices {
	return &PublicServices{
		Edu: &EducationSystem{accrual: map[int]float64{}},
		Med: &HealthcareSystem{},
	}
}

// MonthlyStep 公共服务月步(settlement ⑨G;零 rand,nil/禁用守卫):
//   - 财政支出占比累积:Treasury.LastMonthExpense > 0 时三质量按
//     FiscalBudget 份额 × PublicSvcQualityGain 递增,clamp 1.0;
//   - 投入存量镜像:EduStock/MedStock = Treasury.*Capital ÷ 1e4(万元);
//   - HousingAfford = clamp(1 − 房价收入比/20, 0, 1)
//     (中位城区房价 ÷ 存活玩家中位月收入 × 12);
//   - Employment = Labor.Employment(注:任务卡所述 SocietyStats.EmploymentRate
//     字段不存在 —— SocietyStats 无就业率字段,就业率唯一权威源是 FirmSector)。
func (ps *PublicServices) MonthlyStep(w *World) {
	if ps == nil || w == nil {
		return
	}
	if ps.Edu == nil {
		ps.Edu = &EducationSystem{accrual: map[int]float64{}}
	}
	if ps.Med == nil {
		ps.Med = &HealthcareSystem{}
	}
	ps.MonthsRun++
	ps.LastStepMonth = w.Month

	// ① 三质量累积(财政在花钱 → 按预算占比 × 0.01/月)。
	if t := w.Treasury; t != nil && t.LastMonthExpense > 0 {
		ps.EduQuality = clamp01(ps.EduQuality + FiscalBudget.Education*PublicSvcQualityGain)
		ps.MedQuality = clamp01(ps.MedQuality + FiscalBudget.Healthcare*PublicSvcQualityGain)
		ps.PensionLevel = clamp01(ps.PensionLevel + FiscalBudget.Pension*PublicSvcQualityGain)
		// 投入存量镜像(万元)。
		ps.Edu.PublicEduStock = float64(t.EduCapital) / 1e4
		ps.Med.PublicMedStock = float64(t.HealthCapital) / 1e4
	}

	// ② 住房可负担性(房价收入比映射)。
	ps.HousingAfford = ps.computeHousingAfford(w)

	// ③ 就业服务(FirmSector 直读)。
	ps.Employment = 0
	if w.Labor != nil {
		ps.Employment = clamp01(w.Labor.Employment)
	}
}

// computeHousingAfford 中位城区房价 ÷ (中位月收入 × 12) = 房价收入比,
// afford = clamp(1 − ratio/20, 0, 1);无玩家/无收入/无市场 → 0。
func (ps *PublicServices) computeHousingAfford(w *World) float64 {
	if w.Market == nil {
		return 0
	}
	prices := make([]float64, 0, DistrictCount)
	for _, d := range DistrictDefs {
		prices = append(prices, float64(w.Market.HousePrice(d.ID)))
	}
	medianPrice := medianOf(prices)

	incomes := make([]float64, 0, len(w.Players))
	for _, p := range w.Players {
		if p == nil || !p.Alive {
			continue
		}
		incomes = append(incomes, float64(p.monthlyIncomeEstimate()))
	}
	medianIncome := medianOf(incomes)
	if medianIncome <= 0 || medianPrice <= 0 {
		return 0
	}
	ratio := medianPrice / (medianIncome * 12)
	return clamp01(1 - ratio/HousingRatioDivisor)
}

// AnnualEduStep 教育代际效应年度步(挂 AnnualAdjust;零 rand):
// 月收入 > 15000 的存活座位累积 0.1/年;满 1.0 且 Cognition < 10 → +1 点
// (K 为整数字段,0.1/年语义 = 10 年 +1;达上限清零不累积)。
func (ps *PublicServices) AnnualEduStep(w *World) {
	if ps == nil || w == nil || ps.Edu == nil {
		return
	}
	for _, p := range w.Players {
		if p == nil || !p.Alive {
			continue
		}
		if p.monthlyIncomeEstimate() <= EduIncomeGateCNY {
			continue
		}
		if p.Cognition >= CognitionCap {
			ps.Edu.accrual[p.Seat] = 0
			continue
		}
		ps.Edu.accrual[p.Seat] += EduCognitionAnnualGain
		// ε 容差:0.1 × 10 浮点累积 = 0.999…9,严格 >= 会漏进位(测试捕捉的真 bug)。
		if ps.Edu.accrual[p.Seat] >= 1.0-1e-9 {
			p.Cognition++
			ps.Edu.accrual[p.Seat] -= 1.0
			if ps.Edu.accrual[p.Seat] < 0 {
				ps.Edu.accrual[p.Seat] = 0
			}
			if p.Cognition > CognitionCap {
				p.Cognition = CognitionCap
			}
		}
	}
}

// MedInjuryFactor 意外受伤概率因子:MedQuality > 0.5 → 0.8(−20%),否则 1.0。
// ⚠️ 阶段8 不接入 insurance.go::rollAccident(避免动既有 rand 分支路径,
// 破坏固定种子回归);view 快照下发 + v2.13 接线(§130:查询函数先行)。
func (ps *PublicServices) MedInjuryFactor() float64 {
	if ps != nil && ps.MedQuality > MedInjuryQualityGate {
		return MedInjuryFactor
	}
	return 1.0
}

// MedPremiumFactor 健康险保费因子:MedQuality > 0.7 → 0.9(−10%),否则 1.0。
// ⚠️ 同上:v2.13 接入 insurance.go 定价路径前,仅 view 快照消费。
func (ps *PublicServices) MedPremiumFactor() float64 {
	if ps != nil && ps.MedQuality > MedPremiumQualityGate {
		return MedPremiumFactor
	}
	return 1.0
}

// medianOf 中位数(n 偶 → 中两点平均;n = 0 → 0)。输入副本排序,不改调用方切片。
func medianOf(vals []float64) float64 {
	n := len(vals)
	if n == 0 {
		return 0
	}
	sorted := append([]float64(nil), vals...)
	sort.Float64s(sorted)
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}

// ─────────────────── 监管聚合(包装 regulators 子包) ───────────────────

// RegulatorBundle 四大监管聚合(挂 World.Regulators;阶段8)。
// 子包 regulators 不 import wealth(防循环);本结构负责 World → 子包入参适配:
//   - 证监会:Ledger 当月 CatBuy/CatSell → regulators.TradeRecord;罚款走
//     w.Pay(seat → gov, CatRegulatoryFine),实收按座位现金钳 min(fine, max(cash,0));
//   - 反垄断:SupplyChain → regulators.CapacityView(产能读 + 分拆写回);
//   - 消协:Players → regulators.ConsumerRow(living 口径 Doodad 支出);
//   - 隐私:月度例行审计计数(检查函数见 regulators/data_privacy.go)。
type RegulatorBundle struct {
	Securities *regulators.SecuritiesRegulator
	Antitrust  *regulators.AntitrustRegulator
	Consumer   *regulators.ConsumerRegulator
	Privacy    *regulators.PrivacyRegulator

	FinesTotalCNY     int64 // 累计实收罚款(4 监管合计;现金钳后口径)
	LastMonthFinesCNY int64 // 上月实收罚款
	LastMonthWarnings int   // 上月过度消费警告数
	// LastStepMonth 最近一次 MonthlyStep 的主钟月份(§130 接线验证)。
	LastStepMonth int
}

// NewRegulatorBundle 构造(4 实例齐备)。
func NewRegulatorBundle() *RegulatorBundle {
	return &RegulatorBundle{
		Securities: regulators.NewSecuritiesRegulator(),
		Antitrust:  regulators.NewAntitrustRegulator(),
		Consumer:   regulators.NewConsumerRegulator(),
		Privacy:    regulators.NewPrivacyRegulator(),
	}
}

// MonthlyStep 监管月步(settlement ⑨G;零 rand,nil 守卫)。
func (rb *RegulatorBundle) MonthlyStep(w *World) {
	if rb == nil || w == nil {
		return
	}
	rb.LastStepMonth = w.Month
	rb.LastMonthFinesCNY = 0
	rb.LastMonthWarnings = 0

	// ① 证监会:当月证券类交易 → 内幕检测。
	rb.stepSecurities(w)
	// ② 反垄断:产能市占检查(15 节点,nodeOrder 升序确定性遍历)。
	rb.stepAntitrust(w)
	// ③ 消协:过度消费预警(living 口径 Doodad;座位升序)。
	rb.stepConsumer(w)
	// ④ 隐私:月度例行审计(计数)。
	rb.Privacy.MonthlyAudit(w.Month)
}

// stepSecurities 证监会月步:扫描当月 Ledger 的 seat CatBuy(From=座位)与
// CatSell(To=座位)→ TradeRecord;罚款实收 = min(应罚, 座位现金下限 0)。
func (rb *RegulatorBundle) stepSecurities(w *World) {
	if rb.Securities == nil {
		rb.Securities = regulators.NewSecuritiesRegulator()
	}
	var records []regulators.TradeRecord
	for _, e := range w.Ledger.MonthEntries(w.Month) {
		switch e.Category {
		case CatBuy:
			if seat, ok := IsSeatEntity(e.From); ok {
				records = append(records, regulators.TradeRecord{
					Seat: seat, Month: w.Month, Side: regulators.TradeBuy, AmountCNY: e.AmountCNY,
				})
			}
		case CatSell:
			if seat, ok := IsSeatEntity(e.To); ok {
				records = append(records, regulators.TradeRecord{
					Seat: seat, Month: w.Month, Side: regulators.TradeSell, AmountCNY: e.AmountCNY,
				})
			}
		}
	}
	fines := rb.Securities.MonthlyStep(w.Month, records, func(seat int) int64 {
		if p := w.Players[seat]; p != nil {
			return p.Monthly.Income
		}
		return 0
	})
	for _, f := range fines {
		p := w.Players[f.Seat]
		if p == nil {
			continue
		}
		collect := f.FineCNY
		if p.Cash < collect {
			collect = max64(p.Cash, 0) // 现金不足:实收钳到可收部分,不强扣为负
		}
		if collect <= 0 {
			continue
		}
		w.Pay(f.Seat, SeatEntity(f.Seat), EntityGov, collect, CatRegulatoryFine,
			fmt.Sprintf("证监会·内幕交易罚款(反向 ¥%d)", f.BaseCNY))
		rb.FinesTotalCNY += collect
		rb.LastMonthFinesCNY += collect
		w.emitEvent("policy", f.Seat, fmt.Sprintf(
			"证监会认定 %d 号位大额交易后短期反向操作,罚款 ¥%d(计罚基数 ¥%d)",
			f.Seat, collect, f.BaseCNY))
	}
}

// supplyCapacityView SupplyChain → regulators.CapacitySource 适配器
// (同包直读 Nodes/nodeOrder;TotalCapacity 按 nodeOrder 求和保证浮点确定性)。
type supplyCapacityView struct{ sc *SupplyChain }

func (v supplyCapacityView) CapacityOf(id string) float64 {
	if n := v.sc.Nodes[id]; n != nil {
		return n.Capacity
	}
	return 0
}

func (v supplyCapacityView) TotalCapacity() float64 {
	var total float64
	for _, id := range v.sc.nodeOrder {
		if n := v.sc.Nodes[id]; n != nil {
			total += n.Capacity
		}
	}
	return total
}

func (v supplyCapacityView) SetCapacity(id string, capacity float64) {
	if n := v.sc.Nodes[id]; n != nil && capacity > 0 {
		n.Capacity = capacity
	}
}

// stepAntitrust 反垄断月步:分拆裁决 → 写回 Capacity + 事件播报。
func (rb *RegulatorBundle) stepAntitrust(w *World) {
	if rb.Antitrust == nil {
		rb.Antitrust = regulators.NewAntitrustRegulator()
	}
	if w.SupplyChain == nil || len(w.SupplyChain.nodeOrder) == 0 {
		return
	}
	ids := append([]string(nil), w.SupplyChain.nodeOrder...)
	sort.Strings(ids) // 升序遍历(map/nodeOrder 序之外的确定性双保险)
	actions := rb.Antitrust.MonthlyStep(w.Month, supplyCapacityView{sc: w.SupplyChain}, ids)
	for _, a := range actions {
		w.emitEvent("policy", -1, fmt.Sprintf(
			"反垄断:节点 %s 市占 %.0f%% 连续 %d 月超 40%%,执行分拆(产能 %.1f → %.1f 万/月)",
			a.NodeID, a.MarketShare*100, regulators.AntitrustConsecutiveMonths,
			a.OldCapacity, a.NewCapacity))
	}
}

// stepConsumer 消协月步:Doodad 支出 = 当月生活支出(fiscal_policy.go
// livingExpenseOf 同口径:Monthly.Detail key=living)+ 家庭支出(family),
// 月收入取 Monthly.Income(settlement ③ 已结算)。
func (rb *RegulatorBundle) stepConsumer(w *World) {
	if rb.Consumer == nil {
		rb.Consumer = regulators.NewConsumerRegulator()
	}
	var rows []regulators.ConsumerRow
	for _, p := range w.Players {
		if p == nil || !p.Alive {
			continue
		}
		doodad := livingExpenseOf(p) + familyExpenseOf(p)
		rows = append(rows, regulators.ConsumerRow{
			Seat: p.Seat, MonthlyIncomeCNY: p.Monthly.Income, DoodadExpenseCNY: doodad,
		})
	}
	warnings := rb.Consumer.MonthlyStep(w.Month, rows)
	rb.LastMonthWarnings = len(warnings)
	for _, warn := range warnings {
		w.emitEvent("policy", warn.Seat, fmt.Sprintf(
			"消协提醒:%d 号位本月消费支出 ¥%d 超过月收入 ¥%d 的 50%%,谨防过度消费",
			warn.Seat, warn.DoodadCNY, warn.IncomeCNY))
	}
}

// familyExpenseOf 当月家庭支出(Monthly.Detail key=family;与 livingExpenseOf 同构)。
func familyExpenseOf(p *Player) int64 {
	var sum int64
	for _, it := range p.Monthly.Detail {
		if it.Key == "family" && it.AmountCNY < 0 {
			sum += -it.AmountCNY
		}
	}
	return sum
}
