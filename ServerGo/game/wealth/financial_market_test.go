// Package wealth — financial_market_test.go: 阶段6 金融市场扩展单测
// (2026-09-21 §城市扩张v2.12 阶段6)。
//
// 覆盖:基金评级分层/漂移限制、CD 利率联动/流动性折价(R6-3)、可转债定价/
// 强赎/回售、融券强平/双护栏(R6-1)、量化自适应权重(R6-2)/月度 clamp、
// ⑥B 接线(§130)与固定种子确定性。
package wealth

import (
	"encoding/json"
	"math"
	"testing"

	"LsmAgentGame/game/wealth/profession"
)

// newFinTestWorld 单玩家测试世界(seat 0,现金 cash)。
func newFinTestWorld(seed int64, cash int64) *World {
	return NewWorld(seed, [MaxSeats]profession.Card{{ID: "P01", Title: "测试", Savings: cash}})
}

// ── 1. 基金评级:夏普分层 ──

// TestFundRating_5StarSeparation 分层阈值(纯函数)+ 多空市场路径下
// 高权益基金星数显著高于纯债基金。
func TestFundRating_5StarSeparation(t *testing.T) {
	// 阈值表(契约:夏普>2且回撤<10%→5;>1.5→4;>1→3;>0.5→2;其余 1)。
	cases := []struct {
		sharpe, dd float64
		want       int
	}{
		{2.5, 0.05, 5},  // 夏普>2 且回撤<10%
		{2.5, 0.15, 4},  // 回撤≥10% 降 4 星
		{2.0, 0.05, 4},  // 夏普=2 非严格大于 → 4
		{1.6, 0.05, 4},  // >1.5
		{1.5, 0.05, 3},  // =1.5 非严格大于 → 3
		{1.1, 0.05, 3},  // >1
		{0.6, 0.05, 2},  // >0.5
		{0.5, 0.05, 1},  // =0.5 非严格大于 → 1
		{0.2, 0.05, 1},  // 其余
		{0.2, 0.50, 1},  // 深回撤同 1 星
	}
	for _, c := range cases {
		if got := fundStars(c.sharpe, c.dd); got != c.want {
			t.Errorf("fundStars(%v, %v) = %d, want %d", c.sharpe, c.dd, got, c.want)
		}
	}

	// 集成:真实 SettleMonth 路径(固定种子确定性)。
	// 阶段A 24 月强制繁荣:仿射收益下夏普分层结构性单调 —— 权益仓位越高
	// 趋势市夏普越高(sharpe(w) = (w×μ−fee)/(w×σ√12),μ/σ 同源单调)。
	w := newFinTestWorld(7, 100_000)
	for i := 0; i < 24; i++ {
		w.Market.CyclePhase = PhaseBoom // 防相位重掷漂移。
		if fin, _ := w.SettleMonth(); fin {
			t.Fatalf("game ended prematurely")
		}
	}
	byID := map[string]*FundRating{}
	for i := range w.FundRatings {
		byID[w.FundRatings[i].FundID] = &w.FundRatings[i]
	}
	f01, f02, f04 := byID["F01"], byID["F02"], byID["F04"]
	if !(f04.Sharpe > f02.Sharpe && f02.Sharpe > f01.Sharpe) {
		t.Errorf("sharpe ordering: F04 %.2f / F02 %.2f / F01 %.2f should descend",
			f04.Sharpe, f02.Sharpe, f01.Sharpe)
	}
	// 阶段B 追加 4 月强制衰退:股指下行 → 高权益基金回撤显著大于纯债基金。
	for i := 0; i < 4; i++ {
		w.Market.CyclePhase = PhaseRecession
		if fin, _ := w.SettleMonth(); fin {
			t.Fatalf("game ended prematurely")
		}
	}
	if f04.MaxDrawdown <= f01.MaxDrawdown {
		t.Errorf("drawdown: F04 %.3f should exceed F01 %.3f(权益仓位)", f04.MaxDrawdown, f01.MaxDrawdown)
	}
	for id, f := range byID {
		if f.Stars < 1 || f.Stars > 5 {
			t.Errorf("fund %s stars %d out of [1,5]", id, f.Stars)
		}
		if f.Stars == 5 && f.MaxDrawdown >= FundDD5StarLimit {
			t.Errorf("fund %s 5-star violates dd gate(dd %.3f)", id, f.MaxDrawdown)
		}
	}
}

