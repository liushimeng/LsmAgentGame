// Package virtual_city — family_test.go: 代际财富转移引擎单测
// (批次52 §20261002-01;覆盖 §9 测试表 T1–T11 的 backend 部分)。
//
// 契约: lag_docs/虚拟城市/已实现/52-代际财富转移引擎/
// 虚拟城市-批次52-代际财富转移引擎-实施设计-v1.md §9。
package virtual_city

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"LsmAgentGame/errcode"
)

// familyWorld 构造单测世界:n 个座位,可选地按 seat 定制玩家。
func familyWorld(t *testing.T, seats int) *World {
	t.Helper()
	w := NewWorld(7, emptyCardsFor(seats))
	for s := 0; s < seats && s < MaxSeats; s++ {
		w.Players[s] = newPlayerFromCard(s, synthCard(s, 100000))
	}
	w.StartGame()
	return w
}

// findSeatWithDeathAge 返回 deathAge > wantMin 的座位号(找不到 Fatalf)。
func findSeatWithDeathAge(t *testing.T, wantMin int) int {
	t.Helper()
	for s := 0; s < MaxSeats; s++ {
		if deathAgeOf(s) > wantMin {
			return s
		}
	}
	t.Fatalf("no seat with deathAge > %d", wantMin)
	return -1
}

// ── T1 parentsAliveOf 单调性(裁决 D4) ──

// TestFamily_T1_ParentsAliveMonotonic 同 seat 随年龄单调(死不复活);
// 不同 seat 的寿数分布 ≠ 全同。
func TestFamily_T1_ParentsAliveMonotonic(t *testing.T) {
	seat := findSeatWithDeathAge(t, 75)
	w := familyWorld(t, 2)
	p := w.Players[seat%2]
	p.Seat = seat // 散列口径按 seat 计
	deathAge := deathAgeOf(seat)

	seenDead := false
	for age := 25; age <= 70; age++ {
		p.Age = age
		alive, da := w.parentsAliveOf(p)
		if da != deathAge {
			t.Fatalf("deathAge: got %d, want %d", da, deathAge)
		}
		if !alive {
			seenDead = true
		} else if seenDead {
			t.Fatalf("seat %d age %d: parents resurrected(死不复活)", seat, age)
		}
	}
	if !seenDead {
		// 单调性主体已验证;deathAge 很大时区间内全活是允许的(找的 seat 已保证 >75,
		// 父母 50–95 岁窗内必然出现死亡点,这里兜底提示)。
		t.Logf("seat %d deathAge=%d: 窗口内未见死亡(允许,单调性仍成立)", seat, deathAge)
	}

	// 不同 seat 分布 ≠ 全同。
	first := deathAgeOf(0)
	same := true
	for s := 1; s < MaxSeats; s++ {
		if deathAgeOf(s) != first {
			same = false
			break
		}
	}
	if same {
		t.Fatalf("deathAge all identical = %d(散列分布退化)", first)
	}
}

// ── T2 DistributeInheritance 三分支(裁决 D6/D8) ──

