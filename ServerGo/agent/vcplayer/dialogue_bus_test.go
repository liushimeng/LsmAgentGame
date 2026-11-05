// Package vcplayer — dialogue_bus_test.go: 批次84「Agent 双向对话」单测。
//
// 覆盖:
//  1. "message" 分支:居民口吻回应(非空中文),且不编造数据;
//  2. 非法载荷(缺 text / text 为空 / 载荷类型不对)⇒ 明确失败说明,不 panic;
//  3. 非 "message" 类型 ⇒ 批次81 既有居民摘要分支(零回归);
//  4. 注入回答器时优先走既有 LLM 通道;
//  5. **上行消息**经总线抵达 City-System 的 handler;
//  6. AnswerQuestion 复用既有 Provider + 既有令牌桶节流。
//
// 注:本文件操作包级全局注入点(CityRoomSource / CityResidentAnswerer /
// 总线句柄),用例**不得** t.Parallel()(全局替换 + t.Cleanup 还原安全)。
package vcplayer

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	agentroot "LsmAgentGame/agent"
	"LsmAgentGame/agent/citybus"
	"LsmAgentGame/agent/vctypes"
	llmtypes "LsmAgentGame/llm/types"
)

// dialogueFakeProvider 记录请求并返回一段居民口吻文本。
type dialogueFakeProvider struct {
	calls int
	text  string
	err   error
}

func (f *dialogueFakeProvider) Chat(ctx context.Context, key string, req llmtypes.LLMRequest) (llmtypes.LLMResponse, error) {
	f.calls++
	if f.err != nil {
		return llmtypes.LLMResponse{}, f.err
	}
	text := f.text
	if text == "" {
		text = "我也想知道呢,日子不太好过。"
	}
	return llmtypes.LLMResponse{
		StopReason: "end_turn",
		Content:    []llmtypes.ContentBlock{{Type: "text", Text: text}},
	}, nil
}

func (f *dialogueFakeProvider) ChatStream(ctx context.Context, key string, req llmtypes.LLMRequest) (io.ReadCloser, error) {
	return nil, errors.New("fake: no stream")
}
func (f *dialogueFakeProvider) ProviderType() string { return "fake-dialogue" }

// dialogueFakeRegistry 是只实现最小 LLMRegistry 的 fake。
type dialogueFakeRegistry struct{ p llmtypes.LLMProvider }

func (r dialogueFakeRegistry) Get(modelKey string) (llmtypes.LLMProvider, string, error) {
	return r.p, "k", nil
}
func (r dialogueFakeRegistry) GetThinkingEnabled(modelKey string) (bool, int) { return false, 0 }

// withResidentAnswerer 临时替换居民回答器(测试结束后还原)。
func withResidentAnswerer(t *testing.T, src ResidentAnswerSource) {
	t.Helper()
	old := CityResidentAnswerer
	CityResidentAnswerer = src
	t.Cleanup(func() { CityResidentAnswerer = old })
}

// TestBusCapabilityMetadata 能力描述元信息(装配层 RegisterWith 登记用):
// 必须含 "message" 且描述非空(§130 防「声明了却从不接线」)。
func TestBusCapabilityMetadata(t *testing.T) {
	if len(BusMessageTypes) == 0 || BusMessageTypes[0] != BusTypeMessage {
		t.Fatalf("BusMessageTypes = %v, want 首项为 message", BusMessageTypes)
	}
	if strings.TrimSpace(BusCapabilityDesc) == "" {
		t.Fatal("BusCapabilityDesc 不得为空")
	}
}

// TestHandleBusMessage_MessageBranchFallback "message" 分支:无可用 LLM 通道
// ⇒ 退化为基于房间快照的确定性回应(非空中文、含快照事实、不编造)。
func TestHandleBusMessage_MessageBranchFallback(t *testing.T) {
	withCityRoomSource(t, &fakePlainSource{briefs: []ResidentBrief{
		{Seat: 0, Nickname: "张三", ModelKey: "DeepSeek", Alive: true, Cash: 12345, District: "d1"},
	}})
	withResidentAnswerer(t, nil)

	res, err := HandleBusMessage(context.Background(), citybus.Message{
		Type:    BusTypeMessage,
		RoomID:  "R1",
		Payload: map[string]any{"text": "最近物价涨了吗"},
	})
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	text, ok := res.(string)
	if !ok || strings.TrimSpace(text) == "" {
		t.Fatalf("res = %v, want 非空中文回应", res)
	}
	if !strings.Contains(text, "张三") {
		t.Fatalf("res = %q, want 含快照里的居民昵称", text)
	}
	if !strings.Contains(text, "12345") {
		t.Fatalf("res = %q, want 含快照里的现金事实", text)
	}
	if !strings.Contains(text, "物价涨了吗") {
		t.Fatalf("res = %q, want 复述用户提问", text)
	}
}

