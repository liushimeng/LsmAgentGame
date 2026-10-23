// Package citychrome — sites.go: 类目 → 备选源映射(契约 §6 / 02-数据源清单.md)。
//
// 每类目 3-5 个国内可直连公开免登录站点;Agent mission 首选 Sites[0],
// 遇拦截/验证码/超时按序换备选(最多 3 次)。纪律:只读公开页面,不登录、
// 不绕验证码、不提交表单。
//
// 2026-10-20 E2E 实测结论(本机):东财 push2 JSON API 对非浏览器 UA/高频
// 访问触发 IIS 403,SPA 页面 DOM 提取耗步;腾讯行情 qt.gtimg.cn 免登录、
// 无反爬、纯文本返回(A股/港股/美股 ~ 分隔,外盘期货 hf_ 逗号分隔),故
// a_share/hk/us/metal/futures 四类首选源改为腾讯行情直读接口,Sites[1:]
// 保留东财系等站点作兜底。各类目附「确定性提取配方」(Mission.Hints),
// Agent 按配方 navigate → 读 body → 解析,2-4 步即可 web_done。
package citychrome

// categorySites 类目 → 备选源(首选在首位)。来源:
// lag_docs/虚拟城市/已实现/65-CityChrome实时数据通道与实时看板/02-数据源清单.md
// 「类目 → 首选 mission 源映射」表(E2E 后修订:首选=腾讯行情直读)。
var categorySites = map[string][]SiteInfo{
	CategoryAShare: {
		{Name: "腾讯行情·A股指数", URL: "https://qt.gtimg.cn/q=sh000001,sz399001,sz399006,sh000300,sh000905,sz399852,sh000016,sh000688"},
		{Name: "东方财富行情中心", URL: "https://quote.eastmoney.com"},
		{Name: "同花顺行情中心", URL: "https://q.10jqka.com.cn"},
		{Name: "新浪财经", URL: "https://finance.sina.com.cn"},
	},
	CategoryHK: {
		{Name: "腾讯行情·港股指数", URL: "https://qt.gtimg.cn/q=hkHSI,hkHSTECH,hkHSCEI"},
		{Name: "东方财富港股", URL: "https://hk.eastmoney.com"},
		{Name: "阿斯达克财经网", URL: "https://www.aastocks.com/tc"},
		{Name: "新浪港股", URL: "https://finance.sina.com.cn/stock/hkstock"},
	},
	CategoryUS: {
		{Name: "腾讯行情·美股指数", URL: "https://qt.gtimg.cn/q=usDJI,usIXIC,usINX"},
		{Name: "东方财富美股", URL: "https://us.eastmoney.com"},
		{Name: "新浪美股", URL: "https://finance.sina.com.cn/stock/usstock"},
		{Name: "老虎证券美股频道", URL: "https://www.itiger.com/us"},
	},
	CategoryMetal: {
		{Name: "腾讯行情·外盘贵金属", URL: "https://qt.gtimg.cn/q=hf_GC,hf_SI"},
		{Name: "上海黄金交易所", URL: "https://www.sge.com.cn"},
		{Name: "金投网", URL: "https://www.fx678.com"},
		{Name: "上海有色金属网", URL: "https://www.smm.cn"},
	},
	CategoryFutures: {
		{Name: "腾讯行情·外盘商品", URL: "https://qt.gtimg.cn/q=hf_CL,hf_GC,hf_SI,hf_HG,hf_NID"},
		{Name: "上海期货交易所", URL: "https://www.shfe.com.cn"},
		{Name: "大连商品交易所", URL: "https://www.dce.com.cn"},
		{Name: "生意社", URL: "https://www.100ppi.com"},
	},
	CategoryCrypto: {
		{Name: "CoinGecko", URL: "https://www.coingecko.com"},
		{Name: "非小号", URL: "https://www.feixiaohao.com"},
		{Name: "金色财经", URL: "https://www.jinse.com"},
		{Name: "CoinMarketCap", URL: "https://coinmarketcap.com"},
	},
	// 批次76(§五.6 分级调度):宏观类目,日级轮转(默认 86400s)。
	// 源顺序按 2026-10-22 本机 CDP 实测导航耗时排序:东财数据中心秒开;
	// 统计局/央行官网页在 CDP 下 60s 超时(network idle 等不到),降为兜底。
	CategoryMacro: {
		{Name: "东方财富·宏观数据", URL: "https://data.eastmoney.com/cjsj/"},
		{Name: "国家外汇管理局", URL: "https://www.safe.gov.cn"},
		{Name: "国家统计局·数据发布", URL: "https://www.stats.gov.cn"},
		{Name: "中国人民银行", URL: "https://www.pbc.gov.cn"},
	},
	// 批次76:时政新闻类目,分钟级轮转(默认 600s),纯新闻产出。
	CategoryNews: {
		{Name: "财联社·电报", URL: "https://www.cls.cn/telegraph"},
		{Name: "华尔街见闻·快讯", URL: "https://wallstreetcn.com/live/global"},
		{Name: "第一财经", URL: "https://www.yicai.com"},
		{Name: "澎湃新闻", URL: "https://www.thepaper.cn"},
		{Name: "新华网", URL: "https://www.news.cn"},
	},
}

