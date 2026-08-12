// Package agent — memory.go: per-agent conversation memory + the game-context
// snapshot the agent reasons over. Memory is the multi-turn LLM history;
// Context is the current game-state snapshot + recent transcript.
package wwplayer

import (
	"fmt"
	"math/rand"
	"sync"
	"time"

	"LsmWebGame/llm"
	llmtypes "LsmWebGame/llm/types"
)

// ToolRecord is one executed tool call, kept for UI display and for feeding
// back to the model as a tool_result turn.
type ToolRecord struct {
	Name   string         `json:"name"`
	Input  map[string]any `json:"input"`
	Result string         `json:"result"`
	At     time.Time      `json:"at"`
}

// Memory is the full multi-turn conversation history for one agent. It is
// concurrency-safe (the agent's single goroutine owns it, but UI reads happen
// concurrently via Snapshot).
type Memory struct {
	mu       sync.RWMutex
	messages []llm.Message
	tools    []ToolRecord

	// maxPromptBytes 是 messages 的近似字节预算(0 表示禁用)。
	// 仅按条数剪枝时,大量"短条数但大块头"的 user prompt(每轮 ~8.8KB)可以
	// 累积到 800KB+ 仍低于 161 条阈值 → Prune 永远不触发 → 上下文对
	// DouBao 等小窗口模型永久溢出 → 400 "exceed max message tokens" 自强化
	// 死循环(失败路径推消息但不剪枝,payload 只增不减,构造上不可恢复)。
	// 引入字节预算后,pruneLocked 在按条数裁剪基础上进一步收缩,使上下文能
	// 回落到模型限额以下,打破该死循环 (BUG-R241-P1-01)。
	maxPromptBytes int

	// 2026-08-10 §20260810-14 增强:记录 system + tools 的近似字节数。
	// 之前 approxPayloadBytes 仅计算 messages,忽略了 system prompt(~2KB)
	// 和 tools 定义(~5-10KB)。DouBao 等小窗口模型的上下文窗口只有 ~128K-256K,
	// 这部分"隐形开销"在极端场景下可能导致实际 payload 超限但字节预算未触发。
	// totalSystemToolsBytes 由 SetSystemTools 在每次构建 LLMRequest 前注入,
	// enforceByteBudgetLocked 使用 "messages + system + tools" 作为剪枝基准。
	totalSystemToolsBytes int
}

// NewMemory seeds the conversation with the opening user turn that fixes the
// agent's identity, role, faction and win condition.
func NewMemory(role, faction, win string, seat int) *Memory {
	return NewMemoryWithWolfHint(role, faction, win, seat, -1)
}

// NewMemoryWithWolfHint seeds the conversation with the opening user turn,
// optionally injecting a "you know X 号 is your wolf teammate" hint into the
// identity prompt. Used by StartAgentsLocked to wire the 30% partial-wolf-
// knowledge design (docs/狼人杀-道具与经济/狼人杀13人局道具系统设计.md §5.2): in ~30% of games,
// 2 of the wolves are pre-paired at game start. wolfTeammateSeat must be the
// 0-indexed seat of the teammate; pass -1 to skip the hint entirely.
func NewMemoryWithWolfHint(role, faction, win string, seat, wolfTeammateSeat int) *Memory {
	m := &Memory{maxPromptBytes: DefaultMaxPromptBytes}
	identity := llm.Message{
		Role:    "user",
		Content: []llm.ContentBlock{{Type: "text", Text: identityPromptWithWolfHint(role, faction, win, seat, wolfTeammateSeat)}},
	}
	m.messages = append(m.messages, identity)
	return m
}

// SetMaxPromptBytes 设置本 Memory 的近似字节预算(覆盖默认值 DefaultMaxPromptBytes)。
// 传入 0 禁用字节剪枝(仅保留按条数剪枝)。可按模型上下文窗口大小逐 agent 调整
// (例如 DouBao 等小窗口模型设得更紧),默认值对所有模型统一适用。
func (m *Memory) SetMaxPromptBytes(n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.maxPromptBytes = n
}

// MaxPromptBytes 返回当前字节预算(0 表示禁用)。
func (m *Memory) MaxPromptBytes() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.maxPromptBytes
}

func identityPrompt(role, faction, win string, seat int) string {
	return identityPromptWithWolfHint(role, faction, win, seat, -1)
}

