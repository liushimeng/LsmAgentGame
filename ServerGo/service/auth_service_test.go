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

// TestLogin_NoCaptchaBypass verifies that NO account (including former
// test/automation accounts, and even with DevMode=true) can bypass the
// CAPTCHA gate — 2026-08-25 安全加固回归测试。
func TestLogin_NoCaptchaBypass(t *testing.T) {
	store := util.NewCaptchaStore()
	devCfg := &config.Config{}
	devCfg.Server.DevMode = true
	s2 := NewAuthService(stubDB(), devCfg, store)

	for _, account := range []string{"test_01", "test19082jauishf8", "autowork2026"} {
		// 验证码一次性消费，每个账号签发新的。
		id, _, _ := store.Issue(5, time.Minute)
		// 错误验证码 + DevMode=true 也必须被 CAPTCHA gate 拒绝。
		_, err := s2.Login(context.Background(), LoginInput{
			Account:       account,
			Password:      "x",
			CaptchaID:     id,
			CaptchaAnswer: "ZZZZZ",
		})
		ce := errcode.AsError(err)
		if ce.Code != errcode.ErrAuthCaptchaWrong {
			t.Fatalf("account %q should hit captcha gate even in DevMode, got code=%d (%s)", account, ce.Code, ce.Message)
		}
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
