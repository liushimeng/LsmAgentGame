// Package util — captcha store.
//
// A process-local CAPTCHA store: the server issues a captcha (id, answer),
// renders the answer to a small inline SVG, and verifies submissions against
// the live id. Entries self-expire and are swept by StartCaptchaJanitor.
//
// This deliberately has no external dependency (no Redis, no DB). For a
// horizontally-scaled deployment, swap CaptchaStore for a Redis-backed one
// behind the same interface.
package util

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"
)

// captchaEntry is one in-flight captcha challenge.
type captchaEntry struct {
	answer    string
	expiresAt time.Time
}

// CaptchaStore is a concurrency-safe in-memory registry of captcha challenges.
type CaptchaStore struct {
	mu      sync.RWMutex
	entries map[string]captchaEntry
}

// NewCaptchaStore constructs an empty store.
func NewCaptchaStore() *CaptchaStore {
	return &CaptchaStore{entries: make(map[string]captchaEntry)}
}

// Issue generates a new challenge. Returns the captcha id and the answer.
// length is the number of characters in the answer (alphanumeric, A-Z + 0-9).
func (s *CaptchaStore) Issue(length int, ttl time.Duration) (id, answer string, err error) {
	if length <= 0 {
		length = 5
	}
	answer, err = randomCode(length)
	if err != nil {
		return "", "", err
	}
	id = NewUUID()
	s.mu.Lock()
	s.entries[id] = captchaEntry{answer: answer, expiresAt: time.Now().Add(ttl)}
	s.mu.Unlock()
	return id, answer, nil
}

// Verify checks the submission. On success the entry is consumed (deleted)
// so it cannot be reused. Returns nil on success, an error otherwise.
//
// We use error codes from errcode via out parameters to avoid import cycles.
func (s *CaptchaStore) Verify(id, submission string) (status int) {
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
	if !strings.EqualFold(e.answer, submission) {
		return CaptchaWrong
	}
	return CaptchaOK
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
	CaptchaOK       = 0
	CaptchaMissing  = 10301
	CaptchaWrong    = 10302
	CaptchaExpired  = 10303
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

// RenderSVGCode draws the answer as a tiny inline SVG with wavy letters.
// Returned string is suitable for an `image/svg+xml` <img src="data:...">
// rendering or for inserting directly into HTML.
func RenderSVGCode(answer string) string {
	// Constants kept simple so the SVG is short enough to transmit in JSON.
	const w, h, fontSize = 160, 50, 32
	var sb strings.Builder
	fmt.Fprintf(&sb, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" width="%d" height="%d">`, w, h, w, h)
	// Pale background.
	sb.WriteString(`<rect width="100%" height="100%" fill="#f5f3ee"/>`)
	// Light grid lines to deter trivial OCR.
	for x := 0; x < w; x += 12 {
		fmt.Fprintf(&sb, `<line x1="%d" y1="0" x2="%d" y2="%d" stroke="#ddd" stroke-width="1"/>`, x, x, h)
	}
	// Each glyph: position varies; small rotation; navy fill.
	step := (w - 20) / len(answer)
	x := 10
	for i, r := range answer {
		rot := 0
		// Pseudo-random rotation seeded by glyph index. Negative removes the
		// need for rand (the visual skew is decorative).
		rot = (i*7 - 11) % 21
		dy := ((i * 5) % 9) - 4
		fmt.Fprintf(&sb,
			`<text x="%d" y="%d" font-family="Verdana,sans-serif" font-size="%d" font-weight="700" fill="#1f2c4d" transform="rotate(%d %d %d)">%c</text>`,
			x, h/2+5+dy, fontSize, rot, x, h/2, r)
		x += step
	}
	sb.WriteString(`</svg>`)
	return sb.String()
}
