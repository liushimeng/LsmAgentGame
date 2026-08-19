// Package thpagent — tools.go: 德州扑克 Bot 工具集（Anthropic wire）。
//
// 按 [德州扑克Agent工具协议.md] §1 定义 2 个工具：
//   - poker_action  （必填）— 出牌决策,每轮 1 次
//   - poker_chat    （可选）— 公屏发言,每手牌最多 2 次
//
// 与狼人杀的差异：德州扑克只暴露 2 个工具,狼人杀有 5+ 个。
package thpagent

import (
	"LsmAgentGame/llm/types"
)

// BuildTools 返回固定 2 个工具的列表。
//
// 当前 v1.0 简化版：所有 bot 看到的工具相同（德州扑克信息不对称体现在 system prompt 而非 tool 裁剪）。
func BuildTools() []types.ToolDef {
	return []types.ToolDef{
		pokerActionTool(),
		pokerChatTool(),
	}
}

// pokerActionTool 构造 poker_action 工具定义。
//
// wire 形状：tool_use 块必带 {"type","id","name","input"} 四键；
// 服务端校验在 llm/types/types.go::ContentBlock.MarshalJSON() 按 Type 分支产出。
func pokerActionTool() types.ToolDef {
	return types.ToolDef{
		Name: "poker_action",
		Description: "出牌决策(必填 internal_thought)。每轮限调一次。" +
			"可选动作: fold(弃牌)/check(过牌)/call(跟注)/bet(下注)/raise(加注)/allin(全押)。" +
			"bet/raise 必须填 amount 字段(目标绝对金额)。" +
			"internal_thought 描述你的真实思考(牌面+赔率+对手风格+位置),不广播给其他玩家。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action": map[string]any{
					"type": "string",
					"enum": []string{"fold", "check", "call", "bet", "raise", "allin"},
				},
				"amount": map[string]any{
					"type":        "integer",
					"minimum":     0,
					"description": "bet/raise 的目标绝对金额(与 engine.Action.Amount 一致);fold/check/call 时填 0 或省略",
				},
				"internal_thought": map[string]any{
					"type":        "string",
					"maxLength":   200,
					"description": "仅 Agent 自己可见的内心独白(协议层隔离,不入 chat_message 表)",
				},
			},
			"required": []string{"action", "internal_thought"},
		},
	}
}

// pokerChatTool 构造 poker_chat 工具定义。
//
// 限流：每手牌 ≤ 2 次 + 相邻 ≥ 30s。
func pokerChatTool() types.ToolDef {
	return types.ToolDef{
		Name: "poker_chat",
		Description: "在公屏发言。每手牌最多 2 次,相邻 30s 节流。" +
			"text 是广播给所有玩家的发言,internal_thought 是你内心的思考(不广播)。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{
					"type":      "string",
					"minLength": 1,
					"maxLength": 80,
				},
				"internal_thought": map[string]any{
					"type":      "string",
					"maxLength": 200,
				},
			},
			"required": []string{"text"},
		},
	}
}