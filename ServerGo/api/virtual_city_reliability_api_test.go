// Package api — virtual_city_reliability_api_test.go: 批次86 可靠性与可观测性
// HTTP 六端点契约测试(2026-11-06 §可靠性与可观测性 §4)。
//
// 覆盖:
//
//	① 快照 round-trip:存档 → 列表 → 读档(code 0,data 齐备);
//	② 读档未知 snapshot_id → 35066;
//	③ 留痕查询:恒数组(空 = [])且带 total;
//	④ 回放未知 trace_id → 35067;
//	⑤ 房间不存在 → 35001(六端点全查);
//	⑥ 删除快照 → {ok:true} 且列表收敛;
//	⑦ 回滚:注入 → POST rollback ⇒ code 0 且状态精确回到注入前。
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"LsmAgentGame/errcode"
	"LsmAgentGame/game/virtual_city"

	"github.com/gin-gonic/gin"
)

// reliabilityAPIRouter 挂载批次86 六端点(绕过鉴权中间件 —— 与其它
// virtual_city 端点同口径:鉴权由 router.go 的 AuthRequired 统一保证)。
func reliabilityAPIRouter(rooms VirtualCityRoomSource) *gin.Engine {
	gin.SetMode(gin.TestMode)
	a := NewVirtualCityReliabilityAPI(rooms)
	r := gin.New()
	g := r.Group("/api/games/virtual_city/rooms/:id")
	g.POST("/snapshot", a.Snapshot)
	g.GET("/snapshots", a.ListSnapshots)
	g.POST("/snapshot/restore", a.RestoreSnapshot)
	g.POST("/snapshot/rollback", a.Rollback)
	g.DELETE("/snapshot/:snapshot_id", a.DeleteSnapshot)
	g.GET("/event_lifecycle", a.EventLifecycle)
	g.POST("/event_lifecycle/replay", a.ReplayTrace)
	return r
}

// reliabilityRequest 打一次端点并解析 JSON(body 为空串时发空体)。
func reliabilityRequest(t *testing.T, r *gin.Engine, method, url, body string) (int, map[string]any) {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, url, reader)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("bad json %q: %v", w.Body.String(), err)
	}
	return w.Code, out
}

// newReliabilityTestRoom 构造一个 10 bot 开局中的房间(带 World),并把它登记
// 进 manager(供 VirtualCityRoomSource 查询)。快照目录注入 t.TempDir()。
func newReliabilityTestRoom(t *testing.T, roomID string) VirtualCityRoomSource {
	t.Helper()
	virtual_city.SetSnapshotDir(t.TempDir())
	m := virtual_city.NewManager(virtual_city.Config{MonthMs: 3000, AgentConcurrency: 4}, nil)
	r := m.CreateRoom(roomID)
	botUsers := map[int]string{}
	botModels := map[int]string{}
	for seat := 0; seat < 10; seat++ {
		botUsers[seat] = fmt.Sprintf("bot-%d", seat)
		botModels[seat] = "TestModel"
	}
	r.RegisterBotSeats(botUsers, botModels)
	if e := r.Start(nil); e != nil {
		t.Fatalf("start room: %v", e)
	}
	return m
}

// TestVirtualCityReliabilityAPI_SnapshotRoundTrip 存档 → 列表 → 读档。
func TestVirtualCityReliabilityAPI_SnapshotRoundTrip(t *testing.T) {
	const roomID = "rel-api-roundtrip"
	m := newReliabilityTestRoom(t, roomID)
	r := reliabilityAPIRouter(m)

	// ① 存档。
	_, body := reliabilityRequest(t, r, http.MethodPost,
		"/api/games/virtual_city/rooms/"+roomID+"/snapshot", `{"reason":"roundtrip"}`)
	if int(body["code"].(float64)) != errcode.OK {
		t.Fatalf("snapshot code = %v, want 0 (msg=%v)", body["code"], body["message"])
	}
	data := body["data"].(map[string]any)
	snap, ok := data["snapshot"].(map[string]any)
	if !ok || snap["snapshot_id"] == "" || snap["snapshot_id"] == nil {
		t.Fatalf("snapshot meta = %v, want snapshot_id", data["snapshot"])
	}
	snapshotID := snap["snapshot_id"].(string)

	// ② 列表:恒数组,含刚存的快照。
	_, body = reliabilityRequest(t, r, http.MethodGet,
		"/api/games/virtual_city/rooms/"+roomID+"/snapshots", "")
	if int(body["code"].(float64)) != errcode.OK {
		t.Fatalf("list code = %v, want 0", body["code"])
	}
	data = body["data"].(map[string]any)
	list, ok := data["snapshots"].([]any)
	if !ok || len(list) != 1 {
		t.Fatalf("snapshots = %v, want len 1", data["snapshots"])
	}

	// ③ 读档:元信息回显同一 snapshot_id。
	_, body = reliabilityRequest(t, r, http.MethodPost,
		"/api/games/virtual_city/rooms/"+roomID+"/snapshot/restore",
		`{"snapshot_id":"`+snapshotID+`"}`)
	if int(body["code"].(float64)) != errcode.OK {
		t.Fatalf("restore code = %v, want 0 (msg=%v)", body["code"], body["message"])
	}
	data = body["data"].(map[string]any)
	restored, ok := data["snapshot"].(map[string]any)
	if !ok || restored["snapshot_id"] != snapshotID {
		t.Fatalf("restored snapshot = %v, want id %q", data["snapshot"], snapshotID)
	}
}