// TestFamily_T2_InheritanceThreeBranches 有配偶有子女 50/50;无配偶有子女
// 100% 均分;无配偶无子女 → EntityWorld。
func TestFamily_T2_InheritanceThreeBranches(t *testing.T) {
	const cash = int64(1200000)
	estate := cash - FuneralCNY // 1,195,000

	t.Run("married_with_kids", func(t *testing.T) {
		w := familyWorld(t, 2)
		p := w.Players[0]
		p.Cash = cash
		p.Family.Marital = "married"
		p.Family.Children = 2
		p.Card.ChildrenCount = 2
		before := w.Ledger.EntityNet(EntityFamily)
		if err := w.DistributeInheritance(0); err != nil {
			t.Fatalf("DistributeInheritance: %v", err)
		}
		got := w.Ledger.EntityNet(EntityFamily) - before
		if got != estate {
			t.Errorf("family net: got %d, want estate %d", got, estate)
		}
		spouse := estate / 2
		perChild := (estate - spouse) / 2
		byNote := map[string]int64{}
		for _, e := range w.Ledger.Entries {
			if e.Category == CatInheritance && e.From == SeatEntity(0) {
				byNote[e.Note] += e.AmountCNY
			}
		}
		if byNote["遗产继承-配偶"] != spouse {
			t.Errorf("配偶: got %d, want %d", byNote["遗产继承-配偶"], spouse)
		}
		if byNote["遗产继承-子女1"] != perChild {
			t.Errorf("子女1: got %d, want %d", byNote["遗产继承-子女1"], perChild)
		}
		if byNote["遗产继承-子女2"] != perChild {
			t.Errorf("子女2: got %d, want %d", byNote["遗产继承-子女2"], perChild)
		}
		if p.Cash != 0 || len(p.Assets) != 0 || len(p.Loans) != 0 {
			t.Errorf("死者账簿未清零: cash=%d", p.Cash)
		}
	})

	t.Run("single_parent_with_kids", func(t *testing.T) {
		w := familyWorld(t, 2)
		p := w.Players[0]
		p.Cash = cash
		p.Family.Marital = "single"
		p.Family.Children = 2
		p.Card.ChildrenCount = 2
		before := w.Ledger.EntityNet(EntityFamily)
		if err := w.DistributeInheritance(0); err != nil {
			t.Fatalf("DistributeInheritance: %v", err)
		}
		got := w.Ledger.EntityNet(EntityFamily) - before
		if got != estate {
			t.Errorf("family net: got %d, want estate %d", got, estate)
		}
		perChild := estate / 2
		for _, note := range []string{"遗产继承-子女1", "遗产继承-子女2"} {
			var sum int64
			for _, e := range w.Ledger.Entries {
				if e.Category == CatInheritance && e.Note == note {
					sum += e.AmountCNY
				}
			}
			if sum != perChild {
				t.Errorf("%s: got %d, want %d", note, sum, perChild)
			}
		}
	})

	t.Run("no_heir_to_world", func(t *testing.T) {
		w := familyWorld(t, 2)
		p := w.Players[0]
		p.Cash = cash
		p.Family.Marital = "single"
		p.Family.Children = 0
		p.Card.ChildrenCount = 0
		beforeWorld := w.Ledger.EntityNet(EntityWorld)
		beforeFam := w.Ledger.EntityNet(EntityFamily)
		if err := w.DistributeInheritance(0); err != nil {
			t.Fatalf("DistributeInheritance: %v", err)
		}
		if got := w.Ledger.EntityNet(EntityFamily) - beforeFam; got != 0 {
			t.Errorf("family net: got %d, want 0(充公分支不入 family)", got)
		}
		if got := w.Ledger.EntityNet(EntityWorld) - beforeWorld; got != estate {
			t.Errorf("world net: got %d, want estate %d", got, estate)
		}
	})
}

// ── T3 丧葬费 / clamp / 幂等(裁决 D8) ──

