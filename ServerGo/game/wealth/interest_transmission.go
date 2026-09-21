// Package wealth — interest_transmission.go: 利率传导 4 步链
// (2026-09-21 §城市扩张v2.12 阶段3)。
//
// 传导链: MLF(政策利率锚) → SHIBOR(银行间) → 商业银行资金成本
// → 零售利率(存款/贷款) → 实体经济(投资/消费信贷/购房意愿)。
// 纯函数(§92a 无锁):只读 World,不改任何状态;供央行行长 Bot(阶段 4)、
// view 层展示与单测消费。
//
// 数值标定(以真实 MLF=2.4% 为例;引擎内 MLF 锚 = World.CB.PolicyRate,
// 初始 3%,公式一致、数值随引擎状态浮动):
//
//	SHIBOR 1Y  = MLF + 0.1%(基准) + 0.2%(流动性溢价)  → 2.4% 基准下 = 2.7%
//	资金成本    = SHIBOR + 信贷紧迫加点(基准情形 0)
//	存款利率    = 资金成本 − NIM/2                      → 2.7% − 0.75% = 1.95%
//	零售贷款    = 资金成本 + 1% 风险溢价
//	房贷利率    = 5Y LPR + MortgageSpread + CreditSpread(central_bank.go 已有)
//	消费贷利率  = 1Y LPR + ConsumerSpread + CreditSpread(本文件新定)
package wealth

import "fmt"

// 利率传导系数常量(v2.12 阶段3 新定)。
const (
	// SHIBORBaseSpread MLF→SHIBOR 基准加点 0.1%。
	SHIBORBaseSpread = 0.001
	// LiquidityPremium 流动性溢价 0.2%。
	LiquidityPremium = 0.002
	// InterbankStressSpread 信贷紧迫时银行间资金加点上限 0.2%(×CreditTightness)。
	InterbankStressSpread = 0.002
	// DefaultNIM 商业银行净息差行业均值 1.5%(World.CB.NIM 缺省)。
	DefaultNIM = 0.015
	// RetailRiskPremium 零售贷款风险溢价 1%(资金成本之上)。
	RetailRiskPremium = 0.01
	// ConsumerSpread 消费贷 LPR 加点 2.5%(区别于 P0 固定 ConsumerRate=10%)。
	ConsumerSpread = 0.025
)

// 零售→宏观的需求敏感度(每 +1% 有效借贷利率的意愿降幅)。
const (
	HousingDemandSensitivity    = 0.08 // 购房意愿 −8%/+1%
	ConsumerCreditSensitivity   = 0.06 // 消费信贷需求 −6%/+1%
	InvestmentDemandSensitivity = 0.05 // 企业投资意愿 −5%/+1%
)

// 传导步骤名(TransmissionStep.Name 取值)。
const (
	StepCentralToInterbank    = "MLF→SHIBOR"
	StepInterbankToCommercial = "SHIBOR→资金成本"
	StepCommercialToRetail    = "资金成本→零售利率"
	StepRetailToMacro         = "零售利率→实体经济"
)

// TransmissionStep 利率传导单步结果(TraceTransmission 元素)。
//
// 命名说明:任务规格中单步函数名与切片元素类型同名冲突,Go 不允许;
// 故类型保留 TransmissionStep(进入 TraceTransmission 导出签名),
// 单步派发函数命名为 RunTransmissionStep(idx 显式指定第几步)。
type TransmissionStep struct {
	Index       int     // 0..3
	Name        string  // 步骤名(Step* 常量)
	InRate      float64 // 输入利率(小数)
	OutRate     float64 // 输出利率(小数;retail 步为零售贷款利率)
	DepositRate float64 // 仅 retail 步填充:存款利率(其余步为 0)
	Description string  // 人读摘要
}

// nimOf 取净息差(World.CB.NIM,未初始化回落 DefaultNIM)。
func nimOf(w *World) float64 {
	if w == nil || w.CB == nil || w.CB.NIM <= 0 {
		return DefaultNIM
	}
	return w.CB.NIM
}

// mlfAnchor 政策利率锚(World.CB.PolicyRate,nil 安全回落初始值)。
func mlfAnchor(w *World) float64 {
	if w == nil || w.CB == nil {
		return InitialPolicyRate
	}
	return w.CB.PolicyRate
}

// CentralToInterbank 第 1 步:MLF → SHIBOR。
// SHIBOR 1Y = MLF + 0.1%(基准) + 0.2%(流动性溢价)。
func CentralToInterbank(w *World, mlfRate float64) (float64, string) {
	shibor := mlfRate + SHIBORBaseSpread + LiquidityPremium
	return shibor, fmt.Sprintf("MLF %.2f%% + 基准 %.2f%% + 流动性溢价 %.2f%% = SHIBOR %.2f%%",
		mlfRate*100, SHIBORBaseSpread*100, LiquidityPremium*100, shibor*100)
}

// InterbankToCommercial 第 2 步:SHIBOR → 商业银行资金成本。
// 资金成本 = SHIBOR + 信贷紧迫加点(InterbankStressSpread × CreditTightness;
// 基准情形 CreditTightness=0 → 资金成本 = SHIBOR,NIM 加成移至零售端拆分)。
func InterbankToCommercial(w *World, shibor float64) (float64, string) {
	tight := 0.0
	if w != nil && w.CB != nil {
		tight = w.CB.CreditTightness
	}
	cost := shibor + InterbankStressSpread*tight
	return cost, fmt.Sprintf("SHIBOR %.2f%% + 紧迫加点 %.2f%%(tightness %.2f)= 资金成本 %.2f%%",
		shibor*100, InterbankStressSpread*tight*100, tight, cost*100)
}

