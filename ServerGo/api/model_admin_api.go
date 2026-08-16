// Package api — model_admin_api.go exposes the admin CRUD endpoints for
// t_lsm_game_llm_provider rows, plus the lightweight "test" and "reload"
// helpers. These endpoints back the React ModelAdminPage (Phase 5) and let
// operators add / disable / remove LLM providers without editing
// LsmAgentGame.conf + restarting the server.
//
// Endpoints (all require admin role; reload also requires super admin so a
// non-super admin can't trigger a registry-wide reload that might race with
// another admin's create):
//
//	GET    /api/admin/llm/providers
//	POST   /api/admin/llm/providers
//	PUT    /api/admin/llm/providers/:id
//	DELETE /api/admin/llm/providers/:id          (soft delete: enabled=false)
//	POST   /api/admin/llm/providers/:id/test
//	POST   /api/admin/llm/providers/reload       (super admin only)
//
// Hard rules:
//   - API keys are NEVER returned to clients. Responses echo api_key_hint
//     (the short fingerprint computed at write time) instead.
//   - All handlers call requireAdmin / requireSuper FIRST so a non-admin
//     caller is rejected before any DB work happens.
//   - JSON bodies use json.Decoder with DisallowUnknownFields so the front-
//     end / test harness gets a clear 400 on typos instead of silent data
//     loss.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"LsmAgentGame/errcode"
	"LsmAgentGame/llm"
	"LsmAgentGame/llm/anthropic"
	"LsmAgentGame/llm/openai"
	types "LsmAgentGame/llm/types"
	"LsmAgentGame/logger"
	"LsmAgentGame/models"
	"LsmAgentGame/service"
	"LsmAgentGame/util"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// ModelAdminAPI is the admin resource handler for LLM provider CRUD.
type ModelAdminAPI struct {
	svc        UserRoleChecker
	registry   *llm.Registry
	gormDB     *gorm.DB
	botUserSvc *service.BotUserService
	modelSvc   *service.ModelLogService
	walletSvc  *service.WalletService
}

// NewModelAdminAPI wires the handler with its dependencies. nil values for
// any service are tolerated at construction; each handler enforces "not nil"
// before using it.
func NewModelAdminAPI(
	svc UserRoleChecker,
	registry *llm.Registry,
	gormDB *gorm.DB,
	botUserSvc *service.BotUserService,
	modelSvc *service.ModelLogService,
	walletSvc *service.WalletService,
) *ModelAdminAPI {
	return &ModelAdminAPI{
		svc:        svc,
		registry:   registry,
		gormDB:     gormDB,
		botUserSvc: botUserSvc,
		modelSvc:   modelSvc,
		walletSvc:  walletSvc,
	}
}

// ─────────────────── auth helpers ───────────────────

// requireAdmin returns the authenticated user_id and a "passed" flag. When
// it returns false, the caller must NOT write the response again — requireAdmin
// has already written a 401/403 envelope.
//
// Mirrors the pattern from admin_api.go (which re-uses UserService.GetUserType
// to validate the role a second time after AuthRequired middleware did so).
func (h *ModelAdminAPI) requireAdmin(c *gin.Context) (string, bool) {
	uid := uidFromContext(c)
	if uid == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    errcode.ErrAuthMissingToken,
			"message": errcode.DefaultMessages[errcode.ErrAuthMissingToken],
		})
		return "", false
	}
	if h.svc == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    errcode.ErrInternal,
			"message": "user service not wired",
		})
		return "", false
	}
	userType, err := h.svc.GetUserType(c.Request.Context(), uid)
	if err != nil {
		ce := errcode.AsError(err)
		c.JSON(http.StatusBadRequest, gin.H{"code": ce.Code, "message": ce.Message})
		return "", false
	}
	if userType < models.UserTypeAdmin {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    errcode.ErrPermissionDenied,
			"message": "需要管理员权限",
		})
		return "", false
	}
	return uid, true
}

// requireSuper is the same shape as requireAdmin but enforces super-admin.
// Used by the reload endpoint because a registry reload mid-flight could race
// with another admin's create/update and surprise 7-AI rooms.
func (h *ModelAdminAPI) requireSuper(c *gin.Context) (string, bool) {
	uid, ok := h.requireAdmin(c)
	if !ok {
		return "", false
	}
	userType, err := h.svc.GetUserType(c.Request.Context(), uid)
	if err != nil {
		ce := errcode.AsError(err)
		c.JSON(http.StatusBadRequest, gin.H{"code": ce.Code, "message": ce.Message})
		return "", false
	}
	if userType < models.UserTypeSuper {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    errcode.ErrPermissionDenied,
			"message": "需要超级管理员权限",
		})
		return "", false
	}
	return uid, true
}

// ─────────────────── request types ───────────────────

// CreateProviderRequest is the JSON body for POST /api/admin/llm/providers.
// CreatedAt is server-set; APIKey is required (we never want a placeholder
// row leaking into the in-memory providers map). ProviderType must be one of
// the LLM protocol names currently wired — today only "anthropic" / "openai".
type CreateProviderRequest struct {
	AgentName    string `json:"agent_name" binding:"required,min=1,max=64"`
	Model        string `json:"model"      binding:"required,min=1,max=64"`
	// §20260814-01 — 规范值 anthropic-messages / openai-completions;旧值
	// "anthropic"/"openai" 由服务端归一化(binding 放宽为 required + 自定义校验)。
	ProviderType string `json:"provider_type" binding:"required,min=1,max=32"`
	APIKey       string `json:"api_key"    binding:"required,min=1"`
	Endpoint     string `json:"endpoint,omitempty"`
	// §135 修复 — 新增模型表单会带 `enabled` checkbox,与 UpdateProviderRequest 对齐。
	// 使用 *bool 是因为前端编辑表单在「未改动」时也常把 enabled=false 写出来(默认值),
	// 想要「保留 DB 原值」必须靠 nil 判定;新建场景下 nil 视作 true。
	Enabled  *bool  `json:"enabled,omitempty"`
	Remark   string `json:"remark,omitempty"`
	// §R224 (2026-08-01) — 重新引入 thinking 配置字段。
	// *bool + *int 是为了 Update 路径能用 nil 区分"未设置" / "显式 false" /
	// "显式 0 budget"。Create 路径用 nil 兜底为 false / 0(LLM API 实际
	// 不发请求时由 operator 在 admin UI 自行打开)。
	ThinkingEnabled      *bool `json:"thinking_enabled,omitempty"`
	ThinkingBudgetTokens *int  `json:"thinking_budget_tokens,omitempty"`
}

