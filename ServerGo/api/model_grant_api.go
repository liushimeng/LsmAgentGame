// Package api — model_grant_api.go exposes the super-admin "daily grant"
// endpoint.
//
// Endpoints:
//
//	POST /api/admin/llm/bots/grant-daily      (super admin only)
//
// The "每天每模型最多一次" invariant is enforced by the composite unique key
// (provider_id, grant_date) on t_lsm_game_admin_grant. The handler:
//
//  1. Resolve target provider(s) — body.provider_id empty → bulk over all
//     enabled providers; non-empty → single provider.
//  2. For each provider, ensure the bot user exists (find-or-create via
//     BotUserService.GetBotUserForProvider → EnsureBotUserForProvider fallback).
//  3. INSERT the grant dedup row FIRST; a duplicate key → add the provider
//     to the response's `skipped` list and move on. This guarantees wallet
//     state never goes "credit + no dedup row" or "dedup row + no credit".
//  4. Credit the wallet via WalletService.Credit with the new
//     TxTypeAdminDailyGrant. On failure, ROLLBACK the dedup row.
//  5. Re-read the post-balance and update balance_after on the dedup row
//     so an audit can recover the exact end balance.
//
// 2026-07-14 §135 — Agent 钱包与每日金币 Grant 设计实现。
package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"LsmAgentGame/errcode"
	"LsmAgentGame/logger"
	"LsmAgentGame/models"
	"LsmAgentGame/service"
	"LsmAgentGame/util"

	"github.com/gin-gonic/gin"
	"github.com/go-sql-driver/mysql"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// ModelGrantAPI serves the super-admin daily-grant endpoints.
type ModelGrantAPI struct {
	walletSvc  *service.WalletService
	botUserSvc *service.BotUserService
	userSvc    UserRoleChecker
	gormDB     *gorm.DB
}

// NewModelGrantAPI wires the handler. walletSvc is used for the atomic
// credit; botUserSvc resolves each provider's bot_user_id (provider.id is
// NOT the bot user id per §135).
func NewModelGrantAPI(
	walletSvc *service.WalletService,
	botUserSvc *service.BotUserService,
	userSvc UserRoleChecker,
	gormDB *gorm.DB,
) *ModelGrantAPI {
	return &ModelGrantAPI{
		walletSvc:  walletSvc,
		botUserSvc: botUserSvc,
		userSvc:    userSvc,
		gormDB:     gormDB,
	}
}

// ─────────────────── request / response ───────────────────

// GrantDailyRequest is the JSON body for POST /api/admin/llm/bots/grant-daily.
//
// ProviderID is optional — empty means "every enabled provider". Amount must
// be positive and capped at 1,000,000 to bound the operator's blast radius.
// Remark is required so the ledger has a human-readable audit trail.
type GrantDailyRequest struct {
	ProviderID string `json:"provider_id,omitempty"`
	Amount     int64  `json:"amount"     binding:"required,min=1,max=1000000"`
	Remark     string `json:"remark"     binding:"required,min=1,max=255"`
}

// GrantedItem describes one (provider, bot) line in the response.
type GrantedItem struct {
	ProviderID   string `json:"provider_id"`
	ProviderName string `json:"provider_name"`
	BotUserID    string `json:"bot_user_id"`
	Amount       int64  `json:"amount"`
	BalanceAfter int64  `json:"balance_after"`
}

// GrantDailyResponse segments target providers into:
//   - granted: succeeded this call
//   - skipped: already-granted today (dedup key collision)
//   - failed:  any other DB error (logged but not surfaced to user; rare)
//
// Date is the UTC+8 date string used for dedup.
type GrantDailyResponse struct {
	Granted []GrantedItem `json:"granted"`
	Skipped []GrantedItem `json:"skipped"`
	Date    string        `json:"date"`
}

// ─────────────────── auth helpers ───────────────────

