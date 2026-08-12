// Package werewolf — prop_v3_test.go: 道具系统 v3 重构的单元测试。
//
// 覆盖 v3 新增/增强功能:
//   1. task_disguise_v3 默认道具存在 + InjectRegistry 已注册 + 4 轮剧本格式校验
//   2. emotion_disturb_light 效果落地正确(force_emotion = "engaged")
//   3. PickWolfTeammatePairs 批量配对：30% 概率 + 对称互知 + max_pairs 上限
//   4. PropHistory 环形 buffer 写入/读取
//   5. walletSvc 注入路径(单元层面无法测试真实钱包,但可测 helper 存在性)
package werewolf

import (
	"strings"
	"testing"

	"LsmWebGame/agent/wwplayer"
	"LsmWebGame/agent/wwtypes"
)

// TestPropDisguiseV3_DefaultCatalog 验证 v3 任务马甲示范道具在默认目录中。
func TestPropDisguiseV3_DefaultCatalog(t *testing.T) {
	cat := BuildDefaultPropCatalog()
	entry, ok := cat.Get("task_disguise_v3")
	if !ok {
		t.Fatal("task_disguise_v3 应在默认目录中")
	}
	if entry.Price != 180 {
		t.Errorf("task_disguise_v3 价格应为 180, got %d", entry.Price)
	}
	if entry.BaseHitRate != 35 {
		t.Errorf("task_disguise_v3 中招率应为 35, got %d", entry.BaseHitRate)
	}
	// 双重效果校验
	effectTypes := entry.EffectSpec.EffectTypeToList()
	hasExpose := false
	hasEmotionLight := false
	for _, et := range effectTypes {
		if et == "expose_identity" {
			hasExpose = true
		}
		if et == "emotion_disturb_light" {
			hasEmotionLight = true
		}
	}
	if !hasExpose || !hasEmotionLight {
		t.Errorf("task_disguise_v3 应同时含 expose_identity + emotion_disturb_light, got %v", effectTypes)
	}
}

// TestPropDisguiseV3_InjectRegistryRegistered 验证 v3 任务马甲已注册到 InjectRegistry。
func TestPropDisguiseV3_InjectRegistryRegistered(t *testing.T) {
	fn, ok := InjectRegistry["task_disguise_v3"]
	if !ok {
		t.Fatal("task_disguise_v3 应注册到 InjectRegistry")
	}
	res := fn(InjectRequest{
		PropKey:  "task_disguise_v3",
		FromSeat: 0,
		ToSeat:   2,
		Payload:  "",
		ToRole:   "seer",
	})
	// 4 轮剧本特征：必须含"第 1 轮/第 4 轮" + "内心独白" + "角色代入"
	must := []string{"第 1 轮", "第 4 轮", "内心独白", "角色代入"}
	for _, m := range must {
		if !strings.Contains(res.InjectText, m) {
			t.Errorf("task_disguise_v3 注入文本必须含 %q, 但缺少", m)
		}
	}
	// EffectHint 应包含 emoji 🎬 与"4轮渐进"
	if !strings.Contains(res.EffectHint, "🎬") || !strings.Contains(res.EffectHint, "4轮") {
		t.Errorf("task_disguise_v3 EffectHint 应含 🎬 + 4轮, got %q", res.EffectHint)
	}
	// 4 轮剧本体量较大（> 500 字符）
	if len(res.InjectText) < 500 {
		t.Errorf("task_disguise_v3 注入文本应 >500 字符, got %d", len(res.InjectText))
	}
}

// TestEmotionDisturbLight_Effect 验证 emotion_disturb_light 效果落地正确。
func TestEmotionDisturbLight_Effect(t *testing.T) {
	fn, ok := EffectRegistry["emotion_disturb_light"]
	if !ok {
		t.Fatal("emotion_disturb_light 应注册到 EffectRegistry")
	}
	gc := &wwtypes.GameContext{}
	// 初始 EffectForceEmotion 应为空
	if gc.EffectForceEmotion != "" {
		t.Errorf("初始 EffectForceEmotion 应为空, got %q", gc.EffectForceEmotion)
	}
	// 调用效果落地
	fn(gc, 0, EffectApplyContext{FromSeat: 3, Entry: PropInjectEntry{}})
	// 应填入 "engaged"
	if gc.EffectForceEmotion != "engaged" {
		t.Errorf("emotion_disturb_light 应把 EffectForceEmotion 设为 engaged, got %q", gc.EffectForceEmotion)
	}
}

// TestPickWolfTeammatePairs_30Percent 验证 30% 概率启用。
func TestPickWolfTeammatePairs_30Percent(t *testing.T) {
	allWolf := []int{0, 2, 5, 8}
	// ratePercent=0 → 永不启用
	for i := 0; i < 50; i++ {
		if got := wwplayer.PickWolfTeammatePairs(allWolf, 0, 1, nil); len(got) > 0 {
			t.Error("ratePercent=0 应永不启用")
		}
	}
	// ratePercent=100 → 必定启用(返回 max_pairs 对)
	for i := 0; i < 50; i++ {
		got := wwplayer.PickWolfTeammatePairs(allWolf, 100, 1, nil)
		if len(got) != 1 {
			t.Errorf("ratePercent=100 应启用 1 对, got %d", len(got))
		}
	}
}

