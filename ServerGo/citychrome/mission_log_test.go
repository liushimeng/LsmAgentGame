// Package citychrome — mission_log_test.go: MissionLog 环形缓冲单测
// (批次77 §3.2:容量 20 / 同 ID 覆盖 / LastOK·LastFail / 并发 Append)。
package citychrome

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

// mlRec 造一条遥测记录(测试辅助)。
func mlRec(id, category, status string) MissionRecord {
	return MissionRecord{ID: id, Category: category, Status: status, StartedAt: 1000}
}

// TestMissionLog_RingCap20 容量 20:写入 21 条只留 20,按新→旧排列且最旧被挤出。
func TestMissionLog_RingCap20(t *testing.T) {
	l := NewMissionLog()
	for i := 0; i < 21; i++ {
		l.Append(mlRec(fmt.Sprintf("news-%d", i), CategoryNews, MissionStatusOK))
	}
	got := l.Snapshot()
	if len(got) != 20 {
		t.Fatalf("snapshot len = %d, want 20 (环形上限)", len(got))
	}
	if got[0].ID != "news-20" {
		t.Errorf("newest record = %q, want news-20", got[0].ID)
	}
	if got[len(got)-1].ID != "news-1" {
		t.Errorf("oldest kept record = %q, want news-1", got[len(got)-1].ID)
	}
	for _, r := range got {
		if r.ID == "news-0" {
			t.Error("news-0 should have been evicted by the ring cap")
		}
	}
}

// TestMissionLog_SameIDOverwrite 同 ID 覆盖更新(running → ok 原地翻状态)。
func TestMissionLog_SameIDOverwrite(t *testing.T) {
	l := NewMissionLog()
	l.Append(mlRec("a_share-1", CategoryAShare, MissionStatusRunning))
	l.Append(MissionRecord{ID: "a_share-1", Category: CategoryAShare,
		Status: MissionStatusOK, Source: "腾讯行情", StepsUsed: 5, Quotes: 6, News: 2})
	got := l.Snapshot()
	if len(got) != 1 {
		t.Fatalf("snapshot len = %d, want 1 (同 ID 覆盖不追加)", len(got))
	}
	if got[0].Status != MissionStatusOK || got[0].StepsUsed != 5 ||
		got[0].Source != "腾讯行情" || got[0].Quotes != 6 || got[0].News != 2 {
		t.Errorf("record not overwritten: %+v", got[0])
	}
}

// TestMissionLog_LastOKLastFail 按类目取最近成功/失败记录。
func TestMissionLog_LastOKLastFail(t *testing.T) {
	l := NewMissionLog()
	l.Append(mlRec("hk-1", CategoryHK, MissionStatusFail))
	l.Append(MissionRecord{ID: "hk-2", Category: CategoryHK, Status: MissionStatusOK, Source: "腾讯行情"})
	l.Append(mlRec("hk-3", CategoryHK, MissionStatusFail))
	ok := l.LastOK(CategoryHK)
	if ok == nil || ok.ID != "hk-2" {
		t.Errorf("LastOK(hk) = %+v, want hk-2", ok)
	}
	fail := l.LastFail(CategoryHK)
	if fail == nil || fail.ID != "hk-3" {
		t.Errorf("LastFail(hk) = %+v, want hk-3", fail)
	}
	// 其它类目无记录 ⇒ nil。
	if l.LastOK(CategoryUS) != nil || l.LastFail(CategoryUS) != nil {
		t.Error("unrelated category should have no last record")
	}
	// 空日志 ⇒ nil。
	empty := NewMissionLog()
	if empty.LastOK(CategoryNews) != nil || empty.LastFail(CategoryNews) != nil {
		t.Error("empty log should have no last record")
	}
	// 返回的是副本:改动不得污染内部状态。
	ok.Source = "改坏了"
	if got := l.LastOK(CategoryHK).Source; got != "腾讯行情" {
		t.Errorf("LastOK must return a copy, internal source = %q", got)
	}
}

// TestMissionLog_TruncateOnAppend Error/Plan 入库按 rune 截断。
func TestMissionLog_TruncateOnAppend(t *testing.T) {
	l := NewMissionLog()
	l.Append(MissionRecord{ID: "x-1", Category: CategoryNews,
		Error: strings.Repeat("错", 400), Plan: strings.Repeat("步", 300)})
	r := l.Snapshot()[0]
	if n := utf8.RuneCountInString(r.Error); n > missionErrorCap+utf8.RuneCountInString("…(截断)") {
		t.Errorf("error runes = %d, want <= %d", n, missionErrorCap+5)
	}
	if !strings.Contains(r.Error, "截断") {
		t.Errorf("truncated error should mark itself: %q", r.Error)
	}
	if n := utf8.RuneCountInString(r.Plan); n > missionPlanCap+utf8.RuneCountInString("…(截断)") {
		t.Errorf("plan runes = %d, want <= %d", n, missionPlanCap+5)
	}
}

// TestMissionLog_ConcurrentAppend 并发 Append(-race 心智:不得重复/丢失 ID)。
func TestMissionLog_ConcurrentAppend(t *testing.T) {
	l := NewMissionLog()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			l.Append(mlRec(fmt.Sprintf("m-%d", i), CategoryNews, MissionStatusOK))
		}(i)
	}
	wg.Wait()
	got := l.Snapshot()
	if len(got) != 20 {
		t.Fatalf("snapshot len = %d, want 20 (并发后仍受环形上限)", len(got))
	}
	seen := map[string]bool{}
	for _, r := range got {
		if seen[r.ID] {
			t.Fatalf("duplicate id %q in snapshot", r.ID)
		}
		seen[r.ID] = true
	}
}

