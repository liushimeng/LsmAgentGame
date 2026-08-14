// Package openai implements llm.LLMProvider against the OpenAI Chat
// Completions wire format (the "openai-completions" protocol). This is a
// FIRST-CLASS protocol implementation — NOT a translation layer on top of the
// anthropic provider. It owns its request body (chatRequest), SSE chunk parser
// (chatChunk), and circuit breaking.
//
// Design contract: docs/LLM与Agent/AgentOpenAI工具集与道具协议.md §3.
//
// Outbound headers (no Anthropic-private headers):
//
//	Authorization: Bearer <key>
//	Content-Type:  application/json
//	Accept:        text/event-stream        (stream only)
//	User-Agent:    <AgentClassName>/<AppVersion> <buildDateTime>
package openai

import (
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

// Error surfaces a provider-level failure with a retryable flag — the same
// shape as anthropic.Error so callers' type-assertion handling keeps working.
// HTTPStatus 0 means transport-layer (dial / reset / timeout) failure.
type Error struct {
	HTTPStatus int
	Retryable  bool
	Message    string
	Source     string // "http" | "stream" | "breaker" | "transport"
}

func (e *Error) Error() string {
	src := e.Source
	if src == "" {
		return fmt.Sprintf("openai: http=%d retryable=%v: %s", e.HTTPStatus, e.Retryable, e.Message)
	}
	return fmt.Sprintf("openai: http=%d retryable=%v source=%s: %s", e.HTTPStatus, e.Retryable, src, e.Message)
}

// Provider is a reusable, concurrency-safe client bound to one or more
// endpoints (failover). The per-request API key is supplied by the registry
// via Chat(ctx, key, req), so a single instance serves all configured models
// that share this endpoint list.
type Provider struct {
	endpoints    []string
	endpointMu   sync.RWMutex
	activeIdx    int
	httpClient   *http.Client // non-stream overall timeout
	streamClient *http.Client // no whole-request timeout for live SSE
	chatTimeout  time.Duration
	maxRetries   int
	userAgent    string

	// §130 semantics: idle==0 disables the post-first-byte inter-chunk guard so
	// a live stream is never aborted once the upstream starts producing.
	streamIdleTimeout      time.Duration // post-first-byte (forced to 0)
	streamFirstByteTimeout time.Duration // pre-first-byte cap (2 min default)

	// Endpoint-level short-circuit breaker (mirrors anthropic's endpoint
	// breaker: 60s window / 3 hits / 60s cooldown). Model-scoped 400/429
	// circuits are intentionally NOT implemented for the openai protocol in
	// the first release (known gap documented in the design doc §3) — the
	// agent's run_llm.go circuit pre-check only type-asserts *anthropic.Provider,
	// so openai models simply skip it, which is the correct behavior.
	breakerMu     sync.Mutex
	breakerWindow map[string][]time.Time // endpoint → recent failures
	breakerOpen   map[string]time.Time   // endpoint → cooldown end
}

const (
	breakerWindowDuration = 60 * time.Second
	breakerThreshold      = 3
	breakerCooldown       = 60 * time.Second
)

// New builds a Provider. endpoints[0] is the primary; the rest are the
// failover list. timeout bounds the non-stream HTTP client; maxRetries is the
// number of extra attempts on retryable failures. Empty list → a single
// "" endpoint so legacy single-endpoint configs keep working.
func New(endpoints []string, timeout time.Duration, maxRetries int) *Provider {
	if timeout <= 0 {
		timeout = 300 * time.Second
	}
	if maxRetries < 0 {
		maxRetries = 0
	}
	cleaned := make([]string, 0, len(endpoints))
	for _, ep := range endpoints {
		if ep = strings.TrimSpace(ep); ep != "" {
			cleaned = append(cleaned, ep)
		}
	}
	if len(cleaned) == 0 {
		cleaned = []string{""}
	}
	streamTransport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: 120 * time.Second, // bounds dial+headers only, never the body
	}
	return &Provider{
		endpoints:              cleaned,
		activeIdx:              0,
		httpClient:             &http.Client{Timeout: timeout},
		streamClient:           &http.Client{Transport: streamTransport},
		chatTimeout:            timeout,
		maxRetries:             maxRetries,
		streamIdleTimeout:      0,
		streamFirstByteTimeout: 120 * time.Second,
		breakerWindow:          make(map[string][]time.Time, len(cleaned)),
		breakerOpen:            make(map[string]time.Time, len(cleaned)),
	}
}

