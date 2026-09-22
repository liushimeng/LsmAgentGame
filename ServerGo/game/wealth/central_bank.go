// Package wealth — central_bank.go: 央行-商业银行信用创造与货币传导引擎
// (2026-09-16 §财商流P1,设计文档: lag_docs/虚拟城市/已实现/02-架构设计/
// 虚拟城市-央行信用创造引擎-v1.md)。
//
// 纯引擎层:无锁、无 goroutine、无 IO;所有随机性经注入的 *rand.Rand 驱动。
// 传导链:央行货币政策 → 商业银行信用创造(部分准备金 → 货币乘数)
// → M2 增长 → 通胀传导(MV=PY) → 利率传导(LPR) → 信贷约束 → 影响每个 Agent。
package wealth

import (
	"fmt"
	"math"
	"math/rand"
)

// 央行初始参数(设计文档 §2.1)。
const (
	InitialPolicyRate   = 0.03  // 政策利率(1Y 基准)
	InitialReserveRatio  = 0.10  // 准备金率
	InitialGovBondsHeld  = 200000.0 // 央行国债持有
	InitialLoansToBanks  = 350000.0 // 央行再贴现贷款
	InitialM0            = 500000.0 // 初始流通中现金
	InitialM2            = 500000.0 // 初始 M2
	InitialBaseMoney     = 550000.0 // 初始基础货币 MB = M0 + Reserves
)

// 利率传导系数(设计文档 §2.2)。
const (
	TermPremium       = 0.005 // 期限溢价(1Y→5Y)
	CreditSpreadMax   = 0.02  // 信贷紧缩加点上限
	MortgageSpread    = 0.005 // 房贷加点
	BusinessSpread    = 0.02  // 经营贷加点
	ConsumerRate      = 0.10  // 消费贷固定利率
	CreditScoreBonus  = 50    // 信贷约束时信用分门槛提高
)

// 通胀传导系数(设计文档 §2.3)。
const (
	TargetCPI  = 0.02 // 目标通胀
	KCPI       = 0.5  // 通胀传导系数
	CPIMin     = 0.0  // CPI 下限
	CPIMax     = 0.05 // CPI 上限
	GDPGrowth  = 0.05 // GDP 增速基线(recovery 阶段)
)

// 信贷约束阈值(设计文档 §2.4)。
const (
	TargetUtil    = 0.85 // 目标乘数利用率
	LoanQuotaMin  = 0.5  // 额度收紧系数下限
	CreditTightMax = 1.0 // 信贷约束上限
)

// 公开市场操作常量(设计文档 §11.2 P2 扩展,P1 预留)。
const (
	OMOMaxAmount = 50000.0 // 公开市场操作最大吞吐 ±5万/月
)

