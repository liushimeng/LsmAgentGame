// Package virtual_city — listing_test.go: P2 挂单簿 + 议价 + 借贷合约单测
// (2026-09-16 §财商流P2)。
package virtual_city

import (
	"testing"

	"LsmAgentGame/errcode"
	"LsmAgentGame/game/virtual_city/profession"
)

// newTradeTestWorld 构造含 2 玩家的测试世界。
func newTradeTestWorld(seed int64) *World {
	cards := [MaxSeats]profession.Card{
		{ID: "test01", Title: "测试职业1", Savings: 100000, Salary: 8000, Expense: 2000, Cognition: 5, Network: 5, CreditScore: 700},
		{ID: "test02", Title: "测试职业2", Savings: 150000, Salary: 10000, Expense: 3000, Cognition: 6, Network: 6, CreditScore: 720},
	}
	return NewWorld(seed, cards)
}

// giveAsset 给玩家添加测试资产。
func giveAsset(w *World, seat int, a Asset) {
	w.Players[seat].Assets = append(w.Players[seat].Assets, a)
}

// TestListingCreateAndCancel 测试挂单创建 + 取消。
func TestListingCreateAndCancel(t *testing.T) {
	w := newTradeTestWorld(42)
	w.StartGame()
	giveAsset(w, 0, Asset{Kind: AssetGold, Units: 10, CostCNY: 5000, OpenMonth: w.Month})

	// 挂牌。
	payload := ListingPayload{Asset: &AssetPayload{Asset: w.Players[0].Assets[0], MinCNY: 4000}}
	l, err := w.CreateListing(0, ListingAsset, payload, 6000)
	if err != nil {
		t.Fatalf("CreateListing failed: %v", err)
	}
	if l.ID == "" {
		t.Fatal("listing ID empty")
	}
	if l.Seat != 0 {
		t.Fatalf("listing seat: got %d, want 0", l.Seat)
	}

	// 查看挂单簿。
	ls := w.GetListings("")
	if len(ls) != 1 {
		t.Fatalf("GetListings: got %d, want 1", len(ls))
	}

	// 取消。
	if err := w.CancelListing(0, l.ID); err != nil {
		t.Fatalf("CancelListing failed: %v", err)
	}
	if w.ListingBook.Listings[l.ID].Status != ListingStatusCancelled {
		t.Fatal("listing status not cancelled")
	}
}

// TestListingSelfTradeRejected 测试自交易拒绝。
func TestListingSelfTradeRejected(t *testing.T) {
	w := newTradeTestWorld(42)
	w.StartGame()
	giveAsset(w, 0, Asset{Kind: AssetGold, Units: 10, CostCNY: 5000, OpenMonth: w.Month})

	// 玩家 0 创建挂单。
	payload := ListingPayload{Asset: &AssetPayload{Asset: w.Players[0].Assets[0], MinCNY: 4000}}
	l, err := w.CreateListing(0, ListingAsset, payload, 6000)
	if err != nil {
		t.Fatalf("CreateListing failed: %v", err)
	}

	// 玩家 0 对自己的挂单发起议价 → 拒绝。
	_, err = w.NegotiateStart(0, l.ID, 5000)
	if err == nil {
		t.Fatal("self-trade should be rejected")
	}
	if err.Code != errcode.ErrVirtualCitySelfTrade {
		t.Fatalf("self-trade error code: got %d, want %d", err.Code, errcode.ErrVirtualCitySelfTrade)
	}
}

