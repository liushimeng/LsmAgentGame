// Package debateplayer — 辩方 Agent 驱动(2026-08-31 §20260831-01)。
//
// 设计:
//   - 每个 Bot 是一个独立 goroutine,监听 DebateEngine.BotEventChan(team, seat)
//   - 收到事件后:拉取 DebateContext → 构造 prompt → 多轮 tool_use 循环
//     (§20260831-02:≤ 5 轮,失败把错误作为 tool_result 喂回 LLM 自纠错)
//     → 调 DebateRoom.SubmitSpeech/CrossExam*/SubmitFreeDebateSpeech
//   - Memory 跨轮持久(assistant/tool_result 全量入记忆),
//     超阈值(>50 条 / >300KB)触发 8 段结构化摘要压缩(§20260831-03)
//   - 每轮结束 NotifyTurnDone 通知引擎推进(§20260831-02 同步语义)
//
// 与狼人杀 Agent 区别:
//   - 单一角色驱动(立论/驳论/质询/总结),无角色切换
//   - 工具集随 phase + role 动态过滤
//   - 跨局 MEMORY.md 迭代仍未实现(本版本)
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
//
// §20260831-02 — 每轮结束(含隔离跳过)必须 NotifyTurnDone,
// 否则引擎 waitTurnDone 只能干等阶段超时。
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
				a.engine.NotifyTurnDone(a.TeamID, a.Seat)
				continue
			}
			a.runTurn()
			a.engine.NotifyTurnDone(a.TeamID, a.Seat)
		}
	}
}

// runTurn 执行一轮完整的多轮 tool_use 循环(§20260831-02)。
//
// 流程(对齐 docs/辩论比赛/02 §2.3 + §5.1):
//
//	user prompt 入 memory → 循环(≤ maxToolUseRounds):
//	  Chat(memory 快照) → 无 tool_use:
//	    · 本轮已派发过终态工具 → 结束(把文本当补充发言忽略)
//	    · 未派发 → 纯文本按 speech 提交(视为口语化发言),结束
//	  有 tool_use → 逐个派发 → tool_result(含错误信息)回填 memory:
//	    · 任一工具 OK(成功落库) → 终态,结束
//	    · 全部失败 → 把错误喂回 LLM 重试(下一轮循环)
//	LLM 调用失败 / 超过轮次上限 → fallback 模板发言。
func (a *Agent) runTurn() {
	if a.provider == nil {
		logger.L().Warn("debate agent: llm provider is nil, skipping turn",
			zap.String("room_id", a.RoomID),
			zap.Int("team", a.TeamID),
			zap.Int("seat", a.Seat))
		a.useFallbackResponse()
		return
	}

	systemPrompt := a.buildSystemPrompt()
	gc := a.collectContext()
	userPrompt := a.buildUserPrompt(gc)
	tools := a.buildTools(gc.Phase)

	if !a.engine.Manager().AcquireLLM(a.ctx) {
		logger.L().Warn("debate agent: failed to acquire LLM slot",
			zap.String("room_id", a.RoomID))
		a.useFallbackResponse()
		return
	}
	defer a.engine.Manager().ReleaseLLM()

	// §20260831-03 — 记忆压缩触发点(§05 §5.1:消息数 > 50 或字节 > 300KB)。
	// 压缩失败不阻塞发言:保留原记忆继续本轮。
	if a.memory.ShouldCompact() {
		a.compactMemory()
	}

	// 本轮 user prompt 追加进记忆(跨轮持久;失败重试轮不重复追加)
	a.memory.Append(llm.Message{
		Role:    "user",
		Content: []llm.ContentBlock{{Type: "text", Text: userPrompt}},
	})

	anyDispatchedOK := false
	for round := 0; round < maxToolUseRounds; round++ {
		messages := sanitizeDebateMessages(a.memory.Snapshot())

		llmCtx, cancel := context.WithTimeout(a.ctx, llmTurnTimeoutSec*time.Second)
		req := llm.LLMRequest{
			AgentClassName: string(a.ClassName()),
			Model:          a.ModelKey,
			System:         []llm.SystemBlock{{Type: "text", Text: systemPrompt}},
			Messages:       messages,
			Tools:          tools,
			MaxTokens:      1024,
		}
		resp, err := a.provider.Chat(llmCtx, a.apiKey, req)
		cancel()
		if err != nil {
			logger.L().Warn("debate agent: LLM call failed, using fallback",
				zap.String("room_id", a.RoomID),
				zap.String("model", a.ModelKey),
				zap.Int("round", round),
				zap.Error(err))
			if !anyDispatchedOK {
				a.useFallbackResponse()
			}
			return
		}

		// assistant 回复原样入记忆(text + tool_use 块保持配对来源)
		a.memory.Append(llm.Message{Role: "assistant", Content: cloneBlocks(resp.Content)})

		var toolUses []llm.ContentBlock
		for _, blk := range resp.Content {
			if blk.Type == "tool_use" {
				toolUses = append(toolUses, blk)
			}
		}

		if len(toolUses) == 0 {
			// 纯文本回复:本轮尚未成功派发 → 把文本当发言提交;否则仅作补充说明忽略
			if !anyDispatchedOK {
				if text := joinTextBlocks(resp.Content); strings.TrimSpace(text) != "" {
					a.submitPlainTextAsSpeech(text)
				} else {
					a.useFallbackResponse()
				}
			}
			return
		}

		// 逐个派发 tool_use,并构造 tool_result 回填
		results := make([]llm.ContentBlock, 0, len(toolUses))
		for _, blk := range toolUses {
			res := a.dispatchTool(blk)
			if res.OK {
				anyDispatchedOK = true
			}
			results = append(results, llm.ContentBlock{
				Type:      "tool_result",
				ToolUseID: blk.ID,
				Content:   []llm.ContentBlock{{Type: "text", Text: resultText(res)}},
				IsError:   !res.OK,
			})
		}
		a.memory.Append(llm.Message{Role: "user", Content: results})

		if anyDispatchedOK {
			// 辩论工具全部是一次性动作(speech/质询/沉默/交还发言权),
			// 任一成功即本轮终态 —— 防止 LLM 连发两次 speech 造成重复发言。
			return
		}
		// 全部派发失败 → 错误已作为 tool_result 喂回,进入下一轮重试
		logger.L().Warn("debate agent: all tool_use failed, retrying",
			zap.String("room_id", a.RoomID),
			zap.Int("team", a.TeamID),
			zap.Int("seat", a.Seat),
			zap.Int("round", round))
	}

	// 超过轮次上限仍未成功 → fallback
	if !anyDispatchedOK {
		a.useFallbackResponse()
	}
}

