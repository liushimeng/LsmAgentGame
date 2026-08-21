// Package anthropic implements llm.LLMProvider against the Anthropic Messages
// API wire format. All outbound requests carry:
//
//	Authorization: Bearer <key>
//	anthropic-version: 2023-06-01
//	User-Agent: <program/version build-time>          (SetUserAgent)
//	x-anthropic-billing-header: <opaque tag>          (SetBillingHeader, optional)
//
// The provider never sees the key at construction time — it is passed in per
// Chat(ctx, key, req) call by the registry, so a single shared Provider
// instance serves every configured model over the same endpoint.
package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"LsmAgentGame/logger"
	types "LsmAgentGame/llm/types"

	"go.uber.org/zap"
)

// AnthropicVersion is the protocol version header the proxy expects.
const AnthropicVersion = "2023-06-01"

// anthropicRequest is the wire body the proxy understands. It is isomorphic to
// types.LLMRequest but kept separate so we can add Anthropic-only fields (e.g.
// anthropic-beta) later without polluting the generic type.
//
// Wire additions beyond the generic types.LLMRequest:
//   - Stream      → emits `"stream":true` when req.Stream is set
//   - ToolChoice  → forwarded 1:1
//   - OutputConfig → forwarded 1:1 (Anthropic `output_config` object)
type anthropicRequest struct {
	Model        string                `json:"model"`
	System       []types.SystemBlock   `json:"system,omitempty"`
	Messages     []types.Message       `json:"messages"`
	Tools        []types.ToolDef       `json:"tools,omitempty"`
	MaxTokens    int                 `json:"max_tokens"`
	Metadata     types.Metadata      `json:"metadata,omitempty"`
	OutputConfig *types.OutputConfig `json:"output_config,omitempty"`
	ToolChoice   *types.ToolChoice   `json:"tool_choice,omitempty"`
	Stream       bool                `json:"stream,omitempty"`
	Temperature  *float64            `json:"temperature,omitempty"`
	// BUG-R226-P1-02 (2026-08-01) — 顶层 Thinking 字段,wire 形状为
	// {"type":"enabled","budget_tokens":N} / {"type":"disabled"}
	// (§14.1 权威用例形状,type 判别符由 ThinkingConfig.MarshalJSON 保证);
	// nil (omitted) 表示不使用 extended thinking。消息级 thinking 内容块
	// 已移除 —— 权威用例的 content[] 只含 text/tool_use/tool_result。
	Thinking     *types.ThinkingConfig `json:"thinking,omitempty"`
}

// anthropicResponse mirrors the Anthropic Messages response body.
type anthropicResponse struct {
	ID      string             `json:"id"`
	Type    string             `json:"type"`
	Model   string             `json:"model"`
	Role    string             `json:"role"`
	Content []types.ContentBlock `json:"content"`
	StopReason string          `json:"stop_reason"`
	StopSequence *string       `json:"stop_sequence"`
	Usage   struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
		CacheCreation struct {
			Ephemeral1hInputTokens int `json:"ephemeral_1h_input_tokens"`
			Ephemeral5mInputTokens int `json:"ephemeral_5m_input_tokens"`
		} `json:"cache_creation"`
		CacheRead struct {
			Ephemeral1hInputTokens int `json:"ephemeral_1h_input_tokens"`
			Ephemeral5mInputTokens int `json:"ephemeral_5m_input_tokens"`
		} `json:"cache_read"`
	} `json:"usage"`
}

