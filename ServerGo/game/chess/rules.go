package chess

import (
	"LsmAgentGame/errcode"
)

// ─────────────────── Public API ───────────────────

// plan encodes the parsed behavior of a validated move so ExecuteMove can apply it.
// Computed inside ValidateMove, consumed by ExecuteMove (no global mutable state).
type plan struct {
	isCastle       string // "" | "king" | "queen"
	isEnPassant    bool
	promotionPiece PieceType
}

// ValidateMove checks whether a move is legal and returns an error if not.
// It does NOT mutate the game state — call ExecuteMove afterwards.
//
// Special-case: when a pawn reaches the back rank but `promotion` is zero,
// ValidateMove returns ErrInvalidMove with the message "choose a promotion piece".
// The pawn move is recorded on `gs.pendingPromotion`; the WS layer then calls
// PromoteTo (or a direct ExecuteMove with the chosen promotion piece).
func (gs *GameState) ValidateMove(from, to Position, player Color, promotion PieceType) (*plan, *errcode.Error) {
	if gs.Status != StatusPlaying {
		return nil, errcode.Code(errcode.ErrGameAlreadyOver)
	}
	if player != gs.Turn {
		return nil, errcode.Code(errcode.ErrNotYourTurn)
	}
	if !from.IsValid() || !to.IsValid() {
		return nil, errcode.Code(errcode.ErrInvalidMove)
	}
	piece := gs.Get(from)
	if piece == nil || piece.Color != player {
		return nil, errcode.Code(errcode.ErrInvalidMove)
	}
	target := gs.Get(to)
	if target != nil && target.Color == player {
		return nil, errcode.Code(errcode.ErrInvalidMove) // can't capture own piece
	}

	p := &plan{}

	// Special handling for castling king moves (king moves 2 squares).
	if piece.Type == King {
		dx := to.X - from.X
		dy := to.Y - from.Y
		if dy == 0 && abs(dx) == 2 {
			if !gs.canCastle(player, dx > 0) {
				return nil, errcode.Code(errcode.ErrInvalidMove)
			}
			if dx > 0 {
				p.isCastle = "king"
			} else {
				p.isCastle = "queen"
			}
		} else if !isValidKingMove(dx, dy) {
			return nil, errcode.Code(errcode.ErrInvalidMove)
		}
	} else if !isValidPieceMove(gs, from, to, piece) {
		return nil, errcode.Code(errcode.ErrInvalidMove)
	}

	// Pawn promotion: require a valid promotion piece when pawn reaches back rank.
	promoPiece := promotion
	if piece.Type == Pawn && isPromotionRank(to, player) {
		if !isValidPromotionPiece(promoPiece) {
			// Record the half-completed move; client should send the promotion choice.
			gs.pendingPromotion = &Move{
				From:  from,
				To:    to,
				Piece: *piece,
			}
			return nil, errcode.CodeMsg(errcode.ErrInvalidMove, "choose a promotion piece (queen/rook/bishop/knight)")
		}
		p.promotionPiece = promoPiece
	}

	// En-passant detection (the capture target is the en-passant square).
	if piece.Type == Pawn && target == nil && abs(to.X-from.X) == 1 && to.Y != from.Y {
		if to == gs.EnPassantTarget {
			p.isEnPassant = true
		} else {
			return nil, errcode.Code(errcode.ErrInvalidMove)
		}
	}

	// Simulate the move and verify the moving side's king is not left in check.
	if gs.wouldBeInCheck(from, to, player, p.isEnPassant) {
		return nil, errcode.Code(errcode.ErrInvalidMove)
	}
	return p, nil
}

// PendingPromotion returns (and consumes) the pending pawn-promotion move if any.
func (gs *GameState) PendingPromotion() *Move {
	m := gs.pendingPromotion
	gs.pendingPromotion = nil
	return m
}

// SetPendingPromotion is used by the WS layer when a client sends a promotion
// choice to retry the move with a real promotion piece.
func (gs *GameState) SetPendingPromotion(m *Move) {
	gs.pendingPromotion = m
}

