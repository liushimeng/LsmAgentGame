// Package debate — 数据库持久化接线(2026-08-31 §20260831-08)。
//
// 设计依据:docs/辩论比赛/03-辩论比赛房间创建与配置设计.md §8 +
// docs/辩论比赛/06-辩论比赛公平性与评审系统设计.md §9「落库持久化(重启不清零)」。
//
// AttachPersistence 由 main.go 在 gormDB 初始化成功后调用一次:
//
//   - onSpeech     → t_lsm_game_debate_speech 异步写入(一发言一行)
//   - onJudgeScore → t_lsm_game_debate_score  异步写入(一裁判×一队一行)
//   - onResult     → t_lsm_game_debate_room upsert(评审结果产出)+
//     t_lsm_game_debate_model_stats UPSERT 原子累加
//   - onGameOver   → t_lsm_game_debate_room upsert(终局状态;无结果 → IsAbnormal)
//
// 钩子采用「链式包装」:先调用 main.go 已注入的 WS 广播钩子,再异步落库,
// 因此接线顺序不影响 §20260831-02 的实时推送。
//
// 硬约束:
//   - gormDB == nil 时完全 no-op(in-memory 降级,单测不依赖数据库);
//   - 所有落库路径异步 goroutine + recover,失败仅 logger.Warn,绝不阻塞比赛主流程;
//   - 日志一律带 room_id 上下文。
package debate

import (
	"encoding/json"
	"fmt"

	"LsmAgentGame/logger"
	"LsmAgentGame/models"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// debatePersister 持久化执行器(持有 gormDB,由 AttachPersistence 构造)。
type debatePersister struct {
	db *gorm.DB
}

// AttachPersistence 给 DebateManager 接入数据库持久化。
//
//	gormDB == nil → 直接返回(in-memory 降级,不接线);
//	m == nil      → 返回错误(调用方 bug,直接暴露);
//	启动回读失败  → 返回 nil 但 Warn:钩子照常接线,本局统计仍会落库,
//	                 仅历史累计缺席(下一局 UPSERT 会自动补回本进程内的新增部分)。
func AttachPersistence(m *DebateManager, gormDB *gorm.DB) error {
	if m == nil {
		return fmt.Errorf("debate: AttachPersistence: nil manager")
	}
	if gormDB == nil {
		// in-memory 降级:不接线,保证单元测试与无 DB 部署可用。
		return nil
	}

	p := &debatePersister{db: gormDB}
	p.loadModelStats(m)

	// 链式包装(先广播、后异步落库)。
	chainSpeechHook(m, p.persistSpeech)
	chainJudgeScoreHook(m, p.persistJudgeScore)
	chainResultHook(m, func(roomID string, res *DebateResult) {
		p.persistGameResult(m, roomID, res)
	})
	chainGameOverHook(m, func(roomID string) {
		p.persistGameOver(m, roomID)
	})

	logger.L().Info("debate persistence attached (§20260831-08)")
	return nil
}

// ============================================================================
// 钩子链式包装(manager.mu 写锁内替换,与 SetOn* 完全同构)
// ============================================================================

// chainSpeechHook 在现有 onSpeech 钩子外再包一层异步落库。
func chainSpeechHook(m *DebateManager, persist func(roomID string, sp Speech)) {
	m.mu.Lock()
	prev := m.onSpeech
	m.onSpeech = func(roomID string, sp Speech) {
		if prev != nil {
			prev(roomID, sp)
		}
		go runSafe("persist_speech", func() { persist(roomID, sp) })
	}
	m.mu.Unlock()
}

// chainJudgeScoreHook 在现有 onJudgeScore 钩子外再包一层异步落库。
func chainJudgeScoreHook(m *DebateManager, persist func(roomID string, sc JudgeScore)) {
	m.mu.Lock()
	prev := m.onJudgeScore
	m.onJudgeScore = func(roomID string, sc JudgeScore) {
		if prev != nil {
			prev(roomID, sc)
		}
		go runSafe("persist_judge_score", func() { persist(roomID, sc) })
	}
	m.mu.Unlock()
}

// chainResultHook 在现有 onResult 钩子外再包一层异步落库。
func chainResultHook(m *DebateManager, persist func(roomID string, res *DebateResult)) {
	m.mu.Lock()
	prev := m.onResult
	m.onResult = func(roomID string, res *DebateResult) {
		if prev != nil {
			prev(roomID, res)
		}
		go runSafe("persist_game_result", func() { persist(roomID, res) })
	}
	m.mu.Unlock()
}

// chainGameOverHook 在现有 onGameOver 钩子外再包一层异步落库。
func chainGameOverHook(m *DebateManager, persist func(roomID string)) {
	m.mu.Lock()
	prev := m.onGameOver
	m.onGameOver = func(roomID string) {
		if prev != nil {
			prev(roomID)
		}
		go runSafe("persist_game_over", func() { persist(roomID) })
	}
	m.mu.Unlock()
}

// runSafe 带 recover 的异步执行体(任何落库 panic 都不允许击穿比赛主流程)。
func runSafe(op string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			logger.L().Error("debate persistence panic",
				zap.String("op", op), zap.Any("panic", r))
		}
	}()
	fn()
}

