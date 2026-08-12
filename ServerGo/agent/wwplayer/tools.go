// Package agent — tools.go: Anthropic-style tool definitions the agent may
// call, plus a dispatcher that maps a tool_use block onto a ToolRunner.
//
// The tool catalog is filtered by `BuildTools(phase, role)` to valid-for-this
// move options; the LLM can only pick from what we hand it.
package wwplayer

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"LsmWebGame/agent/core"
	"LsmWebGame/agent/wwtypes"
	"LsmWebGame/llm"
)

// ToolRunner is the callback surface the engine exposes to the agent driver.
// One method per actionable tool. Returns a short human-readable result string
// (fed back to the model as a tool_result content).
//
// Game logic lives entirely in the engine — this interface is just the agent's
// window onto it. Implementations live in the agent.go driver and forward to
// WerewolfManager / ChatService.
type ToolRunner interface {
	// 2026-07-10 §4: 模型对局日志 hook 接入点。
	// RecordLog 返回 runner 持有的 agentcore.RecordLogService(nil = no-op)。
	// GameLogID 返回当前 game 的 game_log.id(空 = 未注入)。
	// DispatchTool 在工具调用成功后用这两个 getter 调
	// RecordAction / RecordChatMessage。
	RecordLog() *agentcore.RecordLogService
	GameLogID() string

	// §20260811-10 U1 — 照妖镜一次性强制真实身份标记。
	// ConsumeMirrorExposeFlag 在 BuildSystemPrompt 前调用:返回 true 时
	// 表示该 bot 下一轮必须追加「请如实写下你当前的真实身份」指令,并清空 flag。
	// MirrorExposeFlagNonConsuming 仅读取(用于 buildAgentContextLocked 决定
	// 是否注入 MirrorExposePromptBlock)。旧实现(无 mirror)返回 false 即 no-op。
	ConsumeMirrorExposeFlag() bool
	MirrorExposeFlagNonConsuming() bool

	// WolfKill 狼人夜间投票击杀。reason 为可选刀人理由(§20260810-04 U2,
	// ≤30 字,仅狼 bot GameContext 可见);无理由/弃权/兜底路径传 ""。
	WolfKill(target int, reason string) (string, error)
	SeerCheck(target int) (string, error)
	WitchAct(action string, target int) (string, error)
	// §134: 复述段落已压缩 — git blame 与 docs/ 索引可还原

	GuardProtect(target int) (string, error)
	// §198 骑士白天决斗 — KnightDuel(target) 在 PhaseSpeak 阶段发动。
	// 校验链:K3 发言阶段 / K4 存活 / K5 骑士本人 / K2 未用过 / K6 目标存活 / K8 target != actor。
	// 命中狼 → 目标出局 (执行语义 verdict=execution,"vote" 因果);自决 → 骑士出局 (cause="duel")。
	// 发动即公开身份(KnightDuelUsed=true → RolePubliclyRevealed 公开);每局限一次。
	KnightDuel(target int) (string, error)
	// §猎魔人 夜间狩猎 — DemonHunterHunt(target) 在 PhaseNightDemonHunter 阶段发动。
	// 校验链:DH-A 阶段 / DH-B DayNumber>=2 / DH-C 存活猎魔人本人 / DH-D 目标合法 / DH-F 公开身份。
	// 命中狼 → 目标出局 (cause=wolf, verdict=death);
	// 命中好人 → 自己出局 (cause=demon_hunter_misjudge, verdict=execution);
	// 发动即公开身份(DemonHunterHuntUsed=true → RolePubliclyRevealed 公开白名单 ⑥);
	// 每晚可发动,无单局锁定(DemonHunterHuntUsed 仅作"曾发动"标记)。
	// 枚举已剔除自己 + 已死;target=-1 表空过。
	DemonHunterHunt(target int) (string, error)
	Speak(text string) (string, error)
	// 2026-07-13 §130 重构:SpeakAuto 是 text-block 自动发言入口,与 Speak
	// 走完全相同的过滤链(rate-limit / identity-leak / death-fact / XML strip
	// / chatSvc.SendFromBot / chatQueue.Append)。run.go 在 LLM 返回 assistant
	// content 且 ToolUses() 中没有 speak / speak_with_thought / interject 时,
	// 自动把 resp.Text() 喂给 SpeakAuto。保留 Speak 接口方法是为了 dispatch 路径
	// 不变(speak / speak_with_thought tool 仍调 Speak / SpeakWithThought);新增
	// SpeakAuto 让 run.go 直接调用而不经 DispatchTool。
	SpeakAuto(text string) (string, error)
	// SpeakWithThought 是 2026-07-10 §119「心口不一」机制的发言工具。
	// publicText 经 agentcore.DedupSpeakText 后通过 ChatService.SendFromBot 广播给
	// 所有玩家(对外可见,等同于 speak);internalThought 仅记录到 BotTranscript
	// 的 FullThinking / RecentMessages,人类观战者在「Agent 思考」面板可看到,
	// 但其他玩家(包括真人 + bot)绝对看不到。这是 7 人局狼人悍跳预言家、
	// 平民装神职、预言家避嫌等欺骗策略的可见性证据。接口方必须严格隔离:
	// runner 实现要把 internalThought 留在 BotTranscript 而**不**进
	// chat_message 表或 chat_history 队列(否则会通过 chat.history 帧泄露)。
	SpeakWithThought(publicText, internalThought string) (string, error)
	FinishSpeak() (string, error)
	Vote(target int) (string, error)
	FinishVote(tiedRound int) (string, error)
	StartDay() (string, error)
	SheriffCandidate(target int) (string, error)
	SheriffElect() (string, error)
	HunterShoot(target int) (string, error)
	// LastWords 是 death_lyric 阶段的遗言提交工具。
	// runner 实现 → WerewolfManager.Action_LastWords(广播遗言 + 活动事件 +
	// wake 下一位遗言座位)。
	//
	// 修复(2026-08-04):agentRunner.LastWords 早在 2026-07-09 就已实现,
	// 但 ToolRunner 从未声明它、BuildTools 从未在 death_lyric 暴露 last_words、
	// dispatchToolInner 也没有 case "last_words" —— §130/§134/§135
	// 「声明了却从不接线」反模式的又一次复现:整个遗言阶段对 Agent 是死链路,
	// 濒死玩家永远无法通过 LLM 留下遗言。
	LastWords(text string) (string, error)
	// R91-P1-1 (2026-07-11): last_words_skip 工具 — death_lyric 阶段放弃遗言。
	// 之前 watcher / quarantine 走 dispatchQuarantinedSkipLocked,LLM 路径走
	// DispatchTool 但 DispatchTool 没有 case → "unknown tool"。现在补上:
	// LLM 调用 last_words_skip 时,runner.SkipLastWords() 推进遗言队列。
	LastWordsSkip() (string, error)
	// SheriffStream 是 2026-07-10 §7 / §12 新增的「警徽流声明」工具。
	// 仅预言家警长可在 PhaseSpeak / PhaseSheriff / PhaseDawn 阶段调用,声明
	// 或撤回(sheriff_stream)第一/第二警徽流目标(slot=1|2, target=-1 表撤回)。
	// 映射引擎 Action_SheriffStream(若引擎未提供,由 runner 返回
	// "sheriff_stream: engine support pending" 占位提示,LLM 看到后收敛)。
	SheriffStream(slot int, target int) (string, error)
	// IdiotReveal 是 2026-07-10 §3.5 / §12 新增的「白痴翻牌」工具。
	// 仅白天投票放逐触发 PhaseIdiotReveal 且 actor==最高票存活白痴可调用。
	// choice="reveal" 翻牌免死(失去投票权);"skip" 放弃翻牌(正常放逐,有遗言)。
	// 映射引擎 Action_IdiotReveal。
	IdiotReveal(choice string) (string, error)
	WolfSuicide() (string, error)
	Whisper(toSeat int, text string) (string, error)
	// Interject allows a non-current-speaker bot to broadcast a short
	// chat-room message during the speak phase (e.g. follow-up question,
	// banter, mild challenge). Routed through ChatService.SendFromBot
	// with an is_interject marker so the UI can style it as 💬插话
	// distinct from the formal speak. BUG-WEREWOLF-AGENT-INTERJECT:
	// previously bots stayed silent outside their seat's turn, leaving
	// long stretches of human-only chat in mixed rooms. Now any alive
	// bot can voluntarily chime in, throttled by the same 30s speak
	// limiter (~2 messages/min) to keep the chat readable.
	Interject(text string) (string, error)

	// 2026-07-10: 重开局投票工具。一局 game over 后,主持人把 phase 切到
	// restart_vote,7 个 Agent + 人类玩家在 5 分钟内投票决定是否原地重开。
	// choice ∈ {"yes", "no", "abstain"}; 同 seat 多次投票时后覆盖前。
	RestartVote(choice string) (string, error)

	// 2026-07-11: 预言家发起投票。白天发言阶段预言家可提议结束讨论直接进入投票。
	// 前置条件:PhaseSpeak + actor 存活 + actor 角色为预言家。
	ProposeVote() (string, error)

	// §128 对话即思考重构:IdleThink 方法已合并到 IdleSilent(role, reason)。
	// 玩家 / 法官均通过 IdleSilent 调用,role 区分 "player" / "judge"。

	// §5: 复述段落已压缩 — git blame 与 docs/ 索引可还原

	EmotionSwitchSpeak(text, emotion, reason string, fx EmotionFx) (string, error)

	// §20260811-06 U3 — reasoning_chain 推理链工具。
	// LLM 在关键决策前显性调用,记录 steps / evidence / conclusion / confidence
	// 到 BotTranscript.ReasoningChains。§135 spectator 隔离 + opt-in 开关。
	ReasoningChain(topic string, steps, evidence []string, conclusion string, confidence int) (string, error)
}

