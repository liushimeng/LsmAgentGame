// Package vcplayer — tools.go: 工具定义(Anthropic wire)+ DispatchTool +
// ToolRunner 接口定义(2026-09-14 §财商流P0;P1-2 追加 set_consumption /
// answer_survey / query_economy 三工具)。
//
// 契约: Agent 设计文档 §7 工具表;动作语义唯一事实来源 = 协议契约文档 §4
// (与人类 game.virtual_city_action 同一 actions.go 代码路径,无分叉)。
package vcplayer

import (
	"encoding/json"
	"fmt"
	"strings"

	"LsmAgentGame/agent/vctypes"
	"LsmAgentGame/errcode"
	"LsmAgentGame/game/virtual_city/profession"
	llmtypes "LsmAgentGame/llm/types"
)

// districtIDsHintDesc 城区 id 描述串(批次20 B2-9):动态从
// profession.DistrictIDs() 生成 —— BE-1 扩表(8→16→32)时本串自动跟随,
// **不写死数量**(旧文案「8 区 id:…」在 16 区表下曾误导 LLM,§130)。
// 格式:前 8 个示例 + 「…等 N 区」(N=总数;N≤8 时全列并标 N 区)。
func districtIDsHintDesc() string {
	ids := profession.DistrictIDs()
	n := len(ids)
	first := ids
	if n > 8 {
		first = ids[:8]
	}
	base := strings.Join(first, "|")
	if n > 8 {
		return fmt.Sprintf("城区 id:%s…等 %d 区", base, n)
	}
	return fmt.Sprintf("城区 id:%s(%d 区)", base, n)
}

// ToolRunner 是引擎桥接口(game/virtual_city/agent_runner.go 实现;in-process 不走 WS)。
// 所有方法返回 error(引擎侧为 *errcode.Error,350xx 错误码)。
type ToolRunner interface {
	CheckState(seat int) string
	BuyAsset(seat int, asset string, amountCNY int64) error
	SellAsset(seat int, asset string, units float64) error
	BuyHouse(seat int, district string, downpayRatio float64, asset string) error
	TakeLoan(seat int, kind string, amountCNY int64) error
	RepayLoan(seat int, loanID string, amountCNY int64) error
	// StartSideBusiness 批次20(文档2 §3)增 tier 参数:0=中价(缺省旧行为)
	// 1=低价 2=高价。
	StartSideBusiness(seat int, kind string, tier int) error
	StopSideBusiness(seat int) error
	// SetSidePrice 副业改价(批次20 文档2 §3;耗 1 点月预算,每月 ≤1 次)。
	SetSidePrice(seat int, tier int) error
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
	// P1-4(2026-09-19 §财商流P1-4 §7): 商业保险(投保/退保走 ApplyAction 同一路径)。
	BuyInsurance(seat int, kind string) error
	CancelInsurance(seat int, kind string) error
	GetInsuranceStatus(seat int) (string, error)
	// P2(2026-09-16 §财商流P2): 玩家间交易与财富流动系统 12 工具。
	ListAsset(seat int, assetIndex int, askCNY, minCNY int64) error
	CancelListing(seat int, listingID string) error
	ViewListings(seat int, typeFilter string) (string, error)
	StartNegotiate(seat int, listingID string, offerCNY int64) error
	RespondNegotiate(seat int, negID string, action string, offerCNY int64, comment string) error
	CreateLoanListing(seat int, direction string, principal int64, rate float64, term int, needGuarantee bool) error
	AcceptLoan(seat int, listingID string) error
	RepayP2PLoan(seat int, loanID string, amountCNY int64) error
	AddGuarantor(seat int, loanID string) error
	BidAuction(seat int, auctionID string, amountCNY int64) error
	SellInfo(seat int, category string, title string, detail string, minBid int64) error
	BidInfo(seat int, listingID string, bidCNY int64) error
	// §CityHuman重构(2026-09-22): 感知与行动五件套。感知三件套不耗动作预算
	// (每月各限 2 次,Agent 侧计数);move 耗 1 次动作预算;speak private
	// 与 area 合计每月 ≤2。
	See(seat int) (*vctypes.SenseResult, error)
	Hear(seat int) (*vctypes.SenseResult, error)
	Smell(seat int) (*vctypes.SenseResult, error)
	Move(seat int, destination string, mode string) error
	SpeakTo(seat int, targetSeat int, text string) error
}

