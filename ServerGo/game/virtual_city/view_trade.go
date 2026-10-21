// Package virtual_city — view_trade.go: 挂单簿 / 议价 / P2P 借贷 / 拍卖的
// game.state 视图层(2026-10-21 批次70 §契约补全)。
//
// 背景:批次 69 的 JSON tag 对账发现前端 `VirtualCityGameState.listing_book`
// (类型 `VirtualCityListingBook`,见 ClientWeb/src/types/virtualCity.ts)一直
// 没有后端数据源 —— `BuildClientState` 从未把 `World.ListingBook` /
// `World.AuctionHouse` 放进 game.state ⇒ 前端 `ListingPanel` / `LoanPanel` /
// `NegotiateModal` / `AuctionModal` 与 store 的挂单簿种子恒空。
//
// 本文件是 game.state 挂单簿的**唯一实现**,字段名/层级与前端
// `VirtualCityListingBook` 逐字对齐(只读口径,不改前端任何文件):
//
//	listing_book
//	├── listings[]   → ListingJSON         (id/type/seat/payload/ask_cny/status/create_month/expire_month)
//	│   └── payload  → {asset|loan|info}
//	├── negotiates[] → NegotiateSessionJSON
//	├── p2p_loans[]  → P2PLoanJSON
//	└── auctions[]   → AuctionJSON
//
// 可见性口径(与 P2 交易系统设计稿「公开信息,不公布底价」一致):
//   - 底价 `min_cny`(卖家保密)与拍卖保留价 `reserve_cny` **只发给卖家本人**;
//   - 密封拍卖( sealed / vickrey )的出价金额在开标前**不下发**(只发空数组);
//   - 信息挂单的明文详情(`Detail`)、暗标出价记录(`Bids`)前端类型无对应字段 ⇒ 不下发。
//
// 性能:BuildClientState 是每帧热路径 —— 本视图只遍历四张房间级 map(挂单 ≤36 条
// / 议价 / 借贷 / 拍卖,均为有界小集合),且只拷贝需要的字段(深拷贝资产快照除外)。
package virtual_city

import (
	"sort"
	"strconv"
)

// ── 视图结构体(字段名与前端 VirtualCityListingBook 逐字对齐)────────────

// ListingBookJSON 是 game.state.listing_book(批次70 契约补全)。
//
// 四个数组恒非 nil(序列化为 [] 而非 null —— 前端 xxx.map / .length 的 P0 纪律)。
type ListingBookJSON struct {
	Listings   []ListingJSON         `json:"listings"`
	Negotiates []NegotiateSessionJSON `json:"negotiates"`
	P2PLoans   []P2PLoanJSON         `json:"p2p_loans"`
	Auctions   []AuctionJSON          `json:"auctions"`
}

// ListingJSON 是 game.state.listing_book.listings[] 单项。
type ListingJSON struct {
	ID          string             `json:"id"`
	Type        string             `json:"type"` // asset|buy|info|loan_ofr|loan_req
	Seat        int                `json:"seat"`
	Payload     ListingPayloadJSON `json:"payload"`
	AskCNY      int64              `json:"ask_cny"`
	Status      string             `json:"status"` // open|negotiating|deal|expired|cancelled
	CreateMonth int                `json:"create_month"`
	ExpireMonth int                `json:"expire_month"`
}

// ListingPayloadJSON 是挂单内容联合(前端 VirtualCityListingPayload)。
// 三类互斥;未使用的分支不下发(omitempty)。
type ListingPayloadJSON struct {
	Asset *TradeAssetPayloadJSON `json:"asset,omitempty"`
	Loan  *TradeLoanPayloadJSON  `json:"loan,omitempty"`
	Info  *TradeInfoPayloadJSON  `json:"info,omitempty"`
}

// TradeAssetPayloadJSON 是资产挂单详情(前端 VirtualCityTradeAssetPayload)。
//
// MinCNY(底价)为**卖家保密**信息 ⇒ 仅 viewer == 卖家座位时下发(omitempty)。
type TradeAssetPayloadJSON struct {
	Kind     string  `json:"kind"`
	Name     string  `json:"name"`
	Units    float64 `json:"units"`
	Price    float64 `json:"price"`
	ValueCNY int64   `json:"value_cny"`
	MinCNY   int64   `json:"min_cny,omitempty"` // 底价:仅卖家本人可见
}

// TradeLoanPayloadJSON 是借贷挂单详情(前端 VirtualCityTradeLoanPayload)。
type TradeLoanPayloadJSON struct {
	Direction     string  `json:"direction"` // lend|borrow
	PrincipalCNY  int64   `json:"principal_cny"`
	MaxRate       float64 `json:"max_rate"`
	TermN         int     `json:"term_n"`
	NeedGuarantee bool    `json:"need_guarantee"`
}

