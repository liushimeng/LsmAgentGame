// Package city — profile_test.go: 居民人物卡档案锚定契约测试
// (2026-09-21 §档案锚定 §12 G3/G4/G5/G6)。
//
//	AnchorProfiles 数值覆盖/clamp、employed/stressed 判定、投影字段、池不足
//	ProfilesPage 分页边界、q 匹配、进度字段
//	AnchorProfiles × TickMonth 并发(-race)
//	城市之声锚定 persona(prompt 带真实档案;VoiceRecord 增字段;未锚定退化代号)
package city

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"

	"LsmAgentGame/game/virtual_city/profession"
	llmtypes "LsmAgentGame/llm/types"
)

// anchorCard 构造一张测试 DomainCard(绕过 loader,直接控值测 clamp 规则)。
func anchorCard(id, name, occupation, domain, district string, salary, expense, savings int64, age int) profession.DomainCard {
	return profession.DomainCard{
		Card: profession.Card{
			ID: id, Name: name, Title: occupation,
			Salary: salary, Expense: expense, Savings: savings,
			StartAge: age, HealthGrade: "A", Marital: "single",
			Personality:  []string{"务实主义", "尽责坚韧"},
			OpeningHook:  "我是档案锚定测试居民,正在验证契约数值覆盖规则。",
			Goals:        []string{"5 年内把储蓄翻一番。"},
			HomeDistrict: district,
		},
		Domain: domain,
	}
}

// TestAnchorProfiles(§12 G3):数值覆盖/clamp、employed/stressed 判定、
// profiles 投影字段、池不足时 Anchored=n_pool。
func TestAnchorProfiles(t *testing.T) {
	b := NewBackdrop(5, rand.New(rand.NewSource(21)), nil)
	cards := []profession.DomainCard{
		// 0:收入超 clamp 上限、expense=0 兜底、年龄下限 clamp、域/城区可解析。
		anchorCard("N1", "张三", "银行职员", "Q-金融与保险", "oldtown", 300000, 0, 1000, 15),
		// 1:收入 0(如实失业)、储蓄低于 3×月支出(压力位)、年龄上限 clamp、
		//   域/城区不可解析(保持合成原值;小写首字符 → domainIndex=-1)。
		anchorCard("N2", "李四", "待业青年", "w-不存在域", "nowhere", 0, 4000, 1000, 80),
		// 2:常规值(无 clamp;储蓄充足不压力)。
		anchorCard("N3", "王五", "软件工程师", "P-信息与通信技术", "tech", 5000, 3000, 100000, 40),
	}
	// 锚定前记录居民 1 的合成 domain/district(不可解析 → 必须保持原值)。
	b.mu.Lock()
	synthDomain1 := b.residents[1].domain
	synthDistrict1 := b.residents[1].district
	b.mu.Unlock()

	anchored := b.AnchorProfiles(cards, []string{"Q-金融与保险/n1.md", "Q-金融与保险/n2.md", "P-信息与通信技术/n3.md"})
	if anchored != 3 {
		t.Fatalf("anchored = %d, want 3", anchored)
	}

	// 居民 0:clamp 与投影。
	r0 := func() resident {
		b.mu.Lock()
		defer b.mu.Unlock()
		return b.residents[0]
	}()
	if r0.income != 200000 || r0.expense != 100000 || r0.savings != 1000 || r0.age != 18 {
		t.Fatalf("resident0 = income %f expense %f savings %f age %d; want 200000/100000(=income×0.5 兜底)/1000/18(clamp)",
			r0.income, r0.expense, r0.savings, r0.age)
	}
	if r0.domain != 16 { // Q → 16
		t.Fatalf("resident0 domain = %d, want 16 (Q-金融与保险)", r0.domain)
	}
	if r0.district != 3 { // oldtown → profession.DistrictIDs 下标 3
		t.Fatalf("resident0 district = %d, want 3 (oldtown)", r0.district)
	}
	if r0.flags&flagEmployed == 0 {
		t.Fatal("resident0 income>0 must be employed")
	}
	if r0.flags&flagStressed == 0 {
		t.Fatal("resident0 savings(1000) < 3×expense(300000) must be stressed")
	}

	// 居民 1:失业 + 压力 + 域/城区保持合成原值。
	r1 := func() resident {
		b.mu.Lock()
		defer b.mu.Unlock()
		return b.residents[1]
	}()
	if r1.flags&flagEmployed != 0 {
		t.Fatal("resident1 income=0 must be unemployed")
	}
	if r1.flags&flagStressed == 0 {
		t.Fatal("resident1 savings(1000) < 3×expense(12000) must be stressed")
	}
	if r1.domain != synthDomain1 || r1.district != synthDistrict1 {
		t.Fatalf("resident1 unresolvable domain/district must keep synthetic values: %d/%d vs %d/%d",
			r1.domain, r1.district, synthDomain1, synthDistrict1)
	}
	if r1.age != 70 {
		t.Fatalf("resident1 age = %d, want 70 (clamp 上限)", r1.age)
	}

	// 居民 2:常规值,储蓄充足 → 不压力。
	r2 := func() resident {
		b.mu.Lock()
		defer b.mu.Unlock()
		return b.residents[2]
	}()
	if r2.flags&flagEmployed == 0 || r2.flags&flagStressed != 0 {
		t.Fatalf("resident2 flags = %d, want employed set / stressed clear", r2.flags)
	}

	// 投影字段(居民 0)。
	p0, ok := b.ProfileOf(0)
	if !ok {
		t.Fatal("ProfileOf(0) must be ok")
	}
	if p0.CardID != "N1" || p0.Name != "张三" || p0.Occupation != "银行职员" ||
		p0.DomainName != "Q-金融与保险" || p0.District != "老城区" ||
		p0.Age != 18 || p0.Income != 200000 || p0.Expense != 100000 || p0.Savings != 1000 ||
		!p0.Employed || !p0.Stressed ||
		p0.Personality != "务实主义、尽责坚韧" ||
		!strings.Contains(p0.OpeningHook, "档案锚定测试居民") ||
		p0.Goal != "5 年内把储蓄翻一番。" || p0.Marital != "single" || p0.HealthGrade != "A" ||
		p0.SourceFile != "Q-金融与保险/n1.md" {
		t.Fatalf("profile0 projection drifted: %+v", p0)
	}
	// 池不足:居民 3/4 保持合成,无档案。
	if _, ok := b.ProfileOf(3); ok {
		t.Fatal("residents beyond card count must have no profile")
	}
	if _, ok := b.ProfileOf(4); ok {
		t.Fatal("residents beyond card count must have no profile")
	}
	if _, ok := b.ProfileOf(-1); ok {
		t.Fatal("negative index must be not-ok")
	}
	if _, ok := b.ProfileOf(999); ok {
		t.Fatal("out-of-range index must be not-ok")
	}
	prog := b.ProfileProgress()
	if prog.Anchored != 3 {
		t.Fatalf("progress anchored = %d, want 3 (池不足如实披露)", prog.Anchored)
	}
}

