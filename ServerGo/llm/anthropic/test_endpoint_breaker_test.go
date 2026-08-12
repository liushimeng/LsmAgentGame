// Package anthropic — BUG-R214 / BUG-R220 / BUG-R221 端点级熔断回归测试。
//
// 覆盖 4 类不变式(报告 20260801_061438 / 20260801_083235 中"不可达上游 →
// 15 小时挂起"的根因):
//
//	(a) 对不可达端点的重复 dial 失败,必须在窗口内打开熔断器;
//	(b) **调用方 ctx 取消**不得打开熔断器(那是 Agent 放弃,不是端点死);
//	(c) 2 端点、第一个死时,调用必须 failover 到第二个并成功;
//	(d) 全部端点死时,后续调用必须经熔断器**快速失败**,而不是再付一整轮
//	    dial 超时 —— 断言实际耗时(留足宽裕余量)。
//
// 所有测试只使用 httptest / 环回地址上的已关闭端口,绝不发起对生产上游
// (8.130.85.252)的真实网络调用。
package anthropic_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	anthropic "LsmWebGame/llm/anthropic"
	llmtypes "LsmWebGame/llm/types"
)

// deadEndpoint 返回一个"曾经监听、现已彻底关闭"的 http:// 端点 URL。
// 用 httptest 起一个服务再立刻关掉,拿到的端口在 Linux 环回地址上会立即
// 返回 ECONNREFUSED —— 这是"主机不可达"最快、最确定的本地模拟,不依赖
// 任何外部网络,也不会引入 dial 超时的墙钟等待。
func deadEndpoint(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	u := srv.URL
	srv.Close()
	return u
}

// blackholeEndpoint 返回一个"接受 TCP 连接但永不响应"的端点。用于模拟
// "dial 成功但上游卡死"的另一种不可达形态(生产上 ResponseHeaderTimeout
// 场景)。返回的 cleanup 必须在测试结束时调用。
func blackholeEndpoint(t *testing.T) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	done := make(chan struct{})
	go func() {
		for {
			conn, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			// 接受后什么都不做,连接一直挂着直到 cleanup。
			go func() { <-done; _ = conn.Close() }()
		}
	}()
	return "http://" + ln.Addr().String(), func() {
		close(done)
		_ = ln.Close()
	}
}

func smallReq() llmtypes.LLMRequest {
	return llmtypes.LLMRequest{Model: "x", MaxTokens: 1}
}

// TestR214_DialFailuresOpenBreaker (a) —— 对**不可达**端点的重复 dial 失败
// 必须在滑动窗口内把熔断器打开。这是 BUG-R214 的核心:修复前 record503 只
// 在 HTTP 503 响应分支被调用,而 dial error 走的是 `client.Do` 的 err!=nil
// 分支(HTTPStatus:0),熔断器**永远不会打开**。
func TestR214_DialFailuresOpenBreaker(t *testing.T) {
	dead := deadEndpoint(t)
	// maxRetries=0 → 一次 Chat = 一次 dial,便于精确计数到阈值。
	p := anthropic.New([]string{dead}, 2*time.Second, 0)

	// breakerThreshold = 3:前 3 次调用各记一笔传输层失败。
	for i := 0; i < 3; i++ {
		_, err := p.Chat(context.Background(), "k", smallReq())
		if err == nil {
			t.Fatalf("call %d: expected dial failure against a closed port", i)
		}
		var ae *anthropic.Error
		if errors.As(err, &ae) && ae.Source == "breaker" {
			t.Fatalf("call %d short-circuited too early (breaker opened before threshold)", i)
		}
	}

	// 第 4 次必须被熔断器短路。
	_, err := p.Chat(context.Background(), "k", smallReq())
	if err == nil {
		t.Fatal("post-threshold call: expected breaker short-circuit error")
	}
	var ae *anthropic.Error
	if !errors.As(err, &ae) {
		t.Fatalf("post-threshold call: expected *anthropic.Error, got %T: %v", err, err)
	}
	if ae.Source != "breaker" {
		t.Errorf("post-threshold call Source = %q, want %q — 传输层失败未计入熔断窗口(BUG-R214 回归)", ae.Source, "breaker")
	}
	if !ae.Retryable {
		t.Errorf("breaker error Retryable = false, want true(BUG-R89 级联防护:让 Agent 走 cooldown 窗口而非立刻 quarantine)")
	}
}