// TestHandleBusMessage_NoSnapshotSource 房间快照未注入 ⇒ 明确可读说明
// (状态而非故障,不返回 error)。
func TestHandleBusMessage_NoSnapshotSource(t *testing.T) {
	withCityRoomSource(t, nil)
	withResidentAnswerer(t, nil)

	res, err := HandleBusMessage(context.Background(), citybus.Message{
		Type:    BusTypeMessage,
		RoomID:  "R1",
		Payload: map[string]any{"text": "最近怎么样"},
	})
	if err != nil {
		t.Fatalf("err = %v, want nil(缺注入是状态而非故障)", err)
	}
	if !strings.Contains(res.(string), "居民视角暂不可用") {
		t.Fatalf("res = %q, want 含「居民视角暂不可用」", res)
	}
}

// TestHandleBusMessage_InvalidPayload 非法载荷:明确中文失败说明,不 panic。
func TestHandleBusMessage_InvalidPayload(t *testing.T) {
	cases := []struct {
		name    string
		payload any
		want    string
	}{
		{"缺 text", map[string]any{"foo": "bar"}, "缺少 text 字段"},
		{"text 为空串", map[string]any{"text": "  "}, "消息内容为空"},
		{"text 类型错", map[string]any{"text": 42}, "text 字段类型错误"},
		{"载荷为 nil", nil, "消息载荷格式错误"},
		{"载荷为数字", 7, "消息载荷格式错误"},
		{"载荷为数组", []any{"a"}, "消息载荷格式错误"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := HandleBusMessage(context.Background(), citybus.Message{
				Type:    BusTypeMessage,
				RoomID:  "R1",
				Payload: tc.payload,
			})
			if err == nil {
				t.Fatalf("err = nil, want 明确失败说明(res=%v)", res)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %q, want 含 %q", err.Error(), tc.want)
			}
		})
	}
}

// TestHandleBusMessage_LegacyStringPayload 批次81 既有契约:send_agent_message
// 传的是裸字符串 ⇒ 按消息正文解析(不得回归既有工具路径)。
func TestHandleBusMessage_LegacyStringPayload(t *testing.T) {
	withCityRoomSource(t, &fakePlainSource{briefs: []ResidentBrief{
		{Seat: 0, Nickname: "张三", Alive: true, Cash: 42, District: "d1"},
	}})
	withResidentAnswerer(t, nil)

	res, err := HandleBusMessage(context.Background(), citybus.Message{
		Type:    BusTypeMessage,
		RoomID:  "R1",
		Payload: "最近生意怎么样",
	})
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	text := res.(string)
	if !strings.Contains(text, "张三") {
		t.Fatalf("res = %q, want 含快照事实", text)
	}
	if !strings.Contains(text, "最近生意怎么样") {
		t.Fatalf("res = %q, want 复述消息正文", text)
	}
}

// TestHandleBusMessage_LegacyTypeUnchanged 非 "message" 类型 ⇒ 批次81 既有
// 居民摘要分支(零回归)。
func TestHandleBusMessage_LegacyTypeUnchanged(t *testing.T) {
	withCityRoomSource(t, &fakePlainSource{briefs: []ResidentBrief{
		{Seat: 0, Nickname: "张三", ModelKey: "DeepSeek", Alive: true, Cash: 12345, District: "d1"},
	}})
	res, err := HandleBusMessage(context.Background(), citybus.Message{Type: "", RoomID: "R1"})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	text := res.(string)
	if !strings.Contains(text, "#1 张三") {
		t.Fatalf("res = %q, want 含既有格式「#1 张三」", text)
	}
}

// fakeAnswerer 是注入的居民回答器(记录调用参数)。
type fakeAnswerer struct {
	reply string
	err   error
	room  string
	quest string
}

func (f *fakeAnswerer) ResidentAnswer(ctx context.Context, roomID, question string) (string, error) {
	f.room = roomID
	f.quest = question
	return f.reply, f.err
}

