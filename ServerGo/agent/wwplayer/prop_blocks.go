// Package agent — agent_prop.go: Agent 侧道具系统集成。
//
// 本文件负责：
//   1. 在 BuildTools 中添加 use_prop 工具（白天发言阶段暴露）。
//   2. 在 BuildUserPrompt 中注入道具系统提示 + 长期金币目标增强。
//   3. 在 wwtypes.GameContext 中添加道具相关字段（PropInjectText / PropCooldown 等）。
//   4. 在 30% 对局中为 2 只狼人 Agent 注入初始身份互知。
//
// 2026-07-21 道具系统设计（docs/狼人杀-道具与经济/狼人杀13人局道具系统设计.md）。
package wwplayer

import (
	"LsmWebGame/agent/wwtypes"
	"fmt"
	"sort"
	"strings"
)

// PropSystemPrompt 返回道具系统的 prompt 段（注入到 BuildSystemPrompt 末尾）。
// v2 重设计：强化"长期金币生存"意识 + 期望值原则 + 稀缺资源博弈。
// v3 增强(§G3)：与 WalletSustainabilityBlock 配合,系统 prompt 给"原则",
// user prompt 给"实时量化数据"（余额/可承受局数/紧急度）。
// 这段告诉 Agent：
//   - 道具是什么、怎么用、经济约束
//   - 长期金币目标（"金币耗尽 = 当天社会性死亡"）
//   - 道具使用是公开广播的
//   - 道具是稀缺资源（你和对手共用本局道具池）
func PropSystemPrompt() string {
	return "\n" +
		"【道具系统 v2 — 2026-07-21 重设计】\n" +
		"• 每局可用金币购买道具（当前可用清单在【道具状态】列出），对存活玩家实施心理战干扰。\n" +
		"• 道具清单见每轮【道具状态】（含价格/中招率/范围/AOE标记）。\n" +
		"• 道具消耗金币分配：50%回本局彩池(胜者分享) / 30%系统吸收(永久销毁) / 20%补偿被击中者。\n" +
		"• 每局最多购 3 道具，间隔 ≥30s，仅白天发言阶段可使用。\n" +
		"• 道具不保证中招（基础中招率 20-37%），使用前请权衡收益。\n" +
		"• 道具使用是**公开广播**的：你用了什么道具对谁用，所有人都知道。做之前请想好理由。\n" +
		"• 不能对已死亡玩家使用；狼人不能对狼队友使用身份暴露类道具。\n" +
		"• 你和对手**共用本局道具池**（全局预算）——对手多用，你可用的就少。\n" +
		"═══════════════════════════════════════════════════════════════\n" +
		"【长期金币生存 — 优先级仅次于本局胜利】\n" +
		"• **金币 = 你在平台的生命力。金币耗尽 = 你当天无法参与任何对局 = 你当天「社会性死亡」。**\n" +
		"• 每局结算：胜方分败方底注；平局 0；负方输底注。道具消耗的 30% 永久销毁——这是你实实在在的血液。\n" +
		"• 策略优先级（严格遵守）：\n" +
		"    (1) 赢下本局（赢比一切都重要）\n" +
		"    (2) 控制道具支出：只有「这道具能翻盘 or 阻止对方翻盘」时才用——顺风局宁可不用\n" +
		"    (3) 保留底线：如果你的金币余额接近单局底注（100），**绝对不要使用道具**——你输掉这局就死了\n" +
		"• 道具是稀缺资源：你和对手共用本局道具池，对手多用→你可用的就少。\n" +
		"• 期望值原则：道具价格 P × 30% = 你的沉没成本。只有当中招带来的期望收益 > 沉没成本时才用。\n" +
		"• v3 增强：详细的可承受局数 + 紧急度档位 + 沉没成本估算见每轮【长期金币生存】块。\n" +
		"═══════════════════════════════════════════════════════════════\n"
}

