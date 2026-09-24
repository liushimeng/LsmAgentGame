// Package wealthtypes — 虚拟城市玩家 Agent 的 GameContext 契约类型
// (2026-09-14 §财商流P0)。
//
// 本包与 agent/thptypes/ 同源设计,是 **leaf 包**:不 import game/wealth、
// agent/wealthplayer、llm/ws(避免循环 import);唯一例外是 import
// agentroot(LsmAgentGame/agent,自身零依赖)以引用 AgentClassCityHuman
// 常量(2026-09-22 §CityHuman重构: 五类 AgentClass 合一,§130 防散写字面量)。
//
// 生命周期约定(与 thptypes 一致):引擎侧(game/wealth/agent_runner.go)在
// **持锁态**构造 GameContext 快照;Agent 侧(wealthplayer)锁外只读消费。
// 各 Brief 结构的字段名与协议文档 §3 的 JSON 键一一对应。
//
// 详见 lag_docs/虚拟城市/已实现/03-Agent设计/虚拟城市-WealthPlayer-Agent设计-v1.md §3。
package wealthtypes

import agentroot "LsmAgentGame/agent"

// GameContext 是虚拟城市 Bot 单月决策所需的全部上下文快照。
type GameContext struct {
	// 身份元数据
	RoomID   string
	GameKind string // 恒 "wealth"
	MySeat   int    // 0..7
	MyUserID string
	ModelKey string

	Month int    // 1..420
	Age   int    // 主时钟年龄
	Phase string // "acting" | "settling"
	// TimeRemainingSec 本月窗口剩余秒(watchdog / prompt 渲染用)。
	TimeRemainingSec int

	// 市场快照
	Cycle  CycleBrief
	Market MarketBrief

	// Me 本人(my.* 全量镜像:三表摘要/资产/贷款/资源/信用/家庭/FI/预算)。
	Me SelfBrief

	// MyCard 本人职业卡(System prompt 渲染;引擎侧从 profession.Card 映射)。
	MyCard CardBrief

	// Peers 同场玩家(公开字段:职业/区/净资产/FI/资源/收入档)。
	Peers []PeerBrief

	// 近期事件与流水(与 game.state 同源)
	RecentEvents []EventBrief  // 最近 30 条
	RecentLedger []LedgerBrief // 本人最近 20 条

	// BotIdentity 自己的 Bot 身份。
	BotIdentity BotIdentityBrief

	// P1: 央行只读快照 + 信贷约束参数(AltAgent 可见)。
	CentralBank     *CentralBankSnapshot // 央行快照(CB 为 nil 时回退 PhaseTable 基础值)
	CreditTightness float64              // 信贷约束系数
	LoanQuotaFactor float64              // 贷款额度乘数

	// P1(2026-09-16 §财商流P1-2 §7.2): 真实经济循环(引擎侧 BuildContextForAgent 填充)。
	CPIYoY             float64  // 篮子 CPI 同比(小数)
	UnemploymentRate   float64  // 内生失业率(小数)
	ConsumptionLevel   int      // 本人当前档位(0-3,兜底后)
	OpenSurveyID       string   // 进行中调研 id(无则 "")
	OpenSurveyQuestion string   // 问题文本
	OpenSurveyOptions  []string // 选项列表
	EconomyBrief       string   // 一行价格涨跌摘要,如 "CPI同比2.3% 失业5.1% 涨幅前二:食品+1.2% 交通+0.8%"

	// §CityHuman重构(2026-09-22): 感官上下文(引擎持锁构造、Agent 锁外只读)。
	Surroundings []NeighborBrief // 同城区邻居摘要(座位居民优先 + 抽样背景居民,≤8 条)
	Ambiance     AmbianceBrief   // 当月所在城区感官画像(气味/声响标签)

	// 批次20(2026-09-24):三块引擎侧预渲染上下文小节(§2 依赖反转 ——
	// 本包不 import game/wealth,文案在 game/wealth/agent_runner.go 生成)。
	// SideMarketBrief 副业定价市场小节(文档2 §4.3;≤350B;无副业 → "")。
	SideMarketBrief string
	// ElectionBrief 市长选举小节(文档3 A5;两行;未启用 → "")。
	ElectionBrief string
	// MicroPriceBrief 股票微观结构现价小节(文档3 B4;≤120B;无信息 → "")。
	MicroPriceBrief string
}

// NeighborBrief 是 see/hear 感知结果中「人」的摘要(全部来自已锚定真实档案;
// 档案未就绪时降级为合成姓名并省略 CardID)。
type NeighborBrief struct {
	Kind       string `json:"kind"` // "seat" | "resident"
	Seat       int    `json:"seat,omitempty"`
	CardID     string `json:"card_id,omitempty"`
	Name       string `json:"name"`
	Occupation string `json:"occupation"`
	District   string `json:"district"`
	MoodHint   string `json:"mood_hint,omitempty"` // 由压力/情绪字段映射的一词状态
}

// AmbianceBrief 是单城区当月感官画像(基底表 + 动态事件叠加)。
type AmbianceBrief struct {
	District string   `json:"district"`
	Smells   []string `json:"smells"` // 基底 + 动态事件气味
	Sounds   []string `json:"sounds"` // 基底 + 动态事件声响
}

// SenseResult 是 see/hear/smell 三个感知工具的统一返回(确定性构造,不调 LLM)。
type SenseResult struct {
	District   string          `json:"district"`
	People     []NeighborBrief `json:"people,omitempty"`     // see 专用
	Things     []string        `json:"things,omitempty"`     // see: 挂牌/店铺/建筑
	Events     []string        `json:"events,omitempty"`     // see/hear: 本区本月事件
	Utterances []string        `json:"utterances,omitempty"` // hear: 近期公开发言摘录
	Smells     []string        `json:"smells,omitempty"`     // smell
	Sounds     []string        `json:"sounds,omitempty"`     // hear/smell 共用环境声
}

