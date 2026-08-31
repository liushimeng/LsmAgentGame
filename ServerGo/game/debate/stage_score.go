// Package debate — 裁判实时打分(阶段式)存储与累计(2026-08-31 §20260831-09)。
//
// 设计目标:
//   - 评审阶段(submit_score)之前,裁判可在每个发言阶段(立论/驳论/质询/小结/自由辩/总结)
//     调用 submit_stage_score 工具提交「本阶段临时打分」。
//   - 每条 stage_score 触发:累计该裁判对各队的加权平均 + 广播 debate.stage_score 帧。
//   - submit_score 提交最终版本时,所有累计冻结,StageScore.IsFinal=true,
//     并同步写入最终结果路径(由 submit_score 派发逻辑负责)。
//
// 线程安全:由 DebateRoom.scoreboardsMu 保护;r.mu 与 scoreboardsMu 不同层级,
// 不存在锁序倒置(§92a 范式)。
//
// 详细设计见 docs/辩论比赛/07-辩论比赛Agent统计与裁判实时打分设计.md §3。
package debate

import (
	"sort"
	"sync"
)

// scoreboardsStore 房间级裁判实时打分存储。
type scoreboardsStore struct {
	mu          sync.RWMutex
	scoreboards map[int]*JudgeScoreboard // judge_id → 看板
	seqByJudge  map[int]int              // judge_id → 累计 stage_score 序号
}

func newScoreboardsStore() *scoreboardsStore {
	return &scoreboardsStore{
		scoreboards: make(map[int]*JudgeScoreboard),
		seqByJudge:  make(map[int]int),
	}
}

// recordStageScore 累计一份 stage_score,并返回该裁判更新后的看板(用于广播)。
//
// 入参 ss 已经经过 JSON 反序列化与 5 维度钳制;submit_stage_score 工具派发
// 时构造完整字段。IsFinal=true 时冻结该裁判的看板(后续 stage_score 不再覆盖累计)。
func (s *scoreboardsStore) recordStageScore(ss *StageScore) *JudgeScoreboard {
	if s == nil || ss == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	board, ok := s.scoreboards[ss.JudgeID]
	if !ok {
		board = &JudgeScoreboard{
			JudgeID:    ss.JudgeID,
			ModelKey:   ss.ModelKey,
			TeamScores: make(map[int]*AccumulatedTeamScore, 4),
		}
		s.scoreboards[ss.JudgeID] = board
	}
	if board.ModelKey == "" {
		board.ModelKey = ss.ModelKey
	}

	// 累加每队的 5 维度分(加权平均)
	for _, ts := range ss.TeamScores {
		acc, ok := board.TeamScores[ts.TeamID]
		if !ok {
			acc = &AccumulatedTeamScore{TeamID: ts.TeamID}
			board.TeamScores[ts.TeamID] = acc
		}
		oldCount := acc.SubmissionCount
		newCount := oldCount + 1
		// 5 维度累计平均(避免旧版本被新版完全覆盖)
		acc.ArgumentQuality = avgDim(acc.ArgumentQuality, float64(ts.Scores.ArgumentQuality), oldCount, newCount)
		acc.LogicRigor = avgDim(acc.LogicRigor, float64(ts.Scores.LogicRigor), oldCount, newCount)
		acc.LanguageExpression = avgDim(acc.LanguageExpression, float64(ts.Scores.LanguageExpression), oldCount, newCount)
		acc.TeamCoordination = avgDim(acc.TeamCoordination, float64(ts.Scores.TeamCoordination), oldCount, newCount)
		acc.RebuttalEffectiveness = avgDim(acc.RebuttalEffectiveness, float64(ts.Scores.RebuttalEffectiveness), oldCount, newCount)
		acc.TotalScore = acc.ArgumentQuality + acc.LogicRigor + acc.LanguageExpression +
			acc.TeamCoordination + acc.RebuttalEffectiveness
		acc.LatestComment = ts.Comment
		acc.LatestPhase = ss.Phase
		acc.LatestPhaseCN = ss.PhaseCN
		acc.SubmissionCount = newCount
	}

	// 追加历史(最多保留 10 条;超限队首淘汰)
	board.StageHistory = append(board.StageHistory, *ss)
	if len(board.StageHistory) > 10 {
		board.StageHistory = board.StageHistory[len(board.StageHistory)-10:]
	}

	// 序号累加(便于前端按时间排序)
	s.seqByJudge[ss.JudgeID]++

	if ss.IsFinal {
		board.IsFinal = true
	}

	// 返回副本(避免锁外读到内部 map 被改)
	return cloneJudgeScoreboardLocked(board)
}

