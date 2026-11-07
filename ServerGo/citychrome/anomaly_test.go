// Package citychrome — anomaly_test.go: 批次89 异常判定 / 置信度派生单测
// (设计文档 §9.1 第 1 条:unit 命中与兜底、confidence 上下界、4 类 kind
// 触发与优先级、阈值边界、stale 阈值恒等于 2×档位、纯函数不修改入参)。
package citychrome

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"LsmAgentGame/config"
)

// b89Tiers 是**非默认**档位配置(验证 stale 阈值随配置走,批次88 同口径)。
const (
	b89OpenSecs   = 111
	b89ClosedSecs = 222
	b89NewsSecs   = 333
	b89MacroSecs  = 444
)

// b89Tiers 返回非默认档位的 IntervalTiers。
func b89Tiers() IntervalTiers {
	return IntervalTiers{
		QuoteOpenSeconds:   b89OpenSecs,
		QuoteClosedSeconds: b89ClosedSecs,
		NewsSeconds:        b89NewsSecs,
		MacroSeconds:       b89MacroSecs,
	}
}

// b89Feed 构造单类目单报价的 Feed(便于精确控制 UpdatedAt 与价环)。
func b89Feed(cat string, q RealtimeQuote, hist map[string][]RealtimePoint) Feed {
	return Feed{
		Channel: ChannelStatus{Status: ChannelOnline},
		Categories: map[string]RealtimeCategory{
			cat: {
				UpdatedAt: q.UpdatedAt,
				Source:    "腾讯行情",
				Quotes:    []RealtimeQuote{q},
				History:   hist,
			},
		},
	}
}

// b89One 取第一条异常(无则返回零值 + false)。
func b89One(t *testing.T, anoms []Anomaly) (Anomaly, bool) {
	t.Helper()
	if len(anoms) == 0 {
		return Anomaly{}, false
	}
	return anoms[0], true
}

// TestDeriveAnomalies_Kinds 4 类 kind 均可触发(high / low / stale / jump)。
func TestDeriveAnomalies_Kinds(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	tiers := b89Tiers()

	// high:a_share 涨 3.5% > 阈值 3.0%。
	highFeed := b89Feed(CategoryAShare, RealtimeQuote{
		Symbol: "000001", Name: "上证指数", Price: 100, ChangePct: 3.5, UpdatedAt: now.Unix()}, nil)
	a, ok := b89One(t, DeriveAnomalies(highFeed, now, tiers))
	if !ok || a.Kind != AnomalyHigh {
		t.Fatalf("high expected, got %+v (ok=%v)", a, ok)
	}
	if a.Severity != 1 { // ratio = 3.5/3.0 ≈ 1.17 ⇒ 1 + floor(0.17) = 1(轻微)
		t.Errorf("high severity = %d, want 1", a.Severity)
	}
	if a.Value != 3.5 {
		t.Errorf("high value = %v, want 3.5", a.Value)
	}

	// low:a_share 跌 4.0% < -阈值 3.0%。
	lowFeed := b89Feed(CategoryAShare, RealtimeQuote{
		Symbol: "000001", Name: "上证指数", Price: 100, ChangePct: -4.0, UpdatedAt: now.Unix()}, nil)
	a, ok = b89One(t, DeriveAnomalies(lowFeed, now, tiers))
	if !ok || a.Kind != AnomalyLow {
		t.Fatalf("low expected, got %+v (ok=%v)", a, ok)
	}

	// stale:crypto 恒开市 ⇒ 档位 = QuoteOpenSeconds(111) ⇒ 阈值 222s。
	// age = 300s > 222s ⇒ stale(severity 固定 2)。
	staleFeed := b89Feed(CategoryCrypto, RealtimeQuote{
		Symbol: "BTC", Name: "比特币", Price: 100, ChangePct: 0, UpdatedAt: now.Unix() - 300}, nil)
	a, ok = b89One(t, DeriveAnomalies(staleFeed, now, tiers))
	if !ok || a.Kind != AnomalyStale {
		t.Fatalf("stale expected, got %+v (ok=%v)", a, ok)
	}
	if a.Severity != 2 {
		t.Errorf("stale severity = %d, want 2", a.Severity)
	}

	// jump:prev = 100 → 现价 120 ⇒ 跳变 20% > 2×5%(crypto 阈值 5%)。
	jumpFeed := b89Feed(CategoryCrypto, RealtimeQuote{
		Symbol: "BTC", Name: "比特币", Price: 120, ChangePct: 0, UpdatedAt: 2000},
		map[string][]RealtimePoint{"BTC": {{T: 1000, P: 100}, {T: 2000, P: 120}}})
	a, ok = b89One(t, DeriveAnomalies(jumpFeed, time.Unix(2000, 0), tiers))
	if !ok || a.Kind != AnomalyJump {
		t.Fatalf("jump expected, got %+v (ok=%v)", a, ok)
	}
	if a.Severity != 3 {
		t.Errorf("jump severity = %d, want 3", a.Severity)
	}
	if a.Value < 19.99 || a.Value > 20.01 {
		t.Errorf("jump value = %v, want ~20", a.Value)
	}
}

