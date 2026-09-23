// security_config_test.go — 20260923-01 §4.1 单测。
//
// 两条不变量：
//  1. LsmAgentGame.conf.example 去掉 // 注释后必须是合法 JSON，且能解出
//     security{} / captcha.decoys（bootstrap 会把它逐字节复制成运行态 conf）；
//  2. example 里写出的 security 值必须与 applyDefaults 的兜底值**逐字段一致**
//     —— 否则「按 example 引导的新部署」与「conf 里没有 security 段的老部署」
//     行为不同，方案 §8 的「conf 不回写、零配置可运行」承诺就破了。
package config

import (
	"encoding/json"
	"os"
	"testing"
)

// examplePath 是 ServerGo/LsmAgentGame.conf.example —— 它是指向仓库根同名
// 文件的符号链接（ensureRuntimeConfigFile 就是按这个路径复制成 conf 的）。
const examplePath = "../LsmAgentGame.conf.example"

func loadExampleConfig(t *testing.T) Config {
	t.Helper()
	raw, err := os.ReadFile(examplePath)
	if err != nil {
		t.Fatalf("read %s: %v", examplePath, err)
	}
	var c Config
	if err := json.Unmarshal([]byte(stripJSONComments(string(raw))), &c); err != nil {
		t.Fatalf("%s must parse as JSON after comment stripping: %v", examplePath, err)
	}
	return c
}

func TestSecurityExampleFileMatchesApplyDefaults(t *testing.T) {
	got := loadExampleConfig(t)

	var want Config
	applyDefaults(&want) // 全零值 → §4.1 的兜底默认

	if got.Security.Enabled == nil {
		t.Fatal(`security.enabled must be present in the example (it is a *bool: absent ≠ false)`)
	}
	if *got.Security.Enabled != want.Security.EnabledResolved() {
		t.Errorf("security.enabled = %v, want %v", *got.Security.Enabled, want.Security.EnabledResolved())
	}
	if got.Security.TrustedProxies == nil {
		t.Error("security.trusted_proxies must be an explicit [] (nil would mean \"not configured\")")
	}
	if len(got.Security.TrustedProxies) != len(want.Security.TrustedProxies) {
		t.Errorf("security.trusted_proxies = %v, want %v", got.Security.TrustedProxies, want.Security.TrustedProxies)
	}
	if got.Security.BodyLimitBytes != want.Security.BodyLimitBytes {
		t.Errorf("security.body_limit_bytes = %d, want %d", got.Security.BodyLimitBytes, want.Security.BodyLimitBytes)
	}

	if got.Security.LoginGuard != want.Security.LoginGuard {
		t.Errorf("security.login_guard = %+v, want %+v", got.Security.LoginGuard, want.Security.LoginGuard)
	}
	if got.Security.WSGuard.PerIPPerMinute != want.Security.WSGuard.PerIPPerMinute {
		t.Errorf("ws_guard.per_ip_per_minute = %d, want %d", got.Security.WSGuard.PerIPPerMinute, want.Security.WSGuard.PerIPPerMinute)
	}
	if got.Security.WSGuard.MaxConnsPerUser != want.Security.WSGuard.MaxConnsPerUser {
		t.Errorf("ws_guard.max_conns_per_user = %d, want %d", got.Security.WSGuard.MaxConnsPerUser, want.Security.WSGuard.MaxConnsPerUser)
	}
	if len(got.Security.WSGuard.AllowedOrigins) != 0 {
		t.Errorf("ws_guard.allowed_origins = %v, want [] (追加白名单由运维按需填)", got.Security.WSGuard.AllowedOrigins)
	}

	// captcha_guard：bind_ip 是 *bool，example 必须显式写 true。
	if got.Security.CaptchaGuard.BindIP == nil {
		t.Fatal("security.captcha_guard.bind_ip must be present in the example")
	}
	if got.Security.CaptchaGuard.BindIPResolved() != want.Security.CaptchaGuard.BindIPResolved() {
		t.Errorf("captcha_guard.bind_ip = %v, want %v", got.Security.CaptchaGuard.BindIPResolved(), want.Security.CaptchaGuard.BindIPResolved())
	}
	if got.Security.CaptchaGuard.PerIPPerMinute != want.Security.CaptchaGuard.PerIPPerMinute {
		t.Errorf("captcha_guard.per_ip_per_minute = %d, want %d", got.Security.CaptchaGuard.PerIPPerMinute, want.Security.CaptchaGuard.PerIPPerMinute)
	}
	if got.Security.CaptchaGuard.MaxPending != want.Security.CaptchaGuard.MaxPending {
		t.Errorf("captcha_guard.max_pending = %d, want %d", got.Security.CaptchaGuard.MaxPending, want.Security.CaptchaGuard.MaxPending)
	}
	if got.Security.CaptchaGuard.BurstRefillSeconds != want.Security.CaptchaGuard.BurstRefillSeconds {
		t.Errorf("captcha_guard.burst_refill_seconds = %d, want %d", got.Security.CaptchaGuard.BurstRefillSeconds, want.Security.CaptchaGuard.BurstRefillSeconds)
	}

	// decoys 的唯一事实来源是 CaptchaConfig.Decoys（§4.6 接线说明）。
	if got.Captcha.Decoys != want.Captcha.Decoys {
		t.Errorf("captcha.decoys = %d, want %d", got.Captcha.Decoys, want.Captcha.Decoys)
	}
}

