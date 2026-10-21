// Package virtual_city — scenario_test.go: 场景推演引擎单测
// (2026-10-21 §场景推演)。
//
// 覆盖:目录完整性(10 场景 × L1-L5)/ 注入校验(未知 kind / 非法强度 /
// 非法持续 / 非法 target / 活动上限)/ 确定性(同状态同输入 → 同效果)/
// 记录环形缓冲 cap 20 / 月度衰减与到期 / view.scenarios_recent 下发。
package virtual_city

import (
	"fmt"
	"testing"

	"LsmAgentGame/errcode"
)

// newScenarioTestRoom 构造 10 bot 开局中的房间(与 survey_test 同款装配)。
func newScenarioTestRoom(t *testing.T, roomID string, seed int64) *VirtualCityRoom {
	t.Helper()
	r := NewVirtualCityRoom(roomID, 3000, seed, 4)
	botUsers := map[int]string{}
	botModels := map[int]string{}
	for seat := 0; seat < 10; seat++ {
		botUsers[seat] = fmt.Sprintf("bot-%d", seat)
		botModels[seat] = "TestModel"
	}
	r.RegisterBotSeats(botUsers, botModels)
	if e := r.Start(nil); e != nil {
		t.Fatalf("start room: %v", e)
	}
	return r
}

// TestScenarioCatalog_Complete 目录 = 10 个 kind、4 个分组、L1-L5。
func TestScenarioCatalog_Complete(t *testing.T) {
	want := map[string]string{
		ScenarioGoodsPriceShock: ScenarioGroupMarket,
		ScenarioPolicyShock:     ScenarioGroupMarket,
		ScenarioMarketCrash:     ScenarioGroupMarket,
		ScenarioSurveyPulse:     ScenarioGroupSociety,
		ScenarioElectionMood:    ScenarioGroupSociety,
		ScenarioEarthquake:      ScenarioGroupNature,
		ScenarioTsunami:         ScenarioGroupNature,
		ScenarioExtremeDisaster: ScenarioGroupNature,
		ScenarioMeteorite:       ScenarioGroupUnknown,
		ScenarioAlienInvasion:   ScenarioGroupUnknown,
	}
	catalog := ScenarioCatalog()
	if len(catalog) != len(want) {
		t.Fatalf("catalog size = %d, want %d", len(catalog), len(want))
	}
	seen := map[string]bool{}
	for _, spec := range catalog {
		group, ok := want[spec.Kind]
		if !ok {
			t.Fatalf("unexpected kind %q", spec.Kind)
		}
		if seen[spec.Kind] {
			t.Fatalf("duplicate kind %q", spec.Kind)
		}
		seen[spec.Kind] = true
		if spec.Group != group {
			t.Errorf("%s group = %q, want %q", spec.Kind, spec.Group, group)
		}
		if spec.Name == "" || spec.Description == "" {
			t.Errorf("%s missing name/description", spec.Kind)
		}
		if len(spec.Magnitudes) != 5 {
			t.Errorf("%s magnitudes = %v, want 1-5", spec.Kind, spec.Magnitudes)
		}
		for i, m := range spec.Magnitudes {
			if m != i+1 {
				t.Errorf("%s magnitudes[%d] = %d, want %d", spec.Kind, i, m, i+1)
			}
		}
		if spec.DefaultMagnitude < 1 || spec.DefaultMagnitude > 5 {
			t.Errorf("%s default magnitude = %d, want 1-5", spec.Kind, spec.DefaultMagnitude)
		}
		if spec.Params == nil {
			t.Errorf("%s params must be [] not null", spec.Kind)
		}
	}
	// 目录副本隔离:修改返回值不污染全局目录。
	catalog[0].Magnitudes[0] = 99
	if ScenarioCatalog()[0].Magnitudes[0] != 1 {
		t.Fatal("ScenarioCatalog must return an independent copy")
	}
}

