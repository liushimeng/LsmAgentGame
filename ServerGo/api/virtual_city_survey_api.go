// Package api — wealth_survey_api.go: 虚拟城市社会调研 HTTP 入口
// (2026-09-16 §财商流P1-2,调研契约 §3.1)。
//
// 契约: lag_docs/虚拟城市/已实现/05-P1扩展/虚拟城市-P1-社会调研系统-v1.md §3。
//   POST /api/games/virtual_city/rooms/:id/survey    发起调研(任意登录用户)
//   GET  /api/games/virtual_city/rooms/:id/surveys   全部历史调研(≤20)
// 房间不存在 → 35001;全部业务校验在 virtual_city.VirtualCityRoom.LaunchSurvey(持锁)。
package api

import (
	"net/http"

	"LsmAgentGame/errcode"
	"LsmAgentGame/game/virtual_city"

	"github.com/gin-gonic/gin"
)

// VirtualCityRoomSource 是房间源窄接口(*virtual_city.Manager 天然满足;ws 层/main.go
// 把 manager 适配传入,同 professionAPI 装配模式,避免 api → ws 反向依赖)。
type VirtualCityRoomSource interface {
	Get(roomID string) *virtual_city.VirtualCityRoom
}

// VirtualCitySurveyAPI 是调研两端点的处理器。
type VirtualCitySurveyAPI struct {
	rooms VirtualCityRoomSource
}

// NewVirtualCitySurveyAPI 构造(rooms 由 main.go 注入 wealthMgr)。
func NewVirtualCitySurveyAPI(rooms VirtualCityRoomSource) *VirtualCitySurveyAPI {
	return &VirtualCitySurveyAPI{rooms: rooms}
}

// launchSurveyReq POST body。
type launchSurveyReq struct {
	Question string   `json:"question"`
	Options  []string `json:"options"`
}

// Launch 处理 POST /api/games/virtual_city/rooms/:id/survey。
// 权限:任意登录用户(在座玩家/观战者/房外用户均可)—— 调研是「向 AI 提问」,
// 与座位无关(AuthRequired 中间件保证登录态)。
func (a *VirtualCitySurveyAPI) Launch(c *gin.Context) {
	roomID := c.Param("id")
	if a.rooms == nil {
		c.JSON(http.StatusOK, gin.H{"code": errcode.ErrInternal, "message": "virtual_city manager not wired"})
		return
	}
	r := a.rooms.Get(roomID)
	if r == nil {
		c.JSON(http.StatusOK, gin.H{"code": errcode.ErrVirtualCityRoomNotFound, "message": errcode.DefaultMessages[errcode.ErrVirtualCityRoomNotFound]})
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
		"data":    gin.H{"survey": virtual_city.SurveyJSONFrom(sv)},
	})
}

// List 处理 GET /api/games/virtual_city/rooms/:id/surveys(全部历史,≤20)。
func (a *VirtualCitySurveyAPI) List(c *gin.Context) {
	roomID := c.Param("id")
	if a.rooms == nil {
		c.JSON(http.StatusOK, gin.H{"code": errcode.ErrInternal, "message": "virtual_city manager not wired"})
		return
	}
	r := a.rooms.Get(roomID)
	if r == nil {
		c.JSON(http.StatusOK, gin.H{"code": errcode.ErrVirtualCityRoomNotFound, "message": errcode.DefaultMessages[errcode.ErrVirtualCityRoomNotFound]})
		return
	}
	surveys := r.ListSurveys()
	out := make([]virtual_city.SurveyJSON, 0, len(surveys))
	for i := range surveys {
		out = append(out, virtual_city.SurveyJSONFrom(&surveys[i]))
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data":    gin.H{"surveys": out},
	})
}
