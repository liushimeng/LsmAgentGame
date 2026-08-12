package junqi

import (
	"testing"

	"LsmWebGame/errcode"
)

// standardLayout returns a valid 25-piece layout for the given color.
// Delegates to buildLayoutProgrammatic which handles all the layout rules.
func standardLayout(color Color) []Placement {
	return buildLayoutProgrammatic(color)
}

// buildLayoutProgrammatic generates a valid 25-piece layout for the given color
// by placing each piece type on a legal cell chosen in scan order.
func buildLayoutProgrammatic(color Color) []Placement {
	mk := func(y int) int {
		if color == Red {
			return y
		}
		return 11 - y
	}
	type cellInfo struct {
		pos       Position
		isHQ      bool
		isRailRow int // 0 if not rail, else 1=back rail, 5=front rail
	}
	cells := []cellInfo{
		{pos: Position{X: 0, Y: mk(0)}, isHQ: true},
		{pos: Position{X: 4, Y: mk(0)}, isHQ: true},
		{pos: Position{X: 1, Y: mk(0)}}, {pos: Position{X: 2, Y: mk(0)}}, {pos: Position{X: 3, Y: mk(0)}},
		{pos: Position{X: 0, Y: mk(1)}, isRailRow: 1}, {pos: Position{X: 1, Y: mk(1)}, isRailRow: 1},
		{pos: Position{X: 2, Y: mk(1)}, isRailRow: 1}, {pos: Position{X: 3, Y: mk(1)}, isRailRow: 1},
		{pos: Position{X: 4, Y: mk(1)}, isRailRow: 1},
		{pos: Position{X: 0, Y: mk(2)}}, {pos: Position{X: 2, Y: mk(2)}}, {pos: Position{X: 4, Y: mk(2)}},
		{pos: Position{X: 0, Y: mk(3)}, isRailRow: 3}, {pos: Position{X: 1, Y: mk(3)}, isRailRow: 3},
		{pos: Position{X: 2, Y: mk(3)}, isRailRow: 3}, {pos: Position{X: 3, Y: mk(3)}, isRailRow: 3},
		{pos: Position{X: 4, Y: mk(3)}, isRailRow: 3},
		{pos: Position{X: 0, Y: mk(4)}}, {pos: Position{X: 2, Y: mk(4)}}, {pos: Position{X: 4, Y: mk(4)}},
		{pos: Position{X: 1, Y: mk(5)}, isRailRow: 5}, {pos: Position{X: 2, Y: mk(5)}, isRailRow: 5},
		{pos: Position{X: 3, Y: mk(5)}, isRailRow: 5}, {pos: Position{X: 4, Y: mk(5)}, isRailRow: 5},
	}
	// 25 cells total.
	if len(cells) != 25 {
		panic("internal: cells must be exactly 25")
	}

	frontRow := mk(5)
	backRows := [2]int{mk(0), mk(1)}

	// Define the 25 pieces with their constraints.
	type pieceSpec struct {
		t      PieceType
		constraint func(ci cellInfo) bool
	}
	specs := []pieceSpec{
		// HQ-bound: flag and commander go in HQs.
		{t: Flag, constraint: func(ci cellInfo) bool { return ci.isHQ && ci.pos.X == 0 }},
		{t: Commander, constraint: func(ci cellInfo) bool { return ci.isHQ && ci.pos.X == 4 }},
		// Mines must be in back two rows.
		{t: Mine, constraint: func(ci cellInfo) bool { return ci.pos.Y == backRows[0] || ci.pos.Y == backRows[1] }},
		{t: Mine, constraint: func(ci cellInfo) bool { return ci.pos.Y == backRows[0] || ci.pos.Y == backRows[1] }},
		{t: Mine, constraint: func(ci cellInfo) bool { return ci.pos.Y == backRows[0] || ci.pos.Y == backRows[1] }},
		// Bombs cannot be on front rail row.
		{t: Bomb, constraint: func(ci cellInfo) bool { return ci.pos.Y != frontRow }},
		{t: Bomb, constraint: func(ci cellInfo) bool { return ci.pos.Y != frontRow }},
	}
	// Remaining 18 pieces (general + 2 major + 2 colonel + 2 captain + 2 lieutenant + 6 sergeant + 3 engineer)
	// can go on any non-HQ non-camp cell (constraint: !isHQ — we already filtered HQ for flag/commander,
	// and the remaining cells are non-HQ non-camp by construction).
	noConstraint := func(ci cellInfo) bool { return !ci.isHQ }
	for _, t := range []PieceType{
		General,
		Major, Major,
		Colonel, Colonel,
		Captain, Captain,
		Lieutenant, Lieutenant,
		Sergeant, Sergeant, Sergeant, Sergeant, Sergeant, Sergeant,
		Engineer, Engineer, Engineer,
	} {
		specs = append(specs, pieceSpec{t: t, constraint: noConstraint})
	}
	if len(specs) != 25 {
		panic("internal: must have exactly 25 piece specs")
	}

	// Greedy assign each piece to the first cell matching its constraint.
	used := map[Position]bool{}
	pl := make([]Placement, 0, 25)
	for _, s := range specs {
		var chosen *cellInfo
		for i := range cells {
			ci := cells[i]
			if used[ci.pos] {
				continue
			}
			if s.constraint(ci) {
				chosen = &cells[i]
				break
			}
		}
		if chosen == nil {
			panic("internal: no valid cell for piece " + PieceTypeName(s.t))
		}
		used[chosen.pos] = true
		pl = append(pl, Placement{Type: s.t, At: chosen.pos})
	}
	if len(pl) != 25 {
		panic("internal: pl must have 25 pieces")
	}
	return pl
}

