// Package virtual_city — bankruptcy_v2.go: 批次75 P0 经济平衡 v2 第二步
// 「三阶段破产」(B75-1,方案 §3.6)。
//
// 状态机(纯逻辑,零 rand):
//
//	Normal ──月末 Cash<0 且 Cash ≥ GraceFloor──▶ H1(困难期,不记破产)
//	H1 ──到期仍负 / Cash<GraceFloor / 连续两月无法付息──▶ R1(协助重组)
//	H1 ──月末 Cash ≥ 0──▶ Normal(恢复)
//	R1 ──重组后基础月现金流 ≥ 0 且 Cash ≥ 0──▶ Normal(恢复)
//	R1 ──基础现金流 < 0 且清算后资不抵债──▶ B1(正式破产 / 出局)
//
// 全部现金变动走 World.Pay;常量与纯函数见 balance_v2.go。
// BalanceV2Enabled=false ⇒ 本文件所有函数 no-op(旧口径由 settlement.go 承载)。
package virtual_city

import "fmt"

// isConsumerOrCredit 消费贷/信用贷判定(H1 只付息 / R1 重组的适用面)。
func isConsumerOrCredit(kind string) bool {
	switch kind {
	case LoanConsumer, LoanCreditT1, LoanCreditT2, LoanCreditT3:
		return true
	}
	return false
}

// hardshipLoan 该座位本月贷款是否按 H1 只付息口径处理(方案 §3.6.2)。
//
// 已在 H1,或本月现金 < 0 且 ≥ GraceFloor(将于步骤7 进入 H1)—— 覆盖「入 H1
// 当月」的贷款步,避免入 H1 前的月供先记逾期。R1 内贷款已重组(FreeInterest),
// 不走此口径。
func (w *World) hardshipLoan(p *Player) bool {
	if w == nil || !w.BalanceV2Enabled || p == nil || p.InR1 {
		return false
	}
	if p.InHardship {
		return true
	}
	return p.Cash < 0 && p.Cash >= w.graceFloor(p)
}

// bankruptcyStep 三阶段破产状态机月度推进(月末步骤7 调用)。
// 仅 BalanceV2Enabled=true 生效;false ⇒ no-op(旧口径路径在 settlement.go)。
func (w *World) bankruptcyStep(p *Player) {
	if w == nil || !w.BalanceV2Enabled || p == nil || !p.Alive {
		return
	}
	switch {
	case p.InR1:
		w.stepR1(p)
	case p.InHardship:
		w.stepH1(p)
	default:
		w.stepNormal(p)
	}
}

// stepNormal 常态:现金 < 0 ⇒ 进入 H1(≥ GraceFloor)或直落 R1(< GraceFloor)。
func (w *World) stepNormal(p *Player) {
	if p.Cash >= 0 {
		return
	}
	if p.Cash >= w.graceFloor(p) {
		w.enterHardship(p)
		return
	}
	w.enterR1(p)
}

// stepH1 困难期:月末 Cash ≥ 0 ⇒ 恢复;Cash < GraceFloor / 连续两月无法付息 ⇒
// 直落 R1;H1 到期仍负 ⇒ R1。
func (w *World) stepH1(p *Player) {
	if p.Cash >= 0 {
		w.exitHardship(p)
		w.emitEvent("settle", p.Seat, fmt.Sprintf("%d 号位度过困难期,重返赛道", p.Seat))
		return
	}
	if p.Cash < w.graceFloor(p) || p.HardshipUnpaidMonths >= 2 {
		w.exitHardship(p)
		w.enterR1(p)
		return
	}
	p.HardshipMonths--
	if p.HardshipMonths <= 0 {
		w.exitHardship(p)
		w.enterR1(p)
	}
}

// stepR1 协助重组:重组后基础现金流 ≥ 0 且 Cash ≥ 0 ⇒ 恢复;基础现金流 < 0 且
// 清算后资不抵债 ⇒ B1(正式破产/出局)。
func (w *World) stepR1(p *Player) {
	if p.R1FreezeMonths > 0 {
		p.R1FreezeMonths--
	}
	if w.predictedBaseCashFlow(p) >= 0 && p.Cash >= 0 {
		w.exitR1(p)
		w.emitEvent("settle", p.Seat, fmt.Sprintf("%d 号位重组成功,重返赛道", p.Seat))
		return
	}
	if w.predictedBaseCashFlow(p) < 0 && !w.postLiquidationSolvent(p) {
		w.liquidate(p) // B1 清算 + 资不抵债才 eliminate
	}
}

