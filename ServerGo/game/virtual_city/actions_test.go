package virtual_city

import (
	"testing"

	"LsmAgentGame/errcode"
	"LsmAgentGame/game/virtual_city/profession"
)

// TestActions_BuySellStock I7:股票佣金 0.025% 最低 5 元;批次20 文档3 B2:
// 成交按 ask/bid 单边价、当月买入 T+1 冻结(35044)、月结开头解冻后放行。
func TestActions_BuySellStock(t *testing.T) {
	w := NewWorld(1, [MaxSeats]profession.Card{{ID: "P09"}})
	w.Players[0] = newPlayerFromCard(0, w.Players[0].Card)
	w.Players[0].Cash = 100000
	w.StartGame()
	w.Month = 1

	// 买 10000 元:成交价 = ask 单边(recovery 价差 4bp → idx×1.0002)。
	ask := w.Market.StockBuyUnit()
	wantUnits := float64(int64(10000 / ask)) // floor 口径复核用
	text, err := w.ApplyAction(0, Action{Type: ActBuyAsset, Asset: AssetStockIndex, AmountCNY: 10000})
	if err != nil {
		t.Fatalf("buy stock: %v", err)
	}
	if text == "" {
		t.Errorf("expected non-empty result text")
	}
	at := w.Players[0].assetOf(AssetStockIndex)
	if at == nil || at.Units <= 0 {
		t.Fatalf("stock not held after buy")
	}
	if at.Units != wantUnits {
		t.Errorf("units by ask price: got %v, want %v (mid=%.4f ask=%.4f)", at.Units, wantUnits, w.Market.StockIndex, ask)
	}
	// B2-3 T+1:当月买入全部冻结。
	if w.Players[0].StockT1Locked != int64(at.Units) {
		t.Fatalf("T1 lock: got %d, want %d", w.Players[0].StockT1Locked, int64(at.Units))
	}
	// 同月卖出 → 35044(不再成功)。
	_, err = w.ApplyAction(0, Action{Type: ActSellAsset, Asset: AssetStockIndex, Units: 1000})
	if err == nil || err.Code != errcode.ErrVirtualCityStockT1Locked {
		t.Fatalf("same-month sell must be 35044, got %v", err)
	}
	// 月结开头解冻 → 次月可卖(bid 单边价成交,毛额略低于中价)。
	w.SettleMonth()
	if w.Players[0].StockT1Locked != 0 {
		t.Fatalf("T1 must thaw at settle month head, got %d", w.Players[0].StockT1Locked)
	}
	cashBefore := w.Players[0].Cash
	if _, err = w.ApplyAction(0, Action{Type: ActSellAsset, Asset: AssetStockIndex, Units: 1000}); err != nil {
		t.Fatalf("next-month sell: %v", err)
	}
	if w.Players[0].Cash <= cashBefore {
		t.Errorf("next-month sell must credit cash: %d -> %d", cashBefore, w.Players[0].Cash)
	}
}

// TestActions_Study 测试 study 行为(认知+1,精力-1,现金-2000)。
func TestActions_Study(t *testing.T) {
	w := NewWorld(1, [MaxSeats]profession.Card{{ID: "P05"}})
	w.Players[0] = newPlayerFromCard(0, w.Players[0].Card)
	w.Players[0].Cash = 5000
	w.Players[0].Energy = 5
	w.Players[0].Cognition = 3
	w.StartGame()

	_, err := w.ApplyAction(0, Action{Type: ActStudy})
	if err != nil {
		t.Fatalf("study: %v", err)
	}
	if w.Players[0].Cognition != 4 {
		t.Errorf("cognition: got %d, want 4", w.Players[0].Cognition)
	}
	if w.Players[0].Energy != 4 {
		t.Errorf("energy: got %d, want 4", w.Players[0].Energy)
	}
	if w.Players[0].Cash != 3000 {
		t.Errorf("cash: got %d, want 3000", w.Players[0].Cash)
	}
}

