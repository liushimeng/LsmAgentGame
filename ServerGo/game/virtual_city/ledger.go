// Package virtual_city — ledger.go: 财富流水账(双式记录 + 实体枚举 + 守恒校验)
// 2026-09-14 §财商流P0。
//
// 契约: 后端架构文档 §13。核心特色:所有现金变动都有 From→To 双式记录,
// 座位现金守恒不变量 I1 由构造保证(pay 辅助函数同记账同扣款)。
package virtual_city

import (
	"fmt"
)

// Ledger 实体常量(§13 实体语义硬约束)。
const (
	EntityBank    = "bank"    // 银行:贷款放出/还本付息;工资代发(雇主并入 bank)
	EntityMarket  = "market"  // 市场:资产买卖/租金/佣金/中介费/资本利得
	EntityGov     = "gov"     // 政府:个税/社保(pension 类白名单 gov→seat)
	EntityInsurer = "insurer" // 保险公司:保费收 / 理赔付(P1-4 激活,2026-09-19 §财商流P1-4)
	EntityWorld   = "world"   // 系统外:from=world 仅限初始注入;to=world=公益转移/消费类
	// EntityFirms 企业部门(P1 §财商流P1-2):家庭消费的接收方;只收不付
	// (工资仍由 bank 代发;企业营收回流玩家为 P2 分红)。
	EntityFirms = "firms"
	// EntityGovernment 国库(阶段4 2026-09-21 §城市扩张v2.12):政府财政
	// 子系统的唯一付款方(treasury.go / fiscal_policy.go / transfer_payment.go)。
	// 与 EntityGov 分离:gov 是 P0 既有税收归集实体(个税/社保/养老金回流);
	// gov:treasury 是阶段4 国库 —— 只付不收(转移支付/财政直发 CatWelfare)。
	EntityGovernment = "gov:treasury"
	// EntityFamily 家庭(批次52 §3.3 裁决 D6):代际财富转移的板外家庭实体 ——
	// 赡养/教育/遗产的接收方、子女回流的付款方。照 EntityGov 白名单守卫风格:
	// 只收代际三类,只付 CatChildSupport(→ seat),其余方向恒非法。
	EntityFamily = "family"
)

// SeatEntity 拼装座位实体 id。
func SeatEntity(seat int) string { return fmt.Sprintf("seat:%d", seat) }