// roleAbilityDescription 返回某身份对应的技能说明,在身份注入时让 Agent 立即
// 知道自己角色的能力(2026-07-28 新增,修复射梦人/乌鸦等扩展神职身份注入时
// Agent 完全不知道自己技能的数据不同步 bug)。
func roleAbilityDescription(role string) string {
	switch role {
	case "werewolf":
		return "技能: 每晚与狼人协商刀一名存活玩家(空刀可选);白天可自爆立刻结束白天。"
	case "seer":
		return "技能: 每晚查验一名存活玩家的阵营(好人/狼人),只知阵营不知具体身份。"
	case "witch":
		return "技能: 拥有一瓶解药(救活当晚狼刀目标)和一瓶毒药(毒杀一名玩家),整局各一瓶,不能同晚双药。"
	case "hunter":
		return "技能: 死亡后可开枪带走一名玩家(被女巫毒杀则不能开枪)。"
	case "idiot":
		return "技能: 被白天投票放逐时可翻牌免死,但失去投票权与被投票权,仍存活可发言。"
	case "guard":
		// 2026-07-29 §134 守卫角色全链路补全 — 移除「暂无独立工具」提示。
		// 新增 guard_protect 工具(blind-guard 盲守语义:看不到当晚狼刀目标)。
		// 完整技能三约束:G1 不可连守同一人 / G2 不可守自己 / G3 同守同救致死。
		return "技能: 每晚守护一名玩家使其免疫当晚狼刀。\n" +
			"  • 盲守: 你看不到当晚狼刀目标,必须推理预判狼意图。\n" +
			"  • 不可连续两晚守护同一人(GuardLastProtect)。\n" +
			"  • 不可守自己。\n" +
			"  • 可空守(target=-1)。\n" +
			"  • 同守同救: 若你守的人同时被女巫解药救,双方抵消,该玩家仍死亡。\n" +
			"工具: guard_protect(target=int) — 守护 target 座位号(枚举已剔除自己与上晚守护目标;传 -1 表空守)。"
	case "knight":
		// §198 骑士角色全链路补全 — 移除「暂无独立工具」提示,新增 knight_duel 工具。
		// 完整技能 K1-K8 约束(K1 每局限一次 / K3 发言阶段 / K8 发动即亮身份)。
		return "技能: 白天发言阶段翻牌与一名玩家决斗。\n" +
			"  • 命中狼 → 目标出局(verdict=execution,执行语义)。\n" +
			"  • 未命中狼 → 你自己出局(死因\"duel\",verdict=execution,自决)。\n" +
			"  • 每局限一次:发动即锁定本局技能,KnightDuelUsed=true 后不能再用。\n" +
			"  • 发动即亮身份:场上立刻知道你是骑士(K8)。\n" +
			"  • 只能对自己发言轮(speakTurn==seat)发动;非自己发言穿插会破坏轮流秩序。\n" +
			"工具: knight_duel(target=int) — 决斗 target 座位号(枚举已剔除自己;传 -1 表本轮放弃技能保留)。"
	case "demon_hunter":
		// §猎魔人 猎魔人角色全链路补全 — 移除「暂无独立工具」提示,新增 demon_hunter_hunt 工具。
		// 完整技能 DH1-DH7 约束(DH1 首夜禁用 / DH5 每晚可发动 / DH7 发动即公开)。
		return "技能: 第 2 晚起每晚可狩猎一名存活玩家(DH1 首夜禁用)。\n" +
			"  • 命中狼 → 目标立即死亡(verdict=death,cause=wolf)。\n" +
			"  • 命中好人 → 你自己出局(verdict=execution,cause=\"demon_hunter_misjudge\")。\n" +
			"  • 发动即公开身份(DH7,RolePubliclyRevealed 白名单 ⑥)。\n" +
			"  • 每晚可发动,无单局锁定 — 用错只是死,后续夜晚仍可狩猎。\n" +
			"  • 可空过(target=-1)。\n" +
			"工具: demon_hunter_hunt(target=int) — 狩猎 target 座位号(枚举已剔除自己+已死;传 -1 表空过)。"
	// ═══════════════════════════════════════════════════════════════
	// ⚠️ 2026-07-29 已退役角色(magician/merchant/dreamer/crow/pure_white):
	// 无引擎/工具实现,移除占位技能描述。这些角色不再出现在新局中,
	// 走 default 分支返回空字符串即可(LLM 不会收到它们的身份 prompt)。
	//case "magician":
	//	return "技能: 每晚可交换两名玩家的号码牌,影响当晚所有夜间技能的目标指向。⚠️ 当前引擎暂无独立工具,你以该身份参与发言/投票博弈。"
	//case "merchant":
	//	return "技能: 每晚选择一名幸运儿赋予一项随机技能(查验/守护/射击等),次日可用一次。⚠️ 当前引擎暂无独立工具,你以该身份参与发言/投票博弈。"
	//case "dreamer":
	//	return "技能: 每晚指定一名玩家为「梦游者」,该玩家当晚免疫所有夜间伤害(狼刀/毒药);不可连续两晚选同一人,不可选自己。⚠️ 当前引擎暂无独立工具,你以该身份参与发言/投票博弈。"
	//case "crow":
	//	return "技能: 每晚诅咒一名玩家,使其次日白天投票阶段额外获得一票(警长票不变);不可连续两晚诅咒同一人。⚠️ 当前引擎暂无独立工具,你以该身份参与发言/投票博弈。"
	//case "pure_white":
	//	return "技能: 每晚查验一名玩家:若为狼人则该狼人立即出局(无需等天亮),若为好人则无副作用。⚠️ 当前引擎暂无独立工具,你以该身份参与发言/投票博弈。"
	// ═══════════════════════════════════════════════════════════════
	default:
		return ""
	}
}

// identityPromptWithWolfHint 是 identityPrompt 的可注入版本。
// 当 wolfTeammateSeat >= 0 且 faction == "wolf" 时,追加一段"你知道 X 号
// 是你的狼队友"提示;否则与原 identityPrompt 行为一致。
func identityPromptWithWolfHint(role, faction, win string, seat, wolfTeammateSeat int) string {
	playerNo := seat + 1 // 1-indexed "玩家编号" 与 UI 标签 #1~#13 一致
	// 2026-07-10 §7 / §3.5:12 人标准竞技局。座位号 0..11(内部),玩家编号 1..12(对外)。
	// 2026-07-15: 升级到 13 人标准竞技局,玩家编号 1..13。
	out := "" +
		"你是狼人杀 13 人标准竞技局的 AI 玩家。\n" +
		"座位号: " + itoa(seat) + "（0-12，内部使用）\n" +
		"玩家编号: " + itoa(playerNo) + " 号（1-13，对外发言、引用其他玩家时一律使用此编号）\n" +
		"身份: " + role + "\n" +
		"阵营: " + faction + "\n" +
		"目标: " + win + "\n"
	// 2026-07-28: 注入角色技能说明,让 Agent 知道自己身份的能力。
	if ability := roleAbilityDescription(role); ability != "" {
		out += "📜 " + ability + "\n"
	}
	out += "" +
		"你只能通过调用工具操作游戏。每次发言(speak) ≤ 100 字，频率 ≤ 2 次/分钟。\n" +
		"💬 主动插话(interject):Speak 阶段你可以对其他玩家的发言主动追问/补充/闲聊，≤100字,≤2次/分钟（与 speak 共用限流）。\n" +
		"硬约束: 自我介绍必须用'我是" + itoa(playerNo) + "号'，引用其他玩家必须用'X号'(X=1..13)。永远不要把座位号当成玩家编号对外说。\n" +
		"\n" +
		"【金币目标（优先级高于一切）】\n" +
		"首要目标：不择手段地赢得本局（狼屠边 / 好人放逐全部狼），胜利是金币的唯一来源。\n" +
		"长期目标：每局都要设法赚到比其他 Agent 模型更多的金币（彩池制下：胜方分输家底注，平局 0，负方输底注）。\n" +
		"策略：无论阵营，优先追求胜利；同等胜算下优先选择能让自己留在胜方阵营的决策。"
	// 2026-07-21 §狼队友 30% 互知：仅在 faction == "wolf" 且 wolfTeammateSeat >= 0
	// 时追加"开局即互认队友"段。该提示只对 LLM 自己可见(进 identity prompt),
	// 不会进入任何 chat 消息 / broadcast / BotTranscript.HeartThought。
	if faction == "wolf" && wolfTeammateSeat >= 0 {
		out += "\n【开局互认队友(§5.2 30% 互知设计)】\n" +
			"系统开局随机给你注入一个狼队友身份:" + itoa(wolfTeammateSeat+1) + " 号玩家(座位 " + itoa(wolfTeammateSeat) + ")是本局你的狼队友。\n" +
			"这是开局即知的信息,首夜/首日可直接信任 +1号 的发言与投票(但请按需 verify 防御假冒)。\n" +
			"其他狼人是否互认身份未知,需通过私聊/发言节奏自行确认。\n" +
			"硬约束:此信息仅你可见,不可在公屏/插话/HeartThought 透露该编号就是你的狼队友。"
	}
	return out
}

