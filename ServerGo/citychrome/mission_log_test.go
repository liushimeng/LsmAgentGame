// Package citychrome — mission_log_test.go: MissionLog 环形缓冲单测
// (批次77 §3.2:容量 20 / 同 ID 覆盖 / LastOK·LastFail / 并发 Append)。
package citychrome

import (
	"fmt"
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
