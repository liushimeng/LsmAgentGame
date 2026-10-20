// Package virtual_city — actions.go: 动作校验与执行(2026-09-14 §财商流P0)。
//
// 唯一事实来源: 协议契约文档 §4 动作语义表。人类按钮(WS game.virtual_city_action)、
// Agent 工具(vcplayer DispatchTool)与本文件三方共用同一代码路径,无分叉。
// 通用校验①(status/phase)与④(观战者)在房间层;②③(alive/budget)在此复核。
package virtual_city

import (
	"fmt"
	"strings"

	"LsmAgentGame/errcode"
)

// Action 是 game.virtual_city_action 的载荷(人类与 Agent 共用)。
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
	// move(§CityHuman重构): walk|run(区内) / bus|metro|taxi(跨城区)
	Mode string `json:"mode,omitempty"`
	// set_side_price / start_side_business(批次20 文档2):定价档位
	// 0=中价(缺省,兼容旧客户端/旧 Agent) 1=低价 2=高价。
	Tier int `json:"tier,omitempty"`
	// upgrade_education(批次52 §2 裁决 D11/D13):子女序下标(0 起)。
	ChildIdx int `json:"child_idx,omitempty"`
	// reflect(批次62 §4.3):反思模式 light|deep|preview。
	ReflectMode string `json:"reflect_mode,omitempty"`
	// defer_payment(批次62 §3.3.1):延期支付的备注/用途标签。
	PaymentTag string `json:"payment_tag,omitempty"`
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
	// ActMove 统一移动(2026-09-22 §CityHuman重构):区内 walk/run 或跨城区
	// bus/metro/taxi;跨城区语义替代 move_district(后者保留兼容)。
	ActMove        = "move"
	ActConsume     = "consume"
	ActDonate      = "donate"
	ActSubmitMonth = "submit_month"
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
	// 批次20 新增(文档2 §3):副业改价 { "tier": 0|1|2 },预算 1(working)。
	ActSetSidePrice = "set_side_price"
	// 批次52 新增(§2 裁决 D13):代际财富两动作 {amount_cny}/{child_idx},预算各 1。
	ActPaySupportExtra  = "pay_support_extra"
	ActUpgradeEducation = "upgrade_education"
	// 批次62 新增(§4.3 主动反思 / §3.3.1 延期支付 / §3.2.3 合并账户):各耗 1 次动作预算。
	ActReflect      = "reflect"        // reflect_mode = light|deep|preview
	ActDeferPayment = "defer_payment"  // 双曲贴现:本月免付,3 个月后 ×1.3
	ActMergeAccounts = "merge_accounts" // 心理账户归并(合并后放大效果消失)
)

// 信用贷档位面额(协议 §4:credit 档位必须是 50000/100000/200000 之一)。
var creditTierAmounts = map[int64]string{
	50000:  LoanCreditT1,
	100000: LoanCreditT2,
	200000: LoanCreditT3,
}

// studyCostCNY 学习进修固定费用(¥2000;批次62 沉没成本偏差可放大该金额)。
const studyCostCNY = 2000

// 副业档位(协议 §4)。Kind 批次20 补入:sideTierGate 按品类差异化门槛
// (tutoring/freelance 高价认知 +1),见 side_market.go。
type sideBizDef struct {
	Kind          string
	GateCognition int
	Low, High     int64
}

var sideBizDefs = map[string]sideBizDef{
	"delivery":  {Kind: "delivery", GateCognition: 0, Low: 2500, High: 4000},
	"content":   {Kind: "content", GateCognition: 2, Low: 2000, High: 6000},
	"tutoring":  {Kind: "tutoring", GateCognition: 4, Low: 3000, High: 5000},
	"freelance": {Kind: "freelance", GateCognition: 3, Low: 3000, High: 6000},
}

