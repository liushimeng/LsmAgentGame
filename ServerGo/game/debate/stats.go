// Package debate — 模型胜率统计(§20260831-06)。
//
// 设计依据:docs/辩论比赛/06-辩论比赛公平性与评审系统设计.md §9「历史统计」:
//
//	每局评审结果产出后,按辩手模型累加 TotalGames / WinCount /
//	BestDebaterCount / 队伍总分;GET /api/games/debate/stats 返回快照。
//
// 首期定位:进程内统计(与辩论房间同为 in-memory);未来可落库
// t_lsm_game_debate_model_stats 做跨重启持久化(§03 §8 数据库设计延伸)。
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

// recordGameResult 按一局评审结果累加统计。
//
// 规则:
//   - 仅统计辩方模型(裁判模型不参与胜负);
//   - TotalGames:该模型每个参赛座位 +1(同局同模型多座位分别计);
//   - WinCount:座位所在队伍 == WinnerTeamID;
//   - BestDebaterCount:座位 == 最佳辩手(队伍 + 座位均匹配);
//   - AvgTotalScore:座位所在队伍的最终总分(0-50)累加后取场均。
//
// 结果异常(全部 fallback)的对局也计入 —— 模型「参赛」事实成立,
// 但 IsAbnormal 时跳过胜负与最佳辩手,避免 fallback 随机胜方污染胜率。
func (s *statsStore) recordGameResult(room *DebateRoom, res *DebateResult) {
	if s == nil || room == nil || res == nil {
		return
	}

	teamScores := make(map[int]float64, len(res.TeamScores))
	for _, ts := range res.TeamScores {
		teamScores[ts.TeamID] = ts.TotalScore
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, team := range room.Config.Teams {
		for _, agent := range team.Agents {
			key := agent.ModelKey
			if key == "" {
				continue
			}
			acc, ok := s.accum[key]
			if !ok {
				acc = &modelStatsAccum{}
				s.accum[key] = acc
			}
			acc.totalGames++
			if !res.IsAbnormal {
				if res.WinnerTeamID == team.TeamID {
					acc.winCount++
				}
				if res.BestDebater.TeamID == team.TeamID && res.BestDebater.Seat == agent.SeatID {
					acc.bestDebaterCount++
				}
			}
			acc.scoreSum += teamScores[team.TeamID]
		}
	}
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