// UpdateProviderRequest is the JSON body for PUT /api/admin/llm/providers/:id.
// All fields are pointers so the handler can distinguish "field omitted" from
// "field set to zero". A nil pointer means "don't change"; a non-nil pointer
// means "update to this value".
//
// 前端 ModelAdminPage.submitForm 在编辑时会对比原始值,仅把「用户改过的字段」
// 塞进 body(未改动字段留空/等于原值则不传),所以后端必须靠 nil 判定「是否修改」。
// §R224 (2026-08-01) — 重新引入 ThinkingEnabled / ThinkingBudgetTokens 字段;
// §128 误删后,这里把对应 *bool / *int 字段加回。
type UpdateProviderRequest struct {
	AgentName    *string `json:"agent_name,omitempty"`
	Model        *string `json:"model,omitempty"`
	ProviderType *string `json:"provider_type,omitempty"`
	APIKey       *string `json:"api_key,omitempty"`
	Endpoint     *string `json:"endpoint,omitempty"`
	Enabled      *bool   `json:"enabled,omitempty"`
	Remark       *string `json:"remark,omitempty"`
	// §R224 — thinking 配置。Update 用 *bool/*int 是为了支持"关闭 thinking"
	// (改 *bool = false) 与"调整 budget" (改 *int = 4096) 两种操作,
	// 同时用 nil 区分"未改"(保留 DB 原值)。
	ThinkingEnabled      *bool `json:"thinking_enabled,omitempty"`
	ThinkingBudgetTokens *int  `json:"thinking_budget_tokens,omitempty"`
}

// apiKeyHint builds the human-readable fingerprint stored in
// t_lsm_game_llm_provider.api_key_hint. "sk-XXXX...YYYY" — first 4 + last 4
// chars, dashes kept. Empty / placeholder → "". Mirrors registry.apiKeyHint so
// the API hint matches what the registry would have computed at seed time.
func apiKeyHintLocal(plain string) string {
	plain = strings.TrimSpace(plain)
	if plain == "" || plain == types.PlaceholderKey {
		return ""
	}
	if len(plain) <= 8 {
		return plain
	}
	return plain[:4] + "..." + plain[len(plain)-4:]
}

// providerView is the API representation shared by list/create/update.
// It embeds the persisted row and adds endpoint values derived from the
// LLM registry configuration so CRUD responses stay shape-compatible with
// ListProviders (which §133.2 / commit 35cc245 introduced this view for).
//
// The derived fields are NOT persisted into t_lsm_game_llm_provider — they
// are computed on the fly from the row + registry default endpoint.
type providerView struct {
	models.TLsmGameLlmProvider
	EffectiveEndpoint string `json:"effective_endpoint"`
	EndpointInherited bool   `json:"endpoint_inherited"`
	// §135 修复 — bot_user_id 是钱包/对局日志路由的真正参数(provider.id 不等于 bot_user.id)。
	// 用 omitempty:未生成 bot user 的 provider 不带该字段,前端按 null 走"无钱包"提示。
	BotUserID string `json:"bot_user_id,omitempty"`
	// Balance is the bot user's wallet coin balance, surfaced in the list
	// response so the admin LLM model page can render a "coin" column. Pointer
	// + omitempty so a missing bot user / wallet row omits the field entirely
	// (rather than printing "balance": 0 which would conflate "no wallet" with
	// "wallet has 0 coins").
	Balance *int64 `json:"balance,omitempty"`
}

// newProviderView returns the API view for a persisted provider row.
// Pass defaultEndpoint as registryDefaultEndpoint(h.registry) — kept as a
// parameter so the helper is a pure function and trivially unit-testable.
// botUserID is "" when the provider has no backing bot user yet.
// balance is nil when the bot user has no wallet row.
func newProviderView(row models.TLsmGameLlmProvider, defaultEndpoint, botUserID string, balance *int64) providerView {
	rowEP := strings.TrimSpace(row.Endpoint)
	effective := rowEP
	inherited := false
	if effective == "" {
		effective = defaultEndpoint
		inherited = true
	}
	return providerView{
		TLsmGameLlmProvider: row,
		EffectiveEndpoint:   effective,
		EndpointInherited:   inherited,
		BotUserID:           botUserID,
		Balance:             balance,
	}
}

// ─────────────────── ListProviders ───────────────────

// ListProviders GET /api/admin/llm/providers.
//
// Returns rows from t_lsm_game_llm_provider, ordered by created_at ASC.
// API keys are never included (the model's JSON tag has `json:"-"` for
// APIKeyEnc; APIKeyHint is the only key-related field on the wire).
//
// §20260816-03 —— 默认**只返回 enabled=true**。
// DeleteProvider 是软删除(enabled=false),历史上本接口裸 Find() 不带任何
// 过滤,导致管理员「删掉」的行在刷新/重启后原样重现 —— 用户观感是「删不掉」,
// 而实际上删除早已成功,只是列表又把它读回来了。
//
// 带 ?include_disabled=1 时返回全部(含已停用),供运维查看/恢复误删的行。
// 这是软删除必须配套的「可见开关」:既然选择保留行,就必须让它可见可恢复,
// 而不是既看得见又删不掉(§7.1 操作结果必须真实可见)。
func (h *ModelAdminAPI) ListProviders(c *gin.Context) {
	if _, ok := h.requireAdmin(c); !ok {
		return
	}
	if h.gormDB == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrInternal, "message": "db not wired",
		})
		return
	}
	includeDisabled := c.Query("include_disabled") == "1"
	q := h.gormDB.WithContext(c.Request.Context()).Order("created_at ASC")
	if !includeDisabled {
		q = q.Where("enabled = ?", true)
	}
	var rows []models.TLsmGameLlmProvider
	if err := q.Find(&rows).Error; err != nil {
		logger.L().Error("admin list llm providers failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrDB, "message": "list providers failed",
		})
		return
	}
	// §133 / §133.2 — 把每行的「实际生效 endpoint」+「是否回退到全局」一起返回。
	// 见 registry.go:registry.Endpoint — endpoint 为空时回退到全局 cfg.LLM.Endpoint。
	// providerView 已由包级定义复用，不再在 handler 内匿名声明。
	defaultEndpoint := registryDefaultEndpoint(h.registry)

	// §135 修复 — 批量查询 bot user id,避免 N+1。
	// 单次失败仅 log,不阻塞列表(前端个别行展示空 bot_user_id 即可)。
	//
	// 同时把所有 bot user id 收集起来,再走一次 GetWalletBalances 把对应钱包余额
	// 一次性取回来(LIST 端点 N+1 兜底),然后组装时按 id 命中写入 view.Balance。
	botUserIDs := make([]string, 0, len(rows))
	rowBotUserID := make([]string, len(rows))
	ctx := c.Request.Context()
	for i, row := range rows {
		var botUserID string
		if h.botUserSvc != nil {
			bu, err := h.botUserSvc.GetBotUserForProvider(ctx, row.ID)
			if err != nil {
				logger.L().Warn("list providers: resolve bot user failed",
					zap.String("provider_id", row.ID), zap.Error(err))
			} else if bu != nil {
				botUserID = bu.ID
			}
		}
		rowBotUserID[i] = botUserID
		if botUserID != "" {
			botUserIDs = append(botUserIDs, botUserID)
		}
	}
	// Batch balance lookup. A missing wallet service or a failed batch is
	// treated as "no balance info" — every row's Balance stays nil and the
	// list endpoint still returns 200 (frontend renders "—" in the coin
	// column). We don't want a transient wallet read failure to break the
	// whole list page.
	balances := map[string]int64{}
	if h.walletSvc != nil && len(botUserIDs) > 0 {
		b, err := h.walletSvc.GetWalletBalances(ctx, botUserIDs)
		if err != nil {
			logger.L().Warn("list providers: batch wallet balance fetch failed",
				zap.Int("bot_user_count", len(botUserIDs)), zap.Error(err))
		} else {
			balances = b
		}
	}

	views := make([]providerView, 0, len(rows))
	for i, row := range rows {
		botUserID := rowBotUserID[i]
		var balancePtr *int64
		if botUserID != "" {
			if bal, ok := balances[botUserID]; ok {
				// Take a copy so the loop variable doesn't alias the same
				// memory across iterations.
				v := bal
				balancePtr = &v
			}
		}
		views = append(views, newProviderView(row, defaultEndpoint, botUserID, balancePtr))
	}
	// §20260816-03 —— 顺带回传「已停用行数」,让前端能显示
	// 「另有 N 个已停用模型」并提供查看入口,而不是让软删除的行凭空消失。
	// 单次统计失败仅 log,不影响列表返回(disabled_count 保持 0)。
	var disabledCount int64
	if err := h.gormDB.WithContext(ctx).
		Model(&models.TLsmGameLlmProvider{}).
		Where("enabled = ?", false).
		Count(&disabledCount).Error; err != nil {
		logger.L().Warn("list providers: count disabled failed", zap.Error(err))
	}

	c.JSON(http.StatusOK, gin.H{
		"code": errcode.OK, "message": "ok",
		"data": gin.H{
			"providers":        views,
			"total":            len(views),
			"source":           registrySource(h.registry),
			"default_endpoint": defaultEndpoint,
			"include_disabled": includeDisabled,
			"disabled_count":   disabledCount,
		},
	})
}

