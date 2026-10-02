// Package virtual_city — agent_batch35_test.go: 批次35 后端增量单测
// (§4.1 协议 local_pos/avatar + §4.2 apply 补 OnState + §5.1 sense 事件)。
package virtual_city

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sync"
	"testing"

	"LsmAgentGame/agent/vcplayer"
	"LsmAgentGame/game/virtual_city/city"
	"LsmAgentGame/game/virtual_city/profession"
)

// batch35State 为房间构造 viewer 视图(复用 view_null_test 的快照装配口径;
// Engine/Snapshot* 各自持锁,此处不得再持 r.mu —— §92a)。
func batch35State(r *VirtualCityRoom, viewer int) *ClientGameState {
	return BuildClientState(r.RoomID, viewer, r.Engine(), r.SnapshotSeats(), r.SnapshotNicknames(),
		r.SnapshotBotSeats(), r.SnapshotModelKeys(), r.SnapshotTranscripts(),
		r.GameStartedAtUnix(), r.NextMonthAtUnix(), r.CityClockMs(), nil, nil)
}

// TestBatch35_PlayerJSONLocalPosAvatar players[].local_pos / avatar 下发
// (§4.1):在座座位两字段齐备(avatar 原型合法、域下标口径一致);空座位
// local_pos 为非 nil [0,0](防 JSON null 崩前端)。
func TestBatch35_PlayerJSONLocalPosAvatar(t *testing.T) {
	r := newRoomWithSeats(t, MaxSeats)
	if e := r.Start(nil); e != nil {
		t.Fatalf("start: %v", e)
	}
	cs := batch35State(r, 0)
	occupied := 0
	for s, pj := range cs.Players {
		if pj.LocalPos == nil || len(pj.LocalPos) != 2 {
			t.Fatalf("players[%d].local_pos = %v, want [x,y] 非 nil", s, pj.LocalPos)
		}
		if r.SnapshotSeats()[s] == "" {
			if pj.Avatar != nil {
				t.Errorf("players[%d] empty seat avatar should be nil", s)
			}
			continue
		}
		occupied++
		if pj.Avatar == nil {
			t.Fatalf("players[%d].avatar = nil, want non-nil(§4.1)", s)
		}
		if !regexp.MustCompile(`^char_[a-z]+$`).MatchString(pj.Avatar.Archetype) {
			t.Errorf("players[%d].avatar.archetype = %q, want char_* 原型", s, pj.Avatar.Archetype)
		}
		// 域下标口径:Player.Domain(合成兜底卡 = "" → -1)与 avatar.domain 一致。
		wantDomain := city.DomainIndexOf(r.World.Players[s].Domain)
		if pj.Avatar.Domain != wantDomain {
			t.Errorf("players[%d].avatar.domain = %d, want %d (Domain=%q)",
				s, pj.Avatar.Domain, wantDomain, r.World.Players[s].Domain)
		}
		if pj.Avatar.Age != r.World.Players[s].Card.StartAge {
			t.Errorf("players[%d].avatar.age = %d, want 开局年龄 %d", s, pj.Avatar.Age, r.World.Players[s].Card.StartAge)
		}
	}
	if occupied != MaxSeats {
		t.Fatalf("occupied = %d, want %d", occupied, MaxSeats)
	}
}