// ApplyAction 校验并执行座位动作(引擎层;phase/status 由房间层前置校验)。
// 成功返回人读结果文本(写入 LastActionText / game.event)。
func (w *World) ApplyAction(seat int, a Action) (string, *errcode.Error) {
	p := w.Players[seat]
	if p == nil {
		return "", errcode.Code(errcode.ErrVirtualCityPlayerInactive)
	}
	if !p.Alive || p.StoppedMonths > 0 {
		return "", errcode.Code(errcode.ErrVirtualCityPlayerInactive)
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
	case ActSetSidePrice:
		return w.actSetSidePrice(p, a)
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
	case ActMove:
		return w.actMove(p, a)
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
	case ActPaySupportExtra:
		return w.actPaySupportExtra(p, a)
	case ActUpgradeEducation:
		return w.actUpgradeEducation(p, a)
	case ActReflect:
		return w.actReflect(p, a)
	case ActDeferPayment:
		return w.actDeferPayment(p, a)
	case ActMergeAccounts:
		return w.actMergeAccounts(p)
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
		return "", errcode.Code(errcode.ErrVirtualCityActionBudgetExhausted)
	}
	if a.AmountCNY < 1000 {
		return "", errcode.CodeMsg(errcode.ErrVirtualCityAssetInvalid, "buy_asset amount_cny must be >= 1000")
	}
	switch a.Asset {
	case AssetStockIndex:
		// 批次20 文档3 B2:熔断期禁买(35043);成交价用 ask 单边价(B2-2);
		// 当月买入份数记入 T+1 冻结量(B2-3,月结开头解冻)。
		if w.Market.StockBreakerActive(w.Month) {
			return "", errcode.CodeMsg(errcode.ErrVirtualCityMarketCircuitBreak,
				fmt.Sprintf("熔断期股票交易暂停至第 %d 月", w.Market.BreakerUntilMonth))
		}
		// 批次62:偏差修正(过度自信/赌徒谬误放大金额、锚定抬价、羊群滑点)
		// + 流动性价差(§7.2 股票 2%,叠加在微观结构 ask 价之上)。
		plan := planBuyBias(p, a.AmountCNY)
		liq := w.LiquiditySpread(ClassStock)
		buyUnit := w.Market.StockBuyUnit() * (1 + liq/2) * plan.PriceMul
		ex := w.executeBuy(p, plan, buyUnit, commissionStock)
		if ex.Units < 1 {
			return "", errcode.CodeMsg(errcode.ErrVirtualCityAssetInvalid, "amount too small for 1 unit")
		}
		if ex.Total > p.Cash {
			return "", errcode.Code(errcode.ErrVirtualCityInsufficientCash)
		}
		w.Pay(p.Seat, SeatEntity(p.Seat), EntityMarket, ex.Cost+ex.Fee, CatBuy, "买入指数基金")
		if ex.Extra > 0 {
			w.Pay(p.Seat, SeatEntity(p.Seat), EntityMarket, ex.Extra, CatBiasCost, "跟风滑点")
		}
		if at := p.assetOf(AssetStockIndex); at != nil {
			at.Units += ex.Units
			at.CostCNY += ex.Cost
		} else {
			p.Assets = append(p.Assets, Asset{Kind: AssetStockIndex, Units: ex.Units, CostCNY: ex.Cost, OpenMonth: w.Month})
		}
		p.StockT1Locked += int64(ex.Units)
		p.recordBuyAnchor(AssetStockIndex, w.Market.StockIndex, buyUnit)
		w.recordTradeDirection(p, "buy")
		w.TurnBuyCount++
		w.TurnTradeCount++
		// 批次62 §4.1:未识破的偏差**不报名字**,只报"决策出现非理性偏移"(教学闭环)。
		if ev := biasEventText(p.Seat, plan.Detected, plan.Hidden); ev != "" {
			w.emitEvent("settle", p.Seat, ev)
		}
		text := fmt.Sprintf("买入指数基金 %.0f 份(¥%d)%s", ex.Units, ex.Total, biasAuditText(plan.Detected))
		w.spendBudget(p, "trading", text)
		return text, nil
	case AssetBond:
		// 批次62:债券流动性价差 3% + 偏差修正(债券按面值 1.00 计)。
		plan := planBuyBias(p, a.AmountCNY)
		liq := w.LiquiditySpread(ClassBond)
		unitPrice := 1.0 * (1 + liq/2) * plan.PriceMul
		rate := w.Market.Params().BondRate
		ex := w.executeBuy(p, plan, unitPrice, func(int64) int64 { return 0 })
		if ex.Units < 1 {
			return "", errcode.CodeMsg(errcode.ErrVirtualCityAssetInvalid, "amount too small for 1 unit")
		}
		if ex.Total > p.Cash {
			return "", errcode.Code(errcode.ErrVirtualCityInsufficientCash)
		}
		units := ex.Units
		w.Pay(p.Seat, SeatEntity(p.Seat), EntityMarket, ex.Cost, CatBuy, "买入债券理财")
		if ex.Extra > 0 {
			w.Pay(p.Seat, SeatEntity(p.Seat), EntityMarket, ex.Extra, CatBiasCost, "跟风滑点")
		}
		if at := p.assetOf(AssetBond); at != nil {
			// 追加买入按加权平均锁定利率近似:新份额单独记账(拆分多头)。
			at.Units += units
			at.CostCNY += ex.Cost
			// 追加部分利率并入加权平均。
			oldRate := at.AssetRate()
			oldUnits := at.Units - units
			if oldUnits < 0 {
				oldUnits = 0
			}
			at.Extra["rate"] = (oldRate*oldUnits + rate*units) / at.Units
		} else {
			p.Assets = append(p.Assets, Asset{Kind: AssetBond, Units: units, CostCNY: ex.Cost, OpenMonth: w.Month,
				Extra: map[string]any{"rate": rate}})
		}
		p.recordBuyAnchor(AssetBond, 1.0, unitPrice)
		w.recordTradeDirection(p, "buy")
		w.TurnBuyCount++
		w.TurnTradeCount++
		text := fmt.Sprintf("买入债券 ¥%d(锁定年化 %.1f%%)%s", ex.Cost, rate*100, biasAuditText(plan.Detected))
		w.spendBudget(p, "trading", text)
		return text, nil
	case AssetGold:
		// 批次62:贵金属流动性价差 5% + 偏差修正。
		plan := planBuyBias(p, a.AmountCNY)
		liq := w.LiquiditySpread(ClassGold)
		unitPrice := w.Market.GoldPrice * (1 + liq/2) * plan.PriceMul
		ex := w.executeBuy(p, plan, unitPrice, func(int64) int64 { return 0 })
		if ex.Units < 1 {
			return "", errcode.CodeMsg(errcode.ErrVirtualCityAssetInvalid, "amount too small for 1 gram")
		}
		if ex.Total > p.Cash {
			return "", errcode.Code(errcode.ErrVirtualCityInsufficientCash)
		}
		units := ex.Units
		w.Pay(p.Seat, SeatEntity(p.Seat), EntityMarket, ex.Cost, CatBuy, "买入黄金")
		if ex.Extra > 0 {
			w.Pay(p.Seat, SeatEntity(p.Seat), EntityMarket, ex.Extra, CatBiasCost, "跟风滑点")
		}
		if at := p.assetOf(AssetGold); at != nil {
			at.Units += units
			at.CostCNY += ex.Cost
		} else {
			p.Assets = append(p.Assets, Asset{Kind: AssetGold, Units: units, CostCNY: ex.Cost, OpenMonth: w.Month})
		}
		p.recordBuyAnchor(AssetGold, w.Market.GoldPrice, unitPrice)
		w.recordTradeDirection(p, "buy")
		w.TurnBuyCount++
		w.TurnTradeCount++
		text := fmt.Sprintf("买入黄金 %.0f 克(¥%d)%s", units, ex.Total, biasAuditText(plan.Detected))
		w.spendBudget(p, "trading", text)
		return text, nil
	default:
		return "", errcode.CodeMsg(errcode.ErrVirtualCityAssetInvalid, "buy_asset asset must be stock_index|bond|gold")
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
		return "", errcode.Code(errcode.ErrVirtualCityActionBudgetExhausted)
	}
	if a.Units < 1 && !isHouseKind(a.Asset) && !isShopKind(a.Asset) {
		return "", errcode.CodeMsg(errcode.ErrVirtualCityAssetInvalid, "units must be >= 1")
	}
	switch {
	case a.Asset == AssetStockIndex:
		// 批次20 文档3 B2:熔断期禁卖(35043);成交价用 bid 单边价(B2-2);
		// 可卖量 = 持仓 − 当月买入 T+1 冻结量(B2-3,不足 → 35044)。
		if w.Market.StockBreakerActive(w.Month) {
			return "", errcode.CodeMsg(errcode.ErrVirtualCityMarketCircuitBreak,
				fmt.Sprintf("熔断期股票交易暂停至第 %d 月", w.Market.BreakerUntilMonth))
		}
		at := p.assetOf(AssetStockIndex)
		if at == nil || at.Units < a.Units {
			return "", errcode.Code(errcode.ErrVirtualCityAssetInvalid)
		}
		if a.Units > p.stockSellableUnits() {
			return "", errcode.CodeMsg(errcode.ErrVirtualCityStockT1Locked,
				fmt.Sprintf("可卖 %d 份,当月买入 %d 份 T+1 冻结",
					int(at.Units-float64(p.StockT1Locked)+0.5), p.StockT1Locked))
		}
		// 批次62:流动性价差(§7.2)+ 偏差(损失厌恶/处置效应折价、羊群滑点)。
		avgCost := 0.0
		if at.Units > 0 {
			avgCost = float64(at.CostCNY) / at.Units
		}
		plan := planSellBias(p, biasCtxForAsset(int64(avgCost+0.5), w.Market.StockIndex))
		liq := w.LiquiditySpread(ClassStock)
		sellUnit := w.Market.StockSellUnit() * (1 - liq/2) * plan.PriceMul
		gross := int64(a.Units*sellUnit + 0.5)
		extra := int64(float64(gross)*(plan.CostMul-1) + 0.5)
		net := gross - extra
		fee := commissionStock(gross)
		w.Pay(p.Seat, EntityMarket, SeatEntity(p.Seat), net, CatSell, "卖出指数基金")
		if extra > 0 {
			w.Pay(p.Seat, SeatEntity(p.Seat), EntityMarket, extra, CatBiasCost, "跟风滑点")
		}
		w.Pay(p.Seat, SeatEntity(p.Seat), EntityMarket, fee, CatFee, "卖出佣金")
		at.Units -= a.Units
		if at.Units <= 0.000001 {
			w.removeAsset(p, AssetStockIndex, 0)
		}
		w.recordTradeDirection(p, "sell")
		w.TurnSellCount++
		w.TurnTradeCount++
		text := fmt.Sprintf("卖出指数基金 %.0f 份,入账 ¥%d%s", a.Units, net-fee, biasAuditText(plan.Detected))
		w.spendBudget(p, "trading", text)
		return text, nil
	case a.Asset == AssetBond:
		at := p.assetOf(AssetBond)
		if at == nil || at.Units < a.Units {
			return "", errcode.Code(errcode.ErrVirtualCityAssetInvalid)
		}
		// 免费赎回(面值,协议 §4);批次62:债券流动性价差 3% + 偏差折价。
		avgCost := 0.0
		if at.Units > 0 {
			avgCost = float64(at.CostCNY) / at.Units
		}
		plan := planSellBias(p, biasCtxForAsset(int64(avgCost+0.5), 1.0))
		liq := w.LiquiditySpread(ClassBond)
		gross := int64(a.Units * 1.0 * (1 - liq/2) * plan.PriceMul)
		extra := int64(float64(gross)*(plan.CostMul-1) + 0.5)
		w.Pay(p.Seat, EntityMarket, SeatEntity(p.Seat), gross-extra, CatSell, "赎回债券理财")
		if extra > 0 {
			w.Pay(p.Seat, SeatEntity(p.Seat), EntityMarket, extra, CatBiasCost, "跟风滑点")
		}
		at.Units -= a.Units
		if at.Units <= 0.000001 {
			w.removeAsset(p, AssetBond, 0)
		}
		w.recordTradeDirection(p, "sell")
		w.TurnSellCount++
		w.TurnTradeCount++
		text := fmt.Sprintf("赎回债券 ¥%d%s", gross-extra, biasAuditText(plan.Detected))
		w.spendBudget(p, "trading", text)
		return text, nil
	case a.Asset == AssetGold:
		at := p.assetOf(AssetGold)
		if at == nil || at.Units < a.Units {
			return "", errcode.Code(errcode.ErrVirtualCityAssetInvalid)
		}
		// 批次62:贵金属流动性价差 5% + 偏差(损失厌恶/处置效应折价、羊群滑点)。
		avgCost := 0.0
		if at.Units > 0 {
			avgCost = float64(at.CostCNY) / at.Units
		}
		plan := planSellBias(p, biasCtxForAsset(int64(avgCost+0.5), w.Market.GoldPrice))
		liq := w.LiquiditySpread(ClassGold)
		sellUnit := w.Market.GoldPrice * (1 - liq/2) * plan.PriceMul
		gross := int64(a.Units*sellUnit + 0.5)
		extra := int64(float64(gross)*(plan.CostMul-1) + 0.5)
		fee := int64(float64(gross)*0.005 + 0.5) // 0.5% 手续费(I7)
		w.Pay(p.Seat, EntityMarket, SeatEntity(p.Seat), gross-extra, CatSell, "卖出黄金")
		if extra > 0 {
			w.Pay(p.Seat, SeatEntity(p.Seat), EntityMarket, extra, CatBiasCost, "跟风滑点")
		}
		w.Pay(p.Seat, SeatEntity(p.Seat), EntityMarket, fee, CatFee, "黄金手续费")
		at.Units -= a.Units
		if at.Units <= 0.000001 {
			w.removeAsset(p, AssetGold, 0)
		}
		w.recordTradeDirection(p, "sell")
		w.TurnSellCount++
		w.TurnTradeCount++
		text := fmt.Sprintf("卖出黄金 %.0f 克,入账 ¥%d%s", a.Units, gross-extra-fee, biasAuditText(plan.Detected))
		w.spendBudget(p, "trading", text)
		return text, nil
	case isHouseKind(a.Asset) || isShopKind(a.Asset):
		return w.sellProperty(p, kindDistrict(a.Asset), isShopKind(a.Asset))
	default:
		return "", errcode.CodeMsg(errcode.ErrVirtualCityAssetInvalid, "unknown asset kind")
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
		return "", errcode.Code(errcode.ErrVirtualCityActionBudgetExhausted)
	}
	kind := AssetKindHouse(district)
	label := "住宅"
	if shop {
		kind = AssetKindShop(district)
		label = "商铺"
	}
	idx := propertyIndexOf(p, kind, district, 0)
	if idx < 0 {
		return "", errcode.Code(errcode.ErrVirtualCityAssetInvalid)
	}
	at := &p.Assets[idx]
	// 批次62:房产流动性价差 10%(§7.2)+ 偏差(损失厌恶/处置效应折价、羊群滑点)。
	fair := w.Market.HousePrice(district)
	plan := planSellBias(p, biasCtxForAsset(at.CostCNY, float64(fair)))
	price := w.liquiditySellPrice(fair, ClassHouse)
	price = int64(float64(price)*plan.PriceMul + 0.5)
	extra := int64(float64(price)*(plan.CostMul-1) + 0.5)

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
	net := price - tax - agentFee - extra

	// Ledger:market→seat(gross) + seat→gov(tax) + seat→market(中介)。
	w.Pay(p.Seat, EntityMarket, SeatEntity(p.Seat), price-extra, CatSell, "出售"+label)
	if extra > 0 {
		w.Pay(p.Seat, SeatEntity(p.Seat), EntityMarket, extra, CatBiasCost, "跟风滑点")
	}
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
	w.recordTradeDirection(p, "sell")
	w.TurnSellCount++
	w.TurnTradeCount++
	text := fmt.Sprintf("出售%s%s ¥%d(税¥%d 中介¥%d%s)%s",
		DistrictCN(district), label, price, tax, agentFee, repaySuffix(repaid), biasAuditText(plan.Detected))
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
		return "", errcode.Code(errcode.ErrVirtualCityActionBudgetExhausted)
	}
	if !ValidDistrict(a.District) {
		return "", errcode.CodeMsg(errcode.ErrVirtualCityAssetInvalid, "unknown district")
	}
	if a.DownpayRatio < 0.3 || a.DownpayRatio > 1.0 {
		return "", errcode.CodeMsg(errcode.ErrVirtualCityAssetInvalid, "downpay_ratio must be in [0.3,1.0]")
	}
	if a.Asset == "" {
		a.Asset = "house"
	}
	if a.Asset != "house" && a.Asset != "shop" {
		return "", errcode.CodeMsg(errcode.ErrVirtualCityAssetInvalid, "asset must be house|shop")
	}
	// 批次62:房产流动性价差 10%(§7.2:买卖一次吃掉 10% 价差 + 税费)
	// + 偏差修正(锚定追高 / 羊群滑点)。房价是首付与贷款的计费基准,
	// 故价差与偏差必须作用在 price 上,而不是只作用在首付上。
	plan := planBuyBias(p, 0)
	fair := w.Market.HousePrice(a.District)
	price := w.liquidityBuyPrice(fair, ClassHouse)
	price = int64(float64(price)*plan.PriceMul + 0.5)
	extra := int64(float64(price)*(plan.CostMul-1) + 0.5)
	downpay := int64(float64(price)*a.DownpayRatio + 0.5)

	if a.Asset == "house" {
		if p.houseCount() >= MaxHouses {
			return "", errcode.CodeMsg(errcode.ErrVirtualCityAssetInvalid, "house holdings capped at 4")
		}
		loanAmount := price - downpay
		if loanAmount > 0 {
			if p.CreditScore < 600 {
				return "", errcode.Code(errcode.ErrVirtualCityLoanInvalid)
			}
			if p.CreditGrade() == "E" {
				return "", errcode.Code(errcode.ErrVirtualCityLoanInvalid)
			}
			// P1: 房贷利率 = CB 内生(LPR5Y + 0.5% + CreditSpread);CB nil 回退 PhaseTable。
			rate := w.Market.Params().LPR + MortgageSpread + p.CreditMarkup()
			if w.CB != nil {
				rate = w.CB.MortgageRate() + p.CreditMarkup()
			}
			payment := AnnuityPayment(loanAmount, rate, 360)
			if p.Cash < downpay+extra {
				return "", errcode.Code(errcode.ErrVirtualCityInsufficientCash)
			}
			w.Pay(p.Seat, SeatEntity(p.Seat), EntityMarket, downpay, CatBuy, "购房首付")
			if extra > 0 {
				w.Pay(p.Seat, SeatEntity(p.Seat), EntityMarket, extra, CatBiasCost, "跟风滑点")
			}
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
			p.recordBuyAnchor(AssetKindHouse(a.District), float64(fair), float64(price))
			w.recordTradeDirection(p, "buy")
			w.TurnBuyCount++
			w.TurnTradeCount++
			text := fmt.Sprintf("购入%s住宅 ¥%d(首付 ¥%d,月供 ¥%d)%s",
				DistrictCN(a.District), price, downpay, payment, biasAuditText(plan.Detected))
			w.spendBudget(p, "trading", text)
			return text, nil
		}
		// 全款。
		if p.Cash < price+extra {
			return "", errcode.Code(errcode.ErrVirtualCityInsufficientCash)
		}
		w.Pay(p.Seat, SeatEntity(p.Seat), EntityMarket, price, CatBuy, "全款购房")
		if extra > 0 {
			w.Pay(p.Seat, SeatEntity(p.Seat), EntityMarket, extra, CatBiasCost, "跟风滑点")
		}
		p.Assets = append(p.Assets, w.newPropertyAsset(AssetKindHouse(a.District), price))
		w.autoSelfOccupy(p)
		p.recordBuyAnchor(AssetKindHouse(a.District), float64(fair), float64(price))
		w.recordTradeDirection(p, "buy")
		w.TurnBuyCount++
		w.TurnTradeCount++
		text := fmt.Sprintf("全款购入%s住宅 ¥%d%s", DistrictCN(a.District), price, biasAuditText(plan.Detected))
		w.spendBudget(p, "trading", text)
		return text, nil
	}

	// 商铺:P0 仅全款(经营贷额度与商铺价格差距悬殊,协议文档已同步标注)。
	if p.shopCount() >= MaxShops {
		return "", errcode.CodeMsg(errcode.ErrVirtualCityAssetInvalid, "shop holdings capped at 4")
	}
	if a.DownpayRatio < 1.0 {
		return "", errcode.CodeMsg(errcode.ErrVirtualCityAssetInvalid, "shop requires full payment in P0")
	}
	if p.Cash < price+extra {
		return "", errcode.Code(errcode.ErrVirtualCityInsufficientCash)
	}
	w.Pay(p.Seat, SeatEntity(p.Seat), EntityMarket, price, CatBuy, "全款购铺")
	if extra > 0 {
		w.Pay(p.Seat, SeatEntity(p.Seat), EntityMarket, extra, CatBiasCost, "跟风滑点")
	}
	p.Assets = append(p.Assets, w.newPropertyAsset(AssetKindShop(a.District), price))
	p.recordBuyAnchor(AssetKindShop(a.District), float64(fair), float64(price))
	w.recordTradeDirection(p, "buy")
	w.TurnBuyCount++
	w.TurnTradeCount++
	text := fmt.Sprintf("全款购入%s商铺 ¥%d%s", DistrictCN(a.District), price, biasAuditText(plan.Detected))
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
		return "", errcode.Code(errcode.ErrVirtualCityActionBudgetExhausted)
	}
	if p.CreditGrade() == "E" {
		return "", errcode.Code(errcode.ErrVirtualCityLoanInvalid)
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
			return "", errcode.Code(errcode.ErrVirtualCityLoanInvalid)
		}
		if p.OverdueCount > 0 {
			return "", errcode.CodeMsg(errcode.ErrVirtualCityLoanInvalid, "overdue record blocks consumer loan")
		}
		if p.CreditScore < creditThreshold {
			return "", errcode.CodeMsg(errcode.ErrVirtualCityLoanInvalid, "credit score below threshold")
		}
		cap := p.monthlyIncomeEstimate() * 12
		if cap > 200000 {
			cap = 200000
		}
		cap = int64(float64(cap) * quotaFactor)
		if a.AmountCNY > cap {
			return "", errcode.CodeMsg(errcode.ErrVirtualCityLoanInvalid, "consumer loan cap exceeded")
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
			return "", errcode.CodeMsg(errcode.ErrVirtualCityLoanInvalid, "credit amount must be 50000|100000|200000")
		}
		if p.CreditScore < creditThreshold {
			return "", errcode.Code(errcode.ErrVirtualCityLoanInvalid)
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
			return "", errcode.CodeMsg(errcode.ErrVirtualCityLoanInvalid, "business loan requires running side business")
		}
		if a.AmountCNY <= 0 || a.AmountCNY > 200000 {
			return "", errcode.CodeMsg(errcode.ErrVirtualCityLoanInvalid, "business loan cap is 200000")
		}
		if p.CreditScore < creditThreshold {
			return "", errcode.CodeMsg(errcode.ErrVirtualCityLoanInvalid, "credit score below threshold")
		}
		cap := int64(200000 * quotaFactor)
		if a.AmountCNY > cap {
			return "", errcode.CodeMsg(errcode.ErrVirtualCityLoanInvalid, "business loan cap exceeded")
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
		return "", errcode.CodeMsg(errcode.ErrVirtualCityLoanInvalid, "loan kind must be consumer|credit|business")
	}
}

