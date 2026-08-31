// Package api — 辩论比赛历史对局 + 辩题池 API(2026-08-31 §20260831-08)。
//
// 设计依据:docs/辩论比赛/03-辩论比赛房间创建与配置设计.md §2.4(辩题池 API)
// + §8(数据库设计);docs/辩论比赛/06-辩论比赛公平性与评审系统设计.md §9(历史统计落库)。
//
// 数据全部来自 t_lsm_game_debate_* 表(写入方 game/debate/persistence.go):
//
//	GET  /api/games/debate/history        已结束比赛分页列表(finished_at desc)
//	GET  /api/games/debate/history/:id    复盘详情(房间 + 全部发言 + 全部评分)
//	GET  /api/games/debate/topics/:id     辩题详情(内置池 + DB 自定义池)
//	POST /api/games/debate/topics         添加自定义辩题(仅管理员)
//
// 本文件与 debate_api.go 同属 DebateAPI;响应统一走 {code, message, data} 封装
// (与 debate_api.go 现有端点一致,gormDB 未接线时返回 ErrDB)。
package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"LsmAgentGame/errcode"
	"LsmAgentGame/game/debate"
	"LsmAgentGame/logger"
	"LsmAgentGame/models"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// ============================================================================
// GET /api/games/debate/history — 已结束比赛分页列表
// ============================================================================

// historyRoomItem 列表精简条目(§03 §8.1 的复盘入口字段)。
//
// ID 序列化为 room_id:辩论房间视图一律 json:"room_id"(game/debate/view.go
// ClientState / PublicRoomSummary 同款惯例),前端 DebateHistoryListPanel
// 以 r.room_id 作 React key 与复盘详情跳转参数(§20260831-08 契约对齐)。
type historyRoomItem struct {
	ID                string          `json:"room_id"`
	TopicText         string          `json:"topic_text"`
	TopicType         string          `json:"topic_type"`
	Mode              string          `json:"mode"`
	Status            string          `json:"status"`
	WinnerTeamID      int             `json:"winner_team_id"`
	BestDebaterSeat   int             `json:"best_debater_seat"`
	BestDebaterTeamID int             `json:"best_debater_team_id"`
	FinishedAt        int64           `json:"finished_at"`
	CreatedBy         string          `json:"created_by"`
	IsAbnormal        bool            `json:"is_abnormal"`
	TeamConfig        json.RawMessage `json:"team_config,omitempty"`
}

// HistoryList GET /api/games/debate/history — 分页查询已结束比赛。
//
// 查询参数:page(默认 1)、page_size(默认 20,上限 100)。
// 「已结束」= finished_at > 0(persistence 在评审结果产出 / 终局时写入)。
func (a *DebateAPI) HistoryList(c *gin.Context) {
	if a.gormDB == nil {
		c.JSON(http.StatusOK, gin.H{
			"code":    errcode.ErrDB,
			"message": "debate history requires database persistence (not wired)",
		})
		return
	}

	page := mustAtoi(c.Query("page"))
	if page <= 0 {
		page = 1
	}
	pageSize := mustAtoi(c.Query("page_size"))
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}

	var total int64
	if err := a.gormDB.Model(&models.TLsmGameDebateRoom{}).
		Where("finished_at > 0").Count(&total).Error; err != nil {
		logger.L().Error("debate: history list count failed", zap.Error(err))
		c.JSON(http.StatusOK, gin.H{"code": errcode.ErrDB, "message": "history query failed"})
		return
	}

	var rows []models.TLsmGameDebateRoom
	if err := a.gormDB.
		Where("finished_at > 0").
		Order("finished_at DESC").
		Limit(pageSize).Offset((page - 1) * pageSize).
		Find(&rows).Error; err != nil {
		logger.L().Error("debate: history list query failed", zap.Error(err))
		c.JSON(http.StatusOK, gin.H{"code": errcode.ErrDB, "message": "history query failed"})
		return
	}

	items := make([]historyRoomItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, historyRoomItem{
			ID:                r.ID,
			TopicText:         r.TopicText,
			TopicType:         r.TopicType,
			Mode:              r.Mode,
			Status:            r.Status,
			WinnerTeamID:      r.WinnerTeamID,
			BestDebaterSeat:   r.BestDebaterSeat,
			BestDebaterTeamID: r.BestDebaterTeamID,
			FinishedAt:        r.FinishedAt,
			CreatedBy:         r.CreatedBy,
			IsAbnormal:        r.IsAbnormal,
			TeamConfig:        rawJSON(r.TeamConfig),
		})
	}

	// data 内嵌 {rooms, total, page, page_size}:前端 http 封装只取 body.data
	// (ClientWeb/src/services/http.ts),分页字段必须随 data 下发;与
	// git_log_api / model_log_api 的分页惯例(data 内嵌 entries/games + total)一致。
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data": gin.H{
			"rooms":     items,
			"total":     total,
			"page":      page,
			"page_size": pageSize,
		},
	})
}