// SetUserAgent sets the User-Agent header for every outbound request. Format:
// "ProgramName/Version BuildTime". The per-request AgentClassName overlay is
// applied in userAgentFor().
func (p *Provider) SetUserAgent(ua string) { p.userAgent = ua }

// UserAgent exposes the configured UA so the registry can propagate it to
// lazily-built per-endpoint providers.
func (p *Provider) UserAgent() string { return p.userAgent }

// userAgentFor overlays an optional AgentClassName onto the base UA (§24):
//
//	base = "LsmAgentGame/v1.0.0-7740495 Aug  6 2026 11:37:43"
//	cn   = "LsmAgentGame-Werewolf-Player"
//	→    = "LsmAgentGame-Werewolf-Player/v1.0.0-7740495 Aug  6 2026 11:37:43"
//
// Mirrors anthropic.Provider.userAgentFor exactly so the two providers produce
// identical UA shapes for the same AgentClassName.
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
		return agentClassName
	}
	return agentClassName + base[slash:]
}

// SetStreamTimeouts configures the streaming timeouts with §130 semantics:
//
//   - idle  → streamFirstByteTimeout: max wait for the FIRST byte of the SSE
//     body (dial + upstream first-token latency). The registry passes
//     config.StreamIdleTimeoutMs (default 300s). No first byte in this window
//     ⇒ Retryable failure.
//   - total → ignored (kept only for signature symmetry with the registry).
//
// It ALSO forces streamIdleTimeout=0 so a live stream is NEVER aborted after
// the first byte (§130 req 3).
func (p *Provider) SetStreamTimeouts(idle, total time.Duration) {
	if idle > 0 {
		p.streamFirstByteTimeout = idle
	}
	p.streamIdleTimeout = 0 // §130: never kill a live stream post-first-byte
}

// ChatTimeout exposes the non-stream overall timeout so callers can size their
// context budget.
func (p *Provider) ChatTimeout() time.Duration { return p.chatTimeout }

// ProviderType implements llm.LLMProvider.
func (p *Provider) ProviderType() string { return types.ProviderTypeOpenAICompletions }

// Endpoint exposes the active endpoint (for diagnostics).
func (p *Provider) Endpoint() string {
	p.endpointMu.RLock()
	defer p.endpointMu.RUnlock()
	if p.activeIdx < len(p.endpoints) {
		return p.endpoints[p.activeIdx]
	}
	return ""
}

// Endpoints returns the failover list in priority order.
func (p *Provider) Endpoints() []string {
	p.endpointMu.RLock()
	defer p.endpointMu.RUnlock()
	out := make([]string, len(p.endpoints))
	copy(out, p.endpoints)
	return out
}

// ─── Chat (non-streaming) ───

// Chat implements llm.LLMProvider. POSTs to {endpoint}/chat/completions and
// decodes the chat.completion body into types.LLMResponse. Retryable failures
// get bounded linear backoff (1s→2s→3s→4s cap). tool_use.input is normalized
// to {} pre-flight (some OpenAI-compatible proxies reject missing arguments).
func (p *Provider) Chat(ctx context.Context, key string, req types.LLMRequest) (types.LLMResponse, error) {
	body := buildRequest(req, false)
	normalizeToolInput(body.Messages)
	payload, err := json.Marshal(body)
	if err != nil {
		return types.LLMResponse{}, fmt.Errorf("openai: marshal request: %w", err)
	}

	if !p.ensureHealthyEndpoint() {
		return types.LLMResponse{}, &Error{HTTPStatus: 503, Retryable: true, Source: "breaker",
			Message: "openai: endpoint breaker open (all upstream endpoints marked dead)"}
	}

	var lastErr error
	attempts := 1 + p.maxRetries
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
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
		respBody, _, err := p.doRequest(ctx, key, payload, req.AgentClassName)
		if err == nil {
			var cr chatResponse
			if jerr := json.Unmarshal(respBody, &cr); jerr != nil {
				return types.LLMResponse{}, fmt.Errorf("openai: decode response: %w", jerr)
			}
			return normalizeResponse(cr), nil
		}
		lastErr = err
		if ae, ok := err.(*Error); ok && !ae.Retryable {
			return types.LLMResponse{}, err
		}
		if attempt+1 < attempts {
			if p.breakerOpenAny() {
				return types.LLMResponse{}, &Error{HTTPStatus: 503, Retryable: true, Source: "breaker",
					Message: "openai: endpoint breaker open mid-retry (all upstream endpoints marked dead)"}
			}
			p.advanceEndpoint()
		}
	}
	return types.LLMResponse{}, fmt.Errorf("openai: exhausted %d attempts: %w", attempts, lastErr)
}

