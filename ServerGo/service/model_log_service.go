// Package service — model_log_service.go
//
// Per the "模型管理 + 模型玩家持久化 + 模型金币" plan (kind-skipping-moth §2.5),
// this service encapsulates all read-side GORM queries against the 5 new
// tables (t_lsm_game_llm_provider, t_lsm_game_model_game_log,
// t_lsm_game_model_chat_message, t_lsm_game_model_action, t_lsm_game_wallet +
// t_lsm_game_wallet_tx). Handlers in api/ must call into this service instead
// of touching gormDB directly — keeps GORM out of the handler layer and lets
// the same query primitives be reused by future cron jobs / admin tooling.
//
// Read-only by design: writes go through agent.RecordLogService (game_log /
// chat_message / action), service.BotUserService (bot user provisioning) and
// service.WalletService (wallet mutations). This service never INSERTs.
package service

import (
	"context"
	"errors"
	"math"
	"time"

	"LsmAgentGame/errcode"
	"LsmAgentGame/logger"
	"LsmAgentGame/models"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// ModelLogService is a read-only GORM façade for the LLM/model/wallet tables.
// Safe for concurrent use — gorm.DB itself is concurrency-safe per the GORM
// docs and this service adds no internal state of its own.
type ModelLogService struct {
	gormDB *gorm.DB
}

// NewModelLogService builds a ModelLogService. A nil gormDB is accepted and
// surfaced via ErrInternal from every method so handlers don't have to nil-
// check before calling.
func NewModelLogService(db *gorm.DB) *ModelLogService {
	return &ModelLogService{gormDB: db}
}

// ─────────────────── provider games ───────────────────

// ListProviderGames returns paginated game_log rows for one provider, newest
// first. since is inclusive; zero value means "no time filter".
//
// limit <= 0 → 20, limit > 200 → 200; offset < 0 → 0. The composite index
// idx_provider_created = (provider_id, started_at) covers this query exactly.
func (s *ModelLogService) ListProviderGames(
	ctx context.Context, providerID string, limit, offset int, since time.Time,
) ([]models.TLsmGameModelGameLog, error) {
	if providerID == "" {
		return nil, errcode.Code(errcode.ErrValidationFailed)
	}
	if s.gormDB == nil {
		return nil, errcode.Code(errcode.ErrInternal)
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}
	q := s.gormDB.WithContext(ctx).
		Where("provider_id = ?", providerID)
	if !since.IsZero() {
		q = q.Where("started_at >= ?", since)
	}
	var rows []models.TLsmGameModelGameLog
	if err := q.Order("started_at DESC").
		Limit(limit).Offset(offset).
		Find(&rows).Error; err != nil {
		logger.L().Error("model_log list provider games failed",
			zap.String("provider_id", providerID), zap.Error(err))
		return nil, errcode.Code(errcode.ErrDB)
	}
	return rows, nil
}

// ─────────────────── single game_log ───────────────────

// GetGameLog fetches one game_log row by id. Returns ErrValidationFailed (404
// semantics in our error table — the caller decides the HTTP status code) on
// gorm.ErrRecordNotFound so the handler can map it to a clean 404 instead of
// a generic 500.
func (s *ModelLogService) GetGameLog(
	ctx context.Context, gameLogID string,
) (*models.TLsmGameModelGameLog, error) {
	if gameLogID == "" {
		return nil, errcode.Code(errcode.ErrValidationFailed)
	}
	if s.gormDB == nil {
		return nil, errcode.Code(errcode.ErrInternal)
	}
	var row models.TLsmGameModelGameLog
	err := s.gormDB.WithContext(ctx).
		Where("id = ?", gameLogID).
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.Code(errcode.ErrValidationFailed)
		}
		logger.L().Error("model_log get game_log failed",
			zap.String("game_log_id", gameLogID), zap.Error(err))
		return nil, errcode.Code(errcode.ErrDB)
	}
	return &row, nil
}

// ─────────────────── chat_message + action ───────────────────

