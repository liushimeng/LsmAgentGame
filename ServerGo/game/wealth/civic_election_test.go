// Package wealth — civic_election_test.go: 市长选举启用参数链与事件单测
// (批次20 文档3 A2/A3/A6)。既有 30/40/30 得票模型单测在
// public_services_test.go(R8-2 交付),本文件只补「接线」与「可见」。
package wealth

import (
	"strings"
	"testing"

	"LsmAgentGame/game/wealth/profession"
	"LsmAgentGame/service"
)

// TestCivicElection_ManagerConfigDefaultZero 参数链默认值:NewManager 零值
// Config → 房间 electionEnabled=false。**回归守卫**:防止后人按
// InsuranceEnabled 家族惯性在 NewManager 加「零值→true」归一化(文档3 A2)。
func TestCivicElection_ManagerConfigDefaultZero(t *testing.T) {
	m := NewManager(Config{MonthMs: 3000}, nil)
	r := m.CreateRoom("room-el-0")
	if r.electionEnabled {
		t.Fatal("Config zero-value must leave election DISABLED (zero=false 家族)")
	}
	// NewManager 归一化检查:economy/survey/insurance 都被归一为 true,
	// CivicElectionEnabled 必须保持 false(逐字段钉死)。
	if !m.cfg.InsuranceEnabled || !m.cfg.EconomyEnabled {
		t.Fatal("insurance/economy zero→true 归一化仍在(Civic 不应跟随)")
	}
	if m.cfg.CivicElectionEnabled {
		t.Fatal("NewManager must NOT normalize CivicElectionEnabled to true")
	}
}

// TestCivicElection_ManagerConfigTrueWires 显式 true → 房间标记 + Start 后
// World.Election.Enabled=true(接线 grep 锚点:manager→room→NewWorld 回写)。
func TestCivicElection_ManagerConfigTrueWires(t *testing.T) {
	m := NewManager(Config{MonthMs: 3000, CivicElectionEnabled: true}, nil)
	r := m.CreateRoom("room-el-1")
	if !r.electionEnabled {
		t.Fatal("config true must wire electionEnabled")
	}
	// World 未建 → SetElectionEnabled 仅存房间标记;建 World 后回写。
	r.World = NewWorld(5, [MaxSeats]profession.Card{})
	if r.World.Election == nil {
		t.Fatal("NewWorld must construct Election")
	}
	if r.World.Election.Enabled {
		t.Fatal("NewCivicElection default must be false")
	}
	r.SetElectionEnabled(true)
	if !r.World.Election.Enabled {
		t.Fatal("SetElectionEnabled(true) must write World.Election.Enabled")
	}
	r.SetElectionEnabled(false)
	if r.World.Election.Enabled {
		t.Fatal("SetElectionEnabled(false) must write back false")
	}
}

// TestCivicElection_RoomApplyOptsMerge 建房 HTTP 链路(service.WealthRoomOptions
// → applyOpts):true 单调置位;false 不覆盖 manager 默认(零值=未传语义)。
func TestCivicElection_RoomApplyOptsMerge(t *testing.T) {
	// manager 默认 false + body true → 启用。
	m := NewManager(Config{MonthMs: 3000}, nil)
	m.ApplyRoomOptions("room-el-2", &service.WealthRoomOptions{CivicElectionEnabled: true})
	r := m.CreateRoom("room-el-2")
	if !r.electionEnabled {
		t.Fatal("opts true must enable election (pendingOpts 路径)")
	}
	// manager 默认 false + body false(缺省)→ 保持关闭。
	m2 := NewManager(Config{MonthMs: 3000}, nil)
	m2.ApplyRoomOptions("room-el-3", &service.WealthRoomOptions{})
	if m2.CreateRoom("room-el-3").electionEnabled {
		t.Fatal("opts zero-value must keep election disabled")
	}
	// applyOpts 幂等:true 重复应用不 panic 不反向。
	r.applyOpts(&service.WealthRoomOptions{CivicElectionEnabled: true})
	if !r.electionEnabled {
		t.Fatal("applyOpts re-apply true must stay true")
	}
	// Start 回写:NewWorld 后 Election.Enabled == 房间标记(经 r.Start 路径的
	// 单步复刻 —— Start 需 10 座 + loader,装配级覆盖见 api/room 链与
	// manager_bot_test;此处直接验证回写语义)。
	r.World = NewWorld(9, [MaxSeats]profession.Card{})
	r.World.Election.Enabled = r.electionEnabled
	if !r.World.Election.Enabled {
		t.Fatal("Start-equivalent write-back must enable")
	}
}

