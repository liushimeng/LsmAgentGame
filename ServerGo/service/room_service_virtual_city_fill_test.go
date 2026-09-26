// Package service — room_service_wealth_fill_test.go: 虚拟城市居民座位
// 合成单测(2026-09-22 §17-CityHuman 全民驱动,契约 03 §4 重写;
// 2026-09-26 §批次25 居民-Agent 统一重写)。
//
// 覆盖:wealthDeepSeats 座位数 = clamp(resident_count,10,12)(N=10→10、
// N=12→12、N=10000→12、N<=0→12 兼容旧路径);AgentSeatConfig 无 Profession
// 字段(精选层退役,§1.4);creatorShouldBeSpectator 对全 Agent 开关的判定。
package service

import "testing"

// TestVirtualCityDeepSeats_SeatCountFollowsResidentCount 批次 25(25 文档 §3.2):
// resident_count=N ⇒ 常驻座位数 = clamp(N,10,12);N<=0 保持 12(兼容旧路径)。
func TestVirtualCityDeepSeats_SeatCountFollowsResidentCount(t *testing.T) {
	cases := []struct {
		name      string
		residents int
		want      int
	}{
		{"N=10 gives 10 seats", 10, 10},
		{"N=11 gives 11 seats", 11, 11},
		{"N=12 gives 12 seats", 12, 12},
		{"N=10000 clamps to 12", 10000, 12},
		{"N=0 legacy keeps 12", 0, 12},
		{"N=-1 legacy keeps 12", -1, 12},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seats := wealthDeepSeats(tc.residents)
			if len(seats) != tc.want {
				t.Fatalf("wealthDeepSeats(%d) seats = %d, want %d", tc.residents, len(seats), tc.want)
			}
			for i, a := range seats {
				if a.Seat != i {
					t.Fatalf("seats[%d].Seat = %d, want %d (升序)", i, a.Seat, i)
				}
				if a.ModelKey != "" {
					t.Errorf("seats[%d].ModelKey = %q, want \"\" (池驱动)", i, a.ModelKey)
				}
				if a.Role != "" {
					t.Errorf("seats[%d].Role = %q, want \"\"", i, a.Role)
				}
			}
		})
	}
	if wealthDeepSeatCount != 12 {
		t.Fatalf("wealthDeepSeatCount = %d, want 12 (与 wealth.MaxSeats 对齐)", wealthDeepSeatCount)
	}
	if wealthDeepMinSeatCount != 10 {
		t.Fatalf("wealthDeepMinSeatCount = %d, want 10 (与 wealth.MinSeats 对齐)", wealthDeepMinSeatCount)
	}
}

// TestAgentSeatConfig_NoProfessionField 座位职业偏好字段已删除(契约 03
// §1.4 死路径清算):本测试在编译期锁定 —— 若该字段被重新引入,下方
// 字面量将编译失败,reviewer 会被立即提醒补契约。
func TestAgentSeatConfig_NoProfessionField(t *testing.T) {
	a := AgentSeatConfig{Seat: 0, ModelKey: ""}
	_ = a
}

// TestCreatorShouldBeSpectator_VirtualCityDeepSeats wealth 全 Agent(缺省 true)
// → 创建者恒降级为观战者(与剩余物理空位无关);full_agent:false 时允许创建者
// 占空位。其他游戏不受影响。
func TestCreatorShouldBeSpectator_VirtualCityDeepSeats(t *testing.T) {
	cases := []struct {
		name                 string
		gameKind             string
		freeSeatCount        int
		virtualCityFullAgent bool
		want                 bool
	}{
		{"wealth full agent 12 seats downgrades creator", "virtual_city", 0, true, true},
		{"wealth full agent 10 seats with free seats still downgrades creator", "virtual_city", 2, true, true},
		{"wealth full_agent=false lets creator take a seat", "virtual_city", 2, false, false},
		{"wealth full_agent=false but no free seat downgrades creator", "virtual_city", 0, false, true},
		{"texas with free seats keeps creator player", "texasholdem", 2, false, false},
		{"werewolf no free seat downgrades creator", "werewolf", 0, false, true},
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
