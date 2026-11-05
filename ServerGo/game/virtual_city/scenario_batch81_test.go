// Package virtual_city — scenario_batch81_test.go: 批次81 场景目录扩展单测
// (2026-11-03 §批次81 City-System 主 Agent 与系统对话)。
//
// 覆盖(契约 §7.1):
//   - 目录完整性:kind 唯一、分组属于 6 个合法组、默认强度 1–5
//     (总数断言随批次83 扩容改为 101,见 TestScenarioCatalog_Batch81_Complete);
//   - 确定性:任选 5 个新场景,固定 seed 双跑 applyScenarioToWorld → 逐位相等;
//   - 月度衰减:注入新场景后跑 3 个 scenarioMonthStep 不 panic。
package virtual_city

import (
	"reflect"
	"testing"
)

// batch81NewKinds 批次81 新增的 30 个 kind(顺序与 scenario_catalog_batch81.go
// 一致;本测试钉死「恰好 30 个新增」,防目录被静默裁剪)。
var batch81NewKinds = []string{
	ScenarioInterestRateHike, ScenarioCurrencyDepreciation, ScenarioTradeWar,
	ScenarioCommodityBoom, ScenarioInflationSpiral, ScenarioDeflation,
	ScenarioHousingBubbleBurst, ScenarioBankRun,
	ScenarioFiscalStimulus, ScenarioAusterity, ScenarioRegulatoryCrackdown,
	ScenarioInfrastructureInvestment, ScenarioSocialWelfareExpansion,
	ScenarioSocialUnrest, ScenarioPandemicOutbreak, ScenarioPopulationSurge,
	ScenarioAgingCrisis, ScenarioConsumerConfidenceCollapse,
	ScenarioFlood, ScenarioTyphoon, ScenarioDrought, ScenarioWildfire,
	ScenarioVolcanicEruption, ScenarioExtremeHeat,
	ScenarioNuclearAccident, ScenarioSolarStorm, ScenarioBioweaponLeak,
	ScenarioAIBoom, ScenarioUnknownCivilizationContact, ScenarioCyberAttack,
}

// TestScenarioCatalog_Batch81_Complete 批次81 新增 30 个 kind 全部在目录内;
// kind 唯一、分组合法、默认强度 1–5。
//
// ⚠ 批次84 起目录 = L1 ∪ L2 ∪ L3(规模 ≥1000),本测试不再钉死总数;
// L1 逐字段金标准见 TestScenarioCatalog_Complete。
func TestScenarioCatalog_Batch81_Complete(t *testing.T) {
	catalog := ScenarioCatalog()
	legalGroups := map[string]bool{}
	for _, g := range ScenarioGroupOrder() {
		legalGroups[g] = true
	}
	// 批次84 回归防护:L1 既有条目仍必须落在批次84 之前的 6 个合法组内
	// (字面量,**不随 ScenarioGroupOrder 扩展而放宽**;kind→组精确金标准见
	// TestScenarioCatalog_L1GroupGolden)。L2/L3 新条目走上面 13 组全集。
	legalGroupsPreB84 := map[string]bool{
		ScenarioGroupMarket: true, ScenarioGroupPolicy: true,
		ScenarioGroupSociety: true, ScenarioGroupNature: true,
		ScenarioGroupUnknown: true, ScenarioGroupTech: true,
	}
	seen := make(map[string]bool, len(catalog))
	for _, spec := range catalog {
		if seen[spec.Kind] {
			t.Fatalf("duplicate kind %q", spec.Kind)
		}
		seen[spec.Kind] = true
		if _, isL1 := l1GroupGolden[spec.Kind]; isL1 {
			if !legalGroupsPreB84[spec.Group] {
				t.Errorf("L1 %s group = %q, must stay in one of the 6 pre-batch84 groups",
					spec.Kind, spec.Group)
			}
		} else if !legalGroups[spec.Group] {
			t.Errorf("%s group = %q, not one of the legal groups", spec.Kind, spec.Group)
		}
		if spec.Name == "" || spec.Description == "" {
			t.Errorf("%s missing name/description", spec.Kind)
		}
		if spec.DefaultMagnitude < scenarioMinMagnitude || spec.DefaultMagnitude > scenarioMaxMagnitude {
			t.Errorf("%s default magnitude = %d, want %d-%d",
				spec.Kind, spec.DefaultMagnitude, scenarioMinMagnitude, scenarioMaxMagnitude)
		}
	}
	for _, kind := range batch81NewKinds {
		if !seen[kind] {
			t.Errorf("batch81 kind %q missing from catalog", kind)
		}
	}
	if len(batch81NewKinds) != 30 {
		t.Fatalf("batch81NewKinds len = %d, want 30", len(batch81NewKinds))
	}
}

