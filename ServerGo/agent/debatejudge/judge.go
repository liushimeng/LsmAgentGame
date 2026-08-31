// Package debatejudge — 裁判 Agent 驱动(2026-08-31 §20260831-01)。
//
// 设计:
//   - 每个裁判是一个独立 goroutine,监听 DebateEngine.JudgeEventChan(idx)
//   - 收到事件后:拉取 DebateContext(全部发言)→ 构造 prompt → 多轮 tool_use 循环
//     (§20260831-03:≤ 5 轮,失败把错误作为 tool_result 喂回 LLM 自纠错)
//     → 调 DebateRoom.AddJudgeScore
//
// 与辩方 Agent 区别:
//   - 评审是单次动作(submit_score),不需要跨轮持久记忆
//   - 多轮循环主要用于:LLM 首次未调 submit_score 时,把错误喂回重试
//   - 工具集固定:submit_score + announce + idle_silent
//
// 详细设计见 docs/辩论比赛/02 §3 + 06 §4。
package debatejudge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"LsmAgentGame/agent"
	"LsmAgentGame/game/debate"
	"LsmAgentGame/llm"
	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// AgentJudge 裁判 Bot。
type AgentJudge struct {
	RoomID   string
	JudgeID  int
	ModelKey string

	registry *llm.Registry
	provider llm.LLMProvider
	apiKey   string
	engine   *debate.DebateEngine
	room     *debate.DebateRoom

	ctx    context.Context
	cancel  context.CancelFunc
	mu      sync.Mutex
}

// NewJudge 构造裁判。
func NewJudge(room *debate.DebateRoom, engine *debate.DebateEngine, judgeID int, modelKey string, registry *llm.Registry) *AgentJudge {
	j := &AgentJudge{
		RoomID:   room.RoomID,
		JudgeID:  judgeID,
		ModelKey: modelKey,
		registry: registry,
		engine:   engine,
		room:     room,
	}
	if registry != nil {
		if p, k, err := registry.Get(modelKey); err == nil {
			j.provider = p
			j.apiKey = k
		}
	}
	return j
}

// ClassName 辩论裁判 AgentClassName。
func (j *AgentJudge) ClassName() agent.AgentClassName {
	return agent.AgentClassDebateJudge
}

// Run 主循环:监听 JudgeEventChan。
func (j *AgentJudge) Run(ctx context.Context) {
	j.ctx, j.cancel = context.WithCancel(ctx)
	defer j.cancel()

	ch := j.engine.JudgeEventChan(j.JudgeID)
	for {
		select {
		case <-ctx.Done():
			return
		case <-j.ctx.Done():
			return
		case <-ch:
			j.runJudgeTurn()
		}
	}
}

// maxToolUseRounds 裁判单轮最多 tool_use 重试轮次(§20260831-03)。
const maxToolUseRounds = 5

// llmTurnTimeoutSec 单次 LLM 调用超时(秒)。
const llmTurnTimeoutSec = 90

