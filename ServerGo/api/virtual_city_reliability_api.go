// Package api — virtual_city_reliability_api.go: 批次86 可靠性与可观测性
// HTTP 入口(2026-11-06 §可靠性与可观测性)。
//
// 契约: lag_docs/虚拟城市/已实现/86-可靠性与可观测性/01-方案设计.md §4。
//
//	POST   /api/games/virtual_city/rooms/:id/snapshot              手动存档
//	GET    /api/games/virtual_city/rooms/:id/snapshots             列出快照
//	POST   /api/games/virtual_city/rooms/:id/snapshot/restore      读档
//	POST   /api/games/virtual_city/rooms/:id/snapshot/rollback     回滚最近一次注入
//	DELETE /api/games/virtual_city/rooms/:id/snapshot/:snapshot_id 删除快照
//	GET    /api/games/virtual_city/rooms/:id/event_lifecycle       事件生命周期留痕
//	POST   /api/games/virtual_city/rooms/:id/event_lifecycle/replay 回放
//
// 权限:任意登录用户(AuthRequired 中间件保证登录态,与场景推演/调研同口径)。
// 房间不存在 → 35001;业务错误全部由 virtual_city.VirtualCityRoom 的持锁方法
// 返回并原样透传(e.Code / e.Message);缺必填 body 字段 → ErrValidationFailed。
package api

import (
	"net/http"
	"strconv"
	"strings"

	"LsmAgentGame/errcode"
	"LsmAgentGame/game/virtual_city"

	"github.com/gin-gonic/gin"
)

// eventLifecycleDefaultLimit 留痕查询缺省条数(契约 §4:limit 缺省 50)。
const eventLifecycleDefaultLimit = 50

// VirtualCityReliabilityAPI 是批次86 存档/读档/留痕/回放六端点的处理器。
type VirtualCityReliabilityAPI struct {
	rooms VirtualCityRoomSource // 复用 survey/scenario 的窄接口(main.go 注入 vcMgr)
}

// NewVirtualCityReliabilityAPI 构造(rooms 由 main.go 注入 vcMgr)。
func NewVirtualCityReliabilityAPI(rooms VirtualCityRoomSource) *VirtualCityReliabilityAPI {
	return &VirtualCityReliabilityAPI{rooms: rooms}
}

// snapshotReq POST /snapshot body(reason 可空 ⇒ 引擎取 SnapshotReasonManual)。
type snapshotReq struct {
	Reason string `json:"reason"`
}

// restoreReq POST /snapshot/restore body(snapshot_id 必填)。
type restoreReq struct {
	SnapshotID string `json:"snapshot_id"`
}

// replayReq POST /event_lifecycle/replay body(trace_id 必填)。
type replayReq struct {
	TraceID string `json:"trace_id"`
}

// roomOrError 取房间:未接线 → ErrInternal;房间不存在 → 35001。
// 命中时返回 (room, nil) 且**已写出响应**;未命中返回 (nil, err) 由调用方透传。
func (a *VirtualCityReliabilityAPI) roomOrError(c *gin.Context) (*virtual_city.VirtualCityRoom, *errcode.Error) {
	roomID := c.Param("id")
	if a.rooms == nil {
		return nil, errcode.Code(errcode.ErrInternal)
	}
	r := a.rooms.Get(roomID)
	if r == nil {
		return nil, errcode.Code(errcode.ErrVirtualCityRoomNotFound)
	}
	return r, nil
}

// Snapshot 处理 POST /api/games/virtual_city/rooms/:id/snapshot。
func (a *VirtualCityReliabilityAPI) Snapshot(c *gin.Context) {
	r, e := a.roomOrError(c)
	if e != nil {
		c.JSON(http.StatusOK, gin.H{"code": e.Code, "message": e.Message})
		return
	}
	// reason 可空 ⇒ 空 body 视为 {} (前端可能只发空对象 / 空体)。
	var req snapshotReq
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusOK, gin.H{"code": errcode.ErrValidationFailed, "message": "invalid snapshot payload: " + err.Error()})
			return
		}
	}
	meta, be := r.Snapshot(req.Reason)
	if be != nil {
		c.JSON(http.StatusOK, gin.H{"code": be.Code, "message": be.Message})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data":    gin.H{"snapshot": meta},
	})
}

// ListSnapshots 处理 GET /api/games/virtual_city/rooms/:id/snapshots。
// 恒返回数组(空 = []),不返回 null。
func (a *VirtualCityReliabilityAPI) ListSnapshots(c *gin.Context) {
	r, e := a.roomOrError(c)
	if e != nil {
		c.JSON(http.StatusOK, gin.H{"code": e.Code, "message": e.Message})
		return
	}
	list := r.ListSnapshots()
	if list == nil {
		list = []virtual_city.SnapshotMeta{}
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data":    gin.H{"snapshots": list},
	})
}