// TestBatch35_DocsDomainWired 文档池开局域名接线(§4.1):真实卡必带
// L1 域名(^[A-Z]-…),Player.Domain 非空且 avatar.domain ≥ 0。
// 文档池不可用时跳过(与 seats12 同惯例;直连 lag_docs 真实池)。
func TestBatch35_DocsDomainWired(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: 跳过真实文档池开局")
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Skip("runtime.Caller unavailable")
	}
	root := filepath.Join(filepath.Dir(file), "..", "..", "..", "lag_docs", "虚拟城市", "玩家职业设计")
	if _, err := os.Stat(root); err != nil {
		t.Skipf("docs pool not available at %s: %v", root, err)
	}
	m := NewManager(Config{MonthMs: 3000, Seed: 42}, nil)
	m.SetLoader(profession.NewLoader(root))
	r := m.CreateRoom("room-b35-domain")
	botUsers := make(map[int]string, MaxSeats)
	botModels := make(map[int]string, MaxSeats)
	for seat := 0; seat < MaxSeats; seat++ {
		botUsers[seat] = "b" + string(rune('0'+seat))
		botModels[seat] = "M" + string(rune('A'+seat))
	}
	r.RegisterBotSeats(botUsers, botModels)
	if e := r.Start(m.loader); e != nil {
		t.Fatalf("start: %v", e)
	}
	pattern := regexp.MustCompile(`^[A-Z]-`)
	domainCount := 0
	for seat := 0; seat < MaxSeats; seat++ {
		p := r.World.Players[seat]
		if p == nil {
			t.Fatalf("players[%d] nil", seat)
		}
		// 注:drawPairs 允许文档池供不满时混入合成兜底卡(Design:域名 = 空串),
		// 故仅要求「多数真实卡带域名」且非空域名必在 0..25 域表内。
		if p.Domain == "" {
			continue
		}
		if !pattern.MatchString(p.Domain) {
			t.Errorf("players[%d].Domain = %q, want ^[A-Z]- L1 域名", seat, p.Domain)
		}
		if di := city.DomainIndexOf(p.Domain); di < 0 || di > 25 {
			t.Errorf("DomainIndexOf(%q) = %d, want 0..25", p.Domain, di)
		}
		domainCount++
	}
	if domainCount == 0 {
		t.Fatal("no player got a docs-pool domain (域名接线未生效)")
	}
	// 卡池域名列表与卡同长且下标同步(drawCardLocked 两池同步递增口径)。
	r.mu.Lock()
	poolLen, domLen := len(r.cardPool), len(r.cardPoolDomains)
	r.mu.Unlock()
	// Start 后 cardPool 可能已被 rebuild(池尽循环),仅断言「两池长度恒等」不变量。
	if poolLen != domLen {
		t.Errorf("cardPool(%d) 与 cardPoolDomains(%d) 长度不一致", poolLen, domLen)
	}
}

// TestBatch35_ApplyBroadcastsOnState bot 动作成功路径 OnEvent 之后补发 OnState
// (§4.2):与人类动作路径对齐,local_pos/district 变更实时可见。
func TestBatch35_ApplyBroadcastsOnState(t *testing.T) {
	r := newRoomWithSeats(t, MaxSeats)
	if e := r.Start(nil); e != nil {
		t.Fatalf("start: %v", e)
	}
	var mu sync.Mutex
	events, states := 0, 0
	r.SetHooks(BroadcastHooks{
		OnEvent: func(_ string, _ EventRecord) { mu.Lock(); events++; mu.Unlock() },
		OnState: func(_ string) { mu.Lock(); states++; mu.Unlock() },
	})
	runner := NewAgentRunner(r, 0)
	if err := runner.BeginDecision(0, 1); err != nil {
		t.Fatalf("begin decision: %v", err)
	}
	defer runner.EndDecision(0, 1)
	if err := runner.Study(0); err != nil {
		t.Fatalf("study(经 activity 派发): %v", err)
	}
	mu.Lock()
	ev, st := events, states
	mu.Unlock()
	if ev != 1 {
		t.Fatalf("events = %d, want 1", ev)
	}
	if st < 1 {
		t.Fatalf("onState calls = %d, want ≥1 (批次35 §4.2 动作后实时广播)", st)
	}
}