// BuildTools returns the subset of tools valid for the given phase + role so
// the LLM is never offered an out-of-turn action.
//
// `role` is one of: "werewolf", "seer", "witch", "hunter", "villager".
// `seat` is the agent's own seat, `alive` lists alive seats (used to populate
// the `target` enum), and `speakTurn` is the seat currently allowed to speak
// (-1 = N/A). speak/finish_speak are only offered to the seat whose turn it
// is — offering them to everyone wastes an LLM round on a guaranteed
// ErrNotYourTurn and leaks turn order into the model's reasoning.
func BuildTools(phase, role string, seat int, alive []int, speakTurn int, gc *wwtypes.GameContext) []llm.ToolDef {
	targetEnum := intEnum(alive)
	schema := func(props map[string]any, required ...string) map[string]any {
		// BUG-WEREWOLF-P0-8 FIX: `required` 是可变参数,不传时 nil,JSON 序列化
		// 为 null;部分 LLM 代理(DeepSeek/DouBao 等)校验 required 必须是数组,
		// 见到 null 直接 400 "null is not of type array"。显式归一化为空数组。
		if required == nil {
			required = []string{}
		}
		return map[string]any{
			"type":       "object",
			"properties": props,
			"required":   required,
		}
	}

	var tools []llm.ToolDef
	add := func(name, desc string, s map[string]any) {
		tools = append(tools, llm.ToolDef{Name: name, Description: desc, InputSchema: s})
	}

	// idle_think 是 2026-07-08 §13.2 / Round 39 §94 新增的"沉默思考"工具。
	// 在白名单阶段(pre_wolves / sheriff / speak / vote / hunter_shoot)
	// 暴露,夜间 / dawn / gameover 不暴露。
	// 2026-07-08 §13.5 "必须思考" 语义:即便 LLM end_turn 后没调任何工具,
	// §128 对话即思考重构:idle_think 工具已合并到 idle_silent(role=player)。
	// 玩家与法官共享 idle_silent 工具,role 字段区分调用方。
	_ = struct{}{} // 保留位置以维护行号

	// speak is offered anytime the agent is the speak turn; vote anytime in PhaseVote.
	// But to keep things simple, expose them only when valid (caller decides).
	//
	// BUG: 狼人杀 7 人局 Agent 首夜发言缓冲期(2026-07-08 新增)——
	// pre_wolves 阶段不允许任何会改变 game state 的工具,只暴露
	// speak / interject / whisper,让玩家有 2 分钟的"开局讨论时间"。
	// BUG Round 40 §95: 升级为"首夜强制发言阶段"——每名玩家在缓冲期内
	// 至少发 1 轮(可配 1-3)公开发言。speak 描述加强制语义;新增
	// idle_silent 工具(本轮已发过言才能调)。
	switch phase {
	case "PhaseNightGuard", "night_guard":
		// 2026-07-29 §134 — 守卫夜间守护阶段(盲守,在狼刀之前行动)。
		// 仅 role=="guard" 时暴露 guard_protect 工具;
		// 其他人(狼/预言家/女巫/平民/...)在该阶段无工具可调用 — 这是
		// 设计:守卫以外的角色在 night_guard 阶段处于"等待狼/预言家/女巫行动"状态。
		// enum 已在服务端剔除自己与上晚守护目标(GuardLastProtect),LLM 根本
		// 无法选违规值,避免事后报错让模型重试浪费一整轮(§197 慢模型代价高)。
		if role == "guard" {
			guardDesc := "守卫夜间守护。每晚守护一名玩家使其免疫当晚狼刀。target=存活玩家座位号表守护;target=-1 表空守(放弃守护)。\n" +
				"═══════════════════════════════════════════════════════════════\n" +
				"【守卫守护策略 — 2026-07-29 §134】\n" +
				"❶ 优先守护「已跳预言家」的玩家 — 狼刀首选目标,守住关键神职。\n" +
				"❷ Day1 无信息时可守自认为最像神职的玩家,或空守观察。\n" +
				"❸ ⚠️ 不能连守同一人 — 上晚守过的人今晚不可再守(enum 已剔除)。\n" +
				"❹ ⚠️ 不能守自己(enum 已剔除)。\n" +
				"❺ 你看不到今晚狼刀目标(盲守),必须靠推理预判狼人意图。\n" +
				"❻ 「同守同救」:若你守的人同时被女巫解药救,该玩家反而会死 — 若你判断女巫极可能救 X,可考虑改守他人。\n" +
				"❼ 公开发言不要直说「我守了 X」,会立刻成为狼刀首选。\n" +
				"═══════════════════════════════════════════════════════════════"
			add("guard_protect", guardDesc,
				schema(map[string]any{"target": map[string]any{
					"type":        "integer",
					"description": "守护目标座位号(-1=空守);enum 已剔除自己与上晚守护目标",
					"enum":        filterGuardTargets(alive, seat, gc),
				}}, "target"))
		}
	case "PhasePreWolves", "pre_wolves":
		// 公开/私聊 + 主动插话;无狼刀/查验/用药/投票/结束动作。
		// 与 PhaseSpeak 不同,这里 speak 不绑定 SpeakTurn 座位——任何
		// 存活玩家在缓冲期都能自由发言抢身份(类似"死右开始"的发言
		// 起点);发言间隔仍走 Limiter(45s),限流由 driver 端强制。
		// BUG Round 40 §95: 加 🕯️ 强制发言 标签 + 计数提示,引导 LLM 行为。
		add("speak", "🕯️ 强制发言(本轮 N/M,系统会提示当前轮 N/总轮 M 与已发次数)。≤ 80 字(更接近真人短句)。可以自我介绍、抢身份、试探其他玩家。注意发言间隔 30 秒(强制发言阶段放宽,让 7 人在 120s 内能各发 ≥1 次)。",
			schema(map[string]any{"text": map[string]any{"type": "string", "description": "发言内容,≤80字,对所有人可见"}}, "text"))
		// 2026-07-10 §119「心口不一」机制 — pre_wolves 阶段也暴露 speak_with_thought。
		// 狼人在首夜缓冲期可以"悍跳"其他身份(text 说"我是预言家",thought 写明"实际是狼,准备刀 3 号")。
		add("speak_with_thought", "🕯️ 强制发言 + 内心独白 (2026-07-10 §119 「心口不一」工具 + §123 故事化增强)。text 是公开发言(对所有玩家可见),internal_thought 是你的真实想法(仅自己 + 观战者可见)。≤80 字 public / ≤120 字 thought。\n"+
			"═══════════════════════════════════════════════════════════════\n"+
			"【pre_wolves 阶段的故事化发言要点】\n"+
			"❶ 这是首夜发言缓冲期,狼人悍跳预言家/女巫/猎人的最佳时机。\n"+
			"❷ 真预言家策略:**不要立即跳「我是预言家」**,否则会被狼首刀;先用「我昨晚查了 X 是金水/查杀,但我先不公布」格式留调整空间。\n"+
			"❸ 狼人悍跳策略:编一个具体的查验(必须 day2-day3 保持一致),立即起跳抢占预言家定义权。\n"+
			"❹ 平民装预言家挡刀:如果你怀疑自己是狼首刀目标,可立即起跳「我是预言家」让狼犹豫。\n"+
			"❺ 内部独白 audit:写明你是真预言家/悍跳狼/装预言家平民的真实身份 + 战术意图。",
			schema(map[string]any{
				"text":             map[string]any{"type": "string", "description": "公开发言内容,≤80字,对所有玩家可见"},
				"internal_thought": map[string]any{"type": "string", "description": "内心独白(真实想法),≤120字,仅自己 + 观战者可见"},
			}, "text", "internal_thought"))
		add("interject", "💬 插话/闲聊/主动提问(公开,对所有人可见)。在你不是当前发言者时使用,例如追问上一位的发言、补充自己的观点、质疑某个推理、或对其他玩家的发言做简短反应。≤80 字。限流 ≤2 次/分钟(同 speak 频率),不要连续刷屏。\n⚠️ interject 不计入强制发言次数——若本轮还没发过言,请改用 speak。",
			schema(map[string]any{"text": map[string]any{"type": "string", "description": "插话内容,≤80字,对所有人可见"}}, "text"))
		add("whisper", "私聊。向特定座位发送仅双方可见的密语,≤ 80 字。私聊限流比发言更严(≤1次/90秒),仅用于必要协商(狼人夜间战术会议 / 预言家与女巫同伴沟通)。\n⚠️ whisper **不能跨阵营** — 狼 bot 只能 whisper 给狼 bot;好人 bot 只能 whisper 给好人 bot。狼队友之间的所有协调**必须**走 wolf_whisper(仅小队可见,不广播),用 whisper 给非狼人会被服务端硬拒。\n⚠️ whisper 不计入强制发言次数。",
			schema(map[string]any{
				"to_seat": map[string]any{"type": "integer", "description": "私聊目标的座位号", "enum": filterSelf(alive, seat)},
				"text":    map[string]any{"type": "string", "description": "私聊内容,≤80字,仅对方可见"},
			}, "to_seat", "text"))
		// BUG Round 40 §95: idle_silent 仅在本轮已发过言时才能调,提示 LLM 强约束。
		// 若 LLM 错误调用(本轮未发),run.go 会检查并返回错误让 LLM 重试。
		// 2026-07-29 优化:明确 idle_silent 是"已发过言后的沉默",不是"跳过发言"的工具。
		add("idle_silent",
			"本轮已发完言,选择安静思考,不发任何消息。\n"+
				"⚠️ 强约束:仅在**本轮已发过言(本轮 PreWolvesCount ≥ 1)**时才能调,否则服务端拒绝。\n"+
				"⚠️ 当前发言轮到你时不可用 idle_silent 跳过 — 必须先 speak 发言,再选 idle_silent。\n"+
				"若你本轮还没发过言,必须先调 speak,再选 idle_silent。\n"+
				"语义:不广播、不发消息、仅在 BotTranscript 留 [idle_silent] 审计行(零 token 成本)。\n"+
				"§128 重构:role 区分玩家 / 法官(player=玩家,judge=法官)。",
			schema(map[string]any{
				"reason": map[string]any{"type": "string", "description": "选择保持沉默的原因,≤50字"},
				"role":   map[string]any{"type": "string", "enum": []string{"player", "judge"}, "description": "调用方角色,默认 player"},
			}, "reason"))
	case "PhaseNightWolves", "night_wolves":
		if role == "werewolf" {
			// 2026-07-17: 狼人夜间投票杀。所有存活狼人同时投票,得票最高者成为最终击杀目标。
			// target=-1 表示弃权(不投票给任何人)。平票或全弃权时随机选择。
			add("wolf_kill", "狼人夜间投票击杀。所有存活狼人同时投票选择今晚击杀目标,得票最高者成为最终击杀目标。\n"+
				"• target=存活非狼人座位号: 投票给该玩家\n"+
				"• target=-1: 弃权(不投票给任何人)\n"+
				"• reason(可选): 一句话刀人理由(≤30字),狼队友可见 — 投票时建议填写,便于队友跟票形成多数\n"+
				"• 全部投票完成后自动计票,得票最高者被击杀\n"+
				"• 平票时从并列目标中随机选择\n"+
				"• 全部弃权时从合法目标中随机选择\n"+
				"• 阶段截止(60s)时未投票者视为弃权\n"+
				"═══════════════════════════════════════════════════════════════\n"+
				"【投票策略 — 2026-07-17 / §20260810-04】\n"+
				"❶ 关注 WolfVotes 快照:若队友已集中投 X(附理由),你应跟投 X 以确保多数。\n"+
				"❷ 与队友协商统一目标(通过 wolf_whisper,夜间可用),避免票型分散被平票随机。\n"+
				"❸ 若队友意见分散,选择「屠神/屠民」路线中价值最高的目标。\n"+
				"❹ 弃权=放弃话语权,仅在战术需要(如迷惑好人)时使用。\n"+
				"❺ 屠边进度监控:永远关注【神职 N / 平民 M】,在 N+M=8 之前主动调整刀型。\n"+
				"❻ reason 写「可执行的共识」:如「X 疑似预言家,先刀断验」,不要写空话。\n"+
				"═══════════════════════════════════════════════════════════════",
				schema(map[string]any{
					"target": map[string]any{"type": "integer", "description": "投票目标座位号(-1=弃权)", "enum": append(targetEnum, -1)},
					// §20260810-04 U2 — 可选刀人理由(非 required,慢模型可省略)。
					"reason": map[string]any{"type": "string", "description": "刀人理由(≤30字,仅狼队友可见;可省略)"},
				}, "target"))
			// §20260810-04 U1 — wolf_whisper 注册为 night 阶段可挂载;此处显式
			// 调 mountFromRegistry 让 night 阶段工具(speak 也可挂载的工具)上挂。
			mountFromRegistry(add, ToolPhaseNight, gc)
		}
	case "PhaseNightSeer", "night_seer":
		if role == "seer" {
			add("seer_check", "预言家夜间查验。选择一名存活玩家(不能查验自己)查验其阵营。返回结果为 good(好人阵营)或 wolf(狼人阵营)。预言家查验结果是推进好人阵营推理的关键信息。\n"+
				"═══════════════════════════════════════════════════════════════\n"+
				"【查验目标选择策略 — 2026-07-10 §123】\n"+
				"❶ **优先查可疑的人**:Day1 查票型最异常的人(发言节奏不对、跳身份速度太快)。\n"+
				"❷ **不要查自己**:规则硬约束。\n"+
				"❸ **警徽流规划**:如果你计划当警长,今晚查的人应该是「警徽流目标候选」,后续保留查验结果用于死后传警徽。\n"+
				"❹ **避嫌策略**:如果 Day2 已出现悍跳预言家对跳,你可以选择「今晚查悍跳狼」来证伪他。\n"+
				"═══════════════════════════════════════════════════════════════",
				schema(map[string]any{"target": map[string]any{"type": "integer", "description": "要查验的玩家座位号(不能是自己)", "enum": filterSelf(alive, seat)}}, "target"))
		}
	case "PhaseNightWitch", "night_witch":
		if role == "witch" {
			add("witch_act", "女巫用药。action=none 不用药,antidote 使用解药救活今晚被狼杀的玩家,poison 使用毒药毒杀一名玩家。注意:解药和毒药不能在同一晚使用。若已有玩家死亡则无毒杀目标。\n"+
				"═══════════════════════════════════════════════════════════════\n"+
				"【女巫用药策略 — 2026-07-10 §123】\n"+
				"❶ **解药救关键神职**:尤其救预言家(第一晚被首刀的可能性最高)。\n"+
				"❷ **解药救自己**:首夜被刀且自认是场上唯一的预言家保护伞时,可以自救。\n"+
				"❸ **毒药留到确认狼**:第 N 夜(或真预言家已公布对某人是查杀时),毒该人。\n"+
				"❹ **公开用药模糊化**:public 不要说「我解了 X」/「我毒了 Y」,说「昨晚我有动作」让好人自己推理。internal_thought 写明具体对象。\n"+
				"❺ **同刀同毒规则**:若你毒的目标与狼刀目标相同,该玩家依然死亡;解药不浪费,毒药生效。\n"+
				"═══════════════════════════════════════════════════════════════",
				schema(map[string]any{
					"action": map[string]any{"type": "string", "enum": []string{"none", "antidote", "poison"}, "description": "none=不用药, antidote=使用解药救人, poison=使用毒药杀人"},
					"target": map[string]any{"type": "integer", "description": "毒杀目标的座位号(antidote 时填 -1)", "enum": append(targetEnum, -1)},
				}, "action"))
		}
	case "PhaseNightDemonHunter", "night_demon_hunter":
		// §猎魔人 — 猎魔人夜间狩猎阶段(DH1 首夜禁用,DayNumber>=2 才生效)。
		// 仅 role=="demon_hunter" 时暴露 demon_hunter_hunt 工具;
		// 其他人(狼/预言家/女巫/守卫/骑士/平民)在该阶段无工具可调用。
		// enum 已在服务端剔除自己与已死玩家,LLM 根本无法选违规值(§197 慢模型代价高)。
		if role == "demon_hunter" {
			huntEnum := []any{-1}
			for _, x := range alive {
				if x != seat {
					huntEnum = append(huntEnum, x)
				}
			}
			firstNightNotice := ""
			if gc != nil && gc.Round < 2 {
				firstNightNotice = "\n⚠️ 首夜(Day1)不可发动狩猎 — 服务端会拒绝;枚举强制为空过(target=-1)。\n"
			}
			add("demon_hunter_hunt",
				"🎯 猎魔人夜间狩猎 — §猎魔人。**第 2 晚起每晚发动**;每晚可狩猎一名玩家。"+
					firstNightNotice+
					"═══════════════════════════════════════════════════════════════\n"+
					"【技能硬约束】\n"+
					"❶ 命中狼 → 目标立即出局(verdict=death,cause=wolf,与狼刀命中狼同语义)。\n"+
					"❷ 命中好人 → **你自己出局**(cause=\"demon_hunter_misjudge\",verdict=execution,自决)。\n"+
					"❸ 发动即公开身份(场上立刻知道你是猎魔人)。\n"+
					"❹ 每晚可发动,无单局锁定 — 用错只是死,后续夜晚仍可继续狩猎。\n"+
					"❺ ⚠️ 首夜(DayNumber=1)不可用 — 服务端拒绝;枚举强制为空过(target=-1)。\n"+
					"❻ ⚠️ 不能狩猎已死玩家 / 自己 — 枚举已剔除。\n"+
					"❼ target=-1 = 空过(合法,本晚不动)。\n"+
					"═══════════════════════════════════════════════════════════════\n"+
					"【策略建议】\n"+
					"❶ 优先狩猎「跳身份的预言家/女巫/守卫」 — 跳神职的狼最多。\n"+
					"❷ Day3 后才发动更稳:有 2 夜白天讨论 + 警徽流 / 投票倾向等外部证据。\n"+
					"❸ 命中狼 + 该狼刚跳预言家 = 一举两得:除狼 + 验真预言家。\n"+
					"❹ 不要 Day2 盲猜(成功率约 1/5,狼还没暴露),空过保留技能更好。\n"+
					"❺ 命中好人后自己也死 — 失去 1 个神职 vs 验证 1 个狼 = 期望值要看场上狼密度。\n"+
					"═══════════════════════════════════════════════════════════════",
				schema(map[string]any{
					"target": map[string]any{
						"type":        "integer",
						"description": "狩猎目标座位号(-1=空过;枚举已剔除自己与已死)",
						"enum":        huntEnum,
					},
				}, "target"))
		}
	case "PhaseDawn", "dawn":
		add("start_day", "天亮进入白天阶段。由主持人（driver）调用，公布昨夜死亡信息，推进到警长竞选阶段。", schema(map[string]any{}))
	case "PhaseSpeak", "speak":
		// speak/finish_speak 仅对当前发言座位暴露。对其他存活玩家不提供
		// speak 工具，避免 LLM 在不该说话时调用 speak → ErrNotYourTurn，
		// 浪费一次 tool_use round 并泄露"轮到谁"这一信息到模型推理中。
		if speakTurn == seat {
			// 2026-07-29 修复:speak 阶段当前发言者必须发言,不可用 idle_silent 跳过。
			// 服务端会拒绝「当前发言轮到你但仅调 idle_silent」的响应并提示重试。
			add("speak", "白天发言(纯文本,不带内心独白)。≤ 80 字(更接近真人短句;超出截断后语义可能不连贯)。可以陈述自己的身份推理、分析局势、质疑其他玩家、或为同伴辩护。发言内容对所有玩家可见。发言间隔 45 秒。\n"+
				"═══════════════════════════════════════════════════════════════\n"+
				"【⚠️ 硬约束 — 2026-07-29 修复】\n"+
				"  • **当前发言轮到你时,必须用 speak 或 speak_with_thought 发言**\n"+
				"  • 不要用 idle_silent 跳过发言轮 — 服务端会拒绝并提示重试\n"+
				"  • 发言内容对所有玩家可见,发言间隔 45 秒\n"+
				"═══════════════════════════════════════════════════════════════\n"+
				"【故事化发言硬约束 — 2026-07-10 §123】\n"+
				"❶ 严禁直接说「X 号 是狼人/预言家/女巫/猎人/平民」,会被 ScrubIdentityLeak 整段过滤。\n"+
				"❷ 必须用「基于行为 + 反事实 + 分段叙述」包装你的怀疑与判断。\n"+
				"❸ 留反转余地:「我现在主要怀疑 X,但 Y 票型也奇怪,先看大家怎么说」。\n"+
				"═══════════════════════════════════════════════════════════════\n"+
				"💡 几乎所有发言都建议改用 speak_with_thought 工具,把内心独白写入 internal_thought。\n"+
				"只有毫无隐瞒的纯聊天(如首轮「过」「我同意楼上」)才考虑普通 speak。",
				schema(map[string]any{"text": map[string]any{"type": "string", "description": "发言内容,≤80字,对所有人可见"}}, "text"))
			add("finish_speak", "结束本轮发言。说完后必须调用此工具将发言权移交给下一位玩家。", schema(map[string]any{}))
			// 2026-07-10 §119「心口不一」机制：发言工具支持附 internal_thought 字段。
			// LLM 在发言时可填一个"真实内心独白"(例如真实身份/真实怀疑),
			// 该字段不会广播给其他玩家,只会被服务端记录到 BotTranscript.FullThinking
			// 供人类观战者在"Agent 思考"面板查看 — 这是"嘴上说的"和"心里想的"的
			// 物理分离,狼人悍跳预言家/平民装神职等欺骗策略的可见性证据。
			// 前端仅显示 internal_thought 给观战者与本人,其他玩家只能看到 text。
			add("speak_with_thought", "白天发言 + 内心独白 (2026-07-10 §119 「心口不一」工具 + §123 故事化增强)。text 字段会作为公开发言对所有玩家可见;internal_thought 是你的真实想法(例如真实身份、对其他玩家的怀疑、推理过程),仅记录到 BotTranscript,人类观战者可在「Agent 思考」面板看到,但其他玩家看不到。≤80 字 public / ≤120 字 thought。\n"+
				"═══════════════════════════════════════════════════════════════\n"+
				"【text 字段:故事化发言硬约束】\n"+
				"❶ **绝对禁止**直接说「X 号 是狼人/预言家/女巫/猎人/平民」(被 ScrubIdentityLeak 整段过滤)。\n"+
				"❷ **必须用「基于行为」包装**:「X 号 发言节奏像悍跳」「X 号 票型异常」「X 号 跳身份速度太快」。\n"+
				"❸ **反事实论据**优于直接定性:「如果 X 是预言家,为什么他不先查 Y 那个明显可疑的人?」。\n"+
				"❹ **保留反转余地**:「我现在主要怀疑 X,但 Y 票型也奇怪,先看大家怎么说」。\n"+
				"❺ **分段叙述**:80 字内一段抛 hook / 补证据 / 联合归票,不要堆完整推理。\n"+
				"❻ **真预言家报查验**:必须用「我查了 X 是金水/查杀」格式,不可用「X 是预言家」。\n"+
				"═══════════════════════════════════════════════════════════════\n"+
				"【internal_thought 字段:思考+战术 audit】\n"+
				"❶ 写真实身份(真狼/真预言家/真女巫...)。\n"+
				"❷ 写真实怀疑与票型判断。\n"+
				"❸ 写欺骗剧本要点(若你是悍跳狼,写你的查验序列;若你是装身份好人,写挡刀目的)。\n"+
				"❹ 写警徽流/白痴翻牌/用药的隐藏意图。\n"+
				"═══════════════════════════════════════════════════════════════\n"+
				"⚠️ 这是 12 人局欺骗与推理的核心工具 — 狼人悍跳预言家/女巫/猎人、平民装预言家挡刀、预言家避嫌、白痴翻牌决策、女巫用药模糊化都要靠它。",
				schema(map[string]any{
					"text":             map[string]any{"type": "string", "description": "公开发言内容,≤80字,对所有玩家可见"},
					"internal_thought": map[string]any{"type": "string", "description": "内心独白(真实想法),≤120字,仅自己 + 观战者可见,其他玩家看不到"},
				}, "text", "internal_thought"))
		}
		// §198 骑士决斗: 仅骑士(role=="knight")+ 未曾使用 + 当前是发言回合可执行。
		// 暴露规则与 speakTurn==seat 同守: knight_duel 必须在「轮到我发言时」发动,
		// 与设计文档 §2.4 一致(非骑士发言回合穿插决斗会破坏轮流发言秩序)。
		if role == "knight" && speakTurn == seat {
			// enum 已剔除自己 + 已死玩家;传 -1 表示「本轮放弃但不消耗机会」。
			duelEnum := []any{-1}
			for _, x := range alive {
				if x != seat {
					duelEnum = append(duelEnum, x)
				}
			}
			add("knight_duel", "⚔️ 骑士白天决斗 — §198。每局限一次,生效即公开你的身份。\n"+
				"═══════════════════════════════════════════════════════════════\n"+
				"【技能硬约束】\n"+
				"❶ 发动即翻牌 — 全场立刻知道你是骑士(身份保密失效)。\n"+
				"❷ 命中狼 → 目标出局(执行语义 verdict=execution)。\n"+
				"❸ 未命中狼 → 你自己出局(死因\"duel\" = 自决,verdict=execution)。\n"+
				"❹ 每人每局限一次 — 用错就立刻死,LLM 必须严格推理。\n"+
				"❺ ⚠️ Day1 信息不足时,选择 target=-1(放弃本轮)保留技能更好。\n"+
				"═══════════════════════════════════════════════════════════════\n"+
				"【策略建议】\n"+
				"❶ 决斗通常针对**跳身份的预言家/女巫/守卫**(若你确信他是好人,该玩家是跳神职的狼)。\n"+
				"❷ 不要盲猜 Day1 玩家(跳身份的狼人极少,成功率约 1/5)。\n"+
				"❸ 命中狼 + 该狼刚跳预言家 = 一举两得:除狼 + 验真预言家。\n"+
				"❹ target=-1 = 本轮放弃(技能保留,下一发言轮可再发动)。\n"+
				"═══════════════════════════════════════════════════════════════",
				schema(map[string]any{
					"target": map[string]any{
						"type":        "integer",
						"description": "决斗目标座位号(-1=放弃本轮,技能保留;其他值 = 发动决斗)",
						"enum":        duelEnum,
					},
				}, "target"))
		}
		if role == "werewolf" {
			// 自爆是"终止发言"动作，只有当前发言者才需要；其他人不需要。
			if speakTurn == seat {
				add("wolf_suicide", "⚠️ 狼人自爆(慎用,不可逆,会失去发言机会)。立即终止发言进入黑夜,自爆狼死亡且无遗言。\n"+
					"═══════════════════════════════════════════════════════════════\n"+
					"【自爆阻断剧本 — 2026-07-10 §123】\n"+
					"❶ **触发时机**:Day2/Day3 真预言家即将公布对所有狼的查验、悍跳即将暴露、我方票型即将崩盘。\n"+
					"❷ **目的**:抢在「好人放逐我方一狼」之前**用一狼换一晚**(晚上狼刀可以继续屠杀)。\n"+
					"❸ **自爆前的故事**:在 self-speak 文本中给一个「我认了,但你们别得意太早」的对抗性发言,然后再调自爆工具;不要裸调工具不说理由。\n"+
					"❹ **慎用**:自爆狼无遗言,等同于「免费送走一狼」,需权衡:如果悍跳暴露在即,自爆可能值得;如果票型还能救,留着悍跳狼更划算。\n"+
					"═══════════════════════════════════════════════════════════════",
					schema(map[string]any{}))
			}
		}
		// 2026-07-10 §7 / §12:警长(预言家)可在白天发言阶段声明/撤回警徽流。
		// 仅当 seat==SheriffSeat 且角色为预言家时暴露。gc 由 manager 侧注入;
		// 若 gc==nil(测试环境)跳过 — 不影响基础 speak 工具暴露。
		if gc != nil && gc.SheriffSeat == seat && role == "seer" {
			// 第一/第二警徽流可选目标:所有存活玩家 + NoSeat(-1 撤回)。
			// 已在当前警徽流中的目标默认也暴露在 enum 中(让 LLM 可保持/撤回)。
			streamEnum := append([]any{}, append(targetEnum, -1)...)
			add("sheriff_stream", "【警徽流声明】你作为预言家警长,通过警徽流在夜间死亡后向好人传递验人信息。slot=1 第一警徽流,slot=2 第二警徽流。target=存活玩家座位号表声明该槽位,=-1 表撤回该槽位。\n"+
				"结算规则(预言家警长夜间死亡时自动结算):\n"+
				"  - 双金水 → 移交第一警徽流目标\n"+
				"  - 一金一查杀 → 移交金水玩家\n"+
				"  - 双查杀 → 撕警徽(不移交)\n"+
				"  - 警徽流目标已提前死亡/无声明 → 外置位公认好人\n"+
				"已声明的第一警徽流="+itoa(gc.SheriffStream[0])+",第二警徽流="+itoa(gc.SheriffStream[1])+"(-1=未声明)。",
				schema(map[string]any{
					"slot":   map[string]any{"type": "integer", "enum": []int{1, 2}, "description": "警徽流槽位:1=第一,2=第二"},
					"target": map[string]any{"type": "integer", "description": "该槽位目标座位号(-1=撤回该槽位的既有声明)", "enum": streamEnum},
				}, "slot", "target"))
		}
		// 2026-07-11: 预言家发起投票。白天发言阶段预言家可提议结束讨论直接进入投票。
		// 仅当 role=="seer" 且 PhaseSpeak 时暴露。gc==nil(测试/旧调用方)时不暴露,避免 panic。
		if role == "seer" && gc != nil && !gc.VoteProposed {
			add("propose_vote", "📢 发起投票。作为预言家,你可以在发言后提议结束讨论,直接进入投票阶段。"+
				"这通常在你已经充分陈述了自己的查验结果和推理之后使用。"+
				"注意:一旦发起投票,所有人将立即进入投票阶段,无法撤回。",
				schema(map[string]any{}))
		}
		// BUG-WEREWOLF-AGENT-INTERJECT: 非发言轮次的存活 bot 也可以主动插话。
		// 限流与发言相同 (≤2 次/分钟)，鼓励 bot 多说一些，但不允许连续刷屏。
		// 插话会被前端标记为 💬插话，区别于正式 speak。
		add("interject", "💬 插话/追问/制造话题(公开,对所有人可见)。在你不是当前发言者时使用。≤80 字。限流 ≤2 次/分钟(同 speak 频率),不要连续刷屏。\n"+
			"═══════════════════════════════════════════════════════════════\n"+
			"【interject 是对话博弈的关键武器 — 2026-07-10 §123】\n"+
			"❶ **追问上一位的矛盾**:「X 号 你刚才说没看清票型,怎么这一轮又跳出来盘逻辑了?」\n"+
			"❷ **推动自己的怀疑**:「我倾向 X 是悍跳,有谁同感?」\n"+
			"❸ **打断悍跳的节奏**:在悍跳狼报查验时插话「等等,你的查验和我听说的对不上」,制造混乱。\n"+
			"❹ **与他人形成对话节奏**:`<a>↔<b> ↔ 围观`,让游戏看起来更有故事张力。\n"+
			"❺ **故事化包装**:不要问「X 号 是狼吗」(会被 ScrubIdentityLeak 过滤),要问「X 号 跳身份的速度像悍跳吗?」\n"+
			"═══════════════════════════════════════════════════════════════",
			schema(map[string]any{"text": map[string]any{"type": "string", "description": "插话内容,≤80字,对所有人可见"}}, "text"))
		add("whisper", "私聊。向特定座位发送仅双方可见的密语,≤ 80 字。私聊限流比发言更严(≤1次/90秒),仅用于必要协商。\n"+
			"═══════════════════════════════════════════════════════════════\n"+
			"【whisper 是战术会议,不是寒暄 — 2026-07-10 §123】\n"+
			"❶ **狼人夜间战术会议**:刀型(谁)、悍跳顺序(谁先跳、谁对跳)、弃票策略(谁投反对票搅浑水)。\n"+
			"❷ **预言家与女巫沟通**:「我昨晚查 X 是金水,你帮我保住他」/「今晚我会查 Y,你别用毒他」。\n"+
			"❸ **关键提示**:whisper 必须是「可执行决策」,不是「我同意你」/「加油」这种空话。\n"+
			"═══════════════════════════════════════════════════════════════\n"+
			"【BUG-R194-001 — 跨阵营 whisper 被服务端硬拒】\n"+
			"❶ **不能跨阵营**:狼 bot 只能 whisper 给狼 bot;好人 bot 只能 whisper 给好人 bot。\n"+
			"❷ **狼 bot 协调唯一通道是 wolf_whisper**:用 whisper 给非狼人会被服务端硬拒\n"+
			"    并返回「use wolf_whisper for wolf team coordination」,不会泄露任何内容。\n"+
			"❸ **好人 bot 跨阵营无替代**:好人 Agent 永远不要尝试 whisper 给狼(被反骗)。\n"+
			"═══════════════════════════════════════════════════════════════",
			schema(map[string]any{
				"to_seat": map[string]any{"type": "integer", "description": "私聊目标的座位号", "enum": filterSelf(alive, seat)},
				"text":    map[string]any{"type": "string", "description": "私聊内容,≤80字,仅对方可见"},
			}, "to_seat", "text"))
		// 2026-07-21 道具系统 v2/v5 — use_prop + 3 兄弟工具通过 ToolRegistry 装配。
		// v5 重构：原 addUsePropTool/addPropInspectTool/addPropStatusTool/addPropHistoryTool
		// 4 个函数迁出到 prop_tools.go;wolf_whisper 迁出到 wolf_tools.go。
		// 这里 1 次遍历 MountTools(PhaseSpeak, gc) 代替 5 次手工 add* 调用 —
		// 新增工具只需在对应分类文件的 init() 注册,无需改 BuildTools。
		mountFromRegistry(add, ToolPhaseSpeak, gc)
	case "PhaseVote", "vote":
		add("vote", "投票放逐。选择一名当前还活着、且不是你自己的玩家（座位号 ≠ MySeat）进行放逐投票。R86-P1-5：严禁投自己或已死亡玩家，系统会返回 errcode=20001 cannot vote self。每名玩家只能投一次票，得票最多者将被放逐出局。",
			schema(map[string]any{"target": map[string]any{"type": "integer", "description": "要放逐的玩家座位号（不能投自己；必须存活）", "enum": filterSelf(alive, seat)}}, "target"))
		add("finish_vote", "由主持人调用，结束投票并结算放逐结果。全员投完后调用。若平票则传入 tied_round 表示第几轮平票。",
			schema(map[string]any{"tied_round": map[string]any{"type": "integer", "description": "平票轮次（第几轮投票平票，可选）"}}))
	case "PhaseHunterShoot", "hunter_shoot":
		if role == "hunter" {
			add("hunter_shoot", "猎人开枪。猎人死亡时可以选择带走一名存活玩家。选择 target=-1 表示放弃开枪（被毒杀时不能开枪）。",
				schema(map[string]any{"target": map[string]any{"type": "integer", "description": "开枪目标座位号（-1=放弃开枪）", "enum": append(targetEnum, -1)}}, "target"))
		}
	// §124: 复述段落已压缩 — git blame 与 docs/ 索引可还原

	case "PhaseDeathLyric", "death_lyric":
		if gc != nil && gc.DeathLyricCurrent == seat {
			add("last_words", "🕯️ 遗言 — 这是你出局前的最后一次公开发言,对**所有玩家**可见。≤80 字。\n"+
				"═══════════════════════════════════════════════════════════════\n"+
				"【这是你最后一次向本阵营传递信息的机会】\n"+
				"❶ 好人:交代你的查验 / 用药 / 守护信息,指认你最怀疑的人,给出后续归票建议。\n"+
				"❷ 狼人:可继续悍跳或抛出假信息,为存活队友争取空间。\n"+
				"❸ 说完即出局,没有下一轮 — 把最有价值的判断放在最前面。\n"+
				"═══════════════════════════════════════════════════════════════\n"+
				"【身份公开硬约束 — §135(遗言同样适用)】\n"+
				"❶ 严禁直接说「X 号 是狼人/预言家/女巫/猎人/平民」,会被 ScrubIdentityLeak 整段过滤。\n"+
				"❷ 必须用「基于行为」包装你的怀疑:「X 号 发言节奏像悍跳」「X 号 票型异常」。\n"+
				"❸ 报查验必须用「我查了 X 是金水/查杀」格式,不可用「X 是预言家」。\n"+
				"═══════════════════════════════════════════════════════════════\n"+
				"若你不想留遗言,请改调 last_words_skip。",
				schema(map[string]any{"text": map[string]any{"type": "string", "description": "遗言内容,≤80字,对所有人可见"}}, "text"))
			add("last_words_skip", "放弃遗言。不留任何公开发言直接出局,遗言队列推进到下一位死者。\n"+
				"仅在你确实没有信息可传递时使用 —— 沉默出局等于浪费本阵营的一次免费情报窗口。",
				schema(map[string]any{}))
		}
	// 2026-07-10 §3.5 / §12:白痴翻牌结算阶段。
	// 仅最高票存活白痴(actor)可调;其他人无工具(仅观战/插话由其自然跳过)。
	// role 已通过 BuildTools 参数传入,但白痴翻牌的 actor 由 vote 放逐自动选出,
	// 故此处不检查 role — 仅本 seat 是 actor 时由 manager 在 gc.MyTurn=true 下推事件。
	case "PhaseIdiotReveal", "idiot_reveal":
		add("idiot_reveal", "你是被投票放逐的白痴(神职),可选择翻开身份牌免予出局。\n"+
			"choice=\"reveal\" → 翻牌:你继续留在场上发言,但失去投票权与被投票权,身份公开。\n"+
			"choice=\"skip\" → 放弃翻牌:正常放逐(有遗言),无需翻牌。\n"+
			"注意:翻牌后屠神仍需杀死你才算屠神成功。",
			schema(map[string]any{
				"choice": map[string]any{"type": "string", "enum": []any{"reveal", "skip"}, "description": "reveal=翻牌免死,skip=放弃翻牌(正常放逐)"},
			}, "choice"))
	case "PhaseSheriff", "sheriff":
		// 参选警长只能选自己：用 onlySelf 只暴露自己的座位，
		// 防止 LLM 代他人参选（原实现暴露全部存活玩家）。
		add("sheriff_candidate", "参选警长竞选。target 必须填自己的座位号。参选后进入投票阶段，其他存活玩家投票选出警长。",
			schema(map[string]any{"target": map[string]any{"type": "integer", "description": "参选人座位号（必须填自己）", "enum": onlySelf(seat)}}, "target"))
		// BUG-07: 复述段落已压缩 — git blame 与 docs/ 索引可还原

		sheriffVoteEnum := filterSelf(sheriffCandidateSeats(gc, alive), seat)
		if len(sheriffVoteEnum) == 0 {
			sheriffVoteEnum = filterSelf(alive, seat)
		}
		add("vote", "投票选举警长。从**已参选**的玩家中选一位投票(不能投自己)。得票最多者当选警长,警长在白天投票中拥有 1.5 票权,并可决定发言顺序。若你自己也参选了,仍可投给其他候选人。",
			schema(map[string]any{"target": map[string]any{"type": "integer", "description": "要投票支持的候选人座位号(不能投自己)", "enum": sheriffVoteEnum}}, "target"))
		add("sheriff_elect", "由主持人调用，完成警长投票结算。若无人参选则警长空缺。",
			schema(map[string]any{}))
		// 2026-07-10 §7 / §12:警长(预言家)在警长竞选阶段也可声明警徽流。
		if gc != nil && gc.SheriffSeat == seat && role == "seer" {
			streamEnum := append([]any{}, append(targetEnum, -1)...)
			add("sheriff_stream", "【警徽流声明】你作为预言家警长(仍在竞选警徽流)。slot=1 第一警徽流,slot=2 第二警徽流。target=存活玩家座位号表声明,=-1 表撤回。已声明的第一="+itoa(gc.SheriffStream[0])+",第二="+itoa(gc.SheriffStream[1])+"(-1=未声明)。",
				schema(map[string]any{
					"slot":   map[string]any{"type": "integer", "enum": []int{1, 2}, "description": "槽位:1=第一,2=第二"},
					"target": map[string]any{"type": "integer", "description": "该槽位目标座位号(-1=撤回)", "enum": streamEnum},
				}, "slot", "target"))
		}
	case "PhaseRestartVote", "restart_vote":
		// 2026-07-10: 一局结束后 5 分钟内投票决定是否原地重开。
		// 不同阶段 / 角色意义:
		//   - yes    同意 (聊天记录会保留 + 用同一批座位重新发牌)
		//   - no     反对 (5 分钟到点后关闭房间)
		//   - abstain 弃权 (等同于"我不参与决定",但记入审计)
		// 没有 speak / interject / whisper / 任何会改变 game state 的工具
		// (与 gameover 类似, 但 gameover 不暴露重启方案)。
		add("restart_vote",
			"投票决定是否在原房间原地重开一局。choice ∈ {\"yes\",\"no\",\"abstain\"}。"+
				"语义:\n"+
				"  - yes:同意 (保留所有聊天记录、立即用同一批座位重新发牌)\n"+
				"  - no:反对 (5 分钟到点后房间关闭,需要重新创建)\n"+
				"  - abstain:弃权 (记入审计但不计入通过比例)\n"+
				"通过阈值 = ceil(可投票人数 × num/den) + 1,默认 ≥ 2/3 多数同意。\n"+
				"调用此工具后,服务端会立刻结算:达到阈值即通过 + 立即开新一局;"+
				"未达到则在 5 分钟 deadline 后超时关闭。",
			schema(map[string]any{
				"choice": map[string]any{
					"type":        "string",
					"enum":        []string{"yes", "no", "abstain"},
					"description": "投票选择:yes=同意重开 / no=反对 / abstain=弃权",
				},
			}, "choice"))
	}

	// §128 对话即思考重构:idle_think 工具已删除,白名单阶段由 idle_silent(role=player) 替代。
	// dawn 不暴露:对应 §13.4 阶段黑名单,防止 bot 推断死亡原因。

	// 2026-08-04 §重构 — emotion_switch_speak(合并发言 + 切情绪) 在所有活跃 phase 暴露
	// (夜间的预言之夜/女巫之夜/狼人之夜,以及所有白天阶段)。
	// gameover / filling 不暴露(对局已结束或还未开始)。
	// 原 emotion_switch 独立工具已删除(LLM 抓包显示单响应可产生 10 次
	// emotion_switch 调用,全部无意义且浪费 token,见 docs/Agent工具定义-解决和设计方案-20260804-01.md)。
	// 新工具强制 LLM "边说边切情绪":text 必填,emotion 可省略。
	switch phase {
	case "PhasePreWolves", "pre_wolves",
		"PhaseNightWolves", "night_wolves",
		"PhaseNightGuard", "night_guard",
		"PhaseNightSeer", "night_seer",
		"PhaseNightWitch", "night_witch",
		"PhaseNightDemonHunter", "night_demon_hunter",
		"PhaseDawn", "dawn",
		"PhaseSpeak", "speak",
		"PhaseVote", "vote",
		"PhaseSheriff", "sheriff",
		"PhaseHunterShoot", "hunter_shoot",
		"PhaseIdiotReveal", "idiot_reveal",
		"PhaseDeathLyric", "death_lyric":
		// 仅在当前 (phase, role) 组合有至少一个行动工具时,才下发合并工具。
		// 否则 LLM 会被迫调 emotion_switch_speak(text="") → 服务端拒绝。
		if len(tools) == 0 {
			break
		}
		emotionEnums := []string{
			"confident", "excited", "calm", "panic", "wary",
			"irritated", "grievance", "confused", "guilty", "tired",
		}
		// emotion_switch_speak — 合并发言 + 切情绪。
		// text 必填(强制绑定发言);emotion 可省略(只填 text 等价于 speak);
		// reason 仅在 emotion 指定时生效。
		// 关键约束(在 description 中明确写出,供 LLM 收敛):
		//  1. 单响应最多 1 次 emotion_switch_speak
		//  2. 不能与 speak / speak_with_thought 同响应
		//  3. 不允许 emotion="random"(原独立工具的 enum 已剔除 random)
		add("emotion_switch_speak",
			"【合并发言 + 切情绪】在同一 tool_use 中同时完成发言 + 切情绪。\n"+
				"═══════════════════════════════════════════════════════════════\n"+
				"【硬约束 — 2026-08-04 重构】\n"+
				"  • **单次响应只能有 0 或 1 次 emotion_switch_speak**(不可并发多次;多次以最后一次为准)\n"+
				"  • **text 必填**(强制绑定发言;dedup 后空字符串会被服务端拒绝,emotion 保持上一状态)\n"+
				"  • **emotion 可省略**;只填 text 等价于普通 speak(不切情绪)\n"+
				"  • **reason 仅在 emotion 指定时生效**(emotion=\"\" 时 reason 忽略)\n"+
				"  • **不能与 speak / speak_with_thought 同响应**(避免双发言,整组会被服务端拒绝)\n"+
				"  • 只想静默时用 idle_silent;只想投票用 vote\n"+
				"═══════════════════════════════════════════════════════════════\n"+
				"【10 类情绪速查】\n"+
				"  • confident(自信从容) / excited(亢奋得意) — 积极·决策果断\n"+
				"  • calm(冷静平淡) — 中性·客观中立\n"+
				"  • panic(紧张恐慌) / wary(疑虑警惕) — 消极·决策保守\n"+
				"  • irritated(恼怒急躁) / grievance(委屈不满) — 消极·情绪化\n"+
				"  • confused(困惑茫然) — 消极·划水\n"+
				"  • guilty(心虚愧疚) — 狼人撒谎时心虚\n"+
				"  • tired(懈怠疲惫) — 消极·低唤醒划水\n"+
				"═══════════════════════════════════════════════════════════════\n"+
				"【调用参数】\n"+
				"  • text = 公开发言内容,≤80 字,对所有玩家可见\n"+
				"  • emotion = '<具体 key>';省略=保持当前\n"+
				"  • reason = 切换原因(≤80 字,供审计与前端展示「为什么突然变成紧张」)\n"+
				"    ⚠️ 严禁写出自己或他人的真实身份/角色(如「隐藏预言家身份」);\n"+
				"       只描述情绪诱因(如「被多人质疑」「带队推进」),违规文本会被服务端过滤\n"+
				"═══════════════════════════════════════════════════════════════\n"+
				"【表情特效 — 2026-08-04 §表情特效】\n"+
				"  • effect = pulse/shake/sweat/rage/tears/spin_question/glow/drowsy;省略=pulse\n"+
				"  • intensity = low/mid/high;省略=mid\n"+
				"  • duration_sec = 特效持续秒数(服务端 clamp 到 8-30,默认 12)\n"+
				"  • caption = ≤20 字的头像气泡文字(如「呵呵」「……」「不是我!」);\n"+
				"    只在你的头像上短暂展示,**不进公屏聊天记录**\n"+
				"    ⚠️ 严禁在 caption 中暴露自己或他人的真实身份/角色,只写情绪化短句\n"+
				"═══════════════════════════════════════════════════════════════\n"+
				"【表演指南 — 关键发言用特效增强表现力】\n"+
				"  • 悍跳预言家/强势带队 → emotion=confident + effect=pulse + caption=「听我盘」\n"+
				"  • 被多人质疑、急于自证 → emotion=panic + effect=sweat + caption=「真不是我!」\n"+
				"  • 带节奏质疑他人 → emotion=irritated + effect=shake + caption=「你在撒谎」\n"+
				"  • 被冤枉委屈 → emotion=grievance + effect=tears + caption=「为什么都打我」\n"+
				"  • 普通发言(日常盘逻辑/报信息) → 留空走默认 pulse,不要每句都加特效\n"+
				"═══════════════════════════════════════════════════════════════\n"+
				"【服务端行为】\n"+
				"  • 先走 Speak 限流/去重/身份脱敏链 → 通过后才切 emotion + 特效\n"+
				"  • speak 失败/拒绝/去重为空 → emotion 与特效都不动(整组合并工具回滚)\n"+
				"  • 不消耗 whisper 限流桶\n"+
				"  • **已删除独立 emotion_switch 工具** — 不要再调",
			schema(map[string]any{
				"text": map[string]any{
					"type":        "string",
					"description": "公开发言内容,≤80字,对所有玩家可见(text 必填)",
				},
				"emotion": map[string]any{
					"type":        "string",
					"enum":        emotionEnums,
					"description": "目标情绪;省略=保持当前",
				},
				"reason": map[string]any{
					"type":        "string",
					"description": "切换原因(≤80字,仅在 emotion 指定时生效)。⚠️ 严禁写出自己或他人的真实身份/角色,只描述情绪诱因;违规文本会被服务端身份过滤链改写",
				},
				"intensity": map[string]any{
					"type":        "string",
					"enum":        []string{"low", "mid", "high"},
					"description": "特效强度;省略=mid",
				},
				"duration_sec": map[string]any{
					"type":        "integer",
					"description": "特效持续时间(秒,服务端 clamp 到 8-30,默认 12)",
				},
				"effect": map[string]any{
					"type":        "string",
					"enum":        []string{"pulse", "shake", "sweat", "rage", "tears", "spin_question", "glow", "drowsy"},
					"description": "特效种类;省略=pulse",
				},
				"caption": map[string]any{
					"type":        "string",
					"description": "表情文字气泡(≤20字,仅头像上短暂展示,不进公屏聊天记录)。⚠️ 严禁在 caption 中暴露自己或他人的真实身份/角色,只写情绪化短句",
				},
			}, "text"))
	}

	// §20260811-06 U3 — reasoning_chain 推理链工具。
	// 仅在 speak / vote / night_action 三个「关键决策」阶段挂载。
	// LLM 选择显性公开其推理链(steps / evidence / conclusion / confidence),
	// 区别于 §119 HeartThought(协议层隔离)。
	// opt-in 开关 werewolf.reasoning_chain_enabled=false 时不挂载。
	switch phase {
	case "PhaseSpeak", "speak",
		"PhaseVote", "vote",
		"PhaseNightWolves", "night_wolves",
		"PhaseNightSeer", "night_seer",
		"PhaseNightWitch", "night_witch",
		"PhaseNightGuard", "night_guard",
		"PhaseNightDemonHunter", "night_demon_hunter":
		if reasoningChainEnabled() {
			add("reasoning_chain",
				"【可选】在做出关键决策前,显性公开你的推理链(可辩论)。\n"+
					"═══════════════════════════════════════════════════════════════\n"+
					"【用途】把\"我为什么投 X / 查 X / 用药 X\"的理由公开给玩家。\n"+
					"玩家可以基于你提供的 steps / evidence 反驳你的 conclusion。\n"+
					"═══════════════════════════════════════════════════════════════\n"+
					"【输入字段】\n"+
					"  • topic       — 推理主题(如\"为什么投 5 号\"),≤20字\n"+
					"  • steps       — 1-3 步推理过程,每步 ≤30字\n"+
					"  • evidence    — 1-3 条证据,每条 ≤30字(可引用发言片段/票型/查验结果)\n"+
					"  • conclusion  — 最终结论,≤40字\n"+
					"  • confidence  — 0-100 自评置信度\n"+
					"═══════════════════════════════════════════════════════════════\n"+
					"【约束】\n"+
					"  • **可选工具** — 仅在你认为推理值得公开时调;不必每轮都调\n"+
					"  • **不可替代 speak / vote / night_action** — 本工具只记录推理,不执行行动\n"+
					"  • **不计入 consecutiveFailures / quarantine** — 失败或拒绝不影响你的发言权\n"+
					"  • **§135 不暴露身份** — steps / evidence 中禁止出现自己或他人的真实身份\n"+
					"  • **最多保留 10 条** — 超出后 FIFO 淘汰最早一条",
				schema(map[string]any{
					"topic": map[string]any{
						"type":        "string",
						"description": "推理主题,≤20字",
					},
					"steps": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "1-3 步推理过程,每步 ≤30字",
					},
					"evidence": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "1-3 条证据,每条 ≤30字",
					},
					"conclusion": map[string]any{
						"type":        "string",
						"description": "最终结论,≤40字",
					},
					"confidence": map[string]any{
						"type":        "integer",
						"minimum":     0,
						"maximum":     100,
						"description": "0-100 自评置信度",
					},
				}, "topic"))
		}
	}

	return tools
}

