// Package profession — loader_docs_pool_test.go: 真实文档池集成测试
// (2026-09-16 §文档池解析修复 P0)。
//
// 背景: 旧 docCard 与知识库 Schema v1.1 形状不匹配(work_intensity 是 map、
// goals_short 是 []map、字段名是 employment/name),75,115 张卡 100% 解析失败,
// Draw 永远回退兜底 —— 「75k 文档池」在运行时形同不存在,而单元测试因为
// 只用合成 fixture(纯字符串形状)全绿,事故被完全掩盖。
//
// 本测试直接打真实池(root 由 runtime.Caller 定位仓库根,不写死绝对路径),
// 默认 `go test ./...` 就跑;只在 -short 下跳过(walk 75k 文件 ≈1s)。
package profession

import (
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// docsPoolRoot 返回真实文档池根目录(仓库根 + lag_docs/虚拟城市/玩家职业设计)。
// 2026-09-19: 知识库已迁移为 lag_docs submodule,旧路径 docs/... 不再存在;
// 若 root 不存在则 SkipDir 类警告而非 panic(被 TestDocsPool_DistrictSpread
// rand.Intn(len=0) 撞上),引导 reviewer 检出 submodule。
//
// 用 runtime.Caller 定位本文件,再上溯 4 层到仓库根:
// ServerGo/game/virtual_city/profession → ServerGo/game/virtual_city → ServerGo/game →
// ServerGo → 仓库根。
func docsPoolRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Skip("runtime.Caller unavailable; cannot locate docs pool")
	}
	root := filepath.Join(filepath.Dir(file), "..", "..", "..", "..",
		"lag_docs", "虚拟城市", "玩家职业设计")
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("abs(%s): %v", root, err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Skipf("docs pool not present at %s (请执行 `git submodule update --init --recursive` 检出 lag_docs): %v", abs, err)
	}
	return abs
}

// TestDocsPool_IndexSize 阶段 1 索引:条目数 ≥ 70000(实测 75,115),
// 且 `_` 前缀目录(_框架 / _交付说明)被 SkipDir 排除。
func TestDocsPool_IndexSize(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: 跳过 75k 文件索引")
	}
	l := NewLoader(docsPoolRoot(t))
	l.ForceIndex()
	avail, total, _, _ := l.PoolInfo()
	if !avail {
		t.Fatalf("docs pool not available at %s", l.Root())
	}
	if total < 70000 {
		t.Errorf("indexed entries = %d, want ≥ 70000", total)
	}
	l.mu.RLock()
	for _, rel := range l.indexPath {
		if hasUnderscoreSegment(rel) {
			l.mu.RUnlock()
			t.Fatalf("`_`/`.` 前缀目录未被 SkipDir 排除: %s", rel)
		}
	}
	l.mu.RUnlock()
}

