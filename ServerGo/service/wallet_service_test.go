// Package service — wallet_service unit tests.
//
// Uses the standard `testing` package only (matches project conventions, see
// auth_service_test.go). getDB short-circuits via t.Skip when the configured
// database isn't reachable so CI without MariaDB doesn't fail the whole suite.
package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"LsmAgentGame/config"
	"LsmAgentGame/db"
	"LsmAgentGame/errcode"
	"LsmAgentGame/models"
	"LsmAgentGame/util"

	"gorm.io/gorm"
)

// testDB lazily initializes (and memoizes) a single shared GORM handle for the
// wallet test suite.
var testDB *gorm.DB

func getDB(t *testing.T) *gorm.DB {
	t.Helper()
	if testDB != nil {
		return testDB
	}
	cfg := config.Load()
	gormDB, err := db.Init(cfg)
	if err != nil {
		t.Skipf("skip: cannot connect to DB: %v", err)
		return nil
	}
	testDB = gormDB
	return testDB
}

// assert helper (project-style — no testify).
func assertEq(t *testing.T, want, got int64) {
	t.Helper()
	if want != got {
		t.Fatalf("want %d got %d", want, got)
	}
}

func assertNoErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
}

func assertErrCode(t *testing.T, err error, code int) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected err code %d, got nil", code)
	}
	ce := errcode.AsError(err)
	if ce.Code != code {
		t.Fatalf("expected err code %d, got %d (%s)", code, ce.Code, ce.Message)
	}
}

func TestCreateWallet(t *testing.T) {
	gormDB := getDB(t)
	if gormDB == nil {
		return
	}
	ctx := context.Background()
	ws := NewWalletService(gormDB)

	uid := util.NewUUID()
	assertNoErr(t, ws.CreateWallet(ctx, uid, 0))

	bal, err := ws.GetBalance(ctx, uid)
	assertNoErr(t, err)
	assertEq(t, int64(DefaultInitialBalance), bal)

	// The register_bonus ledger row must exist with matching balance_after.
	var txRow models.TLsmGameWalletTx
	assertNoErr(t, gormDB.Where("user_id = ? AND tx_type = ?", uid,
		string(TxTypeRegisterBonus)).First(&txRow).Error)
	assertEq(t, int64(DefaultInitialBalance), txRow.Amount)
	assertEq(t, int64(DefaultInitialBalance), txRow.BalanceAfter)
}

func TestCreateWallet_CustomInitial(t *testing.T) {
	gormDB := getDB(t)
	if gormDB == nil {
		return
	}
	ctx := context.Background()
	ws := NewWalletService(gormDB)
	uid := util.NewUUID()
	assertNoErr(t, ws.CreateWallet(ctx, uid, 500))

	bal, _ := ws.GetBalance(ctx, uid)
	assertEq(t, int64(500), bal)
}

func TestCredit(t *testing.T) {
	gormDB := getDB(t)
	if gormDB == nil {
		return
	}
	ctx := context.Background()
	ws := NewWalletService(gormDB)
	uid := util.NewUUID()
	assertNoErr(t, ws.CreateWallet(ctx, uid, 500))

	assertNoErr(t, ws.Credit(ctx, uid, string(TxTypeWinReward), "room", "r1", "xiangqi", "测试胜利", 300))
	bal, _ := ws.GetBalance(ctx, uid)
	assertEq(t, int64(800), bal)

	var rows []models.TLsmGameWalletTx
	assertNoErr(t, gormDB.Where("user_id = ?", uid).Order("created_at ASC").Find(&rows).Error)
	if len(rows) != 2 {
		t.Fatalf("want 2 ledger rows, got %d", len(rows))
	}
	assertEq(t, int64(800), rows[1].BalanceAfter)
}

func TestDebit_Insufficient(t *testing.T) {
	gormDB := getDB(t)
	if gormDB == nil {
		return
	}
	ctx := context.Background()
	ws := NewWalletService(gormDB)
	uid := util.NewUUID()
	assertNoErr(t, ws.CreateWallet(ctx, uid, 500))

	err := ws.Debit(ctx, uid, string(TxTypeAnteBuyin), "room", "r1", "xiangqi", "ante", 1000)
	assertErrCode(t, err, errcode.ErrWalletInsufficientBalance)

	// Balance must be unchanged.
	bal, _ := ws.GetBalance(ctx, uid)
	assertEq(t, int64(500), bal)
}

func TestDebit_Success(t *testing.T) {
	gormDB := getDB(t)
	if gormDB == nil {
		return
	}
	ctx := context.Background()
	ws := NewWalletService(gormDB)
	uid := util.NewUUID()
	assertNoErr(t, ws.CreateWallet(ctx, uid, 500))

	assertNoErr(t, ws.Debit(ctx, uid, string(TxTypeAnteBuyin), "room", "r1", "xiangqi", "ante", 200))
	bal, _ := ws.GetBalance(ctx, uid)
	assertEq(t, int64(300), bal)

	var txRow models.TLsmGameWalletTx
	assertNoErr(t, gormDB.Where("user_id = ? AND tx_type = ?", uid,
		string(TxTypeAnteBuyin)).First(&txRow).Error)
	assertEq(t, int64(-200), txRow.Amount)
	assertEq(t, int64(300), txRow.BalanceAfter)
}

