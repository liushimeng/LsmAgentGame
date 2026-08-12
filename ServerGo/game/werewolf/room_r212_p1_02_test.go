// Package werewolf — regression test for BUG-R212-P1-02 (Round 212 follow-up).
//
// R212 混合模式复测报告(20260731_004620)发现: 1真人+12AI 房间中,创建者真人坐
// seat 0 且为狼人时,`firstLivingWolf()` 返回 seat 0 → `TurnActingSeat=0`(真人)。
// 真人可通过 NightActionPanel / game.werewolf_action 投票,但若真人不投票(掉线/
// 挂机未触发 HandleDisconnect),watchdog 的 bot 派发路径(PushEvent →
// BotAgents[0]=nil)空转,`skipCount` 永远到不了 1,只能等 360s 硬兜底 → 整阶段
// 卡 ~8 分钟。
//
// 修复在 phaseWatchdogTick 的 night_wolves early force-tally 路径上增加
// "人类 acting seat" 识别: 存活 + !IsBot + BotAgents[seat]==nil + 未投票,
// 在 phaseWatchdogSingleActorDeadline (120s) 时直接 force-tally(真人视为弃权),
// 不必等 skipCount>=1。
//
// 本测试搭建一个真人 acting wolf 房间(seat 0 = 真人狼人, seats 1-6 = bot),
// 强制进入 night_wolves、actingSeat=0、skipCount=0、elapsed 刚过 120s,触发
// 一次 phaseWatchdogTick,断言阶段推进到 night_seer(即 early force-tally 生效)。
// 未修复时 humanActingWolf 条件不存在,120s 路径不触发,阶段仍停在 night_wolves。
package werewolf

import (
	"testing"
	"time"

	"LsmAgentGame/agent/wwplayer"
)

// TestPhaseWatchdogTick_NightWolves_HumanActingWolf_EarlyForceTally 验证
// BUG-R212-P1-02 核心修复: night_wolves acting seat 是真人狼人且未投票时,
// watchdog 在 120s 即 force-tally 推进阶段,不必等 360s 硬兜底。
func TestPhaseWatchdogTick_NightWolves_HumanActingWolf_EarlyForceTally(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	r.mu.Lock()

	// seat 0 = 真人狼人(IsBot=false, 不投票); seats 1-6 = bot。
	// 设两个狼(seat 0 真人 + seat 1 bot)均未投票,使 counts 为空,
	// tallyWolfVotes 走 randomAliveNonWolf 兜底(需要 ≥1 存活非狼)。
	r.State.Phase = PhaseNightWolves
	r.State.DayNumber = 1
	r.State.TurnActingSeat = Seat(0)
	r.State.Roles[0] = RoleWerewolf // 真人 acting wolf
	r.State.Roles[1] = RoleWerewolf
	r.State.Roles[2] = RoleSeer
	r.State.Roles[3] = RoleWitch
	r.State.Roles[4] = RoleVillager
	r.State.Roles[5] = RoleVillager
	r.State.Roles[6] = RoleVillager
	for i := 0; i < 7; i++ {
		r.State.Players[i].Alive = true
		r.State.Players[i].IsBot = true
		r.State.WolfVoteCast[i] = false
		r.State.WolfVotes[i] = NoSeat
	}
	r.State.Players[0].IsBot = false // seat 0 是真人

	// 注册 bot: seats 1-6 有 bot 驱动上下文构建; seat 0(真人)无 bot → BotAgents[0]=nil。
	r.BotAgents = make(map[int]*wwplayer.Agent)
	for i := 1; i < 7; i++ {
		bot, _ := stubBotWithChannel(i)
		r.BotAgents[i] = bot
	}
	if r.BotAgents[0] != nil {
		r.mu.Unlock()
		t.Fatal("seat 0 必须无 bot(真人),BotAgents[0] 应为 nil")
	}

	// 前向 watchdog: skipCount=0(未经过 360s 硬兜底),elapsed 刚过 120s。
	// key 必须匹配 watchdogActingSeat 在 night_wolves 返回的 TurnActingSeat=0。
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
		t.Fatalf("BUG-R212-P1-02: 真人 acting wolf 未投票时,watchdog 应在 120s " +
			"force-tally 推进阶段;阶段仍停在 PhaseNightWolves 意味着人类 acting seat " +
			"早路径未生效(只能等 360s 硬兜底,整阶段卡 ~8 分钟)")
	}
	t.Logf("真人 acting wolf early force-tally 推进阶段到 %s(actingSeat=0, "+
		"elapsed ≈ 120s)", phase)
}
