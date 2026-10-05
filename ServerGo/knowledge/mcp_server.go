// Package knowledge — mcp_server.go: MySQL MCP 服务
// (2026-10-05 §LsmKLBaseServer 知识库引擎重构)。
//
// 职责: 将 MySQL 中的特征数据通过 MCP (Model Context Protocol) 标准协议
// 暴露,供外部 LLM Agent 消费。
//
// MCP 协议: JSON-RPC 2.0 over HTTP POST。
// 支持的方法:
//   - tools/list — 列出可用工具
//   - tools/call — 调用工具
//
// 可用工具:
//   - search_cards      — 按特征字符串搜索卡片
//   - get_card          — 按卡号获取单卡
//   - list_by_domain    — 按行业域列出卡片
//   - list_by_district  — 按城区列出卡片
//   - get_stats         — 获取知识库统计
//   - extract_features  — 触发特征抽取(管理)
package knowledge

import (
	"encoding/json"
	"fmt"
)

// MCPServer 是知识库 MCP 服务。
type MCPServer struct {
	loader *Loader
}

// NewMCPServer 构造 MCP 服务。
func NewMCPServer(loader *Loader) *MCPServer {
	return &MCPServer{loader: loader}
}

// HandleRequest 处理 MCP JSON-RPC 请求,返回响应。
func (s *MCPServer) HandleRequest(req MCPRequest) MCPResponse {
	switch req.Method {
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(req)
	default:
		return MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &MCPError{
				Code:    -32601,
				Message: fmt.Sprintf("method not found: %s", req.Method),
			},
		}
	}
}

// handleToolsList 处理 tools/list 方法。
func (s *MCPServer) handleToolsList(req MCPRequest) MCPResponse {
	tools := []MCPToolDescriptor{
		{
			Name:        "search_cards",
			Description: "按特征字符串搜索人物卡。特征字符串格式为「键:值」，如「行业:交通运输」。多个特征为 AND 语义。",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"features": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "特征字符串数组(AND 语义)",
					},
					"domain": map[string]interface{}{
						"type":        "string",
						"description": "行业域过滤(精确匹配)",
					},
					"district": map[string]interface{}{
						"type":        "string",
						"description": "城区过滤(精确匹配)",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "返回数量上限(默认 50)",
						"default":     50,
					},
				},
			},
		},
		{
			Name:        "get_card",
			Description: "按卡号获取单张人物卡详情。",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"card_id": map[string]interface{}{
						"type":        "string",
						"description": "卡号(如 N9012345)",
					},
				},
				"required": []string{"card_id"},
			},
		},
		{
			Name:        "list_by_domain",
			Description: "按行业域列出人物卡。",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"domain": map[string]interface{}{
						"type":        "string",
						"description": "行业域(如 A-农林牧渔)",
					},
					"offset": map[string]interface{}{
						"type":        "integer",
						"description": "分页偏移(默认 0)",
						"default":     0,
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "每页数量(默认 50)",
						"default":     50,
					},
				},
				"required": []string{"domain"},
			},
		},
		{
			Name:        "list_by_district",
			Description: "按城区列出人物卡。",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"district": map[string]interface{}{
						"type":        "string",
						"description": "城区 id(如 commerce)",
					},
					"offset": map[string]interface{}{
						"type":        "integer",
						"description": "分页偏移(默认 0)",
						"default":     0,
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "每页数量(默认 50)",
						"default":     50,
					},
				},
				"required": []string{"district"},
			},
		},
		{
			Name:        "get_stats",
			Description: "获取知识库统计信息(总卡数、已抽取数、各域分布等)。",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "extract_features",
			Description: "触发对未抽取卡的特征抽取(管理用途)。",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"card_ids": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "指定卡号列表(空 = 全量未抽取)",
					},
				},
			},
		},
	}

	return MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"tools": tools,
		},
	}
}

