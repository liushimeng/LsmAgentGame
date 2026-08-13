// Package agent — tools_cache.go: 按 (phase, role, aliveHash) 缓存 BuildTools 输出。
//
// 2026-08-13 §20260813-01 优化: 借鉴 agent-studio `plugin_key` 去重模式
// (docs/其他Agent代码分析/agent-studio_意图识别与任务分解分析.md §4.2),
// 13 bot 房间 × 4 角色 × 6 phase 累计 BuildTools 调用频次高,缓存按
// (phase, role, aliveHash) 维度避免重复构造 []llm.ToolDef。
//
// 设计要点:
//   - cache key 仅由 (phase, role, aliveHash) 决定。座位号 / speakTurn
//     不参与 key(它们影响的是 LLM 决策,不是工具 schema 的形状)。
//   - 工具 targetEnum(目标座位列表)由 BuildTools 内部从 alive[] 动态生成,
//     所以 aliveHash 必须纳入 key(同 phase/role 但存活集合不同时,target enum 不同)。
//   - sync.RWMutex 保护,read 多 goroutine 并发,write 单飞。
//   - 容量上限 64(覆盖 8 phase × 8 role 组合),超过走头部覆盖(简化 LRU)。
//   - 房间销毁时由 caller 调 Clear() 释放引用。
//
// 与 §130 教训的兼容性:
//   - 不参与任何"声明了却从不接线"路径,本缓存是 BuildTools 的"前置加速层",
//     行为 100% 等价于直接调用,只是省 CPU。
//   - 测试双向验证:缓存命中结果 = 直接调用结果(完全一致)。
package wwplayer

import (
	"hash/fnv"
	"sort"
	"sync"

	"LsmAgentGame/agent/wwtypes"
	"LsmAgentGame/llm"
)

// toolsCacheMaxEntries 是缓存条数上限(8 phase × 8 role = 64,留冗余)。
// 超出后从头覆盖——LRU 简化版,场景规模无需独立 evict 算法。
const toolsCacheMaxEntries = 64

// toolsCacheEntry 是单条缓存项。
type toolsCacheEntry struct {
	key   string
	tools []llm.ToolDef
}

// ToolsCache 是按 (phase, role, aliveHash) 维度的 BuildTools 结果缓存。
//
// 零值不可用,必须 NewToolsCache。线程安全。
type ToolsCache struct {
	mu      sync.RWMutex
	entries map[string]*toolsCacheEntry
	order   []string // 插入顺序,容量满时淘汰头部
	hits    uint64   // 命中次数(诊断用)
	misses  uint64   // 未命中次数(诊断用)
}

// NewToolsCache 创建并初始化缓存。
func NewToolsCache() *ToolsCache {
	return &ToolsCache{
		entries: make(map[string]*toolsCacheEntry, toolsCacheMaxEntries),
		order:   make([]string, 0, toolsCacheMaxEntries),
	}
}

// aliveHash 把 alive seats 切片转为稳定哈希字符串(排序后 FNV-1a 32-bit)。
// 仅用于 cache key 区分,不参与 LLM 推理。
func aliveHash(alive []int) string {
	if len(alive) == 0 {
		return "empty"
	}
	cp := make([]int, len(alive))
	copy(cp, alive)
	sort.Ints(cp)
	h := fnv.New32a()
	for _, s := range cp {
		// 写入 4 字节 + 分隔符避免 [1,23] vs [12,3] 哈希碰撞。
		var buf [5]byte
		buf[0] = byte(s >> 24)
		buf[1] = byte(s >> 16)
		buf[2] = byte(s >> 8)
		buf[3] = byte(s)
		buf[4] = '|'
		_, _ = h.Write(buf[:])
	}
	return uint32ToHex(h.Sum32())
}

func uint32ToHex(v uint32) string {
	const hexChars = "0123456789abcdef"
	var buf [8]byte
	for i := 7; i >= 0; i-- {
		buf[i] = hexChars[v&0x0f]
		v >>= 4
	}
	return string(buf[:])
}

// cacheKey 构造缓存键。
func (c *ToolsCache) cacheKey(phase, role string, alive []int) string {
	return phase + "|" + role + "|" + aliveHash(alive)
}

// Get 取缓存。未命中返回 (nil, false)。
func (c *ToolsCache) Get(phase, role string, alive []int) ([]llm.ToolDef, bool) {
	if c == nil {
		return nil, false
	}
	key := c.cacheKey(phase, role, alive)
	c.mu.RLock()
	e, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok {
		return nil, false
	}
	c.mu.Lock()
	c.hits++
	c.mu.Unlock()
	return e.tools, true
}

// Put 写入缓存(自动容量控制)。
func (c *ToolsCache) Put(phase, role string, alive []int, tools []llm.ToolDef) {
	if c == nil {
		return
	}
	key := c.cacheKey(phase, role, alive)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.misses++
	// 二次检查:另一个 goroutine 可能已写入。
	if _, ok := c.entries[key]; ok {
		return
	}
	// 容量控制:超限从头部淘汰。
	if len(c.order) >= toolsCacheMaxEntries {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.entries, oldest)
	}
	c.entries[key] = &toolsCacheEntry{key: key, tools: tools}
	c.order = append(c.order, key)
}

// Clear 清空缓存(房间销毁时调)。
func (c *ToolsCache) Clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]*toolsCacheEntry, toolsCacheMaxEntries)
	c.order = c.order[:0]
}

// Stats 返回当前缓存命中/未命中计数与条目数(诊断与监控用)。
func (c *ToolsCache) Stats() (hits, misses uint64, size int) {
	if c == nil {
		return 0, 0, 0
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.hits, c.misses, len(c.entries)
}

// BuildToolsCached 是 BuildTools 的缓存版本(外部调用入口)。
//
// 推荐接入路径: 在调用 BuildTools 的地方(例如 run.go::buildAgentContextLocked
// 或 toolsFor() 辅助函数)用本函数替代直调,可立即享受缓存收益。
//
// 返回的切片是缓存内部引用,调用方**不得**修改(否则污染后续读)。
func BuildToolsCached(cache *ToolsCache, phase, role string, seat int, alive []int, speakTurn int, gc *wwtypes.GameContext) []llm.ToolDef {
	if tools, ok := cache.Get(phase, role, alive); ok {
		return tools
	}
	tools := BuildTools(phase, role, seat, alive, speakTurn, gc)
	cache.Put(phase, role, alive, tools)
	return tools
}
