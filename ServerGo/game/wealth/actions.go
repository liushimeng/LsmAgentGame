// Package wealth — actions.go: 动作校验与执行(2026-09-14 §财商流P0)。
//
// 唯一事实来源: 协议契约文档 §4 动作语义表。人类按钮(WS game.wealth_action)、
// Agent 工具(wealthplayer DispatchTool)与本文件三方共用同一代码路径,无分叉。
// 通用校验①(status/phase)与④(观战者)在房间层;②③(alive/budget)在此复核。
package wealth

import (
	"fmt"
	"math"
	"strings"

	"LsmAgentGame/errcode"
)

// Action 是 game.wealth_action 的载荷(人类与 Agent 共用)。
type Action struct {
	Type string `json:"type"`
	// buy_asset/sell_asset: "stock_index"|"bond"|"gold"|"house:<d>"|"shop:<d>";
	// buy_house: "house"(默认)|"shop"。
	Asset string `json:"asset,omitempty"`
	// buy_asset / take_loan / early_repay
	AmountCNY int64 `json:"amount_cny,omitempty"`
	// sell_asset
	Units float64 `json:"units,omitempty"`
	// buy_house / move_district
	District string `json:"district,omitempty"`
	// buy_house
	DownpayRatio float64 `json:"downpay_ratio,omitempty"`
	// take_loan / repay_loan
	Kind string `json:"kind,omitempty"`
	// repay_loan / early_repay
	LoanID string `json:"loan_id,omitempty"`
	// consume / donate
	Reason string `json:"reason,omitempty"`
	// set_consumption(P1 §3.5):0 节俭/1 标准/2 精致/3 奢侈
	Level int `json:"level,omitempty"`
}

// 动作类型常量(协议 §4)。
const (
	ActBuyAsset     = "buy_asset"
	ActSellAsset    = "sell_asset"
	ActBuyHouse     = "buy_house"
	ActTakeLoan     = "take_loan"
	ActRepayLoan    = "repay_loan"
	ActStartSide    = "start_side_business"
	ActStopSide     = "stop_side_business"
	ActStudy        = "study"
	ActSocialize    = "socialize"
	ActRest         = "rest"
	ActWorkOvertime = "work_overtime"
	ActMoveDistrict = "move_district"
	ActConsume      = "consume"
	ActDonate       = "donate"
	ActSubmitMonth  = "submit_month"
	// P1 新增: 活期→定期 / 定期→活期。
	ActDeposit  = "deposit"
	ActWithdraw = "withdraw"
	// P1 新增: 提前还款(v2.60 N12-5)。
	ActEarlyRepay = "early_repay"
	// P1 新增(§财商流P1-2 §3.5): 设置消费档位(0-3)。
	ActSetConsumption = "set_consumption"
	// P1 新增(§财商流P1-4 §8.1): 商业保险投保/退保(复用 Kind 字段,Action struct 零变更)。
	ActBuyInsurance    = "buy_insurance"
	ActCancelInsurance = "cancel_insurance"
)

// 信用贷档位面额(协议 §4:credit 档位必须是 50000/100000/200000 之一)。
var creditTierAmounts = map[int64]string{
	50000:  LoanCreditT1,
	100000: LoanCreditT2,
	200000: LoanCreditT3,
}

// 副业档位(协议 §4)。
type sideBizDef struct {
	GateCognition int
	Low, High     int64
}

var sideBizDefs = map[string]sideBizDef{
	"delivery":  {0, 2500, 4000},
	"content":   {2, 2000, 6000},
	"tutoring":  {4, 3000, 5000},
	"freelance": {3, 3000, 6000},
}

// ApplyAction 校验并执行座位动作(引擎层;phase/status 由房间层前置校验)。
// 成功返回人读结果文本(写入 LastActionText / game.event)。
func (w *World) ApplyAction(seat int, a Action) (string, *errcode.Error) {
	p := w.Players[seat]
	if p == nil {
		return "", errcode.Code(errcode.ErrWealthPlayerInactive)
	}
	if !p.Alive || p.StoppedMonths > 0 {
		return "", errcode.Code(errcode.ErrWealthPlayerInactive)
	}

	switch a.Type {
	case ActBuyAsset:
		return w.actBuyAsset(p, a)
	case ActSellAsset:
		return w.actSellAsset(p, a)
	case ActBuyHouse:
		return w.actBuyHouse(p, a)
	case ActTakeLoan:
		return w.actTakeLoan(p, a)
	case ActRepayLoan:
		return w.actRepayLoan(p, a)
	case ActStartSide:
		return w.actStartSide(p, a)
	case ActStopSide:
		return w.actStopSide(p)
	case ActStudy:
		return w.actStudy(p)
	case ActSocialize:
		return w.actSocialize(p)
	case ActRest:
		return w.actRest(p)
	case ActWorkOvertime:
		return w.actWorkOvertime(p)
	case ActMoveDistrict:
		return w.actMoveDistrict(p, a)
	case ActConsume:
		return w.actConsume(p, a)
	case ActDonate:
		return w.actDonate(p, a)
	case ActDeposit:
		return w.actDeposit(p, a)
	case ActWithdraw:
		return w.actWithdraw(p, a)
	case ActEarlyRepay:
		return w.actEarlyRepay(p, a)
	case ActSetConsumption:
		return w.actSetConsumption(p, a)
	case ActBuyInsurance:
		return w.actBuyInsurance(p, a)
	case ActCancelInsurance:
		return w.actCancelInsurance(p, a)
	default:
		return "", errcode.CodeMsg(errcode.ErrValidationFailed, "unknown wealth action: "+a.Type)
	}
}

// spendBudget 动作成功后统一扣预算 + 记公开动作文本。
func (w *World) spendBudget(p *Player, icon, text string) {
	if p.ActionBudget > 0 {
		p.ActionBudget--
	}
	p.LastActionText = text
	p.StatusIcon = icon
	w.emitEvent("action", p.Seat, text)
}

