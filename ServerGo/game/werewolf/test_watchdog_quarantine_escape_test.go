// Package werewolf — 回归测试:BUG-R223(vote 阶段全 quarantine 卡死)与
// BUG-R221(全员 quarantine 房间无逃生路径)。
// 对应自动化测试报告 20260801_061438 / 20260801_083235。
//
// BUG-R223:phaseWatchdogTick 的 **stall 分支** 曾把
// `PhaseVote → finishVoteLocked` 检查放在 `if actingSeat >= 0 {` 守卫**内部**,
// 而 watchdogActingSeat(PhaseVote) 走 lowestActiveBotSeatLocked —— 该 helper
// 跳过所有 quarantined bot,全员 quarantine 时返回 -1,整个救援块退化为
// "只 skipCount++ 后重新计时",每 360s 空转一次,永不计票。对比 **deadline 分支**
// 的同一检查一直放在守卫**之外**,结构不对称正是缺陷根因。
//
// BUG-R221:所有 bot 都被 quarantine 的房间没有任何推进动力(wake 被
// IsQuarantined guard 短路),既无房间 TTL 也无"全员失联"升级,实测房间
// ce288893 保持"游戏中" 15+ 小时持续占用大厅席位。
//
// 所有测试均用 runWithDeadlockGuard 包住生产调用(§92a):
// phaseWatchdogTick 内部经 lockRoomBriefly 拿 r.mu,若引入自死锁必须
// FAIL 而不是挂起。
package werewolf

import (
	"testing"
	"time"

	"LsmAgentGame/agent/wwplayer"
)

// setupAllQuarantinedVoteRoom 构造一个 7 人全 AI 房间:全部存活、全部 bot、
// 全部 quarantined,phase=vote。返回时**不持** r.mu(phaseWatchdogTick 自己抢锁)。
func setupAllQuarantinedVoteRoom(t *testing.T, m *WerewolfManager, r *WerewolfRoom) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.State.Phase = PhaseVote
	r.State.DayNumber = 1
	r.BotAgents = make(map[int]*wwplayer.Agent)
	for i := 0; i < 7; i++ {
		r.State.Players[i].Alive = true
		r.State.Players[i].IsBot = true
		r.State.Players[i].Voted = false
		r.State.Players[i].VoteTarget = NoSeat
		bot, _ := stubBotWithChannel(i)
		bot.SetQuarantined()
		r.BotAgents[i] = bot
	}
	syncQuarantinedLocked(r)
}

// TestBUG_R223_VoteStall_AllQuarantined_ForceTallied 验证 BUG-R223 核心修复:
// vote 阶段所有 bot 都 quarantine(actingSeat == -1)且 stall deadline 已过时,
// watchdog 的 **stall 分支** 必须仍然 force-tally 推进阶段。
//
// 缺陷版本(PhaseVote 检查在 `actingSeat >= 0` 内)下本测试 FAIL:
// 阶段永远停在 PhaseVote。
func TestBUG_R223_VoteStall_AllQuarantined_ForceTallied(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)
	setupAllQuarantinedVoteRoom(t, m, r)

	r.mu.Lock()
	// 前置断言:确认本测试确实处在"actingSeat == -1"的缺陷触发条件下。
	if got := watchdogActingSeat(r); got != -1 {
		r.mu.Unlock()
		t.Fatalf("前置条件不成立:watchdogActingSeat = %d,期望 -1"+
			"(全员 quarantine 时 lowestActiveBotSeatLocked 应返回 -1)", got)
	}
	// deadline 分支必须**不能**抢跑,否则测不到 stall 分支 —— 把
	// PhaseDeadlineAt 顶到 1 小时之后。
	r.State.PhaseDeadlineAt = time.Now().Add(time.Hour)
	// 部分座位已投票,让 FinishVote 走"有最高票 → 放逐"的正常路径。
	for _, i := range []int{0, 1, 2} {
		r.State.Players[i].Voted = true
		r.State.Players[i].VoteTarget = Seat(4)
	}
	// stall 计时:同 key 已持续超过 phaseWatchdogDeadlineFor(SeatCount)。
	r.phaseWatchdog.key = "vote/-1"
	r.phaseWatchdog.skipCount = 0
	r.phaseWatchdog.enteredAt = time.Now().
		Add(-(phaseWatchdogDeadlineFor(r.State.SeatCount) + time.Second))
	r.mu.Unlock()

	runWithDeadlockGuard(t, "phaseWatchdogTick(vote, all quarantined)", func() {
		if err := m.phaseWatchdogTick(r); err != nil {
			t.Errorf("phaseWatchdogTick returned unexpected error: %v", err)
		}
	})

	r.mu.Lock()
	phase := r.State.Phase
	status := r.State.Status
	r.mu.Unlock()

	if phase == PhaseVote && status != "over" {
		t.Fatalf("BUG-R223:vote 阶段全员 quarantine + stall 超时后,watchdog 必须 "+
			"force-tally 推进阶段;实际仍停在 %s(status=%s)。这说明 PhaseVote → "+
			"finishVoteLocked 检查仍被关在 `actingSeat >= 0` 守卫内部,只剩 630s "+
			"deadline 分支能救场", phase, status)
	}
	t.Logf("vote 阶段全员 quarantine 下 stall 分支成功推进到 %s(status=%s)", phase, status)
}

