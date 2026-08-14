// Package wwplayer — preflight.go: 发 HTTP 前的上下文预算预检。
//
// 2026-08-13 §20260813-04 U4。借鉴 Hermes Agent 的
// ContextCompressor._compute_threshold_tokens（agent/context_compressor.py:2472）。
//
// # 修的是什么
//
// prompt_budget.go:19-20 自述「没有任何 pre-flight 检查」：
// 现状唯一的上下文自适应路径是**等上游返回 400** `exceed max message tokens`
// 之后才走 isContextExceededError → PruneByBytesAggressive。
//
// 对慢模型这个代价极高：首字节 1-3 分钟（§197），一次 400 = 白等数分钟，
// 且期间占着房间级 llmSema 槽位（cap=4），拖慢整个房间。
//
// # Hermes 的三个洞察（每个都对应一个真实 issue）
//
//	① 必须从模型窗口减去 max_tokens 输出预留（Hermes #43547）
//	   provider 从同一个窗口里预留输出空间，可用**输入**预算是 window - max_tokens。
//	   本项目此前完全没有这个概念 —— getModelContextBudget 返回的是整个窗口。
//
//	② 地板值在小窗口上会退化（Hermes #14690）
//	   若预留吃掉整个窗口，阈值等于窗口本身，压缩永远无法触发
//	   （provider 在使用率到 100% 前就拒绝了）。退化时改按比例触发。
//
//	③ max_tokens 未知时保守假设无预留（完整窗口）
//
// # 与既有机制的关系
//
// 本文件**不替代** post-error 路径（isContextExceededError → 激进压缩）——
// 两者是独立机制：pre-flight 处理「可预测的超限」，
// post-error 处理「预算估算不准导致的意外超限」。Hermes 同时保留两者。
package wwplayer

import (
	"fmt"
	"time"
)

const (
	// llmMaxTokensPerCall 是每次 LLM 调用请求的 max_tokens。
	//
	// 2026-08-13 §20260813-04 U4：抽出为常量（原先在 run.go 两处硬编码 2048）。
	// pre-flight 预算计算必须与实际请求的 max_tokens 一致 —— 若两处漂移，
	// 预留量算错，pre-flight 的整个前提就失效了（这正是 Hermes #43547 的教训：
	// provider 从同一窗口预留输出空间，预留量必须是真实值）。
	//
	// 历史（R139）：1024 在 Kimi-model 等中文模型 + wolf_kill tool_use 路径下
	// 偶发 stop_reason=max_tokens 截断，提到 2048。
	llmMaxTokensPerCall = 2048

	// preflightDegradeRatioPct 是输出预留吃掉整个窗口时的退化触发比例。
	//
	// 借鉴 Hermes _MIN_CTX_TRIGGER_RATIO（85%）：高到让小窗口模型用掉大部分
	// 预算才裁剪，但低于 100% 以便在 provider 拒绝之前动手。
	preflightDegradeRatioPct = 85

	// preflightMinBudgetBytes 是有效预算的下限（64KB）。
	//
	// 防止极端配置（窗口很小 + max_tokens 很大）把预算压到几 KB，
	// 那会让每一轮都触发裁剪，反而把 identity/近期上下文全裁掉。
	// 宁可偶尔让 provider 报 400（有 post-error 兜底），
	// 也不要 pre-flight 把上下文裁到无法对局（§20260812-04 教训 5：护栏宁松勿紧）。
	preflightMinBudgetBytes = 64 * 1024

	// toolResultPruneTriggerPct 是 tool_result 廉价剪枝的触发比例（占有效预算）。
	//
	// 2026-08-13 §20260813-04 U6。取 60% 让三层回收形成梯度：
	//
	//	60%  → PruneToolResultsOnly   无 LLM、只截断工具返回文本（最廉价）
	//	100% → pre-flight PruneByBytes 无 LLM、丢弃整条旧消息
	//	400  → PruneByBytesAggressive  post-error，压到 50% 预算
	//
	// 借鉴 Hermes 的「low, cost-oriented trigger independent of should_compress」：
	// 廉价手段先上场，昂贵手段兜底。
	toolResultPruneTriggerPct = 60

	// preflightBytesPerToken 是 token → 字节的保守折算率。
	//
	// 中文 UTF-8 是 3 bytes/字，而中文对 Anthropic 类 tokenizer 约
	// 1-1.5 字/token，故 1 token ≈ 2-4.5 bytes。取 3 作为中值。
	//
	// 这是**近似**：本项目全链路用字节而非真实 tokenizer 计量
	// （见方案文档 §5「刻意不做的事」）。折算只用于把 max_tokens 换算成
	// 字节量级的预留，不用于精确判定 —— 精度不足由 post-error 路径兜底。
	preflightBytesPerToken = 3
)

