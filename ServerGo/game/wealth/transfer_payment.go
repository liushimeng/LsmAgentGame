// Package wealth — transfer_payment.go: 转移支付(阶段4)
// 2026-09-21 §城市扩张v2.12 阶段4。
//
// 四类转移支付(全部 from=EntityGovernment → seat, category=CatWelfare,
// 走 w.Pay 通道,I1 守恒;国库现金不足时按座位序截断,不透支):
//
//	失业救济金  = 基本生活成本 2000 × 50% × 失业月数系数
//	             系数: 失业 1–3 月 = 0(等待期,R4-3)/ 4–12 月 = 1.0 /
//	             > 12 月 = 0.7(递减防躺平;月数取**终生累计**口径)
//	低保补贴    = max(0, 贫困线 1200 − 月收入) × 50%
//	             R4-3:需累计失业 ≥ 3 个月(有稳定工作者不领低保)
//	儿童津贴    = 200 元/孩/月(仅 6 岁以下 = 出生 < 72 主钟月)
//	养老金      = PensionCNY 个人账户余额 ÷ 139(年金除数;60 岁起)
//
// 计数口径:UnemployedAccumMonths 在本文件月末递增(仍处失业期的座位 +1),
// 即"已度过的完整失业月数"(events.go 失业判定设置的 UnemployedMonths 为
// **剩余**月数,两者正交);BirthMonths 由 events.go 生育事件追加。
package wealth

// 转移支付常量(阶段4 新定)。
const (
	// BasicLivingCostCNY 基本生活成本 2000 元/月(失业救济/低保基准)。
	BasicLivingCostCNY = 2000
	// UnemploymentBenefitRate 失业救济覆盖率(R4-3:只覆盖基本生活成本 50%)。
	UnemploymentBenefitRate = 0.5
	// UnemploymentWaitMonths 失业救济等待期(前 3 个月不计发,R4-3)。
	UnemploymentWaitMonths = 3
	// UnemploymentDecayFrom 累计失业超过此月数 → 系数衰减 0.7(防躺平)。
	UnemploymentDecayFrom = 12
	// UnemploymentDecayFactor 长期失业递减系数。
	UnemploymentDecayFactor = 0.7
	// PovertyLineCNY 贫困线 1200 元/月(低保补差基准)。
	PovertyLineCNY = 1200
	// LowIncomeCoverRate 低保补差覆盖率(只补缺口 50%)。
	LowIncomeCoverRate = 0.5
	// LowIncomeMinUnemployed R4-3:领低保需累计失业 ≥ 3 个月。
	LowIncomeMinUnemployed = 3
	// ChildAllowanceCNY 儿童津贴 200 元/孩/月。
	ChildAllowanceCNY = 200
	// ChildUnderAgeYears 津贴年龄上限(6 岁以下)。
	ChildUnderAgeYears = 6
	// PensionAnnuityDivisor 养老金计发除数(139 个月,个人账户年金口径)。
	PensionAnnuityDivisor = 139
)

// TransferType 转移支付类型。
type TransferType int

const (
	TransferUnemployment   TransferType = iota // 失业救济金
	TransferLowIncome                          // 低保补贴
	TransferChildAllowance                     // 儿童津贴
	TransferPension                            // 养老金
)

// String 中文标签(备注用)。
func (tt TransferType) String() string {
	switch tt {
	case TransferUnemployment:
		return "失业救济金"
	case TransferLowIncome:
		return "低保补贴"
	case TransferChildAllowance:
		return "儿童津贴"
	case TransferPension:
		return "养老金"
	}
	return "未知"
}

// unemploymentCoef 失业月数 → 救济系数:1–3 月 0(等待期)/ 4–12 月 1.0 /
// >12 月 0.7(递减防躺平)。
func unemploymentCoef(accumMonths int) float64 {
	switch {
	case accumMonths >= 1 && accumMonths <= UnemploymentWaitMonths:
		return 0
	case accumMonths > UnemploymentDecayFrom:
		return UnemploymentDecayFactor
	default:
		return 1.0
	}
}