// enterHardship 进入 H1 困难期(设置时长 + 房贷展期条款)。
// ⚠ 时长必须在 HardshipCount 递增**之前**取(首次 6 月 / 重复 3 月)。
func (w *World) enterHardship(p *Player) {
	p.InHardship = true
	p.HardshipMonths = w.hardshipDuration(p)
	p.HardshipCount++
	// ⚠ **不得**在此清零 HardshipUnpaidMonths:步骤 6(贷款步)已按座位口径算出
	// 本月「无法付息」计数(入 H1 当月即为 1),清零会使「连续两月无法付息 ⇒ R1」
	// 的触发器永远不生效(计数每被 enterHardship 抹平一次就永远到不了 2)。
	// 计数归零只在 exitHardship(恢复)时发生。
	w.applyHardshipTerms(p)
	w.emitEvent("settle", p.Seat, fmt.Sprintf(
		"%d 号位进入困难期(H1),时长 %d 月(消费贷/信用贷只付息)", p.Seat, p.HardshipMonths))
}

// exitHardship 退出 H1(记录结束月,供重复触发 3 月判定)。
func (w *World) exitHardship(p *Player) {
	p.LastHardshipEndMonth = w.Month
	p.InHardship = false
	p.HardshipMonths = 0
	p.HardshipUnpaidMonths = 0
}

// applyHardshipTerms H1 贷款条款(方案 §3.6.2):
//   - 房贷月供 > 家庭常规收入 35% ⇒ 一次性展期 12 月并重算月供(≤ 35%);
//   - 消费贷/信用贷只付息由 settlePlayer 步骤6 承载(本函数只处理房贷展期)。
//
// 自住房保留条件(房贷+物业+基础生活 ≤ 常规收入 55%)在 R1 重组时校验。
func (w *World) applyHardshipTerms(p *Player) {
	if w == nil || p == nil {
		return
	}
	income := householdRegularGrossIncome(p)
	if income <= 0 {
		return
	}
	capPayment := int64(float64(income)*MortgagePaymentIncomeRatioV2 + 0.5)
	for i := range p.Loans {
		loan := &p.Loans[i]
		if loan.Kind != LoanMortgage || loan.Balance <= 0 {
			continue
		}
		if loan.MonthsLeft <= 0 || loan.MonthlyPayment <= capPayment {
			continue
		}
		// 展期 12 月(剩余期数 +12)并重算月供;展期后仍超 35% ⇒ 维持展期
		// (已尽最大努力),不回退。
		loan.MonthsLeft += MortgageExtensionMonthsV2
		loan.TermN += MortgageExtensionMonthsV2
		loan.MonthlyPayment = AnnuityPayment(loan.Balance, loan.AnnualRate, loan.MonthsLeft)
		if loan.MonthlyPayment > capPayment {
			loan.MonthlyPayment = capPayment
		}
		w.emitEvent("settle", p.Seat, fmt.Sprintf(
			"%d 号位房贷展期 %d 月,月供调整为 ¥%d", p.Seat, MortgageExtensionMonthsV2, loan.MonthlyPayment))
	}
}

// enterR1 进入 R1 协助重组(重组条款 + 信用分下限 + 动作冻结 1 月)。
func (w *World) enterR1(p *Player) {
	p.InR1 = true
	p.R1Months = w.reorgPeriods(p)
	p.R1FreezeMonths = 1
	p.NegativeCashMonths = 0
	w.reorganize(p)
	w.emitEvent("settle", p.Seat, fmt.Sprintf(
		"%d 号位进入协助重组(R1),消费贷/信用贷本金减半分 %d 期免息", p.Seat, p.R1Months))
}

// exitR1 退出 R1(记录结束月,供重复重组 36 期判定)。
func (w *World) exitR1(p *Player) {
	p.LastR1EndMonth = w.Month
	p.InR1 = false
	p.R1Months = 0
	p.R1FreezeMonths = 0
}

// reorganize R1 重组动作(方案 §3.6.3):
//   - 消费贷/信用贷:本金 ×0.5、免息、60/36 期(Balance 按期递减);
//   - 信用分下限 400(不归零);
//   - 记清算事件(BankruptCount)+ 破产时刻快照(首次)。
//
// 自住房保留条件(房贷+物业+基础生活 ≤ 常规收入 55%)不满足时,仅告警不改状态
// —— 清算顺序已在 B1 按「非自住 → 自住」保护自住房。
func (w *World) reorganize(p *Player) {
	if w == nil || p == nil {
		return
	}
	periods := p.R1Months
	if periods <= 0 {
		periods = ReorgFirstPeriods
	}
	for i := range p.Loans {
		loan := &p.Loans[i]
		if !isConsumerOrCredit(loan.Kind) {
			continue
		}
		loan.Balance /= 2
		loan.FreeInterest = true
		loan.InterestOnly = false
		loan.LumpAtMaturity = false
		loan.AnnualRate = 0
		loan.TermN = periods
		loan.MonthsLeft = periods
		if loan.Balance < int64(periods) {
			loan.MonthlyPayment = 1
		} else {
			loan.MonthlyPayment = loan.Balance / int64(periods)
		}
	}
	if p.CreditScore < CreditScoreFloorV2 {
		p.CreditScore = CreditScoreFloorV2
	}
	// 自住房保留校验(方案 §3.6.3-3:房贷 + 物业 + 基础生活 ≤ 常规收入 55%)。
	// 不满足 ⇒ 记告警事件(清算顺序已按「非自住 → 自住」保护自住房)。
	if p.selfOccupiedHouse() != nil {
		income := householdRegularGrossIncome(p)
		if income > 0 {
			obligation := propertyFeeCNY +
				int64(float64(p.Card.Expense)*SurvivalExpenseRate+0.5)
			for i := range p.Loans {
				if p.Loans[i].Kind == LoanMortgage {
					obligation += p.Loans[i].MonthlyPayment
				}
			}
			if int64(float64(income)*SelfOccupiedRetentionRatioV2+0.5) < obligation {
				w.emitEvent("settle", p.Seat, fmt.Sprintf(
					"%d 号位重组后自住房支出占比超 %.0f%%,建议出售非自住房或迁区降租",
					p.Seat, SelfOccupiedRetentionRatioV2*100))
			}
		}
	}
	p.BankruptCount++
	p.Energy -= 2
	if p.Energy < -3 {
		p.Energy = -3
	}
	w.captureBankruptSnapshot(p)
}

