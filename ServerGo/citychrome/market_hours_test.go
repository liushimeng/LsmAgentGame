// Package citychrome — market_hours_test.go: 交易时段感知单测(批次88
// §三.2/§五.6)。全部为固定时刻纯函数断言,零时钟依赖、可复现。
//
// 日历锚点(UTC):2026-03-02 周一 / 03-06 周五 / 03-07 周六 / 03-08 周日
// (美东夏令时切换日)/ 03-09 周一(EDT)/ 10-30 周五(EDT)/ 11-01 周日
// (美东冬令时切换日)/ 11-02 周一(EST)。
package citychrome

import (
	"encoding/json"
	"testing"
	"time"
)

func utc(y int, m time.Month, d, hh, mm, ss int) time.Time {
	return time.Date(y, m, d, hh, mm, ss, 0, time.UTC)
}

// TestCategoryMarketOpen_AShare 上交所/深交所(UTC+8):周一~五
// 09:30-11:30 / 13:00-15:00,午休休市,区间左闭右开。
func TestCategoryMarketOpen_AShare(t *testing.T) {
	cases := []struct {
		name string
		now  time.Time
		want bool
	}{
		{"周一盘中 10:00 CST", utc(2026, 3, 2, 2, 0, 0), true},
		{"开盘前 1 分钟 09:29 CST", utc(2026, 3, 2, 1, 29, 0), false},
		{"开盘边界 09:30 CST(左闭)", utc(2026, 3, 2, 1, 30, 0), true},
		{"午盘前 1 秒 11:29:59 CST", utc(2026, 3, 2, 3, 29, 59), true},
		{"午盘收盘边界 11:30 CST(右开⇒休市)", utc(2026, 3, 2, 3, 30, 0), false},
		{"午休 12:00 CST(右开⇒休市)", utc(2026, 3, 2, 4, 0, 0), false},
		{"午休 11:45 CST", utc(2026, 3, 2, 3, 45, 0), false},
		{"午后开盘 13:00 CST(左闭)", utc(2026, 3, 2, 5, 0, 0), true},
		{"午后盘中 13:30 CST", utc(2026, 3, 2, 5, 30, 0), true},
		{"收盘边界 15:00 CST(右开⇒休市)", utc(2026, 3, 2, 7, 0, 0), false},
		{"收盘前 1 秒 14:59:59 CST", utc(2026, 3, 2, 6, 59, 59), true},
		{"周五收盘后 1 分钟 15:01 CST", utc(2026, 3, 6, 7, 1, 0), false},
		{"周一开盘前 1 分钟 09:29 CST", utc(2026, 3, 2, 1, 29, 0), false},
		{"周六全天休市", utc(2026, 3, 7, 3, 0, 0), false},
		{"周日休市", utc(2026, 3, 8, 3, 0, 0), false},
		{"周五夜间 21:00 CST", utc(2026, 3, 6, 13, 0, 0), false},
	}
	for _, c := range cases {
		if got := CategoryMarketOpen(CategoryAShare, c.now); got != c.want {
			t.Errorf("a_share %s (%v UTC): got %v, want %v", c.name, c.now, got, c.want)
		}
	}
}

// TestCategoryMarketOpen_HK 港交所(UTC+8):周一~五 09:30-12:00 /
// 13:00-16:00,午休 12:30 应判休市。
func TestCategoryMarketOpen_HK(t *testing.T) {
	cases := []struct {
		name string
		now  time.Time
		want bool
	}{
		{"上午盘中 10:00 CST", utc(2026, 3, 2, 2, 0, 0), true},
		{"上午收盘边界 12:00 CST(右开⇒休市)", utc(2026, 3, 2, 4, 0, 0), false},
		{"上午收盘前 1 分钟 11:59 CST", utc(2026, 3, 2, 3, 59, 0), true},
		{"午休 12:30 CST", utc(2026, 3, 2, 4, 30, 0), false},
		{"午后开盘 13:00 CST(左闭)", utc(2026, 3, 2, 5, 0, 0), true},
		{"收盘边界 16:00 CST(右开⇒休市)", utc(2026, 3, 2, 8, 0, 0), false},
		{"收盘前 1 分钟 15:59 CST", utc(2026, 3, 2, 7, 59, 0), true},
		{"周六休市", utc(2026, 3, 7, 3, 0, 0), false},
		{"周日休市", utc(2026, 3, 8, 5, 0, 0), false},
	}
	for _, c := range cases {
		if got := CategoryMarketOpen(CategoryHK, c.now); got != c.want {
			t.Errorf("hk %s (%v UTC): got %v, want %v", c.name, c.now, got, c.want)
		}
	}
}

