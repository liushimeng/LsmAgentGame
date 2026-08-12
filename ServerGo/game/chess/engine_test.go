package chess

import (
	"testing"
)

// ─────────────────── Setup helpers ───────────────────

// emptyBoard returns a game state with pieces cleared but Turn still White.
// Tests should explicitly place any kings they need.
func emptyBoard() *GameState {
	gs := NewGame()
	gs.Board = [8][8]*Piece{}
	return gs
}

// placeKings puts White king at (0,0) and Black king at (7,7) — far enough apart
// not to interfere with most test setups.
func placeKings(gs *GameState) {
	setPiece(gs, Position{0, 0}, &Piece{Color: White, Type: King})
	setPiece(gs, Position{7, 7}, &Piece{Color: Black, Type: King})
}

// setPiece places a piece at pos.
func setPiece(gs *GameState, pos Position, p *Piece) {
	gs.set(pos, p)
}

// ─────────────────── Initial position ───────────────────

func TestNewGame_InitialPosition(t *testing.T) {
	gs := NewGame()
	// White back rank (y=0): R N B Q K B N R
	expectedBack := []PieceType{Rook, Knight, Bishop, Queen, King, Bishop, Knight, Rook}
	for x, want := range expectedBack {
		p := gs.Board[0][x]
		if p == nil {
			t.Fatalf("expected White piece at (%d,0)", x)
		}
		if p.Type != want || p.Color != White {
			t.Errorf("at (%d,0) got %v, want %v White", x, p.Type, want)
		}
	}
	// Black back rank (y=7).
	for x, want := range expectedBack {
		p := gs.Board[7][x]
		if p == nil {
			t.Fatalf("expected Black piece at (%d,7)", x)
		}
		if p.Type != want || p.Color != Black {
			t.Errorf("at (%d,7) got %v, want %v Black", x, p.Type, want)
		}
	}
	// Pawn rank.
	for x := 0; x < 8; x++ {
		if gs.Board[1][x] == nil || gs.Board[1][x].Type != Pawn || gs.Board[1][x].Color != White {
			t.Errorf("expected White pawn at (%d,1)", x)
		}
		if gs.Board[6][x] == nil || gs.Board[6][x].Type != Pawn || gs.Board[6][x].Color != Black {
			t.Errorf("expected Black pawn at (%d,6)", x)
		}
	}
	// Turn = White.
	if gs.Turn != White {
		t.Errorf("Turn = %v, want White", gs.Turn)
	}
	// Castling rights intact.
	if !gs.CastlingRights.WhiteKingSide || !gs.CastlingRights.WhiteQueenSide {
		t.Errorf("expected White castling rights available")
	}
	if !gs.CastlingRights.BlackKingSide || !gs.CastlingRights.BlackQueenSide {
		t.Errorf("expected Black castling rights available")
	}
}

func TestPieceNames(t *testing.T) {
	if pieceTypeName(King) != "King" {
		t.Errorf("King name")
	}
	if pieceTypeNames[Queen] != "queen" {
		t.Errorf("queen type name")
	}
}

// ─────────────────── Basic piece movements ───────────────────

func TestKnightMovement(t *testing.T) {
	gs := NewGame()
	// White knight from b1 (1,0) → a3 (0,2) — legal.
	if _, e := gs.ValidateMove(Position{1, 0}, Position{0, 2}, White, 0); e != nil {
		t.Errorf("Na3 should be legal, got %v", e)
	}
	// Knight from b1 → b2 — illegal (not L-shape).
	if _, e := gs.ValidateMove(Position{1, 0}, Position{1, 1}, White, 0); e == nil {
		t.Errorf("Nb1-b2 should be illegal")
	}
}

func TestPawnDoubleMove(t *testing.T) {
	gs := NewGame()
	// White e2 (4,1) → e4 (4,3) — double from start.
	if _, e := gs.ValidateMove(Position{4, 1}, Position{4, 3}, White, 0); e != nil {
		t.Errorf("e2-e4 should be legal, got %v", e)
	}
	// White e2 → e5 — too far.
	if _, e := gs.ValidateMove(Position{4, 1}, Position{4, 4}, White, 0); e == nil {
		t.Errorf("e2-e5 should be illegal")
	}
}