// TestHandleBusMessage_UsesInjectedAnswerer 注入回答器 ⇒ 优先走既有 LLM 通道。
func TestHandleBusMessage_UsesInjectedAnswerer(t *testing.T) {
	withCityRoomSource(t, &fakePlainSource{briefs: []ResidentBrief{
		{Seat: 0, Nickname: "张三", Alive: true, Cash: 1, District: "d1"},
	}})
	ans := &fakeAnswerer{reply: "日子还过得去,就是物价有点恼人。"}
	withResidentAnswerer(t, ans)

	res, err := HandleBusMessage(context.Background(), citybus.Message{
		Type:    BusTypeMessage,
		RoomID:  "R7",
		Payload: map[string]any{"text": "最近怎么样"},
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if res.(string) != ans.reply {
		t.Fatalf("res = %q, want %q", res, ans.reply)
	}
	if ans.room != "R7" || ans.quest != "最近怎么样" {
		t.Fatalf("answerer got room=%q question=%q", ans.room, ans.quest)
	}
}

// TestHandleBusMessage_AnswererErrorFallsBack 回答器出错 ⇒ 退化为确定性说明。
func TestHandleBusMessage_AnswererErrorFallsBack(t *testing.T) {
	withCityRoomSource(t, &fakePlainSource{briefs: []ResidentBrief{
		{Seat: 0, Nickname: "张三", Alive: true, Cash: 7, District: "d1"},
	}})
	withResidentAnswerer(t, &fakeAnswerer{err: errors.New("llm boom")})

	res, err := HandleBusMessage(context.Background(), citybus.Message{
		Type:    BusTypeMessage,
		RoomID:  "R1",
		Payload: map[string]any{"text": "最近怎么样"},
	})
	if err != nil {
		t.Fatalf("err = %v, want nil(回退不是故障)", err)
	}
	if !strings.Contains(res.(string), "张三") {
		t.Fatalf("res = %q, want 含确定性说明", res)
	}
}

// TestNotifySystem_ReachesSystemHandler 上行消息经总线抵达 City-System 的
// 房间级 handler(「Agent 来信」链路)。
func TestNotifySystem_ReachesSystemHandler(t *testing.T) {
	hub := citybus.New()
	defer hub.Close()
	received := make(chan string, 4)
	if err := hub.RegisterRoom("R1", string(agentroot.AgentClassCitySystem),
		func(_ context.Context, msg citybus.Message) (any, error) {
			payload, _ := msg.Payload.(map[string]any)
			text, _ := payload["text"].(string)
			received <- text
			return "收到", nil
		}); err != nil {
		t.Fatalf("register system handler: %v", err)
	}
	if err := NotifySystem(context.Background(), hub, "R1", "居民来信:今天菜价又涨了"); err != nil {
		t.Fatalf("NotifySystem err = %v", err)
	}
	select {
	case text := <-received:
		if text != "居民来信:今天菜价又涨了" {
			t.Fatalf("received = %q", text)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("上行消息未抵达 City-System handler")
	}
}

// TestNotifySystem_NilHub 未注入总线时返回明确错误(不 panic)。
func TestNotifySystem_NilHub(t *testing.T) {
	if err := NotifySystem(context.Background(), nil, "R1", "hello"); err == nil {
		t.Fatal("err = nil, want 明确错误")
	}
	if err := NotifySystem(context.Background(), citybus.New(), "R1", "  "); err == nil {
		t.Fatal("空消息必须返回错误")
	}
}

// TestAnswerQuestion_UsesExistingChannel AnswerQuestion 复用既有 Provider
// 与既有令牌桶节流(桶容量 2 ⇒ 第 3 次被节流)。
func TestAnswerQuestion_UsesExistingChannel(t *testing.T) {
	fp := &dialogueFakeProvider{text: "我这手头紧着呢,日子难过。"}
	a := NewAgent("R1", "u1", "model-x", "模型X", 0, 3, 5*time.Second)
	a.BindRegistry(dialogueFakeRegistry{p: fp})
	card := vctypes.CardBrief{ID: "c1", Name: "张三", Title: "摊贩", HomeDistrictCN: "东城区"}

	reply, err := a.AnswerQuestion(context.Background(), card, "最近物价怎么样")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if reply != fp.text {
		t.Fatalf("reply = %q, want %q", reply, fp.text)
	}
	if fp.calls != 1 {
		t.Fatalf("provider calls = %d, want 1", fp.calls)
	}

	// 令牌桶容量 2:再调 1 次成功,第 3 次被节流(既有节流,零新增策略)。
	if _, err := a.AnswerQuestion(context.Background(), card, "再问一次"); err != nil {
		t.Fatalf("second err = %v", err)
	}
	if _, err := a.AnswerQuestion(context.Background(), card, "第三次"); err == nil {
		t.Fatal("第三次必须被既有令牌桶节流")
	}
}

// TestAnswerQuestion_NoChannel 未绑定 LLM 通道时返回明确错误。
func TestAnswerQuestion_NoChannel(t *testing.T) {
	a := NewAgent("R1", "u1", "model-x", "模型X", 0, 3, 5*time.Second)
	if _, err := a.AnswerQuestion(context.Background(), vctypes.CardBrief{}, "在吗"); err == nil {
		t.Fatal("err = nil, want 明确错误")
	}
}