// ─────────────────── CreateProvider ───────────────────

// CreateProvider POST /api/admin/llm/providers.
//
// Inserts a new t_lsm_game_llm_provider row (API key encrypted via the master
// AES key) and provisions a backing bot user via BotUserService. The registry
// itself is NOT auto-reloaded — the operator hits POST /reload explicitly so
// 7-AI rooms aren't surprised by a mid-flight provider list change. Returns
// the freshly-inserted row so the front-end can append it to its list.
func (h *ModelAdminAPI) CreateProvider(c *gin.Context) {
	uid, ok := h.requireAdmin(c)
	if !ok {
		return
	}

	// Parse body before any DB check so a typo'd field gets the standard 400
	// envelope instead of being shadowed by a "db not wired" 500.
	var req CreateProviderRequest
	if !decodeJSONStrict(c, &req) {
		return
	}

	if h.gormDB == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrInternal, "message": "db not wired",
		})
		return
	}

	// R187-2: SanitizeModelKey strips invisible Unicode format (Cf) runes
	// (ZWSP/ZWNJ/ZWJ/BOM/soft hyphen…) in addition to trimming. A key pasted
	// into the admin UI with a zero-width char is transparently normalized;
	// if the sanitized key collides with an existing row, the DB unique
	// constraint on `model` triggers the isMySQLDuplicateErr 400 below.
	modelKey := util.SanitizeModelKey(req.Model)
	agentName := strings.TrimSpace(req.AgentName)
	if modelKey == "" || agentName == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": errcode.ErrValidationFailed, "message": "model / agent_name required",
		})
		return
	}

	// §20260814-01 — 归一化 + 白名单校验协议标识。兼容旧值 anthropic/openai。
	providerType := types.NormalizeProviderType(req.ProviderType)
	if !types.IsSupportedProviderType(req.ProviderType) {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    errcode.ErrValidationFailed,
			"message": "provider_type 必须为 anthropic-messages 或 openai-completions",
		})
		return
	}
	// openai-completions 行必须有 per-row endpoint(无全局默认可回退)。
	if providerType == types.ProviderTypeOpenAICompletions && strings.TrimSpace(req.Endpoint) == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    errcode.ErrValidationFailed,
			"message": "openai-completions 协议必须填写 endpoint(基础地址,请求时自动追加 /chat/completions)",
		})
		return
	}

	// Encrypt the API key with the persisted master key.
	enc, err := util.EncryptAPIKey(c.Request.Context(), h.gormDB, req.APIKey)
	if err != nil {
		logger.L().Error("admin create provider: encrypt api_key failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrInternal, "message": "encrypt api_key failed",
		})
		return
	}

	// §135 修复 — Enabled 取 req.Enabled;nil 视作 true(默认开启)。
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	row := models.TLsmGameLlmProvider{
		ID:           util.NewUUID(),
		AgentName:    agentName,
		Model:        modelKey,
		ProviderType: providerType,
		APIKeyEnc:    enc,
		APIKeyHint:   apiKeyHintLocal(req.APIKey),
		Endpoint:     strings.TrimSpace(req.Endpoint),
		Enabled:      enabled,
		Remark:       strings.TrimSpace(req.Remark),
		// §R224 (2026-08-01) — 把 thinking 配置写入 DB;Create 路径下
		// nil 视作 false / 0,operator 可在 admin UI 后续打开。
		ThinkingEnabled:      req.ThinkingEnabled != nil && *req.ThinkingEnabled,
		ThinkingBudgetTokens: budgetFromPtr(req.ThinkingBudgetTokens),
	}

	if err := h.gormDB.WithContext(c.Request.Context()).
		Create(&row).Error; err != nil {
		if isMySQLDuplicateErr(err) {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    errcode.ErrValidationFailed,
				"message": "model already exists",
			})
			return
		}
		logger.L().Error("admin create llm provider failed",
			zap.String("model", modelKey), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrDB, "message": "create provider failed",
		})
		return
	}

	// Provision the backing bot user so the model can join rooms as a player.
	if h.botUserSvc != nil {
		if _, err := h.botUserSvc.EnsureBotUserForProvider(c.Request.Context(), &row); err != nil {
			// Non-fatal — the provider row is committed; the bot user can be
			// backfilled on next reload. Log + surface as a warning in the
			// response so the operator can decide whether to retry.
			logger.L().Warn("admin create provider: bot user provisioning failed",
				zap.String("model", modelKey), zap.Error(err))
		}
	}

	logger.L().Info("admin created llm provider",
		zap.String("admin_id", uid),
		zap.String("model", modelKey),
		zap.String("agent_name", agentName),
		zap.String("provider_type", row.ProviderType))

	// §135 修复 — 创建成功后回查 bot_user_id,前端「模型详情」页要用它查钱包。
	// 这里调用纯读不写入的 GetBotUserForProvider,失败时 bot_user_id 留空。
	var botUserID string
	if h.botUserSvc != nil {
		if bu, lookupErr := h.botUserSvc.GetBotUserForProvider(c.Request.Context(), row.ID); lookupErr == nil && bu != nil {
			botUserID = bu.ID
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code": errcode.OK, "message": "ok",
		"data": gin.H{
			// Balance is list-only (batched N+1 avoidance); create echoes nil
			// so the field is omitted and the front-end falls back to the
			// detail-page fetch path.
			"provider": newProviderView(row, registryDefaultEndpoint(h.registry), botUserID, nil),
			"warning":  botProvisionWarning(h.botUserSvc, row),
		},
	})
}

