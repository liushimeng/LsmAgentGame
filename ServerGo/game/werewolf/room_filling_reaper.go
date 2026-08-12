// Package werewolf — room_filling_reaper.go: filling 阶段房间回收器(R187-1,
// 2026-07-23)。
//
// 背景:狼人杀房间创建后停在 PhaseFilling(等人入座)。若创建者离开 /
// 关闭浏览器且再也不回来,房间此前唯一的兜底是 30 分钟周期的
// service.JanitorSweepStale —— 大厅里会长时间挂着「1/13 filling」的死房间。
// 本文件提供 in-memory 快照 + 强制关闭入口,由 service 层的
// JanitorSweepStaleFilling 周期调用(挂在既有 RunJanitor ticker 上,
// 不新增 per-room goroutine)。
//
// 设计要点(对齐 §92a / §129 教训):
//   - FillingRoomSnapshot 在 m.mu / r.mu 锁内只做纯读取,绝不调用任何
//     会再次取锁的函数(sync.Mutex 不可重入)。
//   - ForceCloseFillingRoom 走与 RemoveGame 完全一致的 teardown 路径
//     (先 m.mu 摘表,再在 m.mu 外持 r.mu 调 stopAgentsLocked),避免与
//     agent goroutine 的 GameContext 构建死锁。
//   - createdAt 为零值的老对象按"不过期"处理,交给 30 分钟的
//     JanitorSweepStale 清理 —— 新对象在 5 个分配点全部记录 time.Now()。
package werewolf

import (
	"time"

	"LsmWebGame/logger"

	"go.uber.org/zap"
)

// FillingRoomInfo 是一个 filling 阶段房间的只读快照,供 service 层
// janitor 判定是否回收。
type FillingRoomInfo struct {
	RoomID        string
	Phase         string    // GameState.Phase.String();State==nil 时视为 filling
	CreatedAt     time.Time // in-memory 对象创建时间;零值 = 老对象
	OccupiedSeats int       // 已入座数(含 bot 座位)
	Spectators    int       // 观察者数
}

// FillingRoomSnapshot 返回当前所有 in-memory 狼人杀房间中处于
// PhaseFilling(或 State==nil,等价于尚未开始)的房间快照。
//
// 只读、幂等;锁内纯读取,符合 §92a(不调用任何 *Locked 引擎函数)。
func (m *WerewolfManager) FillingRoomSnapshot() []FillingRoomInfo {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	rooms := make([]*WerewolfRoom, 0, len(m.rooms))
	for _, r := range m.rooms {
		rooms = append(rooms, r)
	}
	m.mu.RUnlock()

	out := make([]FillingRoomInfo, 0, len(rooms))
	for _, r := range rooms {
		if r == nil {
			continue
		}
		r.mu.Lock()
		isFilling := r.State == nil || r.State.Phase == PhaseFilling
		if !isFilling {
			r.mu.Unlock()
			continue
		}
		info := FillingRoomInfo{
			RoomID:     r.RoomID,
			Phase:      PhaseFilling.String(),
			CreatedAt:  r.createdAt,
			Spectators: len(r.Spectators),
		}
		if r.State != nil {
			info.Phase = r.State.Phase.String()
		}
		for _, uid := range r.Seats {
			if uid != "" {
				info.OccupiedSeats++
			}
		}
		r.mu.Unlock()
		out = append(out, info)
	}
	return out
}

// ForceCloseFillingRoom 从管理器摘除一个 filling 阶段房间并停掉其
// agent goroutine(若有)。与 RemoveGame 的 teardown 路径完全一致:
// 先在 m.mu 下摘表,再在 m.mu 外持 r.mu 调 stopAgentsLocked。
//
// 安全约束(由调用方 service.JanitorSweepStaleFilling 保证):
//   - 房间必须仍处 filling;本函数在摘表前会复核 phase,若房间在两次
//     检查之间已经开始游戏(State 推进离开 PhaseFilling),则放弃摘除
//     并返回 false —— 绝不在游戏进行中强拆。
//   - 人类在线判定(hub.IsRoomEmpty)在 service 层完成,本函数不感知 hub。
//
// 幂等:房间不存在时返回 false,nil-safe。
func (m *WerewolfManager) ForceCloseFillingRoom(roomID string) bool {
	if m == nil || roomID == "" {
		return false
	}
	m.mu.Lock()
	r, ok := m.rooms[roomID]
	if !ok {
		m.mu.Unlock()
		return false
	}
	delete(m.rooms, roomID)
	m.mu.Unlock()

	// 复核 phase(摘表后房间对象已脱离管理器,后续任何 JoinGame 都会
	// 重建新对象,不会再拿到这个 r —— 但同一 roomID 的旧引用可能还
	// 被 agent goroutine 持有,所以 teardown 仍要走完整 stopAgentsLocked)。
	r.mu.Lock()
	if r.State != nil && r.State.Phase != PhaseFilling {
		// 房间在两次检查之间已经开始 —— 把房间放回管理器,放弃回收。
		// 这是极窄的竞态窗口(快照 → 摘除之间有人入座触发了 StartGame)。
		r.mu.Unlock()
		m.mu.Lock()
		// 只有当管理器里没有同名新房间时才放回,避免覆盖重建的新对象。
		if _, exists := m.rooms[roomID]; !exists {
			m.rooms[roomID] = r
			m.mu.Unlock()
			logger.L().Info("werewolf: filling reaper aborted, room started mid-sweep",
				zap.String("room_id", roomID))
		} else {
			m.mu.Unlock()
		}
		return false
	}
	m.stopAgentsLocked(r)
	r.mu.Unlock()
	logger.L().Info("werewolf: filling room reaped from in-memory manager",
		zap.String("room_id", roomID))
	return true
}
