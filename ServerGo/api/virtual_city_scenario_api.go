// Package api — virtual_city_scenario_api.go: 虚拟城市场景推演 / 预测运行 HTTP 入口
// (2026-10-21 §场景推演)。
//
// 契约: tmpPlan/虚拟城市-模拟程序化与预测运行优化_20261021_095114.md §5.1。
//
//	GET  /api/games/virtual_city/scenario_catalog        场景目录(10 个)
//	POST /api/games/virtual_city/rooms/:id/scenario      注入场景(条件触发者)
//	GET  /api/games/virtual_city/rooms/:id/scenarios     最近 20 条注入记录
//
// 权限:任意登录用户(观察者 = 条件触发者;AuthRequired 中间件保证登录态),
// 与 social survey 同口径。房间不存在 → 35001;业务校验全部在
// virtual_city.VirtualCityRoom.ApplyScenario(持锁)。
package api

import (
	"net/http"

	"LsmAgentGame/errcode"
	"LsmAgentGame/game/virtual_city"

	"github.com/gin-gonic/gin"
)

// VirtualCityScenarioAPI 是场景推演三端点的处理器。
type VirtualCityScenarioAPI struct {
	rooms VirtualCityRoomSource // 复用 survey API 的窄接口(main.go 注入 vcMgr)
}

// NewVirtualCityScenarioAPI 构造(rooms 由 main.go 注入 vcMgr)。
func NewVirtualCityScenarioAPI(rooms VirtualCityRoomSource) *VirtualCityScenarioAPI {
	return &VirtualCityScenarioAPI{rooms: rooms}
}

// injectScenarioReq POST body(契约 §5.1)。
//
// magnitude 缺省/0 = 场景目录 default_magnitude;duration_months 缺省/0 =
// 场景目录默认持续月数。target 仅 goods_price_shock(goods id)与
// policy_shock(tight|loose)消费,其余场景宽松忽略。
type injectScenarioReq struct {
	Kind           string `json:"kind"`
	Magnitude      int    `json:"magnitude"`
	DurationMonths int    `json:"duration_months"`
	Target         string `json:"target"`
	Note           string `json:"note"`
}

// Catalog 处理 GET /api/games/virtual_city/scenario_catalog。
// 与房间无关(全局目录),登录即可读。
func (a *VirtualCityScenarioAPI) Catalog(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data":    gin.H{"scenarios": virtual_city.ScenarioCatalog()},
	})
}

// Inject 处理 POST /api/games/virtual_city/rooms/:id/scenario。
func (a *VirtualCityScenarioAPI) Inject(c *gin.Context) {
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
	var req injectScenarioReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": errcode.ErrValidationFailed, "message": "invalid scenario payload: " + err.Error()})
		return
	}
	rec, effects, e := r.ApplyScenario(req.Kind, req.Magnitude, req.DurationMonths, req.Target, req.Note)
	if e != nil {
		c.JSON(http.StatusOK, gin.H{"code": e.Code, "message": e.Message})
		return
	}
	if effects == nil {
		effects = []virtual_city.ScenarioEffect{}
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data":    gin.H{"scenario": rec, "effects": effects},
	})
}

// List 处理 GET /api/games/virtual_city/rooms/:id/scenarios(最近 20 条,
// 旧→新)。
func (a *VirtualCityScenarioAPI) List(c *gin.Context) {
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
	list := r.ListScenarios()
	if list == nil {
		list = []virtual_city.ScenarioAppliedRecord{}
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data":    gin.H{"scenarios": list},
	})
}