// ─── ChatStream (streaming) ───

// ChatStream implements llm.LLMProvider. POSTs with stream:true and returns
// the raw SSE body for the caller to parse. The returned ReadCloser MUST be
// closed by the caller.
func (p *Provider) ChatStream(ctx context.Context, key string, req types.LLMRequest) (io.ReadCloser, error) {
	body := buildRequest(req, true)
	normalizeToolInput(body.Messages)
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("openai: marshal stream request: %w", err)
	}
	if !p.ensureHealthyEndpoint() {
		return nil, &Error{HTTPStatus: 503, Retryable: true, Source: "breaker",
			Message: "openai: endpoint breaker open (all upstream endpoints marked dead)"}
	}
	return p.doStreamWithRetry(ctx, key, payload, req.AgentClassName)
}

// ChatStreamAccumulate is the high-level streaming wrapper (same shape as
// anthropic.Provider.ChatStreamAccumulate). run_llm.go type-asserts any
// provider exposing this signature, so implementing it lets the Agent stream
// through openai models without any agent-side change. onProgress receives the
// synthesized StreamEvent sequence (§2.7).
func (p *Provider) ChatStreamAccumulate(ctx context.Context, key string, req types.LLMRequest, onProgress func(types.StreamEvent) error) (types.LLMResponse, error) {
	body, err := p.ChatStream(ctx, key, req)
	if err != nil {
		return types.LLMResponse{}, err
	}
	defer body.Close()
	return AccumulateStream(body, onProgress)
}

// ─── HTTP plumbing ───

func (p *Provider) doRequest(ctx context.Context, key string, payload []byte, agentClassName string) ([]byte, int, error) {
	endpoint := p.activeEndpoint()
	url := ChatCompletionsURL(endpoint)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, 0, err
	}
	applyHeaders(req, key, false, p.userAgentFor(agentClassName))
	client := p.httpClient
	resp, err := client.Do(req)
	if err != nil {
		p.noteTransportFailure(ctx, endpoint, err)
		return nil, 0, &Error{HTTPStatus: 0, Retryable: true, Source: "transport", Message: err.Error()}
	}
	defer resp.Body.Close()
	respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if readErr != nil {
		p.noteTransportFailure(ctx, endpoint, readErr)
		return nil, resp.StatusCode, &Error{HTTPStatus: resp.StatusCode, Retryable: true, Source: "transport", Message: readErr.Error()}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		retryable := resp.StatusCode == 429 || (resp.StatusCode >= 500 && resp.StatusCode < 600)
		if resp.StatusCode == 503 {
			p.recordEndpointFailure(endpoint)
		}
		msg := decodeChatError(respBody)
		return nil, resp.StatusCode, &Error{HTTPStatus: resp.StatusCode, Retryable: retryable, Source: "http", Message: msg}
	}
	p.recordSuccess(endpoint)
	return respBody, resp.StatusCode, nil
}

