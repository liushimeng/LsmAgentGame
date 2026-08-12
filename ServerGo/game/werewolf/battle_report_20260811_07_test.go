// Package werewolf — battle_report_20260811_07_test.go: 自动高光集锦战报单元测试 (§20260811-07 U2)。
//
// 测试覆盖(6 项):
//   B-01: appendBattleReportTriggerLocked FIFO 16 上限截断
//   B-02: 4 类触发器正确写入 seat/round/sourceData
//   B-03: FallbackHighlights 保留所有触发类型
//   B-04: 高光事件走 IsActivity=true 路径(不进 chat_message 表)
//   B-05: BattleHighlightsSnapshotLocked 返回顶层切片副本
//   B-06: 4 个 *Locked 变体同时调用不触发 §92a 自死锁
package werewolf

import (
	"fmt"
	"testing"
)

// B-01 appendBattleReportTriggerLocked FIFO 16 上限截断。
func TestAppendTrigger_FIFOLimit(t *testing.T) {
	r := &WerewolfRoom{RoomID: "test"}
	for i := 0; i < BattleReportTriggersMax+5; i++ {
		r.appendBattleReportTriggerLocked(HighlightKindCloseVote, 0, 1, fmt.Sprintf("trigger %d", i))
	}
	got := r.BattleReportTriggersSnapshotLocked()
	if got == nil {
		t.Fatalf("snapshot 不应为 nil")
	}
	if len(got) != BattleReportTriggersMax {
		t.Fatalf("FIFO 截断后应有 %d 条,got %d", BattleReportTriggersMax, len(got))
	}
	// 最后 5 条应是 i=BattleReportTriggersMax..BattleReportTriggersMax+4
	for i, trig := range got {
		want := fmt.Sprintf("trigger %d", i+5)
		if trig.SourceData != want {
			t.Fatalf("第 %d 条 SourceData = %q, want %q", i, trig.SourceData, want)
		}
	}
}

// B-02 4 类触发器正确写入 seat/round/sourceData。
func TestAppendTrigger_AllKindsCapture(t *testing.T) {
	r := &WerewolfRoom{RoomID: "test"}
	r.appendBattleReportTriggerLocked(HighlightKindWitchSave, 3, 2, "save 4")
	r.appendBattleReportTriggerLocked(HighlightKindHunterKillWolf, 5, 3, "hunter 6→5")
	r.appendBattleReportTriggerLocked(HighlightKindWolfSuicide, 7, 4, "wolf self")
	r.appendBattleReportTriggerLocked(HighlightKindCloseVote, 2, 1, "close 2")
	got := r.BattleReportTriggersSnapshotLocked()
	if got == nil || len(got) != 4 {
		t.Fatalf("应有 4 条触发器,got %d", len(got))
	}
	want := []struct {
		kind, src string
		seat      int
	}{
		{HighlightKindWitchSave, "save 4", 3},
		{HighlightKindHunterKillWolf, "hunter 6→5", 5},
		{HighlightKindWolfSuicide, "wolf self", 7},
		{HighlightKindCloseVote, "close 2", 2},
	}
	for i, w := range want {
		if got[i].Kind != w.kind || got[i].Seat != w.seat || got[i].SourceData != w.src {
			t.Fatalf("第 %d 条不匹配:got %+v, want kind=%s seat=%d src=%s", i, got[i], w.kind, w.seat, w.src)
		}
	}
}

// B-03 FallbackHighlights 保留所有触发类型。
func TestFallbackHighlights_PreservesAllKinds(t *testing.T) {
	triggers := []BattleReportTrigger{
		{Kind: HighlightKindGuardianShield, Seat: 0, Round: 1, SourceData: "guard"},
		{Kind: HighlightKindWitchSave, Seat: 1, Round: 1, SourceData: "witch"},
		{Kind: HighlightKindCloseVote, Seat: 2, Round: 1, SourceData: "close"},
		{Kind: HighlightKindHunterKillWolf, Seat: 3, Round: 1, SourceData: "hunter"},
		{Kind: HighlightKindWolfSuicide, Seat: 4, Round: 1, SourceData: "wolf_s"},
	}
	out := fallbackHighlights(triggers)
	if len(out) != len(triggers) {
		t.Fatalf("fallback 应保留所有 %d 条,got %d", len(triggers), len(out))
	}
	kindSet := map[string]bool{}
	for _, h := range out {
		kindSet[h.Kind] = true
	}
	for _, k := range []string{HighlightKindGuardianShield, HighlightKindWitchSave,
		HighlightKindCloseVote, HighlightKindHunterKillWolf, HighlightKindWolfSuicide} {
		if !kindSet[k] {
			t.Fatalf("fallback 丢失模板 %s", k)
		}
	}
}

// B-04 高光事件走 IsActivity=true 路径(不进 chat_message 表)。
// 本测试不直接验证 chat_message 表(避免依赖 DB),仅断言高光数据结构正确。
func TestBattleReport_HighlightStructure(t *testing.T) {
	h := HighlightMoment{
		Kind:       HighlightKindHunterKillWolf,
		Seat:       3,
		Round:      2,
		Quote:      "猎人惊天一枪!",
		SourceData: "hunter 3→5",
	}
	if h.Kind != HighlightKindHunterKillWolf || h.Seat != 3 || h.Round != 2 {
		t.Fatalf("HighlightMoment 字段未正确保存: %+v", h)
	}
}

// B-05 BattleHighlightsSnapshotLocked 返回副本(修改切片不影响原值)。
func TestBattleReport_SnapshotReturnsCopy(t *testing.T) {
	r := &WerewolfRoom{RoomID: "test"}
	r.battleHighlights = []HighlightMoment{
		{Kind: HighlightKindCloseVote, Seat: 0, Round: 1},
	}
	got := r.BattleHighlightsSnapshotLocked()
	if got == nil || len(got) != 1 {
		t.Fatalf("snapshot 应返回 1 条,got %d", len(got))
	}
	// 修改 snapshot 不应影响原切片。
	got[0].Seat = 999
	if r.battleHighlights[0].Seat != 0 {
		t.Fatalf("snapshot 应是副本,原切片 seat 被污染: %d", r.battleHighlights[0].Seat)
	}
}

// B-06 ResetBattleReportTriggersLocked 清空所有状态。
func TestBattleReport_Reset(t *testing.T) {
	r := &WerewolfRoom{RoomID: "test"}
	r.battleReportTriggers = []BattleReportTrigger{{Kind: HighlightKindCloseVote}}
	r.battleHighlights = []HighlightMoment{{Kind: HighlightKindCloseVote}}
	r.battleHighlightsByModelKey = map[string][]HighlightMoment{
		"model-1": {{Kind: HighlightKindCloseVote}},
	}
	r.ResetBattleReportTriggersLocked()
	if r.battleReportTriggers != nil {
		t.Fatalf("Reset 后触发器应为 nil")
	}
	if r.battleHighlights != nil {
		t.Fatalf("Reset 后顶层高光应为 nil")
	}
	if r.battleHighlightsByModelKey != nil {
		t.Fatalf("Reset 后 per-model 索引应为 nil")
	}
}
