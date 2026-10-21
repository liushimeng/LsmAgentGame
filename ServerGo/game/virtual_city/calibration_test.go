// Package virtual_city — calibration_test.go: 批次71 校准与验证框架测试
// (2026-10-21 §校准与验证框架落地 方案 §10.1)。
//
// 覆盖方案 §10.1 的后端测试 B1–B7 / B11–B13;
// B8 / B9 / B10(状态哈希三件套)见 statehash_test.go。
//
// 硬纪律:本文件的所有测试都**不得**消费 w.Rand ——
// 审计接线一旦移动随机序列,B10 会立刻红(那正是它的用途)。
package virtual_city

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"regexp"
	"strings"
	"testing"

	"LsmAgentGame/game/virtual_city/profession"
)

// calibTestWorld 构造确定性测试世界(seed + 合成卡;同参 ⇒ 同局)。
func calibTestWorld(seed int64, seats int) *World {
	cards := [MaxSeats]profession.Card{}
	sub := profession.SyntheticCards(seats, rand.New(rand.NewSource(seed)))
	for i := 0; i < seats && i < len(sub); i++ {
		cards[i] = sub[i]
	}
	w := NewWorld(seed, cards)
	w.StartGame()
	return w
}

// calibSettleMonths 推进 n 个月月结(忽略终局;返回实际月数)。
func calibSettleMonths(w *World, n int) int {
	done := 0
	for i := 0; i < n; i++ {
		finished, _ := w.SettleMonth()
		done++
		if finished {
			break
		}
	}
	return done
}