// TestBatch35_SenseEventBroadcast See 成功广播 type=sense 事件(§5.1),
// 且**不写入** World.Events / utterances(不污染月度事件流与感知回读)。
func TestBatch35_SenseEventBroadcast(t *testing.T) {
	r := newRoomWithSeats(t, MaxSeats)
	if e := r.Start(nil); e != nil {
		t.Fatalf("start: %v", e)
	}
	var mu sync.Mutex
	var got []EventRecord
	states := 0
	r.SetHooks(BroadcastHooks{
		OnEvent: func(_ string, ev EventRecord) { mu.Lock(); got = append(got, ev); mu.Unlock() },
		OnState: func(_ string) { mu.Lock(); states++; mu.Unlock() },
	})
	r.mu.Lock()
	eventsBefore := len(r.World.Events)
	utterBefore := len(r.utterances)
	r.mu.Unlock()

	runner := NewAgentRunner(r, 0)
	if err := runner.BeginDecision(0, 1); err != nil {
		t.Fatalf("begin decision: %v", err)
	}
	defer runner.EndDecision(0, 1)
	if _, err := runner.See(0); err != nil {
		t.Fatalf("see: %v", err)
	}

	mu.Lock()
	if len(got) != 1 {
		mu.Unlock()
		t.Fatalf("sense events = %d, want 1", len(got))
	}
	ev := got[0]
	st := states
	mu.Unlock()
	if ev.Type != EventAgentSense || EventAgentSense != "sense" {
		t.Errorf("event type = %q, want sense(%q)", ev.Type, EventAgentSense)
	}
	if ev.Seat != 0 || ev.Month != 1 || ev.Text == "" {
		t.Errorf("sense event = %+v, want seat=0/month=1/text 非空", ev)
	}
	if st < 1 {
		t.Errorf("onState calls = %d, want ≥1", st)
	}
	// 不入月度事件流 / 发言缓冲(§5.1 硬约束)。
	r.mu.Lock()
	eventsAfter := len(r.World.Events)
	utterAfter := len(r.utterances)
	r.mu.Unlock()
	if eventsAfter != eventsBefore || utterAfter != utterBefore {
		t.Errorf("sense polluted streams: events %d→%d, utterances %d→%d",
			eventsBefore, eventsAfter, utterBefore, utterAfter)
	}
}

// TestBatch35_MoveEventAndOnState move 动作事件类型 = move(批次35:
// ToolMoveDistrict 引用删除后仅判 ToolMove)。
func TestBatch35_MoveEventAndOnState(t *testing.T) {
	r := newRoomWithSeats(t, MaxSeats)
	if e := r.Start(nil); e != nil {
		t.Fatalf("start: %v", e)
	}
	var mu sync.Mutex
	var evType string
	r.SetHooks(BroadcastHooks{
		OnEvent: func(_ string, ev EventRecord) { mu.Lock(); evType = ev.Type; mu.Unlock() },
	})
	runner := NewAgentRunner(r, 0)
	if err := runner.BeginDecision(0, 1); err != nil {
		t.Fatalf("begin decision: %v", err)
	}
	defer runner.EndDecision(0, 1)
	// 现金不够打车也无妨:事件类型断言只看成功路径;给足现金后走区内步行。
	r.mu.Lock()
	r.World.Players[0].Cash += 100000
	dest := r.World.Players[0].District
	r.mu.Unlock()
	if err := runner.Move(0, dest, "walk"); err != nil {
		t.Fatalf("move: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if evType != "move" {
		t.Errorf("move event type = %q, want move", evType)
	}
}

// TestBatch35_ToolNames22 引用 vcplayer 断言工具口径(防两包漂移;
// 批次35 §3.2 = 21,批次52 §2 裁决 D13 增 family → 22)。
func TestBatch35_ToolNames22(t *testing.T) {
	names := vcplayer.ToolNames()
	if len(names) != 22 {
		t.Fatalf("vcplayer.ToolNames() = %d, want 22 (批次35 §3.2 + 批次52 family)", len(names))
	}
	if len(vcplayer.BuildTools()) != 22 {
		t.Fatalf("vcplayer.BuildTools() = %d, want 22", len(vcplayer.BuildTools()))
	}
}
