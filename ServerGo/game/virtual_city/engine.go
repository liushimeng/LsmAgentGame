// Package virtual_city — engine.go: 纯引擎装配与月推进原语(2026-09-14 §财商流P0)。
//
// World 是无锁纯状态:所有变更经显式入参(含 *rand.Rand)驱动 → 可确定性单测
// (后端架构 §1 分层)。房间层(room.go)负责锁 + 定时 + 广播。
package virtual_city

import (
	"math"
	"math/rand"

	"LsmAgentGame/game/virtual_city/profession"
)

// 对局状态。
const (
	StatusOpen    = "open"
	StatusPlaying = "playing"
	StatusOver    = "over"
)

// Phase 月窗口阶段。
const (
	PhaseActing   = "acting"
	PhaseSettling = "settling"
)

// MaxSeats UI 展示位数 = 月抽样数(2026-09-16 §12 座扩容:8 → 12;
// 2026-09-24 重构:12 = 当月从 Backdrop 居民中随机均匀抽样展示的代表数,
// 无固定/特权/抽样层)。
const MaxSeats = 12

// MinSeats 最少开局展示位数(2026-09-16 §12 座扩容:3 → 10;
// 2026-09-24 重构:10 = 最少 10 个抽样展示居民即可开局,无人类玩家概念)。
// 2026-10-08 §背景居民下限降至1:本常量自此**不再直接充当门槛**,只作为
// 房间级 minSeats 字段的「默认值 + 上限」—— minSeats =
// min(MinSeats, clamp(resident_count, 1, MaxSeats));居民数 ≥ 10 时门槛仍是 10
// (行为逐字节不变),< 10 的小城门槛为 N。见 room.go::EffectiveMinSeats。
const MinSeats = 10

// DefaultAgentConcurrency 是房间级 LLM 并发信号量(agentSem)的默认容量
// (2026-09-16 §12 座扩容 新定)。
//
// 设计取舍:
//   - 狼人杀 / 德扑的 bot 是「轮到才动」,4 并发够用;虚拟城市 12 个抽样居民每月
//     同时**被 LLM 驱动 → 4 并发会把 12 人压成 4 批串行,月窗口(默认 8s)内
//     后几批根本来不及跑 → 「10+ Agent 跑全场」等于 4 个在跑、其余被强制
//     submit。放宽到 8(≈ MaxSeats 的 2/3)后,12 人分 2 批,配合月窗口上限
//     30s 与 decisionTimeout 20s,足够全部抽样居民完成一轮 LLM 驱动。
//   - 不设成 MaxSeats(12):LLM Provider 自身有全局并发/配额上限,12 路并发
//     容易触发 429 被 quarantine;8 是 Provider 配额与房间并发间的平衡。
//   - NewVirtualCityRoom 的 llmConcurrency 参数=0 时回落此默认;管理器可按需覆盖。
const DefaultAgentConcurrency = 8

// DefaultAgentConcurrencyFor 根据 maxSeats + 线路池容量返回动态并发数
// (2026-09-21 §城市扩张v2.12)。
//
// 输入:
//   - maxSeats 房容量(12 默认);≤0 或 >MaxSeats 时回落 MaxSeats。
//   - poolTotal llm.LinePool 总线路数;≤0 时回落 DefaultAgentConcurrency。
//
// 输出: ∈ [4, 64];目标值 = maxSeats/2+1,再 cap 到 poolTotal 与 64。
//
// 设计动机:12 抽样居民 + 8 线路 → 7;200 背景居民 + 64 线路 → 64。
// 典型值:12 焦点玩家 + 8 线路 → 7;200 背景居民 + 64 线路 → 64。
// 向后兼容:老客户端 poolTotal 传入 0 时回落 DefaultAgentConcurrency,
// 与 NewVirtualCityRoom 既有行为一致,客户无感。
func DefaultAgentConcurrencyFor(maxSeats, poolTotal int) int {
	if maxSeats <= 0 || maxSeats > MaxSeats {
		maxSeats = MaxSeats
	}
	if poolTotal <= 0 {
		poolTotal = DefaultAgentConcurrency
	}
	target := maxSeats/2 + 1
	if target > poolTotal {
		target = poolTotal
	}
	if target > 64 {
		target = 64
	}
	if target < 4 {
		target = 4
	}
	return target
}