// ── buy_asset:stock_index / bond / gold,金额 ≥1000(gold ≥1 克价) ──
func (w *World) actBuyAsset(p *Player, a Action) (string, *errcode.Error) {
	if p.ActionBudget <= 0 {
		return "", errcode.Code(errcode.ErrWealthActionBudgetExhausted)
	}
	if a.AmountCNY < 1000 {
		return "", errcode.CodeMsg(errcode.ErrWealthAssetInvalid, "buy_asset amount_cny must be >= 1000")
	}
	switch a.Asset {
	case AssetStockIndex:
		units := math.Floor(float64(a.AmountCNY) / w.Market.StockIndex)
		if units < 1 {
			return "", errcode.CodeMsg(errcode.ErrWealthAssetInvalid, "amount too small for 1 unit")
		}
		cost := int64(units*w.Market.StockIndex + 0.5)
		fee := commissionStock(cost)
		if p.Cash < cost+fee {
			return "", errcode.Code(errcode.ErrWealthInsufficientCash)
		}
		w.Pay(p.Seat, SeatEntity(p.Seat), EntityMarket, cost+fee, CatBuy, "买入指数基金")
		if at := p.assetOf(AssetStockIndex); at != nil {
			at.Units += units
			at.CostCNY += cost
		} else {
			p.Assets = append(p.Assets, Asset{Kind: AssetStockIndex, Units: units, CostCNY: cost, OpenMonth: w.Month})
		}
		text := fmt.Sprintf("买入指数基金 %.0f 份(¥%d)", units, cost+fee)
		w.spendBudget(p, "trading", text)
		return text, nil
	case AssetBond:
		units := float64(a.AmountCNY) // 面值 1.00 元/份
		rate := w.Market.Params().BondRate
		if p.Cash < a.AmountCNY {
			return "", errcode.Code(errcode.ErrWealthInsufficientCash)
		}
		w.Pay(p.Seat, SeatEntity(p.Seat), EntityMarket, a.AmountCNY, CatBuy, "买入债券理财")
		if at := p.assetOf(AssetBond); at != nil {
			// 追加买入按加权平均锁定利率近似:新份额单独记账(拆分多头)。
			at.Units += units
			at.CostCNY += a.AmountCNY
			// 追加部分利率并入加权平均。
			oldRate := at.AssetRate()
			oldUnits := at.Units - units
			if oldUnits < 0 {
				oldUnits = 0
			}
			at.Extra["rate"] = (oldRate*oldUnits + rate*units) / at.Units
		} else {
			p.Assets = append(p.Assets, Asset{Kind: AssetBond, Units: units, CostCNY: a.AmountCNY, OpenMonth: w.Month,
				Extra: map[string]any{"rate": rate}})
		}
		text := fmt.Sprintf("买入债券 ¥%d(锁定年化 %.1f%%)", a.AmountCNY, rate*100)
		w.spendBudget(p, "trading", text)
		return text, nil
	case AssetGold:
		units := math.Floor(float64(a.AmountCNY) / w.Market.GoldPrice)
		if units < 1 {
			return "", errcode.CodeMsg(errcode.ErrWealthAssetInvalid, "amount too small for 1 gram")
		}
		cost := int64(units*w.Market.GoldPrice + 0.5)
		if p.Cash < cost {
			return "", errcode.Code(errcode.ErrWealthInsufficientCash)
		}
		w.Pay(p.Seat, SeatEntity(p.Seat), EntityMarket, cost, CatBuy, "买入黄金")
		if at := p.assetOf(AssetGold); at != nil {
			at.Units += units
			at.CostCNY += cost
		} else {
			p.Assets = append(p.Assets, Asset{Kind: AssetGold, Units: units, CostCNY: cost, OpenMonth: w.Month})
		}
		text := fmt.Sprintf("买入黄金 %.0f 克(¥%d)", units, cost)
		w.spendBudget(p, "trading", text)
		return text, nil
	default:
		return "", errcode.CodeMsg(errcode.ErrWealthAssetInvalid, "buy_asset asset must be stock_index|bond|gold")
	}
}

// commissionStock 股票佣金 0.025%,最低 5 元(I7)。
func commissionStock(amount int64) int64 {
	fee := int64(float64(amount)*0.00025 + 0.5)
	if fee < 5 {
		fee = 5
	}
	return fee
}

// ── sell_asset:金融资产 + 房产整售(units=1) ──
func (w *World) actSellAsset(p *Player, a Action) (string, *errcode.Error) {
	if p.ActionBudget <= 0 {
		return "", errcode.Code(errcode.ErrWealthActionBudgetExhausted)
	}
	if a.Units < 1 && !isHouseKind(a.Asset) && !isShopKind(a.Asset) {
		return "", errcode.CodeMsg(errcode.ErrWealthAssetInvalid, "units must be >= 1")
	}
	switch {
	case a.Asset == AssetStockIndex:
		at := p.assetOf(AssetStockIndex)
		if at == nil || at.Units < a.Units {
			return "", errcode.Code(errcode.ErrWealthAssetInvalid)
		}
		gross := int64(a.Units*w.Market.StockIndex + 0.5)
		fee := commissionStock(gross)
		w.Pay(p.Seat, EntityMarket, SeatEntity(p.Seat), gross, CatSell, "卖出指数基金")
		w.Pay(p.Seat, SeatEntity(p.Seat), EntityMarket, fee, CatFee, "卖出佣金")
		at.Units -= a.Units
		if at.Units <= 0.000001 {
			w.removeAsset(p, AssetStockIndex, 0)
		}
		text := fmt.Sprintf("卖出指数基金 %.0f 份,入账 ¥%d", a.Units, gross-fee)
		w.spendBudget(p, "trading", text)
		return text, nil
	case a.Asset == AssetBond:
		at := p.assetOf(AssetBond)
		if at == nil || at.Units < a.Units {
			return "", errcode.Code(errcode.ErrWealthAssetInvalid)
		}
		// 免费赎回(面值,协议 §4)。
		w.Pay(p.Seat, EntityMarket, SeatEntity(p.Seat), int64(a.Units+0.5), CatSell, "赎回债券理财")
		at.Units -= a.Units
		if at.Units <= 0.000001 {
			w.removeAsset(p, AssetBond, 0)
		}
		text := fmt.Sprintf("赎回债券 ¥%d", int64(a.Units+0.5))
		w.spendBudget(p, "trading", text)
		return text, nil
	case a.Asset == AssetGold:
		at := p.assetOf(AssetGold)
		if at == nil || at.Units < a.Units {
			return "", errcode.Code(errcode.ErrWealthAssetInvalid)
		}
		gross := int64(a.Units*w.Market.GoldPrice + 0.5)
		fee := int64(float64(gross)*0.005 + 0.5) // 0.5% 手续费(I7)
		w.Pay(p.Seat, EntityMarket, SeatEntity(p.Seat), gross, CatSell, "卖出黄金")
		w.Pay(p.Seat, SeatEntity(p.Seat), EntityMarket, fee, CatFee, "黄金手续费")
		at.Units -= a.Units
		if at.Units <= 0.000001 {
			w.removeAsset(p, AssetGold, 0)
		}
		text := fmt.Sprintf("卖出黄金 %.0f 克,入账 ¥%d", a.Units, gross-fee)
		w.spendBudget(p, "trading", text)
		return text, nil
	case isHouseKind(a.Asset) || isShopKind(a.Asset):
		return w.sellProperty(p, kindDistrict(a.Asset), isShopKind(a.Asset))
	default:
		return "", errcode.CodeMsg(errcode.ErrWealthAssetInvalid, "unknown asset kind")
	}
}

