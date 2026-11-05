// Package virtual_city — scenario_catalog_batch84.go: 批次84 L2 / L3 目录
// 装配入口(2026-11-05 §批次84 全维度事件体系与 Agent 双向对话)。
//
// 契约: lag_docs/虚拟城市/已实现/84-全维度事件体系与Agent双向对话/01-方案设计.md §4.1.1。
//
// 三层来源(装配见 scenario_catalog.go):
//
//	L1 = scenarioDefs              (既有 101 条,一字不改)
//	L2 = scenarioDefsBatch84*(本批精选条目,≥150 条,见
//	     scenario_catalog_batch84_*.go)
//	L3 = scenarioTemplatesBatch84*(本批模板派生变体,≥850 条,见
//	     scenario_templates_batch84_*.go;派生引擎见 scenario_templates.go)
//
// 纪律:纯数据聚合,零 rand,不改任何既有切片。
package virtual_city

// scenarioCatalogL2 返回批次84 L2 精选条目(按维度文件聚合)。
//
// 拼接顺序按文件声明(市场+政策 → 社会+自然 → 未知 → 环境+安全+民生+科技 →
// 公共卫生+基础设施 → 交通+文化);**对外展示/检索顺序**由
// scenarioCatalogSortIndex 按 scenarioGroupOrder(既有 6 组 + 新增 7 组)
// 稳定排序,与本函数的拼接顺序无关。
func scenarioCatalogL2() []scenarioDef {
	out := make([]scenarioDef, 0, 192)
	out = append(out, scenarioDefsBatch84MarketPolicy...)
	out = append(out, scenarioDefsBatch84SocietyNature...)
	out = append(out, scenarioDefsBatch84Unknown...)
	out = append(out, scenarioDefsBatch84EnvSecurityLife...)
	out = append(out, scenarioDefsBatch84HealthInfra...)
	out = append(out, scenarioDefsBatch84TransportCulture...)
	return out
}

// scenarioCatalogL3 返回批次84 模板派生的全部变体(确定性、零 RNG)。
//
// 派生顺序:各模板切片按声明顺序;每个模板内按 axes 顺序的笛卡尔积
// (第一轴最慢)。同一模板重复调用结果逐字段相等。
func scenarioCatalogL3() []scenarioDef {
	groups := [][]scenarioTemplate{
		scenarioTemplatesBatch84Health,
		scenarioTemplatesBatch84Infra,
		scenarioTemplatesBatch84Transport,
		scenarioTemplatesBatch84CultureEnv,
		scenarioTemplatesBatch84TechMarket,
		scenarioTemplatesBatch84Security,
	}
	out := make([]scenarioDef, 0, 1024)
	for _, ts := range groups {
		for i := range ts {
			out = append(out, ts[i].scenarioTemplateVariants()...)
		}
	}
	return out
}
