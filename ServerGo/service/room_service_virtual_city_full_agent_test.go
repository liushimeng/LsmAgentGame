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

func TestCreateRoomVirtualCity_TenOrElevenBotsAreFullAgentBeforeRegistration(t *testing.T) {
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

			s.prepareVirtualCityAgentRoom("room-virtual-city-order", seats, true)

			if len(rec.events) != 2 {
				t.Fatalf("events = %v, want full-agent then register", rec.events)
			}
			if rec.events[0] != "full_agent:virtual_city" || rec.events[1] != "register:virtual_city" {
				t.Fatalf("order = %v, want SetFullAgentMode before RegisterAgentSeats", rec.events)
			}
			if seater.gameKind != "virtual_city" || len(seater.seats) != count {
				t.Fatalf("registered game/seats = %q/%d, want wealth/%d", seater.gameKind, len(seater.seats), count)
			}
		})
	}
}

// TestCreatorShouldBeSpectator_VirtualCityFullAgent 2026-09-26 §批次25:
// wealth 判定从「座位数阈值」改为「全 Agent 开关」—— 居民-Agent 统一后
// 10/11 座位是常态,全 Agent 语义由 full_agent(缺省 true)承载。
func TestCreatorShouldBeSpectator_VirtualCityFullAgent(t *testing.T) {
	cases := []struct {
		name                 string
		gameKind             string
		freeSeatCount        int
		virtualCityFullAgent bool
		want                 bool
	}{
		{"wealth full agent 10 seats downgrades creator", "virtual_city", 2, true, true},
		{"wealth full agent 11 seats downgrades creator", "virtual_city", 1, true, true},
		{"wealth full agent 12 seats downgrades creator", "virtual_city", 0, true, true},
		{"wealth full_agent=false with free seat keeps creator player", "virtual_city", 2, false, false},
		{"texas with free seats keeps creator player seat", "texasholdem", 2, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := creatorShouldBeSpectator(tc.gameKind, tc.freeSeatCount, tc.virtualCityFullAgent); got != tc.want {
				t.Fatalf("creatorShouldBeSpectator(%q, free=%d, fullAgent=%v) = %v, want %v",
					tc.gameKind, tc.freeSeatCount, tc.virtualCityFullAgent, got, tc.want)
			}
		})
	}
}
