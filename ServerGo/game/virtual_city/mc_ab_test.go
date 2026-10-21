// Package virtual_city — mc_ab_test.go: 批次73 O5 A/B 成对对照测试
// (方案 §10 T-AB1–T-AB4)。
package virtual_city

import (
	"reflect"
	"testing"
)

// TestMCAB_NilOverrideEqualsDefault T-AB1:MCRunOneWith(seed, strategy, nil)
// 与 MCRunOne(seed, strategy) 逐字段相等(默认路径零行为差异)。
func TestMCAB_NilOverrideEqualsDefault(t *testing.T) {
	for _, s := range []string{MCStrategyBalanced, MCStrategyStress} {
		seed := MCSeedFor(s, 5)
		a := MCRunOne(seed, s)
		var nilOv *MCOverride // 显式 nil 指针
		b := MCRunOneWith(seed, s, nilOv)
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("T-AB1 失败(%s):nil override 与默认不等\na=%+v\nb=%+v", s, a, b)
		}
		// 空结构体(全 nil 字段)亦等价。
		c := MCRunOneWith(seed, s, &MCOverride{})
		if !reflect.DeepEqual(a, c) {
			t.Fatalf("T-AB1 失败(%s):空 override 与默认不等", s)
		}
	}
}

// TestMCAB_PairedDeterminism T-AB2:配对确定性 —— 同种子同覆盖双跑逐字段
// 相等(MCCompare 输入前提)。
func TestMCAB_PairedDeterminism(t *testing.T) {
	mul := 1.2
	ov := MCOverride{InitialCashMul: &mul}
	seed := MCSeedFor(MCStrategyBalanced, 2)
	a := MCRunOneWith(seed, MCStrategyBalanced, &ov)
	b := MCRunOneWith(seed, MCStrategyBalanced, &ov)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("T-AB2 失败:同种子同覆盖双跑不一致")
	}
	// 覆盖确实生效(初始现金缩放 ⇒ 世界注入总额变化 ⇒ 结果可不同)。
	def := MCRunOneWith(seed, MCStrategyBalanced, nil)
	if reflect.DeepEqual(a, def) {
		t.Log("注:该 seed 下 InitialCashMul=1.2 与默认结果恰好全等(允许,逐字段断言已钉死确定性)")
	}
}

// TestMCAB_InitialCashMonotonic T-AB3:InitialCashMul=1.2 ⇒ D5 破产率不降
// (单调方向:钱更多 ⇒ 破产不更多;配对种子带双侧各 6 局,样本小仅验单调
// 聚合方向与报告形态)。
func TestMCAB_InitialCashMonotonic(t *testing.T) {
	mul := 1.2
	rep := MCCompare(MCStrategyBalanced, MCOverride{}, MCOverride{InitialCashMul: &mul}, 6)
	if rep.N != 6 || rep.Strategy != MCStrategyBalanced {
		t.Fatalf("报告形态不合规:%+v", rep)
	}
	if rep.VariantD5 > rep.BaseD5+1e-9 {
		t.Errorf("T-AB3:初始现金 +20%% 后 D5 上升(%f → %f),违反单调方向", rep.BaseD5, rep.VariantD5)
	}
	if !rep.Underpowered {
		t.Error("n=6 < 20 应标 underpowered")
	}
	t.Logf("T-AB3 报告:D5 %.3f→%.3f Δ%.3f χ²=%.3f p=%.4f | D6 %.3f→%.3f t=%.3f df=%.1f p=%.4f | guard=%v verdict=%s",
		rep.BaseD5, rep.VariantD5, rep.DeltaD5, rep.Chi2, rep.Chi2P,
		rep.BaseD6, rep.VariantD6, rep.T, rep.Df, rep.TP, rep.GuardBreach, rep.Verdict)
}

// TestMCAB_Boundary T-AB4:n=0 / n=1 边界不 panic;n=0 出空报告。
func TestMCAB_Boundary(t *testing.T) {
	rep := MCCompare(MCStrategyBalanced, MCOverride{}, MCOverride{}, 0)
	if rep.N != 0 || rep.Verdict != ABVerdictNoDifference {
		t.Errorf("n=0 应出空报告:%+v", rep)
	}
	rep = MCCompare(MCStrategyBalanced, MCOverride{}, MCOverride{}, 1) // 不 panic 即过
	if rep.N != 1 || !rep.Underpowered {
		t.Errorf("n=1 报告形态:%+v", rep)
	}
	// 未知策略 ⇒ 空报告。
	rep = MCCompare("no_such", MCOverride{}, MCOverride{}, 10)
	if rep.N != 0 {
		t.Errorf("未知策略应出空报告:%+v", rep)
	}
	// StockBoomMonthly / RecessionMinMonths 覆盖不 panic 且确定性。
	boom := 0.05
	rec := 36
	seed := MCSeedFor(MCStrategyAggressiveLeverage, 1)
	x := MCRunOneWith(seed, MCStrategyAggressiveLeverage, &MCOverride{StockBoomMonthly: &boom, RecessionMinMonths: &rec})
	y := MCRunOneWith(seed, MCStrategyAggressiveLeverage, &MCOverride{StockBoomMonthly: &boom, RecessionMinMonths: &rec})
	if !reflect.DeepEqual(x, y) {
		t.Error("市场参数覆盖双跑不一致")
	}
}
