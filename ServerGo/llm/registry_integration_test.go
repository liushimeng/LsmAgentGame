//go:build llmintegration

// Package llm — DB-backed Registry integration tests.
//
// These tests require a real MariaDB / MySQL connection (the same one used by
// the rest of the server) because t_lsm_game_llm_provider + t_lsm_game_kv
// cannot be exercised with mocks — they exercise the AES-256-GCM
// encrypt/decrypt round-trip and the transactional seed behavior.
//
// Gated behind the `llmintegration` build tag so the default test runner
// (`go test ./...`) skips them and stays DB-optional. Run with:
//
//   go test -tags llmintegration ./llm/...
//
// The DSN is taken from $LSM_CONF (env var the config loader honors) so
// operators can point the suite at a clean schema. When neither LSM_CONF nor
// ./LsmWebGame.conf.example is reachable, the whole file is short-circuited
// via TestMain's t.Skip.
package llm

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"LsmWebGame/config"
	"LsmWebGame/db"
	"LsmWebGame/models"
	"LsmWebGame/util"

	"gorm.io/gorm"
)

// shared gorm handle for the integration suite (avoids re-running migrations
// for every test).
var registryIntegrationDB *gorm.DB

// shared provisioner mock so we can verify the seed path calls it once per
// provider, in order, without exercising the real bot-user service (which
// needs wallet + bcrypt hashing and would slow the test down).
type provisionerCall struct {
	model  string
	rowID  string
	result interface{}
	err    error
}

type fakeProvisioner struct {
	mu    sync.Mutex
	calls []provisionerCall
	failN int // 0 ⇒ never fail; > 0 ⇒ fail the Nth call with errN
	errN  error
}

func (f *fakeProvisioner) EnsureBotUserForProvider(ctx context.Context, p *models.TLsmGameLlmProvider) (interface{}, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	idx := len(f.calls) + 1
	f.calls = append(f.calls, provisionerCall{model: p.Model, rowID: p.ID})
	if f.failN > 0 && idx == f.failN {
		return nil, f.errN
	}
	return "fake-bot-user-" + p.Model, nil
}

func (f *fakeProvisioner) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeProvisioner) Models() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.calls))
	for _, c := range f.calls {
		out = append(out, c.model)
	}
	return out
}

// ensureChdirProjectRoot walks up until it finds LsmWebGame.conf.example so
// config.Load() can find its fallback target.
func ensureChdirProjectRoot() {
	for i := 0; i < 4; i++ {
		if _, err := os.Stat("./LsmWebGame.conf.example"); err == nil {
			return
		}
		if err := os.Chdir(".."); err != nil {
			return
		}
	}
}

// newRegistryIntegrationDB returns the shared gorm handle or t.Skip when
// the DB is unreachable so CI runs cleanly without MariaDB.
func newRegistryIntegrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	if registryIntegrationDB != nil {
		return registryIntegrationDB
	}
	ensureChdirProjectRoot()
	cfg := config.Load()
	gormDB, err := db.Init(cfg)
	if err != nil {
		t.Skipf("SKIP: cannot connect to DB: %v", err)
		return nil
	}
	registryIntegrationDB = gormDB
	return gormDB
}

// uniqueModelSuffix lets each test claim an unused Model/AgentName without
// colliding with other tests that share the same DB.
func uniqueModelSuffix(t *testing.T) string {
	t.Helper()
	return strings.ReplaceAll(t.Name(), "/", "_") + "_" + time.Now().Format("150405.000")
}

