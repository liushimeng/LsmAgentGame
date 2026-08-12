package werewolf

// engine_sheriff_test.go — §问题描述报告-20260804-03 警长竞选回归测试。
//
// 覆盖 8 条缺陷中可在引擎/视图层验证的部分:
//   R-S01  sheriff 阶段 SpeakTurnSeat 必须保持 NoSeat            (BUG-03)
//   R-S02  StartDay 在 sheriff 阶段仍必须失败(不是竞选出口)      (BUG-01)
//   R-S03  参选后 sheriff_candidates 必须下发                     (BUG-04)
//   R-S04  非 sheriff 阶段不得把「已发言」当「已参选」下发         (BUG-04)
//   R-S05  投票后 my_voted / my_vote_target 必须下发              (BUG-05)
//   R-S06  未投票玩家 my_vote_target 必须是 -1 而非 0             (BUG-05)
//   R-S07  死亡玩家结算 sheriff_elect 必须被拒                    (BUG-06)
//   R-S08  无人参选时 elect 走空缺分支并推进到 speak              (BUG-01/06)

import "testing"

// enterSheriffPhase 把一局推进到 PhaseSheriff。
func enterSheriffPhase(t *testing.T) *GameState {
	t.Helper()
	gs := makeStartedGame(t, 7, fillSeats("a", "b", "c", "d", "e", "f", "g"))
	gs.endWitchPhase()
	if gs.Phase != PhaseSheriff {
		t.Fatalf("expected PhaseSheriff, got %v", gs.Phase)
	}
	return gs
}

// firstAlive 返回第 n 个存活座位(n 从 0 开始)。
func firstAlive(gs *GameState, n int) Seat {
	for i := 0; i < MaxPlayers; i++ {
		if gs.AliveSeat(Seat(i)) {
			if n == 0 {
				return Seat(i)
			}
			n--
		}
	}
	return NoSeat
}

// R-S01: 警长竞选首轮没有轮流发言顺序,SpeakTurnSeat 必须是 NoSeat。
// 缺陷时前端 `speak_turn_seat + 1` 渲染出不存在的座位 #0。
func TestSheriffR_S01_SpeakTurnSeatStaysNoSeat(t *testing.T) {
	gs := enterSheriffPhase(t)
	if gs.SpeakTurnSeat != NoSeat {
		t.Fatalf("SpeakTurnSeat = %d, want NoSeat(%d) — 前端会渲染出 #%d",
			gs.SpeakTurnSeat, NoSeat, gs.SpeakTurnSeat+1)
	}
	cs := BuildClientState("r1", gs.Seats, int(firstAlive(gs, 0)), gs)
	if cs.SpeakTurnSeat != -1 {
		t.Fatalf("cs.speak_turn_seat = %d, want -1", cs.SpeakTurnSeat)
	}
}

// R-S02: StartDay 在 sheriff 阶段必须失败 —— 它只接受 PhaseDawn。
// 前端「结束竞选」按钮曾错误地发 start_day,导致 100% 必定失败。
func TestSheriffR_S02_StartDayRejectedInSheriffPhase(t *testing.T) {
	gs := enterSheriffPhase(t)
	e := gs.StartDay()
	if e == nil {
		t.Fatal("StartDay() in PhaseSheriff must fail — 前端出口必须是 SheriffElect")
	}
	if gs.Phase != PhaseSheriff {
		t.Fatalf("失败的 StartDay 不应改变 phase, got %v", gs.Phase)
	}
}

// R-S03: 参选后必须能通过 game.state 观察到 —— 否则 UI 与「按钮坏了」无法区分。
func TestSheriffR_S03_CandidateSurfacedInClientState(t *testing.T) {
	gs := enterSheriffPhase(t)
	me := firstAlive(gs, 0)

	before := BuildClientState("r1", gs.Seats, int(me), gs)
	if len(before.SheriffCandidates) != 0 {
		t.Fatalf("参选前 sheriff_candidates 应为空, got %v", before.SheriffCandidates)
	}

	if e := gs.SheriffCandidate(me); e != nil {
		t.Fatalf("SheriffCandidate: %v", e)
	}

	after := BuildClientState("r1", gs.Seats, int(me), gs)
	if len(after.SheriffCandidates) != 1 || after.SheriffCandidates[0] != int(me) {
		t.Fatalf("参选后 sheriff_candidates = %v, want [%d]", after.SheriffCandidates, me)
	}
	// 其他玩家也必须看得到候选人名单(全场可见)。
	other := firstAlive(gs, 1)
	fromOther := BuildClientState("r1", gs.Seats, int(other), gs)
	if len(fromOther.SheriffCandidates) != 1 {
		t.Fatalf("候选名单必须全场可见, other 视角 = %v", fromOther.SheriffCandidates)
	}
}

// R-S04: HasSpoken 在 PhaseSpeak 语义是「白天已发言」。若无 phase 守卫直接
// 下发,会把发言状态误当成参选状态泄漏出去。
func TestSheriffR_S04_NoCandidateLeakOutsideSheriffPhase(t *testing.T) {
	gs := enterSheriffPhase(t)
	me := firstAlive(gs, 0)
	if e := gs.SheriffCandidate(me); e != nil {
		t.Fatalf("SheriffCandidate: %v", e)
	}
	// 强制切到发言阶段(HasSpoken 仍为 true)。
	gs.startSpeakPhase()
	if gs.Phase != PhaseSpeak {
		t.Fatalf("expected PhaseSpeak, got %v", gs.Phase)
	}
	cs := BuildClientState("r1", gs.Seats, int(me), gs)
	if len(cs.SheriffCandidates) != 0 {
		t.Fatalf("非 sheriff 阶段不得下发 sheriff_candidates, got %v", cs.SheriffCandidates)
	}
	if got := gs.SheriffCandidates(); got != nil {
		t.Fatalf("SheriffCandidates() 在非 sheriff 阶段应返回 nil, got %v", got)
	}
}

