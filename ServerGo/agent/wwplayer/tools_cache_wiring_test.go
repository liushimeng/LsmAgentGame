// Package wwplayer — tools_cache_wiring_test.go: §20260813-02 U2 接线与 key 正确性测试。
//
// 缺陷背景:
//  1. BuildToolsCached 20260813 新增但零生产调用(§130 死代码);
//  2. 旧 cache key 只有 (phase, role, aliveHash) —— 但 BuildTools 的工具集合
//     同时依赖 seat / speakTurn / gc(GuardLastProtect 等),按旧 key 命中会
//     返回「轮到发言却不含 speak 工具」的静默残缺工具集。
//
// 断言清单:
//
//	U2-01 命中等价:同输入两次调用,第二次命中缓存且与直调 BuildTools 深度一致
//	U2-02 alive 失效:存活集合变化 → 换 key → target enum 不同
//	U2-03 speakTurn 失效(核心缺陷捕捉):轮到发言时必须出现 speak 工具
//	U2-04 GuardLastProtect 失效:上晚守护目标变化 → guard_protect enum 不同
//	U2-05 多阶段等价矩阵:各 phase/role/gc 变体缓存命中结果与直调一致
package wwplayer

import (
	"reflect"
	"testing"

	"LsmAgentGame/agent/wwtypes"
	"LsmAgentGame/llm"
)

func toolNames(tools []llm.ToolDef) map[string]bool {
	out := make(map[string]bool, len(tools))
	for _, t := range tools {
		out[t.Name] = true
	}
	return out
}

// TestToolsCacheWiring_U2_01_HitIdentical 同输入两次调用 → 第二次命中,
// 且命中结果与直调 BuildTools 深度一致(字节稳定是 prompt cache 前缀命中前提)。
func TestToolsCacheWiring_U2_01_HitIdentical(t *testing.T) {
	c := NewToolsCache()
	gc := &wwtypes.GameContext{MySeat: 2, Round: 2}
	alive := []int{0, 1, 2, 3, 4}
	first := BuildToolsCached(c, "speak", "villager", 2, alive, 3, gc)
	second := BuildToolsCached(c, "speak", "villager", 2, alive, 3, gc)
	direct := BuildTools("speak", "villager", 2, alive, 3, gc)
	if !reflect.DeepEqual(first, direct) {
		t.Fatal("first (miss) result must deep-equal direct BuildTools")
	}
	if !reflect.DeepEqual(second, direct) {
		t.Fatal("second (hit) result must deep-equal direct BuildTools")
	}
	hits, _, _ := c.Stats()
	if hits != 1 {
		t.Fatalf("cache hits = %d, want 1 (second call must hit)", hits)
	}
}

// TestToolsCacheWiring_U2_02_AliveChangeInvalidates 存活集合变化 → 缓存失效,
// 新结果的 target enum 反映新的 alive(旧 key 已覆盖,防回归)。
func TestToolsCacheWiring_U2_02_AliveChangeInvalidates(t *testing.T) {
	c := NewToolsCache()
	gc := &wwtypes.GameContext{MySeat: 0, Round: 1}
	r1 := BuildToolsCached(c, "vote", "villager", 0, []int{0, 1, 2, 3}, -1, gc)
	r2 := BuildToolsCached(c, "vote", "villager", 0, []int{0, 2, 3}, -1, gc)
	if reflect.DeepEqual(r1, r2) {
		t.Fatal("alive change must invalidate cache (vote target enum differs)")
	}
}

// TestToolsCacheWiring_U2_03_SpeakTurnInvalidates 【核心缺陷捕捉】。
// 先在「没轮到我」时填充缓存,再轮到我说话 —— 若 key 不含 speakTurn(旧实现),
// 第二次会命中旧缓存,工具集里**没有 speak**,bot 永远无法正式发言。
// 双向验证:把 BuildToolsCached 退回 Get/Put(3 段 key)→ 本测试必失败。
func TestToolsCacheWiring_U2_03_SpeakTurnInvalidates(t *testing.T) {
	c := NewToolsCache()
	gc := &wwtypes.GameContext{MySeat: 2, Round: 1}
	alive := []int{0, 1, 2, 3, 4}
	// 没轮到我(speakTurn=3,我 seat=2)→ 缓存不含 speak 的版本。
	notMine := BuildToolsCached(c, "speak", "villager", 2, alive, 3, gc)
	if toolNames(notMine)["speak"] {
		t.Fatal("sanity: speak tool must NOT be offered when speakTurn != seat")
	}
	// 轮到我了(speakTurn=2)→ 必须出现 speak,绝不能命中上一行的缓存。
	mine := BuildToolsCached(c, "speak", "villager", 2, alive, 2, gc)
	if !toolNames(mine)["speak"] {
		t.Fatal("speak tool MUST appear when speakTurn == seat — cache key missing speakTurn (stale hit)")
	}
	if !toolNames(mine)["finish_speak"] {
		t.Fatal("finish_speak tool MUST appear when speakTurn == seat")
	}
	direct := BuildTools("speak", "villager", 2, alive, 2, gc)
	if !reflect.DeepEqual(mine, direct) {
		t.Fatal("my-turn result must deep-equal direct BuildTools")
	}
}

