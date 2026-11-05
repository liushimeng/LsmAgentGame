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

// TestScenarioCatalog_Batch83_Total101 L1 源 = 101 条(批次68 10 + 批次81 30
// + 批次83 61);kind 全局唯一;分组合法;默认强度 1–5;默认持续 ≥ 1 月;
// Name/Description 非空。
//
// ⚠ 批次84 起目录 = L1 ∪ L2 ∪ L3(规模 ≥1000),本测试只钉 L1 源不变;
// 全目录断言见 scenario_catalog_batch84_test.go。
func TestScenarioCatalog_Batch83_Total101(t *testing.T) {
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
	// L1 源 = 101 条,逐条 defaultDuration ≥ 1。
	if len(scenarioDefs) != 101 {
		t.Fatalf("scenarioDefs(L1) len = %d, want 101", len(scenarioDefs))
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

// TestScenarioCatalog_Batch83_GroupCounts L1 源六组分布 = 文档 §4.1 目标
// (市场 21 / 政策 15 / 社会 15 / 自然 21 / 未知 16 / 科技 13)。
//
// ⚠ 批次84 起全目录含 L2/L3 新条目,本测试只钉 L1 源分布不变。
func TestScenarioCatalog_Batch83_GroupCounts(t *testing.T) {
	want := map[string]int{
		ScenarioGroupMarket: 21, ScenarioGroupPolicy: 15,
		ScenarioGroupSociety: 15, ScenarioGroupNature: 21,
		ScenarioGroupUnknown: 16, ScenarioGroupTech: 13,
	}
	got := map[string]int{}
	for _, d := range scenarioDefs {
		got[d.spec.Group]++
	}
	for group, n := range want {
		if got[group] != n {
			t.Errorf("L1 group %s = %d entries, want %d", group, got[group], n)
		}
	}
}
