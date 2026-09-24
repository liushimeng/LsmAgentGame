// Package profession — loader_batch_test.go: 批量抽路径与并行水合契约测试
// (2026-09-21 §档案锚定 §12 G1/G2)。
//
//	DrawPaths 确定性 / 不重复 / n>池返回全池 / 零文件读(删盘后仍可抽路径)
//	HydrateBatch 成功/失败计数 / progress 终值 / workers=1 与 8 结果一致
//	绕过 LRU 直读(水合后 indexed 仍为 0)
package profession

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

// batchCardMD 渲染一张最小可解析的人物卡 frontmatter(id 与文件名一致,
// 便于断言 HydrateBatch 输出与输入路径按序对应)。
func batchCardMD(i int) string {
	return fmt.Sprintf(`---
id: C%04d
name: 居民%04d
occupation: 测试职业%02d
income_monthly: %d
monthly_expense: %d
savings_stock: %d
age: %d
work_intensity: 中
health_grade: A
risk_preference: balanced
marital: 单身
personality: ["务实主义","尽责坚韧"]
opening_hook: 我是批量水合测试居民,正在验证文档池批量契约与解析路径。
goals_short: ["5 年内把储蓄翻一番"]
housing_city: 一线城市
employment_type: 全职
---
正文略`, i, i, i%40, 6000+100*i, 4000, 30000, 25+i%30)
}