// TestNewRegistry_LoadFromDB_OverridesConfig verifies that when
// t_lsm_game_llm_provider already has rows, the cfg.LLM.Providers slice is
// ignored. Two phases:
//   1. seed DB directly with one row (no cfg.LLM.Providers needed)
//   2. construct registry with cfg.LLM.Providers containing 2 DIFFERENT models
//   3. verify List() returns only the DB row, not the cfg entries
func TestNewRegistry_LoadFromDB_OverridesConfig(t *testing.T) {
	gormDB := newRegistryIntegrationDB(t)
	if gormDB == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	suffix := uniqueModelSuffix(t)
	dbModel := "dbmodel_" + suffix
	plain := "sk-real-db-" + suffix
	enc, err := util.EncryptAPIKey(ctx, gormDB, plain)
	if err != nil {
		t.Fatalf("EncryptAPIKey: %v", err)
	}
	row := models.TLsmGameLlmProvider{
		ID:           util.NewUUID(),
		AgentName:    "DB Agent",
		Model:        dbModel,
		ProviderType: "anthropic",
		APIKeyEnc:    enc,
		APIKeyHint:   "sk-real-db...",
		Enabled:      true,
	}
	if err := gormDB.WithContext(ctx).Create(&row).Error; err != nil {
		t.Fatalf("seed DB row: %v", err)
	}
	t.Cleanup(func() {
		_ = gormDB.WithContext(ctx).Where("id = ?", row.ID).Delete(&models.TLsmGameLlmProvider{}).Error
	})

	// cfg has 2 completely different models — must be IGNORED.
	cfg := config.LLMConfig{
		Endpoint:   "http://localhost:1/x",
		TimeoutMs:  5000,
		Providers: []config.ProviderConfig{
			{AgentName: "CFG-A", Model: "cfgmodel_a_" + suffix, APIKey: "sk-cfg-a", ProviderType: "anthropic"},
			{AgentName: "CFG-B", Model: "cfgmodel_b_" + suffix, APIKey: "sk-cfg-b", ProviderType: "anthropic"},
		},
	}
	prov := &fakeProvisioner{}
	r := NewRegistryWithDB(cfg, gormDB, prov)
	if r == nil {
		t.Fatal("registry is nil")
	}
	if r.Source() != "db" {
		t.Errorf("Source = %q, want db", r.Source())
	}

	list := r.List()
	var sawDB, sawCfgA, sawCfgB bool
	for _, m := range list {
		switch m.Model {
		case dbModel:
			sawDB = true
		case "cfgmodel_a_" + suffix:
			sawCfgA = true
		case "cfgmodel_b_" + suffix:
			sawCfgB = true
		}
	}
	if !sawDB {
		t.Errorf("DB model %q missing from List()", dbModel)
	}
	if sawCfgA || sawCfgB {
		t.Errorf("cfg.LLM.Providers leaked into registry: sawA=%v sawB=%v", sawCfgA, sawCfgB)
	}
	// Ensure the decrypted key is usable.
	provider, key, err := r.Get(dbModel)
	if err != nil {
		t.Fatalf("Get(dbModel): %v", err)
	}
	if key != plain {
		t.Errorf("decrypted key = %q, want %q", key, plain)
	}
	if provider == nil {
		t.Error("provider is nil")
	}
	// Provisioner must NOT be called on the DB-wins path.
	if prov.CallCount() != 0 {
		t.Errorf("provisioner called %d times on DB-wins path, want 0", prov.CallCount())
	}
}

// TestNewRegistry_SeedFromConfig_OnEmptyDB verifies the auto-seed path:
// given an empty t_lsm_game_llm_provider + 8 cfg.LLM.Providers, the registry
// should:
//   - seed all 8 rows into the DB
//   - register a bot user per provider via the provisioner
//   - reflect the seeded set in List()
func TestNewRegistry_SeedFromConfig_OnEmptyDB(t *testing.T) {
	gormDB := newRegistryIntegrationDB(t)
	if gormDB == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	suffix := uniqueModelSuffix(t)

	// Wipe any leftover rows for this test's model prefix BEFORE the
	// assertion so we know DB started empty for these models.
	prefix := "seedtest_" + suffix
	if err := gormDB.WithContext(ctx).
		Where("model LIKE ?", prefix+"%").
		Delete(&models.TLsmGameLlmProvider{}).Error; err != nil {
		t.Fatalf("pre-clean DB: %v", err)
	}
	t.Cleanup(func() {
		_ = gormDB.WithContext(ctx).
			Where("model LIKE ?", prefix+"%").
			Delete(&models.TLsmGameLlmProvider{}).Error
	})

	cfg := config.LLMConfig{
		Endpoint:  "http://localhost:1/x",
		TimeoutMs: 5000,
	}
	for i := 1; i <= 8; i++ {
		cfg.Providers = append(cfg.Providers, config.ProviderConfig{
			AgentName:    "Seed " + suffix,
			Model:        prefix + "_m" + string(rune('0'+i)),
			APIKey:       "sk-seed-" + suffix + "-" + string(rune('0'+i)),
			ProviderType: "anthropic",
		})
	}

	prov := &fakeProvisioner{}
	r := NewRegistryWithDB(cfg, gormDB, prov)
	if r == nil {
		t.Fatal("registry is nil")
	}
	if r.Source() != "config-seed" {
		t.Errorf("Source = %q, want config-seed", r.Source())
	}

	// Verify all 8 rows are in the DB now.
	var count int64
	if err := gormDB.WithContext(ctx).
		Model(&models.TLsmGameLlmProvider{}).
		Where("model LIKE ?", prefix+"%").
		Count(&count).Error; err != nil {
		t.Fatalf("count seeded rows: %v", err)
	}
	if count != 8 {
		t.Errorf("seeded rows = %d, want 8", count)
	}

	// Verify the registry exposes all 8.
	if got := len(r.List()); got != 8 {
		t.Errorf("r.List() len = %d, want 8", got)
	}

	// Verify the provisioner was called exactly once per provider.
	if got := prov.CallCount(); got != 8 {
		t.Errorf("provisioner call count = %d, want 8", got)
	}

	// Spot-check that decryption round-trips for one provider.
	oneModel := prefix + "_m1"
	provider, key, err := r.Get(oneModel)
	if err != nil {
		t.Fatalf("Get(%s): %v", oneModel, err)
	}
	if key != "sk-seed-"+suffix+"-1" {
		t.Errorf("decrypted key = %q, want sk-seed-...1", key)
	}
	if provider == nil {
		t.Error("provider is nil for seeded model")
	}
}

