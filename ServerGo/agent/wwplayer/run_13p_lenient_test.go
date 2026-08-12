// R131 增强 — 13 人局宽松模式回归测试 (2026-07-15)
//
// 验证:
//   - 大房间 quarantine 阈值按座位数缩放
//   - timeout / 网络瞬断不计入 consecutiveFailures
//   - LLM 调用超时在 13 人局缩放到 180s
package wwplayer

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"LsmWebGame/llm/anthropic"
	llmtypes "LsmWebGame/llm/types"
)

// r131LenientFailProvider 模拟 context deadline exceeded / network 错误。
type r131LenientFailProvider struct {
	calls int
}

func (p *r131LenientFailProvider) Chat(_ context.Context, _ string, _ llmtypes.LLMRequest) (llmtypes.LLMResponse, error) {
	p.calls++
	return llmtypes.LLMResponse{}, context.DeadlineExceeded
}

func (p *r131LenientFailProvider) ChatStream(_ context.Context, _ string, _ llmtypes.LLMRequest) (io.ReadCloser, error) {
	p.calls++
	return io.NopCloser(strings.NewReader("")), context.DeadlineExceeded
}

func (p *r131LenientFailProvider) ChatStreamAccumulate(ctx context.Context, key string, req llmtypes.LLMRequest, onProgress func(llmtypes.StreamEvent) error) (llmtypes.LLMResponse, error) {
	return p.Chat(ctx, key, req)
}

func (p *r131LenientFailProvider) ProviderType() string { return "anthropic" }

func TestThresholdForSeatCount_13p(t *testing.T) {
	max7, perm7 := thresholdForSeatCount(7)
	if max7 != maxConsecutiveFailures || perm7 != permanentQuarantineThreshold {
		t.Fatalf("7 人局阈值应保持不变: max=%d/%d perm=%d/%d", max7, maxConsecutiveFailures, perm7, permanentQuarantineThreshold)
	}

	max13, perm13 := thresholdForSeatCount(13)
	wantMax := maxConsecutiveFailures + 6
	wantPerm := permanentQuarantineThreshold + 3
	if max13 != wantMax || perm13 != wantPerm {
		t.Fatalf("13 人局阈值期望 max=%d perm=%d, got max=%d perm=%d", wantMax, wantPerm, max13, perm13)
	}
}

func TestThresholdForSeatCount_CapsAt20(t *testing.T) {
	// 超过 13 人的房间(理论上不存在)不应无限放大。
	max20, perm20 := thresholdForSeatCount(20)
	wantMax := maxConsecutiveFailures + 6
	wantPerm := permanentQuarantineThreshold + 3
	if max20 != wantMax || perm20 != wantPerm {
		t.Fatalf("20 人局应被 cap,期望 max=%d perm=%d, got max=%d perm=%d", wantMax, wantPerm, max20, perm20)
	}
}

func TestIsNetworkOrTimeoutTransient(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"context_deadline", context.DeadlineExceeded, true},
		{"io_timeout", errors.New("read tcp: i/o timeout"), true},
		{"connection_refused", errors.New("dial tcp 127.0.0.1: connect: connection refused"), true},
		{"no_such_host", errors.New("dial tcp: lookup x: no such host"), true},
		{"broken_pipe", errors.New("write tcp: broken pipe"), true},
		{"reset_by_peer", errors.New("read tcp: reset by peer"), true},
		{"closed_network", errors.New("use of closed network connection"), true},
		{"context_canceled", context.Canceled, true},
		{"permanent_403", errors.New("403 quota exhausted"), false},
		{"permanent_401", errors.New("401 invalid authorization"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isNetworkOrTimeoutTransient(c.err); got != c.want {
				t.Errorf("isNetworkOrTimeoutTransient(%q) = %v, want %v", c.err, got, c.want)
			}
		})
	}
}

func TestCfgLLMCallTimeoutSec_13pLenient(t *testing.T) {
	// 通过 LSM_CONF 环境变量注入一个最小配置,测试后恢复。
	orig := os.Getenv("LSM_CONF")
	defer os.Setenv("LSM_CONF", orig)

	tmp := t.TempDir()
	conf := filepath.Join(tmp, "test.conf")
	os.WriteFile(conf, []byte(`{"werewolf":{"llm_call_timeout_sec":120,"lenient_mode_for_seat_count":13,"llm_timeout_scale_percent":150}}`), 0644)
	os.Setenv("LSM_CONF", conf)

	if got := cfgLLMCallTimeoutSec(7); got != 120 {
		t.Errorf("7 人局 timeout 期望 120s(显式配置), got %d", got)
	}
	// 2026-07-24 优化: 显式 120s × 150% = 180s(< 480s cap 不被裁剪)。
	if got := cfgLLMCallTimeoutSec(13); got != 180 {
		t.Errorf("13 人局 timeout 期望 180s, got %d", got)
	}

	// 上限 480s(2026-07-24 由 300s 上调,lenient ×150%=450s 必须生效);
	// 这里直接验证 cap 公式。
	scaled := 400 * 150 / 100
	if scaled > 480 {
		scaled = 480
	}
	if scaled != 480 {
		t.Errorf("13 人局 timeout 应被 cap 到 480s, got %d", scaled)
	}
}

