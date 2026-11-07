// Package citychrome — metric_spec_batch89_market.go: 批次89 口径卡条目
// (6 个行情类目:a_share / hk / us / metal / futures / crypto)。
//
// 与 metric_spec.go 同 package(纯数据拆分,不改逻辑);条目顺序稳定,
// LookupMetricSpec 的「同长度取首个」依赖该顺序。
//
// ⚠️ **覆盖 = 采集器实际能产出的标的集合**(见 metric_spec.go 文件头硬纪律):
// 逐条对应 sites.go 的首选源 URL 与确定性提取配方;采集器写入 quote 的
// symbol 是**代码形态**(配方「symbol 用代码」),而 URL 里是带市场前缀的
// 形态 —— **两种形态都进 Keywords**。
//
// Keywords 纪律:覆盖「站点 symbol(两种形态)」「站点 name」「常见别名」;
// **禁止过宽裸词**(裸词 ⊂ 具体指数名 ⇒ 错配到别的指标的卡上,等于给用户
// 看另一个指标的定义,违反「绝不编造口径」)。
package citychrome

// metricSpecsBatch89Market 返回 6 个行情类目的口径卡条目。
//
// 覆盖清单(与 sites.go 首选源逐条对齐):
//   - a_share 8:sh000001 sz399001 sz399006 sh000300 sh000905 sz399852
//     sh000016 sh000688 ⇒ 上证指数/深证成指/创业板指/沪深300/中证500/
//     中证1000/上证50/科创50(unit 全部 point)
//   - hk 3:hkHSI hkHSTECH hkHSCEI ⇒ 恒生指数/恒生科技指数/恒生中国企业指数
//   - us 3:usDJI usIXIC usINX ⇒ 道琼斯/纳斯达克/标普500
//   - metal 2:hf_GC hf_SI ⇒ 纽约黄金/纽约白银(配方产出的名字就是「纽约黄金」)
//   - futures 3:hf_CL hf_GC hf_SI ⇒ 纽约原油(usd_per_bbl)+ 纽约黄金/纽约白银
//     (同品种在 metal 与 futures 两个类目各自成卡:类目兜底单位不同,
//     futures 兜底是 usd_per_bbl ⇒ 不登记会把黄金标成「美元/桶」)
//     ⚠️ hf_HG(伦铜) / hf_NID(伦镍) **刻意不登记**:单位表无「美元/吨」取值,
//     登记即写错单位 ⇒ 走类目兜底(见 metric_spec.go 已知缺口)
//   - crypto 3:BTC/ETH/狗狗币(unit usd)
func metricSpecsBatch89Market() []MetricSpec {
	return []MetricSpec{
		// ── a_share(A 股宽基指数,8 张:sites.go 首选源全清单)───────────
		{
			ID:        "a_share.index.sh",
			Category:  CategoryAShare,
			Indicator: "上证指数",
			Unit:      UnitPoint,
			Caliber:   "上海证券交易所全部 A 股的加权综合指数,反映沪市整体走势,是 A 股市场最核心的宽基基准",
			Formula:   "change_pct = (price - prev_close) / prev_close * 100",
			Source:    "腾讯行情",
			SourceURL: "https://qt.gtimg.cn/q=sh000001",
			Refresh:   "交易时段 600s / 休市 7200s",
			Keywords:  []string{"000001", "sh000001", "上证指数", "上证综指"},
		},
		{
			ID:        "a_share.index.sz",
			Category:  CategoryAShare,
			Indicator: "深证成指",
			Unit:      UnitPoint,
			Caliber:   "深圳证券交易所代表性成份股指数,覆盖深市主要上市公司,反映深市整体走势",
			Formula:   "change_pct = (price - prev_close) / prev_close * 100",
			Source:    "腾讯行情",
			SourceURL: "https://qt.gtimg.cn/q=sz399001",
			Refresh:   "交易时段 600s / 休市 7200s",
			Keywords:  []string{"399001", "sz399001", "深证成指"},
		},
		{
			ID:        "a_share.index.cyb",
			Category:  CategoryAShare,
			Indicator: "创业板指",
			Unit:      UnitPoint,
			Caliber:   "创业板 100 只成份股指数,反映成长型中小企业与新兴产业的整体表现",
			Formula:   "change_pct = (price - prev_close) / prev_close * 100",
			Source:    "腾讯行情",
			SourceURL: "https://qt.gtimg.cn/q=sz399006",
			Refresh:   "交易时段 600s / 休市 7200s",
			Keywords:  []string{"399006", "sz399006", "创业板指"},
		},
		{
			ID:        "a_share.index.csi300",
			Category:  CategoryAShare,
			Indicator: "沪深300",
			Unit:      UnitPoint,
			Caliber:   "沪深两市规模最大、流动性最好的 300 只 A 股构成的宽基指数,机构资金基准",
			Formula:   "change_pct = (price - prev_close) / prev_close * 100",
			Source:    "腾讯行情",
			SourceURL: "https://qt.gtimg.cn/q=sh000300",
			Refresh:   "交易时段 600s / 休市 7200s",
			Keywords:  []string{"000300", "sh000300", "沪深300"},
		},
		{
			ID:        "a_share.index.csi500",
			Category:  CategoryAShare,
			Indicator: "中证500",
			Unit:      UnitPoint,
			Caliber:   "沪深两市市值前 500 只证券构成的宽基指数,覆盖中盘蓝筹,是指数化产品与机构对冲的主要基准",
			Formula:   "change_pct = (price - prev_close) / prev_close * 100",
			Source:    "腾讯行情",
			SourceURL: "https://qt.gtimg.cn/q=sh000905",
			Refresh:   "交易时段 600s / 休市 7200s",
			Keywords:  []string{"000905", "sh000905", "中证500"},
		},
		{
			ID:        "a_share.index.csi1000",
			Category:  CategoryAShare,
			Indicator: "中证1000",
			Unit:      UnitPoint,
			Caliber:   "沪深两市市值最小的 1000 只证券构成的宽基指数,覆盖小盘股,反映市场风险偏好尾部",
			Formula:   "change_pct = (price - prev_close) / prev_close * 100",
			Source:    "腾讯行情",
			SourceURL: "https://qt.gtimg.cn/q=sz399852",
			Refresh:   "交易时段 600s / 休市 7200s",
			Keywords:  []string{"399852", "sz399852", "中证1000"},
		},
		{
			ID:        "a_share.index.sh50",
			Category:  CategoryAShare,
			Indicator: "上证50",
			Unit:      UnitPoint,
			Caliber:   "上海证券交易所规模最大、流动性最好的 50 只成份股指数,反映沪市蓝筹大盘与核心资产走势",
			Formula:   "change_pct = (price - prev_close) / prev_close * 100",
			Source:    "腾讯行情",
			SourceURL: "https://qt.gtimg.cn/q=sh000016",
			Refresh:   "交易时段 600s / 休市 7200s",
			Keywords:  []string{"000016", "sh000016", "上证50"},
		},
		{
			ID:        "a_share.index.st50",
			Category:  CategoryAShare,
			Indicator: "科创50",
			Unit:      UnitPoint,
			Caliber:   "上海证券交易所科创板 50 只代表性成份股指数,反映硬科技与新兴成长板块走势",
			Formula:   "change_pct = (price - prev_close) / prev_close * 100",
			Source:    "腾讯行情",
			SourceURL: "https://qt.gtimg.cn/q=sh000688",
			Refresh:   "交易时段 600s / 休市 7200s",
			Keywords:  []string{"000688", "sh000688", "科创50"},
		},

		// ── hk(港股指数,3 张)─────────────────────────────────────────
		{
			ID:        "hk.index.hsi",
			Category:  CategoryHK,
			Indicator: "恒生指数",
			Unit:      UnitPoint,
			Caliber:   "香港交易所主板市值最大的 50 只成份股指数,反映港股整体走势",
			Formula:   "change_pct = (price - prev_close) / prev_close * 100",
			Source:    "腾讯行情",
			SourceURL: "https://qt.gtimg.cn/q=hkHSI",
			Refresh:   "交易时段 600s / 休市 7200s",
			Keywords:  []string{"HSI", "hkHSI", "恒生指数", "恒指"},
		},
		{
			ID:        "hk.index.hstech",
			Category:  CategoryHK,
			Indicator: "恒生科技指数",
			Unit:      UnitPoint,
			Caliber:   "恒生指数公司编制的科技主题指数,覆盖港股头部科技与互联网公司",
			Formula:   "change_pct = (price - prev_close) / prev_close * 100",
			Source:    "腾讯行情",
			SourceURL: "https://qt.gtimg.cn/q=hkHSTECH",
			Refresh:   "交易时段 600s / 休市 7200s",
			Keywords:  []string{"HSTECH", "hkHSTECH", "恒生科技指数"},
		},
		{
			ID:        "hk.index.hscei",
			Category:  CategoryHK,
			Indicator: "恒生中国企业指数",
			Unit:      UnitPoint,
			Caliber:   "在港上市的内地国企成份股指数,反映 H 股与红筹板块表现",
			Formula:   "change_pct = (price - prev_close) / prev_close * 100",
			Source:    "腾讯行情",
			SourceURL: "https://qt.gtimg.cn/q=hkHSCEI",
			Refresh:   "交易时段 600s / 休市 7200s",
			Keywords:  []string{"HSCEI", "hkHSCEI", "国企指数", "恒生中国企业指数"},
		},

		// ── us(美股指数,3 张)─────────────────────────────────────────
		{
			ID:        "us.index.dji",
			Category:  CategoryUS,
			Indicator: "道琼斯工业平均指数",
			Unit:      UnitPoint,
			Caliber:   "美国 30 家大型蓝筹公司股价的加权平均,代表美国传统蓝筹股走势",
			Formula:   "change_pct = (price - prev_close) / prev_close * 100",
			Source:    "腾讯行情",
			SourceURL: "https://qt.gtimg.cn/q=usDJI",
			Refresh:   "交易时段 600s / 休市 7200s",
			Keywords:  []string{"DJI", "usDJI", "道琼斯", "道指"},
		},
		{
			ID:        "us.index.ixic",
			Category:  CategoryUS,
			Indicator: "纳斯达克综合指数",
			Unit:      UnitPoint,
			Caliber:   "纳斯达克市场全部上市普通股的综合指数,科技成长股的主要风向标",
			Formula:   "change_pct = (price - prev_close) / prev_close * 100",
			Source:    "腾讯行情",
			SourceURL: "https://qt.gtimg.cn/q=usIXIC",
			Refresh:   "交易时段 600s / 休市 7200s",
			Keywords:  []string{"IXIC", "usIXIC", "纳斯达克", "纳指"},
		},
		{
			ID:        "us.index.inx",
			Category:  CategoryUS,
			Indicator: "标普500指数",
			Unit:      UnitPoint,
			Caliber:   "美国 500 家大型上市公司指数,覆盖大盘蓝筹与科技股,被视为美国市场总基准",
			Formula:   "change_pct = (price - prev_close) / prev_close * 100",
			Source:    "腾讯行情",
			SourceURL: "https://qt.gtimg.cn/q=usINX",
			Refresh:   "交易时段 600s / 休市 7200s",
			Keywords:  []string{"INX", "usINX", "标普500"},
		},

		// ── metal(贵金属,2 张:hf_GC / hf_SI)──────────────────────────
		{
			ID:        "metal.futures.gc",
			Category:  CategoryMetal,
			Indicator: "纽约黄金",
			Unit:      UnitUSDPerOz,
			Caliber:   "COMEX 黄金期货连续合约最新成交价,以美元/盎司计价,国际金价基准",
			Formula:   "change_pct = (price - prev_close) / prev_close * 100",
			Source:    "腾讯行情",
			SourceURL: "https://qt.gtimg.cn/q=hf_GC",
			Refresh:   "交易时段 600s / 休市 7200s",
			Keywords:  []string{"GC", "hf_GC", "纽约黄金", "COMEX黄金"},
		},
		{
			ID:        "metal.futures.si",
			Category:  CategoryMetal,
			Indicator: "纽约白银",
			Unit:      UnitUSDPerOz,
			Caliber:   "COMEX 白银期货连续合约最新成交价,以美元/盎司计价,兼具工业与贵金属属性",
			Formula:   "change_pct = (price - prev_close) / prev_close * 100",
			Source:    "腾讯行情",
			SourceURL: "https://qt.gtimg.cn/q=hf_SI",
			Refresh:   "交易时段 600s / 休市 7200s",
			Keywords:  []string{"SI", "hf_SI", "纽约白银", "COMEX白银"},
		},

		// ── futures(大宗商品期货,3 张:CL / GC / SI)─────────────────────
		{
			ID:        "futures.oil.cl",
			Category:  CategoryFutures,
			Indicator: "纽约原油",
			Unit:      UnitUSDPerBbl,
			Caliber:   "NYMEX 轻质原油期货连续合约最新成交价,以美元/桶计价,国际油价基准",
			Formula:   "change_pct = (price - prev_close) / prev_close * 100",
			Source:    "腾讯行情",
			SourceURL: "https://qt.gtimg.cn/q=hf_CL",
			Refresh:   "交易时段 600s / 休市 7200s",
			Keywords:  []string{"CL", "hf_CL", "纽约原油", "WTI"},
		},
		{
			ID:        "futures.metal.gc",
			Category:  CategoryFutures,
			Indicator: "纽约黄金",
			Unit:      UnitUSDPerOz,
			Caliber:   "COMEX 黄金期货连续合约最新成交价(大宗商品任务口径),以美元/盎司计价",
			Formula:   "change_pct = (price - prev_close) / prev_close * 100",
			Source:    "腾讯行情",
			SourceURL: "https://qt.gtimg.cn/q=hf_GC",
			Refresh:   "交易时段 600s / 休市 7200s",
			Keywords:  []string{"GC", "hf_GC", "纽约黄金"},
		},
		{
			ID:        "futures.metal.si",
			Category:  CategoryFutures,
			Indicator: "纽约白银",
			Unit:      UnitUSDPerOz,
			Caliber:   "COMEX 白银期货连续合约最新成交价(大宗商品任务口径),以美元/盎司计价",
			Formula:   "change_pct = (price - prev_close) / prev_close * 100",
			Source:    "腾讯行情",
			SourceURL: "https://qt.gtimg.cn/q=hf_SI",
			Refresh:   "交易时段 600s / 休市 7200s",
			Keywords:  []string{"SI", "hf_SI", "纽约白银"},
		},

		// ── crypto(加密货币,3 张)────────────────────────────────────
		{
			ID:        "crypto.coin.btc",
			Category:  CategoryCrypto,
			Indicator: "比特币",
			Unit:      UnitUSD,
			Caliber:   "比特币(BTC)对美元的最新成交价,按全球主要交易所加权报价,市值最大的加密资产",
			Formula:   "change_pct = 24 小时涨跌幅(相对 24 小时前价格)",
			Source:    "CoinGecko",
			SourceURL: "https://www.coingecko.com/en/coins/bitcoin",
			Refresh:   "7×24 恒开市,轮转 600s",
			Keywords:  []string{"BTC", "bitcoin", "比特币"},
		},
		{
			ID:        "crypto.coin.eth",
			Category:  CategoryCrypto,
			Indicator: "以太坊",
			Unit:      UnitUSD,
			Caliber:   "以太坊(ETH)对美元的最新成交价,第二大加密资产,智能合约生态代表",
			Formula:   "change_pct = 24 小时涨跌幅(相对 24 小时前价格)",
			Source:    "CoinGecko",
			SourceURL: "https://www.coingecko.com/en/coins/ethereum",
			Refresh:   "7×24 恒开市,轮转 600s",
			Keywords:  []string{"ETH", "ethereum", "以太坊"},
		},
		{
			ID:        "crypto.coin.doge",
			Category:  CategoryCrypto,
			Indicator: "狗狗币",
			Unit:      UnitUSD,
			Caliber:   "狗狗币(DOGE)对美元的最新成交价,高波动性 meme 类资产代表",
			Formula:   "change_pct = 24 小时涨跌幅(相对 24 小时前价格)",
			Source:    "CoinGecko",
			SourceURL: "https://www.coingecko.com/en/coins/dogecoin",
			Refresh:   "7×24 恒开市,轮转 600s",
			Keywords:  []string{"DOGE", "dogecoin", "狗狗币"},
		},
	}
}
