// Package wealthtypes — 财商流游戏玩家 Agent 的 GameContext 契约类型
// (2026-09-14 §财商流P0)。
//
// 本包与 agent/thptypes/ 同源设计,是 **leaf 包**:仅依赖基本类型,
// 不 import game/wealth、agent/wealthplayer、llm/ws(避免循环 import)。
//
// 生命周期约定(与 thptypes 一致):引擎侧(game/wealth/agent_runner.go)在
// **持锁态**构造 GameContext 快照;Agent 侧(wealthplayer)锁外只读消费。
// 各 Brief 结构的字段名与协议文档 §3 的 JSON 键一一对应。
//
// 详见 docs/财商流游戏/已实现/03-Agent设计/财商流游戏-WealthPlayer-Agent设计-v1.md §3。
package wealthtypes

// GameContext 是财商流 Bot 单月决策所需的全部上下文快照。
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
	CreditTightness float64               // 信贷约束系数
	LoanQuotaFactor float64               // 贷款额度乘数
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
	Key        string // "salary" | "tax" | "living" | ...
	AmountCNY  int64  // 正=收入,负=支出
	Text       string
}

// SelfBrief 是 my.* 的全量镜像。
type SelfBrief struct {
	Cash         int64
	Salary       int64  // 当前基准月薪(税前)
	SpouseIncome int64  // 税后净额
	SideIncome   int64  // 上月副业净收入
	PassiveIncome int64 // 上月被动收入合计

	Monthly MonthlyBrief // 最近一次月结

	Energy    int
	Network   int
	Cognition int

	Assets []AssetBrief
	Loans  []LoanBrief

	PensionCNY  int64
	CreditScore int

	Marital   string // single | married
	Children  int

	FIIndex  float64
	NetWorth int64

	ActionBudget int    // 本月剩余动作数
	Goals        []string
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
	Kind            string  // stock_index|bond|gold|house:<d>|shop:<d>|side_business|pension
	Name            string
	Units           float64
	Price           float64
	ValueCNY        int64
	MonthlyFlowCNY  int64
}

// LoanBrief 是单笔负债。
type LoanBrief struct {
	ID            string
	Kind          string
	Principal     int64
	Balance       int64
	AnnualRate    float64
	MonthlyPayment int64
	MonthsLeft    int
}

// PeerBrief 是同场玩家的公开信息。
type PeerBrief struct {
	Seat        int
	Account     string
	Nickname    string
	IsBot       bool
	ProfessionTitle string
	District    string
	NetWorth    int64
	FIIndex     float64
	Alive       bool
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
	Month      int
	From       string
	To         string
	AmountCNY  int64
	Category   string
	Note       string
}

// BotIdentityBrief 是 Bot 自己的身份信息。
type BotIdentityBrief struct {
	UserID     string
	ModelKey   string
	ModelName  string
	AgentClass string // "LsmAgentGame-Wealth-Player"
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
			AgentClass: "LsmAgentGame-Wealth-Player",
		},
	}
}
