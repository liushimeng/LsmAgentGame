// Package wealthplayer — tools.go: 17 工具(Anthropic wire)+ DispatchTool +
// ToolRunner 接口定义(2026-09-14 §财商流P0)。
//
// 契约: Agent 设计文档 §7 工具表;动作语义唯一事实来源 = 协议契约文档 §4
// (与人类 game.wealth_action 同一 actions.go 代码路径,无分叉)。
package wealthplayer

import (
	"encoding/json"
	"fmt"

	"LsmAgentGame/errcode"
	llmtypes "LsmAgentGame/llm/types"
)

// ToolRunner 是引擎桥接口(game/wealth/agent_runner.go 实现;in-process 不走 WS)。
// 所有方法返回 error(引擎侧为 *errcode.Error,350xx 错误码)。
type ToolRunner interface {
	CheckState(seat int) string
	BuyAsset(seat int, asset string, amountCNY int64) error
	SellAsset(seat int, asset string, units float64) error
	BuyHouse(seat int, district string, downpayRatio float64, asset string) error
	TakeLoan(seat int, kind string, amountCNY int64) error
	RepayLoan(seat int, loanID string, amountCNY int64) error
	StartSideBusiness(seat int, kind string) error
	StopSideBusiness(seat int) error
	Study(seat int) error
	Socialize(seat int) error
	Rest(seat int) error
	WorkOvertime(seat int) error
	MoveDistrict(seat int, district string) error
	Consume(seat int, amountCNY int64, reason string) error
	Donate(seat int, amountCNY int64) error
	Speak(seat int, text, internalThought string) error
	SubmitMonth(seat int) error
}

// 工具名常量。
const (
	ToolCheckState       = "check_state"
	ToolBuyAsset         = "buy_asset"
	ToolSellAsset        = "sell_asset"
	ToolBuyHouse         = "buy_house"
	ToolTakeLoan         = "take_loan"
	ToolRepayLoan        = "repay_loan"
	ToolStartSide        = "start_side_business"
	ToolStopSide         = "stop_side_business"
	ToolStudy            = "study"
	ToolSocialize        = "socialize"
	ToolRest             = "rest"
	ToolWorkOvertime     = "work_overtime"
	ToolMoveDistrict     = "move_district"
	ToolConsume          = "consume"
	ToolDonate           = "donate"
	ToolSpeak            = "speak"
	ToolSubmitMonth      = "submit_month"
)