// kindDistrict 从 "house:<d>"/"shop:<d>" 解出城区。
func kindDistrict(kind string) string {
	if i := strings.IndexByte(kind, ':'); i >= 0 {
		return kind[i+1:]
	}
	return ""
}

// removeAsset 删除第 n 笔指定 kind 持仓。
func (w *World) removeAsset(p *Player, kind string, n int) {
	idx, seen := -1, 0
	for i := range p.Assets {
		if p.Assets[i].Kind == kind {
			if seen == n {
				idx = i
				break
			}
			seen++
		}
	}
	if idx < 0 {
		return
	}
	p.Assets = append(p.Assets[:idx], p.Assets[idx+1:]...)
}

// propertyIndexOf 找到城区 d 的第 n 笔住宅/商铺持仓下标。
func propertyIndexOf(p *Player, kind, d string, n int) int {
	seen := 0
	for i := range p.Assets {
		if p.Assets[i].Kind == kind && p.Assets[i].AssetDistrict() == d {
			if seen == n {
				return i
			}
			seen++
		}
	}
	return -1
}

// sellProperty 房产/商铺整售:增值税 5% + 中介 2%(满 5 年唯一免增值税);
// 卖房先偿房贷(余额部分),余款入现金。
func (w *World) sellProperty(p *Player, district string, shop bool) (string, *errcode.Error) {
	if p.ActionBudget <= 0 {
		return "", errcode.Code(errcode.ErrWealthActionBudgetExhausted)
	}
	kind := AssetKindHouse(district)
	label := "住宅"
	if shop {
		kind = AssetKindShop(district)
		label = "商铺"
	}
	idx := propertyIndexOf(p, kind, district, 0)
	if idx < 0 {
		return "", errcode.Code(errcode.ErrWealthAssetInvalid)
	}
	at := &p.Assets[idx]
	price := w.Market.HousePrice(district)

	// 满 5 年唯一免增值税:持有 ≥60 月 且 名下唯一住宅(商铺不适用)。
	vatFree := false
	if !shop {
		held := w.Month - at.OpenMonth
		onlyHouse := p.houseCount() == 1
		vatFree = held >= 60 && onlyHouse
	}
	tax := int64(0)
	if !vatFree {
		tax = int64(float64(price) * 0.05)
	}
	agentFee := int64(float64(price) * 0.02)
	net := price - tax - agentFee

	// Ledger:market→seat(gross) + seat→gov(tax) + seat→market(中介)。
	w.Pay(p.Seat, EntityMarket, SeatEntity(p.Seat), price, CatSell, "出售"+label)
	if tax > 0 {
		w.Pay(p.Seat, SeatEntity(p.Seat), EntityGov, tax, CatTax, "房产增值税")
	}
	w.Pay(p.Seat, SeatEntity(p.Seat), EntityMarket, agentFee, CatFee, "中介费")

	// 先偿房贷(住宅卖出时):净款优先偿还 mortgage,余款留在现金。
	repaid := int64(0)
	if !shop {
		for i := range p.Loans {
			loan := &p.Loans[i]
			if loan.Kind != LoanMortgage || loan.Balance <= 0 || net-repaid <= 0 {
				continue
			}
			pay := loan.Balance
			if pay > net-repaid {
				pay = net - repaid
			}
			loan.Balance -= pay
			repaid += pay
			w.Pay(p.Seat, SeatEntity(p.Seat), EntityBank, pay, CatRepay, "卖房偿贷")
			if loan.Balance <= 0 {
				w.removeLoan(p, loan.ID)
			}
		}
	}
	// 商铺贷款关联 P0 不做(全款购置),net 已入现金。

	wasSelfOccupied := at.IsSelfOccupied()
	w.removeAssetAt(p, idx)
	if wasSelfOccupied {
		p.HomeDistrict = p.District // 回到租房状态
	}
	text := fmt.Sprintf("出售%s%s ¥%d(税¥%d 中介¥%d%s)",
		DistrictCN(district), label, price, tax, agentFee, repaySuffix(repaid))
	w.spendBudget(p, "trading", text)
	return text, nil
}

func repaySuffix(repaid int64) string {
	if repaid > 0 {
		return fmt.Sprintf(",偿还房贷 ¥%d", repaid)
	}
	return ""
}

func (w *World) removeAssetAt(p *Player, idx int) {
	p.Assets = append(p.Assets[:idx], p.Assets[idx+1:]...)
}

func (w *World) removeLoan(p *Player, id string) {
	for i := range p.Loans {
		if p.Loans[i].ID == id {
			p.Loans = append(p.Loans[:i], p.Loans[i+1:]...)
			return
		}
	}
}

