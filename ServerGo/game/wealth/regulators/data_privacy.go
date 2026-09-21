// Package regulators — data_privacy.go: 数据隐私监管(2026-09-21 §城市扩张v2.12 阶段8)。
//
// 职责(阶段8 范围裁剪:**只做 Survey 快照脱敏检查函数**,不改引擎下发路径):
//   - AggregateSurveyAnswers:调研参与者逐座位回答 → **仅聚合**结果
//     (选项计数;座位号/自由文本理由全部丢弃)—— 供调研快照构造方调用;
//   - SnapshotPrivacyPass:公开快照脱敏检查 —— 逐座位答案条数与未脱敏
//     自由文本条数必须为 0,否则判定泄漏(观战者不应看到个人答案)。
//
// 现状对齐:wealth::view.go SurveyJSONFrom 本就只下发 AnswersCount + 聚合
// Result,从不下发 Answers 明细 —— 本文件把该不变量固化为可测试的检查函数,
// 供 v2.13 接入调研快照构造路径(§130:检查函数先行,接线下阶段)。
//
// 解耦约束:本包不 import wealth 主包;输入结构与 wealth.SurveyAnswer
// 字段对齐(SurveyAnswerInput)。
package regulators

// SurveyAnswerInput 单座位调研回答(脱敏前;字段与 wealth.SurveyAnswer 对齐)。
type SurveyAnswerInput struct {
	Seat      int
	OptionIdx int
	Reason    string
}

// SurveyAggregate 聚合结果(脱敏后唯一可公开形态;不含任何个人标识)。
type SurveyAggregate struct {
	Counts []int // 各选项计数(len = optionCount)
	Total  int   // 回答总数
}

// SurveySnapshotInput 公开快照脱敏检查输入:除聚合字段外,两个计数字段
// 统计快照中"不应存在"的个人信息条数(正常应为 0)。
type SurveySnapshotInput struct {
	OptionCount        int // 选项总数
	Counts             []int
	Total              int
	SeatAnswersExposed int // 快照中暴露的逐座位回答条数(应 = 0)
	RawReasonsExposed  int // 快照中暴露的未聚合自由文本条数(应 = 0)
}

// PrivacyRegulator 数据隐私监管状态(挂 wealth.RegulatorBundle.Privacy)。
type PrivacyRegulator struct {
	AuditsRun     int // 累计月度审计次数(月度例行)
	LastAuditMonth int // 最近一次审计月份(§130 接线验证)
}

// NewPrivacyRegulator 构造。
func NewPrivacyRegulator() *PrivacyRegulator { return &PrivacyRegulator{} }

// MonthlyAudit 月度例行审计计数(阶段8:纯计数占位;检查函数见下)。
func (p *PrivacyRegulator) MonthlyAudit(month int) {
	if p == nil {
		return
	}
	p.AuditsRun++
	p.LastAuditMonth = month
}

// AggregateSurveyAnswers 逐座位回答 → 聚合(脱敏):丢弃座位号与理由文本,
// 仅保留选项计数。optionCount ≤ 0 时按答案中最大 OptionIdx+1 推导(无答案 → 空计数)。
func AggregateSurveyAnswers(answers []SurveyAnswerInput, optionCount int) SurveyAggregate {
	maxIdx := optionCount - 1
	for _, a := range answers {
		if a.OptionIdx > maxIdx {
			maxIdx = a.OptionIdx
		}
	}
	if maxIdx < 0 {
		return SurveyAggregate{Counts: []int{}, Total: len(answers)}
	}
	agg := SurveyAggregate{Counts: make([]int, maxIdx+1), Total: len(answers)}
	for _, a := range answers {
		if a.OptionIdx >= 0 && a.OptionIdx < len(agg.Counts) {
			agg.Counts[a.OptionIdx]++
		}
	}
	return agg
}

// SnapshotPrivacyPass 公开快照脱敏检查:逐座位答案 / 未脱敏自由文本计数为 0
// 即通过(true = 无泄漏)。Counts 长度与 OptionCount 一致性一并校验
// (optionCount > 0 时 len(Counts) 必须相等,防聚合口径漂移)。
func SnapshotPrivacyPass(s SurveySnapshotInput) bool {
	if s.SeatAnswersExposed != 0 || s.RawReasonsExposed != 0 {
		return false
	}
	if s.OptionCount > 0 && len(s.Counts) != s.OptionCount {
		return false
	}
	return true
}
