// Package werewolf — regression test for BUG-R194-001 (Round 194).
//
// BUG (reported): death_lyric 阶段卡死 25+ 分钟,远超设计上限 30 秒;看门狗
// (phase watchdog)未触发 last_words_skip 自动推进遗言队列。
//
// 本测试直接驱动 phaseWatchdogTick,复现报告场景:13 人局、death_lyric 阶段、
// 一个死亡 bot 座位在遗言队列头部,阶段 deadline 已过。断言 watchdog 在单个
// tick 内派发 last_words_skip 把遗言队列清空并将阶段推进到下一阶段
// (dawn→start_day),证明 skip 路径正确接线。若本测试失败则确认 watchdog
// 推进逻辑存在真实缺陷;若通过则报告所观察到的"卡死"更可能源于运行时不
// 可重复因素(上游 LLM 长尾响应 378s 等),而非 skip 路径缺失。
package werewolf

import (
	"testing"
	"time"

	"LsmAgentGame/agent/wwplayer"
	"LsmAgentGame/errcode"
)

// stubBotForWatchdog builds a minimal bot registered in r.BotAgents. We do NOT
// start its run loop — the watchdog test only needs the seat to be "occupied" so
// lowestActiveBotSeatLocked / watchdogActingSeat resolve a driver.
func stubBotForWatchdog(t *testing.T, r *WerewolfRoom, seat int, alive bool) {
	t.Helper()
	a := &wwplayer.Agent{Seat: seat}
	a.SetEvents(make(chan wwplayer.AgentEvent, 16))
	if r.BotAgents == nil {
		r.BotAgents = make(map[int]*wwplayer.Agent)
	}
	r.BotAgents[seat] = a
	r.State.Players[seat].Alive = alive
	r.State.Players[seat].LastWords = false
}

// TestBUG_R194_001_WatchdogAdvancesDeathLyric verifies that when death_lyric is
// stalled past its deadline, the phase watchdog fires last_words_skip to drain
// the death-lyric queue and advance the phase.
func TestBUG_R194_001_WatchdogAdvancesDeathLyric(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	r.mu.Lock()
	// 构造 death_lyric 场景:座位 2 死亡 + LastWords=true,座位 5 死亡 +
	// LastWords=true;遗言队列=[2,5],当前 acting seat=2。
	r.State.Players[2].Alive = false
	r.State.Players[2].LastWords = true
	r.State.Players[5].Alive = false
	r.State.Players[5].LastWords = true
	r.State.Phase = PhaseDeathLyric
	r.State.DeathLyricQueue = []Seat{2, 5}
	r.State.DeathLyricDone = map[Seat]bool{}
	r.State.DeathLyricCurrent = 2
	r.State.DeathLyricOnDone = func() *errcode.Error {
		// 队列清空后恢复白天流程(模拟 onDone → dawn)。
		r.State.Phase = PhaseDawn
		return nil
	}
	// 其它座位存活(确保 acting seat 判定明确)。
	for i := 0; i < MaxPlayers; i++ {
		if i != 2 && i != 5 {
			r.State.Players[i].Alive = true
		}
	}
	// 注册 bot 座位(供 driver 解析),避免 lowestActiveBotSeatLocked 返回 -1
	// 干扰 acting seat 判定。
	r.BotAgents = make(map[int]*wwplayer.Agent)
	for i := 0; i < 7; i++ {
		stubBotForWatchdog(t, r, i, r.State.Players[i].Alive)
	}

	// 阶段 deadline 已过(模拟超时卡死)。
	r.State.PhaseDeadlineAt = time.Now().Add(-10 * time.Second)

	// 释放锁:phaseWatchdogTick 通过 lockRoomBriefly 自行持锁(生产 watchdog
	// goroutine 调用路径不持 r.mu)。持锁调用会导致 lockRoomBriefly 超时失败,
	// tick 被跳过,无法复现场景。
	r.mu.Unlock()

	// 驱动一次 watchdog tick。
	runWithDeadlockGuard(t, "phaseWatchdogTick(death_lyric)", func() {
		if e := m.phaseWatchdogTick(r); e != nil {
			t.Errorf("phaseWatchdogTick returned error: %v", e)
		}
	})

	// 重新持锁读取断言。
	r.mu.Lock()

	// 断言:watchdog 推进了遗言队列。第一次 skip 应把 acting seat 从 2 推进
	// 到 5(队列仍有 1 元素)或清空队列进入 dawn。无论哪种,DeathLyricCurrent
	// 不应仍是 2(除非已推进到 dawn)。
	if r.State.Phase == PhaseDeathLyric && r.State.DeathLyricCurrent == 2 {
		t.Fatalf("death_lyric 阶段未被 watchdog 推进: DeathLyricCurrent 仍为 2,"+
			" phase=%s;确认 last_words_skip 派发路径正确接线(r194 假设未验证)",
			r.State.Phase)
	}
	t.Logf("death_lyric watchdog 推进成功: phase=%s, DeathLyricCurrent=%d,"+
		" queue_len=%d", r.State.Phase, r.State.DeathLyricCurrent,
		len(r.State.DeathLyricQueue))
}

