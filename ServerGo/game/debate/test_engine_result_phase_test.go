// Package debate — result 阶段推进测试(2026-08-31 §20260831-11 R8 P1-B)。
//
// 覆盖 R8 测试报告 §1.4「result 阶段不推进 game_over」的两处根因修复:
//   - runResultPhase 改 deadline 驱动 + advanceTo(触发 debate.phase 广播)
//   - Run() 主循环不再把 PhaseResult 当终局(IsGameOver 旧语义导致主循环
//     在进入 result 后立即退出,runResultPhase 从未被调用)
//   - forceGameOverIfResultStuck 防御性 watchdog(报告 §6.2 建议)
//
// 测试不依赖数据库:NewDebateManager 无 gormDB,persistence 钩子未注入时降级 no-op。
package debate

import (
	"context"
	"sync"
	"testing"
	"time"
)

// ============================================================================
// 测试基建
// ============================================================================

// newResultPhaseTestEnv 构造带 2 队 × 2 人 + 1 裁判的房间、已启动 ctx 的引擎,
// 并注册 phase/game_over 钩子记录器。resultShowSec 允许覆写(测 deadline<=0 兜底)。
func newResultPhaseTestEnv(t *testing.T, resultShowSec int) (*DebateManager, *DebateRoom, *DebateEngine, *resultPhaseHookRecorder, context.CancelFunc) {
	t.Helper()
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
		Judges:   []JudgeConfig{{JudgeID: 0, ModelKey: "m1"}},
		CreatedBy: "user1",
	}
	if resultShowSec > 0 {
		cfg.PhaseConfig.ResultShowSec = resultShowSec
	}
	room, e := mgr.CreateRoom(cfg)
	if e != nil {
		t.Fatalf("CreateRoom failed: %v", e.Message)
	}
	eng := NewDebateEngine(room, mgr)
	ctx, cancel := context.WithCancel(context.Background())
	eng.ctx = ctx
	eng.cancel = cancel

	rec := &resultPhaseHookRecorder{}
	mgr.SetOnPhaseChange(rec.recordPhase)
	mgr.SetOnGameOver(rec.recordGameOver)
	return mgr, room, eng, rec, cancel
}

// resultPhaseHookRecorder 记录 onPhaseChange / onGameOver 触发情况。
//
// 两个钩子均由 advanceTo / runResultPhase 以 `go fn(...)` 异步触发,必须加锁 + 轮询断言。
type resultPhaseHookRecorder struct {
	mu           sync.Mutex
	phaseChanges []Phase
	gameOvers    int
}

func (rec *resultPhaseHookRecorder) recordPhase(_ string, p Phase) {
	rec.mu.Lock()
	rec.phaseChanges = append(rec.phaseChanges, p)
	rec.mu.Unlock()
}

func (rec *resultPhaseHookRecorder) recordGameOver(string) {
	rec.mu.Lock()
	rec.gameOvers++
	rec.mu.Unlock()
}

func (rec *resultPhaseHookRecorder) phaseChangeCount(p Phase) int {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	n := 0
	for _, got := range rec.phaseChanges {
		if got == p {
			n++
		}
	}
	return n
}

func (rec *resultPhaseHookRecorder) gameOverCount() int {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return rec.gameOvers
}

// expirePhaseDeadline 把当前阶段 deadline 直接改写为「pastSec 秒前」。
//
// 同包测试专用:走 r.mu 写锁,不绕过锁;模拟「result 倒计时已结束」的 R8 现场。
func expirePhaseDeadline(room *DebateRoom, pastSec int64) {
	room.mu.Lock()
	room.phaseDeadline = WallNow() - pastSec
	room.mu.Unlock()
}

// zeroPhaseDeadline 把 deadline 清零,模拟「未经 SetPhase 进入 result」的异常路径。
func zeroPhaseDeadline(room *DebateRoom) {
	room.mu.Lock()
	room.phaseDeadline = 0
	room.mu.Unlock()
}

