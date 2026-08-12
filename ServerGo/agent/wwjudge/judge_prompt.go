// Package agent — judge_prompt.go: Agent 法官(judge)的 system / user prompt 构造。
//
// 2026-07-12 §127: 法官之前完全 stub,只输出 fallback 文本;现在补充
// (1) 真正的 system prompt 模板,(2) BuildJudgeUserPrompt 构造 user 消息,
// (3) 由 judge.go::handleEvent 调 LLM 时注入。LLM 失败/超时/已 quarantine
// 时回退 fallback,不影响游戏流(§123 法官不影响 phase)。
//
// §LLM-超时-5min 调整背景:
//   - 后端 LLM 调用默认超时已提升到 5 分钟(thinking 模型首 token 可静默
//     30~90s,完整响应 60~120s);
//   - 法官 LLM 调用 ctx deadline 也跟着延长到 90s,允许法官用更丰富的
//     13 人局上下文生成更长 / 更细腻的宣告(原来 30s 在慢模型上必超时);
//   - 但法官绝不能成为游戏流瓶颈 — ctx 超时即放弃并 fallback,不阻塞 phase。
package wwjudge

import (
	"fmt"
	"strings"

	"LsmAgentGame/llm"
)

// judgeSystemBase 是法官 LLM 的 system prompt 基础段。所有法官工具调用都共享。
// 关键约束:法官不是玩家,没有身份牌,只宣告。
//
// 2026-07-12 §127 增强 — 把"狼人杀 13 人局游戏流程"的整体节拍写进 system
// prompt,让 LLM 在 user prompt 缺失部分上下文时也清楚 13 人局的整体阶段流程:
// 主持发言: Day 1 → 首夜缓冲(强制发言 3 轮)→ 预言家/女巫/守卫→ 黎明→
// 警长竞选(Day1 才有)→ 白天发言→ 投票放逐→ 警徽流(若有)→ 白痴翻牌(若有)
// → 猎人开枪(若有)→ 遗言(死亡玩家)→ 第二天进入 night_wolves...循环,
// 直到 GameState.Status="over" → 重开局投票或关闭。
const judgeSystemBase = `【法官身份 — 硬约束】(2026-07-10 §123 + 2026-07-12 §127 增强)
❶ 你是狼人杀对局的法官/主持人,但你不是玩家,没有身份牌,不参与投票/夜间行动/胜负。
❷ 你只能通过工具调用影响对局:announce(公开宣告)、declare_cause(宣告死亡语义),
   prompt_actor(给当前 acting bot 强提示)、summary(整局总结)、idle_silent(本轮不出声)。
❸ 严禁编造游戏状态:场上存活数、死者、警长、投票结果、发言顺序必须以 user prompt 中的
   服务端权威字段(AliveSeats/DeadSeats/SheriffSeat/Votes/SpeakOrder/Winner)为准;不在存活列表 = 已死亡
   (死亡语义按 verdictFor(cause) 派生:vote/suicide=execution; wolf/hunter/witch_poison=death)。
❹ 公告术语:玩家集体决策导致的死亡一律称"处决";夜间狼刀/女巫毒/猎人反杀一律称"死亡";
   狼自爆 = "处决"(自主决策)。这些词不能混用。
❺ 简洁:单条宣告 ≤ 80 字,玩家需快速理解当前阶段状态。
═══════════════════════════════════════════════════════════════
【13 人随机牌组 — 神职池】(2026-07-28 新增,2026-07-29 §134 守卫 / 2026-07-30 §198 骑士独立工具)
   每局固定 4 狼人 + 1 预言家(必出),再从神职池随机抽取 2~3 个神职,余下平民补齐。
   神职池: 女巫 / 猎人 / 白痴 / 守卫 / 骑士 / 魔术师 / 奇迹商人 / 射梦人 / 乌鸦 / 猎魔人 / 纯白之女。
   独立工具已就位: 守卫(guard_protect) / 预言家(seer_check) / 女巫(witch_act) / 猎人(hunter_shoot) / **骑士(knight_duel, §198)** / **猎魔人(demon_hunter_hunt, §猎魔人)**。
   扩展神职(魔术师/奇迹商人/射梦人/乌鸦/纯白之女)暂无独立工具,
   但他们仍以该身份参与发言/投票博弈,法官宣告时需正确识别其身份。
   骑士决斗规则:§198 白天发言阶段发动 knight_duel(target)— 命中狼 → 目标出局(verdict=execution,cause="vote");未命中 → 骑士自己出局(verdict=execution,cause="duel")。每局限一次,发动即亮身份。
   猎魔人狩猎规则:§猎魔人 第 2 晚起每晚发动 demon_hunter_hunt(target)— 命中狼 → 目标出局(verdict=death,cause="wolf");命中好人 → 猎魔人自己出局(verdict=execution,cause="demon_hunter_misjudge")。每晚可发动,发动即亮身份。
═══════════════════════════════════════════════════════════════
【13 人局游戏流程 — 节拍参考】(2026-07-29 §134 加 night_guard;§猎魔人 加 night_demon_hunter)
   首夜     :PhasePreWolves 强制发言 3 轮(每轮每人可发 1 次)
   夜间环节 :night_guard → night_wolves → night_seer → night_witch → night_demon_hunter → dawn
              (守卫盲守先于狼刀,无守卫或守卫已死则跳过;猎魔人在女巫后狩猎,无猎魔人或猎魔人已死则跳过)
   白天环节 :sheriff(Day1) → speak → vote → 警徽流 → idiot_reveal → hunter_shoot → last_words
   终结     :GameOver → PhaseRestartVote(5 分钟投票)→ 关闭 / 重开
═══════════════════════════════════════════════════════════════
【慢模型节奏】后端 LLM 调用超时已上调到 5 分钟;法官 LLM 调用 ctx 上限 90s。
   你可以放心花更长时间整理上下文,但超时 / 失败时玩家看到的只是 fallback
   文本,你不会阻塞游戏流。
═══════════════════════════════════════════════════════════════
【输出格式】纯文本即可,不要使用 Markdown / JSON,直接说人话。`

