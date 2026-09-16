// Package wealthplayer — tools_trade.go: P2 玩家间交易与财富流动系统
// (2026-09-16 §财商流P2)。
//
// 实现 12 个交易类 Agent 工具(挂牌/议价/借贷/拍卖/信息),供 LLM 月度决策
// 调用。工具经 ToolRunner 接口桥接到 game/wealth 引擎(in-process,不走 WS);
// 引擎侧实现为 listing.go / auction.go / trade_actions.go(独立交付)。
//
// 本文件只含 Agent 侧工具定义 + 派发 + 决策增强提示,不 import game/wealth
// (依赖反转,与 tools.go 同构)。
package wealthplayer

import (
	"encoding/json"
	"fmt"

	"LsmAgentGame/agent/wealthtypes"
	"LsmAgentGame/errcode"
	llmtypes "LsmAgentGame/llm/types"
)

// ── P2 交易工具名常量 ──

const (
	ToolListAsset         = "list_asset"
	ToolCancelListing     = "cancel_listing"
	ToolViewListings      = "view_listings"
	ToolNegotiateStart    = "start_negotiate"
	ToolRespondNegotiate  = "respond_negotiate"
	ToolCreateLoanListing = "create_loan_listing"
	ToolAcceptLoan        = "accept_loan"
	ToolRepayLoanP2P      = "repay_p2p_loan"
	ToolAddGuarantor      = "add_guarantor"
	ToolBidAuction        = "bid_auction"
	ToolSellInfo          = "sell_info"
	ToolBidInfo           = "bid_info"
)

// tradeToolNames 是 12 个交易工具名有序列表(测试/lint 用)。
var tradeToolNames = []string{
	ToolListAsset, ToolCancelListing, ToolViewListings,
	ToolNegotiateStart, ToolRespondNegotiate,
	ToolCreateLoanListing, ToolAcceptLoan, ToolRepayLoanP2P, ToolAddGuarantor,
	ToolBidAuction, ToolSellInfo, ToolBidInfo,
}

// TradeToolNames 返回 12 个交易工具名(测试/lint 用)。
func TradeToolNames() []string {
	out := make([]string, len(tradeToolNames))
	copy(out, tradeToolNames)
	return out
}

// isTradeBudgetAction 是否消耗月度动作预算(view_listings 不耗,其余均耗)。
func isTradeBudgetAction(name string) bool {
	return name != ToolViewListings
}

// ── 工具定义 ──

