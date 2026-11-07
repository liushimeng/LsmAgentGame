// Package citychrome — metric_spec.go: 真实数据口径卡注册表(批次89)。
//
// 契约出处:lag_docs/虚拟城市/已实现/89-真实数据口径卡与异常告警/01-方案设计.md
//   - §3.2 MetricSpec 形状(逐字对齐);
//   - §3.3 单位取值表 + 类目兜底;
//   - §4.1 口径卡注册表(静态切片,≥20 条,8 类目各 2~4 条核心指标);
//   - §4.2 异常阈值表(单一事实来源);
//   - §4.3 sourceAuthority 静态权重表。
//
// 纪律(对齐 market_hours.go 文件头):
//   - **纯静态**:包级只读目录,零 IO、零 RNG、零全局可变态;
//   - **口径单一事实来源**:阈值 / 单位 / 权威度 / 口径文案全部集中在本文件,
//     前端与 Agent 侧**禁止**另立阈值(否则重演批次88「stale 两套口径」返工);
//   - **副本纪律**:MetricSpecs() 返回深拷贝(对齐 DashboardDimensions()),
//     调用方改动返回值不污染包级目录。
//
// ⚠️ **硬纪律:口径卡覆盖 = 采集器实际能产出的标的集合**(批次89 活体验收
// D1/D2 返工)。即:注册表里的每张卡都对应 sites.go 首选源真能采到的标的,
// 且 sites.go 每个首选源产出的标的**都必须**有卡(覆盖率由
// TestMetricSpecs_CoverCollectorSymbols 断言,漏登记 ⇒ 测试直接红)。
//   - 采集器写入 quote 的 symbol 是**代码形态**(配方:symbol 用代码,如
//     000001 / HSI / DJI / GC / CL),而 sites.go 首选 URL 里是带市场前缀的
//     形态(sh000001 / hkHSI / usDJI / hf_GC)——**两种形态都要进 Keywords**;
//   - Keywords 必须同时覆盖「站点 symbol」「站点 name」「常见别名」,
//     且**禁止**过宽的裸词(如 "上证"/"深证"/"创业板" —— 裸词 ⊂ 具体指数名,
//     会把「上证50」错配到「上证指数」卡上 ⇒ 等于给用户看另一个指标的定义,
//     违反「绝不编造口径」)。
//
// 已知缺口(刻意未登记,理由见上;若将来补卡,须同步更新本注释与
// TestMetricSpecs_CoverCollectorSymbols 的缺口清单):
//   - futures: hf_HG(伦铜) / hf_NID(伦镍) —— 单位表无「美元/吨」取值,
//     登记即写错单位 ⇒ 保持未登记,走类目兜底单位;
//   - macro:   FX_RESERVE(外汇储备) —— 单位表无「万亿美元」取值
//     (cny_trillion 语义为人民币万亿元),登记即写错单位 ⇒ 保持未登记。
//
// 文件拆分:条目数据按类目分置于 metric_spec_batch89_market.go(6 个行情类目)
// 与 metric_spec_batch89_macro.go(macro / news),避免单文件逼近 §4 行数上限。
package citychrome

import "strings"

// ── 单位取值表(设计文档 §3.3)─────────────────────────────────────────
//
// unit 与 RealtimeQuote.unit 同取值表;「类目兜底」在 DefaultUnit 中定义。
const (
	UnitPoint       = "point"        // 指数点位(上证/恒生/纳斯达克/美元指数)
	UnitCNY         = "cny"          // 人民币金额(A 股个股)
	UnitHKD         = "hkd"          // 港币金额(港股个股)
	UnitUSD         = "usd"          // 美元金额(美股个股 / BTC / ETH)
	UnitCNYPerG     = "cny_per_g"    // 人民币/克(沪金 / 沪银)
	UnitUSDPerOz    = "usd_per_oz"   // 美元/盎司(伦敦金 / COMEX 黄金 / 白银)
	UnitUSDPerBbl   = "usd_per_bbl"  // 美元/桶(WTI / 布伦特原油)
	UnitCNYPerT     = "cny_per_t"    // 人民币/吨(沪铜 / 螺纹钢)
	UnitPct         = "pct"          // 百分比(LPR / CPI)
	UnitCNYTrillion = "cny_trillion" // 万亿元(GDP)
	UnitText        = "text"         // 文本(财经快讯)
)

