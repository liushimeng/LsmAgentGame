// Package thpagent — driver_chat.go: Driver 的聊天增强路径(§3.2/§3.4
// 德州扑克Agent聊天系统设计):
//   - applyDecisionCompression: 决策前按 prompt 字节预算四梯度压缩上下文
//   - compressChatWindowLLM:     Tier80 的 LLM 语义压缩(失败回退规则式)
//   - HandOverChat:              摊牌局结算闲聊(限流复用 DispatchPokerChat)
//   - RoomMemorySnapshots:       局末 MemoryIter 的事实快照(风格画像/对手笔记)
package thpagent

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	agentroot "LsmAgentGame/agent"
	agentcore "LsmAgentGame/agent/core"
	"LsmAgentGame/llm/types"
	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// applyDecisionCompression 在渲染决策 prompt 之前按 §3.4 四梯度压缩
// promptContext/mem(就地修改)。Tier≥80 且 Provider 可用时先尝试 LLM 语义
// 压缩 ChatWindow;任何失败静默回退规则式(调用方无感)。
func applyDecisionCompression(ctx context.Context, a *Agent, provider types.LLMProvider, apiKey string, promptContext *GameContextForAgent, mem *Memory) {
	if promptContext == nil {
		return
	}
	budget := ModelContextBudget(a.ModelKey)
	estimate := len(BuildSystemPrompt(promptContext, mem)) + len(BuildUserPrompt(promptContext, mem))
	tier := PromptTierFor(estimate, budget)
	if tier == TierNone {
		return
	}
	var llmSummary string
	if tier == Tier80 && provider != nil && apiKey != "" {
		llmSummary = compressChatWindowLLM(ctx, provider, apiKey, a.ModelKey, promptContext.ChatWindow)
	}
	logger.L().Info("texasholdem prompt compression applied",
		zap.String("room_id", promptContext.RoomID),
		zap.Int("seat", promptContext.MySeat),
		zap.Int("tier", int(tier)),
		zap.Int("prompt_bytes", estimate),
		zap.Int("budget", budget),
		zap.Bool("llm_summary", llmSummary != ""))
	ApplyPromptCompression(promptContext, mem, tier, llmSummary)
}

// compressChatWindowLLM 用 LLM 把 ChatWindow 语义压缩为 ≤400 rune 摘要。
// 15s 短超时;任何失败(空窗口/超时/空输出)返回空串,由
// ApplyPromptCompression 回退规则式截断。绝不阻塞主决策链超过 15s。
func compressChatWindowLLM(ctx context.Context, provider types.LLMProvider, apiKey, modelKey, chatWindow string) string {
	if strings.TrimSpace(chatWindow) == "" {
		return ""
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	user, system := BuildChatWindowCompressPrompt(chatWindow, 400)
	req := types.LLMRequest{
		Model:          modelKey,
		System:         []types.SystemBlock{{Type: "text", Text: system}},
		Messages:       []types.Message{{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: user}}}},
		MaxTokens:      1024,
		AgentClassName: string(agentroot.AgentClassTexasHoldemPlayer),
	}
	resp, err := provider.Chat(cctx, apiKey, req)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(resp.Text())
}

