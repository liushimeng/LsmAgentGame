// Package virtual_city — scenario_catalog_batch83_test.go: 批次83 场景目录
// 扩展单测(2026-11-04 §批次83 系统对话窗口优化与场景目录扩充)。
//
// 覆盖(契约 §6.1):
//   - 目录完整性:总数 101、kind 全局唯一、分组属于 6 个合法组、默认强度
//     1–5、默认持续 ≥ 1 月、Name/Description 非空;
//   - profile 健全性:每条至少 1 个非零字段(反射判定,防「声明了却从不
//     接线」式空转条目 §130);
//   - 批次83 新增 61 个 kind 全部在目录内(钉死「恰好 61 个新增」)。
package virtual_city

import (
	"reflect"
	"testing"
)

// batch83NewKinds 批次83 新增的 61 个 kind(顺序与 scenario_catalog_batch83.go
// 一致;本列表钉死「恰好 61 个新增」,防目录被静默裁剪或扩水)。
var batch83NewKinds = []string{
	// 市场与经济(+10)
	ScenarioLiquidityCrisis, ScenarioOilPriceShock, ScenarioFoodCrisis,
	ScenarioTechBubble, ScenarioHousingBoom, ScenarioConsumerDowngrade,
	ScenarioExportBoom, ScenarioStockMeltup, ScenarioCurrencyAppreciation,
	ScenarioBondMarketStorm,
	// 政策与治理(+10)
	ScenarioTaxReform, ScenarioPropertyTaxPilot, ScenarioDigitalCurrency,
	ScenarioAntiCorruption, ScenarioImmigrationTightening, ScenarioSnapElection,
	ScenarioRecallReferendum, ScenarioPoliticalDeadlock, ScenarioPriceControls,
	ScenarioEducationReform,
	// 社会与舆情(+8)
	ScenarioGeneralStrike, ScenarioViralScandal, ScenarioInfluencerEconomy,
	ScenarioCharityScandal, ScenarioMentalHealthCrisis, ScenarioTalentInflux,
	ScenarioBirthrateIncentive, ScenarioSocialTrustCrisis,
	// 自然灾害(+12)
	ScenarioBlizzard, ScenarioColdWave, ScenarioSandstorm, ScenarioLandslide,
	ScenarioLocustPlague, ScenarioHailstorm, ScenarioSevereThunderstorm,
	ScenarioUrbanWaterlogging, ScenarioGroundSubsidence, ScenarioSpringFlood,
	ScenarioTornado, ScenarioVectorBorneDisease,
	// 未知灾难(+11)
	ScenarioGridBlackout, ScenarioDroneSwarmAttack, ScenarioUfoWave,
	ScenarioDeepSeaSignal, ScenarioUnknownVirusX, ScenarioChemicalSpill,
	ScenarioNuclearBrinkmanship, ScenarioAIRunawayIncident, ScenarioSatelliteDebris,
	ScenarioAnomalousFog, ScenarioTimeAnomaly,
	// 科技与新文明(+10)
	ScenarioAGIBreakthrough, ScenarioFusionCommercialization, ScenarioQuantumPractical,
	ScenarioBrainMachineInterface, ScenarioRobotLaborShift, ScenarioLongevityTherapy,
	ScenarioSpaceElevator, ScenarioMetaverseMigration, ScenarioAlienSignalDecoded,
	ScenarioCloneEthicsCrisis,
}

// batch83KeyKinds 文档 §6.1 点名抽样断言的关键 kind(≥10 个,横跨六组)。
var batch83KeyKinds = []string{
	ScenarioLiquidityCrisis, ScenarioFoodCrisis, ScenarioPropertyTaxPilot,
	ScenarioSnapElection, ScenarioGeneralStrike, ScenarioBlizzard,
	ScenarioLocustPlague, ScenarioGridBlackout, ScenarioUnknownVirusX,
	ScenarioNuclearBrinkmanship, ScenarioAGIBreakthrough,
	ScenarioFusionCommercialization, ScenarioAlienSignalDecoded,
}

