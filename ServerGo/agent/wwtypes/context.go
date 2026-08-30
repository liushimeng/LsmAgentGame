// Package wwtypes — context.go: 狼人杀玩家 Agent 的 GameContext 契约类型。
//
// 2026-08-06 §Agent 重构 Step 2:从 ServerGo/agent/prompt.go 抽出。
// GameContext 是 wwplayer(玩家 Bot) 与 game/werewolf(引擎) 共享的核心
// 契约 — 引擎侧 buildAgentContextLocked 填充它,玩家 Agent 消费它,法官
// Agent 在 GameSnapshot 派生路径也引用它。
//
// 依赖约束:本包**只** import agentcore (通用基础设施),
// **不得** import game/werewolf 或 agent/wwplayer / agent/wwjudge,
// 避免循环 import。
package wwtypes

import (
	agentcore "LsmAgentGame/agent/core"
)
type GameContext struct {
	Round           int
	Phase           string
	Role            string // role string (e.g. "werewolf", "villager"); "" if unknown
	MySeat          int    // 0-indexed seat number; -1 if unknown. Used to compute the 1-indexed 玩家编号 for self-introduction (BUG-WEREWOLF-P0-NEW-10).
	MyTurn          bool
	SpeakTurn       int // -1 = N/A
	AliveSeats      []int
	LastNightDeaths []int
	MySeerCheck     int // -1 = N/A (only meaningful for seer)
	WolfTarget      int // -1 = N/A (witch view)

	// 2026-08-13 §20260813-01 U2 — 分层缓存:静态层(整局不变) + 阶段层(阶段内不变)。
	// 由 buildAgentContextLocked 填充,供 prompt 构建器读取。
	Static      *StaticContext      `json:"static,omitempty"`
	PhaseState  *PhaseStateContext  `json:"phase_state,omitempty"`

	// 2026-08-12 §20260812-04 U1 (P0-1) — 夜间私有信息进 prompt。
	//
	// 缺陷背景:MySeerCheck / WolfTarget 自落地起就由引擎填充(room_agent.go:766/770),
	// 但 agent/ 目录下 **零读取点**、prompt.go **零渲染** —— 即 AI 预言家查完人
	// 永远不知道结果、AI 女巫永远不知道今晚谁被刀,两个核心神职技能对 Agent
	// 完全失效。而人类玩家走 view.go:1285 BuildSeerInform 可正常看到 —— 直接
	// 违反 §15 公平性与 §120。
	//
	// 更关键:MySeerCheck 只存「查验的座位号」,连阵营结果都没存。
	// FactionOf(gs.Roles[t]) 这个结果原先只在 BuildSeerInform 里算过,
	// 从未进过 GameContext。故此处必须新增 Faction 字段而非仅渲染旧字段。
	//
	// MySeerCheckFaction 是上一晚查验结果的阵营:"wolf"(查杀) / "good"(金水) / ""(未查)。
	MySeerCheckFaction string
	// MySeerCheckHistory 是本局全部查验历史(按轮次升序),仅预言家填充。
	// 预言家的核心价值就是这张表 —— 单看「上一晚」会丢失前几轮的金水/查杀。
	MySeerCheckHistory []SeerCheckRecord
	// WitchAntidoteUsed / WitchPoisonUsed 是女巫两瓶药的剩余状态(仅女巫填充)。
	// 女巫需要知道「我还有没有药」才能做决策;原先只有 tool enum 隐式表达。
	WitchAntidoteUsed bool
	WitchPoisonUsed   bool
	// 2026-07-29 §134 守卫角色 — 上晚守护目标。
	// -1 = 无(首夜/上晚空守/从未守过);
	// 仅守卫 Guard 填充;用于 BuildTools 时 enum 剔除上晚守护目标(G1 不可连守同一人)。
	// 守卫 GameContext.WolfTarget 恒为 -1(盲守语义,守卫看不到狼刀目标)。
	GuardLastProtect int
	RecentSpeeches   []SpeechEvent
	WhisperInbox     []WhisperEvent

	// BUG-WEREWOLF-P0-4: structural-phase driver signals. In full-AI rooms
	// there is no human GM, so the lowest-seat alive bot is the "host driver"
	// responsible for advancing dawn→start_day, sheriff→sheriff_elect and
	// vote→finish_vote. MyVoted/AllVoted keep non-driver agents from
	// re-voting on every wake and let the driver know when to tally.
	IsDriver bool
	MyVoted  bool
	AllVoted bool

	// GameStartedAt is the Unix timestamp (seconds) when the game started.
	// Used by BuildUserPrompt to inform agents of game duration context.
	GameStartedAt int64

	// BUG: 狼人杀 7 人局 Agent 多轮上下文(2026-07-08 新增)——每玩家档案表。
	// manager 侧在构造 GameContext 时遍历 Seats()/BotAccount()/BotAgentName()
	// 填充;prompt.go 的 appendIdentityBlock 用它生成【玩家档案】块。
	AllPlayers []PlayerBrief

	// BUG: 狼人杀 7 人局 Agent 首夜发言缓冲期(2026-07-08 新增)。
	// 仅在 phase == "pre_wolves" 期间 > 0:缓冲期剩余秒数。
	GraceRemainingSec int

	// BUG Round 40 §95: 首夜强制发言阶段进度字段。仅在 phase == "pre_wolves"
	// 时填充;其余阶段为 0。LLM 看到"第 N/M 轮,你已发 X 次"强提示后,会主动
	// 调 speak 工具累加计数,避免选 idle_silent/end_turn 沉默冷场。
	PreWolvesRound          int
	PreWolvesRoundsTotal    int
	PreWolvesCountForMySeat int

	// 2026-07-09 §13-bugfix 改造 — 500K 聊天历史队列 WindowFor。
	// manager 侧 WerewolfRoom.buildAgentContextLocked 从 r.chatQueue.WindowFor(seat)
	// 填充(取自该 bot 的 read pointer 之后的所有新增消息,公平性保证);
	// BuildUserPrompt 在 RecentSpeeches / WhisperInbox 段之后渲染。
	ChatHistory []agentcore.ChatMessage

	// BUG 2026-07-09: 遗言阶段当前遗言座位(0-indexed)。仅在 phase ==
	// "death_lyric" 时 >= 0;其余阶段为 -1。构建 BuildUserPrompt 的【遗言】块
	// 与 agent/tools.go 的 last_words 工具暴露判定都基于此字段。
	DeathLyricCurrent int

	// 2026-07-10: 重开局投票阶段状态(由 manager.buildAgentContextLocked
	// 在 phase == "restart_vote" 时填充)。BuildUserPrompt 据此渲染【重开局投票】块。
	LastWinner              string // 上一局 winner ("wolf"/"good"/"")
	RestartVoteRemainingSec int    // 投票窗口剩余秒数;0 = 未到/无 deadline
	RestartVoteDecided      bool   // true → 已结算
	RestartVoteResult       string // "passed" | "rejected" | "timeout"

	// 2026-07-10 §120 增强 — 模型响应速率公平性字段(由 manager 侧填充)。
	// ModelName: 当前 bot 的 LLM 模型名(例 "DeepSeek-model"),用于在
	//   【模型响应速率】块渲染对比。
	// MyAvgLLMLatencyMs / MyLastLLMLatencyMs / MyTotalLLMCalls: 本 bot 自己
	//   的 API 调用耗时统计(由 Agent.AvgLLMLatencyMs() 等 getter 取)。
	// RoomFastestModel / RoomSlowestModel: 房间内最快/最慢模型的名称与
	//   耗时(ms),让 LLM 调整对话策略(慢模型少说话,快模型多承担)。
	// IsHumanInRoom: 是否房间里有真人玩家(决定走「与人类交互」还是
	//   「全 AI 房间」实时性策略块)。
	ModelName            string // 当前 bot 的 LLM AgentName/ModelKey
	MyAvgLLMLatencyMs    int64  // 本 bot 平均 API 耗时 (ms)
	MyLastLLMLatencyMs   int64  // 本 bot 上一次 API 耗时 (ms)
	MyTotalLLMCalls      int    // 本 bot 本局累计 LLM 调用次数
	RoomFastestModel     string // 房间内最快模型名
	RoomFastestLatencyMs int64  // 房间内最快模型平均耗时 (ms)
	RoomSlowestModel     string // 房间内最慢模型名
	RoomSlowestLatencyMs int64  // 房间内最慢模型平均耗时 (ms)
	IsHumanInRoom        bool   // 是否有真人玩家在座位上(非观战者)

	// 2026-07-24 优化:房间是否处于 UI 暂停状态。Agent handleEvent 入口
	// 据此跳过 LLM 调用(防批量 quarantine),watchdog / wake 同样跳过。
	// 仅作为 game.state 字段下发,LLM prompt 中不渲染(避免泄漏策略)。
	RoomPaused bool

	// SeatCount 本局实际人数(13=默认,12=werewolf_12 历史兼容,7=werewolf_7 历史兼容)。
	// 由 manager.buildAgentContextLocked 在构造 GameContext 时从 r.State.SeatCount 写入。
	// BuildSystemPrompt / BuildSystemPromptWithEmotion 据此选择对应规则摘要渲染,
	// 让 13/12/7 人局看到各自匹配的屠边阈值 + 角色配置说明。
	SeatCount int

	// 2026-07-10 12 人标准竞技局新增字段(由 manager.buildAgentContextLocked 填充)。
	// SheriffSeat: 当前警长座位(-1=无警长)。
	// SheriffStream: 第一/第二警徽流目标(槽位 [0]=第一, [1]=第二),-1=未声明。
	// IdiotRevealedSeats: 已翻牌的白痴座位(全局公开,翻牌后仍存活但失去投票权)。
	// DivineCnt / PlainCnt / WolfAliveCnt: 当前存活的神职/平民/狼人屠边计数。
	SheriffSeat        int    // 当前警长座位(-1=无)
	SheriffStream      [2]int // 第一/第二警徽流目标(-1 未声明)
	// SheriffCandidates 警长竞选已参选座位(仅 PhaseSheriff 有意义)。
	// §报告-20260804-03 BUG-07: sheriff 阶段此前不挂 vote 工具,AI 全程无法投
	// 警长票 → 每局都「本局无警长」。补挂 vote 后需要这份名单来收敛 enum ——
	// 只能投给已参选者,避免 LLM 投给没参选的人导致票数分散。
	SheriffCandidates  []int
	// MyCandidate / MyVoted 当前 bot 自己的参选 / 投票状态,供 prompt 与工具
	// enum 判断是否还需要行动(与 BUG-08 的 MyTurn 判定同源)。
	MyCandidate        bool
	IdiotRevealedSeats []int  // 已翻牌白痴
	DivineCnt          int    // 存活神职数(预+女巫+猎+白痴)
	PlainCnt           int    // 存活平民数
	WolfAliveCnt       int    // 存活狼人屠边参考

	// 2026-07-11: 预言家发起投票状态。
	VoteProposed bool // 是否已由预言家发起投票
	VoteProposer int  // 发起投票的座位号(-1=未发起)

	// 2026-07-10 §124 增强 — 情绪模块字段(由 manager.buildAgentContextLocked 填充)。
	// MyEmotion: 当前 bot 的情绪 key;MyEmotionReason: 切换原因(LLM 给出);
	// OthersEmotion: 其它 bot 的 (seat, emotion, reason, updated_at) 列表。
	// 情绪走 wire 层公开,与 §119 HeartThought 的协议层隔离形成对照 —
	// 所有真人玩家 + 其它 Agent + 观众都能看到。
	MyEmotion       string             `json:"my_emotion,omitempty"`
	MyEmotionReason string             `json:"my_emotion_reason,omitempty"`
	OthersEmotion   []SeatEmotionBrief `json:"others_emotion,omitempty"`

	// 2026-07-12 §127 — 阶段剩余秒数,由 manager.buildAgentContextLocked 从
	// gs.PhaseDeadlineAt 计算,供 BuildUserPrompt 注入紧迫感提示。
	PhaseDeadlineRemainingSec int

	// 2026-08-11 §20260811-05 U1 — 玩家行为画像(PlayerProfile)。
	// seat(0-indexed) → 该座位人类玩家的跨局打法画像摘要(≤400 rune)。
	// 仅人类座位有值,且只含「本 bot 模型视角」的画像(主键 model_key+user_id
	// 天然按模型隔离);全 AI 房间或无画像时为 nil,PlayerProfileBlock 渲染空串。
	// 数据源:t_lsm_game_agent_player_profile,经房间级预取缓存注入(热路径零 DB)。
	PlayerProfiles map[int]string `json:"player_profiles,omitempty"`

	// ─── 道具系统 (2026-07-21 v2 重设计) ───
	// PropSnapshot 是 speak 阶段可购买的道具快照列表（buildAgentContextLocked 填充），
	// 驱动 use_prop 工具 schema 动态生成（对齐设计文档 §4.1）。
	PropSnapshot []PropSnapshot
	// PropCooldownRemainingSec 道具冷却剩余秒数（0 = 可用）。
	PropCooldownRemainingSec int
	// PropUsedThisGame 本局已使用道具数量。
	PropUsedThisGame int
	// PropMaxPerGame 本局最多可购买道具数（默认 3）。
	PropMaxPerGame int
	// PropInjectText 是待注入到 user prompt 的道具注入文本（由服务端生成）。
	// buildAgentContextLocked 消费 GameContext 时把它渲染到 prompt 末尾。
	PropInjectText string
	// PropLastEffect 是最近一次道具使用的效果摘要（反馈给 LLM）。
	PropLastEffect string

	// ─── 道具命中干扰信号（v2 重设计 —— 命中后 GameContext 注入"干扰信号"，不替 LLM 决策） ───
	// EffectExpose 为 true 时 prompt 追加"你的 internal_thought 已被系统标记为可疑"。
	EffectExpose bool
	// EffectAttentionScatter 为 true 时 ToolUseMaxOverride 生效（强制简化决策）。
	EffectAttentionScatter bool
	// ToolUseMaxOverride 覆盖本轮 tool_use 上限（0 = 默认 5；命中后降到 2）。
	ToolUseMaxOverride int
	// EffectTargetTwistSeat 是目标选择的直觉引导座位（0-indexed, -1=无）。
	// 仅对目标选择类工具生效（wolf_kill/seer_check/witch_poison/vote/hunter_shoot）。
	EffectTargetTwistSeat int
	// EffectForceEmotion 强制切换为情绪不稳（"confused"/"guilty", ""=无）。
	EffectForceEmotion string
	// PropSeatBudgetUsed 是本座位本局道具消耗累计（用于全局预算校验/展示）。
	PropSeatBudgetUsed int64
	// RoomPropBudget 是房间级全局预算上限。
	RoomPropBudget int64
	// RoomPropBudgetUsed 是房间级全局已消耗。
	RoomPropBudgetUsed int64

	// ─── 长期金币生存 (2026-07-21 v3 重构 §G3) ───
	// WalletBalance 是 bot 自身的金币余额（由 manager.buildAgentContextLocked
	// 从 walletSvc.GetBalance 填充）。WalletSustainabilityBlock 渲染"可承受局数"
	// + 4 档紧急度(健康/警戒/危险/濒死) + 决策原则。
	WalletBalance int64
	// AnteAmount 是单局底注金额（决定"还能承受几局"的计算分母）。默认 100。
	AnteAmount int64

	// ─── 道具公开使用历史 (2026-07-21 v3 §G5) ───
	// PropHistorySnapshot 是最近 20 条公开道具使用记录（from/to/prop/hit/effect）。
	// 由 manager.buildAgentContextLocked 从 r.GetPropHistoryLocked 填充,
	// 供 prop_history 工具查询 + PropUserPromptBlock 渲染"近期道具使用动态"段。
	PropHistorySnapshot []PropHistoryRecord

	// ─── 狼人夜间投票 (2026-07-17) ───
	// WolfVotes 是 seat -> target 投票快照(仅狼人玩家在 night_wolves 阶段可见)。
	WolfVotes map[int]int
	// WolfVoteReasons 是 seat -> 刀人理由 快照(§20260810-04 U2,K3-S1 二期)。
	// 与 WolfVotes 同条件填充(仅狼 bot / night_wolves / 已投票且理由非空)。
	WolfVoteReasons map[int]string

	// §20260809-02 U2 Bot 票型回灌 —— 上一轮白天投票的「谁投了谁」快照。
	// key = voter seat (0-indexed, 0~12), value = target seat (0-indexed)。
	// 仅在白天投票阶段结束后填充(下一个白天阶段起始时 buildAgentContextLocked
	// 从 r.State.LastDayVoteMap 拷贝);其余阶段为 nil。
	// 这是 §135 公平性的镜像修复:人类玩家已通过聊天流能看到票型,
	// 现在 Agent 也获得同样的推理素材(原 LongCat-D1 文档 P0 缺陷)。
	LastDayVoteMap map[int]int `json:"last_day_vote_map,omitempty"`
	// WolfVotesCast 已投票狼人数(含弃权)。
	WolfVotesCast int
	// WolfTotalWolves 存活狼总数。
	WolfTotalWolves int
	// WolfVoting true=投票中;false=已结算。
	WolfVoting bool
	// WolfVoteTally 计票结果(WolfVoting=false 时填充)。
	WolfVoteTally *WolfVoteTally
	// MyWolfVoteCast 是当前 bot 是否已投票(含弃权)。仅对狼 bot 在
	// night_wolves 阶段有意义。R196 报告 P1:Bot 反复调 wolf_kill 15+ 次
	// 而服务端仅覆盖,LLM 看不到反馈循环;新增此字段让 prompt 显式提示
	// 「你已投票,无需再调 wolf_kill」。
	MyWolfVoteCast bool

	// ─── 狼人小队交流 v4 §13.1 ───
	// Faction 是本 bot 所属阵营（"wolf"/"good"/""）。仅在狼 bot 时挂载 wolf_whisper 工具。
	Faction string
	// WolfTeammateSeats 是开局系统注入的所有狼队友座位列表（0-indexed, 空=非狼人）。
	// v20260830-01 起所有狼人可知全部狼队友身份，无随机性，保证公平。
	WolfTeammateSeats []int
	// WolfPackSnapshot 是最近 20 条狼小队留言（buildAgentContextLocked 从房间 WolfPackRoom.Snapshot 填充）。
	// 仅狼 bot 在 user prompt 中看到(协议层隔离 — 不入 HeartThought/公屏/观众)。
	WolfPackSnapshot []WolfPackMsg

	// ─── 狼队战术分工 (2026-08-10 §20260810-10 U1) ───
	// WolfPackRole 是本 bot 的战术分工("hype"/"charger"/"hook"/"deep", "" = 未指派/非狼)。
	// WolfPackRoleTable 是全狼分工表(seat -> role),仅狼 bot 可见。
	// WolfKingSeat 是当前轮值狼王座位(-1 = 无);狼王可用 wolfpack_assign 工具重排分工。
	// §119 协议层隔离:三者仅经 GameContext 注入 user prompt,不进 chat 表/队列/HeartThought。
	WolfPackRole      string
	WolfPackRoleTable map[int]string
	WolfKingSeat      int

	// WolfPackCipher 是 §20260811-04 U1 的「狼队暗号系统」快照。
	// 仅在 cipher_mode 为 starter/advanced 时非 nil;否则 nil → prompt 不渲染暗号块。
	// §119 协议层隔离:不进 chat_message / chat_history / HeartThought。
	WolfPackCipher *WolfPackCipherBundle `json:"wolf_pack_cipher,omitempty"`

	// ─── 经济档位 v4 §13.2 ───
	// EconTier 是当前房间的经济档位（"health"/"caution"/"danger"）。
	// EconTierFeedbackBlock 拼到 PropUserPromptBlock 末尾展示档位 + 销毁率,
	// 让 LLM 在决策"是否用道具"时感知房间经济压力。
	EconTier string

	// ─── 人类反制道具 (2026-08-07 §20260807-04 P0-3) ───
	// HumanDebuff 非 nil 时,表示本 bot 对某人类座位施加了游戏内 debuff
	// （公告前缀 / 伪造投票推荐 / 乱码干扰）,HumanDebuffBlock 据此渲染告知文案。
	HumanDebuff *HumanDebuffSpec

	// PropHitLastRound 是上一轮本 bot 被道具击中的摘要（如「苦苦哀求(emotion_disturb)」）。
	// 由 buildAgentContextLocked 在防御性重置前把上一轮 PropLastEffect 转存到房间字段,
	// 本轮填入 GameContext；PropEffectSignalBlock 末尾渲染,形成「被击中反馈闭环」。
	PropHitLastRound string

	// ─── 行为承诺系统 (2026-08-10 §20260810-06) ───
	// MyCommitments 是本 bot 自己的承诺列表（含真实兑现状态）。
	// PublicCommitments 是公开可见的他人承诺列表（仅 pending 状态）。
	MyCommitments    []CommitmentInfo
	PublicCommitments []CommitmentInfo

	// ─── 信息账本二期 (2026-08-10 §20260810-08) ───
	// KnowledgeDigest 是本 bot 的知情清单摘要，仅经 GameContext 注入 user prompt。
	// §119：不进 chat_message / chat_history / BotTranscript。
	KnowledgeDigest *KnowledgeDigest `json:"knowledge_digest,omitempty"`

	// ─── 行为一致性校验 (§20260811-06 U4) ───
	// LastConsistencyCheck 是本 bot 最近一次一致性校验结果(R1 反复跳变 /
	// R2 平民跳神 / R3 投票矛盾 / OK)。仅经 GameContext 注入 user prompt 末尾
	// 的 ⚠️ 块,§120 公平性:不计入 consecutiveFailures。
	LastConsistencyCheck *AgentConsistencyCheck `json:"last_consistency_check,omitempty"`

	// ─── 黎明流言系统 (§20260811-06 U5) ───
	// LastRumors 是最近 5 条流言(由 buildAgentContextLocked 从
	// werewolf.LastRumors 拷贝填入)。RumorBlock 据此拼到 user prompt
	// 末尾,Agent 据此评估"哪些流言可信"。
	LastRumors []RumorJSON `json:"last_rumors,omitempty"`

	// ─── 多假说并行推演 (2026-08-10 §20260810-07) ───
	// HypothesisTable 是本 bot 对其他 12 名玩家的身份假说集。
	// buildAgentContextLocked 从房间 HypothesisStore.GetLocked(seat) 拷贝填充;
	// 不存在的 bot(HypothesisStore 未记录)此处为 nil。
	// §128 对话即思考:仅承载运行时中转,不增加 wire 字段。
	// §119 协议层隔离:不进 chat_message / chat_history。
	HypothesisTable *HypothesisTableSnapshot `json:"hypothesis_table,omitempty"`

	// ─── 公开质疑 (2026-08-10 §20260810-11 H2) ───
	// LastChallenge 非空时,表示本 bot 在本白天被 {BySeat+1} 号公开质疑。
	// buildAgentContextLocked 从房间 r.State.Players[seat].LastChallengedBy/Question 读取;
	// 渲染走 ChallengeBlock,LLM 自行决定是否在下一轮 speak 回应(§128 对话即思考)。
	LastChallenge *LastChallengeSpec `json:"last_challenge,omitempty"`

	// ─── 发言影响力生态 (2026-08-11 §20260811-02 U1) ───
	// InfluenceScores 是全场每个座位的公开影响力分数(0~100)。
	// buildAgentContextLocked 从房间 InfluenceTracker.SnapshotLocked() 拷贝填充。
	// MyInfluence 是本 bot 自己那一条(便于 prompt 渲染时免去查找);nil = 尚未计算
	//(第 1 天投票结束前 tracker 为空)。
	// §119 对照:与 HeartThought 相反,影响力是**公开**信息 —— 它同时进
	// ClientGameState 全员可见,此处注入 prompt 只是让 Agent 也能感知自己的分量。
	// §135:不含任何角色信息,只反映公开行为(票型/发言/被指向)。
	InfluenceScores []InfluenceBrief `json:"influence_scores,omitempty"`
	MyInfluence     *InfluenceBrief  `json:"my_influence,omitempty"`

	// ─── 身份偏见 (2026-08-26 §20260826-01 U1) ───
	// RolePrior 是本 bot 对其他 12 个座位的身份先验分布(开局均匀 + 死亡公开 + 人格加权)。
	// buildAgentContextLocked 从房间 RolePriorStore.GetLocked(seat) 拷贝填充。
	// §119 协议层隔离:不进 chat_message / chat_history / HeartThought。
	// §128 对话即思考:不新独立 LLM 调用;纯服务端计算。
	RolePrior *RolePriorSnapshot `json:"role_prior,omitempty"`

	// ─── 记忆印象 (2026-08-26 §20260826-01 U2) ───
	// ImpressionMemory 是本 bot 对其他 12 个玩家的多维人格观感
	// (Trust/Competence/Sincerity/Cooperation/Threat + 衰减)。
	// buildAgentContextLocked 从房间 ImpressionStore.GetLocked(seat) 拷贝填充。
	// §119 协议层隔离:不进 chat_message / chat_history / HeartThought。
	// §128 对话即思考:5 类事件触发自动聚合;不新独立 LLM 调用。
	ImpressionMemory *ImpressionMemorySnapshot `json:"impression_memory,omitempty"`

	// ─── 情绪→推理权重 (2026-08-26 §20260826-01 U4) ───
	// EmotionReasoningWeights 是当前 bot 情绪对应的推理修正向量。
	// 由 buildAgentContextLocked 根据 r.State.Players[seat].Emotion 实时计算。
	// 仅供 prompt.go 渲染「情绪影响推理」段,LLM 自由决定是否遵守(§128)。
	EmotionReasoningWeights *EmotionReasoningWeightsSnapshot `json:"emotion_reasoning_weights,omitempty"`

	// ─── 死亡亮身份 (2026-08-30 §20260830-01) ───
	// RevealRoleOnDeath 本局房间级「死亡亮身份」开关(system prompt §135 规则段
	// 双模式切换用)。true = 任何玩家死亡/处决时身份对全场公开;false = §135
	// 竞技规则(死者身份牌不翻开)。由 buildAgentContextLocked 从
	// gs.RevealRoleOnDeath 拷贝,整局不变(prompt 字节稳定,§20260813-05 U5)。
	RevealRoleOnDeath bool `json:"reveal_role_on_death,omitempty"`
	// RevealedRoles 已对全场公开身份的座位(0-indexed)→ 角色名(role key,
	// 如 "werewolf"/"hunter")。§135 单点判定:仅 RolePubliclyRevealed 命中的
	// 座位进入(引擎侧 revealedRolesSnapshotLocked 备好),未公开座位不在 map
	// 中 —— 禁止任何一侧直接读 gs.Roles 原始数组推导。prompt 的【死亡白名单】
	// 据此为死亡座位附带「(身份:X,已公开)」;关闭模式下普通死亡不在 map,
	// 输出与现状逐字节一致。
	RevealedRoles map[int]string `json:"revealed_roles,omitempty"`
}

