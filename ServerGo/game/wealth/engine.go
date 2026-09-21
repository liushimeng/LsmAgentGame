// Package wealth — engine.go: 纯引擎装配与月推进原语(2026-09-14 §财商流P0)。
//
// World 是无锁纯状态:所有变更经显式入参(含 *rand.Rand)驱动 → 可确定性单测
// (后端架构 §1 分层)。房间层(room.go)负责锁 + 定时 + 广播。
package wealth

import (
	"math"
	"math/rand"

	"LsmAgentGame/game/wealth/profession"
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

// MaxSeats 房间容量(2026-09-16 §12 座扩容:8 → 12,给「1 人类 + 11 bot」
// 留头寸;同时也是精选卡池 / 文档池抽卡张数的硬下限)。
const MaxSeats = 12

// MinSeats 最少开局座位(2026-09-16 §12 座扩容:3 → 10,即最少 10 个 Agent
// 可跑全场;与 MaxSeats 之间保留 2 个头寸给人类玩家)。
const MinSeats = 10

// DefaultAgentConcurrency 是房间级 LLM 并发信号量(agentSem)的默认容量
// (2026-09-16 §12 座扩容 新定)。
//
// 设计取舍:
//   - 狼人杀 / 德扑的 bot 是「轮到才动」,4 并发够用;虚拟城市 12 个 bot 每
//     个月**同时**决策 → 4 并发会把 12 人压成 4 批串行,月窗口(默认 8s)内
//     后几批根本来不及跑 → 「10+ Agent 跑全场」等于 4 个在跑、其余被强制
//     submit。放宽到 8(≈ MaxSeats 的 2/3)后,12 人分 2 批,配合月窗口上限
//     30s 与 decisionTimeout 20s,足够全部 bot 完成一轮决策。
//   - 不设成 MaxSeats(12):LLM Provider 自身有全局并发/配额上限,12 路并发
//     容易触发 429 被 quarantine;8 是 Provider 配额与房间并发间的平衡。
//   - NewWealthRoom 的 llmConcurrency 参数=0 时回落此默认;管理器可按需覆盖。
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
// 设计动机:12 焦点玩家 × 时 LinePool 容量动态调整,避免 Provider 429。
// 典型值:12 焦点玩家 + 8 线路 → 7;200 背景居民 + 64 线路 → 64。
// 向后兼容:老客户端 poolTotal 传入 0 时回落 DefaultAgentConcurrency,
// 与 NewWealthRoom 既有行为一致,客户无感。
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

	// P1: 真实经济循环(2026-09-16 §财商流P1-2)。NewWorld 恒置
	// EconomyEnabled=true;房间层 Start 时按配置回写(§6.5 回退开关)。
	EconomyEnabled bool
	Goods          *GoodsMarket  // 八大类消费篮子 + 内生 CPI(nil 惰性初始化)
	Labor          *FirmSector   // 企业部门/劳动力市场(nil 惰性初始化)
	Society        *SocietyStats // 社会结构统计(月度缓存,view 直读)

	// P1: 社会调研系统(§财商流P1-2 调研契约 §2)。
	Surveys   []*Survey // 全房调研(≤20,按发起序)
	SurveySeq int       // id 自增序列

	// P1-4: 商业保险与风险转移引擎(2026-09-19 §财商流P1-4 §11)。NewWorld 恒置
	// true;房间层 Start 时按配置回写(false 时投保/退保 35041、月结不扣缴、
	// 意外事件不掷骰 —— rand 序列零偏移,固定种子存量对局回归一致)。
	InsuranceEnabled bool

	Players [MaxSeats]*Player // 空座 nil

	Rand *rand.Rand

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
}

// NewWorld 构造世界(seed=0 时用时间随机;cards[seat] 可为零值 Card 表示空座)。
func NewWorld(seed int64, cards [MaxSeats]profession.Card) *World {
	var rng *rand.Rand
	if seed != 0 {
		rng = rand.New(rand.NewSource(seed))
	} else {
		rng = rand.New(rand.NewSource(rand.Int63()))
	}
	w := &World{
		Month:    1,
		Status:   StatusOpen,
		Market:   NewMarket(rng),
		Ledger:   &Ledger{},
		CB:       NewCentralBank(),
		Rand:     rng,
		startAge: MasterStartAge,
		// P1: 真实经济循环恒开启(§6.5);economy_enabled=false 由房间层
		// Start 时回写(NewManager 归一后传入)。
		EconomyEnabled: true,
		// P1-4: 保险引擎默认开启(§11;SetInsuranceEnabled 可覆盖)。
		InsuranceEnabled: true,
		Goods:            NewGoodsMarket(),
		Labor:            NewFirmSector(),
		// P2: 玩家间交易与财富流动系统(2026-09-16 §财商流P2)。
		ListingBook:  NewListingBook(),
		AuctionHouse: NewAuctionHouse(),
	}
	for seat := 0; seat < MaxSeats; seat++ {
		if cards[seat].ID == "" {
			continue
		}
		w.Players[seat] = newPlayerFromCard(seat, cards[seat])
	}
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

// StartGame 开局:状态切 playing + 初始注入 Ledger(world→seat = 卡面 Savings,I2)。
// 由房间层在发卡完成后调用一次。
func (w *World) StartGame() {
	w.Status = StatusPlaying
	for seat, p := range w.Players {
		if p == nil {
			continue
		}
		p.ActionBudget = monthlyActionBudget
		p.Submitted = false
		p.SpokenThisMonth = false
		if p.Card.Savings > 0 {
			// 直接 Record(不走 Pay:注入即初始现金,不产生增量)。
			w.Ledger.Record(0, EntityWorld, SeatEntity(seat), p.Card.Savings, CatInject,
				"初始储蓄注入 "+p.Card.ID)
		}
	}
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
