package doudizhu

import (
	"sync"
	"time"

	"LsmAgentGame/errcode"
	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// ─────────────────── Room ───────────────────

// DoudizhuRoom 持有一局斗地主的内存状态。
type DoudizhuRoom struct {
	mu         sync.Mutex
	RoomID     string
	Seats      [3]string // 座位 0/1/2 的 userID，空串表示空位
	State      *GameState
	Spectators map[string]struct{} // 观察者 userID 集合（不入座）
}

// SeatOf 返回 userID 所在座位，未入座返回 (-1,false)。
func (r *DoudizhuRoom) SeatOf(userID string) (int, bool) {
	for i, u := range r.Seats {
		if u == userID {
			return i, true
		}
	}
	return -1, false
}

// Occupied 返回已入座人数。
func (r *DoudizhuRoom) Occupied() int {
	n := 0
	for _, u := range r.Seats {
		if u != "" {
			n++
		}
	}
	return n
}

// IsReady 报告是否满 3 人。
func (r *DoudizhuRoom) IsReady() bool {
	return r.Occupied() == 3
}

// ─────────────────── Manager ───────────────────

// DoudizhuManager 管理所有活跃的斗地主房间。
type DoudizhuManager struct {
	mu    sync.RWMutex
	rooms map[string]*DoudizhuRoom
	// seedFn 提供发牌随机种子；测试可替换为固定值。
	seedFn func() int64
}

// NewDoudizhuManager 创建空管理器，默认用时间作为发牌种子。
func NewDoudizhuManager() *DoudizhuManager {
	return &DoudizhuManager{
		rooms:  make(map[string]*DoudizhuRoom),
		seedFn: func() int64 { return time.Now().UnixNano() },
	}
}

func (m *DoudizhuManager) getRoom(roomID string) *DoudizhuRoom {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.rooms[roomID]
}

// JoinGame 让 userID 入座（创建房间或加入已有房间）。幂等。
// 满 3 人时自动发牌并进入叫地主阶段。返回房间与是否「刚好满员开始」。
func (m *DoudizhuManager) JoinGame(roomID, userID string) (*DoudizhuRoom, bool, *errcode.Error) {
	m.mu.Lock()
	r, ok := m.rooms[roomID]
	if !ok {
		r = &DoudizhuRoom{RoomID: roomID}
		m.rooms[roomID] = r
		logger.L().Info("doudizhu room created", zap.String("room_id", roomID), zap.String("by", userID))
	}
	m.mu.Unlock()

	r.mu.Lock()
	defer r.mu.Unlock()

	// 已入座 → 幂等返回。
	if _, seated := r.SeatOf(userID); seated {
		return r, false, nil
	}
	// 找空位。
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
	if r.IsReady() && r.State == nil {
		r.State = NewGame(m.seedFn(), 0)
		started = true
		logger.L().Info("doudizhu game started",
			zap.String("room_id", roomID),
			zap.Int("first_bidder", r.State.FirstBidder))
	}
	return r, started, nil
}

// Bid 处理叫分。返回 (房间, 是否需重发, error)。
func (m *DoudizhuManager) Bid(roomID, userID string, score int) (*DoudizhuRoom, bool, *errcode.Error) {
	r := m.getRoom(roomID)
	if r == nil {
		return nil, false, errcode.Code(errcode.ErrRoomNotFound)
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State == nil {
		return nil, false, errcode.Code(errcode.ErrGameNotStarted)
	}
	seat, ok := r.SeatOf(userID)
	if !ok {
		return nil, false, errcode.Code(errcode.ErrRoomNotIn)
	}
	needRedeal, e := r.State.Bid(seat, score)
	if e != nil {
		return nil, false, e
	}
	if needRedeal {
		// 全员不叫，重新发牌，首叫顺延一位。
		next := (r.State.FirstBidder + 1) % 3
		r.State = NewGame(m.seedFn(), next)
		return r, true, nil
	}
	return r, false, nil
}

// Play 处理出牌。返回 (房间, 是否结束, error)。
func (m *DoudizhuManager) Play(roomID, userID string, cards []Card) (*DoudizhuRoom, bool, *errcode.Error) {
	r := m.getRoom(roomID)
	if r == nil {
		return nil, false, errcode.Code(errcode.ErrRoomNotFound)
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State == nil {
		return nil, false, errcode.Code(errcode.ErrGameNotStarted)
	}
	seat, ok := r.SeatOf(userID)
	if !ok {
		return nil, false, errcode.Code(errcode.ErrRoomNotIn)
	}
	over, e := r.State.Play(seat, cards)
	if e != nil {
		return nil, false, e
	}
	return r, over, nil
}

// Pass 处理过牌。
func (m *DoudizhuManager) Pass(roomID, userID string) (*DoudizhuRoom, *errcode.Error) {
	r := m.getRoom(roomID)
	if r == nil {
		return nil, errcode.Code(errcode.ErrRoomNotFound)
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State == nil {
		return nil, errcode.Code(errcode.ErrGameNotStarted)
	}
	seat, ok := r.SeatOf(userID)
	if !ok {
		return nil, errcode.Code(errcode.ErrRoomNotIn)
	}
	if e := r.State.Pass(seat); e != nil {
		return nil, e
	}
	return r, nil
}

// Resign 认输。认输者若为地主→农民胜；若为农民→地主胜。
func (m *DoudizhuManager) Resign(roomID, userID string) (*DoudizhuRoom, *errcode.Error) {
	r := m.getRoom(roomID)
	if r == nil {
		return nil, errcode.Code(errcode.ErrRoomNotFound)
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State == nil {
		return nil, errcode.Code(errcode.ErrGameNotStarted)
	}
	if r.State.Status != StatusPlaying {
		return nil, errcode.Code(errcode.ErrGameAlreadyOver)
	}
	seat, ok := r.SeatOf(userID)
	if !ok {
		return nil, errcode.Code(errcode.ErrRoomNotIn)
	}
	if r.State.IsLandlord(seat) {
		r.State.Status = StatusFarmerWin
	} else {
		r.State.Status = StatusLandlordWin
	}
	r.State.Phase = PhaseOver
	return r, nil
}

// GetState 返回某玩家可见的对局视图。
func (m *DoudizhuManager) GetState(roomID, userID string) (*ClientGameState, *errcode.Error) {
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

// StateForSeat 在已持有房间引用时构造指定座位视图（供 ws 层逐座位下发）。
// 调用方负责加锁安全：本方法自身加锁。
func (m *DoudizhuManager) StateForSeat(roomID string, seat int) *ClientGameState {
	r := m.getRoom(roomID)
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return BuildClientState(roomID, r.Seats, seat, r.State)
}

// Seats 返回房间三座位 userID 的快照。
func (m *DoudizhuManager) Seats(roomID string) ([3]string, bool) {
	r := m.getRoom(roomID)
	if r == nil {
		return [3]string{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Seats, true
}

// RemoveGame 从管理器移除房间。
func (m *DoudizhuManager) RemoveGame(roomID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.rooms, roomID)
}

// ─────────────────── Spectator API ───────────────────

// SpectateGame 注册 userID 为房间观察者；若房间不存在则按需创建。
// 不会消耗座位。幂等。
func (m *DoudizhuManager) SpectateGame(roomID, userID string) (*DoudizhuRoom, *errcode.Error) {
	m.mu.Lock()
	r, ok := m.rooms[roomID]
	if !ok {
		r = &DoudizhuRoom{RoomID: roomID}
		m.rooms[roomID] = r
		logger.L().Info("doudizhu room created by spectator",
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
func (m *DoudizhuManager) UnspectateGame(roomID, userID string) *errcode.Error {
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
func (m *DoudizhuManager) SpectatorList(roomID string) []string {
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

// SpectatorState 返回观察者可见的客户端视图（手牌全部隐藏，只显张数）。
func (m *DoudizhuManager) SpectatorState(roomID, userID string) (*ClientGameState, *errcode.Error) {
	r := m.getRoom(roomID)
	if r == nil {
		return nil, errcode.Code(errcode.ErrRoomNotFound)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	// -1 ⇒ "no seat" ⇒ BuildClientState 会跳过 MyHand 填充。
	return BuildClientState(roomID, r.Seats, -1, r.State), nil
}

// SpectatorView 是 SpectatorState 的"无关 userID"变体——所有观察者共享同一
// 张重新构建的视图，构建一次即可向多个观察者广播。
func (m *DoudizhuManager) SpectatorView(roomID string) *ClientGameState {
	r := m.getRoom(roomID)
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return BuildClientState(roomID, r.Seats, -1, r.State)
}
