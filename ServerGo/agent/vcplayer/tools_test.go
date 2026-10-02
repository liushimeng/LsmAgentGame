package vcplayer

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"LsmAgentGame/errcode"
	"LsmAgentGame/game/virtual_city/profession"
)

// TestBuildTools_22Tools 批次35 §3.2:工具收敛 47 → 21,感知四件套与
// speak/check_state/submit_month 原样保留(V2 验收);批次52 §2 裁决 D13
// 增 ToolFamily 分组工具 → 合计 22。
func TestBuildTools_22Tools(t *testing.T) {
	tools := BuildTools()
	if len(tools) != 22 {
		t.Errorf("tools count: got %d, want 22", len(tools))
	}
	want := map[string]bool{
		// 感知与基础(7,原样保留)。
		ToolCheckState: false, ToolSpeak: false, ToolSubmitMonth: false,
		ToolSee: false, ToolHear: false, ToolSmell: false, ToolMove: false,
		// 金融合并(5)。
		ToolAssetTrade: false, ToolBankLoan: false, ToolSavings: false,
		ToolQueryFinance: false, ToolInsurance: false,
		// 代际财富(1,批次52 裁决 D13)。
		ToolFamily: false,
		// 生活(3)。
		ToolActivity: false, ToolSetConsumption: false, ToolAnswerSurvey: false,
		// 副业(1)。
		ToolSideBusiness: false,
		// 交易合并(5)。
		ToolMarketListing: false, ToolNegotiate: false, ToolP2PLending: false,
		ToolAuctionBid: false, ToolInfoMarket: false,
	}
	for _, t1 := range tools {
		if _, ok := want[t1.Name]; !ok {
			t.Errorf("unknown tool: %s", t1.Name)
		}
		if want[t1.Name] {
			t.Errorf("duplicate tool: %s", t1.Name)
		}
		want[t1.Name] = true
	}
	for name, ok := range want {
		if !ok {
			t.Errorf("missing tool: %s", name)
		}
	}
	// 旧工具名不得复活(§3.5)。
	for _, old := range []string{
		"buy_asset", "sell_asset", "buy_house", "take_loan", "repay_loan",
		"apply_loan_with_credit", "early_repay", "deposit_savings", "withdraw_savings",
		"query_central_bank", "query_banking_system", "query_minsky", "query_economy",
		"buy_insurance", "cancel_insurance", "get_insurance_status",
		"study", "socialize", "rest", "work_overtime", "consume", "donate",
		"start_side_business", "stop_side_business", "set_side_price",
		"list_asset", "cancel_listing", "view_listings",
		"start_negotiate", "respond_negotiate", "create_loan_listing", "accept_loan",
		"repay_p2p_loan", "add_guarantor", "bid_auction", "sell_info", "bid_info",
		"move_district",
	} {
		for _, t1 := range tools {
			if t1.Name == old {
				t.Errorf("legacy tool %q must be removed (批次35 §3.2)", old)
			}
		}
	}
}

// TestBuildTools_RequiredFields 检查全部工具的 wire 完整性(§14.1 协议偏差防呆)。
func TestBuildTools_RequiredFields(t *testing.T) {
	for _, tool := range BuildTools() {
		if tool.Name == "" {
			t.Errorf("tool missing name")
		}
		if tool.InputSchema == nil {
			t.Errorf("tool %s missing input_schema", tool.Name)
		}
		if tool.Description == "" {
			t.Errorf("tool %s missing description", tool.Name)
		}
	}
}

// TestToolNames_Returns22Names 工具名列表 = 22(测试/lint 口径;批次52 起)。
func TestToolNames_Returns22Names(t *testing.T) {
	names := ToolNames()
	if len(names) != 22 {
		t.Errorf("names count: got %d, want 22", len(names))
	}
	seen := map[string]bool{}
	for _, n := range names {
		if seen[n] {
			t.Errorf("duplicate name: %s", n)
		}
		seen[n] = true
	}
	// 必含感知四件套与月度循环骨架(用户硬性要求)。
	for _, must := range []string{"see", "hear", "smell", "move", "speak", "submit_month", "check_state"} {
		if !seen[must] {
			t.Errorf("ToolNames missing mandatory tool %q", must)
		}
	}
}

