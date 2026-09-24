// Package virtual_city — listing.go: 挂单簿 + 议价会话 + 玩家间借贷合约(2026-09-16 §财商流P2)。
//
// 契约: lag_docs/虚拟城市/已实现/06-P2交易系统/虚拟城市-P2-玩家间交易与财富流动系统-v1.md §3/§5/§6。
// 所有现金变动走 World.Pay(双式记账);World 是无锁纯状态,房间层持锁调用。
package virtual_city

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"LsmAgentGame/errcode"
)

// ── 挂牌类型 ──

// ListingType 挂牌类型。
type ListingType string

const (
	ListingAsset   ListingType = "asset"    // 资产出售
	ListingBuy     ListingType = "buy"      // 收购挂单
	ListingInfo    ListingType = "info"     // 信息出售
	ListingLoanOfr ListingType = "loan_ofr" // 出借要约
	ListingLoanReq ListingType = "loan_req" // 借款请求
)

// ── 挂单状态 ──

const (
	ListingStatusOpen         = "open"
	ListingStatusNegotiating  = "negotiating"
	ListingStatusDeal         = "deal"
	ListingStatusExpired      = "expired"
	ListingStatusCancelled    = "cancelled"
	NegotiateStatusActive     = "active"
	NegotiateStatusDeal       = "deal"
	NegotiateStatusReject     = "reject"
	NegotiateStatusExpired    = "expired"
)

// ── 议价动作 ──

const (
	NegotiateActOffer   = "offer"
	NegotiateActAccept  = "accept"
	NegotiateActReject  = "reject"
	NegotiateActCounter = "counter"
)

// ── 挂单结构 ──

// ListingPayload 挂单内容联合(三类互斥)。
type ListingPayload struct {
	Asset     *AssetPayload     // 资产类
	Loan      *LoanPayload      // 借贷类
	InfoOffer *InfoOfferPayload // 信息类
}

// AssetPayload 资产挂单详情。
type AssetPayload struct {
	Asset  Asset // 资产快照(深拷贝)
	MinCNY int64 // 底价(卖家保密)
}

// LoanPayload 借贷挂单详情。
type LoanPayload struct {
	Direction     string  // lend|borrow
	PrincipalCNY  int64   // 金额
	MaxRate       float64 // 最高可接受利率(借入)/最低(贷出)
	TermN         int     // 期数(月)
	NeedGuarantee bool    // 是否需要担保
}

// InfoOfferPayload 信息出售。
type InfoOfferPayload struct {
	Category   string // market|intel|personal
	Title      string // 信息标题(公开)
	DetailHash string // 详情哈希(成交后揭示)
	Detail     string // 明文详情(仅卖方与中标者可见)
	MinBidCNY  int64  // 最低出价(暗标)
	Bids       []InfoBid // 暗标出价记录
}

// InfoBid 单笔暗标出价。
type InfoBid struct {
	Seat   int
	Amount int64
}

// Listing 单笔挂单(挂单簿条目)。
type Listing struct {
	ID          string
	Type        ListingType
	Seat        int
	Payload     ListingPayload
	AskCNY      int64
	Status      string // open|negotiating|deal|expired|cancelled
	CreateMonth int
	ExpireMonth int
}

// ── 议价会话 ──

// NegotiateTurn 单轮议价。
type NegotiateTurn struct {
	From    int
	OfferCNY int64
	Rate    float64 // 借贷利率(借贷类)
	Comment string
	Action  string // offer|accept|reject|counter
}

// NegotiateSession 单笔议价会话。
type NegotiateSession struct {
	ID           string
	ListingID    string
	ProposerSeat int
	RespondSeat  int
	Turns        []NegotiateTurn
	Status       string // active|deal|reject|expired
	CreateMonth  int
	ExpireMonth  int
	LastOfferCNY int64
	LastOfferBy  int
}

// ── 玩家间借贷合约 ──

// P2PLoan 玩家间借贷合约(deal 后生成)。
type P2PLoan struct {
	ID             string
	LenderSeat     int
	BorrowerSeat   int
	PrincipalCNY   int64
	BalanceCNY     int64
	AnnualRate     float64
	MonthlyPayment int64
	TermN          int
	MonthsLeft     int
	GuarantorSeat  int  // -1 无担保
	Overdue        bool
	OverdueStreak  int  // 连续逾期月数
	CreateMonth    int
	ListingID      string
}

// ── 挂单簿 ──

// ListingBook 房间级挂单簿(World.ListingBook)。
type ListingBook struct {
	Listings    map[string]*Listing          // id → 挂单
	Negotiates  map[string]*NegotiateSession // id → 议价
	P2PLoans    map[string]*P2PLoan          // id → 借贷合约
	SeqListing  int
	SeqNeg      int
	SeqP2P      int
}

// ── 常量 ──

const (
	// listingExpireMonths 挂单有效期(创建月+3)。
	listingExpireMonths = 3
	// negotiateExpireMonths 议价有效期(创建月+1)。
	negotiateExpireMonths = 1
	// maxListingsPerSeat 每座位最多活跃挂单数。
	maxListingsPerSeat = 3
	// p2pMinRatePerMonth 最低月利率 0.3%(年化 3.6%)。
	p2pMinRatePerMonth = 0.003
	// p2pMaxRatePerMonth 最高月利率 3.6%(年化 43.2%)。
	p2pMaxRatePerMonth = 0.036
	// p2pMaxTermN 最长借款期数(60 月)。
	p2pMaxTermN = 60
	// p2pMinTermN 最短借款期数(1 月)。
	p2pMinTermN = 1
	// p2pMinPrincipal 最低借款金额(1000 元)。
	p2pMinPrincipal = 1000
	// p2pMaxOverdueStreak 连续逾期上限(3 月 → 担保代偿)。
	p2pMaxOverdueStreak = 3
	// p2pPenaltyRate 逾期罚息月利率 5%。
	p2pPenaltyRate = 0.05
	// tradeTaxRate 玩家间资产交易增值税率 3%。
	tradeTaxRate = 0.03
	// tradeAgentRate 玩家间资产交易中介费率 1%。
	tradeAgentRate = 0.01
)

