package util

import (
	"fmt"
	"html"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCaptchaStore_IssueAndVerify(t *testing.T) {
	store := NewCaptchaStore()
	id, answer, err := store.Issue(5, 30*time.Second)
	if err != nil {
		t.Fatalf("Issue failed: %v", err)
	}
	if id == "" || len(answer) != 5 {
		t.Fatalf("unexpected issue result id=%q answer=%q", id, answer)
	}
	if status := store.Verify(id, answer); status != CaptchaOK {
		t.Fatalf("verify ok status: got %d want %d", status, CaptchaOK)
	}
	// Single-use: second verify on the same id must say expired/missing.
	if status := store.Verify(id, answer); status == CaptchaOK {
		t.Fatalf("captcha reused: expected non-OK, got OK")
	}
}

func TestCaptchaStore_RejectsWrongAnswer(t *testing.T) {
	store := NewCaptchaStore()
	id, _, _ := store.Issue(5, 30*time.Second)
	if status := store.Verify(id, "ZZZZZ"); status != CaptchaWrong {
		t.Fatalf("wrong answer: got %d want %d", status, CaptchaWrong)
	}
}

func TestCaptchaStore_RejectsExpired(t *testing.T) {
	store := NewCaptchaStore()
	id, answer, _ := store.Issue(5, 1*time.Millisecond)
	time.Sleep(20 * time.Millisecond)
	if status := store.Verify(id, answer); status != CaptchaExpired {
		t.Fatalf("expected expired, got %d", status)
	}
}

func TestCaptchaStore_JanitorPurgesExpired(t *testing.T) {
	store := NewCaptchaStore()
	id, _, _ := store.Issue(3, 1*time.Millisecond)
	stop := make(chan struct{})
	go store.Janitor(5*time.Millisecond, stop)
	time.Sleep(50 * time.Millisecond)
	// After Janitor sweep the id must be gone (returns expired/missing — both non-OK).
	if status := store.Verify(id, "ABC"); status == CaptchaOK {
		t.Fatalf("captcha should have been purged, got OK")
	}
	close(stop)
}

// ── 20260923-01 §4.6 加固单测 ─────────────────────────────────────────────
//
// 三块新增能力：IssueBound（IP 哈希 + max_pending 驱逐）、VerifyWithIP
// （bind_ip 且两侧 IP 非空才校验绑定）、RenderSVGPuzzle（反爬渲染）。

// pendingCount 读包内条目数（同包可见）。
func pendingCount(s *CaptchaStore) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entries)
}

func TestCaptchaStore_IssueBoundVerifyWithIP_MatchingIP(t *testing.T) {
	store := NewCaptchaStore()
	store.SetBindIP(true)
	id, answer, err := store.IssueBound(5, 30*time.Second, "203.0.113.7")
	if err != nil {
		t.Fatalf("IssueBound failed: %v", err)
	}
	if id == "" || len(answer) != 5 {
		t.Fatalf("unexpected issue result id=%q answer=%q", id, answer)
	}
	// 签发 IP 以 sha256 摘要存储，绝不明文落内存条目。
	e := store.entries[id]
	if e.ipHash == "203.0.113.7" || e.ipHash == "" {
		t.Fatalf("ipHash = %q, want a non-empty digest of the issuing IP", e.ipHash)
	}
	if len(e.ipHash) != 64 {
		t.Fatalf("ipHash = %q, want a 64-char sha256 hex digest", e.ipHash)
	}
	if status := store.VerifyWithIP(id, answer, "203.0.113.7"); status != CaptchaOK {
		t.Fatalf("same-IP redemption: got %d want %d", status, CaptchaOK)
	}
}

