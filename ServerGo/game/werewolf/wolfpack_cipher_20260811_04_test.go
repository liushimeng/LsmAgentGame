// Package werewolf — wolfpack_cipher_20260811_04_test.go: §20260811-04 U1 暗号系统单测。
//
// 验证:
//   - WolfPackCipher.Set/Get/PurgeByDeath/Reset 行为;
//   - CipherTemplatesForMode 返回值(4 starter/2 advanced/0 default);
//   - CipherBundleToAgentSpec 与 werewolf/agent wwtypes 镜像一致;
//   - WerewolfRoom.WolfPackCipherSnapshotLocked 懒初始化;
package werewolf

import (
	"testing"
)

func TestCipherTemplatesForMode(t *testing.T) {
	cases := map[string]int{
		"":          0, // 关闭暗号
		"starter":   2,
		"advanced":  4,
		"garbage":   0, // 非法模式归零
		"STARTER":   0, // 大小写敏感
	}
	for mode, wantLen := range cases {
		got := CipherTemplatesForMode(mode)
		if len(got) != wantLen {
			t.Errorf("mode=%q: got %d templates, want %d", mode, len(got), wantLen)
		}
	}
}

func TestCipherTemplates_KeyConstantsStable(t *testing.T) {
	// §130 教训:enum 常量不能漂移;前端 schema 也引用这些 key。
	wantKeys := map[string]bool{
		"target_position":    false,
		"sentiment_word":     false,
		"vote_target":        false,
		"fake_seer_posture":  false,
	}
	for _, tpl := range CipherTemplatesAdvanced {
		if _, ok := wantKeys[tpl.Key]; ok {
			wantKeys[tpl.Key] = true
		} else {
			t.Errorf("unknown cipher template key: %q", tpl.Key)
		}
	}
	for k, found := range wantKeys {
		if !found {
			t.Errorf("advanced mode missing key %q", k)
		}
	}
}

func TestWolfPackCipher_SetGetRoundTrip(t *testing.T) {
	c := NewWolfPackCipher()
	bundle := CipherBundle{
		Seat: 5, Day: 2,
		Templates: CipherTemplatesForMode(CipherModeStarter),
	}
	c.Set(5, bundle)
	got := c.Get(5, 2)
	if got.Seat != 5 || got.Day != 2 || len(got.Templates) != 2 {
		t.Errorf("round-trip drift: got %+v", got)
	}
	// 缺失 day 返零值
	if c.Get(5, 999).Seat != 0 {
		t.Errorf("missing day should return zero value, got %+v", c.Get(5, 999))
	}
	// 缺失 seat 返零值
	if c.Get(999, 2).Seat != 0 {
		t.Errorf("missing seat should return zero value, got %+v", c.Get(999, 2))
	}
}

func TestWolfPackCipher_PurgeByDeath(t *testing.T) {
	c := NewWolfPackCipher()
	for _, seat := range []int{1, 3, 7} {
		c.Set(seat, CipherBundle{Seat: seat, Day: 1, Templates: CipherTemplatesForMode(CipherModeAdvanced)})
	}
	purged := c.PurgeByDeath([]int{3})
	if purged != 1 {
		t.Errorf("expected 1 purge, got %d", purged)
	}
	if len(c.SnapshotAll(1)) != 2 {
		t.Errorf("after purge expected 2 bundles, got %d", len(c.SnapshotAll(1)))
	}
	// 空 deadSeats 不做事
	if c.PurgeByDeath(nil) != 0 {
		t.Errorf("nil deadSeats should return 0")
	}
}

func TestWolfPackCipher_SnapshotAllSortedBySeat(t *testing.T) {
	c := NewWolfPackCipher()
	for _, seat := range []int{5, 1, 3, 9, 0} {
		c.Set(seat, CipherBundle{Seat: seat, Day: 1, Templates: CipherTemplatesForMode(CipherModeStarter)})
	}
	out := c.SnapshotAll(1)
	if len(out) != 5 {
		t.Fatalf("expected 5, got %d", len(out))
	}
	for i := 1; i < len(out); i++ {
		if out[i-1].Seat > out[i].Seat {
			t.Errorf("snapshot not sorted: %d > %d", out[i-1].Seat, out[i].Seat)
		}
	}
}