// ──────────── Layout tests ────────────

func TestValidateLayout_HappyPath(t *testing.T) {
	gs := NewGame()
	pl := standardLayout(Red)
	if e := gs.ApplyLayout(Red, pl); e != nil {
		t.Fatalf("expected valid red layout, got error: %v", e)
	}
	if !gs.LayoutDone[0] {
		t.Fatal("Red layout should be marked done")
	}
}

func TestValidateLayout_FlagNotInHQ(t *testing.T) {
	pl := standardLayout(Red)
	pl[0] = Placement{Type: Flag, At: Position{X: 2, Y: 1}}
	if e := ValidateLayout(Red, pl); e == nil {
		t.Fatal("expected error: flag not in HQ")
	}
}

func TestValidateLayout_WrongCount(t *testing.T) {
	pl := standardLayout(Red)[:24]
	if e := ValidateLayout(Red, pl); e == nil {
		t.Fatal("expected error: wrong piece count")
	}
}

func TestValidateLayout_MineInFrontRow(t *testing.T) {
	pl := standardLayout(Red)
	pl[2] = Placement{Type: Mine, At: Position{X: 2, Y: 5}}
	if e := ValidateLayout(Red, pl); e == nil {
		t.Fatal("expected error: mine in front row")
	}
}

func TestValidateLayout_BombInFrontRow(t *testing.T) {
	pl := standardLayout(Red)
	// put a bomb at the front rail row (y=5 for Red)
	for i := range pl {
		if pl[i].Type == Bomb {
			pl[i] = Placement{Type: Bomb, At: Position{X: 2, Y: 5}}
			break
		}
	}
	if e := ValidateLayout(Red, pl); e == nil {
		t.Fatal("expected error: bomb in front row")
	}
}

func TestValidateLayout_OnCamp(t *testing.T) {
	pl := standardLayout(Red)
	for i := range pl {
		if IsCamp(pl[i].At) {
			// already invalid; fail anyway
			t.Fatalf("helper placed a piece on camp at %v", pl[i].At)
		}
	}
	// now manually place one on a camp
	for i := range pl {
		if pl[i].At.Y != 0 {
			pl[i] = Placement{Type: pl[i].Type, At: Position{X: 1, Y: 2}}
			break
		}
	}
	if e := ValidateLayout(Red, pl); e == nil {
		t.Fatal("expected error: piece on camp")
	}
}

func TestValidateLayout_Overlap(t *testing.T) {
	pl := standardLayout(Red)
	pl[1] = pl[2]
	if e := ValidateLayout(Red, pl); e == nil {
		t.Fatal("expected error: pieces overlap")
	}
}

func TestValidateLayout_OutOfHalf(t *testing.T) {
	pl := standardLayout(Red)
	pl[1] = Placement{Type: pl[1].Type, At: Position{X: 2, Y: 8}}
	if e := ValidateLayout(Red, pl); e == nil {
		t.Fatal("expected error: piece in opponent half")
	}
}

// ──────────── Battle tests ────────────

func TestResolveBattle_HigherWins(t *testing.T) {
	a := &Piece{Color: Red, Type: Commander}
	d := &Piece{Color: Black, Type: Major}
	aSurv, dSurv, _, _ := resolveBattle(a, d)
	if !aSurv || dSurv {
		t.Fatal("commander should beat major")
	}
}

func TestResolveBattle_SameRankMutualDestruction(t *testing.T) {
	a := &Piece{Color: Red, Type: Major}
	d := &Piece{Color: Black, Type: Major}
	aSurv, dSurv, both, _ := resolveBattle(a, d)
	if aSurv || dSurv {
		t.Fatal("both should be destroyed")
	}
	if !both {
		t.Fatal("both_destroyed flag must be true")
	}
}

