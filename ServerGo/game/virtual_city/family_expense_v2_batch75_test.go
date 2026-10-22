// Package virtual_city — family_expense_v2_batch75_test.go: 批次75 B75-2
// 「家庭支出重复计入」修正的验收测试(方案 §3.7 / §5.1 T15–T17)。
//
// 事实基础:profession.Card.Expense(=卡面 monthly_expense)= **家庭总生活消费**
// (含初始配偶/子女/赡养;批次74 方案 §2.3 用 4000 张真实卡实证:
// monthly_expense/income 随子女数上升 0.541 → 0.685 → 0.699)。
// 引擎旧口径在 living(=Expense × infl × 档位乘数)之上**再叠加**
// 「配偶 2,000 + 每孩 5,000 + 赡养 1,000/位」⇒ 重复计入
// (批次74 实测:已婚有孩卡 median 总生活支出/收入 = 1.16)。
//
// v2 口径(BalanceV2Enabled=true):
//   - 步骤5 家庭加项只计**游戏内新增成员**(新配偶 / 新生儿);
//   - 恩格尔切分改为按成员构成权重比例切分(金额守恒)。
// false(BalanceV2Enabled=false)⇒ 旧口径常量叠加原样保留(回滚阀)。
//
// ⚠ MC 门禁使用 profession.SyntheticCards(全 single / 无子女 / 无赡养)
//   ⇒ 本项对 G1–G12 千局门禁**零影响**,只能用真实卡测试判定
//   (方案 §2.3 关键约束 / §5.3)。
package virtual_city

import (
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"

	"LsmAgentGame/game/virtual_city/profession"
)

// b75DocsPoolRoot 真实文档卡池根目录(仓库根 + lag_docs/虚拟城市/玩家职业设计)。
// 用 runtime.Caller 定位本文件(位于 ServerGo/game/virtual_city)再上溯 3 层
// 到仓库根;池不存在(未检出 lag_docs 子模块)时 Skip 而非 panic。
func b75DocsPoolRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Skip("runtime.Caller unavailable; cannot locate docs pool")
	}
	root := filepath.Join(filepath.Dir(file), "..", "..", "..",
		"lag_docs", "虚拟城市", "玩家职业设计")
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("abs(%s): %v", root, err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Skipf("docs pool not present at %s (请执行 `git submodule update --init --recursive`): %v", abs, err)
	}
	return abs
}

// b75DetailSum 本月明细中指定 key 的支出绝对值之和(明细为负值金额)。
func b75DetailSum(p *Player, key string) int64 {
	var sum int64
	for _, it := range p.Monthly.Detail {
		if it.Key == key && it.AmountCNY < 0 {
			sum += -it.AmountCNY
		}
	}
	return sum
}

// b75MedianF 浮点切片中位数(空切片返回 0)。
func b75MedianF(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	c := append([]float64(nil), v...)
	sort.Float64s(c)
	n := len(c)
	if n%2 == 1 {
		return c[n/2]
	}
	return (c[n/2-1] + c[n/2]) / 2
}

// b75FamilyConst 卡面初始家庭成员的旧口径常数合计(配偶 2000 + 每孩 5000 +
// 每位老人 1000)—— B75-2 修正的「重复计入」金额。
func b75FamilyConst(c profession.Card) int64 {
	var sum int64
	if c.Marital == "married" {
		sum += livingSpouseCNY
	}
	sum += int64(c.ChildrenCount) * livingChildCNY
	sum += int64(c.EldersDependent) * livingElderCNY
	return sum
}

