// Package virtual_city — agent_sense_test.go: City-Human 感知与行动工具引擎侧单测
// (2026-09-22 §CityHuman重构)。
//
// 覆盖:actMove 三模式计费/区内步行跑步、16 城区感官基底表、事件气味/声响叠加、
// See/Hear/Smell 桥(同区人/物/事 + utterance + last_senses 写入)、
// SpeakTo 私聊定向(目标校验 + 月度 ≤2 限额)、city.ambiance 快照下发。
package virtual_city

import (
	"strings"
	"testing"

	"LsmAgentGame/errcode"
)

// ── actMove 三模式 ──

func TestActMove_InDistrictWalkRun(t *testing.T) {
	r := newRoomWithSeats(t, 12)
	if e := r.Start(nil); e != nil {
		t.Fatalf("start: %v", e)
	}
	w := r.World
	p := w.Players[0]
	cash0, energy0 := p.Cash, p.Energy
	pos0 := p.LocalPos

	// 区内步行:免费、精力−1、LocalPos 变化、不进 Ledger。
	if _, e := w.ApplyAction(0, Action{Type: ActMove, Mode: "walk"}); e != nil {
		t.Fatalf("walk: %v", e)
	}
	if p.Cash != cash0 {
		t.Errorf("walk must be free: cash %d → %d", cash0, p.Cash)
	}
	if p.Energy != energy0-1 {
		t.Errorf("walk energy: got %d, want %d", p.Energy, energy0-1)
	}
	if p.ActionBudget != monthlyActionBudget-1 {
		t.Errorf("walk must consume 1 budget: got %d", p.ActionBudget)
	}
	_ = pos0 // LocalPos 随机游走,不保证变化(rng 可能撞同值),只验证不 panic

	// 区内跑步:精力−2。
	energy1 := p.Energy
	if _, e := w.ApplyAction(0, Action{Type: ActMove, Mode: "run"}); e != nil {
		t.Fatalf("run: %v", e)
	}
	if p.Energy != energy1-2 {
		t.Errorf("run energy: got %d, want %d", p.Energy, energy1-2)
	}
}

func TestActMove_CrossDistrictModes(t *testing.T) {
	r := newRoomWithSeats(t, 12)
	if e := r.Start(nil); e != nil {
		t.Fatalf("start: %v", e)
	}
	w := r.World
	p := w.Players[0]
	from := p.District
	dest := "riverside"
	if from == "riverside" {
		dest = "suburb"
	}

	// bus:¥500(×CPI 系数,curated 池 CPIYoY=0 → 1.0)精力−2。
	cash0 := p.Cash
	if _, e := w.ApplyAction(0, Action{Type: ActMove, District: dest, Mode: "bus"}); e != nil {
		t.Fatalf("bus: %v", e)
	}
	if p.District != dest {
		t.Fatalf("bus district: got %q, want %q", p.District, dest)
	}
	if got := cash0 - p.Cash; got != 500 {
		t.Errorf("bus cost: got %d, want 500", got)
	}
	if p.ActionBudget != monthlyActionBudget-1 {
		t.Errorf("bus must consume 1 budget: got %d", p.ActionBudget)
	}

	// 未知 mode → 35100。
	p2 := w.Players[1]
	_ = p2
	if _, e := w.ApplyAction(1, Action{Type: ActMove, District: "suburb", Mode: "rocket"}); e == nil || e.Code != errcode.ErrVirtualCitySenseInvalid {
		t.Errorf("unknown mode: got %v, want 35100", e)
	}
	// 未知城区 → 35100。
	if _, e := w.ApplyAction(1, Action{Type: ActMove, District: "moon", Mode: "bus"}); e == nil || e.Code != errcode.ErrVirtualCitySenseInvalid {
		t.Errorf("unknown district: got %v, want 35100", e)
	}
	// 区内移动不许用交通工具 → 35100。
	if _, e := w.ApplyAction(1, Action{Type: ActMove, Mode: "taxi"}); e == nil || e.Code != errcode.ErrVirtualCitySenseInvalid {
		t.Errorf("in-district taxi: got %v, want 35100", e)
	}
}

// ── 感官基底与叠加 ──

func TestDistrictAmbiance_AllSixteenNonEmpty(t *testing.T) {
	if len(districtAmbianceBase) != DistrictCount {
		t.Fatalf("ambiance base table: got %d districts, want %d", len(districtAmbianceBase), DistrictCount)
	}
	for _, d := range DistrictDefs {
		ab := DistrictAmbiance(d.ID)
		if len(ab.Smells) == 0 || len(ab.Sounds) == 0 {
			t.Errorf("district %s ambiance empty", d.ID)
		}
	}
}