// TestScenarioBatch81_Deterministic 固定 seed 双跑 applyScenarioToWorld →
// 逐位相等(零 rand 契约:注入不消耗 w.Rand,同输入必同输出)。
func TestScenarioBatch81_Deterministic(t *testing.T) {
	cases := []struct {
		kind     string
		mag      int
		duration int
	}{
		{ScenarioInterestRateHike, 3, 6},
		{ScenarioHousingBubbleBurst, 4, 9},
		{ScenarioFiscalStimulus, 2, 6},
		{ScenarioPandemicOutbreak, 4, 9},
		{ScenarioVolcanicEruption, 4, 9},
	}
	const seed = int64(20261103)
	for _, c := range cases {
		t.Run(c.kind, func(t *testing.T) {
			w1 := PlaceholderWorld(seed)
			w2 := PlaceholderWorld(seed)
			_, _, _, e1 := applyScenarioToWorld(w1, c.kind, c.mag, c.duration, "", "batch81-det", 1730000000000, seed)
			_, _, _, e2 := applyScenarioToWorld(w2, c.kind, c.mag, c.duration, "", "batch81-det", 1730000000000, seed)
			if e1 != nil || e2 != nil {
				t.Fatalf("apply errors = %v / %v", e1, e2)
			}
			if !reflect.DeepEqual(w1, w2) {
				t.Fatalf("world diverged after %s L%d", c.kind, c.mag)
			}
		})
	}
}

// TestScenarioBatch81_MonthlyStepNoPanic 注入新场景后跑 3 个月度衰减步
// 不 panic,且活动态剩余月数单调递减(衰减链接线正确)。
func TestScenarioBatch81_MonthlyStepNoPanic(t *testing.T) {
	r := newScenarioTestRoom(t, "sc-b81-step", 424242)
	kinds := []string{
		ScenarioInterestRateHike, ScenarioInflationSpiral, ScenarioFlood,
		ScenarioNuclearAccident, ScenarioAIBoom,
	}
	for _, kind := range kinds {
		if _, _, e := r.ApplyScenario(kind, 3, 6, "", "batch81-step"); e != nil {
			t.Fatalf("inject %s: %v", kind, e)
		}
	}
	r.MuLock()
	w := r.World
	if len(w.Scenarios) != len(kinds) {
		r.MuUnlock()
		t.Fatalf("active scenarios = %d, want %d", len(w.Scenarios), len(kinds))
	}
	for i := 0; i < 3; i++ {
		w.scenarioMonthStep() // 不得 panic
	}
	for _, sc := range w.Scenarios {
		if sc.RemainingMonths >= sc.TotalMonths {
			r.MuUnlock()
			t.Fatalf("scenario %s did not decay: remaining=%d total=%d",
				sc.Kind, sc.RemainingMonths, sc.TotalMonths)
		}
	}
	r.MuUnlock()
}

// TestScenarioBatch81_RateFieldsActive 批次81 新增场景的 PolicyRateDelta /
// CreditWindowDelta 必须真实生效(回归 policyDir 缺省方向:非 policy_shock
// kind 的利率/信贷字段按符号生效,否则新场景的利率传导链为空转)。
func TestScenarioBatch81_RateFieldsActive(t *testing.T) {
	r := newScenarioTestRoom(t, "sc-b81-rate", 9090)
	w := r.World
	rateBefore := w.CB.PolicyRate
	windowBefore := w.CB.CreditWindowFactor
	if _, _, e := r.ApplyScenario(ScenarioInterestRateHike, 3, 6, "", ""); e != nil {
		t.Fatalf("interest_rate_hike: %v", e)
	}
	if w.CB.PolicyRate <= rateBefore {
		t.Fatalf("policy rate %v did not rise (before %v)", w.CB.PolicyRate, rateBefore)
	}
	if w.CB.CreditWindowFactor >= windowBefore {
		t.Fatalf("credit window %v did not tighten (before %v)", w.CB.CreditWindowFactor, windowBefore)
	}
	// 通缩螺旋:利率应下行(负 delta × 正方向)。
	rateBefore = w.CB.PolicyRate
	if _, _, e := r.ApplyScenario(ScenarioDeflation, 3, 9, "", ""); e != nil {
		t.Fatalf("deflation: %v", e)
	}
	if w.CB.PolicyRate >= rateBefore {
		t.Fatalf("deflation policy rate %v did not fall (before %v)", w.CB.PolicyRate, rateBefore)
	}
}

// TestScenarioBatch81_AllKindsApplicable 全目录场景(批次83 起 101 个)均可注入(目录条目 →
// 引擎分派无死链;防「声明了却从不接线」§130)。单房间逐个注入(持续 1 月后
// 立即走场景月步到期腾出活动名额),任一失败即报错并附 kind。
func TestScenarioBatch81_AllKindsApplicable(t *testing.T) {
	r := newScenarioTestRoom(t, "sc-b81-all", 777001)
	for _, spec := range ScenarioCatalog() {
		if _, _, e := r.ApplyScenario(spec.Kind, spec.DefaultMagnitude, 1, "", ""); e != nil {
			t.Fatalf("kind %s (L%d) not applicable: %v", spec.Kind, spec.DefaultMagnitude, e)
		}
		r.MuLock()
		r.World.scenarioMonthStep() // 持续 1 月 → 本次即到期,腾出活动名额
		r.MuUnlock()
	}
	r.MuLock()
	n := len(r.World.Scenarios)
	r.MuUnlock()
	if n != 0 {
		t.Fatalf("active scenarios = %d, want 0 (all expired)", n)
	}
}
