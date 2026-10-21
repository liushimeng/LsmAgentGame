// Package virtual_city — montecarlo.go: 蒙特卡洛验收 harness + 5 策略代理
// (批次72《蒙特卡洛验收与运行采集写盘》§3/§4)。
//
// 硬纪律(方案 §3.1,与批次71 B10 同源):
//   - **策略与 harness 绝不消费 w.Rand** —— 策略是 (w, seat, month) → []Action
//     的纯状态函数;stress 剧本为固定月/固定金额/固定字段的确定性写回。
//     本文件零 rand.New、零 Rand.Intn/Float64 调用(卡源 rng 独立于世界 rng)。
//   - 动作面仅走既有 w.ApplyAction(与人类/Agent 同一入口、同一预算、同一校验),
//     不新建动作类型;每月动作 ≤ 2(预算 3 留 1 冗余)。
//   - 动作失败(预算尽/现金不足/信用不足)→ 静默跳过,不重试不报错。
package virtual_city

import (
	"math/rand"

	"LsmAgentGame/game/virtual_city/profession"
)

// ── 策略 id 常量(archetype id 即 API 字符串,恒定)────────────────────

const (
	MCStrategySafetyNetOnly      = "safety_net_only"     // 零动作基线锚
	MCStrategyMinimumEffort      = "minimum_effort"      // 闲钱买国债
	MCStrategyBalanced           = "balanced"            // 盈余 40/30/30
	MCStrategyAggressiveLeverage = "aggressive_leverage" // 繁荣期杠杆满上
	MCStrategyStress             = "stress"              // balanced + 剧本覆盖
)

// mcStrategyOrder 策略固定序(种子分带 = 100_000 × 下标;顺序不可调)。
var mcStrategyOrder = []string{
	MCStrategySafetyNetOnly,
	MCStrategyMinimumEffort,
	MCStrategyBalanced,
	MCStrategyAggressiveLeverage,
	MCStrategyStress,
}

// MCStrategies 返回全部策略 id(副本,调用方可安全改)。
func MCStrategies() []string {
	out := make([]string, len(mcStrategyOrder))
	copy(out, mcStrategyOrder)
	return out
}

// mcStrategyIndex 返回策略序号(-1 = 未知策略)。
func mcStrategyIndex(id string) int {
	for i, s := range mcStrategyOrder {
		if s == id {
			return i
		}
	}
	return -1
}

// ── 种子派生(方案 §4.1)──────────────────────────────────────────────

const (
	// mcBaseSeed 第 0 局基底(保留验收方案原基底 20260919_0000)。
	mcBaseSeed int64 = 202609190000
	// mcStrategyBand 逐策略种子分带宽度(防跨策略种子交叠)。
	mcStrategyBand int64 = 100_000
	// mcMaxMonths 单局最大推进月数(M420 = 60 岁终局)。
	mcMaxMonths = 420
	// mcMonthlyActionCap 策略每月动作上限(预算 3 留 1 冗余)。
	mcMonthlyActionCap = 2
)

// MCSeedFor 第 i 局(strategy 单策略全桌)的世界种子:
// base + 100_000 × 策略序号 + i。未知策略分带 -1(仍确定性)。
func MCSeedFor(strategy string, i int) int64 {
	return mcBaseSeed + mcStrategyBand*int64(mcStrategyIndex(strategy)) + int64(i)
}

// ── 策略决策(纯状态函数,零 rand)────────────────────────────────────

