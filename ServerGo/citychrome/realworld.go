// Package citychrome — realworld.go: City-Human 工具 realworld_info 的紧凑
// JSON 出口(契约 §3.7;批次78 §3.5 扩为 5 个 op)。纯只读,零 LLM 成本。
//
// op 语义(批次78 §3.5):
//   - overview:通道状态 + 各类目最近更新 + **age_seconds/stale** + 头条 3 条;
//   - quotes:各类目前 5 条报价(可选 symbol 精确过滤,大小写不敏感等值);
//   - news:最近 10 条(或指定 category/keyword);
//   - history(新):某 symbol 的最近 ≤48 个自采价点 {t,p} + 所属类目 + 最新价;
//   - sources(新):采集源清单(citychrome 包内 categorySites)+ 类目健康度
//     (ok/fail/last_source/last_ok_at/age_seconds),数据源取自 MissionLog 遥测。
//
// import 方向:vcplayer → citychrome;本包**不 import game/virtual_city**
// (该包 import vcplayer,会成环)。
package citychrome

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// realworldQuoteCap 是 realworld_info op=quotes 时每类目最多返回条数(契约 §3.7)。
const realworldQuoteCap = 5

// realworldNewsCap 是 realworld_info op=news 时最多返回条数(契约 §3.7)。
const realworldNewsCap = 10

// realworldHeadlineCap 是 op=overview 的头条条数(契约 §3.7)。
const realworldHeadlineCap = 3

// realworldHistoryCap 是 op=history 返回的自采价点上限(与 HistoryCap 一致:≤48)。
const realworldHistoryCap = HistoryCap

// FeedCompactJSON 生成 realworld_info 的紧凑 JSON(契约 §3.7;批次78 §3.5 扩 op)。
//   - op=overview:channel 状态 + 各类目最近更新时间(含 age_seconds/stale)+ 头条 3 条;
//   - op=quotes:全部类目(或指定 category)各前 5 条报价(可选 symbol 过滤);
//   - op=news:最近 10 条(或指定 category);
//   - op=history:指定 symbol 的自采价点(≤48 个)+ 所属类目 + 最新价;
//   - op=sources:见 SourcesCompactJSON(需要遥测日志,不在本函数内)。
//
// category 非空时必须是 8 类目之一(批次76 起)。
// keyword(批次77 §3.5)**仅 op=news 生效**:标题大小写不敏感包含过滤;
// 空串 = 不过滤;其余 op 忽略 keyword。
// symbol(批次78 §3.5)仅 op=quotes/op=history 生效;op=history 缺省时返回错误。
// 返回人读 JSON 文本。
func FeedCompactJSON(f Feed, op, category, keyword, symbol string) (string, error) {
	if op == "" {
		op = "overview"
	}
	if category != "" && !IsMissionCategory(category) {
		return "", fmt.Errorf("未知 category %q(须 a_share|hk|us|metal|futures|crypto|macro|news)", category)
	}
	switch op {
	case "overview":
		return compactOverview(f, category)
	case "quotes":
		return compactQuotes(f, category, symbol)
	case "news":
		return compactNews(f, category, keyword)
	case "history":
		return compactHistory(f, category, symbol)
	case "sources":
		// 采集源清单 + 类目健康度需要遥测日志(不在 Feed 内)。
		return SourcesCompactJSON(nil, category)
	default:
		return "", fmt.Errorf("未知 op %q(须 overview|quotes|news|history|sources)", op)
	}
}

type compactQuote struct {
	Category  string  `json:"category"`
	Symbol    string  `json:"symbol"`
	Name      string  `json:"name"`
	Price     float64 `json:"price"`
	ChangePct float64 `json:"change_pct"`
}

type compactNewsItem struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Source      string `json:"source"`
	Category    string `json:"category"`
	PublishedAt int64  `json:"published_at"`
}

// compactPoint 是 op=history 的单个自采价点(字段名 t/p 逐字对齐)。
type compactPoint struct {
	T int64   `json:"t"`
	P float64 `json:"p"`
}

