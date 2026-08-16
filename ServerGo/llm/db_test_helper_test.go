//go:build llmintegration

// Package llm — 共享测试基础设施：DB 集成测试的准入门禁 + 清理助手。
//
// §20260816-03 P0-1 修复。历史缺陷：本包的 DB 集成测试把测试行写进了生产
// schema `lsmDB`，且清理**静默失败**，导致 7 行测试数据（model key 里带着
// 测试函数名，如 `mt_TestMigrateConfigProvidersToDB_Insert`）在生产库里
// 存活了整整 3 天，出现在管理员的「🤖 LLM 模型管理」页面上。
//
// 根因是 Go 测试清理的头号陷阱：
//
//	ctx, cancel := context.WithTimeout(...)
//	defer cancel()                       // ← ① 测试函数返回时执行
//	t.Cleanup(func() {                   // ← ② 测试函数返回【之后】才执行
//	    _ = gormDB.WithContext(ctx).Delete(...).Error   // ③ ctx 已 canceled
//	})                                                 // ④ 错误被 `_ =` 吞掉
//
// `t.Cleanup` 晚于 `defer`，所以 ③ 的 DELETE 必然返回 context canceled，
// 一行都删不掉；而 `_ =` 把错误彻底吞掉，测试照常 PASS，作者永远看不到。
//
// 本文件提供三层防御：
//  1. requireTestDB   —— 环境变量门禁 + 生产 schema 黑名单，让「测试连生产库」
//     默认不可能，而不是靠作者自觉。
//  2. cleanupProviderRows —— 自建 context（绝不复用测试主 ctx）+ 清理失败
//     `t.Errorf`，让污染在产生的那一刻就 FAIL。
//  3. TestMain 兜底 —— 按测试前缀扫尾，兜住 t.Fatal 提前返回漏掉的行。
package llm

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"LsmAgentGame/models"

	"gorm.io/gorm"
)

// testModelPrefix 是所有 DB 集成测试所建行的统一前缀。TestMain 的兜底清理
// 按它扫尾，所以**任何**测试新建的 model key 都必须以它开头，否则 t.Fatal
// 提前返回时那一行就会永久残留（正是 §20260816-03 的事故形态）。
// 前缀刻意保持短：model / agent_name 都是 varchar(64)，测试名本身就很长，
// 前缀每多一个字节就少一个字节的唯一性空间（见 §20260816-03 的 varchar 溢出）。
const testModelPrefix = "zzt_"

// envAllowDB 必须显式设为 "1" 才允许 DB 集成测试运行。
//
// 缺省不跑是硬约束：CLAUDE.md §4 要求提交前跑 `go test ./...`，若集成测试
// 默认联库，那道门禁本身就成了污染源。
const envAllowDB = "LSM_TEST_ALLOW_DB"

// envForceProdDB 是生产 schema 黑名单的逃生舱。即便开了 envAllowDB，
// 只要目标 schema 命中黑名单仍会 skip，除非再显式设置本变量。
const envForceProdDB = "LSM_TEST_FORCE_PROD_DB"

// prodSchemaNames 是拒绝写入的 schema 名单。测试应指向独立的
// `lsmDB_test`（或任何非生产 schema）。
var prodSchemaNames = []string{"lsmDB"}

// isProdSchema 判断 schema 名是否命中生产黑名单（大小写不敏感）。
func isProdSchema(name string) bool {
	for _, p := range prodSchemaNames {
		if strings.EqualFold(strings.TrimSpace(name), p) {
			return true
		}
	}
	return false
}

// dbTestGateReason 返回非空字符串表示「应当 skip」，内容即 skip 原因。
// 抽成纯函数以便单测覆盖三条分支，而无需真的连库。
func dbTestGateReason(allowDB, forceProd, schemaName string) string {
	if strings.TrimSpace(allowDB) != "1" {
		return "DB integration test disabled by default; set " +
			envAllowDB + "=1 to run (§20260816-03: 默认不跑，避免污染生产库)"
	}
	if isProdSchema(schemaName) && strings.TrimSpace(forceProd) != "1" {
		return "refusing to run DB tests against production schema " + schemaName +
			"; point LSM_CONF at a test schema (e.g. lsmDB_test) or set " +
			envForceProdDB + "=1 (§20260816-03)"
	}
	return ""
}

// requireTestDB 是所有 DB 集成测试的第一行。返回时要么已 t.Skip，要么
// 目标库已通过门禁校验。schemaName 由调用方从 cfg.Database.Name 传入。
func requireTestDB(t *testing.T, schemaName string) {
	t.Helper()
	if reason := dbTestGateReason(
		os.Getenv(envAllowDB), os.Getenv(envForceProdDB), schemaName,
	); reason != "" {
		t.Skip("SKIP: " + reason)
	}
}