// ============================================================================
// GET /api/games/debate/history/:id — 复盘详情
// ============================================================================

// HistoryDetail GET /api/games/debate/history/:id — 复盘详情。
//
// 返回:房间记录(含 result JSON)+ 该房间全部发言(created_at asc)
// + 全部评审记录。房间不存在 → ErrRoomNotFound(项目 errcode 风格)。
func (a *DebateAPI) HistoryDetail(c *gin.Context) {
	if a.gormDB == nil {
		c.JSON(http.StatusOK, gin.H{
			"code":    errcode.ErrDB,
			"message": "debate history requires database persistence (not wired)",
		})
		return
	}
	roomID := c.Param("id")

	var room models.TLsmGameDebateRoom
	err := a.gormDB.Where("id = ?", roomID).First(&room).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusOK, gin.H{
				"code":    errcode.ErrRoomNotFound,
				"message": "debate room history not found",
			})
			return
		}
		logger.L().Error("debate: history detail query failed",
			zap.String("room_id", roomID), zap.Error(err))
		c.JSON(http.StatusOK, gin.H{"code": errcode.ErrDB, "message": "history query failed"})
		return
	}

	var speeches []models.TLsmGameDebateSpeech
	if err := a.gormDB.Where("room_id = ?", roomID).
		Order("created_at ASC").Find(&speeches).Error; err != nil {
		logger.L().Error("debate: history speeches query failed",
			zap.String("room_id", roomID), zap.Error(err))
		c.JSON(http.StatusOK, gin.H{"code": errcode.ErrDB, "message": "history query failed"})
		return
	}

	var scores []models.TLsmGameDebateScore
	if err := a.gormDB.Where("room_id = ?", roomID).
		Order("judge_id ASC, team_id ASC").Find(&scores).Error; err != nil {
		logger.L().Error("debate: history scores query failed",
			zap.String("room_id", roomID), zap.Error(err))
		c.JSON(http.StatusOK, gin.H{"code": errcode.ErrDB, "message": "history query failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data": gin.H{
			"room":     buildHistoryRoomDetail(room),
			"speeches": speeches,
			"scores":   scores,
		},
	})
}

// historyRoomDetail 房间详情载荷(JSON 列还原为嵌套对象而非字符串)。
//
// RoomID 外层遮蔽嵌入行的 json:"id":前端 DebateHistoryDetail.room 复用
// DebateHistoryRoom 类型,统一以 room_id 消费(嵌入 id 保留,冗余无害)。
type historyRoomDetail struct {
	models.TLsmGameDebateRoom
	RoomID          string          `json:"room_id"`
	TeamConfig      json.RawMessage `json:"team_config"`
	PhaseConfig     json.RawMessage `json:"phase_config"`
	JudgeConfig     json.RawMessage `json:"judge_config"`
	SpectatorConfig json.RawMessage `json:"spectator_config"`
	Result          json.RawMessage `json:"result"`
}