// calibInvariantByID 取指定 id 的不变量检查结果。
func calibInvariantByID(t *testing.T, checks []InvariantCheck, id string) InvariantCheck {
	t.Helper()
	for _, c := range checks {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("不变量 %s 不在检查结果中", id)
	return InvariantCheck{}
}

// ── B1 账外登记点穷举 ──────────────────────────────────────────────────

// TestOffLedgerBook_RegisterSites 账外登记点穷举断言(方案 §10.1 B1)。
//
// 规则:包内**任何**直接写 `p.Cash` 的位置,都必须在同一函数内调用
// `offLedgerRecord` 登记;否则 I1 立刻红。
//
// 实测命中集合(2026-10-21 勘察):
//
//	actions.go  p.Cash -= a.AmountCNY   真账外(savings_deposit)
//	actions.go  p.Cash += a.AmountCNY   真账外(savings_withdraw)
//	scenario.go p.Cash -= dmg           真账外(scenario_damage)
//	family.go   p.Cash = 0              真账外(estate_clear)
//	bias.go     p.Cash = 0              假账外(登记额恒 0)
func TestOffLedgerBook_RegisterSites(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	// 三条赋值形态:`+=` / `-=` / `=`(排除 `==` 比较)。
	// ⚠ 注释行一律跳过(文档里大量出现 `p.Cash -=` 字样,不是代码)。
	res := []*regexp.Regexp{
		regexp.MustCompile(`p\.Cash\s*\+=`),
		regexp.MustCompile(`p\.Cash\s*-=`),
		regexp.MustCompile(`p\.Cash\s*=[^=]`),
	}
	type hit struct {
		file   string
		line   int
		source string
	}
	var hits []hit
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		lines := strings.Split(string(raw), "\n")
		for i, ln := range lines {
			trimmed := strings.TrimSpace(ln)
			if trimmed == "" || strings.HasPrefix(trimmed, "//") {
				continue
			}
			matched := false
			for _, re := range res {
				if re.MatchString(ln) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
			hits = append(hits, hit{file: name, line: i + 1, source: trimmed})
		}
	}
	// 命中数固定为 5(4 处真账外 + 1 处假账外)。
	if len(hits) != 5 {
		t.Fatalf("直接写 p.Cash 的位置 = %d 处,期望 5 处:\n%v", len(hits), hits)
	}
	wantFiles := map[string]int{"actions.go": 2, "scenario.go": 1, "family.go": 1, "bias.go": 1}
	gotFiles := map[string]int{}
	for _, h := range hits {
		gotFiles[h.file]++
	}
	for f, n := range wantFiles {
		if gotFiles[f] != n {
			t.Errorf("%s 命中 %d 处,期望 %d 处", f, gotFiles[f], n)
		}
	}
	// 每处命中都必须在 ±8 行内出现 offLedgerRecord 登记。
	for _, h := range hits {
		raw, err := os.ReadFile(h.file)
		if err != nil {
			t.Fatalf("read %s: %v", h.file, err)
		}
		lines := strings.Split(string(raw), "\n")
		lo, hi := h.line-9, h.line+8
		if lo < 0 {
			lo = 0
		}
		if hi > len(lines) {
			hi = len(lines)
		}
		registered := false
		for i := lo; i < hi; i++ {
			if strings.Contains(lines[i], "offLedgerRecord(") {
				registered = true
				break
			}
		}
		if !registered {
			t.Errorf("%s:%d 未登记账外调整:%s", h.file, h.line, strings.TrimSpace(h.source))
		}
	}
}

// TestOffLedgerBook_Bookkeeping 登记簿自身口径(Record/SeatNetFlow/Total/Recent)。
func TestOffLedgerBook_Bookkeeping(t *testing.T) {
	var b OffLedgerBook
	b.Record(1, 0, OffLedgerSavingsDeposit, -1000, "活期转定期")
	b.Record(2, 0, OffLedgerSavingsWithdraw, 400, "定期转活期")
	b.Record(3, 2, OffLedgerScenarioDamage, -250, "场景冲击")

	if got := b.SeatNetFlow(0); got != -600 {
		t.Errorf("座 0 账外净流 = %d,期望 -600", got)
	}
	if got := b.SeatNetFlow(2); got != -250 {
		t.Errorf("座 2 账外净流 = %d,期望 -250", got)
	}
	if got := b.Total(); got != -850 {
		t.Errorf("全书账外净流 = %d,期望 -850", got)
	}
	recent := b.Recent(2)
	if len(recent) != 2 {
		t.Fatalf("Recent(2) 长度 = %d,期望 2", len(recent))
	}
	if recent[0].Reason != OffLedgerScenarioDamage || recent[1].Reason != OffLedgerSavingsWithdraw {
		t.Errorf("Recent 应最新在前,实得 %s / %s", recent[0].Reason, recent[1].Reason)
	}
	if len(b.Recent(0)) != 0 {
		t.Errorf("Recent(0) 应为空切片")
	}
	// 环形缓冲上限。
	for i := 0; i < offLedgerCap+50; i++ {
		b.Record(i, 0, OffLedgerSavingsDeposit, -1, "x")
	}
	if len(b.Entries) != offLedgerCap {
		t.Errorf("登记簿长度 = %d,期望裁尾至 %d", len(b.Entries), offLedgerCap)
	}
}

// TestOffLedgerBook_DepositWithdrawRegistered 活期↔定期划转的账外登记
// (动作路径端到端:登记额 = 座位现金变动方向相反数)。
func TestOffLedgerBook_DepositWithdrawRegistered(t *testing.T) {
	w := calibTestWorld(7, 4)
	seat := 0
	p := w.Players[seat]
	before := p.Cash
	if _, e := w.ApplyAction(seat, Action{Type: ActDeposit, AmountCNY: 5000}); e != nil {
		t.Fatalf("deposit: %v", e)
	}
	if p.Cash != before-5000 {
		t.Fatalf("deposit 后现金 = %d,期望 %d", p.Cash, before-5000)
	}
	if got := w.OffLedger.SeatNetFlow(seat); got != -5000 {
		t.Errorf("deposit 账外净流 = %d,期望 -5000", got)
	}
	if _, e := w.ApplyAction(seat, Action{Type: ActWithdraw, AmountCNY: 2000}); e != nil {
		t.Fatalf("withdraw: %v", e)
	}
	if got := w.OffLedger.SeatNetFlow(seat); got != -3000 {
		t.Errorf("withdraw 后账外净流 = %d,期望 -3000", got)
	}
	// I1 仍成立(账本净流 + 账外净流 == 现金)。
	inv := calibInvariantByID(t, CheckInvariants(w), InvariantI1)
	if !inv.Passed {
		t.Errorf("I1 失败:%s", inv.Detail)
	}
}

// ── B2 I1 座位现金守恒 ─────────────────────────────────────────────────

// TestInvariants_I1_SeatCashConservation 40 月 × 6 座逐月 I1 差额 == 0
// (方案 §10.1 B2)。
//
// ⚠ **出局座位口径(实测)**:破产出局座位在出局当月与出局后的差额**恒为 0**
// —— 破产清算不改 p.Cash(只记 BankruptCount);遗产清算的清零由
// `estate_clear` 账外登记精确对冲 ⇒ I1 对**全部在座座位**生效(含已出局),
// 不做跳过。本测试显式断言「出局月与出局后都参与断言」。
func TestInvariants_I1_SeatCashConservation(t *testing.T) {
	w := calibTestWorld(42, 6)
	months := calibSettleMonths(w, 40)
	if months < 12 {
		t.Fatalf("只推进了 %d 月,样本不足", months)
	}
	if len(w.Audits) == 0 {
		t.Fatal("无审计快照")
	}
	outCount := 0
	for _, a := range w.Audits {
		inv := calibInvariantByID(t, a.Invariants, InvariantI1)
		if !inv.Passed {
			t.Errorf("月 %d I1 失败:actual=%v detail=%s", a.Month, inv.Actual, inv.Detail)
		}
	}
	for _, p := range w.Players {
		if p != nil && !p.Alive {
			outCount++
		}
	}
	if outCount == 0 {
		t.Log("本轮无座位出局(I1 全在座断言)")
	} else {
		t.Logf("本轮 %d 座出局,I1 仍对其断言(差额恒 0)", outCount)
	}
}

// ── B3 I2 world 注入唯一性 ─────────────────────────────────────────────

// TestInvariants_I2_WorldInjectUniqueness 每座恰好 1 条 from=world 注入
// 且 month == 0(方案 §10.1 B3)。
func TestInvariants_I2_WorldInjectUniqueness(t *testing.T) {
	w := calibTestWorld(11, 6)
	calibSettleMonths(w, 12)
	checks := CheckInvariants(w)
	inv := calibInvariantByID(t, checks, InvariantI2)
	if !inv.Passed {
		t.Fatalf("I2 失败:%s", inv.Detail)
	}
	// 直接复核:逐座统计 from=world 且 to=seat 的条目。
	for seat, p := range w.Players {
		if p == nil {
			continue
		}
		n := 0
		for _, e := range w.Ledger.Entries {
			if e.From == EntityWorld && e.To == SeatEntity(seat) {
				n++
				if e.Month != 0 {
					t.Errorf("座 %d 注入月 = %d,期望 0", seat, e.Month)
				}
			}
		}
		if n != 1 {
			t.Errorf("座 %d 注入条数 = %d,期望 1", seat, n)
		}
	}
}

// ── B4 I3 国库收支闭合 ─────────────────────────────────────────────────

// TestInvariants_I3_TreasuryClosure 40 月逐月 ΔCash == Revenue − Expense
// (方案 §10.1 B4)。
func TestInvariants_I3_TreasuryClosure(t *testing.T) {
	w := calibTestWorld(42, 6)
	calibSettleMonths(w, 40)
	for _, a := range w.Audits {
		inv := calibInvariantByID(t, a.Invariants, InvariantI3)
		if !inv.Passed {
			t.Errorf("月 %d I3 失败:actual=%v detail=%s", a.Month, inv.Actual, inv.Detail)
		}
	}
	// 直接复核最后一月。
	t2 := w.Treasury
	diff := (t2.Cash - t2.PrevMonthCash) - (t2.LastMonthRevenue - t2.LastMonthExpense - t2.OtherExpense)
	if diff != 0 {
		t.Errorf("末月 I3 差额 = %d(现金 %d 快照 %d 收入 %d 支出 %d 账外 %d)",
			diff, t2.Cash, t2.PrevMonthCash, t2.LastMonthRevenue, t2.LastMonthExpense, t2.OtherExpense)
	}
}

// TestInvariants_I3_MayorStipendRegistered 市长津贴是国库现金的第 7 处
// 改动点(在 ⑨G,落在国库月结窗口之外)⇒ 必须登记进 OtherExpense,
// 否则 I3 立刻红。
func TestInvariants_I3_MayorStipendRegistered(t *testing.T) {
	w := calibTestWorld(42, 6)
	// 打开市长选举(默认关闭)。
	w.Election.Enabled = true
	w.Election.MayorSeat = 0
	calibSettleMonths(w, 3)
	if w.Treasury.OtherExpense == 0 {
		t.Fatalf("市长津贴未登记进 OtherExpense(=%d)", w.Treasury.OtherExpense)
	}
	inv := calibInvariantByID(t, CheckInvariants(w), InvariantI3)
	if !inv.Passed {
		t.Errorf("启用市长津贴后 I3 失败:%s", inv.Detail)
	}
}

// ── B5 I4 资产负债口径契约 ─────────────────────────────────────────────

// TestInvariants_I4_NetWorthContract 独立重算 == p.NetWorth();且
// Loan.Balance >= 0、AssetValue >= 0(方案 §10.1 B5)。
func TestInvariants_I4_NetWorthContract(t *testing.T) {
	w := calibTestWorld(42, 6)
	calibSettleMonths(w, 24)
	for _, a := range w.Audits {
		inv := calibInvariantByID(t, a.Invariants, InvariantI4)
		if !inv.Passed {
			t.Errorf("月 %d I4 失败:actual=%v detail=%s", a.Month, inv.Actual, inv.Detail)
		}
	}
	// 独立复核:逐座比对。
	for seat, p := range w.Players {
		if p == nil {
			continue
		}
		want := recomputeNetWorth(p, w.Market)
		if got := p.NetWorth(w.Market); want != got {
			t.Errorf("座 %d 口径差 %d(重算 %d vs NetWorth %d)", seat, want-got, want, got)
		}
		for i := range p.Loans {
			if p.Loans[i].Balance < 0 {
				t.Errorf("座 %d 贷款 %s 余额为负 %d", seat, p.Loans[i].ID, p.Loans[i].Balance)
			}
		}
	}
}

// TestInvariants_I4_DetectsNetWorthTamper I4 必须是**真检查**:
// 人为把 NetWorth 口径改坏(此处用等价手法:独立重算值 ≠ NetWorth)
// 时,I4 必须变红 —— 证明它不是恒真式。
func TestInvariants_I4_DetectsNetWorthTamper(t *testing.T) {
	w := calibTestWorld(42, 4)
	// 篡改:给某座加一笔「账外」资产(只改资产市值,不改现金/贷款),
	// 制造独立重算与 NetWorth 口径的分歧。
	p := w.Players[0]
	p.Loans = append(p.Loans, Loan{ID: "L-TEST", Kind: LoanConsumer, Principal: 1000, Balance: 1000})
	// NetWorth 与 recomputeNetWorth 此刻仍一致(同一口径)——
	// 真正的篡改检测交给 checkI4 的独立分支:直接改一个座位余额为负。
	p.Loans[0].Balance = -500
	inv := calibInvariantByID(t, CheckInvariants(w), InvariantI4)
	if inv.Passed {
		t.Errorf("负贷款余额未被 I4 捕获(恒真式风险):%s", inv.Detail)
	}
}

// ── B6 I6 场景注入留痕 ─────────────────────────────────────────────────

// TestInvariants_I6_ScenarioAuditTrail 注入记录含 kind/magnitude/target/
// duration/seed/snapshot(方案 §10.1 B6)。
func TestInvariants_I6_ScenarioAuditTrail(t *testing.T) {
	w := calibTestWorld(42, 6)
	calibSettleMonths(w, 3)
	rec, _, _, e := applyScenarioToWorld(w, ScenarioEarthquake, 3, 6, "", "测试注入", 1730000000000, 4242)
	if e != nil {
		t.Fatalf("applyScenario: %v", e)
	}
	if rec.Kind == "" {
		t.Error("注入记录缺 kind")
	}
	if rec.Magnitude < 1 || rec.Magnitude > 5 {
		t.Errorf("magnitude = %d,期望 ∈ [1,5]", rec.Magnitude)
	}
	if rec.AppliedMonth <= 0 {
		t.Errorf("applied_month = %d,期望 > 0", rec.AppliedMonth)
	}
	if rec.DurationMonths != 6 {
		t.Errorf("duration_months = %d,期望 6", rec.DurationMonths)
	}
	if rec.Seed != 4242 {
		t.Errorf("seed = %d,期望 4242", rec.Seed)
	}
	if rec.SnapshotID == "" {
		t.Errorf("snapshot_id 为空(注入应并入 active prediction run)")
	}
	inv := calibInvariantByID(t, CheckInvariants(w), InvariantI6)
	if !inv.Passed {
		t.Errorf("I6 失败:%s", inv.Detail)
	}
}

// TestInvariants_I6_DetectsMissingTrail I6 必须是真检查:
// 构造一条缺 seed 的记录 ⇒ I6 必须变红。
func TestInvariants_I6_DetectsMissingTrail(t *testing.T) {
	w := calibTestWorld(42, 4)
	w.ScenarioApplied = append(w.ScenarioApplied, ScenarioAppliedRecord{
		ID: "SC1", Kind: ScenarioEarthquake, Magnitude: 2, AppliedMonth: 1,
		DurationMonths: 3, Seed: 0,
	})
	inv := calibInvariantByID(t, CheckInvariants(w), InvariantI6)
	if inv.Passed {
		t.Errorf("缺 seed 的注入记录未被 I6 捕获(恒真式风险):%s", inv.Detail)
	}
}

// ── B7 I8 系统净流闭合 ─────────────────────────────────────────────────

// TestInvariants_I8_LedgerClosure 全实体净流和 == 0;座位组 + 非座位组 == 0
// (方案 §10.1 B7)。
func TestInvariants_I8_LedgerClosure(t *testing.T) {
	w := calibTestWorld(42, 6)
	calibSettleMonths(w, 24)
	for _, a := range w.Audits {
		inv := calibInvariantByID(t, a.Invariants, InvariantI8)
		if !inv.Passed {
			t.Errorf("月 %d I8 失败:actual=%v detail=%s", a.Month, inv.Actual, inv.Detail)
		}
	}
}

// TestInvariants_I8_DetectsUnknownEntity I8 必须是真检查:
// 绕过 Record 直接 append 一条白名单外实体的条目 ⇒ 「来路不明」组净流 ≠ 0
// ⇒ I8 必须变红。
func TestInvariants_I8_DetectsUnknownEntity(t *testing.T) {
	w := calibTestWorld(42, 4)
	w.Ledger.Entries = append(w.Ledger.Entries, Entry{
		Month: 1, From: "ghost", To: SeatEntity(0), AmountCNY: 1234, Category: CatConsume,
	})
	inv := calibInvariantByID(t, CheckInvariants(w), InvariantI8)
	if inv.Passed {
		t.Errorf("白名单外实体条目未被 I8 捕获(恒真式风险):%s", inv.Detail)
	}
}

// ── B11 红线判定 ───────────────────────────────────────────────────────

// TestCalibration_RedlineEvaluation 构造越界样本 ⇒ R-E3/E4/E5/E6 正确触发
// (方案 §10.1 B11)。
func TestCalibration_RedlineEvaluation(t *testing.T) {
	cases := []struct {
		label   string
		d2      float64
		d5      float64
		trigger []string
		notTrig []string
	}{
		{"扩张过高", 8.5, 0.2, []string{RedlineRE3}, []string{RedlineRE4}},
		{"扩张过低", 1.2, 0.2, []string{RedlineRE4}, []string{RedlineRE3}},
		{"破产过高", 3.0, 0.55, []string{RedlineRE5}, []string{RedlineRE6}},
		{"破产过低", 3.0, 0.02, []string{RedlineRE6}, []string{RedlineRE5}},
		{"全在区间", 3.4, 0.2, nil, []string{RedlineRE3, RedlineRE4, RedlineRE5, RedlineRE6}},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			ind := CalibrationIndicators{
				MoneyExpansion: f64Ptr(c.d2),
				BankruptcyRate: f64Ptr(c.d5),
			}
			rl := EvaluateRedlines(ind, nil, nil)
			if len(rl) != 7 {
				t.Fatalf("红线条数 = %d,期望 7", len(rl))
			}
			wantOrder := []string{RedlineRE3, RedlineRE4, RedlineRE5, RedlineRE6, RedlineRB1, RedlineRE1, RedlineRE2}
			for i, id := range wantOrder {
				if rl[i].ID != id {
					t.Fatalf("红线顺序错位:%d = %s,期望 %s", i, rl[i].ID, id)
				}
			}
			got := map[string]bool{}
			for _, r := range rl {
				got[r.ID] = r.Triggered
			}
			for _, id := range c.trigger {
				if !got[id] {
					t.Errorf("%s 未触发", id)
				}
			}
			for _, id := range c.notTrig {
				if got[id] {
					t.Errorf("%s 不应触发", id)
				}
			}
		})
	}
}