// categoryGoals 类目 → 采集目标描述(mission.Goal,写入 Agent user prompt)。
var categoryGoals = map[string]string{
	CategoryAShare:  "采集 A 股核心行情:上证指数/深证成指/创业板指及代表性宽基指数(代码/名称/最新价/涨跌幅),并顺带 2-3 条当日市场热点新闻",
	CategoryHK:      "采集港股核心行情:恒生指数/恒生科技指数/国企指数(代码/名称/最新价/涨跌幅),并顺带 2-3 条当日港股热点新闻",
	CategoryUS:      "采集美股核心行情:道琼斯/纳斯达克/标普500(代码/名称/最新价/涨跌幅),并顺带 2-3 条当日美股热点新闻",
	CategoryMetal:   "采集贵金属行情:纽约黄金/纽约白银(名称/最新价/涨跌幅),并顺带 1-2 条贵金属市场新闻",
	CategoryFutures: "采集大宗商品期货行情:纽约原油/黄金/白银/铜/镍等(名称/最新价/涨跌幅),并顺带 1-2 条大宗商品新闻",
	CategoryCrypto:  "采集加密货币行情:BTC/ETH 及市值前 5 币种(名称/最新价/24h涨跌幅),并顺带 1-2 条加密市场新闻",
	CategoryMacro:   "采集最新发布的关键宏观指标:LPR/CPI同比/GDP同比/制造业PMI/外汇储备等 5-8 条(指标名/最新值/较上期变动),并顺带 2-3 条宏观政策新闻",
	CategoryNews:    "采集最新 5-8 条时政/财经/国际快讯(标题/原文链接/发布时间),纯 news 提交(quotes 留空)",
}

