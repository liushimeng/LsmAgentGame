// Package debate — 客户端状态视图。
//
// 2026-08-31 §20260831-01 — 视图层首期实现:
//
//   - ClientState:对外暴露的精简状态(用于 HTTP 详情 + WS 帧 payload)
//   - BuildClientState:从 DebateRoom 构造 ClientState
//   - PhaseCN / StanceLabel:已经在 types.go / room_config.go 提供
//
// 字段过滤:
//   - 隐藏内部系统字段(closed / manager 引用)
//   - 保留 Agent 思考过程(由 SpectatorConfig.RevealAgentThought 控制)
//
// 详细设计见 docs/辩论比赛/00-辩论比赛总体架构设计.md §3.3。
package debate

// ClientTeam 客户端可见的队伍信息。
type ClientTeam struct {
	TeamID      int          `json:"team_id"`
	Stance      Stance       `json:"stance"`
	StanceLabel string       `json:"stance_label"`
	Agents      []ClientAgent `json:"agents"`
}

// ClientAgent 客户端可见的辩手信息。
type ClientAgent struct {
	SeatID   int    `json:"seat_id"`
	Role     Role   `json:"role"`
	RoleName string `json:"role_name"`
	ModelKey string `json:"model_key,omitempty"`
	BotUserID string `json:"bot_user_id,omitempty"`
	Name     string `json:"name"`
}

// ClientJudge 客户端可见的裁判信息。
type ClientJudge struct {
	JudgeID  int    `json:"judge_id"`
	ModelKey string `json:"model_key,omitempty"`
	Name     string `json:"name"`
	BotUserID string `json:"bot_user_id,omitempty"`
}

// ClientState 客户端可见的房间状态(精简版)。
//
// 不含 DebateRoom 内部字段(closed / 内部 mu / manager / engine),
// 通过 BuildClientState 由 DebateRoom 投影得到。
type ClientState struct {
	// 基础信息
	RoomID        string `json:"room_id"`
	Topic         DebateTopic `json:"topic"`
	Mode          Mode    `json:"mode"`
	Status        string  `json:"status"`        // filling / playing / over
	CurrentPhase  Phase   `json:"current_phase"`
	PhaseCN       string  `json:"phase_cn"`
	PhaseDeadline int64   `json:"phase_deadline"`
	TimeRemaining int     `json:"time_remaining_sec"`
	CreatedAt     int64   `json:"created_at"`
	StartedAt     int64   `json:"started_at"`
	FinishedAt    int64   `json:"finished_at,omitempty"`

	// 比赛控制
	CreatedBy        string         `json:"created_by"`
	IsOwner         bool           `json:"is_owner"`
	SpectatorCount  int            `json:"spectator_count"`
	CurrentSpeaker  string         `json:"current_speaker,omitempty"`
	FreeDebateOwner string         `json:"free_debate_owner,omitempty"`

	// 队伍 + 裁判
	Teams  []ClientTeam  `json:"teams"`
	Judges []ClientJudge `json:"judges"`

	// 发言 / 质询 / 评审
	Speeches       []Speech              `json:"speeches,omitempty"`
	CrossExam      []CrossExamEntry      `json:"cross_exam,omitempty"`
	JudgeScores    []JudgeScore          `json:"judge_scores,omitempty"`
	Result         *DebateResult         `json:"result,omitempty"`
	AgentThoughts  map[string]string     `json:"agent_thoughts,omitempty"`

	// 配置(供前端 lobby / 设置弹窗使用)
	PhaseConfig     PhaseConfig      `json:"phase_config"`
	SpectatorConfig SpectatorConfig `json:"spectator_config"`

	// §20260831-09 — Agent Token + API 统计(聚合层)。
	// 由 BuildClientState 锁内变体 aggregateAgentStatsLocked() 填充;
	// 字段命名对齐前端 DebateRoomAgentStats。
	AgentStats *DebateRoomAgentStats `json:"agent_stats,omitempty"`
}

// BuildClientState 由 DebateRoom 投影得到 ClientState。
//
// 参数:
//   - forUserID: 请求者 userID(决定是否暴露 agent_thought 等隐私字段)
//   - includeSpeeches: 是否包含发言列表(true 用于 HTTP 详情 + WS 帧)
//   - includeJudgeScores: 是否包含评分(reveal 后才 true)
func (r *DebateRoom) BuildClientState(forUserID string, includeSpeeches, includeJudgeScores bool) *ClientState {
	r.mu.RLock()
	defer r.mu.RUnlock()

	cs := &ClientState{
		RoomID:        r.RoomID,
		Topic:         r.Config.Topic,
		Mode:          r.Config.Mode,
		Status:        PhaseToStatus(r.currentPhase),
		CurrentPhase:  r.currentPhase,
		PhaseCN:       PhaseCN(r.currentPhase),
		PhaseDeadline: r.phaseDeadline,
		TimeRemaining: r.phaseDeadlineToRemaining(),
		CreatedAt:     r.Config.CreatedAt,
		StartedAt:     r.startedAt,
		FinishedAt:    r.finishedAt,
		CreatedBy:     r.Config.CreatedBy,
		IsOwner:       r.Config.CreatedBy == forUserID,
		SpectatorCount: r.SpectatorCount(),
		CurrentSpeaker: r.currentSpeaker,
		FreeDebateOwner: r.freeDebateTurnOwner,
		Teams:         buildClientTeams(r.Config.Teams),
		Judges:        buildClientJudges(r.Config.Judges),
		PhaseConfig:     r.Config.PhaseConfig,
		SpectatorConfig: r.Config.SpectatorConfig,
	}

	if includeSpeeches {
		cs.Speeches = r.speeches.All()
		cs.CrossExam = r.crossExam.All()
	}
	if includeJudgeScores {
		cs.JudgeScores = make([]JudgeScore, len(r.judgeScores))
		copy(cs.JudgeScores, r.judgeScores)
		if r.result != nil {
			res := *r.result
			cs.Result = &res
		}
	}
	// Agent thoughts 仅在 reveal_agent_thought=true 且 phase 已开始时下发
	if r.Config.SpectatorConfig.RevealAgentThought && r.currentPhase != PhaseFilling {
		cs.AgentThoughts = r.collectAgentThoughts()
	}

	// §20260831-09 — 房间级 Agent 统计聚合(锁内变体,§92a 范式)。
	// BotStats / JudgeStats 完整详情走 debate.stats_update WS 帧增量下发,
	// 此处仅填充聚合层(Aggregate 子结构)节省带宽。
	if detail := r.aggregateAgentStatsLocked(); detail != nil {
		cs.AgentStats = &detail.Aggregate
	}

	return cs
}

