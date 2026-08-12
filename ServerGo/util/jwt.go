package util

import (
	"errors"
	"time"

	"LsmAgentGame/errcode"

	"github.com/golang-jwt/jwt/v5"
)

// Claims is the JWT payload we sign and verify.
type Claims struct {
	UserID string `json:"uid"`
	jwt.RegisteredClaims
}

// IssueToken signs an HS256 token for the given user ID, with the supplied
// secret, TTL, and issuer. Returns the signed string and its absolute expiry.
func IssueToken(userID, secret, issuer string, ttl time.Duration) (string, time.Time, error) {
	now := time.Now()
	exp := now.Add(ttl)
	claims := Claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString([]byte(secret))
	if err != nil {
		return "", time.Time{}, err
	}
	return signed, exp, nil
}

// ParseToken validates the token signature and expiry, returning the user ID.
func ParseToken(tokenStr, secret string) (string, error) {
	claims := &Claims{}
	tok, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(secret), nil
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return "", errcode.Code(errcode.ErrAuthTokenExpired)
		}
		return "", errcode.Code(errcode.ErrAuthInvalidToken)
	}
	if !tok.Valid {
		return "", errcode.Code(errcode.ErrAuthInvalidToken)
	}
	return claims.UserID, nil
}