// buildHistoryRoomDetail 构造器:嵌入原始行 + 把 JSON 列还原为嵌套对象。
func buildHistoryRoomDetail(r models.TLsmGameDebateRoom) historyRoomDetail {
	d := historyRoomDetail{TLsmGameDebateRoom: r}
	d.RoomID = r.ID
	d.TeamConfig = rawJSON(r.TeamConfig)
	d.PhaseConfig = rawJSON(r.PhaseConfig)
	d.JudgeConfig = rawJSON(r.JudgeConfig)
	d.SpectatorConfig = rawJSON(r.SpectatorConfig)
	d.Result = rawJSON(r.Result)
	return d
}

// rawJSON 把 DB 里的 JSON 字符串列还原为 json.RawMessage(非法/空值 → nil,
// 序列化时因 omitempty 不出现,前端拿到即对象而非带引号字符串)。
func rawJSON(s string) json.RawMessage {
	if !json.Valid([]byte(s)) {
		return nil
	}
	return json.RawMessage(s)
}

// ============================================================================
// GET /api/games/debate/topics/:id — 辩题详情
// ============================================================================

// TopicDetail GET /api/games/debate/topics/:id — 辩题详情。
//
// 查找顺序:内置池(cards.go)→ DB 自定义池;均未命中 → ErrTopicNotFound。
func (a *DebateAPI) TopicDetail(c *gin.Context) {
	id := c.Param("id")
	if t, ok := debate.FindTopicByID(id); ok {
		c.JSON(http.StatusOK, gin.H{"code": errcode.OK, "message": "ok", "data": t})
		return
	}
	if t, ok := a.findCustomTopic(id); ok {
		c.JSON(http.StatusOK, gin.H{"code": errcode.OK, "message": "ok", "data": t})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.ErrTopicNotFound,
		"message": errcode.DefaultMessages[errcode.ErrTopicNotFound],
	})
}

// ============================================================================
// POST /api/games/debate/topics — 添加自定义辩题(管理员)
// ============================================================================

// createTopicRequest POST /topics 请求体。
type createTopicRequest struct {
	Text        string `json:"text"`                   // 必填,≤200 rune
	Type        string `json:"type"`                   // 默认 custom
	ProPosition string `json:"pro_position,omitempty"` // 可选
	ConPosition string `json:"con_position,omitempty"` // 可选
	Background  string `json:"background,omitempty"`   // 可选
}

