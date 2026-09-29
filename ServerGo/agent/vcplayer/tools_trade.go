// Package vcplayer — tools_trade.go: P2 玩家间交易与财富流动系统
// (2026-09-16 §财商流P2;批次35 §3.2 工具收敛重组)。
//
// 批次35(2026-09-29):旧 12 个交易工具(挂牌/议价/借贷/担保/拍卖/信息)
// 合并为 5 个(market_listing / negotiate / p2p_lending / auction_bid /
// info_market),经 op 参数派发回原 ToolRunner 接口方法 —— 引擎侧
// (listing.go / auction.go / trade_actions.go)零改动。被吸收工具的全部
// 规则文案(限额/利率上下限/罚息/流拍/暗标规则)逐字保留在 Description。
//
// 本文件只含 Agent 侧工具定义 + 派发 + 决策增强提示,不 import
// game/virtual_city(依赖反转,与 tools.go 同构)。
package vcplayer

import (
	"encoding/json"
	"fmt"

	"LsmAgentGame/agent/vctypes"
	"LsmAgentGame/errcode"
	llmtypes "LsmAgentGame/llm/types"
)

// ── 批次35 合并交易工具名常量(5 个)──

const (
	// ToolMarketListing 吸收 list_asset + cancel_listing + view_listings(3→1)。
	ToolMarketListing = "market_listing"
	// ToolNegotiate 吸收 start_negotiate + respond_negotiate(2→1)。
	ToolNegotiate = "negotiate"
	// ToolP2PLending 吸收 create_loan_listing + accept_loan + repay_p2p_loan +
	// add_guarantor(4→1)。
	ToolP2PLending = "p2p_lending"
	// ToolAuctionBid 原 bid_auction 改名(参数原样)。
	ToolAuctionBid = "auction_bid"
	// ToolInfoMarket 吸收 sell_info + bid_info(2→1)。
	ToolInfoMarket = "info_market"
)

// tradeToolNames 是 5 个合并交易工具名有序列表(DispatchTool default 分支路由用)。
var tradeToolNames = []string{
	ToolMarketListing, ToolNegotiate, ToolP2PLending, ToolAuctionBid, ToolInfoMarket,
}

// TradeToolNames 返回 5 个合并交易工具名(测试/lint 用)。
func TradeToolNames() []string {
	out := make([]string, len(tradeToolNames))
	copy(out, tradeToolNames)
	return out
}

// ── 工具定义 ──

