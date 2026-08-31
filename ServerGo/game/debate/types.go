// Package debate implements the "辩论比赛" (Debate Competition) engine.
//
// 2026-08-31 §20260831-01 — 辩论比赛首期实现。
//
// 与狼人杀核心差异:
//   - 人类仅以「观众 / 房主」身份参与,所有「参赛辩手」均为 Agent Bot。
//   - 比赛节奏按"阶段"推进:立论 → 驳论 → 质询 → 自由辩论 → 总结 → 评审。
//   - 胜负由 3 个裁判 Agent 独立评审打分决定,无阵营博弈。
//   - 信息全对称:辩题 + 立场 + 公开发言对所有 Bot 可见,裁判可见全部。
//
// 包结构(全部 snake_case.go,符合 CLAUDE.md §3):
//   - types.go         阶段枚举 / 数据结构
//   - cards.go         内置辩题池(30+ 经典/最新/政策/发散题)
//   - room.go          DebateRoom 房间 + 状态机
//   - room_config.go   队伍/阶段/裁判配置
//   - room_manage.go   DebateManager 房间管理 + 生命周期
//   - room_spectator.go 观战者支持
//   - view.go          客户端状态视图(过滤 Agent 内部思考等)
//   - engine.go        主循环 + watchdog + 阶段推进
//   - engine_phase.go  阶段细节(发言 / 质询 / 自由辩论 / 总结)
//   - engine_judge.go  评审阶段
//   - fair_assignment.go 模型公平分配算法
//   - doc.go           包文档
//
// 详细设计见 docs/辩论比赛/*.md(2026-08-31 §00-§06 体系)。
package debate

import (
	"sync"
	"time"
)

// ============================================================================
// 阶段枚举
// ============================================================================

// Phase 辩论比赛阶段机状态。
//
// 推进链:PhaseFilling → PhasePreparation → PhaseOpeningArgument
// → PhaseRebuttal → PhaseCrossExamination → PhaseCrossExamSummary
// → PhaseFreeDebate → PhaseClosingArgument → PhaseJudging
// → PhaseResult → PhaseGameOver
type Phase string

const (
	PhaseFilling            Phase = "filling"             // 等待房主点击开始
	PhasePreparation        Phase = "preparation"         // 赛前准备(审题)
	PhaseOpeningArgument    Phase = "opening_argument"       // 开篇立论
	PhaseRebuttal           Phase = "rebuttal"            // 驳论
	PhaseCrossExamination   Phase = "cross_examination"    // 质询
	PhaseCrossExamSummary   Phase = "cross_exam_summary"    // 质询小结
	PhaseFreeDebate         Phase = "free_debate"          // 自由辩论
	PhaseClosingArgument    Phase = "closing_argument"      // 总结陈词
	PhaseJudging            Phase = "judging"              // 评审打分
	PhaseResult             Phase = "result"               // 公布结果
	PhaseGameOver           Phase = "game_over"            // 对局结束
)

// IsValidPhase 判断字符串是否为有效 Phase。
func IsValidPhase(p Phase) bool {
	switch p {
	case PhaseFilling, PhasePreparation, PhaseOpeningArgument, PhaseRebuttal,
		PhaseCrossExamination, PhaseCrossExamSummary, PhaseFreeDebate,
		PhaseClosingArgument, PhaseJudging, PhaseResult, PhaseGameOver:
		return true
	}
	return false
}

// PhaseCN 把 Phase 转为中文显示名。
func PhaseCN(p Phase) string {
	switch p {
	case PhaseFilling:
		return "等待开始"
	case PhasePreparation:
		return "赛前准备"
	case PhaseOpeningArgument:
		return "开篇立论"
	case PhaseRebuttal:
		return "驳论"
	case PhaseCrossExamination:
		return "质询"
	case PhaseCrossExamSummary:
		return "质询小结"
	case PhaseFreeDebate:
		return "自由辩论"
	case PhaseClosingArgument:
		return "总结陈词"
	case PhaseJudging:
		return "裁判评审"
	case PhaseResult:
		return "公布结果"
	case PhaseGameOver:
		return "对局结束"
	default:
		return string(p)
	}
}