// TestDeriveAnomalies_HistoryInsufficient 历史点不足 2 ⇒ 不判 jump。
func TestDeriveAnomalies_HistoryInsufficient(t *testing.T) {
	now := time.Unix(2000, 0)
	f := b89Feed(CategoryCrypto, RealtimeQuote{
		Symbol: "BTC", Name: "比特币", Price: 120, ChangePct: 0, UpdatedAt: 2000},
		map[string][]RealtimePoint{"BTC": {{T: 2000, P: 120}}})
	if anoms := DeriveAnomalies(f, now, b89Tiers()); len(anoms) != 0 {
		t.Errorf("single history point must not trigger jump: %+v", anoms)
	}
}

// TestDeriveAnomalies_Priority 优先级 jump > stale > high/low。
func TestDeriveAnomalies_Priority(t *testing.T) {
	tiers := b89Tiers()
	// jump + stale 同现 ⇒ jump。
	now := time.Unix(5000, 0)
	f := b89Feed(CategoryCrypto, RealtimeQuote{
		Symbol: "BTC", Name: "比特币", Price: 120, ChangePct: 0, UpdatedAt: 2000},
		map[string][]RealtimePoint{"BTC": {{T: 1000, P: 100}, {T: 2000, P: 120}}})
	a, ok := b89One(t, DeriveAnomalies(f, now, tiers))
	if !ok || a.Kind != AnomalyJump {
		t.Fatalf("jump should win over stale: %+v (ok=%v)", a, ok)
	}
	// stale + high 同现 ⇒ stale。
	f2 := b89Feed(CategoryCrypto, RealtimeQuote{
		Symbol: "BTC", Name: "比特币", Price: 120, ChangePct: 6.0, UpdatedAt: 2000}, nil)
	a, ok = b89One(t, DeriveAnomalies(f2, now, tiers))
	if !ok || a.Kind != AnomalyStale {
		t.Fatalf("stale should win over high: %+v (ok=%v)", a, ok)
	}
}

// TestDeriveAnomalies_ThresholdBoundary 恰好等于阈值**不触发**(设计文档 §9.1)。
func TestDeriveAnomalies_ThresholdBoundary(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	tiers := b89Tiers()
	// crypto 阈值 5.0%:恰好 5.0 ⇒ 不触发;5.01 ⇒ high。
	exact := b89Feed(CategoryCrypto, RealtimeQuote{
		Symbol: "BTC", Name: "比特币", Price: 100, ChangePct: 5.0, UpdatedAt: now.Unix()}, nil)
	if anoms := DeriveAnomalies(exact, now, tiers); len(anoms) != 0 {
		t.Errorf("change_pct == threshold must not trigger: %+v", anoms)
	}
	over := b89Feed(CategoryCrypto, RealtimeQuote{
		Symbol: "BTC", Name: "比特币", Price: 100, ChangePct: 5.01, UpdatedAt: now.Unix()}, nil)
	if anoms := DeriveAnomalies(over, now, tiers); len(anoms) != 1 || anoms[0].Kind != AnomalyHigh {
		t.Errorf("change_pct > threshold must trigger high: %+v", anoms)
	}
	// a_share 阈值 3.0%:恰好 3.0 ⇒ 不触发;恰好 -3.0 ⇒ 不触发。
	for _, chg := range []float64{3.0, -3.0} {
		f := b89Feed(CategoryAShare, RealtimeQuote{
			Symbol: "000001", Name: "上证指数", Price: 100, ChangePct: chg, UpdatedAt: now.Unix()}, nil)
		if anoms := DeriveAnomalies(f, now, tiers); len(anoms) != 0 {
			t.Errorf("change_pct %v == threshold must not trigger: %+v", chg, anoms)
		}
	}
	// jump 阈值 = 2×5% = 10%:恰好 10% ⇒ 不触发;10.01% ⇒ 触发。
	exactJump := b89Feed(CategoryCrypto, RealtimeQuote{
		Symbol: "BTC", Name: "比特币", Price: 110, ChangePct: 0, UpdatedAt: 2000},
		map[string][]RealtimePoint{"BTC": {{T: 1000, P: 100}, {T: 2000, P: 110}}})
	if anoms := DeriveAnomalies(exactJump, time.Unix(2000, 0), tiers); len(anoms) != 0 {
		t.Errorf("jump == 2x threshold must not trigger: %+v", anoms)
	}
}