func TestTransfer(t *testing.T) {
	gormDB := getDB(t)
	if gormDB == nil {
		return
	}
	ctx := context.Background()
	ws := NewWalletService(gormDB)
	from := util.NewUUID()
	to := util.NewUUID()
	assertNoErr(t, ws.CreateWallet(ctx, from, 1000))
	assertNoErr(t, ws.CreateWallet(ctx, to, 500))

	assertNoErr(t, ws.Transfer(ctx, from, to, "room", "r1", "xiangqi", "对局结算", 300))
	fromBal, _ := ws.GetBalance(ctx, from)
	toBal, _ := ws.GetBalance(ctx, to)
	assertEq(t, int64(700), fromBal)
	assertEq(t, int64(800), toBal)

	// Two new ledger rows — locate the transfer entries specifically.
	var fromRows, toRows []models.TLsmGameWalletTx
	_ = gormDB.Where("user_id = ? AND ref_id = ?", from, "r1").Find(&fromRows).Error
	_ = gormDB.Where("user_id = ? AND ref_id = ?", to, "r1").Find(&toRows).Error
	var fromXfer, toXfer *models.TLsmGameWalletTx
	for i := range fromRows {
		if fromRows[i].TxType == string(TxTypeLoseDeduct) {
			fromXfer = &fromRows[i]
		}
	}
	for i := range toRows {
		if toRows[i].TxType == string(TxTypeWinReward) {
			toXfer = &toRows[i]
		}
	}
	if fromXfer == nil || toXfer == nil {
		t.Fatalf("missing transfer ledger rows: from=%v to=%v", fromXfer, toXfer)
	}
	assertEq(t, int64(-300), fromXfer.Amount)
	assertEq(t, int64(300), toXfer.Amount)
	assertEq(t, int64(700), fromXfer.BalanceAfter)
	assertEq(t, int64(800), toXfer.BalanceAfter)
}

func TestTransfer_Insufficient(t *testing.T) {
	gormDB := getDB(t)
	if gormDB == nil {
		return
	}
	ctx := context.Background()
	ws := NewWalletService(gormDB)
	from := util.NewUUID()
	to := util.NewUUID()
	assertNoErr(t, ws.CreateWallet(ctx, from, 100))
	assertNoErr(t, ws.CreateWallet(ctx, to, 100))

	err := ws.Transfer(ctx, from, to, "", "", "", "x", 200)
	assertErrCode(t, err, errcode.ErrWalletInsufficientBalance)
}

func TestDailyReward_Idempotent(t *testing.T) {
	gormDB := getDB(t)
	if gormDB == nil {
		return
	}
	ctx := context.Background()
	ws := NewWalletService(gormDB)
	uid := util.NewUUID()
	assertNoErr(t, ws.CreateWallet(ctx, uid, 0))

	now := time.Now()
	first, err := ws.ClaimDailyReward(ctx, uid, now)
	assertNoErr(t, err)
	if first <= 0 {
		t.Fatalf("first claim should credit and return positive balance, got %d", first)
	}

	second, err := ws.ClaimDailyReward(ctx, uid, now)
	assertErrCode(t, err, errcode.ErrWalletDailyRewardClaimed)
	assertEq(t, int64(0), second)
}

func TestDailyReward_DedupRowCount(t *testing.T) {
	gormDB := getDB(t)
	if gormDB == nil {
		return
	}
	ctx := context.Background()
	ws := NewWalletService(gormDB)
	uid := util.NewUUID()
	assertNoErr(t, ws.CreateWallet(ctx, uid, 0))

	now := time.Now()
	_, err := ws.ClaimDailyReward(ctx, uid, now)
	assertNoErr(t, err)
	// Re-run to confirm count stays at 1.
	_, _ = ws.ClaimDailyReward(ctx, uid, now)
	var dedupCount int64
	_ = gormDB.Model(&models.TLsmGameDailyReward{}).Where("user_id = ?", uid).Count(&dedupCount).Error
	assertEq(t, int64(1), dedupCount)
}

func TestDailyReward_DifferentDays(t *testing.T) {
	gormDB := getDB(t)
	if gormDB == nil {
		return
	}
	ctx := context.Background()
	ws := NewWalletService(gormDB)
	uid := util.NewUUID()
	assertNoErr(t, ws.CreateWallet(ctx, uid, 0))

	// Separate calendar days must both credit.
	yesterday := time.Now().Add(-24 * time.Hour)
	today := time.Now()
	_, err := ws.ClaimDailyReward(ctx, uid, yesterday)
	assertNoErr(t, err)
	_, err = ws.ClaimDailyReward(ctx, uid, today)
	assertNoErr(t, err)
	bal, _ := ws.GetBalance(ctx, uid)
	// CreateWallet(0) → 1000 注册奖励 + 昨日 2000 + 今日 2000 = 5000
	assertEq(t, int64(DefaultInitialBalance)+2*int64(DefaultDailyLoginReward), bal)
}

