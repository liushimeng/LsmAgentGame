// Package llm — Registry: builds a model-key → provider map.
//
// Loading order (post "模型管理 + 模型玩家持久化" refactor, kind-skipping-moth):
//
//  1. DB-first. If gormDB != nil and t_lsm_game_llm_provider has rows, load
//     them all from DB and decrypt each API key with the master AES key in
//     t_lsm_game_kv (util.EnsureMasterKey / util.DecryptAPIKey).
//  2. DB-empty + cfg has providers → auto-seed. Walk cfg.LLM.Providers, encrypt
//     each api_key, insert into t_lsm_game_llm_provider + register a bot user
//     via the BotUserProvisioner (typically service.BotUserService).
//  3. DB has rows + cfg has providers → DB wins. cfg.LLM.Providers is logged
//     as deprecated; operators are expected to migrate their conf edits into
//     the new admin UI (Phase 5).
//
// gormDB == nil (the common unit-test path) ⇒ pure cfg mode, identical to the
// pre-refactor behavior. All callers in agent/ that pass nil are unaffected.
package llm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"LsmWebGame/config"
	"LsmWebGame/llm/anthropic"
	types "LsmWebGame/llm/types"
	"LsmWebGame/logger"
	"LsmWebGame/models"
	"LsmWebGame/util"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// BotUserProvisioner is the narrow contract the registry needs to provision
// a backing bot user for each seeded LLM provider. Implemented by
// service.BotUserService; declared as an interface here so the llm package
// doesn't grow a service import dependency. main.go wires the real impl.
//
// The signature mirrors service.BotUserService.EnsureBotUserForProvider
// exactly so the wiring is a one-liner. The returned bot user pointer is
// only used for logging — callers that need the typed *models.TLsmGameUser
// can query the DB directly.
type BotUserProvisioner interface {
	EnsureBotUserForProvider(ctx context.Context, p *models.TLsmGameLlmProvider) (any, error)
}

// noopBotUserProvisioner is the default used when main.go hasn't wired the
// real service (test path, or DB-empty + no cfg).
type noopBotUserProvisioner struct{}

func (noopBotUserProvisioner) EnsureBotUserForProvider(context.Context, *models.TLsmGameLlmProvider) (any, error) {
	return nil, nil
}

type registeredProvider struct {
	info      types.ModelInfo
	key       string
	provider  types.LLMProvider
	available bool
	enabled   bool
	endpoint  string // per-provider override; "" ⇒ use the shared one
	// §R224 (2026-08-01) — 重新引入 §128 误删的 extended thinking 配置,
	// 由 per-provider DB 行(t_lsm_game_llm_provider.thinking_enabled /
	// thinking_budget_tokens) 或 cfg.LLM.Providers[].ThinkingRequired/Budget
	// 决定是否在每条 message 头注入 `{type:"thinking", budget:N}` 块。
	// agent.callProvider 通过 Registry.GetThinkingEnabled(modelKey) 查询。
	thinkingEnabled bool
	thinkingBudget  int
}

// Registry holds the set of configured models. Safe for concurrent use.
//
// Construction modes:
//
//   - NewRegistry(cfg)         — pure-cfg fallback for tests (kept for back-compat).
//   - NewRegistryWithDB(cfg, db) — DB-first with cfg seed-on-empty (production).
//
// After construction, use Reload(ctx) to refresh from DB at runtime (e.g.
// after an admin CRUDs a row via the upcoming /api/admin/llm/* handlers).
type Registry struct {
	mu        sync.RWMutex
	providers map[string]registeredProvider // key = model (e.g. "MeiTuan-model")
	endpoint  string                       // shared endpoint; per-provider override takes precedence
	// endpoints (BUG-R220) is the failover list. Always non-empty after
	// construction; populated from cfg.Endpoints (with cfg.Endpoint folded
	// in if Endpoints is empty). Kept here so /api/llm/health can render
	// the full failover chain for operators.
	endpoints []string
	lastErr   string    // last health check error, "" if healthy
	lastCheck time.Time // when the last health check ran
	// lastEndpointStatuses (BUG-R214) caches the per-endpoint probe result
	// from the most recent HealthCheck so Health() (the cheap cached read
	// behind /api/llm/health) can replay the whole failover chain's health
	// instead of only the aggregate boolean. Empty before the first probe.
	lastEndpointStatuses []EndpointHealth

	// sharedProvider is the single anthropic.Provider instance reused for
	// every registered model. Sharing keeps the User-Agent / billing-header
	// / stream-timeout configuration consistent across all models and avoids
	// allocating a new HTTP client per model.
	sharedProvider *anthropic.Provider

	// gormDB is retained (non-nil only in the production constructor) so
	// Reload can re-query the table at runtime. Pure-cfg mode leaves it nil.
	gormDB *gorm.DB
	// cfg is the original LLMConfig so Reload can detect "config still set"
	// and emit the deprecation warning when both DB and cfg have rows.
	cfg config.LLMConfig
	// masterKey caches the resolved AES-256 key after the first decrypt.
	masterKey atomic.Pointer[[]byte]
	// source describes where the current state came from; used in startup
	// logs only ("db" | "config-seed" | "config-only" | "empty").
	source string
	// botUsers provisions a backing bot user for each newly seeded LLM
	// provider. The default is a no-op so the registry is test-friendly;
	// main.go wires the real service.BotUserService.
	botUsers BotUserProvisioner
}