// MonthWindowFor 根据 maxSeats + LLM 并发返回月窗毫秒数
// (2026-09-21 §城市扩张v2.12)。
//
// 输入:
//   - maxSeats 房容量;≤0 回落 MaxSeats。
//   - llmConcurrency 信号量;≤0 回落 DefaultAgentConcurrency。
//
// 输出: ∈ [3000ms, 60000ms]。
// 公式: totalMs = batches*4000ms + 20000ms(决策超时) + 4000ms(余量),
// batches = ceil(maxSeats / llmConcurrency),clamp [3s, 60s]。
//
// 设计动机:月窗必须 ≥ 所有 Agent 完成一轮决策的总耗时,否则末批 Agent
// 会被强制 submit。向后兼容:llmConcurrency=0 时回落默认公式,老房间行为不变。
func MonthWindowFor(maxSeats, llmConcurrency int) int {
	if llmConcurrency <= 0 {
		llmConcurrency = DefaultAgentConcurrency
	}
	if maxSeats <= 0 {
		maxSeats = MaxSeats
	}
	batches := (maxSeats + llmConcurrency - 1) / llmConcurrency
	totalMs := batches*4000 + 20000 + 4000
	if totalMs < 3000 {
		totalMs = 3000
	}
	if totalMs > 60000 {
		totalMs = 60000
	}
	return totalMs
}

// MasterStartAge 主时钟开局基准年龄(精选手卡恒 25;文档池混龄卡仅展示个人
// age,主时钟统一 25 起 — 验收清单 B1 设计取舍)。
const MasterStartAge = 25

// TerminalAge 终局年龄(《规则》§14.1:60 岁强制结算)。
const TerminalAge = 60

// 终局结局 id(后端架构 §12)。
const (
	EndingWinner    = "winner"      // 财务自由·人生赢家(≥85)
	EndingAffluent  = "affluent"    // 富足安稳(70–84)
	EndingOrdinary  = "ordinary"    // 平淡度日(50–69)
	EndingIndebted  = "indebted"    // 债务缠身(30–49)
	EndingBankrupt  = "bankrupt"    // 破产出局(<30)
	EndingLonelyRic = "lonely_rich" // 孤独富翁(FI≥1.5 且人生满意度<40)
	// P1-4: 意外身故结局(2026-09-19 §财商流P1-4 §5.3;HandleDeath 唯一登记口)。
	EndingAccidentDeath = "accident_death" // 意外身故(寿险/意外险赔付入遗产池)
)

// EventRecord 单条事件(10.3 事件播报;view 层映射 game.event / events_recent)。
type EventRecord struct {
	Month int
	Type  string // action|move|settle|market|life|chat|error
	Seat  int    // -1 = 全房事件
	Text  string
}

