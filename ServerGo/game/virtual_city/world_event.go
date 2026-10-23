// Package virtual_city — world_event.go: 批次80 真实世界事件规则引擎
// (RealWorld → WorldEvent → Scenario)。
//
// 契约出处:lag_docs/虚拟城市/已实现/80-真实世界事件自动注入引擎/01-方案设计.md
// §3.1(规则表 R1–R10)/§3.2(指纹与幂等)。
//
// 定位:批次65 采集真实行情/新闻 → 批次66 只做「外盘情绪锚」(±0.3%/月微偏置)
// → **本文件**把真实数据派生为「世界事件」(WorldEvent) → 房间层复用批次68
// 场景引擎(applyScenarioToWorld)自动注入城市。
//
// 纪律:
//   - **纯函数**:无 IO、无 RNG、无时钟依赖(now 作入参)、**不 import citychrome**
//     (引擎纯度;真实数据经 WorldEventInput 中性结构注入,Feed→中性结构的映射
//     在 main.go 装配层完成,与批次66 exogenous.go 同款依赖反转范式);
//   - **阈值写死为常量**:禁运行时配置漂移(强度上限由 config 安全阀兜底);
//   - **零 rand**:事件派生与注入不消费任何 RNG(存量对局零偏移,§92a/§197);
//   - **同输入同输出**:可全量单测、可复现(《规范》§一「可复现的参数」)。
package virtual_city

import (
	"fmt"
	"strconv"
	"strings"
)

// ── 批次80 常量(阈值写死;禁运行时配置漂移) ──

const (
	// worldEventFreshWindowSec 报价/新闻保鲜窗口:超过 72h 无更新 ⇒ 不参与判定
	// (与批次66 exoFreshWindowSec 同值同源)。
	worldEventFreshWindowSec int64 = 72 * 3600
	// worldEventDayBucketSec 自然日分桶秒数(指纹 dayBucket 用)。
	worldEventDayBucketSec int64 = 86400
	// worldEventLogCap 事件日志环容量(World.WorldEventLog;≤50)。
	worldEventLogCap = 50
	// worldEventBriefMax 居民 prompt「■ 世界事件」最多列出的已注入事件条数。
	worldEventBriefMax = 3
)

// 中性结构里用的类目字面量(与 citychrome.Category* 同值;引擎不 import
// citychrome,由装配层保证一致性)。
const (
	weCatAShare  = "a_share"
	weCatHK      = "hk"
	weCatUS      = "us"
	weCatMetal   = "metal"
	weCatFutures = "futures"
	weCatCrypto  = "crypto"
)

// 事件类型 kind(契约 §3.1 WorldEvent.Kind 枚举)。
const (
	WorldEventKindEquityCrash        = "equity_crash"
	WorldEventKindGoldSpike          = "gold_spike"
	WorldEventKindCryptoCrash        = "crypto_crash"
	WorldEventKindCommodityInflation = "commodity_inflation"
	WorldEventKindPolicyEasing       = "policy_easing"
	WorldEventKindPolicyTightening   = "policy_tightening"
	WorldEventKindEarthquake         = "earthquake"
	WorldEventKindTsunami            = "tsunami"
	WorldEventKindExtremeDisaster    = "extreme_disaster"
	WorldEventKindSurveyPulse        = "survey_pulse"
)

// 方向 / 作用域字面量(契约 §3.1)。
const (
	WorldEventDirBullish = "bullish"
	WorldEventDirBearish = "bearish"
	WorldEventDirNeutral = "neutral"

	WorldEventScopeMarket   = "market"
	WorldEventScopePolicy   = "policy"
	WorldEventScopeDisaster = "disaster"
	WorldEventScopeMacro    = "macro"
	WorldEventScopeSociety  = "society"
)

// WorldEventQuote 中性真实报价(装配层从 citychrome 映射;引擎不感知来源)。
// ChangePct 为百分数口径(0.82 = +0.82%),与批次65 契约一致。
type WorldEventQuote struct {
	Category  string  // "a_share"|"hk"|"us"|"metal"|"futures"|"crypto"
	Symbol    string  // 站点口径代码/名称
	Name      string  // 显示名
	ChangePct float64 // 百分数口径
	UpdatedAt int64   // Unix 秒
}