// MCStrategyActions 返回座位 seat 在 month 月的策略动作(≤2 条)。
//
// 未知策略 → nil(safety_net_only 语义);空座/出局座 → nil。
// 返回的动作**不保证可执行**(现金/信用/预算不足时 ApplyAction 拒绝,
// 调用方静默跳过 —— 方案 §3.1-4)。
func MCStrategyActions(w *World, strategy string, seat, month int) []Action {
	if w == nil || seat < 0 || seat >= MaxSeats {
		return nil
	}
	p := w.Players[seat]
	if p == nil || !p.Alive || p.StoppedMonths > 0 {
		return nil
	}
	switch strategy {
	case MCStrategySafetyNetOnly:
		return nil // 零动作:仅靠自动月结(节俭档/失业保险/社保)生存
	case MCStrategyMinimumEffort:
		return mcMinimumEffortActions(p)
	case MCStrategyBalanced, MCStrategyStress:
		return mcBalancedActions(p)
	case MCStrategyAggressiveLeverage:
		return mcAggressiveActions(w, p)
	default:
		return nil
	}
}

// mcMinimumEffortActions:cash > 6×月支出 时买国债 0.2×(cash − 6×月支出)。
//
// 「月支出」取最近一次月结的 p.Monthly.Expense(首月无月结 = 0)。
func mcMinimumEffortActions(p *Player) []Action {
	threshold := 6 * p.Monthly.Expense
	if p.Cash <= threshold {
		return nil
	}
	amount := int64(0.2 * float64(p.Cash-threshold))
	if amount < 1000 { // 引擎下限:buy_asset ≥ ¥1000
		return nil
	}
	return []Action{{Type: ActBuyAsset, Asset: AssetBond, AmountCNY: amount}}
}

// mcBalancedActions:新钱(上月净结余 > 0)30% bond + 30% stock,40% 留现金;
// 不做存量再平衡(减少交易噪音)。至多 2 条动作。
func mcBalancedActions(p *Player) []Action {
	net := p.Monthly.Net
	if net <= 0 {
		return nil
	}
	var out []Action
	bond := int64(0.3 * float64(net))
	stock := int64(0.3 * float64(net))
	if bond >= 1000 {
		out = append(out, Action{Type: ActBuyAsset, Asset: AssetBond, AmountCNY: bond})
	}
	if stock >= 1000 {
		out = append(out, Action{Type: ActBuyAsset, Asset: AssetStockIndex, AmountCNY: stock})
	}
	return out
}

// mcAggressiveActions:仅繁荣期行动 —— ① 房产 <4 且信用 ≥600 ⇒ 首付 0.3 购房
// (贷款自动生成,即「贷款上限用满」口径);② 余钱 ≥ ¥10,000 ⇒ 买股票。
//
// ⚠ ② 的「全部买 stock」按 0.98×cash 执行:引擎买入价含流动性价差 + 佣金 +
// 偏差滑点(ex.Total > p.Cash 即拒),全额下单恒被拒,0.98 留出费用余量
// (方案 §3.2 的可执行化裁决,见批次72 实施记录)。
func mcAggressiveActions(w *World, p *Player) []Action {
	if w == nil || w.Market == nil || w.Market.CyclePhase != PhaseBoom {
		return nil
	}
	var out []Action
	if p.houseCount() < 4 && p.CreditScore >= 600 {
		out = append(out, Action{
			Type: ActBuyHouse, Asset: "house",
			District: mcHomeDistrict(p), DownpayRatio: 0.3,
		})
	}
	if p.Cash >= 10000 {
		out = append(out, Action{
			Type: ActBuyAsset, Asset: AssetStockIndex,
			AmountCNY: int64(0.98 * float64(p.Cash)),
		})
	}
	return out
}

// mcHomeDistrict 购房城区:HomeDistrict → District → 首区兜底(恒合法)。
func mcHomeDistrict(p *Player) string {
	if ValidDistrict(p.HomeDistrict) {
		return p.HomeDistrict
	}
	if ValidDistrict(p.District) {
		return p.District
	}
	return DistrictDefs[0].ID
}

// ── stress 剧本覆盖(方案 §3.3;固定月/固定金额/固定字段,零 rand)──

