// Package profession — loader.go: 文档池懒加载器(P0 v1,2026-09-14 §财商流P0)。
//
// 契约: docs/财商流游戏/已实现/03-Agent设计/财商流游戏-职业卡与加载器设计-v1.md §3。
// 三段式:
//   - 阶段 0(NewLoader):不读盘,仅记录根路径 + sync.Once。
//   - 阶段 1(buildIndex):首次抽卡 / HTTP professions 触发;walk 收集 *.md 相对
//     路径清单,**不解析 frontmatter**;结果缓存进程内。
//   - 阶段 2(Draw):均匀抽 n 张不重复 → 逐张读文件 → 解析 frontmatter
//     (gopkg.in/yaml.v3)→ 映射 Card → LRU 缓存(上限 256)。
//
// 失败语义:任一卡解析失败 → 跳过并 logger.Warn + 计数;可用卡 < n → 回退精选
// 补足;根目录不可用 → 全量回退 curated。seed 注入的 *rand.Rand 保证确定性。
package profession

import (
	"fmt"
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"LsmAgentGame/logger"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// lruCapacity 解析缓存上限(加载器文档 §3.1)。
const lruCapacity = 256

// Loader 是文档池懒加载器(并发安全)。
type Loader struct {
	root string // 文档池磁盘根

	mu         sync.RWMutex
	indexOnce  sync.Once
	indexPath  []string // 相对路径清单(排序稳定,保证同 seed 同抽卡)
	indexBuilt bool     // 阶段 1 是否已执行
	available  bool     // 根目录可读且非空
	lru        map[string]*lruEntry
	lruOrder   []string // 最近使用在尾
	parseFail  int      // 解析失败累计(跳过的卡数)
}

type lruEntry struct {
	card Card
	err  error
}

// NewLoader 构造加载器(阶段 0:不读盘)。
func NewLoader(root string) *Loader {
	return &Loader{
		root: strings.TrimSpace(root),
		lru:  make(map[string]*lruEntry),
	}
}

// Root 返回根路径。
func (l *Loader) Root() string { return l.root }

// buildIndex 阶段 1:walk 根目录收集 *.md 相对路径(排序后缓存)。
func (l *Loader) buildIndex() {
	l.indexOnce.Do(func() {
		if l.root == "" {
			l.available = false
			return
		}
		var paths []string
		rootAbs, err := filepath.Abs(l.root)
		if err != nil {
			l.available = false
			return
		}
		err = filepath.WalkDir(l.root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // 单目录读失败跳过,不影响整体
			}
			if d.IsDir() {
				// 路径安全:跳过隐藏目录(_框架 等下划线前缀目录为框架文档,非卡池)。
				name := d.Name()
				if strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".") {
					if path != l.root {
						return filepath.SkipDir
					}
				}
				return nil
			}
			if !strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
				return nil
			}
			abs, err := filepath.Abs(path)
			if err != nil {
				return nil
			}
			// 拒绝符号链接逃逸与根前缀外路径(加载器文档 §3.1)。
			if !strings.HasPrefix(abs, rootAbs+string(os.PathSeparator)) {
				return nil
			}
			rel, err := filepath.Rel(l.root, path)
			if err != nil {
				return nil
			}
			paths = append(paths, rel)
			return nil
		})
		sort.Strings(paths) // 稳定顺序 → 同 seed 同抽卡
		l.indexPath = paths
		l.available = err == nil && len(paths) > 0
		if !l.available {
			logger.L().Warn("wealth profession docs pool unavailable, will fall back to curated",
				zap.String("root", l.root),
				zap.Int("entries", len(paths)))
		}
	})
}