// ExecuteMove applies a move to the game state. Assumes ValidateMove already passed.
func (gs *GameState) ExecuteMove(from, to Position, player Color, promotion PieceType) Move {
	plan, e := gs.ValidateMove(from, to, player, promotion)
	if e != nil {
		// Caller is misusing the API (calling Execute before Validate passed).
		// Return an empty Move to keep signature compatibility; caller should
		// have detected this via ValidateMove returning an error.
		return Move{}
	}
	piece := gs.Get(from)
	captured := gs.applyMoveInternal(from, to, *piece, plan.promotionPiece, plan.isEnPassant, plan.isCastle)

	m := Move{From: from, To: to, Piece: *piece, Captured: captured, Promotion: plan.promotionPiece, CastleKind: plan.isCastle, EnPassant: plan.isEnPassant}
	gs.MoveHistory = append(gs.MoveHistory, m)
	gs.fullMoveNumber++
	gs.recordPosition()

	// Switch turn.
	gs.Turn = gs.Turn.Opposite()

	// Check game-ending conditions.
	gs.updateStatus()
	return m
}

// applyMoveInternal physically mutates the board. Returns the captured piece (or nil).
// Used by both ExecuteMove (with stashed validation) and PromoteTo.
func (gs *GameState) applyMoveInternal(from, to Position, piece Piece, promo PieceType, isEnPassant bool, castleKind string) *Piece {
	target := gs.Get(to)
	captured := target
	isCapture := target != nil

	// Move the piece.
	gs.set(to, &piece)
	gs.set(from, nil)

	// Castling: move the rook as well.
	if castleKind != "" {
		var rookFromX, rookToX int
		var rankY int
		if piece.Color == White {
			rankY = 0
		} else {
			rankY = 7
		}
		if castleKind == "king" {
			rookFromX = 7
			rookToX = 5
		} else {
			rookFromX = 0
			rookToX = 3
		}
		r := gs.Board[rankY][rookFromX]
		gs.set(Position{X: rookToX, Y: rankY}, r)
		gs.set(Position{X: rookFromX, Y: rankY}, nil)
	}

	// En passant: capture the enemy pawn on the rank behind the destination.
	if isEnPassant {
		var pawnY int
		if piece.Color == White {
			pawnY = to.Y - 1
		} else {
			pawnY = to.Y + 1
		}
		captured = gs.Board[pawnY][to.X]
		gs.set(Position{X: to.X, Y: pawnY}, nil)
		isCapture = true
	}

	// Promotion: replace the pawn at `to` with the chosen piece.
	if piece.Type == Pawn && isPromotionRank(to, piece.Color) {
		if !isValidPromotionPiece(promo) {
			promo = Queen // default; should be set explicitly via ValidateMove(_, _, _, Queen)
		}
		gs.set(to, &Piece{Color: piece.Color, Type: promo})
	}

	// Update castling rights when king or rook moves from starting square.
	if piece.Type == King {
		if piece.Color == White {
			gs.CastlingRights.WhiteKingSide = false
			gs.CastlingRights.WhiteQueenSide = false
		} else {
			gs.CastlingRights.BlackKingSide = false
			gs.CastlingRights.BlackQueenSide = false
		}
	}
	if piece.Type == Rook {
		if piece.Color == White && from.Y == 0 {
			if from.X == 0 {
				gs.CastlingRights.WhiteQueenSide = false
			}
			if from.X == 7 {
				gs.CastlingRights.WhiteKingSide = false
			}
		}
		if piece.Color == Black && from.Y == 7 {
			if from.X == 0 {
				gs.CastlingRights.BlackQueenSide = false
			}
			if from.X == 7 {
				gs.CastlingRights.BlackKingSide = false
			}
		}
	}
	// Capturing a rook on its starting square removes that side's castling right.
	if isCapture && target != nil && target.Type == Rook {
		if target.Color == White && to.Y == 0 {
			if to.X == 0 {
				gs.CastlingRights.WhiteQueenSide = false
			}
			if to.X == 7 {
				gs.CastlingRights.WhiteKingSide = false
			}
		}
		if target.Color == Black && to.Y == 7 {
			if to.X == 0 {
				gs.CastlingRights.BlackQueenSide = false
			}
			if to.X == 7 {
				gs.CastlingRights.BlackKingSide = false
			}
		}
	}

	// Update en-passant target: only set when a pawn moves two squares.
	gs.EnPassantTarget = Position{X: -1, Y: -1}
	if piece.Type == Pawn && abs(to.Y-from.Y) == 2 {
		epY := (to.Y + from.Y) / 2
		gs.EnPassantTarget = Position{X: to.X, Y: epY}
	}

	// 50-move rule counter.
	if isCapture || piece.Type == Pawn {
		gs.halfMoveClock = 0
	} else {
		gs.halfMoveClock++
	}

	return captured
}