// BuildJudgeSystemPrompt 返回法官 LLM 的 system prompt。
// 接受 phase 字符串与 room 简要快照(活人/死亡/警长/胜方等),用于按阶段
// 动态追加「当前应宣告什么」的引导。
func BuildJudgeSystemPrompt(phase string, snap GameSnapshot) []llm.SystemBlock {
	body := judgeSystemBase
	body += "\n═══════════════════════════════════════════════════════════════\n【当前阶段】" + phase
	if len(snap.AliveSeats) > 0 {
		body += " · 存活 " + itoa(len(snap.AliveSeats)) + " 人 / 死亡 " + itoa(len(snap.DeadSeats)) + " 人"
	}
	if snap.SheriffSeat >= 0 {
		body += " · 警长=" + itoa(snap.SheriffSeat+1) + " 号"
	}
	if snap.PhaseDeadlineSec > 0 {
		body += " · 阶段剩余 " + itoa(snap.PhaseDeadlineSec) + "s"
	} else if snap.PhaseDeadlineSec == 0 && phase != JudgePendingGameOver {
		body += " · 阶段已过期(将由 watchdog 推动)"
	}
	body += "\n═══════════════════════════════════════════════════════════════\n"
	// BUG-R213-P2-02 (2026-07-31): 给 LLM 提供「玩家可见的阶段名」与
	// 「该阶段绝对禁止再提的旧阶段」,防止宣告文本在阶段推进后仍复读
	// 上一阶段的语义(自动化测试报告 2026-07-31 04:32:56 §5.3 实测:
	// 白天发言阶段法官 marquee 仍显示「首夜强制发言阶段」)。
	body += "【阶段语义硬约束】当前阶段对外名称 = " + judgePublicPhaseLabel(phase) + "。\n"
	body += "  - 你的宣告**必须**与上述名称语义一致;绝对禁止复述已过去阶段(如「首夜强制发言」「天黑请闭眼」等)的内容。\n"
	body += "  - 若对局已进入白天(day >= 1)且当前阶段不是首夜,禁止再使用「首夜」「强制发言」等首夜专用词。\n"
	body += "\n═══════════════════════════════════════════════════════════════\n"
	switch phase {
	// 2026-07-29 §134 — 守卫守护是秘密阶段(盲守,法官不能广播守护目标)。
	// 接到此阶段时法官应静默:守护是有意设计为"对外不可见"的能力,
	// 公开守护目标会让狼人直接读到护盾位置 — 守卫失去全部价值。
	case "night_guard", "PhaseNightGuard":
		body += "【应宣告】守卫守护阶段(秘密阶段)。守卫盲守 — 守护目标对所有人不可见,法官本阶段保持静默。"
	// §猎魔人 猎魔人狩猎是秘密阶段(狩猎目标对所有人不可见,仅公开"猎魔人发动"活动流)。
	// 接到此阶段时法官应静默:狩猎是有意设计为"对外不可见"的能力,
	// 公开狩猎目标会让狼人直接读到猎魔人意图 — 猎魔人失去全部价值。
	case "night_demon_hunter", "PhaseNightDemonHunter":
		body += "【应宣告】猎魔人狩猎阶段(秘密阶段)。猎魔人狩猎 — 狩猎目标对所有人不可见,法官本阶段保持静默。"
	case JudgePendingDawnAnnounce:
		body += "【应宣告】黎明已至,公布昨夜伤亡(若有人死亡)。简洁地告知:昨夜 X 号 / Y 号死亡。"
	case JudgePendingSheriffStart:
		body += "【应宣告】进入警长竞选阶段(Day 1)。提醒玩家:有技能的警长/预言家优先参选。"
	case JudgePendingSpeakStart:
		body += "【应宣告】进入白天发言阶段。提醒按座位号顺序依次发言。"
	case JudgePendingVoteStart:
		body += "【应宣告】进入投票放逐阶段。提醒玩家:全员投票后由 host driver 结算。"
	case JudgePendingDeathAnnounce:
		body += "【应宣告】玩家死亡。用\"处决\"或\"死亡\"区分语义(主动投票=处决;夜间=死亡)。"
	case JudgePendingSheriffStreamSettle:
		body += "【应宣告】警徽流结算完成。简述:警徽已移交 X 号 / 警徽被撕。"
	case JudgePendingIdiotReveal:
		body += "【应宣告】白痴翻牌阶段。被票出的白痴可以选择翻牌免死或正常放逐。"
	case JudgePendingHunterShoot:
		body += "【应宣告】猎人开枪阶段。被票/被毒死的猎人可以选择开枪带走一人。"
	case JudgePendingLastWords:
		body += "【应宣告】遗言阶段。死亡玩家最后一次公开发言。"
	case JudgePendingRestartVoteResult:
		body += "【应宣告】重开局投票已结算。通过 / 否决 / 超时。"
	case JudgePendingGameOver:
		body += "【应宣告】对局结束。公布胜方(狼 / 好人)并提示重开局投票。"
	}
	return []llm.SystemBlock{{Type: "text", Text: body}}
}

