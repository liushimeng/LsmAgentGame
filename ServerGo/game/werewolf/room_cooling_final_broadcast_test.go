// Package werewolf — room_cooling_final_broadcast_test.go: 验证
// 2026-07-30 解决和设计方案-20260730-03 的终局收编修复:
//
//	BUG-A: §129 冷却期入口必须先做「终局清场 + 终局广播」——
//	  (a) 所有 bot 的 LLMCallPhase 清场为 idle(quarantined 保留);
//	  (b) onGameOverBroadcast 回调被触发一次(phase=over,status=over);
//	BUG-B: Status=="over" 时 client view 的 judge_context.last_announcement
//	  必须被覆盖为终局文案,不再透传游戏内阶段宣告残留(「遗言阶段…」)。
package werewolf

import (
	"strings"
	"testing"
	"time"

	"LsmWebGame/agent/wwjudge"
	"LsmWebGame/agent/wwplayer"
	"LsmWebGame/agent/core"
)

// newCoolingTestRoom 创建一局 status=over + phase=game_over 的房间,
// 可注入 bot agents 与 judge,用于冷却期入口测试。
func newCoolingTestRoom(t *testing.T) (*WerewolfManager, *WerewolfRoom) {
	t.Helper()
	m := NewWerewolfManager()
	r := &WerewolfRoom{
		RoomID:    "test-room-cooling-final",
		chatQueue: agentcore.NewChatHistoryQueue(500 * 1024),
	}
	r.State = NewGame(time.Now().UnixNano())
	r.SeatCount = 7
	r.State.SeatCount = 7
	for i := 0; i < 7; i++ {
		uid := "u" + string(rune('0'+i))
		r.Seats[i] = uid
		r.State.Seats[i] = uid
		r.State.PlayerByID[uid] = Seat(i)
		r.State.Players[i] = Player{UserID: uid, Seat: Seat(i), Alive: false}
	}
	r.State.Status = "over"
	r.State.Winner = "good"
	r.State.Phase = PhaseGameOver
	r.gameStartedAt = time.Now().Unix()
	m.rooms[r.RoomID] = r
	return m, r
}

// TestCoolingEntry_ResetsBotLLMPhaseAndBroadcasts 验证 Fix-A1:
// 进入冷却期时 (a) bot LLM 相位清场为 idle;(b) onGameOverBroadcast 触发一次,
// 携带 roomID + winner。
func TestCoolingEntry_ResetsBotLLMPhaseAndBroadcasts(t *testing.T) {
	m, r := newCoolingTestRoom(t)

	// 注入两个 bot: 一个 streaming(应清场为 idle),一个 quarantined(应保留)。
	// 直接构造 &wwplayer.Agent{} 桩(wwplayer.New 需要真实 LLM registry,
	// 测试环境 config 缺失会 panic — 与 room_round194_test.go 同模式)。
	ag0 := &wwplayer.Agent{Seat: 0, ModelKey: "TestModel-A"}
	ag0.SetLLMCallPhase(wwplayer.PhaseStreaming)
	ag1 := &wwplayer.Agent{Seat: 1, ModelKey: "TestModel-B"}
	ag1.SetLLMCallPhase(wwplayer.PhaseQuarantined)
	r.BotAgents = map[int]*wwplayer.Agent{0: ag0, 1: ag1}

	// 注册终局广播回调。
	var gotRoom, gotWinner string
	var calls int
	m.SetOnGameOverBroadcast(func(roomID, winner string) {
		calls++
		gotRoom = roomID
		gotWinner = winner
	})

	r.mu.Lock()
	entered := m.tryEnterCoolingFromGameOverLocked(r)
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		if r.coolingCancel != nil {
			r.coolingCancel()
			r.coolingCancel = nil
		}
		r.mu.Unlock()
	}()

	if !entered {
		t.Fatal("tryEnterCoolingFromGameOverLocked 应成功进入冷却期(cooling_sec 默认 1800)")
	}
	if got := ag0.LLMCallPhase(); got != wwplayer.PhaseIdle {
		t.Fatalf("streaming bot 应被清场为 idle, 实际 %q", got)
	}
	if got := ag1.LLMCallPhase(); got != wwplayer.PhaseQuarantined {
		t.Fatalf("quarantined bot 相位应保留, 实际 %q", got)
	}
	if calls != 1 {
		t.Fatalf("onGameOverBroadcast 应触发 1 次, 实际 %d", calls)
	}
	if gotRoom != r.RoomID {
		t.Fatalf("广播 roomID = %q, 期望 %q", gotRoom, r.RoomID)
	}
	if gotWinner != "good" {
		t.Fatalf("广播 winner = %q, 期望 good", gotWinner)
	}
}

// TestBuildClientState_GameOverOverridesJudgeAnnounce 验证 Fix-A2:
// Status=="over" 时 judge_context.last_announcement 被覆盖为终局文案,
// 不再透传「遗言阶段。…」等游戏内阶段宣告残留。
func TestBuildClientState_GameOverOverridesJudgeAnnounce(t *testing.T) {
	m, r := newCoolingTestRoom(t)
	_ = m

	j := wwjudge.NewAgentJudge("test-room-cooling-final", "JudgeModel")
	// 模拟游戏内最后一条阶段宣告残留(终局时法官 LLM 熔断,game_over
	// 宣告未能生成,last_announcement 仍停留在 death_lyric)。
	j.RecordAnnouncement("遗言阶段。刚刚被投出的玩家,请留下你的最后一句话。")
	r.judge = j

	cs := BuildClientStateWithRoom(r.RoomID, r, -1)
	if cs == nil {
		t.Fatal("BuildClientStateWithRoom 返回 nil")
	}
	if cs.Status != "over" {
		t.Fatalf("cs.Status = %q, 期望 over", cs.Status)
	}
	if cs.JudgeContext == nil {
		t.Fatal("judge_context 不应为 nil")
	}
	ann := cs.JudgeContext.LastAnnouncement
	if strings.Contains(ann, "遗言阶段") {
		t.Fatalf("终局后法官语仍透传阶段宣告残留: %q", ann)
	}
	if !strings.Contains(ann, "对局结束") || !strings.Contains(ann, "good") {
		t.Fatalf("终局法官语应含「对局结束」与胜方, 实际 %q", ann)
	}
}

// TestBuildClientState_InGameKeepsJudgeAnnounce 对照组: 游戏进行中
// (status=playing) 时法官语必须原样透传,Fix-A2 不得误伤。
func TestBuildClientState_InGameKeepsJudgeAnnounce(t *testing.T) {
	m, r := newCoolingTestRoom(t)
	_ = m
	r.State.Status = "playing"
	r.State.Phase = PhaseDeathLyric

	j := wwjudge.NewAgentJudge("test-room-cooling-final", "JudgeModel")
	j.RecordAnnouncement("遗言阶段。刚刚被投出的玩家,请留下你的最后一句话。")
	r.judge = j

	cs := BuildClientStateWithRoom(r.RoomID, r, -1)
	if cs == nil || cs.JudgeContext == nil {
		t.Fatal("cs/judge_context 不应为 nil")
	}
	if !strings.Contains(cs.JudgeContext.LastAnnouncement, "遗言阶段") {
		t.Fatalf("游戏进行中法官语应原样透传, 实际 %q", cs.JudgeContext.LastAnnouncement)
	}
}