// TestCategoryMarketOpen_US 纽交所/纳斯达克(美东 09:30-16:00):
// EDT 期 UTC 13:30-20:00 开市,EST 期 UTC 14:30-21:00 开市。
func TestCategoryMarketOpen_US(t *testing.T) {
	cases := []struct {
		name string
		now  time.Time
		want bool
	}{
		// EDT 期(2026-03-08 07:00 UTC 后 ~ 11-01 06:00 UTC 前)。
		{"EDT 期 UTC 13:45 周一 = 09:45 EDT 开市", utc(2026, 3, 9, 13, 45, 0), true},
		{"EDT 期开盘边界 13:30 UTC(左闭)", utc(2026, 3, 9, 13, 30, 0), true},
		{"EDT 期开盘前 1 分钟 13:29 UTC", utc(2026, 3, 9, 13, 29, 0), false},
		{"EDT 期收盘边界 20:00 UTC(右开⇒休市)", utc(2026, 3, 9, 20, 0, 0), false},
		{"EDT 期收盘前 1 分钟 19:59 UTC", utc(2026, 3, 9, 19, 59, 0), true},
		{"EDT 期周五 UTC 13:45 = 09:45 EDT 开市", utc(2026, 10, 30, 13, 45, 0), true},
		// EST 期(2026-03-08 07:00 UTC 前 / 11-01 06:00 UTC 后)。
		{"EST 期 UTC 13:45 周五 = 08:45 EST 未开", utc(2026, 3, 6, 13, 45, 0), false},
		{"EST 期 UTC 13:45 周一 = 08:45 EST 未开", utc(2026, 11, 2, 13, 45, 0), false},
		{"EST 期开盘边界 14:30 UTC(左闭)", utc(2026, 11, 2, 14, 30, 0), true},
		{"EST 期开盘前 1 分钟 14:29 UTC", utc(2026, 11, 2, 14, 29, 0), false},
		{"EST 期收盘边界 21:00 UTC(右开⇒休市)", utc(2026, 11, 2, 21, 0, 0), false},
		{"EST 期收盘前 1 分钟 20:59 UTC", utc(2026, 11, 2, 20, 59, 0), true},
		// 周末。
		{"周六休市(EDT 期)", utc(2026, 3, 7, 15, 0, 0), false},
		{"周日休市(EST 期)", utc(2026, 3, 1, 15, 0, 0), false},
	}
	for _, c := range cases {
		if got := CategoryMarketOpen(CategoryUS, c.now); got != c.want {
			t.Errorf("us %s (%v UTC): got %v, want %v", c.name, c.now, got, c.want)
		}
	}
}

// TestUsEasternDaylight_Boundaries 美东夏令时边界(纯函数):3 月第二个周日
// 07:00 UTC 起(左闭)~ 11 月第一个周日 06:00 UTC 止(右开)。
func TestUsEasternDaylight_Boundaries(t *testing.T) {
	cases := []struct {
		name string
		now  time.Time
		want bool
	}{
		{"切换前 1 秒(2026-03-08 06:59:59)", utc(2026, 3, 8, 6, 59, 59), false},
		{"切换瞬间(2026-03-08 07:00:00,左闭)", utc(2026, 3, 8, 7, 0, 0), true},
		{"切换前日(2026-03-07 12:00)", utc(2026, 3, 7, 12, 0, 0), false},
		{"切换次日(2026-03-09 12:00)", utc(2026, 3, 9, 12, 0, 0), true},
		{"盛夏(2026-07-01)", utc(2026, 7, 1, 12, 0, 0), true},
		{"切换前 1 秒(2026-11-01 05:59:59)", utc(2026, 11, 1, 5, 59, 59), true},
		{"切换瞬间(2026-11-01 06:00:00,右开⇒冬令时)", utc(2026, 11, 1, 6, 0, 0), false},
		{"切换次日(2026-11-02 12:00)", utc(2026, 11, 2, 12, 0, 0), false},
		{"隆冬(2026-01-15)", utc(2026, 1, 15, 12, 0, 0), false},
	}
	for _, c := range cases {
		if got := usEasternDaylight(c.now); got != c.want {
			t.Errorf("usEasternDaylight %s (%v): got %v, want %v", c.name, c.now, got, c.want)
		}
	}
}

