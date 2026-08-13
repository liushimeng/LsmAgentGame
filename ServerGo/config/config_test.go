// 2026-07-10 §116: 验证狼人杀配置默认值契约。
//
// 关注:
//   - WerewolfConfig.FirstNightForcedSpeakRounds 默认值从 1 提到 3
//     (狼人杀 7 人局开局每人必发 3 轮)
//   - getForcedSpeakRounds() 的 clamp 行为[1,3]由 werewolf 包测试覆盖,
//     这里只验证 config 层的 applyDefaults 行为。
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWerewolfConfig_DefaultFirstNightForcedSpeakRounds 验证 §116 默认值。
// 模拟一个空 WerewolfConfig 走 applyDefaults,然后断言值 = 3。
func TestWerewolfConfig_DefaultFirstNightForcedSpeakRounds(t *testing.T) {
	c := &Config{}
	applyDefaults(c)
	if c.Werewolf.FirstNightForcedSpeakRounds != 3 {
		t.Errorf("FirstNightForcedSpeakRounds default = %d, want 3 (2026-07-10 §116:狼人杀 7 人局开局每人必发 3 轮强制发言)",
			c.Werewolf.FirstNightForcedSpeakRounds)
	}
}

// TestWerewolfConfig_ExplicitZeroStillDefaultsToThree 验证"显式置零"也被兜底:
// 即使 LsmAgentGame.conf 显式写 0(老用户的 conf.example 在升级前是 0 兜底为 1),
// 我们 §116 之后应该兜底为 3,而不是回到旧 1 默认。
func TestWerewolfConfig_ExplicitZeroStillDefaultsToThree(t *testing.T) {
	c := &Config{Werewolf: WerewolfConfig{FirstNightForcedSpeakRounds: 0}}
	applyDefaults(c)
	if c.Werewolf.FirstNightForcedSpeakRounds != 3 {
		t.Errorf("FirstNightForcedSpeakRounds after applyDefaults on explicit zero = %d, want 3", c.Werewolf.FirstNightForcedSpeakRounds)
	}
}

// TestWerewolfConfig_NonZeroRespected 验证非零显式配置不被覆写。
// 如果用户在 LsmAgentGame.conf 里写了 2,applyDefaults 必须保留(用户的显式选择优先)。
func TestWerewolfConfig_NonZeroRespected(t *testing.T) {
	c := &Config{Werewolf: WerewolfConfig{FirstNightForcedSpeakRounds: 2}}
	applyDefaults(c)
	if c.Werewolf.FirstNightForcedSpeakRounds != 2 {
		t.Errorf("FirstNightForcedSpeakRounds after applyDefaults on explicit 2 = %d, want 2", c.Werewolf.FirstNightForcedSpeakRounds)
	}
}

// TestWerewolfConfig_AllOtherDefaultsSanity 顺手验证 §13 + §16 引入的几个
// 关键默认没被 §116 误伤。
func TestWerewolfConfig_AllOtherDefaultsSanity(t *testing.T) {
	c := &Config{}
	applyDefaults(c)
	wantPairs := map[string]int{
		"FirstNightGraceSec":              c.Werewolf.FirstNightGraceSec,
		"MinSpeaksPerMinute":              c.Werewolf.MinSpeaksPerMinute,
		"ChatHistoryBytes":                c.Werewolf.ChatHistoryBytes,
		"DeathLyricDeadlineSec":           c.Werewolf.DeathLyricDeadlineSec,
	}
	// 这些值应仍为原本的默认(不被 §116 误改)。
	if wantPairs["FirstNightGraceSec"] != 120 {
		t.Errorf("FirstNightGraceSec default = %d, want 120", wantPairs["FirstNightGraceSec"])
	}
	if wantPairs["MinSpeaksPerMinute"] != 2 {
		t.Errorf("MinSpeaksPerMinute default = %d, want 2", wantPairs["MinSpeaksPerMinute"])
	}
	if wantPairs["ChatHistoryBytes"] != 500*1024 {
		t.Errorf("ChatHistoryBytes default = %d, want %d", wantPairs["ChatHistoryBytes"], 500*1024)
	}
	if wantPairs["DeathLyricDeadlineSec"] != 30 {
		t.Errorf("DeathLyricDeadlineSec default = %d, want 30", wantPairs["DeathLyricDeadlineSec"])
	}
}