// TestDeriveAnomalies_StaleThresholdIsTwoTimesTier stale 阈值恒等于
// 2 × tiers.Seconds(cat, now)(**非默认配置档位**下同样成立;批次88 同口径)。
func TestDeriveAnomalies_StaleThresholdIsTwoTimesTier(t *testing.T) {
	tiers := b89Tiers()
	now := time.Unix(1_800_000_000, 0)
	cases := []struct {
		cat      string
		interval int64
	}{
		{CategoryCrypto, b89OpenSecs}, // 7×24 恒开市 ⇒ 开市档
		{CategoryNews, b89NewsSecs},
		{CategoryMacro, b89MacroSecs},
	}
	for _, c := range cases {
		limit := 2 * tiers.Seconds(c.cat, now)
		if limit != 2*c.interval {
			t.Fatalf("%s tier = %d, want %d", c.cat, limit/2, c.interval)
		}
		// age 恰好等于阈值 ⇒ 不触发(严格大于才触发)。
		exact := b89Feed(c.cat, RealtimeQuote{
			Symbol: "X", Name: "X", Price: 100, ChangePct: 0, UpdatedAt: now.Unix() - limit}, nil)
		if anoms := DeriveAnomalies(exact, now, tiers); len(anoms) != 0 {
			t.Errorf("%s age == stale threshold must not trigger: %+v", c.cat, anoms)
		}
		// age 超过阈值 1 秒 ⇒ stale。
		over := b89Feed(c.cat, RealtimeQuote{
			Symbol: "X", Name: "X", Price: 100, ChangePct: 0, UpdatedAt: now.Unix() - limit - 1}, nil)
		anoms := DeriveAnomalies(over, now, tiers)
		if len(anoms) != 1 || anoms[0].Kind != AnomalyStale {
			t.Errorf("%s stale expected: %+v", c.cat, anoms)
		}
	}
}

// TestDeriveAnomalies_SeverityLadder 严重度三档边界(批次89 收口修订):
// severity = 1 + min(2, floor(|change_pct|/阈值 - 1))。
//
//	ratio == 1.0      ⇒ 不触发(严格大于)
//	ratio == 1.0001   ⇒ severity 1(轻微)
//	ratio == 2.0      ⇒ severity 2(中等)
//	ratio == 2.0001   ⇒ severity 2(中等)
//	ratio == 3.0      ⇒ severity 3(严重)
//	ratio == 5.0      ⇒ severity 3(封顶)
func TestDeriveAnomalies_SeverityLadder(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	tiers := b89Tiers()
	const thr = 5.0 // crypto 阈值
	cases := []struct {
		name      string
		magnitude float64
		wantSev   int
		wantHit   bool
	}{
		{"ratio_1.0_not_triggered", 5.0, 0, false},
		{"ratio_1.0001", 5.0005, 1, true},
		{"ratio_2.0", 10.0, 2, true},
		{"ratio_2.0001", 10.0005, 2, true},
		{"ratio_3.0", 15.0, 3, true},
		{"ratio_5.0", 25.0, 3, true},
	}
	if AnomalyThresholdPct(CategoryCrypto) != thr {
		t.Fatalf("crypto threshold = %v, want %v", AnomalyThresholdPct(CategoryCrypto), thr)
	}
	for _, c := range cases {
		f := b89Feed(CategoryCrypto, RealtimeQuote{
			Symbol: "BTC", Name: "比特币", Price: 100, ChangePct: c.magnitude, UpdatedAt: now.Unix()}, nil)
		anoms := DeriveAnomalies(f, now, tiers)
		if !c.wantHit {
			if len(anoms) != 0 {
				t.Errorf("%s: must not trigger, got %+v", c.name, anoms)
			}
			continue
		}
		if len(anoms) != 1 {
			t.Errorf("%s: want 1 anomaly, got %d", c.name, len(anoms))
			continue
		}
		if anoms[0].Kind != AnomalyHigh {
			t.Errorf("%s: kind = %q, want high", c.name, anoms[0].Kind)
		}
		if anoms[0].Severity != c.wantSev {
			t.Errorf("%s: severity = %d, want %d", c.name, anoms[0].Severity, c.wantSev)
		}
	}
}

