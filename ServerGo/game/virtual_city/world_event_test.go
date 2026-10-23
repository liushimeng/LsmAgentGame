// Package virtual_city — world_event_test.go: 批次80 世界事件规则引擎单测
// (2026-10-23 §真实世界事件自动注入引擎)。
//
// 覆盖:规则 R1–R10 命中/未命中(阈值边界、过期报价剔除、空类目不命中)/
// 映射(场景 kind + 强度 + target)/ 指纹幂等键 / 确定性。
package virtual_city

import (
	"strings"
	"testing"
)

// weNow 固定「当前时刻」,便于构造保鲜/过期边界。
const weNow int64 = 1_700_000_000

// weQuote 构造一条中性报价(默认保鲜)。
func weQuote(cat string, pct float64) WorldEventQuote {
	return WorldEventQuote{Category: cat, Symbol: cat + "-1", Name: cat + "标的",
		ChangePct: pct, UpdatedAt: weNow - 60}
}

// weNews 构造一条中性新闻(默认保鲜)。
func weNews(id, title string) WorldEventNews {
	return WorldEventNews{ID: id, Title: title, Source: "测试源", URL: "http://x/" + id,
		PublishedAt: weNow - 60}
}

// weFind 按规则 id 取事件(未命中返回 false)。
func weFind(events []WorldEvent, rule string) (WorldEvent, bool) {
	for _, e := range events {
		if e.Rule == rule {
			return e, true
		}
	}
	return WorldEvent{}, false
}

// ─────────────────────────── R1 ───────────────────────────

// TestDerive_R1_HitAndMiss R1 命中(均值 ≤ -2% / 单标的 ≤ -3%)与未命中。
func TestDerive_R1_HitAndMiss(t *testing.T) {
	// 均值 -2.0% ⇒ 命中(均值口径)。
	evs := DeriveWorldEvents(WorldEventInput{
		Quotes: []WorldEventQuote{weQuote("a_share", -2.0), weQuote("us", -2.0)},
	}, weNow)
	ev, ok := weFind(evs, "R1")
	if !ok {
		t.Fatalf("R1 should hit at mean -2.0%%: %+v", evs)
	}
	if ev.Kind != WorldEventKindEquityCrash || ev.ScenarioKind != ScenarioMarketCrash {
		t.Errorf("R1 mapping wrong: %+v", ev)
	}
	if ev.ScenarioMag != 1 || ev.Severity != 2 {
		t.Errorf("R1 light = L1/severity2, got L%d/sev%d", ev.ScenarioMag, ev.Severity)
	}
	if ev.ScenarioDur != 4 {
		t.Errorf("R1 duration = %d, want 4", ev.ScenarioDur)
	}

	// 均值 -1.9% ⇒ 未命中(阈值边界之下)。
	evs = DeriveWorldEvents(WorldEventInput{
		Quotes: []WorldEventQuote{weQuote("a_share", -1.9), weQuote("us", -1.9)},
	}, weNow)
	if _, ok := weFind(evs, "R1"); ok {
		t.Fatalf("R1 must not hit at mean -1.9%%: %+v", evs)
	}

	// 单标的 -3.0% ⇒ 命中(极值口径;均值 -1% 不触发均值条件)。
	evs = DeriveWorldEvents(WorldEventInput{
		Quotes: []WorldEventQuote{weQuote("hk", -1.0), weQuote("us", -3.0)},
	}, weNow)
	ev, ok = weFind(evs, "R1")
	if !ok {
		t.Fatalf("R1 should hit on single -3.0%%: %+v", evs)
	}
	if ev.Magnitude != -3.0 {
		t.Errorf("R1 magnitude = %v, want -3.0 (extreme trigger)", ev.Magnitude)
	}

	// 单标的 -2.9% ⇒ 未命中。
	evs = DeriveWorldEvents(WorldEventInput{
		Quotes: []WorldEventQuote{weQuote("hk", -1.0), weQuote("us", -2.9)},
	}, weNow)
	if _, ok := weFind(evs, "R1"); ok {
		t.Fatalf("R1 must not hit on single -2.9%%: %+v", evs)
	}
}

