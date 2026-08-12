package werewolf

// view.go — 把权威 GameState 投影成「某个座位可见」的客户端视图。
//
// 隐藏信息规则:
//   - 座位 viewer == -1 → 观察者;不暴露任何玩家角色与查验结果
//   - 玩家只看见:
//       - 自己:自己的角色身份、上一晚查验目标、用药历史
//       - 所有玩家:是否存活、警长标记、是否死过
//   - 夜晚:狼人看其他狼人(用于协商);预言家/女巫/猎人/平民不看其他玩家身份
//   - 白天:死亡玩家强制公开其角色(便于推理)
//   - 警察长:不暴露角色类别(同好人规则)

import (
	"fmt"
	"sort"
	"time"

	"LsmWebGame/agent/wwjudge"
	"LsmWebGame/agent/wwplayer"
	"LsmWebGame/agent/wwtypes"
	"LsmWebGame/config"
)

// PlayerJSON 单个座位对客户端可见的信息。
type PlayerJSON struct {
	UserID    string `json:"user_id"`
	Seat      int    `json:"seat"`
	Alive     bool   `json:"alive"`
	IsSheriff bool   `json:"is_sheriff"`
	// 是否能查看本座位的"角色身份"。
	// 仅当座位死过或(座位==自己 && 角色已分配)时为 true。
	RoleRevealed bool   `json:"role_revealed"`
	Role         string `json:"role,omitempty"` // 仅 RoleRevealed=true 时填充
	Faction      string `json:"faction,omitempty"`
	AgentName    string `json:"agent_name,omitempty"` // bot 专属:LLM 模型展示名

	// 2026-08-05 §02 — 座位级「最后一次公开发言」,人机统一。
	// 数据源 room.lastSpeechBySeat,由 appendRoomMessage 在**公开发言**落库时
	// 写入,因此 bot 与真人走同一条路径,无需分别接线 —— 对照 bot_contexts:
	// 它由 populateBotContexts 按 seatModelKeys ∪ BotAgents 构造,**只含 bot
	// 座位**,真人玩家永远拿不到气泡。
	// 私聊(whisper)**不写入** —— 私聊原文只对收发双方可见,而本字段随
	// players[] 全房下发,任何写入都是公开面泄露。
	LastSpeech   string `json:"last_speech,omitempty"`    // ≤200 rune
	LastSpeechAt int64  `json:"last_speech_at,omitempty"` // unix ms

	// 2026-08-07 §20260807-04 P0-3 — 人类反制道具 debuff(仅真人座位可见)。
	// 由 Player.HumanDebuff 透传,前端 GameChatPanel / VotePanel 读取渲染。
	HumanDebuff *wwtypes.HumanDebuffSpec `json:"human_debuff,omitempty"`
}