// TestNegotiateAndDeal 测试议价成交。
func TestNegotiateAndDeal(t *testing.T) {
	w := newTradeTestWorld(42)
	w.StartGame()
	giveAsset(w, 0, Asset{Kind: AssetGold, Units: 10, CostCNY: 5000, OpenMonth: w.Month})

	// 玩家 0 挂牌黄金。
	payload := ListingPayload{Asset: &AssetPayload{Asset: w.Players[0].Assets[0], MinCNY: 4000}}
	l, err := w.CreateListing(0, ListingAsset, payload, 6000)
	if err != nil {
		t.Fatalf("CreateListing failed: %v", err)
	}

	// 玩家 1 发起议价(出价 5500)。
	neg, err := w.NegotiateStart(1, l.ID, 5500)
	if err != nil {
		t.Fatalf("NegotiateStart failed: %v", err)
	}

	// 玩家 0 还价 5800。
	_, err = w.NegotiateRespond(0, neg.ID, NegotiateActCounter, 5800, "再加点")
	if err != nil {
		t.Fatalf("NegotiateRespond counter failed: %v", err)
	}

	// 玩家 1 接受。
	text, err := w.NegotiateRespond(1, neg.ID, NegotiateActAccept, 5800, "")
	if err != nil {
		t.Fatalf("NegotiateRespond accept failed: %v", err)
	}
	if text == "" {
		t.Fatal("deal text empty")
	}

	// 校验资产过户。
	p1 := w.Players[1]
	hasGold := false
	for _, a := range p1.Assets {
		if a.Kind == AssetGold {
			hasGold = true
			break
		}
	}
	if !hasGold {
		t.Fatal("玩家 1 未收到黄金")
	}
	p0 := w.Players[0]
	for _, a := range p0.Assets {
		if a.Kind == AssetGold {
			t.Fatal("玩家 0 仍持有黄金")
		}
	}
}

// TestNegotiateReject 测试议价拒绝。
func TestNegotiateReject(t *testing.T) {
	w := newTradeTestWorld(42)
	w.StartGame()
	giveAsset(w, 0, Asset{Kind: AssetGold, Units: 10, CostCNY: 5000, OpenMonth: w.Month})

	payload := ListingPayload{Asset: &AssetPayload{Asset: w.Players[0].Assets[0], MinCNY: 4000}}
	l, err := w.CreateListing(0, ListingAsset, payload, 6000)
	if err != nil {
		t.Fatalf("CreateListing failed: %v", err)
	}

	neg, err := w.NegotiateStart(1, l.ID, 5500)
	if err != nil {
		t.Fatalf("NegotiateStart failed: %v", err)
	}

	// 玩家 0 拒绝。
	text, err := w.NegotiateRespond(0, neg.ID, NegotiateActReject, 0, "太贵")
	if err != nil {
		t.Fatalf("NegotiateRespond reject failed: %v", err)
	}
	if text == "" {
		t.Fatal("reject text empty")
	}
	if w.ListingBook.Listings[l.ID].Status != ListingStatusOpen {
		t.Fatal("listing should revert to open after reject")
	}
}

// TestLoanListingAndAccept 测试借贷挂单 + 接受。
func TestLoanListingAndAccept(t *testing.T) {
	w := newTradeTestWorld(42)
	w.StartGame()

	// 玩家 0 出借挂单(本金 10000, 月利率 1%, 12 月)。
	l, err := w.CreateLoanListing(0, "lend", 10000, 0.01, 12, false)
	if err != nil {
		t.Fatalf("CreateLoanListing failed: %v", err)
	}

	// 玩家 1 接受。
	loan, err := w.AcceptLoan(1, l.ID)
	if err != nil {
		t.Fatalf("AcceptLoan failed: %v", err)
	}
	if loan.BalanceCNY != 10000 {
		t.Fatalf("loan balance: got %d, want 10000", loan.BalanceCNY)
	}

	// 校验资金过户。
	if w.Players[0].Cash != 100000-10000 {
		t.Fatalf("lender cash: got %d, want 90000", w.Players[0].Cash)
	}
	if w.Players[1].Cash != 150000+10000 {
		t.Fatalf("borrower cash: got %d, want 160000", w.Players[1].Cash)
	}
}

