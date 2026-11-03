// Package api — virtual_city_system_api_test.go: 批次81 City-System 会话
// HTTP 两端点契约测试(2026-11-03 §批次81)。
//
//	GET  /rooms/:id/system_agent/history → {messages:[]}
//	POST /rooms/:id/system_agent/reset   → {ok:true}
//	房间不存在 → 35001;未接线 → ErrInternal。
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"LsmAgentGame/errcode"
	"LsmAgentGame/game/virtual_city"
	llmtypes "LsmAgentGame/llm/types"

	"github.com/gin-gonic/gin"
)

// fakeSystemAgentSource 实现 SystemAgentSource(测试桩)。
type fakeSystemAgentSource struct {
	msgs    []llmtypes.Message
	resets  int
	history int
}

func (f *fakeSystemAgentSource) SystemAgentHistory(roomID, userID string) []llmtypes.Message {
	f.history++
	if f.msgs == nil {
		return []llmtypes.Message{}
	}
	return f.msgs
}

func (f *fakeSystemAgentSource) ResetSystemAgent(roomID, userID string) { f.resets++ }

func sysAPIRouter(rooms VirtualCityRoomSource, agents SystemAgentSource) *gin.Engine {
	gin.SetMode(gin.TestMode)
	a := NewVirtualCitySystemAPI(rooms, agents)
	r := gin.New()
	g := r.Group("/api/games/virtual_city/rooms/:id")
	g.GET("/system_agent/history", a.History)
	g.POST("/system_agent/reset", a.Reset)
	return r
}

func sysAPIRequest(t *testing.T, r *gin.Engine, method, url string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, url, strings.NewReader(""))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("bad json %q: %v", w.Body.String(), err)
	}
	return w.Code, body
}

// TestVirtualCitySystemAPI_History 无会话 → 空数组;有会话 → 原样返回。
func TestVirtualCitySystemAPI_History(t *testing.T) {
	m := newSysAPITestManager(t, "sys-api-history")
	src := &fakeSystemAgentSource{}
	r := sysAPIRouter(m, src)
	_, body := sysAPIRequest(t, r, http.MethodGet, "/api/games/virtual_city/rooms/sys-api-history/system_agent/history")
	if int(body["code"].(float64)) != errcode.OK {
		t.Fatalf("code = %v, want 0", body["code"])
	}
	data := body["data"].(map[string]any)
	msgs, ok := data["messages"].([]any)
	if !ok || len(msgs) != 0 {
		t.Fatalf("messages = %v, want []", data["messages"])
	}

	// 有会话:两条消息原样返回。
	src.msgs = []llmtypes.Message{
		{Role: "user", Content: []llmtypes.ContentBlock{{Type: "text", Text: "hi"}}},
		{Role: "assistant", Content: []llmtypes.ContentBlock{{Type: "text", Text: "ok"}}},
	}
	_, body = sysAPIRequest(t, r, http.MethodGet, "/api/games/virtual_city/rooms/sys-api-history/system_agent/history")
	data = body["data"].(map[string]any)
	if got := len(data["messages"].([]any)); got != 2 {
		t.Fatalf("messages len = %d, want 2", got)
	}
	if src.history != 2 {
		t.Fatalf("source history calls = %d, want 2", src.history)
	}
}

// TestVirtualCitySystemAPI_Reset 清空会话 → {ok:true} 且调用源。
func TestVirtualCitySystemAPI_Reset(t *testing.T) {
	m := newSysAPITestManager(t, "sys-api-reset")
	src := &fakeSystemAgentSource{}
	r := sysAPIRouter(m, src)
	_, body := sysAPIRequest(t, r, http.MethodPost, "/api/games/virtual_city/rooms/sys-api-reset/system_agent/reset")
	if int(body["code"].(float64)) != errcode.OK {
		t.Fatalf("code = %v, want 0", body["code"])
	}
	data := body["data"].(map[string]any)
	if data["ok"] != true {
		t.Fatalf("ok = %v, want true", data["ok"])
	}
	if src.resets != 1 {
		t.Fatalf("source resets = %d, want 1", src.resets)
	}
}

// TestVirtualCitySystemAPI_RoomNotFound 房间不存在 → 35001。
func TestVirtualCitySystemAPI_RoomNotFound(t *testing.T) {
	m := newSysAPITestManager(t, "sys-api-exists")
	r := sysAPIRouter(m, &fakeSystemAgentSource{})
	for _, tc := range []struct {
		method, url string
	}{
		{http.MethodGet, "/api/games/virtual_city/rooms/nope/system_agent/history"},
		{http.MethodPost, "/api/games/virtual_city/rooms/nope/system_agent/reset"},
	} {
		_, body := sysAPIRequest(t, r, tc.method, tc.url)
		if int(body["code"].(float64)) != errcode.ErrVirtualCityRoomNotFound {
			t.Fatalf("%s %s code = %v, want %d", tc.method, tc.url, body["code"], errcode.ErrVirtualCityRoomNotFound)
		}
	}
}

// newSysAPITestManager 建一个未开局的空房间(系统会话端点只读会话存储,
// 不依赖 World;房间存在即可)。
func newSysAPITestManager(t *testing.T, roomID string) VirtualCityRoomSource {
	t.Helper()
	m := virtual_city.NewManager(virtual_city.Config{MonthMs: 3000}, nil)
	m.CreateRoom(roomID)
	return m
}