// PoolInfo 返回池状态(HTTP /api/games/wealth/professions 用)。
// total: -1 = 索引未建(available 按根目录存在性判断);≥0 = 索引条目数。
// indexed: 已解析进 LRU 的张数;parseFail: 累计跳过的解析失败卡数。
func (l *Loader) PoolInfo() (available bool, total, indexed, parseFail int) {
	l.mu.RLock()
	built, avail := l.indexBuilt, l.available
	indexed, parseFail = len(l.lru), l.parseFail
	total = -1
	if built {
		total = len(l.indexPath)
	}
	l.mu.RUnlock()
	if built {
		return avail, total, indexed, parseFail
	}
	// 索引未建:按根目录存在性判断(不触发 walk,HTTP 路径保持轻量)。
	if l.root != "" {
		if st, err := os.Stat(l.root); err == nil && st.IsDir() {
			return true, total, indexed, parseFail
		}
	}
	return false, total, indexed, parseFail
}

// ForceIndex 显式触发阶段 1(HTTP professions / 首次抽卡共用)。
func (l *Loader) ForceIndex() {
	l.buildIndex()
	l.mu.Lock()
	l.indexBuilt = true
	l.mu.Unlock()
}

// Draw 从文档池均匀抽 n 张不重复卡;不足/失败部分回退精选补足
//(加载器文档 §3.1 失败语义)。rng 为注入的随机源(同 seed 同索引 → 同卡集)。
func (l *Loader) Draw(n int, rng *rand.Rand) []Card {
	if n <= 0 {
		return nil
	}
	l.ForceIndex()

	l.mu.RLock()
	pool := append([]string(nil), l.indexPath...)
	l.mu.RUnlock()

	var out []Card
	if len(pool) > 0 {
		// Fisher-Yates 部分洗牌:前 n 位即抽中集合。
		for i := 0; i < n && i < len(pool); i++ {
			j := i + rng.Intn(len(pool)-i)
			pool[i], pool[j] = pool[j], pool[i]
		}
		for _, rel := range pool[:min(n, len(pool))] { // builtin min(Go 1.21+)
			if card, err := l.loadCard(rel); err == nil {
				out = append(out, card)
			}
		}
	}
	// 回退精选补足。
	if len(out) < n {
		curated := CuratedCards()
		perm := rng.Perm(len(curated))
		k := 0
		for len(out) < n && k < len(perm) {
			out = append(out, curated[perm[k]])
			k++
		}
	}
	return out
}

// loadCard 读单卡:LRU 命中免读盘;否则读文件 + 解析 frontmatter。
func (l *Loader) loadCard(rel string) (Card, error) {
	l.mu.RLock()
	if e, ok := l.lru[rel]; ok {
		l.mu.RUnlock()
		l.touchLRU(rel)
		return e.card, e.err
	}
	l.mu.RUnlock()

	card, err := l.parseFile(rel)

	l.mu.Lock()
	l.lru[rel] = &lruEntry{card: card, err: err}
	l.lruOrder = append(l.lruOrder, rel)
	for len(l.lruOrder) > lruCapacity {
		oldest := l.lruOrder[0]
		l.lruOrder = l.lruOrder[1:]
		delete(l.lru, oldest)
	}
	if err != nil {
		l.parseFail++
	}
	l.mu.Unlock()
	if err != nil {
		logger.L().Warn("wealth profession card parse failed, skipped",
			zap.String("path", rel), zap.Error(err))
	}
	return card, err
}

func (l *Loader) touchLRU(rel string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i, r := range l.lruOrder {
		if r == rel {
			l.lruOrder = append(append(l.lruOrder[:i:i], l.lruOrder[i+1:]...), rel)
			return
		}
	}
}

// parseFile 读文件并解析 frontmatter(首个 --- 块,YAML)。
func (l *Loader) parseFile(rel string) (Card, error) {
	full := filepath.Join(l.root, rel)
	data, err := os.ReadFile(full)
	if err != nil {
		return Card{}, fmt.Errorf("read %s: %w", rel, err)
	}
	fm := extractFrontmatter(string(data))
	if fm == nil {
		return Card{}, fmt.Errorf("no frontmatter in %s", rel)
	}
	var raw docCard
	if err := yaml.Unmarshal(fm, &raw); err != nil {
		return Card{}, fmt.Errorf("yaml %s: %w", rel, err)
	}
	card, err := l.mapCard(raw, rel)
	if err != nil {
		return Card{}, fmt.Errorf("map %s: %w", rel, err)
	}
	return card, nil
}