// TestApplyScenario_Deterministic 相同初始状态 + 相同输入 → 相同效果与状态增量。
func TestApplyScenario_Deterministic(t *testing.T) {
	cases := []struct {
		kind   string
		mag    int
		target string
	}{
		{ScenarioGoodsPriceShock, 3, ""},
		{ScenarioGoodsPriceShock, 2, "food"},
		{ScenarioPolicyShock, 4, "tight"},
		{ScenarioPolicyShock, 4, "loose"},
		{ScenarioMarketCrash, 5, ""},
		{ScenarioSurveyPulse, 3, ""},
		{ScenarioElectionMood, 4, ""},
		{ScenarioEarthquake, 3, ""},
		{ScenarioTsunami, 4, ""},
		{ScenarioExtremeDisaster, 5, ""},
		{ScenarioMeteorite, 5, ""},
		{ScenarioAlienInvasion, 5, ""},
	}
	for _, c := range cases {
		t.Run(c.kind+"/"+c.target, func(t *testing.T) {
			r1 := newScenarioTestRoom(t, "sc-det-a", 20261021)
			r2 := newScenarioTestRoom(t, "sc-det-b", 20261021)
			_, _, e1 := r1.ApplyScenario(c.kind, c.mag, 6, c.target, "determinism")
			_, _, e2 := r2.ApplyScenario(c.kind, c.mag, 6, c.target, "determinism")
			if e1 != nil || e2 != nil {
				t.Fatalf("apply errors = %v / %v", e1, e2)
			}
			w1, w2 := r1.World, r2.World
			if w1.Market.StockIndex != w2.Market.StockIndex ||
				w1.Market.GoldPrice != w2.Market.GoldPrice ||
				w1.Market.DriftExtra != w2.Market.DriftExtra {
				t.Fatalf("market diverged: %+v vs %+v", w1.Market, w2.Market)
			}
			for id := range w1.Market.DistrictIdx {
				if w1.Market.DistrictIdx[id] != w2.Market.DistrictIdx[id] {
					t.Fatalf("district %s diverged", id)
				}
			}
			for _, id := range goodsOrder {
				if w1.Goods.Items[id].PriceIdx != w2.Goods.Items[id].PriceIdx ||
					w1.Goods.SupplyIdx[id] != w2.Goods.SupplyIdx[id] {
					t.Fatalf("goods %s diverged", id)
				}
			}
			if w1.Labor.Unemployment != w2.Labor.Unemployment {
				t.Fatalf("unemployment diverged: %v vs %v", w1.Labor.Unemployment, w2.Labor.Unemployment)
			}
			if w1.Treasury.Cash != w2.Treasury.Cash {
				t.Fatalf("treasury diverged: %d vs %d", w1.Treasury.Cash, w2.Treasury.Cash)
			}
			if w1.Election.SentimentBias != w2.Election.SentimentBias {
				t.Fatalf("election bias diverged: %v vs %v", w1.Election.SentimentBias, w2.Election.SentimentBias)
			}
			// 零 rand:注入不得消耗 w.Rand —— 同 seed 世界注入后,下一月结的
			// 随机序列必须与未注入世界一致(Rand 状态同源可由此间接验证)。
			if w1.MinskyMomentCount != w2.MinskyMomentCount ||
				w1.BubbleState != w2.BubbleState {
				t.Fatal("scenario injection mutated unrelated deterministic state")
			}
		})
	}
}

// TestApplyScenario_EffectsApplied 效果确实落到既有状态字段(非空转,§130):
// 地震 → 居民现金/国库/供给指数下行;外星人入侵 → 股指深跌 + 黄金上涨;
// 加息政策冲击 → 政策利率上行、信贷窗口收紧。
func TestApplyScenario_EffectsApplied(t *testing.T) {
	r := newScenarioTestRoom(t, "sc-effects", 2026)
	w := r.World
	cashBefore := map[int]int64{}
	for seat, p := range w.Players {
		if p != nil {
			cashBefore[seat] = p.Cash
		}
	}
	treasuryBefore := w.Treasury.Cash
	supplyBefore := w.Goods.SupplyIdx["food"]
	if _, effects, e := r.ApplyScenario(ScenarioEarthquake, 4, 6, "", ""); e != nil {
		t.Fatalf("earthquake: %v", e)
	} else if len(effects) == 0 {
		t.Fatal("earthquake produced no effects")
	}
	if w.Treasury.Cash >= treasuryBefore {
		t.Fatalf("treasury cash %d did not drop (before %d)", w.Treasury.Cash, treasuryBefore)
	}
	if w.Goods.SupplyIdx["food"] >= supplyBefore {
		t.Fatalf("food supply %v did not drop (before %v)", w.Goods.SupplyIdx["food"], supplyBefore)
	}
	damaged := 0
	for seat, before := range cashBefore {
		if w.Players[seat] != nil && w.Players[seat].Cash < before {
			damaged++
		}
	}
	if damaged == 0 {
		t.Fatal("no resident cash damage applied")
	}

	stockBefore := w.Market.StockIndex
	goldBefore := w.Market.GoldPrice
	if _, _, e := r.ApplyScenario(ScenarioAlienInvasion, 5, 12, "", ""); e != nil {
		t.Fatalf("alien invasion: %v", e)
	}
	if w.Market.StockIndex >= stockBefore {
		t.Fatalf("stock index %v did not fall (before %v)", w.Market.StockIndex, stockBefore)
	}
	if w.Market.GoldPrice <= goldBefore {
		t.Fatalf("gold %v did not rise (before %v)", w.Market.GoldPrice, goldBefore)
	}

	rateBefore := w.CB.PolicyRate
	windowBefore := w.CB.CreditWindowFactor
	if _, _, e := r.ApplyScenario(ScenarioPolicyShock, 4, 6, "tight", ""); e != nil {
		t.Fatalf("policy shock: %v", e)
	}
	if w.CB.PolicyRate <= rateBefore {
		t.Fatalf("policy rate %v did not rise (before %v)", w.CB.PolicyRate, rateBefore)
	}
	if w.CB.CreditWindowFactor >= windowBefore {
		t.Fatalf("credit window %v did not tighten (before %v)", w.CB.CreditWindowFactor, windowBefore)
	}
}

