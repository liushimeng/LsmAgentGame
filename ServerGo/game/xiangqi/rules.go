package xiangqi

import (
	"LsmAgentGame/errcode"
)

// ─────────────────── Public API ───────────────────

// ValidateMove checks whether a move is legal and returns an error if not.
// It does NOT mutate the game state — call ExecuteMove afterwards.
func (gs *GameState) ValidateMove(from, to Position, player Color) *errcode.Error {
	if gs.Status != StatusPlaying {
		return errcode.Code(errcode.ErrGameAlreadyOver)
	}
	if player != gs.Turn {
		return errcode.Code(errcode.ErrNotYourTurn)
	}
	if !from.IsValid() || !to.IsValid() {
		return errcode.Code(errcode.ErrInvalidMove)
	}
	piece := gs.Get(from)
	if piece == nil || piece.Color != player {
		return errcode.Code(errcode.ErrInvalidMove)
	}
	target := gs.Get(to)
	if target != nil && target.Color == player {
		return errcode.Code(errcode.ErrInvalidMove) // can't capture own piece
	}
	if from == to {
		return errcode.Code(errcode.ErrInvalidMove)
	}
	if !isValidPieceMove(gs, from, to, piece) {
		return errcode.Code(errcode.ErrInvalidMove)
	}
	// Simulate the move and check if it leaves own king in check.
	if gs.wouldBeInCheck(from, to, player) {
		return errcode.Code(errcode.ErrInvalidMove)
	}
	return nil
}

// ExecuteMove applies a move to the game state. Assumes ValidateMove already passed.
// Returns the move record (with captured piece) and whether the game ended.
func (gs *GameState) ExecuteMove(from, to Position) Move {
	piece := gs.Get(from)
	captured := gs.Get(to)

	m := Move{From: from, To: to, Piece: *piece, Captured: captured}
	gs.set(to, piece)
	gs.set(from, nil)
	gs.MoveHistory = append(gs.MoveHistory, m)

	// 60-move rule counter.
	if captured != nil || piece.Type == Soldier {
		gs.moveCounter = 0
	} else {
		gs.moveCounter++
	}

	// Switch turn.
	gs.Turn = gs.Turn.Opposite()

	// Check game-ending conditions for the side that just moved-into-turn.
	gs.updateStatus()
	return m
}

// GenerateLegalMoves returns all legal moves for the given color.
func (gs *GameState) GenerateLegalMoves(c Color) []Move {
	var moves []Move
	for y := 0; y <= 9; y++ {
		for x := 0; x <= 8; x++ {
			p := gs.Board[y][x]
			if p == nil || p.Color != c {
				continue
			}
			from := Position{x, y}
			targets := gs.pseudoLegalTargets(from, p)
			for _, to := range targets {
				if gs.ValidateMove(from, to, c) == nil {
					captured := gs.Get(to)
					moves = append(moves, Move{From: from, To: to, Piece: *p, Captured: captured})
				}
			}
		}
	}
	return moves
}

// IsInCheck reports whether the given color's king is under attack.
func (gs *GameState) IsInCheck(c Color) bool {
	kingPos, ok := gs.FindKing(c)
	if !ok {
		return true // king missing = in check
	}
	return gs.isAttackedBy(kingPos, c.Opposite())
}

// ─────────────────── Internal helpers ───────────────────

// isValidPieceMove dispatches to per-piece movement rules.
func isValidPieceMove(gs *GameState, from, to Position, piece *Piece) bool {
	dx := to.X - from.X
	dy := to.Y - from.Y

	switch piece.Type {
	case King:
		return isValidKingMove(from, to, dx, dy, piece.Color)
	case Advisor:
		return isValidAdvisorMove(from, to, dx, dy, piece.Color)
	case Elephant:
		return isValidElephantMove(gs, from, to, dx, dy, piece.Color)
	case Horse:
		return isValidHorseMove(gs, from, to, dx, dy)
	case Chariot:
		return isValidChariotMove(gs, from, to)
	case Cannon:
		return isValidCannonMove(gs, from, to, gs.Get(to) != nil)
	case Soldier:
		return isValidSoldierMove(from, to, dx, dy, piece.Color)
	}
	return false
}

// ── King (帅/将) ──
func isValidKingMove(from, to Position, dx, dy int, c Color) bool {
	// Must stay in palace.
	if !inPalace(to, c) {
		return false
	}
	// Move one step orthogonally.
	absdx, absdy := abs(dx), abs(dy)
	return (absdx == 1 && dy == 0) || (dx == 0 && absdy == 1)
}

