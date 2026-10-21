package virtual_city

// view_contract_batch70_test.go — game.state 契约补全的回归单测(2026-10-21 批次70)。
//
// 覆盖批次 70 的 5 项补全:
//  ① game.state.listing_book(挂单簿 / 议价 / P2P 借贷 / 拍卖)+ 私密字段过滤;
//  ② game.state.early_repay_eligible(提前还款资格);
//  ③ my.savings_deposit(声明却从不赋值 → 已接线);
//  ④ cycle.minsky_moment_count / ponzi_count / ponzi_ratio(与 minsky_overview 同源);
//  ⑤ bot_contexts[].last_senses[] 富形状(人/物/事/声/味)。

import (
	"encoding/json"
	"strings"
	"testing"
)

// b70State 用 viewer 视角构造 game.state(测试用便捷封装)。
func b70State(w *World, viewer int) *ClientGameState {
	return BuildClientState("room-b70", viewer, w,
		[MaxSeats]string{"u0", "u1"},
		[MaxSeats]string{"玩家0", "玩家1"},
		[MaxSeats]bool{false, false},
		[MaxSeats]string{"", ""},
		[MaxSeats]BotTranscript{},
		0, 0, 0,
		nil, nil,
	)
}

// b70JSON 序列化 game.state。
func b70JSON(t *testing.T, cs *ClientGameState) string {
	t.Helper()
	raw, err := json.Marshal(cs)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(raw)
}

// ── ① 挂单簿 ───────────────────────────────────────────────────────────

// TestGameState_ListingBook_AssetListing 资产挂单:字段齐备 + 底价仅卖家可见。
func TestGameState_ListingBook_AssetListing(t *testing.T) {
	w := newTradeTestWorld(70)
	w.StartGame()
	giveAsset(w, 0, Asset{Kind: AssetGold, Units: 10, CostCNY: 5000, OpenMonth: w.Month})
	payload := ListingPayload{Asset: &AssetPayload{Asset: w.Players[0].Assets[0], MinCNY: 4000}}
	l, err := w.CreateListing(0, ListingAsset, payload, 6000)
	if err != nil {
		t.Fatalf("CreateListing: %v", err)
	}

	// 他座视角:挂单可见,底价不可见。
	csOther := b70State(w, 1)
	if csOther.ListingBook == nil {
		t.Fatal("listing_book missing")
	}
	if len(csOther.ListingBook.Listings) != 1 {
		t.Fatalf("listings: got %d, want 1", len(csOther.ListingBook.Listings))
	}
	li := csOther.ListingBook.Listings[0]
	if li.ID != l.ID || li.Seat != 0 || li.Type != string(ListingAsset) {
		t.Errorf("listing head mismatch: %+v", li)
	}
	if li.AskCNY != 6000 || li.Status != ListingStatusOpen {
		t.Errorf("listing ask/status mismatch: %+v", li)
	}
	if li.Payload.Asset == nil {
		t.Fatal("payload.asset missing")
	}
	if li.Payload.Asset.Kind != AssetGold || li.Payload.Asset.Units != 10 {
		t.Errorf("asset payload mismatch: %+v", li.Payload.Asset)
	}
	if li.Payload.Asset.MinCNY != 0 {
		t.Errorf("底价泄漏给非卖家: min_cny=%d", li.Payload.Asset.MinCNY)
	}
	if strings.Contains(b70JSON(t, csOther), `"min_cny"`) {
		t.Error("非卖家帧内出现 min_cny 键(底价应保密)")
	}

	// 卖家视角:底价可见。
	csOwner := b70State(w, 0)
	if len(csOwner.ListingBook.Listings) != 1 {
		t.Fatalf("owner listings: got %d, want 1", len(csOwner.ListingBook.Listings))
	}
	if got := csOwner.ListingBook.Listings[0].Payload.Asset.MinCNY; got != 4000 {
		t.Errorf("卖家底价: got %d, want 4000", got)
	}
	if !strings.Contains(b70JSON(t, csOwner), `"min_cny":4000`) {
		t.Error("卖家帧内缺少 min_cny")
	}
}

