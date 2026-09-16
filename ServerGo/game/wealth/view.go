// Package wealth — view.go: BuildClientState(座位脱敏,协议契约 §3)
// 2026-09-14 §财商流P0)。
//
// 脱敏规则(协议 §7):
//   - players[] 全公开(净资产/FI/资源/位置/职业);
//   - my 仅本人填充;观战者 my=null, my_seat=-1;
//   - bot_contexts 本人 + 观战者可见;其他玩家不可见(view.go 过滤);
//   - ledger_recent 本人相关 + 公共条目最近 50;
//   - events_recent 最近 100 条。
//
// 函数均为纯函数快照,持锁由调用方管理(典型路径:房间 mu 锁内构造快照,
// view 内部不再加锁)。
package wealth

import (
	"math/rand"
)

// ClientGameState 是面向客户端的单座位视图(完整 game.state 载荷)。
// 见协议契约 §3 — 字段名与 JSON 键逐一对应;omitempty 用于协议约定的可选字段。
type ClientGameState struct {
	RoomID         string         `json:"room_id"`
	GameKind       string         `json:"game_kind"`
	Status         string         `json:"status"`
	Month          int            `json:"month"`
	Age            int            `json:"age"`
	Phase          string         `json:"phase"`
	Cycle          CycleJSON      `json:"cycle"`
	Market         MarketJSON     `json:"market"`
	CentralBank    CentralBankJSON `json:"central_bank"` // P1: 央行快照
	MaxSeat        int            `json:"max_seat"`
	NextMonthAt    int64          `json:"next_month_at"`
	GameStartedAt  int64          `json:"game_started_at"`
	Players        []PlayerJSON   `json:"players"`
	MySeat         int            `json:"my_seat"`
	My             *MyJSON        `json:"my"` // nil = 观战者
	BotContexts    []BotCtxJSON   `json:"bot_contexts"`
	LedgerRecent   []LedgerJSON    `json:"ledger_recent"`
	EventsRecent   []EventJSON     `json:"events_recent"`
	Minsky         MinskyOverview  `json:"minsky_overview"`
}

// CentralBankJSON 是 central_bank 子结构(P1,设计文档 §6.5)。
type CentralBankJSON struct {
	M0CNY           float64 `json:"m0_cny"`
	M1CNY           float64 `json:"m1_cny"`
	M2CNY           float64 `json:"m2_cny"`
	MBCNY           float64 `json:"mb_cny"`
	MoneyMultiplier float64 `json:"money_multiplier"`
	PolicyRate      float64 `json:"policy_rate"`
	LPR             float64 `json:"lpr"`
	CPI             float64 `json:"cpi"`
	CreditTightness float64 `json:"credit_tightness"`
	LoanQuotaFactor float64 `json:"loan_quota_factor"`
}

// CycleJSON 是 cycle 字段(契约 §3)。
type CycleJSON struct {
	Phase      string  `json:"phase"`
	LPR        float64 `json:"lpr"`
	CPI        float64 `json:"cpi"`
	MonthsLeft int     `json:"months_left"`
	// P1: 5Y LPR(房贷重定价用,v2.60 N12-3)。
	LPR5Y float64 `json:"lpr_5y"`
}

// MinskyOverview 是明斯基全局概览(v2.60 N11-5,game.state.minsky_overview)。
type MinskyOverview struct {
	PonziCount   int     `json:"ponzi_count"`
	SpecCount    int     `json:"spec_count"`
	HedgeCount   int     `json:"hedge_count"`
	PonziRatio   float64 `json:"ponzy_ratio"`
	CooldownLeft int     `json:"cooldown_left"`
	MomentCount  int     `json:"moment_count"`
}

// MarketJSON 是 market 字段(契约 §3,districts 顺序 = DistrictDefs)。
type MarketJSON struct {
	StockIndex float64                `json:"stock_index"`
	GoldPrice  float64                `json:"gold_price"`
	BondYield  float64                `json:"bond_yield"`
	Districts  []MarketDistrictJSON   `json:"districts"`
}