func TestWolfPackCipher_Reset(t *testing.T) {
	c := NewWolfPackCipher()
	c.Set(2, CipherBundle{Seat: 2, Day: 1, Templates: CipherTemplatesForMode(CipherModeStarter)})
	c.Reset()
	if len(c.SnapshotAll(1)) != 0 {
		t.Errorf("after Reset expected empty, got %d", len(c.SnapshotAll(1)))
	}
}

func TestWolfPackCipher_NilSafe(t *testing.T) {
	var c *WolfPackCipher
	// 不 panic 即可
	c.Set(1, CipherBundle{})
	if got := c.Get(1, 1); got.Seat != 0 {
		t.Errorf("nil cipher Get should return zero, got %+v", got)
	}
	if c.SnapshotAll(1) != nil {
		t.Errorf("nil cipher SnapshotAll should return nil")
	}
	if c.PurgeByDeath([]int{1}) != 0 {
		t.Errorf("nil cipher PurgeByDeath should return 0")
	}
	c.Reset()
}

func TestCipherBundleToAgentSpec(t *testing.T) {
	b := CipherBundle{
		Seat: 3, Day: 5,
		Templates: []CipherTemplate{
			{Key: CipherKeyTargetPosition, Label: "目标位置", Description: "test", Keyword: "X号", Severity: CipherSeverityStrong},
		},
	}
	spec := CipherBundleToAgentSpec(b)
	if spec.Seat != 3 || spec.Day != 5 {
		t.Errorf("spec mirror drift: %+v", spec)
	}
	if len(spec.Templates) != 1 {
		t.Fatalf("expected 1 template, got %d", len(spec.Templates))
	}
	if spec.Templates[0].Key != CipherKeyTargetPosition || spec.Templates[0].Severity != int(CipherSeverityStrong) {
		t.Errorf("template mirror drift: %+v", spec.Templates[0])
	}
}

func TestWerewolfRoom_WolfPackCipherSnapshotLocked_LazyInit(t *testing.T) {
	r := &WerewolfRoom{RoomID: "test"}
	idx := r.WolfPackCipherSnapshotLocked()
	if idx == nil {
		t.Fatal("lazy init should produce non-nil index")
	}
	// 再次调用返回同一对象
	idx2 := r.WolfPackCipherSnapshotLocked()
	if idx != idx2 {
		t.Error("lazy init should return same instance on subsequent calls")
	}
}

func TestWerewolfRoom_WolfPackCipherSnapshotLocked_NilRoomSafe(t *testing.T) {
	var r *WerewolfRoom
	if got := r.WolfPackCipherSnapshotLocked(); got != nil {
		t.Errorf("nil room should return nil, got %+v", got)
	}
}

func TestResetWolfPackCipherLocked(t *testing.T) {
	r := &WerewolfRoom{RoomID: "test"}
	r.wolfPackCipher = NewWolfPackCipher()
	r.wolfPackCipher.Set(2, CipherBundle{Seat: 2, Day: 1, Templates: CipherTemplatesForMode(CipherModeStarter)})
	r.ResetWolfPackCipherLocked()
	if r.WolfPackCipherSnapshotLocked().Get(2, 1).Seat != 0 {
		t.Errorf("reset should clear all bundles")
	}
}

func TestCipherBundleForPrompt_EmptyReturnsEmptyString(t *testing.T) {
	if got := CipherBundleForPrompt(CipherBundle{}); got != "" {
		t.Errorf("empty bundle should return empty, got %q", got)
	}
}

func TestCipherBundleForPrompt_NonEmptyContainsAllTemplates(t *testing.T) {
	b := CipherBundle{Seat: 1, Day: 2, Templates: CipherTemplatesForMode(CipherModeAdvanced)}
	got := CipherBundleForPrompt(b)
	if got == "" {
		t.Fatal("non-empty bundle should render")
	}
	if !contains(got, "目标位置") || !contains(got, "情感关键词") || !contains(got, "投票目标") || !contains(got, "假预言家强度") {
		t.Errorf("advanced mode should render all 4 templates, got: %q", got)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || indexOf(s, substr) >= 0)
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}