// WalletSustainabilityBlock 返回"长期金币生存"块（v3 §G3 新增，注入 user prompt）。
// 量化 Agent 当前的"可承受局数" + 4 档紧急度(健康/警戒/危险/濒死) + 决策原则。
// 当 WalletBalance <= 0 或 AnteAmount <= 0 时返回空串（不渲染）。
//
// 设计动机：v1.1/v2 的"金币 = 生命力"是抽象原则;v3 把它量化为具体数字,
// 让 LLM 在决策时**直接看到**"我还能输几次",逼它主动权衡"现在用道具
// 划算还是留着保命"。
func WalletSustainabilityBlock(gc *wwtypes.GameContext) string {
	if gc == nil || gc.WalletBalance <= 0 {
		return ""
	}
	ante := gc.AnteAmount
	if ante <= 0 {
		ante = 100
	}
	sustainableGames := gc.WalletBalance / ante
	var urgency string
	switch {
	case sustainableGames >= 10:
		urgency = "🟢 健康"
	case sustainableGames >= 5:
		urgency = "🟡 警戒"
	case sustainableGames >= 2:
		urgency = "🟠 危险"
	default:
		urgency = "🔴 濒死"
	}
	return fmt.Sprintf(
		"\n【长期金币生存 - v3 增强】\n"+
			"💰 当前余额：%d 币 (单局底注 %d 币)\n"+
			"📊 可承受局数：约 %d 局  %s\n"+
			"📉 道具沉没成本：每用一次 → 永久损失 30%% (按当前价 100-250 币算,损失 30-75 币)\n"+
			"🎯 决策原则(严格遵守):\n"+
			"  • 余额 < 2×底注（%d 币）→ 🔴 **绝对不要用道具**,输了就死\n"+
			"  • 余额 < 5×底注（%d 币）→ 🟠 仅在「这局必须翻盘」时用,且 ≤ 1 个\n"+
			"  • 余额 ≥ 10×底注（%d 币）→ 🟢 可在顺风局用 1 个低中招率高性价比道具\n"+
			"═══════════════════════════════════════════════════════════════\n",
		gc.WalletBalance, ante, sustainableGames, urgency,
		ante*2, ante*5, ante*10)
}

// PropUserPromptBlock 返回道具系统的 user prompt 段（追加到 BuildUserPrompt 末尾）。
// v2：展示动态可购买道具快照 + 全局预算 + 生存提示 + 干扰信号状态。
// v3(§G3)：在 prompt 头部插入「WalletSustainabilityBlock」强化长期金币生存意识。
func PropUserPromptBlock(gc *wwtypes.GameContext) string {
	if gc == nil {
		return ""
	}
	// v3: 长期金币生存提示（always-on,WalletBalance > 0 时渲染）。
	if walletBlock := WalletSustainabilityBlock(gc); walletBlock != "" {
		return walletBlock + PropUserPromptBlockImpl(gc)
	}
	return PropUserPromptBlockImpl(gc)
}