func TestCaptchaStore_VerifyWithIP_MismatchIsWrong(t *testing.T) {
	store := NewCaptchaStore()
	store.SetBindIP(true)
	id, answer, _ := store.IssueBound(5, 30*time.Second, "203.0.113.7")
	// 跨 IP 复用同一 captcha_id（方案 A3）→ 按 CaptchaWrong 处理。
	if status := store.VerifyWithIP(id, answer, "198.51.100.9"); status != CaptchaWrong {
		t.Fatalf("cross-IP redemption: got %d want %d (CaptchaWrong)", status, CaptchaWrong)
	}
	// 条目已单次消费（即使拒绝也删除），不可重放。
	if status := store.VerifyWithIP(id, answer, "203.0.113.7"); status == CaptchaOK {
		t.Fatal("captcha must be single-use even after an IP mismatch")
	}
}

func TestCaptchaStore_VerifyWithIP_EmptySideSkipsBinding(t *testing.T) {
	// ① 校验侧 IP 为空（进程内 bot / 单测）→ 跳过绑定。
	store := NewCaptchaStore()
	store.SetBindIP(true)
	id, answer, _ := store.IssueBound(5, 30*time.Second, "203.0.113.7")
	if status := store.VerifyWithIP(id, answer, ""); status != CaptchaOK {
		t.Fatalf("empty redeeming IP must skip binding, got %d", status)
	}

	// ② 签发侧 IP 为空（旧 Issue 路径）→ 跳过绑定。
	id2, answer2, _ := store.Issue(5, 30*time.Second)
	if status := store.VerifyWithIP(id2, answer2, "198.51.100.9"); status != CaptchaOK {
		t.Fatalf("unbound issue must skip binding, got %d", status)
	}

	// ③ 旧 Verify 委托 VerifyWithIP(…, "")：与加固前逐字节等价。
	id3, answer3, _ := store.IssueBound(5, 30*time.Second, "203.0.113.7")
	if status := store.Verify(id3, answer3); status != CaptchaOK {
		t.Fatalf("legacy Verify on a bound entry: got %d want %d", status, CaptchaOK)
	}
}

func TestCaptchaStore_BindIPDisabledIgnoresMismatch(t *testing.T) {
	store := NewCaptchaStore() // bindIP 默认 false（未接线时的旧行为）
	id, answer, _ := store.IssueBound(5, 30*time.Second, "203.0.113.7")
	if status := store.VerifyWithIP(id, answer, "198.51.100.9"); status != CaptchaOK {
		t.Fatalf("bind_ip=false must ignore the IP mismatch, got %d", status)
	}
	// SetBindIP(false) 显式关闭后同样放行。
	store.SetBindIP(false)
	id2, answer2, _ := store.IssueBound(5, 30*time.Second, "203.0.113.7")
	if status := store.VerifyWithIP(id2, answer2, "198.51.100.9"); status != CaptchaOK {
		t.Fatalf("explicit SetBindIP(false) must ignore the mismatch, got %d", status)
	}
}

func TestCaptchaStore_MaxPendingEvictsOldestWithoutDenying(t *testing.T) {
	store := NewCaptchaStore()
	store.SetMaxPending(3)
	var lastID, lastAnswer string
	for i := 0; i < 20; i++ {
		id, answer, err := store.IssueBound(4, 30*time.Second, "203.0.113.7")
		if err != nil {
			t.Fatalf("IssueBound #%d failed: %v", i, err)
		}
		lastID, lastAnswer = id, answer
		if n := pendingCount(store); n > 3 {
			t.Fatalf("pending entries = %d, want <= max_pending(3)", n)
		}
	}
	// 最新签发的验证码必须仍然可用 —— 洪泛只能驱逐旧条目，绝不拒绝正常用户。
	if status := store.VerifyWithIP(lastID, lastAnswer, "203.0.113.7"); status != CaptchaOK {
		t.Fatalf("newest captcha must survive eviction, got %d", status)
	}
}

