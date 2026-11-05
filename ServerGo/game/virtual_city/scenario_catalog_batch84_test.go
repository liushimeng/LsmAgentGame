// Package virtual_city — scenario_catalog_batch84_test.go: 批次84 场景目录
// 三层来源(101 → 1100+)契约单测(2026-11-05 §批次84)。
//
// 契约: lag_docs/虚拟城市/已实现/84-全维度事件体系与Agent双向对话/01-方案设计.md §10.1。
//
// 覆盖(硬性断言):
//  1. 目录条目数 1000 ≤ n ≤ 10000;
//  2. 目录含 **13** 个分组,且 7 个新分组各 ≥ 1 条;
//  3. **金标准**:既有 101 条(L1)逐条仍在,且 Name/Group/DefaultMagnitude/
//     Params/Description 与 L1 源逐字段相等(拿 L1 切片做对照,不硬编码快照);
//  4. 模板确定性:同一模板派生两次逐字段相等;全目录 kind 唯一;
//  5. ScenarioDimensionIndex() 渲染文本 ≤ 4 KB;
//  6. ScenarioCatalogSearch:关键词命中 / group 过滤 / limit 上限 /
//     未知 group 返回空 + 35064;
//  7. SystemMetricHistory 只读与参数夹取;
//  8. 上限截断(max_entries)按 L3 → L2 → L1 逆序且 L1 永不丢。
package virtual_city

import (
	"encoding/json"
	"strings"
	"testing"

	"LsmAgentGame/errcode"
)

// TestScenarioCatalog_Batch84_Size 断言 1:目录条目数 1000 ≤ n ≤ 10000。
func TestScenarioCatalog_Batch84_Size(t *testing.T) {
	n := ScenarioCatalogCount()
	if n < 1000 || n > 10000 {
		t.Fatalf("catalog size = %d, want [1000,10000]", n)
	}
	if n != len(scenarioCatalogCached()) {
		t.Fatalf("ScenarioCatalogCount = %d, cache len = %d (must match)",
			n, len(scenarioCatalogCached()))
	}
}

// TestScenarioCatalog_Batch84_Groups 断言 2:13 个分组;7 个新分组各 ≥ 1 条。
func TestScenarioCatalog_Batch84_Groups(t *testing.T) {
	order := ScenarioGroupOrder()
	if len(order) != 13 {
		t.Fatalf("group order len = %d, want 13", len(order))
	}
	// 既有 6 组在前(逐字不变),7 个新组追加在后。
	want := []string{
		ScenarioGroupMarket, ScenarioGroupPolicy, ScenarioGroupSociety,
		ScenarioGroupNature, ScenarioGroupUnknown, ScenarioGroupTech,
		ScenarioGroupPublicHealth, ScenarioGroupInfrastructure,
		ScenarioGroupTransport, ScenarioGroupCulture,
		ScenarioGroupEnvironment, ScenarioGroupSecurity, ScenarioGroupDailyLife,
	}
	for i, g := range want {
		if order[i] != g {
			t.Fatalf("group order[%d] = %q, want %q", i, order[i], g)
		}
	}
	counts := scenarioCatalogGroupCount()
	newGroups := []string{
		ScenarioGroupPublicHealth, ScenarioGroupInfrastructure,
		ScenarioGroupTransport, ScenarioGroupCulture,
		ScenarioGroupEnvironment, ScenarioGroupSecurity, ScenarioGroupDailyLife,
	}
	for _, g := range newGroups {
		if counts[g] < 1 {
			t.Errorf("new group %q has %d entries, want >= 1", g, counts[g])
		}
	}
	for _, g := range want[:6] {
		if counts[g] < 1 {
			t.Errorf("existing group %q has %d entries, want >= 1", g, counts[g])
		}
	}
}

