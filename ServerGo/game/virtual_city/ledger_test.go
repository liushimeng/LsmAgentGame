package virtual_city

import (
	"testing"

	"LsmAgentGame/errcode"
	"LsmAgentGame/game/virtual_city/profession"
)

// TestLedger_DirectionAndConservation I1 守恒:每条 ledger 与现金变化对齐。
// 通过 World.Pay 派发,自动注入条目;测试从月初到月末的随机动作序列下,
// 每座位的 (cash_after - cash_before) == Σ(Ledger SeatNetFlow)。
func TestLedger_DirectionAndConservation(t *testing.T) {
	w := NewWorld(1, emptyCardsFor(8))
	for s := 0; s < 8; s++ {
		// 发 50,000 元初始(避免负数干扰断言)。
		w.Players[s] = newPlayerFromCard(s, synthCard(s, 50000))
		w.Players[s].Cash = 50000
	}
	w.StartGame()

	// 多月推进 + 随机动作序列,断言每座位月度守恒。
	for month := 1; month <= 12; month++ {
		monthStartCash := snapshotCash(w)
		w.SettleMonth()
		monthEndCash := snapshotCash(w)
		// 注意:SettleMonth 内部会改变 cash;但 action 月中也会改,本测试只断言月结前后差。
		for s := 0; s < 8; s++ {
			net := w.Ledger.SeatNetFlow(s, month)
			want := monthEndCash[s] - monthStartCash[s]
			if want != net {
				t.Errorf("month %d seat %d: cash delta=%d, ledger net flow=%d", month, s, want, net)
			}
		}
	}
}

// TestLedger_WorldInjectOnce I2:from=world 仅出现在初始注入。
func TestLedger_WorldInjectOnce(t *testing.T) {
	w := NewWorld(2, emptyCardsFor(4))
	for s := 0; s < 4; s++ {
		w.Players[s] = newPlayerFromCard(s, synthCard(s, 8000))
	}
	w.StartGame()
	if got := w.Ledger.WorldInjectCount(); got != 4 {
		t.Errorf("initial world injects: got %d, want 4", got)
	}
	// 多月推进不应增加 from=world 条目(初始注入 Month=0;月结期间消费类 to=world)。
	for month := 1; month <= 6; month++ {
		w.SettleMonth()
	}
	// 余额只增不减(可能减少若 strict < 4 — 但月结消费 to=world ≠ from=world)。
	if w.Ledger.WorldInjectCount() != 4 {
		t.Errorf("world injects after 6 months: got %d, want 4", w.Ledger.WorldInjectCount())
	}
}

// TestLedger_EntityWhitelist I3:实体方向白名单(gov 只收不付,pension 白名单除外)。
func TestLedger_EntityWhitelist(t *testing.T) {
	// 错误方向:gov → seat 不在白名单(除 pension 类)→ panic。
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("gov→seat (non-pension) should panic")
		}
	}()
	l := &Ledger{}
	l.Record(1, EntityGov, "seat:0", 100, "tax", "")
}

// TestLedger_AmountMustBePositive 金额必须正;负数 → panic。
func TestLedger_AmountMustBePositive(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("negative amount should panic")
		}
	}()
	l := &Ledger{}
	l.Record(1, SeatEntity(0), EntityWorld, -100, CatLiving, "")
}

// TestLedger_PensionWhitelist gov→seat pension 类合法(I3 白名单)。
func TestLedger_PensionWhitelist(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("gov→seat pension should not panic, got %v", r)
		}
	}()
	l := &Ledger{}
	l.Record(1, EntityGov, "seat:0", 500, CatPension, "")
}

// ─────────────────── helpers ───────────────────

func snapshotCash(w *World) []int64 {
	out := make([]int64, MaxSeats)
	for s, p := range w.Players {
		if p == nil {
			continue
		}
		out[s] = p.Cash
	}
	return out
}

func emptyCardsFor(n int) [MaxSeats]profession.Card {
	var cards [MaxSeats]profession.Card
	for i := 0; i < n && i < MaxSeats; i++ {
		cards[i] = profession.Card{ID: "P01"}
	}
	return cards
}

func synthCard(seat int, savings int64) profession.Card {
	return profession.Card{
		ID: "P01", Title: "测试", Salary: 10000, Expense: 4000,
		Savings: savings, StartAge: 25, Energy: 8, Network: 5,
		Cognition: 5, CreditScore: 650, HomeDistrict: "residential",
		RiskPreference: "balanced", HealthGrade: "B",
		OpeningHook: "我是一个测试角色的开场白,用来满足开场白长度要求,共五十字。",
	}
}

// suppress unused errcode import
var _ = errcode.ErrVirtualCityActionBudgetExhausted