// ── 构造 ──

// PaySeatToSeat 玩家间资金过户(双式记账 + 双方现金更新)。
// 引擎 Pay 只更新单方,玩家间转账需显式更新双方。
func (w *World) PaySeatToSeat(fromSeat, toSeat int, amount int64, category, note string) {
	if amount <= 0 {
		return
	}
	w.Ledger.Record(w.Month, SeatEntity(fromSeat), SeatEntity(toSeat), amount, category, note)
	if w.Players[fromSeat] != nil {
		w.Players[fromSeat].Cash -= amount
	}
	if w.Players[toSeat] != nil {
		w.Players[toSeat].Cash += amount
	}
}

// NewListingBook 构造空挂单簿。
func NewListingBook() *ListingBook {
	return &ListingBook{
		Listings:   map[string]*Listing{},
		Negotiates: map[string]*NegotiateSession{},
		P2PLoans:   map[string]*P2PLoan{},
	}
}

// ── 辅助 ──

// hashDetail 计算信息详情哈希(sha256 前 16 字符)。
func hashDetail(detail string) string {
	h := sha256.Sum256([]byte(detail))
	return hex.EncodeToString(h[:8])
}

// activeListingsBySeat 统计座位活跃挂单数(open|negotiating)。
func (b *ListingBook) activeListingsBySeat(seat int) int {
	n := 0
	for _, l := range b.Listings {
		if l.Seat == seat && (l.Status == ListingStatusOpen || l.Status == ListingStatusNegotiating) {
			n++
		}
	}
	return n
}

// ── 挂牌 ──

// CreateListing 创建挂单(资产/收购)。
// 校验:资产存在、未超活跃上限、非自交易(收购挂单 seat 与资产主不同)。
func (w *World) CreateListing(seat int, lType ListingType, payload ListingPayload, askCNY int64) (*Listing, *errcode.Error) {
	p := w.Players[seat]
	if p == nil || !p.Alive {
		return nil, errcode.Code(errcode.ErrVirtualCityPlayerInactive)
	}
	if w.ListingBook.activeListingsBySeat(seat) >= maxListingsPerSeat {
		return nil, errcode.Code(errcode.ErrVirtualCityListingFull)
	}

	listing := &Listing{
		Type:        lType,
		Seat:        seat,
		Payload:     payload,
		AskCNY:      askCNY,
		Status:      ListingStatusOpen,
		CreateMonth: w.Month,
		ExpireMonth: w.Month + listingExpireMonths,
	}

	switch lType {
	case ListingAsset:
		if payload.Asset == nil {
			return nil, errcode.CodeMsg(errcode.ErrVirtualCityListingInvalid, "asset payload required")
		}
		// 校验资产存在:按快照 kind 在玩家持仓中找到匹配项。
		if !w.assetExists(p, payload.Asset.Asset) {
			return nil, errcode.CodeMsg(errcode.ErrVirtualCityListingInvalid, "asset not in seller portfolio")
		}
		// 批次20 文档3 B2-3:股票挂牌过户同受 T+1 约束(评审口径「更严格
		// 更安全」)—— 可挂量 = 持仓 − 当月买入冻结,不足 → 35044。
		if payload.Asset.Asset.Kind == AssetStockIndex &&
			payload.Asset.Asset.Units > p.stockSellableUnits() {
			return nil, errcode.CodeMsg(errcode.ErrVirtualCityStockT1Locked,
				"当月买入份数 T+1 冻结,暂不可挂牌")
		}
		if askCNY < 100 {
			return nil, errcode.CodeMsg(errcode.ErrVirtualCityListingInvalid, "ask_cny must be >= 100")
		}
	case ListingBuy:
		// 收购挂单:askCNY 为愿意支付的最高价。
		if askCNY < 1000 {
			return nil, errcode.CodeMsg(errcode.ErrVirtualCityListingInvalid, "buy listing ask_cny must be >= 1000")
		}
	default:
		return nil, errcode.CodeMsg(errcode.ErrVirtualCityListingInvalid, "unsupported listing type for CreateListing")
	}

	w.ListingBook.SeqListing++
	listing.ID = fmt.Sprintf("L%d", w.ListingBook.SeqListing)
	w.ListingBook.Listings[listing.ID] = listing
	w.emitEvent("action", seat, fmt.Sprintf("创建挂单 %s(类型 %s,要价 ¥%d)", listing.ID, lType, askCNY))
	return listing, nil
}

// assetExists 校验玩家是否持有指定快照对应的资产。
func (w *World) assetExists(p *Player, snap Asset) bool {
	for i := range p.Assets {
		a := &p.Assets[i]
		if a.Kind != snap.Kind {
			continue
		}
		// house/shop 按城区匹配;side_business 按 kind 匹配;其它按 Units 足够。
		if isHouseKind(a.Kind) || isShopKind(a.Kind) {
			if a.AssetDistrict() == snap.AssetDistrict() {
				return true
			}
			continue
		}
		if a.Units >= snap.Units {
			return true
		}
	}
	return false
}

// CancelListing 取消挂单(仅挂单主可操作,且状态为 open)。
func (w *World) CancelListing(seat int, listingID string) *errcode.Error {
	l := w.ListingBook.Listings[listingID]
	if l == nil {
		return errcode.Code(errcode.ErrVirtualCityListingNotFound)
	}
	if l.Seat != seat {
		return errcode.Code(errcode.ErrVirtualCityNoPrivilege)
	}
	if l.Status != ListingStatusOpen && l.Status != ListingStatusNegotiating {
		return errcode.CodeMsg(errcode.ErrVirtualCityListingInvalid, "cannot cancel listing in status "+l.Status)
	}
	l.Status = ListingStatusCancelled
	w.emitEvent("action", seat, fmt.Sprintf("取消挂单 %s", listingID))
	return nil
}

