// Package citychrome — feed_store.go: FeedStore(线程安全)。
//
// 契约 §3.2/§3.6:报价替换式合并入 categories;History 入环 ≤48 点;
// 新闻去重 md5(title+url) 前 12 位、超 news_limit 裁旧(新→旧);
// Snapshot() 返回 Feed 深拷贝。
package citychrome

import (
	"crypto/md5"
	"encoding/hex"
	"sort"
	"sync"
)

// FeedStore 是 server 级全局行情/新闻存储(与房间无关,契约 §2 关键决策 1)。
type FeedStore struct {
	mu   sync.RWMutex
	feed Feed
	// newsSeen 是新闻去重集(id → 存在)。feed.News ≤ news_limit,集合同寿。
	newsSeen map[string]struct{}

	symbolsPerCategory int
	newsLimit          int
}

// NewFeedStore 构造空存储。symbolsPerCategory/newsLimit ≤0 时取 12/20 缺省。
func NewFeedStore(symbolsPerCategory, newsLimit int) *FeedStore {
	if symbolsPerCategory <= 0 {
		symbolsPerCategory = 12
	}
	if newsLimit <= 0 {
		newsLimit = 20
	}
	return &FeedStore{
		feed: Feed{
			Categories: map[string]RealtimeCategory{},
			News:       []RealtimeNews{},
		},
		newsSeen:           map[string]struct{}{},
		symbolsPerCategory: symbolsPerCategory,
		newsLimit:          newsLimit,
	}
}

// newsID 计算新闻去重 id:md5(title+url) 前 12 位(契约 §3.2)。
func newsID(title, url string) string {
	sum := md5.Sum([]byte(title + url))
	return hex.EncodeToString(sum[:])[:12]
}

// Snapshot 返回 Feed 深拷贝(并发安全;HTTP 快照 / WS 广播共用)。
func (s *FeedStore) Snapshot() Feed {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneFeed(s.feed)
}

// Channel 返回当前通道状态(并发安全)。
func (s *FeedStore) Channel() ChannelStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.feed.Channel
}

// SetChannel 更新通道状态。status 仅接受 online/offline;online 时刷新
// LastOKAt 并清 Detail,offline 时 Detail 记原因(契约 §3.6 熔断语义)。
func (s *FeedStore) SetChannel(status, detail string, now int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if status != ChannelOnline && status != ChannelOffline {
		status = ChannelOffline
	}
	s.feed.Channel.Status = status
	s.feed.Channel.Detail = detail
	if status == ChannelOnline {
		s.feed.Channel.LastOKAt = now
	}
	if now > 0 {
		s.feed.UpdatedAt = now
	}
}

