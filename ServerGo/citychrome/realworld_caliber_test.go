// Package citychrome — realworld_caliber_test.go: 批次89 收口增强单测
// (口径卡目录随 realworld_info op=sources 下发给 Agent,报价行带 spec=)。
//
// 覆盖:
//
//	① op=overview|quotes 的每条 quote 带 spec 字段(命中口径卡 ⇒ 等于该
//	   报价的 spec_id;未命中 ⇒ 空串);
//	② op=sources 返回体含 calibers 目录(条数 = 口径卡总数、id 唯一、
//	   每条 caliber/formula 非空);
//	③ op=sources 的 category 过滤**不**过滤 calibers(口径卡是全局静态目录)。
package citychrome

import (
	"encoding/json"
	"testing"
	"time"

	"LsmAgentGame/config"
)

// b89CaliberFeed 构造含「命中口径卡」与「未命中口径卡」两条报价的 Feed。
func b89CaliberFeed(now int64) Feed {
	return Feed{
		Channel: ChannelStatus{Status: ChannelOnline},
		Categories: map[string]RealtimeCategory{
			CategoryAShare: {UpdatedAt: now, Source: "腾讯行情", Quotes: []RealtimeQuote{
				{Symbol: "000001", Name: "上证指数", Price: 3245.67, ChangePct: 0.82, UpdatedAt: now},
			}},
			CategoryCrypto: {UpdatedAt: now, Source: "CoinGecko", Quotes: []RealtimeQuote{
				{Symbol: "ZZZZ", Name: "查无此币", Price: 1.23, ChangePct: 0.1, UpdatedAt: now},
			}},
		},
	}
}

// TestFeedCompactJSON_QuoteSpec op=quotes 每条 quote 带 spec 字段:
// 命中口径卡 ⇒ 等于该报价的 spec_id;未命中 ⇒ 空串。
func TestFeedCompactJSON_QuoteSpec(t *testing.T) {
	now := time.Now().Unix()
	f := b89CaliberFeed(now)
	// 先取后端派生的 spec_id(基准)。
	derived := DeriveQuoteQuality(f, time.Unix(now, 0), IntervalTiers{})
	wantHit := derived.Categories[CategoryAShare].Quotes[0].SpecID
	wantMiss := derived.Categories[CategoryCrypto].Quotes[0].SpecID
	if wantHit == "" {
		t.Fatal("000001/上证指数 应命中口径卡,spec_id 不得为空")
	}
	if wantMiss != "" {
		t.Fatalf("ZZZZ/查无此币 不应命中口径卡,spec_id = %q", wantMiss)
	}

	text, err := FeedCompactJSON(f, "quotes", "", "", "", IntervalTiers{})
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Quotes []struct {
			Category string `json:"category"`
			Symbol   string `json:"symbol"`
			Spec     string `json:"spec"`
		} `json:"quotes"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("bad json: %v (%s)", err, text)
	}
	got := map[string]string{}
	for _, q := range out.Quotes {
		got[q.Category+"/"+q.Symbol] = q.Spec
	}
	if got[CategoryAShare+"/000001"] != wantHit {
		t.Errorf("hit quote spec = %q, want %q", got[CategoryAShare+"/000001"], wantHit)
	}
	if got[CategoryCrypto+"/ZZZZ"] != "" {
		t.Errorf("miss quote spec = %q, want empty", got[CategoryCrypto+"/ZZZZ"])
	}
}

// TestFeedCompactJSON_OverviewSpec op=overview 的头条报价同样带 spec。
func TestFeedCompactJSON_OverviewSpec(t *testing.T) {
	now := time.Now().Unix()
	f := b89CaliberFeed(now)
	text, err := FeedCompactJSON(f, "overview", "", "", "", IntervalTiers{})
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Categories map[string]struct {
			Top []struct {
				Symbol string `json:"symbol"`
				Spec   string `json:"spec"`
			} `json:"top"`
		} `json:"categories"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("bad json: %v (%s)", err, text)
	}
	top := out.Categories[CategoryAShare].Top
	if len(top) != 1 {
		t.Fatalf("a_share top = %d, want 1", len(top))
	}
	if top[0].Spec == "" {
		t.Error("overview 头条报价应带 spec(命中口径卡)")
	}
}