// TestR214_StreamDialFailuresOpenBreaker (a, 流式路径) —— 生产 Agent 走的是
// ChatStream,doStream 的 client.Do 错误分支同样必须记账。修复前它与
// doRequest 一样只是原样返回 *Error{HTTPStatus:0},熔断器不动。
func TestR214_StreamDialFailuresOpenBreaker(t *testing.T) {
	dead := deadEndpoint(t)
	p := anthropic.New([]string{dead}, 2*time.Second, 0)

	for i := 0; i < 3; i++ {
		body, err := p.ChatStream(context.Background(), "k", smallReq())
		if err == nil {
			body.Close()
			t.Fatalf("stream call %d: expected dial failure against a closed port", i)
		}
	}

	_, err := p.ChatStream(context.Background(), "k", smallReq())
	var ae *anthropic.Error
	if !errors.As(err, &ae) || ae.Source != "breaker" {
		t.Fatalf("post-threshold stream call: want Source=\"breaker\", got %v", err)
	}
}

// TestR214_CallerCancellationDoesNotOpenBreaker (b) —— **调用方**的 ctx 被
// 取消 / 超时是 Agent 放弃等待,而不是端点不可用。若把它计入熔断窗口,一个
// 正常但慢的端点会被大量取消事件误判为死,反而把健康代理熔断掉。
//
// 用一个"接受连接但永不响应"的黑洞端点 + 极短的 caller ctx 制造取消,
// 连打 6 次(threshold 的两倍),然后验证熔断器仍然关闭 —— 我们通过"下一次
// 调用是否真的又去拨号"来间接观测熔断状态。
func TestR214_CallerCancellationDoesNotOpenBreaker(t *testing.T) {
	blackhole, cleanup := blackholeEndpoint(t)
	defer cleanup()

	p := anthropic.New([]string{blackhole}, 30*time.Second, 0)

	for i := 0; i < 6; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
		_, err := p.Chat(ctx, "k", smallReq())
		cancel()
		if err == nil {
			t.Fatalf("call %d: expected caller-context cancellation error", i)
		}
		var ae *anthropic.Error
		if errors.As(err, &ae) && ae.Source == "breaker" {
			t.Fatalf("call %d: caller cancellation must NOT open the endpoint breaker (BUG-R214 误熔断)", i)
		}
	}

	// 决定性断言:熔断器若真的开了,这次调用会**立刻**以 Source="breaker"
	// 返回;熔断器关着的话,它会真的去拨号并挂到 caller ctx 超时。
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_, err := p.Chat(ctx, "k", smallReq())
	var ae *anthropic.Error
	if errors.As(err, &ae) && ae.Source == "breaker" {
		t.Fatalf("breaker opened purely from caller cancellations — 健康但慢的端点会被误熔断")
	}
}

// TestR214_FailoverToSecondWhenFirstUnreachable (c) —— 2 个端点、第一个彻底
// 不可达时,调用必须 failover 到第二个并成功返回。
func TestR214_FailoverToSecondWhenFirstUnreachable(t *testing.T) {
	dead := deadEndpoint(t)
	var aliveHits int32
	alive := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&aliveHits, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"m1","model":"x","role":"assistant","stop_reason":"end_turn","content":[],"usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer alive.Close()

	// maxRetries=1 → 第一次 attempt 打死端点,advanceEndpoint 后第二次
	// attempt 落到活端点。
	p := anthropic.New([]string{dead, alive.URL}, 3*time.Second, 1)

	resp, err := p.Chat(context.Background(), "k", smallReq())
	if err != nil {
		t.Fatalf("expected failover to the healthy secondary, got err: %v", err)
	}
	if resp.ID != "m1" {
		t.Errorf("resp.ID = %q, want %q (响应未来自健康备用端点)", resp.ID, "m1")
	}
	if got := atomic.LoadInt32(&aliveHits); got < 1 {
		t.Errorf("secondary endpoint was never dialed (hits=%d) — failover 未生效", got)
	}
}

// TestR221_ChatStreamFailsOverInsteadOfDying (c, BUG-R221) —— ChatStream 在
// 主端点熔断时必须像 Chat 一样先尝试 advanceEndpoint,而不是直接返回
// breaker 错误。修复前 ChatStream 只查 breakerOpen() 就 return,主端点一
// 熔断,即便配置了健康备用端点,整条流式路径(生产 Agent 走的正是它)也
// 一起阵亡。
func TestR221_ChatStreamFailsOverInsteadOfDying(t *testing.T) {
	dead := deadEndpoint(t)
	var aliveHits int32
	alive := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&aliveHits, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer alive.Close()

	p := anthropic.New([]string{dead, alive.URL}, 3*time.Second, 0)

	// 先把主端点的熔断器打满(3 次传输层失败)。maxRetries=0 保证每次
	// Chat 只拨一次号,且失败后不会 advance(attempt+1 < attempts 不成立)。
	for i := 0; i < 3; i++ {
		_, _ = p.Chat(context.Background(), "k", smallReq())
	}

	// 现在主端点熔断、备用端点健康。ChatStream 必须切过去并成功。
	body, err := p.ChatStream(context.Background(), "k", smallReq())
	if err != nil {
		t.Fatalf("ChatStream should have failed over to the healthy secondary, got: %v (BUG-R221 回归:ChatStream 未调用 advanceEndpoint)", err)
	}
	defer body.Close()
	if got := atomic.LoadInt32(&aliveHits); got < 1 {
		t.Errorf("secondary endpoint was never dialed by ChatStream (hits=%d)", got)
	}
}