// runJudgeTurn 执行一轮评审(多轮 tool_use 循环)。
//
// 流程(对齐 docs/辩论比赛/02 §3.5 + §5.1):
//
//	user prompt 入 memory → 循环(≤ maxToolUseRounds):
//	  Chat(memory 快照) → 无 tool_use:
//	    · 本轮已派发过 submit_score → 结束
//	    · 未派发 → 把错误喂回 LLM 重试
//	  有 tool_use → 逐个派发 → tool_result(含错误信息)回填 memory:
//	    · submit_score 成功 → 终态,结束
//	    · 全部失败 → 把错误喂回 LLM 重试(下一轮循环)
//	LLM 调用失败 / 超过轮次上限 → fallback 评分。
func (j *AgentJudge) runJudgeTurn() {
	if j.provider == nil {
		j.useFallback()
		return
	}

	// 1) 构造 system prompt
	systemPrompt := buildJudgeSystemPrompt()

	// 2) 收集全部发言
	speeches := j.room.Speeches()
	userPrompt := buildJudgeUserPrompt(j.room, speeches)

	// 3) 工具集(submit_score + announce + idle_silent)
	tools := judgeTools()

	if !j.engine.Manager().AcquireLLM(j.ctx) {
		j.useFallback()
		return
	}
	defer j.engine.Manager().ReleaseLLM()

	// 4) 多轮 tool_use 循环
	messages := []llm.Message{
		{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: userPrompt}}},
	}

	scoreSubmitted := false
	for round := 0; round < maxToolUseRounds; round++ {
		llmCtx, cancel := context.WithTimeout(j.ctx, llmTurnTimeoutSec*time.Second)
		req := llm.LLMRequest{
			AgentClassName: string(j.ClassName()),
			Model:          j.ModelKey,
			System:         []llm.SystemBlock{{Type: "text", Text: systemPrompt}},
			Messages:       sanitizeJudgeMessages(messages),
			Tools:          tools,
			MaxTokens:      2048,
		}
		resp, err := j.provider.Chat(llmCtx, j.apiKey, req)
		cancel()
		if err != nil {
			logger.L().Warn("debate judge: LLM call failed, using fallback",
				zap.String("room_id", j.RoomID),
				zap.String("model", j.ModelKey),
				zap.Int("round", round),
				zap.Error(err))
			if !scoreSubmitted {
				j.useFallback()
			}
			return
		}

		// assistant 回复原样入 memory
		messages = append(messages, llm.Message{Role: "assistant", Content: cloneBlocks(resp.Content)})

		var toolUses []llm.ContentBlock
		for _, blk := range resp.Content {
			if blk.Type == "tool_use" {
				toolUses = append(toolUses, blk)
			}
		}

		if len(toolUses) == 0 {
			// 纯文本回复:本轮尚未成功提交评分 → 把错误喂回 LLM 重试
			if !scoreSubmitted {
				messages = append(messages, llm.Message{
					Role: "user",
					Content: []llm.ContentBlock{{Type: "text", Text: "你必须调用 submit_score 工具提交评分。请重新调用 submit_score 提交完整评分。"}},
				})
				continue
			}
			return
		}

		// 逐个派发 tool_use,并构造 tool_result 回填
		results := make([]llm.ContentBlock, 0, len(toolUses))
		for _, blk := range toolUses {
			res := j.dispatchTool(blk)
			if res.OK && blk.Name == string(debate.ToolJudgeSubmitScore) {
				scoreSubmitted = true
			}
			results = append(results, llm.ContentBlock{
				Type:      "tool_result",
				ToolUseID: blk.ID,
				Content:   []llm.ContentBlock{{Type: "text", Text: judgeResultText(res)}},
				IsError:   !res.OK,
			})
		}
		messages = append(messages, llm.Message{Role: "user", Content: results})

		if scoreSubmitted {
			// submit_score 已成功提交,本轮终态
			return
		}
		// 全部派发失败 → 错误已作为 tool_result 喂回,进入下一轮重试
		logger.L().Warn("debate judge: all tool_use failed, retrying",
			zap.String("room_id", j.RoomID),
			zap.Int("judge", j.JudgeID),
			zap.Int("round", round))
	}

	// 超过轮次上限仍未成功 → fallback
	if !scoreSubmitted {
		j.useFallback()
	}
}

// dispatchTool 派发 LLM 返回的 tool_use。
func (j *AgentJudge) dispatchTool(blk llm.ContentBlock) judgeActionResult {
	switch blk.Name {
	case string(debate.ToolJudgeSubmitScore):
		inputBytes, _ := json.Marshal(blk.Input)
		return j.dispatchSubmitScore(inputBytes)
	case string(debate.ToolJudgeAnnounce):
		// §20260831-06 — announce 不再是空操作:经 manager 钩子广播
		// debate.judge_announce 帧给全体观众(首期实现文本被吞掉)。
		text := extractAnnounceText(blk.Input)
		if mgr := j.engine.Manager(); mgr != nil {
			mgr.EmitJudgeAnnounce(j.RoomID, j.JudgeID, text)
		}
		return judgeActionResult{OK: true, Message: "announce broadcasted"}
	case string(debate.ToolJudgeAnswerSpectator):
		// §20260831-06 — 回答观众提问(观众提问闭环)。
		inputBytes, _ := json.Marshal(blk.Input)
		return j.dispatchAnswerSpectator(inputBytes)
	case string(debate.ToolIdleSilent):
		return judgeActionResult{OK: true, Message: "idle"}
	default:
		return judgeActionResult{OK: false, Message: "unknown tool: " + blk.Name}
	}
}

// judgeActionResult 裁判工具派发结果。
type judgeActionResult struct {
	OK      bool
	Message string
}

// judgeResultText 把 judgeActionResult 转成回填给 LLM 的 tool_result 文本。
func judgeResultText(res judgeActionResult) string {
	if res.OK {
		return "ok: " + res.Message
	}
	return "error: " + res.Message
}