// phaseDeadlineToRemaining 计算当前剩余秒数(必须持有读锁)。
func (r *DebateRoom) phaseDeadlineToRemaining() int {
	if r.phaseDeadline == 0 {
		return 0
	}
	rem := r.phaseDeadline - WallNow()
	if rem < 0 {
		return 0
	}
	return int(rem)
}

// buildClientTeams 把 TeamConfig 投影为 ClientTeam。
func buildClientTeams(teams []TeamConfig) []ClientTeam {
	out := make([]ClientTeam, 0, len(teams))
	for _, t := range teams {
		agents := make([]ClientAgent, 0, len(t.Agents))
		for _, a := range t.Agents {
			agents = append(agents, ClientAgent{
				SeatID:   a.SeatID,
				Role:     a.Role,
				RoleName: a.RoleName,
				ModelKey: a.ModelKey,
				BotUserID: a.BotUserID,
				Name:     StanceLabel(t.Stance) + RoleCN(a.Role) + "(" + ModelShort(a.ModelKey) + ")",
			})
		}
		out = append(out, ClientTeam{
			TeamID:      t.TeamID,
			Stance:      t.Stance,
			StanceLabel: t.StanceLabel,
			Agents:      agents,
		})
	}
	return out
}

// buildClientJudges 把 JudgeConfig 投影为 ClientJudge。
func buildClientJudges(judges []JudgeConfig) []ClientJudge {
	out := make([]ClientJudge, 0, len(judges))
	for i, j := range judges {
		out = append(out, ClientJudge{
			JudgeID:   j.JudgeID,
			ModelKey:  j.ModelKey,
			BotUserID: j.BotUserID,
			Name:      "裁判" + fmtInt(i+1) + "(" + ModelShort(j.ModelKey) + ")",
		})
	}
	return out
}

// collectAgentThoughts 从最近发言中提取 internal_thought 字段。
//
// 当前实现:以 "team_id:seat" 作为 key,value = 该 Bot 最近一条 internal_thought。
// 未来可扩展为独立 ThoughtStore,但本版本复用 speeches 字段即可。
func (r *DebateRoom) collectAgentThoughts() map[string]string {
	out := map[string]string{}
	for _, sp := range r.speeches.All() {
		if sp.InternalThought == "" {
			continue
		}
		key := SeatKey(sp.TeamID, sp.Seat)
		// 后写覆盖前写(保留最新一次)
		out[key] = sp.InternalThought
	}
	return out
}

// PhaseToStatus 把阶段转为前端展示的"状态"。
//
// 状态语义对齐前端 lobby 列表:
//   - filling → "waiting"
//   - playing → "playing"
//   - over → "over"
func PhaseToStatus(p Phase) string {
	switch p {
	case PhaseFilling:
		return "waiting"
	case PhaseGameOver:
		return "over"
	default:
		return "playing"
	}
}

// PublicRoomSummary 用于大厅列表的精简房间卡片。
type PublicRoomSummary struct {
	RoomID         string      `json:"room_id"`
	Topic          DebateTopic `json:"topic"`
	Mode           Mode        `json:"mode"`
	Phase          Phase       `json:"phase"`
	PhaseCN        string      `json:"phase_cn"`
	Status         string      `json:"status"`
	SpectatorCount int         `json:"spectator_count"`
	TeamCount      int         `json:"team_count"`
	JudgeCount     int         `json:"judge_count"`
	CreatedBy      string      `json:"created_by"`
	CreatedAt      int64       `json:"created_at"`
	StartedAt      int64       `json:"started_at"`
}

// PublicSummary 返回供大厅列表使用的精简摘要。
func (r *DebateRoom) PublicSummary() PublicRoomSummary {
	return PublicRoomSummary{
		RoomID:         r.RoomID,
		Topic:          r.Config.Topic,
		Mode:           r.Config.Mode,
		Phase:          r.Phase(),
		PhaseCN:        PhaseCN(r.Phase()),
		Status:         PhaseToStatus(r.Phase()),
		SpectatorCount: r.SpectatorCount(),
		TeamCount:      len(r.Config.Teams),
		JudgeCount:     len(r.Config.Judges),
		CreatedBy:      r.Config.CreatedBy,
		CreatedAt:      r.Config.CreatedAt,
		StartedAt:      r.startedAt,
	}
}

// PublicSummaries 把多个 DebateRoom 转成 PublicRoomSummary 列表。
func (m *DebateManager) PublicSummaries() []PublicRoomSummary {
	rooms := m.List()
	out := make([]PublicRoomSummary, 0, len(rooms))
	for _, r := range rooms {
		out = append(out, r.PublicSummary())
	}
	return out
}