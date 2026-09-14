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

// MaxSeats 房间容量(8 座,3–8 可玩)。
const MaxSeats = 8

// MinSeats 最少开局座位。
const MinSeats = 3

// MasterStartAge 主时钟开局基准年龄(精选手卡恒 25;文档池混龄卡仅展示个人
// age,主时钟统一 25 起 — 验收清单 B1 设计取舍)。
const MasterStartAge = 25

// TerminalAge 终局年龄(《规则》§14.1:60 岁强制结算)。
const TerminalAge = 60

// 终局结局 id(后端架构 §12)。
const (
	EndingWinner    = "winner"     // 财务自由·人生赢家(≥85)
	EndingAffluent  = "affluent"   // 富足安稳(70–84)
	EndingOrdinary  = "ordinary"   // 平淡度日(50–69)
	EndingIndebted  = "indebted"   // 债务缠身(30–49)
	EndingBankrupt  = "bankrupt"   // 破产出局(<30)
	EndingLonelyRic = "lonely_rich" // 孤独富翁(FI≥1.5 且人生满意度<40)
)

// EventRecord 单条事件(10.3 事件播报;view 层映射 game.event / events_recent)。
type EventRecord struct {
	Month int
	Type  string // action|move|settle|market|life|chat|error
	Seat  int    // -1 = 全房事件
	Text  string
}

// World 财商流纯引擎世界状态。
type World struct {
	Month  int // 1..420(主时钟)
	Status string

	Market *MarketState
	Ledger *Ledger

	Players [MaxSeats]*Player // 空座 nil

	Rand *rand.Rand

	Events []EventRecord // 全量事件(增长缓慢:每月 ≈ 1–3 条)

	startAge int
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
		Rand:     rng,
		startAge: MasterStartAge,
	}
	for seat := 0; seat < MaxSeats; seat++ {
		if cards[seat].ID == "" {
			continue
		}
		w.Players[seat] = newPlayerFromCard(seat, cards[seat])
	}
	return w
}

// newPlayerFromCard 按职业卡初始化单座位(初始注入 world→seat = Savings,I2)。
func newPlayerFromCard(seat int, card profession.Card) *Player {
	p := &Player{
		Seat:        seat,
		Card:        card,
		Age:         card.StartAge,
		Cash:        card.Savings,
		Energy:      card.Energy,
		Network:     card.Network,
		Cognition:   card.Cognition,
		CreditScore: card.CreditScore,
		District:    card.HomeDistrict,
		HomeDistrict: card.HomeDistrict,
		Family:      Family{Marital: card.Marital, Children: card.ChildrenCount},
		SalaryBase:  card.Salary,
		Alive:       true,
		StatusIcon:  "idle",
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
func InflationFactor(month int) float64 {
	years := (month - 1) / 12
	if years < 0 {
		years = 0
	}
	return math.Pow(1.05, float64(years))
}
