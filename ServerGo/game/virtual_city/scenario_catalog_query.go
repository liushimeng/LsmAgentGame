// Package virtual_city — scenario_catalog_query.go: 场景目录检索 API
// (2026-11-05 §批次84 全维度事件体系与 Agent 双向对话)。
//
// 契约: lag_docs/虚拟城市/已实现/84-全维度事件体系与Agent双向对话/01-方案设计.md §4.1.4。
//
// 定位:目录扩容到 1000+ 条后,模型与前端**必须**经检索定位目标 kind ——
// 本文件是唯一检索入口(纯只读、纯函数、零副作用)。
//
// 纪律:
//   - **只读**:全部方法只读目录缓存,不改任何状态;
//   - **恒定体积**:维度索引每组只给 ≤3 个代表 kind(不随目录条目数膨胀);
//   - **稳定排序**:结果按「分组展示顺序 → 目录内原顺序」排序;
//   - **不回调房间**(§92a):本文件不含任何房间/锁/回调。
package virtual_city

import (
	"strings"

	"LsmAgentGame/errcode"
)

// ScenarioDimension 是维度索引条目(供 system 提示词内联与目录浏览器用)。
type ScenarioDimension struct {
	// Group 分组名(前端展示名;13 个合法分组)。
	Group string `json:"group"`
	// Count 该组条目数(装配后恒定)。
	Count int `json:"count"`
	// Samples 代表 kind(最多 3 个,取该组目录内原顺序的前 3 个)。
	Samples []string `json:"samples"`
}

// scenarioDimensionSamplesMax 每组代表 kind 数上限(契约 §4.1.4:恒定体积)。
const scenarioDimensionSamplesMax = 3

// ScenarioCatalogSearch 按关键词 + 分组检索目录(只读;纯函数,零副作用)。
//
// 契约 §4.1.4:
//   - query 为空 ⇒ 只按 group 过滤;group 为空 ⇒ 全目录;两者都空 ⇒ 返回前 limit 条;
//   - 匹配:kind / name / description / group 的**子串**匹配(大小写不敏感);
//   - limit <= 0 ⇒ 缺省 30(config.scenario_catalog.search_limit);上限 200;
//   - 未知 group 名 ⇒ 返回空切片(调用方如需错误码请用
//     ScenarioCatalogSearchErr → ErrVirtualCityScenarioGroupUnknown 35064);
//   - 结果按「分组展示顺序 → 目录内原顺序」稳定排序。
func ScenarioCatalogSearch(query, group string, limit int) []ScenarioSpec {
	specs, _ := ScenarioCatalogSearchErr(query, group, limit)
	return specs
}