// ============================================================================
// 辩论模式 / 立场
// ============================================================================

// Mode 辩论模式(队伍数 + 每队数)。
type Mode string

const (
	ModeTwoTeam   Mode = "two_team"   // 2 队 × 3-4 人
	ModeThreeTeam Mode = "three_team" // 3 队 × 3 人
	ModeFourTeam  Mode = "four_team"  // 4 队 × 2 人(BP 制)
	ModeFiveTeam  Mode = "five_team"  // 5 队 × 2 人
)

// Stance 立场标签。
type Stance string

const (
	StancePro             Stance = "pro"               // 正方
	StanceCon             Stance = "con"               // 反方
	StanceNeutral         Stance = "neutral"           // 中立
	StanceGovUpper        Stance = "gov_upper"         // 政府上院(BP)
	StanceGovLower        Stance = "gov_lower"         // 政府下院(BP)
	StanceOppUpper        Stance = "opp_upper"         // 反对上院(BP)
	StanceOppLower        Stance = "opp_lower"         // 反对下院(BP)
	StanceAngle1          Stance = "angle_1"           // 角度 1
	StanceAngle2          Stance = "angle_2"           // 角度 2
	StanceAngle3          Stance = "angle_3"           // 角度 3
	StanceAngle4          Stance = "angle_4"           // 角度 4
	StanceAngle5          Stance = "angle_5"           // 角度 5
)

// Role 队内辩位。
type Role string

const (
	RoleFirst  Role = "first"  // 一辩:立论
	RoleSecond Role = "second" // 二辩:驳论
	RoleThird  Role = "third"  // 三辩:质询
	RoleFourth Role = "fourth" // 四辩:总结
)

// RoleCN 辩位中文名。
func RoleCN(r Role) string {
	switch r {
	case RoleFirst:
		return "一辩"
	case RoleSecond:
		return "二辩"
	case RoleThird:
		return "三辩"
	case RoleFourth:
		return "四辩"
	default:
		return string(r)
	}
}

// RoleName 根据 Role 返回「一/二/三/四辩」中文名。
// 与 RoleCN 等价;保留别名便于语义对齐。
func RoleName(r Role) string { return RoleCN(r) }

// StanceLabel 立场简短显示名(例:"正方"/"反方")。
func StanceLabel(s Stance) string {
	switch s {
	case StancePro:
		return "正方"
	case StanceCon:
		return "反方"
	case StanceNeutral:
		return "中立"
	case StanceGovUpper:
		return "政府上院"
	case StanceGovLower:
		return "政府下院"
	case StanceOppUpper:
		return "反对上院"
	case StanceOppLower:
		return "反对下院"
	case StanceAngle1:
		return "角度一"
	case StanceAngle2:
		return "角度二"
	case StanceAngle3:
		return "角度三"
	case StanceAngle4:
		return "角度四"
	case StanceAngle5:
		return "角度五"
	default:
		return string(s)
	}
}

// ============================================================================
// 辩位 / 工具可用性
// ============================================================================

// ToolName 辩方可用工具名(对应 Agent 的 tool_use 调用)。
type ToolName string

const (
	ToolSpeech             ToolName = "speech"               // 正式发言
	ToolCrossExamQuestion  ToolName = "cross_exam_question"   // 质询提问
	ToolCrossExamAnswer    ToolName = "cross_exam_answer"     // 质询回答
	ToolFreeDebateSpeak    ToolName = "free_debate_speak"     // 自由辩发言
	ToolFinishSpeak        ToolName = "finish_speak"          // 主动结束发言
	ToolIdleSilent         ToolName = "idle_silent"           // 沉默放弃

	// 裁判专属工具
	ToolJudgeSubmitScore  ToolName = "submit_score"            // 提交评分
	ToolJudgeAnnounce     ToolName = "announce"                // 公开宣告
	// §20260831-06 — 裁判回答观众提问(观众提问闭环,
	// docs/辩论比赛/01 §6.1「可向裁判 Agent 提问,裁判可选择性回应」)。
	ToolJudgeAnswerSpectator ToolName = "answer_spectator"     // 回答观众提问
)

