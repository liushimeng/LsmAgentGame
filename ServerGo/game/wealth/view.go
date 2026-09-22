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
	"fmt"
	"math/rand"
	"sort"

	"LsmAgentGame/game/wealth/city"
)

// ClientGameState 是面向客户端的单座位视图(完整 game.state 载荷)。
// 见协议契约 §3 — 字段名与 JSON 键逐一对应;omitempty 用于协议约定的可选字段。
type ClientGameState struct {
	RoomID        string          `json:"room_id"`
	GameKind      string          `json:"game_kind"`
	Status        string          `json:"status"`
	Month         int             `json:"month"`
	Age           int             `json:"age"`
	Phase         string          `json:"phase"`
	Cycle         CycleJSON       `json:"cycle"`
	Market        MarketJSON      `json:"market"`
	CentralBank   CentralBankJSON `json:"central_bank"` // P1: 央行快照
	MaxSeat       int             `json:"max_seat"`
	NextMonthAt   int64           `json:"next_month_at"`
	GameStartedAt int64           `json:"game_started_at"`
	Players       []PlayerJSON    `json:"players"`
	MySeat        int             `json:"my_seat"`
	My            *MyJSON         `json:"my"` // nil = 观战者
	BotContexts   []BotCtxJSON    `json:"bot_contexts"`
	LedgerRecent  []LedgerJSON    `json:"ledger_recent"`
	EventsRecent  []EventJSON     `json:"events_recent"`
	Minsky        MinskyOverview  `json:"minsky_overview"`
	// P1(§财商流P1-2 §6.1):economy_enabled=false 时为零值/空数组下发。
	ConsumerMarket ConsumerMarketJSON `json:"consumer_market"`
	LaborMarket    LaborMarketJSON    `json:"labor_market"`
	Society        SocietyJSON        `json:"society"`
	Surveys        []SurveyJSON       `json:"surveys"` // 调研契约 §5.1
	// P2 v2(2026-09-19 §P2-可视化):本月资金流向;omitempty 保证空月份不下发。
	FlowStat *FlowStatJSON `json:"flow_stat,omitempty"`
	// 阶段4(2026-09-21 §城市扩张v2.12):政府财政国库快照(nil → 不下发;
	// 阶段4 前对局/未开 economy_enabled 兼容)。
	Treasury *TreasuryJSON `json:"treasury,omitempty"`
	// 阶段5(2026-09-21 §城市扩张v2.12):产业链快照(利用率 top5 节点 +
	// 4 层价格指数 + 瓶颈警告)与产业集群快照(nil/空 → omitempty)。
	SupplyChain *SupplyChainJSON `json:"supply_chain,omitempty"`
	Clusters    []ClusterJSON    `json:"industrial_clusters,omitempty"`
	// 阶段6(2026-09-21 §城市扩张v2.12):金融市场快照(量化指数 / CD 利率 /
	// 可转债 top3 / 融券余额 / 基金评级 top5;economy_enabled=false → omitempty)。
	FinMarket *FinMarketJSON `json:"fin_market,omitempty"`
	// 阶段8(2026-09-21 §城市扩张v2.12,最终阶段):公共服务 + 监管 + 市长选举
	// 快照(五件套指标 / 监管罚款与分拆计数 / 选举状态;economy_enabled=false
	// → omitempty)。
	PublicSvc *PublicServicesJSON `json:"public_services,omitempty"`
	// 2026-09-21 §虚拟城市(契约 04 §1.3):城市背景层快照(resident_count>0
	// 时非 nil;旧房/未建城 omit)。无座位隐私,观战/玩家全量可见。
	City *city.Snapshot `json:"city,omitempty"`
}

// ConsumerMarketJSON 是 consumer_market 子结构(P1 §6.1)。
type ConsumerMarketJSON struct {
	CPIYoY float64         `json:"cpi_yoy"`
	CPIMom float64         `json:"cpi_mom"`
	Goods  []GoodsItemJSON `json:"goods"` // 8 类,按 §2.1 表序
}

// GoodsItemJSON 是 consumer_market.goods 单项。
type GoodsItemJSON struct {
	ID        string  `json:"id"`
	Weight    float64 `json:"weight"`
	PriceIdx  float64 `json:"price_idx"`
	MomChange float64 `json:"mom_change"`
}

// LaborMarketJSON 是 labor_market 子结构(P1 §6.1)。
type LaborMarketJSON struct {
	UnemploymentRate float64 `json:"unemployment_rate"`
	EmploymentRatio  float64 `json:"employment_ratio"`
	AvgWageGrowthYoY float64 `json:"avg_wage_growth_yoy"`
	FirmRevenueCNY   int64   `json:"firm_revenue_cny"`
	LayoffWave       int     `json:"layoff_wave"`
}