// TestActions_StudyFail_CognitionMax 认知=10 不可学习 → 35010。
func TestActions_StudyFail_CognitionMax(t *testing.T) {
	w := NewWorld(1, [MaxSeats]profession.Card{{ID: "P05"}})
	w.Players[0] = newPlayerFromCard(0, w.Players[0].Card)
	w.Players[0].Cash = 5000
	w.Players[0].Cognition = 10
	w.StartGame()
	_, err := w.ApplyAction(0, Action{Type: ActStudy})
	if err == nil || err.Code != errcode.ErrVirtualCityGateFailed {
		t.Errorf("expected ErrVirtualCityGateFailed, got %v", err)
	}
}

// TestActions_BuyAssetInsufficientCash 现金不足 → 35007。
func TestActions_BuyAssetInsufficientCash(t *testing.T) {
	w := NewWorld(1, [MaxSeats]profession.Card{{ID: "P09"}})
	w.Players[0] = newPlayerFromCard(0, w.Players[0].Card)
	w.Players[0].Cash = 100
	w.StartGame()
	_, err := w.ApplyAction(0, Action{Type: ActBuyAsset, Asset: AssetStockIndex, AmountCNY: 1000})
	if err == nil || err.Code != errcode.ErrVirtualCityInsufficientCash {
		t.Errorf("expected ErrVirtualCityInsufficientCash, got %v", err)
	}
}

// TestActions_TakeLoanConsumerAndCredit consumer ≤ 月收入×12、20万;credit 三档校验。
func TestActions_TakeLoanConsumerAndCredit(t *testing.T) {
	w := NewWorld(1, [MaxSeats]profession.Card{{ID: "P10", Salary: 25000, CreditScore: 700}})
	w.Players[0] = newPlayerFromCard(0, w.Players[0].Card)
	w.Players[0].Cash = 0
	w.StartGame()

	// consumer 50,000(月入×12=30 万 cap 限制)。
	_, err := w.ApplyAction(0, Action{Type: ActTakeLoan, Kind: "consumer", AmountCNY: 50000})
	if err != nil {
		t.Fatalf("consumer loan: %v (budget=%d cash=%d salary=%d credit=%d)",
			err, w.Players[0].ActionBudget, w.Players[0].Cash,
			w.Players[0].SalaryBase, w.Players[0].CreditScore)
	}
	// credit 50000:必须有 credit ≥500 信用分。
	w.Players[0].CreditScore = 400 // 降为 E
	_, err = w.ApplyAction(0, Action{Type: ActTakeLoan, Kind: "credit", AmountCNY: 50000})
	if err == nil || err.Code != errcode.ErrVirtualCityLoanInvalid {
		t.Errorf("credit with E grade should fail, got %v", err)
	}
	w.Players[0].CreditScore = 600 // B
	_, err = w.ApplyAction(0, Action{Type: ActTakeLoan, Kind: "credit", AmountCNY: 75000})
	if err == nil {
		t.Errorf("credit with non-standard amount should fail")
	}
}

// TestActions_BudgetExhausted 月动作预算耗尽 → 35006。
func TestActions_BudgetExhausted(t *testing.T) {
	w := NewWorld(1, [MaxSeats]profession.Card{{ID: "P05"}})
	w.Players[0] = newPlayerFromCard(0, w.Players[0].Card)
	w.Players[0].Cash = 100000
	w.StartGame()
	w.Players[0].ActionBudget = 0
	_, err := w.ApplyAction(0, Action{Type: ActStudy})
	if err == nil || err.Code != errcode.ErrVirtualCityActionBudgetExhausted {
		t.Errorf("expected ErrVirtualCityActionBudgetExhausted, got %v", err)
	}
}

// TestActions_BondLockRate 债券锁定利率(追加买入按权重平均)。
func TestActions_BondLockRate(t *testing.T) {
	w := NewWorld(1, [MaxSeats]profession.Card{{ID: "P05"}})
	w.Players[0] = newPlayerFromCard(0, w.Players[0].Card)
	w.Players[0].Cash = 100000
	w.StartGame()
	w.Market.CyclePhase = PhaseBoom
	_, err := w.ApplyAction(0, Action{Type: ActBuyAsset, Asset: AssetBond, AmountCNY: 1000})
	if err != nil {
		t.Fatalf("buy bond: %v", err)
	}
	at := w.Players[0].assetOf(AssetBond)
	if at == nil || at.AssetRate() != 0.027 {
		t.Errorf("bond rate: got %f, want 0.027 (boom)", at.AssetRate())
	}
}