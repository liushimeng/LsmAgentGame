// Package api — model_agent_memory_api.go exposes the admin endpoints for the
// werewolf Agent persistent memory (t_lsm_game_agent_memory, 2026-07-20 §131).
//
// Endpoints (all require admin role):
//
//	GET    /api/admin/llm/providers/:id/memory — 查看该模型 MEMORY.md 原文 + version/game_count
//	DELETE /api/admin/llm/providers/:id/memory — 清空记忆(软重置,version+1,memory_md="")
//
// 路由参数 :id 是 provider 行 ID(t_lsm_game_llm_provider.id);handler 先按
// id 查 provider 拿 model_key,再走 AgentMemoryService。详见
// docs/狼人杀-Agent与系统/狼人杀Agent持久化记忆设计.md §8。
package api

import (
	"errors"
	"net/http"
	"strings"

	"LsmWebGame/errcode"
	"LsmWebGame/logger"
	"LsmWebGame/models"
	"LsmWebGame/service"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// ModelAgentMemoryAPI 是 agent 持久化记忆的管理端点 handler。
type ModelAgentMemoryAPI struct {
	svc    UserRoleChecker
	gormDB *gorm.DB
	memSvc *service.AgentMemoryService
}

// NewModelAgentMemoryAPI wires the handler with its dependencies.
// nil values are tolerated at construction; each handler enforces "not nil".
func NewModelAgentMemoryAPI(
	svc UserRoleChecker,
	gormDB *gorm.DB,
	memSvc *service.AgentMemoryService,
) *ModelAgentMemoryAPI {
	return &ModelAgentMemoryAPI{svc: svc, gormDB: gormDB, memSvc: memSvc}
}

// requireAdmin 复用 ModelAdminAPI 的鉴权形状(2026-07-20 §131)。
func (h *ModelAgentMemoryAPI) requireAdmin(c *gin.Context) (string, bool) {
	uid := uidFromContext(c)
	if uid == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    errcode.ErrAuthMissingToken,
			"message": errcode.DefaultMessages[errcode.ErrAuthMissingToken],
		})
		return "", false
	}
	if h.svc == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    errcode.ErrInternal,
			"message": "user service not wired",
		})
		return "", false
	}
	userType, err := h.svc.GetUserType(c.Request.Context(), uid)
	if err != nil {
		ce := errcode.AsError(err)
		c.JSON(http.StatusBadRequest, gin.H{"code": ce.Code, "message": ce.Message})
		return "", false
	}
	if userType < models.UserTypeAdmin {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    errcode.ErrPermissionDenied,
			"message": "需要管理员权限",
		})
		return "", false
	}
	return uid, true
}

// resolveModelKey 把路由参数 :id(provider 行 ID)解析为 model_key。
// 返回 ("", false) 时响应已写入,调用方直接 return。
func (h *ModelAgentMemoryAPI) resolveModelKey(c *gin.Context) (string, bool) {
	if h.gormDB == nil || h.memSvc == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrInternal, "message": "db not wired",
		})
		return "", false
	}
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": errcode.ErrValidationFailed, "message": "id required",
		})
		return "", false
	}
	var row models.TLsmGameLlmProvider
	if err := h.gormDB.WithContext(c.Request.Context()).
		Where("id = ?", id).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"code": errcode.ErrValidationFailed, "message": "provider not found",
			})
			return "", false
		}
		logger.L().Error("admin agent memory: provider lookup failed",
			zap.String("id", id), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrDB, "message": "lookup failed",
		})
		return "", false
	}
	return row.Model, true
}

// GetMemory GET /api/admin/llm/providers/:id/memory。
// 返回 {model_key, memory_md, version, game_count, last_game_id, last_iterated_at}。
// 记忆行不存在(该模型从未完成过迭代)时返回空 memory_md + version=0。
func (h *ModelAgentMemoryAPI) GetMemory(c *gin.Context) {
	if _, ok := h.requireAdmin(c); !ok {
		return
	}
	modelKey, ok := h.resolveModelKey(c)
	if !ok {
		return
	}
	row, err := h.memSvc.LoadFull(c.Request.Context(), modelKey)
	if err != nil {
		logger.L().Error("admin agent memory: load failed",
			zap.String("model_key", modelKey), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrDB, "message": "load memory failed",
		})
		return
	}
	data := gin.H{
		"model_key":        modelKey,
		"memory_md":        "",
		"version":          0,
		"game_count":       0,
		"last_game_id":     "",
		"last_iterated_at": nil,
	}
	if row != nil {
		data["memory_md"] = row.MemoryMD
		data["version"] = row.Version
		data["game_count"] = row.GameCount
		data["last_game_id"] = row.LastGameID
		data["last_iterated_at"] = row.LastIteratedAt
	}
	c.JSON(http.StatusOK, gin.H{
		"code": errcode.OK, "message": "ok",
		"data": data,
	})
}

// ClearMemory DELETE /api/admin/llm/providers/:id/memory。
// 清空该模型记忆(软重置:memory_md="",version+1);行不存在时 no-op。
func (h *ModelAgentMemoryAPI) ClearMemory(c *gin.Context) {
	uid, ok := h.requireAdmin(c)
	if !ok {
		return
	}
	modelKey, ok := h.resolveModelKey(c)
	if !ok {
		return
	}
	if err := h.memSvc.Clear(c.Request.Context(), modelKey); err != nil {
		logger.L().Error("admin agent memory: clear failed",
			zap.String("model_key", modelKey), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrDB, "message": "clear memory failed",
		})
		return
	}
	logger.L().Info("admin cleared agent memory",
		zap.String("admin_id", uid),
		zap.String("model_key", modelKey))
	c.JSON(http.StatusOK, gin.H{
		"code": errcode.OK, "message": "ok",
		"data": gin.H{"model_key": modelKey, "cleared": true},
	})
}
