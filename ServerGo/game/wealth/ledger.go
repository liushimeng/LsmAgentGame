// Package wealth — ledger.go: 财富流水账(双式记录 + 实体枚举 + 守恒校验)
// 2026-09-14 §财商流P0。
//
// 契约: 后端架构文档 §13。核心特色:所有现金变动都有 From→To 双式记录,
// 座位现金守恒不变量 I1 由构造保证(pay 辅助函数同记账同扣款)。
package wealth

import (
	"fmt"
)

// Ledger 实体常量(§13 实体语义硬约束)。
const (
	EntityBank    = "bank"    // 银行:贷款放出/还本付息;工资代发(雇主并入 bank)
	EntityMarket  = "market"  // 市场:资产买卖/租金/佣金/中介费/资本利得
	EntityGov     = "gov"     // 政府:个税/社保(pension 类白名单 gov→seat)
	EntityInsurer = "insurer" // 保险(P0 预留,无交易)
	EntityWorld   = "world"   // 系统外:from=world 仅限初始注入;to=world=公益转移/消费类
	// EntityFirms 企业部门(P1 §财商流P1-2):家庭消费的接收方;只收不付
	// (工资仍由 bank 代发;企业营收回流玩家为 P2 分红)。
	EntityFirms = "firms"
)

// SeatEntity 拼装座位实体 id。
func SeatEntity(seat int) string { return fmt.Sprintf("seat:%d", seat) }

// IsSeatEntity 判定并解出座位号。
func IsSeatEntity(e string) (seat int, ok bool) {
	var n int
	if _, err := fmt.Sscanf(e, "seat:%d", &n); err == nil {
		return n, true
	}
	return -1, false
}

// Ledger category 全集(§13;P0 增补 study|social|moving|overtime|property,
// 见协议文档同步说明)。
const (
	CatSalary       = "salary"
	CatSpouse       = "spouse"
	CatSide         = "side"
	CatRent         = "rent"
	CatBondInterest = "bond_interest"
	CatPension      = "pension"
	CatTax          = "tax"
	CatSocial       = "social"
	CatLiving       = "living"
	CatMortgage     = "mortgage"
	CatRentPay      = "rent_pay"
	CatInterest     = "interest"
	CatPrincipal    = "principal"
	CatBuy          = "buy"
	CatSell         = "sell"
	CatFee          = "fee"
	CatLoan         = "loan"
	CatRepay        = "repay"
	CatConsume      = "consume"
	CatDonate       = "donate"
	CatMedical      = "medical"
	CatWedding      = "wedding"
	CatInject       = "inject"
	// P0 增补(动作表 §4 的 Ledger 备注列)。
	CatStudy    = "study"
	CatSocialEv = "social_event"
	CatMoving   = "moving"
	CatOvertime = "overtime"
	CatProperty = "property"
)

// Entry 单条双式流水。
type Entry struct {
	Month     int    `json:"month"`
	From      string `json:"from"`
	To        string `json:"to"`
	AmountCNY int64  `json:"amount_cny"` // 恒正;方向由 From→To 表达
	Category  string `json:"category"`
	Note      string `json:"note"` // ≤60 字符
}

// Ledger 全量流水(全量内存保留,§13 容量估算 ≈3.4 万条无压力)。
type Ledger struct {
	Entries []Entry
}

// validEntity 实体白名单(I3)。
func validEntity(e string) bool {
	switch e {
	case EntityBank, EntityMarket, EntityGov, EntityInsurer, EntityWorld, EntityFirms:
		return true
	default:
		if _, ok := IsSeatEntity(e); ok {
			return true
		}
		return false
	}
}

// firmsInCategories to=firms 允许的消费类 category(§3.6 方向白名单表)。
var firmsInCategories = map[string]bool{
	CatLiving: true, CatConsume: true, CatRentPay: true, CatProperty: true,
	CatStudy: true, CatSocialEv: true, CatMoving: true, CatMedical: true, CatWedding: true,
}

