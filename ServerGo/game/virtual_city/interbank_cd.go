// Package virtual_city — interbank_cd.go: 同业存单市场(银行间短期融资工具)
// (2026-09-21 §城市扩张v2.12 阶段6)。
//
// 3 家城商行(A/B/C)× 4 档期限(1/3/6/12 月)= 12 只挂牌存单。利率锚定
// interest_transmission.go 的 SHIBOR 传导链:
//
//	票面利率 = SHIBOR(同期限) + 30bp
//	SHIBOR(期限) = SHIBOR_1Y(MLF 锚,传导第 1 步) − 期限贴水(1M 30bp/3M 20bp/6M 10bp/12M 0)
//
// 估值模型(线性计息,零 rand):
//
//	应计价值 AV(t) = 面值 × (1 + 票面利率 × t/12)   t = 持有月数
//	到期兑付 = 面值 × (1 + 票面利率 × 期限/12)
//	二级卖出 = AV(t) × (1 − 1%)                     R6-3 流动性折价
//
// 玩家可按面值平价申购(seat→bank CatBuy)、持有期内二级卖出(bank→seat
// CatSell,吃 1% 折价)或持有到期自动兑付(⑥B 内完成,与国债月结同口径)。
// 已发行存单票面利率锁定(现实同款:票面利率发行时固定);月度仅刷新
// CurRate(新发行定价基准 + 快照展示)。
//
// 零 rand 消费 —— 固定种子存量对局回归零偏移(treasury/供应链同款纪律)。
package virtual_city

import (
	"fmt"

	"LsmAgentGame/errcode"
)

// 同业存单常量(阶段6 新定)。
const (
	// CDCouponSpread 票面利率 = SHIBOR(同期限) + 30bp。
	CDCouponSpread = 0.0030
	// CDSellDiscount 二级市场流动性折价 1%(R6-3)。
	CDSellDiscount = 0.01
	// CDBuyMinFaceWan 单笔申购下限(万元)。
	CDBuyMinFaceWan = 1.0
)

// CDIssuerNames 发行行(简化:城商行 A/B/C 三家;顺序固定)。
var CDIssuerNames = []string{"城商行A", "城商行B", "城商行C"}

// CDTenures 挂牌期限档(月;顺序固定 = 下发序)。
var CDTenures = []int{1, 3, 6, 12}

// cdTermDiscount SHIBOR 期限贴水(1Y 为锚,短端贴水;正常向上倾斜曲线)。
var cdTermDiscount = map[int]float64{1: 0.0030, 3: 0.0020, 6: 0.0010, 12: 0.0}

// cdOutstandingWan 每只挂牌存单存量基准(万元;静态发行量)。
var cdOutstandingWan = map[int]float64{1: 800, 3: 1500, 6: 2000, 12: 3000}

// InterbankCD 同业存单挂牌标的(挂 CDMarket.Listed;静态规格)。
type InterbankCD struct {
	ID          string  // 如 "CD-A-3M"
	Issuer      string  // 发行行(城商行A/B/C)
	TenureMonths int    // 1/3/6/12
	Rate        float64 // 展示用最新票面利率(CurRate 同步;已发行持仓票面在 CDHolding.Rate 锁定)
	Outstanding float64 // 存量(万元,静态)
}

// CDHolding 玩家持仓(申购即锁定票面利率;到期/卖出后移除)。
type CDHolding struct {
	ID           string  // 如 "H1"
	Seat         int
	CDID         string  // 对应挂牌 id
	Issuer       string
	TenureMonths int
	Rate         float64 // 票面利率(申购月 CurRate 锁定)
	FaceWan      float64 // 面值(万元)
	BuyMonth     int     // 申购月(主钟)
}

