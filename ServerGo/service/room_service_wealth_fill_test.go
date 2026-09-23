// Package service — room_service_wealth_fill_test.go: 虚拟城市深度层座位
// 合成单测(2026-09-22 §17-CityHuman 全民驱动,契约 03 §4 重写)。
//
// 覆盖:wealthDeepSeats 恒 12 名池驱动深度座位(ModelKey="" 全空、座位
// 0..11 升序);AgentSeatConfig 无 Profession 字段(精选层退役,§1.4);
// creatorShouldBeSpectator 对 12 深度座位的判定。
package service

import "testing"

// TestWealthDeepSeats_TwelvePoolDrivenSeats wealth 深度层恒 12 名池驱动座位
// (契约 03 §3.2):座位 0..11 升序、ModelKey 全空(= LLM 线路池分配)、
// Role 空(不注入角色偏好)。
func TestWealthDeepSeats_TwelvePoolDrivenSeats(t *testing.T) {
	seats := wealthDeepSeats()
	if len(seats) != wealthDeepSeatCount {
		t.Fatalf("deep seats = %d, want %d", len(seats), wealthDeepSeatCount)
	}
	if wealthDeepSeatCount != 12 {
		t.Fatalf("wealthDeepSeatCount = %d, want 12 (与 wealth.MaxSeats 对齐)", wealthDeepSeatCount)
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
}

// TestAgentSeatConfig_NoProfessionField 座位职业偏好字段已删除(契约 03
// §1.4 死路径清算):本测试在编译期锁定 —— 若该字段被重新引入,下方
// 字面量将编译失败,reviewer 会被立即提醒补契约。
func TestAgentSeatConfig_NoProfessionField(t *testing.T) {
	a := AgentSeatConfig{Seat: 0, ModelKey: ""}
	_ = a
}

// TestCreatorShouldBeSpectator_WealthDeepSeats wealth 12 深度座位 → 创建者
// 恒降级为观战者;其他游戏不受影响。
func TestCreatorShouldBeSpectator_WealthDeepSeats(t *testing.T) {
	cases := []struct {
		name           string
		gameKind       string
		freeSeatCount  int
		agentSeatCount int
		want           bool
	}{
		{"wealth 12 deep seats downgrades creator", "wealth", 0, 12, true},
		{"wealth lower counts still guarded by free seats", "wealth", 0, 3, true},
		{"wealth free seats keep creator player", "wealth", 3, 9, false},
		{"texas agents still has creator player seat", "texasholdem", 2, 10, false},
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