// validFromTo 实体方向白名单(I3 + P1 §3.6):
//   - firms 只收不付:from=firms 恒非法;to=firms 仅消费类白名单;
//   - world 只能作为 from(初始注入)或 to(donate 公益转移 + 消费类 —— 后者
//     为 economy_enabled=false 的 P0 回退路径保留,§6.5 回退是一等公民);
//   - gov 只收不付(pension 类白名单除外);
//   - insurer P0 无交易。
func validFromTo(from, to, category string) error {
	if !validEntity(from) || !validEntity(to) {
		return fmt.Errorf("invalid entity: %s → %s", from, to)
	}
	if from == EntityInsurer || to == EntityInsurer {
		return fmt.Errorf("insurer has no trades in P0: %s → %s", from, to)
	}
	if from == EntityFirms {
		return fmt.Errorf("firms never pays out (wages are paid by bank): %s → %s", from, to)
	}
	if to == EntityFirms && !firmsInCategories[category] {
		return fmt.Errorf("firms only receives consumption categories (category=%s)", category)
	}
	if from == EntityWorld && category != CatInject {
		return fmt.Errorf("world can only be the source of initial inject (category=%s)", category)
	}
	if to == EntityWorld {
		if category == CatInject {
			return fmt.Errorf("inject must come from world")
		}
		if category != CatDonate && !firmsInCategories[category] {
			return fmt.Errorf("world only receives donate/consumption categories (category=%s)", category)
		}
	}
	if from == EntityGov && category != CatPension {
		return fmt.Errorf("gov never pays out except pension (category=%s)", category)
	}
	return nil
}

// truncateNote 备注截断 ≤60 rune。
func truncateNote(note string) string {
	r := []rune(note)
	if len(r) > 60 {
		return string(r[:59]) + "…"
	}
	return note
}

// Record 追加一条流水(带实体方向校验;校验失败 panic 交由上层测试捕获——
// 引擎代码路径只产出合法方向,校验是开发期不变量而非运行时错误通道)。
func (l *Ledger) Record(month int, from, to string, amount int64, category, note string) {
	if amount < 0 {
		panic(fmt.Sprintf("ledger amount must be >= 0: %d", amount))
	}
	if err := validFromTo(from, to, category); err != nil {
		panic(err.Error())
	}
	l.Entries = append(l.Entries, Entry{
		Month: month, From: from, To: to,
		AmountCNY: amount, Category: category, Note: truncateNote(note),
	})
}

// SeatNetFlow 座位净流入 = Σ(to=seat) − Σ(from=seat)(I1 右端)。
func (l *Ledger) SeatNetFlow(seat int, month int) int64 {
	id := SeatEntity(seat)
	var net int64
	for _, e := range l.Entries {
		if month > 0 && e.Month != month {
			continue
		}
		if e.To == id {
			net += e.AmountCNY
		}
		if e.From == id {
			net -= e.AmountCNY
		}
	}
	return net
}

// WorldInjectCount 统计 from=world 的条目数(I2:应 = 座位数,仅初始注入)。
func (l *Ledger) WorldInjectCount() int {
	n := 0
	for _, e := range l.Entries {
		if e.From == EntityWorld {
			n++
		}
	}
	return n
}

// SeatRecent 本人相关 + 公共条目最近 limit 条(view 下发用,§13)。
func (l *Ledger) SeatRecent(seat int, limit int) []Entry {
	id := SeatEntity(seat)
	var out []Entry
	for i := len(l.Entries) - 1; i >= 0 && len(out) < limit; i-- {
		e := l.Entries[i]
		if e.From == id || e.To == id {
			out = append(out, e)
		}
	}
	return out
}

// VerifySeatConservation 守恒不变量 I1:对单座位单月,
// 期末 − 期初 = Σ(to) − Σ(from)。返回差异(0 = 守恒)。
func (l *Ledger) VerifySeatConservation(seat, month int, cashBefore, cashAfter int64) int64 {
	return (cashAfter - cashBefore) - l.SeatNetFlow(seat, month)
}

// MonthEntries 返回指定月份全部条目(调试/测试用)。
func (l *Ledger) MonthEntries(month int) []Entry {
	var out []Entry
	for _, e := range l.Entries {
		if e.Month == month {
			out = append(out, e)
		}
	}
	return out
}
