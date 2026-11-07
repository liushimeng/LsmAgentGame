// Package citychrome — anomaly.go: 批次89 异常判定与置信度派生(纯函数)。
//
// 契约出处:lag_docs/虚拟城市/已实现/89-真实数据口径卡与异常告警/01-方案设计.md
//   - §3.4 Anomaly 形状(逐字对齐);
//   - §4.2 异常判定(4 类 kind:high / low / stale / jump;优先级
//     jump > stale > high/low;一条报价最多一个标记);
//   - §4.3 置信度公式(新鲜度 × 权威度 × 惩罚系数,保留 3 位小数);
//   - §4.4 DeriveQuoteQuality(返回新副本,派生量**不进存储层**)。
//
// 纪律(对齐 market_hours.go 文件头):
//   - **纯函数**:零 IO、零 RNG、零全局可变态;时刻一律作入参(now time.Time);
//   - **stale 口径唯一**:恒用 `2 × tiers.Seconds(cat, now)` —— 与批次88 的
//     realworld_info stale 判定、调度器真实轮转间隔**同一表达式**
//     (IntervalTiers.Seconds 是单一解析路径),禁止另立口径;
//   - **阈值唯一**:阈值/单位/权威度全在 metric_spec.go,本文件只调用;
//   - **不修改入参**:DeriveQuoteQuality 返回**新副本**,FeedStore 存储层
//     永远只存原始值。
package citychrome

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Anomaly 单条异常告警(设计文档 §3.4,字段名与 json tag 逐字对齐)。
//
// ⚠️ **Value 的口径按 kind 分**(以本文为准,设计文档 §3.4 将同步修订):
//   - high / low = 涨跌幅幅度(%);
//   - jump       = 跳变幅度(%);
//   - stale      = **age_seconds(秒)** —— 触发量本身(陈旧多久),
//     前端渲染 stale 时**不应**按 % 追加后缀(判据在 Detail 文本)。
type Anomaly struct {
	Category   string  `json:"category"`    // 行情类目
	Symbol     string  `json:"symbol"`      // 站点口径代码
	Name       string  `json:"name"`        // 显示名
	Kind       string  `json:"kind"`        // high|low|stale|jump
	Severity   int     `json:"severity"`    // 1..3(L1 轻微 / L2 中等 / L3 严重)
	Value      float64 `json:"value"`       // 触发值(见上:stale = age_seconds 秒)
	Detail     string  `json:"detail"`      // 触发依据(含阈值与实测值)
	ObservedAt int64   `json:"observed_at"` // Unix 秒
}

// 异常 kind 取值(设计文档 §3.4)。
const (
	AnomalyHigh  = "high"  // 涨幅超阈值
	AnomalyLow   = "low"   // 跌幅超阈值
	AnomalyStale = "stale" // 数据陈旧(超 2×轮转间隔)
	AnomalyJump  = "jump"  // 相邻两次采样跳变超 2×阈值
)

// 置信度惩罚系数(设计文档 §4.3)。
const (
	confidencePenaltyNone  = 1.0
	confidencePenaltyHigh  = 0.8
	confidencePenaltyStale = 0.2
	confidencePenaltyJump  = 0.3
)

