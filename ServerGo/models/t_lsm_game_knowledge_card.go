// Package models — 知识库人物卡特征表。
//
// 2026-10-05 §LsmKLBaseServer 知识库引擎重构：LLM 抽取的特征字符串数组
// 持久化到 MySQL，供 MCP 服务与 Graph+LangChain 检索层消费。
//
// 与 profession.Loader 的关系：
//   - profession.Loader 是运行时文档池加载器（Markdown → frontmatter → Card）；
//   - 本表是 LLM 特征抽取的持久化存储（Card → 特征字符串数组 → MySQL）；
//   - 抽取失败时回退到 profession.Loader 的 frontmatter 解析（零回归）。
//
// Per CLAUDE.md §3, models in this directory use the t_lsm_game_*.go prefix.
package models

import "time"

// TLsmGameKnowledgeCard 是知识库人物卡的 LLM 特征抽取结果。
//
// Features 存储 LLM 抽取的特征字符串数组（JSON），如：
// ["职业:外卖骑手", "行业:交通运输", "收入:5000", ...]
// FeatureText 是特征字符串的拼接全文，用于全文索引。
// ExtractStatus: 0=未抽取, 1=已抽取, 2=抽取失败。
type TLsmGameKnowledgeCard struct {
	ID              string    `gorm:"type:varchar(64);primaryKey"                     json:"id"               comment:"卡号(N9012345)"`
	SourcePath      string    `gorm:"type:varchar(512);not null"                       json:"source_path"      comment:"来源 Markdown 相对路径"`
	Domain          string    `gorm:"type:varchar(128);index"                         json:"domain"           comment:"L1 行业域"`
	Occupation      string    `gorm:"type:varchar(256);index"                         json:"occupation"       comment:"职业名"`
	Salary          int       `gorm:"type:int;default:0"                               json:"salary"           comment:"月薪(元)"`
	Expense         int       `gorm:"type:int;default:0"                               json:"expense"          comment:"月支出(元)"`
	Savings         int       `gorm:"type:int;default:0"                               json:"savings"          comment:"初始储蓄(元)"`
	Age             int       `gorm:"type:int;default:25"                              json:"age"              comment:"年龄"`
	Gender          string    `gorm:"type:varchar(4);default:'u'"                     json:"gender"           comment:"性别(m/f/u)"`
	HealthGrade     string    `gorm:"type:varchar(4);default:'B'"                     json:"health_grade"     comment:"健康档(A/B/C)"`
	RiskPreference  string    `gorm:"type:varchar(32);default:'balanced'"             json:"risk_preference"  comment:"风险偏好"`
	Employment      string    `gorm:"type:varchar(128)"                               json:"employment"       comment:"就业形态"`
	HomeDistrict    string    `gorm:"type:varchar(64);index"                         json:"home_district"    comment:"城区 id"`
	Personality     string    `gorm:"type:varchar(512)"                               json:"personality"      comment:"人格标签(、连接)"`
	BehaviorTraits  string    `gorm:"type:varchar(512)"                               json:"behavior_traits"  comment:"行为特征(、连接)"`
	Marital         string    `gorm:"type:varchar(32);default:'single'"               json:"marital"          comment:"婚姻状态"`
	ChildrenCount   int       `gorm:"type:int;default:0"                               json:"children_count"   comment:"子女数"`
	EldersDependent int       `gorm:"type:int;default:0"                               json:"elders_dependent" comment:"赡养老人数"`
	OpeningHook     string    `gorm:"type:text"                                       json:"opening_hook"     comment:"开场白"`
	Goals           string    `gorm:"type:text"                                       json:"goals"            comment:"目标(JSON 数组)"`
	Features        string    `gorm:"type:json;not null"                              json:"features"         comment:"LLM 抽取的特征字符串数组"`
	FeatureText     string    `gorm:"type:text;index:idx_feature_text,class:FULLTEXT" json:"feature_text"     comment:"特征全文(全文索引用)"`
	ExtractStatus   int       `gorm:"type:tinyint;default:0;index"                    json:"extract_status"   comment:"抽取状态(0=未抽取,1=已抽取,2=抽取失败)"`
	CreatedAt       time.Time `gorm:"autoCreateTime"                                  json:"created_at"`
	UpdatedAt       time.Time `gorm:"autoUpdateTime"                                  json:"updated_at"`
}

// TableName pins the SQL table name.
func (TLsmGameKnowledgeCard) TableName() string { return "t_lsm_game_knowledge_card" }
