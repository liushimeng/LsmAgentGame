// Package city — crowd_test.go: 批次34 人流分布/人物原型/crowd manifest 单测
// (2026-09-29;契约 34-人物模型与人流分布与视觉操控/01-方案设计 §5.1/§6.1/§6.4)。
//
//	OutdoorCount 公式验证点(§6.1 表)
//	ArchetypeFor 八分支 + 优先级(§5.1 表)
//	WealthTierFor 四分位边界(§5.1)
//	CrowdSnapshot 确定性/户外户内计数/换班候选/json tag(§6.4)
package city

import (
	"encoding/json"
	"math/rand"
	"reflect"
	"strings"
	"testing"

	"LsmAgentGame/game/virtual_city/profession"
)

// TestOutdoorCount §6.1 公式验证点(与设计文档表格逐行对齐)。
func TestOutdoorCount(t *testing.T) {
	cases := []struct{ n, cap, want int }{
		{10, 160, 10},     // 人少全部上街
		{50, 160, 50},     // 人少全部上街
		{100, 160, 100},   // 拐点 N0
		{400, 160, 160},   // 触顶画质上限
		{10000, 160, 160}, // 触顶
		{0, 160, 0},       // 空城
		{5, 160, 5},       // 极小房
		{1000, 160, 160},  // §6.1 表 1000 → 160
		{100000, 160, 160},
		{500, 90, 90}, // 低画质档 CAP
		{80, 90, 80},  // 低画质档下仍「人少全上街」
		{500, 0, 0},   // cap=0 防御
		{-3, 160, 0},  // 负数防御
		// ── 权威口径 cap=crowdCapHigh/crowdCapLow(批次34 修订:110/60,不抬上限)──
		{10, 110, 10},     // 用户 R6:N=10 全上街
		{50, 110, 50},     // 用户 R6:N=50 全上街
		{100, 110, 100},   // 拐点 N0,仍全上街
		{400, 110, 110},   // 触顶画质上限
		{10000, 110, 110}, // 触顶
		{100, 60, 60},     // 低画质档触顶(N=100 > 60)
		{30, 60, 30},      // 低画质档下人少仍全上街
	}
	for _, c := range cases {
		if got := OutdoorCount(c.n, c.cap); got != c.want {
			t.Errorf("OutdoorCount(%d,%d) = %d, want %d", c.n, c.cap, got, c.want)
		}
	}
}

// TestOutdoorCount_Monotonic 次线性语义:N 增大 V 不减;N≤100 恒 V(N)=N。
func TestOutdoorCount_Monotonic(t *testing.T) {
	prev := 0
	for n := 0; n <= 2000; n++ {
		v := OutdoorCount(n, 160)
		if v < prev {
			t.Fatalf("V(%d)=%d < V(%d)=%d(必须单调不减)", n, v, n-1, prev)
		}
		if n <= 100 && v != n {
			t.Fatalf("V(%d)=%d, want %d(N≤N0 必须全部上街)", n, v, n)
		}
		prev = v
	}
}

