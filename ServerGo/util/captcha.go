// Package util — captcha store.
//
// A process-local CAPTCHA store: the server issues a captcha (id, answer),
// renders the answer to a small inline SVG, and verifies submissions against
// the live id. Entries self-expire and are swept by StartCaptchaJanitor.
//
// This deliberately has no external dependency (no Redis, no DB). For a
// horizontally-scaled deployment, swap CaptchaStore for a Redis-backed one
// behind the same interface.
//
// 20260923-01 hardening (§4.6):
//   - IssueBound stores a sha256 of the issuing IP and enforces a pending
//     capacity (max_pending) with LRU eviction — old Issue() is preserved.
//   - VerifyWithIP enforces same-IP redemption when bind_ip is on and both
//     sides carry an IP; either side empty skips the binding (dev + tests).
//   - RenderSVGPuzzle replaces the deterministic letter wobble with
//     crypto/rand rotation/position/colour jitter, DOM-order scrambling,
//     XML numeric entities, decoy glyphs, bezier curves and noise dots.
//     RenderSVGCode keeps its old signature as a thin wrapper.
package util

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"
)

// captchaEntry is one in-flight captcha challenge.
type captchaEntry struct {
	answer     string
	expiresAt  time.Time
	ipHash     string    // sha256hex of the issuing IP ("" when unbound)
	lastAccess time.Time // issue time, refreshed on redemption — LRU key
}

// CaptchaStore is a concurrency-safe in-memory registry of captcha challenges.
type CaptchaStore struct {
	mu      sync.RWMutex
	entries map[string]captchaEntry

	// maxPending bounds the pending-entry count (security.captcha_guard).
	// 0 = unbounded (legacy default; unit tests never hit eviction).
	maxPending int
	// bindIP turns on same-IP redemption inside VerifyWithIP. The flag is
	// owned by the store so main.go wires security.captcha_guard.bind_ip
	// exactly once at startup.
	bindIP bool
}

// NewCaptchaStore constructs an empty store.
func NewCaptchaStore() *CaptchaStore {
	return &CaptchaStore{entries: make(map[string]captchaEntry)}
}

// SetMaxPending configures the pending-entry capacity (0 = unbounded).
// Called from main.go with security.captcha_guard.max_pending.
func (s *CaptchaStore) SetMaxPending(n int) {
	if n < 0 {
		n = 0
	}
	s.mu.Lock()
	s.maxPending = n
	s.mu.Unlock()
}

// SetBindIP toggles same-IP redemption enforcement in VerifyWithIP.
// Called from main.go with security.captcha_guard.bind_ip (and gated by
// security.enabled). Verify / VerifyWithIP(id, code, "") are unaffected.
func (s *CaptchaStore) SetBindIP(on bool) {
	s.mu.Lock()
	s.bindIP = on
	s.mu.Unlock()
}

// Issue generates a new challenge. Returns the captcha id and the answer.
// length is the number of characters in the answer (alphanumeric, A-Z + 0-9).
// The entry is unbound to any IP.
func (s *CaptchaStore) Issue(length int, ttl time.Duration) (id, answer string, err error) {
	return s.IssueBound(length, ttl, "")
}

// IssueBound is Issue with the §4.6 hardening: the entry records a sha256
// hash of the issuing IP (empty ip → unbound), and when the store is at its
// max_pending capacity the least-recently-accessed entries are evicted
// FIRST so a flood can never deny service to normal users.
func (s *CaptchaStore) IssueBound(length int, ttl time.Duration, ip string) (id, answer string, err error) {
	if length <= 0 {
		length = 5
	}
	answer, err = randomCode(length)
	if err != nil {
		return "", "", err
	}
	id = NewUUID()
	now := time.Now()
	s.mu.Lock()
	if s.maxPending > 0 {
		// Expired entries go first, then LRU-evict down to capacity - 1.
		for k, e := range s.entries {
			if now.After(e.expiresAt) {
				delete(s.entries, k)
			}
		}
		for len(s.entries) >= s.maxPending {
			victim := ""
			var oldest time.Time
			for k, e := range s.entries {
				if victim == "" || e.lastAccess.Before(oldest) {
					victim, oldest = k, e.lastAccess
				}
			}
			if victim == "" {
				break
			}
			delete(s.entries, victim)
		}
	}
	s.entries[id] = captchaEntry{
		answer:     answer,
		expiresAt:  now.Add(ttl),
		ipHash:     hashCaptchaIP(ip),
		lastAccess: now,
	}
	s.mu.Unlock()
	return id, answer, nil
}

