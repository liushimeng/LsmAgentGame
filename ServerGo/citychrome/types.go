// Package citychrome — 批次65(2026-10-20 §批次65):City-Chrome 实时数据通道。
//
// 三层架构的 L2 层:经 HTTP 调 go-web-debug-tool 子模块(内存 Chrome/CDP)
// 让 LsmAgentGame-City-Chrome Agent 浏览真实金融站点,采集行情/新闻入
// FeedStore,再经 WS game.realtime_feed / HTTP 快照 / City-Human 工具
// realworld_info 三路出口供给前端看板与城市居民 Agent。
//
// 契约唯一事实来源: lag_docs/虚拟城市/已实现/65-CityChrome实时数据通道与实时看板/
//
// 关键设计:
//   - 数据是全局的(与房间无关):FeedStore 为 server 级单例;
//   - ServerGo 不直接依赖 chromedp,只走 HTTP;底座不可达 ⇒ channel=offline,
//     绝不阻塞对局;
//   - 广播出口为 listener 注入(OnFeed),本包不得 import ws(防反向依赖)。
package citychrome

// 类目常量(契约 §3.2)。前 6 个为轮转 mission 类目;general 仅用于综合新闻。
const (
	CategoryAShare  = "a_share"
	CategoryHK      = "hk"
	CategoryUS      = "us"
	CategoryMetal   = "metal"
	CategoryFutures = "futures"
	CategoryCrypto  = "crypto"
	CategoryGeneral = "general"
)

// MissionCategories 是调度器轮转的 6 个类目(契约 §3.6:a_share→hk→us→
// metal→futures→crypto→回卷)。
var MissionCategories = []string{
	CategoryAShare, CategoryHK, CategoryUS, CategoryMetal, CategoryFutures, CategoryCrypto,
}

// IsMissionCategory 报告 s 是否是 6 个轮转类目之一(web_done payload / HTTP
// refresh 的 category 只允许这 6 个;general 不是有效 mission 类目)。
func IsMissionCategory(s string) bool {
	for _, c := range MissionCategories {
		if c == s {
			return true
		}
	}
	return false
}

// RealtimeQuote 单条实时报价(契约 §3.2,字段名逐字对齐)。
type RealtimeQuote struct {
	Symbol    string  `json:"symbol"`               // 站点口径代码或名称,如 "000001"/"上证指数"/"BTC"
	Name      string  `json:"name"`                 // 显示名
	Price     float64 `json:"price"`                // 最新价
	ChangePct float64 `json:"change_pct"`           // 百分比,-100~+100
	Currency  string  `json:"currency,omitempty"`   // 币种(可选)
	UpdatedAt int64   `json:"updated_at"`           // Unix 秒
	Source    string  `json:"source"`               // 采集来源站点名
}

// RealtimePoint 自采价点(服务端入环;契约 §3.2)。
type RealtimePoint struct {
	T int64   `json:"t"` // Unix 秒
	P float64 `json:"p"` // 价
}

// RealtimeCategory 单类目行情(契约 §3.2)。
type RealtimeCategory struct {
	UpdatedAt int64                      `json:"updated_at"` // Unix 秒
	Source    string                     `json:"source"`     // 采集来源站点名
	Quotes    []RealtimeQuote            `json:"quotes"`     // ≤ symbols_per_category
	History   map[string][]RealtimePoint `json:"history"`    // symbol → 最近 ≤48 点
}

// RealtimeNews 单条新闻(契约 §3.2)。
type RealtimeNews struct {
	ID          string `json:"id"`           // md5(title+url) 前 12 位
	Title       string `json:"title"`        // 标题
	Source      string `json:"source"`       // 来源站点/栏目
	Category    string `json:"category"`     // 见 Category 常量;综合新闻用 "general"
	PublishedAt int64  `json:"published_at"` // Unix 秒
	URL         string `json:"url"`          // 原文链接
}

// ChannelStatus 数据通道状态(契约 §3.2):online | offline。
type ChannelStatus struct {
	Status   string `json:"status"`     // "online" | "offline"
	Detail   string `json:"detail"`     // 离线原因/最近错误
	LastOKAt int64  `json:"last_ok_at"` // Unix 秒
}

// Feed 是 WS 帧(game.realtime_feed)与 HTTP 快照共用的载荷(契约 §3.2)。
type Feed struct {
	Channel    ChannelStatus               `json:"channel"`
	UpdatedAt  int64                       `json:"updated_at"` // Unix 秒(最近合并时间)
	Categories map[string]RealtimeCategory `json:"categories"` // key: 6 类目
	News       []RealtimeNews              `json:"news"`       // ≤ news_limit,新→旧
}

// Channel status 取值。
const (
	ChannelOnline  = "online"
	ChannelOffline = "offline"
)

// HistoryCap 是自采价点环上限(契约 §3.2:每 symbol 最近 ≤48 点)。
const HistoryCap = 48

// SiteInfo 一个备选采集源(名称 + URL)。
type SiteInfo struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// Mission 是一次采集任务(契约 §3.4)。
type Mission struct {
	Category  string     `json:"category"`   // 6 类目之一
	TargetURL string     `json:"target_url"` // 首选站点 URL(Sites[0])
	Sites     []SiteInfo `json:"sites"`      // 备选源(3-5 个)
	Goal      string     `json:"goal"`       // 采集目标描述
	Hints     string     `json:"hints"`      // 确定性提取配方(E2E 实测验证,见 sites.go)
}

// MissionPayload 是 web_done 的结构化输出(契约 §3.4 schema)。
// 服务端校验:quotes ≤ symbols_per_category、price/change_pct 数值合法、
// 未知 category 拒绝;单条脏数据丢弃不整包失败。
type MissionPayload struct {
	Category string          `json:"category"`
	Source   string          `json:"source"`
	Quotes   []RealtimeQuote `json:"quotes"`
	News     []RealtimeNews  `json:"news"`
}

// MergeStats 是一次 MergeMissionResult 的合并统计(测试/日志用)。
type MergeStats struct {
	QuotesKept      int `json:"quotes_kept"`       // 通过校验并入的报价条数
	QuotesDropped   int `json:"quotes_dropped"`    // 脏数据丢弃条数
	QuotesTruncated int `json:"quotes_truncated"`  // 超 symbols_per_category 截断条数
	NewsAdded       int `json:"news_added"`        // 新增新闻条数
	NewsDupSkipped  int `json:"news_dup_skipped"`  // 去重跳过条数
}