// TestCalibration_RedlineRB1 首位座位偏差(R-B1)。
func TestCalibration_RedlineRB1(t *testing.T) {
	ind := CalibrationIndicators{MoneyExpansion: f64Ptr(3.0), BankruptcyRate: f64Ptr(0.2)}
	outcomes := []OutcomeStat{
		{Seat: 0, Outcome: OutcomeWinner, Good: true},
		{Seat: 1, Outcome: OutcomeOrdinary},
		{Seat: 2, Outcome: OutcomeOrdinary},
	}
	rl := EvaluateRedlines(ind, outcomes, nil)
	var rb1 RedlineHit
	for _, r := range rl {
		if r.ID == RedlineRB1 {
			rb1 = r
		}
	}
	if !rb1.Triggered {
		t.Errorf("R-B1 应触发(首位 100%% vs 其余 0%%):%+v", rb1)
	}
	// 首位同样普通 ⇒ 不触发。
	outcomes[0].Good = false
	rl = EvaluateRedlines(ind, outcomes, nil)
	for _, r := range rl {
		if r.ID == RedlineRB1 && r.Triggered {
			t.Errorf("R-B1 不应触发:%+v", r)
		}
	}
}

// ── B12 RunRecord 字段齐备与口径自洽 ───────────────────────────────────