// TestFamily_T3_FuneralClampIdempotent estate=0 不 panic;理赔入池后分配正确;
// 同 seat 二次调用幂等(仅记一次 log)。
func TestFamily_T3_FuneralClampIdempotent(t *testing.T) {
	t.Run("estate_zero_no_panic", func(t *testing.T) {
		w := familyWorld(t, 2)
		p := w.Players[0]
		p.Cash = 1000 // 不足丧葬费 → estate=0
		p.Family.Marital = "married"
		p.Family.Children = 1
		p.Card.ChildrenCount = 1
		if err := w.DistributeInheritance(0); err != nil {
			t.Fatalf("estate=0 不应报错: %v", err)
		}
		if len(w.InheritanceLog) != 0 {
			t.Errorf("estate=0 不应记 log: %d 条", len(w.InheritanceLog))
		}
		if p.Cash != 0 || len(p.Assets) != 0 {
			t.Errorf("死者账簿未清零: cash=%d", p.Cash)
		}
	})

	t.Run("claim_pooled_then_distributed", func(t *testing.T) {
		w := familyWorld(t, 2)
		p := w.Players[0]
		p.Cash = 0
		p.Family.Marital = "single"
		p.Family.Children = 1
		p.Card.ChildrenCount = 1
		// 模拟身故理赔入死者 Cash(insurer→seat,CatClaim)。
		claim := int64(500000)
		w.Pay(0, EntityInsurer, SeatEntity(0), claim, CatClaim, "身故理赔-定期寿险")
		if err := w.DistributeInheritance(0); err != nil {
			t.Fatalf("DistributeInheritance: %v", err)
		}
		want := claim - FuneralCNY
		var got int64
		for _, e := range w.Ledger.Entries {
			if e.Category == CatInheritance && e.From == SeatEntity(0) {
				got += e.AmountCNY
			}
		}
		if got != want {
			t.Errorf("继承实付: got %d, want claim−丧葬 = %d", got, want)
		}
	})

	t.Run("idempotent_second_call", func(t *testing.T) {
		w := familyWorld(t, 2)
		p := w.Players[0]
		p.Cash = 1000000
		p.Family.Marital = "married"
		p.Family.Children = 1
		p.Card.ChildrenCount = 1
		if err := w.DistributeInheritance(0); err != nil {
			t.Fatalf("first: %v", err)
		}
		if err := w.DistributeInheritance(0); err != nil {
			t.Fatalf("second: %v", err)
		}
		if len(w.InheritanceLog) != 1 {
			t.Errorf("log 条数: got %d, want 1(幂等)", len(w.InheritanceLog))
		}
	})

	t.Run("clamp_to_cash", func(t *testing.T) {
		// 资产为主、现金不足:实付 clamp 到死者现金,不透支(批次52 §4)。
		w := familyWorld(t, 2)
		p := w.Players[0]
		p.Cash = 10000
		p.Assets = []Asset{{Kind: AssetGold, Units: 100, CostCNY: 0}} // 金市值约 100×GoldPrice
		p.Family.Marital = "married"
		p.Family.Children = 0
		p.Card.ChildrenCount = 0
		if err := w.DistributeInheritance(0); err != nil {
			t.Fatalf("DistributeInheritance: %v", err)
		}
		var paid int64
		for _, e := range w.Ledger.Entries {
			if e.Category == CatInheritance && e.From == SeatEntity(0) {
				paid += e.AmountCNY
			}
		}
		maxPayable := int64(10000) - FuneralCNY
		if paid > maxPayable {
			t.Errorf("实付 %d 超过死者现金可付额度 %d(透支?)", paid, maxPayable)
		}
		if p.Cash != 0 {
			t.Errorf("清零后现金: got %d, want 0", p.Cash)
		}
	})
}

// ── T4 MonthlyFamilySupport(裁决 D10/D5) ──

// TestFamily_T4_MonthlyFamilySupport 健康 fair → ×1.5;贫困(NetWorth<中位)→
// ×0.5;两者可叠加;父母未满 60 → 0。
func TestFamily_T4_MonthlyFamilySupport(t *testing.T) {
	seat := findSeatWithDeathAge(t, 80)

	t.Run("under_60_zero", func(t *testing.T) {
		w := familyWorld(t, 4)
		p := w.Players[0]
		p.Seat = seat
		p.Age = 30 // 父母 55 岁 < 60
		if got := w.monthlyFamilySupport(p); got != 0 {
			t.Errorf("父母 55 岁: got %d, want 0", got)
		}
	})

	t.Run("fair_x1_5", func(t *testing.T) {
		w := familyWorld(t, 4)
		p := w.Players[0]
		p.Seat = seat
		p.Age = 40        // 父母 65 岁 → fair
		p.Cash = 10000000 // 远高于中位数,无能力减免
		for _, s := range []int{1, 2, 3} {
			w.Players[s].Cash = 1000 // 压低中位数
		}
		if h := parentsHealthOf(w.parentsAgeOf(p)); h != "fair" {
			t.Fatalf("health: got %s, want fair", h)
		}
		want := int64(float64(FamilySupportBase) * FamilySupportFair)
		if got := w.monthlyFamilySupport(p); got != want {
			t.Errorf("fair: got %d, want %d", got, want)
		}
	})

	t.Run("poor_and_ability_cut_stack", func(t *testing.T) {
		w := familyWorld(t, 4)
		p := w.Players[0]
		p.Seat = seat
		p.Age = 50   // 父母 75 岁 → poor
		p.Cash = 100 // 贫困(NetWorth < 中位数)
		for _, s := range []int{1, 2, 3} {
			w.Players[s].Cash = 1000000
		}
		if h := parentsHealthOf(w.parentsAgeOf(p)); h != "poor" {
			t.Fatalf("health: got %s, want poor", h)
		}
		want := int64(float64(FamilySupportBase) * FamilySupportPoor * FamilyAbilityCut) // 1875
		if got := w.monthlyFamilySupport(p); got != want {
			t.Errorf("poor×ability: got %d, want %d(两者可叠加)", got, want)
		}
	})
}

