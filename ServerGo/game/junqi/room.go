package junqi

import (
	"encoding/json"
	"sync"

	"LsmWebGame/errcode"
	"LsmWebGame/logger"

	"go.uber.org/zap"
)

// ─────────────────── Room state ───────────────────

// JunqiRoom holds the in-memory state of one Junqi game.
type JunqiRoom struct {
	mu         sync.Mutex
	RoomID     string
	RedID      string
	BlackID    string
	Mode       VisibilityMode // 暗棋 or 明棋
	State      *GameState
	Spectators map[string]struct{}
}

// PlayerColor returns the color for the given user, or false if not a participant.
func (r *JunqiRoom) PlayerColor(userID string) (Color, bool) {
	if userID == r.RedID {
		return Red, true
	}
	if userID == r.BlackID {
		return Black, true
	}
	return 0, false
}

// IsReady reports whether both players have joined.
func (r *JunqiRoom) IsReady() bool {
	return r.RedID != "" && r.BlackID != ""
}

// IsLayoutComplete reports whether both players have submitted their layouts.
func (r *JunqiRoom) IsLayoutComplete() bool {
	if r.State == nil {
		return false
	}
	return r.State.LayoutDone[0] && r.State.LayoutDone[1]
}

// ─────────────────── Manager ───────────────────

// JunqiManager manages all active Junqi game rooms.
type JunqiManager struct {
	mu    sync.RWMutex
	rooms map[string]*JunqiRoom
}

// NewJunqiManager creates an empty manager.
func NewJunqiManager() *JunqiManager {
	return &JunqiManager{rooms: make(map[string]*JunqiRoom)}
}

// CreateGame creates a new game room. The creator plays Red. `mode` selects
// 暗棋 (hidden) or 明棋 (open). Idempotent for the same creator.
func (m *JunqiManager) CreateGame(roomID, redUserID string, mode VisibilityMode) *JunqiRoom {
	m.mu.Lock()
	defer m.mu.Unlock()

	if existing, ok := m.rooms[roomID]; ok {
		if existing.RedID == redUserID {
			return existing
		}
		return nil
	}

	r := &JunqiRoom{
		RoomID: roomID,
		RedID:  redUserID,
		Mode:   mode,
	}
	m.rooms[roomID] = r
	logger.L().Info("junqi game created",
		zap.String("room_id", roomID),
		zap.String("red", redUserID),
		zap.String("mode", mode.String()))
	return r
}