// DeriveAnomalies 对 Feed 逐条报价做异常判定(纯函数)。
//
// 判定式(设计文档 §4.2):
//   - stale: age_seconds > 2 × tiers.Seconds(cat, now)(阈值口径与批次88 同源);
//   - high : change_pct  > 阈值(cat)(严格大于;恰好等于阈值**不触发**);
//   - low  : change_pct < -阈值(cat)(严格小于;恰好等于阈值**不触发**);
//   - jump : |price - prev| / prev × 100 > 2 × 阈值(cat),prev = 同 symbol
//     历史中**早于本次报价**的最后一点(历史点不足 2 或 prev ≤ 0 ⇒ 不判)。
//
// 优先级:jump > stale > high/low —— 一条报价最多产出一条告警(取最严重者)。
// 严重度:high/low = 1 + min(2, floor(|change_pct|/阈值 - 1))(1 轻微 / 2 中等 /
// 3 严重);stale 固定 2;jump 固定 3。
// 报价集合为空 ⇒ 返回空切片(非 nil)。
func DeriveAnomalies(feed Feed, now time.Time, tiers IntervalTiers) []Anomaly {
	out := make([]Anomaly, 0, 16)
	for _, cat := range sortedCategoryKeys(feed.Categories) {
		c := feed.Categories[cat]
		interval := tiers.Seconds(cat, now)
		staleLimit := 2 * interval
		threshold := AnomalyThresholdPct(cat)
		for _, q := range c.Quotes {
			age := quoteAgeSeconds(q, now)
			jump := jumpPct(c.History, q, threshold)
			kind, severity, value, detail := "", 0, 0.0, ""
			switch {
			case jump > 0:
				pct := jump
				kind = AnomalyJump
				severity = 3
				value = pct
				detail = fmt.Sprintf("价格跳变 %.2f%% > 跳变阈值 %.2f%%(2×%.2f%%,类目 %s)",
					pct, 2*threshold, threshold, cat)
			case age > staleLimit:
				kind = AnomalyStale
				severity = 2
				value = float64(age)
				detail = fmt.Sprintf("数据陈旧 %ds > 陈旧阈值 %ds(2×%ds 轮转间隔,类目 %s)",
					age, staleLimit, interval, cat)
			case q.ChangePct > threshold:
				kind = AnomalyHigh
				severity = magnitudeSeverity(q.ChangePct, threshold)
				value = q.ChangePct
				detail = fmt.Sprintf("涨幅 %.2f%% > 阈值 %.2f%%(类目 %s)", q.ChangePct, threshold, cat)
			case q.ChangePct < -threshold:
				kind = AnomalyLow
				severity = magnitudeSeverity(-q.ChangePct, threshold)
				value = q.ChangePct
				detail = fmt.Sprintf("跌幅 %.2f%% > 阈值 %.2f%%(类目 %s)", -q.ChangePct, threshold, cat)
			}
			if kind == "" {
				continue
			}
			out = append(out, Anomaly{
				Category:   cat,
				Symbol:     q.Symbol,
				Name:       q.Name,
				Kind:       kind,
				Severity:   severity,
				Value:      value,
				Detail:     detail,
				ObservedAt: q.UpdatedAt,
			})
		}
	}
	return out
}

// DeriveQuoteQuality 为 Feed 的每条报价补齐派生量(纯函数,返回**新副本**):
//   - unit      : 命中口径卡取 spec.Unit,否则取类目兜底单位;
//   - source_url: 命中口径卡取 spec.SourceURL,否则留空;
//   - confidence: 新鲜度 × 采集源权威度 × 异常惩罚(0..1,3 位小数);
//   - anomaly   : 该报价的最严重异常 kind(无异常为空串);
//   - spec_id   : 命中口径卡取 spec.ID,否则留空(批次89 收口增强:前端拿不到
//     MetricSpec.Keywords(json:"-"),只能靠 indicator 模糊匹配 ⇒ 站点口径名
//     与指标名不一致时前端「查无口径卡」而后端已命中;spec_id 提供**稳定唯一**
//     的精确关联键,前端应优先按它关联口径卡)。
//
// ⚠️ **不修改入参**(副本纪律);派生量**不进 FeedStore 存储层** —— 置信度
// 随时间衰减,冻结在采集时刻会失真,故一律在出口派生。
func DeriveQuoteQuality(feed Feed, now time.Time, tiers IntervalTiers) Feed {
	out := cloneFeed(feed)
	anomalies := DeriveAnomalies(feed, now, tiers)
	idx := make(map[string]Anomaly, len(anomalies))
	for _, a := range anomalies {
		idx[anomalyKey(a.Category, a.Symbol)] = a
	}
	for cat, c := range out.Categories {
		for i := range c.Quotes {
			q := &c.Quotes[i]
			spec, hit := LookupMetricSpec(cat, q.Symbol, q.Name)
			if hit {
				q.Unit = spec.Unit
				q.SourceURL = spec.SourceURL
				q.SpecID = spec.ID
			} else {
				// 未命中口径卡:单位走类目兜底;source_url / spec_id **留空**
				// —— 采集层(MissionPayload)不带逐条原文链接,故**不猜测、不编造**
				// 链接与口径 id(批次89 D5:诚实降级优于编造)。
				q.Unit = DefaultUnit(cat)
				q.SourceURL = ""
				q.SpecID = ""
			}
			kind := ""
			if a, ok := idx[anomalyKey(cat, q.Symbol)]; ok {
				kind = a.Kind
			}
			q.Anomaly = kind
			q.Confidence = quoteConfidence(c.Source, *q, cat, now, tiers, kind)
		}
		out.Categories[cat] = c
	}
	return out
}

