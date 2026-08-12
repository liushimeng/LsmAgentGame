package util

import (
	"crypto/rand"
	"math/big"
)

// inviteAlphabet is the set of characters used to render an 18-character
// invite code. We deliberately stay away from visually ambiguous glyphs
// (0/O, 1/l/I) so codes stay easy to read on the info page and in chat
// screenshots.
const inviteAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghjkmnpqrstuvwxyz23456789"

// InviteCodeLen is the fixed length of every invite code.
const InviteCodeLen = 18

// NewInviteCode returns a fresh 18-character invite code drawn from
// inviteAlphabet using crypto/rand. Collisions are astronomically unlikely at
// 18 chars from a 56-symbol alphabet (~58^18 possibilities) but callers
// should still treat the code column as unique and retry on the rare
// duplicate-key error.
func NewInviteCode() (string, error) {
	out := make([]byte, InviteCodeLen)
	max := big.NewInt(int64(len(inviteAlphabet)))
	for i := range out {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		out[i] = inviteAlphabet[n.Int64()]
	}
	return string(out), nil
}