func TestListTransactions(t *testing.T) {
	gormDB := getDB(t)
	if gormDB == nil {
		return
	}
	ctx := context.Background()
	ws := NewWalletService(gormDB)
	uid := util.NewUUID()
	assertNoErr(t, ws.CreateWallet(ctx, uid, 1000))

	for i := 0; i < 3; i++ {
		assertNoErr(t, ws.Credit(ctx, uid, string(TxTypeTaskReward), "task", "t1", "", "奖励", 100))
	}

	rows, total, err := ws.ListTransactions(ctx, uid, 10, 0)
	assertNoErr(t, err)
	// 1 register + 3 task = 4.
	assertEq(t, int64(4), total)
	if len(rows) != 4 {
		t.Fatalf("want 4 rows, got %d", len(rows))
	}
	// Newest first.
	if rows[0].TxType != string(TxTypeTaskReward) {
		t.Fatalf("newest row should be %s, got %s", TxTypeTaskReward, rows[0].TxType)
	}
}

func TestLedgerAndWallet_Consistent(t *testing.T) {
	gormDB := getDB(t)
	if gormDB == nil {
		return
	}
	ctx := context.Background()
	ws := NewWalletService(gormDB)
	uid := util.NewUUID()
	assertNoErr(t, ws.CreateWallet(ctx, uid, 1000))

	assertNoErr(t, ws.Debit(ctx, uid, string(TxTypeAnteBuyin), "r1", "", "xiangqi", "ante", 200))
	assertNoErr(t, ws.Credit(ctx, uid, string(TxTypeWinReward), "r1", "", "xiangqi", "win", 500))
	assertNoErr(t, ws.Debit(ctx, uid, string(TxTypeAnteBuyin), "r2", "", "chess", "ante", 150))

	walletBal, _ := ws.GetBalance(ctx, uid)
	var lastTx models.TLsmGameWalletTx
	assertNoErr(t, gormDB.Where("user_id = ?", uid).Order("created_at DESC").First(&lastTx).Error)
	if walletBal != lastTx.BalanceAfter {
		t.Fatalf("last ledger balance_after %d != wallet.balance %d", lastTx.BalanceAfter, walletBal)
	}
	// 1000 - 200 + 500 - 150 = 1150
	assertEq(t, int64(1150), walletBal)
}

func TestGetBalance_MissingWallet(t *testing.T) {
	gormDB := getDB(t)
	if gormDB == nil {
		return
	}
	ctx := context.Background()
	ws := NewWalletService(gormDB)
	uid := util.NewUUID()

	bal, err := ws.GetBalance(ctx, uid)
	assertNoErr(t, err)
	assertEq(t, int64(0), bal)
}

func TestIsMySQLDuplicate(t *testing.T) {
	if isMySQLDuplicate(nil) {
		t.Fatal("nil should not match")
	}
	if isMySQLDuplicate(errors.New("random")) {
		t.Fatal("non-mysql err should not match")
	}
}

// ensureRootUser upserts the genesis root account so registration has a
// valid referrer. Idempotent — works whether the row exists or not.
func ensureRootUser(t *testing.T, gormDB *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	var existing models.TLsmGameUser
	err := gormDB.Where("my_invite_code = ?", RootInviteCode).First(&existing).Error
	if err == nil {
		return // already seeded
	}
	agg, _ := util.HashPassword("Lsm@Root2026_Test")
	root := models.TLsmGameUser{
		ID:           util.NewUUID(),
		Account:      "lsm_root_test_" + util.NewUUID()[:8],
		Nickname:     "root_test",
		PasswordHash: agg,
		MyInviteCode: RootInviteCode,
		Language:     "zh-CN",
	}
	assertNoErr(t, gormDB.WithContext(ctx).Create(&root).Error)
}

func TestRegisterCreatesWallet(t *testing.T) {
	gormDB := getDB(t)
	if gormDB == nil {
		return
	}
	cfg := config.Load()
	authSvc := NewAuthService(gormDB, cfg, nil)
	ws := NewWalletService(gormDB)
	authSvc.SetWalletService(ws)

	// Make sure referrer exists before registering.
	ensureRootUser(t, gormDB)

	ctx := context.Background()
	code := util.NewUUID()[:12]
	acct := "wtest_reg_" + code
	resp, err := authSvc.Register(ctx, RegisterInput{
		Account:      acct,
		Password:     "test_pwd_" + code,
		ReferrerCode: RootInviteCode,
	})
	assertNoErr(t, err)

	bal, err := ws.GetBalance(ctx, resp.UserID)
	assertNoErr(t, err)
	assertEq(t, int64(DefaultInitialBalance), bal)
}

// Avoid unused-import complaints if the stub tests are compiled out.
var _ = errors.New