// ============================================================================
// 写入路径
// ============================================================================

// persistSpeech 异步写一条发言记录(onSpeech 钩子)。
func (p *debatePersister) persistSpeech(roomID string, sp Speech) {
	row := buildSpeechRow(roomID, sp)
	if err := p.db.Create(&row).Error; err != nil {
		logger.L().Warn("debate: persist speech failed",
			zap.String("room_id", roomID), zap.String("speech_id", row.ID), zap.Error(err))
	}
}

// persistJudgeScore 异步写一份裁判评分(onJudgeScore 钩子;Rankings 展开多行)。
func (p *debatePersister) persistJudgeScore(roomID string, sc JudgeScore) {
	rows := buildScoreRows(roomID, sc, WallNowMS())
	if len(rows) == 0 {
		return
	}
	// ID 确定性生成("<room>:j<judge>:t<team>"),重复评分 upsert 幂等覆盖。
	err := p.db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&rows).Error
	if err != nil {
		logger.L().Warn("debate: persist judge score failed",
			zap.String("room_id", roomID), zap.Int("judge_id", sc.JudgeID), zap.Error(err))
	}
}

// persistGameResult 比赛评审结果产出:onResult 钩子。
//
//  1. upsert 房间记录(含 result JSON / 胜方 / 最佳辩手);
//  2. model_stats UPSERT 原子累加(与进程内 RecordGameResult 同一增量来源)。
func (p *debatePersister) persistGameResult(m *DebateManager, roomID string, res *DebateResult) {
	if res == nil {
		return
	}
	room, ok := m.Get(roomID)
	if !ok {
		logger.L().Warn("debate: persist result: room gone", zap.String("room_id", roomID))
		return
	}
	rec, err := buildDebateRoomRecord(roomID, room, res, WallNow())
	if err != nil {
		logger.L().Warn("debate: build room record failed",
			zap.String("room_id", roomID), zap.Error(err))
		return
	}
	p.upsertRoomRecord(rec)
	p.upsertModelStats(computeStatsDeltas(room, res))
}

// persistGameOver 比赛结束:onGameOver 钩子(runResultPhase / StopGame 触发)。
//
// 正常局:结果已在 persistGameResult 写入,此处 upsert 把
// status/current_phase/finished_at 刷新为终局值(幂等)。
// 异常局(被 StopGame 强制终止,无评审结果):补写一行 IsAbnormal=true,
// 保留「比赛存在过」的历史事实。
func (p *debatePersister) persistGameOver(m *DebateManager, roomID string) {
	room, ok := m.Get(roomID)
	if !ok {
		logger.L().Warn("debate: persist game over: room gone", zap.String("room_id", roomID))
		return
	}
	res := room.Result()
	rec, err := buildDebateRoomRecord(roomID, room, res, WallNow())
	if err != nil {
		logger.L().Warn("debate: build room record failed",
			zap.String("room_id", roomID), zap.Error(err))
		return
	}
	if res == nil {
		rec.IsAbnormal = true
	}
	p.upsertRoomRecord(rec)
}

// upsertRoomRecord 按 ID upsert 房间记录(冲突时全字段覆盖,幂等)。
func (p *debatePersister) upsertRoomRecord(rec models.TLsmGameDebateRoom) {
	err := p.db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&rec).Error
	if err != nil {
		logger.L().Warn("debate: upsert room record failed",
			zap.String("room_id", rec.ID), zap.Error(err))
	}
}