// ── 2. 基金评级:单月漂移 ≤ 1 星 ──

// TestFundRating_DriftLimit 星数 1 → 优异月最多升到 2;星数 5 → 崩盘月最多降到 4。
func TestFundRating_DriftLimit(t *testing.T) {
	up := &FundRating{Stars: 1, filled: 12, equityW: 1, lastStockIdx: 1}
	for i := 0; i < 12; i++ {
		up.MonthlyReturn[i] = 0.02
	}
	up.rateFundUpdate(0.02, 0.032, 0.032, 0) // 全常数列 → 夏普封顶 9 → raw 5。
	if up.Stars != 2 {
		t.Errorf("drift up: stars = %d, want 2(1 → max +1)", up.Stars)
	}

	down := &FundRating{Stars: 5, filled: 11, equityW: 1, lastStockIdx: 1}
	for i := 0; i < 11; i++ {
		down.MonthlyReturn[i] = 0.02
	}
	down.rateFundUpdate(-0.10, 0.032, 0.032, 0) // 崩盘月 → raw ~2。
	if down.Stars != 4 {
		t.Errorf("drift down: stars = %d, want 4(5 → max −1)", down.Stars)
	}
	if down.filled != 12 {
		t.Errorf("filled = %d, want 12", down.filled)
	}
}

// ── 3. 同业存单:利率跟随 SHIBOR ──

// TestInterbankCD_RateFollowsSHIBOR 票面 = SHIBOR(期限)+30bp;政策利率
// +1% 精确传导 +1%;期限结构向上倾斜。
func TestInterbankCD_RateFollowsSHIBOR(t *testing.T) {
	w := NewWorld(7, [MaxSeats]profession.Card{})
	// 默认 PolicyRate 3% → SHIBOR_1Y = 3%+0.1%+0.2% = 3.3%。
	if got := w.CDMarket.CurRate[12]; math.Abs(got-0.036) > 1e-9 {
		t.Errorf("CurRate[12] = %v, want 0.036(SHIBOR 3.3%% + 30bp)", got)
	}
	if got := w.CDMarket.CurRate[1]; math.Abs(got-0.033) > 1e-9 {
		t.Errorf("CurRate[1] = %v, want 0.033(1M 贴水 30bp 与加点抵消)", got)
	}
	if !(w.CDMarket.CurRate[1] < w.CDMarket.CurRate[3] &&
		w.CDMarket.CurRate[3] < w.CDMarket.CurRate[6] &&
		w.CDMarket.CurRate[6] < w.CDMarket.CurRate[12]) {
		t.Errorf("term structure should be upward sloping: %v", w.CDMarket.CurRate)
	}
	before := w.CDMarket.CurRate[12]
	w.CB.PolicyRate = 0.04 // MLF +1%。
	w.CDMarket.CDMonthlyStep(w)
	if after := w.CDMarket.CurRate[12]; math.Abs(after-before-0.01) > 1e-9 {
		t.Errorf("rate should follow SHIBOR +1%%: %v → %v", before, after)
	}
	// 12 只挂牌(3 行 × 4 档)且展示利率同步。
	if len(w.CDMarket.Listed) != 12 {
		t.Errorf("listed CDs = %d, want 12", len(w.CDMarket.Listed))
	}
	for _, cd := range w.CDMarket.Listed {
		if math.Abs(cd.Rate-w.CDMarket.CurRate[cd.TenureMonths]) > 1e-12 {
			t.Errorf("listed %s rate not synced", cd.ID)
		}
	}
}

// ── 4. 同业存单:二级 1% 流动性折价(R6-3)──

