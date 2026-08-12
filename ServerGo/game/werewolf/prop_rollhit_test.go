// Package werewolf — prop_rollhit_test.go: §20260810-02 E1 验证。
//
// 覆盖 rollHit 的「富人光环」修正（使用者余额 > 2000 → 中招率 +5%）：
// 该修正此前只写在函数注释与设计文档 §1.3 里，代码从未实现
// （K3-Surpport-01 §1 F4「道具骰点注释与实现不符」）。
//
// 由于 rollHit 内部使用 rand，本测试用「基础率 0 / 基础率 100」两个确定性边界
// 来断言修正是否被计入，避免依赖随机数种子：
//   - BaseHitRate=0 且余额 ≤ 2000 → rate=0 → 恒不中招
//   - BaseHitRate=0 且余额 > 2000 → rate=5 → 多次采样必然出现至少一次中招
package werewolf

import "testing"

// newRollHitTestRoom 构造一个最小房间：单个存活真人座位（不走 consecutiveFailures 分支）。
func newRollHitTestRoom() *WerewolfRoom {
	gs := NewGame(7)
	gs.SeatCount = 3
	for i := 0; i < 3; i++ {
		gs.Players[i].Alive = true
		gs.Players[i].IsBot = false
		gs.Roles[i] = RoleVillager
	}
	return &WerewolfRoom{
		RoomID:      "rollhit-test-room",
		State:       gs,
		propCatalog: BuildDefaultPropCatalog(),
	}
}

// TestRollHit_E1_RichAuraNotAppliedBelowThreshold 余额未超门槛时不加成。
// BaseHitRate=0 → rate 恒为 0 → 任意次采样都不应中招。
func TestRollHit_E1_RichAuraNotAppliedBelowThreshold(t *testing.T) {
	r := newRollHitTestRoom()
	e := &PropEngine{}
	entry := &PropCatalogEntry{BaseHitRate: 0}

	for _, bal := range []int64{0, 1000, propRichAuraBalance} { // 注意：== 门槛不触发（严格大于）
		for i := 0; i < 200; i++ {
			if e.rollHit(entry, r, 1, bal) {
				t.Fatalf("balance=%d 未超门槛(%d)，BaseHitRate=0 时不应中招", bal, propRichAuraBalance)
			}
		}
	}
}

// TestRollHit_E1_RichAuraAppliedAboveThreshold 余额超门槛时 +5% 生效。
// BaseHitRate=0 + 富人光环 → rate=5 → 2000 次采样中命中概率极高（漏报概率 ~1e-45）。
func TestRollHit_E1_RichAuraAppliedAboveThreshold(t *testing.T) {
	r := newRollHitTestRoom()
	e := &PropEngine{}
	entry := &PropCatalogEntry{BaseHitRate: 0}

	hits := 0
	for i := 0; i < 2000; i++ {
		if e.rollHit(entry, r, 1, propRichAuraBalance+1) {
			hits++
		}
	}
	if hits == 0 {
		t.Fatalf("balance>%d 时应有 %d%% 中招率，2000 次采样却 0 命中 —— 富人光环修正未生效",
			propRichAuraBalance, propRichAuraBonus)
	}
}

// TestRollHit_E1_CapStillEnforced 富人光环叠加后仍受 70% 上限保护。
// BaseHitRate=100 + 5 = 105 → 封顶 70 → 必须存在不中招的采样。
func TestRollHit_E1_CapStillEnforced(t *testing.T) {
	r := newRollHitTestRoom()
	e := &PropEngine{}
	entry := &PropCatalogEntry{BaseHitRate: 100}

	misses := 0
	for i := 0; i < 2000; i++ {
		if !e.rollHit(entry, r, 1, propRichAuraBalance+1) {
			misses++
		}
	}
	if misses == 0 {
		t.Fatalf("中招率必须被 propHitRateCap(%d%%) 封顶，2000 次采样却 0 次未中招", propHitRateCap)
	}
}
