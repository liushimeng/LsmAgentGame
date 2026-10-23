// Package citychrome — agent_mission_test.go: RunMission 遥测/经验注入/
// 页面回收单测(批次77 §3.3;假 LLMRegistry + httptest 假底座)。
package citychrome

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	llmtypes "LsmAgentGame/llm/types"
)

// missionFakeProvider 按脚本逐轮返回响应;记录每次请求的 messages 快照
// (用于断言经验注入的 prompt 内容)。
type missionFakeProvider struct {
	mu       sync.Mutex
	script   []llmtypes.LLMResponse
	calls    int
	messages [][]llmtypes.Message
}

func (f *missionFakeProvider) Chat(ctx context.Context, key string, req llmtypes.LLMRequest) (llmtypes.LLMResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := f.calls
	f.calls++
	f.messages = append(f.messages, append([]llmtypes.Message(nil), req.Messages...))
	if n >= len(f.script) {
		return llmtypes.LLMResponse{}, errors.New("fake: script exhausted")
	}
	return f.script[n], nil
}

func (f *missionFakeProvider) ChatStream(ctx context.Context, key string, req llmtypes.LLMRequest) (io.ReadCloser, error) {
	return nil, errors.New("fake: no stream")
}

func (f *missionFakeProvider) ProviderType() string { return "fake-mission" }

// missionFakeRegistry 是假 registry(始终返回同一 provider)。
type missionFakeRegistry struct{ p llmtypes.LLMProvider }

func (r missionFakeRegistry) Get(modelKey string) (llmtypes.LLMProvider, string, error) {
	return r.p, "k", nil
}

func (r missionFakeRegistry) GetThinkingEnabled(modelKey string) (bool, int) { return false, 0 }

func (r missionFakeRegistry) ListEnabled() []llmtypes.ModelInfo { return nil }

// missionErrRegistry 的 Get 恒报错(模拟模型 key 缺失/上游不可用)。
type missionErrRegistry struct{}

func (missionErrRegistry) Get(modelKey string) (llmtypes.LLMProvider, string, error) {
	return nil, "", errors.New("model missing")
}

func (missionErrRegistry) GetThinkingEnabled(modelKey string) (bool, int) { return false, 0 }

func (missionErrRegistry) ListEnabled() []llmtypes.ModelInfo { return nil }

// newMissionTestAgent 构造注入假 provider + 遥测日志的最小 Agent。
func newMissionTestAgent(t *testing.T, p llmtypes.LLMProvider, baseURL string) (*Agent, *MissionLog) {
	t.Helper()
	mlog := NewMissionLog()
	a := NewAgent(AgentConfig{
		ModelKey:           "Fake-model",
		MaxStepsPerMission: 14,
		SymbolsPerCategory: 12,
		NewsLimit:          20,
		MissionTimeout:     5 * time.Second,
	}, NewWebUseClient(baseURL, "", 5000), missionFakeRegistry{p: p})
	a.SetMissionLog(mlog)
	return a, mlog
}

// missionToolUse 造一条 tool_use 响应。
func missionToolUse(id, name string, input map[string]any) llmtypes.LLMResponse {
	return llmtypes.LLMResponse{
		StopReason: "tool_use",
		Content: []llmtypes.ContentBlock{
			{Type: "tool_use", ID: id, Name: name, Input: input},
		},
	}
}

// missionASharePayload 造一份合法 web_done payload。
func missionASharePayload() map[string]any {
	return map[string]any{
		"category": CategoryAShare,
		"source":   "腾讯行情",
		"quotes": []any{map[string]any{
			"symbol": "000001", "name": "上证指数", "price": 3221.5, "change_pct": 0.82,
		}},
		"news": []any{map[string]any{"title": "A股放量上涨", "url": "https://x/1", "published_at": 1729300000}},
	}
}

