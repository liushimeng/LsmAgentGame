// Package virtual_city — balance_v2.go: P0 经济平衡 v2「只救命」常量与纯函数
// (批次74 第一步,2026-10-22 §74;批次75 第二步「三阶段破产」B75-1 扩展)。
//
// 权威契约:lag_docs/虚拟城市/已实现/02-架构设计/
// 虚拟城市-P0经济平衡修复与蒙特卡洛验收方案-v1.md §3.2/§3.3/§3.4/§3.6,
// 批次74 口径见 lag_docs/虚拟城市/已实现/74-P0经济平衡v2第一步/01-方案设计.md §3,
// 批次75 口径见 lag_docs/虚拟城市/已实现/75-P0经济平衡v2第二步-三阶段破产/01-方案设计.md §3。
//
// 批次74 五项杠杆(全部由 World.BalanceV2Enabled 总开关控制,false = 批次73 旧口径
// 逐字段一致):
//  1. 生效初始现金 = ceil_1000(max(card.Savings, StartCashMonths × SurvivalCore));
//  2. 基础住房租金 = min(Market.HouseRent, clamp(BasicRentRate × 家庭常规收入,
//     1,800, 12,000));
//  3. 新手默认节俭档(M1–FrugalMonths / 储备 < 6×SurvivalCore / 失业期);
//  4. 失业救济金 v2 = clamp(0.5 × SalaryBase, 1,800, 8,000),取消等待期与衰减;
//  5. 失业 hazard v2 = 0.8% / 3% / 5%,乘 Labor 倍率与认知守卫后封顶 8%。
//
// 批次75 三阶段破产(B75-1)新增:GraceFloor / H1 时长 / R1 分期 / 信用分上下限 /
// 房贷展期 / 自住房保留 + graceFloor / hardshipDuration / reorgPeriods /
// predictedBaseCashFlow 纯函数;状态机与清算动作在 bankruptcy_v2.go。
//
// 硬纪律:
//   - **零新增 rand 消费点**(只改阈值/公式/状态,随机序列形态不变);
//   - 常量集中本文件;不加 LsmAgentGame.conf 配置项;
//   - 所有现金变动仍走 World.Pay(本文件全部为纯计算,不动账)。
package virtual_city

// ── 杠杆常量(方案 §3.1:先落常量,配置化归后续批次)──────────────────

const (
	// BasicRentDefaultRate 基础租占家庭常规月收入比(敏感度 0.20/0.25/0.30)。
	BasicRentDefaultRate = 0.25
	// BasicRentFloorCNY 基础租下限(元/月)。
	BasicRentFloorCNY int64 = 1800
	// BasicRentCapCNY 基础租上限(元/月)。
	BasicRentCapCNY int64 = 12000
	// StartCashMonthsDefault 生效初始现金 = 生存核心的月数(敏感度 3/6/9)。
	StartCashMonthsDefault = 6
	// FrugalMonthsDefault 新手节俭档强制月数(敏感度 3/6/12)。
	FrugalMonthsDefault = 6
	// SurvivalExpenseRate 生存核心中的生活支出系数(节俭档 ×0.6)。
	SurvivalExpenseRate = 0.6
	// FrugalReserveMonths 解除节俭档所需的生存核心储备月数。
	FrugalReserveMonths int64 = 6

	// UnemploymentBenefitRateV2 失业救济金替代率(裁员前月薪的 50%)。
	UnemploymentBenefitRateV2 = 0.5
	// UnemploymentBenefitFloorCNY 失业救济金下限(元/月)。
	UnemploymentBenefitFloorCNY int64 = 1800
	// UnemploymentBenefitCapCNY 失业救济金上限(元/月)。
	UnemploymentBenefitCapCNY int64 = 8000

	// UnemploymentHazardNormalV2 复苏/繁荣/其它相位的月度失业 hazard。
	UnemploymentHazardNormalV2 = 0.008
	// UnemploymentHazardRecessionV2 衰退相位月度失业 hazard。
	UnemploymentHazardRecessionV2 = 0.03
	// UnemploymentHazardDepressionV2 萧条相位月度失业 hazard。
	UnemploymentHazardDepressionV2 = 0.05
	// UnemploymentHazardCapV2 单月最终失业概率封顶(乘 Labor 倍率与认知守卫后)。
	UnemploymentHazardCapV2 = 0.08

	// 旧口径 hazard 表(批次73 及之前;**只在 BalanceV2Enabled=false 时读取**,
	// 与旧实现逐位一致)。
	UnemploymentHazardNormalLegacy     = 0.05
	UnemploymentHazardRecessionLegacy  = 0.15
	UnemploymentHazardDepressionLegacy = 0.25
)

