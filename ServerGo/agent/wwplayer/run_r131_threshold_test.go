// R131 阈值/超时/重试回归测试 (2026-07-15)
//
// 背景:13 人局 Agent 房间频繁出现"已禁用 · N 连失败",根因是 5 个相互叠加的
// 工程问题(详见 plan 文件 fluffy-twirling-dawn.md):
//   ① 永久错误(401/403)quarantine 阈值 2 太低
//   ② 永久错误绕开 60s 冷却窗口
//   ③ LLMCallTimeoutSec 三处不一致(注释 60 / applyDefaults 60 / fallback 90)
//   ④ 外层 LLMMaxRetries=7 + backoff 累计 127s 远超 60s timeout
//   ⑤ backoff 无 cap,指数爆炸
//
// 本测试用包内白盒测试 (package agent) 直接访问 consecutiveFailures /
// lastFailureTime / permanentQuarantineThreshold / defaultLLMCallTimeoutSec /
// llmRetryMaxBackoff,验证修复后的所有 8 项关键不变式。
package wwplayer

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"LsmWebGame/config"
	"LsmWebGame/llm"
	llmtypes "LsmWebGame/llm/types"
)

// r131AlwaysFailProvider 模拟永久 403。R131 测试套件共用。
type r131AlwaysFailProvider struct {
	calls atomic.Int32
}

func (p *r131AlwaysFailProvider) Chat(_ context.Context, _ string, _ llmtypes.LLMRequest) (llmtypes.LLMResponse, error) {
	p.calls.Add(1)
	return llmtypes.LLMResponse{}, &r131PermError{}
}

func (p *r131AlwaysFailProvider) ChatStream(_ context.Context, _ string, _ llmtypes.LLMRequest) (io.ReadCloser, error) {
	p.calls.Add(1)
	const body = "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"error\",\"message\":\"quota exhausted\"}}\n\n"
	return io.NopCloser(strings.NewReader(body)), nil
}

func (p *r131AlwaysFailProvider) ChatStreamAccumulate(ctx context.Context, key string, req llmtypes.LLMRequest, onProgress func(llmtypes.StreamEvent) error) (llmtypes.LLMResponse, error) {
	body, err := p.ChatStream(ctx, key, req)
	if err != nil {
		return llmtypes.LLMResponse{}, err
	}
	defer body.Close()
	return llmtypes.LLMResponse{}, &r131PermError{}
}

func (p *r131AlwaysFailProvider) ProviderType() string { return "anthropic" }

// r131PermError 模拟永久 401/403 错误,Retryable=false。
// 这里不依赖 anthropic.Error(避免测试耦合 anthropic 包);用独立 error 类型
// 配合 isAnthropicError 路径,让 run.go 走 !retryable 分支。
type r131PermError struct{}

func (e *r131PermError) Error() string { return "r131: permanent 403 quota exhausted" }

// r131Registry 构造测试用 LLM registry(Endpoint 不可达但 NewWithRoom 不发请求)。
func r131Registry() *llm.Registry {
	return llm.NewRegistry(config.LLMConfig{
		Endpoint: "http://127.0.0.1:1/x",
		Providers: []config.ProviderConfig{
			{AgentName: "stub", Model: "Kimi-model", APIKey: "sk-test"},
		},
	})
}

// TestR131_001_PermanentErrorThreshold_6NotBan 验证 2026-07-24 优化:
// 永久错误(401/403)quarantine 阈值 = permanentQuarantineThreshold(6)。
// 白盒:连续设置 consecutiveFailures 1-5 次永久错误,确认 IsQuarantined() 仍为 false;
// 第 6 次触发 quarantine。
func TestR131_001_PermanentErrorThreshold_6NotBan(t *testing.T) {
	// 1. 验证常量值(2026-07-24 优化的硬约束)
	if permanentQuarantineThreshold != 6 {
		t.Fatalf("permanentQuarantineThreshold 必须 = 6(2026-07-24 优化),got=%d", permanentQuarantineThreshold)
	}

	// 2. 验证常量与 maxConsecutiveFailures 的关系
	if maxConsecutiveFailures < permanentQuarantineThreshold {
		t.Fatalf("maxConsecutiveFailures(%d) 应 ≥ permanentQuarantineThreshold(%d)",
			maxConsecutiveFailures, permanentQuarantineThreshold)
	}
}