// GenerateLegalMoves returns all legal moves for the given color.
func (gs *GameState) GenerateLegalMoves(c Color) []Move {
	var moves []Move
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			p := gs.Board[y][x]
			if p == nil || p.Color != c {
				continue
			}
			from := Position{X: x, Y: y}
			targets := gs.pseudoLegalTargets(from, p)
			for _, to := range targets {
				if _, e := gs.ValidateMove(from, to, c, Queen); e == nil {
					m := Move{From: from, To: to, Piece: *p}
					if p.Type == Pawn && isPromotionRank(to, c) {
						m.Promotion = Queen
					}
					if p.Type == King && abs(to.X-from.X) == 2 {
						if to.X > from.X {
							m.CastleKind = "king"
						} else {
							m.CastleKind = "queen"
						}
					}
					moves = append(moves, m)
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

// ─────────────────── Castling ───────────────────

// canCastle checks all castling preconditions.
// kingSide=true → O-O (king-side), false → O-O-O (queen-side).
// Preconditions per FIDE 5.1:
//   - King + the chosen Rook have never moved
//   - No pieces between king and rook
//   - King is not currently in check
//   - King does not pass through an attacked square
//   - King does not end on an attacked square
func (gs *GameState) canCastle(c Color, kingSide bool) bool {
	// Castling rights still available?
	if c == White {
		if kingSide && !gs.CastlingRights.WhiteKingSide {
			return false
		}
		if !kingSide && !gs.CastlingRights.WhiteQueenSide {
			return false
		}
	} else {
		if kingSide && !gs.CastlingRights.BlackKingSide {
			return false
		}
		if !kingSide && !gs.CastlingRights.BlackQueenSide {
			return false
		}
	}

	// King and rook on starting squares.
	var rankY int
	if c == White {
		rankY = 0
	} else {
		rankY = 7
	}
	king := gs.Board[rankY][4]
	if king == nil || king.Type != King || king.Color != c {
		return false
	}
	var rookX int
	if kingSide {
		rookX = 7
	} else {
		rookX = 0
	}
	rook := gs.Board[rankY][rookX]
	if rook == nil || rook.Type != Rook || rook.Color != c {
		return false
	}

	// Squares between king and rook must be empty.
	if kingSide {
		if gs.Board[rankY][5] != nil || gs.Board[rankY][6] != nil {
			return false
		}
	} else {
		if gs.Board[rankY][1] != nil || gs.Board[rankY][2] != nil || gs.Board[rankY][3] != nil {
			return false
		}
	}

	// King not currently in check, doesn't pass through or land on attacked square.
	opp := c.Opposite()
	if gs.isAttackedBy(Position{X: 4, Y: rankY}, opp) {
		return false
	}
	if gs.isAttackedBy(Position{X: 5, Y: rankY}, opp) {
		return false
	}
	if gs.isAttackedBy(Position{X: 6, Y: rankY}, opp) {
		return false
	}
	return true
}

// ─────────────────── Per-piece pseudo-legal move generation ───────────────────

// pseudoLegalTargets returns all positions the piece at `from` could move to
// ignoring self-check / castling / en-passant restrictions. Used to enumerate
// candidates for ValidateMove.
func (gs *GameState) pseudoLegalTargets(from Position, piece *Piece) []Position {
	var targets []Position
	add := func(x, y int) {
		if x < 0 || x > 7 || y < 0 || y > 7 {
			return
		}
		to := Position{X: x, Y: y}
		t := gs.Get(to)
		if t != nil && t.Color == piece.Color {
			return // can't land on own piece
		}
		targets = append(targets, to)
	}

	switch piece.Type {
	case Pawn:
		dir := -1
		startRank := 6 // y=6 for Black pawns (rank 7)
		if piece.Color == White {
			dir = 1
			startRank = 1 // y=1 for White pawns (rank 2)
		}
		// Forward 1
		add(from.X, from.Y+dir)
		// Forward 2 from starting rank
		if from.Y == startRank && gs.Get(Position{X: from.X, Y: from.Y + dir}) == nil {
			add(from.X, from.Y+2*dir)
		}
		// Captures
		add(from.X-1, from.Y+dir)
		add(from.X+1, from.Y+dir)
		// En passant target square (only if pawn isn't actually capturing anything there)
		ep := gs.EnPassantTarget
		if ep.IsValid() && abs(from.Y+dir-ep.Y) == 0 && abs(from.X-ep.X) == 1 {
			targets = append(targets, ep)
		}

	case Knight:
		moves := [][2]int{{-2, -1}, {-2, 1}, {-1, -2}, {-1, 2}, {1, -2}, {1, 2}, {2, -1}, {2, 1}}
		for _, m := range moves {
			add(from.X+m[0], from.Y+m[1])
		}

	case Bishop, Rook, Queen:
		dirs := []struct{ dx, dy, slides int }{}
		if piece.Type == Bishop || piece.Type == Queen {
			dirs = append(dirs, struct{ dx, dy, slides int }{1, 1, 8})
			dirs = append(dirs, struct{ dx, dy, slides int }{-1, 1, 8})
			dirs = append(dirs, struct{ dx, dy, slides int }{1, -1, 8})
			dirs = append(dirs, struct{ dx, dy, slides int }{-1, -1, 8})
		}
		if piece.Type == Rook || piece.Type == Queen {
			dirs = append(dirs, struct{ dx, dy, slides int }{1, 0, 8})
			dirs = append(dirs, struct{ dx, dy, slides int }{-1, 0, 8})
			dirs = append(dirs, struct{ dx, dy, slides int }{0, 1, 8})
			dirs = append(dirs, struct{ dx, dy, slides int }{0, -1, 8})
		}
		for _, d := range dirs {
			for i := 1; i < d.slides; i++ {
				x, y := from.X+d.dx*i, from.Y+d.dy*i
				if x < 0 || x > 7 || y < 0 || y > 7 {
					break
				}
				to := Position{X: x, Y: y}
				if gs.Get(to) != nil {
					if gs.Get(to).Color != piece.Color {
						targets = append(targets, to)
					}
					break // path blocked
				}
				targets = append(targets, to)
			}
		}

	case King:
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				if dx == 0 && dy == 0 {
					continue
				}
				add(from.X+dx, from.Y+dy)
			}
		}
		// Castling destinations (kingSide/queenSide)
		dir := 1
		if piece.Color == Black {
			dir = -1
		}
		rankY := from.Y
		if piece.Color == White && from.Y == 0 || piece.Color == Black && from.Y == 7 {
			if gs.canCastle(piece.Color, true) {
				targets = append(targets, Position{X: from.X + 2, Y: rankY})
			}
			if gs.canCastle(piece.Color, false) {
				targets = append(targets, Position{X: from.X - 2, Y: rankY})
			}
		}
		_ = dir
	}

	return targets
}

// isValidPieceMove dispatches to per-piece movement rules.
func isValidPieceMove(gs *GameState, from, to Position, piece *Piece) bool {
	dx := to.X - from.X
	dy := to.Y - from.Y

	switch piece.Type {
	case Pawn:
		return isValidPawnMove(gs, from, to, piece)
	case Knight:
		return isValidKnightMove(dx, dy)
	case Bishop:
		return isValidBishopMove(gs, from, to, dx, dy)
	case Rook:
		return isValidRookMove(gs, from, to, dx, dy)
	case Queen:
		return isValidQueenMove(gs, from, to, dx, dy)
	case King:
		return isValidKingMove(dx, dy)
	}
	return false
}

// ── King ──
func isValidKingMove(dx, dy int) bool {
	absdx, absdy := abs(dx), abs(dy)
	return absdx <= 1 && absdy <= 1 && (absdx+absdy) >= 1
}

// ── Queen ──
func isValidQueenMove(gs *GameState, from, to Position, dx, dy int) bool {
	if dx == 0 || dy == 0 {
		return isValidRookMove(gs, from, to, dx, dy)
	}
	if abs(dx) == abs(dy) {
		return isValidBishopMove(gs, from, to, dx, dy)
	}
	return false
}

// ── Rook ──
func isValidRookMove(gs *GameState, from, to Position, dx, dy int) bool {
	if dx != 0 && dy != 0 {
		return false
	}
	return gs.countBetween(from, to) == 0
}

// ── Bishop ──
func isValidBishopMove(gs *GameState, from, to Position, dx, dy int) bool {
	if abs(dx) != abs(dy) {
		return false
	}
	return gs.countBetween(from, to) == 0
}

// ── Knight ──
func isValidKnightMove(dx, dy int) bool {
	absdx, absdy := abs(dx), abs(dy)
	return (absdx == 1 && absdy == 2) || (absdx == 2 && absdy == 1)
}

// ── Pawn ──
func isValidPawnMove(gs *GameState, from, to Position, piece *Piece) bool {
	dx := to.X - from.X
	dy := to.Y - from.Y
	absdx, absdy := abs(dx), abs(dy)

	dir := -1
	startRank := 6
	if piece.Color == White {
		dir = 1
		startRank = 1
	}

	target := gs.Get(to)

	if absdx == 0 {
		// Forward move, no capture.
		if dy == dir && target == nil {
			return true
		}
		if from.Y == startRank && dy == 2*dir && target == nil {
			// Must have intermediate square empty too.
			mid := Position{X: from.X, Y: from.Y + dir}
			if gs.Get(mid) == nil {
				return true
			}
		}
		return false
	}

	if absdx == 1 && absdy == 1 && dy == dir {
		// Diagonal: capture if target, or en-passant if EP target matches.
		if target != nil && target.Color != piece.Color {
			return true
		}
		if to == gs.EnPassantTarget {
			return true
		}
	}
	return false
}

// isPromotionRank checks if `pos` is the back rank for `c`.
func isPromotionRank(pos Position, c Color) bool {
	if c == White {
		return pos.Y == 7
	}
	return pos.Y == 0
}

// isValidPromotionPiece returns true for Q/R/B/N.
func isValidPromotionPiece(t PieceType) bool {
	return t == Queen || t == Rook || t == Bishop || t == Knight
}

// ─────────────────── Attack / Check utilities ───────────────────

// isAttackedBy reports whether any piece of `attacker` color attacks `pos`.
func (gs *GameState) isAttackedBy(pos Position, attacker Color) bool {
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			p := gs.Board[y][x]
			if p == nil || p.Color != attacker {
				continue
			}
			from := Position{X: x, Y: y}
			if gs.canAttack(from, pos, p) {
				return true
			}
		}
	}
	return false
}