// ClientGameState 发送给客户端的对局视图。
type ClientGameState struct {
	RoomID   string                 `json:"room_id"`
	GameKind string                 `json:"game_kind"`
	MaxSeat  int                    `json:"max_seat"`
	Seats    [MaxPlayers]string     `json:"seats"`
	Players  [MaxPlayers]PlayerJSON `json:"players"`

	MySeat    int    `json:"my_seat"`              // -1 = 观察者
	MyRole    string `json:"my_role,omitempty"`    // 仅自己可见
	MyFaction string `json:"my_faction,omitempty"` // "wolf" | "good"

	// 2026-08-11 BUG-ROLE-MISMATCH-P0 — 「自选角色未生效」仅对本人下发。
	// 玩家创建房间时选了角色(creator_role / agent_seats[].role),但本局
	// 随机牌组未抽到该角色(13 人局 2~3 张神职,骑士/守卫/猎魔人常缺席)
	// 或与其他座位偏好冲突时,其偏好降级为随机。此前该信息只记 Warn 日志,
	// 玩家只能实测发现身份不符;现由 ApplyPreferredRoles unmet 直接下发:
	//   - 本人座位在 unmet 列表 → MyPreferredRole=想要的角色名, MyRolePrefUnmet=true
	//   - 本人座位已满足或无偏好 → 两字段 omitempty 不下发
	// 其余玩家/观战者**永远**收不到这两个字段(身份保密,§135)。
	MyPreferredRole  string `json:"my_preferred_role,omitempty"`
	MyRolePrefUnmet  bool   `json:"my_role_pref_unmet,omitempty"`

	Phase     string `json:"phase"`
	Day       int    `json:"day"`
	Status    string `json:"status"`
	Winner    string `json:"winner,omitempty"`

	// 夜晚操作相关(只有"被轮到"的玩家能看到)
	TurnActingSeat int  `json:"turn_acting_seat"`
	MyTurn         bool `json:"my_turn"`

	// 白天相关
	SpeakTurnSeat int   `json:"speak_turn_seat"`
	MySpeakTurn   bool  `json:"my_speak_turn"`
	SpeakOrder    []int `json:"speak_order,omitempty"`

	// 投票与死亡
	DayEliminated   int   `json:"day_eliminated"`    // 白天被票死的人 seat;-1=无
	LastNightDeaths []int `json:"last_night_deaths"` // 昨晚死的座位列表
	// 2026-07-10 §123: 完整死亡信息(含死因 + 处决/死亡决断);供前端按 verdict 分色徽章。
	LastNightDeathsVerbose []DeadPlayerJSON `json:"last_night_deaths_verbose,omitempty"`
	// 2026-07-11 R96-P1: 全部已死亡玩家详细列表(含 verdict / cause / day);
	// 不依赖 LastNightDeaths(每晚重置),始终包含 day1..current 全部死亡记录。
	// 前端 WerewolfTable 据此为所有 dead seat 渲染 §123 处决 / 死亡 verdict 徽章,
	// 而不仅是上一晚的新死者。
	AllDeadListVerbose []DeadPlayerJSON `json:"all_dead_list_verbose,omitempty"`
	SuicidedWolfSeat   int              `json:"suicided_wolf_seat"` // 自爆狼 seat;-1=无
	TiedPlayers        []int            `json:"tied_players,omitempty"`

	// 2026-07-11: 预言家发起投票状态(全员可见)
	VoteProposed bool `json:"vote_proposed,omitempty"` // 是否已由预言家发起投票
	VoteProposer int  `json:"vote_proposer,omitempty"` // 发起投票的座位号

	// 神职专属(仅自己 & 自己 = 该角色)
	SeerLastCheck     int  `json:"seer_last_check,omitempty"`     // 仅预言家
	WitchAntidoteUsed bool `json:"witch_antidote_used,omitempty"` // 仅女巫
	WitchPoisonUsed   bool `json:"witch_poison_used,omitempty"`   // 仅女巫
	WitchWolfTarget   int  `json:"witch_wolf_target,omitempty"`   // 女巫可见当晚狼刀(仅女巫)

	// §134 守卫专属(仅守卫本人可见;盲守语义,绝不可见 WolfKillTarget)
	GuardLastProtect   int `json:"guard_last_protect,omitempty"`   // §134 上晚守护目标(-1=无)
	GuardProtectTarget int `json:"guard_protect_target,omitempty"` // §134 今晚已守目标(-1=未守/空守)

	// 猎人
	HunterPending bool `json:"hunter_pending,omitempty"`

	// 狼人投票视图(2026-07-17): 仅狼人玩家在 night_wolves 阶段可见,携带投票快照 + 计票结果。
	WolfVoteView *WolfPeerView `json:"wolf_vote_view,omitempty"`

	// 警长
	SheriffSeat int `json:"sheriff_seat"` // -1 = 无

	// 警徽流(2026-07-10 新增):第一/第二警徽流目标(全场可见);-1 = 未声明。
	SheriffStreams [2]int `json:"sheriff_streams"`
	// SheriffSuccessor 警长死亡后的警徽继承者(结算后可见);-1 = 撕警徽。
	SheriffSuccessor int `json:"sheriff_successor"` // -1 = 撕 / 无

	// §20260811-10 U5 — 警徽流保鲜期。每个条目附带 age_rounds 与 is_stale,
	// 前端 HistoryDrawer 据此渲染灰蒙版 + ⌛ icon。
	SheriffStreamAges []SheriffStreamAgeJSON `json:"sheriff_stream_ages,omitempty"`

	// §20260811-10 U3 — 狼队阵营金币池余额(全员可见,负数 = 已透支)。
	// 默认 0;omitempty 避免零值污染历史回放。前端 HistoryDrawer 加「🐺 狼池」stat。
	WolfPoolBalance int64 `json:"wolf_pool_balance,omitempty"`

	// SheriffCandidates 警长竞选已参选座位列表(全场可见)。
	//
	// §报告-20260804-03 BUG-04: 参选状态此前只写在 Player.HasSpoken 上,而该字段
	// 没有任何 json tag —— 玩家点「参选警长」后端成功返回 nil,但广播出去的
	// game.state 与点击前逐字节相同,UI 零变化,体感等同「按钮坏了」。
	// 仅 Phase==PhaseSheriff 时填充(其它阶段 HasSpoken 语义是「白天已发言」,
	// 下发会泄漏发言状态)。
	SheriffCandidates []int `json:"sheriff_candidates,omitempty"`

	// MyVoted / MyVoteTarget 当前 viewer 的投票状态(仅入座玩家可见)。
	//
	// §报告-20260804-03 BUG-05: Player.Voted / VoteTarget 同样没有 json tag,
	// 玩家投完警长票后 UI 无法区分「投票成功」与「点击无效」。
	// Votes(聚合票数)是全场视角,回答不了「我投过没 / 投给了谁」。
	MyVoted      bool `json:"my_voted"`
	MyVoteTarget int  `json:"my_vote_target"` // -1 = 未投 / 弃票

	// 白痴翻牌(全场公开):已翻牌的白痴座位列表。
	IdiotRevealedSeats []int `json:"idiot_revealed_seats,omitempty"`

	// 屠边计数(全员可见,便于推理)
	WolfAliveCnt int `json:"wolf_alive"`   // 存活狼人数
	DivineCnt    int `json:"divine_alive"` // 存活神职数(预+女巫+猎+白痴)
	PlainCnt     int `json:"plain_alive"`  // 存活平民数

	// 投票计数(全员可见)
	Votes map[string]int `json:"votes,omitempty"` // seat(int key as string) -> count

	// BotContexts 是每个 bot 座位最近的决策快照(thinking / 工具调用 / 摘要),
	// 供观战者 AgentThoughtPanel 渲染。由 WerewolfRoom.populateBotContexts 在
	// BuildClientState 之后填充,且仅对观察者(viewer==-1)填充——避免把狼人 bot
	// 的推理泄漏给人类对手,破坏隐藏信息不变量。
	// BUG-WEREWOLF-P0-NEW-2: 此前服务端从未下发该字段,前端思考面板永远空状态。
	// 字段对齐 ClientWeb/src/types/werewolf.ts BotContextJSON。
	BotContexts []wwplayer.BotTranscript `json:"bot_contexts,omitempty"`

	// 2026-08-10 §20260810-07 — 多假说并行推演(§135 spectator 隔离)。
	// 仅 spectator(viewer==-1)下发;玩家(viewer>=0)omitempty。
	// 前端 HistoryDrawer 第 5 sub-tab「🔮 假说」渲染折线图;
	// 字段对齐 ClientWeb/src/types/werewolf.ts BotHypothesisJSON。
	BotHypotheses []BotHypothesisJSON `json:"bot_hypotheses,omitempty"`

	// 2026-07-10 §123 增强 — Agent 法官上下文。挂在 game.state.judge_context;
	// 前端 JudgePanel.tsx 渲染法官模型名 + 最近宣告 + quarantine 徽章。
	// JudgeEnabled=false 时不渲染法官面板(走 host driver 旧路径)。
	JudgeEnabled bool `json:"judge_enabled,omitempty"`
	// JudgeContext 法官详细摘要(模型/最近宣告/工具调用/quarantine);
	// 与 BotContexts 区分:BotContexts 是玩家 bot,这是法官 bot。
	JudgeContext *wwjudge.JudgeTranscript `json:"judge_context,omitempty"`
	// JudgePendingAnnounce 下一次应唤醒法官的事件类型;空 = 无。
	// 取值同 GameState.JudgePendingAnnounce。
	JudgePendingAnnounce string `json:"judge_pending_announce,omitempty"`
	// 2026-07-10 §125 增强 — 法官最近一次整局总结(挂在 judge_context.last_summary)。
	JudgeSummary string `json:"judge_summary,omitempty"`
	// 2026-07-10 §125 增强 — 按模型 key 的"上一局记忆"切片(用于前端调试面板)。
	JudgeModelMemories map[string][]string `json:"judge_model_memories,omitempty"`
	// JudgeSpeakOrder 法官生成"按顺序发言"用的座位列表;空 = 无。
	JudgeSpeakOrder []int `json:"judge_speak_order,omitempty"`

	// §20260811-07 U2 — 自动高光集锦战报。
	// BattleReportHighlights 是终局时的 3~5 张高光卡片数据,供 SettlementModal
	// 顶部 BattleReportHighlights 组件渲染(omitempt:终局前不暴露)。
	BattleReportHighlights []HighlightMoment `json:"battle_report_highlights,omitempty"`

	// 2026-07-18 §UX-运行时: 整局开始 Unix 秒。0 表示尚未开局(filling 阶段)。
	// 前端 <RoomRunningClock> 组件据此渲染"已运行 X 分 Y 秒"。
	GameStartedAt int64 `json:"game_started_at,omitempty"`

	// 2026-08-10 §20260810-12 D2 — 死者身份「终局延时揭晓」配置(可选 0/5/15)。
	// 由房间创建时按 RoomConfig.DeathRevealDelayMin 下发,前端 SettlementModal 据此倒计时。
	// 仅影响前端 UI 显示时机;RolePubliclyRevealed 单点判定(§135)不受影响。
	// 0 = 立即揭晓(零回归,与旧行为完全一致);5 / 15 = 倒计时后揭晓。
	DeathRevealDelayMin int `json:"death_reveal_delay_min,omitempty"`
	// §20260811-09 U2 — Agent 难度档位(easy/normal/hard/hell),全员可见,
	// 前端 Room 信息面板 + 座位徽章渲染。omitempty 保证 normal(默认值)
	// 不污染老客户端。
	AgentDifficulty string `json:"agent_difficulty,omitempty"`
	// §20260811-09 U1 — AI 实时解说 feed(仅 viewer<0 渲染,见 §U1.2.4)。
	// 全部观众订阅 roomID 频道时一次性下发最近 20 条;后续增量走 WS 帧
	// chat.commentary(seq 单调递增,前端按 seq 去重)。
	CommentaryFeed []CommentaryLineJSON `json:"commentary_feed,omitempty"`

	// BUG Round 40 §95: 首夜强制发言阶段视图。仅在 Phase == PhasePreWolves 时填充;
	// 其余阶段为 nil(前端可安全用 `?.` 访问,GameState 字段已与 view.go 同步)。
	PhaseExtra *PhaseExtraJSON `json:"phase_extra,omitempty"`

	// §20260810-09 — 上帝视角观战快照。**仅** spectator(viewer<0)下发;
	// 玩家侧与 REST 房间视图永远 omitempty(§135 公平性 + §121 数据形状契约)。
	// 服务端**不**做开关(由前端 localStorage.ww_god_mode 控制是否渲染),
	// 见 docs/狼人杀-Agent与系统/狼人杀13人局Agent升级-20260810-09.md §2.1.2。
	GodMode *GodModeSnapshot `json:"god_mode,omitempty"`

	// §20260812-03 U1 — 阵营胜率热力图概率数组。**仅** spectator(viewer<0)下发;
	// 玩家侧与 REST 房间视图永远 omitempty(§132 隐私隔离 + §135 不含身份明文)。
	// 长度恒为 13,下标 0..12 对应 1..13 号位;值为该座位"是狼人"的启发式概率
	// (0.0~1.0),数据源: r.State.Players[] + r.voteTally + r.recentSpeeches。
	WinRateProbability []float64 `json:"win_rate_probability,omitempty"`

	// §20260810-09 — 警长定序状态(全场可见,非上帝视角专属)。
	SheriffOrderSet       bool   `json:"sheriff_order_set,omitempty"`
	SheriffSpeakDirection string `json:"sheriff_speak_direction,omitempty"`
	SheriffSpeakSelfPos   string `json:"sheriff_speak_self_pos,omitempty"`

	// 房间元信息
	Ready  bool `json:"ready"`
	Filled bool `json:"filled"` // 7 人已满

	// 2026-07-24 优化:UI 暂停标志。true 时房间处于"暂停"状态 —
	//   - bot 不再调用 LLM(避免被批量 quarantine)
	//   - 阶段时钟暂停
	//   - watchdog 暂停跳过
	// 仅房主/admin 可设置。前端 GameInfoPanel 提供 ⏸ 暂停 / ▶ 恢复 按钮。
	// 持久化在房间内存(r.paused),不写 DB;重启后默认继续。
	Paused bool `json:"paused,omitempty"`
	// PausedBy 谁暂停的(房主 userID);前端 UI 据此渲染归属。
	PausedBy string `json:"paused_by,omitempty"`
	// PausedReason 暂停原因(可选,如"等待 LLM 服务恢复")。
	PausedReason string `json:"paused_reason,omitempty"`

	// 2026-07-30 §统计增强 — 房间级聚合 Agent + 法官 API/Token 统计。
	// 纯内存态，不进 DB，房间解散自动释放。由 BuildClientStateWithRoom 调用
	// WerewolfRoom.AggregateAgentStats() 填充。前端 WerewolfStatusBar 据此渲染。
	AgentStats *AgentRoomStats `json:"agent_stats,omitempty"`

	// 2026-08-10 §20260810-05 — 信息账本(Information Ledger)观战者快照。
	// 仅 spectator 视图(viewer==-1)填充;玩家视图与 REST 房间视图不下发。
	// Fact 已在写入侧经 redactLedgerFact 剔除身份明文(§119/§135);
	// 观战者享有上帝视角数据(HeartThought/WolfPack 判例已在 HistoryDrawer 存在)。
	// 一期「只写不读」:本字段为二期前端「信息传播时序图」提供数据通道。
	InfoLedger []InfoEntryJSON `json:"info_ledger,omitempty"`

	// 2026-08-10 §20260810-08 — 疑似说漏嘴记录，仅 spectator 下发。
	// 玩家与 bot 永远拿不到该审计线索，避免污染博弈（§135）。
	InfoLeaks []InfoLeak `json:"info_leaks,omitempty"`

	// 2026-08-10 §20260810-06 — 行为承诺列表。
	// 按视角脱敏：viewer==-1(观战者)可见全部真实状态；
	// 玩家仅可见自己的承诺(含真实状态) + 他人的 pending 承诺。
	Commitments []CommitmentJSON `json:"commitments,omitempty"`

	// §20260811-01 U3 — 投票阶段「半公开计票」悬念配置。
	// 前端据此在投票结束后延迟 VoteSuspenseDelayMs 毫秒再显示完整票型。
	VoteSuspense        bool `json:"vote_suspense,omitempty"`
	VoteSuspenseDelayMs int  `json:"vote_suspense_delay_ms,omitempty"`

	// §20260811-02 U1 — 发言影响力生态。全场每座位 0~100 的公开影响力分数。
	// **全员可见**(与 BotHypotheses / GodMode / InfoLedger 的 spectator-only 相反):
	// 影响力完全由公开信息计算(票型/发言/被指向),不含任何角色信息(§135),
	// 因此对玩家开放不构成信息泄露 —— 反而是本机制「社交资本」语义的前提。
	// 前端 WerewolfTable SeatCell 渲染 ⭐/◉/○ 徽章 + 分项 tooltip。
	InfluenceScores []InfluenceScore `json:"influence_scores,omitempty"`
}

// AgentRoomStats 是本局所有 Agent + 法官的聚合 API/Token 统计。
// 2026-07-30 §统计增强。纯内存态，不进 DB，房间解散自动释放。
// 对齐 WerewolfRoom.AggregateAgentStats() 产出。
type AgentRoomStats struct {
	TotalInputTokens  int  `json:"total_input_tokens"`
	TotalOutputTokens int  `json:"total_output_tokens"`
	TotalAPITokens    int  `json:"total_api_tokens"`
	APICallCount      int  `json:"api_call_count"`
	APISuccessCount   int  `json:"api_success_count"`
	APIFailCount      int  `json:"api_fail_count"`
	AgentCount        int  `json:"agent_count"`
	JudgeEnabled      bool `json:"judge_enabled"`
	JudgeTotalInputTokens  int `json:"judge_total_input_tokens"`
	JudgeTotalOutputTokens int `json:"judge_total_output_tokens"`
	JudgeTotalAPITokens    int `json:"judge_total_api_tokens"`
	JudgeAPICallCount      int `json:"judge_api_call_count"`
	JudgeAPISuccessCount   int `json:"judge_api_success_count"`
	JudgeAPIFailCount      int `json:"judge_api_fail_count"`
}

// PhaseExtraJSON 阶段专属扩展信息(Round 40 §95)。
//
// 当前仅 PhasePreWolves(首夜强制发言)使用 RoundsTotal/RoundsCurrent/SpeakCountPerSeat;
// 2026-07-09 §13 增强新增 PhaseDeadlineAt/RemainingSec 适用于所有阶段(给前端
// <PhaseClock> 组件使用)。
// §20260811-09 U1 — 观战者侧 AI 解说单条载荷(由 werewolf.commentaryLine
// 投影为 JSON)。前端 store 接收后追加到 commentaryFeed 切片。
type CommentaryLineJSON struct {
	Seq      uint64 `json:"seq"`
	Text     string `json:"text"`
	Style    string `json:"style"`
	ModelKey string `json:"model_key,omitempty"`
	Kind     string `json:"kind"`
	TsMs     int64  `json:"ts_ms"`
}

