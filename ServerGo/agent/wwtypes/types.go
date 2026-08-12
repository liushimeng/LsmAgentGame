// Package wwtypes — types.go: 狼人杀专属契约类型 (PropSnapshot /
// PropHistoryRecord / WolfPackMsg / WolfVoteTally / SeatEmotionBrief /
// SpeechEvent / WhisperEvent / PlayerBrief)。
//
// 2026-08-06 §Agent 重构 Step 2:从 ServerGo/agent/prompt.go 抽出。
// 与 GameContext 同包,因为 GameContext 字段直接引用这些类型
// (AllPlayers []PlayerBrief / WolfPackSnapshot []WolfPackMsg 等)。
//
// 依赖约束:本包**不** import 任何 ServerGo 内部包,
// **不得** import game/werewolf 或 agent/wwplayer / agent/wwjudge,
// 避免循环 import。
package wwtypes
type PropSnapshot struct {
	PropKey      string `json:"prop_key"`
	NameZh       string `json:"name_zh"`
	NameEn       string `json:"name_en"`
	NameJa       string `json:"name_ja"`
	Description  string `json:"description"`
	Price        int64  `json:"price"`
	BaseHitRate  int    `json:"base_hit_rate"`
	IsAOE        bool   `json:"is_aoe"`
	InjectGenKey string `json:"inject_gen_key"`
}

// PropHistoryRecord 是公开道具使用记录的 agent 包本地副本（v3 §G5）。
// 与 werewolf.PropHistoryRecord 同形,避免 agent↔werewolf 循环导入。
// 内容仅 from/to/prop_key/hit/effect_hint/phase/round — 不含注入内容。
type PropHistoryRecord struct {
	FromSeat   int    `json:"from_seat"`
	ToSeat     int    `json:"to_seat"`
	PropKey    string `json:"prop_key"`
	PropNameZh string `json:"prop_name_zh"`
	Hit        bool   `json:"hit"`
	EffectHint string `json:"effect_hint"`
	Phase      string `json:"phase"`
	Round      int    `json:"round"`
	CreatedAt  int64  `json:"created_at"`
}

// GameContext is the snapshot the agent reasons over. It is rebuilt each turn

type WolfPackMsg struct {
	FromSeat  int
	Text      string
	CreatedAt int64 // unix seconds
}

// §20260811-04 U1 — 狼队暗号系统 CipherProtocol 数据镜像。
// 与 werewolf.CipherBundle / CipherTemplate 同形;agent 包独立定义
// 避免循环导入（§133 教训5 GameContext vs Agent 字段对称）。
type CipherTemplateSpec struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Keyword     string `json:"keyword"`
	Severity    int    `json:"severity"` // 0/1/2 → CipherSeverityNone/Weak/Strong
}

type WolfPackCipherBundle struct {
	Seat      int                  `json:"seat"`
	Day       int                  `json:"day"`
	Templates []CipherTemplateSpec `json:"templates,omitempty"`
}

// WolfVoteTally 是狼人夜间投票的计票结果(2026-07-17)。
// 在 agent 包中独立定义,避免 agent ↔ werewolf 循环导入。
type WolfVoteTally struct {
	Counts map[int]int `json:"counts"` // target seat -> 得票数
	Tied   []int       `json:"tied"`   // 最高票并列目标
	Reason string      `json:"reason"` // "majority" | "random_tie_break" | "random_all_abstain"
	Final  int         `json:"final"`  // 最终击杀目标 seat
}

// SeatEmotionBrief 是 GameContext.OthersEmotion 的单条记录,描述一个座位
// 的实时情绪状态(用于 BuildSystemPrompt / BuildUserPrompt 渲染)。
type SeatEmotionBrief struct {
	Seat      int    `json:"seat"`       // 0-indexed 座位号
	Emotion   string `json:"emotion"`    // 情绪 key (confident/excited/...)
	Reason    string `json:"reason"`     // 切换原因(LLM 给出,≤80 字)
	UpdatedAt int64  `json:"updated_at"` // 上次切换 unix 毫秒;0=初始
}

// SpeechEvent describes one chat / speak / last-words utterance observed
// in the room, projected into the agent's GameContext for prompt assembly.
//
// BUG: 狼人杀 7 人局 Agent 多轮上下文 — Account, AgentName, IsBot,
// IsSpectator are the four fields that let the LLM distinguish AI / 真人 /
// 观战发言. Without AgentName, an AI's 5-bot room produces five near-
// identical "X号(玩家): ..." lines that the LLM can't tell apart, so it
// can't reason about who-said-what. With AgentName the model can
// disambiguate "1号(美团 LongCat-2.0)" from "2号(豆包 2.0)" and react.
type SpeechEvent struct {
	Seat        int
	Account     string // 玩家昵称(空字符串时格式化为"玩家")
	AgentName   string // bot 专属:LLM AgentName (e.g. "美团 LongCat-2.0");人类为空
	IsBot       bool   // 来自 AI Agent
	IsSpectator bool   // 来自观战者(只读,影响权重)
	Text        string
	Ts          int64
}

// WhisperEvent describes one private message addressed to this bot's seat.
// Same identification fields as SpeechEvent; the sender is identified by
// FromSeat / From / AgentName so the receiving bot knows whether the
// message came from another AI (likely a wolf teammate), a human player,
// or — defensively — a spectator.
type WhisperEvent struct {
	FromSeat    int
	From        string // 发送者昵称
	AgentName   string // bot 专属:发送者 AgentName
	IsBot       bool
	IsSpectator bool
	Text        string
	Ts          int64
}

// PlayerBrief describes one seated player, surfaced to the LLM via the
// 【玩家档案】 block in BuildUserPrompt. BUG: 狼人杀 7 人局 Agent 多轮上下文
// (2026-07-08 新增) — 这是 per-bot 视角下"1号是谁"的权威来源:LLM 跨多轮
// 后能用 UserID/Account/AgentName/IsBot 区分真人 vs AI vs 队友狼人。
// 2026-07-10 12 人竞技局:座位 0..11,玩家编号 1..12。
type PlayerBrief struct {
	Seat      int    // 0-11
	UserID    string // t_lsm_game_user.id(空 = 座位未坐)
	Account   string // 玩家昵称
	AgentName string // bot 专属:LLM AgentName;人类/观战为空
	IsBot     bool   // 是否为 Agent bot
}