// dispatchAnswerSpectator 派发 answer_spectator(§20260831-06)。
//
// 观众提问闭环:提问在评审 prompt 的【观众提问】段注入;裁判选择性回答,
// 回答写回房间提问队列并经 debate.spectator_answer 帧广播给观众。
// 已回答的提问幂等返回成功(不重复广播)。
func (j *AgentJudge) dispatchAnswerSpectator(input json.RawMessage) judgeActionResult {
	var payload struct {
		QuestionID string `json:"question_id"`
		Answer     string `json:"answer"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return judgeActionResult{OK: false, Message: "invalid answer_spectator input: " + err.Error()}
	}
	if payload.QuestionID == "" {
		return judgeActionResult{OK: false, Message: "question_id is required"}
	}
	if strings.TrimSpace(payload.Answer) == "" {
		return judgeActionResult{OK: false, Message: "answer is required"}
	}
	q, err := j.room.AnswerSpectatorQuestion(j.JudgeID, payload.QuestionID, payload.Answer)
	if err != nil {
		return judgeActionResult{OK: false, Message: "answer failed: " + err.Error()}
	}
	return judgeActionResult{OK: true, Message: "answered question " + q.ID}
}

// extractAnnounceText 从 announce 工具 input 中取 text 字段。
//
// blk.Input 是 map[string]any(tool_use wire 解码结果),直接取键。
func extractAnnounceText(input map[string]any) string {
	if input == nil {
		return ""
	}
	text, _ := input["text"].(string)
	return strings.TrimSpace(text)
}

// dispatchSubmitScore 派发 submit_score。
func (j *AgentJudge) dispatchSubmitScore(input json.RawMessage) judgeActionResult {
	var payload struct {
		Rankings       []debate.TeamRanking `json:"rankings"`
		OverallComment string               `json:"overall_comment"`
		WinnerTeamID   int                  `json:"winner_team_id"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		logger.L().Warn("debate judge: invalid submit_score input",
			zap.Error(err))
		return judgeActionResult{OK: false, Message: "invalid submit_score input: " + err.Error()}
	}

	// 校验每个 team 都有评分
	teamCount := j.room.TeamCount()
	score := debate.JudgeScore{
		JudgeID:        j.JudgeID,
		ModelKey:       j.ModelKey,
		Rankings:       payload.Rankings,
		OverallComment: payload.OverallComment,
		WinnerTeamID:   payload.WinnerTeamID,
		IsFallback:     false,
	}

	// §20260831-02 — 钳制 best_debater 到该队合法座位区间。
	// 实测有模型把「四辩」当 1-based 提交 4,越界座位号导致
	// 最终结果 BestDebater.Name 查空。
	for i := range score.Rankings {
		score.Rankings[i].BestDebater = j.clampBestDebater(
			score.Rankings[i].TeamID, score.Rankings[i].BestDebater)
	}

	// 补齐缺失队伍
	if len(score.Rankings) < teamCount {
		for t := 0; t < teamCount; t++ {
			found := false
			for _, r := range score.Rankings {
				if r.TeamID == t {
					found = true
					break
				}
			}
			if !found {
				score.Rankings = append(score.Rankings, debate.TeamRanking{
					TeamID: t,
					Scores: debate.ScoreDimensions{
						ArgumentQuality:       5,
						LogicRigor:            5,
						LanguageExpression:    5,
						TeamCoordination:      5,
						RebuttalEffectiveness: 5,
					},
					TotalScore:  25.0,
					Comment:     "未评分(裁判未覆盖此队)",
					BestDebater: 0,
				})
			}
		}
	}

	j.room.AddJudgeScore(score)
	return judgeActionResult{OK: true, Message: "score submitted"}
}

// useFallback LLM 失败时使用默认评分。
func (j *AgentJudge) useFallback() {
	score := debate.FallbackJudgeScore(j.JudgeID, j.ModelKey, j.room.TeamCount())
	j.room.AddJudgeScore(score)
}

// clampBestDebater 把裁判提交的最佳辩手座位号钳制到该队合法区间。
//
// LLM 可能按 1-based 提交(「四辩」→4)或直接幻觉越界值;
// 找不到座位时回退 0(一辩)。队伍不存在时返回 0。
func (j *AgentJudge) clampBestDebater(teamID, seat int) int {
	for _, t := range j.room.Config.Teams {
		if t.TeamID != teamID || len(t.Agents) == 0 {
			continue
		}
		for _, a := range t.Agents {
			if a.SeatID == seat {
				return seat
			}
		}
		// 命中队伍但座位越界 → 尝试 1-based 换算,再退一辩
		if seat >= 1 && seat <= len(t.Agents) {
			return seat - 1
		}
		return t.Agents[0].SeatID
	}
	return 0
}

// ============================================================================
// 消息清洗(对齐 CLAUDE.md §14.1)
// ============================================================================