// SeerCheckRecord 是预言家一次查验的完整结果(座位 + 阵营 + 轮次)。
// 2026-08-12 §20260812-04 U1 (P0-1) 新增。
//
// Faction 取 "wolf"(查杀) / "good"(金水)。注意这是**阵营**不是具体身份 ——
// 预言家只知阵营不知具体神职(§15 规则),故此处刻意不含 Role 字段,
// 从类型上杜绝「查验结果泄漏具体身份」这类越权渲染。
type SeerCheckRecord struct {
	Round   int    `json:"round"`
	Seat    int    `json:"seat"`    // 0-indexed;渲染时 +1 转对外编号(§82a)
	Faction string `json:"faction"` // "wolf" / "good"
}

// InfluenceBrief 是 werewolf.InfluenceScore 在 agent 侧的镜像结构。
// 为避免 agent → werewolf 循环导入,此处独立定义(同 WolfPackMsg 的处理方式,§133)。
// §20260812-02 U2 — 新增第 5 维度 insight（洞察力，0~15）。
type InfluenceBrief struct {
	Seat       int `json:"seat"`
	Total      int `json:"total"`      // 0~100
	Persuasion int `json:"persuasion"` // 0~35 跟票率
	Attention  int `json:"attention"`  // 0~20 关注度
	Presence   int `json:"presence"`   // 0~18 发言参与
	Survival   int `json:"survival"`   // 0~12 存活加成
	Insight    int `json:"insight"`    // 0~15 洞察力(§20260812-02 U2)
}