// postLiquidationSolvent 模拟清算后是否仍具备偿债能力(方案 §3.6.4):
//
//	清算口径 = 现金 + 流动资产市值 + 非自住房/商铺 ×70% + 自住房 ×70% − 贷款
//
// 纯计算(不实际清算),供 B1 判据预判。
func (w *World) postLiquidationSolvent(p *Player) bool {
	if w == nil || p == nil {
		return true
	}
	total := p.Cash
	for i := range p.Assets {
		a := &p.Assets[i]
		if isHouseKind(a.Kind) || isShopKind(a.Kind) {
			total += int64(float64(AssetValue(a, w.Market)) * liquidationDiscount)
			continue
		}
		total += AssetValue(a, w.Market)
	}
	for i := range p.Loans {
		total -= p.Loans[i].Balance
	}
	return total >= 0
}

// liquidateInsolvent B1 清算(方案 §3.6.4):
//
// 顺序:流动资产(股票/债券/黄金)→ 非自住房/商铺 → 自住房(房产均按市价 70%
// 强制变现);先偿债,余额留现金。全部经 World.Pay;不直接改写 p.Cash。
func (w *World) liquidateInsolvent(p *Player) {
	if w == nil || p == nil {
		return
	}
	w.captureBankruptSnapshot(p)
	seat := p.Seat
	// 1) 流动资产。
	for i := len(p.Assets) - 1; i >= 0; i-- {
		a := &p.Assets[i]
		switch {
		case a.Kind == AssetStockIndex:
			if gross := int64(a.Units*w.Market.StockIndex + 0.5); gross > 0 {
				w.Pay(seat, EntityMarket, SeatEntity(seat), gross, CatSell, "清算卖出基金")
			}
		case a.Kind == AssetBond:
			if a.Units > 0 {
				w.Pay(seat, EntityMarket, SeatEntity(seat), int64(a.Units+0.5), CatSell, "清算赎回债券")
			}
		case a.Kind == AssetGold:
			if gross := int64(a.Units*w.Market.GoldPrice + 0.5); gross > 0 {
				w.Pay(seat, EntityMarket, SeatEntity(seat), gross, CatSell, "清算卖出黄金")
			}
		}
	}
	// 2) 非自住房/商铺(70%)。
	for i := len(p.Assets) - 1; i >= 0; i-- {
		a := &p.Assets[i]
		if (isHouseKind(a.Kind) || isShopKind(a.Kind)) && !a.IsSelfOccupied() {
			if forced := int64(float64(AssetValue(a, w.Market)) * liquidationDiscount); forced > 0 {
				w.Pay(seat, EntityMarket, SeatEntity(seat), forced, CatSell, "强制变现非自住房产")
			}
		}
	}
	// 3) 自住房(70%)。
	for i := len(p.Assets) - 1; i >= 0; i-- {
		a := &p.Assets[i]
		if (isHouseKind(a.Kind) || isShopKind(a.Kind)) && a.IsSelfOccupied() {
			if forced := int64(float64(AssetValue(a, w.Market)) * liquidationDiscount); forced > 0 {
				w.Pay(seat, EntityMarket, SeatEntity(seat), forced, CatSell, "强制变现自住房")
			}
		}
	}
	p.Assets = nil
	p.SideBusiness = nil
	// 4) 清偿贷款(不足部分留负现金 ⇒ 资不抵债)。
	for i := len(p.Loans) - 1; i >= 0; i-- {
		loan := &p.Loans[i]
		pay := int64(0)
		if p.Cash > 0 {
			pay = loan.Balance
			if pay > p.Cash {
				pay = p.Cash
			}
		}
		if pay > 0 {
			w.Pay(seat, SeatEntity(seat), EntityBank, pay, CatRepay, "清算偿债 "+loan.ID)
		}
		loan.Balance -= pay
		if loan.Balance <= 0 {
			w.removeLoan(p, loan.ID)
		}
	}
}
