// Package agent — commitment_tools.go: 行为承诺工具（§20260810-06）。
//
// 注册 1 个工具：
//   - public_commit（PhaseSpeak，白天发言阶段可用）
//
// 设计要点：
//   - 承诺是公开信息（所有玩家可见承诺内容），但兑现状态仅本人+观战者可见（§135）。
//   - 计入 speakLimiter（与发言共享令牌桶，防刷屏）。
//   - 每人每天最多 3 条（服务端硬限制）。
//
// 2026-08-10 §20260810-06。
package wwplayer

import (
	"LsmWebGame/agent/wwtypes"
)

// init — 注册 public_commit 工具到全局 registry。
func init() {
	RegisterTool(&ToolSpec{
		Name:        "public_commit",
		Description: "公开做出一个可验证的行为承诺。所有玩家都会看到你做出了承诺，但兑现状态只有你自己和观战者知道（终局时全部公开）。承诺是博弈武器：高兑现率 = 高信任度，低兑现率 = 被怀疑。支持 5 种模板：seer_check（预言家查验）/ vote_target（投票目标）/ no_vote_for（不投票给某人）/ no_use_skill（不使用技能）/ apology_if_good（赛后道歉）。",
		Phase:       ToolPhaseSpeak,
		Category:    "commitment",
		Builder:     buildPublicCommitSchema,
		MountIf:     nil, // 始终挂载（白天发言阶段任何存活玩家可承诺）
		Dispatcher:  dispatchPublicCommit,
	})
}

// buildPublicCommitSchema 生成 public_commit 工具的 input_schema。
func buildPublicCommitSchema(gc *wwtypes.GameContext) map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"template": map[string]any{
				"type":        "string",
				"enum":        []string{"seer_check", "vote_target", "no_vote_for", "no_use_skill", "apology_if_good"},
				"description": "承诺模板类型",
			},
			"target_seat": map[string]any{
				"type":        "integer",
				"description": "目标座位号（0-indexed，no_use_skill 模板可填 -1）",
			},
			"reason": map[string]any{
				"type":        "string",
				"description": "做出此承诺的公开理由（≤30字，所有玩家可见）",
			},
		},
		"required": []string{"template", "target_seat", "reason"},
	}
}

// PublicCommitRunner 是 public_commit 工具的运行器接口。
// 由 werewolf.agentRunner 实现，转发到 WerewolfManager.Action_PublicCommit。
type PublicCommitRunner interface {
	PublicCommit(template string, targetSeat int, reason string) (string, error)
}

// dispatchPublicCommit 派发 public_commit。
func dispatchPublicCommit(args map[string]any, gc *wwtypes.GameContext, runner ToolRunner) (string, error) {
	template, _ := args["template"].(string)
	targetSeat := intInput(args, "target_seat")
	reason, _ := args["reason"].(string)
	if template == "" {
		return "public_commit rejected: template required", nil
	}
	if reason == "" {
		return "public_commit rejected: reason required", nil
	}
	// 截断 reason
	if len(reason) > 30 {
		reason = reason[:30]
	}
	if r, ok := runner.(PublicCommitRunner); ok {
		return r.PublicCommit(template, targetSeat, reason)
	}
	return "public_commit rejected: runner does not support commitments", nil
}