// AllowedToolsForPhaseRole 返回「辩位 × 阶段」下辩方 Bot 可调用的工具集。
// 设计见 docs/辩论比赛/05-辩论比赛工具与记忆系统设计.md §1.2 工具过滤规则。
func AllowedToolsForPhaseRole(phase Phase, role Role) []ToolName {
	// 默认有 idle_silent
	allowed := []ToolName{ToolIdleSilent}

	switch phase {
	case PhaseOpeningArgument:
		// 一辩立论
		if role == RoleFirst {
			allowed = append(allowed, ToolSpeech)
		}
	case PhaseRebuttal:
		// 二辩驳论
		if role == RoleSecond {
			allowed = append(allowed, ToolSpeech)
		}
	case PhaseCrossExamination:
		// 三辩可提问;二辩/三辩都可被质询作答(§05 §1.2:
		// 三辩同时拥有 question + answer 两件工具 —— 提问方与被质询方
		// 都可能是三辩,else-if 写法曾让三辩拿不到 answer 工具,
		// 实测造成「有问无答」,§20260831-02 修复)。
		if role == RoleThird {
			allowed = append(allowed, ToolCrossExamQuestion)
		}
		if role == RoleSecond || role == RoleThird {
			allowed = append(allowed, ToolCrossExamAnswer)
		}
	case PhaseCrossExamSummary:
		// 一辩或二辩做小结
		if role == RoleFirst || role == RoleSecond {
			allowed = append(allowed, ToolSpeech)
		}
	case PhaseFreeDebate:
		// 全员可发自由辩
		allowed = append(allowed, ToolFreeDebateSpeak, ToolFinishSpeak)
	case PhaseClosingArgument:
		// 四辩总结
		if role == RoleFourth {
			allowed = append(allowed, ToolSpeech)
		}
	case PhaseJudging:
		// 评审阶段辩方只能沉默
	}

	return allowed
}

// ============================================================================
// 数据结构
// ============================================================================

// DebateTopic 辩题(内置 + 用户自定义)。
type DebateTopic struct {
	ID           string   `json:"id"`
	Text         string   `json:"text"`
	Type         string   `json:"type"`          // classic/policy/value/tech/education/divergent/custom
	Category     string   `json:"category"`      // 细分分类
	ProPosition  string   `json:"pro_position"`
	ConPosition  string   `json:"con_position"`
	Background   string   `json:"background,omitempty"`
	Keywords     []string `json:"keywords,omitempty"`
	Difficulty   int      `json:"difficulty"`    // 1-5
	IsOfficial   bool     `json:"is_official"`
}

// AgentConfig 辩方 Agent 配置。
type AgentConfig struct {
	SeatID    int    `json:"seat_id"`    // 队伍内辩位(0-3)
	Role      Role   `json:"role"`
	RoleName  string `json:"role_name"`
	ModelKey  string `json:"model_key"`
	BotUserID string `json:"bot_user_id,omitempty"` // 自动分配后填充
}

// TeamConfig 队伍配置。
type TeamConfig struct {
	TeamID      int          `json:"team_id"`
	Stance      Stance       `json:"stance"`
	StanceLabel string       `json:"stance_label"`
	Agents      []AgentConfig `json:"agents"`
}

// JudgeConfig 裁判配置。
type JudgeConfig struct {
	JudgeID   int    `json:"judge_id"`
	ModelKey  string `json:"model_key"`
	BotUserID string `json:"bot_user_id,omitempty"`
}

// PhaseConfig 阶段参数配置(秒 / 字数)。
type PhaseConfig struct {
	PreparationSec        int `json:"preparation_sec"`         // 准备阶段时长(秒)
	OpeningArgumentSec    int `json:"opening_argument_sec"`    // 立论时长
	RebuttalSec           int `json:"rebuttal_sec"`            // 驳论时长
	CrossExamSec          int `json:"cross_exam_sec"`          // 质询时长
	CrossExamSummarySec   int `json:"cross_exam_summary_sec"`  // 质询小结时长
	FreeDebateSec         int `json:"free_debate_sec"`         // 自由辩论时长
	ClosingArgumentSec    int `json:"closing_argument_sec"`    // 总结时长
	JudgingSec            int `json:"judging_sec"`             // 评审时长
	ResultShowSec         int `json:"result_show_sec"`         // 结果展示时长
	MaxSpeechChars        int `json:"max_speech_chars"`         // 立论字数
	MaxRebuttalChars      int `json:"max_rebuttal_chars"`      // 驳论字数
	MaxCrossExamQChars    int `json:"max_cross_exam_q_chars"`  // 质询提问字数
	MaxCrossExamAChars    int `json:"max_cross_exam_a_chars"`  // 质询回答字数
	MaxFreeDebateChars    int `json:"max_free_debate_chars"`  // 自由辩字数
	MaxClosingChars       int `json:"max_closing_chars"`       // 总结字数
}