// CommercialToRetail 第 3 步:商业银行资金成本 → 零售利率。
// 存款利率 = 资金成本 − NIM/2;零售贷款利率 = 资金成本 + 1% 风险溢价。
// 返回值 outRate 为零售贷款利率(存款利率见 description)。
func CommercialToRetail(w *World, fundingCost float64) (float64, string) {
	nim := nimOf(w)
	deposit := fundingCost - nim/2
	loan := fundingCost + RetailRiskPremium
	return loan, fmt.Sprintf("资金成本 %.2f%%: 存款 %.2f%%(−NIM/2, NIM %.2f%%), 零售贷款 %.2f%%(+风险溢价 %.2f%%)",
		fundingCost*100, deposit*100, nim*100, loan*100, RetailRiskPremium*100)
}

// retailDepositRate 第 3 步伴生:存款利率(与 CommercialToRetail 同公式)。
func retailDepositRate(w *World, fundingCost float64) float64 {
	return fundingCost - nimOf(w)/2
}

// ConsumerLoanRate 消费贷利率 = 1Y LPR + ConsumerSpread + CreditSpread
// (区别于 P0 固定 ConsumerRate;方法挂 CentralBankState 但定义在本文件,
// 不动 central_bank.go)。
func (cb *CentralBankState) ConsumerLoanRate() float64 {
	if cb == nil {
		return InitialPolicyRate + ConsumerSpread
	}
	return cb.LPR1Y() + ConsumerSpread + cb.CreditSpread
}

// RetailToMacro 第 4 步:零售利率 → 实体经济。
// 有效借贷利率 = 40% 房贷 + 30% 消费贷 + 30% 经营贷(存量结构权重);
// 相对初始基准的利差按敏感度换算投资/消费信贷/购房意愿变化。
func RetailToMacro(w *World, retailLoanRate float64) (float64, string) {
	mortgage := InitialPolicyRate + TermPremium + MortgageSpread // 基准房贷(P0 初值口径)
	consumer := InitialPolicyRate + ConsumerSpread               // 基准消费贷
	business := InitialPolicyRate + BusinessSpread               // 基准经营贷
	if w != nil && w.CB != nil {
		mortgage = w.CB.MortgageRate()
		consumer = w.CB.ConsumerLoanRate()
		business = w.CB.BusinessRate()
	}
	effective := 0.4*mortgage + 0.3*consumer + 0.3*business

	// 相对初始基准的利差 → 需求意愿变化(每 +1% 利率按敏感度降幅)。
	baseline := 0.4*(InitialPolicyRate+TermPremium+MortgageSpread) +
		0.3*(InitialPolicyRate+ConsumerSpread) + 0.3*(InitialPolicyRate+BusinessSpread)
	gap := effective - baseline
	housing := -gap * HousingDemandSensitivity
	consumption := -gap * ConsumerCreditSensitivity
	investment := -gap * InvestmentDemandSensitivity
	return effective, fmt.Sprintf(
		"有效借贷利率 %.2f%%(房贷 %.2f%%/消费贷 %.2f%%/经营贷 %.2f%%, 利差 %+.2f%%): 购房意愿 %+.1f%%, 消费信贷 %+.1f%%, 投资意愿 %+.1f%%",
		effective*100, mortgage*100, consumer*100, business*100, gap*100,
		housing*100, consumption*100, investment*100)
}

// RunTransmissionStep 单步传导派发器(idx 0..3 对应 4 步链)。
//
// 参数 rate 为该步输入利率;返回输出利率、步骤名与人读摘要。
// idx 越界返回零值 + 错误提示字符串(纯函数,不 panic)。
func RunTransmissionStep(w *World, idx int, rate float64) (outRate float64, step string, description string) {
	switch idx {
	case 0:
		outRate, description = CentralToInterbank(w, rate)
		return outRate, StepCentralToInterbank, description
	case 1:
		outRate, description = InterbankToCommercial(w, rate)
		return outRate, StepInterbankToCommercial, description
	case 2:
		outRate, description = CommercialToRetail(w, rate)
		return outRate, StepCommercialToRetail, description
	case 3:
		outRate, description = RetailToMacro(w, rate)
		return outRate, StepRetailToMacro, description
	default:
		return 0, "", fmt.Sprintf("transmission step index %d out of range [0,3]", idx)
	}
}

// TraceTransmission 完整 4 步传导链(纯函数,只读 World):
// MLF → SHIBOR → 资金成本 → 零售利率(存/贷)→ 实体经济有效借贷利率。
// w 或 w.CB 为 nil 时返回 nil。
func TraceTransmission(w *World) []TransmissionStep {
	if w == nil || w.CB == nil {
		return nil
	}
	mlf := mlfAnchor(w)

	shibor, d1 := CentralToInterbank(w, mlf)
	cost, d2 := InterbankToCommercial(w, shibor)
	loan, d3 := CommercialToRetail(w, cost)
	deposit := retailDepositRate(w, cost)
	effective, d4 := RetailToMacro(w, loan)

	return []TransmissionStep{
		{Index: 0, Name: StepCentralToInterbank, InRate: mlf, OutRate: shibor, Description: d1},
		{Index: 1, Name: StepInterbankToCommercial, InRate: shibor, OutRate: cost, Description: d2},
		{Index: 2, Name: StepCommercialToRetail, InRate: cost, OutRate: loan, DepositRate: deposit, Description: d3},
		{Index: 3, Name: StepRetailToMacro, InRate: loan, OutRate: effective, Description: d4},
	}
}
