// Package virtual_city — bias_settle.go: 行为金融偏差的月结接线(批次62 §20261020-62)。
//
// 唯一事实来源:lag_docs/虚拟城市/虚拟城市行为金融与投资心理机制设计.md
//   §3.3.2 储蓄激励曲线(连续 3 个月正储蓄 ⇒ 第 4 个月起奖励 = 月结余 × 2%)
//   §3.3.3 年度延迟满足评分(延迟满足指数 ≥0.6 ⇒ 认知 +1;≤0.3 ⇒ 复盘重点)
//   §3.1.4 锚定指数(月末以"玩家已观测价格"刷新锚点)
//   §4.3 主动反思的每回合限制(月结复位)
//   §4.4.1 跨局认知继承 30%
package virtual_city

import (
	"fmt"
	"strings"
)

// settleBehaviorMonth 月结的行为金融部分(批次62)。
//
// surplus = 本月月结余(收入 − 支出;由 settlePlayer 传入 —— 此刻 p.Monthly
// 还是**上个月**的快照,不能从 p.Monthly 读)。
//
// 返回 (本月额外收入, 本月额外支出, 说明文本);由 settlePlayer 并入月度收支。
//
// 顺序:
//  1. 延期支付到期扣款(×1.3;现金不足记违约)
//  2. 储蓄纪律奖励(连续 3 个月正储蓄 ⇒ 第 4 个月起 月结余 × 2%)
//  3. 锚点刷新(玩家"已观测价格" = 本月末价格)
//  4. 连续投资亏损计数(纯价格损益口径,不含收支)
//  5. 计数器复位(反思次数 / 本月逆势次数 / 本回合免疫 / 复盘预习到期)
func (w *World) settleBehaviorMonth(p *Player, surplus int64) (int64, int64, string) {
	if p == nil {
		return 0, 0, ""
	}
	var income, expense int64
	note := ""

	// ① 延期支付到期(§3.3.1)。
	if extra := w.settleDeferredPayments(p); extra > 0 {
		expense += extra
		note = fmt.Sprintf("延期支付贴现成本 ¥%d", extra)
	}

	// ② 储蓄纪律奖励(§3.3.2)。
	if surplus > 0 {
		p.SavingsStreakMonths++
	} else {
		p.SavingsStreakMonths = 0
	}
	p.LastMonthSurplusCNY = surplus
	if p.SavingsStreakMonths > savingsRewardStreak && surplus > 0 {
		reward := int64(float64(surplus) * savingsRewardRate)
		if reward > 0 {
			w.Pay(p.Seat, EntityBank, SeatEntity(p.Seat), reward, CatBehaviorBonus, "储蓄纪律奖励")
			income += reward
			if note == "" {
				note = fmt.Sprintf("储蓄纪律奖励 ¥%d(连续正储蓄 %d 个月)", reward, p.SavingsStreakMonths)
			}
		}
	}

	// ③ 锚点刷新(§3.1):玩家"已观测到的价格"更新为本月末价格。
	p.refreshAnchorMarks(w.Market)

	// ④ 连续投资亏损计数(§1 赌徒谬误触发条件):金融类资产市值环比下跌即记一亏。
	finValue := financialAssetValue(p, w.Market)
	if p.LastFinancialAssetValue > 0 && finValue < p.LastFinancialAssetValue {
		p.LastConsecutiveLossCount++
	} else if finValue >= p.LastFinancialAssetValue {
		p.LastConsecutiveLossCount = 0
	}
	p.LastFinancialAssetValue = finValue

	// ⑤ 计数器复位。
	p.ReflectCountThisMonth = 0
	p.HerdCounterMovesThisMonth = 0
	p.BiasImmuneThisTurn = false
	// 复盘预习到期清理:设置月的月结不清理(预习覆盖的是**下一个**行动回合),
	// 下一月的月结才清理。
	if p.BiasPreviewNextTurn && p.BiasPreviewNextTurnMonth < w.Month {
		p.BiasPreviewNextTurn = false
	}

	return income, expense, note
}

// financialAssetValue 金融类资产(股/债/金)当前总市值。
func financialAssetValue(p *Player, m *MarketState) int64 {
	if p == nil || m == nil {
		return 0
	}
	var total int64
	for i := range p.Assets {
		a := &p.Assets[i]
		switch a.Kind {
		case AssetStockIndex:
			total += int64(a.Units*m.StockIndex + 0.5)
		case AssetBond:
			total += int64(a.Units + 0.5)
		case AssetGold:
			total += int64(a.Units*m.GoldPrice + 0.5)
		}
	}
	return total
}

// biasEffectLabel 偏差效果的中文标签(乘在哪一项上)。
func biasEffectLabel(effect BiasEffect) string {
	switch effect {
	case EffectSellPrice:
		return "卖出成交价 ×"
	case EffectBuyPrice:
		return "买入成交价 ×"
	case EffectBuyAmount:
		return "买入金额 ×"
	case EffectInvestAmt:
		return "投入金额 ×"
	case EffectConsumeCap:
		return "消费金额 ×"
	case EffectCostExtra:
		return "额外成本 ×"
	case EffectDeferredPay:
		return "延期支付选项"
	}
	return ""
}