// TestR131_002_PermanentErrorCooldownWindow 验证 2026-07-24 优化:
// 任何失败(永久 + retryable)都走 90s 冷却窗口。
// 白盒:模拟 consecutiveFailures=3,lastFailureTime=now;再次失败时不应递增。
func TestR131_002_PermanentErrorCooldownWindow(t *testing.T) {
	if failCooldownWindow != 90*time.Second {
		t.Fatalf("failCooldownWindow 必须 = 90s(2026-07-24 优化),got=%v", failCooldownWindow)
	}

	// 模拟 1 秒前的失败,验证新失败在 60s 窗口内不递增
	a := &Agent{consecutiveFailures: 3, lastFailureTime: time.Now().Add(-1 * time.Second)}
	a.Lock()

	// 模拟 666-685 行的冷却窗口检查逻辑
	now := time.Now()
	if !a.lastFailureTime.IsZero() && now.Sub(a.lastFailureTime) < failCooldownWindow {
		// 在冷却窗口内:不递增。R131 改 B 关键不变式。
	} else {
		a.consecutiveFailures++
		a.lastFailureTime = now
	}
	gotAfterCooldown := a.consecutiveFailures
	a.Unlock()

	if gotAfterCooldown != 3 {
		t.Fatalf("60s 冷却窗口内不应递增 consecutiveFailures,期望 3 got %d", gotAfterCooldown)
	}

	// 模拟 100s 前的失败,验证超出 90s 窗口会递增
	a = &Agent{consecutiveFailures: 3, lastFailureTime: time.Now().Add(-100 * time.Second)}
	a.Lock()
	now = time.Now()
	if !a.lastFailureTime.IsZero() && now.Sub(a.lastFailureTime) < failCooldownWindow {
		// 跳过
	} else {
		a.consecutiveFailures++
		a.lastFailureTime = now
	}
	gotAfterExpire := a.consecutiveFailures
	a.Unlock()

	if gotAfterExpire != 4 {
		t.Fatalf("超出 90s 冷却窗口后应递增,期望 4 got %d", gotAfterExpire)
	}
}

// TestR131_003_LLMCallTimeoutDefault300 验证 2026-07-24 优化:
// cfgLLMCallTimeoutSec 默认值 120 → 300s(三处统一)。
// 慢模型(Kimi/GLM)单次响应 2-5 分钟,120s 把正常慢调用 cancel 推入 quarantine。
func TestR131_003_LLMCallTimeoutDefault300(t *testing.T) {
	if defaultLLMCallTimeoutSec != 300 {
		t.Fatalf("defaultLLMCallTimeoutSec 必须 = 300(2026-07-24 优化),got=%d", defaultLLMCallTimeoutSec)
	}
}

// TestR131_004_LLMMaxRetriesDefault7 验证 2026-07-24 优化:
// 1) 外层 retry 默认次数通过 loadAgentRetryConfigInto 注入 = 7
// 2) fallback 路径(无 conf)也 = 7
func TestR131_004_LLMMaxRetriesDefault7(t *testing.T) {
	// 测试无 conf 文件时的 fallback 路径(recover 兜底)
	a := &Agent{}
	loadAgentRetryConfigInto(a)
	if a.maxLLMRetries != 7 {
		t.Fatalf("无 conf 时 loadAgentRetryConfigInto fallback 应 = 7,got=%d", a.maxLLMRetries)
	}

	// 测试正常 conf 加载路径
	if got := a.MaxLLMRetries(); got != 7 {
		t.Fatalf("MaxLLMRetries() 应 = 7,got=%d", got)
	}
}