func TestResolveBattle_BombMutualDestruction(t *testing.T) {
	a := &Piece{Color: Red, Type: Bomb}
	d := &Piece{Color: Black, Type: Commander}
	aSurv, dSurv, both, _ := resolveBattle(a, d)
	if aSurv || dSurv {
		t.Fatal("bomb vs commander: both should die")
	}
	if !both {
		t.Fatal("both_destroyed must be true")
	}
}

func TestResolveBattle_MineKillsNonEngineer(t *testing.T) {
	a := &Piece{Color: Red, Type: General}
	d := &Piece{Color: Black, Type: Mine}
	aSurv, dSurv, _, _ := resolveBattle(a, d)
	if aSurv {
		t.Fatal("general should die attacking mine")
	}
	if !dSurv {
		t.Fatal("mine should survive")
	}
}

func TestResolveBattle_EngineerDefusesMine(t *testing.T) {
	a := &Piece{Color: Red, Type: Engineer}
	d := &Piece{Color: Black, Type: Mine}
	aSurv, dSurv, _, defused := resolveBattle(a, d)
	if !aSurv {
		t.Fatal("engineer should survive")
	}
	if dSurv {
		t.Fatal("mine should be removed")
	}
	if !defused {
		t.Fatal("defused flag should be true")
	}
}

func TestResolveBattle_FlagCapture(t *testing.T) {
	a := &Piece{Color: Red, Type: Colonel}
	d := &Piece{Color: Black, Type: Flag}
	aSurv, dSurv, _, _ := resolveBattle(a, d)
	if !aSurv {
		t.Fatal("attacker should win when capturing flag")
	}
	if dSurv {
		t.Fatal("flag should be captured")
	}
}

// ──────────── Move validation tests ────────────

func TestValidateMove_FlagCannotMove(t *testing.T) {
	gs := NewGame()
	gs.ApplyLayout(Red, standardLayout(Red))
	gs.ApplyLayout(Black, standardLayout(Black))
	gs.Phase = PhasePlaying

	var flagPos Position
	for y := 0; y <= 5; y++ {
		for x := 0; x <= 4; x++ {
			p := gs.Get(Position{X: x, Y: y})
			if p != nil && p.Type == Flag && p.Color == Red {
				flagPos = Position{X: x, Y: y}
			}
		}
	}
	if flagPos.X == 0 && flagPos.Y == 0 {
		// flag may be at (0,0) which is valid HQ
		flagPos = Position{X: 0, Y: 0}
	}
	if e := ValidateMove(gs, flagPos, Position{X: 2, Y: 1}, Red); e == nil {
		t.Fatal("flag should not be movable")
	}
}

func TestValidateMove_WrongTurn(t *testing.T) {
	gs := NewGame()
	gs.ApplyLayout(Red, standardLayout(Red))
	gs.ApplyLayout(Black, standardLayout(Black))
	gs.Phase = PhasePlaying
	gs.Turn = Red

	// find a movable black piece (not flag, not mine, not in HQ)
	// Scan all black half rows including HQ row but skip HQs.
	var piecePos Position
found:
	for y := 6; y <= 11; y++ {
		for x := 0; x <= 4; x++ {
			cell := Position{X: x, Y: y}
			if IsHQ(cell) {
				continue
			}
			p := gs.Get(cell)
			if p == nil || p.Color != Black {
				continue
			}
			if p.Type == Flag || p.Type == Mine {
				continue
			}
			piecePos = cell
			break found
		}
	}
	if piecePos.X == 0 && piecePos.Y == 0 {
		// Debug: print all black pieces
		t.Logf("gs.String():\n%s", gs.String())
		t.Fatal("no movable black piece")
	}
	e := ValidateMove(gs, piecePos, Position{X: piecePos.X, Y: piecePos.Y - 1}, Black)
	if e == nil || e.Code != errcode.ErrNotYourTurn {
		t.Fatalf("expected ErrNotYourTurn, got %v", e)
	}
}

func TestValidateMove_EngineerFliesRail(t *testing.T) {
	gs := NewGame()
	gs.Board = [12][5]*Piece{}
	// rail rows are y=1, 3, 5 (red) and y=6, 8, 10 (black)
	gs.Board[1][0] = &Piece{Color: Red, Type: Engineer}
	gs.Board[10][4] = &Piece{Color: Black, Type: General}
	gs.LayoutDone[0] = true
	gs.LayoutDone[1] = true
	gs.Phase = PhasePlaying
	gs.Turn = Red

	if e := ValidateMove(gs, Position{X: 0, Y: 1}, Position{X: 4, Y: 1}, Red); e != nil {
		t.Fatalf("engineer should fly rail: %v", e)
	}
}