// TradeToolDefinitions 返回 12 个交易工具定义(Anthropic wire)。
// 每个工具的 InputSchema 是标准 JSON Schema object;required 字段在描述中
// 显式标注,避免 LLM 遗漏。
func TradeToolDefinitions() []llmtypes.ToolDef {
	return []llmtypes.ToolDef{
		{
			Name: ToolListAsset,
			Description: "挂牌出售自有资产(房产/商铺/副业/股票/债券/黄金),进入挂单簿公开可见。" +
				"设定 ask_cny(要价)与 min_cny(底价,保密;低于底价系统自动拒绝)。" +
				"每座位最多 3 笔 open 挂单;资产挂牌期间不可重复操作。消耗 1 次动作预算。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"asset_index": intSchema(0, "资产在 check_state 资产列表中的下标(0 起)"),
					"ask_cny":     intSchema(1, "要价(元)"),
					"min_cny":     intSchema(1, "底价(元,保密);低于底价自动流拍"),
				},
				"required": []string{"asset_index", "ask_cny", "min_cny"},
			},
		},
		{
			Name:        ToolCancelListing,
			Description: "取消自己的一笔 open 挂单(资产归还,不消耗预算外的手续费)。消耗 1 次动作预算。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"listing_id": strSchema("挂单 id,如 LF1(挂出)/LB1(收购)"),
				},
				"required": []string{"listing_id"},
			},
		},
		{
			Name:        ToolViewListings,
			Description: "查看当前挂单簿(open 状态的挂单):资产出售/收购/信息/借贷要约。不消耗动作预算。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"type_filter": strSchema("可选过滤:asset|buy|info|loan_ofr|loan_req(空=全部)"),
				},
				"required": []string{},
			},
		},
		{
			Name: ToolNegotiateStart,
			Description: "对一笔 open 挂单发起议价(响应方为挂单主)。" +
				"首轮报价 offer_cny 必须 ≥ 挂单 ask_cny × 50%。系统创建议价会话 N1,轮到对方响应。" +
				"消耗 1 次动作预算。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"listing_id": strSchema("目标挂单 id"),
					"offer_cny":  intSchema(1, "首轮报价(元)"),
				},
				"required": []string{"listing_id", "offer_cny"},
			},
		},
		{
			Name: ToolRespondNegotiate,
			Description: "响应议价会话(还价/接受/拒绝)。action=offer(还价)/accept(接受)/reject(拒绝)。" +
				"还价时 offer_cny 必填;accept/reject 时可不填。消耗 1 次动作预算。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"neg_id":    strSchema("议价会话 id,如 N1"),
					"action":    strSchema("offer|accept|reject"),
					"offer_cny": intSchema(0, "还价金额(元);action=offer 时必填"),
					"comment":   map[string]any{"type": "string", "maxLength": 60, "description": "议价留言(≤60字,可选)"},
				},
				"required": []string{"neg_id", "action"},
			},
		},
		{
			Name: ToolCreateLoanListing,
			Description: "创建借贷挂单(出借或借款)。" +
				"direction=lend(出借要约):本金从现金冻结,利率 ≥ 0.3%/月;" +
				"direction=borrow(借款请求):利率 ≤ 3.6%/月(法定上限)。" +
				"term 为期数(月);need_guarantee=true 可降低利率 0.3%/月。消耗 1 次动作预算。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"direction":       strSchema("lend(出借)|borrow(借款)"),
					"principal":       intSchema(1, "本金(元)"),
					"rate":            map[string]any{"type": "number", "minimum": 0.003, "maximum": 0.036, "description": "月利率(小数,0.3%-3.6%)"},
					"term":            intSchema(1, "期数(月)"),
					"need_guarantee":  map[string]any{"type": "boolean", "description": "是否需要担保"},
				},
				"required": []string{"direction", "principal", "rate", "term"},
			},
		},
		{
			Name: ToolAcceptLoan,
			Description: "接受一笔 open 借贷挂单(匹配成交):" +
				"若挂单为 lend(出借要约),你作为借方签合约;若为 borrow(借款请求),你作为贷方签合约。" +
				"系统生成 P2P 合约 P2P1,资金+利息计划生效。消耗 1 次动作预算。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"listing_id": strSchema("借贷挂单 id"),
				},
				"required": []string{"listing_id"},
			},
		},
		{
			Name: ToolRepayLoanP2P,
			Description: "偿还 P2P 借贷合约(部分或全额)。" +
				"amount_cny=0 或 ≥ 余额视为全额结清;部分还款 ≥ 1000 元。" +
				"月结时系统自动扣月供,现金不足则逾期(罚息 5%、信用 −50)。消耗 1 次动作预算。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"loan_id":    strSchema("P2P 合约 id,如 P2P1"),
					"amount_cny": intSchema(0, "还款金额(元);0=全额结清"),
				},
				"required": []string{"loan_id"},
			},
		},
		{
			Name: ToolAddGuarantor,
			Description: "为他人的一笔 P2P 借款提供担保(降低利率 0.3%/月)。" +
				"不可自担保/不可为同一笔借多次担保;担保后若借款人连续 3 月逾期,你须代偿。" +
				"消耗 1 次动作预算。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"loan_id": strSchema("P2P 合约 id"),
				},
				"required": []string{"loan_id"},
			},
		},
		{
			Name: ToolBidAuction,
			Description: "参与英式公开叫价拍卖(房产大宗交易)。" +
				"起拍价 = 评估价 × 80%;加价 ≥ 最小加价额(住宅 10000 / 商业 50000);" +
				"连续一轮无人加价时最高价者得;最高价 < 评估价 × 90% 挂牌者可拒绝(流拍)。" +
				"消耗 1 次动作预算。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"auction_id": strSchema("拍卖 id,如 A1"),
					"amount_cny": intSchema(1, "出价(元);必须 > 当前最高价"),
				},
				"required": []string{"auction_id", "amount_cny"},
			},
		},
		{
			Name: ToolSellInfo,
			Description: "出售信息(密封暗标):category=market(市场内幕)/intel(玩家情报)/personal(个人概况)。" +
				"信息以密封暗标形式竞购,60 秒内最高价者得;平局人脉高者得。" +
				"detail 为信息详情(成交后揭示给中标者)。消耗 1 次动作预算。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"category":  strSchema("market|intel|personal"),
					"title":     map[string]any{"type": "string", "maxLength": 40, "description": "信息标题(公开,≤40字)"},
					"detail":    map[string]any{"type": "string", "maxLength": 200, "description": "信息详情(密封,≤200字)"},
					"min_bid":   intSchema(1, "最低出价(元)"),
				},
				"required": []string{"category", "title", "detail", "min_bid"},
			},
		},
		{
			Name: ToolBidInfo,
			Description: "参与信息密封暗标竞购(暗标):同一信息可有多人出价,最高价者得。" +
				"出价必须 ≥ min_bid;不可自购自己出售的信息。消耗 1 次动作预算。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"listing_id": strSchema("信息出售挂单 id"),
					"bid_cny":    intSchema(1, "出价(元)"),
				},
				"required": []string{"listing_id", "bid_cny"},
			},
		},
	}
}

