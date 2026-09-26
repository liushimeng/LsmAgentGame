// Package virtual_city — room_batch25_test.go: 批次 25(居民-Agent 统一 /
// LLM 调用节流 / 城市时钟)房间级单测。
//
//   - driverPerMonthBudget 预算公式(N≤12 ⇒ 0;llm_lines 不再放大)
//   - CityClockMs 60× 换算 + 暂停冻结
//   - BeginDecision 同月去重(agentDecisionDone)
//   - wakeBots 跳过当月已提交座位
//   - 座位 speak 同步产出城市之声(批次 25 §3.2 发言气泡不回归)
package virtual_city

import (
	"testing"
	"time"

	"LsmAgentGame/agent/vcplayer"
)

// TestDriverPerMonthBudget 驱动层月预算 = min(cfg, max(0, N-座位数))。
func TestDriverPerMonthBudget(t *testing.T) {
	cases := []struct {
		name                  string
		cfg, residents, seats int
		want                  int
	}{
		{"N=10 seats=10 ⇒ 0 (无人需抽样)", 8, 10, 10, 0},
		{"N=12 seats=12 ⇒ 0", 8, 12, 12, 0},
		{"N=20 seats=12 ⇒ min(8,8)=8", 8, 20, 12, 8},
		{"N=15 seats=12 ⇒ min(8,3)=3", 8, 15, 12, 3},
		{"N=100000 seats=12 ⇒ cfg 8", 8, 100000, 12, 8},
		{"cfg=0 恒 0", 0, 100000, 12, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := driverPerMonthBudget(tc.cfg, tc.residents, tc.seats); got != tc.want {
				t.Fatalf("driverPerMonthBudget(%d,%d,%d) = %d, want %d",
					tc.cfg, tc.residents, tc.seats, got, tc.want)
			}
		})
	}
}

// TestStartCityLocked_DriverPerMonthZeroWhenResidentsFitSeats N≤12 建房后驱动层
// PerMonth=0(全体居民皆常驻座位 Agent,驱动层空转),且 llm_lines 不再放大
// PerMonth(仅保留并发语义)。
func TestStartCityLocked_DriverPerMonthZeroWhenResidentsFitSeats(t *testing.T) {
	r := newRoomWithSeats(t, 10)
	r.mu.Lock()
	r.ResidentCount = 10
	r.cityDriverEnabled = true
	r.cityDriverPerMonth = 8
	r.cityDriverLines = 5 // 旧实现会把 PerMonth 放大到 max(8,5)... 新实现恒 0
	r.mu.Unlock()
	if e := r.Start(nil); e != nil {
		t.Fatalf("start: %v", e)
	}
	if r.cityDriver == nil {
		t.Fatal("driver must be assembled when cityDriverEnabled")
	}
	if got := r.cityDriver.Snapshot().PerMonth; got != 0 {
		t.Fatalf("driver PerMonth = %d, want 0 (N=10 ≤ 10 座位,驱动层空转)", got)
	}
}

// TestCityClockMs 城市时钟:纪元 2025-01-01 08:00 +0800;现实 1 分钟 = 城市
// 1 小时(60×);未开局返回 0。
func TestCityClockMs(t *testing.T) {
	r := NewVirtualCityRoom("room-clock", 3000, 7, 4)
	if got := r.CityClockMs(); got != 0 {
		t.Fatalf("not started: CityClockMs = %d, want 0", got)
	}
	// 直接置开局时刻为 60s 前(免 sleep)。
	r.mu.Lock()
	r.gameStartedAt = time.Now().Unix() - 60
	r.mu.Unlock()
	got := r.CityClockMs()
	want := r.cityEpochBaseMs + 60_000*60 // 现实 60s → 城市 60min
	// 允许 1s 现实抖动(60s 城市)。
	if diff := got - want; diff < -60_000 || diff > 120_000 {
		t.Fatalf("CityClockMs = %d, want ≈ %d (diff %d ms)", got, want, diff)
	}
	// 纪元必须落在 2025-01-01 08:00 +0800(= 2025-01-01T00:00:00Z =
	// 1735689600s)。防御:若时区算错,此断言会暴露。
	if base := cityEpochBaseMillis(); base != 1735689600000 {
		t.Fatalf("cityEpochBaseMillis = %d, want 1735689600000 (2025-01-01 08:00 +0800)", base)
	}
}

// TestCityClockMs_PauseFreezes 暂停期间城市时钟冻结:暂停 5s(现实)后恢复,
// 时钟不回跳;暂停中读取与恢复后读取的差值 ≈ 恢复后流逝 × 60。
func TestCityClockMs_PauseFreezes(t *testing.T) {
	r := newRoomWithSeats(t, 10)
	r.SetOwner("tester")
	if e := r.Start(nil); e != nil {
		t.Fatalf("start: %v", e)
	}
	if e := r.Pause("tester", true); e != nil {
		t.Fatalf("pause: %v", e)
	}
	c1 := r.CityClockMs()
	time.Sleep(120 * time.Millisecond) // 现实流逝,城市时钟必须冻结
	c2 := r.CityClockMs()
	if c1 != c2 {
		t.Fatalf("paused clock moved: %d → %d", c1, c2)
	}
	if e := r.Pause("tester", false); e != nil {
		t.Fatalf("resume: %v", e)
	}
	time.Sleep(60 * time.Millisecond)
	c3 := r.CityClockMs()
	if c3 <= c2 {
		t.Fatalf("resumed clock must advance: %d → %d", c2, c3)
	}
	// 恢复后流逝 60ms 现实 ≈ 3.6s 城市(上限放宽到 1 分钟城市吸收调度抖动)。
	if delta := c3 - c2; delta > 60_000 {
		t.Fatalf("clock jump after resume too large: %d ms (pause 累计未扣除?)", delta)
	}
}

