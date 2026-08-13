// Package werewolf — agent_memory_iter_u4_test.go: §20260813-02 U4 记忆迭代加固测试。
//
// 覆盖:
//
//	U4-A 流式优先:provider 实现 ChatStreamAccumulate 时走流式路径(§197 慢模型友好)
//	U4-B transient 重试 1 次:retryable 错误 → 5s 后重试,第二次成功即返回
//	U4-C permanent 不重试:401/403(Retryable=false)→ 仅 1 次调用直接失败
//	U4-D 超时配置兜底:cfgAgentMemoryIterTimeoutSec 默认 480(config panic 安全)
package werewolf

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	"LsmAgentGame/llm"
	"LsmAgentGame/llm/anthropic"
	llmtypes "LsmAgentGame/llm/types"
)

// iterFakeProvider 按脚本控制 Chat / ChatStreamAccumulate 行为。
type iterFakeProvider struct {
	chatCalls   atomic.Int32
	streamCalls atomic.Int32
	// failFirst:第一次调用返回该错误(retryable 或 permanent 由调用方填)。
	failFirst error
	// alwaysFail:所有调用都失败。
	alwaysFail error
}

func (p *iterFakeProvider) nextErr() error {
	if p.alwaysFail != nil {
		return p.alwaysFail
	}
	if p.failFirst != nil {
		e := p.failFirst
		p.failFirst = nil
		return e
	}
	return nil
}

func (p *iterFakeProvider) Chat(_ context.Context, _ string, _ llm.LLMRequest) (llm.LLMResponse, error) {
	p.chatCalls.Add(1)
	if err := p.nextErr(); err != nil {
		return llm.LLMResponse{}, err
	}
	return llm.LLMResponse{
		Content: []llmtypes.ContentBlock{{Type: "text", Text: "ITERATED-VIA-CHAT"}},
	}, nil
}

func (p *iterFakeProvider) ChatStream(_ context.Context, _ string, _ llm.LLMRequest) (io.ReadCloser, error) {
	return nil, errors.New("not implemented")
}

func (p *iterFakeProvider) ChatStreamAccumulate(_ context.Context, _ string, _ llm.LLMRequest, _ func(llmtypes.StreamEvent) error) (llm.LLMResponse, error) {
	p.streamCalls.Add(1)
	if err := p.nextErr(); err != nil {
		return llm.LLMResponse{}, err
	}
	return llm.LLMResponse{
		Content: []llmtypes.ContentBlock{{Type: "text", Text: "ITERATED-VIA-STREAM"}},
	}, nil
}

func (p *iterFakeProvider) ProviderType() string { return "fake" }

// TestMemoryIter_U4_A_StreamingPreferred 断言迭代调用走流式管线
// (ChatStreamAccumulate),与主对话一致(§197)。
// 双向验证:把 callMemoryIterLLM 改回 provider.Chat → streamCalls=0 断言失败。
func TestMemoryIter_U4_A_StreamingPreferred(t *testing.T) {
	p := &iterFakeProvider{}
	text, err := callMemoryIterLLM(context.Background(), p, "k", llm.LLMRequest{Model: "fake"})
	if err != nil {
		t.Fatalf("call failed: %v", err)
	}
	if !strings.Contains(text, "ITERATED-VIA-STREAM") {
		t.Fatalf("expect streaming response, got %q", text)
	}
	if p.streamCalls.Load() != 1 || p.chatCalls.Load() != 0 {
		t.Fatalf("streaming path not used: stream=%d chat=%d", p.streamCalls.Load(), p.chatCalls.Load())
	}
}

// TestMemoryIter_U4_B_TransientRetryOnce 断言 retryable 失败重试 1 次后成功。
// 双向验证:删除重试循环 → 第一次错误直接返回,断言失败。
func TestMemoryIter_U4_B_TransientRetryOnce(t *testing.T) {
	p := &iterFakeProvider{
		failFirst: &anthropic.Error{HTTPStatus: 500, Retryable: true, Message: "upstream 500"},
	}
	text, err := callMemoryIterLLM(context.Background(), p, "k", llm.LLMRequest{Model: "fake"})
	if err != nil {
		t.Fatalf("transient failure must be retried and succeed, got err: %v", err)
	}
	if !strings.Contains(text, "ITERATED-VIA-STREAM") {
		t.Fatalf("unexpected text: %q", text)
	}
	if got := p.streamCalls.Load(); got != 2 {
		t.Fatalf("calls = %d, want exactly 2 (1 fail + 1 retry)", got)
	}
}

// TestMemoryIter_U4_C_PermanentNoRetry 断言 permanent(401/403)不重试,
// 直接失败交给 FallbackMerge(不浪费配额)。
// 双向验证:把 isMemoryIterTransient 改成恒 true → 调用数变 2 断言失败。
func TestMemoryIter_U4_C_PermanentNoRetry(t *testing.T) {
	p := &iterFakeProvider{
		alwaysFail: &anthropic.Error{HTTPStatus: 403, Retryable: false, Message: "quota exhausted"},
	}
	_, err := callMemoryIterLLM(context.Background(), p, "k", llm.LLMRequest{Model: "fake"})
	if err == nil {
		t.Fatal("permanent failure must surface as error (caller falls back)")
	}
	if got := p.streamCalls.Load(); got != 1 {
		t.Fatalf("calls = %d, want exactly 1 (permanent must not retry)", got)
	}
}

// TestMemoryIter_U4_D_TimeoutConfigFallback 断言超时配置读取的默认值与
// panic 安全(测试环境 config.Load 可能 panic,§197 教训 3)。
func TestMemoryIter_U4_D_TimeoutConfigFallback(t *testing.T) {
	got := cfgAgentMemoryIterTimeoutSec()
	if got <= 0 {
		t.Fatalf("cfgAgentMemoryIterTimeoutSec = %d, want > 0", got)
	}
	// 测试环境无 LsmAgentGame.conf 时 config.Load panic → 兜底常量 480。
	// 有配置时 applyDefaults 也填 480;两种路径都必须 ≥ 90s 的旧硬编码。
	if got < 90 {
		t.Fatalf("timeout %d must be ≥ old 90s hardcode (U4 long-budget intent)", got)
	}
}