// R-S05: 投票后 my_voted / my_vote_target 必须下发 —— Votes(聚合票数)
// 回答不了「我投过没 / 投给了谁」。
func TestSheriffR_S05_MyVoteStateSurfaced(t *testing.T) {
	gs := enterSheriffPhase(t)
	me := firstAlive(gs, 0)
	target := firstAlive(gs, 1)
	if e := gs.SheriffCandidate(target); e != nil {
		t.Fatalf("SheriffCandidate: %v", e)
	}
	if e := gs.DayVote(me, target); e != nil {
		t.Fatalf("DayVote: %v", e)
	}
	cs := BuildClientState("r1", gs.Seats, int(me), gs)
	if !cs.MyVoted {
		t.Fatal("my_voted 必须为 true")
	}
	if cs.MyVoteTarget != int(target) {
		t.Fatalf("my_vote_target = %d, want %d", cs.MyVoteTarget, target)
	}
	// 观战者不应拿到某个玩家的私有投票状态。
	spec := BuildClientState("r1", gs.Seats, -1, gs)
	if spec.MyVoted || spec.MyVoteTarget != -1 {
		t.Fatalf("观战者 my_voted=%v my_vote_target=%d, want false/-1",
			spec.MyVoted, spec.MyVoteTarget)
	}
}

// R-S06: my_vote_target 的默认值必须是 -1,而非 Go 零值 0。
//
// 0 是**合法座位号**(1号位) —— 与 §134 GuardLastProtect 同源的坑。
// 入座玩家路径由 Players[viewer].VoteTarget(初值 NoSeat)天然保证,
// 但 gs==nil 的早退路径(房间尚未开局)与观战者路径只能靠结构体字面量默认值。
// 缺失该默认值时,未开局房间会下发 my_vote_target=0,前端渲染成「我投了 1 号」。
func TestSheriffR_S06_UnvotedTargetDefaultsToMinusOne(t *testing.T) {
	// 早退路径:gs == nil。
	var seats [MaxPlayers]string
	empty := BuildClientState("r1", seats, 0, nil)
	if empty.MyVoteTarget != -1 {
		t.Fatalf("gs==nil 时 my_vote_target = %d, want -1(0 是合法座位号,会渲染成投给 1 号)",
			empty.MyVoteTarget)
	}
	if empty.MyVoted {
		t.Fatal("gs==nil 时 my_voted 必须为 false")
	}

	// 正常路径:已开局但尚未投票的入座玩家。
	gs := enterSheriffPhase(t)
	me := firstAlive(gs, 0)
	cs := BuildClientState("r1", gs.Seats, int(me), gs)
	if cs.MyVoted {
		t.Fatal("未投票时 my_voted 必须为 false")
	}
	if cs.MyVoteTarget != -1 {
		t.Fatalf("未投票时 my_vote_target = %d, want -1", cs.MyVoteTarget)
	}
}

// R-S07: 死亡玩家不得结算警长竞选。修复「结束按钮改发 elect」后,
// 越权问题会从「永远失败」变成「永远成功且可滥用」。
func TestSheriffR_S07_DeadPlayerCannotElect(t *testing.T) {
	gs := enterSheriffPhase(t)
	dead := firstAlive(gs, 0)
	gs.Players[dead].Alive = false

	if e := gs.SheriffElect(dead); e == nil {
		t.Fatal("死亡玩家结算 sheriff_elect 必须被拒")
	}
	if gs.Phase != PhaseSheriff {
		t.Fatalf("被拒的 elect 不应改变 phase, got %v", gs.Phase)
	}
	// 死亡玩家同样不得参选。
	if e := gs.SheriffCandidate(dead); e == nil {
		t.Fatal("死亡玩家参选必须被拒")
	}
	// NoSeat 是系统内部哨兵(watchdog / quarantine-skip),必须放行。
	if e := gs.SheriffElect(NoSeat); e != nil {
		t.Fatalf("系统哨兵 NoSeat 结算必须放行, got %v", e)
	}
}

// R-S08: 无人参选时 elect 走空缺分支,SheriffSeat 保持 NoSeat 并推进到 speak。
func TestSheriffR_S08_NoCandidateElectsNobodyAndAdvances(t *testing.T) {
	gs := enterSheriffPhase(t)
	actor := firstAlive(gs, 0)
	if e := gs.SheriffElect(actor); e != nil {
		t.Fatalf("SheriffElect: %v", e)
	}
	if gs.SheriffSeat != NoSeat {
		t.Fatalf("无人参选/投票时应无警长, got seat %d", gs.SheriffSeat)
	}
	if gs.Phase != PhaseSpeak {
		t.Fatalf("elect 后应推进到 PhaseSpeak, got %v", gs.Phase)
	}
	// 推进到发言阶段后 SpeakTurnSeat 必须是真实座位(与 R-S01 的 sheriff 阶段相反)。
	if gs.SpeakTurnSeat == NoSeat {
		t.Fatal("PhaseSpeak 必须有当前发言座位")
	}
}