// WorldEventNews 中性新闻头(装配层从 citychrome 映射)。
type WorldEventNews struct {
	ID          string // md5(title+url) 前 12 位(幂等键的一部分)
	Title       string
	Source      string
	URL         string
	PublishedAt int64
}

// WorldEventInput 是引擎的唯一入参(中性、无 IO)。
type WorldEventInput struct {
	Quotes []WorldEventQuote
	News   []WorldEventNews
}

// WorldEvent 是派生出的世界事件(事件层产物)。
type WorldEvent struct {
	Key        string // 幂等指纹(见契约 §3.2)
	Rule       string // 规则 id:R1..R10
	Kind       string // 事件类型:equity_crash|gold_spike|...
	Severity   int    // 1..3(强度档;映射到场景 L1..L5 时受上限约束)
	Direction  string // bullish|bearish|neutral
	Scope      string // market|policy|disaster|macro|society
	Title      string // 中文标题(看板/居民可见)
	Detail     string // 一句话说明(触发依据,含关键数值)
	Magnitude  float64
	Source     string
	URL        string
	ObservedAt int64
	// 映射结果(引擎内一并给出,房间层只做执行)。
	ScenarioKind   string // 批次68 场景 kind
	ScenarioMag    int    // 注入强度 L1..L5
	ScenarioDur    int    // 持续月数(0 = 用场景默认)
	ScenarioTarget string // 如 policy_shock 的 "loose"/"tight";缺省 ""
}

// ─────────────────────────── 规则表 ───────────────────────────

// worldEventQuoteRule 行情类规则(契约 §3.1 规则表 R1–R4)。
//
// 统计口径(契约 §3.1 修订版):
//   - 「均值」= 统计集合内**有效报价** ChangePct 的算术平均;
//   - R1 的统计集合 = a_share ∪ hk ∪ us **全体报价**(报价等权,非类目等权);
//   - R2/R3/R4 的统计集合 = 对应类目内全部报价;
//   - 「极值」= 集合内 ChangePct 的最小/最大值;
//   - 集合为空 ⇒ 该规则不命中;过期报价(>72h)不参与判定。
type worldEventQuoteRule struct {
	rule     string // 规则 id
	kind     string // 事件 kind
	scenario string // 批次68 场景 kind
	cats     []string
	catKey   string // 指纹里的「类目集合」标识(排序后以 "+" 连接)
	title    string
	dir      string
	scope    string
	dur      int // 持续月数
	mag      int // 注入强度
	magHeavy int // 重度档强度(0 = 无重度档)
	sev      int
	sevHeavy int
	// 触发条件:均值口径(meanOp = "le"/"ge";meanThr 为 0 时不启用)。
	meanOp  string
	meanThr float64
	// 触发条件:极值口径(extremeOp = "min_le"/"max_ge";为空时不启用)。
	extremeOp  string
	extremeThr float64
	// 重度条件(契约 §3.1:R1 均值 ≤ -3.5% 或极值 ≤ -5.0% ⇒ L2/severity 3)。
	heavyMeanThr    float64
	heavyExtremeThr float64
	// 触发说明模板(占位符 {cats} {mean} {min} {max} {meanThr} {extThr})。
	detail string
}

