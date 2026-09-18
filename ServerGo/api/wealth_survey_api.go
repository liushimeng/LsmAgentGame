// Package api — wealth_survey_api.go: 财商流社会调研 HTTP 入口
// (2026-09-16 §财商流P1-2,调研契约 §3.1)。
//
// 契约: lag_docs/财商流游戏/已实现/05-P1扩展/财商流游戏-P1-社会调研系统-v1.md §3。
//   POST /api/games/wealth/rooms/:id/survey    发起调研(任意登录用户)
//   GET  /api/games/wealth/rooms/:id/surveys   全部历史调研(≤20)
// 房间不存在 → 35001;全部业务校验在 wealth.WealthRoom.LaunchSurvey(持锁)。
package api

import (
	"net/http"

	"LsmAgentGame/errcode"
	"LsmAgentGame/game/wealth"

	"github.com/gin-gonic/gin"
)

// WealthRoomSource 是房间源窄接口(*wealth.Manager 天然满足;ws 层/main.go
// 把 manager 适配传入,同 professionAPI 装配模式,避免 api → ws 反向依赖)。
type WealthRoomSource interface {
	Get(roomID string) *wealth.WealthRoom
}

// WealthSurveyAPI 是调研两端点的处理器。
type WealthSurveyAPI struct {
	rooms WealthRoomSource
}

// NewWealthSurveyAPI 构造(rooms 由 main.go 注入 wealthMgr)。
func NewWealthSurveyAPI(rooms WealthRoomSource) *WealthSurveyAPI {
	return &WealthSurveyAPI{rooms: rooms}
}

// launchSurveyReq POST body。
type launchSurveyReq struct {
	Question string   `json:"question"`
	Options  []string `json:"options"`
}

// Launch 处理 POST /api/games/wealth/rooms/:id/survey。
// 权限:任意登录用户(在座玩家/观战者/房外用户均可)—— 调研是「向 AI 提问」,
// 与座位无关(AuthRequired 中间件保证登录态)。
func (a *WealthSurveyAPI) Launch(c *gin.Context) {
	roomID := c.Param("id")
	if a.rooms == nil {
		c.JSON(http.StatusOK, gin.H{"code": errcode.ErrInternal, "message": "wealth manager not wired"})
		return
	}
	r := a.rooms.Get(roomID)
	if r == nil {
		c.JSON(http.StatusOK, gin.H{"code": errcode.ErrWealthRoomNotFound, "message": errcode.DefaultMessages[errcode.ErrWealthRoomNotFound]})
		return
	}
	var req launchSurveyReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": errcode.ErrValidationFailed, "message": "invalid survey payload: " + err.Error()})
		return
	}
	sv, e := r.LaunchSurvey(req.Question, req.Options)
	if e != nil {
		c.JSON(http.StatusOK, gin.H{"code": e.Code, "message": e.Message})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data":    gin.H{"survey": wealth.SurveyJSONFrom(sv)},
	})
}

// List 处理 GET /api/games/wealth/rooms/:id/surveys(全部历史,≤20)。
func (a *WealthSurveyAPI) List(c *gin.Context) {
	roomID := c.Param("id")
	if a.rooms == nil {
		c.JSON(http.StatusOK, gin.H{"code": errcode.ErrInternal, "message": "wealth manager not wired"})
		return
	}
	r := a.rooms.Get(roomID)
	if r == nil {
		c.JSON(http.StatusOK, gin.H{"code": errcode.ErrWealthRoomNotFound, "message": errcode.DefaultMessages[errcode.ErrWealthRoomNotFound]})
		return
	}
	surveys := r.ListSurveys()
	out := make([]wealth.SurveyJSON, 0, len(surveys))
	for i := range surveys {
		out = append(out, wealth.SurveyJSONFrom(&surveys[i]))
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data":    gin.H{"surveys": out},
	})
}