// ── repay_loan:提前还本 ≥1 万或结清;等额本息重算月供 ──
func (w *World) actRepayLoan(p *Player, a Action) (string, *errcode.Error) {
	if p.ActionBudget <= 0 {
		return "", errcode.Code(errcode.ErrVirtualCityActionBudgetExhausted)
	}
	loan := p.loanByID(a.LoanID)
	if loan == nil || loan.Balance <= 0 {
		return "", errcode.CodeMsg(errcode.ErrVirtualCityLoanInvalid, "loan not found")
	}
	if a.AmountCNY < 10000 && a.AmountCNY < loan.Balance {
		return "", errcode.CodeMsg(errcode.ErrVirtualCityLoanInvalid, "early repayment must be >= 10000 or full settlement")
	}
	pay := a.AmountCNY
	if pay > loan.Balance {
		pay = loan.Balance
	}
	if p.Cash < pay {
		return "", errcode.Code(errcode.ErrVirtualCityInsufficientCash)
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
		return "", errcode.Code(errcode.ErrVirtualCityActionBudgetExhausted)
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

// actStartSide 启动副业。批次20(文档2 §3)增可选 tier 参数:
// 缺省/0 = 中价(旧行为逐字不变),1=低价,2=高价。TierSetMonth 保持 0
// —— 开业不是改档,当月仍可用 set_side_price 调一次。
func (w *World) actStartSide(p *Player, a Action) (string, *errcode.Error) {
	if p.ActionBudget <= 0 {
		return "", errcode.Code(errcode.ErrVirtualCityActionBudgetExhausted)
	}
	def, ok := sideBizDefs[a.Kind]
	if !ok {
		return "", errcode.CodeMsg(errcode.ErrVirtualCityGateFailed, "side business kind must be delivery|content|tutoring|freelance")
	}
	if p.SideBusiness != nil {
		return "", errcode.CodeMsg(errcode.ErrVirtualCityGateFailed, "side business already running")
	}
	if !sideTierValid(a.Tier) {
		return "", errcode.CodeMsg(errcode.ErrVirtualCityGateFailed, "tier must be 0(中价)|1(低价)|2(高价)")
	}
	if p.Cognition < def.GateCognition {
		return "", errcode.Code(errcode.ErrVirtualCityGateFailed)
	}
	if e := sideTierGate(p, def, a.Tier); e != nil {
		return "", e
	}
	base := (def.Low + def.High) / 2
	p.SideBusiness = &SideBusiness{Kind: a.Kind, BaseIncome: base, OpenedMonth: w.Month, PriceTier: a.Tier}
	p.Assets = append(p.Assets, Asset{Kind: AssetSideBusiness, Units: 1, CostCNY: 0, OpenMonth: w.Month,
		Extra: map[string]any{"kind": a.Kind}})
	text := fmt.Sprintf("启动副业(%s),月入约 ¥%d–%d", sideBizCN(a.Kind), def.Low, def.High)
	if a.Tier != PriceTierMid {
		text += fmt.Sprintf(",定价%s档(客群%d%%)", sideTierCN(a.Tier), int(sideTierWeight(a.Tier)*100+0.5))
	}
	w.spendBudget(p, "working", text)
	return text, nil
}

// actSetSidePrice 副业改价(批次20 文档2 §3):预算 1(working);gate =
// 有副业 / tier ∈ 0..2 / 本月未改过档(每月 ≤1 次)/ sideTierGate 品类门槛。
func (w *World) actSetSidePrice(p *Player, a Action) (string, *errcode.Error) {
	if p.ActionBudget <= 0 {
		return "", errcode.Code(errcode.ErrVirtualCityActionBudgetExhausted)
	}
	if p.SideBusiness == nil {
		return "", errcode.CodeMsg(errcode.ErrVirtualCityGateFailed, "no running side business")
	}
	if !sideTierValid(a.Tier) {
		return "", errcode.CodeMsg(errcode.ErrVirtualCityGateFailed, "tier must be 0(中价)|1(低价)|2(高价)")
	}
	if p.SideBusiness.TierSetMonth == w.Month {
		return "", errcode.CodeMsg(errcode.ErrVirtualCityGateFailed, "本月已改过价,每月限 1 次")
	}
	def, ok := sideBizDefs[p.SideBusiness.Kind]
	if !ok {
		def = sideBizDef{Kind: p.SideBusiness.Kind}
	}
	if e := sideTierGate(p, def, a.Tier); e != nil {
		return "", e
	}
	p.SideBusiness.PriceTier = a.Tier
	p.SideBusiness.TierSetMonth = w.Month
	text := fmt.Sprintf("副业改价:%s档(客群%d%%)", sideTierCN(a.Tier), int(sideTierWeight(a.Tier)*100+0.5))
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
		return "", errcode.Code(errcode.ErrVirtualCityActionBudgetExhausted)
	}
	if p.SideBusiness == nil {
		return "", errcode.CodeMsg(errcode.ErrVirtualCityGateFailed, "no running side business")
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
		return "", errcode.Code(errcode.ErrVirtualCityActionBudgetExhausted)
	}
	if p.Cognition >= 10 {
		return "", errcode.Code(errcode.ErrVirtualCityGateFailed)
	}
	if p.Energy < 1 {
		return "", errcode.Code(errcode.ErrVirtualCityGateFailed)
	}
	if p.Cash < studyCostCNY {
		return "", errcode.Code(errcode.ErrVirtualCityInsufficientCash)
	}
	// 批次62:沉没成本偏差(§1)——明知道学不动仍追加投入。
	cost, det, _ := planInvest(p, studyCostCNY)
	if cost > p.Cash {
		cost = p.Cash
	}
	w.Pay(p.Seat, SeatEntity(p.Seat), w.consumerPayTo(), cost, CatStudy, "学习进修")
	p.Energy--
	p.Cognition++
	text := fmt.Sprintf("学习进修(¥%d,认知 +1)%s", cost, biasAuditText(det))
	w.spendBudget(p, "resting", text)
	return text, nil
}

func (w *World) actSocialize(p *Player) (string, *errcode.Error) {
	if p.ActionBudget <= 0 {
		return "", errcode.Code(errcode.ErrVirtualCityActionBudgetExhausted)
	}
	if p.Network >= 10 {
		return "", errcode.Code(errcode.ErrVirtualCityGateFailed)
	}
	if p.Cash < 1000 {
		return "", errcode.Code(errcode.ErrVirtualCityInsufficientCash)
	}
	w.Pay(p.Seat, SeatEntity(p.Seat), w.consumerPayTo(), 1000, CatSocialEv, "社交应酬")
	p.Network++
	text := "社交应酬(¥1000,人脉 +1)"
	w.spendBudget(p, "resting", text)
	return text, nil
}

func (w *World) actRest(p *Player) (string, *errcode.Error) {
	if p.ActionBudget <= 0 {
		return "", errcode.Code(errcode.ErrVirtualCityActionBudgetExhausted)
	}
	if p.Energy >= 10 {
		return "", errcode.Code(errcode.ErrVirtualCityGateFailed)
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
		return "", errcode.Code(errcode.ErrVirtualCityActionBudgetExhausted)
	}
	if p.Energy < 2 {
		return "", errcode.Code(errcode.ErrVirtualCityGateFailed)
	}
	if p.UnemployedMonths > 0 {
		return "", errcode.CodeMsg(errcode.ErrVirtualCityGateFailed, "overtime requires employment")
	}
	p.Energy -= 2
	p.OvertimeThisMonth = true
	text := "加班(精力 −2,本月奖金 = 工资×0.3)"
	w.spendBudget(p, "working", text)
	return text, nil
}

func (w *World) actMoveDistrict(p *Player, a Action) (string, *errcode.Error) {
	if p.ActionBudget <= 0 {
		return "", errcode.Code(errcode.ErrVirtualCityActionBudgetExhausted)
	}
	if !ValidDistrict(a.District) {
		return "", errcode.CodeMsg(errcode.ErrVirtualCityAssetInvalid, "unknown district")
	}
	if a.District == p.District {
		return "", errcode.CodeMsg(errcode.ErrVirtualCityAssetInvalid, "already in this district")
	}
	if p.Energy < 1 {
		return "", errcode.Code(errcode.ErrVirtualCityGateFailed)
	}
	if p.Cash < 3000 {
		return "", errcode.Code(errcode.ErrVirtualCityInsufficientCash)
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

// moveModeDef 移动方式定价(2026-09-22 §CityHuman重构,设计文档 1 §4.1 move):
// 跨城区三档(bus 最便宜 / metro 居中 / taxi 最贵最快),价格随 CPI 浮动。
type moveModeDef struct {
	CostCNY    int64 // 基准费用(元)
	EnergyCost int   // 精力消耗
	Label      string
}

var moveModeDefs = map[string]moveModeDef{
	"walk":  {CostCNY: 0, EnergyCost: 1, Label: "步行"},
	"run":   {CostCNY: 0, EnergyCost: 2, Label: "跑步"},
	"bus":   {CostCNY: 500, EnergyCost: 2, Label: "公交"},
	"metro": {CostCNY: 1500, EnergyCost: 1, Label: "地铁"},
	"taxi":  {CostCNY: 3000, EnergyCost: 1, Label: "出租车"},
}

// moveCPIFactor 移动费用 CPI 浮动系数(economy_enabled=false → 恒 1;
// clamp [0.5, 3.0] 防极端通胀/通缩失真)。
func (w *World) moveCPIFactor() float64 {
	if !w.EconomyEnabled || w.Goods == nil {
		return 1.0
	}
	return clampF(1+w.Goods.CPIYoY, 0.5, 3.0)
}

// actMove 统一移动:walk/run = 区内移动(仅改 LocalPos,不进 Ledger);
// bus/metro/taxi = 跨城区(语义替代 move_district,按方式计费/耗精力)。
func (w *World) actMove(p *Player, a Action) (string, *errcode.Error) {
	if p.ActionBudget <= 0 {
		return "", errcode.Code(errcode.ErrVirtualCityActionBudgetExhausted)
	}
	def, ok := moveModeDefs[a.Mode]
	if !ok {
		return "", errcode.CodeMsg(errcode.ErrVirtualCitySenseInvalid, "unknown move mode (walk|run|bus|metro|taxi)")
	}
	if p.StoppedMonths > 0 {
		return "", errcode.Code(errcode.ErrVirtualCityMoveForbidden)
	}
	if p.Energy < def.EnergyCost {
		return "", errcode.Code(errcode.ErrVirtualCityGateFailed)
	}

	// 区内移动:destination 为空或与当前区相同。
	if a.District == "" || a.District == p.District {
		if a.Mode != "walk" && a.Mode != "run" {
			return "", errcode.CodeMsg(errcode.ErrVirtualCitySenseInvalid, "in-district move only supports walk|run")
		}
		p.Energy -= def.EnergyCost
		// 区内位置随机游走(确定性 rng,不耗经济流)。
		p.LocalPos[0] = w.Rand.Float64()
		p.LocalPos[1] = w.Rand.Float64()
		text := def.Label + "在" + DistrictCN(p.District) + "内移动(精力 −" + itoaInt(def.EnergyCost) + ")"
		w.spendBudget(p, "moving", text)
		return text, nil
	}

	// 跨城区移动。
	if !ValidDistrict(a.District) {
		return "", errcode.CodeMsg(errcode.ErrVirtualCitySenseInvalid, "unknown district")
	}
	cost := int64(float64(def.CostCNY)*w.moveCPIFactor() + 0.5)
	if p.Cash < cost {
		return "", errcode.Code(errcode.ErrVirtualCityInsufficientCash)
	}
	if cost > 0 {
		w.Pay(p.Seat, SeatEntity(p.Seat), w.consumerPayTo(), cost, CatMoving, def.Label+"出行")
	}
	p.Energy -= def.EnergyCost
	p.District = a.District
	p.LocalPos = [2]float64{w.Rand.Float64(), w.Rand.Float64()}
	// 目标区有自住房 → 自动改自住(与 actMoveDistrict 同语义)。
	if p.selfOccupiedHouse() == nil {
		p.HomeDistrict = a.District
	} else if p.selfOccupiedHouse().AssetDistrict() == a.District {
		// 已在该区自住,无事。
	} else {
		w.switchSelfOccupy(p, a.District)
	}
	text := def.Label + "前往" + DistrictCN(a.District) + "(¥" + itoaInt64(cost) + ")"
	w.emitEvent("move", p.Seat, fmt.Sprintf("%d 号位乘%s前往%s", p.Seat, def.Label, DistrictCN(a.District)))
	w.spendBudget(p, "moved", text)
	return text, nil
}

// itoaInt / itoaInt64 小整数转字符串(避免新增 strconv import 漂移)。
func itoaInt(v int) string     { return fmt.Sprintf("%d", v) }
func itoaInt64(v int64) string { return fmt.Sprintf("%d", v) }

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
		return "", errcode.Code(errcode.ErrVirtualCityActionBudgetExhausted)
	}
	if a.AmountCNY < 1 {
		return "", errcode.CodeMsg(errcode.ErrVirtualCityAssetInvalid, "consume amount_cny must be >= 1")
	}
	if p.Cash < a.AmountCNY {
		return "", errcode.Code(errcode.ErrVirtualCityInsufficientCash)
	}
	note := a.Reason
	if len([]rune(note)) > 40 {
		note = string([]rune(note)[:40])
	}
	if note == "" {
		note = "自由消费"
	}
	// 批次62:心理账户偏差(§3.2.2)——用"意外之财"标签资金消费时冲动被放大。
	plan := planConsume(p, a.AmountCNY)
	amount := plan.Amount
	if amount < 1 {
		amount = a.AmountCNY
	}
	if p.Cash < amount {
		amount = p.Cash
	}
	w.Pay(p.Seat, SeatEntity(p.Seat), w.consumerPayTo(), amount, CatConsume, note)
	// P1: 自由消费同时计入消费篮子 misc(与步骤5 恩格尔分配同口径,§3.6)。
	if p.ConsumptionByGoods == nil {
		p.ConsumptionByGoods = map[string]float64{}
	}
	p.ConsumptionByGoods["misc"] += float64(amount)
	p.ImmediateConsumeCount++
	text := fmt.Sprintf("消费 ¥%d(%s)%s", amount, note, biasAuditText(plan.Detected))
	w.spendBudget(p, "idle", text)
	return text, nil
}

func (w *World) actDonate(p *Player, a Action) (string, *errcode.Error) {
	if p.ActionBudget <= 0 {
		return "", errcode.Code(errcode.ErrVirtualCityActionBudgetExhausted)
	}
	if a.AmountCNY < 1000 {
		return "", errcode.CodeMsg(errcode.ErrVirtualCityAssetInvalid, "donate amount_cny must be >= 1000")
	}
	if p.Cash < a.AmountCNY {
		return "", errcode.Code(errcode.ErrVirtualCityInsufficientCash)
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
		return "", errcode.Code(errcode.ErrVirtualCityActionBudgetExhausted)
	}
	if a.AmountCNY <= 0 {
		return "", errcode.CodeMsg(errcode.ErrVirtualCityAssetInvalid, "deposit amount_cny must be > 0")
	}
	if p.Cash < a.AmountCNY {
		return "", errcode.Code(errcode.ErrVirtualCityInsufficientCash)
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
		return "", errcode.Code(errcode.ErrVirtualCityActionBudgetExhausted)
	}
	if a.AmountCNY <= 0 {
		return "", errcode.CodeMsg(errcode.ErrVirtualCityAssetInvalid, "withdraw amount_cny must be > 0")
	}
	if p.SavingsDeposit < a.AmountCNY {
		return "", errcode.CodeMsg(errcode.ErrVirtualCityInsufficientCash, "savings deposit insufficient")
	}
	p.SavingsDeposit -= a.AmountCNY
	p.Cash += a.AmountCNY
	text := fmt.Sprintf("定期转活期 ¥%d(提前支取,利息损失)", a.AmountCNY)
	w.spendBudget(p, "trading", text)
	return text, nil
}

// ── set_consumption: 消费档位(P1 §财商流P1-2 §3.5) ──

// actSetConsumption 设置消费档位(耗 1 次动作预算;立即生效,本月月结按新档位
// 结算)。人类走 WS game.virtual_city_action {type:"set_consumption", level},与 Agent
// 同一 ApplyAction 路径。
func (w *World) actSetConsumption(p *Player, a Action) (string, *errcode.Error) {
	if p.ActionBudget <= 0 {
		return "", errcode.Code(errcode.ErrVirtualCityActionBudgetExhausted)
	}
	if a.Level < 0 || a.Level > 3 {
		return "", errcode.Code(errcode.ErrVirtualCityConsumptionLevelInvalid) // 35020
	}
	p.ConsumptionLevel = a.Level
	text := fmt.Sprintf("调整消费档位:%s(支出×%.1f,精力%+d)",
		consumptionLevelCN(a.Level), consumptionLevelMult[a.Level], consumptionLevelEnergy[a.Level])
	w.spendBudget(p, "idle", text)
	return text, nil
}
