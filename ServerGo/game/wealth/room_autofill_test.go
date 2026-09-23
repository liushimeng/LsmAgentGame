// Package wealth — room_autofill_test.go: 焦点居民自动填充后开局链路单测
// (2026-09-22 §CityHuman重构,前端联调)。
//
// 场景:wealth 建房服务端恒合成 12 深度座位(契约 03 §3.2;旧填充逻辑已
// 退役),ws 层注册池驱动 bot 座位(ModelKey="")→ FullAgentMode=true →
// 满 MinSeats 自动开局。本测试在房间层验证该终态:座位集注册 →
// Occupied()>=MinSeats → Start 成功 → playing。
package wealth

import "testing"

// TestAutoFilledPoolSeats_StartsSimulation 模拟 agent_seats=3 填充后的房间:
// 10 个池驱动 bot 座位注册满 MinSeats,全 Agent 模式开局成功。
func TestAutoFilledPoolSeats_StartsSimulation(t *testing.T) {
	r := NewWealthRoom("room-autofill", 3000, 7, 4)
	// 等价于 wealthDeepSeats() 的座位集:全部池驱动(ModelKey="")。
	botUsers := map[int]string{}
	botModels := map[int]string{}
	seats := []int{1, 5, 9, 0, 2, 3, 4, 6, 7, 8}
	for _, seat := range seats {
		botUsers[seat] = "bot-autofill-" + string(rune('a'+seat))
		botModels[seat] = ""
	}
	if len(botUsers) != MinSeats {
		t.Fatalf("fixture seats: got %d, want %d", len(botUsers), MinSeats)
	}
	// ws 层顺序:先 FullAgentMode,再注册,再自动开局判定。
	r.SetFullAgentMode(true)
	r.RegisterBotSeats(botUsers, botModels)
	if !r.FullAgentMode {
		t.Fatal("FullAgentMode must be true")
	}
	if got := r.Occupied(); got < MinSeats {
		t.Fatalf("occupied: got %d, want >= %d", got, MinSeats)
	}
	if e := r.Start(nil); e != nil {
		t.Fatalf("start: %v", e)
	}
	if r.GetStatus() != StatusPlaying {
		t.Fatalf("status: got %q, want playing", r.GetStatus())
	}
	// 池驱动座位昵称占位 → Start 抽卡后升级(接线不回归)。
	r.mu.Lock()
	nick := r.Nicknames[0]
	r.mu.Unlock()
	if nick == "" {
		t.Error("filled pool seat 0 must have nickname after Start")
	}
	r.Close()
}
