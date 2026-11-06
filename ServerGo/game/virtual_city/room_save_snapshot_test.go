// Package virtual_city — room_save_snapshot_test.go: 批次86 房间层存档 /
// 读档 / 回滚 / 灾难恢复单测(2026-11-06 §可靠性与可观测性 §3.2)。
//
// 覆盖(契约 §7.1「恢复点写入 / 启动恢复」):
//   - Snapshot / RestoreSnapshot / ListSnapshots / DeleteSnapshot;
//   - RollbackLastScenario 精确回到注入前;
//   - 读档后房间级开关重新落位;
//   - 月结成功后异步写 recovery.json;
//   - Start 时自动从 recovery.json 恢复(同 roomID 重建房)。
package virtual_city

import (
	"fmt"
	"testing"
	"time"

	"LsmAgentGame/errcode"
)

// rsWaitRecovery 轮询等待恢复点写到指定月份(异步写盘;超时 fatal)。
func rsWaitRecovery(t *testing.T, roomID string, month int) *SnapshotMeta {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if m, err := LoadWorldSnapshotMeta(roomID, recoverySnapshotID); err == nil && m.Month >= month {
			return m
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("recovery point (month>=%d) not written for room %s", month, roomID)
	return nil
}

// rsSettleNow 把月结窗口推到过去并执行一次 trySettle(真实月结路径)。
func rsSettleNow(t *testing.T, r *VirtualCityRoom) {
	t.Helper()
	r.mu.Lock()
	r.NextMonthAt = time.Now().Add(-time.Millisecond)
	r.mu.Unlock()
	if !r.trySettle(nil) {
		t.Fatal("trySettle did not advance month")
	}
}

// TestRoomSnapshotRestore 手动存档 → 状态前进 → 读档 ⇒ 精确回到快照时点。
func TestRoomSnapshotRestore(t *testing.T) {
	SetSnapshotDir(t.TempDir())
	roomID := "rs-basic"
	r := newScenarioTestRoom(t, roomID, 20261106)
	if _, _, e := r.ApplyScenario(ScenarioEarthquake, 3, 6, "", "before-snap"); e != nil {
		t.Fatalf("inject: %v", e)
	}
	snapHash := r.World.StateHash()
	meta, e := r.Snapshot("") // reason 缺省 = manual
	if e != nil {
		t.Fatalf("snapshot: %v", e)
	}
	if meta.SnapshotID == "" || meta.Reason != SnapshotReasonManual {
		t.Fatalf("meta = %+v", meta)
	}
	// 状态前进:再注入 + 推进一月。
	if _, _, e := r.ApplyScenario(ScenarioTsunami, 2, 6, "", "after-snap"); e != nil {
		t.Fatalf("inject2: %v", e)
	}
	rsSettleNow(t, r)
	if r.World.StateHash() == snapHash {
		t.Fatal("state must change after 2nd inject + settle")
	}
	// 读档
	restored, e := r.RestoreSnapshot(meta.SnapshotID)
	if e != nil {
		t.Fatalf("restore: %v", e)
	}
	if restored.SnapshotID != meta.SnapshotID {
		t.Errorf("restored meta = %+v", restored)
	}
	if got := r.World.StateHash(); got != snapHash {
		t.Errorf("state after restore = %s, want %s", got, snapHash)
	}
	// 批次84 指标历史环(房间层观测日志)不得残留恢复点之后的月份。
	r.mu.Lock()
	hist := len(r.metricHistory)
	r.mu.Unlock()
	if hist != 0 {
		t.Errorf("metric history after restore = %d, want 0", hist)
	}
	// 列表非空(至少含手动快照)
	if list := r.ListSnapshots(); len(list) == 0 {
		t.Errorf("snapshot list empty")
	}
	// 删除后再读 ⇒ 35066
	if e := r.DeleteSnapshot(meta.SnapshotID); e != nil {
		t.Fatalf("delete: %v", e)
	}
	if _, e := r.RestoreSnapshot(meta.SnapshotID); e == nil ||
		e.Code != errcode.ErrVirtualCitySnapshotNotFound {
		t.Errorf("restore deleted: got %v, want code %d", e, errcode.ErrVirtualCitySnapshotNotFound)
	}
	// 未开局房间 ⇒ 35002
	open := NewVirtualCityRoom("rs-open", 3000, 1, 4)
	if _, e := open.Snapshot(""); e == nil || e.Code != errcodeVirtualCityNotPlaying {
		t.Errorf("open room snapshot: got %v, want %d", e, errcodeVirtualCityNotPlaying)
	}
}

// errcodeVirtualCityNotPlaying 避免测试文件直接依赖 errcode 包名(可读性)。
const errcodeVirtualCityNotPlaying = 35002

// TestRollbackLastScenario 回滚最近一次注入 ⇒ 精确回到注入前状态。
func TestRollbackLastScenario(t *testing.T) {
	SetSnapshotDir(t.TempDir())
	roomID := "rs-rollback"
	r := newScenarioTestRoom(t, roomID, 20261106)
	pre := r.World.StateHash()
	if _, _, e := r.ApplyScenario(ScenarioEarthquake, 3, 6, "", "rb"); e != nil {
		t.Fatalf("inject: %v", e)
	}
	if r.World.StateHash() == pre {
		t.Fatal("inject must change state")
	}
	meta, e := r.RollbackLastScenario()
	if e != nil {
		t.Fatalf("rollback: %v", e)
	}
	if meta == nil || meta.SnapshotID == "" {
		t.Fatalf("rollback meta = %+v", meta)
	}
	if got := r.World.StateHash(); got != pre {
		t.Errorf("state after rollback = %s, want %s", got, pre)
	}
	if len(r.World.Scenarios) != 0 {
		t.Errorf("active scenarios after rollback = %d, want 0", len(r.World.Scenarios))
	}
	if len(r.World.ScenarioApplied) != 0 {
		t.Errorf("scenario records after rollback = %d, want 0", len(r.World.ScenarioApplied))
	}
}

// TestRollbackLastScenario_None 无成功留痕 ⇒ 35066。
func TestRollbackLastScenario_None(t *testing.T) {
	SetSnapshotDir(t.TempDir())
	r := newScenarioTestRoom(t, "rs-rollback-none", 20261106)
	if _, e := r.RollbackLastScenario(); e == nil ||
		e.Code != errcode.ErrVirtualCitySnapshotNotFound {
		t.Errorf("got %v, want code %d", e, errcode.ErrVirtualCitySnapshotNotFound)
	}
}

// TestRestoreSnapshot_ReappliesRoomConfig 快照只存 World ⇒ 读档后按当前
// 房间级开关重新落位(快照时刻的开关不生效)。
func TestRestoreSnapshot_ReappliesRoomConfig(t *testing.T) {
	SetSnapshotDir(t.TempDir())
	roomID := "rs-config"
	r := newScenarioTestRoom(t, roomID, 20261106)
	r.MuLock()
	r.World.EconomyEnabled = false // 快照时刻的(陈旧)开关
	r.MuUnlock()
	meta, e := r.Snapshot("")
	if e != nil {
		t.Fatalf("snapshot: %v", e)
	}
	r.MuLock()
	r.World.EconomyEnabled = true
	r.MuUnlock()
	if _, e := r.RestoreSnapshot(meta.SnapshotID); e != nil {
		t.Fatalf("restore: %v", e)
	}
	if r.World.EconomyEnabled != r.economyEnabled {
		t.Errorf("EconomyEnabled after restore = %v, want room flag %v",
			r.World.EconomyEnabled, r.economyEnabled)
	}
}

// TestRecoveryPointWrittenAfterSettle 月结成功后异步写 recovery.json。
func TestRecoveryPointWrittenAfterSettle(t *testing.T) {
	SetSnapshotDir(t.TempDir())
	roomID := "rs-recovery"
	r := newScenarioTestRoom(t, roomID, 20261106)
	rsSettleNow(t, r)
	meta := rsWaitRecovery(t, roomID, r.World.Month)
	if meta.Reason != SnapshotReasonRecovery {
		t.Errorf("reason = %q", meta.Reason)
	}
	if meta.Month != r.World.Month {
		t.Errorf("recovery month = %d, want %d", meta.Month, r.World.Month)
	}
	// 恢复点可加载并还原出 World。
	snap, err := LoadWorldSnapshot(roomID, recoverySnapshotID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	w2, err := unmarshalWorld(snap.WorldJSON)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if w2.StateHash() != snap.StateHash {
		t.Errorf("recovered hash = %s, want %s", w2.StateHash(), snap.StateHash)
	}
}

// TestStartRecoversFromRecoveryPoint 同 roomID 重建房并 Start ⇒ 自动读档
// 恢复到最近一次成功月结(契约 §3.2.4「启动恢复」)。
func TestStartRecoversFromRecoveryPoint(t *testing.T) {
	SetSnapshotDir(t.TempDir())
	roomID := "rs-start"
	r := newScenarioTestRoom(t, roomID, 20261106)
	const settles = 3
	for i := 0; i < settles; i++ {
		rsSettleNow(t, r)
	}
	recoveredMonth := settles + 1 // Start 后 Month=1,每次月结 +1
	rsWaitRecovery(t, roomID, recoveredMonth)

	// 同 roomID 重建房间(模拟进程重启后房间对象重建)。
	r2 := NewVirtualCityRoom(roomID, 3000, 20261106, 4)
	botUsers := map[int]string{}
	botModels := map[int]string{}
	for seat := 0; seat < 10; seat++ {
		botUsers[seat] = fmt.Sprintf("bot-%d", seat)
		botModels[seat] = "TestModel"
	}
	r2.RegisterBotSeats(botUsers, botModels)
	if e := r2.Start(nil); e != nil {
		t.Fatalf("start: %v", e)
	}
	if r2.World == nil {
		t.Fatal("world nil after start")
	}
	if r2.World.Month != recoveredMonth {
		t.Errorf("recovered month = %d, want %d", r2.World.Month, recoveredMonth)
	}
	// 恢复点来自 playing 态 ⇒ 状态应保持 playing。
	if r2.World.Status != StatusPlaying {
		t.Errorf("recovered status = %q, want %q", r2.World.Status, StatusPlaying)
	}
	// 恢复出的世界不是新建的空世界(玩家已在)。
	players := 0
	for _, p := range r2.World.Players {
		if p != nil {
			players++
		}
	}
	if players == 0 {
		t.Errorf("recovered world has no players")
	}
}