// trimTo clamps s to at most n bytes. model / agent_name 均为 varchar(64)，
// 超长会直接 Error 1406 Data too long。
func trimTo(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// testUniqueToken 返回本次测试的唯一标识：**时间戳在最前**。
//
// §20260816-03 —— 顺序至关重要。model 列 varchar(64)，加前缀后需要截断；
// 若时间戳在尾部会被截掉，跨次运行的 key 就会重复并撞 unique 索引 —— 历史
// 事故正是如此（残留行让后续运行走 update 分支，断言 inserted=1 直接失败）。
func testUniqueToken(t *testing.T) string {
	t.Helper()
	return time.Now().Format("150405.000") + "_" +
		strings.ReplaceAll(t.Name(), "/", "_")
}

// testModelKey 拼装 model key：testModelPrefix 必须是**真正的开头**，
// 因为 TestMain 的兜底清理只按 `model LIKE 'zzt_%'` 扫尾。
// 任何把前缀夹在中间的写法（如 "reload_" + suffix）都会漏出清理网。
//
// 截断到 52 字节而非 64：调用方常把返回值当作**前缀**再追加 "_a" / "_m1"
// 等区分段（见 TestReload / TestRegistry_SeedFailure），必须留出余量，
// 否则 varchar(64) 溢出报 Error 1406。
func testModelKey(seg, token string) string {
	return trimTo(testModelPrefix+seg+"_"+token, 52)
}

// testAgentName 拼装 agent_name。
//
// §20260816-03 —— agent_name 上有 UNIQUE 索引（idx_..._agent_name），
// 所以它和 model 一样必须带唯一 token。历史测试里硬编码 "Reload" /
// "DB Agent" 之所以没炸，只是因为上一轮的残留行恰好是同名同内容；
// 一旦清理真的生效，第二次运行立刻 Duplicate entry。
func testAgentName(seg, token string) string {
	return trimTo(testModelPrefix+seg+"_"+token, 52)
}

// requireEmptyProviderTable 供「整表语义」测试使用 —— 那些断言
// 「DB 为空 ⇒ seed N 行」「List() 长度 == N」的测试，其前提是整张
// t_lsm_game_llm_provider **只有它自己的行**。
//
// §20260816-03 —— 这类测试跑在共享/生产 schema 上永远不可能通过（生产库有
// 10 行真实模型，List() 必然 ≠ 8）。历史上它们之所以"没报错"，是因为从来
// 没人在清理生效的前提下跑完整套。诚实的做法是显式 skip 并说明需要独立
// schema，而不是放宽断言把测试变成永远为真的空壳。
func requireEmptyProviderTable(t *testing.T, gormDB *gorm.DB) {
	t.Helper()
	if gormDB == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var n int64
	if err := gormDB.WithContext(ctx).
		Model(&models.TLsmGameLlmProvider{}).
		Where("model NOT LIKE ?", testModelPrefix+"%").
		Count(&n).Error; err != nil {
		t.Skipf("SKIP: cannot count provider rows: %v", err)
		return
	}
	if n > 0 {
		t.Skipf("SKIP: 整表语义测试需要空的 t_lsm_game_llm_provider，"+
			"当前有 %d 行非测试数据。请把 LSM_CONF 指向独立的空 schema "+
			"(如 lsmDB_test) 再跑 (§20260816-03)", n)
	}
}

// cleanupProviderRows 注册一个「一定会真的执行」的清理钩子。
//
// 两个关键点，都是 §20260816-03 的直接教训：
//  1. **自建 context** —— 绝不复用测试主 ctx。测试主 ctx 在 t.Cleanup 运行前
//     就已被 defer cancel() 取消，复用它等于清理必然失败。
//  2. **失败即 t.Errorf** —— 清理失败意味着生产/测试库留下了脏数据，这本身
//     就是缺陷，必须让测试 FAIL。历史上的 `_ =` 吞错误让污染潜伏了 3 天。
func cleanupProviderRows(t *testing.T, gormDB *gorm.DB, where string, args ...any) {
	t.Helper()
	if gormDB == nil {
		return
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		res := gormDB.WithContext(ctx).
			Where(where, args...).
			Delete(&models.TLsmGameLlmProvider{})
		if res.Error != nil {
			t.Errorf("cleanup failed — 测试数据残留在库中! where=%q args=%v: %v",
				where, args, res.Error)
		}
	})
}

// TestMain 在整包测试跑完后做一次按前缀的兜底清理。
//
// 为什么需要它：单个测试若在 cleanupProviderRows 注册**之前**就 t.Fatal
// （例如 EncryptAPIKey 失败），那一行就没有任何清理钩子。TestMain 是最后
// 一道网。非零删除数会被打印出来 —— 那是「有测试没管好自己」的信号。
func TestMain(m *testing.M) {
	code := m.Run()
	if registryIntegrationDB != nil {
		n, err := purgeTestProviderRows(registryIntegrationDB)
		switch {
		case err != nil:
			println("WARNING: TestMain purge failed, 测试数据可能残留:", err.Error())
			if code == 0 {
				code = 1 // 清理失败必须让整包测试失败
			}
		case n > 0:
			println("WARNING: TestMain purged", n,
				"leftover test provider row(s) — 有测试未走 cleanupProviderRows")
		}
	}
	os.Exit(code)
}

// purgeTestProviderRows 是 TestMain 的兜底扫尾：按 testModelPrefix 删掉本包
// 测试建立的所有行。兜住 t.Fatal 提前返回、或未走 cleanupProviderRows 的遗漏。
//
// 返回删除行数以便 TestMain 打印 —— 非零即说明有测试没做好自己的清理，
// 这是需要被看见的信号，不是可以静默的正常现象。
func purgeTestProviderRows(gormDB *gorm.DB) (int64, error) {
	if gormDB == nil {
		return 0, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res := gormDB.WithContext(ctx).
		Where("model LIKE ?", testModelPrefix+"%").
		Delete(&models.TLsmGameLlmProvider{})
	return res.RowsAffected, res.Error
}