// TestR214_AllEndpointsDeadFailsFast (d) —— 全部端点不可达时,熔断打开后的
// 调用必须**快速失败**,而不是再付一整轮 dial 超时。
//
// 用黑洞端点(接受连接后永不响应)让"未熔断"的调用真实地耗掉 ~600ms
// (provider timeout),对比熔断后的调用耗时。断言留足宽裕余量:熔断后单次
// 调用必须远快于一次真实拨号预算。
func TestR214_AllEndpointsDeadFailsFast(t *testing.T) {
	bh1, c1 := blackholeEndpoint(t)
	defer c1()
	bh2, c2 := blackholeEndpoint(t)
	defer c2()

	const callBudget = 600 * time.Millisecond
	p := anthropic.New([]string{bh1, bh2}, callBudget, 0)

	// 打满两个端点的熔断窗口:每端点各 3 次。maxRetries=0 时失败后不会
	// advance,所以显式交替 —— 先把 bh1 打死,advance 会自动带到 bh2。
	deadline := time.Now().Add(20 * time.Second)
	for i := 0; i < 12 && time.Now().Before(deadline); i++ {
		_, err := p.Chat(context.Background(), "k", smallReq())
		var ae *anthropic.Error
		if errors.As(err, &ae) && ae.Source == "breaker" {
			break // 两个端点都已熔断
		}
	}

	// 快速失败断言。真实拨号至少要 callBudget(600ms);熔断短路是纯内存
	// 判定,给 150ms 的宽裕上限。
	start := time.Now()
	_, err := p.Chat(context.Background(), "k", smallReq())
	elapsed := time.Since(start)

	var ae *anthropic.Error
	if !errors.As(err, &ae) || ae.Source != "breaker" {
		t.Fatalf("all-endpoints-dead call: want Source=\"breaker\", got %v", err)
	}
	if elapsed >= callBudget {
		t.Errorf("all-endpoints-dead call took %v (>= 单次拨号预算 %v) — 熔断器未做到快速失败,13 bot 房间会重演 BUG-R214 挂起", elapsed, callBudget)
	}
	if elapsed > 150*time.Millisecond {
		t.Errorf("breaker short-circuit took %v, want < 150ms(纯内存判定不应有任何网络等待)", elapsed)
	}
}

// TestR214_SuccessClearsTransportFailureWindow —— 传输层失败累积到阈值以下
// 时,一次成功响应必须清空窗口,避免历史瞬断(网络抖动)慢慢积累成误熔断。
//
// 用一个可切换模式的 httptest server:mode=0 时 hijack 连接并直接关闭
// (客户端侧表现为 EOF / connection reset,即传输层失败),mode=1 时返回
// 正常 200 JSON。
func TestR214_SuccessClearsTransportFailureWindow(t *testing.T) {
	mode := int32(0) // 0=断连,1=正常
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.LoadInt32(&mode) == 0 {
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Errorf("ResponseWriter is not a Hijacker; cannot simulate a transport drop")
				return
			}
			conn, _, err := hj.Hijack()
			if err != nil {
				t.Errorf("hijack: %v", err)
				return
			}
			_ = conn.Close() // 不写任何响应 → 客户端拿到 EOF
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"m1","model":"x","role":"assistant","stop_reason":"end_turn","content":[],"usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()

	p := anthropic.New([]string{srv.URL}, 2*time.Second, 0)

	// 2 次传输层失败(低于阈值 3)。
	for i := 0; i < 2; i++ {
		if _, err := p.Chat(context.Background(), "k", smallReq()); err == nil {
			t.Fatalf("call %d: expected a transport drop error", i)
		}
	}

	// 一次成功 → 清空窗口。
	atomic.StoreInt32(&mode, 1)
	if _, err := p.Chat(context.Background(), "k", smallReq()); err != nil {
		t.Fatalf("healthy call: %v", err)
	}

	// 再来 2 次失败:若窗口未被清空,累计已达 4 次会误熔断。
	atomic.StoreInt32(&mode, 0)
	for i := 0; i < 2; i++ {
		_, err := p.Chat(context.Background(), "k", smallReq())
		var ae *anthropic.Error
		if errors.As(err, &ae) && ae.Source == "breaker" {
			t.Fatalf("post-recovery call %d was breaker-short-circuited — 成功响应未清空传输层失败窗口", i)
		}
	}
}
