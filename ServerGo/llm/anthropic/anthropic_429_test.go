// Package anthropic — anthropic_429_test.go
//
// §20260810-15: model-scoped 429 (rate-limit) circuit breaker 单元测试。
//
// 覆盖:
//   - Model429CircuitOpen 在首次 429 后立即返回 true(1-hit 即开)
//   - cooldown 过期后自动关闭
//   - recordModelSuccess 清理 429 窗口
//   - CircuitState 三态映射正确
//   - 429 与 400 circuit 互不污染(独立熔断)

package anthropic

import (
	"testing"
	"time"
)

func newTestProvider() *Provider {
	p := &Provider{
		model400Window:   map[string][]time.Time{},
		model400Dumped:   map[string]bool{},
		model400Circuit:  map[string]time.Time{},
		model429Window:   map[string][]time.Time{},
		model429Circuit:  map[string]time.Time{},
		breakerWindow:    map[string][]time.Time{},
		breakerOpenTill:  map[string]time.Time{},
	}
	return p
}

func TestModel429CircuitOpen_FirstHitOpens(t *testing.T) {
	// §20260810-15: 1 次 429 即打开熔断,避免 retry chain 把配额打爆。
	p := newTestProvider()
	model := "Tencent-model"

	if p.Model429CircuitOpen(model) {
		t.Fatalf("fresh provider should report circuit closed")
	}

	p.recordModel429(model)

	if !p.Model429CircuitOpen(model) {
		t.Fatalf("after one 429, circuit should be open (§20260810-15: 1-hit threshold)")
	}
}

func TestModel429CircuitOpen_CooldownExpires(t *testing.T) {
	// §20260810-15: cooldown 60s 后自动关闭,记录再次清空。
	p := newTestProvider()
	model := "Tencent-model"

	p.recordModel429(model)
	if !p.Model429CircuitOpen(model) {
		t.Fatalf("circuit should be open right after 429")
	}

	// 手工把 circuit 设置为 1ms 前过期,模拟 cooldown 过期
	p.model400Mu.Lock()
	p.model429Circuit[model] = time.Now().Add(-1 * time.Millisecond)
	p.model400Mu.Unlock()

	if p.Model429CircuitOpen(model) {
		t.Fatalf("circuit should auto-close after cooldown expires")
	}
}

func TestModel429Circuit_IndependentFrom400(t *testing.T) {
	// §20260810-15: 400 circuit 打开不应影响 429 circuit(独立熔断)。
	p := newTestProvider()
	model := "Tencent-model"

	// 直接打开 400 circuit(模拟)
	p.model400Mu.Lock()
	p.model400Circuit[model] = time.Now().Add(120 * time.Second)
	p.model400Mu.Unlock()

	if p.Model429CircuitOpen(model) {
		t.Fatalf("400 circuit open should NOT leak into 429 circuit (§20260810-15)")
	}
	if !p.Model400CircuitOpen(model) {
		t.Fatalf("400 circuit should still report open")
	}
}

func TestRecordModelSuccess_Clears429Window(t *testing.T) {
	// §20260810-15: 成功后清空 429 窗口(同 400 路径)。
	p := newTestProvider()
	model := "Tencent-model"

	// 注入几次窗口值
	p.model400Mu.Lock()
	now := time.Now()
	p.model429Window[model] = []time.Time{now.Add(-30 * time.Second), now.Add(-10 * time.Second)}
	p.model400Mu.Unlock()

	p.recordModelSuccess(model)

	p.model400Mu.Lock()
	remaining := len(p.model429Window[model])
	p.model400Mu.Unlock()
	if remaining != 0 {
		t.Fatalf("recordModelSuccess should clear 429 window; remaining=%d", remaining)
	}
}

func TestCircuitState_ThreeStateMapping(t *testing.T) {
	// §20260810-15 P2: closed/open_400/open_429 三态映射。
	p := newTestProvider()
	model := "Tencent-model"

	if got := p.CircuitState(model); got != "closed" {
		t.Fatalf("fresh provider should be closed, got %q", got)
	}

	// 打开 400 circuit
	p.model400Mu.Lock()
	p.model400Circuit[model] = time.Now().Add(120 * time.Second)
	p.model400Mu.Unlock()
	if got := p.CircuitState(model); got != "open_400" {
		t.Fatalf("after 400 circuit open, expected open_400, got %q", got)
	}

	// 关闭 400 + 打开 429
	p.model400Mu.Lock()
	delete(p.model400Circuit, model)
	p.model429Circuit[model] = time.Now().Add(60 * time.Second)
	p.model400Mu.Unlock()
	if got := p.CircuitState(model); got != "open_429" {
		t.Fatalf("after 429 circuit open, expected open_429, got %q", got)
	}

	// 全部关闭
	p.model400Mu.Lock()
	delete(p.model429Circuit, model)
	p.model400Mu.Unlock()
	if got := p.CircuitState(model); got != "closed" {
		t.Fatalf("after both cleared, expected closed, got %q", got)
	}
}

func TestModel429CircuitOpen_EmptyModel(t *testing.T) {
	// §20260810-15: 空 model 不应触发熔断查询(防止误关闭)。
	p := newTestProvider()

	if p.Model429CircuitOpen("") {
		t.Fatalf("empty model should report closed")
	}
	if p.CircuitState("") != "closed" {
		t.Fatalf("empty model CircuitState should be closed")
	}
}

func TestModel429Circuit_WindowSliding(t *testing.T) {
	// §20260810-15: 窗口滑动 — 60s 之前的过期时间应被剔除。
	p := newTestProvider()
	model := "Tencent-model"

	// 注入 1 个 70s 前的旧时间戳(应该被剔除)
	p.model400Mu.Lock()
	p.model429Window[model] = []time.Time{time.Now().Add(-70 * time.Second)}
	p.model400Mu.Unlock()

	// 再触发一次 429
	p.recordModel429(model)

	// 窗口应该只有最近 1 次,旧时间戳被剔除
	p.model400Mu.Lock()
	window := p.model429Window[model]
	p.model400Mu.Unlock()
	if len(window) != 1 {
		t.Fatalf("expected window size 1 after sliding, got %d", len(window))
	}
}

func TestModel400CircuitOpen_ExposedPublic(t *testing.T) {
	// §20260810-15: Model400CircuitOpen 公开方法正确短路 circuit-open 模型。
	p := newTestProvider()
	model := "Tencent-model"

	if p.Model400CircuitOpen(model) {
		t.Fatalf("fresh provider should be closed")
	}

	p.model400Mu.Lock()
	p.model400Circuit[model] = time.Now().Add(120 * time.Second)
	p.model400Mu.Unlock()

	if !p.Model400CircuitOpen(model) {
		t.Fatalf("after opening circuit, should report open")
	}
}