// TestFailOr_NilError 验证 failOr 对 nil error 返回成功文案(不 panic)。
// 2026-09-15 §财商流P0-bugfix:typed-nil *errcode.Error 装入 error 接口后
// != nil 但 .Error() panic,这里是核心防御。
func TestFailOr_NilError(t *testing.T) {
	res := failOr(nil, "买入成功", dispatchToolResult{})
	if res.IsErr {
		t.Errorf("nil error must not be error: %+v", res)
	}
	if res.Text != "买入成功" {
		t.Errorf("nil error must keep success text: got %q", res.Text)
	}
}

// TestFailOr_TypedNilError 验证 failOr 对 typed-nil *errcode.Error 返回成功文案。
func TestFailOr_TypedNilError(t *testing.T) {
	var typedNil *errcode.Error // == nil but type is *errcode.Error
	var err error = typedNil    // interface wrapping typed-nil → err != nil
	if err == nil {
		t.Fatal("typed-nil assignment must produce non-nil interface")
	}
	res := failOr(err, "买入成功", dispatchToolResult{})
	if res.IsErr {
		t.Errorf("typed-nil must not be error: %+v", res)
	}
	if res.Text != "买入成功" {
		t.Errorf("typed-nil must keep success text: got %q", res.Text)
	}
}

// TestFailOr_RealError 验证 failOr 对真实 error 返回失败文案。
func TestFailOr_RealError(t *testing.T) {
	res := failOr(errors.New("boom"), "买入成功", dispatchToolResult{})
	if !res.IsErr {
		t.Errorf("real error must set IsErr: %+v", res)
	}
	if !strings.Contains(res.Text, "boom") {
		t.Errorf("real error text must contain message: got %q", res.Text)
	}
}

// ── 批次35:预算语义(§3.3,V3 验收)──

