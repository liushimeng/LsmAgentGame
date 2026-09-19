package wealthplayer

import (
	"errors"
	"strings"
	"testing"

	"LsmAgentGame/errcode"
)

// TestBuildTools_AllToolsPresent 42 工具齐备(P0 17 + P1 央行/银行 5 + 明斯基/提前还款 2 +
// P1-2 经济循环 3 + P1-4 商业保险 3 + P2 交易系统 12)。
func TestBuildTools_AllToolsPresent(t *testing.T) {
	tools := BuildTools()
	if len(tools) != 42 {
		t.Errorf("tools count: got %d, want 42", len(tools))
	}
	want := map[string]bool{
		ToolCheckState: false, ToolBuyAsset: false, ToolSellAsset: false,
		ToolBuyHouse: false, ToolTakeLoan: false, ToolRepayLoan: false,
		ToolStartSide: false, ToolStopSide: false, ToolStudy: false,
		ToolSocialize: false, ToolRest: false, ToolWorkOvertime: false,
		ToolMoveDistrict: false, ToolConsume: false, ToolDonate: false,
		ToolSpeak: false, ToolSubmitMonth: false,
		// P1 新增: 央行/银行工具。
		ToolQueryCentralBank: false, ToolQueryBankingSystem: false,
		ToolApplyLoanWithCredit: false, ToolDepositSavings: false, ToolWithdrawSavings: false,
		// P1 扩展: 明斯基 / 提前还款。
		ToolQueryMinsky: false, ToolEarlyRepay: false,
		// P1-2(§财商流P1-2 §7.1): 消费档位 / 社会调研 / 经济查询。
		ToolSetConsumption: false, ToolAnswerSurvey: false, ToolQueryEconomy: false,
		// P1-4(§财商流P1-4 §7): 商业保险工具。
		ToolBuyInsurance: false, ToolCancelInsurance: false, ToolGetInsuranceStatus: false,
		// P2 交易系统: 玩家间交易工具。
		ToolListAsset: false, ToolCancelListing: false, ToolViewListings: false,
		ToolNegotiateStart: false, ToolRespondNegotiate: false,
		ToolCreateLoanListing: false, ToolAcceptLoan: false,
		ToolRepayLoanP2P: false, ToolAddGuarantor: false,
		ToolBidAuction: false, ToolSellInfo: false, ToolBidInfo: false,
	}
	for _, t1 := range tools {
		if _, ok := want[t1.Name]; !ok {
			t.Errorf("unknown tool: %s", t1.Name)
		}
		want[t1.Name] = true
	}
	for name, ok := range want {
		if !ok {
			t.Errorf("missing tool: %s", name)
		}
	}
}

// TestBuildTools_RequiredFields 检查关键工具的必填字段(避免 §14.1 wire 协议偏差)。
func TestBuildTools_RequiredFields(t *testing.T) {
	tools := BuildTools()
	for _, tool := range tools {
		if tool.Name == "" {
			t.Errorf("tool missing name")
		}
		if tool.InputSchema == nil {
			t.Errorf("tool %s missing input_schema", tool.Name)
		}
	}
}

// TestToolNames_Returns39Names 工具名列表 = 42(P0 17 + P1 10 + P1-2 3 + P1-4 保险 3 + P2 交易 12 - 3重复)。
func TestToolNames_Returns39Names(t *testing.T) {
	names := ToolNames()
	if len(names) != 42 {
		t.Errorf("names count: got %d, want 42", len(names))
	}
	for _, n := range names {
		if !strings.HasPrefix(n, "check_state") &&
			!strings.HasPrefix(n, "buy_") &&
			!strings.HasPrefix(n, "sell_") &&
			!strings.HasPrefix(n, "start_") &&
			!strings.HasPrefix(n, "stop_") &&
			!strings.HasPrefix(n, "study") &&
			!strings.HasPrefix(n, "socialize") &&
			!strings.HasPrefix(n, "rest") &&
			!strings.HasPrefix(n, "work_") &&
			!strings.HasPrefix(n, "move_") &&
			!strings.HasPrefix(n, "consume") &&
			!strings.HasPrefix(n, "donate") &&
			!strings.HasPrefix(n, "take_") &&
			!strings.HasPrefix(n, "repay_") &&
			!strings.HasPrefix(n, "speak") &&
			!strings.HasPrefix(n, "submit_") &&
			!strings.HasPrefix(n, "query_") &&
			!strings.HasPrefix(n, "apply_loan_with_credit") &&
			!strings.HasPrefix(n, "deposit_") &&
			!strings.HasPrefix(n, "withdraw_") &&
			!strings.HasPrefix(n, "early_") &&
			!strings.HasPrefix(n, "set_consumption") &&
			!strings.HasPrefix(n, "answer_survey") &&
			// P1-4 商业保险工具。
			!strings.HasPrefix(n, "buy_insurance") &&
			!strings.HasPrefix(n, "cancel_insurance") &&
			!strings.HasPrefix(n, "get_insurance_status") &&
			// P2 交易系统工具。
			!strings.HasPrefix(n, "list_asset") &&
			!strings.HasPrefix(n, "cancel_listing") &&
			!strings.HasPrefix(n, "view_listings") &&
			!strings.HasPrefix(n, "negotiate_") &&
			!strings.HasPrefix(n, "respond_negotiate") &&
			!strings.HasPrefix(n, "start_negotiate") &&
			!strings.HasPrefix(n, "create_loan_listing") &&
			!strings.HasPrefix(n, "accept_loan") &&
			!strings.HasPrefix(n, "repay_loan") &&
			!strings.HasPrefix(n, "add_guarantor") &&
			!strings.HasPrefix(n, "bid_auction") &&
			!strings.HasPrefix(n, "sell_info") &&
			!strings.HasPrefix(n, "bid_info") {
			t.Errorf("unexpected name: %s", n)
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