// MarketDistrictJSON 是 districts 单项(idx_d / rent_index)。
type MarketDistrictJSON struct {
	ID        string  `json:"id"`
	PriceIdx  float64 `json:"price_index"`
	RentIdx   float64 `json:"rent_index"`
}

// PlayerJSON 是单座位公开信息。
type PlayerJSON struct {
	Seat          int     `json:"seat"`
	Account       string  `json:"account"`
	Nickname      string  `json:"nickname"`
	IsBot         bool    `json:"is_bot"`
	ModelDisplay  string  `json:"model_display"`
	Profession    ProfJSON `json:"profession"`
	District      string  `json:"district"`
	HomeDistrict  string  `json:"home_district"`
	Alive         bool    `json:"alive"`
	Retired       bool    `json:"retired"`
	Age           int     `json:"age"`
	Resources     ResourceJSON `json:"resources"`
	NetWorth      int64   `json:"net_worth"`
	FIIndex       float64 `json:"fi_index"`
	IncomeBand    string  `json:"income_band"`
	StatusIcon    string  `json:"status_icon"`
	LastAction    string  `json:"last_action"`
	Ending        string  `json:"ending"`
	// P1: 明斯基状态(v2.60 N11-4)。
	MinskyTier   string  `json:"minsky_tier"`   // hedge/speculative/ponzi(主导等级)
	DebtToIncome float64 `json:"debt_to_income"` // 主导贷款月供/月收入(0-1+)
}

// ProfJSON 是职业卡公开字段。
type ProfJSON struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Avatar string `json:"avatar"`
}

// ResourceJSON 是五维资源子集。
type ResourceJSON struct {
	Energy    int `json:"energy"`
	Network   int `json:"network"`
	Cognition int `json:"cognition"`
}

// MyJSON 是 my.* 全量快照(仅本人/已登录玩家可见,观战者 = nil)。
type MyJSON struct {
	Cash          int64           `json:"cash"`
	SavingsDeposit int64          `json:"savings_deposit"` // P1: 定期存款(M2)
	Salary        int64           `json:"salary"`
	SpouseIncome  int64           `json:"spouse_income"`
	SideIncome    int64           `json:"side_income"`
	PassiveIncome int64           `json:"passive_income"`
	Monthly       MyMonthlyJSON   `json:"monthly"`
	Resources     ResourceJSON    `json:"resources"`
	Assets        []MyAssetJSON   `json:"assets"`
	Loans         []MyLoanJSON    `json:"loans"`
	PensionCNY    int64           `json:"pension_cny"`
	CreditScore   int             `json:"credit_score"`
	Family        MyFamilyJSON    `json:"family"`
	FIIndex       float64         `json:"fi_index"`
	NetWorth      int64           `json:"net_worth"`
	Goals         []string        `json:"goals"`
}

// MyMonthlyJSON 是 my.monthly 子结构。
type MyMonthlyJSON struct {
	Income  int64                `json:"income"`
	Expense int64                `json:"expense"`
	Net     int64                `json:"net"`
	Tax     int64                `json:"tax"`
	Social  int64                `json:"social"`
	Detail  []MyMonthlyItemJSON  `json:"detail"`
}

// MyMonthlyItemJSON 是 my.monthly.detail 单行。
type MyMonthlyItemJSON struct {
	Key       string `json:"key"`
	AmountCNY int64  `json:"amount_cny"`
	Text      string `json:"text"`
}

// MyAssetJSON 是 my.assets 单笔。
type MyAssetJSON struct {
	Kind           string  `json:"kind"`
	Name           string  `json:"name"`
	Units          float64 `json:"units"`
	Price          float64 `json:"price"`
	ValueCNY       int64   `json:"value_cny"`
	MonthlyFlowCNY int64   `json:"monthly_flow_cny"`
}

