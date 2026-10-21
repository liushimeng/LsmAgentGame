// Package virtual_city — ab_stats_test.go: 批次73 O5 统计工具已知值测试
// (方案 §10:χ² / Welch 经典算例 + 解析锚点)。
package virtual_city

import (
	"math"
	"testing"
)

// TestABStats_ChiSquareKnownValues 卡方经典算例(无 Yates 校正)。
//
// a=30/100 vs b=18/100:2×2 {{30,70},{18,82}} ⇒ χ² = 200×1200²/(100×100×48×152)
// = 3.9474(1 df);p = P(χ²₁ ≥ 3.9474) = P(|Z| ≥ 1.9868) ≈ 0.0470。
func TestABStats_ChiSquareKnownValues(t *testing.T) {
	chi2, p := ChiSquareTwoProp(30, 100, 18, 100)
	if math.Abs(chi2-3.947368) > 1e-5 {
		t.Errorf("χ² = %f,期望 ≈ 3.947368", chi2)
	}
	if math.Abs(p-0.04700) > 1e-3 {
		t.Errorf("p = %f,期望 ≈ 0.0470", p)
	}
	// 完全相同比例 ⇒ χ² = 0,p = 1。
	chi2, p = ChiSquareTwoProp(30, 100, 30, 100)
	if chi2 != 0 || p != 1 {
		t.Errorf("同比例 χ²/p = %f/%f,期望 0/1", chi2, p)
	}
	// 极端差异:a 全中 b 全不中。
	chi2, p = ChiSquareTwoProp(100, 100, 0, 100)
	if chi2 < 100 || p > 1e-20 {
		t.Errorf("极端差异 χ²/p = %f/%f", chi2, p)
	}
	// 边界:n≤0 / 负计数 ⇒ (0,1) 不 panic。
	if chi2, p = ChiSquareTwoProp(0, 0, 0, 0); chi2 != 0 || p != 1 {
		t.Errorf("空样本应 (0,1),got %f/%f", chi2, p)
	}
	if chi2, p = ChiSquareTwoProp(-1, 100, 0, 100); chi2 != 0 || p != 1 {
		t.Errorf("负计数应 (0,1),got %f/%f", chi2, p)
	}
}

// TestABStats_WelchKnownValues Welch 解析锚点:
//
//	a = [0,2](均值 1,var 2),b = [3,5](均值 4,var 2):
//	t = (1−4)/√(2/2+2/2) = −3/√2 = −2.12132;
//	df = (1+1)²/(1²/1+1²/1) = 2(t 分布 df=2 有闭式 CDF:
//	F(t) = 1/2 + t/(2√(2+t²)));
//	双侧 p = 1 − |t|/√(2+t²) = 1 − 2.12132/√6.5 ≈ 0.167950。
func TestABStats_WelchKnownValues(t *testing.T) {
	tStat, df, p := WelchTTest([]float64{0, 2}, []float64{3, 5})
	if math.Abs(tStat-(-3.0/math.Sqrt2)) > 1e-12 {
		t.Errorf("t = %f,期望 %f", tStat, -3.0/math.Sqrt2)
	}
	if math.Abs(df-2) > 1e-9 {
		t.Errorf("df = %f,期望 2", df)
	}
	wantP := 1 - (3.0/math.Sqrt2)/math.Sqrt(6.5)
	if math.Abs(p-wantP) > 1e-9 {
		t.Errorf("p = %f,期望 %f(df=2 闭式)", p, wantP)
	}
	// 边界:n<2 ⇒ p=1;两方差 0 ⇒ p=1;不 panic。
	if _, _, p = WelchTTest([]float64{1}, []float64{2, 3}); p != 1 {
		t.Errorf("n<2 应 p=1,got %f", p)
	}
	if _, _, p = WelchTTest([]float64{1, 1}, []float64{2, 2}); p != 1 {
		t.Errorf("零方差应 p=1,got %f", p)
	}
}

// TestABStats_IncompleteBetaAnalytic 不完全贝塔解析锚点:
//
//   - I_x(1,1) = x;
//   - I_x(1/2,1/2) = (2/π)·arcsin(√x)。
func TestABStats_IncompleteBetaAnalytic(t *testing.T) {
	for _, x := range []float64{0.1, 0.25, 0.5, 0.77, 0.99} {
		if got := abBetai(1, 1, x); math.Abs(got-x) > 1e-12 {
			t.Errorf("I_%v(1,1) = %f,期望 %f", x, got, x)
		}
		want := 2 / math.Pi * math.Asin(math.Sqrt(0.25))
		if got := abBetai(0.5, 0.5, 0.25); math.Abs(got-want) > 1e-9 {
			t.Errorf("I_.25(.5,.5) = %f,期望 %f", got, want)
		}
	}
	// 端点。
	if abBetai(2, 3, 0) != 0 || abBetai(2, 3, 1) != 1 {
		t.Error("I_x 端点应 0/1")
	}
}
