// Package util — secure auth cookie helpers.
//
// The cookie we issue on /api/auth/login carries an AES-256-GCM encrypted
// payload of the form `user_id|issued_at|expires_at`. The payload is
// self-describing (no DB roundtrip needed on the common path) and is fully
// signature-verified by DecryptCookie.
//
// Wire format (base64-encoded):
//
//	nonce(12 bytes) || ciphertext || gcm_tag_in_tag_suffix
//
// AES-GCM internally appends the 16-byte tag to the ciphertext, so the
// payload is the concatenation above.
package util

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ErrCookieInvalid is returned when an auth cookie fails verification.
var ErrCookieInvalid = errors.New("cookie invalid")

// key32 derives a fixed 32-byte AES key from any secret string via SHA-256.
// We accept arbitrary-length secrets and never store the derived key.
func key32(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

// EncryptCookie returns base64(nonce || ciphertext+tag).
func EncryptCookie(plaintext, secret string) (string, error) {
	key := key32(secret)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize()) // 12 bytes
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	out := make([]byte, 0, len(nonce)+len(sealed))
	out = append(out, nonce...)
	out = append(out, sealed...)
	return base64.RawURLEncoding.EncodeToString(out), nil
}

// DecryptCookie parses and authenticates a value produced by EncryptCookie.
func DecryptCookie(token, secret string) (string, error) {
	if token == "" {
		return "", ErrCookieInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return "", ErrCookieInvalid
	}
	key := key32(secret)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", ErrCookieInvalid
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", ErrCookieInvalid
	}
	if len(raw) < gcm.NonceSize() {
		return "", ErrCookieInvalid
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", ErrCookieInvalid
	}
	return string(pt), nil
}

// BuildAuthCookie constructs an HttpOnly Secure SameSite=Strict cookie
// suitable for Set-Cookie. Pass secure=true for production (HTTPS).
func BuildAuthCookie(name, value string, ttl time.Duration, secure bool) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	}
}

// BuildClearCookie returns a cookie whose MaxAge is 0 — used by /api/auth/logout
// to instruct the browser to drop it.
func BuildClearCookie(name string, secure bool) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	}
}

// EncodeCookiePayload builds the canonical plaintext for the cookie body.
// It is deliberately a flat, parseable format so future middleware can
// refresh-expiry in place.
func EncodeCookiePayload(userID string, ttl time.Duration, now time.Time) string {
	exp := now.Add(ttl).Unix()
	return fmt.Sprintf("v1|%s|%d|%d", userID, now.Unix(), exp)
}

// ParseCookiePayload extracts (userID, expiresAt) from a payload produced by
// EncodeCookiePayload. Returns an error on malformed input.
func ParseCookiePayload(payload string) (string, time.Time, error) {
	parts := strings.Split(payload, "|")
	if len(parts) != 4 || parts[0] != "v1" {
		return "", time.Time{}, ErrCookieInvalid
	}
	userID := parts[1]
	var issuedAt, exp int64
	if _, err := fmt.Sscanf(parts[2], "%d", &issuedAt); err != nil {
		return "", time.Time{}, ErrCookieInvalid
	}
	if _, err := fmt.Sscanf(parts[3], "%d", &exp); err != nil {
		return "", time.Time{}, ErrCookieInvalid
	}
	if exp <= issuedAt {
		return "", time.Time{}, ErrCookieInvalid
	}
	return userID, time.Unix(exp, 0).UTC(), nil
}
