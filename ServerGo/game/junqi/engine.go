// Package junqi implements the Chinese Army Chess (陆战棋 / 军棋) game engine.
//
// Board coordinate system:
//   - x ∈ [0,4] (5 columns, left → right from each player's perspective)
//   - y ∈ [0,11] (12 rows total: 6 rows for each side, no row for mountain)
//   - Red side: y ∈ [0,5] (Red home row = y=0)
//   - Black side: y ∈ [6,11] (Black home row = y=11)
//   - The "mountain border" is conceptually between y=5 and y=6, but since
//     we model the board as 12 contiguous rows, mountain crossing is a
//     movement constraint (must use a railway, can't stop on the gap).
//
// Cell topology (per half — mirrored for the other color):
//   - 5 columns × 6 rows = 30 cells per side
//   - Within those 30 cells: road (path), railway (path), camp (5 圆),
//     HQ (2 个), and several "blocked" cells where mountain sits between.
//
// Per side the canonical cell layout (rows = 0..5, cols = 0..4):
//
//       col 0  col 1  col 2  col 3  col 4
// row 0 [ HQ ]  road   road   road  [ HQ ]
// row 1  rail   rail   rail   rail   rail
// row 2  road [camp]  road [camp]  road
// row 3  rail   rail   rail   rail   rail
// row 4  road [camp]  road [camp]  road
// row 5  rail   rail   rail   rail   rail
//
// The two rows immediately in front of HQ (rows 1 and 2) form an "arc"
// connecting rail corners. Same for the opposing side.
package junqi

import "fmt"

// Color represents a side.
type Color int

const (
	Red   Color = 0
	Black Color = 1
)

// Opposite returns the other color.
func (c Color) Opposite() Color {
	if c == Red {
		return Black
	}
	return Red
}

// String returns "red" or "black".
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

// PieceType enumerates piece kinds.
type PieceType int

const (
	Flag      PieceType = 1  // 军旗 — cannot move
	Commander PieceType = 2  // 司令 (rank 9)
	General   PieceType = 3  // 军长 (rank 8)
	Major     PieceType = 4  // 师长 (rank 7)
	Colonel   PieceType = 5  // 旅长 (rank 6)
	Captain   PieceType = 6  // 团长 (rank 5)
	Lieutenant PieceType = 7 // 营长 (rank 4)
	Sergeant  PieceType = 8  // 连长 (rank 3)
	Engineer  PieceType = 9  // 工兵 (rank 1, can fly on rails, defuse mines)
	Bomb      PieceType = 10 // 炸弹 — mutual destruction
	Mine      PieceType = 11 // 地雷 — immobile, kills anything except engineer
)

// Rank returns the combat rank (1=lowest, 9=highest). Flag/Bomb/Mine have no rank.
func (t PieceType) Rank() int {
	switch t {
	case Commander:
		return 9
	case General:
		return 8
	case Major:
		return 7
	case Colonel:
		return 6
	case Captain:
		return 5
	case Lieutenant:
		return 4
	case Sergeant:
		return 3
	case Engineer:
		return 1
	default:
		return 0 // Flag, Bomb, Mine — handled specially
	}
}

// HasRank reports whether the piece has a combat rank (i.e. can compare with >=/<).
func (t PieceType) HasRank() bool {
	return t != Flag && t != Bomb && t != Mine
}

// Piece names for each type and color. Red/Black use different characters
// for the same rank to mirror Chinese military chess tradition.
var pieceNames = map[Color]map[PieceType]string{
	Red: {
		Flag: "军旗", Commander: "司令", General: "军长",
		Major: "师长", Colonel: "旅长", Captain: "团长",
		Lieutenant: "营长", Sergeant: "连长", Engineer: "工兵",
		Bomb: "炸弹", Mine: "地雷",
	},
	Black: {
		Flag: "军旗", Commander: "司令", General: "军长",
		Major: "师长", Colonel: "旅长", Captain: "团长",
		Lieutenant: "营长", Sergeant: "连长", Engineer: "工兵",
		Bomb: "炸弹", Mine: "地雷",
	},
}

// Name returns the Chinese name of a piece.
func (p Piece) Name() string {
	return pieceNames[p.Color][p.Type]
}

// Piece is a single piece on the board.
type Piece struct {
	Color Color
	Type  PieceType
}

// Position is a board intersection.
type Position struct {
	X int `json:"x"` // 0-4 column
	Y int `json:"y"` // 0-11 row
}

// IsValid reports whether the position is on the board.
func (p Position) IsValid() bool {
	return p.X >= 0 && p.X <= 4 && p.Y >= 0 && p.Y <= 11
}

// Move records a single ply.
type Move struct {
	From              Position `json:"from"`
	To                Position `json:"to"`
	Piece             Piece    `json:"piece"`
	Captured          *Piece   `json:"captured,omitempty"`     // nil if no capture or both destroyed
	EngineerDefused   bool     `json:"engineer_defused,omitempty"` // 工兵排雷
	BothDestroyed     bool     `json:"both_destroyed,omitempty"`   // 同归于尽
	RevealedPiece     *Piece   `json:"revealed_piece,omitempty"`  // 暗棋：翻明对方棋子
}

// GameStatus represents the state of the game.
type GameStatus int

const (
	StatusPlaying  GameStatus = 0
	StatusRedWin   GameStatus = 1
	StatusBlackWin GameStatus = 2
	StatusDraw     GameStatus = 3
)

// String returns a human-readable status.
func (s GameStatus) String() string {
	switch s {
	case StatusPlaying:
		return "playing"
	case StatusRedWin:
		return "red_win"
	case StatusBlackWin:
		return "black_win"
	case StatusDraw:
		return "draw"
	default:
		return "unknown"
	}
}

