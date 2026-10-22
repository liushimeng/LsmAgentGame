// Package api — virtual_city_dashboard_api.go: 虚拟城市「实时看板」统一数据
// 体系 HTTP 入口(批次69,2026-10-21 §批次69)。
//
//	GET /api/virtual_city/dashboard   看板一次性快照(需登录)
//
// 响应:{code, message, data:{dashboard:{...}}}。dashboard 结构:
//
//	{
//	  "schema_version": 1,
//	  "generated_at": 1750000000,
//	  "dimensions":    [11 条维度目录],
//	  "data_sources": [45 条数据源登记册(批次76)],
//	  "realtime":     <citychrome.Feed:channel/categories/news>
//	}
//
// 关键契约:
//   - dimensions / data_sources 来自 game/virtual_city 包级目录(前后端共享
//     的唯一事实来源),纯静态,离线也能渲染看板骨架;
//   - realtime **直接复用 citychrome.Feed 的既有 JSON 形状**,与
//     GET /api/virtual_city/realtime_board 的 `feed` 完全同构 —— 前端可直接
//     把它当 feed 用(批次65 §3.5 契约不破坏);
//   - citychrome 未启动(svc==nil)⇒ code=35053(ErrCityChromeChannelOffline),
//     但 **data.dashboard 仍完整返回**(dimensions + data_sources 齐全,
//     realtime.channel.status="offline"、detail 写明原因、categories 空 map、
//     news 空数组)—— 离线可渲染骨架是本端点的核心价值。
package api

import (
	"net/http"
	"time"

	"LsmAgentGame/citychrome"
	"LsmAgentGame/errcode"
	"LsmAgentGame/game/virtual_city"

	"github.com/gin-gonic/gin"
)

// VirtualCityDashboardAPI 是看板快照端点的处理器。
type VirtualCityDashboardAPI struct {
	svc *citychrome.Service
}

// NewVirtualCityDashboardAPI 构造(svc 由 main.go 注入 citychrome.Start 产物;
// nil 时端点返回 35053 离线语义但仍带完整看板骨架)。
func NewVirtualCityDashboardAPI(svc *citychrome.Service) *VirtualCityDashboardAPI {
	return &VirtualCityDashboardAPI{svc: svc}
}

// dashboardSnapshot 是 data.dashboard 的载荷(字段与前端看板类型一一对应)。
type dashboardSnapshot struct {
	SchemaVersion int                              `json:"schema_version"`
	GeneratedAt   int64                            `json:"generated_at"`
	Dimensions    []virtual_city.DashboardDimension `json:"dimensions"`
	DataSources   []virtual_city.DataSource        `json:"data_sources"`
	Realtime      citychrome.Feed                  `json:"realtime"`
}

// Dashboard 处理 GET /api/virtual_city/dashboard。
// 返回 {code, message, data:{dashboard:{...}}};通道离线时 code=35053 而
// dashboard 依旧完整(见包注释「关键契约」)。
func (a *VirtualCityDashboardAPI) Dashboard(c *gin.Context) {
	code := errcode.OK
	message := "ok"
	if a.svc == nil {
		code = errcode.ErrCityChromeChannelOffline
		message = errcode.DefaultMessages[errcode.ErrCityChromeChannelOffline]
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    code,
		"message": message,
		"data": gin.H{"dashboard": dashboardSnapshot{
			SchemaVersion: virtual_city.DashboardSchemaVersion(),
			GeneratedAt:   time.Now().Unix(),
			Dimensions:    virtual_city.DashboardDimensions(),
			DataSources:   virtual_city.DashboardDataSources(),
			Realtime:      a.realtimeFeed(),
		}},
	})
}

// realtimeFeed 返回看板内嵌的实时通道快照。
//
//	- svc==nil(通道未启动)⇒ 离线占位:status=offline + detail 写明原因,
//	  categories 为空 map、news 为空数组(前端渲染骨架不炸);
//	- svc!=nil ⇒ 直接取 FeedStore 快照(与 realtime_board 同源同构);
//	  仅做**形状兜底**:categories/news 非 nil、channel.status 非空。
func (a *VirtualCityDashboardAPI) realtimeFeed() citychrome.Feed {
	if a.svc == nil {
		return citychrome.Feed{
			Channel: citychrome.ChannelStatus{
				Status: citychrome.ChannelOffline,
				Detail: "city_chrome service not started",
			},
			Categories: map[string]citychrome.RealtimeCategory{},
			News:       []citychrome.RealtimeNews{},
		}
	}
	f := a.svc.Feed()
	if f.Categories == nil {
		f.Categories = map[string]citychrome.RealtimeCategory{}
	}
	if f.News == nil {
		f.News = []citychrome.RealtimeNews{}
	}
	if f.Channel.Status == "" {
		f.Channel.Status = citychrome.ChannelOffline
		if f.Channel.Detail == "" {
			f.Channel.Detail = "channel status unknown"
		}
	}
	return f
}
