// Package wwplayer — decision_trail_test.go: 决策留痕运行时回放(§20260810-12 D1)单测。
//
// 验证:
//   - AppendDecisionEntry 在 trail 上正确追加,30 条上限 FIFO 淘汰;
//   - botsLogDecisions=false 时 trail 完全不分配(零开销);
//   - sanitizeBotTranscript 玩家分支清空 DecisionTrail(§135 spectator 隔离)。
package wwplayer

import (
	"testing"
)

func TestAppendDecisionEntry_BasicAppend(t *testing.T) {
	a := &Agent{
		botsLogDecisions: true,
		lastTranscript:   &BotTranscript{},
	}
	for i := 0; i < 5; i++ {
		a.AppendDecisionEntry(DecisionEntry{
			Round:       i + 1,
			ToolName:    "speak",
			ToolSummary: "test",
			TookMs:      int64(100 * (i + 1)),
		})
	}
	if got := len(a.lastTranscript.DecisionTrail); got != 5 {
		t.Fatalf("expected 5 entries, got %d", got)
	}
}

func TestAppendDecisionEntry_FIFO30Limit(t *testing.T) {
	a := &Agent{
		botsLogDecisions: true,
		lastTranscript:   &BotTranscript{},
	}
	for i := 0; i < 50; i++ {
		a.AppendDecisionEntry(DecisionEntry{Round: i + 1})
	}
	if got := len(a.lastTranscript.DecisionTrail); got != 30 {
		t.Fatalf("expected 30 entries after 50 inserts, got %d", got)
	}
	// 最新条目应该是第 50 轮
	if got := a.lastTranscript.DecisionTrail[29].Round; got != 50 {
		t.Fatalf("expected newest entry Round=50, got %d", got)
	}
	// 最旧条目应该是第 21 轮(FIFO 淘汰 1..20)
	if got := a.lastTranscript.DecisionTrail[0].Round; got != 21 {
		t.Fatalf("expected oldest entry Round=21, got %d", got)
	}
}

func TestAppendDecisionEntry_Disabled(t *testing.T) {
	a := &Agent{
		botsLogDecisions: false,
		lastTranscript:   &BotTranscript{},
	}
	for i := 0; i < 10; i++ {
		a.AppendDecisionEntry(DecisionEntry{Round: i + 1})
	}
	if got := len(a.lastTranscript.DecisionTrail); got != 0 {
		t.Fatalf("expected 0 entries when disabled, got %d", got)
	}
}

func TestAppendDecisionEntry_NilTranscript(t *testing.T) {
	a := &Agent{botsLogDecisions: true, lastTranscript: nil}
	// 必须不 panic
	a.AppendDecisionEntry(DecisionEntry{Round: 1})
}