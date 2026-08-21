// Package config loads LsmAgentGame.conf (JSON) and exposes a typed view of it.
//
// Behavior:
//   - The runtime config is loaded from ./LsmAgentGame.conf.
//   - If the real file is missing, the loader falls back to ./LsmAgentGame.conf.example
//     so first-run onboarding is smooth (the example ships with placeholder secrets;
//     the operator must replace them before going live).
//   - Sensible defaults are applied for any field that is left blank.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
)

// Config is the root of LsmAgentGame.conf.
type Config struct {
	Server      ServerConfig      `json:"server"`
	DB          DBConfig          `json:"db"`
	JWT         JWTConfig         `json:"jwt"`
	Cookie      CookieConfig   `json:"cookie"`
	Captcha     CaptchaConfig     `json:"captcha"`
	Log         LogConfig         `json:"log"`
	CORS        CORSConfig        `json:"cors"`
	Game        GameConfig        `json:"game"`
	LLM         LLMConfig         `json:"llm"`
	Werewolf    WerewolfConfig    `json:"werewolf"`
	TexasHoldem TexasHoldemConfig `json:"texasholdem"`
}

// §128 对话即思考重构:AgentParallelConfig 已删除(原 §122)。
// LLM API 输出的 text + tool_use 即是模型"思考"的产物,无需辅助并行 worker。