// GetListings 按类型筛选挂单(空串 = 全量);仅返回 open 状态。
func (w *World) GetListings(lType ListingType) []*Listing {
	var out []*Listing
	for _, l := range w.ListingBook.Listings {
		if l.Status != ListingStatusOpen {
			continue
		}
		if lType != "" && l.Type != lType {
			continue
		}
		out = append(out, l)
	}
	return out
}

// ── 议价 ──

// NegotiateStart 发起议价(响应挂单)。
// proposerSeat 为响应方(买方/借方/贷方),listing 主为响应方。
func (w *World) NegotiateStart(proposerSeat int, listingID string, firstOffer int64) (*NegotiateSession, *errcode.Error) {
	proposer := w.Players[proposerSeat]
	if proposer == nil || !proposer.Alive {
		return nil, errcode.Code(errcode.ErrVirtualCityPlayerInactive)
	}
	l := w.ListingBook.Listings[listingID]
	if l == nil {
		return nil, errcode.Code(errcode.ErrVirtualCityListingNotFound)
	}
	if l.Status != ListingStatusOpen {
		return nil, errcode.CodeMsg(errcode.ErrVirtualCityListingInvalid, "listing not open")
	}
	if l.Seat == proposerSeat {
		return nil, errcode.Code(errcode.ErrVirtualCitySelfTrade)
	}
	if firstOffer < 0 {
		return nil, errcode.CodeMsg(errcode.ErrVirtualCityListingInvalid, "first_offer must be >= 0")
	}

	w.ListingBook.SeqNeg++
	neg := &NegotiateSession{
		ID:           fmt.Sprintf("N%d", w.ListingBook.SeqNeg),
		ListingID:    listingID,
		ProposerSeat: proposerSeat,
		RespondSeat:  l.Seat,
		Status:       NegotiateStatusActive,
		CreateMonth:  w.Month,
		ExpireMonth:  w.Month + negotiateExpireMonths,
		LastOfferCNY: firstOffer,
		LastOfferBy:  proposerSeat,
	}
	neg.Turns = append(neg.Turns, NegotiateTurn{
		From: proposerSeat, OfferCNY: firstOffer, Action: NegotiateActOffer,
	})

	l.Status = ListingStatusNegotiating
	w.ListingBook.Negotiates[neg.ID] = neg
	w.emitEvent("action", proposerSeat, fmt.Sprintf("发起议价 %s 针对挂单 %s(首轮报价 ¥%d)", neg.ID, listingID, firstOffer))
	return neg, nil
}

// NegotiateRespond 响应议价(还价/接受/拒绝)。
// seat 必须是当前轮次响应方;action ∈ {accept,reject,counter,offer}。
func (w *World) NegotiateRespond(seat int, negID string, action string, offerCNY int64, comment string) (string, *errcode.Error) {
	neg := w.ListingBook.Negotiates[negID]
	if neg == nil {
		return "", errcode.Code(errcode.ErrVirtualCityNegotiateNotFound)
	}
	if neg.Status != NegotiateStatusActive {
		return "", errcode.CodeMsg(errcode.ErrVirtualCityNegotiateNotFound, "negotiate not active")
	}
	// 轮次判定:LastOfferBy 的对方必须响应。
	if neg.LastOfferBy == seat {
		return "", errcode.Code(errcode.ErrVirtualCityNotYourTurn)
	}
	p := w.Players[seat]
	if p == nil || !p.Alive {
		return "", errcode.Code(errcode.ErrVirtualCityPlayerInactive)
	}

	turn := NegotiateTurn{From: seat, OfferCNY: offerCNY, Comment: comment, Action: action}

	switch action {
	case NegotiateActAccept:
		// 接受当前报价 → 成交。
		neg.Status = NegotiateStatusDeal
		neg.Turns = append(neg.Turns, turn)
		return w.executeDeal(neg)
	case NegotiateActReject:
		neg.Status = NegotiateStatusReject
		neg.Turns = append(neg.Turns, turn)
		// 拒绝后挂单恢复 open。
		if l := w.ListingBook.Listings[neg.ListingID]; l != nil {
			l.Status = ListingStatusOpen
		}
		w.emitEvent("action", seat, fmt.Sprintf("拒绝议价 %s", negID))
		return "议价已拒绝", nil
	case NegotiateActCounter, NegotiateActOffer:
		if offerCNY < 0 {
			return "", errcode.CodeMsg(errcode.ErrVirtualCityListingInvalid, "counter offer must be >= 0")
		}
		neg.LastOfferCNY = offerCNY
		neg.LastOfferBy = seat
		neg.Turns = append(neg.Turns, turn)
		return "还价已提交", nil
	default:
		return "", errcode.CodeMsg(errcode.ErrVirtualCityListingInvalid, "unknown negotiate action: "+action)
	}
}

// executeDeal 执行议价成交(按挂单类型分派)。
func (w *World) executeDeal(neg *NegotiateSession) (string, *errcode.Error) {
	l := w.ListingBook.Listings[neg.ListingID]
	if l == nil {
		return "", errcode.Code(errcode.ErrVirtualCityListingNotFound)
	}
	finalPrice := neg.LastOfferCNY

	switch l.Type {
	case ListingAsset:
		return w.dealAsset(l, neg, finalPrice)
	case ListingBuy:
		// 收购挂单:proposer 是卖方,l.Seat 是买方(收购方)。
		return w.dealBuy(l, neg, finalPrice)
	case ListingInfo:
		return w.dealInfo(l, neg, finalPrice)
	case ListingLoanOfr, ListingLoanReq:
		return w.dealLoan(l, neg, finalPrice)
	default:
		return "", errcode.CodeMsg(errcode.ErrVirtualCityListingInvalid, "unknown listing type")
	}
}