// sanitizeJudgeMessages 把裁判 messages 清洗为可直接下发的格式:
//
//  1. 收集 assistant 消息中的全部 tool_use id;
//  2. user 消息里引用未知 id 的 tool_result 块(悬空孤儿)直接剔除;
//  3. 相邻同 role 消息合并(user+user / assistant+assistant → 单条拼接);
//  4. 剔除后为空的消息丢弃;首条若为 assistant 则丢弃(对话必须 user 开头)。
func sanitizeJudgeMessages(msgs []llm.Message) []llm.Message {
	if len(msgs) == 0 {
		return msgs
	}

	// Pass 1: 收集已知 tool_use id
	knownUseIDs := map[string]bool{}
	for _, m := range msgs {
		if m.Role != "assistant" {
			continue
		}
		for _, c := range m.Content {
			if c.Type == "tool_use" && c.ID != "" {
				knownUseIDs[c.ID] = true
			}
		}
	}

	// Pass 2: 剔除悬空 tool_result 块 + 过滤空消息
	cleaned := make([]llm.Message, 0, len(msgs))
	for _, m := range msgs {
		var content []llm.ContentBlock
		for _, c := range m.Content {
			if c.Type == "tool_result" && c.ToolUseID != "" && !knownUseIDs[c.ToolUseID] {
				continue
			}
			content = append(content, c)
		}
		if len(content) == 0 {
			continue
		}
		cm := m
		cm.Content = content
		cleaned = append(cleaned, cm)
	}

	// Pass 3: 相邻同 role 合并
	merged := make([]llm.Message, 0, len(cleaned))
	for _, m := range cleaned {
		if n := len(merged); n > 0 && merged[n-1].Role == m.Role {
			merged[n-1].Content = append(merged[n-1].Content, m.Content...)
			continue
		}
		merged = append(merged, m)
	}

	// Pass 4: 对话必须以 user 开头
	for len(merged) > 0 && merged[0].Role != "user" {
		merged = merged[1:]
	}
	return merged
}

// cloneBlocks 深拷贝 content 块。
func cloneBlocks(blocks []llm.ContentBlock) []llm.ContentBlock {
	out := make([]llm.ContentBlock, len(blocks))
	copy(out, blocks)
	return out
}

// ============================================================================
// Prompt 构建
// ============================================================================

// buildJudgeSystemPrompt 裁判系统提示词。
func buildJudgeSystemPrompt() string {
	return judgeSystemBase
}

// judgeSystemBase 裁判系统提示词常量。
const judgeSystemBase = `【裁判身份 — 硬约束】
❶ 你是一场 AI 辩论比赛的裁判,负责独立评审辩论质量并打分。
❷ 你不是辩手,不参与辩论,只评审。
❸ 你必须保持客观公正,不偏向任何一方。
❹ 你的评分必须基于辩论内容本身,不考虑辩手的模型/身份。
═══════════════════════════════════════════════════════════════
【评分维度】(每维度 1-10 分)
1. 论证质量(argument_quality):论点是否清晰、论据是否充分、论证是否有力
2. 逻辑严谨(logic_rigor):逻辑链条是否完整、是否存在逻辑漏洞
3. 语言表达(language_expression):表达是否清晰、语言是否优美、是否有说服力
4. 团队配合(team_coordination):队友之间是否配合默契、论点是否一致互补
5. 反驳效力(rebuttal_effectiveness):反驳是否精准、是否有效瓦解对方论点
═══════════════════════════════════════════════════════════════
【评分原则】
- 严格按维度打分,不凭印象给分
- 评语需具体指出亮点和不足
- 最佳辩手需给出明确理由
- 综合 5 维度得分 + 评语确定胜方
═══════════════════════════════════════════════════════════════
【输出格式】
- 评分通过 submit_score 工具提交
- 评语使用纯文本,不使用 Markdown/JSON
- 整体评语 ≤ 300 字,每队评语 ≤ 200 字
`