const (
	// stressRecessionFrom/To:M25–M37 强制衰退窗口(每月 SettleMonth 后写)。
	stressRecessionFrom = 25
	stressRecessionTo   = 37
	// stressDepressionFrom/To:M61–M73 强制萧条窗口。
	stressDepressionFrom = 61
	stressDepressionTo   = 73
	// stressUnemploymentMonth:M26 全员失业冲击月。
	stressUnemploymentMonth = 26
	// stressMedicalMonth/stressMedicalCNY:M62 全员固定医疗支出。
	stressMedicalMonth = 62
	stressMedicalCNY   = 50000
	// stressMedicalNote 账本备注(方案 §3.3 逐字)。
	stressMedicalNote = "压力测试医疗冲击"
)

// MCStressPreSettle stress 剧本的月内注入(策略动作之后、SettleMonth 之前):
//
//   - M26:每个存活座位 UnemployedMonths = 2(与 MonthlyEvents hazard 命中
//     同字段同语义;M26/M27 两月无工资);
//   - M62:每个存活座位 ¥50,000 医疗支出(经 w.Pay 走 Ledger,不变量不受染)
//   - Energy −5(clamp −3)+ HadIllnessYear = true。
//
// 零 rand;非 stress 月为 no-op。批次73 O2:两处注入各记一条 scenario
// 事件(step=inject,scenario_id=stress_unemployment / stress_medical)。
func MCStressPreSettle(w *World, month int) {
	if w == nil {
		return
	}
	switch month {
	case stressUnemploymentMonth:
		actors := make([]string, 0, MaxSeats)
		for _, p := range w.Players {
			if p != nil && p.Alive {
				p.UnemployedMonths = 2
				actors = append(actors, SeatEntity(p.Seat))
			}
		}
		w.recordEvent(MCStepInject, MCEventScenario, actors, 0, 0, false, "stress_unemployment")
	case stressMedicalMonth:
		actors := make([]string, 0, MaxSeats)
		for seat, p := range w.Players {
			if p == nil || !p.Alive {
				continue
			}
			actors = append(actors, SeatEntity(seat))
			w.Pay(seat, SeatEntity(seat), w.consumerPayTo(), stressMedicalCNY, CatMedical, stressMedicalNote)
			p.Energy -= 5
			if p.Energy < -3 {
				p.Energy = -3
			}
			p.HadIllnessYear = true
		}
		w.recordEvent(MCStepInject, MCEventScenario, actors, stressMedicalCNY, int64(len(actors)), false, "stress_medical")
	}
}

// MCStressPostSettle stress 剧本的月末相位覆盖(SettleMonth 返回后调用):
// 若**下月**(= SettleMonth 内已 ++ 的 w.Month)落入 M25–M37 ⇒ 写
// PhaseRecession / CycleMonthsLeft=1;M61–M73 ⇒ PhaseDepression。
//
// 引擎自身的相位转移 rand 照常消耗,本函数只做写回覆盖 ⇒ 同种子确定性保持。
// 批次73 O2:窗口首月各记一条 scenario 事件(stress_recession /
// stress_depression;每窗口 1 条,防 13 个月重复计数)。
func MCStressPostSettle(w *World) {
	if w == nil || w.Market == nil {
		return
	}
	m := w.Month
	switch {
	case m == stressRecessionFrom:
		w.recordEvent(MCStepInject, MCEventScenario, nil, 0, 0, false, "stress_recession")
		fallthrough
	case m > stressRecessionFrom && m <= stressRecessionTo:
		w.Market.CyclePhase = PhaseRecession
		w.Market.CycleMonthsLeft = 1
	case m == stressDepressionFrom:
		w.recordEvent(MCStepInject, MCEventScenario, nil, 0, 0, false, "stress_depression")
		fallthrough
	case m > stressDepressionFrom && m <= stressDepressionTo:
		w.Market.CyclePhase = PhaseDepression
		w.Market.CycleMonthsLeft = 1
	}
}

// ── 单局采集结构(方案 §4.2)──────────────────────────────────────────