// TestFeedCompactJSON_SourcesCalibers op=sources 返回体含 calibers 目录:
// 条数 = 口径卡总数、id 唯一、每条 caliber 与 formula 非空。
func TestFeedCompactJSON_SourcesCalibers(t *testing.T) {
	text, err := FeedCompactJSON(b89CaliberFeed(time.Now().Unix()), "sources", "", "", "", IntervalTiers{})
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Categories []json.RawMessage `json:"categories"`
		Calibers   []struct {
			ID        string `json:"id"`
			Category  string `json:"category"`
			Indicator string `json:"indicator"`
			Unit      string `json:"unit"`
			Caliber   string `json:"caliber"`
			Formula   string `json:"formula"`
			Source    string `json:"source"`
			SourceURL string `json:"source_url"`
		} `json:"calibers"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("bad json: %v (%s)", err, text)
	}
	total := len(MetricSpecs())
	if len(out.Calibers) != total {
		t.Errorf("calibers = %d, want %d (口径卡全量)", len(out.Calibers), total)
	}
	ids := map[string]bool{}
	for _, c := range out.Calibers {
		if c.ID == "" {
			t.Error("caliber id 不得为空")
		}
		if ids[c.ID] {
			t.Errorf("duplicate caliber id %q", c.ID)
		}
		ids[c.ID] = true
		if c.Caliber == "" || c.Formula == "" {
			t.Errorf("caliber %q 缺 caliber/formula: %+v", c.ID, c)
		}
		if c.Indicator == "" || c.Unit == "" || c.SourceURL == "" {
			t.Errorf("caliber %q 缺 indicator/unit/source_url: %+v", c.ID, c)
		}
	}
}

// TestFeedCompactJSON_SourcesCalibersNotFilteredByCategory op=sources 的
// category 过滤**不**作用于 calibers(口径卡是全局静态目录,25 条全量)。
func TestFeedCompactJSON_SourcesCalibersNotFilteredByCategory(t *testing.T) {
	f := b89CaliberFeed(time.Now().Unix())
	text, err := FeedCompactJSON(f, "sources", CategoryCrypto, "", "", IntervalTiers{})
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Categories []struct {
			Category string `json:"category"`
		} `json:"categories"`
		Calibers []struct {
			ID       string `json:"id"`
			Category string `json:"category"`
		} `json:"calibers"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("bad json: %v (%s)", err, text)
	}
	// 采集源清单按 category 过滤。
	if len(out.Categories) != 1 || out.Categories[0].Category != CategoryCrypto {
		t.Errorf("categories 未按 category 过滤: %+v", out.Categories)
	}
	// 口径卡**不**按 category 过滤(全量)。
	total := len(MetricSpecs())
	if len(out.Calibers) != total {
		t.Errorf("calibers = %d, want %d (不按 category 过滤)", len(out.Calibers), total)
	}
	hasAShare := false
	for _, c := range out.Calibers {
		if c.Category == CategoryAShare {
			hasAShare = true
			break
		}
	}
	if !hasAShare {
		t.Error("calibers 应含其它类目的口径卡(全局静态目录)")
	}
}

// TestService_RealworldInfoCarriesSpec Service.RealworldInfo 传入的是
// **已派生**的 Feed(s.Feed()),报价行因此带 spec(批次89 收口核对项)。
func TestService_RealworldInfoCarriesSpec(t *testing.T) {
	Stop()
	defer Stop()
	svc := Start(config.CityChromeConfig{Enabled: false}, nil)
	if svc == nil {
		t.Fatal("Start returned nil")
	}
	now := time.Now().Unix()
	svc.store.MergeMissionResult(now, &MissionPayload{
		Category: CategoryAShare, Source: "腾讯行情",
		Quotes: []RealtimeQuote{{Symbol: "000001", Name: "上证指数", Price: 3245.67, ChangePct: 0.82}},
	})
	text, err := svc.RealworldInfo("quotes", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Quotes []struct {
			Symbol string `json:"symbol"`
			Spec   string `json:"spec"`
			Unit   string `json:"unit"`
		} `json:"quotes"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("bad json: %v (%s)", err, text)
	}
	if len(out.Quotes) != 1 {
		t.Fatalf("quotes = %d, want 1: %s", len(out.Quotes), text)
	}
	if out.Quotes[0].Spec == "" {
		t.Errorf("Service.RealworldInfo 的报价行应带 spec: %s", text)
	}
	if out.Quotes[0].Unit != UnitPoint {
		t.Errorf("unit = %q, want %q", out.Quotes[0].Unit, UnitPoint)
	}
	// op=sources 也带 calibers。
	src, err := svc.RealworldInfo("sources", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	var srcOut struct {
		Calibers []struct {
			ID string `json:"id"`
		} `json:"calibers"`
	}
	if err := json.Unmarshal([]byte(src), &srcOut); err != nil {
		t.Fatalf("bad json: %v (%s)", err, src)
	}
	if len(srcOut.Calibers) != len(MetricSpecs()) {
		t.Errorf("calibers = %d, want %d", len(srcOut.Calibers), len(MetricSpecs()))
	}
}