// BUG 2026-07-09: DeathLyric / DeadList 字段在 PhaseDeathLyric 阶段由
// BuildClientState 填充,供前端观察遗言进度。
type PhaseExtraJSON struct {
	// RoundsTotal 总轮数(每名玩家至少发言次数,默认 1)。
	RoundsTotal int `json:"rounds_total"`
	// RoundsCurrent 当前轮 (0-based)。
	RoundsCurrent int `json:"rounds_current"`
	// SpeakCountPerSeat 每座位已发言次数(全 7 座位,公开信息)。
	SpeakCountPerSeat [MaxPlayers]int `json:"speak_count_per_seat"`
	// GraceRemainingSec 缓冲期剩余秒数(< 0 表示已超时)。首夜专用。
	GraceRemainingSec int `json:"grace_remaining_sec"`
	// DeadlineAt 缓冲期截止时间(RFC3339,前端可用来做进度条)。首夜专用。
	DeadlineAt string `json:"deadline_at,omitempty"`

	// 2026-07-09 §13 增强 — 时钟机制。所有阶段都下发这两个字段(零值除外);
	// 前端 PhaseClock 组件根据 PhaseDeadlineAt 本地 setInterval 倒数。
	//
	// PhaseDeadlineAt 阶段截止时间(RFC3339);所有阶段都填充。
	PhaseDeadlineAt string `json:"phase_deadline_at,omitempty"`
	// RemainingSec 阶段剩余秒数(> 0 = 未到;= 0 = 到点;< 0 = 逾期);每帧由
	// 服务端根据 PhaseDeadlineAt - now 计算;前端本地 setInterval 会再次计算。
	RemainingSec int `json:"remaining_sec,omitempty"`

	// BUG 2026-07-09: 遗言阶段进度。phase == "death_lyric" 时填充。
	DeathLyric *DeathLyricExtra `json:"death_lyric,omitempty"`
	// BUG 2026-07-09: 已死亡玩家列表(含遗言状态)。phase == "death_lyric" 时填充,
	// 其他阶段为 nil。全玩家 + 观战者可见(公开信息)。
	DeadList []DeadPlayerJSON `json:"dead_list,omitempty"`
	// 2026-07-10: 重开局投票扩展。phase == "restart_vote" 时填充,
	// 其他阶段为 nil。前端 WerewolfRestartVotePanel 据此渲染投票按钮。
	RestartVote *RestartVoteExtra `json:"restart_vote,omitempty"`

	// 2026-07-21 §人类玩家操作重构 — 「轮到我了」专属标记 + 倒计时。
	// 仅在 hasHumanPlayer(gs) == true 且 viewer 入座时填充;否则为 false/0。
	// MyTurnNow=true 时前端 <MyTurnIndicator> 组件渲染红黄倒计时 + 操作引导。
	// 全 AI 房间永远 false(不影响 bot 行为)。
	MyTurnNow bool `json:"my_turn_now,omitempty"`
	// MyTurnRemainingSec 轮到我的剩余秒数(phase_deadline_at - now)。
	// 仅当 MyTurnNow=true 时填充。前端用此渲染倒计时,无需自己计算。
	MyTurnRemainingSec int `json:"my_turn_remaining_sec,omitempty"`
}

// DeathLyricExtra 遗言阶段的扩展信息。
type DeathLyricExtra struct {
	// CurrentSeat 当前遗言座位(0-indexed)。
	CurrentSeat int `json:"current_seat"`
	// Total 本轮遗言总人数。
	Total int `json:"total"`
	// Done 已经完成遗言/跳过的人数。
	Done int `json:"done"`
}

// DeadPlayerJSON 已死亡玩家的遗言状态(公开信息)。
type DeadPlayerJSON struct {
	// Seat 座位(0-indexed)。
	Seat int `json:"seat"`
	// Account 昵称。
	Account string `json:"account"`
	// Role 角色中文名。
	Role string `json:"role"`
	// LastWordsStatus 遗言状态 spoken / skipped / pending / ineligible。
	LastWordsStatus string `json:"last_words_status"`
	// Cause 死因 wolf / vote / hunter / witch_poison / suicide。
	Cause string `json:"cause"`
	// 2026-07-10 §123: 处决 / 死亡 决断。execution = 处决(vote/suicide);
	// death = 死亡(wolf/hunter/witch_poison)。空字符串 = 未死亡。
	Verdict string `json:"verdict,omitempty"`
	// Day 第几天出局。
	Day int `json:"day"`
}

// SheriffStreamAgeJSON 是 §20260811-10 U5 警徽流保鲜期的单条视图。
// 长度 == 2(slot 0 / slot 1),每个元素描述该警徽流声明距离现在的轮差与
// 是否已过期(超过 cfgWerewolfSheriffPersistRounds 即 IsStale=true)。
//
// 前端使用:
//   - HistoryDrawer 警徽流 sub-tab:IsStale 时灰蒙版 + ⌛ icon + Hover 提示
//   - WerewolfTable 即时显示:stale badge「⌛ 过期」
//
// AgeRounds 为当前轮次与声明轮次的差(current_round - declared_round);
// 当警徽流未被声明(slot 值为 NoSeat)时,该 slot 的 AgeRounds=-1,IsStale=false。
type SheriffStreamAgeJSON struct {
	// Slot 槽位索引(0 / 1),与 SheriffStreams 数组对齐。
	Slot int `json:"slot"`
	// Target 声明的警徽流目标座位(0-indexed);-1 = 未声明。
	Target int `json:"target"`
	// DeclaredRound 警徽流声明时的轮次(DayNumber);0 = 未声明。
	DeclaredRound int `json:"declared_round"`
	// AgeRounds 距声明的轮差(current_round - declared_round);未声明时为 -1。
	AgeRounds int `json:"age_rounds"`
	// IsStale 是否已过期(age_rounds > persist_rounds 阈值)。
	IsStale bool `json:"is_stale"`
}

// RestartVoteExtra 是 2026-07-10 新增的"重开局投票"扩展视图。
// 与 PhaseExtraJSON 同级挂在 game.state.phase_extra.restart_vote;
// 前端 WerewolfRestartVotePanel 负责渲染(投票按钮 + 倒计时 + 结果 banner)。
type RestartVoteExtra struct {
	// DeadlineAt 截止时间(RFC3339,与 phase_extra.phase_deadline_at 冗余但
	// 便于单 panel 渲染)。
	DeadlineAt string `json:"deadline_at,omitempty"`
	// RemainingSec 剩余秒数;前端也可从顶层 phase_extra.remaining_sec 取。
	RemainingSec int `json:"remaining_sec"`
	// Yes/No/Abstain 当前已投票的 seat 列表(座位号,不含 user_id)。
	Yes     []int `json:"yes"`
	No      []int `json:"no"`
	Abstain []int `json:"abstain"`
	// Decided 是否已结算 (true → result 字段填充)。
	Decided bool `json:"decided"`
	// Result 结算结果:"passed" | "rejected" | "timeout",仅 decided=true 时有效。
	Result string `json:"result,omitempty"`
	// Winner 上一局的胜方;"wolf" | "good" — 与 phase_extra 一致,放在此处便于
	// 前端在 layout 层直接组装。
	Winner string `json:"winner,omitempty"`
	// EligibleCount 当前可投票人数(原始入座座位数)。
	EligibleCount int `json:"eligible_count"`
	// YesQuota 通过所需的 yes 票阈值(ceil(N*num/den)+1)。
	YesQuota int `json:"yes_quota"`
	// MyChoice 当前 viewer 的投票选择;"yes" | "no" | "abstain" | ""。
	// 仅当 viewer >= 0 且已投票时填充。
	MyChoice string `json:"my_choice,omitempty"`
	// FastRestart 为 true 时表示「即刻原班重开」模式,通过阈值降至简单多数。
	FastRestart bool `json:"fast_restart,omitempty"`
}

