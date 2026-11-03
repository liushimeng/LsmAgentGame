// Package api — virtual_city_system_api.go: 批次81 City-System 主 Agent 的
// HTTP 入口(2026-11-03 §批次81 City-System 主 Agent 与系统对话)。
//
// 契约: lag_docs/虚拟城市/已实现/81-CitySystem-Agent与系统对话/01-方案设计.md
// §6.2(新增 HTTP)。
//
//	GET  /api/games/virtual_city/rooms/:id/system_agent/history  当前用户在此房的会话
//	POST /api/games/virtual_city/rooms/:id/system_agent/reset    清空会话
//
// WS 是主通道;HTTP 只是首屏历史/兜底,避免 WS 未连上时白屏(契约 §6.2)。
// 权限:AuthRequired 登录即可(与场景推演/调研同口径,观察者可用)。
package api

import (
	"net/http"

	"LsmAgentGame/errcode"
	llmtypes "LsmAgentGame/llm/types"

	"github.com/gin-gonic/gin"
)

// SystemAgentSource 是 City-System 会话源的窄接口(ws 层实现;
// 声明在 api 避免 api → ws 反向依赖,与 VirtualCityRoomSource 同策略)。
type SystemAgentSource interface {
	// SystemAgentHistory 返回该用户在此房的会话消息(副本;无会话 → 空数组)。
	SystemAgentHistory(roomID, userID string) []llmtypes.Message
	// ResetSystemAgent 清空该用户在此房的会话。
	ResetSystemAgent(roomID, userID string)
}

// VirtualCitySystemAPI 是 City-System 会话两端点的处理器。
type VirtualCitySystemAPI struct {
	rooms  VirtualCityRoomSource
	agents SystemAgentSource
}

// NewVirtualCitySystemAPI 构造(rooms = vcMgr,agents = ws 层实现;由 main.go 注入)。
func NewVirtualCitySystemAPI(rooms VirtualCityRoomSource, agents SystemAgentSource) *VirtualCitySystemAPI {
	return &VirtualCitySystemAPI{rooms: rooms, agents: agents}
}

// History 处理 GET /api/games/virtual_city/rooms/:id/system_agent/history。
func (a *VirtualCitySystemAPI) History(c *gin.Context) {
	roomID := c.Param("id")
	userID := c.GetString("user_id")
	if a.rooms == nil || a.agents == nil {
		c.JSON(http.StatusOK, gin.H{"code": errcode.ErrInternal, "message": "virtual_city system agent not wired"})
		return
	}
	if a.rooms.Get(roomID) == nil {
		c.JSON(http.StatusOK, gin.H{"code": errcode.ErrVirtualCityRoomNotFound, "message": errcode.DefaultMessages[errcode.ErrVirtualCityRoomNotFound]})
		return
	}
	msgs := a.agents.SystemAgentHistory(roomID, userID)
	if msgs == nil {
		msgs = []llmtypes.Message{}
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data":    gin.H{"messages": msgs},
	})
}

// Reset 处理 POST /api/games/virtual_city/rooms/:id/system_agent/reset。
func (a *VirtualCitySystemAPI) Reset(c *gin.Context) {
	roomID := c.Param("id")
	userID := c.GetString("user_id")
	if a.rooms == nil || a.agents == nil {
		c.JSON(http.StatusOK, gin.H{"code": errcode.ErrInternal, "message": "virtual_city system agent not wired"})
		return
	}
	if a.rooms.Get(roomID) == nil {
		c.JSON(http.StatusOK, gin.H{"code": errcode.ErrVirtualCityRoomNotFound, "message": errcode.DefaultMessages[errcode.ErrVirtualCityRoomNotFound]})
		return
	}
	a.agents.ResetSystemAgent(roomID, userID)
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data":    gin.H{"ok": true},
	})
}
