// Package virtual_city — offledger.go: 账外调整登记簿(批次71 §3)
//
// 定位(方案 §3.1):**账外** = 现金发生了变动,但没有对应的 Ledger 双式条目。
// 这不是错误,是**记账边界** —— 但它必须被声明、被登记、可被审计,
// 否则 I1 守恒永远红,且没人知道钱去哪了。
//
// 本批立场:**不消灭账外调整**(消灭会波及 view / 白名单 / 历史对局),
// 而是把每一笔账外调整登记成一条带原因的流水,并让 I1 用
// 「账本净流 + 账外净流」口径闭合。
//
// 登记点(方案 §3.3,**恰好 4 处**,穷举):
//
//	1. actions.go::actDeposit   p.Cash -= AmountCNY → −AmountCNY(savings_deposit)
//	2. actions.go::actWithdraw  p.Cash += AmountCNY → +AmountCNY(savings_withdraw)
//	3. scenario.go::scenarioCashDamage p.Cash -= dmg → −dmg(scenario_damage)
//	4. family.go::DistributeInheritance  p.Cash = 0 → −p.Cash(estate_clear,清零前取值)
//
// ⚠ **纪律**:新增任何直接写 p.Cash 的代码,必须同提交登记,否则 I1 立刻红。
// 落地自查:grep -rn "p\.Cash +=\|p\.Cash -=\|p\.Cash = " ServerGo/game/virtual_city/
//
// ⚠ 另有两处**看着像账外、其实是账内**,不得登记(否则 I1 重复扣减变红):
//   - engine.go::World.Pay(账本 + 现金同记账)
//   - listing.go::PaySeatToSeat(先 Record 再直改双方现金)
//
// 零 rand 消费 —— 登记簿不参与任何随机序列(§197 反例教训)。
package virtual_city

// 账外调整原因枚举(枚举值即 i18n / 审计口径)。
const (
	// OffLedgerSavingsDeposit 活期→定期(座位内部划转)。
	OffLedgerSavingsDeposit = "savings_deposit"
	// OffLedgerSavingsWithdraw 定期→活期(座位内部划转)。
	OffLedgerSavingsWithdraw = "savings_withdraw"
	// OffLedgerScenarioDamage 场景冲击对居民现金的直接损失。
	OffLedgerScenarioDamage = "scenario_damage"
	// OffLedgerEstateClear 遗产清算后死者账簿清零。
	OffLedgerEstateClear = "estate_clear"
	// OffLedgerDeferredDefault 延期支付违约后座位现金归零(**假账外**:
	// bias.go 里 w.Pay 已把现金扣到 0,此处登记额恒为 0 —— 仅留痕,
	// 使「所有直接写 p.Cash 的位置都已在账外登记簿声明」字面成立)。
	OffLedgerDeferredDefault = "deferred_default_clear"
)

// offLedgerCap 登记簿环形缓冲容量(最近 200 条)。
const offLedgerCap = 200

// OffLedgerEntry 单条账外调整登记(不可变)。
type OffLedgerEntry struct {
	Month  int    `json:"month"`
	Seat   int    `json:"seat"`
	Reason string `json:"reason"`
	Amount int64  `json:"amount"` // 有符号:正 = 座位现金增加,负 = 座位现金减少
	Note   string `json:"note"`
}

// OffLedgerBook 账外调整登记簿(挂 World.OffLedger;惰性初始化,nil 安全)。
//
// ⚠ 字段/方法同名冲突:方案 §3.2 的数据形状写 `Total int64`,§3.4 的读取入口
// 写 `func Total() int64` —— Go 不允许同名字段与方法并存。本实现取
// **方法**(读取入口是对外契约),累计值存于未导出的 total 字段。
type OffLedgerBook struct {
	// Entries 登记流水(环形缓冲,容量 offLedgerCap=200;最旧在前)。
	Entries []OffLedgerEntry
	// SeatNet 逐座累计账外净流(I1 闭合用;O(1) 读取)。
	SeatNet [MaxSeats]int64
	// Digest 登记流水摘要(增量 FNV-1a 64;StateHash 纳入)。
	Digest uint64
	// total 全书累计账外净流(未导出;经 Total() 读取)。
	total int64
}

// Record 登记一笔账外调整(不可变条目;追加后裁尾保持容量上限)。
//
// amount 有符号:正 = 座位现金增加,负 = 座位现金减少。
// 零 rand 消费。
func (b *OffLedgerBook) Record(month, seat int, reason string, amount int64, note string) {
	if b == nil {
		return
	}
	b.Entries = append(b.Entries, OffLedgerEntry{
		Month: month, Seat: seat, Reason: reason, Amount: amount, Note: note,
	})
	if len(b.Entries) > offLedgerCap {
		b.Entries = b.Entries[len(b.Entries)-offLedgerCap:]
	}
	if seat >= 0 && seat < MaxSeats {
		b.SeatNet[seat] += amount
	}
	b.total += amount
	b.Digest = offLedgerDigestStep(b.Digest, month, seat, reason, amount)
}

// SeatNetFlow 逐座累计账外净流(I1 右端第三项)。
func (b *OffLedgerBook) SeatNetFlow(seat int) int64 {
	if b == nil || seat < 0 || seat >= MaxSeats {
		return 0
	}
	return b.SeatNet[seat]
}

// Total 全书累计账外净流。
func (b *OffLedgerBook) Total() int64 {
	if b == nil {
		return 0
	}
	return b.total
}

// Recent 最近 n 条账外调整(**最新在前**;n ≤ 0 返回空切片而非 nil)。
func (b *OffLedgerBook) Recent(n int) []OffLedgerEntry {
	out := make([]OffLedgerEntry, 0)
	if b == nil || n <= 0 {
		return out
	}
	for i := len(b.Entries) - 1; i >= 0 && len(out) < n; i-- {
		out = append(out, b.Entries[i])
	}
	return out
}

// offLedgerDigestStep 增量更新登记簿摘要(固定 FNV-1a 64;不取 Note 自由文本)。
func offLedgerDigestStep(h uint64, month, seat int, reason string, amount int64) uint64 {
	h = fnvWriteInt64(h, int64(month))
	h = fnvWriteInt64(h, int64(seat))
	h = fnvWriteString(h, reason)
	h = fnvWriteInt64(h, amount)
	return h
}

// ── World 侧接线 ───────────────────────────────────────────────────────

// offLedgerEnsure 惰性初始化账外调整登记簿(nil 安全)。
func (w *World) offLedgerEnsure() *OffLedgerBook {
	if w == nil {
		return nil
	}
	if w.OffLedger == nil {
		w.OffLedger = &OffLedgerBook{}
	}
	return w.OffLedger
}

// offLedgerRecord 登记一笔账外调整(引擎内部统一入口;nil 安全)。
func (w *World) offLedgerRecord(month, seat int, reason string, amount int64, note string) {
	b := w.offLedgerEnsure()
	if b == nil {
		return
	}
	b.Record(month, seat, reason, amount, note)
}