// ListGameMessages returns the chat transcript for one game_log, ordered by
// (seq, id ASC). idx_gamelog_seq = (game_log_id, seq) covers this.
//
// limit <= 0 → 200, limit > 2000 → 2000. offset is forwarded as-is (rare —
// most callers use the default 200-row tail).
func (s *ModelLogService) ListGameMessages(
	ctx context.Context, gameLogID string, limit, offset int,
) ([]models.TLsmGameModelChatMessage, error) {
	if gameLogID == "" {
		return nil, errcode.Code(errcode.ErrValidationFailed)
	}
	if s.gormDB == nil {
		return nil, errcode.Code(errcode.ErrInternal)
	}
	if limit <= 0 {
		limit = 200
	}
	if limit > 2000 {
		limit = 2000
	}
	if offset < 0 {
		offset = 0
	}
	var rows []models.TLsmGameModelChatMessage
	if err := s.gormDB.WithContext(ctx).
		Where("game_log_id = ?", gameLogID).
		Order("seq ASC, id ASC").
		Limit(limit).Offset(offset).
		Find(&rows).Error; err != nil {
		logger.L().Error("model_log list game messages failed",
			zap.String("game_log_id", gameLogID), zap.Error(err))
		return nil, errcode.Code(errcode.ErrDB)
	}
	return rows, nil
}

// ListGameActions returns the action/decision log for one game_log, ordered
// chronologically by created_at then id (id breaks ties when 2 actions land
// in the same millisecond, which is common in werewolf tool-call bursts).
//
// idx_gamelog_phase = (game_log_id, phase) is also a candidate index but
// ordering on created_at is the dominant access pattern for the admin
// game-log viewer.
func (s *ModelLogService) ListGameActions(
	ctx context.Context, gameLogID string, limit, offset int,
) ([]models.TLsmGameModelAction, error) {
	if gameLogID == "" {
		return nil, errcode.Code(errcode.ErrValidationFailed)
	}
	if s.gormDB == nil {
		return nil, errcode.Code(errcode.ErrInternal)
	}
	if limit <= 0 {
		limit = 200
	}
	if limit > 2000 {
		limit = 2000
	}
	if offset < 0 {
		offset = 0
	}
	var rows []models.TLsmGameModelAction
	if err := s.gormDB.WithContext(ctx).
		Where("game_log_id = ?", gameLogID).
		Order("created_at ASC, id ASC").
		Limit(limit).Offset(offset).
		Find(&rows).Error; err != nil {
		logger.L().Error("model_log list game actions failed",
			zap.String("game_log_id", gameLogID), zap.Error(err))
		return nil, errcode.Code(errcode.ErrDB)
	}
	return rows, nil
}

// ─────────────────── bot wallet ───────────────────

// BotWalletSummary is the JSON shape returned by GetBotWalletSummary.
// Transactions are pre-sorted newest-first.
type BotWalletSummary struct {
	Balance      int64                     `json:"balance"`
	TotalEarned  int64                     `json:"total_earned"`
	TotalSpent   int64                     `json:"total_spent"`
	Transactions []models.TLsmGameWalletTx `json:"transactions"`
}

// GetBotWalletSummary returns the wallet balance + totals + the last N ledger
// entries for one bot user. txLimit <= 0 → 50, txLimit > 500 → 500.
//
// Returns an empty (zero-value) BotWalletSummary — NOT an error — when the
// bot user has no wallet row yet (defensive: matches WalletService.GetBalance
// semantics and avoids cascading failures for legacy bot rows).
func (s *ModelLogService) GetBotWalletSummary(
	ctx context.Context, botUserID string, txLimit int,
) (*BotWalletSummary, error) {
	if botUserID == "" {
		return nil, errcode.Code(errcode.ErrValidationFailed)
	}
	if s.gormDB == nil {
		return nil, errcode.Code(errcode.ErrInternal)
	}
	if txLimit <= 0 {
		txLimit = 50
	}
	if txLimit > 500 {
		txLimit = 500
	}

	out := &BotWalletSummary{}

	var wallet models.TLsmGameWallet
	err := s.gormDB.WithContext(ctx).
		Where("user_id = ?", botUserID).
		First(&wallet).Error
	switch {
	case err == nil:
		out.Balance = wallet.Balance
		out.TotalEarned = wallet.TotalEarned
		out.TotalSpent = wallet.TotalSpent
	case errors.Is(err, gorm.ErrRecordNotFound):
		// Defensive: legacy bots without wallets — return zero-value summary.
	default:
		logger.L().Error("model_log bot wallet summary failed",
			zap.String("bot_user_id", botUserID), zap.Error(err))
		return nil, errcode.Code(errcode.ErrDB)
	}

	var txs []models.TLsmGameWalletTx
	if err := s.gormDB.WithContext(ctx).
		Where("user_id = ?", botUserID).
		Order("created_at DESC, id DESC").
		Limit(txLimit).
		Find(&txs).Error; err != nil {
		logger.L().Error("model_log bot wallet ledger fetch failed",
			zap.String("bot_user_id", botUserID), zap.Error(err))
		return nil, errcode.Code(errcode.ErrDB)
	}
	out.Transactions = txs
	return out, nil
}

// ─────────────────── leaderboard (聚合 §20260810-03 F3) ───────────────────