// CentralBankState 央行-商业银行体系月度状态(纯引擎,无锁)。
// 所有金额单位 CNY;利率为小数(0.03 = 3%)。
type CentralBankState struct {
	// ── 货币三层次(每月汇总自玩家状态) ──
	M0 float64 // 流通中现金 = Σ玩家 Cash
	M1 float64 // = M0 + 活期存款(游戏简化:活期=现金,故 M1=M0)
	M2 float64 // = M1 + 定期存款(玩家 SavingsDeposit)

	// ── 货币政策工具 ──
	PolicyRate     float64 // 央行基准利率(1Y,类似 MLF)
	ReserveRatio   float64 // 准备金率 r
	RediscountRate float64 // 再贴现率 = PolicyRate + 0.5%

	// ── 货币乘数 ──
	MoneyMultiplier   float64 // 实际乘数 = M2 / MB
	MultiplierCeiling float64 // 上限 = 1 / ReserveRatio
	BaseMoney         float64 // 基础货币 MB = M0 + Reserves

	// ── 信贷约束 ──
	CreditTightness float64 // [0,1],越高越紧
	LoanQuotaFactor float64 // [0.5,1],贷款额度乘数
	CreditSpread    float64 // [0,2%],LPR 上浮

	// ── 央行资产负债表 ──
	GovBondsHeld  float64 // 资产:持有国债
	LoansToBanks  float64 // 资产:对商业银行再贴现贷款
	Reserves      float64 // 负债:商业银行准备金 = r × M1
	CurrencyIssued float64 // 负债:流通中现金 = M0

	// ── 通胀与产出 ──
	CPI           float64 // 内生 CPI(替代 PhaseTable 硬编码)
	LastCPI       float64 // 上月 CPI(用于 ComputeL5Y 的 P1 5Y LPR 计算,与 CPI 同步更新)
	RealGDPGrowth float64 // 实际 GDP 增速(内生于市场阶段)
	M2GrowthYoY   float64 // M2 同比增速

	// ── v2.12 阶段3:货币政策工具箱扩展字段(policy_toolbox.go /
	// interest_transmission.go 消费;2026-09-21 §城市扩张v2.12)──
	SLFRate            float64 // 利率走廊上限(常备借贷便利)= PolicyRate + 0.5%
	NIM                float64 // 商业银行净息差(行业均值 1.5%)
	CreditWindowFactor float64 // 信贷窗口指导乘数 [0.5,1.5],1.0 中性

	// ── 历史(每月快照,用于 view 展示与复盘) ──
	History []CBMonthlySnapshot
}

// CBMonthlySnapshot 单月央行快照(view 展示用)。
type CBMonthlySnapshot struct {
	Month           int
	M0, M1, M2      float64
	MB              float64
	MoneyMultiplier float64
	PolicyRate      float64
	LPR             float64
	CPI             float64
	CreditTightness float64
}

// CommercialBankState 商业银行体系汇总(纯引擎)。
// 游戏简化:不逐家跟踪,只跟踪全体系汇总。
type CommercialBankState struct {
	DemandDeposits   float64 // 活期存款 = M1
	TimeDeposits     float64 // 定期存款 = M2 - M1
	TotalDeposits    float64 // = DemandDeposits + TimeDeposits
	Reserves         float64 // = r × TotalDeposits
	ExcessReserves   float64 // 超额准备金 = 可贷资金
	LoansOutstanding float64 // 贷款余额 = Σ玩家 Loan.Balance
	BondHoldings     float64 // 银行持有国债(公开市场操作卖出)
}

// NewCentralBank 构造央行初始状态(设计文档 §2.1)。
func NewCentralBank() *CentralBankState {
	cb := &CentralBankState{
		PolicyRate:     InitialPolicyRate,
		ReserveRatio:   InitialReserveRatio,
		RediscountRate: InitialPolicyRate + 0.005,
		GovBondsHeld:   InitialGovBondsHeld,
		LoansToBanks:   InitialLoansToBanks,
		M0:             InitialM0,
		M1:             InitialM0,
		M2:             InitialM2,
		BaseMoney:      InitialBaseMoney,
		MultiplierCeiling: 1.0 / InitialReserveRatio,
	}
	cb.MoneyMultiplier = cb.M2 / cb.BaseMoney
	cb.Reserves = cb.ReserveRatio * cb.M1
	cb.CurrencyIssued = cb.M0
	cb.CPI = TargetCPI
	cb.RealGDPGrowth = GDPGrowth
	cb.CreditTightness = 0
	cb.LoanQuotaFactor = 1.0
	cb.CreditSpread = 0
	// v2.12 阶段3:工具箱扩展字段(policy_toolbox.go / interest_transmission.go)。
	cb.SLFRate = InitialPolicyRate + SLFCorridorWidth
	cb.NIM = DefaultNIM
	cb.CreditWindowFactor = CreditWindowNeutral
	return cb
}