// MergeMissionResult 把一次 mission 结果合并入存储(契约 §3.4/§3.6):
//   - quotes 替换式写入 categories[category](脏条丢弃,超上限截断,
//     updated_at/source 以本次为准);每条报价向 History[symbol] 入环 ≤48 点;
//   - news 按 md5(title+url) 前 12 位去重,超 news_limit 裁旧(保留最新)。
//
// payload.Category 必须是 6 个 mission 类目之一(调用方/校验层保证);
// 返回合并统计(单条脏数据丢弃不整包失败)。
func (s *FeedStore) MergeMissionResult(now int64, p *MissionPayload) MergeStats {
	stats := MergeStats{}
	if p == nil {
		return stats
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	// ── 报价:替换式合并 + 入环 ──
	cat := RealtimeCategory{
		UpdatedAt: now,
		Source:    p.Source,
		Quotes:    make([]RealtimeQuote, 0, s.symbolsPerCategory),
		History:   map[string][]RealtimePoint{},
	}
	// History 从旧类目继承(报价是替换的,自采价环是累积的)。
	if old, ok := s.feed.Categories[p.Category]; ok {
		cat.History = cloneHistory(old.History)
	}
	for _, q := range p.Quotes {
		if !validQuote(q) {
			stats.QuotesDropped++
			continue
		}
		if len(cat.Quotes) >= s.symbolsPerCategory {
			stats.QuotesTruncated++
			continue
		}
		if q.UpdatedAt <= 0 {
			q.UpdatedAt = now
		}
		if q.Source == "" {
			q.Source = p.Source
		}
		cat.Quotes = append(cat.Quotes, q)
		stats.QuotesKept++
		// 入环:追加价点,超 48 裁旧。
		ring := append(cat.History[q.Symbol], RealtimePoint{T: now, P: q.Price})
		if len(ring) > HistoryCap {
			ring = ring[len(ring)-HistoryCap:]
		}
		cat.History[q.Symbol] = ring
	}
	// 批次76:纯新闻 mission(news 类目)quotes 为空 —— 不写入 Categories,
	// 新闻统一走 Feed.News(契约 76 §2.1)。同理任一行情 mission 若全部
	// 脏数据被丢弃,保留旧类目数据而非用空类目覆盖。
	if len(cat.Quotes) > 0 {
		if s.feed.Categories == nil {
			s.feed.Categories = map[string]RealtimeCategory{}
		}
		s.feed.Categories[p.Category] = cat
	}

	// ── 新闻:去重 + 裁旧 ──
	added := make([]RealtimeNews, 0, len(p.News))
	for _, n := range p.News {
		if n.Title == "" {
			stats.NewsDupSkipped++ // 无标题视为脏数据,不入库。
			continue
		}
		id := newsID(n.Title, n.URL)
		if _, dup := s.newsSeen[id]; dup {
			stats.NewsDupSkipped++
			continue
		}
		if n.ID == "" {
			n.ID = id
		}
		if n.Category == "" {
			n.Category = p.Category
		}
		if n.Source == "" {
			n.Source = p.Source // 批次76:Agent 常省略逐条来源,回填 mission 站点名。
		}
		if n.PublishedAt <= 0 {
			n.PublishedAt = now
		}
		s.newsSeen[id] = struct{}{}
		added = append(added, n)
		stats.NewsAdded++
	}
	if len(added) > 0 {
		s.feed.News = append(added, s.feed.News...)
	}
	// 新→旧排序后裁到 newsLimit(集合同寿收缩)。
	sort.SliceStable(s.feed.News, func(i, j int) bool {
		return s.feed.News[i].PublishedAt > s.feed.News[j].PublishedAt
	})
	if len(s.feed.News) > s.newsLimit {
		for _, dropped := range s.feed.News[s.newsLimit:] {
			delete(s.newsSeen, dropped.ID)
		}
		s.feed.News = s.feed.News[:s.newsLimit]
	}

	if now > 0 {
		s.feed.UpdatedAt = now
	}
	return stats
}

// validQuote 单条报价数值校验(契约 §3.4:price/change_pct 数值合法)。
// price 必须 >0 且有限;change_pct 必须在 [-100,+100] 且有限;symbol/name 非空。
func validQuote(q RealtimeQuote) bool {
	if q.Symbol == "" || q.Name == "" {
		return false
	}
	if q.Price <= 0 || isNaNOrInf(q.Price) {
		return false
	}
	if q.ChangePct < -100 || q.ChangePct > 100 || isNaNOrInf(q.ChangePct) {
		return false
	}
	return true
}

// cloneFeed 深拷贝 Feed。
func cloneFeed(f Feed) Feed {
	out := Feed{
		Channel:    f.Channel,
		UpdatedAt:  f.UpdatedAt,
		Categories: make(map[string]RealtimeCategory, len(f.Categories)),
		News:       make([]RealtimeNews, len(f.News)),
	}
	for k, v := range f.Categories {
		out.Categories[k] = RealtimeCategory{
			UpdatedAt: v.UpdatedAt,
			Source:    v.Source,
			Quotes:    append([]RealtimeQuote(nil), v.Quotes...),
			History:   cloneHistory(v.History),
		}
	}
	copy(out.News, f.News)
	return out
}

// cloneHistory 深拷贝 history 环。
func cloneHistory(h map[string][]RealtimePoint) map[string][]RealtimePoint {
	out := make(map[string][]RealtimePoint, len(h))
	for k, v := range h {
		out[k] = append([]RealtimePoint(nil), v...)
	}
	return out
}
