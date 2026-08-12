// Package werewolf — regression test for R91 Bug-4 治理
// (2026-07-11 §13 增强 — watchdog deadline/90s 兜底派发前最后唤醒)。
//
// R90 自动化测试报告 (20260711_002005) 的 §6 Bug-4 描述:
//
//   「watchdog 频繁空跳」:LLM 端点持续不可用 → bot 一轮 LLM 失败 →
//   watchdog 在 deadline/90s 兜底时**立即**派发 skip,导致阶段被"空过":
//   没有任何人真正做出行动,bot 完全没有"再试一次"的机会,13 bot 房间
//   阶段推进过快、聊天覆盖率仅 30.8%。
//
// 修复(2026-07-11 §13-R91 增强):
//   1. `phaseWatchdogTick` 在 deadline 派发前 push 一次 wake 事件给
//      当前 acting bot,让 bot 立即重新尝试执行(同 tick 内并行)。
//   2. `phaseWatchdogTick` 在 90s 兜底派发前做同样的 last-wake。
//   3. skip 仍按既定路径派发 — wake 不是替代,只是给 bot "最后一次机会";
//      wake 失败 → skip 是兜底(无回退路径回归)。
//
// 本测试验证两个关键回归点:
//   - DeadlineLastWake: deadline 到期时,acting bot 收到 wake 事件。
//   - Watchdog90sLastWake: 90s 兜底触发时,acting bot 收到 wake 事件。
//   - 即使 last-wake 派发,skip 仍会派发(语义不变),bot 接收 wake 后
//     立即执行 → 阶段推进有真实行动。
package werewolf

import (
	"testing"
	"time"

	"LsmAgentGame/agent/wwplayer"
)

// TestPhaseWatchdogTick_DeadlineLastWake verifies the 2026-07-11 §13-R91
// enhancement: when PhaseDeadlineAt expires, the watchdog pushes a final wake
// event to the acting bot BEFORE dispatching the skip. Without this R91 fix
// the R90 Bug-4 regression would recur: bot has zero chance to retry between
// deadline expiry and skip dispatch.
func TestPhaseWatchdogTick_DeadlineLastWake(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	// Move to PhaseNightWolves — this phase has a deterministic acting seat
	// (firstLivingWolf) so watchdogActingSeat returns >= 0, which is what
	// we need to verify the last-wake logic.
	r.mu.Lock()
	r.State.Phase = PhaseNightWolves
	r.State.Status = "playing"
	wolfSeat := firstLivingWolf(r.State)
	if wolfSeat == NoSeat {
		t.Fatalf("expected a living wolf in the 7-player game")
	}
	r.State.TurnActingSeat = wolfSeat
	// Set deadline 5s in the past so the deadline branch fires.
	r.State.PhaseDeadlineAt = time.Now().Add(-5 * time.Second)

	// Install a stub agent on the acting seat with a real events channel.
	bot, ch := stubBotWithChannel(int(wolfSeat))
	if r.BotAgents == nil {
		r.BotAgents = make(map[int]*wwplayer.Agent)
	}
	r.BotAgents[int(wolfSeat)] = bot
	r.mu.Unlock()

	// Run the watchdog tick — this should push a wake event before skip.
	if err := m.phaseWatchdogTick(r); err != nil {
		t.Fatalf("phaseWatchdogTick returned unexpected error: %v", err)
	}

	// Verify the wake event arrived on the bot's channel.
	select {
	case evt := <-ch:
		if evt.Kind != "wake" {
			t.Fatalf("expected wake event, got %q (BUG-R91: last-wake not pushed "+
				"before deadline dispatch)", evt.Kind)
		}
		if evt.Context.Phase != PhaseNightWolves.String() {
			t.Fatalf("expected wake phase %q, got %q", PhaseNightWolves.String(), evt.Context.Phase)
		}
		t.Logf("BUG-R91 fix verified: deadline-last-wake delivered phase=%s",
			evt.Context.Phase)
	default:
		t.Fatalf("expected wake event on bot channel but none arrived "+
			"(BUG-R91: last-wake not pushed before deadline dispatch — "+
			"R90 Bug-4 regression)")
	}
}

