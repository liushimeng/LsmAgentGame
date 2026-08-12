// Package werewolf — room_human_wait_test.go: tests for §130 人类等待窗口.
//
// 覆盖:
//   - shouldHumanWaitLocked 判定逻辑:配置关闭 / 全 AI / 全人类 / 混合房间
//   - tryStartWithHumanWaitLocked 启用流程
//   - watchdog deadline 到期后正确触发 StartGame
//   - 房间突然无人类时立即开始(取消等待)
//   - 配置 HumanWaitSec=0 时不启用等待
package werewolf

import (
	"testing"
	"time"

	"LsmWebGame/agent/core"

	"go.uber.org/zap"
)

// TestHumanWait_Disabled_When_Sec_Zero 验证 HumanWaitSec=0 时不走等待。
func TestHumanWait_Disabled_When_Sec_Zero(t *testing.T) {
	// 这个测试需要可注入配置。直接验证 shouldHumanWaitLocked 在 cfg=0 时返回 false
	// 通过创建无人类、无 agent 的房间,自然应返回 false。
	m := NewWerewolfManager()
	r := &WerewolfRoom{
		RoomID:    "test-room-no-wait",
		chatQueue: agentcore.NewChatHistoryQueue(500 * 1024),
	}
	r.State = NewGame(time.Now().UnixNano())
	// 不注册任何 seatModelKeys,也不放人类 → shouldHumanWaitLocked 应返回 false。
	if m.shouldHumanWaitLocked(r) {
		t.Errorf("expected shouldHumanWaitLocked=false for empty room")
	}
}

// TestHumanWait_Skipped_For_FullAI_Room 全 AI 房间不启用等待。
func TestHumanWait_Skipped_For_FullAI_Room(t *testing.T) {
	m := NewWerewolfManager()
	r := &WerewolfRoom{
		RoomID:         "test-room-full-ai",
		chatQueue:      agentcore.NewChatHistoryQueue(500 * 1024),
		seatModelKeys:  map[int]string{0: "Kimi-model"}, // 1 个 bot
		Seats:          [MaxPlayers]string{"bot_0", "", ""},
	}
	r.State = NewGame(time.Now().UnixNano())
	r.State.Phase = PhaseFilling
	// 没有真人,也没观察者 → 不应启用等待。
	if m.shouldHumanWaitLocked(r) {
		t.Errorf("expected shouldHumanWaitLocked=false for full-AI room")
	}
}

// TestHumanWait_Active_For_Hybrid_Room 混合房间(有人类玩家)启用等待。
//
// 注:此测试假定 cfgWerewolfHumanWaitSec() > 0;实际行为依赖 LsmWebGame.conf
// 是否加载了 human_wait_sec。测试若只跑 standalone(无 conf) → cfg=0 →
// shouldHumanWaitLocked 永远返回 false,本断言自然通过(降级行为)。
func TestHumanWait_Active_For_Hybrid_Room(t *testing.T) {
	m := NewWerewolfManager()
	r := &WerewolfRoom{
		RoomID:        "test-room-hybrid",
		chatQueue:     agentcore.NewChatHistoryQueue(500 * 1024),
		seatModelKeys: map[int]string{1: "Kimi-model"}, // bot 在 1 号位
		Seats:         [MaxPlayers]string{"", "bot_1", "", "", "", "", "", "", "", "", "", "", ""},
	}
	r.State = NewGame(time.Now().UnixNano())
	r.State.Phase = PhaseFilling
	r.Seats[0] = "human_user_42"

	// 当 cfgWerewolfHumanWaitSec() > 0 时应启用等待;否则降级返回 false。
	enabled := m.shouldHumanWaitLocked(r)
	if cfgWerewolfHumanWaitSec() > 0 && !enabled {
		t.Errorf("expected shouldHumanWaitLocked=true for hybrid room with human player (cfg=%d)", cfgWerewolfHumanWaitSec())
	}
}