// MetricSpec 是单条指标的口径卡(设计文档 §3.2,字段名与 json tag 逐字对齐)。
//
// Keywords 只在**服务端**做口径卡匹配(不序列化;前端不消费)。
type MetricSpec struct {
	ID        string   `json:"id"`         // "a_share.index.sh" —— 唯一 id
	Category  string   `json:"category"`   // a_share|hk|us|metal|futures|crypto|macro|news
	Indicator string   `json:"indicator"`  // 指标名(如 "上证指数")
	Unit      string   `json:"unit"`       // 计量单位(见 §3.3 取值表)
	Caliber   string   `json:"caliber"`    // 口径说明(一句话:这个数到底是什么)
	Formula   string   `json:"formula"`    // 计算式(如 "change_pct = (price - prev_close) / prev_close * 100")
	Source    string   `json:"source"`     // 来源站点名
	SourceURL string   `json:"source_url"` // 来源站点/接口地址
	Refresh   string   `json:"refresh"`    // 刷新档位说明(如 "交易时段 600s / 休市 7200s")
	Keywords  []string `json:"-"`          // 匹配关键词(不序列化;前端不消费)
}

// metricSpecRegistry 是包级只读口径卡目录(静态;由 buildMetricSpecRegistry
// 汇总各批次文件里的条目切片)。
var metricSpecRegistry = buildMetricSpecRegistry()

// buildMetricSpecRegistry 汇总各批次口径卡条目(单一入口,顺序稳定)。
func buildMetricSpecRegistry() []MetricSpec {
	out := make([]MetricSpec, 0, 32)
	out = append(out, metricSpecsBatch89Market()...)
	out = append(out, metricSpecsBatch89Macro()...)
	return out
}

// MetricSpecs 返回口径卡目录的**深拷贝**(设计文档 §4.1 副本纪律)。
// 调用方改动返回值(含 Keywords 切片)**不污染**包级目录。
func MetricSpecs() []MetricSpec {
	out := make([]MetricSpec, len(metricSpecRegistry))
	for i, s := range metricSpecRegistry {
		out[i] = s
		if len(s.Keywords) > 0 {
			out[i].Keywords = append([]string(nil), s.Keywords...)
		}
	}
	return out
}

// LookupMetricSpec 按 (category, symbol, name) 命中一条口径卡。
//
// 匹配规则(批次89 活体验收 D1 返工后重写;**三段有序,逐段全表扫描**):
//
//	① **symbol 精确匹配**:某张卡的 Keywords 含该 symbol(大小写不敏感,
//	   等值)⇒ 命中。symbol 是最强信号,优先于名称;
//	② **名称精确匹配**:某张卡的 Keywords == name(大小写不敏感)⇒ 命中;
//	③ **名称包含匹配**:name 包含某关键词 ⇒ 命中,**取命中的最长关键词**
//	   (同长度取注册表中首个,顺序稳定)。
//
// ⚠️ **symbol 不做包含匹配**(旧版用 `Contains(sym, kw)` 会让 "000016"
//
//	命中 "000001" 卡);**关键词不得过宽**(裸词 "上证" ⊂ "上证50" ⇒ 曾把
//	上证50 错配到上证指数卡,等于给用户看另一个指标的定义)。
//
// category 非空时只在该类目内匹配;空串 = 跨类目匹配。
// 未命中返回 (MetricSpec{}, false) —— 调用方回退类目兜底单位。
//
// ⚠️ 命中项的 **spec.ID 是稳定唯一键**(注册表内不重复;唯一性由
// TestMetricSpecs_Coverage 断言)—— 消费侧(前端)按 spec.ID **精确**关联
// 口径卡,`Keywords` 不序列化(json:"-")故不可用于关联。
func LookupMetricSpec(category, symbol, name string) (MetricSpec, bool) {
	sym := strings.ToLower(strings.TrimSpace(symbol))
	nm := strings.ToLower(strings.TrimSpace(name))
	// ① symbol 精确匹配(全表扫,首个命中即返回)。
	if sym != "" {
		for _, s := range metricSpecRegistry {
			if category != "" && s.Category != category {
				continue
			}
			if specHasKeyword(s, sym) {
				return s, true
			}
		}
	}
	// ② 名称精确匹配。
	if nm != "" {
		for _, s := range metricSpecRegistry {
			if category != "" && s.Category != category {
				continue
			}
			if specHasKeyword(s, nm) {
				return s, true
			}
		}
	}
	// ③ 名称包含匹配(取最长关键词;同长度取注册表首个)。
	bestIdx := -1
	bestLen := 0
	if nm != "" {
		for i, s := range metricSpecRegistry {
			if category != "" && s.Category != category {
				continue
			}
			for _, kw := range s.Keywords {
				k := strings.ToLower(strings.TrimSpace(kw))
				if k == "" || !strings.Contains(nm, k) {
					continue
				}
				if len(k) > bestLen {
					bestIdx, bestLen = i, len(k)
				}
			}
		}
	}
	if bestIdx >= 0 {
		return metricSpecRegistry[bestIdx], true
	}
	return MetricSpec{}, false
}