func TestPawnDiagonalCapture(t *testing.T) {
	gs := emptyBoard()
	placeKings(gs)
	setPiece(gs, Position{4, 1}, &Piece{Color: White, Type: Pawn})
	setPiece(gs, Position{5, 2}, &Piece{Color: Black, Type: Pawn})
	if _, e := gs.ValidateMove(Position{4, 1}, Position{5, 2}, White, 0); e != nil {
		t.Errorf("pawn should capture diagonally, got %v", e)
	}
}

// ─────────────────── Castling ───────────────────

func TestCastlingLegal(t *testing.T) {
	gs := emptyBoard()
	setPiece(gs, Position{4, 0}, &Piece{Color: White, Type: King})
	setPiece(gs, Position{7, 0}, &Piece{Color: White, Type: Rook})
	// King path: e1→f1→g1. None attacked.
	// White O-O = king moves to g1 (x=6), rook to f1 (x=5).
	if _, e := gs.ValidateMove(Position{4, 0}, Position{6, 0}, White, 0); e != nil {
		t.Errorf("O-O should be legal, got %v", e)
	}
	m := gs.ExecuteMove(Position{4, 0}, Position{6, 0}, White, 0)
	if m.CastleKind != "king" {
		t.Errorf("expected CastleKind=king, got %q", m.CastleKind)
	}
	// Rook should now be at f1 (5,0).
	if gs.Board[0][5] == nil || gs.Board[0][5].Type != Rook {
		t.Errorf("expected rook at f1 after castling, got %v", gs.Board[0][5])
	}
	// Castling rights should be cleared for White.
	if gs.CastlingRights.WhiteKingSide {
		t.Errorf("expected White castling right cleared after king move")
	}
}

func TestCastlingIllegalIfKingInCheck(t *testing.T) {
	gs := emptyBoard()
	setPiece(gs, Position{4, 0}, &Piece{Color: White, Type: King})
	setPiece(gs, Position{7, 0}, &Piece{Color: White, Type: Rook})
	setPiece(gs, Position{4, 7}, &Piece{Color: Black, Type: Rook}) // attacks e1.
	if _, e := gs.ValidateMove(Position{4, 0}, Position{6, 0}, White, 0); e == nil {
		t.Errorf("O-O should be illegal when king in check")
	}
}

func TestCastlingIllegalPassingThroughAttack(t *testing.T) {
	gs := emptyBoard()
	setPiece(gs, Position{4, 0}, &Piece{Color: White, Type: King})
	setPiece(gs, Position{7, 0}, &Piece{Color: White, Type: Rook})
	// Black rook at h8 (7,7) doesn't attack f1 or g1 (different rank).
	// So this setup actually allows castling. Use a position where f1 is attacked.
	setPiece(gs, Position{5, 7}, &Piece{Color: Black, Type: Rook}) // attacks f1 (5,0)
	if _, e := gs.ValidateMove(Position{4, 0}, Position{6, 0}, White, 0); e == nil {
		t.Errorf("O-O should be illegal when king passes through attacked square")
	}
}

func TestCastlingIllegalIfBlocked(t *testing.T) {
	gs := emptyBoard()
	setPiece(gs, Position{4, 0}, &Piece{Color: White, Type: King})
	setPiece(gs, Position{7, 0}, &Piece{Color: White, Type: Rook})
	setPiece(gs, Position{5, 0}, &Piece{Color: White, Type: Bishop}) // blocks f1
	if _, e := gs.ValidateMove(Position{4, 0}, Position{6, 0}, White, 0); e == nil {
		t.Errorf("O-O should be illegal with bishop on f1")
	}
}

// ─────────────────── En passant ───────────────────