// ── buy_house:首付 ≥30% + 30 年房贷;asset="shop" 时全款购铺(P0 偏差见协议文档) ──
func (w *World) actBuyHouse(p *Player, a Action) (string, *errcode.Error) {
	if p.ActionBudget <= 0 {
		return "", errcode.Code(errcode.ErrWealthActionBudgetExhausted)
	}
	if !ValidDistrict(a.District) {
		return "", errcode.CodeMsg(errcode.ErrWealthAssetInvalid, "unknown district")
	}
	if a.DownpayRatio < 0.3 || a.DownpayRatio > 1.0 {
		return "", errcode.CodeMsg(errcode.ErrWealthAssetInvalid, "downpay_ratio must be in [0.3,1.0]")
	}
	if a.Asset == "" {
		a.Asset = "house"
	}
	if a.Asset != "house" && a.Asset != "shop" {
		return "", errcode.CodeMsg(errcode.ErrWealthAssetInvalid, "asset must be house|shop")
	}
	price := w.Market.HousePrice(a.District)
	downpay := int64(float64(price)*a.DownpayRatio + 0.5)

	if a.Asset == "house" {
		if p.houseCount() >= MaxHouses {
			return "", errcode.CodeMsg(errcode.ErrWealthAssetInvalid, "house holdings capped at 4")
		}
		loanAmount := price - downpay
		if loanAmount > 0 {
			if p.CreditScore < 600 {
				return "", errcode.Code(errcode.ErrWealthLoanInvalid)
			}
			if p.CreditGrade() == "E" {
				return "", errcode.Code(errcode.ErrWealthLoanInvalid)
			}
			// P1: 房贷利率 = CB 内生(LPR5Y + 0.5% + CreditSpread);CB nil 回退 PhaseTable。
			rate := w.Market.Params().LPR + MortgageSpread + p.CreditMarkup()
			if w.CB != nil {
				rate = w.CB.MortgageRate() + p.CreditMarkup()
			}
			payment := AnnuityPayment(loanAmount, rate, 360)
			if p.Cash < downpay {
				return "", errcode.Code(errcode.ErrWealthInsufficientCash)
			}
			w.Pay(p.Seat, SeatEntity(p.Seat), EntityMarket, downpay, CatBuy, "购房首付")
			w.Pay(p.Seat, EntityBank, SeatEntity(p.Seat), loanAmount, CatLoan, "房贷放款")
			// P1: 锁定加点 = 银行侧加点(MortgageSpread + CreditSpread)(不含玩家信用加点,
			// 玩家在重定价时按最新信用等级重新计算),用于 LPR 重定价(v2.60 N12-3)。
			lprRef := w.Market.Params().LPR
			if w.CB != nil {
				lprRef = w.CB.ComputeLPR()
			}
			p.Loans = append(p.Loans, Loan{
				ID: p.nextLoanID(), Kind: LoanMortgage, Principal: loanAmount, Balance: loanAmount,
				AnnualRate: rate, MonthlyPayment: payment, TermN: 360, MonthsLeft: 360,
				OrigSpread: rate - lprRef - p.CreditMarkup(), RateFixed: false,
			})
			// P1: 明斯基分级 + 利率调整(v2.60 N11-4)。
			w.recordMinskyForLoan(p, &p.Loans[len(p.Loans)-1])
			w.applyMinskyRateAdjustment(p, &p.Loans[len(p.Loans)-1])
			p.Assets = append(p.Assets, w.newPropertyAsset(AssetKindHouse(a.District), price))
			w.autoSelfOccupy(p)
			text := fmt.Sprintf("购入%s住宅 ¥%d(首付 ¥%d,月供 ¥%d)",
				DistrictCN(a.District), price, downpay, payment)
			w.spendBudget(p, "trading", text)
			return text, nil
		}
		// 全款。
		if p.Cash < price {
			return "", errcode.Code(errcode.ErrWealthInsufficientCash)
		}
		w.Pay(p.Seat, SeatEntity(p.Seat), EntityMarket, price, CatBuy, "全款购房")
		p.Assets = append(p.Assets, w.newPropertyAsset(AssetKindHouse(a.District), price))
		w.autoSelfOccupy(p)
		text := fmt.Sprintf("全款购入%s住宅 ¥%d", DistrictCN(a.District), price)
		w.spendBudget(p, "trading", text)
		return text, nil
	}

	// 商铺:P0 仅全款(经营贷额度与商铺价格差距悬殊,协议文档已同步标注)。
	if p.shopCount() >= MaxShops {
		return "", errcode.CodeMsg(errcode.ErrWealthAssetInvalid, "shop holdings capped at 4")
	}
	if a.DownpayRatio < 1.0 {
		return "", errcode.CodeMsg(errcode.ErrWealthAssetInvalid, "shop requires full payment in P0")
	}
	if p.Cash < price {
		return "", errcode.Code(errcode.ErrWealthInsufficientCash)
	}
	w.Pay(p.Seat, SeatEntity(p.Seat), EntityMarket, price, CatBuy, "全款购铺")
	p.Assets = append(p.Assets, w.newPropertyAsset(AssetKindShop(a.District), price))
	text := fmt.Sprintf("全款购入%s商铺 ¥%d", DistrictCN(a.District), price)
	w.spendBudget(p, "trading", text)
	return text, nil
}

func (w *World) newPropertyAsset(kind string, price int64) Asset {
	return Asset{
		Kind: kind, Units: 1, CostCNY: price, OpenMonth: w.Month,
		Extra: map[string]any{"district": districtOfKind(kind), "self_occupied": false},
	}
}

func districtOfKind(kind string) string { return kindDistrict(kind) }

// autoSelfOccupy 自住判定(P0 新定):当前无自住房(租房)→ 最新购入住宅自住。
func (w *World) autoSelfOccupy(p *Player) {
	if p.selfOccupiedHouse() != nil {
		return
	}
	for i := len(p.Assets) - 1; i >= 0; i-- {
		if isHouseKind(p.Assets[i].Kind) {
			p.Assets[i].SetSelfOccupied(true)
			p.HomeDistrict = p.Assets[i].AssetDistrict()
			return
		}
	}
}