// WerewolfConfig 狼人杀 7 人局首夜强制发言 + 思考与决策合并 + 时钟机制 + 聊天历史队列相关配置
// (Round 40 §95 / 2026-07-08 / 2026-07-09 §13 增强 / 2026-07-13 §130 重构)。
//
// 2026-07-13 §130 重大重构:删除 Agent 大模型 API 调用的竞争规则。
//   - 取消 FirstNightLLMCallMinIntervalSec:单 bot 不再有最小 LLM 调用间隔。
//   - 新增 HumanWaitSec:房间里有 Agent + 真人玩家/观察者时,StartGame 之前
//     等待该秒数,让人类在聊天室自由发言,Agent 启动后第一轮 LLM prompt
//     会吸收这些发言作为开局上下文。
//
// 2026-08-05 BUG-R242-P1-01: 恢复 RoomLLMConcurrency 房间级信号量。
// §130 "13 bot 完全并发" 实测导致上游代理被 13 bot × 内层重试打满 → 级联失败
// (27min 66% 失败率);恢复有界信号量(默认 4),槽位满时 bot 短暂等待后 reWake
// (瞬态,不计入 consecutiveFailures)。
//
// Defaults applied by applyDefaults:
//   - FirstNightGraceSec              = 120  缓冲期长度(秒)
//   - FirstNightForcedSpeakRounds     = 3    每名玩家至少发言轮次(1-3,clamp;2026-07-10 §116
//     从 1 提到 3,让 7 人局开局每人必发 3 轮,
//     充分抢身份、试探、推理、形成初步阵营)
//   - FirstNightSpeakMinIntervalSec   = 30   强制发言阶段 SpeakLimiter 间隔
//   - MinSpeaksPerMinute              = 2    白天发言阶段每分钟发言下限(0=禁用)
//   - ChatHistoryBytes                = 512000 (500K) 每 Agent 聊天历史队列字节上限
//   - PhaseDeadlineSec["pre_wolves"]  = 120
//   - PhaseDeadlineSec["speak"]       = 90
//   - PhaseDeadlineSec["vote"]        = 60
//   - PhaseDeadlineSec["sheriff"]     = 45
//   - PhaseDeadlineSec["hunter_shoot"]= 30
//   - PhaseDeadlineSec["dawn"]        = 8
//   - PhaseDeadlineSec["night_*"]     = 30-45
//   - SpectatorFullWake               = true 观众消息全阶段全频唤醒(移除 15s 节流)
//   - SpeakMaxRunes                   = 80   单条发言最大字符数(去重截断阈值)
//   - RoomLLMConcurrency              = 4    房间级 LLM 并发上限(BUG-R242-P1-01;
//     0=禁用/完全并发);槽位满时 bot 短暂等待后 reWake,不计入 consecutiveFailures
//   - DeathLyricEnabled               = true 启用遗言阶段(2026-07-09 新增);false 回退到旧行为(dawn 直接 → start_day)
//   - DeathLyricDeadlineSec           = 30   遗言每座位截止秒数
//   - HumanWaitSec                    = 60   §130 人类等待窗口秒数;0 = 禁用
//
// 全部为 0 时 applyDefaults 会填默认值。运行时修改 LsmAgentGame.conf 后
// 需要重启服务才能生效(config 是 sync.Once 加载的)。
type WerewolfConfig struct {
	FirstNightGraceSec            int `json:"first_night_grace_sec"`
	FirstNightForcedSpeakRounds   int `json:"first_night_forced_speak_rounds"`
	FirstNightSpeakMinIntervalSec int `json:"first_night_speak_min_interval_sec"`

	// §128 对话即思考重构:AgentParallel 字段已删除(原 §122)。
	// §130 重构(2026-07-13):FirstNightLLMCallMinIntervalSec 已删除 —
	// 取消单 bot LLM 调用最小间隔,允许每个 bot 按模型自身响应速率自由调用。

	// 2026-07-09 §13 增强
	MinSpeaksPerMinute int            `json:"min_speaks_per_minute"` // 每分钟发言下限,默认 2
	ChatHistoryBytes   int            `json:"chat_history_bytes"`    // 500K 队列上限,默认 512000
	PhaseDeadlineSec   map[string]int `json:"phase_deadline_sec"`    // 各阶段截止秒数
	SpectatorFullWake  bool           `json:"spectator_full_wake"`   // 观众全频唤醒开关,默认 true
	SpeakMaxRunes      int            `json:"speak_max_runes"`       // 单条发言最大字符数,默认 80

	// RoomLLMConcurrency 是房间级 LLM HTTP 调用并发上限(BUG-R242-P1-01)。
	// §130 曾删除此信号量以"让每个 bot 按模型自身响应速率自由调用",但实际效果
	// 是 13 bot × 内层重试 fully concurrent → 上游代理被瞬时打满 → 级联失败
	// (实测 6min 2.5% → 27min 66% 失败率,从 DouBao 扩散至多模型)。
	// 恢复为有界信号量:槽位满时 bot 短暂等待后 reWake(视为瞬态,不计入
	// consecutiveFailures),既限制在途调用数,又不让慢模型无限阻塞快模型。
	// 0 / 负值 = 禁用(完全并发,§130 行为,仅用于调试)。
	RoomLLMConcurrency int `json:"room_llm_concurrency"` // 房间级 LLM 并发上限,默认 4

	// §130 增强(2026-07-13) — 人类等待窗口秒数。
	// 当房间里有 Agent 参加(seatModelKeys 非空)且有人类玩家或观察者时,
	// StartGame 之前会先进入"人类等待窗口",持续 HumanWaitSec 秒。期间人类
	// 可在聊天室自由发言,Agent 启动后第一轮 LLM 会把这些发言作为开局上下文。
	// = 0 时禁用等待窗口,直接 StartGame(全 AI 房间默认 0,混合房间默认 60)。
	HumanWaitSec int `json:"human_wait_sec"`

	// BUG 2026-07-09: 遗言阶段配置
	DeathLyricEnabled     bool `json:"death_lyric_enabled"`      // 启用遗言阶段,默认 true
	DeathLyricDeadlineSec int  `json:"death_lyric_deadline_sec"` // 遗言每座位截止秒数,默认 30

	// 2026-07-14 BUG-R116-03 — 同一座位在一轮发言阶段的最小发言间隔(秒)。
	// 防止单个 Agent 因响应快/思考预算松而在同一轮内刷屏发言。默认 60s。
	SameSeatSpeakCooldownSec int `json:"same_seat_speak_cooldown_sec"`

	// 2026-07-15 BUG-R124-UI-001 — 单座位每阶段发言次数上限。
	// 防止 Qwen3.7-Max 等快模型在同一发言阶段占据 40%+ 发言量。
	// 当同座位在当前发言阶段累计发言数 ≥ 该值,新发言被 rate-limit 拒绝
	// 并 hint 给 LLM 让其收敛。默认 3。0 = 不限制(向后兼容)。
	MaxSpeaksPerPhasePerSeat int `json:"max_speaks_per_phase_per_seat"`

	// 2026-07-15 BUG-R124-PERF-002 / BUG-R125-PERF-001 / R131 — 单次 LLM 调用总超时(秒)。
	// 含所有重试;超过此时间强制 cancel 当前调用,推 quarantine 路径。
	// 默认 120s(R131 修复:三处统一 120s;原 60s 在 13 并发/慢模型场景下不足)。
	// 0 = 不强制超时(向后兼容)。
	LLMCallTimeoutSec int `json:"llm_call_timeout_sec"`

	// RestartVote 是 2026-07-10 新增的"游戏结束后重开局投票"配置。
	// 详见 docs/狼人杀-Agent与系统/狼人杀重开局投票设计.md。
	RestartVote RestartVoteConfig `json:"restart_vote"`

	// 2026-07-10 §125 增强 — Agent 法官配置。
	// JudgeMode: "ai"(默认)启用 AgentJudge; "off" 关闭回退 host driver。
	// JudgeModelKey: 法官使用的 LLM model_key; 空 = 取 providers[0]。
	// EnableModelMemoryRecap: true(默认)新一局第一轮 LLM 调用注入上一局记忆段。
	JudgeMode              string `json:"judge_mode"`
	JudgeModelKey          string `json:"judge_model_key"`
	EnableModelMemoryRecap bool   `json:"enable_model_memory_recap"`

	// 2026-07-12 §127 增强 — agent 外层 LLM 调用重试次数。
	// 当 callProvider 返回 retryable 错误时，外层重试循环最大次数。
	// 默认 5；0 时 fallback 到 5。
	LLMMaxRetries int `json:"llm_max_retries"`

	// 2026-07-15 R131 增强 — 大房间(13人局)宽松模式。
	// LenientModeForSeatCount: 当房间人数 >= 该值时启用更宽松的 quarantine 阈值
	// 与 LLM 调用超时缩放。0 表示禁用(向后兼容)。默认 13。
	LenientModeForSeatCount int `json:"lenient_mode_for_seat_count"`

	// 2026-07-21 §5.2 增强 — 开局狼队友互认概率(0-100 整数百分比)。
	// 默认 30:每个狼 bot 有 30% 概率开局即知道另一位狼队友身份(identity prompt
	// 注入"X 号是你的狼队友");0 = 完全关闭本设计;100 = 全部开局互认。
	// docs/狼人杀-道具与经济/狼人杀13人局道具系统设计.md §5.2 详解设计动机与权衡。
	WolfTeammateHintRate int `json:"wolf_teammate_hint_rate"`

	// 2026-07-21 v3 重构 — 开局狼队友互认每局最多几对。
	// 默认 1(每局最多 1 对狼互知 = 2 只狼);2 = 4 只狼全部互知(几乎"全狼抱团")。
	// docs/狼人杀-道具与经济/狼人杀13人局道具系统设计.md §4.1。
	WolfTeammateHintMaxPairs int `json:"wolf_teammate_hint_max_pairs"`

	// 2026-07-21 v2 重设计 — 房间级道具全局预算(金币)。
	// 本局所有玩家的道具消耗累计不得超过该值,逼人类/Agent 把道具当稀缺资源博弈
	// ("一方多用→另一方无道具可用")。默认 900 币(≈ 3~6 道具均价 × 容量系数)。
	// 0 = 不启用全局预算(仅保留个人上限+冷却)。
	// docs/狼人杀-道具与经济/狼人杀13人局道具系统设计.md §5.2。
	RoomPropBudget int64 `json:"room_prop_budget"`

	// 2026-07-21 v5 重构 — EconTier 5 档阈值(可由 LsmAgentGame.conf 覆盖)。
	// 必须单调：EconTierBoomThreshold > EconTierCautionThreshold > EconTierDangerThreshold > EconTierCriticalThreshold >= 0。
	// 默认值与 docs/狼人杀-道具与经济/狼人杀13人局道具系统设计.md §16.3 表一致。
	// = 0 走 werewolf 包常量默认值。
	EconTierBoomThreshold     int64 `json:"econ_tier_boom_threshold"`
	EconTierCautionThreshold  int64 `json:"econ_tier_caution_threshold"`
	EconTierDangerThreshold   int64 `json:"econ_tier_danger_threshold"`
	EconTierCriticalThreshold int64 `json:"econ_tier_critical_threshold"`

	// LLMTimeoutScalePercent: 宽松模式下 LLM 调用超时放大百分比(100 = 不变)。
	// 默认 150，即 120s * 1.5 = 180s。
	LLMTimeoutScalePercent int `json:"llm_timeout_scale_percent"`

	// 2026-07-24 §流式续命 — 首字节到达后的总超时(秒,作为最终兜底)。
	// 旧逻辑:单次 LLM 调用总超时 = LLMCallTimeoutSec(300s/480s),慢模型
	// 首字节 1-3min + 长 thinking + tool_use 经常逼近上限,即使后续持续有
	// token 输出也会被外层 ctx cancel → consecutiveFailures++ → 误 quarantine。
	// 新逻辑(2026-07-24):首字节前 = LLMCallTimeoutSec(熔断慢启动);首字节后
	// = LLMCallTimeoutSec + LLMStreamExtendedTimeoutSec 作为最终截止。
	// 默认 900 (15 min)。0 = 走代码内常量 defaultStreamExtendedTimeoutSec(900)。
	LLMStreamExtendedTimeoutSec int `json:"llm_stream_extended_timeout_sec"`

	// 2026-07-12 §129 增强 — 游戏结束后「冷却期」秒数。
	// 非 0 时, 一局结束 (Status="over" + Phase=PhaseGameOver) 后不立刻进入
	// PhaseRestartVote / forceCloseRoomLocked, 而是先进入「冷却期」:
	//   - 阶段字段继续是 PhaseGameOver, Status 保持 "over", 房间不释放
	//   - cooling watchdog 每 coolingTickInterval 秒探测一次人类存在
	//     (hub 玩家集合 + 观众集合任一非空 = 有人类在线)
	//   - 最后一名人类离开起 CoolingSec 秒内仍无人类加入 → 强制 forceCloseRoomLocked
	//   - 只要有人类在线就持续延长 CoolingSec 窗口(方便人类复盘整局 + 观看 BotTranscript)
	//   = 0 时禁用冷却期, 走立刻重开局投票 / 关闭的原行为。
	// 默认 1800 (30 分钟)。
	CoolingSec int `json:"cooling_sec"`

	// 2026-07-15 狼人杀 13 人局金币系统 — 底注彩池制配置。
	// AnteCoin: 每位参与者的底注,默认 100;=0 关闭金币博弈。
	// PotSplitEnabled: true(默认)启用彩池制(胜方分输家底注);
	//   false 走固定 ±Ante(胜方 +Ante/负方 -Ante)。
	// MinSeatForPot: 至少 N 个参与者(bot + 余额≥Ante 的人类)才开彩池,默认 2。
	AnteCoin        int  `json:"ante_coin"`
	PotSplitEnabled bool `json:"pot_split_enabled"`
	MinSeatForPot   int  `json:"min_seat_for_pot"`

	// 2026-07-20 §131 新增 — Agent 持久化记忆(MEMORY.md)。
	// AgentMemoryEnabled: true(默认)每局结束后按 model_key 自我迭代
	//   t_lsm_game_agent_memory 并在下一局注入;false 整链 no-op。
	// AgentMemoryMaxTokens: 自我迭代单次 LLM 调用的 MaxTokens 预算,默认 2048。
	AgentMemoryEnabled   bool `json:"agent_memory_enabled"`
	AgentMemoryMaxTokens int  `json:"agent_memory_max_tokens"`

	// 2026-08-13 §20260813-02 U4 — 记忆迭代单次 LLM 调用总超时(秒)。
	// 旧版硬编码 90s,慢模型(Kimi/GLM)迭代经常超时被 cut → 走 FallbackMerge。
	// 改走 §197 长预算思想:默认 480s(流式累积,与主对话一致)。0 = 代码内常量兜底。
	AgentMemoryIterTimeoutSec int `json:"agent_memory_iter_timeout_sec"`

	// 2026-08-13 §20260813-02 U1 — 局内 LLM 语义压缩(CompactWithLLM)开关与预算。
	// AgentCompactEnabled: true(默认)消息数超阈值时用 bot 自己的 provider
	//   把最早 1/3 消息压缩为结构化摘要;false 退回纯规则式压缩。
	// AgentCompactMaxTokens: 压缩调用 max_tokens 预算,默认 2048(§20260820-01
	//   从 1200 提到 2048,以容纳 8 段结构化摘要 ~640 字 + system 缓冲)。
	AgentCompactEnabled   bool `json:"agent_compact_enabled"`
	AgentCompactMaxTokens int  `json:"agent_compact_max_tokens"`

	// 2026-08-20 §20260820-01 — 8 段结构化摘要 + 视角隔离开关。
	// AgentCompactEightSectionsEnabled: true(默认) 启用 8 段结构化摘要;
	//   false 退回旧 4 段(向后兼容,允许灰度关闭)。同样需要 applyDefaults
	//   显式置 true(与 AgentCompactEnabled 同款 bool 零值陷阱)。
	AgentCompactEightSectionsEnabled bool `json:"agent_compact_eight_sections_enabled"`

	// 2026-08-10 §20260810-10 U2 新增 — 模型自画像注入开关。
	// true(默认)开局时按 modelKey 聚合 t_lsm_game_model_game_log 生成
	// 「🪞 模型自画像」段注入 Agent system prompt 末尾;false 整链 no-op
	// (system 输出与旧版逐字节一致)。由于 bool 零值为 false,与
	// AgentMemoryEnabled 一样需要 applyDefaults 显式置 true。
	ModelSelfPortraitEnabled bool `json:"model_self_portrait_enabled"`

	// 2026-08-10 §20260810-12 D1 — 决策留痕运行时回放开关。
	// true(默认) → 每个 Agent 的 BotTranscript.DecisionTrail 累计决策记录
	// (上限 30 条 FIFO),前端 HistoryDrawer 第 6 sub-tab「🧠 决策回放」渲染;
	// false → trail 完全不分配(零内存 / 零 wire 开销)。
	BotsLogDecisions bool `json:"bots_log_decisions"`

	// §20260811-06 U3 — reasoning_chain 工具开关。
	// true(默认) → LLM 可在关键决策前显性调用 reasoning_chain 工具公开推理链
	// (上限 10 条 FIFO),前端 HistoryDrawer 第 7 sub-tab「🧩 推理链」渲染;
	// false → 工具不挂载 + ReasoningChains 完全不分配(零内存 / 零 wire 开销)。
	ReasoningChainEnabled bool `json:"reasoning_chain_enabled"`

	// 2026-08-10 §20260810-12 D2 — 死者身份「终局延时揭晓」默认值(分钟)。
	// 由 RoomService.CreateRoomWithAgents 在缺失时兜底写入房间。
	// 可选值:0(立即揭晓,旧行为零回归)/ 5 / 15。=0 走代码内常量 0。
	DeathRevealDelayMinDefault int `json:"death_reveal_delay_min_default"`

	// 2026-07-23 R187-1 新增 — filling 阶段房间回收阈值(秒)。
	// 狼人杀房间创建后停在 PhaseFilling(等人入座)超过该秒数、且
	// hub 上无任何人类玩家/观察者连接时,由 janitor 周期的
	// JanitorSweepStaleFilling 强制解散(此前唯一兜底是 30 分钟的
	// JanitorSweepStale)。= 0 走默认值 300(5 分钟)。
	FillingReaperSec int `json:"filling_reaper_sec"`

	// §20260811-01 U3 — 投票阶段「半公开计票」悬念开关。
	// VoteSuspense: true 时投票结束后 3 秒内只显示"谁投了谁"，不显示得票数；
	//   3 秒后前端自动揭晓完整票型。纯前端视觉层改动，不改变后端投票逻辑。
	// VoteSuspenseDelayMs: 悬念持续毫秒数，默认 3000。
	VoteSuspense        bool `json:"vote_suspense"`
	VoteSuspenseDelayMs int  `json:"vote_suspense_delay_ms"`

	// 2026-08-11 §20260811-05 U1 新增 — Agent 玩家行为画像(PlayerProfile)。
	// PlayerProfileEnabled: true(默认)每局结束后对每个 (bot model_key ×
	//   人类 user_id) 组合异步迭代 t_lsm_game_agent_player_profile 并在下一局
	//   经房间级预取缓存注入 GameContext.PlayerProfiles;false 整链 no-op。
	// PlayerProfileMaxTokens: 画像迭代单次 LLM 调用的 MaxTokens 预算,默认 1024
	//   (画像比 MEMORY.md 短,预算减半)。
	PlayerProfileEnabled   bool `json:"player_profile_enabled"`
	PlayerProfileMaxTokens int  `json:"player_profile_max_tokens"`

	// 2026-08-11 §20260811-05 U2 新增 — 赛后复盘问答(RecallChat)。
	// RecallChatEnabled: true(默认)对局结束后(Status=="over")开放
	//   POST /api/games/werewolf/rooms/:roomId/recall_chat;false 直接 404。
	// RecallChatMaxTokens: 复盘回答单次 LLM 调用的 MaxTokens 预算,默认 1024。
	// RecallChatPerUserLimit: 每用户每房间提问次数上限,默认 10。
	RecallChatEnabled      bool `json:"recall_chat_enabled"`
	RecallChatMaxTokens    int  `json:"recall_chat_max_tokens"`
	RecallChatPerUserLimit int  `json:"recall_chat_per_user_limit"`
}