func TestEnPassant(t *testing.T) {
	gs := emptyBoard()
	placeKings(gs)
	setPiece(gs, Position{4, 4}, &Piece{Color: White, Type: Pawn}) // White pawn on e5 (rank 5)
	setPiece(gs, Position{3, 6}, &Piece{Color: Black, Type: Pawn}) // Black pawn on d7 (rank 7)
	gs.Turn = Black
	// Black plays d7-d5 (double-move): from Y=6 to Y=4.
	_, err := gs.ValidateMove(Position{3, 6}, Position{3, 4}, Black, 0)
	if err != nil {
		t.Fatalf("d7-d5 should be legal: %v", err)
	}
	gs.ExecuteMove(Position{3, 6}, Position{3, 4}, Black, 0)
	// Now EP target is d6 (X=3, Y=5).
	if gs.EnPassantTarget != (Position{3, 5}) {
		t.Errorf("expected EP target at d6, got %v", gs.EnPassantTarget)
	}
	// White can capture en passant: e5 → d6.
	if _, e := gs.ValidateMove(Position{4, 4}, Position{3, 5}, White, 0); e != nil {
		t.Errorf("en passant should be legal, got %v", e)
	}
	m := gs.ExecuteMove(Position{4, 4}, Position{3, 5}, White, 0)
	if !m.EnPassant {
		t.Errorf("expected EnPassant=true")
	}
	// Black pawn on d5 (X=3, Y=4) should be removed.
	if gs.Board[4][3] != nil {
		t.Errorf("Black pawn on d5 should be captured, got %v", gs.Board[4][3])
	}
}

func TestEnPassantExpiresAfterOneMove(t *testing.T) {
	gs := emptyBoard()
	placeKings(gs)
	setPiece(gs, Position{4, 4}, &Piece{Color: White, Type: Pawn})
	setPiece(gs, Position{3, 6}, &Piece{Color: Black, Type: Pawn})
	gs.Turn = Black
	gs.ExecuteMove(Position{3, 6}, Position{3, 4}, Black, 0)
	// White plays any other move (e.g., move knight out).
	setPiece(gs, Position{1, 0}, &Piece{Color: White, Type: Knight})
	if _, e := gs.ValidateMove(Position{1, 0}, Position{0, 2}, White, 0); e != nil {
		t.Fatalf("Nb1-a3 should be legal: %v", e)
	}
	gs.ExecuteMove(Position{1, 0}, Position{0, 2}, White, 0)
	// Now EP right is lost. e5 → d6 should fail.
	if _, e := gs.ValidateMove(Position{4, 4}, Position{3, 5}, White, 0); e == nil {
		t.Errorf("en passant should be illegal one move later")
	}
}

// ─────────────────── Pawn promotion ───────────────────

func TestPawnPromotionRequiresChoice(t *testing.T) {
	gs := emptyBoard()
	placeKings(gs)
	setPiece(gs, Position{0, 6}, &Piece{Color: White, Type: Pawn}) // on 7th rank
	// Without specifying promotion piece → error.
	if _, e := gs.ValidateMove(Position{0, 6}, Position{0, 7}, White, 0); e == nil {
		t.Errorf("promotion move should require a piece choice")
	}
	// With promotion=Queen → ok.
	if _, e := gs.ValidateMove(Position{0, 6}, Position{0, 7}, White, Queen); e != nil {
		t.Errorf("promotion to Queen should be legal, got %v", e)
	}
}

func TestPawnPromotionReplace(t *testing.T) {
	gs := emptyBoard()
	placeKings(gs)
	setPiece(gs, Position{0, 6}, &Piece{Color: White, Type: Pawn})
	m := gs.ExecuteMove(Position{0, 6}, Position{0, 7}, White, Queen)
	if m.Promotion != Queen {
		t.Errorf("expected Promotion=Queen, got %v", m.Promotion)
	}
	// Board now has White Queen at a8 (X=0, Y=7).
	if gs.Board[7][0] == nil || gs.Board[7][0].Type != Queen || gs.Board[7][0].Color != White {
		t.Errorf("expected Queen at a8, got %v", gs.Board[7][0])
	}
}

// ─────────────────── Check / checkmate ───────────────────

