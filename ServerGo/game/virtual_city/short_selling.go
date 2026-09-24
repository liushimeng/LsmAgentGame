// Package virtual_city — short_selling.go: 融券做空子系统(2026-09-21 §城市扩张v2.12 阶段6)。
//
// 资金流模型(broker 记账,LiberalMarket 实体承接;I1 守恒走 w.Pay):
//
//	开仓:seat → market 保证金(市值×50%,CatBuy;卖券所得留存 broker 不落地)
//	月度:seat → bank   融券利息(当期市值 × 8%/年,CatInterest)
//	平仓:market → seat 保证金 + (开仓价−平仓价)×份数(CatSell)
//	      强平线:浮亏达保证金 70%(即股价上涨 ~35%)→ ShortMonthlyStep 强制平仓
//	      (亏损封顶 70%×保证金,剩余 30% 返还;月度间暴涨 >2× 的极端路径由
//	       settle≥0 兜底,player 最大损失 = 保证金)。
//
// R6-1 做空护栏(开仓校验):
//
//	① 单只融券余额 ≤ 流通市值 5%   —— ShortFloatShares=2000 万份 × StockIndex
//	② 整体融券/融资余额比 ≤ 30%    —— 融资 = 全房贷款余额 + ShortFinancingBaseCNY
//	   (玩家贷款为消费性融资,非两融口径;基数代表券商体系对全市场的融出规模)
//
// 纯引擎层:无锁、无 goroutine、无 IO、**零 rand 消费**(价格来自
// Market.StockIndex)—— 固定种子存量对局回归零偏移(treasury/供应链同款纪律)。
package virtual_city

import (
	"fmt"

	"LsmAgentGame/errcode"
)

// 融券做空常量(阶段6 新定)。
const (
	// ShortMarginRatio 保证金比例 = 开仓市值 × 50%。
	ShortMarginRatio = 0.50
	// ShortMarginCallLossRatio 强平线:浮亏达保证金 70%(≈ 股价 +35%)。
	ShortMarginCallLossRatio = 0.70
	// ShortBorrowAnnualRate 融券费率 8%/年(按当期市值计)。
	ShortBorrowAnnualRate = 0.08
	// ShortFloatShares 流通股本(份;stock_index 单一标的口径)。
	ShortFloatShares int64 = 20_000_000
	// ShortFloatCapRatio R6-1①:单只融券余额 ≤ 流通市值 5%。
	ShortFloatCapRatio = 0.05
	// ShortFinancingRatioCap R6-1②:融券/融资余额比 ≤ 30%。
	ShortFinancingRatioCap = 0.30
	// ShortFinancingBaseCNY 融资余额基数(元;券商体系融出规模,见头注释)。
	ShortFinancingBaseCNY int64 = 10_000_000
	// ShortMinShares 单笔最小做空份数。
	ShortMinShares int64 = 100
)

// 融券头寸状态。
const (
	ShortStatusOpen        = "open"
	ShortStatusClosed      = "closed"
	ShortStatusMarginCall  = "margin_call"
)

// ShortSymbol 唯一可做空标的(与 market.go StockIndex 关联)。
const ShortSymbol = "stock_index"

// ShortPosition 融券做空头寸。
type ShortPosition struct {
	ID         string  // 如 "S1"
	PlayerSeat int
	Symbol     string  // 恒 "stock_index"
	Shares     int64   // 做空份数
	EntryPrice float64 // 开仓股指(元/份)
	Margin     float64 // 保证金(元,= 开仓市值 × 50%)
	OpenMonth  int

	Status         string  // open|closed|margin_call
	ClosePrice     float64 // 平仓股指(closed/margin_call 填)
	RealizedPnL    int64   // 平仓净盈亏(元;负=亏损)
	BorrowFeeCNY   int64   // 累计融券利息(元)
}

// ShortBook 全房融券台账(挂 World.ShortBook;引擎纯状态,无锁)。
type ShortBook struct {
	Positions []*ShortPosition // 全量(含已平仓;按开仓序)

	seq int
	// LastStepMonth 最近一次 ShortMonthlyStep 的 w.Month(§130 接线验证)。
	LastStepMonth int
	// MarginCallsLastMonth 上月强平笔数(快照/情绪面)。
	MarginCallsLastMonth int
}

// NewShortBook 构造空台账。
func NewShortBook() *ShortBook {
	return &ShortBook{}
}