// TradeInfoPayloadJSON 是信息挂单详情(前端 VirtualCityTradeInfoPayload)。
//
// 明文详情与暗标出价记录前端类型无对应字段 ⇒ 不下发(仅 detail_hash)。
type TradeInfoPayloadJSON struct {
	Category   string `json:"category"` // market|intel|personal
	Title      string `json:"title"`
	DetailHash string `json:"detail_hash"`
	MinBidCNY  int64  `json:"min_bid_cny"`
}

// NegotiateSessionJSON 是议价会话(前端 VirtualCityNegotiateSession)。
type NegotiateSessionJSON struct {
	ID           string              `json:"id"`
	ListingID    string              `json:"listing_id"`
	ProposerSeat int                 `json:"proposer_seat"`
	RespondSeat  int                 `json:"respond_seat"`
	Turns        []NegotiateTurnJSON `json:"turns"`
	Status       string              `json:"status"` // active|deal|reject|expired
	CreateMonth  int                 `json:"create_month"`
	ExpireMonth  int                 `json:"expire_month"`
	LastOfferCNY int64               `json:"last_offer_cny"`
	LastOfferBy  int                 `json:"last_offer_by"`
}

// NegotiateTurnJSON 是单轮议价(前端 VirtualCityNegotiateTurn)。
type NegotiateTurnJSON struct {
	From     int     `json:"from"`
	OfferCNY int64   `json:"offer_cny"`
	Rate     float64 `json:"rate"`
	Comment  string  `json:"comment"`
	Action   string  `json:"action"` // offer|accept|reject|counter
}

// P2PLoanJSON 是玩家间借贷合约(前端 VirtualCityP2PLoan)。
type P2PLoanJSON struct {
	ID             string  `json:"id"`
	LenderSeat     int     `json:"lender_seat"`
	BorrowerSeat   int     `json:"borrower_seat"`
	PrincipalCNY   int64   `json:"principal_cny"`
	BalanceCNY     int64   `json:"balance_cny"`
	AnnualRate     float64 `json:"annual_rate"`
	MonthlyPayment int64   `json:"monthly_payment"`
	TermN          int     `json:"term_n"`
	MonthsLeft     int     `json:"months_left"`
	GuarantorSeat  int     `json:"guarantor_seat"` // -1 = 无担保
	Overdue        bool    `json:"overdue"`
	CreateMonth    int     `json:"create_month"`
	ListingID      string  `json:"listing_id"`
}

// AuctionJSON 是拍卖场次(前端 VirtualCityAuction)。
//
// ListingID:后端拍卖由「发起拍卖」直接创建,**不挂靠挂单** ⇒ 恒空串
// (前端 AuctionModal 展示「挂单:」行,为空不影响其它信息)。
//
// ReserveCNY(保留价 = 起拍价 × 0.9)与资产底价同口径(卖家保密)
// ⇒ 仅卖家本人可见(omitempty)。
type AuctionJSON struct {
	ID          string           `json:"id"`
	ListingID   string           `json:"listing_id"`
	Kind        string           `json:"kind"`   // english|sealed|dutch|vickrey
	Status      string           `json:"status"` // active|ended|sold|unsold
	Seat        int              `json:"seat"`   // 卖家座位
	StartCNY    int64            `json:"start_cny"`
	CurrentCNY  int64            `json:"current_cny"`
	CurrentSeat int              `json:"current_seat"` // -1 = 无人领先
	EndMonth    int              `json:"end_month"`
	Bids        []AuctionBidJSON `json:"bids"`
	ReserveCNY  int64            `json:"reserve_cny,omitempty"` // 保留价:仅卖家本人可见
}

// AuctionBidJSON 是单笔出价(前端 VirtualCityAuctionBid)。
//
// Month:后端逐笔出价无独立月时间戳(AuctionBid.Time 是出价序号)
// ⇒ 取快照月占位(前端当前不消费该字段)。
type AuctionBidJSON struct {
	Seat      int    `json:"seat"`
	AmountCNY int64  `json:"amount_cny"`
	Month     int    `json:"month"`
	Revealed  bool   `json:"revealed"` // 密封拍卖开标前恒 false(且不下发金额)
}

// ── 组装 ──────────────────────────────────────────────────────────────