// TestInterbankCD_LiquidityDiscount 持有 6 月二级卖出 → 应计价值 × 99%。
func TestInterbankCD_LiquidityDiscount(t *testing.T) {
	w := newFinTestWorld(7, 2_000_000)
	w.Month = 5
	h, err := w.BuyCD(0, "CD-A-12M", 100) // 100 万元面值。
	if err != nil {
		t.Fatalf("BuyCD: %v", err)
	}
	if h.Rate <= 0 {
		t.Fatalf("locked rate = %v, want > 0", h.Rate)
	}
	if want := int64(1_000_000); w.Players[0].Cash != want {
		t.Fatalf("cash after buy = %d, want %d", w.Players[0].Cash, want)
	}
	w.Month = 11 // 持有 6 月。
	proceeds, err := w.SellCD(0, h.ID)
	if err != nil {
		t.Fatalf("SellCD: %v", err)
	}
	fv := CDAccruedValue(h.FaceWan, h.Rate, 6, 12)
	expected := int64(fv*(1-CDSellDiscount) + 0.5)
	if proceeds != expected {
		t.Errorf("proceeds = %d, want %d(fair %.0f × 99%%)", proceeds, expected, fv)
	}
	// 折价量 ≈ 公允价值 × 1%(取整误差 ≤1 元)。
	if discount := fv - float64(proceeds); math.Abs(discount-fv*CDSellDiscount) > 1.0 {
		t.Errorf("liquidity discount = %.2f, want ≈ %.2f", discount, fv*CDSellDiscount)
	}
	if len(w.CDMarket.Holdings) != 0 {
		t.Errorf("holding should be removed after sell")
	}
	// 到期兑付(另一只 1M 券,持有 ≥1 月全额应计、无折价)。
	h2, err := w.BuyCD(0, "CD-B-1M", 50)
	if err != nil {
		t.Fatalf("BuyCD 1M: %v", err)
	}
	w.Month++
	cashBefore := w.Players[0].Cash
	w.CDMarket.CDMonthlyStep(w)
	if len(w.CDMarket.Holdings) != 0 {
		t.Fatalf("matured holding should be redeemed")
	}
	redeem := int64(CDAccruedValue(50, h2.Rate, 1, 1) + 0.5)
	if got := w.Players[0].Cash - cashBefore; got != redeem {
		t.Errorf("maturity redemption = %d, want %d (no discount)", got, redeem)
	}
}

// ── 5. 可转债:max(债底, 转股价值×delta) ──

// TestConvertibleBond_Pricing 价外(低股指)贴债底;价内(高股指)贴转股价值×delta。
func TestConvertibleBond_Pricing(t *testing.T) {
	floor := cbBondFloor(100, 0.01, 24, 0.03)
	if floor <= 90 || floor >= 100 {
		t.Fatalf("bond floor sanity: %v (want 90..100)", floor)
	}
	// 价外:转股价值 50 → equity 腿 50×0.825 = 41.25 < 债底 → 价格 = 债底。
	if got := CBPrice(100, 0.01, 24, 0.03, 50); math.Abs(got-floor) > 1e-9 {
		t.Errorf("out-of-money price = %v, want floor %v", got, floor)
	}
	// 价内:转股价值 200 → equity 腿 200×0.95 = 190 > 债底。
	if got := CBPrice(100, 0.01, 24, 0.03, 200); math.Abs(got-200*cbDelta(200)) > 1e-9 {
		t.Errorf("in-money price = %v, want %v", got, 200*cbDelta(200))
	}
	// delta = clamp(0.70 + 0.25×cv/100, 0.70, 0.95):下界 / 平价 / 上界。
	if d := cbDelta(0); math.Abs(d-0.70) > 1e-12 {
		t.Errorf("cbDelta(0) = %v, want 0.70(floor)", d)
	}
	if d := cbDelta(100); math.Abs(d-0.95) > 1e-12 {
		t.Errorf("cbDelta(100) = %v, want 0.95(平价)", d)
	}
	if d := cbDelta(200); math.Abs(d-0.95) > 1e-12 {
		t.Errorf("cbDelta(200) = %v, want 0.95(cap)", d)
	}
	// 引擎集成:一步后全部 ≥ 债底且 > 0。
	w := NewWorld(7, [MaxSeats]profession.Card{})
	stepConvertibleBonds(w)
	if len(w.CBonds) != 5 {
		t.Fatalf("CB pool = %d, want 5", len(w.CBonds))
	}
	for _, b := range w.CBonds {
		if b.Price <= 0 || b.Price < b.BondFloor-1e-9 {
			t.Errorf("CB %s price %v violates floor %v", b.ID, b.Price, b.BondFloor)
		}
		if b.IssuerNode == "" {
			t.Errorf("CB %s should link supply chain node", b.ID)
		}
	}
}

// ── 6. 可转债:强赎(>130 连续 3 月)与回售(<70 连续 2 月 + 最后 2 年)──