// KnowledgeDigestEntry 是本座位从某来源获知的信息聚合摘要。
// 2026-08-10 §20260810-08 信息账本二期 —— 行为侧消费链路。
type KnowledgeDigestEntry struct {
	Source     string   `json:"source"`
	Count      int      `json:"count"`
	LastRound  int      `json:"last_round"`
	Highlights []string `json:"highlights"`
}

// KnowledgeDigest 是注入 GameContext 的「我的知情清单」。
type KnowledgeDigest struct {
	Seat        int                    `json:"seat"`
	TotalKnown  int                    `json:"total_known"`
	TotalInRoom int                    `json:"total_in_room"`
	Entries     []KnowledgeDigestEntry `json:"entries"`
}

// CommitmentInfo 是 GameContext 中注入的承诺摘要（避免 agent→werewolf 循环导入）。
type CommitmentInfo struct {
	ID        int64  `json:"id"`
	Round     int    `json:"round"`
	Template  string `json:"template"`
	ParamSeat int    `json:"param_seat"`
	Reason    string `json:"reason"`
	Status    string `json:"status"`
}

// HypothesisEntrySnapshot 一条假说快照（避免 agent→werewolf 循环导入）。
// 与 werewolf.HypothesisEntry 字段一致但独立定义。
type HypothesisEntrySnapshot struct {
	TargetSeat int    `json:"target_seat"`
	RoleGuess  string `json:"role_guess"`
	Confidence int    `json:"confidence"`
	Supporting string `json:"supporting"`
	Refuting   string `json:"refuting"`
	UpdatedAt  int64  `json:"updated_at"`
}