// budgetFromPtr unwraps an *int budget, returning the value or 0 when nil.
// Used by Create/Update paths where the absence of `thinking_budget_tokens`
// means "don't change" (Update) or "use 0" (Create).
//
// §R224: 0 budget 在 anthropic provider 端会被 injectThinkingBlocks 兜底为
// 4096,所以"创建设为 0 + thinking_enabled=true"仍然可用,只是 budget 走默认。
func budgetFromPtr(p *int) int {
	if p == nil {
		return 0
	}
	if *p < 0 {
		return 0
	}
	return *p
}

// botProvisionWarning returns a short string when the bot user for a newly-
// created provider couldn't be provisioned, so the caller can surface it in
// the UI. Returns "" when nothing went wrong.
func botProvisionWarning(svc *service.BotUserService, row models.TLsmGameLlmProvider) string {
	if svc == nil {
		return ""
	}
	// Best-effort probe — we already attempted this in CreateProvider; this is
	// just the "did it work?" tail. If it failed, the log carries the detail.
	_, err := svc.EnsureBotUserForProvider(context.Background(), &row)
	if err != nil {
		return "bot user provisioning failed (see server log)"
	}
	return ""
}

// ─────────────────── UpdateProvider ───────────────────

// UpdateProvider PUT /api/admin/llm/providers/:id.
//
// Pointer fields so the caller can distinguish "leave unchanged" from
// "explicitly set to zero value". APIKey is encrypted on the way down. After
// the UPDATE the registry is NOT reloaded — same rationale as CreateProvider.
func (h *ModelAdminAPI) UpdateProvider(c *gin.Context) {
	uid, ok := h.requireAdmin(c)
	if !ok {
		return
	}
	if h.gormDB == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrInternal, "message": "db not wired",
		})
		return
	}

	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": errcode.ErrValidationFailed, "message": "id required",
		})
		return
	}

	var req UpdateProviderRequest
	if !decodeJSONStrict(c, &req) {
		return
	}

	// Find existing row first so the response can echo the final state.
	var existing models.TLsmGameLlmProvider
	if err := h.gormDB.WithContext(c.Request.Context()).
		Where("id = ?", id).First(&existing).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"code": errcode.ErrValidationFailed, "message": "provider not found",
			})
			return
		}
		logger.L().Error("admin update provider: lookup failed",
			zap.String("id", id), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrDB, "message": "lookup failed",
		})
		return
	}

	updates := map[string]any{}
	if req.AgentName != nil {
		updates["agent_name"] = strings.TrimSpace(*req.AgentName)
	}
	if req.Model != nil {
		// model 是 registry 的 key,不允许改成空串。
		// R187-2: 同样经过 SanitizeModelKey,把粘贴进来的零宽字符(Cf)剔除;
		// 归一化后若与其它行冲突,由 DB 唯一索引兜底报 duplicate。
		clean := util.SanitizeModelKey(*req.Model)
		if clean == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"code": errcode.ErrValidationFailed, "message": "model cannot be empty",
			})
			return
		}
		updates["model"] = clean
	}
	if req.ProviderType != nil {
		pt := types.NormalizeProviderType(*req.ProviderType)
		if !types.IsSupportedProviderType(*req.ProviderType) {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    errcode.ErrValidationFailed,
				"message": "provider_type 必须为 anthropic-messages 或 openai-completions",
			})
			return
		}
		updates["provider_type"] = pt
	}
	if req.APIKey != nil {
		enc, err := util.EncryptAPIKey(c.Request.Context(), h.gormDB, *req.APIKey)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code": errcode.ErrInternal, "message": "encrypt api_key failed",
			})
			return
		}
		updates["api_key_enc"] = enc
		updates["api_key_hint"] = apiKeyHintLocal(*req.APIKey)
	}
	if req.Endpoint != nil {
		updates["endpoint"] = strings.TrimSpace(*req.Endpoint)
	}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}
	if req.Remark != nil {
		updates["remark"] = strings.TrimSpace(*req.Remark)
	}
	// §R224 (2026-08-01) — Update 路径:thinking 配置同样走 *bool / *int 语义。
	// nil 保留 DB 原值(不写在 updates map),非 nil 即覆盖。
	if req.ThinkingEnabled != nil {
		updates["thinking_enabled"] = *req.ThinkingEnabled
	}
	if req.ThinkingBudgetTokens != nil {
		budget := budgetFromPtr(req.ThinkingBudgetTokens)
		updates["thinking_budget_tokens"] = budget
	}

	if len(updates) == 0 {
		// Nothing to update — return current row unchanged (still as §133.2 view).
		// §135 修复 — 同样回查 bot_user_id,保证详情页能拿到正确 ID。
		var botUserID string
		if h.botUserSvc != nil {
			if bu, lookupErr := h.botUserSvc.GetBotUserForProvider(c.Request.Context(), existing.ID); lookupErr == nil && bu != nil {
				botUserID = bu.ID
			}
		}
		c.JSON(http.StatusOK, gin.H{
			"code": errcode.OK, "message": "ok",
			"data": gin.H{
				// Balance omitted here (no-op update path).
				"provider": newProviderView(existing, registryDefaultEndpoint(h.registry), botUserID, nil),
			},
		})
		return
	}

	if err := h.gormDB.WithContext(c.Request.Context()).
		Model(&models.TLsmGameLlmProvider{}).
		Where("id = ?", id).
		Updates(updates).Error; err != nil {
		logger.L().Error("admin update llm provider failed",
			zap.String("id", id), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrDB, "message": "update failed",
		})
		return
	}

	// Re-fetch + return the final row.
	if err := h.gormDB.WithContext(c.Request.Context()).
		Where("id = ?", id).First(&existing).Error; err != nil {
		logger.L().Error("admin update provider: post-refetch failed",
			zap.String("id", id), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrDB, "message": "post-refetch failed",
		})
		return
	}

	logger.L().Info("admin updated llm provider",
		zap.String("admin_id", uid),
		zap.String("id", id),
		zap.Int("fields_changed", len(updates)))

	// §135 修复 — 更新后回查 bot_user_id,与 list/create 保持一致。
	var botUserID string
	if h.botUserSvc != nil {
		if bu, lookupErr := h.botUserSvc.GetBotUserForProvider(c.Request.Context(), existing.ID); lookupErr == nil && bu != nil {
			botUserID = bu.ID
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code": errcode.OK, "message": "ok",
		"data": gin.H{
			// §133.2 — Re-fetch 后重新用 view 包装:派生字段 (effective_endpoint / endpoint_inherited)
			// 基于最终持久化结果计算,不沿用 request 中未规范化的值。
			// Balance omitted on update echo; list endpoint is the canonical
			// source for the coin column.
			"provider": newProviderView(existing, registryDefaultEndpoint(h.registry), botUserID, nil),
		},
	})
}

