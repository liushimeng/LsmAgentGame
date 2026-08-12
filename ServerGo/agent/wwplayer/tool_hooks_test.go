package wwplayer

import (
	"fmt"
	"testing"
)

func TestSteeringQueue_BasicEnqueueDrain(t *testing.T) {
	q := NewSteeringQueue(5)
	defer q.Close()

	// 空队列 drain
	msgs := q.Drain()
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages, got %d", len(msgs))
	}

	// 入队 3 条
	q.Enqueue(AgentSteerMsg{Kind: SteerSpectatorInquiry, Content: "hello"})
	q.Enqueue(AgentSteerMsg{Kind: SteerPropHit, Content: "you were hit"})
	q.Enqueue(AgentSteerMsg{Kind: SteerPhaseHint, Content: "time is running out"})

	if q.Len() != 3 {
		t.Fatalf("expected 3 messages, got %d", q.Len())
	}

	// drain 全部
	msgs = q.Drain()
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages after drain, got %d", len(msgs))
	}
	if msgs[0].Kind != SteerSpectatorInquiry {
		t.Errorf("expected first msg kind spectator_inquiry, got %s", msgs[0].Kind)
	}

	// drain 后应为空
	msgs = q.Drain()
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages after second drain, got %d", len(msgs))
	}
}

func TestSteeringQueue_Overflow(t *testing.T) {
	q := NewSteeringQueue(3)
	defer q.Close()

	// 写入超过容量
	for i := 0; i < 10; i++ {
		q.Enqueue(AgentSteerMsg{Kind: SteerPhaseHint, Content: fmt.Sprintf("msg %d", i)})
	}

	// 应该保留最后 3 条 (FIFO, 丢弃最旧)
	msgs := q.Drain()
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages after overflow, got %d", len(msgs))
	}

	dropCount := q.DropCount()
	if dropCount < 5 {
		t.Errorf("expected at least 5 drops, got %d", dropCount)
	}
}

func TestSteeringQueue_DrainAndFormat(t *testing.T) {
	q := NewSteeringQueue(10)
	defer q.Close()

	// 空队列
	text := q.DrainAndFormat()
	if text != "" {
		t.Errorf("expected empty text, got %q", text)
	}

	// 有消息
	q.Enqueue(AgentSteerMsg{Kind: SteerSpectatorInquiry, Content: "who is wolf?"})
	q.Enqueue(AgentSteerMsg{Kind: SteerPropHit, Content: "markdown_bomb hit"})

	text = q.DrainAndFormat()
	if text == "" {
		t.Fatal("expected non-empty text")
	}
	if !containsStr(text, "观众提问") {
		t.Errorf("expected '观众提问' in text, got %q", text)
	}
	if !containsStr(text, "道具影响") {
		t.Errorf("expected '道具影响' in text, got %q", text)
	}
}

func TestToolHooks_BeforeBlocksExecution(t *testing.T) {
	hooks := NewToolHooks()
	blockingHook := func(ctx *ToolHookContext) error {
		if ctx.ToolName == "forbidden_tool" {
			return fmt.Errorf("tool %s is forbidden", ctx.ToolName)
		}
		return nil
	}
	hooks.Before = append(hooks.Before, blockingHook)

	// 应该被阻止
	err := hooks.RunBefore(&ToolHookContext{ToolName: "forbidden_tool"})
	if err == nil {
		t.Fatal("expected error from blocking hook")
	}

	// 应该通过
	err = hooks.RunBefore(&ToolHookContext{ToolName: "speak"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestQuotaHookWrapper(t *testing.T) {
	qw := &QuotaHookWrapper{MaxTools: 2}
	hook := qw.Hook()

	// 前 2 次应该通过
	for i := 0; i < 2; i++ {
		err := hook(&ToolHookContext{ToolName: "speak"})
		if err != nil {
			t.Fatalf("round %d: unexpected error: %v", i, err)
		}
	}

	// 第 3 次应该被阻止
	err := hook(&ToolHookContext{ToolName: "speak"})
	if err == nil {
		t.Fatal("expected quota exceeded error")
	}

	// 重置后应该重新通过
	qw.Reset()
	err = hook(&ToolHookContext{ToolName: "speak"})
	if err != nil {
		t.Fatalf("after reset: unexpected error: %v", err)
	}
}

func TestPhaseConfig_AllPhasesHaveConfig(t *testing.T) {
	// 验证所有活跃阶段都有配置
	expectedPhases := []string{
		"pre_wolves", "night_guard", "night_wolves", "night_seer", "night_witch", "night_demon_hunter",
		"dawn", "speak", "vote", "sheriff",
		"hunter_shoot", "death_lyric", "idiot_reveal", "restart_vote",
	}

	for _, phase := range expectedPhases {
		cfg := GetPhaseConfig(phase)
		if cfg == nil {
			t.Errorf("phase %q has no config", phase)
			continue
		}
		if cfg.SkipAction == "" {
			t.Errorf("phase %q has no SkipAction", phase)
		}
		if len(cfg.ToolKeys) == 0 {
			t.Errorf("phase %q has no ToolKeys", phase)
		}
	}
}

func TestPhaseConfig_SkipActionConsistency(t *testing.T) {
	// 验证所有阶段的 SkipAction 都在 ToolKeys 中 (除了 idle_silent)
	for phase, cfg := range GetAllPhaseConfigs() {
		if cfg.SkipAction == "" {
			continue
		}
		found := false
		for _, key := range cfg.ToolKeys {
			if key == cfg.SkipAction {
				found = true
				break
			}
		}
		if !found && cfg.SkipAction != "idle_silent" {
			t.Errorf("phase %q: SkipAction %q not in ToolKeys %v", phase, cfg.SkipAction, cfg.ToolKeys)
		}
	}
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