// HealthStatus reports the last known health-check result for the upstream
// LLM proxy. Used by /api/llm/health (admin-only) and by startup logs so
// operators see "401 / unreachable" before users hit it via a 7-agent room.
type HealthStatus struct {
	OK         bool             `json:"ok"`
	Endpoint   string           `json:"endpoint"`
	// Endpoints (BUG-R220) is the failover list, in priority order. The
	// first element is the primary that the active call will use; the
	// rest are tried in order on transport-level failure.
	Endpoints  []string         `json:"endpoints,omitempty"`
	LastError  string           `json:"last_error,omitempty"`
	LastCheck  time.Time        `json:"last_check"`
	UsableKeys int              `json:"usable_keys"`
	// Unusable lists configured models that cannot currently drive an agent
	// (placeholder / empty / invalid / disabled api_key), with the reason for
	// each. Empty when every configured model is usable. Backed by
	// UnusableProviders(); surfaced on GET /api/llm/health so operators and
	// the test harness can see exactly which models need a real key, instead
	// of inferring it from N quarantined agents mid-game (BUG-R115-01).
	Unusable []UnusableModel `json:"unusable"`

	// EndpointStatuses (BUG-R214) is the per-endpoint probe detail, one entry
	// per element of Endpoints and in the same order. **新增字段,不改动任何
	// 既有字段** —— 既有消费方(/api/llm/health badge、main.go 启动日志、
	// api/model_admin_api.go 的 HealthStatus 手工构造)全部按名取字段,新增
	// 一个 omitempty 字段对它们完全透明。
	//
	// 修复前 HealthCheck 只探测 legacy 标量 Endpoint,却把整个 Endpoints
	// 列表原样回显,备用端点从未被探测 —— operator 看到的是配置回显而不是
	// 健康事实。现在每个端点都真实探测,结果落在这里。
	EndpointStatuses []EndpointHealth `json:"endpoint_statuses,omitempty"`
}

// EndpointHealth is one endpoint's probe result inside HealthStatus.
// BUG-R214: 使 failover 链上的每一跳都可观测 —— 主端点绿、备用端点红这类
// "看着健康其实一半已死"的状态,不再需要等 13 个 bot 在房间里挂满 dial
// 超时才被发现。绝不携带 api_key(§5)。
type EndpointHealth struct {
	Endpoint string `json:"endpoint"`
	OK       bool   `json:"ok"`
	// StatusCode is the HEAD response code (0 when the dial itself failed).
	StatusCode int `json:"status_code,omitempty"`
	// LatencyMs 是本次探测的往返耗时(含失败路径的 dial 超时),供 operator
	// 区分"秒拒"(connection refused)与"慢到超时"(i/o timeout)。
	LatencyMs int64  `json:"latency_ms,omitempty"`
	LastError string `json:"last_error,omitempty"`
}

// NewRegistry constructs a Registry in pure-cfg mode (no DB). Retained for
// backward compatibility with the dozens of tests + main.go pre-refactor.
// Production should call NewRegistryWithDB(cfg, gormDB) instead so it can pick
// up admin-managed rows from t_lsm_game_llm_provider.
func NewRegistry(cfg config.LLMConfig) *Registry {
	r := newRegistryShared(cfg)
	r.source = "config-only"
	r.loadFromConfigLocked(cfg)
	return r
}