// TestDerive_R1_SevereTier 重度档:均值 ≤ -3.5% 或极值 ≤ -5.0% ⇒ L2/severity3。
func TestDerive_R1_SevereTier(t *testing.T) {
	evs := DeriveWorldEvents(WorldEventInput{
		Quotes: []WorldEventQuote{weQuote("a_share", -3.5), weQuote("us", -3.5)},
	}, weNow)
	ev, ok := weFind(evs, "R1")
	if !ok {
		t.Fatalf("R1 should hit: %+v", evs)
	}
	if ev.ScenarioMag != 2 || ev.Severity != 3 {
		t.Errorf("R1 severe = L2/severity3, got L%d/sev%d", ev.ScenarioMag, ev.Severity)
	}
	// 极值重度:均值 -1%,单标的 -5.0% ⇒ 重度。
	evs = DeriveWorldEvents(WorldEventInput{
		Quotes: []WorldEventQuote{weQuote("hk", -1.0), weQuote("us", -5.0)},
	}, weNow)
	ev, ok = weFind(evs, "R1")
	if !ok {
		t.Fatalf("R1 should hit: %+v", evs)
	}
	if ev.ScenarioMag != 2 || ev.Severity != 3 {
		t.Errorf("R1 severe extreme = L2/severity3, got L%d/sev%d", ev.ScenarioMag, ev.Severity)
	}
}

// TestDerive_R1_QuoteEqualWeight R1 统计集合 = a_share∪hk∪us 全体报价等权
// (非类目等权):hk 单独暴跌 + a_share 小跌 ⇒ 均值仍触发。
func TestDerive_R1_QuoteEqualWeight(t *testing.T) {
	// 3 条报价等权:(-6 + -1 + -1)/3 = -2.67 ⇒ 命中。
	evs := DeriveWorldEvents(WorldEventInput{
		Quotes: []WorldEventQuote{
			weQuote("hk", -6.0), weQuote("a_share", -1.0), weQuote("us", -1.0),
		},
	}, weNow)
	ev, ok := weFind(evs, "R1")
	if !ok {
		t.Fatalf("R1 should hit (quote-equal mean -2.67%%): %+v", evs)
	}
	if ev.Magnitude != -2.6666666666666665 && ev.Magnitude > -2.6 {
		t.Errorf("R1 magnitude = %v, want ~-2.67 (quote-equal mean)", ev.Magnitude)
	}
}

// TestDerive_R1_ExpiredAndEmpty 过期报价剔除 + 空类目不命中。
func TestDerive_R1_ExpiredAndEmpty(t *testing.T) {
	// 报价超过 72h ⇒ 不参与判定 ⇒ 不命中。
	old := weQuote("a_share", -9.0)
	old.UpdatedAt = weNow - worldEventFreshWindowSec - 1
	evs := DeriveWorldEvents(WorldEventInput{Quotes: []WorldEventQuote{old}}, weNow)
	if _, ok := weFind(evs, "R1"); ok {
		t.Fatalf("R1 must not hit on expired quote: %+v", evs)
	}
	// 恰好 72h 边界内 ⇒ 参与判定(保鲜窗口 ≤72h)。
	edge := weQuote("a_share", -9.0)
	edge.UpdatedAt = weNow - worldEventFreshWindowSec
	evs = DeriveWorldEvents(WorldEventInput{Quotes: []WorldEventQuote{edge}}, weNow)
	if _, ok := weFind(evs, "R1"); !ok {
		t.Fatalf("R1 should hit on exactly-72h quote: %+v", evs)
	}
	// 空类目 ⇒ 全部规则不命中。
	evs = DeriveWorldEvents(WorldEventInput{}, weNow)
	if len(evs) != 0 {
		t.Fatalf("empty input must derive no events: %+v", evs)
	}
	// 非统计类目(宏)不参与 R1。
	evs = DeriveWorldEvents(WorldEventInput{
		Quotes: []WorldEventQuote{weQuote("macro", -9.0)},
	}, weNow)
	if _, ok := weFind(evs, "R1"); ok {
		t.Fatalf("R1 must not use macro quotes: %+v", evs)
	}
}