// ── T5 子女回流(裁决 D12) ──

// TestFamily_T5_ChildSupportIn 22 岁前 0;22 岁后 1500;私立 ×1.2;计入收入侧。
func TestFamily_T5_ChildSupportIn(t *testing.T) {
	w := familyWorld(t, 2)
	p := w.Players[0]
	p.Card.ChildrenCount = 0
	p.Family.Children = 1
	p.BirthMonths = []int{1} // 事件生育:age = (Month−1)/12

	// 21 岁前 0。
	w.Month = 1 + 21*12
	if got := w.monthlyChildSupportIn(p); got != 0 {
		t.Errorf("21 岁: got %d, want 0", got)
	}
	// 22 岁 → 1500。
	w.Month = 1 + 22*12
	if got := w.monthlyChildSupportIn(p); got != ChildSupportBase {
		t.Errorf("22 岁公立: got %d, want %d", got, ChildSupportBase)
	}
	// 私立 ×1.2 = 1800。
	p.PrivateEduMask = []bool{true}
	want := int64(float64(ChildSupportBase) * (1 + EduPrivateROI))
	if got := w.monthlyChildSupportIn(p); got != want {
		t.Errorf("22 岁私立: got %d, want %d", got, want)
	}
	// 计入收入侧(family→seat,CatChildSupport)。同月可能还有赡养/教育支出,
	// 按三通道净额断言现金变化。
	p.Cash = 10000
	before := p.Cash
	sup, edu, childIn := w.settleFamily(p)
	if childIn != want {
		t.Errorf("settleFamily childIn: got %d, want %d(sup=%d edu=%d)", childIn, want, sup, edu)
	}
	if p.Cash != before-sup-edu+childIn {
		t.Errorf("cash: got %d, want %d(收入侧 net)", p.Cash, before-sup-edu+childIn)
	}
	if p.ChildSupportReceivedCNY != want {
		t.Errorf("累计回流: got %d, want %d", p.ChildSupportReceivedCNY, want)
	}
}

// ── T6 教育升级(裁决 D11) ──

// TestFamily_T6_UpgradeEducation 现金不足拒绝;成功后 mask 置位、月费生效、累计正确。
func TestFamily_T6_UpgradeEducation(t *testing.T) {
	w := familyWorld(t, 2)
	p := w.Players[0]
	p.Card.ChildrenCount = 0
	p.Family.Children = 1
	p.BirthMonths = []int{1}
	p.ActionBudget = 1
	w.Month = 1 + 10*12 // 子女 10 岁 ∈[3,18]

	// 现金不足拒绝。
	p.Cash = 1000
	if _, ec := w.actUpgradeEducation(p, Action{Type: ActUpgradeEducation, ChildIdx: 0}); ec == nil ||
		ec.Code != errcode.ErrVirtualCityInsufficientCash {
		t.Fatalf("现金不足: got %v, want 35007", ec)
	}
	// child_idx 越界拒绝。
	p.Cash = 250000
	if _, ec := w.actUpgradeEducation(p, Action{Type: ActUpgradeEducation, ChildIdx: 3}); ec == nil ||
		ec.Code != errcode.ErrVirtualCityFamilyInvalid {
		t.Fatalf("child_idx 越界: got %v, want 35046", ec)
	}
	// 成功:扣款、置私立、累计、月费生效。
	p.ActionBudget = 1
	cashBefore := p.Cash
	text, ec := w.actUpgradeEducation(p, Action{Type: ActUpgradeEducation, ChildIdx: 0})
	if ec != nil {
		t.Fatalf("upgrade: %v", ec)
	}
	if !strings.Contains(text, "私立") {
		t.Errorf("text: %s", text)
	}
	if p.Cash != cashBefore-PrivateEduInitCNY {
		t.Errorf("cash: got %d, want %d", p.Cash, cashBefore-PrivateEduInitCNY)
	}
	if childEduOf(p, 0) != "private" {
		t.Errorf("edu: got %s, want private", childEduOf(p, 0))
	}
	if p.EducationTotalCNY != PrivateEduInitCNY {
		t.Errorf("EducationTotalCNY: got %d, want %d", p.EducationTotalCNY, PrivateEduInitCNY)
	}
	if got := w.monthlyEduTuition(p); got != PrivateEduMonthly {
		t.Errorf("月费: got %d, want %d", got, PrivateEduMonthly)
	}
	// 19 岁起月费停止(裁决 D11)。
	w.Month = 1 + 19*12
	if got := w.monthlyEduTuition(p); got != 0 {
		t.Errorf("19 岁月费: got %d, want 0", got)
	}
}