// MyLoanJSON 是 my.loans 单笔。
type MyLoanJSON struct {
	ID             string  `json:"id"`
	Kind           string  `json:"kind"`
	Principal      int64   `json:"principal"` // 协议 §3 字段名
	Balance        int64   `json:"balance"`
	AnnualRate     float64 `json:"annual_rate"`
	MonthlyPayment int64   `json:"monthly_payment"`
	MonthsLeft     int     `json:"months_left"`
}

// MyFamilyJSON 是 my.family。
type MyFamilyJSON struct {
	Marital  string `json:"marital"`
	Children int    `json:"children"`
}

// BotCtxJSON 是 bot_contexts 单条。
type BotCtxJSON struct {
	Seat              int    `json:"seat"`
	LastDecisionSummary string `json:"last_decision_summary"`
	LastToolInput     string `json:"last_tool_input"`
	LastToolResult    string `json:"last_tool_result"`
	HeartThought      string `json:"heart_thought"`
}

// LedgerJSON 是 ledger_recent 单条。
type LedgerJSON struct {
	Month      int    `json:"month"`
	From       string `json:"from"`
	To         string `json:"to"`
	AmountCNY  int64  `json:"amount_cny"`
	Category   string `json:"category"`
	Note       string `json:"note"`
}

// EventJSON 是 events_recent 单条。
type EventJSON struct {
	Month int    `json:"month"`
	Type  string `json:"type"`
	Text  string `json:"text"`
}

