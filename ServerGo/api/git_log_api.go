// git_log_api.go 暴露"提交记录"相关接口给 Web 端弹窗使用:
//
//   GET /api/git/log?skip=0&limit=20  -> 分页列表
//   GET /api/git/log/:id              -> 单次提交的详细文件变更
//
// 两个接口均为公开(不要求登录) —— 提交记录本身已经在 git 历史里,不算敏感
// 业务数据,免去用户弹窗里调用时携带 JWT 的复杂度。
package api

import (
	"net/http"
	"strconv"

	"LsmWebGame/errcode"
	"LsmWebGame/logger"
	"LsmWebGame/service"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// GitLogAPI 提供提交记录相关接口。
type GitLogAPI struct {
	svc *service.GitLogService
}

// NewGitLogAPI 构造 handler。
func NewGitLogAPI(svc *service.GitLogService) *GitLogAPI {
	return &GitLogAPI{svc: svc}
}

// List 处理 GET /api/git/log,支持 skip / limit 两个 query 参数。
// limit 做了硬上限(<=200),避免单次响应过大撑爆前端。
func (a *GitLogAPI) List(c *gin.Context) {
	skip, _ := strconv.Atoi(c.Query("skip"))
	limit, _ := strconv.Atoi(c.Query("limit"))
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}
	entries, total, err := a.svc.List(skip, limit)
	if err != nil {
		logger.L().Warn("git log list failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    errcode.ErrInternal,
			"message": errcode.DefaultMessages[errcode.ErrInternal],
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": errcode.DefaultMessages[errcode.OK],
		"data": gin.H{
			"entries": entries,
			"total":   total,
			"skip":    skip,
			"limit":   limit,
		},
	})
}

// Detail 处理 GET /api/git/log/:id,id 可以是 40 位长 hash 或 abbreviated hash(常见 7 位)。
func (a *GitLogAPI) Detail(c *gin.Context) {
	id := c.Param("id")
	detail, err := a.svc.Detail(id)
	if err != nil {
		logger.L().Warn("git log detail failed", zap.String("id", id), zap.Error(err))
		c.JSON(http.StatusNotFound, gin.H{
			"code":    errcode.ErrValidationFailed,
			"message": err,
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": errcode.DefaultMessages[errcode.OK],
		"data":    detail,
	})
}
