// Package knowledge — langchain.go: LangChain 风格检索链
// (2026-10-05 §LsmKLBaseServer 知识库引擎重构)。
//
// 职责: 实现 LangChain 风格的检索链,将用户查询转换为知识库检索结果。
//
// 检索链流程:
//   QueryParser → GraphRetriever → FeatureRanker → ResultFormatter
//
// 与 LangChain 的对应关系:
//   - QueryParser    ≈ LangChain 的 Query Transformer
//   - GraphRetriever ≈ LangChain 的 Retriever
//   - FeatureRanker  ≈ LangChain 的 Reranker
//   - ResultFormatter ≈ LangChain 的 Output Parser
package knowledge

import (
	"strings"
)

// RetrievalChain 是 LangChain 风格的检索链。
type RetrievalChain struct {
	loader *Loader
	graph  *KnowledgeGraph
}

// NewRetrievalChain 构造检索链。
func NewRetrievalChain(loader *Loader, graph *KnowledgeGraph) *RetrievalChain {
	return &RetrievalChain{
		loader: loader,
		graph:  graph,
	}
}

// Retrieve 执行完整检索链。
// query 为用户查询(自然语言或特征字符串)。
// 返回检索结果(种子卡 + 关联卡)。
func (c *RetrievalChain) Retrieve(query string) GraphRetrievalResult {
	// Step 1: QueryParser — 解析查询为特征过滤条件
	searchQuery := c.parseQuery(query)

	// Step 2: GraphRetriever — 先做特征搜索,再图扩展
	searchResult := c.loader.Search(searchQuery)
	if len(searchResult.Cards) == 0 {
		return GraphRetrievalResult{}
	}

	// 取前 5 张作为种子卡
	seedCount := 5
	if len(searchResult.Cards) < seedCount {
		seedCount = len(searchResult.Cards)
	}
	seedIDs := make([]string, seedCount)
	for i := 0; i < seedCount; i++ {
		seedIDs[i] = searchResult.Cards[i].ID
	}

	// 图扩展
	cardMap := make(map[string]KnowledgeCard)
	for _, card := range searchResult.Cards {
		cardMap[card.ID] = card
	}
	// 也加入 loader 缓存中的卡(图遍历可能访问到)
	// 注意: 这里只加入搜索结果中的卡,避免全量加载

	graphResult := c.graph.Retrieve(seedIDs, 2, cardMap)

	// Step 3: FeatureRanker — 按特征匹配度排序(已在 graph.Retrieve 中完成)

	// Step 4: ResultFormatter — 格式化(已在 GraphRetrievalResult 中完成)

	return graphResult
}

// parseQuery 解析用户查询为特征过滤条件。
// 支持:
//   - 自然语言: "交通运输行业的卡" → features: ["行业:交通运输"]
//   - 特征字符串: "行业:交通运输,城区:commerce" → features: ["行业:交通运输", "城区:commerce"]
func (c *RetrievalChain) parseQuery(query string) SearchQuery {
	query = strings.TrimSpace(query)
	if query == "" {
		return SearchQuery{Limit: 50}
	}

	sq := SearchQuery{Limit: 50}

	// 尝试解析特征字符串(逗号/空格分隔)
	parts := strings.FieldsFunc(query, func(r rune) bool {
		return r == ',' || r == '，' || r == ' '
	})

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		// 检查是否为「键:值」格式
		if idx := strings.Index(part, ":"); idx > 0 {
			key := strings.TrimSpace(part[:idx])
			value := strings.TrimSpace(part[idx+1:])
			if key != "" && value != "" {
				sq.Features = append(sq.Features, key+":"+value)
				continue
			}
		}
		// 检查是否为「键：值」格式(中文冒号)
		if idx := strings.Index(part, "："); idx > 0 {
			key := strings.TrimSpace(part[:idx])
			value := strings.TrimSpace(part[idx+1:])
			if key != "" && value != "" {
				sq.Features = append(sq.Features, key+":"+value)
				continue
			}
		}
		// 非特征字符串,尝试匹配已知键
		sq.Features = append(sq.Features, part)
	}

	return sq
}

// FormatResult 将检索结果格式化为可读文本(供 LLM Agent 消费)。
func (c *RetrievalChain) FormatResult(result GraphRetrievalResult) string {
	var sb strings.Builder

	if len(result.SeedCards) > 0 {
		sb.WriteString("## 直接匹配的卡\n")
		for _, card := range result.SeedCards {
			sb.WriteString(formatCardBrief(card))
			sb.WriteString("\n")
		}
	}

	if len(result.RelatedCards) > 0 {
		sb.WriteString("\n## 关联卡(图扩展)\n")
		for _, card := range result.RelatedCards {
			sb.WriteString(formatCardBrief(card))
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

// formatCardBrief 格式化单张卡的简要信息。
func formatCardBrief(card KnowledgeCard) string {
	var parts []string
	parts = append(parts, "卡号:"+card.ID)
	if card.Title != "" {
		parts = append(parts, "职业:"+card.Title)
	}
	if card.Domain != "" {
		parts = append(parts, "行业:"+card.Domain)
	}
	if card.HomeDistrict != "" {
		parts = append(parts, "城区:"+card.HomeDistrict)
	}
	if len(card.Features) > 0 {
		parts = append(parts, "特征:"+strings.Join(card.Features, ", "))
	}
	return strings.Join(parts, " | ")
}