// TestArchetypeFor §5.1 判据表八分支(每分支至少一条断言 + 优先级)。
func TestArchetypeFor(t *testing.T) {
	cases := []struct {
		name          string
		age           int
		domain        int
		wealth        int
		gender        string
		employment    string
		childrenCount int
		want          string
	}{
		{"elder", 55, 0, 0, "u", "", 0, "char_elder"},
		{"elder_70", 70, 12, 3, "f", "平台就业", 3, "char_elder"}, // 最高优先
		{"student", 24, 0, 0, "m", "", 0, "char_student"},
		{"student_16", 16, 15, 3, "m", "", 0, "char_student"}, // 高于 formal/business
		{"formal", 40, 0, 3, "m", "", 0, "char_formal"},
		{"formal_over_business", 40, 15, 3, "f", "", 0, "char_formal"}, // formal 先于 business
		{"business_P", 35, 15, 1, "m", "", 0, "char_business"},
		{"business_Q", 35, 16, 2, "f", "", 0, "char_business"},
		{"business_R", 35, 17, 1, "m", "", 0, "char_business"},
		{"business_S", 35, 18, 1, "f", "", 0, "char_business"},
		{"business_needs_wealth", 35, 15, 0, "m", "", 0, "char_casual"}, // 域中但财富 0 → 落空
		{"worker_B", 35, 1, 0, "m", "", 0, "char_worker"},
		{"worker_G", 35, 6, 0, "f", "", 0, "char_worker"},
		{"worker_L", 35, 11, 0, "m", "", 0, "char_worker"},
		{"worker_over_service", 35, 7, 0, "m", "平台就业", 0, "char_worker"}, // worker 先于 service
		{"service_M", 35, 12, 0, "f", "", 0, "char_service"},
		{"service_Y", 35, 24, 0, "m", "", 0, "char_service"},
		{"service_platform", 35, 0, 0, "m", "平台就业", 0, "char_service"},
		{"service_flexible", 35, 19, 0, "f", "灵活就业", 0, "char_service"},
		{"parent", 40, 0, 0, "f", "", 2, "char_parent"},
		{"parent_age30", 30, 19, 0, "m", "", 1, "char_parent"},
		{"parent_age54", 54, 19, 0, "f", "", 1, "char_parent"},
		{"parent_too_young", 29, 0, 0, "f", "", 3, "char_casual"}, // 29 不在 [30,54]
		{"parent_zero_children", 40, 0, 0, "f", "", 0, "char_casual"},
		{"casual", 35, 0, 0, "u", "", 0, "char_casual"},
		{"casual_edu_T", 25, 19, 0, "m", "", 0, "char_casual"}, // T 教育不入任何组
		{"casual_neg_domain", 35, -1, 0, "u", "", 0, "char_casual"},
	}
	for _, c := range cases {
		got := ArchetypeFor(c.age, c.domain, c.wealth, c.gender, c.employment, c.childrenCount)
		if got != c.want {
			t.Errorf("%s: ArchetypeFor(age=%d,domain=%d,wealth=%d,emp=%q,kids=%d) = %q, want %q",
				c.name, c.age, c.domain, c.wealth, c.employment, c.childrenCount, got, c.want)
		}
	}
}

// TestWealthTierFor 四分位边界(§5.1:income/savings 各切 4 档取较大)。
func TestWealthTierFor(t *testing.T) {
	cases := []struct {
		income, savings float64
		want            int
	}{
		{0, 0, 0},
		{-100, -100, 0}, // 负值按 0 档
		{8999.99, 0, 0},
		{9000, 0, 1}, // income p25 切点
		{12999, 0, 1},
		{13000, 0, 2}, // income p50 切点
		{18999, 0, 2},
		{19000, 0, 3}, // income p75 切点
		{0, 32999, 0},
		{0, 33000, 1}, // savings p25
		{0, 89999, 1},
		{0, 90000, 2}, // savings p50
		{0, 199999, 2},
		{0, 200000, 3},    // savings p75
		{5000, 500000, 3}, // 取较大:savings 拉满
		{30000, 1000, 3},  // 取较大:income 拉满
		{9000, 33000, 1},  // 双边同档
	}
	for _, c := range cases {
		if got := WealthTierFor(c.income, c.savings); got != c.want {
			t.Errorf("WealthTierFor(%v,%v) = %d, want %d", c.income, c.savings, got, c.want)
		}
	}
}

// crowdCity 造一座 n 人合成城(确定性 seed)。
func crowdCity(n int) *Backdrop {
	return NewBackdrop(n, rand.New(rand.NewSource(20260929)), nil)
}

// TestCrowdSnapshot_SmallCityOutdoor §6.4:人少全上街(entries 无户内候选)。
func TestCrowdSnapshot_SmallCityOutdoor(t *testing.T) {
	b := crowdCity(50)
	cs := b.CrowdSnapshot(160, 0)
	if cs.Outdoor != 50 || cs.Indoor != 0 {
		t.Fatalf("outdoor/indoor = %d/%d, want 50/0", cs.Outdoor, cs.Indoor)
	}
	if cs.Churn != 0 {
		t.Fatalf("churn = %d, want 0", cs.Churn)
	}
	if len(cs.Entries) != 50 {
		t.Fatalf("entries = %d, want 50(无户内换班候选)", len(cs.Entries))
	}
	for i, e := range cs.Entries {
		if e.Indoor {
			t.Fatalf("entries[%d].Indoor = true, want false(全户外)", i)
		}
		if e.Index < 0 || e.Index >= 50 {
			t.Fatalf("entries[%d].Index = %d 越界", i, e.Index)
		}
	}
}

