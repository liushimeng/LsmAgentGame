// Package virtual_city — room_minseats_test.go: 房间级开局门槛 minSeats 单测
// (2026-10-08 §背景居民规模下限降至 1)。
//
// 背景:MinSeats=10 曾是编译期常量硬门槛,导致「3 人城市」永远停在 open
// (room.go Start 的 35003 / JoinGame 的满员判定 / ws 层自动开局三处同时卡死)。
// 方案把门槛降级为房间级字段 minSeats = min(MinSeats, clamp(resident_count,1,MaxSeats)),
// 缺省仍为 MinSeats(10),由 applyOpts 在 resident_count < 10 时下调为 N。
//
// 覆盖:
//  1. EffectiveMinSeats 缺省 = 10(零值回归安全);
//  2. applyOpts 后 N=1→1 / N=3→3 / N=9→9 / N=10000→10(≥10 行为逐字节不变);
//  3. Start:座位数 == minSeats 放行、< minSeats 仍 35003。
package virtual_city

import (
	"testing"

	"LsmAgentGame/errcode"
	"LsmAgentGame/service"
)

// newRoomWithResidentsAndSeats 构造「居民数 N、bot 座位 n 个」的房间,
// 走与生产同序:NewVirtualCityRoom → applyOpts(ResidentCount=N)→ RegisterBotSeats。
func newRoomWithResidentsAndSeats(t *testing.T, residents, n int) *VirtualCityRoom {
	t.Helper()
	if n < 0 || n > MaxSeats {
		t.Fatalf("n=%d out of [0,%d]", n, MaxSeats)
	}
	r := NewVirtualCityRoom("room-minseats", 3000, 7, 4)
	if residents > 0 {
		r.applyOpts(&service.VirtualCityRoomOptions{ResidentCount: residents})
	}
	botUsers := make(map[int]string, n)
	botModels := make(map[int]string, n)
	for seat := 0; seat < n; seat++ {
		botUsers[seat] = "mb" + string(rune('0'+seat))
		botModels[seat] = ""
	}
	r.RegisterBotSeats(botUsers, botModels)
	return r
}

// TestEffectiveMinSeats_DefaultsToMinSeats 未传 resident_count 的房间(旧路径 /
// 缺省 10000 路径)门槛恒为 MinSeats —— 改造前后行为一致。
func TestEffectiveMinSeats_DefaultsToMinSeats(t *testing.T) {
	if MinSeats != 10 {
		t.Fatalf("MinSeats = %d, want 10", MinSeats)
	}
	r := NewVirtualCityRoom("room-min-default", 3000, 7, 4)
	if got := r.EffectiveMinSeats(); got != MinSeats {
		t.Fatalf("fresh room EffectiveMinSeats = %d, want %d", got, MinSeats)
	}
	// applyOpts 传 0(「不启用城市层」/ 未传)不得下调门槛。
	r.applyOpts(&service.VirtualCityRoomOptions{ResidentCount: 0})
	if got := r.EffectiveMinSeats(); got != MinSeats {
		t.Fatalf("ResidentCount=0 EffectiveMinSeats = %d, want %d", got, MinSeats)
	}
}

// TestEffectiveMinSeats_FollowsResidentCount 小城(< 10 居民)门槛 = N;
// ≥ 10 居民门槛恒为 MinSeats(10,零回归)。
func TestEffectiveMinSeats_FollowsResidentCount(t *testing.T) {
	cases := []struct {
		name      string
		residents int
		want      int
	}{
		{"N=1 gives 1", 1, 1},
		{"N=3 gives 3", 3, 3},
		{"N=9 gives 9", 9, 9},
		{"N=10 stays 10", 10, 10},
		{"N=10000 clamps to 10", 10000, 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := NewVirtualCityRoom("room-min-"+tc.name, 3000, 7, 4)
			r.applyOpts(&service.VirtualCityRoomOptions{ResidentCount: tc.residents})
			if got := r.EffectiveMinSeats(); got != tc.want {
				t.Fatalf("EffectiveMinSeats(residents=%d) = %d, want %d", tc.residents, got, tc.want)
			}
		})
	}
}