// TestDeriveAnomalies_SeverityLowSymmetric low 的严重度与 high 对称(|幅度|)。
func TestDeriveAnomalies_SeverityLowSymmetric(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	tiers := b89Tiers()
	f := b89Feed(CategoryCrypto, RealtimeQuote{
		Symbol: "BTC", Name: "比特币", Price: 100, ChangePct: -10.0, UpdatedAt: now.Unix()}, nil)
	anoms := DeriveAnomalies(f, now, tiers)
	if len(anoms) != 1 || anoms[0].Kind != AnomalyLow {
		t.Fatalf("low expected: %+v", anoms)
	}
	if anoms[0].Severity != 2 { // ratio = 2.0 ⇒ severity 2
		t.Errorf("low severity = %d, want 2", anoms[0].Severity)
	}
}

// TestAnomalyKey_CaseInsensitive 异常索引键的 symbol **大小写不敏感**
// (批次89 收口修订:采集源大小写不稳,SH000001 与 sh000001 必须同键)。
func TestAnomalyKey_CaseInsensitive(t *testing.T) {
	a := anomalyKey(CategoryAShare, "SH000001")
	b := anomalyKey(CategoryAShare, "sh000001")
	if a != b {
		t.Errorf("anomalyKey must be case-insensitive: %q vs %q", a, b)
	}
	if anomalyKey(CategoryAShare, "sh000001") == anomalyKey(CategoryAShare, "sz399001") {
		t.Error("different symbols must not share a key")
	}
	if anomalyKey(CategoryAShare, "x") == anomalyKey(CategoryHK, "x") {
		t.Error("different categories must not share a key")
	}
}

// TestDeriveQuoteQuality_AnomalyCaseInsensitive 大小写不一致时,报价行的
// anomaly 标记与后端 anomalies 清单仍须一致(前端徽章对得上)。
func TestDeriveQuoteQuality_AnomalyCaseInsensitive(t *testing.T) {
	tiers := b89Tiers()
	// 报价 symbol 用大写;价环键用小写(采集源口径漂移的典型形态)。
	f := b89Feed(CategoryCrypto, RealtimeQuote{
		Symbol: "BTC", Name: "比特币", Price: 120, ChangePct: 0, UpdatedAt: 2000},
		map[string][]RealtimePoint{"btc": {{T: 1000, P: 100}, {T: 2000, P: 120}}})
	anoms := DeriveAnomalies(f, time.Unix(2000, 0), tiers)
	if len(anoms) != 1 || anoms[0].Kind != AnomalyJump {
		t.Fatalf("jump expected: %+v", anoms)
	}
	out := DeriveQuoteQuality(f, time.Unix(2000, 0), tiers)
	q := out.Categories[CategoryCrypto].Quotes[0]
	if q.Anomaly != AnomalyJump {
		t.Errorf("quote anomaly = %q, want %q", q.Anomaly, AnomalyJump)
	}
}