// dealAsset 资产成交:卖方=l.Seat, 买方=proposer;资产过户 + 资金过户 + 税费。
func (w *World) dealAsset(l *Listing, neg *NegotiateSession, price int64) (string, *errcode.Error) {
	seller := w.Players[l.Seat]
	buyer := w.Players[neg.ProposerSeat]
	if seller == nil || !seller.Alive || buyer == nil || !buyer.Alive {
		return "", errcode.Code(errcode.ErrVirtualCityPlayerInactive)
	}
	snap := l.Payload.Asset.Asset
	// 找到卖方具体资产下标。
	idx := -1
	for i := range seller.Assets {
		a := &seller.Assets[i]
		if a.Kind != snap.Kind {
			continue
		}
		if isHouseKind(a.Kind) || isShopKind(a.Kind) {
			if a.AssetDistrict() == snap.AssetDistrict() {
				idx = i
				break
			}
			continue
		}
		if a.Units >= snap.Units {
			idx = i
			break
		}
	}
	if idx < 0 {
		return "", errcode.CodeMsg(errcode.ErrVirtualCityListingInvalid, "seller asset no longer exists")
	}
	asset := &seller.Assets[idx]

	// 买方现金校验。
	tax := int64(float64(price) * tradeTaxRate)
	fee := int64(float64(price) * tradeAgentRate)
	total := price + tax + fee
	if buyer.Cash < total {
		return "", errcode.Code(errcode.ErrVirtualCityInsufficientCash)
	}

	// 资金过户:buyer → seller(price) + buyer → gov(tax) + buyer → market(fee)。
	w.PaySeatToSeat(buyer.Seat, seller.Seat, price, CatTrade, fmt.Sprintf("购买 %s 来自 %d 号位", asset.Kind, seller.Seat))
	if tax > 0 {
		w.Pay(buyer.Seat, SeatEntity(buyer.Seat), EntityGov, tax, CatTradeFee, "交易增值税")
	}
	if fee > 0 {
		w.Pay(buyer.Seat, SeatEntity(buyer.Seat), EntityMarket, fee, CatTradeFee, "交易中介费")
	}

	// 资产过户:从卖方移除,加入买方(保持快照)。
	transfer := *asset
	transfer.CostCNY = price // 买方成本按成交价重置
	transfer.OpenMonth = w.Month
	if isHouseKind(asset.Kind) {
		transfer.SetSelfOccupied(false) // 买方默认非自住
	}
	w.removeAssetAt(seller, idx)
	buyer.Assets = append(buyer.Assets, transfer)

	l.Status = ListingStatusDeal
	w.emitEvent("action", seller.Seat, fmt.Sprintf("%d 号位向 %d 号位出售 %s,成交价 ¥%d", seller.Seat, buyer.Seat, asset.Kind, price))
	return fmt.Sprintf("成交! %s 以 ¥%d 转让给 %d 号位", asset.Kind, price, buyer.Seat), nil
}

// dealBuy 收购成交:买方=l.Seat(收购方), 卖方=proposer(响应方)。
func (w *World) dealBuy(l *Listing, neg *NegotiateSession, price int64) (string, *errcode.Error) {
	buyer := w.Players[l.Seat]
	seller := w.Players[neg.ProposerSeat]
	if seller == nil || !seller.Alive || buyer == nil || !buyer.Alive {
		return "", errcode.Code(errcode.ErrVirtualCityPlayerInactive)
	}
	// 收购挂单 payload 为 nil(无资产快照),需卖方持有可售资产;此处简化为现金交易。
	// 真实场景:卖方需先挂具体资产;收购挂单仅表达意向,成交时由卖方提供资产。
	// 简化:收购成交 = 买方以 price 现金购入卖方指定资产(由议价留言指定)。
	// 此处按「副业项目转让」处理:卖方如有副业则转让。
	if seller.SideBusiness == nil {
		return "", errcode.CodeMsg(errcode.ErrVirtualCityListingInvalid, "seller has no side business to transfer")
	}
	if buyer.Cash < price {
		return "", errcode.Code(errcode.ErrVirtualCityInsufficientCash)
	}
	w.PaySeatToSeat(buyer.Seat, seller.Seat, price, CatTrade, fmt.Sprintf("收购 %d 号位副业", seller.Seat))
	sb := *seller.SideBusiness
	sb.OpenedMonth = w.Month
	seller.SideBusiness = nil
	buyer.SideBusiness = &sb

	l.Status = ListingStatusDeal
	w.emitEvent("action", l.Seat, fmt.Sprintf("%d 号位收购 %d 号位副业,成交价 ¥%d", buyer.Seat, seller.Seat, price))
	return fmt.Sprintf("收购成交! 副业以 ¥%d 转让给 %d 号位", price, buyer.Seat), nil
}

// dealInfo 信息成交:卖方=l.Seat, 买方=proposer;买方获得明文详情。
func (w *World) dealInfo(l *Listing, neg *NegotiateSession, price int64) (string, *errcode.Error) {
	seller := w.Players[l.Seat]
	buyer := w.Players[neg.ProposerSeat]
	if seller == nil || buyer == nil {
		return "", errcode.Code(errcode.ErrVirtualCityPlayerInactive)
	}
	if buyer.Cash < price {
		return "", errcode.Code(errcode.ErrVirtualCityInsufficientCash)
	}
	w.PaySeatToSeat(buyer.Seat, seller.Seat, price, CatInfoTrade, fmt.Sprintf("购买信息:%s", l.Payload.InfoOffer.Title))
	l.Status = ListingStatusDeal
	w.emitEvent("action", buyer.Seat, fmt.Sprintf("%d 号位购得信息「%s」,成交价 ¥%d", buyer.Seat, l.Payload.InfoOffer.Title, price))
	return fmt.Sprintf("成交! 信息「%s」以 ¥%d 转让(详情已揭示)", l.Payload.InfoOffer.Title, price), nil
}