// listingBookJSON 组装 game.state.listing_book 快照(viewer < 0 = 观战者)。
//
// 下发范围(只发"活的",避免终态条目随月数无界增长):
//   - listings:   status ∈ {open, negotiating}(前端 ListingPanel 同口径);
//   - negotiates: status = active(前端 NegotiateModal 只找 active 会话);
//   - p2p_loans:  余额 > 0(已还清的合约无操作价值);
//   - auctions:   全部(含已结束,AuctionModal 会回看最后一场)。
func (w *World) listingBookJSON(viewer int) *ListingBookJSON {
	book := emptyListingBookJSON()
	if w == nil {
		return book
	}
	// 挂单。
	if w.ListingBook != nil {
		for _, l := range w.ListingBook.Listings {
			if l == nil {
				continue
			}
			if l.Status != ListingStatusOpen && l.Status != ListingStatusNegotiating {
				continue
			}
			book.Listings = append(book.Listings, w.listingJSON(l, viewer))
		}
		// 议价(仅 active)。
		for _, n := range w.ListingBook.Negotiates {
			if n == nil || n.Status != NegotiateStatusActive {
				continue
			}
			book.Negotiates = append(book.Negotiates, negotiateSessionJSON(n))
		}
		// P2P 借贷(仅未结清)。
		for _, p := range w.ListingBook.P2PLoans {
			if p == nil || p.BalanceCNY <= 0 {
				continue
			}
			book.P2PLoans = append(book.P2PLoans, p2pLoanJSON(p))
		}
	}
	// 拍卖。
	if w.AuctionHouse != nil {
		for _, a := range w.AuctionHouse.Auctions {
			if a == nil {
				continue
			}
			book.Auctions = append(book.Auctions, w.auctionJSON(a, viewer))
		}
	}
	// 稳定排序(房间级 map 迭代序随机 ⇒ 必须排序,否则每帧顺序抖动)。
	sortTradeJSON(book)
	return book
}

// emptyListingBookJSON 返回空挂单簿(未开局/无世界时的兜底;四个空数组非 nil)。
func emptyListingBookJSON() *ListingBookJSON {
	return &ListingBookJSON{
		Listings:   []ListingJSON{},
		Negotiates: []NegotiateSessionJSON{},
		P2PLoans:   []P2PLoanJSON{},
		Auctions:   []AuctionJSON{},
	}
}

// listingJSON 组装单条挂单(私密字段按 viewer 过滤)。
func (w *World) listingJSON(l *Listing, viewer int) ListingJSON {
	out := ListingJSON{
		ID:          l.ID,
		Type:        string(l.Type),
		Seat:        l.Seat,
		AskCNY:      l.AskCNY,
		Status:      l.Status,
		CreateMonth: l.CreateMonth,
		ExpireMonth: l.ExpireMonth,
		Payload:     ListingPayloadJSON{},
	}
	switch {
	case l.Payload.Asset != nil:
		a := l.Payload.Asset.Asset
		p := &TradeAssetPayloadJSON{
			Kind:  a.Kind,
			Name:  assetNameCN(&a),
			Units: a.Units,
		}
		// 行情缺失(World.Market nil,仅可能出现在退化测试夹具)⇒ 单价/市值留 0。
		if w.Market != nil {
			p.Price = assetUnitPrice(&a, w.Market)
			p.ValueCNY = AssetValue(&a, w.Market)
		}
		// 底价:仅卖家本人可见(设计稿「公开信息,不公布底价」)。
		if viewer >= 0 && viewer == l.Seat {
			p.MinCNY = l.Payload.Asset.MinCNY
		}
		out.Payload.Asset = p
	case l.Payload.Loan != nil:
		out.Payload.Loan = &TradeLoanPayloadJSON{
			Direction:     l.Payload.Loan.Direction,
			PrincipalCNY:  l.Payload.Loan.PrincipalCNY,
			MaxRate:       l.Payload.Loan.MaxRate,
			TermN:         l.Payload.Loan.TermN,
			NeedGuarantee: l.Payload.Loan.NeedGuarantee,
		}
	case l.Payload.InfoOffer != nil:
		out.Payload.Info = &TradeInfoPayloadJSON{
			Category:   l.Payload.InfoOffer.Category,
			Title:      l.Payload.InfoOffer.Title,
			DetailHash: l.Payload.InfoOffer.DetailHash,
			MinBidCNY:  l.Payload.InfoOffer.MinBidCNY,
		}
	}
	return out
}

// negotiateSessionJSON 组装议价会话。
func negotiateSessionJSON(n *NegotiateSession) NegotiateSessionJSON {
	out := NegotiateSessionJSON{
		ID:           n.ID,
		ListingID:    n.ListingID,
		ProposerSeat: n.ProposerSeat,
		RespondSeat:  n.RespondSeat,
		Turns:        make([]NegotiateTurnJSON, 0, len(n.Turns)),
		Status:       n.Status,
		CreateMonth:  n.CreateMonth,
		ExpireMonth:  n.ExpireMonth,
		LastOfferCNY: n.LastOfferCNY,
		LastOfferBy:  n.LastOfferBy,
	}
	for _, t := range n.Turns {
		out.Turns = append(out.Turns, NegotiateTurnJSON{
			From: t.From, OfferCNY: t.OfferCNY, Rate: t.Rate,
			Comment: t.Comment, Action: t.Action,
		})
	}
	return out
}

