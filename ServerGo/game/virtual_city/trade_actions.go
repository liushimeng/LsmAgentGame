// Package virtual_city — trade_actions.go: P2 交易类动作实现(2026-09-16 §财商流P2)。
//
// 封装 WS 动作帧(game.virtual_city_action)对应的 12 个交易动作;人类按钮与 Agent 工具
// 共用同一代码路径,复用 listing.go / auction.go 的引擎方法。
package virtual_city

import (
	"fmt"

	"LsmAgentGame/errcode"
)

// ── P2 动作类型 ──

const (
	ActionListAsset         = "list_asset"
	ActionCancelListing     = "cancel_listing"
	ActionViewListings      = "view_listings"
	ActionNegotiateStart    = "negotiate_start"
	ActionNegotiateRespond  = "respond_negotiate"
	ActionCreateLoanListing = "create_loan_listing"
	ActionAcceptLoan        = "accept_loan"
	ActionRepayLoan         = "repay_loan"
	ActionAddGuarantor      = "add_guarantor"
	ActionBidAuction        = "bid_auction"
	ActionSellInfo          = "sell_info"
	ActionBidInfo           = "bid_info"
	ActionStartAuction      = "start_auction"
)

// TradeAction 是交易类动作的载荷(各动作字段按需使用)。
type TradeAction struct {
	Type       string  `json:"type"`
	ListingID  string  `json:"listing_id,omitempty"`
	NegID      string  `json:"neg_id,omitempty"`
	LoanID     string  `json:"loan_id,omitempty"`
	AuctionID  string  `json:"auction_id,omitempty"`

	// list_asset / start_auction
	Asset      string  `json:"asset,omitempty"`      // 资产 kind(house:<d>/shop:<d>/side_business/...)
	AssetIndex int     `json:"asset_index,omitempty"` // 资产下标(同 kind 多笔时)
	AskCNY     int64   `json:"ask_cny,omitempty"`    // 要价/起拍价
	MinCNY     int64   `json:"min_cny,omitempty"`    // 底价(议价/资产挂单)

	// negotiate_start / respond_negotiate / bid_auction / bid_info
	OfferCNY   int64   `json:"offer_cny,omitempty"`
	Action     string  `json:"action,omitempty"` // 议价动作(accept/reject/counter/offer)
	Comment    string  `json:"comment,omitempty"`
	Rate       float64 `json:"rate,omitempty"`    // 议价利率(借贷类)

	// create_loan_listing
	Direction     string  `json:"direction,omitempty"`     // lend|borrow
	Principal     int64   `json:"principal,omitempty"`
	TermN         int     `json:"term_n,omitempty"`
	NeedGuarantee bool    `json:"need_guarantee,omitempty"`

	// repay_loan
	Amount int64 `json:"amount,omitempty"` // 0 = 月供

	// sell_info / bid_info
	Category string `json:"category,omitempty"` // market|intel|personal
	Title    string `json:"title,omitempty"`
	Detail   string `json:"detail,omitempty"`
	MinBid   int64  `json:"min_bid,omitempty"`
	BidCNY   int64  `json:"bid_cny,omitempty"`

	// start_auction
	AuctionType string `json:"auction_type,omitempty"` // english|sealed|dutch|vickrey
}

// ApplyTradeAction 校验并执行座位交易动作(引擎层;phase/status 由房间层前置校验)。
func (w *World) ApplyTradeAction(seat int, ta TradeAction) (string, *errcode.Error) {
	p := w.Players[seat]
	if p == nil || !p.Alive || p.StoppedMonths > 0 {
		return "", errcode.Code(errcode.ErrVirtualCityPlayerInactive)
	}
	if p.ActionBudget <= 0 {
		return "", errcode.Code(errcode.ErrVirtualCityActionBudgetExhausted)
	}

	var result string
	var err *errcode.Error

	switch ta.Type {
	case ActionListAsset:
		result, err = w.tradeListAsset(p, ta)
	case ActionCancelListing:
		err = w.CancelListing(seat, ta.ListingID)
		result = "挂单已取消"
	case ActionViewListings:
		result = w.tradeViewListings(p, ta)
	case ActionNegotiateStart:
		result, err = w.tradeNegotiateStart(p, ta)
	case ActionNegotiateRespond:
		result, err = w.NegotiateRespond(seat, ta.NegID, ta.Action, ta.OfferCNY, ta.Comment)
	case ActionCreateLoanListing:
		result, err = w.tradeCreateLoanListing(p, ta)
	case ActionAcceptLoan:
		result, err = w.tradeAcceptLoan(p, ta)
	case ActionRepayLoan:
		result, err = w.RepayLoan(seat, ta.LoanID, ta.Amount)
	case ActionAddGuarantor:
		err = w.AddGuarantor(ta.LoanID, seat)
		result = "担保已添加"
	case ActionBidAuction:
		result, err = w.tradeBidAuction(p, ta)
	case ActionSellInfo:
		result, err = w.tradeSellInfo(p, ta)
	case ActionBidInfo:
		err = w.BidInfo(seat, ta.ListingID, ta.BidCNY)
		result = "暗标已提交"
	case ActionStartAuction:
		result, err = w.tradeStartAuction(p, ta)
	default:
		return "", errcode.CodeMsg(errcode.ErrValidationFailed, "unknown trade action: "+ta.Type)
	}

	if err != nil {
		return "", err
	}
	// 扣预算 + 记公开文本(仅非只读动作)。
	if ta.Type != ActionViewListings {
		w.spendBudget(p, "trading", result)
	}
	return result, nil
}