// TestConvertibleBond_Redemption 强赎触发 + 滚动补发;回售窗口内触发。
func TestConvertibleBond_Redemption(t *testing.T) {
	// 强赎:股指 5.2 → 转股价值 = 100×5.2/3.85 ≈ 135 > 130。
	w := NewWorld(7, [MaxSeats]profession.Card{})
	stepConvertibleBonds(w) // 播种 + 首月估值。
	w.Market.StockIndex = 5.2
	for i := 0; i < 2; i++ {
		stepConvertibleBonds(w)
		for _, b := range w.CBonds[:5] {
			if b.Status != CBStatusActive {
				t.Fatalf("month %d: premature redemption %s", i+1, b.ID)
			}
		}
	}
	stepConvertibleBonds(w) // 第 3 连续月 → 强赎。
	for _, b := range w.CBonds[:5] {
		if b.Status != CBStatusRedeemed {
			t.Errorf("CB %s status = %s, want redeemed(3 连续月 >130)", b.ID, b.Status)
		}
		if b.ExitPrice < 100 {
			t.Errorf("CB %s exit price %v should be ≥ face", b.ID, b.ExitPrice)
		}
	}
	if len(w.CBonds) != 10 {
		t.Errorf("pool after redemption = %d, want 10(5 退出 + 5 补发)", len(w.CBonds))
	}
	for _, b := range w.CBonds[5:] {
		if b.Status != CBStatusActive {
			t.Errorf("replacement %s should be active", b.ID)
		}
	}

	// 回售:股指 2.0 → 转股价值 ≈ 52 < 70;剩余期 ≤ 24 月(最后 2 年)。
	w2 := NewWorld(8, [MaxSeats]profession.Card{})
	stepConvertibleBonds(w2)
	w2.Market.StockIndex = 2.0
	for _, b := range w2.CBonds {
		b.MonthsLeft = 20 // 压入回售窗口。
	}
	stepConvertibleBonds(w2) // loStreak 1。
	for _, b := range w2.CBonds[:5] {
		if b.Status != CBStatusActive {
			t.Fatalf("put premature after 1 month: %s", b.ID)
		}
	}
	stepConvertibleBonds(w2) // loStreak 2 → 回售。
	for _, b := range w2.CBonds[:5] {
		if b.Status != CBStatusPut {
			t.Errorf("CB %s status = %s, want put(2 连续月 <70 且最后 2 年)", b.ID, b.Status)
		}
	}

	// 回售窗口外(剩余 > 24 月)不触发:loStreak 被清零逻辑压制。
	w3 := NewWorld(9, [MaxSeats]profession.Card{})
	stepConvertibleBonds(w3)
	w3.Market.StockIndex = 2.0
	for i := 0; i < 4; i++ {
		stepConvertibleBonds(w3)
	}
	for _, b := range w3.CBonds[:5] {
		if b.Status == CBStatusPut {
			t.Errorf("CB %s should not put outside last-2-years window", b.ID)
		}
		if b.Status != CBStatusActive {
			t.Errorf("CB %s unexpected status %s", b.ID, b.Status)
		}
	}
}

// ── 7. 融券做空:强平线(浮亏 ≥ 保证金 70% ≈ 股价 +35%)──

// TestShortSelling_MarginCall 涨 36% → 强平:返还 保证金−浮亏,状态 margin_call。
func TestShortSelling_MarginCall(t *testing.T) {
	w := newFinTestWorld(7, 1_000_000)
	pos, err := w.OpenShort(0, ShortSymbol, 10000)
	if err != nil {
		t.Fatalf("OpenShort: %v", err)
	}
	if want := 10000.0 * 3.5 * ShortMarginRatio; math.Abs(pos.Margin-want) > 1e-6 {
		t.Fatalf("margin = %v, want %v", pos.Margin, want)
	}
	if want := int64(1_000_000 - 17500); w.Players[0].Cash != want {
		t.Fatalf("cash after open = %d, want %d", w.Players[0].Cash, want)
	}
	// 股价 +36%(> +35% 强平线):浮亏 12600 ≥ 70%×17500 = 12250。
	w.Market.StockIndex = 3.5 * 1.36
	w.ShortBook.ShortMonthlyStep(w)
	if pos.Status != ShortStatusMarginCall {
		t.Errorf("status = %s, want margin_call", pos.Status)
	}
	if w.ShortBook.MarginCallsLastMonth != 1 {
		t.Errorf("margin calls = %d, want 1", w.ShortBook.MarginCallsLastMonth)
	}
	// 结算:利息 + 返还 4900(17500−12600)。
	fee := int64(float64(pos.Shares)*w.Market.StockIndex*ShortBorrowAnnualRate/12 + 0.5)
	if want := int64(1_000_000 - 17500 - fee + 4900); w.Players[0].Cash != want {
		t.Errorf("cash after margin call = %d, want %d", w.Players[0].Cash, want)
	}
	if pos.BorrowFeeCNY != fee {
		t.Errorf("borrow fee = %d, want %d", pos.BorrowFeeCNY, fee)
	}
	// 强平后不再计息/重复强平。
	w.Market.StockIndex = 3.5 * 2.0
	w.ShortBook.ShortMonthlyStep(w)
	if pos.Status != ShortStatusMarginCall || w.ShortBook.MarginCallsLastMonth != 0 {
		t.Errorf("closed position should not be re-processed")
	}
}

