// Package api — wealth_city_api_test.go: 居民人物卡档案两端点契约测试
// (2026-09-21 §档案锚定 §12 G7)。
//
//	分页(clamp)/q 搜索 / 详情 / 未命中 35042 / 房间不存在 35001 / 未建城 idle
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"LsmAgentGame/game/wealth"
	"LsmAgentGame/game/wealth/profession"

	"github.com/gin-gonic/gin"
)

// cityAPIFixtureDir 造 n 张最小可解析人物卡(docs 池;L1 域目录 Q-)。
func cityAPIFixtureDir(t *testing.T, n int) string {
	t.Helper()
	dir := t.TempDir()
	sub := filepath.Join(dir, "Q-金融与保险")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for i := 0; i < n; i++ {
		md := fmt.Sprintf(`---
id: R%04d
name: 接口居民%04d
occupation: 接口测试职业%02d
income_monthly: %d
monthly_expense: 4000
savings_stock: 30000
age: 28
work_intensity: 中
health_grade: A
risk_preference: balanced
marital: 单身
personality: ["务实主义"]
opening_hook: 我是居民档案接口测试居民,验证 REST 分页与详情端点契约。
goals_short: ["5 年内把储蓄翻一番"]
housing_city: 一线城市
employment_type: 全职
---
正文略`, 7000+i, i, i%30, 6000+100*i)
		path := filepath.Join(sub, fmt.Sprintf("R%04d.md", i))
		if err := os.WriteFile(path, []byte(md), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	return dir
}

// startAnchoredRoom 建 docs 房(12 bot + resident_count 城市 Sega)并等待锚定 ready。
func startAnchoredRoom(t *testing.T, roomID string, poolCards, residents int) (*wealth.Manager, *wealth.WealthRoom) {
	t.Helper()
	m := wealth.NewManager(wealth.Config{MonthMs: 3000, PoolDefault: "docs", Seed: 555}, nil)
	m.SetLoader(profession.NewLoader(cityAPIFixtureDir(t, poolCards)))
	r := m.CreateRoom(roomID)
	botUsers := make(map[int]string, 12)
	for seat := 0; seat < 12; seat++ {
		botUsers[seat] = "b" + fmt.Sprint(seat)
	}
	r.RegisterBotSeats(botUsers, nil, nil)
	r.SetResidentCount(residents)
	if e := r.Start(nil); e != nil {
		t.Fatalf("start: %v", e)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, _, prog := r.CityProfilePage(0, 1, ""); prog.Status == "ready" {
			return m, r
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("anchor did not reach ready in time")
	return nil, nil
}

// cityAPIRouter 挂载两端点(绕过鉴权中间件 —— handler 无鉴权逻辑,由
// router.go 的 AuthRequired 统一保证,与 survey 同级)。
func cityAPIRouter(rooms WealthRoomSource) *gin.Engine {
	gin.SetMode(gin.TestMode)
	a := NewWealthCityAPI(rooms)
	r := gin.New()
	g := r.Group("/api/games/wealth/rooms/:id")
	g.GET("/city/residents", a.ListResidents)
	g.GET("/city/residents/:cardId", a.GetResident)
	return r
}

func getCityJSON(t *testing.T, r *gin.Engine, url string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("bad json %q: %v", w.Body.String(), err)
	}
	return w.Code, body
}

// TestWealthCityAPI_ListDetailNotFound(§12 G7)。
func TestWealthCityAPI_ListDetailNotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: 跳过档案接口端到端")
	}
	const residents = 8
	m, _ := startAnchoredRoom(t, "room-cityapi", 12, residents)
	router := cityAPIRouter(m)

	// ① 分页列表:progress ready + residents 全量 + matched。
	_, body := getCityJSON(t, router, "/api/games/wealth/rooms/room-cityapi/city/residents")
	if code := int(body["code"].(float64)); code != 0 {
		t.Fatalf("list code = %d, want 0 (body=%v)", code, body)
	}
	data := body["data"].(map[string]any)
	progress := data["progress"].(map[string]any)
	if progress["status"] != "ready" || int(progress["anchored"].(float64)) != residents {
		t.Fatalf("list progress drifted: %+v", progress)
	}
	residentsArr := data["residents"].([]any)
	if len(residentsArr) != residents || int(data["matched"].(float64)) != residents {
		t.Fatalf("list residents=%d matched=%v, want %d", len(residentsArr), data["matched"], residents)
	}
	first := residentsArr[0].(map[string]any)
	if first["name"] == "" || first["card_id"] == "" || first["source_file"] == "" {
		t.Fatalf("profile projection missing wire fields: %+v", first)
	}
	// 锚定集是 12 张卡池里随机抽的 8 张 —— 后续 q/详情断言必须用**已锚定**的卡号
	// (DrawPaths 不重复抽取,抽中集合 = residentsArr 的 card_id 集)。
	anchoredID := first["card_id"].(string)

	// ② limit clamp:?limit=999 → ≤200;?limit=0 → ≥1。
	_, body = getCityJSON(t, router, "/api/games/wealth/rooms/room-cityapi/city/residents?limit=999")
	if got := len(body["data"].(map[string]any)["residents"].([]any)); got != residents {
		t.Fatalf("limit clamp upper broke page: %d, want %d", got, residents)
	}
	_, body = getCityJSON(t, router, "/api/games/wealth/rooms/room-cityapi/city/residents?limit=0")
	if got := len(body["data"].(map[string]any)["residents"].([]any)); got != 1 {
		t.Fatalf("limit clamp lower must be 1, got %d", got)
	}

	// ③ offset 分页:?offset=6&limit=50 → 剩 2 条。
	_, body = getCityJSON(t, router, "/api/games/wealth/rooms/room-cityapi/city/residents?offset=6")
	if got := len(body["data"].(map[string]any)["residents"].([]any)); got != residents-6 {
		t.Fatalf("offset page = %d, want %d", got, residents-6)
	}

	// ④ q 搜索:唯一卡号子串(已锚定卡号)。
	_, body = getCityJSON(t, router, "/api/games/wealth/rooms/room-cityapi/city/residents?q="+anchoredID)
	data = body["data"].(map[string]any)
	if got := int(data["matched"].(float64)); got != 1 {
		t.Fatalf("q by card id matched = %d, want 1", got)
	}
	hit := data["residents"].([]any)[0].(map[string]any)
	if hit["card_id"] != anchoredID {
		t.Fatalf("q hit = %v, want %s", hit["card_id"], anchoredID)
	}

	// ⑤ 详情:card_id 全字段。
	_, body = getCityJSON(t, router, "/api/games/wealth/rooms/room-cityapi/city/residents/"+anchoredID)
	if code := int(body["code"].(float64)); code != 0 {
		t.Fatalf("detail code = %d", code)
	}
	resident := body["data"].(map[string]any)["resident"].(map[string]any)
	if resident["card_id"] != anchoredID || resident["name"] == "" || resident["domain_name"] != "Q-金融与保险" {
		t.Fatalf("detail projection drifted: %+v", resident)
	}

	// ⑥ 详情未命中 → 35042(契约 §7 新码;35013 已被 ErrLoanNotFound 占用)。
	_, body = getCityJSON(t, router, "/api/games/wealth/rooms/room-cityapi/city/residents/NOPE")
	if code := int(body["code"].(float64)); code != 35042 {
		t.Fatalf("unknown card code = %v, want 35042 (body=%v)", body["code"], body)
	}

	// ⑦ 房间不存在 → 35001。
	_, body = getCityJSON(t, router, "/api/games/wealth/rooms/no-such-room/city/residents")
	if code := int(body["code"].(float64)); code != 35001 {
		t.Fatalf("missing room code = %v, want 35001", body["code"])
	}

	// ⑧ 未建城(未 Start / resident_count=0)→ code 0 + idle 空列表(不算错误)。
	m2 := wealth.NewManager(wealth.Config{MonthMs: 3000, PoolDefault: "docs"}, nil)
	m2.SetLoader(profession.NewLoader(cityAPIFixtureDir(t, 4)))
	m2.CreateRoom("room-nocity")
	_, body = getCityJSON(t, cityAPIRouter(m2), "/api/games/wealth/rooms/room-nocity/city/residents")
	if code := int(body["code"].(float64)); code != 0 {
		t.Fatalf("city-less room code = %v, want 0", body["code"])
	}
	data = body["data"].(map[string]any)
	if data["progress"].(map[string]any)["status"] != "idle" {
		t.Fatalf("city-less progress = %+v, want idle", data["progress"])
	}
	if got := len(data["residents"].([]any)); got != 0 {
		t.Fatalf("city-less residents = %d, want 0", got)
	}
}