// RestoreSnapshot 处理 POST /api/games/virtual_city/rooms/:id/snapshot/restore。
func (a *VirtualCityReliabilityAPI) RestoreSnapshot(c *gin.Context) {
	r, e := a.roomOrError(c)
	if e != nil {
		c.JSON(http.StatusOK, gin.H{"code": e.Code, "message": e.Message})
		return
	}
	var req restoreReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": errcode.ErrValidationFailed, "message": "invalid restore payload: " + err.Error()})
		return
	}
	if strings.TrimSpace(req.SnapshotID) == "" {
		c.JSON(http.StatusOK, gin.H{"code": errcode.ErrValidationFailed, "message": "snapshot_id is required"})
		return
	}
	meta, be := r.RestoreSnapshot(req.SnapshotID)
	if be != nil {
		c.JSON(http.StatusOK, gin.H{"code": be.Code, "message": be.Message})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data":    gin.H{"snapshot": meta},
	})
}

// Rollback 处理 POST /api/games/virtual_city/rooms/:id/snapshot/rollback。
// 回滚最近一次成功的场景注入(用留痕关联的注入前快照)。
func (a *VirtualCityReliabilityAPI) Rollback(c *gin.Context) {
	r, e := a.roomOrError(c)
	if e != nil {
		c.JSON(http.StatusOK, gin.H{"code": e.Code, "message": e.Message})
		return
	}
	meta, be := r.RollbackLastScenario()
	if be != nil {
		c.JSON(http.StatusOK, gin.H{"code": be.Code, "message": be.Message})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data":    gin.H{"snapshot": meta},
	})
}

// DeleteSnapshot 处理 DELETE /api/games/virtual_city/rooms/:id/snapshot/:snapshot_id。
func (a *VirtualCityReliabilityAPI) DeleteSnapshot(c *gin.Context) {
	r, e := a.roomOrError(c)
	if e != nil {
		c.JSON(http.StatusOK, gin.H{"code": e.Code, "message": e.Message})
		return
	}
	snapshotID := c.Param("snapshot_id")
	if strings.TrimSpace(snapshotID) == "" {
		c.JSON(http.StatusOK, gin.H{"code": errcode.ErrValidationFailed, "message": "snapshot_id is required"})
		return
	}
	if be := r.DeleteSnapshot(snapshotID); be != nil {
		c.JSON(http.StatusOK, gin.H{"code": be.Code, "message": be.Message})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data":    gin.H{"ok": true},
	})
}

// EventLifecycle 处理 GET /api/games/virtual_city/rooms/:id/event_lifecycle。
//
// query limit:缺省 50;非数字 / <=0 ⇒ 缺省 50(与目录检索的「非法归一」同口径,
// 不报错)。恒返回数组 + total(命中条数,不受 limit 截断影响)。
func (a *VirtualCityReliabilityAPI) EventLifecycle(c *gin.Context) {
	r, e := a.roomOrError(c)
	if e != nil {
		c.JSON(http.StatusOK, gin.H{"code": e.Code, "message": e.Message})
		return
	}
	limit := eventLifecycleDefaultLimit
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}
	traces := r.EventLifecycleTraces(limit)
	if traces == nil {
		traces = []virtual_city.EventLifecycleTrace{}
	}
	total := len(r.EventLifecycleTraces(0))
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data":    gin.H{"traces": traces, "total": total},
	})
}

// ReplayTrace 处理 POST /api/games/virtual_city/rooms/:id/event_lifecycle/replay。
func (a *VirtualCityReliabilityAPI) ReplayTrace(c *gin.Context) {
	r, e := a.roomOrError(c)
	if e != nil {
		c.JSON(http.StatusOK, gin.H{"code": e.Code, "message": e.Message})
		return
	}
	var req replayReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": errcode.ErrValidationFailed, "message": "invalid replay payload: " + err.Error()})
		return
	}
	if strings.TrimSpace(req.TraceID) == "" {
		c.JSON(http.StatusOK, gin.H{"code": errcode.ErrValidationFailed, "message": "trace_id is required"})
		return
	}
	trace, match, be := r.ReplayScenarioTrace(req.TraceID)
	if be != nil {
		c.JSON(http.StatusOK, gin.H{"code": be.Code, "message": be.Message})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data":    gin.H{"trace": trace, "state_hash_match": match},
	})
}