// maxToolUseRounds 单轮最多 tool_use 重试轮次(§02 §5.1:单轮最多 5 次 tool_use)。
const maxToolUseRounds = 5

// llmTurnTimeoutSec 单次 LLM 调用超时(秒)。
const llmTurnTimeoutSec = 90

// dispatchTool 派发 LLM 返回的 tool_use,返回 ActionResult(含失败原因,
// 供 tool_result 回填让 LLM 自纠错)。
func (a *Agent) dispatchTool(blk llm.ContentBlock) debate.ActionResult {
	r := a.engine.Room()
	if r == nil {
		return debate.ActionResult{OK: false, Message: "room not available"}
	}

	// tool_use.input 是 map[string]any(由 provider 反序列化);二次解析时
	// 用 json.Marshal/Unmarshal 还原成目标结构体。
	inputBytes, err := json.Marshal(blk.Input)
	if err != nil {
		logger.L().Warn("debate agent: marshal tool input failed",
			zap.Error(err))
		return debate.ActionResult{OK: false, Message: "invalid tool input"}
	}

	switch blk.Name {
	case string(debate.ToolSpeech):
		var input struct {
			Content         string   `json:"content"`
			References      []string `json:"references,omitempty"`
			InternalThought string   `json:"internal_thought,omitempty"`
		}
		if err := json.Unmarshal(inputBytes, &input); err != nil {
			return debate.ActionResult{OK: false, Message: "invalid speech input json"}
		}
		return r.SubmitSpeech(a.TeamID, a.Seat, debate.SpeechParams{
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
			return debate.ActionResult{OK: false, Message: "invalid cross_exam_question input json"}
		}
		return r.SubmitCrossExamQuestion(a.TeamID, a.Seat, debate.CrossExamQuestionParams{
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
			return debate.ActionResult{OK: false, Message: "invalid cross_exam_answer input json"}
		}
		return r.SubmitCrossExamAnswer(a.Seat, debate.CrossExamAnswerParams{
			QuestionID: input.QuestionID,
			Answer:     input.Answer,
		})
	case string(debate.ToolFreeDebateSpeak):
		var input struct {
			Content string `json:"content"`
		}
		if err := json.Unmarshal(inputBytes, &input); err != nil {
			return debate.ActionResult{OK: false, Message: "invalid free_debate_speak input json"}
		}
		return r.SubmitFreeDebateSpeech(a.TeamID, a.Seat, debate.FreeDebateParams{
			Content: input.Content,
		})
	case string(debate.ToolFinishSpeak):
		return r.FinishSpeak(a.TeamID, "finished")
	case string(debate.ToolIdleSilent):
		return r.IdleSilent(a.Seat, "idle")
	default:
		return debate.ActionResult{OK: false, Message: "unknown tool: " + blk.Name}
	}
}

// resultText 把 ActionResult 转成回填给 LLM 的 tool_result 文本。
func resultText(res debate.ActionResult) string {
	if res.OK {
		if res.SpeechID != "" {
			return "ok: " + res.Message + " (id=" + res.SpeechID + ")"
		}
		return "ok: " + res.Message
	}
	return "error: " + res.Message
}

// joinTextBlocks 拼接 Content 中的全部 text 块。
func joinTextBlocks(blocks []llm.ContentBlock) string {
	var b strings.Builder
	for _, blk := range blocks {
		if blk.Type == "text" {
			b.WriteString(blk.Text)
		}
	}
	return b.String()
}

// cloneBlocks 深拷贝 content 块(避免与 provider 内部缓冲共享底层数组)。
func cloneBlocks(blocks []llm.ContentBlock) []llm.ContentBlock {
	out := make([]llm.ContentBlock, len(blocks))
	copy(out, blocks)
	return out
}

// submitPlainTextAsSpeech 把 LLM 的纯文本回复(未走工具)按 speech 提交。
//
// 适配阶段:
//   - 正式发言阶段(立论/驳论/小结/总结)→ 按 speech 提交
//   - cross_examination 阶段(三辩纯文本兜底):
//     §20260831-07 — R6 报告 §3.2 实测部分跨厂商模型在 cross_examination
//     阶段不会调 tool_use,只回纯文本;旧实现会把文本按 speech 提交,
//     阶段校验拒收 → 该轮彻底 0 内容,cross_exam count 永远 0。
//     新行为:若我是被点名的三辩且有 pending question → 按 cross_exam_answer
//     提交;否则按 cross_exam_question 兜底(以对方任一三辩为 target)。
//   - 其它阶段(free_debate 等)→ SubmitSpeech 阶段校验拒收,退回 fallback。
func (a *Agent) submitPlainTextAsSpeech(text string) {
	room := a.engine.Room()
	if room == nil {
		a.useFallbackResponse()
		return
	}
	phase := room.Phase()

	if phase == debate.PhaseCrossExamination && a.Role == debate.RoleThird {
		// §20260831-07 — 质询阶段三辩纯文本兜底:拆分为 question 或 answer。
		if gc := a.collectContext(); gc.PendingQuestionText != "" && gc.PendingQuestionID != "" {
			// 被质询方:按 answer 提交
			res := a.dispatchTool(llm.ContentBlock{
				Type: "tool_use",
				ID:   "plain_ans_" + fmt.Sprint(debate.WallNowMS()),
				Name: string(debate.ToolCrossExamAnswer),
				Input: map[string]any{
					"question_id": gc.PendingQuestionID,
					"answer":      truncateRunes(text, 100),
				},
			})
			if !res.OK {
				a.useFallbackResponse()
			}
			return
		}
		// 提问方:把纯文本当作问题,对方任一三辩为 target。
		targetTeam, targetSeat := a.pickCrossExamTarget(room)
		if targetTeam < 0 {
			a.useFallbackResponse()
			return
		}
		res := a.dispatchTool(llm.ContentBlock{
			Type: "tool_use",
			ID:   "plain_q_" + fmt.Sprint(debate.WallNowMS()),
			Name: string(debate.ToolCrossExamQuestion),
			Input: map[string]any{
				"target_team": targetTeam,
				"target_seat": targetSeat,
				"question":    truncateRunes(text, 50),
			},
		})
		if !res.OK {
			a.useFallbackResponse()
		}
		return
	}

	res := a.dispatchTool(llm.ContentBlock{
		Type:  "tool_use",
		ID:    "plain_" + fmt.Sprint(debate.WallNowMS()),
		Name:  string(debate.ToolSpeech),
		Input: map[string]any{"content": text},
	})
	if !res.OK {
		a.useFallbackResponse()
	}
}

// pickCrossExamTarget 选择质询目标:对方任一三辩 seat;无三辩则取对方任一辩手。
//
// §20260831-07 — R6 cross_examination 阶段兜底用。
func (a *Agent) pickCrossExamTarget(room *debate.DebateRoom) (int, int) {
	for _, t := range room.Config.Teams {
		if t.TeamID == a.TeamID {
			continue
		}
		for _, ag := range t.Agents {
			if ag.Role == debate.RoleThird {
				return t.TeamID, ag.SeatID
			}
		}
		if len(t.Agents) > 0 {
			return t.TeamID, t.Agents[0].SeatID
		}
	}
	return -1, -1
}

// useFallbackResponse LLM 失败 / 返回纯文本时使用 fallback 发言。
//
// 简化策略:输出一段固定模板,直接通过 SubmitSpeech 提交;
// 同时把 fallback 文本以 assistant 消息入记忆,保持 user/assistant 交替
// (否则下一轮 user prompt 会紧跟上一轮 user prompt,违反 §14.1)。
func (a *Agent) useFallbackResponse() {
	r := a.engine.Room()
	if r == nil {
		return
	}
	phase := r.Phase()
	content := buildFallbackText(phase, a.Stance, a.Role)
	a.memory.Append(llm.Message{
		Role:    "assistant",
		Content: []llm.ContentBlock{{Type: "text", Text: content}},
	})
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

	// 待答质询(§20260831-02:被质询方必须知道问题才能回答)
	if gc.PendingQuestionText != "" {
		b.WriteString(fmt.Sprintf("【⚠ 待答质询 — 必须正面回答】\n问题(id=%s):%s\n请调用 cross_exam_answer(question_id=\"%s\", answer=...),≤ 100 字,不得回避或反问。\n\n",
			gc.PendingQuestionID, gc.PendingQuestionText, gc.PendingQuestionID))
	}

	// 当前轮次任务
	b.WriteString(fmt.Sprintf("【本轮任务】\n%s\n", phaseTaskGuide(gc.Phase, gc.MyRole, gc.PendingQuestionText != "", gc.PendingQuestionID)))
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

// phaseTaskGuide 阶段任务指引(基于阶段+辩位;hasPendingQuestion 区分质询提问/回答)。
//
// §20260831-07 — cross_examination 阶段三辩补强:
// R6 报告 §3.2 实测 DeepSeek-model(正方三辩)+ Qwen-model(反方三辩)
// 在 cross_examination 阶段连续 5 轮 all tool_use failed,cross_exam count = 0。
// 根因是旧 prompt 只写「调用 cross_exam_question」,没给完整 input 形状,
// 部分跨厂商模型把 tool 名字错认 / 缺字段直接放弃。
// 修复:补 tool_use input 完整示例 + 字段含义 + 「仅且必须 X 工具」强约束。
func phaseTaskGuide(phase debate.Phase, role string, hasPendingQuestion bool, questionID string) string {
	switch phase {
	case debate.PhaseOpeningArgument:
		return "请以一辩身份发表立论:清晰定义辩题概念,明确判定标准,抛出 2-3 个核心论点并提供论据。"
	case debate.PhaseRebuttal:
		return "请以二辩身份发表驳论:针对对方一辩发言中的具体观点,指出逻辑漏洞或论据不足。"
	case debate.PhaseCrossExamination:
		if hasPendingQuestion {
			// question_id 已在上方「待答质询」段给出;这里再次复述防止跨模型截断 user prompt 头/尾。
			qid := questionID
			if qid == "" {
				qid = "<上方待答质询的 id>"
			}
			return "对方三辩已向你发起质询,请**仅且必须**调用 cross_exam_answer 工具(不要用 cross_exam_question、不要用 idle_silent、不要用 speech)。\n" +
				"调用示例(字段值必须填实际值,question_id 直接复制上方的 id):\n" +
				"  cross_exam_answer(question_id=\"" + qid + "\", answer=\"<≤100 字的正面回应>\")\n" +
				"answer ≤ 100 字,正面回应问题,不得回避或反问。若模型无法调用工具,请回一段纯文本回答(系统会自动转写为答案)。"
		}
		if role == string(debate.RoleThird) {
			return "请以三辩身份发起质询:**仅且必须**调用 cross_exam_question 工具(不要用其他任何工具,不要用 idle_silent 跳过)。\n" +
				"调用示例(字段值根据对方队伍号实际替换):\n" +
				"  cross_exam_question(target_team=1, target_seat=0, question=\"<≤50 字的精准问题>\")\n" +
				"字段含义:\n" +
				"  - target_team:对方队伍号(0 表示队0/正方,1 表示队1/反方)\n" +
				"  - target_seat:被质询辩位 seat(0-3,-1 表任意对手)\n" +
				"  - question:问题正文,≤ 50 字\n" +
				"问题要精准、有针对性,挖掘对方逻辑漏洞。若模型无法调用工具,回一段纯文本(系统会按质询提交)。"
		}
		return "当前无质询任务,若无待答问题可选择 idle_silent。"
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
④ 反驳三要素:指出对方错误 + 说明为什么错 + 给出正确观点。
⑤ 团队配合:与队友论点一致、互相补充,不自我矛盾。
═══════════════════════════════════════════════════════════════
【输出格式】
- 发言:纯文本,不使用 Markdown/JSON。
- 内部思考:通过 internal_thought 参数提交,观众可见。
- 引用对方:使用「对方 X 辩提到:...」格式。
`