// TestGameState_ListingBook_LoanAndNegotiate 借贷挂单 + 议价会话下发。
func TestGameState_ListingBook_LoanAndNegotiate(t *testing.T) {
	w := newTradeTestWorld(71)
	w.StartGame()
	l, err := w.CreateLoanListing(0, "lend", 100000, 0.01, 12, true)
	if err != nil {
		t.Fatalf("CreateLoanListing: %v", err)
	}
	// 议价会话(需另一座位发起)。
	if _, e := w.NegotiateStart(1, l.ID, 100000); e != nil {
		t.Fatalf("NegotiateStart: %v", e)
	}

	cs := b70State(w, 2)
	if cs.ListingBook == nil {
		t.Fatal("listing_book missing")
	}
	var loanListing *ListingJSON
	for i := range cs.ListingBook.Listings {
		if cs.ListingBook.Listings[i].ID == l.ID {
			loanListing = &cs.ListingBook.Listings[i]
		}
	}
	if loanListing == nil {
		t.Fatalf("借贷挂单未下发: %+v", cs.ListingBook.Listings)
	}
	lp := loanListing.Payload.Loan
	if lp == nil {
		t.Fatal("payload.loan missing")
	}
	if lp.Direction != "lend" || lp.PrincipalCNY != 100000 || lp.TermN != 12 || !lp.NeedGuarantee {
		t.Errorf("loan payload mismatch: %+v", lp)
	}
	if len(cs.ListingBook.Negotiates) != 1 {
		t.Fatalf("negotiates: got %d, want 1", len(cs.ListingBook.Negotiates))
	}
	ng := cs.ListingBook.Negotiates[0]
	if ng.ListingID != l.ID || ng.Status != NegotiateStatusActive {
		t.Errorf("negotiate mismatch: %+v", ng)
	}
	if len(ng.Turns) == 0 {
		t.Error("negotiate turns missing")
	}
}

// TestGameState_ListingBook_EmptyArraysNotNull 空房时四个数组必须是 [] 而非 null。
func TestGameState_ListingBook_EmptyArraysNotNull(t *testing.T) {
	w := newTradeTestWorld(72)
	w.StartGame()
	cs := b70State(w, 0)
	if cs.ListingBook == nil {
		t.Fatal("listing_book missing")
	}
	s := b70JSON(t, cs)
	for _, key := range []string{`"listings":[]`, `"negotiates":[]`, `"p2p_loans":[]`, `"auctions":[]`} {
		if !strings.Contains(s, key) {
			t.Errorf("game.state 缺 %s", key)
		}
	}
}

// TestGameState_ListingBook_AuctionReserveOwnerOnly 拍卖保留价仅卖家可见。
func TestGameState_ListingBook_AuctionReserveOwnerOnly(t *testing.T) {
	w := newTradeTestWorld(73)
	w.StartGame()
	giveAsset(w, 0, Asset{Kind: AssetGold, Units: 10, CostCNY: 5000, OpenMonth: w.Month})
	a, err := w.StartAuction(0, AuctionEnglish, w.Players[0].Assets[0], 10000)
	if err != nil {
		t.Fatalf("StartAuction: %v", err)
	}
	// 其他座位出价(公开竞价 → 出价记录可见)。
	if e := w.PlaceBid(1, a.ID, 12000); e != nil {
		t.Fatalf("PlaceBid: %v", e)
	}

	csOther := b70State(w, 2)
	if len(csOther.ListingBook.Auctions) != 1 {
		t.Fatalf("auctions: got %d, want 1", len(csOther.ListingBook.Auctions))
	}
	aj := csOther.ListingBook.Auctions[0]
	if aj.Kind != string(AuctionEnglish) || aj.Status != AuctionStatusActive {
		t.Errorf("auction kind/status mismatch: %+v", aj)
	}
	if aj.Seat != 0 || aj.StartCNY != 10000 || aj.CurrentCNY != 12000 || aj.CurrentSeat != 1 {
		t.Errorf("auction head mismatch: %+v", aj)
	}
	if len(aj.Bids) != 1 || aj.Bids[0].Seat != 1 || aj.Bids[0].AmountCNY != 12000 {
		t.Errorf("auction bids mismatch: %+v", aj.Bids)
	}
	if aj.ReserveCNY != 0 {
		t.Errorf("保留价泄漏给非卖家: reserve_cny=%d", aj.ReserveCNY)
	}

	csOwner := b70State(w, 0)
	if got := csOwner.ListingBook.Auctions[0].ReserveCNY; got != a.Item.Reserve {
		t.Errorf("卖家保留价: got %d, want %d", got, a.Item.Reserve)
	}
}

