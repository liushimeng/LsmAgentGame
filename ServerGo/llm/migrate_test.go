//go:build llmintegration

// Package llm — DB-backed integration tests for the 2026-08-13
// "auto-migrate LLM providers from LsmAgentGame.conf to MySQL" bootstrap
// flow. Lives behind the same `llmintegration` build tag as
// registry_integration_test.go so the default `go test ./...` run stays
// DB-optional. Run with:
//
//   go test -tags llmintegration ./llm/... -run Migrate
package llm

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"LsmAgentGame/config"
	"LsmAgentGame/models"
	"LsmAgentGame/util"
)

// shortSuffix 委托共享的 testUniqueToken（时间戳在前）。
// trimTo / testModelKey / testAgentName 统一定义在 db_test_helper_test.go。
func shortSuffix(t *testing.T) string {
	t.Helper()
	return testUniqueToken(t)
}

// TestMigrateConfigProvidersToDB_EmptyNoOp 验证 cfg.LLM.Providers 为空时
// 整个 migrate 流程不动 DB,直接返回 (0, 0, nil)。
func TestMigrateConfigProvidersToDB_EmptyNoOp(t *testing.T) {
	gormDB := newRegistryIntegrationDB(t)
	if gormDB == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	before, err := gormDB.WithContext(ctx).Find(&[]models.TLsmGameLlmProvider{}).Rows()
	if err != nil {
		t.Fatalf("count before: %v", err)
	}
	before.Close()

	ins, upd, err := MigrateConfigProvidersToDB(ctx, gormDB, config.LLMConfig{})
	if err != nil {
		t.Fatalf("migrate empty: %v", err)
	}
	if ins != 0 || upd != 0 {
		t.Errorf("migrate empty returned ins=%d upd=%d, want 0/0", ins, upd)
	}
}

// TestMigrateConfigProvidersToDB_InsertsNewRows 验证 DB 中没有的 model 行
// 会被正确插入(带加密的 api_key + 元数据),且 unique 索引保证去重。
func TestMigrateConfigProvidersToDB_InsertsNewRows(t *testing.T) {
	gormDB := newRegistryIntegrationDB(t)
	if gormDB == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	suffix := shortSuffix(t)
	modelKey := testModelKey("mt", suffix)
	agentName := testAgentName("MigrateTest", suffix)
	apiKey := "sk-mt-" + suffix
	cfg := config.LLMConfig{
		Providers: []config.ProviderConfig{
			{
				AgentName:    agentName,
				Model:        modelKey,
				APIKey:       apiKey,
				ProviderType: "anthropic",
			},
		},
	}

	// §20260816-03 —— 必须走 cleanupProviderRows（自建 ctx + 失败即 FAIL）。
	// 旧写法复用了上面 defer cancel() 的 ctx，清理时 ctx 已取消，删除必然失败。
	cleanupProviderRows(t, gormDB, "model = ?", modelKey)

	ins, upd, err := MigrateConfigProvidersToDB(ctx, gormDB, cfg)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if ins != 1 {
		t.Errorf("inserted = %d, want 1", ins)
	}
	if upd != 0 {
		t.Errorf("updated = %d, want 0", upd)
	}

	// Verify the row is there, encrypted, and decrypts to the same key.
	var row models.TLsmGameLlmProvider
	if err := gormDB.WithContext(ctx).
		Where("model = ?", modelKey).
		First(&row).Error; err != nil {
		t.Fatalf("read inserted row: %v", err)
	}
	if row.APIKeyHint == "" {
		t.Errorf("APIKeyHint should be populated, got empty")
	}
	plain, err := util.DecryptAPIKey(ctx, gormDB, row.APIKeyEnc)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if plain != apiKey {
		t.Errorf("decrypted key = %q, want %q", plain, apiKey)
	}
	if !strings.HasPrefix(row.Remark, "migrated from LsmAgentGame.conf") {
		t.Errorf("remark = %q, want migrated-from-conf prefix", row.Remark)
	}
}