// ── 批次75 三阶段破产常量(B75-1,方案 §3.6)────────────────────────────
const (
	// GraceFloorBaseCNY GraceFloor 下限绝对值(元):现金 ≥ −30000 视为流动性困难。
	GraceFloorBaseCNY int64 = 30000
	// GraceFloorCapCNY GraceFloor 上限绝对值(元)。
	GraceFloorCapCNY int64 = 100000

	// HardshipFirstMonths H1 首次时长(月)。
	HardshipFirstMonths = 6
	// HardshipRepeatMonths H1 重复触发时长(月)。
	HardshipRepeatMonths = 3
	// HardshipRepeatWindowMonths H1 重复触发判定窗口(月)。
	HardshipRepeatWindowMonths = 24

	// ReorgFirstPeriods R1 首次重组分期数(期)。
	ReorgFirstPeriods = 60
	// ReorgRepeatPeriods R1 重复重组分期数(期)。
	ReorgRepeatPeriods = 36
	// ReorgRepeatWindowMonths R1 重复重组判定窗口(月)。
	ReorgRepeatWindowMonths = 24

	// CreditScoreFloorV2 重组后信用分下限(不归零)。
	CreditScoreFloorV2 = 400
	// CreditScoreR1CapV2 重组后信用分上限(按时还款回补封顶)。
	CreditScoreR1CapV2 = 550
	// CreditScoreOnTimeStepV2 每 6 月按时还款的信用分回补步长。
	CreditScoreOnTimeStepV2 = 10
	// CreditScoreOnTimeEveryV2 按时还款回补周期(月)。
	CreditScoreOnTimeEveryV2 = 6

	// MortgageExtensionMonthsV2 H1 房贷展期月数(一次性)。
	MortgageExtensionMonthsV2 = 12
	// MortgagePaymentIncomeRatioV2 H1 房贷展期前提:月供 ≤ 家庭常规收入比。
	MortgagePaymentIncomeRatioV2 = 0.35
	// SelfOccupiedRetentionRatioV2 R1 保留自住房条件:房贷+物业+基础生活 ≤ 常规收入比。
	SelfOccupiedRetentionRatioV2 = 0.55
	// SelfOccupiedReserveMonthsV2 R1 保留自住房需保留的生存金月数。
	SelfOccupiedReserveMonthsV2 = 3
)

// ── 批次75 B75-2 家庭支出口径常量(方案 §3.7)──────────────────────────
//
// 事实基础:profession.Card.Expense(=卡面 monthly_expense)= **家庭总生活消费**
// (含初始配偶/子女/赡养)。批次74 方案 §2.3 已用 4000 张真实卡实证:
// monthly_expense/income 随子女数上升(1 孩 0.541 → 2 孩 0.685 → 3 孩 0.699),
// 与 2025 数据锚点「消费倾向 68%」同口径。
//
// ⇒ 引擎**不得**在 living(=Card.Expense × infl × 档位乘数)之上再叠加
// 「配偶 2,000 + 每孩 5,000 + 赡养 1,000/位」(**重复计入**;批次74 实测
// 已婚有孩卡 median 总生活支出/收入 = 1.16)。
//
// v2 口径(BalanceV2Enabled=true):
//   - 步骤5 家庭加项只计**游戏内新增成员**(新配偶/新生儿);
//   - 恩格尔切分改为按成员构成权重**比例切分**(金额守恒)。
// false(BalanceV2Enabled=false)⇒ 旧口径常量叠加原样保留(批次74 逐字段一致)。
const (
	// FamilyWeightOwn 本人在家庭消费切分中的权重(基准 1.0 ≡ 2000 元档)。
	FamilyWeightOwn = 1.0
	// FamilyWeightSpouse 配偶权重(1.0 ≡ 2000 元/月档)。
	FamilyWeightSpouse = 1.0
	// FamilyWeightChild 每孩权重(2.5 ≡ 5000/2000,沿用旧常数比例)。
	FamilyWeightChild = 2.5
	// FamilyWeightElder 每位赡养老人权重(0.5 ≡ 1000/2000)。
	FamilyWeightElder = 0.5
)

// ── 参数读取(nil/零值安全;零值 World 视为默认值)────────────────────

// basicRentRate 基础租占比(<=0 ⇒ 默认 0.25;MCOverride.BasicRentRate 落点)。
func (w *World) basicRentRate() float64 {
	if w == nil || w.BasicRentRate <= 0 {
		return BasicRentDefaultRate
	}
	return w.BasicRentRate
}

