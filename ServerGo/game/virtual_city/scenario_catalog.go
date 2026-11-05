// Package virtual_city — scenario_catalog.go: 场景目录统一装配与缓存
// (2026-11-05 §批次84 全维度事件体系与 Agent 双向对话)。
//
// 契约: lag_docs/虚拟城市/已实现/84-全维度事件体系与Agent双向对话/01-方案设计.md §4.1.1。
//
// 定位:批次83 后目录为 101 条(L1),本文件把目录重构为**三层来源**:
//
//	L1 = scenarioDefs(既有 101 条,一字不改,批次68/81/83 三批)
//	L2 = scenarioDefsBatch84*(批次84 精选条目,≥150 条)
//	L3 = scenarioTemplates*(批次84 模板参数化派生变体,≥850 条)
//
// 装配规则(契约 §4.1.1):
//   - **L1 优先**:kind 冲突时 L1 胜(既有语义优先);
//   - **上限截断**:目录条目总数上限由 scenario_catalog.max_entries 配置
//     (默认 10000,允许 100–10000);超限按 **L3 → L2 → L1 逆序**截断
//     (L1 既有条目永不丢);
//   - **构建一次**:包级缓存 + `map[string]*scenarioDef` 索引(O(1) 查询),
//     scenarioDefFor 由线性扫描改为查表(目录扩容后扫描是 O(n) 热点)。
//
// 纪律:
//   - **零 rand**:装配是纯数据拼接,无随机源;
//   - **零回归**:L1 切片字节不变(既有 101 条的 kind/幅度/参数逐字段相等);
//   - **线程安全**:目录构建走互斥锁,只读访问走读锁。
package virtual_city

import (
	"strings"
	"sync"
)

// 目录装配常量(契约 §4.1.1 / §6)。
const (
	// scenarioCatalogMaxEntriesDefault 目录条目总数上限缺省值(10000)。
	scenarioCatalogMaxEntriesDefault = 10000
	// scenarioCatalogMinEntries 目录条目总数上限下限(100)。
	scenarioCatalogMinEntries = 100
	// scenarioCatalogSearchLimitDefault 目录检索默认条数(缺省 30)。
	scenarioCatalogSearchLimitDefault = 30
	// scenarioCatalogSearchLimitMax 目录检索条数硬上限(200;防回灌爆)。
	scenarioCatalogSearchLimitMax = 200
)

var (
	// scenarioCatalogMu 目录装配互斥锁(写)与只读访问(读)共用。
	scenarioCatalogMu sync.RWMutex
	// scenarioCatalogBuilt 目录是否已构建(构建一次,配置变更时重建)。
	scenarioCatalogBuilt bool
	// scenarioCatalogMaxEntries 目录条目总数上限(可经 config 覆盖)。
	scenarioCatalogMaxEntries = scenarioCatalogMaxEntriesDefault
	// scenarioCatalogSearchLimit 目录检索默认条数(可经 config 覆盖)。
	scenarioCatalogSearchLimit = scenarioCatalogSearchLimitDefault
	// scenarioCatalogDefs 合并后的完整目录(def 顺序 = 目录展示顺序)。
	scenarioCatalogDefs []scenarioDef
	// scenarioCatalogKindIndex kind → 目录条目指针(O(1) 查询)。
	scenarioCatalogKindIndex map[string]*scenarioDef
)

// SetScenarioCatalogLimits 设置目录上限与检索默认条数
// (config.scenario_catalog → 装配层;缺失 ⇒ 走缺省 10000 / 30)。
//
// ⚠ 必须在任何目录访问**之前**调用(装配层在启动时 wiring);调用后若目录
// 已构建则**重建**(条目上限变化会改变目录内容)。
func SetScenarioCatalogLimits(maxEntries, searchLimit int) {
	scenarioCatalogMu.Lock()
	defer scenarioCatalogMu.Unlock()
	if maxEntries <= 0 {
		maxEntries = scenarioCatalogMaxEntriesDefault
	}
	if maxEntries < scenarioCatalogMinEntries {
		maxEntries = scenarioCatalogMinEntries
	}
	if maxEntries > scenarioCatalogMaxEntriesDefault {
		maxEntries = scenarioCatalogMaxEntriesDefault
	}
	if searchLimit <= 0 {
		searchLimit = scenarioCatalogSearchLimitDefault
	}
	if searchLimit > scenarioCatalogSearchLimitMax {
		searchLimit = scenarioCatalogSearchLimitMax
	}
	changed := maxEntries != scenarioCatalogMaxEntries || searchLimit != scenarioCatalogSearchLimit
	scenarioCatalogMaxEntries = maxEntries
	scenarioCatalogSearchLimit = searchLimit
	if changed && scenarioCatalogBuilt {
		scenarioCatalogBuildLocked()
	}
}