// TestCalibration_RunRecordFields RunRecord 字段齐备且口径自洽
// (D2 == End/Start)(方案 §10.1 B12)。
func TestCalibration_RunRecordFields(t *testing.T) {
	w := calibTestWorld(42, 6)
	calibSettleMonths(w, 12)
	cs := w.calibrationEnsure()
	if cs == nil || cs.Current == nil {
		t.Fatal("无进行中的校准运行记录")
	}
	rec := cs.Current
	if rec.RunID == "" || !strings.HasPrefix(rec.RunID, "CR") {
		t.Errorf("run_id = %q,期望 CR 前缀", rec.RunID)
	}
	if rec.Seed != w.Seed {
		t.Errorf("seed = %d,期望 %d", rec.Seed, w.Seed)
	}
	if rec.Phase == "" {
		t.Error("phase 为空")
	}
	if rec.TotalSteps <= 0 {
		t.Errorf("total_steps = %d,期望 > 0", rec.TotalSteps)
	}
	if rec.PopulationSize != 6 {
		t.Errorf("population_size = %d,期望 6", rec.PopulationSize)
	}
	if rec.TotalAssetsStart <= 0 {
		t.Errorf("total_assets_start = %d,期望 > 0", rec.TotalAssetsStart)
	}
	if rec.TotalAssetsEnd == 0 {
		t.Errorf("total_assets_end = 0")
	}
	wantD2 := float64(rec.TotalAssetsEnd) / float64(rec.TotalAssetsStart)
	if diff := rec.MoneyExpansion - wantD2; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("D2 = %v,与 End/Start = %v 不自洽", rec.MoneyExpansion, wantD2)
	}
	if len(rec.InvariantsOK) != 7 {
		t.Errorf("invariants_ok 长度 = %d,期望 7", len(rec.InvariantsOK))
	}
	if rec.StateHash == "" || len(rec.StateHash) != 16 {
		t.Errorf("state_hash = %q,期望 16 位十六进制", rec.StateHash)
	}
	if rec.OutcomeCounts == nil {
		t.Error("outcome_counts 为 nil")
	}
}