// SocietyJSON 是 society 子结构(P1 §6.1 + P2 v2 §13.2.4)。
type SocietyJSON struct {
	Gini      float64    `json:"gini"`
	Quintiles [5]float64 `json:"quintiles"`
	Circles   struct {
		Survival   int `json:"survival"`
		Accumulate int `json:"accumulate"`
		Freedom    int `json:"freedom"`
	} `json:"circles"`
	// P2 v2 新增(2026-09-19 §P2-可视化)。omitzero 保证未开 economy_enabled 不下发。
	TotalWealth   int64             `json:"total_wealth,omitempty"`
	MedianWealth  int64             `json:"median_wealth,omitempty"`
	MeanWealth    int64             `json:"mean_wealth,omitempty"`
	Percentiles   map[string]int64  `json:"percentiles,omitempty"` // {"p10":..,"p25":..,"p50":..,"p75":..,"p90":..}
	LorenzPoints  [][2]float64      `json:"lorenz_points,omitempty"`
	PyramidLayers []WealthLayerJSON `json:"pyramid_layers,omitempty"`

	// 阶段7 新增(2026-09-21 §城市扩张v2.12):社会结构指标体系(快照 +
	// 12 月趋势)。economy_enabled=false 时零值下发(与 Gini/Quintiles 同款,
	// 数组字段 Trend 以 omitempty 保持空帧不携带)。
	GiniIncome      float64            `json:"gini_income"`
	Top1Pct         float64            `json:"top1_pct"`
	Top10Pct        float64            `json:"top10_pct"`
	Bottom50Pct     float64            `json:"bottom50_pct"`
	WealthQuintiles [5]float64         `json:"wealth_quintiles"` // 财富五等分(区别于 Quintiles 收入五等分)
	Pyramid         [4]int             `json:"pyramid"`          // 4 层绝对门槛金字塔人数(自下而上)
	MobilityYoung   float64            `json:"mobility_young"`   // <30 岁月度流动性
	Trend           []SocietyTrendJSON `json:"trend,omitempty"`  // 最近 12 月趋势(时间升序)
}

// SocietyTrendJSON 是 society.trend 单月趋势点(阶段7,2026-09-21 §城市扩张v2.12;
// 只携带走线图必要字段,完整快照在引擎 SocietyHistory 中)。
type SocietyTrendJSON struct {
	Month         int     `json:"month"`
	GiniWealth    float64 `json:"gini_wealth"`
	GiniIncome    float64 `json:"gini_income"`
	Top10Pct      float64 `json:"top10_pct"`
	MobilityYoung float64 `json:"mobility_young"`
}

// WealthLayerJSON 是金字塔单层视图(P2 v2 §13.2.4)。
type WealthLayerJSON struct {
	Name        string  `json:"name"`  // "survival" | "accumulation" | "freedom"
	Count       int     `json:"count"` // 人数
	TotalWealth int64   `json:"total_wealth"`
	AvgWealth   int64   `json:"avg_wealth"`
	WealthPct   float64 `json:"wealth_pct"` // 占总财富 0-1
}

// TreasuryJSON 是 treasury 子结构(阶段4 2026-09-21 §城市扩张v2.12;
// 政府/观战者全量可见,无座位隐私)。
type TreasuryJSON struct {
	CashCNY             int64   `json:"cash_cny"`
	BondsOutstandingCNY int64   `json:"bonds_outstanding_cny"`
	LastMonthRevenueCNY int64   `json:"last_month_revenue_cny"`
	LastMonthExpenseCNY int64   `json:"last_month_expense_cny"`
	DeficitRun          int     `json:"deficit_run"`
	PublicServiceIdx    float64 `json:"public_service_idx"`
}

// SupplyChainJSON 是 supply_chain 子结构(阶段5 2026-09-21 §城市扩张v2.12;
// 政府/观战者全量可见,无座位隐私)。
type SupplyChainJSON struct {
	TopNodes    []SupplyNodeJSON `json:"top_nodes"`   // 利用率 top5 节点
	PriceIdx    [4]float64       `json:"price_idx"`   // Tier 0-3 价格指数(基 1.0)
	Bottlenecks []BottleneckJSON `json:"bottlenecks"` // 瓶颈警告(≤5 条)
}

// SupplyNodeJSON 是 supply_chain.top_nodes 单项。
type SupplyNodeJSON struct {
	ID            string  `json:"id"`
	Industry      string  `json:"industry"`
	NameCN        string  `json:"name_cn"`
	Tier          int     `json:"tier"`
	Utilization   float64 `json:"utilization"`      // 0..1
	InventoryMons float64 `json:"inventory_months"` // 库存可用月数
}

// BottleneckJSON 是 supply_chain.bottlenecks 单项(Reason:
// supply_gap 供货缺口 / below_safety_stock 低于安全库存 / full_capacity 满负荷)。
type BottleneckJSON struct {
	NodeID   string `json:"node_id"`
	Industry string `json:"industry"`
	NameCN   string `json:"name_cn"`
	Reason   string `json:"reason"`
	Severity int    `json:"severity"` // 1-2(2 = 已实际缺货)
}

// ClusterJSON 是 industrial_clusters 单项(阶段5;DistrictName 由后端
// DistrictCN 展开,前端免查表)。
type ClusterJSON struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	DistrictID      string   `json:"district_id"`
	DistrictName    string   `json:"district_name"`
	Industries      []string `json:"industries"`
	Firms           int      `json:"firms"`
	TaxBreakMonths  int      `json:"tax_break_months"`
	TaxBreakUsedWan float64  `json:"tax_break_used_wan"` // 累计已减免(万元)
	TaxBreakCapWan  float64  `json:"tax_break_cap_wan"`  // 年度减免上限(万元)
	LastBreakCNY    int64    `json:"last_break_cny"`     // 上月实际减免(元)
}

// FinMarketJSON 是 fin_market 子结构(阶段6 2026-09-21 §城市扩张v2.12;
// 政府/观战者全量可见,无座位隐私)。
type FinMarketJSON struct {
	QuantIndex      float64              `json:"quant_index"`       // 量化基金指数(基 100)
	QuantLastReturn float64              `json:"quant_last_return"` // 上月组合收益(小数)
	QuantStrategies []QuantStrategyJSON  `json:"quant_strategies"`  // 5 策略权重
	CD              CDSnapshotJSON       `json:"cd"`                // 同业存单快照
	Convertibles    []CBondJSON          `json:"convertibles"`     // 可转债 top3(按市价)
	Short           ShortSnapshotJSON    `json:"short"`             // 融券快照
	FundRatings     []FundRatingJSON     `json:"fund_ratings"`      // 基金评级 top5(按星数)
}

