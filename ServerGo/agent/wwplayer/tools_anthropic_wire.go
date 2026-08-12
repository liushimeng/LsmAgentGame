// Package agent — tools_anthropic_wire.go: Agent ToolRegistry → Anthropic wire 转换。
//
// 2026-07-21 v5 重构（docs/狼人杀-道具与经济/狼人杀13人局道具系统设计.md §16.2.4 +
// docs/AgentAnthropic工具集与道具协议.md 第 5 章）：
//
//   - v4 把 Agent 工具 schema 直接塞到 anthropic.ChatRequest.Tools，schema 由调用
//     方（agent.Run）传入；Anthropic provider 只做字段拷贝（`req.Tools`），不
//     关心字段顺序。
//   - v5 把"从 ToolRegistry 拉工具列表 + 转 Anthropic wire 格式"明确为
//     BuildAnthropicToolDefs(specs) 一处单一入口；新增的字段（如未来 `_meta.version`）
//     只需要改这里一处。
//   - 测试在 tools_anthropic_wire_test.go 验证字段顺序稳定性（CLAUDE.md §14.1）。
//
// 约束：
//   - wire 字段顺序由 llm/types.ToolDef struct 字段顺序保证（name → description → input_schema）。
//   - 不在这里做 normalization / sanitize；统一在 anthropic.go 的预飞归一化处。
//
// 2026-07-21 道具系统 v5 重构。
package wwplayer

import "LsmAgentGame/agent/wwtypes"

import (
	"LsmAgentGame/llm/types"
)

// BuildAnthropicToolDefs 把 agent 包 ToolRegistry 的 spec 列表转换为 Anthropic
// wire 格式（llm/types.ToolDef）。供 anthropic 包构造 ChatRequest.Tools 时调用。
//
// 参数：
//   - specs    registry 过滤后的 spec（来自 MountTools）
//
// 字段顺序保证：types.ToolDef json 序列化按 struct 字段顺序（name → description →
// input_schema），与 Anthropic Messages API 规范一致；本函数不引入额外字段。
//
// 失败处理：spec.Name 缺失时跳过；Description 缺失时退化为 Name（避免空 desc
// 被某些 Anthropic 上游拒绝）；InputSchema nil 时退化为空 object schema。
//
// 线程安全：纯函数；调用方负责 spec 列表的生命周期。
func BuildAnthropicToolDefs(specs []*ToolSpec) []types.ToolDef {
	if len(specs) == 0 {
		return nil
	}
	out := make([]types.ToolDef, 0, len(specs))
	for _, s := range specs {
		if s == nil || s.Name == "" {
			continue
		}
		desc := s.Description
		if desc == "" && s.BuildDescription != nil {
			desc = s.BuildDescription(nil)
		}
		if desc == "" {
			desc = s.Name
		}
		schema := s.Builder(nil)
		if schema == nil {
			schema = map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			}
		}
		out = append(out, types.ToolDef{
			Name:        s.Name,
			Description: desc,
			InputSchema: schema,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// BuildAnthropicToolDefsForPhase 便捷方法：按 phase 拉 registry 并转 wire。
func BuildAnthropicToolDefsForPhase(phase ToolPhase, gc *wwtypes.GameContext) []types.ToolDef {
	return BuildAnthropicToolDefs(MountTools(phase, gc))
}