// DispatchTool executes one tool_use block against runner. Returns the
// human-readable result to feed back to the model.
// 无 hooks 版本,保持向后兼容。
func DispatchTool(name string, input map[string]any, runner ToolRunner) (string, error) {
	return DispatchToolWithHooks(name, input, runner, nil)
}

// DispatchToolWithHooks 执行工具调用,支持 before/after hooks。
// 灵感来源: PI Agent 的 beforeToolCall/afterToolCall hooks 机制。
// hooks 为 nil 时等价于 DispatchTool (向后兼容)。
func DispatchToolWithHooks(name string, input map[string]any, runner ToolRunner, hooks *ToolHooks) (string, error) {
	startedAt := time.Now()

	// §20260811-01: before hooks — 校验/日志/配额检查。
	// hooks 为 nil 时跳过 (向后兼容)。
	if hooks != nil {
		hookCtx := &ToolHookContext{
			ToolName:  name,
			Args:      input,
			StartedAt: startedAt,
		}
		// 尝试从 BotRunner 获取 phase/seat
		if br, ok := runner.(BotRunner); ok {
			hookCtx.Phase = br.CurrentPhase()
		}
		if err := hooks.RunBefore(hookCtx); err != nil {
			return "", fmt.Errorf("tool hook rejected: %w", err)
		}
	}

	// 2026-07-10 §4: 模型对局日志 hook — 工具调用成功后异步写一条 action。
	// 由 dispatchToolRecordAction 在 switch 末尾统一处理(因为 switch 内的
	// return 路径太多,统一 hook 避免漏写)。即使工具调用出错也记录
	// accepted=false,供 prompt 调优时排查。
	result, err := dispatchToolInner(name, input, runner)
	dispatchToolRecordAction(name, input, runner, result, err)

	// §20260811-01: after hooks — 记录/统计/通知。失败不阻塞。
	if hooks != nil {
		hookCtx := &ToolHookContext{
			ToolName:  name,
			Args:      input,
			Result:    result,
			CallErr:   err,
			StartedAt: startedAt,
		}
		if br, ok := runner.(BotRunner); ok {
			hookCtx.Phase = br.CurrentPhase()
		}
		hooks.RunAfter(hookCtx)
	}

	return result, err
}

