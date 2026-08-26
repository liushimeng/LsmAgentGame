// Package wwplayer — tools_psychology.go: §20260826-01 心理博弈工具 handler。
//
// 设计动机：现有 Agent 只有被动工具（speak / interject / vote）。
// 本文件新增 3 个主动博弈工具，让 Agent 能"测试对手" / "嫁祸" / "从众跟随"：
//
//   - probe_player(target_seat, probe_text, expected_response_kind)
//       试探 — 在发言中向特定玩家抛一个"陷阱问题"，观察回应。
//       服务端把 probe_text 注入目标 bot 下一轮 prompt 的高优先级 question block，
//       并在 goal log 记录一次试探事件。
//
//   - frame_player(target_seat, frame_narrative, evidence_anchor)
//       嫁祸 — 主动把怀疑引向第三方。
//       服务端把 frame_narrative 写进 RumorGraph（hop=0）+ 调
//       EmitImpressionOnFrameLocked 更新被嫁祸者对嫁祸者的 Threat 维度。
//
//   - follow_crowd(leader_seat, reason_summary)
//       从众 — 公开表达跟随某玩家的怀疑。
//       服务端把 leader_seat 写进 commitment_ledger（承诺）+ 调
//       EmitImpressionOnFollowVoteLocked 更新 follower 对 leader 的 Cooperation。
//
// 频率限制：3 工具各 每天 1 次 / bot。由 Action_*Locked 兜底校验。
// §119 协议层隔离：调用记录仅进 BotTranscript.PublicToolUse 字段（不含 target_seat），
// 推理链 target_seat 不写入公开 wire。
// §135 spectator 隔离：调用明细仅 spectator 可见。
package wwplayer

import (
	"errors"
	"fmt"
)

// ─── ProbePlayer 试探 ───

// handleProbePlayerLocked 是 probe_player 工具的 handler。
//
// 入参：
//   - input: LLM 提交的 JSON input
//   - runner: 工具运行上下文（ToolRunner 接口；持 r.mu 由 Action_* 内部处理）
//
// 返回：tool_result 文本 + error。成功时返回 success summary，失败时返回 error 描述。
//
// 频率限制：每天 1 次 / bot。重复调用 → "今日试探次数已用完"。
func handleProbePlayerLocked(input map[string]any, runner ToolRunner) (string, error) {
	if runner == nil {
		return "", errors.New("tool runner not available")
	}
	targetSeat := -1
	if v, ok := input["target_seat"]; ok {
		if f, ok := v.(float64); ok {
			targetSeat = int(f)
		}
	}
	probeText := stringOf(input["probe_text"])
	expectedKind := stringOf(input["expected_response_kind"])

	if targetSeat < 0 || targetSeat >= 13 {
		return "", errors.New("invalid target_seat")
	}
	if len(probeText) == 0 || len(probeText) > 200 {
		return "", errors.New("probe_text must be 1..200 chars")
	}
	if len(expectedKind) == 0 {
		expectedKind = "any"
	}

	// 调用 ToolRunner.Action_ProbePlayer 实现真正的"注入目标下轮 prompt"逻辑
	// （避免 agent 包反向依赖 werewolf 包；ToolRunner 是接口）。
	res, err := runner.Action_ProbePlayer(targetSeat, probeText, expectedKind)
	if err != nil {
		return "", err
	}
	_ = res
	return fmt.Sprintf("probe dispatched → target=%d expected=%s", targetSeat+1, expectedKind), nil
}

// ─── FramePlayer 嫁祸 ───

// handleFramePlayerLocked 是 frame_player 工具的 handler。
//
// 频率限制：每天 1 次 / bot。
// 副作用：写 RumorGraph + ImpressionMemory(target 对 framer 的 Threat+)。
func handleFramePlayerLocked(input map[string]any, runner ToolRunner) (string, error) {
	if runner == nil {
		return "", errors.New("tool runner not available")
	}
	targetSeat := -1
	if v, ok := input["target_seat"]; ok {
		if f, ok := v.(float64); ok {
			targetSeat = int(f)
		}
	}
	narrative := stringOf(input["frame_narrative"])
	evidence := stringOf(input["evidence_anchor"])

	if targetSeat < 0 || targetSeat >= 13 {
		return "", errors.New("invalid target_seat")
	}
	if len(narrative) == 0 || len(narrative) > 200 {
		return "", errors.New("frame_narrative must be 1..200 chars")
	}
	if len(evidence) == 0 || len(evidence) > 120 {
		return "", errors.New("evidence_anchor must be 1..120 chars")
	}

	res, err := runner.Action_FramePlayer(targetSeat, narrative, evidence)
	if err != nil {
		return "", err
	}
	_ = res
	return fmt.Sprintf("frame dispatched → target=%d narrative=%s", targetSeat+1, truncateStr(narrative, 30)), nil
}

// ─── FollowCrowd 从众 ───

// handleFollowCrowdLocked 是 follow_crowd 工具的 handler。
//
// 频率限制：每天 1 次 / bot。
// 副作用：写 commitment_ledger(承诺下一轮与 leader 同票) + ImpressionMemory。
func handleFollowCrowdLocked(input map[string]any, runner ToolRunner) (string, error) {
	if runner == nil {
		return "", errors.New("tool runner not available")
	}
	leaderSeat := -1
	if v, ok := input["leader_seat"]; ok {
		if f, ok := v.(float64); ok {
			leaderSeat = int(f)
		}
	}
	reason := stringOf(input["reason_summary"])

	if leaderSeat < 0 || leaderSeat >= 13 {
		return "", errors.New("invalid leader_seat")
	}
	if len(reason) == 0 || len(reason) > 120 {
		return "", errors.New("reason_summary must be 1..120 chars")
	}

	res, err := runner.Action_FollowCrowd(leaderSeat, reason)
	if err != nil {
		return "", err
	}
	_ = res
	return fmt.Sprintf("follow dispatched → leader=%d reason=%s", leaderSeat+1, truncateStr(reason, 30)), nil
}

// ─── helpers ───

// stringOf 把 input[name] 转 string（兼容 float64/string/[]byte）。
func stringOf(v any) string {
	if v == nil {
		return ""
	}
	switch s := v.(type) {
	case string:
		return s
	case float64:
		return fmt.Sprintf("%v", s)
	case []byte:
		return string(s)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// truncateStr 防御性截断字符串到 maxRunes 字（rune-aware）。
func truncateStr(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes])
}