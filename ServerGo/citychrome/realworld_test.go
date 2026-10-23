// Package citychrome — realworld_test.go: realworld_info 紧凑 JSON 单测
// (契约 §3.7) + Service 手动刷新限流单测。
package citychrome

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"LsmAgentGame/config"
)

func sampleFeed() Feed {
	st := NewFeedStore(12, 20)
	st.MergeMissionResult(1000, &MissionPayload{Category: CategoryAShare, Source: "东方财富",
		Quotes: []RealtimeQuote{
			quote("000001", "上证指数", 3221.5, 0.82),
			quote("000002", "深证成指", 10100, -0.3),
			quote("000003", "创业板指", 2100, 1.1),
			quote("000004", "科创50", 950, 0.5),
			quote("000005", "沪深300", 3900, 0.4),
			quote("000006", "第六只", 1, 0.1), // 第 6 条:验证 quotes 前 5 截断
		},
		News: []RealtimeNews{{Title: "A股放量上涨", URL: "https://x/1", PublishedAt: 999}},
	})
	st.MergeMissionResult(2000, &MissionPayload{Category: CategoryCrypto, Source: "CoinGecko",
		Quotes: []RealtimeQuote{quote("BTC", "比特币", 65000, 2.1)}})
	return st.Snapshot()
}