// TestScenarioCatalog_Batch84_GoldenL1 断言 3(金标准):既有 101 条逐条仍在,
// 且 Name/Group/DefaultMagnitude/Params/Description 与 L1 源逐字段相等。
//
// 对照源 = scenarioDefs(L1 切片,**不硬编码快照**)。
func TestScenarioCatalog_Batch84_GoldenL1(t *testing.T) {
	if len(scenarioDefs) != 101 {
		t.Fatalf("L1 source = %d entries, want 101", len(scenarioDefs))
	}
	catalog := ScenarioCatalog()
	byKind := make(map[string]ScenarioSpec, len(catalog))
	for _, s := range catalog {
		if _, dup := byKind[s.Kind]; dup {
			t.Fatalf("duplicate kind %q in merged catalog", s.Kind)
		}
		byKind[s.Kind] = s
	}
	for i := range scenarioDefs {
		src := scenarioDefs[i].spec
		got, ok := byKind[src.Kind]
		if !ok {
			t.Fatalf("L1 kind %q missing from merged catalog", src.Kind)
		}
		if got.Name != src.Name {
			t.Errorf("%s name drift: %q != %q", src.Kind, got.Name, src.Name)
		}
		if got.Group != src.Group {
			t.Errorf("%s group drift: %q != %q", src.Kind, got.Group, src.Group)
		}
		if got.Description != src.Description {
			t.Errorf("%s description drift", src.Kind)
		}
		if got.DefaultMagnitude != src.DefaultMagnitude {
			t.Errorf("%s default magnitude drift: %d != %d",
				src.Kind, got.DefaultMagnitude, src.DefaultMagnitude)
		}
		if len(got.Params) != len(src.Params) {
			t.Fatalf("%s params drift: %v != %v", src.Kind, got.Params, src.Params)
		}
		for j := range src.Params {
			if got.Params[j] != src.Params[j] {
				t.Fatalf("%s params drift: %v != %v", src.Kind, got.Params, src.Params)
			}
		}
		// L1 的 defaultDuration 也不得漂移(防御:目录装配只读 L1)。
		if scenarioDefs[i].defaultDuration < 1 {
			t.Errorf("%s defaultDuration = %d, want >= 1", src.Kind, scenarioDefs[i].defaultDuration)
		}
	}
}

// TestScenarioCatalog_Batch84_L2Markers 断言 3 补充:对账表 §1–§12 全部
// 🆕 标记 kind 都在目录内(契约 02-全维度事件体系对账表.md)。
func TestScenarioCatalog_Batch84_L2Markers(t *testing.T) {
	// 对账表 §1–§12 的 🆕 kind 全集(逐条对应)。
	want := []string{
		// §1 经济与市场
		"supply_demand_shift", "shop_open_close", "emerging_industry",
		"black_market", "financial_fraud", "subsidy_grant", "industrial_park",
		"investment_attraction", "consumption_voucher", "counterfeit_goods",
		"brand_entry_exit", "currency_reform", "wage_shift",
		// §2 政务与社会
		"census", "hukou_reform", "petition_incident", "community_conflict",
		"demolition_relocation", "public_service_upgrade", "healthcare_policy",
		"antitrust_probe", "ip_dispute", "admin_process_reform",
		"official_appointment", "integrity_probe",
		// §3 自然灾害
		"debris_flow", "permafrost_melt", "red_tide",
		// §4 极端灾难与科幻
		"geomagnetic_reversal", "asteroid_graze", "otherworld_gate",
		"mechanical_collapse", "energy_core_meltdown", "spacetime_rift",
		// §5 公共卫生
		"food_poisoning", "public_health_education", "occupational_disease",
		"drinking_water_contamination", "severe_air_pollution",
		"medical_malpractice", "emergency_public_health_response",
		// §6 基础设施
		"pipe_network_aging", "renewable_station_failure",
		// §10 科技发展
		"startup_boom", "patent_dispute", "digital_transformation",
		"smart_city_landing", "network_upgrade", "biotech_breakthrough",
		"autonomous_driving_pilot",
		// §11 公共安全
		"fire_accident", "data_leak",
	}
	byKind := make(map[string]bool, ScenarioCatalogCount())
	for _, s := range ScenarioCatalog() {
		byKind[s.Kind] = true
	}
	for _, kind := range want {
		if !byKind[kind] {
			t.Errorf("对账表 🆕 kind %q missing from catalog", kind)
		}
	}
}