// ── 工具派发 ──

// DispatchTradeTool 派发单个交易工具到 ToolRunner。
// 入参 input 是 Anthropic tool_use 的 input 字段(RawMessage);返回人读结果
// 文本(供 tool_result 回喂 LLM)。
// 2026-09-16 §财商流P2:与 tools.go DispatchTool 同构,独立文件避免单文件
// 超 1800 行上限。
func DispatchTradeTool(runner ToolRunner, seat int, toolName string, input json.RawMessage) dispatchToolResult {
	res := dispatchToolResult{Name: toolName, Input: string(input)}

	// 通用解析辅助。
	parsed := map[string]any{}
	_ = json.Unmarshal(input, &parsed)
	getStr := func(k string) string { s, _ := parsed[k].(string); return s }
	getInt := func(k string) int64 {
		switch v := parsed[k].(type) {
		case float64:
			return int64(v)
		case int:
			return int64(v)
		case int64:
			return v
		}
		return 0
	}
	getFloat := func(k string) float64 {
		switch v := parsed[k].(type) {
		case float64:
			return v
		case int:
			return float64(v)
		}
		return 0
	}
	getBool := func(k string) bool { b, _ := parsed[k].(bool); return b }

	ok := func(text string) dispatchToolResult { res.Text = text; return res }
	fail := func(err error) dispatchToolResult {
		if err == nil {
			res.IsErr = true
			res.Text = "失败:未知错误(nil)"
			return res
		}
		if e, ok := err.(*errcode.Error); ok && e == nil {
			res.IsErr = true
			res.Text = "失败:未知错误(nil *Error)"
			return res
		}
		res.IsErr = true
		res.Text = "失败:" + err.Error()
		return res
	}
	failOr := func(err error, successText string) dispatchToolResult {
		if err == nil {
			res.Text = successText
			return res
		}
		if e, ok := err.(*errcode.Error); ok && e == nil {
			res.Text = successText
			return res
		}
		res.IsErr = true
		res.Text = "失败:" + err.Error()
		return res
	}

	if runner == nil {
		return fail(fmt.Errorf("tool runner not bound"))
	}

	switch toolName {
	case ToolListAsset:
		return failOr(runner.ListAsset(seat, int(getInt("asset_index")), getInt("ask_cny"), getInt("min_cny")),
			"挂牌成功")
	case ToolCancelListing:
		return failOr(runner.CancelListing(seat, getStr("listing_id")), "已取消挂单")
	case ToolViewListings:
		s, err := runner.ViewListings(seat, getStr("type_filter"))
		if err != nil {
			return fail(err)
		}
		return ok(s)
	case ToolNegotiateStart:
		return failOr(runner.StartNegotiate(seat, getStr("listing_id"), getInt("offer_cny")),
			"议价已发起")
	case ToolRespondNegotiate:
		return failOr(runner.RespondNegotiate(seat, getStr("neg_id"), getStr("action"), getInt("offer_cny"), getStr("comment")),
			"议价已响应")
	case ToolCreateLoanListing:
		return failOr(runner.CreateLoanListing(seat, getStr("direction"), getInt("principal"),
			getFloat("rate"), int(getInt("term")), getBool("need_guarantee")), "借贷挂单已创建")
	case ToolAcceptLoan:
		return failOr(runner.AcceptLoan(seat, getStr("listing_id")), "已接受借贷要约,合约生效")
	case ToolRepayLoanP2P:
		return failOr(runner.RepayP2PLoan(seat, getStr("loan_id"), getInt("amount_cny")), "还款成功")
	case ToolAddGuarantor:
		return failOr(runner.AddGuarantor(seat, getStr("loan_id")), "担保已生效")
	case ToolBidAuction:
		return failOr(runner.BidAuction(seat, getStr("auction_id"), getInt("amount_cny")), "出价成功")
	case ToolSellInfo:
		return failOr(runner.SellInfo(seat, getStr("category"), getStr("title"), getStr("detail"), getInt("min_bid")),
			"信息已挂牌(密封暗标)")
	case ToolBidInfo:
		return failOr(runner.BidInfo(seat, getStr("listing_id"), getInt("bid_cny")), "暗标已提交")
	default:
		res.IsErr = true
		res.Text = "未知交易工具: " + toolName
		return res
	}
}