// TradeToolDefinitions 返回 5 个合并交易工具定义(Anthropic wire)。
// 每个工具的 InputSchema 是标准 JSON Schema object;required 字段在描述中
// 显式标注,避免 LLM 遗漏。
func TradeToolDefinitions() []llmtypes.ToolDef {
	return []llmtypes.ToolDef{
		{
			Name: ToolMarketListing,
			Description: "挂单簿(三 op)。list=挂牌出售自有资产(房产/商铺/副业/股票/债券/黄金),进入挂单簿公开可见;" +
				"设定 ask_cny(要价)与 min_cny(底价,保密;低于底价系统自动拒绝);每座位最多 3 笔 open 挂单," +
				"资产挂牌期间不可重复操作;耗 1 次动作预算。cancel=取消自己的一笔 open 挂单(资产归还," +
				"不消耗预算外的手续费);耗 1 次动作预算。view=查看当前挂单簿(open 状态的挂单):" +
				"资产出售/收购/信息/借贷要约;不消耗动作预算。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"op":          enumStrSchema([]string{"list", "cancel", "view"}, "list=挂牌 / cancel=撤单 / view=看挂单簿"),
					"asset_index": intSchema(0, "资产在 check_state 资产列表中的下标(0 起;op=list 必填)"),
					"ask_cny":     intSchema(1, "要价(元;op=list 必填)"),
					"min_cny":     intSchema(1, "底价(元,保密);低于底价自动流拍(op=list 必填)"),
					"listing_id":  strSchema("挂单 id,如 LF1(挂出)/LB1(收购);op=cancel 必填"),
					"type_filter": strSchema("op=view 可选过滤:asset|buy|info|loan_ofr|loan_req(空=全部)"),
				},
				"required": []string{"op"},
			},
		},
		{
			Name: ToolNegotiate,
			Description: "议价(两 op,均耗 1 次动作预算)。start=对一笔 open 挂单发起议价(响应方为挂单主):" +
				"首轮报价 offer_cny 必须 ≥ 挂单 ask_cny × 50%;系统创建议价会话 N1,轮到对方响应。" +
				"respond=响应议价会话(还价/接受/拒绝):action=offer(还价,offer_cny 必填)/accept(接受)/" +
				"reject(拒绝);可附议价留言 comment(≤60字,可选)。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"op":         enumStrSchema([]string{"start", "respond"}, "start=发起议价 / respond=响应议价"),
					"listing_id": strSchema("目标挂单 id(op=start 必填)"),
					"offer_cny":  intSchema(0, "报价(元);op=start 必填且 ≥ ask×50%;op=respond 且 action=offer 必填"),
					"neg_id":     strSchema("议价会话 id,如 N1(op=respond 必填)"),
					"action":     strSchema("offer|accept|reject(op=respond 必填)"),
					"comment":    map[string]any{"type": "string", "maxLength": 60, "description": "议价留言(≤60字,可选)"},
				},
				"required": []string{"op"},
			},
		},
		{
			Name: ToolP2PLending,
			Description: "居民间借贷(四 op,均耗 1 次动作预算)。create=创建借贷挂单(出借或借款):" +
				"direction=lend(出借要约):本金从现金冻结,利率 ≥ 0.3%/月;direction=borrow(借款请求):" +
				"利率 ≤ 3.6%/月(法定上限);term 为期数(月);need_guarantee=true 可降低利率 0.3%/月。" +
				"accept=接受一笔 open 借贷挂单(匹配成交):若挂单为 lend(出借要约),你作为借方签合约;" +
				"若为 borrow(借款请求),你作为贷方签合约;系统生成 P2P 合约 P2P1,资金+利息计划生效。" +
				"repay=偿还 P2P 借贷合约(部分或全额):amount_cny=0 或 ≥ 余额视为全额结清;部分还款 ≥ 1000 元;" +
				"月结时系统自动扣月供,现金不足则逾期(罚息 5%、信用 −50)。" +
				"guarantee=为他人的一笔 P2P 借款提供担保(降低利率 0.3%/月):不可自担保/不可为同一笔借多次担保;" +
				"担保后若借款人连续 3 月逾期,你须代偿。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"op":             enumStrSchema([]string{"create", "accept", "repay", "guarantee"}, "create=挂借贷单 / accept=接单成交 / repay=还款 / guarantee=担保"),
					"direction":      strSchema("lend(出借)|borrow(借款);op=create 必填"),
					"principal":      intSchema(1, "本金(元;op=create 必填)"),
					"rate":           map[string]any{"type": "number", "minimum": 0.003, "maximum": 0.036, "description": "月利率(小数,0.3%-3.6%;op=create 必填)"},
					"term":           intSchema(1, "期数(月;op=create 必填)"),
					"need_guarantee": map[string]any{"type": "boolean", "description": "是否需要担保(op=create 可选)"},
					"listing_id":     strSchema("借贷挂单 id(op=accept 必填)"),
					"loan_id":        strSchema("P2P 合约 id,如 P2P1(op=repay/guarantee 必填)"),
					"amount_cny":     intSchema(0, "还款金额(元);0=全额结清(op=repay)"),
				},
				"required": []string{"op"},
			},
		},
		{
			Name: ToolAuctionBid,
			Description: "参与英式公开叫价拍卖(房产大宗交易;耗 1 次动作预算)。" +
				"起拍价 = 评估价 × 80%;加价 ≥ 最小加价额(住宅 10000 / 商业 50000);" +
				"连续一轮无人加价时最高价者得;最高价 < 评估价 × 90% 挂牌者可拒绝(流拍)。",
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
			Name: ToolInfoMarket,
			Description: "信息交易(两 op,均耗 1 次动作预算)。sell=出售信息(密封暗标):" +
				"category=market(市场内幕)/intel(玩家情报)/personal(个人概况);信息以密封暗标形式竞购," +
				"60 秒内最高价者得;平局人脉高者得;detail 为信息详情(成交后揭示给中标者)。" +
				"bid=参与信息密封暗标竞购(暗标):同一信息可有多人出价,最高价者得;" +
				"出价必须 ≥ min_bid;不可自购自己出售的信息。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"op":         enumStrSchema([]string{"sell", "bid"}, "sell=出售信息 / bid=暗标竞购"),
					"category":   strSchema("market|intel|personal(op=sell 必填)"),
					"title":      map[string]any{"type": "string", "maxLength": 40, "description": "信息标题(公开,≤40字;op=sell 必填)"},
					"detail":     map[string]any{"type": "string", "maxLength": 200, "description": "信息详情(密封,≤200字;op=sell 必填)"},
					"min_bid":    intSchema(1, "最低出价(元;op=sell 必填)"),
					"listing_id": strSchema("信息出售挂单 id(op=bid 必填)"),
					"bid_cny":    intSchema(1, "出价(元;op=bid 必填)"),
				},
				"required": []string{"op"},
			},
		},
	}
}

// ── 工具派发 ──

