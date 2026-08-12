// Package werewolf — regression test for BUG-R243-P1-01 (Round 243 report
// 20260806_045237).
//
// R243 报告: 13 人混合房间(1 真人女巫 + 12 Agent)进入「夜晚 · 狼人睁眼」后,
// 3 匹狼 bot(GLM/Kwail/Tencent)持续产生成功的 LLM 调用(UI 统计 166/166 成功),
// 反复调 speak / wolf_whisper / idle_silent,却从不提交 wolf_kill 票。阶段从
// 04:48:48 一直卡到 04:53:18(约 270 秒)才被推进。
//
// 根因: night_wolves 的 120s 早期 force-tally 路径要求
// `skipCount >= 1 || humanActingWolf`,而 skipCount 只统计 watchdog 派发的 skip。
// 狼 bot LLM 正常(未 quarantine)、持续活跃(只是不投票),watchdog 无 skip 可派,
// skipCount 恒为 0;真人房间 deadline 被 §127/R131 floor 提升到 ≥480s,
// deadline 分支也不触发 → 只能等 360s 通用兜底(force-tally 同样要 skipCount>=1)。
//
// 修复: 120s 早期 force-tally 增加第三条出口 — 「零投票死锁」:
// 进入阶段 ≥120s 且所有存活狼均未投票(无任何可推进的投票来源)时直接
// force-tally,全狼视为弃权,与 humanActingWolf 语义对称。
//
// 本测试搭建一个全 bot 狼房间(3 狼均未投票、skipCount=0、无真人狼),
// elapsed 刚过 120s,触发一次 phaseWatchdogTick,断言阶段推进出 night_wolves。
// 未修复时 noWolfVoteCast 条件不存在,120s 路径不触发,阶段仍停在 night_wolves。
package werewolf

import (
	"testing"
	"time"

	"LsmAgentGame/agent/wwplayer"
)

// TestPhaseWatchdogTick_NightWolves_NoWolfVoteCast_EarlyForceTally 验证
// BUG-R243-P1-01 核心修复: night_wolves 进入 ≥120s 且所有存活狼均未投票时,
// 即使 skipCount=0(watchdog 从未派发过 skip),watchdog 也应 force-tally 推进
// 阶段,而不是干等 360s 通用兜底 / ≥480s deadline。
func TestPhaseWatchdogTick_NightWolves_NoWolfVoteCast_EarlyForceTally(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	r.mu.Lock()

	// 3 匹狼 bot(seat 0/4/8)均未投票;其余为神民,保证 tallyWolfVotes 的
	// randomAliveNonWolf 兜底有合法目标。无真人座位(R243 房间真人坐女巫位)。
	r.State.Phase = PhaseNightWolves
	r.State.DayNumber = 1
	r.State.TurnActingSeat = Seat(0) // firstLivingWolf
	r.State.Roles[0] = RoleWerewolf
	r.State.Roles[1] = RoleSeer
	r.State.Roles[2] = RoleVillager
	r.State.Roles[3] = RoleVillager
	r.State.Roles[4] = RoleWerewolf
	r.State.Roles[5] = RoleVillager
	r.State.Roles[6] = RoleWitch
	r.State.Roles[7] = RoleVillager
	r.State.Roles[8] = RoleWerewolf
	r.State.Roles[9] = RoleVillager
	r.State.Roles[10] = RoleVillager
	r.State.Roles[11] = RoleVillager
	r.State.Roles[12] = RoleVillager
	for i := 0; i < MaxPlayers; i++ {
		r.State.Players[i].Alive = true
		r.State.Players[i].IsBot = true
		r.State.WolfVoteCast[i] = false
		r.State.WolfVotes[i] = NoSeat
	}
	// 无真人座位:把 fillAndStart 可能放入的真人清掉(对齐 R243 房间真人=女巫
	// 但女巫位是 bot 无所谓的场景——本测试只关心狼投票来源,全 bot 最纯粹)。
	for i := 0; i < MaxPlayers; i++ {
		r.State.Players[i].IsBot = true
	}

	// 所有座位都有 bot(均未 quarantine)——这正是 R243 的形态:bot 活跃、
	// LLM 成功、只是不调 wolf_kill。
	r.BotAgents = make(map[int]*wwplayer.Agent)
	for i := 0; i < MaxPlayers; i++ {
		bot, _ := stubBotWithChannel(i)
		r.BotAgents[i] = bot
	}

	// 前向 watchdog: skipCount=0(狼 bot 从未 stuck 到让 watchdog 派 skip),
	// elapsed 刚过 120s。key 匹配 watchdogActingSeat 在 night_wolves 返回的
	// TurnActingSeat=0。
	r.phaseWatchdog.key = "night_wolves/0"
	r.phaseWatchdog.skipCount = 0
	r.phaseWatchdog.enteredAt = time.Now().Add(-(phaseWatchdogSingleActorDeadline + 1*time.Second))

	// phaseWatchdogTick 内部用 lockRoomBriefly(TryLock)拿 r.mu;测试持锁会致其
	// 立即返回 nil,故先释放,断言前再加锁读取状态。
	r.mu.Unlock()

	if err := m.phaseWatchdogTick(r); err != nil {
		t.Fatalf("phaseWatchdogTick returned unexpected error: %v", err)
	}

	r.mu.Lock()
	phase := r.State.Phase
	r.mu.Unlock()

	if phase == PhaseNightWolves {
		t.Fatalf("BUG-R243-P1-01: 所有存活狼 120s 未投票时,watchdog 应 " +
			"force-tally 推进阶段;阶段仍停在 PhaseNightWolves 意味着零投票早路径 " +
			"未生效(狼 bot 活跃但不投票时阶段卡 270+ 秒)")
	}
	t.Logf("零投票 night_wolves early force-tally 推进阶段到 %s(actingSeat=0, "+
		"skipCount=0, elapsed ≈ 120s)", phase)
}

