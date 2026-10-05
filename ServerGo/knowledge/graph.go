// Package knowledge — graph.go: 知识图谱 + 图遍历
// (2026-10-05 §LsmKLBaseServer 知识库引擎重构)。
//
// 职责: 构建人物卡之间的知识图谱,支持图遍历检索。
//
// 图结构:
//   - 节点: 每张人物卡(卡号 + 域 + 城区 + 人格标签)
//   - 边: 卡片之间的关系
//     - same_industry — 同行业
//     - same_district — 同城区
//     - same_personality — 共同人格标签
//     - same_age_group — 同年龄段
//     - same_risk — 同风险偏好
//
// 图缓存在进程内,启动时从 MySQL 加载。
// 10 万级节点 + 边,内存约 200MB。
package knowledge

import (
	"sort"
	"sync"
)

// GraphRelation 是图边的关系类型。
const (
	RelationSameIndustry    = "same_industry"
	RelationSameDistrict    = "same_district"
	RelationSamePersonality = "same_personality"
	RelationSameAgeGroup    = "same_age_group"
	RelationSameRisk        = "same_risk"
)

// KnowledgeGraph 是知识图谱(进程内缓存)。
type KnowledgeGraph struct {
	mu    sync.RWMutex
	nodes map[string]*GraphNode // 卡号 → 节点
	edges map[string][]GraphEdge // 卡号 → 边列表
}

// NewKnowledgeGraph 构造空知识图谱。
func NewKnowledgeGraph() *KnowledgeGraph {
	return &KnowledgeGraph{
		nodes: make(map[string]*GraphNode),
		edges: make(map[string][]GraphEdge),
	}
}

// BuildFromCards 从知识卡列表构建图谱。
// 时间复杂度 O(n²) — 10 万卡约 100 亿次比较,实际通过预分桶优化。
func (g *KnowledgeGraph) BuildFromCards(cards []KnowledgeCard) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.nodes = make(map[string]*GraphNode, len(cards))
	g.edges = make(map[string][]GraphEdge, len(cards))

	// 预分桶: 按域、城区、年龄段、风险偏好分桶
	domainBuckets := make(map[string][]string)
	districtBuckets := make(map[string][]string)
	ageGroupBuckets := make(map[string][]string)
	riskBuckets := make(map[string][]string)
	personalityBuckets := make(map[string][]string)

	for _, card := range cards {
		node := &GraphNode{
			CardID: card.ID,
			Domain: card.Domain,
		}
		g.nodes[card.ID] = node

		// 分桶
		if card.Domain != "" {
			domainBuckets[card.Domain] = append(domainBuckets[card.Domain], card.ID)
		}
		if card.HomeDistrict != "" {
			districtBuckets[card.HomeDistrict] = append(districtBuckets[card.HomeDistrict], card.ID)
		}
		ageGroup := ageGroupOf(card.StartAge)
		ageGroupBuckets[ageGroup] = append(ageGroupBuckets[ageGroup], card.ID)
		if card.RiskPreference != "" {
			riskBuckets[card.RiskPreference] = append(riskBuckets[card.RiskPreference], card.ID)
		}
		for _, p := range card.Personality {
			personalityBuckets[p] = append(personalityBuckets[p], card.ID)
		}
	}

	// 同桶内两两连边
	for _, bucket := range domainBuckets {
		g.addEdgesWithinBucket(bucket, RelationSameIndustry, 1.0)
	}
	for _, bucket := range districtBuckets {
		g.addEdgesWithinBucket(bucket, RelationSameDistrict, 0.8)
	}
	for _, bucket := range ageGroupBuckets {
		g.addEdgesWithinBucket(bucket, RelationSameAgeGroup, 0.5)
	}
	for _, bucket := range riskBuckets {
		g.addEdgesWithinBucket(bucket, RelationSameRisk, 0.6)
	}
	for _, bucket := range personalityBuckets {
		g.addEdgesWithinBucket(bucket, RelationSamePersonality, 0.7)
	}
}

// addEdgesWithinBucket 在桶内两两连边(无向图,双向添加)。
func (g *KnowledgeGraph) addEdgesWithinBucket(bucket []string, relation string, weight float64) {
	for i := 0; i < len(bucket); i++ {
		for j := i + 1; j < len(bucket); j++ {
			g.edges[bucket[i]] = append(g.edges[bucket[i]], GraphEdge{
				To:       bucket[j],
				Relation: relation,
				Weight:   weight,
			})
			g.edges[bucket[j]] = append(g.edges[bucket[j]], GraphEdge{
				To:       bucket[i],
				Relation: relation,
				Weight:   weight,
			})
		}
	}
}

// Retrieve 从种子卡出发,BFS 遍历关联卡片。
// depth 为遍历深度(1 = 直接关联, 2 = 二度关联)。
// 返回种子卡 + 关联卡(按权重排序)。
func (g *KnowledgeGraph) Retrieve(seedCardIDs []string, depth int, cards map[string]KnowledgeCard) GraphRetrievalResult {
	g.mu.RLock()
	defer g.mu.RUnlock()

	result := GraphRetrievalResult{Depth: depth}

	// 种子卡
	for _, id := range seedCardIDs {
		if card, ok := cards[id]; ok {
			result.SeedCards = append(result.SeedCards, card)
		}
	}

	if depth <= 0 {
		return result
	}

	// BFS
	visited := make(map[string]bool)
	for _, id := range seedCardIDs {
		visited[id] = true
	}

	currentLevel := make([]string, len(seedCardIDs))
	copy(currentLevel, seedCardIDs)

	for d := 0; d < depth; d++ {
		nextLevel := make([]string, 0)
		for _, id := range currentLevel {
			edges := g.edges[id]
			for _, edge := range edges {
				if !visited[edge.To] {
					visited[edge.To] = true
					nextLevel = append(nextLevel, edge.To)
				}
			}
		}
		currentLevel = nextLevel
		if len(currentLevel) == 0 {
			break
		}
	}

	// 收集关联卡(按权重排序)
	type scoredCard struct {
		card  KnowledgeCard
		score float64
	}
	var scored []scoredCard
	for _, id := range currentLevel {
		if card, ok := cards[id]; ok {
			// 计算分数: 所有入边权重之和
			score := 0.0
			for _, edges := range g.edges {
				for _, e := range edges {
					if e.To == id {
						score += e.Weight
					}
				}
			}
			scored = append(scored, scoredCard{card: card, score: score})
		}
	}
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	// 取前 50 张
	limit := 50
	if len(scored) < limit {
		limit = len(scored)
	}
	for i := 0; i < limit; i++ {
		result.RelatedCards = append(result.RelatedCards, scored[i].card)
	}

	return result
}

// ageGroupOf 将年龄分组。
func ageGroupOf(age int) string {
	switch {
	case age < 25:
		return "18-24"
	case age < 30:
		return "25-29"
	case age < 35:
		return "30-34"
	case age < 40:
		return "35-39"
	case age < 45:
		return "40-44"
	case age < 50:
		return "45-49"
	default:
		return "50+"
	}
}

// NodeCount 返回图中节点数。
func (g *KnowledgeGraph) NodeCount() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.nodes)
}

// EdgeCount 返回图中边数。
func (g *KnowledgeGraph) EdgeCount() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	count := 0
	for _, edges := range g.edges {
		count += len(edges)
	}
	return count
}
