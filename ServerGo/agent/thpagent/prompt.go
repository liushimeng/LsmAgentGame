// Package thpagent — prompt.go: 德州扑克 Bot Prompt 构建器（2026-08-19）。
//
// 按 [德州扑克Agent设计.md] §5 设计：
//   - System Prompt 5 段：身份/规则/状态/数学/风格
//   - User Prompt 13 块：按优先级 Critical/Important/Optional 分类
//   - AssembleWithBudget 强制预算省略时留可观测标记（§20260812-04 U2）
package thpagent

import (
	"fmt"
	"strings"
)

// SystemPromptMaxRunes system prompt 软上限（rune 数）。
const SystemPromptMaxRunes = 12000

// UserPromptMaxRunes user prompt 软上限（rune 数）。
const UserPromptMaxRunes = 6000

// BuildSystemPrompt 构造 system prompt（5 段拼接）。
//
// 参数：
//   - ctx: 玩家当前对局上下文
//   - mem: Bot 短期记忆
func BuildSystemPrompt(ctx *GameContextForAgent, mem *Memory) string {
	var b strings.Builder

	// [Identity] 身份段
	b.WriteString("[Identity]\n")
	b.WriteString(fmt.Sprintf("你是 %s,坐在德州扑克房间 %s 的 %d 号位。\n",
		ctx.ModelName(), ctx.RoomID, ctx.MySeat+1))
	b.WriteString(fmt.Sprintf("你面对 %d 个对手。当前手牌号: %d,阶段: %s。\n\n",
		ctx.OpponentsCount, ctx.HandNumber, ctx.Street))

	// [GameRules] 规则段（精简版,避免 system prompt 膨胀）
	b.WriteString("[GameRules]\n")
	b.WriteString("6-max No-Limit Hold'em 规则:\n")
	b.WriteString("- 每人 2 张底牌,5 张公共牌(翻牌3+转牌1+河牌1),7 张选 5 张最优组合。\n")
	b.WriteString("- 牌型从大到小: 皇家同花顺 > 同花顺 > 四条 > 葫芦 > 同花 > 顺子 > 三条 > 两对 > 一对 > 高牌。\n")
	b.WriteString("- 翻前 UTG 先动,翻后庄家后第一存活者先动。\n")
	b.WriteString("- 跟注所需最低胜率 = callAmount / (pot + callAmount)。\n")
	b.WriteString("- 全押筹码 < 加注增量不算加注,只算跟注。\n")
	// §R22-P1: 明确 check 合法性规则 —— LLM 曾在有 bet-to-call 时输出 check,
	// 被 engine 拒绝(30007)后 bot driver 停止驱动,游戏永久卡死。
	b.WriteString("- **check(过牌)仅当 callAmount=0 时合法**;若 callAmount>0 说明有人下注跟注未到,必须 call/fold/raise,绝对不可 check。\n\n")

	// [CurrentState] 当前状态段
	b.WriteString("[CurrentState]\n")
	b.WriteString(fmt.Sprintf("手牌 #%d,阶段: %s,你的位置: %s。\n", ctx.HandNumber, ctx.Street, ctx.Position))
	// 2026-08-20 §B3: 底牌/公共牌用 CardString 渲染（A♠/10♥）,裸 int 对 LLM 无语义
	b.WriteString(fmt.Sprintf("你的底牌: %s\n", CardsString(ctx.MyHole[:])))
	if ctx.CommunityLen > 0 {
		b.WriteString(fmt.Sprintf("公共牌: %d 张: %s\n", ctx.CommunityLen, CardsString(ctx.Community[:ctx.CommunityLen])))
	} else {
		b.WriteString("公共牌: 未亮\n")
	}
	b.WriteString(fmt.Sprintf("底池: %d,当前最高注: %d,跟注所需: %d。\n",
		ctx.Pot, ctx.CurrentBet, ctx.CallAmount))
	// 2026-08-20 §B3: 此前是字面「?」占位,从未被替换
	b.WriteString(fmt.Sprintf("你的剩余筹码: %d,本轮已下注: %d,大盲: %d。\n\n",
		ctx.MyStack, ctx.MyRoundCommitted, ctx.BigBlind))

	// [MathHelpers] 数学信号段
	b.WriteString("[MathHelpers]\n")
	b.WriteString(fmt.Sprintf("牌力 Hand Strength: %.3f (蒙特卡洛 1000 次抽样)\n", ctx.HandStrength))
	b.WriteString(fmt.Sprintf("底池赔率: 跟注 %d 赢得 %d → required_equity %.3f\n",
		ctx.CallAmount, ctx.Pot, ctx.RequiredEquity))
	b.WriteString(fmt.Sprintf("建议虚张频率: %.2f (按对手弃牌率反推)\n", ctx.BluffHint))
	// 2026-08-20 §B3: 明确最小加注规则 —— LLM 此前只能猜,amount 经常被引擎拒绝
	b.WriteString(fmt.Sprintf("最小加注规则: raise 的 amount 是「目标总注额」,必须 ≥ 当前最高注 %d + 最小加注增量 %d = %d;"+
		"低于此值服务端会自动抬到该最小值,超过你的可动用筹码会自动改 allin。\n\n",
		ctx.CurrentBet, ctx.MinRaise, ctx.CurrentBet+ctx.MinRaise))

	// [StyleGuide] 风格指南段
	b.WriteString("[StyleGuide]\n")
	b.WriteString("- 使用 poker_action 工具(必填 internal_thought,描述你的真实思考)。\n")
	// 2026-08-25 §20260825-03 发言升级:每手 ≤4 次 + 间隔 ≥12s;发言时机指引强化。
	b.WriteString("- poker_chat 公屏发言每手牌最多 4 次(相邻 ≥12s),鼓励积极使用:像真人牌手一样,在大注/被压制/被点名/关键手时说一两句(可与 poker_action 同次响应给出)。\n")
	b.WriteString("- 发言可心理战(示弱/造势/反讽),但绝不直接泄露当前底牌;被点名或挑衅时应优先回应。\n")
	b.WriteString("- 摊牌后的 win/loss 关键手,鼓励用一句情绪化短评回应结果。\n")
	b.WriteString("- fold/call/raise 决策基于牌力 vs required_equity:\n")
	b.WriteString("  * 牌力 > required_equity + 5% → 跟注/加注\n")
	b.WriteString("  * 牌力 < required_equity - 5% → 弃牌\n")
	b.WriteString("  * 介于两者之间 → 考虑位置与虚张频率\n")
	b.WriteString(fmt.Sprintf("- 注意: raise 的 amount 是目标总注额,必须 ≥ 当前最高注 + 最小加注增量(当前为 %d)。\n",
		ctx.CurrentBet+ctx.MinRaise))
	// §R22-P1: 再次强调 check 合法性(StyleGuide 是 LLM 最后读的段,强化记忆)
	if ctx.CallAmount > 0 {
		b.WriteString(fmt.Sprintf("- **⚠️ 当前 callAmount=%d > 0,说明有人下注你必须跟注/加注/弃牌,绝对不可 check!**\n",
			ctx.CallAmount))
	} else {
		b.WriteString("- 当前 callAmount=0,你可以 check(过牌)或 bet(下注)。\n")
	}

	result := b.String()
	if len([]rune(result)) > SystemPromptMaxRunes {
		// 截断到上限(保留前缀)
		runes := []rune(result)
		result = string(runes[:SystemPromptMaxRunes])
	}
	return result
}