// ── 动作实现 ──

// tradeListAsset 挂牌出售资产:从玩家持仓中取出快照,创建挂单。
func (w *World) tradeListAsset(p *Player, ta TradeAction) (string, *errcode.Error) {
	if ta.Asset == "" {
		return "", errcode.CodeMsg(errcode.ErrVirtualCityListingInvalid, "asset kind required")
	}
	// 查找资产。
	var snap Asset
	idx := -1
	seen := 0
	for i := range p.Assets {
		a := &p.Assets[i]
		if a.Kind != ta.Asset {
			continue
		}
		if ta.AssetIndex < 0 || seen == ta.AssetIndex {
			snap = *a
			idx = i
			break
		}
		seen++
	}
	if idx < 0 {
		return "", errcode.CodeMsg(errcode.ErrVirtualCityAssetInvalid, "asset not found")
	}
	_ = idx // idx 仅作存在性校验;实际移除在成交时

	payload := ListingPayload{
		Asset: &AssetPayload{
			Asset:  snap,
			MinCNY: ta.MinCNY,
		},
	}
	l, err := w.CreateListing(p.Seat, ListingAsset, payload, ta.AskCNY)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("资产挂单 %s 已创建(要价 ¥%d)", l.ID, ta.AskCNY), nil
}

// tradeViewListings 查看挂单簿(只读,返回摘要文本)。
func (w *World) tradeViewListings(p *Player, ta TradeAction) string {
	ls := w.GetListings(ListingType(ta.Type))
	if len(ls) == 0 {
		return "当前无活跃挂单"
	}
	// 汇总(不暴露底价 MinCNY)。
	countByType := map[ListingType]int{}
	for _, l := range ls {
		countByType[l.Type]++
	}
	return fmt.Sprintf("活跃挂单 %d 笔:资产 %d / 收购 %d / 信息 %d / 出借 %d / 借款 %d",
		len(ls), countByType[ListingAsset], countByType[ListingBuy],
		countByType[ListingInfo], countByType[ListingLoanOfr], countByType[ListingLoanReq])
}

// tradeNegotiateStart 发起议价。
func (w *World) tradeNegotiateStart(p *Player, ta TradeAction) (string, *errcode.Error) {
	if ta.ListingID == "" {
		return "", errcode.CodeMsg(errcode.ErrVirtualCityListingInvalid, "listing_id required")
	}
	neg, err := w.NegotiateStart(p.Seat, ta.ListingID, ta.OfferCNY)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("议价 %s 已发起(首轮报价 ¥%d)", neg.ID, ta.OfferCNY), nil
}

// tradeCreateLoanListing 创建借贷挂单。
func (w *World) tradeCreateLoanListing(p *Player, ta TradeAction) (string, *errcode.Error) {
	l, err := w.CreateLoanListing(p.Seat, ta.Direction, ta.Principal, ta.Rate, ta.TermN, ta.NeedGuarantee)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("借贷挂单 %s 已创建(%s ¥%d)", l.ID, ta.Direction, ta.Principal), nil
}

// tradeAcceptLoan 接受借贷要约。
func (w *World) tradeAcceptLoan(p *Player, ta TradeAction) (string, *errcode.Error) {
	if ta.ListingID == "" {
		return "", errcode.CodeMsg(errcode.ErrVirtualCityListingInvalid, "listing_id required")
	}
	loan, err := w.AcceptLoan(p.Seat, ta.ListingID)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("借贷合约 %s 成立(月供 ¥%d)", loan.ID, loan.MonthlyPayment), nil
}

// tradeBidAuction 拍卖出价(区分公开/密封)。
func (w *World) tradeBidAuction(p *Player, ta TradeAction) (string, *errcode.Error) {
	if ta.AuctionID == "" {
		return "", errcode.CodeMsg(errcode.ErrVirtualCityAuctionNotFound, "auction_id required")
	}
	a := w.AuctionHouse.Auctions[ta.AuctionID]
	if a == nil {
		return "", errcode.Code(errcode.ErrVirtualCityAuctionNotFound)
	}
	switch a.Type {
	case AuctionSealed, AuctionVickrey:
		err := w.PlaceSealedBid(p.Seat, ta.AuctionID, ta.OfferCNY)
		if err != nil {
			return "", err
		}
		return "密封出价已提交", nil
	default:
		err := w.PlaceBid(p.Seat, ta.AuctionID, ta.OfferCNY)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("出价 ¥%d 已提交", ta.OfferCNY), nil
	}
}