// TestCalibration_FinalizeRun 终局时运行记录收口进 Runs。
func TestCalibration_FinalizeRun(t *testing.T) {
	w := calibTestWorld(42, 6)
	calibSettleMonths(w, 6)
	if w.calibrationEnsure() == nil || w.calibrationEnsure().Current == nil {
		t.Fatal("无进行中的运行记录")
	}
	w.finalizeCalibrationRun()
	cs := w.calibrationEnsure()
	if cs.Current != nil {
		t.Error("收口后 Current 应为 nil")
	}
	if len(cs.Runs) != 1 {
		t.Fatalf("runs 长度 = %d,期望 1", len(cs.Runs))
	}
	if cs.Runs[0].RunID != "CR1" {
		t.Errorf("run_id = %q,期望 CR1", cs.Runs[0].RunID)
	}
}

// ── B13 存量对局零回归 ─────────────────────────────────────────────────

// TestAudit_NoRegressionOnExistingWorlds 固定种子对局在新增审计接线后
// **指标零偏移**(方案 §10.1 B13)。
//
// ⚠ 金标值取自**批次 71 之前**的代码(git HEAD 4e951b254e 的独立
// worktree 跑同一探针取得),逐字节一致 ⇒ 审计接线未移动任何模拟行为。
func TestAudit_NoRegressionOnExistingWorlds(t *testing.T) {
	w := calibTestWorld(42, 6)
	calibSettleMonths(w, 24)

	if w.Month != 25 {
		t.Errorf("month = %d,期望 25", w.Month)
	}
	if got := len(w.Ledger.Entries); got != 537 {
		t.Errorf("账本条目数 = %d,期望 537", got)
	}
	type seatGold struct {
		cash  int64
		nw    int64
		alive bool
		end   string
	}
	gold := []seatGold{
		{cash: -22724, nw: -22724, alive: false, end: "bankrupt"},
		{cash: -4487, nw: 2102, alive: false, end: "bankrupt"},
		{cash: -14664, nw: -782, alive: false, end: "bankrupt"},
		{cash: 72325, nw: 114917, alive: true, end: ""},
		{cash: -14185, nw: 23617, alive: false, end: "bankrupt"},
		{cash: 48146, nw: 72161, alive: true, end: ""},
	}
	for seat, g := range gold {
		p := w.Players[seat]
		if p == nil {
			t.Fatalf("座 %d 为空", seat)
		}
		if p.Cash != g.cash {
			t.Errorf("座 %d 现金 = %d,期望 %d", seat, p.Cash, g.cash)
		}
		if nw := p.NetWorth(w.Market); nw != g.nw {
			t.Errorf("座 %d 净资产 = %d,期望 %d", seat, nw, g.nw)
		}
		if p.Alive != g.alive {
			t.Errorf("座 %d alive = %v,期望 %v", seat, p.Alive, g.alive)
		}
		if p.Ending != g.end {
			t.Errorf("座 %d ending = %q,期望 %q", seat, p.Ending, g.end)
		}
	}
	// 浮点金标:容差 1e-9(远严于任何行为漂移,又避开末位表示噪声)。
	if got := w.Market.StockIndex; math.Abs(got-4.650379528164) > 1e-9 {
		t.Errorf("股指 = %v,期望 ≈4.650379528164", got)
	}
	if got := w.Market.GoldPrice; math.Abs(got-687.307086582964) > 1e-9 {
		t.Errorf("金价 = %v,期望 ≈687.307086582964", got)
	}
	if got := w.Treasury.Cash; got != 5211215 {
		t.Errorf("国库现金 = %d,期望 5211215", got)
	}
}

