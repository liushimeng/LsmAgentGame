// Package thpagent — prompt_compress.go: 决策 prompt 字节预算四梯度压缩
// (§3.4 德州扑克Agent聊天系统设计)。
//
// thpagent Memory 与 wwplayer 结构不同(非 messages 队列),压缩目标对象是
// **决策 prompt 的字节量**,梯度:
//
//	| 梯度   | 触发                    | 动作                                          |
//	|--------|-------------------------|-----------------------------------------------|
//	| Tier60 | bytes > 0.6*budget      | ChatWindow 截最近 20 行 + RecentHands 保留 3 手 |
//	| Tier80 | bytes > 0.8*budget      | Tier60 + OpponentStats 收敛主要对手 + LLM 语义压缩 |
//	|        |                         | ChatWindow(失败回退规则式)                    |
//	| Tier100| preflight 强裁          | ChatWindow 只留最近 5 行,HandRecord 只留 1 手  |
//	| Tier400| LLM 返回上下文超限      | Aggressive:可选段清空,仅保留当前街动作历史     |
//
// budget 默认 200KB(DefaultMaxPromptBytes);LLM 语义压缩由 driver 注入
// provider 调用(本文件只提供 prompt 构造与结果判空)。
package thpagent

import (
	"fmt"
	"strings"
)

// DefaultMaxPromptBytes 是决策 prompt 的默认字节预算(200KB)。
// 对齐 wwplayer getModelContextBudget 的兜底值;德扑侧无 per-model 元数据,
// 统一用此常量(后续接入 per-model budget 时在此扩展)。
const DefaultMaxPromptBytes = 200 * 1024

// PromptTier 压缩梯度枚举(§3.4 四梯度)。
type PromptTier int

const (
	// TierNone 未触发(< 60% 预算)。
	TierNone PromptTier = iota
	// Tier60 prompt > 60% 预算:规则式轻压缩。
	Tier60
	// Tier80 prompt > 80% 预算:规则式 + LLM 语义压缩 ChatWindow。
	Tier80
	// Tier100 preflight 强裁:ChatWindow 只留 5 行、HandRecord 只留 1 手。
	Tier100
	// Tier400 上下文超限兜底:全部可选段清空。
	Tier400
)

// ModelContextBudget 返回指定模型的 prompt 字节预算。当前统一默认 200KB;
// 预留 per-model 元数据接入点(§3.4 budget 说明)。
func ModelContextBudget(modelKey string) int {
	_ = modelKey
	return DefaultMaxPromptBytes
}

// PromptTierFor 按 prompt 字节数与预算判定压缩梯度。
func PromptTierFor(promptBytes, budget int) PromptTier {
	if budget <= 0 {
		budget = DefaultMaxPromptBytes
	}
	switch {
	case promptBytes >= budget:
		return Tier100
	case promptBytes > budget*4/5:
		return Tier80
	case promptBytes > budget*3/5:
		return Tier60
	default:
		return TierNone
	}
}

// ApplyPromptCompression 按梯度对决策上下文 + Memory 应用**规则式**压缩
// (就地修改 ctx/mem;LLM 语义压缩摘要由调用方(Tier80 路径)先算好经
// llmSummary 传入 —— 非空时 Tier80 用它替换 ChatWindow,空串回退规则式)。
//
// 各梯度动作见文件头表格。Tier400 同时置位 ctx.suppressOptionalBlocks,
// BuildUserPrompt 据此跳过全部 Optional 段。
func ApplyPromptCompression(ctx *GameContextForAgent, mem *Memory, tier PromptTier, llmSummary string) {
	if ctx == nil || tier == TierNone {
		return
	}
	switch tier {
	case Tier60:
		ctx.ChatWindow = TruncateChatWindowLines(ctx.ChatWindow, 20)
		if mem != nil {
			mem.TruncateRecentHands(3)
		}
	case Tier80:
		if strings.TrimSpace(llmSummary) != "" {
			ctx.ChatWindow = strings.TrimSpace(llmSummary)
		} else {
			ctx.ChatWindow = TruncateChatWindowLines(ctx.ChatWindow, 20)
		}
		if mem != nil {
			mem.TruncateRecentHands(3)
			mem.PruneOpponentStats(5)
		}
	case Tier100:
		ctx.ChatWindow = TruncateChatWindowLines(ctx.ChatWindow, 5)
		if mem != nil {
			mem.TruncateRecentHands(1)
		}
	case Tier400:
		// Aggressive:全部可选段清空(ChatWindow 清零 + Optional 段抑制),
		// 仅保留当前街动作历史等 Critical 段。
		ctx.ChatWindow = ""
		if mem != nil {
			mem.TruncateRecentHands(1)
		}
		ctx.suppressOptionalBlocks = true
	}
}

// BuildChatWindowCompressPrompt 构造 Tier80 LLM 语义压缩的 prompt:
// 把整段「牌桌闲聊(增量)」压缩为不超过 maxRunes rune 的摘要,保留
// 谁说了什么的关键事实(供 bot 回应)。返回 (userPrompt, systemPrompt)。
func BuildChatWindowCompressPrompt(chatWindow string, maxRunes int) (string, string) {
	if maxRunes <= 0 {
		maxRunes = 400
	}
	system := "你是扑克牌桌聊天记录压缩器。把给定的牌桌公屏聊天压缩成简洁的摘要,按发言者分条,保留「谁说了什么」的关键信息与互相回应关系,删除寒暄与重复。只输出摘要本身,不要任何解释。"
	user := fmt.Sprintf("请把以下牌桌聊天压缩为不超过 %d 字的摘要:\n\n%s", maxRunes, chatWindow)
	return user, system
}

// IsContextExceededError 判定 LLM 错误是否为「上下文超限」类
// (触发 Tier400 Aggressive 重试)。匹配上游常见的 context_length /
// context window / prompt is too long 文案。
func IsContextExceededError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "context_length") ||
		strings.Contains(msg, "context length") ||
		strings.Contains(msg, "context window") ||
		strings.Contains(msg, "prompt is too long") ||
		strings.Contains(msg, "maximum context")
}