// writeBatchFixture 在临时目录 L1 域子目录写入 n 张有效卡,返回根目录。
func writeBatchFixture(t *testing.T, n int) string {
	t.Helper()
	dir := t.TempDir()
	sub := filepath.Join(dir, "Q-金融与保险")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for i := 0; i < n; i++ {
		path := filepath.Join(sub, fmt.Sprintf("C%04d.md", i))
		if err := os.WriteFile(path, []byte(batchCardMD(i)), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	return dir
}

// writeBatchBadCards 写 3 张坏卡(缺收入 / 无 frontmatter / 缺职业)到根目录。
func writeBatchBadCards(t *testing.T, dir string) {
	t.Helper()
	bad := map[string]string{
		"BAD_no_income.md": `---
id: BAD1
occupation: 无收入卡
---
正文略`,
		"BAD_no_frontmatter.md": "没有 frontmatter 的普通 markdown 文档。",
		"BAD_no_occupation.md": `---
id: BAD3
income_monthly: 5000
---
正文略`,
	}
	for name, md := range bad {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(md), 0o644); err != nil {
			t.Fatalf("write bad card %s: %v", name, err)
		}
	}
}

// TestDrawPaths_DeterministicUniqueAndFullPool(§12 G1)。
func TestDrawPaths_DeterministicUniqueAndFullPool(t *testing.T) {
	dir := writeBatchFixture(t, 12)
	mk := func() []string {
		l := NewLoader(dir)
		return l.DrawPaths(5, rand.New(rand.NewSource(99)))
	}
	a, b := mk(), mk()
	if len(a) != 5 || len(b) != 5 {
		t.Fatalf("draw paths len: %d / %d, want 5/5", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("same seed must yield same path set: idx %d %q vs %q", i, a[i], b[i])
		}
	}
	seen := map[string]struct{}{}
	for _, p := range a {
		if _, dup := seen[p]; dup {
			t.Fatalf("duplicate path %q", p)
		}
		seen[p] = struct{}{}
	}
	// n > 池:返回全池洗牌结果(12 张全出,不重复)。
	l := NewLoader(dir)
	all := l.DrawPaths(50, rand.New(rand.NewSource(1)))
	if len(all) != 12 {
		t.Fatalf("n>pool must return whole pool: got %d, want 12", len(all))
	}
	seen = map[string]struct{}{}
	for _, p := range all {
		if _, dup := seen[p]; dup {
			t.Fatalf("duplicate path in full-pool draw: %q", p)
		}
		seen[p] = struct{}{}
	}
}

// TestDrawPaths_ZeroFileIO(§12 G1):抽路径零文件读 —— 索引建好后把文件
// 全删,DrawPaths 仍返回路径(仅索引洗牌,不触磁盘)。
func TestDrawPaths_ZeroFileIO(t *testing.T) {
	dir := writeBatchFixture(t, 8)
	l := NewLoader(dir)
	l.ForceIndex()
	if got := l.PoolSize(); got != 8 {
		t.Fatalf("pool size = %d, want 8", got)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("remove fixture: %v", err)
	}
	paths := l.DrawPaths(5, rand.New(rand.NewSource(2)))
	if len(paths) != 5 {
		t.Fatalf("paths after file removal = %d, want 5 (DrawPaths must be index-only)", len(paths))
	}
}

// TestDrawPaths_EmptyPool 空根目录/缺失根目录 → nil。
func TestDrawPaths_EmptyPool(t *testing.T) {
	empty := NewLoader(t.TempDir())
	if got := empty.DrawPaths(3, rand.New(rand.NewSource(3))); got != nil {
		t.Fatalf("empty root must return nil, got %v", got)
	}
	missing := NewLoader(filepath.Join(t.TempDir(), "no-such"))
	if got := missing.DrawPaths(3, rand.New(rand.NewSource(3))); got != nil {
		t.Fatalf("missing root must return nil, got %v", got)
	}
	// n<=0 与 nil rng 防御。
	ok := NewLoader(writeBatchFixture(t, 4))
	if got := ok.DrawPaths(0, rand.New(rand.NewSource(4))); got != nil {
		t.Fatalf("n=0 must return nil, got %v", got)
	}
}

// TestHydrateBatch(§12 G2):30 张卡(含 3 坏)→ 成功 27 失败 3,
// progress 终值 (30,30),workers=1 与 8 结果一致,LRU 未被批量水合污染。
func TestHydrateBatch(t *testing.T) {
	dir := writeBatchFixture(t, 27)
	writeBatchBadCards(t, dir)
	newLoader := func() *Loader { return NewLoader(dir) }

	type runResult struct {
		ids      []string
		failed   []string
		progress [][2]int
	}
	run := func(workers int) runResult {
		l := newLoader()
		paths := l.DrawPaths(30, rand.New(rand.NewSource(7)))
		if len(paths) != 30 {
			t.Fatalf("paths = %d, want 30", len(paths))
		}
		var res runResult
		cards, failed := l.HydrateBatch(paths, workers, func(done, total int) {
			res.progress = append(res.progress, [2]int{done, total})
		})
		for _, c := range cards {
			res.ids = append(res.ids, c.ID)
		}
		res.failed = failed
		// 终值回调 (total,total) 必达。
		if n := len(res.progress); n == 0 || res.progress[n-1] != [2]int{30, 30} {
			t.Fatalf("progress final = %v, want [30 30]", res.progress)
		}
		return res
	}
	w1 := run(1)
	w8 := run(8)

	if len(w1.ids) != 27 || len(w1.failed) != 3 {
		t.Fatalf("hydrate: success=%d failed=%d, want 27/3", len(w1.ids), len(w1.failed))
	}
	// workers=1 与 8 逐位一致(输出与输入成功序对齐 → 并发数不改变结果)。
	for i := range w1.ids {
		if w1.ids[i] != w8.ids[i] {
			t.Fatalf("worker count changed success order: idx %d %q vs %q", i, w1.ids[i], w8.ids[i])
		}
	}
	if len(w1.failed) != len(w8.failed) {
		t.Fatalf("worker count changed failed set size")
	}
	for i := range w1.failed {
		if w1.failed[i] != w8.failed[i] {
			t.Fatalf("worker count changed failed order: %q vs %q", w1.failed[i], w8.failed[i])
		}
	}
	// 失败集恰为 3 张坏卡。
	badSet := map[string]bool{"BAD_no_income.md": true, "BAD_no_frontmatter.md": true, "BAD_no_occupation.md": true}
	if len(w1.failed) != len(badSet) {
		t.Fatalf("failed = %v, want the 3 bad cards", w1.failed)
	}
	for _, f := range w1.failed {
		base := filepath.Base(f)
		if !badSet[base] {
			t.Fatalf("unexpected failed card %q", f)
		}
	}
	// 成功卡带 L1 域名(路径首段 Q-金融与保险),且与输入路径按序对应。
	l := newLoader()
	paths := l.DrawPaths(30, rand.New(rand.NewSource(7)))
	cards, failed := l.HydrateBatch(paths, 4, nil)
	ci := 0
	for _, p := range paths {
		if badSet[filepath.Base(p)] {
			continue
		}
		if cards[ci].Domain != "Q-金融与保险" {
			t.Fatalf("card %d domain = %q, want Q-金融与保险", ci, cards[ci].Domain)
		}
		want := filepath.Base(p)
		want = want[:len(want)-len(".md")]
		if cards[ci].ID != want {
			t.Fatalf("cards[%d].ID = %q, want %q (输出与成功输入路径按序对应)", ci, cards[ci].ID, want)
		}
		ci++
	}
	_ = failed
	// 绕过 LRU 直读:批量水合后 indexed 恒 0(缓存未被 10 万级批量挤穿)。
	_, _, indexed, _ := l.PoolInfo()
	if indexed != 0 {
		t.Fatalf("batch hydration must bypass LRU, indexed = %d, want 0", indexed)
	}
}

// TestHydrateBatch_Empty 空输入直接返回 nil(不启 worker)。
func TestHydrateBatch_Empty(t *testing.T) {
	l := NewLoader(writeBatchFixture(t, 2))
	cards, failed := l.HydrateBatch(nil, 0, func(int, int) { t.Fatal("no progress expected") })
	if cards != nil || failed != nil {
		t.Fatalf("empty paths must return nil/nil, got %v / %v", cards, failed)
	}
}
