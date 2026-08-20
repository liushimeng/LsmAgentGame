// game_service_texas_bot_normalize.go — §B4 bot 动作规范化(2026-08-20)。
//
// 对齐 [德州扑克Agent工具协议.md] §10 失败模式表 + [德州扑克Agent金币设计.md] §4.3:
//   - LLM raise amount < min_raise        → 自动抬到 min_raise(不是报错后 fold)
//   - LLM raise/bet amount > 可动用筹码     → 改 allin
//   - LLM allin 且声明 amount < 90% 筹码总量
//     且剩余筹码 ≥ 2×bigBlind               → 改 raise 到 90% 筹码总量(防自杀式全押)
//   - fold / check / call                 → 透传
//
// 规范化发生时 logger.Debug 记录 original → normalized。
// normalizeBotAction 是纯函数(不读锁、不碰全局),单元测试见
// game_service_texas_bot_normalize_test.go。
package ws

import (
	"LsmAgentGame/agent/thpagent"
	"LsmAgentGame/game/texasholdem"
	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// normalizeBotAction 按工具协议 §10 / 金币设计 §4.3 规范化 bot 动作。
// snap 为决策时的房间快照(BotGameContext),action 为 LLM 原始决策。
func normalizeBotAction(snap *texasholdem.BotGameContextSnapshot, a thpagent.Action) texasholdem.Action {
	original := a
	converted := convertToEngineAction(a)
	out := converted

	// stackTotal = 本轮已下注 + 剩余筹码 = 本手可动用的绝对总注额上限
	stackTotal := snap.MyRoundCommitted + snap.MyStack
	minRaiseTotal := snap.CurrentBet + snap.MinRaise

	switch a.Type {
	case thpagent.ActBet:
		// 最小下注 = bigBlind
		if out.Amount < snap.BigBlind {
			out.Amount = snap.BigBlind
		}
		if out.Amount >= stackTotal && snap.MyStack > 0 {
			out.Type = texasholdem.ActAllIn
			out.Amount = 0
		}
	case thpagent.ActRaise:
		if stackTotal > 0 && minRaiseTotal >= stackTotal {
			// 连最小加注都凑不齐 → 只能 allin(剩余全压)或 fold;LLM 已表达加注意愿,改 allin
			out.Type = texasholdem.ActAllIn
			out.Amount = 0
			break
		}
		if out.Amount < minRaiseTotal {
			out.Amount = minRaiseTotal // 协议 §10: 自动抬到 min_raise
		}
		if out.Amount >= stackTotal {
			out.Type = texasholdem.ActAllIn // 协议 §10: amount > my_stack → allin
			out.Amount = 0
		}
	case thpagent.ActAllIn:
		// 金币设计 §4.3: 声明 amount < 90% 筹码总量 且 剩余筹码 ≥ 2×bigBlind
		// → 改 raise 到 90% 筹码总量(bot 筹码 < 2 大盲时允许 allin,别无选择)
		if snap.BigBlind > 0 && snap.MyStack >= 2*snap.BigBlind && stackTotal > 0 &&
			original.Amount > 0 && original.Amount < int(0.9*float64(stackTotal)) {
			target := int(0.9 * float64(stackTotal))
			if target < minRaiseTotal {
				if minRaiseTotal < stackTotal {
					target = minRaiseTotal
				} else {
					// 90% 目标低于最小加注且最小加注已等于全押 → 保持 allin
					break
				}
			}
			out.Type = texasholdem.ActRaise
			out.Amount = target
		} else {
			out.Amount = 0 // 引擎 ActAllIn 忽略 amount,清零避免歧义
		}
	default:
		// fold / check / call — 透传
	}

	if out.Type != converted.Type || out.Amount != converted.Amount {
		logger.L().Debug("texasholdem bot action normalized",
			zap.String("room_id", snap.RoomID),
			zap.Int("seat", snap.MySeat),
			zap.String("original_type", original.Type),
			zap.Int("original_amount", original.Amount),
			zap.String("normalized_type", out.Type.String()),
			zap.Int("normalized_amount", out.Amount))
	}
	return out
}
