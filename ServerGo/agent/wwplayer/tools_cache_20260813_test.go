// Package agent — tools_cache_20260813_test.go: ToolsCache 单测。
//
// 2026-08-13 §20260813-01 优化: 验证缓存正确性 + 容量控制 + 并发安全 + 哈希稳定性。
package wwplayer

import (
	"fmt"
	"sync"
	"testing"

	"LsmAgentGame/llm"
)

// ── aliveHash 单测 ─────────────────────────────────────────────────────────

func TestAliveHash_Deterministic(t *testing.T) {
	a := aliveHash([]int{3, 1, 2})
	b := aliveHash([]int{2, 3, 1})
	c := aliveHash([]int{1, 2, 3})
	if a != b || b != c {
		t.Fatalf("aliveHash must be order-independent, got %q / %q / %q", a, b, c)
	}
}

func TestAliveHash_Empty(t *testing.T) {
	if got := aliveHash(nil); got != "empty" {
		t.Fatalf("aliveHash(nil) = %q, want \"empty\"", got)
	}
	if got := aliveHash([]int{}); got != "empty" {
		t.Fatalf("aliveHash([]) = %q, want \"empty\"", got)
	}
}

func TestAliveHash_DifferentSets(t *testing.T) {
	a := aliveHash([]int{1, 2, 3})
	b := aliveHash([]int{1, 2, 4})
	if a == b {
		t.Fatalf("aliveHash should differ for [1,2,3] vs [1,2,4], both = %q", a)
	}
}

// 碰撞防御:[1,23] vs [12,3] 应区分(分隔符位 '|' 防 collision)。
func TestAliveHash_NoCollisionOnConcatenation(t *testing.T) {
	a := aliveHash([]int{1, 23})
	b := aliveHash([]int{12, 3})
	if a == b {
		t.Fatalf("aliveHash must avoid concatenation collision: %q vs %q", a, b)
	}
}

// ── ToolsCache 单测 ─────────────────────────────────────────────────────────

func TestToolsCache_GetPutBasic(t *testing.T) {
	c := NewToolsCache()
	if _, ok := c.Get("phase1", "werewolf", []int{1, 2, 3}); ok {
		t.Fatal("Get on empty cache should miss")
	}
	defs := []llm.ToolDef{
		{Name: "wolf_kill", Description: "test", InputSchema: map[string]any{"type": "object"}},
	}
	c.Put("phase1", "werewolf", []int{1, 2, 3}, defs)

	got, ok := c.Get("phase1", "werewolf", []int{1, 2, 3})
	if !ok {
		t.Fatal("Get after Put should hit")
	}
	if len(got) != 1 || got[0].Name != "wolf_kill" {
		t.Fatalf("Get returned wrong tools: %+v", got)
	}

	// 顺序无关:不同 alive 顺序仍命中。
	got2, ok2 := c.Get("phase1", "werewolf", []int{3, 1, 2})
	if !ok2 {
		t.Fatal("Get with reordered alive should still hit (aliveHash sorts)")
	}
	if len(got2) != 1 {
		t.Fatalf("reordered hit returned wrong len: %d", len(got2))
	}
}

func TestToolsCache_DifferentKeyMisses(t *testing.T) {
	c := NewToolsCache()
	c.Put("phase1", "werewolf", []int{1, 2, 3}, []llm.ToolDef{
		{Name: "wolf_kill"},
	})

	if _, ok := c.Get("phase2", "werewolf", []int{1, 2, 3}); ok {
		t.Fatal("Different phase should miss")
	}
	if _, ok := c.Get("phase1", "seer", []int{1, 2, 3}); ok {
		t.Fatal("Different role should miss")
	}
	if _, ok := c.Get("phase1", "werewolf", []int{1, 2, 4}); ok {
		t.Fatal("Different alive should miss")
	}
}

func TestToolsCache_CapacityEviction(t *testing.T) {
	c := NewToolsCache()
	// 填满 +1 触发淘汰
	for i := 0; i < toolsCacheMaxEntries+1; i++ {
		phase := fmt.Sprintf("phase_%d", i)
		c.Put(phase, "werewolf", []int{1}, []llm.ToolDef{
			{Name: phase},
		})
	}
	_, misses, size := c.Stats()
	if size != toolsCacheMaxEntries {
		t.Fatalf("cache size = %d, want %d", size, toolsCacheMaxEntries)
	}
	if misses < 1 {
		t.Fatalf("misses should be ≥1, got %d", misses)
	}
	// 最早插入的 phase_0 应已被淘汰
	if _, ok := c.Get("phase_0", "werewolf", []int{1}); ok {
		t.Fatal("oldest entry should have been evicted")
	}
	// 最新插入的应仍存在
	if _, ok := c.Get(fmt.Sprintf("phase_%d", toolsCacheMaxEntries), "werewolf", []int{1}); !ok {
		t.Fatal("newest entry should still be present")
	}
}

func TestToolsCache_Clear(t *testing.T) {
	c := NewToolsCache()
	c.Put("p1", "werewolf", []int{1}, []llm.ToolDef{{Name: "x"}})
	c.Clear()
	if _, ok := c.Get("p1", "werewolf", []int{1}); ok {
		t.Fatal("Get after Clear should miss")
	}
	_, _, size := c.Stats()
	if size != 0 {
		t.Fatalf("size after Clear = %d, want 0", size)
	}
}

func TestToolsCache_StatsHitMiss(t *testing.T) {
	c := NewToolsCache()
	defs := []llm.ToolDef{{Name: "x"}}
	c.Put("p", "r", []int{1}, defs)
	_, _ = c.Get("p", "r", []int{1})   // hit
	_, _ = c.Get("p", "r", []int{1})   // hit
	_, _ = c.Get("miss", "r", []int{1}) // miss
	hits, misses, _ := c.Stats()
	if hits != 2 {
		t.Fatalf("hits = %d, want 2", hits)
	}
	if misses != 1 {
		t.Fatalf("misses = %d, want 1", misses)
	}
}

func TestToolsCache_ConcurrentSafe(t *testing.T) {
	c := NewToolsCache()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			phase := fmt.Sprintf("p%d", i%10)
			c.Put(phase, "werewolf", []int{i % 5}, []llm.ToolDef{{Name: "x"}})
		}(i)
		go func(i int) {
			defer wg.Done()
			phase := fmt.Sprintf("p%d", i%10)
			_, _ = c.Get(phase, "werewolf", []int{i % 5})
		}(i)
	}
	wg.Wait()
	// 不 panic 即通过(并发出错会 race detector 报警)。
}

func TestToolsCache_NilSafe(t *testing.T) {
	var c *ToolsCache
	if _, ok := c.Get("p", "r", []int{1}); ok {
		t.Fatal("nil cache Get should miss")
	}
	c.Put("p", "r", []int{1}, nil) // 不应 panic
	c.Clear()                       // 不应 panic
	h, m, s := c.Stats()
	if h != 0 || m != 0 || s != 0 {
		t.Fatalf("nil cache Stats = (%d,%d,%d), want (0,0,0)", h, m, s)
	}
}