// DispatchTradeTool 派发单个合并交易工具(按 op 路由)到 ToolRunner。
// 入参 input 是 Anthropic tool_use 的 input 字段(RawMessage);返回结果含
// 人读文本(供 tool_result 回喂 LLM)与 Budget 置位(批次35 §3.3:
// 仅 market_listing.op=view 不耗预算,其余均耗)。
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
	case ToolMarketListing:
		switch getStr("op") {
		case "cancel":
			res.Budget = true
			return failOr(runner.CancelListing(seat, getStr("listing_id")), "已取消挂单")
		case "view":
			// 查询类:不耗预算(旧 view_listings 语义)。
			s, err := runner.ViewListings(seat, getStr("type_filter"))
			if err != nil {
				return fail(err)
			}
			return ok(s)
		default: // list
			res.Budget = true
			return failOr(runner.ListAsset(seat, int(getInt("asset_index")), getInt("ask_cny"), getInt("min_cny")),
				"挂牌成功")
		}
	case ToolNegotiate:
		// 两 op 均耗预算。
		res.Budget = true
		if getStr("op") == "respond" {
			return failOr(runner.RespondNegotiate(seat, getStr("neg_id"), getStr("action"), getInt("offer_cny"), getStr("comment")),
				"议价已响应")
		}
		return failOr(runner.StartNegotiate(seat, getStr("listing_id"), getInt("offer_cny")),
			"议价已发起")
	case ToolP2PLending:
		// 四 op 均耗预算。
		res.Budget = true
		switch getStr("op") {
		case "accept":
			return failOr(runner.AcceptLoan(seat, getStr("listing_id")), "已接受借贷要约,合约生效")
		case "repay":
			return failOr(runner.RepayP2PLoan(seat, getStr("loan_id"), getInt("amount_cny")), "还款成功")
		case "guarantee":
			return failOr(runner.AddGuarantor(seat, getStr("loan_id")), "担保已生效")
		default: // create
			return failOr(runner.CreateLoanListing(seat, getStr("direction"), getInt("principal"),
				getFloat("rate"), int(getInt("term")), getBool("need_guarantee")), "借贷挂单已创建")
		}
	case ToolAuctionBid:
		res.Budget = true
		return failOr(runner.BidAuction(seat, getStr("auction_id"), getInt("amount_cny")), "出价成功")
	case ToolInfoMarket:
		// 两 op 均耗预算。
		res.Budget = true
		if getStr("op") == "bid" {
			return failOr(runner.BidInfo(seat, getStr("listing_id"), getInt("bid_cny")), "暗标已提交")
		}
		return failOr(runner.SellInfo(seat, getStr("category"), getStr("title"), getStr("detail"), getInt("min_bid")),
			"信息已挂牌(密封暗标)")
	default:
		res.IsErr = true
		res.Text = "未知交易工具: " + toolName
		return res
	}
}

// ── 决策增强提示 ──

// TradeStrategyHint 根据本人财务状态生成交易感知策略提示段,追加到
// UserPrompt 末尾(§10.2 Agent 决策增强)。
// 批次35 §3.4:工具名同步为合并后新名(market_listing/p2p_lending/info_market)。
// 返回空字符串表示无交易信号(不追加)。
func TradeStrategyHint(ctx *vctypes.GameContext) string {
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
			"现金充裕(¥%d > 2×月支出 ¥%d):可用 market_listing(op=view) 寻找低估资产,或用 p2p_lending(op=create,direction=lend) 放贷吃息(利率 0.3%%-3.6%%/月)。",
			me.Cash, monthlyExpense))
	}

	// 现金紧张(< 0.5×月支出):挂牌出售资产/发起借款请求。
	if me.Cash < monthlyExpense/2 && len(me.Assets) > 0 {
		signals = append(signals, fmt.Sprintf(
			"现金紧张(¥%d < 0.5×月支出 ¥%d):可用 market_listing(op=list) 挂牌变现非核心资产,或用 p2p_lending(op=create,direction=borrow) 发起借款请求。",
			me.Cash, monthlyExpense))
	}

	// 信息优势(认知 ≥ 5):出售情报获利。
	if me.Cognition >= 5 {
		signals = append(signals, fmt.Sprintf(
			"认知优势(%d/10):可用 info_market(op=sell) 出售市场内幕/玩家情报,密封暗标获利。", me.Cognition))
	}

	// 人脉优势(≥ 5):撮合交易赚佣金/担保赚利差。
	if me.Network >= 5 {
		signals = append(signals, fmt.Sprintf(
			"人脉优势(%d/10):可用 p2p_lending(op=guarantee) 为优质借贷担保(降低利率 0.3%%/月,赚取利差);撮合买卖双方议价赚佣金。", me.Network))
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
