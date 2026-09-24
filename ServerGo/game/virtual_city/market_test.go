package virtual_city

import (
	"math"
	"math/rand"
	"testing"
)

// TestMarket_PhaseTable 四阶段参数固定(后端架构 §5.1)。
func TestMarket_PhaseTable(t *testing.T) {
	expect := map[CyclePhase]PhaseParams{
		PhaseRecovery:   {0.035, 0.02, 0.15, 0.05, -0.05, 0.032, 0.95},
		PhaseBoom:       {0.045, 0.035, 0.30, 0.15, -0.10, 0.027, 0.95},
		PhaseRecession:  {0.058, 0.02, -0.25, -0.05, 0.10, 0.037, 0.80},
		PhaseDepression: {0.028, 0.00, -0.40, -0.15, 0.25, 0.042, 0.65},
	}
	for ph, want := range expect {
		got := PhaseTable[ph]
		if math.Abs(got.LPR-want.LPR) > 1e-9 ||
			math.Abs(got.CPI-want.CPI) > 1e-9 ||
			math.Abs(got.StockAnnual-want.StockAnnual) > 1e-9 ||
			math.Abs(got.HouseAnnual-want.HouseAnnual) > 1e-9 ||
			math.Abs(got.GoldAnnual-want.GoldAnnual) > 1e-9 ||
			math.Abs(got.BondRate-want.BondRate) > 1e-9 ||
			math.Abs(got.EmploymentPct-want.EmploymentPct) > 1e-9 {
			t.Errorf("phase %s params mismatch: got %+v, want %+v", ph, got, want)
		}
	}
}

// TestMarket_NewAndDrift 初始值 + 月度漂移(确定性,seed 固定)。
func TestMarket_NewAndDrift(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	m := NewMarket(rng)
	if m.StockIndex != InitialStockIndex {
		t.Errorf("initial stock: got %f, want %f", m.StockIndex, InitialStockIndex)
	}
	if m.GoldPrice != InitialGoldPrice {
		t.Errorf("initial gold: got %f, want %f", m.GoldPrice, float64(InitialGoldPrice))
	}
	for _, d := range DistrictDefs {
		if m.DistrictIdx[d.ID] != 1.0 {
			t.Errorf("initial idx %s: got %f, want 1.0", d.ID, m.DistrictIdx[d.ID])
		}
	}
	for i := 0; i < 24; i++ {
		m.MonthStep(rng)
	}
	if m.StockIndex <= 0.01 {
		t.Errorf("stock floor violated: %f", m.StockIndex)
	}
	if m.GoldPrice <= 1 {
		t.Errorf("gold floor violated: %f", m.GoldPrice)
	}
}

// TestMarket_CycleReroll 阶段转移概率:每次强制起始于 Depression,断言 Recovery 命中率 ~70%。
func TestMarket_CycleReroll(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	m := NewMarket(rng)
	// Smoke:matrix 行/列齐备。
	rows, ok := transitionTable[PhaseDepression]
	if !ok || len(rows) != 2 {
		t.Fatalf("matrix row missing or wrong length: %d", len(rows))
	}
	sum := 0.0
	for _, r := range rows {
		sum += r.P
	}
	if math.Abs(sum-1.0) > 1e-9 {
		t.Errorf("row prob sum: got %f, want 1.0", sum)
	}
	// 每次强制重置为 Depression;只测 Depression→Recovery 命中。
	const M2 = 10000
	rec := 0
	for i := 0; i < M2; i++ {
		m.CyclePhase = PhaseDepression
		m.RerollPhase(rng)
		if m.CyclePhase == PhaseRecovery {
			rec++
		}
	}
	rate := float64(rec) / float64(M2)
	if rate < 0.60 || rate > 0.80 {
		t.Errorf("depression→recovery observed rate %.3f, expected ~0.70", rate)
	}
}

// TestMarket_HousePriceFormula §4 公式: base_wan × 10000 × beta × idx。
func TestMarket_HousePriceFormula(t *testing.T) {
	m := &MarketState{DistrictIdx: map[string]float64{}}
	for _, d := range DistrictDefs {
		m.DistrictIdx[d.ID] = 1.0
	}
	price := m.HousePrice("finance")
	want := int64(math.Round(800 * 10000 * 1.3 * 1.0))
	if price != want {
		t.Errorf("finance price: got %d, want %d", price, want)
	}
	m.DistrictIdx["finance"] = 1.2
	price2 := m.HousePrice("finance")
	want2 := int64(math.Round(800 * 10000 * 1.3 * 1.2))
	if price2 != want2 {
		t.Errorf("finance*1.2 price: got %d, want %d", price2, want2)
	}
}

// TestMarket_RentFactor 住宅 0.16% / 商铺 0.35% 月租。
func TestMarket_RentFactor(t *testing.T) {
	if math.Abs(HouseRentFactor-0.0016) > 1e-9 {
		t.Errorf("house rent factor: got %f, want 0.0016", HouseRentFactor)
	}
	if math.Abs(ShopRentFactor-0.0035) > 1e-9 {
		t.Errorf("shop rent factor: got %f, want 0.0035", ShopRentFactor)
	}
}