// worldEventQuoteRules 行情类规则表(顺序 = 派生顺序 = 稳定输出顺序)。
var worldEventQuoteRules = []worldEventQuoteRule{
	{
		rule: "R1", kind: WorldEventKindEquityCrash, scenario: ScenarioMarketCrash,
		cats: []string{weCatAShare, weCatHK, weCatUS}, catKey: "a_share+hk+us",
		title: "股市剧震", dir: WorldEventDirBearish, scope: WorldEventScopeMarket,
		dur: 4, mag: 1, magHeavy: 2, sev: 2, sevHeavy: 3,
		meanOp: "le", meanThr: -2.0,
		extremeOp: "min_le", extremeThr: -3.0,
		heavyMeanThr: -3.5, heavyExtremeThr: -5.0,
		detail: "真实市场剧震:{cats} 报价均值 {mean}%(阈值 ≤{meanThr}%),最深 {min}%",
	},
	{
		rule: "R2", kind: WorldEventKindGoldSpike, scenario: ScenarioMarketCrash,
		cats: []string{weCatMetal}, catKey: "metal",
		title: "黄金避险升温", dir: WorldEventDirBullish, scope: WorldEventScopeMarket,
		dur: 3, mag: 1, sev: 2,
		meanOp: "ge", meanThr: 2.0,
		extremeOp: "max_ge", extremeThr: 2.0,
		detail: "避险升温:{cats} 报价均值 {mean}%(阈值 ≥{meanThr}%),最高 {max}%",
	},
	{
		rule: "R3", kind: WorldEventKindCryptoCrash, scenario: ScenarioMarketCrash,
		cats: []string{weCatCrypto}, catKey: "crypto",
		title: "加密资产重挫", dir: WorldEventDirBearish, scope: WorldEventScopeMarket,
		dur: 3, mag: 1, sev: 2,
		meanOp: "le", meanThr: -5.0,
		extremeOp: "min_le", extremeThr: -8.0,
		detail: "加密资产重挫:{cats} 报价均值 {mean}%(阈值 ≤{meanThr}%),最深 {min}%",
	},
	{
		rule: "R4", kind: WorldEventKindCommodityInflation, scenario: ScenarioGoodsPriceShock,
		cats: []string{weCatFutures}, catKey: "futures",
		title: "大宗商品通胀", dir: WorldEventDirBearish, scope: WorldEventScopeMacro,
		dur: 6, mag: 1, sev: 2,
		meanOp: "ge", meanThr: 3.0,
		detail: "大宗商品通胀:{cats} 报价均值 {mean}%(阈值 ≥{meanThr}%)",
	},
}

// worldEventKeywordRule 新闻类规则(契约 §3.1 规则表 R5–R10)。
type worldEventKeywordRule struct {
	rule     string
	kind     string
	scenario string
	target   string // policy_shock 的 loose/tight;缺省 ""
	keywords []string
	// heavyKeywords 命中即升一档强度(如 R7 「≥7级」⇒ L3)。
	heavyKeywords []string
	title         string
	dir           string
	scope         string
	dur           int
	mag           int
	magHeavy      int
	sev           int
	note          string // 注入场景的备注(≤60 rune,场景引擎内部再截断)
}

// worldEventKeywordRules 新闻类规则表(顺序 = 派生顺序)。
var worldEventKeywordRules = []worldEventKeywordRule{
	{
		rule: "R5", kind: WorldEventKindPolicyEasing, scenario: ScenarioPolicyShock,
		target: "loose", keywords: []string{"降准", "降息", "减税", "宽松", "刺激"},
		title: "政策宽松信号", dir: WorldEventDirBullish, scope: WorldEventScopePolicy,
		dur: 6, mag: 1, sev: 2, note: "真实世界事件·政策宽松",
	},
	{
		rule: "R6", kind: WorldEventKindPolicyTightening, scenario: ScenarioPolicyShock,
		target: "tight", keywords: []string{"加息", "收紧", "紧缩", "上调利率", "去杠杆"},
		title: "政策紧缩信号", dir: WorldEventDirBearish, scope: WorldEventScopePolicy,
		dur: 6, mag: 1, sev: 2, note: "真实世界事件·政策紧缩",
	},
	{
		rule: "R7", kind: WorldEventKindEarthquake, scenario: ScenarioEarthquake,
		keywords:      []string{"地震", "6级", "7级", "8级", "9级"},
		heavyKeywords: []string{"7级", "8级", "9级"},
		title:         "地震灾害", dir: WorldEventDirBearish, scope: WorldEventScopeDisaster,
		dur: 6, mag: 2, magHeavy: 3, sev: 3, note: "真实世界事件·地震",
	},
	{
		rule: "R8", kind: WorldEventKindTsunami, scenario: ScenarioTsunami,
		keywords: []string{"海啸", "风暴潮", "洪水", "台风"},
		title:    "海啸与风暴潮", dir: WorldEventDirBearish, scope: WorldEventScopeDisaster,
		dur: 9, mag: 2, sev: 3, note: "真实世界事件·海啸",
	},
	{
		rule: "R9", kind: WorldEventKindExtremeDisaster, scenario: ScenarioExtremeDisaster,
		keywords: []string{"重大事故", "矿难", "爆炸", "停产", "停摆"},
		title:    "极端灾难事件", dir: WorldEventDirBearish, scope: WorldEventScopeDisaster,
		dur: 12, mag: 2, sev: 3, note: "真实世界事件·极端灾难",
	},
	{
		rule: "R10", kind: WorldEventKindSurveyPulse, scenario: ScenarioSurveyPulse,
		keywords: []string{"调研", "问卷", "民调"},
		title:    "社会调研脉冲", dir: WorldEventDirNeutral, scope: WorldEventScopeSociety,
		dur: 3, mag: 1, sev: 1, note: "真实世界事件·调研脉冲",
	},
}

