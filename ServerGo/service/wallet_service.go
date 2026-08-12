// Package service — wallet business logic.
//
// WalletService owns every balance mutation in the system. All changes go
// through Credit / Debit / Transfer, which each:
//   1. Run inside a single GORM transaction.
//   2. Use an atomic `balance + ?` SQL expression (no read-modify-write race).
//   3. Append a ledger row (t_lsm_game_wallet_tx) so the double-entry audit
//      trail is written before the transaction commits.
//
// The wallet table and the ledger can never disagree as long as every caller
// goes through this service.
package service

import (
	"context"
	"errors"
	"time"

	"LsmWebGame/errcode"
	"LsmWebGame/logger"
	"LsmWebGame/models"
	"LsmWebGame/util"

	"github.com/go-sql-driver/mysql"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// WalletTxType enumerates the allowed transaction categories.
type WalletTxType string

const (
	TxTypeRegisterBonus   WalletTxType = "register_bonus"
	TxTypeDailyLogin      WalletTxType = "daily_login"
	TxTypeWinReward       WalletTxType = "win_reward"
	TxTypeLoseDeduct      WalletTxType = "lose_deduct"
	TxTypeAnteBuyin       WalletTxType = "ante_buyin"
	TxTypeAnteRefund      WalletTxType = "ante_refund"
	TxTypeTaskReward      WalletTxType = "task_reward"
	TxTypeReferralBonus   WalletTxType = "referral_bonus"
	TxTypeAdminAdjust     WalletTxType = "admin_adjust"
	// 2026-07-14 §135 — 超级管理员每日 grant 批量发放金币流水类型。
	// 配套表 t_lsm_game_admin_grant 用 (provider_id, grant_date) 复合唯一键
	// 保证"每天每模型最多一次";此 TxType 与 TxTypeAdminAdjust 的差别在于
	// 前者具有幂等去重表,后者只能由人工重复触发。
	TxTypeAdminDailyGrant WalletTxType = "admin_daily_grant"
)

// DefaultInitialBalance is the amount seeded into a freshly registered
// wallet (product decision §135 修订: 注册默认 5000;历史 1000 → 2026-07-14
// 切换到 5000,仅影响新注册用户与新生成的 bot,已有钱包不动)。
const DefaultInitialBalance = 5000

// DefaultDailyLoginReward is the UTC+8 daily login bonus (product decision:
// 每日登录 2000，不补领).
const DefaultDailyLoginReward = 2000

// WalletService manages wallet balances and the double-entry ledger.
type WalletService struct {
	db *gorm.DB
}

// NewWalletService builds a WalletService.
func NewWalletService(db *gorm.DB) *WalletService {
	return &WalletService{db: db}
}

// CreateWallet seeds a new wallet for a brand-new user. Registers call this
// inside the same transaction that inserts the user row, then also writes the
// register_bonus ledger entry so the 1000 starting balance has an audit trail.
//
// Idempotent: if a wallet for the user already exists, this is a no-op. This
// makes it safe to call from non-register paths (e.g. daily-reward claim for a
// legacy user whose wallet row was never seeded by the register transaction).
func (s *WalletService) CreateWallet(ctx context.Context, userID string, initialBalance int64) error {
	if initialBalance == 0 {
		initialBalance = DefaultInitialBalance
	}
	now := time.Now()
	// Idempotency check: a missing-wallet code path (daily reward, ante debit
	// for legacy users) must not double-seed a row that already exists.
	var existing models.TLsmGameWallet
	err := s.db.WithContext(ctx).Where("user_id = ?", userID).First(&existing).Error
	if err == nil {
		// Wallet already exists — nothing to do, ledger was written at register time.
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		logger.L().Error("create wallet precheck failed",
			zap.String("user_id", userID),
			zap.Error(err))
		return errcode.Code(errcode.ErrDB)
	}
	wallet := models.TLsmGameWallet{
		ID:        util.NewUUID(),
		UserID:    userID,
		Balance:   initialBalance,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.db.WithContext(ctx).Create(&wallet).Error; err != nil {
		// Race: another goroutine seeded the wallet between First() and Create().
		// On MySQL the unique key on user_id (or the implicit PK) will turn this
		// into a 1062; treat that as success.
		if isMySQLDuplicate(err) {
			return nil
		}
		logger.L().Error("create wallet failed",
			zap.String("user_id", userID),
			zap.Error(err))
		return errcode.Code(errcode.ErrDB)
	}
	// Seed the register_bonus ledger row so the 1000 starting coins are auditable.
	if err := s.writeTx(ctx, s.db, userID, string(TxTypeRegisterBonus),
		initialBalance, initialBalance, "", "", "", "注册奖励"); err != nil {
		logger.L().Error("write register_bonus tx failed",
			zap.String("user_id", userID),
			zap.Error(err))
		return errcode.Code(errcode.ErrWalletTxFailed)
	}
	return nil
}

// EnsureWalletLazy is the in-transaction equivalent of CreateWallet: it creates
// a zero-balance wallet inside an open transaction if one does not already
// exist. The register_bonus ledger row is NOT written here — that audit row
// belongs to the register flow, not to this on-demand backfill. Used by
// Credit/Debit/ClaimDailyReward as a safety net for legacy users whose wallet
// row was never seeded by the original register transaction.
func (s *WalletService) EnsureWalletLazy(tx *gorm.DB, userID string) error {
	var count int64
	if err := tx.Model(&models.TLsmGameWallet{}).
		Where("user_id = ?", userID).Count(&count).Error; err != nil {
		return errcode.Code(errcode.ErrDB)
	}
	if count > 0 {
		return nil
	}
	wallet := models.TLsmGameWallet{
		ID:        util.NewUUID(),
		UserID:    userID,
		Balance:   0,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := tx.Create(&wallet).Error; err != nil {
		if isMySQLDuplicate(err) {
			return nil
		}
		return errcode.Code(errcode.ErrDB)
	}
	logger.L().Info("ensure wallet (lazy backfill)",
		zap.String("user_id", userID),
		zap.Int64("balance", 0))
	return nil
}

// GetWalletBalances batches balance lookups for a set of user IDs into a
// single IN(...) query so the admin LLM model list can render every row's
// coin balance without N+1 round-trips.
//
// Returns a map keyed by user_id → balance. Users without a wallet row are
// simply absent from the map (callers should treat absence as "no wallet
// yet" rather than an error, matching GetBalance's defensive convention).
//
// An empty input slice yields an empty map with no DB round-trip — no callers
// should rely on "all users have balance 0" semantics from this method.
func (s *WalletService) GetWalletBalances(ctx context.Context, userIDs []string) (map[string]int64, error) {
	out := make(map[string]int64, len(userIDs))
	if len(userIDs) == 0 {
		return out, nil
	}
	if s.db == nil {
		return nil, errcode.Code(errcode.ErrInternal)
	}
	// De-duplicate the input so the IN(...) clause stays bounded even if a
	// caller passes the same bot user twice (e.g. multi-pass bot resolution).
	uniq := make([]string, 0, len(userIDs))
	seen := make(map[string]struct{}, len(userIDs))
	for _, id := range userIDs {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		uniq = append(uniq, id)
	}
	if len(uniq) == 0 {
		return out, nil
	}
	type walletRow struct {
		UserID  string `gorm:"column:user_id"`
		Balance int64  `gorm:"column:balance"`
	}
	var rows []walletRow
	if err := s.db.WithContext(ctx).
		Table("t_lsm_game_wallet").
		Select("user_id, balance").
		Where("user_id IN (?)", uniq).
		Find(&rows).Error; err != nil {
		logger.L().Error("batch get wallet balances failed",
			zap.Int("user_count", len(uniq)), zap.Error(err))
		return nil, errcode.Code(errcode.ErrDB)
	}
	for _, r := range rows {
		out[r.UserID] = r.Balance
	}
	return out, nil
}

// GetBalance returns the current coin balance. Returns 0 with a nil error when
// the wallet row is missing (callers are expected to create one with
// CreateWallet first — a missing wallet is treated as zero rather than an error
// to avoid cascading failures on legacy rows).
func (s *WalletService) GetBalance(ctx context.Context, userID string) (int64, error) {
	var wallet models.TLsmGameWallet
	err := s.db.WithContext(ctx).Where("user_id = ?", userID).First(&wallet).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, nil
		}
		logger.L().Error("get wallet failed",
			zap.String("user_id", userID),
			zap.Error(err))
		return 0, errcode.Code(errcode.ErrDB)
	}
	return wallet.Balance, nil
}

// GetBalanceWithStats returns the current balance alongside the lifetime
// earned / spent totals so the frontend's profile / header can render
// "累计获得/消耗" in a single round-trip. Missing wallet → returns zero
// values with no error (same convention as GetBalance).
func (s *WalletService) GetBalanceWithStats(ctx context.Context, userID string) (balance, totalEarned, totalSpent int64, err error) {
	var wallet models.TLsmGameWallet
	dberr := s.db.WithContext(ctx).Where("user_id = ?", userID).First(&wallet).Error
	if dberr != nil {
		if errors.Is(dberr, gorm.ErrRecordNotFound) {
			return 0, 0, 0, nil
		}
		logger.L().Error("get wallet (with stats) failed",
			zap.String("user_id", userID),
			zap.Error(dberr))
		return 0, 0, 0, errcode.Code(errcode.ErrDB)
	}
	return wallet.Balance, wallet.TotalEarned, wallet.TotalSpent, nil
}

// Credit adds amount (>0) to the user's wallet atomically and writes a ledger
// row that captures the post-change balance.
func (s *WalletService) Credit(ctx context.Context, userID, txType, refType, refID, gameKind, remark string, amount int64) error {
	if amount <= 0 {
		return errcode.Code(errcode.ErrValidationFailed)
	}
	var newBalance int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Defensive backfill: if the wallet row is missing (legacy user), seed
		// a zero-balance row so the UPDATE below can land. Without this, the
		// increment is a silent no-op and the ledger row would record a
		// misleading balance_after of 0.
		if err := s.EnsureWalletLazy(tx, userID); err != nil {
			return err
		}
		// Atomic increment + capture the resulting balance via RETURNING
		// (MySQL/MariaDB 10.5+ supports it through GORM's `RETURNING` clause
		// via `.Scan` on the same statement).

		// Step 1: atomic increment
		if err := tx.Model(&models.TLsmGameWallet{}).
			Where("user_id = ?", userID).
			Updates(map[string]any{
				"balance":      gorm.Expr("balance + ?", amount),
				"total_earned": gorm.Expr("total_earned + ?", amount),
				"updated_at":   time.Now(),
			}).Error; err != nil {
			return errcode.Code(errcode.ErrDB)
		}
		// Step 2: read the new balance
		var w models.TLsmGameWallet
		if err := tx.Where("user_id = ?", userID).First(&w).Error; err != nil {
			return errcode.Code(errcode.ErrDB)
		}
		newBalance = w.Balance
		// Step 3: append ledger row
		return s.writeTx(tx.Statement.Context, tx, userID, txType, amount,
			newBalance, refType, refID, gameKind, remark)
	})
	if err != nil {
		ce := errcode.AsError(err)
		logger.L().Error("credit failed",
			zap.String("user_id", userID),
			zap.Int64("amount", amount),
			zap.String("tx_type", txType),
			zap.Int("code", ce.Code),
			zap.String("msg", ce.Message))
		return ce
	}
	logger.L().Info("credit ok",
		zap.String("user_id", userID),
		zap.Int64("amount", amount),
		zap.Int64("balance_after", newBalance),
		zap.String("tx_type", txType))
	return nil
}