// ── 8. 融券做空:R6-1 双护栏 ──

// TestShortSelling_Limits ① 单只 ≤ 流通市值 5%(100 万份);② 融券/融资 ≤ 30%。
func TestShortSelling_Limits(t *testing.T) {
	w := newFinTestWorld(7, 100_000_000)
	if got := ShortFloatCapShares(); got != 1_000_000 {
		t.Fatalf("float cap shares = %d, want 1,000,000", got)
	}
	// ① 流通市值 5% 上限:110 万份 > 100 万份 → 拒绝。
	if _, err := w.OpenShort(0, ShortSymbol, 1_100_000); err == nil {
		t.Errorf("float cap 5%% should reject 1.1M shares")
	}
	// ② 融券/融资比:基数 1000 万 × 30% = 300 万元;90 万份 ×3.5 = 315 万 → 拒绝。
	if _, err := w.OpenShort(0, ShortSymbol, 900_000); err == nil {
		t.Errorf("short/financing 30%% cap should reject ¥3.15M vs ¥3.0M")
	}
	// 合规开仓:20 万份(70 万元 ≤ 300 万元)。
	pos, err := w.OpenShort(0, ShortSymbol, 200_000)
	if err != nil {
		t.Fatalf("legit open should pass: %v", err)
	}
	if pos.Status != ShortStatusOpen {
		t.Errorf("status = %s, want open", pos.Status)
	}
	// 已持 20 万份再开 70 万份:份数 ≤ 上限但余额 315 万 > 300 万 → 拒绝。
	if _, err := w.OpenShort(0, ShortSymbol, 700_000); err == nil {
		t.Errorf("aggregate ratio cap should reject after existing position")
	}
	// 参数校验:非法标的 / 最小份数 / 现金不足。
	if _, err := w.OpenShort(0, "gold", 1000); err == nil {
		t.Errorf("invalid symbol should reject")
	}
	if _, err := w.OpenShort(0, ShortSymbol, 99); err == nil {
		t.Errorf("shares < 100 should reject")
	}
	w.Players[0].Cash = 100
	if _, err := w.OpenShort(0, ShortSymbol, 10000); err == nil {
		t.Errorf("insufficient cash should reject")
	}
	// 主动平仓:价格跌 10% → 盈利返还。
	w.Players[0].Cash = 1_000_000
	w.Market.StockIndex = 3.15
	p2, _ := w.OpenShort(0, ShortSymbol, 10000)
	w.Market.StockIndex = 3.15 * 0.9
	if _, err := w.CloseShort(0, p2.ID); err != nil {
		t.Fatalf("CloseShort: %v", err)
	}
	if p2.Status != ShortStatusClosed || p2.RealizedPnL <= 0 {
		t.Errorf("closed short should be profitable on -10%% move: %+v", p2.RealizedPnL)
	}
}

// ── 9. 量化引擎:权重随周期自适应(R6-2)──