// TestCrowdSnapshot_LargeCitySplit §6.4:大城触顶 + ≤16 换班候选。
func TestCrowdSnapshot_LargeCitySplit(t *testing.T) {
	b := crowdCity(500)
	cs := b.CrowdSnapshot(160, 0)
	if cs.Outdoor != 160 || cs.Indoor != 340 {
		t.Fatalf("outdoor/indoor = %d/%d, want 160/340", cs.Outdoor, cs.Indoor)
	}
	if len(cs.Entries) != 160+16 {
		t.Fatalf("entries = %d, want 176(CAP+16)", len(cs.Entries))
	}
	var outdoorN, indoorN int
	seen := map[int]bool{}
	for _, e := range cs.Entries {
		if seen[e.Index] {
			t.Fatalf("index %d 重复出现(选择必须互异)", e.Index)
		}
		seen[e.Index] = true
		if e.Indoor {
			indoorN++
		} else {
			outdoorN++
		}
	}
	if outdoorN != 160 || indoorN != 16 {
		t.Fatalf("entry 户外/户内 = %d/%d, want 160/16", outdoorN, indoorN)
	}
}

// TestCrowdSnapshot_Deterministic §6.2 确定性:同 (cap,churn) 同视图;churn
// 代数变化才换人(禁 rng/禁随机)。
func TestCrowdSnapshot_Deterministic(t *testing.T) {
	b := crowdCity(400)
	a1 := b.CrowdSnapshot(160, 7)
	a2 := b.CrowdSnapshot(160, 7)
	if !reflect.DeepEqual(a1, a2) {
		t.Fatal("同 (cap,churn) 两次调用必须完全一致(确定性)")
	}
	// 不同 cap 不同 churn 参数不影响互异性;churn=8 应选出不同身份集合。
	b2 := crowdCity(400) // 同 seed 同城 → 差异只能来自 churn
	b8 := b2.CrowdSnapshot(160, 8)
	same := 0
	for i := range a1.Entries {
		if i < len(b8.Entries) && a1.Entries[i].Index == b8.Entries[i].Index {
			same++
		}
	}
	if same == len(a1.Entries) {
		t.Fatal("churn 变化后身份集合必须变化(换班语义)")
	}
	// 同 churn 在另一实例(同 seed)上也必须一致(纯函数视图,不依赖对象身份)。
	if !reflect.DeepEqual(a1, b2.CrowdSnapshot(160, 7)) {
		t.Fatal("同 seed 同 churn 跨实例必须一致")
	}
}

// TestCrowdSnapshot_EntryFields §6.4 字段值域:Archetype/Gender/Wealth/Domain。
func TestCrowdSnapshot_EntryFields(t *testing.T) {
	b := crowdCity(300)
	cs := b.CrowdSnapshot(160, 0)
	validArch := map[string]bool{
		"char_elder": true, "char_student": true, "char_formal": true,
		"char_business": true, "char_worker": true, "char_service": true,
		"char_parent": true, "char_casual": true,
	}
	for i, e := range cs.Entries {
		if !validArch[e.Archetype] {
			t.Errorf("entries[%d].archetype = %q 非法", i, e.Archetype)
		}
		if e.Gender != "m" && e.Gender != "f" && e.Gender != "u" {
			t.Errorf("entries[%d].gender = %q 非法", i, e.Gender)
		}
		if e.Wealth < 0 || e.Wealth > 3 {
			t.Errorf("entries[%d].wealth = %d 非法", i, e.Wealth)
		}
		if e.Domain < -1 || e.Domain > 25 {
			t.Errorf("entries[%d].domain = %d 非法", i, e.Domain)
		}
		if e.District < 0 || e.District >= districtCount {
			t.Errorf("entries[%d].district = %d 非法", i, e.District)
		}
		if e.Age < 0 {
			t.Errorf("entries[%d].age = %d 非法", i, e.Age)
		}
	}
}

