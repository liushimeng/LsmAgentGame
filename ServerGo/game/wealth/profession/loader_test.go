package profession

import (
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestSyntheticCards_确定性 合成兜底卡(契约 03 §2.2):同 rng 序 → 同卡集;
// 恒返回 n 张且全部通过 Validate;Source=="synthetic";StartAge ∈[20,55]。
func TestSyntheticCards_确定性(t *testing.T) {
	mk := func() []Card {
		rng := rand.New(rand.NewSource(20260922))
		return SyntheticCards(12, rng)
	}
	a, b := mk(), mk()
	if len(a) != 12 || len(b) != 12 {
		t.Fatalf("SyntheticCards(12) = %d / %d, want 12/12", len(a), len(b))
	}
	for i := range a {
		if !reflect.DeepEqual(a[i], b[i]) {
			t.Fatalf("card %d not deterministic:\n%+v\nvs\n%+v", i, a[i], b[i])
		}
		if a[i].Source != "synthetic" {
			t.Errorf("card %s source = %q, want synthetic", a[i].ID, a[i].Source)
		}
		if a[i].StartAge < 20 || a[i].StartAge > 55 {
			t.Errorf("card %s start_age = %d, want in [20,55]", a[i].ID, a[i].StartAge)
		}
		if err := a[i].Validate(); err != nil {
			t.Errorf("synthetic card %s invalid: %v", a[i].ID, err)
		}
	}
	// 职业模板多样性:12 张覆盖 12 个不同 Title(模板表逐一轮转)。
	seen := map[string]struct{}{}
	for _, c := range a {
		seen[c.Title] = struct{}{}
	}
	if len(seen) != 12 {
		t.Fatalf("template spread = %d, want 12", len(seen))
	}
	// n<=0 → nil;rng nil → 确定性默认源(不 panic)。
	if got := SyntheticCards(0, nil); got != nil {
		t.Errorf("SyntheticCards(0) = %v, want nil", got)
	}
	if got := SyntheticCards(3, nil); len(got) != 3 {
		t.Errorf("SyntheticCards(3, nil rng) = %d, want 3", len(got))
	}
}

// TestLoader_回退合成卡 根目录不存在 → loader 自动回退合成卡
// (Draw 恒返回 n 张、Source=synthetic、全部 Validate 通过;契约 03 §2.2)。
func TestLoader_回退合成卡(t *testing.T) {
	l := NewLoader("/nonexistent/path/that/does/not/exist")
	l.ForceIndex()
	cards := l.Draw(8, rand.New(rand.NewSource(1)))
	if len(cards) != 8 {
		t.Errorf("fallback draw: got %d, want 8", len(cards))
	}
	for _, c := range cards {
		if c.Source != "synthetic" {
			t.Errorf("expected synthetic source, got %s", c.Source)
		}
		if err := c.Validate(); err != nil {
			t.Errorf("synthetic card %s invalid: %v", c.ID, err)
		}
	}
}

// TestLoader_ParseFixture 真实 frontmatter 解析。
func TestLoader_ParseFixture(t *testing.T) {
	dir := t.TempDir()
	// 写 3 张测试卡(.md 带 frontmatter)。
	cards := []string{
		`---
id: T001
occupation: 测试员A
income_monthly: 8000
monthly_expense: 5000
savings_stock: 30000
age: 28
work_intensity: 中
health_grade: A
personality: ["务实主义"]
behavior_traits: ["精打细算"]
risk_preference: balanced
marital: 单身
opening_hook: 我是一名测试员,正在测试文档池加载器的端到端路径。
goals_short: ["5 年内存款翻一番"]
housing_city: 一线城市
employment_type: 全职
---
正文略`,
		`---
id: T002
occupation: 测试员B
income_monthly: 15000
income_stability: 收入波动较大
monthly_expense: 10000
savings_stock: 100000
age: 35
work_intensity: 高
health_grade: B
personality: ["务实主义","开放求新"]
risk_preference: aggressive
marital: 已婚
children_count: 1
opening_hook: 我是一名独立承包测试员,收入波动但上限可观。
goals_short: ["建立 6 个月生活费的安全垫"]
housing_city: 高新区
employment_type: 自由职业
---
正文略`,
		`---
id: T003
occupation: 测试员C
income_monthly: 6000
monthly_expense: 4000
savings_stock: 15000
age: 22
work_intensity: 低
health_grade: C
risk_preference: conservative
marital: 单身
opening_hook: 我刚毕业,正在寻找合适的人生方向与稳妥的财富之路。
goals_short: ["先攒出 6 个月生活费"]
housing_city: 县城
---
正文略`,
	}
	for i, c := range cards {
		path := filepath.Join(dir, "card_test"+string(rune('A'+i))+".md")
		if err := os.WriteFile(path, []byte(c), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	// _框架 目录应被跳过。
	_ = os.MkdirAll(filepath.Join(dir, "_framework"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "_framework", "README.md"), []byte("---"), 0o644)

	l := NewLoader(dir)
	l.ForceIndex()
	avail, total, indexed, _ := l.PoolInfo()
	if !avail {
		t.Fatalf("pool not available")
	}
	if total < 3 {
		t.Errorf("total: got %d, want ≥3", total)
	}
	if indexed < 0 {
		t.Errorf("indexed before draw: got %d", indexed)
	}

	drawn := l.Draw(3, rand.New(rand.NewSource(1)))
	if len(drawn) != 3 {
		t.Fatalf("draw 3: got %d", len(drawn))
	}

	// 验证 frontmatter 解析正确(任取一张,id=T00x)。
	byID := map[string]Card{}
	for _, c := range drawn {
		byID[c.ID] = c
	}
	c1, ok := byID["T001"]
	if !ok {
		t.Fatalf("T001 not in draw")
	}
	if c1.Title != "测试员A" {
		t.Errorf("T001 title: got %q, want 测试员A", c1.Title)
	}
	if c1.Salary != 8000 {
		t.Errorf("T001 salary: got %d, want 8000", c1.Salary)
	}
	if c1.SalaryVolatile {
		t.Errorf("T001 should not be volatile")
	}
	if c1.StartAge != 28 {
		t.Errorf("T001 start_age: got %d, want 28", c1.StartAge)
	}
	if c1.Energy != 6 { // 中 → 6
		t.Errorf("T001 energy: got %d, want 6", c1.Energy)
	}
	if c1.HealthGrade != "A" {
		t.Errorf("T001 health: got %s, want A", c1.HealthGrade)
	}
	if c1.HomeDistrict != "finance" { // 一线城市 → finance
		t.Errorf("T001 district: got %s, want finance", c1.HomeDistrict)
	}
	if c1.CreditScore != 650 { // 8000-20000 区间 → 650
		t.Errorf("T001 credit: got %d, want 650", c1.CreditScore)
	}

	// 验证 LRU 缓存:再读一次应命中(无解析报错)。
	drawn2 := l.Draw(3, rand.New(rand.NewSource(1)))
	if len(drawn2) != 3 {
		t.Errorf("cached draw: got %d", len(drawn2))
	}

	// 索引计数 ≥3。
	_, _, indexed2, _ := l.PoolInfo()
	if indexed2 < 3 {
		t.Errorf("indexed after draw: got %d, want ≥3", indexed2)
	}
}

// TestLoader_InvalidFrontmatterIgnored frontmatter 缺关键字段 → 跳过(不 panic)。
func TestLoader_InvalidFrontmatterIgnored(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "bad.md"), []byte(`---
id: BAD1
---
`), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "good.md"), []byte(`---
id: GOOD1
occupation: 测试
income_monthly: 8000
monthly_expense: 5000
savings_stock: 10000
age: 25
opening_hook: 这是一段测试开场白,用来满足长度要求的固定文本内容描述。
---
`), 0o644)
	l := NewLoader(dir)
	drawn := l.Draw(2, rand.New(rand.NewSource(1)))
	if len(drawn) < 1 {
		t.Errorf("draw should yield ≥1 card (good one), got %d", len(drawn))
	}
	for _, c := range drawn {
		if !strings.HasPrefix(c.ID, "GOOD") && !strings.HasPrefix(c.ID, "S") {
			t.Errorf("unexpected card %s in fallback draw", c.ID)
		}
	}
}

// TestPoolInfo_NotBuiltEmptyRoot PoolInfo 在未 ForceIndex 时给出 total=-1。
func TestPoolInfo_NotBuiltEmptyRoot(t *testing.T) {
	l := NewLoader("")
	avail, total, _, _ := l.PoolInfo()
	if avail {
		t.Errorf("empty root: avail should be false")
	}
	if total != -1 {
		t.Errorf("empty root: total should be -1, got %d", total)
	}
}