// LeaderboardEntry is one row in the per-model leaderboard aggregate.
//
// Fields are populated by Leaderboard(): provider_id (FK to
// t_lsm_game_llm_provider), games (total rows), wins (rows where
// result="win"), win_rate (wins/games, 0..1), avg_tokens (mean of
// input_tokens+output_tokens across all rows), net_coins (sum of coin_delta).
//
// §20260810-03 F3 — minimal read-only aggregate over the existing
// t_lsm_game_model_game_log. NO cross-faction breakdown yet (deferred per
// K3 A1 / LongCat G1 follow-up).
type LeaderboardEntry struct {
	ProviderID string  `json:"provider_id"`
	Games      int64   `json:"games"`
	Wins       int64   `json:"wins"`
	WinRate    float64 `json:"win_rate"`
	AvgTokens  float64 `json:"avg_tokens"`
	NetCoins   int64   `json:"net_coins"`
}

// Leaderboard returns the per-model aggregate ordered by games DESC.
// Empty result rows (games=0) are skipped. limit <= 0 → 8 (number of default
// models per CLAUDE.md §14); limit > 64 → 64.
//
// §20260810-03 F3 — query plan hits idx_provider_created = (provider_id,
// started_at) for the GROUP BY. Tests should run inside a tx-rollback
// harness to avoid polluting the shared DB.
func (s *ModelLogService) Leaderboard(
	ctx context.Context, limit int,
) ([]LeaderboardEntry, error) {
	if s.gormDB == nil {
		return nil, errcode.Code(errcode.ErrInternal)
	}
	if limit <= 0 {
		limit = 8
	}
	if limit > 64 {
		limit = 64
	}
	type aggRow struct {
		ProviderID string
		Games      int64
		Wins       int64
		AvgTokens  float64
		NetCoins   int64
	}
	var rows []aggRow
	err := s.gormDB.WithContext(ctx).
		Table("t_lsm_game_model_game_log").
		Select(`provider_id,
			COUNT(*) AS games,
			SUM(CASE WHEN result = 'win' THEN 1 ELSE 0 END) AS wins,
			AVG(input_tokens + output_tokens) AS avg_tokens,
			COALESCE(SUM(coin_delta), 0) AS net_coins`).
		Group("provider_id").
		Order("games DESC").
		Limit(limit).
		Scan(&rows).Error
	if err != nil {
		logger.L().Error("model_log leaderboard aggregate failed", zap.Error(err))
		return nil, errcode.Code(errcode.ErrDB)
	}
	out := make([]LeaderboardEntry, 0, len(rows))
	for _, r := range rows {
		if r.Games <= 0 {
			continue
		}
		rate := float64(0)
		if r.Games > 0 {
			rate = float64(r.Wins) / float64(r.Games)
		}
		out = append(out, LeaderboardEntry{
			ProviderID: r.ProviderID,
			Games:      r.Games,
			Wins:       r.Wins,
			WinRate:    rate,
			AvgTokens:  r.AvgTokens,
			NetCoins:   r.NetCoins,
		})
	}
	return out, nil
}

// ─────────────────── radar stats (§20260812-02 U1) ───────────────────

// ModelRadarStats is one model's 5-dimension capability radar.
//
// §20260812-02 U1 — per-model 5D aggregate for the admin radar chart.
// Dimensions are normalised to 0..100 for direct polar-plot rendering.
// All data comes from t_lsm_game_model_game_log (read-only, no LLM calls).
type ModelRadarStats struct {
	ProviderID  string  `json:"provider_id"`
	AgentName   string  `json:"agent_name"`    // JOIN llm_provider for display name
	Games       int64   `json:"games"`
	WinRate     float64 `json:"win_rate"`      // overall win_rate × 100
	WolfWinRate float64 `json:"wolf_win_rate"` // wolf-only win_rate × 100
	GoodWinRate float64 `json:"good_win_rate"` // good-only win_rate × 100
	TokenEff    float64 `json:"token_eff"`     // win_rate / avg_tokens normalised 0..100
	CoinPerGame float64 `json:"coin_per_game"` // avg coin_delta per game, normalised ±500→0..100
	SampleOK    bool    `json:"sample_ok"`     // Games >= SelfPortraitMinGames
}