// dealLoan 借贷成交:根据挂单方向确定借方/贷方,生成 P2PLoan。
func (w *World) dealLoan(l *Listing, neg *NegotiateSession, price int64) (string, *errcode.Error) {
	loan := l.Payload.Loan
	if loan == nil {
		return "", errcode.CodeMsg(errcode.ErrVirtualCityListingInvalid, "loan payload required")
	}
	var lenderSeat, borrowerSeat int
	switch l.Type {
	case ListingLoanOfr:
		// 出借要约:挂单主=贷方,响应方=借方。
		lenderSeat = l.Seat
		borrowerSeat = neg.ProposerSeat
	case ListingLoanReq:
		// 借款请求:挂单主=借方,响应方=贷方。
		borrowerSeat = l.Seat
		lenderSeat = neg.ProposerSeat
	default:
		return "", errcode.CodeMsg(errcode.ErrVirtualCityListingInvalid, "not a loan listing")
	}
	lender := w.Players[lenderSeat]
	borrower := w.Players[borrowerSeat]
	if lender == nil || !lender.Alive || borrower == nil || !borrower.Alive {
		return "", errcode.Code(errcode.ErrVirtualCityPlayerInactive)
	}
	if lender.Cash < loan.PrincipalCNY {
		return "", errcode.Code(errcode.ErrVirtualCityInsufficientCash)
	}
	// 利率取议价最终报价(如有),否则用挂单 MaxRate。
	ratePerMonth := loan.MaxRate
	for i := len(neg.Turns) - 1; i >= 0; i-- {
		if neg.Turns[i].Rate > 0 {
			ratePerMonth = neg.Turns[i].Rate
			break
		}
	}
	if ratePerMonth < p2pMinRatePerMonth {
		ratePerMonth = p2pMinRatePerMonth
	}
	if ratePerMonth > p2pMaxRatePerMonth {
		ratePerMonth = p2pMaxRatePerMonth
	}
	annualRate := ratePerMonth * 12
	payment := AnnuityPayment(loan.PrincipalCNY, annualRate, loan.TermN)

	// 资金过户:lender → borrower(本金)。
	w.PaySeatToSeat(lender.Seat, borrower.Seat, loan.PrincipalCNY, CatLoan, fmt.Sprintf("P2P 借出 → %d 号位", borrowerSeat))

	w.ListingBook.SeqP2P++
	p2p := &P2PLoan{
		ID:             fmt.Sprintf("P2P%d", w.ListingBook.SeqP2P),
		LenderSeat:     lenderSeat,
		BorrowerSeat:   borrowerSeat,
		PrincipalCNY:   loan.PrincipalCNY,
		BalanceCNY:     loan.PrincipalCNY,
		AnnualRate:     annualRate,
		MonthlyPayment: payment,
		TermN:          loan.TermN,
		MonthsLeft:     loan.TermN,
		GuarantorSeat:  -1,
		CreateMonth:    w.Month,
		ListingID:      l.ID,
	}
	w.ListingBook.P2PLoans[p2p.ID] = p2p
	l.Status = ListingStatusDeal
	w.emitEvent("action", lenderSeat, fmt.Sprintf("P2P 借贷合约 %s 成立:%d → %d,本金 ¥%d,月利率 %.2f%%", p2p.ID, lenderSeat, borrowerSeat, loan.PrincipalCNY, ratePerMonth*100))
	return fmt.Sprintf("借贷合约 %s 成立! 本金 ¥%d,月供 ¥%d", p2p.ID, loan.PrincipalCNY, payment), nil
}

// ── 借贷挂单 ──

// CreateLoanListing 创建借贷挂单。
func (w *World) CreateLoanListing(seat int, direction string, principal int64, rate float64, termN int, needGuarantee bool) (*Listing, *errcode.Error) {
	p := w.Players[seat]
	if p == nil || !p.Alive {
		return nil, errcode.Code(errcode.ErrVirtualCityPlayerInactive)
	}
	if w.ListingBook.activeListingsBySeat(seat) >= maxListingsPerSeat {
		return nil, errcode.Code(errcode.ErrVirtualCityListingFull)
	}
	if direction != "lend" && direction != "borrow" {
		return nil, errcode.CodeMsg(errcode.ErrVirtualCityListingInvalid, "direction must be lend|borrow")
	}
	if principal < p2pMinPrincipal {
		return nil, errcode.CodeMsg(errcode.ErrVirtualCityListingInvalid, "principal must be >= 1000")
	}
	if termN < p2pMinTermN || termN > p2pMaxTermN {
		return nil, errcode.CodeMsg(errcode.ErrVirtualCityListingInvalid, "term_n must be in [1,60]")
	}
	// 利率校验:月利率 0.3%-3.6%。
	if rate < p2pMinRatePerMonth || rate > p2pMaxRatePerMonth {
		return nil, errcode.Code(errcode.ErrVirtualCityLoanRateInvalid)
	}
	// 借入方需信用分 ≥ 500。
	if direction == "borrow" && p.CreditScore < 500 {
		return nil, errcode.Code(errcode.ErrVirtualCityLoanNoCredit)
	}

	lType := ListingLoanOfr
	if direction == "borrow" {
		lType = ListingLoanReq
	}
	listing := &Listing{
		Type:    lType,
		Seat:    seat,
		AskCNY:  principal,
		Status:  ListingStatusOpen,
		CreateMonth: w.Month,
		ExpireMonth: w.Month + listingExpireMonths,
		Payload: ListingPayload{
			Loan: &LoanPayload{
				Direction:     direction,
				PrincipalCNY:  principal,
				MaxRate:       rate,
				TermN:         termN,
				NeedGuarantee: needGuarantee,
			},
		},
	}
	w.ListingBook.SeqListing++
	listing.ID = fmt.Sprintf("L%d", w.ListingBook.SeqListing)
	w.ListingBook.Listings[listing.ID] = listing
	w.emitEvent("action", seat, fmt.Sprintf("创建借贷挂单 %s(%s ¥%d,月利率 %.2f%%)", listing.ID, direction, principal, rate*100))
	return listing, nil
}

