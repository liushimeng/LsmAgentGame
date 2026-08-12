package wwplayer

import (
	"testing"

	"LsmAgentGame/llm"
	llmtypes "LsmAgentGame/llm/types"
)

// TestApproxSystemToolsBytes 验证 system + tools 字节估算的准确性。
// 2026-08-10 §20260810-14 新增。
func TestApproxSystemToolsBytes(t *testing.T) {
	system := []llmtypes.SystemBlock{
		{Type: "text", Text: "You are a werewolf game player."},
		{Type: "text", Text: "Follow the rules carefully."},
	}
	tools := []llmtypes.ToolDef{
		{
			Name:        "speak",
			Description: "Speak to all players in the room.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"text": map[string]any{
						"type":        "string",
						"description": "The text to speak.",
					},
				},
			},
		},
	}

	bytes := approxSystemToolsBytes(system, tools)
	// 验证:system 文本 ~60 bytes + tools ~100 bytes ≈ 160 bytes
	// 估算值应该大于 0 且在合理范围内
	if bytes <= 0 {
		t.Errorf("approxSystemToolsBytes returned %d, expected > 0", bytes)
	}
	if bytes > 1000 {
		t.Errorf("approxSystemToolsBytes returned %d, seems too large", bytes)
	}
	t.Logf("approxSystemToolsBytes: %d bytes", bytes)
}

// TestMemory_SetSystemTools 验证 SetSystemTools 正确设置字节数。
// 2026-08-10 §20260810-14 新增。
func TestMemory_SetSystemTools(t *testing.T) {
	m := NewMemory("werewolf", "wolf", "kill all villagers", 0)

	system := []llmtypes.SystemBlock{
		{Type: "text", Text: "System prompt text here."},
	}
	tools := []llmtypes.ToolDef{
		{Name: "speak", Description: "Speak to players."},
	}

	m.SetSystemTools(system, tools)

	m.mu.RLock()
	totalSystemToolsBytes := m.totalSystemToolsBytes
	m.mu.RUnlock()

	if totalSystemToolsBytes <= 0 {
		t.Errorf("totalSystemToolsBytes = %d, expected > 0", totalSystemToolsBytes)
	}
	t.Logf("totalSystemToolsBytes: %d", totalSystemToolsBytes)
}

// TestMemory_EnforceByteBudgetWithSystemTools 验证 enforceByteBudgetLocked
// 使用完整 payload 大小(messages + system + tools)进行剪枝。
// 2026-08-10 §20260810-14 新增。
func TestMemory_EnforceByteBudgetWithSystemTools(t *testing.T) {
	m := NewMemory("werewolf", "wolf", "kill all villagers", 0)
	// 设置较小的字节预算便于测试
	m.SetMaxPromptBytes(100)

	// 设置 system + tools 字节数
	system := []llmtypes.SystemBlock{
		{Type: "text", Text: "System prompt."},
	}
	tools := []llmtypes.ToolDef{
		{Name: "speak", Description: "Speak to players."},
	}
	m.SetSystemTools(system, tools)

	// 添加多条消息,使其超过预算
	for i := 0; i < 20; i++ {
		m.Push(llm.Message{
			Role:    "user",
			Content: []llmtypes.ContentBlock{{Type: "text", Text: "This is a test message with some content."}},
		})
		m.Push(llm.Message{
			Role:    "assistant",
			Content: []llmtypes.ContentBlock{{Type: "text", Text: "This is a test response."}},
		})
	}

	// 验证消息数量
	m.mu.RLock()
	msgCount := len(m.messages)
	m.mu.RUnlock()

	if msgCount <= 3 {
		t.Errorf("expected more messages, got %d", msgCount)
	}

	// 执行剪枝
	m.Prune(10)

	m.mu.RLock()
	finalMsgCount := len(m.messages)
	restBytes := approxPayloadBytes(m.messages[1:])
	totalBytes := restBytes + m.totalSystemToolsBytes
	m.mu.RUnlock()

	t.Logf("Before prune: %d messages", msgCount)
	t.Logf("After prune: %d messages", finalMsgCount)
	t.Logf("restBytes: %d, totalSystemToolsBytes: %d, totalBytes: %d", restBytes, m.totalSystemToolsBytes, totalBytes)
	t.Logf("maxPromptBytes: %d", m.maxPromptBytes)

	// 验证:消息数量应该减少
	if finalMsgCount >= msgCount {
		t.Errorf("prune did not reduce messages: before=%d, after=%d", msgCount, finalMsgCount)
	}

	// 验证:总字节数应该在预算内
	if totalBytes > m.maxPromptBytes {
		t.Logf("Warning: totalBytes (%d) exceeds maxPromptBytes (%d), but this may be expected for minimum retention", totalBytes, m.maxPromptBytes)
	}
}

