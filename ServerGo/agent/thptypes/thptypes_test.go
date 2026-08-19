package thptypes

import (
	"testing"
)

func TestBuildEmptyContext(t *testing.T) {
	gc := BuildEmptyContext("room1", "user1", "MeiTuan-model", 2)
	if gc.RoomID != "room1" {
		t.Errorf("RoomID = %q, want %q", gc.RoomID, "room1")
	}
	if gc.GameKind != "texasholdem" {
		t.Errorf("GameKind = %q, want %q", gc.GameKind, "texasholdem")
	}
	if gc.MySeat != 2 {
		t.Errorf("MySeat = %d, want 2", gc.MySeat)
	}
	if gc.ModelKey != "MeiTuan-model" {
		t.Errorf("ModelKey = %q, want %q", gc.ModelKey, "MeiTuan-model")
	}
	if gc.BotIdentity.AgentClass != "LsmAgentGame-TexasHoldem-Player" {
		t.Errorf("AgentClass = %q, want %q", gc.BotIdentity.AgentClass, "LsmAgentGame-TexasHoldem-Player")
	}
	if gc.Opponents == nil {
		t.Error("Opponents should not be nil (avoid JSON null crash)")
	}
	if gc.ActionHistory == nil {
		t.Error("ActionHistory should not be nil")
	}
}

func TestGameContext_NilSafety(t *testing.T) {
	gc := &GameContext{}
	// GameContext 零值时切片为 nil,但 BuildEmptyContext 应返回非 nil 切片
	// 这对应了 view.go 的 BUG-TEXAS-HOLE-NULL 教训 — JSON 序列化 nil 会输出 null,
	// 前端 null.length 会崩溃。
	gc2 := BuildEmptyContext("r1", "u1", "m1", 0)
	if gc2.Opponents == nil {
		t.Error("BuildEmptyContext.Opponents should not be nil")
	}
	if gc2.ActionHistory == nil {
		t.Error("BuildEmptyContext.ActionHistory should not be nil")
	}
	if gc2.RecentHands == nil {
		t.Error("BuildEmptyContext.RecentHands should not be nil")
	}
	_ = gc // direct zero-value is allowed to have nil slices, but client-facing paths must use BuildEmptyContext
}