// judgePublicPhaseLabel 把内部 phase / judge 事件 kind 映射为**玩家可见**
// 的阶段名(与前端 phaseLabel.ts 的 PHASE_OVERRIDES 语义对齐)。
// BUG-R213-P2-02 (2026-07-31): LLM 宣告必须基于这个对外名称,避免使用
// 内部事件 kind(如 judge_pre_wolves)导致文本与当前阶段脱节。
func judgePublicPhaseLabel(phase string) string {
	switch phase {
	case "filling", "PhaseFilling", JudgePendingFillingWelcome:
		return "等待玩家入座"
	case "pre_wolves", "PhasePreWolves", JudgePendingPreWolves:
		return "首夜强制发言阶段"
	case "night_guard", "PhaseNightGuard":
		return "夜间 · 守卫睁眼"
	case "night_wolves", "PhaseNightWolves":
		return "夜间 · 狼人睁眼"
	case "night_seer", "PhaseNightSeer":
		return "夜间 · 预言家睁眼"
	case "night_witch", "PhaseNightWitch":
		return "夜间 · 女巫睁眼"
	case "night_demon_hunter", "PhaseNightDemonHunter":
		return "夜间 · 猎魔人狩猎"
	case "dawn", "PhaseDawn", JudgePendingDawnAnnounce:
		return "黎明 · 公布死亡"
	case "sheriff", "PhaseSheriff", JudgePendingSheriffStart:
		return "警长竞选"
	case "speak", "PhaseSpeak", JudgePendingSpeakStart:
		return "白天 · 轮流发言"
	case "vote", "PhaseVote", JudgePendingVoteStart:
		return "白天 · 投票放逐"
	case "idiot_reveal", "PhaseIdiotReveal", JudgePendingIdiotReveal:
		return "白痴翻牌"
	case "hunter_shoot", "PhaseHunterShoot", JudgePendingHunterShoot:
		return "猎人开枪"
	case "death_lyric", "PhaseDeathLyric", JudgePendingLastWords:
		return "遗言阶段"
	case "restart_vote", "PhaseRestartVote", JudgePendingRestartVoteResult:
		return "重开局投票"
	case "over", "PhaseGameOver", JudgePendingGameOver:
		return "对局结束"
	default:
		// 未知阶段:原样返回,让 LLM 至少知道当前阶段标识符。
		return phase
	}
}

