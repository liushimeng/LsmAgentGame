package util

import (
	"testing"
	"time"
)

func TestCaptchaStore_IssueAndVerify(t *testing.T) {
	store := NewCaptchaStore()
	id, answer, err := store.Issue(5, 30*time.Second)
	if err != nil {
		t.Fatalf("Issue failed: %v", err)
	}
	if id == "" || len(answer) != 5 {
		t.Fatalf("unexpected issue result id=%q answer=%q", id, answer)
	}
	if status := store.Verify(id, answer); status != CaptchaOK {
		t.Fatalf("verify ok status: got %d want %d", status, CaptchaOK)
	}
	// Single-use: second verify on the same id must say expired/missing.
	if status := store.Verify(id, answer); status == CaptchaOK {
		t.Fatalf("captcha reused: expected non-OK, got OK")
	}
}

func TestCaptchaStore_RejectsWrongAnswer(t *testing.T) {
	store := NewCaptchaStore()
	id, _, _ := store.Issue(5, 30*time.Second)
	if status := store.Verify(id, "ZZZZZ"); status != CaptchaWrong {
		t.Fatalf("wrong answer: got %d want %d", status, CaptchaWrong)
	}
}

func TestCaptchaStore_RejectsExpired(t *testing.T) {
	store := NewCaptchaStore()
	id, answer, _ := store.Issue(5, 1*time.Millisecond)
	time.Sleep(20 * time.Millisecond)
	if status := store.Verify(id, answer); status != CaptchaExpired {
		t.Fatalf("expected expired, got %d", status)
	}
}

func TestCaptchaStore_JanitorPurgesExpired(t *testing.T) {
	store := NewCaptchaStore()
	id, _, _ := store.Issue(3, 1*time.Millisecond)
	stop := make(chan struct{})
	go store.Janitor(5*time.Millisecond, stop)
	time.Sleep(50 * time.Millisecond)
	// After Janitor sweep the id must be gone (returns expired/missing — both non-OK).
	if status := store.Verify(id, "ABC"); status == CaptchaOK {
		t.Fatalf("captcha should have been purged, got OK")
	}
	close(stop)
}