// TestRunMission_TelemetryOK 用例 A:模型先 web_plan 再 web_done ⇒ 遥测
// status=ok / Plan 非空 / StepsUsed=2 / 计数正确。
func TestRunMission_TelemetryOK(t *testing.T) {
	fp := &missionFakeProvider{script: []llmtypes.LLMResponse{
		missionToolUse("tu-1", ToolWebPlan, map[string]any{
			"goal":  "采 A 股指数",
			"steps": []any{"打开腾讯行情", "提取指数后 web_done"},
		}),
		missionToolUse("tu-2", ToolWebDone, map[string]any{"payload": missionASharePayload()}),
	}}
	a, mlog := newMissionTestAgent(t, fp, "http://127.0.0.1:1")
	got, err := a.RunMission(context.Background(), BuildMission(CategoryAShare))
	if err != nil {
		t.Fatalf("RunMission: %v", err)
	}
	if got.Source != "腾讯行情" || len(got.Quotes) != 1 || len(got.News) != 1 {
		t.Errorf("payload = %+v", got)
	}
	recs := mlog.Snapshot()
	if len(recs) != 1 {
		t.Fatalf("mission records = %d, want 1 (同 ID 覆盖)", len(recs))
	}
	r := recs[0]
	if r.Status != MissionStatusOK {
		t.Errorf("status = %q, want ok", r.Status)
	}
	if r.StepsUsed != 2 {
		t.Errorf("steps_used = %d, want 2", r.StepsUsed)
	}
	if r.MaxSteps != 14 {
		t.Errorf("max_steps = %d, want 14", r.MaxSteps)
	}
	if want := "采 A 股指数：打开腾讯行情 → 提取指数后 web_done"; r.Plan != want {
		t.Errorf("plan = %q, want %q", r.Plan, want)
	}
	if r.Source != "腾讯行情" || r.Quotes != 1 || r.News != 1 {
		t.Errorf("record counts/source wrong: %+v", r)
	}
	if r.Error != "" {
		t.Errorf("error = %q, want empty", r.Error)
	}
	if r.StartedAt <= 0 {
		t.Errorf("started_at = %d, want > 0", r.StartedAt)
	}
	if !strings.HasPrefix(r.ID, CategoryAShare+"-") {
		t.Errorf("id = %q, want prefix %q", r.ID, CategoryAShare+"-")
	}
	// 无历史记录时 prompt 不得含经验段。
	prompt := firstPromptText(fp, 0)
	if prompt == "" {
		t.Fatal("no request captured")
	}
	if strings.Contains(prompt, "上次采集记录") {
		t.Error("prompt must not carry an experience section without history")
	}
}

// TestRunMission_TelemetryFail 用例 B:registry.Get 报错 ⇒ status=fail 且带原因。
func TestRunMission_TelemetryFail(t *testing.T) {
	mlog := NewMissionLog()
	a := NewAgent(AgentConfig{
		ModelKey:           "Gone-model",
		MaxStepsPerMission: 14,
		SymbolsPerCategory: 12,
		NewsLimit:          20,
		MissionTimeout:     5 * time.Second,
	}, NewWebUseClient("http://127.0.0.1:1", "", 500), missionErrRegistry{})
	a.SetMissionLog(mlog)
	if _, err := a.RunMission(context.Background(), BuildMission(CategoryHK)); err == nil {
		t.Fatal("want error when registry.Get fails")
	}
	recs := mlog.Snapshot()
	if len(recs) != 1 {
		t.Fatalf("mission records = %d, want 1", len(recs))
	}
	r := recs[0]
	if r.Status != MissionStatusFail {
		t.Errorf("status = %q, want fail", r.Status)
	}
	if !strings.Contains(r.Error, "model missing") {
		t.Errorf("error = %q, want it to mention the registry failure", r.Error)
	}
	if r.StepsUsed != 1 {
		t.Errorf("steps_used = %d, want 1 (第 1 轮 LLM 调用即失败)", r.StepsUsed)
	}
	if r.Category != CategoryHK {
		t.Errorf("category = %q, want hk", r.Category)
	}
}

// TestRunMission_ExperienceInjection 用例 C:mlog 有历史 ⇒ mission prompt 末尾
// 注入「上次采集记录」段(最近成功 + 最近失败原因)。
func TestRunMission_ExperienceInjection(t *testing.T) {
	fp := &missionFakeProvider{script: []llmtypes.LLMResponse{
		missionToolUse("tu-1", ToolWebDone, map[string]any{"payload": missionASharePayload()}),
	}}
	a, mlog := newMissionTestAgent(t, fp, "http://127.0.0.1:1")
	mlog.Append(MissionRecord{ID: "a_share-9", Category: CategoryAShare, Status: MissionStatusOK,
		Source: "腾讯行情", StepsUsed: 3, Quotes: 5, News: 1})
	mlog.Append(MissionRecord{ID: "a_share-8", Category: CategoryAShare, Status: MissionStatusFail,
		Error: "步数用尽且无合法 web_done"})
	if _, err := a.RunMission(context.Background(), BuildMission(CategoryAShare)); err != nil {
		t.Fatalf("RunMission: %v", err)
	}
	prompt := firstPromptText(fp, 0)
	if !strings.Contains(prompt, "上次采集记录") {
		t.Fatalf("prompt should carry the experience section:\n%s", prompt)
	}
	if !strings.Contains(prompt, "source=腾讯行情") || !strings.Contains(prompt, "步数=3") {
		t.Errorf("experience section missing last-success summary:\n%s", prompt)
	}
	if !strings.Contains(prompt, "步数用尽且无合法 web_done") {
		t.Errorf("experience section missing last-failure reason:\n%s", prompt)
	}
}

