// Package agent — tools_cache.go: 按 (phase, role, aliveHash, shapeExtra) 缓存 BuildTools 输出。
//
// 2026-08-13 §20260813-01 优化: 借鉴 agent-studio `plugin_key` 去重模式
// (docs/其他Agent代码分析/agent-studio_意图识别与任务分解分析.md §4.2),
// 13 bot 房间 × 4 角色 × 6 phase 累计 BuildTools 调用频次高,缓存按
// (phase, role, aliveHash, shapeExtra) 维度避免重复构造 []llm.ToolDef。
//
// 2026-08-13 §20260813-02 U2 — 生产接线 + key 正确性修复:
//
//   - 旧 key 只有 (phase, role, aliveHash),注释声称「座位号 / speakTurn 不参与
//     key(它们影响的是 LLM 决策,不是工具 schema 的形状)」—— **这是错的**:
//     BuildTools 的工具集合与 schema 同时依赖 seat(filterSelf/onlySelf 枚举)、
//     speakTurn==seat(speak/finish_speak/knight_duel/wolf_suicide 是否挂载)、
//     以及一组 gc 字段(GuardLastProtect 剔除上晚守护 / Round<2 首夜提示 /
//     SheriffSeat / SheriffStream / VoteProposed / DeathLyricCurrent /
//     SheriffCandidates 枚举 / Faction / WolfKingSeat / PropSnapshot 动态描述)。
//     若按旧 key 命中,轮到发言的 bot 会拿到不含 speak 工具的缓存副本 ——
//     等同于工具集静默残缺(§130 式静默失效)。
//   - 新 key 追加 shapeExtra(toolsShapeExtra),覆盖全部影响工具形状的入参。
//     缓存按 per-Agent 持有(seat 在 Agent 生命周期内固定),key 仍带 seat
//     作为防御,避免未来误共享缓存实例时串味。
//
// 设计要点:
//   - 工具 targetEnum(目标座位列表)由 BuildTools 内部从 alive[] 动态生成,
//     所以 aliveHash 必须纳入 key(同 phase/role 但存活集合不同时,target enum 不同)。
//   - sync.RWMutex 保护,read 多 goroutine 并发,write 单飞。
//   - 容量上限 64(覆盖 8 phase × 8 role 组合),超过走头部覆盖(简化 LRU)。
//   - 房间销毁时缓存随 Agent 一并 GC(每 Agent 一份,seat 固定)。
//
// 与 §130 教训的兼容性:
//   - 测试双向验证:缓存命中结果 = 直接调用结果(完全一致,含 shapeExtra 变化后的失效)。
package wwplayer

