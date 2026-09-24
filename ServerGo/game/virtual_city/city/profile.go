// Package city — profile.go: 居民人物卡档案锚定(2026-09-21 §虚拟城市
// 居民档案锚定)。
//
// 契约: lag_docs/虚拟城市/已实现/03-Agent设计/虚拟城市-城市居民人物卡档案锚定设计-v1.md §4。
// AnchorProfiles 是一次性热替换:把文档池水合出的人物卡(数值 + 人设)按
// 居民下标写入 profiles[] 并覆盖 resident 合成数值 —— 档案是事实来源,锚定
// 前的演化值直接被覆盖,此后 TickMonth 照常在档案初值上叠加。全部方法在
// Backdrop.mu 内完成,调用方(room 编排 goroutine)绝不持 r.mu(§92a/契约 §11)。
package city

import (
	"strings"

	"LsmAgentGame/game/virtual_city/profession"
)

// 档案锚定流水线状态(Backdrop.profStatus 取值;导出供 room 编排层回写,
// 线上语义见 ProfileProgress.Status 字符串)。
const (
	ProfIdle      = 0 // 未启动(pool=curated / docLoader=nil,纯合成)
	ProfHydrating = 1 // 批量水合进行中(进度经 Snapshot 轮询下发)
	ProfReady     = 2 // 锚定完成(终态,发一条 city_profiles 事件)
	ProfFailed    = 3 // 流水线失败(池空/根目录缺失;合成兜底)
)

// profStatusNames 状态码 → wire 字符串(下标即状态码)。
var profStatusNames = [4]string{"idle", "hydrating", "ready", "failed"}

// profileExpenseFallbackRatio expense 兜底比(契约 §4:expense=0 时取
// income×0.5)。
const profileExpenseFallbackRatio = 0.5

// ResidentProfile 单名居民的档案投影(全部来自人物卡 frontmatter;未锚定居民
// 没有 profile —— ProfilesPage/ProfileOf 对其返回 ok=false)。
type ResidentProfile struct {
	Index       int     `json:"-"`           // resident 下标
	CardID      string  `json:"card_id"`     // 人物卡编号(N1051443 / P01)
	Name        string  `json:"name"`        // 化名
	Occupation  string  `json:"occupation"`  // 职业
	DomainName  string  `json:"domain_name"` // L1 域展示名
	District    string  `json:"district"`    // 城区展示名(锚定时由 HomeDistrict 映射)
	Age         int     `json:"age"`
	Income      float64 `json:"income"` // 月收入(元,档案值;后续月结演化可变)
	Expense     float64 `json:"expense"`
	Savings     float64 `json:"savings"`
	Employed    bool    `json:"employed"`     // 锚定 income>0;其后随演化 flags
	Stressed    bool    `json:"stressed"`     // 锚定 savings < 3×expense;其后随演化
	Personality string  `json:"personality"`  // 「、」连接(2-4 词)
	OpeningHook string  `json:"opening_hook"` // 档案开场白(≤60 rune,loader 已截)
	Goal        string  `json:"goal"`         // goals[0](5 年目标)
	Marital     string  `json:"marital"`      // single|married
	HealthGrade string  `json:"health_grade"` // A|B|C
	SourceFile  string  `json:"source_file"`  // 相对路径(审计:可回溯源 md)
}

// ProfileProgress 档案锚定进度(随 game.state.city.profiles 下发 + REST
// residents 列表头)。
type ProfileProgress struct {
	Status   string `json:"status"` // idle|hydrating|ready|failed
	Done     int    `json:"done"`
	Total    int    `json:"total"`
	PoolSize int    `json:"pool_size"` // 文档池人物卡总数(2026-09-21 实测 100,267)
	Anchored int    `json:"anchored"`  // 已锚定居民数
}

// SetProfileProgress 回写锚定流水线进度(room 编排 goroutine 消费;锁内)。
func (b *Backdrop) SetProfileProgress(status, done, total, poolSize int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.profStatus, b.profDone, b.profTotal, b.profPoolSize = status, done, total, poolSize
}