// TestNewRegistry_GormDBNil_FallsBackToConfig verifies the pure-cfg
// fallback path: when gormDB == nil, the registry must behave identically
// to the pre-refactor NewRegistry(cfg). No DB writes, no provisioner
// invocation.
func TestNewRegistry_GormDBNil_FallsBackToConfig(t *testing.T) {
	cfg := config.LLMConfig{
		Endpoint:  "http://localhost:1/x",
		TimeoutMs: 5000,
		Providers: []config.ProviderConfig{
			{AgentName: "A", Model: "A-model", APIKey: "sk-a", ProviderType: "anthropic"},
			{AgentName: "B", Model: "B-model", APIKey: "sk-b", ProviderType: "anthropic"},
		},
	}
	prov := &fakeProvisioner{}
	r := NewRegistryWithDB(cfg, nil, prov)
	if r == nil {
		t.Fatal("registry is nil")
	}
	if r.Source() != "config-only" {
		t.Errorf("Source = %q, want config-only", r.Source())
	}
	if got := len(r.List()); got != 2 {
		t.Errorf("List len = %d, want 2", got)
	}
	if prov.CallCount() != 0 {
		t.Errorf("provisioner called %d times in pure-cfg mode, want 0", prov.CallCount())
	}
	provider, key, err := r.Get("A-model")
	if err != nil {
		t.Fatalf("Get(A-model): %v", err)
	}
	if key != "sk-a" {
		t.Errorf("key = %q, want sk-a", key)
	}
	if provider == nil {
		t.Error("provider is nil")
	}
}

// TestReload_AfterCRUD_ReflectsChanges verifies that calling Reload after
// the DB has been mutated rebuilds the in-memory map. We simulate the CRUD
// by directly manipulating the DB and then calling Reload.
func TestReload_AfterCRUD_ReflectsChanges(t *testing.T) {
	gormDB := newRegistryIntegrationDB(t)
	if gormDB == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	suffix := uniqueModelSuffix(t)
	prefix := "reload_" + suffix
	t.Cleanup(func() {
		_ = gormDB.WithContext(ctx).
			Where("model LIKE ?", prefix+"%").
			Delete(&models.TLsmGameLlmProvider{}).Error
	})

	// Seed two initial rows.
	for _, model := range []string{prefix + "_a", prefix + "_b"} {
		row := models.TLsmGameLlmProvider{
			ID:           util.NewUUID(),
			AgentName:    "Reload",
			Model:        model,
			ProviderType: "anthropic",
			APIKeyEnc:    "",
			APIKeyHint:   "",
			Enabled:      true,
		}
		if err := gormDB.WithContext(ctx).Create(&row).Error; err != nil {
			t.Fatalf("seed initial %s: %v", model, err)
		}
	}

	cfg := config.LLMConfig{Endpoint: "http://localhost:1/x", TimeoutMs: 5000}
	r := NewRegistryWithDB(cfg, gormDB, nil)
	if got := len(r.List()); got != 2 {
		t.Fatalf("initial List len = %d, want 2", got)
	}

	// CRUD: delete _a, add _c.
	if err := gormDB.WithContext(ctx).
		Where("model = ?", prefix+"_a").
		Delete(&models.TLsmGameLlmProvider{}).Error; err != nil {
		t.Fatalf("delete _a: %v", err)
	}
	cRow := models.TLsmGameLlmProvider{
		ID: util.NewUUID(), AgentName: "Reload C",
		Model: prefix + "_c", ProviderType: "anthropic", Enabled: true,
	}
	if err := gormDB.WithContext(ctx).Create(&cRow).Error; err != nil {
		t.Fatalf("insert _c: %v", err)
	}

	if err := r.Reload(ctx); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	list := r.List()
	haveB, haveC, haveA := false, false, false
	for _, m := range list {
		switch m.Model {
		case prefix + "_a":
			haveA = true
		case prefix + "_b":
			haveB = true
		case prefix + "_c":
			haveC = true
		}
	}
	if !haveB {
		t.Error("Reload lost _b")
	}
	if !haveC {
		t.Error("Reload did not pick up _c")
	}
	if haveA {
		t.Error("Reload still includes deleted _a")
	}
}

