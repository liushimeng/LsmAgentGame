// game_service_texas_memoryiter.go — 德扑 Agent 持久记忆(MEMORY.md)跨局
// 自我迭代编排(§3.4 德州扑克Agent聊天系统设计,2026-08-23)。
//
// 触发点:cleanupTexasHoldemBotRuntime(房间删除/局终,§5 接线清单
// 「德扑 memory_persist 房间生命周期触发点」)。
// 流程(对齐狼人杀 werewolf/agent_memory_bridge.go,德扑版精简):
//
//	for each bot seat(model_key 去重):
//	  goroutine(recover 兜底):
//	    1. driver.RoomMemorySnapshots 快照本局事实(风格画像 + 对手笔记素材)
//	    2. thpMemoryStore.Load 读旧 memory_md(无则空)
//	    3. BuildTexasMemoryIterPrompt(旧记忆 + 本局事实;>80K 加压缩指令)
//	    4. thpRegistry.Get(modelKey) → provider.Chat(AgentClassName=
//	       LsmAgentGame-TexasPoker-MemoryIter,§24)
//	    5. ValidateTexasMemorySections 不通过 → TexasFallbackMerge 规则兜底
//	    6. >100K → TexasHardTruncate 硬截断
//	    7. thpMemoryStore.SaveIterated 写回
//
// 硬约束:异步且不阻塞房间删除;失败仅 logger.Warn;绝不 panic。
package ws

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	agentroot "LsmAgentGame/agent"
	"LsmAgentGame/agent/thpagent"
	"LsmAgentGame/llm/types"
	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// texasMemoryIterTimeoutSec 是德扑 MemoryIter 单次 LLM 调用超时(对齐狼人杀
// defaultAgentMemoryIterTimeoutSec 的 §197 长预算思想,取 480s 兜底慢模型)。
const texasMemoryIterTimeoutSec = 480

// texasMemoryMus 是 modelKey → *sync.Mutex 单飞锁(同一模型并发迭代串行化,
// 与狼人杀 memoryMuFor 双保险语义一致)。
var texasMemoryMus sync.Map

// IterateTexasAgentMemoriesAsync 对房间内每个 bot 模型异步发起一次持久记忆
// 自我迭代。store/registry 未注入、无 driver/无 bot 时 no-op。
func (s *GameService) IterateTexasAgentMemoriesAsync(roomID string) {
	if s.thpMemoryStore == nil || s.thpRegistry == nil || s.thpDriver == nil {
		return
	}
	snapshots := s.thpDriver.RoomMemorySnapshots(roomID)
	if len(snapshots) == 0 {
		return
	}
	// 同一 model_key 只迭代一次(多座位可能共享模型)。
	unique := make(map[string]thpagent.SeatMemorySnapshot, len(snapshots))
	for _, snap := range snapshots {
		if snap.ModelKey == "" {
			continue
		}
		if _, ok := unique[snap.ModelKey]; !ok {
			unique[snap.ModelKey] = snap
		}
	}
	for modelKey, snap := range unique {
		go s.iterateOneTexasMemory(roomID, snap, modelKey)
	}
}

// iterateOneTexasMemory 执行单个模型的记忆迭代全链;全程失败仅 Warn。
func (s *GameService) iterateOneTexasMemory(roomID string, snap thpagent.SeatMemorySnapshot, modelKey string) {
	defer func() {
		if rec := recover(); rec != nil {
			logger.L().Warn("texasholdem: agent memory iterate panicked",
				zap.String("room_id", roomID),
				zap.String("model_key", modelKey),
				zap.Any("recover", rec))
		}
	}()
	muAny, _ := texasMemoryMus.LoadOrStore(modelKey, &sync.Mutex{})
	mu := muAny.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), texasMemoryIterTimeoutSec*time.Second)
	defer cancel()

	// 1. 读旧记忆(行不存在返回 "")。
	oldMD, err := s.thpMemoryStore.Load(ctx, modelKey)
	if err != nil {
		logger.L().Warn("texasholdem: agent memory load failed",
			zap.String("room_id", roomID), zap.String("model_key", modelKey), zap.Error(err))
		return
	}

	// 2. 构造迭代 prompt;旧记忆 > 80K 要求 LLM 主动瘦身。
	compress := len(oldMD) > thpagent.TexasMemoryCompressThresholdBytes
	prompt := thpagent.BuildTexasMemoryIterPrompt(oldMD, snap.Facts, compress)

	// 3. 用该模型自己的 provider 调 LLM(AgentClassName 见 §24/class_names.go)。
	provider, apiKey, err := s.thpRegistry.Get(modelKey)
	if err != nil {
		logger.L().Warn("texasholdem: agent memory registry.Get failed",
			zap.String("room_id", roomID), zap.String("model_key", modelKey), zap.Error(err))
		return
	}
	req := types.LLMRequest{
		Model:          modelKey,
		System:         []types.SystemBlock{{Type: "text", Text: "你是德州扑克 AI 玩家的长期记忆迭代器。"}},
		Messages:       []types.Message{{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: prompt}}}},
		MaxTokens:      thpagent.TexasMemoryIterMaxTokens,
		AgentClassName: string(agentroot.AgentClassTexasPokerMemoryIter),
	}
	resp, llmErr := provider.Chat(ctx, apiKey, req)

	var newMD string
	switch {
	case llmErr != nil:
		logger.L().Warn("texasholdem: agent memory LLM iterate failed, fallback merge",
			zap.String("room_id", roomID), zap.String("model_key", modelKey), zap.Error(llmErr))
		newMD = thpagent.TexasFallbackMerge(oldMD,
			fmt.Sprintf("本局(%s)自我迭代 LLM 调用失败,仅保留旧记忆", roomID))
	default:
		text := strings.TrimSpace(resp.Text())
		if text == "" || !thpagent.ValidateTexasMemorySections(text) {
			logger.L().Warn("texasholdem: agent memory LLM output invalid, fallback merge",
				zap.String("room_id", roomID), zap.String("model_key", modelKey),
				zap.Bool("empty", text == ""))
			newMD = thpagent.TexasFallbackMerge(oldMD,
				fmt.Sprintf("本局(%s)自我迭代输出不合格,仅保留旧记忆", roomID))
		} else {
			newMD = text
		}
	}

	// 4. 超 100K 硬上限 → rune 安全硬截断(最后兜底)。
	if len(newMD) > thpagent.TexasMemoryMaxBytes {
		newMD = thpagent.TexasHardTruncate(newMD, thpagent.TexasMemoryMaxBytes)
	}

	// 5. 写回(t_lsm_game_agent_memory,复用狼人杀的存储布局)。
	if err := s.thpMemoryStore.SaveIterated(ctx, modelKey, newMD, roomID); err != nil {
		logger.L().Warn("texasholdem: agent memory save failed",
			zap.String("room_id", roomID), zap.String("model_key", modelKey), zap.Error(err))
		return
	}
	logger.L().Info("texasholdem: agent memory iterated",
		zap.String("room_id", roomID),
		zap.String("model_key", modelKey),
		zap.Int("old_bytes", len(oldMD)),
		zap.Int("new_bytes", len(newMD)))
}
