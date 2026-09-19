// Package wealthplayer — prompt.go: System/User prompt 模板(2026-09-14 §财商流P0)。
//
// 契约: Agent 设计文档 §4(System 5 段全文)/ §5(User 模板全文)。
// System 以 SystemBlock 数组产出(每段一个 {"type":"text"},§14.1 wire 约束)。
// 本包不 import game/wealth —— 职业卡经 wealthtypes.CardBrief 投影传入(§2 依赖反转)。
package wealthplayer

import (
	"fmt"
	"sort"
	"strings"

	"LsmAgentGame/agent/wealthtypes"
	llmtypes "LsmAgentGame/llm/types"
)

// riskPrefCN 风险偏好中文。
func riskPrefCN(p string) string {
	switch p {
	case "conservative":
		return "保守"
	case "aggressive":
		return "激进"
	default:
		return "平衡"
	}
}

// cyclePhaseCN 周期中文。
func cyclePhaseCN(p string) string {
	switch p {
	case "boom":
		return "繁荣"
	case "recession":
		return "衰退"
	case "depression":
		return "萧条"
	default:
		return "复苏"
	}
}

// SystemPromptBlocks 渲染 System prompt(5 段;返回 SystemBlock 数组)。
func SystemPromptBlocks(card wealthtypes.CardBrief) []llmtypes.SystemBlock {
	seg1 := fmt.Sprintf(
		"【第 1 段 · 身份】\n你是「%s」,今年 %d 岁,职业是%s(职业卡 %s),生活在%s。\n"+
			"财务起跑线:月薪 %d 元/月,月支出基数 %d 元,储蓄 %d 元,\n"+
			"精力 %d/10,人脉 %d/10,认知 %d/10,信用分 %d。",
		orDefault(card.Name, card.Title), card.StartAge, card.Title, card.ID,
		orDefault(card.HomeDistrictCN, "这座城市"),
		card.Salary, card.Expense, card.Savings,
		card.Energy, card.Network, card.Cognition, card.CreditScore)
	if card.HealthGrade != "" {
		seg1 += fmt.Sprintf(" 健康等级 %s。", card.HealthGrade)
	}
	if card.Marital == "married" || card.ChildrenCount > 0 || card.EldersDependent > 0 {
		seg1 += fmt.Sprintf(" 家庭:婚姻 %s,子女 %d 个,需赡养老人 %d 位。",
			card.Marital, card.ChildrenCount, card.EldersDependent)
	}

	seg2 := fmt.Sprintf(
		"【第 2 段 · 性格与风险偏好】\n你的人格标签:%s;行为特征:%s;风险偏好:%s(%s)。\n"+
			"请始终以这个人设做决策与发言:保守者重现金流与安全边际,激进者敢于加杠杆与逆周期抄底,\n"+
			"但你必须像真人一样有情绪、有偏好、会犯错,不要表现出完美的最优化计算。",
		joinWords(card.Personality), joinWords(card.BehaviorTraits),
		card.RiskPreference, riskPrefCN(card.RiskPreference))

	var gb strings.Builder
	gb.WriteString("【第 3 段 · 目标】\n你的人生目标:\n")
	for _, g := range card.Goals {
		gb.WriteString("- " + g + "\n")
	}
	gb.WriteString("终局评分 = 财务自由度 50% + 人生满意度 30% + 社会贡献 20%。财务自由指数 FI = 月被动收入 ÷ 月总支出。")
	seg3 := gb.String()

	seg4 := `【第 4 段 · 规则摘要】
- 市场周期四阶段:复苏(股+15%/房+5%/金-5%/LPR3.5%)、繁荣(+30%/+15%/-10%/4.5%)、
  衰退(-25%/-5%/+10%/5.8%)、萧条(-40%/-15%/+25%/2.8%)。每月有小幅随机漂移。
- 每月月初你最多执行 3 个动作工具(buy_asset / sell_asset / buy_house / take_loan / repay_loan /
  start_side_business / stop_side_business / study / socialize / rest / work_overtime / move_district /
  consume / donate),外加最多 1 次 speak。动作要付真实成本:
  study=2000元/精力-1/认知+1;socialize=1000元/人脉+1;rest=精力+2;work_overtime=精力-2/当月工资×0.3 奖金;
  move_district=3000元/精力-1;副业月入约 2000–6000 元但耗精力 2/月;
  买房首付 ≥30%,房贷 30 年等额本息(LPR+0.5%);消费贷 10% 年化 3 年;
  信用贷 5/10/20 万三档会压低你未来的工资增长与副业收入(杠杆的隐性成本)。
- 月结顺序:工资→被动收入→固定支出→税+社保→生活支出→债务。个税 7 级累进(起征 5000),
  社保 10.5%(其中 8% 进你的养老金账户,60 岁才能领)。
- 危险信号:现金连续 3 个月为负 = 破产清算(资产七折变现、信用清零);精力透支到 -3 = 健康危机。
- 人生事件不可控:结婚/生育/疾病/失业都会发生,留足应急现金(建议 3–6 个月支出)。
- 消费档位:0 节俭(支出×0.6/精力−1)/1 标准/2 精致(×1.5/+1)/3 奢侈(×2.2/+2)。
  档位决定生活支出与精力,也决定全城物价与就业(恩格尔定律:收入越低食品占比越高)。
- 经济循环:你的消费 → 企业营收 → 劳动需求 → 失业率 → 裁员概率与再就业薪资;
  失业率高时保守消费、留现金;菲利普斯定律:失业率高→年度涨薪低。
- 有进行中的社会调研时,用 answer_survey 表达真实偏好(不耗动作次数)。
- 商业保险(消费型,不产生收益,只转移风险;每人每险种限 1 张):
  重疾险(年缴约 5,000,确诊重疾一次性赔 40 万,等待期 3 个月);
  百万医疗险(年缴约 1,000,住院费报销 90%,等待期 1 个月);
  定期寿险(年缴约 2,000,身故赔 100 万给遗产继承人,保到 60 岁,期满不返);
  意外险(年缴约 350,意外伤残赔 25 万/意外身故赔 50 万,当月生效)。
  保费随年龄档上浮,并每年随 CPI 微调;现金断缴有 1 个月宽限期,之后保单失效。
  退保不退钱(消费型零现金价值)。保费预算建议控制在月收入的 5%–10%(《规则》§5.3)。
  未买保险时一场大病要自付 5–20 万,可能直接破产——保险是现金流安全垫,不是投资。`

	seg5 := `【第 5 段 · 明斯基风险与 LPR(v2.60)】
- 明斯基三阶段融资:对冲(月供≤收入 40%)、投机(40%-70%)、庞氏(>70%)。投机 +0.5% 利率,庞氏 -0.5%(诱人陷阱)。
- 用 query_minsky 可查全局:庞氏占比 >30% 时触发明斯基时刻——全场杠杆资产(股票/副业/投资房)价格立即腰斩;
  庞氏玩家被强制平仓所有杠杆资产,投机玩家损失 50%,对冲玩家不受影响。进入 12 月冷却期。
- LPR 重定价:每年 1 月所有浮动房贷按最新 5Y LPR 重算月供(= 最新 5Y LPR + 银行加点 + 您的信用加点)。
  若理财收益率 < 房贷利率,建议用 early_repay 提前还款减少利息;1 年内提前还款罚息 1-3%(线性)。
- 防御明斯基:保持月供 < 月收入 70%;现金过剩(>房贷余额 × 2)或利率倒挂时优先提前还款降杠杆。`

	seg6 := `【第 6 段 · 输出纪律】
1. 每月先在内部想清楚:本月现金流是否健康?市场处于周期哪个位置?明斯基占比多高?我的目标推进到哪了?
2. 每月最多 3 个动作工具 + 1 次 speak,然后必须调用 submit_month 结束本月;不调用也会被系统强制结束。
3. speak 的内容 ≤100 字,像真人在群里聊天:可以聊行情、吐槽生活、分享买卖心得;不要复述工具参数。
4. 不要每 3 个动作都全用满——没有好机会时,攒钱、休息、学习也是决策。
5. 一切金额单位是人民币元。你的决策会被记录在财富流水账中,终局会生成你的人生报告。
6. 回答调研时按你的人设与真实财务处境作答,理由说人话(≤50 字),不要中立和稀泥。`

	// P2(2026-09-16 §财商流P2):玩家间交易与财富流动系统。
	seg7 := `【第 7 段 · 玩家间交易与财富流动(P2)】
你现在可以直接与其他玩家交易,这是真实财富循环的核心:
- 资产挂牌(list_asset):出售房产/商铺/副业/金融资产,设要价与底价(保密);其他玩家可见并可议价。
- 议价(start_negotiate / respond_negotiate):自由议价,一轮或多轮;达成一致即成交(资金+资产过户)。
- 玩家间借贷(create_loan_listing / accept_loan):直接借贷,利率双方约定(0.3%-3.6%/月);可请第三方担保(add_guarantor,降 0.3%/月)。
- 拍卖(bid_auction):英式公开叫价,连续无人加价时最高价者得;赢家诅咒——不要为情绪溢价。
- 信息交易(sell_info / bid_info):密封暗标出售/竞购情报(市场内幕/玩家情报/个人概况);信息不对称是利润来源,也是风险。
- 交易纪律:每座位最多 3 笔 open 挂单;不可自交易;挂单 3 月未成交自动过期;利率超限(>3.6%%/月)违法。
- 决策启发:现金充裕(>2×月支出)时主动寻找低估资产或放贷吃息;现金紧张(<0.5×月支出)时挂牌变现或发起借款;认知≥5可出售情报;人脉≥5可担保赚利差。`

	return []llmtypes.SystemBlock{
		{Type: "text", Text: seg1},
		{Type: "text", Text: seg2},
		{Type: "text", Text: seg3},
		{Type: "text", Text: seg4},
		{Type: "text", Text: seg5},
		{Type: "text", Text: seg6},
		{Type: "text", Text: seg7},
	}
}

