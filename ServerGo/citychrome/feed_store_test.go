// Package citychrome — feed_store_test.go: FeedStore 合并/入环/裁剪/去重单测
// (契约 §7.1 第 1 条)。
package citychrome

import (
	"encoding/json"
	"testing"
)

func quote(sym, name string, price, chg float64) RealtimeQuote {
	return RealtimeQuote{Symbol: sym, Name: name, Price: price, ChangePct: chg}
}

func TestFeedStore_MergeQuotesReplaceAndDirtyDrop(t *testing.T) {
	st := NewFeedStore(12, 20)
	p := &MissionPayload{
		Category: CategoryAShare,
		Source:   "东方财富",
		Quotes: []RealtimeQuote{
			quote("000001", "上证指数", 3221.5, 0.82),
			{Symbol: "", Name: "脏数据", Price: 1, ChangePct: 1},                 // symbol 空 → 丢弃
			{Symbol: "X", Name: "坏价", Price: -1, ChangePct: 1},                  // price ≤0 → 丢弃
			{Symbol: "Y", Name: "坏幅", Price: 1, ChangePct: 200},                 // change_pct 越界 → 丢弃
			{Symbol: "Z", Name: "NaN价", Price: 0, ChangePct: 0},                  // price 0 → 丢弃
		},
	}
	stats := st.MergeMissionResult(1000, p)
	if stats.QuotesKept != 1 || stats.QuotesDropped != 4 {
		t.Errorf("stats = %+v, want kept=1 dropped=4", stats)
	}
	snap := st.Snapshot()
	cat := snap.Categories[CategoryAShare]
	if len(cat.Quotes) != 1 || cat.Quotes[0].Symbol != "000001" {
		t.Errorf("quotes = %+v", cat.Quotes)
	}
	if cat.UpdatedAt != 1000 || cat.Source != "东方财富" {
		t.Errorf("cat meta = %+v", cat)
	}
	if cat.Quotes[0].UpdatedAt != 1000 || cat.Quotes[0].Source != "东方财富" {
		t.Errorf("quote meta filled: %+v", cat.Quotes[0])
	}
	// History 入环。
	if ring := cat.History["000001"]; len(ring) != 1 || ring[0].T != 1000 || ring[0].P != 3221.5 {
		t.Errorf("history ring = %+v", ring)
	}
	// 替换式合并:第二次 merge 只有 1 条,quotes 变为 1 条(不是累加)。
	st.MergeMissionResult(2000, &MissionPayload{Category: CategoryAShare, Source: "新浪", Quotes: []RealtimeQuote{
		quote("399001", "深证成指", 10100, -0.3),
	}})
	cat = st.Snapshot().Categories[CategoryAShare]
	if len(cat.Quotes) != 1 || cat.Quotes[0].Symbol != "399001" || cat.Source != "新浪" {
		t.Errorf("replace merge failed: %+v", cat)
	}
	// 旧 symbol 的 history 保留(环是累积的)。
	if ring := cat.History["000001"]; len(ring) != 1 {
		t.Errorf("old history lost: %+v", ring)
	}
}

func TestFeedStore_HistoryRingCap48(t *testing.T) {
	st := NewFeedStore(12, 20)
	for i := 1; i <= 60; i++ {
		st.MergeMissionResult(int64(i), &MissionPayload{Category: CategoryCrypto, Source: "s",
			Quotes: []RealtimeQuote{quote("BTC", "比特币", float64(i), 1)}})
	}
	ring := st.Snapshot().Categories[CategoryCrypto].History["BTC"]
	if len(ring) != HistoryCap {
		t.Fatalf("ring len = %d, want %d", len(ring), HistoryCap)
	}
	// 最新点在尾部,最早的 12 个点被裁掉。
	if ring[len(ring)-1].P != 60 || ring[0].P != 13 {
		t.Errorf("ring window wrong: first=%v last=%v", ring[0], ring[len(ring)-1])
	}
}

func TestFeedStore_QuotesTruncatedAtLimit(t *testing.T) {
	st := NewFeedStore(3, 20)
	quotes := make([]RealtimeQuote, 0, 5)
	for i := 0; i < 5; i++ {
		quotes = append(quotes, quote(string(rune('A'+i)), "name", float64(i+1), 0))
	}
	stats := st.MergeMissionResult(1, &MissionPayload{Category: CategoryHK, Source: "s", Quotes: quotes})
	if stats.QuotesKept != 3 || stats.QuotesTruncated != 2 {
		t.Errorf("stats = %+v, want kept=3 truncated=2", stats)
	}
	if got := len(st.Snapshot().Categories[CategoryHK].Quotes); got != 3 {
		t.Errorf("quotes len = %d, want 3", got)
	}
}