// ── T7 FamilyScore(裁决 D14) ──

// TestFamily_T7_FamilyScore 教育 20 万 + 赡养 54 万×0.3 → min(100, 362000/20000)=18.1。
func TestFamily_T7_FamilyScore(t *testing.T) {
	w := familyWorld(t, 1)
	p := w.Players[0]
	p.EducationTotalCNY = 200000
	p.FamilySupportTotalCNY = 540000
	want := (200000.0 + 540000.0*0.3) / 20000.0 // 18.1
	if got := w.FamilyScore(p); math.Abs(got-want) > 1e-9 {
		t.Errorf("FamilyScore: got %f, want %f", got, want)
	}
	// 100 封顶。
	p.EducationTotalCNY = 10_000_000
	if got := w.FamilyScore(p); got != 100 {
		t.Errorf("cap: got %f, want 100", got)
	}
}

// ── T8 Ledger 断言(批次52 §3.3) ──

// TestFamily_T8_LedgerWhitelist family 四类双向可达;family 实体仅可付
// CatChildSupport(白名单守卫照 EntityGov 风格)。
func TestFamily_T8_LedgerWhitelist(t *testing.T) {
	l := &Ledger{}
	// 三类收入方向合法:seat→family。
	l.Record(1, SeatEntity(0), EntityFamily, 100, CatInheritance, "")
	l.Record(1, SeatEntity(0), EntityFamily, 100, CatFamilySupport, "")
	l.Record(1, SeatEntity(0), EntityFamily, 100, CatEduTuition, "")
	// 唯一出账方向合法:family→seat CatChildSupport。
	l.Record(1, EntityFamily, SeatEntity(0), 100, CatChildSupport, "")

	// 非法:family 收其他类目。
	assertPanic(t, "family 收 tax", func() {
		l.Record(1, SeatEntity(0), EntityFamily, 1, CatTax, "")
	})
	// 非法:family 付其他类目。
	assertPanic(t, "family 付 living", func() {
		l.Record(1, EntityFamily, SeatEntity(0), 1, CatLiving, "")
	})
	// 非法:family 付非座位。
	assertPanic(t, "family 付 world", func() {
		l.Record(1, EntityFamily, EntityWorld, 1, CatChildSupport, "")
	})
	// 非法:非座位付 family。
	assertPanic(t, "bank 付 family", func() {
		l.Record(1, EntityBank, EntityFamily, 1, CatFamilySupport, "")
	})
	// 遗产充公:seat→world CatInheritance 合法(裁决 D6)。
	l.Record(1, SeatEntity(1), EntityWorld, 100, CatInheritance, "遗产充公")
}

// assertPanic 断言 fn 必 panic(ledger Record 的开发期不变量)。
func assertPanic(t *testing.T, name string, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("%s: 应 panic 未 panic", name)
		}
	}()
	fn()
}

// ── T9 view 序列化(裁决 D15 / 批次52 §7) ──

