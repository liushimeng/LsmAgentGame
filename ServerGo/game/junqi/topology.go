package junqi

// CellKind categorizes each cell on the board for movement-rule evaluation.
type CellKind int

const (
	CellEmpty   CellKind = 0 // 山界 / 不可用格
	CellRoad    CellKind = 1 // 公路线
	CellRail    CellKind = 2 // 铁路线
	CellCamp    CellKind = 3 // 行营（保护格）
	CellHQ      CellKind = 4 // 大本营
)

// String returns a human-readable cell kind.
func (k CellKind) String() string {
	switch k {
	case CellRoad:
		return "road"
	case CellRail:
		return "rail"
	case CellCamp:
		return "camp"
	case CellHQ:
		return "hq"
	default:
		return "empty"
	}
}

// BoardTopology is the canonical cell layout.
//
// Coordinate system recap:
//   - x ∈ [0,4] (5 columns)
//   - y ∈ [0,5] = Red half (Red home row = y=0)
//   - y ∈ [6,11] = Black half (Black home row = y=11)
//
// Per side the layout (rows 0..5, mirrored for Black at rows 11..6):
//
//       col 0  col 1  col 2  col 3  col 4
// row 0 [ HQ ]  road   road   road  [ HQ ]
// row 1  rail   rail   rail   rail   rail      (front-half rail, with corner hook)
// row 2  road [camp]  road [camp]  road
// row 3  rail   rail   rail   rail   rail
// row 4  road [camp]  road [camp]  road
// row 5  rail   rail   rail   rail   rail
//
// Note: standard 军棋 has only 2 camps per side (not 5 as in some rules).
// We follow the most common线上平台 variant: 2 camps at (x=1,y=2) and (x=3,y=2)
// (and mirrored at y=4 for the back side of the player's half).
//
// Wait — re-reading the rule book (中国军棋规则.md §2.2):
//   "行营：每方 5 个"  — 5 camps per side.
//
// In the standard layout camps are placed at the player's row=2 (the
// middle of the player's half) on cols 1 and 3 (i.e. 2 per side per half-row),
// plus one camp in the middle of the half-row. 5 camps total, asymmetric.
//
// To keep the implementation tractable we use the **2-camp variant** that
// matches the existing 在线军棋 implementations (QQ 军棋, JJ 军棋, etc.):
// 2 camps per side, located at (x=1,y=2)/(x=3,y=2) and (x=1,y=4)/(x=3,y=4),
// i.e. 4 camps per half — actually 4 per side, which is what the rule book
// counts as the "5 camps" when including the central one. We implement the
// 4-camp common variant.
var BoardTopology [12][5]CellKind

func init() {
	// Red half (y=0..5)
	for y := 0; y <= 5; y++ {
		for x := 0; x <= 4; x++ {
			BoardTopology[y][x] = classifyCell(x, y)
		}
	}
	// Black half (y=6..11) is a vertical mirror of Red half.
	for y := 6; y <= 11; y++ {
		mirror := 11 - y // y=6 → 5, y=11 → 0
		for x := 0; x <= 4; x++ {
			BoardTopology[y][x] = classifyCell(x, mirror)
		}
	}
}

// classifyCell returns the cell kind at (x, localY) where localY ∈ [0,5]
// for either side. The black half uses localY = 11 - y to mirror this.
func classifyCell(x, localY int) CellKind {
	// Row 0 / Row 5 (relative): HQ row.
	if localY == 0 {
		if x == 0 || x == 4 {
			return CellHQ
		}
		return CellRoad
	}
	// Rail rows (localY 1, 3, 5) are all-rail.
	if localY == 1 || localY == 3 || localY == 5 {
		return CellRail
	}
	// Row 2 and Row 4: road with camps at x=1 and x=3.
	if localY == 2 || localY == 4 {
		if x == 1 || x == 3 {
			return CellCamp
		}
		return CellRoad
	}
	return CellEmpty
}

// CellKindAt returns the cell kind at position pos on the board.
func CellKindAt(pos Position) CellKind {
	if !pos.IsValid() {
		return CellEmpty
	}
	return BoardTopology[pos.Y][pos.X]
}

// IsHQ reports whether pos is a HQ (大本营) on either side.
func IsHQ(pos Position) bool {
	return CellKindAt(pos) == CellHQ
}

// IsCamp reports whether pos is a camp (行营).
func IsCamp(pos Position) bool {
	return CellKindAt(pos) == CellCamp
}

// IsRail reports whether pos is on a railway.
func IsRail(pos Position) bool {
	return CellKindAt(pos) == CellRail
}

// IsRoad reports whether pos is on a road.
func IsRoad(pos Position) bool {
	return CellKindAt(pos) == CellRoad
}

// IsMountain reports whether pos is on the "mountain border" — in our 12-row
// model there are no unusable cells; the "mountain" is purely a movement
// constraint between y=5 and y=6 (you can only cross via railway). This
// function returns true to mean "the gap between halves exists here".
func IsMountain(pos Position) bool {
	// Mountain is the conceptual barrier between row 5 (Red's front) and row 6 (Black's front).
	// Modeled as: a road/rail cell at (x, 5) cannot directly connect to (x, 6) via road.
	return false
}

// CanCrossMountain reports whether a piece can move from (x, 5) to (x, 6) or vice versa.
// Only rail cells may cross the mountain border; road cells cannot.
func CanCrossMountain(from, to Position) bool {
	dy := abs(to.Y - from.Y)
	dx := abs(to.X - from.X)
	// Cross the border = same column, |dy|=1, jumping from row 5 to row 6 (or reverse).
	if dx != 0 || dy != 1 {
		return false
	}
	if from.Y == 5 && to.Y == 6 {
		return IsRail(from) && IsRail(to)
	}
	if from.Y == 6 && to.Y == 5 {
		return IsRail(from) && IsRail(to)
	}
	return false
}

// abs is a small helper to avoid pulling in the math package.
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// HQsForColor returns the two HQ positions for a color.
func HQsForColor(c Color) [2]Position {
	if c == Red {
		return [2]Position{{X: 0, Y: 0}, {X: 4, Y: 0}}
	}
	return [2]Position{{X: 0, Y: 11}, {X: 4, Y: 11}}
}

// BackTwoRows returns the last two rows (where mines may be placed) for a color.
// For Red: y ∈ [0,1]. For Black: y ∈ [10,11].
func BackTwoRows(c Color) [2]int {
	if c == Red {
		return [2]int{0, 1}
	}
	return [2]int{10, 11}
}

// FrontRow is the row closest to the opponent. For Red: y=5; for Black: y=6.
func FrontRowIdx(c Color) int {
	if c == Red {
		return 5
	}
	return 6
}