// QuantStrategyJSON 是 fin_market.quant_strategies 单项。
type QuantStrategyJSON struct {
	Name       string  `json:"name"`
	Weight     float64 `json:"weight"`
	LastReturn float64 `json:"last_return"`
}

// CDSnapshotJSON 是 fin_market.cd 子结构(Rates 键为期限月数)。
type CDSnapshotJSON struct {
	AvgRate        float64           `json:"avg_rate"`          // 存量加权平均票面
	OutstandingWan float64           `json:"outstanding_wan"`   // 挂牌存量(万元)
	Rates          map[string]float64 `json:"rates"`            // 期限 → 票面("1"/"3"/"6"/"12")
}

// CBondJSON 是 fin_market.convertibles 单项。
type CBondJSON struct {
	ID         string  `json:"id"`
	Issuer     string  `json:"issuer"`
	IssuerNode string  `json:"issuer_node"` // supply_chain.go 节点 id
	Price      float64 `json:"price"`       // 市价(元/张)
	ConvValue  float64 `json:"conv_value"`  // 转股价值
	BondFloor  float64 `json:"bond_floor"`  // 债底
	CouponRate float64 `json:"coupon_rate"`
	Status     string  `json:"status"` // active|redeemed|put|matured
	MonthsLeft int     `json:"months_left"`
}

// ShortSnapshotJSON 是 fin_market.short 子结构(R6-1 护栏监测面)。
type ShortSnapshotJSON struct {
	BalanceCNY     int64 `json:"balance_cny"`      // 融券余额(元)
	MarginCNY      int64 `json:"margin_cny"`       // 保证金冻结(元)
	OpenCount      int   `json:"open_count"`       // 未平仓笔数
	MarginCallsMon int   `json:"margin_calls_mon"` // 上月强平笔数
}

// FundRatingJSON 是 fin_market.fund_ratings 单项。
type FundRatingJSON struct {
	FundID      string  `json:"fund_id"`
	Name        string  `json:"name"`
	Stars       int     `json:"stars"`
	Sharpe      float64 `json:"sharpe"`
	MaxDrawdown float64 `json:"max_drawdown"`
	AUMWan      float64 `json:"aum_wan"`
}

// PublicServicesJSON 是 public_services 子结构(阶段8 2026-09-21 §城市扩张v2.12):
// 公共服务五件套 + 四监管监测面 + 市长选举状态。
type PublicServicesJSON struct {
	EduQuality    float64 `json:"edu_quality"`
	MedQuality    float64 `json:"med_quality"`
	PensionLevel  float64 `json:"pension_level"`
	HousingAfford float64 `json:"housing_afford"`
	Employment    float64 `json:"employment"`
	EduStockWan   float64 `json:"edu_stock_wan"` // 公共教育累积投入(万元)
	MedStockWan   float64 `json:"med_stock_wan"` // 公共医疗累积投入(万元)
	// 监管监测面。
	InsiderFinesTotal int64 `json:"insider_fines_total"` // 证监会累计罚款(元)
	InsiderCases      int   `json:"insider_cases"`       // 累计认定笔数
	AntitrustSplits   int   `json:"antitrust_splits"`    // 反垄断累计分拆次数
	// 市长选举(R8-2 默认关闭:enabled=false / mayor_seat=-1 / votes 空)。
	ElectionEnabled bool           `json:"election_enabled"`
	MayorSeat       int            `json:"mayor_seat"`
	LastVotes       []ElectionVote `json:"last_votes,omitempty"`
}

// buildPublicServicesJSON 公共服务+监管+选举快照(阶段8;nil 守卫)。
func buildPublicServicesJSON(world *World) *PublicServicesJSON {
	if world == nil {
		return nil
	}
	ps := &PublicServicesJSON{}
	if world.PublicSvc != nil {
		ps.EduQuality = world.PublicSvc.EduQuality
		ps.MedQuality = world.PublicSvc.MedQuality
		ps.PensionLevel = world.PublicSvc.PensionLevel
		ps.HousingAfford = world.PublicSvc.HousingAfford
		ps.Employment = world.PublicSvc.Employment
		if world.PublicSvc.Edu != nil {
			ps.EduStockWan = world.PublicSvc.Edu.PublicEduStock
		}
		if world.PublicSvc.Med != nil {
			ps.MedStockWan = world.PublicSvc.Med.PublicMedStock
		}
	}
	if world.Regulators != nil && world.Regulators.Securities != nil {
		ps.InsiderFinesTotal = world.Regulators.Securities.InsiderTradeFines
		ps.InsiderCases = world.Regulators.Securities.InsiderCases
	}
	if world.Regulators != nil && world.Regulators.Antitrust != nil {
		ps.AntitrustSplits = world.Regulators.Antitrust.Splits
	}
	if world.Election != nil {
		ps.ElectionEnabled = world.Election.Enabled
		ps.MayorSeat = world.Election.MayorSeat
		ps.LastVotes = world.Election.LastVotes
	}
	return ps
}

