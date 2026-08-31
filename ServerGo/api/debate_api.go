// Package api — 辩论比赛 REST API(2026-08-31 §20260831-01)。
//
// 端点清单(对齐 docs/辩论比赛/00 §4.1):
//   POST   /api/games/debate/rooms                  创建辩论房间
//   GET    /api/games/debate/rooms                  列出辩论房间
//   GET    /api/games/debate/rooms/:id              房间详情
//   POST   /api/games/debate/rooms/:id/join         加入观战
//   POST   /api/games/debate/rooms/:id/spectate     观战
//   POST   /api/games/debate/rooms/:id/leave_spectate 离开观战
//   GET    /api/games/debate/topics                 获取辩题池
//   POST   /api/games/debate/rooms/:id/start        开始比赛(仅房主)
//   GET    /api/games/debate/rooms/:id/history      发言历史
//
// 路由在 router.go 注册。
package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"LsmAgentGame/errcode"
	"LsmAgentGame/game/debate"
	"LsmAgentGame/llm"

	"github.com/gin-gonic/gin"
)

// DebateAPI 辩论 REST API。
type DebateAPI struct {
	mgr      *debate.DebateManager
	registry *llm.Registry
}

// NewDebateAPI 构造 DebateAPI。
func NewDebateAPI(mgr *debate.DebateManager, registry *llm.Registry) *DebateAPI {
	return &DebateAPI{mgr: mgr, registry: registry}
}

// DebaterManager 返回 manager(供 ws 层注入钩子)。
func (a *DebateAPI) DebaterManager() *debate.DebateManager { return a.mgr }

// ============================================================================
// createDebateRoomRequest 创建房间请求体。
// ============================================================================

type createDebateRoomRequest struct {
	Name            string                  `json:"name,omitempty"`
	TopicID         string                  `json:"topic_id,omitempty"`     // 从辩题池选
	TopicText       string                  `json:"topic_text,omitempty"`   // 自定义辩题
	TopicType       string                  `json:"topic_type,omitempty"`   // 自定义时填类型
	Mode            string                  `json:"mode"`                  // two_team/three_team/...
	PhaseConfig     *debate.PhaseConfig     `json:"phase_config,omitempty"`
	SpectatorConfig *debate.SpectatorConfig `json:"spectator_config,omitempty"`
	AgentAssignment string                  `json:"agent_assignment,omitempty"` // auto / manual
	Teams        []manualTeamReq          `json:"teams,omitempty"`         // 手动分配时填
	Judges       []manualJudgeReq         `json:"judges,omitempty"`        // 手动分配时填
}

type manualTeamReq struct {
	TeamID      int                 `json:"team_id"`
	Stance      string              `json:"stance"`
	StanceLabel string              `json:"stance_label,omitempty"`
	Agents      []manualAgentReq    `json:"agents"`
}

type manualAgentReq struct {
	SeatID   int    `json:"seat_id"`
	Role     string `json:"role"`
	RoleName string `json:"role_name,omitempty"`
	ModelKey string `json:"model_key"`
}

type manualJudgeReq struct {
	JudgeID  int    `json:"judge_id"`
	ModelKey string `json:"model_key"`
}

// ============================================================================
// CreateRoom POST /api/games/debate/rooms
// ============================================================================