// DefaultPhaseConfig 默认阶段参数(标准 25 分钟赛制)。
// 设计见 docs/辩论比赛/03-辩论比赛房间创建与配置设计.md §4。
func DefaultPhaseConfig() PhaseConfig {
	return PhaseConfig{
		PreparationSec:      30,
		OpeningArgumentSec:  180,
		RebuttalSec:         120,
		CrossExamSec:        90,
		CrossExamSummarySec: 60,
		FreeDebateSec:       240,
		ClosingArgumentSec:  180,
		JudgingSec:          60,
		ResultShowSec:       30,
		MaxSpeechChars:      500,
		MaxRebuttalChars:    400,
		MaxCrossExamQChars:  50,
		MaxCrossExamAChars:  100,
		MaxFreeDebateChars:  80,
		MaxClosingChars:     600,
	}
}

// QuickPhaseConfig 快速赛制(各阶段时长减半,~10 分钟)。
func QuickPhaseConfig() PhaseConfig {
	return PhaseConfig{
		PreparationSec:      15,
		OpeningArgumentSec:  90,
		RebuttalSec:         60,
		CrossExamSec:        45,
		CrossExamSummarySec: 30,
		FreeDebateSec:       120,
		ClosingArgumentSec:  90,
		JudgingSec:          30,
		ResultShowSec:       20,
		MaxSpeechChars:      400,
		MaxRebuttalChars:    320,
		MaxCrossExamQChars:  40,
		MaxCrossExamAChars:  80,
		MaxFreeDebateChars:  60,
		MaxClosingChars:     480,
	}
}

// DeepPhaseConfig 深度赛制(各阶段时长加倍,~45 分钟)。
func DeepPhaseConfig() PhaseConfig {
	return PhaseConfig{
		PreparationSec:      60,
		OpeningArgumentSec:  300,
		RebuttalSec:         240,
		CrossExamSec:        180,
		CrossExamSummarySec: 120,
		FreeDebateSec:       480,
		ClosingArgumentSec:  300,
		JudgingSec:          120,
		ResultShowSec:       60,
		MaxSpeechChars:      700,
		MaxRebuttalChars:    560,
		MaxCrossExamQChars:  60,
		MaxCrossExamAChars:  120,
		MaxFreeDebateChars:  100,
		MaxClosingChars:     800,
	}
}

// SpectatorConfig 观众配置。
type SpectatorConfig struct {
	AllowChat             bool `json:"allow_chat"`
	RevealAgentThought    bool `json:"reveal_agent_thought"`
	AllowSpectatorQuestion bool `json:"allow_spectator_question"`
	ShowScoreRealtime     bool `json:"show_score_realtime"`
	ShowModelName         bool `json:"show_model_name"`
}

// DefaultSpectatorConfig 默认观众配置。
func DefaultSpectatorConfig() SpectatorConfig {
	return SpectatorConfig{
		AllowChat:              true,
		RevealAgentThought:     true,
		AllowSpectatorQuestion: true,
		ShowScoreRealtime:      false,
		ShowModelName:          true,
	}
}

// RoomConfig 完整房间配置(对外暴露的请求/响应载荷)。
type RoomConfig struct {
	Topic          DebateTopic      `json:"topic"`
	Mode           Mode             `json:"mode"`
	PhaseConfig    PhaseConfig      `json:"phase_config"`
	SpectatorConfig SpectatorConfig `json:"spectator_config"`
	Teams          []TeamConfig     `json:"teams"`
	Judges         []JudgeConfig    `json:"judges"`
	CreatedBy      string           `json:"created_by"`
	CreatedAt      int64            `json:"created_at"`
}

