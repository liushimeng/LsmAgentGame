// Package debatejudge — 裁判 Agent 驱动(2026-08-31 §20260831-01)。
//
// 简化设计:
//   - 每个裁判是一个独立 goroutine,监听 DebateEngine.JudgeEventChan(idx)
//   - 收到事件后:拉取 DebateContext(全部发言)→ 构造 prompt → 调 LLM → 解析 submit_score → 调 DebateRoom.AddJudgeScore
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

// runJudgeTurn 执行一轮评审。
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

	// 3) 工具集(只 submit_score + announce)
	tools := judgeTools()

	// 4) 调 LLM(限流 + 超时)
	if !j.engine.Manager().AcquireLLM(j.ctx) {
		j.useFallback()
		return
	}
	defer j.engine.Manager().ReleaseLLM()

	llmCtx, cancel := context.WithTimeout(j.ctx, 90*time.Second)
	defer cancel()

	messages := []llm.Message{
		{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: userPrompt}}},
	}
	req := llm.LLMRequest{
		AgentClassName: string(j.ClassName()),
		Model:          j.ModelKey,
		System:         []llm.SystemBlock{{Type: "text", Text: systemPrompt}},
		Messages:       messages,
		Tools:          tools,
		MaxTokens:      2048,
	}

	resp, err := j.provider.Chat(llmCtx, j.apiKey, req)
	if err != nil {
		logger.L().Warn("debate judge: LLM call failed, using fallback",
			zap.String("room_id", j.RoomID),
			zap.String("model", j.ModelKey),
			zap.Error(err))
		j.useFallback()
		return
	}

	// 5) 解析 tool_use
	for _, blk := range resp.Content {
		if blk.Type != "tool_use" {
			continue
		}
		if blk.Name == string(debate.ToolJudgeSubmitScore) {
			// tool_use.input 是 map[string]any;marshal 后解析
			inputBytes, _ := json.Marshal(blk.Input)
			j.dispatchSubmitScore(inputBytes)
			return
		}
	}
	// LLM 未调 submit_score → 用 fallback
	j.useFallback()
}

// dispatchSubmitScore 派发 submit_score。
func (j *AgentJudge) dispatchSubmitScore(input json.RawMessage) {
	var payload struct {
		Rankings       []debate.TeamRanking `json:"rankings"`
		OverallComment string               `json:"overall_comment"`
		WinnerTeamID   int                  `json:"winner_team_id"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		logger.L().Warn("debate judge: invalid submit_score input",
			zap.Error(err))
		j.useFallback()
		return
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

	b.WriteString("\n【任务】\n请按 submit_score 工具的格式,对每支队伍进行 5 维度评分(1-10),并给出评语 + 整体评语 + 胜方。\n")
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
	}
}