// AnchorProfiles 把水合成功的 cards 按下标锚定到居民:覆盖 income/expense/
// savings/age/domain/district/flags,并把全量档案投影写入 profiles[i]
// (SourceFile 取 sourceFiles[i],两数组按成功序一一对应)。
// len(cards) < len(residents)(池不足):只锚定前 len(cards) 名,其余保持
// 合成 —— ProfileProgress.Anchored 如实披露。返回锚定成功数。
// 数值覆盖规则(契约 §4 表格):
//
//	income  clamp [0, 200000](对齐合成 incomeMax)
//	expense clamp [0,∞),0 → 兜底 income×0.5
//	savings clamp [0,∞)
//	age     clamp [18, 70]
//	domain  DomainCard.Domain(路径 L1 段)→ 域索引;解析失败保持原值
//	district Card.HomeDistrict → profession.DistrictIDs() 下标;未命中保持原值
//	employed = income>0;stressed = savings < 3×expense(对齐合成判定)
func (b *Backdrop) AnchorProfiles(cards []profession.DomainCard, sourceFiles []string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.profiles) != len(b.residents) {
		b.profiles = make([]ResidentProfile, len(b.residents))
	}
	n := len(cards)
	if n > len(b.residents) {
		n = len(b.residents)
	}
	for i := 0; i < n; i++ {
		c := &cards[i]
		r := &b.residents[i]

		// 数值(契约 §4 clamp 规则)。
		income := clampF(float64(c.Salary), 0, incomeMax)
		expense := pos(float64(c.Expense)) // clamp [0,∞)
		if expense <= 0 {
			expense = income * profileExpenseFallbackRatio
		}
		savings := pos(float64(c.Savings))
		age := int(clampF(float64(c.StartAge), ageMin, ageMax))

		// 域/城区(解析失败保持合成原值)。
		di := domainIndex(c.Domain)
		if di >= 0 {
			r.domain = uint8(di)
		}
		if idx, ok := districtIDIndex[c.HomeDistrict]; ok && idx >= 0 && idx < districtCount {
			r.district = uint8(idx)
		}

		// 就业/压力位(保留 voiced 位 —— 城市之声去重标记不属于锚定语义)。
		employed := income > 0
		stressed := savings < stressExpenseMonths*expense
		r.flags &^= flagEmployed | flagStressed
		if employed {
			r.flags |= flagEmployed
		}
		if stressed {
			r.flags |= flagStressed
		}
		r.income = float32(income)
		r.expense = float32(expense)
		r.savings = savings
		r.age = uint8(age)

		// 档案投影。
		domainName := ""
		if di >= 0 && di < len(l1DomainNames) {
			domainName = l1DomainNames[di]
		}
		districtName := ""
		if int(r.district) < districtCount {
			districtName = districtNames[r.district]
		}
		goal := ""
		if len(c.Goals) > 0 {
			goal = c.Goals[0]
		}
		src := ""
		if i < len(sourceFiles) {
			src = sourceFiles[i]
		}
		b.profiles[i] = ResidentProfile{
			Index:       i,
			CardID:      c.ID,
			Name:        c.Name,
			Occupation:  c.Title,
			DomainName:  domainName,
			District:    districtName,
			Age:         age,
			Income:      income,
			Expense:     expense,
			Savings:     savings,
			Employed:    employed,
			Stressed:    stressed,
			Personality: strings.Join(c.Personality, "、"),
			OpeningHook: c.OpeningHook,
			Goal:        goal,
			Marital:     c.Marital,
			HealthGrade: c.HealthGrade,
			SourceFile:  src,
		}
	}
	b.profAnchored = n
	return n
}

// ProfileOf 按居民下标取档案(城市之声/单卡详情用);未锚定 → ok=false。
func (b *Backdrop) ProfileOf(idx int) (ResidentProfile, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if idx < 0 || idx >= b.profAnchored || idx >= len(b.profiles) {
		return ResidentProfile{}, false
	}
	return b.profiles[idx], true
}

// ProfileByCardID 按人物卡编号查档案(REST /residents/:cardId 用;锚定卡号
// 全城唯一 —— DrawPaths 不重复抽取)。线性扫已锚定前缀,10 万级 <5ms。
func (b *Backdrop) ProfileByCardID(cardID string) (ResidentProfile, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := 0; i < b.profAnchored && i < len(b.profiles); i++ {
		if b.profiles[i].CardID == cardID {
			return b.profiles[i], true
		}
	}
	return ResidentProfile{}, false
}

// ProfilesPage 分页档案列表:q 非空时线性扫 name/occupation/card_id 包含
// 匹配(大小写折叠,10 万 <5ms);返回(页, 命中总数, 进度)。只覆盖已锚定
// 前缀 —— 未锚定居民没有档案,不进列表(契约 §4)。
func (b *Backdrop) ProfilesPage(offset, limit int, q string) ([]ResidentProfile, int, ProfileProgress) {
	b.mu.Lock()
	defer b.mu.Unlock()
	prog := b.profileProgressLocked()
	anchored := b.profAnchored
	if anchored > len(b.profiles) {
		anchored = len(b.profiles)
	}
	q = strings.TrimSpace(q)
	needle := strings.ToLower(q)
	sel := make([]int, 0, anchored)
	for i := 0; i < anchored; i++ {
		if needle == "" || profileMatches(&b.profiles[i], needle) {
			sel = append(sel, i)
		}
	}
	if offset < 0 {
		offset = 0
	}
	if offset > len(sel) {
		offset = len(sel)
	}
	if limit < 0 {
		limit = 0
	}
	end := offset + limit
	if end > len(sel) {
		end = len(sel)
	}
	out := make([]ResidentProfile, 0, end-offset)
	for _, i := range sel[offset:end] {
		out = append(out, b.profiles[i])
	}
	return out, len(sel), prog
}

// profileMatches q 包含匹配(调用方已 ToLower;中文字符不受影响)。
func profileMatches(p *ResidentProfile, needle string) bool {
	return strings.Contains(strings.ToLower(p.Name), needle) ||
		strings.Contains(strings.ToLower(p.Occupation), needle) ||
		strings.Contains(strings.ToLower(p.CardID), needle)
}

// ProfileProgress 返回当前锚定进度(锁内快照)。
func (b *Backdrop) ProfileProgress() ProfileProgress {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.profileProgressLocked()
}

// profileProgressLocked 进度快照(须持 b.mu)。
func (b *Backdrop) profileProgressLocked() ProfileProgress {
	status := "idle"
	if b.profStatus >= ProfIdle && int(b.profStatus) < len(profStatusNames) {
		status = profStatusNames[b.profStatus]
	}
	return ProfileProgress{
		Status:   status,
		Done:     b.profDone,
		Total:    b.profTotal,
		PoolSize: b.profPoolSize,
		Anchored: b.profAnchored,
	}
}