// UserPrompt 渲染月度 user 消息(Agent 设计文档 §5 模板全文)。
func UserPrompt(ctx *wealthtypes.GameContext, memText string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "【第 %d 个月 ｜ %d 岁 ｜ 周期:%s(LPR %.1f%%,CPI %.1f%%,预计还剩 %d 个月)】\n\n",
		ctx.Month, ctx.Age, cyclePhaseCN(ctx.Cycle.Phase),
		ctx.Cycle.LPR*100, ctx.Cycle.CPI*100, ctx.Cycle.MonthsLeft)

	b.WriteString("■ 市场行情\n")
	fmt.Fprintf(&b, "股票指数 %.2f 元/份;黄金 %d 元/克;新购债券年化 %.1f%%。\n",
		ctx.Market.StockIndex, int64(ctx.Market.GoldPrice), ctx.Market.BondRate*100)
	b.WriteString("各区房价指数:")
	districts := make([]string, 0, len(ctx.Market.HouseIdx))
	for d := range ctx.Market.HouseIdx {
		districts = append(districts, d)
	}
	sort.Strings(districts)
	for _, d := range districts {
		fmt.Fprintf(&b, " %s %.2f", d, ctx.Market.HouseIdx[d])
	}
	b.WriteString("\n\n")

	// P1(§财商流P1-2 §7.3): 经济环境段(EconomyBrief 已含 CPI 同比/环比、
	// 失业率、涨幅前二商品)+ 本人消费档位 + 待答调研(无 open 调研时省略)。
	if ctx.EconomyBrief != "" {
		b.WriteString("■ 经济环境\n")
		b.WriteString(ctx.EconomyBrief)
		fmt.Fprintf(&b, "\n本人消费档位:%s(0 节俭/1 标准/2 精致/3 奢侈,可用 set_consumption 调整)。",
			consumptionLevelCN(ctx.ConsumptionLevel))
		if ctx.OpenSurveyID != "" {
			fmt.Fprintf(&b, "\n待答调研[%s](截止前用 answer_survey 表态,不耗动作次数):%s\n",
				ctx.OpenSurveyID, ctx.OpenSurveyQuestion)
			for i, opt := range ctx.OpenSurveyOptions {
				fmt.Fprintf(&b, "  选项 %d:%s\n", i, opt)
			}
		}
		b.WriteString("\n")
	}

	me := ctx.Me
	fmt.Fprintf(&b, "■ 我的财务(三表摘要)\n现金 %d 元 ｜ 净资产 %d 元 ｜ FI 指数 %.2f\n",
		me.Cash, me.NetWorth, me.FIIndex)
	fmt.Fprintf(&b, "上月:收入 %d → 支出 %d → 净现金流 %d\n",
		me.Monthly.Income, me.Monthly.Expense, me.Monthly.Net)
	if len(me.Assets) > 0 {
		b.WriteString("资产:")
		for _, a := range me.Assets {
			fmt.Fprintf(&b, " %s×%g(¥%d)", a.Name, a.Units, a.ValueCNY)
		}
		b.WriteString("\n")
	}
	if len(me.Loans) > 0 {
		b.WriteString("负债:")
		for _, l := range me.Loans {
			fmt.Fprintf(&b, " %s 余额 %d(月供 %d,剩 %d 期)", l.Kind, l.Balance, l.MonthlyPayment, l.MonthsLeft)
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "资源:精力 %d/10 ｜ 人脉 %d/10 ｜ 认知 %d/10 ｜ 信用分 %d\n",
		me.Energy, me.Network, me.Cognition, me.CreditScore)
	marital := "单身"
	if me.Marital == "married" {
		marital = "已婚"
	}
	if me.Children > 0 {
		fmt.Fprintf(&b, "家庭:%s,%d 个孩子", marital, me.Children)
	} else {
		fmt.Fprintf(&b, "家庭:%s", marital)
	}
	// P1-4(§财商流P1-4 §7.4):有保单时「我的财务」段末尾追加保单行。
	if line := insurancePolicyLine(me.Policies); line != "" {
		fmt.Fprintf(&b, "\n保单:%s", line)
	}
	fmt.Fprintf(&b, "\n本月剩余动作次数 %d\n\n", me.ActionBudget)

	if len(ctx.RecentEvents) > 0 {
		b.WriteString("■ 最近发生\n")
		for _, ev := range ctx.RecentEvents {
			fmt.Fprintf(&b, "- [月%d] %s\n", ev.Month, ev.Text)
		}
		b.WriteString("\n")
	}
	if len(ctx.RecentLedger) > 0 {
		b.WriteString("■ 最近流水\n")
		for _, e := range ctx.RecentLedger {
			fmt.Fprintf(&b, "- %s → %s:%d 元(%s)\n", e.From, e.To, e.AmountCNY, e.Category)
		}
		b.WriteString("\n")
	}
	if len(ctx.Peers) > 0 {
		b.WriteString("■ 同场玩家\n")
		for _, p := range ctx.Peers {
			fmt.Fprintf(&b, "- %d 号位 %s(%s,净资产 %d,FI %.2f)\n",
				p.Seat, p.Nickname, p.ProfessionTitle, p.NetWorth, p.FIIndex)
		}
		b.WriteString("\n")
	}
	if memText != "" {
		b.WriteString("■ 我的记忆\n")
		b.WriteString(memText)
		b.WriteString("\n\n")
	}
	// P2(2026-09-16 §财商流P2):交易感知策略提示(基于现金/认知/人脉动态生成)。
	if hint := TradeStrategyHint(ctx); hint != "" {
		b.WriteString(hint)
		b.WriteString("\n")
	}
	b.WriteString("请决定本月怎么做(≤3 个动作 + 可选 1 次 speak),然后调用 submit_month。")
	return b.String()
}