// CreateTopic POST /api/games/debate/topics — 添加自定义辩题(仅管理员)。
//
// 管理员判定与 model_admin_api 同一模式:UserRoleChecker.GetUserType ≥ UserTypeAdmin。
// 写入 t_lsm_game_debate_topic(is_official=false);ID 由 debate.NewCustomTopicID 生成。
func (a *DebateAPI) CreateTopic(c *gin.Context) {
	uid, ok := a.requireAdmin(c)
	if !ok {
		return
	}

	var req createTopicRequest
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

	text := strings.TrimSpace(req.Text)
	if text == "" {
		c.JSON(http.StatusOK, gin.H{
			"code":    errcode.ErrValidationFailed,
			"message": "text is required",
		})
		return
	}
	if debate.CountRune(text) > 200 {
		c.JSON(http.StatusOK, gin.H{
			"code":    errcode.ErrValidationFailed,
			"message": "text too long (max 200 chars)",
		})
		return
	}
	topicType := strings.TrimSpace(req.Type)
	if topicType == "" {
		topicType = "custom"
	}

	if a.gormDB == nil {
		c.JSON(http.StatusOK, gin.H{
			"code":    errcode.ErrDB,
			"message": "custom topics require database persistence (not wired)",
		})
		return
	}

	row := models.TLsmGameDebateTopic{
		ID:          debate.NewCustomTopicID(),
		Text:        text,
		Type:        topicType,
		Category:    "custom",
		ProPosition: strings.TrimSpace(req.ProPosition),
		ConPosition: strings.TrimSpace(req.ConPosition),
		Background:  strings.TrimSpace(req.Background),
		Keywords:    "[]",
		Difficulty:  3,
		CreatedBy:   uid,
		CreatedAt:   debate.WallNow(),
		IsOfficial:  false,
	}
	if err := a.gormDB.Create(&row).Error; err != nil {
		logger.L().Error("debate: create custom topic failed", zap.Error(err))
		c.JSON(http.StatusOK, gin.H{"code": errcode.ErrDB, "message": "create topic failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data":    topicFromModel(row),
	})
}

// requireAdmin 管理员校验(与 debate_api.go 的 {code,message} 封装风格一致;
// 鉴权模式照抄 model_admin_api.requireAdmin:UserType ≥ UserTypeAdmin)。
func (a *DebateAPI) requireAdmin(c *gin.Context) (string, bool) {
	uid := c.GetString("user_id")
	if uid == "" {
		c.JSON(http.StatusOK, gin.H{
			"code":    errcode.ErrAuthMissingToken,
			"message": "login required",
		})
		return "", false
	}
	if a.users == nil {
		c.JSON(http.StatusOK, gin.H{
			"code":    errcode.ErrInternal,
			"message": "user service not wired",
		})
		return "", false
	}
	userType, err := a.users.GetUserType(c.Request.Context(), uid)
	if err != nil {
		ce := errcode.AsError(err)
		c.JSON(http.StatusOK, gin.H{"code": ce.Code, "message": ce.Message})
		return "", false
	}
	if userType < models.UserTypeAdmin {
		c.JSON(http.StatusOK, gin.H{
			"code":    errcode.ErrPermissionDenied,
			"message": "admin required",
		})
		return "", false
	}
	return uid, true
}

// ============================================================================
// DB 自定义辩题 helpers
// ============================================================================

// loadCustomTopics 全量读取 DB 自定义辩题(created_at desc,新题在前)。
// gormDB 未接线或查询失败 → 返回空(不阻塞列表端点)。
func (a *DebateAPI) loadCustomTopics() []debate.DebateTopic {
	if a.gormDB == nil {
		return nil
	}
	var rows []models.TLsmGameDebateTopic
	if err := a.gormDB.Order("created_at DESC").Find(&rows).Error; err != nil {
		logger.L().Warn("debate: load custom topics failed", zap.Error(err))
		return nil
	}
	out := make([]debate.DebateTopic, 0, len(rows))
	for _, r := range rows {
		out = append(out, topicFromModel(r))
	}
	return out
}

// findCustomTopic 按 ID 精确查 DB 自定义辩题。
func (a *DebateAPI) findCustomTopic(id string) (debate.DebateTopic, bool) {
	if a.gormDB == nil || id == "" {
		return debate.DebateTopic{}, false
	}
	var row models.TLsmGameDebateTopic
	err := a.gormDB.Where("id = ?", id).First(&row).Error
	if err != nil {
		if err != gorm.ErrRecordNotFound {
			logger.L().Warn("debate: find custom topic failed",
				zap.String("topic_id", id), zap.Error(err))
		}
		return debate.DebateTopic{}, false
	}
	return topicFromModel(row), true
}

// customTopicMatches 判断自定义辩题是否命中列表过滤条件(空条件 = 全命中)。
func customTopicMatches(t debate.DebateTopic, q, topicType, category string) bool {
	if topicType != "" && t.Type != topicType {
		return false
	}
	if category != "" && t.Category != category {
		return false
	}
	if q != "" {
		if strings.Contains(strings.ToLower(t.Text), strings.ToLower(q)) {
			return true
		}
		for _, kw := range t.Keywords {
			if strings.Contains(strings.ToLower(kw), strings.ToLower(q)) {
				return true
			}
		}
		return false
	}
	return true
}

// topicFromModel DB 行 → debate.DebateTopic(Keywords JSON 字符串还原为切片)。
func topicFromModel(r models.TLsmGameDebateTopic) debate.DebateTopic {
	var keywords []string
	if r.Keywords != "" {
		_ = json.Unmarshal([]byte(r.Keywords), &keywords) // 非法 JSON → 忽略
	}
	return debate.DebateTopic{
		ID:          r.ID,
		Text:        r.Text,
		Type:        r.Type,
		Category:    r.Category,
		ProPosition: r.ProPosition,
		ConPosition: r.ConPosition,
		Background:  r.Background,
		Keywords:    keywords,
		Difficulty:  r.Difficulty,
		IsOfficial:  r.IsOfficial,
	}
}