// ──────────── Visibility tests ────────────

func TestBoardView_OpenMode(t *testing.T) {
	gs := NewGame()
	gs.Board[2][0] = &Piece{Color: Red, Type: Engineer}
	gs.Board[8][4] = &Piece{Color: Black, Type: General}
	view := BoardView(gs, Red, ModeOpen)
	if view[2][0].Color != "red" || view[8][4].Color != "black" {
		t.Fatal("open mode should reveal all pieces")
	}
}

func TestBoardView_HiddenMode(t *testing.T) {
	gs := NewGame()
	gs.Board[2][0] = &Piece{Color: Red, Type: Engineer}
	gs.Board[8][4] = &Piece{Color: Black, Type: General}
	view := BoardView(gs, Red, ModeHidden)
	if view[2][0].Color != "red" {
		t.Fatal("own piece must be visible in hidden mode")
	}
	if !view[8][4].Hidden {
		t.Fatal("opponent unknown piece must be hidden")
	}
}

func TestBoardView_HiddenRevealsAfterBattle(t *testing.T) {
	gs := NewGame()
	gs.Board[2][0] = &Piece{Color: Red, Type: Engineer}
	gs.Board[8][4] = &Piece{Color: Black, Type: General}
	gs.MoveHistory = []Move{{
		From:          Position{X: 0, Y: 2},
		To:            Position{X: 4, Y: 8},
		Piece:         Piece{Color: Red, Type: Engineer},
		RevealedPiece: &Piece{Color: Black, Type: General},
	}}
	view := BoardView(gs, Red, ModeHidden)
	if !view[8][4].Revealed {
		t.Fatal("opponent piece must be revealed after battle")
	}
}

func TestBoardView_FlagRevealedAfterCommanderDeath(t *testing.T) {
	gs := NewGame()
	gs.FlagRevealed[1] = true // black's commander died
	gs.Board[11][0] = &Piece{Color: Black, Type: Flag}
	view := BoardView(gs, Red, ModeHidden)
	if !view[11][0].Revealed || view[11][0].Type != "flag" {
		t.Fatal("flag should be revealed after opponent commander dies")
	}
}

// ──────────── Game-over tests ────────────

func TestExecuteMove_CapturesFlagEndsGame(t *testing.T) {
	gs := NewGame()
	gs.Board[5][2] = &Piece{Color: Red, Type: General}
	gs.Board[6][2] = &Piece{Color: Black, Type: Flag}
	gs.LayoutDone[0] = true
	gs.LayoutDone[1] = true
	gs.Phase = PhasePlaying
	gs.Turn = Red

	move := gs.ExecuteMove(Position{X: 2, Y: 5}, Position{X: 2, Y: 6})
	if move.Captured == nil || move.Captured.Type != Flag {
		t.Fatal("general should capture flag")
	}
	if gs.Status != StatusRedWin {
		t.Fatalf("expected red_win, got %v", gs.Status)
	}
}

func TestExecuteMove_BombMutualDestruction(t *testing.T) {
	gs := NewGame()
	gs.Board[2][0] = &Piece{Color: Red, Type: Bomb}
	gs.Board[2][4] = &Piece{Color: Black, Type: General}
	gs.LayoutDone[0] = true
	gs.LayoutDone[1] = true
	gs.Phase = PhasePlaying
	gs.Turn = Red

	move := gs.ExecuteMove(Position{X: 0, Y: 2}, Position{X: 4, Y: 2})
	if !move.BothDestroyed {
		t.Fatal("both should be destroyed")
	}
	if gs.Get(Position{X: 4, Y: 2}) != nil {
		t.Fatal("cell should be empty after mutual destruction")
	}
}

func TestExecuteMove_CommanderDeathRevealsFlag(t *testing.T) {
	gs := NewGame()
	gs.Board[2][0] = &Piece{Color: Red, Type: Bomb}
	gs.Board[2][1] = &Piece{Color: Black, Type: Commander}
	gs.Board[11][0] = &Piece{Color: Black, Type: Flag}
	gs.LayoutDone[0] = true
	gs.LayoutDone[1] = true
	gs.Phase = PhasePlaying
	gs.Turn = Red

	gs.ExecuteMove(Position{X: 0, Y: 2}, Position{X: 1, Y: 2})
	if !gs.FlagRevealed[1] {
		t.Fatal("commander death should reveal black's flag")
	}
}

// ──────────── Total count test ────────────

func TestTotalPiecesPerSide(t *testing.T) {
	if got := TotalPiecesPerSide(); got != 25 {
		t.Fatalf("total pieces must be 25, got %d", got)
	}
}