// TestScenarioCatalog_Batch84_TemplateDeterministic 断言 4:同一模板派生两次
// 结果逐字段相等(确定性、零 RNG);全目录 kind 唯一。
func TestScenarioCatalog_Batch84_TemplateDeterministic(t *testing.T) {
	groups := [][]scenarioTemplate{
		scenarioTemplatesBatch84Health,
		scenarioTemplatesBatch84Infra,
		scenarioTemplatesBatch84Transport,
		scenarioTemplatesBatch84CultureEnv,
		scenarioTemplatesBatch84TechMarket,
		scenarioTemplatesBatch84Security,
	}
	for _, ts := range groups {
		for i := range ts {
			a := ts[i].scenarioTemplateVariants()
			b := ts[i].scenarioTemplateVariants()
			if len(a) != len(b) {
				t.Fatalf("template %q derived %d vs %d variants", ts[i].Spec.Kind, len(a), len(b))
			}
			for j := range a {
				if a[j].spec.Kind != b[j].spec.Kind ||
					a[j].spec.Name != b[j].spec.Name ||
					a[j].spec.Description != b[j].spec.Description ||
					a[j].profile != b[j].profile ||
					a[j].defaultDuration != b[j].defaultDuration {
					t.Fatalf("template %q variant %d non-deterministic", ts[i].Spec.Kind, j)
				}
			}
			if len(a) > scenarioTemplateMaxVariants {
				t.Fatalf("template %q variants %d > cap %d", ts[i].Spec.Kind, len(a), scenarioTemplateMaxVariants)
			}
		}
	}
	// 全目录 kind 唯一(含 L1/L2/L3)。
	seen := map[string]bool{}
	for _, s := range ScenarioCatalog() {
		if seen[s.Kind] {
			t.Fatalf("duplicate kind %q in merged catalog", s.Kind)
		}
		seen[s.Kind] = true
	}
}

// TestScenarioCatalog_Batch84_DimensionIndexSize 断言 5:维度索引渲染文本
// ≤ 4 KB(恒定体积,不随目录条目数线性膨胀)。
func TestScenarioCatalog_Batch84_DimensionIndexSize(t *testing.T) {
	dims := ScenarioDimensionIndex()
	if len(dims) != 13 {
		t.Fatalf("dimension index len = %d, want 13", len(dims))
	}
	for _, d := range dims {
		if d.Count < 1 {
			t.Errorf("dimension %q count = %d, want >= 1", d.Group, d.Count)
		}
		if len(d.Samples) > scenarioDimensionSamplesMax {
			t.Errorf("dimension %q samples = %d, want <= %d",
				d.Group, len(d.Samples), scenarioDimensionSamplesMax)
		}
	}
	raw, err := json.Marshal(dims)
	if err != nil {
		t.Fatalf("marshal dims: %v", err)
	}
	if len(raw) > 4096 {
		t.Fatalf("dimension index rendered size = %d bytes, want <= 4096", len(raw))
	}
}

// TestScenarioCatalogSearch_Batch84 断言 6:关键词命中 / group 过滤 /
// limit 上限 / 未知 group 返回空 + 35064。
func TestScenarioCatalogSearch_Batch84(t *testing.T) {
	// (a) 无参 ⇒ 前 limit 条(缺省 30)。
	all := ScenarioCatalogSearch("", "", 0)
	if len(all) != 30 {
		t.Fatalf("default search len = %d, want 30", len(all))
	}
	// (b) 关键词命中(子串、大小写不敏感)。
	hit := ScenarioCatalogSearch("地震", "", 200)
	if len(hit) == 0 {
		t.Fatal("query 地震 returned no hits")
	}
	hitEN := ScenarioCatalogSearch("EARTHQUAKE", "", 200)
	if len(hitEN) == 0 {
		t.Fatal("query EARTHQUAKE (case-insensitive) returned no hits")
	}
	// (c) group 过滤:只命中该组。
	for _, s := range ScenarioCatalogSearch("", ScenarioGroupPublicHealth, 200) {
		if s.Group != ScenarioGroupPublicHealth {
			t.Fatalf("group filter leaked %q (group %q)", s.Kind, s.Group)
		}
	}
	// (d) limit 上限 200(即使要求 9999)。
	capped := ScenarioCatalogSearch("", "", 9999)
	if len(capped) > 200 {
		t.Fatalf("limit cap failed: %d > 200", len(capped))
	}
	// (e) 非法 limit(负数)⇒ 缺省 30。
	neg := ScenarioCatalogSearch("", "", -5)
	if len(neg) != 30 {
		t.Fatalf("negative limit len = %d, want 30", len(neg))
	}
	// (f) 未知 group ⇒ 空切片 + 35064。
	specs, e := ScenarioCatalogSearchErr("", "不存在的分组", 10)
	if e == nil || e.Code != 35064 {
		t.Fatalf("unknown group err = %v, want 35064", e)
	}
	if len(specs) != 0 {
		t.Fatalf("unknown group specs = %d, want 0", len(specs))
	}
	// (g) group + q 组合过滤。
	both := ScenarioCatalogSearch("大流行", ScenarioGroupPublicHealth, 200)
	for _, s := range both {
		if s.Group != ScenarioGroupPublicHealth {
			t.Fatalf("group+q filter leaked %q", s.Kind)
		}
	}
	// (h) 稳定排序:同组内按目录原顺序。
	full := ScenarioCatalogSearch("", "", 200)
	order := scenarioCatalogSortIndex()
	for i := 1; i < len(full); i++ {
		prev, cur := order[full[i-1].Group], order[full[i].Group]
		if prev > cur {
			t.Fatalf("stable sort violated at %d: %q(%d) before %q(%d)",
				i, full[i-1].Group, prev, full[i].Group, cur)
		}
	}
	// (i) 搜索结果不得被调用方修改污染目录。
	if len(full) > 0 {
		full[0].Name = "MUTATED"
		if ScenarioCatalogSearch("", "", 200)[0].Name == "MUTATED" {
			t.Fatal("search results must be independent copies")
		}
	}
}

