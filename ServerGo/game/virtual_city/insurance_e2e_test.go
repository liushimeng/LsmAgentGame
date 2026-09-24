// Package virtual_city — insurance_e2e_test.go: P1-4 商业保险引擎级 e2e 跨模块验证
// (2026-09-19 integration-tester,契约文档 §12.2 验收基准)。
//
// 与 insurance_test.go(单元级)的差异:本文件构造 12 座位全 bot 世界(引擎直驱,
// 不调真实 LLM;Agent 工具路径经 AgentRunner 桩式直调),跑 ≥24 个月完整
// SettleMonth,验证保险与 settlement/events/ledger/view 四模块的端到端咬合:
//
//	(a) 至少一个座位产生保单与 CatPremium 保费流水;
//	(b) 全程逐月 I1 守恒(VerifySeatConservation,含投保动作腿);
//	(c) insurer EntityNet = Σ保费 − Σ理赔(注入确定性理赔前后各验一次);
//	(d) 月结 detail key=insurance 金额口径 == 该月该座位 CatPremium 流水合计,
//	    且经 BuildClientState 透出到 my.monthly.detail / my.insurance / insured_kinds;
//	(e) insurance_enabled=false rand 零偏移回归 —— 已由
//	    TestInsurance_DisabledRandZeroOffset(insurance_test.go)覆盖,此处不重复。
//
// §92a 锁纪律(见 TestInsuranceE2E_ConcurrentActionSettleDeathRace 注释):
// HandleDeath 仅由 rollAccident ← MonthlyEvents ← SettleMonth ← trySettle 调用,
// 全程持 r.mu;不存在「月结一把锁 + WS 动作另一把锁」的双锁竞态。
package virtual_city

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"LsmAgentGame/errcode"
)

// ── 构造 helpers ──────────────────────────────────────────────────────

// insE2EWorld 12 座位引擎级世界(全 bot 语义:合成卡 + 30 万初始储蓄)。
// seat 3 零工资:MonthlyEvents 失业掷骰跳过(该座位收入恒 0),便于在月 8/9
// 构造「现金不足 → 宽限 → 失效」断缴链路。
func insE2EWorld(seed int64) *World {
	cards := emptyCardsFor(MaxSeats)
	for seat := 0; seat < MaxSeats; seat++ {
		c := synthCard(seat, 300000)
		if seat == 3 {
			c.Salary = 0
		}
		cards[seat] = c
	}
	w := NewWorld(seed, cards)
	w.StartGame()
	return w
}

// insE2EKindFor 座位 → 险种分配(12 座均分 4 险种,覆盖全部 kind)。
func insE2EKindFor(seat int) string {
	switch {
	case seat < 3:
		return InsCriticalIllness
	case seat < 6:
		return InsMedicalMillion
	case seat < 9:
		return InsTermLife
	default:
		return InsAccident
	}
}

// insE2ESumCat 全账本按 category 求和。
func insE2ESumCat(w *World, cat string) int64 {
	var total int64
	for _, e := range w.Ledger.Entries {
		if e.Category == cat {
			total += e.AmountCNY
		}
	}
	return total
}

// insE2EPremiumOfMonth 指定座位指定月份的 CatPremium 流水合计。
func insE2EPremiumOfMonth(w *World, seat, month int) int64 {
	var total int64
	for _, e := range w.Ledger.MonthEntries(month) {
		if e.From == SeatEntity(seat) && e.To == EntityInsurer && e.Category == CatPremium {
			total += e.AmountCNY
		}
	}
	return total
}

// insE2ERoom 12 bot 房间(固定种子,无 LLM registry;AgentEnabled=false →
// EnsureAgents/wakeBots 均为 no-op,月结由测试显式驱动)。
func insE2ERoom(t *testing.T, roomID string, seed int64) *VirtualCityRoom {
	t.Helper()
	m := NewManager(Config{MonthMs: 30000}, nil)
	m.ApplyRoomOptions(roomID, &VirtualCityRoomOptions{MonthMs: 30000, Seed: seed})
	r := m.CreateRoom(roomID)
	users := make(map[int]string, MaxSeats)
	models := make(map[int]string, MaxSeats)
	for seat := 0; seat < MaxSeats; seat++ {
		users[seat] = fmt.Sprintf("ins-e2e-bot-%d", seat)
		models[seat] = fmt.Sprintf("FakeModel%d", seat)
	}
	r.RegisterBotSeats(users, models)
	if e := r.Start(nil); e != nil {
		t.Fatalf("start room: %v", e)
	}
	return r
}

