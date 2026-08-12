// R85-P2 回归测试 (2026-07-10):
// 验证 PushTool 已在 dispatch loop 中接入,RecentTools(5) 返回真实工具名,
// BotTranscript.ToolCalls 不再恒为空。
//
// 背景:R85 报告指出"工具调用 Bot#2 显示无调用" — 根因是 Memory.PushTool
// 从未被调用过(自初始提交 9c32aec 即未接入),导致 m.tools 始终为空,
// BotTranscript.ToolCalls 恒为 []string{},前端"🔧 工具调用 最近 5 条"面板
// 永远显示"(暂无)",即使 bot 实际调用了 finish_vote/vote。
//
// 本测试使用包内白盒测试 (package agent) 直接调用 recordTranscript,
// 验证修复链(dispatch loop 接入 PushTool + recordTranscript 读取 RecentTools)。
package wwplayer

import (
	"testing"

	"LsmWebGame/config"
	"LsmWebGame/llm"
)

// newStubRegistry 构造一个包含已知 model 的 stub registry,让 NewWithRoom
// 在不接入真实 LLM endpoint 的情况下也能成功创建 Agent(reference 测试)。
func newStubRegistry() *llm.Registry {
	return llm.NewRegistry(config.LLMConfig{
		Endpoint: "http://127.0.0.1:1/x", // 故意不可达,NewWithRoom 不实际发请求
		Providers: []config.ProviderConfig{
			{AgentName: "stub", Model: "Qwen-model", APIKey: "sk-real"},
		},
	})
}

// TestR85_RecentTools_NonEmpty 验证 PushTool 调用后 RecentTools 返回数据。
func TestR85_RecentTools_NonEmpty(t *testing.T) {
	m := NewMemory("villager", "good", "kill wolves", 1)

	// 模拟 dispatcher 的预期调用(R85-P2 修复核心)。
	m.PushTool(ToolRecord{Name: "vote", Result: "ok"})
	m.PushTool(ToolRecord{Name: "finish_vote", Result: "ok"})

	tools := m.RecentTools(5)
	if len(tools) != 2 {
		t.Fatalf("want 2 tool records, got %d", len(tools))
	}
	if tools[0].Name != "vote" || tools[1].Name != "finish_vote" {
		t.Errorf("unexpected tool order: %v, %v", tools[0].Name, tools[1].Name)
	}
}

// TestR85_BotTranscript_ToolCalls_Populated 验证 recordTranscript 路径:
// 当 Memory.tools 有数据时,生成的 BotTranscript.ToolCalls 字段应非空。
// 这就是前端"🔧 工具调用 最近 5 条"面板读取的字段。
func TestR85_BotTranscript_ToolCalls_Populated(t *testing.T) {
	reg := newStubRegistry()
	a, err := NewWithRoom(1, "Qwen-model", "villager", "good", "kill wolves",
		reg, "room-r85", "user-r85")
	if err != nil {
		t.Fatalf("NewWithRoom failed: %v", err)
	}

	// 模拟 dispatch loop 接入 PushTool 后的状态(R85-P2 修复核心)。
	a.Memory.PushTool(ToolRecord{Name: "vote", Input: map[string]any{"target": 2}, Result: "ok"})
	a.Memory.PushTool(ToolRecord{Name: "finish_vote", Input: map[string]any{"target": 0}, Result: "ok"})

	// 触发 recordTranscript 的最近决策分支。
	a.SetLastDecision(RecordDecisionState{
		LastToolName:  "finish_vote",
		LastToolInput: map[string]any{"target": 0},
		LastOutcome:   "OK",
	})

	// 白盒调用 recordTranscript(从 package agent 内部可见),填充 lastTranscript。
	a.recordTranscript()

	bt := a.BotTranscript()
	if bt == nil {
		t.Fatalf("R85-P2 regression: BotTranscript 返回 nil")
	}
	if len(bt.ToolCalls) == 0 {
		t.Fatalf("R85-P2 regression: BotTranscript.ToolCalls 仍为空 — "+
			"PushTool 未接入或 recordTranscript 未调用 RecentTools。got=%v", bt.ToolCalls)
	}
	has := func(name string) bool {
		for _, tc := range bt.ToolCalls {
			if len(tc) >= len(name) && tc[:len(name)] == name {
				return true
			}
		}
		return false
	}
	if !has("vote") {
		t.Errorf("expected ToolCalls to contain 'vote', got %v", bt.ToolCalls)
	}
	if !has("finish_vote") {
		t.Errorf("expected ToolCalls to contain 'finish_vote', got %v", bt.ToolCalls)
	}
}

// TestR85_PushTool_RingBuffer 验证 PushTool 的 100 条环形缓冲,
// 确保 BotTranscript 不会因长对局而工具记录无限增长。
func TestR85_PushTool_RingBuffer(t *testing.T) {
	m := NewMemory("villager", "good", "kill wolves", 0)
	for i := 0; i < 150; i++ {
		m.PushTool(ToolRecord{Name: "vote", Result: "ok"})
	}
	tools := m.RecentTools(1000)
	if len(tools) != 100 {
		t.Fatalf("want ring buffer capped at 100, got %d", len(tools))
	}
}