// ── take_loan:consumer / credit(三档) / business ──
func (w *World) actTakeLoan(p *Player, a Action) (string, *errcode.Error) {
	if p.ActionBudget <= 0 {
		return "", errcode.Code(errcode.ErrWealthActionBudgetExhausted)
	}
	if p.CreditGrade() == "E" {
		return "", errcode.Code(errcode.ErrWealthLoanInvalid)
	}
	// P1: 信贷约束参数(CB 为 nil 时回退宽松)。
	quotaFactor := 1.0
	creditSpread := 0.0
	creditThreshold := 500
	if w.CB != nil {
		quotaFactor = w.CB.LoanQuotaFactor
		creditSpread = w.CB.CreditSpread
		if w.CB.CreditTightness > 0 {
			creditThreshold += CreditScoreBonus
		}
	}
	switch a.Kind {
	case "consumer":
		if a.AmountCNY <= 0 {
			return "", errcode.Code(errcode.ErrWealthLoanInvalid)
		}
		if p.OverdueCount > 0 {
			return "", errcode.CodeMsg(errcode.ErrWealthLoanInvalid, "overdue record blocks consumer loan")
		}
		if p.CreditScore < creditThreshold {
			return "", errcode.CodeMsg(errcode.ErrWealthLoanInvalid, "credit score below threshold")
		}
		cap := p.monthlyIncomeEstimate() * 12
		if cap > 200000 {
			cap = 200000
		}
		cap = int64(float64(cap) * quotaFactor)
		if a.AmountCNY > cap {
			return "", errcode.CodeMsg(errcode.ErrWealthLoanInvalid, "consumer loan cap exceeded")
		}
		rate := ConsumerRate + p.CreditMarkup() + creditSpread
		payment := AnnuityPayment(a.AmountCNY, rate, 36)
		w.Pay(p.Seat, EntityBank, SeatEntity(p.Seat), a.AmountCNY, CatLoan, "消费贷放款")
		p.Loans = append(p.Loans, Loan{
			ID: p.nextLoanID(), Kind: LoanConsumer, Principal: a.AmountCNY, Balance: a.AmountCNY,
			AnnualRate: rate, MonthlyPayment: payment, TermN: 36, MonthsLeft: 36,
		})
		// P1: 明斯基分级 + 利率调整(v2.60 N11-4)。
		w.recordMinskyForLoan(p, &p.Loans[len(p.Loans)-1])
		w.applyMinskyRateAdjustment(p, &p.Loans[len(p.Loans)-1])
		text := fmt.Sprintf("借入消费贷 ¥%d(月供 ¥%d)", a.AmountCNY, payment)
		w.spendBudget(p, "trading", text)
		return text, nil
	case "credit":
		kind, ok := creditTierAmounts[a.AmountCNY]
		if !ok {
			return "", errcode.CodeMsg(errcode.ErrWealthLoanInvalid, "credit amount must be 50000|100000|200000")
		}
		if p.CreditScore < creditThreshold {
			return "", errcode.Code(errcode.ErrWealthLoanInvalid)
		}
		// P1: 额度收紧(实际放款 = 档位 × quotaFactor,最低 50000)。
		amount := int64(float64(a.AmountCNY) * quotaFactor)
		if amount < 50000 {
			amount = 50000
		}
		var monthly float64
		switch kind {
		case LoanCreditT1:
			monthly = 0.008
		case LoanCreditT2:
			monthly = 0.012
		default:
			monthly = 0.018
		}
		w.Pay(p.Seat, EntityBank, SeatEntity(p.Seat), amount, CatLoan, "信用贷放款")
		p.Loans = append(p.Loans, Loan{
			ID: p.nextLoanID(), Kind: kind, Principal: amount, Balance: amount,
			AnnualRate: monthly * 12, MonthlyPayment: int64(float64(amount)*monthly + 0.5),
			TermN: 36, MonthsLeft: 36, InterestOnly: true, LumpAtMaturity: true,
		})
		// P1: 明斯基分级 + 利率调整(v2.60 N11-4)。
		w.recordMinskyForLoan(p, &p.Loans[len(p.Loans)-1])
		w.applyMinskyRateAdjustment(p, &p.Loans[len(p.Loans)-1])
		text := fmt.Sprintf("借入信用贷 ¥%d(月息 %.1f%%)", amount, monthly*100)
		w.spendBudget(p, "trading", text)
		return text, nil
	case "business":
		if p.SideBusiness == nil {
			return "", errcode.CodeMsg(errcode.ErrWealthLoanInvalid, "business loan requires running side business")
		}
		if a.AmountCNY <= 0 || a.AmountCNY > 200000 {
			return "", errcode.CodeMsg(errcode.ErrWealthLoanInvalid, "business loan cap is 200000")
		}
		if p.CreditScore < creditThreshold {
			return "", errcode.CodeMsg(errcode.ErrWealthLoanInvalid, "credit score below threshold")
		}
		cap := int64(200000 * quotaFactor)
		if a.AmountCNY > cap {
			return "", errcode.CodeMsg(errcode.ErrWealthLoanInvalid, "business loan cap exceeded")
		}
		rate := w.Market.Params().LPR + BusinessSpread + p.CreditMarkup() + creditSpread
		if rate < 0.01 {
			rate = 0.01
		}
		w.Pay(p.Seat, EntityBank, SeatEntity(p.Seat), a.AmountCNY, CatLoan, "经营贷放款")
		p.Loans = append(p.Loans, Loan{
			ID: p.nextLoanID(), Kind: LoanBusiness, Principal: a.AmountCNY, Balance: a.AmountCNY,
			AnnualRate: rate, MonthlyPayment: int64(float64(a.AmountCNY)*rate/12 + 0.5),
			TermN: 60, MonthsLeft: 60, InterestOnly: true,
		})
		// P1: 明斯基分级 + 利率调整(v2.60 N11-4)。
		w.recordMinskyForLoan(p, &p.Loans[len(p.Loans)-1])
		w.applyMinskyRateAdjustment(p, &p.Loans[len(p.Loans)-1])
		text := fmt.Sprintf("借入经营贷 ¥%d(月息 ¥%d)", a.AmountCNY, int64(float64(a.AmountCNY)*rate/12+0.5))
		w.spendBudget(p, "trading", text)
		return text, nil
	default:
		return "", errcode.CodeMsg(errcode.ErrWealthLoanInvalid, "loan kind must be consumer|credit|business")
	}
}