// TestBeginDecision_SameMonthDedup 批次 25(§3.3):同月同座位只允许一次决策
// —— EndDecision 后 agentDecisionDone[seat]==month,BeginDecision 同月拒绝;
// 跨月(新月份)放行。
func TestBeginDecision_SameMonthDedup(t *testing.T) {
	r := newRoomWithSeats(t, 10)
	if e := r.Start(nil); e != nil {
		t.Fatalf("start: %v", e)
	}
	ar := NewAgentRunner(r, 0)
	if err := ar.BeginDecision(0, 1); err != nil {
		t.Fatalf("first BeginDecision must succeed: %v", err)
	}
	if err := ar.BeginDecision(0, 1); err == nil {
		t.Fatal("concurrent BeginDecision must be rejected (already active)")
	}
	ar.EndDecision(0, 1)
	if err := ar.BeginDecision(0, 1); err == nil {
		t.Fatal("same-month BeginDecision after EndDecision must be rejected (already done)")
	}
	// 跨月放行(直接推进 World.Month 模拟月结;active 槽已释放)。
	r.mu.Lock()
	r.World.Month = 2
	r.mu.Unlock()
	if err := ar.BeginDecision(0, 2); err != nil {
		t.Fatalf("new-month BeginDecision must succeed: %v", err)
	}
	ar.EndDecision(0, 2)
}

// TestWakeBots_SkipsSubmittedSeats 批次 25(§3.3):wakeBots 跳过当月已提交
// 座位 —— agentSem 占满时,未提交座位落 per-seat pending 单槽,已提交座位
// 不得登记 pending。
func TestWakeBots_SkipsSubmittedSeats(t *testing.T) {
	r := newRoomWithSeats(t, 10)
	if e := r.Start(nil); e != nil {
		t.Fatalf("start: %v", e)
	}
	r.mu.Lock()
	// 装配两个 bot agent(runner 未接,runAgentCtx 会走 OnMonthStart 快速返回
	// 路径 —— 但本测试先把 agentSem 占满,runOneBot 只能走 pending 分支)。
	r.agents[0] = vcplayer.NewAgent(r.RoomID, r.Seats[0], "M", "M", 0, 3, time.Second)
	r.agents[1] = vcplayer.NewAgent(r.RoomID, r.Seats[1], "M", "M", 1, 3, time.Second)
	// 座位 0 已提交,座位 1 未提交。
	r.World.Players[0].Submitted = true
	// 占满并发信号量(NewVirtualCityRoom llmConcurrency=4)。
	for i := 0; i < cap(r.agentSem); i++ {
		r.agentSem <- struct{}{}
	}
	r.mu.Unlock()

	r.wakeBots()
	time.Sleep(50 * time.Millisecond)

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.agentPending[0] {
		t.Fatal("submitted seat 0 must NOT be woken (no pending slot)")
	}
	if !r.agentPending[1] {
		t.Fatal("unsubmitted seat 1 must be registered in per-seat pending slot")
	}
}

// TestSeatSpeak_EmitsCityVoice 批次 25(§3.2 发言气泡不回归):座位 Agent
// speak 除 transcript/utterance 外,同步写 Backdrop 城市之声环形缓冲并广播
// EventCityVoice(N≤12 驱动层 PerMonth=0 时 3D 语音气泡的唯一供给)。
func TestSeatSpeak_EmitsCityVoice(t *testing.T) {
	r := newRoomWithSeats(t, 10)
	r.mu.Lock()
	r.ResidentCount = 100
	r.cityVoiceEnabled = false
	r.cityDriverEnabled = false
	r.mu.Unlock()
	if e := r.Start(nil); e != nil {
		t.Fatalf("start: %v", e)
	}
	events := make(chan EventRecord, 8)
	r.SetHooks(BroadcastHooks{OnEvent: func(_ string, ev EventRecord) {
		select {
		case events <- ev:
		default:
		}
	}})
	ar := NewAgentRunner(r, 0)
	if err := ar.BeginDecision(0, 1); err != nil {
		t.Fatalf("BeginDecision: %v", err)
	}
	if err := ar.Speak(0, "今天工地上活不少,累但踏实。", "内心独白"); err != nil {
		t.Fatalf("speak: %v", err)
	}
	ar.EndDecision(0, 1)

	snap := r.CitySnapshotView()
	if snap == nil {
		t.Fatal("city snapshot nil")
	}
	found := false
	for _, v := range snap.Voices {
		if v.Text == "今天工地上活不少,累但踏实。" && v.Name == r.Nicknames[0] {
			found = true
		}
	}
	if !found {
		t.Fatalf("seat speak must append city voice record, voices = %+v", snap.Voices)
	}
	select {
	case ev := <-events:
		if ev.Type != EventCityVoice {
			t.Fatalf("event type = %q, want %q", ev.Type, EventCityVoice)
		}
	case <-time.After(time.Second):
		t.Fatal("emitCityVoiceEvent must broadcast EventCityVoice")
	}
}
