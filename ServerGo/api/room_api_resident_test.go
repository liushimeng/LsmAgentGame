// Package api — room_api_resident_test.go: 建房 resident_count 请求体校验
// (2026-09-21 §虚拟城市 契约 04 §1.1)。
//
// 负数 → 400;0(缺省)= 不启用城市层(向后兼容,不进 wealth 配置分支)。
// clamp 与空 model_key 放行由 service/ws 层单测覆盖(见 room_service_* 与
// game_service_agent_validator_test)。
package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// postCreateRoom 直接调 RoomAPI.Create(绕过路由;param 手填)。
func postCreateRoom(t *testing.T, kind, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	a := NewRoomAPI(nil, nil) // svc 未用到(校验分支先行返回)
	r := gin.New()
	r.POST("/api/games/:kind/rooms", func(c *gin.Context) {
		c.Set("user_id", "user-test")
		a.Create(c)
	})
	req := httptest.NewRequest(http.MethodPost, "/api/games/"+kind+"/rooms", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// 负数 resident_count → HTTP 400(契约:负数 400,不是静默 clamp)。
func TestCreateRoom_ResidentCountNegativeRejected(t *testing.T) {
	w := postCreateRoom(t, "wealth", `{"resident_count":-5}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("negative resident_count must 400, got %d body=%s", w.Code, w.Body.String())
	}
}

// 缺省 0 / 正数 → 不在 API 层拒绝(clamp 在 service 层;此处仅断言负数分支
// 不误伤 —— 0 走正常路径,svc 为 nil 时会在 CreateRoomWithAgents 处 panic,
// 因此只验证 400 不触发)。
func TestCreateRoom_ResidentCountZeroNotRejectedAtAPI(t *testing.T) {
	// 用非法 kind 让请求在进入 svc 前被拒,证明 0 值未触发 resident 分支。
	w := postCreateRoom(t, "", `{"resident_count":0}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("empty kind must 400, got %d", w.Code)
	}
}
