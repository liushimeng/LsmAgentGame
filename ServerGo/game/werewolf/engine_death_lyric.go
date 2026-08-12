package werewolf

import (
	"LsmAgentGame/config"
	"LsmAgentGame/errcode"
)

func DeathLyricDeadlineSeconds() int {
	defer func() { _ = recover() }()
	if n := config.Load().Werewolf.DeathLyricDeadlineSec; n >= 5 {
		return n
	}
	return DeathLyricDefaultDeadlineSec
}

func isDeathLyricEnabled() bool {
	defer func() { _ = recover() }()
	c := config.Load()
	if c == nil {
		return true
	}
	return c.Werewolf.DeathLyricEnabled
}

func filterLastWords(gs *GameState, seats []Seat) []Seat {
	out := make([]Seat, 0, len(seats))
	seen := make(map[Seat]bool, len(seats))
	for _, s := range seats {
		if s < 0 || s >= MaxPlayers || seen[s] {
			continue
		}
		seen[s] = true
		if !gs.Players[s].LastWords {
			continue
		}
		out = append(out, s)
	}
	// 升序:冒泡简化(MaxPlayers=7,简单即可)
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func (gs *GameState) StartDeathLyricRound(seats []Seat, onDone func() *errcode.Error) *errcode.Error {
	if gs.Status == "over" {
		return ErrDeathLyricSkip
	}
	if !isDeathLyricEnabled() {
		return ErrDeathLyricSkip
	}
	lw := filterLastWords(gs, seats)
	if len(lw) == 0 {
		return ErrDeathLyricSkip
	}
	setPhaseAndDeadline(gs, PhaseDeathLyric)
	gs.DeathLyricQueue = lw
	gs.DeathLyricDone = make(map[Seat]bool)
	gs.DeathLyricCurrent = lw[0]
	gs.DeathLyricOnDone = onDone
	return nil
}

func (gs *GameState) tryEnterDeathLyricRound(seats []Seat, onDone func() *errcode.Error) *errcode.Error {
	if err := gs.StartDeathLyricRound(seats, onDone); err != nil {
		if err == ErrDeathLyricSkip {
			return onDone()
		}
		return err
	}
	return nil
}

func (gs *GameState) SayLastWords(seat Seat, text string) *errcode.Error {
	if gs.Phase != PhaseDeathLyric {
		return errcode.CodeMsg(errcode.ErrNotYourTurn, "not death_lyric phase")
	}
	if gs.DeathLyricCurrent != seat {
		return errcode.CodeMsg(errcode.ErrNotYourTurn, "not your last-words turn")
	}
	if text == "" {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "empty last words")
	}
	gs.Players[seat].LastWords = false // 已消费
	gs.DeathLyricDone[seat] = true
	return gs.popDeathLyricQueue()
}

func (gs *GameState) SkipLastWords(seat Seat) *errcode.Error {
	if gs.Phase != PhaseDeathLyric {
		return errcode.CodeMsg(errcode.ErrNotYourTurn, "not death_lyric phase")
	}
	if gs.DeathLyricCurrent != seat {
		return errcode.CodeMsg(errcode.ErrNotYourTurn, "not your last-words turn")
	}
	gs.Players[seat].LastWords = false
	gs.DeathLyricDone[seat] = true
	return gs.popDeathLyricQueue()
}

func (gs *GameState) popDeathLyricQueue() *errcode.Error {
	if len(gs.DeathLyricQueue) <= 1 {
		return gs.EndDeathLyricRound()
	}
	gs.DeathLyricQueue = gs.DeathLyricQueue[1:]
	gs.DeathLyricCurrent = gs.DeathLyricQueue[0]
	return nil
}

func (gs *GameState) EndDeathLyricRound() *errcode.Error {
	gs.DeathLyricQueue = nil
	gs.DeathLyricCurrent = NoSeat
	gs.DeathLyricDone = nil
	onDone := gs.DeathLyricOnDone
	gs.DeathLyricOnDone = nil
	if onDone != nil {
		return onDone()
	}
	return nil
}