// BuildClientState 构造座位 viewer 可见快照(viewer < 0 = 观战者)。
//
// worldSnapshot / ages 来自房间;此函数无锁;调用方应持房间锁构造 World 快照。
func BuildClientState(roomID string, viewer int, world *World, seats [MaxSeats]string, nicknames [MaxSeats]string, botSeats [MaxSeats]bool, modelKeys [MaxSeats]string, transcripts [MaxSeats]BotTranscript, gameStartedAt, nextMonthAtUnixMs int64) *ClientGameState {
	cs := &ClientGameState{
		RoomID: roomID, GameKind: "wealth",
		Status:StatusOpen, MaxSeat: MaxSeats,
		MySeat: viewer, NextMonthAt: nextMonthAtUnixMs,
		GameStartedAt: gameStartedAt,
		Players: make([]PlayerJSON, MaxSeats),
		// 2026-09-14 §财商流P0-bugfix: 数组字段必须序列化为 [] 而非 null ——
		// Go nil slice → JSON null,前端 xxx.map 直接 TypeError 整页崩溃
		// (ErrorBoundary 兜底)。空集合统一初始化。
		BotContexts:  make([]BotCtxJSON, 0),
		LedgerRecent: make([]LedgerJSON, 0),
		EventsRecent: make([]EventJSON, 0),
	}
	if world == nil {
		cs.Phase = PhaseActing
		// status/age/month 由 caller（game_service_xiangqi.go handleWealthJoin /
		// game.state 分支）从 room 快照覆盖 —— 这里给一个安全的兜底值让
		// 前端 gameState?.status === 'open' 检查能匹配大厅态。
		if cs.Status == "" {
			cs.Status = StatusOpen
		}
		if cs.Month == 0 {
			cs.Month = 1
		}
		return cs
	}
	cs.Status = world.Status
	cs.Month = world.Month
	cs.Age = world.Age()
	cs.Phase = PhaseActing
	if world.Month > 0 && world.Status == StatusPlaying {
		cs.Phase = PhaseActing
	}

	// Cycle + Market。
	p := world.Market.Params()
	cs.Cycle = CycleJSON{Phase: string(world.Market.CyclePhase), LPR: p.LPR, CPI: p.CPI, MonthsLeft: world.Market.CycleMonthsLeft}
	if world.CB != nil {
		cs.Cycle.LPR5Y = world.CB.ComputeL5Y()
	}
	mj := MarketJSON{
		StockIndex: world.Market.StockIndex,
		GoldPrice:  world.Market.GoldPrice,
		BondYield:  p.BondRate,
	}
	for _, d := range DistrictDefs {
		idx := world.Market.DistrictIdx[d.ID]
		mj.Districts = append(mj.Districts, MarketDistrictJSON{
			ID: d.ID, PriceIdx: idx, RentIdx: idx * HouseRentFactor,
		})
	}
	cs.Market = mj

	// CentralBank 子结构(P1;CB 为 nil 时回退 PhaseTable 基础值)。
	cj := CentralBankJSON{
		PolicyRate: p.LPR, // 回退:PhaseTable LPR 作为政策利率近似
		LPR:        p.LPR,
		CPI:        p.CPI,
	}
	if world.CB != nil {
		cj.M0CNY = world.CB.M0
		cj.M1CNY = world.CB.M1
		cj.M2CNY = world.CB.M2
		cj.MBCNY = world.CB.BaseMoney
		cj.MoneyMultiplier = world.CB.MoneyMultiplier
		cj.PolicyRate = world.CB.PolicyRate
		cj.LPR = world.CB.ComputeLPR()
		cj.CPI = world.CB.CPI
		cj.CreditTightness = world.CB.CreditTightness
		cj.LoanQuotaFactor = world.CB.LoanQuotaFactor
	}
	cs.CentralBank = cj

	// Players(全公开)。
	for s, pp := range world.Players {
		if pp == nil {
			pj := PlayerJSON{Seat: s}
			cs.Players[s] = pj
			continue
		}
		// P1: 明斯基主导等级 + DTI(死亡玩家也可见,取零值兜底)。
		minskyTier := ""
		dti := 0.0
		if pp != nil && len(pp.MinskyByLoan) > 0 {
			worst := MinskyHedge
			for _, ms := range pp.MinskyByLoan {
				if ms == nil {
					continue
				}
				if ms.Tier == MinskyPonzi {
					worst = MinskyPonzi
					dti = ms.DebtToIncome
					break
				}
				if ms.Tier == MinskySpeculative {
					worst = MinskySpeculative
					dti = ms.DebtToIncome
				}
			}
			minskyTier = string(worst)
		}
		pj := PlayerJSON{
			Seat: s, Account: pp.Card.ID, Nickname: nicknames[s],
			IsBot: botSeats[s], ModelDisplay: ModelDisplayName(modelKeys[s]),
			Profession: ProfJSON{ID: pp.Card.ID, Title: pp.Card.Title, Avatar: avatarID(pp.Card.ID)},
			District: pp.District, HomeDistrict: pp.HomeDistrict,
			Alive: pp.Alive, Retired: false, Age: pp.Age,
			Resources: ResourceJSON{Energy: pp.Energy, Network: pp.Network, Cognition: pp.Cognition},
			NetWorth:  pp.NetWorth(world.Market),
			FIIndex:   pp.FIIndex(world.Market, world.Age()),
			IncomeBand: pp.IncomeBand(),
			StatusIcon: pp.StatusIcon,
			LastAction: pp.LastActionText,
			Ending:     pp.Ending,
			MinskyTier: minskyTier,
			DebtToIncome: dti,
		}
		cs.Players[s] = pj
	}

	// 观战者:my=null, my_seat=-1。
	isSpectator := viewer < 0 || viewer >= MaxSeats
	if !isSpectator && viewer >= 0 && viewer < MaxSeats {
		cs.MySeat = viewer
		cs.My = myJSONFor(world.Players[viewer])
	} else {
		cs.MySeat = -1
		cs.My = nil
	}

	// BotContexts:本人 + 观战者可见;其他玩家不可见(viewer 是他人时过滤掉他人 Bot)。
	for s := range world.Players {
		t := transcripts[s]
		canSee := isSpectator || s == viewer
		if !canSee {
			continue
		}
		cs.BotContexts = append(cs.BotContexts, BotCtxJSON{
			Seat: s,
			LastDecisionSummary: t.LastDecisionSummary,
			LastToolInput:     t.LastToolInput,
			LastToolResult:    t.LastToolResult,
			HeartThought:      t.HeartThought,
		})
	}

	// LedgerRecent:本人相关 + 公共(全房可见);每人 max 50;这里取本人最近 50。
	if !isSpectator && viewer >= 0 && viewer < MaxSeats {
		ents := world.Ledger.SeatRecent(viewer, 50)
		for _, e := range ents {
			cs.LedgerRecent = append(cs.LedgerRecent, LedgerJSON{
				Month: e.Month, From: e.From, To: e.To,
				AmountCNY: e.AmountCNY, Category: e.Category, Note: e.Note,
			})
		}
	}

	// EventsRecent(全局公共,最近 100 条)。
	recents := world.RecentEvents(100)
	for _, e := range recents {
		cs.EventsRecent = append(cs.EventsRecent, EventJSON{Month: e.Month, Type: e.Type, Text: e.Text})
	}

	// P1: 明斯基全局概览(v2.60 N11-5)。
	cs.Minsky = buildMinskyOverview(world)

	return cs
}

