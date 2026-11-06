// Package virtual_city — save_load_test.go: 批次86 世界快照存盘 / 读档单测
// (2026-11-06 §可靠性与可观测性 §3.2)。
//
// 覆盖(契约 §7.1):
//   - 快照 round-trip 后 StateHash 一致(除 Rand 内部态);
//   - Rand 重播种 / startAge 恢复 / nil 子系统惰性初始化;
//   - 元信息 / 列表(降序)/ 删除(不存在 = no-op)/ 非法 id 拒绝;
//   - auto_rollback 快照保留最近 3 份;
//   - recovery 快照恒 1 份(固定 id "recovery")。
package virtual_city

import (
	"testing"
)

// TestWorldSnapshot_RoundTrip 快照 round-trip:StateHash / Month / Age /
// 玩家数 / 活动场景 / 注入记录全部保持(契约 §7.1)。
func TestWorldSnapshot_RoundTrip(t *testing.T) {
	SetSnapshotDir(t.TempDir())
	roomID := "sl-round"
	r := newScenarioTestRoom(t, roomID, 20261106)
	if _, _, e := r.ApplyScenario(ScenarioEarthquake, 3, 6, "", "round"); e != nil {
		t.Fatalf("inject: %v", e)
	}
	r.MuLock()
	r.World.SettleMonth()
	r.MuUnlock()

	before := r.World.StateHash()
	month := r.World.Month
	age := r.World.Age()
	players, scenarios, records := 0, len(r.World.Scenarios), len(r.World.ScenarioApplied)
	for _, p := range r.World.Players {
		if p != nil {
			players++
		}
	}

	meta, err := SaveWorldSnapshot(roomID, r.World, SnapshotReasonManual)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if meta.SnapshotID == "" || meta.StateHash != before || meta.Month != month || meta.SizeBytes == 0 {
		t.Fatalf("meta = %+v (want hash=%s month=%d)", meta, before, month)
	}
	snap, err := LoadWorldSnapshot(roomID, meta.SnapshotID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if snap.SchemaVersion != SnapshotSchemaVersion || snap.RoomID != roomID {
		t.Errorf("snap header = %d/%q", snap.SchemaVersion, snap.RoomID)
	}
	w2, err := unmarshalWorld(snap.WorldJSON)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := w2.StateHash(); got != before {
		t.Errorf("state hash after round-trip = %s, want %s", got, before)
	}
	if w2.Month != month {
		t.Errorf("month = %d, want %d", w2.Month, month)
	}
	if w2.Age() != age {
		t.Errorf("age = %d, want %d(startAge 未恢复)", w2.Age(), age)
	}
	if w2.Rand == nil {
		t.Errorf("Rand must be re-seeded from Seed")
	}
	players2 := 0
	for _, p := range w2.Players {
		if p != nil {
			players2++
		}
	}
	if players2 != players {
		t.Errorf("players = %d, want %d", players2, players)
	}
	if len(w2.Scenarios) != scenarios || len(w2.ScenarioApplied) != records {
		t.Errorf("scenarios/records = %d/%d, want %d/%d",
			len(w2.Scenarios), len(w2.ScenarioApplied), scenarios, records)
	}
}

// TestUnmarshalWorld_NilSubsystems JSON 缺子系统字段 ⇒ 惰性初始化(照抄
// NewWorld 构造口径),不 panic;startAge 恢复为 MasterStartAge。
func TestUnmarshalWorld_NilSubsystems(t *testing.T) {
	w, err := unmarshalWorld([]byte(`{"Month":3,"Seed":42,"Status":"playing","EconomyEnabled":true}`))
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if w.Market == nil || w.Ledger == nil || w.CB == nil || w.Treasury == nil ||
		w.Goods == nil || w.Labor == nil || w.SocietyHist == nil ||
		w.CDMarket == nil || w.ShortBook == nil || w.QuantEngine == nil ||
		w.PublicSvc == nil || w.Regulators == nil || w.Election == nil ||
		w.ListingBook == nil || w.AuctionHouse == nil || w.OffLedger == nil {
		t.Fatalf("nil subsystem after lazy init: %+v", w)
	}
	if w.Rand == nil {
		t.Errorf("Rand must be re-seeded")
	}
	if w.Age() != MasterStartAge {
		t.Errorf("age = %d, want MasterStartAge %d", w.Age(), MasterStartAge)
	}
}

// TestMarshalWorld_NilWorld nil World ⇒ error(不 panic)。
func TestMarshalWorld_NilWorld(t *testing.T) {
	if _, err := marshalWorld(nil); err == nil {
		t.Errorf("marshalWorld(nil) must return error")
	}
	if _, err := SaveWorldSnapshot("x", nil, SnapshotReasonManual); err == nil {
		t.Errorf("SaveWorldSnapshot(nil) must return error")
	}
}

// TestSnapshotMetaListDelete 元信息 / 列表降序 / 删除 / 非法 id。
func TestSnapshotMetaListDelete(t *testing.T) {
	SetSnapshotDir(t.TempDir())
	roomID := "sl-meta"
	r := newScenarioTestRoom(t, roomID, 20261106)
	m1, err := SaveWorldSnapshot(roomID, r.World, SnapshotReasonManual)
	if err != nil {
		t.Fatalf("save1: %v", err)
	}
	if _, err := SaveWorldSnapshot(roomID, r.World, SnapshotReasonReplay); err != nil {
		t.Fatalf("save2: %v", err)
	}
	meta, err := LoadWorldSnapshotMeta(roomID, m1.SnapshotID)
	if err != nil {
		t.Fatalf("meta: %v", err)
	}
	if meta.SnapshotID != m1.SnapshotID || meta.Reason != SnapshotReasonManual || meta.SizeBytes == 0 {
		t.Errorf("meta = %+v", meta)
	}
	list := ListWorldSnapshots(roomID)
	if len(list) != 2 {
		t.Fatalf("list = %d, want 2", len(list))
	}
	if list[0].CreatedAt < list[1].CreatedAt {
		t.Errorf("list must be sorted by CreatedAt desc")
	}
	if err := DeleteWorldSnapshot(roomID, m1.SnapshotID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := LoadWorldSnapshot(roomID, m1.SnapshotID); err == nil {
		t.Errorf("deleted snapshot must not load")
	}
	if err := DeleteWorldSnapshot(roomID, "no-such-snapshot"); err != nil {
		t.Errorf("delete missing must be no-op, got %v", err)
	}
	// 非法 id / 非法 room ⇒ error(路径穿越防护)。
	if _, err := LoadWorldSnapshot(roomID, "../evil"); err == nil {
		t.Errorf("illegal snapshot id must be rejected")
	}
	if _, err := LoadWorldSnapshot("", "x"); err == nil {
		t.Errorf("empty room id must be rejected")
	}
	if _, err := LoadWorldSnapshot("a/b", "x"); err == nil {
		t.Errorf("room id with separator must be rejected")
	}
	// 不存在的房间 ⇒ 非 nil 空列表。
	if got := ListWorldSnapshots("sl-meta-none"); got == nil || len(got) != 0 {
		t.Errorf("unknown room list must be non-nil [], got %#v", got)
	}
}

// TestAutoRollbackRetention auto_rollback 快照保留最近 3 份(超出删最旧)。
func TestAutoRollbackRetention(t *testing.T) {
	SetSnapshotDir(t.TempDir())
	roomID := "sl-retention"
	r := newScenarioTestRoom(t, roomID, 20261106)
	for i := 0; i < autoRollbackKeep+3; i++ {
		if _, err := SaveWorldSnapshot(roomID, r.World, SnapshotReasonAutoRollback); err != nil {
			t.Fatalf("save #%d: %v", i+1, err)
		}
	}
	list := ListWorldSnapshots(roomID)
	if len(list) != autoRollbackKeep {
		t.Fatalf("auto_rollback snapshots = %d, want %d", len(list), autoRollbackKeep)
	}
	for _, m := range list {
		if m.Reason != SnapshotReasonAutoRollback {
			t.Errorf("reason = %q, want %q", m.Reason, SnapshotReasonAutoRollback)
		}
	}
	// 手动快照不受 auto_rollback 保留策略影响。
	if _, err := SaveWorldSnapshot(roomID, r.World, SnapshotReasonManual); err != nil {
		t.Fatalf("manual save: %v", err)
	}
	if got := len(ListWorldSnapshots(roomID)); got != autoRollbackKeep+1 {
		t.Errorf("list = %d, want %d", got, autoRollbackKeep+1)
	}
}

// TestRecoverySnapshotID 恢复点恒 1 份:固定 id "recovery" + recovery.json。
func TestRecoverySnapshotID(t *testing.T) {
	SetSnapshotDir(t.TempDir())
	roomID := "sl-recovery"
	r := newScenarioTestRoom(t, roomID, 20261106)
	m1, err := SaveWorldSnapshot(roomID, r.World, SnapshotReasonRecovery)
	if err != nil {
		t.Fatalf("save1: %v", err)
	}
	if m1.SnapshotID != recoverySnapshotID {
		t.Errorf("recovery snapshot id = %q, want %q", m1.SnapshotID, recoverySnapshotID)
	}
	m2, err := SaveWorldSnapshot(roomID, r.World, SnapshotReasonRecovery)
	if err != nil {
		t.Fatalf("save2: %v", err)
	}
	if m2.SnapshotID != recoverySnapshotID {
		t.Errorf("recovery snapshot id = %q, want %q", m2.SnapshotID, recoverySnapshotID)
	}
	list := ListWorldSnapshots(roomID)
	if len(list) != 1 {
		t.Fatalf("recovery must be single file, got %d", len(list))
	}
	if list[0].SnapshotID != recoverySnapshotID || list[0].Reason != SnapshotReasonRecovery {
		t.Errorf("recovery meta = %+v", list[0])
	}
	snap, err := LoadWorldSnapshot(roomID, recoverySnapshotID)
	if err != nil {
		t.Fatalf("load recovery: %v", err)
	}
	if snap.Reason != SnapshotReasonRecovery {
		t.Errorf("snap reason = %q", snap.Reason)
	}
	// 恢复点可删除。
	if err := DeleteWorldSnapshot(roomID, recoverySnapshotID); err != nil {
		t.Fatalf("delete recovery: %v", err)
	}
	if got := ListWorldSnapshots(roomID); len(got) != 0 {
		t.Errorf("after delete list = %d, want 0", len(got))
	}
}