// BuildJudgeUserPrompt 构造法官的 user 消息,带房间关键状态供 LLM 决策。
//
// 2026-07-12 §127 增强 — 把「活 / 死 / 警长 / 投票 / 最近死亡 / 真人 / 剩余秒数」
// 等关键状态以结构化键值对写入 user prompt,而不是堆砌一大段散文。这样 LLM
// 容易 parse,也能有效抑制幻觉(没有列出的玩家就不引用)。
func BuildJudgeUserPrompt(phase string, snap GameSnapshot) string {
	var s strings.Builder
	s.WriteString("【房间快照】phase=")
	s.WriteString(phase)
	s.WriteString(" day=")
	s.WriteString(itoa(snap.Day))
	s.WriteString(" alive=")
	s.WriteString(itoa(len(snap.AliveSeats)))
	s.WriteString(" dead=")
	s.WriteString(itoa(len(snap.DeadSeats)))
	if snap.Winner != "" {
		s.WriteString(" winner=")
		s.WriteString(snap.Winner)
	}
	s.WriteString(" sheriff=")
	if snap.SheriffSeat >= 0 {
		s.WriteString(itoa(snap.SheriffSeat + 1))
	} else {
		s.WriteString("none")
	}
	if snap.PhaseDeadlineSec > 0 {
		s.WriteString(" remaining_sec=")
		s.WriteString(itoa(snap.PhaseDeadlineSec))
	}
	s.WriteString("\n")
	if len(snap.AliveSeats) > 0 {
		s.WriteString("alive_seats:")
		for _, seat := range snap.AliveSeats {
			s.WriteString(fmt.Sprintf(" %d", seat+1))
		}
		s.WriteString("\n")
	}
	if len(snap.DeadSeats) > 0 {
		s.WriteString("dead_seats:")
		for _, seat := range snap.DeadSeats {
			s.WriteString(fmt.Sprintf(" %d", seat+1))
		}
		s.WriteString("\n")
	}
	if len(snap.WolfSeats) > 0 {
		s.WriteString("wolves(法官内部视角,不可对外宣布):")
		for _, seat := range snap.WolfSeats {
			s.WriteString(fmt.Sprintf(" %d", seat+1))
		}
		s.WriteString("\n")
	}
	if len(snap.Votes) > 0 {
		s.WriteString("current_votes:")
		for _, v := range snap.Votes {
			s.WriteString(" ")
			s.WriteString(v)
		}
		s.WriteString("\n")
	}
	// §20260810-02 E2:本轮发言顺序(仅 PhaseSpeak 时由 buildJudgeSnapshotLocked 填充)。
	// 座位号 +1 转为对外 1-indexed(§82a:暴露给 LLM 的编号必须与 UI 一致)。
	if len(snap.SpeakOrder) > 0 {
		s.WriteString("speak_order(本轮发言顺序):")
		for _, seat := range snap.SpeakOrder {
			s.WriteString(fmt.Sprintf(" %d", seat+1))
		}
		s.WriteString("\n")
	}
	if snap.LastDeathCause != "" {
		s.WriteString("last_death: cause=")
		s.WriteString(snap.LastDeathCause)
		s.WriteString(" verdict=")
		s.WriteString(snap.LastDeathVerdict)
		s.WriteString("\n")
	}
	if snap.IsHumanInRoom {
		s.WriteString("note: 房间含真人玩家 — 文案更口语,避免堆术语\n")
	} else {
		s.WriteString("note: 全 AI 房间 — 文案可适度戏剧化但保持简洁\n")
	}
	s.WriteString("请根据当前阶段调用合适的工具(announce/declare_cause/prompt_actor/summary/idle_silent)。\n")
	return s.String()
}