// anthropicError is the Anthropic error envelope: {"type":"error","error":{...}}.
type anthropicError struct {
	Type  string `json:"type"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// Error surfaces a provider-level failure with a retryable flag.
type Error struct {
	HTTPStatus int
	Retryable  bool
	Message    string
	// Source identifies where the error originated. "http" = HTTP layer (dial
	// or response status). "stream" = mid-stream SSE error event from the
	// server. Empty string preserves legacy callers' assumptions.
	Source string `json:"source,omitempty"`
}

func (e *Error) Error() string {
	src := e.Source
	if src == "" {
		return fmt.Sprintf("anthropic: http=%d retryable=%v: %s", e.HTTPStatus, e.Retryable, e.Message)
	}
	return fmt.Sprintf("anthropic: http=%d retryable=%v source=%s: %s", e.HTTPStatus, e.Retryable, src, e.Message)
}

// Provider is a reusable, concurrency-safe client bound to ONE OR MORE
// endpoints (BUG-R220 failover). A single instance serves all configured
// models over the configured endpoint list; the per-request API key is
// supplied by the registry. When multiple endpoints are configured, calls
// are routed to the active endpoint (selected by round-robin with
// per-endpoint 503-breaker skipping). On a transport-level failure
// (dial error / network reset / 503 / timeout) the provider advances to
// the next endpoint in the list before giving up.
type Provider struct {
	// endpoints is the failover list (BUG-R220). The first entry is the
	// primary; subsequent entries are tried in order when the primary
	// returns a transport-level failure. len(endpoints)==1 keeps the
	// pre-failover behavior exactly identical.
	endpoints []string
	// endpointMu guards activeIdx + the per-endpoint breaker state below.
	// A read-locked snapshot of activeIdx lets ChatStream/Chat pick an
	// endpoint without serializing the whole HTTP call on the lock.
	endpointMu   sync.RWMutex
	activeIdx    int
	// httpClient 用于非流式 Chat 的常规请求(http.Client.Timeout = 整体超时)。
	// 2026-07-24 优化:当 timeout >= longChatClientThreshold (5 min) 时,
	// doRequest 改用 streamClient 的 Transport(无整体超时),由
	// streamFirstByteTimeout(= StreamIdleTimeoutMs,默认 5 min)控制停滞检测,
	// 避免慢模型(Kimi/GLM 响应 2-5 min)被整体 deadline 中途 cancel。
	httpClient *http.Client
	// streamClient is used ONLY for streaming (SSE) requests. Unlike
	// httpClient it carries NO whole-request Timeout — a live SSE stream must
	// be allowed to run for as long as the model keeps producing tokens
	// (§130). Its Transport still bounds the connection-setup / response-header
	// phase (ResponseHeaderTimeout) so a dead upstream can't hang the dial
	// forever; the first-byte-of-body wait is enforced separately by
	// streamFirstByteTimeout inside idleTimeoutReader.
	//
	// 2026-07-24: doRequest 在长超时场景下也复用本 client 的 Transport
	// (见 doRequest 中的 longChatClientThreshold 分支)。
	streamClient  *http.Client
	// chatTimeout 记录 New() 传入的非流式整体超时,供 doRequest 判断
	// 是否走 streamClient Transport(长超时模式)。
	chatTimeout   time.Duration
	maxRetries    int
	userAgent     string // User-Agent header value for outbound requests
	billingHeader string // x-anthropic-billing-header (ClaudeCode-style call-site tag)

	// streamIdleTimeout caps the wall time between successful Read() calls on
	// the SSE body AFTER the first byte has already been received. Once the
	// upstream has begun streaming we do NOT want to abort a live response
	// (doing so wastes the compute the server already spent and forces a
	// duplicate retry — see §130). The default (15s) applies only when
	// SetStreamTimeouts is never called; the registry overrides it. 0 disables
	// the post-first-byte idle check entirely (stream until upstream EOF).
	//
	// §130 (2026-07-15): semantics changed. Previously this bounded every
	// inter-chunk gap including before the first byte. Now the pre-first-byte
	// wait is governed by streamFirstByteTimeout, and this idle window applies
	// only after data has started flowing. Set to 0 in the registry so a live
	// stream is NEVER killed by our side.
	streamIdleTimeout time.Duration

	// streamFirstByteTimeout caps ONLY the wait for the FIRST byte of the SSE
	// response body (i.e. dial + TLS + request-write + upstream first-token
	// latency). If the upstream delivers not a single byte within this window
	// the call fails Retryable so the agent can recover. Once the first byte
	// arrives this timeout is disarmed and the stream runs unbounded until the
	// model produces the complete response (§130, req 3/4). Default 2 min.
	streamFirstByteTimeout time.Duration

	// streamTotalTimeout is DEPRECATED as of §130 and no longer applied to the
	// streaming path — it used to cap the entire HTTP exchange (dial + headers
	// + body), which killed live streams the upstream was actively producing.
	// Retained only so SetStreamTimeouts keeps a stable 2-arg signature; the
	// value is ignored by doStream.
	streamTotalTimeout time.Duration

	// BUG-R65-3.2 / BUG-R214 端点级不可用短路熔断器。
	//
	// When the upstream proxy (configured via cfg.llm.endpoint / cfg.llm.endpoints)
	// hard 503 Service Unavailable for a specific model (e.g. Kimi 通道完全
	// 不可用) every per-call retry wastes up to ~16s × N bots = 大量 agent
	// wake time, all of which ends in the same failure. To prevent this, we
	// count endpoint-level failures within a rolling window; once the count
	// crosses breakerThreshold we mark that endpoint as "tripped" for
	// breakerCooldown, during which every call short-circuits with
	// *Error{Source:"breaker"} so the agent's auto-skip path (R40 §102) fires
	// immediately instead of waiting for the full timeout+retry chain.
	//
	// BUG-R214(2026-08-01): 计数口径从"仅 HTTP 503"泛化为**端点级不可用**
	// —— dial / connect / DNS / TLS / i-o timeout / connection refused /
	// reset / EOF 全部计入。一台彻底不可达的主机根本不会返回 503,旧口径
	// 下熔断器永不打开,13 个 bot 的每次唤醒都要付满一整轮 dial 超时
	// (30s / 120s / 600s),永远不会停。调用方 ctx 取消(Agent 自己放弃)
	// **不**计入,否则一个正常但慢的端点会被大量取消事件误熔断。
	//
	// The breaker is endpoint-scoped, so a failure from any model routed
	// through that endpoint trips it. This is intentional — the R65/R214
	// evidence shows the failure happens at the dial/response layer, before
	// per-model routing can even happen, so per-model scoping would be
	// ineffective. (Per-model 400 走独立的 model400 circuit,见下方。)
	//
	// BUG-R220: with multiple endpoints, the breaker map is keyed by
	// endpoint URL so a tripped primary doesn't poison the secondary.
	// breakerOpenAny() reports "all endpoints are tripped" and is the signal
	// ensureHealthyEndpoint / 重试循环用来短路的判据;per-endpoint state is
	// the failover advancement signal. 实现见 endpoint_breaker.go。
	breakerMu       sync.Mutex
	breakerWindow   map[string][]time.Time // endpoint → recent failure timestamps
	breakerOpenTill map[string]time.Time   // endpoint → cooldown end (zero = closed)

	// BUG-R172-P2 model-scoped 400 failure counter.
	//
	// R172 报告显示 Qwen-model / MeiTuan-model 的房间内每次调用都返回
	// http=400 retryable=false "Invalid request Error"(上游代理只透传一句
	// 泛化错误,不含字段级诊断)。与 503 breaker 不同,400 是**模型/请求形状**
	// 相关而非端点相关 — 13 人局里其它 11 个模型走同一 endpoint 全部正常,
	// 因此计数必须按 req.Model 分桶,不能按 endpoint 全局熔断。
	// 用途:(1) 第一次 400 时把请求线格式摘要(messages/tools/块数/payload
	// 大小)写入 WARN 日志,供根因定位;(2) 超过阈值后 Error 携带
	// Source="model_400_circuit",让上游调用方(Agent)把该模型视为本局
	// 不可用并快速 quarantine,而不是每个 wake 都白等一轮重试。
	model400Mu      sync.Mutex
	model400Window  map[string][]time.Time // model id → recent 400 timestamps
	model400Dumped  map[string]bool        // model id → 首次摘要日志是否已写
	model400Circuit map[string]time.Time   // model id → circuit 关闭时间(zero = 关闭)

	// §20260810-15: 429 限流熔断 — 与 400 协议错语义不同,单独熔断避免
	// retry chain 把配额打爆 + 累计 cf → 误 quarantine。
	model429Window  map[string][]time.Time // model id → recent 429 timestamps
	model429Circuit map[string]time.Time   // model id → circuit 关闭时间(zero = 关闭)
}

const (
	// breakerWindowDuration is the rolling window during which endpoint-level
	// failures (503 / dial / DNS / reset / i-o timeout) are counted. 60s is
	// wide enough to cover a single werewolf phase's worst-case retry chain
	// (~30s) but narrow enough to recover quickly after the upstream heals.
	// BUG-R214: 语义与常量值均未变,仅计数口径从"仅 503"扩到"端点级不可用"。
	breakerWindowDuration = 60 * time.Second

	// breakerThreshold is the number of failures within the window that
	// trips the breaker. 3 catches a hard upstream outage (Kimi 通道全挂 /
	// 主机彻底不可达) while tolerating a single transient blip.
	breakerThreshold = 3

	// breakerCooldown is how long the breaker stays open after tripping.
	// 60s matches the window so the breaker always has fresh evidence on
	// reopen.
	breakerCooldown = 60 * time.Second

	// BUG-R172-P2 model-scoped 400 circuit constants. 窗口 120s / 阈值 5 次:
	// R172 实测 Qwen seat 3 在 night_wolves 阶段 7 分钟累计 16+ 次 400,
	// 5 次足以判定"该模型本局不可用";冷却 120s 给上游代理(可能临时切
	// 通道)一次恢复机会。与 503 breaker 不同,熔断按 model id 分桶,
	// 不误伤同 endpoint 的其它健康模型。
	model400WindowDuration = 120 * time.Second
	model400Threshold      = 5
	model400Cooldown       = 120 * time.Second

	// longChatClientThreshold 非流式 Chat 的"长超时"判定阈值(2026-07-24)。
	// 当 chatTimeout >= 5 min 时,doRequest 不再使用 httpClient 的整体
	// Timeout(它会把"慢但正常"的 2-5 分钟响应中途 cancel),而改用
	// streamClient 的 Transport(无整体超时),停滞检测交给
	// idleTimeoutReader 的 firstByte/idle 窗口(streamFirstByteTimeout 由
	// StreamIdleTimeoutMs 驱动,默认 5 min)。
	longChatClientThreshold = 5 * time.Minute
)

// New builds a Provider. The first endpoint is the primary; subsequent
// endpoints form the failover list (BUG-R220). timeout controls the
// per-attempt HTTP deadline; maxRetries is the count of additional attempts
// on retryable failures (0 = fail-fast).
//
// Callers MUST NOT include a trailing slash on any endpoint — the client
// appends "/v1/messages". An empty list falls back to a single empty-string
// endpoint so legacy callers (`New(endpoint, ...)`) keep working unchanged.
//
// BUG-WEREWOLF-P0-NEW-1 (revised 2026-07-09): the fallback timeout was 8s,
// which killed real LLM calls (especially extended-thinking, 30-120s) before
// the model could respond. Raised to 300s (5min); the running LsmAgentGame.conf
// normally supplies the real value (2026-07-24: 默认升至 600000ms = 10min)。
// The stream idle/total timeouts are now propagated from config via
// registry.SetStreamTimeouts, overriding the 15s/90s defaults below.
//
// 2026-07-24 优化: 当 timeout >= longChatClientThreshold (5 min) 时,非流式
// Chat 也复用 streamClient 的 Transport(无整体超时,由 streamFirstByteTimeout
// 控制停滞) — 见 doRequest。
//
// BUG-R220 — endpoint failover: `endpoints` is the ordered list (primary
// first). The active endpoint index starts at 0 and is advanced on
// transport-level failures (dial / 5xx / 503 / network). Pass a single-
// element slice (or pass nothing / "" via the legacy overload) to preserve
// the pre-failover behavior.
func New(endpoints []string, timeout time.Duration, maxRetries int) *Provider {
	if timeout <= 0 {
		timeout = 300 * time.Second
	}
	if maxRetries < 0 {
		maxRetries = 0
	}
	// Sanitize: drop trailing whitespace / empty entries so a multi-line
	// conf edit doesn't accidentally introduce a blank primary that every
	// call will dial and immediately time out on.
	cleaned := make([]string, 0, len(endpoints))
	for _, ep := range endpoints {
		ep = strings.TrimSpace(ep)
		if ep != "" {
			cleaned = append(cleaned, ep)
		}
	}
	if len(cleaned) == 0 {
		// Legacy callers that still pass a single endpoint via the old
		// signature or that mis-configured an empty list end up with a
		// no-op provider; their calls will fail-fast at the dial stage.
		cleaned = []string{""}
	}
	// streamClient (§130): NO whole-request Timeout so a live SSE body can run
	// unbounded until the model finishes. We still bound the dial + TLS +
	// response-header phase via a custom Transport so a completely dead
	// upstream (never sends headers) fails fast instead of hanging. The
	// per-body first-byte wait is enforced by streamFirstByteTimeout.
	streamTransport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		// ResponseHeaderTimeout bounds only the wait for the response HEADERS,
		// never the streaming body. 2 min mirrors streamFirstByteTimeout so an
		// upstream that accepts the connection but never responds is reaped.
		ResponseHeaderTimeout: 120 * time.Second,
	}
	return &Provider{
		endpoints:              cleaned,
		activeIdx:              0,
		httpClient:             &http.Client{Timeout: timeout},
		streamClient:           &http.Client{Transport: streamTransport}, // Timeout: 0 → unbounded body
		chatTimeout:            timeout,
		maxRetries:             maxRetries,
		streamIdleTimeout:      15 * time.Second,
		streamFirstByteTimeout: 120 * time.Second,
		streamTotalTimeout:     90 * time.Second, // deprecated, ignored by doStream
		model400Window:         map[string][]time.Time{},
		model400Dumped:         map[string]bool{},
		model400Circuit:        map[string]time.Time{},
		model429Window:         map[string][]time.Time{},
		model429Circuit:        map[string]time.Time{},
		breakerWindow:          make(map[string][]time.Time, len(cleaned)),
		breakerOpenTill:        make(map[string]time.Time, len(cleaned)),
	}
}

// SetStreamTimeouts configures the streaming timeouts (§130 semantics).
//
//   - idle  → streamFirstByteTimeout: the maximum wait for the FIRST byte of
//     the SSE body (dial + upstream first-token latency). The registry passes
//     config.StreamIdleTimeoutMs (default 120s / 2 min) here. If not a single
//     byte arrives within this window the call fails Retryable.
//   - total → IGNORED (deprecated). It used to cap the whole HTTP exchange and
//     killed live streams; §130 removes that behaviour. The parameter is kept
//     only for signature stability. The value is recorded in streamTotalTimeout
//     but never applied by doStream.
//
// Critically, this method also FORCES streamIdleTimeout = 0, meaning that once
// the first byte has arrived the stream runs UNBOUNDED until the model emits
// the complete response — we never abort a response the upstream is actively
// producing (req 3). Use SetStreamIdleAfterFirstByte to re-enable a
// post-first-byte idle guard if ever needed.
//
// Safe for concurrent use — typically called once at registry construction.
func (p *Provider) SetStreamTimeouts(idle, total time.Duration) {
	if idle > 0 {
		p.streamFirstByteTimeout = idle
	}
	p.streamTotalTimeout = total // recorded but ignored by doStream
	// §130 req 3: never kill a live stream after the first byte.
	p.streamIdleTimeout = 0
}

// SetStreamIdleAfterFirstByte optionally re-arms a post-first-byte inter-chunk
// idle guard. 0 (the §130 default set by SetStreamTimeouts) disables it so a
// live stream is never aborted by our side. Exposed for tests / future tuning.
func (p *Provider) SetStreamIdleAfterFirstByte(idle time.Duration) {
	p.streamIdleTimeout = idle
}

// SetUserAgent sets the User-Agent header value carried by every outbound
// HTTP request. Format: "ProgramName/Version BuildTime".
func (p *Provider) SetUserAgent(ua string) { p.userAgent = ua }

// userAgentFor 决定本次出站请求的 User-Agent。
//
// 2026-08-06 §AgentClassName 增强:agentClassName 非空时,把
// p.userAgent 的"程序名前缀"(第一个 "/" 之前的部分)替换为 AgentClassName,
// 保留版本号 + 编译时间。例:
//
//	p.userAgent      = "LsmAgentGame/v1.0.0-7740495 Aug  6 2026 11:37:43"
//	agentClassName   = "LsmAgentGame-Werewolf-Player"
//	→ 出站 UA         = "LsmAgentGame-Werewolf-Player/v1.0.0-7740495 Aug  6 2026 11:37:43"
//
// agentClassName 为空时回退 p.userAgent(向后兼容旧调用方)。
// p.userAgent 为空或不含 "/" 时,直接返回 agentClassName(无版本可拼)。
//
// 详见 ServerGo/agent/class_names.go。
func (p *Provider) userAgentFor(agentClassName string) string {
	if agentClassName == "" {
		return p.userAgent
	}
	base := p.userAgent
	if base == "" {
		return agentClassName
	}
	slash := strings.IndexByte(base, '/')
	if slash < 0 || slash+1 >= len(base) {
		// 无 "/ver time" 后缀,直接返回 className(异常输入防御)。
		return agentClassName
	}
	return agentClassName + base[slash:]
}

// BUG-R214: 复述段落已压缩 — git blame 与 docs/ 索引可还原


// model400CircuitOpen reports whether the per-model 400 circuit is currently
// open for `model`. The cooldown expiry clears the window so a healed model
// re-accumulates from a clean slate. Safe for concurrent use.
func (p *Provider) model400CircuitOpen(model string) bool {
	if model == "" {
		return false
	}
	p.model400Mu.Lock()
	defer p.model400Mu.Unlock()
	till, ok := p.model400Circuit[model]
	if !ok || till.IsZero() {
		return false
	}
	if time.Now().After(till) {
		delete(p.model400Circuit, model)
		p.model400Window[model] = p.model400Window[model][:0]
		return false
	}
	return true
}

// recordModel400 records one HTTP 400 response for `model` and trips the
// per-model circuit when the rolling-window count crosses the threshold.
// On the FIRST recorded 400 for a model it emits a one-shot wire-summary log
// (message/block/tool counts + payload size) so the root cause of R172-style
// opaque "Invalid request Error" failures can be diagnosed from server logs.
func (p *Provider) recordModel400(model string, body *anthropicRequest, payload []byte) {
	if model == "" {
		return
	}
	p.model400Mu.Lock()
	defer p.model400Mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-model400WindowDuration)
	kept := p.model400Window[model][:0]
	for _, t := range p.model400Window[model] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	kept = append(kept, now)
	p.model400Window[model] = kept
	if !p.model400Dumped[model] && body != nil {
		p.model400Dumped[model] = true
		toolUse, toolResult, textBlocks := 0, 0, 0
		for _, m := range body.Messages {
			for _, c := range m.Content {
				switch c.Type {
				case "tool_use":
					toolUse++
				case "tool_result":
					toolResult++
				case "text":
					textBlocks++
				}
			}
		}
		logger.L().Warn("anthropic: model 400 wire summary (first occurrence, BUG-R172-P2)",
			zap.String("model", model),
			zap.Int("messages", len(body.Messages)),
			zap.Int("tools", len(body.Tools)),
			zap.Int("text_blocks", textBlocks),
			zap.Int("tool_use_blocks", toolUse),
			zap.Int("tool_result_blocks", toolResult),
			zap.Int("payload_bytes", len(payload)),
			zap.Int("max_tokens", body.MaxTokens))
	}
	if len(kept) >= model400Threshold {
		if till, ok := p.model400Circuit[model]; !ok || till.IsZero() {
			p.model400Circuit[model] = now.Add(model400Cooldown)
			logger.L().Warn("anthropic: model 400 circuit OPEN (BUG-R172-P2)",
				zap.String("model", model),
				zap.Int("failures_in_window", len(kept)),
				zap.Duration("cooldown", model400Cooldown))
		}
	}
}

// recordModelSuccess clears the per-model 400 window so an intermittent
// failure doesn't slowly accumulate into a circuit trip.
func (p *Provider) recordModelSuccess(model string) {
	if model == "" {
		return
	}
	p.model400Mu.Lock()
	defer p.model400Mu.Unlock()
	p.model400Window[model] = p.model400Window[model][:0]
	p.model429Window[model] = p.model429Window[model][:0]
}

// Model400CircuitOpen reports whether the per-model 400 circuit is currently
// open. This is the public form of model400CircuitOpen — Agent.run_llm
// short-circuits calls before going through the full retry chain when this
// returns true, saving 5-30s of wasted wake-up latency.
//
// §20260810-15: Exposed so the agent's callProvider() can do an O(1)
// short-circuit BEFORE serializing the request body. Without this, every
// wake during a 120s circuit-open period pays full HTTP latency only to
// fail immediately — which is the "4 calls then stop" symptom reported in
// the Tencent-model 16:32-16:34 incident.
func (p *Provider) Model400CircuitOpen(model string) bool {
	return p.model400CircuitOpen(model)
}

// BreakerOpenAny reports whether every configured endpoint currently has an
// open breaker. Same short-circuit contract as Model400CircuitOpen.
func (p *Provider) BreakerOpenAny() bool {
	return p.breakerOpenAny()
}

// ─── §20260810-15 model-scoped 429 (rate limit) circuit ───
//
// 429 是「上游临时被我们打爆」,与 5xx「上游真的坏了」语义不同:
//   - 5xx → circuit breaker (per-endpoint, 60s / 3 hits / 60s cooldown)
//   - 400 → model_400_circuit (per-model, 120s / 5 hits / 120s cooldown)
//   - 429 → model_429_circuit (per-model, 60s / 1 hit / 60s cooldown,本节新增)
//
// 旧实现下 429 走 5xx 同一条路径,每次都跑完 retry chain 才返回
// → 上游 rate-limit 期间 5-10 次 retry × 1-2s 退避 = 浪费 10-30s 配额
// → 同时累计 cf → 跨过 maxConsecutiveFailures → 误 quarantine
//
// 修复:429 立即打开 model_429_circuit(1 hit 即开),Agent.run_llm
// 短路前置 → 不发 HTTP → Agent 走 circuit-stuck 慢恢复(reWake 30s+)。
// 上游恢复 60s 后,circuit 自动关闭,下一次 LLM 调用正常发请求。

// model429WindowDuration / model429Threshold / model429Cooldown
// §20260810-15: 60s 窗口,1 次超阈值即开,60s 冷却。理由:
//   - 429 是上游明确告诉我们"等等再来",无视 Retry-After 是抗上游;
//   - 上游限流大概率短时(秒级到分钟级),60s 冷却足够;
//   - 1-hit 即开避免重试链把配额打爆;
const (
	model429WindowDuration = 60 * time.Second
	model429Threshold      = 1
	model429Cooldown       = 60 * time.Second
)

// Model429CircuitOpen reports whether the per-model 429 circuit is currently
// open for `model`. §20260810-15: 429 是「暂时被限流」,与 400「协议级错误」语义不同;
// 单独熔断避免 429 期间重试链把配额打爆 + 累计 cf 触发误 quarantine。
func (p *Provider) Model429CircuitOpen(model string) bool {
	if model == "" {
		return false
	}
	p.model400Mu.Lock()
	defer p.model400Mu.Unlock()
	till, ok := p.model429Circuit[model]
	if !ok || till.IsZero() {
		return false
	}
	if time.Now().After(till) {
		delete(p.model429Circuit, model)
		p.model429Window[model] = p.model429Window[model][:0]
		return false
	}
	return true
}

// recordModel429 records one HTTP 429 response for `model` and trips the
// per-model 429 circuit. §20260810-15: 1-hit 即开(限流是上游明确告诉我们"等等再来",
// 不需要累计到阈值才反应)。
func (p *Provider) recordModel429(model string) {
	if model == "" {
		return
	}
	p.model400Mu.Lock()
	defer p.model400Mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-model429WindowDuration)
	kept := p.model429Window[model][:0]
	for _, t := range p.model429Window[model] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	kept = append(kept, now)
	p.model429Window[model] = kept
	if len(kept) >= model429Threshold {
		if till, ok := p.model429Circuit[model]; !ok || till.IsZero() {
			p.model429Circuit[model] = now.Add(model429Cooldown)
			logger.L().Warn("anthropic: model 429 circuit OPEN (rate-limit short-circuit, §20260810-15)",
				zap.String("model", model),
				zap.Int("hits_in_window", len(kept)),
				zap.Duration("cooldown", model429Cooldown))
		}
	}
}

// CircuitState reports the circuit state for `model` as one of
// "closed" / "open_400" / "open_429". Used by BotTranscript.CircuitState
// for front-end visibility. §20260810-15 P2 增强。
func (p *Provider) CircuitState(model string) string {
	if model == "" {
		return "closed"
	}
	p.model400Mu.Lock()
	defer p.model400Mu.Unlock()
	now := time.Now()
	if till, ok := p.model400Circuit[model]; ok && !till.IsZero() && now.Before(till) {
		return "open_400"
	}
	if till, ok := p.model429Circuit[model]; ok && !till.IsZero() && now.Before(till) {
		return "open_429"
	}
	return "closed"
}

// ─── §20260810-15 测试-only helper ───
// 以下三个 ForceOpen* 方法通过 anthropic_export_test.go 桥接给外部 _test.go 使用;
// production 代码绝不调用(命名以 ForceOpen 开头,grep 一搜就锁定)。

// ForceOpenModel400Circuit 把 model400Circuit map 写入测试模型。仅 _test.go 用。
func (p *Provider) ForceOpenModel400Circuit(model string, cooldown time.Duration) {
	if model == "" {
		return
	}
	p.model400Mu.Lock()
	defer p.model400Mu.Unlock()
	p.model400Circuit[model] = time.Now().Add(cooldown)
}

// ForceOpenModel429Circuit 把 model429Circuit map 写入测试模型。仅 _test.go 用。
func (p *Provider) ForceOpenModel429Circuit(model string, cooldown time.Duration) {
	if model == "" {
		return
	}
	p.model400Mu.Lock()
	defer p.model400Mu.Unlock()
	p.model429Circuit[model] = time.Now().Add(cooldown)
}

// ForceOpenEndpointBreaker 把所有 endpoint breaker 打开。仅 _test.go 用。
func (p *Provider) ForceOpenEndpointBreaker(cooldown time.Duration) {
	p.breakerMu.Lock()
	defer p.breakerMu.Unlock()
	eps := p.Endpoints()
	if len(eps) == 0 {
		return
	}
	till := time.Now().Add(cooldown)
	for _, ep := range eps {
		p.breakerOpenTill[ep] = till
	}
}

// SetBillingHeader sets the value of the `x-anthropic-billing-header` header
// that every outbound request carries. Anthropic-side proxies / datadog use it
// to attribute traffic to a call site; ClaudeCode ships the literal
// "cc_version=2.1.195.58c; cc_entrypoint=cli;" prefix in their reference
// payload — we use a similar format to identify LsmAgentGame calls.
func (p *Provider) SetBillingHeader(bh string) { p.billingHeader = bh }

// BillingHeader exposes the currently-set billing header (for diagnostics).
func (p *Provider) BillingHeader() string { return p.billingHeader }

// ChatTimeout exposes the non-stream overall chat timeout this provider was
// constructed with (registry: llm.timeout_ms; 600s default). Callers that
// wrap Chat in their own context deadline should use this to size their
// budget — a shorter outer deadline always fires first and reports a
// misleading "context deadline exceeded" while the upstream is still working.
func (p *Provider) ChatTimeout() time.Duration { return p.chatTimeout }

// UserAgent exposes the currently-set User-Agent header value so the registry
// can propagate it to lazily-built per-endpoint providers.
func (p *Provider) UserAgent() string { return p.userAgent }

// ProviderType implements llm.LLMProvider.
func (p *Provider) ProviderType() string { return "anthropic" }

// Endpoint exposes the base endpoint (for diagnostics). When multiple
// endpoints are configured (BUG-R220) this returns the ACTIVE one so
// `/api/llm/health` and the admin UI show "where the next call will go".
// Use Endpoints() to see the full list.
func (p *Provider) Endpoint() string {
	return p.activeEndpoint()
}

// Endpoints returns the full configured failover list in priority order.
// The first element is the primary; the rest are tried in order when the
// primary fails (transport-level errors). Safe for concurrent use.
func (p *Provider) Endpoints() []string {
	p.endpointMu.RLock()
	defer p.endpointMu.RUnlock()
	out := make([]string, len(p.endpoints))
	copy(out, p.endpoints)
	return out
}

// Chat implements llm.LLMProvider. It POSTs to {endpoint}/v1/messages with the
// Bearer <key> authorization header and decodes the Anthropic response into
// types.LLMResponse. Retryable failures are retried with bounded linear
// backoff (1s → 2s → 3s → 4s, capped at 4s).
//
// 2026-07-24 优化:由原指数 500ms/1s/2s 改为线性 1s/2s/3s/4s,降低上游代理
// 把内层 retry-loop 判定为"热循环/滥用"再批量拒请求的概率。
func (p *Provider) Chat(ctx context.Context, key string, req types.LLMRequest) (types.LLMResponse, error) {
	body := anthropicRequest{
		Model:        req.Model,
		System:       req.System,
		Messages:     req.Messages,
		Tools:        req.Tools,
		MaxTokens:    req.MaxTokens,
		Metadata:     req.Metadata,
		OutputConfig: req.OutputConfig,
		ToolChoice:   req.ToolChoice,
		Stream:       false,
		Temperature:  req.Temperature,
	}
	// Pre-flight normalization: some proxies (DouBao) reject tool_use blocks
	// whose "input" field is missing — making sure it is at least {} lets every
	// provider's strictness pass.
	for i := range body.Messages {
		for j := range body.Messages[i].Content {
			if body.Messages[i].Content[j].Type == "tool_use" && body.Messages[i].Content[j].Input == nil {
				body.Messages[i].Content[j].Input = map[string]any{}
			}
		}
	}
	// BUG-R117-01: 复述段落已压缩 — git blame 与 docs/ 索引可还原

	ensureAssistantMessageHasText(body.Messages)
	// BUG-R226-P1-02 (2026-08-01) — thinking 只落顶层字段,type 判别符由
	// ThinkingConfig.MarshalJSON 保证;**不再**往 message 头部注入 thinking
	// 内容块(§14.1 权威用例的 messages[].content[] 只含 text/tool_use/
	// tool_result 三种块,消息级 thinking 块是 DouBao/GLM 400 的真因)。
	body.Thinking = req.Thinking
	payload, err := json.Marshal(body)
	if err != nil {
		return types.LLMResponse{}, fmt.Errorf("anthropic: marshal request: %w", err)
	}

	// BUG-R65-3: 复述段落已压缩 — git blame 与 docs/ 索引可还原

	if !p.ensureHealthyEndpoint() {
		return types.LLMResponse{}, &Error{
			HTTPStatus: 503,
			Retryable:  true,
			Source:     "breaker",
			Message:    "anthropic: endpoint breaker open (all upstream endpoints marked dead)",
		}
	}

	// BUG-R220: doRequest resolves p.activeEndpoint() internally so each
	// attempt can be re-routed to a healthy endpoint without rebuilding
	// the request URL on the caller's side.

	var lastErr error
	attempts := 1 + p.maxRetries
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			// 2026-07-24 优化:线性 1s/2s/3s/4s 退避,封顶 4s。
			// 原 500ms*(1<<n) 在 attempt 3+ 已 4s,但 attempt 1 仅 500ms
			// 太快,容易让上游代理把 retry-loop 误判为 hot-loop 拒请求。
			backoff := time.Duration(attempt) * time.Second
			if backoff > 4*time.Second {
				backoff = 4 * time.Second
			}
			select {
			case <-ctx.Done():
				return types.LLMResponse{}, ctx.Err()
			case <-time.After(backoff):
			}
		}

		respBody, _, err := p.doRequest(ctx, key, payload, false, req.AgentClassName)
		// BUG-R172-P2: record model-scoped 400 evidence for the circuit. We
		// do NOT short-circuit here (Chat callers handle their own
		// degradation); the record only powers the wire-summary dump + the
		// circuit signal surfaced on the streaming path via Source.
		if ae, ok := err.(*Error); ok && ae.HTTPStatus == 400 {
			p.recordModel400(req.Model, &body, payload)
		}
		// §20260810-15: 429 限流单独熔断(per-model 1-hit 即开,避免
		// retry chain 把配额打爆 + 累计 cf 触发误 quarantine)。
		if ae, ok := err.(*Error); ok && ae.HTTPStatus == 429 {
			p.recordModel429(req.Model)
		}
		if err == nil {
			p.recordModelSuccess(req.Model)
			// Success path: decode the body.
			var ar anthropicResponse
			if jerr := json.Unmarshal(respBody, &ar); jerr != nil {
				return types.LLMResponse{}, fmt.Errorf("anthropic: decode response: %w", jerr)
			}
			return types.LLMResponse{
				ID:         ar.ID,
				Model:      ar.Model,
				StopReason: ar.StopReason,
				Content:    ar.Content,
				Usage: types.LLMUsage{
					InputTokens:  ar.Usage.InputTokens,
					OutputTokens: ar.Usage.OutputTokens,
				},
			}, nil
		}
		lastErr = err
		// Only retry retryable failures.
		if ae, ok := err.(*Error); ok && !ae.Retryable {
			return types.LLMResponse{}, err
		}
		// BUG-R220: 复述段落已压缩 — git blame 与 docs/ 索引可还原

		if attempt+1 < attempts {
			// BUG-R214: 若这一轮失败已经把**全部**端点熔断(不可达主机的
			// 典型形态),继续把剩余 attempts 打完只是白付整轮 dial 超时。
			// breakerOpenAny() 在这里做提前收敛判定,让"全端点皆死"立刻
			// 以 Source="breaker" 返回,而不是拖满重试链。
			if p.breakerOpenAny() {
				return types.LLMResponse{}, &Error{
					HTTPStatus: 503,
					Retryable:  true,
					Source:     "breaker",
					Message:    "anthropic: endpoint breaker open mid-retry (all upstream endpoints marked dead)",
				}
			}
			p.advanceEndpoint()
		}
	}
	return types.LLMResponse{}, fmt.Errorf("anthropic: exhausted %d attempts: %w", attempts, lastErr)
}

// ChatStream implements llm.LLMProvider. It POSTs to {endpoint}/v1/messages
// with `stream:true` and returns the raw SSE byte stream for the caller to
// parse. The body bytes are also subject to the same pre-flight normalizations
// (tool_use.input → {}, thinking stub injection).
//
// The returned io.ReadCloser is the HTTP response body; the caller MUST close
// it. HTTP / wire errors are surfaced via the returned error so the caller
// doesn't have to introspect the body to discover failures.
//
// Streaming retries only the dial phase (5xx/429 from the proxy). Once a 2xx
// has arrived, mid-stream parsing / disconnects are the caller's problem:
// a partial SSE stream is a different failure mode than a connection blip
// and re-issuing it would double-charge the upstream. See ChatStreamAccumulate
// for the high-level wrapper that drives an SSE body through an Accumulator.
func (p *Provider) ChatStream(ctx context.Context, key string, req types.LLMRequest) (io.ReadCloser, error) {
	bodyReq := anthropicRequest{
		Model:        req.Model,
		System:       req.System,
		Messages:     req.Messages,
		Tools:        req.Tools,
		MaxTokens:    req.MaxTokens,
		Metadata:     req.Metadata,
		OutputConfig: req.OutputConfig,
		ToolChoice:   req.ToolChoice,
		Stream:       true,
		Temperature:  req.Temperature,
	}
	// Same pre-flight normalizations as Chat(): DouBao / GLM proxies enforce
	// the same wire shape for streaming requests.
	for i := range bodyReq.Messages {
		for j := range bodyReq.Messages[i].Content {
			if bodyReq.Messages[i].Content[j].Type == "tool_use" && bodyReq.Messages[i].Content[j].Input == nil {
				bodyReq.Messages[i].Content[j].Input = map[string]any{}
			}
		}
	}
	// BUG-R117-01: same DouBao text-block requirement applies to streaming
	// requests — see the comment in Chat() above.
	ensureAssistantMessageHasText(bodyReq.Messages)
	// BUG-R226-P1-02 (2026-08-01) — 与 Chat() 对称:thinking 只落顶层字段,
	// 不注入消息级内容块。
	bodyReq.Thinking = req.Thinking
	payload, err := json.Marshal(bodyReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: marshal stream request: %w", err)
	}
	// BUG-R65-3: 复述段落已压缩 — git blame 与 docs/ 索引可还原

	if !p.ensureHealthyEndpoint() {
		return nil, &Error{
			HTTPStatus: 503,
			Retryable:  true,
			Source:     "breaker",
			Message:    "anthropic: endpoint breaker open (all upstream endpoints marked dead)",
		}
	}
	// BUG-R172-P2: if this model's 400 circuit is open, fail fast with a
	// distinct Source so the agent layer can fast-quarantine instead of
	// burning another dial + retry cycle on a model that has 400'd every
	// call in the window.
	if p.model400CircuitOpen(req.Model) {
		return nil, &Error{
			HTTPStatus: 400,
			Retryable:  false,
			Source:     "model_400_circuit",
			Message:    "anthropic: model 400 circuit open (model repeatedly rejected requests in window; see 'model 400 wire summary' log)",
		}
	}
	body, err := p.doStreamWithRetry(ctx, key, payload, req.AgentClassName)
	// BUG-R172-P2: record model-scoped 400 evidence. doStreamWithRetry only
	// retries Retryable errors, so a 400 surfaces here exactly once.
	if ae, ok := err.(*Error); ok && ae.HTTPStatus == 400 {
		p.recordModel400(req.Model, &bodyReq, payload)
	}
	// §20260810-15: 429 限流单独熔断(streaming 路径同样)。
	if ae, ok := err.(*Error); ok && ae.HTTPStatus == 429 {
		p.recordModel429(req.Model)
	}
	if err == nil {
		p.recordModelSuccess(req.Model)
	}
	return body, err
}

// ChatStreamAccumulate is a convenience wrapper that combines ChatStream
// with AccumulateStream — opens the stream, fires onProgress for each
// event (may be nil), closes the body, and returns the reconstructed
// LLMResponse. This is the recommended path for new streaming consumers.
//
// ChatStreamAccumulate is intentionally NOT on the LLMProvider interface:
// adding it there would force any future provider (e.g. OpenAI) to mirror
// the same SSE-shaped protocol. Today only anthropic.Provider implements it.
//
// BUG-R65-3.2: we attach the *Provider to the context so AccumulateStream
// can call recordEndpointFailure() when an SSE error event indicates the
// upstream is hard-503. The hook is best-effort: when ctx carries no provider (e.g.
// tests calling AccumulateStream directly) the breaker is silently skipped.
func (p *Provider) ChatStreamAccumulate(ctx context.Context, key string, req types.LLMRequest, onProgress func(types.StreamEvent) error) (types.LLMResponse, error) {
	body, err := p.ChatStream(ctx, key, req)
	if err != nil {
		return types.LLMResponse{}, err
	}
	defer body.Close()
	return AccumulateStream(withProvider(ctx, p), body, onProgress)
}

// doStream performs the streaming HTTP request and returns the live response
// body. The caller must Close() it when done.
//
// §130 timeout semantics:
//   - The whole-exchange deadline is REMOVED. doStream uses p.streamClient,
//     which has no http.Client.Timeout, and it no longer layers a
//     streamTotalTimeout context deadline. A live SSE stream therefore runs
//     unbounded until the model produces the complete response.
//   - The 2xx body is ALWAYS wrapped in an idleTimeoutReader. The reader
//     enforces streamFirstByteTimeout on the FIRST read only (no-first-byte
//     guard, req 4 = 2 min); after the first byte it applies
//     streamIdleTimeout, which the registry sets to 0 → no further deadline
//     (req 3 = never abort a live response).
//   - Only the connection-setup + response-header phase is bounded, by the
//     streamClient Transport's ResponseHeaderTimeout.
func (p *Provider) doStream(ctx context.Context, key string, payload []byte, agentClassName string) (io.ReadCloser, error) {
	// BUG-R220: resolve the active endpoint URL at dial time so a failover
	// advance between attempts picks up the new primary without rebuilding
	// the request elsewhere. BUG-R214: 捕获本次实际拨号的端点,失败记账必须
	// 记到它头上(并发 goroutine 可能已经把 activeIdx 切走)。
	endpoint := p.activeEndpoint()
	url := endpoint + "/v1/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("anthropic-version", AnthropicVersion)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if ua := p.userAgentFor(agentClassName); ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	if p.billingHeader != "" {
		req.Header.Set("x-anthropic-billing-header", p.billingHeader)
	}
	// Use streamClient (no whole-request Timeout) so a live SSE body is never
	// killed mid-stream (§130). Fall back to httpClient defensively if a
	// Provider was constructed without New (e.g. zero-value in a test).
	client := p.streamClient
	if client == nil {
		client = p.httpClient
	}
	resp, err := client.Do(req)
	if err != nil {
		// BUG-R214: 传输层失败(dial/DNS/reset/i-o timeout)必须计入端点
		// 熔断窗口 —— 一台彻底不可达的主机永远不会返回 503,旧实现只在
		// 503 分支记账,熔断器因此永不打开,13 个 bot 每次唤醒都要付满一
		// 整轮 dial 超时。调用方 ctx 取消不记账(那是 Agent 放弃,不是端点死)。
		p.noteTransportFailure(ctx, endpoint, err)
		return nil, &Error{HTTPStatus: 0, Retryable: true, Message: err.Error()}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		retryable := resp.StatusCode == 429 || (resp.StatusCode >= 500 && resp.StatusCode < 600)
		// 与 doRequest 对称:流式路径的硬 503 同样计入端点熔断。
		if resp.StatusCode == 503 {
			p.recordEndpointFailureFor(endpoint, failReasonHTTP503)
		}
		msg := string(respBody)
		var ae anthropicError
		if jerr := json.Unmarshal(respBody, &ae); jerr == nil && ae.Error.Message != "" {
			msg = ae.Error.Message
		}
		return nil, &Error{HTTPStatus: resp.StatusCode, Retryable: retryable, Message: msg}
	}
	// 成功建流:清空该端点的熔断窗口,避免历史瞬断慢慢累积到阈值。
	p.recordSuccessFor(endpoint)
	// Wrap so the first-byte wait is bounded by streamFirstByteTimeout while
	// the post-first-byte stream is bounded by streamIdleTimeout (0 = never).
	firstByte := p.streamFirstByteTimeout
	if firstByte <= 0 {
		firstByte = 120 * time.Second
	}
	return newIdleTimeoutReader(resp.Body, firstByte, p.streamIdleTimeout), nil
}

// doStreamWithRetry wraps doStream with dial-phase retry on *Error{Retryable:true}.
// Mid-stream errors (after 2xx + body returned) are NOT retried — partial
// streams are the caller's responsibility. This mirrors Chat()'s retry
// behaviour at anthropic.go:222-257.
//
// 2026-07-24 优化:线性退避 1s/2s/3s/4s(cap 4s),代替原指数 500ms*2^n 在
// attempt 1 起步太快,容易被上游代理判定为 retry-storm 拒请求。
func (p *Provider) doStreamWithRetry(ctx context.Context, key string, payload []byte, agentClassName string) (io.ReadCloser, error) {
	var lastErr error
	for attempt := 0; attempt <= p.maxRetries; attempt++ {
		body, err := p.doStream(ctx, key, payload, agentClassName)
		if err == nil {
			return body, nil
		}
		lastErr = err
		ae, ok := err.(*Error)
		if !ok || !ae.Retryable {
			return nil, err
		}
		if attempt == p.maxRetries {
			break
		}
		// 2026-07-24 优化:线性 1s/2s/3s/4s(cap 4s)。
		backoff := time.Duration(attempt+1) * time.Second
		if backoff > 4*time.Second {
			backoff = 4 * time.Second
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
		// BUG-R220: transport-level failures advance to the next endpoint
		// when one is available AND there are still retry attempts left.
		// advanceEndpoint is a no-op for single-endpoint configs so the
		// legacy "retry the same endpoint up to N times" behaviour is
		// preserved exactly.
		if attempt+1 <= p.maxRetries {
			// BUG-R214: 与 Chat 对称 —— 若剩余端点已全部熔断,不再把剩下的
			// attempts 花在整轮 dial 超时上,立刻以 breaker 短路返回。
			if p.breakerOpenAny() {
				return nil, &Error{
					HTTPStatus: 503,
					Retryable:  true,
					Source:     "breaker",
					Message:    "anthropic: endpoint breaker open mid-retry (all upstream endpoints marked dead)",
				}
			}
			p.advanceEndpoint()
		}
	}
	return nil, fmt.Errorf("anthropic: exhausted %d attempts: %w", p.maxRetries+1, lastErr)
}

// doRequest performs one HTTP attempt. On non-2xx it decodes the Anthropic
// error envelope and tags the error retryable iff the HTTP code is 429 / 5xx.
// `stream` controls whether `Accept: text/event-stream` is set; this matters
// for proxies that differentiate based on the Accept header.
//
// 2026-07-24 优化 — 长超时客户端选择:
//   - 常规(chatTimeout < 5 min):使用 httpClient(整体 Timeout),一次性读 body。
//   - 长超时(chatTimeout >= 5 min,如生产 10 min):改用 streamClient 的
//     Transport(无整体超时),body 包一层 idleTimeoutReader — 停滞检测由
//     streamFirstByteTimeout(首字节)与 streamIdleTimeout(首字节后,§130
//     默认 0=不中断)控制。慢模型(Kimi/GLM)响应 2-5 分钟是预期场景,不应
//     被整体 deadline 中途 cancel 进 quarantine 路径。非流式响应同样是
//     chunked 到达,idle reader 语义天然适用。
func (p *Provider) doRequest(ctx context.Context, key string, payload []byte, stream bool, agentClassName string) ([]byte, int, error) {
	// BUG-R220: resolve the active endpoint URL at dial time so a failover
	// advance between attempts picks up the new primary. BUG-R214: 同 doStream,
	// 捕获本次实际拨号的端点用于失败记账。
	endpoint := p.activeEndpoint()
	url := endpoint + "/v1/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("anthropic-version", AnthropicVersion)
	req.Header.Set("Content-Type", "application/json")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	if ua := p.userAgentFor(agentClassName); ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	if p.billingHeader != "" {
		req.Header.Set("x-anthropic-billing-header", p.billingHeader)
	}

	// 2026-07-24: 按超时预算选择 client。
	longTimeout := p.chatTimeout >= longChatClientThreshold
	client := p.httpClient
	if longTimeout && p.streamClient != nil {
		client = p.streamClient
	}

	resp, err := client.Do(req)
	if err != nil {
		// BUG-R214: 见 doStream 中的同款注释 —— 传输层失败必须计入端点熔断,
		// 调用方 ctx 取消不计入。
		p.noteTransportFailure(ctx, endpoint, err)
		return nil, 0, &Error{HTTPStatus: 0, Retryable: true, Message: err.Error()}
	}
	defer resp.Body.Close()

	// 长超时模式:body 包 idleTimeoutReader 做停滞检测(复用 doStream 的
	// 两阶段策略);常规模式直接读(http.Client.Timeout 已兜底整体耗时)。
	var bodyReader io.Reader = resp.Body
	if longTimeout {
		firstByte := p.streamFirstByteTimeout
		if firstByte <= 0 {
			firstByte = 120 * time.Second
		}
		bodyReader = newIdleTimeoutReader(resp.Body, firstByte, p.streamIdleTimeout)
	}

	respBody, readErr := io.ReadAll(io.LimitReader(bodyReader, 1<<20)) // 1 MiB cap
	if readErr != nil {
		// BUG-R214: body 中途断流(reset / closed connection / first-byte
		// 超时)同样是端点级不可用证据。
		p.noteTransportFailure(ctx, endpoint, readErr)
		return nil, resp.StatusCode, &Error{HTTPStatus: resp.StatusCode, Retryable: true, Message: readErr.Error()}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		retryable := resp.StatusCode == 429 || (resp.StatusCode >= 500 && resp.StatusCode < 600)
		// BUG-R65-3.2: hard 503 from the upstream proxy counts toward the
		// short-circuit breaker. We deliberately only count plain 503 (not
		// 429 or other 5xx) to avoid tripping on quota / rate-limit hiccups
		// that the proxy would actually recover from in a few seconds.
		if resp.StatusCode == 503 {
			p.recordEndpointFailureFor(endpoint, failReasonHTTP503)
		}
		msg := string(respBody)
		var ae anthropicError
		if jerr := json.Unmarshal(respBody, &ae); jerr == nil && ae.Error.Message != "" {
			msg = ae.Error.Message
		}
		return nil, resp.StatusCode, &Error{HTTPStatus: resp.StatusCode, Retryable: retryable, Message: msg}
	}
	// BUG-R65-3.2: a successful 2xx clears the breaker window so a healed
	// endpoint doesn't immediately re-trip on a single transient failure.
	p.recordSuccessFor(endpoint)
	return respBody, resp.StatusCode, nil
}

// ParseSSE reads an SSE byte stream (as produced by ChatStream) and yields
// one parsed event per call. Empty `data:` lines are skipped; comment lines
// (starting with `:`) are dropped; multi-line data fields are concatenated
// with newlines per the SSE spec.
//
// The function returns io.EOF when the underlying stream ends cleanly, or
// any other error from the reader. A closing `data: [DONE]` line (used by
// OpenAI but not Anthropic) is forwarded as Type="done" so the caller can
// short-circuit.
//
// In addition to the typed StreamEvent fields, ParseSSE unpacks the
// envelope-specific scalars (message_id / message_model from
// message_start.message; content_block_type / id / name from
// content_block_start.content_block) so the Accumulator can rebuild an
// LLMResponse without re-parsing the JSON payload.
func ParseSSE(r io.Reader, onEvent func(types.StreamEvent) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1<<16), 1<<20) // up to 1 MiB per event
	var (
		eventType string
		dataBuf   strings.Builder
	)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			// Dispatch the buffered event.
			if eventType != "" && dataBuf.Len() > 0 {
				data := dataBuf.String()
				if strings.TrimSpace(data) == "[DONE]" {
					if err := onEvent(types.StreamEvent{Type: "done"}); err != nil {
						return err
					}
				} else {
					ev, perr := parseStreamEventJSON(data)
					if perr == nil {
						if err := onEvent(ev); err != nil {
							return err
						}
					}
					// Parse failure on a single event is non-fatal: the caller
					// can keep accumulating deltas via subsequent events. The
					// ClaudeCode reference client behaves the same way (logs +
					// continues on a per-event parse error).
				}
			}
			eventType = ""
			dataBuf.Reset()
			continue
		}
		if strings.HasPrefix(line, ":") {
			// Comment / heartbeat.
			continue
		}
		if strings.HasPrefix(line, "event:") {
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			chunk := strings.TrimPrefix(line, "data:")
			chunk = strings.TrimPrefix(chunk, " ")
			if dataBuf.Len() > 0 {
				dataBuf.WriteByte('\n')
			}
			dataBuf.WriteString(chunk)
		}
	}
	return scanner.Err()
}

// parseStreamEventJSON decodes one SSE `data:` payload into a StreamEvent.
// Most fields land in the typed struct; the envelope-specific scalars
// (message_start.message.{id,model}, content_block_start.content_block.{type,id,name})
// are unpacked by hand into the StreamEvent's flat scalars so the
// Accumulator doesn't need a second-pass JSON re-decode.
//
// The first pass uses a dedicated decoder type (rawStreamEvent) because
// StreamEvent.Delta is `string` but the wire format emits `delta` as an
// OBJECT (e.g. message_delta.delta = {"stop_reason":"..."}). The strict
// typed decoder rejects the event outright; rawStreamEvent uses
// json.RawMessage so the strict pass survives, and the second pass into
// `raw` lifts the nested scalars we actually need.
func parseStreamEventJSON(data string) (types.StreamEvent, error) {
	// First pass: tolerant typed decoder that survives object/array values
	// for fields declared as string.
	type rawStreamEvent struct {
		Type         string `json:"type"`
		Index        int    `json:"index,omitempty"`
		DeltaType    string `json:"delta_type,omitempty"`
		StopReason   string `json:"stop_reason,omitempty"`
		UsageInput   int    `json:"usage_input_tokens,omitempty"`
		UsageOutput  int    `json:"usage_output_tokens,omitempty"`
		ErrorMessage string `json:"error_message,omitempty"`
		// Leave MessageID/Model/ContentBlock* unset here — we lift them
		// from the second-pass generic map.
	}
	var rs rawStreamEvent
	if err := json.Unmarshal([]byte(data), &rs); err != nil {
		return types.StreamEvent{Type: rs.Type}, err
	}
	ev := types.StreamEvent{
		Type:         rs.Type,
		Index:        rs.Index,
		DeltaType:    rs.DeltaType,
		StopReason:   rs.StopReason,
		UsageInput:   rs.UsageInput,
		UsageOutput:  rs.UsageOutput,
		ErrorMessage: rs.ErrorMessage,
	}
	// Second pass: into a generic map for envelope-scalar enrichment.
	var raw map[string]any
	if err := json.Unmarshal([]byte(data), &raw); err != nil {
		return ev, nil // typed fields populated; bail on envelope enrichment
	}

	// Extract delta.{delta,type,stop_reason,usage.input_tokens,usage.output_tokens}
	// from the message_delta envelope, since StreamEvent's typed fields don't
	// carry mid-stream usage exactly right under all proxies.
	switch ev.Type {
	case "message_start":
		// message_start.message.{id,model}
		if msg, ok := raw["message"].(map[string]any); ok {
			if id, ok := msg["id"].(string); ok {
				ev.MessageID = id
			}
			if model, ok := msg["model"].(string); ok {
				ev.MessageModel = model
			}
			// message_start.usage.{input_tokens,output_tokens}
			if usage, ok := msg["usage"].(map[string]any); ok {
				ev.UsageInput = jsonInt(usage["input_tokens"])
				ev.UsageOutput = jsonInt(usage["output_tokens"])
			}
		}
	case "content_block_start":
		// content_block_start.content_block.{type,id,name}
		if cb, ok := raw["content_block"].(map[string]any); ok {
			if t, ok := cb["type"].(string); ok {
				ev.ContentBlockType = t
			}
			if id, ok := cb["id"].(string); ok {
				ev.ContentBlockID = id
			}
			if name, ok := cb["name"].(string); ok {
				ev.ContentBlockName = name
			}
		}
	case "message_delta":
		// message_delta.usage.{input_tokens,output_tokens}
		if usage, ok := raw["usage"].(map[string]any); ok {
			ev.UsageInput = jsonInt(usage["input_tokens"])
			ev.UsageOutput = jsonInt(usage["output_tokens"])
		}
		// message_delta.delta.stop_reason — typed struct doesn't reach into
		// a nested object, so we lift it here.
		if d, ok := raw["delta"].(map[string]any); ok {
			if sr, ok := d["stop_reason"].(string); ok {
				ev.StopReason = sr
			}
		}
	case "content_block_delta":
		// content_block_delta.delta.{type,text,partial_json,thinking}
		if d, ok := raw["delta"].(map[string]any); ok {
			if t, ok := d["type"].(string); ok {
				ev.DeltaType = t
			}
			switch ev.DeltaType {
			case "text_delta":
				if s, ok := d["text"].(string); ok {
					ev.Delta = s
				}
			case "thinking_delta":
				if s, ok := d["thinking"].(string); ok {
					ev.Delta = s
				}
			case "input_json_delta":
				if s, ok := d["partial_json"].(string); ok {
					ev.Delta = s
				}
			}
		}
	case "error":
		// error.error.message
		if e, ok := raw["error"].(map[string]any); ok {
			if m, ok := e["message"].(string); ok {
				ev.ErrorMessage = m
			}
		}
	}
	return ev, nil
}

// jsonInt coerces a JSON number (json.Unmarshal decodes into float64) back
// into an int. Returns 0 for nil / non-numeric values (matches the
// StreamEvent zero value).
func jsonInt(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case int64:
		return int(x)
	}
	return 0
}

// idleTimeoutReader wraps an io.ReadCloser and enforces the §130 two-phase
// stream timeout policy:
//
//   - firstByteTimeout: applies to the read(s) BEFORE the first byte of the
//     body has been received. If not a single byte arrives within this window
//     the reader fires a Retryable *Error. This is the "no first byte" guard
//     (req 4, default 2 min).
//   - idle: applies to reads AFTER the first byte. When 0 (the §130 default)
//     NO timer is armed and the read blocks on the inner reader until the
//     upstream delivers the next chunk or EOF — i.e. a live stream is never
//     aborted by our side (req 3). When > 0 it caps the inter-chunk gap.
//
// It does NOT spawn a persistent goroutine — each Read races its own timer
// against the inner Read via select, so there's no leak risk if the body is
// left dangling.
//
// Thread safety: a single goroutine should drive the stream (the SSE parser
// is single-threaded by construction). The mutex is purely defensive.
type idleTimeoutReader struct {
	r                io.ReadCloser
	firstByteTimeout time.Duration
	idle             time.Duration
	gotFirstByte     bool
	mu               sync.Mutex
	fired            bool
	lastErr          error
	lastErrN         int
}

func newIdleTimeoutReader(r io.ReadCloser, firstByteTimeout, idle time.Duration) *idleTimeoutReader {
	return &idleTimeoutReader{r: r, firstByteTimeout: firstByteTimeout, idle: idle}
}

func (r *idleTimeoutReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	if r.fired {
		err := r.lastErr
		n := r.lastErrN
		r.mu.Unlock()
		// Surface a non-nil error with n=0 so callers see EOF-style behavior.
		return n, err
	}
	gotFirst := r.gotFirstByte
	r.mu.Unlock()

	// Pick the deadline for THIS read: before the first byte we bound the wait
	// by firstByteTimeout; after it we bound by idle (0 = no deadline → block
	// on the inner reader until the upstream produces more or EOF, §130 req 3).
	var deadline time.Duration
	if !gotFirst {
		deadline = r.firstByteTimeout
	} else {
		deadline = r.idle
	}

	// No deadline armed: delegate straight to the inner reader. This is the
	// hot path once a live stream is flowing (idle == 0).
	if deadline <= 0 {
		n, err := r.r.Read(p)
		if n > 0 {
			r.mu.Lock()
			r.gotFirstByte = true
			r.mu.Unlock()
		}
		return n, err
	}

	// Reset the timer for each call; do not hold the mutex during the inner
	// Read so a slow upstream doesn't block unrelated calls (e.g. Close).
	timer := time.NewTimer(deadline)
	defer timer.Stop()

	readCh := make(chan readResult, 1)
	go func() {
		n, err := r.r.Read(p)
		readCh <- readResult{n: n, err: err}
	}()

	select {
	case res := <-readCh:
		if res.n > 0 {
			r.mu.Lock()
			r.gotFirstByte = true
			r.mu.Unlock()
		}
		if res.err != nil {
			return res.n, res.err
		}
		return res.n, nil
	case <-timer.C:
		// Timed out. Build the sticky error describing WHICH phase timed out so
		// the classifier / logs can tell "no first byte in 2m" apart from a
		// mid-stream stall. Both are Retryable so the agent recovers.
		phase := "first-byte"
		if gotFirst {
			phase = "inter-chunk-idle"
		}
		r.mu.Lock()
		r.fired = true
		r.lastErr = &Error{
			HTTPStatus: 0,
			Retryable:  true,
			Message:    fmt.Sprintf("anthropic: stream %s timeout after %s", phase, deadline),
		}
		r.lastErrN = 0
		lastErr := r.lastErr
		r.mu.Unlock()
		// Close the underlying body so the in-flight Read goroutine returns
		// promptly instead of leaking. Best-effort.
		_ = r.r.Close()
		// Drain the (now-closed) body result so we don't leak a goroutine.
		<-readCh
		return 0, lastErr
	}
}

// readResult carries the inner Read's outcome back from the helper goroutine.
type readResult struct {
	n   int
	err error
}

func (r *idleTimeoutReader) Close() error {
	return r.r.Close()
}

// ensureAssistantMessageHasText walks msgs and prepends a neutral text block
// to any assistant message whose `content` array lacks one. DouBao-model
// (and similar strict proxies) reject requests with the 400 error
//
//	missing `messages.content.text` parameter
//
// when an assistant turn contains ONLY tool_use blocks (e.g. a pure
// `wolf_kill` / `vote` / `hunter_shoot` call with no narration text).
// Standard Anthropic API tolerates tool_use-only assistant turns, but
// DouBao's validator requires every message in `messages[]` to carry at
// least one text block.
//
// We mutate `msgs` in place: the slice header is shared with the caller but
// each Message struct's Content slice is local to the loop variable, so
// prepending does not affect the original request view (Memory still holds
// the unmodified Content for the UI AgentThoughtPanel). The neutral text
// (" ", a single space) is a no-op for permissive providers — they accept a
// leading-whitespace text block alongside tool_use blocks without complaint.
//
// BUG-R117-01 / BUG-R118-01 (2026-07-14): R117 报告 DouBao-model 连续触发
// "missing messages.content.text parameter" 400 错误 → 永久 quarantine。
// R117 初版修复补了一个 Text="" 的 text 块,但 ContentBlock.MarshalJSON
// 在 text 分支对 Text 字段使用 omitempty,空字符串序列化时丢弃 "text"
// key,provider 侧看到 `{"type":"text"}`(无 text 字段)→ 仍然 400。
// R118 修复:改为预置非空文本 " "(空格),保证 "text" key 出现在 wire 上,
// 满足 DouBao 严格校验;对宽容 Provider 仍是 no-op。
func ensureAssistantMessageHasText(msgs []types.Message) {
	for i := range msgs {
		if msgs[i].Role != "assistant" {
			continue
		}
		hasText := false
		for _, c := range msgs[i].Content {
			if c.Type == "text" {
				hasText = true
				break
			}
		}
		if hasText {
			continue
		}
		// Prepend a neutral text block so the assistant turn carries at least
		// one text block. DouBao's validator only checks that a text-type block
		// is present, not its contents.
		//
		// BUG-R118-01 (2026-07-14 BUG-R117-01 修复回归): 这里必须使用非空
		// Text 值,例如 " "。types.ContentBlock.MarshalJSON 在 text 分支对
		// Text 字段使用了 `json:"text,omitempty"`(types.go),空字符串会被
		// omitempty 在序列化时丢弃 "text" key,导致 wire 上这条 text 块退化为
		// `{"type":"text"}`(没有 text 字段)。DouBao-model 的严格校验器检查的是
		// "text" 字段是否存在(错误信息 "missing messages.content.text parameter"),
		// 缺少 key → 仍然报 400 "missing messages.content.text parameter"。
		// 使用一个空格既保留了 "text" key(满足 DouBao),又对宽容 Provider 等价于
		// 无内容 no-op(前导空白不影响文本语义)。
		msgs[i].Content = append([]types.ContentBlock{{Type: "text", Text: " "}}, msgs[i].Content...)
	}
}

