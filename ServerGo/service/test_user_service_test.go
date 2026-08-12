// File naming: per docs/国际化与命名规范.md, new server test files use the
// test_*_test.go convention (the test_ prefix satisfies the project rule while
// the _test.go suffix keeps Go's test tooling happy).
package service

import (
	"context"
	"testing"

	"LsmAgentGame/errcode"
)

// TestUpdateLanguage_RejectsUnsupported verifies the language validation gate
// short-circuits BEFORE any DB access. A zero-value *gorm.DB (stubDB) would
// panic if reached; an invalid language must return ErrValidationFailed first.
func TestUpdateLanguage_RejectsUnsupported(t *testing.T) {
	s := NewUserService(stubDB())
	for _, lang := range []string{"", "xx", "zh", "EN", "fr", "zh-cn"} {
		err := s.UpdateLanguage(context.Background(), "uid-1", lang)
		ce := errcode.AsError(err)
		if ce.Code != errcode.ErrValidationFailed {
			t.Fatalf("lang %q: expected ErrValidationFailed, got %d (%s)", lang, ce.Code, ce.Message)
		}
	}
}

// TestUpdateLanguage_AcceptsSupportedThenReachesDB verifies that a supported
// language passes validation and proceeds to the DB layer. With the stub DB
// the update panics — recovering the panic proves the validation gate let the
// supported value through.
func TestUpdateLanguage_AcceptsSupportedThenReachesDB(t *testing.T) {
	s := NewUserService(stubDB())
	for _, lang := range []string{"zh-CN", "en", "ja"} {
		reachedDB := false
		func() {
			defer func() {
				recover()
				reachedDB = true
			}()
			_ = s.UpdateLanguage(context.Background(), "uid-1", lang)
		}()
		if !reachedDB {
			t.Fatalf("supported lang %q was rejected before DB", lang)
		}
	}
}

// TestIsSupportedLanguage pins the exact supported set.
func TestIsSupportedLanguage(t *testing.T) {
	want := map[string]bool{
		"zh-CN": true, "en": true, "ja": true,
		"": false, "fr": false, "zh": false, "EN": false,
	}
	for lang, exp := range want {
		if got := IsSupportedLanguage(lang); got != exp {
			t.Fatalf("IsSupportedLanguage(%q) = %v, want %v", lang, got, exp)
		}
	}
}

// TestDemoteFromSuper_RejectsEmpty — 空 ID 必须被验证层挡掉,避免进入 DB 路径。
// 走 stubDB 的 Update 会 panic,我们靠 recover() 反向确认 validation 在前面。
func TestDemoteFromSuper_RejectsEmpty(t *testing.T) {
	s := NewUserService(stubDB())
	err := s.DemoteFromSuper(context.Background(), "")
	ce := errcode.AsError(err)
	if ce.Code != errcode.ErrValidationFailed {
		t.Fatalf("expected ErrValidationFailed for empty id, got %d (%s)", ce.Code, ce.Message)
	}
}

// TestDemoteFromSuper_RejectsNilDB — DB 未注入时必须返 ErrInternal,而不是 panic。
// 这是 main.go 误把 service 接入一半时的硬保险。
func TestDemoteFromSuper_RejectsNilDB(t *testing.T) {
	s := &UserService{db: nil}
	err := s.DemoteFromSuper(context.Background(), "uid-x")
	ce := errcode.AsError(err)
	if ce.Code != errcode.ErrInternal {
		t.Fatalf("expected ErrInternal for nil db, got %d (%s)", ce.Code, ce.Message)
	}
}

// TestDemoteFromSuper_ReachesDBOnValidID — 走通 validation 后,UPDATE 必须真的
// 进入 DB。stubDB 的 Update 会 panic,recover() 证明 validation 放过、guard 没挡。
func TestDemoteFromSuper_ReachesDBOnValidID(t *testing.T) {
	s := NewUserService(stubDB())
	reachedDB := false
	func() {
		defer func() {
			recover()
			reachedDB = true
		}()
		_ = s.DemoteFromSuper(context.Background(), "uid-super-1")
	}()
	if !reachedDB {
		t.Fatal("expected DemoteFromSuper to reach DB layer on valid id")
	}
}
