// Package virtual_city — auction.go: 四种拍卖引擎(2026-09-16 §财商流P2)。
//
// 契约: lag_docs/虚拟城市/已实现/06-P2交易系统/虚拟城市-P2-玩家间交易与财富流动系统-v1.md §4。
// 四种拍卖:英式公开叫价(房产) / 密封暗标(信息股权) / 荷兰式降价 / 维克里(二价密封)。
package virtual_city

import (
	"fmt"

	"LsmAgentGame/errcode"
)

// ── 拍卖类型 ──

// AuctionType 拍卖类型。
type AuctionType string

const (
	AuctionEnglish AuctionType = "english" // 英式公开叫价
	AuctionSealed  AuctionType = "sealed"  // 密封暗标
	AuctionDutch   AuctionType = "dutch"   // 荷兰式降价
	AuctionVickrey AuctionType = "vickrey" // 维克里(二价密封)
)

// 拍卖状态。
const (
	AuctionStatusActive  = "active"
	AuctionStatusEnded   = "ended"
	AuctionStatusSold    = "sold"
	AuctionStatusUnsold  = "unsold"
)

// ── 拍卖品 ──

// AuctionAsset 拍卖标的物(资产快照)。
type AuctionAsset struct {
	Asset   Asset
	Reserve int64 // 卖方底价(流拍保护线)
}

// ── 出价记录 ──

// AuctionBid 单笔出价。
type AuctionBid struct {
	Seat   int
	Amount int64
	Time   int64 // 出价比序(用自增序号代替真实时间)
}

// ── 拍卖会话 ──

// Auction 拍卖会话。
type Auction struct {
	ID            string
	Type          AuctionType
	Item          AuctionAsset
	SellerSeat    int
	StartPrice    int64
	CurrentBid    int64
	CurrentBidder int // -1 无人
	Bids          []AuctionBid
	SealedBids    []AuctionBid // 密封出价(开标前不可见)
	Status        string       // active|ended|sold|unsold
	CreateMonth   int
	EndMonth      int    // 拍卖结束月
	MinIncrement  int64  // 最小加价
	Seq           int64  // 出价序号(单调递增)
}

// ── 拍卖行 ──

// AuctionHouse 房间级拍卖行(World.AuctionHouse)。
type AuctionHouse struct {
	Auctions   map[string]*Auction
	SeqAuction int
}

// 拍卖常量。
const (
	// auctionExpireMonths 拍卖有效期(创建月+2)。
	auctionExpireMonths = 2
	// auctionMinIncrementEnglish 英式拍卖最小加价 10000。
	auctionMinIncrementEnglish = 10000
	// auctionMinIncrementSealed 密封拍卖最小加价 10000。
	auctionMinIncrementSealed = 10000
	// dutchPriceDropPerMonth 荷兰式每月降价 5%。
	dutchPriceDropPerMonth = 0.05
	// auctionFeeRate 拍卖佣金 2%。
	auctionFeeRate = 0.02
)

// NewAuctionHouse 构造空拍卖行。
func NewAuctionHouse() *AuctionHouse {
	return &AuctionHouse{
		Auctions: map[string]*Auction{},
	}
}

// ── 拍卖生命周期 ──

// StartAuction 发起拍卖。
// sellerSeat 为卖方;aType 决定拍卖类型;asset 为标的物;startPrice 为起拍价。
func (w *World) StartAuction(sellerSeat int, aType AuctionType, asset Asset, startPrice int64) (*Auction, *errcode.Error) {
	seller := w.Players[sellerSeat]
	if seller == nil || !seller.Alive {
		return nil, errcode.Code(errcode.ErrVirtualCityPlayerInactive)
	}
	if startPrice < 100 {
		return nil, errcode.CodeMsg(errcode.ErrVirtualCityListingInvalid, "start_price must be >= 100")
	}
	// 校验资产存在。
	if !w.assetExists(seller, asset) {
		return nil, errcode.CodeMsg(errcode.ErrVirtualCityAssetInvalid, "asset not in seller portfolio")
	}

	w.AuctionHouse.SeqAuction++
	auction := &Auction{
		ID:            fmt.Sprintf("A%d", w.AuctionHouse.SeqAuction),
		Type:          aType,
		Item:          AuctionAsset{Asset: asset, Reserve: int64(float64(startPrice) * 0.9)},
		SellerSeat:    sellerSeat,
		StartPrice:    startPrice,
		CurrentBid:    0,
		CurrentBidder: -1,
		Status:        AuctionStatusActive,
		CreateMonth:   w.Month,
		EndMonth:      w.Month + auctionExpireMonths,
		MinIncrement:  auctionMinIncrementEnglish,
	}
	if aType == AuctionSealed || aType == AuctionVickrey {
		auction.MinIncrement = auctionMinIncrementSealed
	}
	w.AuctionHouse.Auctions[auction.ID] = auction
	w.emitEvent("action", sellerSeat, fmt.Sprintf("发起 %s 拍卖 %s(起拍 ¥%d)", cnAuctionType(aType), auction.ID, startPrice))
	return auction, nil
}