// TestDispatchTool_BudgetSemantics dispatch 层按(工具,op)置位 Budget:
// 不耗预算的例外 = check_state / submit_month / see / hear / smell /
// savings 全部 / query_finance 全部 / bank_loan.probe / insurance.status /
// market_listing.view / answer_survey;其余全部置位。
func TestDispatchTool_BudgetSemantics(t *testing.T) {
	f := &fakeTradeRunner{}
	a := NewAgent("r1", "u1", "", "", 0, 3, 0)
	a.BindRunner(f)
	a.senseUsed = map[string]int{}
	cases := []struct {
		tool  string
		input map[string]any
		want  bool
	}{
		// 不耗预算(例外表)。
		{ToolCheckState, map[string]any{}, false},
		{ToolSubmitMonth, map[string]any{}, false},
		{ToolSee, map[string]any{}, false},
		{ToolHear, map[string]any{}, false},
		{ToolSmell, map[string]any{}, false},
		{ToolSavings, map[string]any{"op": "deposit", "amount_cny": 1000}, false},
		{ToolSavings, map[string]any{"op": "withdraw", "amount_cny": 1000}, false},
		{ToolQueryFinance, map[string]any{"scope": "central_bank"}, false},
		{ToolQueryFinance, map[string]any{"scope": "banking_system"}, false},
		{ToolQueryFinance, map[string]any{"scope": "minsky"}, false},
		{ToolQueryFinance, map[string]any{"scope": "economy"}, false},
		{ToolBankLoan, map[string]any{"op": "probe", "kind": "consumer", "amount_cny": 50000}, false},
		{ToolInsurance, map[string]any{"op": "status"}, false},
		{ToolMarketListing, map[string]any{"op": "view"}, false},
		{ToolAnswerSurvey, map[string]any{"survey_id": "SV1", "option_index": 1}, false},
		// 批次52 §2 裁决 D13:family query / plan_inheritance 免预算。
		{ToolFamily, map[string]any{"op": "query"}, false},
		{ToolFamily, map[string]any{"op": "plan_inheritance"}, false},
		// 耗预算(其余全部 op)。
		{ToolAssetTrade, map[string]any{"op": "buy", "asset": "stock_index", "amount_cny": 5000}, true},
		{ToolAssetTrade, map[string]any{"op": "sell", "asset": "gold", "units": 10}, true},
		{ToolBankLoan, map[string]any{"op": "take", "kind": "consumer", "amount_cny": 50000}, true},
		{ToolBankLoan, map[string]any{"op": "repay", "loan_id": "L1", "amount_cny": 10000}, true},
		{ToolBankLoan, map[string]any{"op": "early_repay", "loan_id": "L1", "amount_cny": 0}, true},
		{ToolInsurance, map[string]any{"op": "buy", "kind": "term_life"}, true},
		{ToolInsurance, map[string]any{"op": "cancel", "kind": "term_life"}, true},
		// 批次52 §2 裁决 D13:加赡养/教育升级耗 1 动作预算。
		{ToolFamily, map[string]any{"op": "pay_support_extra", "amount_cny": 1000}, true},
		{ToolFamily, map[string]any{"op": "upgrade_education", "child_idx": 0}, true},
		{ToolActivity, map[string]any{"kind": "study"}, true},
		{ToolActivity, map[string]any{"kind": "socialize"}, true},
		{ToolActivity, map[string]any{"kind": "rest"}, true},
		{ToolActivity, map[string]any{"kind": "work_overtime"}, true},
		{ToolActivity, map[string]any{"kind": "consume", "amount_cny": 200}, true},
		{ToolActivity, map[string]any{"kind": "donate", "amount_cny": 1000}, true},
		{ToolSideBusiness, map[string]any{"op": "start", "kind": "delivery"}, true},
		{ToolSideBusiness, map[string]any{"op": "stop"}, true},
		{ToolSideBusiness, map[string]any{"op": "set_price", "tier": 2}, true},
		{ToolSetConsumption, map[string]any{"level": 2}, true},
		{ToolMove, map[string]any{"mode": "walk"}, true},
		{ToolSpeak, map[string]any{"text": "大家好"}, true},
		{ToolMarketListing, map[string]any{"op": "list", "asset_index": 0, "ask_cny": 100, "min_cny": 90}, true},
		{ToolMarketListing, map[string]any{"op": "cancel", "listing_id": "LF1"}, true},
		{ToolNegotiate, map[string]any{"op": "start", "listing_id": "LF1", "offer_cny": 90}, true},
		{ToolNegotiate, map[string]any{"op": "respond", "neg_id": "N1", "action": "accept"}, true},
		{ToolP2PLending, map[string]any{"op": "create", "direction": "lend", "principal": 10000, "rate": 0.01, "term": 12}, true},
		{ToolP2PLending, map[string]any{"op": "accept", "listing_id": "LO1"}, true},
		{ToolP2PLending, map[string]any{"op": "repay", "loan_id": "P2P1", "amount_cny": 0}, true},
		{ToolP2PLending, map[string]any{"op": "guarantee", "loan_id": "P2P1"}, true},
		{ToolAuctionBid, map[string]any{"auction_id": "A1", "amount_cny": 100000}, true},
		{ToolInfoMarket, map[string]any{"op": "sell", "category": "market", "title": "t", "detail": "d", "min_bid": 100}, true},
		{ToolInfoMarket, map[string]any{"op": "bid", "listing_id": "LI1", "bid_cny": 200}, true},
	}
	for _, c := range cases {
		f.lastTool = ""
		res := a.DispatchTool(c.tool, c.input)
		if res.IsErr {
			t.Errorf("%s(%v): unexpected dispatch error: %s", c.tool, c.input["op"], res.Text)
			continue
		}
		if res.Budget != c.want {
			t.Errorf("%s(op=%v): Budget = %v, want %v", c.tool, c.input["op"], res.Budget, c.want)
		}
	}
}