// RadarStats returns 5-dimension capability radar for all models that have at
// least 1 game log. Returns map[provider_id]*ModelRadarStats.
//
// Query plan: single GROUP BY provider_id over t_lsm_game_model_game_log.
// JOINs t_lsm_game_llm_provider for the display agent_name.
// §121: callers must handle the returned map shape (not a wrapper object).
func (s *ModelLogService) RadarStats(
	ctx context.Context,
) (map[string]*ModelRadarStats, error) {
	if s.gormDB == nil {
		return nil, errcode.Code(errcode.ErrInternal)
	}
	type aggRow struct {
		ProviderID string
		AgentName  string
		Games      int64
		Wins       int64
		WolfGames  int64
		WolfWins   int64
		GoodGames  int64
		GoodWins   int64
		AvgTokens  float64
		AvgCoin    float64
	}
	var rows []aggRow
	err := s.gormDB.WithContext(ctx).
		Table("t_lsm_game_model_game_log gl").
		Joins("LEFT JOIN t_lsm_game_llm_provider lp ON lp.id = gl.provider_id").
		Select(`gl.provider_id,
			COALESCE(lp.agent_name, gl.provider_id) AS agent_name,
			COUNT(*) AS games,
			SUM(CASE WHEN gl.result = 'win' THEN 1 ELSE 0 END) AS wins,
			SUM(CASE WHEN gl.role = 'werewolf' THEN 1 ELSE 0 END) AS wolf_games,
			SUM(CASE WHEN gl.role = 'werewolf' AND gl.result = 'win' THEN 1 ELSE 0 END) AS wolf_wins,
			SUM(CASE WHEN gl.role <> 'werewolf' AND gl.role <> '' THEN 1 ELSE 0 END) AS good_games,
			SUM(CASE WHEN gl.role <> 'werewolf' AND gl.role <> '' AND gl.result = 'win' THEN 1 ELSE 0 END) AS good_wins,
			COALESCE(AVG(gl.input_tokens + gl.output_tokens), 0) AS avg_tokens,
			COALESCE(AVG(gl.coin_delta), 0) AS avg_coin`).
		Group("gl.provider_id, lp.agent_name").
		Having("COUNT(*) > 0").
		Scan(&rows).Error
	if err != nil {
		logger.L().Error("model_log radar stats aggregate failed", zap.Error(err))
		return nil, errcode.Code(errcode.ErrDB)
	}
	out := make(map[string]*ModelRadarStats, len(rows))
	for _, r := range rows {
		winRate := float64(0)
		if r.Games > 0 {
			winRate = float64(r.Wins) / float64(r.Games) * 100
		}
		wolfWR := float64(0)
		if r.WolfGames > 0 {
			wolfWR = float64(r.WolfWins) / float64(r.WolfGames) * 100
		}
		goodWR := float64(0)
		if r.GoodGames > 0 {
			goodWR = float64(r.GoodWins) / float64(r.GoodGames) * 100
		}
		// Token efficiency: win_rate / avg_tokens, normalised to 0..100.
		// Higher avg_tokens = lower efficiency. Baseline: 10k tokens = 50%.
		tokenEff := float64(0)
		if r.AvgTokens > 0 {
			raw := winRate / (r.AvgTokens / 10000.0)
			if raw > 100 {
				raw = 100
			}
			tokenEff = raw
		}
		// Coin per game: map ±500 to 0..100.
		coinNorm := 50.0 + r.AvgCoin/10.0
		if coinNorm < 0 {
			coinNorm = 0
		}
		if coinNorm > 100 {
			coinNorm = 100
		}
		out[r.ProviderID] = &ModelRadarStats{
			ProviderID:  r.ProviderID,
			AgentName:   r.AgentName,
			Games:       r.Games,
			WinRate:     math.Round(winRate*10) / 10,
			WolfWinRate: math.Round(wolfWR*10) / 10,
			GoodWinRate: math.Round(goodWR*10) / 10,
			TokenEff:    math.Round(tokenEff*10) / 10,
			CoinPerGame: math.Round(coinNorm*10) / 10,
			SampleOK:    r.Games >= SelfPortraitMinGames,
		}
	}
	return out, nil
}

// ─────────────────── model self portrait (§20260810-10 U2) ───────────────────

// SelfPortraitMinGames 是自画像生效的最小样本局数(§120 公平性:小样本降级
// 为通用自画像,避免 2~3 局偶然结果被当成"模型特点")。
const SelfPortraitMinGames = 8