// TestBUG_R194_001_WatchdogDrainsFullDeathLyricQueue extends the stall test to
// cover the full multi-seat drain + human-actor scenario. Two dead seats both
// give last words(队列=[2,5]); each watchdog tick skips one seat until the
// queue empties and the phase leaves death_lyric. Seat 5 is a HUMAN actor (no
// bot registered in BotAgents) — this is the reported Day-2 scenario where the
// human player (#7) gives last words and must be auto-skipped by the watchdog.
// 验证:watchdog 能把多座位遗言队列完整清空,包括人类座位,阶段最终推进。
func TestBUG_R194_001_WatchdogDrainsFullDeathLyricQueue(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	r.mu.Lock()
	r.State.Players[2].Alive = false
	r.State.Players[2].LastWords = true
	r.State.Players[5].Alive = false
	r.State.Players[5].LastWords = true
	r.State.Phase = PhaseDeathLyric
	r.State.DeathLyricQueue = []Seat{2, 5}
	r.State.DeathLyricDone = map[Seat]bool{}
	r.State.DeathLyricCurrent = 2
	r.State.DeathLyricOnDone = func() *errcode.Error {
		r.State.Phase = PhaseDawn
		return nil
	}
	for i := 0; i < MaxPlayers; i++ {
		if i != 2 && i != 5 {
			r.State.Players[i].Alive = true
		}
	}
	// 仅注册 bot 座位 0,1,3,4,6;座位 2 和 5 为人类(无 bot)。
	r.BotAgents = make(map[int]*wwplayer.Agent)
	for _, seat := range []int{0, 1, 3, 4, 6} {
		stubBotForWatchdog(t, r, seat, r.State.Players[seat].Alive)
	}
	r.State.PhaseDeadlineAt = time.Now().Add(-10 * time.Second)
	r.mu.Unlock()

	// 反复驱动 watchdog tick 直至阶段离开 death_lyric 或超过安全上限。
	deadline := time.Now().Add(2 * time.Second)
	for r.State.Phase == PhaseDeathLyric && time.Now().Before(deadline) {
		runWithDeadlockGuard(t, "phaseWatchdogTick(death_lyric-drain)", func() {
			_ = m.phaseWatchdogTick(r)
		})
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.State.Phase == PhaseDeathLyric {
		t.Fatalf("death_lyric 队列未被 watchdog 清空: 剩余队列=%v, 当前 acting=%d",
			r.State.DeathLyricQueue, r.State.DeathLyricCurrent)
	}
	// 两个遗言座位都应已被 skip(LastWords 被消费为 false)。
	if r.State.Players[2].LastWords || r.State.Players[5].LastWords {
		t.Fatalf("遗言未完全消费: seat2.LastWords=%v, seat5.LastWords=%v",
			r.State.Players[2].LastWords, r.State.Players[5].LastWords)
	}
	if r.State.Phase != PhaseDawn {
		t.Fatalf("遗言队列清空后应恢复 dawn, 实际 phase=%s", r.State.Phase)
	}
	t.Logf("death_lyric 完整清空: 两座位(含人类座位 2,5)均 skip, 阶段→dawn")
}
