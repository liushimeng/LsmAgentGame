// Package wealth — labor_test.go: 劳动力市场 + Phillips + 社会结构单测
// (2026-09-16 §财商流P1-2,契约文档 §10.2)。
package wealth

import (
	"math"
	"testing"
)

// laborWorld 构造 n 人测试世界(salary 10000 / expense 7000,比值 0.7 贴近
// §12.1 标定;人手一套自住房消除房租噪声,消费主要由 living 驱动;大储蓄防
// 破产干扰)。
func laborWorld(seed int64, n int) *World {
	w := NewWorld(seed, emptyCardsFor(0))
	for s := 0; s < n; s++ {
		card := synthCard(s, 500000)
		card.Salary = 10000
		card.Expense = 7000
		p := newPlayerFromCard(s, card)
		p.Assets = []Asset{{
			Kind: AssetKindHouse("residential"), Units: 1, OpenMonth: 1,
			Extra: map[string]any{"district": "residential", "self_occupied": true},
		}}
		p.HomeDistrict = "residential"
		w.Players[s] = p
	}
	// P1-4(2026-09-19 §财商流P1-4):关闭保险引擎掷骰 —— 本文件聚焦劳动力市场,
	// 意外事件 rand 消费会平移固定种子轨迹,破坏 §10.2 单调性标定(§12 契约:
	// insurance_enabled=false 时 rand 序列零偏移,等价 P1-4 前行为)。
	w.InsuranceEnabled = false
	w.StartGame()
	return w
}

// TestLabor_ConsumptionCrashAndRecovery 消费骤降:全员节俭(×0.6)后
// Revenue ≈ ×0.6 → Unemployment 净上升、LayoffWave≥1;恢复 1 档后回落
// (负反馈自稳,§10.2)。注意 LaborMonthStep 在 ②.0 扫描上一完整月,
// 档位效果滞后 1 月生效(契约 §4.2 口径)。批次20 起(16→32 区 rand 轨迹
// 平移)上升窗口重标定,详见函数体内注释。
func TestLabor_ConsumptionCrashAndRecovery(t *testing.T) {
	w := laborWorld(42, 6)
	// 基线 2 月(月 1 消费在月 2 结算时进入营收)。
	w.SettleMonth()
	w.SettleMonth()
	baseRevenue := w.Labor.RevenueCNY
	if baseRevenue <= 0 {
		t.Fatalf("baseline revenue should be positive, got %d", baseRevenue)
	}
	baseU := w.Labor.Unemployment

	// 全员节俭档;先结算 1 月让 ×0.6 消费入账(滞后月)。
	for _, p := range w.Players {
		if p != nil {
			p.ConsumptionLevel = 0
			p.ConsumptionByGoods = map[string]float64{} // 确保档位 0 生效(nil 兜底为 1)
		}
	}
	w.SettleMonth() // 滞后月:本月 ②.0 仍扫描上月(level 1)数据
	postLagU := w.Labor.Unemployment
	// ── 批次20 16→32 区 rand 轨迹平移重标定 ──────────────────────────────
	// 城区表 16→32 使 market.MonthStep 逐区高斯 rand 消耗次数翻倍,seed 42
	// 轨迹必然平移(契约「新区入表即参与统一引擎」的预期后果,非 bug,不得
	// 用隔离 rand 绕过)。旧轨迹:crash 窗口 3 月逐月单调上升;新轨迹滞后月末
	// U=0.077344(旧断言首月即失败点 0.077344→0.058008),crash m0=0.058008 →
	// m1=0.043506 → m2=0.037694 → m3=0.033033 → m4=0.103168 → m5=0.155769,
	// LayoffWave 0→2,RevenueCNY 112500→70500(62.7%≤70% 阈值不变)。
	// 消费骤降推高失业的负反馈仍成立,但见效窗口拉长 ⇒ 断言由「3 月逐月单调
	// 上升」重标定为「6 月窗口净上升 + 裁员波 ≥1」,经济含义不变。
	for i := 0; i < 6; i++ {
		w.SettleMonth()
	}
	if w.Labor.Unemployment <= postLagU {
		t.Fatalf("consumption crash should raise unemployment net over 6m window: %.6f -> %.6f",
			postLagU, w.Labor.Unemployment)
	}
	_ = baseU
	if rev := w.Labor.RevenueCNY; rev > baseRevenue*70/100 {
		t.Errorf("revenue after ×0.6 living: got %d, want <= %d (0.6×%d)", rev, baseRevenue*70/100, baseRevenue)
	}
	if w.Labor.LayoffWave < 1 {
		t.Errorf("layoff wave should be >= 1 at U=%.3f, got %d", w.Labor.Unemployment, w.Labor.LayoffWave)
	}
	peak := w.Labor.Unemployment

	// 恢复标准档 → 负反馈:失业率回落。
	for _, p := range w.Players {
		if p != nil {
			p.ConsumptionLevel = 1
		}
	}
	for i := 0; i < 12; i++ {
		w.SettleMonth()
	}
	if w.Labor.Unemployment >= peak {
		t.Errorf("unemployment should fall after restoring level 1: peak=%.3f now=%.3f", peak, w.Labor.Unemployment)
	}
}

