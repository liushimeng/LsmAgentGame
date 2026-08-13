// Package wwplayer — memory_compact_wiring_test.go: §20260813-02 U1 接线测试。
//
// 缺陷背景(§130 第六次复现):CompactWithLLM 早已完整实现,但
// Agent.compactConfig 无任何 setter,Enabled 恒 false,run.go 触发判断永不
// 生效。本文件用 5 条断言锁住接线:
//
//	U1-01 SetCompactConfig 接线:setter 注入后配置可见(旧代码无 setter,编译期失败)
//	U1-02 触发路径:Enabled + 消息数达阈值 → 压缩真实执行,消息数下降 + 摘要落库
//	U1-03 失败显式回退:provider 报错 → 规则式压缩兜底 + BotTranscript 可观测标记
//	U1-04 增量摘要 prompt:有上次摘要 → PRESERVE+ADD 模式;无 → 全量模式
//	U1-05 配对完整性:recentMsgs 头部悬空 tool_result 被 dropLeadingOrphans 剔除
package wwplayer

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"LsmAgentGame/agent/wwtypes"
	"LsmAgentGame/llm"
	llmtypes "LsmAgentGame/llm/types"
)

// compactFakeProvider 按脚本返回固定摘要或错误。
type compactFakeProvider struct {
	calls   atomic.Int32
	summary string
	err     error
	// lastPrompt 记录最近一次请求的 user 文本(供增量 prompt 断言)。
	lastPrompt atomic.Value
}

func (p *compactFakeProvider) Chat(_ context.Context, _ string, req llm.LLMRequest) (llm.LLMResponse, error) {
	p.calls.Add(1)
	for _, m := range req.Messages {
		for _, b := range m.Content {
			if b.Type == "text" {
				p.lastPrompt.Store(b.Text)
			}
		}
	}
	if p.err != nil {
		return llm.LLMResponse{}, p.err
	}
	return llm.LLMResponse{
		Content: []llmtypes.ContentBlock{{Type: "text", Text: p.summary}},
	}, nil
}

func (p *compactFakeProvider) ChatStream(_ context.Context, _ string, _ llm.LLMRequest) (io.ReadCloser, error) {
	return nil, errors.New("not implemented")
}

func (p *compactFakeProvider) ProviderType() string { return "fake" }

// seedCompactableMemory 构造含 identity + n 条普通 user 消息的 Memory。
func seedCompactableMemory(n int) *Memory {
	m := NewMemory("villager", "good", "放逐全部狼人", 3)
	for i := 0; i < n; i++ {
		m.Push(llm.Message{
			Role:    "user",
			Content: []llmtypes.ContentBlock{{Type: "text", Text: "第" + strings.Repeat("x", 10) + "轮游戏上下文"}},
		})
		m.Push(llm.Message{
			Role:    "assistant",
			Content: []llmtypes.ContentBlock{{Type: "text", Text: "好的"}},
		})
	}
	return m
}

// TestCompactWiring_U1_01_SetterWiresConfig 断言 setter 接线存在且生效。
// 旧代码路径(无 SetCompactConfig)下本测试无法编译 —— 这本身就是 lint 级防护;
// 运行时断言防止未来有人把 setter 改成 no-op。
func TestCompactWiring_U1_01_SetterWiresConfig(t *testing.T) {
	a := &Agent{}
	if got := a.CompactConfigSnapshot().Enabled; got {
		t.Fatal("zero Agent compactConfig.Enabled must be false (default off)")
	}
	cfg := DefaultCompactConfig()
	cfg.MaxTokens = 1200
	a.SetCompactConfig(cfg)
	got := a.CompactConfigSnapshot()
	if !got.Enabled {
		t.Fatal("SetCompactConfig did not wire Enabled=true (§130 wiring regression)")
	}
	if got.MaxTokens != 1200 {
		t.Fatalf("MaxTokens = %d, want 1200", got.MaxTokens)
	}
	if got.TimeoutSec <= 0 {
		t.Fatalf("TimeoutSec = %d, want > 0", got.TimeoutSec)
	}
}

