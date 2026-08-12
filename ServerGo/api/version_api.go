// version_api.go 暴露程序版本号、git SHA 与编译时间，供 Web 端标题栏展示。
//
// 版本号、git short SHA 与编译时间都由 rebuild_restart_app.sh 通过
// `go build -ldflags "-X main.AppVersion=... -X main.gitShortSHA=... -X main.buildDateTime=..."`
// 在编译时注入。**绝不再使用 cgo __DATE__/__TIME__**——后者会被 Go 编译缓存
// 命中，导致 build_time 一直是第一次构建时的旧值（BUG-VERSION-STALE）。
package api

import (
	"net/http"

	"LsmAgentGame/errcode"

	"github.com/gin-gonic/gin"
)

// VersionAPI 是版本信息资源处理器。
type VersionAPI struct {
	version   string
	buildTime string
	gitSHA    string
}

// NewVersionAPI 使用编译期注入的版本号、git SHA 与编译时间构造 handler。
func NewVersionAPI(version, buildTime, gitSHA string) *VersionAPI {
	return &VersionAPI{version: version, buildTime: buildTime, gitSHA: gitSHA}
}

// Get GET /api/version — 返回程序版本号、git SHA 与编译时间。公开接口。
func (v *VersionAPI) Get(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": errcode.DefaultMessages[errcode.OK],
		"data": gin.H{
			"version":    v.version,
			"build_time": v.buildTime,
			"git_sha":    v.gitSHA,
		},
	})
}
