package junqi

// VisibilityMode controls how much of the opponent's pieces a player can see.
type VisibilityMode int

const (
	// ModeOpen: 明棋 — both sides see all pieces.
	ModeOpen VisibilityMode = 0
	// ModeHidden: 暗棋 — opponent's pieces are hidden until revealed.
	ModeHidden VisibilityMode = 1
)

// String returns the mode name.
func (m VisibilityMode) String() string {
	if m == ModeOpen {
		return "open"
	}
	return "hidden"
}

// ParseVisibilityMode parses a mode string.
func ParseVisibilityMode(s string) VisibilityMode {
	if s == "open" {
		return ModeOpen
	}
	return ModeHidden
}

// ClientPieceView is the per-player view of a cell.
//
// JSON tags:
//   - In open mode:  Color, Type, Name are always set
//   - In hidden mode for opponent cells: Hidden=true, others omitted
//   - In hidden mode after battle:    Revealed=true, full piece info set
type ClientPieceView struct {
	Color    string `json:"color,omitempty"`
	Type     string `json:"type,omitempty"`
	Name     string `json:"name,omitempty"`
	Hidden   bool   `json:"hidden,omitempty"`
	Revealed bool   `json:"revealed,omitempty"`
}

// IsEmpty reports whether the cell is empty.
func (v ClientPieceView) IsEmpty() bool {
	return v.Color == "" && v.Type == "" && !v.Hidden && !v.Revealed
}

// EmptyView returns an empty cell view.
func EmptyView() ClientPieceView {
	return ClientPieceView{}
}

// HiddenView returns a hidden (unknown) piece view.
func HiddenView() ClientPieceView {
	return ClientPieceView{Hidden: true}
}

// FullView returns a fully-visible piece view.
func FullView(p *Piece) ClientPieceView {
	if p == nil {
		return ClientPieceView{}
	}
	return ClientPieceView{
		Color: p.Color.String(),
		Type:  PieceTypeName(p.Type),
		Name:  p.Name(),
	}
}

// BoardView builds a 12×5 view of the board from the perspective of `viewer`.
//
// In ModeOpen, returns the full board.
//
// In ModeHidden:
//   - Cells in the viewer's own half: fully visible
//   - Cells in the opponent's half that have never been observed: hidden
//   - Cells where a battle happened: revealed (full info) for both sides
//   - When a Commander dies, the corresponding side's flag is revealed
func BoardView(gs *GameState, viewer Color, mode VisibilityMode) [12][5]ClientPieceView {
	var view [12][5]ClientPieceView

	if mode == ModeOpen {
		for y := 0; y <= 11; y++ {
			for x := 0; x <= 4; x++ {
				view[y][x] = FullView(gs.Board[y][x])
			}
		}
		return view
	}

	// Hidden mode.
	for y := 0; y <= 11; y++ {
		for x := 0; x <= 4; x++ {
			pos := Position{X: x, Y: y}
			p := gs.Board[y][x]
			if p == nil {
				view[y][x] = EmptyView()
				continue
			}
			// Own half = always fully visible.
			if p.Color == viewer {
				view[y][x] = FullView(p)
				continue
			}
			// Opponent piece — check if revealed.
			if isRevealed(gs, pos, viewer) {
				view[y][x] = FullView(p)
				view[y][x].Revealed = true
				continue
			}
			// Hidden.
			view[y][x] = HiddenView()
		}
	}
	return view
}

// isRevealed reports whether the viewer knows what's at `pos`.
// A position is revealed if:
//   - There was a battle at this cell
//   - An Engineer defused a mine here
//   - The opponent's Commander has died (and so their flag is publicly known)
//   - The opponent moved through this cell (e.g. exited a camp)
func isRevealed(gs *GameState, pos Position, viewer Color) bool {
	for _, m := range gs.MoveHistory {
		if m.From == pos || m.To == pos {
			if m.RevealedPiece != nil {
				return true
			}
			if m.EngineerDefused {
				return true
			}
		}
	}
	// If the OPPONENT's commander has died, that opponent's flag is publicly known.
	if gs.FlagRevealed[1-viewerIdx(viewer)] {
		hqs := HQsForColor(oppositeOf(viewer))
		for _, hq := range hqs {
			if pos == hq {
				return true
			}
		}
	}
	return false
}

func viewerIdx(c Color) int {
	if c == Red {
		return 0
	}
	return 1
}

func oppositeOf(c Color) Color {
	return c.Opposite()
}