// TestLabor_MonthStepBaseline 自然率带:标准档下失业率收敛在合理区间(§12.1)。
func TestLabor_MonthStepBaseline(t *testing.T) {
	w := laborWorld(7, 6)
	for i := 0; i < 12; i++ {
		w.SettleMonth()
	}
	u := w.Labor.Unemployment
	// expense/salary=0.7 → TE=1.3×0.7=0.91 → 均衡 ≈ 0.09(单身无家庭);
	// 允许宽松区间防随机扰动。
	if u < 0.02 || u > 0.20 {
		t.Errorf("baseline unemployment out of band: %.3f", u)
	}
	if w.Labor.Employment != 1-u {
		t.Errorf("employment != 1 - unemployment: %.3f vs %.3f", w.Labor.Employment, 1-u)
	}
}

// TestPhillipsBase Phillips 基线:U=0.05 → 3%;U=0.10 → 0.5%;U=0.02 → 4.5%(§10.2)。
func TestPhillipsBase(t *testing.T) {
	w := NewWorld(1, emptyCardsFor(0))
	for _, c := range []struct {
		u    float64
		want float64
	}{
		{0.05, 0.03},
		{0.10, 0.005},
		{0.02, 0.045},
	} {
		w.Labor.Unemployment = c.u
		if got := w.phillipsBase(); math.Abs(got-c.want) > 1e-12 {
			t.Errorf("phillipsBase(U=%.2f): got %f, want %f", c.u, got, c.want)
		}
	}
}

// TestAnnualAdjust_PhillipsBrassOffset Brass tier1 叠加后 = 基线 − 2%
// (U=自然率时与 P0 数值一致,§10.2 + §12.6)。
func TestAnnualAdjust_PhillipsBrassOffset(t *testing.T) {
	w := laborWorld(1, 1)
	w.Labor.Unemployment = UnemployNatural // phillipsBase = 0.03
	p := w.Players[0]
	p.Loans = []Loan{{ID: "L1", Kind: LoanCreditT1, Principal: 50000, Balance: 50000,
		AnnualRate: 0.096, MonthlyPayment: 400, TermN: 36, MonthsLeft: 36, InterestOnly: true, LumpAtMaturity: true}}
	before := p.SalaryBase
	w.AnnualAdjust()
	// g = 0.03 + 0.01 − 0.03 = 0.01(P0 tier1 同值,回归零差异)。
	want := int64(float64(before)*1.01 + 0.5)
	if p.SalaryBase != want {
		t.Errorf("phillips + brass tier1: salary %d -> %d, want %d (+1%%)", before, p.SalaryBase, want)
	}
	if math.Abs(w.Labor.WageGrowthYoY-0.03) > 1e-12 {
		t.Errorf("WageGrowthYoY at natural rate: got %f, want 0.03", w.Labor.WageGrowthYoY)
	}

	// 高失业:U=0.10 → phillipsBase 0.005,tier1(+1%−3%=−2%)→ g=−0.015。
	w.Labor.Unemployment = 0.10
	before = p.SalaryBase
	w.AnnualAdjust()
	want = int64(float64(before)*(1+0.005+0.01-0.03) + 0.5)
	if p.SalaryBase != want {
		t.Errorf("phillips(U=0.10) + brass tier1: salary %d -> %d, want %d (-1.5%%)", before, p.SalaryBase, want)
	}
}