// HypothesisTableSnapshot 是 GameContext 中注入的假说表快照。
type HypothesisTableSnapshot struct {
	Seat      int                        `json:"seat"`
	Entries   []HypothesisEntrySnapshot  `json:"entries"`
	Round     int                        `json:"round"`
	UpdatedAt int64                      `json:"updated_at"`
}

// HumanDebuffSpec 描述对某人类座位施加的游戏内 debuff（2026-08-07 §20260807-04 P0-3）。
// 由 PropEffectSpec.EffectTypes 中的 human_* 效果落地函数写入,最终落到
// werewolf.Player.HumanDebuff 供客户端视图渲染。
type HumanDebuffSpec struct {
	Type        string // human_announce_prefix / human_vote_suggest / human_char_garble
	SuggestSeat int    // 伪造推荐座位（-1=无）
	Duration    int    // 持续轮数
	PropNameZh  string // 道具中文名（渲染用）
}

// LastChallengeSpec §20260810-11 H2 — 公开质疑快照。
// 写时机:本 bot 是被质疑者时,由 buildAgentContextLocked 从 r.State.Players[seat] 读取填入。
// 渲染:ChallengeBlock 拼到 user prompt 末尾,告知「被 X 号质疑 Y」+ 鼓励 LLM 在下轮发言回应。
// §119:质疑内容本身已走活动流(公开),此处仅做 GameContext 镜像,无新增协议层。
type LastChallengeSpec struct {
	BySeat   int    // 质疑者座位(0-indexed;渲染时 +1)
	Question string // 质疑内容(≤60 字)
}

