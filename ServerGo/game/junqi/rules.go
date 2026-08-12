package junqi

import (
	"LsmAgentGame/errcode"
)

// Direction offsets for adjacent (公路线) cells.
var roadDirs = []Position{
	{X: 0, Y: 1},  // up (toward larger y)
	{X: 0, Y: -1}, // down
	{X: 1, Y: 0},  // right
	{X: -1, Y: 0}, // left
}

// ValidateMove checks whether the given move is legal in the current state.
//
// Move rules (中国军棋规则.md §五):
//   - The piece at `from` must belong to `color`
//   - It must be `color`'s turn
//   - Flag, Mine, and any piece inside an HQ cannot move
//   - Pieces in camps can only move one step
//   - Road moves: any direction, exactly one step
//   - Rail moves (no blocking piece on the path):
//       * 普通子: any number of steps straight OR along a curved rail; no right-angle turns
//       * 工兵 (Engineer): can turn freely on rails ("飞铁路")
//   - Mountain (row 5 ↔ row 6): only rail cells may cross the gap
func ValidateMove(gs *GameState, from, to Position, color Color) *errcode.Error {
	if gs.Phase != PhasePlaying {
		return errcode.CodeMsg(errcode.ErrGameNotStarted, "game is not in the playing phase")
	}
	if gs.Status != StatusPlaying {
		return errcode.CodeMsg(errcode.ErrGameAlreadyOver, "game is already over")
	}
	if gs.Turn != color {
		return errcode.CodeMsg(errcode.ErrNotYourTurn, "not your turn")
	}
	if !from.IsValid() || !to.IsValid() {
		return errcode.CodeMsg(errcode.ErrInvalidMove, "position out of board")
	}
	if from == to {
		return errcode.CodeMsg(errcode.ErrInvalidMove, "from and to are the same")
	}
	src := gs.Get(from)
	if src == nil {
		return errcode.CodeMsg(errcode.ErrInvalidMove, "no piece at source")
	}
	if src.Color != color {
		return errcode.CodeMsg(errcode.ErrInvalidMove, "source piece is not yours")
	}

	// Immobile pieces.
	if src.Type == Flag || src.Type == Mine {
		return errcode.CodeMsg(errcode.ErrInvalidMove, "this piece cannot move")
	}
	// Pieces in an HQ cannot move.
	if IsHQ(from) {
		return errcode.CodeMsg(errcode.ErrInvalidMove, "pieces in headquarters cannot move")
	}
	// Cannot land on your own piece.
	if dst := gs.Get(to); dst != nil && dst.Color == color {
		return errcode.CodeMsg(errcode.ErrInvalidMove, "cannot capture your own piece")
	}
	// Cannot enter the enemy's HQ. (Defenders inside HQ are still attackable on
	// their cell, but you can't *enter* their HQ to occupy it — your piece just
	// captures them in place.)
	for _, enemyHQ := range HQsForColor(color.Opposite()) {
		if to == enemyHQ {
			return errcode.CodeMsg(errcode.ErrInvalidMove, "cannot enter the enemy's headquarters")
		}
	}

	// Camps: pieces inside a camp can only move one step in any legal direction.
	if IsCamp(from) {
		if !isOneStep(from, to) {
			return errcode.CodeMsg(errcode.ErrInvalidMove, "camp pieces can only move one step")
		}
		// The one-step must obey road/rail/mountain rules below.
	}

	// Try rail movement first.
	if IsRail(from) && IsRail(to) {
		// Engineer can fly; other pieces must travel in a straight or arc line with no turns (except engineer).
		if src.Type == Engineer {
			if canEngineerReachRail(gs, from, to) {
				return nil
			}
			return errcode.CodeMsg(errcode.ErrInvalidMove, "engineer rail path blocked")
		}
		// Regular piece: straight line or curved rail, no right-angle turns.
		if canStraightOrArcRail(gs, from, to) {
			return nil
		}
		// Also allow one-step road-like adjacent moves when both cells are rail (rare edge case).
		if isOneStep(from, to) {
			return nil
		}
		return errcode.CodeMsg(errcode.ErrInvalidMove, "invalid rail move")
	}

	// Road / camp ↔ road / rail movement: must be one step in a cardinal direction,
	// and we must respect the mountain border.
	if !isOneStep(from, to) {
		return errcode.CodeMsg(errcode.ErrInvalidMove, "road moves must be exactly one step")
	}
	// Mountain border crossing: only rail cells can cross from row 5 to row 6.
	if (from.Y == 5 && to.Y == 6) || (from.Y == 6 && to.Y == 5) {
		if from.X != to.X {
			return errcode.CodeMsg(errcode.ErrInvalidMove, "must cross mountain on same column")
		}
		if !IsRail(from) || !IsRail(to) {
			return errcode.CodeMsg(errcode.ErrInvalidMove, "only rail pieces can cross the mountain border")
		}
	}
	return nil
}