// upsertModelStats 模型胜率统计 UPSERT 累加(§06 §9 落库)。
//
// MySQL: INSERT ... ON DUPLICATE KEY UPDATE total_games = total_games + ? ——
// 右侧未限定列名引用的是「已存在行」的值,天然原子累加,并发安全;
// 每个模型单独一条语句(每局 ≤ 队伍数×每队人数 ≈ 10 条,无批量展开必要)。
func (p *debatePersister) upsertModelStats(deltas map[string]modelStatsDelta) {
	if len(deltas) == 0 {
		return
	}
	now := WallNow()
	for key, d := range deltas {
		row := models.TLsmGameDebateModelStats{
			ModelKey:         key,
			TotalGames:       d.TotalGames,
			WinCount:         d.WinCount,
			BestDebaterCount: d.BestDebaterCount,
			ScoreSum:         d.ScoreSum,
			UpdatedAt:        now,
		}
		err := p.db.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "model_key"}},
			DoUpdates: clause.Assignments(map[string]interface{}{
				"total_games":        gorm.Expr("total_games + ?", d.TotalGames),
				"win_count":          gorm.Expr("win_count + ?", d.WinCount),
				"best_debater_count": gorm.Expr("best_debater_count + ?", d.BestDebaterCount),
				"score_sum":          gorm.Expr("score_sum + ?", d.ScoreSum),
				"updated_at":         now,
			}),
		}).Create(&row).Error
		if err != nil {
			logger.L().Warn("debate: upsert model stats failed",
				zap.String("model_key", key), zap.Error(err))
		}
	}
}

// ============================================================================
// 启动回读
// ============================================================================

// loadModelStats 把 t_lsm_game_debate_model_stats 全量回读到进程内 statsStore。
//
// 重启不清零的关键一环:GET /api/games/debate/stats 逻辑不变(仍读进程内快照),
// 但快照的初值来自 DB 历史累计。
func (p *debatePersister) loadModelStats(m *DebateManager) {
	if m.stats == nil {
		return
	}
	var rows []models.TLsmGameDebateModelStats
	if err := p.db.Find(&rows).Error; err != nil {
		logger.L().Warn("debate: load model stats failed (stats start from zero)",
			zap.Error(err))
		return
	}
	deltas := make(map[string]modelStatsDelta, len(rows))
	for _, r := range rows {
		deltas[r.ModelKey] = modelStatsDelta{
			TotalGames:       r.TotalGames,
			WinCount:         r.WinCount,
			BestDebaterCount: r.BestDebaterCount,
			ScoreSum:         r.ScoreSum,
		}
	}
	m.stats.applyDeltas(deltas)
	logger.L().Info("debate: model stats loaded from db",
		zap.Int("models", len(deltas)))
}

// ============================================================================
// 纯构造函数(无 IO,单测直接覆盖)
// ============================================================================