// AgentConsistencyCheck §20260811-06 U4 — 行为一致性校验结果镜像。
// 本结构是 agent.wwplayer.ConsistencyCheckResult 的 wwtypes 镜像,避免
// agent→wwtypes→agent 循环导入。由 buildAgentContextLocked 拷贝填入
// GameContext.LastConsistencyCheck;prompt.go::ConsistencyCheckBlock 渲染。
//
// §120 公平性:校验失败不计入 consecutiveFailures,仅作为 prompt 末尾
// 参考块,LLM 自由决定是否修正。
type AgentConsistencyCheck struct {
	Rule     string `json:"rule"`               // R1 / R2 / R3
	Severity string `json:"severity"`           // high / medium / low
	Detail   string `json:"detail"`             // 人类可读描述
}

// RumorJSON §20260811-06 U5 — 黎明流言系统镜像。
// 本结构是 werewolf.RumorEntry 的 wwtypes 镜像,避免 agent→werewolf 循环导入。
// 由 buildAgentContextLocked 拷贝到 GameContext.LastRumors;prompt.go::RumorBlock 渲染。
//
// §135:rumor 文本不揭示任何具体玩家身份,只描述"可能"行为。
type RumorJSON struct {
	Day      int    `json:"day"`
	Template string `json:"template"`
	Text     string `json:"text"`
	Truthful bool   `json:"truthful"`
}

