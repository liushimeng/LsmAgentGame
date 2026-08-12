package util

import "github.com/google/uuid"

// NewUUID returns a fresh RFC 4122 v4 UUID as a string.
func NewUUID() string {
	return uuid.NewString()
}
