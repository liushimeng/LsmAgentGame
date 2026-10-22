// Package api — virtual_city_realtime_api.go: City-Chrome 实时看板 HTTP 入口
// (批次65,2026-10-20 §批次65;契约 §3.5)。
//
//   GET  /api/virtual_city/realtime_board           快照(late-joiner 首屏)
//   POST /api/virtual_city/realtime_board/refresh   人工触发一次 mission
//
// 均需登录;refresh 同类目 60s 限流(35054,429 语义),通道离线 35053。
// 数据源 = citychrome 全局单例(与房间无关;房间级枚举在 WS 广播侧做)。
package api

import (
	"net/http"

	"LsmAgentGame/citychrome"
	"LsmAgentGame/errcode"

	"github.com/gin-gonic/gin"
)

// VirtualCityRealtimeAPI 是实时看板两端点的处理器。
type VirtualCityRealtimeAPI struct {
	svc *citychrome.Service
}

// NewVirtualCityRealtimeAPI 构造(svc 由 main.go 注入 citychrome.Start 产物;
// nil 时两端点返回 35053 离线语义)。
func NewVirtualCityRealtimeAPI(svc *citychrome.Service) *VirtualCityRealtimeAPI {
	return &VirtualCityRealtimeAPI{svc: svc}
}

// Board 处理 GET /api/virtual_city/realtime_board:返回 {code:0, data:{feed, missions}}。
// missions 为采集任务遥测(批次77 §3.4:新→旧 ≤20 条,无记录返回 [] 非 null;
// svc 离线分支同样带 missions: [])。
// ⚠️ WS game.realtime_feed 帧结构不动 —— missions 只随 HTTP 快照/手动刷新回读。
func (a *VirtualCityRealtimeAPI) Board(c *gin.Context) {
	if a.svc == nil {
		c.JSON(http.StatusOK, gin.H{
			"code":    errcode.ErrCityChromeChannelOffline,
			"message": errcode.DefaultMessages[errcode.ErrCityChromeChannelOffline],
			"data": gin.H{
				"feed": citychrome.Feed{
					Categories: map[string]citychrome.RealtimeCategory{},
					News:       []citychrome.RealtimeNews{},
				},
				"missions": a.missionsOrNil(),
			},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data": gin.H{
			"feed":     a.svc.Feed(),
			"missions": a.missionsOrNil(),
		},
	})
}

// missionsOrNil 返回 Board 的 missions 字段(批次77 §3.4):svc 在线取遥测快照,
// svc 为 nil 时返回空切片(JSON 序列化为 [] 而非 null)。
func (a *VirtualCityRealtimeAPI) missionsOrNil() []citychrome.MissionRecord {
	if a.svc == nil {
		return []citychrome.MissionRecord{}
	}
	return a.svc.MissionLog()
}

// refreshReq POST body(契约 §3.5:category 可选)。
type refreshReq struct {
	Category string `json:"category"`
}

// Refresh 处理 POST /api/virtual_city/realtime_board/refresh。
// 返回 {code:0, data:{accepted:true, queued_category:"..."}}。
func (a *VirtualCityRealtimeAPI) Refresh(c *gin.Context) {
	if a.svc == nil {
		c.JSON(http.StatusOK, gin.H{
			"code":    errcode.ErrCityChromeChannelOffline,
			"message": errcode.DefaultMessages[errcode.ErrCityChromeChannelOffline],
		})
		return
	}
	var req refreshReq
	if err := c.ShouldBindJSON(&req); err != nil {
		// body 可空(全量刷新入口宽松):解析失败按空 body 处理。
		req = refreshReq{}
	}
	accepted, queued, e := a.svc.Refresh(req.Category)
	if e != nil {
		// 35054 带 429 语义:同时把 HTTP status 置 429,前端按 §7.1 规范展示。
		if e.Code == errcode.ErrCityChromeRefreshTooFrequent {
			c.JSON(http.StatusTooManyRequests, gin.H{"code": e.Code, "message": e.Message})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": e.Code, "message": e.Message})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data":    gin.H{"accepted": accepted, "queued_category": queued},
	})
}
