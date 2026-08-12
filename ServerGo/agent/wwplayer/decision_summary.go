// Package agent — decision_summary.go: 决策可观测性辅助函数(2026-07-09 §重构)。
//
// 设计目标:把"LLM 这次调用收到了什么 + 决定了什么"压缩成几个简短字段,
// 供 BotTranscript 下发到前端,完全替代旧"LastThinking 完整 CoT"展示。
//
// 三个核心函数:
//   - BuildInputSummary   wwtypes.GameContext → 决策输入数字摘要
//   - BuildDecisionSummary tool_use + tool_result → 1 句话决策总结
//   - SanitizeToolInput   工具入参按敏感表脱敏 → JSON 字符串
//
// 任何对 BotTranscript 决策可观测字段的修改都必须保持 wire 兼容(详见
// docs/Agent交互设计.md §2.2 / §4.1)。
package wwplayer

import (
	"LsmWebGame/agent/wwtypes"
	"encoding/json"
	"fmt"
	"strings"
)

// BuildInputSummary 把当前 wwtypes.GameContext 压缩成 ≤ 200 字的"决策输入"摘要,
// 不含任何 CoT 文本、不复制 LLM 输入的全文(这些仍由 Memory 留存供 LLM 多轮)。
//
// 字段说明:
//   - 阶段 + 角色 + 轮数:定位本轮决策的语义背景
//   - 存活玩家数(不含死亡):识别"白天活人局" vs "末日局"
//   - 收到发言数 / whisper 数:本轮新增的情报量
//   - 500K 队列增量(若 ChatQueue 可用):经 ReadPointer 估算
//   - 工具调色板:LLM 本轮可选工具数(仅数量,不暴露工具名细节)
//
// 该函数纯函数、无副作用;在 handleEvent STAGE 1 调用。
func BuildInputSummary(ctx wwtypes.GameContext) string {
	if ctx.Phase == "" {
		return ""
	}
	parts := make([]string, 0, 8)
	// 阶段
	parts = append(parts, "阶段:"+phaseDesc(ctx.Phase))
	// 角色
	if ctx.Role != "" {
		parts = append(parts, "角色:"+ctx.Role)
	}
	// 轮数
	if ctx.Round > 0 {
		parts = append(parts, fmt.Sprintf("第%d天", ctx.Round))
	}
	// 存活数
	parts = append(parts, fmt.Sprintf("存活%d", len(ctx.AliveSeats)))
	// 收到发言数
	if n := len(ctx.RecentSpeeches); n > 0 {
		parts = append(parts, fmt.Sprintf("发言+%d", n))
	}
	// 收到 whisper 数
	if n := len(ctx.WhisperInbox); n > 0 {
		parts = append(parts, fmt.Sprintf("私聊+%d", n))
	}
	// 500K 队列条数
	if n := len(ctx.ChatHistory); n > 0 {
		parts = append(parts, fmt.Sprintf("500K+%d", n))
	}
	if ctx.MyTurn {
		parts = append(parts, "轮到我")
	}
	summary := strings.Join(parts, " | ")
	if len([]rune(summary)) > 200 {
		return string([]rune(summary)[:200]) + "…"
	}
	return summary
}