func compactOverview(f Feed, category string) (string, error) {
	now := time.Now().Unix()
	cats := map[string]map[string]any{}
	keys := make([]string, 0, len(f.Categories))
	for k := range f.Categories {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if category != "" && k != category {
			continue
		}
		c := f.Categories[k]
		top := make([]compactQuote, 0, realworldHeadlineCap)
		for i, q := range c.Quotes {
			if i >= realworldHeadlineCap {
				break
			}
			top = append(top, compactQuote{Category: k, Symbol: q.Symbol, Name: q.Name,
				Price: q.Price, ChangePct: q.ChangePct})
		}
		age := int64(0)
		if c.UpdatedAt > 0 && now > c.UpdatedAt {
			age = now - c.UpdatedAt
		}
		interval := categoryRefreshInterval(k)
		cats[k] = map[string]any{
			"updated_at":  c.UpdatedAt,
			"age_seconds": age,
			"stale":       age > 2*interval,
			"source":      c.Source,
			"quote_count": len(c.Quotes),
			"top":         top,
		}
	}
	headlines := make([]compactNewsItem, 0, realworldHeadlineCap)
	for i, n := range f.News {
		if i >= realworldHeadlineCap {
			break
		}
		if category != "" && n.Category != category {
			continue
		}
		headlines = append(headlines, compactNewsItem{ID: n.ID, Title: n.Title, Source: n.Source,
			Category: n.Category, PublishedAt: n.PublishedAt})
	}
	out := map[string]any{
		"channel":    f.Channel,
		"updated_at": f.UpdatedAt,
		"categories": cats,
		"headlines":  headlines,
	}
	raw, err := json.Marshal(out)
	return string(raw), err
}

func compactQuotes(f Feed, category, symbol string) (string, error) {
	quotes := make([]compactQuote, 0, realworldQuoteCap*len(MissionCategories))
	keys := make([]string, 0, len(f.Categories))
	for k := range f.Categories {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if category != "" && k != category {
			continue
		}
		c := f.Categories[k]
		for i, q := range c.Quotes {
			if i >= realworldQuoteCap {
				break
			}
			if symbol != "" && !strings.EqualFold(q.Symbol, symbol) {
				continue // symbol 精确过滤(大小写不敏感等值;批次78 §3.5)。
			}
			quotes = append(quotes, compactQuote{Category: k, Symbol: q.Symbol, Name: q.Name,
				Price: q.Price, ChangePct: q.ChangePct})
		}
	}
	out := map[string]any{"channel": f.Channel, "quotes": quotes}
	raw, err := json.Marshal(out)
	return string(raw), err
}

// compactNews 组装 op=news 的紧凑 JSON。keyword 非空时按标题大小写不敏感
// 包含过滤(批次77 §3.5;城市居民 Agent 可按主题查新闻)。
func compactNews(f Feed, category, keyword string) (string, error) {
	kw := ""
	if keyword != "" {
		kw = strings.ToLower(keyword)
	}
	news := make([]compactNewsItem, 0, realworldNewsCap)
	for _, n := range f.News {
		if category != "" && n.Category != category {
			continue
		}
		if kw != "" && !strings.Contains(strings.ToLower(n.Title), kw) {
			continue
		}
		news = append(news, compactNewsItem{ID: n.ID, Title: n.Title, Source: n.Source,
			Category: n.Category, PublishedAt: n.PublishedAt})
		if len(news) >= realworldNewsCap {
			break
		}
	}
	out := map[string]any{"channel": f.Channel, "news": news}
	raw, err := json.Marshal(out)
	return string(raw), err
}