// RestartVoteConfig 控制狼人杀 7 人局结束后的"原地重开投票"行为。
// 启用时, 一局结束 (Status=over) 后玩家/Agent 拥有 RestartVoteDeadlineSec 秒
// 投票窗口; 同意比例 ≥ YesQuorumNumerator / YesQuorumDenominator 即原地
// 复用同一批座位 + 保留全部聊天数据开新一局; 拒绝或超时 → 关闭房间。
type RestartVoteConfig struct {
	Enabled              bool `json:"enabled"`                // 总开关, 默认 true
	DeadlineSec          int  `json:"deadline_sec"`           // 投票窗口秒数, 默认 300 (5 分钟)
	YesQuorumNumerator   int  `json:"yes_quorum_numerator"`   // 通过比例分子, 默认 2
	YesQuorumDenominator int  `json:"yes_quorum_denominator"` // 通过比例分母, 默认 3
	MinPlayers           int  `json:"min_players"`            // 至少 N 个玩家入座过才开投票, 默认 2
}

// DefaultSpeakFloorWindowSec 是 speakCounter 滑动窗口长度(60s)。
// 不可配置 — 与 §13 "每分钟 ≥2 次"语义绑定。
const DefaultSpeakFloorWindowSec = 60

// DefaultSpeakFloorWakeIntervalSec speak floor watchdog 强制唤醒间隔下限,防止 1 秒内连发。
const DefaultSpeakFloorWakeIntervalSec = 20

// TexasHoldemConfig 控制德州扑克 Bot Agent 与经济档位(v1.1,2026-08-19 §德州扑克Agent)。
//
//   - AgentEnabled:总开关,默认 true。false 时即使创建房间携带 agent_seats 也被忽略。
//   - AgentActionTimeoutSec:单 bot 决策 LLM 超时,超时服务端 fold 兜底。
//   - BotChatPerHand / BotChatMinIntervalSec:每手牌最多 N 次 + 相邻 N 秒节流(同 dispatch.go 默认)。
//   - RakeRatePct:标准档抽水率(Health 档默认 5%)。
//   - MaxPotPerHand:单手牌最大底池上限(防恶意刷金币)。
type TexasHoldemConfig struct {
	AgentEnabled         bool `json:"agent_enabled"`           // 默认 true
	AgentActionTimeoutSec int `json:"agent_action_timeout_sec"` // 默认 15s(R7 P0-2:原 30s 导致 LLM 失败时 bot 卡住 30s+)
	BotChatPerHand       int `json:"bot_chat_per_hand"`        // 默认 2
	BotChatMinIntervalSec int `json:"bot_chat_min_interval_sec"` // 默认 30s
	RakeRatePct          int `json:"rake_rate_pct"`             // 默认 5(Health 档);Caution 7、Danger 10 由代码常量定
	MaxPotPerHand        int `json:"max_pot_per_hand"`          // 默认 100000
}

// ServerConfig holds the listener addresses and TLS material.
type ServerConfig struct {
	HTTPSAddr      string `json:"https_addr"`
	WSSAddr        string `json:"wss_addr"`
	TLSCert        string `json:"tls_cert"`
	TLSKey         string `json:"tls_key"`
	DevMode        bool   `json:"dev_mode"`         // true → 启用 AgentBypassAccounts 白名单(CAPTCHA 旁路);生产必须 false
	RootAccount    string `json:"root_account"`     // 首次启动时种子的 root 账号;默认 "lsm_root"
	RootPassword   string `json:"root_password"`    // 首次启动时种子的 root 密码;空时由 main.go 随机生成并日志输出一次
	RootInviteCode string `json:"root_invite_code"` // 首次启动时种子的邀请码;空时由 main.go 随机生成
}