// canAttack checks if a piece at `from` can attack `to`. Used for check detection.
func (gs *GameState) canAttack(from, to Position, piece *Piece) bool {
	dx := to.X - from.X
	dy := to.Y - from.Y
	switch piece.Type {
	case Pawn:
		// Pawns attack diagonally forward only.
		dir := -1
		if piece.Color == White {
			dir = 1
		}
		return abs(dx) == 1 && dy == dir
	case Knight:
		return isValidKnightMove(dx, dy)
	case Bishop:
		return isValidBishopMove(gs, from, to, dx, dy)
	case Rook:
		return isValidRookMove(gs, from, to, dx, dy)
	case Queen:
		return isValidQueenMove(gs, from, to, dx, dy)
	case King:
		return isValidKingMove(dx, dy)
	}
	return false
}

// wouldBeInCheck simulates a move and reports whether the moving side's king ends in check.
// Used to validate moves don't leave own king in check.
func (gs *GameState) wouldBeInCheck(from, to Position, c Color, isEnPassant bool) bool {
	moved := gs.Get(from)
	target := gs.Get(to)
	gs.set(to, moved)
	gs.set(from, nil)
	capturedPawn := (*Piece)(nil)
	if isEnPassant {
		var pawnY int
		if c == White {
			pawnY = to.Y - 1
		} else {
			pawnY = to.Y + 1
		}
		capturedPawn = gs.Board[pawnY][to.X]
		gs.set(Position{X: to.X, Y: pawnY}, nil)
	}
	inCheck := gs.IsInCheck(c)
	// Undo
	gs.set(from, moved)
	gs.set(to, target)
	if isEnPassant {
		var pawnY int
		if c == White {
			pawnY = to.Y - 1
		} else {
			pawnY = to.Y + 1
		}
		gs.set(Position{X: to.X, Y: pawnY}, capturedPawn)
	}
	return inCheck
}