// CentralBankSnapshot 央行只读快照(AltAgent 可见)。
type CentralBankSnapshot struct {
	M0, M1, M2      float64
	MB              float64
	MoneyMultiplier float64
	PolicyRate      float64
	LPR             float64
	CPI             float64
	CreditTightness float64
	LoanQuotaFactor float64
}

// CycleBrief 是市场周期快照。
type CycleBrief struct {
	Phase      string  // recovery | boom | recession | depression
	LPR        float64 // 小数,如 0.035
	CPI        float64 // 小数,如 0.02
	MonthsLeft int
}

// MarketBrief 是资产价格快照。
type MarketBrief struct {
	StockIndex float64            // 元/份
	GoldPrice  float64            // 元/克
	BondRate   float64            // 当期新购债券年化(小数)
	HouseIdx   map[string]float64 // district id → idx_d
}

// FlowDetailItem 是月结损益明细单行(my.monthly.detail)。
type FlowDetailItem struct {
	Key       string // "salary" | "tax" | "living" | ...
	AmountCNY int64  // 正=收入,负=支出
	Text      string
}

// SelfBrief 是 my.* 的全量镜像。
type SelfBrief struct {
	Cash          int64
	Salary        int64 // 当前基准月薪(税前)
	SpouseIncome  int64 // 税后净额
	SideIncome    int64 // 上月副业净收入
	PassiveIncome int64 // 上月被动收入合计

	Monthly MonthlyBrief // 最近一次月结

	Energy    int
	Network   int
	Cognition int

	Assets []AssetBrief
	Loans  []LoanBrief

	PensionCNY  int64
	CreditScore int

	Marital  string // single | married
	Children int

	FIIndex  float64
	NetWorth int64

	ActionBudget int // 本月剩余动作数
	Goals        []string

	// P1-4(2026-09-19 §财商流P1-4 §7.4):商业保险保单摘要(prompt 渲染用)。
	Policies []PolicyBrief
}

// PolicyBrief 是单张保单摘要(P1-4;引擎侧 BuildContextForAgent 填充)。
type PolicyBrief struct {
	Kind              string // critical_illness|medical_million|term_life|accident
	MonthlyPremiumCNY int64  // 月缴
	Status            string // active|waiting|grace|lapsed
	WaitingLeft       int    // 等待期剩余月
}

// MonthlyBrief 是最近一次月结摘要。
type MonthlyBrief struct {
	Income        int64
	Expense       int64
	Net           int64
	Tax           int64
	Social        int64
	PassiveIncome int64
	SideIncome    int64
	OvertimeBonus int64
	Detail        []FlowDetailItem
}

// AssetBrief 是单笔持仓。
type AssetBrief struct {
	Kind           string // stock_index|bond|gold|house:<d>|shop:<d>|side_business|pension
	Name           string
	Units          float64
	Price          float64
	ValueCNY       int64
	MonthlyFlowCNY int64
}

// LoanBrief 是单笔负债。
type LoanBrief struct {
	ID             string
	Kind           string
	Principal      int64
	Balance        int64
	AnnualRate     float64
	MonthlyPayment int64
	MonthsLeft     int
}

// PeerBrief 是同场玩家的公开信息。
type PeerBrief struct {
	Seat            int
	Account         string
	Nickname        string
	IsBot           bool
	ProfessionTitle string
	District        string
	NetWorth        int64
	FIIndex         float64
	Alive           bool
}

// EventBrief 是单条事件记录。
type EventBrief struct {
	Month int
	Type  string
	Seat  int
	Text  string
}

// LedgerBrief 是单条流水。
type LedgerBrief struct {
	Month     int
	From      string
	To        string
	AmountCNY int64
	Category  string
	Note      string
}

// BotIdentityBrief 是 Bot 自己的身份信息。
type BotIdentityBrief struct {
	UserID     string
	ModelKey   string
	ModelName  string
	AgentClass string // 恒 string(agentroot.AgentClassCityHuman) = "LsmAgentGame-City-Human"
}

// CardBrief 是职业卡的 Agent 侧投影(System prompt 渲染所需字段)。
// 由引擎侧从 profession.Card 映射 —— wealthplayer 不 import game/wealth,
// 依赖反转与 thptypes 同构(Agent 设计文档 §2)。
type CardBrief struct {
	ID              string
	Title           string
	Name            string
	HomeDistrictCN  string
	Salary          int64
	Expense         int64
	Savings         int64
	StartAge        int
	Energy          int
	Network         int
	Cognition       int
	CreditScore     int
	RiskPreference  string
	Personality     []string
	BehaviorTraits  []string
	HealthGrade     string
	Marital         string
	ChildrenCount   int
	EldersDependent int
	OpeningHook     string
	Goals           []string
}

// BuildEmptyContext 返回 GameContext 的零值(测试 / 单测夹具)。
func BuildEmptyContext(roomID, userID, modelKey string, seat int) *GameContext {
	return &GameContext{
		RoomID:   roomID,
		GameKind: "wealth",
		MySeat:   seat,
		MyUserID: userID,
		ModelKey: modelKey,
		Phase:    "acting",
		Me: SelfBrief{
			Assets: []AssetBrief{},
			Loans:  []LoanBrief{},
			Goals:  []string{},
		},
		Peers:        []PeerBrief{},
		RecentEvents: []EventBrief{},
		RecentLedger: []LedgerBrief{},
		BotIdentity: BotIdentityBrief{
			UserID:     userID,
			ModelKey:   modelKey,
			AgentClass: string(agentroot.AgentClassCityHuman),
		},
	}
}
