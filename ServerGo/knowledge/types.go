// Package knowledge — types.go: LsmKLBaseServer 知识库引擎类型定义
// (2026-10-05 §LsmKLBaseServer 知识库引擎重构)。
//
// 三层架构:
//   ① 特征抽取层 (feature_extract.go) — LLM 抽取 Markdown 人物卡的特征字符串数组
//   ② MySQL MCP 服务层 (mcp_server.go) — 将特征数据通过 MCP 协议暴露
//   ③ Graph + LangChain 检索层 (graph.go / langchain.go) — 知识图谱 + 检索链
//
// 与现有系统的关系:
//   - profession.Loader 是运行时文档池加载器(Markdown → frontmatter → Card);
//   - 本包是 LLM 特征抽取的持久化与检索层(Card → 特征字符串数组 → MySQL → MCP/Graph);
//   - 抽取失败时回退到 profession.Loader 的 frontmatter 解析(零回归)。
package knowledge

import "encoding/json"

// ─────────────────── 特征抽取状态 ───────────────────

// ExtractStatus 特征抽取状态码。
const (
	ExtractStatusPending  = 0 // 未抽取
	ExtractStatusDone     = 1 // 已抽取
	ExtractStatusFailed   = 2 // 抽取失败(回退 frontmatter 解析)
)

// ─────────────────── 知识卡结构 ───────────────────

// KnowledgeCard 是知识库中一张人物卡的完整视图(REST/MCP 返回用)。
// 数值字段与 profession.Card 对齐,额外携带 LLM 抽取的特征字符串数组。
type KnowledgeCard struct {
	ID              string   `json:"id"`               // 卡号(N9012345)
	SourcePath      string   `json:"source_path"`      // 来源 Markdown 相对路径
	Domain          string   `json:"domain"`           // L1 行业域
	Title           string   `json:"title"`            // 职业名
	Name            string   `json:"name"`             // 化名
	Salary          int64    `json:"salary"`           // 月薪(元)
	Expense         int64    `json:"expense"`          // 月支出(元)
	Savings         int64    `json:"savings"`          // 初始储蓄(元)
	StartAge        int      `json:"start_age"`        // 开局年龄
	Energy          int      `json:"energy"`           // 初始精力 0-10
	Network         int      `json:"network"`          // 初始人脉 0-10
	Cognition       int      `json:"cognition"`        // 初始认知 0-10
	CreditScore     int      `json:"credit_score"`     // 初始信用分
	HomeDistrict    string   `json:"home_district"`    // 工作区/初始所在区
	RiskPreference  string   `json:"risk_preference"`  // conservative|balanced|aggressive
	Personality     []string `json:"personality"`      // 人格标签
	BehaviorTraits  []string `json:"behavior_traits"`  // 行为特征
	HealthGrade     string   `json:"health_grade"`     // A|B|C
	Gender          string   `json:"gender"`           // m|f|u
	Employment      string   `json:"employment"`       // 就业形态
	Marital         string   `json:"marital"`          // single|married
	ChildrenCount   int      `json:"children_count"`   // 子女数
	EldersDependent int      `json:"elders_dependent"` // 赡养老人数
	OpeningHook     string   `json:"opening_hook"`     // 开场白
	Goals           []string `json:"goals"`            // 目标
	Features         []string `json:"features"`         // LLM 抽取的特征字符串数组
	ExtractStatus   int      `json:"extract_status"`   // 抽取状态
}

// ─────────────────── 特征抽取请求/响应 ───────────────────

// ExtractRequest 是一次特征抽取请求(单卡或批量)。
type ExtractRequest struct {
	CardID     string `json:"card_id"`     // 卡号
	SourcePath string `json:"source_path"` // 来源路径
	Content    string `json:"content"`     // Markdown 全文
}

// ExtractResult 是单卡抽取结果。
type ExtractResult struct {
	CardID   string   `json:"card_id"`
	Features []string `json:"features"` // 特征字符串数组
	Error    string   `json:"error,omitempty"`
}

// ─────────────────── MCP 协议类型 ───────────────────

// MCPRequest 是 MCP JSON-RPC 2.0 请求。
type MCPRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// MCPResponse 是 MCP JSON-RPC 2.0 响应。
type MCPResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int64       `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *MCPError   `json:"error,omitempty"`
}

// MCPError 是 MCP 错误对象。
type MCPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// MCPToolCall 是 MCP tools/call 请求参数。
type MCPToolCall struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

// MCPToolDescriptor 是 MCP 工具描述。
type MCPToolDescriptor struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

// ─────────────────── 检索查询类型 ───────────────────

// SearchQuery 是特征搜索查询。
type SearchQuery struct {
	Features []string `json:"features"`  // 特征字符串过滤(AND 语义)
	Domain   string   `json:"domain"`    // 行业域过滤
	District string   `json:"district"`  // 城区过滤
	Offset   int      `json:"offset"`
	Limit    int      `json:"limit"`
}

// SearchResult 是搜索结果。
type SearchResult struct {
	Cards   []KnowledgeCard `json:"cards"`
	Matched int             `json:"matched"`
	Total   int             `json:"total"`
}

// KBStats 是知识库统计。
type KBStats struct {
	Total     int            `json:"total"`     // 总卡数
	Extracted int            `json:"extracted"` // 已抽取数
	Failed    int            `json:"failed"`    // 抽取失败数
	Domains   map[string]int `json:"domains"`   // 各域卡数
	Districts map[string]int `json:"districts"` // 各城区卡数
}

// ─────────────────── 图检索类型 ───────────────────

// GraphNode 是知识图谱节点。
type GraphNode struct {
	CardID string
	Domain string
	Edges  []GraphEdge
}

// GraphEdge 是知识图谱边。
type GraphEdge struct {
	To     string // 目标节点 CardID
	Relation string // 关系类型: same_industry / same_district / same_personality / ...
	Weight  float64
}

// GraphRetrievalResult 是图检索结果。
type GraphRetrievalResult struct {
	SeedCards   []KnowledgeCard `json:"seed_cards"`   // 种子卡(直接匹配)
	RelatedCards []KnowledgeCard `json:"related_cards"` // 关联卡(图扩展)
	Depth       int             `json:"depth"`        // 遍历深度
}
