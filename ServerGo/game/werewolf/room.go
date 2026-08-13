package werewolf

import (
	"context"
	"sync"
	"time"

	"LsmAgentGame/agent/core"
	"LsmAgentGame/agent/wwcommentator"
	"LsmAgentGame/agent/wwjudge"
	"LsmAgentGame/agent/wwplayer"
	"LsmAgentGame/agent/wwtypes"
	"LsmAgentGame/logger"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// recentSpeechBufferSize is the rolling cap on per-room chat history fed
// into agent GameContext.RecentSpeeches. Larger buffers let agents reason
// over more rounds, but cost context-window tokens — 50 covers ~7 days of
// short speeches with headroom.
//
// BUG: 狼人杀 7 人局 Agent 多轮上下文 — was 0 (no transcripts at all).
const recentSpeechBufferSize = 50

// whisperInboxSize is the per-seat inbox cap for whispers. Whispers are
// kept per recipient because they carry private information the recipient
// must remember (e.g. a wolf teammate's plan). The cap bounds memory in
// long games.
const whisperInboxSize = 20

// lastSpeechRuneLimit 是 PlayerJSON.LastSpeech 的最大 rune 数。
// 座位卡气泡只显示"最后一次发言"的摘要,超长发言在 wire 上截断,
// 完整原文仍可在房间聊天(GameChatPanel)中回溯。2026-08-05 §02。
const lastSpeechRuneLimit = 200

// seatSpeech 是 WerewolfRoom.lastSpeechBySeat 的值类型 ——
// 某个座位最后一次公开发言的文本 + 时间戳。2026-08-05 §02。
type seatSpeech struct {
	// Text 发言原文,已按 lastSpeechRuneLimit 做 rune 安全截断。
	Text string
	// AtMs 发言时间(Unix 毫秒),与 wwtypes.SpeechEvent.Ts 同源。
	AtMs int64
}

// ─────────────────── Room ───────────────────

// WerewolfRoom 持有一局狼人杀的内存状态。
type WerewolfRoom struct {
	mu         sync.Mutex
	RoomID     string
	Seats      [MaxPlayers]string
	State      *GameState
	Spectators map[string]struct{}

	// createdAt 是 in-memory 房间对象被创建的时间(2026-07-23 R187-1 新增)。
	// filling 阶段回收器(JanitorSweepStaleFilling)用它判断「房间停在
	// PhaseFilling 多久了」— DB 行的 created_at 与 in-memory 对象并不
	// 同步(进程重启后首次 JoinGame/SpectateGame 会重建对象),所以必须
	// 在对象自身上记录。零值 = 老代码路径创建的对象(兜底按"不过期"处理,
	// 交给 30 分钟的 JanitorSweepStale 清理)。
	createdAt time.Time

	// SeatCount 本局实际参与人数(12 = 默认,7 = 历史 werewolf_7 兼容)。
	// 默认 MaxPlayers(12);由 SetSeatCount 在房间创建时按 game_kind 改写。
	// 同步到 r.State.SeatCount(发牌前)以驱动 IsReady / 牌组选择。
	SeatCount int

	// BotAgents holds per-seat agent drivers keyed by seat. Populated after the
	// game starts (StartAgentsLocked constructs these from seatModelKeys). Empty
	// until then. A nil map simply means "no agents wired yet rather than no
	// agents defined".
	BotAgents map[int]*wwplayer.Agent

	// agentCancels mirrors BotAgents with the context.CancelFunc for each
	// agent's Run context. Used by stopAgentsLocked to tear down agents.
	agentCancels map[int]context.CancelFunc

	// agentWG tracks every agent goroutine launched by StartAgentsLocked so
	// stopAgentsLocked can block on agentWG.Wait() until they actually exit
	// their runLoop. Without this, ForceDisbandRoom / RemoveGame returns
	// immediately after issuing cancel() + close(events), but in-flight LLM
	// HTTP calls (up to 8s timeout) and 1s/2s/4s backoff retries continue
	// running with references to r.BotAgents / r.mu that have already been
	// cleared → potential nil-map writes + goroutine/TCP-connection leaks.
	//
	// BUG-WEREWOLF-DISBAND-LEAK: 管理员强制解散 7-AI 狼人杀房间后,Agent
	// goroutine 实际上并未退出,会持续向 LLM 发送请求最多 8s。stopAgentsLocked
	// 必须等到所有 goroutine 真正 return,才能保证 m.rooms 与 Hub.rooms
	// 的清理对所有后台活动可见。
	agentWG sync.WaitGroup

	// seatModelKeys records the LLM model_key configured for each agent seat at
	// room-creation time, so Phase 4 can later construct the corresponding
	// wwplayer.Agent without re-reading t_lsm_game_player. Empty string for human
	// seats.
	seatModelKeys map[int]string

	// seatPreferredRoles 座位级角色偏好(2026-08-06 §20260806-03 自选角色)。
	// 创建房间时经 SetSeatRolePrefs 写入;StartGame 前同步进 State.PreferredRoles。
	// 生命周期与 seatModelKeys 同级:restartGameLocked 原地重开时保留,
	// 每局重新注入,创建者的角色选择跨局生效。
	seatPreferredRoles map[int]Role
	// pendingCreatorRolePref 创建者角色偏好暂存:SetSeatRolePrefs 调用时
	// 创建者尚未 SyncSeat 入座(座位未分配),先暂存;JoinGame 人类入座成功后
	// 立即回填到 seatPreferredRoles[humanSeat]。仅创建窗口内有效。
	pendingCreatorRolePref Role

	// onTranscriptPublished is an optional callback invoked (in a goroutine)
	// after an agent publishes a fresh BotTranscript. The ws layer wires it to
	// broadcast game.state so the frontend seat card EmotionAvatar can refresh
	// emotion + fx in real time (2026-08-05 §表情实时同步). nil-safe: 未接线
	// 时静默跳过,旧部署零感知。
	onTranscriptPublished func(roomID string)

	// gameStartedAt is the Unix timestamp (seconds) when the game started.
	// Used by agent prompts to inform agents of game duration context.
	gameStartedAt int64

	// 2026-08-10 §20260810-12 D2 — 死者身份「终局延时揭晓」配置(可选 0/5/15 分钟)。
	// 由 RoomConfig.DeathRevealDelayMin 创建房间时一次性写入;前端 SettlementModal
	// 据此在终局后倒计时揭晓死者的 role 字段。仅影响 UI 层显示时机,§135
	// RolePubliclyRevealed 单点判定不受影响。0 = 立即揭晓(零回归)。
	deathRevealDelayMin int

	// §20260811-09 U2 — Agent 难度分级房间级配置(easy/normal/hard/hell)。
	// 由 RoomConfig.AgentDifficulty 创建房间时一次性写入;空串 = normal(默认)。
	// 影响 4 个生产注入点(§130 接线验证):
	//   1) StartAgentsLocked 经 ProfileFor 计算 ag.DifficultyDirective + 假说/记忆门控;
	//   2) buildAgentContextLocked 末尾按 InjectHypotheses 决定 gc.HypothesisTable;
	//   3) EmitGameOver 调 settleBotsAfterGameOver / settleHumansAfterGameOver 时
	//      经 difficultyCoinMultiplierX10Locked 缩放胜方 delta(败方扣款不变)。
	//   4) view.go BuildClientStateWithRoom 下发 cs.AgentDifficulty,前端 UI 徽章渲染。
	// 归一化通过 NormalizeAgentDifficulty 在 SetAgentDifficultyLocked 内完成。
	agentDifficulty string

	// §20260811-09 U1 — AI 实时解说房间级配置(观战模式 🎙️ 解说席)。
	// commentaryDesired=true 时 StartAgentsLocked 末尾调用 startCommentatorGoroutine
	// 注入 Provider + 启动 goroutine;commentaryStyle("pro"/"fun") 决定 prompt 风格;
	// commentaryModelKey 空时回退 JudgeModelKey。全部访问须持 r.mu。
	commentaryDesired    bool
	commentaryStyle      string
	commentaryModelKey   string
	commentator          *wwcommentator.CommentatorAgent
	commentaryEvents     chan wwcommentator.CommentaryEvent
	commentaryCancel     context.CancelFunc
	commentaryFeed       []commentaryLine
	commentarySeq        uint64

	// §20260811-04 U2 — 人设倾向参数房间级配置。
	// 创建房间时由 RoomConfig.Personality* 一次性写入;StartAgentsLocked
	// 末尾 resolvePersonalityForSeatLocked 按 (mode, preset, custom) 装配。
	// 空字符串 / nil = 走默认(uniform + logical,保留旧行为零向量)。
	personalityMode       string // "uniform" / "random" / "custom"
	personalityPresetKey  string // "logical" / "emotional" / "aggressive" / "cautious" / "showman"
	personalityCustomVec  *wwplayer.PersonalityVector // custom 模式时填充 5 维向量

	// §20260811-06 U5 — 黎明流言系统房间级配置。
	// RumorsEnabled:bool 指针,nil = 走 cfg 全局默认;true/false 强制开关。
	// RumorCountPerDay:int 指针,每黎明阶段生成多少条(0/1/2);nil = 默认 2。
	// LastRumors:最近 5 条流言(FIFO),供 buildAgentContextLocked 拷贝到 GameContext。
	RumorsEnabled    *bool
	RumorCountPerDay *int
	LastRumors       []RumorEntry

	// §20260812-02 U3 — 观众押注竞猜系统。
	// spectatorBets 内存押注记录;betDB 异步持久化引用(由 WerewolfManager 注入)。
	spectatorBets map[string]*SpectatorBetRecord
	betDB         *gorm.DB

	// 2026-08-11 §20260811-05 U1 — 房间级玩家行为画像缓存。
	// modelKey → humanUserID → 画像条目;由 PrefetchPlayerProfiles 在
	// StartAgentsLocked / 人类入座后一次性预取(热路径零 DB 查询,§130 教训:
	// buildAgentContextLocked 里禁止 DB I/O)。生命周期 = 房间生命周期,
	// restartGameLocked 原地重开时保留(与 seatModelKeys 同级)。
	playerProfileCache map[string]map[string]playerProfileCacheEntry

	// 2026-08-13 §20260813-01 U2 — GameContext 分层缓存。
	// staticContextCache[seat] 缓存整局不变的静态信息(座位/角色/玩家列表),
	// 一局构建一次,游戏结束 GC 回收。phaseStateCache[seat] 缓存阶段内不变的
	// 信息(警长/屠边计数),阶段切换时失效重建。
	// 目的:减少 buildAgentContextLocked 每轮重复计算,每轮节省 3-5KB token。
	staticContextCache map[int]*wwtypes.StaticContext
	phaseStateCache    map[int]*wwtypes.PhaseStateContext
	phaseStatePhase    string // 当前缓存的阶段,phase 变化时失效

	// recentSpeeches is the room-wide rolling buffer of recent chat.message
	// events. Each entry projects the on-wire ChatMessage into an
	// wwtypes.SpeechEvent with seat / account / AgentName / IsBot / IsSpectator
	// fields so the LLM can tell speakers apart. The room manager appends
	// here from the chat hook (see WerewolfManager.SetChatService) and
	// buildAgentContextLocked reads from it for each agent's next decision
	// turn.
	//
	// BUG: 狼人杀 7 人局 Agent 多轮上下文 — was nil (no transcript at all).
	recentSpeeches []wwtypes.SpeechEvent

	// 2026-08-05 §02 — lastSpeechBySeat[seat] = 该座位最后一次**公开**发言。
	// 人机统一:bot 与真人都经由 appendRoomMessage 的公开分支写入,因此真人
	// 玩家座位卡也能拿到发言气泡(对照 bot_contexts 只覆盖 bot 座位)。
	//
	// 并发约定:与紧邻的 recentSpeeches **完全一致** ——
	//   - 写:appendRoomMessage,该函数**不持 r.mu**(见 room.go:727 既有说明);
	//   - 读:BuildClientState 系列,调用方**已持 r.mu**(§92a 锁内直读)。
	// 本字段沿用该既有约定,不新增任何锁、不改变现有锁序;不在本次范围内
	// 重新审视约定本身。
	//
	// 生命周期:与 recentSpeeches 同为房间级运行时缓冲,不进 DB,房间 GC 时释放。
	// 初始化:**惰性**(写入点判 nil 后 make),因此无需修改 WerewolfRoom 的
	// 各处结构体字面量构造点(room_manage.go / room_agent.go / room_action.go)。
	//
	// 私聊(whisper)不写入 —— 见 PlayerJSON.LastSpeech 的说明。
	lastSpeechBySeat map[int]seatSpeech

	// whisperInbox maps recipient seat → rolling list of private messages
	// addressed to that seat. Each entry mirrors a recentSpeeches row but for
	// private channel. The map is per-seat because whisper visibility is
	// recipient-scoped (sender / recipient / admin only) — a wolf teammate's
	// strategy must not leak to other bots.
	whisperInbox map[int][]wwtypes.WhisperEvent

	// §20260811-10 U1 — 照妖镜一次性强制真实身份标记位。
	// map[botSeat] → 标记「该 bot 下一次 LLM 调用 system prompt 必须追加
	// 真实身份指令」。消费后立即清空(避免重复注入)。生命周期 = 房间级,
	// restartGameLocked 原地重开时清零(与 wolf_pool / 狼队金币池对称)。
	//
	// 并发约定:写入由 prop_engine.UseProp 在持锁态完成;读取由
	// BuildSystemPrompt 类钩子在 agent_runner.go 完成。读路径不在持锁态,
	// 但 flag 是单 bit bool,读写竞争窗口窄且语义幂等(多扣一次 = 同一指令),
	// 接受此 race。
	mirrorExposeActive map[int]bool

	// §20260811-10 U2 — 心理侧写报告单帧缓存。
	// 最近一次 behavior_analyze 生成的 4 维报告(由 prop.behavior_report 帧
	// 推送给购买者后立即清空)。持锁写入;agent_runner 读取为零值即视为无报告。
	pendingBehaviorReport *BehaviorReportJSON

	// 2026-07-09 §13-bugfix — chatQueue 是**房间共享**的 500K 字节滚动聊天历史队列。
	// 此前(§13)实现是 per-seat 各自一个 Queue,7 bot × 500K = 3.5MB / 房间,
	// 且公平性靠 push 时机确保; 现改为单 Queue + 每 bot ReadPointer:
	//   - 内存从 3.5MB 降到 500K / 房间
	//   - Append 一次全员可见,推送逻辑极简
	//   - 公平性由"同队列 + 同 ReadPointer 序号"保证
	// 由 StartAgentsLocked 在第一个 bot 启动时分配;所有 bot 共享同一个队列引用,
	// 每个 bot 通过 SetReadPointer / Advance 跟踪自己的消费进度;stopAgentsLocked
	// 在房间 tear down 时清理。
	chatQueue *agentcore.ChatHistoryQueue

	// llmSema 是房间级 LLM HTTP 调用并发信号量(BUG-R242-P1-01)。
	// §130 曾删除以"让 bot 按模型自身响应速率自由调用",但 13 bot fully
	// concurrent × 内层重试打满上游代理 → 级联失败(实测 27min 66% 失败率)。
	// 恢复为有界信号量(默认 cap=4):槽位满时 bot 短暂等待后 reWake(瞬态,
	// 不计入 consecutiveFailures),既限制在途调用数,又不让慢模型无限阻塞快模型。
	// 由 StartAgentsLocked 在首个 bot 启动时创建,所有 bot 共享;stopAgentsLocked
	// 不销毁(房间复用),房间 GC 时随 struct 一起回收。
	llmSema chan struct{}

	// phaseWatchdog tracks the current phase+actingSeat "key" so the
	// background watchdog goroutine can detect a stalled phase (same key
	// for > phaseWatchdogDeadlineFor(seatCount)) and emit a forced skip.
	//
	// BUG-WEREWOLF-P0-NEW-42b (Round 38): a quarantined acting bot whose
	// skip fails (e.g. engine validation race with a concurrent wake), or
	// an agent goroutine that silently dies/stalls, leaves the phase
	// permanently stuck. The watchdog is a safety net that detects this
	// condition and dispatches the phase's safe skip action via the
	// manager, bypassing the stalled agent. Also provides heartbeat logs
	// every phaseWatchdogHeartbeatInterval for diagnostics.
	phaseWatchdog struct {
		key       string    // "phase/actingSeat" — the stalled indicator
		enteredAt time.Time // when this key was first observed
		lastLog   time.Time // last heartbeat log timestamp
		// skipCount 记录当前 key 上已派发的 watchdog skip 次数。
		// 用于 night_wolves 等"单座位投票 ≠ 阶段推进"的阶段:第一次超时可以
		// 给 stuck 座位派 wolf_kill skip 补一票,但若该座位因 LLM 持续失败
		// 永远投不出票,反复 skip 同一座位只是无限循环(R9 报告 §3.1)。
		// skipCount >= 1 时改为直接 tallyWolfVotes + endWolfPhase 强制推进。
		skipCount int
		// allQuarantinedTicks BUG-R221 (2026-08-01):连续多少个 tick 观察到
		// "房间内所有存活 bot 都已 quarantine 且无存活真人可行动"。任一 tick
		// 条件不成立立即清零。达到 allQuarantinedTripTicks 后由 watchdog 走
		// forceEndAllQuarantinedLocked 强制结束对局(否则房间永远"游戏中",
		// R221 实测某房间占用大厅席位 15+ 小时)。
		allQuarantinedTicks int
	}
	watchdogCancel context.CancelFunc // stops the watchdog goroutine on room teardown

	// quarantineSkipDepth counts how many recursive calls into
	// tryDispatchQuarantinedActingSkip have happened while holding r.mu.
	// The dispatchQuarantinedSkipLocked path can call wakeActingAgentsLocked,
	// which itself may try to dispatch another skip for the next acting seat.
	// If the same seat is re-elected as acting (e.g. driver whose vote still
	// leaves it as acting because the others already voted) the chain recurses
	// forever — that is exactly what produced the
	// "runtime: stack overflow (1427747+ frames)" SIGQUIT in the R38 logs.
	//
	// BUG-WEREWOLF-P0-NEW-43 (Round 38, 2026-07-08): full stack trace was
	//   tryDispatchQuarantinedActingSkip -> dispatchQuarantinedSkipLocked
	//   -> dayVoteLocked -> wakeActingAgentsLocked
	//   -> tryDispatchQuarantinedActingSkip -> ... 1000000+ frames
	// The fix caps the depth at quarantineSkipDepthLimit; beyond that we
	// return false (no skip dispatched) and emit a single ERROR-level log
	// so the phase watchdog (90s) can take over, rather than burning the
	// goroutine stack to death. Strictly monotonic per WakeAllAgents /
	// WakeActingAgents / notifyQuarantine entry — incremented on entry,
	// decremented on return via defer.
	quarantineSkipDepth int

	// BUG-R48-P0-3: quarantine-skip 递归深度溢出 (depth=51)。
	// 原因: dayVoteLocked → wakeActingAgentsLocked → tryDispatchQuarantinedActingSkip
	// 对同一 seat 反复派发 vote_skip, 形成自循环。
	// 修复: 用 skippingSeats 记录当前 phase 已派发过的 seat, 重入时直接返回。
	// phase 变化时自动清空 (lastSkipPhase 跟踪)。
	skippingSeats map[int]bool
	lastSkipPhase string

	// BUG-R48-P0-4: gameOverNotified 防止 onGameOver 回调被重复触发。
	// watchdog tick 每 5s 执行一次, 首次检测到 Status=="over" 时置 true。
	gameOverNotified bool

	// 2026-07-10 §125 增强 — 法官「整局总结」状态。
	judge       *wwjudge.AgentJudge
	judgeCancel context.CancelFunc
	judgeEvents chan wwjudge.JudgeEvent

	// 2026-07-16 主持人重构 — 房间级法官设置(由创建者透传)。
	// JudgeDesired=true 表示启用法官;现在两选项(agent/human)都使之为 true,
	// 仅运维级 cfg.Werewolf.JudgeMode="off" 或 0 Agent 时为 false。
	JudgeDesired bool
	// 2026-07-30 §重构 — 房间级法官模式字符串("agent"|"human"),由创建者透传。
	// 当前后端行为:两值都启用 AgentJudge LLM(真人法官路径待实现);字段保留
	// 是为了未来真人法官 WS 契约(game.judge_announce 等)落地后零侵入分流。
	JudgeMode string
	// JudgeModelKey 是创建者指定的法官模型 key;空=服务端随机分配。
	JudgeModelKey string

	// lastJudgePhase 追踪上一次已投递给法官的 phase,用于 phaseWatchdogTick 检测
	// 阶段切换时唤醒法官(初值 Phase(-1) 哨兵,避免首 tick 误触发)。
	lastJudgePhase Phase

	// 2026-07-10 §125 增强 — 模型记忆持久化。
	modelMemories map[string][]string
	memoryMutex   sync.Mutex
	lastSummary   string

	// 2026-07-12 §129 增强 — 冷却期状态。一局结束 (Phase=PhaseGameOver +
	// Status="over") 后, 不立刻走 onGameOver / forceCloseRoomLocked /
	// tryEnterRestartVoteFromGameOverLocked,而是先进入冷却期,让人类玩家与
	// 观察者有足够时间复盘; 冷却期 cooling watchdog 探测到最后一名人类
	// 离开起超过 cfgWerewolfCoolingSec() 秒仍无人加入后才强制关门。
	// coolingSince 是本次冷却期起算时间(首次 Status="over" + PhaseGameOver 时
	// 被设置); coolingEmptySince 是"最后一名人类离开"的时间(有人类时清零,
	// 无人类时记录)。coolingDone=true 表示冷却期结束,已过渡到 force-close。
	// coolingCancel 是 cooling watchdog goroutine 的 cancel,stopAgentsLocked 时清算。
	coolingSince      time.Time
	coolingEmptySince time.Time
	coolingDone       bool
	coolingCancel     context.CancelFunc

	// §20260811-08 U2 — 终局奖励发放幂等标志。
	//
	// 旧版 grantSettlementRewardsLocked 只在 forceCloseRoomLocked 一条路径被调用,
	// 而 EmitGameOver 有 4 个生产调用点(room_watchdog.go:184/205 +
	// room_restart_vote.go:136/353) —— §129 冷却期(最常见的终局路径)漏发。
	// 本批次把发放收口进 EmitGameOver 自身,由本标志保证每局仅发一次。
	//
	// §129 冷却期 + 重开局会让同一 room 对象经历多局,restartGameLocked 必须
	// 重置本标志(对照 resetCoolingStateLocked 的既有模式),否则第二局起永不发奖。
	settlementRewarded bool

	// §130 增强(2026-07-13):人类等待窗口字段。
	// 房间里有 Agent + 真人玩家/观察者时,StartGame 之前先进入等待窗口;
	// 人类可在聊天室自由发言,Agent 启动后第一轮 LLM 会吸收这些发言作为开局上下文。
	humanWaitDeadlineAt    time.Time          // 等待窗口 deadline (Unix 秒);零值 = 未启用
	humanWaitCancel        context.CancelFunc // humanWaitWatchdog goroutine 的 cancel
	humanWaitBroadcastSent bool               // game.pre_wait 帧是否已广播过(防止重复广播)

	// 2026-07-14 BUG-R116-03 — 同一座位单轮发言冷却,防止单个 Agent 刷屏。
	// key=seat; value=该座位最近一次成功公开发言的时间。
	seatLastPublicSpeak map[int]time.Time

	// 2026-07-15 BUG-R124-UI-001 — 单座位每阶段发言次数统计。
	// key=seat; value=该座位在当前发言阶段已成功公开发言次数。
	// 阶段切换 / 房间重启 / restartGameLocked 时清零。
	// 与 seatLastPublicSpeak 配合:同一座位单阶段 ≥ MaxSpeaksPerPhasePerSeat
	// 时直接拒绝新发言,避免 Qwen3.7-Max 等快模型占据 40%+ 发言量。
	seatSpeakCountThisPhase map[int]int
	seatSpeakCountPhaseTag  string // 当前阶段的 tag(PhaseSpeak / PhaseFirstNightSpeak 等),用于检测阶段切换清零

	// 2026-07-24 优化:UI 暂停字段。
	// paused=true 时:
	//   - 所有 Agent 不调 LLM(防批量 quarantine)
	//   - 阶段时钟冻结(不强制 skip)
	//   - watchdog 跳过死锁检测
	// 仅房主/admin 可设置;GameInfoPanel 提供 ⏸ / ▶ 按钮。
	// 不持久化,重启房间即解除(继续 running)。
	paused       bool
	pausedBy     string
	pausedAt     time.Time
	pausedReason string

	// 2026-07-21 道具系统 — 房间级道具状态。
	// propPotBonus 是本局道具消耗回滚到彩池的总额（50% 部分），结算时按比例发放给胜方。
	propPotBonus int64
	// propCooldown 记录每个座位最近一次使用道具的时间（用于冷却判定）。
	propCooldown map[int]time.Time
	// propCount 记录每个座位本局已使用道具次数（用于购买上限判定）。
	propCount map[int]int
	// propSeatBudget 记录每个座位本局道具消耗累计金币（用于个人预算/结算审计）。
	propSeatBudget map[int]int64
	// roomPropBudgetUsed 是本局所有玩家道具消耗累计金币（全局预算 = roomPropBudget）。
	roomPropBudgetUsed int64
	// roomPropBudgetOverride 是测试覆盖钩子（非 0 时优先于配置）。
	roomPropBudgetOverride int64
	// propCatalog 是运行时道具目录的引用（由 manager.SetPropCatalogForRoom 注入），
	// 供 buildAgentContextLocked 填充 PropSnapshot + apply effects 使用。
	propCatalog *PropCatalog
	// v3 §G3 — 道具使用引擎引用(由 manager.SetPropEngine 同步注入),
	// 让 buildAgentContextLocked 能调 walletSvc.GetBalance 填充 WalletBalance。
	propEngine *PropEngine
	// propInjectQueue 是待注入到目标 Agent GameContext 的道具注入文本队列。
	// key=目标座位; value=注入文本列表。buildAgentContextLocked 消费此队列。
	propInjectQueue map[int][]PropInjectEntry

	// 2026-07-22 v4 §13 P2 补缺 (R176 P2) — 链式效果延迟调度表:
	// key=scheduleKey(PropInjectEntry.ScheduleKey); value 包含到期回合 + target seat + 单个 step。
	// 在 phaseWatchdogTick / buildAgentContextLocked 中按 due_round 触发,
	// 触发后调 ApplyEffects 把该 step 应用到对应 GameContext。
	propEffectSchedule map[string]PropEffectScheduledItem
	// propEffectRoundCounter 记录 LLM 调用的累计轮数(用于 DelayTurns 倒计时)。
	propEffectRoundCounter int

	// 2026-07-21 v3 §G5 — 道具使用公开历史环形 buffer（最近 20 条）。
	// 由 recordPropHistoryLocked 写入,GetPropHistoryLocked 读取;
	// Agent 通过 prop_history 工具查询(给 LLM 决策辅助)。
	propHistory     []PropHistoryRecord
	propHistoryHead int

	// 2026-08-07 §20260807-04 P2-2 — 上一轮每个座位被道具击中的道具 key。
	// buildAgentContextLocked 在防御性重置前把上一轮 PropLastEffect 转存到此 map,
	// 下一轮填入 gc.PropHitLastRound,PropEffectSignalBlock 渲染「上一轮被击中」提示。
	lastPropHitEffect map[int]string

	// 2026-07-21 v4 §13.1 — 狼人小队交流通道（wolfpack_room）。
	// 仅狼人阵营 Agent 可访问;WolfPackRoom.Append 在锁内写、Snapshot 在
	// buildAgentContextLocked 拼入狼 bot user prompt。killPlayer 调
	// WolfPack.PurgeByDeath 清理死亡狼人的留言。
	wolfPack *WolfPackRoom

	// §20260812-03 U2 — 私下通道(SecretLetterRoom)。白天 speak→vote 窗口内
	// 玩家可发送 ≤200 字 / 每日 5 条短消息到任意非自己非死亡玩家。
	// §119 三重隔离(不入 chat_message 表/chat_history 队列/BotTranscript.HeartThought);
	// 仅 SecretLetterPanel 渲染自己收发的信件。
	secretLetter *SecretLetterRoom

	// §20260811-04 U1 — 狼队暗号系统 CipherProtocol。
	// WolfPackCipher 是房间级暗号索引,持锁路径配置。
	// 仅狼 bot GameContext 注入,§119 协议层隔离(同 WolfPackRoom msgs)。
	wolfPackCipher *WolfPackCipher

	// §20260811-07 U1 — 死后幽灵语音。
	// ghostVoiceEmitted 记录"已经推过幽灵语音的座位",1 次上限(防御性)。
	// restartGameLocked 清零(同 wolfPackCipher.Reset)。
	ghostVoiceEmitted map[int]bool

	// §20260811-07 U2 — 自动高光集锦战报生成。
	// battleReportTriggers 是 engine 关键节点累积的高光触发器,FIFO 16 上限。
	// battleHighlights 是异步生成的最终高光(顶层,供 view.go 透传前端)。
	// battleHighlightsByModelKey 按 modelKey 索引(同 ModelMemoryLocked 模式)。
	battleReportTriggers        []BattleReportTrigger
	battleHighlights            []HighlightMoment
	battleHighlightsByModelKey  map[string][]HighlightMoment

	// 2026-08-10 §20260810-05 — 信息账本(Information Ledger)一期。
	// 房间级单一事实源,记录「哪条信息在何时被哪些座位获知」。
	// 懒初始化(ledgerLocked),与 wolfPack 同模式,避免 6 处构造点同步遗漏。
	// 纯内存、不进 DB/chat 表;restartGameLocked 原地重开时清零(新一局信息重新累计)。
	// 一期「只写不读」:不影响 buildAgentContextLocked 的 prompt 组装。
	infoLedger *InformationLedger

	// 2026-08-10 §20260810-08 — 信息账本二期：观战者说漏嘴检测缓存。
	// leakCacheSeq 与 infoLedger.seq 相等时可直接复用；仅锁内访问。
	leakCache    []InfoLeak
	leakCacheSeq int64

	// 2026-08-10 §20260810-06 — 行为承诺与兑现追踪（CommitmentLedger）。
	// 懒初始化(commitmentLedgerLocked),与 infoLedger 同模式。
	// 纯内存、不进 DB;restartGameLocked 原地重开时清零。
	commitmentLedger *CommitmentLedger

	// 2026-08-10 §20260810-07 — 多假说并行推演（Multi-Hypothesis Tracker）。
	// 房间级 HypothesisStore:每 bot 维护"我对其他 12 名玩家的身份猜测"。
	// 懒初始化(hypothesisStoreLocked),与 infoLedger / commitmentLedger 同模式。
	// §128 对话即思考:LLM 在 LastDecisionSummary 末尾「📊 [...]」JSON 段提交,
	// 由 HypothesisStore.UpdateFromDecisionSummary 解析并写入。
	// §119 协议层隔离:不进 chat_message / chat_history。
	// restartGameLocked 原地重开时清零。
	hypothesisStore *HypothesisStore

	// 2026-08-11 §20260811-02 U1 — 发言影响力生态(Influence Score)。
	// 房间级 InfluenceTracker:每座位一个 0~100 的公开影响力分数(4 个公开信号加权)。
	// 懒初始化(influenceTrackerLocked),与 infoLedger / hypothesisStore 同模式。
	// §119 对照:影响力是**公开**信息(与 HeartThought 相反),刻意进 wire 全员可见。
	// §92a:重算入口只有 RecalculateInfluenceLocked(锁内变体,无公开变体)。
	// restartGameLocked 原地重开时清零。
	influenceTracker *InfluenceTracker

	// 2026-08-11 §20260811-03 U1 — 信息污染链(Rumor Graph)。
	// 房间级 RumorGraph:有向图边集合(from→to),每条边带 hop 毒性衰减+服务端权威真伪。
	// 懒初始化(rumorGraphLocked),与 infoLedger / hypothesisStore / influenceTracker 同模式。
	// §119 协议层隔离:谣言**绝不**入 chat_message / chat_history / BotTranscript.HeartThought,
	// 仅经 GameContext.RumorInbox 注入 agent user prompt;观战者侧 SettlementModal 单独渲染。
	// §92a:所有读写入口都是 *Locked 锁内变体,公开方法包一层加锁委托。
	// restartGameLocked 原地重开时清零。
	rumorGraph *RumorGraph
}

// PropHistoryRecord 是一条公开道具使用记录（v3 §G5）。
// 不含注入内容/隐藏任务/中招效果细节 — 只公开 from/to/prop_key/hit/effect_hint/phase/round。

// recordPropHistoryLocked 写入一条道具使用公开记录（环形 buffer，20 条上限）。
// 调用方必须持 r.mu。

// GetPropHistoryLocked 读取最近 N 条（按时间顺序，新→旧）。
// 调用方必须持 r.mu；limit ≤ 20，> 20 截断；0/-1 → 返回全部。

// GetPropHistoryForAPI 是 GetPropHistoryLocked 的导出版（用于 REST API）。
// 内部短时持锁读取后立即释放；limit ≤ 20,0 → 默认 10。

// PropInjectEntry 是待注入到 Agent GameContext 的道具注入条目。

// ParseEffectTypes 把逗号分隔的 effect_types 解析为切片。
// v4 起：若 Steps 非空,优先返回 step 链的 EffectType 序列,否则回退到 EffectTypes 解析。

// PropEffectScheduledItem 是 v4 链式效果调度表中的单条记录。
//   - DueAfterCalls:再经过 N 次 Agent LLM 调用后到期（来自 PropEffectStep.DelayTurns）。
//   - TargetSeat:效果应用到哪个座位（0-indexed）。
//   - FromSeat:触发来源（PropInjectEntry.FromSeat,用于情绪/身份注入）。
//   - Step:要落地的单 step。
//   - CreatedAtCall:调度创建时的累计轮数（用于延迟换算）。

// cfgWerewolfSameSeatSpeakCooldownSec 读取单座位单轮发言冷却秒数。

// cfgWerewolfMaxSpeaksPerPhasePerSeat 读取单座位单阶段发言次数上限(BUG-R124-UI-001)。
// 0 = 不限制(向后兼容)。

// cfgWerewolfRoomPropBudget 读取房间级道具全局金币预算（v2 重设计）。
// 0 = 不启用全局预算（仅保留个人上限 + 冷却）。
// 2026-07-21 v2 重设计（docs/狼人杀-道具与经济/狼人杀13人局道具系统设计.md §5.2）。

// cfgWerewolfWolfTeammateHintRate 读取"开局狼队友互认"概率百分比。
// 默认 30;0/-1 都视为禁用(本设计关);100 = 全部狼 bot 开局互认。
// 2026-07-21 §5.2。StartAgentsLocked 用此概率给每个狼 bot 决定是否注入
// "X 号是你的狼队友"identity 提示。

// cfgWerewolfWolfTeammateHintMaxPairs 读取"开局狼队友互认"每局最多几对(v3 新增)。
// 1 对 = 2 只狼互知;0/-1 都视为禁用;>= 狼总数时降级为最多狼总数/2 对。
// 2026-07-21 §G4 v3 增强。docs/狼人杀-道具与经济/狼人杀13人局道具系统设计.md §4.1。

// collectWolfSeatsLocked 收集本局所有狼人座位(0-indexed,内部用)。
// 调用方必须持 r.mu。StartAgentsLocked 用于 §5.2 狼队友互认注入;
// 派散到每个狼 bot 时,候选池 = 除自己外的全部狼人座位。

// allowSeatSpeakThisPhase 检查座位在当前发言阶段是否还能再发言。
// 当且仅当该座位当前阶段累计发言数 < 上限时返回 true,否则 false。
// 同时检查阶段是否变化(若变化则自动重置计数)。
//
// 这是 2026-07-15 BUG-R124-UI-001 的核心防御:防止单个 Agent 在同一发言
// 阶段(如 PhaseSpeak / PhaseFirstNightSpeak)反复补充发言,占据 40%+ 份额。
//
// 调用方语义: Speak / SpeakAuto / SpeakWithThought 三个入口都要先调本函数,
// 然后在 SendFromBot 成功后调 bumpSeatSpeakCountThisPhase 增加计数。
// 持锁时间 < 200ms(与 allowSameSeatPublicSpeak 一致)。

// bumpSeatSpeakCountThisPhase 在 SendFromBot 成功后增加该座位本阶段计数。
// 失败不抛错,仅 best-effort;锁竞争失败时下次调用时由 allowSeatSpeakThisPhase
// 在阶段切换时重新对齐。

// ─── 道具系统方法 (2026-07-21) ───

// propCooldownRemainLocked 返回座位距离下次可使用道具的剩余秒数（0 = 可用）。
// 必须在 r.mu 已持锁状态下调用。

// isPropCooldownLocked 检查座位是否在道具冷却中。

// propCountForSeatLocked 返回座位本局已使用道具次数。
// 必须在 r.mu 已持锁状态下调用。

// PropPerSeatSnapshotLocked 在 r.mu 已持锁状态下,回填某座位的道具 per-room 状态:
//   - *remaining = MaxPerGame - 已用次数(负值截断到 0)
//   - *cooldownSec = 距离下次可使用道具的剩余秒数(0 = 立即可用)
//
// 是读取侧的"前端 PropPanel / REST ListProps"权威数据源;无副作用。
//
// R173 之前 ListProps 只返 {props,total},前端 PropPanel 的余额/剩余/冷却全部
// 显示 0 — 修复:ListProps 内先调 RoomPropPerSeatSnapshot(短线持锁)进入本方法。

// RoomPropPerSeatSnapshot 是 PropPerSeatSnapshotLocked 的导出版 — 供 api 包
// (等外部包)短线持锁后回填 per-seat 道具状态。
// 持锁失败(例如 200ms 超时)时,*remaining/*cooldownSec 不被修改,返回 false。

// recordPropUseLocked 记录一次道具使用（冷却重置 + 计数累加 + 彩池累加 + 预算累加）。
// price 是本次消耗的道具完整价格（用于 v2 全局/个人预算累加）；potReturn
// 是回滚到彩池的部分（price 的 50%）。必须在 r.mu 已持锁状态下调用。

// enqueuePropInjectLocked 把道具注入文本加入目标座位的注入队列。
// buildAgentContextLocked 在构造 GameContext 时消费此队列。
// 必须在 r.mu 已持锁状态下调用。

// schedulePropEffectStepLocked 把一条 v4 链式效果 step 加入延迟调度表。
// 仅在 PropEffectStep.DelayTurns > 0 时调用,即时 step 走 ApplyEffects 直接落地。
// R176 P2 补缺：补回 v4 commit 描述的"效果链"延迟调度路径。
// 必须在 r.mu 已持锁状态下调用。

// tickPropEffectScheduleLocked 把到期的链式效果应用到目标 GameContext。
// 由 buildAgentContextLocked 入口处调用(增加 propEffectRoundCounter 并检查到期项)。
// 返回:已应用的 step 数量(用于日志)。必须在 r.mu 已持锁状态下调用。

// evaluatePropStepCondition 评估 v4 chain step 的 Condition 字符串。
//   - "always" / "" → 始终应用
//   - "target_alive" → 仅当目标在 ctx.AliveSeats 中
//   - "target_in_speak" → 暂等同于 always(发言阶段已在 buildAgentContextLocked 触发,
//     延迟 step 落地的具体 phase 校验留作 v4.1)
//   - 其他 / 不识别 → 默认 always(允许宽松扩展)

// drainPropInjectQueueLocked 消费并返回座位的待注入道具队列（取出后清空）。
// 必须在 r.mu 已持锁状态下调用。

// resetPropStateLocked 在 restartGameLocked / 房间重置时清零道具系统状态。
// 必须在 r.mu 已持锁状态下调用。

// roomTotalCoin 返回房间内所有存活玩家的钱包余额总和（v4 §13.2 经济档位判定入参）。
// 必须在 r.mu 已持锁状态下调用。返回 0 表示无钱包服务或房间内无余额数据。
//
// 实现：仅累加存活玩家的余额；人类 + Bot 都包含。r.propEngine 提供 walletSvc 句柄;
// 若 walletSvc 为 nil（如测试桩）则返回 0 → ComputeEconTier → EconDanger（最严档）。

// enqueuePropHitLocked 把一次命中的道具效果入队（注入文本 + 干扰信号）。
// 由 agent_runner.UseProp / ws handleWerewolfUseProp 在持锁后调用。
// effectTypes: 逗号分隔的效果类型列表；twistSeat: target_twist 引导座位（-1=无）。
// 必须在 r.mu 已持锁状态下调用。

// computeTwistSeatLocked 按道具的 TwistSeatSrc 计算 target_twist 的引导座位（v2）。
// 返回 -1 表示不引导。必须在 r.mu 已持锁状态下调用。
//   - "from_seat": 引导目标打使用者（fromSeat）。
//   - "random_enemy": 引导打目标所在阵营的随机敌对阵营玩家；找不到敌人 → -1。
//   - "most_trusted": 不指定具体座位（返回 -1），由注入文本的隐藏任务引导
//     "做决策时最想选的那个"（注意力失焦专用，实现"杀错人"）。

// cfgWerewolfHumanWaitSec 读取人类等待窗口秒数。
// 0 = 禁用等待窗口(默认全 AI 房间);60(默认) = 混合房间等待 60s。

// quarantineSkipDepthLimit is the maximum number of recursive
// tryDispatchQuarantinedActingSkip calls allowed in a single lock-held chain
// before we bail out. Empirically a healthy chain dispatches at most a
// handful of skips before the phase transitions; 50 is well above normal
// traffic but still bounded — every recursive call was identical (same seat,
// same skip) meaning a self-loop, which is exactly what we want to break.
// BUG-WEREWOLF-P0-NEW-43.

// cfgWerewolfRoomLLMConcurrency 读取房间级 LLM 并发上限(BUG-R242-P1-01)。
// 0 / 负值 = 禁用(完全并发,§130 行为,仅用于调试)。见 room_config.go。

// cfgWerewolfJudgeMode 2026-07-10 §125 增强 — 读取法官模式。

// cfgWerewolfJudgeModelKey 2026-07-10 §125 增强 — 法官使用的 LLM model_key。

// judgeKindForPhase 把对局 phase 映射为法官唤醒事件 kind(对齐 docs/狼人杀-重构方案/主持人Agent重构设计.md
// §6.3 映射表)。秘密阶段(NightWolves/Seer/Witch)返回空字符串 → phaseWatchdogTick 不调
// wake,法官在夜间静默观察。

// cfgWerewolfEnableModelMemoryRecap 2026-07-10 §125 增强 — 是否注入上一局记忆。

// ─────────────────── 观战者 Wake (2026-07-08 §13) ───────────────────

// spectatorWakeInterval 是同一房间内"观战者发言触发 Agent wake"的最短间隔。
// 复用 agentcore.SpeakLimiter(token bucket)。15 秒 — 与 §13.6 限流矩阵对齐。

// spectatorWakeAllowedPhases: 观战者公开消息允许触发 Agent wake 的阶段白名单。
// 夜间 / dawn / hunter_shoot / gameover 不在白名单 — 防止夜间信息泄露 + 减少
// 无意义 LLM 调用。night_wolves/seer/witch 即使没 wake,消息也会进入
// r.recentSpeeches,Agent 在下一 phase wake 时自然读到(延迟 60-180s)。
//
// 2026-07-08 §13.4 阶段限制。Phase.String() 的输出形如 "speak"/"pre_wolves",
// 兼容两种 phase 标识。

// ─────────────────── Phase Watchdog ───────────────────

// phaseWatchdogTickInterval is how often the background watchdog goroutine
// checks whether the current phase+actingSeat is stalled.

// phaseWatchdogDeadlineFor returns the wall-clock time after which a
// phase+actingSeat combination is considered permanently stuck. 2026-07-12
// §LLM-超时调整: 伴随后端 LLM 调用超时提升到 5 分钟,watchdog 兜底门限必须
// 显著高于单次 LLM call 上限。R131 按 seatCount 缩放:7 人局 240s,13 人局 360s,
// 给大房间更多等待时间。

// phaseWatchdogWarningInterval controls how often the watchdog emits a WARN-
// level heartbeat log when a phase is approaching the skip deadline. Logs at
// most once per interval per stalled phase to avoid log spam.

// SeatOf 返回 userID 所在座位,未入座返回 NoSeat。
// SetOnTranscriptPublished registers a callback that fires (in a goroutine)
// after an agent publishes a fresh BotTranscript. The ws layer wires it to
// broadcast game.state so the frontend seat card EmotionAvatar refreshes
// emotion + fx in real time (2026-08-05 §表情实时同步). nil-safe: 未接线时
// 静默跳过,旧部署零感知。
func (r *WerewolfRoom) SetOnTranscriptPublished(cb func(roomID string)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onTranscriptPublished = cb
}

// SetDeathRevealDelayMin 设置 §20260810-12 D2 死者身份「终局延时揭晓」配置(0/5/15)。
// 由 RoomService.CreateRoomWithAgents 在房间创建时一次性调用;锁内变体,公开
// 入口包锁委托。非法值自动归一化为 0(零回归)。仅影响前端 UI 层,SettlementModal
// 倒计时;§135 RolePubliclyRevealed 单点判定不受影响。
func (r *WerewolfRoom) SetDeathRevealDelayMin(min int) {
	r.setDeathRevealDelayMinLocked(min)
}

// SetAgentDifficulty 设置 §20260811-09 U2 Agent 难度分级配置(easy/normal/hard/hell)。
// 由 RoomService.CreateRoomWithAgents 在房间创建时一次性调用;锁内变体,公开
// 入口包锁委托。非法 / 空值自动归一化为 normal(零回归)。
func (r *WerewolfRoom) SetAgentDifficulty(difficulty string) {
	r.setAgentDifficultyLocked(difficulty)
}

// setAgentDifficultyLocked 锁内变体(§92a)。调用方必须已持 r.mu。
func (r *WerewolfRoom) setAgentDifficultyLocked(difficulty string) {
	r.agentDifficulty = string(NormalizeAgentDifficulty(difficulty))
}

// DifficultyCoinMultiplierX10Locked 返回当前房间难度档位对应的胜方金币倍率(×10)。
// 必须在持有 r.mu 时调用(§92a);由 EmitGameOver 内 settleBots/settleHumans
// 读取作为结算因子。败方扣款不受倍率影响 —— 仅胜方收益被放大。
func (r *WerewolfRoom) DifficultyCoinMultiplierX10Locked() int64 {
	return int64(ProfileFor(AgentDifficulty(r.agentDifficulty)).CoinMultiplierX10)
}

// SetDeathRevealDelayMinLocked 锁内变体(§92a)。调用方必须已持 r.mu。
func (r *WerewolfRoom) setDeathRevealDelayMinLocked(min int) {
	switch min {
	case 0, 5, 15:
		r.deathRevealDelayMin = min
	default:
		r.deathRevealDelayMin = 0
	}
}

// §20260811-04 U2 — 人设倾向参数 setter。
// 由 RoomService.CreateRoomWithAgents 在房间创建时一次性调用。
// mode/presetKey 非枚举值自动归一化为默认(uniform + logical);
// customVec 仅 mode="custom" 时使用,其他模式忽略。
func (r *WerewolfRoom) SetAgentPersonality(mode, presetKey string, customVec *wwplayer.PersonalityVector) {
	r.setAgentPersonalityLocked(mode, presetKey, customVec)
}

// setAgentPersonalityLocked 是 SetAgentPersonality 的锁内变体(§92a)。
func (r *WerewolfRoom) setAgentPersonalityLocked(mode, presetKey string, customVec *wwplayer.PersonalityVector) {
	switch mode {
	case PersonalityModeUniform, PersonalityModeRandom, PersonalityModeCustom:
		r.personalityMode = mode
	default:
		r.personalityMode = PersonalityModeUniform
	}
	switch presetKey {
	case "logical", "emotional", "aggressive", "cautious", "showman":
		r.personalityPresetKey = presetKey
	default:
		r.personalityPresetKey = "logical"
	}
	if r.personalityMode == PersonalityModeCustom && customVec != nil {
		clamped := customVec.Clamp()
		r.personalityCustomVec = &clamped
	} else {
		r.personalityCustomVec = nil
	}
}

// PersonalitySnapshotLocked 返回房间级人设配置(供 view.go / 前端展示)。
// 返回值是 (mode, preset_key, custom_vector 副本);调用方不持有 r.mu。
func (r *WerewolfRoom) PersonalitySnapshotLocked() (string, string, *wwplayer.PersonalityVector) {
	if r == nil {
		return PersonalityModeUniform, "logical", nil
	}
	if r.personalityMode == "" {
		return PersonalityModeUniform, "logical", nil
	}
	if r.personalityMode == PersonalityModeCustom && r.personalityCustomVec != nil {
		vecCopy := *r.personalityCustomVec
		return r.personalityMode, r.personalityPresetKey, &vecCopy
	}
	return r.personalityMode, r.personalityPresetKey, nil
}

// cipherLocked 返回房间暗号索引,懒初始化(与 wolfPack/informationLedger 同模式)。
// §92a 锁约束:调用方必须已持 r.mu。
func (r *WerewolfRoom) cipherLocked() *WolfPackCipher {
	if r.wolfPackCipher == nil {
		r.wolfPackCipher = NewWolfPackCipher()
	}
	return r.wolfPackCipher
}

// WolfPackCipherSnapshotLocked 返回暗号索引(供 buildAgentContextLocked / view.go 透传)。
// nil room 安全;非 nil room 返回懒初始化后的索引。
func (r *WerewolfRoom) WolfPackCipherSnapshotLocked() *WolfPackCipher {
	if r == nil {
		return nil
	}
	return r.cipherLocked()
}

// ResetWolfPackCipherLocked 清空暗号索引(restartGameLocked 重开局时调用)。
func (r *WerewolfRoom) ResetWolfPackCipherLocked() {
	if r == nil || r.wolfPackCipher == nil {
		return
	}
	r.wolfPackCipher.Reset()
}

func (r *WerewolfRoom) SeatOf(userID string) (Seat, bool) {
	for i, u := range r.Seats {
		if u == userID {
			return Seat(i), true
		}
	}
	return NoSeat, false
}

// Occupied 返回已入座人数。
func (r *WerewolfRoom) Occupied() int {
	n := 0
	for _, u := range r.Seats {
		if u != "" {
			n++
		}
	}
	return n
}

// IsReady 报告是否本局人数(SeatCount)已满,可开局。
// 默认 SeatCount = MaxPlayers(12);werewolf_7 房间由 SetSeatCount 设为 7。
func (r *WerewolfRoom) IsReady() bool {
	n := r.SeatCount
	if n <= 0 {
		n = MaxPlayers
	}
	return r.Occupied() == n
}

// IsOwner 判定 userID 是否是房主(1 号位玩家)。
// 2026-07-24 优化:UI Pause/Resume 等管理操作仅房主可执行;
//   - 房主 = Seats[0] 对应的 userID
//   - 房主座位空时,任何人都不可暂停(避免被抢占)
//
// 必须在持 r.mu 锁状态下调用(读 Seats)。
func (r *WerewolfRoom) IsOwner(userID string) bool {
	if userID == "" || r == nil {
		return false
	}
	if r.Seats[0] == "" {
		return false
	}
	return r.Seats[0] == userID
}

// IsPaused 当前房间是否处于暂停状态。
// 持 r.mu 锁调用。watchdog / wake 路径必须先用此检查再决定派发。
func (r *WerewolfRoom) IsPaused() bool {
	if r == nil {
		return false
	}
	return r.paused
}

// ─────────────────── Manager ───────────────────

// WerewolfManager 管理所有活跃的狼人杀房间。

// AgentSeatInfo is the persistence-side view of an agent seat, used by the
// hydrator callback. Mirrors service.AgentSeatInfo but lives here to avoid an
// import cycle (service → ws → werewolf → service).

// SetHydrator installs the room-restoration callback. Wire this up after both
// the werewolf manager and the room service are constructed (typically in
// main.go's bootstrap sequence). Passing nil disables restart recovery.

// SetOnSheriffStreamSettle 注册警徽流结算回调。
// 13 人标准竞技局(docs/狼人杀13人标准局规则.md §7.4):engine 在 dawn 结算警徽流后通过此回调
// 委托 ws 层 BroadcastRoom(game.sheriff_stream_settle),避免 engine 反向依赖 hub。
// nil-safe:旧部署不接也不影响(结算逻辑仍运行,仅不广播)。

// SetOnIdiotRevealed 注册白痴翻牌结算回调。
// 13 人标准竞技局(docs/狼人杀13人标准局规则.md §3.5):engine 在白痴翻牌结算后通过此回调
// 委托 ws 层 BroadcastRoom(game.idiot_revealed),避免 engine 反向依赖 hub。
// nil-safe:旧部署不接也不影响(结算逻辑仍运行,仅不广播)。

// SetOnPropUsed 注册道具使用专用广播回调。
// 2026-07-23 §道具特效:engine 在 broadcastPropUseLocked 内(持 r.mu)通过此钩子委托
// ws 层 BroadcastRoom(game.werewolf_prop_used),把道具使用的完整信息
// (from/target/prop_key/emoji/hit/effect)以独立帧推送给前端。
// 此前端帧与 chat.activity(prop_used) 并存:前者驱动 PropUseOverlay 特效,
// 后者进 500K 聊天队列/活动流。nil-safe:旧部署不接也不影响。

// SetOnGameStarted installs the callback invoked after a successful StartGame.
// The callback should update the DB room status from "open" to "playing".
// Wire this up after both the werewolf manager and the room service are
// constructed (typically in main.go's bootstrap sequence).

// BUG-R48-P0-4: SetOnGameOver installs the callback invoked after the engine
// detects a winner. The callback should update the DB room status from
// "playing" to "over". Wire this up in main.go alongside SetOnGameStarted.

// SetRecordLogService 注入 RecordLogService。nil 时清空。2026-07-10 §4
// model_game_log hook 的核心入口;StartAgentsLocked 会在创建每个
// wwplayer.Agent 时把此引用注入 a.RecordLog。

// SetCoolingHumanPresence 注入冷却期人类在线探针。2026-07-12 §129 增强。
// 非 nil 时一局结束后冷却 watchdog 每 coolingTickInterval 调用一次, 探测
// 是否仍有任何人类玩家 / 观察者在房间里; nil-safe(nil 时永不强制关门,
// 仅作兜底保护避免冷却期逻辑因缺依赖而 panic)。
// 典型实现: hub.IsRoomEmpty 取反(向 main.go 传入闭包)。

// cfgWerewolfCoolingSec 2026-07-12 §129 增强 — 读取冷却期秒数。
// 默认 1800(30 分钟)。0 时冷却期逻辑由 watchdog 内部判断跳过。

// SetWalletService 注入 WalletService。EmitGameOver 在对局结束时对所有
// bot + 人类玩家结算金币（底注彩池制：胜方分输家底注 / 负方输底注 / 平局 0）,
// 走 Credit / Debit + ledger 双簿记。2026-07-10 §4,2026-07-15 升级彩池制。

// SetBalancePusher 注入 WS 金币推送回调（通常注入 ws.Hub.PushBalanceChange
// 方法值）。结算成功后调用,向用户所有连接推送 wallet.balance 帧,前端
// useWallet hook 自动订阅并刷新余额显示。nil-safe:nil 时跳过推送,
// 结算仍生效,前端下次 HTTP 拉取时补齐。2026-07-15 金币系统。

// SetSettlementPusher 注入 WS 结算明细推送回调（通常注入 main.go 传递的
// per-user 派生方法,内部走 ws.Hub.SendToUser）。结算成功后调用,向该人类玩家推送
// game.settlement 帧,前端 WerewolfGamePage 据此渲染 SettlementModal。
// nil-safe:nil 时跳过推送,结算仍生效。

// SetChatService registers the ChatService the agent runners use to send bot
// speech. Must be called before agent seats are started. Safe to call with nil
// (bots will see "chat unavailable" errors).

// ─── 道具系统 Wire 方法 (2026-07-21) ───

// SetPropCatalog 注入运行时道具目录。由 main.go 在 wire 时调用。

// SetPropEngine 注入道具使用引擎。由 main.go 在 wire 时调用。

// broadcastPropUseLocked 广播道具使用事件(公开信息)。
// 必须在 r.mu 已持锁状态下调用。2026-07-21 §道具系统:走 activityEmitter
// 路径发送 EmitRoomActivity(kind=prop_used),与 EmitVoteResult / EmitWolfKill
// 一致 — 同一活动流,前端 ActivityDrawer / GameChatPanel 都能看到。
// 2026-07-23 §道具特效:若注册了 onPropUsed 回调,还会发送独立的
// game.werewolf_prop_used 帧(from/target/prop_key/emoji/hit/effect)。
// 此前端帧驱动 PropUseOverlay 特效;与 chat.activity(prop_used) 并存。
// 锁安全:本函数仅调 emitActivity + onPropUsed(均 nil-safe),不获取新锁,
// 持 r.mu 调用安全(§92a)。

// streamChatSvc 是 2026-07-12 §127 聊天 SSE 流式解析需要的极小 chat 广播接口。
// 通过 duck-type 避免 werewolf → ws 引入循环依赖;*ws.ChatService 已实现。
// 仅供 WerewolfRoom.StartAgentsLocked 在 agent 构造后接线 Agent 的 onLLMStream* 回调。

// ChatMessageLike is the minimal subset of ws.ChatMessage the werewolf
// manager needs to record a transcript entry. Implemented structurally by
// ws.ChatMessage so callers can pass one in directly.

// ChatActivityEvent mirrors ws.ActivityEvent for the werewolf manager.
// Structural compatibility is required because main.go converts via
// werewolf.ChatActivityEvent before calling RecordRoomActivity.
//
// 2026-07-09 §13 增强 §115 房间聊天 — see docs/狼人杀-Agent与系统/狼人杀房间聊天设计.md.

// RecordRoomMessage is the per-manager dispatcher for incoming room chat
// events. main.go wires the chat service's onRoomMessage hook to call this
// method directly (see main.go). It looks up the room and forwards to the
// room's append method, which appends to the rolling buffers used by
// buildAgentContextLocked.
//
// 2026-07-08 §13: 观战者公开消息额外触发 maybeSpectatorWake,让 7 个 Agent
// 在 ≤15s 内被 wake 后通过 LLM 决策回应(可选 interject/whisper/idle_think)。
//
// BUG: 狼人杀 7 人局 Agent 多轮上下文 — without this dispatcher the
// transcripts buffer stays empty and the LLM never sees prior rounds.

// RecordRoomActivity is the activity-event counterpart of RecordRoomMessage.
// It is wired through ChatService.SetRoomActivityHook in main.go and decides
// whether the event should also land in the per-bot 500K chat queue.
//
// 2026-07-09 §13 增强 §115 房间聊天 — see docs/狼人杀-Agent与系统/狼人杀房间聊天设计.md.
// silent_for_bots=true events are pure UI cues ("system auto-skip") and must
// NOT pollute the LLM context. silent_for_bots=false events (e.g. "wolf kill",
// "vote result") are also written into the queue so the LLM sees them.
//
// The event itself is not persisted to t_lsm_game_chat_message — only
// game.state.phase / round_number / players[].alive are the replay source.

// appendRoomMessage writes a single chat event into the room's rolling
// buffers. Public-by-method-name (lowercase first letter = unexported but
// callable from same package) and called with the room lock held.
//
// Two cases:
//   - whisper: addressed to a specific seat; routed to that seat's inbox
//     (no global append — whisper is private and must not leak).
//   - public: appended to recentSpeeches, with the seat resolved from
//     FromUserID when the sender is a player.

// appendToChatQueueLocked 把一条 chat 消息写入房间共享的 500K 队列。
// 调用方必须持 r.mu。
//
// 2026-07-09 §13-bugfix 改造: 由"per-seat 多次 Append"改为"单 Queue + 单 Append"。
// Append 一次后所有 bot 通过 ReadPointer 各自消费,因此本函数不再需要
// 按 recipient 拆分推送。IsWhisper 字段仍写入消息本身,LLM prompt 渲染时
// 通过 IsWhisper + ToSeat 决定该 bot 是否能"识别"该私聊内容。
//
// 若队列未分配(r.chatQueue == nil)则静默返回 — 不阻塞主路径。

// appendActivityToChatQueueLocked 把一条活动事件以 ChatMessage 形式注入房间共享
// 500K 队列。Caller must hold r.mu. 2026-07-09 §13-bugfix 改造: 由"per-seat 多次
// Append"改为"单 Queue + 单 Append",活动事件对所有 bot 一视同仁(由 ReadPointer
// 决定每个 bot 是否已经看过该事件)。
//
// 设计动机:
//   - LLM 决策需要"现在处于什么 phase / 谁刚出局"等元信息;
//   - 把它写进 500K 队列,LLM 在下一次 LLMCall 时通过 system prompt 的
//     chatHistory 段看到;
//   - 标记 IsActivity=true + EventKind + Severity,让 prompt 渲染区分
//     普通发言 vs 系统事件;
//
// 活动事件不是 whisper,IsWhisper=false;所有 bot 都能看到(由 ReadPointer 公平消费)。

// msgIDForChat 生成 chat history 队列消息 ID(prefix 区分公开/私聊)。

// maybeSpectatorWake 是 §13 观战者互动的核心触发点。由 RecordRoomMessage 在
// 收到 `FromRole == "spectator"` 的公开消息后调用。
//
// 行为:
//  1. 阶段白名单(spectatorWakeAllowedPhases)— 夜间 / dawn / hunter_shoot /
//     gameover 拒绝 wake,防止夜间信息泄露。
//  2. 对所有存活非 quarantine bot 推 AgentEvent{Kind:"spectator_speech"} —
//     每个 bot 各自从 channel 读到事件后在 runLoop handleEvent 决策。
//
// 2026-07-09 §13 增强:移除 15s 滑动窗口节流(`cfgWerewolfSpectatorFullWake`
// 默认 true → 全阶段全频唤醒;false 时回退旧行为),让观众的每条公开消息
// 都即时唤醒 7 个 bot。
// §130 重构(2026-07-13):LLMCallLimiter 已删除 — 取消单 bot 最小调用间隔锁定。
//
// 锁策略:appendRoomMessage 不持 r.mu;但 wakeAllAgentsLocked 内部用 BotAgents
// map 迭代,需要 r.mu。这里在持锁状态下 PushEvent — PushEvent 内部只用 atomic
// 读 events 通道(channel send 是 lock-free),所以不会重入 r.mu。
//
// 2026-07-08 §13.4 / §13.6 / Round 39 §94.
func (m *WerewolfManager) maybeSpectatorWake(r *WerewolfRoom) {
	if m == nil || r == nil {
		return
	}
	if r.State == nil {
		return
	}
	phaseStr := r.State.Phase.String()
	if !spectatorWakeAllowedPhases[phaseStr] {
		logger.L().Debug("werewolf: spectator wake skipped (phase not whitelisted)",
			zap.String("room_id", r.RoomID),
			zap.String("phase", phaseStr))
		return
	}
	// 2026-07-09 §13 增强:可配置节流开关。SpectatorFullWake=true 时直接跳过
	// 15s 节流;false 时回退旧行为(2026-07-08 §13.6)。
	if !cfgWerewolfSpectatorFullWake() {
		if m.spectatorWakeLimiter == nil || !m.spectatorWakeLimiter.Allow() {
			logger.L().Debug("werewolf: spectator wake throttled (15s window, legacy)",
				zap.String("room_id", r.RoomID),
				zap.String("phase", phaseStr))
			return
		}
	}

	// 持 r.mu 迭代 BotAgents,构造每 bot 独立的 GameContext
	r.mu.Lock()
	driverSeat := lowestActiveBotSeatLocked(r)
	n := 0
	for seat, ag := range r.BotAgents {
		if ag == nil || ag.IsQuarantined() {
			continue
		}
		if !r.State.AliveSeat(Seat(seat)) {
			continue
		}
		gc := buildAgentContextLocked(r, seat, driverSeat)
		ag.PushEvent(wwplayer.AgentEvent{
			Kind:    "spectator_speech",
			Context: gc,
		})
		n++
	}
	r.mu.Unlock()

	logger.L().Info("werewolf: spectator wake fired",
		zap.String("room_id", r.RoomID),
		zap.String("phase", phaseStr),
		zap.Int("awakened_bots", n),
		zap.Int("driver_seat", driverSeat))
}

// NewWerewolfManager 创建空管理器。保留为无_registry_调用方(测试等)的向后兼容入口,
// 等价于 NewWerewolfManagerWithRegistry(nil)。下一次 major 时可移除。

// NewWerewolfManagerWithRegistry 创建管理器并挂接 LLM provider registry。
// registry 为 nil 时游戏仍然能跑(只是 agent 不可用,agent seat 退化为占位座位),
// 便于在测试/无配置的 dev 环境下继续运行。

// Registry 返回该管理器持有的 LLM registry(可能为 nil)。

// GetRoom 导出版（v3 §G5 用于 REST API /api/games/werewolf/rooms/:roomId/prop_history）。
// 与内部 getRoom 等价,但允许外部包(api 等)调用。

// FindUserRoom 反查给定的 userID 当前位于哪个活跃狼人杀房间。
// 用于 REST 入口(GET /api/games/werewolf/props)在玩家未传 room_id 时
// 仍能回填 my_props_remaining / cooldown_remaining_sec 等 per-room 字段。
// 返回 (room, seat) — 若不在任何活跃房间中则 room == nil, seat == -1。
// 导出版:内部用 findUserRoomLocked(持 m.mu 时调用);外部包调本函数。

// findUserRoomLocked 在 m.mu(读或写)已持锁状态下反查 userID。
// 多个活跃房间理论上不会有同一 userID;返回第一个匹配即可。

// seatOfLocked 在 r.mu 已持锁状态下返回 userID 对应的座位。
// 内部辅助:避免 SeatOf(加读锁)在 m.mu 已持锁时被嵌套调用。

// ChatQueueSnapshot returns a defensive snapshot of the room's shared 500K
// chat history queue + per-seat read pointers. Used by admin/UI to render the
// queue contents.
//
// Returns (nil, false) if the room does not exist.
//
// 2026-07-09 §13-bugfix: returns the entire queue regardless of read pointer
// (since callers want to see "what was said" rather than "what each bot
// already consumed").

// RestartVoteSnapshot 是 2026-07-10 新增的"重开局投票快照"接口,
// 供 ws.GameService 在收到玩家投票后通过 game.restart_vote_update 帧广播。
//
// 返回 (payload, true) 仅当房间当前在 PhaseRestartVote + 投票未结算时;
// 否则返回 (nil, false) — 调用方不应广播(例如已 passed / rejected / timeout)。
//
// 字段设计与 view.go::RestartVoteExtra 同步;前端用增量更新合并到 ClientGameState
// 的 phase_extra.restart_vote 之上。

// JoinGame 让 userID 入座。幂等。
// 达到 7 人时自动发牌 + 启动第一夜。

// restoreBotsLocked re-fills the in-memory room's seats + seatModelKeys from
// the persistence layer. Called by JoinGame / SpectateGame when a freshly-
// allocated empty room turns out to correspond to a roomID with previously
// registered bot seats (typical after a server restart).
//
// BUG-WEREWOLF-P0-7 FIX: without this, restart drops bot seats from memory;
// the room then auto-creates as empty on the next join and never reaches
// 7/7, so force-start never fires and the room is stuck at "filling".
//
// Caller must hold r.mu.

// StartAgentsLocked spawns one Agent.Run goroutine per registered bot seat
// (r.seatModelKeys) after a successful StartGame. The caller must hold r.mu.
// Agents are started only if m.registry is non-nil; otherwise the bot seats
// remain placeholders (no in-process LLM).
//
// Each agent gets:
//   - a buffered per-seat events channel (capacity 16) installed via SetEvents,
//   - a newAgentRunner that routes ToolRunner calls to the manager + chatSvc,
//   - a fresh context derived from m.ctx that's cancelled on RemoveGame.

// tryDispatchQuarantinedActingSkip detects a quarantined bot that is also the
// current acting seat (gc.MyTurn=true) and dispatches the phase's safe skip
// (*_skip / finish_*) on its behalf. This bypasses the agent goroutine, which
// is stalled by a permanent LLM error (403 / 401 / context-deadline), so the
// phase can advance.
//
// Returns true if a skip was dispatched (caller should NOT push a wake event
// for this seat — the agent is quarantined and the skip already advanced the
// engine). Returns false when the bot is either healthy or not the acting
// seat, in which case the caller falls through to the normal PushEvent path.
//
// BUG-WEREWOLF-P0-NEW-27 (R32): previous code in WakeActingAgents dispatched
// the skip for *any* quarantined bot regardless of whether it was the current
// acting seat. When the quarantined bot's seat ≠ SpeakTurnSeat, the skip
// (finish_speak) failed with [30008] "not current speaker" and the `continue`
// skipped the PushEvent chain — so the *real* acting bot never got woken and
// the room dead-locked at "phase=speak round=N". We now only dispatch when the
// quarantined bot actually is the acting seat, and otherwise treat it like a
// healthy non-acting bot (PushEvent to sync Memory; runLoop will no-op via the
// IsQuarantined guard). Caller must hold r.mu.

// notifyQuarantine is the callback registered on each agent via
// SetOnQuarantine. It is called (in a goroutine) when the agent transitions
// to quarantined state inside handleEvent → SetQuarantined(). The callback
// re-fetches the room + agent, acquires r.mu, and checks whether the now-
// quarantined bot is the current acting seat — if so, it dispatches the
// phase's safe skip action so the engine keeps moving.
//
// BUG-WEREWOLF-P0-NEW-27 (Round 34): without this proactive notification,
// the manager only learns about quarantine on the next wake event. If the
// quarantined bot is the current speaker and no other event source pushes
// a wake, tryDispatchQuarantinedActingSkip never fires and the room
// dead-locks at "phase=speak round=N" for 7+ minutes.
//
// The goroutine is safe: it acquires room.mu (no agent.mu involved) and
// runs independently of the agent's handleEvent (which continues to the
// quarantine `return` after SetQuarantined).

// channel. Caller should hold no locks (push uses its own non-blocking send).
// Used by the broadcast path to wake agents on phase change.
//
// BUG-WEREWOLF-P0-4 FIX: each agent now receives a *per-seat* GameContext
// built from the live game state (MyTurn / role info / driver flag), instead
// of the empty wwtypes.GameContext{} that was broadcast before. The empty
// snapshot left MyTurn=false for every seat, so BuildUserPrompt appended
// "(暂未轮到你…保持沉默)" to every agent's prompt — including the acting
// wolf / seer / witch — and every LLM dutifully returned end_turn. No agent
// ever acted, so no phase ever advanced, so no new wake ever fired: a
// permanent self-lock at night_wolves.
//
// BUG-WEREWOLF-P0-NEW-27: the broadcast path also handles a quarantined
// acting-bot by dispatching its skip in-place (same as WakeActingAgents),
// so the room advances out of a phase whose current actor's LLM is broken
// without needing the agent goroutine to acts.
//
// `snap` is retained as a backward-compatible fallback for the rare case the
// engine state can't be read (e.g. room tearing down mid-wake).

// wakeAllAgentsLocked is the lock-held variant of WakeAllAgents. Caller must
// hold r.mu. Used by JoinGame / SpectateGame which already hold the room lock
// via defer r.mu.Unlock() and cannot re-enter WakeAllAgents (which acquires
// the same lock → deadlock).

// WakeActingAgents pushes a wake event only to bot seats whose per-seat
// GameContext says MyTurn=true. Used by the in-process agent-action path
// (agentRunner.wakeAll) so that after e.g. a wolf_kill, only the next acting
// seat (the seer) is nudged — not all 7 bots, which would otherwise each burn
// an LLM round-trip just to reply "保持沉默". The broadcast path keeps using
// WakeAllAgents so non-acting agents still get state-synced into Memory.
//
// BUG-WEREWOLF-P0-NEW-3: if the acting seat's agent is quarantined (its LLM
// provider is permanently broken — 403 quota / 401 bad key), the agent would
// yield without dispatching any action, and the phase would stall exactly as
// before. To keep the engine advancing, WakeActingAgents detects a
// quarantined-acting-bot and immediately dispatches the phase's safe skip
// (*_skip / finish_*) via the manager's Action_* path — bypassing the agent
// goroutine entirely. This makes quarantine actually effective: the bot's
// turn becomes a no-op skip and the next phase's acting seat gets woken.

// dispatchQuarantinedSkipLocked performs one phase's safe skip action on
// behalf of a quarantined bot. Caller must hold r.mu. We route through the
// lock-held *Locked variants (NOT the public Action_* methods) so all
// side-effects (death, phase transition, wake) happen exactly as if the agent
// had called the tool directly — WITHOUT re-acquiring r.mu. No agent goroutine
// involvement - the agent is stalled by its permanent LLM error.
// BUG-WEREWOLF-P0-NEW-3.
//
// BUG-WEREWOLF-P0-NEW-42 (Round 37): every case here previously called the
// public Action_* method, each of which does r.mu.Lock(). But this function is
// ALWAYS called with r.mu already held (callers wakeActingAgentsLocked /
// wakeAllAgentsLocked / notifyQuarantine acquire r.mu first). Go's sync.Mutex
// is not reentrant, so the re-Lock self-deadlocked - freezing the whole room.
// R37 reproduced this on speak->vote: Action_FinishSpeak (holding r.mu) ->
// wakeActingAgentsLocked -> quarantined vote_skip -> Action_DayVote ->
// r.mu.Lock() => permanent deadlock. All cases now use the *Locked variants.

// lowestAliveBotSeatLocked returns the lowest-numbered alive bot seat, used as
// the "host driver" that advances structural phases (dawn→start_day,
// sheriff→sheriff_elect, vote→finish_vote) in full-AI rooms with no human GM.
// Caller must hold r.mu. Returns -1 if no alive bot.

// lowestActiveBotSeatLocked returns the lowest-numbered alive, non-quarantined
// bot seat. Used by watchdogActingSeat / MyTurn selection for PhaseVote so a
// quarantined bot is never picked as acting driver — it would never be woken
// (PushEvent → IsQuarantined guard → no-op) and the phase would spin.
// Caller must hold r.mu. Returns -1 if no active bot.
// BUG-R193-001.

// syncQuarantinedLocked mirrors r.BotAgents[*].IsQuarantined() into
// r.State.QuarantinedSeats so engine-layer vote logic (allActiveVoted) can tell
// apart alive-but-unvotable seats from active voters. Caller must hold r.mu.
// BUG-R193-001.

// buildAgentContextLocked projects the live game state into a per-seat
// wwtypes.GameContext. The caller must hold r.mu.
//
// MyTurn is the critical field: it MUST be true exactly when this seat has a
// legal action right now, so BuildUserPrompt tells the LLM "现在轮到你行动"
// (and offers the phase's tools) rather than "保持沉默". Mapping per phase:
//   - night (wolves/seer/witch): the engine's TurnActingSeat (single actor)
//   - dawn / sheriff: the host driver only (advances to day / elects sheriff)
//   - speak: the current SpeakTurnSeat
//   - vote: every alive seat that hasn't voted yet (plus the driver, which
//     tallies via finish_vote once AllVoted)
//   - hunter_shoot: the hunter while a shot is pending

// mgrPropEngineWalletSvc 安全地拿到房间的 wallet 服务（r.mu 持锁下调用）。
// 通过 PropEngine 间接引用,避免在 WerewolfRoom 上挂一个 manager 字段。
// 返回 *service.WalletService（具体类型避免匿名 interface 引入的编译噪音）。

// werewolfAnteAmountLocked 返回本房间单局底注金额（v3 §G3）。
// r.mu 持锁下调用。13 人局默认 100；其它人数按比例缩放。
// 数据源：r.State.SeatCount × 系数。失败 / 无值 → 100。

// ─── 道具系统 v2 辅助函数 ───

// buildPropSnapshotLocked 构造当前座位可购买的道具快照（驱动 use_prop 工具 schema 动态生成）。
// 过滤规则：已达个人上限 / 冷却中 / 全局预算耗尽 → 剔除。这样 LLM 永远看不到也不能选不可用的道具。
// 必须在 r.mu 持锁下调用（由 buildAgentContextLocked 调）。

// roomPropBudget 读取房间级道具全局金币预算（配置 + 0 = 禁用）。
// roomPropBudgetOverride 是测试覆盖钩子（非 0 时优先使用），避免测试依赖全局配置。

// propSeatBudgetUsedLocked 返回座位本局道具消耗累计。

// defaultPropCooldownSec 读默认冷却秒数（固定 30s，与 defaultProps 对齐）。

// defaultPropMaxPerGame 读默认每局购买上限（固定 3，与 defaultProps 对齐）。

// buildAllPlayersLocked 构造当前房间的 7 座位档案表。返回 []wwtypes.PlayerBrief,
// 严格按座位号 0..6 顺序;空座位 IsBot=false / Account="(空座位)"。真人
// 玩家昵称优先从 r.recentSpeeches 最近的同 seat speech 中提取,bot 玩家
// 昵称固定 "Bot N号" + AgentName 来自 r.seatModelKeys。

// stopAgentsLocked shuts down every agent goroutine attached to the room,
// closes their events channels, and waits (up to stopAgentsWaitTimeout) for
// each goroutine to actually exit. The caller must hold r.mu.
//
// Why Wait() is required:
//   - BUG-WEREWOLF-DISBAND-LEAK: 管理员强制解散 7-AI 狼人杀房间时,如果
//     stopAgentsLocked 仅 cancel + close(events) 就返回,正在进行的
//     Provider.Chat HTTP 调用 (默认 8s timeout) 和 llmRetryBaseDelay (1s/2s/4s)
//     backoff 循环仍会继续跑,持着 r.mu/BotAgents 引用。一旦我们清空了
//     m.rooms,这些 goroutine 就成了"孤儿"——它们的 ctx 被 cancel 但
//     runLoop 在 Chat() 返回前不会立即退出。HTTP 响应 / TCP 连接 / LLM
//     计费会一直累积到下一次 retry 完成。
//   - 修复:agentWG.Add(1)/Done() 配对,stopAgentsLocked 调 Wait() (带
//     timeout 兜底以防某 goroutine 真卡死),保证 ForceDisbandRoom 返回
//     200 时所有 agent goroutine 都已 return。
//
// stopAgentsWaitTimeout 是兜底上限:正常情况下 cancel 后 runLoop 几百毫秒内
// 就退出,这里给 5s 应对极端慢的 LLM 响应;超过则记 Warn 日志继续。

// ─────────────────── Phase Watchdog Goroutine ───────────────────

// startPhaseWatchdog launches a background goroutine that polls the room every
// phaseWatchdogTickInterval seconds. It emits a WARN-level heartbeat log when
// the same phase+actingSeat has persisted for longer than
// phaseWatchdogWarningInterval, and dispatches the phase's safe skip action
// when it exceeds phaseWatchdogDeadlineFor(seatCount).
//
// BUG-WEREWOLF-P0-NEW-42b (Round 38): without a watchdog, an agent goroutine
// that stalls silently (goroutine panic, wake-channel drop, or
// quarantine-skip-then-engine-race) leaves the phase permanently stuck with
// no recovery path. The watchdog is a safety net that detects the condition
// and forces the engine forward.
//
// The goroutine terminates when ctx is cancelled (stopAgentsLocked calls
// r.watchdogCancel during room teardown). Caller does NOT hold r.mu.

// phaseWatchdogTick performs one watchdog cycle: acquires r.mu, checks whether
// the current phase+actingSeat is the same as last tick, and either resets
// the timer (if it changed) or fires heartbeat/warning/skip if overdue.
// Returns an error if the room is gone (caller should stop the goroutine).

// watchdogActingSeat returns the seat that should act in the current phase.
// Returns -1 when no single acting seat is applicable (vote).
//
// BUG-WEREWOLF-P0-2 (R42): PhaseHunterShoot previously returned -1 alongside
// PhaseVote, which meant the phase watchdog could never dispatch a skip for a
// stuck hunter_shoot phase. Now we find the hunter seat explicitly; if the
// hunter is dead (normal for the on-death ability) or quarantined, the
// watchdog can still dispatch a hunter_shoot(-1) skip to advance the day.

// findHunterSeat returns the seat index of the hunter role, or -1 if no
// hunter is assigned. The hunter may be dead (on-death ability); this
// function only checks role assignment, not liveness.
// BUG-WEREWOLF-P0-2 (R42).

// State_Begin 返回当前 State,若 nil 则懒加载。

// SetSeatModelKey 记录 agent 座位对应的 LLM model_key。须在 JoinGame 前调用:
// 房间创建时 service 层配置 agent 座位后,让 Manager 记住驱动信息,Phase 4 用来
// 构造 wwplayer.Agent。registry != nil 时才有意义。幂等。

// SetJudgeConfig 2026-07-16 主持人重构 — 落房间级法官设置(JudgeDesired / JudgeModelKey)
// 到 in-memory WerewolfRoom,必须在 RegisterAgentSeats 之前调用(BUG-R136-RACE-001):
// RegisterAgentSeats 内部在座位 13/13 已满时立即触发 ForceStartIfReady →
// startJudgeGoroutine,后者入口守卫 if !r.JudgeDesired { return } 依赖 JudgeDesired 已置位。
// 幂等:重复调用覆盖。

// SetSeatCount 设置本局实际参与人数(13 / 12 / 7)。用于多模式兼容:
//   - 默认 werewolf / werewolf_13:13
//   - 历史 werewolf_12:12
//   - 历史 werewolf_7:7
//
// 在房间创建后、StartGame 前调用。同步到 r.State.SeatCount 驱动发牌选择。
// SeatCount <= 0 视作 13(默认)。幂等。

// SeatModelKey 返回某座位的 LLM model_key(未配置返回空)。

// AgentSeats 返回房间内所有已登记 agent 座位的 model_key 快照。
// Phase 4 启动时据此构造 wwplayer.Agent。

// ManagerAddPlayerAt 是 CreateRoomWithAgents 的内部工具:在指定座位预填 userID
// (通常是 bot user id)。幂等: 同 userID 已入座作幂等返回; 座位已被其它
// 玩家占 → ErrRoomFull。
// 不同于 JoinGame: 不触发自动开局。(所有 agent 和人类创建者都预填完后再
// 统一调用 JoinGame 让人类参与者入座,这不会再次触发开机因为已达 7 人。)

// ForceStartIfReady 当房间已被 ManagerAddPlayerAt 预填到 7/7 时(典型场景:全
// AI 房间,创建者被降级为观察者),立即触发 StartGame + 启动 Agent goroutine。
// 与 JoinGame 行为对称,差别在于不走 AddPlayer 入座流程(因为没有人要入座)。
// 返回 (started bool): true 表示这次调用从 filling → 某个夜间阶段。
//
// BUG-WEREWOLF-P0-1 FIX: 7-AI 全自动房间创建走这条路径,否则引擎永远停留在
// PhaseFilling,玩家(观察者)看不到任何 phase 推进。

// Action 处理玩家各种动作。返回 (room, started/end-event, error)。
// 外部路由参见 game_service.go handleWerewolf* 系列。

// Action_Pause: UI 暂停/恢复游戏。
//
// 2026-07-24 优化:仅房主(userID 匹配 r.Seats[0..n-1] 的第一个真人玩家)或
// admin 可调用。pause=true 暂停当前房间,所有 bot 跳过 LLM 调用、阶段
// 时钟冻结、watchdog 跳过强制 skip;pause=false 恢复。
//
// 设计目标:
//   - 防止 LLM 上游代理批量 429/5xx 时,继续调 LLM 把所有 bot 送进 quarantine。
//   - 真人玩家可"暂停一下"等待上游恢复 / 自己研究战局 / 临时离开。
//
// 不持久化,重启即解除。

// Action_SeerCheck: 预言家查验。

// Action_SheriffStream: 预言家警长声明 / 撤回警徽流。
// 2026-07-10 §7:slot ∈ {1,2}, target = -1(撤回) | 0..11。

// Action_IdiotReveal: 白痴翻牌结算。
// 2026-07-10 §3.5:choice ∈ {reveal, skip}。

// Action_Witch: 女巫用药。

// Action_GuardProtect §134 守卫夜间守护公开入口。与 Action_Witch 完全同构:
// getRoom → r.mu.Lock → SeatOf → r.State.NightGuardProtect(actor, target)。
// 锁内变体 guardProtectLocked 在 room_quarantine_skip_locked.go,供 watchdog /
// dispatchQuarantinedSkipLocked 调用(§92a 硬约束)。

// Action_DayVote: 白天投票/警长投票。

// Action_FinishVote: 白天投票完成(由 GM 触发)。

// Action_FinishSpeak: 白天发言结束(由 GM 触发)。
//
// BUG-WEREWOLF-P0-NEW-16 (Round 29): Action_FinishSpeak previously only
// advanced gs.SpeakTurnSeat via NextSpeaker and returned. The wake to the
// NEXT acting seat was the caller's responsibility — agentRunner.FinishSpeak
// calls r.wakeAll() after success, and the WS path goes through
// broadcastWerewolfState → wakeWerewolfAgents. But the in-process
// dispatchQuarantinedSkipLocked path (used for permanently broken LLMs and
// any future in-process caller that forgets to wake) bypassed both — the
// phase advanced to the next speaker, yet no agent was woken, leaving the
// room dead-locked at "phase=speak round=N" with no recoverable event
// source. Reported by R29 automated test as P0-NEW-16.
//
// Fix: Action_FinishSpeak itself now wakes the next acting seat (if any)
// using the lock-held variant wakeActingAgentsLocked. This makes the wake
// authoritative on the engine side rather than the caller side — every
// caller path (auto-skip from agent.run.go, dispatchQuarantinedSkipLocked,
// agentRunner.FinishSpeak, WS handleWerewolfAction) gets the wake for free.
// Callers that already call r.wakeAll() continue to work; the extra wake is
// harmless because (a) WakeActingAgents only fires for the current
// SpeakTurnSeat, so re-pushing the same event to the same channel just
// buffers a redundant wake that the agent's runLoop drops via the empty-
// Phase guard on its next iteration, and (b) wwplayer.Run is single-event at
// a time so duplicate events are coalesced by the channel buffer.

// finishSpeakLocked is the lock-held variant of Action_FinishSpeak. It
// performs only the engine-state mutation (r.State.FinishSpeak) plus the
// next-acting-seat wake. Used by dispatchQuarantinedSkipLocked when
// acting on behalf of a quarantined bot, where the caller already holds
// r.mu and re-entering Action_FinishSpeak would deadlock.
//
// BUG-WEREWOLF-P0-NEW-16: dispatchQuarantinedSkipLocked was previously the
// canonical "I forgot to wake" path — it called Action_FinishSpeak which
// advanced phase but emitted no wake (caller had no chance to wake before
// the function returned). After Action_FinishSpeak itself started waking
// the next seat, dispatchQuarantinedSkipLocked was reaping the benefit
// automatically — but it still re-acquired r.mu through the public method
// and deadlocked the moment a quarantined bot triggered an in-place skip.
// This lock-held helper is the in-process equivalent: same engine
// mutation, same wake, no re-lock.

// Action_ProposeVote 预言家在白天发言阶段发起投票,直接结束讨论进入投票阶段。
// 2026-07-11: 预言家亮明身份后可主动发起投票。

// hunterShootLocked is the lock-held variant of Action_HunterShoot. It calls
// r.State.HunterShoot(seat, NoSeat) to mean "don't shoot" — the hunter's
// default skip, which advances the day (or ends the game if applicable).
// Used by dispatchQuarantinedSkipLocked and the phase watchdog when a
// quarantined or dead hunter's turn must be skipped.
//
// BUG-WEREWOLF-P0-2 (R42): without this, a quarantined dead hunter's
// hunter_shoot phase permanently stalls — no agent goroutine can advance it,
// and the watchdog has no safe skip to dispatch.

// idiotRevealLocked 是 idiot_reveal 工具的 lock-held 派发。caller 必须持 r.mu。
// dispatchQuarantinedSkipLocked("idiot_reveal") 与 watchdog 派发由此路径结算
// 白痴翻牌(默认 "skip" 放弃翻牌 → 正常放逐)。

// sheriffStreamDeclareLocked 是 sheriff_stream 工具的 lock-held 派发。
// caller 必须持 r.mu。仅预言家警长可在白天声明 / 撤回警徽流。

// ─────────────────── Round 40 §95:首夜强制发言锁内变体 ───────────────────
// 5 个 *Locked 函数配套使用,避免 CLAUDE.md §92a 教训的 r.mu 自死锁:
//   1. actionSpeakPreWolvesLocked — agentRunner.Speak() 在持锁路径上调
//   2. recordForcedSpeakPlaceholderLocked — run.go MaxToolUse 兜底,直接累加
//   3. advancePreWolvesRoundLocked — 检查全员完成,切到下一轮 / 切 PhaseNightWolves
//   4. allForcedSpeakDoneLocked — 哨兵函数
//   5. firstLivingWolfLocked — 切 PhaseNightWolves 时定位首个活狼
//
// 调用方必须持有 r.mu。这些函数不调任何 Action_* 公共方法(避免 r.mu 自死锁),
// 不调 wakeAllAgentsLocked(避免持锁期间触发新一轮 wake 死锁)。
// 若需要推 wake,交给 phaseWatchdogTick 的外层 defer 处理。

// actionSpeakPreWolvesLocked 累加 seat 的强制发言次数,并检查是否全员完成。
// 调用方必须持有 r.mu;不调任何 Action_*,不调 wakeAllAgentsLocked(由 watchdog
// 在下一 tick 推 wake)。

// recordForcedSpeakPlaceholderLocked 占位发言:不调 ChatService,不广播,
// 仅累加 PreWolesSpeakCount[seat]++。用于 run.go MaxToolUse 兜底——LLM 永远
// 沉默或反复 idle_silent 时,引擎派占位发言推进轮次。
//
// 不调 wakeAllAgentsLocked(持锁期间触发 wake 会死锁),由 watchdog 推 wake。

// advancePreWolvesRoundLocked 检查全员完成 → 切换轮次 / 切到 PhaseNightWolves。
// 必须在持锁路径上调用,且调用前已更新 PreWolvesSpeakCount。
// 返回 true 表示已切到 PhaseNightWolves,调用方应停止派更多 wake。

// allForcedSpeakDoneLocked 哨兵:全员(存活)发言次数 >= rounds 目标。
// rounds=0 时永远 false(向后兼容:关闭强制发言)。

// firstLivingWolfLocked 切 PhaseNightWolves 时定位首个活狼;7 人局必有 2 狼,
// 理论上不会返回 NoSeat;若返回则由调用方 endSeerPhase 兜底。

// wakeActingAgentsLocked is the lock-held variant of WakeActingAgents.
// Caller must hold r.mu. Mirrors WakeActingAgents' acting-seat-only push
// (MyTurn=true) so non-acting bots are not woken with redundant events.
// Used by Action_FinishSpeak to guarantee the next speaker is nudged even
// if the caller forgot to wake after dispatching the finish_speak tool.
// BUG-WEREWOLF-P0-NEW-16.

// Action_WolfSuicide: 狼人自爆。

// Action_HunterShoot: 猎人开枪。

// Action_SheriffCandidate: 参选警长。

// Action_SheriffElect: 警长选举结算。

// Action_StartDay: 天亮后启动白天(由 GM / 计时器调用)。
// 13 人标准竞技局扩展(docs/狼人杀13人标准局规则.md §7.4):StartDay 阶段若上夜警长死亡(sheriffSlain) ,
// 自动结算警徽流并把结果广播 game.sheriff_stream_settle。

// maybeSettleSheriffStreamLocked 在 dawn→白天 转换时结算警徽流。
// 规则(docs/狼人杀13人标准局规则.md §7.3):预言家警长按双警徽流结算金水/查杀/撕警徽;
// 非预言家警长走 SheriffSuccessor(生前口头指定),无指定则撕。
// caller 持 r.mu。结算后广播 game.sheriff_stream_settle。

// Action_LastWords: 遗言 actor 提交遗言(btn/human 通过 chat.send + 特殊工具调用)。
// BUG 2026-07-09: 仅当前遗言座位可调用。调用方可能是 bot(driver run loop),因此走
// manager-level Action_* 公开路径(自身持 r.mu,§92a 兼容)。

// enterDeathLyricRoundLocked 是 tryEnterDeathLyricRound 的房间级包装。
// 进入遗言阶段成功时,广播 death_lyric_start 活动事件(首个遗言座位入席)。
// BUG 2026-07-09: 遗言功能 §13。调用方必须持有 r.mu。

// Action_SkipLastWords: 遗言 actor 放弃遗言。

// Action_UseProp 公共入口 — 人类玩家使用道具(2026-07-21 §道具系统)。
// 与 agentRunner.UseProp 共用同一 PropEngine + WalletService + 广播路径,
// 但由 WS 帧 `game.werewolf_use_prop` 经此入口派发:
//
//  1. 短时持锁读取 State / 解析座位 / 准备 PropUseRequest;
//  2. 释放锁后调 propEngine.UseProp(其内部会再持锁完成金币/中招/日志);
//  3. 再次短时持锁:若中招,把注入文本塞入 propInjectQueue;
//  4. 调用 broadcastPropUseLocked 公开广播 + 唤醒所有 bot。
//
// 锁安全:严格遵守 §92a — 持锁时只读,绝不调 Action_*;调 PropEngine
// 时锁已释放,PropEngine 内部自己处理持锁。

// GetState 返回某玩家可见的对局视图。

// StateForSeat 在已持有房间引用时构造指定座位视图。

// PublicState 是房间级别的「公开对局状态」投影,只包含全员可见的元信息
// (phase / day / status / winner / 是否已开局),不含任何座位专属数据
// (角色 / 查验 / 用药 / 投票 / bot 思考),适合 REST 房间详情接口向任何
// 调用方回显,无需 userID。Round 23 P1 BUG FIX: 此前 REST 房间详情只能
// 从 t_lsm_game_room.status 取值,阶段机推进不会反映到 status,导致
// `phase` 字段在客户端永远是 "?"。

// PublicPlayerState 是单个座位的「公开对局状态」投影,只包含全员可见信息
// (存活 / 角色揭示后身份 / 死因 / 处决/死亡决断 / 警长),不暴露
// 查验结果 / 用药历史等私属数据。R100 P1 BUG FIX: 此前 REST /api/rooms/{id}
// 返回的 players[] 仅填充 DB 行的 user_id/seat/role,UI 显示的
// 「存活/已死亡/角色揭示」完全不可见。任何依赖 API 的自动化测试或外部系统
// 无法获取真实游戏状态。本结构体让 RoomService 在 GetRoomDetail 时合并
// in-memory GameState,字段均与视图层 PlayerJSON.Alive 对齐。

// GetPublicState 返回 roomID 的公开对局状态。当房间从未被 JoinGame /
// ForceStartIfReady 创建,或已被 RemoveRoomState 拆除时,返回
// ("", 0, "", "", false)。调用方应自行决定 nil/空值时的回退策略(常见
// 选择:沿用 t_lsm_game_room.status 字段)。

// GetPublicPlayerStates 返回 roomID 的所有座位公开对局状态(每个座位的
// 存活/角色揭示/死因/处决决断/警长标记),纯座位级公开数据。
//
// R100 P1 BUG FIX: RoomService.GetRoomDetail 之前只从 t_lsm_game_player
// 取 user_id/seat/role,导致 REST /api/rooms/{id} 的 players[] 不含
// 存活/死亡状态,与前端 UI 显示的死亡徽章/角色揭示完全脱节。本方法让
// REST 详情接口把 in-memory GameState 的存活/角色/死因数据透传出来,
// 任何依赖 API 的自动化测试或外部系统能拿到真实游戏状态。
//
// 安全语义:
//   - 仅返回全员可见的元信息(存活/死亡后揭示的角色/死因),不暴露
//     角色身份私属数据(查验结果 / 用药历史 / 投票目标)
//   - Role 字段仅在 RoleRevealed=true(玩家死亡或 GameOver)时填充,
//     否则保留空字符串以避免角色泄露
//   - 未开局(r.State == nil)时返回每个占用座位的 alive=true 占位,
//     角色不揭示(对调用方等价于大厅阶段尚未开局的占位数据)
//
// 并发安全:与 GetPublicState 一致,使用 lockRoomBriefly(200ms) +
// publicStateCache 兜底,确保 LLM 重试 / auto-skip / quarantine 等
// 长持锁场景下 REST 调用方不会无限阻塞(R26 教训)。

// SpectatorRoomStatus reports whether the room exists and hosts a live game.
//
// BUG-WEREWOLF-P0-NEW-38 (Round 36): after admin ForceDisband, the room can be
// recreated empty (r := &WerewolfRoom{...}; m.rooms[roomID] = r; later
// r.State = NewGame(...) falling through SpectateGame's fallback at line 2044).
// The broadcast path broadcastWerewolfSpectatorState then pushes a frame
// {phase:"filling", seats:[""x7], players:[{alive:false}x7]} to any spectator
// still subscribed — directly contradicting GET /api/rooms/{id} which reports
// phase=speak / current_count=7 / latest live snapshot. The phantom "filling"
// frame forces the spectator UI back onto the waiting-board spinner.
//
// The fix is to have broadcastWerewolfSpectatorState query this guard BEFORE
// pushing a state frame: if exists=false the room is gone (skip); if
// exists=true but live=false — the room was recreated empty post-disband and
// must also stay silent so the spectator keeps its last good view. Only
// live=true (≥1 seated player) should surface a game.state frame.
//
// Semantics of `live`:
//   - r.State == nil                    → not live (empty / not yet hydrated)
//   - ≥1 seat filled OR ≥1 player seated → live (real in-progress game)
//
// lockRoomBriefly keeps this read-only guard bounded even if the engine is
// mid-op holding r.mu on the action/quarantine path.

// populateBotContexts fills cs.BotContexts from each bot agent's latest
// decision transcript. The caller must hold r.mu (true at every
// BuildClientState call site in this file).
//
// Source of truth for "this room has a bot on seat N":
//   - r.seatModelKeys  (room-creation-time model assignment) — primary
//   - r.BotAgents      (after StartAgentsLocked) — secondary
//
// We union both so the panel keeps showing all 7 bot tabs even when:
//  1. StartAgentsLocked hasn't run yet (PhaseFilling).
//  2. BotAgents is empty for some other reason (registry nil, partial
//     start failure).
//  3. An agent has been created but never completed a decision (placeholder).
//
// Visibility:
//   - Spectator (cs.MySeat < 0): full bot reasoning — thinking, tool history,
//     recent messages are all surfaced so the spectator AgentThoughtPanel can
//     follow the AIs' reasoning.
//   - Mixed human player (cs.MySeat >= 0): sanitized bot reasoning. The panel
//     still shows that *each bot has a recent thought* (seat + model + vague
//     thinking text), but fields that would leak private strategic info are
//     stripped — `recent_messages` may carry wolf-whisper content; `tool_calls`
//     carry `wolf_kill target=3` / `seer_check` targets / `witch` decisions.
//     Keeping thesanitized view for players fixes BUG-WEREWOLF-P0-NEW-3
//     ("mixed 模式下人类玩家面板永远是 0") without breaking the hidden-info
//     invariant (the same invariant that already governs Role / RoleRevealed).
//
// BUG-WEREWOLF-P0-NEW-2: previously the spectator AgentThoughtPanel was
// permanently empty (showed `(0)`) because the server never serialized any
// bot thinking / tool activity. This wires each agent's lastTranscript into
// the game.state payload. Agents that have not yet completed a decision emit
// a minimal placeholder (seat + model) so the panel counts every wired bot.
//
// BUG-WEREWOLF-AGENT-PANEL-EMPTY (Round 23): 当 r.BotAgents 为空(常见于
// 全 AI 房间 spectator 等待阶段、或某个 goroutine 启动失败)时,旧实现
// 早返回导致 cs.BotContexts = nil,JSON 序列化时被 omitempty 吃掉,前端
// `gameState.bot_contexts === undefined` → 显示「尚无思考内容」,即使该
// 房间已经配置了 7 个 bot 座位。新实现改为基于 seatModelKeys + BotAgents
// 的并集:只要房间存在已注册的 bot 座位,就一定输出占位条目,前端面板
// 至少能显示 N 个 tab,符合「7 人标准局 → 7 个 bot」的预期。

// populateAgentNames fills in PlayerJSON.AgentName for bot seats so the
// frontend can display the LLM model name alongside the seat number.

// sensitiveToolNames is the set of tools whose result strings carry private
// strategic info (wolf kill target, seer check target, witch save/poison
// decision). Mixed-mode human players must not see these.

// sanitizeBotTranscript strips the fields of a bot transcript that would leak
// private strategic info to a viewer (human opponent in mixed mode, or a
// spectator who must not see actionable targets). The vague thinking text was
// preserved in earlier revisions, but R55 reported that LastThinking carrys
// role-inference narrative and QuarantineReason leaks upstream HTTP error
// stacks — both aid identity deduction in mixed mode. Both are now dropped,
// and `recent_messages` (may carry wolf-whisper content) is cleared, and
// sensitive tool_calls entries are replaced with a generic "[已隐藏]"
// placeholder so the panel still shows a non-zero call count.
//
// 2026-07-09 §重构 - "Agent 思考 → Agent 交互" 适配:
//   - 旧字段 (LastThinking / FullThinking) 已置空,但为防御性再次清空。
//   - 新字段 LastToolInput 按 sensitiveToolInputs 再次脱敏(防止 LLM 在 input
//     中塞入敏感字段名,虽然后端 SanitizeToolInput 已经过)。
//   - QuarantineReason 仍清空(沿用 R55 P2 规则)。
//
// 2026-07-10 R87 P0-1 / P0-3 修复 — 观战隐私边界:
//   - (a) 敏感工具( wolf_kill / seer_check / witch_act / hunter_shoot )的
//     LastDecisionSummary 字段含字面量"动作名 → N号",会暴露 bot 的夜袭/查验/
//     用药目标,观战者等同于开全图。按 sensitiveToolNames 脱敏:
//     "wolf_kill → 7号" → "wolf_kill → [已隐藏]"。
//   - (b) HeartThought 是 §119「心口不一」的 LLM 真实内心独白(含身份/策略),
//     仅应被 bot 自己 + 观战者看到。isSpectator=false (混合房间人类玩家) 时
//     显式清空;isSpectator=true 时保留,符合 §119 设计契约。
//
// BUG-WEREWOLF-P0-NEW-3 / BUG-WEREWOLF-R55-PRIVACY.

// maskSensitiveDecisionTarget 把 LastDecisionSummary 中的 "动作名 → N号"
// 目标字面量替换为 "动作名 → [已隐藏]",防止观战者/人类玩家读取 bot 的
// 夜袭/查验/用药目标(R87 P0-1)。
// 输入示例: "wolf_kill → 7号" / "seer_check → 0号"
// 输出示例: "wolf_kill → [已隐藏]" / "seer_check → [已隐藏]"

// reSanitizeToolInput 是 sanitizeBotTranscript 的辅助函数:把 LastToolInput
// JSON 字符串解析回 map,再调 wwplayer.SanitizeToolInput 重新脱敏。
//
// 为何不直接调 SanitizeToolInput?因为 SanitizeToolInput 接收 map,但
// LastToolInput 是字符串(JSON 序列化后)。这里"解析-重新序列化"是单点的
// 字符串层脱敏钩子,不会影响 SanitizeToolInput 的核心 API。

// Seats 返回房间各座位 userID 的快照。

// RemoveGame 从管理器移除房间。已经启动的 agent goroutine 通过
// stopAgentsLocked 被取消并清理,避免 goroutine 泄漏。

// WipeAllRooms atomically clears every in-memory werewolf room and stops
// every attached agent goroutine. Used by the shutdown path
// (main.go calls SIGTERM handler → gameSvc.RemoveRoomState plays well with
// RemoveGame per room; this method gives the manager a single hook so a
// process-wide wipe is one call instead of N).
//
// Returns the list of roomIDs that were wiped so the caller can log them.
// Safe to call on a manager with zero rooms (returns an empty slice).
// Caller must NOT hold any per-room lock — WipeAllRooms acquires each one.
//
// BUG-WEREWOLF-RESTART-CLEANUP (Round 34): prior to this method, the SIGTERM
// path left the entire room map populated; the kernel cleaned the goroutines
// but in-flight LLM HTTP calls (up to 8s timeout) plus 1s/2s/4s backoff
// retries kept firing until the process actually died, generating spurious
// 5xx in upstream proxy logs. Wiping first lets cancel() reach every agent
// goroutine inside stopAgentsLocked (5s hard cap), which keeps the upstream
// per-second budget tight on graceful restarts.

// RoomIDs 返回当前在内存中的所有房间 ID 快照。用于 shutdown 时统一广播
// `game.removed` 给所有 still-connected 的客户端(WipeAllRooms 之后)。
// 顺序不稳定(map 遍历);调用方如需确定性请自行 sort。

// RemovePlayer 在 userID 主动 leave 时移除其座位(occupied 减 1)。

// HandleDisconnect 在真人玩家中途断线被 15s 超时踢出时,把该座位标记为死亡,
// 使 firstLivingWolfLocked / firstLivingSeer / refreshCounts / checkWinner 等
// 遍历存活玩家的函数正确跳过该座位,避免 acting seat 永久卡在已断线的座位上
// 导致 night_wolves 阶段 watchdog 无限循环派发 skip。
//
// BUG-R7-P0-disconnect-stuck (R7 报告 §3.1): bot-only 或人机混合房间中,
// 真人中途掉线后 LeaveRoom 仅清理 DB 行,GameState 中该座位仍 Alive=true,
// firstLivingWolfLocked 每夜都返回该座位,但 BotAgents[seat]==nil 无 bot 行动,
// watchdog 90s 派发 wolf_kill skip 仅 reset vote 不推进 acting seat,房间永久卡死。
//
// 修复:断线时把座位标记为死亡(死因 "disconnected"),并推进 acting seat 到下一
// 个存活的行动者。若已无存活行动者,调 endWolfPhase/endSeerPhase 等推进阶段。
// 调用方必须持有 r.mu 或由调用方加锁 — 本方法自行加锁。

// ─────────────────── Spectator API ───────────────────

// SpectateGame 注册 userID 为房间观察者;按需创建房间。不会消耗座位。幂等。

// UnspectateGame 取消观察者身份。

// SpectatorList 返回当前观察者 userID。

// SpectatorState 返回观察者可见的客户端视图。所有玩家 Role 隐藏。

// SpectatorView 与 SpectatorState 共用一份视图(供 hub 广播)。

// lockRoomBriefly tries to acquire r.mu with a bounded wait. Returns true if
// the lock was obtained (caller MUST call r.mu.Unlock()); false if the wait
// expired and the caller should fall back to a cached snapshot rather than
// blocking on long-running engine ops.
//
// BUG-WEREWOLF-P1-LOCK (Round 26): the engine's r.mu is a sync.Mutex held by
// every Action_* dispatch path and every BotAgent run goroutine. When an LLM
// retry / quarantine / auto-skip dispatch grabs the lock for the full duration
// of an upstream HTTP call, REST handlers that need a phase snapshot block
// behind it and appear to hang. With lockRoomBriefly, the REST path returns
// the last-known cached state instead of waiting indefinitely.
//
// 2026-07-18 BUG-R152-LLM-TIMEOUT-001 FIX (lock-poison fix): the original
// implementation spawned a goroutine that called r.mu.Lock(); if the waiter
// timed out the goroutine still eventually grabbed the lock, sent on the
// buffered channel (succeeded immediately), and returned WITHOUT ever calling
// r.mu.Unlock(). The room's mutex became permanently held by a dead
// goroutine — every subsequent r.mu.Lock() blocked forever, including the
// very phase-watchdog goroutine that is supposed to rescue stalled phases.
// Once poisoned, the room froze hard: not even a 13-bot speak phase could
// advance (observed Bot 8 Qwen 3.7-Max LLM timeout → room deadlocked at
// 1295s+). Rewritten to use TryLock polling with an explicit probe
// channel — the lock is released by whichever side wins: the waiter (caller
// MUST Unlock on true) or the polling loop (if the deadline expires first).
// We do NOT switch r.mu to sync.RWMutex here because there are dozens of
// write-side callers in this file and the conversion is out of scope here.

// ─────────────────── 人类等待窗口 (§130) ───────────────────

// shouldHumanWaitLocked 判定当前房间是否需要进入"人类等待窗口"。
//
// 触发条件(全部满足):
//  1. cfgWerewolfHumanWaitSec() > 0(配置启用)
//  2. 房间里有 Agent 座位(len(seatModelKeys) > 0)
//  3. 真人玩家存在(Seats[i] 至少一个不是 bot userID)
//     OR 观察者集合非空(Spectators)
//  4. 等待窗口未启用(humanWaitDeadlineAt 零值)
//  5. 房间尚未开始游戏(State.Phase == PhaseFilling)
//
// 不满足任一条件:走原 StartGame 路径,无延迟。
//
// 锁:调用者必须持有 r.mu。

// isBotUserIDLocked 判定 userID 是否为 bot userID。
//
// 锁:调用者必须持有 r.mu。
//
// 实现:
//  1. 若 m.hydrator 可用,通过 hydrator 拿到的 AgentSeatInfo.UserID 集合判断(精确)。
//  2. 否则用 "bot_" 前缀启发式兜底。

// tryStartWithHumanWaitLocked 判定是否启用人类等待窗口;若启用,设置 deadline
// 并启动 watchdog goroutine,返回 true;否则返回 false 让调用者走原 StartGame 路径。
//
// 锁:调用者必须持有 r.mu。

// humanWaitWatchdog 后台 goroutine:每 5s tick 检查等待窗口状态。
//   - deadline 到期 → 取消等待,执行 StartGame + StartAgentsLocked + wake
//   - 房间被拆 → 直接退出
//   - 房间内突然无人类 → 立即开始(无意义的等待无意义)

// completeHumanWaitAndStart 由 watchdog deadline 到期时调用,执行真正的
// StartGame + 启动 Agent + wake。房间状态机:PhaseFilling → 第一个夜间阶段。