func (p *Provider) doStream(ctx context.Context, key string, payload []byte, agentClassName string) (io.ReadCloser, error) {
	endpoint := p.activeEndpoint()
	url := ChatCompletionsURL(endpoint)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	applyHeaders(req, key, true, p.userAgentFor(agentClassName))
	client := p.streamClient // no whole-request timeout
	resp, err := client.Do(req)
	if err != nil {
		p.noteTransportFailure(ctx, endpoint, err)
		return nil, &Error{HTTPStatus: 0, Retryable: true, Source: "transport", Message: err.Error()}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		retryable := resp.StatusCode == 429 || (resp.StatusCode >= 500 && resp.StatusCode < 600)
		if resp.StatusCode == 503 {
			p.recordEndpointFailure(endpoint)
		}
		msg := decodeChatError(respBody)
		return nil, &Error{HTTPStatus: resp.StatusCode, Retryable: retryable, Source: "http", Message: msg}
	}
	p.recordSuccess(endpoint)
	firstByte := p.streamFirstByteTimeout
	if firstByte <= 0 {
		firstByte = 120 * time.Second
	}
	return newIdleTimeoutReader(resp.Body, firstByte, p.streamIdleTimeout), nil
}

func (p *Provider) doStreamWithRetry(ctx context.Context, key string, payload []byte, agentClassName string) (io.ReadCloser, error) {
	var lastErr error
	for attempt := 0; attempt <= p.maxRetries; attempt++ {
		body, err := p.doStream(ctx, key, payload, agentClassName)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if ae, ok := err.(*Error); ok && !ae.Retryable {
			return nil, err
		}
		if attempt == p.maxRetries {
			break
		}
		backoff := time.Duration(attempt+1) * time.Second
		if backoff > 4*time.Second {
			backoff = 4 * time.Second
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
		if p.breakerOpenAny() {
			return nil, &Error{HTTPStatus: 503, Retryable: true, Source: "breaker",
				Message: "openai: endpoint breaker open mid-retry (all upstream endpoints marked dead)"}
		}
		p.advanceEndpoint()
	}
	return nil, fmt.Errorf("openai: exhausted %d attempts: %w", p.maxRetries+1, lastErr)
}

func applyHeaders(req *http.Request, key string, stream bool, ua string) {
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	if ua != "" {
		req.Header.Set("User-Agent", ua)
	}
}

func decodeChatError(body []byte) string {
	var ce chatError
	if err := json.Unmarshal(body, &ce); err == nil && ce.Error.Message != "" {
		return ce.Error.Message
	}
	return string(body)
}

// normalizeToolInput ensures every assistant tool_calls entry carries at least
// an "{}" arguments string, mirroring the anthropic tool_use.input→{} pre-flight
// (several OpenAI-compatible gateways reject missing arguments).
func normalizeToolInput(msgs []chatMessage) {
	for i := range msgs {
		for j := range msgs[i].ToolCalls {
			if strings.TrimSpace(msgs[i].ToolCalls[j].Function.Arguments) == "" {
				msgs[i].ToolCalls[j].Function.Arguments = "{}"
			}
		}
	}
}

// ─── endpoint failover + breaker ───

func (p *Provider) activeEndpoint() string {
	p.endpointMu.RLock()
	defer p.endpointMu.RUnlock()
	if p.activeIdx < len(p.endpoints) {
		return p.endpoints[p.activeIdx]
	}
	return ""
}

func (p *Provider) advanceEndpoint() {
	p.endpointMu.Lock()
	defer p.endpointMu.Unlock()
	if len(p.endpoints) <= 1 {
		return
	}
	p.activeIdx = (p.activeIdx + 1) % len(p.endpoints)
}

// ensureHealthyEndpoint reports whether the CURRENT endpoint's breaker is
// closed. Returns true when no endpoint is configured (let the dial fail fast).
func (p *Provider) ensureHealthyEndpoint() bool {
	ep := p.activeEndpoint()
	if ep == "" {
		return true
	}
	p.breakerMu.Lock()
	defer p.breakerMu.Unlock()
	till, ok := p.breakerOpen[ep]
	if !ok || till.IsZero() {
		return true
	}
	if time.Now().After(till) {
		delete(p.breakerOpen, ep)
		p.breakerWindow[ep] = p.breakerWindow[ep][:0]
		return true
	}
	return false
}

// breakerOpenAny reports whether EVERY endpoint currently has an open breaker.
func (p *Provider) breakerOpenAny() bool {
	p.breakerMu.Lock()
	defer p.breakerMu.Unlock()
	if len(p.endpoints) == 0 {
		return false
	}
	now := time.Now()
	for _, ep := range p.endpoints {
		if ep == "" {
			continue
		}
		if till, ok := p.breakerOpen[ep]; !ok || till.IsZero() || now.After(till) {
			return false
		}
	}
	return true
}

// noteTransportFailure records a transport-level failure against an endpoint,
// but NOT when the caller's own context canceled (that's the agent giving up,
// not the endpoint being dead — mirrors anthropic's behavior).
func (p *Provider) noteTransportFailure(ctx context.Context, endpoint string, err error) {
	if err == nil {
		return
	}
	if ctx.Err() != nil {
		return // caller canceled — don't blame the endpoint
	}
	p.recordEndpointFailure(endpoint)
}

func (p *Provider) recordEndpointFailure(endpoint string) {
	if endpoint == "" {
		return
	}
	p.breakerMu.Lock()
	defer p.breakerMu.Unlock()
	now := time.Now()
	cutoff := now.Add(-breakerWindowDuration)
	kept := p.breakerWindow[endpoint][:0]
	for _, t := range p.breakerWindow[endpoint] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	kept = append(kept, now)
	p.breakerWindow[endpoint] = kept
	if len(kept) >= breakerThreshold {
		if till, ok := p.breakerOpen[endpoint]; !ok || till.IsZero() {
			p.breakerOpen[endpoint] = now.Add(breakerCooldown)
			logger.L().Warn("openai: endpoint breaker OPEN",
				zap.String("endpoint", endpoint),
				zap.Int("failures_in_window", len(kept)),
				zap.Duration("cooldown", breakerCooldown))
		}
	}
}

func (p *Provider) recordSuccess(endpoint string) {
	if endpoint == "" {
		return
	}
	p.breakerMu.Lock()
	defer p.breakerMu.Unlock()
	p.breakerWindow[endpoint] = p.breakerWindow[endpoint][:0]
}

// ─── idle-timeout reader (shared with the rest of the package) ───

type readResult struct {
	n   int
	err error
}

// idleTimeoutReader enforces the §130 two-phase streaming timeout policy:
// firstByteTimeout caps the wait for the first byte; idle (forced to 0) leaves
// the live stream unbounded. Race-timer-per-Read, no persistent goroutine.
type idleTimeoutReader struct {
	r                io.ReadCloser
	firstByteTimeout time.Duration
	idle             time.Duration
	gotFirstByte     bool
	mu               sync.Mutex
	fired            bool
	lastErr          error
}

func newIdleTimeoutReader(r io.ReadCloser, firstByteTimeout, idle time.Duration) *idleTimeoutReader {
	return &idleTimeoutReader{r: r, firstByteTimeout: firstByteTimeout, idle: idle}
}

func (rd *idleTimeoutReader) Read(p []byte) (int, error) {
	rd.mu.Lock()
	if rd.fired {
		err := rd.lastErr
		rd.mu.Unlock()
		return 0, err
	}
	gotFirst := rd.gotFirstByte
	rd.mu.Unlock()

	deadline := rd.idle
	if !gotFirst {
		deadline = rd.firstByteTimeout
	}
	if deadline <= 0 {
		n, err := rd.r.Read(p)
		if n > 0 {
			rd.mu.Lock()
			rd.gotFirstByte = true
			rd.mu.Unlock()
		}
		return n, err
	}
	timer := time.NewTimer(deadline)
	defer timer.Stop()
	ch := make(chan readResult, 1)
	go func() {
		n, err := rd.r.Read(p)
		ch <- readResult{n: n, err: err}
	}()
	select {
	case res := <-ch:
		if res.n > 0 {
			rd.mu.Lock()
			rd.gotFirstByte = true
			rd.mu.Unlock()
		}
		return res.n, res.err
	case <-timer.C:
		phase := "first-byte"
		if gotFirst {
			phase = "inter-chunk-idle"
		}
		rd.mu.Lock()
		rd.fired = true
		rd.lastErr = &Error{HTTPStatus: 0, Retryable: true, Source: "stream",
			Message: fmt.Sprintf("openai: stream %s timeout after %s", phase, deadline)}
		lastErr := rd.lastErr
		rd.mu.Unlock()
		_ = rd.r.Close()
		<-ch
		return 0, lastErr
	}
}

func (rd *idleTimeoutReader) Close() error { return rd.r.Close() }
