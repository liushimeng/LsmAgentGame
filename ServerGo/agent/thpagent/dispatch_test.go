package thpagent

import (
	"testing"
	"time"
)

func TestDispatcher_PokerAction_OnePerRound(t *testing.T) {
	d := NewDispatcher()

	// 第 1 次 — 通过
	_, err := d.DispatchPokerAction(Action{Type: ActFold, Thought: "weak hand"})
	if err != nil {
		t.Errorf("first poker_action should succeed, got: %v", err)
	}

	// 第 2 次 — 拒绝
	_, err = d.DispatchPokerAction(Action{Type: ActCall, Thought: "changing mind"})
	if err != ErrTooManyPokerActions {
		t.Errorf("second poker_action should fail with ErrTooManyPokerActions, got: %v", err)
	}
}

func TestDispatcher_PokerAction_ActionTypes(t *testing.T) {
	d := NewDispatcher()

	tests := []struct {
		name      string
		action    Action
		expectErr bool
	}{
		{"fold", Action{Type: ActFold}, false},
		{"check", Action{Type: ActCheck}, false},
		{"call", Action{Type: ActCall}, false},
		{"bet positive", Action{Type: ActBet, Amount: 200}, false},
		{"bet negative", Action{Type: ActBet, Amount: -100}, true},
		{"raise positive", Action{Type: ActRaise, Amount: 400}, false},
		{"raise negative", Action{Type: ActRaise, Amount: -50}, true},
		{"allin positive", Action{Type: ActAllIn, Amount: 1000}, false},
		{"allin zero", Action{Type: ActAllIn, Amount: 0}, true},
		{"unknown", Action{Type: "fake"}, true},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d.OnNewRound() // 重置
			_, err := d.DispatchPokerAction(tt.action)
			if tt.expectErr && err == nil {
				t.Errorf("case %d: expected error, got nil", i)
			}
			if !tt.expectErr && err != nil {
				t.Errorf("case %d: expected no error, got %v", i, err)
			}
		})
	}
}

func TestDispatcher_PokerChat_LimitPerHand(t *testing.T) {
	d := NewDispatcher()
	d.minChatIntervalSec = 0 // 测试时关掉时间限制

	for i := 1; i <= 2; i++ {
		err := d.DispatchPokerChat("test")
		if err != nil {
			t.Errorf("chat %d should succeed, got: %v", i, err)
		}
	}

	err := d.DispatchPokerChat("third")
	if err != ErrTooManyChat {
		t.Errorf("third chat should fail with ErrTooManyChat, got: %v", err)
	}
}

func TestDispatcher_PokerChat_IntervalLimit(t *testing.T) {
	d := NewDispatcher()

	// 第 1 条无间隔
	err := d.DispatchPokerChat("first")
	if err != nil {
		t.Errorf("first chat should succeed: %v", err)
	}

	// 第 2 条间隔过短(< 30s) — 失败
	err = d.DispatchPokerChat("second")
	if err != ErrChatIntervalTooShort {
		t.Errorf("second chat should fail with ErrChatIntervalTooShort, got: %v", err)
	}

	// 模拟时间已过 30s
	d.chatLastTimestamp = time.Now().Add(-31 * time.Second)
	err = d.DispatchPokerChat("third")
	if err != nil {
		t.Errorf("third chat (after 31s) should succeed, got: %v", err)
	}
}

func TestDispatcher_OnNewHand_ResetsChat(t *testing.T) {
	d := NewDispatcher()
	d.minChatIntervalSec = 0

	// 第 1 手牌用满 2 次
	d.DispatchPokerChat("1")
	d.DispatchPokerChat("2")
	if d.ChatCountThisHand() != 2 {
		t.Errorf("expected chatCount=2, got %d", d.ChatCountThisHand())
	}

	// 新一手牌重置
	d.OnNewHand()
	if d.ChatCountThisHand() != 0 {
		t.Errorf("expected chatCount=0 after OnNewHand, got %d", d.ChatCountThisHand())
	}
}

func TestDispatcher_OnNewRound_ResetsAction(t *testing.T) {
	d := NewDispatcher()

	d.DispatchPokerAction(Action{Type: ActFold})
	if !d.IsPokerActionTaken() {
		t.Error("expected IsPokerActionTaken=true after action")
	}

	d.OnNewRound()
	if d.IsPokerActionTaken() {
		t.Error("expected IsPokerActionTaken=false after OnNewRound")
	}
}