// pollCondition 轮询等待条件成立(超时 fatal),用于异步钩子断言。
func pollCondition(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	dl := time.Now().Add(timeout)
	for time.Now().Before(dl) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal(msg)
}

// ============================================================================
// 用例 1:runResultPhase deadline 驱动推进
// ============================================================================

// TestRunResultPhaseDeadlineDrivenAdvance deadline 已过的 result 必须在 ~1s 内
// 推进到 game_over,且经 advanceTo 触发 onPhaseChange 广播 + onGameOver 回调。
//
// 旧实现缺陷:固定计数循环 + 裸 SetPhase —— deadline 已过仍要数满 showSec 秒,
// 且前端收不到 debate.phase 终局帧(R8 P1-A 同源)。
func TestRunResultPhaseDeadlineDrivenAdvance(t *testing.T) {
	_, room, eng, rec, cancel := newResultPhaseTestEnv(t, 0)
	defer cancel()

	room.SetPhase(PhaseResult) // deadline = now + ResultShowSec(Quick: 20s)
	expirePhaseDeadline(room, 10)

	start := time.Now()
	eng.runResultPhase()

	if room.Phase() != PhaseGameOver {
		t.Fatalf("阶段 = %s, want %s(deadline 已过应立即推进)", room.Phase(), PhaseGameOver)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("deadline 已过时推进耗时 %v,应在 ~1s 轮询粒度内完成", elapsed)
	}
	if room.FinishedAt() == 0 {
		t.Error("终局后 FinishedAt 应被写入(SetPhase(PhaseGameOver) 语义)")
	}
	pollCondition(t, time.Second, func() bool { return rec.phaseChangeCount(PhaseGameOver) >= 1 },
		"advanceTo 应触发 onPhaseChange(PhaseGameOver) 广播")
	pollCondition(t, time.Second, func() bool { return rec.gameOverCount() >= 1 },
		"runResultPhase 应触发 onGameOver 回调")
}

// TestRunResultPhaseNoPrematureAdvance deadline 未到时不得提前推进;
// 引擎 ctx 取消时提前返回且 phase 保持 result(交由 manager 拆除路径收尾)。
func TestRunResultPhaseNoPrematureAdvance(t *testing.T) {
	_, room, eng, rec, cancel := newResultPhaseTestEnv(t, 0)
	defer cancel()

	room.SetPhase(PhaseResult) // deadline = now + 20s,远未到

	done := make(chan struct{})
	go func() {
		eng.runResultPhase()
		close(done)
	}()

	// 300ms 后取消 ctx → runResultPhase 必须提前返回且不推进
	time.Sleep(300 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ctx 取消后 runResultPhase 应提前返回")
	}
	if room.Phase() != PhaseResult {
		t.Errorf("阶段 = %s, want %s(deadline 未到不应推进)", room.Phase(), PhaseResult)
	}
	if rec.phaseChangeCount(PhaseGameOver) != 0 || rec.gameOverCount() != 0 {
		t.Error("deadline 未到 / ctx 取消时不应触发终局广播与 onGameOver")
	}
}

// TestRunResultPhaseDeadlineZeroFallback deadline 未初始化(=0)时按
// ResultShowSec 重算兜底,最终仍推进 game_over。
func TestRunResultPhaseDeadlineZeroFallback(t *testing.T) {
	_, room, eng, _, cancel := newResultPhaseTestEnv(t, 1) // ResultShowSec = 1s
	defer cancel()

	room.SetPhase(PhaseResult)
	zeroPhaseDeadline(room)

	done := make(chan struct{})
	go func() {
		eng.runResultPhase()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("deadline=0 兜底路径应在 showSec(1s)+轮询粒度内推进")
	}
	if room.Phase() != PhaseGameOver {
		t.Errorf("阶段 = %s, want %s(deadline=0 兜底应推进)", room.Phase(), PhaseGameOver)
	}
}

