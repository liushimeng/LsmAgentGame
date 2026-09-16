// Package profession — frontmatter.go: 文档池 frontmatter 容错解析
// (2026-09-16 §文档池解析修复 P0)。
//
// 事故背景: 知识库 `docs/财商流游戏/玩家职业设计/`(75,115 张人物卡,
// Schema v1.0 → v1.1 混存)的真实 frontmatter 形状与旧 `docCard` 不匹配:
//
//	work_intensity:            {weekly_hours: 48, overtime: 中, risk: 中}   ← map,旧代码按 string 解
//	goals_short:               [{horizon: 开局, text: …}, …]                ← []map,旧代码按 []string 解
//	employment: 全职                                                          ← 字段名不是 employment_type
//	name: 彭民凯                                                              ← 字段名不是 legacy_name
//
// yaml.v3 遇到 "cannot unmarshal !!map into string" 会让**整张卡**解析失败 →
// 抽样 12/12 全错 → Draw 永远回退 curated,75k 文档池在运行时完全没被用上。
//
// 修复原则(硬约束): **不得为迁就代码去批量改 75k 张卡的 YAML**。改为在解析层
// 做形状容错 —— 同一字段的历史两种形状(纯标量 / 结构化 map)都吃下;缺失或异常
// 一律降级为「零值 + 兜底」,绝不因单字段形状拒绝整卡。
package profession