// TestVirtualCityReliabilityAPI_RestoreUnknownSnapshot 未知 snapshot_id → 35066。
func TestVirtualCityReliabilityAPI_RestoreUnknownSnapshot(t *testing.T) {
	const roomID = "rel-api-unknown-snap"
	m := newReliabilityTestRoom(t, roomID)
	r := reliabilityAPIRouter(m)
	_, body := reliabilityRequest(t, r, http.MethodPost,
		"/api/games/virtual_city/rooms/"+roomID+"/snapshot/restore",
		`{"snapshot_id":"snap-does-not-exist"}`)
	if int(body["code"].(float64)) != errcode.ErrVirtualCitySnapshotNotFound {
		t.Fatalf("code = %v, want %d (msg=%v)", body["code"], errcode.ErrVirtualCitySnapshotNotFound, body["message"])
	}
}

// TestVirtualCityReliabilityAPI_EventLifecycleEmpty 留痕查询恒数组 + total。
func TestVirtualCityReliabilityAPI_EventLifecycleEmpty(t *testing.T) {
	const roomID = "rel-api-lifecycle"
	m := newReliabilityTestRoom(t, roomID)
	r := reliabilityAPIRouter(m)
	_, body := reliabilityRequest(t, r, http.MethodGet,
		"/api/games/virtual_city/rooms/"+roomID+"/event_lifecycle", "")
	if int(body["code"].(float64)) != errcode.OK {
		t.Fatalf("code = %v, want 0", body["code"])
	}
	data := body["data"].(map[string]any)
	traces, ok := data["traces"].([]any)
	if !ok || len(traces) != 0 {
		t.Fatalf("traces = %v, want []", data["traces"])
	}
	if int(data["total"].(float64)) != 0 {
		t.Fatalf("total = %v, want 0", data["total"])
	}
}

// TestVirtualCityReliabilityAPI_ReplayUnknownTrace 未知 trace_id → 35067。
func TestVirtualCityReliabilityAPI_ReplayUnknownTrace(t *testing.T) {
	const roomID = "rel-api-unknown-trace"
	m := newReliabilityTestRoom(t, roomID)
	r := reliabilityAPIRouter(m)
	_, body := reliabilityRequest(t, r, http.MethodPost,
		"/api/games/virtual_city/rooms/"+roomID+"/event_lifecycle/replay",
		`{"trace_id":"evt-does-not-exist"}`)
	if int(body["code"].(float64)) != errcode.ErrVirtualCityEventTraceNotFound {
		t.Fatalf("code = %v, want %d (msg=%v)", body["code"], errcode.ErrVirtualCityEventTraceNotFound, body["message"])
	}
}

// TestVirtualCityReliabilityAPI_DeleteSnapshot 删除 → {ok:true} 且列表收敛。
func TestVirtualCityReliabilityAPI_DeleteSnapshot(t *testing.T) {
	const roomID = "rel-api-delete"
	m := newReliabilityTestRoom(t, roomID)
	r := reliabilityAPIRouter(m)

	_, body := reliabilityRequest(t, r, http.MethodPost,
		"/api/games/virtual_city/rooms/"+roomID+"/snapshot", "")
	snapshotID := body["data"].(map[string]any)["snapshot"].(map[string]any)["snapshot_id"].(string)

	_, body = reliabilityRequest(t, r, http.MethodDelete,
		"/api/games/virtual_city/rooms/"+roomID+"/snapshot/"+snapshotID, "")
	if int(body["code"].(float64)) != errcode.OK {
		t.Fatalf("delete code = %v, want 0", body["code"])
	}
	if body["data"].(map[string]any)["ok"] != true {
		t.Fatalf("ok = %v, want true", body["data"])
	}

	_, body = reliabilityRequest(t, r, http.MethodGet,
		"/api/games/virtual_city/rooms/"+roomID+"/snapshots", "")
	if got := len(body["data"].(map[string]any)["snapshots"].([]any)); got != 0 {
		t.Fatalf("snapshots after delete = %d, want 0", got)
	}
}