func TestAmbianceOverlay_EventMapping(t *testing.T) {
	smells, sounds := ambianceOverlay([]EventRecord{
		{Month: 1, Type: "life", Seat: 0, Text: "3 号位(程序员)被裁员,失业 4 个月"},
		{Month: 1, Type: "life", Seat: 1, Text: "5 号位结婚了!婚礼支出 ¥50000"},
		{Month: 1, Type: EventCityVoice, Seat: -1, Text: "张三:本月手头紧"},
	})
	joinedS := strings.Join(smells, ",")
	joinedH := strings.Join(sounds, ",")
	if !strings.Contains(joinedS, "焦虑汗味") {
		t.Errorf("失业事件应映射焦虑汗味: %v", smells)
	}
	if !strings.Contains(joinedS, "喜糖甜香") {
		t.Errorf("婚礼事件应映射喜糖甜香: %v", smells)
	}
	if !strings.Contains(joinedH, "街头议论") {
		t.Errorf("城市之声应映射街头议论: %v", sounds)
	}
}

// ── See/Hear/Smell 桥 ──

// beginDecisionFor 打开座位决策槽(sense 工具门控需要)。
func beginDecisionFor(t *testing.T, r *VirtualCityRoom, seat int) *AgentRunner {
	t.Helper()
	ar := NewAgentRunner(r, seat)
	if err := ar.BeginDecision(seat, r.World.Month); err != nil {
		t.Fatalf("begin decision: %v", err)
	}
	return ar
}

func TestSee_BridgeReturnsDistrictSnapshot(t *testing.T) {
	r := newRoomWithSeats(t, 12)
	r.ResidentCount = 200
	if e := r.Start(nil); e != nil {
		t.Fatalf("start: %v", e)
	}
	ar := beginDecisionFor(t, r, 0)
	defer ar.EndDecision(0, r.World.Month)

	res, err := ar.See(0)
	if err != nil {
		t.Fatalf("see: %v", err)
	}
	if res.District == "" {
		t.Error("see result missing district")
	}
	if len(res.Things) == 0 {
		t.Error("see result missing landmarks")
	}
	// 12 座位同区邻居或背景居民至少 1 人(200 背景居民按城区分布,大概率同区有人;
	// 即使无人,座位同区邻居也可能为 0 —— 仅断言不超上限)。
	if len(res.People) > 8 {
		t.Errorf("people cap: got %d, want ≤8", len(res.People))
	}
	// last_senses 已写入 bot_contexts 数据源。
	r.mu.Lock()
	senses := r.Transcripts[0].LastSenses
	r.mu.Unlock()
	if len(senses) == 0 || senses[0].Kind != "see" {
		t.Fatalf("last_senses not recorded: %+v", senses)
	}
}

func TestHear_AfterSpeakContainsUtterance(t *testing.T) {
	r := newRoomWithSeats(t, 12)
	if e := r.Start(nil); e != nil {
		t.Fatalf("start: %v", e)
	}
	ar := beginDecisionFor(t, r, 0)
	defer ar.EndDecision(0, r.World.Month)

	if err := ar.Speak(0, "今晚行情怎么看", ""); err != nil {
		t.Fatalf("speak: %v", err)
	}
	res, err := ar.Hear(0)
	if err != nil {
		t.Fatalf("hear: %v", err)
	}
	found := false
	for _, u := range res.Utterances {
		if strings.Contains(u, "今晚行情怎么看") {
			found = true
		}
	}
	if !found {
		t.Errorf("hear must include own public utterance: %v", res.Utterances)
	}
	if len(res.Sounds) == 0 {
		t.Error("hear must include ambiance sounds")
	}
}

func TestSmell_BasePlusOverlay(t *testing.T) {
	r := newRoomWithSeats(t, 12)
	if e := r.Start(nil); e != nil {
		t.Fatalf("start: %v", e)
	}
	ar := beginDecisionFor(t, r, 0)
	defer ar.EndDecision(0, r.World.Month)

	res, err := ar.Smell(0)
	if err != nil {
		t.Fatalf("smell: %v", err)
	}
	base := DistrictAmbiance(res.District)
	if len(base.Smells) == 0 {
		t.Fatalf("district %s has no base smells", res.District)
	}
	if !strings.Contains(strings.Join(res.Smells, ","), base.Smells[0]) {
		t.Errorf("smell must contain base %q: %v", base.Smells[0], res.Smells)
	}
}

// ── SpeakTo 私聊 ──

// fakeWhisperSender 记录私聊调用(ChatSender + ChatWhisperer 双接口)。
type fakeWhisperSender struct {
	whisperTo      string
	whisperToAcct  string
	whisperText    string
	whisperCalled  bool
	publicText     string
	publicCalledCt int
}

func (f *fakeWhisperSender) SendFromBot(roomID, botUserID, botAccount, modelKey, text string) error {
	f.publicCalledCt++
	f.publicText = text
	return nil
}