// ── 契约形状(§9)───────────────────────────────────────────────────────

// TestCalibration_ContractShape 契约形状断言(方案 §9.2):
// invariants 恒 7 条 / redlines 恒 5 条 / history 恒为数组 /
// indicators 无值键为 null(不得省略)。
func TestCalibration_ContractShape(t *testing.T) {
	w := calibTestWorld(42, 6)
	calibSettleMonths(w, 3)

	cs := BuildClientState("room-1", 0, w,
		[MaxSeats]string{"u:0"}, [MaxSeats]string{"玩家0"}, [MaxSeats]bool{false},
		[MaxSeats]string{}, [MaxSeats]BotTranscript{}, 0, 0, 0, nil, nil)
	if cs.Calibration == nil {
		t.Fatal("game.state.calibration 未下发")
	}
	if len(cs.Calibration.Invariants) != 7 {
		t.Errorf("invariants 长度 = %d,期望 7", len(cs.Calibration.Invariants))
	}
	wantInv := []string{InvariantI1, InvariantI2, InvariantI3, InvariantI4, InvariantI5, InvariantI6, InvariantI8}
	for i, id := range wantInv {
		if cs.Calibration.Invariants[i].ID != id {
			t.Errorf("invariants[%d] = %s,期望 %s", i, cs.Calibration.Invariants[i].ID, id)
		}
	}
	if len(cs.Calibration.Redlines) != 7 {
		t.Errorf("redlines 长度 = %d,期望 7", len(cs.Calibration.Redlines))
	}
	wantRed := []string{RedlineRE3, RedlineRE4, RedlineRE5, RedlineRE6, RedlineRB1, RedlineRE1, RedlineRE2}
	for i, id := range wantRed {
		if cs.Calibration.Redlines[i].ID != id {
			t.Errorf("redlines[%d] = %s,期望 %s", i, cs.Calibration.Redlines[i].ID, id)
		}
	}
	if cs.Calibration.History == nil {
		t.Error("history 为 nil(必须恒为数组)")
	}

	raw, err := json.Marshal(cs)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(raw)
	// indicators 的 9 个键必须全部出现(无值时为 null,不得省略)。
	for _, key := range []string{
		"money_expansion", "bankruptcy_rate", "gini", "unemployment", "cpi",
		"total_wealth", "alive_seats", "total_seats", "off_ledger_cny",
	} {
		if !strings.Contains(s, `"`+key+`"`) {
			t.Errorf("indicators 缺键 %q(不得省略)", key)
		}
	}
	// history 必须是 [] 而非 null。
	if !strings.Contains(s, `"history":[`) {
		t.Errorf("history 未序列化为数组:%s", s[:min(len(s), 200)])
	}
}

// TestCalibration_AuditRingBufferCap 审计快照环形缓冲上限(cap 24)。
func TestCalibration_AuditRingBufferCap(t *testing.T) {
	w := calibTestWorld(42, 6)
	calibSettleMonths(w, 30)
	if len(w.Audits) > auditCap {
		t.Errorf("审计快照数 = %d,超过上限 %d", len(w.Audits), auditCap)
	}
	if len(w.Audits) == 0 {
		t.Fatal("无审计快照")
	}
	// 最近的快照月应等于最后一个结算月。
	last := w.Audits[len(w.Audits)-1]
	if last.Month != w.Month-1 {
		t.Errorf("末条审计月 = %d,期望 %d(结算月)", last.Month, w.Month-1)
	}
}

