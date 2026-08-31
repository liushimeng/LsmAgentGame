// Package debate — 模型公平分配算法 + 队伍自动配置。
//
// 2026-08-31 §20260831-01 — 公平性约束实现:
//   - 每队内模型尽量不重复(蛇形 + Fisher-Yates 洗牌)
//   - 裁判使用与辩方不同的模型
//   - 立场平衡:正反方模型能力尽量对等
//
// 详见 docs/辩论比赛/03-辩论比赛房间创建与配置设计.md §5
//  + docs/辩论比赛/06-辩论比赛公平性与评审系统设计.md §2。
package debate

import (
	"fmt"
	"math/rand"
)

// FairModelAssignment 公平模型分配。
//
// 参数:
//   - teamCount: 队伍数(2/3/4/5)
//   - agentsPerTeam: 每队人数(2/3/4)
//   - judgeCount: 裁判数(固定 3)
//   - availableModels: 系统可用模型池(8 个 provider)
//   - modelWinRates: 历史胜率(可空)
//
// 返回:
//   - teamAssignments: [teamID][seatID] → modelKey
//   - judgeAssignments: [judgeIdx] → modelKey
func FairModelAssignment(
	teamCount, agentsPerTeam, judgeCount int,
	availableModels []string,
	modelWinRates map[string]float64,
) (teamAssignments map[int]map[int]string, judgeAssignments []string, err error) {

	if teamCount < 2 || teamCount > 5 {
		return nil, nil, fmt.Errorf("debate: invalid teamCount=%d (must be 2..5)", teamCount)
	}
	if agentsPerTeam < 2 || agentsPerTeam > 4 {
		return nil, nil, fmt.Errorf("debate: invalid agentsPerTeam=%d (must be 2..4)", agentsPerTeam)
	}
	if judgeCount < 1 {
		return nil, nil, fmt.Errorf("debate: invalid judgeCount=%d (must be >= 1)", judgeCount)
	}
	if len(availableModels) < teamCount*agentsPerTeam {
		return nil, nil, fmt.Errorf("debate: need at least %d models, got %d",
			teamCount*agentsPerTeam, len(availableModels))
	}

	// Step 1: Fisher-Yates 洗牌
	shuffled := make([]string, len(availableModels))
	copy(shuffled, availableModels)
	rand.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})

	// Step 2: 按历史胜率排序(平衡模式):胜率高 → 末尾,胜率低 → 开头。
	// 这样蛇形分配时,强模型会均匀散到各队,而非集中在一队。
	if modelWinRates != nil {
		sortByWinRate(shuffled, modelWinRates)
	}

	// Step 3: 蛇形分配。
	// - 第 1 轮(a=0):正序填各队 → 强模型先给 0 队
	// - 第 2 轮(a=1):逆序填各队 → 强模型避开 0 队
	// 整体效果:每队获得近似均等的"强 + 弱"组合,实现正反方均衡。
	teamAssignments = make(map[int]map[int]string, teamCount)
	for t := 0; t < teamCount; t++ {
		teamAssignments[t] = make(map[int]string, agentsPerTeam)
	}
	idx := 0
	for a := 0; a < agentsPerTeam; a++ {
		if a%2 == 0 {
			// 正序
			for t := 0; t < teamCount; t++ {
				teamAssignments[t][a] = shuffled[idx%len(shuffled)]
				idx++
			}
		} else {
			// 逆序
			for t := teamCount - 1; t >= 0; t-- {
				teamAssignments[t][a] = shuffled[idx%len(shuffled)]
				idx++
			}
		}
	}

	// Step 4: 分配裁判(使用与辩方不同的模型)。
	usedByTeams := make(map[string]bool, teamCount*agentsPerTeam)
	for t := 0; t < teamCount; t++ {
		for a := 0; a < agentsPerTeam; a++ {
			usedByTeams[teamAssignments[t][a]] = true
		}
	}

	candidates := make([]string, 0, len(shuffled))
	for _, m := range shuffled {
		if !usedByTeams[m] {
			candidates = append(candidates, m)
		}
	}

	judgeAssignments = make([]string, 0, judgeCount)
	for j := 0; j < judgeCount; j++ {
		if j < len(candidates) {
			judgeAssignments = append(judgeAssignments, candidates[j])
		} else {
			// 候选不足,允许重复(从 shuffled 池轮询)
			judgeAssignments = append(judgeAssignments, shuffled[j%len(shuffled)])
		}
	}

	return teamAssignments, judgeAssignments, nil
}

// sortByWinRate 升序排列(胜率低在前)。
func sortByWinRate(models []string, rates map[string]float64) {
	// 简单插入排序(切片很小,无需更高效实现)
	for i := 1; i < len(models); i++ {
		for j := i; j > 0; j-- {
			if rates[models[j-1]] > rates[models[j]] {
				models[j-1], models[j] = models[j], models[j-1]
			} else {
				break
			}
		}
	}
}