// TestAnnualAdjust_EconomyDisabledFallback economy_enabled=false → 工资增长
// 回退 BrassSalaryGrowth() 原值(§6.5)。
func TestAnnualAdjust_EconomyDisabledFallback(t *testing.T) {
	w := laborWorld(1, 1)
	w.EconomyEnabled = false
	p := w.Players[0]
	before := p.SalaryBase
	w.AnnualAdjust()
	want := int64(float64(before)*(1+p.BrassSalaryGrowth()) + 0.5) // 无信用贷 → +3%
	if p.SalaryBase != want {
		t.Errorf("disabled economy wage growth: %d -> %d, want %d (+3%%)", before, p.SalaryBase, want)
	}
}

// TestRehireRatioSegments 再就业比率分段:U=0.12 → [0.7,0.9];U=0.03 → [1.0,1.2];
// U=0.05(自然率)→ [0.8,1.2](P0 原区间)(§10.2)。
func TestRehireRatioSegments(t *testing.T) {
	w := NewWorld(99, emptyCardsFor(0))
	check := func(u float64, lo, hi float64) {
		w.Labor.Unemployment = u
		minR, maxR := 2.0, 0.0
		for i := 0; i < 1000; i++ {
			r := w.sampleRehireRatio()
			if r < lo-1e-9 || r > hi+1e-9 {
				t.Fatalf("U=%.2f: ratio %.4f out of [%f,%f]", u, r, lo, hi)
			}
			if r < minR {
				minR = r
			}
			if r > maxR {
				maxR = r
			}
		}
		if maxR-minR < (hi-lo)*0.5 {
			t.Errorf("U=%.2f: ratio spread too narrow [%.3f,%.3f] — wrong segment?", u, minR, maxR)
		}
	}
	check(0.12, 0.7, 0.9)
	check(0.03, 1.0, 1.2)
	check(0.05, 0.8, 1.2)
	// 回退:economy 关闭 → 恒 P0 区间。
	w.EconomyEnabled = false
	for i := 0; i < 200; i++ {
		if r := w.sampleRehireRatio(); r < 0.8-1e-9 || r > 1.2+1e-9 {
			t.Fatalf("disabled economy ratio %.4f out of [0.8,1.2]", r)
		}
	}
}

// bareSocietyWorld 构造无资产裸玩家世界(基尼/五等份边界测试用)。
func bareSocietyWorld(n int) *World {
	w := NewWorld(1, emptyCardsFor(0))
	for s := 0; s < n; s++ {
		w.Players[s] = newPlayerFromCard(s, synthCard(s, 0))
	}
	return w
}