// BuildUserPrompt 构造 user prompt（13 块拼接, 优先级降级）。
//
// 优先级分类：
//   - Critical: BotIdentity / CurrentHand / MyHand / CommunityCards / ActionHistory（永不丢）
//   - Important: PotOdds / Opponents / HandStrength / BluffHint（预算紧时按需丢）
//   - Optional: Reputation / MemoryMD / RecentHandHistory / Wallet（优先丢）
//
// 预算省略时必须留可观测标记 [因上下文预算省略 N 块: ...]。
func BuildUserPrompt(ctx *GameContextForAgent, mem *Memory) string {
	var b strings.Builder
	b.WriteString("请基于当前状态做出决策。\n\n")

	// Critical 段
	b.WriteString(buildBotIdentityBlock(ctx))
	b.WriteString(buildCurrentHandBlock(ctx))
	b.WriteString(buildMyHandBlock(ctx))
	b.WriteString(buildCommunityCardsBlock(ctx))
	b.WriteString(buildActionHistoryBlock(ctx))

	// Important 段（按预算决定保留）
	remainingBudget := UserPromptMaxRunes - len([]rune(b.String()))
	importantBlocks := []string{
		buildPotOddsBlock(ctx),
		buildOpponentsBlock(ctx),
		buildHandStrengthBlock(ctx),
		buildBluffHintBlock(ctx),
		// 2026-08-23 §3.1「牌桌闲聊(增量)」段:注入 500K 共享队列中该 bot
		// 尚未消费的公屏消息,驱动 bot 回应人类/其他 bot 发言。
		buildChatWindowBlock(ctx),
	}
	droppedBlocks := 0
	for _, blk := range importantBlocks {
		blkRunes := len([]rune(blk))
		if remainingBudget-blkRunes < 500 {
			droppedBlocks++
			continue
		}
		b.WriteString(blk)
		remainingBudget -= blkRunes
	}

	// Optional 段（低优先级,先丢）
	optionalBlocks := []string{
		buildReputationBlock(mem),
		buildMemoryMDBlock(mem),
		buildRecentHandHistoryBlock(mem),
		buildWalletBlock(ctx),
	}
	for _, blk := range optionalBlocks {
		// §3.4 Tier400(上下文超限兜底):Optional 段整体清空,仅保留 Critical/Important。
		if ctx.suppressOptionalBlocks {
			continue
		}
		if blk == "" {
			continue // 无数据块自然省略,不计入 droppedBlocks(§B3 假占位文案禁令)
		}
		blkRunes := len([]rune(blk))
		if remainingBudget-blkRunes < 300 {
			droppedBlocks++
			continue
		}
		b.WriteString(blk)
		remainingBudget -= blkRunes
	}

	if droppedBlocks > 0 {
		b.WriteString(fmt.Sprintf("\n[因上下文预算省略 %d 块: 详情见服务端日志]\n", droppedBlocks))
	}

	return b.String()
}