// TestDeriveQuoteQuality_SpecID 命中口径卡 ⇒ spec_id == spec.ID;
// 未命中 ⇒ spec_id 为空串(前端降级为「暂无口径卡」)。
func TestDeriveQuoteQuality_SpecID(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	tiers := b89Tiers()
	// 命中:symbol 与站点口径代码一致(后端按 Keywords 命中)。
	hit := b89Feed(CategoryAShare, RealtimeQuote{
		Symbol: "000001", Name: "上证指数", Price: 100, ChangePct: 0, UpdatedAt: now.Unix()}, nil)
	out := DeriveQuoteQuality(hit, now, tiers)
	q := out.Categories[CategoryAShare].Quotes[0]
	spec, ok := LookupMetricSpec(CategoryAShare, "000001", "上证指数")
	if !ok {
		t.Fatal("LookupMetricSpec must hit for 000001/上证指数")
	}
	if q.SpecID != spec.ID {
		t.Errorf("spec_id = %q, want %q", q.SpecID, spec.ID)
	}
	if q.SpecID == "" {
		t.Error("hit quote must carry spec_id")
	}
	// 未命中 ⇒ 空串。
	miss := b89Feed(CategoryAShare, RealtimeQuote{
		Symbol: "999999", Name: "查无此股", Price: 10, ChangePct: 0, UpdatedAt: now.Unix()}, nil)
	out2 := DeriveQuoteQuality(miss, now, tiers)
	q2 := out2.Categories[CategoryAShare].Quotes[0]
	if q2.SpecID != "" {
		t.Errorf("miss spec_id = %q, want empty", q2.SpecID)
	}
	// spec_id 命中的口径卡在注册表里必须存在且唯一。
	ids := map[string]int{}
	for _, s := range MetricSpecs() {
		ids[s.ID]++
	}
	if ids[q.SpecID] != 1 {
		t.Errorf("spec_id %q must exist exactly once in the registry (count=%d)", q.SpecID, ids[q.SpecID])
	}
}

func TestDeriveAnomalies_Empty(t *testing.T) {
	anoms := DeriveAnomalies(Feed{}, time.Now(), b89Tiers())
	if anoms == nil {
		t.Fatal("DeriveAnomalies must return non-nil empty slice")
	}
	if len(anoms) != 0 {
		t.Errorf("anomalies = %d, want 0", len(anoms))
	}
}

// TestDeriveQuoteQuality_UnitHitAndFallback unit 命中口径卡优先、否则类目兜底。
func TestDeriveQuoteQuality_UnitHitAndFallback(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	tiers := b89Tiers()
	f := Feed{Categories: map[string]RealtimeCategory{
		CategoryAShare: {UpdatedAt: now.Unix(), Source: "腾讯行情", Quotes: []RealtimeQuote{
			{Symbol: "000001", Name: "上证指数", Price: 100, ChangePct: 0, UpdatedAt: now.Unix()},
			{Symbol: "999999", Name: "查无此股", Price: 10, ChangePct: 0, UpdatedAt: now.Unix()},
		}},
	}}
	out := DeriveQuoteQuality(f, now, tiers)
	qs := out.Categories[CategoryAShare].Quotes
	if len(qs) != 2 {
		t.Fatalf("quotes = %d, want 2", len(qs))
	}
	if qs[0].Unit != UnitPoint {
		t.Errorf("hit unit = %q, want %q", qs[0].Unit, UnitPoint)
	}
	if qs[0].SourceURL == "" {
		t.Error("hit quote must carry source_url")
	}
	if qs[1].Unit != DefaultUnit(CategoryAShare) {
		t.Errorf("fallback unit = %q, want %q", qs[1].Unit, DefaultUnit(CategoryAShare))
	}
	if qs[1].SourceURL != "" {
		t.Errorf("fallback source_url = %q, want empty", qs[1].SourceURL)
	}
}

