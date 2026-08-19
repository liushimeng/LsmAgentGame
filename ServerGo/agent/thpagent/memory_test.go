package thpagent

import (
	"testing"
)

func TestMemory_AppendHand(t *testing.T) {
	m := NewMemory()
	for i := 0; i < 10; i++ {
		m.AppendHand(HandRecord{HandNumber: i + 1}, 5)
	}
	hands := m.RecentHandsSnapshot()
	if len(hands) != 5 {
		t.Errorf("expected 5 hands, got %d", len(hands))
	}
	if hands[0].HandNumber != 6 {
		t.Errorf("expected first hand number 6, got %d", hands[0].HandNumber)
	}
	if hands[4].HandNumber != 10 {
		t.Errorf("expected last hand number 10, got %d", hands[4].HandNumber)
	}
}

func TestMemory_OpponentStat(t *testing.T) {
	m := NewMemory()
	m.IncrementHandsPlayed("user1")
	m.UpdateOpponentStat("user1", 1, "fold")
	m.UpdateOpponentStat("user1", 1, "fold")
	m.UpdateOpponentStat("user1", 1, "call")
	m.UpdateOpponentStat("user1", 1, "raise")

	stat := m.OpponentStatSnapshot("user1")
	if stat == nil {
		t.Fatal("stat not found")
	}
	if stat.TotalFold != 2 || stat.TotalCall != 1 || stat.TotalRaise != 1 {
		t.Errorf("wrong counts: fold=%d call=%d raise=%d", stat.TotalFold, stat.TotalCall, stat.TotalRaise)
	}
	if stat.FoldRate != 0.5 {
		t.Errorf("expected foldRate=0.5, got %f", stat.FoldRate)
	}
}

func TestMemory_OpponentFoldRate_Default(t *testing.T) {
	m := NewMemory()
	rate := m.OpponentFoldRate("unknown-user")
	if rate != 0.5 {
		t.Errorf("default fold rate = %f, want 0.5", rate)
	}
}

func TestMemory_CurrentHandActions(t *testing.T) {
	m := NewMemory()
	m.RecordAction(ActionRecordForMemory{Seat: 1, ActionType: "call", Amount: 200})
	m.RecordAction(ActionRecordForMemory{Seat: 2, ActionType: "raise", Amount: 400})
	actions := m.CurrentHandActionsSnapshot()
	if len(actions) != 2 {
		t.Errorf("expected 2 actions, got %d", len(actions))
	}
	m.ResetCurrentHand()
	actions = m.CurrentHandActionsSnapshot()
	if len(actions) != 0 {
		t.Errorf("expected 0 actions after reset, got %d", len(actions))
	}
}

func TestMemory_DecisionSummary(t *testing.T) {
	m := NewMemory()
	m.SetLastDecisionSummary("fold: low hand strength")
	if m.GetLastDecisionSummary() != "fold: low hand strength" {
		t.Errorf("decision summary not set correctly")
	}
}

func TestMemory_AllOpponentStats(t *testing.T) {
	m := NewMemory()
	m.UpdateOpponentStat("user1", 1, "fold")
	m.UpdateOpponentStat("user2", 2, "raise")
	stats := m.AllOpponentStats()
	if len(stats) != 2 {
		t.Errorf("expected 2 stats, got %d", len(stats))
	}
}