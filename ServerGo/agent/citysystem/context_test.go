// Package citysystem — context_test.go: ConversationStore 单测
// (2026-11-03 §批次81)。
package citysystem

import (
	"context"
	"errors"
	"fmt"
	"testing"

	llmtypes "LsmAgentGame/llm/types"
)

func msg(role, text string) llmtypes.Message {
	return llmtypes.Message{Role: role, Content: []llmtypes.ContentBlock{{Type: "text", Text: text}}}
}

// TestConversationStoreAppendTrim 超限裁剪:保留前 10 + 最近 N-10。
func TestConversationStoreAppendTrim(t *testing.T) {
	s := NewConversationStore(40)
	key := "R1/u1"
	for i := 0; i < 60; i++ {
		s.Append(key, msg("user", fmt.Sprintf("m%d", i)))
	}
	got := s.Get(key)
	if len(got) != 40 {
		t.Fatalf("len = %d, want 40", len(got))
	}
	// 前 10 条保留(头部保底)。
	for i := 0; i < 10; i++ {
		if got[i].Content[0].Text != fmt.Sprintf("m%d", i) {
			t.Fatalf("head[%d] = %q, want m%d", i, got[i].Content[0].Text, i)
		}
	}
	// 尾部保留最近 30 条(最后一条 = m59)。
	if last := got[len(got)-1].Content[0].Text; last != "m59" {
		t.Fatalf("last = %q, want m59", last)
	}
	// 尾部首条 = m30(60-30)。
	if tailFirst := got[10].Content[0].Text; tailFirst != "m30" {
		t.Fatalf("tail[0] = %q, want m30", tailFirst)
	}
}

// TestConversationStoreGetReturnsCopy 返回副本,调用方修改不污染存储。
func TestConversationStoreGetReturnsCopy(t *testing.T) {
	s := NewConversationStore(10)
	s.Append("k", msg("user", "a"))
	got := s.Get("k")
	got[0].Content[0].Text = "mutated"
	if again := s.Get("k"); again[0].Content[0].Text != "a" {
		t.Fatalf("store mutated: %q", again[0].Content[0].Text)
	}
}

// TestConversationStoreReset 清空会话。
func TestConversationStoreReset(t *testing.T) {
	s := NewConversationStore(10)
	s.Append("k", msg("user", "a"))
	s.Reset("k")
	if got := s.Get("k"); len(got) != 0 {
		t.Fatalf("after reset len = %d, want 0", len(got))
	}
	s.Reset("missing") // no-op 不 panic
}

// TestConversationStoreCompact 摘要压缩:最旧一半 → 一条 user 摘要 + 最新一半。
func TestConversationStoreCompact(t *testing.T) {
	s := NewConversationStore(40)
	key := "R1/u1"
	for i := 0; i < 10; i++ {
		s.Append(key, msg("user", fmt.Sprintf("m%d", i)))
	}
	called := 0
	err := s.Compact(context.Background(), key, func(msgs []llmtypes.Message) (string, error) {
		called++
		if len(msgs) != 5 {
			t.Fatalf("compact input len = %d, want 5 (最旧一半)", len(msgs))
		}
		return "前文要点", nil
	})
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if called != 1 {
		t.Fatalf("callLLM called %d times, want 1", called)
	}
	got := s.Get(key)
	if len(got) != 6 { // 1 条摘要 + 最新 5 条
		t.Fatalf("len = %d, want 6", len(got))
	}
	if got[0].Role != "user" || got[0].Content[0].Text != "【前文摘要】前文要点" {
		t.Fatalf("summary message = %+v", got[0])
	}
	if got[1].Content[0].Text != "m5" {
		t.Fatalf("first kept message = %q, want m5", got[1].Content[0].Text)
	}
}

// TestConversationStoreCompactSkipsSmall 消息过少时不压缩。
func TestConversationStoreCompactSkipsSmall(t *testing.T) {
	s := NewConversationStore(40)
	s.Append("k", msg("user", "a"))
	if err := s.Compact(context.Background(), "k", func([]llmtypes.Message) (string, error) {
		t.Fatal("callLLM must not be called for small conversations")
		return "", nil
	}); err != nil {
		t.Fatalf("compact: %v", err)
	}
}