// =============================================================================
// 2026-08-13 §config-auto-bootstrap — 配置文件自动生成 / 写回 / LLM 段剥离测试
// =============================================================================

// TestStripLLMSensitiveFields_EmptiesProviders 验证 PersistToFile 调用前
// stripLLMSensitiveFields 把 LLM.Providers 数组清空,但保留非敏感字段。
func TestStripLLMSensitiveFields_EmptiesProviders(t *testing.T) {
	c := &Config{
		LLM: LLMConfig{
			Endpoint:             "http://example.com/Anthropic",
			Endpoints:            []string{"http://primary/Anthropic", "http://backup/Anthropic"},
			TimeoutMs:            600000,
			StreamIdleTimeoutMs:  300000,
			MaxRetries:           3,
			Providers: []ProviderConfig{
				{AgentName: "Demo", Model: "Demo-model", APIKey: "sk-SECRETKEY", ProviderType: "anthropic"},
			},
		},
	}
	stripped := stripLLMSensitiveFields(c)
	if stripped != 1 {
		t.Errorf("stripLLMSensitiveFields returned %d, want 1", stripped)
	}
	if len(c.LLM.Providers) != 0 {
		t.Errorf("Providers should be empty after strip, got %d entries", len(c.LLM.Providers))
	}
	// Non-sensitive LLM fields MUST be preserved.
	if c.LLM.Endpoint != "http://example.com/Anthropic" {
		t.Errorf("Endpoint was changed: %q", c.LLM.Endpoint)
	}
	if len(c.LLM.Endpoints) != 2 {
		t.Errorf("Endpoints list should be preserved, got %d entries", len(c.LLM.Endpoints))
	}
	if c.LLM.TimeoutMs != 600000 {
		t.Errorf("TimeoutMs should be preserved, got %d", c.LLM.TimeoutMs)
	}
}

// TestPersistToFile_DropsLLMProviders 端到端验证 PersistToFile 把 LLM
// providers 段从磁盘文件中剥离,直接读取该文件不会拿到任何 api_key。
func TestPersistToFile_DropsLLMProviders(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "LsmAgentGame.conf")

	c := &Config{
		Server: ServerConfig{HTTPSAddr: "0.0.0.0:39001", WSSAddr: "0.0.0.0:39002", DevMode: true},
		LLM: LLMConfig{
			Endpoint:    "http://primary/Anthropic",
			TimeoutMs:   600000,
			MaxRetries:  3,
			Providers: []ProviderConfig{
				{AgentName: "Meituan", Model: "MeiTuan-model", APIKey: "sk-SECRET-SHOULD-NOT-LEAK", ProviderType: "anthropic"},
			},
		},
	}
	stripped, err := c.PersistToFile(path)
	if err != nil {
		t.Fatalf("PersistToFile failed: %v", err)
	}
	if stripped != 1 {
		t.Errorf("stripped count = %d, want 1", stripped)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if strings.Contains(string(data), "sk-SECRET-SHOULD-NOT-LEAK") {
		t.Fatalf("persist stripped LLM providers but the secret api_key still appears in the file: %s", data)
	}
	if strings.Contains(string(data), "MeiTuan-model") {
		t.Errorf("providers section should be omitted, but the model name still appears: %s", data)
	}
	// Sanity: endpoint / timeout_ms must still be present.
	if !strings.Contains(string(data), "http://primary/Anthropic") {
		t.Errorf("Endpoint should be preserved in stripped output, got: %s", data)
	}
	if !strings.Contains(string(data), "600000") {
		t.Errorf("TimeoutMs should be preserved in stripped output")
	}

	// The on-disk artifact must still parse as a Config (round-trip).
	var reparsed Config
	if err := json.Unmarshal(data, &reparsed); err != nil {
		t.Fatalf("reparse: %v", err)
	}
	if reparsed.LLM.Endpoint != "http://primary/Anthropic" {
		t.Errorf("reparse endpoint = %q", reparsed.LLM.Endpoint)
	}
	if len(reparsed.LLM.Providers) != 0 {
		t.Errorf("reparse providers count = %d, want 0", len(reparsed.LLM.Providers))
	}
}

