// Package virtual_city — room_save_snapshot.go: 批次86 房间层存档 / 读档 /
// 回滚 / 灾难恢复(2026-11-06 §可靠性与可观测性)。
//
// 契约:lag_docs/虚拟城市/已实现/86-可靠性与可观测性/01-方案设计.md §3.2。
//
// 独立成文件的原因:room.go 已 1778 行(§4 硬上限 1800),本批房间层方法全部
// 落在此处,room.go 只加最小 hook 调用。
//
// 纪律:
//   - **锁纪律(§92a)**:全部方法持 r.mu;**禁止回调房间**(不调 GetStatus() /
//     Engine() 等会再取锁的方法);只读方法返回副本;
//   - **零 panic、绝不阻断对局**:写盘失败只 zap warn / 返回 error;
//   - **零 rand / 零回归**:快照是纯旁路,不消耗 w.Rand、不改 StateHash;
//     EventLifecycleLog 不进 StateHash ⇒ 固定 seed 双跑逐位相等;
//   - **nil 安全**:World nil / 未开局返回 35002 或空切片,不 panic。
package virtual_city

import (
	"time"

	"LsmAgentGame/errcode"
	"LsmAgentGame/logger"
	"go.uber.org/zap"
)

// recoveryPointWriteTimeout 灾难恢复点异步写盘超时(超时只 warn,不阻断月结)。
const recoveryPointWriteTimeout = 3 * time.Second

// ─────────────────────────── 手动存档 / 读档 ───────────────────────────

// Snapshot 手动存档(持 r.mu;返回元信息)。
//
// reason 为空 ⇒ 取 SnapshotReasonManual。失败返回 ErrInternal(磁盘错误
// 只进日志,不回泄路径给前端)。
func (r *VirtualCityRoom) Snapshot(reason string) (*SnapshotMeta, *errcode.Error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.World == nil {
		return nil, errcode.Code(errcode.ErrVirtualCityNotPlaying)
	}
	if reason == "" {
		reason = SnapshotReasonManual
	}
	meta, err := SaveWorldSnapshot(r.RoomID, r.World, reason)
	if err != nil {
		logger.L().Warn("virtual_city snapshot save failed",
			zap.String("room_id", r.RoomID), zap.String("reason", reason), zap.Error(err))
		return nil, errcode.Code(errcode.ErrInternal)
	}
	return meta, nil
}

// RestoreSnapshot 读档(持 r.mu;恢复到 r.World;返回元信息)。
//
// 快照只存 World ⇒ 恢复后按**当前房间级开关**重新落位(economy /
// insurance / family / election / 自动注入安全阀)—— 这些是房间选项,
// 不进存档(与 pendingOpts 同口径)。
func (r *VirtualCityRoom) RestoreSnapshot(snapshotID string) (*SnapshotMeta, *errcode.Error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.World == nil {
		return nil, errcode.Code(errcode.ErrVirtualCityNotPlaying)
	}
	return r.restoreSnapshotLocked(snapshotID)
}

// restoreSnapshotLocked 读档共用体(调用方持 r.mu)。
func (r *VirtualCityRoom) restoreSnapshotLocked(snapshotID string) (*SnapshotMeta, *errcode.Error) {
	meta, err := LoadWorldSnapshotMeta(r.RoomID, snapshotID)
	if err != nil {
		return nil, errSnapshotNotFound(snapshotID)
	}
	snap, err := LoadWorldSnapshot(r.RoomID, snapshotID)
	if err != nil {
		return nil, errSnapshotNotFound(snapshotID)
	}
	nw, err := unmarshalWorld(snap.WorldJSON)
	if err != nil {
		logger.L().Warn("virtual_city snapshot decode failed",
			zap.String("room_id", r.RoomID), zap.String("snapshot_id", snapshotID), zap.Error(err))
		return nil, errSnapshotNotFound(snapshotID)
	}
	r.applyWorldConfigLocked(nw)
	r.World = nw
	// 事件游标跟随恢复后的世界,避免恢复后漏播/重播事件。
	r.eventsSent = len(nw.Events)
	// 批次84 的指标历史环是「按月追加的观测日志」:读档回到更早的月以后,
	// 旧环里会残留恢复点之后的月份(未来数据)⇒ 清空,由后续月结重新采样。
	r.metricHistory = nil
	return meta, nil
}

// ListSnapshots 列出快照(持 r.mu 只读;副本;新→旧)。
func (r *VirtualCityRoom) ListSnapshots() []SnapshotMeta {
	r.mu.Lock()
	defer r.mu.Unlock()
	return ListWorldSnapshots(r.RoomID)
}