// ── Advisor (仕/士) ──
func isValidAdvisorMove(from, to Position, dx, dy int, c Color) bool {
	if !inPalace(to, c) {
		return false
	}
	return abs(dx) == 1 && abs(dy) == 1
}

// ── Elephant (相/象) ──
func isValidElephantMove(gs *GameState, from, to Position, dx, dy int, c Color) bool {
	// Must not cross river.
	if c == Red && to.Y > 4 {
		return false
	}
	if c == Black && to.Y < 5 {
		return false
	}
	// Move exactly 2 steps diagonally ("田" shape).
	if abs(dx) != 2 || abs(dy) != 2 {
		return false
	}
	// Blocking eye: the center of the "田" must be empty.
	ex, ey := from.X+dx/2, from.Y+dy/2
	return gs.Board[ey][ex] == nil
}

// ── Horse (马) ──
func isValidHorseMove(gs *GameState, from, to Position, dx, dy int) bool {
	absdx, absdy := abs(dx), abs(dy)
	// "日" shape: one orthogonal + one diagonal = (1,2) or (2,1).
	if !((absdx == 1 && absdy == 2) || (absdx == 2 && absdy == 1)) {
		return false
	}
	// Blocking leg: the first orthogonal step must be clear.
	var lx, ly int
	if absdx == 2 {
		lx = from.X + dx/2
		ly = from.Y
	} else {
		lx = from.X
		ly = from.Y + dy/2
	}
	return gs.Board[ly][lx] == nil
}

// ── Chariot (车) ──
func isValidChariotMove(gs *GameState, from, to Position) bool {
	if from.X != to.X && from.Y != to.Y {
		return false // must be orthogonal
	}
	return gs.countBetween(from, to) == 0
}

// ── Cannon (炮) ──
func isValidCannonMove(gs *GameState, from, to Position, isCapture bool) bool {
	if from.X != to.X && from.Y != to.Y {
		return false
	}
	between := gs.countBetween(from, to)
	if isCapture {
		return between == 1 // must jump over exactly one piece
	}
	return between == 0 // no capture: must be clear path
}

// ── Soldier (兵/卒) ──
func isValidSoldierMove(from, to Position, dx, dy int, c Color) bool {
	absdx, absdy := abs(dx), abs(dy)
	if absdx+absdy != 1 {
		return false // exactly one step
	}
	if c == Red {
		if from.Y <= 4 {
			// Not crossed river: can only go forward (y+).
			return dy == 1 && dx == 0
		}
		// Crossed river: can go forward or sideways, never backward.
		return dy >= 0
	}
	// Black
	if from.Y >= 5 {
		// Not crossed river: can only go forward (y-).
		return dy == -1 && dx == 0
	}
	// Crossed river: can go forward or sideways, never backward.
	return dy <= 0
}

// ─────────────────── Attack / Check utilities ───────────────────

// isAttackedBy reports whether any piece of `attacker` color attacks `pos`.
func (gs *GameState) isAttackedBy(pos Position, attacker Color) bool {
	for y := 0; y <= 9; y++ {
		for x := 0; x <= 8; x++ {
			p := gs.Board[y][x]
			if p == nil || p.Color != attacker {
				continue
			}
			from := Position{x, y}
			if gs.canAttack(from, pos, p) {
				return true
			}
		}
	}
	return false
}

// canAttack checks if a piece at `from` can attack `to` (ignoring self-check).
func (gs *GameState) canAttack(from, to Position, piece *Piece) bool {
	dx := to.X - from.X
	dy := to.Y - from.Y

	switch piece.Type {
	case King:
		absdx, absdy := abs(dx), abs(dy)
		return (absdx == 1 && dy == 0) || (dx == 0 && absdy == 1)
	case Advisor:
		return abs(dx) == 1 && abs(dy) == 1
	case Elephant:
		if abs(dx) != 2 || abs(dy) != 2 {
			return false
		}
		ex, ey := from.X+dx/2, from.Y+dy/2
		return gs.Board[ey][ex] == nil
	case Horse:
		absdx, absdy := abs(dx), abs(dy)
		if !((absdx == 1 && absdy == 2) || (absdx == 2 && absdy == 1)) {
			return false
		}
		var lx, ly int
		if absdx == 2 {
			lx = from.X + dx/2
			ly = from.Y
		} else {
			lx = from.X
			ly = from.Y + dy/2
		}
		return gs.Board[ly][lx] == nil
	case Chariot:
		if from.X != to.X && from.Y != to.Y {
			return false
		}
		return gs.countBetween(from, to) == 0
	case Cannon:
		if from.X != to.X && from.Y != to.Y {
			return false
		}
		return gs.countBetween(from, to) == 1
	case Soldier:
		absdx, absdy := abs(dx), abs(dy)
		if absdx+absdy != 1 {
			return false
		}
		if piece.Color == Red {
			if from.Y <= 4 {
				return dy == 1 && dx == 0
			}
			return dy >= 0
		}
		if from.Y >= 5 {
			return dy == -1 && dx == 0
		}
		return dy <= 0
	}
	return false
}

