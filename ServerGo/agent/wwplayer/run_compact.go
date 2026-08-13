// Package wwplayer — run_compact.go: 局内 LLM 语义压缩的触发与接线。
//
// 2026-08-13 §20260813-02 U1 新增(从 run.go::handleEvent 搬移并增强)。
//
// 背景(§130「声明了却从不接线」第六次复现):
// memory_compact.go::CompactWithLLM 早已完整实现,run.go 也有触发判断,
// 但 Agent.compactConfig **没有任何 setter** —— Enabled 恒 false,触发判断
// 永不生效,局内压缩实际退化为规则式 CompressHistoryLocked。本文件补上:
//
//  1. SetCompactConfig —— 由 game/werewolf.StartAgentsLocked 每 bot 注入;
//  2. maybeCompactMemory —— handleEvent 入口的触发点(每局最多一次),
//     LLM 压缩失败**显式回退**规则式压缩并留可观测标记(禁止假成功,
//     OpenClaw Context §6.2);
//  3. 压缩 LLM 调用走房间 llmSema 信号量(不占 speak 限流器),
//     失败**不计入 consecutiveFailures**(§112 speak_floor 同款约束)。
package wwplayer

import (
	"context"
	"fmt"
	"time"

	"LsmAgentGame/agent/wwtypes"
	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// SetCompactConfig 注入局内 LLM 压缩配置(2026-08-13 §20260813-02 U1)。
// 由 game/werewolf.StartAgentsLocked 在 NewWithRoom 之后调用;
// 不调用时 Enabled 恒 false,maybeCompactMemory 整链 no-op(旧行为)。
func (a *Agent) SetCompactConfig(cfg CompactConfig) {
	a.Lock()
	defer a.Unlock()
	a.compactConfig = cfg
}

// CompactConfigSnapshot 返回当前压缩配置(测试与诊断用)。
func (a *Agent) CompactConfigSnapshot() CompactConfig {
	a.Lock()
	defer a.Unlock()
	return a.compactConfig
}

// setCompactOutcome 记录一次压缩尝试的结果标记(成功或显式回退),
// 经 BotTranscript() / recordTranscript 透出到 wire,禁止假成功。
func (a *Agent) setCompactOutcome(fallback bool, note string) {
	a.Lock()
	a.compactAt = time.Now().UnixMilli()
	a.compactFallback = fallback
	a.compactNote = truncate(note, 120)
	a.Unlock()
}

// maybeCompactMemory 是 handleEvent 入口的局内压缩触发点(每局最多一次)。
//
// 触发条件(与旧 run.go 内联判断完全等价):
//   - !a.compactDone(每局只压缩一次)
//   - a.compactConfig.Enabled(由 SetCompactConfig 注入)
//   - a.Provider != nil(测试桩可能未注入 provider)
//   - Memory.Len() >= compactConfig.MinMessages
//
// 压缩在 goroutine 内异步执行(不阻塞主决策循环);失败时:
//   - 显式回退规则式 CompressHistoryLocked(OpenClaw:禁止假成功);
//   - setCompactOutcome(true, reason) 留可观测标记;
//   - **不**计入 consecutiveFailures(§112:辅助路径失败不污染 quarantine 计数)。
func (a *Agent) maybeCompactMemory(ctx context.Context, rp RolePhase, gc *wwtypes.GameContext) {
	if a.compactDone || !a.compactConfig.Enabled || a.Provider == nil {
		return
	}
	count := a.Memory.Len()
	if count < a.compactConfig.MinMessages {
		return
	}
	a.compactDone = true
	go func() {
		rp2 := rp
		_, _, _, _, _, _, done2 := rp2()
		if done2 {
			return
		}
		// 2026-08-13 §20260813-02 U1 — 压缩 LLM 调用走房间级 llmSema
		// 信号量(不占 speak 限流器),与主对话调用公平排队;
		// 槽位满时放弃本次压缩(下一局/下次重启仍有机会),不计失败。
		if !a.AcquireLLMSlot(llmSlotAcquireWait) {
			logger.L().Debug("agent: memory compact skipped (LLM slot saturated)",
				zap.Int("seat", a.Seat), zap.String("model", a.ModelKey))
			return
		}
		defer a.ReleaseLLMSlot()
		result := a.Memory.CompactWithLLM(ctx, a.Provider, a.apiKey, a.ModelKey, gc, a.compactConfig)
		if result.Success {
			mode := "全量"
			if result.Incremental {
				mode = "增量"
			}
			a.setCompactOutcome(false, fmt.Sprintf("LLM 压缩成功(%s): %d→%d 条, %d ms",
				mode, result.MessagesBefore, result.MessagesAfter, result.DurationMs))
			logger.L().Info("agent: memory compacted",
				zap.Int("seat", a.Seat),
				zap.Int("before", result.MessagesBefore),
				zap.Int("after", result.MessagesAfter),
				zap.Bool("incremental", result.Incremental),
				zap.Int64("ms", result.DurationMs))
			return
		}
		// 显式回退(OpenClaw Context §6.2:压缩失败必须显式失败,禁止假成功):
		// 规则式压缩兜底收缩上下文,并在 BotTranscript 留 fallback 标记。
		errStr := ""
		if result.Error != nil {
			errStr = result.Error.Error()
		}
		a.Memory.CompressAndPrune(DefaultPruneTurns, DefaultCompressTurns)
		a.setCompactOutcome(true, "LLM 压缩失败,已回退规则式压缩: "+errStr)
		logger.L().Warn("agent: memory compact failed, fell back to rule-based compression",
			zap.Int("seat", a.Seat),
			zap.String("model", a.ModelKey),
			zap.Error(result.Error))
	}()
}