// preflightBudgetBytes 返回本次请求的**有效输入**字节预算。
//
//	effective = getModelContextBudget(modelKey) - maxTokens*3
//
// 预留 ≥ 窗口时退化为窗口的 preflightDegradeRatioPct%，
// 并统一钳到 preflightMinBudgetBytes 下限。
//
// maxTokens <= 0 表示未知/未设置 → 保守假设无预留（等价旧行为）。
func preflightBudgetBytes(modelKey string, maxTokens int) int {
	window := getModelContextBudget(modelKey)
	if window <= 0 {
		window = DefaultMaxPromptBytes
	}
	if maxTokens <= 0 {
		// Hermes 洞察 ③：未知 max_tokens 保守假设无预留。
		return clampPreflightBudget(window)
	}

	reserve := maxTokens * preflightBytesPerToken
	effective := window - reserve
	if effective <= 0 {
		// Hermes 洞察 ②：预留吃掉整个窗口 → 按比例退化，
		// 否则阈值等于窗口本身，pre-flight 永远不触发。
		effective = window * preflightDegradeRatioPct / 100
	}
	return clampPreflightBudget(effective)
}

// clampPreflightBudget 把预算钳到 [preflightMinBudgetBytes, +∞)。
func clampPreflightBudget(n int) int {
	if n < preflightMinBudgetBytes {
		return preflightMinBudgetBytes
	}
	return n
}

// shouldPreflightPrune 判断当前 payload 是否需要在发 HTTP 前主动裁剪。
//
// 返回 (需要裁剪, 目标字节数, 人类可读原因)。
// 不需要裁剪时返回 (false, 0, "")。
//
// 目标字节数取有效预算本身 —— 不额外打折。理由：
// 裁剪本身有信息损失，能过就过；真的估不准由 post-error 激进压缩（50% 预算）兜底。
func shouldPreflightPrune(payloadBytes, budgetBytes int) (bool, int, string) {
	if budgetBytes <= 0 || payloadBytes <= budgetBytes {
		return false, 0, ""
	}
	reason := fmt.Sprintf("payload %dKB > 有效输入预算 %dKB",
		payloadBytes/1024, budgetBytes/1024)
	return true, budgetBytes, reason
}

// SetLastPreflightNote 记录一次 pre-flight 裁剪的可观测标记。
//
// 经 BotTranscript() 透出到 wire —— 禁止静默降级
// （§20260812-04 教训 4，与 setCompactOutcome 同款纪律）。
func (a *Agent) SetLastPreflightNote(note string) {
	a.Lock()
	a.preflightAt = time.Now().UnixMilli()
	a.preflightNoteText = truncate(note, 160)
	a.Unlock()
}

// LastPreflightNote 返回最近一次 pre-flight 裁剪标记（测试与诊断用）。
func (a *Agent) LastPreflightNote() string {
	a.Lock()
	defer a.Unlock()
	return a.preflightNoteText
}

// preflightNote 生成落到 BotTranscript 的可观测标记。
//
// **降级必留可观测标记**（§20260812-04 教训 4）：
// 静默裁剪会让「上下文变短」与「模型本来就没说什么」在日志里同形，
// 正是 §20260811-08 教训 (5) 批评的模式。
func preflightNote(modelKey string, payloadBytes, budgetBytes, maxTokens int) string {
	return fmt.Sprintf(
		"[pre-flight 裁剪] model=%s payload=%dKB 预算=%dKB(窗口 %dKB - 输出预留 %dKB) —— 已在发请求前裁剪",
		modelKey,
		payloadBytes/1024,
		budgetBytes/1024,
		getModelContextBudget(modelKey)/1024,
		maxTokens*preflightBytesPerToken/1024,
	)
}