// TestAnchorProfiles_ByID(§7 REST 单卡查询依赖):卡号唯一可查。
func TestAnchorProfiles_ByID(t *testing.T) {
	b := NewBackdrop(3, rand.New(rand.NewSource(22)), nil)
	cards := []profession.DomainCard{
		anchorCard("NA", "甲", "职业甲", "Q-金融与保险", "finance", 8000, 4000, 50000, 30),
		anchorCard("NB", "乙", "职业乙", "T-教育与培训", "suburb", 7000, 3500, 40000, 35),
	}
	b.AnchorProfiles(cards, []string{"a.md", "b.md"})
	if p, ok := b.ProfileByCardID("NB"); !ok || p.Name != "乙" {
		t.Fatalf("ProfileByCardID(NB) = %+v ok=%v", p, ok)
	}
	if _, ok := b.ProfileByCardID("NOPE"); ok {
		t.Fatal("unknown card id must be not-ok")
	}
}

// TestProfilesPage(§12 G4):分页边界、q 匹配、进度字段。
func TestProfilesPage(t *testing.T) {
	b := NewBackdrop(6, rand.New(rand.NewSource(23)), nil)
	cards := make([]profession.DomainCard, 0, 4)
	srcs := make([]string, 0, 4)
	for i := 0; i < 4; i++ {
		cards = append(cards, anchorCard(
			fmt.Sprintf("N%d", i), fmt.Sprintf("居民%d", i), fmt.Sprintf("职业%02d", i),
			"Q-金融与保险", "finance", int64(6000+i*100), 3000, 30000, 28))
		srcs = append(srcs, fmt.Sprintf("Q-金融与保险/n%d.md", i))
	}
	b.AnchorProfiles(cards, srcs)
	b.SetProfileProgress(ProfReady, 4, 4, 100267)

	// 全量:matched=4;limit 截断。
	page, matched, prog := b.ProfilesPage(0, 2, "")
	if len(page) != 2 || matched != 4 {
		t.Fatalf("page0 len=%d matched=%d, want 2/4", len(page), matched)
	}
	if page[0].Index != 0 || page[1].Index != 1 {
		t.Fatalf("page order drifted: idx %d/%d", page[0].Index, page[1].Index)
	}
	// 中页 + 尾页(不足 limit)。
	page, matched, _ = b.ProfilesPage(2, 2, "")
	if len(page) != 2 || matched != 4 || page[0].Index != 2 {
		t.Fatalf("page1 unexpected: len=%d matched=%d", len(page), matched)
	}
	page, _, _ = b.ProfilesPage(3, 10, "")
	if len(page) != 1 {
		t.Fatalf("tail page len = %d, want 1", len(page))
	}
	// 越界 offset → 空页。
	page, _, _ = b.ProfilesPage(99, 2, "")
	if len(page) != 0 {
		t.Fatalf("offset beyond end must be empty, got %d", len(page))
	}
	// q 匹配:姓名 / 职业 / 卡号。
	_, m1, _ := b.ProfilesPage(0, 50, "居民2")
	if m1 != 1 {
		t.Fatalf("q by name matched = %d, want 1", m1)
	}
	_, m2, _ := b.ProfilesPage(0, 50, "职业03")
	if m2 != 1 {
		t.Fatalf("q by occupation matched = %d, want 1", m2)
	}
	_, m3, _ := b.ProfilesPage(0, 50, "N3")
	if m3 != 1 {
		t.Fatalf("q by card_id matched = %d, want 1", m3)
	}
	_, m4, _ := b.ProfilesPage(0, 50, "查无此人")
	if m4 != 0 {
		t.Fatalf("q miss matched = %d, want 0", m4)
	}
	// 进度字段。
	if prog.Status != "ready" || prog.Done != 4 || prog.Total != 4 || prog.PoolSize != 100267 || prog.Anchored != 4 {
		t.Fatalf("progress drifted: %+v", prog)
	}
	// 未锚定居民不进列表(前缀外)。
	_, matchedAll, _ := b.ProfilesPage(0, 50, "")
	if matchedAll != 4 {
		t.Fatalf("unanchored residents must stay out of listing, matched = %d", matchedAll)
	}
}

