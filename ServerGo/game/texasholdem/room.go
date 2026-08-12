package texasholdem

import (
	"sync"
	"time"

	"LsmAgentGame/errcode"
	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// ─────────────────── Room ───────────────────

// TexasHoldemRoom 持有一局德州扑克的内存状态。
type TexasHoldemRoom struct {
	mu         sync.Mutex
	RoomID     string
	Seats      [MaxPlayers]string // 座位 0..5 的 userID，空串表示空位
	State      *GameState
	Spectators map[string]struct{}
}

// SeatOf 返回 userID 所在座位，未入座返回 (-1,false)。
func (r *TexasHoldemRoom) SeatOf(userID string) (int, bool) {
	for i, u := range r.Seats {
		if u == userID {
			return i, true
		}
	}
	return -1, false
}

// Occupied 返回已入座人数。
func (r *TexasHoldemRoom) Occupied() int {
	n := 0
	for _, u := range r.Seats {
		if u != "" {
			n++
		}
	}
	return n
}

// IsReady 报告是否至少 2 人。
func (r *TexasHoldemRoom) IsReady() bool {
	return r.Occupied() >= 2
}

// ─────────────────── Manager ───────────────────

// TexasHoldemManager 管理所有活跃的德州扑克房间。
type TexasHoldemManager struct {
	mu    sync.RWMutex
	rooms map[string]*TexasHoldemRoom
	// seedFn 提供发牌随机种子；测试可替换为固定值。
	seedFn     func() int64
	BigBlind   int
	StartStack int
}

// NewTexasHoldemManager 创建空管理器。
func NewTexasHoldemManager() *TexasHoldemManager {
	return &TexasHoldemManager{
		rooms:      make(map[string]*TexasHoldemRoom),
		seedFn:     func() int64 { return time.Now().UnixNano() },
		BigBlind:   200,
		StartStack: 10000,
	}
}

func (m *TexasHoldemManager) getRoom(roomID string) *TexasHoldemRoom {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.rooms[roomID]
}

// JoinGame 让 userID 入座。幂等。
// 达到 2 人时自动发牌进入翻前阶段。
func (m *TexasHoldemManager) JoinGame(roomID, userID string) (*TexasHoldemRoom, bool, *errcode.Error) {
	m.mu.Lock()
	r, ok := m.rooms[roomID]
	if !ok {
		r = &TexasHoldemRoom{RoomID: roomID}
		m.rooms[roomID] = r
		logger.L().Info("texasholdem room created", zap.String("room_id", roomID), zap.String("by", userID))
	}
	m.mu.Unlock()

	r.mu.Lock()
	defer r.mu.Unlock()

	// 幂等：已入座直接返回
	if _, seated := r.SeatOf(userID); seated {
		return r, false, nil
	}

	// 找空位
	seat := -1
	for i, u := range r.Seats {
		if u == "" {
			seat = i
			break
		}
	}
	if seat == -1 {
		return nil, false, errcode.Code(errcode.ErrRoomFull)
	}
	r.Seats[seat] = userID

	started := false
	if r.State == nil {
		r.State = NewGame(m.seedFn(), m.BigBlind)
	}
	if _, e := r.State.AddPlayer(userID, m.StartStack); e != nil {
		// 不应发生（刚找到空位）
		logger.L().Warn("texasholdem add player failed", zap.String("room_id", roomID), zap.Error(e))
	}

	// 满 2 人且当前阶段为 waiting，自动开首手
	if r.IsReady() && r.State.Street == PhaseWaiting {
		if e := r.State.StartHand(); e != nil {
			logger.L().Warn("texasholdem start hand failed", zap.String("room_id", roomID), zap.Error(e))
		} else {
			started = true
			logger.L().Info("texasholdem hand started",
				zap.String("room_id", roomID),
				zap.Int("hand_number", r.State.HandNumber))
		}
	}
	return r, started, nil
}

// Action 处理玩家动作。返回 (房间, 手牌是否结束, 错误)。
// 手牌结束后，若满足开新一手条件则自动开始下一手。
func (m *TexasHoldemManager) Action(roomID, userID string, a Action) (*TexasHoldemRoom, bool, *errcode.Error) {
	r := m.getRoom(roomID)
	if r == nil {
		return nil, false, errcode.Code(errcode.ErrRoomNotFound)
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State == nil {
		return nil, false, errcode.Code(errcode.ErrGameNotStarted)
	}
	seat, ok := r.State.GetSeat(userID)
	if !ok {
		return nil, false, errcode.Code(errcode.ErrRoomNotIn)
	}

	handOver, e := r.State.ApplyAction(seat, a)
	if e != nil {
		return nil, false, e
	}

	if handOver {
		// 延迟开新一手（给客户端 time 展示结果）
		if r.State.CanStartHand() {
			go func() {
				time.Sleep(5 * time.Second)
				r.mu.Lock()
				defer r.mu.Unlock()
				if r.State.Street == PhaseOver || r.State.Street == PhaseShowdown {
					if e := r.State.StartHand(); e != nil {
						logger.L().Warn("auto start next hand failed", zap.String("room_id", roomID), zap.Error(e))
					} else {
						logger.L().Info("texasholdem next hand", zap.String("room_id", roomID), zap.Int("hand", r.State.HandNumber))
						m.broadcastState(roomID, r)
					}
				}
			}()
		}
	}

	return r, handOver, nil
}

// broadcastState 广播房间状态（供异步开新手时调用）。
func (m *TexasHoldemManager) broadcastState(roomID string, r *TexasHoldemRoom) {
	// 此方法只在已持有房间锁时调用，外层负责 hub 广播。
	// 返回 rooms 快照供调用方获取座位信息。
}

// Resign 认输（等同弃牌）。
func (m *TexasHoldemManager) Resign(roomID, userID string) (*TexasHoldemRoom, *errcode.Error) {
	r := m.getRoom(roomID)
	if r == nil {
		return nil, errcode.Code(errcode.ErrRoomNotFound)
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State == nil || r.State.Status != StatusPlaying {
		return nil, errcode.Code(errcode.ErrGameAlreadyOver)
	}
	seat, ok := r.State.GetSeat(userID)
	if !ok {
		return nil, errcode.Code(errcode.ErrRoomNotIn)
	}

	if !r.State.Players[seat].Folded {
		r.State.Players[seat].Folded = true
		// 检查是否只剩 1 人
		if r.State.activePlayers() <= 1 {
			r.State.endHandFold()
		}
	}
	return r, nil
}

// GetState 返回某玩家可见的对局视图。
func (m *TexasHoldemManager) GetState(roomID, userID string) (*ClientGameState, *errcode.Error) {
	r := m.getRoom(roomID)
	if r == nil {
		return nil, errcode.Code(errcode.ErrRoomNotFound)
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	seat, ok := r.SeatOf(userID)
	if !ok {
		return nil, errcode.Code(errcode.ErrRoomNotIn)
	}
	return BuildClientState(roomID, r.Seats, seat, r.State), nil
}

// StateForSeat 在已持有房间引用时构造指定座位视图。
func (m *TexasHoldemManager) StateForSeat(roomID string, seat int) *ClientGameState {
	r := m.getRoom(roomID)
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return BuildClientState(roomID, r.Seats, seat, r.State)
}

// Seats 返回房间各座位 userID 的快照。
func (m *TexasHoldemManager) Seats(roomID string) ([MaxPlayers]string, bool) {
	r := m.getRoom(roomID)
	if r == nil {
		return [MaxPlayers]string{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Seats, true
}

// RemoveGame 从管理器移除房间。
func (m *TexasHoldemManager) RemoveGame(roomID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.rooms, roomID)
}

// ─────────────────── Spectator API ───────────────────

// SpectateGame 注册 userID 为房间观察者；按需创建房间。不会消耗座位。幂等。
func (m *TexasHoldemManager) SpectateGame(roomID, userID string) (*TexasHoldemRoom, *errcode.Error) {
	m.mu.Lock()
	r, ok := m.rooms[roomID]
	if !ok {
		r = &TexasHoldemRoom{RoomID: roomID}
		m.rooms[roomID] = r
		logger.L().Info("texasholdem room created by spectator",
			zap.String("room_id", roomID), zap.String("user_id", userID))
	}
	m.mu.Unlock()

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Spectators == nil {
		r.Spectators = make(map[string]struct{})
	}
	r.Spectators[userID] = struct{}{}
	return r, nil
}

// UnspectateGame 取消观察者身份。
func (m *TexasHoldemManager) UnspectateGame(roomID, userID string) *errcode.Error {
	r := m.getRoom(roomID)
	if r == nil {
		return errcode.Code(errcode.ErrRoomNotFound)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.Spectators, userID)
	return nil
}

// SpectatorList 返回当前观察者 userID。
func (m *TexasHoldemManager) SpectatorList(roomID string) []string {
	r := m.getRoom(roomID)
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.Spectators))
	for uid := range r.Spectators {
		out = append(out, uid)
	}
	return out
}

// SpectatorState 返回观察者可见的客户端视图。任何玩家的 Hole 都不填充，
// 仅留下 hole_count。摊牌阶段（亮底牌比大小）结束后隐藏信息才公开。
func (m *TexasHoldemManager) SpectatorState(roomID, userID string) (*ClientGameState, *errcode.Error) {
	r := m.getRoom(roomID)
	if r == nil {
		return nil, errcode.Code(errcode.ErrRoomNotFound)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return BuildClientState(roomID, r.Seats, -1, r.State), nil
}

// SpectatorView 同 SpectatorState 但省去 userID 参数；所有观察者共享同一
// 张重新构建的视图，构建一次即可向多个观察者广播。
func (m *TexasHoldemManager) SpectatorView(roomID string) *ClientGameState {
	r := m.getRoom(roomID)
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return BuildClientState(roomID, r.Seats, -1, r.State)
}