// TestScenarioCatalog_Batch84_MaxEntriesTruncation 断言 8:上限截断按
// L3 → L2 → L1 逆序,L1 永不丢。
func TestScenarioCatalog_Batch84_MaxEntriesTruncation(t *testing.T) {
	restore := ScenarioCatalogMaxEntries()
	defer SetScenarioCatalogLimits(restore, ScenarioCatalogSearchLimitDefault())

	// 上限压到 200:L1(101)必须全保,L2/L3 被截。
	SetScenarioCatalogLimits(200, ScenarioCatalogSearchLimitDefault())
	if n := ScenarioCatalogCount(); n > 200 {
		t.Fatalf("truncated catalog size = %d, want <= 200", n)
	}
	byKind := map[string]bool{}
	for _, s := range ScenarioCatalog() {
		byKind[s.Kind] = true
	}
	for i := range scenarioDefs {
		if !byKind[scenarioDefs[i].spec.Kind] {
			t.Fatalf("L1 kind %q dropped under truncation (L1 must never be lost)",
				scenarioDefs[i].spec.Kind)
		}
	}
	// 下限保护:100 以下被 clamp 到 100。
	SetScenarioCatalogLimits(1, ScenarioCatalogSearchLimitDefault())
	if n := ScenarioCatalogMaxEntries(); n != 100 {
		t.Fatalf("max entries clamp = %d, want 100", n)
	}
	// 恢复缺省。
	SetScenarioCatalogLimits(10000, ScenarioCatalogSearchLimitDefault())
}