// ─────────────────────────── R2/R3/R4 ───────────────────────────

// TestDerive_R2_GoldSpike 黄金避险升温:metal 均值 ≥ +2% 或单标的 ≥ +2%。
func TestDerive_R2_GoldSpike(t *testing.T) {
	evs := DeriveWorldEvents(WorldEventInput{
		Quotes: []WorldEventQuote{weQuote("metal", 2.0)},
	}, weNow)
	ev, ok := weFind(evs, "R2")
	if !ok {
		t.Fatalf("R2 should hit at +2.0%%: %+v", evs)
	}
	if ev.Kind != WorldEventKindGoldSpike || ev.ScenarioKind != ScenarioMarketCrash {
		t.Errorf("R2 mapping wrong: %+v", ev)
	}
	if ev.ScenarioMag != 1 || ev.Severity != 2 || ev.ScenarioDur != 3 {
		t.Errorf("R2 = L1/sev2/3月, got L%d/sev%d/%d月", ev.ScenarioMag, ev.Severity, ev.ScenarioDur)
	}
	// 单标的 +2% ⇒ 命中(均值条件不满足)。
	evs = DeriveWorldEvents(WorldEventInput{
		Quotes: []WorldEventQuote{weQuote("metal", 2.0), weQuote("metal", -1.0)},
	}, weNow)
	if _, ok := weFind(evs, "R2"); !ok {
		t.Fatalf("R2 should hit on single +2.0%%: %+v", evs)
	}
	// +1.9% ⇒ 不命中。
	evs = DeriveWorldEvents(WorldEventInput{
		Quotes: []WorldEventQuote{weQuote("metal", 1.9)},
	}, weNow)
	if _, ok := weFind(evs, "R2"); ok {
		t.Fatalf("R2 must not hit at +1.9%%: %+v", evs)
	}
}

// TestDerive_R3_CryptoCrash 加密重挫:crypto 均值 ≤ -5% 或单标的 ≤ -8%。
func TestDerive_R3_CryptoCrash(t *testing.T) {
	evs := DeriveWorldEvents(WorldEventInput{
		Quotes: []WorldEventQuote{weQuote("crypto", -5.0)},
	}, weNow)
	ev, ok := weFind(evs, "R3")
	if !ok {
		t.Fatalf("R3 should hit at -5.0%%: %+v", evs)
	}
	if ev.Kind != WorldEventKindCryptoCrash || ev.ScenarioKind != ScenarioMarketCrash {
		t.Errorf("R3 mapping wrong: %+v", ev)
	}
	if ev.ScenarioMag != 1 || ev.Severity != 2 || ev.ScenarioDur != 3 {
		t.Errorf("R3 = L1/sev2/3月, got L%d/sev%d/%d月", ev.ScenarioMag, ev.Severity, ev.ScenarioDur)
	}
	// 单标的 -8% ⇒ 命中。
	evs = DeriveWorldEvents(WorldEventInput{
		Quotes: []WorldEventQuote{weQuote("crypto", -2.0), weQuote("crypto", -8.0)},
	}, weNow)
	if _, ok := weFind(evs, "R3"); !ok {
		t.Fatalf("R3 should hit on single -8.0%%: %+v", evs)
	}
	// -4.9% ⇒ 不命中。
	evs = DeriveWorldEvents(WorldEventInput{
		Quotes: []WorldEventQuote{weQuote("crypto", -4.9)},
	}, weNow)
	if _, ok := weFind(evs, "R3"); ok {
		t.Fatalf("R3 must not hit at -4.9%%: %+v", evs)
	}
}