// CDMarket 同业存单市场(挂 World.CDMarket;引擎纯状态,无锁)。
type CDMarket struct {
	Listed  []*InterbankCD
	CurRate map[int]float64 // tenure → 当前票面利率基准(SHIBOR_t+30bp;月度刷新)
	Holdings []*CDHolding   // 全房持仓(按申购序)

	seq     int
	// TotalOutstandingWan 挂牌存量合计(万元,快照)。
	TotalOutstandingWan float64
	// LastAvgRate 存量加权平均票面(快照)。
	LastAvgRate float64
	// LastStepMonth 最近一次 CDMonthlyStep 的 w.Month(§130 接线验证)。
	LastStepMonth int
}

// shiborForTenure 期限 SHIBOR = SHIBOR_1Y(MLF 锚) − 期限贴水。
// SHIBOR_1Y 复用 interest_transmission.go::CentralToInterbank(第 1 步传导)。
func shiborForTenure(w *World, tenureMonths int) float64 {
	shibor1Y, _ := CentralToInterbank(w, mlfAnchor(w))
	disc, ok := cdTermDiscount[tenureMonths]
	if !ok {
		disc = 0
	}
	r := shibor1Y - disc
	if r < 0 {
		r = 0
	}
	return r
}

// NewCDMarket 构造 12 只挂牌存单(3 行 × 4 档;初始 Rate 按 SHIBOR 定价)。
func NewCDMarket() *CDMarket {
	m := &CDMarket{CurRate: map[int]float64{}}
	for _, issuer := range CDIssuerNames {
		for _, t := range CDTenures {
			m.Listed = append(m.Listed, &InterbankCD{
				ID: fmt.Sprintf("CD-%c-%dM", issuer[len(issuer)-1], t),
				Issuer: issuer, TenureMonths: t,
				Outstanding: cdOutstandingWan[t],
			})
		}
	}
	return m
}

// refreshRates 月度刷新 CurRate(新发行定价基准)+ 挂牌展示利率。
func (m *CDMarket) refreshRates(w *World) {
	for t := range cdTermDiscount {
		m.CurRate[t] = shiborForTenure(w, t) + CDCouponSpread
	}
	for _, cd := range m.Listed {
		cd.Rate = m.CurRate[cd.TenureMonths]
	}
}

// CDAccruedValue 应计价值(元)= 面值(万元)×1e4 × (1 + 票面 × min(t,期限)/12)。
func CDAccruedValue(faceWan, rate float64, heldMonths, tenureMonths int) float64 {
	if heldMonths > tenureMonths {
		heldMonths = tenureMonths
	}
	if heldMonths < 0 {
		heldMonths = 0
	}
	return faceWan * 10000 * (1 + rate*float64(heldMonths)/12)
}

// cdByID 查挂牌标的。
func (m *CDMarket) cdByID(id string) *InterbankCD {
	for _, cd := range m.Listed {
		if cd.ID == id {
			return cd
		}
	}
	return nil
}

// BuyCD 平价申购(面值交割):seat → bank CatBuy;票面 = 申购月 CurRate。
// faceWan 为申购面值(万元,≥1);现金须足额。
func (w *World) BuyCD(seat int, cdID string, faceWan float64) (*CDHolding, *errcode.Error) {
	p := w.Players[seat]
	if p == nil || !p.Alive {
		return nil, errcode.Code(errcode.ErrVirtualCityPlayerInactive)
	}
	if faceWan < CDBuyMinFaceWan {
		return nil, errcode.CodeMsg(errcode.ErrVirtualCityAssetInvalid, "cd face_wan must be >= 1")
	}
	if w.CDMarket == nil {
		return nil, errcode.CodeMsg(errcode.ErrVirtualCityAssetInvalid, "cd market not initialized")
	}
	cd := w.CDMarket.cdByID(cdID)
	if cd == nil {
		return nil, errcode.CodeMsg(errcode.ErrVirtualCityAssetInvalid, "cd not found: "+cdID)
	}
	rate := w.CDMarket.CurRate[cd.TenureMonths]
	if rate <= 0 {
		rate = shiborForTenure(w, cd.TenureMonths) + CDCouponSpread
	}
	cost := int64(faceWan*10000 + 0.5)
	if p.Cash < cost {
		return nil, errcode.Code(errcode.ErrVirtualCityInsufficientCash)
	}
	w.Pay(seat, SeatEntity(seat), EntityBank, cost, CatBuy, fmt.Sprintf("申购同业存单 %s", cd.ID))
	w.CDMarket.seq++
	h := &CDHolding{
		ID: fmt.Sprintf("H%d", w.CDMarket.seq), Seat: seat,
		CDID: cd.ID, Issuer: cd.Issuer, TenureMonths: cd.TenureMonths,
		Rate: rate, FaceWan: faceWan, BuyMonth: w.Month,
	}
	w.CDMarket.Holdings = append(w.CDMarket.Holdings, h)
	w.emitEvent("action", seat, fmt.Sprintf("%d 号位申购 %s %s %.0f 万元(票面 %.2f%%)",
		seat, cd.Issuer, cd.ID, faceWan, rate*100))
	return h, nil
}