// TestCompactWiring_U1_02_TriggerPath 断言 maybeCompactMemory 真正触发压缩。
// 双向验证:把 SetCompactConfig 的 Enabled 改回 false(模拟「未接线」旧行为),
// 本测试在「消息数不变 + 摘要为空」处失败。
func TestCompactWiring_U1_02_TriggerPath(t *testing.T) {
	a := &Agent{Seat: 3, ModelKey: "fake-model"}
	a.Memory = seedCompactableMemory(25) // 1 + 50 条 > MinMessages(10)
	prov := &compactFakeProvider{summary: "## 本局概况\n测试摘要\n## 已确认信息\n暂无\n## 关键决策\n暂无\n## 待验证信息\n暂无"}
	a.Provider = prov
	cfg := DefaultCompactConfig()
	cfg.MinMessages = 10
	a.SetCompactConfig(cfg)

	before := a.Memory.Len()
	rp := func() (string, string, int, []int, int, int, bool) {
		return "speak", "villager", 3, []int{1, 2, 3}, -1, -1, false
	}
	a.maybeCompactMemory(context.Background(), rp, &wwtypes.GameContext{MySeat: 3, Round: 2})

	// 压缩在 goroutine 内执行,等待完成。
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if a.Memory.LastCompactSummary() != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if a.Memory.LastCompactSummary() == "" {
		t.Fatal("compact did not execute: lastCompactSummary empty (wiring regression — trigger path dead)")
	}
	if got := a.Memory.Len(); got >= before {
		t.Fatalf("compact did not shrink memory: before=%d after=%d", before, got)
	}
	if prov.calls.Load() != 1 {
		t.Fatalf("provider calls = %d, want exactly 1", prov.calls.Load())
	}
	a.Lock()
	fallback := a.compactFallback
	note := a.compactNote
	a.Unlock()
	if fallback {
		t.Fatalf("successful compact must not set fallback marker, note=%q", note)
	}
	if !strings.Contains(note, "压缩成功") {
		t.Fatalf("compact note missing success marker: %q", note)
	}
}