// specHasKeyword 报告卡的 Keywords 是否含该词(大小写不敏感,等值)。
func specHasKeyword(s MetricSpec, word string) bool {
	w := strings.ToLower(strings.TrimSpace(word))
	if w == "" {
		return false
	}
	for _, kw := range s.Keywords {
		if strings.ToLower(strings.TrimSpace(kw)) == w {
			return true
		}
	}
	return false
}

// ── 异常阈值表(设计文档 §4.2;单一事实来源)─────────────────────────────
//
// 单位:%(涨跌幅绝对值)。阈值只在本文件定义;前端只渲染后端给的 anomaly /
// confidence,**禁止**复制阈值到前端或 Agent 侧。
var anomalyThresholdByCategory = map[string]float64{
	CategoryAShare:  3.0,
	CategoryHK:      4.0,
	CategoryUS:      3.5,
	CategoryMetal:   2.0,
	CategoryFutures: 3.0,
	CategoryCrypto:  5.0,
	CategoryMacro:   1.0,
}

// defaultAnomalyThreshold 是未登记类目的兜底阈值(%)(news 类目无报价)。
const defaultAnomalyThreshold = 3.0

// AnomalyThresholdPct 返回类目的异常阈值(%)(单一事实来源的只读出口)。
// 未登记类目回退 defaultAnomalyThreshold。
func AnomalyThresholdPct(category string) float64 {
	if v, ok := anomalyThresholdByCategory[category]; ok && v > 0 {
		return v
	}
	return defaultAnomalyThreshold
}

// ── 类目兜底单位(设计文档 §3.3;批次89 D2 修订)────────────────────────
//
// ⚠️ a_share 兜底由 `cny` 改为 `point`:该类目的采集清单(sites.go 首选源)
// **全是指数**(上证/深证/创业板/沪深300/中证500/中证1000/上证50/科创50),
// 个股不在采集清单内 ⇒ 兜底 `cny`(人民币)会把指数点位标成人民币金额。
var defaultUnitByCategory = map[string]string{
	CategoryAShare:  UnitPoint,
	CategoryHK:      UnitHKD,
	CategoryUS:      UnitUSD,
	CategoryMetal:   UnitUSDPerOz,
	CategoryFutures: UnitUSDPerBbl,
	CategoryCrypto:  UnitUSD,
	CategoryMacro:   UnitText,
	CategoryNews:    UnitText,
}

// DefaultUnit 返回类目的兜底单位(未登记类目回退 UnitText)。
func DefaultUnit(category string) string {
	if u, ok := defaultUnitByCategory[category]; ok && u != "" {
		return u
	}
	return UnitText
}

// ── 采集源权威度静态表(设计文档 §4.3)─────────────────────────────────
//
// 源名 → 权重;**子串包含**判定,大小写不敏感(源名多为中文站点别名)。
// 判定顺序:高权威档 → 次权威档 → 已知其它源 → 未知兜底。
var (
	// authorityTier1 是最高权威档(交易所直采 / 一线财经门户 / 通讯社)。
	authorityTier1 = []string{
		"腾讯", "新浪", "东方财富", "交易所", "纳斯达克", "路透", "bloomberg",
	}
	// authorityTier2 是次权威档(持牌财经资讯 / 终端厂商)。
	authorityTier2 = []string{
		"财联社", "华尔街见闻", "第一财经", "同花顺",
	}
	// authorityTier3 是「其它已知名」档(公开可查的行情站 / 数据站)。
	authorityTier3 = []string{
		"金投", "和讯", "网易", "英为财情", "雪球", "金十", "证券之星",
		"coingecko", "coinmarketcap", "binance", "非小号", "金色财经",
		"investing", "yahoo", "cnbc", "wind", "澎湃", "新华网", "统计局",
		"人民银行", "外汇管理局", "上期所", "大商所", "生意社", "钢联",
		"有色", "期货日报", "老虎证券", "阿斯达克",
	}
)

// 权威度权重(设计文档 §4.3)。
const (
	authorityTier1Weight   = 1.0
	authorityTier2Weight   = 0.9
	authorityTier3Weight   = 0.75
	authorityUnknownWeight = 0.6
)

// sourceAuthority 返回采集源的权威度权重(0..1)。
// 未知 / 空源名 ⇒ authorityUnknownWeight(0.6)。
func sourceAuthority(source string) float64 {
	s := strings.ToLower(strings.TrimSpace(source))
	if s == "" {
		return authorityUnknownWeight
	}
	for _, k := range authorityTier1 {
		if strings.Contains(s, strings.ToLower(k)) {
			return authorityTier1Weight
		}
	}
	for _, k := range authorityTier2 {
		if strings.Contains(s, strings.ToLower(k)) {
			return authorityTier2Weight
		}
	}
	for _, k := range authorityTier3 {
		if strings.Contains(s, strings.ToLower(k)) {
			return authorityTier3Weight
		}
	}
	return authorityUnknownWeight
}