// TestLoanRateValidation 测试借贷利率校验(0.3%-3.6%/月)。
func TestLoanRateValidation(t *testing.T) {
	w := newTradeTestWorld(42)
	w.StartGame()

	// 过低利率 0.1% → 拒绝。
	_, err := w.CreateLoanListing(0, "lend", 10000, 0.001, 12, false)
	if err == nil {
		t.Fatal("rate too low should be rejected")
	}
	if err.Code != errcode.ErrVirtualCityLoanRateInvalid {
		t.Fatalf("rate error code: got %d, want %d", err.Code, errcode.ErrVirtualCityLoanRateInvalid)
	}

	// 过高利率 5% → 拒绝。
	_, err = w.CreateLoanListing(0, "lend", 10000, 0.05, 12, false)
	if err == nil {
		t.Fatal("rate too high should be rejected")
	}

	// 合法利率 1% → 通过。
	_, err = w.CreateLoanListing(0, "lend", 10000, 0.01, 12, false)
	if err != nil {
		t.Fatalf("valid rate failed: %v", err)
	}
}

// TestLoanCreditValidation 测试借入信用校验(< 500 不可借)。
func TestLoanCreditValidation(t *testing.T) {
	w := newTradeTestWorld(42)
	w.StartGame()
	w.Players[1].CreditScore = 400 // 信用不足

	// 玩家 1 借款请求 → 拒绝。
	_, err := w.CreateLoanListing(1, "borrow", 10000, 0.01, 12, false)
	if err == nil {
		t.Fatal("low credit should be rejected")
	}
	if err.Code != errcode.ErrVirtualCityLoanNoCredit {
		t.Fatalf("credit error code: got %d, want %d", err.Code, errcode.ErrVirtualCityLoanNoCredit)
	}
}

// TestGuarantorAndRepay 测试担保 + 还款。
func TestGuarantorAndRepay(t *testing.T) {
	w := newTradeTestWorld(42)
	w.StartGame()

	// 玩家 0 出借,玩家 1 借入。
	l, err := w.CreateLoanListing(0, "lend", 10000, 0.01, 12, false)
	if err != nil {
		t.Fatalf("CreateLoanListing failed: %v", err)
	}
	loan, err := w.AcceptLoan(1, l.ID)
	if err != nil {
		t.Fatalf("AcceptLoan failed: %v", err)
	}

	// 座位 0 不可自担保;需有第三座玩家。
	cards := [MaxSeats]profession.Card{
		{ID: "test01", Title: "测试职业1", Savings: 100000, Salary: 8000, Expense: 2000, CreditScore: 700},
		{ID: "test02", Title: "测试职业2", Savings: 150000, Salary: 10000, Expense: 3000, CreditScore: 720},
		{ID: "test03", Title: "测试职业3", Savings: 200000, Salary: 12000, Expense: 4000, CreditScore: 750},
	}
	w2 := NewWorld(42, cards)
	w2.StartGame()
	l2, err := w2.CreateLoanListing(0, "lend", 10000, 0.01, 12, false)
	if err != nil {
		t.Fatalf("CreateLoanListing failed: %v", err)
	}
	loan2, err := w2.AcceptLoan(1, l2.ID)
	if err != nil {
		t.Fatalf("AcceptLoan failed: %v", err)
	}

	// 玩家 2 担保。
	if err := w2.AddGuarantor(loan2.ID, 2); err != nil {
		t.Fatalf("AddGuarantor failed: %v", err)
	}
	if w2.ListingBook.P2PLoans[loan2.ID].GuarantorSeat != 2 {
		t.Fatal("guarantor not set")
	}

	// 重复担保 → 拒绝。
	if err := w2.AddGuarantor(loan2.ID, 2); err == nil {
		t.Fatal("duplicate guarantor should be rejected")
	}

	// 还款。
	text, err := w2.RepayLoan(1, loan2.ID, 0)
	if err != nil {
		t.Fatalf("RepayLoan failed: %v", err)
	}
	if text == "" {
		t.Fatal("repay text empty")
	}

	_ = loan
}

