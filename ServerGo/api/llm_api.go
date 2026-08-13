// Package api — llm_api.go serves GET /api/llm/models: the safe, key-free list
// of models configured under llm.providers[] in LsmAgentGame.conf. Used by the
// werewolf room-create modal to populate the AI-model picker. Requires auth
// (logged-in user) but no admin role.
package api

import (
	"context"
	"net/http"
	"time"

	"LsmAgentGame/errcode"
	"LsmAgentGame/llm"
	"LsmAgentGame/service"

	"github.com/gin-gonic/gin"
)

// LlmAPI exposes LLM provider metadata over HTTP.
type LlmAPI struct {
	registry  *llm.Registry
	modelLogs *service.ModelLogService // §20260810-03 F3 — leaderboard aggregate
}

// NewLlmAPI constructs an LlmAPI around a built registry. A nil registry is
// accepted (no providers configured) — List() then returns an empty list.
//
// §20260810-03 F3 — modelLogs is optional; Leaderboard returns an empty list
// when nil (defensive default so old test fixtures keep compiling).
func NewLlmAPI(registry *llm.Registry, modelLogs *service.ModelLogService) *LlmAPI {
	return &LlmAPI{registry: registry, modelLogs: modelLogs}
}

// Leaderboard handles GET /api/llm/leaderboard. Returns a per-model aggregate
// over t_lsm_game_model_game_log (games/wins/win_rate/avg_tokens/net_coins).
// §20260810-03 F3 — minimal read-only view, NO cross-faction breakdown yet.
// Auth: logged-in user (any role).
func (h *LlmAPI) Leaderboard(c *gin.Context) {
	if h.modelLogs == nil {
		c.JSON(http.StatusOK, gin.H{
			"code":    errcode.OK,
			"message": errcode.DefaultMessages[errcode.OK],
			"data":    []service.LeaderboardEntry{},
		})
		return
	}
	rows, err := h.modelLogs.Leaderboard(c.Request.Context(), 8)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"code":    errcode.Code(errcode.ErrDB),
			"message": err.Error(),
			"data":    []service.LeaderboardEntry{},
		})
		return
	}
	if rows == nil {
		rows = []service.LeaderboardEntry{}
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": errcode.DefaultMessages[errcode.OK],
		"data":    rows,
	})
}

// Radar handles GET /api/llm/radar. Returns 5-dimension capability radar stats
// per model (win_rate / wolf_win_rate / good_win_rate / token_eff / coin_per_game).
// §20260812-02 U1 — admin radar chart data source.
// Auth: logged-in user (any role). Returns map[provider_id]ModelRadarStats.
func (h *LlmAPI) Radar(c *gin.Context) {
	if h.modelLogs == nil {
		c.JSON(http.StatusOK, gin.H{
			"code":    errcode.OK,
			"message": errcode.DefaultMessages[errcode.OK],
			"data":    map[string]service.ModelRadarStats{},
		})
		return
	}
	rows, err := h.modelLogs.RadarStats(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"code":    errcode.Code(errcode.ErrDB),
			"message": err.Error(),
			"data":    map[string]service.ModelRadarStats{},
		})
		return
	}
	if rows == nil {
		rows = map[string]*service.ModelRadarStats{}
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": errcode.DefaultMessages[errcode.OK],
		"data":    rows,
	})
}

// WinTrends handles GET /api/llm/win-trends. Returns per-model win-rate trend
// aggregates (daily trend / by-role / by-seat) over t_lsm_game_model_game_log.
// §20260813-02 U1 (T12 胜率趋势追踪). Auth: logged-in user (any role).
// Returns map[provider_id]*ModelWinTrend (§121: 前端直解 map,非 wrapper)。
func (h *LlmAPI) WinTrends(c *gin.Context) {
	if h.modelLogs == nil {
		c.JSON(http.StatusOK, gin.H{
			"code":    errcode.OK,
			"message": errcode.DefaultMessages[errcode.OK],
			"data":    map[string]*service.ModelWinTrend{},
		})
		return
	}
	rows, err := h.modelLogs.WinRateTrends(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"code":    errcode.Code(errcode.ErrDB),
			"message": err.Error(),
			"data":    map[string]*service.ModelWinTrend{},
		})
		return
	}
	if rows == nil {
		rows = map[string]*service.ModelWinTrend{}
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": errcode.DefaultMessages[errcode.OK],
		"data":    rows,
	})
}

// List handles GET /api/llm/models. Returns {agent_name, model, provider_type}
// for every configured model (including placeholder ones — callers decide how
// to surface those). API keys are NEVER returned.
func (h *LlmAPI) List(c *gin.Context) {
	if h.registry == nil {
		c.JSON(http.StatusOK, gin.H{
			"code":    errcode.OK,
			"message": errcode.DefaultMessages[errcode.OK],
			"data":    []llm.ModelInfo{},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": errcode.DefaultMessages[errcode.OK],
		"data":    h.registry.List(),
	})
}

// Health handles GET /api/llm/health. Runs a fresh HEAD probe (3s timeout)
// against the configured upstream and returns the result so the front-end
// can warn operators / show a banner when the proxy is unreachable. Auth:
// logged-in user (any role). Does NOT leak api_keys.
//
// ROUND 25 BUG-WEREWOLF-P0-NEW-7 follow-up: gives the test harness an
// observable surface for "is the LLM proxy alive right now" without
// having to actually create a werewolf room and wait for 7 agent calls
// to fail.
func (h *LlmAPI) Health(c *gin.Context) {
	if h.registry == nil {
		c.JSON(http.StatusOK, gin.H{
			"code":    errcode.OK,
			"message": errcode.DefaultMessages[errcode.OK],
			"data": llm.HealthStatus{
				OK:        false,
				Endpoint:  "",
				LastError: "no LLM registry configured",
				LastCheck: time.Now(),
			},
		})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()
	status := h.registry.HealthCheck(ctx)
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": errcode.DefaultMessages[errcode.OK],
		"data":    status,
	})
}
