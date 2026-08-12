package xiangqi

import (
	"encoding/json"
	"sync"

	"LsmAgentGame/errcode"
	"LsmAgentGame/logger"
	"LsmAgentGame/util"

	"go.uber.org/zap"
)

// ─────────────────── Room state ───────────────────

// XiangqiRoom holds the in-memory state of one Xiangqi game.
type XiangqiRoom struct {
	mu          sync.Mutex
	RoomID      string
	RedID       string // seat 0 — Red player UserID (first to join)
	BlackID     string // seat 1 — Black player UserID (empty until 2nd player joins)
	State       *GameState
	Spectators  map[string]struct{} // userID → is watching (sanitized view only)
}

// PlayerColor returns the color for the given user, or false if not a participant.
func (r *XiangqiRoom) PlayerColor(userID string) (Color, bool) {
	if userID == r.RedID {
		return Red, true
	}
	if userID == r.BlackID {
		return Black, true
	}
	return 0, false
}

// IsReady reports whether both players have joined.
func (r *XiangqiRoom) IsReady() bool {
	return r.RedID != "" && r.BlackID != ""
}

// ─────────────────── Manager ───────────────────

// XiangqiManager manages all active Xiangqi game rooms.
type XiangqiManager struct {
	mu    sync.RWMutex
	rooms map[string]*XiangqiRoom // roomID -> room
}

// NewXiangqiManager creates an empty manager.
func NewXiangqiManager() *XiangqiManager {
	return &XiangqiManager{rooms: make(map[string]*XiangqiRoom)}
}

// CreateGame creates a new game room. The creator plays Red (first move).
// Idempotent: if the same user already created this room, it returns the existing room.
func (m *XiangqiManager) CreateGame(roomID, redUserID string) *XiangqiRoom {
	m.mu.Lock()
	defer m.mu.Unlock()

	if existing, ok := m.rooms[roomID]; ok {
		if existing.RedID == redUserID {
			return existing // idempotent: same creator, return existing
		}
		return nil // different user — room already exists
	}

	r := &XiangqiRoom{
		RoomID: roomID,
		RedID:  redUserID,
	}
	m.rooms[roomID] = r
	logger.L().Info("xiangqi game created",
		zap.String("room_id", roomID),
		zap.String("red", redUserID))
	return r
}