// AcceptLoan 接受借贷要约(直接成交,无需议价)。
// seat 为响应方;listing 为 loan_ofr(响应方=借方)或 loan_req(响应方=贷方)。
func (w *World) AcceptLoan(seat int, listingID string) (*P2PLoan, *errcode.Error) {
	p := w.Players[seat]
	if p == nil || !p.Alive {
		return nil, errcode.Code(errcode.ErrVirtualCityPlayerInactive)
	}
	l := w.ListingBook.Listings[listingID]
	if l == nil {
		return nil, errcode.Code(errcode.ErrVirtualCityListingNotFound)
	}
	if l.Status != ListingStatusOpen {
		return nil, errcode.CodeMsg(errcode.ErrVirtualCityListingInvalid, "listing not open")
	}
	if l.Seat == seat {
		return nil, errcode.Code(errcode.ErrVirtualCitySelfTrade)
	}
	if l.Type != ListingLoanOfr && l.Type != ListingLoanReq {
		return nil, errcode.CodeMsg(errcode.ErrVirtualCityListingInvalid, "not a loan listing")
	}
	loan := l.Payload.Loan
	if loan == nil {
		return nil, errcode.CodeMsg(errcode.ErrVirtualCityListingInvalid, "loan payload required")
	}
	// 借入方信用校验。
	var lenderSeat, borrowerSeat int
	switch l.Type {
	case ListingLoanOfr:
		lenderSeat = l.Seat
		borrowerSeat = seat
	case ListingLoanReq:
		borrowerSeat = l.Seat
		lenderSeat = seat
	}
	borrower := w.Players[borrowerSeat]
	if borrower.CreditScore < 500 {
		return nil, errcode.Code(errcode.ErrVirtualCityLoanNoCredit)
	}
	lender := w.Players[lenderSeat]
	if lender.Cash < loan.PrincipalCNY {
		return nil, errcode.Code(errcode.ErrVirtualCityInsufficientCash)
	}
	annualRate := loan.MaxRate * 12
	payment := AnnuityPayment(loan.PrincipalCNY, annualRate, loan.TermN)

	w.PaySeatToSeat(lender.Seat, borrower.Seat, loan.PrincipalCNY, CatLoan, fmt.Sprintf("P2P 借出 → %d 号位", borrowerSeat))

	w.ListingBook.SeqP2P++
	p2p := &P2PLoan{
		ID:             fmt.Sprintf("P2P%d", w.ListingBook.SeqP2P),
		LenderSeat:     lenderSeat,
		BorrowerSeat:   borrowerSeat,
		PrincipalCNY:   loan.PrincipalCNY,
		BalanceCNY:     loan.PrincipalCNY,
		AnnualRate:     annualRate,
		MonthlyPayment: payment,
		TermN:          loan.TermN,
		MonthsLeft:     loan.TermN,
		GuarantorSeat:  -1,
		CreateMonth:    w.Month,
		ListingID:      l.ID,
	}
	w.ListingBook.P2PLoans[p2p.ID] = p2p
	l.Status = ListingStatusDeal
	w.emitEvent("action", seat, fmt.Sprintf("接受借贷挂单 %s,合约 %s 成立", listingID, p2p.ID))
	return p2p, nil
}

// RepayLoan 还款(借方主动还本付息)。
// amount=0 表示偿还当月月供;amount>0 表示提前还本(额外)。
func (w *World) RepayLoan(seat int, loanID string, amount int64) (string, *errcode.Error) {
	loan := w.ListingBook.P2PLoans[loanID]
	if loan == nil {
		return "", errcode.CodeMsg(errcode.ErrVirtualCityListingInvalid, "loan not found")
	}
	if loan.BorrowerSeat != seat {
		return "", errcode.Code(errcode.ErrVirtualCityNoPrivilege)
	}
	if loan.MonthsLeft <= 0 || loan.BalanceCNY <= 0 {
		return "", errcode.CodeMsg(errcode.ErrVirtualCityListingInvalid, "loan already settled")
	}
	borrower := w.Players[seat]
	lender := w.Players[loan.LenderSeat]
	if borrower == nil || lender == nil {
		return "", errcode.Code(errcode.ErrVirtualCityPlayerInactive)
	}

	// 当月应还利息。
	interest := int64(float64(loan.BalanceCNY) * loan.AnnualRate / 12)
	due := loan.MonthlyPayment
	if amount > 0 {
		due += amount
	}
	if due > loan.BalanceCNY+interest {
		due = loan.BalanceCNY + interest
	}
	if borrower.Cash < due {
		return "", errcode.Code(errcode.ErrVirtualCityInsufficientCash)
	}

	// 还款分配:先息后本。
	w.PaySeatToSeat(seat, loan.LenderSeat, interest, CatP2PInterest, fmt.Sprintf("P2P 利息 %s", loanID))
	principal := due - interest
	if principal > 0 {
		w.PaySeatToSeat(seat, loan.LenderSeat, principal, CatP2PRepay, fmt.Sprintf("P2P 还本 %s", loanID))
	}
	loan.BalanceCNY -= principal
	if loan.BalanceCNY <= 0 {
		loan.BalanceCNY = 0
		loan.MonthsLeft = 0
		w.emitEvent("action", seat, fmt.Sprintf("P2P 借贷 %s 已结清", loanID))
		return "借贷已结清", nil
	}
	// 重算剩余月供。
	if loan.MonthsLeft > 0 {
		loan.MonthlyPayment = AnnuityPayment(loan.BalanceCNY, loan.AnnualRate, loan.MonthsLeft)
	}
	return fmt.Sprintf("已还 ¥%d,剩余本金 ¥%d", due, loan.BalanceCNY), nil
}