// handleToolsCall 处理 tools/call 方法。
func (s *MCPServer) handleToolsCall(req MCPRequest) MCPResponse {
	var call MCPToolCall
	if err := json.Unmarshal(req.Params, &call); err != nil {
		return MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &MCPError{
				Code:    -32602,
				Message: "invalid params: " + err.Error(),
			},
		}
	}

	switch call.Name {
	case "search_cards":
		return s.toolSearchCards(req, call.Arguments)
	case "get_card":
		return s.toolGetCard(req, call.Arguments)
	case "list_by_domain":
		return s.toolListByDomain(req, call.Arguments)
	case "list_by_district":
		return s.toolListByDistrict(req, call.Arguments)
	case "get_stats":
		return s.toolGetStats(req, call.Arguments)
	case "extract_features":
		return s.toolExtractFeatures(req, call.Arguments)
	default:
		return MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &MCPError{
				Code:    -32602,
				Message: fmt.Sprintf("unknown tool: %s", call.Name),
			},
		}
	}
}

// toolSearchCards 实现 search_cards 工具。
func (s *MCPServer) toolSearchCards(req MCPRequest, args map[string]interface{}) MCPResponse {
	query := SearchQuery{}
	if features, ok := args["features"].([]interface{}); ok {
		for _, f := range features {
			if s, ok := f.(string); ok {
				query.Features = append(query.Features, s)
			}
		}
	}
	if domain, ok := args["domain"].(string); ok {
		query.Domain = domain
	}
	if district, ok := args["district"].(string); ok {
		query.District = district
	}
	if limit, ok := args["limit"].(float64); ok {
		query.Limit = int(limit)
	}

	result := s.loader.Search(query)
	return MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"cards":   result.Cards,
			"matched": result.Matched,
			"total":   result.Total,
		},
	}
}

// toolGetCard 实现 get_card 工具。
func (s *MCPServer) toolGetCard(req MCPRequest, args map[string]interface{}) MCPResponse {
	cardID, _ := args["card_id"].(string)
	card, ok := s.loader.GetCard(cardID)
	if !ok {
		return MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &MCPError{
				Code:    -32602,
				Message: fmt.Sprintf("card not found: %s", cardID),
			},
		}
	}
	return MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"card": card,
		},
	}
}

// toolListByDomain 实现 list_by_domain 工具。
func (s *MCPServer) toolListByDomain(req MCPRequest, args map[string]interface{}) MCPResponse {
	domain, _ := args["domain"].(string)
	offset := 0
	if v, ok := args["offset"].(float64); ok {
		offset = int(v)
	}
	limit := 50
	if v, ok := args["limit"].(float64); ok {
		limit = int(v)
	}

	result := s.loader.Search(SearchQuery{
		Domain: domain,
		Offset: offset,
		Limit:  limit,
	})
	return MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"cards":   result.Cards,
			"matched": result.Matched,
			"total":   result.Total,
		},
	}
}

// toolListByDistrict 实现 list_by_district 工具。
func (s *MCPServer) toolListByDistrict(req MCPRequest, args map[string]interface{}) MCPResponse {
	district, _ := args["district"].(string)
	offset := 0
	if v, ok := args["offset"].(float64); ok {
		offset = int(v)
	}
	limit := 50
	if v, ok := args["limit"].(float64); ok {
		limit = int(v)
	}

	result := s.loader.Search(SearchQuery{
		District: district,
		Offset:   offset,
		Limit:    limit,
	})
	return MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"cards":   result.Cards,
			"matched": result.Matched,
			"total":   result.Total,
		},
	}
}

// toolGetStats 实现 get_stats 工具。
func (s *MCPServer) toolGetStats(req MCPRequest, args map[string]interface{}) MCPResponse {
	stats := s.loader.GetStats()
	return MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"total":     stats.Total,
			"extracted": stats.Extracted,
			"failed":    stats.Failed,
			"domains":   stats.Domains,
			"districts": stats.Districts,
		},
	}
}

// toolExtractFeatures 实现 extract_features 工具。
func (s *MCPServer) toolExtractFeatures(req MCPRequest, args map[string]interface{}) MCPResponse {
	// 注意: 实际抽取需要 context 和 profession.Loader 协作,
	// 这里仅返回待抽取数量(实际抽取由管理 API 触发)。
	stats := s.loader.GetStats()
	pending := stats.Total - stats.Extracted - stats.Failed
	return MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"pending": pending,
			"message": "use POST /api/knowledge/extract to trigger extraction",
		},
	}
}