// 工具名常量。
const (
	ToolCheckState   = "check_state"
	ToolBuyAsset     = "buy_asset"
	ToolSellAsset    = "sell_asset"
	ToolBuyHouse     = "buy_house"
	ToolTakeLoan     = "take_loan"
	ToolRepayLoan    = "repay_loan"
	ToolStartSide    = "start_side_business"
	ToolStopSide     = "stop_side_business"
	// ToolSetSidePrice 副业改价(批次20 文档2 §4.1;动作类,耗 1 点月预算)。
	ToolSetSidePrice = "set_side_price"
	ToolStudy        = "study"
	ToolSocialize    = "socialize"
	ToolRest         = "rest"
	ToolWorkOvertime = "work_overtime"
	ToolMoveDistrict = "move_district"
	ToolConsume      = "consume"
	ToolDonate       = "donate"
	ToolSpeak        = "speak"
	ToolSubmitMonth  = "submit_month"
	// P1 新增。
	ToolQueryCentralBank    = "query_central_bank"
	ToolQueryBankingSystem  = "query_banking_system"
	ToolApplyLoanWithCredit = "apply_loan_with_credit"
	ToolDepositSavings      = "deposit_savings"
	ToolWithdrawSavings     = "withdraw_savings"
	// P1 扩展: 明斯基 / 提前还款。
	ToolQueryMinsky = "query_minsky"
	ToolEarlyRepay  = "early_repay"
	// P1(§财商流P1-2 §7.1): 消费档位 / 社会调研 / 经济查询。
	ToolSetConsumption = "set_consumption"
	ToolAnswerSurvey   = "answer_survey"
	ToolQueryEconomy   = "query_economy"
	// P1-4(2026-09-19 §财商流P1-4 §7): 商业保险三工具。
	ToolBuyInsurance       = "buy_insurance"        // 动作类,耗 1 点月预算
	ToolCancelInsurance    = "cancel_insurance"     // 动作类,耗 1 点月预算
	ToolGetInsuranceStatus = "get_insurance_status" // 查询类,不耗月预算
	// §CityHuman重构(2026-09-22): 感知与行动工具(tools_sense.go 定义 wire)。
	ToolSee   = "see"   // 感知类,不耗预算,每月 ≤2
	ToolHear  = "hear"  // 感知类,不耗预算,每月 ≤2
	ToolSmell = "smell" // 感知类,不耗预算,每月 ≤2
	ToolMove  = "move"  // 动作类,耗 1 点月预算
)