// TestQuantFund_AdaptiveWeights 繁荣期动量权重升,切萧条后均值回归权重升。
func TestQuantFund_AdaptiveWeights(t *testing.T) {
	w := NewWorld(9, [MaxSeats]profession.Card{})
	e := w.QuantEngine
	w.Market.CyclePhase = PhaseBoom
	for i := 0; i < 3; i++ {
		w.Market.StockIndex *= 1.05
		e.MonthlyStep(w)
	}
	momBoom, mrBoom := e.QuantWeight(QuantMomentum), e.QuantWeight(QuantMeanReversion)
	if momBoom <= 0.2 || mrBoom >= 0.2 {
		t.Errorf("boom: momentum %v should rise / mean_reversion %v should fall", momBoom, mrBoom)
	}
	w.Market.CyclePhase = PhaseDepression
	for i := 0; i < 3; i++ {
		w.Market.StockIndex *= 0.94
		e.MonthlyStep(w)
	}
	momDep, mrDep := e.QuantWeight(QuantMomentum), e.QuantWeight(QuantMeanReversion)
	if momDep >= momBoom {
		t.Errorf("depression momentum %v should < boom %v", momDep, momBoom)
	}
	if mrDep <= mrBoom {
		t.Errorf("depression mean_reversion %v should > boom %v", mrDep, mrBoom)
	}
	// softmax 归一化:Σ权重恒 1。
	var sum float64
	for _, s := range e.Strategies {
		sum += s.Weight
	}
	if math.Abs(sum-1) > 1e-9 {
		t.Errorf("weights sum = %v, want 1", sum)
	}
	if len(e.Strategies) != 5 {
		t.Errorf("strategies = %d, want 5", len(e.Strategies))
	}
}

// ── 10. 量化引擎:单月 ±8% clamp ──

// TestQuantFund_MonthlyClamp 单月暴涨/暴跌的组合收益被钳在 ±8%。
func TestQuantFund_MonthlyClamp(t *testing.T) {
	w := NewWorld(9, [MaxSeats]profession.Card{})
	e := w.QuantEngine
	w.Market.StockIndex = InitialStockIndex * 3 // 单月 +200%。
	e.MonthlyStep(w)
	if e.LastRawReturn <= QuantMonthlyClamp {
		t.Errorf("raw return %v should exceed clamp", e.LastRawReturn)
	}
	if e.LastMonthReturn != QuantMonthlyClamp {
		t.Errorf("clamped return = %v, want %v", e.LastMonthReturn, QuantMonthlyClamp)
	}
	if math.Abs(e.Index-100*(1+QuantMonthlyClamp)) > 1e-9 {
		t.Errorf("index = %v, want %v", e.Index, 100*(1+QuantMonthlyClamp))
	}
	// 暴跌方向(新引擎,环比锚 3.5)。
	w2 := NewWorld(9, [MaxSeats]profession.Card{})
	e2 := w2.QuantEngine
	w2.Market.StockIndex = InitialStockIndex * 0.4 // 单月 −60%。
	e2.MonthlyStep(w2)
	if e2.LastMonthReturn != -QuantMonthlyClamp {
		t.Errorf("clamped crash return = %v, want %v", e2.LastMonthReturn, -QuantMonthlyClamp)
	}
	if math.Abs(e2.Index-100*(1-QuantMonthlyClamp)) > 1e-9 {
		t.Errorf("crash index = %v, want %v", e2.Index, 100*(1-QuantMonthlyClamp))
	}
}

// ── 11. §130 接线:SettleMonth ⑥B 真的跑到金融市场引擎 ──