// hasUnderscoreSegment 报告相对路径的任一段是否以 `_` / `.` 开头
// (buildIndex 必须 SkipDir 掉 _框架 / _交付说明 等框架文档目录)。
func hasUnderscoreSegment(rel string) bool {
	cur := ""
	segments := []string{}
	for _, r := range rel {
		if r == '/' || r == filepath.Separator {
			segments = append(segments, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	segments = append(segments, cur)
	for _, seg := range segments {
		if seg == "" {
			continue
		}
		if seg[0] == '_' || seg[0] == '.' {
			return true
		}
	}
	return false
}

// TestDocsPool_ParseSuccessRate 随机抽样 300 张真实卡:
//   - 解析成功率 ≥ 99%(旧实现 0%);
//   - Card.Validate() 通过率 ≥ 99%;
//   - 关键字段非空(salary/district/opening_hook/goals)。
func TestDocsPool_ParseSuccessRate(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: 跳过真实池抽样")
	}
	l := NewLoader(docsPoolRoot(t))
	l.ForceIndex()
	l.mu.RLock()
	pool := append([]string(nil), l.indexPath...)
	l.mu.RUnlock()
	if len(pool) < 300 {
		t.Fatalf("pool too small: %d", len(pool))
	}
	rng := rand.New(rand.NewSource(20260916))
	sample := make([]string, 300)
	for i := range sample {
		sample[i] = pool[rng.Intn(len(pool))]
	}

	parsed, valid := 0, 0
	var firstErrs []string
	for _, rel := range sample {
		card, err := l.parseFile(rel)
		if err != nil {
			if len(firstErrs) < 5 {
				firstErrs = append(firstErrs, rel+": "+err.Error())
			}
			continue
		}
		parsed++
		if card.Validate() != nil {
			if len(firstErrs) < 5 {
				firstErrs = append(firstErrs, rel+": validate failed")
			}
			continue
		}
		valid++
		if card.Source != "docs" {
			t.Errorf("%s: source = %q, want docs", rel, card.Source)
		}
		if card.Salary <= 0 {
			t.Errorf("%s: salary = %d, want > 0", rel, card.Salary)
		}
		if !validDistrict(card.HomeDistrict) {
			t.Errorf("%s: home_district = %q invalid", rel, card.HomeDistrict)
		}
		if len([]rune(card.OpeningHook)) < 20 {
			t.Errorf("%s: opening_hook too short: %q", rel, card.OpeningHook)
		}
		if len(card.Goals) == 0 {
			t.Errorf("%s: goals empty", rel)
		}
	}
	parseRate := float64(parsed) / float64(len(sample))
	validRate := float64(valid) / float64(len(sample))
	t.Logf("真实文档池抽样 %d 张:解析成功 %d(%.2f%%),Validate 通过 %d(%.2f%%)",
		len(sample), parsed, parseRate*100, valid, validRate*100)
	if len(firstErrs) > 0 {
		t.Logf("首批失败样例: %v", firstErrs)
	}
	if parseRate < 0.99 {
		t.Errorf("parse success rate = %.4f, want ≥ 0.99 (失败样例 %v)", parseRate, firstErrs)
	}
	if validRate < 0.99 {
		t.Errorf("Card.Validate rate = %.4f, want ≥ 0.99", validRate)
	}
}

// TestDocsPool_Draw12AllDocs Draw(12) 必须返回 12 张**全部来自文档池**的卡
// (不许回退合成兜底),且 id 互不重复 —— 这是 12 座全 Agent 房开局的前提。
func TestDocsPool_Draw12AllDocs(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: 跳过真实池抽卡")
	}
	l := NewLoader(docsPoolRoot(t))
	for seed := int64(1); seed <= 5; seed++ {
		cards := l.Draw(12, rand.New(rand.NewSource(seed)))
		if len(cards) != 12 {
			t.Fatalf("seed %d: Draw(12) returned %d cards", seed, len(cards))
		}
		seen := map[string]struct{}{}
		for i, c := range cards {
			if c.Source != "docs" {
				t.Errorf("seed %d slot %d: source = %q, want docs(不许回退合成兜底)", seed, i, c.Source)
			}
			if _, dup := seen[c.ID]; dup {
				t.Errorf("seed %d slot %d: duplicate card id %s", seed, i, c.ID)
			}
			seen[c.ID] = struct{}{}
			if err := c.Validate(); err != nil {
				t.Errorf("seed %d slot %d: card %s invalid: %v", seed, i, c.ID, err)
			}
			if c.Title == "" || c.Salary <= 0 {
				t.Errorf("seed %d slot %d: card %s incomplete (title=%q salary=%d)",
					seed, i, c.ID, c.Title, c.Salary)
			}
		}
	}
}

// TestDocsPool_DistrictSpread 文档池城区映射不得退化:抽样 600 张卡,
// 命中的城区种类 ≥ 5(旧实现 100% 落 residential,地图/房价/搬迁机制全废);
// 批次20 §2.4:行业分流覆盖 16 个新区出生地,阈值提为 ≥ 16。
func TestDocsPool_DistrictSpread(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: 跳过真实池抽样")
	}
	l := NewLoader(docsPoolRoot(t))
	l.ForceIndex()
	l.mu.RLock()
	pool := append([]string(nil), l.indexPath...)
	l.mu.RUnlock()
	rng := rand.New(rand.NewSource(99))
	dist := map[string]int{}
	for i := 0; i < 600; i++ {
		card, err := l.parseFile(pool[rng.Intn(len(pool))])
		if err != nil {
			continue
		}
		dist[card.HomeDistrict]++
	}
	if len(dist) < 16 {
		t.Errorf("district spread = %d kinds (%v), want ≥ 16(批次20 §2.4 行业分流覆盖新区)", len(dist), dist)
	}
	t.Logf("城区分布: %v", dist)
}

// TestDocsPool_SelfCheck 采样自检本身必须报告 ≥95% 成功率(线上告警红线)。
func TestDocsPool_SelfCheck(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: 跳过真实池自检")
	}
	l := NewLoader(docsPoolRoot(t))
	l.ForceIndex()
	res := l.SelfCheckPool(200)
	if res.Sampled != 200 {
		t.Fatalf("sampled = %d, want 200", res.Sampled)
	}
	if res.SuccessRate < 0.95 {
		t.Errorf("self-check success rate = %.4f, want ≥ 0.95 (errs %v)", res.SuccessRate, res.FirstErrors)
	}
	if got := l.SelfCheckResult(); !got.Done || got.Parsed != res.Parsed {
		t.Errorf("SelfCheckResult not recorded: %+v", got)
	}
}