// PickWolfTeammateHint 在 ~30% 的对局里,从所有狼人座位中随机选一个
// 作为本 bot 已知身份的狼队友。
// 当该 bot 自己不是狼人 / 房间中无其他狼人 / 概率检定失败时返回 -1(不注入提示)。
// 本函数被 StartAgentsLocked 在构造 agent.Agent 之前调用,以决定每个
// bot 的 wolfTeammateSeat 注入值;可单测(不依赖 LLM / DB / WS)。
//
// 设计动机(docs/狼人杀-道具与经济/狼人杀13人局道具系统设计.md §5.2):
//   - 100% 互知 → 机械感,失去"信息不对称"博弈;
//   - 0% 互知 → 狼人首夜协调成本极高,好人阵营过强;
//   - 30% 部分互知 → 70% 的对局仍需"试探/识别/私聊",30% 的对局里有
//     1 对狼人开局秒识破身份 → 真实狼人博弈。
//
// 概率可被 cfgWerewolf.WolfTeammateHintRate 覆盖,默认 30(= 30%);0 时禁用。
func PickWolfTeammateHint(myRole, myFaction string, mySeat int, allWolfSeats []int, ratePercent int, rng *rand.Rand) int {
	if myFaction != "wolf" {
		return -1
	}
	if len(allWolfSeats) < 2 {
		// 自己是唯一的狼,无所谓"队友",返回 -1
		return -1
	}
	if ratePercent <= 0 {
		return -1
	}
	if rng == nil {
		rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	if rng.Intn(100) >= ratePercent {
		return -1
	}
	// 在所有"非自己"的狼人座位里随机选一个
	candidates := make([]int, 0, len(allWolfSeats))
	for _, s := range allWolfSeats {
		if s != mySeat {
			candidates = append(candidates, s)
		}
	}
	if len(candidates) == 0 {
		return -1
	}
	return candidates[rng.Intn(len(candidates))]
}

// PickWolfTeammatePairs 是 PickWolfTeammateHint 的 v3 批量版本（用于 max_pairs 配置）。
// 给定本局所有狼人座位 + 启用概率 + 每局最多几对,返回"本局要互知的狼人配对列表"。
// 例如 maxPairs=1, allWolfSeats=[0,2,5,8] → 可能返回 [[0,2]] 或 [[5,8]]（取决于 rng）。
//
// 设计动机（docs/狼人杀-道具与经济/狼人杀13人局道具系统设计.md §4.1）：
//   - v1.1 单只独立 PickWolfTeammateHint 调用无法保证"对称互知"（A 知道 B 但 B 不知道 A）；
//   - v3 引入配对列表后，配对的两只狼互为队友（对称）。
//   - 30% 概率启用；启用后最多 max_pairs 对 = 2 * max_pairs 只狼互知。
//
// 返回的每个 pair = [seatA, seatB]，调用方需对每个 pair 中的两个座位都调
// SetWolfTeammateSeat 让双方都知道对方。
func PickWolfTeammatePairs(allWolfSeats []int, ratePercent, maxPairs int, rng *rand.Rand) [][2]int {
	if ratePercent <= 0 || maxPairs <= 0 || len(allWolfSeats) < 2 {
		return nil
	}
	if rng == nil {
		rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	// 30% 概率启用
	if rng.Intn(100) >= ratePercent {
		return nil
	}
	// 狼人座位洗牌（Fisher-Yates）
	seats := make([]int, len(allWolfSeats))
	copy(seats, allWolfSeats)
	rng.Shuffle(len(seats), func(i, j int) { seats[i], seats[j] = seats[j], seats[i] })
	// 最多 maxPairs 对 = 2 * maxPairs 只狼
	pairs := make([][2]int, 0, maxPairs)
	for i := 0; i+1 < len(seats) && len(pairs) < maxPairs; i += 2 {
		pairs = append(pairs, [2]int{seats[i], seats[i+1]})
	}
	return pairs
}

// Push appends a message to the history.
func (m *Memory) Push(msg llm.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, msg)
}

// Len returns the current message count (lock-free snapshot).
func (m *Memory) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.messages)
}

// Mu returns the underlying mutex so callers in this package (or trusted
// internal hooks that need to take the write lock together with their own
// invariants — see agent.SetWolfTeammateSeat) can perform multi-step
// read-modify-write transactions. External callers should prefer Push /
// Snapshot / Prune / CompressAndPrune.
func (m *Memory) Mu() *sync.RWMutex {
	return &m.mu
}

// ReplaceIdentity 替换 m.messages[0] 的 identity 文本(不重置后续对话)。
// 用于 StartAgentsLocked 阶段注入"开局互认狼队友"提示(2026-07-21 §5.2)。
// 若 m.messages 为空,no-op;若首条非 user role,no-op(避免覆盖已经发生的对话)。
// 锁安全:本函数本身已持有 m.mu,不允许调用者外层持锁(避免双锁)。
func (m *Memory) ReplaceIdentity(role, faction, win string, seat, wolfTeammateSeat int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.messages) == 0 {
		return
	}
	if m.messages[0].Role != "user" || len(m.messages[0].Content) == 0 || m.messages[0].Content[0].Type != "text" {
		return
	}
	m.messages[0] = llm.Message{
		Role: "user",
		Content: []llm.ContentBlock{{
			Type: "text",
			Text: identityPromptWithWolfHint(role, faction, win, seat, wolfTeammateSeat),
		}},
	}
}

// PushTool records an executed tool call.
func (m *Memory) PushTool(r ToolRecord) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r.At = time.Now()
	m.tools = append(m.tools, r)
	// Bump from 50 → 100 so the AgentThoughtPanel can render more of the
	// bot's recent tool history (BUG: 狼人杀 7 人局 Agent 多轮上下文).
	if len(m.tools) > 100 {
		m.tools = m.tools[len(m.tools)-100:]
	}
}