// TestMissionLog_NilSafe nil 接收者不得 panic。
func TestMissionLog_NilSafe(t *testing.T) {
	var l *MissionLog
	l.Append(MissionRecord{ID: "x"})
	if got := l.Snapshot(); got == nil || len(got) != 0 {
		t.Errorf("nil log snapshot = %v, want empty non-nil slice", got)
	}
	if l.LastOK(CategoryNews) != nil || l.LastFail(CategoryNews) != nil {
		t.Error("nil log must return nil last records")
	}
}

// ── 批次78 §3.3:Stats() 聚合 + Visited 覆盖 ─────────────────────────

// TestMissionLog_Stats_Empty 空日志:全零(成功率 0、last_error 空)。
func TestMissionLog_Stats_Empty(t *testing.T) {
	l := NewMissionLog()
	s := l.Stats()
	if s.Total != 0 || s.OK != 0 || s.Fail != 0 || s.Running != 0 {
		t.Errorf("empty stats = %+v", s)
	}
	if s.SuccessRate != 0 || s.AvgSteps != 0 || s.AvgDuration != 0 || s.LastError != "" {
		t.Errorf("empty stats fields = %+v", s)
	}
	// nil 接收者不得 panic。
	var nilLog *MissionLog
	if got := nilLog.Stats(); got.Total != 0 {
		t.Errorf("nil log stats = %+v", got)
	}
}

// TestMissionLog_Stats_AllOK 全成功:成功率 1,平均步数/耗时按成功记录算。
func TestMissionLog_Stats_AllOK(t *testing.T) {
	l := NewMissionLog()
	l.Append(MissionRecord{ID: "a-2", Category: CategoryAShare, Status: MissionStatusOK,
		StepsUsed: 4, DurationMs: 2000})
	l.Append(MissionRecord{ID: "a-1", Category: CategoryAShare, Status: MissionStatusOK,
		StepsUsed: 6, DurationMs: 4000})
	s := l.Stats()
	if s.Total != 2 || s.OK != 2 || s.Fail != 0 {
		t.Errorf("stats = %+v", s)
	}
	if s.SuccessRate != 1 {
		t.Errorf("success_rate = %v, want 1", s.SuccessRate)
	}
	if s.AvgSteps != 5 {
		t.Errorf("avg_steps = %v, want 5", s.AvgSteps)
	}
	if s.AvgDuration != 3000 {
		t.Errorf("avg_duration_ms = %v, want 3000", s.AvgDuration)
	}
	if s.LastError != "" {
		t.Errorf("last_error = %q, want empty", s.LastError)
	}
}

// TestMissionLog_Stats_Mixed 混合:成功率分母 = OK+Fail;平均步数/耗时只算成功;
// last_error 取最近一条 fail。
func TestMissionLog_Stats_Mixed(t *testing.T) {
	l := NewMissionLog()
	l.Append(MissionRecord{ID: "b-1", Category: CategoryHK, Status: MissionStatusFail,
		Error: "步数用尽且无合法 web_done", StepsUsed: 14, DurationMs: 9000})
	l.Append(MissionRecord{ID: "b-2", Category: CategoryHK, Status: MissionStatusOK,
		StepsUsed: 3, DurationMs: 1000})
	l.Append(MissionRecord{ID: "b-3", Category: CategoryHK, Status: MissionStatusRunning,
		StepsUsed: 1})
	s := l.Stats()
	if s.Total != 3 || s.OK != 1 || s.Fail != 1 || s.Running != 1 {
		t.Errorf("stats = %+v", s)
	}
	if math.Abs(s.SuccessRate-0.5) > 1e-9 {
		t.Errorf("success_rate = %v, want 0.5 (OK/(OK+Fail))", s.SuccessRate)
	}
	if s.AvgSteps != 3 {
		t.Errorf("avg_steps = %v, want 3 (只统计成功记录)", s.AvgSteps)
	}
	if s.AvgDuration != 1000 {
		t.Errorf("avg_duration_ms = %v, want 1000 (只统计成功记录)", s.AvgDuration)
	}
	if !strings.Contains(s.LastError, "步数用尽") {
		t.Errorf("last_error = %q", s.LastError)
	}
}

// TestMissionLog_Visited 批次78:Visited 入库归一化(≤6 条 / ≤120 rune / 去重)。
func TestMissionLog_Visited(t *testing.T) {
	l := NewMissionLog()
	l.Append(MissionRecord{ID: "v-1", Category: CategoryAShare, Status: MissionStatusOK,
		Visited: []string{
			"https://a.example/1", "https://a.example/1", // 重复
			"https://b.example/2", "https://c.example/3", "https://d.example/4",
			"https://e.example/5", "https://f.example/6", "https://g.example/7",
			"https://h.example/8",
		}})
	r := l.Snapshot()[0]
	if len(r.Visited) != missionVisitedCap {
		t.Fatalf("visited len = %d, want %d", len(r.Visited), missionVisitedCap)
	}
	if r.Visited[0] != "https://a.example/1" || r.Visited[1] != "https://b.example/2" {
		t.Errorf("visited order/dedup wrong: %v", r.Visited)
	}
	// 单条 URL 截断到 ≤120 rune。
	long := strings.Repeat("u", 300)
	l.Append(MissionRecord{ID: "v-2", Category: CategoryAShare, Status: MissionStatusOK,
		Visited: []string{long}})
	if n := utf8.RuneCountInString(l.Snapshot()[0].Visited[0]); n > missionVisitedRuneCap+utf8.RuneCountInString("…(截断)") {
		t.Errorf("visited url runes = %d, want <= %d", n, missionVisitedRuneCap+5)
	}
}