// BuildClientState 构造座位 viewer 的可见视图。
//
// 当 viewer == -1 时表示"观察者":所有玩家的 Role / RoleRevealed 都隐藏。
// 警长 / 胜负 / 公开死亡信息仍照常填充。
func BuildClientState(roomID string, seats [MaxPlayers]string, viewer int, gs *GameState) *ClientGameState {
	cs := &ClientGameState{
		RoomID:           roomID,
		GameKind:         "werewolf",
		MaxSeat:          MaxPlayers,
		Seats:            seats,
		MySeat:           viewer,
		Status:           "playing",
		DayEliminated:    -1,
		SuicidedWolfSeat: -1,
		VoteProposer:     -1,
		// §134 守卫字段必须显式默认 -1。Go 零值是 0,而 0 是**合法座位号**(1号位),
		// 若不显式初始化,非守卫视角会拿到 guard_last_protect=0 → 前端把 1 号位
		// 渲染成"昨晚已守",等于凭空泄露一个假的护盾位置。同理守卫本人在首夜
		// (尚无上晚记录)也会被误判为"1号位不可守"。
		GuardLastProtect:   -1,
		GuardProtectTarget: -1,
		SheriffSeat:        -1,
		SheriffSuccessor:   -1,
		// §报告-20260804-03 BUG-05: 与 GuardLastProtect 同理 —— Go 零值 0 是
		// **合法座位号**(1号位),不显式初始化会让未投票的玩家看到「我投了1号」。
		MyVoteTarget:       -1,
		TurnActingSeat:     -1,
		SpeakTurnSeat:      -1,
		LastNightDeaths:    []int{},
		SpeakOrder:         []int{},
		TiedPlayers:        []int{},
		IdiotRevealedSeats: []int{},
		WolfAliveCnt:       0,
		DivineCnt:          0,
		PlainCnt:           0,
		Ready:              false,
		Filled:             false,
	}
	if gs == nil {
		return cs
	}

	cs.Phase = gs.Phase.String()
	cs.Day = gs.DayNumber
	cs.Status = gs.Status
	cs.Winner = gs.Winner
	cs.SheriffSeat = int(gs.SheriffSeat)
	if gs.SheriffSeat != NoSeat {
		cs.SheriffStreams = [2]int{int(gs.SheriffStreams[0]), int(gs.SheriffStreams[1])}
	} else {
		cs.SheriffStreams = [2]int{-1, -1}
	}
	cs.SheriffSuccessor = int(gs.SheriffSuccessor)
	// §20260811-10 U5 — 警徽流保鲜期计算。SheriffStreamRounds[slot]=0 表示未声明,
	// AgeRounds=-1;已声明时 AgeRounds = current_round - declared_round。
	cs.SheriffStreamAges = buildSheriffStreamAgesLocked(cs.Day, gs.SheriffStreams, gs.SheriffStreamRounds)
	// §20260811-10 U3 — 阵营金币池下发给客户端,全员可见。omitempty 让零值不污染 JSON。
	cs.WolfPoolBalance = gs.WolfPoolBalance
	cs.DayEliminated = int(gs.DayEliminated)
	cs.WolfAliveCnt = gs.WolfAliveCnt
	cs.DivineCnt = gs.DivineCnt
	cs.PlainCnt = gs.PlainCnt
	if idSeats := gs.idiotRevealedSeats(); len(idSeats) > 0 {
		cs.IdiotRevealedSeats = idSeats
	} else {
		cs.IdiotRevealedSeats = []int{}
	}
	cs.SuicidedWolfSeat = int(gs.SuicidedWolfSeat)
	cs.Filled = gs.IsReady()
	// Ready = true once the game has started (Phase != filling), regardless of
	// seat occupancy. A player disconnecting mid-game must not flip Ready to false
	// and trigger the filling overlay on spectators.
	cs.Ready = gs.IsReady() || gs.Phase != PhaseFilling

	cs.LastNightDeaths = make([]int, 0, len(gs.LastNightDeaths))
	for _, s := range gs.LastNightDeaths {
		cs.LastNightDeaths = append(cs.LastNightDeaths, int(s))
	}
	// 2026-07-10 §123: 完整死亡信息(含 verdict/cause),供前端按 verdict 分色徽章。
	// 仅在 LastNightDeaths 非空且不在 filling 阶段时填充。
	if len(gs.LastNightDeaths) > 0 {
		cs.LastNightDeathsVerbose = buildDeadListForSeatsLocked(gs, gs.LastNightDeaths)
	}
	// 2026-07-11 R96-P1: 全部历史死亡列表(phase-agnostic,不含遗言状态信息),
	// 让 §123 verdict 徽章在 day2+ 也能为之前几夜死亡玩家正确渲染。
	// 复用了与 LastNightDeathsVerbose 一致的 DeadPlayerJSON 结构,但语意只关心 verdict/cause。
	cs.AllDeadListVerbose = buildAllDeadListLocked(gs)
	if len(gs.DayTiedPlayers) > 0 {
		cs.TiedPlayers = make([]int, 0, len(gs.DayTiedPlayers))
		for _, s := range gs.DayTiedPlayers {
			cs.TiedPlayers = append(cs.TiedPlayers, int(s))
		}
	}

	// 2026-07-11: 预言家发起投票状态
	cs.VoteProposed = gs.VoteProposed
	cs.VoteProposer = int(gs.VoteProposer)

	if len(gs.SpeakOrder) > 0 {
		cs.SpeakOrder = make([]int, 0, len(gs.SpeakOrder))
		for _, s := range gs.SpeakOrder {
			cs.SpeakOrder = append(cs.SpeakOrder, int(s))
		}
	}

	if gs.TurnActingSeat != NoSeat {
		cs.TurnActingSeat = int(gs.TurnActingSeat)
	}
	if gs.SpeakTurnSeat != NoSeat {
		cs.SpeakTurnSeat = int(gs.SpeakTurnSeat)
	}

	// 用户列表:每个座位 + 是否为警长 / 是否死亡。
	isSpectator := viewer < 0 || viewer >= MaxPlayers
	for i := 0; i < MaxPlayers; i++ {
		uid := seats[i]
		p := &gs.Players[i]
		pj := PlayerJSON{
			UserID:    uid,
			Seat:      i,
			Alive:     p.Alive && uid != "",
			IsSheriff: Seat(i) == gs.SheriffSeat,
		}
		// 是否让该玩家在本座位看到角色?
		// §135: 复述段落已压缩 — git blame 与 docs/ 索引可还原

		if uid != "" && gs.RolePubliclyRevealed(Seat(i)) {
			pj.RoleRevealed = true
		} else if !isSpectator && i == viewer {
			pj.RoleRevealed = true
		}
		// 注:狼互看:在 BuildWolfPeerView 里另外暴露,这里不展开(避免
		// 在全员视角下意外泄漏)。
		if pj.RoleRevealed {
			pj.Role = gs.Roles[i].String()
			pj.Faction = FactionOf(gs.Roles[i]).String()
		}
		// 即使没露身份,但玩家死了,ALIVE=false,客户端可显示"已出局"
		// 2026-08-07 §20260807-04 P0-3:人类反制道具 debuff 透传(仅真人座位)。
		if p.HumanDebuff != nil {
			spec := *p.HumanDebuff
			pj.HumanDebuff = &spec
		}
		cs.Players[i] = pj
	}

	// 自己视角专属字段
	if !isSpectator && viewer >= 0 && viewer < MaxPlayers {
		myRole := gs.Roles[viewer]
		cs.MyRole = myRole.String()
		cs.MyFaction = FactionOf(myRole).String()
		// 2026-08-11 BUG-ROLE-MISMATCH-P0:自选角色未满足时向本人显式下发
		// 「你想要的角色名 + 未生效标记」。仅本人可见(身份保密 §135);
		// PreferredRoles 本身不下发,只暴露「我这一座」的偏好与偏差。
		if want, ok := gs.PreferredRoles[viewer]; ok && want > RoleUnknown {
			for _, s := range gs.PreferredRolesUnmet {
				if s == viewer {
					cs.MyPreferredRole = want.String()
					cs.MyRolePrefUnmet = true
					break
				}
			}
		}
		// 夜晚"我的回合"
		cs.MyTurn = gs.TurnActingSeat == Seat(viewer)
		// 白天"我的发言回合"
		cs.MySpeakTurn = gs.SpeakTurnSeat == Seat(viewer)
		// §134 守卫专属字段默认 -1(非守卫视角严格脱敏 — 守卫盲守,绝不暴露 WolfKillTarget)。
		// 与下方 RoleGuard 分支配套使用:守卫视角填真值,其余视角保持 -1。
		cs.GuardLastProtect = -1
		cs.GuardProtectTarget = -1
		// 预言家上一晚查验
		if myRole == RoleSeer && gs.Players[viewer].LastSeerCheck != NoSeat {
			cs.SeerLastCheck = int(gs.Players[viewer].LastSeerCheck)
		}
		// 女巫用药历史 + 本晚狼刀(让她决定救人)
		if myRole == RoleWitch {
			cs.WitchAntidoteUsed = gs.Players[viewer].WitchAntidoteUsed
			cs.WitchPoisonUsed = gs.Players[viewer].WitchPoisonUsed
			if !cs.WitchAntidoteUsed && gs.WolfKillTarget != NoSeat {
				cs.WitchWolfTarget = int(gs.WolfKillTarget)
			} else {
				cs.WitchWolfTarget = -1
			}
		}
		// §134 守卫专属视野:仅自己看到 GuardLastProtect / GuardProtectTarget。
		// 严格脱敏:守卫盲守,绝不暴露 WolfKillTarget(与女巫的 WitchWolfTarget 区分)。
		if myRole == RoleGuard {
			cs.GuardLastProtect = int(gs.GuardLastProtect)
			cs.GuardProtectTarget = int(gs.GuardProtectTarget)
		}
		// 猎人待开枪
		if myRole == RoleHunter && gs.HunterPendingShoot {
			cs.HunterPending = true
		}
		// 2026-07-17: 狼人投票视图(仅狼人玩家在 night_wolves 阶段可见)
		if myRole == RoleWerewolf && gs.Phase == PhaseNightWolves {
			if wv := BuildWolfPeerView(gs); wv != nil {
				cs.WolfVoteView = wv
			}
		}
	}

	// 当前投票计数(若有):所有玩家可看
	if gs.Phase == PhaseVote || gs.Phase == PhaseSheriff {
		tally := gs.TallyVotes(gs.Phase == PhaseSheriff)
		if len(tally) > 0 {
			cs.Votes = make(map[string]int, len(tally))
			for s, c := range tally {
				cs.Votes[seatKey(int(s))] = c
			}
		}
	}

	// §报告-20260804-03 BUG-04: 警长竞选参选名单(全场可见)。
	// SheriffCandidates() 内部已做 Phase==PhaseSheriff 守卫,避免把
	// PhaseSpeak 的「已发言」误当成「已参选」下发。
	if cands := gs.SheriffCandidates(); len(cands) > 0 {
		cs.SheriffCandidates = make([]int, 0, len(cands))
		for _, s := range cands {
			cs.SheriffCandidates = append(cs.SheriffCandidates, int(s))
		}
	}

	// §报告-20260804-03 BUG-05: 当前 viewer 自己的投票状态。
	// 仅入座玩家(viewer>=0)填充;观战者保持 false/-1。
	if viewer >= 0 && viewer < MaxPlayers {
		cs.MyVoted = gs.Players[viewer].Voted
		cs.MyVoteTarget = int(gs.Players[viewer].VoteTarget)
	}

	// BUG Round 40 §95: 首夜强制发言阶段视图填充。
	// 仅在 PhasePreWolves 时下发 PhaseExtra;其余阶段 nil(前端可选渲染)。
	//
	// 2026-07-09 §13 增强:PhaseDeadlineAt/RemainingSec 对所有阶段都下发
	//(不再仅限 pre_wolves),让前端 PhaseClock 组件在所有阶段都显示倒计时。
	if gs.Phase == PhasePreWolves {
		extra := &PhaseExtraJSON{
			RoundsTotal:       gs.PreWolvesSpeakRoundsPerPlayer,
			RoundsCurrent:     gs.PreWolvesSpeakRound,
			SpeakCountPerSeat: gs.PreWolvesSpeakCount,
		}
		if !gs.FirstNightGraceEnd.IsZero() {
			rem := time.Until(gs.FirstNightGraceEnd)
			if rem < 0 {
				rem = 0
			}
			extra.GraceRemainingSec = int(rem.Seconds())
			extra.DeadlineAt = gs.FirstNightGraceEnd.UTC().Format(time.RFC3339)
		}
		cs.PhaseExtra = extra
	}

	// 2026-07-09 §13 增强 — 时钟字段(所有阶段)。如果 PhaseExtra 还没创建,
	// 创建一个只含 deadline 信息的 extra;若已经创建(pre_wolves 路径),则补字段。
	if !gs.PhaseDeadlineAt.IsZero() {
		rem := time.Until(gs.PhaseDeadlineAt)
		remSec := int(rem.Seconds())
		if cs.PhaseExtra == nil {
			cs.PhaseExtra = &PhaseExtraJSON{}
		}
		cs.PhaseExtra.PhaseDeadlineAt = gs.PhaseDeadlineAt.UTC().Format(time.RFC3339)
		cs.PhaseExtra.RemainingSec = remSec
	}

	// BUG 2026-07-09: 遗言阶段进度 + 已死亡玩家列表。
	if gs.Phase == PhaseDeathLyric {
		if cs.PhaseExtra == nil {
			cs.PhaseExtra = &PhaseExtraJSON{}
		}
		total := len(gs.DeathLyricQueue) + len(gs.DeathLyricDone)
		cs.PhaseExtra.DeathLyric = &DeathLyricExtra{
			CurrentSeat: int(gs.DeathLyricCurrent),
			Total:       total,
			Done:        len(gs.DeathLyricDone),
		}
		cs.PhaseExtra.DeadList = buildDeadListLocked(gs)
	}

	// 2026-07-10: 重开局投票阶段视图。
	// 计算 yes/no/abstain 明细,以及 my_choice(若 viewer 入座)。
	if gs.Phase == PhaseRestartVote {
		if cs.PhaseExtra == nil {
			cs.PhaseExtra = &PhaseExtraJSON{}
		}
		// 直接扫描 gs.Seats 求 eligible(视图层无 r 引用)。
		eligible := make([]Seat, 0, MaxPlayers)
		for i, uid := range gs.Seats {
			if uid != "" {
				eligible = append(eligible, Seat(i))
			}
		}
		yesList := make([]int, 0, len(gs.RestartVoteYes))
		for s := range gs.RestartVoteYes {
			yesList = append(yesList, int(s))
		}
		noList := make([]int, 0, len(gs.RestartVoteNo))
		for s := range gs.RestartVoteNo {
			noList = append(noList, int(s))
		}
		absList := make([]int, 0, len(gs.RestartVoteAbstain))
		for s := range gs.RestartVoteAbstain {
			absList = append(absList, int(s))
		}
		sort.Ints(yesList)
		sort.Ints(noList)
		sort.Ints(absList)

		rv := &RestartVoteExtra{
			Yes:           yesList,
			No:            noList,
			Abstain:       absList,
			Decided:       gs.RestartVoteDone,
			Result:        gs.RestartVoteResult,
			Winner:        gs.Winner,
			EligibleCount: len(eligible),
			FastRestart:   gs.FastRestart,
		}
		// 通过阈值 = ceil(eligible * num/den) + 1
		// FastRestart 模式降为简单多数 ceil(N/2)+1。
		num, den := restartVoteQuorumFromConfig()
		if gs.FastRestart {
			num, den = 1, 2
		}
		yesQuota := (len(eligible)*num + den - 1) / den
		if yesQuota < 1 {
			yesQuota = 1
		}
		rv.YesQuota = yesQuota + 1

		if !gs.PhaseDeadlineAt.IsZero() {
			rv.DeadlineAt = gs.PhaseDeadlineAt.UTC().Format(time.RFC3339)
			rem := time.Until(gs.PhaseDeadlineAt)
			if rem < 0 {
				rem = 0
			}
			rv.RemainingSec = int(rem.Seconds())
		}

		if !isSpectator && viewer >= 0 && viewer < MaxPlayers {
			mySeat := Seat(viewer)
			if gs.RestartVoteYes[mySeat] {
				rv.MyChoice = "yes"
			} else if gs.RestartVoteNo[mySeat] {
				rv.MyChoice = "no"
			} else if gs.RestartVoteAbstain[mySeat] {
				rv.MyChoice = "abstain"
			}
		}
		cs.PhaseExtra.RestartVote = rv
	}

	// 2026-07-21 §人类玩家操作重构 — 「轮到我了」字段。
	// 仅当房间有人类且 viewer 是入座玩家时填充;观战者(-1)与全 AI 房间永远 false。
	if viewer >= 0 && viewer < MaxPlayers && !gs.Players[viewer].IsBot {
		fillMyTurnExtra(gs, cs, Seat(viewer))
	}

	return cs
}