// TestSystemMetricHistory_Batch84 断言 7:只读 + 参数夹取。
func TestSystemMetricHistory_Batch84(t *testing.T) {
	r := newScenarioTestRoom(t, "mh-b84", 20261105)
	// 空环 ⇒ 空切片(非 nil)。
	if got := r.SystemMetricHistory("cpi", 10); len(got) != 0 {
		t.Fatalf("empty history len = %d, want 0", len(got))
	}
	// 采样若干次(直接走采样函数;房间层持锁由采样点保证)。
	for i := 0; i < 5; i++ {
		r.mu.Lock()
		r.sampleSystemMetricLocked()
		r.mu.Unlock()
	}
	if n := r.SystemMetricHistoryLen(); n != 5 {
		t.Fatalf("history len = %d, want 5", n)
	}
	// months 夹取:<=0 ⇒ 1;>60 ⇒ 60。
	if got := r.SystemMetricHistory("cpi", -3); len(got) != 1 {
		t.Fatalf("months=-3 len = %d, want 1", len(got))
	}
	if got := r.SystemMetricHistory("cpi", 999); len(got) != 5 {
		t.Fatalf("months=999 len = %d, want 5(全部)", len(got))
	}
	// 非法 metric ⇒ 空切片。
	if got := r.SystemMetricHistory("bogus", 10); len(got) != 0 {
		t.Fatalf("invalid metric len = %d, want 0", len(got))
	}
	if got := r.SystemMetricHistory("", 10); len(got) != 0 {
		t.Fatalf("empty metric len = %d, want 0", len(got))
	}
	// 单指标视图:只填充请求的 metric 字段。
	for _, s := range r.SystemMetricHistory("stock", 10) {
		if s.CPI != 0 || s.Unemployment != 0 || s.Wage != 0 ||
			s.Gold != 0 || s.House != 0 || s.Treasury != 0 || s.Satisfaction != 0 {
			t.Fatalf("metric=stock must zero other fields: %+v", s)
		}
	}
	// 只读:返回副本,修改不污染内部。
	got := r.SystemMetricHistory("cpi", 10)
	if len(got) > 0 {
		got[0].CPI = 999
		again := r.SystemMetricHistory("cpi", 10)
		if again[0].CPI == 999 {
			t.Fatal("SystemMetricHistory must return a copy")
		}
	}
	// nil 安全。
	var nilRoom *VirtualCityRoom
	if got := nilRoom.SystemMetricHistory("cpi", 10); got == nil || len(got) != 0 {
		t.Fatal("nil room must return empty non-nil slice")
	}
	// 全部 metric 名合法。
	for _, m := range SystemMetricNames {
		if !systemMetricNameValid(m) {
			t.Errorf("SystemMetricNames entry %q not valid", m)
		}
		if strings.TrimSpace(m) != m {
			t.Errorf("SystemMetricNames entry %q has surrounding space", m)
		}
	}
}

// TestScenarioBatch84_WageRevenueKnobs 断言 9:批次84 新增两旋钮
// (WageShockPct / RevenueShockPct)真实映射到既有 Labor.AvgWageCNY /
// Labor.RevenueCNY,**不新增 World 字段**;防御性下限(不得为负)。
func TestScenarioBatch84_WageRevenueKnobs(t *testing.T) {
	r := newScenarioTestRoom(t, "knobs-b84", 20261105)
	w := r.World
	if w.Labor == nil {
		t.Fatal("Labor must be non-nil after Start")
	}
	// Labor 月结后才有值(LaborMonthStep 重算);此处显式给基线以便断言比例冲击。
	w.Labor.AvgWageCNY = 10000
	w.Labor.RevenueCNY = 1000000
	wageBefore := w.Labor.AvgWageCNY
	revBefore := w.Labor.RevenueCNY
	// 正向冲击:工资 +8%、营收 +12%(× L3)。
	prof := scenarioProfile{WageShockPct: 0.08, RevenueShockPct: 0.12}
	effects := w.scenarioApplyWageRevenueShock(prof, 3, nil)
	if w.Labor.AvgWageCNY <= wageBefore {
		t.Fatalf("wage did not rise: %d -> %d", wageBefore, w.Labor.AvgWageCNY)
	}
	if w.Labor.RevenueCNY <= revBefore {
		t.Fatalf("revenue did not rise: %d -> %d", revBefore, w.Labor.RevenueCNY)
	}
	if len(effects) != 2 {
		t.Fatalf("effects = %d, want 2", len(effects))
	}
	// 负向冲击 + 防御性下限:极端负值 clamp 到 0,不为负。
	neg := scenarioProfile{WageShockPct: -0.05, RevenueShockPct: -0.05}
	w.scenarioApplyWageRevenueShock(neg, 5, nil) // -25% 每次
	w.scenarioApplyWageRevenueShock(neg, 5, nil)
	w.scenarioApplyWageRevenueShock(neg, 5, nil)
	w.scenarioApplyWageRevenueShock(neg, 5, nil)
	if w.Labor.AvgWageCNY < 0 {
		t.Fatalf("wage must not go negative: %d", w.Labor.AvgWageCNY)
	}
	if w.Labor.RevenueCNY < 0 {
		t.Fatalf("revenue must not go negative: %d", w.Labor.RevenueCNY)
	}
	// 既有 12 旋钮零回归:零值 profile 不改任何字段。
	wageSnap := w.Labor.AvgWageCNY
	revSnap := w.Labor.RevenueCNY
	w.scenarioApplyWageRevenueShock(scenarioProfile{}, 5, nil)
	if w.Labor.AvgWageCNY != wageSnap || w.Labor.RevenueCNY != revSnap {
		t.Fatal("zero-valued new knobs must not change Labor fields")
	}
	// Labor 为 nil ⇒ 跳过(不 panic)。
	var nilWorld *World
	if got := nilWorld.scenarioApplyWageRevenueShock(prof, 3, nil); len(got) != 0 {
		t.Fatal("nil world must return effects unchanged")
	}
	// 既有 101 条场景的这两个旋钮恒为 0(零回归)。
	for i := range scenarioDefs {
		if scenarioDefs[i].profile.WageShockPct != 0 ||
			scenarioDefs[i].profile.RevenueShockPct != 0 {
			t.Fatalf("L1 %q must keep WageShockPct/RevenueShockPct == 0",
				scenarioDefs[i].spec.Kind)
		}
	}
}

