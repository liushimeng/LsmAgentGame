// Package virtual_city — policy_toolbox.go: 央行货币政策工具箱
// (2026-09-21 §城市扩张v2.12 阶段3)。
//
// 六件套工具:SLF(利率走廊上限)/ MLF(政策利率锚)/ RRR 降准与升准 /
// OMO(公开市场操作)/ CreditWindow(信贷窗口指导)。
// 纯引擎层:无锁、无 goroutine、无 IO(§92a);ApplyInstrument 直接改写
// World.CB 状态并经 w.emitEvent 追加事件流,由房间层持锁调用。
// 完整的央行行长 Bot(AgentClassCityHuman(央行行长角色提示词))决策接入在阶段 4 完成 ——
// 本阶段工具箱仅供引擎层调用与单测验证(§130 接线:单测 + MonthlyDecision 公告链)。
package virtual_city

import (
	"fmt"
	"math"
)

// 工具箱参数常量(v2.12 阶段3 新定;数值标定参考 2025 末公开市场基准)。
const (
	// SLFCorridorWidth SLF 利率走廊宽度:SLF 利率 = MLF(PolicyRate) + 0.5%。
	SLFCorridorWidth = 0.005
	// SLFMarkupMax SLF 相对政策利率的最大加点(极端流动性紧张)。
	SLFMarkupMax = 0.02
	// RRRStep 存款准备金率调整步长 0.25%(每次 ±0.25%)。
	RRRStep = 0.0025
	// RRRMin 准备金率下限 5%(再低无货币创造约束意义)。
	RRRMin = 0.05
	// RRRMax 准备金率上限 20%(极端紧缩)。
	RRRMax = 0.20
	// OMOStepFraction 公开市场操作单次基础货币调节幅度 ±5%。
	OMOStepFraction = 0.05
	// CreditWindowMin 信贷窗口指导乘数下限(最紧 0.5)。
	CreditWindowMin = 0.5
	// CreditWindowMax 信贷窗口指导乘数上限(最松 1.5)。
	CreditWindowMax = 1.5
	// CreditWindowNeutral 信贷窗口中性值 1.0(不干预)。
	CreditWindowNeutral = 1.0
	// PolicyRateFloor 政策利率下限(与 MonthlyDecision f 步一致,0.5%)。
	PolicyRateFloor = 0.005
	// PolicyRateCap 政策利率上限(10%,防发散)。
	PolicyRateCap = 0.10
	// RediscountMarkup 再贴现加点(与 MonthlyDecision f 步硬编码 0.5% 保持一致)。
	RediscountMarkup = 0.005
	// ToolboxHistoryMonths 工具箱保留最近 12 月操作历史。
	ToolboxHistoryMonths = 12
)

// 政策倾向字符串常量(与 phillips_curve.go 共用口径)。
const (
	StanceDovish  = "dovish"  // 鸽派(宽松)
	StanceNeutral = "neutral" // 中性
	StanceHawkish = "hawkish" // 鹰派(紧缩)
)

// PolicyInstrument 货币政策工具枚举(v2.12 阶段3)。
type PolicyInstrument int

// 六件套货币政策工具。数值仅作枚举标识,不参与算术。
const (
	// InstrumentSLF 常备借贷便利(Standing Lending Facility),利率走廊上限。
	InstrumentSLF PolicyInstrument = iota
	// InstrumentMLF 中期借贷便利(Medium-term Lending Facility),政策利率锚。
	InstrumentMLF
	// InstrumentRRRRateCut 降准:存款准备金率下调,释放长期流动性。
	InstrumentRRRRateCut
	// InstrumentRRRRateHike 升准:存款准备金率上调,回收流动性。
	InstrumentRRRRateHike
	// InstrumentOMO 公开市场操作:回购/逆回购调节基础货币。
	InstrumentOMO
	// InstrumentCreditWindow 信贷窗口指导:对商业银行信贷投放的行政指导。
	InstrumentCreditWindow
)

// String 返回工具中文名(事件流 / 公告 / Agent 可读)。
func (p PolicyInstrument) String() string {
	switch p {
	case InstrumentSLF:
		return "SLF常备借贷便利"
	case InstrumentMLF:
		return "MLF中期借贷便利"
	case InstrumentRRRRateCut:
		return "降准"
	case InstrumentRRRRateHike:
		return "升准"
	case InstrumentOMO:
		return "公开市场操作"
	case InstrumentCreditWindow:
		return "信贷窗口指导"
	default:
		return fmt.Sprintf("未知工具(%d)", int(p))
	}
}

