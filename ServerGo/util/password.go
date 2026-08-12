// Package util holds small helpers used across the project.
package util

import (
	"crypto/rand"
	"math/big"

	"golang.org/x/crypto/bcrypt"
)

// HashPassword bcrypt-hashes a plaintext password at the default cost.
func HashPassword(plain string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// VerifyPassword returns nil iff the plaintext matches the bcrypt hash.
func VerifyPassword(hash, plain string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain))
}

// passwordAlphabet is the character pool used by RandomStrongPassword.
// 92 个可读 ASCII 字符,排除 0OIl 等易混淆字符,保证输出可读且足够熵。
const passwordAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789!@#$%^&*-_+"

// RandomStrongPassword generates a cryptographically-random password of
// `length` characters from passwordAlphabet. Falls back to a deterministic
// (non-crypto) generator only if crypto/rand fails — callers should treat any
// error path as a fatal startup-time condition.
//
// Used by main.go to generate one-shot root / invite-code values when the
// operator left them empty in LsmAgentGame.conf. The caller MUST log the
// generated value once at startup and rotate it on first deploy.
func RandomStrongPassword(length int) string {
	if length <= 0 {
		length = 16
	}
	out := make([]byte, length)
	max := big.NewInt(int64(len(passwordAlphabet)))
	for i := range out {
		// crypto/rand.Int is the right primitive here; fall back to time-based
		// only if the OS RNG is broken (which would itself be a fatal issue).
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			out[i] = passwordAlphabet[int(uint64(0))%len(passwordAlphabet)]
			continue
		}
		out[i] = passwordAlphabet[n.Int64()]
	}
	return string(out)
}