// TestToolsCacheWiring_U2_04_GuardLastProtectInvalidates 上晚守护目标变化 →
// guard_protect 枚举必须随之剔除新目标(§134 enum 剔除优于事后报错)。
func TestToolsCacheWiring_U2_04_GuardLastProtectInvalidates(t *testing.T) {
	c := NewToolsCache()
	alive := []int{0, 1, 2, 3, 4}
	gc1 := &wwtypes.GameContext{MySeat: 0, Round: 2, GuardLastProtect: 1}
	gc2 := &wwtypes.GameContext{MySeat: 0, Round: 2, GuardLastProtect: 2}
	r1 := BuildToolsCached(c, "night_guard", "guard", 0, alive, -1, gc1)
	r2 := BuildToolsCached(c, "night_guard", "guard", 0, alive, -1, gc2)
	if reflect.DeepEqual(r1, r2) {
		t.Fatal("GuardLastProtect change must invalidate cache (guard_protect enum differs)")
	}
	if len(r2) == 0 {
		t.Fatal("guard must have guard_protect tool in night_guard")
	}
}

// TestToolsCacheWiring_U2_05_EquivalenceMatrix 多阶段/角色/gc 变体下,
// 缓存 miss 与 hit 结果都必须与直调 BuildTools 深度一致。
func TestToolsCacheWiring_U2_05_EquivalenceMatrix(t *testing.T) {
	c := NewToolsCache()
	alive := []int{0, 1, 2, 3, 4, 5}
	cases := []struct {
		phase, role string
		seat        int
		speakTurn   int
		gc          *wwtypes.GameContext
	}{
		{"night_wolves", "werewolf", 1, -1, &wwtypes.GameContext{MySeat: 1, Round: 1, Faction: "wolf", WolfTeammateSeat: 3}},
		{"night_seer", "seer", 2, -1, &wwtypes.GameContext{MySeat: 2, Round: 2}},
		{"night_witch", "witch", 3, -1, &wwtypes.GameContext{MySeat: 3, Round: 2}},
		{"speak", "villager", 4, 4, &wwtypes.GameContext{MySeat: 4, Round: 3}},
		{"speak", "seer", 2, 5, &wwtypes.GameContext{MySeat: 2, Round: 3, VoteProposed: false}},
		{"vote", "villager", 0, -1, &wwtypes.GameContext{MySeat: 0, Round: 3}},
		{"sheriff", "villager", 1, -1, &wwtypes.GameContext{MySeat: 1, Round: 1, SheriffCandidates: []int{1, 3}}},
		{"death_lyric", "hunter", 5, -1, &wwtypes.GameContext{MySeat: 5, Round: 3, DeathLyricCurrent: 5}},
		{"night_demon_hunter", "demon_hunter", 5, -1, &wwtypes.GameContext{MySeat: 5, Round: 1}},
		{"night_demon_hunter", "demon_hunter", 5, -1, &wwtypes.GameContext{MySeat: 5, Round: 2}},
	}
	for i, tc := range cases {
		miss := BuildToolsCached(c, tc.phase, tc.role, tc.seat, alive, tc.speakTurn, tc.gc)
		hit := BuildToolsCached(c, tc.phase, tc.role, tc.seat, alive, tc.speakTurn, tc.gc)
		direct := BuildTools(tc.phase, tc.role, tc.seat, alive, tc.speakTurn, tc.gc)
		if !reflect.DeepEqual(miss, direct) {
			t.Fatalf("case %d (%s/%s): miss result != direct BuildTools", i, tc.phase, tc.role)
		}
		if !reflect.DeepEqual(hit, direct) {
			t.Fatalf("case %d (%s/%s): hit result != direct BuildTools", i, tc.phase, tc.role)
		}
	}
}