// fillMyTurnExtra 2026-07-21 §人类玩家操作重构 — 计算 phase_extra.my_turn_now
// 与 my_turn_remaining_sec。规则:
//   - 仅填充"当前人类 viewer 应当行动"的阶段:night_wolves (狼投票)/night_seer
//     (预言家)/night_witch (女巫)/speak (白天发言)/vote (投票)/sheriff (参选+投票)
//     /idiot_reveal (翻牌)/hunter_shoot (开枪)/death_lyric (遗言)。
//   - 全 AI 房间(任意 seat IsBot=true)不调用,自然保持 false/0。
//   - 旁观者(-1)不调用。
//
// 设计要点:本函数与现有 TurnActingSeat/SpeakTurnSeat/Voted/WolfVoteCast
// 等字段一致,只是把这些"raw"字段压缩成一个 boolean + remaining 秒数,
// 让前端 <MyTurnIndicator> 不用写 11 段 phase 判断。
func fillMyTurnExtra(gs *GameState, cs *ClientGameState, mySeat Seat) {
	if gs == nil || cs == nil || mySeat < 0 || mySeat >= MaxPlayers {
		return
	}
	// 房间必须有人类(IsBot==false 才叫"轮到我了"语义)。
	hasHuman := false
	for i := range gs.Seats {
		if gs.Seats[i] != "" && !gs.Players[i].IsBot {
			hasHuman = true
			break
		}
	}
	if !hasHuman {
		return
	}
	// 死亡玩家在 speak/vote/sheriff 阶段不参与(遗言阶段单独判断)。
	myAlive := gs.AliveSeat(mySeat)

	myTurn := false
	switch gs.Phase {
	case PhaseNightWolves:
		// 所有存活狼人都要投票,直到全部 cast(含弃权) → tally 触发 endWolfPhase。
		if myAlive && gs.Roles[mySeat] == RoleWerewolf {
			myTurn = !gs.WolfVoteCast[mySeat]
		}
	case PhaseNightGuard:
		// §134 守卫守护阶段:仅存活守卫本人 myTurn=true。
		if myAlive && mySeat == gs.GuardSeat {
			myTurn = true
		}
	case PhaseNightSeer:
		if myAlive && gs.Roles[mySeat] == RoleSeer {
			myTurn = true
		}
	case PhaseNightWitch:
		if myAlive && mySeat == gs.WitchSeat {
			myTurn = true
		}
	case PhaseSpeak:
		// 白天发言。死人不能发言(遗言走 death_lyric 独立路径)。
		if myAlive && mySeat == gs.SpeakTurnSeat {
			myTurn = true
		}
	case PhaseVote:
		// 投票。死人不能投票;白痴翻牌后无投票权。
		if myAlive && !gs.Players[mySeat].Voted {
			if !(gs.Roles[mySeat] == RoleIdiot && gs.Players[mySeat].IdiotRevealed) {
				myTurn = true
			}
		}
	case PhaseSheriff:
		// 警长竞选第一轮:所有存活玩家举手;若已有 sheriff=NoSeat 则全员。
		// 二次平票(gs.SheriffSeat==NoSeat 但 phase=speak):本函数不视为"我行动"。
		if myAlive && gs.SheriffSeat == NoSeat && gs.DayNumber == 1 {
			myTurn = !gs.Players[mySeat].HasSpoken
		}
	case PhaseIdiotReveal:
		// 白痴翻牌:仅最高票存活白痴需决定。
		if myAlive && gs.DayEliminated == mySeat && gs.Roles[mySeat] == RoleIdiot && !gs.Players[mySeat].IdiotRevealed {
			myTurn = true
		}
	case PhaseHunterShoot:
		// 猎人开枪:毒杀不开枪,其它情况猎人必开枪。
		if myAlive && gs.Roles[mySeat] == RoleHunter && gs.HunterPendingShoot && gs.HunterPendingFrom != "poison" {
			myTurn = true
		}
	case PhaseDeathLyric:
		// 遗言:死人专属,且仅当本座位是当前遗言座位。
		if !myAlive && gs.DeathLyricCurrent == mySeat {
			myTurn = true
		}
	}

	if !myTurn {
		return
	}

	if cs.PhaseExtra == nil {
		cs.PhaseExtra = &PhaseExtraJSON{}
	}
	cs.PhaseExtra.MyTurnNow = true
	if !gs.PhaseDeadlineAt.IsZero() {
		rem := time.Until(gs.PhaseDeadlineAt)
		cs.PhaseExtra.MyTurnRemainingSec = int(rem.Seconds())
	}
}

// publicRoleName §135 返回该座位**可公开**的角色名;未公开时返回空串。
//
// DeadPlayerJSON.Role 曾无条件填 gs.Roles[i].String(),等于绕过 players[].role
// 的脱敏,把全部死者身份从另一条通道(all_dead_list_verbose / dead_list /
// last_night_deaths_verbose)原样下发给全房 —— 前端 HistoryDrawer ⚰ 死亡页与
// 座位死亡条会直接渲染出来。所有死亡列表构造器必须统一走本函数。
func publicRoleName(gs *GameState, seat Seat) string {
	if !gs.RolePubliclyRevealed(seat) {
		return ""
	}
	return gs.Roles[seat].String()
}

// buildDeadListLocked 构造已死亡玩家列表(含遗言状态)。全部玩家 + 观战者可见。
//
// BUG-R227-P2-01 (2026-08-01): 历史抽屉 ⚰ 死亡 / ⏱ 时间轴渲染的
// DeadPlayerJSON.Account 字段原先填的是 Player.UserID (即
// t_lsm_game_user.id UUID),玩家看到的是
// `#2 ea9587d5-ffe0-4b17-b2b2-534aac5164df`,既丑陋又构成不必要的
// 用户标识符暴露。修复:在三个 buildDeadList*Locked 函数里把 Account
// 改成**座位派生昵称**(bot → "Bot #N号",人类 → "玩家N号"),
// 然后 BuildClientStateWithRoom::enrichDeadListAccountsLocked 在
// populateAgentNames 之后用 cs.Players[i].AgentName 把 bot 昵称升级为
// "agent_name #N号" (与 GameChatPanel.toRoomPlayers 完全一致的策略)。
// 单一事实来源在 enrichDeadListAccountsLocked,此处仅保证 Account 不是 UUID。
func buildDeadListLocked(gs *GameState) []DeadPlayerJSON {
	out := make([]DeadPlayerJSON, 0, MaxPlayers)
	// 当天号 → 死因:辅助判断(死亡顺序的近似)。
	for i := 0; i < MaxPlayers; i++ {
		p := &gs.Players[i]
		if p.Alive || gs.Seats[i] == "" {
			continue
		}
		status := "ineligible"
		if p.LastWords {
			// LastWords 仍为 true 表示还未消费(仍在队列或待发言)。
			if gs.DeathLyricDone[Seat(i)] {
				// 防御性:已完成但 LastWords 未清(不应发生)。
				status = "spoken"
			} else {
				status = "pending"
			}
		} else {
			// LastWords=false:可能已发言/跳过,也可能 ineligible(毒杀/自爆/Day≥3)。
			if gs.DeathLyricDone[Seat(i)] {
				// 在 done map 中,但需区分 spoken / skipped。引擎未分开记录,
				// 统一标 spoken(前端显示"已发言/跳过"通用徽章)。
				status = "spoken"
			} else {
				status = "ineligible"
			}
		}
		out = append(out, DeadPlayerJSON{
			Seat:            i,
			Account:         seatDisplayAccount(p),
			Role:            publicRoleName(gs, Seat(i)),
			LastWordsStatus: status,
			Cause:           p.DeathCause,
			Verdict:         p.DeathVerdict,
			Day:             gs.DayNumber,
		})
	}
	return out
}