// behaviorJSONFor 构造 my.behavior 快照(批次62 §20261020-62)。
//
// ⚠ 信息分层:未识破的偏差**不暴露** name_cn / effect / explanation
// (§4.1 教学闭环 —— 玩家到复盘才知道自己踩了什么坑)。
func behaviorJSONFor(p *Player) *BehaviorJSON {
	if p == nil {
		return nil
	}
	out := &BehaviorJSON{
		Cognition:           p.Cognition,
		BiasProfile:         []BiasProfileJSON{},
		BiasImmunity:        []BiasImmunityJSON{},
		DeferredPayments:    []DeferredPaymentJSON{},
		MentalAccounts:      []MentalAccountJSON{},
		AnchoringIndex:      p.anchoringIndex(),
		HerdingIndex:        p.herdingIndex(),
		SavingsStreakMonths: p.SavingsStreakMonths,
		DelayedPayIndex:     p.DelayedPayIndex,
		MentalAccountMerged: p.MentalAccountMerged,
		LastReflect:         p.LastReflect,
	}
	if out.LastReflect == "" {
		out.LastReflect = "none"
	}
	for _, code := range p.BiasProfile {
		def, ok := biasByCode(code)
		if !ok {
			continue
		}
		mul, detected := applyBias(p, def)
		item := BiasProfileJSON{
			Code:      def.Code,
			Category:  string(def.Category),
			Threshold: def.Threshold,
			Detected:  detected,
		}
		if detected {
			item.NameCN = def.NameCN
			if mul == 1 {
				// 免疫达标 / 回合免疫 ⇒ 本回合不生效(效果 ×1.00 无意义)。
				item.Effect = "已免疫(本回合不生效)"
			} else {
				item.Effect = fmt.Sprintf("%s%.2f", biasEffectLabel(def.Effect), mul)
			}
			item.Explanation = def.Desc
		}
		out.BiasProfile = append(out.BiasProfile, item)
	}
	for _, code := range p.BiasProfile {
		if p.BiasImmunity == nil {
			continue
		}
		if prog, ok := p.BiasImmunity[code]; ok {
			out.BiasImmunity = append(out.BiasImmunity, BiasImmunityJSON{Code: code, Progress: prog})
		}
	}
	for _, d := range p.DeferredPayments {
		out.DeferredPayments = append(out.DeferredPayments, DeferredPaymentJSON{
			AmountCNY:   d.AmountCNY,
			DueMonth:    d.DueMonth,
			PenaltyRate: d.PenaltyRate,
		})
	}
	for _, tag := range moneyTagOrder {
		amount := p.MentalAccount[tag]
		if amount <= 0 {
			continue
		}
		out.MentalAccounts = append(out.MentalAccounts, MentalAccountJSON{
			Tag:       tag,
			LabelCN:   moneyTagLabels[tag],
			AmountCNY: amount,
		})
	}
	return out
}

// behaviorStatusText 行为画像的人读文本(query 工具 / 人类 UI 共用)。
//
// ⚠ **只展示已识破的偏差**;未识破的只报"有未识破的行为倾向",不报名字。
func (w *World) behaviorStatusText(p *Player) string {
	if p == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "认知 %d/10 ｜ 锚定指数 %.2f ｜ 羊群指数 %.2f ｜ 连续正储蓄 %d 个月",
		p.Cognition, p.anchoringIndex(), p.herdingIndex(), p.SavingsStreakMonths)
	if p.LastMonthSurplusCNY != 0 {
		fmt.Fprintf(&b, " ｜ 上月结余 ¥%d", p.LastMonthSurplusCNY)
	}
	if p.DeferredDefaultCount > 0 {
		fmt.Fprintf(&b, " ｜ 延期支付违约 %d 次", p.DeferredDefaultCount)
	}
	if p.MentalAccountMerged {
		b.WriteString(" ｜ 心理账户已合并")
	}
	var named, unnamed []string
	for _, code := range p.BiasProfile {
		def, ok := biasByCode(code)
		if !ok {
			continue
		}
		if _, detected := applyBias(p, def); detected {
			named = append(named, def.NameCN)
		} else {
			unnamed = append(unnamed, def.NameCN)
		}
	}
	if len(named) > 0 {
		fmt.Fprintf(&b, "\n已识破的倾向:%s", strings.Join(named, "、"))
	}
	if len(unnamed) > 0 {
		// 不报名字(教学闭环),只报数量。
		fmt.Fprintf(&b, "\n另有 %d 项尚未识破的行为倾向(复盘时才会揭示)", len(unnamed))
	}
	if len(p.DeferredPayments) > 0 {
		b.WriteString("\n延期支付:")
		for _, d := range p.DeferredPayments {
			fmt.Fprintf(&b, " ¥%d(第 %d 月到期 ×%.1f)", d.AmountCNY, d.DueMonth, deferredPayDueMult)
		}
	}
	if len(p.MentalAccount) > 0 {
		b.WriteString("\n资金来源标签:")
		for _, tag := range moneyTagOrder {
			if amt := p.MentalAccount[tag]; amt > 0 {
				fmt.Fprintf(&b, " %s ¥%d", moneyTagLabels[tag], amt)
			}
		}
	}
	return b.String()
}

// annualBehaviorAdjust 年度行为结算(§3.3.3 延迟满足指数 + §4.4.1 跨局认知继承)。
//
// 由 AnnualAdjust 调用;返回本次年度结算产生的说明文本(可为空)。
func (w *World) annualBehaviorAdjust(p *Player) string {
	if p == nil {
		return ""
	}
	total := p.DelayedConsumeCount + p.ImmediateConsumeCount
	if total > 0 {
		p.DelayedPayIndex = float64(p.DelayedConsumeCount) / float64(total)
	} else {
		p.DelayedPayIndex = 0
	}
	if p.DelayedPayIndex >= 0.6 {
		p.Cognition++
		if p.Cognition > 10 {
			p.Cognition = 10
		}
		return fmt.Sprintf("延迟满足指数 %.2f(≥0.6):年终认知 +1", p.DelayedPayIndex)
	}
	if p.DelayedPayIndex <= 0.3 && total > 0 {
		return fmt.Sprintf("延迟满足指数 %.2f(≤0.3):复盘重点讨论 —— 即时满足偏好过强", p.DelayedPayIndex)
	}
	return ""
}