// TestAnchorConcurrentTick(§12 G5,-race):AnchorProfiles 与 TickMonth 并发
// 无数据竞争(同走 Backdrop.mu,天然串行)。
func TestAnchorConcurrentTick(t *testing.T) {
	b := NewBackdrop(2000, rand.New(rand.NewSource(24)), nil)
	cards := make([]profession.DomainCard, 0, 800)
	for i := 0; i < 800; i++ {
		cards = append(cards, anchorCard(
			fmt.Sprintf("N%04d", i), fmt.Sprintf("居民%04d", i), "并发测试职业",
			"P-信息与通信技术", "tech", int64(5000+i), 3000, 20000, 30))
	}
	tickRng := rand.New(rand.NewSource(25))
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			b.TickMonth(0.002, tickRng)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			b.AnchorProfiles(cards[:400+i*20], nil)
			b.SetProfileProgress(ProfHydrating, i, 20, 1000)
		}
	}()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("concurrent anchor/tick deadlocked")
	}
	if p, ok := b.ProfileOf(10); !ok || p.CardID == "" {
		t.Fatalf("final anchor lost: ok=%v profile=%+v", ok, p)
	}
}

// ── §12 G6:城市之声档案 persona ──

// capturingVoiceProvider 捕获 system prompt 的 fake provider(文本应答)。
type capturingVoiceProvider struct {
	mu      sync.Mutex
	systems []string
}

func (f *capturingVoiceProvider) Chat(_ context.Context, _ string, req llmtypes.LLMRequest) (llmtypes.LLMResponse, error) {
	sys := ""
	for _, b := range req.System {
		sys += b.Text
	}
	f.mu.Lock()
	f.systems = append(f.systems, sys)
	f.mu.Unlock()
	return llmtypes.LLMResponse{
		StopReason: "end_turn",
		Content:    []llmtypes.ContentBlock{{Type: "text", Text: "这个月想多攒点钱。"}},
	}, nil
}

func (f *capturingVoiceProvider) ChatStream(_ context.Context, _ string, _ llmtypes.LLMRequest) (io.ReadCloser, error) {
	return nil, fmt.Errorf("fake: no stream")
}

func (f *capturingVoiceProvider) ProviderType() string { return "fake-capture-voice" }