// TestRunMission_RecyclesOpenedPages 资源卫生:mission 结束回收打开的页面。
func TestRunMission_RecyclesOpenedPages(t *testing.T) {
	var mu sync.Mutex
	var closed []string
	srv := newMockServer(t, func(path string, body map[string]any) (int, string, any) {
		switch path {
		case "/NewChromePage":
			return 0, "ok", map[string]any{"page_id": "p_m1", "title": "t", "final_url": "https://x/"}
		case "/BatchEval":
			return 0, "ok", map[string]any{"results": []any{
				map[string]any{"index": 0, "ok": true, "result": map[string]any{"type": "string", "value": "T"}},
				map[string]any{"index": 1, "ok": true, "result": map[string]any{"type": "string", "value": "B"}},
			}}
		case "/CloseChromePage":
			id, _ := body["page_id"].(string)
			mu.Lock()
			closed = append(closed, id)
			mu.Unlock()
			return 0, "ok", map[string]any{"closed": true}
		}
		return 1000, "bad path", nil
	})
	defer srv.Close()
	fp := &missionFakeProvider{script: []llmtypes.LLMResponse{
		missionToolUse("tu-1", ToolWebRead, map[string]any{"url": "https://quote.example/idx"}),
		missionToolUse("tu-2", ToolWebDone, map[string]any{"payload": missionASharePayload()}),
	}}
	a, _ := newMissionTestAgent(t, fp, srv.URL)
	if _, err := a.RunMission(context.Background(), BuildMission(CategoryAShare)); err != nil {
		t.Fatalf("RunMission: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	found := false
	for _, id := range closed {
		if id == "p_m1" {
			found = true
		}
	}
	if !found {
		t.Errorf("RunMission must recycle opened pages, closed = %v", closed)
	}
}

// firstPromptText 取第 n 次请求的首条 user 文本(空串表示没有该请求)。
func firstPromptText(fp *missionFakeProvider, n int) string {
	fp.mu.Lock()
	defer fp.mu.Unlock()
	if n >= len(fp.messages) || len(fp.messages[n]) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, b := range fp.messages[n][0].Content {
		if b.Type == "text" {
			sb.WriteString(b.Text)
		}
	}
	return sb.String()
}

// TestMissionLog_RecordIDFormat 遥测 ID 形如 "<category>-<unixMilli>"。
func TestMissionLog_RecordIDFormat(t *testing.T) {
	l := NewMissionLog()
	l.Append(MissionRecord{ID: fmt.Sprintf("%s-%d", CategoryNews, time.Now().UnixMilli()),
		Category: CategoryNews, Status: MissionStatusOK})
	if got := l.Snapshot()[0].ID; !strings.HasPrefix(got, CategoryNews+"-") {
		t.Errorf("id = %q, want prefix %q", got, CategoryNews+"-")
	}
}

// TestRunMission_VisitedTelemetry 批次78 §3.3-5:mission 出口把
// web_open/web_read 访问过的 URL 写进 rec.Visited(新→旧去重)。
func TestRunMission_VisitedTelemetry(t *testing.T) {
	srv := newMockServer(t, func(path string, body map[string]any) (int, string, any) {
		switch path {
		case "/NewChromePage":
			return 0, "ok", map[string]any{"page_id": "p_v1", "title": "t", "final_url": "https://x/"}
		case "/BatchEval":
			return 0, "ok", map[string]any{"results": []any{
				map[string]any{"index": 0, "ok": true, "result": "T"},
				map[string]any{"index": 1, "ok": true, "result": "B"},
			}}
		case "/CloseChromePage":
			return 0, "ok", map[string]any{"closed": true}
		}
		return 1000, "bad path", nil
	})
	defer srv.Close()
	fp := &missionFakeProvider{script: []llmtypes.LLMResponse{
		missionToolUse("tu-1", ToolWebRead, map[string]any{"url": "https://qt.gtimg.cn/q=sh000001"}),
		missionToolUse("tu-2", ToolWebDone, map[string]any{"payload": missionASharePayload()}),
	}}
	a, mlog := newMissionTestAgent(t, fp, srv.URL)
	if _, err := a.RunMission(context.Background(), BuildMission(CategoryAShare)); err != nil {
		t.Fatalf("RunMission: %v", err)
	}
	recs := mlog.Snapshot()
	if len(recs) != 1 {
		t.Fatalf("records = %d, want 1", len(recs))
	}
	if len(recs[0].Visited) == 0 {
		t.Fatalf("rec.Visited must not be empty: %+v", recs[0])
	}
	if recs[0].Visited[0] != "https://qt.gtimg.cn/q=sh000001" {
		t.Errorf("rec.Visited = %v", recs[0].Visited)
	}
}

// TestRoleText_DeterministicParseDiscipline 批次78 §3.3-4:角色段必须含
// 「确定性解析纪律」段(行情优先用 web_read 的 parse、不要自己切数字、
// SPA 用 wait_selector、拦截时换源);既有纪律段逐字保留。
func TestRoleText_DeterministicParseDiscipline(t *testing.T) {
	for _, want := range []string{
		"【确定性解析纪律(批次78)】",
		"web_read 的 parse",
		"不要自己按 ~ 或 , 手工切分数字",
		"wait_selector",
	} {
		if !strings.Contains(roleText, want) {
			t.Errorf("roleText missing %q", want)
		}
	}
	// 既有段落逐字保留。
	for _, want := range []string{
		"【规划与效率(批次77)】",
		"【纪律】",
		"【收敛流程(重要,防步数耗尽)】",
		"【输出 schema(web_done.payload)】",
	} {
		if !strings.Contains(roleText, want) {
			t.Errorf("roleText lost existing section %q", want)
		}
	}
}