import (
	"fmt"
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

// cacheKey 构造缓存键(兼容旧 3 段形式,等价于 cacheKeyEx(extra=""))。
func (c *ToolsCache) cacheKey(phase, role string, alive []int) string {
	return c.cacheKeyEx(phase, role, alive, "")
}

// cacheKeyEx 2026-08-13 §20260813-02 U2 — 4 段缓存键。
// extra 由 toolsShapeExtra 计算,覆盖 seat / speakTurn / gc 形状字段。
func (c *ToolsCache) cacheKeyEx(phase, role string, alive []int, extra string) string {
	return phase + "|" + role + "|" + aliveHash(alive) + "|" + extra
}

// Get 取缓存。未命中返回 (nil, false)。
func (c *ToolsCache) Get(phase, role string, alive []int) ([]llm.ToolDef, bool) {
	return c.GetEx(phase, role, alive, "")
}

// GetEx 取缓存(带形状扩展段)。未命中返回 (nil, false)。
func (c *ToolsCache) GetEx(phase, role string, alive []int, extra string) ([]llm.ToolDef, bool) {
	if c == nil {
		return nil, false
	}
	key := c.cacheKeyEx(phase, role, alive, extra)
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
	c.PutEx(phase, role, alive, "", tools)
}

// PutEx 写入缓存(带形状扩展段,自动容量控制)。
func (c *ToolsCache) PutEx(phase, role string, alive []int, extra string, tools []llm.ToolDef) {
	if c == nil {
		return
	}
	key := c.cacheKeyEx(phase, role, alive, extra)
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

// BuildToolsCached 是 BuildTools 的缓存版本(生产调用入口,2026-08-13
// §20260813-02 U2 接线:run.go 内层循环与 speak_floor 路径均走这里)。
//
// cache key = phase + role + aliveHash + toolsShapeExtra(seat, speakTurn, gc)。
// shapeExtra 覆盖所有影响工具集合 / schema / 动态描述的入参,保证缓存命中
// 与直调 BuildTools 字节一致(见 toolsShapeExtra 注释清单)。
//
// 返回的切片是缓存内部引用,调用方**不得**修改(否则污染后续读)。
func BuildToolsCached(cache *ToolsCache, phase, role string, seat int, alive []int, speakTurn int, gc *wwtypes.GameContext) []llm.ToolDef {
	extra := toolsShapeExtra(seat, speakTurn, gc)
	if tools, ok := cache.GetEx(phase, role, alive, extra); ok {
		return tools
	}
	tools := BuildTools(phase, role, seat, alive, speakTurn, gc)
	cache.PutEx(phase, role, alive, extra, tools)
	return tools
}

// toolsShapeExtra 2026-08-13 §20260813-02 U2 — 计算 BuildTools 的「形状指纹」。
//
// 凡能改变返回的 []llm.ToolDef(工具集合 / description 文本 / input_schema
// 枚举值)的入参都必须进入本指纹,漏一项 = 缓存命中返回过时工具集(§130
// 式静默失效)。当前覆盖清单(与 tools.go::BuildTools + registry MountIf /
// BuildDescription 逐条核对):
//
//   - seat                        filterSelf/onlySelf 枚举 + speakTurn==seat 判定
//   - speakTurn==seat             speak/finish_speak/knight_duel/wolf_suicide 是否挂载
//   - gc.GuardLastProtect         guard_protect 枚举剔除上晚守护目标
//   - min(gc.Round,2)             demon_hunter_hunt 首夜(Round<2)提示文案
//   - gc.SheriffSeat              sheriff_stream 是否挂载(== seat 且 role==seer)
//   - gc.SheriffStream[0],[1]     sheriff_stream description 文案
//   - gc.VoteProposed             propose_vote 是否挂载
//   - gc.DeathLyricCurrent        last_words/last_words_skip 是否挂载(== seat)
//   - hash(gc.SheriffCandidates)  sheriff vote 枚举
//   - gc.Faction                  wolf_whisper / wolfpack_assign MountIf
//   - gc.WolfTeammateSeat         wolf_whisper 历史 MountIf(防御性纳入)
//   - gc.WolfKingSeat             wolfpack_assign MountIf(== gc.MySeat 时挂载)
//   - hash(gc.PropSnapshot)       use_prop 动态描述 + prop_id 枚举 + 是否挂载
//   - len(gc.PropHistorySnapshot)>0  prop_history 是否挂载
//   - reasoningChainEnabled()     reasoning_chain 是否挂载(配置级,进程内近似静态)
//
// 新增 BuildTools 输入依赖时**必须**同步本函数 —— U5 wiring lint 不覆盖本
// 清单,靠 code review 与 tools_cache_wiring_test.go 的等价性断言兜底。
func toolsShapeExtra(seat, speakTurn int, gc *wwtypes.GameContext) string {
	mySpeakTurn := 0
	if speakTurn == seat {
		mySpeakTurn = 1
	}
	if gc == nil {
		return fmt.Sprintf("s%d|t%d|nil", seat, mySpeakTurn)
	}
	rcEnabled := 0
	if reasoningChainEnabled() {
		rcEnabled = 1
	}
	round := gc.Round
	if round > 2 {
		round = 2 // Round 只以 <2 / >=2 影响 demon_hunter 首夜文案,归一化提升命中率
	}
	return fmt.Sprintf("s%d|t%d|g%d|r%d|ss%d|st%d,%d|vp%d|dl%d|sc%s|f%s|wt%s|wk%d|ps%s|ph%d|rc%d",
		seat, mySpeakTurn,
		gc.GuardLastProtect,
		round,
		gc.SheriffSeat,
		gc.SheriffStream[0], gc.SheriffStream[1],
		boolToInt(gc.VoteProposed),
		gc.DeathLyricCurrent,
		intSliceHash(gc.SheriffCandidates),
		gc.Faction,
		intSliceHash(gc.WolfTeammateSeats),
		gc.WolfKingSeat,
		propSnapshotHash(gc.PropSnapshot),
		boolToInt(len(gc.PropHistorySnapshot) > 0),
		rcEnabled,
	)
}

// boolToInt 布尔 → 0/1(指纹拼接用)。
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// intSliceHash 把座位切片转为稳定哈希(排序后 FNV-1a),与 aliveHash 同算法。
func intSliceHash(xs []int) string {
	if len(xs) == 0 {
		return "empty"
	}
	cp := make([]int, len(xs))
	copy(cp, xs)
	sort.Ints(cp)
	h := fnv.New32a()
	for _, s := range cp {
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

// propSnapshotHash 把道具快照(影响 use_prop 动态描述与 prop_id 枚举)转为
// 稳定哈希。只哈希影响工具形状的字段(PropKey/Price/BaseHitRate/IsAOE),
// 名称与描述文案不参与(它们进 prompt 但不进 schema 形状 —— 实际上
// buildUsePropDynamicDescription 会用到名称,故一并纳入)。
func propSnapshotHash(snaps []wwtypes.PropSnapshot) string {
	if len(snaps) == 0 {
		return "empty"
	}
	h := fnv.New32a()
	for _, p := range snaps {
		_, _ = h.Write([]byte(fmt.Sprintf("%s|%s|%d|%d|%t|", p.PropKey, p.NameZh, p.Price, p.BaseHitRate, p.IsAOE)))
	}
	return uint32ToHex(h.Sum32())
}
