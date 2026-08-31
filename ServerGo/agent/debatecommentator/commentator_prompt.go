// Package debatecommentator — 解说 Agent prompt 构建(2026-08-31 §20260831-03)。
package debatecommentator

import (
	"fmt"
	"strings"
)

// buildSystemPrompt 构造解说 system prompt。
func buildSystemPrompt(style string) string {
	if style == "fun" {
		return `【辩论比赛解说 — 轻松吐槽风格】
你是一场 AI 辩论比赛的实时解说员,负责为观众提供轻松有趣的解说。
你的解说风格:幽默、接地气、适当吐槽,让观众在娱乐中理解辩论进程。
要求:
- 解说 ≤ 100 字,简短有力
- 可以适当吐槽辩手的逻辑漏洞或精彩表现
- 不要人身攻击,保持善意幽默
- 使用口语化表达,像体育解说一样有激情
- 不要使用 Markdown/JSON`
	}
	// 默认 pro 风格
	return `【辩论比赛解说 — 专业严谨风格】
你是一场 AI 辩论比赛的实时解说员,负责为观众提供专业、客观的解说。
你的解说风格:严谨、专业、有深度,帮助观众理解辩论的战术与逻辑。
要求:
- 解说 ≤ 100 字,简短有力
- 分析辩手的论证策略、逻辑链条、交锋焦点
- 客观中立,不偏向任何一方
- 使用专业术语,但让普通观众也能理解
- 不要使用 Markdown/JSON`
}

// buildUserPrompt 构造解说 user prompt(基于 CommentarySnapshot)。
func buildUserPrompt(snap *CommentarySnapshot) string {
	var b strings.Builder

	// 辩题与阶段
	b.WriteString(fmt.Sprintf("【辩题】%s\n", snap.Topic))
	b.WriteString(fmt.Sprintf("【当前阶段】%s\n", snap.PhaseCN))
	b.WriteString(fmt.Sprintf("【队伍数】%d 支队伍\n\n", snap.TeamCount))

	// 最近发言
	if len(snap.RecentSpeeches) > 0 {
		b.WriteString("【最近发言】\n")
		for _, sp := range snap.RecentSpeeches {
			b.WriteString(fmt.Sprintf("  [%s/%s] %s: %s\n",
				sp.PhaseCN, sp.StanceLabel, sp.SpeakerName,
				truncateForCommentary(sp.Content, 60)))
		}
		b.WriteString("\n")
	}

	// 当前比分(评审阶段后)
	if len(snap.TeamScores) > 0 {
		b.WriteString("【当前比分】\n")
		for _, ts := range snap.TeamScores {
			b.WriteString(fmt.Sprintf("  %s: %.1f 分\n", ts.TeamName, ts.TotalScore))
		}
		b.WriteString("\n")
	}

	// 事件特定引导
	b.WriteString("【本轮任务】\n")
	switch snap.EventKind {
	case CommentaryPendingPhaseChange:
		phaseName, _ := snap.Extra["phase_cn"].(string)
		b.WriteString(fmt.Sprintf("阶段切换至 %s,请简要点评当前局势并展望下一阶段。", phaseName))
	case CommentaryPendingSpeech:
		speaker, _ := snap.Extra["speaker_name"].(string)
		b.WriteString(fmt.Sprintf("选手 %s 完成发言,请简要点评其论证亮点或不足。", speaker))
	case CommentaryPendingCrossExam:
		b.WriteString("质询交锋完成,请点评双方的质询技巧与回答质量。")
	case CommentaryPendingJudgeScore:
		b.WriteString("裁判评分已公布,请点评各队表现与得分合理性。")
	case CommentaryPendingGameOver:
		winner, _ := snap.Extra["winner_team_name"].(string)
		b.WriteString(fmt.Sprintf("比赛结束,胜方是 %s! 请做赛后总结点评。", winner))
	default:
		b.WriteString("请基于当前局势做简短解说(≤ 100 字)。")
	}

	return b.String()
}

// truncateForCommentary 截断文本用于解说 prompt。
func truncateForCommentary(s string, max int) string {
	count := 0
	for i := range s {
		if count == max {
			return s[:i] + "..."
		}
		count++
	}
	return s
}
