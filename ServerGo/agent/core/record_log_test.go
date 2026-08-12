// Package agent — record_log_test.go: 单元测试覆盖 RecordLogService 的核心
// 行为(GameStarted 返回 id / RecordChatMessage 持久化 / RecordAction
// 持久化 / GameEnded + wallet 结算)。
//
// 与 wallet_service_integration_test.go 一致,本文件依赖真实 MariaDB,
// 通过 `recordlogintegration` build tag 隔离,默认 `go test ./...` 不会
// 跑这些用例。运行方式:
//
//   go test -tags recordlogintegration ./agent/...
//
// DB DSN 与 wallet_service_integration_test 共享 LsmAgentGame.conf 加载逻辑;
// DB 不可达时 t.Skip,CI 干净通过。
//
//go:build recordlogintegration

package agentcore_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"LsmAgentGame/agent/core"
	"LsmAgentGame/config"
	"LsmAgentGame/db"
	"LsmAgentGame/models"
	"LsmAgentGame/service"

	"gorm.io/gorm"
)

// ─────────────────── DB harness ───────────────────

// sharedRLDB is the integration suite's gorm handle (one per process).
var sharedRLDB *gorm.DB

// chdirProjectRoot walks up to find LsmAgentGame.conf.
func chdirProjectRoot() {
	for i := 0; i < 4; i++ {
		if _, err := os.Stat("./LsmAgentGame.conf"); err == nil {
			return
		}
		if err := os.Chdir(".."); err != nil {
			return
		}
	}
}

// newRLDB returns the shared gorm handle or t.Skip when DB is unreachable.
func newRLDB(t *testing.T) *gorm.DB {
	t.Helper()
	if sharedRLDB != nil {
		return sharedRLDB
	}
	chdirProjectRoot()
	cfg := config.Load()
	gormDB, err := db.Init(cfg)
	if err != nil {
		t.Skipf("SKIP: cannot connect to DB: %v", err)
		return nil
	}
	sharedRLDB = gormDB
	return gormDB
}

// rlTestSeq returns a per-process monotonic counter used to namespace test
// rows so two tests in parallel don't collide on the same (room, seat).
var rlTestSeq int64

// rlUniqueRoomID produces a room ID unique to this test invocation.
func rlUniqueRoomID(t *testing.T) string {
	n := atomic.AddInt64(&rlTestSeq, 1)
	return fmt.Sprintf("test-rl-%d-%d-%s", time.Now().UnixNano(), n, t.Name())
}

// rlTestProvider is a minimal stub for the record log cache. We use the
// provider's id field for linkage; the model itself doesn't need to be in
// t_lsm_game_llm_provider for these tests (we pass providerID="" and the
// DB write uses that string).
type rlTestProvider struct {
	id       string
	botUser  *models.TLsmGameUser
	walletSv *service.WalletService
}

// newRLTestHarness creates a Service + provider stub against a fresh roomID.
// Returns the service + cleanup func.
func newRLTestHarness(t *testing.T) (*agent.RecordLogService, rlTestProvider, func()) {
	gormDB := newRLDB(t)
	if gormDB == nil {
		return nil, rlTestProvider{}, func() {}
	}
	ws := service.NewWalletService(gormDB)
	rec := agent.NewRecordLogService(gormDB, ws)
	prov := rlTestProvider{
		id:       "test-provider",
		walletSv: ws,
	}
	cleanup := func() {
		_ = rec.Shutdown(context.Background())
	}
	return rec, prov, cleanup
}

// ─────────────────── Tests ───────────────────

// TestRecordLogService_GameStarted_ReturnsID 验证 GameStarted 返回非空
// UUID 字符串,且 worker 真的写入了 t_lsm_game_model_game_log 行。
func TestRecordLogService_GameStarted_ReturnsID(t *testing.T) {
	rec, prov, cleanup := newRLTestHarness(t)
	defer cleanup()
	if rec == nil {
		return
	}
	roomID := rlUniqueRoomID(t)
	gameLogID, err := rec.GameStarted(context.Background(),
		prov.id, "test-bot-user-1", roomID, "werewolf", 0, "werewolf")
	if err != nil {
		t.Fatalf("GameStarted returned err: %v", err)
	}
	if gameLogID == "" {
		t.Fatalf("GameStarted returned empty gameLogID")
	}
	// 验证 DB row 真的写入
	gormDB := newRLDB(t)
	var row models.TLsmGameModelGameLog
	if err := gormDB.Where("id = ?", gameLogID).First(&row).Error; err != nil {
		t.Fatalf("DB row not found: %v", err)
	}
	if row.RoomID != roomID {
		t.Errorf("RoomID = %q, want %q", row.RoomID, roomID)
	}
	if row.Seat != 0 {
		t.Errorf("Seat = %d, want 0", row.Seat)
	}
	if row.Role != "werewolf" {
		t.Errorf("Role = %q, want werewolf", row.Role)
	}
	if row.EndedAt != nil {
		t.Errorf("EndedAt should be nil, got %v", row.EndedAt)
	}
}

