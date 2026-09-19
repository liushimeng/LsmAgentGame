// Package debate — StartGame 启动即广播测试(2026-09-19 §20260919-辩论UI-v2 B-BE)。
//
// 根因回归:「开始比赛」按钮双击才生效的服务端根因是 StartGame 里
// r.SetPhase(PhasePreparation) 为裸调用,不触发 onPhaseChange WS 广播;
// 引擎 goroutine 的 runPreparationPhase 要等 preparation 倒计时结束
// 经 advanceTo(Opening) 才发出第一个 debate.phase 帧 —— preparation 全程
// 前端收不到任何阶段变化,表现为点击开始后零反馈。
//
// 修复:StartGame 在 SetPhase(PhasePreparation) 后立即 `go m.onPhaseChange(...)`
// (模式对齐 engine.go advanceTo)。
//
// 测试不依赖数据库与真实 LLM:NewDebateManager 无 gormDB(persistence 降级 no-op),
// agentStarter 未注入(StartGame 的 nil 分支跳过 Agent 启动)。
package debate

import (
	"testing"
	"time"
)

// TestStartGameBroadcastsPreparationPhase StartGame 成功路径必须在启动瞬间
// 触发 onPhaseChange(PhasePreparation) 广播,而非等引擎首次 advanceTo。
func TestStartGameBroadcastsPreparationPhase(t *testing.T) {
	mgr := NewDebateManager()
	cfg := RoomConfig{
		Topic:           DebateTopic{ID: "classic_001", Text: "人性本善", Type: "classic"},
		Mode:            ModeTwoTeam,
		PhaseConfig:     QuickPhaseConfig(),
		SpectatorConfig: DefaultSpectatorConfig(),
		Teams: []TeamConfig{
			{TeamID: 0, Stance: StancePro, Agents: []AgentConfig{
				{SeatID: 0, Role: RoleFirst, ModelKey: "m1"},
				{SeatID: 1, Role: RoleSecond, ModelKey: "m2"},
			}},
			{TeamID: 1, Stance: StanceCon, Agents: []AgentConfig{
				{SeatID: 0, Role: RoleFirst, ModelKey: "m3"},
				{SeatID: 1, Role: RoleSecond, ModelKey: "m4"},
			}},
		},
		Judges:    []JudgeConfig{{JudgeID: 0, ModelKey: "m1"}},
		CreatedBy: "user1",
	}
	room, e := mgr.CreateRoom(cfg)
	if e != nil {
		t.Fatalf("CreateRoom failed: %v", e.Message)
	}

	rec := &resultPhaseHookRecorder{}
	mgr.SetOnPhaseChange(rec.recordPhase)

	if e := mgr.StartGame(room.RoomID, "user1"); e != nil {
		t.Fatalf("StartGame failed: %v", e.Message)
	}
	// 收尾:停掉 StartGame 拉起的引擎 goroutine(runPreparationPhase/statsTicker),
	// 避免测试结束后泄漏(StopGame 会 cancel 引擎 ctx 并置 game_over)。
	defer mgr.StopGame(room.RoomID)

	// 同步断言:房间状态立即就位
	if room.Phase() != PhasePreparation {
		t.Errorf("阶段 = %s, want %s(StartGame 后应立即进入 preparation)", room.Phase(), PhasePreparation)
	}
	if !room.IsGameStarted() {
		t.Error("StartGame 后 IsGameStarted 应为 true")
	}

	// 异步断言:钩子以 `go fn(...)` 触发,轮询等待(引擎 Run 不会以
	// PhasePreparation 调 advanceTo,该广播只可能来自 StartGame 的补发)
	pollCondition(t, 2*time.Second, func() bool { return rec.phaseChangeCount(PhasePreparation) >= 1 },
		"StartGame 应立即广播 onPhaseChange(PhasePreparation)(裸 SetPhase 不触发广播的根因回归)")

	// 双击场景回归:二次 StartGame 被拒(game already started),不得再补发广播
	if e := mgr.StartGame(room.RoomID, "user1"); e == nil {
		t.Error("已启动的房间二次 StartGame 应返回错误")
	}
	time.Sleep(300 * time.Millisecond)
	if got := rec.phaseChangeCount(PhasePreparation); got != 1 {
		t.Errorf("PhasePreparation 广播次数 = %d, want 1(被拒的二次启动不得重复广播)", got)
	}
}