// TestNthWeekdayUTC 某月第 N 个星期几的 UTC 推算(DST 边界判定的基元)。
func TestNthWeekdayUTC(t *testing.T) {
	cases := []struct {
		year  int
		month time.Month
		wd    time.Weekday
		n     int
		want  time.Time
	}{
		{2026, time.March, time.Sunday, 2, utc(2026, 3, 8, 0, 0, 0)},     // 美东夏令时起点
		{2026, time.November, time.Sunday, 1, utc(2026, 11, 1, 0, 0, 0)}, // 美东冬令时起点
		{2027, time.March, time.Sunday, 2, utc(2027, 3, 14, 0, 0, 0)},
		{2027, time.November, time.Sunday, 1, utc(2027, 11, 7, 0, 0, 0)},
		{2026, time.March, time.Sunday, 1, utc(2026, 3, 1, 0, 0, 0)},
	}
	for _, c := range cases {
		got := nthWeekdayUTC(c.year, c.month, c.wd, c.n)
		if !got.Equal(c.want) {
			t.Errorf("nthWeekdayUTC(%d,%v,%v,%d) = %v, want %v",
				c.year, c.month, c.wd, c.n, got, c.want)
		}
	}
}

// TestCategoryMarketOpen_MetalFutures 外盘 CME Globex 近 24 小时口径(UTC):
// 周日 22:00 开市(左闭)~ 周五 21:00 收盘(右开);周六全天休市。
// metal/futures 两值同表,逐一断言。
func TestCategoryMarketOpen_MetalFutures(t *testing.T) {
	cases := []struct {
		name string
		now  time.Time
		want bool
	}{
		{"周中 12:00 UTC 开市", utc(2026, 3, 4, 12, 0, 0), true},
		{"周六 12:00 UTC 休市", utc(2026, 3, 7, 12, 0, 0), false},
		{"周六凌晨 02:00 UTC 休市", utc(2026, 3, 7, 2, 0, 0), false},
		{"周日 21:59 UTC 未开(22:00 前休市)", utc(2026, 3, 8, 21, 59, 0), false},
		{"周日 22:00 UTC 开市(左闭)", utc(2026, 3, 8, 22, 0, 0), true},
		{"周五 20:59 UTC 开市", utc(2026, 3, 6, 20, 59, 0), true},
		{"周五 21:00 UTC 收盘(右开⇒休市)", utc(2026, 3, 6, 21, 0, 0), false},
		{"周五 23:00 UTC 休市", utc(2026, 3, 6, 23, 0, 0), false},
	}
	for _, cat := range []string{CategoryMetal, CategoryFutures} {
		for _, c := range cases {
			if got := CategoryMarketOpen(cat, c.now); got != c.want {
				t.Errorf("%s %s (%v UTC): got %v, want %v", cat, c.name, c.now, got, c.want)
			}
		}
	}
}

// TestCategoryMarketOpen_Crypto7x24 crypto 恒开市(含周末与深夜)。
func TestCategoryMarketOpen_Crypto7x24(t *testing.T) {
	for _, now := range []time.Time{
		utc(2026, 3, 2, 2, 0, 0),   // 周一盘中
		utc(2026, 3, 7, 3, 0, 0),   // 周六凌晨
		utc(2026, 3, 8, 21, 30, 0), // 周日(metal/futures 未开)
		utc(2026, 11, 1, 5, 59, 0), // DST 切换瞬间附近
	} {
		if !CategoryMarketOpen(CategoryCrypto, now) {
			t.Errorf("crypto must be open 7×24, got closed at %v", now)
		}
	}
}

// TestCategoryMarketOpen_NewsMacroUnconstrained news/macro 不参与时段判定
// (恒 true);未知类目同此处理。
func TestCategoryMarketOpen_NewsMacroUnconstrained(t *testing.T) {
	for _, cat := range []string{CategoryNews, CategoryMacro, CategoryGeneral, "unknown"} {
		for _, now := range []time.Time{
			utc(2026, 3, 7, 3, 0, 0), // 周六
			utc(2026, 3, 2, 4, 0, 0), // A股午休
		} {
			if !CategoryMarketOpen(cat, now) {
				t.Errorf("%s must be unconstrained, got closed at %v", cat, now)
			}
		}
	}
}