// schema helpers。
func strSchema(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

func intSchema(min int64, desc string) map[string]any {
	return map[string]any{"type": "integer", "minimum": min, "description": desc}
}

// BuildTools 返回 17 个工具定义(全部座位相同——财商流信息不对称在 my.* 快照,
// 不在工具裁剪)。
func BuildTools() []llmtypes.ToolDef {
	return []llmtypes.ToolDef{
		{
			Name:        ToolCheckState,
			Description: "查看本人三表/资产/贷款/资源/信用摘要文本(不消耗动作预算)。",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name: ToolBuyAsset,
			Description: "按市价买入金融资产:stock_index(指数基金,佣金0.025%最低5元)/" +
				"bond(债券,锁定当期年化)/gold(黄金)。金额 ≥1000 元;整份成交。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"asset":      strSchema("stock_index|bond|gold"),
					"amount_cny": intSchema(1000, "买入金额(元)"),
				},
				"required": []string{"asset", "amount_cny"},
			},
		},
		{
			Name:        ToolSellAsset,
			Description: "卖出持仓:金融资产按份额(units≥1);房产/商铺整售(units=1)。股票佣金0.025%最低5元;黄金手续费0.5%;房产增值税5%+中介2%(满5年唯一免增值税),卖房先偿房贷。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"asset": strSchema(`"stock_index"|"bond"|"gold"|"house:<district>"|"shop:<district>"`),
					"units": map[string]any{"type": "number", "minimum": 1, "description": "份额/克数;房产=1"},
				},
				"required": []string{"asset", "units"},
			},
		},
		{
			Name:        ToolBuyHouse,
			Description: "购房:住宅(首付≥30%,余额 30 年房贷 LPR+0.5%,上限 4 套)或商铺(asset=shop,P0 全款,上限 4 间)。买入时若当前租房自动设为自住。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"district":      strSchema("8 区 id:finance|tech|industry|oldtown|commerce|residential|suburb|riverside"),
					"downpay_ratio": map[string]any{"type": "number", "minimum": 0.3, "maximum": 1.0, "description": "首付比例"},
					"asset":         strSchema(`"house"(默认)|"shop"`),
				},
				"required": []string{"district", "downpay_ratio"},
			},
		},
		{
			Name:        ToolTakeLoan,
			Description: "借款:consumer(消费贷,10%年化,3年,≤min(月收入×12,20万),需无逾期)/credit(信用贷三档 5/10/20 万,月息0.8%/1.2%/1.8%,到期还本——会压低工资增长与副业收入)/business(经营贷,LPR+2%,先息后本,≤20万,需有副业)。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"kind":        strSchema("consumer|credit|business"),
					"amount_cny":  intSchema(1, "金额(credit 必须是 50000/100000/200000 之一)"),
				},
				"required": []string{"kind", "amount_cny"},
			},
		},
		{
			Name:        ToolRepayLoan,
			Description: "提前还本 ≥1 万元或结清;等额本息自动重算月供;信用贷全部还清即解除 Brass 档副作用。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"loan_id":     strSchema("贷款 id,如 L3"),
					"amount_cny":  intSchema(1, "还款金额(元)"),
				},
				"required": []string{"loan_id", "amount_cny"},
			},
		},
		{
			Name:        ToolStartSide,
			Description: "启动副业:delivery(无门槛)/content(认知≥2)/freelance(认知≥3)/tutoring(认知≥4);月入约 2000–6000 元,月耗精力 2。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"kind": strSchema("delivery|content|tutoring|freelance"),
				},
				"required": []string{"kind"},
			},
		},
		{
			Name:        ToolStopSide,
			Description: "停掉副业(无残值,精力释放)。",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name:        ToolStudy,
			Description: "学习进修:¥2000 / 精力−1 / 认知+1(认知满 10 不可)。",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name:        ToolSocialize,
			Description: "社交应酬:¥1000 / 人脉+1(人脉满 10 不可)。",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name:        ToolRest,
			Description: "休整:精力+2(精力满 10 不可)。",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name:        ToolWorkOvertime,
			Description: "加班:精力−2,当月奖金 = 工资×0.3(需在职)。",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name:        ToolMoveDistrict,
			Description: "迁区:¥3000 / 精力−1;若目标区有自有住宅自动改为自住。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"district": strSchema("8 区 id 之一(不可与当前相同)"),
				},
				"required": []string{"district"},
			},
		},
		{
			Name:        ToolConsume,
			Description: "自由消费(记事,无机制效果)。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"amount_cny": intSchema(1, "金额(元)"),
					"reason":     strSchema("消费理由(≤40字,可选)"),
				},
				"required": []string{"amount_cny"},
			},
		},
		{
			Name:        ToolDonate,
			Description: "公益捐赠 ≥¥1000:计入社会贡献分(每万元 1 分,上限 10);前 3 次人脉+1。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"amount_cny": intSchema(1000, "金额(元)"),
				},
				"required": []string{"amount_cny"},
			},
		},
		{
			Name:        ToolSpeak,
			Description: "公屏发言(每月最多 1 次):像真人聊天,谈行情/吐槽生活/分享买卖心得;不要复述工具参数。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"text":             map[string]any{"type": "string", "minLength": 1, "maxLength": 100, "description": "发言(≤100字)"},
					"internal_thought": map[string]any{"type": "string", "maxLength": 200, "description": "内心独白(仅本人/观战者可见)"},
				},
				"required": []string{"text"},
			},
		},
		{
			Name:        ToolSubmitMonth,
			Description: "结束本月(必须调用;不调用也会被系统强制结束)。不消耗动作预算。",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
	}
}

// ToolNames 返回全部工具名(测试/lint 用)。
func ToolNames() []string {
	return []string{
		ToolCheckState, ToolBuyAsset, ToolSellAsset, ToolBuyHouse, ToolTakeLoan,
		ToolRepayLoan, ToolStartSide, ToolStopSide, ToolStudy, ToolSocialize,
		ToolRest, ToolWorkOvertime, ToolMoveDistrict, ToolConsume, ToolDonate,
		ToolSpeak, ToolSubmitMonth,
	}
}

