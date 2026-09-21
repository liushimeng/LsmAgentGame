// Package wealth — sovereign_bond.go: 国债机制(阶段4)
// 2026-09-21 §城市扩张v2.12 阶段4。
//
// R4-1 关键约束(禁止央行透支):
//   - 财政部不能直接向央行透支 —— 本文件从不读写 CentralBankState,
//     Treasury 与央行实体之间不存在任何 Ledger 通道(ledger.go 白名单);
//   - 国债融资仅记录 BondsOutstanding(政府自持简化,不模拟投资者,
//     发行**不产生**现金流入);利息与到期兑付为真实现金流出
//     (国库账内核算,Cash 直减 + History.BondService 记录),
//     使国债成为纯粹的"未来义务/财政纪律"机制而非印钞通道。
//
// 发行规则:仅当国库现金 < 月支出 × 3 时触发;
// 发行量 = 缺口 × 80%,clamp [10 万, 500 万];
// 票面利率 = 发行时 5Y LPR + 0.20%;期限 60 个月(5 年)。
package wealth

import "fmt"

// 国债常量(阶段4 新定)。
const (
	// BondIssueTriggerMonths 发行触发:现金 < 月支出 × 3。
	BondIssueTriggerMonths = 3
	// BondIssueGapFill 发行量 = 流动性缺口 × 80%。
	BondIssueGapFill = 0.80
	// BondMinIssue 单次最小发行额 10 万元(低于此不具发行经济性,不发行)。
	BondMinIssue = 100_000
	// BondMaxIssue 单次最大发行额 500 万元。
	BondMaxIssue = 5_000_000
	// BondDefaultTenureMonths 默认期限 60 个月(5 年)。
	BondDefaultTenureMonths = 60
	// BondCouponSpread 票面利率加点 = 5Y LPR + 0.20%。
	BondCouponSpread = 0.0020
	// BondCouponFallbackRate CB 缺失时 5Y LPR 回退值 3.5%(与 InitialPolicyRate
	// 3% + TermPremium 0.5% 对齐;正常路径 NewWorld 恒有 CB,不触发)。
	BondCouponFallbackRate = 0.035
)

// SovereignBond 国债(单只;挂 TreasuryState.Bonds)。
type SovereignBond struct {
	ID           string  // "GB<issueMonth>-<seq>"
	IssueMonth   int     // 发行月(主钟)
	TenureMonths int     // 期限(60 = 5 年)
	FaceValue    int64   // 面值(元)
	CouponRate   float64 // 票面利率(年化小数)= 发行时 5Y LPR + 0.20%
}

// matured 到期判定(当前月 ≥ 发行月 + 期限)。
func (b *SovereignBond) matured(month int) bool {
	return month >= b.IssueMonth+b.TenureMonths
}

// monthlyCoupon 单月票息 = 面值 × 票面利率 ÷ 12。
func (b *SovereignBond) monthlyCoupon() int64 {
	return int64(float64(b.FaceValue)*b.CouponRate/12 + 0.5)
}

// couponRate5Y 当前票面利率基准:5Y LPR + 0.20%(CB 缺失回退 3.5%)。
func couponRate5Y(w *World) float64 {
	if w == nil || w.CB == nil {
		return BondCouponFallbackRate + BondCouponSpread
	}
	rate := w.CB.LPR5Y() + BondCouponSpread
	if rate < 0 {
		rate = 0
	}
	return rate
}

// IssueBonds 发行国债:仅当国库现金 < 月支出 × 3 时触发。
// 发行量 = 缺口 × 80%(clamp [10 万, 500 万];计算值 < 10 万则不发行)。
// 政府自持(R4-1):只记 BondsOutstanding,不产生现金流入,不触达央行。
// 返回发行面值(0 = 未发行)。
func (t *TreasuryState) IssueBonds(w *World) int64 {
	if t == nil || w == nil {
		return 0
	}
	threshold := t.monthSpend() * BondIssueTriggerMonths
	if t.Cash >= threshold {
		return 0 // 流动性充足,不发债
	}
	gap := threshold - t.Cash
	amount := int64(float64(gap)*BondIssueGapFill + 0.5)
	if amount < BondMinIssue {
		return 0 // 缺口过小,发行不具经济性
	}
	if amount > BondMaxIssue {
		amount = BondMaxIssue
	}
	bond := &SovereignBond{
		ID:           fmt.Sprintf("GB%d-%d", w.Month, len(t.Bonds)+1),
		IssueMonth:   w.Month,
		TenureMonths: BondDefaultTenureMonths,
		FaceValue:    amount,
		CouponRate:   couponRate5Y(w),
	}
	t.Bonds = append(t.Bonds, bond)
	t.BondsOutstanding += amount
	w.emitEvent("policy", -1, fmt.Sprintf(
		"财政部发行国债 %s:面值 ¥%d,票面利率 %.2f%%,期限 %d 个月(自持记账,R4-1 不向央行透支)",
		bond.ID, bond.FaceValue, bond.CouponRate*100, bond.TenureMonths))
	return amount
}

// MonthlyBondInterest 每月付息:Σ(面值 × 票面利率 ÷ 12)(国库账内核算,
// Cash 直减;现金不足时按发行序付至见底)。返回实付利息。
func (t *TreasuryState) MonthlyBondInterest() int64 {
	if t == nil {
		return 0
	}
	paid := int64(0)
	for _, b := range t.Bonds {
		coupon := b.monthlyCoupon()
		if coupon <= 0 || coupon > t.Cash {
			continue // 国库见底:利息挂账顺延(不透支)
		}
		t.Cash -= coupon
		paid += coupon
	}
	return paid
}

// RepayMatureBonds 到期兑付:本金 + 最后一期利息(国库账内核算;
// 现金不足时按到期序兑付至见底,未兑付债券保留)。返回实付总额。
func (t *TreasuryState) RepayMatureBonds(month int) int64 {
	if t == nil {
		return 0
	}
	paid := int64(0)
	kept := t.Bonds[:0] // 原地过滤已兑付债券
	for _, b := range t.Bonds {
		due := b.FaceValue + b.monthlyCoupon()
		if !b.matured(month) || due > t.Cash {
			kept = append(kept, b) // 未到期或国库不足 → 保留(下月再试)
			continue
		}
		t.Cash -= due
		t.BondsOutstanding -= b.FaceValue
		paid += due
	}
	t.Bonds = kept
	return paid
}

// MonthlyBondStep 国债月度处理(SettleTreasuryMonth ⑨D,顺序锁定):
// ① 每月付息 → ② 到期兑付 → ③ 必要发行(付息/兑付流出后再评估流动性,
// 缺口更真实)。返回当月国债还本付息合计(History.BondService 口径;
// 发行面值不计支出 —— 无现金流出)。
func (t *TreasuryState) MonthlyBondStep(w *World) int64 {
	if t == nil || w == nil {
		return 0
	}
	interest := t.MonthlyBondInterest()
	repaid := t.RepayMatureBonds(w.Month)
	t.IssueBonds(w)
	return interest + repaid
}