// TestQuoteRefreshSeconds_Tiers 档位:行情开市 600s / 休市 7200s;
// news/macro 固定 600/86400。categoryRefreshInterval 与之同口径。
func TestQuoteRefreshSeconds_Tiers(t *testing.T) {
	openNow := utc(2026, 3, 2, 2, 0, 0)       // 周一 10:00 CST,A股开市
	lunchNow := utc(2026, 3, 2, 4, 0, 0)      // 周一午休(A股休市)
	satNow := utc(2026, 3, 7, 12, 0, 0)       // 周六(全市场休市)
	usESTClosed := utc(2026, 3, 6, 13, 45, 0) // 周五 08:45 EST,美股未开

	cases := []struct {
		cat  string
		now  time.Time
		want int64
	}{
		{CategoryAShare, openNow, 600},
		{CategoryAShare, lunchNow, 7200},
		{CategoryAShare, satNow, 7200},
		{CategoryUS, usESTClosed, 7200},
		{CategoryUS, utc(2026, 3, 9, 13, 45, 0), 600}, // 09:45 EDT 开市
		{CategoryHK, openNow, 600},
		{CategoryMetal, satNow, 7200},
		{CategoryFutures, utc(2026, 3, 8, 22, 0, 0), 600}, // 周日 22:00 开市
		{CategoryCrypto, satNow, 600},
		{CategoryNews, satNow, 600},
		{CategoryMacro, openNow, 86400},
	}
	for _, c := range cases {
		got := QuoteRefreshSeconds(c.cat, c.now)
		if got != c.want {
			t.Errorf("QuoteRefreshSeconds(%s, %v) = %d, want %d", c.cat, c.now, got, c.want)
		}
		if got2 := categoryRefreshInterval(c.cat, c.now); got2 != got {
			t.Errorf("categoryRefreshInterval(%s, %v) = %d, want same %d",
				c.cat, c.now, got2, got)
		}
	}
}

// TestRealworldStale_MarketHoursAware 批次88 stale 同口径:age 介于
// 600×2 与 7200×2 之间(2h)时,休市时段 stale=false(不误报),
// 开市时段 stale=true。age_seconds 与档位取同一时刻(compactOverview 注入)。
func TestRealworldStale_MarketHoursAware(t *testing.T) {
	type staleCat struct {
		AgeSeconds int64 `json:"age_seconds"`
		Stale      bool  `json:"stale"`
	}
	newFeed := func(updatedAt int64) Feed {
		return Feed{
			Channel: ChannelStatus{Status: ChannelOnline},
			Categories: map[string]RealtimeCategory{
				CategoryAShare: {UpdatedAt: updatedAt, Source: "腾讯行情",
					Quotes: []RealtimeQuote{quote("000001", "上证指数", 3658.03, -1.48)}},
			},
		}
	}
	openNow := utc(2026, 3, 2, 2, 0, 0)    // 周一 10:00 CST,A股开市(档位 600)
	closedNow := utc(2026, 3, 7, 12, 0, 0) // 周六(档位 7200)
	const age = 7200                       // 600×2 < 7200 < 7200×2

	run := func(now, updatedAt time.Time, tiers IntervalTiers) staleCat {
		t.Helper()
		text, err := compactOverview(newFeed(updatedAt.Unix()), "", now, tiers)
		if err != nil {
			t.Fatal(err)
		}
		var out struct {
			Categories map[string]staleCat `json:"categories"`
		}
		if err := json.Unmarshal([]byte(text), &out); err != nil {
			t.Fatalf("bad json: %v (%s)", err, text)
		}
		return out.Categories[CategoryAShare]
	}

	// 开市:档位 600 ⇒ stale 阈值 1200s,2h 应报 stale。
	got := run(openNow, openNow.Add(-age*time.Second), IntervalTiers{})
	if got.AgeSeconds != age {
		t.Errorf("open age_seconds = %d, want %d", got.AgeSeconds, age)
	}
	if !got.Stale {
		t.Errorf("open-hours stale must be true (age %d > 2×600)", got.AgeSeconds)
	}
	// 休市:档位 7200 ⇒ stale 阈值 14400s,2h 不得误报。
	got = run(closedNow, closedNow.Add(-age*time.Second), IntervalTiers{})
	if got.AgeSeconds != age {
		t.Errorf("closed age_seconds = %d, want %d", got.AgeSeconds, age)
	}
	if got.Stale {
		t.Errorf("closed-hours stale must be false (age %d <= 2×7200)", got.AgeSeconds)
	}
	// 休市但真的太旧(> 2×7200)仍报 stale。
	got = run(closedNow, closedNow.Add(-15000*time.Second), IntervalTiers{})
	if !got.Stale {
		t.Error("closed-hours age 15000s > 2×7200 must be stale")
	}
}