// Debit subtracts amount (>0) from the wallet. Fails with
// ErrWalletInsufficientBalance when balance would go negative.
func (s *WalletService) Debit(ctx context.Context, userID, txType, refType, refID, gameKind, remark string, amount int64) error {
	if amount <= 0 {
		return errcode.Code(errcode.ErrValidationFailed)
	}
	var newBalance int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Step 1: conditional decrement — WHERE guard prevents overdraft
		res := tx.Model(&models.TLsmGameWallet{}).
			Where("user_id = ? AND balance >= ?", userID, amount).
			Updates(map[string]any{
				"balance":      gorm.Expr("balance - ?", amount),
				"total_spent":  gorm.Expr("total_spent + ?", amount),
				"updated_at":   time.Now(),
			})
		if res.Error != nil {
			return errcode.Code(errcode.ErrDB)
		}
		if res.RowsAffected == 0 {
			// Either the wallet doesn't exist or balance < amount. Distinguish.
			var count int64
			if err := tx.Model(&models.TLsmGameWallet{}).
				Where("user_id = ?", userID).Count(&count).Error; err != nil {
				return errcode.Code(errcode.ErrDB)
			}
			if count == 0 {
				// Auto-create the wallet with zero so future ops succeed.
				// This is the "register race where wallet wasn't seeded" path.
				wallet := models.TLsmGameWallet{
					ID:        util.NewUUID(),
					UserID:    userID,
					Balance:   0,
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
				}
				if err := tx.Create(&wallet).Error; err != nil {
					return errcode.Code(errcode.ErrDB)
				}
			}
			return errcode.Code(errcode.ErrWalletInsufficientBalance)
		}
		// Step 2: read the new balance
		var w models.TLsmGameWallet
		if err := tx.Where("user_id = ?", userID).First(&w).Error; err != nil {
			return errcode.Code(errcode.ErrDB)
		}
		newBalance = w.Balance
		// Step 3: append ledger row (negative amount)
		return s.writeTx(tx.Statement.Context, tx, userID, txType, -amount,
			newBalance, refType, refID, gameKind, remark)
	})
	if err != nil {
		ce := errcode.AsError(err)
		logger.L().Error("debit failed",
			zap.String("user_id", userID),
			zap.Int64("amount", amount),
			zap.String("tx_type", txType),
			zap.Int("code", ce.Code),
			zap.String("msg", ce.Message))
		return ce
	}
	logger.L().Info("debit ok",
		zap.String("user_id", userID),
		zap.Int64("amount", amount),
		zap.Int64("balance_after", newBalance),
		zap.String("tx_type", txType))
	return nil
}