// ─────────────────── DeleteProvider ───────────────────

// DeleteProvider DELETE /api/admin/llm/providers/:id[?hard=1].
//
// **默认软删除**: sets enabled=false instead of physically removing the row.
// Keeps the audit trail intact (t_lsm_game_model_game_log.bot_user_id points
// to bot users that derived their linkage from this provider) and avoids
// surprises in 7-AI rooms currently in flight.
//
// §20260816-03 —— 新增 ?hard=1 物理删除(**仅超级管理员**)。
// 动机: 软删除挡不住「本就不该存在」的脏数据 —— 2026-08-13 一次
// `go test -tags llmintegration` 把 7 行测试数据写进了生产库,软删除只能让
// 它们 enabled=false 地继续躺在表里,运维唯一的出路是手工 SQL(而手工 SQL
// 才是真正危险的)。
//
// 硬删除前**必须**确认无审计引用:
//   - t_lsm_game_model_game_log.provider_id
//   - t_lsm_game_model_chat_message.provider_id
//
// 任一有引用即拒绝(409),提示改用软删除 —— 审计链(§118)优先于清洁度。
// 无引用时连同关联 bot user 一并物理删除。
func (h *ModelAdminAPI) DeleteProvider(c *gin.Context) {
	hard := c.Query("hard") == "1"
	// 硬删除不可逆,权限要求提升到超级管理员。
	var uid string
	var ok bool
	if hard {
		uid, ok = h.requireSuper(c)
	} else {
		uid, ok = h.requireAdmin(c)
	}
	if !ok {
		return
	}
	if h.gormDB == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrInternal, "message": "db not wired",
		})
		return
	}

	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": errcode.ErrValidationFailed, "message": "id required",
		})
		return
	}

	if hard {
		h.hardDeleteProvider(c, uid, id)
		return
	}

	res := h.gormDB.WithContext(c.Request.Context()).
		Model(&models.TLsmGameLlmProvider{}).
		Where("id = ?", id).
		Updates(map[string]any{"enabled": false})
	if res.Error != nil {
		logger.L().Error("admin delete llm provider failed",
			zap.String("id", id), zap.Error(res.Error))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrDB, "message": "delete failed",
		})
		return
	}
	if res.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{
			"code": errcode.ErrValidationFailed, "message": "provider not found",
		})
		return
	}
	logger.L().Info("admin soft-deleted llm provider",
		zap.String("admin_id", uid),
		zap.String("id", id))
	c.JSON(http.StatusOK, gin.H{
		"code": errcode.OK, "message": "ok",
		"data": gin.H{
			"id":      id,
			"enabled": false,
			"soft":    true,
		},
	})
}

// hardDeleteProvider 物理删除一行 provider(§20260816-03)。
//
// 调用方已完成 requireSuper 鉴权与 id 非空校验。本函数负责:
//  1. 行存在性检查(404);
//  2. 审计引用检查 —— t_lsm_game_model_game_log / t_lsm_game_model_chat_message
//     任一存在 provider_id 引用即 409 拒绝(审计链优先,§118);
//  3. 在单个事务里删除关联 bot user + provider 行。
//
// 为什么引用检查不能省: 这两张表的 provider_id 是复盘 AI 对局的唯一线索,
// 删掉 provider 行会让历史对局日志指向一个不存在的模型。对「有历史」的
// provider,软删除(enabled=false)才是正确语义;硬删除只服务于「本就不该
// 存在」的脏数据。
func (h *ModelAdminAPI) hardDeleteProvider(c *gin.Context, uid, id string) {
	ctx := c.Request.Context()

	var row models.TLsmGameLlmProvider
	if err := h.gormDB.WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"code": errcode.ErrValidationFailed, "message": "provider not found",
			})
			return
		}
		logger.L().Error("admin hard delete: read row failed",
			zap.String("id", id), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrDB, "message": "read provider failed",
		})
		return
	}

	// 审计引用检查。两张表分别统计,便于在错误信息里告诉管理员到底哪一类
	// 记录挡住了删除。
	var gameLogs, chatMsgs int64
	if err := h.gormDB.WithContext(ctx).
		Model(&models.TLsmGameModelGameLog{}).
		Where("provider_id = ?", id).Count(&gameLogs).Error; err != nil {
		logger.L().Error("admin hard delete: count game logs failed",
			zap.String("id", id), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrDB, "message": "count references failed",
		})
		return
	}
	if err := h.gormDB.WithContext(ctx).
		Model(&models.TLsmGameModelChatMessage{}).
		Where("provider_id = ?", id).Count(&chatMsgs).Error; err != nil {
		logger.L().Error("admin hard delete: count chat messages failed",
			zap.String("id", id), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrDB, "message": "count references failed",
		})
		return
	}
	if gameLogs > 0 || chatMsgs > 0 {
		logger.L().Warn("admin hard delete refused — audit references exist",
			zap.String("admin_id", uid), zap.String("id", id),
			zap.Int64("game_logs", gameLogs), zap.Int64("chat_messages", chatMsgs))
		c.JSON(http.StatusConflict, gin.H{
			"code": errcode.ErrValidationFailed,
			"message": fmt.Sprintf(
				"该模型有 %d 条对局日志、%d 条对话记录,为保留审计链禁止物理删除;请改用停用(软删除)",
				gameLogs, chatMsgs),
			"data": gin.H{
				"id":            id,
				"game_logs":     gameLogs,
				"chat_messages": chatMsgs,
			},
		})
		return
	}

	// 无引用 → 事务内删 bot user + provider 行。bot user 先删,避免中途失败
	// 留下指向已删除 provider 的孤儿 bot。
	var deletedBotUsers int64
	if err := h.gormDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		bu := tx.Where("bot_provider_id = ?", id).Delete(&models.TLsmGameUser{})
		if bu.Error != nil {
			return fmt.Errorf("delete bot user: %w", bu.Error)
		}
		deletedBotUsers = bu.RowsAffected
		if err := tx.Where("id = ?", id).
			Delete(&models.TLsmGameLlmProvider{}).Error; err != nil {
			return fmt.Errorf("delete provider row: %w", err)
		}
		return nil
	}); err != nil {
		logger.L().Error("admin hard delete llm provider failed",
			zap.String("admin_id", uid), zap.String("id", id), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrDB, "message": "hard delete failed",
		})
		return
	}

	logger.L().Info("admin hard-deleted llm provider",
		zap.String("admin_id", uid),
		zap.String("id", id),
		zap.String("model", row.Model),
		zap.String("agent_name", row.AgentName),
		zap.Int64("deleted_bot_users", deletedBotUsers))
	c.JSON(http.StatusOK, gin.H{
		"code": errcode.OK, "message": "ok",
		"data": gin.H{
			"id":                id,
			"model":             row.Model,
			"hard":              true,
			"soft":              false,
			"deleted_bot_users": deletedBotUsers,
		},
	})
}