// ── 批次35:op 派发映射(§3.5,V4 验收——每个 op 打到正确 ToolRunner 方法)──

// TestDispatchTool_AssetTrade asset_trade 三吸收工具的 op 路由:
// 金融买 / 房产买 / 金融卖 / 房产卖(district 拼回 house:<district>)。
func TestDispatchTool_AssetTrade(t *testing.T) {
	f := &fakeTradeRunner{}
	a := NewAgent("r1", "u1", "", "", 0, 3, 0)
	a.BindRunner(f)

	res := a.DispatchTool(ToolAssetTrade, map[string]any{"op": "buy", "asset": "stock_index", "amount_cny": 5000})
	if res.IsErr || f.lastTool != "BuyAsset" || f.buyAssetArgs.asset != "stock_index" || f.buyAssetArgs.amount != 5000 {
		t.Errorf("buy fin: res=%+v method=%s args=%+v", res, f.lastTool, f.buyAssetArgs)
	}
	res = a.DispatchTool(ToolAssetTrade, map[string]any{"op": "buy", "asset": "house", "district": "finance", "downpay_ratio": 0.4})
	if res.IsErr || f.lastTool != "BuyHouse" || f.houseArgs.district != "finance" || f.houseArgs.ratio != 0.4 || f.houseArgs.asset != "house" {
		t.Errorf("buy house: res=%+v method=%s args=%+v", res, f.lastTool, f.houseArgs)
	}
	res = a.DispatchTool(ToolAssetTrade, map[string]any{"op": "buy", "asset": "shop", "district": "tech", "downpay_ratio": 1.0})
	if res.IsErr || f.lastTool != "BuyHouse" || f.houseArgs.asset != "shop" {
		t.Errorf("buy shop: res=%+v method=%s args=%+v", res, f.lastTool, f.houseArgs)
	}
	res = a.DispatchTool(ToolAssetTrade, map[string]any{"op": "sell", "asset": "gold", "units": 3.5})
	if res.IsErr || f.lastTool != "SellAsset" || f.sellAssetArgs.asset != "gold" || f.sellAssetArgs.units != 3.5 {
		t.Errorf("sell fin: res=%+v method=%s args=%+v", res, f.lastTool, f.sellAssetArgs)
	}
	// 房产卖出:分离写法(asset=house + district)拼回引擎 kind。
	res = a.DispatchTool(ToolAssetTrade, map[string]any{"op": "sell", "asset": "house", "district": "finance", "units": 1})
	if res.IsErr || f.lastTool != "SellAsset" || f.sellAssetArgs.asset != "house:finance" {
		t.Errorf("sell house combined: res=%+v method=%s asset=%q", res, f.lastTool, f.sellAssetArgs.asset)
	}
	// 旧格式 "house:<district>" 直传透传。
	res = a.DispatchTool(ToolAssetTrade, map[string]any{"op": "sell", "asset": "shop:oldtown", "units": 1})
	if res.IsErr || f.sellAssetArgs.asset != "shop:oldtown" {
		t.Errorf("sell shop legacy kind: res=%+v asset=%q", res, f.sellAssetArgs.asset)
	}
}