// TestDerive_R4_CommodityInflation 大宗通胀:futures 均值 ≥ +3%。
func TestDerive_R4_CommodityInflation(t *testing.T) {
	evs := DeriveWorldEvents(WorldEventInput{
		Quotes: []WorldEventQuote{weQuote("futures", 3.0)},
	}, weNow)
	ev, ok := weFind(evs, "R4")
	if !ok {
		t.Fatalf("R4 should hit at +3.0%%: %+v", evs)
	}
	if ev.Kind != WorldEventKindCommodityInflation || ev.ScenarioKind != ScenarioGoodsPriceShock {
		t.Errorf("R4 mapping wrong: %+v", ev)
	}
	if ev.ScenarioMag != 1 || ev.Severity != 2 || ev.ScenarioDur != 6 {
		t.Errorf("R4 = L1/sev2/6月, got L%d/sev%d/%d月", ev.ScenarioMag, ev.Severity, ev.ScenarioDur)
	}
	// +2.9% ⇒ 不命中。
	evs = DeriveWorldEvents(WorldEventInput{
		Quotes: []WorldEventQuote{weQuote("futures", 2.9)},
	}, weNow)
	if _, ok := weFind(evs, "R4"); ok {
		t.Fatalf("R4 must not hit at +2.9%%: %+v", evs)
	}
}

// ─────────────────────────── R5–R10 新闻规则 ───────────────────────────

// TestDerive_R5R6_PolicyMapping 政策宽松/紧缩映射到 policy_shock 的 loose/tight。
func TestDerive_R5R6_PolicyMapping(t *testing.T) {
	evs := DeriveWorldEvents(WorldEventInput{
		News: []WorldEventNews{weNews("n1", "央行宣布全面降准0.5个百分点")},
	}, weNow)
	ev, ok := weFind(evs, "R5")
	if !ok {
		t.Fatalf("R5 should hit on 降准: %+v", evs)
	}
	if ev.ScenarioKind != ScenarioPolicyShock || ev.ScenarioTarget != "loose" {
		t.Errorf("R5 mapping wrong: %+v", ev)
	}
	if ev.ScenarioMag != 1 || ev.Severity != 2 || ev.ScenarioDur != 6 {
		t.Errorf("R5 = L1/sev2/6月, got L%d/sev%d/%d月", ev.ScenarioMag, ev.Severity, ev.ScenarioDur)
	}

	evs = DeriveWorldEvents(WorldEventInput{
		News: []WorldEventNews{weNews("n2", "央行加息25个基点，市场预期收紧")},
	}, weNow)
	ev, ok = weFind(evs, "R6")
	if !ok {
		t.Fatalf("R6 should hit on 加息: %+v", evs)
	}
	if ev.ScenarioKind != ScenarioPolicyShock || ev.ScenarioTarget != "tight" {
		t.Errorf("R6 mapping wrong: %+v", ev)
	}
	// 无关键词 ⇒ 不命中。
	evs = DeriveWorldEvents(WorldEventInput{
		News: []WorldEventNews{weNews("n3", "某公司发布季度财报，营收增长")},
	}, weNow)
	if _, ok := weFind(evs, "R5"); ok {
		t.Fatalf("R5 must not hit: %+v", evs)
	}
	if _, ok := weFind(evs, "R6"); ok {
		t.Fatalf("R6 must not hit: %+v", evs)
	}
}

