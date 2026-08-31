// Package debate — 模型胜率统计(§20260831-06)。
//
// 设计依据:docs/辩论比赛/06-辩论比赛公平性与评审系统设计.md §9「历史统计」:
//
//	每局评审结果产出后,按辩手模型累加 TotalGames / WinCount /
//	BestDebaterCount / 队伍总分;GET /api/games/debate/stats 返回快照。
//
// §20260831-08 起统计「双层存储」:
//   - 进程内 statsStore(本文件,API 读这里,快照逻辑不变);
//   - t_lsm_game_debate_model_stats 落库(persistence.go 每局 UPSERT 累加,
//     启动时 AttachPersistence 回读到 statsStore,重启不再清零)。
//
// 公平性用途(§9.2 平衡调整):后续可在 FairModelAssignment 中读取
// 胜率过高/过低的模型调整分配优先级。
package debate

import (
	"sort"
	"sync"
)

// modelStatsAccum 单模型累加器(内部结构,不直接暴露)。
type modelStatsAccum struct {
	totalGames       int
	winCount         int
	bestDebaterCount int
	scoreSum         float64 // 所在队伍总分累加(用于场均)
}

// statsStore 模型胜率统计存储(线程安全)。
type statsStore struct {
	mu    sync.RWMutex
	accum map[string]*modelStatsAccum // modelKey → 累加器
}

// newStatsStore 构造空统计存储。
func newStatsStore() *statsStore {
	return &statsStore{accum: make(map[string]*modelStatsAccum)}
}

// modelStatsDelta 单模型统计增量(纯数据)。
//
// 两个来源共用同一结构:
//   - computeStatsDeltas(单局 room+result)→ 进程内 applyDeltas + DB UPSERT 累加;
//   - t_lsm_game_debate_model_stats 历史行 → 启动时回读 applyDeltas。
type modelStatsDelta struct {
	TotalGames       int
	WinCount         int
	BestDebaterCount int
	ScoreSum         float64
}

// computeStatsDeltas 纯函数:由一局 (room, result) 计算每个辩方模型的统计增量。
//
// 规则(与 statsStore.recordGameResult 语义逐条一致):
//   - 仅统计辩方模型(裁判模型不参与胜负);
//   - TotalGames:该模型每个参赛座位 +1(同局同模型多座位分别计);
//   - WinCount:座位所在队伍 == WinnerTeamID;
//   - BestDebaterCount:座位 == 最佳辩手(队伍 + 座位均匹配);
//   - ScoreSum:座位所在队伍的最终总分(0-50)累加。
//
// 结果异常(全部 fallback)的对局也计入 —— 模型「参赛」事实成立,
// 但 IsAbnormal 时跳过胜负与最佳辩手,避免 fallback 随机胜方污染胜率。
// 该纯函数无副作用,是 §20260831-08 UPSERT 落库与进程内累加的共同事实来源。
func computeStatsDeltas(room *DebateRoom, res *DebateResult) map[string]modelStatsDelta {
	if room == nil || res == nil {
		return nil
	}

	teamScores := make(map[int]float64, len(res.TeamScores))
	for _, ts := range res.TeamScores {
		teamScores[ts.TeamID] = ts.TotalScore
	}

	deltas := make(map[string]modelStatsDelta)
	for _, team := range room.Config.Teams {
		for _, agent := range team.Agents {
			key := agent.ModelKey
			if key == "" {
				continue
			}
			d := deltas[key]
			d.TotalGames++
			if !res.IsAbnormal {
				if res.WinnerTeamID == team.TeamID {
					d.WinCount++
				}
				if res.BestDebater.TeamID == team.TeamID && res.BestDebater.Seat == agent.SeatID {
					d.BestDebaterCount++
				}
			}
			d.ScoreSum += teamScores[team.TeamID]
			deltas[key] = d
		}
	}
	return deltas
}

// recordGameResult 按一局评审结果累加统计(= computeStatsDeltas + applyDeltas)。
func (s *statsStore) recordGameResult(room *DebateRoom, res *DebateResult) {
	if s == nil {
		return
	}
	s.applyDeltas(computeStatsDeltas(room, res))
}

// applyDeltas 把一批增量累加进进程内统计(锁内合并)。
//
// 调用方:recordGameResult(每局)+ AttachPersistence 启动回读(DB 历史行)。
func (s *statsStore) applyDeltas(deltas map[string]modelStatsDelta) {
	if s == nil || len(deltas) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, d := range deltas {
		acc, ok := s.accum[key]
		if !ok {
			acc = &modelStatsAccum{}
			s.accum[key] = acc
		}
		acc.totalGames += d.TotalGames
		acc.winCount += d.WinCount
		acc.bestDebaterCount += d.BestDebaterCount
		acc.scoreSum += d.ScoreSum
	}
}

// exportDeltas 导出当前全量累计(结构同增量;供落库预热 / 调试)。
func (s *statsStore) exportDeltas() map[string]modelStatsDelta {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]modelStatsDelta, len(s.accum))
	for key, acc := range s.accum {
		out[key] = modelStatsDelta{
			TotalGames:       acc.totalGames,
			WinCount:         acc.winCount,
			BestDebaterCount: acc.bestDebaterCount,
			ScoreSum:         acc.scoreSum,
		}
	}
	return out
}

// snapshot 返回全量统计快照(按胜率降序 → 场次降序 → modelKey 升序)。
func (s *statsStore) snapshot() []ModelStats {
	if s == nil {
		return []ModelStats{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]ModelStats, 0, len(s.accum))
	for key, acc := range s.accum {
		ms := ModelStats{ModelKey: key}
		ms.TotalGames = acc.totalGames
		ms.WinCount = acc.winCount
		ms.BestDebaterCount = acc.bestDebaterCount
		if acc.totalGames > 0 {
			ms.WinRate = float64(acc.winCount) / float64(acc.totalGames)
			ms.AvgTotalScore = acc.scoreSum / float64(acc.totalGames)
			// 保留 2 位小数,前端直接展示
			ms.AvgTotalScore = float64(int(ms.AvgTotalScore*100+0.5)) / 100
		}
		out = append(out, ms)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].WinRate != out[j].WinRate {
			return out[i].WinRate > out[j].WinRate
		}
		if out[i].TotalGames != out[j].TotalGames {
			return out[i].TotalGames > out[j].TotalGames
		}
		return out[i].ModelKey < out[j].ModelKey
	})
	return out
}