// cnAuctionType 拍卖类型中文名。
func cnAuctionType(t AuctionType) string {
	switch t {
	case AuctionEnglish:
		return "英式"
	case AuctionSealed:
		return "密封"
	case AuctionDutch:
		return "荷兰式"
	case AuctionVickrey:
		return "维克里"
	default:
		return string(t)
	}
}

// PlaceBid 英式/荷兰式公开出价。
// 英式:须高于当前最高价+最小加价;荷兰式:当前价 ≤ 出价即成交。
func (w *World) PlaceBid(bidderSeat int, auctionID string, amount int64) *errcode.Error {
	bidder := w.Players[bidderSeat]
	if bidder == nil || !bidder.Alive {
		return errcode.Code(errcode.ErrVirtualCityPlayerInactive)
	}
	a := w.AuctionHouse.Auctions[auctionID]
	if a == nil {
		return errcode.Code(errcode.ErrVirtualCityAuctionNotFound)
	}
	if a.Status != AuctionStatusActive {
		return errcode.Code(errcode.ErrVirtualCityAuctionEnded)
	}
	if a.SellerSeat == bidderSeat {
		return errcode.Code(errcode.ErrVirtualCitySelfTrade)
	}
	if amount < a.StartPrice {
		return errcode.Code(errcode.ErrVirtualCityBidTooLow)
	}
	// 现金校验(英式需冻结;简化:仅校验)。
	if bidder.Cash < amount {
		return errcode.Code(errcode.ErrVirtualCityInsufficientCash)
	}

	switch a.Type {
	case AuctionEnglish:
		minNext := a.CurrentBid + a.MinIncrement
		if a.CurrentBidder == -1 {
			minNext = a.StartPrice
		}
		if amount < minNext {
			return errcode.CodeMsg(errcode.ErrVirtualCityBidTooLow, fmt.Sprintf("bid must be >= %d", minNext))
		}
		a.Seq++
		a.Bids = append(a.Bids, AuctionBid{Seat: bidderSeat, Amount: amount, Time: a.Seq})
		a.CurrentBid = amount
		a.CurrentBidder = bidderSeat
		w.emitEvent("action", bidderSeat, fmt.Sprintf("%d 号位英式竞价 %s 出价 ¥%d", bidderSeat, auctionID, amount))
	case AuctionDutch:
		// 荷兰式:出价 ≥ 当前价即成交。
		currentPrice := w.currentDutchPrice(a)
		if amount >= currentPrice {
			a.Seq++
			a.Bids = append(a.Bids, AuctionBid{Seat: bidderSeat, Amount: currentPrice, Time: a.Seq})
			a.CurrentBid = currentPrice
			a.CurrentBidder = bidderSeat
			// 立即成交。
			w.finalizeAuction(a)
		} else {
			return errcode.CodeMsg(errcode.ErrVirtualCityBidTooLow, fmt.Sprintf("dutch price is %d", currentPrice))
		}
	default:
		return errcode.CodeMsg(errcode.ErrVirtualCityListingInvalid, "use PlaceSealedBid for sealed/vickrey auctions")
	}
	return nil
}

// currentDutchPrice 荷兰式当前价 = 起始价 × (1-降价率)^(已过月数)。
func (w *World) currentDutchPrice(a *Auction) int64 {
	monthsPassed := w.Month - a.CreateMonth
	if monthsPassed < 0 {
		monthsPassed = 0
	}
	price := float64(a.StartPrice)
	for i := 0; i < monthsPassed; i++ {
		price *= (1 - dutchPriceDropPerMonth)
	}
	if price < float64(a.Item.Reserve) {
		price = float64(a.Item.Reserve)
	}
	return int64(price + 0.5)
}

