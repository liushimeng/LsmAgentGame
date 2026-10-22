// Package virtual_city — balance_v2.go: P0 经济平衡 v2 第一步「只救命」
// (批次74,2026-10-22 §74)。
//
// 权威契约:lag_docs/虚拟城市/已实现/02-架构设计/
// 虚拟城市-P0经济平衡修复与蒙特卡洛验收方案-v1.md §3.2/§3.3/§3.4,
// 本批实施口径见 lag_docs/虚拟城市/已实现/74-P0经济平衡v2第一步/01-方案设计.md §3。
//
// 五项杠杆(全部由 World.BalanceV2Enabled 总开关控制,false = 批次73 旧口径
// 逐字段一致):
//  1. 生效初始现金 = ceil_1000(max(card.Savings, StartCashMonths × SurvivalCore));
//  2. 基础住房租金 = min(Market.HouseRent, clamp(BasicRentRate × 家庭常规收入,
//     1,800, 12,000));
//  3. 新手默认节俭档(M1–FrugalMonths / 储备 < 6×SurvivalCore / 失业期);
//  4. 失业救济金 v2 = clamp(0.5 × SalaryBase, 1,800, 8,000),取消等待期与衰减;
//  5. 失业 hazard v2 = 0.8% / 3% / 5%,乘 Labor 倍率与认知守卫后封顶 8%。
//
// 硬纪律:
//   - **零新增 rand 消费点**(只改阈值/公式,随机序列形态不变);
//   - 常量集中本文件(settlement.go 已 1052 行);不加 LsmAgentGame.conf 配置项;
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
//  3. 失业期(破产宽限期/重组观察期归批次 75)。
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
	return liquidReserve(p) < FrugalReserveMonths*w.survivalCoreM1(p)
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