// TestCrowdSnapshot_ProfileProjection §6.4:CardID/Name/Health/Gender 从
// profile 层取;未锚定留空/"u"。
func TestCrowdSnapshot_ProfileProjection(t *testing.T) {
	b := crowdCity(20)
	cards := []profession.DomainCard{
		anchorCard("N9", "测试化名", "工程师", "P-信息与通信技术", "tech", 20000, 8000, 200000, 33),
	}
	cards[0].Gender = "f"
	cards[0].HealthGrade = "A"
	if n := b.AnchorProfiles(cards, []string{"P-信息与通信技术/n9.md"}); n != 1 {
		t.Fatalf("anchored = %d, want 1", n)
	}
	cs := b.CrowdSnapshot(160, 0)
	found := false
	for _, e := range cs.Entries {
		if e.Index == 0 {
			found = true
			if e.CardID != "N9" || e.Name != "测试化名" || e.Health != "A" || e.Gender != "f" {
				t.Fatalf("居民0 投影 = %+v, want CardID=N9/Name=测试化名/Health=A/Gender=f", e)
			}
			// income 20000/savings 200000 → 财富档 3;formal(wealth==3)先于
			// business 命中(§5.1 优先级)。
			if e.Archetype != "char_formal" {
				t.Errorf("居民0 archetype = %q, want char_formal(age33/财富3)", e.Archetype)
			}
		}
	}
	if !found {
		t.Fatal("居民0 必须在 entries 中(N=20 全员上街)")
	}
	// 未锚定居民:CardID/Name/Health 空、Gender "u"。
	for _, e := range cs.Entries {
		if e.Index != 0 {
			if e.CardID != "" || e.Name != "" || e.Health != "" {
				t.Fatalf("未锚定居民 %d 不得有档案字段: %+v", e.Index, e)
			}
			if e.Gender != "u" {
				t.Fatalf("未锚定居民 %d gender = %q, want u", e.Index, e.Gender)
			}
		}
	}
}

// TestCrowdSnapshot_JSONTags §6.4 json tag 与设计文档逐字一致(wire 契约)。
func TestCrowdSnapshot_JSONTags(t *testing.T) {
	b := crowdCity(10)
	raw, err := json.Marshal(b.CrowdSnapshot(160, 3))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(raw)
	for _, key := range []string{
		`"outdoor"`, `"indoor"`, `"churn"`, `"entries"`,
		`"index"`, `"card_id"`, `"name"`, `"archetype"`, `"gender"`,
		`"age"`, `"domain"`, `"district"`, `"wealth"`, `"health"`,
	} {
		if !strings.Contains(s, key) {
			t.Errorf("wire 缺少 key %s: %s", key, s)
		}
	}
	if !strings.Contains(s, `"churn":3`) {
		t.Errorf("churn 未透传: %s", s)
	}
}

// TestSnapshot_IncludesCrowd §130 接线防呆:Snapshot() 必须填充 Crowd 块
// (生产消费点 = game.state.city.crowd 随 BuildClientState 原样序列化下发)。
func TestSnapshot_IncludesCrowd(t *testing.T) {
	b := crowdCity(500)
	s := b.Snapshot()
	if s.Crowd == nil {
		t.Fatal("Snapshot().Crowd 不得为 nil(建城后恒填充)")
	}
	if s.Crowd.Outdoor != OutdoorCount(500, crowdCapHigh) {
		t.Fatalf("crowd.outdoor = %d, want %d", s.Crowd.Outdoor, OutdoorCount(500, crowdCapHigh))
	}
	if s.Crowd.Indoor != 500-s.Crowd.Outdoor {
		t.Fatalf("crowd.indoor = %d, want %d", s.Crowd.Indoor, 500-s.Crowd.Outdoor)
	}
	// churn 代数 = 已 tick 月数(§6.4:CHURN_PERIOD ≈ 1 城市月)。
	if s.Crowd.Churn != 0 {
		t.Fatalf("crowd.churn = %d, want 0(未 tick)", s.Crowd.Churn)
	}
	b.TickMonth(0.002, rand.New(rand.NewSource(1)))
	s2 := b.Snapshot()
	if s2.Crowd == nil || s2.Crowd.Churn != 1 {
		t.Fatalf("tick 后 churn = %+v, want churn=1", s2.Crowd)
	}
}

// TestCrowdSelect_NoRngLeakage 选择不得污染演化 rng 流:连续两次 Snapshot
// 后 TickMonth(同 rng 流)演化结果必须与「只 TickMonth」一致(确定性硬约束)。
func TestCrowdSelect_NoRngLeakage(t *testing.T) {
	base := crowdCity(100)
	touched := crowdCity(100)
	_ = touched.CrowdSnapshot(160, 0)
	_ = touched.CrowdSnapshot(160, 1)
	_ = touched.Snapshot()
	r1 := rand.New(rand.NewSource(9))
	r2 := rand.New(rand.NewSource(9))
	base.TickMonth(0.002, r1)
	touched.TickMonth(0.002, r2)
	if !reflect.DeepEqual(base.residents, touched.residents) {
		t.Fatal("crowd 选择不得消耗/扰动演化 rng(禁耗 rng)")
	}
}
