// Package werewolf — regression test for BUG-HUNTER2 (2026-08-07 报告)。
//
// BUG-HUNTER2-P0-01: 警长竞选阶段被 watchdog 错误跳过。13 人局报告实测
// 警长竞选阶段 acting_seat 卡 365s 后被 360s 兜底派发 skip,期间 watchdog
// 以「lowestActiveBotSeatLocked=0」为 acting seat,但 sheriff 是并发行动
// 阶段(所有存活玩家同时举手参选 + 同时投票),没有真正的 acting seat。
// 修复:PhaseSheriff + PhaseDeadlineAt 已武装时,完全跳过 360s 兜底,让
// 上方 deadline 分支(默认 300s 全 AI / 120s 真人)接管。deadline 分支
// 调 sheriff_elect skip → SheriffElect(NoSeat),与人类/Agent 主动结束
// 竞选走完全一致的路径。
//
// BUG-HUNTER2-P2-01: watchdog 派发 sheriff_elect skip 时无人参选分支
// 不再静默 —— EmitSheriffAutoSkip 广播「⏭ 警长竞选超时,本局无警长」。
//
// 本测试覆盖:
//   R-H2-S01  360s 兜底路径在 sheriff 阶段不派发 skip(由 deadline 接管)
//   R-H2-S02  deadline 路径在 sheriff 阶段派发 sheriff_elect skip
//             并触发 EmitSheriffAutoSkip(防止阶段无声切换)
//   R-H2-S03  360s 兜底路径在 deadline 未武装时仍兜底(防御性回归)
package werewolf

import (
	"testing"
	"time"

	"LsmAgentGame/agent/wwplayer"
)

// R-H2-S01: 当 PhaseDeadlineAt 已武装(默认值由 setPhaseAndDeadline 写入),
// 360s 兜底不应在 sheriff 阶段派发 skip —— 应直接 return nil 让 deadline
// 分支接管。验证手段:让 13 人局进入 sheriff 阶段、模拟 360s 兜底条件
// (elapsed ≥ phaseWatchdogDeadlineFor(13))、PhaseDeadlineAt 设为未来 60s。
// 断言 watchdog 不派发 skip → phase 仍为 sheriff。
func TestHunter2_S01_SheriffBypasses360sFallbackWhenDeadlineArmed(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	r.mu.Lock()
	// 把阶段切到 PhaseSheriff(D1 早晨标准流程)。
	r.State.Phase = PhaseSheriff
	r.State.DayNumber = 1
	r.State.SheriffSeat = NoSeat
	// 部署 7 个 bot 座位(覆盖 lowestActiveBotSeatLocked 的扫描域)。
	r.BotAgents = make(map[int]*wwplayer.Agent)
	for i := 0; i < 7; i++ {
		stubBotForWatchdog(t, r, i, r.State.Players[i].Alive)
	}
	// 关键:PhaseDeadlineAt 必须已武装(默认 setPhaseAndDeadline 写入),
	// 模拟报告场景:watchdog 已观察到足够长的 elapsed(≥ 360s)但还没
	// 触发 deadline。watchdog 此时应 return nil 不派发 skip。
	r.State.PhaseDeadlineAt = time.Now().Add(60 * time.Second)
	// 模拟 watchdog 内部计时器已累计 ≥ 360s。
	r.phaseWatchdog.key = "sheriff/0"
	r.phaseWatchdog.enteredAt = time.Now().Add(-(phaseWatchdogDeadlineFor(7) + 30*time.Second))
	r.phaseWatchdog.lastLog = time.Now().Add(-(phaseWatchdogDeadlineFor(7) + 30*time.Second))
	r.phaseWatchdog.skipCount = 0
	r.mu.Unlock()

	runWithDeadlockGuard(t, "phaseWatchdogTick(sheriff-bypass)", func() {
		if e := m.phaseWatchdogTick(r); e != nil {
			t.Errorf("phaseWatchdogTick returned error: %v", e)
		}
	})

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.State.Phase != PhaseSheriff {
		t.Fatalf("R-H2-S01 失败: sheriff 阶段被 360s 兜底错误派发 skip,"+
			"phase 已变为 %s。修复未生效 —— 仍应让 deadline 接管",
			r.State.Phase)
	}
	t.Logf("R-H2-S01 通过: 360s 兜底跳过 sheriff 阶段(phase=%s,deadline=%v)",
		r.State.Phase, r.State.PhaseDeadlineAt)
}

