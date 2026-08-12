// Package util — AES-256-GCM helpers for encrypting per-provider LLM API keys.
//
// Per the "模型管理 + 模型玩家持久化 + 模型金币" plan (kind-skipping-moth §2.1),
// API keys are stored encrypted in t_lsm_game_llm_provider.api_key_enc, and the
// 32-byte master key is persisted in t_lsm_game_kv under the key
// "llm_api_key_master" (auto-generated on first use).
//
// The wire format is base64(nonce_12B || ciphertext || gcm_tag). The 16-byte
// GCM tag is appended to the ciphertext by Seal/Open internally; the prefix
// is just the 12-byte nonce, identical to the cookie encryption scheme
// (cookie.go). Sharing the format means we can reuse the same parser if a
// future refactor wants one.
//
// Concurrency: EnsureMasterKey uses a single-row INSERT (the kv table's
// primary key is the key string). Two concurrent first-callers can race, but
// the unique key constraint guarantees only one INSERT wins; the loser
// re-reads the existing row and returns it. Subsequent reads always go to
// the DB so that a manual key rotation propagates.
package util

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"

	"LsmAgentGame/errcode"
	"LsmAgentGame/logger"
	"LsmAgentGame/models"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// ErrCryptoKeyMissing is returned when neither the KV table nor an explicit
// override produces a usable master key.
var ErrCryptoKeyMissing = errors.New("crypto: master key unavailable")

// LLMAPIKeyMasterKey is the reserved KV row name. Treat it as a constant; a
// rename here would orphan any existing master keys.
const LLMAPIKeyMasterKey = "llm_api_key_master"

// apiKeyMasterKeyBytes is the fixed 32-byte length required by AES-256.
const apiKeyMasterKeyBytes = 32

// GenerateRandomMasterKey returns a freshly-minted 32-byte master key.
// Exposed primarily for tests and the (currently absent) admin "rotate key"
// endpoint; normal operation goes through EnsureMasterKey which persists
// to the KV table.
func GenerateRandomMasterKey() ([]byte, error) {
	b := make([]byte, apiKeyMasterKeyBytes)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}

// EnsureMasterKey returns the persisted master AES key from t_lsm_game_kv.
// If no row exists yet, a fresh 32-byte key is generated, base64-encoded, and
// inserted. On a unique-key race (two concurrent first callers), the function
// re-reads the winning row and returns its key.
//
// Idempotent: calling EnsureMasterKey repeatedly returns the same key.
// A manual rotation requires deleting the KV row (or updating its value)
// externally; this function will not overwrite an existing row.
func EnsureMasterKey(ctx context.Context, gormDB *gorm.DB) ([]byte, error) {
	if gormDB == nil {
		return nil, ErrCryptoKeyMissing
	}

	// Fast path: existing row.
	var existing models.TLsmGameKV
	err := gormDB.WithContext(ctx).
		Where("`key` = ?", LLMAPIKeyMasterKey).
		First(&existing).Error
	if err == nil {
		return decodeMasterKey(existing.Value)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		logger.L().Error("ensure master key: read kv row failed",
			zap.String("key", LLMAPIKeyMasterKey),
			zap.Error(err))
		return nil, errcode.Code(errcode.ErrDB)
	}

	// Slow path: generate + insert.
	key, err := GenerateRandomMasterKey()
	if err != nil {
		logger.L().Error("ensure master key: random gen failed", zap.Error(err))
		return nil, errcode.Code(errcode.ErrInternal)
	}
	encoded := base64.StdEncoding.EncodeToString(key)
	row := models.TLsmGameKV{
		Key:    LLMAPIKeyMasterKey,
		Value:  encoded,
		Remark: "LLM provider API key master (AES-256-GCM)",
	}
	if err := gormDB.WithContext(ctx).Create(&row).Error; err != nil {
		// Race: another goroutine won the INSERT. Re-read.
		if err2 := gormDB.WithContext(ctx).
			Where("`key` = ?", LLMAPIKeyMasterKey).
			First(&existing).Error; err2 == nil {
			return decodeMasterKey(existing.Value)
		}
		logger.L().Error("ensure master key: insert failed",
			zap.String("key", LLMAPIKeyMasterKey),
			zap.Error(err))
		return nil, errcode.Code(errcode.ErrDB)
	}
	logger.L().Info("ensure master key: generated new key",
		zap.String("key", LLMAPIKeyMasterKey))
	return key, nil
}

// decodeMasterKey parses the base64-encoded key value and length-checks it.
// Errors are wrapped as errcode.ErrInternal so callers don't have to
// distinguish between "bad base64" and "wrong length" at the API boundary.
func decodeMasterKey(encoded string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, errcode.Code(errcode.ErrInternal)
	}
	if len(raw) != apiKeyMasterKeyBytes {
		return nil, errcode.Code(errcode.ErrInternal)
	}
	return raw, nil
}

// EncryptAPIKey encrypts plaintext using the persisted master key and returns
// the base64(nonce || ciphertext+tag) wire format. The output is safe to
// store in t_lsm_game_llm_provider.api_key_enc.
func EncryptAPIKey(ctx context.Context, gormDB *gorm.DB, plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	key, err := EnsureMasterKey(ctx, gormDB)
	if err != nil {
		return "", err
	}
	return encryptWithKey(plaintext, key)
}

// DecryptAPIKey is the inverse of EncryptAPIKey. It returns "" for an empty
// ciphertext (which represents "no key set") so admin reads can be
// round-tripped without an explicit "absent" sentinel.
func DecryptAPIKey(ctx context.Context, gormDB *gorm.DB, ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}
	key, err := EnsureMasterKey(ctx, gormDB)
	if err != nil {
		return "", err
	}
	return decryptWithKey(ciphertext, key)
}

// DecryptAPIKeyWithKey is the key-explicit variant of DecryptAPIKey. Callers
// that need to decrypt a batch of ciphertexts (e.g. llm.Registry loading N
// providers on boot) should call EnsureMasterKey once and reuse the result
// rather than re-querying the KV row for every row.
func DecryptAPIKeyWithKey(ciphertext string, key []byte) (string, error) {
	if ciphertext == "" {
		return "", nil
	}
	return decryptWithKey(ciphertext, key)
}

// encryptWithKey is the inner cipher operation. Kept separate from
// EncryptAPIKey so tests can exercise it without a DB.
func encryptWithKey(plaintext string, key []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", errcode.Code(errcode.ErrInternal)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", errcode.Code(errcode.ErrInternal)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", errcode.Code(errcode.ErrInternal)
	}
	sealed := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	out := make([]byte, 0, len(nonce)+len(sealed))
	out = append(out, nonce...)
	out = append(out, sealed...)
	return base64.StdEncoding.EncodeToString(out), nil
}

// decryptWithKey is the inverse of encryptWithKey.
func decryptWithKey(ciphertext string, key []byte) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", errcode.Code(errcode.ErrValidationFailed)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", errcode.Code(errcode.ErrInternal)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", errcode.Code(errcode.ErrInternal)
	}
	if len(raw) < gcm.NonceSize() {
		return "", errcode.Code(errcode.ErrValidationFailed)
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", errcode.Code(errcode.ErrValidationFailed)
	}
	return string(pt), nil
}