// ── (1) 引擎级 e2e:12 座位 × 26 个月 ─────────────────────────────────

// TestInsuranceE2E_TwelveSeats_26Months 全链路 e2e:
// 投保(ApplyAction,人类 WS 与 Agent 工具同一入口)→ 26 次完整 SettleMonth
// (跨月 12/24 两个年结重定价点)→ 断缴链路(grace→lapse)→ 注入确定性医疗
// 理赔 → view(BuildClientState)透出。断言 (a)(b)(c)(d) 全量口径。
func TestInsuranceE2E_TwelveSeats_26Months(t *testing.T) {
	w := insE2EWorld(20260919)

	// 月 1 投保:12 座位各买 1 险种(覆盖全部 4 kind)。
	cashBeforeBuy := make([]int64, MaxSeats)
	for seat := 0; seat < MaxSeats; seat++ {
		cashBeforeBuy[seat] = w.Players[seat].Cash
	}
	for seat := 0; seat < MaxSeats; seat++ {
		if _, e := w.ApplyAction(seat, Action{Type: ActBuyInsurance, Kind: insE2EKindFor(seat)}); e != nil {
			t.Fatalf("seat %d buy %s: %v", seat, insE2EKindFor(seat), e)
		}
	}
	// 投保动作腿 I1 与首次月结合并校验:月 1 全窗口(投保 + 月结)现金变化
	// == 月 1 流水净额(见下方循环 m==0 的 base 回退)。
	// (a) 前置:12 条首月保费流水已入账(每座位 1 条)。
	var firstMonthEntries int
	for seat := 0; seat < MaxSeats; seat++ {
		n, _ := insPremiumCount(w, seat)
		firstMonthEntries += n
	}
	if firstMonthEntries != MaxSeats {
		t.Fatalf("(a) first-month premium entries = %d, want %d", firstMonthEntries, MaxSeats)
	}

	const runMonths = 26 // ≥24;跨月 12 / 24 两个年结重定价点
	var seat3GraceMonth, seat3LapseMonth int
	for m := 0; m < runMonths; m++ {
		settledMonth := w.Month // SettleMonth ⑤ 会自增 w.Month,校验须用结算月
		// 月 8/9 构造 seat 3 断缴(现金不足月缴 83):结算月 8 → grace,9 → lapse。
		if settledMonth == 8 || settledMonth == 9 {
			w.Players[3].Cash = 0
		}
		before := make([]int64, MaxSeats)
		for seat := 0; seat < MaxSeats; seat++ {
			before[seat] = w.Players[seat].Cash
		}
		w.SettleMonth()

		for seat := 0; seat < MaxSeats; seat++ {
			p := w.Players[seat]
			// (b) 逐月 I1 守恒(含意外身故 / 破产清算等一切月结路径)。
			// m==0(结算月 1)基线回退到投保前:首月保费在动作腿入账月 1。
			base := before[seat]
			if m == 0 {
				base = cashBeforeBuy[seat]
			}
			if diff := w.Ledger.VerifySeatConservation(seat, settledMonth, base, p.Cash); diff != 0 {
				t.Fatalf("I1 broken seat %d month %d: diff=%d", seat, settledMonth, diff)
			}
			// (d) 月结 detail 口径:Σ|insurance 明细| == 该月该座位 CatPremium 合计。
			// 死亡座位 settlePlayer 跳过(Monthly 保留上月快照),不比对。
			if !p.Alive {
				continue
			}
			var detailSum int64
			for _, it := range p.Monthly.Detail {
				if it.Key == "insurance" {
					if it.AmountCNY >= 0 {
						t.Fatalf("seat %d month %d: insurance detail must be negative expense, got %d", seat, settledMonth, it.AmountCNY)
					}
					detailSum += -it.AmountCNY
				}
			}
			prem := insE2EPremiumOfMonth(w, seat, settledMonth)
			if settledMonth == 1 {
				// 契约 §4.2 防双扣:投保当月首月保费在动作腿即时扣缴,
				// 月结 SettlePremiums 跳过 → detail 无 insurance 行,但 ledger 有。
				if detailSum != 0 {
					t.Errorf("seat %d month 1: purchase-month premium must not enter my.monthly detail, got %d", seat, detailSum)
				}
				if pol := p.Policies[insE2EKindFor(seat)]; pol == nil || prem != monthlyPremium(pol.AnnualPremiumCNY) {
					t.Errorf("seat %d month 1: action-leg premium = %d, want first monthly premium", seat, prem)
				}
			} else if detailSum != prem {
				t.Errorf("seat %d month %d: my.monthly detail insurance %d vs ledger premium %d", seat, settledMonth, detailSum, prem)
			}
		}

		// seat 3 断缴链路:宽限(结算月 8)→ 失效(结算月 9)。
		if pol := w.Players[3].Policies[InsMedicalMillion]; pol != nil && w.Players[3].Alive {
			if settledMonth == 8 && pol.Active {
				if !pol.GraceActive || pol.Status(w.Month) != "grace" {
					t.Errorf("seat 3 month 8: expected grace, got %+v", pol)
				}
				seat3GraceMonth = settledMonth
			}
			if settledMonth == 9 && !pol.Active {
				if pol.Status(w.Month) != "lapsed" {
					t.Errorf("seat 3 month 9: expected lapsed, got %s", pol.Status(w.Month))
				}
				seat3LapseMonth = settledMonth
			}
		}
	}
	if seat3GraceMonth != 8 || seat3LapseMonth != 9 {
		t.Errorf("seat 3 grace/lapse chain: grace@%d lapse@%d, want 8/9", seat3GraceMonth, seat3LapseMonth)
	}
	// 失效后 seat 3 不再有新保费流水(结算月 10..26)。
	for m := 10; m < w.Month; m++ {
		if prem := insE2EPremiumOfMonth(w, 3, m); prem != 0 {
			t.Errorf("seat 3 month %d: lapsed policy must stop deducting, got %d", m, prem)
		}
	}

	// (a) 收口:全程保费流水显著非零(≥24 月 × 多座位)。
	premTotal := insE2ESumCat(w, CatPremium)
	if premTotal <= 0 {
		t.Fatal("(a) zero premium flow across 26 months")
	}

	// (c) I4 公式(理赔注入前):EntityNet(insurer) == Σpremium − Σclaim。
	claimBefore := insE2ESumCat(w, CatClaim)
	if got, want := w.Ledger.EntityNet(EntityInsurer), premTotal-claimBefore; got != want {
		t.Errorf("I4 before injected claim: EntityNet=%d, want %d", got, want)
	}

	// 注入确定性医疗理赔:seat 4(百万医疗,月 27 早已过等待期)报销 90%。
	p4 := w.Players[4]
	if pol := p4.Policies[InsMedicalMillion]; pol == nil || pol.Status(w.Month) != "active" {
		t.Fatalf("seat 4 medical policy not active at month %d: %+v", w.Month, pol)
	}
	cash4, net4 := p4.Cash, w.Ledger.SeatNetFlow(4, w.Month)
	const wantClaim = int64(108000) // round(120000×0.9)
	w.SettleMedicalClaims(4, 120000, false)
	if got := p4.Cash - cash4; got != wantClaim {
		t.Errorf("seat 4 claim cash delta: %d, want %d", got, wantClaim)
	}
	if got := w.Ledger.SeatNetFlow(4, w.Month) - net4; got != wantClaim {
		t.Errorf("seat 4 claim ledger delta: %d, want %d (I1 增量)", got, wantClaim)
	}
	if got := p4.Policies[InsMedicalMillion].ClaimsTotalCNY; got != wantClaim {
		t.Errorf("seat 4 ClaimsTotalCNY: %d, want %d", got, wantClaim)
	}
	// (c) I4 公式(理赔注入后)。
	prem2, claim2 := insE2ESumCat(w, CatPremium), insE2ESumCat(w, CatClaim)
	if got, want := w.Ledger.EntityNet(EntityInsurer), prem2-claim2; got != want {
		t.Errorf("I4 after injected claim: EntityNet=%d, want %d", got, want)
	}
	t.Logf("insurer EntityNet=%d (premium=%d claim=%d) — 保险公司净额已核算", prem2-claim2, prem2, claim2)

	// (d) view 透出:BuildClientState(viewer=4)三段一致性。
	var seats, nicks [MaxSeats]string
	var bots [MaxSeats]bool
	var models [MaxSeats]string
	for i := 0; i < MaxSeats; i++ {
		seats[i] = fmt.Sprintf("ins-e2e-bot-%d", i)
		nicks[i] = seats[i]
		bots[i] = true
		models[i] = "FakeModel"
	}
	cs := BuildClientState("room-ins-e2e", 4, w, seats, nicks, bots, models, [MaxSeats]BotTranscript{}, 0, 0, nil)
	if cs.My == nil || cs.My.Insurance == nil {
		t.Fatalf("my.insurance missing for viewer 4 (insurance_enabled=true)")
	}
	ins := cs.My.Insurance
	if ins.MonthlyPremium != w.MonthlyInsurancePremium(p4) || ins.MonthlyPremium <= 0 {
		t.Errorf("my.insurance.monthly_premium=%d, want %d(>0)", ins.MonthlyPremium, w.MonthlyInsurancePremium(p4))
	}
	if len(ins.Policies) != 1 || ins.Policies[0].Kind != InsMedicalMillion {
		t.Errorf("my.insurance.policies = %+v, want single medical_million", ins.Policies)
	}
	if got := ins.Policies[0].ClaimsTotalCNY; got != wantClaim {
		t.Errorf("policy.claims_total_cny = %d, want %d", got, wantClaim)
	}
	found := false
	for _, it := range cs.My.Monthly.Detail {
		if it.Key == "insurance" {
			found = true
		}
	}
	if !found {
		t.Error("(d) my.monthly.detail missing key=insurance after premium-bearing months")
	}
	// insured_kinds:座位 0 重疾 Active → 公开可见;座位 3 已失效 → 恒空。
	if p0 := w.Players[0]; p0.Alive {
		if got := cs.Players[0].InsuredKinds; len(got) != 1 || got[0] != InsCriticalIllness {
			t.Errorf("players[0].insured_kinds = %v, want [critical_illness]", got)
		}
	}
	if got := cs.Players[3].InsuredKinds; len(got) != 0 {
		t.Errorf("players[3].insured_kinds = %v, want empty (lapsed)", got)
	}
}