// NewRegistryWithDB is the production constructor. It loads from
// t_lsm_game_llm_provider if any rows exist; otherwise it seeds the table from
// cfg.LLM.Providers. Pure-cfg behavior (same as NewRegistry) is preserved when
// gormDB == nil so test code keeps working unchanged.
//
// botUsers (optional) registers a backing bot user for each provider that gets
// auto-seeded into the DB on first boot. nil ⇒ bot-user provisioning is
// skipped (used by tests + the rare case where main.go wants to do it later).
func NewRegistryWithDB(cfg config.LLMConfig, gormDB *gorm.DB, botUsers BotUserProvisioner) *Registry {
	r := newRegistryShared(cfg)
	r.gormDB = gormDB
	if botUsers == nil {
		r.botUsers = noopBotUserProvisioner{}
	} else {
		r.botUsers = botUsers
	}
	if gormDB == nil {
		// Test / standalone path — preserve old behavior exactly.
		r.source = "config-only"
		r.loadFromConfigLocked(cfg)
		logger.L().Info("llm registry loaded from config (no DB)",
			zap.Int("providers", len(r.providers)))
		return r
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var rows []models.TLsmGameLlmProvider
	if err := gormDB.WithContext(ctx).
		Order("created_at ASC").
		Find(&rows).Error; err != nil {
		// DB read failed → fall back to cfg so the server still boots. The
		// deprecation warning is not applicable here (we never even saw DB
		// state).
		logger.L().Warn("llm registry: DB read failed, falling back to config",
			zap.Error(err))
		r.source = "config-only"
		r.loadFromConfigLocked(cfg)
		return r
	}

	if len(rows) > 0 {
		// DB-wins path.
		if err := r.populateLocked(ctx, rows, r.providers); err != nil {
			// Decryption failure is fatal — refusing to boot is safer than
			// handing a half-broken registry to 7 LLM agents that would
			// instantly 401 the proxy with a bogus key.
			logger.L().Fatal("llm registry: load from DB failed",
				zap.Error(err))
		}
		r.source = "db"
		if len(cfg.Providers) > 0 {
			logger.L().Warn("llm registry: deprecated config_deprecated path — cfg.LLM.Providers ignored, DB rows win",
				zap.Int("cfg_count", len(cfg.Providers)),
				zap.Int("db_count", len(r.providers)))
		} else {
			logger.L().Info("llm registry loaded from DB",
				zap.Int("providers", len(r.providers)))
		}
		return r
	}

	// DB-empty path: auto-seed from cfg.LLM.Providers.
	if len(cfg.Providers) == 0 {
		// Nothing on either side. Return an empty registry so /api/llm/models
		// returns [] and the rest of the app runs without LLM deps.
		r.source = "empty"
		logger.L().Info("llm registry: DB empty + cfg empty — no providers")
		return r
	}
	if err := r.seedFromConfigLocked(ctx, gormDB, cfg); err != nil {
		logger.L().Fatal("llm registry: seed from config failed",
			zap.Error(err))
	}
	r.source = "config-seed"
	logger.L().Info("llm registry seeded from config",
		zap.Int("providers", len(r.providers)))
	return r
}

// newRegistryShared builds the shared scaffolding both constructors need:
// the shared anthropic.Provider, the timeout / stream-timeout knobs, and the
// empty providers map. Must be called under no lock.
func newRegistryShared(cfg config.LLMConfig) *Registry {
	r := &Registry{providers: make(map[string]registeredProvider)}
	r.endpoint = strings.TrimSpace(cfg.Endpoint)
	// BUG-R220 — fold the legacy Endpoint into the Endpoints list when
	// callers only filled Endpoint (the common case for unit tests that
	// skip config.Load). production path goes through applyDefaults which
	// does the same; both code paths converge to the same Provider state.
	endpoints := cfg.Endpoints
	if len(endpoints) == 0 && r.endpoint != "" {
		endpoints = []string{r.endpoint}
	}
	r.endpoints = make([]string, len(endpoints))
	copy(r.endpoints, endpoints)
	timeout := time.Duration(cfg.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	shared := anthropic.New(endpoints, timeout, cfg.MaxRetries)
	streamIdle := time.Duration(cfg.StreamIdleTimeoutMs) * time.Millisecond
	if streamIdle <= 0 {
		// 2026-07-24 优化: 120s → 300s(5 min)。慢模型(Kimi/GLM)首字/间隔
		// 延时 2 分钟以上是预期场景;只要流持续就不中断。
		streamIdle = 300 * time.Second
	}
	// §130: `streamIdle` is now the NO-FIRST-BYTE timeout (2 min default). The
	// second arg (total) is deprecated and ignored by doStream — we pass the
	// HTTP timeout only for signature compatibility. SetStreamTimeouts also
	// forces the post-first-byte idle guard to 0, so a live SSE response is
	// never aborted once the upstream has started streaming.
	streamTotal := timeout
	shared.SetStreamTimeouts(streamIdle, streamTotal)
	r.sharedProvider = shared
	r.cfg = cfg
	return r
}

// loadFromConfigLocked populates providers from cfg.Providers. Caller MUST
// hold r.mu for writing (or invoke before returning to caller).
func (r *Registry) loadFromConfigLocked(cfg config.LLMConfig) {
	shared := r.sharedProvider
	for _, p := range cfg.Providers {
		// R187-2: sanitize so a config key pasted with invisible Cf runes
		// stays addressable by its clean ASCII form.
		model := util.SanitizeModelKey(p.Model)
		if model == "" {
			continue
		}
		key := strings.TrimSpace(p.APIKey)
		available := usableKey(key)
		// §R224 (2026-08-01) — 从 cfg.LLM.Providers[].ThinkingRequired/Budget
		// 读取;若未设置(零值)则默认 false(向后兼容 §128 后的配置)。
		// LsmWebGame.conf.example 已给全部 8 家代理打开(实测 100% 失败时 100%
		// 报 messages.content.thinking missing)。
		r.providers[model] = registeredProvider{
			info: types.ModelInfo{
				AgentName:    strings.TrimSpace(p.AgentName),
				Model:        model,
				ProviderType: strings.TrimSpace(p.ProviderType),
			},
			key:             key,
			provider:        shared,
			available:       available,
			enabled:         true,
			thinkingEnabled: p.ThinkingRequired,
			thinkingBudget:  p.ThinkingBudget,
		}
	}
}

// seedFromConfigLocked is the DB-empty fallback. It runs in a single GORM
// transaction: if any provider insert fails, every prior insert is rolled back
// so DB state stays consistent. Returns the first error encountered; main.go
// treats seed failure as fatal and refuses to boot with a half-populated DB.
func (r *Registry) seedFromConfigLocked(ctx context.Context, gormDB *gorm.DB, cfg config.LLMConfig) error {
	// Encrypt all api_keys BEFORE the transaction so a transient key-gen /
	// encryption failure does not waste a DB write that we'd have to roll back.
	encrypted := make([]models.TLsmGameLlmProvider, 0, len(cfg.Providers))
	for _, p := range cfg.Providers {
		model := util.SanitizeModelKey(p.Model)
		if model == "" {
			continue
		}
		plain := strings.TrimSpace(p.APIKey)
		var enc string
		if plain != "" && plain != types.PlaceholderKey {
			e, err := util.EncryptAPIKey(ctx, gormDB, plain)
			if err != nil {
				return fmt.Errorf("encrypt api_key for %q: %w", model, err)
			}
			enc = e
		}
		providerType := strings.TrimSpace(p.ProviderType)
		if providerType == "" {
			providerType = "anthropic"
		}
		row := models.TLsmGameLlmProvider{
			ID:               util.NewUUID(),
			AgentName:        strings.TrimSpace(p.AgentName),
			Model:            model,
			ProviderType:     providerType,
			APIKeyEnc:  enc,
			APIKeyHint: apiKeyHint(plain),
			Endpoint:   "",
			// §R224 (2026-08-01) — 重新引入 thinking 配置字段。从 cfg 同步过去:
			// §128 误删后,旧 DB 行的两列均为零值(false / 0);admin 可通过
			// PUT /api/admin/llm/providers/:id 在线开启。
			ThinkingEnabled:      p.ThinkingRequired,
			ThinkingBudgetTokens: p.ThinkingBudget,
			Enabled:          true,
			Remark:           "seeded from LsmWebGame.conf on first boot",
		}
		encrypted = append(encrypted, row)
	}

	if err := gormDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for i := range encrypted {
			if err := tx.Create(&encrypted[i]).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("insert providers: %w", err)
	}

	// All inserts committed. Mirror into the in-memory providers map first so
	// the registry is immediately usable, then provision bot users. A bot-user
	// failure is logged but does NOT roll back the LLM-provider inserts — the
	// 7-agent room creation path tolerates a missing bot user (the seat is
	// allocated on first use), whereas a missing provider would 401 every
	// LLM call instantly.
	r.populateLocked(ctx, encrypted, r.providers)
	if r.botUsers != nil {
		for i := range encrypted {
			if _, err := r.botUsers.EnsureBotUserForProvider(ctx, &encrypted[i]); err != nil {
				logger.L().Warn("llm registry: bot user provision failed",
					zap.String("model", encrypted[i].Model),
					zap.Error(err))
			}
		}
	}
	return nil
}

// isLikelyInvalidKey catches keys that look like non-secret strings
// accidentally pasted into the api_key field (e.g. endpoint URLs or other
// non-random values). It is intentionally conservative: keys shorter than 32
// bytes and any URL-shaped key are considered unusable (BUG-R116-02).
//
// Production keys (e.g. "sk-real...") and short test keys used in unit tests
// (e.g. "sk-real", "sk-x") are treated as valid because they do not look like
// URLs and tests intentionally use short synthetic keys.
func isLikelyInvalidKey(key string) bool {
	if len(key) < 32 {
		return false
	}
	lower := strings.ToLower(key)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return true
	}
	if strings.Contains(lower, "/anthropic") {
		return true
	}
	return false
}

// usableKey reports whether a decrypted API key can plausibly drive an agent.
func usableKey(plain string) bool {
	plain = strings.TrimSpace(plain)
	return plain != "" && plain != types.PlaceholderKey && !isLikelyInvalidKey(plain)
}
// t_lsm_game_llm_provider.api_key_hint. "sk-XXXX...YYYY" — first 4 + last 4
// chars, dashes kept. Empty / placeholder → "".
func apiKeyHint(plain string) string {
	plain = strings.TrimSpace(plain)
	if plain == "" || plain == types.PlaceholderKey {
		return ""
	}
	if len(plain) <= 8 {
		return plain
	}
	return plain[:4] + "..." + plain[len(plain)-4:]
}

// Get resolves a model key into its provider + per-model API key. It errors if
// the model is unknown, disabled, OR its key is still the placeholder —
// callers can surface a clean 400 instead of a confusing upstream 401.
//
// When the DB row carries its own endpoint (the common production case — each
// model may be fronted by a different proxy) Get lazily builds a per-endpoint
// anthropic.Provider that dials THAT URL instead of the shared one built from
// the global llm.endpoint(s). Previously rp.endpoint was stored but never
// wired into the returned provider, so a DB row pointing at a healthy local
// proxy silently kept calling the (possibly dead) global primary and admins
// saw "config looks right but every chat call fails".
func (r *Registry) Get(modelKey string) (types.LLMProvider, string, error) {
	r.mu.RLock()
	rp, ok := r.providers[modelKey]
	if !ok {
		r.mu.RUnlock()
		return nil, "", fmt.Errorf("llm: unknown model %q", modelKey)
	}
	if !rp.enabled {
		r.mu.RUnlock()
		return nil, "", fmt.Errorf("llm: model %q is disabled", modelKey)
	}
	if !rp.available {
		r.mu.RUnlock()
		return nil, "", fmt.Errorf("llm: model %q has no usable api_key (placeholder left in config)", modelKey)
	}
	// Fast path: no per-row endpoint, or an override provider already built.
	if rp.endpoint == "" || rp.endpoint == r.endpoint || rp.provider != nil {
		r.mu.RUnlock()
		return rp.provider, rp.key, nil
	}
	r.mu.RUnlock()

	// Slow path: build (once) a provider pinned to the row's endpoint. We
	// escalate to the write lock; another concurrent Get may have done the
	// same upgrade in the meantime, so re-check before allocating.
	r.mu.Lock()
	defer r.mu.Unlock()
	rp, ok = r.providers[modelKey]
	if !ok {
		return nil, "", fmt.Errorf("llm: unknown model %q", modelKey)
	}
	if rp.provider == nil {
		rp.provider = r.newEndpointProviderLocked(rp.endpoint)
		r.providers[modelKey] = rp
	}
	return rp.provider, rp.key, nil
}

// newEndpointProviderLocked constructs an anthropic.Provider pinned to a
// single base URL, inheriting the registry's timeout / retry / UA / billing
// configuration. Caller MUST hold r.mu.
func (r *Registry) newEndpointProviderLocked(endpoint string) types.LLMProvider {
	timeout := time.Duration(r.cfg.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	p := anthropic.New([]string{endpoint}, timeout, r.cfg.MaxRetries)
	streamIdle := time.Duration(r.cfg.StreamIdleTimeoutMs) * time.Millisecond
	if streamIdle <= 0 {
		streamIdle = 300 * time.Second
	}
	p.SetStreamTimeouts(streamIdle, timeout)
	// Inherit the operator-facing headers from the shared provider so admin
	// diagnostics (User-Agent / billing header) stay consistent across
	// per-endpoint providers.
	if r.sharedProvider != nil {
		if ua := r.sharedProvider.UserAgent(); ua != "" {
			p.SetUserAgent(ua)
		}
		if bh := r.sharedProvider.BillingHeader(); bh != "" {
			p.SetBillingHeader(bh)
		}
	}
	return p
}

// IsAvailable reports whether a model key is configured with a real api_key
// AND enabled.
func (r *Registry) IsAvailable(modelKey string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rp, ok := r.providers[modelKey]
	return ok && rp.available && rp.enabled
}

// GetInfo returns the ModelInfo metadata for a given model key.
// Returns (info, true) if the key exists, (zero, false) otherwise.
func (r *Registry) GetInfo(modelKey string) (types.ModelInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rp, ok := r.providers[modelKey]
	if !ok {
		return types.ModelInfo{}, false
	}
	return rp.info, true
}

// List returns key-free metadata for every configured model (including
// placeholder ones and disabled ones), in insertion order. Safe for the
// /api/llm/models handler to return directly. Use ListEnabled if the caller
// wants to filter out disabled rows.
func (r *Registry) List() []types.ModelInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]types.ModelInfo, 0, len(r.providers))
	for _, rp := range r.providers {
		out = append(out, rp.info)
	}
	return out
}

