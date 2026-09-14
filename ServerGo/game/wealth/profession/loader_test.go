package profession

import (
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCurated_Validation 10 张精选卡必须通过 Validate(§1)。
func TestCurated_Validation(t *testing.T) {
	if err := ValidateCurated(); err != nil {
		t.Errorf("curated validation failed: %v", err)
	}
}

// TestLoader_FallbackToCurated 根目录不存在 → loader 自动回退 curated(Draw 仍返回 10 张)。
func TestLoader_FallbackToCurated(t *testing.T) {
	l := NewLoader("/nonexistent/path/that/does/not/exist")
	l.ForceIndex()
	cards := l.Draw(8, rand.New(rand.NewSource(1)))
	if len(cards) != 8 {
		t.Errorf("fallback draw: got %d, want 8", len(cards))
	}
	for _, c := range cards {
		if c.Source != "curated" {
			t.Errorf("expected curated source, got %s", c.Source)
		}
		if err := c.Validate(); err != nil {
			t.Errorf("curated card %s invalid: %v", c.ID, err)
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
		if !strings.HasPrefix(c.ID, "GOOD") && !strings.HasPrefix(c.ID, "P") {
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