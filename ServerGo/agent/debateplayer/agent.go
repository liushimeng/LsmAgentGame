// Package debateplayer — 辩方 Agent 驱动(2026-08-31 §20260831-01)。
//
// 简化设计:
//   - 每个 Bot 是一个独立 goroutine,监听 DebateEngine.BotEventChan(team, seat)
//   - 收到事件后:拉取 DebateContext → 构造 prompt → 调 LLM → 解析 tool_use → 调 DebateRoom.SubmitSpeech/CrossExam*/SubmitFreeDebateSpeech
//   - 简化:Memory 单段线性追加,无复杂压缩(由阶段四 §2.7 设计文档 §4.1 指导)
//
// 与狼人杀 Agent 区别:
//   - 单一角色驱动(立论/驳论/质询/总结),无角色切换
//   - 工具集随 phase + role 动态过滤
//   - 无记忆压缩 / 跨局 MEMORY.md 迭代(本版本)
//
// 详细设计见 docs/辩论比赛/02-辩论比赛Agent设计.md。
package debateplayer

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

// Agent 辩方 Bot。
type Agent struct {
	RoomID  string
	TeamID  int
	Seat    int
	Role    debate.Role
	Stance  debate.Stance
	ModelKey string
	BotUserID string

	// LLM
	registry *llm.Registry
	provider llm.LLMProvider
	apiKey   string

	// 记忆
	memory *Memory

	// 引擎引用(用于事件 channel)
	engine *debate.DebateEngine

	// ctx
	ctx    context.Context
	cancel cancel

	// 状态
	mu         sync.Mutex
	quarantined bool
}

// cancel 内部 cancel func 类型(避免 context 字段名冲突)。
type cancel = context.CancelFunc

// NewAgent 创建辩方 Agent。
func NewAgent(
	room *debate.DebateRoom,
	engine *debate.DebateEngine,
	teamID, seat int,
	role debate.Role,
	stance debate.Stance,
	modelKey string,
	registry *llm.Registry,
) *Agent {
	a := &Agent{
		RoomID:   room.RoomID,
		TeamID:   teamID,
		Seat:     seat,
		Role:     role,
		Stance:   stance,
		ModelKey: modelKey,
		registry: registry,
		memory:   NewMemory(room, teamID, seat, role, stance),
		engine:   engine,
	}
	if registry != nil {
		if p, k, err := registry.Get(modelKey); err == nil {
			a.provider = p
			a.apiKey = k
		}
	}
	return a
}

// ClassName 始终是辩论玩家(对齐 CLAUDE.md §24 AgentClassName 登记)。
func (a *Agent) ClassName() agent.AgentClassName {
	return agent.AgentClassDebatePlayer
}

// Run 主循环:监听 engine.BotEventChan,收到事件后发起 LLM 调用。
//
// 本版本:每收到一次事件 = 一次"轮到我"提示,执行一次完整的 tool_use 循环
// (最多 5 次 tool_use),完成后退出本轮,等待下次事件。
func (a *Agent) Run(ctx context.Context) {
	a.ctx, a.cancel = context.WithCancel(ctx)
	defer a.cancel()

	ch := a.engine.BotEventChan(a.TeamID, a.Seat)
	for {
		select {
		case <-ctx.Done():
			return
		case <-a.ctx.Done():
			return
		case <-ch:
			if a.isQuarantined() {
				continue
			}
			a.runTurn()
		}
	}
}