// UpdateMoneyStats 汇总 M0/M1/M2(遍历玩家 Cash + SavingsDeposit)。
func (cb *CentralBankState) UpdateMoneyStats(w *World) {
	if w == nil {
		return
	}
	var m0, m2 float64
	for _, p := range w.Players {
		if p == nil {
			continue
		}
		m0 += float64(p.Cash)
		m2 += float64(p.Cash) + float64(p.SavingsDeposit)
	}
	cb.M0 = m0
	cb.M1 = m0 // 游戏简化:活期=现金
	cb.M2 = m2
	cb.BaseMoney = cb.M0 + cb.Reserves
	if cb.BaseMoney > 0 {
		cb.MoneyMultiplier = cb.M2 / cb.BaseMoney
	}
}

// ComputeCPI 内生 CPI = clamp(TargetCPI + k_cpi × (M2Growth − GDPGrowth), 0, 5%)。
// MV=PY 简化:货币增速 − 产出增速 = 通胀。
func (cb *CentralBankState) ComputeCPI() {
	cpi := TargetCPI + KCPI*(cb.M2GrowthYoY-cb.RealGDPGrowth)
	if cpi < CPIMin {
		cpi = CPIMin
	}
	if cpi > CPIMax {
		cpi = CPIMax
	}
	cb.CPI = cpi
	cb.LastCPI = cpi
}

// ComputeL5Y 计算 5Y LPR = PolicyRate + 期限溢价 + CPI 加点(v2.60 N12-3)。
// CPI 高于目标时 5Y 加点更高(银行对长期通胀风险的补偿)。
func (cb *CentralBankState) ComputeL5Y() float64 {
	base := cb.PolicyRate + TermPremium
	if cb.LastCPI > TargetCPI {
		base += (cb.LastCPI - TargetCPI) * 0.5
	}
	if base < 0 {
		base = 0
	}
	return base
}

// ComputeLPR 内生 LPR = PolicyRate + TermPremium + CreditSpread。
func (cb *CentralBankState) ComputeLPR() float64 {
	lpr := cb.PolicyRate + TermPremium + cb.CreditSpread
	if lpr < 0 {
		lpr = 0
	}
	return lpr
}

// ComputeCreditTightness 信贷约束:CreditTightness = clamp((m/m_max − TargetUtil) / (1 − TargetUtil), 0, 1)。
func (cb *CentralBankState) ComputeCreditTightness() {
	util := cb.MoneyMultiplier / cb.MultiplierCeiling
	tight := (util - TargetUtil) / (1 - TargetUtil)
	if tight < 0 {
		tight = 0
	}
	if tight > CreditTightMax {
		tight = CreditTightMax
	}
	cb.CreditTightness = tight
	cb.LoanQuotaFactor = 1 - tight*0.5
	if cb.LoanQuotaFactor < LoanQuotaMin {
		cb.LoanQuotaFactor = LoanQuotaMin
	}
	cb.CreditSpread = tight * CreditSpreadMax
}

