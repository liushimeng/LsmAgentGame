// Package wealth — survey.go: 社会调研(对全体 Agent 的预测模拟)
// (2026-09-16 §财商流P1-2)。
//
// 契约: lag_docs/虚拟城市/已实现/05-P1扩展/虚拟城市-P1-社会调研系统-v1.md §2/§4。
// 纯引擎层:调研生命周期全部在 World.Surveys(无锁状态);广播由房间层钩子
// (BroadcastHooks.OnSurvey)在锁外完成(§92a 锁纪律)。
package wealth

import (
	"fmt"
	"sort"
	"strings"
)

// 调研状态常量。
const (
	SurveyOpen   = "open"
	SurveyClosed = "closed"
)

// 调研限流常量(§3.2)。
const (
	surveyMaxPerRoom       = 20 // 每房累计上限
	surveyQuestionMaxRunes = 100
	surveyOptionMaxRunes   = 40
	surveyReasonMaxRunes   = 50
	surveyTopReasonRunes   = 30
)

// Survey 单次调研(引擎纯状态,挂 World.Surveys;JSON 由 view 层 SurveyJSON 映射)。
type Survey struct {
	ID            string               // "SV1"…(World.SurveySeq 自增)
	Question      string               // 问题文本(≤100 rune,发起时截断)
	Options       []string             // 2-6 个选项(每个 ≤40 rune)
	LaunchMonth   int                  // 发起月(w.Month)
	DeadlineMonth int                  // LaunchMonth + 2
	Status        string               // "open" | "closed"
	Answers       map[int]*SurveyAnswer // seat → 回答(1 seat 1 答)
	Result        *SurveyResult        // closed 时聚合填充
}

// SurveyAnswer 单座位回答。
type SurveyAnswer struct {
	Seat      int    // 回答者座位
	OptionIdx int    // 选项下标 0-based
	Reason    string // ≤50 rune(写入时截断)
	ModelKey  string // 回答者模型 key(按模型切片分析用)
}

// SurveyResult 聚合结果。
type SurveyResult struct {
	Counts     []int     // len == len(Options)
	Percents   []float64 // Counts/Total(和≈1)
	Total      int       // 回答总数
	TopReasons []string  // 去重后前 3 条理由摘录(每条 ≤30 rune)
}

// OpenSurvey 返回当前唯一 open 调研(限流保证至多 1 个;无则 nil)。
func (w *World) OpenSurvey() *Survey {
	if w == nil {
		return nil
	}
	for _, sv := range w.Surveys {
		if sv.Status == SurveyOpen {
			return sv
		}
	}
	return nil
}

// findSurveyByID 按 id 查找调研(锁内)。
func (w *World) findSurveyByID(id string) *Survey {
	for _, sv := range w.Surveys {
		if sv.ID == id {
			return sv
		}
	}
	return nil
}

// CloseAndAggregate 关闭调研并聚合结果(§4.3;调用方持房间锁)。
// 关闭动作:Status="closed" → 填 Result → emitEvent("survey", -1, 结果摘要)。
// 幂等:已 closed 的调研直接返回。
func (w *World) CloseAndAggregate(sv *Survey) {
	if sv == nil || sv.Status == SurveyClosed {
		return
	}
	sv.Status = SurveyClosed
	res := &SurveyResult{
		Counts:   make([]int, len(sv.Options)),
		Percents: make([]float64, len(sv.Options)),
		Total:    len(sv.Answers),
	}
	// Answers 是 map(迭代序不确定)→ 按座位号升序遍历,聚合结果确定。
	seats := make([]int, 0, len(sv.Answers))
	for seat := range sv.Answers {
		seats = append(seats, seat)
	}
	sort.Ints(seats)
	var reasons []string
	seen := map[string]bool{}
	for _, seat := range seats {
		a := sv.Answers[seat]
		if a == nil {
			continue
		}
		if a.OptionIdx >= 0 && a.OptionIdx < len(res.Counts) {
			res.Counts[a.OptionIdx]++
		}
		if a.Reason != "" && !seen[a.Reason] {
			seen[a.Reason] = true // 精确匹配去重
			reasons = append(reasons, a.Reason)
		}
	}
	for i := range res.Counts {
		res.Percents[i] = float64(res.Counts[i]) / float64(max(1, res.Total))
	}
	for _, r := range reasons {
		if len(res.TopReasons) >= 3 {
			break
		}
		res.TopReasons = append(res.TopReasons, truncateRunes(r, surveyTopReasonRunes))
	}
	sv.Result = res

	// 事件摘要:前两个选项占比(完整结果经 SurveyJSON / game.survey_result 下发)。
	var sb strings.Builder
	fmt.Fprintf(&sb, "调研『%s』结果:", sv.Question)
	for i := 0; i < len(sv.Options) && i < 2; i++ {
		if i > 0 {
			sb.WriteString("/")
		}
		fmt.Fprintf(&sb, "选项%c %.0f%%", rune('A'+i), res.Percents[i]*100)
	}
	fmt.Fprintf(&sb, "(共 %d 人)", res.Total)
	w.emitEvent("survey", -1, sb.String())
}

// CloseSurveyIfDue 关闭到期调研(§4.3 入口 ①):SettleMonth ⑤ w.Month++ 后逐个
// 检查,w.Month > sv.DeadlineMonth 的 open → 关闭。返回本轮关闭的调研列表
// (房间结算路径锁外逐个调 hooks.OnSurvey)。
func (w *World) CloseSurveyIfDue() []*Survey {
	var closed []*Survey
	for _, sv := range w.Surveys {
		if sv.Status == SurveyOpen && w.Month > sv.DeadlineMonth {
			w.CloseAndAggregate(sv)
			closed = append(closed, sv)
		}
	}
	return closed
}

// truncateRunes 截前 n rune + "…"(不足则原样)。
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