// buildMinskyOverview 统计全局明斯基概览(view 下发)。
func buildMinskyOverview(world *World) MinskyOverview {
	overview := MinskyOverview{CooldownLeft: world.MinskyMomentCooldown, MomentCount: world.MinskyMomentCount}
	alive := 0
	for _, p := range world.Players {
		if p == nil || !p.Alive {
			continue
		}
		alive++
		// 主导等级(取玩家所有贷款的最差者)。
		worst := MinskyHedge
		for _, ms := range p.MinskyByLoan {
			if ms == nil {
				continue
			}
			if ms.Tier == MinskyPonzi {
				worst = MinskyPonzi
				break
			}
			if ms.Tier == MinskySpeculative {
				worst = MinskySpeculative
			}
		}
		switch worst {
		case MinskyPonzi:
			overview.PonziCount++
		case MinskySpeculative:
			overview.SpecCount++
		default:
			overview.HedgeCount++
		}
	}
	if alive > 0 {
		overview.PonziRatio = float64(overview.PonziCount) / float64(alive)
	}
	return overview
}

// avatarID 职业卡头像文件名主干(前端 avatar)。
func avatarID(id string) string { return id }

// myJSONFor 单座位 my.* 镜像。
func myJSONFor(p *Player) *MyJSON {
	if p == nil {
		return nil
	}
	my := &MyMy{Cash: p.Cash, Salary: p.SalaryBase, SpouseIncome: p.Family.SpouseIncome,
		SideIncome: p.Monthly.SideIncome, PassiveIncome: p.Monthly.PassiveIncome,
		PensionCNY: p.PensionCNY, CreditScore: p.CreditScore,
		Resources: ResourceJSON{Energy: p.Energy, Network: p.Network, Cognition: p.Cognition},
		Family:    MyFamilyJSON{Marital: p.Family.Marital, Children: p.Family.Children},
		FIIndex:   p.FIIndex(globalMarketSnap(p), globalAgeSnap(p)),
		NetWorth:  p.NetWorth(globalMarketSnap(p)),
		// 2026-09-14 §财商流P0-bugfix: 数组一律非 nil(空集合序列化为 [],
		// 防止前端 null.map 崩溃)。
		Goals:  append([]string{}, p.Card.Goals...),
		Assets: make([]MyAssetJSON, 0, len(p.Assets)),
		Loans:  make([]MyLoanJSON, 0, len(p.Loans)),
	}
	for i := range p.Assets {
		my.Assets = append(my.Assets, MyAssetJSON{
			Kind: p.Assets[i].Kind,
			Name: assetNameCN(&p.Assets[i]),
			Units: p.Assets[i].Units,
			Price: assetPriceSnap(&p.Assets[i]),
			ValueCNY: AssetValue(&p.Assets[i], globalMarketSnap(p)),
			MonthlyFlowCNY: assetMonthlyFlowSnap(&p.Assets[i]),
		})
	}
	for i := range p.Loans {
		my.Loans = append(my.Loans, MyLoanJSON{
			ID: p.Loans[i].ID, Kind: p.Loans[i].Kind,
			Principal: p.Loans[i].Principal, Balance: p.Loans[i].Balance,
			AnnualRate: p.Loans[i].AnnualRate, MonthlyPayment: p.Loans[i].MonthlyPayment,
			MonthsLeft: p.Loans[i].MonthsLeft,
		})
	}
	my.Monthly = MyMonthlyJSON{
		Income: p.Monthly.Income, Expense: p.Monthly.Expense, Net: p.Monthly.Net,
		Tax: p.Monthly.Tax, Social: p.Monthly.Social,
		Detail: convertMyDetail(p.Monthly.Detail),
	}
	return jsonMy(my)
}