// PlaceSealedBid 密封出价(密封暗标 / 维克里)。
// 出价不公开,到期后开标。
func (w *World) PlaceSealedBid(bidderSeat int, auctionID string, amount int64) *errcode.Error {
	bidder := w.Players[bidderSeat]
	if bidder == nil || !bidder.Alive {
		return errcode.Code(errcode.ErrVirtualCityPlayerInactive)
	}
	a := w.AuctionHouse.Auctions[auctionID]
	if a == nil {
		return errcode.Code(errcode.ErrVirtualCityAuctionNotFound)
	}
	if a.Status != AuctionStatusActive {
		return errcode.Code(errcode.ErrVirtualCityAuctionEnded)
	}
	if a.SellerSeat == bidderSeat {
		return errcode.Code(errcode.ErrVirtualCitySelfTrade)
	}
	if a.Type != AuctionSealed && a.Type != AuctionVickrey {
		return errcode.CodeMsg(errcode.ErrVirtualCityListingInvalid, "not a sealed-bid auction")
	}
	if amount < a.StartPrice {
		return errcode.Code(errcode.ErrVirtualCityBidTooLow)
	}
	if bidder.Cash < amount {
		return errcode.Code(errcode.ErrVirtualCityInsufficientCash)
	}
	a.Seq++
	a.SealedBids = append(a.SealedBids, AuctionBid{Seat: bidderSeat, Amount: amount, Time: a.Seq})
	w.emitEvent("action", bidderSeat, fmt.Sprintf("%d 号位参与密封拍卖 %s", bidderSeat, auctionID))
	return nil
}

// RevealAuction 开标(密封暗标 / 维克里)。
// 密封暗标:最高价者得;维克里:最高价者得,但按第二高价付款。
func (w *World) RevealAuction(auctionID string) (string, *errcode.Error) {
	a := w.AuctionHouse.Auctions[auctionID]
	if a == nil {
		return "", errcode.Code(errcode.ErrVirtualCityAuctionNotFound)
	}
	if a.Type != AuctionSealed && a.Type != AuctionVickrey {
		return "", errcode.CodeMsg(errcode.ErrVirtualCityListingInvalid, "not a sealed-bid auction")
	}
	if a.Status != AuctionStatusActive {
		return "", errcode.Code(errcode.ErrVirtualCityAuctionEnded)
	}
	if len(a.SealedBids) == 0 {
		a.Status = AuctionStatusUnsold
		return "流标(无人出价)", nil
	}
	// 找最高 + 第二高。
	firstIdx, secondIdx := -1, -1
	for i, b := range a.SealedBids {
		if firstIdx < 0 || b.Amount > a.SealedBids[firstIdx].Amount {
			secondIdx = firstIdx
			firstIdx = i
		} else if secondIdx < 0 || b.Amount > a.SealedBids[secondIdx].Amount {
			secondIdx = i
		}
	}
	if firstIdx < 0 {
		a.Status = AuctionStatusUnsold
		return "流标", nil
	}
	winner := a.SealedBids[firstIdx]
	winPrice := winner.Amount
	// 维克里:按第二高价付款。
	if a.Type == AuctionVickrey && secondIdx >= 0 {
		winPrice = a.SealedBids[secondIdx].Amount
	}
	// 底价校验。
	if winPrice < a.Item.Reserve {
		a.Status = AuctionStatusUnsold
		w.emitEvent("action", a.SellerSeat, fmt.Sprintf("密封拍卖 %s 流标(最高价 ¥%d < 底价 ¥%d)", auctionID, winPrice, a.Item.Reserve))
		return "流标(最高价低于底价)", nil
	}
	a.CurrentBid = winPrice
	a.CurrentBidder = winner.Seat
	return w.finalizeAuction(a)
}

// EndAuction 结束英式拍卖(到期 / 连续无人出价时调用)。
// 英式拍卖在到期时,若当前最高价 ≥ 底价则成交,否则流拍。
func (w *World) EndAuction(auctionID string) (string, *errcode.Error) {
	a := w.AuctionHouse.Auctions[auctionID]
	if a == nil {
		return "", errcode.Code(errcode.ErrVirtualCityAuctionNotFound)
	}
	if a.Status != AuctionStatusActive {
		return "", errcode.Code(errcode.ErrVirtualCityAuctionEnded)
	}
	if a.CurrentBidder < 0 || a.CurrentBid < a.Item.Reserve {
		a.Status = AuctionStatusUnsold
		w.emitEvent("action", a.SellerSeat, fmt.Sprintf("英式拍卖 %s 流拍", auctionID))
		return "流拍", nil
	}
	return w.finalizeAuction(a)
}