// DBConfig holds the MariaDB / MySQL connection.
type DBConfig struct {
	Host                   string `json:"host"`
	Port                   int    `json:"port"`
	Name                   string `json:"name"`
	User                   string `json:"user"`
	Password               string `json:"password"`
	MaxOpenConns           int    `json:"max_open_conns"`
	MaxIdleConns           int    `json:"max_idle_conns"`
	ConnMaxLifetimeSeconds int    `json:"conn_max_lifetime_seconds"`
}

// DSN renders a GORM-compatible DSN for the configured database.
func (d DBConfig) DSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		d.User, d.Password, d.Host, d.Port, d.Name)
}

// JWTConfig holds the signing secret and lifetime.
type JWTConfig struct {
	Secret     string `json:"secret"`
	TTLSeconds int    `json:"ttl_seconds"`
	Issuer     string `json:"issuer"`
}

// LogConfig holds the log level and output file.
type LogConfig struct {
	Level string `json:"level"`
	File  string `json:"file"`
}

// CORSConfig holds the allow-list of origins that may call the API.
type CORSConfig struct {
	AllowedOrigins []string `json:"allowed_origins"`
}

// CookieConfig holds the AES-256-GCM signing key, lifetime, and cookie name
// for the encrypted auth cookie issued by /api/auth/login.
//
// Defaults applied by applyDefaults (when omitted):
//   - Secret = JWT secret (single source of cryptographic material)
//   - TTLSeconds = 172800 (48 hours — matches the spec)
//   - Name = "lsm_auth"
//   - Secure = true (HTTPS only)
type CookieConfig struct {
	Secret     string `json:"secret"`
	TTLSeconds int    `json:"ttl_seconds"`
	Name       string `json:"name"`
	Secure     bool   `json:"secure"`
}

// CaptchaConfig holds the lifetime, length, and difficulty of the captcha.
//
// Defaults applied by applyDefaults:
//   - TTLSeconds = 180 (3 minutes)
//   - Length = 5 (alphanumeric, all caps)
type CaptchaConfig struct {
	TTLSeconds int `json:"ttl_seconds"`
	Length     int `json:"length"`
}

// GameConfig holds game-lobby settings.
//
// Defaults applied by applyDefaults:
//   - MaxRoom = 50 (max rooms per game kind)
type GameConfig struct {
	MaxRoom int `json:"max_room"`
}

// ProviderConfig describes one model entry under llm.providers[]. The API key
// MUST come from LsmAgentGame.conf (gitignored), never from source. See
// `docs/LLM与Agent/LLM供应商设计.md`.
//
// DEPRECATED (2026-07-10 kind-skipping-moth, hardened 2026-08-12): the
// runtime source of truth for LLM models is t_lsm_game_llm_provider, edited
// via the admin UI (/api/admin/llm/providers). The Providers slice is no
// longer read by NewRegistryWithDB — when the DB is empty the registry seeds
// from llm.DefaultProviders() (the code-level constant in
// ServerGo/llm/defaults.go) instead.
//
// This struct + the LLMConfig.Providers field are retained solely so existing
// LsmAgentGame.conf files that still carry a `providers` block continue to
// parse without breaking; NewRegistryWithDB will log a one-shot WARN when
// it sees a non-empty Providers field and ignore the entries. New code MUST
// edit t_lsm_game_llm_provider directly.
type ProviderConfig struct {
	AgentName    string `json:"agent_name"`    // human-readable label (UI)
	Model        string `json:"model"`         // model id sent to the proxy
	APIKey       string `json:"api_key"`       // Bearer token for this model
	ProviderType string `json:"provider_type"` // §20260814-01: "anthropic-messages" | "openai-completions" (legacy "anthropic"/"openai" accepted)
	// §R224 (2026-08-01) — 重新引入 §128 误删的 extended thinking 配置。
	// 实测 8 家代理(GLM/豆包/Qwen/DeepSeek/Kimi/MiniMax/美团/小米)要求
	// 每条 message 头部包含 `{type:"thinking", budget:N}` 块;ThinkingRequired=true
	// 时 anthropic.Provider 在 pre-flight 注入对应块;false 时不注入
	// (向后兼容某些不需要 thinking 的代理,如未来切换到 OpenAI 协议)。
	//
	// ThinkingBudget 是 budget 数值(典型 4096/8192;最小 1024)。
	// 历史:LsmAgentGame.conf.example 曾对全部 8 家默认代理设 true / 4096;
	// 该段已删除(2026-08-12),新 seed 走 llm.DefaultProviders()。
	ThinkingRequired bool `json:"thinking_required,omitempty"`
	ThinkingBudget   int  `json:"thinking_budget,omitempty"`
}

// LLMConfig holds the llm{} section of LsmAgentGame.conf — the shared proxy
// endpoint plus the list of available models. The endpoint is a base URL; the
// anthropic provider appends "/v1/messages".
//
// Defaults applied by applyDefaults:
//   - Endpoint = "" (空 — 必须在 LsmAgentGame.conf 显式提供)
//   - TimeoutMs = 600000 (10 minutes)
//   - StreamIdleTimeoutMs = 300000 (5 minutes)
//   - MaxRetries = 2
//
// BUG-WEREWOLF-P0-NEW-1 (revised 2026-07-09): the old 8s "fail-fast" timeout
// was killing real LLM calls (especially with extended thinking enabled, which
// needs 30-120s) before the model could respond. The previous rationale (avoid
// ~14min worst-case block when stacked with retries) is now addressed by:
//
//	(a) raising the timeout to 5min so genuine thinking calls complete,
//	(b) the phase-deadline floor in engine.cfgPhaseDeadlineSec guarantees the
//	    watchdog never force-skips a phase shorter than llm_timeout + 30s, and
//	(c) the per-bot LLMCallInProgress flag + UI countdown gives operators
//	    visibility into "is this bot actually thinking or hung?".
//
// 2026-07-24 优化: 默认 5min → 10min。生产日志显示慢模型(Kimi/GLM)单次
// 响应 2-5 分钟,300s 预算叠加 7 次外层重试后仍触发 werewolf.llm_call_timeout
// cancel 推入 quarantine 路径。2 分钟以上、最长 5 分钟的首字/间隔延时是
// 预期场景,不应作为"卡死"处理。运行时 LsmAgentGame.conf 显式配置优先,此
// 处仅作缺省兜底。
//
// NOTE (kind-skipping-moth §3, 2026-07-10; hardened 2026-08-12): the
// Providers slice is now DEPRECATED as the runtime source of truth. Models
// live in t_lsm_game_llm_provider and are loaded by llm.NewRegistryWithDB at
// boot. The field is retained so existing LsmAgentGame.conf files don't
// break JSON parsing; if Providers is non-empty AND the DB has rows, the DB
// wins and a deprecation warning is logged. If the DB is empty, the registry
// now seeds from llm.DefaultProviders() (the code-level constant in
// ServerGo/llm/defaults.go) instead of from cfg.Providers.
type LLMConfig struct {
	// Endpoint is DEPRECATED as the primary source of truth — see Endpoints
	// below. Kept so existing LsmAgentGame.conf files keep working. When both
	// fields are present Endpoints wins; when only Endpoint is set the
	// registry constructs a single-element Endpoints slice so existing
	// behavior is preserved.
	Endpoint   string `json:"endpoint"`
	TimeoutMs  int    `json:"timeout_ms"`
	MaxRetries int    `json:"max_retries"`

	// Endpoints (BUG-R220) is the failover endpoint list. The first element
	// is the primary; the rest are tried in order when the primary fails
	// (HTTP 5xx, dial / network error, or breaker-open). A non-retryable
	// error from the FIRST working endpoint is returned as-is; we only
	// advance through the list on transient/transport-level failures.
	//
	// When this list is empty the registry falls back to a single-element
	// slice built from `Endpoint`, so existing configs (and the example
	// shipped in LsmAgentGame.conf.example) keep behaving exactly as before.
	Endpoints []string `json:"endpoints,omitempty"`
	// Providers is DEPRECATED. See note above. Kept for backward compatibility
	// with existing LsmAgentGame.conf files; NewRegistryWithDB no longer
	// reads this field — when t_lsm_game_llm_provider is empty the registry
	// seeds from llm.DefaultProviders() instead. New code MUST edit
	// t_lsm_game_llm_provider (via /api/admin/llm/providers) instead of editing
	// the conf file.
	Providers []ProviderConfig `json:"providers"`

	// StreamIdleTimeoutMs caps the wall time between successful SSE Read()
	// calls (i.e. inter-chunk idle). MUST be large enough to tolerate the
	// extended-thinking "first token pause" (30-90s for thinking-type models
	// before they emit any content_delta). 2026-07-24 优化: 默认 120s → 300s
	// (5 min) — 生产日志显示慢模型(Kimi/GLM)首字延时可达 2 分钟以上,
	// 120s 窗口会把"慢但正常"的流误判为停滞而 Retryable 失败。
	// When 0 the registry falls back to 300s. See registry.NewRegistry.
	StreamIdleTimeoutMs int `json:"stream_idle_timeout_ms,omitempty"`
}

