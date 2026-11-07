// Package citychrome — metric_spec_batch89_macro.go: 批次89 口径卡条目
// (macro 宏观类目 + news 时政快讯类目)。
//
// 与 metric_spec.go 同 package(纯数据拆分,不改逻辑)。
// 宏观口径对齐 citychrome/sites.go 的确定性提取配方(东财数据中心 5 页:
// CPI_YOY / GDP_YOY / PMI_MFG / FX_RESERVE / LPR_1Y)。
//
// ⚠️ **FX_RESERVE(外汇储备)刻意不登记**:配方把它换算成「万亿美元」,但
// 单位表(设计文档 §3.3)无「万亿美元」取值 —— cny_trillion 语义是**人民币**
// 万亿元,登记即把美元写成人民币(违反「绝不编造口径」)⇒ 保持未登记,
// 走类目兜底单位;若将来单位表补「usd_trillion」须同步补卡(见
// metric_spec.go 已知缺口注释)。
package citychrome

// metricSpecsBatch89Macro 返回宏观与新闻类目的口径卡条目。
func metricSpecsBatch89Macro() []MetricSpec {
	return []MetricSpec{
		// ── macro(宏观指标)────────────────────────────────────────────
		{
			ID:        "macro.rate.lpr1y",
			Category:  CategoryMacro,
			Indicator: "1年期贷款市场报价利率(LPR)",
			Unit:      UnitPct,
			Caliber:   "全国银行间同业拆借中心每月报价公布的 1 年期贷款市场报价利率,企业短期贷款定价基准",
			Formula:   "change_pct = 本期 LPR - 上期 LPR(百分点差)",
			Source:    "东方财富·宏观数据",
			SourceURL: "https://data.eastmoney.com/cjsj/globalRateLPR.html",
			Refresh:   "日级轮转 86400s",
			Keywords:  []string{"LPR_1Y", "lpr", "1年期lpr", "贷款市场报价利率"},
		},
		{
			ID:        "macro.price.cpi",
			Category:  CategoryMacro,
			Indicator: "居民消费价格指数同比(CPI)",
			Unit:      UnitPct,
			Caliber:   "国家统计局公布的居民消费价格指数同比增速,衡量居民端物价通胀水平",
			Formula:   "price = CPI 同比增速(%);change_pct = 本期值 - 上期值(百分点差)",
			Source:    "东方财富·宏观数据",
			SourceURL: "https://data.eastmoney.com/cjsj/cpi.html",
			Refresh:   "日级轮转 86400s",
			Keywords:  []string{"CPI_YOY", "cpi", "cpi同比", "居民消费价格指数"},
		},
		{
			ID:        "macro.gdp.gdp",
			Category:  CategoryMacro,
			Indicator: "国内生产总值同比(GDP)",
			Unit:      UnitPct,
			Caliber:   "国家统计局公布的国内生产总值不变价同比增速,衡量经济总量扩张速度",
			Formula:   "price = GDP 同比增速(%);change_pct = 本期值 - 上期值(百分点差)",
			Source:    "东方财富·宏观数据",
			SourceURL: "https://data.eastmoney.com/cjsj/gdp.html",
			Refresh:   "日级轮转 86400s",
			Keywords:  []string{"GDP_YOY", "gdp", "国内生产总值"},
		},
		{
			ID:        "macro.pmi.mfg",
			Category:  CategoryMacro,
			Indicator: "制造业采购经理指数(PMI)",
			Unit:      UnitPoint,
			Caliber:   "国家统计局与中国物流与采购联合会联合发布的制造业 PMI,50 为荣枯分界线,反映制造业景气度",
			Formula:   "price = 制造业指数(点位);change_pct = 本期值 - 上期值(百分点差)",
			Source:    "东方财富·宏观数据",
			SourceURL: "https://data.eastmoney.com/cjsj/pmi.html",
			Refresh:   "日级轮转 86400s",
			Keywords:  []string{"PMI_MFG", "pmi", "制造业采购经理指数", "制造业PMI"},
		},

		// ── news(时政 / 财经快讯)──────────────────────────────────────
		{
			ID:        "news.flash.telegraph",
			Category:  CategoryNews,
			Indicator: "财经快讯",
			Unit:      UnitText,
			Caliber:   "财联社电报的实时财经快讯流,逐条标注标题与发布时间,属文本型信息而非数值指标",
			Formula:   "无计算式(文本型指标);时效性以 published_at 与采集时刻之差衡量",
			Source:    "财联社·电报",
			SourceURL: "https://www.cls.cn/telegraph",
			Refresh:   "分钟级轮转 600s",
			Keywords:  []string{"快讯", "电报", "telegraph"},
		},
	}
}
