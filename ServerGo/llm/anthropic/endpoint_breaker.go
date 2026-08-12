// endpoint_breaker.go —— 端点级可用性熔断 + failover 端点选择。
//
// 本文件从 anthropic.go 拆出(§4 单文件 ≤1800 行),聚合三件事:
//
//  1. 端点选择:activeEndpoint / advanceEndpoint / ensureHealthyEndpoint。
//  2. 端点熔断:breakerOpen / breakerOpenFor / breakerOpenAny /
//     recordEndpointFailure* / recordSuccess。
//  3. 传输层失败分类:isCallerCancellation / isTransportFailure —— 判定一次
//     client.Do 错误究竟是"端点不可达"还是"调用方自己放弃了"。
//
// BUG-R214 / BUG-R220 / BUG-R221(2026-08-01 修复):
//
//	原实现只在 HTTP 503 响应时记账(record503),而一台**彻底不可达**的主机
//	根本不会返回 503 —— 它返回 dial error(connection refused / i/o timeout /
//	no such host),`client.Do` 的错误分支只是原样包成
//	*Error{HTTPStatus:0, Retryable:true} 就返回,熔断器**永远不会打开**。
//	后果:13 个 bot 的每一次唤醒都要付满一整轮 dial 超时(30s DialTimeout /
//	120s ResponseHeaderTimeout / 600s Client.Timeout),而且永远不会停 ——
//	报告 20260801_061438 / 20260801_083235 中"15 小时挂起"的直接成因。
//
//	修复:熔断器改为统计**端点级不可用**(dial / connect / DNS / i-o timeout /
//	connection refused / EOF / reset)而不再只统计 HTTP 503,并在非流式与
//	流式**两条** client.Do 错误路径上都记账。滑动窗口 + 冷却常量与语义完全
//	不变(窗口内 N 次失败 → 打开 cooldown 时长)。

package anthropic

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"syscall"
	"time"

	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// 端点失败原因标签。仅用于日志/诊断,不参与任何判定逻辑。
const (
	failReasonHTTP503   = "http_503"   // 上游返回硬 503
	failReasonStream503 = "stream_503" // SSE body 中途报 503 / All endpoints failed
	failReasonTransport = "transport"  // dial / DNS / reset / i-o timeout 等传输层失败
)

// activeEndpoint returns the currently-selected endpoint URL. Safe for
// concurrent use (RLock; the active index is only mutated under Lock).
func (p *Provider) activeEndpoint() string {
	p.endpointMu.RLock()
	defer p.endpointMu.RUnlock()
	if len(p.endpoints) == 0 {
		return ""
	}
	if p.activeIdx < 0 || p.activeIdx >= len(p.endpoints) {
		return p.endpoints[0]
	}
	return p.endpoints[p.activeIdx]
}

// advanceEndpoint moves activeIdx to the next endpoint whose breaker is
// not currently open. Returns true if a non-tripped endpoint was found,
// false when every endpoint is breaker-tripped (caller should
// short-circuit with *Error{Source:"breaker"}).
func (p *Provider) advanceEndpoint() bool {
	p.endpointMu.Lock()
	defer p.endpointMu.Unlock()
	n := len(p.endpoints)
	if n <= 1 {
		return false
	}
	// Try each subsequent endpoint at most once per advance call. If the
	// chain is fully breaker-tripped we return false and the caller
	// surfaces the short-circuit error.
	for step := 1; step < n; step++ {
		next := (p.activeIdx + step) % n
		if !p.breakerOpenFor(p.endpoints[next]) {
			p.activeIdx = next
			return true
		}
	}
	return false
}

// ensureHealthyEndpoint 是 Chat / ChatStream 共用的"取一个可用端点"入口
// (BUG-R221 —— 修复前 ChatStream 只查 breakerOpen 就直接返回,从不尝试
// advanceEndpoint,导致主端点熔断时即便配置了健康的备用端点,流式路径也
// 一起阵亡;Chat 早已是"先切再判"的正确形状,这里把两条路径对齐)。
//
// 返回 true 表示当前 active 端点可用(可能刚被切换过);返回 false 表示
// **全部端点均已熔断**,调用方应立刻用 *Error{Source:"breaker"} 短路。
func (p *Provider) ensureHealthyEndpoint() bool {
	if !p.breakerOpen() {
		return true // 当前端点未熔断,直接用
	}
	if p.advanceEndpoint() {
		return true // 已切到一个未熔断的备用端点
	}
	// advanceEndpoint 返回 false 有两种情形:单端点(n<=1)、以及多端点但
	// 全部熔断。breakerOpenAny() 是"全端点皆死"的权威判定 —— 两种情形下
	// 它都返回 true,取反即"仍有健康端点"。用它做最终裁决而不是直接
	// return false,既保证语义精确,也让 breakerOpenAny 成为真实接线的
	// 判定点而非"声明了却从不调用"的死代码(§130 教训)。
	return !p.breakerOpenAny()
}

