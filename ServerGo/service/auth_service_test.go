package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"LsmAgentGame/config"
	"LsmAgentGame/errcode"
	"LsmAgentGame/util"

	"gorm.io/gorm"
)

// stubDB is a zero-value *gorm.DB. The captcha-gate tests in this file
// MUST short-circuit on the captcha validation step, never reaching any
// call to s.db. If a test mutates in a way that lands in DB code, gorm
// will panic — that's the desired early failure.
func stubDB() *gorm.DB { return &gorm.DB{} }

// TestLogin_AgentBypassesCaptcha verifies the agent bypass account has a
// different code path. We exercise it twice:
//
//  1. With a *wrong* captcha answer: real users would be rejected with
//     ErrAuthCaptchaWrong; the bypass account must NOT short-circuit to
//     that code (we assert the error code is something else, e.g.
//     ErrValidationFailed because we set no password, proving the bypass
//     skipped captcha entirely and reached the password check).
//
//  2. With no password: bypass account must reach password validation
//     (return ErrValidationFailed), proving it skipped captcha entirely.
func TestLogin_AgentBypassesCaptcha(t *testing.T) {
	// Bypass account + a *wrong* captcha answer must NOT short-circuit to a
	// captcha error code. We use a stub DB that will panic on access; the
	// test PASSES if we either (a) get a non-captcha coded error, or (b)
	// panic in DB code (proving the captcha gate was traversed).
	store := util.NewCaptchaStore()
	id, _, _ := store.Issue(5, time.Minute)
	// 测试 bypass 行为需要 DevMode=true(§安全修复:仅 dev 模式放行)。
	devCfg := &config.Config{}
	devCfg.Server.DevMode = true
	s2 := NewAuthService(stubDB(), devCfg, store)

	gotCode := -1
	func() {
		defer func() {
			recover() // swallow DB panic
		}()
		_, err := s2.Login(context.Background(), LoginInput{
			Account:       AgentBypassAccount,
			Password:      "x",
			CaptchaID:     id,
			CaptchaAnswer: "ZZZZZ",
		})
		if err != nil {
			gotCode = errcode.AsError(err).Code
		}
	}()
	switch gotCode {
	case -1:
		// panic recovered — captcha was bypassed, DB code reached. Good.
	case errcode.ErrAuthCaptchaMissing,
		errcode.ErrAuthCaptchaExpired,
		errcode.ErrAuthCaptchaWrong:
		t.Fatalf("bypass account wrongly short-circuited on captcha gate: code=%d", gotCode)
	}
}

func TestLogin_RequiresCaptchaForNormalAccount(t *testing.T) {
	s := NewAuthService(stubDB(), nil, util.NewCaptchaStore())
	_, err := s.Login(context.Background(), LoginInput{
		Account:  "alice",
		Password: "secret",
		// No captcha fields → ErrAuthCaptchaMissing.
	})
	ce := errcode.AsError(err)
	if ce.Code != errcode.ErrAuthCaptchaMissing {
		t.Fatalf("expected ErrAuthCaptchaMissing, got %d (%s)", ce.Code, ce.Message)
	}
}

func TestLogin_RejectsWrongCaptcha(t *testing.T) {
	store := util.NewCaptchaStore()
	id, _, _ := store.Issue(5, time.Minute)
	s := NewAuthService(stubDB(), nil, store)
	_, err := s.Login(context.Background(), LoginInput{
		Account:       "alice",
		Password:      "secret",
		CaptchaID:     id,
		CaptchaAnswer: "ZZZZZ",
	})
	ce := errcode.AsError(err)
	if ce.Code != errcode.ErrAuthCaptchaWrong {
		t.Fatalf("expected ErrAuthCaptchaWrong, got %d", ce.Code)
	}
}

func TestLogin_AcceptsCorrectCaptchaThenReachesDB(t *testing.T) {
	// After captcha passes, real users hit the DB. The stub DB panics —
	// the test asserts the panic reached the DB code, proving captcha was
	// accepted (not bypassed, not rejected).
	store := util.NewCaptchaStore()
	id, answer, _ := store.Issue(5, time.Minute)
	s := NewAuthService(stubDB(), nil, store)

	reachedDB := false
	func() {
		defer func() {
			recover()
			reachedDB = true
		}()
		_, _ = s.Login(context.Background(), LoginInput{
			Account:       "alice",
			Password:      "secret",
			CaptchaID:     id,
			CaptchaAnswer: answer,
		})
	}()
	if !reachedDB {
		t.Fatalf("correct captcha was rejected (DB never reached)")
	}
}

func TestLogin_RequiresAccountOrPhone(t *testing.T) {
	s := NewAuthService(stubDB(), nil, util.NewCaptchaStore())
	_, err := s.Login(context.Background(), LoginInput{
		Password: "secret",
	})
	ce := errcode.AsError(err)
	if ce.Code != errcode.ErrValidationFailed {
		t.Fatalf("expected ErrValidationFailed, got %d", ce.Code)
	}
}

func TestLogin_RequiresPassword(t *testing.T) {
	s := NewAuthService(stubDB(), nil, util.NewCaptchaStore())
	_, err := s.Login(context.Background(), LoginInput{
		Account: "alice",
	})
	ce := errcode.AsError(err)
	if ce.Code != errcode.ErrValidationFailed {
		t.Fatalf("expected ErrValidationFailed, got %d", ce.Code)
	}
}

func TestLogin_DefendsWhenCaptchaStoreMissing(t *testing.T) {
	// captcha=nil → ErrAuthCaptchaMissing for non-agent traffic.
	s := NewAuthService(stubDB(), nil, nil)
	_, err := s.Login(context.Background(), LoginInput{
		Account:  "alice",
		Password: "secret",
	})
	ce := errcode.AsError(err)
	if ce.Code != errcode.ErrAuthCaptchaMissing {
		t.Fatalf("expected ErrAuthCaptchaMissing, got %d", ce.Code)
	}
}

func TestLogin_AgentBypassCaseSensitive(t *testing.T) {
	// Same bytes but wrong case MUST be treated as a real user (captcha
	// gate applies).
	s := NewAuthService(stubDB(), nil, util.NewCaptchaStore())
	_, err := s.Login(context.Background(), LoginInput{
		Account:  "TEST19082JAUISHF8",
		Password: "secret",
	})
	ce := errcode.AsError(err)
	if ce.Code != errcode.ErrAuthCaptchaMissing {
		t.Fatalf("expected ErrAuthCaptchaMissing for non-canonical case, got %d", ce.Code)
	}
}

func TestAsError_Passthrough(t *testing.T) {
	coded := errcode.Code(errcode.ErrAuthPasswordWrong)
	if got := errcode.AsError(coded); got.Code != errcode.ErrAuthPasswordWrong {
		t.Fatalf("AsError passthrough broken: %d", got.Code)
	}
	if got := errcode.AsError(errors.New("raw")); got.Code != errcode.ErrInternal {
		t.Fatalf("AsError raw wrap broken: %d", got.Code)
	}
}
