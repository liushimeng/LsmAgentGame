// Package citywebgl — agent.go: LsmAgentGame-City-WebGL-Show 渲染显示 Agent
// (2026-11-03 §批次81,方案文档 §3.3)。
//
// 两条调用路径:
//
//	Structured(req) — 参数齐备(district 或 x/z)**不过 LLM**,直构 1-2 条指令;
//	Run(ctx, req)   — 自然语言 → ReAct(≤MaxSteps) → 指令序列,逐条 emit 实时下发。
//
// LLM 调用走 ChatStreamAccumulate(§197 流式续命)+ AgentClassName(§24);
// system = sysprompt.EnsureHead(角色段, AgentClassCityWebGLShow)(§14.3);
// tool_use 派发后以 tool_result 成对回灌(§14.1 user/assistant 严格交替)。
package citywebgl

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	agentroot "LsmAgentGame/agent"
	"LsmAgentGame/llm/sysprompt"
	llmtypes "LsmAgentGame/llm/types"
	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// defaultMaxSteps 是 Run 的 ReAct 步数上限缺省值(显示翻译要快)。
const defaultMaxSteps = 3

// Config 是渲染显示 Agent 的运行参数。
type Config struct {
	// ModelKey 是 LLM 模型 key(registry.Get);空 = 未配置(Run 报错)。
	ModelKey string
	// MaxSteps 单次 Run 的 ReAct 步数上限(≤0 → 缺省 3)。
	MaxSteps int
}

// LLMRegistry 是 llm.Registry 的窄接口(与 citysystem/citychrome 同款;
// 测试可注入 fake)。ws 层可直接把 citysystem.LLMRegistry 传入(方法集兼容)。
type LLMRegistry interface {
	Get(string) (llmtypes.LLMProvider, string, error)
	GetThinkingEnabled(string) (bool, int)
	ListEnabled() []llmtypes.ModelInfo
}

// Agent 是渲染显示 Agent(每房间一个,由 ws 层持有并注册进 citybus)。
type Agent struct {
	cfg      Config
	registry LLMRegistry
	// emit 每产出一条 DisplayCommand 实时下发(WS show_command 发帧路径);
	// nil = 不下发(仅收集返回值)。
	emit func(DisplayCommand)

	// lineMu 保护 linePolicy:建房链路(SetLinePolicy)写,Run 读。
	lineMu     sync.RWMutex
	linePolicy string
}

// NewAgent 构造。registry 为 nil 时 Run 直接报错(不 panic);
// emit 为 nil 时指令只收集不实时下发。
func NewAgent(cfg Config, reg LLMRegistry, emit func(DisplayCommand)) *Agent {
	if cfg.MaxSteps <= 0 {
		cfg.MaxSteps = defaultMaxSteps
	}
	return &Agent{cfg: cfg, registry: reg, emit: emit}
}

// SetLinePolicy 设置 LLM 线路策略(批次79 同构;建房链路按
// room.CitySystemLine() 透传):"" = 沿用 cfg.ModelKey,"random" =
// 每次 Run 从线路池加权随机,其它 = 固定 model key。并发安全。
func (a *Agent) SetLinePolicy(line string) {
	line = strings.TrimSpace(line)
	a.lineMu.Lock()
	a.linePolicy = line
	a.lineMu.Unlock()
	logger.L().Info("citywebgl line policy updated", zap.String("line", line))
}

// LinePolicy 返回当前线路策略(测试/诊断)。
func (a *Agent) LinePolicy() string {
	a.lineMu.RLock()
	defer a.lineMu.RUnlock()
	return a.linePolicy
}

// Structured 是结构化路径:参数齐备时**不过 LLM**,直接构造 1-2 条指令
// (视角跟随要快)。不 emit —— 返回值由调用方(citysystem.focusCamera)
// 统一带 trace_id 下发,避免双发。
//
// 组合规则:district 优先于 (x,z)(focus_camera 契约二选一);mode 合法时
// 追加一条 set_mode。全部参数无效 → 空切片。
func (a *Agent) Structured(req DisplayRequest) []DisplayCommand {
	cmds := make([]DisplayCommand, 0, 2)
	if d := strings.TrimSpace(req.District); d != "" {
		cmds = append(cmds, DisplayCommand{Type: DisplayFocusDistrict, District: d})
	} else if req.X != nil && req.Z != nil {
		cmds = append(cmds, DisplayCommand{Type: DisplayFocusPoint, X: *req.X, Z: *req.Z})
	}
	if mode := NormalizeMode(req.Mode); mode != "" {
		cmds = append(cmds, DisplayCommand{Type: DisplaySetMode, Mode: mode})
	}
	return cmds
}

