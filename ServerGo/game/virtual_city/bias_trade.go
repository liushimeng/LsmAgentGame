// Package virtual_city — bias_trade.go: 偏差在交易/消费/投入路径上的接线
// (批次62 §20261020-62)。
//
// 唯一事实来源:lag_docs/虚拟城市/虚拟城市行为金融与投资心理机制设计.md
//   §1 偏差体验机制总表(每个偏差的"玩家可感知的代价"一栏)
//   §3.1 锚定指数 / §3.2 心理账户 / §3.3 双曲贴现 / §3.4 羊群
//   §4.1 认知检定 / §4.2 认知不足加重
//
// 效果落点(与 bias.go 的 Effect 枚举一一对应):
//
//	EffectBuyAmount   买入**金额**放大(过度自信 / 赌徒谬误)⇒ 仓位被系统性放大
//	EffectBuyPrice    买入**成交价**放大(锚定)⇒ 追高时多付
//	EffectSellPrice   卖出**成交价**折价(损失厌恶 / 处置效应)⇒ 不愿割肉 / 急着落袋
//	EffectCostExtra   额外**成本**(羊群跟风)⇒ 进出两头都被滑点吃掉
//	EffectConsumeCap  消费**金额**放大(心理账户)⇒ 意外之财花得更快
//	EffectInvestAmt   投入**金额**放大(沉没成本)⇒ 明知亏损仍追加
//
// ⚠ 教学纪律:偏差生效时若**认知检定未通过**,不得向玩家/Agent 暴露偏差名
// (bias.go::applyBias 返回 detected=false ⇒ 只进 hidden 列表)。
package virtual_city

import (
	"fmt"
	"math"
)

// buyBiasPlan 买入路径的偏差修正计划。
type buyBiasPlan struct {
	Amount   int64    // 修正后的投入金额(元)
	PriceMul float64  // 成交价乘子(锚定)
	CostMul  float64  // 跟风滑点乘子(额外成本)
	Amplified bool    // 投入金额是否被偏差放大(决定现金不足时可否回退份数)
	Detected []BiasDef // 已被识破的偏差(可展示)
	Hidden   []string  // 未被识破的偏差(**不可**展示)
}

// planBuyBias 计算买入决策的偏差修正(批次62)。
//
//	金额放大(过度自信 / 赌徒谬误)→ 投入金额
//	成交价放大(锚定)          → 成交价
//	跟风额外成本(羊群)        → 在成交额外再付一笔滑点
//
// ⚠ 金额放大的上限 = 玩家现金(偏差不能让人花掉不存在的钱);
// 若原始请求金额已超过现金 ⇒ **不**截断(保持原行为,让"现金不足"错误照常抛出)。
func planBuyBias(p *Player, want int64) buyBiasPlan {
	plan := buyBiasPlan{Amount: want, PriceMul: 1, CostMul: 1}
	if p == nil {
		return plan
	}
	amtMul, d1, h1 := p.biasEffectMulOf(EffectBuyAmount, nil)
	priceMul, d2, h2 := p.biasEffectMulOf(EffectBuyPrice, nil)
	costMul, d3, h3 := p.biasEffectMulOf(EffectCostExtra, nil)
	plan.PriceMul = priceMul
	plan.CostMul = costMul
	plan.Detected = append(plan.Detected, d1...)
	plan.Detected = append(plan.Detected, d2...)
	plan.Detected = append(plan.Detected, d3...)
	plan.Hidden = append(plan.Hidden, h1...)
	plan.Hidden = append(plan.Hidden, h2...)
	plan.Hidden = append(plan.Hidden, h3...)
	if amtMul > 1 && want <= p.Cash {
		amount := int64(math.Round(float64(want) * amtMul))
		if amount > p.Cash {
			amount = p.Cash
		}
		plan.Amount = amount
		plan.Amplified = true
	}
	return plan
}

// sellBiasPlan 卖出路径的偏差修正计划。
type sellBiasPlan struct {
	PriceMul float64   // 成交价乘子(损失厌恶 / 处置效应)
	CostMul  float64   // 跟风滑点乘子
	Detected []BiasDef
	Hidden   []string
}