// WolfPackMsg 是狼小队内部单条留言（v4 §13.1）。
// 本结构是 werewolf.WolfPackMsg 的 agent 包本地镜像（避免 agent→werewolf 循环导入）。

// ─── 2026-08-13 §20260813-01 U2 — GameContext 分层缓存 ───
//
// 背景:buildAgentContextLocked 每轮为每个 bot 重建完整 GameContext,
// 但其中大量字段整局不变(座位/角色/玩家列表)或阶段内不变(警长/屠边计数)。
// 13 人局 N bot × 50 轮 = 大量重建,每次 ~5-8KB 重复信息,一局浪费 ~2MB
// token。引入分层缓存后,静态层一局构建一次,阶段层阶段切换时重建,
// 动态层每轮重建——预期每轮减少 3-5KB token。
//
// 缓存由 room_agent.go 的 staticContextCache / phaseStateCache 管理,
// buildAgentContextLocked 通过 getStaticContext / getPhaseStateContext
// 获取,不修改 GameContext 现有字段(保持 wire 兼容)。

// StaticContext 整局不变的静态信息(一局构建一次,缓存到游戏结束)。
// §20260813-01 U2: 从 buildAgentContextLocked 的重复计算中提取。
type StaticContext struct {
	// SeatCount 本局人数(13/12/7)。
	SeatCount int `json:"seat_count"`
	// MySeat 本 bot 座位号(0-indexed)。
	MySeat int `json:"my_seat"`
	// Role 本 bot 身份。
	Role string `json:"role"`
	// Faction 本 bot 阵营("wolf"/"good")。
	Faction string `json:"faction"`
	// WinCondition 胜利条件描述。
	WinCondition string `json:"win_condition"`
	// AllPlayers 本局所有座位的玩家简报(顺序稳定 0..N-1)。
	// 仅含不变信息(ID/昵称/AgentName),不含存活状态(存活状态每轮变化)。
	AllPlayers []PlayerBrief `json:"all_players"`
	// GodRolePool 本局实际发牌的神职池(例 ["女巫","猎人","白痴"])。
	// 让 Agent 知道本局有哪些神职,避免引用已退役角色。
	GodRolePool []string `json:"god_role_pool"`
}

