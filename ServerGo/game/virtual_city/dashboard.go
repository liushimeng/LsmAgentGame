// Package virtual_city — dashboard.go: 虚拟城市「实时看板」维度目录 + 数据源
// 登记册(批次69,2026-10-21 §批次69)。
//
// 背景:虚拟城市右侧栏原有 财务/行情/流水/经济/行为/宏观/交易/保险 8 个分散
// 面板 + 批次65 的「实时」行情面板,共 9 个平级 Tab。批次69 统一重构为单一
// 「虚拟城市 - 实时看板」多 Tab 分类架构;本文件是**前后端共享的唯一事实
// 来源**:维度目录(Tab 分类契约)与公开免登录金融数据源登记册。
//
// 纪律:
//   - **纯静态目录**:不触碰房间状态、不读数据库、不含 rand —— 与既有
//     prediction.go / scenario.go 同款「零副作用纯函数」契约(§130 接线
//     纪律:声明即被 api 层引用,见 ServerGo/api/virtual_city_dashboard_api.go);
//   - **顺序即前端默认 Tab 顺序**:切片下标是契约的一部分,不可重排;
//   - **导出副本**:DashboardDimensions/DashboardDataSources 返回切片副本,
//     调用方改动不污染包级目录(避免 §130 类"改坏全局"的隐性 Bug)。
package virtual_city

// 看板维度分组常量(前端 Tab 分组标题的分类依据)。
const (
	// DashboardGroupCity 城市内生维度(来自 game_state:城市/玩家自身状态)。
	DashboardGroupCity = "city"
	// DashboardGroupMarket 市场维度(来自 game_state:市场与交易系统)。
	DashboardGroupMarket = "market"
	// DashboardGroupExternal 外部数据维度(citychrome 实时通道 / 静态登记册)。
	DashboardGroupExternal = "external"
)

// 看板维度数据源常量(该维度取数自哪里)。
const (
	// DashboardSourceGameState 取数自对局状态(WS game.state / 房间快照)。
	DashboardSourceGameState = "game_state"
	// DashboardSourceCityChrome 取数自 City-Chrome 实时数据通道。
	DashboardSourceCityChrome = "citychrome"
	// DashboardSourceStatic 取数自静态登记册(前端本地渲染)。
	DashboardSourceStatic = "static"
)

// 看板维度作用域常量(数据的可见范围)。
const (
	// DashboardScopeSelf 仅当前用户可见(自身资产/流水/行为/保险)。
	DashboardScopeSelf = "self"
	// DashboardScopeRoom 全房间可见(城市/经济/宏观/市场)。
	DashboardScopeRoom = "room"
	// DashboardScopeGlobal 全局可见(与房间无关;实时通道为 server 级单例)。
	DashboardScopeGlobal = "global"
)

// 看板维度单位常量(前端格式化口径)。
const (
	// DashboardUnitCNY 人民币金额。
	DashboardUnitCNY = "cny"
	// DashboardUnitPct 百分比。
	DashboardUnitPct = "pct"
	// DashboardUnitIndex 指数点位。
	DashboardUnitIndex = "index"
	// DashboardUnitCount 计数。
	DashboardUnitCount = "count"
	// DashboardUnitText 文本。
	DashboardUnitText = "text"
	// DashboardUnitMixed 混合单位(面板内多指标各自带单位)。
	DashboardUnitMixed = "mixed"
)

// 看板维度刷新策略常量(前端取数/轮询节奏)。
const (
	// DashboardRefreshPush 由 WS 推送驱动(随 game.state 增量刷新)。
	DashboardRefreshPush = "push"
	// DashboardRefreshRealtime 外部实时通道(批次65 City-Chrome)。
	DashboardRefreshRealtime = "realtime"
	// DashboardRefreshMonthly 按月刷新(城市演化节奏)。
	DashboardRefreshMonthly = "monthly"
	// DashboardRefreshStatic 静态,不变。
	DashboardRefreshStatic = "static"
)

// DashboardDimension 是看板维度目录项(前后端共享的 Tab 分类契约)。
type DashboardDimension struct {
	ID       string `json:"id"`        // 维度 id(前端 Tab key)
	Group    string `json:"group"`     // city | market | external
	TitleKey string `json:"title_key"` // i18n key(前端 t() 渲染)
	Source   string `json:"source"`    // game_state | citychrome | static
	Scope    string `json:"scope"`     // self | room | global
	Unit     string `json:"unit"`      // cny | pct | index | count | text | mixed
	Refresh  string `json:"refresh"`   // push | realtime | monthly | static
}