// planSellBias 计算卖出决策的偏差修正(批次62)。
// ctx 传入标的的浮盈/浮亏状态(损失厌恶只在浮亏时触发;处置效应只在浮盈时触发)。
func planSellBias(p *Player, ctx *BiasCtx) sellBiasPlan {
	plan := sellBiasPlan{PriceMul: 1, CostMul: 1}
	if p == nil {
		return plan
	}
	priceMul, d1, h1 := p.biasEffectMulOf(EffectSellPrice, ctx)
	costMul, d2, h2 := p.biasEffectMulOf(EffectCostExtra, ctx)
	plan.PriceMul = priceMul
	plan.CostMul = costMul
	plan.Detected = append(plan.Detected, d1...)
	plan.Detected = append(plan.Detected, d2...)
	plan.Hidden = append(plan.Hidden, h1...)
	plan.Hidden = append(plan.Hidden, h2...)
	return plan
}

// biasNote 偏差对本次交易的影响说明(仅含**已识破**的偏差;未识破的不暴露)。
func biasNote(detected []BiasDef) string {
	if len(detected) == 0 {
		return ""
	}
	s := "行为偏差影响:"
	for i, d := range detected {
		if i > 0 {
			s += " / "
		}
		s += d.NameCN
	}
	return s
}

// biasCtxForAsset 依据资产成本与现价判定浮盈/浮亏(供卖出路径的 Guard 使用)。
func biasCtxForAsset(costCNY int64, fairPrice float64) *BiasCtx {
	if costCNY <= 0 {
		return &BiasCtx{}
	}
	if fairPrice < float64(costCNY) {
		return &BiasCtx{AtLoss: true}
	}
	return &BiasCtx{AtProfit: true}
}

// ── 买入执行(共用):金额/价格/滑点三段修正 ──────────────────────────────

// buyExecution 买入执行的最终参数。
type buyExecution struct {
	Units  float64 // 成交份数
	Cost   int64   // 成交金额(不含费用/滑点)
	Fee    int64   // 费用
	Extra  int64   // 跟风滑点(额外成本)
	Total  int64   // 实际支出(Cost + Fee + Extra)
	Plan   buyBiasPlan
}

// executeBuy 通用买入执行(股票/债券/黄金/房产共用)。
//
//	unitPrice  单价(已含流动性价差与锚定乘子)
//	feeOf      费用计算(股票佣金等)
//	unitsFrom  份数换算(以"投入金额 / 单价"为准)
//
// 现金约束:总支出不得超过玩家现金;不够则按份数回退(偏差放大后常见)。
func (w *World) executeBuy(p *Player, plan buyBiasPlan, unitPrice float64, feeOf func(int64) int64) buyExecution {
	budget := plan.Amount
	if unitPrice <= 0 {
		unitPrice = 1
	}
	// 跟风滑点计入单价口径(便于份数回退时一起收敛)。
	effUnit := unitPrice * plan.CostMul
	units := math.Floor(float64(budget) / effUnit)
	if units < 0 {
		units = 0
	}
	ex := buyExecution{Plan: plan}
	compute := func(u float64) (int64, int64, int64) {
		cost := int64(u*unitPrice + 0.5)
		fee := feeOf(cost)
		extra := int64(math.Round(float64(cost) * (plan.CostMul - 1)))
		return cost, fee, extra
	}
	cost, fee, extra := compute(units)
	// 现金不足时**仅当偏差放大了投入金额**才回退份数(否则保持原行为:
	// 让"现金不足"错误照常抛出,不做静默部分成交)。
	if cost+fee+extra > p.Cash && plan.Amplified {
		for units > 0 {
			units--
			cost, fee, extra = compute(units)
			if cost+fee+extra <= p.Cash {
				break
			}
		}
	}
	ex.Units, ex.Cost, ex.Fee, ex.Extra, ex.Total = units, cost, fee, extra, cost+fee+extra
	return ex
}

// recordBuyAnchor 记录一次买入的锚定样本(§3.1.4)。
//
//	决策价 = 玩家上次看到的价格(上月末观测价,AnchorMarks)
//	公允价 = 当前市场价
//	买入价 = 实际成交价
func (p *Player) recordBuyAnchor(asset string, fairPrice, buyPrice float64) {
	if p == nil {
		return
	}
	if p.AnchorMarks == nil {
		p.AnchorMarks = map[string]float64{}
	}
	decision := p.AnchorMarks[asset]
	if decision <= 0 {
		// 首次持有该资产:无锚点样本(分母/分子均无意义)。
		return
	}
	p.recordAnchorSample(AnchorSample{
		Asset:         asset,
		DecisionPrice: decision,
		FairPrice:     fairPrice,
		BuyPrice:      buyPrice,
	})
}

