package chess

import (
	"encoding/json"
	"sync"

	"LsmAgentGame/errcode"
	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// ─────────────────── Room state ───────────────────

// ChessRoom holds the in-memory state of one Chess game.
type ChessRoom struct {
	mu       sync.Mutex
	RoomID   string
	WhiteID  string // seat 0 — White player UserID (first to join, plays first)
	BlackID  string // seat 1 — Black player UserID (empty until 2nd player joins)
	State    *GameState
	// LastEndReason records the most recent end-of-game reason (set when the game ends).
	LastEndReason ReasonEndGame
	// LastEndWinner records the winning side, or "" for draw.
	LastEndWinner string
	// Spectators tracks observer userIDs without seats.
	Spectators map[string]struct{}
}

// PlayerColor returns the color for the given user, or false if not a participant.
func (r *ChessRoom) PlayerColor(userID string) (Color, bool) {
	if userID == r.WhiteID {
		return White, true
	}
	if userID == r.BlackID {
		return Black, true
	}
	return 0, false
}

// IsReady reports whether both players have joined.
func (r *ChessRoom) IsReady() bool {
	return r.WhiteID != "" && r.BlackID != ""
}

// ReasonForStatus converts a finished GameStatus + halfMove/key reasons into a JSON reason string.
func (r *ChessRoom) ReasonForStatus() ReasonEndGame {
	if r.State == nil {
		return ReasonResign
	}
	switch r.LastEndReason {
	case ReasonCheckmate, ReasonStalemate, ReasonFiftyMove, ReasonInsufficient, ReasonThreefold, ReasonResign:
		return r.LastEndReason
	}
	// Infer from status if not already set.
	switch r.State.Status {
	case StatusDraw:
		return ReasonStalemate
	case StatusWhiteWin, StatusBlackWin:
		return ReasonCheckmate
	}
	return ""
}

// ─────────────────── Manager ───────────────────

// ChessManager manages all active Chess game rooms.
type ChessManager struct {
	mu    sync.RWMutex
	rooms map[string]*ChessRoom
}

// NewChessManager creates an empty manager.
func NewChessManager() *ChessManager {
	return &ChessManager{rooms: make(map[string]*ChessRoom)}
}

// CreateGame creates a new game room. The creator plays White (first move).
// Idempotent: if the same user already created this room, it returns the existing room.
func (m *ChessManager) CreateGame(roomID, whiteUserID string) *ChessRoom {
	m.mu.Lock()
	defer m.mu.Unlock()

	if existing, ok := m.rooms[roomID]; ok {
		if existing.WhiteID == whiteUserID {
			return existing
		}
		return nil // different user — room already exists
	}

	r := &ChessRoom{
		RoomID:  roomID,
		WhiteID: whiteUserID,
	}
	m.rooms[roomID] = r
	logger.L().Info("chess game created",
		zap.String("room_id", roomID),
		zap.String("white", whiteUserID))
	return r
}

// JoinGame adds the second player as Black and starts the game.
// started is true when a fresh game was just created (both players present for the
// first time); false on idempotent reconnection (player already seated).
func (m *ChessManager) JoinGame(roomID, blackUserID string) (r *ChessRoom, started bool, e *errcode.Error) {
	m.mu.RLock()
	r, ok := m.rooms[roomID]
	m.mu.RUnlock()
	if !ok {
		return nil, false, errcode.Code(errcode.ErrRoomNotFound)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Idempotent reconnection: if this user is already White or Black, return existing.
	if r.WhiteID == blackUserID || r.BlackID == blackUserID {
		return r, false, nil
	}

	if r.BlackID != "" {
		return nil, false, errcode.Code(errcode.ErrRoomFull)
	}

	r.BlackID = blackUserID
	r.State = NewGame()

	logger.L().Info("chess game started",
		zap.String("room_id", roomID),
		zap.String("black", blackUserID))
	return r, true, nil
}

// MakeMove validates and executes a move. promotion is 0 unless the pawn reached
// the back rank — in which case the client must choose a piece and submit a new move.
func (m *ChessManager) MakeMove(roomID, userID string, from, to Position, promotion PieceType) (*MoveResult, *errcode.Error) {
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

	if _, e := r.State.ValidateMove(from, to, color, promotion); e != nil {
		return nil, e
	}
	move := r.State.ExecuteMove(from, to, color, promotion)

	// Determine end-of-game reason if applicable.
	reason := ""
	if r.State.Status != StatusPlaying {
		reason = string(r.ReasonForStatus())
	}

	result := &MoveResult{
		Move:   move,
		Turn:   r.State.Turn,
		Status: r.State.Status,
		Check:  r.State.IsInCheck(r.State.Turn),
		Board:  r.State.BoardJSON(),
		Reason: reason,
	}

	logger.L().Info("chess move",
		zap.String("room_id", roomID),
		zap.String("user_id", userID),
		zap.Int("from_x", from.X), zap.Int("from_y", from.Y),
		zap.Int("to_x", to.X), zap.Int("to_y", to.Y))
	return result, nil
}

// Resign makes a player resign.
func (m *ChessManager) Resign(roomID, userID string) (*GameResult, *errcode.Error) {
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

	if color == White {
		r.State.Status = StatusBlackWin
	} else {
		r.State.Status = StatusWhiteWin
	}
	r.LastEndReason = ReasonResign
	r.LastEndWinner = color.Opposite().String()

	return &GameResult{
		Winner: r.LastEndWinner,
		Reason: "resign",
		Status: r.State.Status,
	}, nil
}

// GetState returns the current game state for a specific player.
func (m *ChessManager) GetState(roomID, userID string) (*ClientGameState, *errcode.Error) {
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
			WhiteID: r.WhiteID,
			BlackID: r.BlackID,
			Ready:   false,
		}, nil
	}

	myColor := "white"
	if color == Black {
		myColor = "black"
	}

	return &ClientGameState{
		RoomID:   roomID,
		WhiteID:  r.WhiteID,
		BlackID:  r.BlackID,
		Ready:    true,
		Board:    r.State.BoardJSON(),
		Turn:     r.State.Turn.String(),
		MyColor:  myColor,
		Status:   r.State.Status.String(),
		Check:    r.State.IsInCheck(r.State.Turn),
		MoveLen:  len(r.State.MoveHistory),
		Reason:   string(r.ReasonForStatus()),
	}, nil
}