// ─────────────────── End-of-game detection ───────────────────

// updateStatus checks end-of-game conditions after a move.
//
// Precedence: 50-move > threefold > insufficient > checkmate > stalemate.
func (gs *GameState) updateStatus() {
	next := gs.Turn

	// 1. 50-move rule.
	if gs.halfMoveClock >= 100 {
		gs.Status = StatusDraw
		return
	}
	// 2. Threefold repetition (same position 3+ times).
	if gs.positionRepeats(3) {
		gs.Status = StatusDraw
		return
	}
	// 3. Insufficient material.
	if gs.insufficientMaterial() {
		gs.Status = StatusDraw
		return
	}

	// 4/5. Check / stalemate detection via legal-move count.
	legal := gs.GenerateLegalMoves(next)
	inCheck := gs.IsInCheck(next)

	if len(legal) == 0 {
		if inCheck {
			if next == White {
				gs.Status = StatusBlackWin
			} else {
				gs.Status = StatusWhiteWin
			}
		} else {
			gs.Status = StatusDraw
		}
	}
}

// positionKey returns a FEN-like signature of the current position
// (board + side-to-move + castling + EP), ignoring half/full move counters.
// Used to detect threefold repetition.
func (gs *GameState) positionKey() string {
	b := make([]byte, 0, 80)
	for y := 7; y >= 0; y-- {
		for x := 0; x < 8; x++ {
			p := gs.Board[y][x]
			if p == nil {
				b = append(b, '.')
				continue
			}
			ch := byte('k')
			switch p.Type {
			case King:
				ch = 'k'
			case Queen:
				ch = 'q'
			case Rook:
				ch = 'r'
			case Bishop:
				ch = 'b'
			case Knight:
				ch = 'n'
			case Pawn:
				ch = 'p'
			}
			if p.Color == White {
				ch -= 'a' - 'A'
			}
			b = append(b, ch)
		}
		if y != 0 {
			b = append(b, '/')
		}
	}
	t := 'w'
	if gs.Turn == Black {
		t = 'b'
	}
	b = append(b, byte(' '), byte(t), byte(' '))
	if gs.CastlingRights.WhiteKingSide {
		b = append(b, 'K')
	}
	if gs.CastlingRights.WhiteQueenSide {
		b = append(b, 'Q')
	}
	if gs.CastlingRights.BlackKingSide {
		b = append(b, 'k')
	}
	if gs.CastlingRights.BlackQueenSide {
		b = append(b, 'q')
	}
	if len(b) == 0 || (len(b) > 0 && b[len(b)-1] == ' ') {
		b = append(b, '-')
	}
	if gs.EnPassantTarget.IsValid() {
		b = append(b, ' ')
		b = append(b, gs.EnPassantTarget.Algebraic()...)
	} else {
		b = append(b, ' ')
		b = append(b, '-')
	}
	return string(b)
}