// BuildDecisionSummary 把"工具名 + 入参 + 结果"压缩成 ≤ 50 字的 1 句话总结,
// 供 AgentInteractionPanel 主区展示"LLM 决定做了什么"。
//
// 入参:
//   - toolName:本次决策的工具名(可能为空 — 表示 LLM end_turn 无工具调用)
//   - toolInput:本次决策的工具入参(可能为 nil)
//   - toolResult:本次决策的工具结果(可能为空)
//
// 典型输出:
//   "speak(3号):我怀疑4号是狼人"
//   "vote → 2号"
//   "wolf_kill → 3号"
//   "idle (沉默思考)"
//   "" (无决策)
func BuildDecisionSummary(toolName string, toolInput map[string]any, toolResult string) string {
	if toolName == "" {
		return ""
	}
	// 解析 target seat(若存在)
	var targetSeat int
	if toolInput != nil {
		if v, ok := toolInput["target"]; ok {
			switch t := v.(type) {
			case int:
				targetSeat = t
			case int32:
				targetSeat = int(t)
			case int64:
				targetSeat = int(t)
			case float64:
				targetSeat = int(t)
			}
		}
	}
	// 输出策略:按工具名分桶
	var summary string
	switch toolName {
	case "speak_with_thought":
		// 2026-08-12 §20260812-03 U4 — 3 条核心理由从 internal_thought 截取填入。
		// 格式:"speak_with_thought(N号):1.<r1> 2.<r2> 3.<r3>" ≤ 80 字
		thought := ""
		text := ""
		if toolInput != nil {
			if v, ok := toolInput["internal_thought"].(string); ok {
				thought = v
			}
			if v, ok := toolInput["text"].(string); ok {
				text = v
			}
		}
		reasons := ExtractThreeReasons(thought)
		if reasons != "" {
			summary = fmt.Sprintf("%s(%d号):%s", toolName, targetSeat+1, reasons)
		} else {
			// 无 internal_thought(理论上 speak_with_thought 必填,fallback 防御)
			summary = fmt.Sprintf("%s(%d号):%s", toolName, targetSeat+1, truncate(text, 30))
		}
	case "speak", "interject", "whisper", "last_words":
		// 取 text 字段前 30 字
		text := ""
		if toolInput != nil {
			if v, ok := toolInput["text"].(string); ok {
				text = v
			}
		}
		summary = fmt.Sprintf("%s(%d号):%s", toolName, targetSeat+1, truncate(text, 30))
	case "vote", "wolf_kill", "seer_check", "witch_act", "hunter_shoot", "sheriff_elect", "sheriff_candidate", "sheriff_vote", "wolf_suicide", "start_day", "finish_speak", "finish_vote",
		// BUG-R213-P3-01 (2026-07-31): 守卫/猎魔人/骑士行动工具与上面同桶,
		// 生成 "tool → N号" 的决策摘要,再由 sanitizeBotTranscript 统一
		// 脱敏目标(观战者/玩家侧显示 "guard_protect → [已隐藏]")。
		"guard_protect", "demon_hunter_hunt", "knight_duel":
		summary = fmt.Sprintf("%s → %d号", toolName, targetSeat+1)
	case "idle_think", "idle_silent":
		summary = "idle (沉默思考)"
	default:
		summary = toolName
	}
	// §20260812-03 U4: speak_with_thought 决策摘要放宽到 80 字(3 条理由),
	// 其它工具仍按 50 字截断(避免 BotTranscript 展示溢出)。
	cap := 50
	if toolName == "speak_with_thought" {
		cap = 80
	}
	if len([]rune(summary)) > cap {
		return string([]rune(summary)[:cap]) + "…"
	}
	return summary
}

// SanitizeToolInput 把 toolInput 序列化成 JSON 字符串,按 sensitiveToolInputs
// 脱敏敏感字段(如 wolf_kill.target、whisper.text → [已隐藏])。
//
// 入参:
//   - toolName:工具名
//   - toolInput:本次工具入参(可能为 nil)
//
// 返回:脱敏后的 JSON 字符串(无花哨格式化,单行)
//
// 设计取舍:
//   - 不暴露 input 中未涉及的字段(LLM 可能多塞字段,但我们只显示已知字段)
//   - 敏感字段不删除而是替换为 "[已隐藏]",让观众看到"LLM 确实有决策过目标"
//   - whisper 的 to_seat / text 都隐藏(对观战者也不暴露对话内容)
func SanitizeToolInput(toolName string, toolInput map[string]any) string {
	if toolInput == nil {
		return ""
	}
	// 拷贝避免污染 caller
	filtered := make(map[string]any, len(toolInput))
	for k, v := range toolInput {
		filtered[k] = v
	}
	// 按敏感表脱敏
	if sens, ok := sensitiveToolInputs[toolName]; ok {
		for k := range filtered {
			if sens[k] {
				filtered[k] = "[已隐藏]"
			}
		}
	}
	// 序列化为单行 JSON
	b, err := json.Marshal(filtered)
	if err != nil {
		return ""
	}
	return string(b)
}

// ExtractThreeReasons 2026-08-12 §20260812-03 U4 — 从 speak_with_thought 的
// internal_thought 字段中截取前 3 条以"1./2./3."或"1、/2、/3、"或"1)/2)/3)"开头的
// 核心理由行，合并为 1 句话填入 LastDecisionSummary。
//
// 截取规则（顺序）：
//  1. 按 \n 拆分整段文本
//  2. 对每一行 trim 后检查前缀匹配 "^[1-3][.、)]"
//  3. 命中则加入结果,直到 3 条为止
//  4. 若无任何命中,返回原文本的前 50 字
//
// 返回值不超过 80 字（§128 LastDecisionSummary 展示约束）。
//
// §119 协议层隔离：本函数是纯字符串处理,不在任何地方把 internal_thought
// 暴露给公屏,仅在 BotTranscript.LastDecisionSummary 中供玩家/观战者审计。
func ExtractThreeReasons(internalThought string) string {
	if internalThought == "" {
		return ""
	}
	lines := strings.Split(internalThought, "\n")
	var picked []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// 匹配 "1." / "1、" / "1)" / "①" 形式的前缀
		runes := []rune(trimmed)
		if len(runes) >= 2 {
			first := runes[0]
			if first >= '1' && first <= '3' {
				second := runes[1]
				if second == '.' || second == '、' || second == ')' {
					// 去掉前缀,保留内容
					content := strings.TrimSpace(string(runes[2:]))
					if content != "" {
						picked = append(picked, content)
						if len(picked) >= 3 {
							break
						}
					}
				}
			}
		}
	}
	if len(picked) == 0 {
		// 无编号:退化为取原文本前 50 字(与 BuildDecisionSummary 长度约束一致)
		return truncate(internalThought, 50)
	}
	// 3 条理由,每条限 14 字 → 合计 "1.x 2.x 3.x" 最多 50 字
	combined := "1." + truncate(picked[0], 14)
	if len(picked) >= 2 {
		combined += " 2." + truncate(picked[1], 14)
	}
	if len(picked) >= 3 {
		combined += " 3." + truncate(picked[2], 14)
	}
	return combined
}