// ─────────────────────────── 派生引擎 ───────────────────────────

// DeriveWorldEvents 从中性输入派生世界事件列表(契约 §3.1)。
//
// 纯函数:无 IO、无 RNG、无时钟依赖(now 入参)、同输入同输出。
// 规则表 R1–R10 逐条判定;命中即产出一条事件(新闻类规则取**最新**一条命中)。
// 输出顺序固定(行情类 R1→R4,新闻类 R5→R10)⇒ 幂等闸门与安全阀的处理顺序稳定。
func DeriveWorldEvents(input WorldEventInput, now int64) []WorldEvent {
	out := make([]WorldEvent, 0, len(worldEventQuoteRules)+len(worldEventKeywordRules))
	for i := range worldEventQuoteRules {
		if ev, ok := worldEventQuoteRules[i].derive(input, now); ok {
			out = append(out, ev)
		}
	}
	for i := range worldEventKeywordRules {
		if ev, ok := worldEventKeywordRules[i].derive(input, now); ok {
			out = append(out, ev)
		}
	}
	return out
}

// derive 行情类规则派生(命中返回 true)。
func (r worldEventQuoteRule) derive(input WorldEventInput, now int64) (WorldEvent, bool) {
	var (
		sum    float64
		mn, mx float64
		n      int
		newest int64
	)
	for _, q := range input.Quotes {
		if !r.inCat(q.Category) {
			continue
		}
		if q.UpdatedAt <= 0 || now-q.UpdatedAt > worldEventFreshWindowSec {
			continue // 过期报价不参与判定
		}
		if n == 0 {
			mn, mx = q.ChangePct, q.ChangePct
		} else {
			if q.ChangePct < mn {
				mn = q.ChangePct
			}
			if q.ChangePct > mx {
				mx = q.ChangePct
			}
		}
		sum += q.ChangePct
		n++
		if q.UpdatedAt > newest {
			newest = q.UpdatedAt
		}
	}
	if n == 0 {
		return WorldEvent{}, false // 集合为空 ⇒ 不命中
	}
	mean := sum / float64(n)

	// 触发判定:均值口径 与/或 极值口径。
	hit := false
	triggerVal := mean
	switch r.meanOp {
	case "le":
		if mean <= r.meanThr {
			hit = true
		}
	case "ge":
		if mean >= r.meanThr {
			hit = true
		}
	}
	switch r.extremeOp {
	case "min_le":
		if mn <= r.extremeThr {
			hit = true
			if !r.meanHit(mean) || mn < triggerVal {
				triggerVal = mn
			}
		}
	case "max_ge":
		if mx >= r.extremeThr {
			hit = true
			if !r.meanHit(mean) || mx > triggerVal {
				triggerVal = mx
			}
		}
	}
	if !hit {
		return WorldEvent{}, false
	}

	// 重度档(仅 R1 有)。
	mag, sev := r.mag, r.sev
	if r.heavyMeanThr != 0 && mean <= r.heavyMeanThr {
		mag, sev = r.magHeavy, r.sevHeavy
	}
	if r.heavyExtremeThr != 0 && mn <= r.heavyExtremeThr {
		mag, sev = r.magHeavy, r.sevHeavy
	}

	detail := strings.NewReplacer(
		"{cats}", strings.Join(r.cats, "/"),
		"{mean}", weFormatPct(mean),
		"{min}", weFormatPct(mn),
		"{max}", weFormatPct(mx),
		"{meanThr}", weFormatPct(r.meanThr),
		"{extThr}", weFormatPct(r.extremeThr),
	).Replace(r.detail)

	return WorldEvent{
		Key:            r.quoteKey(newest),
		Rule:           r.rule,
		Kind:           r.kind,
		Severity:       sev,
		Direction:      r.dir,
		Scope:          r.scope,
		Title:          r.title,
		Detail:         detail,
		Magnitude:      triggerVal,
		Source:         "真实行情",
		ObservedAt:     newest,
		ScenarioKind:   r.scenario,
		ScenarioMag:    mag,
		ScenarioDur:    r.dur,
		ScenarioTarget: "",
	}, true
}