// TestScenarioCatalog_Batch84_L2L3ProfileNonZero 断言 10:L2/L3 条目
// profile 至少 1 个非零字段(防「声明了却从不接线」§130 空转条目);
// defaultDuration ≥ 1;Name/Description/Group 非空。
func TestScenarioCatalog_Batch84_L2L3ProfileNonZero(t *testing.T) {
	defs := append(scenarioCatalogL2(), scenarioCatalogL3()...)
	for _, d := range defs {
		if !profileHasNonZeroField(d.profile) {
			t.Errorf("%s profile all-zero", d.spec.Kind)
		}
		if d.defaultDuration < 1 {
			t.Errorf("%s defaultDuration = %d", d.spec.Kind, d.defaultDuration)
		}
		if d.spec.Description == "" || d.spec.Name == "" || d.spec.Group == "" {
			t.Errorf("%s missing name/desc/group", d.spec.Kind)
		}
	}
}

// TestScenarioCatalogMatchCount_ConsistentWithSearch ScenarioCatalogMatchCount
// 与 ScenarioCatalogSearch 过滤口径一致(契约 §7.2):同 query/group 下
// limit 取大值时 len(items) == count;count 不受 limit 影响。
func TestScenarioCatalogMatchCount_ConsistentWithSearch(t *testing.T) {
	cases := []struct{ query, group string }{
		{"", ""},
		{"", ScenarioGroupPublicHealth},
		{"", ScenarioGroupMarket},
		{"地震", ""},
		{"EARTHQUAKE", ""},
		{"大流行", ScenarioGroupPublicHealth},
		{"不存在的关键词xyz", ""},
	}
	for _, c := range cases {
		n, err := ScenarioCatalogMatchCount(c.query, c.group)
		if err != nil {
			t.Fatalf("MatchCount(%q,%q) err = %v", c.query, c.group, err)
		}
		// limit 取大值(200 硬上限):len(items) == min(count, 200)。
		want := n
		if want > scenarioCatalogSearchLimitMax {
			want = scenarioCatalogSearchLimitMax
		}
		if got := len(ScenarioCatalogSearch(c.query, c.group, 200)); got != want {
			t.Errorf("MatchCount(%q,%q) = %d but Search(200) len = %d",
				c.query, c.group, n, got)
		}
		// count 不受 limit 影响:limit=5 只截列表。
		wantSmall := n
		if wantSmall > 5 {
			wantSmall = 5
		}
		if got := len(ScenarioCatalogSearch(c.query, c.group, 5)); got != wantSmall {
			t.Errorf("MatchCount(%q,%q) = %d but Search(5) len = %d",
				c.query, c.group, n, got)
		}
	}
	// 无参 ⇒ 全目录条目数。
	if n, _ := ScenarioCatalogMatchCount("", ""); n != ScenarioCatalogCount() {
		t.Errorf("empty query/group count = %d, want catalog count %d",
			n, ScenarioCatalogCount())
	}
	// 未知分组 ⇒ (0, 35064)。
	n, err := ScenarioCatalogMatchCount("", "不存在的分组")
	if n != 0 {
		t.Fatalf("unknown group count = %d, want 0", n)
	}
	ee, ok := err.(*errcode.Error)
	if !ok || ee.Code != 35064 {
		t.Fatalf("unknown group err = %v, want 35064", err)
	}
}