// buildAllDeadListLocked 构造全阶段可用的"全部历史死亡"列表(2026-07-11 R96-P1)。
//
// 与 buildDeadListLocked(仅 PhaseDeathLyric)、buildDeadListForSeatsLocked(仅 LastNightDeaths 涉及的座位)
// 不同:本函数扫描 gs.Players 全表,纳入所有 !p.Alive 的座位,**不依赖** LastNightDeaths(每晚重置)
// 与 DeathLyricDone(仅死亡时更新),让 day2/3/4 已死座位始终带 §123 verdict 字段。
//
// LastWordsStatus 留空(本字段专为遗言进度设计,与 verdict 徽章无关)。
//
// BUG-R227-P2-01: Account 走 seatDisplayAccount 而非 UserID(详见 buildDeadListLocked 注释)。
func buildAllDeadListLocked(gs *GameState) []DeadPlayerJSON {
	out := make([]DeadPlayerJSON, 0, MaxPlayers)
	for i := 0; i < MaxPlayers; i++ {
		p := &gs.Players[i]
		if p.Alive || gs.Seats[i] == "" {
			continue
		}
		out = append(out, DeadPlayerJSON{
			Seat:    i,
			Account: seatDisplayAccount(p),
			Role:    publicRoleName(gs, Seat(i)),
			Cause:   p.DeathCause,
			Verdict: p.DeathVerdict,
			Day:     gs.DayNumber,
		})
	}
	return out
}

// buildDeadListForSeatsLocked 构造指定座位列表的死亡信息(用于 LastNightDeathsVerbose)。
// 2026-07-10 §123: 即使座位仍存活(理论上不应发生),也返回空 verdict,便于前端容错。
//
// BUG-R227-P2-01: Account 走 seatDisplayAccount 而非 UserID(详见 buildDeadListLocked 注释)。
func buildDeadListForSeatsLocked(gs *GameState, seats []Seat) []DeadPlayerJSON {
	out := make([]DeadPlayerJSON, 0, len(seats))
	for _, s := range seats {
		if s < 0 || s >= MaxPlayers || gs.Seats[s] == "" {
			continue
		}
		p := &gs.Players[s]
		out = append(out, DeadPlayerJSON{
			Seat:    int(s),
			Account: seatDisplayAccount(p),
			Role:    publicRoleName(gs, s),
			Cause:   p.DeathCause,
			Verdict: p.DeathVerdict,
			Day:     gs.DayNumber,
		})
	}
	return out
}

// seatDisplayAccount 把 Player 派生为前端可读的显示昵称(非 UUID)。
//
// BUG-R227-P2-01 (2026-08-01): 原本 DeadPlayerJSON.Account 直接填
// Player.UserID (t_lsm_game_user.id UUID),前端 HistoryDrawer 渲染为
// `#2 ea9587d5-ffe0-4b17-b2b2-534aac5164df`。本函数把这一通道关掉:
//   - bot 座位 → "Bot #N号" (与前端 GameChatPanel.toRoomPlayers 一致,
//     后续 BuildClientStateWithRoom.enrichDeadListAccountsLocked 会用
//     cs.Players[].AgentName 升级为 "agent_name #N号")
//   - 真人座位 → "玩家N号"
//
// 单一事实来源:与 GameChatPanel.toRoomPlayers 的命名策略严格对齐,
// 保证对局所有通道(座位卡 / 聊天 / 历史抽屉)显示一致。
func seatDisplayAccount(p *Player) string {
	n := int(p.Seat) + 1
	if p.IsBot {
		return fmt.Sprintf("Bot #%d号", n)
	}
	return fmt.Sprintf("玩家%d号", n)
}

// seatKey 把座位序列化为字典键(JSON map 不允许整型 key 直接序列化为字符串)。
func seatKey(s int) string {
	return intToA(s)
}

func intToA(n int) string {
	if n < 0 {
		return "-1"
	}
	// 手写 Itoa,避免额外 import
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}

// buildSheriffStreamAgesLocked 计算每个警徽流槽位的保鲜期信息。
//
// 参数:
//   - currentRound:当前轮次(DayNumber,BuildClientState 时 gs.DayNumber)
//   - streams / declaredRounds:GameState 中的两条警徽流槽(长度均为 2)
//
// 行为:
//   - 未声明(slot 为 NoSeat):AgeRounds=-1,IsStale=false,Target=-1
//   - 已声明:AgeRounds = currentRound - declaredRound;若 cfg 配置为 0 → 永不 stale
//   - 负 AgeRounds(声明轮次 > 当前,例如刚声明)被 clamp 到 0
//
// 设计依据(CLAUDE.md §130 接线 + §135 单点判定):仅是 UI 提示字段,不影响
// 警徽流真实验人历史或 RolePubliclyRevealed 单点。
func buildSheriffStreamAgesLocked(currentRound int, streams [2]Seat, declaredRounds [2]int) []SheriffStreamAgeJSON {
	persist := cfgWerewolfSheriffPersistRounds()
	out := make([]SheriffStreamAgeJSON, 0, 2)
	for slot := 0; slot < 2; slot++ {
		target := int(streams[slot])
		entry := SheriffStreamAgeJSON{
			Slot:          slot,
			Target:        target,
			DeclaredRound: declaredRounds[slot],
			AgeRounds:     -1,
			IsStale:       false,
		}
		if target == int(NoSeat) || declaredRounds[slot] == 0 {
			out = append(out, entry)
			continue
		}
		age := currentRound - declaredRounds[slot]
		if age < 0 {
			age = 0
		}
		entry.AgeRounds = age
		// 持久化阈值 <=0 时永远不 stale(向旧行为兼容,§20260811-10 U5 S-02)。
		if persist > 0 && age > persist {
			entry.IsStale = true
		}
		out = append(out, entry)
	}
	return out
}

// ─────────────────── 狼人内部互看 ───────────────────
// 狼人协商阶段让所有狼人看见自己的"同伴座位"列表;非狼人不暴露。

// WolfPeerView 构造"夜晚狼人"专属视图:把同阵营座位列表注入。
// WolfPeerView 是狼人夜间投票视图(2026-07-17 扩展)。
// 携带投票快照 + 计票结果,供前端渲染队友投票状态。
type WolfPeerView struct {
	WolfSeats   []int          `json:"wolf_seats"`        // 存活狼人座位
	KillTarget  int            `json:"kill_target"`       // 最终击杀目标(计票后);-1=未决
	Votes       map[int]int    `json:"votes"`             // seat -> target 投票快照
	Abstain     []int          `json:"abstain,omitempty"` // 已弃权(含未投票)的狼人座位;nil/空→省略避免前端 null.includes 崩溃
	VotesCast   int            `json:"votes_cast"`        // 已提交投票(含弃权)的狼人数
	TotalWolves int            `json:"total_wolves"`      // 存活狼总数
	Voting      bool           `json:"voting"`            // true=投票中;false=已结算
	Tally       *WolfVoteTally `json:"tally,omitempty"`   // 计票结果(Voting=false 时)
}

// BotHypothesisJSON 是 ClientGameState 对外的假说条目(避免循环导入 werewolf 包)。
type BotHypothesisJSON struct {
	Seat    int                          `json:"seat"`
	Entries []HypothesisEntryJSONForView `json:"entries"`
	Round   int                          `json:"round"`
}

// HypothesisEntryJSONForView 是单条假说观战视图。
type HypothesisEntryJSONForView struct {
	TargetSeat int    `json:"target_seat"`
	RoleGuess  string `json:"role_guess"`
	Confidence int    `json:"confidence"`
	Supporting string `json:"supporting"`
	Refuting   string `json:"refuting"`
	UpdatedAt  int64  `json:"updated_at"`
}

// populateHypotheses 是 BuildClientStateWithRoom 的 spectator-only 下发入口。
// §135:玩家侧(viewer>=0)整段 omitempty;spectator 全量下发。
// 调用前置:必须已持 r.mu(§92a)。
func (r *WerewolfRoom) populateHypotheses(cs *ClientGameState, viewer int) {
	if cs == nil || r == nil {
		return
	}
	if viewer >= 0 && viewer < MaxPlayers {
		// §135 真人玩家永远收不到假说视图(类似 BotContexts 的 spectator-only 语义)。
		return
	}
	snap := r.hypothesisStoreLocked().SnapshotAllLocked()
	if len(snap) == 0 {
		return
	}
	out := make([]BotHypothesisJSON, 0, len(snap))
	for _, t := range snap {
		row := BotHypothesisJSON{
			Seat:    t.Seat,
			Round:   t.Round,
			Entries: make([]HypothesisEntryJSONForView, 0, len(t.Entries)),
		}
		for _, e := range t.Entries {
			row.Entries = append(row.Entries, HypothesisEntryJSONForView{
				TargetSeat: e.TargetSeat,
				RoleGuess:  e.RoleGuess,
				Confidence: e.Confidence,
				Supporting: e.Supporting,
				Refuting:   e.Refuting,
				UpdatedAt:  e.UpdatedAt,
			})
		}
		out = append(out, row)
	}
	cs.BotHypotheses = out
}

// BuildWolfPeerView 返回狼人夜间投票视图。
// 2026-07-17 扩展: 携带投票快照 + 计票结果。
func BuildWolfPeerView(gs *GameState) *WolfPeerView {
	if gs == nil || gs.Phase != PhaseNightWolves {
		return nil
	}
	out := &WolfPeerView{
		KillTarget: -1,
		Votes:      map[int]int{},
		Abstain:    []int{},
	}
	for i, r := range gs.Roles {
		if r == RoleWerewolf && gs.AliveSeat(Seat(i)) {
			out.WolfSeats = append(out.WolfSeats, i)
			out.TotalWolves++
		}
	}
	// 分类: 已投票 / 已弃权 / 未投票
	for _, ws := range out.WolfSeats {
		if gs.WolfVoteCast[ws] {
			out.VotesCast++
			if gs.WolfVotes[ws] == NoSeat {
				out.Abstain = append(out.Abstain, ws)
			} else {
				out.Votes[int(ws)] = int(gs.WolfVotes[ws])
			}
		}
	}
	// VotesCast 仅统计已提交投票(含弃权)的狼人数
	out.VotesCast = 0
	for _, ws := range out.WolfSeats {
		if gs.WolfVoteCast[ws] {
			out.VotesCast++
		}
	}

	if gs.WolfKillTarget != NoSeat {
		out.KillTarget = int(gs.WolfKillTarget)
		out.Voting = false
		out.Tally = gs.WolfVoteTally
	} else {
		out.Voting = true
	}
	return out
}

// ─────────────────── 女巫情报(夜晚专用) ───────────────────

