// Package xiangqi implements the Chinese Chess (Xiangqi) game engine.
//
// Coordinate system:
//   - x: 0-8 (columns), y: 0-9 (rows)
//   - Red side: y=0 (bottom), Black side: y=9 (top)
//   - Red palace: x∈[3,5], y∈[0,2]
//   - Black palace: x∈[3,5], y∈[7,9]
//   - Red half: y∈[0,4], Black half: y∈[5,9]
package xiangqi

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

// PieceType enumerates piece kinds.
type PieceType int

const (
	King    PieceType = 1 // 帅/将
	Advisor PieceType = 2 // 仕/士
	Elephant PieceType = 3 // 相/象
	Horse   PieceType = 4 // 马
	Chariot PieceType = 5 // 车
	Cannon  PieceType = 6 // 炮
	Soldier PieceType = 7 // 兵/卒
)

// Piece names for each type and color.
var pieceNames = map[Color]map[PieceType]string{
	Red: {
		King: "帅", Advisor: "仕", Elephant: "相",
		Horse: "马", Chariot: "车", Cannon: "炮", Soldier: "兵",
	},
	Black: {
		King: "将", Advisor: "士", Elephant: "象",
		Horse: "马", Chariot: "车", Cannon: "炮", Soldier: "卒",
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
	X int `json:"x"` // 0-8 column
	Y int `json:"y"` // 0-9 row
}

// IsValid reports whether the position is on the board.
func (p Position) IsValid() bool {
	return p.X >= 0 && p.X <= 8 && p.Y >= 0 && p.Y <= 9
}

// Move records a single ply.
type Move struct {
	From    Position `json:"from"`
	To      Position `json:"to"`
	Piece   Piece    `json:"piece"`
	Captured *Piece  `json:"captured,omitempty"` // nil if no capture
}

// GameStatus represents the state of the game.
type GameStatus int

const (
	StatusPlaying   GameStatus = 0
	StatusRedWin    GameStatus = 1
	StatusBlackWin  GameStatus = 2
	StatusDraw      GameStatus = 3
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

// GameState holds the complete state of one game.
type GameState struct {
	Board       [10][9]*Piece // [row][col], nil = empty
	Turn        Color
	MoveHistory []Move
	Status      GameStatus
	// moveCounter counts consecutive non-capture, non-soldier moves for the 60-move rule.
	moveCounter int
}

// NewGame returns a fresh game with pieces in their initial positions.
func NewGame() *GameState {
	gs := &GameState{
		Turn:   Red,
		Status: StatusPlaying,
	}
	gs.setupInitialPosition()
	return gs
}

// setupInitialPosition places all 32 pieces.
func (gs *GameState) setupInitialPosition() {
	// Red back rank (y=0): 车马相仕帅仕相马车
	redBack := []PieceType{Chariot, Horse, Elephant, Advisor, King, Advisor, Elephant, Horse, Chariot}
	for x, t := range redBack {
		gs.Board[0][x] = &Piece{Color: Red, Type: t}
	}
	// Black back rank (y=9)
	blackBack := []PieceType{Chariot, Horse, Elephant, Advisor, King, Advisor, Elephant, Horse, Chariot}
	for x, t := range blackBack {
		gs.Board[9][x] = &Piece{Color: Black, Type: t}
	}
	// Red cannons (y=2, x=1,7)
	gs.Board[2][1] = &Piece{Color: Red, Type: Cannon}
	gs.Board[2][7] = &Piece{Color: Red, Type: Cannon}
	// Black cannons (y=7, x=1,7)
	gs.Board[7][1] = &Piece{Color: Black, Type: Cannon}
	gs.Board[7][7] = &Piece{Color: Black, Type: Cannon}
	// Red soldiers (y=3, x=0,2,4,6,8)
	for _, x := range []int{0, 2, 4, 6, 8} {
		gs.Board[3][x] = &Piece{Color: Red, Type: Soldier}
	}
	// Black soldiers (y=6, x=0,2,4,6,8)
	for _, x := range []int{0, 2, 4, 6, 8} {
		gs.Board[6][x] = &Piece{Color: Black, Type: Soldier}
	}
}

// Get returns the piece at pos, or nil.
func (gs *GameState) Get(pos Position) *Piece {
	if !pos.IsValid() {
		return nil
	}
	return gs.Board[pos.Y][pos.X]
}

// set places a piece (may be nil) at a position.
func (gs *GameState) set(pos Position, p *Piece) {
	if pos.IsValid() {
		gs.Board[pos.Y][pos.X] = p
	}
}

// FindKing returns the position of the given color's king, or false if not found.
func (gs *GameState) FindKing(c Color) (Position, bool) {
	for y := 0; y <= 9; y++ {
		for x := 0; x <= 8; x++ {
			p := gs.Board[y][x]
			if p != nil && p.Color == c && p.Type == King {
				return Position{x, y}, true
			}
		}
	}
	return Position{}, false
}

// PieceJSON is a JSON-friendly representation of a piece.
type PieceJSON struct {
	Color string `json:"color"`
	Type  string `json:"type"`
	Name  string `json:"name"`
}

var pieceTypeNames = map[PieceType]string{
	King: "king", Advisor: "advisor", Elephant: "elephant",
	Horse: "horse", Chariot: "chariot", Cannon: "cannon", Soldier: "soldier",
}

// BoardJSON returns the board as a JSON-serializable 2D array.
func (gs *GameState) BoardJSON() [][]*PieceJSON {
	board := make([][]*PieceJSON, 10)
	for y := 0; y <= 9; y++ {
		row := make([]*PieceJSON, 9)
		for x := 0; x <= 8; x++ {
			p := gs.Board[y][x]
			if p != nil {
				color := "red"
				if p.Color == Black {
					color = "black"
				}
				row[x] = &PieceJSON{
					Color: color,
					Type:  pieceTypeNames[p.Type],
					Name:  p.Name(),
				}
			}
		}
		board[y] = row
	}
	return board
}

// String returns a text representation of the board (for debugging).
func (gs *GameState) String() string {
	s := ""
	for y := 9; y >= 0; y-- {
		s += fmt.Sprintf("%d ", y)
		for x := 0; x <= 8; x++ {
			p := gs.Board[y][x]
			if p == nil {
				s += "·  "
			} else {
				s += fmt.Sprintf("%-3s", p.Name())
			}
		}
		s += "\n"
	}
	s += "  "
	for x := 0; x <= 8; x++ {
		s += fmt.Sprintf("%-3d", x)
	}
	return s
}