// ─────────────────── TestProvider ───────────────────

// TestProvider POST /api/admin/llm/providers/:id/test.
//
// 2026-07-10 重构:从"HEAD 探测 + registry 可用性检查"升级为"真实模拟 Anthropic
// 协议对话"——真正调用一次 LLM Chat,让管理员看到模型是否真的能按 Anthropic
// Messages API 协议返回内容。流程:
//  1. 从 DB 加载 provider 行(404 if missing);
//  2. 走 registry.Get(model) 拿到 provider 实例 + 已解密的 API Key;
//  3. 构造一次性 LLMRequest(Messages=[{user,"Hello…"}], MaxTokens=512);
//  4. 15s 超时调用 provider.Chat;记录耗时、usage、stop_reason、回复文本;
//  5. 把 ok / chat_text / chat_error / usage 全部塞进 data,前端可以拿来
//     直接展示模型的自我介绍。
//
// 注意:Registry.Get 不可用(provider 未注册 / 已禁用 / API Key 是占位符)时
// 退回到 HEAD + registry_ok 老路径,避免空 DB 把所有"测试"按钮打成红。
//
// This endpoint does NOT update the DB.
func (h *ModelAdminAPI) TestProvider(c *gin.Context) {
	if _, ok := h.requireAdmin(c); !ok {
		return
	}
	if h.gormDB == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrInternal, "message": "db not wired",
		})
		return
	}

	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": errcode.ErrValidationFailed, "message": "id required",
		})
		return
	}

	var row models.TLsmGameLlmProvider
	if err := h.gormDB.WithContext(c.Request.Context()).
		Where("id = ?", id).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"code": errcode.ErrValidationFailed, "message": "provider not found",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrDB, "message": "lookup failed",
		})
		return
	}

	result := gin.H{
		"id":                       id,
		"model":                    row.Model,
		"agent_name":               row.AgentName,
		"provider_type":            row.ProviderType,
		"endpoint":                 row.Endpoint,
		"api_key_hint":             row.APIKeyHint,
		"enabled":                  row.Enabled,
		"in_registry":              false,
		"registry_ok":              false,
		"endpoint_ok":              false,
		"chat_ok":                  false,
		"chat_text":                "",
		"chat_error":               "",
		"chat_latency_ms":          0,
		"chat_usage_input_tokens":  0,
		"chat_usage_output_tokens": 0,
		"chat_stop_reason":         "",
		"chat_id":                  "",
		"prompt":                   testProviderPrompt,
		"hint":                     "",
	}

	// 1) registry 可用性
	if h.registry != nil {
		if _, ok := h.registry.GetInfo(row.Model); ok {
			result["in_registry"] = true
			result["registry_ok"] = h.registry.IsAvailable(row.Model)
		}
	}
	if !result["in_registry"].(bool) {
		result["hint"] = "row present in DB but not in registry — call POST /api/admin/llm/providers/reload"
	}

	// 2) HEAD 端点健康探测(3s 超时,作为快速失败兜底)。
	// 优先探测该 provider 自己的 endpoint(DB 行);仅当其未配置(走全局
	// 默认)时才探测 registry 全局 endpoint——否则 DB 行指向健康代理、全局
	// conf endpoint 已死时,endpoint_ok 会误报 false 与 chat_ok=true 打架。
	if h.registry != nil {
		headEndpoint := strings.TrimSpace(row.Endpoint)
		if headEndpoint == "" {
			headEndpoint = h.registry.Endpoint()
		}
		status := llm.HealthStatus{Endpoint: headEndpoint, LastCheck: time.Now()}
		if headEndpoint == "" {
			status.LastError = "no endpoint configured"
		} else if resp, herr := headProbe(c.Request.Context(), headEndpoint); herr != nil {
			status.LastError = herr.Error()
		} else {
			resp.Body.Close()
			if resp.StatusCode >= 500 {
				status.LastError = fmt.Sprintf("upstream %d", resp.StatusCode)
			} else {
				status.OK = true
			}
		}
		result["endpoint_ok"] = status.OK
		if status.LastError != "" {
			result["endpoint_last_error"] = status.LastError
		}
	}

	// 3) 真实模拟 Anthropic 协议对话 —— 用 registry 拿到的 provider + key
	// 调一次 Chat。失败(未注册/占位 key/网络/超时)都不算致命,只把
	// chat_ok=false + chat_error 写回 result,让前端照样能展示。
	chatOK := h.runTestProviderChat(c.Request.Context(), row.Model, result)
	if chatOK {
		result["chat_ok"] = true
	} else {
		if result["hint"] == "" {
			result["hint"] = "real chat call failed — see chat_error / registry_ok / endpoint_ok"
		}
	}

	// 总 verdict:必须 真实对话成功 + registry 正常 才算 ok。
	ok := chatOK && result["registry_ok"].(bool)
	result["ok"] = ok
	c.JSON(http.StatusOK, gin.H{
		"code": errcode.OK, "message": "ok",
		"data": result,
	})
}

// headProbe 对单个 endpoint 发起一次 HEAD 探测(3s 超时)。从
// TestProvider 抽出,便于对"被测 provider 自己的 endpoint"做健康检查,
// 而不是误用 registry 全局 endpoint。
func headProbe(parent context.Context, endpoint string) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", "LsmAgentGame-HealthCheck/1.0")
	client := &http.Client{}
	return client.Do(req)
}