var (
	once sync.Once
	cfg  *Config
)

// Load returns the singleton Config. Subsequent calls return the cached value.
//
// Config resolution order:
//  1. $LSM_CONF  (absolute path — used by tests/CI to bypass cwd quirks)
//  2. ./LsmAgentGame.conf
//  3. ./LsmAgentGame.conf.example  (development-only fallback; placeholder secrets)
//
// First-run onboarding (2026-08-13 §config-auto-bootstrap):
//
//	When neither LsmAgentGame.conf nor the .example are present (e.g. a fresh
//	git clone without copying the example), Load() refuses to panic — it
//	regenerates LsmAgentGame.conf.example from the in-process defaults and
//	then writes a fully-populated LsmAgentGame.conf so the user has a real
//	file to edit before the first production launch. A one-shot INFO message
//	is printed to stderr telling the operator to replace the placeholder
//	secrets and rerun.
//
//	When LsmAgentGame.conf is missing but the .example exists (the common
//	case), Load() copies the .example verbatim to LsmAgentGame.conf first so
//	the operator has a working runtime config out of the box (no manual `cp`
//	step required). The copy preserves comments because we copy bytes — the
//	JSON parser is only used for in-memory defaults, never for the on-disk
//	artifact the operator will edit.
func Load() *Config {
	once.Do(func() {
		// (1) Ensure a writable LsmAgentGame.conf exists. We may have just
		// cloned the repo (no conf) or be running fresh in a CI sandbox. Try
		// to bootstrap before the read loop so Load() never panics on a
		// missing file. Bootstrap failures are non-fatal: we still proceed
		// to the read loop, and if all candidates are missing we panic with
		// a clearer message than "no such file".
		if err := ensureRuntimeConfigFile(); err != nil {
			fmt.Fprintf(os.Stderr,
				"[config] WARN: failed to bootstrap LsmAgentGame.conf: %v\n",
				err)
		}

		candidates := []string{
			os.Getenv("LSM_CONF"),
			"./LsmAgentGame.conf",
			"./LsmAgentGame.conf.example",
		}
		var (
			raw []byte
			err error
		)
		for _, p := range candidates {
			if p == "" {
				continue
			}
			raw, err = readConfigFile(p)
			if err == nil {
				break
			}
		}
		if err != nil {
			panic(fmt.Errorf("config: cannot read LsmAgentGame.conf or .example: %w", err))
		}
		parsed := Config{}
		if err := json.Unmarshal(raw, &parsed); err != nil {
			panic(fmt.Errorf("config: parse LsmAgentGame.conf: %w", err))
		}
		applyDefaults(&parsed)
		cfg = &parsed
	})
	return cfg
}

// SetForTest overrides the singleton Config for tests and returns a restore
// function. It exists because §20260813-03 (config auto-bootstrap) made
// Load() succeed even without an operator-provided LsmAgentGame.conf, so
// tests can no longer rely on "Load panics → recover fallback" to reach
// config-disabled code paths. Callers must not run in parallel with other
// config consumers (no t.Parallel).
func SetForTest(c *Config) (restore func()) {
	prev := Load()
	cfg = c
	return func() { cfg = prev }
}

// ensureRuntimeConfigFile makes sure ./LsmAgentGame.conf exists on disk. If
// only the .example is present we copy it byte-for-byte (preserving comments)
// into LsmAgentGame.conf; if neither is present we materialize both from the
// in-process Default* constants so a fresh clone is one `./rebuild_restart_app.sh`
// away from a working deployment.
//
// This is intentionally idempotent: if LsmAgentGame.conf already exists we
// do nothing. Operators can hand-edit the file freely and we will not
// overwrite their work.
func ensureRuntimeConfigFile() error {
	if _, err := os.Stat("./LsmAgentGame.conf"); err == nil {
		// Already present — leave operator edits alone.
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat LsmAgentGame.conf: %w", err)
	}

	// Missing — try the .example first (the most common path).
	if data, err := os.ReadFile("./LsmAgentGame.conf.example"); err == nil {
		if err := os.WriteFile("./LsmAgentGame.conf", data, 0o600); err != nil {
			return fmt.Errorf("write LsmAgentGame.conf from .example: %w", err)
		}
		fmt.Fprintf(os.Stderr,
			"[config] INFO: LsmAgentGame.conf was missing — copied from LsmAgentGame.conf.example. "+
				"Edit it (db.password, jwt.secret, llm.endpoint) before going live.\n")
		return nil
	}

	// No .example either — synthesize a minimal but valid config from the
	// defaults. This handles the truly-empty repo state (e.g. test sandbox
	// where we did not vendor the example file).
	synth := synthesizeDefaultConfig()
	if err := writeConfigFile("./LsmAgentGame.conf.example", synth); err != nil {
		return fmt.Errorf("write synthetic .example: %w", err)
	}
	if err := writeConfigFile("./LsmAgentGame.conf", synth); err != nil {
		return fmt.Errorf("write synthetic LsmAgentGame.conf: %w", err)
	}
	fmt.Fprintf(os.Stderr,
		"[config] INFO: no LsmAgentGame.conf and no .example found — generated both from code defaults. "+
			"Replace placeholder secrets (db.password, jwt.secret) before going live.\n")
	return nil
}

// synthesizeDefaultConfig returns the minimum-viable Config that satisfies
// applyDefaults' contract (every non-zero field is its default). Used as a
// last-resort bootstrap when neither conf file is present.
func synthesizeDefaultConfig() Config {
	c := Config{}
	applyDefaults(&c)
	return c
}

// readConfigFile returns the bytes of a config file or an error.
func readConfigFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !json.Valid(data) {
		// Strip // line comments and /* */ block comments before retrying.
		stripped := stripJSONComments(string(data))
		if !json.Valid([]byte(stripped)) {
			return nil, fmt.Errorf("%s: not valid JSON", path)
		}
		return []byte(stripped), nil
	}
	return data, nil
}