// ── repay_loan:提前还本 ≥1 万或结清;等额本息重算月供 ──
func (w *World) actRepayLoan(p *Player, a Action) (string, *errcode.Error) {
	if p.ActionBudget <= 0 {
		return "", errcode.Code(errcode.ErrWealthActionBudgetExhausted)
	}
	loan := p.loanByID(a.LoanID)
	if loan == nil || loan.Balance <= 0 {
		return "", errcode.CodeMsg(errcode.ErrWealthLoanInvalid, "loan not found")
	}
	if a.AmountCNY < 10000 && a.AmountCNY < loan.Balance {
		return "", errcode.CodeMsg(errcode.ErrWealthLoanInvalid, "early repayment must be >= 10000 or full settlement")
	}
	pay := a.AmountCNY
	if pay > loan.Balance {
		pay = loan.Balance
	}
	if p.Cash < pay {
		return "", errcode.Code(errcode.ErrWealthInsufficientCash)
	}
	w.Pay(p.Seat, SeatEntity(p.Seat), EntityBank, pay, CatRepay, "提前还本 "+loan.ID)
	loan.Balance -= pay
	if loan.Balance <= 0 {
		w.removeLoan(p, loan.ID)
		text := fmt.Sprintf("结清贷款 %s ¥%d", a.LoanID, pay)
		w.spendBudget(p, "trading", text)
		return text, nil
	}
	if !loan.InterestOnly {
		// 等额本息重算月供(期限不变)。
		loan.MonthlyPayment = AnnuityPayment(loan.Balance, loan.AnnualRate, loan.MonthsLeft)
	}
	text := fmt.Sprintf("提前还款 %s ¥%d(余额 ¥%d)", a.LoanID, pay, loan.Balance)
	w.spendBudget(p, "trading", text)
	return text, nil
}

// applyMinskyRateAdjustment 庞氏等级:月供实际减免 0.5%(诱人陷阱);投机 +0.5%。
// 等额本息贷款重算月供(调用方已持锁)。
func (w *World) applyMinskyRateAdjustment(p *Player, loan *Loan) {
	if loan == nil || p == nil {
		return
	}
	ms := p.MinskyByLoan[loan.ID]
	if ms == nil {
		return
	}
	adj := MinskyRateAdjustment(ms.Tier)
	if adj == 0 {
		return
	}
	newRate := loan.AnnualRate + adj
	if newRate < 0.001 {
		newRate = 0.001
	}
	loan.AnnualRate = newRate
	if !loan.InterestOnly {
		loan.MonthlyPayment = AnnuityPayment(loan.Balance, newRate, loan.MonthsLeft)
	}
}

// ── early_repay:提前还款(仅房贷,全额或部分 + 违约金 1-3%)(v2.60 N12-5) ──

// actEarlyRepay 提前还款(v2.60 N12-5)。
// 参数: loan_id, amount_cny(0/"all"=全部,正数=部分)。
// 1 年内提前还款罚息 1-3%(线性化);仅房贷允许。
func (w *World) actEarlyRepay(p *Player, a Action) (string, *errcode.Error) {
	if p.ActionBudget <= 0 {
		return "", errcode.Code(errcode.ErrWealthActionBudgetExhausted)
	}
	loan := p.loanByID(a.LoanID)
	if loan == nil || loan.Balance <= 0 {
		return "", errcode.Code(errcode.ErrLoanNotFound)
	}
	if loan.Kind != LoanMortgage {
		return "", errcode.Code(errcode.ErrEarlyRepayOnlyMortgage)
	}
	// 1 年内提前还款罚息 1-3%(线性化:开放时点距今 <12 月才收)。
	monthsSinceOpen := loan.TermN - loan.MonthsLeft
	var penaltyRate float64
	if monthsSinceOpen < 12 {
		// 1% → 3% 线性(距今 0 月 ≈ 3%, 12 月 ≈ 0)。
		penaltyRate = 0.01 + float64(12-monthsSinceOpen)*0.00167
		if penaltyRate > 0.03 {
			penaltyRate = 0.03
		}
	}
	// 还款金额(≤0 或 ≥余额 = 全部还清)。
	var payAmount int64
	if a.AmountCNY <= 0 || a.AmountCNY >= loan.Balance {
		payAmount = loan.Balance
	} else {
		payAmount = a.AmountCNY
	}
	penalty := int64(float64(payAmount) * penaltyRate)
	totalPay := payAmount + penalty
	if p.Cash < totalPay {
		return "", errcode.CodeMsg(errcode.ErrCashNotEnoughRepay,
			fmt.Sprintf("现金不足(需 %d 元,含违约金 %d 元)", totalPay, penalty))
	}
	w.Pay(p.Seat, SeatEntity(p.Seat), EntityBank, totalPay, CatRepay, "early_repay")
	loan.Balance -= payAmount
	if loan.Balance <= 0 {
		p.removeLoan(a.LoanID)
		savedInterest := w.estimateSavedInterest(loan)
		text := fmt.Sprintf("🎉 房贷 %s 已结清！节省利息约 ¥%d", a.LoanID, savedInterest)
		w.spendBudget(p, "trading", text)
		return text, nil
	}
	// 部分还款后重算月供(期限不变)。
	loan.MonthlyPayment = AnnuityPayment(loan.Balance, loan.AnnualRate, loan.MonthsLeft)
	text := fmt.Sprintf("提前还款 %s ¥%d(违约金 %d 元,剩余余额 ¥%d,新月供 ¥%d)",
		a.LoanID, payAmount, penalty, loan.Balance, loan.MonthlyPayment)
	w.spendBudget(p, "trading", text)
	return text, nil
}

// estimateSavedInterest 估算结清贷款节省的利息(剩余月供总和 − 剩余余额,最低 0)。
func (w *World) estimateSavedInterest(loan *Loan) int64 {
	if loan == nil {
		return 0
	}
	total := loan.MonthlyPayment * int64(loan.MonthsLeft)
	saved := total - loan.Balance
	if saved < 0 {
		return 0
	}
	return saved
}