// DeleteSnapshot 删除快照(持 r.mu;不存在 = no-op)。
func (r *VirtualCityRoom) DeleteSnapshot(snapshotID string) *errcode.Error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := DeleteWorldSnapshot(r.RoomID, snapshotID); err != nil {
		logger.L().Warn("virtual_city snapshot delete failed",
			zap.String("room_id", r.RoomID), zap.String("snapshot_id", snapshotID), zap.Error(err))
		return errcode.Code(errcode.ErrInternal)
	}
	return nil
}

// RollbackLastScenario 回滚最近一次场景注入(持 r.mu;用 trace 关联的快照)。
//
// 找最近一条 ResultOutcome=success 的留痕,用其 SnapshotID 恢复 —— 快照
// 是注入**之前**拍的,恢复即精确回到注入前状态(活动场景/冲击/记录一并回退)。
func (r *VirtualCityRoom) RollbackLastScenario() (*SnapshotMeta, *errcode.Error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.World == nil {
		return nil, errcode.Code(errcode.ErrVirtualCityNotPlaying)
	}
	for i := range r.World.EventLifecycleLog {
		tr := r.World.EventLifecycleLog[i] // 环为新→旧,首条即最近
		if tr.ResultOutcome != EventResultSuccess || tr.SnapshotID == "" {
			continue
		}
		return r.restoreSnapshotLocked(tr.SnapshotID)
	}
	return nil, errSnapshotNotFound("")
}

// ─────────────────────────── 灾难恢复 ───────────────────────────

// applyWorldConfigLocked 把房间级开关重新落位到 World(持 r.mu)。
//
// 快照只存 World,不存房间选项 ⇒ 读档 / 恢复后必须重放一次,否则恢复出的
// 世界会沿用快照时刻的开关(可能与当前房间配置不一致)。
func (r *VirtualCityRoom) applyWorldConfigLocked(w *World) {
	if w == nil {
		return
	}
	w.EconomyEnabled = r.economyEnabled
	w.InsuranceEnabled = r.insuranceEnabled
	w.FamilyEnabled = r.familyEnabled
	if w.Election != nil {
		w.Election.Enabled = r.electionEnabled
	}
	applyAutoScenarioLimits(w, r.worldEvent.autoEnabled,
		r.worldEvent.maxPerMonth, r.worldEvent.maxMagnitude)
}

// tryRecoverWorldLocked 灾难恢复(批次86 §3.2.4;调用方持 r.mu)。
//
// 本房存在 recovery.json ⇒ 读档并返回恢复出的 World;不存在 / 损坏 ⇒
// 返回 nil(调用方按正常开局继续,零回归)。
func (r *VirtualCityRoom) tryRecoverWorldLocked() *World {
	snap, err := LoadWorldSnapshot(r.RoomID, recoverySnapshotID)
	if err != nil || snap == nil {
		return nil
	}
	nw, err := unmarshalWorld(snap.WorldJSON)
	if err != nil {
		logger.L().Warn("virtual_city recovery snapshot decode failed",
			zap.String("room_id", r.RoomID), zap.Error(err))
		return nil
	}
	r.applyWorldConfigLocked(nw)
	logger.L().Info("virtual_city recovered from snapshot",
		zap.String("room_id", r.RoomID),
		zap.String("recovered_from", snap.SnapshotID),
		zap.Int("month", nw.Month))
	return nw
}

// writeRecoveryPointAsync 月结成功后异步写灾难恢复点(锁外调用;批次86 §3.2.4)。
//
// 流程:① 锁内把 World 序列化 + 取 StateHash/Month(快照内容一致,不受后续
// 月结改写影响);② 锁外 goroutine 落盘(超时只 warn)。
// 写盘失败**绝不阻断月结**,也绝不 panic。
func (r *VirtualCityRoom) writeRecoveryPointAsync() {
	r.mu.Lock()
	if r.World == nil || r.Status != StatusPlaying {
		r.mu.Unlock()
		return
	}
	w := r.World
	data, err := marshalWorld(w)
	hash := w.StateHash()
	month := w.Month
	roomID := r.RoomID
	r.mu.Unlock()
	if err != nil {
		logger.L().Warn("virtual_city recovery snapshot marshal failed",
			zap.String("room_id", roomID), zap.Int("month", month), zap.Error(err))
		return
	}
	go func() {
		done := make(chan struct{})
		go func() {
			defer close(done)
			if _, err := saveSnapshotPayload(roomID, hash, month, SnapshotReasonRecovery, data); err != nil {
				logger.L().Warn("virtual_city recovery snapshot write failed",
					zap.String("room_id", roomID), zap.Int("month", month), zap.Error(err))
			}
		}()
		select {
		case <-done:
		case <-time.After(recoveryPointWriteTimeout):
			logger.L().Warn("virtual_city recovery snapshot write timeout",
				zap.String("room_id", roomID), zap.Int("month", month))
		}
	}()
}