// World 虚拟城市纯引擎世界状态。
type World struct {
	Month  int // 1..420(主钟)
	Status string

	Market *MarketState
	Ledger *Ledger
	CB     *CentralBankState // 央行-商业银行体系(P1,nil 时回退 P0 硬编码)
	// Treasury 政府财政国库(阶段4,2026-09-21 §城市扩张v2.12;nil 时
	// SettleMonth 跳过财政月结)。税收汇总/转移支付/财政支出/国债簿记,
	// 见 treasury.go;与 CB 严格隔离(R4-1 禁止央行透支)。
	Treasury *TreasuryState

	// P1: 真实经济循环(2026-09-16 §财商流P1-2)。NewWorld 恒置
	// EconomyEnabled=true;房间层 Start 时按配置回写(§6.5 回退开关)。
	EconomyEnabled bool
	Goods          *GoodsMarket  // 八大类消费篮子 + 内生 CPI(nil 惰性初始化)
	Labor          *FirmSector   // 企业部门/劳动力市场(nil 惰性初始化)
	Society        *SocietyStats // 社会结构统计(月度缓存,view 直读)
	// SocietyHist 社会结构指标 24 月环形历史(阶段7,2026-09-21 §城市扩张v2.12;
	// settlement ④.6B Push,view 下发当前快照 + 最近 12 月趋势;nil 时接线处
	// 惰性初始化)。零 rand —— 固定种子存量对局回归零偏移。
	SocietyHist *SocietyHistory

	// 阶段5(2026-09-21 §城市扩张v2.12):企业产业链 + 产业集群(15 虚拟
	// 节点 + 6 集群;nil/空时 SettleMonth ②B/⑨F 跳过)。见 supply_chain.go /
	// industrial_cluster.go。零 rand —— 固定种子存量对局回归零偏移。
	SupplyChain *SupplyChain         // 三条产业链网络(月度 ②B 调度)
	Clusters    []*IndustrialCluster // 6 产业集群(月度 ⑨F 调度)

	// 阶段6(2026-09-21 §城市扩张v2.12):金融市场扩展(基金评级 + 同业存单
	// + 可转债 + 融券做空 + 量化引擎;nil/空时 SettleMonth ⑥B 惰性初始化)。
	// 见 fund_rating.go / interbank_cd.go / convertible_bond.go /
	// short_selling.go / quant_fund.go。零 rand —— 固定种子存量对局回归零偏移。
	FundRatings []FundRating       // 5 只虚拟基金评级(月度 ⑥B 末位刷新)
	CDMarket    *CDMarket          // 同业存单市场(3 行 × 4 档期限)
	CBonds      []*ConvertibleBond // 可转债池(恒 5 只,退出滚动补发)
	ShortBook   *ShortBook         // 融券台账(R6-1 双护栏)
	QuantEngine *QuantFundEngine   // 量化基金引擎(R6-2 自适应权重)

	// 阶段8(2026-09-21 §城市扩张v2.12,最终阶段):公共服务 + 监管体系 +
	// 市长选举。见 public_services.go / civic_election.go / regulators/ 子包。
	// 零 rand —— 固定种子存量对局回归零偏移;nil 时 settlement ⑨G 惰性初始化。
	PublicSvc  *PublicServices  // 公共服务五件套(月度 ⑨G)
	Regulators *RegulatorBundle // 证监会/反垄断/消协/隐私四监管(月度 ⑨G)
	Election   *CivicElection   // 市长选举(R8-2 默认关闭 → no-op)

	// P1: 社会调研系统(§财商流P1-2 调研契约 §2)。
	Surveys   []*Survey // 全房调研(≤20,按发起序)
	SurveySeq int       // id 自增序列

	// 2026-10-21 §场景推演:条件触发者注入的场景(预测运行)。见 scenario.go。
	//   - Scenarios 进行中(≤8),SettleMonth ②.0 逐月衰减;
	//   - ScenarioApplied 注入记录环形缓冲(≤20,view 层 scenarios_recent);
	// 零 rand —— 固定种子存量模拟回归零偏移。
	Scenarios       []*ScenarioActive
	ScenarioApplied []ScenarioAppliedRecord
	ScenarioSeq     int

	// 批次68(2026-10-21 §预测运行):预测运行状态(基线快照 + 逐月轨迹 +
	// 最近 3 个 run 环形缓冲;见 prediction.go)。惰性初始化(nil 安全),
	// 零 rand —— 固定种子存量模拟回归零偏移。
	Prediction *PredictionState

	// ── 批次80(2026-10-23 §真实世界事件自动注入引擎)──
	// 真实世界数据 → 世界事件 → 场景注入 的事件层/反馈层状态。
	//   - WorldEventLog 事件日志环(≤50,新→旧;含未注入事件);
	//   - WorldEventSeen 幂等指纹集合(同一事件只处理一次);
	//   - WorldEventAutoEnabled/MaxPerMonth/MaxMagnitude 房间级安全阀配置
	//     (由房间层 Start 回写;见 room_world_event.go);
	//   - WorldEventAutoMonth/Count/Kinds 本月安全阀计数器(与主钟月同步,
	//     跨月自动归零 ⇒「每月 ≤N 条」「同 kind 每月至多 1 次」按月独立生效)。
	// 零 rand —— 不参与 StateHash(仅审计留痕与看板展示)。
	WorldEventLog          []WorldEventRecord
	WorldEventSeen         map[string]bool
	WorldEventAutoEnabled  bool
	WorldEventMaxPerMonth  int
	WorldEventMaxMagnitude int
	// WorldEventAutoMonth 本月安全阀计数器所属月份(= 触发注入时的主钟月)。
	WorldEventAutoMonth int
	// WorldEventAutoCount 该月已自动注入条数。
	WorldEventAutoCount int
	// WorldEventAutoKinds 该月已自动注入的 scenario_kind 集合。
	WorldEventAutoKinds map[string]bool

	// ── 批次71(2026-10-21 §校准与验证框架)──
	// OffLedger 账外调整登记簿(不经 Ledger 的现金变动登记;见 offledger.go)。
	// NewWorld 恒置;惰性初始化 nil 安全。
	OffLedger *OffLedgerBook
	// Audits 逐月审计快照环形缓冲(cap 24;见 calibration.go)。
	// 写入点 = SettleMonth 末尾(w.Month++ 之前)。零 rand。
	Audits []MonthlyAudit
	// Calibration 运行级校准状态(内存版 RunRecord;写盘属批次 72)。
	// 惰性初始化 nil 安全。
	Calibration *CalibrationState

	// P1-4: 商业保险与风险转移引擎(2026-09-19 §财商流P1-4 §11)。NewWorld 恒置
	// true;房间层 Start 时按配置回写(false 时投保/退保 35041、月结不扣缴、
	// 意外事件不掷骰 —— rand 序列零偏移,固定种子存量对局回归一致)。
	InsuranceEnabled bool

	// 批次52(§5):代际财富转移引擎开关 family_enabled。NewWorld 恒置 true;
	// false 时月结零接线 + 家庭工具拒绝(35045)+ 遗产不分配(回滚阀,§5)。
	FamilyEnabled bool

	// ── 批次74(2026-10-22 §P0经济平衡v2第一步「只救命」)──
	// BalanceV2Enabled 总开关(NewWorld 默认 true;false = 批次73 旧口径逐字段
	// 一致 —— 初始现金/基础租/节俭档/失业救济/hazard 全部走旧公式)。
	// 五项杠杆的常量与纯函数见 balance_v2.go;MCOverride 可成对 A/B。
	BalanceV2Enabled bool
	// BasicRentRate 基础租占家庭常规月收入比(默认 0.25;敏感度 0.20/0.30)。
	BasicRentRate float64
	// StartCashMonths 生效初始现金 = 生存核心 × 月数(默认 6;敏感度 3/9)。
	StartCashMonths int
	// FrugalMonths 新手节俭档强制月数(默认 6;敏感度 3/12)。
	FrugalMonths int

	// InheritanceLog 遗产分配日志(批次52 §4 裁决 D15:最近 5 条;ToSeats 恒空 =
	// 板外继承,裁决 D6/D7)。view 层降序下发。
	InheritanceLog []InheritanceEvent

	Players [MaxSeats]*Player // 空座 nil

	Rand *rand.Rand

	// ── 批次73 O2/O1(校准采集,纯旁路;零 rand,不参与 StateHash)──
	// MCEvents 事件级采集环形缓冲(cap mcEventCap;recordEvent 写入,
	// 满则丢最旧 + EventDropped 计数)。与 World.Events(人生事件播报)
	// 完全隔离。
	MCEvents    []MCEvent
	EventDropped int
	// MCAuctionCount 本局 StartAuction 总场次(居民级 auction_participation_rate
	// 分母;O1)。
	MCAuctionCount int

	// Seed 本局随机种子(批次71:RunRecord.Seed / 场景注入记录的 seed 字段
	// 取此值;seed=0 传入时 NewWorld 派生实际种子并回写)。
	// ⚠ 只读字段,绝不参与 rand 消费。
	Seed int64

	Events []EventRecord // 全量事件(增长缓慢:每月 ≈ 1–3 条)

	startAge int

	// P1: 明斯基金融不稳定引擎(v2.60 N11-5)。
	MinskyMomentCooldown int // 明斯基时刻冷却剩余月(触发后置 12)
	MinskyMomentCount    int // 累计触发次数(展示/评分用)

	// P2: 玩家间交易与财富流动系统(2026-09-16 §财商流P2)。
	ListingBook  *ListingBook  // 挂单簿 + 议价 + P2P 借贷合约
	AuctionHouse *AuctionHouse // 拍卖行(四种拍卖)

	// P2 v2: 资金流向缓存(2026-09-19 §财商流P2-可视化)。SettleMonth ⑨ 之后
	// 调用 RecordFlowStat 刷新,只保留本月。
	LastFlowStat *FlowStat

	// ── 批次62 行为金融与泡沫反身性引擎(2026-10-20)──
	// 泡沫状态机(与 Market.CyclePhase 正交):none|overheat|burst|recession。
	BubbleState            BubbleState
	BubbleMonths           int     // 当前状态已持续月数
	BubbleRecessionMonths  int     // 泡沫后衰退期的锁定月数(2~4,确定性)
	BubbleLeverage         float64 // 市场杠杆率(总负债/总净资产)
	BubbleTurnover         float64 // 资产换手率
	BubbleCash             float64 // 现金占比
	BubbleMessage          string  // 最近一次破裂播报(§11.3)
	BubbleBurstMonth       int     // 最近一次破裂发生的月份(爆仓判定用)
	BubbleRateShiftApplied bool    // 泡沫利率位移是否已施加(每周期只一次)

	// 羊群效应(§3.4):回合操作摘要历史(最近 12 回合)。
	TurnSummaries []TurnSummary
	// 本回合累计:买入 / 卖出 / 观望人数(回合末 closeTurnSummary 归零)。
	TurnBuyCount  int
	TurnSellCount int
	TurnHoldCount int
	// 本回合成交资产笔数(资产换手率分子)。
	TurnTradeCount int
}