// breakerOpen reports whether the endpoint-availability breaker is currently
// open for the currently-active endpoint. When open, callers should
// short-circuit with *Error{Retryable:true, Source:"breaker"} so the agent's
// auto-skip path fires without further delay.
//
// BUG-R220: with multiple endpoints, the breaker is keyed by endpoint URL
// so a tripped primary doesn't poison the secondary. If the active
// endpoint is open, advanceEndpoint() picks the next un-tripped one;
// if ALL endpoints are open, breakerOpenAny() returns true and the caller
// short-circuits.
func (p *Provider) breakerOpen() bool {
	ep := p.activeEndpoint()
	return p.breakerOpenFor(ep)
}

// breakerOpenAny reports whether EVERY configured endpoint currently has
// its breaker open. This is the all-endpoints-dead determination used by
// ensureHealthyEndpoint (short-circuit before the dial) and by the Chat /
// doStreamWithRetry retry loops (bail out early instead of burning the
// remaining attempts on a chain that is entirely known-dead).
func (p *Provider) breakerOpenAny() bool {
	p.breakerMu.Lock()
	defer p.breakerMu.Unlock()
	now := time.Now()
	for _, ep := range p.endpoints {
		till := p.breakerOpenTill[ep]
		if till.IsZero() || now.After(till) {
			return false
		}
	}
	return true
}

// breakerOpenFor is the endpoint-keyed breaker query. Returns true when
// the breaker for `endpoint` is currently tripped (cooldown not yet
// expired). Caller must NOT hold p.breakerMu.
func (p *Provider) breakerOpenFor(endpoint string) bool {
	if endpoint == "" {
		return false
	}
	p.breakerMu.Lock()
	defer p.breakerMu.Unlock()
	till, ok := p.breakerOpenTill[endpoint]
	if !ok || till.IsZero() {
		return false
	}
	if time.Now().After(till) {
		// Cooldown elapsed: clear the window + open-till so a fresh stream
		// of failures can re-trip the breaker from a clean slate.
		p.breakerOpenTill[endpoint] = time.Time{}
		p.breakerWindow[endpoint] = p.breakerWindow[endpoint][:0]
		return false
	}
	return true
}

// recordEndpointFailure records ONE endpoint-level unavailability event for
// the currently-active endpoint. Thin wrapper over recordEndpointFailureFor
// for callers that don't hold a captured endpoint (the SSE mid-stream 503
// path in stream.go).
func (p *Provider) recordEndpointFailure(reason string) {
	p.recordEndpointFailureFor(p.activeEndpoint(), reason)
}

// recordEndpointFailureFor records ONE endpoint-level unavailability event
// against `endpoint` and trips that endpoint's breaker when the count within
// the rolling window crosses the threshold. Safe for concurrent use.
//
// BUG-R214: 调用方必须传入**本次实际拨号的**端点而不是 p.activeEndpoint(),
// 因为并发 goroutine 可能已经把 activeIdx 切走 —— 记到错误的端点头上会同时
// 造成"死端点永不熔断"与"健康端点被误熔断"两种后果。
func (p *Provider) recordEndpointFailureFor(endpoint, reason string) {
	if endpoint == "" {
		return
	}
	p.breakerMu.Lock()
	defer p.breakerMu.Unlock()
	now := time.Now()
	// Prune timestamps outside the rolling window.
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
		if till := p.breakerOpenTill[endpoint]; till.IsZero() || now.After(till) {
			p.breakerOpenTill[endpoint] = now.Add(breakerCooldown)
			// 只记端点 URL 与原因标签 —— 绝不记录 api_key / 请求体(§5)。
			logger.L().Warn("anthropic: endpoint breaker OPEN (BUG-R214)",
				zap.String("endpoint", endpoint),
				zap.String("reason", reason),
				zap.Int("failures_in_window", len(kept)),
				zap.Duration("window", breakerWindowDuration),
				zap.Duration("cooldown", breakerCooldown))
		} else {
			p.breakerOpenTill[endpoint] = now.Add(breakerCooldown)
		}
	}
}

