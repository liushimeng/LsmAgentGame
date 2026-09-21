// Package wealth — convertible_bond.go: 可转债市场(债性+股性混合)
// (2026-09-21 §城市扩张v2.12 阶段6)。
//
// 发行人为 supply_chain.go 的 15 个虚拟企业节点(链上中下游企业发债融资;
// 引用节点名做展示关联,不改动供应链引擎状态)。定价(零 rand):
//
//	转股价值  = 100 × StockIndex / ConvPrice        (面值 100 元/张口径)
//	债底      = Σ 票息/(1+y/12)^k + 面值/(1+y/12)^n (y = 当期 PhaseTable.BondRate)
//	delta     = clamp(0.70 + 0.25×转股价值/100, 0.70, 0.95) —— 价内升水系数
//	市价      = max(债底, 转股价值 × delta)         (债底托底 + 股性弹性)
//
// 强赎条款(简化):转股价值 > 130 连续 3 月 → 发行人强赎(赎回价 = 面值+应计票息),
// 促转股;回售条款:最后 2 年(MonthsLeft ≤ 24)转股价值 < 70 连续 2 月 → 持有人
// 回售(回售价 = 面值+应计票息)。到期兑付同价。触发后滚动补发新券(池恒 5 只,
// 转股价按当期股指 ×1.10 溢价发行),保持市场深度。
//
// 零 rand 消费 —— 固定种子存量对局回归零偏移(treasury/供应链同款纪律)。
package wealth

import (
	"fmt"
	"math"
)

// 可转债常量(阶段6 新定)。
const (
	// CBFaceValue 面值(元/张)。
	CBFaceValue int64 = 100
	// CBRedeemTrigger 强赎触发转股价值(>130)。
	CBRedeemTrigger = 130.0
	// CBRedeemStreak 强赎连续月数(15 交易日 ≈ 3 月简化)。
	CBRedeemStreak = 3
	// CBPutTrigger 回售触发转股价值(<70)。
	CBPutTrigger = 70.0
	// CBPutStreak 回售连续月数。
	CBPutStreak = 2
	// CBPutLastYearsMonths 回售窗口:最后 2 年(24 月)。
	CBPutLastYearsMonths = 24
	// CBConvPremium 新券转股溢价(转股价 = 当期股指 × 1.10)。
	CBConvPremium = 1.10
)

// 可转债状态。
const (
	CBStatusActive   = "active"
	CBStatusRedeemed = "redeemed" // 强赎
	CBStatusPut      = "put"      // 回售
	CBStatusMatured  = "matured"  // 到期
)

// ConvertibleBond 单只可转债(挂 World.CBonds;虚拟市场标的,玩家不直接持仓)。
type ConvertibleBond struct {
	ID         string  // 如 "CB1"
	Issuer     string  // 发行企业中文名(关联 supply_chain.go 节点)
	IssuerNode string  // 关联节点 id(如 "food_processing")
	FaceValue  int64   // 面值(元/张,恒 100)
	CouponRate float64 // 票息 0.5%-2%(低于普通债)
	ConvPrice  float64 // 转股价(发行时锁定)
	Months     int     // 期限 36-60 月
	MonthsLeft int
	IssueMonth int

	// ── 动态状态(CBMonthlyStep 维护;快照/测试用)──
	Status    string  // active|redeemed|put|matured
	Price     float64 // 当前市价(元/张)
	ConvValue float64 // 转股价值
	BondFloor float64 // 债底
	ExitPrice float64 // 退出价(强赎/回售/到期;active 为 0)

	specIdx  int // cbSpecs 序(补发同规格;内部)
	hiStreak int // 连续 转股价值>130 月数(强赎计数)
	loStreak int // 连续 转股价值<70 月数(回售计数)
}

// cbSpec 发行规格表(轮转补发;IssuerNode 取自 supply_chain.go 节点 id)。
var cbSpecs = []struct {
	issuer     string
	issuerNode string
	coupon     float64
	months     int
}{
	{"食品加工", "food_processing", 0.008, 48},
	{"能源生产", "energy_production", 0.012, 60},
	{"整机组装", "elec_assembly", 0.015, 36},
	{"批发零售", "food_wholesale", 0.006, 60},
	{"化工新材料", "energy_chemical", 0.020, 48},
}

// newConvertibleBond 按规格发新券(转股价 = 当期股指 × 1.10 溢价)。
// ID = "CB<规格序>-M<发行月>":每规格每月至多发 1 只(池恒 5、一规格一券)→ 恒唯一。
func newConvertibleBond(specIdx int, stockIdx float64, issueMonth int) *ConvertibleBond {
	specIdx = ((specIdx % len(cbSpecs)) + len(cbSpecs)) % len(cbSpecs)
	s := cbSpecs[specIdx]
	if stockIdx <= 0 {
		stockIdx = InitialStockIndex
	}
	return &ConvertibleBond{
		ID: fmt.Sprintf("CB%d-M%d", specIdx+1, issueMonth), Issuer: s.issuer, IssuerNode: s.issuerNode,
		FaceValue: CBFaceValue, CouponRate: s.coupon, Months: s.months,
		MonthsLeft: s.months, IssueMonth: issueMonth,
		ConvPrice: stockIdx * CBConvPremium,
		Status:    CBStatusActive, specIdx: specIdx,
	}
}