// refreshAnchorMarks 月末刷新"玩家已观测到的价格"(锚点的来源)。
// 记录的是玩家**月底**看到的价格 ⇒ 下个月做决策时的"决策价"。
func (p *Player) refreshAnchorMarks(m *MarketState) {
	if p == nil || m == nil {
		return
	}
	if p.AnchorMarks == nil {
		p.AnchorMarks = map[string]float64{}
	}
	for i := range p.Assets {
		a := &p.Assets[i]
		switch {
		case a.Kind == AssetStockIndex:
			p.AnchorMarks[a.Kind] = m.StockIndex
		case a.Kind == AssetGold:
			p.AnchorMarks[a.Kind] = m.GoldPrice
		case isHouseKind(a.Kind), isShopKind(a.Kind):
			p.AnchorMarks[a.Kind] = float64(m.HousePrice(a.AssetDistrict()))
		case a.Kind == AssetBond:
			p.AnchorMarks[a.Kind] = 1.0
		}
	}
}

// ── 消费路径(心理账户) ─────────────────────────────────────────────────

// consumePlan 消费路径的偏差修正计划。
type consumePlan struct {
	Amount   int64
	Detected []BiasDef
	Hidden   []string
}

// planConsume 计算消费偏差修正(批次62 §3.2.2)。
//
// 心理账户偏差在"账户里还有意外之财余额"时生效,把本次消费金额放大
// (Mul 1.5 / 未通过 2.0)—— 文档写的是"消费上限放宽 50%",引擎侧等价落地为
// **冲动消费被放大**:玩家点了 ¥5000,实际扣 ¥7500。效果对玩家完全可见,
// 且不会像硬上限那样阻断正常消费(见实施报告「偏差效果落点」说明)。
//
// ⚠ "合并账户"后(§3.2.3)放大效果立即消失(玩家把标签归并 = 钱就是钱)。
func planConsume(p *Player, want int64) consumePlan {
	plan := consumePlan{Amount: want}
	if p == nil {
		return plan
	}
	if p.MentalAccountMerged {
		return plan
	}
	mul, det, hid := p.biasEffectMulOf(EffectConsumeCap, nil)
	plan.Detected = det
	plan.Hidden = hid
	if mul > 1 {
		amount := int64(math.Round(float64(want) * mul))
		if amount > p.Cash {
			amount = p.Cash
		}
		plan.Amount = amount
	}
	return plan
}

// ── 投入路径(沉没成本) ─────────────────────────────────────────────────

// planInvest 计算投入类动作(学习进修)的偏差修正(批次62 §1 沉没成本)。
func planInvest(p *Player, want int64) (int64, []BiasDef, []string) {
	if p == nil {
		return want, nil, nil
	}
	mul, det, hid := p.biasEffectMulOf(EffectInvestAmt, nil)
	if mul > 1 {
		amount := int64(math.Round(float64(want) * mul))
		if amount > p.Cash {
			amount = p.Cash
		}
		return amount, det, hid
	}
	return want, det, hid
}

// biasAuditText 生成偏差效果的人读说明(供动作结果文本追加)。
// 只包含**已识破**的偏差名(未识破的绝不暴露)。
func biasAuditText(detected []BiasDef) string {
	note := biasNote(detected)
	if note == "" {
		return ""
	}
	return "(" + note + ")"
}

// biasEventText 偏差生效时的引擎事件(仅"已识破"的才带名字)。
func biasEventText(seat int, detected []BiasDef, hidden []string) string {
	if len(detected) == 0 && len(hidden) == 0 {
		return ""
	}
	if len(detected) > 0 {
		return fmt.Sprintf("%d 号位受「%s」影响(已识破,效果减弱 50%%)",
			seat, biasNameList(detected))
	}
	// 未识破:只报"决策偏移",不报偏差名(教学闭环)。
	return fmt.Sprintf("%d 号位本次决策出现非理性偏移(未能识破)", seat)
}

// biasNameList 已识破偏差的中文名列表。
func biasNameList(defs []BiasDef) string {
	s := ""
	for i, d := range defs {
		if i > 0 {
			s += "、"
		}
		s += d.NameCN
	}
	return s
}
