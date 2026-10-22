// Package citychrome — realworld.go: City-Human 工具 realworld_info 的紧凑
// JSON 出口(契约 §3.7)。纯只读,零 LLM 成本。
package citychrome

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// realworldQuoteCap 是 realworld_info op=quotes 时每类目最多返回条数(契约 §3.7)。
const realworldQuoteCap = 5

// realworldNewsCap 是 realworld_info op=news 时最多返回条数(契约 §3.7)。
const realworldNewsCap = 10

// realworldHeadlineCap 是 op=overview 的头条条数(契约 §3.7)。
const realworldHeadlineCap = 3

// FeedCompactJSON 生成 realworld_info 的紧凑 JSON(契约 §3.7):
//   - op=overview:channel 状态 + 各类目最近更新时间 + 头条 3 条;
//   - op=quotes:全部类目(或指定 category)各前 5 条报价;
//   - op=news:最近 10 条(或指定 category)。
//
// category 非空时必须是 8 类目之一(批次76 起)。
// keyword(批次77 §3.5)**仅 op=news 生效**:标题大小写不敏感包含过滤;
// 空串 = 不过滤;其余 op 忽略 keyword。返回人读 JSON 文本。
func FeedCompactJSON(f Feed, op, category, keyword string) (string, error) {
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
		return compactQuotes(f, category)
	case "news":
		return compactNews(f, category, keyword)
	default:
		return "", fmt.Errorf("未知 op %q(须 overview|quotes|news)", op)
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

func compactOverview(f Feed, category string) (string, error) {
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
		cats[k] = map[string]any{
			"updated_at":  c.UpdatedAt,
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

func compactQuotes(f Feed, category string) (string, error) {
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