// ── T16 TestBalanceV2_FamilyExpenseNoDoubleCount(真实卡)──────────────
//
// 断言(每张真实「已婚有孩」卡跑一次月结):
//  1. v2 口径 detail["family"] == 0(初始配偶/子女/赡养已含在 monthly_expense,
//     **不得**再叠加 ⇒ 无重复计入);
//  2. v2 口径 detail["living"] == Card.Expense × infl × 档位乘数(精确算式);
//  3. 明细支出合计 == Monthly.Expense(明细与总额守恒);
//  4. 旧口径路径(BalanceV2Enabled=false)detail["family"] == 旧常数合计
//     ⇒ 回滚阀口径逐字段不变;
//  5. 聚合口径:标准档下「总生活支出 / 月收入」median 旧 > 1.0 → 新 < 0.9。
func TestBalanceV2_FamilyExpenseNoDoubleCount(t *testing.T) {
	root := b75DocsPoolRoot(t)
	l := profession.NewLoader(root)
	rng := rand.New(rand.NewSource(20261022))
	cards := l.Draw(600, rng)
	var kids []profession.Card
	for _, c := range cards {
		if c.Marital == "married" && c.ChildrenCount >= 1 {
			kids = append(kids, c)
		}
	}
	if len(kids) == 0 {
		t.Skip("真实卡池未抽到「已婚有孩」卡(池未检出或样本不足)")
	}
	t.Logf("T16:真实卡池抽样 %d 张,「已婚有孩」%d 张", len(cards), len(kids))

	// (1)–(4) 引擎口径逐卡核对(前 20 张跑月结,控制耗时)。
	const engineCases = 20
	n := engineCases
	if len(kids) < n {
		n = len(kids)
	}
	for i := 0; i < n; i++ {
		card := kids[i]
		// v2 口径世界。
		w := NewWorld(int64(9000+i), [MaxSeats]profession.Card{card})
		w.StartGame()
		p := w.Players[0]
		w.SettleMonth()
		if got := b75DetailSum(p, "family"); got != 0 {
			t.Errorf("卡 %s:detail[family] = %d,期望 0(初始配偶/子女/赡养已含在 monthly_expense,不得重复计入)",
				card.ID, got)
		}
		level := p.ConsumptionLevelSafe()
		wantLiving := int64(float64(card.Expense)*InflationFactorCB(w)*consumptionLevelMult[level] + 0.5)
		if got := b75DetailSum(p, "living"); got != wantLiving {
			t.Errorf("卡 %s:detail[living] = %d,期望 %d(=Expense %d × infl × 档位乘数 %v)",
				card.ID, got, wantLiving, card.Expense, consumptionLevelMult[level])
		}
		var expSum int64
		for _, it := range p.Monthly.Detail {
			if it.AmountCNY < 0 {
				expSum += -it.AmountCNY
			}
		}
		if expSum != p.Monthly.Expense {
			t.Errorf("卡 %s:明细支出合计 %d ≠ Monthly.Expense %d", card.ID, expSum, p.Monthly.Expense)
		}
		// 旧口径世界(回滚阀):仍叠加初始家庭常数。
		wOld := NewWorld(int64(9000+i), [MaxSeats]profession.Card{card})
		wOld.BalanceV2Enabled = false
		wOld.StartGame()
		pOld := wOld.Players[0]
		wOld.SettleMonth()
		wantLegacy := b75FamilyConst(card)
		if got := b75DetailSum(pOld, "family"); got != wantLegacy {
			t.Errorf("卡 %s:旧口径 detail[family] = %d,期望 %d(回滚阀口径不变)",
				card.ID, got, wantLegacy)
		}
	}

	// (5) 聚合口径:标准档(×1.0,M1 通胀因子 = 1)下「总生活支出 / 月收入」。
	var oldRatios, newRatios []float64
	for _, c := range kids {
		inc := float64(c.Salary)
		if inc <= 0 {
			continue
		}
		livingStd := float64(c.Expense)
		oldRatios = append(oldRatios, (livingStd+float64(b75FamilyConst(c)))/inc)
		newRatios = append(newRatios, livingStd/inc)
	}
	oldMed := b75MedianF(oldRatios)
	newMed := b75MedianF(newRatios)
	t.Logf("T16:已婚有孩卡 n=%d,median 总生活支出/收入:旧口径 %.3f → 新口径 %.3f",
		len(oldRatios), oldMed, newMed)
	if oldMed <= 1.0 {
		t.Errorf("旧口径 median 总生活支出/收入 = %.3f,期望 > 1.0(样本未复现重复计入)", oldMed)
	}
	if newMed >= oldMed {
		t.Errorf("新口径 median = %.3f,期望 < 旧口径 %.3f", newMed, oldMed)
	}
	if newMed >= 0.9 {
		t.Errorf("新口径 median 总生活支出/收入 = %.3f,期望 < 0.9", newMed)
	}
}