func TestCaptchaStore_MaxPendingPurgesExpiredFirst(t *testing.T) {
	store := NewCaptchaStore()
	store.SetMaxPending(5)
	id, answer, _ := store.IssueBound(4, time.Millisecond, "")
	time.Sleep(5 * time.Millisecond)
	if _, _, err := store.IssueBound(4, 30*time.Second, ""); err != nil {
		t.Fatalf("IssueBound failed: %v", err)
	}
	if status := store.Verify(id, answer); status == CaptchaOK {
		t.Fatal("the expired entry must have been purged by the capacity sweep")
	}
}

func TestCaptchaStore_SetMaxPendingClampsNegative(t *testing.T) {
	store := NewCaptchaStore()
	store.SetMaxPending(-5)
	if store.maxPending != 0 {
		t.Fatalf("maxPending = %d, want 0 (unbounded) for a negative config value", store.maxPending)
	}
	for i := 0; i < 10; i++ {
		if _, _, err := store.IssueBound(4, 30*time.Second, ""); err != nil {
			t.Fatalf("IssueBound failed: %v", err)
		}
	}
	if n := pendingCount(store); n != 10 {
		t.Fatalf("pending = %d, want 10 (0 = unbounded)", n)
	}
}

func TestCaptchaStore_ConcurrentIssueBoundRespectsCap(t *testing.T) {
	store := NewCaptchaStore()
	// 容量远大于并发度（8）：本用例验证「并发签发 + 并发消费 + 容量驱逐
	// 扫描」在 -race 下无数据竞争、不 panic；确定性的驱逐断言由
	// TestCaptchaStore_MaxPendingEvictsOldestWithoutDenying 覆盖（那里容量
	// 故意小于签发数，且要求最新条目必须存活）。
	const maxPending = 64
	store.SetMaxPending(maxPending)
	store.SetBindIP(true)
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			ip := fmt.Sprintf("203.0.113.%d", n+1)
			for i := 0; i < 40; i++ {
				id, answer, err := store.IssueBound(4, 30*time.Second, ip)
				if err != nil {
					t.Errorf("IssueBound failed: %v", err)
					return
				}
				// 立刻自校验：并发消费与并发签发（含驱逐扫描）同时跑。
				if status := store.VerifyWithIP(id, answer, ip); status != CaptchaOK {
					t.Errorf("concurrent same-IP verify: got %d", status)
					return
				}
			}
		}(w)
	}
	wg.Wait()
	if n := pendingCount(store); n > maxPending {
		t.Fatalf("pending entries = %d after concurrent load, want <= %d", n, maxPending)
	}
}

// ── SVG 反爬渲染 ──────────────────────────────────────────────────────────

// textNodeRe 匹配 <text …>内容</text>（DOM 顺序）。
var textNodeRe = regexp.MustCompile(`<text[^>]*>(.*?)</text>`)

// glyphsInDOMOrder 按 DOM 顺序抽取字形并解码 XML 数字实体（&#65; → A）。
func glyphsInDOMOrder(svg string) string {
	var sb strings.Builder
	for _, m := range textNodeRe.FindAllStringSubmatch(svg, -1) {
		sb.WriteString(html.UnescapeString(m[1]))
	}
	return sb.String()
}

// naiveAnswerRe 是方案 A1 记录的攻击正则：旧渲染器把答案明文按 DOM 顺序
// 写进 <text>，一条正则即可拿答案。加固后它必须一无所获。
var naiveAnswerRe = regexp.MustCompile(`<text[^>]*>(.)</text>`)