// TestDerive_R7R8R9R10_DisasterAndSurvey 灾害与调研规则映射 + 强度档。
func TestDerive_R7R8R9R10_DisasterAndSurvey(t *testing.T) {
	// R7 6 级地震 ⇒ L2/severity3/6 月。
	evs := DeriveWorldEvents(WorldEventInput{
		News: []WorldEventNews{weNews("d1", "某地发生6级地震")},
	}, weNow)
	ev, ok := weFind(evs, "R7")
	if !ok {
		t.Fatalf("R7 should hit: %+v", evs)
	}
	if ev.ScenarioKind != ScenarioEarthquake || ev.ScenarioMag != 2 || ev.Severity != 3 {
		t.Errorf("R7 mapping wrong: %+v", ev)
	}
	if ev.ScenarioDur != 6 {
		t.Errorf("R7 duration = %d, want 6", ev.ScenarioDur)
	}
	// R7 ≥7 级 ⇒ L3。
	evs = DeriveWorldEvents(WorldEventInput{
		News: []WorldEventNews{weNews("d2", "某地发生7级地震")},
	}, weNow)
	ev, ok = weFind(evs, "R7")
	if !ok {
		t.Fatalf("R7 should hit: %+v", evs)
	}
	if ev.ScenarioMag != 3 {
		t.Errorf("R7 >=7级 = L3, got L%d", ev.ScenarioMag)
	}

	// R8 海啸 ⇒ L2/severity3/9 月。
	evs = DeriveWorldEvents(WorldEventInput{
		News: []WorldEventNews{weNews("d3", "沿海发布海啸预警")},
	}, weNow)
	ev, ok = weFind(evs, "R8")
	if !ok {
		t.Fatalf("R8 should hit: %+v", evs)
	}
	if ev.ScenarioKind != ScenarioTsunami || ev.ScenarioMag != 2 || ev.ScenarioDur != 9 {
		t.Errorf("R8 mapping wrong: %+v", ev)
	}

	// R9 极端灾难 ⇒ L2/severity3/12 月。
	evs = DeriveWorldEvents(WorldEventInput{
		News: []WorldEventNews{weNews("d4", "矿区发生矿难，多人停产")},
	}, weNow)
	ev, ok = weFind(evs, "R9")
	if !ok {
		t.Fatalf("R9 should hit: %+v", evs)
	}
	if ev.ScenarioKind != ScenarioExtremeDisaster || ev.ScenarioMag != 2 || ev.ScenarioDur != 12 {
		t.Errorf("R9 mapping wrong: %+v", ev)
	}

	// R10 调研脉冲 ⇒ L1/severity1/3 月。
	evs = DeriveWorldEvents(WorldEventInput{
		News: []WorldEventNews{weNews("s1", "最新民调问卷显示居民信心回升")},
	}, weNow)
	ev, ok = weFind(evs, "R10")
	if !ok {
		t.Fatalf("R10 should hit: %+v", evs)
	}
	if ev.ScenarioKind != ScenarioSurveyPulse || ev.ScenarioMag != 1 || ev.Severity != 1 {
		t.Errorf("R10 mapping wrong: %+v", ev)
	}
	if ev.ScenarioDur != 3 {
		t.Errorf("R10 duration = %d, want 3", ev.ScenarioDur)
	}
}

// TestDerive_News_LatestWins 新闻规则取最新一条命中(同规则同事件取最新)。
func TestDerive_News_LatestWins(t *testing.T) {
	old := weNews("old", "央行宣布降准")
	old.PublishedAt = weNow - 3600
	recent := weNews("new", "央行宣布降准")
	recent.PublishedAt = weNow - 60
	evs := DeriveWorldEvents(WorldEventInput{
		News: []WorldEventNews{old, recent},
	}, weNow)
	ev, ok := weFind(evs, "R5")
	if !ok {
		t.Fatalf("R5 should hit: %+v", evs)
	}
	if ev.Key != "rw:R5:new" {
		t.Errorf("R5 key = %q, want rw:R5:new (latest wins)", ev.Key)
	}
}