// DataSource 是公开免登录金融数据源登记册条目。
type DataSource struct {
	Category string `json:"category"` // 综合财经 | A股市场 | 港股市场 | 美股市场 | 贵金属大宗 | 期货市场 | 加密货币 | 宏观数据 | 数据接口
	Name     string `json:"name"`     // 站点/机构名
	Purpose  string `json:"purpose"`  // 用途说明
	URL      string `json:"url"`      // 站点地址(均为 https)
}

// dashboardSchemaVersion 是看板目录 schema 版本(结构变更即 +1;前端可据此
// 做兼容分支)。当前 1。
const dashboardSchemaVersion = 1

// dashboardDimensions 是包级维度目录(12 条;顺序 = 前端默认 Tab 顺序)。
// 只读语义:对外一律经 DashboardDimensions() 取副本。
//
// 批次71:追加「校准」维度(id=calibration,分组 city,位于 macro 之后 ——
// 方案 §8.1)。⚠ **前端静态目录才是渲染权威**(CLAUDE.md §28.1),本目录
// 仅作校验/补充;前端未接上该维度前,后端下发不影响既有 Tab。
var dashboardDimensions = []DashboardDimension{
	{ID: "city-overview", Group: DashboardGroupCity, TitleKey: "virtualCity.dash.dim.cityOverview", Source: DashboardSourceGameState, Scope: DashboardScopeRoom, Unit: DashboardUnitMixed, Refresh: DashboardRefreshPush},
	{ID: "finance", Group: DashboardGroupCity, TitleKey: "virtualCity.dash.dim.finance", Source: DashboardSourceGameState, Scope: DashboardScopeSelf, Unit: DashboardUnitCNY, Refresh: DashboardRefreshPush},
	{ID: "ledger", Group: DashboardGroupCity, TitleKey: "virtualCity.dash.dim.ledger", Source: DashboardSourceGameState, Scope: DashboardScopeSelf, Unit: DashboardUnitCNY, Refresh: DashboardRefreshPush},
	{ID: "economy", Group: DashboardGroupCity, TitleKey: "virtualCity.dash.dim.economy", Source: DashboardSourceGameState, Scope: DashboardScopeRoom, Unit: DashboardUnitMixed, Refresh: DashboardRefreshPush},
	{ID: "behavior", Group: DashboardGroupCity, TitleKey: "virtualCity.dash.dim.behavior", Source: DashboardSourceGameState, Scope: DashboardScopeSelf, Unit: DashboardUnitMixed, Refresh: DashboardRefreshPush},
	{ID: "macro", Group: DashboardGroupCity, TitleKey: "virtualCity.dash.dim.macro", Source: DashboardSourceGameState, Scope: DashboardScopeRoom, Unit: DashboardUnitMixed, Refresh: DashboardRefreshPush},
	{ID: "calibration", Group: DashboardGroupCity, TitleKey: "virtualCity.dash.dim.calibration", Source: DashboardSourceGameState, Scope: DashboardScopeRoom, Unit: DashboardUnitMixed, Refresh: DashboardRefreshPush},
	{ID: "market", Group: DashboardGroupMarket, TitleKey: "virtualCity.dash.dim.market", Source: DashboardSourceGameState, Scope: DashboardScopeRoom, Unit: DashboardUnitIndex, Refresh: DashboardRefreshPush},
	{ID: "market-trade", Group: DashboardGroupMarket, TitleKey: "virtualCity.dash.dim.marketTrade", Source: DashboardSourceGameState, Scope: DashboardScopeRoom, Unit: DashboardUnitCNY, Refresh: DashboardRefreshPush},
	{ID: "insurance", Group: DashboardGroupMarket, TitleKey: "virtualCity.dash.dim.insurance", Source: DashboardSourceGameState, Scope: DashboardScopeSelf, Unit: DashboardUnitCNY, Refresh: DashboardRefreshPush},
	{ID: "realtime", Group: DashboardGroupExternal, TitleKey: "virtualCity.dash.dim.realtime", Source: DashboardSourceCityChrome, Scope: DashboardScopeGlobal, Unit: DashboardUnitMixed, Refresh: DashboardRefreshRealtime},
	{ID: "datasources", Group: DashboardGroupExternal, TitleKey: "virtualCity.dash.dim.dataSources", Source: DashboardSourceStatic, Scope: DashboardScopeGlobal, Unit: DashboardUnitText, Refresh: DashboardRefreshStatic},
}