// Verify checks the submission. On success the entry is consumed (deleted)
// so it cannot be reused. Returns nil on success, an error otherwise.
//
// We use error codes from errcode via out parameters to avoid import cycles.
func (s *CaptchaStore) Verify(id, submission string) (status int) {
	return s.VerifyWithIP(id, submission, "")
}

// VerifyWithIP is Verify with the §4.6 IP binding. When bind_ip is on and
// BOTH the issuing and the redeeming sides carry a non-empty IP, a hash
// mismatch is reported as CaptchaWrong. Either side empty → binding skipped
// (dev tooling, in-proc bots, unit tests all stay at zero impact).
func (s *CaptchaStore) VerifyWithIP(id, submission, ip string) (status int) {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == "" || submission == "" {
		return CaptchaMissing
	}
	e, ok := s.entries[id]
	if !ok {
		return CaptchaExpired
	}
	delete(s.entries, id) // single-use
	if now.After(e.expiresAt) {
		return CaptchaExpired
	}
	if s.bindIP && ip != "" && e.ipHash != "" && hashCaptchaIP(ip) != e.ipHash {
		return CaptchaWrong
	}
	if !strings.EqualFold(e.answer, submission) {
		return CaptchaWrong
	}
	return CaptchaOK
}

// hashCaptchaIP digests an IP for storage; "" stays "" (unbound).
func hashCaptchaIP(ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(ip))
	return hex.EncodeToString(sum[:])
}

// Janitor periodically purges expired entries. Call StartCaptchaJanitor in a
// long-lived goroutine; pass a channel that receives a struct{} to stop it.
func (s *CaptchaStore) Janitor(interval time.Duration, stop <-chan struct{}) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case now := <-t.C:
			s.mu.Lock()
			for id, e := range s.entries {
				if now.After(e.expiresAt) {
					delete(s.entries, id)
				}
			}
			s.mu.Unlock()
		}
	}
}

// Verify statuses (mirror errcode codes to avoid an import cycle here).
const (
	CaptchaOK      = 0
	CaptchaMissing = 10301
	CaptchaWrong   = 10302
	CaptchaExpired = 10303
)

// randomCode returns a crypto-random string of length n drawn from
// "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789". Excludes visually-confusing chars
// (0/O, 1/I/L) to keep CAPTCHA-solving feasible for humans.
func randomCode(n int) (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // 32 chars, no 0/1/I/O
	if n <= 0 {
		n = 5
	}
	var b strings.Builder
	max := big.NewInt(int64(len(alphabet)))
	for i := 0; i < n; i++ {
		idx, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b.WriteByte(alphabet[idx.Int64()])
	}
	return b.String(), nil
}

// ── SVG 反爬渲染 (20260923-01 §4.6) ──────────────────────────────────────
//
// Threat model: the old renderer wrote the answer in DOM order with a
// deterministic wobble, so `grep -oP '<text[^>]*>\K.'` recovered it. The
// puzzle renderer breaks every cheap heuristic:
//
//  1. glyph rotation (±18°), y-jitter, font size (±4px), colour slot and x
//     slot are all drawn from crypto/rand;
//  2. DOM order is a random permutation while x coordinates stay monotonic
//     (visual reading order is preserved for humans);
//  3. glyph characters are emitted as XML numeric entities (&#65;) — naive
//     regexes capture entity text, not letters;
//  4. `decoys` extra glyphs in a lighter palette (font size −20%) share the
//     same <text> markup — visually distinguishable, DOM-indistinguishable;
//  5. foreground: 4 random cubic bezier curves over the glyphs plus 60–100
//     noise dots; the old vertical grid became random diagonal lines.

const (
	puzzleW       = 200
	puzzleH       = 60
	puzzleBaseFS  = 30
	puzzleMarginX = 14
)

// puzzlePalette holds six dark fills — enough range that colour filtering
// cannot isolate glyphs, still readable on the pale background.
var puzzlePalette = []string{"#1f2c4d", "#16324f", "#243b2f", "#3b2430", "#2c2c2c", "#123a3a"}

// puzzleDecoyPalette is one shade lighter: decoys read as ghost letters.
var puzzleDecoyPalette = []string{"#b9c3d6", "#c2d0c6", "#d6c3c9", "#cfcfcf"}

// captchaGlyph is one rendered <text> node.
type captchaGlyph struct {
	r    rune
	x, y float64
	size int
	rot  int
	fill string
}

// RenderSVGCode draws the answer as a tiny inline SVG. Signature kept for
// compatibility (it predates the hardening); internally it delegates to the
// puzzle renderer without decoys.
func RenderSVGCode(answer string) string {
	return RenderSVGPuzzle(answer, 0)
}