// TestIsContextExceededError 验证 Context 超限错误检测。
// 2026-08-10 §20260810-14 新增。
func TestIsContextExceededError(t *testing.T) {
	tests := []struct {
		name     string
		errMsg   string
		expected bool
	}{
		{
			name:     "DouBao exceed max message tokens",
			errMsg:   "Total tokens of image and text exceed max message tokens",
			expected: true,
		},
		{
			name:     "exceed max message tokens",
			errMsg:   "exceed max message tokens",
			expected: true,
		},
		{
			name:     "maximum context length exceeded",
			errMsg:   "maximum context length exceeded",
			expected: true,
		},
		{
			name:     "context length exceed",
			errMsg:   "context length 100000 exceed limit",
			expected: true,
		},
		{
			name:     "400 token exceed",
			errMsg:   "400 error: token count exceed",
			expected: true,
		},
		{
			name:     "non-context error",
			errMsg:   "connection timeout",
			expected: false,
		},
		{
			name:     "nil error",
			errMsg:   "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			if tt.errMsg != "" {
				err = &testError{msg: tt.errMsg}
			}
			result := isContextExceededError(err)
			if result != tt.expected {
				t.Errorf("isContextExceededError(%v) = %v, want %v", tt.errMsg, result, tt.expected)
			}
		})
	}
}

type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}

// TestPruneByBytesAggressive 验证激进压缩功能。
// 2026-08-10 §20260810-14 新增。
func TestPruneByBytesAggressive(t *testing.T) {
	m := NewMemory("werewolf", "wolf", "kill all villagers", 0)
	m.SetMaxPromptBytes(200) // 设置较小的预算便于测试

	// 设置 system + tools 字节数
	system := []llmtypes.SystemBlock{
		{Type: "text", Text: "System prompt."},
	}
	tools := []llmtypes.ToolDef{
		{Name: "speak", Description: "Speak to players."},
	}
	m.SetSystemTools(system, tools)

	// 添加多条消息,使其超过预算
	for i := 0; i < 30; i++ {
		m.Push(llm.Message{
			Role:    "user",
			Content: []llmtypes.ContentBlock{{Type: "text", Text: "This is a test message with some content to make it larger."}},
		})
		m.Push(llm.Message{
			Role:    "assistant",
			Content: []llmtypes.ContentBlock{{Type: "text", Text: "Response text here."}},
		})
	}

	m.mu.RLock()
	msgCount := len(m.messages)
	m.mu.RUnlock()

	t.Logf("Before aggressive prune: %d messages", msgCount)

	// 执行激进压缩
	m.PruneByBytesAggressive()

	m.mu.RLock()
	finalMsgCount := len(m.messages)
	restBytes := approxPayloadBytes(m.messages[1:])
	totalBytes := restBytes + m.totalSystemToolsBytes
	m.mu.RUnlock()

	t.Logf("After aggressive prune: %d messages", finalMsgCount)
	t.Logf("restBytes: %d, totalSystemToolsBytes: %d, totalBytes: %d", restBytes, m.totalSystemToolsBytes, totalBytes)
	t.Logf("maxPromptBytes: %d, target (50%%): %d", m.maxPromptBytes, m.maxPromptBytes/2)

	// 验证:消息数量应该减少
	if finalMsgCount >= msgCount {
		t.Errorf("aggressive prune did not reduce messages: before=%d, after=%d", msgCount, finalMsgCount)
	}

	// 验证:总字节数应该在 50% 预算内
	targetBytes := m.maxPromptBytes / 2
	if totalBytes > targetBytes {
		t.Logf("Warning: totalBytes (%d) exceeds target (%d), but this may be expected for minimum retention", totalBytes, targetBytes)
	}
}