// TestMigrateConfigProvidersToDB_PreservesDBApiKey 验证当 DB 已经有同名 model
// 行(且持有非空 api_key_enc)时,migrate 只更新元数据,绝不覆盖 DB 里的 key。
// 这是 §130 反复强调的"声明了却从不接线"反面 — 绝不能让 conf 覆盖 DB 的真相。
func TestMigrateConfigProvidersToDB_PreservesDBApiKey(t *testing.T) {
	gormDB := newRegistryIntegrationDB(t)
	if gormDB == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	suffix := shortSuffix(t)
	modelKey := testModelKey("prsv", suffix)
	originalKey := "sk-DB-" + suffix
	confKey := "sk-CONF-overwrite-" + suffix

	// Pre-seed a row with a real (encrypted) key.
	enc, err := util.EncryptAPIKey(ctx, gormDB, originalKey)
	if err != nil {
		t.Fatalf("seed encrypt: %v", err)
	}
	row := models.TLsmGameLlmProvider{
		ID:           util.NewUUID(),
		AgentName:    testAgentName("OrigDBAgent", suffix),
		Model:        modelKey,
		ProviderType: "anthropic",
		APIKeyEnc:    enc,
		APIKeyHint:   "sk-orig...",
		Enabled:      true,
	}
	if err := gormDB.WithContext(ctx).Create(&row).Error; err != nil {
		t.Fatalf("seed row: %v", err)
	}
	cleanupProviderRows(t, gormDB, "id = ?", row.ID)

	// Now run migrate with a conf that names the same model + a NEW api_key
	// + a new agent_name + thinking settings. The migrate MUST update the
	// metadata but leave APIKeyEnc intact.
	cfg := config.LLMConfig{
		Providers: []config.ProviderConfig{
			{
				AgentName:        testAgentName("UpdFromConf", suffix),
				Model:            modelKey,
				APIKey:           confKey,
				ProviderType:     "anthropic",
				ThinkingRequired: true,
				ThinkingBudget:   8192,
			},
		},
	}
	ins, upd, err := MigrateConfigProvidersToDB(ctx, gormDB, cfg)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if ins != 0 {
		t.Errorf("inserted = %d, want 0 (DB row existed)", ins)
	}
	if upd != 1 {
		t.Errorf("updated = %d, want 1", upd)
	}

	// Re-read the row.
	var after models.TLsmGameLlmProvider
	if err := gormDB.WithContext(ctx).Where("id = ?", row.ID).First(&after).Error; err != nil {
		t.Fatalf("re-read: %v", err)
	}

	// CRITICAL: the encrypted key must NOT have been replaced.
	plain, err := util.DecryptAPIKey(ctx, gormDB, after.APIKeyEnc)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if plain != originalKey {
		t.Errorf("api_key was overwritten by conf — DB key was %q, now %q", originalKey, plain)
	}

	// Metadata MUST have been refreshed.
	if after.AgentName != testAgentName("UpdFromConf", suffix) {
		t.Errorf("AgentName = %q, want %q", after.AgentName, testAgentName("UpdFromConf", suffix))
	}
	if !after.ThinkingEnabled {
		t.Errorf("ThinkingEnabled should have been updated to true")
	}
	if after.ThinkingBudgetTokens != 8192 {
		t.Errorf("ThinkingBudgetTokens = %d, want 8192", after.ThinkingBudgetTokens)
	}
	if !strings.Contains(after.Remark, "metadata-only update") {
		t.Errorf("Remark = %q, want metadata-only-update indicator", after.Remark)
	}
}

// TestMigrateConfigProvidersToDB_MixedInsertAndUpdate 验证一行需要 insert、
// 一行需要 update 的混合场景,两个分支都正常。
func TestMigrateConfigProvidersToDB_MixedInsertAndUpdate(t *testing.T) {
	gormDB := newRegistryIntegrationDB(t)
	if gormDB == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	suffix := shortSuffix(t)
	existingModel := testModelKey("ex", suffix)
	newModel := testModelKey("nw", suffix)

	// Pre-seed existing row with a non-empty encrypted key.
	enc, err := util.EncryptAPIKey(ctx, gormDB, fmt.Sprintf("sk-keep-%s", suffix))
	if err != nil {
		t.Fatalf("seed encrypt: %v", err)
	}
	row := models.TLsmGameLlmProvider{
		ID:           util.NewUUID(),
		AgentName:    testAgentName("Existing", suffix),
		Model:        existingModel,
		ProviderType: "anthropic",
		APIKeyEnc:    enc,
		Enabled:      true,
	}
	if err := gormDB.WithContext(ctx).Create(&row).Error; err != nil {
		t.Fatalf("seed existing: %v", err)
	}
	cleanupProviderRows(t, gormDB, "model IN ?", []string{existingModel, newModel})

	cfg := config.LLMConfig{
		Providers: []config.ProviderConfig{
			{AgentName: testAgentName("UpdExisting", suffix), Model: existingModel, APIKey: "sk-should-be-ignored", ProviderType: "anthropic"},
			{AgentName: testAgentName("BrandNew", suffix), Model: newModel, APIKey: fmt.Sprintf("sk-fresh-%s", suffix), ProviderType: "anthropic"},
		},
	}
	ins, upd, err := MigrateConfigProvidersToDB(ctx, gormDB, cfg)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if ins != 1 {
		t.Errorf("inserted = %d, want 1", ins)
	}
	if upd != 1 {
		t.Errorf("updated = %d, want 1", upd)
	}

	// Existing row: api_key preserved, agent_name updated.
	var exist models.TLsmGameLlmProvider
	if err := gormDB.WithContext(ctx).Where("model = ?", existingModel).First(&exist).Error; err != nil {
		t.Fatalf("re-read existing: %v", err)
	}
	if exist.AgentName != testAgentName("UpdExisting", suffix) {
		t.Errorf("existing AgentName = %q, want %q", exist.AgentName, testAgentName("UpdExisting", suffix))
	}
	existKey, _ := util.DecryptAPIKey(ctx, gormDB, exist.APIKeyEnc)
	if existKey != fmt.Sprintf("sk-keep-%s", suffix) {
		t.Errorf("existing key changed: %q", existKey)
	}

	// New row: inserted, encrypted, agent_name correct.
	var fresh models.TLsmGameLlmProvider
	if err := gormDB.WithContext(ctx).Where("model = ?", newModel).First(&fresh).Error; err != nil {
		t.Fatalf("re-read new: %v", err)
	}
	if fresh.AgentName != testAgentName("BrandNew", suffix) {
		t.Errorf("new AgentName = %q, want %q", fresh.AgentName, testAgentName("BrandNew", suffix))
	}
	freshKey, _ := util.DecryptAPIKey(ctx, gormDB, fresh.APIKeyEnc)
	if freshKey != fmt.Sprintf("sk-fresh-%s", suffix) {
		t.Errorf("new key = %q, want %q", freshKey, fmt.Sprintf("sk-fresh-%s", suffix))
	}
}
