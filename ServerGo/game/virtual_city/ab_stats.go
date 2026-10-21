// Package virtual_city — ab_stats.go: A/B 成对对照统计工具(批次73 O5,
// 框架 §6.3;纯函数,零依赖,零 rand)。
//
//   - ChiSquareTwoProp:2×2 列联表 χ²(无 Yates 校正);χ²₁ 精确 p 值经
//     标准库 math.Erfc(√(x/2))(1 df:χ² = Z² ⇒ P(χ²≥x) = erfc(√(x/2)));
//   - WelchTTest:Welch–Satterthwaite 自由度;t 分布双侧 p 经正则不完全
//     贝塔函数 I_x(df/2, 1/2) 计算(continued fraction,参考 Numerical
//     Recipes betai);n<2 / 方差皆 0 ⇒ p=1 不 panic。
package virtual_city

import "math"

// abAlpha 显著性水平(框架 §6.3:p<0.05 才认定改动有效)。
const abAlpha = 0.05

// ChiSquareTwoProp 两比例卡方检验(2×2 无 Yates 校正):
//
//	a 组:aYes / aN;b 组:bYes / bN。
//
// 返回 (χ², p)。任一组 n≤0 ⇒ (0, 1) 不 panic。χ²₁ 精确 p = erfc(√(x/2))。
func ChiSquareTwoProp(aYes, aN, bYes, bN int) (chi2, p float64) {
	if aN <= 0 || bN <= 0 {
		return 0, 1
	}
	aNo, bNo := aN-aYes, bN-bYes
	if aNo < 0 || bNo < 0 {
		return 0, 1
	}
	// 2×2:{{aYes, aNo}, {bYes, bNo}};χ² = n(ad−bc)²/(R1 R2 C1 C2)。
	n := float64(aN + bN)
	r1, r2 := float64(aYes+aNo), float64(bYes+bNo)
	c1, c2 := float64(aYes+bYes), float64(aNo+bNo)
	den := r1 * r2 * c1 * c2
	if den <= 0 {
		return 0, 1 // 某一边际和为 0 ⇒ 无变异 ⇒ 无差异
	}
	adbc := float64(aYes*bNo - aNo*bYes)
	chi2 = n * adbc * adbc / den
	if chi2 < 0 || math.IsNaN(chi2) {
		return 0, 1
	}
	// χ²₁ ⇒ Z = √χ²;P(|Z| ≥ z) = erfc(z/√2) = erfc(√(χ²/2))。
	p = math.Erfc(math.Sqrt(chi2 / 2))
	return chi2, clamp01(p)
}

// WelchTTest 两独立样本 Welch t 检验(双侧):
//
//	t = (m_a − m_b) / √(s_a²/n_a + s_b²/n_b),
//	df = (v_a + v_b)² / (v_a²/(n_a−1) + v_b²/(n_b−1)),v = s²/n。
//
// 双侧 p = I_{df/(df+t²)}(df/2, 1/2)(正则不完全贝塔)。n<2 或两方差组皆 0
// ⇒ (0, 0, 1) 不 panic。
func WelchTTest(a, b []float64) (t, df, p float64) {
	na, nb := len(a), len(b)
	if na < 2 || nb < 2 {
		return 0, 0, 1
	}
	ma, mb := mcMean(a), mcMean(b)
	va := sampleVarOverN(a, ma)
	vb := sampleVarOverN(b, mb)
	den := va + vb
	if den <= 0 {
		return 0, 0, 1 // 两方差皆 0 ⇒ 均值差为 0(常量样本)⇒ 无差异
	}
	t = (ma - mb) / math.Sqrt(den)
	// Welch–Satterthwaite 自由度。
	num := den * den
	dfDen := 0.0
	if va > 0 {
		dfDen += va * va / float64(na-1)
	}
	if vb > 0 {
		dfDen += vb * vb / float64(nb-1)
	}
	if dfDen <= 0 {
		return t, 0, 1
	}
	df = num / dfDen
	// 双侧 p:t 分布 ⇒ p = I_x(df/2, 1/2),x = df/(df+t²)。
	x := df / (df + t*t)
	p = abBetai(df/2, 0.5, x)
	return t, df, clamp01(p)
}

// sampleVarOverN 样本方差 ÷ n(v = s²/n;Welch 分母项)。
func sampleVarOverN(xs []float64, mean float64) float64 {
	n := len(xs)
	if n < 2 {
		return 0
	}
	var ss float64
	for _, v := range xs {
		d := v - mean
		ss += d * d
	}
	return ss / float64(n-1) / float64(n)
}

// ── 正则不完全贝塔函数 I_x(a,b)(Numerical Recipes betai)──────────────

const (
	abBetaiMaxIter = 200
	abBetaiEps     = 3.0e-14
	abFPMIN        = 1.0e-300
)

// abBetai 正则不完全贝塔函数 I_x(a,b) = B_x(a,b) / B(a,b)。
// x≤0 ⇒ 0;x≥1 ⇒ 1。continued fraction(abBetacf)。
func abBetai(a, b, x float64) float64 {
	if x <= 0 {
		return 0
	}
	if x >= 1 {
		return 1
	}
	bt := math.Exp(abLnGamma(a+b) - abLnGamma(a) - abLnGamma(b) +
		a*math.Log(x) + b*math.Log(1-x))
	if x < (a+1)/(a+b+2) {
		return bt * abBetacf(a, b, x) / a
	}
	return 1 - bt*abBetacf(b, a, 1-x)/b
}

// abBetacf 不完全贝塔 continued fraction(Lentz 算法;Numerical Recipes)。
func abBetacf(a, b, x float64) float64 {
	qab := a + b
	qap := a + 1
	qam := a - 1
	c := 1.0
	d := 1 - qab*x/qap
	if math.Abs(d) < abFPMIN {
		d = abFPMIN
	}
	d = 1 / d
	h := d
	for m := 1; m <= abBetaiMaxIter; m++ {
		m2 := 2 * m
		aa := float64(m) * (b - float64(m)) * x / ((qam + float64(m2)) * (a + float64(m2)))
		d = 1 + aa*d
		if math.Abs(d) < abFPMIN {
			d = abFPMIN
		}
		c = 1 + aa/c
		if math.Abs(c) < abFPMIN {
			c = abFPMIN
		}
		d = 1 / d
		h *= d * c
		aa = -(a + float64(m)) * (qab + float64(m)) * x / ((a + float64(m2)) * (qap + float64(m2)))
		d = 1 + aa*d
		if math.Abs(d) < abFPMIN {
			d = abFPMIN
		}
		c = 1 + aa/c
		if math.Abs(c) < abFPMIN {
			c = abFPMIN
		}
		d = 1 / d
		del := d * c
		h *= del
		if math.Abs(del-1) < abBetaiEps {
			break
		}
	}
	return h
}

// abLnGamma ln Γ(x)(Lanczos 近似;Numerical Recipes gammln)。
func abLnGamma(x float64) float64 {
	cof := [6]float64{76.18009172947146, -86.50532032941677,
		24.01409824083091, -1.231739572450155, 0.1208650973866179e-2, -0.5395239384953e-5}
	xx, y := x, x
	tmp := x + 5.5
	tmp -= (x + 0.5) * math.Log(tmp)
	ser := 1.000000000190015
	for j := 0; j < 6; j++ {
		y++
		ser += cof[j] / y
	}
	return -tmp + math.Log(2.5066282746310005*ser/xx)
}