// Create POST /api/games/debate/rooms — 创建辩论房间。
//
// 流程:
//  1. 解析 body → createDebateRoomRequest
//  2. 解析辩题(topic_id 命中内置池;否则视为 custom)
//  3. 解析模式(默认 two_team)
//  4. 根据 AgentAssignment 自动/手动分配模型
//  5. 构造 RoomConfig → DebateManager.CreateRoom
func (a *DebateAPI) Create(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusOK, gin.H{"code": errcode.ErrAuthMissingToken, "message": "login required"})
		return
	}

	var req createDebateRoomRequest
	if c.Request.Body != nil {
		dec := json.NewDecoder(c.Request.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			c.JSON(http.StatusOK, gin.H{
				"code":    errcode.ErrValidationFailed,
				"message": "invalid body: " + err.Error(),
			})
			return
		}
	}

	// 1) 解析辩题
	topic, ok := a.resolveTopic(&req)
	if !ok {
		c.JSON(http.StatusOK, gin.H{
			"code":    errcode.ErrValidationFailed,
			"message": "topic_id / topic_text is required",
		})
		return
	}

	// 2) 模式(默认 two_team)
	mode := debate.Mode(req.Mode)
	if mode == "" {
		mode = debate.ModeTwoTeam
	}

	// 3) 阶段参数
	phaseCfg := debate.DefaultPhaseConfig()
	if req.PhaseConfig != nil {
		phaseCfg = *req.PhaseConfig
		if phaseCfg.MaxSpeechChars == 0 {
			phaseCfg = debate.DefaultPhaseConfig()
		}
	}

	// 4) 观众配置
	specCfg := debate.DefaultSpectatorConfig()
	if req.SpectatorConfig != nil {
		specCfg = *req.SpectatorConfig
	}

	// 5) 解析队伍/裁判配置
	teams, judges, perr := a.resolveTeamsAndJudges(&req, mode)
	if perr != nil {
		c.JSON(http.StatusOK, gin.H{
			"code":    errcode.ErrValidationFailed,
			"message": perr.Error(),
		})
		return
	}

	// 6) 构造 RoomConfig
	cfg := debate.RoomConfig{
		Topic:           topic,
		Mode:            mode,
		PhaseConfig:     phaseCfg,
		SpectatorConfig: specCfg,
		Teams:           teams,
		Judges:          judges,
		CreatedBy:       userID,
		CreatedAt:       debate.WallNow(),
	}

	room, e := a.mgr.CreateRoom(cfg)
	if e != nil {
		c.JSON(http.StatusOK, gin.H{"code": e.Code, "message": e.Message})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data": gin.H{
			"room_id": room.RoomID,
			"summary": room.PublicSummary(),
			"client_state": room.BuildClientState(userID, true, false),
		},
	})
}

// resolveTopic 解析辩题。
//
// 优先级:topic_id 命中内置池 > topic_text 自定义。
func (a *DebateAPI) resolveTopic(req *createDebateRoomRequest) (debate.DebateTopic, bool) {
	if req.TopicID != "" {
		if t, ok := debate.FindTopicByID(req.TopicID); ok {
			return *t, true
		}
	}
	if req.TopicText != "" {
		topicType := req.TopicType
		if topicType == "" {
			topicType = "custom"
		}
		return debate.DebateTopic{
			ID:         "custom_" + debate.NewRoomID(),
			Text:       req.TopicText,
			Type:       topicType,
			IsOfficial: false,
		}, true
	}
	return debate.DebateTopic{}, false
}

// resolveTeamsAndJudges 解析队伍配置(自动分配模型 or 手动)。
func (a *DebateAPI) resolveTeamsAndJudges(req *createDebateRoomRequest, mode debate.Mode) ([]debate.TeamConfig, []debate.JudgeConfig, error) {
	stances := debate.DefaultStancesForMode(mode)
	teamCount := len(stances)

	// 队伍数量上限:5;手动分配时按请求填写
	if req.AgentAssignment == "manual" {
		return a.parseManualTeams(req, stances)
	}

	// 自动分配模型
	availableModels := a.collectAvailableModels()
	if len(availableModels) == 0 {
		return nil, nil, &apiError{msg: "no LLM models configured; cannot auto-assign agents"}
	}

	// 每队人数 = min(4, availableModels/teamCount);确保每队至少 2 人
	perTeam := len(availableModels) / teamCount
	if perTeam > 4 {
		perTeam = 4
	}
	if perTeam < 2 {
		return nil, nil, &apiError{msg: "need at least 2 models per team; add more LLM providers"}
	}

	// 调用公平分配
	teamAssignments, judgeAssignments, err := debate.FairModelAssignment(
		teamCount, perTeam, 3, availableModels, nil,
	)
	if err != nil {
		return nil, nil, &apiError{msg: err.Error()}
	}

	// 构造 TeamConfig
	teams := make([]debate.TeamConfig, 0, teamCount)
	for i := 0; i < teamCount; i++ {
		agents := make([]debate.AgentConfig, 0, perTeam)
		roles := debate.DefaultRolesForTeamSize(perTeam)
		for j := 0; j < perTeam; j++ {
			role := roles[j]
			agents = append(agents, debate.AgentConfig{
				SeatID:   j,
				Role:     role,
				RoleName: debate.RoleCN(role),
				ModelKey: teamAssignments[i][j],
			})
		}
		teams = append(teams, debate.TeamConfig{
			TeamID:      i,
			Stance:      stances[i],
			StanceLabel: debate.StanceLabel(stances[i]),
			Agents:      agents,
		})
	}

	// 构造 JudgeConfig
	judges := make([]debate.JudgeConfig, 0, len(judgeAssignments))
	for i, m := range judgeAssignments {
		judges = append(judges, debate.JudgeConfig{
			JudgeID:  i,
			ModelKey: m,
		})
	}

	return teams, judges, nil
}

