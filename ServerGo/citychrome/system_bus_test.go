// Package citychrome — system_bus_test.go: 批次81 总线薄委托入口单测。
//
// 覆盖三件事:
//  1. BusTrigger == nil 时返回可读说明(不报故障);
//  2. BusTrigger 返回 error 时 SystemBusPayload 原样透传;
//  3. 正常路径返回摘要,且 category 原样传下去。
//
// 另覆盖 Service.SystemBusTrigger 的运营态分支(未启动 / 未启用 /
// 类目非法 / 正常入队)。
package citychrome

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"LsmAgentGame/config"
)

// withBusTrigger 临时替换包级 BusTrigger 并在测试结束后还原。
func withBusTrigger(t *testing.T, fn func(context.Context, string) (string, error)) {
	t.Helper()
	old := BusTrigger
	BusTrigger = fn
	t.Cleanup(func() { BusTrigger = old })
}

func TestSystemBusPayload_NilTrigger(t *testing.T) {
	withBusTrigger(t, nil)
	msg, err := SystemBusPayload(context.Background(), "")
	if err != nil {
		t.Fatalf("err = %v, want nil(未注入是状态而非故障)", err)
	}
	if !strings.Contains(msg, "BusTrigger 未注入") {
		t.Fatalf("msg = %q, want 含「BusTrigger 未注入」", msg)
	}
}

func TestSystemBusPayload_ErrorPassthrough(t *testing.T) {
	want := errors.New("boom")
	withBusTrigger(t, func(context.Context, string) (string, error) { return "", want })
	msg, err := SystemBusPayload(context.Background(), "a_share")
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
	if msg != "" {
		t.Fatalf("msg = %q, want 空串", msg)
	}
}

func TestSystemBusPayload_NormalSummary(t *testing.T) {
	var gotCategory string
	withBusTrigger(t, func(_ context.Context, category string) (string, error) {
		gotCategory = category
		return "已触发 news 采集任务", nil
	})
	msg, err := SystemBusPayload(context.Background(), "news")
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if gotCategory != "news" {
		t.Fatalf("category = %q, want news", gotCategory)
	}
	if msg != "已触发 news 采集任务" {
		t.Fatalf("msg = %q", msg)
	}
}

// newBusTestService 构造一个最小可入队的启用态 Service(仅 SystemBusTrigger 所需字段)。
func newBusTestService(enabled bool) *Service {
	cfg := config.CityChromeConfig{Enabled: enabled}
	if !enabled {
		return &Service{cfg: cfg}
	}
	return &Service{
		cfg: cfg,
		sched: NewScheduler(SchedulerConfig{QueueCapacity: 4},
			NewFeedStore(12, 20),
			func(context.Context, Mission) (*MissionPayload, error) { return nil, nil }),
		refreshNow:        time.Now,
		lastManualRefresh: map[string]time.Time{},
	}
}

func TestServiceSystemBusTrigger_NilService(t *testing.T) {
	var svc *Service
	msg, err := svc.SystemBusTrigger(context.Background(), "")
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if !strings.Contains(msg, "未启动") {
		t.Fatalf("msg = %q, want 含「未启动」", msg)
	}
}

func TestServiceSystemBusTrigger_Disabled(t *testing.T) {
	svc := newBusTestService(false)
	msg, err := svc.SystemBusTrigger(context.Background(), "news")
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if !strings.Contains(msg, "未启用") {
		t.Fatalf("msg = %q, want 含「未启用」", msg)
	}
}

func TestServiceSystemBusTrigger_UnknownCategory(t *testing.T) {
	svc := newBusTestService(true)
	msg, err := svc.SystemBusTrigger(context.Background(), "not_a_category")
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if !strings.Contains(msg, "未知采集类目") {
		t.Fatalf("msg = %q, want 含「未知采集类目」", msg)
	}
	if svc.sched.QueueLen() != 0 {
		t.Fatalf("queue len = %d, want 0(非法类目不得入队)", svc.sched.QueueLen())
	}
}

func TestServiceSystemBusTrigger_Accepted(t *testing.T) {
	svc := newBusTestService(true)
	msg, err := svc.SystemBusTrigger(context.Background(), "news")
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if !strings.Contains(msg, "已触发") || !strings.Contains(msg, "news") {
		t.Fatalf("msg = %q, want 含「已触发」与「news」", msg)
	}
	if svc.sched.QueueLen() != 1 {
		t.Fatalf("queue len = %d, want 1", svc.sched.QueueLen())
	}
}

func TestServiceSystemBusTrigger_RateLimited(t *testing.T) {
	svc := newBusTestService(true)
	// 先入队一次,立即再触发同类目 ⇒ 命中 60s 限流(35054),仍返回可读文本。
	if _, err := svc.SystemBusTrigger(context.Background(), "a_share"); err != nil {
		t.Fatalf("first trigger err = %v", err)
	}
	msg, err := svc.SystemBusTrigger(context.Background(), "a_share")
	if err != nil {
		t.Fatalf("second trigger err = %v, want nil(限流是状态)", err)
	}
	if !strings.Contains(msg, "未能触发") {
		t.Fatalf("msg = %q, want 含「未能触发」", msg)
	}
}