// AddGuarantor 为借贷添加担保人。
func (w *World) AddGuarantor(loanID string, guarantorSeat int) *errcode.Error {
	loan := w.ListingBook.P2PLoans[loanID]
	if loan == nil {
		return errcode.CodeMsg(errcode.ErrVirtualCityListingInvalid, "loan not found")
	}
	if loan.GuarantorSeat != -1 {
		return errcode.Code(errcode.ErrVirtualCityGuarantorConflict)
	}
	if guarantorSeat == loan.LenderSeat || guarantorSeat == loan.BorrowerSeat {
		return errcode.Code(errcode.ErrVirtualCityGuarantorConflict)
	}
	g := w.Players[guarantorSeat]
	if g == nil || !g.Alive {
		return errcode.Code(errcode.ErrVirtualCityPlayerInactive)
	}
	if g.CreditScore < 600 {
		return errcode.Code(errcode.ErrVirtualCityLoanNoCredit)
	}
	loan.GuarantorSeat = guarantorSeat
	w.emitEvent("action", guarantorSeat, fmt.Sprintf("%d 号位为借贷 %s 提供担保", guarantorSeat, loanID))
	return nil
}

// ── 信息交易 ──

// SellInfo 出售信息(密封暗标)。
func (w *World) SellInfo(seat int, category string, title string, detail string, minBid int64) (*Listing, *errcode.Error) {
	p := w.Players[seat]
	if p == nil || !p.Alive {
		return nil, errcode.Code(errcode.ErrVirtualCityPlayerInactive)
	}
	if w.ListingBook.activeListingsBySeat(seat) >= maxListingsPerSeat {
		return nil, errcode.Code(errcode.ErrVirtualCityListingFull)
	}
	if category != "market" && category != "intel" && category != "personal" {
		return nil, errcode.CodeMsg(errcode.ErrVirtualCityListingInvalid, "category must be market|intel|personal")
	}
	if title == "" {
		return nil, errcode.CodeMsg(errcode.ErrVirtualCityListingInvalid, "title required")
	}
	if minBid < 100 {
		return nil, errcode.CodeMsg(errcode.ErrVirtualCityListingInvalid, "min_bid must be >= 100")
	}
	listing := &Listing{
		Type:    ListingInfo,
		Seat:    seat,
		AskCNY:  minBid,
		Status:  ListingStatusOpen,
		CreateMonth: w.Month,
		ExpireMonth: w.Month + listingExpireMonths,
		Payload: ListingPayload{
			InfoOffer: &InfoOfferPayload{
				Category:   category,
				Title:      title,
				DetailHash: hashDetail(detail),
				Detail:     detail,
				MinBidCNY:  minBid,
			},
		},
	}
	w.ListingBook.SeqListing++
	listing.ID = fmt.Sprintf("L%d", w.ListingBook.SeqListing)
	w.ListingBook.Listings[listing.ID] = listing
	w.emitEvent("action", seat, fmt.Sprintf("挂牌出售信息 %s:%s(暗标最低 ¥%d)", listing.ID, title, minBid))
	return listing, nil
}

// BidInfo 参与暗标(密封)。
func (w *World) BidInfo(seat int, listingID string, bidCNY int64) *errcode.Error {
	p := w.Players[seat]
	if p == nil || !p.Alive {
		return errcode.Code(errcode.ErrVirtualCityPlayerInactive)
	}
	l := w.ListingBook.Listings[listingID]
	if l == nil {
		return errcode.Code(errcode.ErrVirtualCityListingNotFound)
	}
	if l.Type != ListingInfo || l.Status != ListingStatusOpen {
		return errcode.CodeMsg(errcode.ErrVirtualCityListingInvalid, "not an open info listing")
	}
	if l.Seat == seat {
		return errcode.Code(errcode.ErrVirtualCitySelfTrade)
	}
	if bidCNY < l.Payload.InfoOffer.MinBidCNY {
		return errcode.Code(errcode.ErrVirtualCityBidTooLow)
	}
	// 密封:仅记录出价,不校验现金(开标时校验)。
	l.Payload.InfoOffer.Bids = append(l.Payload.InfoOffer.Bids, InfoBid{Seat: seat, Amount: bidCNY})
	w.emitEvent("action", seat, fmt.Sprintf("%d 号位参与暗标 %s(出价 ¥%d)", seat, listingID, bidCNY))
	return nil
}

// RevealInfo 开标(挂单主在挂单到期/主动发起时揭示最高价中标者)。
// 返回中标者座位(-1 表示流标)与明文详情。
func (w *World) RevealInfo(listingID string) (int, string, *errcode.Error) {
	l := w.ListingBook.Listings[listingID]
	if l == nil {
		return -1, "", errcode.Code(errcode.ErrVirtualCityListingNotFound)
	}
	if l.Type != ListingInfo {
		return -1, "", errcode.CodeMsg(errcode.ErrVirtualCityListingInvalid, "not an info listing")
	}
	if l.Status != ListingStatusOpen && l.Status != ListingStatusExpired {
		return -1, "", errcode.CodeMsg(errcode.ErrVirtualCityListingInvalid, "listing not revealable")
	}
	if len(l.Payload.InfoOffer.Bids) == 0 {
		l.Status = ListingStatusExpired
		return -1, "", errcode.Code(errcode.ErrVirtualCityInfoNotFound)
	}
	// 最高价中标;平局按座位号小者优先(简化;设计文档为人脉高者得,此处用座位号)。
	winner := -1
	maxBid := int64(0)
	for _, b := range l.Payload.InfoOffer.Bids {
		if b.Amount > maxBid || (b.Amount == maxBid && (winner < 0 || b.Seat < winner)) {
			maxBid = b.Amount
			winner = b.Seat
		}
	}
	if winner < 0 {
		l.Status = ListingStatusExpired
		return -1, "", errcode.Code(errcode.ErrVirtualCityInfoNotFound)
	}
	// 中标者现金校验。
	bidder := w.Players[winner]
	seller := w.Players[l.Seat]
	if bidder == nil || !bidder.Alive || seller == nil {
		l.Status = ListingStatusExpired
		return -1, "", errcode.Code(errcode.ErrVirtualCityPlayerInactive)
	}
	if bidder.Cash < maxBid {
		// 现金不足 → 流标。
		l.Status = ListingStatusExpired
		return -1, "", errcode.Code(errcode.ErrVirtualCityInsufficientCash)
	}
	w.PaySeatToSeat(winner, l.Seat, maxBid, CatInfoTrade, fmt.Sprintf("暗标购得:%s", l.Payload.InfoOffer.Title))
	l.Status = ListingStatusDeal
	w.emitEvent("action", winner, fmt.Sprintf("暗标 %s 开标:%d 号位以 ¥%d 购得「%s」", listingID, winner, maxBid, l.Payload.InfoOffer.Title))
	return winner, l.Payload.InfoOffer.Detail, nil
}