// parseManualTeams 解析手动分配模式。
func (a *DebateAPI) parseManualTeams(req *createDebateRoomRequest, stances []debate.Stance) ([]debate.TeamConfig, []debate.JudgeConfig, error) {
	if len(req.Teams) == 0 {
		return nil, nil, &apiError{msg: "manual assignment requires teams[]"}
	}
	teams := make([]debate.TeamConfig, 0, len(req.Teams))
	for i, t := range req.Teams {
		if len(t.Agents) < 2 || len(t.Agents) > 4 {
			return nil, nil, &apiError{msg: "team agents must be 2..4"}
		}
		agents := make([]debate.AgentConfig, 0, len(t.Agents))
		for _, a := range t.Agents {
			role := debate.Role(a.Role)
			if role == "" {
				role = debate.RoleFirst
			}
			agents = append(agents, debate.AgentConfig{
				SeatID:   a.SeatID,
				Role:     role,
				RoleName: debate.RoleCN(role),
				ModelKey: a.ModelKey,
			})
		}
		stance := debate.Stance(t.Stance)
		if stance == "" && i < len(stances) {
			stance = stances[i]
		}
		teams = append(teams, debate.TeamConfig{
			TeamID:      t.TeamID,
			Stance:      stance,
			StanceLabel: debate.StanceLabel(stance),
			Agents:      agents,
		})
	}

	// 手动裁判
	judges := make([]debate.JudgeConfig, 0, 3)
	if len(req.Judges) > 0 {
		for i, j := range req.Judges {
			judges = append(judges, debate.JudgeConfig{
				JudgeID:  j.JudgeID,
				ModelKey: j.ModelKey,
			})
			_ = i
		}
	} else {
		// 手动队伍未指定裁判时:从 availableModels 选 3 个不同的
		availableModels := a.collectAvailableModels()
		used := make(map[string]bool)
		for _, t := range teams {
			for _, ag := range t.Agents {
				used[ag.ModelKey] = true
			}
		}
		for _, m := range availableModels {
			if !used[m] {
				judges = append(judges, debate.JudgeConfig{JudgeID: len(judges), ModelKey: m})
				if len(judges) == 3 {
					break
				}
			}
		}
		if len(judges) < 3 {
			// 候选不足:从 used 中补足
			for m := range used {
				if len(judges) == 3 {
					break
				}
				judges = append(judges, debate.JudgeConfig{JudgeID: len(judges), ModelKey: m})
			}
		}
	}

	if len(judges) < 3 {
		return nil, nil, &apiError{msg: "at least 3 judges required"}
	}

	// 校验
	if err := debate.ValidateAssignment(
		func() map[int]map[int]string {
			out := map[int]map[int]string{}
			for _, t := range teams {
				out[t.TeamID] = map[int]string{}
				for _, a := range t.Agents {
					out[t.TeamID][a.SeatID] = a.ModelKey
				}
			}
			return out
		}(),
		func() []string {
			out := make([]string, 0, len(judges))
			for _, j := range judges {
				out = append(out, j.ModelKey)
			}
			return out
		}(),
	); err != nil {
		return nil, nil, &apiError{msg: err.Error()}
	}

	return teams, judges, nil
}

// collectAvailableModels 收集 LLM Registry 中的可用模型。
func (a *DebateAPI) collectAvailableModels() []string {
	if a.registry == nil {
		return nil
	}
	models := a.registry.ListEnabled()
	out := make([]string, 0, len(models))
	for _, m := range models {
		out = append(out, m.Model)
	}
	return out
}

// apiError 简单 error 实现。
type apiError struct{ msg string }

func (e *apiError) Error() string { return e.msg }

// ============================================================================
// List GET /api/games/debate/rooms
// ============================================================================

// List GET /api/games/debate/rooms — 列出辩论房间(lobby)。
func (a *DebateAPI) List(c *gin.Context) {
	topicType := c.Query("topic_type")
	mode := c.Query("mode")
	status := c.Query("status")

	var rooms []*debate.DebateRoom
	if topicType != "" || mode != "" || status != "" {
		rooms = a.mgr.ListByFilter(topicType, mode, status)
	} else {
		rooms = a.mgr.List()
	}

	summaries := make([]debate.PublicRoomSummary, 0, len(rooms))
	for _, r := range rooms {
		summaries = append(summaries, r.PublicSummary())
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data":    summaries,
	})
}