// TestDispatchTool_BankLoan 四 op 路由:take/probe/repay/early_repay。
func TestDispatchTool_BankLoan(t *testing.T) {
	f := &fakeTradeRunner{}
	a := NewAgent("r1", "u1", "", "", 0, 3, 0)
	a.BindRunner(f)

	res := a.DispatchTool(ToolBankLoan, map[string]any{"op": "take", "kind": "credit", "amount_cny": 100000})
	if res.IsErr || f.lastTool != "TakeLoan" || f.takeLoanArgs.kind != "credit" || f.takeLoanArgs.amount != 100000 {
		t.Errorf("take: res=%+v method=%s args=%+v", res, f.lastTool, f.takeLoanArgs)
	}
	res = a.DispatchTool(ToolBankLoan, map[string]any{"op": "probe", "kind": "consumer", "amount_cny": 30000})
	if res.IsErr || f.lastTool != "ApplyLoanWithCredit" || f.probeArgs.kind != "consumer" {
		t.Errorf("probe: res=%+v method=%s args=%+v", res, f.lastTool, f.probeArgs)
	}
	res = a.DispatchTool(ToolBankLoan, map[string]any{"op": "repay", "loan_id": "L2", "amount_cny": 20000})
	if res.IsErr || f.lastTool != "RepayLoan" || f.repayArgs.loanID != "L2" || f.repayArgs.amount != 20000 {
		t.Errorf("repay: res=%+v method=%s args=%+v", res, f.lastTool, f.repayArgs)
	}
	res = a.DispatchTool(ToolBankLoan, map[string]any{"op": "early_repay", "loan_id": "L3", "amount_cny": 0})
	if res.IsErr || f.lastTool != "EarlyRepay" || f.earlyRepayArgs.loanID != "L3" {
		t.Errorf("early_repay: res=%+v method=%s args=%+v", res, f.lastTool, f.earlyRepayArgs)
	}
}

// TestDispatchTool_SavingsAndQuery savings 双向 + query_finance 四 scope。
func TestDispatchTool_SavingsAndQuery(t *testing.T) {
	f := &fakeTradeRunner{}
	a := NewAgent("r1", "u1", "", "", 0, 3, 0)
	a.BindRunner(f)

	if res := a.DispatchTool(ToolSavings, map[string]any{"op": "deposit", "amount_cny": 10000}); res.IsErr || f.lastTool != "DepositSavings" || f.depositAmount != 10000 {
		t.Errorf("deposit: res=%+v method=%s amount=%d", res, f.lastTool, f.depositAmount)
	}
	if res := a.DispatchTool(ToolSavings, map[string]any{"op": "withdraw", "amount_cny": 5000}); res.IsErr || f.lastTool != "WithdrawSavings" || f.withdrawAmount != 5000 {
		t.Errorf("withdraw: res=%+v method=%s amount=%d", res, f.lastTool, f.withdrawAmount)
	}
	scopes := map[string]string{
		"central_bank":   "QueryCentralBank",
		"banking_system": "QueryBankingSystem",
		"minsky":         "QueryMinsky",
		"economy":        "QueryEconomy",
	}
	for scope, method := range scopes {
		f.lastTool = ""
		if res := a.DispatchTool(ToolQueryFinance, map[string]any{"scope": scope}); res.IsErr || f.lastTool != method {
			t.Errorf("scope=%s: res=%+v method=%s want %s", scope, res, f.lastTool, method)
		}
	}
}

// TestDispatchTool_Insurance insurance 三 op 路由。
func TestDispatchTool_Insurance(t *testing.T) {
	f := &fakeTradeRunner{}
	a := NewAgent("r1", "u1", "", "", 0, 3, 0)
	a.BindRunner(f)

	if res := a.DispatchTool(ToolInsurance, map[string]any{"op": "buy", "kind": "medical_million"}); res.IsErr || f.lastTool != "BuyInsurance" || f.insuranceKind != "medical_million" {
		t.Errorf("buy: res=%+v method=%s kind=%s", res, f.lastTool, f.insuranceKind)
	}
	if res := a.DispatchTool(ToolInsurance, map[string]any{"op": "cancel", "kind": "accident"}); res.IsErr || f.lastTool != "CancelInsurance" || f.insuranceKind != "accident" {
		t.Errorf("cancel: res=%+v method=%s kind=%s", res, f.lastTool, f.insuranceKind)
	}
	if res := a.DispatchTool(ToolInsurance, map[string]any{"op": "status"}); res.IsErr || f.lastTool != "GetInsuranceStatus" {
		t.Errorf("status: res=%+v method=%s", res, f.lastTool)
	}
}

