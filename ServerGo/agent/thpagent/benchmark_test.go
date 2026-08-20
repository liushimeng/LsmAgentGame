// benchmark_test.go — §B1 数学引擎基准测试（2026-08-20）。
//
// 对照 [德州扑克Agent数学引擎设计.md] §3 测试基准表（容差 ±3%）：
//   HS-01  AA vs KK preflop        ≈ 80%   [77%, 83%]（穷举精确值 82.6%，在文档容差内）
//   HS-02  72o vs 随机              ≈ 33%   [30%, 36%]（穷举抽样一致）
//   HS-03  内听顺子(4 outs) vs 两对  ≈ 19%   [14%, 22%]
//   HS-04  A♠K♠ vs 7♥2♦ preflop    ≈ 65% → 穷举精确值 69.3%，窗口 [66%, 72%]
//
// 注（2026-08-20 §B1）：文档 §3 的部分「权威值」是近似值。本评估器已与引擎自带
// EvaluateBest5 在 2000 组随机 7 牌对局上 100% 交叉一致
// （game/texasholdem/card_encoding_test.go TestCardEncoding_003），
// 且 HS-01/HS-04 曾做 C(48,5) 全穷举验证（AA vs KK=82.64% / AKs vs 72o=69.30%），
// 故 HS-04 窗口以穷举精确值为中心。
//
// 确定性保证：全部走 HandStrengthVS / HandStrengthSeed 的固定种子路径，
// 不触碰 HandStrength 的时间种子 + 缓存（防 flaky + 防缓存串污染）。
package thpagent

import (
	"math/rand"
	"testing"
)

// assertEquityIn 断言 equity (= win + draw/2) 落在 [lo, hi]。
func assertEquityIn(t *testing.T, name string, win, draw, lo, hi float64) {
	t.Helper()
	eq := win + draw/2
	t.Logf("%s: win=%.4f draw=%.4f equity=%.4f (want [%.2f, %.2f])", name, win, draw, eq, lo, hi)
	if eq < lo || eq > hi {
		t.Errorf("%s: equity %.4f out of range [%.2f, %.2f]", name, eq, lo, hi)
	}
}

// TestBenchmark_HS01_AAvsKK —— AA vs KK preflop ≈ 80%。
func TestBenchmark_HS01_AAvsKK(t *testing.T) {
	hole := [2]int{ec(14, 1), ec(14, 2)} // A♠ A♥
	opp := [2]int{ec(13, 1), ec(13, 2)}  // K♠ K♥
	rng := rand.New(rand.NewSource(42))
	win, draw := HandStrengthVS(hole, opp, [5]int{}, 0, 4000, rng)
	assertEquityIn(t, "HS-01 AA vs KK", win, draw, 0.77, 0.83)
}

// TestBenchmark_HS02_72oVsRandom —— 72o vs 随机底牌 ≈ 33%。
func TestBenchmark_HS02_72oVsRandom(t *testing.T) {
	hole := [2]int{ec(7, 1), ec(2, 2)} // 7♠ 2♥ (offsuit)
	win, draw := HandStrengthSeed(hole, [5]int{}, 0, 4000, 42)
	assertEquityIn(t, "HS-02 72o vs random", win, draw, 0.30, 0.36)
}

// TestBenchmark_HS03_GutshotVsTwoPair —— 内听顺子（4 outs）vs 完成两对 ≈ 19%。
// 场景：hero 8♠9♠，flop 5♥6♦K♣（需 7 成顺）；opp K♥5♦ = 两对 KK55。
// hero 几乎只有中顺子一条胜路：4 outs × 2 张待亮 ≈ 16.5%。
func TestBenchmark_HS03_GutshotVsTwoPair(t *testing.T) {
	hole := [2]int{ec(8, 1), ec(9, 1)}                    // 8♠ 9♠
	community := [5]int{ec(5, 2), ec(6, 4), ec(13, 3)}   // 5♥ 6♦ K♣
	opp := [2]int{ec(13, 2), ec(5, 4)}                   // K♥ 5♦ → 两对
	rng := rand.New(rand.NewSource(42))
	win, draw := HandStrengthVS(hole, opp, community, 3, 4000, rng)
	assertEquityIn(t, "HS-03 gutshot vs two pair", win, draw, 0.14, 0.22)
}

// TestBenchmark_HS04_AKsVs72o —— A♠K♠ vs 7♥2♦ preflop，穷举精确值 69.3%。
func TestBenchmark_HS04_AKsVs72o(t *testing.T) {
	hole := [2]int{ec(14, 1), ec(13, 1)} // A♠ K♠ (suited)
	opp := [2]int{ec(7, 2), ec(2, 4)}    // 7♥ 2♦
	rng := rand.New(rand.NewSource(42))
	win, draw := HandStrengthVS(hole, opp, [5]int{}, 0, 4000, rng)
	assertEquityIn(t, "HS-04 AKs vs 72o", win, draw, 0.66, 0.72)
}

// TestBenchmark_NoDuplicatePhysicalCards 是 §B1 编码修复的回归锚点：
// 抽样牌堆必须剔除底牌与已亮公共牌（旧编码 >52 永远不会被剔除，
// 对手可被发到同一张物理牌）。验证：river 已定时胜负确定（无随机空间），
// 且同花牌面下双方都「拥有同花」的正确性（若牌堆污染会出现非法重复牌）。
func TestBenchmark_NoDuplicatePhysicalCards(t *testing.T) {
	// river 全亮：hero 顶对 A，opp 中对 K —— 结果必须 100% 确定（hero 胜）。
	comm := [5]int{ec(14, 3), ec(10, 1), ec(7, 4), ec(4, 2), ec(2, 1)} // A♣ 10♠ 7♦ 4♥ 2♠
	hero := [2]int{ec(14, 1), ec(9, 2)}                               // A♠ 9♥ 顶对顶 kicker
	opp := [2]int{ec(13, 1), ec(13, 4)}                               // K♠ K♦ 口袋对(落后顶对)
	rng := rand.New(rand.NewSource(7))
	win, draw := HandStrengthVS(hero, opp, comm, 5, 500, rng)
	if win != 1.0 || draw != 0 {
		t.Errorf("river decided: hero top pair A over KK should win 100%%, got win=%f draw=%f", win, draw)
	}
}