// ── 月结 ──

// SettleP2PLoans 月结:逐笔 P2PLoan 月供扣款/逾期判定/担保代偿(§6.3)。
func (w *World) SettleP2PLoans(res *SettleResult) {
	for _, loan := range w.ListingBook.P2PLoans {
		if loan.MonthsLeft <= 0 || loan.BalanceCNY <= 0 {
			continue
		}
		borrower := w.Players[loan.BorrowerSeat]
		lender := w.Players[loan.LenderSeat]
		if borrower == nil || !borrower.Alive || lender == nil {
			continue
		}
		interest := int64(float64(loan.BalanceCNY) * loan.AnnualRate / 12)
		principal := loan.MonthlyPayment - interest
		if principal < 0 {
			principal = 0
		}
		if principal > loan.BalanceCNY {
			principal = loan.BalanceCNY
		}
		due := interest + principal

		if borrower.Cash >= due {
			// 正常还款。
			w.PaySeatToSeat(borrower.Seat, loan.LenderSeat, interest, CatP2PInterest, fmt.Sprintf("P2P 月供利息 %s", loan.ID))
			if principal > 0 {
				w.PaySeatToSeat(borrower.Seat, loan.LenderSeat, principal, CatP2PRepay, fmt.Sprintf("P2P 月供还本 %s", loan.ID))
			}
			loan.BalanceCNY -= principal
			loan.MonthsLeft--
			loan.Overdue = false
			loan.OverdueStreak = 0
		} else {
			// 逾期:罚息 5%、信用分 −50。
			loan.Overdue = true
			loan.OverdueStreak++
			penalty := int64(float64(due) * p2pPenaltyRate)
			if borrower.Cash > 0 {
				w.PaySeatToSeat(borrower.Seat, loan.LenderSeat, borrower.Cash, CatP2PInterest, fmt.Sprintf("P2P 逾期部分支付 %s", loan.ID))
			}
			if borrower.CreditScore >= overdueCreditHit {
				borrower.CreditScore -= overdueCreditHit
			} else {
				borrower.CreditScore = 0
			}
			w.emitEvent("settle", borrower.Seat, fmt.Sprintf("P2P 借贷 %s 逾期(第 %d 月),罚息 ¥%d", loan.ID, loan.OverdueStreak, penalty))

			// 连续 3 月逾期 → 担保代偿。
			if loan.OverdueStreak >= p2pMaxOverdueStreak && loan.GuarantorSeat >= 0 {
				w.guarantorCompensate(loan)
			}
		}
		if loan.BalanceCNY <= 0 {
			loan.BalanceCNY = 0
			loan.MonthsLeft = 0
		}
	}
}

// guarantorCompensate 担保人代偿(连续 3 月逾期触发)。
func (w *World) guarantorCompensate(loan *P2PLoan) {
	guarantor := w.Players[loan.GuarantorSeat]
	lender := w.Players[loan.LenderSeat]
	if guarantor == nil || !guarantor.Alive || lender == nil {
		return
	}
	// 代偿金额 = 当月应还(利息+本金)。
	interest := int64(float64(loan.BalanceCNY) * loan.AnnualRate / 12)
	principal := loan.MonthlyPayment - interest
	if principal < 0 {
		principal = 0
	}
	due := interest + principal
	pay := due
	if guarantor.Cash < pay {
		pay = guarantor.Cash
	}
	if pay <= 0 {
		return
	}
	w.PaySeatToSeat(guarantor.Seat, loan.LenderSeat, pay, CatP2PRepay, fmt.Sprintf("担保代偿 %s", loan.ID))
	loan.BalanceCNY -= principal
	if loan.BalanceCNY < 0 {
		loan.BalanceCNY = 0
	}
	loan.OverdueStreak = 0
	loan.Overdue = false
	w.emitEvent("settle", guarantor.Seat, fmt.Sprintf("担保人 %d 号位代偿 P2P 借贷 %s ¥%d", guarantor.Seat, loan.ID, pay))
}

// ExpiredListingsCleanup 挂单过期清理(创建月+3 未成交 → expired)。
func (w *World) ExpiredListingsCleanup() {
	for _, l := range w.ListingBook.Listings {
		if l.Status != ListingStatusOpen && l.Status != ListingStatusNegotiating {
			continue
		}
		if w.Month > l.ExpireMonth {
			l.Status = ListingStatusExpired
			w.emitEvent("action", l.Seat, fmt.Sprintf("挂单 %s 过期", l.ID))
		}
	}
}

// ExpiredNegotiatesCleanup 议价过期清理(创建月+1 未成交 → expired)。
func (w *World) ExpiredNegotiatesCleanup() {
	for _, neg := range w.ListingBook.Negotiates {
		if neg.Status != NegotiateStatusActive {
			continue
		}
		if w.Month > neg.ExpireMonth {
			neg.Status = NegotiateStatusExpired
			// 关联挂单恢复 open。
			if l := w.ListingBook.Listings[neg.ListingID]; l != nil && l.Status == ListingStatusNegotiating {
				l.Status = ListingStatusOpen
			}
			w.emitEvent("action", neg.ProposerSeat, fmt.Sprintf("议价 %s 过期", neg.ID))
		}
	}
}