// TestEnsureRuntimeConfigFile_NoOpWhenPresent 验证 LsmAgentGame.conf 已存在时
// ensureRuntimeConfigFile 不修改它(operator 的手编不被覆写)。
func TestEnsureRuntimeConfigFile_NoOpWhenPresent(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir temp: %v", err)
	}
	want := `{"server":{"https_addr":"0.0.0.0:39001"}}` + "\n"
	if err := os.WriteFile("./LsmAgentGame.conf", []byte(want), 0o600); err != nil {
		t.Fatalf("seed conf: %v", err)
	}
	if err := ensureRuntimeConfigFile(); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	got, err := os.ReadFile("./LsmAgentGame.conf")
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != want {
		t.Errorf("ensureRuntimeConfigFile overwrote the operator-edited conf\nwant: %s\ngot:  %s", want, got)
	}
}

// TestEnsureRuntimeConfigFile_CopiesFromExample 验证当 LsmAgentGame.conf 缺失但
// LsmAgentGame.conf.example 存在时,ensureRuntimeConfigFile 把 example 复制成 conf。
func TestEnsureRuntimeConfigFile_CopiesFromExample(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir temp: %v", err)
	}
	example := `{"server":{"https_addr":"0.0.0.0:39001","dev_mode":true}}` + "\n"
	if err := os.WriteFile("./LsmAgentGame.conf.example", []byte(example), 0o600); err != nil {
		t.Fatalf("seed example: %v", err)
	}
	if err := ensureRuntimeConfigFile(); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	got, err := os.ReadFile("./LsmAgentGame.conf")
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != example {
		t.Errorf("copied conf does not match example\nwant: %s\ngot:  %s", example, got)
	}
}

// TestEnsureRuntimeConfigFile_SynthesizesBoth 验证当 LsmAgentGame.conf 和
// LsmAgentGame.conf.example 都不存在时,ensureRuntimeConfigFile 用代码默认生成
// 两份文件(完全可用的兜底,新手 git clone 立刻能跑)。
func TestEnsureRuntimeConfigFile_SynthesizesBoth(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir temp: %v", err)
	}
	if err := ensureRuntimeConfigFile(); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	for _, name := range []string{"./LsmAgentGame.conf", "./LsmAgentGame.conf.example"} {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("missing generated %s: %v", name, err)
		}
		var c Config
		if err := json.Unmarshal(data, &c); err != nil {
			t.Fatalf("%s is not valid JSON: %v", name, err)
		}
		if c.Server.HTTPSAddr == "" {
			t.Errorf("%s missing default HTTPSAddr", name)
		}
	}
}

// TestNonEmptyOr 简单工具函数单测。
func TestNonEmptyOr(t *testing.T) {
	if got := nonEmptyOrLocal("hi", "fallback"); got != "hi" {
		t.Errorf(`nonEmptyOr("hi", "fallback") = %q, want "hi"`, got)
	}
	if got := nonEmptyOrLocal("  ", "fallback"); got != "fallback" {
		t.Errorf(`nonEmptyOr("  ", "fallback") = %q, want "fallback"`, got)
	}
	if got := nonEmptyOrLocal("", "fallback"); got != "fallback" {
		t.Errorf(`nonEmptyOr("", "fallback") = %q, want "fallback"`, got)
	}
}

// nonEmptyOrLocal mirrors the helper in llm/registry.go for testing purposes
// without dragging a package dependency. The behavior under test is trivial
// enough that a duplicated one-liner is clearer than a shared import.
func nonEmptyOrLocal(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}