// ============================================================================
// 用例 2:Run() 主循环 — 根因回归 + 卡死兜底 watchdog
// ============================================================================

// TestRunLoopAdvancesResultToGameOver 主循环进入 result 后不得提前退出,
// 必须经 runResultPhase 推进到 game_over 再退出。
//
// 根因回归用例:旧 Run() 用 IsGameOver() 判退出,而它对 PhaseResult 也返回
// true → runJudgingPhase → advanceTo(PhaseResult) 后主循环立即退出,
// runResultPhase 永远不会被调用(R8 实测卡 result 90s+)。
func TestRunLoopAdvancesResultToGameOver(t *testing.T) {
	_, room, eng, rec, cancel := newResultPhaseTestEnv(t, 0)
	defer cancel()

	room.SetPhase(PhaseResult)
	expirePhaseDeadline(room, 10)

	runDone := make(chan struct{})
	go func() {
		eng.Run()
		close(runDone)
	}()

	pollCondition(t, 5*time.Second, func() bool { return room.Phase() == PhaseGameOver },
		"Run() 主循环应把 result 推进到 game_over(而非提前退出卡在 result)")
	select {
	case <-runDone:
	case <-time.After(2 * time.Second):
		t.Fatal("phase 到达 game_over 后 Run() 应退出")
	}
	pollCondition(t, time.Second, func() bool { return rec.phaseChangeCount(PhaseGameOver) >= 1 },
		"Run() 推进终局应触发 onPhaseChange(PhaseGameOver) 广播")
	pollCondition(t, time.Second, func() bool { return rec.gameOverCount() >= 1 },
		"Run() 推进终局应触发 onGameOver 回调")
}

// TestRunLoopChainsJudgingToGameOver 全链路回归:judging(评分已齐)→ result
// → game_over 必须自动贯通。
//
// 这是 R8 P1-B 的**现场复现**用例:phase 在 handlePhase(PhaseJudging) 内部
// 变为 result —— 旧 Run() 的 IsGameOver() 退出判定在此刻即返回 true,主循环
// 直接退出,runResultPhase 从未被调用,phase 永远停留 result。
// (TestRunLoopAdvancesResultToGameOver 是"引擎启动时已在 result"的简化变体,
// 不覆盖该转换时序,故单独补此用例。)
func TestRunLoopChainsJudgingToGameOver(t *testing.T) {
	_, room, eng, rec, cancel := newResultPhaseTestEnv(t, 1) // ResultShowSec=1s,缩短 result 展示
	defer cancel()

	// 预置满额评分(1 裁判)→ runJudgingPhase 首个 1s 轮询即收齐
	room.AddJudgeScore(JudgeScore{
		JudgeID: 0, ModelKey: "m1", WinnerTeamID: 0,
		Rankings: []TeamRanking{
			{TeamID: 0, Scores: ScoreDimensions{8, 8, 7, 8, 7}, TotalScore: 38, Comment: "good"},
			{TeamID: 1, Scores: ScoreDimensions{6, 7, 7, 6, 7}, TotalScore: 33, Comment: "ok"},
		},
	})
	room.SetPhase(PhaseJudging)

	runDone := make(chan struct{})
	go func() {
		eng.Run()
		close(runDone)
	}()

	pollCondition(t, 10*time.Second, func() bool { return room.Phase() == PhaseGameOver },
		"judging → result → game_over 未自动贯通(R8 P1-B 现场复现:旧 IsGameOver 退出判定使主循环卡在 result)")
	select {
	case <-runDone:
	case <-time.After(2 * time.Second):
		t.Fatal("phase 到达 game_over 后 Run() 应退出")
	}
	pollCondition(t, time.Second, func() bool { return rec.phaseChangeCount(PhaseResult) >= 1 },
		"judging 完成应广播 onPhaseChange(PhaseResult)")
	pollCondition(t, time.Second, func() bool { return rec.phaseChangeCount(PhaseGameOver) >= 1 },
		"result 到期应广播 onPhaseChange(PhaseGameOver)")
	pollCondition(t, time.Second, func() bool { return rec.gameOverCount() >= 1 },
		"终局应触发 onGameOver 回调")
}