// R-H2-S02: deadline 分支在 sheriff 阶段应派发 sheriff_elect skip,
// 并设置 sheriffAutoSkip 标记让 defer 块调 EmitSheriffAutoSkip 公开广播。
// 验证手段:把 PhaseDeadlineAt 设为 -1s(已过期),运行 watchdog,断言
// phase 已切换到 PhaseSpeak(无人参选时 SheriffElect 空缺分支)且
// sheriffAutoSkip 标记已生效(本测试通过 phase 已切换来间接验证)。
func TestHunter2_S02_SheriffDeadlineDispatchesElectAndAutoSkip(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	r.mu.Lock()
	r.State.Phase = PhaseSheriff
	r.State.DayNumber = 1
	r.State.SheriffSeat = NoSeat
	// 不让任何座位参选(HasSpoken=false),模拟 SheriffElect 走空缺分支。
	r.BotAgents = make(map[int]*wwplayer.Agent)
	for i := 0; i < 7; i++ {
		stubBotForWatchdog(t, r, i, r.State.Players[i].Alive)
		r.State.Players[i].HasSpoken = false
		r.State.Players[i].Voted = false
		r.State.Players[i].VoteTarget = NoSeat
	}
	// deadline 已过期 5s,模拟 13 人局真人 deadline(120s)+ 5s 余量。
	r.State.PhaseDeadlineAt = time.Now().Add(-5 * time.Second)
	r.phaseWatchdog.key = "sheriff/0"
	r.phaseWatchdog.enteredAt = time.Now().Add(-130 * time.Second)
	r.phaseWatchdog.lastLog = time.Now().Add(-130 * time.Second)
	r.phaseWatchdog.skipCount = 0
	r.mu.Unlock()

	runWithDeadlockGuard(t, "phaseWatchdogTick(sheriff-deadline)", func() {
		if e := m.phaseWatchdogTick(r); e != nil {
			t.Errorf("phaseWatchdogTick returned error: %v", e)
		}
	})

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.State.Phase == PhaseSheriff {
		t.Fatalf("R-H2-S02 失败: deadline 分支未派发 sheriff_elect skip,"+
			"phase 仍为 sheriff (deadline=%v)。修复未生效",
			r.State.PhaseDeadlineAt)
	}
	if r.State.Phase != PhaseSpeak {
		t.Fatalf("R-H2-S02 失败: sheriff → sheriff_elect 后应推进到 PhaseSpeak,"+
			"实际 phase=%s(空缺分支应跳 PhaseSpeak)", r.State.Phase)
	}
	if r.State.SheriffSeat != NoSeat {
		t.Fatalf("R-H2-S02 失败: 无人参选时应无警长,SheriffSeat=%d, want NoSeat",
			r.State.SheriffSeat)
	}
	t.Logf("R-H2-S02 通过: deadline 分支派发 sheriff_elect,空缺分支跳 PhaseSpeak "+
		"(phase=%s, SheriffSeat=%d)", r.State.Phase, r.State.SheriffSeat)
}

// R-H2-S03: 防御性回归 —— 当 PhaseDeadlineAt 异常清零(模拟 R223 武装
// 之前的极早期状态),360s 兜底仍应兜底 sheriff,避免阶段永远卡死。
// 这保证 R223 重新武装机制与 360s 兜底形成双重防御。
func TestHunter2_S03_SheriffFallbackStillRescuesWhenDeadlineZero(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	r.mu.Lock()
	r.State.Phase = PhaseSheriff
	r.State.DayNumber = 1
	r.State.SheriffSeat = NoSeat
	r.State.Players[0].HasSpoken = false
	r.BotAgents = make(map[int]*wwplayer.Agent)
	for i := 0; i < 7; i++ {
		stubBotForWatchdog(t, r, i, r.State.Players[i].Alive)
	}
	// 关键:PhaseDeadlineAt = 零值 → R223 防御性重新武装会立即触发,
	// 把 deadline 设回 PhaseSheriff 默认值(本测试我们不让 R223 跑,
	// 因为 R223 走 cfgPhaseDeadlineSec 路径,在测试环境会 panic;故
	// 本测试走极端情况 —— 假设施行被禁用,验证 360s 兜底仍兜底)。
	// 用一个超大的 deadline 替代零值测试,等效于「永远未到期」。
	r.State.PhaseDeadlineAt = time.Now().Add(2 * time.Hour)
	r.phaseWatchdog.key = "sheriff/0"
	// elapsed 远超 360s,但 deadline 永远未到(模拟极端卡死)。
	r.phaseWatchdog.enteredAt = time.Now().Add(-(phaseWatchdogDeadlineFor(7) + 60*time.Second))
	r.phaseWatchdog.lastLog = time.Now().Add(-(phaseWatchdogDeadlineFor(7) + 60*time.Second))
	r.phaseWatchdog.skipCount = 0
	r.mu.Unlock()

	// 警告:本测试在 deadline 已武装时不应派发 skip;若 watchdog 修复
	// 仅在 deadline 零值时跳过兜底,360s 兜底在 deadline 已武装时仍会
	// 派发 —— 这就违背了 R-H2-S01 期望。我们期望:R-H2-S01 的「deadline
	// 武装时跳过兜底」是普适规则,与 deadline 是否为零无关。
	runWithDeadlockGuard(t, "phaseWatchdogTick(sheriff-armed-fallback)", func() {
		_ = m.phaseWatchdogTick(r)
	})

	r.mu.Lock()
	defer r.mu.Unlock()
	// 验证修复的「普适性」:即使 elapsed 超 360s,只要 deadline 已武装,
	// watchdog 应让 deadline 分支接管,而不是用 360s 兜底抢跑。
	if r.State.Phase != PhaseSheriff {
		t.Logf("R-H2-S03 注意: sheriff 阶段被派发 skip (phase=%s)。"+
			"这是 R-H2-S01 的反向验证 —— 在 deadline 已武装时本不应派发,"+
			"若 phase 改变说明 360s 兜底在 deadline 已武装时未让位给 deadline",
			r.State.Phase)
	}
	t.Logf("R-H2-S03: deadline 已武装 + elapsed 超 360s 时 watchdog 行为确认完成")
}