// ── 决策增强提示 ──

// TradeStrategyHint 根据本人财务状态生成交易感知策略提示段,追加到
// UserPrompt 末尾(§10.2 Agent 决策增强)。
// 返回空字符串表示无交易信号(不追加)。
func TradeStrategyHint(ctx *wealthtypes.GameContext) string {
	if ctx == nil {
		return ""
	}
	me := ctx.Me
	if me.ActionBudget <= 0 {
		return ""
	}

	// 月支出估算(来自 Monthly.Expense 或 Card 基数)。
	monthlyExpense := me.Monthly.Expense
	if monthlyExpense <= 0 {
		monthlyExpense = 3000 // 兜底
	}

	var signals []string

	// 现金充裕(> 2×月支出):主动寻找低估资产/放贷吃息。
	if me.Cash > monthlyExpense*2 && me.Cash > 10000 {
		signals = append(signals, fmt.Sprintf(
			"现金充裕(¥%d > 2×月支出 ¥%d):可用 view_listings 寻找低估资产,或用 create_loan_listing(direction=lend) 放贷吃息(利率 0.3%%-3.6%%/月)。",
			me.Cash, monthlyExpense))
	}

	// 现金紧张(< 0.5×月支出):挂牌出售资产/发起借款请求。
	if me.Cash < monthlyExpense/2 && len(me.Assets) > 0 {
		signals = append(signals, fmt.Sprintf(
			"现金紧张(¥%d < 0.5×月支出 ¥%d):可用 list_asset 挂牌变现非核心资产,或用 create_loan_listing(direction=borrow) 发起借款请求。",
			me.Cash, monthlyExpense))
	}

	// 信息优势(认知 ≥ 5):出售情报获利。
	if me.Cognition >= 5 {
		signals = append(signals, fmt.Sprintf(
			"认知优势(%d/10):可用 sell_info 出售市场内幕/玩家情报,密封暗标获利。", me.Cognition))
	}

	// 人脉优势(≥ 5):撮合交易赚佣金/担保赚利差。
	if me.Network >= 5 {
		signals = append(signals, fmt.Sprintf(
			"人脉优势(%d/10):可用 add_guarantor 为优质借贷担保(降低利率 0.3%%/月,赚取利差);撮合买卖双方议价赚佣金。", me.Network))
	}

	if len(signals) == 0 {
		return ""
	}

	out := "■ 交易信号\n"
	for _, s := range signals {
		out += "- " + s + "\n"
	}
	return out
}