// Transfer moves amount coins from one user to another atomically. Used by
// room settlement. Either both wallets update and two ledger rows write, or
// none do.
func (s *WalletService) Transfer(ctx context.Context, fromUserID, toUserID, refType, refID, gameKind, remark string, amount int64) error {
	if amount <= 0 {
		return errcode.Code(errcode.ErrValidationFailed)
	}
	if fromUserID == toUserID {
		return errcode.Code(errcode.ErrValidationFailed)
	}
	var fromAfter, toAfter int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Lock source wallet row and verify sufficient balance.
		var fromWallet models.TLsmGameWallet
		if err := tx.Clauses().
			Where("user_id = ?", fromUserID).
			First(&fromWallet).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errcode.Code(errcode.ErrWalletInsufficientBalance)
			}
			return errcode.Code(errcode.ErrDB)
		}
		if fromWallet.Balance < amount {
			return errcode.Code(errcode.ErrWalletInsufficientBalance)
		}
		// Lock destination wallet row (must exist).
		var toWallet models.TLsmGameWallet
		if err := tx.Clauses().
			Where("user_id = ?", toUserID).
			First(&toWallet).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				// Auto-create receiver wallet if missing.
				toWallet = models.TLsmGameWallet{
					ID:        util.NewUUID(),
					UserID:    toUserID,
					Balance:   0,
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
				}
				if err := tx.Create(&toWallet).Error; err != nil {
					return errcode.Code(errcode.ErrDB)
				}
			} else {
				return errcode.Code(errcode.ErrDB)
			}
		}
		// Debit the source.
		if err := tx.Model(&models.TLsmGameWallet{}).
			Where("user_id = ?", fromUserID).
			Updates(map[string]any{
				"balance":     gorm.Expr("balance - ?", amount),
				"total_spent": gorm.Expr("total_spent + ?", amount),
				"updated_at":  time.Now(),
			}).Error; err != nil {
			return errcode.Code(errcode.ErrDB)
		}
		// Credit the destination.
		if err := tx.Model(&models.TLsmGameWallet{}).
			Where("user_id = ?", toUserID).
			Updates(map[string]any{
				"balance":      gorm.Expr("balance + ?", amount),
				"total_earned": gorm.Expr("total_earned + ?", amount),
				"updated_at":   time.Now(),
			}).Error; err != nil {
			return errcode.Code(errcode.ErrDB)
		}
		// Read both new balances.
		var fw, tw models.TLsmGameWallet
		if err := tx.Where("user_id = ?", fromUserID).First(&fw).Error; err != nil {
			return errcode.Code(errcode.ErrDB)
		}
		if err := tx.Where("user_id = ?", toUserID).First(&tw).Error; err != nil {
			return errcode.Code(errcode.ErrDB)
		}
		fromAfter = fw.Balance
		toAfter = tw.Balance
		// Two ledger rows: negative for sender, positive for receiver.
		if err := s.writeTx(tx.Statement.Context, tx, fromUserID,
			string(TxTypeLoseDeduct), -amount, fromAfter,
			refType, refID, gameKind, remark); err != nil {
			return errcode.Code(errcode.ErrWalletTxFailed)
		}
		return s.writeTx(tx.Statement.Context, tx, toUserID,
			string(TxTypeWinReward), amount, toAfter,
			refType, refID, gameKind, remark)
	})
	if err != nil {
		ce := errcode.AsError(err)
		logger.L().Error("transfer failed",
			zap.String("from", fromUserID),
			zap.String("to", toUserID),
			zap.Int64("amount", amount),
			zap.Int("code", ce.Code),
			zap.String("msg", ce.Message))
		return ce
	}
	logger.L().Info("transfer ok",
		zap.String("from", fromUserID),
		zap.String("to", toUserID),
		zap.Int64("amount", amount),
		zap.Int64("from_after", fromAfter),
		zap.Int64("to_after", toAfter))
	return nil
}

