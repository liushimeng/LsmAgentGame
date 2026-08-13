// Package werewolf - upgrade_20260813_02_test.go: §20260813-02 U4 回归测试
//
// 覆盖 GodModeSnapshot 两个增量字段(夜间血迹图 S2):
//   - WolfKills           : InfoSourceNightWolfVote 账本聚合,每夜取 Seq 最大票
//   - GuardProtectEntries : InfoSourceNightGuard 的结构版(Day+Seat+Target)
//
// §20260811-08 教训 (5):「写入→解析」成对管线必须有端到端断言解析产物非空 ——
// 本文件断言 populateGodModeLocked 在有账本数据时两字段**非空且值正确**。
// §212 教训:populateGodModeLocked 是锁内变体,测试直接调用(不持真实互斥锁,
// 与 newPOVTestRoom 既有测试同范式)。

package werewolf

import (
	"testing"
	"time"
)

// TestU4_01_WolfKills_AggregatedFromLedger 狼刀历史按夜聚合:
// 同夜多狼投票取 Seq 最大者的 target;多夜按 Day 升序。
func TestU4_01_WolfKills_AggregatedFromLedger(t *testing.T) {
	r := newPOVTestRoom()
	r.infoLedger = NewInformationLedger()
	now := time.Now().UnixMilli()
	k := map[int]bool{0: true}

	// 第 1 夜:狼 A 投 4,狼 B 投 5,狼 A 改投 5(最后定刀 = 5)
	r.infoLedger.append(1, "night_wolves", InfoSourceNightWolfVote, "wolf_vote seat=0 target=4 reason=first", k, now)
	r.infoLedger.append(1, "night_wolves", InfoSourceNightWolfVote, "wolf_vote seat=4 target=5 reason=b", k, now)
	r.infoLedger.append(1, "night_wolves", InfoSourceNightWolfVote, "wolf_vote seat=0 target=5 reason=final", k, now)
	// 第 2 夜:直接刀 2
	r.infoLedger.append(2, "night_wolves", InfoSourceNightWolfVote, "wolf_vote seat=0 target=2 reason=x", k, now)

	snap := r.populateGodModeLocked()
	if snap == nil {
		t.Fatal("snapshot must not be nil")
	}
	if len(snap.WolfKills) != 2 {
		t.Fatalf("WolfKills = %d entries, want 2 (两夜)", len(snap.WolfKills))
	}
	if snap.WolfKills[0].Day != 1 || snap.WolfKills[0].Target != 5 {
		t.Errorf("night1 = %+v, want {Day:1 Target:5}(Seq 最大票定刀)", snap.WolfKills[0])
	}
	if snap.WolfKills[1].Day != 2 || snap.WolfKills[1].Target != 2 {
		t.Errorf("night2 = %+v, want {Day:2 Target:2}", snap.WolfKills[1])
	}
}

// TestU4_02_WolfKills_AbstainNightsSkipped 全狼弃权的夜不产生条目;
// 无账本时字段为空切片而非 nil(前端 .map 防崩,§44)。
func TestU4_02_WolfKills_AbstainNightsSkipped(t *testing.T) {
	r := newPOVTestRoom()
	r.infoLedger = NewInformationLedger()
	now := time.Now().UnixMilli()
	k := map[int]bool{0: true}

	// 第 1 夜:唯一一票是弃权(target=-1)
	r.infoLedger.append(1, "night_wolves", InfoSourceNightWolfVote, "wolf_vote seat=0 target=-1 reason=skip", k, now)

	snap := r.populateGodModeLocked()
	if snap == nil {
		t.Fatal("snapshot must not be nil")
	}
	if len(snap.WolfKills) != 0 {
		t.Errorf("WolfKills = %+v, want empty(全弃权夜跳过)", snap.WolfKills)
	}
	if snap.WolfKills == nil || snap.GuardProtectEntries == nil {
		t.Error("WolfKills / GuardProtectEntries 必须是空切片而非 nil(§44)")
	}
}

// TestU4_03_GuardProtectEntries_StructuredVersion 结构版守护条目与既有
// GuardProtects []int 并存,Day/Seat/Target 三字段全部正确。
func TestU4_03_GuardProtectEntries_StructuredVersion(t *testing.T) {
	r := newPOVTestRoom()
	r.infoLedger = NewInformationLedger()
	now := time.Now().UnixMilli()
	k := map[int]bool{3: true}

	r.infoLedger.append(1, "night_guard", InfoSourceNightGuard, "guard_protect seat=3 target=1", k, now)
	r.infoLedger.append(2, "night_guard", InfoSourceNightGuard, "guard_protect seat=3 target=6", k, now)

	snap := r.populateGodModeLocked()
	if snap == nil {
		t.Fatal("snapshot must not be nil")
	}
	// 既有 []int 契约不回归
	if len(snap.GuardProtects) != 2 {
		t.Fatalf("GuardProtects = %d, want 2(既有契约)", len(snap.GuardProtects))
	}
	if len(snap.GuardProtectEntries) != 2 {
		t.Fatalf("GuardProtectEntries = %d, want 2", len(snap.GuardProtectEntries))
	}
	e := snap.GuardProtectEntries[0]
	if e.Day != 1 || e.Seat != 3 || e.Target != 1 {
		t.Errorf("entry0 = %+v, want {Day:1 Seat:3 Target:1}", e)
	}
	e = snap.GuardProtectEntries[1]
	if e.Day != 2 || e.Seat != 3 || e.Target != 6 {
		t.Errorf("entry1 = %+v, want {Day:2 Seat:3 Target:6}", e)
	}
}