// TestDeriveQuoteQuality_ConfidenceBounds 置信度 ∈ [0,1];age ≥ 2×档位 ⇒ 0。
func TestDeriveQuoteQuality_ConfidenceBounds(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	tiers := b89Tiers()
	limit := 2 * tiers.Seconds(CategoryCrypto, now)
	cases := []struct {
		name  string
		age   int64
		want0 bool
	}{
		{"fresh", 0, false},
		{"one_second_before_limit", limit - 1, false},
		{"exactly_limit", limit, true},
		{"beyond_limit", limit + 100, true},
	}
	for _, c := range cases {
		f := b89Feed(CategoryCrypto, RealtimeQuote{
			Symbol: "BTC", Name: "比特币", Price: 100, ChangePct: 0, UpdatedAt: now.Unix() - c.age}, nil)
		out := DeriveQuoteQuality(f, now, tiers)
		q := out.Categories[CategoryCrypto].Quotes[0]
		if q.Confidence < 0 || q.Confidence > 1 {
			t.Errorf("%s confidence = %v, want within [0,1]", c.name, q.Confidence)
		}
		if c.want0 && q.Confidence != 0 {
			t.Errorf("%s confidence = %v, want 0 (age >= 2x tier)", c.name, q.Confidence)
		}
		if !c.want0 && q.Confidence <= 0 {
			t.Errorf("%s confidence = %v, want > 0", c.name, q.Confidence)
		}
	}
	// 新鲜 + 权威源(腾讯行情,authority 1.0)+ 无异常 ⇒ 置信度 1。
	f := b89Feed(CategoryCrypto, RealtimeQuote{
		Symbol: "BTC", Name: "比特币", Price: 100, ChangePct: 0, UpdatedAt: now.Unix()}, nil)
	out := DeriveQuoteQuality(f, now, tiers)
	if got := out.Categories[CategoryCrypto].Quotes[0].Confidence; got != 1 {
		t.Errorf("fresh authoritative confidence = %v, want 1", got)
	}
}

// TestDeriveQuoteQuality_AnomalyField 报价行带最严重异常标记(无异常为空串)。
func TestDeriveQuoteQuality_AnomalyField(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	tiers := b89Tiers()
	// 无异常 ⇒ anomaly 空串。
	f := b89Feed(CategoryCrypto, RealtimeQuote{
		Symbol: "BTC", Name: "比特币", Price: 100, ChangePct: 0, UpdatedAt: now.Unix()}, nil)
	out := DeriveQuoteQuality(f, now, tiers)
	if got := out.Categories[CategoryCrypto].Quotes[0].Anomaly; got != "" {
		t.Errorf("anomaly = %q, want empty", got)
	}
	// 涨 6% ⇒ high。
	f2 := b89Feed(CategoryCrypto, RealtimeQuote{
		Symbol: "BTC", Name: "比特币", Price: 100, ChangePct: 6.0, UpdatedAt: now.Unix()}, nil)
	out2 := DeriveQuoteQuality(f2, now, tiers)
	q := out2.Categories[CategoryCrypto].Quotes[0]
	if q.Anomaly != AnomalyHigh {
		t.Errorf("anomaly = %q, want %q", q.Anomaly, AnomalyHigh)
	}
	if q.Confidence >= 1 {
		t.Errorf("anomalous confidence = %v, want < 1 (penalty applied)", q.Confidence)
	}
}

// TestDeriveQuoteQuality_DoesNotMutateInput 纯函数:**不修改入参 Feed**。
func TestDeriveQuoteQuality_DoesNotMutateInput(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	tiers := b89Tiers()
	in := b89Feed(CategoryCrypto, RealtimeQuote{
		Symbol: "BTC", Name: "比特币", Price: 100, ChangePct: 6.0, UpdatedAt: now.Unix()}, nil)
	qBefore := in.Categories[CategoryCrypto].Quotes[0]
	chanBefore := in.Channel
	updatedBefore := in.UpdatedAt
	out := DeriveQuoteQuality(in, now, tiers)
	qAfter := in.Categories[CategoryCrypto].Quotes[0]
	if !reflect.DeepEqual(qBefore, qAfter) {
		t.Errorf("input quote mutated:\n before = %+v\n after  = %+v", qBefore, qAfter)
	}
	if in.Channel != chanBefore || in.UpdatedAt != updatedBefore {
		t.Errorf("input feed scalars mutated: %+v", in)
	}
	if qAfter.Unit != "" || qAfter.SourceURL != "" || qAfter.Confidence != 0 || qAfter.Anomaly != "" {
		t.Errorf("input quote polluted: %+v", qAfter)
	}
	// 输出是新副本(改动输出不影响入参)。
	out.Categories[CategoryCrypto].Quotes[0].Unit = "被改"
	if in.Categories[CategoryCrypto].Quotes[0].Unit != "" {
		t.Error("output shares quote backing array with input")
	}
}