// TestVirtualCityReliabilityAPI_RoomNotFound 房间不存在 → 35001(六端点全查)。
func TestVirtualCityReliabilityAPI_RoomNotFound(t *testing.T) {
	m := newReliabilityTestRoom(t, "rel-api-exists")
	r := reliabilityAPIRouter(m)
	const missing = "/api/games/virtual_city/rooms/nope"
	for _, tc := range []struct{ method, url, body string }{
		{http.MethodPost, missing + "/snapshot", ""},
		{http.MethodGet, missing + "/snapshots", ""},
		{http.MethodPost, missing + "/snapshot/restore", `{"snapshot_id":"x"}`},
		{http.MethodDelete, missing + "/snapshot/x", ""},
		{http.MethodGet, missing + "/event_lifecycle", ""},
		{http.MethodPost, missing + "/event_lifecycle/replay", `{"trace_id":"x"}`},
	} {
		_, body := reliabilityRequest(t, r, tc.method, tc.url, tc.body)
		if int(body["code"].(float64)) != errcode.ErrVirtualCityRoomNotFound {
			t.Fatalf("%s %s code = %v, want %d", tc.method, tc.url, body["code"], errcode.ErrVirtualCityRoomNotFound)
		}
	}
}

// TestVirtualCityReliabilityAPI_Rollback 注入场景 → POST 回滚 ⇒ code 0、
// snapshot_id 非空,且世界状态精确回到注入前(留痕关联的注入前快照生效)。
func TestVirtualCityReliabilityAPI_Rollback(t *testing.T) {
	const roomID = "rel-api-rollback"
	m := newReliabilityTestRoom(t, roomID)
	r := reliabilityAPIRouter(m)
	room := m.Get(roomID)
	if room == nil {
		t.Fatalf("room %q not registered", roomID)
	}

	room.MuLock()
	preHash := room.World.StateHash()
	room.MuUnlock()

	if _, _, e := room.ApplyScenario(virtual_city.ScenarioEarthquake, 3, 6, "", "rb-http"); e != nil {
		t.Fatalf("inject: %v", e)
	}
	room.MuLock()
	if room.World.StateHash() == preHash {
		room.MuUnlock()
		t.Fatal("inject must change state")
	}
	room.MuUnlock()

	_, body := reliabilityRequest(t, r, http.MethodPost,
		"/api/games/virtual_city/rooms/"+roomID+"/snapshot/rollback", "")
	if int(body["code"].(float64)) != errcode.OK {
		t.Fatalf("rollback code = %v, want 0 (msg=%v)", body["code"], body["message"])
	}
	data := body["data"].(map[string]any)
	snap, ok := data["snapshot"].(map[string]any)
	if !ok || snap["snapshot_id"] == "" || snap["snapshot_id"] == nil {
		t.Fatalf("rollback snapshot meta = %v, want snapshot_id", data["snapshot"])
	}

	room.MuLock()
	defer room.MuUnlock()
	if got := room.World.StateHash(); got != preHash {
		t.Errorf("state after rollback = %s, want %s", got, preHash)
	}
}

// TestVirtualCityReliabilityAPI_MissingRequiredField 缺必填 body 字段 → 40001。
func TestVirtualCityReliabilityAPI_MissingRequiredField(t *testing.T) {
	const roomID = "rel-api-validation"
	m := newReliabilityTestRoom(t, roomID)
	r := reliabilityAPIRouter(m)
	for _, tc := range []struct{ method, url, body string }{
		{http.MethodPost, "/api/games/virtual_city/rooms/" + roomID + "/snapshot/restore", `{}`},
		{http.MethodPost, "/api/games/virtual_city/rooms/" + roomID + "/event_lifecycle/replay", `{}`},
	} {
		_, body := reliabilityRequest(t, r, tc.method, tc.url, tc.body)
		if int(body["code"].(float64)) != errcode.ErrValidationFailed {
			t.Fatalf("%s %s code = %v, want %d", tc.method, tc.url, body["code"], errcode.ErrValidationFailed)
		}
	}
}