// dispatchToolResult 是一次工具派发的结果。
type dispatchToolResult struct {
	Name   string
	Input  string // 原始 input JSON
	Text   string // 人读结果
	IsErr  bool
}

// DispatchTool 派发单个 tool_use 到 ToolRunner。
// input 数值统一按 JSON number(float64)解出;失败作为 IsErr 结果回喂 LLM。
func (a *Agent) DispatchTool(name string, input map[string]any) dispatchToolResult {
	inputJSON, _ := json.Marshal(input)
	res := dispatchToolResult{Name: name, Input: string(inputJSON)}
	getStr := func(k string) string { s, _ := input[k].(string); return s }
	getInt := func(k string) int64 {
		switch v := input[k].(type) {
		case float64:
			return int64(v)
		case int:
			return int64(v)
		case int64:
			return v
		default:
			return 0
		}
	}
	getFloat := func(k string) float64 {
		switch v := input[k].(type) {
		case float64:
			return v
		case int:
			return float64(v)
		default:
			return 0
		}
	}

	ok := func(text string) dispatchToolResult {
		res.Text = text
		return res
	}
	fail := func(err error) dispatchToolResult {
		// typed-nil 防御(2026-09-15 §财商流P0-bugfix):与 failOr 同源保护。
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

	if a.runner == nil {
		return fail(fmt.Errorf("tool runner not bound"))
	}
	seat := a.MySeat
	switch name {
	case ToolCheckState:
		return ok(a.runner.CheckState(seat))
	case ToolBuyAsset:
		return failOr(a.runner.BuyAsset(seat, getStr("asset"), getInt("amount_cny")), "买入成功", res)
	case ToolSellAsset:
		return failOr(a.runner.SellAsset(seat, getStr("asset"), getFloat("units")), "卖出成功", res)
	case ToolBuyHouse:
		return failOr(a.runner.BuyHouse(seat, getStr("district"), getFloat("downpay_ratio"), getStr("asset")), "购房成功", res)
	case ToolTakeLoan:
		return failOr(a.runner.TakeLoan(seat, getStr("kind"), getInt("amount_cny")), "放款成功", res)
	case ToolRepayLoan:
		return failOr(a.runner.RepayLoan(seat, getStr("loan_id"), getInt("amount_cny")), "还款成功", res)
	case ToolStartSide:
		return failOr(a.runner.StartSideBusiness(seat, getStr("kind")), "副业已启动", res)
	case ToolStopSide:
		return failOr(a.runner.StopSideBusiness(seat), "副业已停止", res)
	case ToolStudy:
		return failOr(a.runner.Study(seat), "学习完成", res)
	case ToolSocialize:
		return failOr(a.runner.Socialize(seat), "社交完成", res)
	case ToolRest:
		return failOr(a.runner.Rest(seat), "休整完成", res)
	case ToolWorkOvertime:
		return failOr(a.runner.WorkOvertime(seat), "本月已加班", res)
	case ToolMoveDistrict:
		return failOr(a.runner.MoveDistrict(seat, getStr("district")), "迁居完成", res)
	case ToolConsume:
		return failOr(a.runner.Consume(seat, getInt("amount_cny"), getStr("reason")), "消费完成", res)
	case ToolDonate:
		return failOr(a.runner.Donate(seat, getInt("amount_cny")), "捐赠完成", res)
	case ToolSpeak:
		return failOr(a.runner.Speak(seat, getStr("text"), getStr("internal_thought")), "已发言", res)
	case ToolSubmitMonth:
		return failOr(a.runner.SubmitMonth(seat), "本月已提交", res)
	default:
		res.IsErr = true
		res.Text = "未知工具: " + name
		return res
	}
}

func failOr(err error, successText string, res dispatchToolResult) dispatchToolResult {
	// typed-nil 防御(2026-09-15 §财商流P0-bugfix):实现侧若返回 *errcode.Error
	// nil 指针,装入 error 接口后 != nil 但 .Error() 会 panic。本函数对入参
	// 做 interface-nil + typed-nil 双保险,杜绝上游 panic 蔓延到服务主进程。
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