// ============================================================================
// 发言 / 质询 / 评审 数据结构
// ============================================================================

// Speech 一次正式发言。
type Speech struct {
	ID              string    `json:"id"`
	Phase           Phase     `json:"phase"`
	TeamID          int       `json:"team_id"`
	Seat            int       `json:"seat"`
	SpeakerName     string    `json:"speaker_name"`
	Stance          Stance    `json:"stance"`
	Role            Role      `json:"role"`
	Content         string    `json:"content"`
	WordCount       int       `json:"word_count"`
	DurationSec     int       `json:"duration_sec"`
	Timestamp       int64     `json:"timestamp"`
	References      []string  `json:"references,omitempty"`
	InternalThought string    `json:"internal_thought,omitempty"`
	ModelKey        string    `json:"model_key,omitempty"`
}

// CrossExamEntry 一条质询记录(问或答,统一存)。
type CrossExamEntry struct {
	ID         string `json:"id"`
	Questioner string `json:"questioner"` // "<team_id>:<seat>"
	Answerer   string `json:"answerer,omitempty"`
	Question   string `json:"question,omitempty"`
	Answer     string `json:"answer,omitempty"`
	IsAnswer   bool   `json:"is_answer"`
	Timestamp  int64  `json:"timestamp"`
}

// ScoreDimensions 5 维评分(每维 1-10)。
type ScoreDimensions struct {
	ArgumentQuality       int `json:"argument_quality"`
	LogicRigor            int `json:"logic_rigor"`
	LanguageExpression    int `json:"language_expression"`
	TeamCoordination      int `json:"team_coordination"`
	RebuttalEffectiveness int `json:"rebuttal_effectiveness"`
}

// TotalDimension 计算 5 维度总分(0-50)。
func (sd ScoreDimensions) TotalDimension() int {
	return sd.ArgumentQuality + sd.LogicRigor + sd.LanguageExpression +
		sd.TeamCoordination + sd.RebuttalEffectiveness
}

// TeamRanking 单裁判对单队的评分。
type TeamRanking struct {
	TeamID       int             `json:"team_id"`
	Scores       ScoreDimensions `json:"scores"`
	TotalScore   float64         `json:"total_score"`
	Comment      string          `json:"comment"`
	BestDebater  int             `json:"best_debater"` // 队内座位号
}

// JudgeScore 单裁判完整评分。
type JudgeScore struct {
	JudgeID        int           `json:"judge_id"`
	ModelKey       string        `json:"model_key"`
	Rankings       []TeamRanking `json:"rankings"`
	OverallComment string        `json:"overall_comment"`
	WinnerTeamID   int           `json:"winner_team_id"`
	IsFallback     bool          `json:"is_fallback"`
}

// TeamFinalScore 单队最终得分(3 裁判聚合)。
type TeamFinalScore struct {
	TeamID          int                `json:"team_id"`
	TeamName        string             `json:"team_name"`
	TotalScore      float64            `json:"total_score"`
	DimensionScores map[string]float64 `json:"dimension_scores"`
	Rank            int                `json:"rank"`
}

// BestDebaterInfo 最佳辩手。
type BestDebaterInfo struct {
	Seat      int    `json:"seat"`
	TeamID    int    `json:"team_id"`
	Name      string `json:"name"`
	ModelKey  string `json:"model_key"`
	Votes     int    `json:"votes"`
}

// ============================================================================
// §20260831-06 — 观众提问 / 模型胜率统计
// ============================================================================

// SpectatorQuestion 观众向裁判的提问(§01 §6.1 / §03 §6.2)。
//
// 生命周期:观众发 debate.spectator_question 帧 → ws 层写入房间提问队列
// → 评审阶段注入裁判 prompt → 裁判可选调 answer_spectator 工具回答
// → 回答以 debate.spectator_answer 帧广播给全体观众。
type SpectatorQuestion struct {
	ID            string `json:"id"`                         // "q_<seq>"
	UserID        string `json:"user_id"`                    // 提问者(前端脱敏展示)
	Text          string `json:"text"`                       // 问题正文(≤200 字)
	TimestampMS   int64  `json:"timestamp_ms"`               // 提问时间(毫秒)
	Answer        string `json:"answer,omitempty"`           // 裁判回答(未回答为空)
	AnswerJudgeID int    `json:"answer_judge_id"`            // 回答的裁判 ID(-1=未回答)
	AnsweredAtMS  int64  `json:"answered_at_ms,omitempty"`   // 回答时间(毫秒)
}

