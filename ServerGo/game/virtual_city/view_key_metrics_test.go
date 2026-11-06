// Package virtual_city — view_key_metrics_test.go: 批次86 关键指标看板后端
// 单测(2026-11-06 §可靠性与可观测性 §3.3)。
//
// 覆盖(契约 §7.1「关键指标 4 项取数口径」):
//   - Population = 非空座位数;
//   - EconomicOutput = Society.TotalWealth;
//   - Satisfaction = clampF(100-gini*100+SentimentBias, 0, 100)(与
//     prediction.go 的 Satisfaction 同公式);
//   - EventQueueBacklog = 未注入世界事件数 + 活动场景数;
//   - nil 安全(World nil / Society nil / Election nil);
//   - game.state 恒下发 key_metrics + event_lifecycle(空态零值 / [])。
package virtual_city

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestBuildKeyMetricsJSON 取数口径逐项对账(契约 §3.3.1)。
func TestBuildKeyMetricsJSON(t *testing.T) {
	SetSnapshotDir(t.TempDir())
	r := newScenarioTestRoom(t, "km-basic", 20261106)
	// 推进一月 ⇒ Society 由月结填充(新鲜房间的 Society 可能为 nil)。
	r.MuLock()
	r.World.SettleMonth()
	r.MuUnlock()
	w := r.World

	population := 0
	for _, p := range w.Players {
		if p != nil {
			population++
		}
	}
	pending := 0
	for i := range w.WorldEventLog {
		if !w.WorldEventLog[i].Injected {
			pending++
		}
	}
	// nil 安全口径:与 BuildKeyMetricsJSON 的 nil 分支逐字一致。
	gini, wantEcon := 0.0, int64(0)
	if w.Society != nil {
		gini = clamp01(w.Society.Gini)
		wantEcon = w.Society.TotalWealth
	}
	bias := 0.0
	if w.Election != nil {
		bias = w.Election.SentimentBias
	}
	wantSatisfaction := clampF(100-gini*100+bias, 0, 100)

	m := BuildKeyMetricsJSON(w)
	if m.Population != population {
		t.Errorf("population = %d, want %d", m.Population, population)
	}
	if m.EconomicOutput != wantEcon {
		t.Errorf("economic_output = %d, want %d", m.EconomicOutput, wantEcon)
	}
	if m.Satisfaction != wantSatisfaction {
		t.Errorf("satisfaction = %v, want %v", m.Satisfaction, wantSatisfaction)
	}
	if m.PendingWorldEvents != pending {
		t.Errorf("pending_world_events = %d, want %d", m.PendingWorldEvents, pending)
	}
	if m.ActiveScenarios != len(w.Scenarios) {
		t.Errorf("active_scenarios = %d, want %d", m.ActiveScenarios, len(w.Scenarios))
	}
	if m.EventQueueBacklog != pending+len(w.Scenarios) {
		t.Errorf("event_queue_backlog = %d, want %d", m.EventQueueBacklog, pending+len(w.Scenarios))
	}
	// 注入一个场景 ⇒ ActiveScenarios / Backlog 同步 +1。
	if _, _, e := r.ApplyScenario(ScenarioEarthquake, 2, 6, "", "km"); e != nil {
		t.Fatalf("inject: %v", e)
	}
	m2 := BuildKeyMetricsJSON(w)
	if m2.ActiveScenarios != m.ActiveScenarios+1 {
		t.Errorf("active_scenarios after inject = %d, want %d", m2.ActiveScenarios, m.ActiveScenarios+1)
	}
	if m2.EventQueueBacklog != m2.PendingWorldEvents+m2.ActiveScenarios {
		t.Errorf("backlog decomposition broken: %+v", m2)
	}
}

