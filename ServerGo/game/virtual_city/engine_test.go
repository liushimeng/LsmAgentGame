package virtual_city

import (
	"testing"

	"LsmAgentGame/game/virtual_city/profession"
)

// TestNewWorld_Construction 8 座构造 + 初始值。
func TestNewWorld_Construction(t *testing.T) {
	w := NewWorld(7, [MaxSeats]profession.Card{{ID: "P01", Title: "外卖骑手"}})
	if w.Month != 1 {
		t.Errorf("month: got %d, want 1", w.Month)
	}
	if w.Status != StatusOpen {
		t.Errorf("status: got %s, want open", w.Status)
	}
	if w.Market.StockIndex != InitialStockIndex {
		t.Errorf("stock: got %f", w.Market.StockIndex)
	}
	if w.Players[0] == nil {
		t.Errorf("seat 0 should be populated (card.ID=P01)")
	} else if w.Players[0].Card.ID != "P01" {
		t.Errorf("seat 0 card.ID: got %s, want P01", w.Players[0].Card.ID)
	}
	for s := 0; s < MaxSeats; s++ {
		if s != 0 && w.Players[s] != nil {
			t.Errorf("seat %d should be nil", s)
		}
	}
}

// TestWorld_Age 主时钟年龄 = 25 + floor((month-1)/12)。
func TestWorld_Age(t *testing.T) {
	w := NewWorld(1, [MaxSeats]profession.Card{{ID: "P01"}})
	w.Month = 1
	if w.Age() != 25 {
		t.Errorf("month 1 age: got %d, want 25", w.Age())
	}
	w.Month = 12
	if w.Age() != 25 {
		t.Errorf("month 12 age: got %d, want 25", w.Age())
	}
	w.Month = 13
	if w.Age() != 26 {
		t.Errorf("month 13 age: got %d, want 26", w.Age())
	}
	w.Month = 420
	if w.Age() != 59 {
		t.Errorf("month 420 age: got %d, want 59", w.Age())
	}
}

// TestStartGame_InitialInject 开局后 world→seat 注入条目 = 8(座位数)。
func TestStartGame_InitialInject(t *testing.T) {
	w := NewWorld(1, [MaxSeats]profession.Card{{ID: "P01", Savings: 8000}})
	w.Players[0] = newPlayerFromCard(0, w.Players[0].Card)
	w.StartGame()
	if got := w.Ledger.WorldInjectCount(); got != 1 {
		t.Errorf("initial world injects: got %d, want 1", got)
	}
}

// TestNewWorld_EmptySeats 空座位(无卡)应 nil。
func TestNewWorld_EmptySeats(t *testing.T) {
	w := NewWorld(1, [MaxSeats]profession.Card{})
	for s := 0; s < MaxSeats; s++ {
		if w.Players[s] != nil {
			t.Errorf("seat %d should be nil", s)
		}
	}
}

// TestNewPlayerFromCard_BasicFields Card 字段正确镜像到 Player。
func TestNewPlayerFromCard_BasicFields(t *testing.T) {
	// 2026-09-22 §17-CityHuman(契约 03 §4):精选手卡夹具退役,改内联卡面。
	card := &profession.Card{
		ID: "T01", Title: "测试职业", Salary: 9000, Expense: 6000, Savings: 30000,
		StartAge: 25, Energy: 6, Network: 4, Cognition: 5, CreditScore: 650,
		HomeDistrict: "residential", RiskPreference: "conservative",
		HealthGrade: "B", Marital: "single",
		OpeningHook: "这是一张内联测试职业卡,用于镜像字段断言的固定夹具。",
		Source:      "synthetic",
	}
	p := newPlayerFromCard(3, *card)
	if p.Seat != 3 {
		t.Errorf("seat: got %d", p.Seat)
	}
	if p.Cash != card.Savings {
		t.Errorf("cash: got %d, want %d", p.Cash, card.Savings)
	}
	if p.Energy != card.Energy {
		t.Errorf("energy mismatch")
	}
	if p.HomeDistrict != card.HomeDistrict {
		t.Errorf("home district mismatch")
	}
	if p.SalaryBase != card.Salary {
		t.Errorf("salary base mismatch")
	}
	if !p.Alive {
		t.Errorf("should start alive")
	}
}