// MCSeatSnapshot 锚点月整桌快照(M1/M97/M120/M360/终局)。
// 出局座位 NetWorth 记 0(G9 口径);FI/MonthlyNet 保留最后月结值。
type MCSeatSnapshot struct {
	Month      int
	NetWorth   [MaxSeats]float64
	FI         [MaxSeats]float64
	MonthlyNet [MaxSeats]int64
	Alive      [MaxSeats]bool
	Volatile   [MaxSeats]bool // P16 波动卡(G1 豁免判据)
}

// MCFailureSample 失败样本五元组(方案 §4.1:seed/month/seat/cash/net_worth)。
type MCFailureSample struct {
	Seed     int64
	Month    int
	Seat     int
	Cash     int64
	NetWorth int64
}

// MCRunStats 单局采集(内存,零落盘)。
type MCRunStats struct {
	Seed     int64
	Strategy string
	Months   int  // 实际推进月数
	Finished bool // 是否走到终局(StatusOver)

	// 逐月序列(下标 0 = M1)。
	AliveByMonth      []int     // 存活座位数
	EliminatedByMonth []int     // 累计正式出局(Alive=false)座位数
	LiquidatedByMonth []int     // 累计触发过破产清算(BankruptCount>0)座位数
	D2ByMonth         []float64 // 审计尾快照 D2(货币扩张)
	D5ByMonth         []float64 // 审计尾快照 D5(破产率)
	AuditGiniByMonth  []float64 // 审计尾快照 Gini(存活口径,Society)

	// 单月新增出局峰值(G11 连锁破产判据)。
	ChainBankruptcyPeak int

	// 锚点月快照。
	AnchorM1    MCSeatSnapshot
	AnchorM97   MCSeatSnapshot
	AnchorM120  MCSeatSnapshot
	AnchorM360  MCSeatSnapshot
	AnchorFinal MCSeatSnapshot

	// 全 12 座口径(出局记 0)的分布指标。
	GiniM60, GiniM120, GiniFinal    float64
	Top20M60, Top20M120, Top20Final float64

	// 排名对(G10 Spearman 输入;按 seat 序,长度 MaxSeats,未到月为 nil)。
	NetWorthM1   []float64
	NetWorthM120 []float64

	// 品类年化(§6.2 口径;月数 <12 的局为 nil)。
	CategoryAnnualized map[string]float64

	// 不变量失败计数(G12 输入;含 I1–I8 全部 id)。
	InvariantFailsByID map[string]int

	// 失败样本:每次正式出局记一条五元组。
	EliminationSamples []MCFailureSample

	// 收口信息(来自 CalibrationState 收口记录)。
	RunID     string
	StateHash string
}

// ── 单局驱动(方案 §4.2)──────────────────────────────────────────────

// MCRunOne 驱动单局:12 座合成卡 → StartGame → M1–M420
// 「策略动作 → (stress 注入) → SettleMonth → (stress 相位覆盖) → 采样」,
// 至 M420 或终局。卡源 rng 与世界 rng 分开(世界由 NewWorld 自建)。
//
// 同 seed + 同 strategy ⇒ 返回值逐字段相等(T-MC1)。
// 未知策略 ⇒ 返回零值统计(Months=0),不 panic。
//
// ⚠ 批次73 O5:本体 = MCRunOneWith(seed, strategy, nil) 的薄封装
// (nil 覆盖 ⇒ 与批次72 行为逐字段一致,T-AB1 钉死)。
func MCRunOne(seed int64, strategy string) MCRunStats {
	return MCRunOneWith(seed, strategy, nil)
}