// HasClaimedDailyReward returns true when the user has already received the
// UTC+8 daily login bonus for the given time (the date is converted to a
// YYYY-MM-DD string in the server's local timezone, which the operator is
// expected to run in UTC+8).
func (s *WalletService) HasClaimedDailyReward(ctx context.Context, userID string, date time.Time) (bool, error) {
	dateStr := date.Format("2006-01-02")
	var count int64
	err := s.db.WithContext(ctx).Model(&models.TLsmGameDailyReward{}).
		Where("user_id = ? AND reward_date = ?", userID, dateStr).
		Count(&count).Error
	if err != nil {
		logger.L().Error("check daily reward failed",
			zap.String("user_id", userID),
			zap.Error(err))
		return false, errcode.Code(errcode.ErrDB)
	}
	return count > 0, nil
}

// ClaimDailyReward credits DefaultDailyLoginReward coins once per UTC+8 day.
// Idempotent: the second call within the same day returns ErrWalletDailyRewardClaimed
// without crediting again.
func (s *WalletService) ClaimDailyReward(ctx context.Context, userID string, date time.Time) (int64, error) {
	dateStr := date.Format("2006-01-02")
	amount := int64(DefaultDailyLoginReward)

	// Fast path: if already claimed today, return immediately without
	// touching GORM's Create — avoids noisy MySQL 1062 duplicate-key logs
	// that previously produced 12+ error lines per test run.
	claimed, err := s.HasClaimedDailyReward(ctx, userID, date)
	if err != nil {
		return 0, err
	}
	if claimed {
		logger.L().Info("daily reward already claimed",
			zap.String("user_id", userID),
			zap.String("date", dateStr))
		return 0, errcode.Code(errcode.ErrWalletDailyRewardClaimed)
	}

	var creditedAmount int64
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Defensive: legacy users may have no wallet row (their account predates
		// the register-time CreateWallet patch). Seed a zero-balance row before
		// the credit; the daily reward will then bring it to the expected 2000.
		// The daily_reward insert below would otherwise succeed and silently
		// mark "claimed" while the UPDATE on the missing wallet affects 0 rows.
		if err := s.EnsureWalletLazy(tx, userID); err != nil {
			return err
		}
		// Optimistic insert to claim the date. If the unique key already exists,
		// we've already claimed — abort.
		rec := models.TLsmGameDailyReward{
			ID:         util.NewUUID(),
			UserID:     userID,
			RewardDate: dateStr,
			Amount:     amount,
			CreatedAt:  time.Now(),
		}
		if err := tx.Create(&rec).Error; err != nil {
			// Unique constraint violation: either the GORM sentinel (gorm v1.25+)
			// or the legacy MySQL 1062 errno means someone already claimed today.
			if errors.Is(err, gorm.ErrDuplicatedKey) || isMySQLDuplicate(err) {
				return errcode.Code(errcode.ErrWalletDailyRewardClaimed)
			}
			return errcode.Code(errcode.ErrDB)
		}
		// Credit the wallet atomically.
		if err := tx.Model(&models.TLsmGameWallet{}).
			Where("user_id = ?", userID).
			Updates(map[string]any{
				"balance":      gorm.Expr("balance + ?", amount),
				"total_earned": gorm.Expr("total_earned + ?", amount),
				"updated_at":   time.Now(),
			}).Error; err != nil {
			return errcode.Code(errcode.ErrDB)
		}
		// Read the new balance for the ledger row.
		var w models.TLsmGameWallet
		if err := tx.Where("user_id = ?", userID).First(&w).Error; err != nil {
			return errcode.Code(errcode.ErrDB)
		}
		creditedAmount = w.Balance
		return s.writeTx(tx.Statement.Context, tx, userID,
			string(TxTypeDailyLogin), amount, creditedAmount,
			"", "", "", "每日登录奖励")
	})
	if err != nil {
		ce := errcode.AsError(err)
		if ce.Code == errcode.ErrWalletDailyRewardClaimed {
			logger.L().Info("daily reward already claimed",
				zap.String("user_id", userID),
				zap.String("date", dateStr))
			return 0, ce
		}
		logger.L().Error("claim daily reward failed",
			zap.String("user_id", userID),
			zap.Int("code", ce.Code),
			zap.String("msg", ce.Message))
		return 0, ce
	}
	logger.L().Info("daily reward claimed",
		zap.String("user_id", userID),
		zap.Int64("amount", amount),
		zap.Int64("balance_after", creditedAmount))
	return creditedAmount, nil
}