// quoteConfidence 计算单条报价的置信度(设计文档 §4.3):
//
//	freshness = clamp01(1 - age_seconds / (2 × tiers.Seconds(cat, now)))
//	authority = sourceAuthority(source)(采集源权威度静态表)
//	penalty   = 1.0(无异常)| 0.8(high/low)| 0.2(stale)| 0.3(jump)
//	confidence = round3(clamp01(freshness × authority × penalty))
//
// age ≥ 2×档位 ⇒ freshness ≤ 0 ⇒ confidence == 0(与 stale 判定自洽)。
func quoteConfidence(categorySource string, q RealtimeQuote, cat string, now time.Time,
	tiers IntervalTiers, anomalyKind string) float64 {
	age := quoteAgeSeconds(q, now)
	limit := 2 * tiers.Seconds(cat, now)
	freshness := 1.0
	if limit > 0 {
		freshness = 1 - float64(age)/float64(limit)
	}
	freshness = clamp01(freshness)
	source := q.Source
	if source == "" {
		source = categorySource
	}
	authority := sourceAuthority(source)
	penalty := confidencePenaltyNone
	switch anomalyKind {
	case AnomalyHigh, AnomalyLow:
		penalty = confidencePenaltyHigh
	case AnomalyStale:
		penalty = confidencePenaltyStale
	case AnomalyJump:
		penalty = confidencePenaltyJump
	}
	return round3(clamp01(freshness * authority * penalty))
}

// jumpPct 计算相对上一次采样的跳变幅度(%);不满足判定条件时返回 0。
// prev = 同 symbol 历史中**早于本次报价时间戳**的最后一点;历史点不足 2
// 或 prev ≤ 0 ⇒ 不判跳变(返回 0)。
func jumpPct(history map[string][]RealtimePoint, q RealtimeQuote, threshold float64) float64 {
	if threshold <= 0 {
		return 0
	}
	ring, ok := lookupHistory(history, q.Symbol)
	if !ok || len(ring) < 2 {
		return 0
	}
	prev, ok := previousPrice(ring, q)
	if !ok || prev <= 0 {
		return 0
	}
	pct := math.Abs(q.Price-prev) / prev * 100
	if pct <= 2*threshold { // 严格大于才触发(恰好等于 2×阈值不触发)。
		return 0
	}
	return pct
}

// previousPrice 从价环里取「早于本次报价」的最后一点;同秒合并等边界情况
// 回退到倒数第二点(仍不足则不判)。
func previousPrice(ring []RealtimePoint, q RealtimeQuote) (float64, bool) {
	for i := len(ring) - 1; i >= 0; i-- {
		if ring[i].T < q.UpdatedAt {
			return ring[i].P, true
		}
	}
	if len(ring) >= 2 {
		return ring[len(ring)-2].P, true
	}
	return 0, false
}

// magnitudeSeverity 计算 high/low 的严重度(设计文档 §4.2;批次89 收口修订)。
//
//	severity = 1 + min(2, floor(|change_pct| / 阈值 - 1))
//
// 即按「**超阈值的倍数**」分档(而非按倍数本身分档),三档边界自洽:
//   - ratio ∈ (1, 2)  ⇒ floor(ratio-1)=0 ⇒ severity 1(轻微)
//   - ratio ∈ [2, 3) ⇒ floor(ratio-1)=1 ⇒ severity 2(中等)
//   - ratio ≥ 3      ⇒ floor(ratio-1)≥2 ⇒ severity 3(严重,封顶)
//
// ⚠️ ratio 恰为 1.0 时 high/low **不触发**(严格大于才触发),故 severity 1
// 一定可达(旧式 `1 + min(2, floor(ratio))` 会恒得 ≥2,severity 1 成死档)。
func magnitudeSeverity(magnitude, threshold float64) int {
	if threshold <= 0 {
		return 1
	}
	f := math.Floor(magnitude/threshold - 1)
	if f > 2 {
		f = 2
	}
	if f < 0 {
		f = 0
	}
	return 1 + int(f)
}

// quoteAgeSeconds 返回报价距 now 的秒数(无时间戳 ⇒ 0)。
func quoteAgeSeconds(q RealtimeQuote, now time.Time) int64 {
	if q.UpdatedAt <= 0 {
		return 0
	}
	nowUnix := now.Unix()
	if nowUnix <= q.UpdatedAt {
		return 0
	}
	return nowUnix - q.UpdatedAt
}

// anomalyKey 是异常索引键(类目 + symbol)。
//
// ⚠️ symbol **大小写不敏感**(统一 ToLower):采集源的站点口径代码大小写不稳
// (sh000001 vs SH000001),若索引与查找口径不一致,前端异常徽章与后端
// anomalies 清单会对不上(批次89 收口修订)。
func anomalyKey(category, symbol string) string {
	return category + "\x00" + strings.ToLower(symbol)
}

// sortedCategoryKeys 返回类目键的稳定升序切片(保证输出顺序可复现)。
func sortedCategoryKeys(m map[string]RealtimeCategory) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// clamp01 把 v 夹到 [0,1]。
func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// round3 四舍五入保留 3 位小数。
func round3(v float64) float64 {
	return math.Round(v*1000) / 1000
}