// isOneStep reports whether moving from → to is a single cardinal step.
func isOneStep(from, to Position) bool {
	dx := abs(to.X - from.X)
	dy := abs(to.Y - from.Y)
	return (dx+dy == 1)
}

// canStraightOrArcRail checks if a regular (non-engineer) piece can move from → to
// along rails. Allowed: any straight line, or a single arc turn following the
// rail corners, no right-angle turns.
//
// In our 12×5 topology, rail cells are at:
//   - All of (any x, y=1/3/5) and (any x, y=6/8/10)
//   - With diagonal "arc" connections at the row-2/row-4 corners (modeled
//     implicitly via adjacent step moves).
//
// We simplify by allowing:
//   - Same row, |dx| >= 1, path clear  → straight rail
//   - Same column, |dy| >= 1, path clear → straight rail
//   - One corner: from (x1, y1) to (x2, y2) via the intermediate (x2, y1) or
//     (x1, y2), provided the corner and both segments are on rails and clear
//     of blocking pieces. No more than one corner allowed (no right-angle
//     turns means at most one 90° bend).
func canStraightOrArcRail(gs *GameState, from, to Position) bool {
	if from.X == to.X {
		return railColumnPathClear(gs, from, to)
	}
	if from.Y == to.Y {
		return railRowPathClear(gs, from, to)
	}
	// Try one-corner paths.
	corner1 := Position{X: to.X, Y: from.Y}
	corner2 := Position{X: from.X, Y: to.Y}
	if IsRail(corner1) && railRowPathClear(gs, from, corner1) && railColumnPathClear(gs, corner1, to) {
		gs_ := gs // no-op, just to avoid unused warning if needed
		_ = gs_
		return true
	}
	if IsRail(corner2) && railColumnPathClear(gs, from, corner2) && railRowPathClear(gs, corner2, to) {
		return true
	}
	return false
}

// railRowPathClear reports whether the cells between from and to (same row)
// are all rail and unblocked (except the destination itself, which may hold
// an enemy piece). `from` is allowed to be the source (no blockage check).
func railRowPathClear(gs *GameState, from, to Position) bool {
	if from.Y != to.Y {
		return false
	}
	step := 1
	if to.X < from.X {
		step = -1
	}
	for x := from.X + step; x != to.X+step; x += step {
		pos := Position{X: x, Y: from.Y}
		if !IsRail(pos) {
			return false
		}
		// The destination itself may be an enemy piece (the capture target).
		if pos == to {
			continue
		}
		if gs.Get(pos) != nil {
			return false // blocked
		}
	}
	return true
}

// railColumnPathClear is the same as railRowPathClear but vertical.
func railColumnPathClear(gs *GameState, from, to Position) bool {
	if from.X != to.X {
		return false
	}
	step := 1
	if to.Y < from.Y {
		step = -1
	}
	for y := from.Y + step; y != to.Y+step; y += step {
		pos := Position{X: from.X, Y: y}
		if !IsRail(pos) {
			return false
		}
		if pos == to {
			continue
		}
		if gs.Get(pos) != nil {
			return false
		}
	}
	return true
}

// canEngineerReachRail reports whether an Engineer (工兵) can reach `to`
// from `from` via rails with free turning (the famous "工兵飞铁路").
// We use BFS over rail cells.
func canEngineerReachRail(gs *GameState, from, to Position) bool {
	if from == to {
		return true
	}
	if !IsRail(from) || !IsRail(to) {
		return false
	}
	visited := map[Position]bool{from: true}
	queue := []Position{from}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		// 4 cardinal neighbors (engineer can turn freely on rails).
		for _, d := range roadDirs {
			np := Position{X: cur.X + d.X, Y: cur.Y + d.Y}
			if !np.IsValid() || visited[np] {
				continue
			}
			if !IsRail(np) {
				continue
			}
			if gs.Get(np) != nil && np != to {
				continue // blocked by a piece (destination may be enemy)
			}
			if np == to {
				return true
			}
			visited[np] = true
			queue = append(queue, np)
		}
	}
	return false
}

// ─────────────────── Battle resolution ───────────────────