// sensitiveToolInputs 工具名 → 哪些 input 字段必须脱敏(2026-07-09 §重构)。
//
// 与 werewolf/room.go::sensitiveToolNames 协同:该表只定义"input 字段脱敏",
// 工具名/工具结果脱敏由 sanitizeBotTranscript 单独处理。
//
// 字段名必须与 ToolRunner 接收的入参字段名一致(JSON 序列化后 key 名)。
var sensitiveToolInputs = map[string]map[string]bool{
	"wolf_kill":    {"target": true},
	"seer_check":   {"target": true},
	"witch_act":    {"target": true, "action": true}, // action=poison 揭示意图
	"hunter_shoot": {"target": true},
	// BUG-R213-P3-01 (2026-07-31): 守卫/猎魔人/骑士的 target 同为身份
	// 敏感字段(目标本身不直接暴露身份,但配合工具名足以推断)。
	"guard_protect":     {"target": true},
	"demon_hunter_hunt": {"target": true},
	"knight_duel":       {"target": true},
	"vote":              {"target": true},                     // 投票目标已在 Votes map 公开,但 vote_skip 不脱敏
	"whisper":           {"text": true, "to_seat": true},      // whisper 全文对观战者也不暴露
}

// RecordDecisionState 是 run.go 在 STAGE 3/4 之间填充的临时状态;每次 LLM
// 响应后由 DispatchTool 链写入,recordTranscript 读取。
//
// 字段:
//   - LastToolName:本次决策的工具名(空 = LLM end_turn 无工具)
//   - LastToolInput:本次决策的工具入参(可能 nil)
//   - LastToolResult:本次决策的工具结果(可能空)
//   - LastOutcome:决策结果分类
//   - LastDecisionSummary:可选预计算 summary;若非空,recordTranscript 直接用
//     否则自己调 BuildDecisionSummary 算。
//   - DecisionInputs:本轮决策输入摘要
//
// 由 run.go::handleEvent 在 recordAssistant 之后,recordTranscript 之前写入。
type RecordDecisionState struct {
	LastToolName         string
	LastToolInput        map[string]any
	LastToolResult       string
	LastOutcome          string
	LastDecisionSummary  string
	DecisionInputs       string
}

// SetLastDecision 更新 Agent 的当前决策状态(供 recordTranscript 读取)。
//
// 调用方:run.go::handleEvent 在 STAGE 1 (Memory push) 之后调用一次设置
// DecisionInputs;在 STAGE 4 (dispatch 完最后一个 tool_use) 之后调用设置
// LastTool* / LastOutcome。本实现为「全量覆盖」— 第二次调用必须把
// DecisionInputs 字段也带上,否则会被清空。
func (a *Agent) SetLastDecision(state RecordDecisionState) {
	a.Lock()
	defer a.Unlock()
	a.lastDecision = state
}

// MergeLastDecision 合并 Agent 的当前决策状态 — 决策输出字段(LastTool* /
// LastOutcome)会被新值覆盖,DecisionInputs 字段若新值为空则保留旧值。
// 供 run.go::handleEvent 在 STAGE 4 分多次设置决策输出时使用(典型:
// 多 tool_use 的 dispatch 循环里)。
func (a *Agent) MergeLastDecision(state RecordDecisionState) {
	a.Lock()
	defer a.Unlock()
	if state.LastToolName != "" {
		a.lastDecision.LastToolName = state.LastToolName
	}
	if state.LastToolInput != nil {
		a.lastDecision.LastToolInput = state.LastToolInput
	}
	a.lastDecision.LastToolResult = state.LastToolResult
	if state.LastOutcome != "" {
		a.lastDecision.LastOutcome = state.LastOutcome
	}
	if state.DecisionInputs != "" {
		a.lastDecision.DecisionInputs = state.DecisionInputs
	}
}

// LastDecision 返回当前决策状态(供 recordTranscript 读取)。
func (a *Agent) LastDecision() RecordDecisionState {
	a.Lock()
	defer a.Unlock()
	return a.lastDecision
}
