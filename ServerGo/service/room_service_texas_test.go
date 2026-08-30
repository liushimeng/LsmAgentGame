package service

import (
	"context"
	"strings"
	"testing"

	"LsmAgentGame/config"
	"LsmAgentGame/errcode"
)

// 2026-08-19 §德州扑克盲注透传 — CreateRoomWithAgents 的 big_blind/start_stack
// 校验回归测试。所有用例都在 DB 写入之前 fail-fast,因此 nil db 即可。

func newTexasCfgSvc() *RoomService {
	return &RoomService{cfg: &config.Config{}}
}

// TestTexasCfg_001_InvalidBigBlind: big_blind 不在 {10,50,200,1000,5000} 白名单。
func TestTexasCfg_001_InvalidBigBlind(t *testing.T) {
	s := newTexasCfgSvc()
	_, err := s.CreateRoomWithAgents(context.Background(), "texasholdem", "user-x", "test-room",
		nil, nil, "", nil, "", &TexasTableConfig{BigBlind: 30, StartStack: 1500}, nil)
	if err == nil || err.Code != errcode.ErrValidationFailed {
		t.Fatalf("expected ErrValidationFailed, got %v", err)
	}
}

// TestTexasCfg_002_StackOutOfRange: start_stack 超出 [20bb,100bb] 区间。
func TestTexasCfg_002_StackOutOfRange(t *testing.T) {
	s := newTexasCfgSvc()
	// 低于下限: 500 < 20*50=1000
	_, err := s.CreateRoomWithAgents(context.Background(), "texasholdem", "user-x", "test-room",
		nil, nil, "", nil, "", &TexasTableConfig{BigBlind: 50, StartStack: 500}, nil)
	if err == nil || err.Code != errcode.ErrValidationFailed {
		t.Fatalf("below-min: expected ErrValidationFailed, got %v", err)
	}
	// 高于上限: 50001 > 100*50=5000
	_, err = s.CreateRoomWithAgents(context.Background(), "texasholdem", "user-x", "test-room",
		nil, nil, "", nil, "", &TexasTableConfig{BigBlind: 50, StartStack: 5001}, nil)
	if err == nil || err.Code != errcode.ErrValidationFailed {
		t.Fatalf("above-max: expected ErrValidationFailed, got %v", err)
	}
}

// TestTexasCfg_003_NonTexasGameRejected: 非 texasholdem 游戏设置盲注/买入 → 400。
func TestTexasCfg_003_NonTexasGameRejected(t *testing.T) {
	s := newTexasCfgSvc()
	_, err := s.CreateRoomWithAgents(context.Background(), "xiangqi", "user-x", "test-room",
		nil, nil, "", nil, "", &TexasTableConfig{BigBlind: 50, StartStack: 2500}, nil)
	if err == nil || err.Code != errcode.ErrValidationFailed {
		t.Fatalf("expected ErrValidationFailed, got %v", err)
	}
	if !strings.Contains(err.Message, "texasholdem") {
		t.Fatalf("error message should mention texasholdem, got %q", err.Message)
	}
}

// TestTexasCfg_004_PartialConfigRejected: 两字段必须同时设置,只给一个 → 400。
func TestTexasCfg_004_PartialConfigRejected(t *testing.T) {
	s := newTexasCfgSvc()
	_, err := s.CreateRoomWithAgents(context.Background(), "texasholdem", "user-x", "test-room",
		nil, nil, "", nil, "", &TexasTableConfig{BigBlind: 50}, nil)
	if err == nil || err.Code != errcode.ErrValidationFailed {
		t.Fatalf("bb-only: expected ErrValidationFailed, got %v", err)
	}
	_, err = s.CreateRoomWithAgents(context.Background(), "texasholdem", "user-x", "test-room",
		nil, nil, "", nil, "", &TexasTableConfig{StartStack: 2500}, nil)
	if err == nil || err.Code != errcode.ErrValidationFailed {
		t.Fatalf("stack-only: expected ErrValidationFailed, got %v", err)
	}
}