// startCashMonths 生效初始现金月数(<=0 ⇒ 默认 6;MCOverride.StartCashMonths)。
func (w *World) startCashMonths() int {
	if w == nil || w.StartCashMonths <= 0 {
		return StartCashMonthsDefault
	}
	return w.StartCashMonths
}

// frugalMonths 新手节俭档强制月数(<=0 ⇒ 默认 6;MCOverride.FrugalMonths)。
func (w *World) frugalMonths() int {
	if w == nil || w.FrugalMonths <= 0 {
		return FrugalMonthsDefault
	}
	return w.FrugalMonths
}

// ── 家庭常规收入 / 生存核心 ──────────────────────────────────────────

// householdRegularGrossIncome 家庭常规月总收入 = 本人月薪基数 + 配偶收入。
// 失业期 SalaryBase 不归零(advanceUnemployment 只在再就业时改基数)⇒
// 天然满足「失业期参照裁员前常规收入」(方案 §3.2)。
func householdRegularGrossIncome(p *Player) int64 {
	if p == nil {
		return 0
	}
	return p.SalaryBase + p.Family.SpouseIncome
}

// familyLivingAddV2 游戏内新增成员的月生活支出加项(v2 口径,方案 §3.7)。
//
// **只计**:
//   - 游戏内新配偶:结婚事件(events.go)置 Marital="married" 且 SpouseIncome=6000;
//     卡面初始已婚者 SpouseIncome=0 ⇒ 天然可区分;
//   - 游戏内新生儿:生育事件(events.go)向 p.BirthMonths 追加一条(每次生育一条)。
//
// **不加**:初始卡面配偶 / 初始子女(Card.ChildrenCount)/ 赡养老人
// (Card.EldersDependent)—— 三者已含在卡面 monthly_expense(=Card.Expense)内,
// 再加即**重复计入**(B75-2 修正对象)。
//
// 纯计算(零 rand);BalanceV2Enabled=false 路径不调用本函数(旧口径常量叠加)。
func familyLivingAddV2(p *Player) int64 {
	if p == nil {
		return 0
	}
	var add int64
	if p.Family.Marital == "married" && p.Family.SpouseIncome > 0 {
		add += livingSpouseCNY
	}
	add += int64(len(p.BirthMonths)) * livingChildCNY
	return add
}

// basicRent 租房者的基础住房月租(方案 §3.3.1):
//
//	min(Market.HouseRent(district), clamp(BasicRentRate × 家庭常规收入, 1800, 12000))
//
// 随当月市场价**动态**计算(不写死);只作用于租房支出,不改变投资房租金
// 收入(步骤 2 仍走 Market.HouseRent/ShopRent)、不改变资产市价与税费。
func (w *World) basicRent(p *Player) int64 {
	if w == nil || p == nil || w.Market == nil {
		return 0
	}
	capRent := int64(float64(w.basicRentRate())*float64(householdRegularGrossIncome(p)) + 0.5)
	if capRent < BasicRentFloorCNY {
		capRent = BasicRentFloorCNY
	}
	if capRent > BasicRentCapCNY {
		capRent = BasicRentCapCNY
	}
	market := w.Market.HouseRent(p.HomeDistrict)
	if market < capRent {
		return market
	}
	return capRent
}

// survivalCoreM1 单月生存核心 = 基础租 + 0.6 × 卡面生活支出(方案 §3.2)。
func (w *World) survivalCoreM1(p *Player) int64 {
	if w == nil || p == nil {
		return 0
	}
	return w.basicRent(p) + int64(float64(p.Card.Expense)*SurvivalExpenseRate+0.5)
}

// effectiveStartCash 生效初始现金 = ceil_1000(max(card.Savings, 月数 × SurvivalCore))。
// 只抬不削(高储蓄卡原值仅向上取整到千元,保留阶层差异)。
func (w *World) effectiveStartCash(p *Player) int64 {
	if w == nil || p == nil {
		return 0
	}
	base := p.Card.Savings
	if floor := int64(w.startCashMonths()) * w.survivalCoreM1(p); floor > base {
		base = floor
	}
	return ceilTo1000(base)
}

// ceilTo1000 向上取整到千元(v<=0 ⇒ 0;整数运算,无浮点误差)。
func ceilTo1000(v int64) int64 {
	if v <= 0 {
		return 0
	}
	return ((v + 999) / 1000) * 1000
}