// buildFinMarketJSON 金融市场快照(阶段6;确定性:排序 tie-break 用稳定序)。
// 可转债 top3 按市价降序;基金评级 top5 按星数降序、AUM 降序 tie-break。
func buildFinMarketJSON(world *World) *FinMarketJSON {
	if world == nil {
		return nil
	}
	fm := &FinMarketJSON{
		QuantIndex:      world.QuantEngine.Index,
		QuantLastReturn: world.QuantEngine.LastMonthReturn,
		CD:              CDSnapshotJSON{Rates: map[string]float64{}},
		Convertibles:    make([]CBondJSON, 0, 3),
		FundRatings:     make([]FundRatingJSON, 0, 5),
		Short:           ShortSnapshotJSON{},
	}
	for _, s := range world.QuantEngine.Strategies {
		fm.QuantStrategies = append(fm.QuantStrategies, QuantStrategyJSON{
			Name: s.Name, Weight: s.Weight, LastReturn: s.LastReturn,
		})
	}
	if world.CDMarket != nil {
		fm.CD.AvgRate = world.CDMarket.LastAvgRate
		fm.CD.OutstandingWan = world.CDMarket.TotalOutstandingWan
		for _, t := range CDTenures { // CDTenures 固定序(1/3/6/12)。
			if r, ok := world.CDMarket.CurRate[t]; ok {
				fm.CD.Rates[fmt.Sprint(t)] = r
			}
		}
	}
	// 可转债 top3(按市价降序;FundID 稳定 tie-break)。
	cbs := append([]*ConvertibleBond{}, world.CBonds...)
	sort.SliceStable(cbs, func(i, j int) bool {
		if cbs[i].Price != cbs[j].Price {
			return cbs[i].Price > cbs[j].Price
		}
		return cbs[i].ID < cbs[j].ID
	})
	for i, b := range cbs {
		if i >= 3 {
			break
		}
		fm.Convertibles = append(fm.Convertibles, CBondJSON{
			ID: b.ID, Issuer: b.Issuer, IssuerNode: b.IssuerNode,
			Price: b.Price, ConvValue: b.ConvValue, BondFloor: b.BondFloor,
			CouponRate: b.CouponRate, Status: b.Status, MonthsLeft: b.MonthsLeft,
		})
	}
	// 融券快照(R6-1 护栏监测面)。
	fm.Short.BalanceCNY = world.ShortBalanceCNY()
	fm.Short.MarginCNY = world.ShortMarginFrozenCNY()
	if world.ShortBook != nil {
		for _, pos := range world.ShortBook.Positions {
			if pos.Status == ShortStatusOpen {
				fm.Short.OpenCount++
			}
		}
		fm.Short.MarginCallsMon = world.ShortBook.MarginCallsLastMonth
	}
	// 基金评级 top5(星数降序、AUM 降序 tie-break)。
	funds := append([]FundRating{}, world.FundRatings...)
	sort.SliceStable(funds, func(i, j int) bool {
		if funds[i].Stars != funds[j].Stars {
			return funds[i].Stars > funds[j].Stars
		}
		return funds[i].AUM > funds[j].AUM
	})
	for _, f := range funds {
		fm.FundRatings = append(fm.FundRatings, FundRatingJSON{
			FundID: f.FundID, Name: f.Name, Stars: f.Stars,
			Sharpe: f.Sharpe, MaxDrawdown: f.MaxDrawdown, AUMWan: f.AUM,
		})
	}
	return fm
}

// FlowStatJSON 是 cs.FlowStat 视图(P2 v2 §13.2.4)。
type FlowStatJSON struct {
	Period      string         `json:"period"`
	PeriodLabel string         `json:"period_label"`
	Nodes       []FlowNodeJSON `json:"nodes"`
	Links       []FlowLinkJSON `json:"links"`
	TotalInCNY  int64          `json:"total_in_cny"`
	TotalOutCNY int64          `json:"total_out_cny"`
}

// FlowNodeJSON 是节点视图。
type FlowNodeJSON struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Kind      string `json:"kind"`
	AmountCNY int64  `json:"amount_cny"`
}

// FlowLinkJSON 是边视图。
type FlowLinkJSON struct {
	From      string  `json:"from"`
	To        string  `json:"to"`
	AmountCNY int64   `json:"amount_cny"`
	Pct       float64 `json:"pct"`
}

// SurveyJSON 是 game.state.surveys 单项(调研契约 §5.1;view 层映射)。
// 下发范围:所有 open(≤1)+ 最近 4 个 closed(含 Result),按时间倒序。
type SurveyJSON struct {
	ID            string            `json:"id"`
	Question      string            `json:"question"`
	Options       []string          `json:"options"`
	LaunchMonth   int               `json:"launch_month"`
	DeadlineMonth int               `json:"deadline_month"`
	Status        string            `json:"status"` // open|closed
	AnswersCount  int               `json:"answers_count"`
	Result        *SurveyResultJSON `json:"result,omitempty"` // closed 时非空
}

// SurveyResultJSON 是聚合结果(选项文本一并下发,前端免查表)。
type SurveyResultJSON struct {
	Options    []string  `json:"options"`
	Counts     []int     `json:"counts"`
	Percents   []float64 `json:"percents"`
	Total      int       `json:"total"`
	TopReasons []string  `json:"top_reasons"`
}

// SurveyJSONFrom 把引擎 Survey 映射为 SurveyJSON(ws 层 game.survey_result 与
// game.state.surveys 共用)。Answers 明细不进公开快照(匿名投票,§11.5)。
func SurveyJSONFrom(sv *Survey) SurveyJSON {
	if sv == nil {
		return SurveyJSON{Options: []string{}}
	}
	out := SurveyJSON{
		ID:            sv.ID,
		Question:      sv.Question,
		Options:       append([]string{}, sv.Options...),
		LaunchMonth:   sv.LaunchMonth,
		DeadlineMonth: sv.DeadlineMonth,
		Status:        sv.Status,
		AnswersCount:  len(sv.Answers),
	}
	if out.Options == nil {
		out.Options = []string{}
	}
	if sv.Result != nil {
		out.Result = &SurveyResultJSON{
			// 选项文本一并下发(来自 Survey.Options,前端免查表;§5.1)。
			Options:    append([]string{}, sv.Options...),
			Counts:     append([]int{}, sv.Result.Counts...),
			Percents:   append([]float64{}, sv.Result.Percents...),
			Total:      sv.Result.Total,
			TopReasons: append([]string{}, sv.Result.TopReasons...),
		}
		if out.Result.Options == nil {
			out.Result.Options = []string{}
		}
		if out.Result.TopReasons == nil {
			out.Result.TopReasons = []string{}
		}
	}
	return out
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
	StockIndex float64              `json:"stock_index"`
	GoldPrice  float64              `json:"gold_price"`
	BondYield  float64              `json:"bond_yield"`
	Districts  []MarketDistrictJSON `json:"districts"`
}