// buildDebateRoomRecord 由房间 + 评审结果构造房间记录行。
//
// res == nil 时胜方相关字段为 -1(无评审结果);FinishedAt 取
// room.FinishedAt(),未标记结束则用 nowSec(比赛事实上已到记录时点)。
func buildDebateRoomRecord(roomID string, room *DebateRoom, res *DebateResult, nowSec int64) (models.TLsmGameDebateRoom, error) {
	if room == nil {
		return models.TLsmGameDebateRoom{}, fmt.Errorf("debate: nil room")
	}

	teamJSON, err := marshalJSONOrEmpty(room.Config.Teams)
	if err != nil {
		return models.TLsmGameDebateRoom{}, fmt.Errorf("marshal teams: %w", err)
	}
	phaseJSON, err := marshalJSONOrEmpty(room.Config.PhaseConfig)
	if err != nil {
		return models.TLsmGameDebateRoom{}, fmt.Errorf("marshal phase config: %w", err)
	}
	judgeJSON, err := marshalJSONOrEmpty(room.Config.Judges)
	if err != nil {
		return models.TLsmGameDebateRoom{}, fmt.Errorf("marshal judges: %w", err)
	}
	specJSON, err := marshalJSONOrEmpty(room.Config.SpectatorConfig)
	if err != nil {
		return models.TLsmGameDebateRoom{}, fmt.Errorf("marshal spectator config: %w", err)
	}
	resultJSON := "null"
	if res != nil {
		b, err := json.Marshal(res)
		if err != nil {
			return models.TLsmGameDebateRoom{}, fmt.Errorf("marshal result: %w", err)
		}
		resultJSON = string(b)
	}

	finishedAt := room.FinishedAt()
	if finishedAt <= 0 {
		finishedAt = nowSec
	}

	rec := models.TLsmGameDebateRoom{
		ID:                roomID,
		TopicID:           room.Config.Topic.ID,
		TopicText:         room.Config.Topic.Text,
		TopicType:         room.Config.Topic.Type,
		Mode:              string(room.Config.Mode),
		TeamConfig:        teamJSON,
		PhaseConfig:       phaseJSON,
		JudgeConfig:       judgeJSON,
		SpectatorConfig:   specJSON,
		Status:            string(room.Phase()),
		CurrentPhase:      string(room.Phase()),
		CreatedBy:         room.Config.CreatedBy,
		CreatedAt:         room.Config.CreatedAt,
		StartedAt:         room.StartedAt(),
		FinishedAt:        finishedAt,
		WinnerTeamID:      -1,
		BestDebaterSeat:   -1,
		BestDebaterTeamID: -1,
		Result:            resultJSON,
	}
	if res != nil {
		rec.WinnerTeamID = res.WinnerTeamID
		rec.BestDebaterSeat = res.BestDebater.Seat
		rec.BestDebaterTeamID = res.BestDebater.TeamID
		rec.IsAbnormal = res.IsAbnormal
	}
	return rec, nil
}

// buildSpeechRow 由 Speech 构造发言记录行(ID 兜底 "<room>:s<ts>")。
func buildSpeechRow(roomID string, sp Speech) models.TLsmGameDebateSpeech {
	id := sp.ID
	if id == "" {
		id = fmt.Sprintf("%s:s%d", roomID, sp.Timestamp)
	}
	refs, _ := json.Marshal(sp.References) // []string 序列化不可能失败;nil → "null"
	return models.TLsmGameDebateSpeech{
		ID:              id,
		RoomID:          roomID,
		Phase:           string(sp.Phase),
		TeamID:          sp.TeamID,
		Seat:            sp.Seat,
		Stance:          string(sp.Stance),
		SpeakerName:     sp.SpeakerName,
		Role:            string(sp.Role),
		Content:         sp.Content,
		WordCount:       sp.WordCount,
		References:      string(refs),
		InternalThought: sp.InternalThought,
		ModelKey:        sp.ModelKey,
		DurationMs:      sp.DurationSec * 1000,
		CreatedAt:       sp.Timestamp,
	}
}

// buildScoreRows 由 JudgeScore 构造评审记录行(Rankings 每队一行)。
//
// ID 确定性生成 "<room_id>:j<judge_id>:t<team_id>" → upsert 幂等。
func buildScoreRows(roomID string, sc JudgeScore, nowMS int64) []models.TLsmGameDebateScore {
	rows := make([]models.TLsmGameDebateScore, 0, len(sc.Rankings))
	for _, r := range sc.Rankings {
		rows = append(rows, models.TLsmGameDebateScore{
			ID:                    fmt.Sprintf("%s:j%d:t%d", roomID, sc.JudgeID, r.TeamID),
			RoomID:                roomID,
			JudgeID:               sc.JudgeID,
			JudgeModelKey:         sc.ModelKey,
			TeamID:                r.TeamID,
			ArgumentQuality:       r.Scores.ArgumentQuality,
			LogicRigor:            r.Scores.LogicRigor,
			LanguageExpression:    r.Scores.LanguageExpression,
			TeamCoordination:      r.Scores.TeamCoordination,
			RebuttalEffectiveness: r.Scores.RebuttalEffectiveness,
			TotalScore:            r.TotalScore,
			Comment:               r.Comment,
			BestDebaterSeat:       r.BestDebater,
			WinnerTeamID:          sc.WinnerTeamID,
			OverallComment:        sc.OverallComment,
			IsFallback:            sc.IsFallback,
			CreatedAt:             nowMS,
		})
	}
	return rows
}

// marshalJSONOrEmpty 序列化并保证产物是合法 JSON(nil 切片 → "null")。
func marshalJSONOrEmpty(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