// WitchInform 返回女巫当晚应知道的信息:当晚狼刀目标 + 解药/毒药剩余。
type WitchInform struct {
	WolfTarget        int   `json:"wolf_target"` // 今晚被狼杀的人;空刀=-1
	AntidoteAvailable bool  `json:"antidote_available"`
	PoisonAvailable   bool  `json:"poison_available"`
	AliveCandidates   []int `json:"alive_candidates"` // 可毒杀的活人(不含女巫自己)
}

// BuildWitchInform 在 PhaseNightWitch 期间构造女巫应看到的情报。
func BuildWitchInform(gs *GameState) *WitchInform {
	if gs == nil || gs.Phase != PhaseNightWitch {
		return nil
	}
	out := &WitchInform{
		WolfTarget:        -1,
		AntidoteAvailable: !gs.Players[gs.WitchSeat].WitchAntidoteUsed,
		PoisonAvailable:   !gs.Players[gs.WitchSeat].WitchPoisonUsed,
	}
	if gs.WolfKillTarget != NoSeat {
		out.WolfTarget = int(gs.WolfKillTarget)
	}
	for i := 0; i < MaxPlayers; i++ {
		if i != int(gs.WitchSeat) && gs.AliveSeat(Seat(i)) {
			out.AliveCandidates = append(out.AliveCandidates, i)
		}
	}
	return out
}

// SeerInform 在 PhaseNightSeer 期间构造预言家可查看的目标列表 + 自己已知
// 查验结果(供 UI 显示"金水/查杀")。
type SeerInform struct {
	AliveCandidates []int `json:"alive_candidates"`
	// LastResult: 上一晚查验结果(阵营字符串, 仅自己可见)
	LastResultFaction string `json:"last_result_faction,omitempty"`
	LastResultSeat    int    `json:"last_result_seat,omitempty"`
}

// BuildSeerInform 在 PhaseNightSeer 期间构造预言家应看到的信息。
func BuildSeerInform(gs *GameState, actor Seat) *SeerInform {
	if gs == nil || gs.Phase != PhaseNightSeer {
		return nil
	}
	out := &SeerInform{}
	for i := 0; i < MaxPlayers; i++ {
		if i == int(actor) {
			continue
		}
		if gs.AliveSeat(Seat(i)) {
			out.AliveCandidates = append(out.AliveCandidates, i)
		}
	}
	if t := gs.Players[actor].LastSeerCheck; t != NoSeat {
		out.LastResultFaction = FactionOf(gs.Roles[t]).String()
		out.LastResultSeat = int(t)
	}
	return out
}

// BuildClientStateWithRoom 2026-07-10 §125 增强 — BuildClientState 的扩展,
// 额外填充法官总结(JudgeSummary / JudgeModelMemories)。
//
// 旧 BuildClientState 签名不变,测试与外部调用继续可用。
func BuildClientStateWithRoom(roomID string, r *WerewolfRoom, viewer int) *ClientGameState {
	cs := BuildClientState(roomID, r.Seats, viewer, r.State)
	if cs == nil {
		return nil
	}
	// 2026-07-18 §UX-运行时: 把游戏开始时间下发给前端,用于显示房间已运行时间。
	// 0 = 未开始(filling 阶段),前端显示"⏱ —"。
	cs.GameStartedAt = r.gameStartedAt
	// 2026-08-10 §20260810-12 D2 — 死者身份延时揭晓配置下发。
	// RoomConfig.DeathRevealDelayMin(0/5/15) — 0 时 omitempty 不下发,前端走
	// 立即揭晓分支(零回归);5 / 15 时 SettlementModal 启动倒计时。
	cs.DeathRevealDelayMin = r.deathRevealDelayMin
	// §20260811-09 U2 — Agent 难度档位全员可见。
	cs.AgentDifficulty = r.agentDifficulty
	// §20260811-01 U3 — 投票半公开计票悬念配置下发。
	func() {
		defer func() { _ = recover() }()
		cfg := config.Load()
		if cfg != nil && cfg.Werewolf.VoteSuspense {
			cs.VoteSuspense = true
			delay := cfg.Werewolf.VoteSuspenseDelayMs
			if delay <= 0 {
				delay = 3000
			}
			cs.VoteSuspenseDelayMs = delay
		}
	}()
	if r.judge != nil {
		cs.JudgeEnabled = true
		tr := r.judge.JudgeTranscript()
		cs.JudgeContext = &tr
		cs.JudgeSummary = tr.LastSummary
		// 2026-07-30 解决和设计方案-20260730-03 Fix-A2: 终局法官语分流。
		// 法官 announce 是「软信息」(LLM 自由文本 + fallback),对局结束后
		// judge_context 里可能仍滞留游戏内最后一条阶段宣告(如「遗言阶段。
		// 刚刚被投出的玩家…」——终局时法官 LLM 正处熔断期,game_over 宣告
		// 未能生成)。在 view 组装层单点收口:Status=="over" 时覆盖为终局
		// 文案,不再透传阶段宣告残留(与 §135 RolePubliclyRevealed 单点判定
		// 同思路),前端 JudgeActionBar 无需改动。
		if cs.Status == "over" && cs.JudgeContext != nil {
			winner := cs.Winner
			if winner == "" {
				winner = "?"
			}
			finalText := "🏆 对局结束:" + winner + " 阵营胜利。整局总结生成中,可在 📜 历史 抽屉查看。"
			cs.JudgeContext.LastAnnouncement = finalText
		}
	}
	cs.JudgeModelMemories = r.ModelMemoriesSnapshotLocked()
	// 2026-07-24 优化:下发暂停状态。前端 GameInfoPanel 渲染 ⏸ 暂停/▶ 恢复按钮,
	// PausedBy/PauseReason 让真人玩家知道是谁暂停 + 为什么(避免误操作疑惑)。
	cs.Paused = r.paused
	cs.PausedBy = r.pausedBy
	cs.PausedReason = r.pausedReason
	// 2026-07-30 §统计增强 — 聚合所有 Agent + 法官的 Token + API 统计。
	// BUG-R212-P0-01: 必须调锁内变体 —— 本函数的全部调用方都已持有 r.mu,
	// 调公开变体会二次 Lock 同一把不可重入的 sync.Mutex → 永久自死锁。
	cs.AgentStats = r.aggregateAgentStatsLocked()
	// 2026-08-10 §20260810-05 — 信息账本观战者快照。
	// 仅 spectator(viewer==-1)填充;玩家视图(viewer>=0)与 REST 房间视图
	// 不下发,与 BotContexts 的 spectator-only 语义对齐。账本快照上限 200 条
	// (信息量远超单屏可读范围,超出部分二期前端分页时再调)。
	if viewer < 0 && r.infoLedger != nil {
		cs.InfoLedger = r.infoLedger.SnapshotJSON(200)
		// 2026-08-10 §20260810-08：本函数全部调用方已持 r.mu，
		// detectLeaksLocked 只做锁内缓存读写，绝不再次 Lock（R212）。
		cs.InfoLeaks = r.detectLeaksLocked()
	}
	// 2026-08-10 §20260810-06 — 承诺列表（按视角脱敏）。
	// 观战者(viewer<0)可见全部真实状态；玩家仅见自己的承诺(含真实状态) + 他人的 pending。
	// 锁内直读：本函数全部调用方已持 r.mu（§92a）。
	cs.Commitments = r.getCommitmentsForViewerLocked(viewer)
	// 2026-08-10 §20260810-07 — 多假说并行推演观战者快照。
	// 仅 spectator(viewer<0)填充;玩家视图(viewer>=0)omitempty。
	// 与 BotContexts / InfoLedger 的 spectator-only 语义对齐(§135 公平性)。
	r.populateHypotheses(cs, viewer)
	// §20260810-09 — 上帝视角观战快照。仅 spectator(viewer<0)填充;
	// 玩家与 REST 房间视图**永远不**下发(§135 公平性)。
	// ⚠️ §92a:populateGodModeLocked 是**锁内**变体 —— 本函数全部调用方
	// (GetState / StateForSeat / SpectatorState / SpectatorView)已持有 r.mu,
	// 调用方**不可**再 Lock;公开包装见 (§135) 函数注释。
	if viewer < 0 {
		cs.GodMode = r.populateGodModeLocked()
		// §20260812-03 U1 — 阵营胜率热力图(仅 spectator 可见,§132 隐私隔离)。
		// 启发式算法不调 LLM(§120 公平性 + §13 跨职责约束)。
		cs.WinRateProbability = computeWinRateProbabilityLocked(r)
		// §20260811-09 U1 — 观战者专属:解说 feed(最近 20 条)。锁内拷 snapshot
		// 后拷结构,§92a 一致(reader 已持 r.mu,直接读)。
		if len(r.commentaryFeed) > 0 {
			cs.CommentaryFeed = make([]CommentaryLineJSON, 0, len(r.commentaryFeed))
			for _, line := range r.commentaryFeed {
				cs.CommentaryFeed = append(cs.CommentaryFeed, CommentaryLineJSON{
					Seq:      line.Seq,
					Text:     line.Text,
					Style:    line.Style,
					ModelKey: line.ModelKey,
					Kind:     line.Kind,
					TsMs:     line.TsMs,
				})
			}
		}
	}
	// §20260811-07 U2 — 自动高光集锦战报。终局时全员可见(player + spectator + REST);
	// 锁内变体 §92a。
	if cs.Status == "over" {
		if highlights := r.BattleHighlightsSnapshotLocked(); len(highlights) > 0 {
			cs.BattleReportHighlights = highlights
		}
	}
	// §20260810-09 — 警长定序状态(全场可见,非上帝视角专属)。
	// 三字段均由 startSpeakPhase 写入,玩家与观战者一致。
	cs.SheriffOrderSet = r.State.SheriffOrderSet
	cs.SheriffSpeakDirection = r.State.SheriffSpeakDirection
	cs.SheriffSpeakSelfPos = r.State.SheriffSpeakSelfPos
	// §20260811-02 U1 — 发言影响力生态(全场可见,非 spectator 专属)。
	// ⚠️ §92a:influenceTrackerLocked / SnapshotLocked 均为**锁内**变体,
	// 本函数 4 个调用点已持 r.mu,严禁在此新增 Lock(R212 自死锁教训)。
	cs.InfluenceScores = r.influenceTrackerLocked().SnapshotLocked()
	// 2026-08-05 §02 — 座位级「最后一次公开发言」回填(人机统一)。
	// ⚠️ §92a:本函数的全部 4 个调用点(GetState / StateForSeat / SpectatorState /
	// SpectatorView)都**已持有 r.mu**,这里是**锁内直读** r.lastSpeechBySeat,
	// 严禁新增任何 Lock(R212 自死锁教训)。
	fillLastSpeechLocked(cs, r)
	return cs
}

