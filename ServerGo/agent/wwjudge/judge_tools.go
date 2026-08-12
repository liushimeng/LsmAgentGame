// Package agent — judge_tools.go: 法官(judge)的工具定义与派发。
//
// 2026-07-12 §127: 法官原本只有 stub,现在补充工具集定义(announce /
// declare_cause / prompt_actor / summary / idle_silent),供 BuildJudgeTools
// 在 AgentJudge.handleEvent 调 LLM 时作为 tools[] 下发。LLM 调用工具返回
// tool_use 时由 DispatchJudgeTool 派发 — 当前实现以 transcript 记录为主,
// 因为法官 LLM 仅做宣告,实际游戏状态由 watchdog / manager 推进,法官不
// 直接修改。
package wwjudge

import (
	"LsmAgentGame/llm"
)

// BuildJudgeTools 返回法官 LLM 的工具集(announce / declare_cause /
// prompt_actor / summary / idle_silent)。所有工具的输入 schema 在 §123
// 设计中定义;此处只构造 Anthropic 协议所需的 JSON schema。
func BuildJudgeTools() []llm.ToolDef {
	schema := func(props map[string]any, required ...string) map[string]any {
		if required == nil {
			required = []string{}
		}
		return map[string]any{
			"type":       "object",
			"properties": props,
			"required":   required,
		}
	}
	return []llm.ToolDef{
		{
			Name: "announce",
			Description: "公开宣告 — 法官对全体玩家的口语播报,会被广播到房间聊天。" +
				"\n适用:每个阶段切换时的开场白 / 黎明公布死亡 / 投票放逐后的胜负提示。" +
				"\n≤ 80 字,简洁有力,纯文本。",
			InputSchema: schema(map[string]any{
				"kind": map[string]any{"type": "string", "description": "宣告类型(如 judge_dawn_announce/judge_speak_start/judge_filling_welcome 等)"},
				"text": map[string]any{"type": "string", "description": "公开宣告文本,≤80字"},
			}, "kind", "text"),
		},
		{
			Name: "declare_cause",
			Description: "宣告玩家死亡原因 + 决断(execution/death)。" +
				"\n适用:玩家被处决/死亡时,把死因广播给全体玩家。" +
				"\nverdict 必须是 \"execution\"(处决,玩家集体决策)或 \"death\"(死亡,夜间/技能)。",
			InputSchema: schema(map[string]any{
				"seat":    map[string]any{"type": "integer", "description": "死亡座位号(0-indexed)"},
				"cause":   map[string]any{"type": "string", "description": "wolf/vote/hunter/witch_poison/suicide"},
				"verdict": map[string]any{"type": "string", "enum": []string{"execution", "death"}, "description": "处决或死亡"},
				"text":    map[string]any{"type": "string", "description": "宣告文本,≤80字"},
			}, "seat", "verdict", "text"),
		},
		{
			Name: "prompt_actor",
			Description: "强提示当前 acting bot:在阶段切换后由法官追问/提醒当前行动者。" +
				"\n适用:acting bot 思考慢,法官可广播一个温和提醒。",
			InputSchema: schema(map[string]any{
				"seat": map[string]any{"type": "integer", "description": "当前 acting 座位号(0-indexed)"},
				"text": map[string]any{"type": "string", "description": "强提示文本,≤80字"},
			}, "seat", "text"),
		},
		{
			Name: "summary",
			Description: "整局总结 — 一局结束时生成 5 段式复盘(胜方 / 关键翻盘 / 时间线 / MVP / 悍跳记录)。" +
				"\n每段 ≤ 80 字,总长 ≤ 400 字。仅在 GameOver 后调用。",
			InputSchema: schema(map[string]any{
				"outcome":       map[string]any{"type": "string", "description": "胜方阵营 wolf/good"},
				"key_moments":   map[string]any{"type": "string", "description": "关键翻盘点"},
				"timeline":      map[string]any{"type": "string", "description": "角色操作时间线"},
				"mvp":           map[string]any{"type": "string", "description": "MVP 玩家 seat + 简短理由"},
				"wolf_decoy_log": map[string]any{"type": "string", "description": "狼人悍跳记录"},
			}, "outcome", "key_moments", "timeline", "mvp", "wolf_decoy_log"),
		},
		{
			Name: "idle_silent",
			Description: "本阶段不出声 — LLM 主动选择沉默(无重要事可说)。" +
				"\n不广播、不发消息、仅在 JudgeTranscript 留 [idle_silent] 审计行。",
			InputSchema: schema(map[string]any{
				"reason": map[string]any{"type": "string", "description": "选择沉默的原因,≤50字"},
			}, "reason"),
		},
	}
}

