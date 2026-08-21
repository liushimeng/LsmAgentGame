package texasholdem

import (
	"context"
	"sync"
	"testing"
)

// 2026-08-21 §20260821-P1-1 — 金币结算回归测试(v1.2:不再做单玩家 clamp,
// 仅保留 pot 级 MaxPotPerHand 缩放)。历史 §B7 单玩家 ±5000 clamp 已在
// R10/R11 报告中被反复复现短付赢家 + 扣款失败,导致筹码守恒被破坏。
// 现测试断言:delta 范围内 → 全额进钱包,pot 超 MaxPotPerHand → 比例缩放
// 赢家,输家按真实 delta 扣款(失败时记 shortfall 由用户在下次入金结算)。

// fakeWallet 记录所有 Credit/Debit 调用(净额)。
type fakeWallet struct {
	mu      sync.Mutex
	credits map[string]int64
	debits  map[string]int64
	rakes   map[string]int64
}

func newFakeWallet() *fakeWallet {
	return &fakeWallet{
		credits: make(map[string]int64),
		debits:  make(map[string]int64),
		rakes:   make(map[string]int64),
	}
}

func (f *fakeWallet) Credit(ctx context.Context, userID, txType, refType, refID, gameKind, remark string, amount int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.credits[userID] += amount
	return nil
}

func (f *fakeWallet) Debit(ctx context.Context, userID, txType, refType, refID, gameKind, remark string, amount int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if refType == "texasholdem_rake" {
		f.rakes[userID] += amount
	} else {
		f.debits[userID] += amount
	}
	return nil
}

// settleRoomWithStacks 构造一个已开局的双人房间,并把结束后的筹码设为指定值。
func settleRoomWithStacks(t *testing.T, m *TexasHoldemManager, roomID string, startStack int, endStacks [2]int) {
	t.Helper()
	m.SetRoomConfig(roomID, 200, startStack)
	if _, _, e := m.JoinGame(roomID, "user-a"); e != nil {
		t.Fatalf("join a: %v", e)
	}
	if _, _, e := m.JoinGame(roomID, "user-b"); e != nil {
		t.Fatalf("join b: %v", e)
	}
	r := m.getRoom(roomID)
	if r == nil {
		t.Fatal("room missing")
	}
	r.mu.Lock()
	r.State.Players[0].Stack = endStacks[0]
	r.State.Players[1].Stack = endStacks[1]
	r.mu.Unlock()
}

// TestSettle_001_DeltaNormal: 单手牌净盈亏 +20000/-20000,2026-08-21
// §20260821-P1-1 起不再 clamp,直接全额进钱包。房间总金币 200K ≥ 50K
// → Health 档 5% 抽水:赢家 credit = 20000 - 5% = 19000, rake = 1000;
// 输家 debit = 20000(无抽水)。先前(R10/R11)±5000 hard clamp 已废除。
func TestSettle_001_DeltaNormal(t *testing.T) {
	m := NewTexasHoldemManager()
	fw := newFakeWallet()
	m.SetWalletService(fw)
	settleRoomWithStacks(t, m, "room-clamp", 100000, [2]int{120000, 80000}) // delta +20000 / -20000

	m.SettleHandCoins("room-clamp")

	if got := fw.credits["user-a"]; got != 19000 {
		t.Errorf("winner credit = %d, want 19000 (20000 - 5%% rake)", got)
	}
	if got := fw.rakes["user-a"]; got != 1000 {
		t.Errorf("winner rake = %d, want 1000", got)
	}
	if got := fw.debits["user-b"]; got != 20000 {
		t.Errorf("loser debit = %d, want 20000 (no clamp, full delta)", got)
	}
}

// TestSettle_002_MaxPotPerHand: 底池 > MaxPotPerHand 时赢家按比例封顶(§B7)。
// §20260821-P1-1 后只剩这一层 cap(单玩家 clamp 已废除)。maxPot=1000,
// pot=8000 → scale 0.125 → 赢家 delta 1000,输家 -8000 不被 clamp。
// 房间总金币 20000 → Caution 档 7% 抽水:赢家 credit = 1000 - 7% = 930。
func TestSettle_002_MaxPotPerHand(t *testing.T) {
	m := NewTexasHoldemManager()
	m.SetMaxPotPerHand(1000)
	fw := newFakeWallet()
	m.SetWalletService(fw)
	settleRoomWithStacks(t, m, "room-maxpot", 10000, [2]int{18000, 2000}) // delta +8000 / -8000, pot=8000

	m.SettleHandCoins("room-maxpot")

	// 赢家: 8000 → pot 缩放到 1000 → 无单玩家 clamp → Caution 7% → credit 930
	if got := fw.credits["user-a"]; got != 930 {
		t.Errorf("winner credit = %d, want 930 (1000 capped - 7%% rake)", got)
	}
	// 输家: -8000 → 不 clamp → 全额 debit 8000
	if got := fw.debits["user-b"]; got != 8000 {
		t.Errorf("loser debit = %d, want 8000 (no clamp, full delta)", got)
	}
}

// TestSettle_003_NormalHandUnclamped: 正常小手牌不触发任何 cap(回归保护)。
// 房间总金币 20000 → Caution 档 7% 抽水:赢家 credit = 300 - 7% = 279。
func TestSettle_003_NormalHandUnclamped(t *testing.T) {
	m := NewTexasHoldemManager()
	fw := newFakeWallet()
	m.SetWalletService(fw)
	settleRoomWithStacks(t, m, "room-normal", 10000, [2]int{10300, 9700}) // delta +300 / -300

	m.SettleHandCoins("room-normal")

	// 赢家: 300 - 7% = 279
	if got := fw.credits["user-a"]; got != 279 {
		t.Errorf("winner credit = %d, want 279", got)
	}
	if got := fw.debits["user-b"]; got != 300 {
		t.Errorf("loser debit = %d, want 300", got)
	}
}

// TestSettle_004_SetMaxPotPerHandZeroKeepsDefault: <=0 注入被忽略(防配置清零)。
func TestSettle_004_SetMaxPotPerHandZeroKeepsDefault(t *testing.T) {
	m := NewTexasHoldemManager()
	m.SetMaxPotPerHand(0)
	m.mu.RLock()
	got := m.maxPotPerHand
	m.mu.RUnlock()
	if got != 100000 {
		t.Errorf("maxPotPerHand = %d, want default 100000", got)
	}
}