// testProviderPrompt 是真正下发给 LLM 的中文提示词。固定 200 字约束 + 中文
// 回答,要求模型在对话里直接自我介绍 —— 这是管理员肉眼判断"模型能不能用"
// 的最快方式(是否能流利中文 / 是否守规矩在 200 字内 / 是否声明了模型家族)。
const testProviderPrompt = "Hello，请用中文回答你什么模型？都支持什么功能？200字以内？"

// redactBearerKey 把完整的 API Key 脱敏为 "Bearer first8...last4" 形式,供诊断面板
// 展示。这样运维可以看到"用了某个 key 调用的",但完整 key 不会通过 HTTP 响应
// 回到浏览器 (避免 admin token 被浏览器缓存 / DevTools 截取)。
func redactBearerKey(plain string) string {
	plain = strings.TrimSpace(plain)
	if plain == "" {
		return "Bearer <empty>"
	}
	if plain == types.PlaceholderKey {
		return "Bearer <placeholder>"
	}
	if len(plain) <= 12 {
		return "Bearer " + plain
	}
	return "Bearer " + plain[:8] + "..." + plain[len(plain)-4:]
}

// runTestProviderChat 走 registry.Get → provider.Chat 一次。
// 超时预算与 provider 的 HTTP client 超时对齐(registry.ChatTimeout() =
// llm.timeout_ms,默认 600s)——不能再硬编码 15s:慢模型(Kimi/GLM/DeepSeek)
// 首字节就要 1-3 分钟,15s 的调用方 deadline 必然先触发,把"上游还在正常
// 生成"误报成 "context deadline exceeded",且 status=0 掩盖了真实原因。
// 任何错误都安全写入 chat_error,绝不 panic;返回 chat_ok。
//
// §134 增强:无论成功失败都把完整请求 / 响应信息写入 result
// (request_url / request_headers / request_body / response_status /
// response_headers / response_body),便于模型测试弹窗把"调用了什么、回了什么"
// 全量展示给运维(原版只给一行 chat_error,无法定位 placeholder / 401 / 400 /
// 网络层的真实原因)。request_headers 中的 Authorization 字段按
// "Bearer <first8>...<last4>" 脱敏,避免把完整 key 透回前端(API key
// hint 已经在 hint / api_key_hint 字段中提供)。
func (h *ModelAdminAPI) runTestProviderChat(parent context.Context, modelKey string, result gin.H) bool {
	if h.registry == nil {
		result["chat_error"] = "registry not wired"
		return false
	}

	// §134 — 即使 registry.Get 失败,也要把诊断字段填好(URL / headers / body),
	// 让运维在弹窗里看到"我打算调哪个 URL、用什么 Key、发了什么 Body",而不是只看
	// 到一行 chat_error 才能反推。
	// §20260814-01 — 按协议分叉诊断面板的出站 URL / 请求头 / 请求体预览。
	// anthropic-messages → {ep}/v1/messages(带 anthropic-version 头);
	// openai-completions → {ep}/chat/completions(无 anthropic-version 头)。
	protocol := h.registryProtocolLocked(modelKey)
	var requestURL string
	headers := map[string]string{
		"Content-Type": "application/json",
		"Accept":       "application/json",
	}
	if protocol == types.ProviderTypeOpenAICompletions {
		endpoint := h.registry.EndpointFor(modelKey)
		if endpoint == "" {
			endpoint = "registry endpoint (unknown)"
		}
		requestURL = openai.ChatCompletionsURL(endpoint)
	} else {
		endpoint := h.registry.EndpointFor(modelKey)
		if endpoint == "" {
			endpoint = "registry endpoint (unknown)"
		}
		requestURL = strings.TrimRight(endpoint, "/") + "/v1/messages"
		headers["anthropic-version"] = "2023-06-01"
	}
	// 占位 key(registry 未拿到) → 显示 Bearer <placeholder> 让运维一眼看出原因。
	authPreview := redactBearerKey("")
	headers["Authorization"] = authPreview
	headers["x-model-key"] = modelKey
	result["request_url"] = requestURL
	result["request_headers"] = headers
	result["request_body"] = h.testProviderRequestBody(protocol, modelKey)
	result["response_status"] = 0
	result["response_headers"] = map[string]string{}
	result["response_body"] = ""

	provider, key, err := h.registry.Get(modelKey)
	if err != nil {
		result["chat_error"] = "registry.Get: " + err.Error()
		// 占位 key 场景重写 Authorization 字段,显示 registry 实际状态。
		errHeaders := map[string]string{}
		for k, v := range headers {
			errHeaders[k] = v
		}
		errHeaders["Authorization"] = redactBearerKey(key)
		errHeaders["x-registry-error"] = err.Error()
		result["request_headers"] = errHeaders
		return false
	}
	// Get 成功 → 用真实 key 重新渲染 Authorization 头(脱敏)。
	gotHeaders := map[string]string{}
	for k, v := range headers {
		gotHeaders[k] = v
	}
	gotHeaders["Authorization"] = redactBearerKey(key)
	result["request_headers"] = gotHeaders

	req := llm.LLMRequest{
		Model:     modelKey,
		MaxTokens: 512,
		Messages: []llm.Message{
			{
				Role: "user",
				Content: []llm.ContentBlock{
					{Type: "text", Text: testProviderPrompt},
				},
			},
		},
	}

	// 超时预算与 provider HTTP client 对齐(llm.timeout_ms,默认 600s);
	// 拿不到(非 anthropic provider / registry 异常)时回退到 120s,仍比
	// 旧硬编码 15s 宽松一个数量级,避免慢模型被误杀。
	timeout := h.registry.ChatTimeout()
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	start := time.Now()
	resp, err := provider.Chat(ctx, key, req)
	latency := time.Since(start)
	result["chat_latency_ms"] = latency.Milliseconds()
	// chat_error 路径下,err 已是结构化 *anthropic.Error (含 HTTPStatus / Retryable / Source);
	// 我们把这些字段拆出来,前端可以分开展示。同时填充 response_* 占位便于折叠面板渲染。
	if err != nil {
		result["chat_error"] = err.Error()
		if ae, ok := err.(*anthropic.Error); ok && ae != nil {
			result["response_status"] = ae.HTTPStatus
			result["response_headers"] = map[string]string{"X-Error-Source": ae.Source}
			if ae.Message != "" {
				result["response_body"] = ae.Message
			} else {
				result["response_body"] = ""
			}
		} else {
			// 网络层 / 解码层错误,无 status / headers。
			result["response_status"] = 0
			result["response_headers"] = map[string]string{}
			result["response_body"] = err.Error()
		}
		logger.L().Warn("model_admin TestProvider Chat failed",
			zap.String("model", modelKey),
			zap.Error(err),
			zap.Int64("latency_ms", latency.Milliseconds()),
		)
		return false
	}

	text := strings.TrimSpace(resp.Text())
	result["chat_text"] = text
	result["chat_id"] = resp.ID
	result["chat_stop_reason"] = resp.StopReason
	result["chat_usage_input_tokens"] = resp.Usage.InputTokens
	result["chat_usage_output_tokens"] = resp.Usage.OutputTokens
	// §134 增强:成功路径下,Anthropic chat endpoint 是 200。响应 body 重新渲染为 JSON
	// 给前端展示。resp.Content 是一组 ContentBlock;序列化为可见 JSON。
	result["response_status"] = 200
	xProvider := "anthropic"
	if protocol == types.ProviderTypeOpenAICompletions {
		xProvider = "openai"
	}
	result["response_headers"] = map[string]string{
		"Content-Type": "application/json",
		"X-Provider":   xProvider,
	}
	if bodyJSON, jerr := json.MarshalIndent(map[string]any{
		"id":           resp.ID,
		"model":        resp.Model,
		"stop_reason":  resp.StopReason,
		"content":      resp.Content,
		"input_tokens": resp.Usage.InputTokens,
		"output_tokens": resp.Usage.OutputTokens,
	}, "", "  "); jerr == nil {
		result["response_body"] = string(bodyJSON)
	} else {
		result["response_body"] = "marshal failed: " + jerr.Error()
	}

	logger.L().Info("model_admin TestProvider Chat OK",
		zap.String("model", modelKey),
		zap.Int64("latency_ms", latency.Milliseconds()),
		zap.Int("input_tokens", resp.Usage.InputTokens),
		zap.Int("output_tokens", resp.Usage.OutputTokens),
		zap.String("stop_reason", resp.StopReason),
	)
	return true
}