// resolveBattle decides the outcome of an attacker moving onto a defender's cell.
//
// Rules (中国军棋规则.md §六):
//   - Bomb vs anything: both destroyed
//   - Anything (except Engineer) vs Mine: attacker destroyed, mine survives
//   - Engineer vs Mine: mine destroyed, engineer survives ("排雷")
//   - Same rank: both destroyed
//   - Higher rank vs lower rank: higher wins, lower destroyed
//   - Lower rank vs higher rank: lower destroyed
//
// Returns (attackerSurvives, defenderSurvives, bothDestroyed, engineerDefused).
// "engineerDefused" is true when an engineer successfully removed a mine.
func resolveBattle(attacker, defender *Piece) (aSurvives, dSurvives, bothDestroyed, engineerDefused bool) {
	// Bomb mutual destruction
	if attacker.Type == Bomb {
		return false, false, true, false
	}
	if defender.Type == Bomb {
		return false, false, true, false
	}
	// Mine rules
	if defender.Type == Mine {
		if attacker.Type == Engineer {
			return true, false, false, true // engineer defuses
		}
		return false, true, false, false // attacker dies, mine survives
	}
	// Flag cannot attack (it can't move). Defensive capture of flag = attacker wins.
	if defender.Type == Flag {
		return true, false, false, false
	}
	// Rank comparison
	if !attacker.Type.HasRank() || !defender.Type.HasRank() {
		// Defensive fallback — should not reach here since Flag/Bomb/Mine handled above.
		return false, false, true, false
	}
	if attacker.Type.Rank() == defender.Type.Rank() {
		return false, false, true, false
	}
	if attacker.Type.Rank() > defender.Type.Rank() {
		return true, false, false, false
	}
	return false, true, false, false
}

// ─────────────────── Move execution ───────────────────

// ExecuteMove applies a validated move to the game state, returning the move
// record (including capture info). It updates Turn, history, status, and the
// flag-reveal state for暗棋 mode.
func (gs *GameState) ExecuteMove(from, to Position) Move {
	src := *gs.Get(from)
	dst := gs.Get(to)

	move := Move{From: from, To: to, Piece: src}

	aSurvives, dSurvives, bothDestroyed, engineerDefused := resolveBattle(&src, dst)

	// Update the board.
	if aSurvives {
		gs.set(to, &src)
	} else {
		gs.set(to, nil)
	}
	gs.set(from, nil)

	// Record battle outcome flags.
	move.BothDestroyed = bothDestroyed
	move.EngineerDefused = engineerDefused
	if dst != nil {
		if !dSurvives {
			captured := *dst
			move.Captured = &captured
		}
		// For暗棋: when any piece is captured/revealed, mark it as known to the opponent.
		move.RevealedPiece = dst // we send dst even if destroyed, so client can display
	}

	// NoCapture counter — only reset on actual captures (mine defusal counts as no capture).
	if dst != nil && engineerDefused {
		gs.NoCaptureCount++
	} else if dst != nil {
		gs.NoCaptureCount = 0
	} else {
		gs.NoCaptureCount++
	}

	// Switch turn.
	gs.Turn = gs.Turn.Opposite()

	// Check game over.
	gs.checkGameOver(&move)

	gs.MoveHistory = append(gs.MoveHistory, move)
	return move
}

// checkGameOver evaluates win/draw conditions after a move:
//   - One side's Flag was captured/destroyed → other side wins
//   - Commander death (in dark mode) → reveal that side's flag position
//   - NoCaptureCount >= 70 → draw
//   - One side has no movable pieces → other side wins (困毙)
func (gs *GameState) checkGameOver(lastMove *Move) {
	// Flag capture → win.
	if lastMove.Captured != nil && lastMove.Captured.Type == Flag {
		// The attacker wins, which is the opposite color of the flag's owner.
		if lastMove.Captured.Color == Red {
			gs.Status = StatusBlackWin
		} else {
			gs.Status = StatusRedWin
		}
		gs.Phase = PhaseOver
		return
	}
	// 70-move no-capture draw.
	if gs.NoCaptureCount >= 70 {
		gs.Status = StatusDraw
		gs.Phase = PhaseOver
		return
	}
	// Commander death → reveal flag.
	if lastMove.Captured != nil && lastMove.Captured.Type == Commander {
		// The captured commander belonged to `lastMove.Captured.Color`. That side's flag
		// is now publicly known (we'll surface this via FlagRevealed + visibility layer).
		gs.FlagRevealed[colorIndex(lastMove.Captured.Color)] = true
	}
	// 困毙: after this move, if the side-to-move has no movable pieces, opponent wins.
	other := gs.Turn
	if !hasAnyMovablePiece(gs, other) {
		if other == Red {
			gs.Status = StatusBlackWin
		} else {
			gs.Status = StatusRedWin
		}
		gs.Phase = PhaseOver
	}
}

// hasAnyMovablePiece reports whether the given color has at least one piece
// that can legally move. Used for 困毙 detection.
func hasAnyMovablePiece(gs *GameState, c Color) bool {
	for y := 0; y <= 11; y++ {
		for x := 0; x <= 4; x++ {
			p := gs.Board[y][x]
			if p == nil || p.Color != c {
				continue
			}
			if p.Type == Flag || p.Type == Mine {
				continue
			}
			if IsHQ(Position{X: x, Y: y}) {
				continue
			}
			// Found at least one movable piece.
			return true
		}
	}
	return false
}