// TestRecordChatMessage_PersistsToDB 验证异步写聊天原文到
// t_lsm_game_model_chat_message。Shutdown 等 drain 后 DB 查行。
func TestRecordChatMessage_PersistsToDB(t *testing.T) {
	rec, prov, cleanup := newRLTestHarness(t)
	defer cleanup()
	if rec == nil {
		return
	}
	roomID := rlUniqueRoomID(t)
	gameLogID, err := rec.GameStarted(context.Background(),
		prov.id, "test-bot-user-2", roomID, "werewolf", 1, "seer")
	if err != nil {
		t.Fatalf("GameStarted: %v", err)
	}
	rec.RecordChatMessage(
		gameLogID, "test-bot-user-2", prov.id, roomID,
		"assistant", "speak", 1,
		"我查验了3号,他是狼人",
		"让我想想...3号说话有点可疑",
		"", "", "end_turn", 1234,
	)
	// Shutdown 等 worker 写完
	if err := rec.Shutdown(context.Background()); err != nil && !errors.Is(err, context.Canceled) {
		t.Logf("shutdown returned: %v (continuing)", err)
	}
	gormDB := newRLDB(t)
	var rows []models.TLsmGameModelChatMessage
	if err := gormDB.Where("game_log_id = ?", gameLogID).Find(&rows).Error; err != nil {
		t.Fatalf("DB query failed: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 chat_message row, got %d", len(rows))
	}
	r := rows[0]
	if r.Role != "assistant" {
		t.Errorf("Role = %q, want assistant", r.Role)
	}
	if r.Content != "我查验了3号,他是狼人" {
		t.Errorf("Content = %q", r.Content)
	}
	if r.Thinking != "让我想想...3号说话有点可疑" {
		t.Errorf("Thinking not persisted")
	}
	if r.LatencyMs != 1234 {
		t.Errorf("LatencyMs = %d, want 1234", r.LatencyMs)
	}
	if r.Phase != "speak" {
		t.Errorf("Phase = %q, want speak", r.Phase)
	}
}

// TestRecordAction_PersistsToDB 验证异步写动作决策到
// t_lsm_game_model_action。
func TestRecordAction_PersistsToDB(t *testing.T) {
	rec, prov, cleanup := newRLTestHarness(t)
	defer cleanup()
	if rec == nil {
		return
	}
	roomID := rlUniqueRoomID(t)
	gameLogID, err := rec.GameStarted(context.Background(),
		prov.id, "test-bot-user-3", roomID, "werewolf", 2, "werewolf")
	if err != nil {
		t.Fatalf("GameStarted: %v", err)
	}
	payload := map[string]any{"target": 3, "text": "发言内容"}
	payloadJSON, _ := json.Marshal(payload)
	rec.RecordAction(
		gameLogID, "test-bot-user-3", "night_wolves",
		"wolf_kill", "3", string(payloadJSON),
		"3号是预言家,先刀他", true,
	)
	if err := rec.Shutdown(context.Background()); err != nil && !errors.Is(err, context.Canceled) {
		t.Logf("shutdown: %v (continuing)", err)
	}
	gormDB := newRLDB(t)
	var rows []models.TLsmGameModelAction
	if err := gormDB.Where("game_log_id = ?", gameLogID).Find(&rows).Error; err != nil {
		t.Fatalf("DB query failed: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 action row, got %d", len(rows))
	}
	r := rows[0]
	if r.ActionType != "wolf_kill" {
		t.Errorf("ActionType = %q, want wolf_kill", r.ActionType)
	}
	if r.ActionTarget != "3" {
		t.Errorf("ActionTarget = %q, want 3", r.ActionTarget)
	}
	if !r.Accepted {
		t.Errorf("Accepted = false, want true")
	}
	if r.Phase != "night_wolves" {
		t.Errorf("Phase = %q, want night_wolves", r.Phase)
	}
	if r.Reasoning != "3号是预言家,先刀他" {
		t.Errorf("Reasoning not persisted")
	}
}