// ListEnabled is the same as List but skips disabled rows. The front-end
// model picker uses this so operators can hide a model without deleting it.
func (r *Registry) ListEnabled() []types.ModelInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]types.ModelInfo, 0, len(r.providers))
	for _, rp := range r.providers {
		if rp.enabled {
			out = append(out, rp.info)
		}
	}
	return out
}

// Count returns the number of usable (non-placeholder AND enabled) providers.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	n := 0
	for _, rp := range r.providers {
		if rp.available && rp.enabled {
			n++
		}
	}
	return n
}

// UnusableModel records a configured model that cannot currently drive an
// agent — either its api_key was never replaced (still the literal
// PlaceholderKey sentinel or empty) or the provider was deliberately
// disabled. The Reason is one of "placeholder", "empty_key", or "disabled".
type UnusableModel struct {
	Model  string `json:"model"`
	Reason string `json:"reason"`
}

// UnusableProviders returns every configured model that is NOT currently
// usable (available==false), in deterministic (sorted) order. It is the
// observability surface behind the startup placeholder-detect warning
// (BUG-R115-01): rather than letting N/13 agents get silently quarantined
// mid-game with a confusing upstream 401, the operator sees the exact list of
// unconfigured models at boot and a pointer to the fix docs.
func (r *Registry) UnusableProviders() []UnusableModel {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]UnusableModel, 0, len(r.providers))
	for model, rp := range r.providers {
		if rp.available {
			continue
		}
		reason := "disabled"
		if rp.enabled {
			// Both loaders (cfg + DB-wins) compute
			// `available = key != "" && key != PlaceholderKey`, so a
			// non-empty, non-sentinel key is always available. The only
			// enabled-but-unusable classes are therefore the literal
			// placeholder sentinel and the empty string.
			if rp.key == types.PlaceholderKey {
				reason = "placeholder"
			} else if strings.TrimSpace(rp.key) == "" {
				reason = "empty_key"
			} else {
				reason = "invalid_key"
			}
		}
		out = append(out, UnusableModel{Model: model, Reason: reason})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Model < out[j].Model })
	return out
}

