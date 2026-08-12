// Package util holds small helpers used across the project.
package util

import "golang.org/x/crypto/bcrypt"

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
