// Package citysystem — agent_test.go: ReAct 主循环与线路策略单测
// (2026-11-03 §批次81)。
package citysystem

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	agentroot "LsmAgentGame/agent"
	llmtypes "LsmAgentGame/llm/types"
)

// TestNewAgentDefaults 零值 cfg 归一。
func TestNewAgentDefaults(t *testing.T) {
	a := NewAgent(nil, Config{}, nil, nil, nil, nil)
	if a.cfg.MaxSteps != 8 {
		t.Errorf("MaxSteps = %d, want 8", a.cfg.MaxSteps)
	}
	if a.cfg.MaxMessages != 40 {
		t.Errorf("MaxMessages = %d, want 40", a.cfg.MaxMessages)
	}
	if a.cfg.RequestTimeout != 180*time.Second {
		t.Errorf("RequestTimeout = %v, want 180s", a.cfg.RequestTimeout)
	}
	if a.cfg.ToolResultLimit != 4000 {
		t.Errorf("ToolResultLimit = %d, want 4000", a.cfg.ToolResultLimit)
	}
	if a.store == nil || a.emit == nil {
		t.Fatal("nil store/emit must be defaulted")
	}
}

// TestRun_TextOnly 纯文本作答:事件流 + 会话持久化。
func TestRun_TextOnly(t *testing.T) {
	assertAgentClassWired(t)
	room := newTestRoom(t, "R-run")
	fp := &fakeProvider{responses: []llmtypes.LLMResponse{textResponse("你好,城市运行正常。")}}
	a, events := newTestAgent(t, room, &fakeRegistry{p: fp})

	if err := a.Run(context.Background(), "u1", "你好"); err != nil {
		t.Fatalf("run: %v", err)
	}
	got := collectEvents(events)
	for _, kind := range []string{EventAgentStart, EventAgentEnd} {
		if !hasEventKind(got, kind) {
			t.Fatalf("missing event %q in %v", kind, kinds(got))
		}
	}
	if hasEventKind(got, EventError) {
		t.Fatalf("unexpected error event: %v", kinds(got))
	}
	// agent_end 携带最终文本。
	for _, e := range got {
		if e.Kind == EventAgentEnd {
			data, _ := e.Data.(map[string]any)
			if data["text"] != "你好,城市运行正常。" {
				t.Fatalf("agent_end text = %v", data["text"])
			}
		}
	}
	msgs := a.store.Get("R-run/u1")
	if len(msgs) != 2 {
		t.Fatalf("store len = %d, want 2", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[1].Role != "assistant" {
		t.Fatalf("roles = %s/%s, want user/assistant", msgs[0].Role, msgs[1].Role)
	}
}

// TestRun_ToolCall 一轮工具调用:tool_start/tool_result 事件 + 严格交替回灌。
func TestRun_ToolCall(t *testing.T) {
	room := newTestRoom(t, "R-tool")
	fp := &fakeProvider{responses: []llmtypes.LLMResponse{
		toolResponse("tu-1", ToolQueryCityState, map[string]any{"scope": "overview"}),
		textResponse("城市状态如下。"),
	}}
	a, events := newTestAgent(t, room, &fakeRegistry{p: fp})

	if err := a.Run(context.Background(), "u1", "现在城市怎么样?"); err != nil {
		t.Fatalf("run: %v", err)
	}
	got := collectEvents(events)
	for _, kind := range []string{
		EventAgentStart, EventToolStart, EventToolResult, EventAgentEnd,
	} {
		if !hasEventKind(got, kind) {
			t.Fatalf("missing event %q in %v", kind, kinds(got))
		}
	}
	// tool_start / tool_result 的 data 带工具名与输入。
	for _, e := range got {
		if e.Kind != EventToolStart {
			continue
		}
		rec, ok := e.Data.(ToolCallRecord)
		if !ok {
			t.Fatalf("tool_start data type = %T", e.Data)
		}
		if rec.Tool != ToolQueryCityState || rec.Ts == 0 {
			t.Fatalf("unexpected tool record: %+v", rec)
		}
	}
	msgs := a.store.Get("R-tool/u1")
	if len(msgs) != 4 {
		t.Fatalf("store len = %d, want 4", len(msgs))
	}
	wantRoles := []string{"user", "assistant", "user", "assistant"}
	for i, want := range wantRoles {
		if msgs[i].Role != want {
			t.Fatalf("msgs[%d].Role = %s, want %s", i, msgs[i].Role, want)
		}
	}
	// 第 3 条(user)必须携带 tool_result 块。
	hasToolResult := false
	for _, b := range msgs[2].Content {
		if b.Type == "tool_result" && b.ToolUseID == "tu-1" {
			hasToolResult = true
		}
	}
	if !hasToolResult {
		t.Fatal("round-1 user message must carry tool_result(tu-1)")
	}
}

// TestRun_LLMError LLM 失败:返回 error + error 事件,且不落地悬空消息。
func TestRun_LLMError(t *testing.T) {
	room := newTestRoom(t, "R-err")
	fp := &fakeProvider{errs: []error{errors.New("provider down")}}
	a, events := newTestAgent(t, room, &fakeRegistry{p: fp})

	err := a.Run(context.Background(), "u1", "你好")
	if err == nil {
		t.Fatal("expected error")
	}
	got := collectEvents(events)
	if !hasEventKind(got, EventError) {
		t.Fatalf("missing error event: %v", kinds(got))
	}
	if !hasEventKind(got, EventAgentEnd) {
		t.Fatalf("missing agent_end event: %v", kinds(got))
	}
	// 只有开头的 user 消息,不得留下悬空 tool_result。
	if msgs := a.store.Get("R-err/u1"); len(msgs) != 1 {
		t.Fatalf("store len = %d, want 1", len(msgs))
	}
}

// TestRun_StreamTextEmitted 流式 text_delta 逐条下发(前端打字机)。
func TestRun_StreamTextEmitted(t *testing.T) {
	room := newTestRoom(t, "R-stream")
	fp := &fakeProvider{
		stream: []llmtypes.StreamEvent{
			{Type: "content_block_delta", DeltaType: "text_delta", Delta: "你好"},
			{Type: "content_block_delta", DeltaType: "text_delta", Delta: "!"},
		},
		responses: []llmtypes.LLMResponse{textResponse("你好!")},
	}
	a, events := newTestAgent(t, room, &fakeRegistry{p: fp})
	if err := a.Run(context.Background(), "u1", "你好"); err != nil {
		t.Fatalf("run: %v", err)
	}
	var deltas []string
	for _, e := range collectEvents(events) {
		if e.Kind != EventText {
			continue
		}
		data, _ := e.Data.(map[string]string)
		deltas = append(deltas, data["delta"])
	}
	if strings.Join(deltas, "") != "你好!" {
		t.Fatalf("deltas = %v, want 你好!", deltas)
	}
}

// TestRun_MaxStepsExhausted 步数用尽:循环收敛,不报错;补收尾 assistant
// 说明(会话以 assistant 结束,下一轮 user 不与悬空 tool_result 相邻)。
func TestRun_MaxStepsExhausted(t *testing.T) {
	room := newTestRoom(t, "R-steps")
	responses := make([]llmtypes.LLMResponse, 8)
	for i := range responses {
		responses[i] = toolResponse(fmt.Sprintf("tu-%d", i), "no_such_tool", map[string]any{})
	}
	fp := &fakeProvider{responses: responses}
	a, events := newTestAgent(t, room, &fakeRegistry{p: fp})
	if err := a.Run(context.Background(), "u1", "一直调工具"); err != nil {
		t.Fatalf("run: %v", err)
	}
	if fp.calls != 8 {
		t.Fatalf("llm calls = %d, want 8 (MaxSteps)", fp.calls)
	}
	msgs := a.store.Get("R-steps/u1")
	if len(msgs) != 18 {
		t.Fatalf("store len = %d, want 18 (1 user + 8×2 + 1 closing assistant)", len(msgs))
	}
	last := msgs[len(msgs)-1]
	if last.Role != "assistant" || !strings.Contains(last.Content[0].Text, "最大步数") {
		t.Fatalf("closing message = %s/%q, want assistant 收尾说明", last.Role, last.Content[0].Text)
	}
	if hasEventKind(collectEvents(events), EventError) {
		t.Fatal("exhausted steps must not emit error event")
	}
	// agent_end 文本 = 收尾说明(用户可见「发生了什么」)。
	for _, e := range collectEvents(events) {
		if e.Kind == EventAgentEnd {
			data, _ := e.Data.(map[string]any)
			if s, _ := data["text"].(string); !strings.Contains(s, "最大步数") {
				t.Fatalf("agent_end text = %q, want 收尾说明", s)
			}
		}
	}
}

// TestRun_EmptyText 空消息直接拒绝(不消耗 LLM)。
func TestRun_EmptyText(t *testing.T) {
	room := newTestRoom(t, "R-empty")
	fp := &fakeProvider{}
	a, _ := newTestAgent(t, room, &fakeRegistry{p: fp})
	if err := a.Run(context.Background(), "u1", "   "); err == nil {
		t.Fatal("expected error for empty text")
	}
	if fp.calls != 0 {
		t.Fatalf("llm calls = %d, want 0", fp.calls)
	}
}

// TestRun_ThinkingInjected thinking 开启时注入请求。
func TestRun_ThinkingInjected(t *testing.T) {
	room := newTestRoom(t, "R-think")
	fp := &fakeProvider{responses: []llmtypes.LLMResponse{textResponse("ok")}}
	a, _ := newTestAgent(t, room, &fakeRegistry{p: fp, enabled: true, budget: 1024})
	if err := a.Run(context.Background(), "u1", "你好"); err != nil {
		t.Fatalf("run: %v", err)
	}
	fp.mu.Lock()
	req := fp.reqs[len(fp.reqs)-1]
	fp.mu.Unlock()
	if req.Thinking == nil || req.Thinking.BudgetTokens != 1024 {
		t.Fatalf("thinking = %+v, want budget 1024", req.Thinking)
	}
	if req.AgentClassName != string(agentroot.AgentClassCitySystem) {
		t.Fatalf("AgentClassName = %q", req.AgentClassName)
	}
}

// TestRun_AcquireLLM 节流槽被调用,拿不到槽位即失败。
func TestRun_AcquireLLM(t *testing.T) {
	room := newTestRoom(t, "R-acquire")
	fp := &fakeProvider{responses: []llmtypes.LLMResponse{textResponse("ok")}}
	a, _ := newTestAgent(t, room, &fakeRegistry{p: fp})
	calls := 0
	a.SetLLMAcquire(func(ctx context.Context) (func(), bool) {
		calls++
		return func() {}, true
	})
	if err := a.Run(context.Background(), "u1", "你好"); err != nil {
		t.Fatalf("run: %v", err)
	}
	if calls != 1 {
		t.Fatalf("acquire calls = %d, want 1", calls)
	}
	// 槽位不可用 → 立即失败,不发起 LLM 调用。
	a.SetLLMAcquire(func(ctx context.Context) (func(), bool) { return nil, false })
	before := fp.calls
	if err := a.Run(context.Background(), "u1", "你好"); err == nil {
		t.Fatal("expected acquire failure")
	}
	if fp.calls != before {
		t.Fatalf("llm calls = %d, want %d (不得发起)", fp.calls, before)
	}
}

// TestReset 清空会话。
func TestReset(t *testing.T) {
	room := newTestRoom(t, "R-reset")
	fp := &fakeProvider{responses: []llmtypes.LLMResponse{textResponse("ok")}}
	a, _ := newTestAgent(t, room, &fakeRegistry{p: fp})
	if err := a.Run(context.Background(), "u1", "你好"); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := a.store.Get("R-reset/u1"); len(got) == 0 {
		t.Fatal("expected messages before reset")
	}
	a.Reset("u1")
	if got := a.store.Get("R-reset/u1"); len(got) != 0 {
		t.Fatalf("after reset len = %d, want 0", len(got))
	}
}

// TestLinePolicy 线路策略:缺省 / 固定 / 随机。
func TestLinePolicy(t *testing.T) {
	room := newTestRoom(t, "R-line")
	reg := &fakeRegistry{
		p: &fakeProvider{},
		list: []llmtypes.ModelInfo{
			{Model: "M1", ConcurrencyLines: 1},
			{Model: "M2", ConcurrencyLines: 9},
		},
	}
	a, _ := newTestAgent(t, room, reg)

	if got := a.resolveLineKey(); got != "TestModel" {
		t.Fatalf("default line = %q, want TestModel", got)
	}
	a.SetLinePolicy("fixed-key")
	if got := a.resolveLineKey(); got != "fixed-key" {
		t.Fatalf("fixed line = %q, want fixed-key", got)
	}
	a.SetLinePolicy(LinePolicyRandom)
	seen := map[string]int{}
	for i := 0; i < 200; i++ {
		seen[a.resolveLineKey()]++
	}
	if len(seen) != 2 || seen["M1"] == 0 || seen["M2"] == 0 {
		t.Fatalf("random line distribution = %v, want both M1 and M2", seen)
	}
	// 池空 → 回退 cfg.ModelKey。
	a.SetLinePolicy(LinePolicyRandom)
	reg.list = nil
	if got := a.resolveLineKey(); got != "TestModel" {
		t.Fatalf("empty pool line = %q, want TestModel", got)
	}
	if a.LinePolicy() != LinePolicyRandom {
		t.Fatalf("LinePolicy() = %q", a.LinePolicy())
	}
}

// TestLinePolicy_Concurrent 并发读写线路策略(-race 覆盖)。
func TestLinePolicy_Concurrent(t *testing.T) {
	room := newTestRoom(t, "R-line-race")
	a, _ := newTestAgent(t, room, &fakeRegistry{
		p:    &fakeProvider{},
		list: []llmtypes.ModelInfo{{Model: "M1"}},
	})
	done := make(chan struct{})
	go func() {
		defer func() { done <- struct{}{} }()
		for i := 0; i < 200; i++ {
			a.SetLinePolicy(LinePolicyRandom)
			a.SetLinePolicy("")
		}
	}()
	for i := 0; i < 200; i++ {
		_ = a.resolveLineKey()
		_ = a.LinePolicy()
	}
	<-done
}

// TestRun_NotBound registry 未绑定时立即失败(不干等超时)。
func TestRun_NotBound(t *testing.T) {
	room := newTestRoom(t, "R-noreg")
	a, _ := newTestAgent(t, room, nil)
	if err := a.Run(context.Background(), "u1", "你好"); err == nil {
		t.Fatal("expected error with nil registry")
	}
}

// TestRun_NilRoom nil room 直接拒绝。
func TestRun_NilRoom(t *testing.T) {
	a := NewAgent(nil, Config{ModelKey: "m"}, &fakeRegistry{p: &fakeProvider{}},
		newTestHub(t), NewConversationStore(40), nil)
	if err := a.Run(context.Background(), "u1", "你好"); err == nil {
		t.Fatal("expected error with nil room")
	}
}

// TestTruncateRunes 工具结果截断。
func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("abc", 0); got != "abc" {
		t.Fatalf("limit 0 = %q", got)
	}
	if got := truncateRunes("abc", 10); got != "abc" {
		t.Fatalf("short = %q", got)
	}
	got := truncateRunes("abcdef", 3)
	if got != "abc…(已截断)" {
		t.Fatalf("truncated = %q", got)
	}
	// 中文按 rune 截断。
	if got := truncateRunes("你好世界", 2); got != "你好…(已截断)" {
		t.Fatalf("cjk = %q", got)
	}
}

// TestNewTraceID 非空且互不相同。
func TestNewTraceID(t *testing.T) {
	a, b := NewTraceID(), NewTraceID()
	if a == "" || b == "" || a == b {
		t.Fatalf("trace ids = %q / %q", a, b)
	}
}

// TestRun_StepMarker 每个工具结果带 [step i/N] 进度前缀(促收敛)。
func TestRun_StepMarker(t *testing.T) {
	room := newTestRoom(t, "R-marker")
	fp := &fakeProvider{responses: []llmtypes.LLMResponse{
		toolResponse("tu-1", ToolQueryCityState, map[string]any{"scope": "overview"}),
		textResponse("城市状态如下。"),
	}}
	a, events := newTestAgent(t, room, &fakeRegistry{p: fp})
	if err := a.Run(context.Background(), "u1", "现在城市怎么样?"); err != nil {
		t.Fatalf("run: %v", err)
	}
	// 事件流与会话里的工具结果都带进度前缀(前端工具卡显示同源)。
	want := "[step 1/8] "
	found := false
	for _, e := range collectEvents(events) {
		if e.Kind != EventToolResult {
			continue
		}
		rec := e.Data.(ToolCallRecord)
		if !strings.HasPrefix(rec.Text, want) {
			t.Fatalf("tool result = %q, want prefix %q", rec.Text, want)
		}
		found = true
	}
	if !found {
		t.Fatal("no tool_result event")
	}
	msgs := a.store.Get("R-marker/u1")
	var trText string
	for _, b := range msgs[2].Content {
		if b.Type == "tool_result" {
			trText = b.Content[0].Text
		}
	}
	if !strings.HasPrefix(trText, want) {
		t.Fatalf("stored tool_result = %q, want prefix %q", trText, want)
	}
	// 出站时该轮消息必须满足 wire 纪律。
	assertWireValid(t, sanitizeForWire(msgs))
}

// TestRun_ConcurrentUsers 多用户并发 Run(-race 覆盖):身份随调用链显式
// 传递,会话按 roomID/userID 隔离,互不串号。
func TestRun_ConcurrentUsers(t *testing.T) {
	room := newTestRoom(t, "R-conc")
	fp := &fakeProvider{responses: []llmtypes.LLMResponse{
		textResponse("你好 A"),
		textResponse("你好 B"),
	}}
	a, _ := newTestAgent(t, room, &fakeRegistry{p: fp})
	done := make(chan error, 2)
	for _, uid := range []string{"u-a", "u-b"} {
		go func(uid string) {
			done <- a.Run(context.Background(), uid, "你好,我是 "+uid)
		}(uid)
	}
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatalf("run: %v", err)
		}
	}
	for _, uid := range []string{"u-a", "u-b"} {
		msgs := a.store.Get("R-conc/" + uid)
		if len(msgs) != 2 {
			t.Fatalf("store[%s] len = %d, want 2", uid, len(msgs))
		}
	}
}

// TestRun_FailedTurnThenRetry 失败轮残留相邻 user:下一轮出站仍须合法
// (sanitizeForWire 合并,不触发上游 400)。
func TestRun_FailedTurnThenRetry(t *testing.T) {
	room := newTestRoom(t, "R-retry")
	fp := &fakeProvider{errs: []error{errors.New("provider down")}}
	a, _ := newTestAgent(t, room, &fakeRegistry{p: fp})
	if err := a.Run(context.Background(), "u1", "第一句"); err == nil {
		t.Fatal("first run must fail")
	}
	// 换一个好 provider 重试(同会话)。
	a.registry = &fakeRegistry{p: &fakeProvider{
		responses: []llmtypes.LLMResponse{textResponse("第二次答复")},
	}}
	if err := a.Run(context.Background(), "u1", "第二句"); err != nil {
		t.Fatalf("retry: %v", err)
	}
	msgs := a.store.Get("R-retry/u1")
	// 存储层:失败轮 user + 重试轮 user + assistant(存储允许相邻 user)。
	if len(msgs) != 3 {
		t.Fatalf("store len = %d, want 3", len(msgs))
	}
	// 出站修整后必须严格交替。
	assertWireValid(t, sanitizeForWire(msgs))
}