// TestFamily_T9_ViewSerialization family stats/inheritance_log(≤5,降序);
// my.family 三段;降级 omit。
func TestFamily_T9_ViewSerialization(t *testing.T) {
	t.Run("stats_and_log", func(t *testing.T) {
		w := familyWorld(t, 3)
		w.Players[0].Family.Children = 2
		w.Players[0].Card.ChildrenCount = 2
		w.Players[1].Family.Children = 1
		w.Players[1].Card.ChildrenCount = 1
		w.Players[1].PrivateEduMask = []bool{true}
		stats := w.buildFamilyStatsJSON()
		if stats == nil {
			t.Fatal("stats nil")
		}
		if stats.ChildrenCount != 3 {
			t.Errorf("children_count: got %d, want 3", stats.ChildrenCount)
		}
		if stats.PrivateEduCount != 1 {
			t.Errorf("private_edu_count: got %d, want 1", stats.PrivateEduCount)
		}
		// 7 条 log → 只留 5,降序。
		for i := 0; i < 7; i++ {
			w.InheritanceLog = append(w.InheritanceLog, InheritanceEvent{
				FromSeat: i, ToSeats: []int{}, AmountCNY: int64(100 + i), Month: i + 1,
			})
		}
		log := w.inheritanceLogJSON()
		if len(log) != 5 {
			t.Fatalf("log len: got %d, want 5", len(log))
		}
		if log[0].Month != 7 || log[4].Month != 3 {
			t.Errorf("降序: got month %d..%d, want 7..3", log[0].Month, log[4].Month)
		}
		// to_seats 恒空数组(板外,裁决 D6/D7),序列化非 null。
		b, _ := json.Marshal(log[0])
		if !strings.Contains(string(b), `"to_seats":[]`) {
			t.Errorf("to_seats 应为空数组: %s", b)
		}
	})

	t.Run("my_family_sections", func(t *testing.T) {
		w := familyWorld(t, 2)
		p := w.Players[0]
		p.Family.Marital = "married"
		p.Family.Children = 1
		p.Card.ChildrenCount = 1
		p.FamilySupportTotalCNY = 3000
		p.EducationTotalCNY = 200000
		p.ChildSupportReceivedCNY = 1500
		my := w.buildMyFamilyJSON(p)
		if my.Parents == nil || len(my.Kids) != 1 || my.Totals == nil {
			t.Fatalf("三段缺失: parents=%v kids=%v totals=%v", my.Parents, my.Kids, my.Totals)
		}
		if my.Totals.Support != 3000 || my.Totals.Education != 200000 || my.Totals.ChildReceived != 1500 {
			t.Errorf("totals: %+v", my.Totals)
		}
	})

	t.Run("degrade_omit", func(t *testing.T) {
		w := familyWorld(t, 2)
		w.FamilyEnabled = false
		p := w.Players[0]
		my := w.buildMyFamilyJSON(p)
		if my.Parents != nil || len(my.Kids) != 0 || my.Totals != nil {
			t.Errorf("family_enabled=false 应 omit 明细段: %+v", my)
		}
		b, _ := json.Marshal(my)
		for _, key := range []string{"parents", "kids", "totals", "support_cny"} {
			if strings.Contains(string(b), key) {
				t.Errorf("omit 失败,key=%s: %s", key, b)
			}
		}
		if w.buildFamilyStatsJSON() != nil {
			t.Error("family_enabled=false stats 应 nil")
		}
		// insurance_enabled=false 同样 omit(批次52 §7)。
		w.FamilyEnabled = true
		w.InsuranceEnabled = false
		my2 := w.buildMyFamilyJSON(p)
		if my2.Parents != nil {
			t.Error("insurance_enabled=false 应 omit 明细段")
		}
	})
}

// ── T11 总分权重 45/25/15/15(裁决 D14) ──

