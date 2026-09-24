// Package regulators — consumer_protection.go: 消费者保护监管
// (2026-09-21 §城市扩张v2.12 阶段8)。
//
// 职责:过度消费预警 —— 玩家"突发性消费"(Doodad 支出;引擎侧以当月
// **生活支出**(settlement 步骤5 living 口径)代理,财富主包从
// Monthly.Detail 聚合)> 月收入 50% → 触发"过度消费警告"事件(教育提示,
// 不罚款);同一座位 6 个月冷却,防事件刷屏。
//
// 解耦约束:本包不 import wealth 主包;输入经 ConsumerRow 注入,由
// wealth 主包(public_services.go::RegulatorBundle.MonthlyStep)适配。
//
// 纯引擎层:无锁、无 IO、零 rand。
package regulators

// 消费者保护常量(阶段8 新定)。
const (
	// OverconsumeIncomeRatio Doodad 支出 / 月收入 触发比(> 0.5 预警)。
	OverconsumeIncomeRatio = 0.5
	// ConsumerWarnCooldownMonths 同座位预警冷却月数。
	ConsumerWarnCooldownMonths = 6
)

// ConsumerRow 单座位月度消费画像(wealth 侧适配)。
type ConsumerRow struct {
	Seat             int
	MonthlyIncomeCNY int64 // 当月月收入(settlement ③ 已结算的 Monthly.Income)
	DoodadExpenseCNY int64 // 当月 Doodad 支出(living 口径聚合)
}

// OverconsumeWarning 单条过度消费警告(wealth 侧据此 emit 事件)。
type OverconsumeWarning struct {
	Seat     int   `json:"seat"`
	Month    int   `json:"month"`
	DoodadCNY int64 `json:"doodad_cny"`
	IncomeCNY int64 `json:"income_cny"`
}

// ConsumerRegulator 消费者保护监管状态(挂 wealth.RegulatorBundle.Consumer)。
type ConsumerRegulator struct {
	WarningsTotal int // 累计警告次数

	// lastWarn 座位 → 上次警告月份(冷却判定)。
	lastWarn map[int]int
	// LastStepMonth 最近一次 MonthlyStep 的月份(§130 接线验证)。
	LastStepMonth int
}

// NewConsumerRegulator 构造。
func NewConsumerRegulator() *ConsumerRegulator {
	return &ConsumerRegulator{lastWarn: map[int]int{}}
}

// IsOverconsuming 纯判定:Doodad 支出 > 月收入 × 50%(收入 ≤ 0 不判 ——
// 失业期强制消费不预警,与"失业救济/强制降档"逻辑不冲突)。
func IsOverconsuming(row ConsumerRow) bool {
	if row.MonthlyIncomeCNY <= 0 || row.DoodadExpenseCNY <= 0 {
		return false
	}
	return float64(row.DoodadExpenseCNY) > OverconsumeIncomeRatio*float64(row.MonthlyIncomeCNY)
}

// MonthlyStep 月度检查:rows 内命中 IsOverconsuming 且不在冷却期的座位
// 产出警告(顺序 = rows 传入序;wealth 侧按座位升序传入保证确定性)。
func (c *ConsumerRegulator) MonthlyStep(month int, rows []ConsumerRow) []OverconsumeWarning {
	if c == nil {
		return nil
	}
	c.LastStepMonth = month
	var out []OverconsumeWarning
	for _, row := range rows {
		if !IsOverconsuming(row) {
			continue
		}
		if last, ok := c.lastWarn[row.Seat]; ok && month-last < ConsumerWarnCooldownMonths {
			continue
		}
		c.lastWarn[row.Seat] = month
		c.WarningsTotal++
		out = append(out, OverconsumeWarning{
			Seat: row.Seat, Month: month,
			DoodadCNY: row.DoodadExpenseCNY, IncomeCNY: row.MonthlyIncomeCNY,
		})
	}
	return out
}