// ScenarioCatalogMaxEntries 返回当前目录条目总数上限(只读)。
func ScenarioCatalogMaxEntries() int {
	scenarioCatalogMu.RLock()
	defer scenarioCatalogMu.RUnlock()
	return scenarioCatalogMaxEntries
}

// ScenarioCatalogSearchLimitDefault 返回当前目录检索默认条数(只读)。
func ScenarioCatalogSearchLimitDefault() int {
	scenarioCatalogMu.RLock()
	defer scenarioCatalogMu.RUnlock()
	return scenarioCatalogSearchLimit
}

// scenarioCatalogBuildLocked 构建完整目录(L1 ∪ L2 ∪ L3)并建索引。
// 调用方必须持 scenarioCatalogMu 写锁。
//
// 步骤:
//  1. 各来源内部按 kind 去重(先出现者胜);
//  2. 跨来源去重按 L1 → L2 → L3 顺序(L1 优先);
//  3. 按上限截断:L3 先截、再截 L2,L1 永不丢;
//  4. 建 map 索引(kind → *scenarioDef)。
func scenarioCatalogBuildLocked() {
	l1 := scenarioDefs
	l2 := scenarioCatalogL2()
	l3 := scenarioCatalogL3()

	seen := make(map[string]bool, len(l1)+len(l2)+len(l3))
	keepL1 := make([]scenarioDef, 0, len(l1))
	for _, d := range l1 {
		if d.spec.Kind == "" || seen[d.spec.Kind] {
			continue
		}
		seen[d.spec.Kind] = true
		keepL1 = append(keepL1, d)
	}
	keepL2 := make([]scenarioDef, 0, len(l2))
	for _, d := range l2 {
		if d.spec.Kind == "" || seen[d.spec.Kind] {
			continue
		}
		seen[d.spec.Kind] = true
		keepL2 = append(keepL2, d)
	}
	keepL3 := make([]scenarioDef, 0, len(l3))
	for _, d := range l3 {
		if d.spec.Kind == "" || seen[d.spec.Kind] {
			continue
		}
		seen[d.spec.Kind] = true
		keepL3 = append(keepL3, d)
	}

	// 上限截断:L3 → L2 → L1 逆序(L1 永不丢)。
	budget := scenarioCatalogMaxEntries - len(keepL1)
	if budget < 0 {
		budget = 0
	}
	n2 := len(keepL2)
	if n2 > budget {
		n2 = budget
	}
	budget -= n2
	n3 := len(keepL3)
	if n3 > budget {
		n3 = budget
	}

	total := len(keepL1) + n2 + n3
	out := make([]scenarioDef, 0, total)
	out = append(out, keepL1...)
	out = append(out, keepL2[:n2]...)
	out = append(out, keepL3[:n3]...)

	index := make(map[string]*scenarioDef, len(out))
	for i := range out {
		index[out[i].spec.Kind] = &out[i]
	}
	scenarioCatalogDefs = out
	scenarioCatalogKindIndex = index
	scenarioCatalogBuilt = true
}

// ensureScenarioCatalogLocked 保证目录已构建(调用方持写锁)。
func ensureScenarioCatalogLocked() {
	if !scenarioCatalogBuilt {
		scenarioCatalogBuildLocked()
	}
}

