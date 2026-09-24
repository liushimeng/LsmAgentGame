// Package api — wealth_city_api.go: 虚拟城市居民人物卡档案 HTTP 入口
// (2026-09-21 §档案锚定)。
//
// 契约: lag_docs/虚拟城市/已实现/03-Agent设计/虚拟城市-城市居民人物卡档案锚定设计-v1.md §7。
//
//	GET /api/games/virtual_city/rooms/:id/city/residents          分页档案列表
//	    query: offset(默认 0)、limit(默认 50,clamp 1..200)、q(姓名/职业/卡号包含匹配)
//	    data: {progress:{status,done,total,pool_size,anchored}, residents:[ResidentProfile], matched:int}
//	GET /api/games/virtual_city/rooms/:id/city/residents/:cardId  单卡详情
//	    data: {resident: ResidentProfile};查不到 → 35042 ErrVirtualCityResidentNotFound
//
// 房间不存在 → 35001;未建城(City=nil)→ data:{progress:{status:"idle"},
// residents:[], matched:0}(不算错误)。权限:AuthRequired 登录即可
// (档案是公开合成人格,无隐私 —— 与 survey 同级)。
package api

import (
	"net/http"
	"strconv"

	"LsmAgentGame/errcode"
	"LsmAgentGame/game/virtual_city/city"

	"github.com/gin-gonic/gin"
)

// VirtualCityAPI 是居民档案两端点的处理器(房间源 = wealthMgr,与
// VirtualCitySurveyAPI 同装配模式)。
type VirtualCityAPI struct {
	rooms VirtualCityRoomSource
}

// NewVirtualCityAPI 构造(rooms 由 main.go 注入 wealthMgr)。
func NewVirtualCityAPI(rooms VirtualCityRoomSource) *VirtualCityAPI {
	return &VirtualCityAPI{rooms: rooms}
}

// cityResidentsLimitDefault / clamp 边界(契约 §7:limit 默认 50,clamp 1..200)。
const (
	cityResidentsLimitDefault = 50
	cityResidentsLimitMin     = 1
	cityResidentsLimitMax     = 200
)

// queryInt 读取整型 query 参数(缺参/非法回退 def)。
func queryInt(c *gin.Context, key string, def int) int {
	raw := c.Query(key)
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return v
}

// ListResidents 处理 GET /api/games/virtual_city/rooms/:id/city/residents。
func (a *VirtualCityAPI) ListResidents(c *gin.Context) {
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
	offset := queryInt(c, "offset", 0)
	limit := queryInt(c, "limit", cityResidentsLimitDefault)
	if limit < cityResidentsLimitMin {
		limit = cityResidentsLimitMin
	}
	if limit > cityResidentsLimitMax {
		limit = cityResidentsLimitMax
	}
	residents, matched, progress := r.CityProfilePage(offset, limit, c.Query("q"))
	if residents == nil {
		residents = make([]city.ResidentProfile, 0) // JSON [] 而非 null(前端 .map 兼容)
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data": gin.H{
			"progress":  progress,
			"residents": residents,
			"matched":   matched,
		},
	})
}

// GetResident 处理 GET /api/games/virtual_city/rooms/:id/city/residents/:cardId。
func (a *VirtualCityAPI) GetResident(c *gin.Context) {
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
	p, ok := r.CityProfileOf(c.Param("cardId"))
	if !ok {
		c.JSON(http.StatusOK, gin.H{"code": errcode.ErrVirtualCityResidentNotFound, "message": errcode.DefaultMessages[errcode.ErrVirtualCityResidentNotFound]})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data":    gin.H{"resident": p},
	})
}