// TestCompactWiring_U1_03_FallbackExplicit 断言 LLM 压缩失败显式回退规则式压缩,
// 且可观测标记被写入(禁止假成功,OpenClaw Context §6.2)。
// 双向验证:删除 setCompactOutcome 调用 → fallback/note 断言失败。
func TestCompactWiring_U1_03_FallbackExplicit(t *testing.T) {
	a := &Agent{Seat: 1, ModelKey: "fake-model"}
	a.Memory = seedCompactableMemory(25)
	prov := &compactFakeProvider{err: errors.New("upstream 500")}
	a.Provider = prov
	cfg := DefaultCompactConfig()
	cfg.MinMessages = 10
	a.SetCompactConfig(cfg)

	rp := func() (string, string, int, []int, int, int, bool) {
		return "speak", "villager", 1, []int{1, 2}, -1, -1, false
	}
	a.maybeCompactMemory(context.Background(), rp, &wwtypes.GameContext{MySeat: 1, Round: 1})

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		a.Lock()
		done := a.compactAt != 0
		a.Unlock()
		if done {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	a.Lock()
	fallback := a.compactFallback
	note := a.compactNote
	at := a.compactAt
	a.Unlock()
	if at == 0 {
		t.Fatal("compact outcome never recorded (observability regression)")
	}
	if !fallback {
		t.Fatal("fallback marker not set after LLM compact failure (fake-success forbidden)")
	}
	if !strings.Contains(note, "回退") {
		t.Fatalf("fallback note missing 回退 marker: %q", note)
	}
	// 摘要不得写入(失败路径不允许假成功摘要)。
	if s := a.Memory.LastCompactSummary(); s != "" {
		t.Fatalf("failed compact must not store summary, got %q", s)
	}
}

// TestCompactWiring_U1_04_IncrementalPrompt 断言增量更新模式:
// 有上次摘要 → prompt 含 PRESERVE + 旧摘要全文;无 → 全量模式。
// 双向验证:删除 buildCompactUserPrompt 的增量分支 → PRESERVE 断言失败。
func TestCompactWiring_U1_04_IncrementalPrompt(t *testing.T) {
	// 纯函数层:全量 vs 增量。
	full := buildCompactUserPrompt("", 2, 3, "seer", "good", "CONV")
	if strings.Contains(full, "PRESERVE") || strings.Contains(full, "previous_summary") {
		t.Fatal("empty prevSummary must use full-rebuild prompt, got incremental markers")
	}
	inc := buildCompactUserPrompt("旧摘要:3号是金水", 2, 3, "seer", "good", "CONV")
	if !strings.Contains(inc, "PRESERVE") || !strings.Contains(inc, "旧摘要:3号是金水") {
		t.Fatalf("incremental prompt missing PRESERVE / prev summary: %q", inc)
	}

	// 全链路:第一次压缩(无上次摘要)→ Incremental=false;预置摘要后再压 → true。
	a := &Agent{Seat: 2, ModelKey: "fake-model"}
	a.Memory = seedCompactableMemory(25)
	prov := &compactFakeProvider{summary: "## 本局概况\nS1\n## 已确认信息\n暂无\n## 关键决策\n暂无\n## 待验证信息\n暂无"}
	cfg := DefaultCompactConfig()
	cfg.MinMessages = 10
	gc := &wwtypes.GameContext{MySeat: 2, Round: 2}
	r1 := a.Memory.CompactWithLLM(context.Background(), prov, "k", "fake-model", gc, cfg)
	if !r1.Success {
		t.Fatalf("first compact failed: %v", r1.Error)
	}
	if r1.Incremental {
		t.Fatal("first compact (no prev summary) must be full mode")
	}
	// 再塞够消息触发第二次(直接调用,不经过 compactDone — 测 Memory 层语义)。
	for i := 0; i < 20; i++ {
		a.Memory.Push(llm.Message{Role: "user", Content: []llmtypes.ContentBlock{{Type: "text", Text: "更多上下文更多上下文"}}})
	}
	r2 := a.Memory.CompactWithLLM(context.Background(), prov, "k", "fake-model", gc, cfg)
	if !r2.Success {
		t.Fatalf("second compact failed: %v", r2.Error)
	}
	if !r2.Incremental {
		t.Fatal("second compact (prev summary exists) must be incremental mode")
	}
	prompt, _ := prov.lastPrompt.Load().(string)
	if !strings.Contains(prompt, "PRESERVE") || !strings.Contains(prompt, "S1") {
		t.Fatalf("incremental wire prompt missing PRESERVE / prev summary content")
	}
}

// TestCompactWiring_U1_05_PairIntegrity 断言压缩后无悬空 tool_result
// (§82b:严格代理见到孤儿 tool_result 直接 400)。
// 双向验证:删除 CompactWithLLM 里的 dropLeadingOrphans 调用 → 断言失败。
func TestCompactWiring_U1_05_PairIntegrity(t *testing.T) {
	m := NewMemory("villager", "good", "放逐全部狼人", 0)
	// identity(0) + 2 对普通消息(1..4),使 msgCount=16 时 splitIdx=16/3=5,
	// 下方孤儿 tool_result 恰好落在切分边界 msgs[5](recentMsgs 头部)——
	// 这是生产里唯一会产生孤儿的位置(其配对 tool_use 落在被压缩的旧段)。
	for i := 0; i < 2; i++ {
		m.Push(llm.Message{Role: "user", Content: []llmtypes.ContentBlock{{Type: "text", Text: "上下文内容上下文内容"}}})
		m.Push(llm.Message{Role: "assistant", Content: []llmtypes.ContentBlock{{Type: "text", Text: "回复"}}})
	}
	// msgs[5] = 孤儿 tool_result(其配对 tool_use 在被压缩的旧段里 → 必被剔除)。
	orphan := llm.Message{
		Role: "user",
		Content: []llmtypes.ContentBlock{{
			Type: "tool_result", ToolUseID: "toolu_orphan_1",
			Content: []llmtypes.ContentBlock{{Type: "text", Text: "OK"}},
		}},
	}
	m.Push(orphan)
	for i := 0; i < 10; i++ {
		m.Push(llm.Message{Role: "user", Content: []llmtypes.ContentBlock{{Type: "text", Text: "近端消息近端消息近端消息"}}})
	}
	prov := &compactFakeProvider{summary: "## 本局概况\nS\n## 已确认信息\n暂无\n## 关键决策\n暂无\n## 待验证信息\n暂无"}
	cfg := DefaultCompactConfig()
	cfg.MinMessages = 10
	res := m.CompactWithLLM(context.Background(), prov, "k", "fake-model",
		&wwtypes.GameContext{MySeat: 0, Round: 1}, cfg)
	if !res.Success {
		t.Fatalf("compact failed: %v", res.Error)
	}
	msgs, _ := m.Snapshot()
	for i, msg := range msgs {
		for _, b := range msg.Content {
			if b.Type == "tool_result" && b.ToolUseID == "toolu_orphan_1" {
				t.Fatalf("orphan tool_result survived compact at msgs[%d] (dropLeadingOrphans missing)", i)
			}
		}
	}
	// 头部结构:identity + compact 摘要,且 compact 之后的第一条不是孤儿 tool_result。
	if len(msgs) < 2 {
		t.Fatalf("post-compact messages too short: %d", len(msgs))
	}
}