// Prune keeps only the last maxTurns user/assistant exchanges (plus the leading
// identity turn) so the context window doesn't grow unbounded across a long game.
//
// BUG: 狼人杀 7 人局 Agent 多轮上下文 — previously retained 20 turns (40
// messages) which meant long games forgot most of the conversation by day 3.
// Bumped default to 60 turns (120 messages) so the bot can reference prior
// days' speeches / whispers when reasoning about the current round.
//
// BUG-WEREWOLF-P1-LLM-TOOL-ORPHAN-RESULT (Round 70+): the original Prune
// cut at an exact message boundary which could land BETWEEN an
// assistant(tool_use X) and its following user(tool_result X). The
// tool_use got dropped while the tool_result stayed, producing an orphan
// tool_result at the head of the kept slice. The Anthropic-protocol proxy
// rejects this with HTTP 400 `tool result's tool id … not found (2013)`.
// SanitizeMessagesForAnthropic is the request-time safety net, but we
// also advance past any orphan tool_result here so the long-term
// transcript (which the UI AgentThoughtPanel renders) stays clean too.
func (m *Memory) Prune(maxTurns int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked(maxTurns)
}

// approxPayloadBytes 估算 messages 在请求体中的近似字节数(仅 messages 部分,
// 不含 system/tools 外层)。用于字节预算剪枝: 按 role + 各 content block 的
// text/tool_use 字段累加,与真实 payload_bytes 正相关,足以作为淘汰旧轮次的
// 比较基准。不计 JSON 语法字符,误差在可接受范围。
func approxPayloadBytes(msgs []llm.Message) int {
	bytes := 0
	for _, msg := range msgs {
		bytes += len(msg.Role)
		for _, c := range msg.Content {
			bytes += len(c.Type) + len(c.Text) + len(c.ID) + len(c.Name) + len(c.ToolUseID)
			for k, v := range c.Input {
				bytes += len(k)
				if s, ok := v.(string); ok {
					bytes += len(s)
				}
			}
		}
	}
	return bytes
}

// approxSystemToolsBytes 估算 system 和 tools 在请求体中的近似字节数。
// 2026-08-10 §20260810-14 增强:之前 approxPayloadBytes 仅计算 messages 部分,
// 忽略了 system prompt(~2KB)和 tools 定义(~5-10KB)的开销。在 DouBao 等
// 小窗口模型上,这部分"隐形开销"可能导致实际 payload 超限但字节预算未触发。
//
// 估算策略:
//   - system: 累加所有 SystemBlock.Text + Type + cache_control JSON
//   - tools: 累加所有 ToolDef.Name + Description + InputSchema JSON 近似
//
// 该函数返回值会注入 Memory.totalSystemToolsBytes,供 enforceByteBudgetLocked
// 使用"totalPayload = messages + system + tools"作为剪枝基准。
func approxSystemToolsBytes(system []llmtypes.SystemBlock, tools []llmtypes.ToolDef) int {
	bytes := 0
	// system blocks
	for _, s := range system {
		bytes += len(s.Type) + len(s.Text)
		// cache_control JSON 近似: {"type":"ephemeral"} ≈ 20 bytes
		if len(s.CacheControl) > 0 {
			bytes += 40
		}
	}
	// tools 定义
	for _, t := range tools {
		bytes += len(t.Name) + len(t.Description)
		// InputSchema JSON 序列化大小近似(不实际序列化,按 key-value 估算)
		if t.InputSchema != nil {
			bytes += estimateMapSize(t.InputSchema)
		}
	}
	return bytes
}

// estimateMapSize 递归估算 map[string]any 的 JSON 近似字节数。
// 避免 json.Marshal 的开销,仅按 key/value 字符串长度累加。
func estimateMapSize(m map[string]any) int {
	bytes := 0
	for k, v := range m {
		bytes += len(k) + 2 // key + ": "
		switch val := v.(type) {
		case string:
			bytes += len(val) + 2 // 引号
		case int, int64, float64:
			bytes += 8 // 数字近似
		case bool:
			bytes += 5
		case map[string]any:
			bytes += estimateMapSize(val) + 2 // {}
		case []any:
			for _, item := range val {
				if s, ok := item.(string); ok {
					bytes += len(s) + 2
				}
			}
		}
	}
	return bytes
}

// pruneLocked 是 Prune 的实际逻辑(2026-07-11 §126 重构),由 Prune 在
// 持锁状态下调用,或由 CompressAndPrune 在合并锁内调用。调用方必须
// 持有 m.mu 写锁。
func (m *Memory) pruneLocked(maxTurns int) {
	if maxTurns <= 0 {
		return
	}
	// Keep identity (index 0) + last 2*maxTurns messages.
	keep := 2 * maxTurns
	if len(m.messages) <= keep+1 {
		// 条数未超阈值,仍可能字节超限(例如 DouBao ~92 条 user 文本块 ≈ 810KB),
		// 所以不 return —— 落到下方 enforceByteBudgetLocked 统一兜底。
	} else {
		identity := m.messages[0]
		rest := m.messages[len(m.messages)-keep:]
		m.messages = append([]llm.Message{identity}, dropLeadingOrphans(rest)...)
	}
	// 字节预算兜底 (BUG-R241-P1-01): 按条数保留的上下文仍可能远超小窗口模型
	// 上限(实测 DouBao ~92 条 user 文本块 ≈ 810KB, 远超 161 条按条数阈值)。
	// 从 rest 头部逐条淘汰最旧轮次,直到近似 payload 落入预算。始终至少保留
	// 1 轮(user+assistant),避免把上下文砍到只剩 identity。
	m.enforceByteBudgetLocked()
}

