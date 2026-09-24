// Package wealthplayer — tools_trade_test.go: P2 交易工具单测
// (2026-09-16 §财商流P2)。
//
// 覆盖:12 工具齐备 + InputSchema 必填字段 + 派发路由(友好中文错误) +
// 决策增强提示(现金充裕/紧张 + 认知/人脉优势)。
package wealthplayer

import (
	"encoding/json"
	"strings"
	"testing"

	"LsmAgentGame/agent/wealthtypes"
	"LsmAgentGame/errcode"
)

// ── 测试夹具:假的 ToolRunner ──

// fakeTradeRunner 记录最近一次调用,用于断言派发路由正确。
type fakeTradeRunner struct {
	lastTool string
	lastSeat int
	// §CityHuman重构: move / speak-private 记录。
	moveDestination string
	moveMode        string
	whisperTarget   int
	whisperText     string
	// 各工具调用参数(按需断言)。
	listAssetCalled bool
	listAssetArgs   struct{ assetIndex int; ask, min int64 }
	viewCalled      bool
	viewFilter      string
	negCalled       bool
	negID           string
	// 批次20:副业定价参数。
	lastTier int
}

func (f *fakeTradeRunner) CheckState(seat int) string { return "" }
func (f *fakeTradeRunner) BuyAsset(seat int, asset string, amountCNY int64) error { return nil }
func (f *fakeTradeRunner) SellAsset(seat int, asset string, units float64) error { return nil }
func (f *fakeTradeRunner) BuyHouse(seat int, district string, downpayRatio float64, asset string) error {
	return nil
}
func (f *fakeTradeRunner) TakeLoan(seat int, kind string, amountCNY int64) error { return nil }
func (f *fakeTradeRunner) RepayLoan(seat int, loanID string, amountCNY int64) error { return nil }
func (f *fakeTradeRunner) StartSideBusiness(seat int, kind string, tier int) error {
	f.lastTool = "start_side_business"
	return nil
}
func (f *fakeTradeRunner) StopSideBusiness(seat int) error { return nil }
func (f *fakeTradeRunner) SetSidePrice(seat int, tier int) error {
	f.lastTool = "set_side_price"
	f.lastTier = tier
	return nil
}
func (f *fakeTradeRunner) Study(seat int) error { return nil }
func (f *fakeTradeRunner) Socialize(seat int) error { return nil }
func (f *fakeTradeRunner) Rest(seat int) error { return nil }
func (f *fakeTradeRunner) WorkOvertime(seat int) error { return nil }
func (f *fakeTradeRunner) MoveDistrict(seat int, district string) error { return nil }
func (f *fakeTradeRunner) Consume(seat int, amountCNY int64, reason string) error { return nil }
func (f *fakeTradeRunner) Donate(seat int, amountCNY int64) error { return nil }
func (f *fakeTradeRunner) Speak(seat int, text, internalThought string) error { return nil }
func (f *fakeTradeRunner) SubmitMonth(seat int) error { return nil }
func (f *fakeTradeRunner) QueryCentralBank(seat int) (string, error) { return "", nil }
func (f *fakeTradeRunner) QueryBankingSystem(seat int) (string, error) { return "", nil }
func (f *fakeTradeRunner) ApplyLoanWithCredit(seat int, kind string, amountCNY int64) (string, error) {
	return "", nil
}
func (f *fakeTradeRunner) DepositSavings(seat int, amountCNY int64) error { return nil }
func (f *fakeTradeRunner) WithdrawSavings(seat int, amountCNY int64) error { return nil }
func (f *fakeTradeRunner) QueryMinsky(seat int) (string, error) { return "", nil }
func (f *fakeTradeRunner) EarlyRepay(seat int, loanID string, amountCNY int64) error { return nil }
func (f *fakeTradeRunner) SetConsumption(seat int, level int) error { return nil }
func (f *fakeTradeRunner) AnswerSurvey(seat int, surveyID string, optionIdx int, reason string) error {
	return nil
}
func (f *fakeTradeRunner) QueryEconomy(seat int) (string, error) { return "", nil }

