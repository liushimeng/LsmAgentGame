// Package service — bot user provisioning.
//
// Per the "模型管理 + 模型玩家持久化 + 模型金币" plan (kind-skipping-moth §2.2),
// every t_lsm_game_llm_provider row needs a backing t_lsm_game_user row so
// the model can participate in games as a persistent player (with a wallet
// and a t_lsm_game_player seat). BotUserService owns that provisioning.
//
// Conventions:
//
//   - account:  "bot_" + snake_case(model), capped at 32 chars to fit the
//               t_lsm_game_user.account VARCHAR(32) column. e.g. "DouBao-model"
//               → "bot_doubao_model".
//   - nickname: equal to p.AgentName (e.g. "豆包 2.0").
//   - password: 64 random bytes hex-encoded and bcrypt-hashed. Never used for
//               human login; stored only because the schema requires it.
//   - IsBot:    true.
//   - BotProviderID: set to &p.ID on first create, refreshed on every call so
//                   renames / re-imports of a provider keep the linkage.
//
// Idempotency:
//
//   - The natural unique index on t_lsm_game_user.account guarantees that
//     re-running for the same model returns the existing row without error.
//   - When the row already exists we refresh BotProviderID to the latest
//     provider ID (which is stable across calls but may have changed after a
//     delete-and-recreate cycle, e.g. when an admin re-imports a model).
package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"unicode"

	"LsmWebGame/errcode"
	"LsmWebGame/logger"
	"LsmWebGame/models"
	"LsmWebGame/util"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// botAccountMaxLen is the VARCHAR(32) cap on t_lsm_game_user.account. The
// "bot_" prefix (4 chars) plus the snake_case model must fit within this.
const botAccountMaxLen = 32

// botPasswordRandomBytes is the size of the random password we hash for a
// bot. 32 bytes → 64 hex chars, comfortably under bcrypt's 72-byte plaintext
// cap (which would otherwise error with "password length exceeds 72 bytes").
// 256 bits of entropy is more than enough for a never-logged-in bot account
// — the password only exists to satisfy the schema's NOT NULL constraint.
const botPasswordRandomBytes = 32

// BotUserService manages bot player users and their wallets.
type BotUserService struct {
	gormDB        *gorm.DB
	walletService *WalletService
}

// NewBotUserService builds a BotUserService.
func NewBotUserService(db *gorm.DB, ws *WalletService) *BotUserService {
	return &BotUserService{gormDB: db, walletService: ws}
}

// SetWalletService wires the wallet service after construction. Mirrors the
// pattern used by AuthService so main.go can wire dependencies in any order.
func (s *BotUserService) SetWalletService(ws *WalletService) {
	s.walletService = ws
}

// BuildBotAccountFromModel converts "DouBao-model" to "bot_doubao_model"
// using the rules documented on the type. Exposed for unit tests and any
// future "preview account name" UI affordance.
func BuildBotAccountFromModel(model string) string {
	// Lowercase + map non-alphanumeric to '_' + collapse runs of '_'.
	runes := make([]rune, 0, len(model)+4)
	prevUnderscore := false
	for _, r := range model {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			runes = append(runes, unicode.ToLower(r))
			prevUnderscore = false
		default:
			if !prevUnderscore && len(runes) > 0 {
				runes = append(runes, '_')
				prevUnderscore = true
			}
		}
	}
	// Trim a trailing '_' so "Foo-model" → "foo_model", not "foo_model_".
	for len(runes) > 0 && runes[len(runes)-1] == '_' {
		runes = runes[:len(runes)-1]
	}
	snake := string(runes)
	prefix := "bot_"
	combined := prefix + snake
	// Cap at botAccountMaxLen total characters.
	if len(combined) > botAccountMaxLen {
		combined = combined[:botAccountMaxLen]
		// Avoid ending on a truncated '_'.
		combined = strings.TrimRight(combined, "_")
	}
	return combined
}

