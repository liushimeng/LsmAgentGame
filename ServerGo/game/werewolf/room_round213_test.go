// Package werewolf — regression test for R213 报告 §4.1 P1 修复:
//
// 当 watchdog 在 90s 兜底(或 deadline)派发 wolf_kill skip 时,如果该 wolf
// 在本轮已经投过票(WolfVoteCast[seat]=true,NightWolfKill 会拒绝 [30201]
// "you have already voted this round"),skip 会被静默吞掉 → 阶段不再推进。
//
// 在 13 人局 + 多家模型 provider 持续 400 circuit 场景下,wolf #1 在第一夜
// 反复超时触发 watchdog,但由于前序 LLM 调用可能已成功登记本狼的弃权/目标
// (WolfVoteCast[seat]=true),watchdog 后续派发的 skip 命中 30201,D1→Night2
// 延迟 ~4 分钟。
//
// 修复:在 dispatchQuarantinedSkipLocked 的 "wolf_kill" 分支中,
// 调用 wolfKillLocked 之前若检测到 WolfVoteCast[seat]=true,复位该狼的
// 本轮投票状态(WolfVoteCast[seat]=false, WolfVotes[seat]=NoSeat),
// 把 watchdog skip 视为一次合法的"重新弃权"。
package werewolf

import (
	"testing"
)

// TestDispatchQuarantinedSkip_WolfKill_ResetsPriorVote 验证 R213 P1 修复:
// 当 watchdog 在 night_wolves 阶段派出 wolf_kill skip,且 acting wolf
// 已经投过本轮票(WolfVoteCast[seat]=true),skip 必须成功登记该狼的
// 弃权/目标 —— 不能返回 [30201] "you have already voted this round"。
//
// 修复前:wolfKillLocked → NightWolfKill(seat, target) 在
// WolfVoteCast[seat]=true 时返回 ErrAlreadyWolfVoted,skip 被吞,
// phase 永远卡在 PhaseNightWolves。
//
// 修复后:dispatchQuarantinedSkipLocked 在 wolf_kill 分支检测到
// WolfVoteCast[seat]=true 时复位该槽位,再调 wolfKillLocked,
// engine 接受该次"重新弃权",phase 正常推进。
func TestDispatchQuarantinedSkip_WolfKill_ResetsPriorVote(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	r.mu.Lock()
	defer r.mu.Unlock()

	// 把阶段推进到 PhaseNightWolves,设定两个狼座位:
	//   - wolf A (acting seat):WolfVoteCast[A]=true(已投过)
	//   - wolf B:未投
	// 然后模拟 watchdog 派出 wolf_kill skip 作用于 wolf A。
	r.State.Phase = PhaseNightWolves
	r.State.Status = "playing"

	// 找两个不同的狼座位
	var wolfA, wolfB Seat = NoSeat, NoSeat
	for i := 0; i < MaxPlayers; i++ {
		if r.State.Roles[i] == RoleWerewolf && r.State.AliveSeat(Seat(i)) {
			if wolfA == NoSeat {
				wolfA = Seat(i)
			} else if wolfB == NoSeat {
				wolfB = Seat(i)
				break
			}
		}
	}
	if wolfA == NoSeat || wolfB == NoSeat {
		t.Fatalf("need at least 2 living wolves for this test, got A=%d B=%d", wolfA, wolfB)
	}

	// 让 wolf A 处于"已投票"状态(模拟前序 LLM 成功调用了 wolf_kill),
	// 但目标存活,合法。
	// 找一个非狼存活目标作为合法 target(用于 R73-P1 fallback 选择)。
	var target Seat = NoSeat
	for i := 0; i < MaxPlayers; i++ {
		if Seat(i) != wolfA && r.State.AliveSeat(Seat(i)) && r.State.Roles[i] != RoleWerewolf {
			target = Seat(i)
			break
		}
	}
	if target == NoSeat {
		t.Fatalf("no alive non-wolf target available")
	}

	r.State.WolfVotes[wolfA] = target // 之前投过这个目标
	r.State.WolfVoteCast[wolfA] = true // 标记为已投
	r.State.TurnActingSeat = wolfA     // acting seat = wolfA(触发 watchdog)
	r.State.DayNumber = 1

	// R213-P1:派发 wolf_kill skip。在修复前,这会返回 [30201]。
	if e := m.dispatchQuarantinedSkipLocked(r, int(wolfA), "wolf_kill", -1); e != nil {
		t.Fatalf("dispatchQuarantinedSkipLocked(wolf_kill) on already-voted wolf must "+
			"succeed (BUG-R213-P1: prior vote state should be reset), got %d (%s)",
			e.Code, e.Message)
	}

	// wolfA 必须被标记为已投(WolfVoteCast[wolfA]=true),
	// 因为 skip 被视为一次合法的"重新投票/弃权"。
	if !r.State.WolfVoteCast[wolfA] {
		t.Errorf("wolfA WolfVoteCast must remain true after skip reset+re-vote, got false "+
			"(BUG-R213-P1: skip may not have called wolfKillLocked)")
	}
	// wolfA 的新投票必须是合法目标(由 R73-P1 fallback 选出,非 NoSeat)。
	if r.State.WolfVotes[wolfA] != target {
		t.Errorf("wolfA new WolfVotes = %d, want %d (R73-P1 fallback target)",
			r.State.WolfVotes[wolfA], target)
	}
	// wolfB 票状态不受影响(只清当前 acting seat 的槽位)。
	if r.State.WolfVoteCast[wolfB] {
		t.Errorf("wolfB WolfVoteCast must remain false (only acting seat is reset), got true")
	}
}

// TestDispatchQuarantinedSkip_WolfKill_NotYetVoted_NoReset 验证未投票的狼
// 走 skip 时行为不变:直接派发 wolfKillLocked → NightWolfKill 成功,
// WolfVoteCast[seat] 被设为 true,无需任何复位逻辑。
func TestDispatchQuarantinedSkip_WolfKill_NotYetVoted_NoReset(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	r.mu.Lock()
	defer r.mu.Unlock()

	r.State.Phase = PhaseNightWolves
	r.State.Status = "playing"

	wolfA := NoSeat
	for i := 0; i < MaxPlayers; i++ {
		if r.State.Roles[i] == RoleWerewolf && r.State.AliveSeat(Seat(i)) {
			wolfA = Seat(i)
			break
		}
	}
	if wolfA == NoSeat {
		t.Fatalf("need at least 1 living wolf for this test")
	}

	var target Seat = NoSeat
	for i := 0; i < MaxPlayers; i++ {
		if Seat(i) != wolfA && r.State.AliveSeat(Seat(i)) && r.State.Roles[i] != RoleWerewolf {
			target = Seat(i)
			break
		}
	}
	if target == NoSeat {
		t.Fatalf("no alive non-wolf target")
	}

	r.State.TurnActingSeat = wolfA
	r.State.DayNumber = 1
	// WolfVoteCast[wolfA] 默认 false(未投)

	if e := m.dispatchQuarantinedSkipLocked(r, int(wolfA), "wolf_kill", -1); e != nil {
		t.Fatalf("dispatchQuarantinedSkipLocked(wolf_kill) on fresh wolf must succeed, got %d (%s)",
			e.Code, e.Message)
	}

	if !r.State.WolfVoteCast[wolfA] {
		t.Fatalf("wolfA WolfVoteCast must be true after skip")
	}
	if r.State.WolfVotes[wolfA] != target {
		t.Errorf("wolfA WolfVotes = %d, want %d", r.State.WolfVotes[wolfA], target)
	}
}