// runTurn 执行一轮:调 LLM → 解析 tool_use → 派发。
func (a *Agent) runTurn() {
	// 1) 取引擎事件(已经触发,跳过)
	if a.provider == nil {
		logger.L().Warn("debate agent: llm provider is nil, skipping turn",
			zap.String("room_id", a.RoomID),
			zap.Int("team", a.TeamID),
			zap.Int("seat", a.Seat))
		a.useFallbackResponse()
		return
	}

	// 2) 构造 system prompt
	systemPrompt := a.buildSystemPrompt()

	// 3) 构造 user prompt(基于 DebateContext)
	gc := a.collectContext()
	userPrompt := a.buildUserPrompt(gc)

	// 4) 取工具集
	tools := a.buildTools(gc.Phase)

	// 5) 调 LLM(限流 + 超时)
	if !a.engine.Manager().AcquireLLM(a.ctx) {
		logger.L().Warn("debate agent: failed to acquire LLM slot",
			zap.String("room_id", a.RoomID))
		return
	}
	defer a.engine.Manager().ReleaseLLM()

	llmCtx, cancel := context.WithTimeout(a.ctx, 90*time.Second)
	defer cancel()

	// 6) 调 LLM(简化:仅 1 次 Chat;不循环 tool_use)
	messages := []llm.Message{
		{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: userPrompt}}},
	}

	req := llm.LLMRequest{
		AgentClassName: string(a.ClassName()),
		Model:          a.ModelKey,
		System:         []llm.SystemBlock{{Type: "text", Text: systemPrompt}},
		Messages:       messages,
		Tools:          tools,
		MaxTokens:      1024,
	}

	resp, err := a.provider.Chat(llmCtx, a.apiKey, req)
	if err != nil {
		logger.L().Warn("debate agent: LLM call failed, using fallback",
			zap.String("room_id", a.RoomID),
			zap.String("model", a.ModelKey),
			zap.Error(err))
		a.useFallbackResponse()
		return
	}

	// 7) 解析 tool_use(简化:取第一个 tool_use 块)
	toolUsed := false
	for _, blk := range resp.Content {
		if blk.Type != "tool_use" {
			continue
		}
		a.dispatchTool(blk)
		toolUsed = true
		break
	}
	if !toolUsed {
		// LLM 直接返回文本 → fallback 当作 speech
		a.useFallbackResponse()
	}
}

// dispatchTool 派发 LLM 返回的 tool_use。
func (a *Agent) dispatchTool(blk llm.ContentBlock) {
	r := a.engine.Room()
	if r == nil {
		return
	}

	// tool_use.input 是 map[string]any(由 provider 反序列化);二次解析时
	// 用 json.Marshal/Unmarshal 还原成目标结构体。
	inputBytes, err := json.Marshal(blk.Input)
	if err != nil {
		logger.L().Warn("debate agent: marshal tool input failed",
			zap.Error(err))
		return
	}

	switch blk.Name {
	case string(debate.ToolSpeech):
		var input struct {
			Content         string   `json:"content"`
			References      []string `json:"references,omitempty"`
			InternalThought string   `json:"internal_thought,omitempty"`
		}
		if err := json.Unmarshal(inputBytes, &input); err != nil {
			logger.L().Warn("debate agent: invalid speech input",
				zap.Error(err))
			return
		}
		r.SubmitSpeech(a.TeamID, a.Seat, debate.SpeechParams{
			Content:         input.Content,
			References:      input.References,
			InternalThought: input.InternalThought,
		})
	case string(debate.ToolCrossExamQuestion):
		var input struct {
			TargetTeam int    `json:"target_team"`
			TargetSeat int    `json:"target_seat"`
			Question   string `json:"question"`
		}
		if err := json.Unmarshal(inputBytes, &input); err != nil {
			return
		}
		r.SubmitCrossExamQuestion(a.TeamID, a.Seat, debate.CrossExamQuestionParams{
			TargetTeam: input.TargetTeam,
			TargetSeat: input.TargetSeat,
			Question:   input.Question,
		})
	case string(debate.ToolCrossExamAnswer):
		var input struct {
			QuestionID string `json:"question_id"`
			Answer     string `json:"answer"`
		}
		if err := json.Unmarshal(inputBytes, &input); err != nil {
			return
		}
		r.SubmitCrossExamAnswer(a.Seat, debate.CrossExamAnswerParams{
			QuestionID: input.QuestionID,
			Answer:     input.Answer,
		})
	case string(debate.ToolFreeDebateSpeak):
		var input struct {
			Content string `json:"content"`
		}
		if err := json.Unmarshal(inputBytes, &input); err != nil {
			return
		}
		r.SubmitFreeDebateSpeech(a.TeamID, a.Seat, debate.FreeDebateParams{
			Content: input.Content,
		})
	case string(debate.ToolFinishSpeak):
		r.FinishSpeak(a.TeamID, "finished")
	case string(debate.ToolIdleSilent):
		r.IdleSilent(a.Seat, "idle")
	}
}

// useFallbackResponse LLM 失败 / 返回纯文本时使用 fallback 发言。
//
// 简化策略:输出一段固定模板,直接通过 SubmitSpeech 提交。
func (a *Agent) useFallbackResponse() {
	r := a.engine.Room()
	if r == nil {
		return
	}
	phase := r.Phase()
	content := buildFallbackText(phase, a.Stance, a.Role)
	r.SubmitSpeech(a.TeamID, a.Seat, debate.SpeechParams{
		Content:         content,
		InternalThought: "[fallback]",
	})
}