// buildJudgeUserPrompt 裁判 user prompt。
func buildJudgeUserPrompt(room *debate.DebateRoom, speeches []debate.Speech) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("【辩题】%s(%s)\n\n", room.Config.Topic.Text, room.Config.Topic.Type))
	b.WriteString("【队伍配置】\n")
	for _, t := range room.Config.Teams {
		b.WriteString(fmt.Sprintf("  队 %d (%s):\n", t.TeamID, debate.StanceLabel(t.Stance)))
		for _, a := range t.Agents {
			b.WriteString(fmt.Sprintf("    - %s(%s, %s 模型)\n",
				debate.RoleCN(a.Role), debate.RoleName(a.Role), debate.ModelShort(a.ModelKey)))
		}
	}
	b.WriteString("\n【全场发言记录】\n")
	for _, sp := range speeches {
		b.WriteString(fmt.Sprintf("  [%s][%s] %s:\n",
			debate.PhaseCN(sp.Phase),
			debate.StanceLabel(debate.Stance(sp.Stance)),
			sp.SpeakerName))
		b.WriteString(fmt.Sprintf("    %s\n\n", truncateForJudge(sp.Content, 200)))
	}

	// §20260831-06 — 注入未回答的观众提问(观众提问闭环,§01 §6.1)。
	// 裁判可选择性回答:调 answer_spectator 工具;不值得回答的可跳过。
	unanswered := room.UnansweredSpectatorQuestions()
	if len(unanswered) > 0 {
		if len(unanswered) > 10 {
			unanswered = unanswered[len(unanswered)-10:] // 只注入最近 10 条
		}
		b.WriteString("\n【观众提问】(观众向裁判的现场提问,可选择性回应)\n")
		for _, q := range unanswered {
			b.WriteString(fmt.Sprintf("  - [%s] %s\n", q.ID, truncateForJudge(q.Text, 100)))
		}
		b.WriteString("  说明:值得回应的提问请用 answer_spectator 工具简短回答(≤100 字);与评审无关的提问可忽略。\n")
	}

	b.WriteString("\n【任务】\n请按 submit_score 工具的格式,对每支队伍进行 5 维度评分(1-10),并给出评语 + 整体评语 + 胜方。\n")
	b.WriteString("提交评分前,可先调用 announce 向观众播报评审开始,并用 answer_spectator 回应值得回答的观众提问。\n")
	return b.String()
}

// truncateForJudge 截断发言用于裁判评审 prompt(简化)。
func truncateForJudge(s string, max int) string {
	count := 0
	for i := range s {
		if count == max {
			return s[:i] + "..."
		}
		count++
	}
	return s
}

// judgeTools 裁判工具集。
func judgeTools() []llm.ToolDef {
	return []llm.ToolDef{
		{
			Name: string(debate.ToolJudgeSubmitScore),
			Description: "提交评分 — 对一场辩论的完整评分。\n" +
				"5 个维度各 1-10 分,附带评语。必须对所有队伍评分。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"rankings": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"team_id": map[string]any{"type": "integer"},
								"scores": map[string]any{
									"type": "object",
									"properties": map[string]any{
										"argument_quality":        map[string]any{"type": "number", "minimum": 1, "maximum": 10},
										"logic_rigor":             map[string]any{"type": "number", "minimum": 1, "maximum": 10},
										"language_expression":     map[string]any{"type": "number", "minimum": 1, "maximum": 10},
										"team_coordination":       map[string]any{"type": "number", "minimum": 1, "maximum": 10},
										"rebuttal_effectiveness":  map[string]any{"type": "number", "minimum": 1, "maximum": 10},
									},
								},
								"total_score":   map[string]any{"type": "number"},
								"comment":       map[string]any{"type": "string", "description": "对该队的评语,≤ 200 字"},
								"best_debater":  map[string]any{"type": "integer", "description": "该队最佳辩手座位号"},
							},
							"required": []string{"team_id", "scores", "total_score", "comment"},
						},
					},
					"overall_comment": map[string]any{"type": "string", "description": "整体评语,≤ 300 字"},
					"winner_team_id":  map[string]any{"type": "integer", "description": "获胜队伍 ID"},
				},
				"required": []string{"rankings", "overall_comment", "winner_team_id"},
			},
		},
		{
			Name:        string(debate.ToolJudgeAnnounce),
			Description: "公开宣告 — 裁判对全体观众的口语播报。适用:评审开始/评分公布/点评。≤ 100 字。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"text": map[string]any{"type": "string", "description": "宣告文本,≤ 100 字"},
				},
				"required": []string{"text"},
			},
		},
		{
			Name: string(debate.ToolJudgeAnswerSpectator),
			Description: "回答观众提问 — 对【观众提问】中的某条提问给出简短回应。\n" +
				"选择性使用:只回答与评审/辩论相关、值得回应的问题;每条 ≤ 100 字;\n" +
				"回答必须客观中立,不得透露其他裁判的评分倾向。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"question_id": map[string]any{"type": "string", "description": "被回答的提问 ID(来自【观众提问】段)"},
					"answer":      map[string]any{"type": "string", "description": "回答内容,≤ 100 字"},
				},
				"required": []string{"question_id", "answer"},
			},
		},
		{
			Name:        string(debate.ToolIdleSilent),
			Description: "本轮不出声。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"reason": map[string]any{"type": "string"},
				},
				"required": []string{"reason"},
			},
		},
	}
}
