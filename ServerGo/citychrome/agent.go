// Package citychrome — agent.go: LsmAgentGame-City-Chrome Agent 循环(契约 §3.4)。
//
// Mission{Category,TargetURL,Sites,Goal} → system = sysprompt.EnsureHead(角色段,
// AgentClassCityChrome)(三段式自动注入,§14.3) → user = mission JSON + 输出
// schema + 站点备选清单 → for step < max_steps:LLM 规划 → 工具派发(mcp_tools)
// → tool_result 回灌 → web_done{payload} 校验通过即 break。
//
// 无 web_done:最后一条 assistant 文本按 JSON 提取兜底;仍失败 ⇒ mission 失败
// 计熔断(调度器侧)。LLM 调用走 ChatStreamAccumulate(§197 流式续命)+
// AgentClassName(§24),节流经注入的 acquireLLM(复用项目 llmSema 模式)。
package citychrome

import (
	"context"
	"encoding/json"
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

// LLMRegistry 是 llm.Registry 的窄接口(与 vcplayer 同款;测试可注入 fake)。
type LLMRegistry interface {
	Get(modelKey string) (llmtypes.LLMProvider, string, error)
	GetThinkingEnabled(modelKey string) (bool, int)
	// ListEnabled 返回可用模型目录(批次79:随机线路模式的候选集,
	// 权重 = concurrency_lines)。
	ListEnabled() []llmtypes.ModelInfo
}

// LinePolicyRandom 是线路策略的「随机」取值(建房弹窗默认):
// 每个 mission 从 LLM 线路池随机取一条线路。
const LinePolicyRandom = "random"

// roleText 是 City-Chrome Agent 角色段(契约 §3.4「角色段要点」逐字落地;
// 批次77 §3.3-1 追加「规划与效率」段,批次78 §3.3-4 追加「确定性解析纪律」段;
// 既有纪律段与输出 schema 段逐字保留)。
const roleText = `你是虚拟城市的数据采集员。你只用工具操作浏览器(绝不臆造数据),按 mission 从真实金融站点采集行情与新闻。

【规划与效率(批次77)】
1. mission 第 1 步必须调 web_plan 提交 ≤5 步规划(含目标站与换源预案),再开始执行。
2. 简单读取优先 web_read(一步=打开+读正文);需要精细 DOM 提取才用 web_open+web_eval。
3. 备选站点全部失败时,可用 web_search 检索该类目数据页(如「上证指数 实时行情」),从结果里选公开可读页用 web_read 提取,仍禁止登录/验证码/表单提交。

【纪律】
1. 优先 web_eval(BatchEval 单次多条提取)减少往返;只有交互必需时才用 web_control。
2. 每条数据必须来自页面真实 DOM 文本(eval_js 从页面提取),注明 source(站点名)。
3. 遇拦截/验证码/超时 → 换备选站点(Sites 列表),最多换 3 次;仍失败则终止。
4. 禁止登录、禁止提交表单、禁止绕过验证码,只读公开页面。
5. 同站点访问保持克制;不点广告、不触发下载(数据型下载=eval_js 内 fetch)。
6. 完成后必须调 web_done 提交结构化 payload(唯一使命出口)。
7. payload.source 只填站点名(如「财联社·电报」),不要附带链接说明或时间口径解释;口径差异写在 quotes/news 各自字段里。

【收敛流程(重要,防步数耗尽)】
推荐节奏:web_open 目标站 → 1 次 web_eval 读 document.title + body 摘要定位数据区 →
1-2 次 web_eval 批量提取数值 → 数据够 6 条以上立即 web_done。
同一页面连续 eval 试探不超过 2 次;提取不到就果断换备选站点,不要在单页反复试错。
quotes 宁缺毋滥:拿到核心指数(≥5 条)即可提交,不必凑满 12 条。

【输出 schema(web_done.payload)】
{"category":"<mission.category>","source":"<站点名>",
 "quotes":[{"symbol":"站点口径代码或名称","name":"显示名","price":<数字>,"change_pct":<-100~100>,"currency":"CNY|HKD|USD|..."}],
 "news":[{"title":"标题","url":"https://...","published_at":<Unix秒>}]}
quotes 至多 12 条(price>0、change_pct 百分比),news 至多 5 条;单条脏数据会被服务端丢弃,不影响其它数据。

【确定性解析纪律(批次78)】
1. 行情类目优先用 web_read 的 parse(缺省 auto)拿结构化报价:**不要自己按 ~ 或 , 手工切分数字**;结构化块里的 price/change_pct 已是服务端口径(涨跌幅自算),原样抄进 web_done 即可。
2. 只有当 parse 返回 matched=false(未命中行情特征)时,才改用 web_eval 手工提取;不要在已命中时重复手工解析。
3. 遇到 SPA 页面正文为空,不要盲目重试同一 URL:用 web_read 的 wait_selector(≤15s)等目标元素出现再读一次;仍为空就换备选站点。
4. web_search 返回「该源被拦截」时直接换备选站点,不要重试同一搜索引擎。`

// AgentConfig 是 City-Chrome Agent 的运行参数(从 config.CityChromeConfig 收窄)。
type AgentConfig struct {
	ModelKey           string        // LLM 模型 key(registry.Get)
	MaxStepsPerMission int           // 单 mission 步数上限(契约默认 14)
	SymbolsPerCategory int           // web_done quotes 上限校验
	NewsLimit          int           // FeedStore 新闻上限(透传)
	MissionTimeout     time.Duration // 单 mission 总超时
}

// Agent 是 City-Chrome 采集 Agent(全局单例,由 Service 持有)。
type Agent struct {
	cfg      AgentConfig
	client   *WebUseClient
	registry LLMRegistry
	// acquireLLM 是 LLM 并发节流槽(复用项目 llmSema 模式);nil = 不节流。
	acquireLLM func(ctx context.Context) (release func(), ok bool)
	// mlog 是采集任务遥测日志(批次77 §3.3-3);nil = 不记录遥测。
	mlog *MissionLog
	// lineMu 保护 linePolicy:建房链路(Service.SetLinePolicy)写入,
	// RunMission 读取;读多写少用 RWMutex。
	lineMu sync.RWMutex
	// linePolicy 是 LLM 线路策略(批次79):"" = 沿用 cfg.ModelKey(旧行为);
	// LinePolicyRandom = 每个 mission 从线路池随机取线;其它 = 固定 model key。
	linePolicy string
}

// NewAgent 构造。registry 为 nil 时 mission 直接失败(由调度器计熔断)。
func NewAgent(cfg AgentConfig, client *WebUseClient, registry LLMRegistry) *Agent {
	if cfg.MaxStepsPerMission <= 0 {
		cfg.MaxStepsPerMission = 14
	}
	if cfg.SymbolsPerCategory <= 0 {
		cfg.SymbolsPerCategory = 12
	}
	if cfg.MissionTimeout <= 0 {
		cfg.MissionTimeout = 10 * time.Minute
	}
	return &Agent{cfg: cfg, client: client, registry: registry}
}

// SetLLMAcquire 注入 LLM 节流槽(调度器/Service 装配)。
func (a *Agent) SetLLMAcquire(fn func(ctx context.Context) (release func(), ok bool)) {
	a.acquireLLM = fn
}

// SetMissionLog 注入采集任务遥测日志(Service 装配;nil = 不记录遥测)。
func (a *Agent) SetMissionLog(l *MissionLog) { a.mlog = l }

// SetLinePolicy 设置 LLM 线路策略(批次79;建房链路 Service.SetLinePolicy
// 透传):"" = 沿用 cfg.ModelKey(旧行为),LinePolicyRandom = 每个 mission
// 从线路池随机取线,其它 = 固定 model key。并发安全(建房线程写 / 调度线程读)。
func (a *Agent) SetLinePolicy(line string) {
	line = strings.TrimSpace(line)
	a.lineMu.Lock()
	a.linePolicy = line
	a.lineMu.Unlock()
	logger.L().Info("citychrome line policy updated", zap.String("line", line))
}

// LinePolicy 返回当前线路策略(测试/诊断)。
func (a *Agent) LinePolicy() string {
	a.lineMu.RLock()
	defer a.lineMu.RUnlock()
	return a.linePolicy
}

// resolveLineKey 解析本 mission 使用的 model key(批次79):
//   - 固定策略(非空且非 random)→ 原样返回(Get 失败走既有 mission 失败兜底);
//   - 缺省 "" → cfg.ModelKey(旧行为,零回归);
//   - random → 从 registry.ListEnabled() 按 concurrency_lines 加权随机;
//     池空时回退 cfg.ModelKey(不因线路池空缺而整条通道失败)。
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

// randomLineKey 按线路数加权随机取一个 model key(权重 = concurrency_lines,
// ≤0 视作 1;空列表返回 ""）。与 LinePool 的线路数量权重口径一致。
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

// missionUserPrompt 构造 mission 的 user 文本(mission JSON + 输出 schema +
// 站点备选 + 跨 mission 经验注入)。经验段按 MissionLog 该类目最近成功/失败
// 摘要追加(批次77 §3.3-2;两者皆无则不加段)。
func missionUserPrompt(m Mission, mlog *MissionLog) string {
	sites := make([]string, 0, len(m.Sites))
	for _, s := range m.Sites {
		sites = append(sites, fmt.Sprintf("%s(%s)", s.Name, s.URL))
	}
	var exp strings.Builder
	if mlog != nil {
		lastOK, lastFail := mlog.LastOK(m.Category), mlog.LastFail(m.Category)
		if lastOK != nil || lastFail != nil {
			exp.WriteString("\n\n【上次采集记录(自动注入,据此优化本次规划)】\n")
			if lastOK != nil {
				exp.WriteString(fmt.Sprintf("最近成功: source=%s, 步数=%d, 提交 quotes=%d news=%d\n",
					lastOK.Source, lastOK.StepsUsed, lastOK.Quotes, lastOK.News))
			}
			if lastFail != nil {
				exp.WriteString(fmt.Sprintf("最近失败原因: %s\n", lastFail.Error))
			}
		}
	}
	return fmt.Sprintf(`【mission】
category: %s
目标站点: %s
备选站点(按序换源,最多 3 次): %s
采集目标: %s

%s

请按上面的提取配方执行:首选源通常是「navigate → 读 body → 解析」三步即可拿到数据,
拿到后**立即 web_done 提交**;只有首选源失败时才换备选站点(备选站为常规网页,用
web_eval 从 DOM 提取)。web_done 的 category 必须 = %s。%s`,
		m.Category, m.TargetURL, strings.Join(sites, " / "), m.Goal, m.Hints, m.Category, exp.String())
}

// RunMission 执行一次采集 mission。成功返回校验后的 payload;
// web_done 缺失/非法且兜底 JSON 提取失败 ⇒ 返回错误(调度器计熔断)。
//
// 批次77 §3.3:入口 Append(running) 遥测、出口同 ID 覆盖为 ok/fail;
// defer best-effort 回收 Toolset.OpenedPages 全部页面(资源卫生)。
func (a *Agent) RunMission(ctx context.Context, m Mission) (*MissionPayload, error) {
	if a.registry == nil {
		return nil, fmt.Errorf("llm registry not bound")
	}
	mctx, cancel := context.WithTimeout(ctx, a.cfg.MissionTimeout)
	defer cancel()

	// 批次79:一次 mission 内固定使用同一条线路(随机策略在入口抽一次,
	// 避免同一任务多步跨模型漂移);线路名进遥测记录便于诊断。
	modelKey := a.resolveLineKey()

	// ── 遥测:running 入环,出口 finish 同 ID 覆盖(批次77 §3.3-3)──
	start := time.Now()
	rec := MissionRecord{
		ID:        fmt.Sprintf("%s-%d", m.Category, start.UnixMilli()),
		Category:  m.Category,
		StartedAt: start.Unix(),
		Status:    MissionStatusRunning,
		MaxSteps:  a.cfg.MaxStepsPerMission,
		Model:     modelKey,
	}
	if a.mlog != nil {
		a.mlog.Append(rec)
	}

	toolset := NewToolset(a.client, a.cfg.SymbolsPerCategory, a.cfg.NewsLimit)
	// 资源卫生:mission 结束 best-effort 回收打开的全部页面(批次77 §3.3-4;
	// CloseChromePage 对 2000 幂等,重复关无害;此 defer 先于 cancel 执行)。
	defer func() {
		if a.client == nil {
			return
		}
		for _, pageID := range toolset.OpenedPages {
			_ = a.client.CloseChromePage(mctx, pageID)
		}
	}()
	// finish 把 running 记录原地翻成终态(Error/Plan 截断在 MissionLog.Append 内)。
	finish := func(status string, stepsUsed int, payload *MissionPayload, errText string) {
		if a.mlog == nil {
			return
		}
		rec.Status = status
		rec.StepsUsed = stepsUsed
		rec.Plan = toolset.Plan
		rec.DurationMs = time.Since(start).Milliseconds()
		// 站点尝试轨迹(批次78 §3.3-5):web_open/web_read 访问过的 URL。
		rec.Visited = toolset.VisitedURLs
		if payload != nil {
			rec.Source = payload.Source
			rec.Quotes = len(payload.Quotes)
			rec.News = len(payload.News)
		}
		rec.Error = errText
		a.mlog.Append(rec) // 同 ID 覆盖更新。
	}

	system := sysprompt.EnsureHead([]llmtypes.SystemBlock{
		{Type: "text", Text: roleText},
	}, string(agentroot.AgentClassCityChrome))

	messages := []llmtypes.Message{
		{Role: "user", Content: []llmtypes.ContentBlock{{Type: "text", Text: missionUserPrompt(m, a.mlog)}}},
	}

	lastText := ""
	stepsUsed := 0
	for step := 0; step < a.cfg.MaxStepsPerMission; step++ {
		resp, err := a.callLLM(mctx, modelKey, system, messages)
		stepsUsed = step + 1
		if err != nil {
			finish(MissionStatusFail, stepsUsed, nil, err.Error())
			return nil, fmt.Errorf("mission step %d llm: %w", step, err)
		}
		lastText = resp.Text()
		tus := resp.ToolUses()
		if len(tus) == 0 {
			// 模型直接文本作答:尝试按 JSON 兜底提取。
			break
		}
		results := make([]llmtypes.ContentBlock, 0, len(tus))
		for _, tu := range tus {
			text, isErr := toolset.Dispatch(mctx, tu.Name, tu.Input)
			// 步数预算回灌:让模型感知剩余步数,促其收敛 web_done(防步数耗尽)。
			text = fmt.Sprintf("[step %d/%d] %s", step+1, a.cfg.MaxStepsPerMission, text)
			results = append(results, llmtypes.ContentBlock{
				Type:      "tool_result",
				ToolUseID: tu.ID,
				Content:   []llmtypes.ContentBlock{{Type: "text", Text: text}},
				IsError:   isErr,
			})
			logger.L().Info("citychrome tool dispatch",
				zap.String("mission", m.Category),
				zap.Int("step", step+1),
				zap.String("tool", tu.Name),
				zap.Bool("is_err", isErr),
				zap.String("result_preview", truncate(text, 200)))
		}
		if toolset.Done != nil {
			finish(MissionStatusOK, stepsUsed, toolset.Done, "")
			return toolset.Done, nil
		}
		// assistant(tool_use)+ user(tool_result) 成对回灌(§14.1 交替纪律)。
		messages = append(messages,
			llmtypes.Message{Role: "assistant", Content: resp.Content},
			llmtypes.Message{Role: "user", Content: results},
		)
	}
	// 兜底:最后一条 assistant 文本按 JSON 提取(契约 §3.4)。
	if payload, err := payloadFromText(a.cfg.SymbolsPerCategory, lastText); err == nil {
		finish(MissionStatusOK, stepsUsed, payload, "")
		return payload, nil
	}
	logger.L().Warn("citychrome mission exhausted",
		zap.String("mission", m.Category),
		zap.Int("steps", a.cfg.MaxStepsPerMission),
		zap.String("last_text_preview", truncate(lastText, 500)))
	finish(MissionStatusFail, stepsUsed, nil, fmt.Sprintf("mission %s 步数用尽且无合法 web_done", m.Category))
	return nil, fmt.Errorf("mission %s 步数用尽且无合法 web_done", m.Category)
}

// payloadFromText 从文本提取 JSON 对象并校验(找首个 '{' 与末个 '}' 的子串)。
func payloadFromText(symbolsPerCategory int, text string) (*MissionPayload, error) {
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("文本中无 JSON 对象")
	}
	var probe map[string]any
	if err := json.Unmarshal([]byte(text[start:end+1]), &probe); err != nil {
		return nil, err
	}
	return ValidatePayload(symbolsPerCategory, probe)
}