// Run 是 LLM 路径:自然语言请求 → ReAct(≤MaxSteps) → 指令序列。
// 每条指令都经 emit 实时下发;返回值是本次全部指令。
//
// 返回 error 仅表示翻译未能完成(LLM 失败 / 请求为空);工具级失败已作为
// isErr tool_result 回灌模型,不在此返回(已产出的指令仍随切片返回)。
func (a *Agent) Run(ctx context.Context, req DisplayRequest) ([]DisplayCommand, error) {
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return nil, fmt.Errorf("citywebgl: display request text is empty")
	}
	if a.registry == nil {
		return nil, fmt.Errorf("citywebgl: llm registry not bound")
	}
	system := sysprompt.EnsureHead([]llmtypes.SystemBlock{
		{Type: "text", Text: roleText},
	}, string(agentroot.AgentClassCityWebGLShow))
	messages := []llmtypes.Message{
		{Role: "user", Content: []llmtypes.ContentBlock{{Type: "text", Text: userPrompt(req)}}},
	}
	trace := newTraceID()
	cmds := make([]DisplayCommand, 0, 2)
	for step := 0; step < a.cfg.MaxSteps; step++ {
		resp, err := a.callLLM(ctx, system, messages)
		if err != nil {
			return cmds, fmt.Errorf("citywebgl: step %d llm: %w", step, err)
		}
		tus := resp.ToolUses()
		if len(tus) == 0 {
			break // 模型直接文本作答 = 翻译结束(或无可翻译内容)。
		}
		results := make([]llmtypes.ContentBlock, 0, len(tus))
		for _, tu := range tus {
			cmd, resultText, isErr := dispatchTool(tu.Name, tu.Input, trace)
			if !isErr && cmd != nil {
				if a.emit != nil {
					a.emit(*cmd)
				}
				cmds = append(cmds, *cmd)
			}
			results = append(results, llmtypes.ContentBlock{
				Type:      "tool_result",
				ToolUseID: tu.ID,
				Content:   []llmtypes.ContentBlock{{Type: "text", Text: resultText}},
				IsError:   isErr,
			})
			logger.L().Info("citywebgl tool dispatch",
				zap.String("tool", tu.Name), zap.Bool("is_err", isErr))
		}
		// assistant(tool_use) + user(tool_result) 成对回灌(§14.1 交替纪律)。
		messages = append(messages,
			llmtypes.Message{Role: "assistant", Content: resp.Content},
			llmtypes.Message{Role: "user", Content: results},
		)
	}
	return cmds, nil
}

// callLLM 单步 LLM 调用:thinking 注入 + ChatStreamAccumulate(§197)。
func (a *Agent) callLLM(ctx context.Context, system []llmtypes.SystemBlock,
	messages []llmtypes.Message) (llmtypes.LLMResponse, error) {
	modelKey := a.resolveLineKey()
	provider, key, err := a.registry.Get(modelKey)
	if err != nil {
		return llmtypes.LLMResponse{}, fmt.Errorf("registry.Get(%s): %w", modelKey, err)
	}
	req := llmtypes.LLMRequest{
		Model:          modelKey,
		System:         system,
		Messages:       messages,
		Tools:          BuildTools(),
		MaxTokens:      2048,
		AgentClassName: string(agentroot.AgentClassCityWebGLShow),
	}
	if enabled, budget := a.registry.GetThinkingEnabled(modelKey); enabled {
		req.Thinking = &llmtypes.ThinkingConfig{Type: "enabled", BudgetTokens: budget}
	}
	type streamingProvider interface {
		ChatStreamAccumulate(context.Context, string, llmtypes.LLMRequest,
			func(llmtypes.StreamEvent) error) (llmtypes.LLMResponse, error)
	}
	if sp, ok := provider.(streamingProvider); ok {
		return sp.ChatStreamAccumulate(ctx, key, req, func(llmtypes.StreamEvent) error { return nil })
	}
	return provider.Chat(ctx, key, req)
}

// resolveLineKey 解析本次 Run 使用的 model key(批次79 同构):
//   - 固定策略(非空且非 random)→ 原样返回;
//   - 缺省 "" → cfg.ModelKey(零回归);
//   - random → registry.ListEnabled() 按 concurrency_lines 加权随机,
//     池空回退 cfg.ModelKey。
func (a *Agent) resolveLineKey() string {
	line := a.LinePolicy()
	if line != "" && !strings.EqualFold(line, LinePolicyRandom) {
		return line
	}
	if line == "" {
		return a.cfg.ModelKey
	}
	if a.registry != nil {
		if key := randomLineKey(a.registry.ListEnabled()); key != "" {
			return key
		}
	}
	return a.cfg.ModelKey
}

// LinePolicyRandom 是线路策略的「随机」取值(建房弹窗默认,与批次79一致)。
const LinePolicyRandom = "random"

// randomLineKey 按线路数加权随机取一个 model key(权重 = concurrency_lines,
// ≤0 视作 1;空列表返回 "")。与 citychrome/citysystem 同口径。
func randomLineKey(list []llmtypes.ModelInfo) string {
	total := 0
	for _, m := range list {
		total += lineWeight(m)
	}
	if total <= 0 {
		return ""
	}
	n := rand.Intn(total)
	for _, m := range list {
		w := lineWeight(m)
		if n < w {
			return m.Model
		}
		n -= w
	}
	return ""
}

// lineWeight 返回单模型的线路权重(concurrency_lines,≤0 → 1)。
func lineWeight(m llmtypes.ModelInfo) int {
	if m.ConcurrencyLines <= 0 {
		return 1
	}
	return m.ConcurrencyLines
}

// newTraceID 生成显示指令的 trace_id(与 citysystem.NewTraceID 同格式;
// 跨包不 import,各自生成 —— 前端按 trace_id 串联事件流与 show_command)。
func newTraceID() string {
	return fmt.Sprintf("tr-%d", time.Now().UnixNano())
}