// TestBuildKeyMetricsJSON_NilSafety nil World / nil 子系统 ⇒ 零值,不 panic。
func TestBuildKeyMetricsJSON_NilSafety(t *testing.T) {
	var nilWorld *World
	m := BuildKeyMetricsJSON(nilWorld)
	if m.Population != 0 || m.EconomicOutput != 0 || m.EventQueueBacklog != 0 ||
		m.ActiveScenarios != 0 || m.PendingWorldEvents != 0 {
		t.Errorf("nil world metrics = %+v, want all zero", m)
	}
	if m.Satisfaction != 100 {
		t.Errorf("nil world satisfaction = %v, want 100(100-0+0)", m.Satisfaction)
	}
	// 空 World(无 Society / Election)⇒ gini=0、bias=0 ⇒ 100。
	m2 := BuildKeyMetricsJSON(&World{})
	if m2.Population != 0 || m2.Satisfaction != 100 {
		t.Errorf("empty world metrics = %+v", m2)
	}
	// Election 非 nil 但 Society nil ⇒ 只加偏置。
	w := &World{Election: NewCivicElection()}
	w.Election.SentimentBias = -20
	if got := BuildKeyMetricsJSON(w).Satisfaction; got != 80 {
		t.Errorf("satisfaction with bias only = %v, want 80", got)
	}
}

// TestBuildClientStateKeyMetricsAndLifecycle game.state 恒下发
// key_metrics + event_lifecycle(未开局也非 null;空态 = 零值 / [])。
func TestBuildClientStateKeyMetricsAndLifecycle(t *testing.T) {
	SetSnapshotDir(t.TempDir())
	r := newScenarioTestRoom(t, "km-view", 20261106)
	if _, _, e := r.ApplyScenario(ScenarioEarthquake, 3, 6, "", "view"); e != nil {
		t.Fatalf("inject: %v", e)
	}
	// 房间级只读快照方法自带 r.mu(§92a 不可重入)⇒ 必须先取好再进锁。
	seats, nicknames, botSeats := r.SnapshotSeats(), r.SnapshotNicknames(), r.SnapshotBotSeats()
	modelKeys, transcripts := r.SnapshotModelKeys(), r.SnapshotTranscripts()
	startedAt, nextMonthAt, clockMs := r.GameStartedAtUnix(), r.NextMonthAtUnix(), r.CityClockMs()
	r.MuLock()
	cs := BuildClientState("km-view", -1, r.World, seats, nicknames, botSeats,
		modelKeys, transcripts, startedAt, nextMonthAt, clockMs, nil, nil)
	scenariosInWorld := len(r.World.Scenarios)
	r.MuUnlock()

	if cs.KeyMetrics.Population == 0 {
		t.Errorf("key_metrics.population = 0, want > 0")
	}
	if cs.KeyMetrics.EventQueueBacklog != scenariosInWorld {
		t.Errorf("key_metrics.backlog = %d, want %d",
			cs.KeyMetrics.EventQueueBacklog, scenariosInWorld)
	}
	if len(cs.EventLifecycle) != 1 {
		t.Errorf("event_lifecycle = %d, want 1", len(cs.EventLifecycle))
	}
	if cs.EventLifecycle[0].TriggerSource != TriggerSourceManual {
		t.Errorf("event_lifecycle[0].trigger_source = %q", cs.EventLifecycle[0].TriggerSource)
	}

	raw, err := json.Marshal(cs)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(raw)
	if !strings.Contains(s, `"key_metrics":`) {
		t.Errorf("key_metrics missing from game.state")
	}
	if !strings.Contains(s, `"event_lifecycle":[`) {
		t.Errorf("event_lifecycle must serialize as an array")
	}
	if strings.Contains(s, `"event_lifecycle":null`) || strings.Contains(s, `"key_metrics":null`) {
		t.Errorf("key_metrics / event_lifecycle must not be null")
	}

	// 未开局房:world == nil ⇒ 两字段仍非 null。
	open := NewVirtualCityRoom("km-open", 3000, 1, 4)
	cs2 := BuildClientState("km-open", -1, nil, [MaxSeats]string{}, [MaxSeats]string{},
		[MaxSeats]bool{}, [MaxSeats]string{}, [MaxSeats]BotTranscript{}, 0, 0, 0, nil, nil)
	raw2, err := json.Marshal(cs2)
	if err != nil {
		t.Fatalf("marshal open: %v", err)
	}
	s2 := string(raw2)
	if strings.Contains(s2, `"event_lifecycle":null`) || strings.Contains(s2, `"key_metrics":null`) {
		t.Errorf("open room: key_metrics / event_lifecycle must not be null")
	}
	_ = open
}
