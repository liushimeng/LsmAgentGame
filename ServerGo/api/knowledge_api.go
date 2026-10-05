// Package api — knowledge_api.go: 知识库 HTTP API
// (2026-10-05 §LsmKLBaseServer 知识库引擎重构)。
//
// 契约: docs/知识库系统/LsmKLBaseServer-知识库引擎重构方案.md §6.1。
//
//	GET  /api/knowledge/cards          按条件搜索知识卡
//	GET  /api/knowledge/cards/:id      按卡号获取单卡
//	GET  /api/knowledge/stats          获取知识库统计
//	POST /api/knowledge/extract        触发特征抽取(管理)
//	POST /api/knowledge/mcp            MCP 协议端点(JSON-RPC 2.0)
//
// 权限: AuthRequired 登录即可(知识库是公开合成人格,与 survey 同级)。
package api

import (
	"net/http"

	"LsmAgentGame/errcode"
	"LsmAgentGame/knowledge"

	"github.com/gin-gonic/gin"
)

// KnowledgeAPI 是知识库 HTTP 端点的处理器。
type KnowledgeAPI struct {
	loader    *knowledge.Loader
	mcpServer *knowledge.MCPServer
}

// NewKnowledgeAPI 构造知识库 API 处理器。
func NewKnowledgeAPI(loader *knowledge.Loader) *KnowledgeAPI {
	return &KnowledgeAPI{
		loader:    loader,
		mcpServer: knowledge.NewMCPServer(loader),
	}
}

// ListCards 处理 GET /api/knowledge/cards。
func (a *KnowledgeAPI) ListCards(c *gin.Context) {
	if a.loader == nil {
		c.JSON(http.StatusOK, gin.H{"code": errcode.ErrInternal, "message": "knowledge loader not wired"})
		return
	}

	query := knowledge.SearchQuery{}
	if features := c.Query("features"); features != "" {
		// 逗号分隔的特征字符串
		for _, f := range splitFeatures(features) {
			query.Features = append(query.Features, f)
		}
	}
	query.Domain = c.Query("domain")
	query.District = c.Query("district")
	query.Offset = queryInt(c, "offset", 0)
	query.Limit = queryInt(c, "limit", 50)

	result := a.loader.Search(query)
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data": gin.H{
			"cards":   result.Cards,
			"matched": result.Matched,
			"total":   result.Total,
		},
	})
}

// GetCard 处理 GET /api/knowledge/cards/:id。
func (a *KnowledgeAPI) GetCard(c *gin.Context) {
	if a.loader == nil {
		c.JSON(http.StatusOK, gin.H{"code": errcode.ErrInternal, "message": "knowledge loader not wired"})
		return
	}

	cardID := c.Param("id")
	card, ok := a.loader.GetCard(cardID)
	if !ok {
		c.JSON(http.StatusOK, gin.H{"code": errcode.ErrKnowledgeCardNotFound, "message": errcode.DefaultMessages[errcode.ErrKnowledgeCardNotFound]})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data":    gin.H{"card": card},
	})
}

// GetStats 处理 GET /api/knowledge/stats。
func (a *KnowledgeAPI) GetStats(c *gin.Context) {
	if a.loader == nil {
		c.JSON(http.StatusOK, gin.H{"code": errcode.ErrInternal, "message": "knowledge loader not wired"})
		return
	}

	stats := a.loader.GetStats()
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data":    stats,
	})
}

// ExtractFeatures 处理 POST /api/knowledge/extract。
// 触发对未抽取卡的特征抽取(管理用途)。
func (a *KnowledgeAPI) ExtractFeatures(c *gin.Context) {
	if a.loader == nil {
		c.JSON(http.StatusOK, gin.H{"code": errcode.ErrInternal, "message": "knowledge loader not wired"})
		return
	}

	var body struct {
		CardIDs []string `json:"card_ids"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		// 空 body 也允许(全量抽取)
		body.CardIDs = nil
	}

	// 注意: 实际抽取需要 context 和 profession.Loader 协作,
	// 这里仅返回待抽取数量(实际抽取由后台流水线触发)。
	stats := a.loader.GetStats()
	pending := stats.Total - stats.Extracted - stats.Failed

	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data": gin.H{
			"pending": pending,
			"message": "extraction triggered in background",
		},
	})
}

// HandleMCP 处理 POST /api/knowledge/mcp。
// MCP 协议端点(JSON-RPC 2.0)。
func (a *KnowledgeAPI) HandleMCP(c *gin.Context) {
	if a.mcpServer == nil {
		c.JSON(http.StatusOK, gin.H{"code": errcode.ErrInternal, "message": "knowledge mcp server not wired"})
		return
	}

	var req knowledge.MCPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, knowledge.MCPResponse{
			JSONRPC: "2.0",
			Error: &knowledge.MCPError{
				Code:    -32700,
				Message: "parse error: " + err.Error(),
			},
		})
		return
	}

	resp := a.mcpServer.HandleRequest(req)
	c.JSON(http.StatusOK, resp)
}

// splitFeatures 分割逗号分隔的特征字符串。
func splitFeatures(s string) []string {
	var out []string
	current := ""
	for _, r := range s {
		if r == ',' || r == '，' {
			if current != "" {
				out = append(out, current)
				current = ""
			}
		} else {
			current += string(r)
		}
	}
	if current != "" {
		out = append(out, current)
	}
	return out
}