// randomBotPassword returns a hex string of botPasswordRandomBytes random
// bytes (128 hex chars). Used as the bcrypt plaintext for a bot user.
func randomBotPassword() (string, error) {
	b := make([]byte, botPasswordRandomBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// EnsureBotUserForProvider creates (or fetches) the bot user backing p and
// seeds its wallet. Returns the bot user row on success.
//
// Behavior:
//
//   - account: BuildBotAccountFromModel(p.Model). Stable across calls.
//   - nickname: p.AgentName; falls back to account if empty.
//   - IsBot = true; BotProviderID = &p.ID (refreshed on every call).
//   - If the row already exists, only BotProviderID is rewritten.
//   - Wallet seeded via WalletService.CreateWallet (1000 coins by default).
//
// Errors:
//
//   - Returns ErrDB / ErrInternal on database or bcrypt failures.
//   - Returns the underlying GORM error if the wallet seed itself fails.
func (s *BotUserService) EnsureBotUserForProvider(
	ctx context.Context, p *models.TLsmGameLlmProvider,
) (*models.TLsmGameUser, error) {
	if p == nil {
		return nil, errcode.Code(errcode.ErrValidationFailed)
	}
	if s.gormDB == nil {
		return nil, errcode.Code(errcode.ErrInternal)
	}

	account := BuildBotAccountFromModel(p.Model)
	nickname := strings.TrimSpace(p.AgentName)
	if nickname == "" {
		nickname = account
	}

	// Fast path: existing bot user. Refresh BotProviderID + nickname (cheap
	// idempotent update) and return the row.
	var existing models.TLsmGameUser
	err := s.gormDB.WithContext(ctx).
		Where("account = ?", account).
		First(&existing).Error
	if err == nil {
		updates := map[string]any{}
		if existing.BotProviderID != p.ID {
			updates["bot_provider_id"] = p.ID
		}
		if existing.Nickname != nickname {
			updates["nickname"] = nickname
		}
		if !existing.IsBot {
			updates["is_bot"] = true
		}
		if len(updates) > 0 {
			if err := s.gormDB.WithContext(ctx).
				Model(&models.TLsmGameUser{}).
				Where("id = ?", existing.ID).
				Updates(updates).Error; err != nil {
				logger.L().Error("bot user update failed",
					zap.String("account", account),
					zap.Error(err))
				return nil, errcode.Code(errcode.ErrDB)
			}
		}
		// Ensure wallet row exists for legacy bot rows predating the wallet
		// guarantee (defensive: idempotent and cheap).
		if s.walletService != nil {
			if err := s.walletService.CreateWallet(ctx, existing.ID, DefaultInitialBalance); err != nil {
				logger.L().Error("bot wallet backfill failed",
					zap.String("bot_user_id", existing.ID),
					zap.Error(err))
				return nil, errcode.Code(errcode.ErrDB)
			}
		}
		return &existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		logger.L().Error("bot user lookup failed",
			zap.String("account", account),
			zap.Error(err))
		return nil, errcode.Code(errcode.ErrDB)
	}

	// Slow path: create bot user + seed wallet in one transaction so the
	// user row never exists without its wallet row (matching human register
	// guarantees from AuthService.Register).
	plaintext, err := randomBotPassword()
	if err != nil {
		return nil, errcode.Code(errcode.ErrInternal)
	}
	hash, err := util.HashPassword(plaintext)
	if err != nil {
		logger.L().Error("bot user hash failed", zap.Error(err))
		return nil, errcode.Code(errcode.ErrInternal)
	}

	var created models.TLsmGameUser
	err = s.gormDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Re-check inside the transaction in case another goroutine won the
		// race between our first read and the INSERT.
		var race models.TLsmGameUser
		rerr := tx.WithContext(ctx).
			Where("account = ?", account).
			First(&race).Error
		if rerr == nil {
			created = race
			return nil
		}
		if !errors.Is(rerr, gorm.ErrRecordNotFound) {
			return rerr
		}

		created = models.TLsmGameUser{
			ID:                  util.NewUUID(),
			Account:             account,
			Nickname:            nickname,
			PasswordHash:        hash,
			MyInviteCode:        "BOT" + util.NewUUID()[:29], // 32 chars total
			Language:            "zh-CN",
			IsBot:               true,
			BotProviderID:       p.ID,
		}
		if err := tx.Create(&created).Error; err != nil {
			return err
		}

		// Seed wallet in the SAME transaction.
		if s.walletService != nil {
			wallet := models.TLsmGameWallet{
				ID:          util.NewUUID(),
				UserID:      created.ID,
				Balance:     DefaultInitialBalance,
				TotalEarned: DefaultInitialBalance, // 2026-07-14 §135 fix: 用直接 INSERT 而非 Credit() 路径播种时,必须自带 total_earned = balance,否则后续 ledger 与 total_earned 不一致。
			}
			if err := tx.Create(&wallet).Error; err != nil {
				return err
			}
			// register_bonus ledger row to match AuthService.Register's audit
			// trail (so the 1000 starting coins are traceable).
			if err := s.walletService.writeTx(tx.Statement.Context, tx, created.ID,
				string(TxTypeRegisterBonus), DefaultInitialBalance,
				DefaultInitialBalance, "llm_provider", p.ID, "werewolf",
				"模型玩家初始金币"); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		ce := errcode.AsError(err)
		logger.L().Error("ensure bot user failed",
			zap.String("account", account),
			zap.Int("code", ce.Code),
			zap.String("msg", ce.Message))
		return nil, ce
	}
	logger.L().Info("ensure bot user ok",
		zap.String("bot_user_id", created.ID),
		zap.String("account", created.Account),
		zap.String("provider_id", p.ID))
	return &created, nil
}

// GetBotUserForProvider returns the backing bot user for a provider WITHOUT
// creating one if missing. Looks up by BotProviderID (preferred) and falls
// back to the deterministic account naming rule used by
// EnsureBotUserForProvider so legacy / pre-wallet rows can still be resolved.
//
// §135 修复 — 模型详情页需要按 provider_id 找 bot_user_id(钱包/对局日志的
// 路由参数都是 bot_user_id),而此前要么传错(provider.id != bot_user_id),
// 要么只能先调 Ensure 触发写入。改成纯读不写入的查找函数:
//
//   - 优先 BotProviderID 索引命中(主路径,fast path)
//   - 回退到 account = BuildBotAccountFromModel(p.Model),兼容老数据
//
// 返回 (nil, nil) 表示「该 provider 还没有 bot user」,调用方按"未生成"展示。
// 返回 (nil, err) 表示真错误(查 DB 失败 / 多行重复)。
func (s *BotUserService) GetBotUserForProvider(
	ctx context.Context, providerID string,
) (*models.TLsmGameUser, error) {
	if providerID == "" {
		return nil, errcode.Code(errcode.ErrValidationFailed)
	}
	if s.gormDB == nil {
		return nil, errcode.Code(errcode.ErrInternal)
	}

	// 1) fast path — BotProviderID 索引。EnsureBotUserForProvider 在创建
	//    或刷新时都会写这个字段,所以最新数据一定能命中。
	var u models.TLsmGameUser
	err := s.gormDB.WithContext(ctx).
		Where("bot_provider_id = ?", providerID).
		First(&u).Error
	if err == nil {
		return &u, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		logger.L().Error("bot user lookup by bot_provider_id failed",
			zap.String("provider_id", providerID), zap.Error(err))
		return nil, errcode.Code(errcode.ErrDB)
	}

	// 2) legacy fallback — 早期代码或手工 INSERT 的行可能 BotProviderID
	//    为空,按 account 命名规则反查。
	var provider models.TLsmGameLlmProvider
	if err := s.gormDB.WithContext(ctx).
		Where("id = ?", providerID).
		First(&provider).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// provider 都不存在 → 自然也没有 bot user
			return nil, nil
		}
		logger.L().Error("bot user lookup: provider fetch failed",
			zap.String("provider_id", providerID), zap.Error(err))
		return nil, errcode.Code(errcode.ErrDB)
	}
	account := BuildBotAccountFromModel(provider.Model)
	err = s.gormDB.WithContext(ctx).
		Where("account = ? AND is_bot = ?", account, true).
		First(&u).Error
	if err == nil {
		return &u, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// 该 provider 尚未生成 bot user(例如创建时 EnsureBotUserForProvider
		// 失败被吞掉的情况) — 返回 (nil, nil) 让调用方按"未生成"处理。
		return nil, nil
	}
	logger.L().Error("bot user lookup by account failed",
		zap.String("provider_id", providerID),
		zap.String("account", account), zap.Error(err))
	return nil, errcode.Code(errcode.ErrDB)
}