// JoinGame adds the second player as Black. Idempotent.
func (m *JunqiManager) JoinGame(roomID, blackUserID string) (r *JunqiRoom, started bool, e *errcode.Error) {
	m.mu.RLock()
	r, ok := m.rooms[roomID]
	m.mu.RUnlock()
	if !ok {
		return nil, false, errcode.Code(errcode.ErrRoomNotFound)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.RedID == blackUserID {
		return r, false, nil
	}
	if r.BlackID == blackUserID {
		return r, false, nil
	}
	if r.BlackID != "" {
		return nil, false, errcode.Code(errcode.ErrRoomFull)
	}

	r.BlackID = blackUserID
	r.State = NewGame()

	logger.L().Info("junqi game started",
		zap.String("room_id", roomID),
		zap.String("black", blackUserID))
	return r, true, nil
}

// SubmitLayout validates and applies a player's layout.
func (m *JunqiManager) SubmitLayout(roomID, userID string, placements []Placement) *errcode.Error {
	r := m.getRoom(roomID)
	if r == nil {
		return errcode.Code(errcode.ErrRoomNotFound)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State == nil {
		return errcode.Code(errcode.ErrGameNotStarted)
	}
	if r.State.Phase != PhaseLayout {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "layout phase is over")
	}

	color, ok := r.PlayerColor(userID)
	if !ok {
		return errcode.Code(errcode.ErrRoomNotIn)
	}

	if e := r.State.ApplyLayout(color, placements); e != nil {
		return e
	}

	logger.L().Info("junqi layout submitted",
		zap.String("room_id", roomID),
		zap.String("user_id", userID),
		zap.String("color", color.String()))
	return nil
}

// StartGameIfReady transitions from layout to playing phase if both players have submitted.
// Returns true if the game just started.
func (m *JunqiManager) StartGameIfReady(roomID string) bool {
	r := m.getRoom(roomID)
	if r == nil || r.State == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.State.Phase != PhaseLayout {
		return false
	}
	if !r.IsLayoutComplete() {
		return false
	}
	r.State.Phase = PhasePlaying
	r.State.Turn = Red
	logger.L().Info("junqi game entered playing phase", zap.String("room_id", roomID))
	return true
}

// MakeMove validates and executes a move.
func (m *JunqiManager) MakeMove(roomID, userID string, from, to Position) (*MoveResult, *errcode.Error) {
	r := m.getRoom(roomID)
	if r == nil {
		return nil, errcode.Code(errcode.ErrRoomNotFound)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State == nil {
		return nil, errcode.Code(errcode.ErrGameNotStarted)
	}
	if r.State.Phase != PhasePlaying {
		return nil, errcode.CodeMsg(errcode.ErrValidationFailed, "game is not in playing phase")
	}

	color, ok := r.PlayerColor(userID)
	if !ok {
		return nil, errcode.Code(errcode.ErrRoomNotIn)
	}

	if e := ValidateMove(r.State, from, to, color); e != nil {
		return nil, e
	}

	move := r.State.ExecuteMove(from, to)

	view := BoardView(r.State, color, r.Mode)

	result := &MoveResult{
		Move:      move,
		Turn:      r.State.Turn.String(),
		Status:    r.State.Status.String(),
		Phase:     r.State.Phase.String(),
		BoardView: viewToJSON(view),
		Board:     boardToSimpleJSON(r.State),
		MyColor:   color.String(),
	}

	logger.L().Info("junqi move",
		zap.String("room_id", roomID),
		zap.String("user_id", userID),
		zap.Int("from_x", from.X), zap.Int("from_y", from.Y),
		zap.Int("to_x", to.X), zap.Int("to_y", to.Y),
		zap.String("status", r.State.Status.String()))
	return result, nil
}

// Resign handles a player resigning.
func (m *JunqiManager) Resign(roomID, userID string) (*GameResult, *errcode.Error) {
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
	r.State.Phase = PhaseOver

	return &GameResult{
		Winner: color.Opposite().String(),
		Reason: "resign",
		Status: r.State.Status,
	}, nil
}

// GetState returns the current game state for a specific player.
func (m *JunqiManager) GetState(roomID, userID string) (*ClientGameState, *errcode.Error) {
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

	myColor := "red"
	if color == Black {
		myColor = "black"
	}

	if r.State == nil {
		return &ClientGameState{
			RoomID:  roomID,
			RedID:   r.RedID,
			BlackID: r.BlackID,
			Ready:   false,
			MyColor: myColor,
			Mode:    r.Mode.String(),
		}, nil
	}

	view := BoardView(r.State, color, r.Mode)
	return &ClientGameState{
		RoomID:    roomID,
		RedID:     r.RedID,
		BlackID:   r.BlackID,
		Ready:     r.IsReady(),
		MyColor:   myColor,
		Mode:      r.Mode.String(),
		Phase:     r.State.Phase.String(),
		Turn:      r.State.Turn.String(),
		Status:    r.State.Status.String(),
		BoardView: viewToJSON(view),
		MoveLen:   len(r.State.MoveHistory),
	}, nil
}

// RemoveGame removes a game room from the manager.
func (m *JunqiManager) RemoveGame(roomID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.rooms, roomID)
}

// buildSpectatorBoardView returns a "no-side" view for observers. Unlike a
// normal player's hidden-mode view (which reveals the viewer's OWN half and
// any combat-revealed opponent cells), the spectator view redacts every cell
// unless it has been publicly revealed by combat, engineer defusal, or
// flag-revealed-by-commander-death. This ensures observers never learn what
// each player had in their camp.
func (r *JunqiRoom) buildSpectatorBoardView() [][]ClientPieceView {
	if r.State == nil {
		return nil
	}
	var view [12][5]ClientPieceView
	gs := r.State
	for y := 0; y <= 11; y++ {
		for x := 0; x <= 4; x++ {
			pos := Position{X: x, Y: y}
			p := gs.Board[y][x]
			if p == nil {
				view[y][x] = EmptyView()
				continue
			}
			// A spectator only sees what was public to at least one side.
			if isRevealed(gs, pos, Red) || isRevealed(gs, pos, Black) {
				v := FullView(p)
				v.Revealed = true
				view[y][x] = v
				continue
			}
			view[y][x] = HiddenView()
		}
	}
	return viewToJSON(view)
}

// ─────────────────── Spectator API ───────────────────
//
// For junqi, spectators are particularly important in hidden mode: the
// server already filters the per-player view via BoardViewFor; we expose a
// similar but slightly redacted view so that observers don't see either
// player's hidden pieces. In open mode the board is public, so the spectator
// view mirrors that.

// SpectateGame registers userID as a spectator of the room and defaults the
// visibility mode to hidden (暗棋) if this is the first joiner. Idempotent.
func (m *JunqiManager) SpectateGame(roomID, userID string, mode VisibilityMode) (*JunqiRoom, *errcode.Error) {
	m.mu.Lock()
	r, ok := m.rooms[roomID]
	if !ok {
		r = &JunqiRoom{RoomID: roomID, Mode: mode}
		if r.Mode == 0 {
			r.Mode = ModeHidden
		}
		m.rooms[roomID] = r
		logger.L().Info("junqi room created by spectator",
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

// UnspectateGame removes userID from the spectator set.
func (m *JunqiManager) UnspectateGame(roomID, userID string) *errcode.Error {
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
func (m *JunqiManager) SpectatorList(roomID string) []string {
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

// SpectatorState returns a sanitized client view for a spectator. In hidden
// mode, neither player's hidden pieces are revealed; we just emit placeholder
// rows for the layout phase. In open mode the full board is public.
func (m *JunqiManager) SpectatorState(roomID, userID string) (*ClientGameState, *errcode.Error) {
	r := m.getRoom(roomID)
	if r == nil {
		return nil, errcode.Code(errcode.ErrRoomNotFound)
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	cs := &ClientGameState{
		RoomID:  roomID,
		RedID:   r.RedID,
		BlackID: r.BlackID,
		Mode:    r.Mode.String(),
		Phase:   PhaseLayout.String(),
		Ready:   r.IsReady(),
		MyColor: "", // omitted for spectators
	}

	if r.State == nil {
		return cs, nil
	}

	cs.Phase = r.State.Phase.String()
	cs.Turn = r.State.Turn.String()
	cs.Status = r.State.Status.String()
	cs.MoveLen = len(r.State.MoveHistory)

	// In open mode the full board is public — emit it for spectators too.
	if r.Mode == ModeOpen {
		cs.BoardView = viewToJSON(BoardView(r.State, Red, ModeOpen))
	}
	// In hidden mode, build a "no-side" board view: each cell shows either
	// the visible piece (if revealed by combat) or a generic hidden marker.
	if r.Mode == ModeHidden {
		cs.BoardView = r.buildSpectatorBoardView()
	}
	return cs, nil
}

func (m *JunqiManager) getRoom(roomID string) *JunqiRoom {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.rooms[roomID]
}

// ─────────────────── Response types ───────────────────

// MoveResult is the response after a successful move.
type MoveResult struct {
	Move      Move                 `json:"move"`
	Turn      string               `json:"turn"`
	Status    string               `json:"status"`
	Phase     string               `json:"phase"`
	BoardView [][]ClientPieceView  `json:"board_view"` // per-player view
	Board     [][]*PieceJSON       `json:"board"`      // full board (open mode only — hidden mode duplicates board_view)
	MyColor   string               `json:"my_color"`
}

// GameResult is the response when a game ends.
type GameResult struct {
	Winner string     `json:"winner"`
	Reason string     `json:"reason"`
	Status GameStatus `json:"status"`
}

// ClientGameState is the game state sent to a specific player.
type ClientGameState struct {
	RoomID    string                `json:"room_id"`
	RedID     string                `json:"red_id"`
	BlackID   string                `json:"black_id"`
	Ready     bool                  `json:"ready"`
	MyColor   string                `json:"my_color,omitempty"`
	Mode      string                `json:"mode"`
	Phase     string                `json:"phase,omitempty"`
	Turn      string                `json:"turn,omitempty"`
	Status    string                `json:"status,omitempty"`
	BoardView [][]ClientPieceView   `json:"board_view,omitempty"`
	MoveLen   int                   `json:"move_count"`
}

// viewToJSON serializes the client piece view so it can be sent across WS.
// Returns the 2D array form (12 rows × 5 cols) for JSON.
func viewToJSON(view [12][5]ClientPieceView) [][]ClientPieceView {
	out := make([][]ClientPieceView, 12)
	for y := 0; y <= 11; y++ {
		row := make([]ClientPieceView, 5)
		copy(row, view[y][:])
		out[y] = row
	}
	return out
}

// boardToSimpleJSON returns the full board as 2D JSON (open-mode helper).
func boardToSimpleJSON(gs *GameState) [][]*PieceJSON {
	return gs.BoardJSON()
}

// MarshalJSON customizes JSON serialization so the BoardView goes out as 2D slices.
func (mr MoveResult) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Move      Move                `json:"move"`
		Turn      string              `json:"turn"`
		Status    string              `json:"status"`
		Phase     string              `json:"phase"`
		BoardView [][]ClientPieceView `json:"board_view"`
		Board     [][]*PieceJSON      `json:"board"`
		MyColor   string              `json:"my_color"`
	}{
		Move:      mr.Move,
		Turn:      mr.Turn,
		Status:    mr.Status,
		Phase:     mr.Phase,
		BoardView: mr.BoardView,
		Board:     mr.Board,
		MyColor:   mr.MyColor,
	})
}

// MarshalJSON for ClientGameState flattens BoardView to 2D.
func (cs ClientGameState) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		RoomID    string              `json:"room_id"`
		RedID     string              `json:"red_id"`
		BlackID   string              `json:"black_id"`
		Ready     bool                `json:"ready"`
		MyColor   string              `json:"my_color,omitempty"`
		Mode      string              `json:"mode"`
		Phase     string              `json:"phase,omitempty"`
		Turn      string              `json:"turn,omitempty"`
		Status    string              `json:"status,omitempty"`
		BoardView [][]ClientPieceView `json:"board_view,omitempty"`
		MoveLen   int                 `json:"move_count"`
	}{
		RoomID:    cs.RoomID,
		RedID:     cs.RedID,
		BlackID:   cs.BlackID,
		Ready:     cs.Ready,
		MyColor:   cs.MyColor,
		Mode:      cs.Mode,
		Phase:     cs.Phase,
		Turn:      cs.Turn,
		Status:    cs.Status,
		BoardView: cs.BoardView,
		MoveLen:   cs.MoveLen,
	})
}