// TestApplyScenario_Validation 未知 kind / 非法强度 / 非法持续 / 非法 target。
func TestApplyScenario_Validation(t *testing.T) {
	r := newScenarioTestRoom(t, "sc-invalid", 7)
	cases := []struct {
		name     string
		kind     string
		mag      int
		duration int
		target   string
		want     int
	}{
		{"unknown kind", "nuclear_winter", 3, 6, "", errcode.ErrVirtualCityScenarioUnknown},
		{"magnitude too high", ScenarioEarthquake, 6, 6, "", errcode.ErrVirtualCityScenarioParamsInvalid},
		{"magnitude negative", ScenarioEarthquake, -1, 6, "", errcode.ErrVirtualCityScenarioParamsInvalid},
		{"duration too long", ScenarioEarthquake, 3, 25, "", errcode.ErrVirtualCityScenarioParamsInvalid},
		{"duration negative", ScenarioEarthquake, 3, -1, "", errcode.ErrVirtualCityScenarioParamsInvalid},
		{"bad goods target", ScenarioGoodsPriceShock, 3, 6, "gold", errcode.ErrVirtualCityScenarioParamsInvalid},
		{"bad policy stance", ScenarioPolicyShock, 3, 6, "sideways", errcode.ErrVirtualCityScenarioParamsInvalid},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, _, e := r.ApplyScenario(c.kind, c.mag, c.duration, c.target, ""); e == nil || e.Code != c.want {
				t.Fatalf("got %v, want code %d", e, c.want)
			}
		})
	}
	// 未开局(open) → 35002。
	open := NewVirtualCityRoom("sc-open", 3000, 1, 4)
	if _, _, e := open.ApplyScenario(ScenarioEarthquake, 3, 6, "", ""); e == nil || e.Code != errcode.ErrVirtualCityNotPlaying {
		t.Fatalf("open room: got %v, want %d", e, errcode.ErrVirtualCityNotPlaying)
	}
}

// TestApplyScenario_ActiveLimit 同时生效场景上限 = 8(第 9 个 → 35057)。
func TestApplyScenario_ActiveLimit(t *testing.T) {
	r := newScenarioTestRoom(t, "sc-limit", 11)
	for i := 0; i < scenarioMaxActive; i++ {
		if _, _, e := r.ApplyScenario(ScenarioEarthquake, 1, 6, "", ""); e != nil {
			t.Fatalf("inject #%d: %v", i+1, e)
		}
	}
	if _, _, e := r.ApplyScenario(ScenarioTsunami, 1, 6, "", ""); e == nil || e.Code != errcode.ErrVirtualCityScenarioActiveLimit {
		t.Fatalf("9th inject: got %v, want %d", e, errcode.ErrVirtualCityScenarioActiveLimit)
	}
}