// TestFamily_T11_FinalScoreWeights total = fi×0.45 + life×0.25 + social×0.15 + family×0.15。
func TestFamily_T11_FinalScoreWeights(t *testing.T) {
	w := familyWorld(t, 2)
	p := w.Players[0]
	p.EducationTotalCNY = 200000
	p.FamilySupportTotalCNY = 540000
	scores := w.FinalScores()
	if len(scores) == 0 {
		t.Fatal("no scores")
	}
	for _, s := range scores {
		want := s.FIScore*0.45 + s.LifeScore*0.25 + s.SocialScore*0.15 + s.FamilyScore*0.15
		if math.Abs(s.Total-want) > 1e-9 {
			t.Errorf("seat %d total: got %f, want 45/25/15/15 → %f", s.Seat, s.Total, want)
		}
	}
	// family_score 字段确实进入了输出。
	var me FinalScore
	for _, s := range scores {
		if s.Seat == 0 {
			me = s
		}
	}
	if math.Abs(me.FamilyScore-18.1) > 1e-9 {
		t.Errorf("family_score: got %f, want 18.1", me.FamilyScore)
	}
}

// ── 月结接线(批次52 §5)+ HandleDeath 接线(§130) ──

// TestFamily_SettleMonthWiring 月结三通道落账;family_enabled=false 零接线。
func TestFamily_SettleMonthWiring(t *testing.T) {
	seat := findSeatWithDeathAge(t, 80)
	w := familyWorld(t, 2)
	p := w.Players[0]
	p.Seat = seat
	p.Age = 50 // 父母 75 → poor,必有赡养
	p.Cash = 1000000
	for _, s := range []int{1} {
		w.Players[s].Cash = 1000 // 压低中位数 → 能力减免
	}
	w.SettleMonth()
	if p.FamilySupportTotalCNY == 0 {
		t.Error("月结赡养未落账")
	}
	found := false
	for _, f := range p.Monthly.Detail {
		if f.Key == "family_support" {
			found = true
			if f.AmountCNY >= 0 {
				t.Errorf("赡养应为负项: %d", f.AmountCNY)
			}
		}
	}
	if !found {
		t.Error("Monthly.Detail 缺 family_support 行")
	}

	// family_enabled=false 零接线。
	w2 := familyWorld(t, 2)
	w2.FamilyEnabled = false
	p2 := w2.Players[0]
	p2.Seat = seat
	p2.Age = 50
	p2.Cash = 1000000
	w2.SettleMonth()
	if p2.FamilySupportTotalCNY != 0 {
		t.Errorf("family_enabled=false 应零接线: %d", p2.FamilySupportTotalCNY)
	}
}

// TestFamily_HandleDeathWiresInheritance §130:DistributeInheritance 被
// HandleDeath 真实调用;family_enabled=false 不分配。
func TestFamily_HandleDeathWiresInheritance(t *testing.T) {
	w := familyWorld(t, 2)
	p := w.Players[0]
	p.Cash = 800000
	p.Family.Marital = "married"
	p.Family.Children = 1
	p.Card.ChildrenCount = 1
	w.HandleDeath(0, "意外身故")
	if p.Alive {
		t.Fatal("应已身故")
	}
	if len(w.InheritanceLog) != 1 {
		t.Errorf("HandleDeath 未接线 DistributeInheritance: log=%d", len(w.InheritanceLog))
	}
	if p.Cash != 0 {
		t.Errorf("死者账簿未清零: %d", p.Cash)
	}

	// 回滚阀:family_enabled=false 不触发分配。
	w2 := familyWorld(t, 2)
	w2.FamilyEnabled = false
	p2 := w2.Players[0]
	p2.Cash = 800000
	w2.HandleDeath(0, "意外身故")
	if len(w2.InheritanceLog) != 0 || p2.Cash != 800000 {
		t.Errorf("family_enabled=false 应保持旧行为: log=%d cash=%d", len(w2.InheritanceLog), p2.Cash)
	}
}

// ── T10 工具侧动作闸(裁决 D13;vcplayer 派发另测) ──