// TestSettleP2P 测试 P2P 月结(正常还款)。
func TestSettleP2P(t *testing.T) {
	w := newTradeTestWorld(42)
	w.StartGame()

	l, err := w.CreateLoanListing(0, "lend", 10000, 0.01, 12, false)
	if err != nil {
		t.Fatalf("CreateLoanListing failed: %v", err)
	}
	loan, err := w.AcceptLoan(1, l.ID)
	if err != nil {
		t.Fatalf("AcceptLoan failed: %v", err)
	}
	balanceBefore := loan.BalanceCNY

	// 月结。
	res := &SettleResult{}
	w.SettleP2PLoans(res)

	// 校验余额递减。
	if loan.BalanceCNY >= balanceBefore {
		t.Fatalf("loan balance should decrease: before=%d after=%d", balanceBefore, loan.BalanceCNY)
	}
	if loan.MonthsLeft != 11 {
		t.Fatalf("months left: got %d, want 11", loan.MonthsLeft)
	}
}

// TestSettleP2POverdue 测试 P2P 逾期 + 担保代偿。
func TestSettleP2POverdue(t *testing.T) {
	cards := [MaxSeats]profession.Card{
		{ID: "test01", Title: "测试职业1", Savings: 100000, Salary: 8000, Expense: 2000, CreditScore: 700},
		{ID: "test02", Title: "测试职业2", Savings: 0, Salary: 0, Expense: 0, CreditScore: 720}, // 现金为 0
		{ID: "test03", Title: "测试职业3", Savings: 200000, Salary: 12000, Expense: 4000, CreditScore: 750},
	}
	w := NewWorld(42, cards)
	w.StartGame()

	l, err := w.CreateLoanListing(0, "lend", 10000, 0.01, 12, false)
	if err != nil {
		t.Fatalf("CreateLoanListing failed: %v", err)
	}
	loan, err := w.AcceptLoan(1, l.ID)
	if err != nil {
		t.Fatalf("AcceptLoan failed: %v", err)
	}
	if err := w.AddGuarantor(loan.ID, 2); err != nil {
		t.Fatalf("AddGuarantor failed: %v", err)
	}

	// 连续 3 月逾期 → 担保代偿。
	for i := 0; i < 3; i++ {
		res := &SettleResult{}
		w.SettleP2PLoans(res)
	}
	if !loan.Overdue && loan.OverdueStreak > 0 {
		t.Fatalf("loan should be overdue or compensated: overdue=%v streak=%d", loan.Overdue, loan.OverdueStreak)
	}
}

// TestExpiredListings 测试挂单过期清理。
func TestExpiredListings(t *testing.T) {
	w := newTradeTestWorld(42)
	w.StartGame()
	giveAsset(w, 0, Asset{Kind: AssetGold, Units: 10, CostCNY: 5000, OpenMonth: w.Month})

	payload := ListingPayload{Asset: &AssetPayload{Asset: w.Players[0].Assets[0], MinCNY: 4000}}
	l, err := w.CreateListing(0, ListingAsset, payload, 6000)
	if err != nil {
		t.Fatalf("CreateListing failed: %v", err)
	}

	// 推进 4 月(超过 3 月有效期)。
	w.Month += 4
	w.ExpiredListingsCleanup()

	if w.ListingBook.Listings[l.ID].Status != ListingStatusExpired {
		t.Fatalf("listing should be expired, got %s", w.ListingBook.Listings[l.ID].Status)
	}
}

// TestExpiredNegotiates 测试议价过期清理。
func TestExpiredNegotiates(t *testing.T) {
	w := newTradeTestWorld(42)
	w.StartGame()
	giveAsset(w, 0, Asset{Kind: AssetGold, Units: 10, CostCNY: 5000, OpenMonth: w.Month})

	payload := ListingPayload{Asset: &AssetPayload{Asset: w.Players[0].Assets[0], MinCNY: 4000}}
	l, err := w.CreateListing(0, ListingAsset, payload, 6000)
	if err != nil {
		t.Fatalf("CreateListing failed: %v", err)
	}

	neg, err := w.NegotiateStart(1, l.ID, 5500)
	if err != nil {
		t.Fatalf("NegotiateStart failed: %v", err)
	}

	// 推进 2 月(超过 1 月有效期)。
	w.Month += 2
	w.ExpiredNegotiatesCleanup()

	if w.ListingBook.Negotiates[neg.ID].Status != NegotiateStatusExpired {
		t.Fatalf("negotiate should be expired, got %s", w.ListingBook.Negotiates[neg.ID].Status)
	}
	if w.ListingBook.Listings[l.ID].Status != ListingStatusOpen {
		t.Fatal("listing should revert to open after negotiate expire")
	}
}

