package virtual_city

import (
	"math"
	"math/rand"
	"strings"
	"testing"
)

// 批次66 外盘情绪锚单测:公式确定性 / 零回归 / 封顶 / 保鲜 / 注入链。

func TestExoSignal_EmptyAndExpired(t *testing.T) {
	// 空报价 ⇒ Active=false 且 bias 精确 0.0(零回归契约)。
	s := ComputeExoSignal(nil, 1000)
	if s.Active || s.StockBias != 0 || s.GoldBias != 0 {
		t.Fatalf("empty quotes: %+v, want Active=false bias=0", s)
	}
	// 全部过期(>72h)⇒ 同样置零。
	s = ComputeExoSignal([]ExoQuote{
		{Category: exoCatAShare, ChangePct: 5, UpdatedAt: 1000},
	}, 1000+80*3600)
	if s.Active || s.StockBias != 0 || s.GoldBias != 0 {
		t.Fatalf("expired quotes: %+v, want Active=false bias=0", s)
	}
}

func TestExoSignal_SignMapping(t *testing.T) {
	// 股票大涨 ⇒ Sentiment>0 ⇒ StockBias>0、GoldBias<0(避险反向)。
	s := ComputeExoSignal([]ExoQuote{
		{Category: exoCatAShare, ChangePct: 1.5, UpdatedAt: 5000},
		{Category: exoCatUS, ChangePct: 1.1, UpdatedAt: 5000},
	}, 5000)
	if !s.Active {
		t.Fatal("should be active")
	}
	if s.Sentiment <= 0 || s.StockBias <= 0 || s.GoldBias >= 0 {
		t.Fatalf("bullish mapping wrong: %+v", s)
	}
	// 股票大跌 ⇒ 反向。
	s2 := ComputeExoSignal([]ExoQuote{
		{Category: exoCatHK, ChangePct: -2.0, UpdatedAt: 5000},
	}, 5000)
	if s2.Sentiment >= 0 || s2.StockBias >= 0 || s2.GoldBias <= 0 {
		t.Fatalf("bearish mapping wrong: %+v", s2)
	}
}

func TestExoSignal_CapsAndSaturation(t *testing.T) {
	// 极端 change_pct:tanh 饱和 + 封顶双保险,不得出界。
	s := ComputeExoSignal([]ExoQuote{
		{Category: exoCatAShare, ChangePct: 999, UpdatedAt: 5000},
		{Category: exoCatCrypto, ChangePct: 999, UpdatedAt: 5000},
	}, 5000)
	if math.Abs(s.Sentiment) > 1 || math.Abs(s.StockBias) > exoStockBiasCap+1e-12 ||
		math.Abs(s.GoldBias) > exoGoldBiasCap+1e-12 {
		t.Fatalf("out of bounds: %+v", s)
	}
}

func TestExoSignal_MissingCategoryDegrades(t *testing.T) {
	// 缺类目只降权不置 inactive(如 crypto mission 未完成)。
	s := ComputeExoSignal([]ExoQuote{
		{Category: exoCatMetal, ChangePct: 1.2, UpdatedAt: 5000},
	}, 5000)
	if !s.Active {
		t.Fatalf("metal-only should stay active: %+v", s)
	}
	if s.Sentiment >= 0 { // 金价上涨 ⇒ 风险规避 ⇒ sentiment 负
		t.Fatalf("metal-only mapping wrong: %+v", s)
	}
}

func TestExoSignal_Deterministic(t *testing.T) {
	qs := []ExoQuote{
		{Category: exoCatAShare, ChangePct: 0.82, UpdatedAt: 100},
		{Category: exoCatUS, ChangePct: -0.4, UpdatedAt: 200},
		{Category: exoCatCrypto, ChangePct: 2.1, UpdatedAt: 150},
	}
	a := ComputeExoSignal(qs, 300)
	b := ComputeExoSignal(qs, 300)
	if a != b {
		t.Fatalf("not deterministic: %+v vs %+v", a, b)
	}
	if a.UpdatedAt != 200 {
		t.Fatalf("UpdatedAt should be newest quote: %d", a.UpdatedAt)
	}
}

// TestMonthStep_ZeroExoRegression 零回归核心:bias=0 时 MonthStep 与旧公式
// 逐位一致(同 seed 双跑对比);bias≠0 时漂移方向正确。
func TestMonthStep_ZeroExoRegression(t *testing.T) {
	mk := func() *MarketState {
		m := NewMarket(rand.New(rand.NewSource(42)))
		return m
	}
	// 双跑:一次走 ComputeExoSignal(nil) 全零,一次字段保持零值。
	m1 := mk()
	m2 := mk()
	m1.Exo = ComputeExoSignal(nil, 1)
	r1 := rand.New(rand.NewSource(7))
	r2 := rand.New(rand.NewSource(7))
	m1.MonthStep(r1)
	m2.MonthStep(r2)
	if m1.StockIndex != m2.StockIndex || m1.GoldPrice != m2.GoldPrice {
		t.Fatalf("zero-exo regression: stock %v vs %v, gold %v vs %v",
			m1.StockIndex, m2.StockIndex, m1.GoldPrice, m2.GoldPrice)
	}
	// 偏置生效:+0.003 后股票漂移严格高于零基线(同 seed)。
	m3 := mk()
	m3.Exo = ComputeExoSignal([]ExoQuote{
		{Category: exoCatAShare, ChangePct: 30, UpdatedAt: 1}, // 巨阳 → 封顶
	}, 1)
	if m3.Exo.StockBias <= 0 {
		t.Fatalf("expected positive stock bias, got %+v", m3.Exo)
	}
	base := mk()
	r3 := rand.New(rand.NewSource(7))
	r4 := rand.New(rand.NewSource(7))
	m3.MonthStep(r3)
	base.MonthStep(r4)
	if m3.StockIndex <= base.StockIndex {
		t.Fatalf("exo bias not applied: %v vs %v", m3.StockIndex, base.StockIndex)
	}
	// 黄金反向:正 sentiment ⇒ GoldBias<0 ⇒ 黄金漂移低于基线。
	m4 := mk()
	m4.Exo = m3.Exo
	gb := mk()
	r5 := rand.New(rand.NewSource(7))
	r6 := rand.New(rand.NewSource(7))
	m4.MonthStep(r5)
	gb.MonthStep(r6)
	if m4.GoldPrice >= gb.GoldPrice {
		t.Fatalf("gold bias not applied: %v vs %v", m4.GoldPrice, gb.GoldPrice)
	}
}

func TestExoBrief_ActiveInactive(t *testing.T) {
	w := &World{Market: NewMarket(rand.New(rand.NewSource(1)))}
	// inactive ⇒ 空串整段省略。
	w.Market.Exo = ComputeExoSignal(nil, 1)
	if got := exoBriefLocked(w); got != "" {
		t.Fatalf("inactive should omit brief, got %q", got)
	}
	// active ⇒ 含标题与 realworld_info 引导(批次65 遗留 #1)。
	w.Market.Exo = ComputeExoSignal([]ExoQuote{
		{Category: exoCatAShare, ChangePct: 1, UpdatedAt: 10},
	}, 10)
	got := exoBriefLocked(w)
	if got == "" {
		t.Fatal("active brief empty")
	}
	for _, want := range []string{"真实外盘", "realworld_info", "股票漂移"} {
		if !strings.Contains(got, want) {
			t.Fatalf("brief missing %q: %s", want, got)
		}
	}
}