// NewWorld 构造世界(seed=0 时用时间随机;cards[seat] 可为零值 Card 表示空座)。
func NewWorld(seed int64, cards [MaxSeats]profession.Card) *World {
	// 批次71:回写实际种子(供 RunRecord.Seed / 场景注入记录 seed 使用);
	// seed=0 时派生一个实际种子(行为与旧实现逐字节一致)。
	seedVal := seed
	if seedVal == 0 {
		seedVal = rand.Int63()
	}
	rng := rand.New(rand.NewSource(seedVal))
	w := &World{
		Month:  1,
		Status: StatusOpen,
		Market: NewMarket(rng),
		Ledger: &Ledger{},
		CB:     NewCentralBank(),
		// 阶段4: 政府财政国库(初始现金 500 万;2026-09-21 §城市扩张v2.12)。
		Treasury: NewTreasury(),
		Rand:     rng,
		startAge: MasterStartAge,
		// P1: 真实经济循环恒开启(§6.5);economy_enabled=false 由房间层
		// Start 时回写(NewManager 归一后传入)。
		EconomyEnabled: true,
		// P1-4: 保险引擎默认开启(§11;SetInsuranceEnabled 可覆盖)。
		InsuranceEnabled: true,
		// 批次52(§5):代际财富转移引擎默认开启(family_enabled;false 回滚阀)。
		FamilyEnabled: true,
		// 批次74:P0 经济平衡 v2 默认开启(常量口径;false = 批次73 旧口径)。
		BalanceV2Enabled: true,
		BasicRentRate:    BasicRentDefaultRate,
		StartCashMonths:  StartCashMonthsDefault,
		FrugalMonths:     FrugalMonthsDefault,
		Goods:            NewGoodsMarket(),
		Labor:            NewFirmSector(),
		// 阶段7: 社会结构指标 24 月环形历史(2026-09-21 §城市扩张v2.12)。
		SocietyHist: &SocietyHistory{},
		// 阶段5: 产业链 15 节点 + 产业集群 6 集群(2026-09-21 §城市扩张v2.12)。
		SupplyChain: NewSupplyChain(),
		Clusters:    NewIndustrialClusters(),
		// 阶段6: 金融市场扩展(2026-09-21 §城市扩张v2.12;评级首月在 ⑥B 播种)。
		FundRatings: nil, // rateFundsMonthly 空表惰性初始化(锚当期股指)。
		CDMarket:    NewCDMarket(),
		CBonds:      nil, // stepConvertibleBonds 空池惰性初始化(锚当期股指)。
		ShortBook:   NewShortBook(),
		QuantEngine: NewQuantFundEngine(),
		// 阶段8: 公共服务 + 监管 + 市长选举(2026-09-21 §城市扩张v2.12;
		// 选举默认关闭 → MonthlyStep no-op,零偏移)。
		PublicSvc:  NewPublicServices(),
		Regulators: NewRegulatorBundle(),
		Election:   NewCivicElection(),
		// P2: 玩家间交易与财富流动系统(2026-09-16 §财商流P2)。
		ListingBook:  NewListingBook(),
		AuctionHouse: NewAuctionHouse(),
		// 批次62:泡沫状态机初始为"常态"(零值是空串,view 快照要显式 none)。
		BubbleState: BubbleNone,
		// 批次71:账外调整登记簿恒置(惰性初始化兜底见 offLedgerEnsure)。
		OffLedger: &OffLedgerBook{},
		// 批次71:本局实际随机种子(seed=0 时为派生值)。
		Seed: seedVal,
	}
	for seat := 0; seat < MaxSeats; seat++ {
		if cards[seat].ID == "" {
			continue
		}
		w.Players[seat] = newPlayerFromCard(seat, cards[seat])
	}
	// 批次62:偏差画像分配(确定性;按 seat 派生,同 seat 每次对局一致)。
	w.assignBiasProfiles()
	// 阶段6: CD 票面基准锚当前 SHIBOR(首次 ⑥B 之前 view/申购即可用)。
	w.CDMarket.refreshRates(w)
	return w
}