// buildFallbackText 生成 fallback 文本。
func buildFallbackText(phase debate.Phase, stance debate.Stance, role debate.Role) string {
	stanceCN := debate.StanceLabel(stance)
	roleCN := debate.RoleCN(role)
	switch phase {
	case debate.PhaseOpeningArgument:
		return fmt.Sprintf("[%s%s立论] 我方坚持%s立场。核心论点是:基于逻辑推理与常识,我方认为这一立场更具合理性。",
			stanceCN, roleCN, stanceCN)
	case debate.PhaseRebuttal:
		return fmt.Sprintf("[%s%s驳论] 对方观点存在逻辑漏洞,我方坚持己方立场更具说服力。",
			stanceCN, roleCN)
	case debate.PhaseCrossExamSummary:
		return fmt.Sprintf("[%s质询小结] 综合质询交锋,对方关键论点难以成立。", stanceCN)
	case debate.PhaseClosingArgument:
		return fmt.Sprintf("[%s%s总结] 全场攻防显示我方论点更扎实、论证更完整。",
			stanceCN, roleCN)
	case debate.PhaseFreeDebate:
		return "[自由辩论] 我方坚持前述观点。"
	default:
		return "我方坚持己方立场。"
	}
}

// isQuarantined 判断 Bot 是否被隔离。
func (a *Agent) isQuarantined() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.quarantined
}

// Quarantine 标记隔离(由外部调用,如 LLM 连续失败)。
func (a *Agent) Quarantine() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.quarantined = true
}

// ============================================================================
// Prompt 构建
// ============================================================================

// buildSystemPrompt 构造系统提示词。
//
// 设计见 docs/辩论比赛/02 §2.7。
func (a *Agent) buildSystemPrompt() string {
	var b strings.Builder
	b.WriteString(debaterSystemBase)
	b.WriteString("\n═══════════════════════════════════════════════════════════════\n")
	b.WriteString(fmt.Sprintf("【你的身份】\n你是第 %d 队的 %s(%s),代表 %s 立场。\n",
		a.TeamID+1, debate.RoleCN(a.Role), a.Role, debate.StanceLabel(a.Stance)))
	b.WriteString(fmt.Sprintf("【辩题】\n%s\n", a.engine.Room().Config.Topic.Text))
	b.WriteString(fmt.Sprintf("【辩题背景】\n%s\n", a.engine.Room().Config.Topic.Background))
	b.WriteString("\n═══════════════════════════════════════════════════════════════\n")
	b.WriteString("【辩位职责】\n")
	b.WriteString(roleDutyText(a.Role))
	return b.String()
}

// roleDutyText 不同辩位的额外职责说明。
func roleDutyText(role debate.Role) string {
	switch role {
	case debate.RoleFirst:
		return "一辩(立论):抢占定义权、明确判准、抛出 2-3 个核心论点、搭建论证框架。\n" +
			"要求:结构清晰、论点鲜明、论据充分,字数 ≤ 500。\n"
	case debate.RoleSecond:
		return "二辩(驳论):针对对方开篇立论的逻辑漏洞、论据缺陷、定义偏差进行反驳。\n" +
			"要求:必须引用对方具体论点(对方 X 辩提到:...),指出漏洞或不足,字数 ≤ 400。\n"
	case debate.RoleThird:
		return "三辩(质询):向对方辩手发起提问,获取对方逻辑漏洞或回避点。\n" +
			"要求:问题精准、有针对性,字数 ≤ 50;被提问时正面回应,字数 ≤ 100。\n"
	case debate.RoleFourth:
		return "四辩(总结):梳理全场攻防、汇总对方漏洞、回应己方被质疑、升华辩题价值。\n" +
			"要求:不重复基础论点、针对全场态势回应、升华价值高度,字数 ≤ 600。\n"
	}
	return ""
}