// TestBUG_R223_VoteDeadline_AllQuarantined_ForceTallied 对照测试:
// deadline 分支在同样的全 quarantine 条件下本来就能救场(结构对称的参照系)。
// 修复前后都应 PASS —— 它锁定的是"两个分支现在结构一致"这一不变式。
func TestBUG_R223_VoteDeadline_AllQuarantined_ForceTallied(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)
	setupAllQuarantinedVoteRoom(t, m, r)

	r.mu.Lock()
	// deadline 已过 → 走 deadline 分支。
	r.State.PhaseDeadlineAt = time.Now().Add(-time.Second)
	for _, i := range []int{0, 1, 2} {
		r.State.Players[i].Voted = true
		r.State.Players[i].VoteTarget = Seat(4)
	}
	r.phaseWatchdog.key = "vote/-1"
	r.phaseWatchdog.enteredAt = time.Now()
	r.mu.Unlock()

	runWithDeadlockGuard(t, "phaseWatchdogTick(vote deadline, all quarantined)", func() {
		if err := m.phaseWatchdogTick(r); err != nil {
			t.Errorf("phaseWatchdogTick returned unexpected error: %v", err)
		}
	})

	r.mu.Lock()
	phase := r.State.Phase
	status := r.State.Status
	r.mu.Unlock()

	if phase == PhaseVote && status != "over" {
		t.Fatalf("deadline 分支在全员 quarantine 下也应 force-tally;实际仍停在 %s", phase)
	}
}

// TestBUG_R223_DeadlineRearm_UnarmedActingPhase 验证防御性重新武装:
// 行动阶段的 PhaseDeadlineAt 为零值时,watchdog 必须用既有
// setPhaseAndDeadline 把时钟重新武装,而不是把该阶段永久留在"无 deadline"状态。
func TestBUG_R223_DeadlineRearm_UnarmedActingPhase(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	r.mu.Lock()
	r.State.Phase = PhaseSpeak
	r.State.SpeakTurnSeat = Seat(0)
	r.State.PhaseDeadlineAt = time.Time{} // 未武装
	r.BotAgents = make(map[int]*wwplayer.Agent)
	for i := 0; i < 7; i++ {
		r.State.Players[i].Alive = true
		r.State.Players[i].IsBot = true
		bot, _ := stubBotWithChannel(i)
		r.BotAgents[i] = bot // 均未 quarantine → 不触发 R221 熔断
	}
	r.phaseWatchdog.key = "speak/0"
	r.phaseWatchdog.enteredAt = time.Now()
	r.mu.Unlock()

	runWithDeadlockGuard(t, "phaseWatchdogTick(unarmed deadline)", func() {
		if err := m.phaseWatchdogTick(r); err != nil {
			t.Errorf("phaseWatchdogTick returned unexpected error: %v", err)
		}
	})

	r.mu.Lock()
	armed := !r.State.PhaseDeadlineAt.IsZero()
	deadlineAt := r.State.PhaseDeadlineAt
	r.mu.Unlock()

	if !armed {
		t.Fatal("BUG-R223:行动阶段 PhaseDeadlineAt 为零值时 watchdog 必须重新武装," +
			"否则该阶段永久失去时钟救援路径")
	}
	if !deadlineAt.After(time.Now()) {
		t.Fatalf("重新武装的 deadline 必须在未来,实际 = %v", deadlineAt)
	}
}

// TestBUG_R221_AllQuarantined_TripsCircuitBreaker 验证 BUG-R221 房间级熔断:
// 全员 quarantine 条件持续到阈值后,房间必须被强制结束(不再永久占用大厅席位)。
func TestBUG_R221_AllQuarantined_TripsCircuitBreaker(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)
	setupAllQuarantinedVoteRoom(t, m, r)

	closed := false
	m.SetOnGameOver(func(roomID string) { closed = true })

	r.mu.Lock()
	if !allBotsQuarantinedLocked(r) {
		r.mu.Unlock()
		t.Fatal("前置条件不成立:allBotsQuarantinedLocked 应为 true")
	}
	// 差 1 个 tick 到阈值 —— 本次 tick 应当触发熔断。
	r.phaseWatchdog.allQuarantinedTicks = allQuarantinedTripTicks - 1
	r.State.PhaseDeadlineAt = time.Now().Add(time.Hour)
	r.phaseWatchdog.key = "vote/-1"
	r.phaseWatchdog.enteredAt = time.Now()
	r.mu.Unlock()

	runWithDeadlockGuard(t, "phaseWatchdogTick(all quarantined trip)", func() {
		if err := m.phaseWatchdogTick(r); err != nil {
			t.Errorf("phaseWatchdogTick returned unexpected error: %v", err)
		}
	})

	r.mu.Lock()
	phase := r.State.Phase
	status := r.State.Status
	winner := r.State.Winner
	notified := r.gameOverNotified
	r.mu.Unlock()

	if status != "over" || phase != PhaseGameOver {
		t.Fatalf("BUG-R221:全员 quarantine 持续超阈值后房间必须强制结束;"+
			"实际 phase=%s status=%s(房间会一直显示「游戏中」占用大厅席位)",
			phase, status)
	}
	if winner != "draw" {
		t.Errorf("熔断结束的对局无任何一方达成胜利条件,winner 应为 \"draw\",实际 %q", winner)
	}
	if !notified {
		t.Error("forceCloseRoomLocked 必须置 gameOverNotified=true,防止后续 tick 重复结算")
	}
	if !closed {
		t.Error("熔断必须复用既有 forceCloseRoomLocked → onGameOver 回调(DB status 同步为 over)")
	}
}