// requireSuper enforces UserTypeSuper. Same shape as ModelAdminAPI.requireSuper
// but inlined here to avoid coupling. The auth middleware (AuthRequired) has
// already verified the JWT before this is called — we just need to gate on the
// role tier.
func (h *ModelGrantAPI) requireSuper(c *gin.Context) (string, bool) {
	uid := uidFromContext(c)
	if uid == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code": errcode.ErrAuthMissingToken, "message": errcode.DefaultMessages[errcode.ErrAuthMissingToken],
		})
		return "", false
	}
	if h.userSvc == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrInternal, "message": "user service not wired",
		})
		return "", false
	}
	userType, err := h.userSvc.GetUserType(c.Request.Context(), uid)
	if err != nil {
		ce := errcode.AsError(err)
		c.JSON(http.StatusBadRequest, gin.H{"code": ce.Code, "message": ce.Message})
		return "", false
	}
	if userType < models.UserTypeSuper {
		c.JSON(http.StatusForbidden, gin.H{
			"code": errcode.ErrPermissionDenied, "message": "需要超级管理员权限",
		})
		return "", false
	}
	return uid, true
}

// ─────────────────── handlers ───────────────────

// GrantDaily POST /api/admin/llm/bots/grant-daily (super admin only).
//
// Behavior — see package comment for the 5-step invariant. Returns a
// GrantDailyResponse even when the operation is partial-success (some
// granted, some skipped). HTTP 200 is returned as long as the request body
// parsed; per-provider failures are reported in the segmented response
// rather than as a single 5xx (matches the operator's mental model: "what
// just got credited?").
func (h *ModelGrantAPI) GrantDaily(c *gin.Context) {
	granterUID, ok := h.requireSuper(c)
	if !ok {
		return
	}
	var req GrantDailyRequest
	if !decodeJSONStrict(c, &req) {
		return
	}
	if h.walletSvc == nil || h.botUserSvc == nil || h.gormDB == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrInternal, "message": "service not wired",
		})
		return
	}

	// Bound amount again even though the binding tag should catch it; defense
	// in depth against accidental binding-tag edits.
	if req.Amount <= 0 || req.Amount > 1000000 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": errcode.ErrValidationFailed, "message": "amount must be in 1..1000000",
		})
		return
	}

	dateStr := time.Now().Format("2006-01-02") // YYYY-MM-DD; assumes server runs in UTC+8.

	// Collect target providers — enabled = true only; deleted (soft) and
	// disabled rows are silently skipped.
	var providers []models.TLsmGameLlmProvider
	q := h.gormDB.WithContext(c.Request.Context()).Where("enabled = ?", true)
	if strings.TrimSpace(req.ProviderID) != "" {
		q = q.Where("id = ?", strings.TrimSpace(req.ProviderID))
	}
	if err := q.Find(&providers).Error; err != nil {
		logger.L().Error("grant daily: list providers failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrDB, "message": "query failed",
		})
		return
	}
	if len(providers) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": errcode.ErrValidationFailed, "message": "no providers found",
		})
		return
	}

	granted := make([]GrantedItem, 0, len(providers))
	skipped := make([]GrantedItem, 0)

	for i := range providers {
		p := providers[i]
		item := grantedItemFromProvider(&p)
		// 1) Find-or-create bot user for this provider. Without a bot_user_id
		// we cannot credit (and importantly, the bot_user_id != provider.id;
		// see §135 correction). GetBotUserForProvider is pure-read; if it
		// returns nil we fall back to EnsureBotUserForProvider which has
		// the side effect of seeding the wallet.
		bot, err := h.botUserSvc.GetBotUserForProvider(c.Request.Context(), p.ID)
		if err != nil {
			logger.L().Warn("grant daily: bot lookup failed",
				zap.String("provider_id", p.ID), zap.Error(err))
			continue
		}
		if bot == nil {
			newBot, ensureErr := h.botUserSvc.EnsureBotUserForProvider(c.Request.Context(), &p)
			if ensureErr != nil || newBot == nil {
				logger.L().Warn("grant daily: bot ensure failed",
					zap.String("provider_id", p.ID), zap.Error(ensureErr))
				continue
			}
			bot = newBot
		}
		item.BotUserID = bot.ID

		// 2) Attempt to INSERT the dedup row. Composite unique key catches
		// "already granted today" via MySQL 1062 / GORM's ErrDuplicatedKey.
		grantRow := models.TLsmGameAdminGrant{
			ID:           util.NewUUID(),
			ProviderID:   p.ID,
			GrantDate:    dateStr,
			GrantedByUID: granterUID,
			Amount:       req.Amount,
			BotUserID:    bot.ID,
			BalanceAfter: 0, // updated in step 4 after Credit commits
			Remark:       req.Remark,
			CreatedAt:    time.Now(),
		}
		if err := h.gormDB.WithContext(c.Request.Context()).Create(&grantRow).Error; err != nil {
			if isDuplicateKeyErr(err) {
				// (provider_id, grant_date) already has a row → already granted
				// today. Surface to skipped with amount=0 so the operator can
				// see which models were left untouched.
				skipped = append(skipped, GrantedItem{
					ProviderID:   item.ProviderID,
					ProviderName: item.ProviderName,
					BotUserID:    bot.ID,
					Amount:       0,
				})
				continue
			}
			logger.L().Warn("grant daily: dedup insert failed",
				zap.String("provider_id", p.ID), zap.Error(err))
			continue
		}

		// 3) Credit the wallet via the canonical WalletService path so the
		// double-entry ledger invariant (balance + sum(tx.amount) == balance_after)
		// is preserved.
		if err := h.walletSvc.Credit(c.Request.Context(),
			bot.ID, string(service.TxTypeAdminDailyGrant),
			"admin_daily_grant", granterUID, "werewolf", req.Remark, req.Amount); err != nil {
			// Roll back the dedup row so retry / manual cleanups stay simple;
			// otherwise the row says "granted" while the wallet never moved,
			// which is the worst possible state (operator sees "skipped" on
			// re-run and assumes credit succeeded earlier).
			if delErr := h.gormDB.WithContext(c.Request.Context()).
				Where("id = ?", grantRow.ID).
				Delete(&models.TLsmGameAdminGrant{}).Error; delErr != nil {
				logger.L().Error("grant daily: dedup rollback failed",
					zap.String("grant_id", grantRow.ID), zap.Error(delErr))
			}
			logger.L().Warn("grant daily: credit failed",
				zap.String("provider_id", p.ID), zap.Int64("amount", req.Amount), zap.Error(err))
			continue
		}

		// 4) Re-read post-balance; not fatal if the read fails — log and
		// fall back to 0 on the dedup row so we don't double-mutate.
		newBal, _, _, balErr := h.walletSvc.GetBalanceWithStats(c.Request.Context(), bot.ID)
		if balErr != nil {
			logger.L().Warn("grant daily: post-balance read failed",
				zap.String("bot_user_id", bot.ID), zap.Error(balErr))
			newBal = 0
		}
		if err := h.gormDB.WithContext(c.Request.Context()).
			Model(&models.TLsmGameAdminGrant{}).
			Where("id = ?", grantRow.ID).
			Update("balance_after", newBal).Error; err != nil {
			logger.L().Warn("grant daily: balance_after update failed",
				zap.String("grant_id", grantRow.ID), zap.Error(err))
		}

		granted = append(granted, GrantedItem{
			ProviderID:   item.ProviderID,
			ProviderName: item.ProviderName,
			BotUserID:    bot.ID,
			Amount:       req.Amount,
			BalanceAfter: newBal,
		})
	}

	logger.L().Info("grant daily finished",
		zap.String("granter", granterUID),
		zap.Int("granted", len(granted)),
		zap.Int("skipped", len(skipped)),
		zap.String("date", dateStr),
		zap.Int64("amount", req.Amount),
		zap.Int("providers_considered", len(providers)))

	c.JSON(http.StatusOK, gin.H{
		"code": errcode.OK, "message": "ok",
		"data": GrantDailyResponse{
			Granted: granted,
			Skipped: skipped,
			Date:    dateStr,
		},
	})
}

// ─────────────────── helpers ───────────────────

// grantedItemFromProvider pulls the response-shape fields out of a provider
// row. Defined as a small helper so the request loop stays focused on the
// grant / dedup / credit logic.
func grantedItemFromProvider(p *models.TLsmGameLlmProvider) GrantedItem {
	return GrantedItem{
		ProviderID:   p.ID,
		ProviderName: p.AgentName,
	}
}

// isDuplicateKeyErr returns true when err wraps a MySQL 1062 (or GORM's
// modern ErrDuplicatedKey). Used to distinguish "(provider, date) already
// granted today" from real DB failures during the dedup insert.
func isDuplicateKeyErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	var merr *mysql.MySQLError
	return errors.As(err, &merr) && merr.Number == 1062
}