// TestDispatchTool_Family family 四 op 路由(批次52 §2 裁决 D13)。
func TestDispatchTool_Family(t *testing.T) {
	f := &fakeTradeRunner{}
	a := NewAgent("r1", "u1", "", "", 0, 3, 0)
	a.BindRunner(f)

	if res := a.DispatchTool(ToolFamily, map[string]any{"op": "query"}); res.IsErr || f.lastTool != "QueryFamily" {
		t.Errorf("query: res=%+v method=%s", res, f.lastTool)
	}
	if res := a.DispatchTool(ToolFamily, map[string]any{"op": "plan_inheritance"}); res.IsErr || f.lastTool != "PlanInheritance" {
		t.Errorf("plan_inheritance: res=%+v method=%s", res, f.lastTool)
	}
	if res := a.DispatchTool(ToolFamily, map[string]any{"op": "pay_support_extra", "amount_cny": 2000}); res.IsErr || f.lastTool != "PaySupportExtra" || f.familyArgs.amount != 2000 {
		t.Errorf("pay_support_extra: res=%+v method=%s amount=%d", res, f.lastTool, f.familyArgs.amount)
	}
	if res := a.DispatchTool(ToolFamily, map[string]any{"op": "upgrade_education", "child_idx": 1}); res.IsErr || f.lastTool != "UpgradeEducation" || f.familyArgs.childIdx != 1 {
		t.Errorf("upgrade_education: res=%+v method=%s idx=%d", res, f.lastTool, f.familyArgs.childIdx)
	}
	// 缺省 op → query(与 insurance 缺省 buy 同款容错)。
	f.lastTool = ""
	if res := a.DispatchTool(ToolFamily, map[string]any{}); res.IsErr || f.lastTool != "QueryFamily" {
		t.Errorf("default op: res=%+v method=%s", res, f.lastTool)
	}
}

// TestDispatchTool_Activity 六 kind 路由到六个原 ToolRunner 方法。
func TestDispatchTool_Activity(t *testing.T) {
	f := &fakeTradeRunner{}
	a := NewAgent("r1", "u1", "", "", 0, 3, 0)
	a.BindRunner(f)

	cases := []struct {
		kind   string
		method string
		input  map[string]any
	}{
		{"study", "Study", map[string]any{"kind": "study"}},
		{"socialize", "Socialize", map[string]any{"kind": "socialize"}},
		{"rest", "Rest", map[string]any{"kind": "rest"}},
		{"work_overtime", "WorkOvertime", map[string]any{"kind": "work_overtime"}},
		{"consume", "Consume", map[string]any{"kind": "consume", "amount_cny": 300, "reason": "请客"}},
		{"donate", "Donate", map[string]any{"kind": "donate", "amount_cny": 2000}},
	}
	for _, c := range cases {
		f.lastTool = ""
		res := a.DispatchTool(ToolActivity, c.input)
		if res.IsErr || f.lastTool != c.method {
			t.Errorf("kind=%s: res=%+v method=%s want %s", c.kind, res, f.lastTool, c.method)
		}
	}
	if f.consumeArgs.amount != 300 || f.consumeArgs.reason != "请客" {
		t.Errorf("consume args: %+v", f.consumeArgs)
	}
	if f.donateAmount != 2000 {
		t.Errorf("donate amount: %d", f.donateAmount)
	}
}