// PolicyAction 单次政策操作(v2.12 阶段3)。
type PolicyAction struct {
	Instrument PolicyInstrument // 工具
	Month      int              // 操作月份(主钟)
	Magnitude  float64          // 幅度:利率类为小数(0.0025=25bp);OMO 为基础货币比例(±0.05);窗口为乘数增量
	Stance     string           // 政策倾向 dovish / neutral / hawkish
}

// PolicyToolbox 央行货币政策工具箱:持央行状态引用 + 最近 12 月操作历史。
// 纯引擎结构(无锁);由央行行长 Bot(阶段 4)或规则引擎调用。
type PolicyToolbox struct {
	CB      *CentralBankState // 央行状态引用(不可为 nil,构造时兜底)
	History []PolicyAction    // 最近 ToolboxHistoryMonths 月操作
}

// NewPolicyToolbox 构造工具箱;cb 为 nil 时兜底新建央行初始状态。
func NewPolicyToolbox(cb *CentralBankState) *PolicyToolbox {
	if cb == nil {
		cb = NewCentralBank()
	}
	return &PolicyToolbox{CB: cb}
}

// ApplyInstrument 应用一件政策工具到 World.CB(纯状态机改写,§92a 无锁)。
//
// 幅度约定:
//   - SLF:   Magnitude 为走廊加点增量(如 +0.001 = 上限抬 10bp)。
//   - MLF:   Magnitude 为政策利率增量(±0.0025 = ±25bp),传导到 LPR(按需派生)。
//   - RRR:   Magnitude 符号被工具方向归一(降准必降 / 升准必升),
//     步长量化为 0.25% 整数倍,clamp [5%, 20%]。
//   - OMO:   Magnitude 为基础货币比例增量(±0.05 = ±5%/次)。
//   - CreditWindow: Magnitude 为乘数增量,clamp [0.5, 1.5]。
//
// 每次成功操作追加 PolicyAction 到 History(保留 12 月)并 emitEvent 一条
// "policy" 事件;返回 error 仅用于参数非法(nil World/CB、未知工具、步长为零)。
func (tb *PolicyToolbox) ApplyInstrument(w *World, act PolicyAction) error {
	if tb == nil || tb.CB == nil {
		return fmt.Errorf("policy toolbox: nil central bank")
	}
	if w == nil || w.CB == nil {
		return fmt.Errorf("policy toolbox: nil world")
	}

	var desc string
	switch act.Instrument {
	case InstrumentSLF:
		desc = tb.applySLF(act)
	case InstrumentMLF:
		desc = tb.applyMLF(act)
	case InstrumentRRRRateCut, InstrumentRRRRateHike:
		var err error
		desc, err = tb.applyRRR(act)
		if err != nil {
			return err
		}
	case InstrumentOMO:
		desc = tb.applyOMO(act)
	case InstrumentCreditWindow:
		desc = tb.applyCreditWindow(act)
	default:
		return fmt.Errorf("policy toolbox: unknown instrument %d", int(act.Instrument))
	}

	// 记录历史(最近 12 月)。
	tb.History = append(tb.History, act)
	if len(tb.History) > ToolboxHistoryMonths {
		tb.History = tb.History[len(tb.History)-ToolboxHistoryMonths:]
	}

	// 事件流播报(全房事件,seat=-1)。
	w.emitEvent("policy", -1, fmt.Sprintf("央行[%s|%s] %s", act.Instrument, act.Stance, desc))
	return nil
}

// applySLF 调利率走廊上限:SLFRate = clamp(PolicyRate + 0.5% + markup, 政策利率, 政策利率+2%)。
func (tb *PolicyToolbox) applySLF(act PolicyAction) string {
	cb := tb.CB
	slf := clampF(cb.PolicyRate+SLFCorridorWidth+act.Magnitude, cb.PolicyRate, cb.PolicyRate+SLFMarkupMax)
	old := cb.SLFRate
	cb.SLFRate = slf
	return fmt.Sprintf("利率走廊上限 %.2f%%→%.2f%% (加点 %.2f%%)",
		old*100, slf*100, act.Magnitude*100)
}