// JoinGame adds the second player as Black and starts the game.
// Idempotent: if the same user already joined, returns existing room.
func (m *XiangqiManager) JoinGame(roomID, blackUserID string) (r *XiangqiRoom, started bool, e *errcode.Error) {
	m.mu.RLock()
	r, ok := m.rooms[roomID]
	m.mu.RUnlock()
	if !ok {
		return nil, false, errcode.Code(errcode.ErrRoomNotFound)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Idempotent: if this user is already Red, return the room (reconnection).
	if r.RedID == blackUserID {
		return r, false, nil
	}
	// Idempotent: if this user is already Black, return the room (reconnection).
	if r.BlackID == blackUserID {
		return r, false, nil
	}

	if r.BlackID != "" {
		return nil, false, errcode.Code(errcode.ErrRoomFull)
	}

	r.BlackID = blackUserID
	r.State = NewGame()

	logger.L().Info("xiangqi game started",
		zap.String("room_id", roomID),
		zap.String("black", blackUserID))
	return r, true, nil
}

// MakeMove validates and executes a move.
func (m *XiangqiManager) MakeMove(roomID, userID string, from, to Position) (*MoveResult, *errcode.Error) {
	r := m.getRoom(roomID)
	if r == nil {
		return nil, errcode.Code(errcode.ErrRoomNotFound)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State == nil {
		return nil, errcode.Code(errcode.ErrGameNotStarted)
	}

	color, ok := r.PlayerColor(userID)
	if !ok {
		return nil, errcode.Code(errcode.ErrRoomNotIn)
	}

	if e := r.State.ValidateMove(from, to, color); e != nil {
		return nil, e
	}

	move := r.State.ExecuteMove(from, to)

	result := &MoveResult{
		Move:    move,
		Turn:    r.State.Turn,
		Status:  r.State.Status,
		Check:   r.State.IsInCheck(r.State.Turn),
		Board:   r.State.BoardJSON(),
	}

	logger.L().Info("xiangqi move",
		zap.String("room_id", roomID),
		zap.String("user_id", userID),
		zap.Int("from_x", from.X), zap.Int("from_y", from.Y),
		zap.Int("to_x", to.X), zap.Int("to_y", to.Y))
	return result, nil
}

// Resign makes a player resign.
func (m *XiangqiManager) Resign(roomID, userID string) (*GameResult, *errcode.Error) {
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

	color, ok := r.PlayerColor(userID)
	if !ok {
		return nil, errcode.Code(errcode.ErrRoomNotIn)
	}

	if color == Red {
		r.State.Status = StatusBlackWin
	} else {
		r.State.Status = StatusRedWin
	}

	return &GameResult{
		Winner: color.Opposite().String(),
		Reason: "resign",
		Status: r.State.Status,
	}, nil
}

// OfferDraw records a draw offer. If both players offer, the game is drawn.
func (m *XiangqiManager) OfferDraw(roomID, userID string) (*GameResult, *errcode.Error) {
	r := m.getRoom(roomID)
	if r == nil {
		return nil, errcode.Code(errcode.ErrRoomNotFound)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State == nil || r.State.Status != StatusPlaying {
		return nil, errcode.Code(errcode.ErrGameNotStarted)
	}

	_, ok := r.PlayerColor(userID)
	if !ok {
		return nil, errcode.Code(errcode.ErrRoomNotIn)
	}

	// Simple implementation: both must offer by calling this sequentially.
	// In a real system, track draw_offers per player. For now, require both calls.
	// This is handled by the game service layer (track offers externally).
	return nil, nil
}

// GetState returns the current game state for a specific player.
func (m *XiangqiManager) GetState(roomID, userID string) (*ClientGameState, *errcode.Error) {
	r := m.getRoom(roomID)
	if r == nil {
		return nil, errcode.Code(errcode.ErrRoomNotFound)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	color, isPlayer := r.PlayerColor(userID)
	if !isPlayer {
		return nil, errcode.Code(errcode.ErrRoomNotIn)
	}

	if r.State == nil {
		return &ClientGameState{
			RoomID:  roomID,
			RedID:   r.RedID,
			BlackID: r.BlackID,
			Ready:   false,
		}, nil
	}

	myColor := "red"
	if color == Black {
		myColor = "black"
	}

	return &ClientGameState{
		RoomID:   roomID,
		RedID:    r.RedID,
		BlackID:  r.BlackID,
		Ready:    true,
		Board:    r.State.BoardJSON(),
		Turn:     r.State.Turn.String(),
		MyColor:  myColor,
		Status:   r.State.Status.String(),
		Check:    r.State.IsInCheck(r.State.Turn),
		MoveLen:  len(r.State.MoveHistory),
	}, nil
}

// RemoveGame removes a game room from the manager.
func (m *XiangqiManager) RemoveGame(roomID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.rooms, roomID)
}

// ─────────────────── Spectator API ───────────────────
//
// Spectators attach to a room without taking a seat. They receive a sanitized
// view of the game (board + turn + status) but no `my_color`, no chat
// participant count, no input affordances. A spectator cannot send any
// `game.*` input frame — see ws/game_service.handleXiangqiSpectate for the
// permission gate.

// SpectateGame registers userID as a spectator of the room. If the room
// does not exist yet (no one has joined), it is created on the spectator's
// behalf with no players and a nil State; the spectator can wait for the
// first player to arrive. Idempotent.
func (m *XiangqiManager) SpectateGame(roomID, userID string) (*XiangqiRoom, *errcode.Error) {
	m.mu.Lock()
	r, ok := m.rooms[roomID]
	if !ok {
		r = &XiangqiRoom{RoomID: roomID}
		m.rooms[roomID] = r
		logger.L().Info("xiangqi room created by spectator",
			zap.String("room_id", roomID),
			zap.String("user_id", userID))
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

// UnspectateGame removes userID from the room's spectator set. Idempotent.
func (m *XiangqiManager) UnspectateGame(roomID, userID string) *errcode.Error {
	r := m.getRoom(roomID)
	if r == nil {
		return errcode.Code(errcode.ErrRoomNotFound)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.Spectators, userID)
	return nil
}

// SpectatorList returns the userIDs currently registered as spectators. The
// game service uses this to fan out `game.spectators_resp` frames.
func (m *XiangqiManager) SpectatorList(roomID string) []string {
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

// SpectatorState returns the sanitized client view for a spectator. No
// `my_color` is set (spectators are not playing either side); the board is
// public anyway, so the player fields are filtered but the board itself is
// included verbatim.
func (m *XiangqiManager) SpectatorState(roomID, userID string) (*ClientGameState, *errcode.Error) {
	r := m.getRoom(roomID)
	if r == nil {
		return nil, errcode.Code(errcode.ErrRoomNotFound)
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State == nil {
		// Game hasn't started yet — only red is known.
		return &ClientGameState{
			RoomID:  roomID,
			RedID:   r.RedID,
			BlackID: r.BlackID,
			Ready:   false,
		}, nil
	}

	return &ClientGameState{
		RoomID:   roomID,
		RedID:    r.RedID,
		BlackID:  r.BlackID,
		Ready:    true,
		Board:    r.State.BoardJSON(),
		Turn:     r.State.Turn.String(),
		Status:   r.State.Status.String(),
		Check:    r.State.IsInCheck(r.State.Turn),
		MoveLen:  len(r.State.MoveHistory),
		MyColor:  "", // explicitly omitted for spectators
	}, nil
}

func (m *XiangqiManager) getRoom(roomID string) *XiangqiRoom {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.rooms[roomID]
}

// ─────────────────── Response types ───────────────────

// MoveResult is the response after a successful move.
type MoveResult struct {
	Move   Move        `json:"move"`
	Turn   Color       `json:"turn"`
	Status GameStatus  `json:"status"`
	Check  bool        `json:"check"`
	Board  [][]*PieceJSON `json:"board"`
}

// MarshalJSON customizes JSON serialization.
func (mr MoveResult) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Move   Move           `json:"move"`
		Turn   string         `json:"turn"`
		Status string         `json:"status"`
		Check  bool           `json:"check"`
		Board  [][]*PieceJSON `json:"board"`
	}{
		Move:   mr.Move,
		Turn:   mr.Turn.String(),
		Status: mr.Status.String(),
		Check:  mr.Check,
		Board:  mr.Board,
	})
}

// GameResult is the response when a game ends.
type GameResult struct {
	Winner string     `json:"winner"`
	Reason string     `json:"reason"`
	Status GameStatus `json:"status"`
}

// ClientGameState is the game state sent to a specific player.
type ClientGameState struct {
	RoomID  string         `json:"room_id"`
	RedID   string         `json:"red_id"`
	BlackID string         `json:"black_id"`
	Ready   bool           `json:"ready"`
	Board   [][]*PieceJSON `json:"board,omitempty"`
	Turn    string         `json:"turn,omitempty"`
	MyColor string         `json:"my_color,omitempty"`
	Status  string         `json:"status,omitempty"`
	Check   bool           `json:"check"`
	MoveLen int            `json:"move_count"`
}

// Color.String returns "red" or "black".
func (c Color) String() string {
	if c == Red {
		return "red"
	}
	return "black"
}

// ParseColor parses a color string.
func ParseColor(s string) (Color, bool) {
	switch s {
	case "red":
		return Red, true
	case "black":
		return Black, true
	default:
		return 0, false
	}
}

// ColorForSeat returns the color for a seat number (0=red, 1=black).
func ColorForSeat(seat int) Color {
	if seat == 0 {
		return Red
	}
	return Black
}

// String returns the color name.
func (p PieceJSON) String() string {
	return util.NewUUID() // placeholder; PieceJSON is just a DTO
}