// 13 块 block 构建函数

func buildBotIdentityBlock(ctx *GameContextForAgent) string {
	return fmt.Sprintf("【身份】Bot 座位 #%d,模型 %s\n\n", ctx.MySeat+1, ctx.ModelName())
}

func buildCurrentHandBlock(ctx *GameContextForAgent) string {
	return fmt.Sprintf("【当前手牌】#%d,阶段 %s\n\n", ctx.HandNumber, ctx.Street)
}

func buildMyHandBlock(ctx *GameContextForAgent) string {
	return fmt.Sprintf("【我的底牌】%s\n\n", CardsString(ctx.MyHole[:]))
}

func buildCommunityCardsBlock(ctx *GameContextForAgent) string {
	if ctx.CommunityLen == 0 {
		return "【公共牌】未亮\n\n"
	}
	return fmt.Sprintf("【公共牌】%d 张: %s\n\n", ctx.CommunityLen, CardsString(ctx.Community[:ctx.CommunityLen]))
}

func buildPotOddsBlock(ctx *GameContextForAgent) string {
	return fmt.Sprintf("【底池赔率】底池 %d,跟注 %d → 赔率 %.3f,需胜率 %.3f\n\n",
		ctx.Pot, ctx.CallAmount, ctx.RequiredEquity, ctx.RequiredEquity)
}

func buildOpponentsBlock(ctx *GameContextForAgent) string {
	return fmt.Sprintf("【对手数】%d 个存活对手\n\n", ctx.OpponentsCount)
}

func buildHandStrengthBlock(ctx *GameContextForAgent) string {
	return fmt.Sprintf("【牌力】Hand Strength %.3f (蒙特卡洛 1000 次)\n\n", ctx.HandStrength)
}

func buildActionHistoryBlock(ctx *GameContextForAgent) string {
	if ctx.ActionHistory == "" {
		return "【本手动作】暂无\n\n"
	}
	return fmt.Sprintf("【本手动作】%s\n\n", ctx.ActionHistory)
}

func buildBluffHintBlock(ctx *GameContextForAgent) string {
	return fmt.Sprintf("【虚张建议】Bluff 频率 %.2f\n\n", ctx.BluffHint)
}