// ShortFloatCapShares 单只融券份数上限 = 流通股本 × 5%(R6-1①,份数口径)。
func ShortFloatCapShares() int64 {
	return int64(float64(ShortFloatShares) * ShortFloatCapRatio)
}

// openShortShares 已开仓份数合计(指定标的;open 状态)。
func (m *ShortBook) openShortShares(symbol string) int64 {
	if m == nil {
		return 0
	}
	var n int64
	for _, pos := range m.Positions {
		if pos.Status == ShortStatusOpen && pos.Symbol == symbol {
			n += pos.Shares
		}
	}
	return n
}

// ShortBalanceCNY 融券余额(元)= Σ open 份数 × 当前股指。
func (w *World) ShortBalanceCNY() int64 {
	if w == nil || w.ShortBook == nil {
		return 0
	}
	var v int64
	for _, pos := range w.ShortBook.Positions {
		if pos.Status == ShortStatusOpen {
			v += int64(float64(pos.Shares)*w.Market.StockIndex + 0.5)
		}
	}
	return v
}

// ShortMarginFrozenCNY 全房保证金冻结合计(元;open 状态)。
func (w *World) ShortMarginFrozenCNY() int64 {
	if w == nil || w.ShortBook == nil {
		return 0
	}
	var v int64
	for _, pos := range w.ShortBook.Positions {
		if pos.Status == ShortStatusOpen {
			v += int64(pos.Margin + 0.5)
		}
	}
	return v
}

// FinancingBalanceCNY 融资余额(元)= 全房贷款余额 + 券商融出基数(R6-1② 分母)。
func (w *World) FinancingBalanceCNY() int64 {
	var loans int64
	for _, p := range w.Players {
		if p == nil {
			continue
		}
		for i := range p.Loans {
			loans += p.Loans[i].Balance
		}
	}
	return loans + ShortFinancingBaseCNY
}

// OpenShort 开仓做空(R6-1 双护栏校验)。返回新头寸。
func (w *World) OpenShort(seat int, symbol string, shares int64) (*ShortPosition, *errcode.Error) {
	p := w.Players[seat]
	if p == nil || !p.Alive {
		return nil, errcode.Code(errcode.ErrVirtualCityPlayerInactive)
	}
	if symbol != ShortSymbol {
		return nil, errcode.CodeMsg(errcode.ErrVirtualCityAssetInvalid, "short symbol must be stock_index")
	}
	if shares < ShortMinShares {
		return nil, errcode.CodeMsg(errcode.ErrVirtualCityAssetInvalid,
			fmt.Sprintf("short shares must be >= %d", ShortMinShares))
	}
	if w.ShortBook == nil {
		return nil, errcode.CodeMsg(errcode.ErrVirtualCityAssetInvalid, "short book not initialized")
	}
	// R6-1① 单只融券余额 ≤ 流通市值 5%(份数口径:流通股本 × 5%)。
	if existing := w.ShortBook.openShortShares(symbol); existing+shares > ShortFloatCapShares() {
		return nil, errcode.CodeMsg(errcode.ErrVirtualCityListingInvalid,
			fmt.Sprintf("short float cap exceeded: %d+%d > %d shares(流通市值 5%%)",
				existing, shares, ShortFloatCapShares()))
	}
	// R6-1② 融券/融资余额比 ≤ 30%。
	price := w.Market.StockIndex
	newBalance := w.ShortBalanceCNY() + int64(float64(shares)*price+0.5)
	if financing := w.FinancingBalanceCNY(); financing > 0 &&
		float64(newBalance) > ShortFinancingRatioCap*float64(financing) {
		return nil, errcode.CodeMsg(errcode.ErrVirtualCityListingInvalid,
			fmt.Sprintf("short/financing ratio cap 30%% exceeded: ¥%d > 30%%×¥%d",
				newBalance, financing))
	}
	margin := float64(shares) * price * ShortMarginRatio
	if float64(p.Cash) < margin {
		return nil, errcode.Code(errcode.ErrVirtualCityInsufficientCash)
	}
	w.Pay(seat, SeatEntity(seat), EntityMarket, int64(margin+0.5), CatBuy, "融券开仓保证金")
	w.ShortBook.seq++
	pos := &ShortPosition{
		ID: fmt.Sprintf("S%d", w.ShortBook.seq), PlayerSeat: seat,
		Symbol: symbol, Shares: shares, EntryPrice: price,
		Margin: margin, OpenMonth: w.Month, Status: ShortStatusOpen,
	}
	w.ShortBook.Positions = append(w.ShortBook.Positions, pos)
	w.emitEvent("action", seat, fmt.Sprintf("%d 号位融券做空 %d 份 @¥%.2f(保证金 ¥%.0f)",
		seat, shares, price, margin))
	return pos, nil
}