// SellCD 二级卖出:应计价值 × (1−1%) 流动性折价(R6-3);bank → seat CatSell。
func (w *World) SellCD(seat int, holdingID string) (int64, *errcode.Error) {
	if w.CDMarket == nil {
		return 0, errcode.CodeMsg(errcode.ErrVirtualCityAssetInvalid, "cd market not initialized")
	}
	idx := -1
	for i, h := range w.CDMarket.Holdings {
		if h.ID == holdingID && h.Seat == seat {
			idx = i
			break
		}
	}
	if idx < 0 {
		return 0, errcode.CodeMsg(errcode.ErrVirtualCityAssetInvalid, "cd holding not found: "+holdingID)
	}
	h := w.CDMarket.Holdings[idx]
	held := w.Month - h.BuyMonth
	fv := CDAccruedValue(h.FaceWan, h.Rate, held, h.TenureMonths)
	proceeds := int64(fv*(1-CDSellDiscount) + 0.5) // R6-3:1% 折价。
	w.Pay(seat, EntityBank, SeatEntity(seat), proceeds, CatSell,
		fmt.Sprintf("二级卖出存单 %s(折价 1%%)", h.CDID))
	w.CDMarket.Holdings = append(w.CDMarket.Holdings[:idx], w.CDMarket.Holdings[idx+1:]...)
	w.emitEvent("action", seat, fmt.Sprintf("%d 号位二级卖出 %s,入账 ¥%d", seat, h.CDID, proceeds))
	return proceeds, nil
}

// CDMonthlyStep 月度:① 刷新 CurRate(SHIBOR 联动)→ ② 到期兑付(线性计息
// 全额,走 w.Pay)→ ③ 快照聚合。SettleMonth ⑥B 调用;零 rand;nil 安全。
func (m *CDMarket) CDMonthlyStep(w *World) {
	if m == nil || w == nil {
		return
	}
	m.LastStepMonth = w.Month
	m.refreshRates(w)

	// ② 到期兑付:持有月数 ≥ 期限 → bank→seat 全额应计(到期无折价)。
	kept := m.Holdings[:0]
	for _, h := range m.Holdings {
		if w.Month-h.BuyMonth >= h.TenureMonths {
			redeem := int64(CDAccruedValue(h.FaceWan, h.Rate, h.TenureMonths, h.TenureMonths) + 0.5)
			if redeem > 0 {
				w.Pay(h.Seat, EntityBank, SeatEntity(h.Seat), redeem, CatSell,
					fmt.Sprintf("存单到期兑付 %s", h.CDID))
				w.emitEvent("market", h.Seat, fmt.Sprintf("%d 号位存单 %s 到期,兑付 ¥%d",
					h.Seat, h.CDID, redeem))
			}
			continue
		}
		kept = append(kept, h)
	}
	m.Holdings = kept

	// ③ 快照聚合:存量合计 + 加权平均票面。
	total, weighted := 0.0, 0.0
	for _, cd := range m.Listed {
		total += cd.Outstanding
		weighted += cd.Outstanding * cd.Rate
	}
	m.TotalOutstandingWan = total
	if total > 0 {
		m.LastAvgRate = weighted / total
	}
}