// MCRunOneWith MCRunOne + 参数覆盖(批次73 O5)。ov 只允许常量类参数
// (见 MCOverride 注释);nil ⇒ 与 MCRunOne 完全等价。
//
// 出口旁路(零 rand、失败静默/zap warn,绝不阻断收口):
//   - O1:residents.jsonl 落盘(mcCollectResidents 12 记录);
//   - O2:events.jsonl 明细仅 balance_gate 构建的 gate 运行落盘
//     (mcGateDetail 常量判定;默认构建零写盘开销)。
func MCRunOneWith(seed int64, strategy string, ov *MCOverride) MCRunStats {
	stats := MCRunStats{Seed: seed, Strategy: strategy, InvariantFailsByID: map[string]int{}}
	if mcStrategyIndex(strategy) < 0 {
		return stats
	}

	// 卡源 rng:每局独立(方案 §4.1);与 NewWorld 内部世界 rng 互不影响。
	cardRng := rand.New(rand.NewSource(seed))
	cards := [MaxSeats]profession.Card{}
	for i, c := range profession.SyntheticCards(MaxSeats, cardRng) {
		if i >= MaxSeats {
			break
		}
		cards[i] = c
	}
	w := NewWorld(seed, cards)
	// 批次73 O5:覆盖生效点①(初始现金缩放须早于 StartGame 的注入记账)。
	if ov != nil {
		ov.apply(w)
	}
	w.StartGame()
	if cs := w.calibrationEnsure(); cs != nil {
		// MC 局策略标注(RunRecord.Strategy;真实对局恒空串)。
		cs.Strategy = strategy
	}

	var recorded [MaxSeats]bool // 已记失败样本的座位
	for m := 1; m <= mcMaxMonths; m++ {
		mcResetMonthBudgets(w)
		for seat := range w.Players {
			acts := MCStrategyActions(w, strategy, seat, m)
			for i := 0; i < len(acts) && i < mcMonthlyActionCap; i++ {
				_, _ = w.ApplyAction(seat, acts[i]) // 失败静默跳过(§3.1-4)
			}
		}
		if strategy == MCStrategyStress {
			MCStressPreSettle(w, m)
		}
		finished, _ := w.SettleMonth()
		if strategy == MCStrategyStress {
			MCStressPostSettle(w)
		}
		mcSampleMonth(&stats, w, m, &recorded)
		if finished || m == mcMaxMonths {
			stats.Finished = finished
			stats.Months = m
			stats.AnchorFinal = mcSeatSnapshot(w, m)
			nw := mcNetWorth12(stats.AnchorFinal)
			stats.GiniFinal = mcGini(nw)
			stats.Top20Final = mcTop20Share(nw)
			break
		}
	}

	// 收口信息:优先进行中记录(未终局),否则已收口 Runs 尾条。
	if cs := w.Calibration; cs != nil {
		rec := cs.Current
		if rec == nil && len(cs.Runs) > 0 {
			rec = &cs.Runs[len(cs.Runs)-1]
		}
		if rec != nil {
			stats.CategoryAnnualized = rec.CategoryAnnualized
			stats.RunID = rec.RunID
			stats.StateHash = rec.StateHash
		}
	}
	if stats.Months == 0 { // 防御:首月即终局的极端情形
		stats.Months = minInt(w.Month, mcMaxMonths)
	}

	// 批次73 O1/O2 出口旁路:居民级采集恒落盘(12 记录/局);事件明细
	// 仅 gate 构建落盘。失败 zap warn / 静默降级,绝不阻断收口。
	if stats.Months > 0 {
		residents := mcCollectResidents(w, stats.RunID, w.MCAuctionCount)
		appendResidentsLogging(residents)
		if mcGateDetail && len(w.MCEvents) > 0 {
			appendEventsLogging(w.MCEvents)
		}
	}
	return stats
}

// mcResetMonthBudgets 新月动作预算重置(对齐 room.resetMonthFlagsLocked;
// harness 侧无房间层,须自置)。
func mcResetMonthBudgets(w *World) {
	for _, p := range w.Players {
		if p == nil {
			continue
		}
		p.ActionBudget = monthlyActionBudget
	}
}