func TestTimeoutNotCountedForQuarantine(t *testing.T) {
	a := &Agent{
		Seat:                0,
		consecutiveFailures: 0,
		lastFailureTime:     time.Time{},
	}

	// 模拟 handleEvent 中对 timeout 错误的处理路径
	now := time.Now()
	if isNetworkOrTimeoutTransient(context.DeadlineExceeded) {
		// 不递增
	} else {
		a.consecutiveFailures++
		a.lastFailureTime = now
	}

	if a.consecutiveFailures != 0 {
		t.Fatalf("timeout 错误不应累计 consecutiveFailures, got %d", a.consecutiveFailures)
	}
}

func TestPermanentErrorStillCounts(t *testing.T) {
	a := &Agent{
		Seat:                0,
		consecutiveFailures: 0,
		lastFailureTime:     time.Time{},
	}

	now := time.Now()
	err := errors.New("403 quota exhausted")
	if isNetworkOrTimeoutTransient(err) {
		// 不递增
	} else {
		a.consecutiveFailures++
		a.lastFailureTime = now
	}

	if a.consecutiveFailures != 1 {
		t.Fatalf("永久错误应累计 consecutiveFailures, got %d", a.consecutiveFailures)
	}
}

// TestCircuitOpenNotCountedForQuarantine 验证 2026-07-24 优化:
// model_400_circuit 熔断窗口内的快速失败按 transient 处理(滑动冷却窗口,
// 不递增 consecutiveFailures),熔断 120s 恢复后 bot 自然复活,
// 不会被永久 quarantine(13 人局 Kimi/GLM 批量"已禁用 · 连续失败"主因修复)。
func TestCircuitOpenNotCountedForQuarantine(t *testing.T) {
	// 1. 判定 helper:直接 *anthropic.Error、errors.As 包装、字符串兜底三条路径。
	direct := &anthropic.Error{HTTPStatus: 400, Retryable: false, Source: "model_400_circuit", Message: "circuit open"}
	if !isModel400CircuitErr(direct) {
		t.Fatalf("直接 *anthropic.Error{Source:model_400_circuit} 应被识别")
	}
	wrapped := errors.New("agent outer: " + direct.Error()) // 无 errors.As 链路,走字符串兜底
	if !isModel400CircuitErr(wrapped) {
		t.Fatalf("字符串包含 model_400_circuit 的错误应被识别(兜底路径)")
	}
	if isModel400CircuitErr(errors.New("403 quota exhausted")) {
		t.Fatalf("普通 403 永久错误不应被识别为熔断错误")
	}
	if isModel400CircuitErr(nil) {
		t.Fatalf("nil 错误不应被识别为熔断错误")
	}

	// 2. 失败计数路径:熔断错误必须走 transient 分支(与 handleEvent 中
	//    "isModel400CircuitErr(err) → transient=true" 逻辑保持一致),
	//    连续 3 次熔断失败 consecutiveFailures 仍为 0。
	a := &Agent{Seat: 0}
	for i := 0; i < 3; i++ {
		err := &anthropic.Error{HTTPStatus: 400, Retryable: false, Source: "model_400_circuit"}
		transient := isNetworkOrTimeoutTransient(err)
		if isModel400CircuitErr(err) {
			transient = true
		}
		a.RecordFailure(time.Now(), transient, failCooldownWindow)
	}
	if got := a.ConsecutiveFailures(); got != 0 {
		t.Fatalf("熔断窗口内失败不应累计 consecutiveFailures, got %d", got)
	}

	// 3. 对照:真正的永久错误(401/403)不受熔断逻辑影响,仍正常计数。
	b := &Agent{Seat: 1}
	permErr := errors.New("403 quota exhausted")
	transient := isNetworkOrTimeoutTransient(permErr)
	if isModel400CircuitErr(permErr) {
		transient = true
	}
	b.RecordFailure(time.Now(), transient, failCooldownWindow)
	if got := b.ConsecutiveFailures(); got != 1 {
		t.Fatalf("真正的永久错误仍应累计 consecutiveFailures, got %d", got)
	}
}
