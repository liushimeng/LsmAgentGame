// Package config — scenario_catalog_config.go: 批次84 场景目录配置段
// (2026-11-05 §批次84 全维度事件体系与 Agent 双向对话)。
//
// 契约: lag_docs/虚拟城市/已实现/84-全维度事件体系与Agent双向对话/01-方案设计.md §6。
//
// 独立成文件而非并入 config.go —— config.go 已接近 §4 单文件 ≤1800 行硬上限
// (1767 行),本段约 60 行;拆出后 config.go 维持在限内,结构也与既有
// 「配置段就近声明」惯例一致(字段仍在 Config 结构体、默认值仍在 applyDefaults)。
package config

// ScenarioCatalogConfig 是 scenario_catalog{} 顶级段(批次84;契约 §6):
// 虚拟城市场景目录的规模上限与检索默认条数。
//
// ⚠ 本段**缺失**等同零值 ⇒ 走 Resolved() 的缺省值
// (max_entries=10000、search_limit=30),不会导致目录为空。
type ScenarioCatalogConfig struct {
	// MaxEntries 目录条目总数上限(缺省 10000;允许 100–10000)。
	// 装配时若超上限,按 L3 → L2 → L1 的**逆序**截断(L1 既有 101 条永不丢)。
	MaxEntries int `json:"max_entries"`
	// SearchLimit 目录检索默认返回条数(缺省 30;上限 200 由检索层强制)。
	SearchLimit int `json:"search_limit"`
}

// ResolvedScenarioCatalogConfig 是归一化后的 scenario_catalog 配置。
type ResolvedScenarioCatalogConfig struct {
	// MaxEntries 目录条目总数上限(clamp [100,10000])。
	MaxEntries int
	// SearchLimit 检索默认条数(clamp [1,200])。
	SearchLimit int
}

// 场景目录配置边界常量(契约 §6:max_entries 允许 100–10000)。
const (
	// ScenarioCatalogMaxEntriesDefault 目录条目总数上限缺省值。
	ScenarioCatalogMaxEntriesDefault = 10000
	// ScenarioCatalogMaxEntriesMin 目录条目总数上限下限。
	ScenarioCatalogMaxEntriesMin = 100
	// ScenarioCatalogMaxEntriesMax 目录条目总数上限上限。
	ScenarioCatalogMaxEntriesMax = 10000
	// ScenarioCatalogSearchLimitDefault 检索默认条数缺省值。
	ScenarioCatalogSearchLimitDefault = 30
	// ScenarioCatalogSearchLimitMax 检索条数硬上限(防 tool_result_limit 回灌爆)。
	ScenarioCatalogSearchLimitMax = 200
)

// Resolved 返回归一化配置(缺省值见上;零值安全,不 panic)。
func (c ScenarioCatalogConfig) Resolved() ResolvedScenarioCatalogConfig {
	r := ResolvedScenarioCatalogConfig{
		MaxEntries:  c.MaxEntries,
		SearchLimit: c.SearchLimit,
	}
	if r.MaxEntries <= 0 {
		r.MaxEntries = ScenarioCatalogMaxEntriesDefault
	}
	if r.MaxEntries < ScenarioCatalogMaxEntriesMin {
		r.MaxEntries = ScenarioCatalogMaxEntriesMin
	}
	if r.MaxEntries > ScenarioCatalogMaxEntriesMax {
		r.MaxEntries = ScenarioCatalogMaxEntriesMax
	}
	if r.SearchLimit <= 0 {
		r.SearchLimit = ScenarioCatalogSearchLimitDefault
	}
	if r.SearchLimit > ScenarioCatalogSearchLimitMax {
		r.SearchLimit = ScenarioCatalogSearchLimitMax
	}
	return r
}