func TestFeedCompactJSON_Overview(t *testing.T) {
	text, err := FeedCompactJSON(sampleFeed(), "overview", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"status"`, `"a_share"`, `"crypto"`, "上证指数", `"headlines"`, "A股放量上涨"} {
		if !strings.Contains(text, want) {
			t.Errorf("overview missing %q: %s", want, text)
		}
	}
	if strings.Contains(text, "第六只") {
		t.Error("overview top should cap at 3 quotes per category")
	}
}

func TestFeedCompactJSON_Quotes(t *testing.T) {
	text, err := FeedCompactJSON(sampleFeed(), "quotes", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "第六只") {
		t.Error("quotes should cap 5 per category")
	}
	if !strings.Contains(text, "BTC") || !strings.Contains(text, "上证指数") {
		t.Errorf("quotes should include both categories: %s", text)
	}
	// category 过滤。
	text, err = FeedCompactJSON(sampleFeed(), "quotes", CategoryCrypto, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "上证指数") || !strings.Contains(text, "BTC") {
		t.Errorf("category filter failed: %s", text)
	}
}

func TestFeedCompactJSON_News(t *testing.T) {
	text, err := FeedCompactJSON(sampleFeed(), "news", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "A股放量上涨") || !strings.Contains(text, `"news"`) {
		t.Errorf("news = %s", text)
	}
}

func TestFeedCompactJSON_Errors(t *testing.T) {
	if _, err := FeedCompactJSON(sampleFeed(), "hack", "", "", ""); err == nil {
		t.Error("unknown op should fail")
	}
	if _, err := FeedCompactJSON(sampleFeed(), "quotes", "mars", "", ""); err == nil {
		t.Error("unknown category should fail")
	}
	// op 缺省 = overview。
	if _, err := FeedCompactJSON(sampleFeed(), "", "", "", ""); err != nil {
		t.Errorf("empty op should default overview: %v", err)
	}
}

// TestFeedCompactJSON_NewsKeyword 批次77 §3.5:keyword 仅 op=news 生效,
// 标题大小写不敏感包含过滤;空串不过滤;其余 op 忽略 keyword。
func TestFeedCompactJSON_NewsKeyword(t *testing.T) {
	f := Feed{
		Channel: ChannelStatus{Status: ChannelOnline},
		News: []RealtimeNews{
			{ID: "1", Title: "黄金价格创新高", Category: CategoryNews},
			{ID: "2", Title: "A股三大指数集体上涨", Category: CategoryNews},
			{ID: "3", Title: "GOLD 期货收涨", Category: CategoryNews},
		},
	}
	// 命中:大小写不敏感。
	text, err := FeedCompactJSON(f, "news", "", "gold", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "GOLD 期货收涨") {
		t.Errorf("keyword should match case-insensitively: %s", text)
	}
	if strings.Contains(text, "A股三大指数集体上涨") {
		t.Errorf("keyword should filter out non-matching titles: %s", text)
	}
	// 中文命中。
	text, err = FeedCompactJSON(f, "news", "", "黄金", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "黄金价格创新高") || strings.Contains(text, "A股三大指数集体上涨") {
		t.Errorf("chinese keyword filter failed: %s", text)
	}
	// 不命中 ⇒ 空数组(非 null)。
	text, err = FeedCompactJSON(f, "news", "", "不存在的词", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, `"news":[]`) {
		t.Errorf("non-matching keyword should yield empty news: %s", text)
	}
	// 空串 = 不过滤。
	text, err = FeedCompactJSON(f, "news", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"黄金价格创新高", "A股三大指数集体上涨", "GOLD 期货收涨"} {
		if !strings.Contains(text, want) {
			t.Errorf("empty keyword must not filter, missing %q: %s", want, text)
		}
	}
	// 其余 op 忽略 keyword。
	text, err = FeedCompactJSON(f, "overview", "", "黄金", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "黄金价格创新高") {
		t.Errorf("overview must ignore keyword: %s", text)
	}
}

// TestService_RefreshRateLimit 手动刷新 60s 限流(35054)。
func TestService_RefreshRateLimit(t *testing.T) {
	Stop()
	defer Stop()
	svc := Start(config.CityChromeConfig{
		Enabled:            true,
		DebugToolURL:       "http://127.0.0.1:1", // 底座不可达不影响入队。
		ModelKey:           "DeepSeek-model",
		RefreshSeconds:     1800,
		MaxStepsPerMission: 14,
		SymbolsPerCategory: 12,
		NewsLimit:          20,
		RequestTimeoutMs:   1000,
	}, nil)
	fakeNow := time.Unix(1_800_000_000, 0)
	svc.SetRefreshClock(func() time.Time { return fakeNow })

	ok, queued, e := svc.Refresh("a_share")
	if !ok || queued != "a_share" || e != nil {
		t.Fatalf("first refresh: ok=%v queued=%q e=%v", ok, queued, e)
	}
	ok, _, e = svc.Refresh("a_share")
	if ok || e == nil || e.Code != 35054 {
		t.Errorf("second refresh within 60s should be 35054: ok=%v e=%v", ok, e)
	}
	// 其它类目不受限。
	ok, _, e = svc.Refresh("hk")
	if !ok || e != nil {
		t.Errorf("other category should pass: ok=%v e=%v", ok, e)
	}
	// 过 60s → 同类目可再刷。
	fakeNow = fakeNow.Add(61 * time.Second)
	ok, _, e = svc.Refresh("a_share")
	if !ok || e != nil {
		t.Errorf("after 60s should pass: ok=%v e=%v", ok, e)
	}
	// 空 category → 轮转类目(再前进 61s 避免撞上 a_share 的限流窗口)。
	fakeNow = fakeNow.Add(61 * time.Second)
	ok, queued, e = svc.Refresh("")
	if !ok || queued == "" || e != nil {
		t.Errorf("empty category should queue rotation category: ok=%v queued=%q e=%v", ok, queued, e)
	}
	// 未知类目 → 校验错。
	ok, _, e = svc.Refresh("nope")
	if ok || e == nil || e.Code != 20001 {
		t.Errorf("unknown category should be validation error: ok=%v e=%v", ok, e)
	}
}

// TestService_DisabledOffline 占位实例(enabled=false)刷新返回 35053。
func TestService_DisabledOffline(t *testing.T) {
	Stop()
	defer Stop()
	svc := Start(config.CityChromeConfig{Enabled: false}, nil)
	if got := svc.Channel(); got.Status != ChannelOffline {
		t.Errorf("disabled channel = %+v", got)
	}
	if ok, _, e := svc.Refresh("a_share"); ok || e == nil || e.Code != 35053 {
		t.Errorf("disabled refresh should be 35053: ok=%v e=%v", ok, e)
	}
}

// ── 批次78 §3.5:realworld_info 扩 op ────────────────────────────────

// TestFeedCompactJSON_OverviewAgeStale op=overview:各类目带 age_seconds/stale。
func TestFeedCompactJSON_OverviewAgeStale(t *testing.T) {
	old := time.Now().Unix() - 7200 // 2 小时前(行情类目间隔 1800s ⇒ stale)。
	f := Feed{
		Channel: ChannelStatus{Status: ChannelOnline},
		Categories: map[string]RealtimeCategory{
			CategoryAShare: {UpdatedAt: old, Source: "腾讯行情", Quotes: []RealtimeQuote{
				quote("000001", "上证指数", 3658.03, -1.48),
			}},
			CategoryMacro: {UpdatedAt: time.Now().Unix(), Source: "统计局", Quotes: []RealtimeQuote{
				quote("CPI_YOY", "CPI同比", 0.4, -0.1),
			}},
		},
	}
	text, err := FeedCompactJSON(f, "overview", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Categories map[string]struct {
			AgeSeconds int64 `json:"age_seconds"`
			Stale      bool  `json:"stale"`
			UpdatedAt  int64 `json:"updated_at"`
		} `json:"categories"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("bad json: %v (%s)", err, text)
	}
	a := out.Categories[CategoryAShare]
	if a.AgeSeconds < 7000 {
		t.Errorf("a_share age_seconds = %d, want >= 7000", a.AgeSeconds)
	}
	if !a.Stale {
		t.Error("a_share should be stale (age > 2×1800s)")
	}
	if out.Categories[CategoryMacro].Stale {
		t.Error("macro (fresh) must not be stale")
	}
}

