// Package citychrome — sites_batch76_test.go: 批次76 新类目(macro/news)
// 配置完整性守护 —— MissionCategories 每个类目必须有非空站点表/目标/提取
// 配方(§134「全链路补全」:上轮转目录的角色必须有完整实现)。
package citychrome

import (
	"strings"
	"testing"
)

// TestMissionCategoriesConfigComplete 遍历 8 个轮转类目,逐项断言
// Sites/Goal/Hints 三件套齐备且首选源 URL 合法(https 开头)。
func TestMissionCategoriesConfigComplete(t *testing.T) {
	if len(MissionCategories) != 8 {
		t.Fatalf("MissionCategories len = %d, want 8 (批次76)", len(MissionCategories))
	}
	for _, cat := range MissionCategories {
		sites := categorySites[cat]
		if len(sites) < 3 {
			t.Errorf("category %s: %d sites, want >= 3", cat, len(sites))
		}
		for i, s := range sites {
			if s.Name == "" || !strings.HasPrefix(s.URL, "https://") {
				t.Errorf("category %s site[%d] invalid: %+v", cat, i, s)
			}
		}
		if categoryGoals[cat] == "" {
			t.Errorf("category %s: empty Goal", cat)
		}
		if categoryHints[cat] == "" {
			t.Errorf("category %s: empty Hints", cat)
		}
	}
}

// TestBuildMissionNewCategories 批次76 新类目的 mission 构造:首选源 = Sites[0]
// (macro=国家统计局,news=财联社电报),Goal/Hints 注入非空。
func TestBuildMissionNewCategories(t *testing.T) {
	macro := BuildMission(CategoryMacro)
	if macro.TargetURL != "https://data.eastmoney.com/cjsj/" {
		t.Errorf("macro target = %q, want data.eastmoney.com/cjsj", macro.TargetURL)
	}
	if macro.Goal == "" || macro.Hints == "" || len(macro.Sites) != 4 {
		t.Errorf("macro mission incomplete: %+v", macro)
	}
	news := BuildMission(CategoryNews)
	if news.TargetURL != "https://www.cls.cn/telegraph" {
		t.Errorf("news target = %q, want cls.cn/telegraph", news.TargetURL)
	}
	if news.Goal == "" || news.Hints == "" || len(news.Sites) != 5 {
		t.Errorf("news mission incomplete: %+v", news)
	}
}

// TestNewsOnlyPayloadAccepted news 类目纯新闻 payload(quotes 空)经
// ValidatePayload 校验通过(批次76 §2.1:news 不产出 quotes)。
func TestNewsOnlyPayloadAccepted(t *testing.T) {
	raw := map[string]any{
		"category": CategoryNews,
		"source":   "财联社·电报",
		"quotes":   []any{},
		"news": []any{
			map[string]any{"title": "测试快讯", "url": "https://www.cls.cn/detail/1",
				"published_at": int64(1_700_000_000)},
		},
	}
	p, err := ValidatePayload(12, raw)
	if err != nil {
		t.Fatalf("news-only payload rejected: %v", err)
	}
	if len(p.Quotes) != 0 || len(p.News) != 1 {
		t.Errorf("payload = %d quotes / %d news, want 0/1", len(p.Quotes), len(p.News))
	}
	// news 条目的 category 由 FeedStore.MergeMissionResult 回填(feed_store.go
	// 「n.Category == \"\" ⇒ p.Category」),ValidatePayload 层不注入。
	if p.News[0].Category != "" {
		t.Errorf("news[0].category = %q, want empty before merge", p.News[0].Category)
	}
	// 合并后应回填为 mission 类目 news,并可被 realworld 按类目筛出。
	st := NewFeedStore(12, 20)
	st.MergeMissionResult(1_700_000_000, p)
	merged := st.Snapshot().News
	if len(merged) != 1 || merged[0].Category != CategoryNews {
		t.Errorf("merged news = %+v, want 1 entry with category %q", merged, CategoryNews)
	}
	// source 回填 mission 站点名(Agent 常省略逐条来源)。
	if merged[0].Source != "财联社·电报" {
		t.Errorf("news[0].source = %q, want mission source backfilled", merged[0].Source)
	}
	// 纯新闻 mission 不得写入 categories(契约 76 §2.1)。
	if cats := st.Snapshot().Categories; len(cats) != 0 {
		t.Errorf("pure-news mission created categories %v, want none", keysOf(cats))
	}
	// 行情 mission 全脏数据 ⇒ 不用空类目覆盖旧数据。
	old := NewFeedStore(12, 20)
	old.MergeMissionResult(1, &MissionPayload{Category: CategoryAShare, Source: "s",
		Quotes: []RealtimeQuote{quote("000001", "上证指数", 3200, 1)}})
	old.MergeMissionResult(2, &MissionPayload{Category: CategoryAShare, Source: "s2",
		Quotes: []RealtimeQuote{{Symbol: "", Name: "", Price: -1}}})
	if got := old.Snapshot().Categories[CategoryAShare]; len(got.Quotes) != 1 || got.Source != "s" {
		t.Errorf("all-dirty mission overwrote category: %+v", got)
	}
}

// keysOf 返回 map 键集合(测试排序用)。
func keysOf(m map[string]RealtimeCategory) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestMacroQuotesPayloadAccepted macro 类目指标映射 payload(LPR/CPI)通过校验。
func TestMacroQuotesPayloadAccepted(t *testing.T) {
	raw := map[string]any{
		"category": CategoryMacro,
		"source":   "国家统计局·数据发布",
		"quotes": []any{
			map[string]any{"symbol": "LPR_1Y", "name": "1年期LPR", "price": 3.0, "change_pct": 0},
			map[string]any{"symbol": "CPI_YOY", "name": "CPI同比", "price": 0.4, "change_pct": -0.1},
		},
	}
	p, err := ValidatePayload(12, raw)
	if err != nil {
		t.Fatalf("macro payload rejected: %v", err)
	}
	if len(p.Quotes) != 2 {
		t.Fatalf("quotes = %d, want 2", len(p.Quotes))
	}
	if p.Quotes[0].Symbol != "LPR_1Y" || p.Quotes[1].Price != 0.4 {
		t.Errorf("macro quotes malformed: %+v", p.Quotes)
	}
}

// TestFeedCompactJSONNewCategories realworld_info 查询新类目(macro/news)通过,
// 未知类目报错文案含 8 类目清单。
func TestFeedCompactJSONNewCategories(t *testing.T) {
	f := Feed{
		Channel: ChannelStatus{Status: ChannelOnline},
		Categories: map[string]RealtimeCategory{
			CategoryMacro: {Source: "国家统计局", Quotes: []RealtimeQuote{
				{Symbol: "CPI_YOY", Name: "CPI同比", Price: 0.4, ChangePct: -0.1},
			}},
		},
		News: []RealtimeNews{{ID: "x", Title: "t", Source: "财联社·电报", Category: CategoryNews}},
	}
	if _, err := FeedCompactJSON(f, "quotes", CategoryMacro); err != nil {
		t.Errorf("quotes macro: %v", err)
	}
	if _, err := FeedCompactJSON(f, "news", CategoryNews); err != nil {
		t.Errorf("news news: %v", err)
	}
	_, err := FeedCompactJSON(f, "quotes", "bogus")
	if err == nil || !strings.Contains(err.Error(), "macro") || !strings.Contains(err.Error(), "news") {
		t.Errorf("bogus category error should list 8 categories, got: %v", err)
	}
}