// TestPhaseWatchdogTick_NightWolves_PartialVoteCast_NoEarlyForceTally 反向验证:
// 只要有 1 匹狼已投票,零投票早路径不得触发(尊重其它狼的协商时间),
// 阶段应仍停在 night_wolves(交给 360s 通用兜底 / deadline 路径)。
func TestPhaseWatchdogTick_NightWolves_PartialVoteCast_NoEarlyForceTally(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	r.mu.Lock()

	r.State.Phase = PhaseNightWolves
	r.State.DayNumber = 1
	r.State.TurnActingSeat = Seat(0)
	r.State.Roles[0] = RoleWerewolf
	r.State.Roles[1] = RoleSeer
	r.State.Roles[2] = RoleVillager
	r.State.Roles[3] = RoleVillager
	r.State.Roles[4] = RoleWerewolf
	r.State.Roles[5] = RoleVillager
	r.State.Roles[6] = RoleWitch
	for i := 0; i < MaxPlayers; i++ {
		r.State.Players[i].Alive = true
		r.State.Players[i].IsBot = true
		r.State.WolfVoteCast[i] = false
		r.State.WolfVotes[i] = NoSeat
	}
	// seat 4 已投票(目标 seat 1),seat 0 未投。
	r.State.WolfVoteCast[4] = true
	r.State.WolfVotes[4] = Seat(1)

	r.BotAgents = make(map[int]*wwplayer.Agent)
	for i := 0; i < MaxPlayers; i++ {
		bot, _ := stubBotWithChannel(i)
		r.BotAgents[i] = bot
	}

	r.phaseWatchdog.key = "night_wolves/0"
	r.phaseWatchdog.skipCount = 0
	r.phaseWatchdog.enteredAt = time.Now().Add(-(phaseWatchdogSingleActorDeadline + 1*time.Second))

	r.mu.Unlock()

	if err := m.phaseWatchdogTick(r); err != nil {
		t.Fatalf("phaseWatchdogTick returned unexpected error: %v", err)
	}

	r.mu.Lock()
	phase := r.State.Phase
	r.mu.Unlock()

	if phase != PhaseNightWolves {
		t.Fatalf("BUG-R243-P1-01 反向用例: 已有狼投票(seat 4)时,零投票早路径 " +
			"不得触发;阶段被推进到 %s 意味着 noWolfVoteCast 判定过宽,会误伤正常协商",
			phase)
	}
	t.Logf("部分投票场景正确保持 night_wolves(未误触发 force-tally)")
}