func TestSecurityDefaultsAreSafeWhenSectionAbsent(t *testing.T) {
	// 运行态 conf 完全没有 security 段（老部署）⇒ applyDefaults 必须全量兜底，
	// 且兜底值是「加固开启」而不是静默关闭。
	var c Config
	if err := json.Unmarshal([]byte(`{"server":{"dev_mode":true}}`), &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	applyDefaults(&c)

	if !c.Security.EnabledResolved() {
		t.Error("an absent security section must default to enabled")
	}
	if c.Security.TrustedProxies == nil || len(c.Security.TrustedProxies) != 0 {
		t.Errorf("trusted_proxies = %#v, want a non-nil empty slice (trust no proxy)", c.Security.TrustedProxies)
	}
	if c.Security.BodyLimitBytes != 16384 {
		t.Errorf("body_limit_bytes = %d, want 16384", c.Security.BodyLimitBytes)
	}
	lg := c.Security.LoginGuard
	if lg.WindowSeconds != 900 || lg.MaxFailures != 5 || lg.IPMaxFailures != 50 ||
		lg.LockSeconds != 60 || lg.MemorySeconds != 86400 || lg.FailDelayMs != 300 {
		t.Errorf("login_guard defaults = %+v, want §4.1 values", lg)
	}
	cg := c.Security.CaptchaGuard
	if cg.PerIPPerMinute != 30 || cg.MaxPending != 20000 || !cg.BindIPResolved() || cg.BurstRefillSeconds != 2 {
		t.Errorf("captcha_guard defaults = %+v (bind_ip resolved %v), want §4.1 values", cg, cg.BindIPResolved())
	}
	ws := c.Security.WSGuard
	if ws.PerIPPerMinute != 20 || ws.MaxConnsPerUser != 10 {
		t.Errorf("ws_guard defaults = %+v, want §4.1 values", ws)
	}
	if c.Captcha.Decoys != 2 {
		t.Errorf("captcha.decoys = %d, want 2", c.Captcha.Decoys)
	}
}

func TestSecurityEnabledResolvedTriState(t *testing.T) {
	var c Config
	if !c.Security.EnabledResolved() {
		t.Error("nil Enabled must resolve to true")
	}
	off := false
	c.Security.Enabled = &off
	if c.Security.EnabledResolved() {
		t.Error("explicit false must resolve to false")
	}
	on := true
	c.Security.Enabled = &on
	if !c.Security.EnabledResolved() {
		t.Error("explicit true must resolve to true")
	}

	var cg CaptchaGuardConfig
	if !cg.BindIPResolved() {
		t.Error("nil BindIP must resolve to true")
	}
	cg.BindIP = &off
	if cg.BindIPResolved() {
		t.Error("explicit false BindIP must resolve to false")
	}
}