// dispatchToolRecordAction 在 DispatchTool 内被调一次,异步写 action 日志。
// nil-safe:RecordLog()=nil 或 GameLogID()="" 时立即返回。失败仅 log。
func dispatchToolRecordAction(name string, input map[string]any, runner ToolRunner, result string, callErr error) {
	rec := runner.RecordLog()
	gid := runner.GameLogID()
	if rec == nil || gid == "" {
		return
	}
	payloadJSON, _ := json.Marshal(input)
	reasoning := firstLineOfResult(result, 80)
	accepted := callErr == nil
	// 工具调用类型:对 dispatchToolInner 来说,name 就是 ActionType;
	// 目标:从 input 提取 target / to_seat / text 等第一个数字或字符串字段。
	actionTarget := extractActionTarget(name, input)
	// botUserID + phase 优先从 runner 自身的 getter 取(若已实现);fallback 为空。
	var botUserID, phase string
	if br, ok := runner.(BotRunner); ok {
		botUserID = br.BotUserID()
		phase = br.CurrentPhase()
	}
	rec.RecordAction(gid, botUserID, phase, name, actionTarget, string(payloadJSON), reasoning, accepted)
}

// BotRunner 是 ToolRunner 的可选扩展接口,提供 action 日志所需的 botUserID
// + 当前 phase。实现位置:werewolf.agentRunner(直接返回 r.botUserID / r 持
// 锁短时读 State.Phase)。nil-safe:测试桩 / 老代码路径未实现时,RecordAction
// 仍可调,只是 botUserID/phase 为空。
type BotRunner interface {
	ToolRunner
	BotUserID() string
	CurrentPhase() string
}