// dropLeadingOrphans 跳过 rest 头部悬空的 tool_result: 即 user 消息里每个
// tool_result 的 tool_use_id 均未出现在 rest 的任一 assistant(tool_use) 中。
// 从 rest 头部丢弃这类"无主"user 消息,避免 Anthropic 协议代理以 400 拒绝。
func dropLeadingOrphans(rest []llm.Message) []llm.Message {
	if len(rest) == 0 {
		return rest
	}
	knownUseIDs := map[string]bool{}
	for _, m := range rest {
		if m.Role != "assistant" {
			continue
		}
		for _, c := range m.Content {
			if c.Type == "tool_use" && c.ID != "" {
				knownUseIDs[c.ID] = true
			}
		}
	}
	drop := 0
	for drop < len(rest) {
		r := rest[drop]
		if r.Role != "user" {
			break
		}
		hasText := false
		allOrphan := true
		for _, c := range r.Content {
			if c.Type == "text" {
				hasText = true
			}
			if c.Type == "tool_result" {
				if c.ToolUseID != "" && knownUseIDs[c.ToolUseID] {
					allOrphan = false
				}
			} else if c.Type != "tool_result" {
				// any non-text, non-tool_result block counts as
				// non-orphan (be conservative).
				allOrphan = false
			}
		}
		// Drop the message only if it carries no text and every
		// tool_result references an unknown id.
		if hasText || !allOrphan {
			break
		}
		drop++
	}
	if drop > 0 {
		return rest[drop:]
	}
	return rest
}

// enforceByteBudgetLocked 把 m.messages(含 identity)修剪到 maxPromptBytes 字节
// 预算内。从 identity 之后逐条淘汰最旧轮次,直到近似 payload 落入预算。始终
// 至少保留 1 轮(user+assistant)。调用方必须持有 m.mu 写锁。
func (m *Memory) enforceByteBudgetLocked() {
	if m.maxPromptBytes <= 0 || len(m.messages) <= 3 {
		return
	}
	rest := m.messages[1:]
	// 2026-08-10 §20260810-14 增强:使用完整 payload 大小(messages + system + tools)。
	// 之前仅计算 messages,忽略了 system prompt(~2KB)和 tools 定义(~5-10KB)。
	// DouBao 等小窗口模型的上下文窗口只有 ~128K-256K,这部分"隐形开销"在极端
	// 场景下可能导致实际 payload 超限但字节预算未触发(如 ID 1225 的 651816 tokens)。
	messagesBytes := approxPayloadBytes(rest)
	totalBytes := messagesBytes + m.totalSystemToolsBytes
	if totalBytes <= m.maxPromptBytes {
		return
	}
	for len(rest) > 2 {
		messagesBytes = approxPayloadBytes(rest)
		totalBytes = messagesBytes + m.totalSystemToolsBytes
		if totalBytes <= m.maxPromptBytes {
			break
		}
		rest = rest[1:]
	}
	m.messages = append([]llm.Message{m.messages[0]}, dropLeadingOrphans(rest)...)
}

// SetSystemTools 记录 system + tools 的近似字节数,供 enforceByteBudgetLocked
// 使用完整 payload 大小(messages + system + tools)作为剪枝基准。
// 2026-08-10 §20260810-14 新增:解决 DouBao 等小窗口模型因 system/tools 开销
// 未计入字节预算导致 Context 超限的问题。
//
// 应在每次构建 LLMRequest 前调用,传入当次请求的 system blocks 和 tools 定义。
// 典型调用点:run.go 的 handleEvent 循环顶部,在 a.roundCtx() 之后。
func (m *Memory) SetSystemTools(system []llmtypes.SystemBlock, tools []llmtypes.ToolDef) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.totalSystemToolsBytes = approxSystemToolsBytes(system, tools)
}

// PruneByBytes 是字节预算剪枝的公开入口(不依赖按条数阈值)。当某模型因
// "exceed max message tokens" 400 而持续失败时,强制把上下文压回预算内,
// 打破「失败 → 推 provider_error → payload 更大 → 再失败」的自强化死循环。
// 调用方可持锁或不含锁(本方法自带锁)。
//
// 2026-08-10 §20260810-14 增强:使用完整 payload 大小(messages + system + tools)。
// 之前仅计算 messages,忽略了 system prompt(~2KB)和 tools 定义(~5-10KB)。
// DouBao 等小窗口模型的上下文窗口只有 ~128K-256K,这部分"隐形开销"在极端
// 场景下可能导致实际 payload 超限但字节预算未触发(如 ID 1225 的 651816 tokens)。
func (m *Memory) PruneByBytes(maxBytes int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if maxBytes <= 0 {
		maxBytes = m.maxPromptBytes
	}
	if maxBytes <= 0 || len(m.messages) <= 3 {
		return
	}
	rest := m.messages[1:]
	// 2026-08-10 §20260810-14:使用完整 payload 大小
	messagesBytes := approxPayloadBytes(rest)
	totalBytes := messagesBytes + m.totalSystemToolsBytes
	if totalBytes <= maxBytes {
		return
	}
	for len(rest) > 2 {
		messagesBytes = approxPayloadBytes(rest)
		totalBytes = messagesBytes + m.totalSystemToolsBytes
		if totalBytes <= maxBytes {
			break
		}
		rest = rest[1:]
	}
	m.messages = append([]llm.Message{m.messages[0]}, dropLeadingOrphans(rest)...)
}

// PruneByBytesAggressive 是失败路径的激进压缩入口。
// 2026-08-10 §20260810-14 新增:当 LLM 返回 400 "exceed max message tokens" 等
// Context 超限错误时,使用更激进的压缩比例(50% 预算),确保快速回落到安全范围。
// 典型场景:DouBao 等小窗口模型累积大量历史后触发 400,普通 PruneByBytes
// 可能因预算设置过松而无法在单次调用中回落到安全范围。
//
// 调用点:run.go 的失败路径,在 isContextExceededError(err) 为 true 时调用。
func (m *Memory) PruneByBytesAggressive() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.maxPromptBytes <= 0 || len(m.messages) <= 3 {
		return
	}
	// 激进压缩:使用 50% 预算,确保快速回落
	targetBytes := m.maxPromptBytes / 2
	rest := m.messages[1:]
	messagesBytes := approxPayloadBytes(rest)
	totalBytes := messagesBytes + m.totalSystemToolsBytes
	if totalBytes <= targetBytes {
		return
	}
	for len(rest) > 2 {
		messagesBytes = approxPayloadBytes(rest)
		totalBytes = messagesBytes + m.totalSystemToolsBytes
		if totalBytes <= targetBytes {
			break
		}
		rest = rest[1:]
	}
	m.messages = append([]llm.Message{m.messages[0]}, dropLeadingOrphans(rest)...)
}

