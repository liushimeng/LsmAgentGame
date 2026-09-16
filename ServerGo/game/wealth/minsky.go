// Package wealth — minsky.go: 明斯基金融不稳定引擎(2026-09-16 §财商流P1)。
//
// 实现设计文档: docs/财商流游戏/已实现/05-P1扩展/财商流游戏-P1-Minsky与LPR引擎-v1.md §2。
// 纯引擎层:无锁、无 goroutine、无 IO;triggerMinskyMoment / forceLiquidate 由
// settlement.go 持房间锁调用。
package wealth

import (
	"fmt"
	"strings"
)

// MinskyTier 明斯基融资等级(v2.60 N11-4)。
type MinskyTier string

const (
	MinskyHedge      MinskyTier = "hedge"       // 对冲性:月供 ≤ 月收入 40%
	MinskySpeculative MinskyTier = "speculative" // 投机性:月供 占收入 40%-70%
	MinskyPonzi      MinskyTier = "ponzi"       // 庞氏性:月供 占收入 > 70%
)

// MinskyStatus 单笔贷款的明斯基分级状态。
type MinskyStatus struct {
	Tier         MinskyTier `json:"tier"`           // hedge/speculative/ponzi
	DebtToIncome float64    `json:"debt_to_income"` // 月供/月收入(0-1+)
	IsRolling    bool       `json:"is_rolling"`     // 投机/庞氏是否已滚动借新还旧
}

// ClassifyMinsky 根据贷款月供与借款人月收入划分明斯基等级(v2.60 N11-4)。
// 参数: monthlyPayment(月供), monthlyIncome(月收入+配偶+副业), rolling(是否滚动借新还旧)。
func ClassifyMinsky(monthlyPayment, monthlyIncome int64, rolling bool) MinskyTier {
	if monthlyIncome <= 0 {
		return MinskyPonzi
	}
	ratio := float64(monthlyPayment) / float64(monthlyIncome)
	switch {
	case ratio <= 0.40:
		return MinskyHedge
	case ratio <= 0.70:
		return MinskySpeculative
	default:
		return MinskyPonzi
	}
}

// MinskyRateAdjustment 明斯基等级利率调整(小数,诱人陷阱)。
func MinskyRateAdjustment(tier MinskyTier) float64 {
	switch tier {
	case MinskyHedge:
		return 0
	case MinskySpeculative:
		return 0.005 // +0.5%
	case MinskyPonzi:
		return -0.005 // 市场奖励高风险 -0.5%(陷阱)
	}
	return 0
}

// shouldLiquidate 报告资产是否属于杠杆清仓范围(精确匹配或前缀匹配 "house:")。
func shouldLiquidate(a *Asset, leveragedKinds []string) bool {
	for _, k := range leveragedKinds {
		if strings.HasPrefix(a.Kind, k) {
			return true
		}
	}
	return false
}

// forceLiquidate 强制变现指定比例杠杆资产(v2.60 N11-5)。
// 调用方持房间锁;proceeds 按变现前市值 × ratio 入现金(I1 守恒走 World.Pay)。
func (w *World) forceLiquidate(p *Player, seat int, ratio float64) {
	leveragedKinds := []string{AssetStockIndex, AssetSideBusiness}
	if ratio >= 1.0 {
		leveragedKinds = append(leveragedKinds, "house:") // 庞氏清仓房产
	}
	newAssets := make([]Asset, 0, len(p.Assets))
	for i := range p.Assets {
		a := p.Assets[i]
		if shouldLiquidate(&a, leveragedKinds) {
			sellUnits := a.Units * ratio
			// 按变现前市值 × ratio 计算收入(AssetValue 依赖 a.Units)。
			proceeds := int64(float64(AssetValue(&a, w.Market)) * ratio)
			a.Units -= sellUnits
			if proceeds > 0 {
				w.Pay(seat, EntityMarket, SeatEntity(seat), proceeds, CatSell, "minsky_liquidation")
			}
		}
		if a.Units > 0.001 {
			newAssets = append(newAssets, a)
		}
	}
	p.Assets = newAssets
}

// playerDominantMinskyTier 返回玩家最差的明斯基等级(取所有贷款最差者)。
func (w *World) playerDominantMinskyTier(p *Player) MinskyTier {
	worst := MinskyHedge
	for _, ms := range p.MinskyByLoan {
		if ms == nil {
			continue
		}
		if ms.Tier == MinskyPonzi {
			return MinskyPonzi
		}
		if ms.Tier == MinskySpeculative {
			worst = MinskySpeculative
		}
	}
	return worst
}