// TestGameEnded_UpdatesCoinAndWallet 验证 GameEnded 触发 game_log
// 收尾 + wallet 结算。但 wallet 结算需要 botUser 真正存在于
// t_lsm_game_user 表里 — 我们创建测试用户 + 钱包,然后调 GameEnded。
func TestGameEnded_UpdatesCoinAndWallet(t *testing.T) {
	gormDB := newRLDB(t)
	if gormDB == nil {
		return
	}
	// 1. 创建测试用户 + 钱包
	testAccount := fmt.Sprintf("test-bot-rl-%d", time.Now().UnixNano())
	testUser := models.TLsmGameUser{
		ID:       newUUID(t),
		Account:  testAccount,
		Nickname: testAccount,
		// 密码 hash 占位(bot 不会真登录)
		PasswordHash: "$2a$10$" + newUUID(t)[:53],
		Language:     "zh-CN",
		IsBot:        true,
	}
	if err := gormDB.Create(&testUser).Error; err != nil {
		t.Fatalf("create test user: %v", err)
	}
	defer gormDB.Where("id = ?", testUser.ID).Delete(&models.TLsmGameUser{})

	wallet := models.TLsmGameWallet{
		ID:      newUUID(t),
		UserID:  testUser.ID,
		Balance: 1000,
	}
	if err := gormDB.Create(&wallet).Error; err != nil {
		t.Fatalf("create test wallet: %v", err)
	}
	defer gormDB.Where("user_id = ?", testUser.ID).Delete(&models.TLsmGameWallet{})
	initialBalance := wallet.Balance

	// 2. 注入 walletService
	ws := service.NewWalletService(gormDB)
	rec := agent.NewRecordLogService(gormDB, ws)
	defer rec.Shutdown(context.Background())

	roomID := rlUniqueRoomID(t)
	gameLogID, err := rec.GameStarted(context.Background(),
		"", testUser.ID, roomID, "werewolf", 0, "werewolf")
	if err != nil {
		t.Fatalf("GameStarted: %v", err)
	}

	// 3. 调 GameEnded(win +100)
	rec.GameEnded(context.Background(), gameLogID, "win", 100, 5, 100, 50, "")

	if err := rec.Shutdown(context.Background()); err != nil && !errors.Is(err, context.Canceled) {
		t.Logf("shutdown: %v", err)
	}

	// 4. 验证 game_log 收尾
	var logRow models.TLsmGameModelGameLog
	if err := gormDB.Where("id = ?", gameLogID).First(&logRow).Error; err != nil {
		t.Fatalf("query game_log: %v", err)
	}
	if logRow.Result != "win" {
		t.Errorf("Result = %q, want win", logRow.Result)
	}
	if logRow.CoinDelta != 100 {
		t.Errorf("CoinDelta = %d, want 100", logRow.CoinDelta)
	}
	if logRow.EndedAt == nil {
		t.Errorf("EndedAt should be set")
	}
	if logRow.LLMCallCount != 5 {
		t.Errorf("LLMCallCount = %d, want 5", logRow.LLMCallCount)
	}

	// 5. 验证 wallet balance(初始 1000 + 100 = 1100)
	var newWallet models.TLsmGameWallet
	if err := gormDB.Where("user_id = ?", testUser.ID).First(&newWallet).Error; err != nil {
		t.Fatalf("query wallet: %v", err)
	}
	if newWallet.Balance != initialBalance+100 {
		t.Errorf("Wallet balance = %d, want %d", newWallet.Balance, initialBalance+100)
	}
}

// newUUID is a small wrapper to avoid pulling util package (避免循环依赖测试)。
func newUUID(t *testing.T) string {
	// 简单生成: 8-4-4-4-12 hex pattern
	const hex = "0123456789abcdef"
	parts := []int{8, 4, 4, 4, 12}
	var out string
	for i, n := range parts {
		if i > 0 {
			out += "-"
		}
		// 用 time-based random;不要求 cryptographically strong
		now := time.Now().UnixNano() + int64(n)
		for j := 0; j < n; j++ {
			out += string(hex[(int(now)+j)%16])
		}
	}
	return out
}