// recordPosition adds the current position to history.
func (gs *GameState) recordPosition() {
	gs.positionKeys = append(gs.positionKeys, gs.positionKey())
}

// positionRepeats reports whether the same position has occurred at least `n` times.
func (gs *GameState) positionRepeats(n int) bool {
	if len(gs.positionKeys) == 0 {
		return false
	}
	cur := gs.positionKeys[len(gs.positionKeys)-1]
	count := 0
	for _, k := range gs.positionKeys {
		if k == cur {
			count++
		}
	}
	return count >= n
}

// insufficientMaterial checks FIDE 5.2.2 / Article 5: positions where checkmate
// is impossible. We consider the standard FIDE list of dead positions:
//
//	- K vs K
//	- K+B vs K
//	- K+N vs K
//	- K+B vs K+B with both bishops on same-colored squares
//
// Note: K+B+N vs K can theoretically mate, so it is NOT insufficient.
func (gs *GameState) insufficientMaterial() bool {
	// Gather all pieces, also tracking bishop positions by color.
	type bsq struct {
		pos  Position
		colC Color
	}
	bishops := []bsq{}
	pieces := 0
	knights := 0
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			p := gs.Board[y][x]
			if p == nil {
				continue
			}
			pieces++
			switch p.Type {
			case Pawn, Rook, Queen:
				return false
			case Bishop:
				bishops = append(bishops, bsq{Position{X: x, Y: y}, p.Color})
			case Knight:
				knights++
			}
		}
	}
	// K vs K
	if pieces == 2 {
		return true
	}
	// K + minor(s) vs K : if the only non-king pieces are ≤ 2 minor pieces,
	// we need to be more careful. K+N vs K = 4 pieces: insufficient.
	// K+B vs K = insufficient. K+B+N vs K = NOT insufficient.
	if len(bishops) == 0 && knights <= 1 {
		// No bishops, only knights.
		if pieces <= 4 && knights == 1 {
			return true
		}
	}
	if knights == 0 && len(bishops) <= 2 {
		// Only bishops (plus kings).
		if len(bishops) == 0 {
			// Already handled above (K vs K = pieces==2).
			return pieces == 2
		}
		if len(bishops) == 1 {
			// K+B vs K : 3 pieces.
			return pieces == 3
		}
		if len(bishops) == 2 {
			// K+B vs K+B : 4 pieces. Insufficient if same-color squares.
			c1 := (bishops[0].pos.X + bishops[0].pos.Y) % 2
			c2 := (bishops[1].pos.X + bishops[1].pos.Y) % 2
			if c1 == c2 {
				return true
			}
			return false
		}
	}
	return false
}

