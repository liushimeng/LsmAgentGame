package wealthplayer

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"LsmAgentGame/errcode"
	"LsmAgentGame/game/wealth/profession"
	llmtypes "LsmAgentGame/llm/types"
)

// TestBuildTools_AllToolsPresent 47 工具齐备(原 42 + §CityHuman重构 感知行动 4 +
// 批次20 副业改价 1:P1-2 经济循环 3 + P1-4 商业保险 3 + P2 交易系统 12)。
func TestBuildTools_AllToolsPresent(t *testing.T) {
	tools := BuildTools()
	if len(tools) != 47 {
		t.Errorf("tools count: got %d, want 47", len(tools))
	}
	want := map[string]bool{
		ToolCheckState: false, ToolBuyAsset: false, ToolSellAsset: false,
		ToolBuyHouse: false, ToolTakeLoan: false, ToolRepayLoan: false,
		ToolStartSide: false, ToolStopSide: false, ToolSetSidePrice: false, ToolStudy: false,
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
		// §CityHuman重构(2026-09-22): 感知与行动工具。
		ToolSee: false, ToolHear: false, ToolSmell: false, ToolMove: false,
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

// TestToolNames_Returns39Names 工具名列表 = 47(原 42 + §CityHuman重构 4 + 批次20 改价 1)。
func TestToolNames_Returns39Names(t *testing.T) {
	names := ToolNames()
	if len(names) != 47 {
		t.Errorf("names count: got %d, want 47", len(names))
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
			!strings.HasPrefix(n, "set_side_price") &&
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
			!strings.HasPrefix(n, "bid_info") &&
			// §CityHuman重构: 感知与行动工具。
			!strings.HasPrefix(n, "see") &&
			!strings.HasPrefix(n, "hear") &&
			!strings.HasPrefix(n, "smell") &&
			n != "move" {
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
// ── 批次20:副业定价工具接线(§130:schema 常量 + dispatcher 分支 + 测试)──

// TestSetSidePriceTool_Wiring set_side_price 工具在 BuildTools/ToolNames 中、
// tier enum [0,1,2]、required=[tier];start_side_business 增可选 tier 参数。
func TestSetSidePriceTool_Wiring(t *testing.T) {
	tools := BuildTools()
	var ssp, ssb *llmtypes.ToolDef
	for i := range tools {
		switch tools[i].Name {
		case ToolSetSidePrice:
			ssp = &tools[i]
		case ToolStartSide:
			ssb = &tools[i]
		}
	}
	if ssp == nil {
		t.Fatal("set_side_price missing from BuildTools")
	}
	// 描述含份额公式与竞争语义(文档2 §4.1 ≤2 句)。
	if !strings.Contains(ssp.Description, "份额") || !strings.Contains(ssp.Description, "集体低价") {
		t.Errorf("set_side_price description: %q", ssp.Description)
	}
	schema, _ := ssp.InputSchema["properties"].(map[string]any)
	tier, _ := schema["tier"].(map[string]any)
	if tier == nil {
		t.Fatal("set_side_price missing tier property")
	}
	if enum, _ := tier["enum"].([]int); len(enum) != 3 {
		t.Errorf("tier enum: got %v", tier["enum"])
	}
	req, _ := ssp.InputSchema["required"].([]string)
	if len(req) != 1 || req[0] != "tier" {
		t.Errorf("required: got %v, want [tier]", req)
	}
	// ToolNames 同步(§130)。
	found := false
	for _, n := range ToolNames() {
		if n == ToolSetSidePrice {
			found = true
		}
	}
	if !found {
		t.Error("ToolNames missing set_side_price")
	}
	// start_side_business.tier 可选(required 仍只有 kind,旧客户端兼容)。
	if ssb == nil {
		t.Fatal("start_side_business missing")
	}
	sbSchema, _ := ssb.InputSchema["properties"].(map[string]any)
	if _, ok := sbSchema["tier"]; !ok {
		t.Error("start_side_business missing optional tier")
	}
	sbReq, _ := ssb.InputSchema["required"].([]string)
	for _, r := range sbReq {
		if r == "tier" {
			t.Error("start_side_business tier must be OPTIONAL")
		}
	}
}

// TestSetSidePrice_Dispatch dispatcher 路由 + tier 透传(含 0 中价显式)。
func TestSetSidePrice_Dispatch(t *testing.T) {
	f := &fakeTradeRunner{}
	a := NewAgent("r1", "u1", "", "", 0, 3, 0)
	a.BindRunner(f)
	for _, tier := range []float64{0, 1, 2} {
		res := a.DispatchTool(ToolSetSidePrice, map[string]any{"tier": tier})
		if res.IsErr {
			t.Fatalf("tier %v: %s", tier, res.Text)
		}
		if f.lastTool != ToolSetSidePrice || f.lastTier != int(tier) {
			t.Fatalf("dispatch: tool=%q tier=%d want set_side_price/%v", f.lastTool, f.lastTier, tier)
		}
	}
	// start_side_business 带 tier 透传。
	res := a.DispatchTool(ToolStartSide, map[string]any{"kind": "delivery", "tier": float64(2)})
	if res.IsErr || f.lastTier != 2 {
		t.Fatalf("start tier passthrough: %+v tier=%d", res, f.lastTier)
	}
}

// TestDistrictHintDesc_Dynamic 城区描述串动态取自 profession.DistrictIDs()
// (批次20 B2-9:不写死数量;BE-1 扩表时自动跟随)。
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
	// buy_house / move_district 两处 schema 均用动态串(无「8 区」陈旧字面)。
	for _, td := range BuildTools() {
		if td.Name != ToolBuyHouse && td.Name != ToolMoveDistrict {
			continue
		}
		props, _ := td.InputSchema["properties"].(map[string]any)
		d, _ := props["district"].(map[string]any)
		ds, _ := d["description"].(string)
		if strings.Contains(ds, "8 区") {
			t.Errorf("%s district desc still hardcoded: %q", td.Name, ds)
		}
		if !strings.Contains(ds, strconv.Itoa(n)) {
			t.Errorf("%s district desc not dynamic: %q", td.Name, ds)
		}
	}
}

// TestStockToolDescriptions_Microstructure buy/sell 工具描述补 T+1/价差/熔断
// 一句话(文档3 B4)。
func TestStockToolDescriptions_Microstructure(t *testing.T) {
	for _, td := range BuildTools() {
		if td.Name != ToolBuyAsset && td.Name != ToolSellAsset {
			continue
		}
		for _, kw := range []string{"T+1", "熔断", "单边价"} {
			if !strings.Contains(td.Description, kw) {
				t.Errorf("%s description missing %s: %q", td.Name, kw, td.Description)
			}
		}
	}
}
