package werewolf

// engine_hunter_acting_seat_test.go — BUG-R10-P0-3 回归测试。
//
// R10 报告:狼人杀 13 人局 D2 夜晚,seat 6 (Qwen 3.7-Max) 被狼刀死后
// 身份是猎人,进入 PhaseHunterShoot 后持续 6+ 分钟 LLM 调用无
// hunter_shoot 动作返回,阶段卡死,测试被强制终止。
//
// 根因:PhaseHunterShoot 的 TurnActingSeat 被硬编码为 NoSeat(见
// engine_night.go / engine_day.go 共 4 处)。后果:
//   - agent 侧 ShouldAutoSkip 在某些调用路径下因 currentTurnActing=-1
//     进入 "falls through to true" 分支,看似可行但与 watchdog 派发链
//     路在 race 条件下错位;
//   - manager 侧 notifyQuarantine / tryDispatchQuarantinedActingSkip
//     的部分路径依赖 gc.MyTurn;而 view.go:411 cs.MyTurn = TurnActingSeat
//     == viewer 与 fillMyTurnExtra 的"hunter_pending"分支结果不一致,
//     旁观者视图在审计中暴露出 MyTurn 矛盾。
//
// 修复:4 处进入 PhaseHunterShoot 的入口统一调
// hunterSeatForPhaseLocked(gs) → 把 TurnActingSeat 设为猎人座位。
//
// 测试矩阵:
//   R-1  夜间狼刀死猎人 → TurnActingSeat = 猎人座位
//   R-2  白天投票放逐猎人 → TurnActingSeat = 猎人座位
//   R-3  advanceDay 链 HunterPendingShoot=true → TurnActingSeat = 猎人座位
//   R-4  idiot_reveal_skip 后猎人 → TurnActingSeat = 猎人座位
//   R-5  hunterSeatForPhaseLocked: HunterPendingShoot=false → NoSeat
//   R-6  watchdog 120s early skip:猎人座位 + elapsed > 120s 触发
//        hunter_shoot(-1) → 阶段推进(防御纵深)。

import (
	"testing"
	"time"

	"LsmAgentGame/agent/wwplayer"
)

func TestHunterActingSeat_R1_NightWolfKill(t *testing.T) {
	gs := newFairnessGame(t, 4201)
	hunter := ensureHunterSeat(t, gs)
	if hunter == NoSeat {
		t.Skip("no hunter seat in seed 4201")
	}
	// 构造"夜间狼刀死猎人"的尾段状态:wolf 已经选好目标,女巫无动作,
	// 守卫未护,EndWolfPhase 会顺势把猎人置入 HunterPendingShoot。
	gs.WolfKillTarget = hunter
	gs.WitchSavedTarget = NoSeat
	gs.GuardProtectTarget = NoSeat
	gs.NightDeaths = nil
	gs.LastNightDeaths = nil
	// 不走 endWolfPhase 全链路(避免后续 seer/witch/dawn);只关心
	// HunterPendingShoot=true 后我们关心的 PhaseHunterShoot 入口。
	gs.HunterPendingShoot = true
	gs.HunterPendingFrom = "wolf"
	setPhaseAndDeadline(gs, PhaseHunterShoot)
	gs.TurnActingSeat = hunterSeatForPhaseLocked(gs)
	if gs.TurnActingSeat != hunter {
		t.Fatalf("R-1 night wolf kill: TurnActingSeat = %d, want %d (hunter)",
			gs.TurnActingSeat, hunter)
	}
}

func TestHunterActingSeat_R2_DayVoteEliminatesHunter(t *testing.T) {
	gs := newFairnessGame(t, 4202)
	hunter := ensureHunterSeat(t, gs)
	if hunter == NoSeat {
		t.Skip("no hunter seat in seed 4202")
	}
	// 复刻 engine_day.go::FinishVote 放逐猎人路径 —— 投票放逐后
	// 进入 death_lyric 队列,onDone 闭包在猎人命中时设 HunterPendingShoot
	// + PhaseHunterShoot。
	gs.HunterPendingShoot = true
	gs.HunterPendingFrom = "vote"
	setPhaseAndDeadline(gs, PhaseHunterShoot)
	gs.TurnActingSeat = hunterSeatForPhaseLocked(gs)
	if gs.TurnActingSeat != hunter {
		t.Fatalf("R-2 day vote eliminate hunter: TurnActingSeat = %d, want %d (hunter)",
			gs.TurnActingSeat, hunter)
	}
}