// TestGameState_ListingBook_SealedBidsHidden 密封拍卖开标前不下发出价金额。
func TestGameState_ListingBook_SealedBidsHidden(t *testing.T) {
	w := newTradeTestWorld(74)
	w.StartGame()
	giveAsset(w, 0, Asset{Kind: AssetGold, Units: 10, CostCNY: 5000, OpenMonth: w.Month})
	a, err := w.StartAuction(0, AuctionSealed, w.Players[0].Assets[0], 10000)
	if err != nil {
		t.Fatalf("StartAuction: %v", err)
	}
	if e := w.PlaceSealedBid(1, a.ID, 15000); e != nil {
		t.Fatalf("PlaceSealedBid: %v", e)
	}
	cs := b70State(w, 2)
	if len(cs.ListingBook.Auctions) != 1 {
		t.Fatalf("auctions: got %d, want 1", len(cs.ListingBook.Auctions))
	}
	if got := len(cs.ListingBook.Auctions[0].Bids); got != 0 {
		t.Errorf("密封拍卖进行中不得下发出价记录: got %d", got)
	}
	// 只查拍卖段(整帧含玩家现金等其它数字,不能全文匹配金额)。
	ajRaw, mErr := json.Marshal(cs.ListingBook.Auctions)
	if mErr != nil {
		t.Fatalf("marshal auctions: %v", mErr)
	}
	if strings.Contains(string(ajRaw), "15000") {
		t.Errorf("密封出价金额泄漏: %s", ajRaw)
	}
}

// ── ② 提前还款资格 ─────────────────────────────────────────────────────

// TestGameState_EarlyRepayEligible 资格判定:仅「余额 > 0 的房贷」为 true。
func TestGameState_EarlyRepayEligible(t *testing.T) {
	w := newTradeTestWorld(75)
	w.StartGame()

	// 无贷款 → false。
	if b70State(w, 0).EarlyRepayEligible {
		t.Error("无贷款时 early_repay_eligible 应为 false")
	}
	// 消费贷 → false(仅房贷可提前还款)。
	w.Players[0].Loans = append(w.Players[0].Loans,
		Loan{ID: "L1", Kind: LoanConsumer, Balance: 10000, TermN: 36, MonthsLeft: 30})
	if b70State(w, 0).EarlyRepayEligible {
		t.Error("仅消费贷时 early_repay_eligible 应为 false")
	}
	// 房贷(余额 > 0)→ true。
	w.Players[0].Loans = append(w.Players[0].Loans,
		Loan{ID: "L2", Kind: LoanMortgage, Balance: 1000000, TermN: 360, MonthsLeft: 300})
	if !b70State(w, 0).EarlyRepayEligible {
		t.Error("有房贷时 early_repay_eligible 应为 true")
	}
	// 房贷已结清(余额 0)→ false。
	w.Players[0].Loans[1].Balance = 0
	if b70State(w, 0).EarlyRepayEligible {
		t.Error("房贷已结清时 early_repay_eligible 应为 false")
	}
	// 观战者 → false。
	w.Players[0].Loans[1].Balance = 1000000
	if b70State(w, -1).EarlyRepayEligible {
		t.Error("观战者 early_repay_eligible 应为 false")
	}
	// 键必须总是下发(前端靠 false 判断入口显隐)。
	if !strings.Contains(b70JSON(t, b70State(w, 0)), `"early_repay_eligible"`) {
		t.Error("game.state 缺 early_repay_eligible 键")
	}
}

// ── ③ my.savings_deposit ──────────────────────────────────────────────

