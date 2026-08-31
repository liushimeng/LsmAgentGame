// Package api — debate_history_api_test.go(2026-08-31 §20260831-08)。
//
// 覆盖历史对局 / 辩题池端点的纯逻辑部分(无 DB):
//   - gormDB 未接线时 history / create-topic 返回 ErrDB 降级封装
//   - 辩题详情:内置池命中 / 双池均未命中(ErrTopicNotFound)
//   - POST /topics 权限门:普通用户 10403;管理员 + 空文本 20001
//   - Topics 列表:无 DB 时仍返回完整内置池(合并路径降级)
//   - customTopicMatches / rawJSON 纯函数
//
// 真实 CRUD / 分页 SQL 依赖 MySQL,由集成环境验证(api 包测试基建无
// sqlite/sqlmock,照抄 model_log_api_test.go 的 stub 模式)。
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"LsmAgentGame/errcode"
	"LsmAgentGame/game/debate"
	"LsmAgentGame/models"

	"github.com/gin-gonic/gin"
)

// newDebateTestRouter 构造仅含被测端点的路由(authCtx 模拟 AuthRequired
// 注入的 user_id;角色由 stubAuthChecker 返回)。
func newDebateTestRouter(role models.UserType) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := NewDebateAPI(debate.NewDebateManager(), nil, nil, &stubAuthChecker{role: role})
	r := gin.New()
	uid := "test-user"
	{
		r.GET("/api/games/debate/history", authCtx(uid, int(role)), h.HistoryList)
		r.GET("/api/games/debate/history/:id", authCtx(uid, int(role)), h.HistoryDetail)
		r.GET("/api/games/debate/topics", authCtx(uid, int(role)), h.Topics)
		r.GET("/api/games/debate/topics/:id", authCtx(uid, int(role)), h.TopicDetail)
		r.POST("/api/games/debate/topics", authCtx(uid, int(role)), h.CreateTopic)
	}
	return r
}

// decodeEnvelope 解析 {code, message, data} 响应。
func decodeEnvelope(t *testing.T, w *httptest.ResponseRecorder) (int, map[string]any) {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response not JSON: %v body=%s", err, w.Body.String())
	}
	codeF, ok := body["code"].(float64)
	if !ok {
		t.Fatalf("missing numeric code: %s", w.Body.String())
	}
	return int(codeF), body
}

func TestDebateHistoryListNoDB(t *testing.T) {
	r := newDebateTestRouter(models.UserTypeNormal)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/games/debate/history?page=1&page_size=20", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if code, _ := decodeEnvelope(t, w); code != errcode.ErrDB {
		t.Fatalf("expected ErrDB, got %d", code)
	}
}

func TestDebateHistoryDetailNoDB(t *testing.T) {
	r := newDebateTestRouter(models.UserTypeNormal)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/games/debate/history/debate_abc", nil))
	if code, _ := decodeEnvelope(t, w); code != errcode.ErrDB {
		t.Fatalf("expected ErrDB, got %d", code)
	}
}

func TestDebateTopicDetailBuiltinHit(t *testing.T) {
	r := newDebateTestRouter(models.UserTypeNormal)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/games/debate/topics/classic_001", nil))
	code, body := decodeEnvelope(t, w)
	if code != errcode.OK {
		t.Fatalf("expected OK, got %d", code)
	}
	data, _ := body["data"].(map[string]any)
	if data == nil || data["id"] != "classic_001" {
		t.Fatalf("data.id != classic_001: %v", body["data"])
	}
}

func TestDebateTopicDetailMiss(t *testing.T) {
	r := newDebateTestRouter(models.UserTypeNormal)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/games/debate/topics/nonexistent_999", nil))
	if code, _ := decodeEnvelope(t, w); code != errcode.ErrTopicNotFound {
		t.Fatalf("expected ErrTopicNotFound, got %d", code)
	}
}