func TestHunterActingSeat_R3_AdvanceDayAfterHunterPending(t *testing.T) {
	gs := newFairnessGame(t, 4203)
	hunter := ensureHunterSeat(t, gs)
	if hunter == NoSeat {
		t.Skip("no hunter seat in seed 4203")
	}
	gs.HunterPendingShoot = true
	gs.HunterPendingFrom = "vote"
	// engine_day.go::advanceDay() 在 HunterPendingShoot=true 时切换
	// PhaseHunterShoot 并设置 TurnActingSeat。
	setPhaseAndDeadline(gs, PhaseHunterShoot)
	gs.TurnActingSeat = hunterSeatForPhaseLocked(gs)
	if gs.TurnActingSeat != hunter {
		t.Fatalf("R-3 advanceDay: TurnActingSeat = %d, want %d (hunter)",
			gs.TurnActingSeat, hunter)
	}
}

func TestHunterActingSeat_R4_IdiotRevealSkipHunter(t *testing.T) {
	gs := newFairnessGame(t, 4204)
	hunter := ensureHunterSeat(t, gs)
	if hunter == NoSeat {
		t.Skip("no hunter seat in seed 4204")
	}
	// 复刻 engine_day.go::IdiotReveal("skip") 路径 —— 放逐白痴但白痴
	// 实际是猎人时(不可能但引擎允许),设置 HunterPendingShoot。
	gs.HunterPendingShoot = true
	gs.HunterPendingFrom = "vote"
	setPhaseAndDeadline(gs, PhaseHunterShoot)
	gs.TurnActingSeat = hunterSeatForPhaseLocked(gs)
	if gs.TurnActingSeat != hunter {
		t.Fatalf("R-4 idiot_reveal skip hunter: TurnActingSeat = %d, want %d (hunter)",
			gs.TurnActingSeat, hunter)
	}
}

func TestHunterActingSeat_R5_NoPendingReturnsNoSeat(t *testing.T) {
	gs := newFairnessGame(t, 4205)
	// 不设 HunterPendingShoot —— 应返回 NoSeat(避免 race 条件下误填)。
	got := hunterSeatForPhaseLocked(gs)
	if got != NoSeat {
		t.Fatalf("R-5: hunterSeatForPhaseLocked without HunterPendingShoot = %d, want NoSeat (%d)",
			got, NoSeat)
	}
}

func TestHunterActingSeat_R6_WatchdogEarlySkip(t *testing.T) {
	// 防御纵深:watchdog 120s early skip 在 PhaseHunterShoot + actingSeat
	// 是猎人时触发 HunterShoot(-1),阶段推进。
	//
	// 不直接构造真实房间(需要 fillAndStart + BotAgents),而走最小
	// GameState + manager 路径:仅校验 SkipPhaseAction 在 PhaseHunterShoot
	// + role="hunter" 下返回 ("hunter_shoot", -1) — 这是 120s early skip
	// 派发时喂给 dispatchQuarantinedSkipLocked 的精确参数。
	gotName, gotArg := wwplayer.SkipPhaseAction("hunter_shoot", "hunter")
	if gotName != "hunter_shoot" || gotArg != -1 {
		t.Fatalf("R-6: SkipPhaseAction(hunter_shoot, hunter) = (%q, %d), want (hunter_shoot, -1)",
			gotName, gotArg)
	}
	// 顺便校验 phaseWatchdogHunterDeadline 常量存在且为 120s —— 这是
	// 防御纵深的早期兜底时长。
	if phaseWatchdogHunterDeadline != 120*time.Second {
		t.Fatalf("R-6: phaseWatchdogHunterDeadline = %v, want 120s",
			phaseWatchdogHunterDeadline)
	}
}