// meanHit 均值口径是否命中(仅用于说明取哪个触发值)。
func (r worldEventQuoteRule) meanHit(mean float64) bool {
	switch r.meanOp {
	case "le":
		return mean <= r.meanThr
	case "ge":
		return mean >= r.meanThr
	}
	return false
}

// inCat 报价是否属于本规则的统计集合。
func (r worldEventQuoteRule) inCat(cat string) bool {
	for _, c := range r.cats {
		if c == cat {
			return true
		}
	}
	return false
}

// quoteKey 行情类事件指纹(契约 §3.2):
// "rw:" + rule + ":" + 类目集合 + ":" + dayBucket(UpdatedAt/86400)。
// 按自然日分桶 ⇒ 同一天同一规则只注入一次(次日行情再跌可再次触发)。
func (r worldEventQuoteRule) quoteKey(updatedAt int64) string {
	return "rw:" + r.rule + ":" + r.catKey + ":" +
		strconv.FormatInt(updatedAt/worldEventDayBucketSec, 10)
}

// derive 新闻类规则派生(命中返回 true;取最新一条命中新闻)。
func (r worldEventKeywordRule) derive(input WorldEventInput, now int64) (WorldEvent, bool) {
	var (
		best   *WorldEventNews
		bestKw string
	)
	for i := range input.News {
		n := input.News[i]
		if strings.TrimSpace(n.Title) == "" {
			continue
		}
		if n.PublishedAt <= 0 || now-n.PublishedAt > worldEventFreshWindowSec {
			continue // 只扫 72h 内新闻
		}
		kw := r.matchKeyword(n.Title)
		if kw == "" {
			continue
		}
		if best == nil || n.PublishedAt > best.PublishedAt {
			cp := n
			best = &cp
			bestKw = kw
		}
	}
	if best == nil {
		return WorldEvent{}, false
	}
	mag, sev := r.mag, r.sev
	if r.magHeavy > 0 && r.matchHeavy(best.Title) {
		mag = r.magHeavy
	}
	detail := fmt.Sprintf("触发依据:标题命中「%s」—— %s(来源:%s)", bestKw, best.Title, best.Source)
	return WorldEvent{
		Key:            "rw:" + r.rule + ":" + best.ID,
		Rule:           r.rule,
		Kind:           r.kind,
		Severity:       sev,
		Direction:      r.dir,
		Scope:          r.scope,
		Title:          r.title,
		Detail:         detail,
		Magnitude:      1,
		Source:         best.Source,
		URL:            best.URL,
		ObservedAt:     best.PublishedAt,
		ScenarioKind:   r.scenario,
		ScenarioMag:    mag,
		ScenarioDur:    r.dur,
		ScenarioTarget: r.target,
	}, true
}

// matchKeyword 返回标题命中的第一个关键词(未命中返回空串)。
func (r worldEventKeywordRule) matchKeyword(title string) string {
	for _, kw := range r.keywords {
		if strings.Contains(title, kw) {
			return kw
		}
	}
	return ""
}

// matchHeavy 标题是否命中重度关键词(如 R7 「≥7级」)。
func (r worldEventKeywordRule) matchHeavy(title string) bool {
	for _, kw := range r.heavyKeywords {
		if strings.Contains(title, kw) {
			return true
		}
	}
	return false
}

// weFormatPct 百分数格式化(带符号、两位小数)。
func weFormatPct(v float64) string {
	return strconv.FormatFloat(v, 'f', 2, 64)
}
