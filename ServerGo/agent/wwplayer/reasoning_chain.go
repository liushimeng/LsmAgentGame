// Package agent — reasoning_chain.go: 推理链可视化 (§20260811-06 U3)。
//
// 设计动机:
//   - LLM 在关键决策(vote / speak / night_action)前可选择调用
//     reasoning_chain 工具,显性化其推理步骤(steps / evidence / conclusion / confidence)
//     而不仅是最终决策。
//   - 与 §119 HeartThought(协议层隔离,仅本人+观战者可见)的区别:
//     reasoning_chain 是**公开可辩论**的:LLM 选择公开自己的推理链,
//     玩家也可以"反驳"链中的证据。属 §128 对话即思考的扩展。
//   - 前端 HistoryDrawer 第 7 sub-tab「🧩 推理链」渲染(仅 spectator)。
//
// §130 接线验证:
//   - tools.go::BuildTools 挂载到 speak / vote / night_action 三阶段;
//   - agent_runner.go::recordTranscript / dispatchReasoningChain 写 BotTranscript.ReasoningChains;
//   - view.go::sanitizeBotTranscript 玩家分支清空 ReasoningChains(§135 隔离)。
//
// §120 公平性:ReasoningChains 走 opt-in(默认开);不计入 consecutiveFailures,
// 不走 quarantine 路径,误判零成本。
package wwplayer

import (
	"time"

	"LsmWebGame/config"
)

// reasoningChainMaxEntries 推理链最大保留条数。
// 决策频次低于 DecisionTrail(后者记录每一次 LLM 决策),30 条足矣。
const reasoningChainMaxEntries = 10

// ReasoningChainEntry 单次公开推理链结构(§20260811-06 U3)。
//
// JSON wire ≤ 200 字(典型):
//   {
//     "round": 2,
//     "phase": "speak",
//     "topic": "为什么投 5 号",
//     "steps": ["5 号票型跟 8 号一致", "8 号是已确认狼", "5 号可能悍跳"],
//     "evidence": ["第 1 轮 5 号投票给 8 号", "第 2 轮 5 号发言赞同 8 号"],
//     "conclusion": "5 号是狼,投 5 号",
//     "confidence": 72,
//     "created_at": 1754912345000
//   }
//
// 字段语义:
//   - Round:1-based 天数(0 = 夜间未明 / pre-day);
//   - Phase:阶段 key(speak / vote / night_wolves / ...);
//   - Topic:本条推理的主题,LLM 自填,≤20 字;
//   - Steps:1-3 步推理,每步 ≤30 字;
//   - Evidence:1-3 条证据,每条 ≤30 字;
//   - Conclusion:最终结论,≤40 字;
//   - Confidence:0-100,LLM 自评置信度;
//   - CreatedAt:unix 毫秒。
type ReasoningChainEntry struct {
	Round      int      `json:"round"`
	Phase      string   `json:"phase,omitempty"`
	Topic      string   `json:"topic,omitempty"`
	Steps      []string `json:"steps,omitempty"`
	Evidence   []string `json:"evidence,omitempty"`
	Conclusion string   `json:"conclusion,omitempty"`
	Confidence int      `json:"confidence,omitempty"`
	CreatedAt  int64    `json:"created_at,omitempty"`
}

// AppendReasoningChain 追加一条推理链到 BotTranscript。已持 a.mu。
//
// 与 AppendDecisionEntry 同款模式:上限 10 条 FIFO 淘汰;
// opt-in 开关 `werewolf.reasoning_chain_enabled`(默认 true);
// 关闭时 AppendReasoningChain 提前 return,BotTranscript.ReasoningChains
// 字段保持 nil → JSON omitempty → wire 上零开销。
//
// 与 §128 对话即思考的协调:本结构不替代 LastDecisionSummary(后者是
// 单决策一句话),而是 LLM 在**关键决策**前可选择**追加**的更详细推理;
// LLM 调用 reasoning_chain 后,正常继续调 speak/vote/...;两条记录并存。
func (a *Agent) AppendReasoningChain(entry ReasoningChainEntry) {
	if !reasoningChainEnabled() {
		return
	}
	if a.lastTranscript == nil {
		return
	}
	if entry.CreatedAt == 0 {
		entry.CreatedAt = time.Now().UnixMilli()
	}
	// 拼接 + FIFO 淘汰
	chains := append(a.lastTranscript.ReasoningChains, entry)
	if len(chains) > reasoningChainMaxEntries {
		chains = chains[len(chains)-reasoningChainMaxEntries:]
	}
	a.lastTranscript.ReasoningChains = chains
}

// reasoningChainEnabled 全局读取推理链开关(默认 true;测试环境
// config.Load() panic 时按"关闭"兜底)。
func reasoningChainEnabled() (enabled bool) {
	defer func() {
		if recover() != nil {
			enabled = false
		}
	}()
	return config.Load().Werewolf.ReasoningChainEnabled
}
