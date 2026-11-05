// Package citychrome — dialogue_bus_test.go: 批次84「Agent 双向对话」单测。
//
// 覆盖:
//  1. "collect" 分支行为与批次81 完全一致(零回归);
//  2. "message" 分支:提问 → 采集意图 → 立即回「已受理」;
//  3. 非法载荷(缺 text / text 为空 / 载荷类型不对)⇒ 明确失败说明,不 panic;
//  4. 未知消息类型 ⇒ 明确失败说明;
//  5. 提问 → 类目的确定性映射(零 RNG);
//  6. **端到端上行**:对话分支受理 → mission 完成 → 结果摘要经总线抵达
//     City-System 的 handler(「Agent 来信」链路)。
package citychrome

import (
	"context"
	"strings"
	"testing"
	"time"

	agentroot "LsmAgentGame/agent"
	"LsmAgentGame/agent/citybus"
	"LsmAgentGame/config"
)

// newDialogueTestService 构造一个可跑通「受理 → mission 完成」的最小 Service。
func newDialogueTestService(t *testing.T, runner MissionRunner) *Service {
	t.Helper()
	store := NewFeedStore(12, 20)
	svc := &Service{
		cfg:               config.CityChromeConfig{Enabled: true},
		store:             store,
		sched:             NewScheduler(SchedulerConfig{QueueCapacity: 4}, store, runner),
		refreshNow:        time.Now,
		lastManualRefresh: map[string]time.Time{},
	}
	svc.bindMissionCompletion()
	return svc
}

// withDialogueService 临时替换对话分支使用的服务(测试结束后还原)。
func withDialogueService(t *testing.T, svc *Service) {
	t.Helper()
	old := dialogueServiceProvider
	dialogueServiceProvider = func() *Service { return svc }
	t.Cleanup(func() { dialogueServiceProvider = old })
}

// withBusHub 临时替换总线句柄(测试结束后还原)。
func withBusHub(t *testing.T, hub *citybus.Hub) {
	t.Helper()
	old := currentBusHub()
	SetBusHub(hub)
	t.Cleanup(func() { SetBusHub(old) })
}

// TestBusCapabilityMetadata 能力描述元信息(装配层 RegisterWith 登记用):
// 必须含两种消息类型且描述非空(§130 防「声明了却从不接线」)。
func TestBusCapabilityMetadata(t *testing.T) {
	joined := strings.Join(BusMessageTypes, ",")
	if !strings.Contains(joined, BusTypeCollect) || !strings.Contains(joined, BusTypeMessage) {
		t.Fatalf("BusMessageTypes = %v, want 含 collect 与 message", BusMessageTypes)
	}
	if strings.TrimSpace(BusCapabilityDesc) == "" {
		t.Fatal("BusCapabilityDesc 不得为空")
	}
}

// TestHandleBusMessage_CollectBranchUnchanged "collect" 分支:category 原样
// 传给既有 BusTrigger(批次81 行为,零回归)。
func TestHandleBusMessage_CollectBranchUnchanged(t *testing.T) {
	var gotCategory string
	withBusTrigger(t, func(_ context.Context, category string) (string, error) {
		gotCategory = category
		return "已触发 news 采集任务", nil
	})
	res, err := HandleBusMessage(context.Background(), citybus.Message{
		Type:    BusTypeCollect,
		Payload: map[string]any{"category": "news"},
	})
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if gotCategory != "news" {
		t.Fatalf("category = %q, want news", gotCategory)
	}
	if res.(string) != "已触发 news 采集任务" {
		t.Fatalf("res = %v", res)
	}
}

// TestHandleBusMessage_EmptyTypeFallsBackToCollect 空类型沿用既有采集分支
// (批次81 的调用方可能不带 Type;零回归)。
func TestHandleBusMessage_EmptyTypeFallsBackToCollect(t *testing.T) {
	var called bool
	withBusTrigger(t, func(_ context.Context, category string) (string, error) {
		called = true
		return "ok", nil
	})
	if _, err := HandleBusMessage(context.Background(), citybus.Message{Type: ""}); err != nil {
		t.Fatalf("err = %v", err)
	}
	if !called {
		t.Fatal("空类型必须走既有采集分支")
	}
}