// ============================================================================
// Detail GET /api/games/debate/rooms/:id
// ============================================================================

// Detail GET /api/games/debate/rooms/:id — 房间详情。
func (a *DebateAPI) Detail(c *gin.Context) {
	roomID := c.Param("id")
	userID := c.GetString("user_id")
	r, ok := a.mgr.Get(roomID)
	if !ok {
		c.JSON(http.StatusOK, gin.H{
			"code":    errcode.ErrRoomNotFound,
			"message": "debate room not found",
		})
		return
	}
	state := r.BuildClientState(userID, true, r.IsGameOver())
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data":    state,
	})
}

// ============================================================================
// Spectate POST /api/games/debate/rooms/:id/spectate
// ============================================================================

// Spectate POST /api/games/debate/rooms/:id/spectate — 观战(辩论比赛无"加入",仅观战)。
func (a *DebateAPI) Spectate(c *gin.Context) {
	roomID := c.Param("id")
	userID := c.GetString("user_id")
	r, ok := a.mgr.Get(roomID)
	if !ok {
		c.JSON(http.StatusOK, gin.H{"code": errcode.ErrRoomNotFound, "message": "debate room not found"})
		return
	}
	r.AddSpectator(userID, debate.SpectatorKindViewer)
	state := r.BuildClientState(userID, true, r.IsGameOver())
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data":    state,
	})
}

// LeaveSpectate POST /api/games/debate/rooms/:id/leave_spectate — 离开观战。
func (a *DebateAPI) LeaveSpectate(c *gin.Context) {
	roomID := c.Param("id")
	userID := c.GetString("user_id")
	r, ok := a.mgr.Get(roomID)
	if !ok {
		c.JSON(http.StatusOK, gin.H{"code": errcode.ErrRoomNotFound, "message": "debate room not found"})
		return
	}
	r.RemoveSpectator(userID)
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
	})
}

// ============================================================================
// Start POST /api/games/debate/rooms/:id/start
// ============================================================================

// Start POST /api/games/debate/rooms/:id/start — 房主点击开始。
func (a *DebateAPI) Start(c *gin.Context) {
	roomID := c.Param("id")
	userID := c.GetString("user_id")
	e := a.mgr.StartGame(roomID, userID)
	if e != nil {
		c.JSON(http.StatusOK, gin.H{"code": e.Code, "message": e.Message})
		return
	}
	r, _ := a.mgr.Get(roomID)
	state := r.BuildClientState(userID, true, false)
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data":    state,
	})
}

// ============================================================================
// History GET /api/games/debate/rooms/:id/history
// ============================================================================

// History GET /api/games/debate/rooms/:id/history — 发言历史。
func (a *DebateAPI) History(c *gin.Context) {
	roomID := c.Param("id")
	r, ok := a.mgr.Get(roomID)
	if !ok {
		c.JSON(http.StatusOK, gin.H{"code": errcode.ErrRoomNotFound, "message": "debate room not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data": gin.H{
			"speeches":  r.Speeches(),
			"cross_exam": r.CrossExamEntries(),
			"results":   r.Result(),
		},
	})
}

// ============================================================================
// Topics GET /api/games/debate/topics
// ============================================================================

// Topics GET /api/games/debate/topics — 获取辩题池。
//
// 查询参数:
//   q        关键词搜索(text/keywords)
//   type     按类型筛选
//   category 按分类筛选
func (a *DebateAPI) Topics(c *gin.Context) {
	q := c.Query("q")
	topicType := c.Query("type")
	category := c.Query("category")

	var topics []debate.DebateTopic
	switch {
	case q != "":
		topics = debate.SearchTopics(q)
	case topicType != "":
		topics = debate.TopicsByType(topicType)
	case category != "":
		topics = debate.TopicsByCategory(category)
	default:
		topics = debate.BuiltInTopics()
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data":    topics,
	})
}

// ============================================================================
// helpers
// ============================================================================

// mustAtoi 把字符串转 int,失败返回 0。
func mustAtoi(s string) int {
	if s == "" {
		return 0
	}
	v := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		v = v*10 + int(c - '0')
	}
	return v
}

// trimTopicSearch 修剪搜索字符串(避免空查询触发全量)。
func trimTopicSearch(s string) string { return strings.TrimSpace(s) }