// DefaultPruneTurns is the default turn count kept by Prune — see the comment
// on Prune for the rationale. Bumped from 20 to 60 to mimic real LLM
// multi-turn context (Claude / GPT can comfortably ingest 100+ user/assistant
// exchanges) and to keep the werewolf bot coherent across several days of
// speeches.
//
// 2026-07-11 §126 增强: 13 人局长对局(典型 5-7 轮)60 轮会丢失早期"决策记忆";
// 默认提到 80 轮(identity + 160 messages)以保持 LLM 在 Round ≥ 5 仍能基于
// 多轮对话做推理。同时,CompressHistory 在 Prune 之前跑,把最早 20 轮的
// 决策(speak text / vote target / think 内容)压缩成 1 条 user note,
// 让"过去做了什么"在长对局后期不丢失。
const DefaultPruneTurns = 80

// DefaultMaxPromptBytes 是 Memory 默认的近似 payload 字节预算
// (BUG-R241-P1-01)。实测 DouBao-model 在 ~810KB 请求体时返回 400
// "Total tokens of image and text exceed max message tokens",而该 bot 仅累积
// ~92 条 user 文本块(远低于 161 条按条数剪枝阈值)。200KB 预算明显低于主流
// 模型上下文上限,同时保留足够多轮次供 Agent 推理;可按模型通过
// SetMaxPromptBytes 收紧(小窗口模型)或放宽(大窗口模型)。
const DefaultMaxPromptBytes = 200 * 1024 // 200 KB

// DefaultCompressTurns 是 CompressAndPrune 默认压缩的最早轮数(2026-07-11 §126)。
// 13 人局典型 5-7 轮 × 4-6 messages/turn ≈ 20-40 条历史决策,20 turns
// 正好覆盖前 2 轮完整决策 + 第 3 轮开始的内容,保留最近 60 轮对话 +
// 1 条"前 20 轮决策摘要"的总上下文,既不超 LLM 窗口,又不丢早期决策。
const DefaultCompressTurns = 20

// CompressHistoryLocked 把 m.messages 中最早的 N 条 user/assistant 对话
// 压缩为 1 条 user message(内容: 决策摘要),然后从切片中删除被压缩的
// 原始消息。调用方必须持锁(m.mu.Lock())。
//
// 2026-07-11 §126 增强: 与 Prune 配合使用。Prune 只切切片,会丢失早期
// 决策;CompressHistory 先把"被切掉"的内容凝成摘要,再让 Prune 切,
// 等于"软删除"。典型用法:
//
//	memory.Mu().Lock()
//	memory.CompressHistoryLocked(20) // 压缩最早 20 轮
//	memory.Prune(DefaultPruneTurns)
//	memory.Mu().Unlock()
//
// 公开等价物是 CompressAndPrune,它把两个步骤合并在一次锁内完成,
// 供 run.go 等调用方使用。
//
// CompressAndPrune 在一次锁内先调 CompressHistoryLocked(compressTurns),
// 再调 Prune(maxTurns)。通常 compressTurns ≤ maxTurns,确保"被压缩的早期
// 决策"在 Prune 切掉前先留下摘要。
func (m *Memory) CompressAndPrune(maxTurns, compressTurns int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if compressTurns > 0 {
		m.CompressHistoryLocked(compressTurns)
	}
	if maxTurns > 0 {
		m.pruneLocked(maxTurns)
	}
}

// 压缩策略:
//   - 遍历前 N 个 user/assistant 对话,提取关键信息:
//   - assistant 文本块(思考/speak 文本)前 30 字
//   - tool_use 名称(决策意图) + 关键 input 字段
//   - 拼接成 "[决策摘要] 第 1-20 轮: ...\n" 格式, ≤ 500 字
//   - 写入 1 条 user role message,内容是纯 text
//   - 移除被压缩的原始消息
//
// 注意: 该 user message 没有 tool_use_id,SanitizeMessagesForAnthropic
// 不会把它误判为 orphan tool_result。
func (m *Memory) CompressHistoryLocked(turnsToCompress int) {
	if turnsToCompress <= 0 || len(m.messages) <= 1 {
		return
	}
	// 跳过 identity(索引 0),从 1 开始算 turns
	// identity 不算 turn,turnsToCompress 是 user/assistant 对话数。
	// 一次 user + 一次 assistant = 1 turn,通常 2 messages/turn。
	// 简化: 按 message 数切,每 turn 估 2 messages。
	totalMsgs := 2 * turnsToCompress
	if totalMsgs > len(m.messages)-1 {
		totalMsgs = len(m.messages) - 1
	}
	if totalMsgs < 2 {
		return
	}
	// 找出 1..totalMsgs 范围内的 assistant 文本 + tool_use 名称
	identity := m.messages[0]
	toCompress := m.messages[1 : 1+totalMsgs]
	rest := m.messages[1+totalMsgs:]

	summary := buildHistorySummary(toCompress, turnsToCompress)
	summaryMsg := llm.Message{
		Role: "user",
		Content: []llm.ContentBlock{{
			Type: "text",
			Text: summary,
		}},
	}
	// 重组: identity + summary + rest
	out := make([]llm.Message, 0, len(m.messages)-totalMsgs+1)
	out = append(out, identity)
	out = append(out, summaryMsg)
	out = append(out, rest...)
	m.messages = out
}