// RenderSVGPuzzle renders the anti-scraping captcha SVG. decoys <= 0 adds no
// ghost glyphs; the config default is 2 (security captcha.decoys).
func RenderSVGPuzzle(answer string, decoys int) string {
	if answer == "" {
		answer = "A"
	}
	glyphs := make([]captchaGlyph, 0, len(answer)+decoys)

	// ── answer glyphs: monotonic visual x slots, random everything else ──
	runes := []rune(answer)
	n := len(runes)
	usable := float64(puzzleW - 2*puzzleMarginX)
	step := usable / float64(n)
	for i, r := range runes {
		slotJitter := 0.0
		if step > 8 {
			slotJitter = randFloat(-step/5, step/5)
		}
		x := float64(puzzleMarginX) + step*float64(i) + step/2 - 9 + slotJitter
		glyphs = append(glyphs, captchaGlyph{
			r:    r,
			x:    clampF(x, 2, float64(puzzleW-22)),
			y:    float64(puzzleH)/2 + 8 + randFloat(-6, 6),
			size: puzzleBaseFS + randIntN(9) - 4, // ±4px
			rot:  randIntN(37) - 18,              // ±18°
			fill: puzzlePalette[randIntN(len(puzzlePalette))],
		})
	}

	// ── decoy glyphs: lighter palette, −20% size, anywhere on canvas ──────
	for i := 0; i < decoys; i++ {
		glyphs = append(glyphs, captchaGlyph{
			r:    rune(puzzleAlphabetByte()),
			x:    randFloat(2, float64(puzzleW-24)),
			y:    randFloat(14, float64(puzzleH-6)),
			size: puzzleBaseFS*8/10 + randIntN(5) - 2,
			rot:  randIntN(37) - 18,
			fill: puzzleDecoyPalette[randIntN(len(puzzleDecoyPalette))],
		})
	}

	// ── DOM shuffle: visual order (x) unchanged, markup order scrambled ──
	for i := len(glyphs) - 1; i > 0; i-- {
		j := randIntN(i + 1)
		glyphs[i], glyphs[j] = glyphs[j], glyphs[i]
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" width="%d" height="%d">`,
		puzzleW, puzzleH, puzzleW, puzzleH)
	sb.WriteString(`<rect width="100%" height="100%" fill="#f5f3ee"/>`)
	// Background: diagonal random hairlines instead of the old vertical grid.
	for i := 0; i < 8; i++ {
		fmt.Fprintf(&sb, `<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="#e3e0d8" stroke-width="1"/>`,
			randIntN(puzzleW), randIntN(puzzleH), randIntN(puzzleW), randIntN(puzzleH))
	}
	// Glyphs (XML numeric entities defeat naive `<text>(.)</text>` regexes).
	for _, g := range glyphs {
		fmt.Fprintf(&sb,
			`<text x="%.1f" y="%.1f" font-family="Verdana,sans-serif" font-size="%d" font-weight="700" fill="%s" transform="rotate(%d %.1f %.1f)">&#%d;</text>`,
			g.x, g.y, g.size, g.fill, g.rot, g.x, g.y, g.r)
	}
	// Foreground interference: 4 cubic beziers pressed over the letters.
	for i := 0; i < 4; i++ {
		fmt.Fprintf(&sb,
			`<path d="M %d %d C %d %d %d %d %d %d" stroke="%s" stroke-width="%d" fill="none" opacity="0.55"/>`,
			randIntN(puzzleW), randIntN(puzzleH),
			randIntN(puzzleW), randIntN(puzzleH),
			randIntN(puzzleW), randIntN(puzzleH),
			randIntN(puzzleW), randIntN(puzzleH),
			puzzlePalette[randIntN(len(puzzlePalette))], 1+randIntN(2))
	}
	// 60–100 noise dots.
	for i, cnt := 0, 60+randIntN(41); i < cnt; i++ {
		fmt.Fprintf(&sb, `<circle cx="%d" cy="%d" r="1" fill="#9aa3b5" opacity="0.5"/>`,
			randIntN(puzzleW), randIntN(puzzleH))
	}
	sb.WriteString(`</svg>`)
	return sb.String()
}

// puzzleAlphabetByte returns one random captcha-alphabet byte for decoys.
func puzzleAlphabetByte() byte {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	return alphabet[randIntN(len(alphabet))]
}

// randIntN returns [0, n) via crypto/rand; 0 on entropy failure (the SVG
// degrades to a deterministic-but-still-scrambled-layout render, never a
// panic — captcha issuance itself uses randomCode which DOES surface errors).
func randIntN(n int) int {
	if n <= 0 {
		return 0
	}
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0
	}
	return int(v.Int64())
}

func randFloat(lo, hi float64) float64 {
	if hi <= lo {
		return lo
	}
	return lo + float64(randIntN(1000))/1000.0*(hi-lo)
}

func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
