// Package virtual_city — agent_behavior.go: 批次62 行为金融/泡沫上下文小节。
//
// 唯一事实来源:lag_docs/虚拟城市/虚拟城市行为金融与投资心理机制设计.md §3/§4
// + 《虚拟城市社交资本与信用体系设计.md》§7/§11。
//
// ⚠ **教学纪律(最高优先级)**:认知检定**未通过**的偏差,其名称与解释
// **绝不可**进入 Agent 上下文 —— 否则 Agent 会"知道自己踩了什么坑",
// 偏差教学(§4.1「未通过时不显示偏差名称」)彻底失效。
package virtual_city

import (
	"fmt"
	"strings"
)

// behaviorBriefLocked 构造本人行为偏差小节(锁内纯读;≤400B)。
//
// 只列出**已识破**(认知检定通过)的偏差;未识破的偏差一律不出现。
func behaviorBriefLocked(p *Player) string {
	if p == nil {
		return ""
	}
	var parts []string
	for _, code := range p.BiasProfile {
		def, ok := biasByCode(code)
		if !ok {
			continue
		}
		mul, detected := applyBias(p, def)
		if !detected {
			// 未识破 ⇒ 不暴露偏差名(教学闭环)。
			continue
		}
		if mul == 1 {
			// 免疫达标 / 深度反思回合免疫 ⇒ 该倾向本回合不生效,
			// 不应作为"正在影响你的倾向"展示。
			continue
		}
		parts = append(parts, fmt.Sprintf("「%s」%s%.2f(%s)",
			def.NameCN, biasEffectLabel(def.Effect), mul, def.Desc))
	}
	if len(parts) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("你已识破自己身上的行为倾向:")
	for i, s := range parts {
		if i > 0 {
			b.WriteString(";")
		}
		b.WriteString(s)
	}
	b.WriteString("。可用 reflect(op=reflect) 消耗认知主动反思(轻度 −1 / 深度 −2 / 复盘预习 −1,每月 1 次)。")
	return b.String()
}

// herdBriefLocked 构造羊群效应小节(锁内纯读;≤200B)。
//
// 只含方向与人数,绝不含金额(§3.4.1:金额属于私有信息)。
// 同时给出**当前周期的跟风基准概率**(§3.4.2)—— 让 Agent 知道"周围人有多大概率
// 会跟风",这是社交压力的真实来源(繁荣期 80% / 萧条期 70% / 复苏期 50%)。
func herdBriefLocked(w *World) string {
	if w == nil {
		return ""
	}
	dir := w.marketMajorityDirection()
	if dir == MajorityFlat {
		return ""
	}
	var buy, sell int
	for _, s := range w.TurnSummaries {
		buy += s.Buy
		sell += s.Sell
	}
	phase := PhaseRecovery
	if w.Market != nil {
		phase = w.Market.CyclePhase
	}
	return fmt.Sprintf("最近几回合多数人方向:%s(买入 %d 人 / 卖出 %d 人)。当前周期约 %.0f%% 的居民会跟随多数方向(繁荣期最热)。跟风要多付滑点;逆势且获利 ⇒ 认知 +2。",
		majorityCN(dir), buy, sell, herdFollowProbability(phase)*100)
}

// bubbleBriefLocked 构造泡沫/集体杠杆小节(锁内纯读;≤220B)。
func bubbleBriefLocked(w *World) string {
	if w == nil {
		return ""
	}
	state := bubbleStateCN(w.BubbleState)
	var b strings.Builder
	fmt.Fprintf(&b, "市场状态:%s。全场平均杠杆率 %.0f%%(>60%% 触发信贷过热),资产换手率 %.0f%%(>30%% 投机过热),现金占比 %.0f%%(<10%% 流动性紧张)。",
		state, w.BubbleLeverage*100, w.BubbleTurnover*100, w.BubbleCash*100)
	if w.BubbleState == BubbleOverheat {
		b.WriteString("当前资产涨得比基本面快,加杠杆的风险在累积。")
	} else if w.BubbleState == BubbleBurst || w.BubbleState == BubbleRecession {
		b.WriteString("泡沫刚破裂/处于衰退期,资产在下跌,优先保现金。")
	}
	return b.String()
}

// liquidityBriefLocked 构造资产流动性价差小节(锁内纯读;≤200B)。
func liquidityBriefLocked(w *World) string {
	if w == nil || w.Market == nil {
		return ""
	}
	m := liquidityPhaseMultiplier(w.Market.CyclePhase)
	return fmt.Sprintf("买卖价差(流动性成本):股票 %.1f%% / 债券 %.1f%% / 黄金 %.1f%% / 房产 %.1f%% / 企业股权 %.1f%%(周期系数 ×%.1f)。买卖一次即吃掉该价差。",
		liquiditySpreadTable[ClassStock]*m*100,
		liquiditySpreadTable[ClassBond]*m*100,
		liquiditySpreadTable[ClassGold]*m*100,
		liquiditySpreadTable[ClassHouse]*m*100,
		liquiditySpreadTable[ClassEquity]*m*100,
		m)
}