func TestDebateTopicsListWithoutDBStillReturnsBuiltin(t *testing.T) {
	r := newDebateTestRouter(models.UserTypeNormal)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/games/debate/topics", nil))
	code, body := decodeEnvelope(t, w)
	if code != errcode.OK {
		t.Fatalf("expected OK, got %d", code)
	}
	data, _ := body["data"].([]any)
	if len(data) < 30 {
		t.Fatalf("builtin pool should have >=30 topics, got %d", len(data))
	}
}

func TestDebateCreateTopicNormalUserForbidden(t *testing.T) {
	r := newDebateTestRouter(models.UserTypeNormal)
	body := bytes.NewBufferString(`{"text":"测试辩题"}`)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/games/debate/topics", body))
	if code, _ := decodeEnvelope(t, w); code != errcode.ErrPermissionDenied {
		t.Fatalf("normal user should be denied, got %d", code)
	}
}

func TestDebateCreateTopicAdminValidationBeforeDB(t *testing.T) {
	r := newDebateTestRouter(models.UserTypeAdmin)

	// 管理员 + 空文本 → 校验错误(在 DB 检查之前)。
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/games/debate/topics",
		bytes.NewBufferString(`{"text":"   "}`)))
	if code, _ := decodeEnvelope(t, w); code != errcode.ErrValidationFailed {
		t.Fatalf("empty text should fail validation, got %d", code)
	}

	// 管理员 + 合法文本 + 无 DB → ErrDB 降级。
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, httptest.NewRequest(http.MethodPost, "/api/games/debate/topics",
		bytes.NewBufferString(`{"text":"测试辩题","type":"value"}`)))
	if code, _ := decodeEnvelope(t, w2); code != errcode.ErrDB {
		t.Fatalf("admin + valid text without DB should degrade to ErrDB, got %d", code)
	}

	// 非法字段 → DisallowUnknownFields 校验错误。
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, httptest.NewRequest(http.MethodPost, "/api/games/debate/topics",
		bytes.NewBufferString(`{"text":"x","unknown_field":1}`)))
	if code, _ := decodeEnvelope(t, w3); code != errcode.ErrValidationFailed {
		t.Fatalf("unknown field should fail validation, got %d", code)
	}
}

func TestDebateCreateTopicNoLogin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewDebateAPI(debate.NewDebateManager(), nil, nil, &stubAuthChecker{role: models.UserTypeAdmin})
	r := gin.New()
	r.POST("/api/games/debate/topics", h.CreateTopic) // 不挂 authCtx → 无 user_id
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/games/debate/topics",
		bytes.NewBufferString(`{"text":"x"}`)))
	if code, _ := decodeEnvelope(t, w); code != errcode.ErrAuthMissingToken {
		t.Fatalf("no login should yield ErrAuthMissingToken, got %d", code)
	}
}

func TestCustomTopicMatches(t *testing.T) {
	topic := debate.DebateTopic{
		Text: "AI 是否拥有人格", Type: "tech", Category: "custom",
		Keywords: []string{"AI", "人格"},
	}
	cases := []struct {
		q, typ, cat string
		want        bool
	}{
		{"", "", "", true},                // 无条件全命中
		{"ai", "", "", true},              // 大小写不敏感命中 text
		{"人格", "", "", true},              // 命中 text(中文)
		{"不存在", "", "", false},            // 未命中
		{"", "tech", "", true},            // type 命中
		{"", "policy", "", false},         // type 不符
		{"", "", "custom", true},          // category 命中
		{"", "", "classic", false},        // category 不符
		{"人格", "tech", "custom", true},    // 三条件同时命中
		{"人格", "policy", "custom", false}, // type 一票否决
	}
	for _, c := range cases {
		if got := customTopicMatches(topic, c.q, c.typ, c.cat); got != c.want {
			t.Errorf("customTopicMatches(q=%q,type=%q,cat=%q) = %v, want %v",
				c.q, c.typ, c.cat, got, c.want)
		}
	}
}