// ── (2) Agent 工具路径(AgentRunner,无 LLM)──────────────────────────

// TestInsuranceE2E_AgentToolPath Bot 工具三件套端到端(BeginDecision 槽位 +
// a.apply 持 r.mu + ApplyAction 同一入口;GetInsuranceStatus 锁内只读)。
func TestInsuranceE2E_AgentToolPath(t *testing.T) {
	r := insE2ERoom(t, "room-ins-e2e-tool", 20260919)
	a := NewAgentRunner(r, 0)

	r.mu.Lock()
	month := r.World.Month
	r.mu.Unlock()
	if e := a.BeginDecision(0, month); e != nil {
		t.Fatalf("BeginDecision: %v", e)
	}
	// 投保成功:保单 + 首月保费 + transcript 可观测。
	if err := a.BuyInsurance(0, InsAccident); err != nil {
		t.Fatalf("agent BuyInsurance: %v", err)
	}
	r.mu.Lock()
	pol := r.World.Players[0].Policies[InsAccident]
	n, total := insPremiumCount(r.World, 0)
	summary := r.Transcripts[0].LastDecisionSummary
	r.mu.Unlock()
	if pol == nil || !pol.Active {
		t.Fatal("agent buy did not create active policy")
	}
	if n != 1 || total != monthlyPremium(pol.AnnualPremiumCNY) {
		t.Errorf("agent buy premium entries: n=%d total=%d, want 1/%d", n, total, monthlyPremium(pol.AnnualPremiumCNY))
	}
	if summary == "" {
		t.Error("agent buy did not record transcript LastDecisionSummary (observability)")
	}
	// 重复投保 → 35038(工具路径同校验链)。
	err := a.BuyInsurance(0, InsAccident)
	if err == nil {
		t.Fatal("duplicate agent buy must fail")
	}
	if e, ok := err.(*errcode.Error); !ok || e.Code != errcode.ErrVirtualCityInsuranceExists {
		t.Errorf("duplicate agent buy error = %v, want 35038", err)
	}
	// 查询:不耗预算,人读文本含险种与状态。
	txt, err := a.GetInsuranceStatus(0)
	if err != nil {
		t.Fatalf("GetInsuranceStatus: %v", err)
	}
	if !containsAll(txt, "意外险", "有效") {
		t.Errorf("GetInsuranceStatus text missing kind/status:\n%s", txt)
	}
	// 退保:零现金价值 + 次月停缴。
	if err := a.CancelInsurance(0, InsAccident); err != nil {
		t.Fatalf("agent CancelInsurance: %v", err)
	}
	r.mu.Lock()
	pol = r.World.Players[0].Policies[InsAccident]
	kinds := r.World.insuredKindsOf(r.World.Players[0])
	r.mu.Unlock()
	if pol == nil || pol.Active {
		t.Error("agent cancel must deactivate policy")
	}
	if len(kinds) != 0 {
		t.Errorf("insured_kinds after cancel = %v, want empty", kinds)
	}
	a.EndDecision(0, month)
}