// TestRegistry_SeedFailure_RollsBack verifies the transactional rollback:
// when the seed path fails mid-way (simulated by a unique-index collision
// on a pre-existing DB row), the entire batch of cfg.LLM.Providers must be
// rolled back so the DB never holds a partial seed.
//
// NewRegistryWithDB calls logger.Fatal on seed failure, which terminates the
// process via os.Exit. We can't recover from that in a test goroutine, so
// this test runs the same code path via the lower-level SyncFromConfig
// method instead — that path surfaces the error to the caller without
// exiting. The seed transaction body is identical to the one inside
// NewRegistryWithDB (it calls the same seedFromConfigLocked helper), so
// the rollback assertion is the same.
func TestRegistry_SeedFailure_RollsBack(t *testing.T) {
	gormDB := newRegistryIntegrationDB(t)
	if gormDB == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	suffix := uniqueModelSuffix(t)
	prefix := "rollback_" + suffix
	t.Cleanup(func() {
		_ = gormDB.WithContext(ctx).
			Where("model LIKE ?", prefix+"%").
			Delete(&models.TLsmGameLlmProvider{}).Error
	})

	// Pre-seed a row whose AgentName collides with the SECOND provider in
	// our cfg. The seed transaction must fail on _m2 and roll _m1 back.
	collisionRow := models.TLsmGameLlmProvider{
		ID:           util.NewUUID(),
		AgentName:    "DupAgent-" + suffix,
		Model:        prefix + "_collision",
		ProviderType: "anthropic",
		Enabled:      true,
	}
	if err := gormDB.WithContext(ctx).Create(&collisionRow).Error; err != nil {
		t.Fatalf("pre-seed collision row: %v", err)
	}

	cfg := config.LLMConfig{
		Endpoint:  "http://localhost:1/x",
		TimeoutMs: 5000,
		Providers: []config.ProviderConfig{
			{AgentName: "OK-" + suffix, Model: prefix + "_m1", APIKey: "sk-1", ProviderType: "anthropic"},
			{AgentName: "DupAgent-" + suffix, Model: prefix + "_m2", APIKey: "sk-2", ProviderType: "anthropic"},
			{AgentName: "OK2-" + suffix, Model: prefix + "_m3", APIKey: "sk-3", ProviderType: "anthropic"},
		},
	}

	// SyncFromConfig uses the same seedFromConfigLocked body as the
	// NewRegistryWithDB DB-empty branch, but it surfaces the error to the
	// caller instead of calling Fatal. That makes it testable.
	r := NewRegistry(cfg) // pure-cfg stub; we only need r.SyncFromConfig
	r.gormDB = gormDB
	if err := r.SyncFromConfig(ctx, cfg); err == nil {
		t.Fatalf("SyncFromConfig succeeded despite unique-index collision; expected error")
	}

	// The DB must NOT contain _m1 or _m3 (the rows that would have been
	// inserted before the collision triggered rollback).
	var count int64
	if err := gormDB.WithContext(ctx).Model(&models.TLsmGameLlmProvider{}).
		Where("model IN ?", []string{prefix + "_m1", prefix + "_m2", prefix + "_m3"}).
		Count(&count).Error; err != nil {
		t.Fatalf("count post-rollback: %v", err)
	}
	if count != 0 {
		t.Errorf("post-rollback rows = %d, want 0 (full rollback)", count)
	}
}