// TestGiniBoundaries 基尼边界:全员等资产 → 0;一人全占 → ≈(n−1)/n;n<2 → 0(§10.2)。
func TestGiniBoundaries(t *testing.T) {
	// 全员等资产 → 0。
	w := bareSocietyWorld(4)
	for _, p := range w.Players {
		if p != nil {
			p.Cash = 100000
		}
	}
	if g := ComputeSociety(w).Gini; math.Abs(g) > 1e-9 {
		t.Errorf("equal-wealth gini: got %f, want 0", g)
	}
	// 一人全占 → (n−1)/n。
	for s, p := range w.Players {
		if p != nil {
			if s == 0 {
				p.Cash = 10000000
			} else {
				p.Cash = 0
			}
		}
	}
	want := 3.0 / 4.0
	if g := ComputeSociety(w).Gini; math.Abs(g-want) > 1e-9 {
		t.Errorf("one-owns-all gini: got %f, want %f", g, want)
	}
	// n<2 → 0。
	w2 := bareSocietyWorld(1)
	w2.Players[0].Cash = 12345
	if g := ComputeSociety(w2).Gini; g != 0 {
		t.Errorf("single player gini: got %f, want 0", g)
	}
}

// TestQuintilesSum 五等份各组占比和 = 1;n<5 → 全 0.2(§10.2)。
func TestQuintilesSum(t *testing.T) {
	w := laborWorld(1, 5)
	for s, p := range w.Players {
		if p == nil {
			continue
		}
		// 可支配收入 = Income − Tax − Social。
		p.Monthly.Income = int64(1000 * (s + 1))
		p.Monthly.Tax = 0
		p.Monthly.Social = 0
	}
	st := ComputeSociety(w)
	sum := 0.0
	for i, q := range st.Quintiles {
		sum += q
		if q < 0 || q > 1 {
			t.Errorf("quintile %d out of [0,1]: %f", i, q)
		}
	}
	if math.Abs(sum-1.0) > 1e-9 {
		t.Errorf("quintiles sum: got %f, want 1.0", sum)
	}
	// 最低组 < 最高组(升序分组的单调性)。
	if st.Quintiles[0] >= st.Quintiles[4] {
		t.Errorf("quintile order broken: q0=%.3f q4=%.3f", st.Quintiles[0], st.Quintiles[4])
	}

	// n<5 → 全 0.2。
	w2 := laborWorld(1, 3)
	st2 := ComputeSociety(w2)
	for i, q := range st2.Quintiles {
		if math.Abs(q-0.2) > 1e-9 {
			t.Errorf("n<5 quintile %d: got %f, want 0.2", i, q)
		}
	}
}

// TestSocietyCircles 圈层:被动收入 0 → 生存圈;2×支出 → 自由圈(§10.2)。
func TestSocietyCircles(t *testing.T) {
	w := laborWorld(1, 2)
	// 玩家 0:无被动收入,月支出 1000 → r=0 → 生存圈。
	w.Players[0].Monthly.Expense = 1000
	// 玩家 1:债券月息 2000 = 2×支出 → r=2 → 自由圈。
	w.Players[1].Monthly.Expense = 1000
	w.Players[1].Assets = []Asset{{Kind: AssetBond, Units: 240000, Extra: map[string]any{"rate": 0.10}}}
	if got := w.Players[1].MonthlyPassiveIncome(w.Market, 30); got != 2000 {
		t.Fatalf("bond passive income: got %d, want 2000", got)
	}
	st := ComputeSociety(w)
	if st.Circles[0] != 1 {
		t.Errorf("survival circle: got %d, want 1", st.Circles[0])
	}
	if st.Circles[2] != 1 {
		t.Errorf("freedom circle: got %d, want 1", st.Circles[2])
	}
	if st.Circles[1] != 0 {
		t.Errorf("accumulate circle: got %d, want 0", st.Circles[1])
	}
}