// TestVoiceScheduler_AnchoredPersona(§12 G6):锚定后 prompt 带姓名/职业/
// 人格,VoiceRecord 含 resident_id/occupation;未锚定退化为代号(零变化)。
func TestVoiceScheduler_AnchoredPersona(t *testing.T) {
	b := NewBackdrop(30, rand.New(rand.NewSource(26)), nil)
	cards := make([]profession.DomainCard, 0, 30)
	for i := 0; i < 30; i++ {
		cards = append(cards, anchorCard(
			fmt.Sprintf("CARD%02d", i), fmt.Sprintf("named%02d", i), fmt.Sprintf("歌手%02d", i),
			"V-文化传媒体育与娱乐", "oldtown", 9000, 4000, 50000, 29+i%10))
	}
	b.AnchorProfiles(cards, nil)

	fp := &capturingVoiceProvider{}
	sched := NewVoiceScheduler(true, 3, newFakePool(fp))
	var recs []VoiceRecord
	sched.Run(b, 2, func(vr VoiceRecord) { recs = append(recs, vr) })
	if len(recs) != 3 {
		t.Fatalf("voices = %d, want 3", len(recs))
	}
	fp.mu.Lock()
	defer fp.mu.Unlock()
	if len(fp.systems) != 3 {
		t.Fatalf("captured systems = %d, want 3", len(fp.systems))
	}
	for i, vr := range recs {
		// VoiceRecord 增字段。
		if vr.ResidentID == "" || vr.Occupation == "" {
			t.Fatalf("anchored voice %d missing resident_id/occupation: %+v", i, vr)
		}
		if !strings.HasPrefix(vr.ResidentID, "CARD") {
			t.Fatalf("anchored voice %d resident_id = %q, want CARDxx", i, vr.ResidentID)
		}
		// prompt 带真实档案(姓名/职业/人格/目标)。
		sys := fp.systems[i]
		if !strings.Contains(sys, "居民「named") || !strings.Contains(sys, "歌手") ||
			!strings.Contains(sys, "务实主义") || !strings.Contains(sys, "5 年目标") ||
			!strings.Contains(sys, "老城区") {
			t.Fatalf("anchored prompt missing persona fields: %q", sys)
		}
		if strings.Contains(sys, "代号") {
			t.Fatalf("anchored prompt must not fall back to codename: %q", sys)
		}
	}
}

// TestVoiceScheduler_UnanchoredCodename(§12 G6):未锚定退化为代号,
// VoiceRecord 两新字段为空(前端兼容)。
func TestVoiceScheduler_UnanchoredCodename(t *testing.T) {
	b := NewBackdrop(30, rand.New(rand.NewSource(27)), nil)
	fp := &capturingVoiceProvider{}
	sched := NewVoiceScheduler(true, 2, newFakePool(fp))
	var recs []VoiceRecord
	sched.Run(b, 1, func(vr VoiceRecord) { recs = append(recs, vr) })
	if len(recs) != 2 {
		t.Fatalf("voices = %d, want 2", len(recs))
	}
	fp.mu.Lock()
	defer fp.mu.Unlock()
	for i, vr := range recs {
		if vr.ResidentID != "" || vr.Occupation != "" {
			t.Fatalf("unanchored voice %d must leave new fields empty: %+v", i, vr)
		}
		// 代号形状:<域字母><序号>·<城区>。
		if !strings.Contains(vr.Name, "·") {
			t.Fatalf("unanchored name %q must be codename (域字母序号·城区)", vr.Name)
		}
		if !strings.Contains(fp.systems[i], "代号") {
			t.Fatalf("unanchored prompt must keep codename semantics: %q", fp.systems[i])
		}
	}
}

// TestSnapshot_ProfilesProgress:锚定中/后的快照透出;纯合成房不下发。
func TestSnapshot_ProfilesProgress(t *testing.T) {
	// 纯合成(从未启动锚定)→ profiles 块 omit。
	b := NewBackdrop(10, rand.New(rand.NewSource(28)), nil)
	if s := b.Snapshot(); s.Profiles != nil {
		t.Fatalf("pure synthetic city must omit profiles block, got %+v", s.Profiles)
	}
	// hydrating → 进度随快照下发。
	b.SetProfileProgress(ProfHydrating, 5, 10, 100267)
	s := b.Snapshot()
	if s.Profiles == nil || s.Profiles.Status != "hydrating" || s.Profiles.Done != 5 || s.Profiles.Total != 10 || s.Profiles.PoolSize != 100267 {
		t.Fatalf("hydrating snapshot profiles = %+v", s.Profiles)
	}
}