func (f *fakeWhisperSender) WhisperFromBot(roomID, botUserID, botAccount, modelKey, toUserID, toAccount, text string) error {
	f.whisperCalled = true
	f.whisperTo = toUserID
	f.whisperToAcct = toAccount
	f.whisperText = text
	return nil
}

func TestSpeakTo_WhisperDirected(t *testing.T) {
	r := newRoomWithSeats(t, 12)
	fake := &fakeWhisperSender{}
	r.SetChatSender(fake)
	if e := r.Start(nil); e != nil {
		t.Fatalf("start: %v", e)
	}
	ar := beginDecisionFor(t, r, 0)
	defer ar.EndDecision(0, r.World.Month)

	if err := ar.SpeakTo(0, 5, "私下结盟吗"); err != nil {
		t.Fatalf("speak private: %v", err)
	}
	if !fake.whisperCalled {
		t.Fatal("whisper channel not invoked")
	}
	r.mu.Lock()
	toUser := r.Seats[5]
	speakCt := r.World.Players[0].SpeakCountThisMonth
	r.mu.Unlock()
	if fake.whisperTo != toUser {
		t.Errorf("whisper target: got %q, want seat5 user %q", fake.whisperTo, toUser)
	}
	if speakCt != 1 {
		t.Errorf("speak count: got %d, want 1", speakCt)
	}

	// 自我私聊 → 35103。
	if err := ar.SpeakTo(0, 0, "自言自语"); err == nil {
		t.Error("self whisper must be rejected")
	} else if e, ok := err.(*errcode.Error); !ok || e.Code != errcode.ErrVirtualCityWhisperTarget {
		t.Errorf("self whisper code: got %v, want 35103", err)
	}
	// 越界座位 → 35103。
	if err := ar.SpeakTo(0, 99, "hi"); err == nil {
		t.Error("out-of-range whisper must be rejected")
	}
}

func TestSpeak_MonthlyLimitTwo(t *testing.T) {
	r := newRoomWithSeats(t, 12)
	fake := &fakeWhisperSender{}
	r.SetChatSender(fake)
	if e := r.Start(nil); e != nil {
		t.Fatalf("start: %v", e)
	}
	ar := beginDecisionFor(t, r, 0)
	defer ar.EndDecision(0, r.World.Month)

	// area + private 合计 ≤2(§CityHuman重构 1→2)。
	if err := ar.Speak(0, "第一条", ""); err != nil {
		t.Fatalf("speak #1: %v", err)
	}
	if err := ar.SpeakTo(0, 3, "第二条"); err != nil {
		t.Fatalf("speak #2 (private): %v", err)
	}
	if err := ar.Speak(0, "第三条", ""); err == nil {
		t.Fatal("3rd speak must be rejected (monthly limit 2)")
	} else if e, ok := err.(*errcode.Error); !ok || e.Code != errcode.ErrVirtualCitySenseLimit {
		t.Errorf("3rd speak code: got %v, want 35101", err)
	}
}

// ── city.ambiance 快照下发 ──

func TestCitySnapshotView_AmbianceAttached(t *testing.T) {
	r := newRoomWithSeats(t, 12)
	r.ResidentCount = 100
	if e := r.Start(nil); e != nil {
		t.Fatalf("start: %v", e)
	}
	snap := r.CitySnapshotView()
	if snap == nil {
		t.Fatal("city snapshot nil")
	}
	if len(snap.Ambiance) != DistrictCount {
		t.Fatalf("ambiance districts: got %d, want %d", len(snap.Ambiance), DistrictCount)
	}
	for _, d := range DistrictDefs {
		tags := snap.Ambiance[d.ID]
		if len(tags.Smells) == 0 {
			t.Errorf("district %s ambiance smells empty", d.ID)
		}
	}
}

// ── GameContext 感官上下文 ──

func TestBuildContext_SurroundingsAndAmbiance(t *testing.T) {
	r := newRoomWithSeats(t, 12)
	r.ResidentCount = 200
	if e := r.Start(nil); e != nil {
		t.Fatalf("start: %v", e)
	}
	ctx, ok := BuildContextForAgent(r, 0)
	if !ok {
		t.Fatal("BuildContextForAgent failed")
	}
	if ctx.Ambiance.District == "" || len(ctx.Ambiance.Smells) == 0 {
		t.Errorf("context ambiance empty: %+v", ctx.Ambiance)
	}
	if len(ctx.Surroundings) > 8 {
		t.Errorf("surroundings cap: got %d, want ≤8", len(ctx.Surroundings))
	}
	for _, n := range ctx.Surroundings {
		if n.Name == "" {
			t.Error("neighbor name must be non-empty (真实档案或代号兜底)")
		}
	}
}