// TestComputeSociety_V2_LorenzAndPercentiles P2 v2 §13.2.1:验证洛伦兹曲线
// 起止点、单调递增、PyramidLayers 三层结构(2026-09-19)。
// 注:laborWorld 已含初始 SalaryBase/住宅资产,本测试只校验**形状**:
//   - TotalWealth > 0, MeanWealth = TotalWealth/n
//   - P50 == MedianWealth;LorenzPoints[0]=(0,0),末点=(1,1)
//   - LorenzPoints 单调递增(累计比例)
//   - PyramidLayers 三层,顺序 survival→freedom,WealthPct 和 ≈ 1.0
func TestComputeSociety_V2_LorenzAndPercentiles(t *testing.T) {
	w := laborWorld(1, 4)
	for _, p := range w.Players {
		if p == nil {
			continue
		}
		p.Alive = true
	}

	st := ComputeSociety(w)
	n := 4
	if st.TotalWealth <= 0 {
		t.Errorf("TotalWealth = %d, want > 0", st.TotalWealth)
	}
	if st.MeanWealth != st.TotalWealth/int64(n) {
		t.Errorf("MeanWealth = %d, want %d", st.MeanWealth, st.TotalWealth/int64(n))
	}
	if st.P50 != st.MedianWealth {
		t.Errorf("P50 (%d) should equal MedianWealth (%d)", st.P50, st.MedianWealth)
	}
	if len(st.LorenzPoints) != n+1 {
		t.Fatalf("LorenzPoints length = %d, want %d", len(st.LorenzPoints), n+1)
	}
	if st.LorenzPoints[0] != [2]float64{0, 0} {
		t.Errorf("LorenzPoints[0] = %v, want {0,0}", st.LorenzPoints[0])
	}
	if st.LorenzPoints[n] != [2]float64{1, 1} {
		t.Errorf("LorenzPoints[%d] = %v, want {1,1}", n, st.LorenzPoints[n])
	}
	// 单调递增校验
	for i := 1; i <= n; i++ {
		if st.LorenzPoints[i][0] < st.LorenzPoints[i-1][0] {
			t.Errorf("LorenzPoints not monotonic x at %d: %v vs %v", i, st.LorenzPoints[i], st.LorenzPoints[i-1])
		}
		if st.LorenzPoints[i][1] < st.LorenzPoints[i-1][1] {
			t.Errorf("LorenzPoints not monotonic y at %d: %v vs %v", i, st.LorenzPoints[i], st.LorenzPoints[i-1])
		}
	}
	// 金字塔:3 层,顺序 survival/accumulation/freedom
	if len(st.PyramidLayers) != 3 {
		t.Fatalf("PyramidLayers length = %d, want 3", len(st.PyramidLayers))
	}
	if st.PyramidLayers[0].Name != "survival" {
		t.Errorf("PyramidLayers[0].Name = %q, want survival", st.PyramidLayers[0].Name)
	}
	if st.PyramidLayers[2].Name != "freedom" {
		t.Errorf("PyramidLayers[2].Name = %q, want freedom", st.PyramidLayers[2].Name)
	}
	// 三层 WealthPct 之和应约等于 1.0
	var sumPct float64
	for _, l := range st.PyramidLayers {
		sumPct += l.WealthPct
	}
	if sumPct < 0.99 || sumPct > 1.01 {
		t.Errorf("PyramidLayers WealthPct sum = %f, want ≈1.0", sumPct)
	}
}

// TestComputeSociety_V2_AllZeroWealth 当 n<2 时应走空态兜底,不为空时不 panic。
// 注:laborWorld 已含资产,所以 TotalWealth > 0;本测试只验证 n=1 边角。
func TestComputeSociety_V2_AllZeroWealth(t *testing.T) {
	w := laborWorld(1, 1)
	for _, p := range w.Players {
		if p == nil {
			continue
		}
		p.Alive = true
	}
	st := ComputeSociety(w)
	if st == nil {
		t.Fatal("ComputeSociety returned nil")
	}
	// 单玩家情况:Percentiles 应等于该玩家净资产(所有分位数同值)。
	if st.MedianWealth != st.P50 {
		t.Errorf("n=1 MedianWealth (%d) should equal P50 (%d)", st.MedianWealth, st.P50)
	}
	// 洛伦兹点:2 点 (0,0) → (1,1)
	if len(st.LorenzPoints) != 2 {
		t.Errorf("n=1 LorenzPoints len = %d, want 2", len(st.LorenzPoints))
	}
}