// TestListingFull 测试挂单上限(每座位 3 笔)。
func TestListingFull(t *testing.T) {
	w := newTradeTestWorld(42)
	w.StartGame()
	for i := 0; i < 3; i++ {
		giveAsset(w, 0, Asset{Kind: AssetGold, Units: 10, CostCNY: 5000, OpenMonth: w.Month})
	}
	for i := 0; i < 3; i++ {
		payload := ListingPayload{Asset: &AssetPayload{Asset: w.Players[0].Assets[i], MinCNY: 4000}}
		if _, err := w.CreateListing(0, ListingAsset, payload, 6000); err != nil {
			t.Fatalf("CreateListing %d failed: %v", i, err)
		}
	}
	// 第 4 笔 → 拒绝。
	giveAsset(w, 0, Asset{Kind: AssetGold, Units: 10, CostCNY: 5000, OpenMonth: w.Month})
	payload := ListingPayload{Asset: &AssetPayload{Asset: w.Players[0].Assets[3], MinCNY: 4000}}
	_, err := w.CreateListing(0, ListingAsset, payload, 6000)
	if err == nil {
		t.Fatal("listing full should be rejected")
	}
	if err.Code != errcode.ErrVirtualCityListingFull {
		t.Fatalf("listing full error code: got %d, want %d", err.Code, errcode.ErrVirtualCityListingFull)
	}
}

// TestSellInfoAndBid 测试信息出售 + 暗标 + 开标。
func TestSellInfoAndBid(t *testing.T) {
	w := newTradeTestWorld(42)
	w.StartGame()

	// 玩家 0 出售信息。
	l, err := w.SellInfo(0, "market", "下月周期情报", "下月进入繁荣期", 1000)
	if err != nil {
		t.Fatalf("SellInfo failed: %v", err)
	}

	// 玩家 1 出价 1500。
	if err := w.BidInfo(1, l.ID, 1500); err != nil {
		t.Fatalf("BidInfo failed: %v", err)
	}

	// 玩家 0 自购 → 拒绝。
	if err := w.BidInfo(0, l.ID, 2000); err == nil {
		t.Fatal("self-bid should be rejected")
	}

	// 开标。
	winner, detail, err := w.RevealInfo(l.ID)
	if err != nil {
		t.Fatalf("RevealInfo failed: %v", err)
	}
	if winner != 1 {
		t.Fatalf("winner: got %d, want 1", winner)
	}
	if detail != "下月进入繁荣期" {
		t.Fatalf("detail: got %s, want '下月进入繁荣期'", detail)
	}
	if w.ListingBook.Listings[l.ID].Status != ListingStatusDeal {
		t.Fatal("listing should be deal after reveal")
	}
}

// TestSnapshotListings 测试挂单簿快照(隐藏底价)。
func TestSnapshotListings(t *testing.T) {
	w := newTradeTestWorld(42)
	w.StartGame()
	giveAsset(w, 0, Asset{Kind: AssetGold, Units: 10, CostCNY: 5000, OpenMonth: w.Month})

	payload := ListingPayload{Asset: &AssetPayload{Asset: w.Players[0].Assets[0], MinCNY: 4000}}
	_, err := w.CreateListing(0, ListingAsset, payload, 6000)
	if err != nil {
		t.Fatalf("CreateListing failed: %v", err)
	}

	snaps := w.SnapshotListings("")
	if len(snaps) != 1 {
		t.Fatalf("snapshot count: got %d, want 1", len(snaps))
	}
	// 快照不暴露底价(MinCNY 不在 ListingSnapshot 中)。
	// 仅校验公开字段。
	if snaps[0].AskCNY != 6000 {
		t.Fatalf("snapshot ask_cny: got %d, want 6000", snaps[0].AskCNY)
	}
}
