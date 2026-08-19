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
	b.WriteString("- 全押筹码 < 加注增量不算加注,只算跟注。\n\n")

	// [CurrentState] 当前状态段
	b.WriteString("[CurrentState]\n")
	b.WriteString(fmt.Sprintf("手牌 #%d,阶段: %s,你的位置: %s。\n", ctx.HandNumber, ctx.Street, ctx.Position))
	b.WriteString(fmt.Sprintf("你的底牌: %d,%d\n", ctx.MyHole[0], ctx.MyHole[1]))
	b.WriteString(fmt.Sprintf("公共牌: %d 张: %v\n", ctx.CommunityLen, ctx.Community[:ctx.CommunityLen]))
	b.WriteString(fmt.Sprintf("底池: %d,当前最高注: %d,跟注所需: %d。\n",
		ctx.Pot, ctx.CurrentBet, ctx.CallAmount))
	b.WriteString(fmt.Sprintf("你的剩余筹码: ?,本轮已下注: ?(由服务端 GameContext 注入)\n\n"))

	// [MathHelpers] 数学信号段
	b.WriteString("[MathHelpers]\n")
	b.WriteString(fmt.Sprintf("牌力 Hand Strength: %.3f (蒙特卡洛 1000 次抽样)\n", ctx.HandStrength))
	b.WriteString(fmt.Sprintf("底池赔率: 跟注 %d 赢得 %d → required_equity %.3f\n",
		ctx.CallAmount, ctx.Pot, ctx.RequiredEquity))
	b.WriteString(fmt.Sprintf("建议虚张频率: %.2f (按对手弃牌率反推)\n\n", ctx.BluffHint))

	// [StyleGuide] 风格指南段
	b.WriteString("[StyleGuide]\n")
	b.WriteString("- 使用 poker_action 工具(必填 internal_thought,描述你的真实思考)。\n")
	b.WriteString("- poker_chat 是可选公屏发言,每手牌最多 2 次。\n")
	b.WriteString("- fold/call/raise 决策基于牌力 vs required_equity:\n")
	b.WriteString("  * 牌力 > required_equity + 5% → 跟注/加注\n")
	b.WriteString("  * 牌力 < required_equity - 5% → 弃牌\n")
	b.WriteString("  * 介于两者之间 → 考虑位置与虚张频率\n")
	b.WriteString("- 注意: raise amount 必须 ≥ 当前最高注 + 最小加注增量。\n")

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
	return fmt.Sprintf("【我的底牌】%d, %d\n\n", ctx.MyHole[0], ctx.MyHole[1])
}

func buildCommunityCardsBlock(ctx *GameContextForAgent) string {
	if ctx.CommunityLen == 0 {
		return "【公共牌】未亮\n\n"
	}
	return fmt.Sprintf("【公共牌】%d 张: %v\n\n", ctx.CommunityLen, ctx.Community[:ctx.CommunityLen])
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

func buildReputationBlock(mem *Memory) string {
	return fmt.Sprintf("【玩家画像】v1.0 简化:无 Profile 数据\n\n")
}

func buildMemoryMDBlock(mem *Memory) string {
	summary := mem.GetLastDecisionSummary()
	if summary == "" {
		return "【持久化记忆】暂无\n\n"
	}
	return fmt.Sprintf("【持久化记忆】最近决策: %s\n\n", summary)
}

func buildRecentHandHistoryBlock(mem *Memory) string {
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
	return fmt.Sprintf("【筹码】剩余 %d (v1.0 不注入金币余额)\n\n", ctx.MyStack)
}

// ModelName 返回模型展示名（占位,实际从 registry 注入）。
func (ctx *GameContextForAgent) ModelName() string {
	if ctx.ModelNameField != "" {
		return ctx.ModelNameField
	}
	return "德州扑克Bot"
}