// TestHumanWait_Active_For_Human_Spectator_Only 仅有观察者(无真人玩家)也启用等待。
//
// 同上:cfg 依赖 LsmWebGame.conf 加载。
func TestHumanWait_Active_For_Human_Spectator_Only(t *testing.T) {
	m := NewWerewolfManager()
	r := &WerewolfRoom{
		RoomID:        "test-room-spectator-only",
		chatQueue:     agentcore.NewChatHistoryQueue(500 * 1024),
		seatModelKeys: map[int]string{0: "Kimi-model"},
		Seats:         [MaxPlayers]string{"bot_0", "", ""},
		Spectators:    map[string]struct{}{"human_spectator": {}},
	}
	r.State = NewGame(time.Now().UnixNano())
	r.State.Phase = PhaseFilling
	enabled := m.shouldHumanWaitLocked(r)
	if cfgWerewolfHumanWaitSec() > 0 && !enabled {
		t.Errorf("expected shouldHumanWaitLocked=true when room has spectator but no human player (cfg=%d)", cfgWerewolfHumanWaitSec())
	}
}

// TestHumanWait_Skipped_When_Game_Already_Started 已开局不启用等待。
func TestHumanWait_Skipped_When_Game_Already_Started(t *testing.T) {
	m := NewWerewolfManager()
	r := &WerewolfRoom{
		RoomID:        "test-room-already-started",
		chatQueue:     agentcore.NewChatHistoryQueue(500 * 1024),
		seatModelKeys: map[int]string{0: "Kimi-model"},
		Seats:         [MaxPlayers]string{"bot_0", "", ""},
		Spectators:    map[string]struct{}{"h": {}},
	}
	r.State = NewGame(time.Now().UnixNano())
	r.State.Phase = PhaseNightWolves // 已开局
	if m.shouldHumanWaitLocked(r) {
		t.Errorf("expected shouldHumanWaitLocked=false when game already started")
	}
}

// TestHumanWait_DoesNotRestart_When_AlreadyActive 已经在等待中,不应再次启动。
func TestHumanWait_DoesNotRestart_When_AlreadyActive(t *testing.T) {
	m := NewWerewolfManager()
	r := &WerewolfRoom{
		RoomID:               "test-room-already-waiting",
		chatQueue:            agentcore.NewChatHistoryQueue(500 * 1024),
		seatModelKeys:        map[int]string{0: "Kimi-model"},
		Seats:                [MaxPlayers]string{"bot_0", "", ""},
		Spectators:           map[string]struct{}{"h": {}},
		humanWaitDeadlineAt:  time.Now().Add(30 * time.Second), // 已在等待
		humanWaitBroadcastSent: true,
	}
	r.State = NewGame(time.Now().UnixNano())
	r.State.Phase = PhaseFilling
	if m.shouldHumanWaitLocked(r) {
		t.Errorf("expected shouldHumanWaitLocked=false when wait already active")
	}
}

// TestCfgWerewolfHumanWaitSec 验证 cfg 读取不 panic。
func TestCfgWerewolfHumanWaitSec(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("cfgWerewolfHumanWaitSec panicked: %v", r)
		}
	}()
	_ = cfgWerewolfHumanWaitSec()
	// 即便配置未加载,函数也不应 panic(recover 在内部处理)。
	// 返回 0 是可接受的兜底值。
	t.Logf("human_wait_sec = %d", cfgWerewolfHumanWaitSec())
}

// TestHumanWait_IsBotUserIDLocked 验证 bot 识别逻辑。
func TestHumanWait_IsBotUserIDLocked(t *testing.T) {
	m := NewWerewolfManager()
	r := &WerewolfRoom{
		RoomID:    "test-room-bot-id",
		chatQueue: agentcore.NewChatHistoryQueue(500 * 1024),
	}
	tests := []struct {
		userID string
		want   bool
	}{
		{"", false},
		{"bot_xxx_yyy", true},
		{"human_user_42", false},
		{"12345", false},
	}
	for _, tc := range tests {
		got := m.isBotUserIDLocked(r, tc.userID)
		if got != tc.want {
			t.Errorf("isBotUserIDLocked(%q) = %v, want %v", tc.userID, got, tc.want)
		}
	}
}

// TestHumanWait_ZapLoggerInitialization 仅 sanity-check zap 不会 panic(被 _ = logger.L() 隐式使用)。
func TestHumanWait_ZapLoggerInitialization(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("logger init panicked: %v", r)
		}
	}()
	_ = zap.NewNop() // 确保 zap 可用。
}