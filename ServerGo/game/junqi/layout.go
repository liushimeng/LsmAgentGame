package junqi

import (
	"LsmAgentGame/errcode"
)

// RequiredPieceCounts is the canonical composition of one side's 25-piece army.
// Source: 中国军棋规则.md §三.
//
// We consolidate 连长 (sergeant, rank 3) and 排长 (corporal, rank 2) into a single
// "Sergeant" type with combined count 6, because from a battle-rank standpoint
// they are adjacent and have no special-casing differences. This keeps the
// PieceType enum tractable while preserving the 25-piece total.
var RequiredPieceCounts = map[PieceType]int{
	Flag:       1,
	Commander:  1,
	General:    1,
	Major:      2,
	Colonel:    2,
	Captain:    2,
	Lieutenant: 2,
	Sergeant:   6, // 连长 3 + 排长 3
	Engineer:   3,
	Bomb:       2,
	Mine:       3,
}

// TotalPiecesPerSide returns the canonical 25.
func TotalPiecesPerSide() int {
	total := 0
	for _, n := range RequiredPieceCounts {
		total += n
	}
	return total
}

// ValidateLayout checks that a proposed 25-piece layout obeys the rules:
//
//   1. Exactly TotalPiecesPerSide() pieces, with the correct count per type
//   2. Flag must be in one of the player's two HQs
//   3. Mines may only occupy the back two rows' cells (兵站 + HQ)
//   4. Bombs may not be in the player's front row (closest to opponent)
//   5. No piece may be placed on a camp (行营)
//   6. No two pieces may share the same cell
//
// The placement list comes directly from the client's drag-and-drop board.
func ValidateLayout(color Color, placements []Placement) *errcode.Error {
	// (1) total count + per-type count
	if len(placements) != TotalPiecesPerSide() {
		return errcode.CodeMsg(errcode.ErrValidationFailed,
			"layout must contain exactly 25 pieces, got "+itoa(len(placements)))
	}
	counts := map[PieceType]int{}
	for _, p := range placements {
		counts[p.Type]++
	}
	for t, want := range RequiredPieceCounts {
		if counts[t] != want {
			return errcode.CodeMsg(errcode.ErrValidationFailed,
				"piece count mismatch for "+PieceTypeName(t)+": want "+itoa(want)+", got "+itoa(counts[t]))
		}
	}

	// (2) Flag must be in one of the player's two HQs.
	hqs := HQsForColor(color)
	flagInHQ := false
	for _, p := range placements {
		if p.Type != Flag {
			continue
		}
		for _, hq := range hqs {
			if p.At == hq {
				flagInHQ = true
				break
			}
		}
	}
	if !flagInHQ {
		return errcode.CodeMsg(errcode.ErrValidationFailed,
			"flag must be placed in one of the two headquarters")
	}

	// (3) Mines only in back two rows.
	backRows := BackTwoRows(color)
	for _, p := range placements {
		if p.Type != Mine {
			continue
		}
		if p.At.Y != backRows[0] && p.At.Y != backRows[1] {
			return errcode.CodeMsg(errcode.ErrValidationFailed,
				"mines may only be placed in the back two rows")
		}
	}

	// (4) Bombs not in the front row.
	frontRow := FrontRowIdx(color)
	for _, p := range placements {
		if p.Type != Bomb {
			continue
		}
		if p.At.Y == frontRow {
			return errcode.CodeMsg(errcode.ErrValidationFailed,
				"bombs may not be placed in the front row")
		}
	}

	// (5) No piece may be placed on a camp (行营).
	for _, p := range placements {
		if IsCamp(p.At) {
			return errcode.CodeMsg(errcode.ErrValidationFailed,
				"pieces may not be placed on camps (行营)")
		}
	}

	// (6) No overlaps, all cells within own half.
	seen := map[Position]PieceType{}
	for _, p := range placements {
		if !p.At.IsValid() {
			return errcode.CodeMsg(errcode.ErrValidationFailed,
				"position out of board: "+itoa(p.At.X)+","+itoa(p.At.Y))
		}
		if !InHomeArea(p.At, color) {
			return errcode.CodeMsg(errcode.ErrValidationFailed,
				"position outside own half: "+itoa(p.At.X)+","+itoa(p.At.Y))
		}
		if other, ok := seen[p.At]; ok {
			return errcode.CodeMsg(errcode.ErrValidationFailed,
				"two pieces at "+itoa(p.At.X)+","+itoa(p.At.Y)+" ("+PieceTypeName(other)+" and "+PieceTypeName(p.Type)+")")
		}
		seen[p.At] = p.Type
	}

	return nil
}

// ApplyLayout writes the validated placements into the board, replacing any
// previous layout for that color. Returns an error if validation fails.
func (gs *GameState) ApplyLayout(color Color, placements []Placement) *errcode.Error {
	if e := ValidateLayout(color, placements); e != nil {
		return e
	}
	// Wipe existing pieces on this side (in case of re-layout).
	for y := 0; y <= 11; y++ {
		for x := 0; x <= 4; x++ {
			p := gs.Board[y][x]
			if p != nil && p.Color == color {
				gs.Board[y][x] = nil
			}
		}
	}
	// Place new pieces.
	for _, pl := range placements {
		gs.Board[pl.At.Y][pl.At.X] = &Piece{Color: color, Type: pl.Type}
	}
	gs.LayoutDone[colorIndex(color)] = true
	return nil
}

// colorIndex converts a color to the 0/1 array index used in GameState.
func colorIndex(c Color) int {
	if c == Red {
		return 0
	}
	return 1
}

// itoa is a tiny local itoa to avoid pulling in strconv just for error messages.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}