// TestDerive_News_ExpiredAndEmptyTitle 过期新闻 / 空标题不参与判定。
func TestDerive_News_ExpiredAndEmptyTitle(t *testing.T) {
	expired := weNews("x1", "央行宣布降准")
	expired.PublishedAt = weNow - worldEventFreshWindowSec - 1
	evs := DeriveWorldEvents(WorldEventInput{News: []WorldEventNews{expired}}, weNow)
	if _, ok := weFind(evs, "R5"); ok {
		t.Fatalf("expired news must not hit: %+v", evs)
	}
	blank := weNews("x2", "   ")
	evs = DeriveWorldEvents(WorldEventInput{News: []WorldEventNews{blank}}, weNow)
	if _, ok := weFind(evs, "R5"); ok {
		t.Fatalf("blank title must not hit: %+v", evs)
	}
}

// ─────────────────────────── 指纹与确定性 ───────────────────────────

// TestDerive_KeyFormat 行情类按自然日分桶 + 新闻类用新闻 ID。
func TestDerive_KeyFormat(t *testing.T) {
	evs := DeriveWorldEvents(WorldEventInput{
		Quotes: []WorldEventQuote{weQuote("a_share", -3.0)},
	}, weNow)
	ev, ok := weFind(evs, "R1")
	if !ok {
		t.Fatalf("R1 should hit: %+v", evs)
	}
	want := "rw:R1:a_share+hk+us:" + itoaInt64(weNow/worldEventDayBucketSec)
	if ev.Key != want {
		t.Errorf("R1 key = %q, want %q", ev.Key, want)
	}
	// 同日不同 UpdatedAt(同 bucket)⇒ 同 key(幂等)。
	q2 := weQuote("a_share", -3.0)
	q2.UpdatedAt = weNow - 3600
	evs2 := DeriveWorldEvents(WorldEventInput{Quotes: []WorldEventQuote{q2}}, weNow)
	ev2, _ := weFind(evs2, "R1")
	if ev2.Key != ev.Key {
		t.Errorf("same-day keys differ: %q vs %q", ev.Key, ev2.Key)
	}
	// 次日 ⇒ 不同 key(可再次触发)。
	q3 := weQuote("a_share", -3.0)
	q3.UpdatedAt = weNow + 86400
	evs3 := DeriveWorldEvents(WorldEventInput{Quotes: []WorldEventQuote{q3}}, weNow)
	ev3, _ := weFind(evs3, "R1")
	if ev3.Key == ev.Key {
		t.Errorf("next-day key must differ: %q", ev3.Key)
	}
}

// TestDerive_Deterministic 同输入同输出(可复现)。
func TestDerive_Deterministic(t *testing.T) {
	in := WorldEventInput{
		Quotes: []WorldEventQuote{weQuote("a_share", -3.0), weQuote("metal", 2.5)},
		News:   []WorldEventNews{weNews("n1", "央行降准")},
	}
	a := DeriveWorldEvents(in, weNow)
	b := DeriveWorldEvents(in, weNow)
	if len(a) != len(b) {
		t.Fatalf("length differs: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].Key != b[i].Key || a[i].ScenarioKind != b[i].ScenarioKind ||
			a[i].ScenarioMag != b[i].ScenarioMag || a[i].ScenarioTarget != b[i].ScenarioTarget {
			t.Errorf("event[%d] not deterministic: %+v vs %+v", i, a[i], b[i])
		}
	}
}

// TestDerive_DetailContainsNumbers 事件说明含关键数值(居民/看板可读)。
func TestDerive_DetailContainsNumbers(t *testing.T) {
	evs := DeriveWorldEvents(WorldEventInput{
		Quotes: []WorldEventQuote{weQuote("a_share", -3.0)},
	}, weNow)
	ev, ok := weFind(evs, "R1")
	if !ok {
		t.Fatalf("R1 should hit: %+v", evs)
	}
	if !strings.Contains(ev.Detail, "-3.00") {
		t.Errorf("R1 detail must contain trigger value: %q", ev.Detail)
	}
	if ev.Source == "" || ev.Title == "" || ev.Direction == "" || ev.Scope == "" {
		t.Errorf("R1 display fields empty: %+v", ev)
	}
}