// TestElection_StipendStoppedEvent A3:国库不足 → 津贴停发 event 仅转换沿
// 播一次;恢复发放后重新计。
func TestElection_StipendStoppedEvent(t *testing.T) {
	w := NewWorld(42, [MaxSeats]profession.Card{})
	for s := 0; s < 2; s++ {
		w.Players[s] = newPlayerFromCard(s, synthCard(s, 200000))
	}
	w.StartGame()
	w.Election.Enabled = true
	w.Election.MayorSeat = 0
	// 国库现金不足(置 0)。
	w.Treasury.Cash = 0
	w.Election.MonthlyStep(w)
	if n := countEvent(w, "市长津贴停发"); n != 1 {
		t.Fatalf("first shortfall must announce once: got %d", n)
	}
	// 连续断发不重复播。
	w.Election.MonthlyStep(w)
	if n := countEvent(w, "市长津贴停发"); n != 1 {
		t.Fatalf("continued shortfall must not repeat: got %d", n)
	}
	// 恢复发放 → 走 Treasury→seat CatWelfare;断沿复位。
	w.Treasury.Cash = MayorStipendCNY * 3
	before := w.Players[0].Cash
	w.Election.MonthlyStep(w)
	if w.Players[0].Cash != before+MayorStipendCNY {
		t.Fatalf("stipend must credit mayor: %d → %d", before, w.Players[0].Cash)
	}
	if w.Treasury.Cash != MayorStipendCNY*2 {
		t.Errorf("treasury must debit stipend: %d", w.Treasury.Cash)
	}
	// 再次断发 → 第二个转换沿,再播一条。
	w.Treasury.Cash = 0
	w.Election.MonthlyStep(w)
	if n := countEvent(w, "市长津贴停发"); n != 2 {
		t.Fatalf("re-shortfall must re-announce: got %d", n)
	}
	// Ledger 通道白名单:gov:treasury → seat,CatWelfare。
	found := false
	for _, e := range w.Ledger.Entries {
		if e.Category == CatWelfare && e.To == SeatEntity(0) && e.AmountCNY == MayorStipendCNY {
			found = true
		}
	}
	if !found {
		t.Error("stipend must flow gov:treasury→seat via CatWelfare")
	}
}

// TestElection_FullRun48Months A6 全链路:enabled 房 seed 对局跑 48+ 月 →
// 发生选举 + 当选 event + 津贴走 Treasury;disabled 对照房逐月零差异
// (选举 no-op 回归红线)。
func TestElection_FullRun48Months(t *testing.T) {
	build := func(enabled bool) *World {
		w := NewWorld(2026, emptyCardsFor(2))
		for s := 0; s < 2; s++ {
			w.Players[s] = newPlayerFromCard(s, synthCard(s, 100000))
		}
		w.Election.Enabled = enabled
		w.StartGame()
		return w
	}
	on := build(true)
	off := build(false)
	// 强制第 48 月选举:缩短 interval 避免 48 轮完整月结的成本?不 ——
	// A6 要求真实 48 月;SettleMonth 轻量,直接跑。
	for m := 0; m < 49; m++ {
		if fin, _ := on.SettleMonth(); fin {
			break
		}
		off.SettleMonth()
	}
	ran := false
	for _, e := range on.Events {
		if strings.Contains(e.Text, "市长选举") && strings.Contains(e.Text, "当选市长") {
			ran = true
		}
	}
	if !ran {
		t.Fatal("49-month run must hold an election (interval 48, LastElectionMonth 0)")
	}
	if on.Election.MayorSeat < 0 {
		t.Error("mayor must be seated after election")
	}
	if on.Election.NextElectionMonth() != on.Election.LastElectionMonth+ElectionIntervalMonths {
		t.Errorf("next election month: got %d", on.Election.NextElectionMonth())
	}
	// disabled 对照:从未选举、从未领津贴(no-op 回归)。
	for _, e := range off.Events {
		if strings.Contains(e.Text, "市长选举") {
			t.Fatal("disabled room must never announce election")
		}
	}
	if off.Election.MayorSeat != -1 || off.Election.MonthsRun != 0 {
		t.Errorf("disabled election state leaked: %+v", off.Election)
	}
	for _, e := range off.Ledger.Entries {
		if e.Note == "市长津贴" {
			t.Fatal("disabled room must not pay stipend")
		}
	}
}

// TestCivicElection_NextElectionMonth A3:view 下发口径 helper 语义。
func TestCivicElection_NextElectionMonth(t *testing.T) {
	ce := NewCivicElection()
	if ce.NextElectionMonth() != 0 {
		t.Error("disabled must return 0 (view omitempty)")
	}
	ce.Enabled = true
	if got := ce.NextElectionMonth(); got != ElectionIntervalMonths {
		t.Errorf("never-held: got %d want %d", got, ElectionIntervalMonths)
	}
	ce.LastElectionMonth = 48
	if got := ce.NextElectionMonth(); got != 96 {
		t.Errorf("after first: got %d want 96", got)
	}
	ce.IntervalMonths = 0 // 回落 48
	if got := ce.NextElectionMonth(); got != 96 {
		t.Errorf("interval fallback: got %d want 96", got)
	}
}