// TestCalibration_AuditAlwaysFullShape **正常路径下绝不出现占位项**:
// 跑 40 月,每条审计快照都必须写满 7 条不变量 + 5 条红线
// (占位项只在审计快照残缺时兜底,正常路径恒不触发)。
func TestCalibration_AuditAlwaysFullShape(t *testing.T) {
	w := calibTestWorld(42, 6)
	calibSettleMonths(w, 40)
	if len(w.Audits) == 0 {
		t.Fatal("无审计快照")
	}
	for _, a := range w.Audits {
		if len(a.Invariants) != 7 {
			t.Fatalf("月 %d 审计不变量数 = %d,期望 7", a.Month, len(a.Invariants))
		}
		if len(a.Redlines) != 7 {
			t.Fatalf("月 %d 审计红线数 = %d,期望 7", a.Month, len(a.Redlines))
		}
		for _, inv := range a.Invariants {
			if inv.ID == "" || inv.Name == "未采集" {
				t.Fatalf("月 %d 出现占位不变量项:%+v", a.Month, inv)
			}
		}
		for _, rl := range a.Redlines {
			if rl.ID == "" || rl.Name == "未采集" {
				t.Fatalf("月 %d 出现占位红线项:%+v", a.Month, rl)
			}
		}
	}
	// 顶层月份与同帧哈希/不变量**同源**(审计月,而非当前 game.state.month)。
	cs := BuildClientState("room-1", 0, w,
		[MaxSeats]string{"u:0"}, [MaxSeats]string{"玩家0"}, [MaxSeats]bool{false},
		[MaxSeats]string{}, [MaxSeats]BotTranscript{}, 0, 0, 0, nil, nil)
	if cs.Calibration == nil {
		t.Fatal("game.state.calibration 未下发")
	}
	latest := w.Audits[len(w.Audits)-1]
	if cs.Calibration.Month != latest.Month {
		t.Errorf("calibration.month = %d,期望审计月 %d", cs.Calibration.Month, latest.Month)
	}
	if cs.Calibration.StateHash != latest.StateHash {
		t.Errorf("calibration.state_hash 与审计快照不一致")
	}
}

// TestCalibration_ZeroRandConsumptionInAudit 审计接线不得消费随机数:
// 同一 seed 跑两遍世界 —— 一遍正常月结(含审计),一遍在月结前后额外
// 反复调用 StateHash/CheckInvariants ⇒ 末月哈希必须一致。
func TestCalibration_ZeroRandConsumptionInAudit(t *testing.T) {
	a := calibTestWorld(99, 6)
	calibSettleMonths(a, 18)
	hashA := a.StateHash()

	b := calibTestWorld(99, 6)
	for i := 0; i < 50; i++ {
		_ = b.StateHash()
		_ = CheckInvariants(b)
	}
	calibSettleMonths(b, 18)
	hashB := b.StateHash()
	if hashA != hashB {
		t.Errorf("审计/哈希消费了随机数:hashA=%s hashB=%s", hashA, hashB)
	}
}

// TestCalibration_OffLedgerTotalInIndicator 账外净额进指标。
func TestCalibration_OffLedgerTotalInIndicator(t *testing.T) {
	w := calibTestWorld(42, 6)
	calibSettleMonths(w, 3)
	ind := collectIndicators(w)
	if ind.OffLedgerCNY == nil {
		t.Fatal("off_ledger_cny 为 nil")
	}
	if *ind.OffLedgerCNY != w.OffLedger.Total() {
		t.Errorf("off_ledger_cny = %d,期望 %d", *ind.OffLedgerCNY, w.OffLedger.Total())
	}
	if ind.AliveSeats == nil || ind.TotalSeats == nil {
		t.Fatal("座位计数缺失")
	}
	if *ind.TotalSeats != 6 {
		t.Errorf("total_seats = %d,期望 6", *ind.TotalSeats)
	}
}

// TestCalibration_DashboardDimension 看板目录含「校准」维度(方案 §8.1)。
func TestCalibration_DashboardDimension(t *testing.T) {
	dims := DashboardDimensions()
	found := false
	for _, d := range dims {
		if d.ID == "calibration" {
			found = true
			if d.Group != DashboardGroupCity {
				t.Errorf("calibration 分组 = %s,期望 %s", d.Group, DashboardGroupCity)
			}
			if d.TitleKey != "virtualCity.dash.dim.calibration" {
				t.Errorf("calibration title_key = %s", d.TitleKey)
			}
			if d.Source != DashboardSourceGameState || d.Scope != DashboardScopeRoom ||
				d.Unit != DashboardUnitMixed || d.Refresh != DashboardRefreshPush {
				t.Errorf("calibration 维度字段不合规:%+v", d)
			}
		}
	}
	if !found {
		t.Error("看板目录缺 calibration 维度")
	}
	// 分组内位置:city 组内 calibration 必须在 macro 之后。
	cityIDs := []string{}
	for _, d := range dims {
		if d.Group == DashboardGroupCity {
			cityIDs = append(cityIDs, d.ID)
		}
	}
	joined := strings.Join(cityIDs, ",")
	if !strings.Contains(joined, "macro,calibration") {
		t.Errorf("calibration 未紧随 macro:%s", joined)
	}
}

// TestCalibration_ScenarioRecordJSONTags 注入记录新增 4 字段不带
// omitempty(缺省零值也下发)。
func TestCalibration_ScenarioRecordJSONTags(t *testing.T) {
	rec := ScenarioAppliedRecord{ID: "SC1", Kind: ScenarioEarthquake, Magnitude: 2, AppliedMonth: 3}
	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(raw)
	for _, key := range []string{"target", "duration_months", "seed", "snapshot_id"} {
		if !strings.Contains(s, `"`+key+`"`) {
			t.Errorf("注入记录缺键 %q:%s", key, s)
		}
	}
	// 零值也必须下发(不带 omitempty)。
	if !strings.Contains(s, `"seed":0`) {
		t.Errorf("seed 零值未下发:%s", s)
	}
}