// PhaseStateContext 阶段内不变的信息(阶段切换时重建)。
// §20260813-01 U2: 从 buildAgentContextLocked 的重复计算中提取。
// 阶段内不变的字段:警长/警徽流/白痴翻牌/屠边计数/预言家发起投票状态。
type PhaseStateContext struct {
	// Phase 当前阶段。
	Phase string `json:"phase"`
	// SheriffSeat 当前警长座位(-1=无)。
	SheriffSeat int `json:"sheriff_seat"`
	// SheriffStream 第一/第二警徽流目标。
	SheriffStream [2]int `json:"sheriff_stream"`
	// SheriffCandidates 警长竞选参选座位(仅 PhaseSheriff 有意义)。
	SheriffCandidates []int `json:"sheriff_candidates"`
	// IdiotRevealedSeats 已翻牌白痴座位。
	IdiotRevealedSeats []int `json:"idiot_revealed_seats"`
	// DivineCnt 存活神职数。
	DivineCnt int `json:"divine_cnt"`
	// PlainCnt 存活平民数。
	PlainCnt int `json:"plain_cnt"`
	// WolfAliveCnt 存活狼人屠边参考。
	WolfAliveCnt int `json:"wolf_alive_cnt"`
	// VoteProposed 是否已由预言家发起投票。
	VoteProposed bool `json:"vote_proposed"`
	// VoteProposer 发起投票的座位号(-1=未发起)。
	VoteProposer int `json:"vote_proposer"`
}