// liquidReserve 流动性储备 = 现金 + 可免费赎回债券面值(方案 §3.4 条件②)。
// 标准债券按面值 1:1 免费赎回(actions.go ActSellAsset bond 分支)⇒ Units
// 即面值(元);其它资产(股票/房产/商铺)不计入。
func liquidReserve(p *Player) int64 {
	if p == nil {
		return 0
	}
	total := p.Cash
	for i := range p.Assets {
		a := &p.Assets[i]
		if a.Kind == AssetBond && a.Units > 0 {
			total += int64(a.Units + 0.5)
		}
	}
	return total
}

// forcedFrugal 引擎是否强制节俭档(方案 §3.4):
//  1. w.Month ≤ FrugalMonths(新手保护期);
//  2. 流动性储备 < 6 × SurvivalCore;
//  3. 失业期;
//  4. 批次75:困难期(H1)/重组观察期(R1) —— 方案 §3.3.2 条件③。
func (w *World) forcedFrugal(p *Player) bool {
	if w == nil || p == nil {
		return false
	}
	if w.Month <= w.frugalMonths() {
		return true
	}
	if p.UnemployedMonths > 0 {
		return true
	}
	if w.BalanceV2Enabled && (p.InHardship || p.InR1) {
		return true
	}
	return liquidReserve(p) < FrugalReserveMonths*w.survivalCoreM1(p)
}

// ── 批次75 三阶段破产纯函数(方案 §3.6)──────────────────────────────

// graceFloor 现金宽限下限(方案 §3.6.1):
//
//	GraceFloor(p) = −min(max(30000, 3 × SurvivalCoreM1(p)), 100000)
//
// 月末现金 ≥ GraceFloor 且 < 0 ⇒ 流动性困难(H1);< GraceFloor ⇒ 直接 R1。
// 纯计算(零 rand)。
func (w *World) graceFloor(p *Player) int64 {
	if w == nil || p == nil {
		return -GraceFloorBaseCNY
	}
	core := 3 * w.survivalCoreM1(p)
	if core < GraceFloorBaseCNY {
		core = GraceFloorBaseCNY
	}
	if core > GraceFloorCapCNY {
		core = GraceFloorCapCNY
	}
	return -core
}

// hardshipDuration H1 时长(方案 §3.6.1):首次 6 月;上次 H1 结束 24 月内重复 ⇒ 3 月。
func (w *World) hardshipDuration(p *Player) int {
	if p == nil {
		return HardshipFirstMonths
	}
	if p.HardshipCount > 0 && w != nil &&
		w.Month-p.LastHardshipEndMonth <= HardshipRepeatWindowMonths {
		return HardshipRepeatMonths
	}
	return HardshipFirstMonths
}

// reorgPeriods R1 重组分期数(方案 §3.6.3):首次 60 期;上次 R1 结束 24 月内重复 ⇒ 36 期。
func (w *World) reorgPeriods(p *Player) int {
	if p == nil {
		return ReorgFirstPeriods
	}
	if p.LastR1EndMonth > 0 && w != nil &&
		w.Month-p.LastR1EndMonth <= ReorgRepeatWindowMonths {
		return ReorgRepeatPeriods
	}
	return ReorgFirstPeriods
}

// hardshipIncome 常规收入(工资 + 配偶 + 被动),用于 H1「无法付息」判定。
// 纯计算(零 rand)。
func (w *World) hardshipIncome(p *Player) int64 {
	if w == nil || p == nil {
		return 0
	}
	var inc int64
	if p.UnemployedMonths <= 0 {
		inc += p.SalaryBase
	}
	inc += p.Family.SpouseIncome
	inc += p.MonthlyPassiveIncome(w.Market, w.Age())
	return inc
}

