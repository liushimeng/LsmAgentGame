package werewolf

import (
	"testing"
)

// TestIsExposeProp_MirrorCheck 测试 M-01:isExposeProp(PropMirrorCheck) 返回 true。
func TestIsExposeProp_MirrorCheck(t *testing.T) {
	if !isExposeProp(PropMirrorCheck) {
		t.Fatalf("isExposeProp(PropMirrorCheck) must be true")
	}
	// 旧道具保持不变。
	if !isExposeProp(PropMarkdownBomb) {
		t.Fatalf("regression: PropMarkdownBomb lost expose flag")
	}
	if isExposeProp(PropLongSwear) {
		t.Fatalf("regression: PropLongSwear wrongly marked as expose")
	}
}

// TestMirrorExposeActive_Lifecycle 测试 M-02 + M-03:命中后置位,消费一次后清除。
func TestMirrorExposeActive_Lifecycle(t *testing.T) {
	r := &WerewolfRoom{}
	r.mu.Lock()
	r.SetMirrorExposeActiveLocked(Seat(2))
	r.mu.Unlock()

	if got := r.ConsumeMirrorExposeActive(Seat(2)); !got {
		t.Fatalf("expected ConsumeMirrorExposeActive=true first time")
	}
	// 第二次消费应该返回 false(已清空)。
	if got := r.ConsumeMirrorExposeActive(Seat(2)); got {
		t.Fatalf("expected second consume=false (cleared)")
	}
}

// TestMirrorExposeActive_OnlyTargeted 测试 M-02 延伸:不同座位互不干扰。
func TestMirrorExposeActive_OnlyTargeted(t *testing.T) {
	r := &WerewolfRoom{}
	r.mu.Lock()
	r.SetMirrorExposeActiveLocked(Seat(3))
	r.mu.Unlock()

	if !r.ConsumeMirrorExposeActive(Seat(3)) {
		t.Fatalf("seat 3 must be flagged")
	}
	if r.ConsumeMirrorExposeActive(Seat(4)) {
		t.Fatalf("seat 4 must not be flagged (different seat)")
	}
}

// TestPropCatalog_HasMirrorCheck 测试 M-02 副断言:prop_catalog 中确实注册了 mirror_check。
func TestPropCatalog_HasMirrorCheck(t *testing.T) {
	cat := BuildDefaultPropCatalog()
	e, ok := cat.Get("mirror_check")
	if !ok {
		t.Fatalf("mirror_check not in default catalog")
	}
	if e.Price != 200 {
		t.Fatalf("mirror_check price expected 200, got %d", e.Price)
	}
	if !e.Enabled {
		t.Fatalf("mirror_check should be enabled by default")
	}
	if e.IsAOE {
		t.Fatalf("mirror_check must not be AOE")
	}
	if e.InjectType != PropMirrorCheck {
		t.Fatalf("mirror_check InjectType mismatch")
	}
}

// TestPropCatalog_HasMagnetChallenge 测试 M-05 副断言:magnet_challenge 注册。
func TestPropCatalog_HasMagnetChallenge(t *testing.T) {
	cat := BuildDefaultPropCatalog()
	e, ok := cat.Get("magnet_challenge")
	if !ok {
		t.Fatalf("magnet_challenge not in default catalog")
	}
	if !e.IsAOE {
		t.Fatalf("magnet_challenge must be AOE")
	}
	if e.BaseHitRate < 1 {
		t.Fatalf("magnet_challenge hit rate must be > 0, got %d", e.BaseHitRate)
	}
}

// TestInjectRegistry_MirrorMagnet 测试 inject 生成器全部注册到位。
func TestInjectRegistry_MirrorMagnet(t *testing.T) {
	for _, key := range []string{"mirror_check", "magnet_challenge"} {
		if _, ok := InjectRegistry[key]; !ok {
			t.Fatalf("InjectRegistry missing %q", key)
		}
	}
}

// TestPropInjectTypeFromKey_AllNew 测试 3 个新 key 均映射成功。
func TestPropInjectTypeFromKey_AllNew(t *testing.T) {
	cases := []struct {
		key  string
		want PropInjectType
	}{
		{"mirror_check", PropMirrorCheck},
		{"magnet_challenge", PropMagnetChallenge},
		{"behavior_analyze", PropBehaviorAnalyze},
	}
	for _, c := range cases {
		got, ok := PropInjectTypeFromKey(c.key)
		if !ok {
			t.Fatalf("PropInjectTypeFromKey(%q) not registered", c.key)
		}
		if got != c.want {
			t.Fatalf("PropInjectTypeFromKey(%q) = %v, want %v", c.key, got, c.want)
		}
	}
}