// ValidateAssignment 验证模型分配的合法性(给手动配置用)。
//
// 校验项:
//   - 每队内不重复
//   - 裁判与辩方不重复
//   - 裁判之间不重复
func ValidateAssignment(teamAssignments map[int]map[int]string, judgeAssignments []string) error {
	// 1. 每队内不重复
	for teamID, agents := range teamAssignments {
		seen := make(map[string]bool, len(agents))
		for _, model := range agents {
			if seen[model] {
				return fmt.Errorf("debate: team %d has duplicate model: %s", teamID, model)
			}
			seen[model] = true
		}
	}
	// 2. 裁判与辩方不重复
	usedByTeams := make(map[string]bool)
	for _, agents := range teamAssignments {
		for _, model := range agents {
			usedByTeams[model] = true
		}
	}
	for _, jm := range judgeAssignments {
		if usedByTeams[jm] {
			return fmt.Errorf("debate: judge model %s is also used by a team", jm)
		}
	}
	// 3. 裁判之间不重复
	seenJudges := make(map[string]bool, len(judgeAssignments))
	for _, jm := range judgeAssignments {
		if seenJudges[jm] {
			return fmt.Errorf("debate: duplicate judge model: %s", jm)
		}
		seenJudges[jm] = true
	}
	return nil
}

// DefaultStancesForMode 根据 Mode 返回默认立场分配。
//
// 设计见 docs/辩论比赛/03-辩论比赛房间创建与配置设计.md §3.3。
func DefaultStancesForMode(mode Mode) []Stance {
	switch mode {
	case ModeTwoTeam:
		return []Stance{StancePro, StanceCon}
	case ModeThreeTeam:
		return []Stance{StancePro, StanceCon, StanceNeutral}
	case ModeFourTeam:
		return []Stance{StanceGovUpper, StanceGovLower, StanceOppUpper, StanceOppLower}
	case ModeFiveTeam:
		return []Stance{StanceAngle1, StanceAngle2, StanceAngle3, StanceAngle4, StanceAngle5}
	default:
		return []Stance{StancePro, StanceCon}
	}
}

// DefaultRolesForTeamSize 根据队伍人数返回默认辩位配置。
//
// 2 人 → 一辩 + 四辩(精简版,只做立论 + 总结)
// 3 人 → 一辩 + 二辩 + 四辩(略去三辩质询)
// 4 人 → 一辩 + 二辩 + 三辩 + 四辩(完整版)
func DefaultRolesForTeamSize(n int) []Role {
	switch n {
	case 2:
		return []Role{RoleFirst, RoleFourth}
	case 3:
		return []Role{RoleFirst, RoleSecond, RoleFourth}
	case 4:
		return []Role{RoleFirst, RoleSecond, RoleThird, RoleFourth}
	default:
		return []Role{RoleFirst, RoleSecond, RoleThird, RoleFourth}
	}
}

// MaxSpeechCharsForPhase 根据阶段返回发言字数上限。
func MaxSpeechCharsForPhase(phase Phase, cfg PhaseConfig) int {
	switch phase {
	case PhaseOpeningArgument:
		return cfg.MaxSpeechChars
	case PhaseRebuttal:
		return cfg.MaxRebuttalChars
	case PhaseCrossExamSummary:
		return cfg.MaxRebuttalChars // 小结同驳论
	case PhaseClosingArgument:
		return cfg.MaxClosingChars
	case PhaseFreeDebate:
		return cfg.MaxFreeDebateChars
	case PhaseCrossExamination:
		return cfg.MaxCrossExamQChars // 提问阶段上限,回答由 AChars 控制
	default:
		return cfg.MaxSpeechChars
	}
}

// MaxAnswerCharsForPhase 质询回答字数上限(PhaseCrossExamination 专用)。
func MaxAnswerCharsForPhase(_ Phase, cfg PhaseConfig) int {
	return cfg.MaxCrossExamAChars
}

// PhaseDurationSec 返回阶段时长(秒)。
func PhaseDurationSec(phase Phase, cfg PhaseConfig) int {
	switch phase {
	case PhasePreparation:
		return cfg.PreparationSec
	case PhaseOpeningArgument:
		return cfg.OpeningArgumentSec
	case PhaseRebuttal:
		return cfg.RebuttalSec
	case PhaseCrossExamination:
		return cfg.CrossExamSec
	case PhaseCrossExamSummary:
		return cfg.CrossExamSummarySec
	case PhaseFreeDebate:
		return cfg.FreeDebateSec
	case PhaseClosingArgument:
		return cfg.ClosingArgumentSec
	case PhaseJudging:
		return cfg.JudgingSec
	case PhaseResult:
		return cfg.ResultShowSec
	default:
		return 30
	}
}