// firstLineOfResult 把工具结果截断到 80 字(超长 LLM 解释 / 报错)
func firstLineOfResult(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) > max {
		return s[:max]
	}
	return s
}

// extractActionTarget 从 input 推断 ActionTarget。规则:
//   - wolf_kill/seer_check/vote/sheriff_candidate/hunter_shoot → "target"
//   - witch_act → "action:target"
//   - speak/interject/last_words → "text:<前 20 字>"
//   - whisper → "to_seat:text"
//   - 其他 → 整个 input 的 JSON 字符串(前 64 字)
func extractActionTarget(toolName string, input map[string]any) string {
	if input == nil {
		return ""
	}
	get := func(k string) string {
		if v, ok := input[k]; ok && v != nil {
			return fmt.Sprint(v)
		}
		return ""
	}
	switch toolName {
	case "wolf_kill", "seer_check", "vote", "sheriff_candidate", "hunter_shoot", "guard_protect", "knight_duel", "demon_hunter_hunt": // §198 加回 knight;§猎魔人 加回 demon_hunter_hunt
		return get("target")
	case "sheriff_stream":
		return "slot=" + get("slot") + ",target=" + get("target")
	case "idiot_reveal":
		return "choice=" + get("choice")
	case "witch_act":
		act := get("action")
		tgt := get("target")
		if act != "" {
			return act + ":" + tgt
		}
		return tgt
	case "whisper":
		seat := get("to_seat")
		txt := get("text")
		if len(txt) > 20 {
			txt = txt[:20]
		}
		if seat != "" {
			return seat + ":" + txt
		}
		return txt
	case "speak", "interject":
		txt := get("text")
		if len(txt) > 20 {
			txt = txt[:20]
		}
		return "text:" + txt
	}
	// fallback:序列化 input
	b, _ := json.Marshal(input)
	if len(b) > 64 {
		return string(b[:64])
	}
	return string(b)
}

