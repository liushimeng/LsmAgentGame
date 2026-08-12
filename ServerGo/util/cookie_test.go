package util

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestCookie_RoundTrip(t *testing.T) {
	secret := "test-secret-string-1234567890abcdef"
	plain := "v1|user-id-abc|1700000000|1700086400"
	enc, err := EncryptCookie(plain, secret)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if enc == "" || enc == plain {
		t.Fatalf("unexpected encrypt output %q", enc)
	}
	dec, err := DecryptCookie(enc, secret)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if dec != plain {
		t.Fatalf("decrypt mismatch: got %q want %q", dec, plain)
	}
}

func TestCookie_DecryptFailsWithWrongSecret(t *testing.T) {
	enc, _ := EncryptCookie("v1|u|1|2", "secret-A")
	if _, err := DecryptCookie(enc, "secret-B"); err == nil {
		t.Fatalf("expected error with wrong secret")
	}
}

func TestCookie_DecryptFailsOnTamperedData(t *testing.T) {
	enc, _ := EncryptCookie("v1|u|1|2", "secret")
	// Flip one char to simulate tampering — base64 alphabet may reject the
	// byte; if so we still expect an error, which is the property we care about.
	if enc == "" {
		t.Fatalf("missing enc value")
	}
	tampered := strings.TrimRight(enc, "=") + "X"
	if _, err := DecryptCookie(tampered, "secret"); err == nil {
		t.Fatalf("expected decrypt error on tampered ciphertext")
	}
}

func TestPayload_EncodeParse(t *testing.T) {
	now := time.Now()
	payload := EncodeCookiePayload("user-123", 48*time.Hour, now)
	uid, exp, err := ParseCookiePayload(payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if uid != "user-123" {
		t.Fatalf("user_id mismatch: %s", uid)
	}
	if !exp.After(now) {
		t.Fatalf("expected expiry after now")
	}
}

func TestPayload_RejectsGarbage(t *testing.T) {
	if _, _, err := ParseCookiePayload("garbage"); err == nil {
		t.Fatalf("expected error on garbage payload")
	}
	if _, _, err := ParseCookiePayload("v2|u|1|2"); err == nil {
		t.Fatalf("expected error on wrong version")
	}
}

func TestBuildAuthCookie_Attributes(t *testing.T) {
	c := BuildAuthCookie("lsm_auth", "value", 48*time.Hour, true)
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode {
		t.Fatalf("missing security attributes: %+v", c)
	}
	if c.Name != "lsm_auth" || c.Value != "value" || c.Path != "/" {
		t.Fatalf("cookie fields wrong: %+v", c)
	}
	if c.MaxAge != int((48 * time.Hour).Seconds()) {
		t.Fatalf("max age: got %d", c.MaxAge)
	}
}

func TestBuildClearCookie_NegatesMaxAge(t *testing.T) {
	c := BuildClearCookie("lsm_auth", true)
	if c.MaxAge >= 0 {
		t.Fatalf("expected negative MaxAge, got %d", c.MaxAge)
	}
}