// scenarioCatalogCached 返回目录条目切片(内部使用,**不拷贝**)。
//
// ⚠ 返回的是包级切片的**内部视图**:调用方只读,不得修改条目或切片。
// 外部调用方请用 ScenarioCatalog()(返回独立副本)。
func scenarioCatalogCached() []scenarioDef {
	scenarioCatalogMu.RLock()
	if scenarioCatalogBuilt {
		defer scenarioCatalogMu.RUnlock()
		return scenarioCatalogDefs
	}
	scenarioCatalogMu.RUnlock()

	scenarioCatalogMu.Lock()
	defer scenarioCatalogMu.Unlock()
	ensureScenarioCatalogLocked()
	return scenarioCatalogDefs
}

// ScenarioCatalogCount 返回目录条目总数(装配后恒定;只读)。
func ScenarioCatalogCount() int {
	return len(scenarioCatalogCached())
}

// ScenarioCatalog 返回场景目录(契约 §5.1 GET scenario_catalog)。
//
// 每次调用返回独立副本 —— Magnitudes/Params 切片可安全被调用方修改
// (既有契约不变);内部热路径请用 scenarioCatalogCached()。
func ScenarioCatalog() []ScenarioSpec {
	defs := scenarioCatalogCached()
	out := make([]ScenarioSpec, 0, len(defs))
	for i := range defs {
		spec := defs[i].spec
		spec.Magnitudes = append([]int(nil), scenarioMagnitudes...)
		spec.Params = append([]string{}, spec.Params...)
		if spec.DefaultMagnitude == 0 {
			spec.DefaultMagnitude = 3
		}
		out = append(out, spec)
	}
	return out
}

// scenarioDefFor 按 kind 查目录(未知 → false)。
//
// 批次84 起改为查 map(目录 1000+ 条后线性扫描是 O(n) 热点);
// 语义与既有线性扫描完全一致(首个匹配项胜)。
func scenarioDefFor(kind string) (*scenarioDef, bool) {
	if kind == "" {
		return nil, false
	}
	scenarioCatalogMu.RLock()
	if !scenarioCatalogBuilt {
		scenarioCatalogMu.RUnlock()
		scenarioCatalogMu.Lock()
		ensureScenarioCatalogLocked()
		scenarioCatalogMu.Unlock()
		scenarioCatalogMu.RLock()
	}
	def, ok := scenarioCatalogKindIndex[kind]
	scenarioCatalogMu.RUnlock()
	if !ok {
		return nil, false
	}
	return def, true
}

// scenarioCatalogKindIn 报告 kind 是否在目录内(只读;O(1))。
func scenarioCatalogKindIn(kind string) bool {
	_, ok := scenarioDefFor(kind)
	return ok
}

// scenarioCatalogGroupCount 返回各分组条目数(只读;仅含非空分组)。
func scenarioCatalogGroupCount() map[string]int {
	defs := scenarioCatalogCached()
	out := make(map[string]int, len(scenarioGroupOrder))
	for i := range defs {
		out[defs[i].spec.Group]++
	}
	return out
}

// scenarioCatalogSortIndex 返回「分组展示顺序 → 目录内原顺序」的稳定排序
// 下标映射(只读)。
//
// 分组顺序 = scenarioGroupOrder(既有 6 组 + 批次84 新增 7 组);不在分组表内
// 的未知分组(防御未来扩组)排在末尾,按首次出现顺序。
func scenarioCatalogSortIndex() map[string]int {
	defs := scenarioCatalogCached()
	order := make(map[string]int, len(scenarioGroupOrder))
	for i, g := range scenarioGroupOrder {
		order[g] = i
	}
	next := len(scenarioGroupOrder)
	for i := range defs {
		g := defs[i].spec.Group
		if _, ok := order[g]; !ok {
			order[g] = next
			next++
		}
	}
	return order
}

// scenarioCatalogMatch reports whether the spec matches the query
// (kind / name / description / group 的子串匹配;英文大小写不敏感)。
func scenarioCatalogMatch(spec ScenarioSpec, query string) bool {
	if query == "" {
		return true
	}
	q := strings.ToLower(query)
	return strings.Contains(strings.ToLower(spec.Kind), q) ||
		strings.Contains(strings.ToLower(spec.Name), q) ||
		strings.Contains(strings.ToLower(spec.Description), q) ||
		strings.Contains(strings.ToLower(spec.Group), q)
}