// fillLastSpeechLocked 把 r.lastSpeechBySeat 回填到 cs.Players[].LastSpeech*。
// **锁内变体**:调用方必须已持有 r.mu(§92a)。
//
// 2026-08-05 §02 — 之所以放在 BuildClientStateWithRoom 而不是 BuildClientState:
// 后者只拿到 (seats, viewer, gs),没有房间指针,而「最后一次发言」是房间级
// 运行时缓冲而非 GameState 的一部分。改 BuildClientState 签名会波及全部测试与
// 外部调用点,故采用「WithRoom 后置回填」的最小变更方案。
func fillLastSpeechLocked(cs *ClientGameState, r *WerewolfRoom) {
	if cs == nil || r == nil || len(r.lastSpeechBySeat) == 0 {
		return
	}
	for seat := range cs.Players {
		sp, ok := r.lastSpeechBySeat[seat]
		if !ok || sp.Text == "" {
			continue
		}
		cs.Players[seat].LastSpeech = sp.Text
		cs.Players[seat].LastSpeechAt = sp.AtMs
	}
}

// AggregateAgentStats 聚合本局所有 Agent + 法官的 API/Token 统计。
// 2026-07-30 §统计增强。不进 DB，纯内存态。
// 房间解散时 WerewolfRoom 对象被 GC，BotAgents/judge 引用释放，统计自动回收。
//
// ⚠️ 本函数是**公开变体**:自己获取 r.mu,只能由**未持锁**的调用方使用。
// BuildClientStateWithRoom 等已持锁路径必须调 aggregateAgentStatsLocked。
//
// BUG-R212-P0-01 (2026-07-30) §92a: 本函数原先被 BuildClientStateWithRoom 直接
// 调用,而后者的全部 4 个调用点(GetState / StateForSeat / SpectatorState /
// SpectatorView)都已持有 r.mu。sync.Mutex 不可重入 → 第二次 Lock() 永久阻塞
// 且不释放,表现为「创建 12AI 房间 HTTP 永不返回(弹窗卡死)」+「刷新后
// game.state 永不下发(永久 ⏳ 正在同步游戏状态…)」+ 该房间所有 REST 快照
// 退化为 lockRoomBriefly 200ms 超时兜底。修复:拆出锁内变体。
func (r *WerewolfRoom) AggregateAgentStats() *AgentRoomStats {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.aggregateAgentStatsLocked()
}

// aggregateAgentStatsLocked 是 AggregateAgentStats 的**锁内变体**(§92a 范式)。
// 调用方必须已持有 r.mu。
//
// 只读 r.BotAgents / r.judge 两个字段;内部 ag.AgentTokenStats() 与
// j.JudgeTokenStats() 取的是 Agent.mu / AgentJudge.mu(不同层级的锁),且二者
// 都不反向获取 r.mu,因此不存在锁序倒置。
func (r *WerewolfRoom) aggregateAgentStatsLocked() *AgentRoomStats {
	var stats AgentRoomStats
	for _, ag := range r.BotAgents {
		if ag == nil {
			continue
		}
		s := ag.AgentTokenStats()
		stats.TotalInputTokens += s.TotalInputTokens
		stats.TotalOutputTokens += s.TotalOutputTokens
		stats.TotalAPITokens += s.TotalAPITokens
		stats.APICallCount += s.APICallCount
		stats.APISuccessCount += s.APISuccessCount
		stats.APIFailCount += s.APIFailCount
		stats.AgentCount++
	}
	if r.judge != nil {
		js := r.judge.JudgeTokenStats()
		stats.JudgeEnabled = true
		stats.JudgeTotalInputTokens = js.TotalInputTokens
		stats.JudgeTotalOutputTokens = js.TotalOutputTokens
		stats.JudgeTotalAPITokens = js.TotalAPITokens
		stats.JudgeAPICallCount = js.APICallCount
		stats.JudgeAPISuccessCount = js.APISuccessCount
		stats.JudgeAPIFailCount = js.APIFailCount
	}
	return &stats
}

// ─────────────────── §20260810-09 上帝视角观战快照 ───────────────────
//
// GodModeSnapshot 是 spectator 专属的全知视角快照。**仅**在 BuildClientStateWithRoom
// 的 spectator 路径(viewer<0)填充,玩家视图与 REST 房间视图**永远不**下发。
//
// §135 公平性约束:
//   - 不增加 RolePubliclyRevealed 白名单分支(GodMode 是观战者侧附加视图,
//     不是玩家身份公开机制,不影响 §135 既定的「死者不翻牌」规则)。
//   - 与 BotContexts / InfoLedger 的 spectator-only 语义对齐。
//
// §121 数据形状契约:前端 types/werewolf.ts 必须显式声明此 struct。
//
// §20260811-08 更正:原注释称「后端 DisallowUnknownFields 校验已就位」——**不属实**。
// DisallowUnknownFields 只用于入站解码器(api/model_admin_api.go / api/room_api.go /
// ws/user_service.go),出站 ClientGameState 没有任何 schema 校验。本 struct 与
// ClientWeb/src/types/werewolf.ts 是**手工双文件维护,无编译期/测试期保障** ——
// 新增字段必须人工同步两处,不要依赖一个不存在的护栏。
type GodModeSnapshot struct {
	Enabled        bool             `json:"enabled"`         // 当前 spectator 是否在客户端开启上帝视角(localStorage.ww_god_mode === "1")
	Roles          map[int]string   `json:"roles"`           // seat → role_key(全 13 座位)
	Factions       map[int]string   `json:"factions"`        // seat → faction_key
	WolfKillTarget int              `json:"wolf_kill_target"` // 当前夜狼刀最终目标(-1=无/已守/已救)
	WolfVotes      map[int]int      `json:"wolf_votes"`      // wolf_seat → target_seat(本局累计狼投票快照)
	SeerChecks     []SeerCheckEntry `json:"seer_checks"`     // 预言家查验历史(从 InformationLedger 聚合)
	WitchDecisions []WitchDecision  `json:"witch_decisions"` // 女巫用药历史(从 InformationLedger 聚合)
	GuardProtects  []int            `json:"guard_protects"`  // 守卫守护历史(从 InformationLedger 聚合)

	// §20260810-11 V1 — 全视角读心观战(spectator 视角切换面板)
	// PerSeatPOV 是 13 座位的「第一视角」快照,前端 GodModeView 切换时取对应 seat 的快照渲染。
	// §119 协议层隔离:仅 spectator 视图下发,玩家视图不含;前端按 user_id !== player.user_id
	// 双重判断防止玩家"自切他视角"反作弊。
	// §135 身份公开:identity_revealed 字段由 RolePubliclyRevealed(seat) 派生,
	// 终局前为 false,终局后视规则(白痴翻牌/狼自爆/猎人开枪/终局)命中白名单。
	PerSeatPOV map[int]PerSeatPOV `json:"per_seat_pov,omitempty"`

	// §20260811-08 U3 — 已公开的技能行动(猎人开枪/骑士决斗/猎魔人狩猎/白痴翻牌)。
	//
	// 这 4 类事件在 InformationLedger 中**早有写入点**(room_action.go:110/238/275/625,
	// §20260810-05 一期已接线),但 populateGodModeLocked 的 switch 只消费了
	// night_seer / night_witch / night_guard 三类 —— 上帝视角面板因此看不到
	// 「谁开了枪 / 谁决斗了 / 谁狩猎了 / 谁翻了白痴牌」。
	//
	// §135 公平性:这 4 类恰是身份公开白名单里的事件,本就全房可见,聚合它们
	// 不构成新的身份下发通道。
	PublicActions []PublicActionEntry `json:"public_actions"`
}

// PublicActionEntry §20260811-08 U3 — 已公开的技能行动条目(spectator 上帝视角)。
//
// 设计取舍:不做 HunterShots/KnightDuels/DemonHunts/IdiotReveals 四个平行切片 ——
// 那会让 GodModeSnapshot 从 9 字段膨胀到 13,且前端要写 4 段几乎相同的渲染。
type PublicActionEntry struct {
	Day    int    `json:"day"`
	Kind   string `json:"kind"`   // "hunter_shot" / "knight_duel" / "demon_hunter" / "idiot_reveal"
	Seat   int    `json:"seat"`   // 行动者座位
	Target int    `json:"target"` // 目标座位(-1 = 无,如白痴翻牌)
	// HitWolf 仅骑士决斗 / 猎魔人狩猎有意义;其余为 nil(不适用),
	// 与「false = 没打中狼」在语义上必须可区分,故用指针而非 bool。
	HitWolf *bool `json:"hit_wolf,omitempty"`
}

// PerSeatPOV §20260810-11 V1 — 单座位的「第一视角」快照(仅 spectator 可见)。
// 与 BotTranscript 不同,这里只含「LLM 思考/决策的对外可读摘要」,不含 raw LLM token。
type PerSeatPOV struct {
	Role             string   `json:"role"`               // 角色 key(§135 终局前可显示"[已隐藏]")
	RoleRevealed     bool     `json:"role_revealed"`      // true = RolePubliclyRevealed(seat) 命中
	Faction          string   `json:"faction"`            // "wolf" / "good"
	HeartThought     string   `json:"heart_thought"`      // BotTranscript.HeartThought 截断到 200 字
	LastDecision     string   `json:"last_decision"`      // BotTranscript.LastDecisionSummary
	NightActions     []string `json:"night_actions"`      // 该座位本局夜间行动摘要(狼刀/守卫/女巫/预言家)
	ToolCallCount    int      `json:"tool_call_count"`    // 本局 tool_use 总次数
	LLMCallCount     int      `json:"llm_call_count"`     // 本局 LLM 调用次数
	LastEmotion      string   `json:"last_emotion"`       // BotTranscript.LastEmotionZh
	// §20260810-11 H2 / §20260811-08 U1 — 当前是否处于「被质疑态」(0 或 1)。
	// 注释原写「本局被质疑次数」,但引擎只有 Player.LastChallengedBy 这一「最近
	// 一次」字段(engine.go:162,每轮在 engine_day.go:337 重置),没有累计计数器。
	// §20260811-08 修正注释与实现对齐,而非新增引擎字段(那需同步 startNight /
	// advanceDay 的重置语义,风险高于收益;参见 §134 教训 (7))。
	ChallengeCount int `json:"challenge_count"`
	PublicCommitments []string `json:"public_commitments"` // 该座位的公开承诺 key 列表
}

// SeerCheckEntry 预言家查验条目(spectator 上帝视角专用)。
type SeerCheckEntry struct {
	Day    int    `json:"day"`    // 第几天夜晚(0=首夜)
	Seat   int    `json:"seat"`   // 预言家座位
	Target int    `json:"target"` // 被查验座位
	Result string `json:"result"` // "good" / "werewolf"(原始身份,不脱敏)
}

// WitchDecision 女巫用药决策(spectator 上帝视角专用)。
type WitchDecision struct {
	Day         int    `json:"day"`
	Seat        int    `json:"seat"`        // 女巫座位
	AntidoteUse int    `json:"antidote_use"` // 解药用掉的目标座位(空=-1)
	PoisonUse   int    `json:"poison_use"`   // 毒药用掉的目标座位(空=-1)
}