// childrenUnder6 6 岁以下孩子数(出生月 < 当前月 − 72;BirthMonths 口径)。
func childrenUnder6(p *Player, month int) int {
	n := 0
	for _, m := range p.BirthMonths {
		if month-m < ChildUnderAgeYears*12 {
			n++
		}
	}
	return n
}

// payUnemployment 失业救济金:仍处失业期(UnemployedMonths > 0)→ 先推进累计
// 月数(+1,即「第 N 个失业月」)→ 按新计数取系数计发(2000 × 50% × 系数)。
// 等待期(第 1–3 月)只累计不计发(R4-3)。返回实发金额(国库不足 → 0)。
func (t *TreasuryState) payUnemployment(w *World, p *Player) int64 {
	if p.UnemployedMonths <= 0 {
		return 0 // 在职/再就业:不累计不计发
	}
	p.UnemployedAccumMonths++
	amount := int64(float64(BasicLivingCostCNY)*UnemploymentBenefitRate*
		unemploymentCoef(p.UnemployedAccumMonths) + 0.5)
	if amount <= 0 {
		return 0
	}
	return t.pay(w, p.Seat, TransferUnemployment, amount)
}

// payLowIncome 低保补贴(R4-3 双门槛:累计失业 ≥ 3 月 且 月收入 < 贫困线):
// 补差 = max(0, 1200 − 月收入) × 50%。
func (t *TreasuryState) payLowIncome(w *World, p *Player) int64 {
	if p.UnemployedAccumMonths < LowIncomeMinUnemployed {
		return 0
	}
	income := p.Monthly.Income
	if income < 0 {
		income = 0
	}
	gap := int64(PovertyLineCNY) - income
	if gap <= 0 {
		return 0
	}
	amount := int64(float64(gap)*LowIncomeCoverRate + 0.5)
	if amount <= 0 {
		return 0
	}
	return t.pay(w, p.Seat, TransferLowIncome, amount)
}

// payChildAllowance 儿童津贴:200 元 × 6 岁以下孩子数(BirthMonths 口径;
// 卡面初始子女不计 —— 仅追踪生育事件)。
func (t *TreasuryState) payChildAllowance(w *World, p *Player) int64 {
	n := childrenUnder6(p, w.Month)
	if n <= 0 {
		return 0
	}
	return t.pay(w, p.Seat, TransferChildAllowance, int64(n)*ChildAllowanceCNY)
}

// payPension 养老金(转移支付口径):60 岁起,个人账户余额 ÷ 139 计发。
// 不减 PensionCNY(与月结步骤2 个人账户养老金 0.04/12 同口径,避免净资产
// 漂移;139 仅为计发除数)。
func (t *TreasuryState) payPension(w *World, p *Player) int64 {
	if w.Age() < 60 || p.PensionCNY <= 0 {
		return 0
	}
	amount := p.PensionCNY / PensionAnnuityDivisor
	if amount <= 0 {
		return 0
	}
	return t.pay(w, p.Seat, TransferPension, amount)
}

// pay 统一发放通道:w.Pay(gov:treasury → seat, CatWelfare)+ 国库扣减;
// 国库现金不足 → 不发(返回 0,不透支)。
func (t *TreasuryState) pay(w *World, seat int, tt TransferType, amount int64) int64 {
	if amount <= 0 || amount > t.Cash {
		return 0
	}
	w.Pay(seat, EntityGovernment, SeatEntity(seat), amount, CatWelfare, tt.String())
	t.Cash -= amount
	return amount
}

// PayTransferPayments 月末派发四类转移支付(SettleTreasuryMonth ⑨B):
// 按座位序逐人评估(失业救济 → 低保 → 儿童津贴 → 养老金),
// 返回 (发放总额, 受益人数)。受益人数 = 当月收到任意一笔的座位数。
func (t *TreasuryState) PayTransferPayments(w *World) (total int64, beneficiaries int) {
	if t == nil || w == nil {
		return 0, 0
	}
	for _, p := range w.Players {
		if p == nil || !p.Alive {
			continue
		}
		got := int64(0)
		got += t.payUnemployment(w, p)
		got += t.payLowIncome(w, p)
		got += t.payChildAllowance(w, p)
		got += t.payPension(w, p)
		if got > 0 {
			total += got
			beneficiaries++
		}
	}
	return total, beneficiaries
}