// wouldBeInCheck simulates a move and reports whether the moving side's king is in check.
func (gs *GameState) wouldBeInCheck(from, to Position, c Color) bool {
	// Temporarily apply the move.
	moved := gs.Get(from)
	captured := gs.Get(to)
	gs.set(to, moved)
	gs.set(from, nil)

	inCheck := gs.IsInCheck(c)

	// Undo.
	gs.set(from, moved)
	gs.set(to, captured)
	return inCheck
}

// kingsAreFacing reports whether the two kings are on the same file with nothing
// between them (the "将帅对面" rule).
func (gs *GameState) kingsAreFacing() bool {
	rk, rok := gs.FindKing(Red)
	bk, bok := gs.FindKing(Black)
	if !rok || !bok || rk.X != bk.X {
		return false
	}
	for y := rk.Y + 1; y < bk.Y; y++ {
		if gs.Board[y][rk.X] != nil {
			return false
		}
	}
	return true
}

// updateStatus checks end-of-game conditions after a move.
func (gs *GameState) updateStatus() {
	// The side to move now is gs.Turn.
	next := gs.Turn
	opp := next.Opposite()

	// 60-move rule.
	if gs.moveCounter >= 120 { // 60 full moves = 120 half-moves
		gs.Status = StatusDraw
		return
	}

	// Generate legal moves for the side to move.
	legal := gs.GenerateLegalMoves(next)
	inCheck := gs.IsInCheck(next)

	if len(legal) == 0 {
		if inCheck {
			// Checkmate: the side that just moved wins.
			if opp == Red {
				gs.Status = StatusRedWin
			} else {
				gs.Status = StatusBlackWin
			}
		} else {
			// Stalemate: the side that just moved wins (in Chinese Chess, stalemate = loss).
			if opp == Red {
				gs.Status = StatusRedWin
			} else {
				gs.Status = StatusBlackWin
			}
		}
		return
	}

	// "将帅对面" — if the just-moved side caused kings to face, they lose.
	if gs.kingsAreFacing() {
		if opp == Red {
			gs.Status = StatusRedWin
		} else {
			gs.Status = StatusBlackWin
		}
		return
	}
}

// pseudoLegalTargets returns all positions the piece at `from` could move to
// ignoring self-check constraints.
func (gs *GameState) pseudoLegalTargets(from Position, piece *Piece) []Position {
	var targets []Position
	for y := 0; y <= 9; y++ {
		for x := 0; x <= 8; x++ {
			to := Position{x, y}
			if from == to {
				continue
			}
			t := gs.Get(to)
			if t != nil && t.Color == piece.Color {
				continue
			}
			dx := to.X - from.X
			dy := to.Y - from.Y
			if isValidPieceMove(gs, from, to, piece) {
				targets = append(targets, to)
			}
			_ = dx
			_ = dy
		}
	}
	return targets
}

// ─────────────────── Geometry helpers ───────────────────

// inPalace reports whether pos is inside the palace for the given color.
func inPalace(pos Position, c Color) bool {
	if pos.X < 3 || pos.X > 5 {
		return false
	}
	if c == Red {
		return pos.Y >= 0 && pos.Y <= 2
	}
	return pos.Y >= 7 && pos.Y <= 9
}

// countBetween counts the number of pieces strictly between two positions on the same row or column.
func (gs *GameState) countBetween(a, b Position) int {
	count := 0
	if a.X == b.X {
		lo, hi := min(a.Y, b.Y), max(a.Y, b.Y)
		for y := lo + 1; y < hi; y++ {
			if gs.Board[y][a.X] != nil {
				count++
			}
		}
	} else if a.Y == b.Y {
		lo, hi := min(a.X, b.X), max(a.X, b.X)
		for x := lo + 1; x < hi; x++ {
			if gs.Board[a.Y][x] != nil {
				count++
			}
		}
	}
	return count
}

func abs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