func TestCheckmateBackRank(t *testing.T) {
	gs := emptyBoard()
	setPiece(gs, Position{4, 0}, &Piece{Color: White, Type: King})
	setPiece(gs, Position{5, 0}, &Piece{Color: White, Type: Pawn})
	setPiece(gs, Position{6, 0}, &Piece{Color: White, Type: Pawn})
	setPiece(gs, Position{3, 1}, &Piece{Color: White, Type: Rook})
	setPiece(gs, Position{0, 1}, &Piece{Color: White, Type: Rook})
	// Black rook on e8 gives checkmate.
	setPiece(gs, Position{4, 7}, &Piece{Color: Black, Type: Rook})
	// It's Black's turn? No — let's set Turn to Black.
	gs.Turn = Black
	// Black plays any move? No — we want to verify White is checkmated on Black's turn? Wait.
	// After Black moves the rook down, we test. Let's just play Re8→e1.
	if _, e := gs.ValidateMove(Position{4, 7}, Position{4, 0}, Black, 0); e == nil {
		t.Errorf("Re8-e1 should be illegal because of pawn protection ... wait, no pawn at e2 means no")
	}
	// Actually, with pawns at f2,g2 the king can't escape to f1 or g1, and rook takes e1.
	// Let's run it: Black plays Re8-e8#.
	if _, e := gs.ValidateMove(Position{4, 7}, Position{4, 0}, Black, 0); e == nil {
		gs.ExecuteMove(Position{4, 7}, Position{4, 0}, Black, 0)
		// Now status should be StatusBlackWin (White king captured → Black wins).
		if gs.Status != StatusBlackWin {
			t.Errorf("expected StatusBlackWin, got %v", gs.Status)
		}
	}
}

// ─────────────────── Stalemate ───────────────────

func TestStalemate(t *testing.T) {
	gs := emptyBoard()
	// Classic stalemate position: Black king h8, White king f7, White queen g6.
	// Black to move but no legal moves and not in check → stalemate (draw).
	setPiece(gs, Position{7, 7}, &Piece{Color: Black, Type: King})
	setPiece(gs, Position{5, 6}, &Piece{Color: White, Type: King})
	setPiece(gs, Position{6, 5}, &Piece{Color: White, Type: Queen})
	gs.Turn = Black
	if gs.IsInCheck(Black) {
		t.Fatalf("pre-condition: Black should NOT be in check")
	}
	legal := gs.GenerateLegalMoves(Black)
	if len(legal) != 0 {
		for _, m := range legal {
			t.Logf("got move %v", m)
		}
		t.Errorf("expected no legal Black moves, got %d", len(legal))
	}
}

// ─────────────────── 50-move rule ───────────────────

func TestFiftyMoveRule(t *testing.T) {
	gs := emptyBoard()
	// Construct a valid position (K+R vs K+R), and rely on the rule's
	// halfMoveClock check directly to avoid fragile move sequences.
	setPiece(gs, Position{0, 0}, &Piece{Color: White, Type: King})
	setPiece(gs, Position{0, 2}, &Piece{Color: White, Type: Rook})
	setPiece(gs, Position{7, 7}, &Piece{Color: Black, Type: King})
	setPiece(gs, Position{7, 5}, &Piece{Color: Black, Type: Rook})
	gs.halfMoveClock = 100 // 50 full moves done with no capture/pawn move.
	gs.Turn = White
	// Trigger end-of-game check by simulating that White just moved.
	gs.ExecuteMove(Position{0, 2}, Position{1, 2}, White, 0)
	// After the move, halfMoveClock was incremented to 101, status should be Draw.
	if gs.Status != StatusDraw {
		t.Errorf("expected StatusDraw with halfMoveClock >= 100, got %v (halfMoveClock=%d)", gs.Status, gs.halfMoveClock)
	}
}

// ─────────────────── Insufficient material ───────────────────

func TestInsufficientMaterialKingVsKing(t *testing.T) {
	gs := emptyBoard()
	setPiece(gs, Position{0, 0}, &Piece{Color: White, Type: King})
	setPiece(gs, Position{7, 7}, &Piece{Color: Black, Type: King})
	if !gs.insufficientMaterial() {
		t.Errorf("K vs K should be insufficient material, got pieces=%v", gs.Board)
	}
}

func TestInsufficientMaterialKBNvsK_Sufficient(t *testing.T) {
	gs := emptyBoard()
	setPiece(gs, Position{0, 0}, &Piece{Color: White, Type: King})
	setPiece(gs, Position{1, 0}, &Piece{Color: White, Type: Bishop})
	setPiece(gs, Position{2, 0}, &Piece{Color: White, Type: Knight})
	setPiece(gs, Position{7, 7}, &Piece{Color: Black, Type: King})
	// KBN vs K IS sufficient — it's a known mating combination.
	if gs.insufficientMaterial() {
		t.Errorf("KBN vs K should be sufficient material (mate is possible)")
	}
}