// PlaceholderWorld 返回一个最小可用的 World 视图（房间尚未 Start 时用于
// game.state 轮询返回：保证 Market 非 nil、Players/ledger 为零值，build
// 客户端快照时不触发 nil 解引用；不参与任何真实结算）。
func PlaceholderWorld(seed int64) *World {
	return NewWorld(seed, [MaxSeats]profession.Card{})
}

// newPlayerFromCard 按职业卡初始化单座位(初始注入 world→seat = Savings,I2)。
func newPlayerFromCard(seat int, card profession.Card) *Player {
	p := &Player{
		Seat:         seat,
		Card:         card,
		Age:          card.StartAge,
		Cash:         card.Savings,
		Energy:       card.Energy,
		Network:      card.Network,
		Cognition:    card.Cognition,
		CreditScore:  card.CreditScore,
		District:     card.HomeDistrict,
		HomeDistrict: card.HomeDistrict,
		Family:       Family{Marital: card.Marital, Children: card.ChildrenCount},
		SalaryBase:   card.Salary,
		Alive:        true,
		StatusIcon:   "idle",
		MinskyByLoan: map[string]*MinskyStatus{},
		// P1: 消费档位默认 1(标准)。⚠️ 零值陷阱:0 是合法档位(节俭),
		// 必须显式置 1;存量玩家以 ConsumptionByGoods==nil 判未初始化(§3.1)。
		ConsumptionLevel: 1,
	}
	if card.Marital == "" {
		p.Family.Marital = "single"
	}
	if p.Family.Children == 0 {
		p.Family.Children = card.ChildrenCount
	}
	return p
}