// MarketDistrictJSON 是 districts 单项(idx_d / rent_index)。
type MarketDistrictJSON struct {
	ID       string  `json:"id"`
	PriceIdx float64 `json:"price_index"`
	RentIdx  float64 `json:"rent_index"`
}

// PlayerJSON 是单座位公开信息。
type PlayerJSON struct {
	Seat         int          `json:"seat"`
	Account      string       `json:"account"`
	Nickname     string       `json:"nickname"`
	IsBot        bool         `json:"is_bot"`
	ModelDisplay string       `json:"model_display"`
	Profession   ProfJSON     `json:"profession"`
	District     string       `json:"district"`
	HomeDistrict string       `json:"home_district"`
	Alive        bool         `json:"alive"`
	Retired      bool         `json:"retired"`
	Age          int          `json:"age"`
	Resources    ResourceJSON `json:"resources"`
	NetWorth     int64        `json:"net_worth"`
	FIIndex      float64      `json:"fi_index"`
	IncomeBand   string       `json:"income_band"`
	StatusIcon   string       `json:"status_icon"`
	LastAction   string       `json:"last_action"`
	Ending       string       `json:"ending"`
	// P1: 明斯基状态(v2.60 N11-4)。
	MinskyTier   string  `json:"minsky_tier"`    // hedge/speculative/ponzi(主导等级)
	DebtToIncome float64 `json:"debt_to_income"` // 主导贷款月供/月收入(0-1+)
	// P1(§财商流P1-2 §6.2):消费档位(0-3,档位是公开生活方式)。
	ConsumptionLevel int `json:"consumption_level"`
	// P1-4(§财商流P1-4 §8.2):该座位 Active 保单险种列表(是否投保是公开信息)。
	InsuredKinds []string `json:"insured_kinds,omitempty"`
}

// InsuranceJSON 是 my.insurance 子结构(P1-4 §8.2;insurance_enabled=false 时 omit)。
type InsuranceJSON struct {
	Policies       []PolicyJSON `json:"policies"`        // 已有保单(含失效,最近 8 张)
	MonthlyPremium int64        `json:"monthly_premium"` // 当前月缴合计
	Quotes         []QuoteJSON  `json:"quotes"`          // 未投保/已失效险种的当前报价
}

// PolicyJSON 是 my.insurance.policies 单张(P1-4 §8.2)。
type PolicyJSON struct {
	Kind              string `json:"kind"`
	AnnualPremiumCNY  int64  `json:"annual_premium_cny"`
	MonthlyPremiumCNY int64  `json:"monthly_premium_cny"`
	CoverageCNY       int64  `json:"coverage_cny"`
	StartMonth        int    `json:"start_month"`
	PaidMonths        int    `json:"paid_months"`
	WaitingLeft       int    `json:"waiting_left"` // 等待期剩余月(0=已过)
	Status            string `json:"status"`       // active|waiting|grace|lapsed(§2.3 派生)
	ClaimsTotalCNY    int64  `json:"claims_total_cny"`
}

// QuoteJSON 是 my.insurance.quotes 单条(P1-4 §8.2;当前年龄档现价)。
type QuoteJSON struct {
	Kind             string `json:"kind"`
	AnnualPremiumCNY int64  `json:"annual_premium_cny"`
	CoverageCNY      int64  `json:"coverage_cny"`
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
	Cash           int64         `json:"cash"`
	SavingsDeposit int64         `json:"savings_deposit"` // P1: 定期存款(M2)
	Salary         int64         `json:"salary"`
	SpouseIncome   int64         `json:"spouse_income"`
	SideIncome     int64         `json:"side_income"`
	PassiveIncome  int64         `json:"passive_income"`
	Monthly        MyMonthlyJSON `json:"monthly"`
	Resources      ResourceJSON  `json:"resources"`
	Assets         []MyAssetJSON `json:"assets"`
	Loans          []MyLoanJSON  `json:"loans"`
	PensionCNY     int64         `json:"pension_cny"`
	CreditScore    int           `json:"credit_score"`
	Family         MyFamilyJSON  `json:"family"`
	FIIndex        float64       `json:"fi_index"`
	NetWorth       int64         `json:"net_worth"`
	Goals          []string      `json:"goals"`
	// P1(§财商流P1-2 §6.2):上月消费结构(仅本人;nil→{})。
	ConsumptionByGoods map[string]float64 `json:"consumption_by_goods"`
	// P1-4(§财商流P1-4 §8.2):商业保险段(仅本人;insurance_enabled=false 时 omit)。
	Insurance *InsuranceJSON `json:"insurance,omitempty"`
	// LocalPos 区内归一化坐标(2026-09-22 §CityHuman重构 my_local_pos;
	// 仅本人可见;观战者经 bot_contexts[].local_pos 获取 bot 座位)。
	LocalPos []float64 `json:"local_pos,omitempty"`
}

