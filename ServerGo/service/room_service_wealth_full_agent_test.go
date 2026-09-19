package service

import (
	"testing"

	"LsmAgentGame/errcode"
)

type wealthOrderRecorder struct {
	events []string
}

type wealthOrderJoiner struct {
	GameJoiner
	rec *wealthOrderRecorder
}

func (f *wealthOrderJoiner) SetFullAgentMode(gameKind, roomID string, enabled bool) *errcode.Error {
	f.rec.events = append(f.rec.events, "full_agent:"+gameKind)
	return nil
}

type wealthOrderSeater struct {
	AgentSeater
	rec      *wealthOrderRecorder
	gameKind string
	seats    []AgentSeatConfig
}

func (f *wealthOrderSeater) RegisterAgentSeats(gameKind, roomID string, seats []AgentSeatConfig) *errcode.Error {
	f.rec.events = append(f.rec.events, "register:"+gameKind)
	f.gameKind = gameKind
	f.seats = seats
	return nil
}

func TestCreateRoomWealth_TenOrElevenBotsAreFullAgentBeforeRegistration(t *testing.T) {
	for _, count := range []int{10, 11} {
		t.Run(map[int]string{10: "10", 11: "11"}[count], func(t *testing.T) {
			rec := &wealthOrderRecorder{}
			seater := &wealthOrderSeater{rec: rec}
			joiner := &wealthOrderJoiner{rec: rec}
			s := &RoomService{agentSeater: seater, gameJoiner: joiner}
			seats := make([]AgentSeatConfig, count)
			for i := range seats {
				seats[i] = AgentSeatConfig{Seat: i, ModelKey: "test-model"}
			}

			s.prepareWealthAgentRoom("room-wealth-order", seats)

			if len(rec.events) != 2 {
				t.Fatalf("events = %v, want full-agent then register", rec.events)
			}
			if rec.events[0] != "full_agent:wealth" || rec.events[1] != "register:wealth" {
				t.Fatalf("order = %v, want SetFullAgentMode before RegisterAgentSeats", rec.events)
			}
			if seater.gameKind != "wealth" || len(seater.seats) != count {
				t.Fatalf("registered game/seats = %q/%d, want wealth/%d", seater.gameKind, len(seater.seats), count)
			}
		})
	}
}

func TestCreatorShouldBeSpectator_WealthTenOrElevenBots(t *testing.T) {
	cases := []struct {
		name           string
		gameKind       string
		freeSeatCount  int
		agentSeatCount int
		want           bool
	}{
		{"wealth 9 bots keeps creator player", "wealth", 3, 9, false},
		{"wealth 10 bots downgrades creator", "wealth", 2, 10, true},
		{"wealth 11 bots downgrades creator", "wealth", 1, 11, true},
		{"wealth 12 bots has no physical seat", "wealth", 0, 12, true},
		{"texas 10 agents still has creator player seat", "texasholdem", 2, 10, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := creatorShouldBeSpectator(tc.gameKind, tc.freeSeatCount, tc.agentSeatCount); got != tc.want {
				t.Fatalf("creatorShouldBeSpectator(%q, free=%d, agents=%d) = %v, want %v",
					tc.gameKind, tc.freeSeatCount, tc.agentSeatCount, got, tc.want)
			}
		})
	}
}