// TestFeedCompactJSON_QuotesSymbolFilter op=quotes 的 symbol 精确过滤。
func TestFeedCompactJSON_QuotesSymbolFilter(t *testing.T) {
	f := sampleFeed()
	// 命中(大小写不敏感)。
	text, err := FeedCompactJSON(f, "quotes", "", "", "btc")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "BTC") || strings.Contains(text, "上证指数") {
		t.Errorf("symbol filter failed: %s", text)
	}
	// 未命中 ⇒ 空数组。
	text, err = FeedCompactJSON(f, "quotes", "", "", "NOPE")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, `"quotes":[]`) {
		t.Errorf("non-matching symbol should yield empty quotes: %s", text)
	}
}

// TestFeedCompactJSON_History op=history:symbol 必填;命中返价点;未命中返空。
func TestFeedCompactJSON_History(t *testing.T) {
	st := NewFeedStore(12, 20)
	st.MergeMissionResult(1000, &MissionPayload{Category: CategoryAShare, Source: "腾讯行情",
		Quotes: []RealtimeQuote{quote("000001", "上证指数", 3658.03, -1.48)}})
	st.MergeMissionResult(2000, &MissionPayload{Category: CategoryAShare, Source: "腾讯行情",
		Quotes: []RealtimeQuote{quote("000001", "上证指数", 3660.00, -1.2)}})
	f := st.Snapshot()

	text, err := FeedCompactJSON(f, "history", "", "", "000001")
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Symbol     string  `json:"symbol"`
		Category   string  `json:"category"`
		Price      float64 `json:"price"`
		PointCount int     `json:"point_count"`
		Points     []struct {
			T int64   `json:"t"`
			P float64 `json:"p"`
		} `json:"points"`
		SpanSeconds int64 `json:"span_seconds"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("bad json: %v (%s)", err, text)
	}
	if out.Category != CategoryAShare {
		t.Errorf("category = %q, want a_share", out.Category)
	}
	if out.PointCount != 2 || len(out.Points) != 2 {
		t.Fatalf("points = %d, want 2: %s", out.PointCount, text)
	}
	if out.Points[0].T != 1000 || out.Points[1].T != 2000 {
		t.Errorf("points timestamps = %+v", out.Points)
	}
	if out.SpanSeconds != 1000 {
		t.Errorf("span_seconds = %d, want 1000", out.SpanSeconds)
	}
	if out.Price != 3660.00 {
		t.Errorf("price = %v, want 3660", out.Price)
	}
	// 未命中 ⇒ points 为空数组。
	text, err = FeedCompactJSON(f, "history", "", "", "NOPE")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, `"points":[]`) {
		t.Errorf("unmatched symbol should yield empty points: %s", text)
	}
	// symbol 缺省 ⇒ 错误文本(不 panic)。
	if _, err := FeedCompactJSON(f, "history", "", "", ""); err == nil {
		t.Error("history without symbol should error")
	}
}

// TestSourcesCompactJSON op=sources:采集源清单 + 类目健康度(遥测聚合)。
func TestSourcesCompactJSON(t *testing.T) {
	mlog := NewMissionLog()
	mlog.Append(MissionRecord{ID: "s-1", Category: CategoryAShare, Status: MissionStatusOK,
		Source: "腾讯行情·A股指数", StartedAt: 1000, DurationMs: 2000})
	mlog.Append(MissionRecord{ID: "s-2", Category: CategoryAShare, Status: MissionStatusFail,
		Error: "超时", StartedAt: 900})
	text, err := SourcesCompactJSON(mlog, "")
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Categories []struct {
			Category string `json:"category"`
			Sites    []struct {
				Name string `json:"name"`
				URL  string `json:"url"`
			} `json:"sites"`
			LastSource string `json:"last_source"`
			LastOKAt   int64  `json:"last_ok_at"`
			AgeSeconds int64  `json:"age_seconds"`
			OK         int    `json:"ok"`
			Fail       int    `json:"fail"`
		} `json:"categories"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("bad json: %v (%s)", err, text)
	}
	if len(out.Categories) == 0 {
		t.Fatalf("sources categories empty: %s", text)
	}
	var aShare *struct {
		Category string `json:"category"`
		Sites    []struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"sites"`
		LastSource string `json:"last_source"`
		LastOKAt   int64  `json:"last_ok_at"`
		AgeSeconds int64  `json:"age_seconds"`
		OK         int    `json:"ok"`
		Fail       int    `json:"fail"`
	}
	for i := range out.Categories {
		if out.Categories[i].Category == CategoryAShare {
			aShare = &out.Categories[i]
		}
	}
	if aShare == nil {
		t.Fatalf("a_share category missing: %s", text)
	}
	if len(aShare.Sites) < 3 {
		t.Errorf("a_share sites = %d, want >= 3", len(aShare.Sites))
	}
	if aShare.LastSource != "腾讯行情·A股指数" {
		t.Errorf("last_source = %q", aShare.LastSource)
	}
	if aShare.OK != 1 || aShare.Fail != 1 {
		t.Errorf("ok/fail = %d/%d, want 1/1", aShare.OK, aShare.Fail)
	}
	if aShare.LastOKAt != 1002 {
		t.Errorf("last_ok_at = %d, want 1002 (started_at+duration)", aShare.LastOKAt)
	}
	// 类目过滤。
	text2, err := SourcesCompactJSON(mlog, CategoryMetal)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text2, `"category":"a_share"`) {
		t.Errorf("category filter failed: %s", text2)
	}
	if !strings.Contains(text2, "fx678.com") {
		t.Errorf("metal sites should include 金投网 fx678.com: %s", text2)
	}
}

// TestFeedCompactJSON_OpSources op=sources 经 FeedCompactJSON 也能取到清单
// (健康度需 MissionLog,由 Service.RealworldInfo 注入)。
func TestFeedCompactJSON_OpSources(t *testing.T) {
	text, err := FeedCompactJSON(sampleFeed(), "sources", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, `"categories"`) || !strings.Contains(text, `"sites"`) {
		t.Errorf("sources shape mismatch: %s", text)
	}
}