// TestConversationStoreCompactErrors 回调错误时会话不被改动。
func TestConversationStoreCompactErrors(t *testing.T) {
	s := NewConversationStore(40)
	key := "R1/u1"
	for i := 0; i < 10; i++ {
		s.Append(key, msg("user", fmt.Sprintf("m%d", i)))
	}
	if err := s.Compact(context.Background(), key, nil); err == nil {
		t.Fatal("nil callback must fail")
	}
	want := errors.New("llm down")
	if err := s.Compact(context.Background(), key, func([]llmtypes.Message) (string, error) {
		return "", want
	}); !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
	if got := s.Get(key); len(got) != 10 {
		t.Fatalf("store changed after failed compact: len = %d, want 10", len(got))
	}
	// 空摘要视为失败。
	if err := s.Compact(context.Background(), key, func([]llmtypes.Message) (string, error) {
		return "   ", nil
	}); err == nil {
		t.Fatal("empty summary must fail")
	}
}

// TestConversationStoreConcurrent 并发 Append / Get(-race 覆盖)。
func TestConversationStoreConcurrent(t *testing.T) {
	s := NewConversationStore(40)
	done := make(chan struct{})
	for g := 0; g < 8; g++ {
		go func(g int) {
			defer func() { done <- struct{}{} }()
			for i := 0; i < 50; i++ {
				s.Append("k", msg("user", fmt.Sprintf("g%d-%d", g, i)))
				_ = s.Get("k")
			}
		}(g)
	}
	for g := 0; g < 8; g++ {
		<-done
	}
	if got := s.Get("k"); len(got) != 40 {
		t.Fatalf("len = %d, want 40", len(got))
	}
}

// turn 构造一个完整轮次:user 问 → assistant(tool_use) → user(tool_result)
// → assistant 答(4 条,严格交替 + 配对完整)。
func turn(i int) []llmtypes.Message {
	id := fmt.Sprintf("tu-%d", i)
	return []llmtypes.Message{
		msg("user", fmt.Sprintf("问%d", i)),
		toolUseMsg("", id),
		toolResultMsg(id),
		msg("assistant", fmt.Sprintf("答%d", i)),
	}
}

// TestConversationStoreTrimPairSafe 裁剪切口不拆散 tool_use/tool_result 对。
func TestConversationStoreTrimPairSafe(t *testing.T) {
	s := NewConversationStore(20)
	key := "R1/u1"
	for i := 0; i < 10; i++ {
		s.Append(key, turn(i)...)
	}
	got := s.Get(key)
	if len(got) != 20 {
		t.Fatalf("len = %d, want 20", len(got))
	}
	// 裁剪后的序列仍须配对完整(悬空 tool_use / 孤儿 tool_result 均不可接受);
	// 相邻同角色(切口接缝)是允许的存储形态,由出站闸口合并。
	for i := 1; i < len(got); i++ {
		if isToolResultMsg(got[i]) && !isToolUseMsg(got[i-1]) {
			// 接缝处:孤儿 tool_result 不得出现。
			t.Fatalf("trim split pair: msgs[%d] orphan tool_result", i)
		}
	}
	for i := 0; i < len(got)-1; i++ {
		if isToolUseMsg(got[i]) && !isToolResultMsg(got[i+1]) {
			t.Fatalf("trim split pair: msgs[%d] dangling tool_use", i)
		}
	}
	// 出站修整后必须完全合法。
	assertWireValid(t, sanitizeForWire(got))
}

// TestConversationStoreCompactPairSafe Compact 切口不拆散 tool 对:
// 切口恰落在 tool_result 上时,该对整体进摘要侧,rest 不得出现孤儿。
func TestConversationStoreCompactPairSafe(t *testing.T) {
	s := NewConversationStore(40)
	key := "R1/u1"
	// 4 条恰好成 2 个「半」:half=2 落在 tool_result(tu-0)上。
	s.Append(key, turn(0)...)
	err := s.Compact(context.Background(), key, func(oldest []llmtypes.Message) (string, error) {
		if len(oldest) != 2 {
			t.Fatalf("compact input len = %d, want 2", len(oldest))
		}
		return "前文要点", nil
	})
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	got := s.Get(key)
	for i := 1; i < len(got); i++ {
		if isToolResultMsg(got[i]) && !isToolUseMsg(got[i-1]) {
			t.Fatalf("compact split pair: msgs[%d] orphan tool_result", i)
		}
	}
	// 守卫后 rest = [assistant 答];无守卫则为 [orphan tool_result, assistant 答]。
	if len(got) != 2 || got[0].Role != "user" || got[1].Role != "assistant" {
		t.Fatalf("got = %+v, want [summary user, assistant]", got)
	}
	assertWireValid(t, sanitizeForWire(got))
}
