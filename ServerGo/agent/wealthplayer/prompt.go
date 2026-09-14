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
- 人生事件不可控:结婚/生育/疾病/失业都会发生,留足应急现金(建议 3–6 个月支出)。`

	seg5 := `【第 5 段 · 输出纪律】
1. 每月先在内部想清楚:本月现金流是否健康?市场处于周期哪个位置?我的目标推进到哪了?
2. 每月最多 3 个动作工具 + 1 次 speak,然后必须调用 submit_month 结束本月;不调用也会被系统强制结束。
3. speak 的内容 ≤100 字,像真人在群里聊天:可以聊行情、吐槽生活、分享买卖心得;不要复述工具参数。
4. 不要每 3 个动作都全用满——没有好机会时,攒钱、休息、学习也是决策。
5. 一切金额单位是人民币元。你的决策会被记录在财富流水账中,终局会生成你的人生报告。`

	return []llmtypes.SystemBlock{
		{Type: "text", Text: seg1},
		{Type: "text", Text: seg2},
		{Type: "text", Text: seg3},
		{Type: "text", Text: seg4},
		{Type: "text", Text: seg5},
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
	fmt.Fprintf(&b, " ｜ 本月剩余动作次数 %d\n\n", me.ActionBudget)

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
	b.WriteString("请决定本月怎么做(≤3 个动作 + 可选 1 次 speak),然后调用 submit_month。")
	return b.String()
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