// TestMinSeatsLocked_ZeroValueFallsBack 零值回归安全:minSeats 字段为 0
// (老对象反序列化 / 未来新增构造路径漏赋值)时,锁内变体回退 MinSeats。
func TestMinSeatsLocked_ZeroValueFallsBack(t *testing.T) {
	r := NewVirtualCityRoom("room-min-zero", 3000, 7, 4)
	r.minSeats = 0
	if got := r.EffectiveMinSeats(); got != MinSeats {
		t.Fatalf("zero-value EffectiveMinSeats = %d, want %d (回归兜底)", got, MinSeats)
	}
}

// TestStart_SmallCityStartsAtRoomMinSeats 3 人城市(resident_count=3):
// 3 个 bot 座位(== minSeats)可开局;2 座仍返回 35003。
func TestStart_SmallCityStartsAtRoomMinSeats(t *testing.T) {
	r := newRoomWithResidentsAndSeats(t, 3, 3)
	if got := r.EffectiveMinSeats(); got != 3 {
		t.Fatalf("EffectiveMinSeats = %d, want 3", got)
	}
	if e := r.Start(nil); e != nil {
		t.Fatalf("3 座(== minSeats)开局应放行,got %v", e)
	}
	if r.GetStatus() != StatusPlaying {
		t.Fatalf("status = %q, want playing", r.GetStatus())
	}
	r.Close()
}

// TestStart_SmallCityRejectsBelowRoomMinSeats 3 人城市只有 2 座时仍拒绝开局(35003)。
func TestStart_SmallCityRejectsBelowRoomMinSeats(t *testing.T) {
	r := newRoomWithResidentsAndSeats(t, 3, 2)
	e := r.Start(nil)
	if e == nil {
		t.Fatal("2 座(< minSeats=3)开局应被拒绝")
	}
	if e.Code != errcode.ErrVirtualCityNotEnoughPlayers {
		t.Fatalf("code = %d, want %d (ErrVirtualCityNotEnoughPlayers)", e.Code, errcode.ErrVirtualCityNotEnoughPlayers)
	}
}

// TestStart_OneResidentCityStartsWithOneSeat 1 人城市:1 座即可开局。
func TestStart_OneResidentCityStartsWithOneSeat(t *testing.T) {
	r := newRoomWithResidentsAndSeats(t, 1, 1)
	if e := r.Start(nil); e != nil {
		t.Fatalf("1 座(== minSeats=1)开局应放行,got %v", e)
	}
	if r.GetStatus() != StatusPlaying {
		t.Fatalf("status = %q, want playing", r.GetStatus())
	}
	r.Close()
}

// TestStart_LargeCityKeepsLegacyTenSeatGate ≥10 居民城市的门槛仍是 10:
// 9 座拒绝(35003)、10 座放行 —— 零回归。
func TestStart_LargeCityKeepsLegacyTenSeatGate(t *testing.T) {
	reject := newRoomWithResidentsAndSeats(t, 10000, 9)
	e := reject.Start(nil)
	if e == nil || e.Code != errcode.ErrVirtualCityNotEnoughPlayers {
		t.Fatalf("10000 居民 9 座: got %v, want 35003 (旧语义不变)", e)
	}
	ok := newRoomWithResidentsAndSeats(t, 10000, 10)
	if e := ok.Start(nil); e != nil {
		t.Fatalf("10000 居民 10 座开局应放行,got %v", e)
	}
	if ok.GetStatus() != StatusPlaying {
		t.Fatalf("status = %q, want playing", ok.GetStatus())
	}
	ok.Close()
}

// TestJoinGame_FullTriggersAutoStartAtRoomMinSeats JoinGame 的「满员即自动开局」
// 判定(M4)同样走房间级 minSeats:3 人城第 3 人入座即返回 full=true。
func TestJoinGame_FullTriggersAutoStartAtRoomMinSeats(t *testing.T) {
	r := newRoomWithResidentsAndSeats(t, 3, 2)
	seat, full, e := r.JoinGame("human-3", "人类3")
	if e != nil {
		t.Fatalf("JoinGame: %v", e)
	}
	if seat != 2 {
		t.Fatalf("seat = %d, want 2", seat)
	}
	if !full {
		t.Fatalf("full = false, want true (3 人城第 3 人入座即满员)")
	}
	if got := r.EffectiveMinSeats(); got != 3 {
		t.Fatalf("EffectiveMinSeats = %d, want 3", got)
	}
}