// NewConvertibleBonds 构造初始 5 只(规格表序;转股价锚定当期股指)。
func NewConvertibleBonds(stockIdx float64) []*ConvertibleBond {
	out := make([]*ConvertibleBond, 0, len(cbSpecs))
	for i := range cbSpecs {
		out = append(out, newConvertibleBond(i, stockIdx, 1))
	}
	return out
}

// cbBondFloor 债底 = 剩余票息现值 + 面值现值(月度贴现,y 为年化到期收益率)。
func cbBondFloor(face int64, coupon float64, monthsLeft int, y float64) float64 {
	if monthsLeft < 0 {
		monthsLeft = 0
	}
	r := y / 12
	if r < 1e-9 {
		r = 1e-9 // 防零利率除零。
	}
	pv := 0.0
	couponMonthly := float64(face) * coupon / 12
	for k := 1; k <= monthsLeft; k++ {
		pv += couponMonthly / math.Pow(1+r, float64(k))
	}
	pv += float64(face) / math.Pow(1+r, float64(monthsLeft))
	return pv
}

// cbDelta 股性系数:价内(转股价值高)升水、价外收敛 0.70。
func cbDelta(convValue float64) float64 {
	return clampF(0.70+0.25*convValue/100.0, 0.70, 0.95)
}

// CBPrice 市价 = max(债底, 转股价值 × delta)(纯函数)。
func CBPrice(face int64, coupon float64, monthsLeft int, y float64, convValue float64) float64 {
	floor := cbBondFloor(face, coupon, monthsLeft, y)
	equity := convValue * cbDelta(convValue)
	return math.Max(floor, equity)
}

// accruedExitPrice 退出价(强赎/回售/到期)= 面值 + 应计票息(线性)。
func (b *ConvertibleBond) accruedExitPrice() float64 {
	elapsed := b.Months - b.MonthsLeft
	if elapsed < 0 {
		elapsed = 0
	}
	return float64(b.FaceValue) * (1 + b.CouponRate*float64(elapsed)/12)
}

// stepConvertibleBonds 全房可转债月度推进(SettleMonth ⑥B):
// ① 估值(转股价值/债底/市价)→ ② 强赎/回售/到期检查(条款计数器)→
// ③ 退出结算(退出价)+ 事件播报 → ④ 滚动补发(池恒 5 只)。
// 零 rand;economy_enabled=false 由调用方(SettleMonth ⑥B)跳过。
func stepConvertibleBonds(w *World) {
	if w == nil || w.Market == nil {
		return
	}
	if len(w.CBonds) == 0 {
		// 首月播种后**继续**估值(当月 view 快照即有市价,不空窗一月)。
		w.CBonds = NewConvertibleBonds(w.Market.StockIndex)
	}
	y := w.Market.Params().BondRate
	replacements := []int{} // 本月退出券的规格序(补发同规格新券)。
	for _, b := range w.CBonds {
		if b.Status != CBStatusActive {
			continue
		}
		// ① 估值。
		b.ConvValue = 100.0 * w.Market.StockIndex / b.ConvPrice
		b.BondFloor = cbBondFloor(b.FaceValue, b.CouponRate, b.MonthsLeft, y)
		b.Price = CBPrice(b.FaceValue, b.CouponRate, b.MonthsLeft, y, b.ConvValue)

		// ② 条款计数器。
		if b.ConvValue > CBRedeemTrigger {
			b.hiStreak++
		} else {
			b.hiStreak = 0
		}
		if b.ConvValue < CBPutTrigger && b.MonthsLeft <= CBPutLastYearsMonths {
			b.loStreak++
		} else {
			b.loStreak = 0
		}

		// ③ 触发结算(强赎 > 回售 > 到期;退出后不再推进)。
		switch {
		case b.hiStreak >= CBRedeemStreak:
			b.Status = CBStatusRedeemed
			b.ExitPrice = b.accruedExitPrice()
			b.Price = b.ExitPrice
			w.emitEvent("market", -1, fmt.Sprintf(
				"可转债 %s(%s)触发强赎:转股价值 %.0f 连续 %d 月 > %.0f,赎回价 ¥%.2f",
				b.ID, b.Issuer, b.ConvValue, b.hiStreak, CBRedeemTrigger, b.ExitPrice))
			replacements = append(replacements, b.specIdx)
			continue
		case b.loStreak >= CBPutStreak:
			b.Status = CBStatusPut
			b.ExitPrice = b.accruedExitPrice()
			b.Price = b.ExitPrice
			w.emitEvent("market", -1, fmt.Sprintf(
				"可转债 %s(%s)触发回售:转股价值 %.0f 连续 %d 月 < %.0f(最后 2 年),回售价 ¥%.2f",
				b.ID, b.Issuer, b.ConvValue, b.loStreak, CBPutTrigger, b.ExitPrice))
			replacements = append(replacements, b.specIdx)
			continue
		}

		// 到期:剩余期数递减(退出券不递减)。
		b.MonthsLeft--
		if b.MonthsLeft <= 0 {
			b.Status = CBStatusMatured
			b.ExitPrice = b.accruedExitPrice()
			b.Price = b.ExitPrice
			replacements = append(replacements, b.specIdx)
		}
	}
	// ④ 滚动补发:退出一只补发一只(同规格;新转股价锚当期股指,池恒 5 只)。
	for _, specIdx := range replacements {
		w.CBonds = append(w.CBonds, newConvertibleBond(specIdx, w.Market.StockIndex, w.Month))
	}
}
