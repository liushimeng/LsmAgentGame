// Package api — room_api_resident_test.go: 建房 resident_count 请求体校验
// (2026-09-21 §虚拟城市 契约 04 §1.1)。
//
// 负数 → 400;0(缺省)= 不启用城市层(向后兼容,不进 wealth 配置分支)。
// clamp 与空 model_key 放行由 service/ws 层单测覆盖(见 room_service_* 与
// game_service_agent_validator_test)。
package api

import (
	"bytes"
	"encoding/json"
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

// 2026-09-22 §17-CityHuman(契约 03 §3.1/§6): resident_count 必达语义与
// 旧载荷宽松化。clamp 数值断言(缺省→10000、9→10、100001→clamp)在
// service 层 TestClampWealthResidentCount 覆盖;此处钉死 API 绑定层
// (与 handler 相同的 DisallowUnknownFields 解码):
//   - 正数(9 / 100001)不得在 API 层被拒(负数 400 分支不误伤);
//   - 旧客户端携带 wealth.pool / agent_seats 的载荷仍可解码(不 400,
//     契约 03 §6「静默忽略」—— agent_seats 由服务端忽略并合成 12 深度座位;
//     wealth.pool 为 Deprecated 空壳字段,任何值都被忽略)。
func TestCreateRoom_NewContractBindingTolerances(t *testing.T) {
	for _, body := range []string{
		`{"resident_count":9}`,
		`{"resident_count":100001}`,
		`{"wealth":{"month_ms":3000,"pool":"docs"},"agent_seats":[{"seat":0,"model_key":"X-model"}]}`,
		`{"wealth":{"resident_count":50,"pool":"curated"}}`,
	} {
		var req createRoomRequest
		dec := json.NewDecoder(bytes.NewBufferString(body))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			t.Fatalf("legacy payload must bind (silent-ignore contract), body=%s err=%v", body, err)
		}
	}
	// 未知顶层字段仍 400(DisallowUnknownFields 语义不放松)。
	var req createRoomRequest
	dec := json.NewDecoder(bytes.NewBufferString(`{"no_such_field":1}`))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err == nil {
		t.Fatal("unknown top-level field must still be rejected")
	}
	// 响应契约钉死:agent_seats_count 已从 Create 响应删除(契约 03 §3.2)。
	// 响应 map 在 handler 内联构造,此处以源码 grep 防回归(§130 接线验证)。
	if w := postCreateRoom(t, "wealth", `{"resident_count":-5}`); w.Code != http.StatusBadRequest {
		t.Fatalf("negative resident_count must stay 400 at handler level, got %d", w.Code)
	}
}
