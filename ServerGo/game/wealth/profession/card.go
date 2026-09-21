// Package profession — card.go: 职业卡结构(P0 v1,2026-09-14 §财商流P0)。
//
// 契约: lag_docs/虚拟城市/已实现/03-Agent设计/虚拟城市-职业卡与加载器设计-v1.md §1。
// 精选 10 卡(curated.go)与文档池卡(loader.go)共用此结构。
package profession

import (
	"fmt"
	"strings"
)

// Card 是一张可发到座位的职业(人物)卡。
type Card struct {
	ID              string   // "P01"…(文档池为 "N9012345")
	Title           string   // 职业名
	Name            string   // 化名(文档池卡;精选手卡为空)
	Salary          int64    // 月薪(税前,元;P16 为波动带中值)
	SalaryVolatile  bool     // true = 收入波动带(P16)
	Expense         int64    // 月支出基数(元)
	Savings         int64    // 初始储蓄(元)
	StartAge        int      // 开局年龄(精选手卡恒 25;文档卡 clamp 20–55)
	Energy          int      // 初始精力 0–10
	Network         int      // 初始人脉 0–10
	Cognition       int      // 初始认知 0–10
	CreditScore     int      // 初始信用分 400–850
	HomeDistrict    string   // 工作区/初始所在区(8 区 id)
	RiskPreference  string   // conservative | balanced | aggressive
	Personality     []string // 2–4 词(Schema v1.1 词库)
	BehaviorTraits  []string // 2–4 词(可空)
	HealthGrade     string   // "A"|"B"|"C"
	Marital         string   // single | married(文档池)
	ChildrenCount   int      // 初始子女数
	EldersDependent int      // 需赡养老人数(月支出 +1000/位)
	OpeningHook     string   // 30–50 字开场白(开局 SendFromBot)
	Goals           []string // 含 1 条 5 年目标
	Source          string   // "curated" | "docs"
}

// Validate 校验卡面完整性(加载器文档 §1)。
// 返回首个不满足项的错误;nil = 合法。
func (c *Card) Validate() error {
	if c.Salary < 0 || c.Expense < 0 || c.Savings < 0 {
		return fmt.Errorf("card %s: salary/expense/savings must be >= 0", c.ID)
	}
	if c.Energy < 0 || c.Energy > 10 || c.Network < 0 || c.Network > 10 || c.Cognition < 0 || c.Cognition > 10 {
		return fmt.Errorf("card %s: energy/network/cognition must be in [0,10]", c.ID)
	}
	if c.CreditScore < 400 || c.CreditScore > 850 {
		return fmt.Errorf("card %s: credit_score must be in [400,850]", c.ID)
	}
	switch c.HealthGrade {
	case "A", "B", "C":
	default:
		return fmt.Errorf("card %s: health_grade must be A|B|C", c.ID)
	}
	switch c.RiskPreference {
	case "conservative", "balanced", "aggressive":
	default:
		return fmt.Errorf("card %s: risk_preference invalid: %q", c.ID, c.RiskPreference)
	}
	if !validDistrict(c.HomeDistrict) {
		return fmt.Errorf("card %s: home_district invalid: %q", c.ID, c.HomeDistrict)
	}
	// OpeningHook 长度 ∈[20,60] rune。文档池超长卡在 loader 截断,curated 恒满足。
	if n := len([]rune(c.OpeningHook)); n < 20 || n > 60 {
		return fmt.Errorf("card %s: opening_hook length %d out of [20,60]", c.ID, n)
	}
	return nil
}

// districtIDs 16 区 id 集中定义(与 game/wealth/districts.go 静态表对齐;
// profession 不得反向 import wealth,此处独立维护——两处同步由 loader_test 覆盖)。
// 2026-09-21 §城市扩张v2.12 阶段2:8 → 16(前 8 P0 区顺序冻结,追加 8 新区)。
var districtIDs = []string{
	"finance", "tech", "industry", "oldtown", "commerce", "residential", "suburb", "riverside",
	"logistics_port", "hightech_park", "edu_district", "medical_city",
	"industrial_park", "central_park", "transport_hub", "cultural_creative",
}

func validDistrict(id string) bool {
	for _, d := range districtIDs {
		if d == id {
			return true
		}
	}
	return false
}

// DistrictIDs 返回 8 区 id 副本(供 loader 映射/校验)。
func DistrictIDs() []string {
	out := make([]string, len(districtIDs))
	copy(out, districtIDs)
	return out
}

// Personality 词库(Schema v1.1 十三词子集;词库过滤用)。
var personalityVocab = map[string]struct{}{
	"务实主义": {}, "顺从协作": {}, "尽责坚韧": {}, "外向社交": {}, "开放求新": {},
	"果决有力": {}, "独立自主": {}, "谨慎保守": {}, "乐观豁达": {}, "内省深思": {},
	"冒险敢为": {}, "平和包容": {}, "进取心强": {},
}

// BehaviorTraits 词库(十词库;可空)。
var behaviorVocab = map[string]struct{}{
	"精打细算": {}, "保守储蓄": {}, "长线规划": {}, "记账习惯": {}, "积极投资": {},
	"风险管理": {}, "冲动决策": {}, "勤奋自律": {}, "社交达人": {}, "学习导向": {},
}

// FilterByVocab 词库过滤:保留命中词库的词,最多 keep 个(加载器文档 §4)。
func FilterByVocab(words []string, vocab map[string]struct{}, keep int) []string {
	out := make([]string, 0, keep)
	seen := map[string]struct{}{}
	for _, w := range words {
		w = strings.TrimSpace(w)
		if w == "" {
			continue
		}
		if _, ok := vocab[w]; !ok {
			continue
		}
		if _, dup := seen[w]; dup {
			continue
		}
		seen[w] = struct{}{}
		out = append(out, w)
		if len(out) >= keep {
			break
		}
	}
	return out
}

// PersonalityVocab / BehaviorVocab 导出词库(loader 与单测用)。
func PersonalityVocab() map[string]struct{} { return personalityVocab }
func BehaviorVocab() map[string]struct{}    { return behaviorVocab }
