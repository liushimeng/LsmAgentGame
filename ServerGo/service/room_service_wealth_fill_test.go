// Package service — room_service_wealth_fill_test.go: 焦点居民自动填充单测
// (2026-09-22 §CityHuman重构,前端联调)。
//
// 覆盖:agent_seats=3 填充至 MinSeats(10)、原有座位/职业偏好保留、
// ≥MinSeats 不填、0(人类可入座房)不填、座位号升序补空闲位。
package service

import "testing"

func seatSetOf(seats []AgentSeatConfig) map[int]struct{} {
	m := make(map[int]struct{}, len(seats))
	for _, a := range seats {
		m[a.Seat] = struct{}{}
	}
	return m
}

// TestPadWealthAgentSeats_FillToMin agent_seats=3 → 填充至 10(池驱动 ModelKey="")。
func TestPadWealthAgentSeats_FillToMin(t *testing.T) {
	in := []AgentSeatConfig{
		{Seat: 1, ModelKey: "", Profession: "P03"},
		{Seat: 5, ModelKey: "DouBao-model"},
		{Seat: 9, ModelKey: ""},
	}
	out := padWealthAgentSeats(in, seatSetOf(in))
	if len(out) != wealthMinAgentSeats {
		t.Fatalf("padded seats: got %d, want %d", len(out), wealthMinAgentSeats)
	}
	// 原座位保留(模型 key / 职业偏好不覆盖)。
	bySeat := map[int]AgentSeatConfig{}
	for _, a := range out {
		bySeat[a.Seat] = a
	}
	if bySeat[1].Profession != "P03" {
		t.Errorf("seat 1 profession lost: %+v", bySeat[1])
	}
	if bySeat[5].ModelKey != "DouBao-model" {
		t.Errorf("seat 5 explicit model overwritten: %+v", bySeat[5])
	}
	// 填充座位 = 升序空闲位 0,2,3,4,6,7,8,且全部池驱动。
	wantFill := []int{0, 2, 3, 4, 6, 7, 8}
	for _, s := range wantFill {
		a, ok := bySeat[s]
		if !ok {
			t.Fatalf("seat %d not filled", s)
		}
		if a.ModelKey != "" {
			t.Errorf("filled seat %d must be pool-driven (ModelKey=\"\"), got %q", s, a.ModelKey)
		}
	}
	// 填充后调用方判定:满 MinSeats → creatorShouldBeSpectator 为真(全 Agent)。
	if !creatorShouldBeSpectator("wealth", 0, len(out)) {
		t.Error("after padding, wealth room must be spectator-creator (full agent)")
	}
}

// TestPadWealthAgentSeats_NoFillCases 不填充场景:≥MinSeats / 空(人类可入座房)。
func TestPadWealthAgentSeats_NoFillCases(t *testing.T) {
	// 空:人类可入座房旧语义不变。
	if got := padWealthAgentSeats(nil, map[int]struct{}{}); len(got) != 0 {
		t.Errorf("empty agent_seats must stay empty, got %d", len(got))
	}
	// 已满 12:不填。
	full := make([]AgentSeatConfig, wealthMaxAgentSeats)
	for i := range full {
		full[i] = AgentSeatConfig{Seat: i}
	}
	if got := padWealthAgentSeats(full, seatSetOf(full)); len(got) != wealthMaxAgentSeats {
		t.Errorf("full room must not grow: got %d", len(got))
	}
	// 恰好 10:不填。
	ten := full[:wealthMinAgentSeats]
	if got := padWealthAgentSeats(ten, seatSetOf(ten)); len(got) != wealthMinAgentSeats {
		t.Errorf("exactly MinSeats must stay: got %d", len(got))
	}
}