// MonthlyDecision 央行月度决策(逆周期调节 + 公开市场操作)。
// 所有随机性经注入的 rng 驱动。
func (cb *CentralBankState) MonthlyDecision(w *World, rng *rand.Rand) {
	if w == nil {
		return
	}

	// a. 更新基础货币 MB(含上月公开市场操作/再贴现效果)。
	cb.BaseMoney = cb.M0 + cb.Reserves

	// b. 汇总 M0/M1/M2。
	cb.UpdateMoneyStats(w)

	// c. 计算实际货币乘数 m = M2 / MB。
	if cb.BaseMoney > 0 {
		cb.MoneyMultiplier = cb.M2 / cb.BaseMoney
	}

	// d. 计算 M2 同比增速(简化:与上月比较)。
	prevM2 := cb.M2
	if len(cb.History) > 0 {
		prevM2 = cb.History[len(cb.History)-1].M2
	}
	if prevM2 > 0 {
		cb.M2GrowthYoY = (cb.M2 - prevM2) / prevM2
	}
	// prevM2=0 时保留已有 M2GrowthYoY(首月同比增速未知)。

	// e. 内生 CPI:P1(§财商流P1-2 §2.4)在 MV-PY 理论值之上做 50/50 混合 ——
	// 货币供给决定中长期趋势,八大类篮子捕捉短期结构性涨价(猪周期/油价)。
	// ComputeCPI 本体不动;篮子未积累 12 月环比(CPIYoYReady=false)时不混合。
	cb.ComputeCPI()
	if w.Goods != nil && w.EconomyEnabled && w.Goods.CPIYoYReady() {
		blended := 0.5*cb.CPI + 0.5*w.Goods.CPIYoY
		cb.CPI = clampF(blended, CPIMin, CPIMax)
		cb.LastCPI = cb.CPI
	}

	// f. 央行逆周期调节:调整 PolicyRate(通胀↑→加息,产出缺口↓→降息)。
	oldRate := cb.PolicyRate
	if cb.CPI > TargetCPI+0.005 {
		// 通胀高于目标 0.5% → 加息 25bp。
		cb.PolicyRate += 0.0025
	} else if cb.CPI < TargetCPI-0.005 {
		// 通胀低于目标 0.5% → 降息 25bp。
		cb.PolicyRate -= 0.0025
	}
	// 利率下限 0.5%。
	if cb.PolicyRate < 0.005 {
		cb.PolicyRate = 0.005
	}
	cb.RediscountRate = cb.PolicyRate + 0.005

	// g. 内生 LPR。
	cb.ComputeLPR()

	// h. 更新信贷约束。
	cb.ComputeCreditTightness()

	// i. 公开市场操作:根据通胀/产出缺口决定买/卖国债(±5万)。
	// P1 简化:仅当通胀偏离目标时操作,幅度固定 ±5万。
	if rng != nil {
		if cb.CPI > TargetCPI+0.01 {
			// 通胀过高 → 卖出国债回收基础货币。
			amount := math.Min(OMOMaxAmount, cb.GovBondsHeld)
			cb.GovBondsHeld -= amount
		} else if cb.CPI < TargetCPI-0.01 {
			// 通胀过低 → 买入国债投放基础货币。
			cb.GovBondsHeld += OMOMaxAmount
		}
	}

	// j. 更新资产负债表(§8.3 守恒:资产 = 负债)。
	// 负债端:倒挤 Reserves 使 Reserves + CurrencyIssued = GovBondsHeld + LoansToBanks。
	cb.CurrencyIssued = cb.M0
	assets := cb.GovBondsHeld + cb.LoansToBanks
	cb.Reserves = assets - cb.CurrencyIssued
	if cb.Reserves < 0 {
		cb.Reserves = 0
	}
	cb.BaseMoney = cb.M0 + cb.Reserves

	// k. 记录 CBMonthlySnapshot。
	snap := CBMonthlySnapshot{
		Month:           w.Month,
		M0:              cb.M0,
		M1:              cb.M1,
		M2:              cb.M2,
		MB:              cb.BaseMoney,
		MoneyMultiplier: cb.MoneyMultiplier,
		PolicyRate:      cb.PolicyRate,
		LPR:             cb.ComputeLPR(),
		CPI:             cb.CPI,
		CreditTightness: cb.CreditTightness,
	}
	cb.History = append(cb.History, snap)

	// 保留最近 420 月(35 年)。
	if len(cb.History) > 420 {
		cb.History = cb.History[len(cb.History)-420:]
	}

	// 发出政策事件(如果利率有变化)。
	if oldRate != cb.PolicyRate {
		direction := "加息"
		if cb.PolicyRate < oldRate {
			direction = "降息"
		}
		w.emitEvent("policy", -1, fmt.Sprintf("央行%s 25bp,PolicyRate %.2f%%→%.2f%%",
			direction, oldRate*100, cb.PolicyRate*100))
	}

	// l. 季度央行公告(v2.12 阶段3):每 3 月生成一次文本公告追加到事件流。
	// 本阶段为文本拼接占位;完整 LLM 接入在阶段 4 政府财政子系统完成(§197)。
	if w.Month%3 == 0 {
		w.emitEvent("policy", -1, "央行公告: "+GenerateCentralBankStatement(w, w.Month))
	}
}