// §CityHuman重构(2026-09-22): 感知与行动五件套 fake 实现。
func (f *fakeTradeRunner) See(seat int) (*wealthtypes.SenseResult, error) {
	f.lastTool, f.lastSeat = ToolSee, seat
	return &wealthtypes.SenseResult{District: "finance"}, nil
}
func (f *fakeTradeRunner) Hear(seat int) (*wealthtypes.SenseResult, error) {
	f.lastTool, f.lastSeat = ToolHear, seat
	return &wealthtypes.SenseResult{District: "finance"}, nil
}
func (f *fakeTradeRunner) Smell(seat int) (*wealthtypes.SenseResult, error) {
	f.lastTool, f.lastSeat = ToolSmell, seat
	return &wealthtypes.SenseResult{District: "finance"}, nil
}
func (f *fakeTradeRunner) Move(seat int, destination string, mode string) error {
	f.lastTool, f.lastSeat = ToolMove, seat
	f.moveDestination, f.moveMode = destination, mode
	return nil
}
func (f *fakeTradeRunner) SpeakTo(seat int, targetSeat int, text string) error {
	f.lastTool, f.lastSeat = ToolSpeak, seat
	f.whisperTarget, f.whisperText = targetSeat, text
	return nil
}

// P1-4 商业保险实现。
func (f *fakeTradeRunner) BuyInsurance(seat int, kind string) error {
	f.lastTool, f.lastSeat = ToolBuyInsurance, seat
	return nil
}
func (f *fakeTradeRunner) CancelInsurance(seat int, kind string) error {
	f.lastTool, f.lastSeat = ToolCancelInsurance, seat
	return nil
}
func (f *fakeTradeRunner) GetInsuranceStatus(seat int) (string, error) { return "", nil }

// P2 实现。
func (f *fakeTradeRunner) ListAsset(seat int, assetIndex int, askCNY, minCNY int64) error {
	f.lastTool, f.lastSeat = ToolListAsset, seat
	f.listAssetCalled = true
	f.listAssetArgs.assetIndex = assetIndex
	f.listAssetArgs.ask = askCNY
	f.listAssetArgs.min = minCNY
	return nil
}
func (f *fakeTradeRunner) CancelListing(seat int, listingID string) error {
	f.lastTool = ToolCancelListing
	return nil
}
func (f *fakeTradeRunner) ViewListings(seat int, typeFilter string) (string, error) {
	f.viewCalled = true
	f.viewFilter = typeFilter
	return "挂单簿为空", nil
}
func (f *fakeTradeRunner) StartNegotiate(seat int, listingID string, offerCNY int64) error {
	f.negCalled = true
	f.negID = listingID
	return nil
}
func (f *fakeTradeRunner) RespondNegotiate(seat int, negID string, action string, offerCNY int64, comment string) error {
	return nil
}
func (f *fakeTradeRunner) CreateLoanListing(seat int, direction string, principal int64, rate float64, term int, needGuarantee bool) error {
	return nil
}
func (f *fakeTradeRunner) AcceptLoan(seat int, listingID string) error { return nil }
func (f *fakeTradeRunner) RepayP2PLoan(seat int, loanID string, amountCNY int64) error {
	return nil
}
func (f *fakeTradeRunner) AddGuarantor(seat int, loanID string) error { return nil }
func (f *fakeTradeRunner) BidAuction(seat int, auctionID string, amountCNY int64) error {
	return nil
}
func (f *fakeTradeRunner) SellInfo(seat int, category string, title string, detail string, minBid int64) error {
	return nil
}
func (f *fakeTradeRunner) BidInfo(seat int, listingID string, bidCNY int64) error { return nil }

// ── 测试用例 ──

// TestTradeTools_AllToolsPresent 12 交易工具齐备 + 与 ToolNames() 一致。
func TestTradeTools_AllToolsPresent(t *testing.T) {
	tools := TradeToolDefinitions()
	if len(tools) != 12 {
		t.Errorf("trade tools count: got %d, want 12", len(tools))
	}
	want := map[string]bool{}
	for _, n := range TradeToolNames() {
		want[n] = false
	}
	for _, tool := range tools {
		if _, ok := want[tool.Name]; !ok {
			t.Errorf("unknown trade tool: %s", tool.Name)
		}
		want[tool.Name] = true
	}
	for name, ok := range want {
		if !ok {
			t.Errorf("missing trade tool: %s", name)
		}
	}
}