// mcSampleMonth 采单月序列 + 锚点快照(零 rand)。
func mcSampleMonth(stats *MCRunStats, w *World, month int, recorded *[MaxSeats]bool) {
	alive, elim, liq := 0, 0, 0
	for _, p := range w.Players {
		if p == nil {
			continue
		}
		if p.Alive {
			alive++
		} else {
			elim++
		}
		if p.BankruptCount > 0 {
			liq++
		}
	}
	prevElim := 0
	if n := len(stats.EliminatedByMonth); n > 0 {
		prevElim = stats.EliminatedByMonth[n-1]
	}
	stats.AliveByMonth = append(stats.AliveByMonth, alive)
	stats.EliminatedByMonth = append(stats.EliminatedByMonth, elim)
	stats.LiquidatedByMonth = append(stats.LiquidatedByMonth, liq)
	if elim-prevElim > stats.ChainBankruptcyPeak {
		stats.ChainBankruptcyPeak = elim - prevElim
	}

	// 失败样本:本月新出局的座位各记一条五元组。
	for seat, p := range w.Players {
		if p == nil || p.Alive || recorded[seat] {
			continue
		}
		recorded[seat] = true
		stats.EliminationSamples = append(stats.EliminationSamples, MCFailureSample{
			Seed: stats.Seed, Month: month, Seat: seat,
			Cash: p.Cash, NetWorth: p.NetWorth(w.Market),
		})
	}

	// 审计尾快照指标(D2/D5/Gini)+ 不变量失败计数。
	if n := len(w.Audits); n > 0 {
		a := w.Audits[n-1]
		d2, d5, gini := 0.0, 0.0, 0.0
		if a.Indicators.MoneyExpansion != nil {
			d2 = *a.Indicators.MoneyExpansion
		}
		if a.Indicators.BankruptcyRate != nil {
			d5 = *a.Indicators.BankruptcyRate
		}
		if a.Indicators.Gini != nil {
			gini = *a.Indicators.Gini
		}
		stats.D2ByMonth = append(stats.D2ByMonth, d2)
		stats.D5ByMonth = append(stats.D5ByMonth, d5)
		stats.AuditGiniByMonth = append(stats.AuditGiniByMonth, gini)
		for _, inv := range a.Invariants {
			if !inv.Passed {
				stats.InvariantFailsByID[inv.ID]++
			}
		}
	}

	// 锚点月快照 + 分布指标(M60/M120 不存整表,只存 Gini/top20)。
	switch month {
	case 1:
		stats.AnchorM1 = mcSeatSnapshot(w, month)
		stats.NetWorthM1 = mcNetWorth12(stats.AnchorM1)
	case 60:
		nw := mcNetWorth12(mcSeatSnapshot(w, month))
		stats.GiniM60 = mcGini(nw)
		stats.Top20M60 = mcTop20Share(nw)
	case 97:
		stats.AnchorM97 = mcSeatSnapshot(w, month)
	case 120:
		stats.AnchorM120 = mcSeatSnapshot(w, month)
		stats.NetWorthM120 = mcNetWorth12(stats.AnchorM120)
		nw := stats.NetWorthM120
		stats.GiniM120 = mcGini(nw)
		stats.Top20M120 = mcTop20Share(nw)
	case 360:
		stats.AnchorM360 = mcSeatSnapshot(w, month)
	}
}

// mcSeatSnapshot 整桌锚点快照(零 rand)。
func mcSeatSnapshot(w *World, month int) MCSeatSnapshot {
	s := MCSeatSnapshot{Month: month}
	if w == nil {
		return s
	}
	for seat, p := range w.Players {
		if p == nil {
			continue
		}
		s.NetWorth[seat] = float64(p.NetWorth(w.Market))
		s.FI[seat] = p.FIIndex(w.Market, w.Age())
		s.MonthlyNet[seat] = p.Monthly.Net
		s.Alive[seat] = p.Alive
		s.Volatile[seat] = p.SalaryVolatile
	}
	return s
}

// mcNetWorth12 快照转 12 元切片(出局座位已为 0)。
func mcNetWorth12(s MCSeatSnapshot) []float64 {
	out := make([]float64, MaxSeats)
	for i := 0; i < MaxSeats; i++ {
		if !s.Alive[i] {
			continue // 出局按 0 计(G9 口径;快照本身也记 0)
		}
		out[i] = s.NetWorth[i]
	}
	return out
}

// minInt 小工具。
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
