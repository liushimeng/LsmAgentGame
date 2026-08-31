// Package debate — 评审阶段具体实现。
//
// 2026-08-31 §20260831-01 — 评审阶段首期实现:
//
//   - 触发 3 个裁判 Agent 并发评审
//   - 收集评分 → 中位数聚合 → 产生 DebateResult
//   - 中位数为单数裁判(3 取中)天然支持
//
// 详细设计见 docs/辩论比赛/06-辩论比赛公平性与评审系统设计.md §4。
package debate

import (
	"sort"
)

// ComputeFinalScores 聚合 3 份裁判评分,产出最终 TeamFinalScore[]。
//
// 算法:每个队伍的每维度取 3 裁判中位数(排序后取第 2 个)。
//
// 设计依据:docs/辩论比赛/06 §4.3。
func ComputeFinalScores(judges []JudgeScore, teamCount int) []TeamFinalScore {
	results := make([]TeamFinalScore, teamCount)

	for tid := 0; tid < teamCount; tid++ {
		var totals []float64
		dims := map[string][]float64{
			"argument_quality":       {},
			"logic_rigor":            {},
			"language_expression":    {},
			"team_coordination":      {},
			"rebuttal_effectiveness": {},
		}

		for _, js := range judges {
			for _, r := range js.Rankings {
				if r.TeamID != tid {
					continue
				}
				totals = append(totals, r.TotalScore)
				dims["argument_quality"] = append(dims["argument_quality"], float64(r.Scores.ArgumentQuality))
				dims["logic_rigor"] = append(dims["logic_rigor"], float64(r.Scores.LogicRigor))
				dims["language_expression"] = append(dims["language_expression"], float64(r.Scores.LanguageExpression))
				dims["team_coordination"] = append(dims["team_coordination"], float64(r.Scores.TeamCoordination))
				dims["rebuttal_effectiveness"] = append(dims["rebuttal_effectiveness"], float64(r.Scores.RebuttalEffectiveness))
			}
		}

		dimAverages := map[string]float64{}
		for dim, vs := range dims {
			if len(vs) == 0 {
				dimAverages[dim] = 0
				continue
			}
			sort.Float64s(vs)
			dimAverages[dim] = vs[len(vs)/2]
		}

		medianScore := median(totals)

		results[tid] = TeamFinalScore{
			TeamID:          tid,
			TotalScore:      medianScore,
			DimensionScores: dimAverages,
			Rank:            0, // 由 DetermineRanks 回填
		}
	}

	// 回填 Rank
	DetermineRanks(results)

	// 补 TeamName
	for i := range results {
		results[i].TeamName = teamNameByID(teamCount, results[i].TeamID)
	}

	return results
}

// median 返回切片的中位数(len=0 → 0)。
func median(vs []float64) float64 {
	if len(vs) == 0 {
		return 0
	}
	sorted := make([]float64, len(vs))
	copy(sorted, vs)
	sort.Float64s(sorted)
	return sorted[len(sorted)/2]
}

// DetermineRanks 按总分降序排名(填充 TeamFinalScore.Rank)。
func DetermineRanks(scores []TeamFinalScore) {
	// 按 TotalScore 降序排序(原序保留作为 tiebreaker)
	idx := make([]int, len(scores))
	for i := range scores {
		idx[i] = i
	}
	sort.SliceStable(idx, func(i, j int) bool {
		return scores[idx[i]].TotalScore > scores[idx[j]].TotalScore
	})
	for rank, i := range idx {
		scores[i].Rank = rank + 1
	}
}

// DetermineWinner 根据多数决确定胜方 + 最佳辩手。
//
// 返回 (winnerTeamID, bestDebaterSeat)。
func DetermineWinner(judges []JudgeScore) (int, int) {
	if len(judges) == 0 {
		return 0, 0
	}

	// 1) 多数决确定 winnerTeamID
	winVotes := make(map[int]int)
	for _, js := range judges {
		winVotes[js.WinnerTeamID]++
	}
	maxVotes := 0
	winner := 0
	for tid, v := range winVotes {
		if v > maxVotes {
			maxVotes = v
			winner = tid
		}
	}

	// 2) 最佳辩手 = winner 队内得票最多的 seat
	bestVotes := make(map[int]int)
	for _, js := range judges {
		for _, r := range js.Rankings {
			if r.TeamID != winner {
				continue
			}
			bestVotes[r.BestDebater]++
		}
	}
	maxBest := 0
	bestSeat := 0
	for s, v := range bestVotes {
		if v > maxBest {
			maxBest = v
			bestSeat = s
		}
	}

	return winner, bestSeat
}

// FallbackJudgeScore 生成 fallback 评分(用于 LLM 调用失败兜底)。
//
// 所有队伍均给 5 分(中位),评语 = "由于技术原因使用默认评分"。
func FallbackJudgeScore(judgeID int, modelKey string, teamCount int) JudgeScore {
	rankings := make([]TeamRanking, teamCount)
	for t := 0; t < teamCount; t++ {
		rankings[t] = TeamRanking{
			TeamID: t,
			Scores: ScoreDimensions{
				ArgumentQuality:       5,
				LogicRigor:            5,
				LanguageExpression:    5,
				TeamCoordination:      5,
				RebuttalEffectiveness: 5,
			},
			TotalScore:  25.0,
			Comment:     "由于技术原因,裁判使用默认评分。",
			BestDebater: 0,
		}
	}
	return JudgeScore{
		JudgeID:        judgeID,
		ModelKey:       modelKey,
		Rankings:       rankings,
		OverallComment: "由于技术原因,使用默认评审结果。",
		WinnerTeamID:   0,
		IsFallback:     true,
	}
}

// teamNameByID 由队伍 ID 取名字(用于 ClientState.TeamScores[i].TeamName)。
func teamNameByID(teamCount, teamID int) string {
	// 简化版:返回"队伍 N"
	return "队伍" + fmtInt(teamID+1)
}

// BuildResult 构造 DebateResult(评审完成后调用)。
//
// 入参:judges = 0~3 份评分;teamCount 决定 results 长度;teams 用于补 TeamName。
func BuildResult(judges []JudgeScore, teamCount int) *DebateResult {
	winner, bestSeat := DetermineWinner(judges)
	teamScores := ComputeFinalScores(judges, teamCount)

	var winnerName string
	for _, t := range teamScores {
		if t.TeamID == winner {
			winnerName = t.TeamName
			break
		}
	}

	return &DebateResult{
		WinnerTeamID:   winner,
		WinnerTeamName: winnerName,
		BestDebater: BestDebaterInfo{
			Seat:     bestSeat,
			TeamID:   winner,
			Name:     teamNameByID(teamCount, winner),
			Votes:    len(judges),
		},
		TeamScores:   teamScores,
		JudgeDetails: judges,
		IsAbnormal:   false,
	}
}