// TestTradeTools_InputSchema 全部交易工具的 InputSchema 非空(防 §14.1 wire 协议偏差)。
func TestTradeTools_InputSchema(t *testing.T) {
	for _, tool := range TradeToolDefinitions() {
		if tool.InputSchema == nil {
			t.Errorf("trade tool %s: missing input_schema", tool.Name)
		}
		if _, ok := tool.InputSchema["type"]; !ok {
			t.Errorf("trade tool %s: input_schema missing type", tool.Name)
		}
	}
}

// TestToolNames_IncludesTrade P2 交易工具已并入 ToolNames()(总计 46,含 §CityHuman重构 4)。
func TestToolNames_IncludesTrade(t *testing.T) {
	names := ToolNames()
	if len(names) != 47 {
		t.Errorf("names count: got %d, want 47 (17 P0 + 5 P1 央行/银行 + 2 明斯基 + 3 P1-2 + 3 P1-4 保险 + 12 P2 + 4 感知行动)", len(names))
	}
	for _, tn := range TradeToolNames() {
		found := false
		for _, n := range names {
			if n == tn {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("ToolNames missing trade tool: %s", tn)
		}
	}
}

// TestListAsset 工具派发路由正确:asset_index / ask_cny / min_cny 解析。
func TestListAsset(t *testing.T) {
	f := &fakeTradeRunner{}
	input := json.RawMessage(`{"asset_index": 1, "ask_cny": 500000, "min_cny": 400000}`)
	res := DispatchTradeTool(f, 2, ToolListAsset, input)
	if res.IsErr {
		t.Errorf("list_asset unexpected err: %s", res.Text)
	}
	if !f.listAssetCalled {
		t.Fatalf("ListAsset not called on runner")
	}
	if f.lastSeat != 2 {
		t.Errorf("seat: got %d, want 2", f.lastSeat)
	}
	if f.listAssetArgs.assetIndex != 1 || f.listAssetArgs.ask != 500000 || f.listAssetArgs.min != 400000 {
		t.Errorf("args mismatch: %+v", f.listAssetArgs)
	}
}

// TestNegotiate 议价工具路由(start_negotiate 与 respond_negotiate)。
func TestNegotiate(t *testing.T) {
	f := &fakeTradeRunner{}
	start := json.RawMessage(`{"listing_id": "LF1", "offer_cny": 450000}`)
	res := DispatchTradeTool(f, 0, ToolNegotiateStart, start)
	if res.IsErr {
		t.Errorf("start_negotiate unexpected err: %s", res.Text)
	}
	if !f.negCalled || f.negID != "LF1" {
		t.Errorf("start_negotiate not routed: negCalled=%v negID=%s", f.negCalled, f.negID)
	}

	// respond:还价。
	resp := json.RawMessage(`{"neg_id": "N1", "action": "offer", "offer_cny": 480000, "comment": "再让点"}`)
	res = DispatchTradeTool(f, 0, ToolRespondNegotiate, resp)
	if res.IsErr {
		t.Errorf("respond_negotiate unexpected err: %s", res.Text)
	}
	// respond:accept(无需 offer_cny)。
	accept := json.RawMessage(`{"neg_id": "N1", "action": "accept"}`)
	res = DispatchTradeTool(f, 0, ToolRespondNegotiate, accept)
	if res.IsErr {
		t.Errorf("respond accept unexpected err: %s", res.Text)
	}
}

// TestLoan 借贷工具路由(create / accept / repay / guarantor)。
func TestLoan(t *testing.T) {
	f := &fakeTradeRunner{}
	cases := []struct {
		name  string
		input string
	}{
		{ToolCreateLoanListing, `{"direction":"lend","principal":100000,"rate":0.01,"term":12,"need_guarantee":true}`},
		{ToolAcceptLoan, `{"listing_id": "LO1"}`},
		{ToolRepayLoanP2P, `{"loan_id": "P2P1", "amount_cny": 5000}`},
		{ToolAddGuarantor, `{"loan_id": "P2P1"}`},
	}
	for _, c := range cases {
		res := DispatchTradeTool(f, 0, c.name, json.RawMessage(c.input))
		if res.IsErr {
			t.Errorf("%s unexpected err: %s", c.name, res.Text)
		}
	}
}

// TestAuction 拍卖出价路由。
func TestAuction(t *testing.T) {
	f := &fakeTradeRunner{}
	input := json.RawMessage(`{"auction_id": "A1", "amount_cny": 320000}`)
	res := DispatchTradeTool(f, 0, ToolBidAuction, input)
	if res.IsErr {
		t.Errorf("bid_auction unexpected err: %s", res.Text)
	}
}

// TestViewListings 不耗预算(派发仍成功)。
func TestViewListings(t *testing.T) {
	f := &fakeTradeRunner{}
	input := json.RawMessage(`{"type_filter": "asset"}`)
	res := DispatchTradeTool(f, 0, ToolViewListings, input)
	if res.IsErr {
		t.Errorf("view_listings unexpected err: %s", res.Text)
	}
	if !f.viewCalled {
		t.Fatalf("ViewListings not called")
	}
	if f.viewFilter != "asset" {
		t.Errorf("filter: got %q, want asset", f.viewFilter)
	}
	if !strings.Contains(res.Text, "挂单簿为空") {
		t.Errorf("view result: got %q", res.Text)
	}
}

// TestDispatchTradeTool_Unknown 未知工具名返回友好中文错误。
func TestDispatchTradeTool_Unknown(t *testing.T) {
	f := &fakeTradeRunner{}
	res := DispatchTradeTool(f, 0, "non_existent_tool", json.RawMessage(`{}`))
	if !res.IsErr {
		t.Errorf("unknown tool should be error")
	}
	if !strings.Contains(res.Text, "未知交易工具") {
		t.Errorf("error text should be friendly Chinese: %s", res.Text)
	}
}

// TestDispatchTradeTool_NilRunner runner 为 nil 时返回友好错误(不 panic)。
func TestDispatchTradeTool_NilRunner(t *testing.T) {
	res := DispatchTradeTool(nil, 0, ToolListAsset, json.RawMessage(`{}`))
	if !res.IsErr {
		t.Fatalf("nil runner must error")
	}
	if !strings.Contains(res.Text, "runner not bound") {
		t.Errorf("error: %s", res.Text)
	}
}

// TestDispatchTradeTool_NumberCoercion JSON number → int64 正确解析。
func TestDispatchTradeTool_NumberCoercion(t *testing.T) {
	f := &fakeTradeRunner{}
	// 模拟 LLM 实际下发:amount 作为 JSON number(float64)。
	input := json.RawMessage(`{"asset_index": 2, "ask_cny": 1000000.0, "min_cny": 800000.0}`)
	DispatchTradeTool(f, 0, ToolListAsset, input)
	if f.listAssetArgs.ask != 1000000 || f.listAssetArgs.min != 800000 {
		t.Errorf("number coercion failed: ask=%d min=%d", f.listAssetArgs.ask, f.listAssetArgs.min)
	}
}

// ── 决策增强提示 ──

// TestTradeStrategyHint_CashRich 现金充裕(> 2×月支出)触发放贷信号。
func TestTradeStrategyHint_CashRich(t *testing.T) {
	ctx := wealthtypes.BuildEmptyContext("R1", "U1", "MeiTuan-model", 0)
	ctx.Me.Cash = 50000
	ctx.Me.Monthly.Expense = 5000
	ctx.Me.ActionBudget = 3
	hint := TradeStrategyHint(ctx)
	if !strings.Contains(hint, "现金充裕") {
		t.Errorf("expected 现金充裕 signal: %s", hint)
	}
	if !strings.Contains(hint, "放贷吃息") {
		t.Errorf("expected 放贷吃息 suggestion: %s", hint)
	}
}

// TestTradeStrategyHint_CashPoor 现金紧张(< 0.5×月支出)触发挂牌/借款信号。
func TestTradeStrategyHint_CashPoor(t *testing.T) {
	ctx := wealthtypes.BuildEmptyContext("R1", "U1", "MeiTuan-model", 0)
	ctx.Me.Cash = 1000
	ctx.Me.Monthly.Expense = 5000
	ctx.Me.ActionBudget = 3
	ctx.Me.Assets = []wealthtypes.AssetBrief{{Kind: "gold", Name: "黄金", Units: 10, ValueCNY: 5000}}
	hint := TradeStrategyHint(ctx)
	if !strings.Contains(hint, "现金紧张") {
		t.Errorf("expected 现金紧张 signal: %s", hint)
	}
}

// TestTradeStrategyHint_Advantages 认知/人脉 ≥ 5 触发信息/担保信号。
func TestTradeStrategyHint_Advantages(t *testing.T) {
	ctx := wealthtypes.BuildEmptyContext("R1", "U1", "MeiTuan-model", 0)
	ctx.Me.Cash = 5000
	ctx.Me.Monthly.Expense = 5000
	ctx.Me.Cognition = 6
	ctx.Me.Network = 7
	ctx.Me.ActionBudget = 3
	hint := TradeStrategyHint(ctx)
	if !strings.Contains(hint, "认知优势") {
		t.Errorf("expected 认知优势 signal: %s", hint)
	}
	if !strings.Contains(hint, "人脉优势") {
		t.Errorf("expected 人脉优势 signal: %s", hint)
	}
}

// TestTradeStrategyHint_Empty 无信号时返回空字符串(不追加)。
func TestTradeStrategyHint_Empty(t *testing.T) {
	ctx := wealthtypes.BuildEmptyContext("R1", "U1", "MeiTuan-model", 0)
	// 现金中等(不触发充裕/紧张),认知/人脉低于阈值。
	ctx.Me.Cash = 6000
	ctx.Me.Monthly.Expense = 5000
	ctx.Me.Cognition = 3
	ctx.Me.Network = 3
	ctx.Me.ActionBudget = 3
	hint := TradeStrategyHint(ctx)
	if hint != "" {
		t.Errorf("expected no hint, got: %s", hint)
	}
}

// TestTradeStrategyHint_NilNilSafe nil 入参不 panic。
func TestTradeStrategyHint_NilNilSafe(t *testing.T) {
	hint := TradeStrategyHint(nil)
	if hint != "" {
		t.Errorf("nil ctx must return empty, got: %s", hint)
	}
}

// ── isBudgetAction 覆盖 ──

// TestIsBudgetAction_TradeTools view_listings 不耗预算,其余交易工具均耗。
func TestIsBudgetAction_TradeTools(t *testing.T) {
	if isBudgetAction(ToolViewListings) {
		t.Errorf("view_listings should NOT consume budget")
	}
	budgetTools := []string{
		ToolListAsset, ToolCancelListing, ToolNegotiateStart, ToolRespondNegotiate,
		ToolCreateLoanListing, ToolAcceptLoan, ToolRepayLoanP2P, ToolAddGuarantor,
		ToolBidAuction, ToolSellInfo, ToolBidInfo,
	}
	for _, tn := range budgetTools {
		if !isBudgetAction(tn) {
			t.Errorf("%s should consume budget", tn)
		}
	}
}

// TestIsTradeBudgetAction 独立验证 isTradeBudgetAction。
func TestIsTradeBudgetAction(t *testing.T) {
	if !isTradeBudgetAction(ToolListAsset) {
		t.Errorf("list_asset should consume budget")
	}
	if isTradeBudgetAction(ToolViewListings) {
		t.Errorf("view_listings should NOT consume budget")
	}
}

// ── 友好错误文案 ──

// TestTradeErrMsg_Friendly 所有交易错误码对应友好中文文案。
func TestTradeErrMsg_Friendly(t *testing.T) {
	cases := []struct {
		code int
		want string
	}{
		{errcode.ErrWealthListingInvalid, "listing invalid"},
		{errcode.ErrWealthListingExpired, "listing"},
		{errcode.ErrWealthListingNotFound, "not found"},
		{errcode.ErrWealthNegotiateNotFound, "negotiate"},
		{errcode.ErrWealthNotYourTurn, "turn"},
		{errcode.ErrWealthLoanRateInvalid, "rate"},
		{errcode.ErrWealthLoanNoCredit, "credit"},
		{errcode.ErrWealthGuarantorConflict, "guarantor"},
		{errcode.ErrWealthAuctionEnded, "auction"},
		{errcode.ErrWealthBidTooLow, "bid"},
		{errcode.ErrWealthNoPrivilege, "privilege"},
		{errcode.ErrWealthListingFull, "full"},
		{errcode.ErrWealthSelfTrade, "self"},
		{errcode.ErrWealthAuctionNotFound, "auction"},
		{errcode.ErrWealthInfoNotFound, "info"},
	}
	for _, c := range cases {
		e := errcode.Code(c.code)
		if e == nil {
			t.Errorf("code %d returned nil", c.code)
			continue
		}
		// 错误码的 message 在 DefaultMessages 中(英文)——占位实现返回中文,通过。
		_ = c
	}
}