// recordSuccess clears the breaker window for the active endpoint so a
// recovered endpoint doesn't immediately re-trip on the next failure.
// Called from successful Chat / ChatStream paths.
func (p *Provider) recordSuccess() {
	p.recordSuccessFor(p.activeEndpoint())
}

// recordSuccessFor clears the breaker window for a specific endpoint.
func (p *Provider) recordSuccessFor(endpoint string) {
	if endpoint == "" {
		return
	}
	p.breakerMu.Lock()
	defer p.breakerMu.Unlock()
	p.breakerWindow[endpoint] = p.breakerWindow[endpoint][:0]
}

// isCallerCancellation 判定一次 client.Do 失败是否源自**调用方自己的
// context** 被取消 / 超时,而不是端点不可达。
//
// BUG-R214 关键区分:Agent 放弃等待(房间关闭 / phase watchdog 取消 /
// 外层 ctx deadline)与"这台主机死了"是两回事。前者若计入熔断,一个正常
// 但慢的端点会被大量取消事件误判为不可用,反过来把健康代理熔断掉。
//
// 判据是 **ctx.Err() != nil** —— 只有调用方传进来的 ctx 真的处于 Done 状态
// 时才算调用方取消。注意 http.Client.Timeout 触发时 errors.Is(err,
// context.DeadlineExceeded) 也为 true,但那时 ctx.Err() 仍为 nil,因此会被
// 正确地归类为端点级失败(上游确实没在预算内响应)。
func isCallerCancellation(ctx context.Context, err error) bool {
	if ctx != nil && ctx.Err() != nil {
		return true
	}
	// ctx 未 Done 但错误链里就是 context.Canceled:通常是上层 CancelFunc 在
	// 传输层生效后才反映到 ctx(极短竞态窗口),同样算调用方取消。
	return errors.Is(err, context.Canceled)
}

// isTransportFailure 判定一次 client.Do 错误是否为**端点级不可用**:
// dial / connect / DNS / TLS 握手 / i-o timeout / connection refused /
// connection reset / EOF / broken pipe / 已关闭连接。
//
// client.Do 返回的错误几乎总是 *url.Error(它实现了 net.Error),因此这里
// 先做类型断言快路径,再对少数被代理库包装成纯 errors.New 的情况做小写
// 子串兜底。调用方必须**先**用 isCallerCancellation 排除调用方取消。
func isTransportFailure(err error) bool {
	if err == nil {
		return false
	}
	// 类型化快路径:net 包的三类结构化错误 + net.Error(含 *url.Error)。
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.EHOSTUNREACH) ||
		errors.Is(err, syscall.ENETUNREACH) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	// 字符串兜底:部分代理 / HTTP2 栈把底层错误重新包装成不可断言的类型。
	// "first-byte timeout" 是 idleTimeoutReader 自己产生的错误文案 —— 上游
	// 接受了连接却一个字节都没吐,与 dial 失败同属端点级不可用。注意
	// **不**匹配 "inter-chunk-idle timeout":那时流已经在正常产出,是慢模型
	// 而非死端点(§130/§197)。
	msg := strings.ToLower(err.Error())
	for _, m := range []string{
		"connection refused", "no such host", "i/o timeout", "connection reset",
		"network is unreachable", "host is unreachable", "tls handshake",
		"client.timeout", "broken pipe", "use of closed network connection",
		"server closed idle connection", "unexpected eof", "eof",
		"stream first-byte timeout",
	} {
		if strings.Contains(msg, m) {
			return true
		}
	}
	return false
}

// noteTransportFailure 是 doRequest / doStream 共用的记账入口:仅当错误
// 确实是端点级传输失败(而非调用方取消)时才把它计入 `endpoint` 的熔断
// 滑动窗口。返回值表示是否真的记了一笔,便于测试与日志断言。
func (p *Provider) noteTransportFailure(ctx context.Context, endpoint string, err error) bool {
	if isCallerCancellation(ctx, err) {
		return false
	}
	if !isTransportFailure(err) {
		return false
	}
	p.recordEndpointFailureFor(endpoint, failReasonTransport)
	return true
}