// GamePhase indicates which phase the game is in.
type GamePhase int

const (
	PhaseLayout  GamePhase = 0
	PhasePlaying GamePhase = 1
	PhaseOver    GamePhase = 2
)

// String returns a human-readable phase.
func (p GamePhase) String() string {
	switch p {
	case PhaseLayout:
		return "layout"
	case PhasePlaying:
		return "playing"
	case PhaseOver:
		return "over"
	default:
		return "unknown"
	}
}

// Placement describes where a player wants to place a piece during layout.
type Placement struct {
	Type PieceType `json:"type"`
	At   Position  `json:"at"`
}

// GameState holds the complete state of one game.
type GameState struct {
	Board        [12][5]*Piece
	Turn         Color
	Status       GameStatus
	Phase        GamePhase
	LayoutDone   [2]bool // 红/黑是否提交布局
	FlagRevealed [2]bool // 司令阵亡 → 己方军旗位置公开（暗棋专用）
	MoveHistory  []Move
	NoCaptureCount int // 70 回合无吃子 = 和棋
	KnownToRed   [12][5]bool // red knows the piece at this cell (initially all red cells)
	KnownToBlack [12][5]bool // black knows the piece at this cell (initially all black cells)
	// NoPiece is a sentinel — stored in Board[y][x] when the cell is empty.
	// (using nil, see Get/Set below)
}

// NewGame returns a fresh, empty (no pieces) game state for the layout phase.
func NewGame() *GameState {
	gs := &GameState{
		Turn:   Red, // Red moves first once layout is complete
		Status: StatusPlaying,
		Phase:  PhaseLayout,
	}
	return gs
}

// Get returns the piece at pos, or nil if empty.
func (gs *GameState) Get(pos Position) *Piece {
	if !pos.IsValid() {
		return nil
	}
	return gs.Board[pos.Y][pos.X]
}

// Set places a piece (may be nil) at a position. Used during layout and moves.
func (gs *GameState) set(pos Position, p *Piece) {
	if pos.IsValid() {
		gs.Board[pos.Y][pos.X] = p
	}
}

// PieceJSON is a JSON-friendly representation of a piece.
type PieceJSON struct {
	Color string `json:"color,omitempty"`
	Type  string `json:"type,omitempty"`
	Name  string `json:"name,omitempty"`
	// For dark mode / hidden mode
	Hidden   bool `json:"hidden,omitempty"`
	Revealed bool `json:"revealed,omitempty"`
}

var pieceTypeNames = map[PieceType]string{
	Flag: "flag", Commander: "commander", General: "general",
	Major: "major", Colonel: "colonel", Captain: "captain",
	Lieutenant: "lieutenant", Sergeant: "sergeant", Engineer: "engineer",
	Bomb: "bomb", Mine: "mine",
}

// PieceTypeName returns the lower-case English name of a piece type.
func PieceTypeName(t PieceType) string {
	return pieceTypeNames[t]
}

// ParsePieceType parses a piece type name back into a PieceType.
func ParsePieceType(s string) (PieceType, bool) {
	for t, name := range pieceTypeNames {
		if name == s {
			return t, true
		}
	}
	return 0, false
}

// BoardJSON returns the board as a JSON-serializable 2D array.
// Each row is a slice of 5 PieceJSON entries (nil = empty cell).
// Used for "open mode" / 明棋 where both sides see all pieces.
func (gs *GameState) BoardJSON() [][]*PieceJSON {
	board := make([][]*PieceJSON, 12)
	for y := 0; y <= 11; y++ {
		row := make([]*PieceJSON, 5)
		for x := 0; x <= 4; x++ {
			piece := gs.Board[y][x]
			if piece != nil {
				row[x] = &PieceJSON{
					Color: piece.Color.String(),
					Type:  pieceTypeNames[piece.Type],
					Name:  piece.Name(),
				}
			}
		}
		board[y] = row
	}
	return board
}

// String returns a text representation of the board (for debugging).
func (gs *GameState) String() string {
	s := "    "
	for x := 0; x <= 4; x++ {
		s += fmt.Sprintf("%-4s", colHeader(x))
	}
	s += "\n"
	for y := 11; y >= 0; y-- {
		s += fmt.Sprintf("%2d: ", y)
		for x := 0; x <= 4; x++ {
			p := gs.Board[y][x]
			if p == nil {
				s += "·    "
			} else {
				color := "R"
				if p.Color == Black {
					color = "B"
				}
				s += fmt.Sprintf("%s%-3s ", color, pieceTypeNames[p.Type][:3])
			}
		}
		s += "\n"
	}
	return s
}

func colHeader(x int) string {
	return fmt.Sprintf("%d", x)
}

// IsOnBoard reports whether (x, y) is a legal position.
func IsOnBoard(pos Position) bool {
	return pos.IsValid()
}

// HomeRow returns the home row (y) of a color (Red=0, Black=11).
func HomeRow(c Color) int {
	if c == Red {
		return 0
	}
	return 11
}

// FrontRow returns the front row (most-forward y) of a color.
func FrontRow(c Color) int {
	if c == Red {
		return 5
	}
	return 6
}

// InHomeArea reports whether the position is in the player's own home rows.
// For Red: y ∈ [0,5]. For Black: y ∈ [6,11].
func InHomeArea(pos Position, c Color) bool {
	if c == Red {
		return pos.Y >= 0 && pos.Y <= 5
	}
	return pos.Y >= 6 && pos.Y <= 11
}