// TestGameState_MySavingsDeposit 本人定期存款必须如实下发(此前恒 0)。
func TestGameState_MySavingsDeposit(t *testing.T) {
	w := newTradeTestWorld(76)
	w.StartGame()
	w.Players[0].SavingsDeposit = 123456
	cs := b70State(w, 0)
	if cs.My == nil {
		t.Fatal("my missing")
	}
	if cs.My.SavingsDeposit != 123456 {
		t.Errorf("my.savings_deposit: got %d, want 123456", cs.My.SavingsDeposit)
	}
	if !strings.Contains(b70JSON(t, cs), `"savings_deposit":123456`) {
		t.Error("game.state 缺 my.savings_deposit 取值")
	}
	// 他人视角不受影响(观战者 my = null)。
	if b70State(w, -1).My != nil {
		t.Error("观战者 my 应为 nil")
	}
}

// ── ④ cycle 明斯基三字段 ──────────────────────────────────────────────

// TestGameState_CycleMinskyFields cycle 三字段与 minsky_overview 同源。
func TestGameState_CycleMinskyFields(t *testing.T) {
	w := newTradeTestWorld(77)
	w.StartGame()
	// 造一个庞氏等级玩家(主导等级取最差)。
	w.Players[0].MinskyByLoan = map[string]*MinskyStatus{
		"L1": {Tier: MinskyPonzi, DebtToIncome: 1.2},
	}
	w.MinskyMomentCount = 3

	cs := b70State(w, 0)
	if cs.Minsky.PonziCount != 1 {
		t.Fatalf("minsky_overview.ponzi_count: got %d, want 1", cs.Minsky.PonziCount)
	}
	if cs.Cycle.PonziCount != cs.Minsky.PonziCount {
		t.Errorf("cycle.ponzi_count: got %d, want %d", cs.Cycle.PonziCount, cs.Minsky.PonziCount)
	}
	if cs.Cycle.PonziRatio != cs.Minsky.PonziRatio {
		t.Errorf("cycle.ponzi_ratio: got %v, want %v", cs.Cycle.PonziRatio, cs.Minsky.PonziRatio)
	}
	if cs.Cycle.MinskyMomentCount != cs.Minsky.MomentCount {
		t.Errorf("cycle.minsky_moment_count: got %d, want %d", cs.Cycle.MinskyMomentCount, cs.Minsky.MomentCount)
	}
	if cs.Cycle.MinskyMomentCount != 3 {
		t.Errorf("cycle.minsky_moment_count: got %d, want 3", cs.Cycle.MinskyMomentCount)
	}
}

// ── ⑤ last_senses 富形状 ──────────────────────────────────────────────

// TestGameState_LastSensesRichShape 感知记录必须带富形状(人/物/事/声/味)。
func TestGameState_LastSensesRichShape(t *testing.T) {
	r := newRoomWithSeats(t, 12)
	r.ResidentCount = 200
	if e := r.Start(nil); e != nil {
		t.Fatalf("start: %v", e)
	}
	ar := beginDecisionFor(t, r, 0)
	defer ar.EndDecision(0, r.World.Month)

	if _, err := ar.See(0); err != nil {
		t.Fatalf("see: %v", err)
	}
	r.mu.Lock()
	senses := r.Transcripts[0].LastSenses
	r.mu.Unlock()
	if len(senses) == 0 {
		t.Fatal("last_senses not recorded")
	}
	s := senses[0]
	if s.Kind != "see" {
		t.Errorf("kind: got %q, want see", s.Kind)
	}
	if s.District == "" {
		t.Error("富形状缺 district")
	}
	if len(s.Things) == 0 {
		t.Error("富形状缺 things(地标/挂牌)")
	}
	// 序列化后前端可读到富字段键。
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal sense: %v", err)
	}
	str := string(raw)
	for _, key := range []string{`"district"`, `"things"`} {
		if !strings.Contains(str, key) {
			t.Errorf("last_senses JSON 缺 %s: %s", key, str)
		}
	}
	// hear 的发声摘录同样入形状。
	if err := ar.Speak(0, "今晚行情怎么看", ""); err != nil {
		t.Fatalf("speak: %v", err)
	}
	if _, err := ar.Hear(0); err != nil {
		t.Fatalf("hear: %v", err)
	}
	r.mu.Lock()
	senses = r.Transcripts[0].LastSenses
	r.mu.Unlock()
	if len(senses) == 0 || senses[0].Kind != "hear" {
		t.Fatalf("hear 未记录: %+v", senses)
	}
	if len(senses[0].Utterances) == 0 {
		t.Error("富形状缺 utterances(公开发言摘录)")
	}
}
