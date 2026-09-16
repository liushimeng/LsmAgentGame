// Package wealthplayer — tools.go: 工具定义(Anthropic wire)+ DispatchTool +
// ToolRunner 接口定义(2026-09-14 §财商流P0;P1-2 追加 set_consumption /
// answer_survey / query_economy 三工具)。
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
	// P1 新增: 央行/银行体系查询 + 存款。
	QueryCentralBank(seat int) (string, error)
	QueryBankingSystem(seat int) (string, error)
	ApplyLoanWithCredit(seat int, kind string, amountCNY int64) (string, error)
	DepositSavings(seat int, amountCNY int64) error
	WithdrawSavings(seat int, amountCNY int64) error
	// P1 扩展: 明斯基 / 提前还款。
	QueryMinsky(seat int) (string, error)
	EarlyRepay(seat int, loanID string, amountCNY int64) error
	// P1(2026-09-16 §财商流P1-2 §7.1): 消费档位 / 社会调研 / 经济查询。
	SetConsumption(seat int, level int) error
	AnswerSurvey(seat int, surveyID string, optionIdx int, reason string) error
	QueryEconomy(seat int) (string, error)
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
	// P1 新增。
	ToolQueryCentralBank  = "query_central_bank"
	ToolQueryBankingSystem = "query_banking_system"
	ToolApplyLoanWithCredit = "apply_loan_with_credit"
	ToolDepositSavings    = "deposit_savings"
	ToolWithdrawSavings   = "withdraw_savings"
	// P1 扩展: 明斯基 / 提前还款。
	ToolQueryMinsky       = "query_minsky"
	ToolEarlyRepay        = "early_repay"
	// P1(§财商流P1-2 §7.1): 消费档位 / 社会调研 / 经济查询。
	ToolSetConsumption = "set_consumption"
	ToolAnswerSurvey   = "answer_survey"
	ToolQueryEconomy   = "query_economy"
)

// schema helpers。
func strSchema(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

func intSchema(min int64, desc string) map[string]any {
	return map[string]any{"type": "integer", "minimum": min, "description": desc}
}

// BuildTools 返回全部工具定义(全部座位相同——财商流信息不对称在 my.* 快照,
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
		{
			Name:        ToolQueryCentralBank,
			Description: "查询央行货币政策状态(不消耗动作预算):M0/M1/M2、基础货币、货币乘数、政策利率、LPR、CPI、信贷约束、额度系数。",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name:        ToolQueryBankingSystem,
			Description: "查询商业银行体系汇总(不消耗动作预算):活期/定期存款、准备金、超额准备金、贷款余额。",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name:        ToolApplyLoanWithCredit,
			Description: "带信贷约束的贷款申请(不消耗动作预算,仅查询额度/利率/批准结果):消费贷/经营贷在信贷紧缩时额度收紧、利率上浮、信用门槛提高。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"kind":        strSchema("consumer|credit|business"),
					"amount_cny":  intSchema(1, "申请金额(元)"),
				},
				"required": []string{"kind", "amount_cny"},
			},
		},
		{
			Name:        ToolDepositSavings,
			Description: "活期→定期存款(不消耗动作预算):年利率 1.5%;提前支取损失全部利息。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"amount_cny": intSchema(1, "金额(元)"),
				},
				"required": []string{"amount_cny"},
			},
		},
		{
			Name:        ToolWithdrawSavings,
			Description: "定期→活期(不消耗动作预算):提前支取损失全部利息。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"amount_cny": intSchema(1, "金额(元)"),
				},
				"required": []string{"amount_cny"},
			},
		},
		{
			Name:        ToolQueryMinsky,
			Description: "查询明斯基全局状态(不消耗动作预算):庞氏/投机/对冲玩家数与占比、明斯基时刻冷却剩余月、累计触发次数。",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name:        ToolEarlyRepay,
			Description: "提前还款(仅房贷):amount_cny=全额还清(≤0)或部分还款;1 年内罚息 1-3%(线性化)。可节省未来利息、降低杠杆、规避明斯基清算。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"loan_id":     strSchema("房贷 id,如 L3"),
					"amount_cny":  intSchema(0, "还款金额(元);0 或 ≥余额=全额还清"),
				},
				"required": []string{"loan_id"},
			},
		},
		{
			Name:        ToolSetConsumption,
			Description: "设置消费档位(耗 1 次动作预算,立即生效):0 节俭(生活支出×0.6/精力−1)/1 标准(×1.0)/2 精致(×1.5/精力+1)/3 奢侈(×2.2/精力+2)。你的消费决定全城物价与就业;现金 < 2×月生活支出时会被强制降为节俭档。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"level": intSchema(0, "0 节俭/1 标准/2 精致/3 奢侈"),
				},
				"required": []string{"level"},
			},
		},
		{
			Name:        ToolAnswerSurvey,
			Description: "回答进行中的社会调研(不耗动作次数):按你的人设与真实财务处境表态,理由说人话(≤50 字),不要中立和稀泥。每月只能答一次,提交后不可修改。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"survey_id":    strSchema("调研 id,如 SV1(见经济环境段)"),
					"option_index": intSchema(0, "选项下标(0 起,对应经济环境段列表)"),
					"reason":       map[string]any{"type": "string", "maxLength": 50, "description": "≤50 字理由"},
				},
				"required": []string{"survey_id", "option_index"},
			},
		},
		{
			Name:        ToolQueryEconomy,
			Description: "查询经济全景(不消耗动作预算):CPI 同比/环比、八大类价格环比、失业率、工资增长、企业营收、基尼系数、圈层分布。",
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
		ToolQueryCentralBank, ToolQueryBankingSystem, ToolApplyLoanWithCredit,
		ToolDepositSavings, ToolWithdrawSavings,
		ToolQueryMinsky, ToolEarlyRepay,
		ToolSetConsumption, ToolAnswerSurvey, ToolQueryEconomy,
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
	case ToolQueryCentralBank:
		s, err := a.runner.QueryCentralBank(seat)
		if err != nil {
			return fail(err)
		}
		return ok(s)
	case ToolQueryBankingSystem:
		s, err := a.runner.QueryBankingSystem(seat)
		if err != nil {
			return fail(err)
		}
		return ok(s)
	case ToolApplyLoanWithCredit:
		s, err := a.runner.ApplyLoanWithCredit(seat, getStr("kind"), getInt("amount_cny"))
		if err != nil {
			return fail(err)
		}
		return ok(s)
	case ToolDepositSavings:
		return failOr(a.runner.DepositSavings(seat, getInt("amount_cny")), "存款完成", res)
	case ToolWithdrawSavings:
		return failOr(a.runner.WithdrawSavings(seat, getInt("amount_cny")), "支取完成", res)
	case ToolQueryMinsky:
		s, err := a.runner.QueryMinsky(seat)
		if err != nil {
			return fail(err)
		}
		return ok(s)
	case ToolEarlyRepay:
		return failOr(a.runner.EarlyRepay(seat, getStr("loan_id"), getInt("amount_cny")), "提前还款完成", res)
	case ToolSetConsumption:
		return failOr(a.runner.SetConsumption(seat, int(getInt("level"))), "已调整消费档位", res)
	case ToolAnswerSurvey:
		return failOr(a.runner.AnswerSurvey(seat, getStr("survey_id"), int(getInt("option_index")), getStr("reason")), "已回答调研", res)
	case ToolQueryEconomy:
		s, err := a.runner.QueryEconomy(seat)
		if err != nil {
			return fail(err)
		}
		return ok(s)
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