// AlreadySettled reports whether a ledger row already exists for the given
// (user_id, ref_type, ref_id, tx_type) tuple. It powers **application-level
// settlement idempotency** (DEFECT 2, option c).
//
// Why NOT a DB UNIQUE(user_id, ref_type, ref_id, tx_type) index (doc §10.1):
// existing tx types legitimately repeat with EMPTY or REPEATING ref fields —
//   - daily_login  : writeTx(..., "", "", "", ...) → same user, empty ref, one
//                    row EVERY day → (user, "", "", "daily_login") collides.
//   - admin_adjust : refID = admin's callerID (NOT a per-tx UUID) → collides on
//                    a second manual adjust of the same bot by the same admin.
//   - admin_daily_grant : refID = granterUID → collides daily.
// A global unique index would reject these legitimate rows, and a "benign
// no-op on 1062" inside writeTx would silently drop the ledger row while the
// balance still moved — corrupting the double-entry invariant
// (balance + Σtx.amount == balance_after). So settlement dedup is enforced in
// the application layer, scoped ONLY to settlement tx types, leaving all other
// tx types untouched. This is defense-in-depth behind the primary L1 guard
// (room-level r.gameOverNotified in-memory flag); it additionally protects the
// cross-process / process-restart case where L1 is lost.
//
// Settlement callers pass a per-game unique refID (the werewolf room UUID or
// the model game_log ID), so (user_id, ref_type, ref_id, tx_type) is unique per
// player per game and this check is exact.
func (s *WalletService) AlreadySettled(ctx context.Context, userID, refType, refID, txType string) (bool, error) {
	if s == nil || s.db == nil {
		return false, nil
	}
	var count int64
	if err := s.db.WithContext(ctx).Model(&models.TLsmGameWalletTx{}).
		Where("user_id = ? AND ref_type = ? AND ref_id = ? AND tx_type = ?",
			userID, refType, refID, txType).
		Count(&count).Error; err != nil {
		logger.L().Error("settlement dedup precheck failed",
			zap.String("user_id", userID),
			zap.String("ref_type", refType),
			zap.String("ref_id", refID),
			zap.String("tx_type", txType),
			zap.Error(err))
		return false, errcode.Code(errcode.ErrDB)
	}
	return count > 0, nil
}