// bishopSquareColor returns "light" or "dark" — the color of a square.
func bishopSquareColor(p Position) int {
	return (p.X + p.Y) % 2
}

// ─────────────────── Geometry helpers ───────────────────

// countBetween counts pieces strictly between two positions on a rook/bishop line.
func (gs *GameState) countBetween(a, b Position) int {
	count := 0
	if a.X == b.X {
		lo, hi := a.Y, b.Y
		if lo > hi {
			lo, hi = hi, lo
		}
		for y := lo + 1; y < hi; y++ {
			if gs.Board[y][a.X] != nil {
				count++
			}
		}
	} else if a.Y == b.Y {
		lo, hi := a.X, b.X
		if lo > hi {
			lo, hi = hi, lo
		}
		for x := lo + 1; x < hi; x++ {
			if gs.Board[a.Y][x] != nil {
				count++
			}
		}
	} else {
		// Diagonal: a and b must be on the same diagonal.
		dx := b.X - a.X
		dy := b.Y - a.Y
		if abs(dx) != abs(dy) {
			return 0
		}
		stepX := 1
		if dx < 0 {
			stepX = -1
		}
		stepY := 1
		if dy < 0 {
			stepY = -1
		}
		x, y := a.X+stepX, a.Y+stepY
		for x != b.X && y != b.Y {
			if gs.Board[y][x] != nil {
				count++
			}
			x += stepX
			y += stepY
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