// HandOverChat 让指定 bot 座位在摊牌结算后做一次「结算闲聊」(§3.2 触发点:
// onHandOver 后,摊牌局且 bot 参与)。复用 Dispatcher.DispatchPokerChat 限流,
// 被限流/LLM 失败时返回空串(调用方静默)。
//
// 不改引擎语义:纯额外 LLM 调用,不产生 poker_action。
func (d *Driver) HandOverChat(ctx context.Context, roomID string, seat int, handNumber int, won bool, netDelta int) string {
	d.mu.RLock()
	ra, ok := d.rooms[roomID]
	if !ok || seat < 0 || seat >= 6 {
		d.mu.RUnlock()
		return ""
	}
	a := ra.agents[seat]
	disp := ra.dispatch[seat]
	d.mu.RUnlock()
	if a == nil || disp == nil || a.IsCancelled() {
		return ""
	}

	a.mu.Lock()
	provider := a.Provider
	apiKey := a.apiKey
	a.mu.Unlock()
	if provider == nil || apiKey == "" {
		return ""
	}

	system := "你是德州扑克玩家,刚打完一手摊牌。请用一两句话对这手牌的结果做情绪化短评(赢了的得意/输了的懊恼均可)。只输出短评文本本身,不要任何前缀、引号或解释。绝不透露你本手的底牌(历史手牌除外可模糊提及)。"
	mood := "输了"
	if won {
		mood = "赢了"
	}
	user := fmt.Sprintf("手牌 #%d 结束,你%s(本手净盈亏 %+d)。请说一句结算闲聊。", handNumber, mood, netDelta)
	req := types.LLMRequest{
		Model:          a.ModelKey,
		System:         []types.SystemBlock{{Type: "text", Text: system}},
		Messages:       []types.Message{{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: user}}}},
		MaxTokens:      256,
		AgentClassName: a.AgentClass,
	}
	callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	resp, err := provider.Chat(callCtx, apiKey, req)
	if err != nil {
		logger.L().Debug("texasholdem hand-over chat LLM failed, silent skip",
			zap.String("room_id", roomID), zap.Int("seat", seat), zap.Error(err))
		return ""
	}
	text := strings.TrimSpace(resp.Text())
	if text == "" {
		return ""
	}
	if cleaned, deduped, truncated := agentcore.DedupSpeakText(text); cleaned != "" {
		text = cleaned
		_ = deduped
		_ = truncated
	}
	if err := disp.DispatchPokerChat(text); err != nil {
		return "" // 限流拒绝 — 静默
	}
	return text
}

// SeatMemorySnapshot 是单个 bot 座位的局末记忆快照(供 MemoryIter)。
type SeatMemorySnapshot struct {
	Seat     int
	ModelKey string
	// Facts 是「本局事实」文本(最近手牌净盈亏 + 对手画像摘要),
	// 喂给德扑版 MemoryIter 迭代 prompt 的【本局事实】段。
	Facts string
}

// RoomMemorySnapshots 返回房间全部 bot 座位的记忆快照(锁内快照,锁外返回)。
// 房间未注册/无 bot 时返回空切片。供 ws 层局末 MemoryIter 异步迭代使用。
func (d *Driver) RoomMemorySnapshots(roomID string) []SeatMemorySnapshot {
	d.mu.RLock()
	ra, ok := d.rooms[roomID]
	d.mu.RUnlock()
	if !ok {
		return nil
	}
	out := make([]SeatMemorySnapshot, 0, 6)
	for i := 0; i < 6; i++ {
		if ra.agents[i] == nil || ra.memories[i] == nil {
			continue
		}
		out = append(out, SeatMemorySnapshot{
			Seat:     i,
			ModelKey: ra.models[i],
			Facts:    buildSeatMemoryFacts(ra.memories[i], roomID, i, ra.models[i]),
		})
	}
	return out
}

// buildSeatMemoryFacts 从 Memory 构造「本局事实」文本:最近手牌净盈亏 +
// 对手画像摘要(按 HandsPlayed 降序 top 5)。纯读快照,不持 driver 锁。
func buildSeatMemoryFacts(mem *Memory, roomID string, seat int, modelKey string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "房间 %s;你坐 %d 号位(模型 %s)。", roomID, seat+1, modelKey)
	hands := mem.RecentHandsSnapshot()
	if len(hands) == 0 {
		b.WriteString("本局无手牌记录。")
	} else {
		net := 0
		b.WriteString("最近手牌净盈亏: ")
		for i, h := range hands {
			net += h.NetChipDelta
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "#%d:%+d", h.HandNumber, h.NetChipDelta)
		}
		fmt.Fprintf(&b, ";本局合计约 %+d。", net)
	}
	stats := mem.AllOpponentStats()
	if len(stats) > 0 {
		ids := make([]string, 0, len(stats))
		for id := range stats {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(x, y int) bool { return stats[ids[x]].HandsPlayed > stats[ids[y]].HandsPlayed })
		b.WriteString(" 对手画像: ")
		n := 0
		for _, id := range ids {
			st := stats[id]
			if st.HandsPlayed <= 0 {
				continue
			}
			if n > 0 {
				b.WriteString("; ")
			}
			fmt.Fprintf(&b, "%d号位 共%d手 弃牌率%.0f%% 净盈亏%+d",
				st.Seat+1, st.HandsPlayed, st.FoldRate*100, st.NetChips)
			n++
			if n >= 5 {
				break
			}
		}
	}
	return b.String()
}