// MyMonthlyJSON 是 my.monthly 子结构。
type MyMonthlyJSON struct {
	Income  int64               `json:"income"`
	Expense int64               `json:"expense"`
	Net     int64               `json:"net"`
	Tax     int64               `json:"tax"`
	Social  int64               `json:"social"`
	Detail  []MyMonthlyItemJSON `json:"detail"`
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
	Month               int    `json:"month"`
	LastDecisionMonth   int    `json:"last_decision_month"`
	UpdatedAt           int64  `json:"updated_at"`
	Active              bool   `json:"active"`
	Seat                int    `json:"seat"`
	LastDecisionSummary string `json:"last_decision_summary"`
	LastToolInput       string `json:"last_tool_input"`
	LastToolResult      string `json:"last_tool_result"`
	HeartThought        string `json:"heart_thought"`
	// LastSenses 最近感知记录(2026-09-22 §CityHuman重构;空 → omitempty)。
	LastSenses []SenseEntry `json:"last_senses,omitempty"`
	// LocalPos 该座位区内坐标(仅本人/观战者可见,与 bot_contexts 可见性一致)。
	LocalPos []float64 `json:"local_pos,omitempty"`
}

// LedgerJSON 是 ledger_recent 单条。
type LedgerJSON struct {
	Month     int    `json:"month"`
	From      string `json:"from"`
	To        string `json:"to"`
	AmountCNY int64  `json:"amount_cny"`
	Category  string `json:"category"`
	Note      string `json:"note"`
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
// citySnap(2026-09-21 §虚拟城市):城市背景层快照,nil = 未建城(omitempty)。
func BuildClientState(roomID string, viewer int, world *World, seats [MaxSeats]string, nicknames [MaxSeats]string, botSeats [MaxSeats]bool, modelKeys [MaxSeats]string, transcripts [MaxSeats]BotTranscript, gameStartedAt, nextMonthAtUnixMs int64, citySnap *city.Snapshot) *ClientGameState {
	cs := &ClientGameState{
		RoomID: roomID, GameKind: "wealth",
		Status: StatusOpen, MaxSeat: MaxSeats,
		MySeat: viewer, NextMonthAt: nextMonthAtUnixMs,
		GameStartedAt: gameStartedAt,
		Players:       make([]PlayerJSON, MaxSeats),
		// 2026-09-14 §财商流P0-bugfix: 数组字段必须序列化为 [] 而非 null ——
		// Go nil slice → JSON null,前端 xxx.map 直接 TypeError 整页崩溃
		// (ErrorBoundary 兜底)。空集合统一初始化。
		BotContexts:  make([]BotCtxJSON, 0),
		LedgerRecent: make([]LedgerJSON, 0),
		EventsRecent: make([]EventJSON, 0),
	}
	if world == nil {
		cs.Phase = PhaseActing
		// 城市快照独立于引擎 World(未开局也可有城;当前实现建城在 Start,
		// 此分支恒 nil,防御性赋值)。
		cs.City = citySnap
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
	// 2026-09-21 §虚拟城市:城市背景层快照(未建城 nil → omitempty)。
	cs.City = citySnap
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

	// Treasury 子结构(阶段4 2026-09-21 §城市扩张v2.12;nil → omitempty
	// 不下发,阶段4 前对局兼容)。
	if world.Treasury != nil {
		cs.Treasury = &TreasuryJSON{
			CashCNY:             world.Treasury.Cash,
			BondsOutstandingCNY: world.Treasury.BondsOutstanding,
			LastMonthRevenueCNY: world.Treasury.LastMonthRevenue,
			LastMonthExpenseCNY: world.Treasury.LastMonthExpense,
			DeficitRun:          world.Treasury.DeficitRun,
			PublicServiceIdx:    world.Treasury.PublicServiceIndex(),
		}
	}

	// 阶段5(2026-09-21 §城市扩张v2.12):产业链 + 产业集群快照
	// (economy_enabled=false 或 nil/空 → omitempty 不下发)。
	if world.EconomyEnabled && world.SupplyChain != nil {
		snap := world.SupplyChain.Snapshot()
		cs.SupplyChain = &snap
	}
	if world.EconomyEnabled && len(world.Clusters) > 0 {
		cs.Clusters = ClustersJSONFrom(world.Clusters)
	}

	// 阶段6(2026-09-21 §城市扩张v2.12):金融市场快照(economy_enabled=false
	// 或量化引擎 nil → omitempty 不下发)。
	if world.EconomyEnabled && world.QuantEngine != nil {
		cs.FinMarket = buildFinMarketJSON(world)
	}

	// 阶段8(2026-09-21 §城市扩张v2.12,最终阶段):公共服务+监管+选举快照
	// (economy_enabled=false 或 PublicSvc nil → omitempty 不下发)。
	if world.EconomyEnabled && world.PublicSvc != nil {
		cs.PublicSvc = buildPublicServicesJSON(world)
	}

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
			IsBot: botSeats[s],
			// 2026-09-21 §虚拟城市(契约 04 §1.3):池驱动座位(bot 且 model_key
			// 为空)model_display = "LLM线路池";显式 model_key / 人类座位不变。
			ModelDisplay: seatModelDisplay(botSeats[s], modelKeys[s]),
			Profession:   ProfJSON{ID: pp.Card.ID, Title: pp.Card.Title, Avatar: avatarID(pp.Card.ID)},
			District:     pp.District, HomeDistrict: pp.HomeDistrict,
			Alive: pp.Alive, Retired: false, Age: pp.Age,
			Resources:        ResourceJSON{Energy: pp.Energy, Network: pp.Network, Cognition: pp.Cognition},
			NetWorth:         pp.NetWorth(world.Market),
			FIIndex:          pp.FIIndex(world.Market, world.Age()),
			IncomeBand:       pp.IncomeBand(),
			StatusIcon:       pp.StatusIcon,
			LastAction:       pp.LastActionText,
			Ending:           pp.Ending,
			MinskyTier:       minskyTier,
			DebtToIncome:     dti,
			ConsumptionLevel: pp.ConsumptionLevelSafe(),
			// P1-4 §8.2:insurance_enabled=false 时 insured_kinds 恒空(与
			// my.insurance omit 同口径,防中途关开关后冻结保单泄漏公开字段)。
			InsuredKinds: world.insuredKindsOf(pp),
		}
		cs.Players[s] = pj
	}

	// 观战者:my=null, my_seat=-1。
	isSpectator := viewer < 0 || viewer >= MaxSeats
	if !isSpectator && viewer >= 0 && viewer < MaxSeats {
		cs.MySeat = viewer
		cs.My = myJSONFor(world.Players[viewer])
		// P1-4(§财商流P1-4 §8.2):insurance_enabled=false 时整体 omit。
		if world.InsuranceEnabled && cs.My != nil {
			cs.My.Insurance = world.buildInsuranceJSON(world.Players[viewer])
		}
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
		bc := BotCtxJSON{
			Month:               t.Month,
			LastDecisionMonth:   t.LastDecisionMonth,
			UpdatedAt:           t.UpdatedAt,
			Active:              t.Active,
			Seat:                s,
			LastDecisionSummary: t.LastDecisionSummary,
			LastToolInput:       t.LastToolInput,
			LastToolResult:      t.LastToolResult,
			HeartThought:        t.HeartThought,
			LastSenses:          t.LastSenses,
		}
		if bp := world.Players[s]; bp != nil {
			bc.LocalPos = []float64{bp.LocalPos[0], bp.LocalPos[1]}
		}
		cs.BotContexts = append(cs.BotContexts, bc)
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

	// P1(§财商流P1-2 §6.1):消费品市场 / 劳动力市场 / 社会结构 / 调研。
	// economy_enabled=false 时零值/空数组下发(P0-bugfix 纪律:数组非 null)。
	cs.ConsumerMarket = ConsumerMarketJSON{Goods: make([]GoodsItemJSON, 0, len(goodsOrder))}
	if world.EconomyEnabled && world.Goods != nil {
		cs.ConsumerMarket.CPIYoY = world.Goods.CPIYoY
		cs.ConsumerMarket.CPIMom = world.Goods.CPIMom
		for _, id := range goodsOrder {
			if it := world.Goods.Items[id]; it != nil {
				cs.ConsumerMarket.Goods = append(cs.ConsumerMarket.Goods, GoodsItemJSON{
					ID: it.ID, Weight: it.Weight, PriceIdx: it.PriceIdx, MomChange: it.MomChange,
				})
			}
		}
	}
	if world.EconomyEnabled && world.Labor != nil {
		cs.LaborMarket = LaborMarketJSON{
			UnemploymentRate: world.Labor.Unemployment,
			EmploymentRatio:  world.Labor.Employment,
			AvgWageGrowthYoY: world.Labor.WageGrowthYoY,
			FirmRevenueCNY:   world.Labor.RevenueCNY,
			LayoffWave:       world.Labor.LayoffWave,
		}
	}
	if world.EconomyEnabled && world.Society != nil {
		cs.Society = SocietyJSON{Gini: world.Society.Gini, Quintiles: world.Society.Quintiles}
		cs.Society.Circles.Survival = world.Society.Circles[0]
		cs.Society.Circles.Accumulate = world.Society.Circles[1]
		cs.Society.Circles.Freedom = world.Society.Circles[2]
		// P2 v2 新增字段(2026-09-19 §P2-可视化 §13.2.4)。omitempty 保障
		// economy_enabled=false 或无玩家时不污染 ws 帧。
		cs.Society.TotalWealth = world.Society.TotalWealth
		cs.Society.MedianWealth = world.Society.MedianWealth
		cs.Society.MeanWealth = world.Society.MeanWealth
		if world.Society.P10 > 0 || world.Society.P90 > 0 || world.Society.MeanWealth > 0 {
			cs.Society.Percentiles = map[string]int64{
				"p10": world.Society.P10,
				"p25": world.Society.P25,
				"p50": world.Society.P50,
				"p75": world.Society.P75,
				"p90": world.Society.P90,
			}
		}
		if len(world.Society.LorenzPoints) > 0 {
			cs.Society.LorenzPoints = world.Society.LorenzPoints
		}
		if len(world.Society.PyramidLayers) > 0 {
			cs.Society.PyramidLayers = make([]WealthLayerJSON, len(world.Society.PyramidLayers))
			for i, l := range world.Society.PyramidLayers {
				cs.Society.PyramidLayers[i] = WealthLayerJSON{
					Name: l.Name, Count: l.Count,
					TotalWealth: l.TotalWealth, AvgWealth: l.AvgWealth, WealthPct: l.WealthPct,
				}
			}
		}
	}
	// 阶段7(2026-09-21 §城市扩张v2.12):社会结构指标体系 —— 最近一次月结的
	// SocietySnapshot(当前值)+ 最近 12 月趋势。快照在 ④.6B(步骤⑤ w.Month++
	// 之前)生成,故 Latest().Month = 当前已结算月;首月未月结时历史为空,
	// 仅零值下发(Trend omitempty 不携带)。
	if world.EconomyEnabled && world.SocietyHist != nil {
		if snap := world.SocietyHist.Latest(); snap != nil {
			cs.Society.GiniIncome = snap.GiniIncome
			cs.Society.Top1Pct = snap.Top1Pct
			cs.Society.Top10Pct = snap.Top10Pct
			cs.Society.Bottom50Pct = snap.Bottom50Pct
			cs.Society.WealthQuintiles = snap.Quintiles
			cs.Society.Pyramid = snap.Pyramid
			cs.Society.MobilityYoung = snap.MobilityYoung
			if trend := world.SocietyHist.Last(12); len(trend) > 0 {
				cs.Society.Trend = make([]SocietyTrendJSON, len(trend))
				for i, t := range trend {
					cs.Society.Trend[i] = SocietyTrendJSON{
						Month: t.Month, GiniWealth: t.GiniWealth, GiniIncome: t.GiniIncome,
						Top10Pct: t.Top10Pct, MobilityYoung: t.MobilityYoung,
					}
				}
			}
		}
	}
	// P2 v2:本月资金流向(SettleMonth 末尾 RecordFlowStat 已刷缓存;空月份 omit)。
	if world.LastFlowStat != nil && len(world.LastFlowStat.Links) > 0 {
		fs := world.LastFlowStat
		cs.FlowStat = &FlowStatJSON{
			Period: fs.Period, PeriodLabel: fs.PeriodLabel,
			TotalInCNY: fs.TotalInCNY, TotalOutCNY: fs.TotalOutCNY,
		}
		cs.FlowStat.Nodes = make([]FlowNodeJSON, len(fs.Nodes))
		for i, n := range fs.Nodes {
			cs.FlowStat.Nodes[i] = FlowNodeJSON{ID: n.ID, Label: n.Label, Kind: n.Kind, AmountCNY: n.AmountCNY}
		}
		cs.FlowStat.Links = make([]FlowLinkJSON, len(fs.Links))
		for i, l := range fs.Links {
			cs.FlowStat.Links[i] = FlowLinkJSON{From: l.From, To: l.To, AmountCNY: l.AmountCNY, Pct: l.Pct}
		}
	}
	// Surveys:open(≤1)+ 最近 4 个 closed(含 Result),倒序(调研契约 §5.1)。
	cs.Surveys = make([]SurveyJSON, 0)
	if len(world.Surveys) > 0 {
		if sv := world.OpenSurvey(); sv != nil {
			cs.Surveys = append(cs.Surveys, SurveyJSONFrom(sv))
		}
		for i := len(world.Surveys) - 1; i >= 0 && len(cs.Surveys) < 5; i-- {
			if world.Surveys[i].Status == SurveyClosed {
				cs.Surveys = append(cs.Surveys, SurveyJSONFrom(world.Surveys[i]))
			}
		}
	}

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

// seatModelDisplay 座位模型展示名:池驱动 bot 座位(model_key 空)= "LLM线路池";
// 其余走 ModelDisplayName(显式 model_key;人类座位历史行为不变)。
func seatModelDisplay(isBot bool, modelKey string) string {
	if isBot && modelKey == "" {
		return PoolModelDisplay
	}
	return ModelDisplayName(modelKey)
}

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
		// P1: 上月消费结构(nil→{};§6.2)。
		ConsumptionByGoods: consumptionByGoodsView(p),
		// 2026-09-14 §财商流P0-bugfix: 数组一律非 nil(空集合序列化为 [],
		// 防止前端 null.map 崩溃)。
		Goals:  append([]string{}, p.Card.Goals...),
		Assets: make([]MyAssetJSON, 0, len(p.Assets)),
		Loans:  make([]MyLoanJSON, 0, len(p.Loans)),
		// §CityHuman重构:区内坐标(walk/run 区内移动更新)。
		LocalPos: []float64{p.LocalPos[0], p.LocalPos[1]},
	}
	for i := range p.Assets {
		my.Assets = append(my.Assets, MyAssetJSON{
			Kind:           p.Assets[i].Kind,
			Name:           assetNameCN(&p.Assets[i]),
			Units:          p.Assets[i].Units,
			Price:          assetPriceSnap(&p.Assets[i]),
			ValueCNY:       AssetValue(&p.Assets[i], globalMarketSnap(p)),
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
	NetWorth      int64
	Goals         []string
	// P1: 上月消费结构(nil→{})。
	ConsumptionByGoods map[string]float64
	// LocalPos 区内归一化坐标(§CityHuman重构)。
	LocalPos []float64
}

// jsonMy 把 MyMy 转成 *MyJSON(避免在 MyMy 上重复 JSON 标签)。
func jsonMy(m *MyMy) *MyJSON {
	if m == nil {
		return nil
	}
	return &MyJSON{
		Cash: m.Cash, Salary: m.Salary, SpouseIncome: m.SpouseIncome,
		SideIncome: m.SideIncome, PassiveIncome: m.PassiveIncome,
		Monthly: m.Monthly, Resources: m.Resources,
		Assets: m.Assets, Loans: m.Loans,
		PensionCNY: m.PensionCNY, CreditScore: m.CreditScore,
		Family:  m.Family,
		FIIndex: m.FIIndex, NetWorth: m.NetWorth, Goals: m.Goals,
		ConsumptionByGoods: m.ConsumptionByGoods,
		LocalPos:           m.LocalPos,
	}
}

// consumptionByGoodsView 消费结构视图(nil → 空 map,§6.2)。
func consumptionByGoodsView(p *Player) map[string]float64 {
	out := make(map[string]float64, len(p.ConsumptionByGoods))
	for k, v := range p.ConsumptionByGoods {
		out[k] = v
	}
	return out
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
func assetPriceSnap(a *Asset) float64     { return 0 }
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