// dashboardDataSources 是包级数据源登记册(45 条;按分类分组顺序排列;
// 批次76 对齐《自迭代优化规范》§八 公开数据源清单 2026-10-22 实测版)。
// 全部为公开免登录可浏览站点(「数据接口」类含需注册的平台,用途注明)——
// 前端「数据源」Tab 直接渲染,不做任何鉴权代理。
// 语义分层:登记册=可查清单(宽),citychrome/sites.go=可采集源(窄)。
var dashboardDataSources = []DataSource{
	// 综合财经(5)
	{Category: "综合财经", Name: "东方财富网", Purpose: "全品类行情、财报、资金流向", URL: "https://www.eastmoney.com"},
	{Category: "综合财经", Name: "新浪财经", Purpose: "全球市场资讯、宏观数据", URL: "https://finance.sina.com.cn"},
	{Category: "综合财经", Name: "中国证券网", Purpose: "证券市场资讯、官方公告", URL: "https://www.cnstock.com"},
	{Category: "综合财经", Name: "中证网", Purpose: "资本市场权威资讯、行情数据", URL: "https://www.cs.com.cn"},
	{Category: "综合财经", Name: "财联社", Purpose: "7×24 财经快讯、突发消息", URL: "https://www.cls.cn"},
	// A股市场(4)
	{Category: "A股市场", Name: "上交所官网", Purpose: "沪市官方行情与规则文件", URL: "https://www.sse.com.cn"},
	{Category: "A股市场", Name: "巨潮资讯网", Purpose: "证监会指定信息披露平台", URL: "https://www.cninfo.com.cn"},
	{Category: "A股市场", Name: "雪球", Purpose: "个股热度、讨论与组合数据", URL: "https://xueqiu.com"},
	{Category: "A股市场", Name: "乐咕乐股", Purpose: "估值、指数与宏观指标", URL: "https://www.legulegu.com"},
	// 港股市场(1)
	{Category: "港股市场", Name: "香港交易所官网", Purpose: "港股官方行情与披露数据", URL: "https://www.hkex.com.hk"},
	// 美股市场(1)
	{Category: "美股市场", Name: "纳斯达克官网", Purpose: "美股官方行情(国内可直连)", URL: "https://www.nasdaq.com"},
	// 贵金属大宗(7)
	{Category: "贵金属大宗", Name: "上海有色金属网", Purpose: "金属现货与期货价格", URL: "https://www.smm.cn"},
	{Category: "贵金属大宗", Name: "生意社", Purpose: "大宗商品价格指数", URL: "https://www.100ppi.com"},
	{Category: "贵金属大宗", Name: "卓创资讯", Purpose: "大宗商品行情分析", URL: "https://www.sci99.com"},
	{Category: "贵金属大宗", Name: "LBMA 伦敦金银协会", Purpose: "国际黄金基准价", URL: "https://www.lbma.org.uk"},
	{Category: "贵金属大宗", Name: "金投网", Purpose: "黄金/外汇实时报价", URL: "https://www.fx678.com"},
	{Category: "贵金属大宗", Name: "Kitco", Purpose: "国际金银与金属报价", URL: "https://www.kitco.com"},
	{Category: "贵金属大宗", Name: "GoldPrice", Purpose: "实时金价", URL: "https://www.goldprice.org"},
	// 期货市场(6)
	{Category: "期货市场", Name: "上海期货交易所", Purpose: "金属与能化期货行情", URL: "https://www.shfe.com.cn"},
	{Category: "期货市场", Name: "上海国际能源交易中心", Purpose: "原油等能源期货行情", URL: "https://www.ine.cn"},
	{Category: "期货市场", Name: "中国金融期货交易所", Purpose: "股指与国债期货官方数据", URL: "https://www.cffex.com.cn"},
	{Category: "期货市场", Name: "郑州商品交易所", Purpose: "农产品与能化期货行情", URL: "https://www.czce.com.cn"},
	{Category: "期货市场", Name: "广州期货交易所", Purpose: "工业硅与碳酸锂期货行情", URL: "https://www.gfex.com.cn"},
	{Category: "期货市场", Name: "期货日报网", Purpose: "期货资讯与策略研究", URL: "https://www.qhrb.com.cn"},
	// 加密货币(3)
	{Category: "加密货币", Name: "AICoin", Purpose: "加密货币行情与快讯", URL: "https://www.aicoin.com"},
	{Category: "加密货币", Name: "非小号", Purpose: "加密货币行情与交易所数据", URL: "https://www.feixiaohao.com"},
	{Category: "加密货币", Name: "金色财经", Purpose: "区块链与加密货币动态", URL: "https://www.jinse.com"},
	// 宏观数据(4)
	{Category: "宏观数据", Name: "中国人民银行官网", Purpose: "货币政策、宏观金融数据", URL: "https://www.pbc.gov.cn"},
	{Category: "宏观数据", Name: "国家统计局", Purpose: "CPI/GDP 等宏观数据", URL: "https://www.stats.gov.cn"},
	{Category: "宏观数据", Name: "财政部", Purpose: "财政收支与国债数据", URL: "https://www.mof.gov.cn"},
	{Category: "宏观数据", Name: "国家外汇管理局", Purpose: "汇率与外汇储备数据", URL: "https://www.safe.gov.cn"},
	// 时政军事(8;批次76 新分组)
	{Category: "时政军事", Name: "新华网", Purpose: "权威时政与突发消息", URL: "https://www.news.cn"},
	{Category: "时政军事", Name: "中国政府网", Purpose: "政策发布与国务院动态", URL: "https://www.gov.cn"},
	{Category: "时政军事", Name: "环球网", Purpose: "国际时事与军事动态", URL: "https://www.huanqiu.com"},
	{Category: "时政军事", Name: "澎湃新闻", Purpose: "深度报道与社会热点", URL: "https://www.thepaper.cn"},
	{Category: "时政军事", Name: "中国新闻网", Purpose: "综合新闻与舆情", URL: "https://www.chinanews.com.cn"},
	{Category: "时政军事", Name: "华尔街见闻", Purpose: "全球财经快讯", URL: "https://wallstreetcn.com"},
	{Category: "时政军事", Name: "第一财经", Purpose: "财经与产业报道", URL: "https://www.yicai.com"},
	{Category: "时政军事", Name: "联合早报", Purpose: "华文国际视角", URL: "https://www.zaobao.com"},
	// 数据接口(6)
	{Category: "数据接口", Name: "东方财富行情 API", Purpose: "股票/指数 JSON 行情(免鉴权)", URL: "https://push2.eastmoney.com/api/qt/clist/get"},
	{Category: "数据接口", Name: "新浪行情接口", Purpose: "股票/期货/外汇快照(需带 Referer)", URL: "https://hq.sinajs.cn/list=sh000001"},
	{Category: "数据接口", Name: "Tushare", Purpose: "金融数据 API(需注册 Token)", URL: "https://www.tushare.pro"},
	{Category: "数据接口", Name: "聚宽 JoinQuant", Purpose: "策略与行情 API(需注册)", URL: "https://www.joinquant.com"},
	{Category: "数据接口", Name: "阿里云 API 市场", Purpose: "多类金融行情接口(需注册)", URL: "https://market.aliyun.com"},
	{Category: "数据接口", Name: "腾讯云 API 市场", Purpose: "证券与大宗商品数据接口(需注册)", URL: "https://market.cloud.tencent.com"},
}

// DashboardSchemaVersion 返回看板目录 schema 版本(当前 1)。
func DashboardSchemaVersion() int { return dashboardSchemaVersion }

// DashboardDimensions 返回看板维度目录副本(11 条;顺序 = 前端默认 Tab 顺序)。
// 返回副本而非包级切片本体 —— 调用方即使排序/改字段也不会污染全局目录。
func DashboardDimensions() []DashboardDimension {
	out := make([]DashboardDimension, len(dashboardDimensions))
	copy(out, dashboardDimensions)
	return out
}

// DashboardDataSources 返回数据源登记册副本(45 条;批次76)。
func DashboardDataSources() []DataSource {
	out := make([]DataSource, len(dashboardDataSources))
	copy(out, dashboardDataSources)
	return out
}