// avgDim 计算加权平均:old_count=0 时直接返回 new_val。
//
// 公式:new_avg = (old_avg × old_count + new_val) / (old_count + 1)
func avgDim(oldAvg, newVal float64, oldCount, newCount int) float64 {
	if oldCount == 0 {
		return newVal
	}
	return (oldAvg*float64(oldCount) + newVal) / float64(newCount)
}

// cloneJudgeScoreboardLocked 拷贝一份 JudgeScoreboard(锁内调用)。
func cloneJudgeScoreboardLocked(b *JudgeScoreboard) *JudgeScoreboard {
	if b == nil {
		return nil
	}
	out := &JudgeScoreboard{
		JudgeID:      b.JudgeID,
		ModelKey:     b.ModelKey,
		TeamScores:   make(map[int]*AccumulatedTeamScore, len(b.TeamScores)),
		StageHistory: make([]StageScore, len(b.StageHistory)),
		IsFinal:      b.IsFinal,
	}
	for tid, acc := range b.TeamScores {
		cp := *acc
		out.TeamScores[tid] = &cp
	}
	copy(out.StageHistory, b.StageHistory)
	return out
}

// allScoreboards 返回所有看板的副本(按 judge_id 升序)。
func (s *scoreboardsStore) allScoreboards() []*JudgeScoreboard {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.scoreboards) == 0 {
		return nil
	}
	ids := make([]int, 0, len(s.scoreboards))
	for id := range s.scoreboards {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	out := make([]*JudgeScoreboard, 0, len(ids))
	for _, id := range ids {
		out = append(out, cloneJudgeScoreboardLocked(s.scoreboards[id]))
	}
	return out
}

// ============================================================================
// DebateRoom 辅助方法:暴露 scoreboards 入口(供 BuildClientState / hook 调用)
// ============================================================================

// AddStageScore 累计一份 stage_score(线程安全)。
//
// 由 debatejudge.AgentJudge.dispatchSubmitStageScore 调用;同时经 manager hook
// 广播 debate.stage_score / debate.judge_scoreboard 帧给全员。
func (r *DebateRoom) AddStageScore(ss *StageScore) *JudgeScoreboard {
	if r == nil || ss == nil {
		return nil
	}
	if r.scoreboards == nil {
		r.scoreboards = newScoreboardsStore()
	}
	if ss.SubmittedAtMS == 0 {
		ss.SubmittedAtMS = WallNowMS()
	}
	if ss.PhaseCN == "" {
		ss.PhaseCN = PhaseCN(ss.Phase)
	}
	board := r.scoreboards.recordStageScore(ss)
	// 锁外外抛 hook(避免与广播回调死锁)
	if r.manager != nil {
		r.manager.EmitStageScore(r.RoomID, ss)
	}
	return board
}

// Scoreboards 返回所有裁判实时打分看板副本(读锁)。
func (r *DebateRoom) Scoreboards() []*JudgeScoreboard {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.scoreboards == nil {
		return nil
	}
	return r.scoreboards.allScoreboards()
}

// scoreboardsCopyLocked 是 Scoreboards 的**锁内变体**(§92a 范式)。
//
// 调用方必须已持有 r.mu(读锁或写锁)。
// 仅读 r.scoreboards,内部取 scoreboards.mu(不同层级锁,无锁序倒置)。
func (r *DebateRoom) scoreboardsCopyLocked() []*JudgeScoreboard {
	if r.scoreboards == nil {
		return nil
	}
	return r.scoreboards.allScoreboards()
}

// MarkJudgeFinalized 把指定裁判的看板标记为 IsFinal=true(供 submit_score 派发调用)。
func (r *DebateRoom) MarkJudgeFinalized(judgeID int) {
	if r.scoreboards == nil {
		return
	}
	r.scoreboards.mu.Lock()
	if board, ok := r.scoreboards.scoreboards[judgeID]; ok {
		board.IsFinal = true
	}
	r.scoreboards.mu.Unlock()
}