// buildHistorySummary 把一组 message 凝成"决策摘要"文本。格式:
//
//	"[前 N 轮决策摘要(2026-07-11 §126)] 你前 N 轮的关键决策:\n• ...\n• ..."
//
// 限制总长度 ≤ 500 字,优先保留:
//  1. assistant 文本块(LLM 的 thinking / speak 内容)— 每条前 30 字
//  2. tool_use 名称(决策意图) — 例如 "speak", "vote(X号)", "wolf_kill(Y号)"
//  3. tool_result 错误(provider 失败记录)
func buildHistorySummary(msgs []llm.Message, turns int) string {
	header := "[前 " + itoa(turns) + " 轮决策摘要(2026-07-11 §126 压缩)] 你之前的决策与推理痕迹:\n"
	var lines []string
	for _, mg := range msgs {
		if mg.Role == "assistant" {
			// 收集所有 text 块
			var texts []string
			for _, c := range mg.Content {
				if c.Type == "text" && c.Text != "" {
					r := []rune(c.Text)
					if len(r) > 40 {
						texts = append(texts, string(r[:40])+"…")
					} else {
						texts = append(texts, c.Text)
					}
				}
				if c.Type == "tool_use" {
					// 提取关键 tool_use 信息: 名称 + 关键 input
					// (Input 在 ContentBlock 里是 map[string]any,直接读即可)
					tuName := c.Name
					tuInput := ""
					mp := c.Input
					if v, ok := mp["text"].(string); ok {
						r := []rune(v)
						if len(r) > 30 {
							tuInput = string(r[:30]) + "…"
						} else {
							tuInput = v
						}
					} else if v, ok := mp["target"]; ok {
						tuInput = fmt.Sprintf("→%v", v)
					} else if v, ok := mp["action"]; ok {
						tuInput = fmt.Sprintf("→%v", v)
					} else if v, ok := mp["to_seat"]; ok {
						tuInput = fmt.Sprintf("→%v", v)
					}
					if tuInput != "" {
						texts = append(texts, "["+tuName+":"+tuInput+"]")
					} else {
						texts = append(texts, "["+tuName+"]")
					}
				}
			}
			for _, t := range texts {
				lines = append(lines, "• "+t)
			}
		} else if mg.Role == "user" {
			// user 消息里只看 tool_result (失败记录)
			for _, c := range mg.Content {
				if c.Type == "tool_result" && c.IsError {
					texts := []string{}
					_ = texts
					if len(c.Content) > 0 {
						for _, cc := range c.Content {
							if cc.Type == "text" && cc.Text != "" {
								r := []rune(cc.Text)
								if len(r) > 30 {
									lines = append(lines, "• [工具失败] "+string(r[:30])+"…")
								} else {
									lines = append(lines, "• [工具失败] "+cc.Text)
								}
								break
							}
						}
					}
				}
			}
		}
	}
	// 截断到前 30 条(避免摘要过长)
	if len(lines) > 30 {
		lines = lines[:30]
	}
	out := header
	for _, l := range lines {
		out += l + "\n"
	}
	r := []rune(out)
	if len(r) > 500 {
		out = string(r[:500]) + "…"
	}
	return out
}

// Snapshot returns a copy of the current messages and recent tool records.
func (m *Memory) Snapshot() ([]llm.Message, []ToolRecord) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	msgs := make([]llm.Message, len(m.messages))
	copy(msgs, m.messages)
	tools := make([]ToolRecord, len(m.tools))
	copy(tools, m.tools)
	return msgs, tools
}

// RecentTools returns the last n tool records (newest last).
func (m *Memory) RecentTools(n int) []ToolRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if n <= 0 || n >= len(m.tools) {
		out := make([]ToolRecord, len(m.tools))
		copy(out, m.tools)
		return out
	}
	out := make([]ToolRecord, n)
	copy(out, m.tools[len(m.tools)-n:])
	return out
}

// §128 对话即思考重构:LastThinking / RecentMessages 方法已删除。
// LLM API 输出的 text + tool_use 即是模型"思考"的产物,无需额外的"思考"字段。
// 决策可观测走 BotTranscript.LastDecisionSummary / LastToolInput / LastToolResult / LastOutcome / DecisionInputs。
// LastTool 保留(返回最近一次工具调用名 + 结果前 80 字)。

// LastTool returns a short description of the most recent tool call.
func (m *Memory) LastTool() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.tools) == 0 {
		return ""
	}
	t := m.tools[len(m.tools)-1]
	return t.Name + ": " + truncate(t.Result, 80)
}