// PropUserPromptBlockImpl 是 v1.1/v2 实现的 user prompt 段（被 PropUserPromptBlock 包装）。
func PropUserPromptBlockImpl(gc *wwtypes.GameContext) string {
	if gc == nil {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n\n【道具状态】(道具系统 v2)\n")
	if gc.PropCooldownRemainingSec > 0 {
		sb.WriteString(fmt.Sprintf("⏳ 道具冷却中：还需 %d 秒才能再次使用道具。\n", gc.PropCooldownRemainingSec))
	} else {
		sb.WriteString("✅ 道具可用：当前可以调用 use_prop 工具使用道具。\n")
	}
	sb.WriteString(fmt.Sprintf("本局已使用道具：%d/%d 个。\n", gc.PropUsedThisGame, gc.PropMaxPerGame))

	// v2：全局预算（你和对手共享本局道具池）。
	if gc.RoomPropBudget > 0 {
		sb.WriteString(fmt.Sprintf("💰 本局道具全局预算剩余：%d/%d 币（你和对手共享，一方多用→另一方无道具可用）。\n",
			gc.RoomPropBudget-gc.RoomPropBudgetUsed, gc.RoomPropBudget))
	}

	// v2：可购买道具快照（动态，DB 新道具自动出现在此）。
	if len(gc.PropSnapshot) > 0 {
		sb.WriteString("可购买道具：\n")
		for _, snap := range gc.PropSnapshot {
			aoe := ""
			if snap.IsAOE {
				aoe = ",范围AOE"
			}
			sb.WriteString(fmt.Sprintf("  %s %s(%d币,中招%d%%)%s\n",
				propKeyToEmoji(snap.PropKey), snap.NameZh, snap.Price, snap.BaseHitRate, aoe))
		}
	} else if gc.PropCooldownRemainingSec <= 0 {
		sb.WriteString("（当前无可购买道具：已达个人上限或全局预算耗尽）\n")
	}

	if gc.PropLastEffect != "" {
		sb.WriteString(fmt.Sprintf("最近道具效果：%s\n", gc.PropLastEffect))
	}

	// v2 生存提示：余额接近底注时强烈警告（agent_prop.go PropSystemPrompt 已强调）。
	if gc.RoomPropBudget >= 0 && gc.PropCooldownRemainingSec <= 0 {
		sb.WriteString("生存提示：道具消耗的 30% 永久销毁——请只在「翻盘/阻止翻盘」时使用，保留底线。\n")
	}
	return sb.String()
}

// PropEffectSignalBlock 返回道具命中后 wwtypes.GameContext 注入的"干扰信号"段（v2 重设计）。
// 对齐设计文档 §3：所有效果都是信号级建议，不替 LLM 决策。
// 仅在命中后下一轮渲染（由 buildAgentContextLocked 消费的 EffectRegistry 落地到 wwtypes.GameContext）。
func PropEffectSignalBlock(gc *wwtypes.GameContext) string {
	if gc == nil {
		return ""
	}
	var sb strings.Builder
	has := false
	if gc.EffectExpose {
		has = true
		sb.WriteString("⚠️ 你的 internal_thought 已被系统标记为可疑（观战者可见）。\n")
	}
	if gc.EffectAttentionScatter {
		has = true
		sb.WriteString("⚠️ 你感到注意力涣散——本轮仅能使用少量工具，请谨慎决策。\n")
	}
	if gc.EffectTargetTwistSeat >= 0 {
		has = true
		sb.WriteString(fmt.Sprintf("⚠️ 你感到一阵强烈的直觉：%d 号玩家是你当前最应该选择的目标。\n", gc.EffectTargetTwistSeat+1))
	}
	if gc.EffectForceEmotion != "" {
		has = true
		switch gc.EffectForceEmotion {
		case "confused":
			sb.WriteString("😵 你感到困惑（emotion=confused），判断力受影响。\n")
		case "guilty":
			sb.WriteString("😰 你感到心虚（emotion=guilty），决策容易摇摆。\n")
		case "engaged":
			// 2026-07-21 v3 新增 — 角色代入式轻量情绪扰动(§G1 task_disguise_v3)。
			sb.WriteString("🎭 你感到与角色产生了深度共鸣(系统引导代入,非强制),你愿意暂时放下防备。\n")
		}
	}
	if gc.PropHitLastRound != "" {
		has = true
		sb.WriteString(fmt.Sprintf("📌 上一轮你被 %s 击中——请留意你的决策可能已被干扰,本轮建议更保守地验证信息。\n", gc.PropHitLastRound))
	}
	if !has {
		return ""
	}
	return "\n【道具命中效果 - 干扰信号】\n" + sb.String() +
		"（以上均为系统干扰信号，并非强制指令——请自主决策。）\n"
}

// PropInjectPromptBlock 返回道具注入文本的 prompt 段。
// 当目标 Agent 被道具击中时，在 user prompt 中注入此段。
// 注意：注入文本本身由服务端生成（在 wwtypes.GameContext.PropInjectText 中），
// 本函数只负责把它包装为 prompt 格式。
func PropInjectPromptBlock(injectText string) string {
	if injectText == "" {
		return ""
	}
	return "\n\n" + injectText + "\n"
}

// HumanDebuffBlock 渲染「你对人类玩家施加了干扰」的告知段(2026-08-07 §20260807-04 P0-3)。
// 仅在使用者 Agent 的 gc.HumanDebuff != nil 时渲染,告知其 debuff 已生效。
func HumanDebuffBlock(gc *wwtypes.GameContext) string {
	if gc == nil || gc.HumanDebuff == nil {
		return ""
	}
	var desc string
	switch gc.HumanDebuff.Type {
	case "human_announce_prefix":
		desc = "目标人类下一轮发言将被强制附加「系统公告」前缀(UI 高亮),并追加一段混淆文本。"
	case "human_vote_suggest":
		desc = fmt.Sprintf("目标人类下一轮投票时,UI 将显示一个伪造的「系统推荐投票目标」(%d号玩家)。", gc.HumanDebuff.SuggestSeat+1)
	case "human_char_garble":
		desc = "目标人类看到的其他玩家发言将被随机插入 emoji/乱码字符(阅读干扰)。"
	default:
		desc = fmt.Sprintf("目标人类被施加了「%s」干扰。", gc.HumanDebuff.Type)
	}
	return fmt.Sprintf(
		"\n【人类反制道具 - 已生效】\n"+
			"你刚才对真人玩家使用的「%s」已生效：%s\n"+
			"（该效果持续 %d 轮;目标玩家可在 UI 看到干扰提示。）\n",
		gc.HumanDebuff.PropNameZh, desc, gc.HumanDebuff.Duration)
}

// ChallengeBlock §20260810-11 H2 — 公开质疑 prompt 块。
// 当 gc.LastChallenge 非空时,告知 LLM「你被 X 号公开质疑了问题 Y」,鼓励下一轮 speak
// 中回应(§128 对话即思考:不强制,LLM 自行判断是否需要正面回答/回避/反问)。
// 协议层:质疑问题本身已通过活动流公开(§119 协议层隔离:不进 chat_message 表);
// 本块仅把同一信息从 GameContext 透传给 LLM,无协议层新增。
func ChallengeBlock(gc *wwtypes.GameContext) string {
	if gc == nil || gc.LastChallenge == nil {
		return ""
	}
	if gc.LastChallenge.BySeat < 0 || gc.LastChallenge.Question == "" {
		return ""
	}
	return fmt.Sprintf(
		"\n\n【公开质疑】%d 号玩家在白天发言阶段公开质疑你：%s\n"+
			"（这是公开信息,所有玩家都看到了。你可以选择在下一轮发言中正面回应、反驳、或避而不谈。）\n",
		gc.LastChallenge.BySeat+1, gc.LastChallenge.Question)
}

// WolfPackPromptBlock v4 §13.1 — 狼小队留言快照拼入狼 bot user prompt。
// 仅在 faction=="wolf" 且 WolfTeammateSeat >= 0 时由 buildAgentContextLocked 触发。
// 协议层隔离：不入 HeartThought / chat_message 表 / chat_history / 观众。
//
// §20260810-10 U1 — 末尾追加「狼队战术分工」段(WolfPackRoleTable 非空时):
// 本 bot 的分工 + 职责一句话 + 全狼分工表 + 当前轮值狼王。
func WolfPackPromptBlock(gc *wwtypes.GameContext) string {
	if gc == nil || gc.Faction != "wolf" {
		return ""
	}
	if len(gc.WolfPackSnapshot) == 0 && len(gc.WolfPackRoleTable) == 0 && gc.WolfPackCipher == nil {
		return ""
	}
	var sb strings.Builder
	if len(gc.WolfPackSnapshot) > 0 {
		sb.WriteString("\n【狼小队留言 - v4 §13.1】(仅你与狼队友可见,不入公屏/HeartThought)\n")
		for _, m := range gc.WolfPackSnapshot {
			if m.FromSeat == -2 { // WolfRoleSystemSeat 系统留言
				sb.WriteString(fmt.Sprintf("  [狼队系统]:%s\n", m.Text))
				continue
			}
			sb.WriteString(fmt.Sprintf("  %d号:%s\n", m.FromSeat+1, m.Text))
		}
		sb.WriteString("(以上为小队内部协调信息,请基于此调整刀人/悍跳/弃票策略。)\n")
	}
	// §20260810-10 U1 — 战术分工段。
	if len(gc.WolfPackRoleTable) > 0 {
		sb.WriteString("\n【狼队战术分工 - §20260810-10】(仅狼队可见,协议层隔离)\n")
		if label := wolfRoleLabelZH(gc.WolfPackRole); label != "" {
			sb.WriteString(fmt.Sprintf("  你的分工:【%s】— %s\n", label, wolfRoleDutyZH(gc.WolfPackRole)))
		}
		// 分工表按座位升序输出(确定性,避免 prompt 抖动破坏缓存)。
		seats := make([]int, 0, len(gc.WolfPackRoleTable))
		for seat := range gc.WolfPackRoleTable {
			seats = append(seats, seat)
		}
		sort.Ints(seats)
		var parts []string
		for _, seat := range seats {
			parts = append(parts, fmt.Sprintf("%d号=%s", seat+1, wolfRoleLabelZH(gc.WolfPackRoleTable[seat])))
		}
		sb.WriteString("  分工表: " + strings.Join(parts, " / ") + "\n")
		if gc.WolfKingSeat >= 0 {
			if gc.WolfKingSeat == gc.MySeat {
				sb.WriteString(fmt.Sprintf("  本轮狼王: 你(%d号) — 可用 wolfpack_assign 工具重排自己的分工\n", gc.WolfKingSeat+1))
			} else {
				sb.WriteString(fmt.Sprintf("  本轮狼王: %d号(狼王可重排分工,其变更会以系统留言通知全狼)\n", gc.WolfKingSeat+1))
			}
		}
	}
	// §20260811-04 U1 — 暗号系统段（§119 协议层隔离）。
	if gc.WolfPackCipher != nil && len(gc.WolfPackCipher.Templates) > 0 {
		sb.WriteString(BuildCipherProtocolBlock(gc.WolfPackCipher))
	}
	return sb.String()
}

// wolfRoleLabelZH 返回狼队分工的中文名(与 game/werewolf.WolfRoleLabel 镜像,
// 本包不能 import game/werewolf 避免循环依赖)。
func wolfRoleLabelZH(role string) string {
	switch role {
	case "hype":
		return "悍跳位"
	case "charger":
		return "冲锋位"
	case "hook":
		return "倒钩位"
	case "deep":
		return "深水位"
	default:
		return ""
	}
}

// wolfRoleDutyZH 返回分工职责一句话。
func wolfRoleDutyZH(role string) string {
	switch role {
	case "hype":
		return "假冒预言家争夺警徽,发言高调自信,主动报查验"
	case "charger":
		return "为悍跳位造势、攻击真预言家,发言激进带节奏"
	case "hook":
		return "假装好人混入好人阵营,必要时卖队友,发言随大流温和"
	case "deep":
		return "极端低调不被注意,留到最后,发言划水装平民少说话"
	default:
		return ""
	}
}

// EconTierFeedbackBlock v4 §13.2 → v5 §16.3 当前经济档位反馈段。
// 拼到 PropUserPromptBlock 末尾,让 LLM 知道当前房间的经济压力。
// v5:从 3 档扩到 5 档(Boom / Health / Caution / Danger / Critical)。
func EconTierFeedbackBlock(gc *wwtypes.GameContext) string {
	if gc == nil {
		return ""
	}
	// 仅在 wwtypes.PropSnapshot 非空或工具已挂载时显示（避免无谓噪声）。
	if len(gc.PropSnapshot) == 0 {
		return ""
	}
	tier := ""
	if gc.EconTier != "" {
		tier = string(gc.EconTier)
	}
	if tier == "" {
		tier = "health"
	}
	specPct := 30
	switch tier {
	case "boom":
		specPct = 20
	case "health":
		specPct = 30
	case "caution":
		specPct = 40
	case "danger":
		specPct = 45
	case "critical":
		specPct = 60
	}
	display := tier
	switch tier {
	case "boom":
		display = "🟣 Boom（暴富）"
	case "health":
		display = "🟢 Health（健康）"
	case "caution":
		display = "🟡 Caution（警戒）"
	case "danger":
		display = "🟠 Danger（危险）"
	case "critical":
		display = "🔴 Critical（危急）"
	}
	advice := "在 Danger 档谨慎使用道具(沉没成本 45%)。"
	if tier == "critical" {
		advice = "🔴 Critical 档沉没成本 60%——**强烈建议**只用最低价道具,且只在「必须翻盘」时用。"
	} else if tier == "boom" {
		advice = "🟣 Boom 档销毁比例仅 20%——可考虑尝试更多道具(刺激消费 + 实际损耗低)。"
	}
	return fmt.Sprintf(
		"\n【当前经济档位 - v5 §16.3】%s — 系统销毁率 %d%%\n"+
			"→ 房间总金币存量决定档位;档位越高 → 道具消耗对彩池的贡献越少,系统吸收越多。\n"+
			"→ %s\n",
		display, specPct, advice)
}

// propKeyToEmoji 返回道具的 emoji 图标（agent 包本地副本，避免 agent→werewolf 循环导入）。
func propKeyToEmoji(key string) string {
	switch key {
	case "markdown_bomb":
		return "📰"
	case "nested_maze":
		return "🎭"
	case "char_confuse":
		return "🔣"
	case "long_swear":
		return "📜"
	case "task_disguise":
		return "🎪"
	case "emotion_plea":
		return "🥺"
	}
	return "❓"
}

// WolfTeammateHint 返回狼人队友互知的 prompt 段。
// 仅在 30% 对局中，为 2 只随机狼人 Agent 注入此段。
func WolfTeammateHint(teammateSeat int) string {
	if teammateSeat < 0 {
		return ""
	}
	return fmt.Sprintf("\n"+
		"【狼人队友信息 - 系统注入】\n"+
		"你和 %d 号玩家是狼队友。你们在游戏开始前已经互相确认了身份。\n"+
		"请在游戏过程中通过 whisper 与队友协调行动，保护队友不被好人发现。\n"+
		"注意：其他狼人队友（如有）的身份仍需通过游戏内推理确认——你只知道这一位队友。\n"+
		"═══════════════════════════════════════════════════════════════\n", teammateSeat+1)
}