// ─── 2026-08-26 §20260826-01 — 心理博弈增强镜像类型 ───
//
// 为避免 agent→werewolf 循环导入,以下结构是 werewolf.{RolePriorTable,
// ImpressionMemory, EmotionReasoningWeights} 的 agent 包本地镜像。
// 由 buildAgentContextLocked 从房间 store 拷贝填充。

// RolePriorSingleSnapshot 是 RolePriorSingle 的镜像（§20260826-01 U1）。
type RolePriorSingleSnapshot struct {
	TargetSeat   int     `json:"target_seat"`
	RoleGuess    string  `json:"role_guess"`
	PriorProb    float32 `json:"prior_prob"`
	EvidenceKind string  `json:"evidence_kind"`
	Note         string  `json:"note"`
	ComputedAt   int64   `json:"computed_at"`
}

// RolePriorSnapshot 是 RolePriorTable 的镜像（§20260826-01 U1）。
type RolePriorSnapshot struct {
	Seat       int                        `json:"seat"`
	Entries    []RolePriorSingleSnapshot  `json:"entries"`
	ComputedAt int64                      `json:"computed_at"`
}

// ImpressionDimsSnapshot 是 ImpressionDims 的镜像（§20260826-01 U2）。
type ImpressionDimsSnapshot struct {
	Trust       float32 `json:"trust"`
	Competence  float32 `json:"competence"`
	Sincerity   float32 `json:"sincerity"`
	Cooperation float32 `json:"cooperation"`
	Threat      float32 `json:"threat"`
}

// ImpressionEntrySnapshot 是 ImpressionEntry 的镜像（§20260826-01 U2）。
type ImpressionEntrySnapshot struct {
	TargetSeat   int                     `json:"target_seat"`
	Dims         ImpressionDimsSnapshot  `json:"dims"`
	LastUpdateMS int64                   `json:"last_update_ms"`
	EventCount   int                     `json:"event_count"`
	SampleEvents []string                `json:"sample_events"`
}

// ImpressionMemorySnapshot 是 ImpressionMemory 的镜像（§20260826-01 U2）。
type ImpressionMemorySnapshot struct {
	Seat      int                       `json:"seat"`
	Entries   []ImpressionEntrySnapshot `json:"entries"`
	UpdatedAt int64                     `json:"updated_at"`
}

// EmotionReasoningWeightsSnapshot 是 EmotionReasoningWeights 的镜像（§20260826-01 U4）。
type EmotionReasoningWeightsSnapshot struct {
	HypothesisConfidenceFloor int     `json:"hypothesis_confidence_floor"`
	HypothesisConfidenceCeil  int     `json:"hypothesis_confidence_ceil"`
	ThreatMultiplier          float32 `json:"threat_multiplier"`
	TrustMultiplier           float32 `json:"trust_multiplier"`
	StabilityBias             float32 `json:"stability_bias"`
	SampleEvent               string  `json:"sample_event"`
}