// ModelStats 模型辩论统计(docs/辩论比赛/06 §9.1)。
//
// 每局结束(评审结果产出)时由 DebateManager.recordGameResult 累加;
// GET /api/games/debate/stats 返回全量快照(按胜率降序)。
type ModelStats struct {
	ModelKey         string  `json:"model_key"`
	TotalGames       int     `json:"total_games"`
	WinCount         int     `json:"win_count"`
	BestDebaterCount int     `json:"best_debater_count"`
	AvgTotalScore    float64 `json:"avg_total_score"` // 所在队伍场均总分(0-50)
	WinRate          float64 `json:"win_rate"`        // WinCount / TotalGames
}

// DebateResult 对局最终结果。
type DebateResult struct {
	WinnerTeamID   int              `json:"winner_team_id"`
	WinnerTeamName string           `json:"winner_team_name"`
	BestDebater    BestDebaterInfo  `json:"best_debater"`
	TeamScores     []TeamFinalScore `json:"team_scores"`
	JudgeDetails   []JudgeScore     `json:"judge_details"`
	IsAbnormal     bool             `json:"is_abnormal"`
	AbnormalReason string           `json:"abnormal_reason,omitempty"`
}

// ============================================================================
// 内部状态
// ============================================================================

// speechStore 房间内发言的 in-memory 存储(线程安全)。
//
// 设计:Phase 推进时,全部发言按时间序列追加,供 view / 评审使用。
type speechStore struct {
	mu       sync.RWMutex
	speeches []Speech
}

func (s *speechStore) Append(sp Speech) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.speeches = append(s.speeches, sp)
}

func (s *speechStore) All() []Speech {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Speech, len(s.speeches))
	copy(out, s.speeches)
	return out
}

func (s *speechStore) ByPhase(phase Phase) []Speech {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []Speech{}
	for _, sp := range s.speeches {
		if sp.Phase == phase {
			out = append(out, sp)
		}
	}
	return out
}

// lastN 返回最近 N 条(从最新到最旧)。
func (s *speechStore) lastN(n int) []Speech {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if n <= 0 || len(s.speeches) == 0 {
		return nil
	}
	if n > len(s.speeches) {
		n = len(s.speeches)
	}
	out := make([]Speech, n)
	copy(out, s.speeches[len(s.speeches)-n:])
	return out
}

// crossExamStore 质询记录存储。
type crossExamStore struct {
	mu       sync.RWMutex
	entries  []CrossExamEntry
}

func (s *crossExamStore) Append(e CrossExamEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, e)
}

func (s *crossExamStore) All() []CrossExamEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]CrossExamEntry, len(s.entries))
	copy(out, s.entries)
	return out
}

// ============================================================================
// 计时器 / 时间工具
// ============================================================================

// WallNow 取得当前 unix 秒数(便于注入测试桩)。
func WallNow() int64 { return time.Now().Unix() }

// WallNowMS 取得当前 unix 毫秒数。
func WallNowMS() int64 { return time.Now().UnixMilli() }

// ClampInt 把 v 限制在 [lo, hi]。
func ClampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// CountRune 类似 len(s),但按 rune 数(中文/表情按 1 字算)。
func CountRune(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}

// TruncateRune 把 s 截断到 ≤ max 个 rune(避免截半个字符)。
//
// §20260831-06 修复:首期实现用 `for i, r := range s` 的 i(字节索引)
// 与 max(rune 数)比较,中文 3 字节/字导致实际只截出约 1/3 长度
// (立论 500 字上限实测只保留 ~167 字)。改为按 rune 计数截断。
func TruncateRune(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if CountRune(s) <= max {
		return s
	}
	out := make([]rune, 0, max)
	for _, r := range s {
		if len(out) >= max {
			break
		}
		out = append(out, r)
	}
	return string(out)
}