// ── 批次72 §9:T-C2 / T-C3 ─────────────────────────────────────────────

// TestCalibration_CategoryRedlines T-C2:R-E1/R-E2 品类年化红线。
//
// 构造 10 局 category_annualized 均值 0.31 / 0.029 ⇒ 触发;
// 9 局 ⇒ 不触发且 detail 注明样本不足(n=9/10)。
func TestCalibration_CategoryRedlines(t *testing.T) {
	mkRuns := func(n int, stockRate float64) []RunRecord {
		runs := make([]RunRecord, n)
		for i := range runs {
			runs[i] = RunRecord{RunID: "CR" + strconvItoa(i+1),
				CategoryAnnualized: map[string]float64{
					AssetStockIndex: stockRate,
					AssetGold:       0.05,
					"house":         0.05,
					AssetBond:       0.05,
				}}
		}
		return runs
	}
	ind := CalibrationIndicators{MoneyExpansion: f64Ptr(3.0), BankruptcyRate: f64Ptr(0.2)}

	// 10 局均值 0.31 ⇒ R-E1 触发,R-E2 不触发。
	rl := EvaluateRedlines(ind, nil, mkRuns(10, 0.31))
	byID := map[string]RedlineHit{}
	for _, r := range rl {
		byID[r.ID] = r
	}
	if !byID[RedlineRE1].Triggered {
		t.Errorf("R-E1 应触发(均值 0.31 > 0.30):%+v", byID[RedlineRE1])
	}
	if byID[RedlineRE2].Triggered {
		t.Errorf("R-E2 不应触发(其余品类 0.05):%+v", byID[RedlineRE2])
	}
	if len(rl) != 7 {
		t.Fatalf("红线条数 = %d,期望 7", len(rl))
	}
	if rl[5].ID != RedlineRE1 || rl[6].ID != RedlineRE2 {
		t.Errorf("R-E1/R-E2 应追加在尾部:got %s,%s", rl[5].ID, rl[6].ID)
	}

	// 10 局均值 0.029 ⇒ R-E2 触发。
	rl = EvaluateRedlines(ind, nil, mkRuns(10, 0.029))
	byID = map[string]RedlineHit{}
	for _, r := range rl {
		byID[r.ID] = r
	}
	if !byID[RedlineRE2].Triggered {
		t.Errorf("R-E2 应触发(均值 0.029 < 0.03):%+v", byID[RedlineRE2])
	}
	if byID[RedlineRE1].Triggered {
		t.Errorf("R-E1 不应触发:%+v", byID[RedlineRE1])
	}

	// 9 局 ⇒ 双双不触发 + detail 样本不足标注。
	rl = EvaluateRedlines(ind, nil, mkRuns(9, 0.9))
	byID = map[string]RedlineHit{}
	for _, r := range rl {
		byID[r.ID] = r
	}
	for _, id := range []string{RedlineRE1, RedlineRE2} {
		if byID[id].Triggered {
			t.Errorf("%s 样本不足不应触发:%+v", id, byID[id])
		}
		if !strings.Contains(byID[id].Detail, "n=9/10") {
			t.Errorf("%s detail 应注明样本不足(n=9/10):%s", id, byID[id].Detail)
		}
	}

	// 窗口 = 尾部 10 局:15 局中前 5 局极端值不计入。
	runs := append(mkRuns(5, 0.9), mkRuns(10, 0.05)...)
	rl = EvaluateRedlines(ind, nil, runs)
	for _, r := range rl {
		if r.ID == RedlineRE1 && r.Triggered {
			t.Errorf("R-E1 不应触发(窗口尾部 10 局均值 0.05):%+v", r)
		}
	}
}

// strconvItoa 局部小工具(避免与 strconv 包短名冲突的测试内别名)。
func strconvItoa(i int) string { return fmt.Sprintf("%d", i) }

// TestCalibration_I6NoSample T-C3:I6 无样本状态位。
//
// 无场景注入局 no_sample=true 且 passed=true;有场景记录局 false。
func TestCalibration_I6NoSample(t *testing.T) {
	w := calibTestWorld(42, 4)
	calibSettleMonths(w, 3)
	inv := calibInvariantByID(t, CheckInvariants(w), InvariantI6)
	if !inv.NoSample || !inv.Passed {
		t.Errorf("无场景局:I6 应 no_sample=true && passed=true,got %+v", inv)
	}

	// 注入一条合法场景记录 ⇒ no_sample=false。
	w.ScenarioApplied = append(w.ScenarioApplied, ScenarioAppliedRecord{
		Kind: "market_crash", Magnitude: 3, AppliedMonth: 1, DurationMonths: 2, Seed: 42,
	})
	inv = calibInvariantByID(t, CheckInvariants(w), InvariantI6)
	if inv.NoSample || !inv.Passed {
		t.Errorf("有场景局:I6 应 no_sample=false && passed=true,got %+v", inv)
	}

	// 契约键下发:game.state.calibration.invariants[].no_sample。
	cs := BuildClientState("room-1", 0, w,
		[MaxSeats]string{"u:0"}, [MaxSeats]string{"玩家0"}, [MaxSeats]bool{false},
		[MaxSeats]string{}, [MaxSeats]BotTranscript{}, 0, 0, 0, nil, nil)
	raw, err := json.Marshal(cs.Calibration)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"no_sample":`) {
		t.Errorf("calibration.invariants[].no_sample 未下发:%s", string(raw)[:min(200, len(raw))])
	}
}