// DispatchJudgeTool 把法官 LLM 返回的 tool_use block 转换为
// JudgeTranscript 记录(供前端 JudgePanel 渲染)。实际游戏状态变更
// 仍由 watchdog / host driver 推进,法官工具调用只产生 transcript。
//
// 返回值:result 是 LLM 看到的 tool_result 字符串(用于下一轮 LLM 决策);
// updated 是修改后的 transcript 快照(已 recordAnnouncement)。
func DispatchJudgeTool(toolName string, input map[string]any, j *AgentJudge, transcript *JudgeTranscript) (result string, updated *JudgeTranscript) {
	if transcript == nil {
		transcript = &JudgeTranscript{}
	}
	switch toolName {
	case "announce":
		text, _ := input["text"].(string)
		transcript.LastAnnouncement = text
		transcript.LastTool = "announce"
		transcript.RecentAnnouncements = appendRecentString(transcript.RecentAnnouncements, text, 10)
		transcript.ToolCalls = appendRecentString(transcript.ToolCalls, "announce", 5)
		j.appendActivity("announce", truncateJudgeInput(text), text, 0)
		// 2026-07-16 主持人重构:announce 进公屏。
		if j.onAnnounce != nil {
			kind, _ := input["kind"].(string)
			j.onAnnounce(j.RoomID, text, kind)
		}
		return "announce: 已广播", transcript
	case "declare_cause":
		text, _ := input["text"].(string)
		verdict, _ := input["verdict"].(string)
		cause, _ := input["cause"].(string)
		transcript.LastAnnouncement = text
		transcript.LastTool = "declare_cause"
		transcript.RecentAnnouncements = appendRecentString(transcript.RecentAnnouncements, text, 10)
		transcript.ToolCalls = appendRecentString(transcript.ToolCalls, "declare_cause:"+verdict+":"+cause, 5)
		j.appendActivity("declare_cause", verdict+"/"+cause, text, 0)
		// 2026-07-16 主持人重构:declare_cause 进公屏。
		if j.onAnnounce != nil {
			j.onAnnounce(j.RoomID, text, "declare_cause:"+verdict)
		}
		return "declare_cause: 已广播死亡宣告(" + verdict + ")", transcript
	case "prompt_actor":
		text, _ := input["text"].(string)
		transcript.LastAnnouncement = text
		transcript.LastTool = "prompt_actor"
		transcript.RecentAnnouncements = appendRecentString(transcript.RecentAnnouncements, text, 10)
		j.appendActivity("prompt_actor", truncateJudgeInput(text), text, 0)
		// 2026-08-05 §Agent聊天显示优化 (B6, 修复 P1-1):prompt_actor 与
		// announce / declare_cause 对称进公屏。此前只写 JudgeTranscript,
		// 法官「请 N 号行动」这类提醒在 房间聊天 里完全看不到。
		if j.onAnnounce != nil {
			j.onAnnounce(j.RoomID, text, "prompt_actor")
		}
		return "prompt_actor: 已提醒", transcript
	case "summary":
		// summary 工具实际由 judge_summary.go::GenerateSummary 处理;
		// 此处仅记录工具调用,真实总结生成在 EmitGameOverSummary 路径。
		transcript.LastTool = "summary"
		transcript.ToolCalls = appendRecentString(transcript.ToolCalls, "summary", 5)
		j.appendActivity("summary", "", "已触发整局总结生成", 0)
		return "summary: 已触发整局总结生成", transcript
	case "idle_silent":
		reason, _ := input["reason"].(string)
		transcript.LastTool = "idle_silent"
		transcript.ToolCalls = appendRecentString(transcript.ToolCalls, "idle_silent:"+reason, 5)
		j.appendActivity("idle_silent", reason, "本轮不出声", 0)
		return "idle_silent: 本轮不出声", transcript
	default:
		return "unknown tool: " + toolName, transcript
	}
}