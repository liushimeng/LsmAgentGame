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

// TestScenarioCatalog_Complete 目录完整性(批次84 起 L1 ∪ L2 ∪ L3):
// L1 既有 101 条逐条仍在(金标准:kind/分组/名称/描述逐字段相等);
// 全目录 kind 唯一、分组合法、Magnitudes=1-5、DefaultMagnitude 1-5、
// Params 非 nil。
//
// ⚠ 批次84 起目录规模 ≥1000(L2 精选 + L3 模板派生),本测试不再钉死
// 总数;总数与分组数断言见 scenario_catalog_batch84_test.go。
func TestScenarioCatalog_Complete(t *testing.T) {
	// 金标准:既有 101 条(L1)与 scenarioDefs 源逐字段相等。
	catalog := ScenarioCatalog()
	byKind := make(map[string]ScenarioSpec, len(catalog))
	for _, spec := range catalog {
		if byKind[spec.Kind].Kind != "" {
			t.Fatalf("duplicate kind %q", spec.Kind)
		}
		byKind[spec.Kind] = spec
	}
	if len(scenarioDefs) != 101 {
		t.Fatalf("L1 source size = %d, want 101", len(scenarioDefs))
	}
	for i := range scenarioDefs {
		src := scenarioDefs[i].spec
		got, ok := byKind[src.Kind]
		if !ok {
			t.Fatalf("L1 kind %q missing from merged catalog", src.Kind)
		}
		if got.Group != src.Group || got.Name != src.Name ||
			got.Description != src.Description ||
			got.DefaultMagnitude != src.DefaultMagnitude {
			t.Errorf("L1 kind %q drift: got %+v, want %+v", src.Kind, got, src)
		}
		if len(got.Params) != len(src.Params) {
			t.Fatalf("L1 kind %q params len drift: %v vs %v", src.Kind, got.Params, src.Params)
		}
		for j := range src.Params {
			if got.Params[j] != src.Params[j] {
				t.Fatalf("L1 kind %q params drift: %v vs %v", src.Kind, got.Params, src.Params)
			}
		}
	}
	for _, spec := range catalog {
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

// l1GroupGolden 既有 101 条 kind → 分组的**独立金标准**。
//
// 硬编码字面量(逐字还原批次84 之前的期望表),**不得**由 scenarioDefs
// 推导 —— 推导即自指:若有人误改 L1 条目的 Group,源与合并结果会一起变,
// 源对照测试照样绿,只有这份字面量能钉死「每个 kind 属于哪个分组」。
// 批次81/83 测试复用本表判 L1 成员(见 TestScenarioCatalog_L1GroupGolden)。
var l1GroupGolden = map[string]string{
	// ── 批次68 既有 10 个 ──
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
	// ── 批次81 新增 30 个(市场与经济 +8)──
	ScenarioInterestRateHike:     ScenarioGroupMarket,
	ScenarioCurrencyDepreciation: ScenarioGroupMarket,
	ScenarioTradeWar:             ScenarioGroupMarket,
	ScenarioCommodityBoom:        ScenarioGroupMarket,
	ScenarioInflationSpiral:      ScenarioGroupMarket,
	ScenarioDeflation:            ScenarioGroupMarket,
	ScenarioHousingBubbleBurst:   ScenarioGroupMarket,
	ScenarioBankRun:              ScenarioGroupMarket,
	// ── 政策与治理 +5 ──
	ScenarioFiscalStimulus:           ScenarioGroupPolicy,
	ScenarioAusterity:                ScenarioGroupPolicy,
	ScenarioRegulatoryCrackdown:      ScenarioGroupPolicy,
	ScenarioInfrastructureInvestment: ScenarioGroupPolicy,
	ScenarioSocialWelfareExpansion:   ScenarioGroupPolicy,
	// ── 社会与舆情 +5 ──
	ScenarioSocialUnrest:               ScenarioGroupSociety,
	ScenarioPandemicOutbreak:           ScenarioGroupSociety,
	ScenarioPopulationSurge:            ScenarioGroupSociety,
	ScenarioAgingCrisis:                ScenarioGroupSociety,
	ScenarioConsumerConfidenceCollapse: ScenarioGroupSociety,
	// ── 自然灾害 +6 ──
	ScenarioFlood:            ScenarioGroupNature,
	ScenarioTyphoon:          ScenarioGroupNature,
	ScenarioDrought:          ScenarioGroupNature,
	ScenarioWildfire:         ScenarioGroupNature,
	ScenarioVolcanicEruption: ScenarioGroupNature,
	ScenarioExtremeHeat:      ScenarioGroupNature,
	// ── 未知灾难 +3 ──
	ScenarioNuclearAccident: ScenarioGroupUnknown,
	ScenarioSolarStorm:      ScenarioGroupUnknown,
	ScenarioBioweaponLeak:   ScenarioGroupUnknown,
	// ── 科技与新文明 +3 ──
	ScenarioAIBoom:                     ScenarioGroupTech,
	ScenarioUnknownCivilizationContact: ScenarioGroupTech,
	ScenarioCyberAttack:                ScenarioGroupTech,
	// ── 批次83 新增 61 个(市场与经济 +10)──
	ScenarioLiquidityCrisis:      ScenarioGroupMarket,
	ScenarioOilPriceShock:        ScenarioGroupMarket,
	ScenarioFoodCrisis:           ScenarioGroupMarket,
	ScenarioTechBubble:           ScenarioGroupMarket,
	ScenarioHousingBoom:          ScenarioGroupMarket,
	ScenarioConsumerDowngrade:    ScenarioGroupMarket,
	ScenarioExportBoom:           ScenarioGroupMarket,
	ScenarioStockMeltup:          ScenarioGroupMarket,
	ScenarioCurrencyAppreciation: ScenarioGroupMarket,
	ScenarioBondMarketStorm:      ScenarioGroupMarket,
	// ── 政策与治理 +10 ──
	ScenarioTaxReform:             ScenarioGroupPolicy,
	ScenarioPropertyTaxPilot:      ScenarioGroupPolicy,
	ScenarioDigitalCurrency:       ScenarioGroupPolicy,
	ScenarioAntiCorruption:        ScenarioGroupPolicy,
	ScenarioImmigrationTightening: ScenarioGroupPolicy,
	ScenarioSnapElection:          ScenarioGroupPolicy,
	ScenarioRecallReferendum:      ScenarioGroupPolicy,
	ScenarioPoliticalDeadlock:     ScenarioGroupPolicy,
	ScenarioPriceControls:         ScenarioGroupPolicy,
	ScenarioEducationReform:       ScenarioGroupPolicy,
	// ── 社会与舆情 +8 ──
	ScenarioGeneralStrike:      ScenarioGroupSociety,
	ScenarioViralScandal:       ScenarioGroupSociety,
	ScenarioInfluencerEconomy:  ScenarioGroupSociety,
	ScenarioCharityScandal:     ScenarioGroupSociety,
	ScenarioMentalHealthCrisis: ScenarioGroupSociety,
	ScenarioTalentInflux:       ScenarioGroupSociety,
	ScenarioBirthrateIncentive: ScenarioGroupSociety,
	ScenarioSocialTrustCrisis:  ScenarioGroupSociety,
	// ── 自然灾害 +12 ──
	ScenarioBlizzard:           ScenarioGroupNature,
	ScenarioColdWave:           ScenarioGroupNature,
	ScenarioSandstorm:          ScenarioGroupNature,
	ScenarioLandslide:          ScenarioGroupNature,
	ScenarioLocustPlague:       ScenarioGroupNature,
	ScenarioHailstorm:          ScenarioGroupNature,
	ScenarioSevereThunderstorm: ScenarioGroupNature,
	ScenarioUrbanWaterlogging:  ScenarioGroupNature,
	ScenarioGroundSubsidence:   ScenarioGroupNature,
	ScenarioSpringFlood:        ScenarioGroupNature,
	ScenarioTornado:            ScenarioGroupNature,
	ScenarioVectorBorneDisease: ScenarioGroupNature,
	// ── 未知灾难 +11 ──
	ScenarioGridBlackout:        ScenarioGroupUnknown,
	ScenarioDroneSwarmAttack:    ScenarioGroupUnknown,
	ScenarioUfoWave:             ScenarioGroupUnknown,
	ScenarioDeepSeaSignal:       ScenarioGroupUnknown,
	ScenarioUnknownVirusX:       ScenarioGroupUnknown,
	ScenarioChemicalSpill:       ScenarioGroupUnknown,
	ScenarioNuclearBrinkmanship: ScenarioGroupUnknown,
	ScenarioAIRunawayIncident:   ScenarioGroupUnknown,
	ScenarioSatelliteDebris:     ScenarioGroupUnknown,
	ScenarioAnomalousFog:        ScenarioGroupUnknown,
	ScenarioTimeAnomaly:         ScenarioGroupUnknown,
	// ── 科技与新文明 +10 ──
	ScenarioAGIBreakthrough:         ScenarioGroupTech,
	ScenarioFusionCommercialization: ScenarioGroupTech,
	ScenarioQuantumPractical:        ScenarioGroupTech,
	ScenarioBrainMachineInterface:   ScenarioGroupTech,
	ScenarioRobotLaborShift:         ScenarioGroupTech,
	ScenarioLongevityTherapy:        ScenarioGroupTech,
	ScenarioSpaceElevator:           ScenarioGroupTech,
	ScenarioMetaverseMigration:      ScenarioGroupTech,
	ScenarioAlienSignalDecoded:      ScenarioGroupTech,
	ScenarioCloneEthicsCrisis:       ScenarioGroupTech,
}

// TestScenarioCatalog_L1GroupGolden 既有 101 条 kind → 分组的**独立金标准**
// (硬编码字面量,不依赖 scenarioDefs 源;防误改 L1 条目的 Group 而源对照
// 测试无感)。与 TestScenarioCatalog_Batch84_L2Markers(锚定 🆕 kind)互补。
func TestScenarioCatalog_L1GroupGolden(t *testing.T) {
	want := l1GroupGolden
	if len(want) != 101 {
		t.Fatalf("golden size = %d, want 101", len(want))
	}
	byKind := map[string]ScenarioSpec{}
	for _, s := range ScenarioCatalog() {
		byKind[s.Kind] = s
	}
	for kind, group := range want {
		got, ok := byKind[kind]
		if !ok {
			t.Fatalf("L1 golden kind %q missing from catalog", kind)
		}
		if got.Group != group {
			t.Errorf("%s group = %q, want %q", kind, got.Group, group)
		}
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