// ModelSelfPortrait 是一个模型在本平台的聚合「自画像」统计。
//
// §20260810-10 U2 (DeepSeek §五.1) — 注入 Agent system prompt 末尾,
// 让模型知道自己"狼胜率/好人胜率/与全模型平均的差距",实现元认知差异化策略。
// 只含聚合统计,不含任何单局聊天原文/对手信息(不触碰 §135)。
//
// 聚合 key 说明:t_lsm_game_model_game_log.provider_id 当前由 RecordLogService
// 以 modelKey(LLMProvider.AgentName,如 "DouBao-model")写入 — 与
// t_lsm_game_llm_provider.agent_name 同域。因此本聚合按 provider_id 列 GROUP BY
// 即可得到 per-model 统计,无需 JOIN provider 表。
type ModelSelfPortrait struct {
	ModelKey      string  `json:"model_key"`        // = game_log.provider_id 列值(= AgentName)
	Games         int64   `json:"games"`
	WinRate       float64 `json:"win_rate"`         // 总胜率 0..1
	WolfGames     int64   `json:"wolf_games"`       // role='werewolf' 局数
	WolfWinRate   float64 `json:"wolf_win_rate"`    // 狼人胜率 0..1
	GoodGames     int64   `json:"good_games"`       // 非狼角色局数
	GoodWinRate   float64 `json:"good_win_rate"`    // 好人胜率 0..1
	AvgWinRateAll float64 `json:"avg_win_rate_all"` // 全模型平均胜率(基准线)
	SampleOK      bool    `json:"sample_ok"`        // Games >= SelfPortraitMinGames
}

// SelfPortraits 批量返回指定 modelKey 集合的自画像聚合。
// 返回 map[modelKey]*ModelSelfPortrait;DB 中无记录的 modelKey 不出现在 map 中
// (调用方按"无数据 → 通用自画像"处理)。
//
// 查询计划:GROUP BY provider_id 命中 idx_provider_created;全模型平均胜率用
// 一次独立轻量聚合(不 JOIN,避免行放大)。§118:本方法仅在开局
// StartAgentsLocked 调用一次,失败仅 logger.Warn 降级,不阻塞游戏流。
func (s *ModelLogService) SelfPortraits(
	ctx context.Context, modelKeys []string,
) (map[string]*ModelSelfPortrait, error) {
	out := make(map[string]*ModelSelfPortrait, len(modelKeys))
	if s.gormDB == nil || len(modelKeys) == 0 {
		return out, nil
	}
	type aggRow struct {
		ProviderID string
		Games      int64
		Wins       int64
		WolfGames  int64
		WolfWins   int64
		GoodGames  int64
		GoodWins   int64
	}
	var rows []aggRow
	err := s.gormDB.WithContext(ctx).
		Table("t_lsm_game_model_game_log").
		Select(`provider_id,
			COUNT(*) AS games,
			SUM(CASE WHEN result = 'win' THEN 1 ELSE 0 END) AS wins,
			SUM(CASE WHEN role = 'werewolf' THEN 1 ELSE 0 END) AS wolf_games,
			SUM(CASE WHEN role = 'werewolf' AND result = 'win' THEN 1 ELSE 0 END) AS wolf_wins,
			SUM(CASE WHEN role <> 'werewolf' THEN 1 ELSE 0 END) AS good_games,
			SUM(CASE WHEN role <> 'werewolf' AND result = 'win' THEN 1 ELSE 0 END) AS good_wins`).
		Where("provider_id IN ?", modelKeys).
		Group("provider_id").
		Scan(&rows).Error
	if err != nil {
		logger.L().Error("model_log self portrait aggregate failed", zap.Error(err))
		return nil, errcode.Code(errcode.ErrDB)
	}
	// 全模型平均胜率(基准线):所有有记录模型的胜率均值。
	var avgAll float64
	var avgCnt int64
	{
		var r struct {
			Avg float64
			Cnt int64
		}
		if err := s.gormDB.WithContext(ctx).
			Table("t_lsm_game_model_game_log").
			Select("AVG(CASE WHEN result = 'win' THEN 1.0 ELSE 0.0 END) AS avg, COUNT(*) AS cnt").
			Scan(&r).Error; err == nil && r.Cnt > 0 {
			avgAll = r.Avg
			avgCnt = r.Cnt
		}
	}
	_ = avgCnt
	for _, r := range rows {
		if r.Games <= 0 {
			continue
		}
		p := &ModelSelfPortrait{
			ModelKey:      r.ProviderID,
			Games:         r.Games,
			WinRate:       float64(r.Wins) / float64(r.Games),
			WolfGames:     r.WolfGames,
			GoodGames:     r.GoodGames,
			AvgWinRateAll: avgAll,
			SampleOK:      r.Games >= SelfPortraitMinGames,
		}
		if r.WolfGames > 0 {
			p.WolfWinRate = float64(r.WolfWins) / float64(r.WolfGames)
		}
		if r.GoodGames > 0 {
			p.GoodWinRate = float64(r.GoodWins) / float64(r.GoodGames)
		}
		out[r.ProviderID] = p
	}
	return out, nil
}