// TestDispatchTool_SideBusiness 三 op 路由(start 含 kind+tier 透传)。
func TestDispatchTool_SideBusiness(t *testing.T) {
	f := &fakeTradeRunner{}
	a := NewAgent("r1", "u1", "", "", 0, 3, 0)
	a.BindRunner(f)

	res := a.DispatchTool(ToolSideBusiness, map[string]any{"op": "start", "kind": "tutoring", "tier": 2})
	if res.IsErr || f.lastTool != "StartSideBusiness" || f.sideBizArgs.kind != "tutoring" || f.sideBizArgs.tier != 2 {
		t.Errorf("start: res=%+v method=%s args=%+v", res, f.lastTool, f.sideBizArgs)
	}
	if res := a.DispatchTool(ToolSideBusiness, map[string]any{"op": "stop"}); res.IsErr || f.lastTool != "StopSideBusiness" {
		t.Errorf("stop: res=%+v method=%s", res, f.lastTool)
	}
	res = a.DispatchTool(ToolSideBusiness, map[string]any{"op": "set_price", "tier": 1})
	if res.IsErr || f.lastTool != "SetSidePrice" || f.lastTier != 1 {
		t.Errorf("set_price: res=%+v method=%s tier=%d", res, f.lastTool, f.lastTier)
	}
}

// TestSideBusinessTool_Schema tier enum + required 声明齐全(§130 接线)。
func TestSideBusinessTool_Schema(t *testing.T) {
	for _, td := range BuildTools() {
		if td.Name != ToolSideBusiness {
			continue
		}
		schema, _ := td.InputSchema["properties"].(map[string]any)
		tier, _ := schema["tier"].(map[string]any)
		if tier == nil {
			t.Fatal("side_business missing tier property")
		}
		if enum, _ := tier["enum"].([]int); len(enum) != 3 {
			t.Errorf("tier enum: got %v", tier["enum"])
		}
		req, _ := td.InputSchema["required"].([]string)
		if len(req) != 1 || req[0] != "op" {
			t.Errorf("required: got %v, want [op]", req)
		}
		return
	}
	t.Fatal("side_business missing from BuildTools")
}

// TestDistrictHintDesc_Dynamic 城区描述串动态取自 profession.DistrictIDs()
// (批次20 B2-9:不写死数量;批次35:buy_house 收敛进 asset_trade)。
func TestDistrictHintDesc_Dynamic(t *testing.T) {
	ids := profession.DistrictIDs()
	n := len(ids)
	desc := districtIDsHintDesc()
	if !strings.Contains(desc, strconv.Itoa(n)) {
		t.Fatalf("desc must embed dynamic count %d: %q", n, desc)
	}
	if !strings.Contains(desc, ids[0]) {
		t.Errorf("desc must list ids: %q", desc)
	}
	if n > 8 && !strings.Contains(desc, "…等") {
		t.Errorf("n>8 must use …等 N 区 form: %q", desc)
	}
	// asset_trade 的 district schema 用动态串(无「8 区」陈旧字面)。
	for _, td := range BuildTools() {
		if td.Name != ToolAssetTrade {
			continue
		}
		props, _ := td.InputSchema["properties"].(map[string]any)
		d, _ := props["district"].(map[string]any)
		ds, _ := d["description"].(string)
		if strings.Contains(ds, "8 区") {
			t.Errorf("asset_trade district desc still hardcoded: %q", ds)
		}
		if !strings.Contains(ds, strconv.Itoa(n)) {
			t.Errorf("asset_trade district desc not dynamic: %q", ds)
		}
	}
}

// TestAssetTradeDescription_Microstructure 合并描述带走 T+1/价差/熔断规则
// (批次35 §3.2「规则文案一字不丢」)。
func TestAssetTradeDescription_Microstructure(t *testing.T) {
	for _, td := range BuildTools() {
		if td.Name != ToolAssetTrade {
			continue
		}
		for _, kw := range []string{"T+1", "熔断", "单边价", "首付", "LPR+0.5%", "增值税5%", "手续费0.5%"} {
			if !strings.Contains(td.Description, kw) {
				t.Errorf("asset_trade description missing %s: %q", kw, td.Description)
			}
		}
	}
}