// ListTransactions returns ledger rows for a user in reverse-chronological
// order, plus the total count so a paginator can work.
func (s *WalletService) ListTransactions(ctx context.Context, userID string, limit, offset int) ([]models.TLsmGameWalletTx, int64, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}
	var total int64
	if err := s.db.WithContext(ctx).Model(&models.TLsmGameWalletTx{}).
		Where("user_id = ?", userID).Count(&total).Error; err != nil {
		return nil, 0, errcode.Code(errcode.ErrDB)
	}
	var rows []models.TLsmGameWalletTx
	if err := s.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Limit(limit).Offset(offset).
		Find(&rows).Error; err != nil {
		return nil, 0, errcode.Code(errcode.ErrDB)
	}
	return rows, total, nil
}

// ─────────────────── internal ───────────────────

// writeTx appends a ledger row directly to the supplied tx/db handle. It
// should only be called from inside an active transaction.
func (s *WalletService) writeTx(ctx context.Context, tx *gorm.DB,
	userID, txType string, amount, balanceAfter int64,
	refType, refID, gameKind, remark string) error {
	rec := models.TLsmGameWalletTx{
		ID:           util.NewUUID(),
		UserID:       userID,
		TxType:       txType,
		Amount:       amount,
		BalanceAfter: balanceAfter,
		RefType:      refType,
		RefID:        refID,
		GameKind:     gameKind,
		Remark:       remark,
		CreatedAt:    time.Now(),
	}
	if err := tx.WithContext(ctx).Create(&rec).Error; err != nil {
		logger.L().Error("write ledger tx failed",
			zap.String("user_id", userID),
			zap.String("tx_type", txType),
			zap.Error(err))
		return err
	}
	return nil
}

// isMySQLDuplicate returns true when the error wraps a MySQL/MariaDB
// Error 1062 (duplicate key). Used as a fallback when GORM doesn't surface
// gorm.ErrDuplicatedKey on its own (older drivers, custom Tx middleware).
func isMySQLDuplicate(err error) bool {
	if err == nil {
		return false
	}
	var merr *mysql.MySQLError
	if errors.As(err, &merr) && merr.Number == 1062 {
		return true
	}
	return false
}