// ── 副业 ──
func (w *World) actStartSide(p *Player, a Action) (string, *errcode.Error) {
	if p.ActionBudget <= 0 {
		return "", errcode.Code(errcode.ErrWealthActionBudgetExhausted)
	}
	def, ok := sideBizDefs[a.Kind]
	if !ok {
		return "", errcode.CodeMsg(errcode.ErrWealthGateFailed, "side business kind must be delivery|content|tutoring|freelance")
	}
	if p.SideBusiness != nil {
		return "", errcode.CodeMsg(errcode.ErrWealthGateFailed, "side business already running")
	}
	if p.Cognition < def.GateCognition {
		return "", errcode.Code(errcode.ErrWealthGateFailed)
	}
	base := (def.Low + def.High) / 2
	p.SideBusiness = &SideBusiness{Kind: a.Kind, BaseIncome: base, OpenedMonth: w.Month}
	p.Assets = append(p.Assets, Asset{Kind: AssetSideBusiness, Units: 1, CostCNY: 0, OpenMonth: w.Month,
		Extra: map[string]any{"kind": a.Kind}})
	text := fmt.Sprintf("启动副业(%s),月入约 ¥%d–%d", sideBizCN(a.Kind), def.Low, def.High)
	w.spendBudget(p, "working", text)
	return text, nil
}

func sideBizCN(kind string) string {
	switch kind {
	case "delivery":
		return "跑腿配送"
	case "content":
		return "内容创作"
	case "tutoring":
		return "家教"
	default:
		return "自由接单"
	}
}

func (w *World) actStopSide(p *Player) (string, *errcode.Error) {
	if p.ActionBudget <= 0 {
		return "", errcode.Code(errcode.ErrWealthActionBudgetExhausted)
	}
	if p.SideBusiness == nil {
		return "", errcode.CodeMsg(errcode.ErrWealthGateFailed, "no running side business")
	}
	w.removeAsset(p, AssetSideBusiness, 0)
	p.SideBusiness = nil
	text := "停掉副业"
	w.spendBudget(p, "working", text)
	return text, nil
}

// ── 五维动作 ──
func (w *World) actStudy(p *Player) (string, *errcode.Error) {
	if p.ActionBudget <= 0 {
		return "", errcode.Code(errcode.ErrWealthActionBudgetExhausted)
	}
	if p.Cognition >= 10 {
		return "", errcode.Code(errcode.ErrWealthGateFailed)
	}
	if p.Energy < 1 {
		return "", errcode.Code(errcode.ErrWealthGateFailed)
	}
	if p.Cash < 2000 {
		return "", errcode.Code(errcode.ErrWealthInsufficientCash)
	}
	w.Pay(p.Seat, SeatEntity(p.Seat), w.consumerPayTo(), 2000, CatStudy, "学习进修")
	p.Energy--
	p.Cognition++
	text := "学习进修(¥2000,认知 +1)"
	w.spendBudget(p, "resting", text)
	return text, nil
}

func (w *World) actSocialize(p *Player) (string, *errcode.Error) {
	if p.ActionBudget <= 0 {
		return "", errcode.Code(errcode.ErrWealthActionBudgetExhausted)
	}
	if p.Network >= 10 {
		return "", errcode.Code(errcode.ErrWealthGateFailed)
	}
	if p.Cash < 1000 {
		return "", errcode.Code(errcode.ErrWealthInsufficientCash)
	}
	w.Pay(p.Seat, SeatEntity(p.Seat), w.consumerPayTo(), 1000, CatSocialEv, "社交应酬")
	p.Network++
	text := "社交应酬(¥1000,人脉 +1)"
	w.spendBudget(p, "resting", text)
	return text, nil
}

func (w *World) actRest(p *Player) (string, *errcode.Error) {
	if p.ActionBudget <= 0 {
		return "", errcode.Code(errcode.ErrWealthActionBudgetExhausted)
	}
	if p.Energy >= 10 {
		return "", errcode.Code(errcode.ErrWealthGateFailed)
	}
	p.Energy += 2
	if p.Energy > 10 {
		p.Energy = 10
	}
	text := "休整(精力 +2)"
	w.spendBudget(p, "resting", text)
	return text, nil
}

func (w *World) actWorkOvertime(p *Player) (string, *errcode.Error) {
	if p.ActionBudget <= 0 {
		return "", errcode.Code(errcode.ErrWealthActionBudgetExhausted)
	}
	if p.Energy < 2 {
		return "", errcode.Code(errcode.ErrWealthGateFailed)
	}
	if p.UnemployedMonths > 0 {
		return "", errcode.CodeMsg(errcode.ErrWealthGateFailed, "overtime requires employment")
	}
	p.Energy -= 2
	p.OvertimeThisMonth = true
	text := "加班(精力 −2,本月奖金 = 工资×0.3)"
	w.spendBudget(p, "working", text)
	return text, nil
}

func (w *World) actMoveDistrict(p *Player, a Action) (string, *errcode.Error) {
	if p.ActionBudget <= 0 {
		return "", errcode.Code(errcode.ErrWealthActionBudgetExhausted)
	}
	if !ValidDistrict(a.District) {
		return "", errcode.CodeMsg(errcode.ErrWealthAssetInvalid, "unknown district")
	}
	if a.District == p.District {
		return "", errcode.CodeMsg(errcode.ErrWealthAssetInvalid, "already in this district")
	}
	if p.Energy < 1 {
		return "", errcode.Code(errcode.ErrWealthGateFailed)
	}
	if p.Cash < 3000 {
		return "", errcode.Code(errcode.ErrWealthInsufficientCash)
	}
	w.Pay(p.Seat, SeatEntity(p.Seat), w.consumerPayTo(), 3000, CatMoving, "迁区搬家")
	p.Energy--
	p.District = a.District
	// 目标区有自住房 → 自动改自住(P0 新定);否则租房跟随当前区。
	if p.selfOccupiedHouse() == nil {
		p.HomeDistrict = a.District
	} else if p.selfOccupiedHouse().AssetDistrict() == a.District {
		// 已在该区自住,无事。
	} else {
		// 目标区有自有房产 → 迁入自住(释放原自住)。
		w.switchSelfOccupy(p, a.District)
	}
	text := "迁居" + DistrictCN(a.District)
	w.emitEvent("move", p.Seat, fmt.Sprintf("%d 号位迁居%s", p.Seat, DistrictCN(a.District)))
	w.spendBudget(p, "moved", text)
	return text, nil
}