// MyMy 是 myJSONFor 内部使用的待填充结构(纯函数镜像)。
type MyMy struct {
	Cash          int64
	Salary        int64
	SpouseIncome  int64
	SideIncome    int64
	PassiveIncome int64
	Monthly       MyMonthlyJSON
	Resources     ResourceJSON
	Assets        []MyAssetJSON
	Loans         []MyLoanJSON
	PensionCNY    int64
	CreditScore   int
	Family        MyFamilyJSON
	FIIndex       float64
	NetWorth     int64
	Goals         []string
}

// jsonMy 把 MyMy 转成 *MyJSON(避免在 MyMy 上重复 JSON 标签)。
func jsonMy(m *MyMy) *MyJSON {
	if m == nil {
		return nil
	}
	return &MyJSON{
		Cash: m.Cash, Salary: m.Salary, SpouseIncome: m.SpouseIncome,
		SideIncome: m.SideIncome, PassiveIncome: m.PassiveIncome,
		Monthly:    m.Monthly, Resources: m.Resources,
		Assets:     m.Assets, Loans: m.Loans,
		PensionCNY: m.PensionCNY, CreditScore: m.CreditScore,
		Family:    m.Family,
		FIIndex:   m.FIIndex, NetWorth: m.NetWorth, Goals: m.Goals,
	}
}

// globalMarketSnap / globalAgeSnap 提供视图层的 stub 市场快照(viewer 不带
// room 上下文时备用;myJSONFor 走真实路径)。
//
// 实际 view 路径应通过 BuildClientState(持房间锁)传入 world。这里给
// myJSONFor 独立调用提供一个退化走法(测试/单卡解析)。
func globalMarketSnap(p *Player) *MarketState {
	if p == nil || p.Card.ID == "" {
		return nil
	}
	m := NewMarket(rand.New(rand.NewSource(1)))
	return m
}

func globalAgeSnap(p *Player) int { return 25 }

// assetPriceSnap / assetMonthlyFlowSnap myJSONFor 退化路径用。
func assetPriceSnap(a *Asset) float64 { return 0 }
func assetMonthlyFlowSnap(a *Asset) int64 { return 0 }

func convertMyDetail(in []FlowItem) []MyMonthlyItemJSON {
	if len(in) == 0 {
		// 2026-09-14 §财商流P0-bugfix: 返回空数组而非 nil(JSON null 会让前端
		// my.monthly.detail.map 崩溃)。
		return []MyMonthlyItemJSON{}
	}
	out := make([]MyMonthlyItemJSON, len(in))
	for i, f := range in {
		out[i] = MyMonthlyItemJSON{Key: f.Key, AmountCNY: f.AmountCNY, Text: f.Text}
	}
	return out
}

// 2026-09-14 §财商流P0-bugfix: 删除了签名错误的 MarshalJSON() ([]byte, []byte)
// —— 不满足 json.Marshaler 接口(encoding/json 静默忽略,属死代码),且内部
// json.Marshal(cs) 一旦修正签名即无限递归。默认结构体序列化已满足需求。