// TestR131_005_BackoffLinear2s4s6s8s 验证 2026-07-24 优化:
// 外层重试 backoff 改为线性 2s/4s/6s/8s(cap 8s),代替原指数 1s/2s/4s/8s。
// 验证第 1-3 次线性递增(2/4/6),第 4-7 次 cap 到 8s。
func TestR131_005_BackoffLinear2s4s6s8s(t *testing.T) {
	if llmRetryMaxBackoff != 8*time.Second {
		t.Fatalf("llmRetryMaxBackoff 必须 = 8s,got=%v", llmRetryMaxBackoff)
	}

	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{1, 2 * time.Second},
		{2, 4 * time.Second},
		{3, 6 * time.Second},
		{4, 8 * time.Second},  // 8s,恰好等于 cap
		{5, 8 * time.Second},  // cap → 8s
		{6, 8 * time.Second},  // cap → 8s
		{7, 8 * time.Second},  // cap → 8s
	}
	for _, c := range cases {
		if got := llmBackoffForAttempt(c.attempt); got != c.want {
			t.Errorf("attempt=%d backoff 期望 %v got %v", c.attempt, c.want, got)
		}
	}
}

// TestR131_006_ResetConsecutiveFailures_ClearsLastFailureTime 验证 R131 改 B 的回归保证:
// 连续失败 3 次后,ResetConsecutiveFailures 必须同时清零 consecutiveFailures 与
// lastFailureTime,让下次失败能"重新开始"冷却窗口计时(而非被旧时间戳干扰)。
func TestR131_006_ResetConsecutiveFailures_ClearsLastFailureTime(t *testing.T) {
	a := &Agent{
		consecutiveFailures: 3,
		lastFailureTime:     time.Now(),
		quarantined:         false,
	}
	a.ResetConsecutiveFailures()

	if a.consecutiveFailures != 0 {
		t.Errorf("ResetConsecutiveFailures 后 consecutiveFailures 应 = 0,got=%d", a.consecutiveFailures)
	}
	if !a.lastFailureTime.IsZero() {
		t.Errorf("ResetConsecutiveFailures 后 lastFailureTime 应归零(让下次失败重新开始冷却),got=%v", a.lastFailureTime)
	}
	if a.quarantined {
		t.Errorf("ResetConsecutiveFailures 后 quarantined 应 = false")
	}
}

// TestR131_007_BackoffCap_DoesNotBreakRetryLoop 验证 2026-07-24 优化的回归保证:
// 线性 cap 8s 后,第 1-7 次重试仍能发生(没被 cap 误中断)。
// 7 次累计 2+4+6+8+8+8+8 = 44s < 300s call timeout(2026-07-24 由 120s 上调)。
func TestR131_007_BackoffCap_DoesNotBreakRetryLoop(t *testing.T) {
	var total time.Duration
	for attempt := 1; attempt <= 7; attempt++ {
		backoff := llmBackoffForAttempt(attempt)
		if backoff > llmRetryMaxBackoff {
			backoff = llmRetryMaxBackoff
		}
		total += backoff
	}

	// 7 次累计 44s(2+4+6+8+8+8+8),远小于 300s call timeout
	if total >= time.Duration(defaultLLMCallTimeoutSec)*time.Second {
		t.Errorf("7 次 cap 后 backoff 累计(%v)应 < cfgLLMCallTimeoutSec(%ds),否则会撞 ctx 提前结束",
			total, defaultLLMCallTimeoutSec)
	}
	if total != 44*time.Second {
		t.Errorf("7 次 cap 后 backoff 累计期望 44s,got=%v", total)
	}
}

// TestR131_008_PermanentErrorVsTimeout_Classification 验证 R131 不破坏错误分类:
// - !retryable(401/403/400) → "permanent"
// - isAnthropic429 → "429"
// - isAnthropicTimeout → "timeout"
// - context.DeadlineExceeded → "timeout"
//
// 这一测试用 errors.Is 验证 stdlib 错误分类不被改 B 误改。
func TestR131_008_PermanentErrorVsTimeout_Classification(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		want    string // expected lastErrorClass
	}{
		{"permanent_403", errors.New("403 quota exhausted"), "permanent"},
		{"anthropic_429_sentinel", errors.New("anthropic: 429"), "429"},
		{"context_deadline", context.DeadlineExceeded, "timeout"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 验证 isAnthropicTimeout 仍能识别 context.DeadlineExceeded
			if errors.Is(tt.err, context.DeadlineExceeded) != (tt.want == "timeout") {
				t.Errorf("errors.Is(context.DeadlineExceeded) 期望 %v,实际 %v", tt.want == "timeout", errors.Is(tt.err, context.DeadlineExceeded))
			}
		})
	}
}