// TestDeriveQuoteQuality_EmptyFeed 空 Feed ⇒ 不 panic,返回可序列化形状。
func TestDeriveQuoteQuality_EmptyFeed(t *testing.T) {
	out := DeriveQuoteQuality(Feed{}, time.Now(), b89Tiers())
	if out.Categories == nil {
		t.Error("Categories must not be nil")
	}
	if out.News == nil {
		t.Error("News must not be nil")
	}
}

// TestService_FeedDerivesQuality 批次89:Service.Feed() 是派生量的**统一出口**
// (看板 WS / HTTP 快照 / Agent 工具同源同口径);派生量**不进存储层**。
func TestService_FeedDerivesQuality(t *testing.T) {
	Stop()
	defer Stop()
	svc := Start(config.CityChromeConfig{Enabled: false}, nil)
	if svc == nil {
		t.Fatal("Start returned nil")
	}
	now := time.Now().Unix()
	svc.store.MergeMissionResult(now, &MissionPayload{
		Category: CategoryAShare, Source: "腾讯行情",
		Quotes: []RealtimeQuote{{Symbol: "000001", Name: "上证指数", Price: 100, ChangePct: 0}},
	})
	f := svc.Feed()
	q := f.Categories[CategoryAShare].Quotes[0]
	if q.Unit != UnitPoint {
		t.Errorf("derived unit = %q, want %q", q.Unit, UnitPoint)
	}
	if q.SourceURL == "" {
		t.Error("derived source_url must not be empty")
	}
	if q.Confidence <= 0 || q.Confidence > 1 {
		t.Errorf("derived confidence = %v, want within (0,1]", q.Confidence)
	}
	// 存储层保持原始值(派生量不进存储层)。
	raw := svc.store.Snapshot()
	rq := raw.Categories[CategoryAShare].Quotes[0]
	if rq.Unit != "" || rq.SourceURL != "" || rq.Confidence != 0 || rq.Anomaly != "" {
		t.Errorf("存储层被派生量污染: %+v", rq)
	}
	// 口径卡 / 异常清单出口。
	if len(svc.MetricSpecs()) < 20 {
		t.Errorf("MetricSpecs = %d, want >= 20", len(svc.MetricSpecs()))
	}
	if svc.Anomalies(time.Now()) == nil {
		t.Error("Anomalies must return non-nil slice")
	}
}

// TestService_NilSafeExports nil 接收者 ⇒ 空切片(非 nil)。
func TestService_NilSafeExports(t *testing.T) {
	var svc *Service
	if svc.MetricSpecs() == nil {
		t.Error("nil service MetricSpecs must not be nil")
	}
	if svc.Anomalies(time.Now()) == nil {
		t.Error("nil service Anomalies must not be nil")
	}
}

// TestFeedCompactJSON_QuotesCarryQuality 批次89 §6:realworld_info 的
// op=quotes 每条 quote **行尾追加** unit / confidence / anomaly 三字段
// (既有字段名与顺序不动)。
func TestFeedCompactJSON_QuotesCarryQuality(t *testing.T) {
	now := time.Now()
	f := b89Feed(CategoryCrypto, RealtimeQuote{
		Symbol: "BTC", Name: "比特币", Price: 100, ChangePct: 0, UpdatedAt: now.Unix()}, nil)
	text, err := FeedCompactJSON(f, "quotes", "", "", "", IntervalTiers{})
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Quotes []struct {
			Category   string  `json:"category"`
			Symbol     string  `json:"symbol"`
			Unit       string  `json:"unit"`
			Confidence float64 `json:"confidence"`
			Anomaly    string  `json:"anomaly"`
		} `json:"quotes"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("bad json: %v (%s)", err, text)
	}
	if len(out.Quotes) != 1 {
		t.Fatalf("quotes = %d, want 1: %s", len(out.Quotes), text)
	}
	q := out.Quotes[0]
	if q.Category != CategoryCrypto || q.Symbol != "BTC" {
		t.Errorf("既有字段被破坏: %+v", q)
	}
	if q.Unit != UnitUSD {
		t.Errorf("unit = %q, want %q", q.Unit, UnitUSD)
	}
	if q.Confidence <= 0 || q.Confidence > 1 {
		t.Errorf("confidence = %v, want within (0,1]", q.Confidence)
	}
	if q.Anomaly != "" {
		t.Errorf("anomaly = %q, want empty", q.Anomaly)
	}
}