// buildChatWindowBlock 渲染「牌桌闲聊(增量)」段(§3.1)。ChatWindow 为空
// (本轮无新消息)时返回空串,由预算机制自然省略 —— 不喂占位符给 LLM。
func buildChatWindowBlock(ctx *GameContextForAgent) string {
	if ctx.ChatWindow == "" {
		return ""
	}
	return fmt.Sprintf("【牌桌闲聊(增量)】以下是自你上次决策后牌桌上新增的公屏发言:\n%s(你可以在 poker_chat 中回应他人,但不要泄露自己的底牌)\n\n",
		ctx.ChatWindow)
}

// buildReputationBlock 基于 Memory.AllOpponentStats 的真实画像（2026-08-20 §B3）。
// 此前固定返回「v1.0 简化:无 Profile 数据」假占位文案（§130 伪装模式）。
// 无任何统计数据时返回空字符串 —— 让 BuildUserPrompt 的预算机制自然省略,
// 而不是给 LLM 喂一句没有信息量的占位符。
func buildReputationBlock(mem *Memory) string {
	stats := mem.AllOpponentStats()
	if len(stats) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("【玩家画像】(按座位)\n")
	wrote := false
	for seat := 0; seat < 6; seat++ {
		for _, st := range stats {
			if st.Seat != seat || st.HandsPlayed <= 0 {
				continue
			}
			total := st.TotalFold + st.TotalCall + st.TotalRaise + st.TotalAllIn
			raiseRate := 0.0
			if total > 0 {
				raiseRate = float64(st.TotalRaise) / float64(total)
			}
			b.WriteString(fmt.Sprintf("  - 座位%d: 共打 %d 手,弃牌率 %.0f%%,加注率 %.0f%%,全押 %d 次,净盈亏 %+d\n",
				seat+1, st.HandsPlayed, st.FoldRate*100, raiseRate*100, st.TotalAllIn, st.NetChips))
			wrote = true
		}
	}
	if !wrote {
		return ""
	}
	b.WriteString("\n")
	return b.String()
}

func buildMemoryMDBlock(mem *Memory) string {
	summary := mem.GetLastDecisionSummary()
	if summary == "" {
		return "【持久化记忆】暂无\n\n"
	}
	return fmt.Sprintf("【持久化记忆】最近决策: %s\n\n", summary)
}

func buildRecentHandHistoryBlock(mem *Memory) string {
	// 2026-08-24 §3.4 移植 memory_compact:若 LastCompactSummary 存在,优先
	// 渲染 4 段结构化摘要(LLM 压缩产物);空则回退 RawHandHistory 渲染。
	if summary := mem.LastCompactSummary(); strings.TrimSpace(summary) != "" {
		return "【本局经验摘要(LLM 压缩)】\n" + summary + "\n\n"
	}
	hands := mem.RecentHandsSnapshot()
	if len(hands) == 0 {
		return "【最近手牌回顾】暂无\n\n"
	}
	var b strings.Builder
	b.WriteString("【最近手牌回顾】\n")
	for _, h := range hands {
		b.WriteString(fmt.Sprintf("  - 手牌 #%d: 净盈亏 %d\n", h.HandNumber, h.NetChipDelta))
	}
	b.WriteString("\n")
	return b.String()
}

func buildWalletBlock(ctx *GameContextForAgent) string {
	if ctx.RakeRatePct == 0 {
		// 缺省值:不显式提抽水(向后兼容旧 GameContext)
		return fmt.Sprintf("【筹码】剩余 %d (v1.0 不注入金币余额)\n\n", ctx.MyStack)
	}
	return fmt.Sprintf("【筹码】剩余 %d | 房间总金币 %d | 当前档位 %s | 赢家抽水 %d%%\n\n",
		ctx.MyStack, ctx.RoomTotalCoin, ctx.EconTier, ctx.RakeRatePct)
}

// ModelName 返回模型展示名（占位,实际从 registry 注入）。
func (ctx *GameContextForAgent) ModelName() string {
	if ctx.ModelNameField != "" {
		return ctx.ModelNameField
	}
	return "德州扑克Bot"
}