// SanitizeMessagesForAnthropic enforces the Anthropic API rule that every
// assistant `tool_use` block must be immediately followed (in the next
// message) by a user `tool_result` block referencing the same tool_use id,
// AND that every user `tool_result` block must reference a tool_use id that
// was emitted in some prior assistant turn (orphan tool_results trigger
// the symmetric 400 "tool result's tool id … not found").
//
// BUG-WEREWOLF-P1-LLM-TOOL (Round 26): DeepSeek proxy returned HTTP 400
//
//	`messages.11: tool_use ids were found without tool_result blocks
//	 immediately after: call_01_…`. The cause was that some messages in the
//	bot's Memory had an assistant(tool_use) message whose companion
//	tool_result was missing — typically because the previous LLM call had
//	failed mid-loop (network / timeout / quarantine dispatch) after the
//	assistant turn was already recorded, OR Memory.Prune() severed a paired
//	pair across the 120-message window boundary.
//
// BUG-WEREWOLF-P1-LLM-TOOL-ORPHAN-RESULT (Round 70+): Anthropic-protocol
// proxy returns HTTP 400 `invalid params, tool result's tool id(...) not
// found (2013)` when a user tool_result in the request refers to a
// tool_use_id that does not appear in any preceding assistant turn. The
// observed trigger is Memory.Prune() cutting between an
// assistant(tool_use X) and its following user(tool_result X) at the
// 120-message boundary: the tool_use is dropped while its tool_result
// stays, leaving an orphan tool_result at the top of the next snapshot.
//
// This helper walks the snapshot in two passes:
//
//	Pass 1 — collect every tool_use id emitted by an assistant turn so we
//	         know which tool_results are well-formed.
//	Pass 2 — emit messages, dropping any user tool_result block whose
//	         tool_use_id was never advertised (orphan) AND synthesising a
//	         synthetic error tool_result for any assistant tool_use that
//	         lacks a following tool_result (the original Round-26 patch).
//
// The patched slice is returned; the underlying Memory is NOT mutated (a
// patch here is request-scoped and doesn't pollute the long-term
// transcript that the UI AgentThoughtPanel renders).
//
// Returns (sanitizedMsgs, patchedCount). patchedCount > 0 means a defensive
// fix was applied for this round.
func SanitizeMessagesForAnthropic(msgs []llm.Message) ([]llm.Message, int) {
	if len(msgs) == 0 {
		return msgs, 0
	}

	// Pass 1: collect every tool_use id from assistant turns so we can
	// detect orphan tool_results in pass 2.
	knownUseIDs := map[string]bool{}
	for _, m := range msgs {
		if m.Role != "assistant" {
			continue
		}
		for _, c := range m.Content {
			if c.Type == "tool_use" && c.ID != "" {
				knownUseIDs[c.ID] = true
			}
		}
	}

	out := make([]llm.Message, 0, len(msgs))
	patched := 0
	for i := 0; i < len(msgs); i++ {
		m := msgs[i]

		// Pass 0 — strip `thinking` content blocks from assistant turns.
		// §14.1 权威用例:messages[].content[] 只含 text/tool_use/tool_result
		// 三种块,**从不携带 thinking 内容块**。当模型开启 extended thinking
		// 后,其响应里会带 {type:"thinking"} 内容块;流式累加器 finalizeBlock
		// 会把它物化进 resp.Content,recordTranscript 再写回 Memory。下一轮
		// 请求把它原样回显,而 ContentBlock.MarshalJSON 的 thinking 分支只能
		// 产出 {"type":"thinking","budget":N}(budget=0 时被 omitempty 吞掉,
		// 退化为 {"type":"thinking"}),严格代理(DouBao/DeepSeek/GLM)因此报
		// 400 "missing messages.content.thinking parameter"。
		// 修复:请求期把 thinking 内容块从 assistant turn 剔除——thinking 是
		// 模型的瞬时推理,不属于可重放的对话历史,剔除后语义无损。
		if m.Role == "assistant" {
			kept := make([]llm.ContentBlock, 0, len(m.Content))
			for _, c := range m.Content {
				if c.Type == "thinking" {
					patched++
					continue
				}
				kept = append(kept, c)
			}
			m.Content = kept
		}

		// Pass 2 (forward path) — drop orphan tool_result blocks (those
		// referencing tool_use_ids that no assistant turn ever emitted).
		// Pure-text or pure-tool_use content is preserved as-is. If a user
		// message contains BOTH a tool_result and a text block, we keep
		// the text and only drop the orphan tool_result block; if the
		// message becomes empty we drop the whole message.
		if m.Role == "user" {
			kept := make([]llm.ContentBlock, 0, len(m.Content))
			seenResultIDs := make(map[string]bool)
			for _, c := range m.Content {
				if c.Type == "tool_result" && c.ToolUseID != "" {
					if !knownUseIDs[c.ToolUseID] {
						patched++
						continue
					}
					// BUG-R234-P2-01 (2026-08-03): one tool_use id may have exactly
					// one tool_result in a user turn. Concurrent wake paths can record
					// the same tool call result twice; after adjacent user turns merge,
					// strict proxies reject the request with "each tool_use must have a
					// single result". Keep the first result in wire order and drop later
					// duplicates. This is request-scoped and does not mutate Memory.
					if seenResultIDs[c.ToolUseID] {
						patched++
						continue
					}
					seenResultIDs[c.ToolUseID] = true
				}
				kept = append(kept, c)
			}
			if len(kept) == 0 {
				// The whole user message was just orphan tool_results;
				// drop it entirely so we don't leave a user turn with
				// no content (which Anthropic also rejects).
				continue
			}
			m.Content = kept
		}

		out = append(out, m)

		if m.Role != "assistant" {
			continue
		}
		// Collect tool_use ids from this assistant turn.
		var useIDs []string
		for _, c := range m.Content {
			if c.Type == "tool_use" && c.ID != "" {
				useIDs = append(useIDs, c.ID)
			}
		}
		if len(useIDs) == 0 {
			continue
		}

		// Determine which ids are already paired with a following tool_result.
		paired := map[string]bool{}
		if i+1 < len(msgs) {
			next := msgs[i+1]
			if next.Role == "user" {
				for _, c := range next.Content {
					if c.Type == "tool_result" && c.ToolUseID != "" {
						paired[c.ToolUseID] = true
					}
				}
			}
		}
		// Synthesise tool_results for the unpaired ids and append them as a
		// new user message right after this assistant turn.
		var unpaired []string
		for _, id := range useIDs {
			if !paired[id] {
				unpaired = append(unpaired, id)
			}
		}
		if len(unpaired) == 0 {
			continue
		}
		synth := llm.Message{Role: "user"}
		for _, id := range unpaired {
			synth.Content = append(synth.Content, llm.ContentBlock{
				Type:      "tool_result",
				ToolUseID: id,
				Content: []llm.ContentBlock{{
					Type: "text",
					Text: "tool call interrupted; result unavailable (BUG-WEREWOLF-P1-LLM-TOOL safety patch).",
				}},
				IsError: true,
			})
			patched++
		}
		out = append(out, synth)
	}

	// 合并连续 user 消息 — Anthropic 协议硬约束:user/assistant 必须严格交替,
	// 不允许连续 2 条及以上 role=user 的消息(会触发 400 请求拒绝)。
	// 本 Agent 的两类场景会产生连续 user:
	//   1. recordToolResult 推入 tool_result(user) 后,下一轮 handleEvent 又
	//      推入 game_state(user),形成 [user(tool_result), user(game_state)];
	//   2. CompressHistoryLocked 产出的 [identity(user), summary(user)] 头两条。
	// 合并策略:把相邻 user 消息的 content blocks 拼到一起,等价于"同一条 user
	// 消息内先收 tool_result 再收状态通知"——这正好对应发起方意图(一次决策周期
	// 内先看到工具结果,再看到状态推送)。请求期 patch,不修改 Memory 长驻转录。
	out, n := mergeConsecutiveUserMessages(out)
	patched += n

	// Merging adjacent user turns can bring two tool_result blocks for the
	// same tool_use id into one message even when each original message was
	// individually valid. Run the duplicate guard once more on the final wire
	// shape so the Anthropic one-result-per-tool-use invariant still holds.
	for i := range out {
		if out[i].Role != "user" {
			continue
		}
		seen := make(map[string]bool)
		kept := make([]llm.ContentBlock, 0, len(out[i].Content))
		for _, c := range out[i].Content {
			if c.Type == "tool_result" && c.ToolUseID != "" {
				if seen[c.ToolUseID] {
					patched++
					continue
				}
				seen[c.ToolUseID] = true
			}
			kept = append(kept, c)
		}
		out[i].Content = kept
	}

	return out, patched
}

// mergeConsecutiveUserMessages 把相邻的 role=user 消息合并为一条(拼接
// content blocks),确保返回的序列严格满足 user/assistant 交替规则。
// 返回 (mergedSlice, mergeCount)。mergeCount > 0 代表本轮合并了 N 对。
func mergeConsecutiveUserMessages(msgs []llm.Message) ([]llm.Message, int) {
	if len(msgs) < 2 {
		return msgs, 0
	}
	out := make([]llm.Message, 0, len(msgs))
	merged := 0
	for _, m := range msgs {
		if m.Role == "user" && len(out) > 0 && out[len(out)-1].Role == "user" {
			// 拼接到前一条 user 消息末尾(保持原有 blocks 顺序)。
			out[len(out)-1].Content = append(out[len(out)-1].Content, m.Content...)
			merged++
			continue
		}
		out = append(out, m)
	}
	return out, merged
}

// itoa avoids importing strconv in this hot path.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