func TestRenderSVGPuzzle_WellFormedAndGlyphsPresent(t *testing.T) {
	const answer = "ABCDEFGHJKLMNPQR"
	for _, decoys := range []int{0, 2, 5} {
		svg := RenderSVGPuzzle(answer, decoys)
		if !strings.HasPrefix(svg, `<svg xmlns="http://www.w3.org/2000/svg"`) {
			t.Fatalf("decoys=%d: missing svg root, got %.60s", decoys, svg)
		}
		if !strings.HasSuffix(svg, "</svg>") {
			t.Fatalf("decoys=%d: missing </svg> terminator", decoys)
		}
		// 字形总数 = 答案 + 干扰字符（decoy 与真字同 markup，DOM 不可分）。
		if got := len([]rune(glyphsInDOMOrder(svg))); got != len(answer)+decoys {
			t.Fatalf("decoys=%d: glyph count = %d, want %d", decoys, got, len(answer)+decoys)
		}
		// 每个答案字符都以 XML 数字实体形式出现（人眼/OCR 可读性保持不变）。
		for _, r := range answer {
			if !strings.Contains(svg, fmt.Sprintf("&#%d;", r)) {
				t.Fatalf("decoys=%d: glyph %q not emitted as a numeric entity", decoys, r)
			}
		}
		// 答案绝不以明文子串出现（旧渲染器的 A1 泄漏形态）。
		if strings.Contains(svg, answer) {
			t.Fatalf("decoys=%d: answer leaked as plaintext into the SVG", decoys)
		}
	}
}

func TestRenderSVGPuzzle_DOMOrderDoesNotLeakAnswer(t *testing.T) {
	// 16 个字符 ⇒ 恒等排列概率 1/16! ≈ 5e-14；32 轮抽样足以捕获「忘记乱序」
	// 这类回归，且不会 flaky。
	const answer = "ABCDEFGHJKLMNPQR"
	for i := 0; i < 32; i++ {
		svg := RenderSVGPuzzle(answer, 2)
		if got := glyphsInDOMOrder(svg); got == answer {
			t.Fatalf("round %d: DOM order concatenated to the answer (%q) — scrambling is broken", i, got)
		}
		// A1 回归锚点：朴素单字符正则必须匹配不到任何节点。
		if m := naiveAnswerRe.FindAllStringSubmatch(svg, -1); len(m) != 0 {
			t.Fatalf("round %d: naive `<text>(.)</text>` regex matched %d nodes — glyphs must be entity-encoded", i, len(m))
		}
	}
}

func TestRenderSVGPuzzle_IsNondeterministic(t *testing.T) {
	const answer = "ABCDE"
	for _, decoys := range []int{0, 2} {
		seen := make(map[string]struct{}, 8)
		for i := 0; i < 8; i++ {
			svg := RenderSVGPuzzle(answer, decoys)
			if _, dup := seen[svg]; dup {
				t.Fatalf("decoys=%d: identical SVG rendered twice — crypto/rand jitter is not wired", decoys)
			}
			seen[svg] = struct{}{}
		}
	}
}

func TestRenderSVGCode_BackCompatWrapper(t *testing.T) {
	const answer = "ABCDE"
	svg := RenderSVGCode(answer)
	if !strings.HasPrefix(svg, "<svg") || !strings.HasSuffix(svg, "</svg>") {
		t.Fatalf("RenderSVGCode must still emit an svg document, got %.60s", svg)
	}
	// 旧签名 = 无 decoy 的 puzzle 渲染（字形数 == 答案长度）。
	if got := len([]rune(glyphsInDOMOrder(svg))); got != len(answer) {
		t.Fatalf("RenderSVGCode glyph count = %d, want %d (no decoys)", got, len(answer))
	}
	if svg == RenderSVGCode(answer) {
		t.Fatal("RenderSVGCode must stay non-deterministic (it delegates to the puzzle renderer)")
	}
}

func TestRenderSVGPuzzle_EmptyAnswerDoesNotPanic(t *testing.T) {
	svg := RenderSVGPuzzle("", 2)
	if !strings.HasPrefix(svg, "<svg") || !strings.HasSuffix(svg, "</svg>") {
		t.Fatalf("empty answer must still render a document, got %.60s", svg)
	}
	if got := len([]rune(glyphsInDOMOrder(svg))); got != 3 { // 1 兜底字形 + 2 decoys
		t.Fatalf("glyph count = %d, want 3 (fallback glyph + decoys)", got)
	}
}