// stripJSONComments removes // and /* */ comments. Naive but good enough for hand-edited config.
func stripJSONComments(s string) string {
	var b strings.Builder
	inString, inLine, inBlock := false, false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inLine {
			if c == '\n' {
				inLine = false
				b.WriteByte(c)
			}
			continue
		}
		if inBlock {
			if c == '*' && i+1 < len(s) && s[i+1] == '/' {
				inBlock = false
				i++
			}
			continue
		}
		if inString {
			b.WriteByte(c)
			if c == '\\' && i+1 < len(s) {
				b.WriteByte(s[i+1])
				i++
				continue
			}
			if c == '"' {
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
			b.WriteByte(c)
			continue
		}
		if c == '/' && i+1 < len(s) && s[i+1] == '/' {
			inLine = true
			i++
			continue
		}
		if c == '/' && i+1 < len(s) && s[i+1] == '*' {
			inBlock = true
			i++
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// applyDefaults fills in blanks with reasonable defaults.
func applyDefaults(c *Config) {
	if c.Server.HTTPSAddr == "" {
		c.Server.HTTPSAddr = "0.0.0.0:39001"
	}
	if c.Server.WSSAddr == "" {
		c.Server.WSSAddr = "0.0.0.0:39002"
	}
	if c.Server.TLSCert == "" {
		c.Server.TLSCert = "./server.crt"
	}
	if c.Server.TLSKey == "" {
		c.Server.TLSKey = "./server.key"
	}
	if c.DB.MaxOpenConns == 0 {
		c.DB.MaxOpenConns = 20
	}
	if c.DB.MaxIdleConns == 0 {
		c.DB.MaxIdleConns = 5
	}
	if c.DB.ConnMaxLifetimeSeconds == 0 {
		c.DB.ConnMaxLifetimeSeconds = 3600
	}
	if c.JWT.TTLSeconds == 0 {
		c.JWT.TTLSeconds = 7200
	}
	if c.JWT.Issuer == "" {
		c.JWT.Issuer = "LsmAgentGame"
	}
	// Cookie: reuse the JWT secret so operators only rotate one key.
	if c.Cookie.Secret == "" {
		c.Cookie.Secret = c.JWT.Secret
	}
	if c.Cookie.TTLSeconds == 0 {
		c.Cookie.TTLSeconds = 172800 // 48 hours per spec
	}
	if c.Cookie.Name == "" {
		c.Cookie.Name = "lsm_auth"
	}
	if !c.Cookie.Secure {
		// Default to true (HTTPS-only). Operators can set it to false in dev if
		// they really need to inspect cookies over plain HTTP.
		c.Cookie.Secure = true
	}
	if c.Captcha.TTLSeconds == 0 {
		c.Captcha.TTLSeconds = 180
	}
	if c.Captcha.Length == 0 {
		c.Captcha.Length = 5
	}
	if c.Log.Level == "" {
		c.Log.Level = "info"
	}
	if c.Log.File == "" {
		c.Log.File = "./LsmAgentGame.log"
	}
	if len(c.CORS.AllowedOrigins) == 0 {
		c.CORS.AllowedOrigins = []string{
			"https://localhost:39001",
			"https://127.0.0.1:39001",
		}
	}
	if c.Game.MaxRoom == 0 {
		c.Game.MaxRoom = 50
	}
	// 不再设置默认 LLM endpoint。空配置应在启动时由 main.go 主动拒绝
	//(避免上游 proxy 拓扑泄漏到公开仓库的默认行为)。生产部署必须在
	// LsmAgentGame.conf 中显式提供 llm.endpoint 或 llm.endpoints[]。
	// 旧的默认值 "<internal-proxy>:29000/Anthropic" 已移除以减少公开仓库
	// 中的拓扑泄漏面。
	//
	// 此处不调用 logger(避免 config 包依赖 logger 包)。main.go 在
	// applyDefaults 之后会主动检查 LLM 配置是否为空并输出 WARN 日志。
	// BUG-R220: fold the legacy Endpoint into the Endpoints failover list
	// when the operator hasn't provided an explicit list. Trimming whitespace
	// + dropping empties keeps a copy-pasted multi-line config from
	// accidentally leaving a blank entry that would short-circuit failover.
	if len(c.LLM.Endpoints) == 0 {
		if ep := strings.TrimSpace(c.LLM.Endpoint); ep != "" {
			c.LLM.Endpoints = []string{ep}
		}
	} else {
		cleaned := c.LLM.Endpoints[:0]
		for _, ep := range c.LLM.Endpoints {
			if ep = strings.TrimSpace(ep); ep != "" {
				cleaned = append(cleaned, ep)
			}
		}
		c.LLM.Endpoints = cleaned
		if len(c.LLM.Endpoints) == 0 && strings.TrimSpace(c.LLM.Endpoint) != "" {
			c.LLM.Endpoints = []string{strings.TrimSpace(c.LLM.Endpoint)}
		}
	}
	if c.LLM.TimeoutMs == 0 {
		// 2026-07-24 优化: 300000 → 600000 (10 min)。慢模型(Kimi/GLM)单次
		// 响应 2-5 分钟是预期场景;叠加 7 次外层重试(累计 ~44s backoff)后
		// 5min 预算仍会把正常调用 cancel 进 quarantine 路径。
		c.LLM.TimeoutMs = 600000
	}
	if c.LLM.StreamIdleTimeoutMs <= 0 {
		// 2026-07-24 优化: 120000 → 300000 (5 min)。SSE 首字/间隔空闲窗口,
		// 只要流持续就不中断;2 分钟以上的 thinking 首字延时是预期场景。
		c.LLM.StreamIdleTimeoutMs = 300000
	}
	if c.LLM.MaxRetries == 0 {
		// 2026-07-15 R131 修复: 2→3(内层额外多 1 次重试 + 累计 0.5+1+2=3.5s backoff,
		// 配合外层 5 次 + 120s call timeout 更宽容)。
		c.LLM.MaxRetries = 3
	}
	// BUG-R229-P0-01 (2026-08-01) — 防御性默认: 所有 anthropic 协议 provider 默认
	// thinking_required=true / thinking_budget=4096。上游代理(LsmHttpAgent → 真实
	// 厂商 API)的 anthropic 协议转换层要求所有模型请求体都带顶层 thinking 块,否则
	// 400 "missing messages.content.thinking parameter"。实测 8 家代理全部需要 thinking。
	// 历史缺陷:LsmAgentGame.conf 中只有 2 家配了 thinking_required=true,其余 6 家
	// Agent 首夜即被永久 quarantine → 对局卡死。修复:applyDefaults 把 anthropic 协议
	// provider 的零值(false / budget=0)改写为 true / 4096,DB 空行 seed 路径读 cfg,
	// 所以也会被改写;存量 DB 行在 registry.Reload 时自愈(见 registry.go)。
	for i := range c.LLM.Providers {
		// §20260814-01 — thinking 自愈仅对 anthropic-messages 协议生效
		// (openai-completions 无 thinking 概念,强制不改写)。此处内联归一化
		// 以避免 import llm/types 产生循环依赖(config 被 llm 依赖)。
		switch strings.ToLower(strings.TrimSpace(c.LLM.Providers[i].ProviderType)) {
		case "", "anthropic", "anthropic-messages":
			// anthropic-messages 族 → 继续下面自愈逻辑。
		default:
			// openai / openai-completions / 未知 → 跳过,不改写 thinking。
			continue
		}
		if !c.LLM.Providers[i].ThinkingRequired && c.LLM.Providers[i].ThinkingBudget <= 0 {
			c.LLM.Providers[i].ThinkingRequired = true
			c.LLM.Providers[i].ThinkingBudget = 4096
		}
	}
	// WerewolfConfig (Round 40 §95) — 首夜强制发言默认值。
	if c.Werewolf.FirstNightGraceSec == 0 {
		c.Werewolf.FirstNightGraceSec = 120
	}
	// 2026-07-10 §116: 默认值从 1 提到 3,狼人杀 7 人局开局每人必发 3 轮
	// 强制发言,充实首夜缓冲期的策略博弈; 0 / 负值 / >3 仍由
	// werewolf.getForcedSpeakRounds clamp 到 [1,3]。
	if c.Werewolf.FirstNightForcedSpeakRounds == 0 {
		c.Werewolf.FirstNightForcedSpeakRounds = 3
	}
	if c.Werewolf.FirstNightSpeakMinIntervalSec == 0 {
		c.Werewolf.FirstNightSpeakMinIntervalSec = 30
	}
	// §130 重构(2026-07-13):FirstNightLLMCallMinIntervalSec 默认值已删除。
	// 每个 bot 现在按模型自身响应速率自由调用 LLM,不再有最小调用间隔。
	// 2026-07-09 §13 增强 — 思考与决策合并 + 时钟 + 聊天历史队列
	if c.Werewolf.MinSpeaksPerMinute == 0 {
		c.Werewolf.MinSpeaksPerMinute = 2
	}
	if c.Werewolf.ChatHistoryBytes == 0 {
		// 500K = 500*1024 — 与 agent.DefaultChatHistoryCapBytes 保持一致。
		// 此处直接 hardcode 字面量避免 config → agent 反向依赖。
		c.Werewolf.ChatHistoryBytes = 500 * 1024
	}
	if c.Werewolf.SpeakMaxRunes == 0 {
		c.Werewolf.SpeakMaxRunes = 80
	}
	if !c.Werewolf.SpectatorFullWake {
		// 默认 true(全频唤醒);只在 operator 显式设 false 时回退旧行为
		c.Werewolf.SpectatorFullWake = true
	}
	if c.Werewolf.JudgeMode == "" {
		// 2026-07-30 §重构:三选项(ai/human/off)合并为(agent/human/off);
		// 默认值对齐新契约;旧 cfg 中的 "ai" 仍可被识别(见
		// cfgWerewolfJudgeMode 的归一化处理)。
		c.Werewolf.JudgeMode = "agent"
	}
	// §130 重构(2026-07-13):RoomLLMConcurrency 默认值已删除。
	// 13 bot 现在完全并发调用 LLM,公平性由模型自身响应速率决定。
	if c.Werewolf.RoomLLMConcurrency == 0 {
		// BUG-R242-P1-01: 恢复房间级并发上限。13 bot fully concurrent × 重试
		// 会打满上游代理 → 级联失败;4 个槽位足以让快模型不被慢模型阻塞(槽位
		// 满时 reWake 而非阻塞),同时把在途调用数限制在代理可承受范围内。
		c.Werewolf.RoomLLMConcurrency = 4
	}
	if c.Werewolf.HumanWaitSec == 0 {
		// 默认 60 秒。混合房间(有人类玩家或观察者)在 StartGame 前等待该时长,
		// 允许人类在聊天室发言,Agent 启动后第一轮 LLM 会吸收这些发言。
		// 全 AI 房间(无真人)走 tryStartWithHumanWaitLocked 的判断逻辑直接跳过。
		c.Werewolf.HumanWaitSec = 60
	}
	// BUG 2026-07-09: 遗言阶段默认启用,每座位 30s 截止。
	// BUG-R70-P1: 必须先初始化 DeathLyricDeadlineSec,再用于 PhaseDeadlineSec["death_lyric"]。
	if !c.Werewolf.DeathLyricEnabled {
		c.Werewolf.DeathLyricEnabled = true
	}
	if c.Werewolf.DeathLyricDeadlineSec == 0 {
		c.Werewolf.DeathLyricDeadlineSec = 30
	}
	// 2026-07-10: 重开局投票默认启用; DeadlineSec=300(5 分钟);
	// 通过比例 ≥ 2/3 + 1 票 (实现里 +1); MinPlayers=2 (单人不进入投票)。
	if !c.Werewolf.RestartVote.Enabled {
		c.Werewolf.RestartVote.Enabled = true
	}
	if c.Werewolf.RestartVote.DeadlineSec == 0 {
		c.Werewolf.RestartVote.DeadlineSec = 300
	}
	if c.Werewolf.RestartVote.YesQuorumNumerator == 0 {
		c.Werewolf.RestartVote.YesQuorumNumerator = 2
	}
	if c.Werewolf.RestartVote.YesQuorumDenominator == 0 {
		c.Werewolf.RestartVote.YesQuorumDenominator = 3
	}
	if c.Werewolf.RestartVote.MinPlayers == 0 {
		c.Werewolf.RestartVote.MinPlayers = 2
	}

	// 2026-08-19 §德州扑克Agent — 房间级 TexasHoldem 配置默认值。
	// AgentEnabled 默认 true(开发期);生产 operator 可设为 false 关闭全部 Bot。
	if !c.TexasHoldem.AgentEnabled {
		c.TexasHoldem.AgentEnabled = true
	}
	// 2026-08-21 §P0-2: 默认超时 45s。实测多模型(Gemini/MeiTuan/Xiaomi)决策耗时 8~30s+,
	// 原 15s 导致 3/5 模型全灭 context deadline exceeded;watchdog 兜底 fold 等待期 = 45+10s。
	if c.TexasHoldem.AgentActionTimeoutSec == 0 {
		c.TexasHoldem.AgentActionTimeoutSec = 45
	}
	if c.TexasHoldem.BotChatPerHand == 0 {
		c.TexasHoldem.BotChatPerHand = 2
	}
	if c.TexasHoldem.BotChatMinIntervalSec == 0 {
		c.TexasHoldem.BotChatMinIntervalSec = 30
	}
	if c.TexasHoldem.RakeRatePct == 0 {
		c.TexasHoldem.RakeRatePct = 5
	}
	if c.TexasHoldem.MaxPotPerHand == 0 {
		c.TexasHoldem.MaxPotPerHand = 100000
	}
	// §128 对话即思考重构:AgentParallel 默认值已删除(原 §122)。

	// 2026-07-12 §127 增强 — agent 外层 LLM 调用重试次数。
	// 2026-07-15 R131 修复: 默认 7→5(累计 backoff 1+2+4+8+8 cap=23s,留 97s
	// 给真正 LLM 调用;7 次累计 127s 远超 cfgLLMCallTimeoutSec=120s,后 3 次白跑)。
	// 2026-07-24 优化:5→7(线性 2s/4s/6s/8s/8s + 后续 8s cap,累计 2+4+6+8+8+8+8=44s,
	// 仍小于 120s call timeout),给上游更宽容的恢复时间,减少被批量 quarantine。
	if c.Werewolf.LLMMaxRetries == 0 {
		c.Werewolf.LLMMaxRetries = 7
	}
	if c.Werewolf.MaxSpeaksPerPhasePerSeat == 0 {
		c.Werewolf.MaxSpeaksPerPhasePerSeat = 3
	}
	// 2026-07-15 R131 修复: 三处(注释/applyDefaults/fallback)统一为 120s。
	// 13 并发/慢模型场景下 60s 容易触发误 timeout,120s 给足响应时间。
	// 2026-07-24 优化: 120 → 300 (5 min)。慢模型(Kimi/GLM)单次响应 2-5
	// 分钟,120s 调用超时把正常慢调用 cancel 推入 quarantine;lenient ×150%
	// = 450s ≈ 7.5min,仍在 timeout_ms=600s 预算内(见 cfgLLMCallTimeoutSecScaled cap 上调)。
	if c.Werewolf.LLMCallTimeoutSec == 0 {
		c.Werewolf.LLMCallTimeoutSec = 300
	}

	// 2026-07-15 R131 增强 — 大房间宽松模式默认启用。
	if c.Werewolf.LenientModeForSeatCount == 0 {
		c.Werewolf.LenientModeForSeatCount = 13
	}
	if c.Werewolf.LLMTimeoutScalePercent == 0 {
		c.Werewolf.LLMTimeoutScalePercent = 150
	}
	// 2026-07-24 §流式续命:首字节后总超时默认值 900s (15 min)。
	// 与其它 §13/§130 字段保持"0 = 走默认"语义一致;operator 若真要
	// 关闭本机制可设为 -1(代码内 _ = 0 判定)。
	if c.Werewolf.LLMStreamExtendedTimeoutSec == 0 {
		c.Werewolf.LLMStreamExtendedTimeoutSec = 900
	}
	// 2026-07-21 §5.2: 开局狼队友互认概率。显式置 0 也回退到默认 30,
	// 与 LenientModeForSeatCount 等其它 §13/§130 字段保持一致;
	// 若 operator 真要禁用,把 werewolf.wolf_teammate_hint_rate 设为 -1。
	if c.Werewolf.WolfTeammateHintRate == 0 {
		c.Werewolf.WolfTeammateHintRate = 30
	}
	// 2026-07-21 v3: 开局狼队友互认每局最多几对(0/未设置 → 默认 1 对 = 2 只狼)。
	if c.Werewolf.WolfTeammateHintMaxPairs == 0 {
		c.Werewolf.WolfTeammateHintMaxPairs = 1
	}
	// 2026-07-21 v2: 房间级道具全局预算默认 900 币。
	if c.Werewolf.RoomPropBudget == 0 {
		c.Werewolf.RoomPropBudget = 900
	}

	// 2026-07-21 v5: EconTier 5 档阈值默认值。
	// 配置缺失 = 0 时保留 werewolf 包常量默认值;
	// 若 4 个字段全部显式非零,启动期会调 werewolf.ConfigureEconTier 注入。
	// 见 cmd/main.go 启动流程。
	// 这里不主动设置默认值,避免与 werewolf 包常量重复(单一来源)。

	// 2026-07-12 §129 增强 — 冷却期默认 30 分钟。
	if c.Werewolf.CoolingSec == 0 {
		c.Werewolf.CoolingSec = 1800
	}

	// 2026-07-15 狼人杀 13 人局金币系统 — 底注彩池制默认值。
	if c.Werewolf.AnteCoin == 0 {
		c.Werewolf.AnteCoin = 100
	}
	if !c.Werewolf.PotSplitEnabled {
		c.Werewolf.PotSplitEnabled = true
	}
	if c.Werewolf.MinSeatForPot == 0 {
		c.Werewolf.MinSeatForPot = 2
	}

	// 2026-07-20 §131 新增 — Agent 持久化记忆默认值。
	// 与 SpectatorFullWake 同模式:bool 默认 true,operator 需显式设 false 关闭。
	if !c.Werewolf.AgentMemoryEnabled {
		c.Werewolf.AgentMemoryEnabled = true
	}
	if c.Werewolf.AgentMemoryMaxTokens <= 0 {
		c.Werewolf.AgentMemoryMaxTokens = 2048
	}

	// 2026-08-13 §20260813-02 U4 — 记忆迭代超时默认 480s(流式长预算)。
	if c.Werewolf.AgentMemoryIterTimeoutSec <= 0 {
		c.Werewolf.AgentMemoryIterTimeoutSec = 480
	}

	// 2026-08-13 §20260813-02 U1 — 局内 LLM 语义压缩默认启用。
	// 与 AgentMemoryEnabled 同模式:bool 默认 true,operator 需显式设 false 关闭。
	if !c.Werewolf.AgentCompactEnabled {
		c.Werewolf.AgentCompactEnabled = true
	}
	// 2026-08-20 §20260820-01 — MaxTokens 默认值从 1200 提到 2048(8 段 schema)。
	if c.Werewolf.AgentCompactMaxTokens <= 0 {
		c.Werewolf.AgentCompactMaxTokens = 2048
	}

	// 2026-08-20 §20260820-01 — 8 段结构化摘要默认启用。
	// 与 AgentCompactEnabled 同模式:bool 默认 true,operator 需显式设 false 关闭
	// (允许灰度退回旧 4 段 schema)。
	if !c.Werewolf.AgentCompactEightSectionsEnabled {
		c.Werewolf.AgentCompactEightSectionsEnabled = true
	}

	// 2026-08-10 §20260810-10 U2 — 模型自画像默认启用。
	// 与 AgentMemoryEnabled 同模式:bool 默认 true,operator 需显式设 false 关闭。
	if !c.Werewolf.ModelSelfPortraitEnabled {
		c.Werewolf.ModelSelfPortraitEnabled = true
	}

	// 2026-08-10 §20260810-12 D1 — 决策留痕默认启用。
	// 与 ModelSelfPortraitEnabled 同模式:bool 默认 true,operator 需显式设 false 关闭。
	if !c.Werewolf.BotsLogDecisions {
		c.Werewolf.BotsLogDecisions = true
	}

	// §20260811-06 U3 — 推理链开关默认启用。
	// 与 BotsLogDecisions 同模式:bool 默认 true,operator 需显式设 false 关闭。
	if !c.Werewolf.ReasoningChainEnabled {
		c.Werewolf.ReasoningChainEnabled = true
	}

	// 2026-08-11 §20260811-05 U1 — 玩家行为画像默认启用。
	if !c.Werewolf.PlayerProfileEnabled {
		c.Werewolf.PlayerProfileEnabled = true
	}
	if c.Werewolf.PlayerProfileMaxTokens <= 0 {
		c.Werewolf.PlayerProfileMaxTokens = 1024
	}

	// 2026-08-11 §20260811-05 U2 — 赛后复盘问答默认启用。
	if !c.Werewolf.RecallChatEnabled {
		c.Werewolf.RecallChatEnabled = true
	}
	if c.Werewolf.RecallChatMaxTokens <= 0 {
		c.Werewolf.RecallChatMaxTokens = 1024
	}
	if c.Werewolf.RecallChatPerUserLimit <= 0 {
		c.Werewolf.RecallChatPerUserLimit = 10
	}

	// 2026-07-23 R187-1 新增 — filling 阶段回收阈值默认 5 分钟。
	if c.Werewolf.FillingReaperSec == 0 {
		c.Werewolf.FillingReaperSec = 300
	}

	// 2026-07-14 BUG-R116-03 — 同一座位单轮发言最小间隔默认 60s。
	if c.Werewolf.SameSeatSpeakCooldownSec == 0 {
		c.Werewolf.SameSeatSpeakCooldownSec = 60
	}

	if c.Werewolf.PhaseDeadlineSec == nil {
		// Acting-phase deadlines MUST be ≥ llm_timeout + buffer so the
		// watchdog never force-skips a phase shorter than a single LLM call.
		// With timeout_ms=300000 (5min) these values give +30~60s headroom.
		// cfgPhaseDeadlineSec also enforces a runtime floor of
		// llmTimeoutSec+30 on all acting phases, so even if an operator sets
		// a too-small value here the bot won't be killed mid-think.
		c.Werewolf.PhaseDeadlineSec = map[string]int{
			"pre_wolves":   360, // 5min + 60s buffer (role confirmation + first speak)
			"speak":        360, // 5min + 60s buffer (daytime discussion)
			"vote":         360, // 5min + 60s buffer (7-bot concurrent vote)
			"sheriff":      240, // 5min is generous for single-actor sheriff campaign
			"hunter_shoot": 240, // single-actor; needs headroom for thinking
			"dawn":         8,   // non-acting: immediate transition
			"night_wolves": 240, // wolf team coordination
			"night_seer":   240, // single-actor seer check
			"night_witch":  240, // single-actor witch decision
			// §猎魔人 单座位猎魔人狩猎(每晚限一次)。
			"night_demon_hunter": 240,
			// BUG-R70-P1 (2026-07-09): death_lyric 阶段必须挂截止时间,否则
			// watchdog 兜底 90s 太久; R70 Day 1 death_lyric 95s 才 skip。
			// 使用专用 DeathLyricDeadlineSec(默认 30s),与单座位发言节流匹配。
			"death_lyric": c.Werewolf.DeathLyricDeadlineSec,
		}
	}
}

// PhaseDeadlineSec 是 config.WerewolfConfig.PhaseDeadlineSec 的安全读取。
// 未配置 phase 时返回 fallback(秒)。fallback ≤ 0 时返回 90s 兜底。
func (c *Config) PhaseDeadlineSec(phase string) int {
	if c == nil {
		return 90
	}
	if v, ok := c.Werewolf.PhaseDeadlineSec[phase]; ok && v > 0 {
		return v
	}
	// 兜底:pre_wolves 120s,dawn 8s,death_lyric 30s(2026-07-09 BUG-R70-P1),
	// 其他 90s(避免 watchdog 误触发)
	switch phase {
	case "pre_wolves", "PhasePreWolves":
		return 120
	case "dawn", "PhaseDawn":
		return 8
	case "death_lyric", "PhaseDeathLyric":
		// BUG-R70-P1 (2026-07-09):death_lyric 兜底 30s,与单座位发言节流匹配
		if c.Werewolf.DeathLyricDeadlineSec >= 5 {
			return c.Werewolf.DeathLyricDeadlineSec
		}
		return 30
	default:
		return 90
	}
}

// =============================================================================
// 持久化与 LLM 敏感字段剥离辅助函数(2026-08-13 §config-auto-bootstrap)
// =============================================================================

// writeConfigFile serializes c as a JSON file with two-space indentation. We
// deliberately drop the runtime-only `omitempty` semantics on sensitive fields
// so the operator always sees explicit placeholders to edit (rather than
// silently inheriting defaults).
//
// Security note: callers MUST strip sensitive fields before persisting if
// they want the on-disk artifact to be safe to commit. The function below,
// writeConfigFileStrippedLLM, does that automatically.
func writeConfigFile(path string, c Config) error {
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// writeConfigFileStrippedLLM writes c to path with the LLM section's
// per-provider api_key stripped out and the entire `providers` array
// removed. Non-sensitive LLM fields (endpoint / endpoints / timeout_ms /
// stream_idle_timeout_ms / max_retries) are preserved so the on-disk
// artifact is still editable.
//
// The intent is the post-migration state of the 2026-08-13 bootstrap flow:
// once the providers[] block has been upserted into t_lsm_game_llm_provider,
// it has no business living in LsmAgentGame.conf any more (operators edit
// the DB via the admin UI; the conf file would only ever drift out of sync).
func writeConfigFileStrippedLLM(path string, c Config) error {
	// Drop all provider entries; their api_key + endpoint rows now live in DB.
	c.LLM.Providers = nil
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// stripLLMSensitiveFields mutates c in place to remove any field that
// contains an LLM provider API key. Returns the count of stripped providers
// so callers can log the migration outcome.
//
// Why we keep this separate from writeConfigFileStrippedLLM:
//   - Pure mutation (no I/O) keeps it testable in isolation.
//   - Future callers (e.g. an admin "export config without secrets" endpoint)
//     may want to reuse the mutation without immediately writing to disk.
func stripLLMSensitiveFields(c *Config) int {
	if c == nil {
		return 0
	}
	n := len(c.LLM.Providers)
	c.LLM.Providers = nil
	return n
}

// PersistToFile writes c (with the LLM providers block stripped) back to the
// given path. Used by main.go after the LLM registry has finished seeding
// t_lsm_game_llm_provider from the conf file — we want the on-disk artifact
// to drop the secrets so a future accidental `git add LsmAgentGame.conf`
// cannot leak them.
//
// If path is empty we default to ./LsmAgentGame.conf. Returns the number of
// providers stripped (always ≥ 0) and any I/O error.
func (c *Config) PersistToFile(path string) (strippedProviders int, err error) {
	if path == "" {
		path = "./LsmAgentGame.conf"
	}
	strippedProviders = stripLLMSensitiveFields(c)
	if err := writeConfigFileStrippedLLM(path, *c); err != nil {
		return strippedProviders, err
	}
	return strippedProviders, nil
}

// PersistFull writes c back to disk WITHOUT stripping — used during the very
// first bootstrap when we have nothing to migrate yet. Useful for tests that
// want to materialize a synthesized default config. Operators should normally
// use PersistToFile.
func (c *Config) PersistFull(path string) error {
	if path == "" {
		path = "./LsmAgentGame.conf"
	}
	return writeConfigFile(path, *c)
}
