// Package chess implements the International Chess (Western Chess) game engine
// following FIDE Laws of Chess 2023.
//
// Coordinate system:
//   - x: 0-7 (files, a..h mapped from White's perspective), y: 0-7 (ranks, 1..8)
//   - White side: y=0 (rank 1, near White player), Black side: y=7 (rank 8)
//   - The 8x8 board uses Board[y][x] addressing; nil = empty square.
package chess

import (
	"fmt"
	"strings"
)

// Color represents a side.
type Color int

const (
	White Color = 0
	Black Color = 1
)

// Opposite returns the other color.
func (c Color) Opposite() Color {
	if c == White {
		return Black
	}
	return White
}

// String returns "white" or "black".
func (c Color) String() string {
	if c == White {
		return "white"
	}
	return "black"
}

// PieceType enumerates piece kinds.
type PieceType int

const (
	King   PieceType = 1 // 王
	Queen  PieceType = 2 // 后
	Rook   PieceType = 3 // 车
	Bishop PieceType = 4 // 象
	Knight PieceType = 5 // 马
	Pawn   PieceType = 6 // 兵
)

// PieceName returns the English piece name (e.g., "King", "White King").
func (p Piece) Name() string {
	prefix := "White"
	if p.Color == Black {
		prefix = "Black"
	}
	return prefix + " " + pieceTypeName(p.Type)
}

func pieceTypeName(t PieceType) string {
	switch t {
	case King:
		return "King"
	case Queen:
		return "Queen"
	case Rook:
		return "Rook"
	case Bishop:
		return "Bishop"
	case Knight:
		return "Knight"
	case Pawn:
		return "Pawn"
	}
	return ""
}

// pieceTypeNames returns the lowercase type name (used in JSON).
var pieceTypeNames = map[PieceType]string{
	King: "king", Queen: "queen", Rook: "rook",
	Bishop: "bishop", Knight: "knight", Pawn: "pawn",
}

// Piece is a single piece on the board.
type Piece struct {
	Color Color
	Type  PieceType
}

// Position is a board square (file, rank).
type Position struct {
	X int `json:"x"` // 0-7 (a..h)
	Y int `json:"y"` // 0-7 (rank 1..8)
}

// IsValid reports whether the position is on the board.
func (p Position) IsValid() bool {
	return p.X >= 0 && p.X <= 7 && p.Y >= 0 && p.Y <= 7
}

// Algebraic returns the standard algebraic notation (e.g., "e4").
func (p Position) Algebraic() string {
	if !p.IsValid() {
		return ""
	}
	return fmt.Sprintf("%c%d", 'a'+p.X, p.Y+1)
}

// ParseAlgebraic parses an algebraic like "e4" into a Position.
func ParseAlgebraic(s string) (Position, bool) {
	if len(s) < 2 {
		return Position{}, false
	}
	s = strings.TrimSpace(s)
	x := int(s[0] - 'a')
	if x < 0 || x > 7 {
		return Position{}, false
	}
	// rank char: '1'..'8'
	var y int
	if s[1] >= '1' && s[1] <= '8' {
		y = int(s[1] - '1')
	} else {
		return Position{}, false
	}
	return Position{X: x, Y: y}, true
}

// Move records a single ply.
type Move struct {
	From            Position `json:"from"`
	To              Position `json:"to"`
	Piece           Piece    `json:"piece"`
	Captured        *Piece   `json:"captured,omitempty"`         // nil if no capture
	Promotion       PieceType `json:"promotion,omitempty"`        // non-zero when pawn promotes
	CastleKind      string   `json:"castle_kind,omitempty"`       // "king" or "queen", non-empty for castling
	EnPassant       bool     `json:"en_passant,omitempty"`        // true when this move captures en passant
}

// GameStatus represents the state of the game.
type GameStatus int

const (
	StatusPlaying  GameStatus = 0
	StatusWhiteWin GameStatus = 1
	StatusBlackWin GameStatus = 2
	StatusDraw     GameStatus = 3
)

// String returns a human-readable status.
func (s GameStatus) String() string {
	switch s {
	case StatusPlaying:
		return "playing"
	case StatusWhiteWin:
		return "white_win"
	case StatusBlackWin:
		return "black_win"
	case StatusDraw:
		return "draw"
	}
	return "unknown"
}

// ReasonEndGame describes how the game ended (sent to client).
type ReasonEndGame string

const (
	ReasonCheckmate ReasonEndGame = "checkmate"
	ReasonStalemate ReasonEndGame = "stalemate"
	ReasonFiftyMove ReasonEndGame = "fifty_move"
	ReasonInsufficient ReasonEndGame = "insufficient_material"
	ReasonThreefold ReasonEndGame = "threefold"
	ReasonResign    ReasonEndGame = "resign"
)