import (
	"fmt"
	"hash/fnv"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ─────────────────── 形状容错原语 ───────────────────

// flexText 容错标量文本:
//   - 纯字符串(Schema v1.0):直取;
//   - 结构体 map(Schema v1.1):优先取 text/desc/name/value/label/goal/level,
//     否则按 key 字典序取第一个非空标量值(确定性,不依赖 map 遍历序);
//   - 数组:各项递归取文本后用「、」拼接;
//   - null / 其它形状:空串(交由 mapCard 兜底)。
//
// 任何形状都不返回 error —— 单字段形状异常不得让整卡解析失败。
type flexText struct{ Text string }

// flexTextPriorityKeys 是 map 形状下优先取用的文本键(按序尝试)。
var flexTextPriorityKeys = []string{"text", "desc", "description", "name", "value", "label", "goal", "level", "title"}

// UnmarshalYAML 实现 yaml.Unmarshaler(形状容错;永不报错)。
func (f *flexText) UnmarshalYAML(value *yaml.Node) error {
	if value == nil {
		return nil
	}
	switch value.Kind {
	case yaml.ScalarNode:
		f.Text = strings.TrimSpace(value.Value)
	case yaml.MappingNode:
		f.Text = mapNodeText(value)
	case yaml.SequenceNode:
		var items []flexText
		if err := value.Decode(&items); err == nil {
			parts := make([]string, 0, len(items))
			for _, it := range items {
				if it.Text != "" {
					parts = append(parts, it.Text)
				}
			}
			f.Text = strings.Join(parts, "、")
		}
	}
	return nil
}

// String 返回文本值(便于直接当 string 用)。
func (f flexText) String() string { return f.Text }

// mapNodeText 从 MappingNode 里抽取一个代表性文本值。
func mapNodeText(node *yaml.Node) string {
	var m map[string]yaml.Node
	if err := node.Decode(&m); err != nil || len(m) == 0 {
		return ""
	}
	for _, k := range flexTextPriorityKeys {
		if n, ok := m[k]; ok && n.Kind == yaml.ScalarNode {
			if s := strings.TrimSpace(n.Value); s != "" {
				return s
			}
		}
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys) // 确定性:map 遍历序随机,必须排序后取值
	for _, k := range keys {
		if n := m[k]; n.Kind == yaml.ScalarNode {
			if s := strings.TrimSpace(n.Value); s != "" {
				return s
			}
		}
	}
	return ""
}

// flexInt 容错整数:兼容 19500 / "19500" / 19500.0 / "19,500" / "1.95万" / null。
// Valid=false 表示缺失或不可解析(调用方走兜底)。
type flexInt struct {
	Value int64
	Valid bool
}

// Int 返回整数值(缺失时 0)。
func (f flexInt) Int() int { return int(f.Value) }

// UnmarshalYAML 实现 yaml.Unmarshaler(形状容错;永不报错)。
func (f *flexInt) UnmarshalYAML(value *yaml.Node) error {
	if value == nil || value.Kind != yaml.ScalarNode {
		if value != nil && value.Kind == yaml.MappingNode {
			// 结构化金额(如 {value_cny: 120000})→ 抽代表值再解。
			if s := mapNodeText(value); s != "" {
				*f = parseFlexInt(s)
			}
		}
		return nil
	}
	*f = parseFlexInt(strings.TrimSpace(value.Value))
	return nil
}

// parseFlexInt 解析宽松整数文本(空/null/不可解析 → Valid=false)。
func parseFlexInt(s string) flexInt {
	s = strings.TrimSpace(strings.ReplaceAll(s, ",", ""))
	if s == "" || s == "null" || s == "~" || s == "未知" || s == "无" {
		return flexInt{}
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return flexInt{Value: n, Valid: true}
	}
	if fl, err := strconv.ParseFloat(s, 64); err == nil {
		return flexInt{Value: int64(fl + 0.5), Valid: fl >= 0}
	}
	// "1.95万" / "12万" 中文数量级。
	if strings.HasSuffix(s, "万") {
		if fl, err := strconv.ParseFloat(strings.TrimSuffix(s, "万"), 64); err == nil {
			return flexInt{Value: int64(fl*10000 + 0.5), Valid: fl >= 0}
		}
	}
	return flexInt{}
}

// flexTextList 容错字符串数组:兼容 ["a","b"] / "a" / [{text: a}, …] / null。
type flexTextList struct{ Items []string }

// UnmarshalYAML 实现 yaml.Unmarshaler(形状容错;永不报错)。
func (l *flexTextList) UnmarshalYAML(value *yaml.Node) error {
	if value == nil {
		return nil
	}
	switch value.Kind {
	case yaml.ScalarNode:
		if s := strings.TrimSpace(value.Value); s != "" && s != "null" && s != "~" {
			l.Items = []string{s}
		}
	case yaml.SequenceNode:
		var nodes []yaml.Node
		if err := value.Decode(&nodes); err != nil {
			return nil
		}
		out := make([]string, 0, len(nodes))
		for i := range nodes {
			n := &nodes[i]
			var s string
			switch n.Kind {
			case yaml.ScalarNode:
				s = strings.TrimSpace(n.Value)
			case yaml.MappingNode:
				s = mapNodeText(n)
			}
			if s != "" && s != "null" {
				out = append(out, s)
			}
		}
		l.Items = out
	case yaml.MappingNode:
		if s := mapNodeText(value); s != "" {
			l.Items = []string{s}
		}
	}
	return nil
}

// workIntensitySpec 兼容两种形状的劳动强度:
//   - Schema v1.0 纯标量:"高" | "中" | "低"
//   - Schema v1.1 结构体:{weekly_hours: 48, overtime: 中, risk: 中}
//
// Level 是归一化后的档位(高/中/低;空 = 未知),Energy 映射只读 Level。
type workIntensitySpec struct {
	Level       string
	WeeklyHours int
	Overtime    string
	Risk        string
}

// UnmarshalYAML 实现 yaml.Unmarshaler(形状容错;永不报错)。
func (w *workIntensitySpec) UnmarshalYAML(value *yaml.Node) error {
	if value == nil {
		return nil
	}
	switch value.Kind {
	case yaml.ScalarNode:
		w.Level = normalizeIntensityLevel(strings.TrimSpace(value.Value))
	case yaml.MappingNode:
		var m map[string]yaml.Node
		if err := value.Decode(&m); err != nil {
			return nil
		}
		if n, ok := m["weekly_hours"]; ok {
			w.WeeklyHours = int(parseFlexInt(scalarText(&n)).Value)
		}
		if n, ok := m["overtime"]; ok {
			w.Overtime = strings.TrimSpace(textOfNode(&n))
		}
		if n, ok := m["risk"]; ok {
			w.Risk = strings.TrimSpace(textOfNode(&n))
		}
		if n, ok := m["level"]; ok {
			w.Level = normalizeIntensityLevel(strings.TrimSpace(textOfNode(&n)))
		}
		if w.Level == "" {
			w.Level = deriveIntensityLevel(w.Overtime, w.WeeklyHours)
		}
	}
	return nil
}

// textOfNode 取任意节点的文本(标量直取;map 取代表键)。
func textOfNode(n *yaml.Node) string {
	if n == nil {
		return ""
	}
	switch n.Kind {
	case yaml.ScalarNode:
		return strings.TrimSpace(n.Value)
	case yaml.MappingNode:
		return mapNodeText(n)
	}
	return ""
}

// scalarText 取标量节点文本(非标量返回空)。
func scalarText(n *yaml.Node) string {
	if n != nil && n.Kind == yaml.ScalarNode {
		return strings.TrimSpace(n.Value)
	}
	return ""
}

// normalizeIntensityLevel 把任意强度文本归一到 高/中/低(未命中 → 空)。
func normalizeIntensityLevel(s string) string {
	switch {
	case strings.Contains(s, "高"), strings.Contains(s, "重"), strings.Contains(s, "强度大"):
		return "高"
	case strings.Contains(s, "低"), strings.Contains(s, "轻"):
		return "低"
	case strings.Contains(s, "中"):
		return "中"
	}
	return ""
}

// deriveIntensityLevel 结构体形状下的档位推导公式(2026-09-16 新定,写入
// 加载器文档 §4):
//  1. overtime 显式档位(高/中/低)优先 —— 真实卡 100% 带该字段;
//  2. 否则按 weekly_hours 分档:≥50 → 高;40–49 → 中;<40 → 低;
//  3. 两者皆缺 → 中(与旧实现「default → Energy 6」等价,保证零回归)。
func deriveIntensityLevel(overtime string, weeklyHours int) string {
	if lv := normalizeIntensityLevel(overtime); lv != "" {
		return lv
	}
	switch {
	case weeklyHours >= 50:
		return "高"
	case weeklyHours > 0 && weeklyHours < 40:
		return "低"
	case weeklyHours > 0:
		return "中"
	}
	return "中"
}

// goalSpec 兼容 goals_short 的两种形状:
//   - Schema v1.0:["攒下第一桶金", …]
//   - Schema v1.1:[{horizon: 开局, text: 型号合格审定TC受理}, …]
type goalSpec struct {
	Horizon string
	Text    string
}

// UnmarshalYAML 实现 yaml.Unmarshaler(形状容错;永不报错)。
func (g *goalSpec) UnmarshalYAML(value *yaml.Node) error {
	if value == nil {
		return nil
	}
	switch value.Kind {
	case yaml.ScalarNode:
		g.Text = strings.TrimSpace(value.Value)
	case yaml.MappingNode:
		var m map[string]yaml.Node
		if err := value.Decode(&m); err != nil {
			return nil
		}
		if n, ok := m["horizon"]; ok {
			g.Horizon = strings.TrimSpace(textOfNode(&n))
		}
		if n, ok := m["text"]; ok {
			g.Text = strings.TrimSpace(textOfNode(&n))
		}
		if g.Text == "" {
			g.Text = mapNodeText(value)
		}
	case yaml.SequenceNode:
		var items []flexText
		if err := value.Decode(&items); err == nil {
			parts := make([]string, 0, len(items))
			for _, it := range items {
				if it.Text != "" {
					parts = append(parts, it.Text)
				}
			}
			g.Text = strings.Join(parts, "、")
		}
	}
	if g.Text == "null" || g.Text == "~" {
		g.Text = ""
	}
	return nil
}

// String 渲染成 prompt 用的一行目标(有 horizon 时前缀「horizon：text」)。
func (g goalSpec) String() string {
	if g.Horizon == "" || strings.Contains(g.Text, g.Horizon) {
		return g.Text
	}
	return g.Horizon + "：" + g.Text
}

// ─────────────────── docCard(容错子集) ───────────────────

// docCard 是文档池 frontmatter 的可空子集(Schema v1.0/v1.1 双兼容;缺失字段
// 走 mapCard 兜底)。**所有字段都用上面的容错类型**,任何单字段形状漂移都不会
// 让整卡解析失败(2026-09-16 §文档池解析修复)。
type docCard struct {
	ID              string            `yaml:"id"`
	Name            flexText          `yaml:"name"`        // Schema v1.1 真实字段名
	LegacyName      flexText          `yaml:"legacy_name"` // 旧字段名,保留兼容
	Occupation      flexText          `yaml:"occupation"`
	IndustryL1      flexText          `yaml:"industry_l1"`
	IndustryL3      flexText          `yaml:"industry_l3"`
	IncomeMonthly   flexInt           `yaml:"income_monthly"`
	IncomeRange     []flexInt         `yaml:"income_range"`
	IncomeStability flexText          `yaml:"income_stability"`
	MonthlyExpense  flexInt           `yaml:"monthly_expense"`
	SavingsStock    flexInt           `yaml:"savings_stock"`
	Age             flexInt           `yaml:"age"`
	WorkIntensity   workIntensitySpec `yaml:"work_intensity"`
	HealthGrade     flexText          `yaml:"health_grade"`
	Personality     flexTextList      `yaml:"personality"`
	BehaviorTraits  flexTextList      `yaml:"behavior_traits"`
	RiskPreference  flexText          `yaml:"risk_preference"`
	Marital         flexText          `yaml:"marital"`
	ChildrenCount   flexInt           `yaml:"children_count"`
	EldersDependent flexInt           `yaml:"elders_dependent"`
	OpeningHook     flexText          `yaml:"opening_hook"`
	GoalsShort      []goalSpec        `yaml:"goals_short"`
	HousingCity     flexText          `yaml:"housing_city"`
	Employment      flexText          `yaml:"employment"`      // Schema v1.1 真实字段名
	EmploymentType  flexText          `yaml:"employment_type"` // 旧字段名,保留兼容
	HousingTenure   flexText          `yaml:"housing_tenure"`
}

// employmentText 就业形态(新字段名优先,回落旧字段名)。
func (d docCard) employmentText() string {
	if s := strings.TrimSpace(d.Employment.Text); s != "" {
		return s
	}
	return strings.TrimSpace(d.EmploymentType.Text)
}

// nameText 化名(name 优先,回落 legacy_name)。
func (d docCard) nameText() string {
	if s := strings.TrimSpace(d.Name.Text); s != "" {
		return s
	}
	return strings.TrimSpace(d.LegacyName.Text)
}

// salaryFallback 月薪兜底链:income_monthly → income_range 均值 → 0(不可用)。
// income_range 形如 [15000, 24000] → 取算术均值(四舍五入到百元),用于
// income_monthly 缺失但区间可用的卡(抽样 ≈0.3%)。
func (d docCard) salaryFallback() int64 {
	if d.IncomeMonthly.Valid && d.IncomeMonthly.Value > 0 {
		return d.IncomeMonthly.Value
	}
	vals := make([]int64, 0, 2)
	for _, r := range d.IncomeRange {
		if r.Valid && r.Value > 0 {
			vals = append(vals, r.Value)
		}
	}
	if len(vals) == 0 {
		return 0
	}
	var sum int64
	for _, v := range vals {
		sum += v
	}
	mean := sum / int64(len(vals))
	return (mean / 100) * 100 // 归整到百元,避免伪精度
}

// extractFrontmatter 取首个 "---" 围起的 YAML 块(无则 nil)。
// 实测知识库卡 frontmatter 结束行 P100 = 184 行,SplitN 上限 4096 足够;
// CRLF 行尾由 TrimSpace 兜住。
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

// parseDocCard 解析 frontmatter YAML → docCard(形状容错;仅 YAML 语法错误
// 才返回 error)。
func parseDocCard(fm []byte) (docCard, error) {
	var raw docCard
	if err := yaml.Unmarshal(fm, &raw); err != nil {
		return docCard{}, err
	}
	return raw, nil
}

// ─────────────────── 城区映射(行业 → 城区) ───────────────────

// industryDistrictMap 一级行业代码 → 起始城区(2026-09-16 §文档池解析修复)。
//
// 动机: 真实卡 housing_city 是具体城市名(抽样 146 个:上海/北京/成都/杭州…),
// 旧 cityToDistrict 的关键词(一线/高新/县城/新区…)一条都命不中 → 75k 文档池
// 100% 落在 residential,12 座同开局挤在同一区,房价指数/搬迁/商铺机制全部退化。
// 改为按「职业所在产业」定城区(语义更贴近工作地),分布见下表注释。
var industryDistrictMap = map[string]string{
	"A": "suburb",      // 农林牧渔 → 郊区
	"B": "industry",    // 采矿与冶金
	"C": "industry",    // 食品饮料与烟草
	"D": "industry",    // 纺织服装与鞋帽
	"E": "industry",    // 木材家具与造纸印刷
	"F": "tech",        // 医药与生物制造
	"G": "industry",    // 化工与新材料
	"H": "industry",    // 金属制品与通用机械
	"I": "tech",        // 电子半导体与仪器仪表
	"J": "industry",    // 汽车与交通装备
	"K": "industry",    // 能源与电力
	"L": "commerce",    // 建筑与房地产
	"M": "commerce",    // 批发零售与商贸流通
	"N": "suburb",      // 交通运输与物流仓储
	"O": "oldtown",     // 住宿与餐饮
	"P": "tech",        // 信息与通信技术
	"Q": "finance",     // 金融与保险
	"R": "finance",     // 专业服务
	"S": "tech",        // 科学研究与技术服务
	"T": "residential", // 教育与培训
	"U": "residential", // 医疗健康与社会照护
	"V": "riverside",   // 文化传媒体育与娱乐
	"W": "oldtown",     // 公共管理与国防
	"X": "residential", // 社会组织与公益慈善
	"Y": "oldtown",     // 居民生活服务
	"Z": "riverside",   // 新兴交叉职业与其他
}

// cityTierDistrict 真实城市名 → 城区(行业缺失时的次级信号)。
// 一线 → finance;新一线/强产业城市 → tech;其余省会/计划单列 → commerce。
var cityTierDistrict = []struct {
	Cities   []string
	District string
}{
	{[]string{"北京", "上海", "深圳", "广州"}, "finance"},
	{[]string{"杭州", "成都", "武汉", "西安", "南京", "苏州", "合肥", "长沙", "重庆", "天津",
		"郑州", "东莞", "宁波", "青岛", "无锡", "佛山", "珠海"}, "tech"},
	{[]string{"厦门", "福州", "济南", "沈阳", "大连", "哈尔滨", "长春", "南昌", "贵阳", "南宁",
		"太原", "常州", "温州", "中山", "惠州", "石家庄", "昆明", "兰州", "海口", "徐州",
		"烟台", "绍兴", "扬州", "盐城", "潍坊", "唐山", "保定", "洛阳", "临沂"}, "commerce"},
}

// spreadDistricts 末级兜底:行业/城市都无信号(如 housing_city=未知)时,按卡 id
// 的 FNV-1a 哈希在 5 个非核心城区里稳定散列 —— 确定性(同卡同区,不吃 rng)、
// 且避免全部落 residential。
var spreadDistricts = []string{"oldtown", "residential", "suburb", "riverside", "industry"}

// resolveDistrict 城区解析优先级(2026-09-16 §文档池解析修复):
//  1. industry_l1 → industryDistrictMap(真实卡 100% 命中);
//  2. housing_city 关键词表 cityToDistrict(兼容合成 fixture:一线城市/高新区/县城);
//  3. 真实城市名分层 cityTierDistrict;
//  4. 卡 id 哈希散列 spreadDistricts(确定性兜底)。
func resolveDistrict(industryL1, city, cardID string) string {
	code := strings.ToUpper(strings.TrimSpace(industryL1))
	if len(code) > 1 {
		code = code[:1]
	}
	if d, ok := industryDistrictMap[code]; ok {
		return d
	}
	for _, m := range cityToDistrict {
		if strings.Contains(city, m.Keyword) {
			return m.District
		}
	}
	for _, tier := range cityTierDistrict {
		for _, c := range tier.Cities {
			if strings.Contains(city, c) {
				return tier.District
			}
		}
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(cardID))
	return spreadDistricts[int(h.Sum32())%len(spreadDistricts)]
}

// cityToDistrict 城市名关键词 → 8 区启发映射(加载器文档 §4;合成 fixture 与
// 少量描述性城市名用,如「一线城市」「高新区」「县城」)。
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

// ─────────────────── mapCard(frontmatter → Card) ───────────────────

// mapCard 按加载器文档 §4 映射表把 frontmatter 转成 Card。
// 只有「完全没有收入档案」(income_monthly 与 income_range 双缺)才算硬失败 ——
// 无收入的卡进池会让座位月薪 0、开局即破产,属数据缺陷而非形状问题;抽样占比
// ≈0.33%(20/6000),Draw 的补抽机制会自动跳过(见 loader.go Draw 注释)。
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
	c.Name = raw.nameText()
	if c.Name == "" {
		if i := strings.LastIndexByte(strings.TrimSuffix(filepath.Base(rel), ".md"), '-'); i >= 0 {
			c.Name = strings.TrimSuffix(filepath.Base(rel), ".md")[i+1:]
		}
	}
	c.Title = strings.TrimSpace(raw.Occupation.Text)
	if c.Title == "" {
		c.Title = strings.TrimSpace(raw.IndustryL3.Text)
	}
	if c.Title == "" {
		return Card{}, fmt.Errorf("missing occupation/industry_l3")
	}
	salary := raw.salaryFallback()
	if salary <= 0 {
		return Card{}, fmt.Errorf("missing income_monthly/income_range")
	}
	c.Salary = salary
	c.SalaryVolatile = strings.Contains(raw.IncomeStability.Text, "波动") ||
		strings.Contains(raw.IncomeStability.Text, "不稳定")
	if raw.MonthlyExpense.Valid && raw.MonthlyExpense.Value > 0 {
		c.Expense = raw.MonthlyExpense.Value
	} else {
		c.Expense = c.Salary * 60 / 100 // 兜底 60% 消费率
	}
	// savings_stock 可空 → 兜底 = income_monthly × 6(P0 新定)。
	if raw.SavingsStock.Valid {
		if raw.SavingsStock.Value > 0 {
			c.Savings = raw.SavingsStock.Value
		}
	} else {
		c.Savings = c.Salary * 6
	}
	c.StartAge = 25
	if raw.Age.Valid && raw.Age.Value > 0 {
		c.StartAge = int(raw.Age.Value)
	}
	if c.StartAge < 20 {
		c.StartAge = 20
	}
	if c.StartAge > 55 {
		c.StartAge = 55
	}
	// Energy 由归一化后的劳动强度档位映射:高→4、中→6、低→8(P0 新定;
	// 档位推导见 deriveIntensityLevel)。
	switch raw.WorkIntensity.Level {
	case "高":
		c.Energy = 4
	case "低":
		c.Energy = 8
	default:
		c.Energy = 6
	}
	c.Network = 5
	c.Cognition = 5
	c.HealthGrade = strings.TrimSpace(raw.HealthGrade.Text)
	switch c.HealthGrade {
	case "A", "B", "C":
	default:
		c.HealthGrade = "B"
	}
	// 人格/行为:先过词库;词库全miss(真实卡含「情绪稳定/细腻敏感/月光族」等
	// 词库外标签)→ 保留原始前 2 词,避免 Agent 人设全空(prompt 第 2 段失血)。
	c.Personality = filterWithFallback(raw.Personality.Items, personalityVocab, 4, 2)
	c.BehaviorTraits = filterWithFallback(raw.BehaviorTraits.Items, behaviorVocab, 4, 2)
	switch strings.TrimSpace(raw.RiskPreference.Text) {
	case "conservative", "balanced", "aggressive":
		c.RiskPreference = strings.TrimSpace(raw.RiskPreference.Text)
	default:
		c.RiskPreference = "balanced"
	}
	if strings.Contains(raw.Marital.Text, "已婚") || strings.TrimSpace(raw.Marital.Text) == "married" {
		c.Marital = "married"
	} else {
		c.Marital = "single"
	}
	if raw.ChildrenCount.Valid && raw.ChildrenCount.Value > 0 {
		c.ChildrenCount = int(raw.ChildrenCount.Value)
	}
	if raw.EldersDependent.Valid && raw.EldersDependent.Value > 0 {
		c.EldersDependent = int(raw.EldersDependent.Value)
	}
	// opening_hook:骨架 30–50 字直接使用;>60 rune 截断 + 「…」。
	c.OpeningHook = strings.TrimSpace(raw.OpeningHook.Text)
	if runes := []rune(c.OpeningHook); len(runes) > 60 {
		c.OpeningHook = string(runes[:59]) + "…"
	}
	if len([]rune(c.OpeningHook)) < 20 {
		c.OpeningHook = fmt.Sprintf("我是%s，今年 %d 岁，正在为想要的生活努力攒第一桶金。", c.Title, c.StartAge)
	}
	// goals_short 取前 3 条(结构化形状渲染成「horizon：text」)。
	for i, g := range raw.GoalsShort {
		if i >= 3 {
			break
		}
		if s := strings.TrimSpace(g.String()); s != "" {
			c.Goals = append(c.Goals, s)
		}
	}
	if len(c.Goals) == 0 {
		c.Goals = []string{"5 年内把储蓄翻一番。"}
	}
	// 城区:行业 → 城市关键词 → 城市分层 → 卡 id 哈希散列(见 resolveDistrict)。
	c.HomeDistrict = resolveDistrict(raw.IndustryL1.Text, raw.HousingCity.Text, c.ID)
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
	emp := raw.employmentText()
	if strings.Contains(emp, "个体") || strings.Contains(emp, "自由") ||
		strings.Contains(emp, "灵活") || strings.Contains(emp, "平台") {
		c.CreditScore -= 50
		if c.CreditScore < 500 {
			c.CreditScore = 500
		}
	}
	return c, nil
}

// filterWithFallback 词库过滤 + 兜底:命中词库的按序保留(≤keep);
// 若一条都没命中,保留原始前 fallbackKeep 个非空词(去重),保证人设不为空。
func filterWithFallback(words []string, vocab map[string]struct{}, keep, fallbackKeep int) []string {
	out := FilterByVocab(words, vocab, keep)
	if len(out) > 0 {
		return out
	}
	seen := map[string]struct{}{}
	fallback := make([]string, 0, fallbackKeep)
	for _, w := range words {
		w = strings.TrimSpace(w)
		if w == "" {
			continue
		}
		if _, dup := seen[w]; dup {
			continue
		}
		seen[w] = struct{}{}
		fallback = append(fallback, w)
		if len(fallback) >= fallbackKeep {
			break
		}
	}
	return fallback
}