// RemoveGame removes a game room from the manager.
func (m *ChessManager) RemoveGame(roomID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.rooms, roomID)
}

// ─────────────────── Spectator API ───────────────────

// SpectateGame registers userID as a spectator of the room; creates the
// room on demand so a spectator can wait for both players to arrive.
// Idempotent.
func (m *ChessManager) SpectateGame(roomID, userID string) (*ChessRoom, *errcode.Error) {
	m.mu.Lock()
	r, ok := m.rooms[roomID]
	if !ok {
		r = &ChessRoom{RoomID: roomID}
		m.rooms[roomID] = r
		logger.L().Info("chess room created by spectator",
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

// UnspectateGame removes userID from the room's spectator set.
func (m *ChessManager) UnspectateGame(roomID, userID string) *errcode.Error {
	r := m.getRoom(roomID)
	if r == nil {
		return errcode.Code(errcode.ErrRoomNotFound)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.Spectators, userID)
	return nil
}

// SpectatorList returns currently-registered spectator userIDs.
func (m *ChessManager) SpectatorList(roomID string) []string {
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

// SpectatorState returns the sanitized client view for a spectator.
func (m *ChessManager) SpectatorState(roomID, userID string) (*ClientGameState, *errcode.Error) {
	r := m.getRoom(roomID)
	if r == nil {
		return nil, errcode.Code(errcode.ErrRoomNotFound)
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State == nil {
		return &ClientGameState{
			RoomID:  roomID,
			WhiteID: r.WhiteID,
			BlackID: r.BlackID,
			Ready:   false,
		}, nil
	}

	return &ClientGameState{
		RoomID:   roomID,
		WhiteID:  r.WhiteID,
		BlackID:  r.BlackID,
		Ready:    true,
		Board:    r.State.BoardJSON(),
		Turn:     r.State.Turn.String(),
		Status:   r.State.Status.String(),
		Check:    r.State.IsInCheck(r.State.Turn),
		MoveLen:  len(r.State.MoveHistory),
		MyColor:  "",
	}, nil
}

func (m *ChessManager) getRoom(roomID string) *ChessRoom {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.rooms[roomID]
}

// ─────────────────── Response types ───────────────────

// MoveResult is the response after a successful move.
type MoveResult struct {
	Move   Move         `json:"move"`
	Turn   Color        `json:"turn"`
	Status GameStatus   `json:"status"`
	Check  bool         `json:"check"`
	Board  [][]*PieceJSON `json:"board"`
	Reason string       `json:"reason,omitempty"`
}

// MarshalJSON customizes JSON serialization so Turn and Status appear as strings.
func (mr MoveResult) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Move   Move           `json:"move"`
		Turn   string         `json:"turn"`
		Status string         `json:"status"`
		Check  bool           `json:"check"`
		Board  [][]*PieceJSON `json:"board"`
		Reason string         `json:"reason,omitempty"`
	}{
		Move:   mr.Move,
		Turn:   mr.Turn.String(),
		Status: mr.Status.String(),
		Check:  mr.Check,
		Board:  mr.Board,
		Reason: mr.Reason,
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
	WhiteID string         `json:"white_id"`
	BlackID string         `json:"black_id"`
	Ready   bool           `json:"ready"`
	Board   [][]*PieceJSON `json:"board,omitempty"`
	Turn    string         `json:"turn,omitempty"`
	MyColor string         `json:"my_color,omitempty"`
	Status  string         `json:"status,omitempty"`
	Check   bool           `json:"check"`
	MoveLen int            `json:"move_count"`
	Reason  string         `json:"reason,omitempty"`
}