// MoveResult is the response after a successful move (returned by ChessManager).
// Defined in room.go.

// GameState holds the complete state of one game.
type GameState struct {
	Board       [8][8]*Piece // [rank][file], nil = empty
	Turn        Color
	MoveHistory []Move
	Status      GameStatus

	// Castling rights: KingSide/QueenSide for each side.
	CastlingRights CastlingRights

	// En-passant target square (the square a pawn skipped over last move), or invalid.
	EnPassantTarget Position

	// halfMoveClock counts plies since the last capture or pawn advance (for 50-move rule).
	halfMoveClock int

	// fullMoveNumber starts at 1, increments after Black's move.
	fullMoveNumber int

	// positionHistory records position keys (for threefold repetition).
	positionKeys []string

	// pendingPromotion indicates a pawn move that requires client to pick a piece.
	pendingPromotion *Move
}

func (gs *GameState) HalfMoveClock() int { return gs.halfMoveClock }
func (gs *GameState) FullMoveNumber() int { return gs.fullMoveNumber }

// CastlingRights tracks which rooks/kings have moved.
type CastlingRights struct {
	WhiteKingSide  bool // K
	WhiteQueenSide bool // Q
	BlackKingSide  bool // k
	BlackQueenSide bool // q
}

// ParseCastlingRights converts a FEN-style string ("KQkq" etc.) into the struct.
func ParseCastlingRights(s string) CastlingRights {
	cr := CastlingRights{}
	for _, r := range s {
		switch r {
		case 'K':
			cr.WhiteKingSide = true
		case 'Q':
			cr.WhiteQueenSide = true
		case 'k':
			cr.BlackKingSide = true
		case 'q':
			cr.BlackQueenSide = true
		}
	}
	return cr
}

// NewGame returns a fresh game with pieces in their starting positions.
func NewGame() *GameState {
	gs := &GameState{
		Turn:             White,
		Status:           StatusPlaying,
		CastlingRights:   CastlingRights{WhiteKingSide: true, WhiteQueenSide: true, BlackKingSide: true, BlackQueenSide: true},
		EnPassantTarget:  Position{X: -1, Y: -1},
		halfMoveClock:    0,
		fullMoveNumber:   1,
	}
	gs.setupInitialPosition()
	gs.recordPosition()
	return gs
}

// setupInitialPosition places all 32 pieces following the standard chess layout.
func (gs *GameState) setupInitialPosition() {
	// White back rank (y=0): R N B Q K B N R  (x=0..7)
	whiteBack := []PieceType{Rook, Knight, Bishop, Queen, King, Bishop, Knight, Rook}
	for x, t := range whiteBack {
		gs.Board[0][x] = &Piece{Color: White, Type: t}
	}
	// White pawns (y=1)
	for x := 0; x < 8; x++ {
		gs.Board[1][x] = &Piece{Color: White, Type: Pawn}
	}
	// Black back rank (y=7)
	blackBack := []PieceType{Rook, Knight, Bishop, Queen, King, Bishop, Knight, Rook}
	for x, t := range blackBack {
		gs.Board[7][x] = &Piece{Color: Black, Type: t}
	}
	// Black pawns (y=6)
	for x := 0; x < 8; x++ {
		gs.Board[6][x] = &Piece{Color: Black, Type: Pawn}
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
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			p := gs.Board[y][x]
			if p != nil && p.Color == c && p.Type == King {
				return Position{X: x, Y: y}, true
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

// BoardJSON returns the board as a JSON-serializable 2D array [rank][file].
func (gs *GameState) BoardJSON() [][]*PieceJSON {
	board := make([][]*PieceJSON, 8)
	for y := 0; y < 8; y++ {
		row := make([]*PieceJSON, 8)
		for x := 0; x < 8; x++ {
			p := gs.Board[y][x]
			if p != nil {
				row[x] = &PieceJSON{
					Color: p.Color.String(),
					Type:  pieceTypeNames[p.Type],
					Name:  pieceTypeName(p.Type),
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
	for y := 7; y >= 0; y-- {
		s += fmt.Sprintf("%d ", y+1)
		for x := 0; x < 8; x++ {
			p := gs.Board[y][x]
			if p == nil {
				s += " .  "
			} else {
				ch := "w"
				if p.Color == Black {
					ch = "b"
				}
				s += fmt.Sprintf("%s%-3s", strings.ToUpper(ch)[:1], pieceTypeName(p.Type)[:3])
			}
		}
		s += "\n"
	}
	s += "   a  b  c  d  e  f  g  h"
	return s
}