// LPR5Y 返回 5Y LPR(房贷用)。
func (cb *CentralBankState) LPR5Y() float64 {
	return cb.ComputeLPR()
}

// LPR1Y 返回 1Y LPR(经营贷用)。
func (cb *CentralBankState) LPR1Y() float64 {
	return cb.PolicyRate + cb.CreditSpread
}

// MortgageRate 返回房贷利率 = LPR(5Y) + 0.5% + CreditSpread。
func (cb *CentralBankState) MortgageRate() float64 {
	return cb.LPR5Y() + MortgageSpread + cb.CreditSpread
}

// BusinessRate 返回经营贷利率 = LPR(1Y) + 2.0% + CreditSpread。
func (cb *CentralBankState) BusinessRate() float64 {
	return cb.LPR1Y() + BusinessSpread + cb.CreditSpread
}

// Snapshot 返回央行只读快照(Agent 可见)。
func (cb *CentralBankState) Snapshot() *CentralBankSnapshot {
	if cb == nil {
		return nil
	}
	return &CentralBankSnapshot{
		M0:              cb.M0,
		M1:              cb.M1,
		M2:              cb.M2,
		MB:              cb.BaseMoney,
		MoneyMultiplier: cb.MoneyMultiplier,
		PolicyRate:      cb.PolicyRate,
		LPR:             cb.ComputeLPR(),
		CPI:             cb.CPI,
		CreditTightness: cb.CreditTightness,
		LoanQuotaFactor: cb.LoanQuotaFactor,
	}
}

// CentralBankSnapshot 央行只读快照(Agent 可见)。
type CentralBankSnapshot struct {
	M0, M1, M2      float64
	MB              float64
	MoneyMultiplier float64
	PolicyRate      float64
	LPR             float64
	CPI             float64
	CreditTightness float64
	LoanQuotaFactor float64
}

// BankingSystem 返回商业银行体系汇总(Agent 可见)。
func (cb *CentralBankState) BankingSystem() *CommercialBankState {
	if cb == nil {
		return nil
	}
	demand := cb.M1
	time := cb.M2 - cb.M1
	total := demand + time
	reserves := cb.ReserveRatio * total
	excess := total - reserves
	var loans float64
	// 注意:这里不遍历 World,由调用方传入或从快照获取。
	// P1 简化:贷款余额从 History 最后一条的 M2 推算。
	if len(cb.History) > 0 {
		loans = cb.History[len(cb.History)-1].M2 - cb.M0
	}
	return &CommercialBankState{
		DemandDeposits:   demand,
		TimeDeposits:     time,
		TotalDeposits:    total,
		Reserves:         reserves,
		ExcessReserves:   excess,
		LoansOutstanding: loans,
		BondHoldings:     0, // P1 简化:银行持有国债 = 央行卖出部分
	}
}

// GenerateCentralBankStatement 生成央行公告(季度)(2026-09-21 §城市扩张v2.12)。
// 本函数仅声明 + 占位实现(文本拼接),完整 LLM 接入(AgentClassCityHuman(央行行长角色提示词)
// 央行行长 Bot)在阶段 4 政府财政子系统时完成。
// 注意:LPR1Y/LPR5Y 是方法(非字段),格式化时必须调用。
func GenerateCentralBankStatement(w *World, month int) string {
	if w == nil || w.CB == nil {
		return ""
	}
	return fmt.Sprintf("央行%d月: LPR %.2f%%/%.2f%% CPI %.1f%% 准备金率 %.1f%% 货币乘数 %.1f",
		month, w.CB.LPR1Y()*100, w.CB.LPR5Y()*100, w.CB.CPI*100,
		w.CB.ReserveRatio*100, w.CB.MoneyMultiplier)
}