// finalizeAuction 成交:资金过户 + 资产过户 + 佣金。
func (w *World) finalizeAuction(a *Auction) (string, *errcode.Error) {
	seller := w.Players[a.SellerSeat]
	buyer := w.Players[a.CurrentBidder]
	if seller == nil || !seller.Alive || buyer == nil || !buyer.Alive {
		a.Status = AuctionStatusUnsold
		return "", errcode.Code(errcode.ErrVirtualCityPlayerInactive)
	}
	price := a.CurrentBid
	fee := int64(float64(price) * auctionFeeRate)
	total := price + fee
	if buyer.Cash < total {
		a.Status = AuctionStatusUnsold
		return "", errcode.Code(errcode.ErrVirtualCityInsufficientCash)
	}

	// 资金过户。
	w.PaySeatToSeat(buyer.Seat, a.SellerSeat, price, CatTrade, fmt.Sprintf("拍卖成交 %s", a.ID))
	if fee > 0 {
		w.Pay(buyer.Seat, SeatEntity(buyer.Seat), EntityMarket, fee, CatAuctionFee, "拍卖佣金")
	}

	// 资产过户(与 dealAsset 同款逻辑)。
	idx := w.findAssetIndexForAuction(seller, a.Item.Asset)
	if idx < 0 {
		a.Status = AuctionStatusUnsold
		return "", errcode.CodeMsg(errcode.ErrVirtualCityAssetInvalid, "auction asset no longer exists")
	}
	transfer := seller.Assets[idx]
	transfer.CostCNY = price
	transfer.OpenMonth = w.Month
	if isHouseKind(transfer.Kind) {
		transfer.SetSelfOccupied(false)
	}
	w.removeAssetAt(seller, idx)
	buyer.Assets = append(buyer.Assets, transfer)

	a.Status = AuctionStatusSold
	w.emitEvent("action", buyer.Seat, fmt.Sprintf("拍卖 %s 成交:%d 号位以 ¥%d 从 %d 号位购得 %s", a.ID, buyer.Seat, price, seller.Seat, transfer.Kind))
	return fmt.Sprintf("拍卖成交! %s 以 ¥%d 转让给 %d 号位", transfer.Kind, price, buyer.Seat), nil
}

// findAssetIndexForAuction 查找拍卖资产在卖方持仓中的下标。
func (w *World) findAssetIndexForAuction(seller *Player, snap Asset) int {
	for i := range seller.Assets {
		a := &seller.Assets[i]
		if a.Kind != snap.Kind {
			continue
		}
		if isHouseKind(a.Kind) || isShopKind(a.Kind) {
			if a.AssetDistrict() == snap.AssetDistrict() {
				return i
			}
			continue
		}
		if a.Units >= snap.Units {
			return i
		}
	}
	return -1
}

// GetActiveAuctions 返回活跃拍卖列表。
func (w *World) GetActiveAuctions() []*Auction {
	var out []*Auction
	for _, a := range w.AuctionHouse.Auctions {
		if a.Status == AuctionStatusActive {
			out = append(out, a)
		}
	}
	return out
}

// EndDueAuctions 拍卖到期处理(月结调用):到期未成交 → 流拍/荷兰式降价处理。
func (w *World) EndDueAuctions() {
	for _, a := range w.AuctionHouse.Auctions {
		if a.Status != AuctionStatusActive {
			continue
		}
		if w.Month <= a.EndMonth {
			continue
		}
		switch a.Type {
		case AuctionEnglish:
			// 英式:有最高价则成交,否则流拍。
			if a.CurrentBidder >= 0 && a.CurrentBid >= a.Item.Reserve {
				w.finalizeAuction(a)
			} else {
				a.Status = AuctionStatusUnsold
				w.emitEvent("action", a.SellerSeat, fmt.Sprintf("英式拍卖 %s 到期流拍", a.ID))
			}
		case AuctionSealed, AuctionVickrey:
			// 密封:到期自动开标。
			w.RevealAuction(a.ID)
		case AuctionDutch:
			// 荷兰式:到期无人应价 → 降至底价,若仍无人应价则流拍。
			if a.CurrentBidder < 0 {
				a.Status = AuctionStatusUnsold
				w.emitEvent("action", a.SellerSeat, fmt.Sprintf("荷兰式拍卖 %s 到期流拍", a.ID))
			} else {
				w.finalizeAuction(a)
			}
		}
	}
}