// schema helpers。
func strSchema(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

func intSchema(min int64, desc string) map[string]any {
	return map[string]any{"type": "integer", "minimum": min, "description": desc}
}

// BuildTools 返回全部工具定义(全部座位相同——虚拟城市信息不对称在 my.* 快照,
// 不在工具裁剪)。
// P0/P1/P1-2 基础工具 + P2 交易工具(§财商流P2),返回前追加 TradeToolDefinitions()。
func BuildTools() []llmtypes.ToolDef {
	base := []llmtypes.ToolDef{
		{
			Name:        ToolCheckState,
			Description: "查看本人三表/资产/贷款/资源/信用摘要文本(不消耗动作预算)。",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name: ToolBuyAsset,
			Description: "按市价买入金融资产:stock_index(指数基金,佣金0.025%最低5元)/" +
				"bond(债券,锁定当期年化)/gold(黄金)。金额 ≥1000 元;整份成交。" +
				"股票按 ask 单边价成交,当月买入 T+1 冻结不可当月卖出,熔断月暂停交易(见上下文市场现价行)。",
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
			Description: "卖出持仓:金融资产按份额(units≥1);房产/商铺整售(units=1)。股票按 bid 单边价成交(与买价有阶段价差),佣金0.025%最低5元;当月买入的股票 T+1 冻结不可卖,熔断月暂停交易;黄金手续费0.5%;房产增值税5%+中介2%(满5年唯一免增值税),卖房先偿房贷。",
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
					"district":      strSchema(districtIDsHintDesc()),
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
					"kind":       strSchema("consumer|credit|business"),
					"amount_cny": intSchema(1, "金额(credit 必须是 50000/100000/200000 之一)"),
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
					"loan_id":    strSchema("贷款 id,如 L3"),
					"amount_cny": intSchema(1, "还款金额(元)"),
				},
				"required": []string{"loan_id", "amount_cny"},
			},
		},
		{
			Name: ToolStartSide,
			Description: "启动副业:delivery(无门槛)/content(认知≥2)/freelance(认知≥3)/tutoring(认知≥4);" +
				"月入约 2000–6000 元,月耗精力 2。可选定价档位 tier(缺省中价):同品类多经营者时按客群份额切分收入。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"kind": strSchema("delivery|content|tutoring|freelance"),
					"tier": map[string]any{
						"type":        "integer",
						"enum":        []int{0, 1, 2},
						"description": "定价档(可选,缺省 0):0 中价(客群30%,×1.0)/1 低价(客群50%,×0.75)/2 高价(客群20%,×1.25;家教/自由接单需认知≥门槛+1;跑腿高价月结额外精力-1;内容高价且认知<3 收入折半)",
					},
				},
				"required": []string{"kind"},
			},
		},
		{
			Name: ToolStopSide,
			Description: "停掉副业(无残值,精力释放)。",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name: ToolSetSidePrice,
			Description: "副业改价(耗 1 次动作预算,每月限 1 次):份额 = 我的客群权重 ÷ 同品类全部经营者权重和" +
				"(低50/中30/高20)。高价收入×1.25 但份额看对手;集体低价 = 集体受损(定价战囚徒困境)。当前竞争简况见「副业定价市场」段。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"tier": map[string]any{
						"type":        "integer",
						"enum":        []int{0, 1, 2},
						"description": "0 中价/1 低价/2 高价",
					},
				},
				"required": []string{"tier"},
			},
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
					"district": strSchema(districtIDsHintDesc() + "(不可与当前相同)"),
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
			Description: "说话(每月 area+private 合计最多 2 次):像真人聊天,谈行情/吐槽生活/分享买卖心得;不要复述工具参数。scope=area(默认) 同城区公开放话;scope=private 对 target_seat 指定居民耳语(仅对方可见)。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"scope":            strSchema(`"area"(默认)|"private"`),
					"target_seat":      intSchema(0, "private 时必填:目标座位号"),
					"text":             map[string]any{"type": "string", "minLength": 1, "maxLength": 100, "description": "发言(≤100字)"},
					"internal_thought": map[string]any{"type": "string", "maxLength": 200, "description": "内心独白(仅本人/观战者可见,仅 area 生效)"},
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
					"kind":       strSchema("consumer|credit|business"),
					"amount_cny": intSchema(1, "申请金额(元)"),
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
					"loan_id":    strSchema("房贷 id,如 L3"),
					"amount_cny": intSchema(0, "还款金额(元);0 或 ≥余额=全额还清"),
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
	// P1-4(§财商流P1-4 §7):商业保险三工具(Schema 逐字照抄契约 §7.1–§7.3)。
	base = append(base, []llmtypes.ToolDef{
		{
			Name:        ToolBuyInsurance,
			Description: "购买商业保险(每人每险种限 1 张有效保单)。保险不产生收益,只转移风险:重疾确诊一次性赔付,百万医疗报销住院费 90%,寿险/意外险身故时赔付给遗产继承人。年保费按月扣缴,现金断缴 1 个月宽限期后保单失效。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"kind": map[string]any{
						"type":        "string",
						"enum":        []string{"critical_illness", "medical_million", "term_life", "accident"},
						"description": "险种:critical_illness=重疾险 / medical_million=百万医疗险 / term_life=定期寿险 / accident=意外险",
					},
				},
				"required": []string{"kind"},
			},
		},
		{
			Name:        ToolCancelInsurance,
			Description: "退保。消费型保险零现金价值:不退还已缴保费,次月起停止扣缴,保障立即终止。退保前请确认已有替代保障。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"kind": map[string]any{
						"type": "string",
						"enum": []string{"critical_illness", "medical_million", "term_life", "accident"},
					},
				},
				"required": []string{"kind"},
			},
		},
		{
			Name:        ToolGetInsuranceStatus,
			Description: "查询本人全部保单状态:险种/年保费/月缴/保额/已缴月数/状态(有效/等待期/宽限期/失效)/累计赔付,以及未投保的险种与当前报价。无副作用,不消耗动作预算。",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
	}...)
	// 追加 P2 交易工具(挂牌/议价/借贷/拍卖/信息)。
	base = append(base, TradeToolDefinitions()...)
	// 追加 §CityHuman重构 感知与行动工具(see/hear/smell/move)。
	base = append(base, SenseToolDefinitions()...)
	return base
}

