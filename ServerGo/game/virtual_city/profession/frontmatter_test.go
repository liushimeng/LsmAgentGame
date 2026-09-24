// Package profession — frontmatter_test.go: frontmatter 形状容错单测
// (2026-09-16 §文档池解析修复 P0)。
//
// 用例直接内联**真实知识库 Schema v1.1 的字段形状**(从
// lag_docs/虚拟城市/玩家职业设计/…/N2005-彭民凯.md 节选),确保「map 形
// work_intensity / []map 形 goals_short / employment / name」永不再把整卡
// 解析打挂 —— 旧实现正是因为只测合成 v1.0 纯字符串 fixture 而全绿漏过事故。
package profession

import (
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// realSchemaV11Card 是真实卡 frontmatter 的形状样本(v1.1)。
const realSchemaV11Card = `---
id: N2005
name: 彭民凯
schema_version: '1.0'
card_type: person
gender: 男
age: 36
education: 硕士
health_grade: B
industry_l1: Z
industry_l2: Z03
industry_l3: Z0310
occupation: 电动航空适航工程师
employment: 全职
employer: 成都用人单位
work_intensity:
  weekly_hours: 48
  overtime: 中
  risk: 中
career_stage: 骨干
income_monthly: 19500
income_range:
- 15000
- 24000
income_structure: 年薪制
income_stability: 高
household_monthly: 37050
monthly_expense: 11000
savings_stock: 120000
debt_stock: 0
marital: 已婚
children_count: 1
elders_dependent: 0
housing_tenure: 租赁
housing_city: 成都
goals_short:
- horizon: 开局
  text: 型号合格审定TC受理
- horizon: 开局
  text: 局方审查组进场
- horizon: 开局
  text: 女儿幼儿园
personality:
  - "尽责坚韧"
  - "情绪稳定"
  - "顺从协作"
behavior_traits:
  - "记账习惯"
  - "长线规划"
  - "风险管理"
risk_preference: balanced
opening_hook: "成都，他面对合规责任焦虑，目标「型号合格审定TC受理」，尽责坚韧的他需在本回合做出取舍"
---
正文略`

// writeCard 把 frontmatter 文本落成临时池里的一张卡。
func writeCard(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// TestParse_RealSchemaV11Shapes 真实 v1.1 形状必须全部解析成功且字段映射正确。
func TestParse_RealSchemaV11Shapes(t *testing.T) {
	dir := t.TempDir()
	writeCard(t, dir, "N2005-彭民凯.md", realSchemaV11Card)

	l := NewLoader(dir)
	cards := l.Draw(1, rand.New(rand.NewSource(1)))
	if len(cards) != 1 {
		t.Fatalf("draw = %d, want 1", len(cards))
	}
	c := cards[0]
	if c.Source != "docs" {
		t.Fatalf("source = %q, want docs(v1.1 形状解析失败会回退合成卡)", c.Source)
	}
	if c.ID != "N2005" {
		t.Errorf("id = %q, want N2005", c.ID)
	}
	if c.Name != "彭民凯" {
		t.Errorf("name = %q, want 彭民凯(字段名是 name 而非 legacy_name)", c.Name)
	}
	if c.Title != "电动航空适航工程师" {
		t.Errorf("title = %q", c.Title)
	}
	if c.Salary != 19500 {
		t.Errorf("salary = %d, want 19500", c.Salary)
	}
	if c.Expense != 11000 {
		t.Errorf("expense = %d, want 11000", c.Expense)
	}
	if c.Savings != 120000 {
		t.Errorf("savings = %d, want 120000", c.Savings)
	}
	if c.StartAge != 36 {
		t.Errorf("start_age = %d, want 36", c.StartAge)
	}
	// work_intensity 是 map{overtime:中} → 档位「中」→ Energy 6。
	if c.Energy != 6 {
		t.Errorf("energy = %d, want 6(work_intensity.overtime=中)", c.Energy)
	}
	if c.HealthGrade != "B" {
		t.Errorf("health_grade = %q, want B", c.HealthGrade)
	}
	if c.Marital != "married" {
		t.Errorf("marital = %q, want married", c.Marital)
	}
	if c.ChildrenCount != 1 {
		t.Errorf("children = %d, want 1", c.ChildrenCount)
	}
	// goals_short 是 []map{horizon,text} → 渲染「horizon：text」,取前 3。
	if len(c.Goals) != 3 {
		t.Fatalf("goals = %v, want 3 条", c.Goals)
	}
	if c.Goals[0] != "开局：型号合格审定TC受理" {
		t.Errorf("goals[0] = %q", c.Goals[0])
	}
	// 词库过滤:「情绪稳定」不在 13 词库 → 只保留尽责坚韧/顺从协作。
	if len(c.Personality) == 0 {
		t.Errorf("personality 不应为空(词库过滤后仍应有命中)")
	}
	for _, p := range c.Personality {
		if _, ok := personalityVocab[p]; !ok {
			t.Errorf("personality %q 不在词库", p)
		}
	}
	if len(c.BehaviorTraits) == 0 {
		t.Errorf("behavior_traits 不应为空")
	}
	// 城市「成都」+ industry_l1「Z」→ 行业优先:Z → riverside。
	if c.HomeDistrict != "riverside" {
		t.Errorf("home_district = %q, want riverside(industry_l1=Z)", c.HomeDistrict)
	}
	if c.CreditScore != 650 {
		t.Errorf("credit_score = %d, want 650(8000–20000 档)", c.CreditScore)
	}
	if err := c.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

// TestParse_LegacyV10ShapesStillWork v1.0 纯字符串形状(旧 fixture)零回归:
// work_intensity 标量 / goals_short []string / employment_type / legacy_name。
func TestParse_LegacyV10ShapesStillWork(t *testing.T) {
	dir := t.TempDir()
	writeCard(t, dir, "T001-甲.md", `---
id: T001
legacy_name: 测试甲
occupation: 测试员A
industry_l1: ""
income_monthly: 8000
monthly_expense: 5000
savings_stock: 30000
age: 28
work_intensity: 高
health_grade: A
personality: ["务实主义"]
behavior_traits: ["精打细算"]
risk_preference: balanced
marital: 单身
opening_hook: 我是一名测试员,正在测试文档池加载器的端到端路径。
goals_short: ["5 年内存款翻一番", "建立应急金"]
housing_city: 一线城市
employment_type: 自由职业
---
正文略`)
	l := NewLoader(dir)
	cards := l.Draw(1, rand.New(rand.NewSource(2)))
	if len(cards) != 1 || cards[0].Source != "docs" {
		t.Fatalf("v1.0 形状解析失败: %+v", cards)
	}
	c := cards[0]
	if c.Name != "测试甲" {
		t.Errorf("name = %q, want 测试甲(legacy_name 兼容)", c.Name)
	}
	if c.Energy != 4 {
		t.Errorf("energy = %d, want 4(work_intensity=高)", c.Energy)
	}
	if len(c.Goals) != 2 || c.Goals[0] != "5 年内存款翻一番" {
		t.Errorf("goals = %v, want 2 条纯字符串", c.Goals)
	}
	if c.HomeDistrict != "finance" {
		t.Errorf("home_district = %q, want finance(housing_city 含「一线」)", c.HomeDistrict)
	}
	// employment_type=自由职业 → 信用分 650 − 50 = 600。
	if c.CreditScore != 600 {
		t.Errorf("credit_score = %d, want 600(employment_type 兼容 + 自由职业 −50)", c.CreditScore)
	}
	if err := c.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

// TestParse_FlowStyleAndOddShapes 流式写法 {a: 1, b: 2} 与各种脏形状:
// 全部不得让整卡解析失败(容错优先,缺失走兜底)。
func TestParse_FlowStyleAndOddShapes(t *testing.T) {
	dir := t.TempDir()
	writeCard(t, dir, "T002-乙.md", `---
id: T002
name: 测试乙
occupation: 社区团长
work_intensity: {weekly_hours: 55, overtime: 高, risk: 中}
income_monthly: "12,000"
monthly_expense: 6000
savings_stock: 1.5万
age: "41"
health_grade: null
personality: 外向社交
behavior_traits: [{text: 月光族}, {text: 冲动消费}]
marital: null
goals_short:
- {horizon: 40 岁前, text: 把家庭被动收入提升到月支出的一半}
housing_city: 未知
employment: 个体经营
opening_hook: ""
---
正文略`)
	l := NewLoader(dir)
	cards := l.Draw(1, rand.New(rand.NewSource(3)))
	if len(cards) != 1 || cards[0].Source != "docs" {
		t.Fatalf("脏形状解析失败(回退合成卡): %+v", cards)
	}
	c := cards[0]
	if c.Salary != 12000 {
		t.Errorf("salary = %d, want 12000(带千分位引号字符串)", c.Salary)
	}
	if c.Savings != 15000 {
		t.Errorf("savings = %d, want 15000(「1.5万」中文数量级)", c.Savings)
	}
	if c.StartAge != 41 {
		t.Errorf("start_age = %d, want 41(字符串数字)", c.StartAge)
	}
	if c.Energy != 4 {
		t.Errorf("energy = %d, want 4(overtime=高 → 档位高)", c.Energy)
	}
	if c.HealthGrade != "B" {
		t.Errorf("health_grade = %q, want B(null 兜底)", c.HealthGrade)
	}
	if c.Marital != "single" {
		t.Errorf("marital = %q, want single(null 兜底)", c.Marital)
	}
	if len(c.Personality) != 1 || c.Personality[0] != "外向社交" {
		t.Errorf("personality = %v, want [外向社交](标量形状)", c.Personality)
	}
	// 「月光族/冲动消费」都不在词库 → 兜底保留原始前 2 词,人设不得为空。
	if len(c.BehaviorTraits) == 0 {
		t.Errorf("behavior_traits 空:词库全 miss 时应兜底保留原词")
	}
	if len(c.Goals) != 1 || c.Goals[0] != "40 岁前：把家庭被动收入提升到月支出的一半" {
		t.Errorf("goals = %v", c.Goals)
	}
	// opening_hook 空 → 兜底生成(≥20 rune);housing_city 未知 + 无行业 →
	// 按卡 id 哈希散列到 5 个非核心城区之一。
	if len([]rune(c.OpeningHook)) < 20 {
		t.Errorf("opening_hook = %q, want ≥20 rune(兜底生成)", c.OpeningHook)
	}
	if !validDistrict(c.HomeDistrict) {
		t.Errorf("home_district = %q invalid", c.HomeDistrict)
	}
	// employment=个体经营 → 信用分下调。
	if c.CreditScore != 600 {
		t.Errorf("credit_score = %d, want 600(个体经营 −50)", c.CreditScore)
	}
	if err := c.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

// TestParse_MissingIncomeStillSkippedOnlyThatCard 一张「无收入档案」卡
// (income_monthly 与 income_range 双 null,真实池占比 ≈0.33%)只跳过自己,
// 同池其它卡照常发出,且 Draw 会补抽到 n 张(不掉合成卡)。
func TestParse_MissingIncomeStillSkippedOnlyThatCard(t *testing.T) {
	dir := t.TempDir()
	writeCard(t, dir, "BAD-缺收入.md", `---
id: BAD1
name: 缺收入
occupation: 古籍修复师
income_monthly: null
income_range: null
opening_hook: 这是一张收入档案缺失的卡,用来验证单卡失败不影响整池。
---
正文略`)
	for i := 0; i < 3; i++ {
		writeCard(t, dir, string(rune('A'+i))+"-好卡.md", `---
id: GOOD`+string(rune('0'+i))+`
name: 好卡`+string(rune('0'+i))+`
occupation: 测试职业`+string(rune('0'+i))+`
income_monthly: 9000
monthly_expense: 5000
savings_stock: 20000
age: 30
work_intensity: {weekly_hours: 44, overtime: 中, risk: 低}
opening_hook: 这是一张形状完整的测试卡,用于验证补抽机制。
goals_short:
- {horizon: 35 岁前, text: 攒下第一笔 20 万}
housing_city: 杭州
employment: 全职
---
正文略`)
	}
	l := NewLoader(dir)
	cards := l.Draw(3, rand.New(rand.NewSource(5)))
	if len(cards) != 3 {
		t.Fatalf("draw = %d, want 3", len(cards))
	}
	for _, c := range cards {
		if c.Source != "docs" {
			t.Errorf("card %s source = %q, want docs(坏卡应被补抽跳过而非回退合成卡)", c.ID, c.Source)
		}
		if c.ID == "BAD1" {
			t.Errorf("无收入卡 BAD1 不应入池")
		}
	}
	if _, _, _, fail := l.PoolInfo(); fail == 0 {
		t.Errorf("parseFail 计数应为 ≥1(坏卡可观测)")
	}
}

// TestParse_RecoverRealPoolGeneratorYAMLFragments 覆盖真实池两类确定性 YAML 生成器瑕疵：
// `key:` 后零缩进 `[]`，以及 src_line 未闭合单引号且含竖线/冒号。修复只发生在
// 解析输入副本上，核心字段仍按原值解析，磁盘文档池不被批量改写。
func TestParse_RecoverRealPoolGeneratorYAMLFragments(t *testing.T) {
	fm := []byte("id: NTEST\r\n" +
		"name: 测试丙\r\n" +
		"occupation: 蔬菜大棚技术员\r\n" +
		"industry_l1: A\r\n" +
		"income_monthly: 12000\r\n" +
		"monthly_expense: 6000\r\n" +
		"savings_stock: 30000\r\n" +
		"age: 36\r\n" +
		"work_intensity: {weekly_hours: 48, overtime: 中, risk: 中}\r\n" +
		"debts:\r\n" +
		"[]\r\n" +
		"_raw:\r\n" +
		"_enrich_v44:\r\n" +
		"  src_line: '| NTEST | 测试丙 · 36/健康B | 长沙租房2400 | 未婚 | O'Brien:大棚技术员,12000(8000-16000)\r\n" +
		"  legacy_name: 测试丙\r\n")

	clean := sanitizeFrontmatterYAML(fm)
	if !strings.Contains(string(clean), "debts: []") {
		t.Fatalf("空集合未合并为合法 flow sequence:\n%s", clean)
	}
	if !strings.Contains(string(clean), "src_line: '| NTEST | 测试丙 · 36/健康B | 长沙租房2400 | 未婚 | O''Brien:大棚技术员,12000(8000-16000)'") {
		t.Fatalf("src_line 未重写为合法 YAML 单引号标量:\n%s", clean)
	}

	raw, err := parseDocCard(fm)
	if err != nil {
		t.Fatalf("parseDocCard: %v", err)
	}
	if raw.ID != "NTEST" || raw.Name.Text != "测试丙" || raw.Occupation.Text != "蔬菜大棚技术员" {
		t.Fatalf("核心身份数据被 YAML 修复影响: %+v", raw)
	}
	if !raw.IncomeMonthly.Valid || raw.IncomeMonthly.Value != 12000 {
		t.Fatalf("income_monthly = %+v, want 12000", raw.IncomeMonthly)
	}
	if raw.WorkIntensity.Level != "中" {
		t.Fatalf("work_intensity.level = %q, want 中", raw.WorkIntensity.Level)
	}
}

// TestParse_DuplicateEnrichKeysAreMerged 覆盖极少数真实卡的整段 enrich 重复追加：
// duplicate key 不能让整卡失败；同名字段按后写覆盖前写合并，未知元数据重复同样被净化。
func TestParse_DuplicateEnrichKeysAreMerged(t *testing.T) {
	raw, err := parseDocCard([]byte(`id: DUP
name: 重复字段卡
occupation: 区块链应用工程师
income_monthly: 18000
personality:
  - 保守
personality:
  - 理性
opening_hook: 第一版开场白
opening_hook: 第二版开场白，长度足以通过基础兜底校验。
_enrich_v44:
  ts: "2026-09-18T00:00:00Z"
_enrich_v44:
  ts: "2026-09-19T00:00:00Z"
`))
	if err != nil {
		t.Fatalf("parseDocCard: %v", err)
	}
	if raw.ID != "DUP" || raw.Occupation.Text != "区块链应用工程师" || !raw.IncomeMonthly.Valid {
		t.Fatalf("核心字段解析异常: %+v", raw)
	}
	if len(raw.Personality.Items) != 1 || raw.Personality.Items[0] != "理性" {
		t.Fatalf("personality = %v, want 后写覆盖 [理性]", raw.Personality.Items)
	}
	if raw.OpeningHook.Text != "第二版开场白，长度足以通过基础兜底校验。" {
		t.Fatalf("opening_hook = %q, want 后写覆盖", raw.OpeningHook.Text)
	}
}

// TestParse_ValidMultilineSrcLineIsPreserved 真实池中合法 src_line 可以跨多个
// 更深层缩进行，闭合引号在最后一行；净化器不得在首行提前补引号。
func TestParse_ValidMultilineSrcLineIsPreserved(t *testing.T) {
	fm := []byte(`id: MLINE
name: 多行元数据卡
occupation: 市场推广与营销
income_monthly: 15000
_raw:
_enrich_v44:
  src_line: '| NMLINE | 多行元数据卡 · 长沙租房2400
    | 已婚;父母县城退休 | 市场推广与营销,15000(10000-20000)
    |'
  legacy_name: 多行元数据卡
`)
	raw, err := parseDocCard(fm)
	if err != nil {
		t.Fatalf("parseDocCard: %v", err)
	}
	if raw.ID != "MLINE" || raw.Occupation.Text != "市场推广与营销" || !raw.IncomeMonthly.Valid {
		t.Fatalf("核心字段解析异常: %+v", raw)
	}
}

// TestResolveDistrict_Priority 城区解析优先级:行业 > 城市关键词 > 城市分层 > 哈希散列。
// 批次20 §2.4:行业值为候选集(多候选按卡 id 哈希稳定散列),断言相应改为
// 「落在契约候选集内」;单候选行业仍为逐字相等。
func TestResolveDistrict_Priority(t *testing.T) {
	cases := []struct {
		industry, city, id string
		wants              []string // 允许结果集(单元素 = 逐字相等)
	}{
		{"Q", "县城", "X1", []string{"finance", "fin_sub_center"}},  // 行业优先(§2.4 row2:finance + 金融副中心)
		{"", "一线城市", "X2", []string{"finance"}},                 // 关键词表
		{"", "高新区", "X3", []string{"tech"}},                      // 关键词表
		{"", "北京", "X4", []string{"finance"}},                     // 城市分层:一线
		{"", "杭州", "X5", []string{"tech"}},                        // 城市分层:新一线
		{"", "厦门", "X6", []string{"commerce"}},                    // 城市分层:二线
		{"P", "未知", "X7", []string{"tech", "software_park"}},      // 行业:信息传输/软件(§2.4 row1)
		{"A", "未知", "X8", []string{"agri_park"}},                  // 行业:农林牧渔 → 现代农业园(§2.4 改派)
		{"V", "未知", "X9", []string{"sports_new_city", "cultural_creative", "old_city_culture"}}, // 文体传媒(§2.4 row10/11)
	}
	for _, c := range cases {
		got := resolveDistrict(c.industry, c.city, c.id)
		ok := false
		for _, w := range c.wants {
			if got == w {
				ok = true
				break
			}
		}
		if !ok {
			t.Errorf("resolveDistrict(%q,%q,%q) = %q, want ∈ %v", c.industry, c.city, c.id, got, c.wants)
		}
	}
	// 同 id 同区(确定性,不吃 rng);不同 id 落在 spreadDistricts(批次20 扩为 12 区)之一。
	hashSet := map[string]struct{}{}
	for _, d := range spreadDistricts {
		hashSet[d] = struct{}{}
	}
	if len(spreadDistricts) != 12 {
		t.Errorf("spreadDistricts = %d 区, want 12(批次20 §2.4)", len(spreadDistricts))
	}
	got := resolveDistrict("", "未知", "N2005")
	if _, ok := hashSet[got]; !ok {
		t.Errorf("N2005 应落在 spreadDistricts, got %s", got)
	}
	if resolveDistrict("", "未知", "N2005") != resolveDistrict("", "未知", "N2005") {
		t.Errorf("resolveDistrict 必须确定性")
	}
}

// TestDeriveIntensityLevel 档位推导公式:overtime 优先 → weekly_hours 分档 → 中。
func TestDeriveIntensityLevel(t *testing.T) {
	cases := []struct {
		overtime string
		hours    int
		want     string
	}{
		{"高", 40, "高"},
		{"低", 60, "低"},
		{"中", 55, "中"},
		{"", 50, "高"},
		{"", 48, "中"},
		{"", 36, "低"},
		{"", 0, "中"},
	}
	for _, c := range cases {
		if got := deriveIntensityLevel(c.overtime, c.hours); got != c.want {
			t.Errorf("deriveIntensityLevel(%q,%d) = %q, want %q", c.overtime, c.hours, got, c.want)
		}
	}
}

// TestSalaryFallback_IncomeRange income_monthly 缺失但 income_range 可用 →
// 取区间均值并归整到百元(抽样 ≈0.3% 的卡靠这条兜底留在池里)。
func TestSalaryFallback_IncomeRange(t *testing.T) {
	dir := t.TempDir()
	writeCard(t, dir, "R1-区间.md", `---
id: R1
name: 区间卡
occupation: 自由撰稿人
income_monthly: null
income_range: [15000, 24000]
monthly_expense: 8000
opening_hook: 收入没有固定数字,只有一段区间,用来验证均值兜底。
housing_city: 武汉
employment: 自由职业
---
正文略`)
	l := NewLoader(dir)
	cards := l.Draw(1, rand.New(rand.NewSource(9)))
	if len(cards) != 1 || cards[0].Source != "docs" {
		t.Fatalf("区间兜底失败: %+v", cards)
	}
	// (15000+24000)/2 = 19500 → 归整百元 = 19500。
	if cards[0].Salary != 19500 {
		t.Errorf("salary = %d, want 19500(区间均值)", cards[0].Salary)
	}
}