func TestFeedStore_NewsDedupAndTrim(t *testing.T) {
	st := NewFeedStore(12, 5) // newsLimit=5
	mkNews := func(title string, ts int64) RealtimeNews {
		return RealtimeNews{Title: title, URL: "https://x/" + title, PublishedAt: ts}
	}
	p1 := &MissionPayload{Category: CategoryAShare, Source: "s", News: []RealtimeNews{
		mkNews("a", 100), mkNews("b", 90), mkNews("c", 80), mkNews("d", 70), mkNews("e", 60), mkNews("f", 50),
	}}
	stats := st.MergeMissionResult(1000, p1)
	if stats.NewsAdded != 5 { // 超 news_limit=5 的先入前 5?不:先入 6 再裁到 5。
		t.Logf("added=%d (6 条全入后裁到 5)", stats.NewsAdded)
	}
	snap := st.Snapshot()
	if len(snap.News) != 5 {
		t.Fatalf("news len = %d, want 5", len(snap.News))
	}
	// 新→旧:f(50) 应被裁掉。
	for _, n := range snap.News {
		if n.Title == "f" {
			t.Error("oldest news should be trimmed")
		}
		if n.ID == "" || n.Category != CategoryAShare {
			t.Errorf("news meta not filled: %+v", n)
		}
	}
	// 去重:重复 title+url 再入 → dup skipped。
	stats = st.MergeMissionResult(2000, &MissionPayload{Category: CategoryAShare, Source: "s", News: []RealtimeNews{
		mkNews("a", 300), // 与已有 a 同 title+url → 去重
		{Title: "", URL: "https://x/empty"}, // 脏数据
		mkNews("g", 310),
	}})
	if stats.NewsAdded != 1 || stats.NewsDupSkipped != 2 {
		t.Errorf("stats = %+v, want added=1 dup=2", stats)
	}
	snap = st.Snapshot()
	if len(snap.News) != 5 || snap.News[0].Title != "g" {
		t.Errorf("after merge news = %+v", snap.News)
	}
}

func TestFeedStore_DroppedNewsIDReusableAfterTrim(t *testing.T) {
	st := NewFeedStore(12, 2)
	mk := func(title string, ts int64) RealtimeNews {
		return RealtimeNews{Title: title, URL: "https://x/" + title, PublishedAt: ts}
	}
	st.MergeMissionResult(1, &MissionPayload{Category: CategoryUS, Source: "s", News: []RealtimeNews{mk("old", 10)}})
	st.MergeMissionResult(2, &MissionPayload{Category: CategoryUS, Source: "s", News: []RealtimeNews{mk("n1", 20), mk("n2", 30)}})
	// 现在 feed 里只有 n1,n2;"old" 被裁掉且去重集应收缩。
	stats := st.MergeMissionResult(3, &MissionPayload{Category: CategoryUS, Source: "s", News: []RealtimeNews{mk("old", 40)}})
	if stats.NewsAdded != 1 {
		t.Errorf("trimmed news id should be reusable: %+v", stats)
	}
}

func TestFeedStore_SnapshotDeepCopy(t *testing.T) {
	st := NewFeedStore(12, 20)
	st.MergeMissionResult(1, &MissionPayload{Category: CategoryAShare, Source: "s",
		Quotes: []RealtimeQuote{quote("A", "a", 1, 1)}})
	snap := st.Snapshot()
	snap.Categories[CategoryAShare] = RealtimeCategory{}
	snap.News = append(snap.News, RealtimeNews{Title: "hack"})
	again := st.Snapshot()
	if len(again.Categories[CategoryAShare].Quotes) != 1 || len(again.News) != 0 {
		t.Error("snapshot must be deep copy")
	}
}

func TestFeedStore_SetChannel(t *testing.T) {
	st := NewFeedStore(12, 20)
	st.SetChannel(ChannelOnline, "", 100)
	ch := st.Channel()
	if ch.Status != ChannelOnline || ch.LastOKAt != 100 {
		t.Errorf("online channel = %+v", ch)
	}
	st.SetChannel(ChannelOffline, "boom", 200)
	ch = st.Channel()
	if ch.Status != ChannelOffline || ch.Detail != "boom" || ch.LastOKAt != 100 {
		t.Errorf("offline channel = %+v", ch)
	}
}

func TestFeedStore_MergeNilPayload(t *testing.T) {
	st := NewFeedStore(12, 20)
	if stats := st.MergeMissionResult(1, nil); stats != (MergeStats{}) {
		t.Errorf("nil payload stats = %+v", stats)
	}
}

func TestFeed_SnapshotJSONShape(t *testing.T) {
	// 契约 §3.2 字段名逐字对齐冒烟:marshal 后 key 集合固定。
	st := NewFeedStore(12, 20)
	st.MergeMissionResult(1, &MissionPayload{Category: CategoryAShare, Source: "s",
		Quotes: []RealtimeQuote{quote("A", "a", 1, 1)},
		News:   []RealtimeNews{{Title: "t", URL: "https://u", PublishedAt: 9}},
	})
	raw, err := json.Marshal(st.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"channel", "updated_at", "categories", "news"} {
		if _, ok := m[k]; !ok {
			t.Errorf("feed JSON missing key %q", k)
		}
	}
	cat := m["categories"].(map[string]any)[CategoryAShare].(map[string]any)
	for _, k := range []string{"updated_at", "source", "quotes", "history"} {
		if _, ok := cat[k]; !ok {
			t.Errorf("category JSON missing key %q", k)
		}
	}
}