// p2pLoanJSON 组装玩家间借贷合约。
func p2pLoanJSON(p *P2PLoan) P2PLoanJSON {
	return P2PLoanJSON{
		ID:             p.ID,
		LenderSeat:     p.LenderSeat,
		BorrowerSeat:   p.BorrowerSeat,
		PrincipalCNY:   p.PrincipalCNY,
		BalanceCNY:     p.BalanceCNY,
		AnnualRate:     p.AnnualRate,
		MonthlyPayment: p.MonthlyPayment,
		TermN:          p.TermN,
		MonthsLeft:     p.MonthsLeft,
		GuarantorSeat:  p.GuarantorSeat,
		Overdue:        p.Overdue,
		CreateMonth:    p.CreateMonth,
		ListingID:      p.ListingID,
	}
}

// auctionJSON 组装拍卖场次(保留价按 viewer 过滤;密封出价开标前不下发)。
func (w *World) auctionJSON(a *Auction, viewer int) AuctionJSON {
	out := AuctionJSON{
		ID:          a.ID,
		ListingID:   "", // 后端拍卖不挂靠挂单
		Kind:        string(a.Type),
		Status:      a.Status,
		Seat:        a.SellerSeat,
		StartCNY:    a.StartPrice,
		CurrentCNY:  a.CurrentBid,
		CurrentSeat: a.CurrentBidder,
		EndMonth:    a.EndMonth,
		Bids:        []AuctionBidJSON{},
	}
	// 保留价:仅卖家本人可见(与资产底价同口径)。
	if viewer >= 0 && viewer == a.SellerSeat {
		out.ReserveCNY = a.Item.Reserve
	}
	sealed := a.Type == AuctionSealed || a.Type == AuctionVickrey
	switch {
	case !sealed:
		// 公开竞价:出价金额全量可见。
		for _, b := range a.Bids {
			out.Bids = append(out.Bids, AuctionBidJSON{
				Seat: b.Seat, AmountCNY: b.Amount, Month: w.Month, Revealed: true,
			})
		}
	case a.Status != AuctionStatusActive:
		// 密封拍卖已开标:金额可见。
		for _, b := range a.SealedBids {
			out.Bids = append(out.Bids, AuctionBidJSON{
				Seat: b.Seat, AmountCNY: b.Amount, Month: w.Month, Revealed: true,
			})
		}
	default:
		// 密封拍卖进行中:金额保密 ⇒ 不下发任何出价记录。
	}
	return out
}

// assetUnitPrice 返回资产单价(元);与 AssetValue 同口径(见 player.go)。
func assetUnitPrice(a *Asset, m *MarketState) float64 {
	if a == nil || m == nil {
		return 0
	}
	switch {
	case a.Kind == AssetStockIndex:
		return m.StockIndex
	case a.Kind == AssetBond:
		return 1.0
	case a.Kind == AssetGold:
		return m.GoldPrice
	case isHouseKind(a.Kind):
		return float64(m.HousePrice(a.AssetDistrict()))
	case isShopKind(a.Kind):
		return float64(m.ShopPrice(a.AssetDistrict()))
	default:
		return 0
	}
}

// sortTradeJSON 对四张表做稳定排序(id 的数字后缀升序;同号按字符串序兜底)。
//
// ⚠ 不能直接按 id 字符串排序:"L10" < "L2"(字典序坑)⇒ 用「数字后缀补零 + 原始 id」
// 作排序键(L2 < L10)。
func sortTradeJSON(book *ListingBookJSON) {
	sort.SliceStable(book.Listings, func(i, j int) bool {
		return tradeIDSortKey(book.Listings[i].ID) < tradeIDSortKey(book.Listings[j].ID)
	})
	sort.SliceStable(book.Negotiates, func(i, j int) bool {
		return tradeIDSortKey(book.Negotiates[i].ID) < tradeIDSortKey(book.Negotiates[j].ID)
	})
	sort.SliceStable(book.P2PLoans, func(i, j int) bool {
		return tradeIDSortKey(book.P2PLoans[i].ID) < tradeIDSortKey(book.P2PLoans[j].ID)
	})
	sort.SliceStable(book.Auctions, func(i, j int) bool {
		return tradeIDSortKey(book.Auctions[i].ID) < tradeIDSortKey(book.Auctions[j].ID)
	})
}

// tradeIDSortKey 生成排序键("数字后缀补零 | 原始 id");无数字后缀时按 0 处理。
func tradeIDSortKey(id string) string {
	i := len(id)
	for i > 0 && id[i-1] >= '0' && id[i-1] <= '9' {
		i--
	}
	n, err := strconv.Atoi(id[i:])
	if err != nil {
		n = 0
	}
	s := strconv.Itoa(n)
	for len(s) < 6 {
		s = "0" + s
	}
	return s + "|" + id
}