// compactHistory 组装 op=history 的紧凑 JSON(批次78 §3.5)。
// symbol 必填(缺省 → 错误文本,不 panic);未命中返回 points: []。
// 返回:{channel,symbol,category,price,points[],point_count,first_at,last_at,span_seconds}。
func compactHistory(f Feed, category, symbol string) (string, error) {
	sym := strings.TrimSpace(symbol)
	if sym == "" {
		return "", fmt.Errorf("op=history 需要 symbol 参数(该 symbol 的行情代码,如 000001)")
	}
	out := map[string]any{
		"channel":      f.Channel,
		"symbol":       sym,
		"category":     "",
		"price":        0,
		"points":       []compactPoint{},
		"point_count":  0,
		"first_at":     int64(0),
		"last_at":      int64(0),
		"span_seconds": int64(0),
	}
	keys := make([]string, 0, len(f.Categories))
	for k := range f.Categories {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if category != "" && k != category {
			continue
		}
		c := f.Categories[k]
		ring, ok := lookupHistory(c.History, sym)
		if !ok || len(ring) == 0 {
			continue
		}
		if len(ring) > realworldHistoryCap {
			ring = ring[len(ring)-realworldHistoryCap:]
		}
		points := make([]compactPoint, 0, len(ring))
		for _, p := range ring {
			points = append(points, compactPoint{T: p.T, P: p.P})
		}
		price := points[len(points)-1].P
		// 最新价兜底:价环缺最后一价时用当前报价。
		for _, q := range c.Quotes {
			if strings.EqualFold(q.Symbol, sym) {
				price = q.Price
				break
			}
		}
		out["category"] = k
		out["price"] = price
		out["points"] = points
		out["point_count"] = len(points)
		out["first_at"] = points[0].T
		out["last_at"] = points[len(points)-1].T
		out["span_seconds"] = points[len(points)-1].T - points[0].T
		break
	}
	raw, err := json.Marshal(out)
	return string(raw), err
}

// lookupHistory 在价环表里查 symbol(先精确,再大小写不敏感)。
func lookupHistory(h map[string][]RealtimePoint, symbol string) ([]RealtimePoint, bool) {
	if ring, ok := h[symbol]; ok {
		return ring, true
	}
	for k, ring := range h {
		if strings.EqualFold(k, symbol) {
			return ring, true
		}
	}
	return nil, false
}

// sourceSite 是一个采集源(名称 + URL)。
type sourceSite struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// sourceCategory 是一个类目的采集源清单 + 健康度(批次78 §3.5)。
type sourceCategory struct {
	Category   string       `json:"category"`
	Sites      []sourceSite `json:"sites"`
	LastSource string       `json:"last_source"`
	LastOKAt   int64        `json:"last_ok_at"`
	AgeSeconds int64        `json:"age_seconds"`
	OK         int          `json:"ok"`
	Fail       int          `json:"fail"`
}

// SourcesCompactJSON 组装 op=sources 的紧凑 JSON(批次78 §3.5):
// 采集源清单取 citychrome 包内 categorySites(窄:可采集源),与看板 45 条
// 宽登记册互补;last_source/last_ok_at/ok/fail 从 MissionLog 遥测聚合。
//
// mlog 为 nil 时只返回清单(健康度全零);category 非空时只返回该类目。
func SourcesCompactJSON(mlog *MissionLog, category string) (string, error) {
	if category != "" && !IsMissionCategory(category) {
		return "", fmt.Errorf("未知 category %q(须 a_share|hk|us|metal|futures|crypto|macro|news)", category)
	}
	now := time.Now().Unix()
	keys := make([]string, 0, len(categorySites))
	for k := range categorySites {
		if category != "" && k != category {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	cats := make([]sourceCategory, 0, len(keys))
	for _, k := range keys {
		sites := make([]sourceSite, 0, len(categorySites[k]))
		for _, s := range categorySites[k] {
			sites = append(sites, sourceSite{Name: s.Name, URL: s.URL})
		}
		sc := sourceCategory{Category: k, Sites: sites}
		// 遥测聚合(记录按新→旧:首个 ok 即最近一次成功)。
		for _, r := range mlog.Snapshot() {
			if r.Category != k {
				continue
			}
			switch r.Status {
			case MissionStatusOK:
				sc.OK++
				if sc.LastSource == "" {
					sc.LastSource = r.Source
					sc.LastOKAt = r.StartedAt + r.DurationMs/1000
				}
			case MissionStatusFail:
				sc.Fail++
			}
		}
		if sc.LastOKAt > 0 && now > sc.LastOKAt {
			sc.AgeSeconds = now - sc.LastOKAt
		}
		cats = append(cats, sc)
	}
	out := map[string]any{"categories": cats}
	raw, err := json.Marshal(out)
	return string(raw), err
}
