// Package agent — decision_trail.go: 决策留痕运行时回放（§20260810-12 D1）。
//
// 设计动机:每位 Agent 在每轮 LLM 决策时把「轮次 / 阶段 / 工具名 / 入参 /
// 耗时 / 完成时刻」压缩成一条结构化记录(DecisionEntry),追加进
// BotTranscript.DecisionTrail。前端 HistoryDrawer 第 6 sub-tab「🧠 决策
// 回放」按时间倒序展示所有 Bot 的决策流(§119 spectator 隔离)。
//
// §128 对话即思考:本结构不复制 LLM CoT / 输入全文(LastThinking / FullThinking
// 已删除),只存结构化「步骤」;§135 spectator 隔离由 sanitizeBotTranscript
// 在玩家分支清空整个数组处理;§130 接线验证由 run.go::recordTranscript 两个
// 调用点同步 AppendDecisionEntry 保障。
//
// opt-in 开关:werewolf.bots_log_decisions=true(默认) → trail 启用;false →
// AppendDecisionEntry 提前 return + BotTranscript.DecisionTrail 字段为 nil
// (omitempty) → 零内存 / 零 wire 开销。
package wwplayer

import (
	"LsmWebGame/config"
	"time"
)

// decisionTrailMaxEntries 决策留痕最大保留条数(约 30 个轮次),超过则
// FIFO 淘汰最早一条。前端 sub-tab 渲染按 CreatedAt DESC 排序,淘汰对可见性
// 无影响。
const decisionTrailMaxEntries = 30

// DecisionEntry 是单次 LLM 决策的结构化留痕(§20260810-12 D1)。
//
// 设计取舍:
//   - 不复制 LLM CoT / 输入全文(§128 已删除 LastThinking);
//   - 只存「决策时发生了什么」,对前端的暴露仅限于 spectator + 玩家自己;
//   - JSON 序列化后 ≤ 80 字(典型 "speak(5号): 我怀疑 4 号是狼人 | 4.2s"),
//     保证 wire 包大小可控。
//
// 字段语义:
//   - Round        : 1-based 天数(0 = 夜间未明 / pre-day);
//   - Phase        : 阶段 key(speak/vote/night_wolves/...)；
//   - ToolName     : 本次决策调的工具名("" = LLM end_turn 无工具)；
//   - ToolSummary  : 决策一句话(走 BuildDecisionSummary,含脱敏目标);
//   - TookMs       : 本次决策 LLM 调用耗时(毫秒;0 = 未知);
//   - CreatedAt    : unix 毫秒(决策完成时刻,前端排序 + 倒计时渲染)。
type DecisionEntry struct {
	Round       int    `json:"round"`
	Phase       string `json:"phase,omitempty"`
	ToolName    string `json:"tool_name,omitempty"`
	ToolSummary string `json:"tool_summary,omitempty"`
	TookMs      int64  `json:"took_ms,omitempty"`
	CreatedAt   int64  `json:"created_at,omitempty"`
}

// AppendDecisionEntry 追加一条决策留痕到 BotTranscript。已持 a.mu。
//
// 调用方:run.go::recordTranscript 在两个调用点前 hook,提供 round/phase/
// toolName/toolSummary/tookMs;若 botsLogDecisions=false 则直接 return(零
// 开销,BotTranscript.DecisionTrail 字段保持 nil → JSON omitempty → wire 上
// 不出现该字段)。
//
// 上限:30 条 FIFO 淘汰,保留最近 30 个决策;前端渲染按 CreatedAt DESC 排序,
// 淘汰对最近 N 轮回放无影响。
func (a *Agent) AppendDecisionEntry(entry DecisionEntry) {
	if !a.botsLogDecisions {
		return
	}
	if a.lastTranscript == nil {
		return
	}
	// 补 CreatedAt(若调用方未填)
	if entry.CreatedAt == 0 {
		entry.CreatedAt = time.Now().UnixMilli()
	}
	// 拼接 + FIFO 淘汰
	trail := append(a.lastTranscript.DecisionTrail, entry)
	if len(trail) > decisionTrailMaxEntries {
		trail = trail[len(trail)-decisionTrailMaxEntries:]
	}
	a.lastTranscript.DecisionTrail = trail
}

// botsLogDecisionsEnabled 全局读取决策留痕开关(默认 true;测试环境
// config.Load() panic 时按"关闭"兜底)。
//
// 2026-08-10 §20260810-12 D1:与 cfgWerewolfModelSelfPortraitEnabled 同款
// 兜底模式,避免无配置环境 panic。
func botsLogDecisionsEnabled() (enabled bool) {
	defer func() {
		if recover() != nil {
			enabled = false
		}
	}()
	return config.Load().Werewolf.BotsLogDecisions
}