// TestFamily_T10_ActionsBudgetAndCap 动作预算闸 + 加赡养上限 clamp + 累计。
func TestFamily_T10_ActionsBudgetAndCap(t *testing.T) {
	w := familyWorld(t, 2)
	p := w.Players[0]
	p.Cash = 100000
	p.NetWorthHistory = nil

	// 预算闸。
	p.ActionBudget = 0
	if _, ec := w.actPaySupportExtra(p, Action{Type: ActPaySupportExtra, AmountCNY: 100}); ec == nil ||
		ec.Code != errcode.ErrVirtualCityActionBudgetExhausted {
		t.Fatalf("预算闸: got %v, want 35006", ec)
	}

	// 上限 clamp:NetWorth×30%。
	p.ActionBudget = 1
	nw := p.NetWorth(w.Market)
	cap64 := int64(float64(nw) * 0.30)
	if _, ec := w.actPaySupportExtra(p, Action{Type: ActPaySupportExtra, AmountCNY: cap64 + 1}); ec == nil ||
		ec.Code != errcode.ErrVirtualCityFamilyInvalid {
		t.Fatalf("超上限应拒绝: got %v, want 35046", ec)
	}
	// 合法金额成功 + 累计 + Energy −1。
	p.ActionBudget = 1
	energyBefore := p.Energy
	text, ec := w.actPaySupportExtra(p, Action{Type: ActPaySupportExtra, AmountCNY: 1000})
	if ec != nil {
		t.Fatalf("pay_support_extra: %v", ec)
	}
	if !strings.Contains(text, "加赡养") {
		t.Errorf("text: %s", text)
	}
	if p.FamilySupportTotalCNY != 1000 || p.FamilySupportExtraCNY != 1000 {
		t.Errorf("累计: total=%d extra=%d, want 1000/1000", p.FamilySupportTotalCNY, p.FamilySupportExtraCNY)
	}
	if p.Energy != energyBefore-1 {
		t.Errorf("Energy: got %d, want %d", p.Energy, energyBefore-1)
	}
	// family_enabled=false 拒绝。
	w.FamilyEnabled = false
	p.ActionBudget = 1
	if _, ec := w.actPaySupportExtra(p, Action{Type: ActPaySupportExtra, AmountCNY: 100}); ec == nil ||
		ec.Code != errcode.ErrVirtualCityFamilyDisabled {
		t.Fatalf("family_enabled=false: got %v, want 35045", ec)
	}
}

// TestFamily_PlanInheritancePreview 遗产预览不落账(裁决 D13)。
func TestFamily_PlanInheritancePreview(t *testing.T) {
	w := familyWorld(t, 2)
	p := w.Players[0]
	p.Cash = 1000000
	p.Family.Marital = "married"
	p.Family.Children = 1
	p.Card.ChildrenCount = 1
	text := w.planInheritance(p)
	if !strings.Contains(text, "配偶") {
		t.Errorf("preview: %s", text)
	}
	if len(w.InheritanceLog) != 0 {
		t.Error("plan_inheritance 不应落账")
	}
	// 无遗产场景。
	p.Cash = 100
	p.Assets = nil
	p.Loans = nil
	if txt := w.planInheritance(p); !strings.Contains(txt, "无遗产") {
		t.Errorf("空遗产 preview: %s", txt)
	}
}

// ── 附:hash16 确定性(裁决 D4) ──

// TestFamily_Hash16Deterministic 同 (seat, age) 恒定;折叠域 ⊂ [0,65535];
// 禁用 w.Rand —— 两次调用不推进 Rand 流。
func TestFamily_Hash16Deterministic(t *testing.T) {
	if hash16(3, 50) != hash16(3, 50) {
		t.Error("hash16 不确定")
	}
	for s := 0; s < 8; s++ {
		for a := 50; a < 60; a++ {
			if v := hash16(s, a); v > 65535 {
				t.Fatalf("hash16(%d,%d)=%d 超域", s, a, v)
			}
		}
	}
	w := familyWorld(t, 1)
	before := w.Rand.Int63()
	w2 := familyWorld(t, 1)
	_ = w2.parentsAgeOf(w2.Players[0])
	_, _ = w2.parentsAliveOf(w2.Players[0])
	_ = w2.childAgeOf(w2.Players[0], 0)
	after := w2.Rand.Int63()
	// 两个独立同种子世界:Rand 流应一致(派生函数零 rand 消耗)。
	w3 := familyWorld(t, 1)
	mid := w3.Rand.Int63()
	if before != mid || mid != after {
		t.Errorf("派生函数消耗了 Rand 流: %d/%d/%d", before, mid, after)
	}
}