// extractFrontmatter 取首个 "---" 围起的 YAML 块。
func extractFrontmatter(s string) []byte {
	lines := strings.SplitN(s, "\n", 4096)
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil
	}
	var b strings.Builder
	for _, ln := range lines[1:] {
		if strings.TrimSpace(ln) == "---" {
			return []byte(b.String())
		}
		b.WriteString(ln)
		b.WriteString("\n")
	}
	return nil
}

// docCard 是文档池 frontmatter 的可空子集(Schema v1.1;缺失字段走默认)。
type docCard struct {
	ID               string   `yaml:"id"`
	LegacyName       string   `yaml:"legacy_name"`
	Occupation       string   `yaml:"occupation"`
	IndustryL3       string   `yaml:"industry_l3"`
	IncomeMonthly    *int64   `yaml:"income_monthly"`
	IncomeStability  string   `yaml:"income_stability"`
	MonthlyExpense   *int64   `yaml:"monthly_expense"`
	SavingsStock     *int64   `yaml:"savings_stock"`
	Age              *int     `yaml:"age"`
	WorkIntensity    string   `yaml:"work_intensity"`
	HealthGrade      string   `yaml:"health_grade"`
	Personality      []string `yaml:"personality"`
	BehaviorTraits   []string `yaml:"behavior_traits"`
	RiskPreference   string   `yaml:"risk_preference"`
	Marital          string   `yaml:"marital"`
	ChildrenCount    *int     `yaml:"children_count"`
	EldersDependent  *int     `yaml:"elders_dependent"`
	OpeningHook      string   `yaml:"opening_hook"`
	GoalsShort       []string `yaml:"goals_short"`
	HousingCity      string   `yaml:"housing_city"`
	EmploymentType   string   `yaml:"employment_type"`
}

// cityToDistrict 城市名 → 8 区启发映射(加载器文档 §4;无命中 → residential)。
var cityToDistrict = []struct {
	Keyword  string
	District string
}{
	{"一线", "finance"},
	{"核心", "finance"},
	{"金融", "finance"},
	{"高新", "tech"},
	{"科技", "tech"},
	{"开发", "tech"},
	{"工业", "industry"},
	{"县城", "oldtown"},
	{"老城", "oldtown"},
	{"商业", "commerce"},
	{"居住", "residential"},
	{"郊区", "suburb"},
	{"新区", "riverside"},
	{"滨海", "riverside"},
}