// tradeSellInfo 出售信息(密封暗标)。
func (w *World) tradeSellInfo(p *Player, ta TradeAction) (string, *errcode.Error) {
	l, err := w.SellInfo(p.Seat, ta.Category, ta.Title, ta.Detail, ta.MinBid)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("信息挂单 %s 已创建(暗标最低 ¥%d)", l.ID, ta.MinBid), nil
}

// tradeStartAuction 发起拍卖。
func (w *World) tradeStartAuction(p *Player, ta TradeAction) (string, *errcode.Error) {
	aType := AuctionType(ta.AuctionType)
	if aType == "" {
		aType = AuctionEnglish
	}
	if ta.Asset == "" {
		return "", errcode.CodeMsg(errcode.ErrVirtualCityAssetInvalid, "asset kind required")
	}
	// 查找资产快照。
	var snap Asset
	seen := 0
	found := false
	for i := range p.Assets {
		a := &p.Assets[i]
		if a.Kind != ta.Asset {
			continue
		}
		if ta.AssetIndex < 0 || seen == ta.AssetIndex {
			snap = *a
			found = true
			break
		}
		seen++
	}
	if !found {
		return "", errcode.CodeMsg(errcode.ErrVirtualCityAssetInvalid, "asset not found")
	}
	auction, err := w.StartAuction(p.Seat, aType, snap, ta.AskCNY)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s 拍卖 %s 已发起(起拍 ¥%d)", cnAuctionType(aType), auction.ID, ta.AskCNY), nil
}

// ── 查询接口(前端展示用) ──

// ListingSnapshot 挂单簿条目视图(隐藏卖家底价)。
type ListingSnapshot struct {
	ID          string      `json:"id"`
	Type        ListingType `json:"type"`
	Seat        int         `json:"seat"`
	AskCNY      int64       `json:"ask_cny"`
	Status      string      `json:"status"`
	CreateMonth int         `json:"create_month"`
	ExpireMonth int         `json:"expire_month"`
	// Asset 挂单公开字段。
	AssetKind  string `json:"asset_kind,omitempty"`
	AssetUnits string `json:"asset_units,omitempty"` // 人读字符串(城区/克数等)
	// 信息挂单公开字段。
	InfoTitle string `json:"info_title,omitempty"`
	Category  string `json:"category,omitempty"`
	MinBidCNY int64  `json:"min_bid_cny,omitempty"`
	// 借贷挂单公开字段。
	Direction    string  `json:"direction,omitempty"`
	PrincipalCNY int64   `json:"principal_cny,omitempty"`
	MaxRate     float64 `json:"max_rate,omitempty"`
	TermN       int     `json:"term_n,omitempty"`
}

// SnapshotListings 生成挂单簿快照(view 下发用;隐藏 MinCNY 底价)。
func (w *World) SnapshotListings(lType ListingType) []ListingSnapshot {
	var out []ListingSnapshot
	for _, l := range w.ListingBook.Listings {
		if lType != "" && l.Type != lType {
			continue
		}
		if l.Status != ListingStatusOpen {
			continue
		}
		s := ListingSnapshot{
			ID: l.ID, Type: l.Type, Seat: l.Seat, AskCNY: l.AskCNY,
			Status: l.Status, CreateMonth: l.CreateMonth, ExpireMonth: l.ExpireMonth,
		}
		if l.Payload.Asset != nil {
			s.AssetKind = l.Payload.Asset.Asset.Kind
			s.AssetUnits = assetUnitsText(l.Payload.Asset.Asset)
		}
		if l.Payload.InfoOffer != nil {
			s.InfoTitle = l.Payload.InfoOffer.Title
			s.Category = l.Payload.InfoOffer.Category
			s.MinBidCNY = l.Payload.InfoOffer.MinBidCNY
		}
		if l.Payload.Loan != nil {
			s.Direction = l.Payload.Loan.Direction
			s.PrincipalCNY = l.Payload.Loan.PrincipalCNY
			s.MaxRate = l.Payload.Loan.MaxRate
			s.TermN = l.Payload.Loan.TermN
		}
		out = append(out, s)
	}
	return out
}

// assetUnitsText 资产数量人读文本。
func assetUnitsText(a Asset) string {
	switch {
	case a.Kind == AssetStockIndex:
		return fmt.Sprintf("%.0f 份", a.Units)
	case a.Kind == AssetBond:
		return fmt.Sprintf("%.0f 元", a.Units)
	case a.Kind == AssetGold:
		return fmt.Sprintf("%.1f 克", a.Units)
	case isHouseKind(a.Kind), isShopKind(a.Kind):
		return DistrictCN(a.AssetDistrict())
	case a.Kind == AssetSideBusiness:
		return "1 个"
	default:
		return fmt.Sprintf("%.0f", a.Units)
	}
}
