// Package api — virtual_city_prediction_api.go: 虚拟城市预测运行 HTTP 入口
// (2026-10-21 §预测运行,批次68)。
//
// 契约: tmpPlan/虚拟城市-预测运行报告_20261021_113136.md §5。
//
//	GET /api/games/virtual_city/rooms/:id/prediction             最新 run 报告
//	GET /api/games/virtual_city/rooms/:id/prediction?run_id=PR2  指定 run 报告
//
// 权限:任意登录用户(观察者/条件触发者同口径,AuthRequired),与场景注入
// 三端点同级。房间不存在 → 35001;run_id 不在该房 → 35058;无任何 run 不
// 报错 —— 返回 runs:[] 且 report:null(前端展示「暂无预测运行」)。
package api

import (
	"net/http"

	"LsmAgentGame/errcode"
	"LsmAgentGame/game/virtual_city"

	"github.com/gin-gonic/gin"
)

// VirtualCityPredictionAPI 是预测运行端点的处理器。
type VirtualCityPredictionAPI struct {
	rooms VirtualCityRoomSource // 复用 survey/scenario 的窄接口(main.go 注入 vcMgr)
}

// NewVirtualCityPredictionAPI 构造(rooms 由 main.go 注入 vcMgr)。
func NewVirtualCityPredictionAPI(rooms VirtualCityRoomSource) *VirtualCityPredictionAPI {
	return &VirtualCityPredictionAPI{rooms: rooms}
}

// Report 处理 GET /api/games/virtual_city/rooms/:id/prediction。
//
// 响应(契约 §5):
//
//	{code:0, data:{ runs:[{id,start_month,months_observed,tier,injections}],
//	               report: <PredictionReport> | null }}
func (a *VirtualCityPredictionAPI) Report(c *gin.Context) {
	roomID := c.Param("id")
	if a.rooms == nil {
		c.JSON(http.StatusOK, gin.H{"code": errcode.ErrInternal, "message": "virtual_city manager not wired"})
		return
	}
	r := a.rooms.Get(roomID)
	if r == nil {
		c.JSON(http.StatusOK, gin.H{
			"code":    errcode.ErrVirtualCityRoomNotFound,
			"message": errcode.DefaultMessages[errcode.ErrVirtualCityRoomNotFound],
		})
		return
	}
	report, e := r.PredictionReport(c.Query("run_id"))
	if e != nil {
		c.JSON(http.StatusOK, gin.H{"code": e.Code, "message": e.Message})
		return
	}
	runs := r.PredictionRuns()
	if runs == nil {
		runs = []virtual_city.PredictionRunSummary{}
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data":    gin.H{"runs": runs, "report": report},
	})
}