// TestFinancialMarket_WiredIntoSettleMonth 一次 SettleMonth 后:量化引擎
// MonthsRun+1、CD/融台账盖本月戳、评级 5 只已积累样本、可转债已定价,
// 且 view 快照下发 fin_market。
func TestFinancialMarket_WiredIntoSettleMonth(t *testing.T) {
	w := newFinTestWorld(11, 50_000)
	monthBefore := w.Month
	if fin, _ := w.SettleMonth(); fin {
		t.Fatalf("game ended prematurely")
	}
	if w.QuantEngine.MonthsRun != 1 {
		t.Errorf("quant MonthsRun = %d, want 1(⑥B 接线)", w.QuantEngine.MonthsRun)
	}
	if w.CDMarket.LastStepMonth != monthBefore {
		t.Errorf("CD LastStepMonth = %d, want %d", w.CDMarket.LastStepMonth, monthBefore)
	}
	if w.ShortBook.LastStepMonth != monthBefore {
		t.Errorf("ShortBook LastStepMonth = %d, want %d", w.ShortBook.LastStepMonth, monthBefore)
	}
	if len(w.FundRatings) != 5 {
		t.Fatalf("fund ratings = %d, want 5", len(w.FundRatings))
	}
	if w.FundRatings[0].filled < 1 {
		t.Errorf("fund F01 should accumulate sample on first month")
	}
	if len(w.CBonds) != 5 {
		t.Fatalf("CB pool = %d, want 5", len(w.CBonds))
	}
	for _, b := range w.CBonds {
		if b.Price <= 0 {
			t.Errorf("CB %s should be priced on first month(播种即估值)", b.ID)
		}
	}
	// view 接线:game.state 载荷含 fin_market。
	cs := BuildClientState("fin-test", 0, w, [MaxSeats]string{}, [MaxSeats]string{},
		[MaxSeats]bool{}, [MaxSeats]string{}, [MaxSeats]BotTranscript{}, 0, 0, nil)
	if cs.FinMarket == nil {
		t.Fatalf("cs.FinMarket should be wired into view snapshot")
	}
	if cs.FinMarket.QuantIndex <= 0 || len(cs.FinMarket.FundRatings) == 0 ||
		len(cs.FinMarket.Convertibles) == 0 || len(cs.FinMarket.CD.Rates) == 0 {
		t.Errorf("fin_market snapshot incomplete: %+v", cs.FinMarket)
	}
	// economy_enabled=false → ⑥B 跳过 + 快照 omitempty。
	w2 := newFinTestWorld(12, 50_000)
	w2.EconomyEnabled = false
	before := w2.QuantEngine.MonthsRun
	w2.SettleMonth()
	if w2.QuantEngine.MonthsRun != before {
		t.Errorf("economy_enabled=false should skip ⑥B")
	}
}

// ── 12. 固定种子确定性:同种子双世界 8 月全量一致 ──

// TestFinancialMarket_Deterministic 零 rand 消费 + 确定性公式 →
// 同种子 SettleMonth×8 后金融市场状态逐字段一致(含 view JSON)。
func TestFinancialMarket_Deterministic(t *testing.T) {
	const seed = int64(20260921)
	run := func() *World {
		w := newFinTestWorld(seed, 300_000)
		for i := 0; i < 8; i++ {
			if fin, _ := w.SettleMonth(); fin {
				t.Fatalf("game ended prematurely at month %d", w.Month)
			}
		}
		return w
	}
	a, b := run(), run()
	if a.QuantEngine.MonthsRun != 8 || b.QuantEngine.MonthsRun != 8 {
		t.Fatalf("MonthsRun: %d / %d, want 8", a.QuantEngine.MonthsRun, b.QuantEngine.MonthsRun)
	}
	if a.QuantEngine.Index != b.QuantEngine.Index {
		t.Errorf("quant index diverged: %v vs %v", a.QuantEngine.Index, b.QuantEngine.Index)
	}
	if a.QuantEngine.LastMonthReturn != b.QuantEngine.LastMonthReturn {
		t.Errorf("quant last return diverged")
	}
	if a.CDMarket.LastAvgRate != b.CDMarket.LastAvgRate {
		t.Errorf("CD avg rate diverged: %v vs %v", a.CDMarket.LastAvgRate, b.CDMarket.LastAvgRate)
	}
	for t2 := range a.CDMarket.CurRate {
		if a.CDMarket.CurRate[t2] != b.CDMarket.CurRate[t2] {
			t.Errorf("CD CurRate[%d] diverged", t2)
		}
	}
	if len(a.CBonds) != len(b.CBonds) {
		t.Fatalf("CB pool size diverged: %d vs %d", len(a.CBonds), len(b.CBonds))
	}
	for i := range a.CBonds {
		if a.CBonds[i].ID != b.CBonds[i].ID || a.CBonds[i].Status != b.CBonds[i].Status ||
			a.CBonds[i].Price != b.CBonds[i].Price {
			t.Errorf("CB[%d] diverged: %+v vs %+v", i, a.CBonds[i], b.CBonds[i])
		}
	}
	for i := range a.FundRatings {
		x, y := a.FundRatings[i], b.FundRatings[i]
		if x.Stars != y.Stars || x.Sharpe != y.Sharpe || x.AUM != y.AUM {
			t.Errorf("fund %s diverged: %+v vs %+v", x.FundID, x, y)
		}
	}
	// view 快照 JSON 全量一致(排序 tie-break 稳定)。
	ja, _ := json.Marshal(buildFinMarketJSON(a))
	jb, _ := json.Marshal(buildFinMarketJSON(b))
	if string(ja) != string(jb) {
		t.Errorf("fin_market JSON diverged:\n%s\n%s", ja, jb)
	}
}