// ToolNames 返回全部工具名(测试/lint 用)。
// P0 基础 17 + P1 央行/银行 5 + 明斯基/提前还款 2 + P1-2 经济循环 3 +
// P1-4 商业保险 3 + P2 交易 12 + 批次20 副业改价 1 = 43。
func ToolNames() []string {
	out := []string{
		ToolCheckState, ToolBuyAsset, ToolSellAsset, ToolBuyHouse, ToolTakeLoan,
		ToolRepayLoan, ToolStartSide, ToolStopSide, ToolSetSidePrice, ToolStudy, ToolSocialize,
		ToolRest, ToolWorkOvertime, ToolMoveDistrict, ToolConsume, ToolDonate,
		ToolSpeak, ToolSubmitMonth,
		ToolQueryCentralBank, ToolQueryBankingSystem, ToolApplyLoanWithCredit,
		ToolDepositSavings, ToolWithdrawSavings,
		ToolQueryMinsky, ToolEarlyRepay,
		ToolSetConsumption, ToolAnswerSurvey, ToolQueryEconomy,
		ToolBuyInsurance, ToolCancelInsurance, ToolGetInsuranceStatus,
		ToolSee, ToolHear, ToolSmell, ToolMove,
	}
	out = append(out, TradeToolNames()...)
	return out
}

// dispatchToolResult 是一次工具派发的结果。
type dispatchToolResult struct {
	Name  string
	Input string // 原始 input JSON
	Text  string // 人读结果
	IsErr bool
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
		return failOr(a.runner.StartSideBusiness(seat, getStr("kind"), int(getInt("tier"))), "副业已启动", res)
	case ToolStopSide:
		return failOr(a.runner.StopSideBusiness(seat), "副业已停止", res)
	case ToolSetSidePrice:
		return failOr(a.runner.SetSidePrice(seat, int(getInt("tier"))), "副业改价完成", res)
	case ToolStudy:
		return failOr(a.runner.Study(seat), "学习完成", res)
	case ToolSocialize:
		return failOr(a.runner.Socialize(seat), "社交完成", res)
	case ToolRest:
		return failOr(a.runner.Rest(seat), "休整完成", res)
	case ToolWorkOvertime:
		return failOr(a.runner.WorkOvertime(seat), "本月已加班", res)
	case ToolMoveDistrict:
		// §CityHuman重构:move_district 保留为 move 跨城区模式的兼容别名
		// (设计文档 1 §4.2;下个大版本删除)。
		return failOr(a.runner.Move(seat, getStr("district"), "bus"), "迁居完成", res)
	case ToolConsume:
		return failOr(a.runner.Consume(seat, getInt("amount_cny"), getStr("reason")), "消费完成", res)
	case ToolDonate:
		return failOr(a.runner.Donate(seat, getInt("amount_cny")), "捐赠完成", res)
	case ToolSpeak:
		if getStr("scope") == "private" {
			return failOr(a.runner.SpeakTo(seat, int(getInt("target_seat")), getStr("text")), "已私聊", res)
		}
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
	case ToolBuyInsurance:
		return failOr(a.runner.BuyInsurance(seat, getStr("kind")), "投保成功", res)
	case ToolCancelInsurance:
		return failOr(a.runner.CancelInsurance(seat, getStr("kind")), "退保成功", res)
	case ToolGetInsuranceStatus:
		s, err := a.runner.GetInsuranceStatus(seat)
		if err != nil {
			return fail(err)
		}
		return ok(s)
	default:
		// §CityHuman重构 感知与行动工具路由(tools_sense.go)。
		for _, tn := range senseToolNames {
			if tn == name {
				return a.dispatchSenseTool(name, inputJSON, getStr, getInt)
			}
		}
		// P2 交易工具路由到 DispatchTradeTool(独立文件,避免本文件过长)。
		for _, tn := range tradeToolNames {
			if tn == name {
				return DispatchTradeTool(a.runner, seat, name, inputJSON)
			}
		}
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