// Endpoint returns the shared (registry-global) LLM proxy endpoint that
// per-provider `t_lsm_game_llm_provider.endpoint` rows override. Returns ""
// when the registry is nil or no endpoint has been configured.
//
// §133 — admin 管理页需要把"实际生效"的 endpoint 暴露出去,因为数据库行
// 可能为空但实际仍走全局默认,让管理员误以为「这条 provider 没配 endpoint」。
func (r *Registry) Endpoint() string {
	if r == nil {
		return ""
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.endpoint
}

// Endpoints (BUG-R220) returns the failover endpoint list in priority
// order. The first element is the primary the next call will use; the
// rest are tried in order on transport-level failure. Returns nil when
// the registry is nil.
func (r *Registry) Endpoints() []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.endpoints) == 0 {
		// Legacy / pure-cfg fallback — synthesize a single-element slice so
		// callers always get a non-nil result.
		return []string{r.endpoint}
	}
	out := make([]string, len(r.endpoints))
	copy(out, r.endpoints)
	return out
}

// EndpointFor returns the per-model "effective" endpoint — the one that
// outbound LLM calls for that model would actually use. Order of precedence:
// per-provider DB-row endpoint override → registry global endpoint. Returns ""
// when the registry is nil OR the model is unknown OR no endpoint has been
// configured at any layer. §134 — model test diagnostic surfaces this string
// in request_url so operators can verify "which URL the test call hit".
func (r *Registry) EndpointFor(modelKey string) string {
	if r == nil {
		return ""
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if rp, ok := r.providers[modelKey]; ok {
		if rp.endpoint != "" {
			return rp.endpoint
		}
	}
	return r.endpoint
}

// SetUserAgent propagates a User-Agent string to the shared anthropic Provider
// so every outbound LLM request carries it. Format expected by the caller:
// "ProgramName/Version BuildTime".
func (r *Registry) SetUserAgent(ua string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, rp := range r.providers {
		if p, ok := rp.provider.(*anthropic.Provider); ok {
			p.SetUserAgent(ua)
		}
	}
}

// SetBillingHeader propagates an `x-anthropic-billing-header` value to the
// shared anthropic Provider.
func (r *Registry) SetBillingHeader(bh string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, rp := range r.providers {
		if p, ok := rp.provider.(*anthropic.Provider); ok {
			p.SetBillingHeader(bh)
		}
	}
}

// ChatTimeout returns the non-stream overall chat timeout the shared provider
// was constructed with (llm.timeout_ms; 600s default). Exposed so callers that
// wrap a chat call in their own context deadline (e.g. the admin "model test"
// endpoint) can align their budget with the provider's HTTP client timeout
// instead of hard-coding a shorter one — a shorter caller deadline always
// fires first and surfaces as a misleading "context deadline exceeded" while
// the upstream is still happily generating. Returns 0 when unset.
func (r *Registry) ChatTimeout() time.Duration {
	if r == nil || r.sharedProvider == nil {
		return 0
	}
	return r.sharedProvider.ChatTimeout()
}

// SetStreamTimeouts configures the idle and total stream timeouts on every
// registered anthropic provider. Both values may be 0 (the provider default
// applies).
func (r *Registry) SetStreamTimeouts(idle, total time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, rp := range r.providers {
		if p, ok := rp.provider.(*anthropic.Provider); ok {
			p.SetStreamTimeouts(idle, total)
		}
	}
}

// §R224 (2026-08-01) — 重新引入 GetThinkingEnabled(modelKey) 公共方法,
// 替代 §128 误删的 ThinkingFor 方法。详见 BUG-NEW-1 (20260801_124553) §3.1。
//
// 当 modelKey 未在 registry 中注册(空 placeholder / 未知 model_key)时返回
// (false, 0);agent.callProvider 看到 false 时不会注入 thinking 块,避免
// 对未注册模型产生意外的请求体污染。模型存在但 thinkingEnabled=false 时
// 也返回 (false, budget) — budget 仍可被调用方用作下游逻辑的"默认值",
// 但 enabled=false 已足以让 provider 跳过注入。
func (r *Registry) GetThinkingEnabled(modelKey string) (enabled bool, budget int) {
	if r == nil {
		return false, 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	rp, ok := r.providers[modelKey]
	if !ok {
		return false, 0
	}
	return rp.thinkingEnabled, rp.thinkingBudget
}

// Reload refreshes the in-memory provider map from t_lsm_game_llm_provider.
// Acquires the write lock for the duration of the rebuild so Get() callers
// either see the old set or the new set, never a half-mutated map. The shared
// anthropic.Provider instance is preserved — only the per-model API key /
// metadata mapping changes — so in-flight LLM calls finish uninterrupted.
//
// Returns an error if gormDB is nil (pure-cfg mode has nothing to reload from)
// or if the DB read / decrypt step fails; on error the previous map is kept
// untouched.
func (r *Registry) Reload(ctx context.Context) error {
	if r.gormDB == nil {
		return errors.New("llm: Reload unavailable in pure-cfg mode")
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	// Force the master-key cache to be re-fetched on the next decrypt. This
	// is safe even if EnsureMasterKey itself doesn't actually rotate — the
	// cache is process-local and is invalidated for correctness symmetry only.
	r.masterKey.Store(nil)

	var rows []models.TLsmGameLlmProvider
	if err := r.gormDB.WithContext(ctx).
		Order("created_at ASC").
		Find(&rows).Error; err != nil {
		return fmt.Errorf("llm: reload: read providers: %w", err)
	}
	if len(rows) == 0 {
		// DB was emptied by an admin. Preserve the in-memory state and warn;
		// a hard wipe from Reload would surprise every 7-agent room in flight.
		logger.L().Warn("llm registry Reload: DB is empty — keeping previous in-memory state")
		return nil
	}

	newMap := make(map[string]registeredProvider, len(rows))
	if err := r.populateLocked(ctx, rows, newMap); err != nil {
		return err
	}
	r.providers = newMap
	r.source = "db"
	logger.L().Info("llm registry reloaded from DB",
		zap.Int("providers", len(newMap)))
	return nil
}

// SyncFromConfig re-seeds the DB from cfg.LLM.Providers. Only intended for the
// "DB had no rows" bootstrap path; calling this on a non-empty DB will create
// duplicate provider rows (the unique index on model will reject them with
// a DB error).
func (r *Registry) SyncFromConfig(ctx context.Context, cfg config.LLMConfig) error {
	if r.gormDB == nil {
		return errors.New("llm: SyncFromConfig unavailable in pure-cfg mode")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.seedFromConfigLocked(ctx, r.gormDB, cfg); err != nil {
		return err
	}
	r.source = "config-seed"
	return nil
}

// populateLocked is the shared inner loop used by both the initial load from
// DB rows and the Reload refresh. Caller MUST hold r.mu for writing; the
// destination map is filled in place — initial load passes r.providers
// directly, Reload passes a fresh map and atomically swaps it in.
func (r *Registry) populateLocked(ctx context.Context, rows []models.TLsmGameLlmProvider, dst map[string]registeredProvider) error {
	shared := r.sharedProvider
	mk, err := r.ensureMasterKey(ctx)
	if err != nil {
		return fmt.Errorf("ensure master key: %w", err)
	}
	for _, row := range rows {
		// R187-2: sanitize on load so a dirty legacy row (e.g. a model column
		// containing an invisible ZWNJ pasted via the admin UI) becomes
		// addressable in-memory by its clean ASCII key. The DB row itself is
		// left untouched; the unique constraint on model is enforced at CRUD
		// time against the sanitized key.
		model := util.SanitizeModelKey(row.Model)
		if model == "" {
			continue
		}
		var plain string
		if row.APIKeyEnc != "" {
			plain, err = util.DecryptAPIKeyWithKey(row.APIKeyEnc, mk)
			if err != nil {
				return fmt.Errorf("decrypt api_key for %q: %w", model, err)
			}
		}
		available := usableKey(plain)
		providerType := strings.TrimSpace(row.ProviderType)
		if providerType == "" {
			providerType = "anthropic"
		}
		// Per-row endpoint: empty (or exactly the global default) ⇒ share the
		// global provider instance; anything else ⇒ leave provider nil and let
		// Get lazily pin a dedicated anthropic.Provider to that URL. Storing
		// the raw value keeps Get's fast/slow-path decision cheap.
		endpoint := strings.TrimSpace(row.Endpoint)
		var provider types.LLMProvider
		if endpoint == "" || endpoint == r.endpoint {
			endpoint = ""
			provider = shared
		}
		// BUG-R229-P0-01 (2026-08-01) — 存量 DB 行自愈: 早期 seed 的
		// thinking_enabled=0 行在 Reload 时被改写为 true / 4096,避免存量部署重启后
		// 仍 400 "missing messages.content.thinking parameter"。仅对 anthropic 协议
		// 生效(OpenAI 预留协议不注入 thinking,未来显式切到 OpenAI 才绕过)。
		thinkingEnabled := row.ThinkingEnabled
		thinkingBudget := row.ThinkingBudgetTokens
		if providerType == "anthropic" && !thinkingEnabled && thinkingBudget <= 0 {
			thinkingEnabled = true
			thinkingBudget = 4096
		}
		dst[model] = registeredProvider{
			info: types.ModelInfo{
				AgentName:    strings.TrimSpace(row.AgentName),
				Model:        model,
				ProviderType: providerType,
			},
			key:       plain,
			provider:  provider,
			available: available,
			enabled:   row.Enabled,
			endpoint:  endpoint,
			// §R224 (2026-08-01) — DB 行(t_lsm_game_llm_provider)的两列字段。
			// thinking_enabled=false 时 anthropic.Provider 不会注入 thinking
			// 块;true 时用 thinking_budget_tokens(默认 4096)作 budget。
			thinkingEnabled: thinkingEnabled,
			thinkingBudget:  thinkingBudget,
		}
	}
	return nil
}

// ensureMasterKey returns the cached master AES key (or fetches it on first
// use). The cache is process-local; manual rotation requires invalidating
// the cache by calling Reload.
func (r *Registry) ensureMasterKey(ctx context.Context) ([]byte, error) {
	if cached := r.masterKey.Load(); cached != nil {
		out := make([]byte, len(*cached))
		copy(out, *cached)
		return out, nil
	}
	mk, err := util.EnsureMasterKey(ctx, r.gormDB)
	if err != nil {
		return nil, err
	}
	r.masterKey.Store(&mk)
	return mk, nil
}

// Source returns a short tag describing where the current state came from.
// Used by startup logs only ("db" / "config-seed" / "config-only" / "empty").
func (r *Registry) Source() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.source
}

// HealthCheck performs a lightweight HTTP HEAD against EVERY configured LLM
// proxy endpoint to confirm network reachability. It does NOT validate any
// API key — that's per-provider and surfaced as ErrLLMUnavailable on
// CreateRoomWithAgents. A failed probe is recorded as the registry's last
// health error and exposed via Health(); a successful probe clears it.
//
// BUG-R214(2026-08-01): 修复前本函数只探测 legacy 标量 r.endpoint,却把
// r.endpoints 整个列表原样塞进 status.Endpoints 供 UI 展示 —— 备用端点
// **从未被探测过**,operator 看到的"failover 链"完全是配置回显而非健康
// 事实。一台备用主机彻底不可达时,健康页照样绿灯,直到 13 个 bot 在房间里
// 集体挂满 dial 超时才暴露。现在逐个端点并发探测,结果写入新增的
// status.EndpointStatuses(**新增字段,不改动任何既有字段**,保持
// /api/llm/health 消费方与前端 badge 兼容)。
//
// OK 语义:**任一**端点可达即 OK —— 有 failover 的前提下,主端点死而备用
// 活时业务仍然可用,旧的"只看主端点"会误报红灯。单端点配置下语义与修复前
// 完全一致。
//
// Safe to call concurrently; the lock is held only for the brief status
// update at the end.
func (r *Registry) HealthCheck(ctx context.Context) HealthStatus {
	r.mu.RLock()
	endpoint := r.endpoint
	endpoints := make([]string, len(r.endpoints))
	copy(endpoints, r.endpoints)
	r.mu.RUnlock()

	// 兜底:构造期通常已把 legacy 标量折进 endpoints 列表;若某条路径没折
	// (纯手工构造的 Registry),这里补上,保证探测面不为空。
	if len(endpoints) == 0 && endpoint != "" {
		endpoints = []string{endpoint}
	}

	status := HealthStatus{
		Endpoint:   endpoint,
		Endpoints:  endpoints,
		LastCheck:  time.Now(),
		UsableKeys: r.Count(),
		Unusable:   r.UnusableProviders(),
	}

	if len(endpoints) == 0 {
		status.LastError = "no endpoint configured"
		r.record(status)
		return status
	}

	// 并发探测(端点数量个位数,一人一个 goroutine 足够)。
	results := make([]EndpointHealth, len(endpoints))
	var wg sync.WaitGroup
	for i, ep := range endpoints {
		wg.Add(1)
		go func(i int, ep string) {
			defer wg.Done()
			results[i] = probeEndpoint(ctx, ep)
		}(i, ep)
	}
	wg.Wait()
	status.EndpointStatuses = results

	// 汇总:任一可达即 OK;全挂时 LastError 拼出每个端点的失败原因,
	// 让 startup 日志 / health badge 一眼看清是"全链路死"还是"个别死"。
	var failures []string
	for _, res := range results {
		if res.OK {
			status.OK = true
			continue
		}
		failures = append(failures, fmt.Sprintf("%s: %s", res.Endpoint, res.LastError))
	}
	if !status.OK {
		status.LastError = strings.Join(failures, "; ")
	}
	r.record(status)
	return status
}

// probeEndpoint 对单个端点做一次 3s HEAD 探测。绝不携带任何 api_key(§5)。
func probeEndpoint(ctx context.Context, endpoint string) EndpointHealth {
	out := EndpointHealth{Endpoint: endpoint}
	if strings.TrimSpace(endpoint) == "" {
		out.LastError = "no endpoint configured"
		return out
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		out.LastError = fmt.Sprintf("invalid endpoint %q", endpoint)
		return out
	}

	start := time.Now()
	client := &http.Client{Timeout: 3 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, endpoint, nil)
	if err != nil {
		out.LastError = fmt.Sprintf("build request: %v", err)
		return out
	}
	req.Header.Set("User-Agent", "LsmWebGame-HealthCheck/1.0")

	resp, err := client.Do(req)
	out.LatencyMs = time.Since(start).Milliseconds()
	if err != nil {
		// Connection refused / DNS / timeout / TLS — all collapsed here.
		out.LastError = err.Error()
		return out
	}
	defer resp.Body.Close()
	out.StatusCode = resp.StatusCode
	if resp.StatusCode >= 500 {
		out.LastError = fmt.Sprintf("upstream %d", resp.StatusCode)
		return out
	}
	out.OK = true
	return out
}

func (r *Registry) record(s HealthStatus) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastCheck = s.LastCheck
	if s.OK {
		r.lastErr = ""
	} else {
		r.lastErr = s.LastError
	}
	// BUG-R214: 缓存 per-endpoint 明细,让 Health()(不重新探测的缓存读)
	// 也能把整条 failover 链的健康事实还给调用方。
	r.lastEndpointStatuses = make([]EndpointHealth, len(s.EndpointStatuses))
	copy(r.lastEndpointStatuses, s.EndpointStatuses)
}

// Health returns the last cached HealthCheck result without performing a new
// probe. Used by /api/llm/health to render a status badge cheaply.
func (r *Registry) Health() HealthStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()
	eps := make([]string, len(r.endpoints))
	copy(eps, r.endpoints)
	// BUG-R214: 把最近一次探测的 per-endpoint 明细一并回放(可能为空 ——
	// 进程刚起、还没跑过 HealthCheck 时)。
	epStatuses := make([]EndpointHealth, len(r.lastEndpointStatuses))
	copy(epStatuses, r.lastEndpointStatuses)
	return HealthStatus{
		OK:               r.lastErr == "",
		Endpoint:         r.endpoint,
		Endpoints:        eps,
		EndpointStatuses: epStatuses,
		LastError:        r.lastErr,
		LastCheck:        r.lastCheck,
		UsableKeys:       r.countLocked(),
		Unusable:         r.UnusableProviders(),
	}
}

func (r *Registry) countLocked() int {
	n := 0
	for _, rp := range r.providers {
		if rp.available && rp.enabled {
			n++
		}
	}
	return n
}