// registryProtocolLocked returns the normalized protocol for a model by reading
// the registry's ModelInfo (which is already normalized by the read path). It
// falls back to anthropic-messages when the model is unknown, preserving the
// historical diagnostic behavior.
func (h *ModelAdminAPI) registryProtocolLocked(modelKey string) string {
	if h.registry == nil {
		return types.ProviderTypeAnthropicMessages
	}
	if info, ok := h.registry.GetInfo(modelKey); ok {
		return info.ProviderType
	}
	return types.ProviderTypeAnthropicMessages
}

// testProviderRequestBody renders the outbound request body preview according to
// the protocol — anthropic-messages (ContentBlock array) vs openai-completions
// (string content) — so the admin test dialog shows exactly what the proxy sees.
func (h *ModelAdminAPI) testProviderRequestBody(protocol, modelKey string) string {
	if protocol == types.ProviderTypeOpenAICompletions {
		return fmt.Sprintf(
			"{\n  \"model\": %q,\n  \"max_tokens\": 512,\n  \"stream\": false,\n  \"messages\": [{\"role\":\"user\",\"content\":%q}]\n}",
			modelKey, testProviderPrompt,
		)
	}
	return fmt.Sprintf(
		"{\n  \"model\": %q,\n  \"max_tokens\": 512,\n  \"stream\": false,\n  \"messages\": [\n    {\"role\":\"user\",\"content\":[{\"type\":\"text\",\"text\":%q}]}\n  ]\n}",
		modelKey, testProviderPrompt,
	)
}

// ─────────────────── ReloadProviders ───────────────────

// ReloadProviders POST /api/admin/llm/providers/reload (super admin only).
//
// Calls Registry.Reload(ctx) so subsequent LLM calls pick up the latest
// t_lsm_game_llm_provider rows. Returns the registry's Source tag so the
// caller can verify the state came from "db" (vs. "config-only" fallback).
func (h *ModelAdminAPI) ReloadProviders(c *gin.Context) {
	uid, ok := h.requireSuper(c)
	if !ok {
		return
	}
	if h.registry == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrInternal, "message": "registry not wired",
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	if err := h.registry.Reload(ctx); err != nil {
		logger.L().Error("admin reload llm registry failed",
			zap.String("admin_id", uid), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrInternal, "message": "reload failed: " + err.Error(),
		})
		return
	}

	logger.L().Info("admin reloaded llm registry",
		zap.String("admin_id", uid),
		zap.String("source", h.registry.Source()),
		zap.Int("providers", len(h.registry.List())))

	c.JSON(http.StatusOK, gin.H{
		"code": errcode.OK, "message": "ok",
		"data": gin.H{
			"reloaded": len(h.registry.List()),
			"source":   h.registry.Source(),
			"usable":   h.registry.Count(),
		},
	})
}

// ─────────────────── helpers ───────────────────

// decodeJSONStrict wraps json.Decoder + DisallowUnknownFields so every
// handler gets the same 400 envelope on a typo'd field. Mirrors
// room_api.go:75-86 (the established pattern in this codebase).
//
// Returns true on success; on failure the helper has already written the
// 400 response so the caller just returns.
func decodeJSONStrict(c *gin.Context, dst any) bool {
	if c.Request.Body == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": errcode.ErrValidationFailed, "message": "missing request body",
		})
		return false
	}
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    errcode.ErrValidationFailed,
			"message": "invalid body: " + err.Error(),
		})
		return false
	}
	// Reject trailing garbage so two JSON objects concatenated in one body
	// don't silently drop the second one (rare but observed in unit tests).
	if dec.More() {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": errcode.ErrValidationFailed, "message": "trailing data after JSON body",
		})
		return false
	}
	return true
}

// isMySQLDuplicateErr matches MySQL/MariaDB Error 1062 (duplicate key) so we
// can return a clean 400 instead of a generic 500 when an admin tries to
// create a provider whose (agent_name, model) already exists.
func isMySQLDuplicateErr(err error) bool {
	if err == nil {
		return false
	}
	type mysqlErr interface {
		Error() string
	}
	if me, ok := err.(mysqlErr); ok {
		return strings.Contains(me.Error(), "Error 1062") || strings.Contains(me.Error(), "Duplicate entry")
	}
	return false
}

// registrySource returns "" when registry is nil (so the JSON omits it).
func registrySource(r *llm.Registry) string {
	if r == nil {
		return ""
	}
	return r.Source()
}

// registryDefaultEndpoint returns the shared (registry-global) LLM proxy
// endpoint. Per-row `t_lsm_game_llm_provider.endpoint` overrides this.
//
// §133 — 让 admin 管理页能区分「DB 单条覆盖」与「回退全局默认值」,避免
// 空 DB 行被误读为「这条 provider 没配 endpoint」(实际仍走全局默认)。
func registryDefaultEndpoint(r *llm.Registry) string {
	if r == nil {
		return ""
	}
	return r.Endpoint()
}