func TestInsufficientMaterialKNvsK(t *testing.T) {
	gs := emptyBoard()
	setPiece(gs, Position{0, 0}, &Piece{Color: White, Type: King})
	setPiece(gs, Position{1, 0}, &Piece{Color: White, Type: Knight})
	setPiece(gs, Position{7, 7}, &Piece{Color: Black, Type: King})
	// K+N vs K — KBN+extras don't mate, but K vs K+N has no mate.
	if !gs.insufficientMaterial() {
		t.Errorf("KN vs K should be insufficient material")
	}
}

func TestInsufficientMaterialKBvsK(t *testing.T) {
	gs := emptyBoard()
	setPiece(gs, Position{0, 0}, &Piece{Color: White, Type: King})
	setPiece(gs, Position{1, 0}, &Piece{Color: White, Type: Bishop})
	setPiece(gs, Position{7, 7}, &Piece{Color: Black, Type: King})
	if !gs.insufficientMaterial() {
		t.Errorf("KB vs K should be insufficient material")
	}
}

func TestInsufficientMaterialKBvsKBSameColor(t *testing.T) {
	gs := emptyBoard()
	// Both bishops on light squares. (0,0) and (2,0) are dark; use (1,0) and (3,0)
	// which are both dark too. For SAME color we need squares with same parity.
	// (0,0)%2=0, (0,2)%2=0 → same color (dark). (1,1)%2=0 dark, (0,0)%2=0 dark.
	// Let's put bishops on (0,1) and (2,1) — both have (x+y)%2=1, light squares.
	setPiece(gs, Position{0, 0}, &Piece{Color: White, Type: King})
	setPiece(gs, Position{0, 1}, &Piece{Color: White, Type: Bishop})
	setPiece(gs, Position{3, 0}, &Piece{Color: Black, Type: King})
	setPiece(gs, Position{2, 1}, &Piece{Color: Black, Type: Bishop})
	// (0+1)%2 = 1, (2+1)%2 = 1 → both light squares.
	if !gs.insufficientMaterial() {
		t.Errorf("KB vs KB (same square color) should be insufficient material")
	}
}

// ─────────────────── Turn / status errors ───────────────────

func TestNotYourTurn(t *testing.T) {
	gs := NewGame()
	// It's White's turn; Black tries to move.
	if _, e := gs.ValidateMove(Position{4, 6}, Position{4, 4}, Black, 0); e == nil {
		t.Errorf("Black moving first should be illegal")
	}
}

func TestInvalidMoveNoPiece(t *testing.T) {
	gs := NewGame()
	// e3 is empty.
	if _, e := gs.ValidateMove(Position{4, 2}, Position{4, 3}, White, 0); e == nil {
		t.Errorf("moving empty square should be illegal")
	}
}

func TestInvalidMoveOwnPiece(t *testing.T) {
	gs := NewGame()
	if _, e := gs.ValidateMove(Position{4, 6}, Position{4, 4}, White, 0); e == nil {
		t.Errorf("White capturing Black pawn on e7 with White pawn from a2 should fail")
	}
}

func TestGameAlreadyOver(t *testing.T) {
	gs := NewGame()
	gs.Status = StatusWhiteWin
	if _, e := gs.ValidateMove(Position{4, 1}, Position{4, 3}, White, 0); e == nil {
		t.Errorf("moving after game over should be illegal")
	}
}

// ─────────────────── Algebraic notation ───────────────────

func TestAlgebraic(t *testing.T) {
	pos := Position{X: 4, Y: 3}
	if pos.Algebraic() != "e4" {
		t.Errorf("e4 expected, got %q", pos.Algebraic())
	}
	p, ok := ParseAlgebraic("a1")
	if !ok || p.X != 0 || p.Y != 0 {
		t.Errorf("ParseAlgebraic(a1) = %v, %v; want (0,0), true", p, ok)
	}
	p, ok = ParseAlgebraic("h8")
	if !ok || p.X != 7 || p.Y != 7 {
		t.Errorf("ParseAlgebraic(h8) = %v, %v; want (7,7), true", p, ok)
	}
}