// StartGame 开局:状态切 playing + 初始注入 Ledger(world→seat,金额见下,I2)。
// 由房间层在发卡完成后调用一次。
//
// 批次74:BalanceV2Enabled 时注入金额 = **生效初始现金**
// (ceil_1000(max(card.Savings, StartCashMonths × SurvivalCore)),方案 §3.2),
// 同时写 p.Card.Savings 与 p.Cash;高储蓄卡只向上取整不削减(保留阶层差异)。
// false ⇒ 与批次73 逐字段一致(金额 = 卡面 Savings)。
func (w *World) StartGame() {
	w.Status = StatusPlaying
	for seat, p := range w.Players {
		if p == nil {
			continue
		}
		p.ActionBudget = monthlyActionBudget
		p.Submitted = false
		p.SpeakCountThisMonth = 0
		injection := p.Card.Savings
		if w.BalanceV2Enabled {
			injection = w.effectiveStartCash(p)
			p.Card.Savings = injection
			p.Cash = injection
		}
		if injection > 0 {
			// 直接 Record(不走 Pay:注入即初始现金,不产生增量)。
			w.Ledger.Record(0, EntityWorld, SeatEntity(seat), injection, CatInject,
				"初始储蓄注入 "+p.Card.ID)
		}
	}
	// 批次72 §6.2:品类年化期初基线捕获(幂等,零 rand,不加任何账目)。
	w.calibrationCaptureMarketStart()
}

// Age 主时钟年龄 = 开局基准 + floor((month-1)/12)。
func (w *World) Age() int {
	if w.Month < 1 {
		w.Month = 1
	}
	return w.startAge + (w.Month-1)/12
}

// Occupied 已入座(存活或未出局)人数。
func (w *World) Occupied() int {
	n := 0
	for _, p := range w.Players {
		if p != nil {
			n++
		}
	}
	return n
}

// alivePlayers 存活玩家座位列表。
func (w *World) alivePlayers() []int {
	var out []int
	for i, p := range w.Players {
		if p != nil && p.Alive {
			out = append(out, i)
		}
	}
	return out
}

