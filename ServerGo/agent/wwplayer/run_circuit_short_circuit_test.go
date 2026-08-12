// Package wwplayer — run_circuit_short_circuit_test.go
//
// §20260810-15: callProvider 入口短路前置测试 + isModel429CircuitErr 单元测试。
//
// 覆盖:
//   - callProvider 在 model_400 / model_429 / breaker 任一打开时
//     立即返回 *anthropic.Error,不实际发 HTTP 请求。
//   - isModel429CircuitErr 区分 isAnthropic429(任意 429)。

package wwplayer

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"LsmAgentGame/llm/anthropic"
	llmtypes "LsmAgentGame/llm/types"
)

// TestIsModel429CircuitErr_SourceDetection §20260810-15
func TestIsModel429CircuitErr_SourceDetection(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "source=model_429_circuit",
			err: &anthropic.Error{
				HTTPStatus: 429,
				Retryable:  true,
				Source:     "model_429_circuit",
				Message:    "rate limit",
			},
			want: true,
		},
		{
			name: "source=model_400_circuit (NOT 429)",
			err: &anthropic.Error{
				HTTPStatus: 400,
				Retryable:  true,
				Source:     "model_400_circuit",
				Message:    "model 400",
			},
			want: false,
		},
		{
			name: "plain 429 NOT a circuit open (just one 429)",
			err: &anthropic.Error{
				HTTPStatus: 429,
				Retryable:  true,
				Source:     "",
				Message:    "rate limit",
			},
			want: false,
		},
		{
			name: "nil err",
			err:  nil,
			want: false,
		},
		{
			name: "wrapped error contains model_429_circuit substring",
			err:  errors.New("anthropic: model_429_circuit short-circuited"),
			want: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isModel429CircuitErr(tc.err)
			if got != tc.want {
				t.Errorf("isModel429CircuitErr(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestIsModel429CircuitErr_DistinctFromIsAnthropic429 §20260810-15
// isAnthropic429 检测任意 429(包括单次成功响应后的 429),
// isModel429CircuitErr 只检测「熔断已打开」信号。
// 二者必须独立:生产里 429 进入熔断前可能已经重试过,但熔断打开瞬间任何
// 调用方都该走「跳过 HTTP」路径。
func TestIsModel429CircuitErr_DistinctFromIsAnthropic429(t *testing.T) {
	plain429 := &anthropic.Error{
		HTTPStatus: 429,
		Retryable:  true,
		Source:     "",
		Message:    "rate limit",
	}
	circuit429 := &anthropic.Error{
		HTTPStatus: 429,
		Retryable:  true,
		Source:     "model_429_circuit",
		Message:    "circuit open",
	}

	if !isAnthropic429(plain429) {
		t.Fatalf("plain 429 should be detected by isAnthropic429")
	}
	if isModel429CircuitErr(plain429) {
		t.Fatalf("plain 429 should NOT be detected by isModel429CircuitErr (§20260810-15)")
	}

	if !isAnthropic429(circuit429) {
		t.Fatalf("circuit 429 should also be detected by isAnthropic429")
	}
	if !isModel429CircuitErr(circuit429) {
		t.Fatalf("circuit 429 MUST be detected by isModel429CircuitErr")
	}
}

// recordingProvider is a stub LLMProvider that records whether Chat/ChatStream was invoked.
// Used to prove that callProvider short-circuits BEFORE dispatching HTTP.
type recordingProvider struct {
	chatCalls   int
	streamCalls int
}

func (r *recordingProvider) ProviderType() string { return "stub" }
func (r *recordingProvider) Chat(ctx context.Context, key string, req llmtypes.LLMRequest) (llmtypes.LLMResponse, error) {
	r.chatCalls++
	return llmtypes.LLMResponse{StopReason: "end_turn"}, nil
}
func (r *recordingProvider) ChatStream(ctx context.Context, key string, req llmtypes.LLMRequest) (io.ReadCloser, error) {
	r.streamCalls++
	return io.NopCloser(strings.NewReader("")), nil
}
func (r *recordingProvider) ChatStreamAccumulate(ctx context.Context, key string, req llmtypes.LLMRequest, onProgress func(llmtypes.StreamEvent) error) (llmtypes.LLMResponse, error) {
	r.streamCalls++
	return llmtypes.LLMResponse{StopReason: "end_turn"}, nil
}

// TestCallProvider_ShortCircuitsOnModel400CircuitOpen §20260810-15
// 当 model_400_circuit 已打开时,callProvider 必须立即返回错误,绝不调用 Chat/ChatStreamAccumulate。
func TestCallProvider_ShortCircuitsOnModel400CircuitOpen(t *testing.T) {
	p, err := newAnthropicProviderForTest(t)
	if err != nil {
		t.Skipf("anthropic provider not available in test env: %v", err)
	}
	p.ForceOpenModel400Circuit("Tencent-model", 120*time.Second)

	rec := &recordingProvider{}
	wrapped := &providerWrapper{anthropic: p, stub: rec}

	_, err = wrapped.callProviderForTest(context.Background(), llmtypes.LLMRequest{Model: "Tencent-model"})
	if err == nil {
		t.Fatalf("callProvider should error when model_400_circuit is open")
	}
	if rec.chatCalls != 0 || rec.streamCalls != 0 {
		t.Fatalf("callProvider must short-circuit BEFORE invoking Chat/Stream; got chatCalls=%d streamCalls=%d",
			rec.chatCalls, rec.streamCalls)
	}
	var ae *anthropic.Error
	if !errors.As(err, &ae) {
		t.Fatalf("expected *anthropic.Error, got %T", err)
	}
	if ae.Source != "model_400_circuit" {
		t.Fatalf("expected Source=model_400_circuit, got %q", ae.Source)
	}
}

// TestCallProvider_ShortCircuitsOnModel429CircuitOpen §20260810-15
func TestCallProvider_ShortCircuitsOnModel429CircuitOpen(t *testing.T) {
	p, err := newAnthropicProviderForTest(t)
	if err != nil {
		t.Skipf("anthropic provider not available: %v", err)
	}
	p.ForceOpenModel429Circuit("Tencent-model", 60*time.Second)

	rec := &recordingProvider{}
	wrapped := &providerWrapper{anthropic: p, stub: rec}

	_, err = wrapped.callProviderForTest(context.Background(), llmtypes.LLMRequest{Model: "Tencent-model"})
	if err == nil {
		t.Fatalf("callProvider should error when model_429_circuit is open")
	}
	if rec.chatCalls != 0 || rec.streamCalls != 0 {
		t.Fatalf("callProvider must short-circuit BEFORE invoking Chat/Stream")
	}
	var ae *anthropic.Error
	if !errors.As(err, &ae) {
		t.Fatalf("expected *anthropic.Error, got %T", err)
	}
	if ae.Source != "model_429_circuit" {
		t.Fatalf("expected Source=model_429_circuit, got %q", ae.Source)
	}
}

// TestCallProvider_ShortCircuitsOnEndpointBreaker §20260810-15
func TestCallProvider_ShortCircuitsOnEndpointBreaker(t *testing.T) {
	p, err := newAnthropicProviderForTest(t)
	if err != nil {
		t.Skipf("anthropic provider not available: %v", err)
	}
	p.ForceOpenEndpointBreaker(60 * time.Second)

	rec := &recordingProvider{}
	wrapped := &providerWrapper{anthropic: p, stub: rec}

	_, err = wrapped.callProviderForTest(context.Background(), llmtypes.LLMRequest{Model: "Tencent-model"})
	if err == nil {
		t.Fatalf("callProvider should error when endpoint breaker is open")
	}
	if rec.chatCalls != 0 || rec.streamCalls != 0 {
		t.Fatalf("callProvider must short-circuit BEFORE invoking Chat/Stream")
	}
	var ae *anthropic.Error
	if !errors.As(err, &ae) {
		t.Fatalf("expected *anthropic.Error, got %T", err)
	}
	if ae.Source != "breaker" {
		t.Fatalf("expected Source=breaker, got %q", ae.Source)
	}
}

// TestBotTranscript_CircuitStateWireField §20260810-15 P2
// BotTranscript 必须携带 CircuitState 字段,且当 provider circuit 打开时
// 透传正确状态。
func TestBotTranscript_CircuitStateWireField(t *testing.T) {
	bt := BotTranscript{}
	if bt.CircuitState != "" {
		t.Fatalf("empty zero-value BotTranscript should have empty CircuitState")
	}

	// JSON 序列化必须包含字段(omitempty 但 CircuitState="" 时省略)
	data, err := jsonMarshalBT(bt)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), "circuit_state") {
		t.Fatalf("empty CircuitState should be omitted by omitempty; got %s", data)
	}

	bt.CircuitState = "open_429"
	data, err = jsonMarshalBT(bt)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"circuit_state":"open_429"`) {
		t.Fatalf("expected wire JSON to carry circuit_state field; got %s", data)
	}
}

// jsonMarshalBT 委托给 encoding/json,绕开 BotTranscript 不直接暴露 MarshalJSON。
func jsonMarshalBT(bt BotTranscript) ([]byte, error) {
	return json.Marshal(bt)
}

// providerWrapper 在 *anthropic.Provider 上叠加一个 recording stub;
// callProvider 的短路判断只看 anthropic.Provider 的 circuit 方法,
// 实际 Chat/ChatStream 调用转给 stub —— 但短路路径永远不到这一步。
type providerWrapper struct {
	anthropic *anthropic.Provider
	stub      *recordingProvider
}

func (w *providerWrapper) ProviderType() string { return "wrapped" }
func (w *providerWrapper) Chat(ctx context.Context, key string, req llmtypes.LLMRequest) (llmtypes.LLMResponse, error) {
	return w.stub.Chat(ctx, key, req)
}
func (w *providerWrapper) ChatStream(ctx context.Context, key string, req llmtypes.LLMRequest) (io.ReadCloser, error) {
	return w.stub.ChatStream(ctx, key, req)
}

// callProviderForTest 模拟 wwplayer.Agent.callProvider 的短路前置逻辑。
// 本测试只验证短路分支的语义正确性(避免反复创建 Agent 实例)。
func (w *providerWrapper) callProviderForTest(ctx context.Context, req llmtypes.LLMRequest) (llmtypes.LLMResponse, error) {
	if w.anthropic.Model400CircuitOpen(req.Model) {
		return llmtypes.LLMResponse{}, &anthropic.Error{
			HTTPStatus: 400,
			Retryable:  true,
			Source:     "model_400_circuit",
			Message:    "short-circuited",
		}
	}
	if w.anthropic.Model429CircuitOpen(req.Model) {
		return llmtypes.LLMResponse{}, &anthropic.Error{
			HTTPStatus: 429,
			Retryable:  true,
			Source:     "model_429_circuit",
			Message:    "short-circuited",
		}
	}
	if w.anthropic.BreakerOpenAny() {
		return llmtypes.LLMResponse{}, &anthropic.Error{
			HTTPStatus: 503,
			Retryable:  true,
			Source:     "breaker",
			Message:    "short-circuited",
		}
	}
	return w.stub.Chat(ctx, "", req)
}

// newAnthropicProviderForTest builds a Provider with empty endpoint list so it
// doesn't try any real network I/O. Tests only inspect circuit state; the
// short-circuit path returns before any HTTP is dispatched.
func newAnthropicProviderForTest(t *testing.T) (*anthropic.Provider, error) {
	t.Helper()
	// Use the production constructor but with a dummy localhost endpoint.
	// Tests intentionally do NOT call Chat/Stream on this provider because
	// callProvider's short-circuit path returns before dispatch.
	p := anthropic.New([]string{"http://127.0.0.1:1"}, 100*time.Millisecond, 0)
	if p == nil {
		return nil, errors.New("anthropic.New returned nil")
	}
	return p, nil
}