// TestHandleBusMessage_MessageBranchAccepted "message" 分支:提问 → 受理 →
// 返回非空中文摘要,并把提问写进 mission.Goal(采集意图)。
func TestHandleBusMessage_MessageBranchAccepted(t *testing.T) {
	goalCh := make(chan string, 1)
	svc := newDialogueTestService(t, func(_ context.Context, m Mission) (*MissionPayload, error) {
		select {
		case goalCh <- m.Goal:
		default:
		}
		return &MissionPayload{Category: m.Category, Source: "测试源"}, nil
	})
	withDialogueService(t, svc)
	go svc.sched.Run()
	t.Cleanup(svc.sched.Stop)

	res, err := HandleBusMessage(context.Background(), citybus.Message{
		Type:    BusTypeMessage,
		RoomID:  "R1",
		Payload: map[string]any{"text": "今天 A 股行情怎么样"},
	})
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	text, ok := res.(string)
	if !ok || strings.TrimSpace(text) == "" {
		t.Fatalf("res = %v, want 非空中文摘要", res)
	}
	if !strings.Contains(text, "已受理") {
		t.Fatalf("res = %q, want 含「已受理」", text)
	}
	if !strings.Contains(text, "a_share") {
		t.Fatalf("res = %q, want 含采集类目 a_share", text)
	}
	select {
	case goal := <-goalCh:
		if !strings.Contains(goal, "今天 A 股行情怎么样") {
			t.Fatalf("mission goal = %q, want 含用户提问", goal)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("mission 未在时限内执行")
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
		{"text 为空串", map[string]any{"text": "   "}, "消息内容为空"},
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
	svc := newDialogueTestService(t, func(_ context.Context, m Mission) (*MissionPayload, error) {
		return &MissionPayload{Category: m.Category}, nil
	})
	withDialogueService(t, svc)

	res, err := HandleBusMessage(context.Background(), citybus.Message{
		Type:    BusTypeMessage,
		RoomID:  "R1",
		Payload: "帮我看看今天的港股",
	})
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if !strings.Contains(res.(string), "已受理") {
		t.Fatalf("res = %q, want 含「已受理」", res)
	}
	if !strings.Contains(res.(string), "hk") {
		t.Fatalf("res = %q, want 含采集类目 hk", res)
	}
}

// TestHandleBusMessage_UnknownType 未知类型:明确失败说明。
func TestHandleBusMessage_UnknownType(t *testing.T) {
	_, err := HandleBusMessage(context.Background(), citybus.Message{Type: "run_display"})
	if err == nil {
		t.Fatal("err = nil, want 明确失败说明")
	}
	if !strings.Contains(err.Error(), "run_display") {
		t.Fatalf("err = %q, want 含类型名", err.Error())
	}
}

// TestHandleBusMessage_ThrottledSecondRequest 同类目 60s 内第二次提问命中
// 既有节流 ⇒ 明确说明「未能受理」(不得绕过既有节流)。
func TestHandleBusMessage_ThrottledSecondRequest(t *testing.T) {
	svc := newDialogueTestService(t, func(_ context.Context, m Mission) (*MissionPayload, error) {
		return &MissionPayload{Category: m.Category}, nil
	})
	withDialogueService(t, svc)

	msg := citybus.Message{Type: BusTypeMessage, RoomID: "R1",
		Payload: map[string]any{"text": "港股今天怎么样"}}
	if _, err := HandleBusMessage(context.Background(), msg); err != nil {
		t.Fatalf("first err = %v", err)
	}
	res, err := HandleBusMessage(context.Background(), msg)
	if err != nil {
		t.Fatalf("second err = %v, want nil(限流是状态而非故障)", err)
	}
	if !strings.Contains(res.(string), "未能受理") {
		t.Fatalf("res = %q, want 含「未能受理」", res)
	}
	if svc.sched.QueueLen() != 1 {
		t.Fatalf("queue len = %d, want 1(限流时不得再入队)", svc.sched.QueueLen())
	}
}

// TestInferCategory 提问 → 类目的确定性映射(零 RNG)。
func TestInferCategory(t *testing.T) {
	cases := []struct {
		text string
		want string
	}{
		{"今天 A 股行情怎么样", CategoryAShare},
		{"恒生指数涨了吗", CategoryHK},
		{"美股道琼斯今天如何", CategoryUS},
		{"黄金价格多少", CategoryMetal},
		{"原油期货怎么样", CategoryFutures},
		{"比特币价格", CategoryCrypto},
		{"最新 CPI 是多少", CategoryMacro},
		{"有什么财经新闻", CategoryNews},
		{"随便聊聊天气", CategoryNews},
	}
	for _, tc := range cases {
		if got := inferCategory(tc.text); got != tc.want {
			t.Fatalf("inferCategory(%q) = %q, want %q", tc.text, got, tc.want)
		}
	}
}

// TestBuildMissionWithGoal 附加目标:非空时写进 Goal;空时与 BuildMission
// 逐位一致(零回归)。
func TestBuildMissionWithGoal(t *testing.T) {
	base := BuildMission(CategoryAShare)
	same := BuildMissionWithGoal(CategoryAShare, "")
	if base.Goal != same.Goal || base.TargetURL != same.TargetURL || len(base.Sites) != len(same.Sites) {
		t.Fatalf("空附加目标时 mission 必须与 BuildMission 一致:\n%+v\n%+v", base, same)
	}
	withGoal := BuildMissionWithGoal(CategoryAShare, "帮我看看沪深300")
	if !strings.Contains(withGoal.Goal, "帮我看看沪深300") {
		t.Fatalf("goal = %q, want 含附加目标", withGoal.Goal)
	}
	if withGoal.TargetURL != base.TargetURL {
		t.Fatalf("target url 被改动: %q", withGoal.TargetURL)
	}
}

// TestMissionSummaryText 摘要只复述 payload 里的真实字段;失败只报原因。
func TestMissionSummaryText(t *testing.T) {
	payload := &MissionPayload{
		Category: CategoryAShare,
		Source:   "腾讯行情·A股指数",
		Quotes: []RealtimeQuote{
			{Symbol: "sh000001", Name: "上证指数", Price: 3120.5, ChangePct: 1.25},
		},
		News: []RealtimeNews{{Title: "两市成交额创新高"}},
	}
	text := missionSummaryText(CategoryAShare, "今天 A 股怎么样", payload, nil)
	if !strings.Contains(text, "腾讯行情·A股指数") {
		t.Fatalf("summary = %q, want 含来源", text)
	}
	if !strings.Contains(text, "上证指数") || !strings.Contains(text, "1.25") {
		t.Fatalf("summary = %q, want 含报价与涨跌幅", text)
	}
	failed := missionSummaryText(CategoryAShare, "今天 A 股怎么样", nil, errTestBoom)
	if !strings.Contains(failed, "采集失败") {
		t.Fatalf("summary = %q, want 含「采集失败」", failed)
	}
}

// errTestBoom 是单测用的固定错误。
var errTestBoom = errBoom{}

type errBoom struct{}

func (errBoom) Error() string { return "底座不可达" }

// TestDialogueBranch_UpstreamReachesSystemHandler 端到端:对话分支受理 →
// mission 完成 → 结果摘要经总线抵达 City-System 的房间级 handler。
func TestDialogueBranch_UpstreamReachesSystemHandler(t *testing.T) {
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
	withBusHub(t, hub)

	svc := newDialogueTestService(t, func(_ context.Context, m Mission) (*MissionPayload, error) {
		return &MissionPayload{
			Category: m.Category,
			Source:   "腾讯行情·A股指数",
			Quotes: []RealtimeQuote{
				{Symbol: "sh000001", Name: "上证指数", Price: 3120.5, ChangePct: 1.25},
			},
		}, nil
	})
	withDialogueService(t, svc)
	go svc.sched.Run()
	t.Cleanup(svc.sched.Stop)

	res, err := HandleBusMessage(context.Background(), citybus.Message{
		From:    string(agentroot.AgentClassCitySystem),
		To:      string(agentroot.AgentClassCityChrome),
		Type:    BusTypeMessage,
		RoomID:  "R1",
		Payload: map[string]any{"text": "今天 A 股行情怎么样"},
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(res.(string), "已受理") {
		t.Fatalf("res = %q, want 含「已受理」", res)
	}
	deadline := time.After(15 * time.Second)
	for {
		select {
		case text := <-received:
			// 忽略其它用例遗留的旧消息(总线是全局单例,单测间可能串扰)。
			if !strings.Contains(text, "上证指数") {
				continue
			}
			if !strings.Contains(text, "采集结果") {
				t.Fatalf("上行消息 = %q, want 含「采集结果」", text)
			}
			return
		case <-deadline:
			t.Fatal("上行消息未在时限内抵达 City-System handler")
		}
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