// callLLM 单步 LLM 调用:节流槽 + thinking 注入 + ChatStreamAccumulate(§197)。
// modelKey 由 RunMission 入口按线路策略解析(批次79),一次 mission 内恒定。
func (a *Agent) callLLM(ctx context.Context, modelKey string, system []llmtypes.SystemBlock, messages []llmtypes.Message) (llmtypes.LLMResponse, error) {
	if a.acquireLLM != nil {
		release, ok := a.acquireLLM(ctx)
		if !ok {
			return llmtypes.LLMResponse{}, fmt.Errorf("llm acquire timeout")
		}
		defer release()
	}
	provider, key, err := a.registry.Get(modelKey)
	if err != nil {
		return llmtypes.LLMResponse{}, fmt.Errorf("registry.Get(%s): %w", modelKey, err)
	}
	req := llmtypes.LLMRequest{
		Model:          modelKey,
		System:         system,
		Messages:       messages,
		Tools:          BuildTools(),
		MaxTokens:      4096,
		AgentClassName: string(agentroot.AgentClassCityChrome),
	}
	if enabled, budget := a.registry.GetThinkingEnabled(modelKey); enabled {
		req.Thinking = &llmtypes.ThinkingConfig{Type: "enabled", BudgetTokens: budget}
	}
	type streamingProvider interface {
		ChatStreamAccumulate(context.Context, string, llmtypes.LLMRequest, func(llmtypes.StreamEvent) error) (llmtypes.LLMResponse, error)
	}
	if sp, ok := provider.(streamingProvider); ok {
		return sp.ChatStreamAccumulate(ctx, key, req, func(llmtypes.StreamEvent) error { return nil })
	}
	return provider.Chat(ctx, key, req)
}