// ScenarioCatalogSearchErr 与 ScenarioCatalogSearch 同语义,但额外返回
// 「未知分组」错误码(ErrVirtualCityScenarioGroupUnknown 35064)。
//
// 未知 group 时返回(空切片, 35064 错误);其余情况错误为 nil。
func ScenarioCatalogSearchErr(query, group string, limit int) ([]ScenarioSpec, *errcode.Error) {
	out, e := scenarioCatalogFilter(query, group)
	if e != nil {
		return out, e
	}
	limit = scenarioCatalogLimit(limit)
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// ScenarioCatalogMatchCount 返回匹配 query/group 的条目总数(**limit 截断前**)。
//
// 口径(契约 §7.2):total = 「命中多少条」,用于分页 —— 跟随 group/q 过滤,
// **不受** limit 影响;无参时 = 全目录条目数。与 ScenarioCatalogSearch 共用
// scenarioCatalogFilter 过滤实现,保证「命中条数」与检索结果口径一致。
// 未知分组返回 (0, errcode 35064)。
func ScenarioCatalogMatchCount(query, group string) (int, error) {
	specs, e := scenarioCatalogFilter(query, group)
	if e != nil {
		return 0, e
	}
	return len(specs), nil
}

// scenarioCatalogFilter 是 ScenarioCatalogSearch / ScenarioCatalogMatchCount
// 的**共享过滤实现**(避免两处过滤逻辑漂移):返回匹配 query/group 的全部条目
// (limit 截断前),已按「分组展示顺序 → 目录内原顺序」稳定排序。
//
// 未知 group ⇒ (空切片, 35064)。
func scenarioCatalogFilter(query, group string) ([]ScenarioSpec, *errcode.Error) {
	group = strings.TrimSpace(group)
	if group != "" && !scenarioGroupKnown(group) {
		return []ScenarioSpec{}, errcode.CodeMsg(errcode.ErrVirtualCityScenarioGroupUnknown,
			"场景分组未知："+group+"（可用分组："+strings.Join(scenarioGroupOrder, "/")+"）")
	}

	defs := scenarioCatalogCached()
	order := scenarioCatalogSortIndex()

	type hit struct {
		idx  int
		spec ScenarioSpec
	}
	hits := make([]hit, 0, 16)
	for i := range defs {
		spec := defs[i].spec
		if group != "" && spec.Group != group {
			continue
		}
		if !scenarioCatalogMatch(spec, query) {
			continue
		}
		hits = append(hits, hit{idx: i, spec: scenarioCatalogSpec(defs[i])})
	}
	// 稳定排序:分组展示顺序 → 目录内原顺序(索引 i 已保证原顺序)。
	for a := 1; a < len(hits); a++ {
		for b := a; b > 0 &&
			order[hits[b].spec.Group] < order[hits[b-1].spec.Group]; b-- {
			hits[b], hits[b-1] = hits[b-1], hits[b]
		}
	}
	out := make([]ScenarioSpec, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.spec)
	}
	return out, nil
}

// ScenarioDimensionIndex 返回维度索引(13 个分组;每组 ≤3 个代表 kind)。
//
// **体积恒定**:不随目录条目数线性膨胀(单测钉死渲染文本 ≤ 4 KB)。
// 顺序 = scenarioGroupOrder(既有 6 组在前,批次84 新增 7 组在后)。
func ScenarioDimensionIndex() []ScenarioDimension {
	defs := scenarioCatalogCached()
	byGroup := make(map[string]*ScenarioDimension, len(scenarioGroupOrder))
	out := make([]ScenarioDimension, 0, len(scenarioGroupOrder))
	for _, g := range scenarioGroupOrder {
		d := ScenarioDimension{Group: g, Count: 0, Samples: []string{}}
		out = append(out, d)
		byGroup[g] = &out[len(out)-1]
	}
	for i := range defs {
		d, ok := byGroup[defs[i].spec.Group]
		if !ok {
			// 未知分组(防御未来扩组):不入索引,避免体积不可控。
			continue
		}
		d.Count++
		if len(d.Samples) < scenarioDimensionSamplesMax {
			d.Samples = append(d.Samples, defs[i].spec.Kind)
		}
	}
	return out
}

// ScenarioCatalogCount 返回目录条目总数(只读;实现见 scenario_catalog.go)。

// scenarioCatalogLimit 归一化 limit(<=0 ⇒ 缺省;硬上限 200)。
func scenarioCatalogLimit(limit int) int {
	if limit <= 0 {
		return ScenarioCatalogSearchLimitDefault()
	}
	if limit > scenarioCatalogSearchLimitMax {
		return scenarioCatalogSearchLimitMax
	}
	return limit
}

// scenarioCatalogSpec 把目录条目转换成对外的 ScenarioSpec(填充 Magnitudes
// 与默认强度,Params 拷贝防污染)。
func scenarioCatalogSpec(d scenarioDef) ScenarioSpec {
	spec := d.spec
	spec.Magnitudes = append([]int(nil), scenarioMagnitudes...)
	spec.Params = append([]string{}, spec.Params...)
	if spec.DefaultMagnitude == 0 {
		spec.DefaultMagnitude = 3
	}
	return spec
}