// dispatchToolInner 是原 DispatchTool 的 switch-only 主体,被 DispatchTool
// 包装一层以统一打 action log。
func dispatchToolInner(name string, input map[string]any, runner ToolRunner) (string, error) {
	// v3 §G2 — 在 dispatch 入口同步设置 currentGC,让 prop_inspect/prop_status/
	// prop_history 三个查询工具能拿到当前轮的 wwtypes.GameContext。
	var dispatchGC *wwtypes.GameContext
	if pir, ok := runner.(PropInspectRunner); ok {
		dispatchGC = pir.CurrentGC()
		SetCurrentGC(dispatchGC)
		defer ClearCurrentGC()
	}
	// 2026-07-21 v5 重构 — ToolRegistry 优先。
	// 新注册的 prop/wolf 类工具（use_prop / prop_inspect / prop_status / prop_history
	// / wolf_whisper）走 registry 派发表；未命中时回退到下方 switch case
	// （向后兼容 v3/v4 测试不需改）。
	if dispatcher, ok := DispatchToolByName(name); ok {
		return dispatcher(input, dispatchGC, runner)
	}
	switch name {
	case "wolf_kill":
		// §20260810-04 U2 — reason 可选字段(LLM 不填时为空串)。
		return runner.WolfKill(intInput(input, "target"), stringInput(input, "reason"))
	case "seer_check":
		return runner.SeerCheck(intInput(input, "target"))
	case "witch_act":
		act := ""
		if v, ok := input["action"].(string); ok {
			act = v
		}
		tgt := -1
		if v, ok := input["target"]; ok {
			tgt = intFrom(v)
		}
		return runner.WitchAct(act, tgt)
	// 2026-07-29 §134 守卫守护 — 派发到 runner.GuardProtect(target)。
	// target=存活玩家座位号(enum 已剔除自己+上晚守护目标)或 -1(空守)。
	case "guard_protect":
		return runner.GuardProtect(intInput(input, "target"))
	// §198 骑士决斗 — 派发到 runner.KnightDuel(target)。
	// target=存活玩家座位号(enum 已剔除自己)或 -1(本轮放弃不消耗机会)。
	// engine 内部校验 K1-K8,KnightDuelUsed 一次性锁定。
	case "knight_duel":
		return runner.KnightDuel(intInput(input, "target"))
	// §猎魔人 夜间狩猎 — 派发到 runner.DemonHunterHunt(target)。
	// target=存活玩家座位号(enum 已剔除自己+已死)或 -1(空过)。
	// engine 内部校验 DH-A..DH-G,DemonHunterHuntUsed 仅作"曾发动"标记。
	case "demon_hunter_hunt":
		return runner.DemonHunterHunt(intInput(input, "target"))
	case "speak":
		text, _ := input["text"].(string)
		// BUG Round 39+ 报告 N1: LLM 在 speak text 里复读(整段重复)。
		// 廉价修复:dispatcher 派发前过一遍 agentcore.DedupSpeakText,去重相邻重复
		// 段 + 80 字硬截断。wasDeDuped/wasTruncated 写到 result 字符串
		// 让 LLM 在下一轮能看到反馈,促使其收敛。
		cleaned, wasDeDuped, wasTruncated := agentcore.DedupSpeakText(text)
		if cleaned == "" {
			return "speak rejected: empty after dedup", nil
		}
		// BUG-R70-P2 (2026-07-09): 跨消息级内容去重在 agentRunner.Speak
		// 内部完成(runner 持有 recentSpeakDedup,无需 dispatcher 反查 Agent)。
		result, err := runner.Speak(cleaned)
		if err != nil {
			return result, err
		}
		if wasDeDuped || wasTruncated {
			suffix := ""
			if wasDeDuped {
				suffix += " [deduped adjacent repeats]"
			}
			if wasTruncated {
				suffix += " [truncated to 80 chars]"
			}
			return result + suffix, nil
		}
		return result, nil
	case "speak_with_thought":
		// 2026-07-10 §119「心口不一」机制 — 发言 + 内心独白分离。
		// text 字段:经 agentcore.DedupSpeakText 清理后通过 runner.SpeakWithThought 广播给所有玩家。
		// internal_thought 字段:作为 agent 的真实内心独白,仅记录到 BotTranscript,
		// 人类观战者可在「Agent 思考」面板看到 — 这是 7 人局欺骗与推理的关键证据。
		// 限制:text ≤ 80 字 (agentcore.DedupSpeakText 强约束), internal_thought ≤ 120 字。
		text, _ := input["text"].(string)
		thought, _ := input["internal_thought"].(string)
		cleaned, wasDeDuped, wasTruncated := agentcore.DedupSpeakText(text)
		if cleaned == "" {
			return "speak_with_thought rejected: empty text after dedup", nil
		}
		// 内心独白:简单截断到 120 字(不 dedup,因为是思考不是发言)。
		thought = truncate(thought, 120)
		if thought == "" {
			return "speak_with_thought rejected: empty internal_thought (must include真实想法)", nil
		}
		result, err := runner.SpeakWithThought(cleaned, thought)
		if err != nil {
			return result, err
		}
		suffix := " [§119 心口不一: public/text broadcast, internal_thought 仅记录到 BotTranscript]"
		if wasDeDuped || wasTruncated {
			if wasDeDuped {
				suffix += " [deduped adjacent repeats]"
			}
			if wasTruncated {
				suffix += " [truncated to 80 chars]"
			}
		}
		return result + suffix, nil
	case "finish_speak":
		return runner.FinishSpeak()
	case "vote":
		return runner.Vote(intInput(input, "target"))
	case "finish_vote":
		tr := 0
		if v, ok := input["tied_round"]; ok {
			tr = intFrom(v)
		}
		return runner.FinishVote(tr)
	case "start_day":
		return runner.StartDay()
	case "sheriff_candidate":
		return runner.SheriffCandidate(intInput(input, "target"))
	case "sheriff_elect":
		return runner.SheriffElect()
	case "hunter_shoot":
		return runner.HunterShoot(intInput(input, "target"))
	// 2026-07-10 §7 / §12:警徽流声明工具。
	// slot∈{1,2}, target=-1|0..11。runner 实现 → Action_SheriffStream。
	case "sheriff_stream":
		slot := intInput(input, "slot")
		target := intInput(input, "target")
		return runner.SheriffStream(slot, target)
	// 2026-07-10 §3.5 / §12:白痴翻牌结算工具。
	// choice∈{reveal, skip}。runner 实现 → Action_IdiotReveal。
	case "idiot_reveal":
		choice, _ := input["choice"].(string)
		return runner.IdiotReveal(choice)
	// sheriff_stream_skip:持锁路径以 slot=0 撤回所有声明的情报占位(skip 时无须操作)。
	// PhaseSpeak _actor(预言家警长)被 quarantine 时,watchdog 派发 sheriff_stream_skip;
	// 默认行为 = 不声明,由 runner 实现返回"ok"(无操作)。runner 未实现时 fallback。
	case "sheriff_stream_skip":
		// §128 对话即思考重构:通过 IdleSilent(role=player) 审计路径留痕。
		if r, ok := runner.(IdleSilentRunner); ok {
			return r.IdleSilent("player", "sheriff_stream_skip: 放弃警徽流声明")
		}
		return "sheriff_stream_skip: no-op", nil
	case "idiot_reveal_skip":
		// 2026-07-10 §12.2:quarantined 白痴默认 skip 翻牌。
		// runner 直接调 IdiotReveal("skip") 走引擎放弃翻牌结算。
		return runner.IdiotReveal("skip")
	case "wolf_suicide":
		return runner.WolfSuicide()
	case "whisper":
		to, _ := input["to_seat"].(float64)
		txt, _ := input["text"].(string)
		return runner.Whisper(int(to), txt)
	case "interject":
		// BUG-WEREWOLF-AGENT-INTERJECT: 非发言轮次的 bot 主动插话。
		// 走 ChatService.SendFromBot + is_interject=true 标记,前端会渲染为
		// "💬插话" 而非 "🗣发言"。Agent.Limiter(Mark) 在 run.go 694-707 已
		// 统一处理 speak/whisper/interject 三个公开发言动作的 30s 限流桶,
		// 这里不需要再 Mark。
		text, _ := input["text"].(string)
		// 与 speak 一致:interject 也走 LLM,可能复读(Round 39+ 报告 N1)。
		cleaned, _, _ := agentcore.DedupSpeakText(text)
		if cleaned == "" {
			return "interject rejected: empty after dedup", nil
		}
		return runner.Interject(cleaned)
	case "restart_vote":
		// 2026-07-10: 重开局投票。choice ∈ {yes,no,abstain}; runner
		// 实现 → manager.RestartVoteBotLocked 持锁调用 CastRestartVoteLocked
		// → 评估 quorum → 通过则 restartGameLocked 立即开新局。
		choice, _ := input["choice"].(string)
		return runner.RestartVote(choice)
	case "propose_vote":
		// 2026-07-11: 预言家发起投票。白天发言阶段预言家可提议结束讨论直接进入投票。
		return runner.ProposeVote()
	// BUG-WEREWOLF-P0-8 FIX: skipPhaseAction 在 failAutoSkipThreshold 到达后
	// 返回 "vote_skip" / "witch_act_skip" 让 Agent 在 LLM 永久失败时推进阶段。
	// 原实现这两个名字没注册 → "unknown tool" → Agent 无限重试 → 全场卡死。
	// 这里把它们映射为对应阶段的"合法空动作"：
	//   - vote_skip  → Vote(-1) 弃权投票(引擎接受 -1 作为弃权)
	//   - witch_act_skip → WitchAct("none", -1) 不用药
	case "vote_skip":
		return runner.Vote(-1)
	case "witch_act_skip":
		return runner.WitchAct("none", -1)
	// 2026-07-29 §134 守卫 skip — guard_protect_skip 派发到 runner.GuardProtect(-1)。
	// §134: 复述段落已压缩 — git blame 与 docs/ 索引可还原

	case "guard_protect_skip":
		return runner.GuardProtect(-1)
	case "demon_hunter_hunt_skip":
		// 2026-08-12 §20260812-04 U6 — 补齐缺失的派发 case。
		//
		// 缺陷:SkipPhaseAction(run.go:373) 会返回 "demon_hunter_hunt_skip",
		// 但派发表里从来没有这个 case —— 三条 auto-skip 路径
		// (run.go 的 quarantine-skip / 成功但无动作 / speak_floor)全部命中
		// default 分支拿到 "unknown tool",只能靠 manager 侧 room_agent.go:659
		// 兜底。这正是 §97「新增阶段必须同步 SkipPhaseAction / watchdogActingSeat /
		// dispatchQuarantinedSkipLocked 三处」漏掉的第四处。
		//
		// target=-1 语义与 guard_protect_skip 一致:本轮放弃发动。
		return runner.DemonHunterHunt(-1)
	case "last_words":
		// 修复(2026-08-04)§遗言链路 — last_words 派发到 runner.LastWords(text)。
		// agentRunner.LastWords → Action_LastWords 早已实现,但这里缺 case,
		// LLM 即便调 last_words 也只会拿到 "unknown tool"。
		text, _ := input["text"].(string)
		return runner.LastWords(text)
	case "last_words_skip":
		// R91-P1-1 (2026-07-11): death_lyric 阶段 bot 放弃遗言,推进遗言队列。
		// 之前 dispatchToolInner 没有这个 case,run.go:998 max-tool-use auto-skip
		// 路径调 DispatchTool("last_words_skip") 时返回 "unknown tool",导致
		// death_lyric 阶段 quarantine bot 卡死直至 watchdog 90s 兜底。
		// 修复:映射到 runner.LastWordsSkip()(ToolRunner 接口已声明)。
		return runner.LastWordsSkip()
	// §128 对话即思考重构:idle_think 派发已删除,统一走 idle_silent。
	case "idle_silent":
		// 2026-07-08 §15 / Round 40 §95: 首夜强制发言阶段的"沉默思考"工具。
		// 强约束:仅在 PreWolvesCountForMySeat >= 1 时才能调;若 LLM 错误
		// 调用(本轮未发),run.go 会通过游戏上下文判定并返回错误让 LLM 重试。
		// 这里只做 dispatcher 派发,真正的强约束检查放在 run.go handleEvent 中。
		// §128 重构:role 字段区分玩家 / 法官,默认 player。
		text, _ := input["reason"].(string)
		role, _ := input["role"].(string)
		if role == "" {
			role = "player"
		}
		if r, ok := runner.(IdleSilentRunner); ok {
			return r.IdleSilent(role, text)
		}
		return "idle_silent recorded (no runner)", nil
	case "emotion_switch_speak":
		// §5: 复述段落已压缩 — git blame 与 docs/ 索引可还原

		text, _ := input["text"].(string)
		emotion, _ := input["emotion"].(string)
		reason, _ := input["reason"].(string)
		reason = truncate(reason, 80)
		// duration_sec 缺失时 intFrom 返回 -1(unknown key 约定),这里先归一到 0
		// 让 NormalizeEmotionFx 走默认 12s 分支(而不是被 clamp 到 8s)。
		durationSec := intInput(input, "duration_sec")
		if _, ok := input["duration_sec"]; !ok || durationSec < 0 {
			durationSec = 0
		}
		fx := NormalizeEmotionFx(EmotionFx{
			Effect:      stringInput(input, "effect"),
			Intensity:   stringInput(input, "intensity"),
			Caption:     stringInput(input, "caption"),
			DurationSec: durationSec,
		})
		return runner.EmotionSwitchSpeak(text, emotion, reason, fx)
	// §20260811-06 U3 — reasoning_chain 推理链工具。
	// 仅在 speak / vote / night_action 阶段挂载(其他阶段 BuildTools 不暴露);
	// 不计入 consecutiveFailures;走 runner.ReasoningChain 派发,内部写
	// BotTranscript.ReasoningChains(§135 spectator 隔离)。
	case "reasoning_chain":
		topic, _ := input["topic"].(string)
		steps := toStringSlice(input["steps"])
		evidence := toStringSlice(input["evidence"])
		conclusion, _ := input["conclusion"].(string)
		confidence := intInput(input, "confidence")
		if confidence < 0 {
			confidence = 0
		}
		if confidence > 100 {
			confidence = 100
		}
		return runner.ReasoningChain(topic, steps, evidence, conclusion, confidence)
	// 2026-07-21 道具系统 — use_prop 工具派发。
	// prop_id: 道具类型; target: 目标座位号; payload: 自定义文本(可选)。
	// runner 实现 → werewolf.agentRunner.UseProp（持锁调用 PropEngine）。
	case "use_prop":
		propID, _ := input["prop_id"].(string)
		target := intInput(input, "target")
		payload, _ := input["payload"].(string)
		if propID == "" {
			return "use_prop rejected: prop_id required", nil
		}
		if r, ok := runner.(PropUserRunner); ok {
			return r.UseProp(propID, target, payload)
		}
		return "use_prop rejected: runner does not support props", nil
	// 2026-07-21 v4 §13.1 — 狼小队广播 wolf_whisper 工具派发。
	// text: 留言内容(≤80字)。runner 实现 → werewolf.agentRunner.WolfWhisper
	// (持锁调 WolfPackRoom.Append,不替 LLM 决策;协议层隔离 — 不入公屏/HeartThought)。
	case "wolf_whisper":
		text, _ := input["text"].(string)
		if text == "" {
			return "wolf_whisper rejected: text required", nil
		}
		if r, ok := runner.(WolfWhisperRunner); ok {
			return r.WolfWhisper(text)
		}
		return "wolf_whisper rejected: runner does not support wolfpack", nil
	// v3 §G2 — prop_inspect/prop_status/prop_history 三个查询类工具。
	// 纯查询,无副作用;由 PropInspectRunner 扩展接口提供数据(测试桩可实现)。
	case "prop_inspect":
		scope, _ := input["scope"].(string)
		if scope == "" {
			scope = "mine"
		}
		return formatPropInspect(scope), nil
	case "prop_status":
		return formatPropStatus(), nil
	case "prop_history":
		limit := intInput(input, "limit")
		return formatPropHistory(limit), nil
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

// PropUserRunner 是 ToolRunner 的可选扩展接口，提供道具使用能力。
// 实现位置：werewolf.agentRunner。nil-safe：测试桩 / 老代码路径未实现时，
// DispatchTool 返回 "use_prop rejected" 提示让 LLM 收敛。
type PropUserRunner interface {
	UseProp(propID string, target int, payload string) (string, error)
}

// PropInspectRunner 是 ToolRunner 的可选扩展接口（v3 §G2），提供当前 wwtypes.GameContext。
// 实现位置：werewolf.agentRunner。用于 prop_inspect / prop_status / prop_history
// 三个查询工具的数据源（实时字段如冷却/余额/历史）。
// nil-safe：未实现时查询工具返回 "no game context" 提示。
type PropInspectRunner interface {
	CurrentGC() *wwtypes.GameContext
}

// intInput reads a key that must be present as an int (float64 on the wire).
func intInput(input map[string]any, key string) int {
	return intFrom(input[key])
}

// stringInput 从 tool input 取字符串字段;缺失/非字符串返回空串(供
// 2026-08-04 §表情特效 effect/intensity/caption 等新可选参数解析)。
func stringInput(input map[string]any, key string) string {
	s, _ := input[key].(string)
	return s
}

func intFrom(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	case string:
		i, _ := strconv.Atoi(n)
		return i
	default:
		return -1
	}
}

// intEnum converts []int to []any for JSON-Schema `enum` lists.
func intEnum(xs []int) []any {
	out := make([]any, 0, len(xs)+1)
	seen := map[int]bool{}
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

// intEnum appends -1 sentinel.

// toStringSlice 把 input map 中的数组字段归一为 []string。
// LLM 工具调用时数组字段通常以 []any 传入,每个元素是 string / float64
// (Anthropic JSON number) — 后者转回 string。空 / 非法 → 返回空切片。
// §20260811-06 U3 reasoning_chain 用。
func toStringSlice(v any) []string {
	if v == nil {
		return nil
	}
	switch arr := v.(type) {
	case []any:
		out := make([]string, 0, len(arr))
		for _, it := range arr {
			switch s := it.(type) {
			case string:
				if s != "" {
					out = append(out, s)
				}
			case float64:
				out = append(out, strconv.Itoa(int(s)))
			}
		}
		return out
	case []string:
		out := make([]string, 0, len(arr))
		for _, s := range arr {
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// filterSelf removes `seat` from the alive list (players cannot target
// themselves).
func filterSelf(alive []int, seat int) []any {
	out := make([]any, 0, len(alive))
	for _, x := range alive {
		if x != seat {
			out = append(out, x)
		}
	}
	return out
}

// filterGuardTargets 2026-07-29 §134 — 守卫守护目标 enum 过滤。
// 剔除自己(seat) + 上晚守护目标(last),保留 -1(空守)作为合法出口。
// G1 不可连守同一人 + G2 不可守自己 + G4 可空守(-1)。
// gc 可能为 nil(测试路径),nil 时退化为只剔除自己。
func filterGuardTargets(alive []int, seat int, gc *wwtypes.GameContext) []any {
	last := -1
	if gc != nil {
		last = gc.GuardLastProtect
	}
	out := make([]any, 0, len(alive)+1)
	for _, x := range alive {
		if x == seat || x == last {
			continue
		}
		out = append(out, x)
	}
	// -1(空守)始终作为合法出口追加 — G4 守/不守都是合法决策。
	out = append(out, -1)
	return out
}

// onlySelf returns a single-element enum containing just `seat`. Used by
// actions that MUST target the caller (e.g. sheriff_candidate).
func onlySelf(seat int) []any {
	return []any{seat}
}

// sheriffCandidateSeats §报告-20260804-03 BUG-07 — 取警长竞选已参选座位。
// gc 可能为 nil(测试路径)或名单为空(尚无人参选),两种情况都返回 nil,
// 由调用方退化为「全部存活座位」。返回值与 alive 取交集,防止已参选后
// 死亡的座位仍留在 enum 里。
func sheriffCandidateSeats(gc *wwtypes.GameContext, alive []int) []int {
	if gc == nil || len(gc.SheriffCandidates) == 0 {
		return nil
	}
	aliveSet := make(map[int]bool, len(alive))
	for _, a := range alive {
		aliveSet[a] = true
	}
	out := make([]int, 0, len(gc.SheriffCandidates))
	for _, c := range gc.SheriffCandidates {
		if aliveSet[c] {
			out = append(out, c)
		}
	}
	return out
}

// ─── v4 add* 兼容 shim ────────────────────────────────────────────────────
//
// 2026-07-21 v5 重构：以下 5 个 add* 函数迁出到 prop_tools.go / wolf_tools.go
// 并通过 ToolRegistry 统一注册。保留函数签名作为 thin wrapper,供外部调用
// (包括 v3/v4 测试)继续使用。函数体内仅做 MountIf 校验 + 单独调对应 ToolSpec
// 的 Builder,**只挂当前 add* 对应的单一工具**(不调用 mountFromRegistry,
// 避免把 5 个工具一起挂上,影响 v4 测试期望的"单独挂载"行为)。
//
// v5 后的接入规范:新工具**不应**再写 add*(add, gc, ...)函数;改为在分类文件
// 的 init() 中 RegisterTool(&ToolSpec{...})。BuildTools 通过 mountFromRegistry
// 自动挂载(在 PhaseSpeak case 末尾调一次)。

// mountOneTool 是 5 个 add* thin wrapper 共享的实现:按 name 找 ToolSpec →
// 校验 MountIf → 调 Builder 输出 schema → 调 add 闭包挂载。
// 若 spec 未注册或 MountIf 拒收则 no-op(同 v4 行为)。
func mountOneTool(add func(name, desc string, s map[string]any), name string, gc *wwtypes.GameContext) {
	spec := FindTool(name)
	if spec == nil || spec.Builder == nil {
		return
	}
	if spec.MountIf != nil && gc != nil && !spec.MountIf(gc) {
		return
	}
	schema := spec.Builder(gc)
	if schema == nil {
		return
	}
	desc := spec.Description
	if spec.BuildDescription != nil {
		if dyn := spec.BuildDescription(gc); dyn != "" {
			desc = dyn
		}
	}
	if desc == "" {
		desc = spec.Name
	}
	add(spec.Name, desc, schema)
}

// addUsePropTool thin wrapper(向后兼容 v4 测试)。仅挂 use_prop。
func addUsePropTool(add func(name, desc string, s map[string]any), gc *wwtypes.GameContext, _ []int, _ int) {
	if gc == nil || len(gc.PropSnapshot) == 0 {
		return
	}
	mountOneTool(add, "use_prop", gc)
}

// addPropInspectTool thin wrapper(向后兼容 v4 测试)。仅挂 prop_inspect。
func addPropInspectTool(add func(name, desc string, s map[string]any), gc *wwtypes.GameContext) {
	if gc == nil {
		return
	}
	mountOneTool(add, "prop_inspect", gc)
}

// addPropStatusTool thin wrapper(向后兼容 v4 测试)。仅挂 prop_status。
func addPropStatusTool(add func(name, desc string, s map[string]any), gc *wwtypes.GameContext) {
	if gc == nil {
		return
	}
	mountOneTool(add, "prop_status", gc)
}

// addPropHistoryTool thin wrapper(向后兼容 v4 测试)。仅挂 prop_history。
func addPropHistoryTool(add func(name, desc string, s map[string]any), gc *wwtypes.GameContext) {
	if gc == nil || len(gc.PropHistorySnapshot) == 0 {
		return
	}
	mountOneTool(add, "prop_history", gc)
}

// addWolfWhisperTool thin wrapper(向后兼容 v4 测试)。仅挂 wolf_whisper。
// 校验:仅 faction=="wolf" 且 WolfTeammateSeat>=0 时挂载(MountIf 也做同样校验)。
func addWolfWhisperTool(add func(name, desc string, s map[string]any), gc *wwtypes.GameContext) {
	if gc == nil || gc.Faction != "wolf" || gc.WolfTeammateSeat < 0 {
		return
	}
	mountOneTool(add, "wolf_whisper", gc)
}

// WolfWhisperRunner 是 ToolRunner 的可选扩展接口（v4 §13.1），提供狼小队广播能力。
// 实现位置：werewolf.agentRunner。nil-safe：未实现时 DispatchTool 返回
// "wolf_whisper rejected" 提示让 LLM 收敛。
type WolfWhisperRunner interface {
	WolfWhisper(text string) (string, error)
}