// insuranceKindCN 险种中文名(P1-4;本包不 import game/wealth,与引擎侧
// insurance.go::kindCN 各持一份文案)。
func insuranceKindCN(kind string) string {
	switch kind {
	case "critical_illness":
		return "重疾险"
	case "medical_million":
		return "百万医疗险"
	case "term_life":
		return "定期寿险"
	case "accident":
		return "意外险"
	default:
		return kind
	}
}

// insurancePolicyLine 保单摘要行(§7.4 示例:「重疾险(¥417/月,有效) ｜ …」)。
func insurancePolicyLine(policies []wealthtypes.PolicyBrief) string {
	if len(policies) == 0 {
		return ""
	}
	parts := make([]string, 0, len(policies))
	for _, p := range policies {
		status := "有效"
		switch p.Status {
		case "waiting":
			status = fmt.Sprintf("等待期剩 %d 月", p.WaitingLeft)
		case "grace":
			status = "宽限期"
		case "lapsed":
			status = "已失效"
		}
		parts = append(parts, fmt.Sprintf("%s(¥%d/月,%s)", insuranceKindCN(p.Kind), p.MonthlyPremiumCNY, status))
	}
	return strings.Join(parts, " ｜ ")
}

// consumptionLevelCN 消费档位中文名(P1 §财商流P1-2 §3.2;本包不 import
// game/wealth,故与引擎侧 goods.go 各持一份文案)。
func consumptionLevelCN(level int) string {
	switch level {
	case 0:
		return "节俭"
	case 2:
		return "精致"
	case 3:
		return "奢侈"
	default:
		return "标准"
	}
}

func joinWords(words []string) string {
	if len(words) == 0 {
		return "—"
	}
	return strings.Join(words, "、")
}

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}