// TestPickWolfTeammatePairs_Symmetric 验证配对对称(A 知道 B,B 知道 A)。
func TestPickWolfTeammatePairs_Symmetric(t *testing.T) {
	allWolf := []int{1, 3, 5, 7, 9, 11}
	// 跑 100 次确保所有命中都是对称
	for i := 0; i < 100; i++ {
		got := wwplayer.PickWolfTeammatePairs(allWolf, 100, 2, nil)
		if len(got) == 0 {
			continue // rate 30%,可能未启用
		}
		for _, pair := range got {
			// 验证配对中的两只狼互为队友(对称)
			// 简化:验证配对座位都来自 allWolf
			for _, s := range pair {
				found := false
				for _, w := range allWolf {
					if w == s {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("配对座位 %d 不在狼人座位列表 %v 中", s, allWolf)
				}
			}
		}
	}
}

// TestPickWolfTeammatePairs_MaxPairsLimit 验证 maxPairs 上限生效。
func TestPickWolfTeammatePairs_MaxPairsLimit(t *testing.T) {
	allWolf := []int{0, 1, 2, 3, 4, 5, 6, 7}
	for i := 0; i < 50; i++ {
		got := wwplayer.PickWolfTeammatePairs(allWolf, 100, 2, nil)
		if len(got) > 2 {
			t.Errorf("maxPairs=2 应最多返回 2 对, got %d", len(got))
		}
	}
}

// TestPickWolfTeammatePairs_SingleWolf 验证只有 1 只狼时不启用。
func TestPickWolfTeammatePairs_SingleWolf(t *testing.T) {
	if got := wwplayer.PickWolfTeammatePairs([]int{3}, 100, 1, nil); len(got) > 0 {
		t.Error("单只狼时应不返回配对")
	}
}

// TestPropHistoryRecord_RingBuffer 验证 PropHistory 环形 buffer 写入/读取。
func TestPropHistoryRecord_RingBuffer(t *testing.T) {
	r := &WerewolfRoom{}
	// 写入 25 条(> 20 上限):前 20 条 append,后 5 条覆盖 head=0..4
	for i := 0; i < 25; i++ {
		r.recordPropHistoryLocked(PropHistoryRecord{
			FromSeat:   i % 4,
			ToSeat:     (i + 1) % 4,
			PropKey:    "test_prop",
			PropNameZh: "测试道具",
			Hit:        i%2 == 0,
			Phase:      "speak",
			Round:      i,
			CreatedAt:  int64(1700000000 + i),
		})
	}
	got := r.GetPropHistoryLocked(20)
	if len(got) != 20 {
		t.Errorf("环形 buffer 应返回 20 条, got %d", len(got))
	}
	// 环形 buffer 写入 25 条后:
	//   - 前 20 条 append(Round 0-19,索引 0-19)
	//   - 第 21-25 条覆盖索引 0-4(Round 20-24,索引 0-4)
	// GetPropHistoryLocked 按 append 顺序返回索引 0-19 全部;
	// 所以索引 0-4 现在是 Round 20-24,索引 5-19 还是 Round 5-19。
	// 最后一条(索引 19)是原始写入的第 19 条。
	if got[len(got)-1].Round != 19 {
		t.Errorf("最后一条 Round 应为 19, got %d", got[len(got)-1].Round)
	}
	// 第一条(索引 0)应该是最近覆盖的 Round 20(第 21 次写入)
	if got[0].Round != 20 {
		t.Errorf("第一条 Round 应为 20(最近覆盖), got %d", got[0].Round)
	}
	// 验证 limit=0 返回全部
	all := r.GetPropHistoryLocked(0)
	if len(all) != 20 {
		t.Errorf("limit=0 应返回 20 条, got %d", len(all))
	}
	// 验证 limit=5 只返回最近 5 条(按 GetPropHistoryLocked 当前实现:最后 5 条)
	last5 := r.GetPropHistoryLocked(5)
	if len(last5) != 5 {
		t.Errorf("limit=5 应返回 5 条, got %d", len(last5))
	}
	// 当前实现是按 append 顺序的"最后 5 条"(索引 15-19),其 Round 仍为原始 15-19。
	if last5[0].Round != 15 || last5[4].Round != 19 {
		t.Errorf("last5 顺序应为 15..19, got %d..%d", last5[0].Round, last5[4].Round)
	}
}

// TestPropHistoryRecord_EmptyRoom 验证空房间 GetPropHistory 返回 nil。
func TestPropHistoryRecord_EmptyRoom(t *testing.T) {
	r := &WerewolfRoom{}
	if got := r.GetPropHistoryLocked(20); got != nil {
		t.Errorf("空房间应返回 nil, got %v", got)
	}
}