func TestRawJSON(t *testing.T) {
	if rawJSON(`[{"team_id":0}]`) == nil {
		t.Error("valid JSON should return non-nil RawMessage")
	}
	if rawJSON("not json") != nil {
		t.Error("invalid JSON should return nil")
	}
	if rawJSON("") != nil {
		t.Error("empty string should return nil")
	}
	if rawJSON("null") == nil {
		t.Error("literal null is valid JSON and should be preserved")
	}
}

// TestHistoryRoomItemJSONContract 锁定列表条目 wire 契约(§20260831-08 契约对齐):
// 主键序列化为 room_id(而非 id),且必须携带前端消费的 topic_type +
// best_debater_team_id(DebateHistoryListPanel 拼最佳辩手队名用)。
func TestHistoryRoomItemJSONContract(t *testing.T) {
	item := historyRoomItem{
		ID:                "debate_px",
		TopicText:         "AI 是否拥有人格",
		TopicType:         "tech",
		Mode:              "two_team",
		Status:            "over",
		WinnerTeamID:      1,
		BestDebaterSeat:   2,
		BestDebaterTeamID: 1,
		FinishedAt:        1700000000,
		CreatedBy:         "u1",
	}
	b, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m["room_id"] != "debate_px" {
		t.Errorf("room_id mismatch: %v", m["room_id"])
	}
	if _, exists := m["id"]; exists {
		t.Error("legacy \"id\" key must not appear in history list items")
	}
	if m["topic_type"] != "tech" {
		t.Errorf("topic_type mismatch: %v", m["topic_type"])
	}
	if m["best_debater_team_id"] != float64(1) {
		t.Errorf("best_debater_team_id mismatch: %v", m["best_debater_team_id"])
	}
}

// TestHistoryRoomDetailJSONShadowing 锁定「嵌入原始行 + 重声明 JSON 列」的
// 遮蔽语义:team_config / result 等必须序列化为嵌套对象(而非带引号字符串);
// room_id 外层遮蔽字段与嵌入 id 同值(前端统一按 room_id 消费)。
func TestHistoryRoomDetailJSONShadowing(t *testing.T) {
	row := models.TLsmGameDebateRoom{
		ID:         "debate_zz",
		TeamConfig: `[{"team_id":0},{"team_id":1}]`,
		Result:     `{"winner_team_id":1}`,
	}
	b, err := json.Marshal(buildHistoryRoomDetail(row))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	tc, ok := m["team_config"].([]any)
	if !ok || len(tc) != 2 {
		t.Errorf("team_config should be a nested array, got %T", m["team_config"])
	}
	res, ok := m["result"].(map[string]any)
	if !ok || res["winner_team_id"] != float64(1) {
		t.Errorf("result should be a nested object, got %T", m["result"])
	}
	if m["id"] != "debate_zz" {
		t.Errorf("embedded row field lost: %v", m["id"])
	}
	if m["room_id"] != "debate_zz" {
		t.Errorf("detail room_id shadow mismatch: %v", m["room_id"])
	}
}

func TestTopicFromModel(t *testing.T) {
	row := models.TLsmGameDebateTopic{
		ID: "custom_abc", Text: "题面", Type: "custom", Category: "custom",
		ProPosition: "正", ConPosition: "反", Keywords: `["k1","k2"]`,
		Difficulty: 4, CreatedBy: "admin1", CreatedAt: 1700000000, IsOfficial: false,
	}
	tp := topicFromModel(row)
	if tp.ID != "custom_abc" || tp.Text != "题面" || tp.Difficulty != 4 || tp.IsOfficial {
		t.Errorf("topicFromModel mismatch: %+v", tp)
	}
	if len(tp.Keywords) != 2 || tp.Keywords[0] != "k1" {
		t.Errorf("keywords not restored: %v", tp.Keywords)
	}

	// 非法 Keywords JSON → 静默忽略,不 panic。
	row.Keywords = "{bad json"
	if tp2 := topicFromModel(row); len(tp2.Keywords) != 0 {
		t.Errorf("invalid keywords JSON should yield empty slice, got %v", tp2.Keywords)
	}
}