// applyMLF 调政策利率锚:PolicyRate += Magnitude(clamp [0.5%,10%]);
// RediscountRate / SLFRate 随行就市;LPR 由 LPR1Y/LPR5Y/MortgageRate 按需派生。
func (tb *PolicyToolbox) applyMLF(act PolicyAction) string {
	cb := tb.CB
	old := cb.PolicyRate
	cb.PolicyRate = clampF(old+act.Magnitude, PolicyRateFloor, PolicyRateCap)
	cb.RediscountRate = cb.PolicyRate + RediscountMarkup
	if cb.SLFRate < cb.PolicyRate {
		cb.SLFRate = cb.PolicyRate + SLFCorridorWidth
	}
	return fmt.Sprintf("政策利率(MLF锚) %.2f%%→%.2f%% → LPR1Y %.2f%% / LPR5Y %.2f%%",
		old*100, cb.PolicyRate*100, cb.LPR1Y()*100, cb.LPR5Y()*100)
}

// applyRRR 调存款准备金率:方向由工具决定(降准必降/升准必升),
// 步长量化 0.25%,clamp [5%,20%];联动乘数上限与准备金规模。
func (tb *PolicyToolbox) applyRRR(act PolicyAction) (string, error) {
	cb := tb.CB
	magnitude := act.Magnitude
	if act.Instrument == InstrumentRRRRateCut && magnitude > 0 {
		magnitude = -magnitude // 降准方向归一
	}
	if act.Instrument == InstrumentRRRRateHike && magnitude < 0 {
		magnitude = -magnitude // 升准方向归一
	}
	steps := int(math.Round(math.Abs(magnitude) / RRRStep))
	if steps == 0 {
		return "", fmt.Errorf("policy toolbox: RRR magnitude %.4f not a multiple of 0.25%% step", act.Magnitude)
	}
	dir := 1.0
	if magnitude < 0 {
		dir = -1.0
	}
	old := cb.ReserveRatio
	cb.ReserveRatio = clampF(old+dir*float64(steps)*RRRStep, RRRMin, RRRMax)
	cb.MultiplierCeiling = 1.0 / cb.ReserveRatio
	cb.Reserves = cb.ReserveRatio * cb.M1
	return fmt.Sprintf("准备金率 %.2f%%→%.2f%% (步长%d×0.25%%, 乘数上限×%.1f)",
		old*100, cb.ReserveRatio*100, steps, cb.MultiplierCeiling), nil
}

// applyOMO 调基础货币:±5%/次(逆回购买入投放 / 正回购卖出国债回收,受持有量约束)。
func (tb *PolicyToolbox) applyOMO(act PolicyAction) string {
	cb := tb.CB
	frac := clampF(act.Magnitude, -OMOStepFraction, OMOStepFraction)
	amount := math.Abs(frac) * cb.BaseMoney
	old := cb.BaseMoney
	if frac >= 0 {
		// 逆回购投放:央行买入国债,扩表投放基础货币。
		cb.GovBondsHeld += amount
		cb.Reserves += amount
	} else {
		// 正回购回收:卖出国债回收基础货币(受国债持有量约束)。
		amount = math.Min(amount, cb.GovBondsHeld)
		cb.GovBondsHeld -= amount
		cb.Reserves = math.Max(0, cb.Reserves-amount)
	}
	cb.BaseMoney = cb.M0 + cb.Reserves
	if cb.BaseMoney > 0 {
		cb.MoneyMultiplier = cb.M2 / cb.BaseMoney
	}
	return fmt.Sprintf("基础货币 %.0f→%.0f (%.0f%%, ±5%%/次)",
		old, cb.BaseMoney, frac*100)
}

// applyCreditWindow 调信贷窗口指导乘数:clamp [0.5,1.5](1.0 中性);
// 同步收紧/放松当月贷款额度乘数(MonthlyDecision 下月重算覆盖)。
func (tb *PolicyToolbox) applyCreditWindow(act PolicyAction) string {
	cb := tb.CB
	old := cb.CreditWindowFactor
	cb.CreditWindowFactor = clampF(old+act.Magnitude, CreditWindowMin, CreditWindowMax)
	cb.LoanQuotaFactor = clampF(cb.LoanQuotaFactor*cb.CreditWindowFactor, LoanQuotaMin, 1.0)
	return fmt.Sprintf("信贷窗口乘数 %.2f→%.2f, 当月额度乘数 %.2f",
		old, cb.CreditWindowFactor, cb.LoanQuotaFactor)
}