// categoryHints 类目 → 确定性提取配方(E2E 2026-10-20 本机实测验证;
// 批次78 §3.1 实机复测 2026-10-23 修正字段下标)。
//
// ⚠️ 批次78 起**优先用 web_read 的 parse(缺省 auto)做确定性解析**,不要自己
// 按分隔符切数字;下面的人工切分说明只是「parse 未命中」时的兜底口径。
//
// 腾讯行情直读:web_read/web_control navigate 到 URL → 读 body → 解析。
// 首选源失败(403/超时)再换 Sites[1:] 按 DOM 提取。
var categoryHints = map[string]string{
	CategoryAShare: `【确定性提取配方·首选源】
0. **优先用 web_read 一步完成「打开+读正文+自动解析」(parse 缺省 auto)**,
   直接把【结构化解析】块里的 price/change_pct 抄进 web_done,不要自己切数字;
   仅当返回 matched=false 时才按下面的下标手工提取。
1. web_control navigate 到目标 URL;2. web_eval 读 document.body.innerText;
3. 每行形如 v_sh000001="1~上证指数~000001~3658.03~3713.03~...",按 ~ 分割:
  [1]=名称 [2]=代码 [3]=最新价 [4]=昨收 [30]=时间 [31]=涨跌额 [32]=涨跌幅(百分数口径,如 -1.48=-1.48%,**原样填写不要乘 100**);
symbol 用代码(如 000001),currency="CNY"。
注意:若 body 是 "403 Forbidden"(触发了 WAF),立即换备选站点,不要重试同一 URL。`,
	CategoryHK: `【确定性提取配方·首选源】
0. **优先用 web_read 一步完成「打开+读正文+自动解析」(parse 缺省 auto)**,
   直接把【结构化解析】块里的 price/change_pct 抄进 web_done,不要自己切数字;
   仅当返回 matched=false 时才按下面的下标手工提取。
1. web_control navigate 到目标 URL;2. web_eval 读 document.body.innerText;
3. 每行形如 v_hkHSI="100~恒生指数~HSI~22934.680~23651.870~...",按 ~ 分割:
  [1]=名称 [2]=代码 [3]=最新价 [4]=昨收 [30]=时间 [31]=涨跌额 [32]=涨跌幅(百分数口径,如 -3.03=-3.03%,**原样填写不要乘 100**);symbol 用 HSI/HSTECH/HSCEI,currency="HKD"。
注意:若 body 是 "403 Forbidden"(触发了 WAF),立即换备选站点,不要重试同一 URL。`,
	CategoryUS: `【确定性提取配方·首选源】
0. **优先用 web_read 一步完成「打开+读正文+自动解析」(parse 缺省 auto)**,
   直接把【结构化解析】块里的 price/change_pct 抄进 web_done,不要自己切数字;
   仅当返回 matched=false 时才按下面的下标手工提取。
1. web_control navigate 到目标 URL;2. web_eval 读 document.body.innerText;
3. 每行形如 v_usIXIC="200~纳斯达克~.IXIC~26008.86~26702.86~...",按 ~ 分割:
  [1]=名称 [2]=代码 [3]=最新价 [4]=昨收 [30]=时间 [31]=涨跌额 [32]=涨跌幅(百分数口径,如 -2.60=-2.60%,**原样填写不要乘 100**);symbol 用 DJI/IXIC/INX,currency="USD"。
注意:若 body 是 "403 Forbidden"(触发了 WAF),立即换备选站点,不要重试同一 URL。`,
	CategoryMetal: `【确定性提取配方·首选源】
0. **优先用 web_read 一步完成「打开+读正文+自动解析」(parse 缺省 auto)**,
   直接把【结构化解析】块里的 price/change_pct 抄进 web_done,不要自己切数字;
   仅当返回 matched=false 时才按下面的下标手工提取。
1. web_control navigate 到目标 URL;2. web_eval 读 document.body.innerText;
3. 每行形如 v_hf_GC="4036.75,-0.58,...,纽约黄金",
  按逗号分割:[0]=最新价 [1]=涨跌幅(百分数口径,如 -0.58=-0.58%,**原样填写不要乘 100**) [13]=名称(纽约黄金/纽约白银);symbol 用 GC/SI,currency="USD"。
注意:若 body 是 "403 Forbidden"(触发了 WAF),立即换备选站点,不要重试同一 URL。`,
	CategoryFutures: `【确定性提取配方·首选源】
0. **优先用 web_read 一步完成「打开+读正文+自动解析」(parse 缺省 auto)**,
   直接把【结构化解析】块里的 price/change_pct 抄进 web_done,不要自己切数字;
   仅当返回 matched=false 时才按下面的下标手工提取。
1. web_control navigate 到目标 URL;2. web_eval 读 document.body.innerText;
3. 每行形如 v_hf_CL="99.35,-0.12,99.34,99.36,100.15,98.04,15:36:07,98.54,98.24,0,3,5,2026-10-20,纽约原油",
  按逗号分割:[0]=最新价 [1]=涨跌幅(百分数口径,**原样填写不要乘 100**) [13]=名称(纽约原油/纽约黄金/纽约白银/伦铜/伦镍);symbol 用 CL/GC/SI/HG/NID,currency="USD"。
注意:若 body 是 "403 Forbidden"(触发了 WAF),立即换备选站点,不要重试同一 URL。`,
	CategoryCrypto: `【提取建议·首选源】CoinGecko 为 SPA:web_open 后先用 1 次 web_eval 读
document.title + document.body.innerText.slice(0,3000) 定位币种表;再用 web_eval
从表格 DOM 提取 BTC/ETH 等前 5 币种的名称/价格/24h 涨跌(class 名含 price/change)。
备选:navigate 到 https://api.coingecko.com/api/v3/simple/price?ids=bitcoin,ethereum&vs_currencies=usd
读 body JSON(可能 403,403 则回退 DOM 提取);或换非小号/金色财经用 DOM 提取。
price 单位为 USD,change_pct 用 24h 涨跌幅(没有则给 0 并在 source 注明)。`,
	// 批次76:首选东财数据中心(结构化表格,秒开),官网仅作兜底。
	CategoryMacro: `【确定性提取配方·首选源(东方财富数据中心)·严格限量】
只抓下列 5 个指标页,每页 1 次 web_eval 读表格就够,**不要**再点别的标签页:
  ① https://data.eastmoney.com/cjsj/cpi.html           → CPI_YOY
  ② https://data.eastmoney.com/cjsj/gdp.html           → GDP_YOY
  ③ https://data.eastmoney.com/cjsj/pmi.html           → PMI_MFG
  ④ https://data.eastmoney.com/cjsj/hjwh.html          → FX_RESERVE
  ⑤ https://data.eastmoney.com/cjsj/globalRateLPR.html → LPR_1Y
每页流程固定 2 步(web_open → web_eval 读表格),**5 页 ≤ 10 步,必须 web_done**。
表格解析(取最新月份那一行,列序以表头为准):
  - CPI 页列:月份 | 今值 | 预测值 | 前值 ⇒ price=今值, change_pct=今值-前值;
  - PMI 页列:月份 | 制造业指数 | 同比增长 | 非制造业指数 | 同比增长
    ⇒ PMI_MFG.price=制造业指数, change_pct=同比增长(百分点差);
  - 外汇储备页列:月份 | 国家外汇储备(亿美元) | 黄金储备(亿美元)(各带同比/环比)
    ⇒ FX_RESERVE.price=国家外汇储备(亿美元)/10000 换算万亿美元, change_pct=环比;
  - LPR 页列:日期 | 1年期LPR | 5年期以上LPR ⇒ LPR_1Y.price=1年期LPR, change_pct=与上一行之差;
  - GDP 页列:季度 | 当季值 | 同比增长 ⇒ GDP_YOY.price=同比增长, change_pct=与上一季之差。
change_pct 一律是百分点差(**原样填写不要乘 100**,取不到给 0),currency 留空。
某页打不开(403/导航超时)→ 跳过该页、换下一个,**绝不重试同一 URL**,也不要换源。
5 页全失败时降级为纯新闻:从首页「投资日历/宏观新闻」取 2-3 条标题提交。
禁止登录、禁止下载报表文件,只读公开页面文本。`,
	CategoryNews: `【提取建议·批次76】首选财联社电报(Vue SPA):web_open 后快讯流自动加载,
1 次 web_eval 批量提取列表项(title/时间,class 含 telegraph 或快讯流元素);
每条 news 填 title/url/published_at,published_at 按页面时间换算 Unix 秒,
取不到精确时间用当前时间并在 source 注明「时间未标注」。
body 为空/403/验证码 → 立即换备选站点(华尔街见闻 live 页同套路),
不要重试同一 URL。quotes 留空(纯新闻 mission,服务端允许)。`,
}

// BuildMission 按类目构造一次 mission(首选源 = Sites[0])。
func BuildMission(category string) Mission {
	sites := categorySites[category]
	m := Mission{Category: category, Goal: categoryGoals[category], Sites: sites,
		Hints: categoryHints[category]}
	if len(sites) > 0 {
		m.TargetURL = sites[0].URL
	}
	return m
}