// containsAll 子串全包含。
func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}

// ── (3) §92a 锁纪律:并发动作 / 月结 / 身故 ──────────────────────────

// TestInsuranceE2E_ConcurrentActionSettleDeathRace 并发不变量(-race 下跑)。
//
// §92a 竞态分析结论(2026-09-19 integration-tester):
//   - HandleDeath 唯一生产调用链:rollAccident ← MonthlyEvents ← SettleMonth
//     ← trySettle(r.mu 持有中)。WS 动作路径(applyVirtualCityAction → MuLock →
//     ApplyAction)与 Agent 工具路径(a.apply → r.mu)用的是**同一把 r.mu**
//     —— 不存在「月结一把锁 + WS 动作另一把锁」的双锁窗口;身故与投保在同一
//     互斥量上天然串行,HandleDeath 不会与任何 Action_* 重入。
//   - 保险视图 helper(insuredKindsOf / buildInsuranceJSON)是纯读函数,仅由
//     BuildClientState(调用方持锁或读 Engine() 快照)与 GetInsuranceStatus
//     (r.mu 持有中)调用,无 Action_* 重入锁路径。
//   - 已知存量注意项(非 P1-4 引入):ws 层 broadcastVirtualCityState 经 Engine()
//     取指针后锁外构造快照 —— P0 起全量 view 的既有模式,保险段沿用同模式,
//     未新增违规路径(见验收报告「遗留风险」)。
//
// 本测试用三路 goroutine(月结 / WS 动作 / 身故)压力并发,任何绕过 r.mu 的
// World 访问都会被 race detector 抓获;事后校验 I1 / I4 / 身故理赔入账。
func TestInsuranceE2E_ConcurrentActionSettleDeathRace(t *testing.T) {
	r := insE2ERoom(t, "room-ins-e2e-race", 20260919)

	forceSettle := func() {
		r.mu.Lock()
		r.NextMonthAt = time.Now().Add(-time.Hour)
		r.mu.Unlock()
		r.trySettle(nil)
	}
	// 抽卡随机(curated 池储蓄 5k–300k)→ 取现金最足的 3 个座位各投定期寿险,
	// 推进 4 月过等待期后从中选取「存活且生效」的座位做身故断言,消除
	// 低储蓄座位偶发断缴 / 等待期内意外身故对断言的干扰。
	r.mu.Lock()
	type seatCash struct {
		seat int
		cash int64
	}
	rich := make([]seatCash, 0, MaxSeats)
	for seat := 0; seat < MaxSeats; seat++ {
		if p := r.World.Players[seat]; p != nil {
			rich = append(rich, seatCash{seat, p.Cash})
		}
	}
	r.mu.Unlock()
	for i := 0; i < len(rich); i++ { // 按 cash 降序(12 元素,冒泡足够)
		for j := i + 1; j < len(rich); j++ {
			if rich[j].cash > rich[i].cash {
				rich[i], rich[j] = rich[j], rich[i]
			}
		}
	}
	if len(rich) < 3 {
		t.Fatalf("expected ≥3 seated players, got %d", len(rich))
	}
	insured := rich[:3]
	for _, sc := range insured {
		r.MuLock()
		_, e := r.EngineLocked().ApplyAction(sc.seat, Action{Type: ActBuyInsurance, Kind: InsTermLife})
		r.MuUnlock()
		if e != nil {
			t.Fatalf("buy term_life on seat %d: %v", sc.seat, e)
		}
	}
	for i := 0; i < 4; i++ {
		forceSettle()
	}
	// 选首个「存活且保单 active」的已投保座位。
	var deathSeat = -1
	r.mu.Lock()
	monthNow := r.World.Month
	for _, sc := range insured {
		p := r.World.Players[sc.seat]
		if p != nil && p.Alive {
			if pol := p.Policies[InsTermLife]; pol != nil && pol.Status(monthNow) == "active" {
				deathSeat = sc.seat
				break
			}
		}
	}
	r.mu.Unlock()
	if deathSeat < 0 {
		t.Fatal("no alive seat with active term_life after waiting period")
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() { // 月结路径(trySettle 持 r.mu;意外身故只可能经此发生)
		defer wg.Done()
		for i := 0; i < 40; i++ {
			forceSettle()
		}
	}()
	wg.Add(1)
	go func() { // WS 动作路径同款(MuLock + ApplyAction,仿 applyVirtualCityAction)
		defer wg.Done()
		for i := 0; i < 200; i++ {
			seat := 1 + i%6
			act := Action{Type: ActBuyInsurance, Kind: InsAccident}
			if i%2 == 1 {
				act = Action{Type: ActCancelInsurance, Kind: InsAccident}
			}
			r.MuLock()
			if r.Status == StatusPlaying && r.Phase == PhaseActing && r.World != nil {
				_, _ = r.EngineLocked().ApplyAction(seat, act)
			}
			r.MuUnlock()
		}
	}()
	wg.Add(1)
	go func() { // 身故路径:与动作路径同锁串行(HandleDeath 幂等:已死则 no-op)
		defer wg.Done()
		r.MuLock()
		if w := r.World; w != nil && w.Players[deathSeat] != nil && w.Players[deathSeat].Alive {
			w.HandleDeath(deathSeat, "意外身故")
		}
		r.MuUnlock()
	}()
	wg.Wait()

	// 事后不变量(锁内读)。
	r.mu.Lock()
	defer r.mu.Unlock()
	w := r.World
	p0 := w.Players[deathSeat]
	if p0.Alive {
		t.Fatalf("seat %d should be dead after HandleDeath path", deathSeat)
	}
	if p0.Ending != EndingAccidentDeath {
		t.Errorf("seat %d ending = %s, want %s", deathSeat, p0.Ending, EndingAccidentDeath)
	}
	var deathClaim int64
	for _, e := range w.Ledger.Entries {
		if e.From == EntityInsurer && e.To == SeatEntity(deathSeat) && e.Category == CatClaim && e.Note == "身故理赔-定期寿险" {
			deathClaim += e.AmountCNY
		}
	}
	if deathClaim != 1000000 {
		t.Errorf("term_life death claim = %d, want 1000000", deathClaim)
	}
	// I1:每座位现金 == 全量流水净额(初始注入 month 0 亦在账)。
	for seat := 0; seat < MaxSeats; seat++ {
		p := w.Players[seat]
		if p == nil {
			continue
		}
		if net := w.Ledger.SeatNetFlow(seat, 0); p.Cash != net {
			t.Errorf("I1 broken seat %d: cash %d vs ledger net %d", seat, p.Cash, net)
		}
	}
	// I4:EntityNet(insurer) == Σpremium − Σclaim(公式与账本自洽)。
	var prem, claim int64
	for _, e := range w.Ledger.Entries {
		if e.Category == CatPremium {
			prem += e.AmountCNY
		}
		if e.Category == CatClaim {
			claim += e.AmountCNY
		}
	}
	if got, want := w.Ledger.EntityNet(EntityInsurer), prem-claim; got != want {
		t.Errorf("I4 broken after concurrency: EntityNet=%d, want %d", got, want)
	}
}