// TestPhaseWatchdogTick_90sLastWake verifies the 2026-07-11 §13-R91
// enhancement for the 90s兜底 (non-deadline) path. When the watchdog fires
// after phaseWatchdogDeadline (90s) of stalled phase+actingSeat, the bot
// receives a final wake before skip dispatch.
func TestPhaseWatchdogTick_90sLastWake(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	r.mu.Lock()
	r.State.Phase = PhaseNightWolves
	r.State.Status = "playing"
	wolfSeat := firstLivingWolf(r.State)
	if wolfSeat == NoSeat {
		t.Fatalf("expected a living wolf in the 7-player game")
	}
	r.State.TurnActingSeat = wolfSeat
	r.State.PhaseDeadlineAt = time.Time{} // zero → deadline branch skipped
	// BUG-R243-P1-01 (2026-08-06) 交互修复:本测试模拟 >120s 停滞以触发 90s
	// last-wake 分支,但 R243 新增的「零投票 120s 早期 force-tally」第三出口
	// 会在所有存活狼 WolfVoteCast=false 时抢先触发(测试日志 elapsed=4m1s)。
	// 本测试的目标是 90s last-wake 而非 R243 出口,因此标记 acting wolf 已投票
	// (noWolfVoteCast=false),让执行流落到被测的 90s 分支。
	r.State.WolfVoteCast[wolfSeat] = true
	// Pre-set the watchdog key/enteredAt so the 90s branch fires.
	r.phaseWatchdog.key = PhaseNightWolves.String() + "/" + itoa(int(wolfSeat))
	r.phaseWatchdog.enteredAt = time.Now().Add(-(phaseWatchdogDeadlineFor(r.State.SeatCount) + 1*time.Second))
	r.phaseWatchdog.lastLog = time.Now()

	bot, ch := stubBotWithChannel(int(wolfSeat))
	if r.BotAgents == nil {
		r.BotAgents = make(map[int]*wwplayer.Agent)
	}
	r.BotAgents[int(wolfSeat)] = bot
	r.mu.Unlock()

	// Run the watchdog tick.
	if err := m.phaseWatchdogTick(r); err != nil {
		t.Fatalf("phaseWatchdogTick returned unexpected error: %v", err)
	}

	// Verify the wake event arrived.
	select {
	case evt := <-ch:
		if evt.Kind != "wake" {
			t.Fatalf("expected wake event, got %q (BUG-R91: 90s兜底 "+
				"未推送最后唤醒)", evt.Kind)
		}
		t.Logf("BUG-R91 fix verified: 90s-last-wake delivered phase=%s",
			evt.Context.Phase)
	default:
		t.Fatalf("expected wake event on bot channel but none arrived "+
			"(BUG-R91: 90s兜底前未推送 last-wake — R90 Bug-4 回归)")
	}
}

// TestPhaseWatchdogTick_NilBot_NoPanic verifies the R91 fix safely no-ops when
// the acting seat has no BotAgent (e.g. human player). The skip dispatch path
// is preserved, and the watchdog does not panic.
func TestPhaseWatchdogTick_NilBot_NoPanic(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	r.mu.Lock()
	r.State.Phase = PhaseNightWolves
	r.State.Status = "playing"
	wolfSeat := firstLivingWolf(r.State)
	if wolfSeat == NoSeat {
		t.Fatalf("expected a living wolf in the 7-player game")
	}
	r.State.TurnActingSeat = wolfSeat
	r.State.PhaseDeadlineAt = time.Time{}
	r.phaseWatchdog.key = PhaseNightWolves.String() + "/" + itoa(int(wolfSeat))
	r.phaseWatchdog.enteredAt = time.Now().Add(-(phaseWatchdogDeadlineFor(r.State.SeatCount) + 1*time.Second))
	r.phaseWatchdog.lastLog = time.Now()

	// Intentionally do NOT install a BotAgent. The watchdog must not panic.
	r.mu.Unlock()

	// Should not panic.
	if err := m.phaseWatchdogTick(r); err != nil {
		t.Fatalf("phaseWatchdogTick returned unexpected error: %v", err)
	}
	t.Logf("BUG-R91 safety: nil-bot no-panic verified")
}

// ensure agent import is referenced (compile-time guard).
var _ = wwplayer.AgentEvent{}