// settleShort 平仓结算(主动平仓与强平共用):market → seat 保证金+盈亏。
// settle = margin + (entry−close)×shares;月度间暴涨导致亏损 > 保证金时
// settle 钳 0(player 最大损失 = 保证金,broker 承担尾部)。
func (w *World) settleShort(pos *ShortPosition, closePrice float64, status string) {
	pnl := (pos.EntryPrice - closePrice) * float64(pos.Shares)
	settle := pos.Margin + pnl
	if settle < 0 {
		settle = 0
	}
	pos.Status = status
	pos.ClosePrice = closePrice
	pos.RealizedPnL = int64(pnl + 0.5)
	if settle > 0 {
		w.Pay(pos.PlayerSeat, EntityMarket, SeatEntity(pos.PlayerSeat),
			int64(settle+0.5), CatSell, "融券平仓结算 "+pos.ID)
	}
}

// CloseShort 主动平仓(seat 须为头寸持有人;open 状态)。
func (w *World) CloseShort(seat int, posID string) (*ShortPosition, *errcode.Error) {
	if w.ShortBook == nil {
		return nil, errcode.CodeMsg(errcode.ErrVirtualCityAssetInvalid, "short book not initialized")
	}
	for _, pos := range w.ShortBook.Positions {
		if pos.ID != posID || pos.PlayerSeat != seat {
			continue
		}
		if pos.Status != ShortStatusOpen {
			return nil, errcode.CodeMsg(errcode.ErrVirtualCityListingInvalid, "short position already closed: "+posID)
		}
		w.settleShort(pos, w.Market.StockIndex, ShortStatusClosed)
		w.emitEvent("action", seat, fmt.Sprintf("%d 号位平空 %s @¥%.2f,盈亏 ¥%d",
			seat, pos.ID, pos.ClosePrice, pos.RealizedPnL))
		return pos, nil
	}
	return nil, errcode.CodeMsg(errcode.ErrVirtualCityAssetInvalid, "short position not found: "+posID)
}

// ShortMonthlyStep 月度融券台账(SettleMonth ⑥B):
// ① 融券利息(当期市值 × 8%/12,seat→bank,现金可为负的强制扣款口径)→
// ② 强平检查(浮亏 ≥ 保证金 70% → market→seat 结算,status=margin_call)。
// 零 rand;nil 安全。
func (m *ShortBook) ShortMonthlyStep(w *World) {
	if m == nil || w == nil || w.Market == nil {
		return
	}
	m.LastStepMonth = w.Month
	m.MarginCallsLastMonth = 0
	price := w.Market.StockIndex
	for _, pos := range m.Positions {
		if pos.Status != ShortStatusOpen {
			continue
		}
		// ① 融券利息(按当期市值计息)。
		fee := int64(float64(pos.Shares)*price*ShortBorrowAnnualRate/12 + 0.5)
		if fee > 0 {
			w.Pay(pos.PlayerSeat, SeatEntity(pos.PlayerSeat), EntityBank, fee,
				CatInterest, "融券利息 "+pos.ID)
			pos.BorrowFeeCNY += fee
		}
		// ② 强平线:浮亏 ≥ 保证金 × 70%(股价 +~35%)。
		loss := (price - pos.EntryPrice) * float64(pos.Shares)
		if loss >= ShortMarginCallLossRatio*pos.Margin {
			w.settleShort(pos, price, ShortStatusMarginCall)
			m.MarginCallsLastMonth++
			w.emitEvent("market", pos.PlayerSeat, fmt.Sprintf(
				"%d 号位融券头寸 %s 触发强平(浮亏 ¥%.0f ≥ 保证金 70%%),返还 ¥%.0f",
				pos.PlayerSeat, pos.ID, loss, pos.Margin+ (pos.EntryPrice-price)*float64(pos.Shares)))
		}
	}
}