// Pay 引擎统一收付通道:同记账同扣款(I1 守恒由构造保证)。
// from/to 必须有一端是座位;金额恒正。返回参与座位的新现金。
func (w *World) Pay(seat int, from, to string, amount int64, category, note string) {
	if amount == 0 {
		return
	}
	w.Ledger.Record(w.Month, from, to, amount, category, note)
	id := SeatEntity(seat)
	if to == id {
		w.Players[seat].Cash += amount
	} else if from == id {
		w.Players[seat].Cash -= amount
	}
}

// emitEvent 追加事件记录。
func (w *World) emitEvent(evType string, seat int, text string) {
	w.Events = append(w.Events, EventRecord{Month: w.Month, Type: evType, Seat: seat, Text: text})
}

// RecentEvents 最近 n 条事件(view / GameContext 共用)。
func (w *World) RecentEvents(n int) []EventRecord {
	if len(w.Events) <= n {
		out := make([]EventRecord, len(w.Events))
		copy(out, w.Events)
		return out
	}
	out := make([]EventRecord, n)
	copy(out, w.Events[len(w.Events)-n:])
	return out
}

// ─────────────────── 税务 / 金融纯函数(§8 / §7.3) ───────────────────

// taxBrackets 个税 7 级累进(月度,《规则》§9.1 速算扣除数照抄)。
var taxBrackets = []struct {
	Upper     float64 // 月应纳税所得额上界(元)
	Rate      float64
	QuickDedc float64
}{
	{3000, 0.03, 0},
	{12000, 0.10, 210},
	{25000, 0.20, 1410},
	{35000, 0.25, 2660},
	{55000, 0.30, 4410},
	{80000, 0.35, 7160},
	{math.MaxFloat64, 0.45, 15160},
}

// MonthlyIncomeTax 个税 = 应纳税所得额 × 税率 − 速算扣除数(<0 记 0)。
// 应纳税所得额 = 税前月工资 − 5000(起征) − 社保(工资×10.5%)。
func MonthlyIncomeTax(salary int64) int64 {
	if salary <= 0 {
		return 0
	}
	social := float64(salary) * 0.105
	taxable := float64(salary) - 5000 - social
	if taxable <= 0 {
		return 0
	}
	for _, b := range taxBrackets {
		if taxable <= b.Upper {
			tax := taxable*b.Rate - b.QuickDedc
			if tax < 0 {
				return 0
			}
			return int64(tax + 0.5)
		}
	}
	return 0
}

// SocialSecurity 个人社保缴纳 = 工资 × 10.5%(养老 8% 入养老金账户 + 医疗 2% + 失业 0.5%)。
func SocialSecurity(salary int64) int64 {
	if salary <= 0 {
		return 0
	}
	return int64(float64(salary)*0.105 + 0.5)
}

// PensionContribution 养老金个人账户月缴存 = 工资 × 8%。
func PensionContribution(salary int64) int64 {
	if salary <= 0 {
		return 0
	}
	return int64(float64(salary)*0.08 + 0.5)
}

// AnnuityPayment 等额本息月供(《规则》§8.3):
// 月供 = P × r × (1+r)^n / [(1+r)^n − 1];r = 月利率。
func AnnuityPayment(principal int64, annualRate float64, n int) int64 {
	if principal <= 0 || n <= 0 {
		return 0
	}
	r := annualRate / 12
	if r <= 0 {
		return principal / int64(n)
	}
	f := math.Pow(1+r, float64(n))
	pay := float64(principal) * r * f / (f - 1)
	if pay < 1 {
		pay = 1
	}
	return int64(pay + 0.5)
}

// InflationFactor 通胀因子 = 1.05^(游戏年数)(§9.2,恒定 5% 取舍)。
// P0 回退:World.CB 为 nil 时使用(向前兼容)。
func InflationFactor(month int) float64 {
	years := (month - 1) / 12
	if years < 0 {
		years = 0
	}
	return math.Pow(1.05, float64(years))
}

// InflationFactorCB 内生通胀因子 = (1 + max(CPI, 0.02))^年数(P1)。
// CPI 来自 World.CB;CB 为 nil 时回退 P0 硬编码 1.05^年数。
func InflationFactorCB(w *World) float64 {
	if w == nil || w.CB == nil {
		return InflationFactor(w.Month)
	}
	cpi := w.CB.CPI
	if cpi < 0.02 {
		cpi = 0.02 // 保底 2%,避免通缩时生活支出为 0
	}
	years := (w.Month - 1) / 12
	if years < 0 {
		years = 0
	}
	return math.Pow(1+cpi, float64(years))
}
