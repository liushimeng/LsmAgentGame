// Package service — agent_memory_service unit tests (2026-07-20 §131)。
//
// Uses the shared `testing`-only conventions of this package (see
// wallet_service_test.go). getDB short-circuits via t.Skip when the configured
// database isn't reachable so CI without MariaDB doesn't fail the whole suite.
package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"LsmWebGame/models"
)

// cleanupAgentMemory 删除测试遗留的记忆行(按 model_key 前缀)。
func cleanupAgentMemory(t *testing.T, prefix string) {
	t.Helper()
	db := getDB(t)
	if db == nil {
		return
	}
	if err := db.Where("model_key LIKE ?", prefix+"%").
		Delete(&models.TLsmGameAgentMemory{}).Error; err != nil {
		t.Logf("cleanup agent memory rows failed: %v", err)
	}
}

func TestAgentMemorySaveIterated_InsertThenVersionBump(t *testing.T) {
	db := getDB(t)
	if db == nil {
		return
	}
	key := fmt.Sprintf("test-mem-%d", time.Now().UnixNano())
	cleanupAgentMemory(t, key)
	defer cleanupAgentMemory(t, key)

	svc := NewAgentMemoryService(db)
	ctx := context.Background()

	// 行不存在 → Load 返回空,nil 错误。
	md, err := svc.Load(ctx, key)
	assertNoErr(t, err)
	if md != "" {
		t.Fatalf("missing row should load empty, got %q", md)
	}

	// 首次 SaveIterated → INSERT(version=1, game_count=1)。
	if err := svc.SaveIterated(ctx, key, "# v1 记忆", "room-1"); err != nil {
		t.Fatalf("first SaveIterated failed: %v", err)
	}
	row, err := svc.LoadFull(ctx, key)
	assertNoErr(t, err)
	if row == nil {
		t.Fatalf("row should exist after first SaveIterated")
	}
	if row.Version != 1 || row.GameCount != 1 {
		t.Fatalf("first insert: want version=1 game_count=1, got version=%d game_count=%d",
			row.Version, row.GameCount)
	}
	if row.MemoryMD != "# v1 记忆" || row.LastGameID != "room-1" {
		t.Fatalf("first insert content mismatch: md=%q game=%q", row.MemoryMD, row.LastGameID)
	}
	if row.LastIteratedAt == nil {
		t.Fatalf("last_iterated_at should be set")
	}

	// 第二次 → version+1, game_count+1,内容替换。
	if err := svc.SaveIterated(ctx, key, "# v2 记忆", "room-2"); err != nil {
		t.Fatalf("second SaveIterated failed: %v", err)
	}
	row, err = svc.LoadFull(ctx, key)
	assertNoErr(t, err)
	if row.Version != 2 || row.GameCount != 2 {
		t.Fatalf("second save: want version=2 game_count=2, got version=%d game_count=%d",
			row.Version, row.GameCount)
	}
	if row.MemoryMD != "# v2 记忆" || row.LastGameID != "room-2" {
		t.Fatalf("second save content mismatch: md=%q game=%q", row.MemoryMD, row.LastGameID)
	}
}

func TestAgentMemorySaveIterated_VersionConflictRetry(t *testing.T) {
	db := getDB(t)
	if db == nil {
		return
	}
	key := fmt.Sprintf("test-mem-conflict-%d", time.Now().UnixNano())
	cleanupAgentMemory(t, key)
	defer cleanupAgentMemory(t, key)

	svc := NewAgentMemoryService(db)
	ctx := context.Background()
	if err := svc.SaveIterated(ctx, key, "# 初始", "room-a"); err != nil {
		t.Fatalf("seed save failed: %v", err)
	}

	// 模拟外部并发把 version 抢走:直接 SQL UPDATE version=version+1,
	// 使 SaveIterated 内部第一次 UPDATE (WHERE version=1) 影响 0 行,
	// 触发"重读合并重试 1 次"路径,最终应以 version=3 落库成功。
	res := db.Model(&models.TLsmGameAgentMemory{}).
		Where("model_key = ?", key).
		Update("version", 2)
	assertNoErr(t, res.Error)
	if res.RowsAffected != 1 {
		t.Fatalf("setup update should affect 1 row")
	}

	if err := svc.SaveIterated(ctx, key, "# 冲突后写入", "room-b"); err != nil {
		t.Fatalf("SaveIterated should retry once and succeed, got: %v", err)
	}
	row, err := svc.LoadFull(ctx, key)
	assertNoErr(t, err)
	if row.Version != 3 {
		t.Fatalf("after conflict retry: want version=3, got %d", row.Version)
	}
	if row.MemoryMD != "# 冲突后写入" {
		t.Fatalf("after conflict retry: content mismatch %q", row.MemoryMD)
	}
}

func TestAgentMemoryClear(t *testing.T) {
	db := getDB(t)
	if db == nil {
		return
	}
	key := fmt.Sprintf("test-mem-clear-%d", time.Now().UnixNano())
	cleanupAgentMemory(t, key)
	defer cleanupAgentMemory(t, key)

	svc := NewAgentMemoryService(db)
	ctx := context.Background()

	// 行不存在 → Clear no-op,nil 错误。
	assertNoErr(t, svc.Clear(ctx, key))

	if err := svc.SaveIterated(ctx, key, "# 待清空", "room-c"); err != nil {
		t.Fatalf("seed save failed: %v", err)
	}
	assertNoErr(t, svc.Clear(ctx, key))
	row, err := svc.LoadFull(ctx, key)
	assertNoErr(t, err)
	if row == nil {
		t.Fatalf("row should still exist after clear")
	}
	if row.MemoryMD != "" {
		t.Fatalf("clear should empty memory_md, got %q", row.MemoryMD)
	}
	if row.Version != 2 {
		t.Fatalf("clear should bump version to 2, got %d", row.Version)
	}
	// game_count 保留(历史审计链路)。
	if row.GameCount != 1 {
		t.Fatalf("clear should preserve game_count=1, got %d", row.GameCount)
	}
}

func TestAgentMemoryDuplicateInsertFallback(t *testing.T) {
	db := getDB(t)
	if db == nil {
		return
	}
	key := fmt.Sprintf("test-mem-dup-%d", time.Now().UnixNano())
	cleanupAgentMemory(t, key)
	defer cleanupAgentMemory(t, key)

	// 直接 SQL 插一行(Load 之后的竞态 INSERT 场景),再走 SaveIterated:
	// Load 命中已存在的行 → UPDATE 路径,不应报 unique 冲突。
	now := time.Now()
	row := models.TLsmGameAgentMemory{
		ID:             fmt.Sprintf("dup-%d", time.Now().UnixNano()),
		ModelKey:       key,
		MemoryMD:       "seed",
		Version:        7,
		GameCount:      3,
		LastGameID:     "room-seed",
		LastIteratedAt: &now,
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("seed insert failed: %v", err)
	}

	svc := NewAgentMemoryService(db)
	if err := svc.SaveIterated(context.Background(), key, "# 覆盖", "room-d"); err != nil {
		t.Fatalf("SaveIterated over seeded row failed: %v", err)
	}
	got, err := svc.LoadFull(context.Background(), key)
	assertNoErr(t, err)
	if got.Version != 8 || got.GameCount != 4 {
		t.Fatalf("want version=8 game_count=4, got version=%d game_count=%d",
			got.Version, got.GameCount)
	}
	if !strings.Contains(got.MemoryMD, "覆盖") {
		t.Fatalf("content mismatch %q", got.MemoryMD)
	}
}