// mapCard 按 §4 映射表把 frontmatter 转成 Card。
func (l *Loader) mapCard(raw docCard, rel string) (Card, error) {
	c := Card{Source: "docs"}

	c.ID = strings.TrimSpace(raw.ID)
	if c.ID == "" {
		// 文件名编号段兜底:…/<编号>-<姓名>.md。
		base := strings.TrimSuffix(filepath.Base(rel), ".md")
		if i := strings.IndexByte(base, '-'); i > 0 {
			c.ID = base[:i]
		} else {
			c.ID = base
		}
	}
	c.Name = strings.TrimSpace(raw.LegacyName)
	if c.Name == "" {
		if i := strings.LastIndexByte(strings.TrimSuffix(filepath.Base(rel), ".md"), '-'); i >= 0 {
			c.Name = strings.TrimSuffix(filepath.Base(rel), ".md")[i+1:]
		}
	}
	c.Title = strings.TrimSpace(raw.Occupation)
	if c.Title == "" {
		c.Title = strings.TrimSpace(raw.IndustryL3)
	}
	if c.Title == "" {
		return Card{}, fmt.Errorf("missing occupation/industry_l3")
	}
	if raw.IncomeMonthly == nil || *raw.IncomeMonthly <= 0 {
		return Card{}, fmt.Errorf("missing income_monthly")
	}
	c.Salary = *raw.IncomeMonthly
	c.SalaryVolatile = strings.Contains(raw.IncomeStability, "波动") || strings.Contains(raw.IncomeStability, "不稳定")
	if raw.MonthlyExpense != nil && *raw.MonthlyExpense > 0 {
		c.Expense = *raw.MonthlyExpense
	} else {
		c.Expense = c.Salary * 60 / 100 // 兜底 60% 消费率
	}
	// savings_stock 可空 → 兜底 = income_monthly × 6(P0 新定)。
	if raw.SavingsStock != nil {
		c.Savings = *raw.SavingsStock
	} else {
		c.Savings = c.Salary * 6
	}
	c.StartAge = 25
	if raw.Age != nil {
		c.StartAge = *raw.Age
	}
	if c.StartAge < 20 {
		c.StartAge = 20
	}
	if c.StartAge > 55 {
		c.StartAge = 55
	}
	// Energy 由 work_intensity 映射:高→4、中→6、低→8(P0 新定)。
	switch {
	case strings.Contains(raw.WorkIntensity, "高"):
		c.Energy = 4
	case strings.Contains(raw.WorkIntensity, "低"):
		c.Energy = 8
	default:
		c.Energy = 6
	}
	c.Network = 5
	c.Cognition = 5
	c.HealthGrade = strings.TrimSpace(raw.HealthGrade)
	switch c.HealthGrade {
	case "A", "B", "C":
	default:
		c.HealthGrade = "B"
	}
	c.Personality = FilterByVocab(raw.Personality, personalityVocab, 4)
	c.BehaviorTraits = FilterByVocab(raw.BehaviorTraits, behaviorVocab, 4)
	switch strings.TrimSpace(raw.RiskPreference) {
	case "conservative", "balanced", "aggressive":
		c.RiskPreference = strings.TrimSpace(raw.RiskPreference)
	default:
		c.RiskPreference = "balanced"
	}
	if strings.Contains(raw.Marital, "已婚") || strings.TrimSpace(raw.Marital) == "married" {
		c.Marital = "married"
	} else {
		c.Marital = "single"
	}
	if raw.ChildrenCount != nil && *raw.ChildrenCount > 0 {
		c.ChildrenCount = *raw.ChildrenCount
	}
	if raw.EldersDependent != nil && *raw.EldersDependent > 0 {
		c.EldersDependent = *raw.EldersDependent
	}
	// opening_hook:骨架 30–50 字直接使用;>60 rune 截断 + 「…」。
	c.OpeningHook = strings.TrimSpace(raw.OpeningHook)
	if runes := []rune(c.OpeningHook); len(runes) > 60 {
		c.OpeningHook = string(runes[:59]) + "…"
	}
	if len([]rune(c.OpeningHook)) < 20 {
		c.OpeningHook = fmt.Sprintf("我是%s，今年 %d 岁，正在为想要的生活努力攒第一桶金。", c.Title, c.StartAge)
	}
	// goals_short 取前 3 条。
	for i, g := range raw.GoalsShort {
		if i >= 3 {
			break
		}
		if g = strings.TrimSpace(g); g != "" {
			c.Goals = append(c.Goals, g)
		}
	}
	if len(c.Goals) == 0 {
		c.Goals = []string{"5 年内把储蓄翻一番。"}
	}
	// 城市名 → 8 区启发(无命中 → residential)。
	c.HomeDistrict = "residential"
	for _, m := range cityToDistrict {
		if strings.Contains(raw.HousingCity, m.Keyword) {
			c.HomeDistrict = m.District
			break
		}
	}
	// CreditScore 按收入档:≥20000→700、8000–20000→650、<8000→550;
	// 个体/自由就业 -50 下限 500(P0 新定)。
	switch {
	case c.Salary >= 20000:
		c.CreditScore = 700
	case c.Salary >= 8000:
		c.CreditScore = 650
	default:
		c.CreditScore = 550
	}
	if strings.Contains(raw.EmploymentType, "个体") || strings.Contains(raw.EmploymentType, "自由") {
		c.CreditScore -= 50
		if c.CreditScore < 500 {
			c.CreditScore = 500
		}
	}
	return c, nil
}