// ── T17 TestBalanceV2_FamilyInGameNewbornStillCharged ─────────────────
//
// 游戏内新增成员(新生儿 / 新配偶)**仍计费**;卡面初始成员不加。
func TestBalanceV2_FamilyInGameNewbornStillCharged(t *testing.T) {
	// Case A:游戏内新生儿 ⇒ detail["family"] == 5,000(1 孩)。
	t.Run("Newborn", func(t *testing.T) {
		w := b75World(7601, b75Card(20000, 6000, 50000, "residential"))
		p := w.Players[0]
		p.Family.Children = 1
		p.BirthMonths = []int{1} // 生育事件追加(events.go)
		w.SettleMonth()
		if got := b75DetailSum(p, "family"); got != livingChildCNY {
			t.Errorf("新生儿 detail[family] = %d,期望 %d(游戏内新增成员仍计费)", got, livingChildCNY)
		}
		level := p.ConsumptionLevelSafe()
		wantLiving := int64(float64(p.Card.Expense)*InflationFactorCB(w)*consumptionLevelMult[level] + 0.5)
		if got := b75DetailSum(p, "living"); got != wantLiving {
			t.Errorf("新生儿 detail[living] = %d,期望 %d", got, wantLiving)
		}
	})

	// Case B:游戏内新配偶(SpouseIncome>0)⇒ detail["family"] == 2,000。
	t.Run("NewSpouse", func(t *testing.T) {
		w := b75World(7602, b75Card(20000, 6000, 50000, "residential"))
		p := w.Players[0]
		p.Family.Marital = "married"
		p.Family.SpouseIncome = 6000 // 结婚事件(events.go:99)
		w.SettleMonth()
		if got := b75DetailSum(p, "family"); got != livingSpouseCNY {
			t.Errorf("新配偶 detail[family] = %d,期望 %d(游戏内新增成员仍计费)", got, livingSpouseCNY)
		}
	})

	// Case C:卡面初始成员(已婚 2 孩 1 老人)⇒ detail["family"] 不存在/为 0。
	t.Run("CardFaceMembersNotCharged", func(t *testing.T) {
		card := b75CardFull(20000, 6000, 50000, "residential", "married", 2)
		card.EldersDependent = 1
		w := b75World(7603, card)
		p := w.Players[0]
		w.SettleMonth()
		if got := b75DetailSum(p, "family"); got != 0 {
			t.Errorf("卡面初始家庭成员 detail[family] = %d,期望 0(已含在 monthly_expense)", got)
		}
		if b75HasDetailKey(p, "family") {
			t.Error("卡面初始家庭成员不应出现 detail[family] 项")
		}
	})
}

// ── 旧口径回归:BalanceV2Enabled=false 家庭加项逐字段不变 ──────────────
//
// 批次75 B75-2 的新口径**只在** BalanceV2Enabled=true 生效;
// false ⇒ 与批次74 口径一致(配偶 2000 + 每孩 5000 + 每位老人 1000 照旧叠加)。
func TestBalanceV2_FamilyOldPathUnchanged(t *testing.T) {
	card := b75CardFull(9000, 6000, 30000, "residential", "married", 2)
	card.EldersDependent = 1
	want := int64(livingSpouseCNY + 2*livingChildCNY + livingElderCNY)

	w := b75WorldLegacy(7604, card)
	p := w.Players[0]
	w.SettleMonth()
	if got := b75DetailSum(p, "family"); got != want {
		t.Errorf("旧口径 detail[family] = %d,期望 %d(批次74 口径逐字段不变)", got, want)
	}
	// 旧口径的恩格尔切分仍是「常数扣减」口径(splitEngelLegacy)。
	legacySplit := splitEngelLegacy(int64(6000)+want, p, 40)
	var legacySum int64
	for _, v := range legacySplit {
		legacySum += int64(v)
	}
	if legacySum != int64(6000)+want {
		t.Errorf("旧口径恩格尔金额守恒失败: sum=%d, want %d", legacySum, int64(6000)+want)
	}
	t.Logf("旧口径家庭加项 = %d(配偶 %d + 2 孩 %d + 老人 %d);恩格尔守恒 %d",
		want, livingSpouseCNY, 2*livingChildCNY, livingElderCNY, legacySum)
}
