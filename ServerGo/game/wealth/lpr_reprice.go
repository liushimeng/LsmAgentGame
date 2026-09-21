// Package wealth — lpr_reprice.go: LPR 年度重定价引擎 + 等额本息计算(2026-09-16 §财商流P1)。
//
// 实现设计文档: lag_docs/虚拟城市/已实现/05-P1扩展/虚拟城市-P1-Minsky与LPR引擎-v1.md §3。
// 纯引擎层:RepriceMortgageLPR 由 settlement.go 持房间锁调用。
package wealth

import (
	"fmt"
	"math"
)

// LPRRepriceRecord LPR 重定价变更记录(view / event 用)。
type LPRRepriceRecord struct {
	Seat       int
	LoanID     string
	OldRate    float64
	NewRate    float64
	OldPayment int64
	NewPayment int64
	LPR5Y      float64
}

// computeAnnuityPayment 等额本息月供(标准公式)(v2.60 N12-3)。
// M = P * r(1+r)^n / ((1+r)^n - 1);r = 月利率;处理 annualRate <= 0 退化。
// 与 engine.go AnnuityPayment 同构,独立副本以避免循环依赖/重命名冲突。
func computeAnnuityPayment(principal int64, annualRate float64, months int) int64 {
	if months <= 0 || principal <= 0 {
		return 0
	}
	if annualRate <= 0 {
		return principal / int64(months)
	}
	r := annualRate / 12
	factor := math.Pow(1+r, float64(months))
	m := float64(principal) * r * factor / (factor - 1)
	if m < 1 {
		m = 1
	}
	return int64(m + 0.5)
}

// RepriceMortgageLPR 年度 1 月 LPR 重定价(v2.60 N12-3)。
// 对每个玩家的房贷(非固定利率)按最新 5Y LPR + 原加点 + 信用加点重算月供。
// 返回变更摘要(event 用)。调用方持房间锁。
func (w *World) RepriceMortgageLPR() []LPRRepriceRecord {
	if w.CB == nil {
		return nil
	}
	newLPR5Y := w.CB.ComputeL5Y()
	var records []LPRRepriceRecord
	for seat, p := range w.Players {
		if p == nil || !p.Alive {
			continue
		}
		for i := range p.Loans {
			loan := &p.Loans[i]
			if loan.Kind != LoanMortgage || loan.RateFixed {
				continue
			}
			if loan.MonthsLeft <= 0 {
				continue
			}
			oldPayment := loan.MonthlyPayment
			oldRate := loan.AnnualRate
			// 新利率 = 最新 5Y LPR + 原加点(锁定) + 信用加点(按最新信用等级)。
			newRate := newLPR5Y + loan.OrigSpread + p.CreditMarkup()
			if newRate < 0 {
				newRate = 0
			}
			remaining := loan.MonthsLeft
			loan.AnnualRate = newRate
			loan.MonthlyPayment = computeAnnuityPayment(loan.Balance, newRate, remaining)
			records = append(records, LPRRepriceRecord{
				Seat:       seat,
				LoanID:     loan.ID,
				OldRate:    oldRate,
				NewRate:    newRate,
				OldPayment: oldPayment,
				NewPayment: loan.MonthlyPayment,
				LPR5Y:      newLPR5Y,
			})
			w.emitEvent("lpr_reprice", seat, fmt.Sprintf(
				"LPR 重定价:房贷 %s 利率 %.2f%%→%.2f%%,月供 %d→%d",
				loan.ID, oldRate*100, newRate*100, oldPayment, loan.MonthlyPayment))
		}
	}
	return records
}

// max1 返回较大值(int64)。
func max1(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