// TestScenarioCatalog_Batch83_Total101 目录总数 = 101(批次68 10 +
// 批次81 30 + 批次83 61);kind 全局唯一;分组 ∈ 六常量;默认强度 1–5;
// 默认持续 ≥ 1 月;Name/Description 非空。
func TestScenarioCatalog_Batch83_Total101(t *testing.T) {
	catalog := ScenarioCatalog()
	if len(catalog) != 101 {
		t.Fatalf("catalog size = %d, want 101", len(catalog))
	}
	legalGroups := map[string]bool{
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
		if !legalGroups[spec.Group] {
			t.Errorf("%s group = %q, not one of the 6 legal groups", spec.Kind, spec.Group)
		}
		if spec.Name == "" || spec.Description == "" {
			t.Errorf("%s missing name/description", spec.Kind)
		}
		if spec.DefaultMagnitude < scenarioMinMagnitude || spec.DefaultMagnitude > scenarioMaxMagnitude {
			t.Errorf("%s default magnitude = %d, want %d-%d",
				spec.Kind, spec.DefaultMagnitude, scenarioMinMagnitude, scenarioMaxMagnitude)
		}
	}
	// defaultDuration 走 scenarioDefs(ScenarioSpec 不暴露该字段)。
	if len(scenarioDefs) != len(catalog) {
		t.Fatalf("scenarioDefs len = %d, catalog len = %d (must match)", len(scenarioDefs), len(catalog))
	}
	for i := range scenarioDefs {
		if scenarioDefs[i].defaultDuration < 1 {
			t.Errorf("%s defaultDuration = %d, want >= 1",
				scenarioDefs[i].spec.Kind, scenarioDefs[i].defaultDuration)
		}
	}
}

// TestScenarioCatalog_Batch83_Kinds61 批次83 恰好新增 61 个,全部在目录内;
// 文档点名的 13 个关键 kind 全部命中。
func TestScenarioCatalog_Batch83_Kinds61(t *testing.T) {
	if len(batch83NewKinds) != 61 {
		t.Fatalf("batch83NewKinds len = %d, want 61", len(batch83NewKinds))
	}
	seen := map[string]bool{}
	for _, spec := range ScenarioCatalog() {
		seen[spec.Kind] = true
	}
	for _, kind := range batch83NewKinds {
		if !seen[kind] {
			t.Errorf("batch83 kind %q missing from catalog", kind)
		}
	}
	for _, kind := range batch83KeyKinds {
		if !seen[kind] {
			t.Errorf("key kind %q missing from catalog (doc §6.1 sample)", kind)
		}
	}
}

// profileHasNonZeroField 反射判定 profile 至少 1 个非零字段。
func profileHasNonZeroField(p scenarioProfile) bool {
	v := reflect.ValueOf(p)
	for i := 0; i < v.NumField(); i++ {
		switch v.Field(i).Kind() {
		case reflect.Float64:
			if v.Field(i).Float() != 0 {
				return true
			}
		case reflect.Int64:
			if v.Field(i).Int() != 0 {
				return true
			}
		}
	}
	return false
}

// TestScenarioCatalog_Batch83_ProfileNonZero 每条目录条目的 profile 至少
// 1 个非零字段(零值 profile = 注入无任何效果,即「声明了却从不接线」)。
func TestScenarioCatalog_Batch83_ProfileNonZero(t *testing.T) {
	for i := range scenarioDefs {
		kind := scenarioDefs[i].spec.Kind
		if !profileHasNonZeroField(scenarioDefs[i].profile) {
			t.Errorf("%s profile is all-zero (need >= 1 non-zero field)", kind)
		}
	}
}

// TestScenarioCatalog_Batch83_GroupCounts 六组分布 = 文档 §4.1 目标
// (市场 21 / 政策 15 / 社会 15 / 自然 21 / 未知 16 / 科技 13)。
func TestScenarioCatalog_Batch83_GroupCounts(t *testing.T) {
	want := map[string]int{
		ScenarioGroupMarket: 21, ScenarioGroupPolicy: 15,
		ScenarioGroupSociety: 15, ScenarioGroupNature: 21,
		ScenarioGroupUnknown: 16, ScenarioGroupTech: 13,
	}
	got := map[string]int{}
	for _, spec := range ScenarioCatalog() {
		got[spec.Group]++
	}
	for group, n := range want {
		if got[group] != n {
			t.Errorf("group %s = %d entries, want %d", group, got[group], n)
		}
	}
}