// IsSeatEntity 判定并解出座位号。
func IsSeatEntity(e string) (seat int, ok bool) {
	// 批次72 性能修复:本函数位于 Record / I1 / I2 / I8 等账本扫描热路径
	//(每条 Entry × 每月多次),fmt.Sscanf ≈1µs/次,420 月 × 数万条 ⇒ 单局
	// 秒级(蒙特卡洛 harness 实测 77% CPU 花在 Fscanf)。改为手写解析:
	// SeatEntity 是本 id 的唯一构造函数(恒 "seat:%d"、seat ≥ 0),
	// 非规范输入(如 "seat:-3" / "seat:12x")引擎内不存在,按非座位处理。
	const prefix = "seat:"
	if len(e) <= len(prefix) || e[:len(prefix)] != prefix {
		return -1, false
	}
	n := 0
	for i := len(prefix); i < len(e); i++ {
		c := e[i]
		if c < '0' || c > '9' {
			return -1, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
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
	// P2: 玩家间交易 / 拍卖 / 信息 / 借贷(2026-09-16 §财商流P2)。
	CatTrade        = "trade"         // 玩家间资产成交(买卖双方)
	CatTradeFee     = "trade_fee"     // 交易税费(增值税/中介费)
	CatP2PInterest  = "p2p_interest"  // P2P 借贷利息
	CatP2PRepay     = "p2p_repay"     // P2P 借贷还本
	CatAuctionFee   = "auction_fee"   // 拍卖佣金
	CatInfoTrade    = "info_trade"    // 信息交易
	CatNegotiateFee = "negotiate_fee" // 议价服务费
	// P1-4: 商业保险(2026-09-19 §财商流P1-4 §6.1)。
	CatPremium = "premium" // 保费(座位→保险公司)
	CatClaim   = "claim"   // 理赔(保险公司→座位)
	// 阶段4: 政府财政(2026-09-21 §城市扩张v2.12)。
	CatWelfare = "welfare" // 转移支付/财政直发(gov:treasury → seat;I1 守恒走 w.Pay)
	// 批次52: 代际财富转移四类别(§3.3;数值与算法见 family.go)。
	CatInheritance   = "inheritance"    // 遗产继承(seat→family/World,CatInheritance)
	CatFamilySupport = "family_support" // 赡养支出(seat→family;含自愿加赡养)
	CatEduTuition    = "education"      // 教育投入(seat→family;升级一次性 + 月度)
	CatChildSupport  = "child_support"  // 子女成年回流(family→seat,收入侧)
	// 批次62: 行为金融偏差引擎(2026-10-20 §20261020-62)。
	CatBiasCost = "bias_cost"       // 偏差成本(seat→market):追高溢价 / 跟风滑点 / 沉没追加
	CatBehaviorBonus = "behavior_bonus" // 储蓄纪律奖励(bank→seat):月结余 × 2%
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
	// SeatNets 逐座累计净流(批次71 §4.2:I1 闭合用;Record 内**增量**维护,
	// 避免每次检查 O(entries) 全扫 —— 480 月 × 34k 条不可接受)。
	// 口径:Σ(to=seat) − Σ(from=seat),**含** world 初始注入。
	SeatNets [MaxSeats]int64
	// Digest 账本摘要(批次71 §5.3-6:Record 内**增量** FNV-1a 64 更新,
	// 与 Entries 同序,不重算全表;StateHash 纳入)。
	// 口径:只取语义字段(month/from/to/amount/category),不含 Note 自由文本。
	Digest uint64
}

// validEntity 实体白名单(I3)。
func validEntity(e string) bool {
	switch e {
	case EntityBank, EntityMarket, EntityGov, EntityInsurer, EntityWorld, EntityFirms, EntityGovernment, EntityFamily:
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
//   - insurer 只与座位交易(P1-4 激活,§财商流P1-4 §6.1):
//     seat→insurer 仅 CatPremium;insurer→seat 仅 CatClaim。
func validFromTo(from, to, category string) error {
	if !validEntity(from) || !validEntity(to) {
		return fmt.Errorf("invalid entity: %s → %s", from, to)
	}
	if to == EntityInsurer {
		if category != CatPremium {
			return fmt.Errorf("insurer only receives premium (category=%s)", category)
		}
		if _, ok := IsSeatEntity(from); !ok {
			return fmt.Errorf("insurer only trades with seats: %s → %s", from, to)
		}
	}
	if from == EntityInsurer {
		if category != CatClaim {
			return fmt.Errorf("insurer only pays claims (category=%s)", category)
		}
		if _, ok := IsSeatEntity(to); !ok {
			return fmt.Errorf("insurer only trades with seats: %s → %s", from, to)
		}
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
		// 批次52(§4 裁决 D6):无配偶无子女 → 遗产充公入 world(CatInheritance)。
		if category != CatDonate && category != CatInheritance && !firmsInCategories[category] {
			return fmt.Errorf("world only receives donate/inheritance/consumption categories (category=%s)", category)
		}
	}
	if from == EntityGov && category != CatPension {
		return fmt.Errorf("gov never pays out except pension (category=%s)", category)
	}
	// 批次52(§3.3 裁决 D6):family 照 EntityGov 白名单守卫风格 ——
	// 只收代际三类(继承/赡养/教育),只付 CatChildSupport(→ seat)。
	if to == EntityFamily {
		switch category {
		case CatInheritance, CatFamilySupport, CatEduTuition:
			if _, ok := IsSeatEntity(from); !ok {
				return fmt.Errorf("family only receives from seats: %s → %s", from, to)
			}
		default:
			return fmt.Errorf("family only receives inheritance/family_support/education (category=%s)", category)
		}
	}
	if from == EntityFamily {
		if category != CatChildSupport {
			return fmt.Errorf("family only pays child_support (category=%s)", category)
		}
		if _, ok := IsSeatEntity(to); !ok {
			return fmt.Errorf("family only pays child_support to seats: %s → %s", from, to)
		}
	}
	// 阶段4: gov:treasury 通道(2026-09-21 §城市扩张v2.12,I3 增补):
	//   - to=gov:treasury 恒非法(税收经 EntityGov 归集;国库现金由
	//     TreasuryState 簿记,不经 Ledger 收款 —— 防止双重入账);
	//   - from=gov:treasury 仅允许 → seat + CatWelfare(转移支付/财政直发)。
	//     R4-1:gov:treasury 与央行/银行/市场/企业/保险公司/系统外之间
	//     **不存在任何通道** —— 财政部不能向央行透支;财政采购与国债还本
	//     付息为国库账内核算(treasury.go 头注释),不产生 Ledger 条目。
	if to == EntityGovernment {
		return fmt.Errorf("gov:treasury never receives via ledger (taxes flow to gov): %s → %s", from, to)
	}
	if from == EntityGovernment {
		if _, ok := IsSeatEntity(to); !ok || category != CatWelfare {
			return fmt.Errorf("gov:treasury only pays welfare to seats: %s → %s (category=%s)", from, to, category)
		}
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
//
// ⚠ 批次71:本函数**额外**增量维护 SeatNets / Digest(不改既有校验与 panic 行为)。
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
	// 增量维护逐座净流(批次71 §4.2)。
	if seat, ok := IsSeatEntity(to); ok {
		l.SeatNets[seat] += amount
	}
	if seat, ok := IsSeatEntity(from); ok {
		l.SeatNets[seat] -= amount
	}
	// 增量维护账本摘要(批次71 §5.3-6;固定 FNV-1a 64)。
	l.Digest = ledgerDigestStep(l.Digest, month, from, to, amount, category)
}

// ledgerDigestStep 增量更新账本摘要(固定 FNV-1a 64;不取 Note 自由文本 ——
// 备注是可变文本,纳入摘要会让同一经济事实因措辞不同而哈希不同)。
func ledgerDigestStep(h uint64, month int, from, to string, amount int64, category string) uint64 {
	h = fnvWriteInt64(h, int64(month))
	h = fnvWriteString(h, from)
	h = fnvWriteString(h, to)
	h = fnvWriteInt64(h, amount)
	h = fnvWriteString(h, category)
	return h
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

// EntityNet 实体净流入 = Σ(to=entity) − Σ(from=entity)(I4,调试/验收用)。
// insurer 净额 = Σ保费 − Σ理赔 = 保险公司利润(可为负,大灾年亏损是教学内容)。
func (l *Ledger) EntityNet(entity string) int64 {
	var net int64
	for _, e := range l.Entries {
		if e.To == entity {
			net += e.AmountCNY
		}
		if e.From == entity {
			net -= e.AmountCNY
		}
	}
	return net
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