// minskyPonziCount 统计庞氏玩家数(主导等级 = Ponzi 的存活玩家)。
func (w *World) minskyPonziCount() int {
	n := 0
	for _, p := range w.Players {
		if p == nil || !p.Alive {
			continue
		}
		if w.playerDominantMinskyTier(p) == MinskyPonzi {
			n++
		}
	}
	return n
}

// triggerMinskyMoment 触发明斯基时刻(v2.60 N11-5)。
// 效果:
//   - 所有杠杆资产(stock_index/side_business 持仓)立即 -50%
//   - 庞氏玩家强制平仓所有杠杆资产,损失 100% 头寸
//   - 投机玩家损失 50% 杠杆头寸
//   - 对冲玩家不受影响
//   - 触发后进入 12 月冷却期(避免连续触发)
//
// 内嵌前置判定(冷却期内 / 庞氏占比 ≤30% 不触发),确保直接调用也安全。
// 调用方持房间锁。
func (w *World) triggerMinskyMoment(res *SettleResult) {
	alive := len(w.alivePlayers())
	if alive == 0 {
		return
	}
	ponziCount := w.minskyPonziCount()
	if ponziCount == 0 {
		return
	}
	ponziRatio := float64(ponziCount) / float64(alive)
	if ponziRatio <= 0.30 {
		return
	}
	if w.MinskyMomentCooldown > 0 {
		return
	}
	w.MinskyMomentCooldown = 12
	w.MinskyMomentCount++
	w.emitEvent("minsky", -1, fmt.Sprintf(
		"🚨 明斯基时刻！庞氏玩家占比 %.0f%% > 30%%，全场杠杆资产价格腰斩！", ponziRatio*100))

	for seat, p := range w.Players {
		if p == nil || !p.Alive {
			continue
		}
		// 标记玩家本回合经历明斯基时刻(内部状态,用于前端/复盘)。
		p.MinskyMomentTriggered = true
		tier := w.playerDominantMinskyTier(p)
		switch tier {
		case MinskyPonzi:
			w.forceLiquidate(p, seat, 1.0) // 100% 头寸
		case MinskySpeculative:
			w.forceLiquidate(p, seat, 0.5) // 50% 头寸
		// Hedge 不受影响
		}
	}
	// 市场指数立即 -50%。
	w.Market.StockIndex *= 0.5
	for _, d := range DistrictDefs {
		w.Market.DistrictIdx[d.ID] *= 0.5
	}
	// 记录到 SettleResult.Events(让前端 game.month 也能看到)。
	if res != nil {
		res.Events = append(res.Events, EventRecord{
			Month: w.Month, Type: "minsky", Seat: -1,
			Text: fmt.Sprintf("明斯基时刻触发(第 %d 次),全场杠杆资产 -50%%", w.MinskyMomentCount),
		})
	}
}

// reclassifyPlayerMinsky 月结时重算玩家所有贷款的明斯基分级(工资/收入可能变化)。
// 调用方持房间锁。
func (w *World) reclassifyPlayerMinsky(p *Player) {
	if p == nil {
		return
	}
	inc := p.monthlyIncomeEstimate()
	for i := range p.Loans {
		loan := &p.Loans[i]
		ms := p.MinskyByLoan[loan.ID]
		if ms == nil {
			ms = &MinskyStatus{}
			p.MinskyByLoan[loan.ID] = ms
		}
		ms.Tier = ClassifyMinsky(loan.MonthlyPayment, inc, ms.IsRolling)
		if inc > 0 {
			ms.DebtToIncome = float64(loan.MonthlyPayment) / float64(inc)
		} else {
			ms.DebtToIncome = 999
		}
	}
}

// recordMinskyForLoan 发放贷款时立即分级并写入 MinskyByLoan。
// 调用方持房间锁。
func (w *World) recordMinskyForLoan(p *Player, loan *Loan) {
	if loan == nil || p == nil {
		return
	}
	inc := p.monthlyIncomeEstimate()
	tier := ClassifyMinsky(loan.MonthlyPayment, inc, false)
	p.MinskyByLoan[loan.ID] = &MinskyStatus{
		Tier: tier,
		DebtToIncome: func() float64 {
			if inc > 0 {
				return float64(loan.MonthlyPayment) / float64(inc)
			}
			return 999
		}(),
		IsRolling: false,
	}
}