// switchSelfOccupy 把自住标记迁到目标区的自有住宅(无则保持现状)。
func (w *World) switchSelfOccupy(p *Player, district string) {
	cur := p.selfOccupiedHouse()
	for i := range p.Assets {
		a := &p.Assets[i]
		if isHouseKind(a.Kind) && a.AssetDistrict() == district && !a.IsSelfOccupied() {
			if cur != nil {
				cur.SetSelfOccupied(false)
			}
			a.SetSelfOccupied(true)
			p.HomeDistrict = district
			return
		}
	}
}

func (w *World) actConsume(p *Player, a Action) (string, *errcode.Error) {
	if p.ActionBudget <= 0 {
		return "", errcode.Code(errcode.ErrWealthActionBudgetExhausted)
	}
	if a.AmountCNY < 1 {
		return "", errcode.CodeMsg(errcode.ErrWealthAssetInvalid, "consume amount_cny must be >= 1")
	}
	if p.Cash < a.AmountCNY {
		return "", errcode.Code(errcode.ErrWealthInsufficientCash)
	}
	note := a.Reason
	if len([]rune(note)) > 40 {
		note = string([]rune(note)[:40])
	}
	if note == "" {
		note = "自由消费"
	}
	w.Pay(p.Seat, SeatEntity(p.Seat), w.consumerPayTo(), a.AmountCNY, CatConsume, note)
	// P1: 自由消费同时计入消费篮子 misc(与步骤5 恩格尔分配同口径,§3.6)。
	if p.ConsumptionByGoods == nil {
		p.ConsumptionByGoods = map[string]float64{}
	}
	p.ConsumptionByGoods["misc"] += float64(a.AmountCNY)
	text := fmt.Sprintf("消费 ¥%d(%s)", a.AmountCNY, note)
	w.spendBudget(p, "idle", text)
	return text, nil
}

func (w *World) actDonate(p *Player, a Action) (string, *errcode.Error) {
	if p.ActionBudget <= 0 {
		return "", errcode.Code(errcode.ErrWealthActionBudgetExhausted)
	}
	if a.AmountCNY < 1000 {
		return "", errcode.CodeMsg(errcode.ErrWealthAssetInvalid, "donate amount_cny must be >= 1000")
	}
	if p.Cash < a.AmountCNY {
		return "", errcode.Code(errcode.ErrWealthInsufficientCash)
	}
	w.Pay(p.Seat, SeatEntity(p.Seat), EntityWorld, a.AmountCNY, CatDonate, "公益捐赠")
	p.DonationTotalCNY += a.AmountCNY
	p.DonationCount++
	if p.DonationCount <= 3 && p.Network < 10 {
		p.Network++ // 累计前 3 次捐赠人脉 +1(《规则》§2.5)
	}
	text := fmt.Sprintf("捐赠 ¥%d(累计 ¥%d)", a.AmountCNY, p.DonationTotalCNY)
	w.spendBudget(p, "idle", text)
	return text, nil
}

// ── deposit / withdraw:活期 ↔ 定期(P1) ──
// 定期存款利率 1.5%/年;提前支取损失全部利息(简化:支取时无利息)。

// actDeposit 活期 → 定期转账。
func (w *World) actDeposit(p *Player, a Action) (string, *errcode.Error) {
	if p.ActionBudget <= 0 {
		return "", errcode.Code(errcode.ErrWealthActionBudgetExhausted)
	}
	if a.AmountCNY <= 0 {
		return "", errcode.CodeMsg(errcode.ErrWealthAssetInvalid, "deposit amount_cny must be > 0")
	}
	if p.Cash < a.AmountCNY {
		return "", errcode.Code(errcode.ErrWealthInsufficientCash)
	}
	p.Cash -= a.AmountCNY
	p.SavingsDeposit += a.AmountCNY
	text := fmt.Sprintf("活期转定期 ¥%d(年利率 1.5%%)", a.AmountCNY)
	w.spendBudget(p, "trading", text)
	return text, nil
}

// actWithdraw 定期 → 活期(提前支取损失全部利息,简化:无利息)。
func (w *World) actWithdraw(p *Player, a Action) (string, *errcode.Error) {
	if p.ActionBudget <= 0 {
		return "", errcode.Code(errcode.ErrWealthActionBudgetExhausted)
	}
	if a.AmountCNY <= 0 {
		return "", errcode.CodeMsg(errcode.ErrWealthAssetInvalid, "withdraw amount_cny must be > 0")
	}
	if p.SavingsDeposit < a.AmountCNY {
		return "", errcode.CodeMsg(errcode.ErrWealthInsufficientCash, "savings deposit insufficient")
	}
	p.SavingsDeposit -= a.AmountCNY
	p.Cash += a.AmountCNY
	text := fmt.Sprintf("定期转活期 ¥%d(提前支取,利息损失)", a.AmountCNY)
	w.spendBudget(p, "trading", text)
	return text, nil
}

// ── set_consumption: 消费档位(P1 §财商流P1-2 §3.5) ──

// actSetConsumption 设置消费档位(耗 1 次动作预算;立即生效,本月月结按新档位
// 结算)。人类走 WS game.wealth_action {type:"set_consumption", level},与 Agent
// 同一 ApplyAction 路径。
func (w *World) actSetConsumption(p *Player, a Action) (string, *errcode.Error) {
	if p.ActionBudget <= 0 {
		return "", errcode.Code(errcode.ErrWealthActionBudgetExhausted)
	}
	if a.Level < 0 || a.Level > 3 {
		return "", errcode.Code(errcode.ErrWealthConsumptionLevelInvalid) // 35020
	}
	p.ConsumptionLevel = a.Level
	text := fmt.Sprintf("调整消费档位:%s(支出×%.1f,精力%+d)",
		consumptionLevelCN(a.Level), consumptionLevelMult[a.Level], consumptionLevelEnergy[a.Level])
	w.spendBudget(p, "idle", text)
	return text, nil
}