// TestBUG_R221_AllQuarantined_CounterResetsOnRecovery 验证熔断计数器的回滚语义:
// 阈值前只要有任一 bot 恢复(不再 quarantine),计数器必须清零且房间**不**被结束。
// 这是给上游 LLM 代理瞬断留出的恢复窗口。
func TestBUG_R221_AllQuarantined_CounterResetsOnRecovery(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)
	setupAllQuarantinedVoteRoom(t, m, r)

	r.mu.Lock()
	// 已经攒了一半的 tick,但 seat 3 的模型此刻恢复了。
	r.phaseWatchdog.allQuarantinedTicks = allQuarantinedTripTicks / 2
	recovered, _ := stubBotWithChannel(3)
	r.BotAgents[3] = recovered // 全新 bot,未 quarantine
	syncQuarantinedLocked(r)
	if allBotsQuarantinedLocked(r) {
		r.mu.Unlock()
		t.Fatal("前置条件不成立:有 1 个未 quarantine 的存活 bot 时 " +
			"allBotsQuarantinedLocked 应为 false")
	}
	// 让本 tick 不触发任何 stall / deadline 救援,单测熔断计数分支。
	r.State.PhaseDeadlineAt = time.Now().Add(time.Hour)
	r.phaseWatchdog.key = "vote/3"
	r.phaseWatchdog.enteredAt = time.Now()
	r.mu.Unlock()

	runWithDeadlockGuard(t, "phaseWatchdogTick(all quarantined recovery)", func() {
		if err := m.phaseWatchdogTick(r); err != nil {
			t.Errorf("phaseWatchdogTick returned unexpected error: %v", err)
		}
	})

	r.mu.Lock()
	ticks := r.phaseWatchdog.allQuarantinedTicks
	status := r.State.Status
	phase := r.State.Phase
	r.mu.Unlock()

	if ticks != 0 {
		t.Fatalf("BUG-R221:任一 bot 恢复后熔断计数器必须清零,实际 allQuarantinedTicks=%d", ticks)
	}
	if status == "over" || phase == PhaseGameOver {
		t.Fatalf("BUG-R221:条件在阈值前被打断时房间**不**应被强制结束;"+
			"实际 phase=%s status=%s", phase, status)
	}
}

// TestBUG_R221_AllBotsQuarantinedLocked_AliveHumanBlocks 验证熔断判定口径:
// 只要还有存活真人座位(存活 + !IsBot + BotAgents[seat]==nil,识别信号与
// BUG-R212-P1-02 的 humanActingWolf 一致),房间仍有推进能力,不得熔断。
func TestBUG_R221_AllBotsQuarantinedLocked_AliveHumanBlocks(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)
	setupAllQuarantinedVoteRoom(t, m, r)

	r.mu.Lock()
	defer r.mu.Unlock()

	if !allBotsQuarantinedLocked(r) {
		t.Fatal("前置条件不成立:全 bot 全 quarantine 时应为 true")
	}
	// seat 0 改为真人(无 bot agent)。
	r.State.Players[0].IsBot = false
	delete(r.BotAgents, 0)
	if allBotsQuarantinedLocked(r) {
		t.Fatal("BUG-R221:存在存活真人座位时不得熔断 —— 真人仍可通过 " +
			"NightActionPanel / game.werewolf_action 推进对局")
	}
	// 真人死亡后房间才真正失去推进能力。
	r.State.Players[0].Alive = false
	if !allBotsQuarantinedLocked(r) {
		t.Fatal("真人死亡后房间已无任何推进动力,应判定为可熔断")
	}
}

// TestBUG_R221_AllBotsQuarantinedLocked_NoBotsNeverTrips 验证纯真人房间
// (0 个 bot agent)永不触发熔断 —— 熔断只针对"Agent 驱动的房间"。
func TestBUG_R221_AllBotsQuarantinedLocked_NoBotsNeverTrips(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	r.mu.Lock()
	defer r.mu.Unlock()
	r.BotAgents = make(map[int]*wwplayer.Agent)
	for i := 0; i < 7; i++ {
		r.State.Players[i].Alive = true
		r.State.Players[i].IsBot = false
	}
	if allBotsQuarantinedLocked(r) {
		t.Fatal("纯真人房间(0 bot agent)不得触发全员 quarantine 熔断")
	}
}
