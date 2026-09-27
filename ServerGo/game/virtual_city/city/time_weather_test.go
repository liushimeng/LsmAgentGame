// Package city — time_weather_test.go: 批次 27 城市时间行接线测试。
//
//	SetTimeSource 注入 → runOne 的 user 消息首行「■ 城市时间:… · 季 · 天气」;
//	未注入(nil)→ 整行省略(零回归)。
package city

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"

	llmtypes "LsmAgentGame/llm/types"
)

// capturingDriverProvider 捕获最近一次 user 消息文本(验证 contextText 组装)。
type capturingDriverProvider struct {
	mu   sync.Mutex
	last string
}

func (f *capturingDriverProvider) Chat(ctx context.Context, key string, req llmtypes.LLMRequest) (llmtypes.LLMResponse, error) {
	f.mu.Lock()
	if len(req.Messages) > 0 && len(req.Messages[0].Content) > 0 {
		f.last = req.Messages[0].Content[0].Text
	}
	f.mu.Unlock()
	return llmtypes.LLMResponse{
		StopReason: "tool_use",
		Content: []llmtypes.ContentBlock{
			{Type: "tool_use", ID: "tw_1", Name: "set_intent",
				Input: map[string]any{"intent": "frugal"}},
		},
	}, nil
}

func (f *capturingDriverProvider) ChatStream(ctx context.Context, key string, req llmtypes.LLMRequest) (io.ReadCloser, error) {
	return nil, fmt.Errorf("fake: no stream")
}
func (f *capturingDriverProvider) ProviderType() string { return "fake-tw" }

func (f *capturingDriverProvider) lastText() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.last
}

// runOnceAndCapture 跑一次 RunMonth(PerMonth=1),返回捕获到的 user 消息文本。
func runOnceAndCapture(t *testing.T, d *ResidentDriver, fp *capturingDriverProvider) string {
	t.Helper()
	b := NewBackdrop(5, rand.New(rand.NewSource(31)), nil)
	done := make(chan struct{})
	go func() {
		d.RunMonth(b, 1, func(VoiceRecord) {})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RunMonth blocked")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s := fp.lastText(); s != "" {
			return s
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("driver never issued an LLM call")
	return ""
}

// TestDriverContextText_TimeLineRendered SetTimeSource 注入 → user 消息首行
// 「■ 城市时间:2006-01-02 15:04 · <季> · <天气>」,与 SeasonAt/WeatherAt
// 显示侧口径一致。
func TestDriverContextText_TimeLineRendered(t *testing.T) {
	fp := &capturingDriverProvider{}
	d := NewResidentDriver(DriverConfig{Enabled: true, Workers: 1, PerMonth: 1}, newDriverPool(fp, 2))
	// 固定时间源:2026-01-15 09:30(+0800,冬季)+ seed 42。
	fixedMs := time.Date(2026, 1, 15, 9, 30, 0, 0, cityEpochTZ).UnixMilli()
	d.SetTimeSource(func() (int64, int64) { return fixedMs, 42 })
	got := runOnceAndCapture(t, d, fp)
	if !strings.HasPrefix(got, "■ 城市时间:") {
		t.Fatalf("user text must start with city time line, got: %.80s", got)
	}
	firstLine := strings.SplitN(got, "\n", 2)[0]
	kind, _ := WeatherAt(42, fixedMs)
	want := fmt.Sprintf("■ 城市时间:%s · %s · %s",
		time.UnixMilli(fixedMs).In(cityEpochTZ).Format("2006-01-02 15:04"),
		seasonLabelZH[SeasonAt(fixedMs)], weatherLabelZH[kind])
	if firstLine != want {
		t.Fatalf("time line = %q, want %q", firstLine, want)
	}
}

// TestDriverContextText_TimeLineOmittedWithoutSource 未注入 timeSource(nil)
// → 整行省略,首行仍为「■ 本月状态」(零回归)。
func TestDriverContextText_TimeLineOmittedWithoutSource(t *testing.T) {
	fp := &capturingDriverProvider{}
	d := NewResidentDriver(DriverConfig{Enabled: true, Workers: 1, PerMonth: 1}, newDriverPool(fp, 2))
	// 不调用 SetTimeSource。
	got := runOnceAndCapture(t, d, fp)
	if strings.Contains(got, "城市时间") {
		t.Fatalf("nil timeSource must omit city time line, got: %.80s", got)
	}
	if !strings.HasPrefix(got, "■ 本月状态") {
		t.Fatalf("first line must stay 本月状态, got: %.80s", got)
	}
}