// TestForceGameOverIfResultStuckWatchdog 卡死兜底分支生效判定。
func TestForceGameOverIfResultStuckWatchdog(t *testing.T) {
	t.Run("命中_超期强制终局", func(t *testing.T) {
		_, room, eng, rec, cancel := newResultPhaseTestEnv(t, 0)
		defer cancel()

		room.SetPhase(PhaseResult)
		expirePhaseDeadline(room, 30) // 超过 5s 调度余量

		if !eng.forceGameOverIfResultStuck() {
			t.Fatal("result 超 deadline+5s 应命中兜底(返回 true)")
		}
		if room.Phase() != PhaseGameOver {
			t.Errorf("阶段 = %s, want %s", room.Phase(), PhaseGameOver)
		}
		pollCondition(t, time.Second, func() bool { return rec.phaseChangeCount(PhaseGameOver) >= 1 },
			"兜底推进应触发 onPhaseChange(PhaseGameOver) 广播")
		pollCondition(t, time.Second, func() bool { return rec.gameOverCount() >= 1 },
			"兜底推进应触发 onGameOver 回调")
	})

	t.Run("幂等_已终局不重复触发", func(t *testing.T) {
		_, room, eng, rec, cancel := newResultPhaseTestEnv(t, 0)
		defer cancel()

		room.SetPhase(PhaseResult)
		expirePhaseDeadline(room, 30)
		if !eng.forceGameOverIfResultStuck() {
			t.Fatal("首次应命中兜底")
		}
		// 先等首次异步 onGameOver 落定,再验证第二次调用不再补触发
		pollCondition(t, time.Second, func() bool { return rec.gameOverCount() >= 1 },
			"首次兜底应触发 onGameOver")
		before := rec.gameOverCount()
		if eng.forceGameOverIfResultStuck() {
			t.Fatal("phase 已是 game_over 时再次调用不应命中(幂等)")
		}
		time.Sleep(300 * time.Millisecond)
		if got := rec.gameOverCount(); got != before {
			t.Errorf("onGameOver 触发 %d 次, want %d(幂等调用不得重复触发)", got, before)
		}
		if room.Phase() != PhaseGameOver {
			t.Errorf("阶段 = %s, want %s", room.Phase(), PhaseGameOver)
		}
	})

	t.Run("未超期_不触发", func(t *testing.T) {
		_, room, eng, _, cancel := newResultPhaseTestEnv(t, 0)
		defer cancel()

		room.SetPhase(PhaseResult) // deadline = now + 20s
		if eng.forceGameOverIfResultStuck() {
			t.Fatal("deadline 未超 5s 余量时不应触发兜底")
		}
		if room.Phase() != PhaseResult {
			t.Errorf("阶段 = %s, want %s(未超期不应变更)", room.Phase(), PhaseResult)
		}
	})

	t.Run("ctx已取消_交由manager收尾", func(t *testing.T) {
		_, room, eng, _, cancel := newResultPhaseTestEnv(t, 0)
		defer cancel()

		room.SetPhase(PhaseResult)
		expirePhaseDeadline(room, 30)
		cancel() // 引擎拆除中:StopGame/Disband 自带 SetPhase(PhaseGameOver)+onGameOver
		if eng.forceGameOverIfResultStuck() {
			t.Fatal("引擎 ctx 已取消时不应再补触发兜底(避免与拆除路径重复回调)")
		}
		if room.Phase() != PhaseResult {
			t.Errorf("阶段 = %s, want %s(拆除路径接管)", room.Phase(), PhaseResult)
		}
	})
}
