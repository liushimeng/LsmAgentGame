// Package debateplayer — 工具定义(LLM tool_use 格式)。
//
// 2026-08-31 §20260831-01 — 6 个工具定义:
//   - speech               正式发言
//   - cross_exam_question  质询提问
//   - cross_exam_answer    质询回答
//   - free_debate_speak    自由辩论发言
//   - finish_speak         主动结束发言
//   - idle_silent          沉默
//
// 详细字段定义见 docs/辩论比赛/05-辩论比赛工具与记忆系统设计.md §2。
package debateplayer

import (
	"LsmAgentGame/game/debate"
	"LsmAgentGame/llm"
)

// debateToolDefs 全部工具定义(按 ToolName → llm.ToolDef)。
//
// 设计:工具的 input_schema 严格遵循 §05 §2。
var debateToolDefs = map[debate.ToolName]llm.ToolDef{
	debate.ToolSpeech: {
		Name: string(debate.ToolSpeech),
		Description: "正式发言 — 在立论/驳论/质询小结/总结阶段提交发言正文。\n" +
			"字数限制:立论 ≤ 500 字,驳论 ≤ 400 字,小结 ≤ 400 字,总结 ≤ 600 字。\n" +
			"必须紧扣辩题和立场。引用对方观点时需注明来源(对方 X 辩)。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"content": map[string]any{
					"type":        "string",
					"description": "发言正文(纯文本,不使用 Markdown)",
				},
				"references": map[string]any{
					"type":  "array",
					"items": map[string]any{"type": "string"},
					"description": "引用的对方发言 ID 列表(可选)",
				},
				"internal_thought": map[string]any{
					"type":        "string",
					"description": "内部思考过程 — 观众可在「Agent 思考」面板看到,≤ 200 字",
				},
			},
			"required": []string{"content"},
		},
	},
	debate.ToolCrossExamQuestion: {
		Name: string(debate.ToolCrossExamQuestion),
		Description: "质询提问 — 向对方辩手发起质询问题。\n" +
			"规则:只能提问、不能阐述;问题需精准、有针对性;字数 ≤ 50 字。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"target_team": map[string]any{
					"type":        "integer",
					"description": "被质询队伍 ID",
				},
				"target_seat": map[string]any{
					"type":        "integer",
					"description": "被质询辩位(0-3,-1 表任意)",
				},
				"question": map[string]any{
					"type":        "string",
					"description": "质询问题,≤ 50 字",
				},
			},
			"required": []string{"target_team", "question"},
		},
	},
	debate.ToolCrossExamAnswer: {
		Name: string(debate.ToolCrossExamAnswer),
		Description: "质询回答 — 回答对方的质询问题。\n" +
			"规则:必须正面回应、不得回避或反问;字数 ≤ 100 字。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"question_id": map[string]any{
					"type":        "string",
					"description": "被回答的问题 ID",
				},
				"answer": map[string]any{
					"type":        "string",
					"description": "回答内容,≤ 100 字",
				},
			},
			"required": []string{"question_id", "answer"},
		},
	},
	debate.ToolFreeDebateSpeak: {
		Name: string(debate.ToolFreeDebateSpeak),
		Description: "自由辩论发言 — 在自由辩论环节发言。\n" +
			"字数 ≤ 80 字。需简短有力、针对性强。发言后自动交还发言权。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"content": map[string]any{
					"type":        "string",
					"description": "发言内容,≤ 80 字",
				},
			},
			"required": []string{"content"},
		},
	},
	debate.ToolFinishSpeak: {
		Name:        string(debate.ToolFinishSpeak),
		Description: "结束发言 — 主动交还发言权给对方队伍。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"reason": map[string]any{
					"type":        "string",
					"description": "结束原因(可选)",
				},
			},
			"required": []string{"reason"},
		},
	},
	debate.ToolIdleSilent: {
		Name:        string(debate.ToolIdleSilent),
		Description: "本轮不出声 — 选择不发言或放弃发言机会。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"reason": map[string]any{
					"type":        "string",
					"description": "选择沉默的原因,≤ 50 字",
				},
			},
			"required": []string{"reason"},
		},
	},
}

// ToolDefsByPhaseRole 按 phase + role 返回可见工具集(同 debate.AllowedToolsForPhaseRole)。
func ToolDefsByPhaseRole(phase debate.Phase, role debate.Role) []llm.ToolDef {
	allowed := debate.AllowedToolsForPhaseRole(phase, role)
	out := make([]llm.ToolDef, 0, len(allowed))
	for _, t := range allowed {
		if def, ok := debateToolDefs[t]; ok {
			out = append(out, def)
		}
	}
	return out
}