// buildUserPrompt 构造 user prompt(基于 DebateContext)。
func (a *Agent) buildUserPrompt(gc DebateContext) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("【当前阶段】%s(%s)\n", debate.PhaseCN(gc.Phase), gc.Phase))
	b.WriteString(fmt.Sprintf("【辩题】%s(%s)\n", gc.Topic, gc.TopicType))
	b.WriteString(fmt.Sprintf("【你的立场】%s / 【你的辩位】%s\n", gc.MyStance, gc.MyRole))
	b.WriteString(fmt.Sprintf("【剩余时间】%d 秒\n\n", gc.TimeRemaining))

	// 我方核心论点
	if len(gc.MyArguments) > 0 {
		b.WriteString("【我方核心论点】\n")
		for i, arg := range gc.MyArguments {
			b.WriteString(fmt.Sprintf("  %d. %s\n", i+1, arg))
		}
		b.WriteString("\n")
	}

	// 对方核心论点
	if len(gc.OppArguments) > 0 {
		b.WriteString("【对方核心论点】\n")
		for i, arg := range gc.OppArguments {
			b.WriteString(fmt.Sprintf("  %d. %s\n", i+1, arg))
		}
		b.WriteString("\n")
	}

	// 最近发言
	if len(gc.RecentSpeeches) > 0 {
		b.WriteString("【最近发言】\n")
		for _, sp := range gc.RecentSpeeches {
			b.WriteString(fmt.Sprintf("  [%s/%s] %s\n",
				debate.StanceLabel(debate.Stance(sp.Stance)),
				debate.RoleCN(debate.Role(sp.Role)),
				truncateRunes(sp.Content, 80)))
		}
		b.WriteString("\n")
	}

	// 当前轮次任务
	b.WriteString(fmt.Sprintf("【本轮任务】\n%s\n", phaseTaskGuide(gc.Phase, gc.MyRole)))
	return b.String()
}

// buildTools 构造工具集(根据 phase + role 过滤)。
func (a *Agent) buildTools(phase debate.Phase) []llm.ToolDef {
	allowed := debate.AllowedToolsForPhaseRole(phase, a.Role)
	out := make([]llm.ToolDef, 0, len(allowed))
	for _, t := range allowed {
		if def, ok := debateToolDefs[t]; ok {
			out = append(out, def)
		}
	}
	return out
}

// truncateRunes 截断字符串(简化版)。
func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	count := 0
	for i := range s {
		if count == max {
			return s[:i] + "..."
		}
		count++
	}
	return s
}

// phaseTaskGuide 阶段任务指引(基于阶段+辩位)。
func phaseTaskGuide(phase debate.Phase, role string) string {
	switch phase {
	case debate.PhaseOpeningArgument:
		return "请以一辩身份发表立论:清晰定义辩题概念,明确判定标准,抛出 2-3 个核心论点并提供论据。"
	case debate.PhaseRebuttal:
		return "请以二辩身份发表驳论:针对对方一辩发言中的具体观点,指出逻辑漏洞或论据不足。"
	case debate.PhaseCrossExamination:
		if role == string(debate.RoleThird) {
			return "请以三辩身份发起质询:向对方辩手提出精准问题,挖掘其逻辑漏洞。"
		}
		return "请回答对方三辩的质询:正面回应,不得回避或反问。"
	case debate.PhaseCrossExamSummary:
		return "请做质询小结:梳理质询战果,归纳交锋焦点,固化己方优势。"
	case debate.PhaseFreeDebate:
		return "请参与自由辩论:简短有力,针对性强,论点明确。"
	case debate.PhaseClosingArgument:
		return "请以四辩身份做总结陈词:梳理全场攻防,汇总对方漏洞,升华辩题价值。"
	}
	return "请按辩位职责发言。"
}

// debaterSystemBase 辩方系统提示词常量。
const debaterSystemBase = `【辩论比赛 — 硬约束】
❶ 你是一场 AI 辩论比赛的辩方 Agent,代表一支辩论队参赛。
❷ 你只能使用工具调用影响比赛:speech(正式发言)、cross_exam_question(质询提问)、
   cross_exam_answer(质询回答)、free_debate_speak(自由辩论)、idle_silent(沉默)。
❸ 严禁编造事实:所有论据必须基于逻辑推理和常识,不可编造数据/研究/案例。
❹ 尊重辩论规则:不人身攻击、不跑题、不打断对方、按阶段规则发言。
❺ 字数限制:严格遵守各阶段字数上限,超出部分将被截断。
═══════════════════════════════════════════════════════════════
【辩论核心原则】
① 定义权:对辩题核心概念的定义是立论的基础,要抢占有利定义。
② 判准:明确衡量胜负的标准,引导辩论方向。
③ 论点-论据-论证:每个论点必须有论据支撑,论证链条完整。
⑤ 团队配合:与队友论点一致、互相补充,不自我矛盾。
═══════════════════════════════════════════════════════════════
【输出格式】
- 发言:纯文本,不使用 Markdown/JSON。
- 内部思考:通过 internal_thought 参数提交,观众可见。
- 引用对方:使用「对方 X 辩提到:...」格式。
`