// predictedBaseCashFlow 预测基础月现金流(方案 §3.6.1 B1 判据;纯计算零 rand):
//
//	= 常规收入(工资/配偶/副业基准/被动/养老金) − 固定支出(租/物业/生活/家庭/
//	  税/社保/保险/贷款月供)
//
// H1 内消费贷/信用贷按只付息口径计息(本金延期);其余贷款按当前月供。
// 副业取档位基准(不含竞争份额与方差),以保证确定性。
func (w *World) predictedBaseCashFlow(p *Player) int64 {
	if w == nil || p == nil {
		return 0
	}
	var inflow, outflow int64
	salary := int64(0)
	if p.UnemployedMonths <= 0 {
		salary = p.SalaryBase
		inflow += p.SalaryBase
	}
	if p.Family.Marital == "married" && p.Family.SpouseIncome > 0 {
		inflow += p.Family.SpouseIncome
	}
	if p.SideBusiness != nil && p.Energy >= 0 {
		inflow += int64(float64(p.SideBusiness.BaseIncome)*
			sideTierMultiplier(p.SideBusiness.PriceTier)*p.BrassSideFactor() + 0.5)
	}
	inflow += p.MonthlyPassiveIncome(w.Market, w.Age())

	if p.selfOccupiedHouse() == nil {
		outflow += w.basicRent(p)
	}
	if p.ownsProperty() {
		outflow += propertyFeeCNY
	}
	infl := InflationFactorCB(w)
	level := p.ConsumptionLevelSafe()
	outflow += int64(float64(p.Card.Expense)*infl*consumptionLevelMult[level] + 0.5)
	// 批次75 B75-2(v2 口径):与 settlement 步骤5 **同口径** —— 家庭加项只计
	// 游戏内新增成员(卡面初始成员已含在 Card.Expense 内);否则预测现金流与
	// 实际月结口径不一致,会系统性高估流出 ⇒ 误判 B1。
	outflow += familyLivingAddV2(p)
	outflow += MonthlyIncomeTax(salary) + SocialSecurity(salary)
	for _, kind := range insuranceKindOrder {
		if pol := p.Policies[kind]; pol != nil && pol.Active {
			outflow += monthlyPremium(pol.AnnualPremiumCNY)
		}
	}
	for i := range p.Loans {
		loan := &p.Loans[i]
		if w.hardshipLoan(p) && isConsumerOrCredit(loan.Kind) {
			outflow += int64(float64(loan.Balance)*loan.AnnualRate/12 + 0.5)
			continue
		}
		outflow += loan.MonthlyPayment
	}
	return inflow - outflow
}

// ── 失业保险 v2(方案 §3.4.2)─────────────────────────────────────────

// unemploymentBenefitV2 失业救济金 = clamp(0.5 × SalaryBase, 1800, 8000)。
// 无等待期、无 >12 月衰减(单次失业 ≤ U(2,6) 月,天然满足「最长 6 个月」)。
// ReferenceGross 取 p.SalaryBase:失业期不减(再就业才改写基数)⇒ 等价于
// 「裁员前 12 个月常规月薪」。
func unemploymentBenefitV2(p *Player) int64 {
	if p == nil {
		return 0
	}
	amount := int64(float64(p.SalaryBase)*UnemploymentBenefitRateV2 + 0.5)
	if amount < UnemploymentBenefitFloorCNY {
		amount = UnemploymentBenefitFloorCNY
	}
	if amount > UnemploymentBenefitCapCNY {
		amount = UnemploymentBenefitCapCNY
	}
	return amount
}

// ── 失业 hazard v2(方案 §3.4.1)──────────────────────────────────────

// unemploymentProbability 单月最终失业概率(方案 §3.4.1):
//
//	base(phase) × Labor 倍率 × negativeGuard(cognition),v2 再封顶 8%
//
// 纯计算(零 rand):MonthlyEvents 只消费一次该值作为抽样阈值。
func (w *World) unemploymentProbability(p *Player) float64 {
	if p == nil {
		return 0
	}
	factor := 1.0
	if w != nil && w.EconomyEnabled && w.Labor != nil {
		factor = clampF(w.Labor.Unemployment/UnemployNatural, 0.5, 2.5)
	}
	probe := w.unemploymentHazardBase() * factor * negativeGuard(p.Cognition)
	if w != nil && w.BalanceV2Enabled && probe > UnemploymentHazardCapV2 {
		probe = UnemploymentHazardCapV2
	}
	return probe
}

// unemploymentHazardBase 月度失业 hazard 基准(按市场相位)。
// BalanceV2Enabled=true ⇒ 0.8% / 3% / 5%;false ⇒ 旧表 5% / 15% / 25%。
func (w *World) unemploymentHazardBase() float64 {
	phase := PhaseRecovery
	if w != nil && w.Market != nil {
		phase = w.Market.CyclePhase
	}
	v2 := w != nil && w.BalanceV2Enabled
	switch phase {
	case PhaseRecession:
		if v2 {
			return UnemploymentHazardRecessionV2
		}
		return UnemploymentHazardRecessionLegacy
	case PhaseDepression:
		if v2 {
			return UnemploymentHazardDepressionV2
		}
		return UnemploymentHazardDepressionLegacy
	default:
		if v2 {
			return UnemploymentHazardNormalV2
		}
		return UnemploymentHazardNormalLegacy
	}
}