// TestApplyScenario_RecordCap 记录环形缓冲:21 次注入(每次持续 1 月后消退)
// → 仅保留最近 20 条,ID 递增。
func TestApplyScenario_RecordCap(t *testing.T) {
	r := newScenarioTestRoom(t, "sc-record", 13)
	for i := 0; i < scenarioRecordCap+1; i++ {
		if _, _, e := r.ApplyScenario(ScenarioSurveyPulse, 1, 1, "", fmt.Sprintf("n%d", i)); e != nil {
			t.Fatalf("inject #%d: %v", i+1, e)
		}
		r.MuLock()
		r.World.scenarioMonthStep() // 持续 1 月 → 本次即到期,腾出活动名额
		r.MuUnlock()
	}
	recs := r.ListScenarios()
	if len(recs) != scenarioRecordCap {
		t.Fatalf("records = %d, want %d", len(recs), scenarioRecordCap)
	}
	if recs[0].Note != "n1" || recs[len(recs)-1].Note != fmt.Sprintf("n%d", scenarioRecordCap) {
		t.Fatalf("record window = %q..%q, want n1..n%d",
			recs[0].Note, recs[len(recs)-1].Note, scenarioRecordCap)
	}
	if len(r.World.Scenarios) != 0 {
		t.Fatalf("active scenarios = %d, want 0 (all expired)", len(r.World.Scenarios))
	}
}

// TestScenario_MonthlyDecay 月度衰减:持续 4 月的市场崩盘 —— DriftExtra 逐月
// 递减、第 4 次月步后活动态清空并播报「影响结束」。
func TestScenario_MonthlyDecay(t *testing.T) {
	r := newScenarioTestRoom(t, "sc-decay", 17)
	w := r.World
	if _, _, e := r.ApplyScenario(ScenarioMarketCrash, 4, 4, "", "decay"); e != nil {
		t.Fatalf("inject: %v", e)
	}
	if len(w.Scenarios) != 1 || w.Scenarios[0].RemainingMonths != 4 {
		t.Fatalf("active = %+v, want 1 scenario with 4 months left", w.Scenarios)
	}
	prev := -1.0
	for i := 0; i < 4; i++ {
		// 模拟 SettleMonth ⓪C:泡沫月步重置 DriftExtra 后再走场景月步。
		w.Market.DriftExtra = 0
		w.scenarioMonthStep()
		if i < 3 {
			if len(w.Scenarios) != 1 {
				t.Fatalf("month %d: active = %d, want 1", i+1, len(w.Scenarios))
			}
			drift := w.Market.DriftExtra
			if drift >= 0 {
				t.Fatalf("month %d: DriftExtra = %v, want negative", i+1, drift)
			}
			if prev >= 0 && drift <= prev {
				t.Fatalf("month %d: drift %v did not decay (prev %v)", i+1, drift, prev)
			}
			prev = drift
		}
	}
	if len(w.Scenarios) != 0 {
		t.Fatalf("active after expiry = %d, want 0", len(w.Scenarios))
	}
	found := false
	for _, ev := range w.RecentEvents(10) {
		if ev.Type == EventScenario && ev.Text == "场景『市场崩盘』影响结束(共持续 4 个月)" {
			found = true
		}
	}
	if !found {
		t.Error("expiry event missing")
	}
	// 到期后选举偏置归零(不残留)。
	if w.Election.SentimentBias != 0 {
		t.Fatalf("election bias after expiry = %v, want 0", w.Election.SentimentBias)
	}
}

// TestScenario_BuildClientState 下发 scenarios_recent(空 = [] 非 null)。
func TestScenario_BuildClientState(t *testing.T) {
	r := newScenarioTestRoom(t, "sc-view", 19)
	w := r.World
	var transcripts [MaxSeats]BotTranscript
	empty := BuildClientState(r.RoomID, -1, w, r.Seats, r.Nicknames, r.BotSeats, r.SeatModelKeys,
		transcripts, 0, 0, 0, nil, nil)
	if empty.ScenariosRecent == nil || len(empty.ScenariosRecent) != 0 {
		t.Fatalf("empty scenarios_recent = %#v, want []", empty.ScenariosRecent)
	}
	if _, _, e := r.ApplyScenario(ScenarioEarthquake, 3, 6, "", "观测"); e != nil {
		t.Fatalf("inject: %v", e)
	}
	cs := BuildClientState(r.RoomID, -1, w, r.Seats, r.Nicknames, r.BotSeats, r.SeatModelKeys,
		transcripts, 0, 0, 0, nil, nil)
	if len(cs.ScenariosRecent) != 1 {
		t.Fatalf("scenarios_recent len = %d, want 1", len(cs.ScenariosRecent))
	}
	rec := cs.ScenariosRecent[0]
	if rec.Kind != ScenarioEarthquake || rec.Magnitude != 3 || rec.ID != "SC1" || rec.Note != "观测" {
		t.Fatalf("record = %+v", rec)
	}
	if rec.AppliedMonth != w.Month || rec.At == 0 {
		t.Fatalf("record month/at = %d/%d", rec.AppliedMonth, rec.At)
	}
}