// TestRealworldStale_ConfiguredTiers 批次88 返工:stale 阈值吃**配置档位**,
// 与 Scheduler.categoryInterval(调度真实轮转间隔)逐档一致 —— 任意合法配置
// 下 stale = age > 2×调度间隔(同一时刻、同一档位、同一解析路径)。
// 非默认档位 open=300 / closed=3600(另配 news=1200 / macro=72000)。
func TestRealworldStale_ConfiguredTiers(t *testing.T) {
	tiers := IntervalTiers{
		QuoteOpenSeconds:   300,
		QuoteClosedSeconds: 3600,
		NewsSeconds:        1200,
		MacroSeconds:       72000,
	}
	openNow := utc(2026, 3, 2, 2, 0, 0)    // 周一 10:00 CST,A股开市(档位 300)
	closedNow := utc(2026, 3, 7, 12, 0, 0) // 周六(档位 3600)

	// ① 与调度 interval 逐档一致(开市/休市各一例 + news/macro)。
	s := NewScheduler(SchedulerConfig{
		QuoteOpenSeconds:    300,
		QuoteClosedSeconds:  3600,
		NewsRefreshSeconds:  1200,
		MacroRefreshSeconds: 72000,
		QueueCapacity:       1,
	}, NewFeedStore(1, 1), nil)
	for _, now := range []time.Time{openNow, closedNow} {
		for _, cat := range []string{CategoryAShare, CategoryHK, CategoryUS,
			CategoryMetal, CategoryCrypto, CategoryNews, CategoryMacro} {
			want := time.Duration(tiers.Seconds(cat, now)) * time.Second
			if got := s.categoryInterval(cat, now); got != want {
				t.Errorf("调度 interval 与 stale 档位不同口径: cat=%s now=%v got=%v want=%v",
					cat, now, got, want)
			}
		}
	}

	// ② stale 判定逐档断言(注入 now;解析 JSON 取 stale)。
	type staleCat struct {
		AgeSeconds int64 `json:"age_seconds"`
		Stale      bool  `json:"stale"`
	}
	staleAt := func(now, updatedAt time.Time, tiers IntervalTiers) staleCat {
		t.Helper()
		f := Feed{
			Channel: ChannelStatus{Status: ChannelOnline},
			Categories: map[string]RealtimeCategory{
				CategoryAShare: {UpdatedAt: updatedAt.Unix(), Source: "腾讯行情",
					Quotes: []RealtimeQuote{quote("000001", "上证指数", 3658.03, -1.48)}},
			},
		}
		text, err := compactOverview(f, "", now, tiers)
		if err != nil {
			t.Fatal(err)
		}
		var out struct {
			Categories map[string]staleCat `json:"categories"`
		}
		if err := json.Unmarshal([]byte(text), &out); err != nil {
			t.Fatalf("bad json: %v (%s)", err, text)
		}
		return out.Categories[CategoryAShare]
	}
	// 开市档 300s ⇒ 阈值 600s:age 500 不 stale;age 700 stale。
	if got := staleAt(openNow, openNow.Add(-500*time.Second), tiers); got.Stale {
		t.Errorf("open-hours age 500s <= 2×300 must not be stale: %+v", got)
	}
	if got := staleAt(openNow, openNow.Add(-700*time.Second), tiers); !got.Stale {
		t.Errorf("open-hours age 700s > 2×300 must be stale: %+v", got)
	}
	// 休市档 3600s ⇒ 阈值 7200s:age 5000 不 stale;age 8000 stale。
	if got := staleAt(closedNow, closedNow.Add(-5000*time.Second), tiers); got.Stale {
		t.Errorf("closed-hours age 5000s <= 2×3600 must not be stale: %+v", got)
	}
	if got := staleAt(closedNow, closedNow.Add(-8000*time.Second), tiers); !got.Stale {
		t.Errorf("closed-hours age 8000s > 2×3600 must be stale: %+v", got)
	}
}
