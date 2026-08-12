// Package agent — judge_summary.go: 法官「整局总结」端到端实现。
//
// 2026-07-10 §125 增强。
//
// 把所有 summary 相关逻辑集中在一个文件,避免分散到多个文件被 rebase revert:
//   - 类型: SummaryInput / SummarySections / SummarySectionsJSON
//   - Prompt 拼装 + 5 段解析 + Fallback + Flatten
//   - JudgeSummaryBridge 全局接口 + EmitGameOverSummary 投递事件
//   - recordSummaryInternal / handleGameOverSummaryInternal
//   - LastGameMemoryBlock:BuildSystemPrompt 注入「上一局记忆」段
package wwjudge

import (
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// SummarySections 法官总结的 6 个分段(§20260810-11 H1 扩字段)。
// 第 6 段「Highlight 高光时刻」是 §20260810-11 H1 新增,详见本批次设计文档。
// 旧 LLM 输出仅 5 段时,Highlight=""(向后兼容)。
type SummarySections struct {
	Outcome       string `json:"outcome"`
	TurningPoint  string `json:"turning_point"`
	RoleTimeline  string `json:"role_timeline"`
	MVP           string `json:"mvp"`
	WolfDeception string `json:"wolf_deception"`
	Highlight     string `json:"highlight,omitempty"` // §20260810-11 H1:JSON 数组字符串,如 [{"seat":1,"moment":"...","quote":"..."}]
}

func (s SummarySections) IsEmpty() bool {
	return s.Outcome == "" && s.TurningPoint == "" && s.RoleTimeline == "" &&
		s.MVP == "" && s.WolfDeception == ""
}

func (s SummarySections) AllSectionsFilled() bool {
	// §20260810-11 H1:Highlight 是可选的(向后兼容,旧 LLM 不输出第 6 段)。
	// AllSectionsFilled 仅校验前 5 段必填;Highlight 由 HasHighlight() 单独校验。
	return s.Outcome != "" && s.TurningPoint != "" && s.RoleTimeline != "" &&
		s.MVP != "" && s.WolfDeception != ""
}

// HasHighlight §20260810-11 H1 — 第 6 段高光时刻是否填写(非空且非 "[]")。
func (s SummarySections) HasHighlight() bool {
	h := strings.TrimSpace(s.Highlight)
	if h == "" {
		return false
	}
	// "[]" 也算空(无高光)
	if h == "[]" {
		return false
	}
	return true
}

type SummaryInput struct {
	RoomID     string
	DayNumber  int
	Winner     string
	WinnerSeat int
	AliveSeats []int
	DeadSeats  []int
	Roles      map[int]string
	Deaths     []DeathEvent
	Speeches   []string
	ChatTail   []string
}

type DeathEvent struct {
	Seat    int
	Cause   string
	Verdict string
	Day     int
	Round   string
}

type SummarySectionsJSON struct {
	Outcome       string `json:"outcome"`
	TurningPoint  string `json:"turning_point"`
	RoleTimeline  string `json:"role_timeline"`
	MVP           string `json:"mvp"`
	WolfDeception string `json:"wolf_deception"`
	Highlight     string `json:"highlight,omitempty"` // §20260810-11 H1
	GeneratedAt   int64  `json:"generated_at"`
	Model         string `json:"model,omitempty"`
}

func (s SummarySections) ToJSON(model string) SummarySectionsJSON {
	return SummarySectionsJSON{
		Outcome:       s.Outcome,
		TurningPoint:  s.TurningPoint,
		RoleTimeline:  s.RoleTimeline,
		MVP:           s.MVP,
		WolfDeception: s.WolfDeception,
		Highlight:     s.Highlight,
		GeneratedAt:   time.Now().UnixMilli(),
		Model:         model,
	}
}

func BuildSummaryPrompt(in SummaryInput) string {
	if in.DayNumber < 1 {
		in.DayNumber = 1
	}
	if in.Roles == nil {
		in.Roles = map[int]string{}
	}

	var sb strings.Builder
	sb.WriteString("你是狼人杀 13 人局的法官/主持人。\n")
	sb.WriteString("【任务】基于下方客观事实,严格按 6 段输出中文总结,前 5 段每段 ≤ 80 字;第 6 段是 JSON 数组(3 个戏剧化瞬间)。\n")
	sb.WriteString("【硬约束】\n")
	sb.WriteString("1. 必须输出 6 个段标题,顺序不可调换:【阵营胜负】/【关键翻盘点】/【角色操作时间线】/【MVP 玩家】/【狼人悍跳记录】/【高光时刻】\n")
	sb.WriteString("2. 每段开头必须用对应标题\n")
	sb.WriteString("3. 不输出段标题外的任何额外字符\n")
	sb.WriteString("4. 不杜撰客观事实未给出的事件;无证据用 \"无明显翻盘点\"\n")
	sb.WriteString("5. 不要重复引用聊天原文;使用自己的概括\n")
	sb.WriteString("6. 第 6 段「【高光时刻】」**必须**是合法 JSON 数组,3 个对象,按戏剧性排序,无高光填 []。\n")
	sb.WriteString("   格式: [{\"seat\":1,\"moment\":\"被全场票出时亮明女巫身份\",\"quote\":\"我还有一瓶毒\"},...]\n\n")
	sb.WriteString(fmt.Sprintf("【房间】%s · 第 %d 夜结束\n", in.RoomID, in.DayNumber))
	sb.WriteString(fmt.Sprintf("【胜方】%s\n", winnerChinese(in.Winner)))

	sb.WriteString("【存活座位】")
	if len(in.AliveSeats) == 0 {
		sb.WriteString("无")
	} else {
		parts := make([]string, 0, len(in.AliveSeats))
		for _, s := range in.AliveSeats {
			parts = append(parts, fmt.Sprintf("%d号(%s)", s+1, roleChinese(in.Roles[s])))
		}
		sb.WriteString(strings.Join(parts, "、"))
	}
	sb.WriteString("\n")

	if len(in.Deaths) > 0 {
		sb.WriteString("【死亡时间线】")
		parts := make([]string, 0, len(in.Deaths))
		for _, d := range in.Deaths {
			parts = append(parts, fmt.Sprintf("D%d·%d号(%s·%s)",
				d.Day, d.Seat+1, roleChinese(in.Roles[d.Seat]), verdictChinese(d.Verdict)))
		}
		sb.WriteString(strings.Join(parts, " → "))
		sb.WriteString("\n")
	}

	if len(in.Speeches) > 0 {
		sb.WriteString("【关键发言摘录】\n")
		maxN := 12
		if len(in.Speeches) < maxN {
			maxN = len(in.Speeches)
		}
		for i := 0; i < maxN; i++ {
			text := truncateForPrompt(in.Speeches[i], 80)
			sb.WriteString(fmt.Sprintf("- %s\n", text))
		}
	}

	if len(in.ChatTail) > 0 {
		sb.WriteString("\n【近期聊天上下文(仅供参考)】\n")
		maxN := 8
		if len(in.ChatTail) < maxN {
			maxN = len(in.ChatTail)
		}
		start := len(in.ChatTail) - maxN
		for i := start; i < len(in.ChatTail); i++ {
			sb.WriteString(fmt.Sprintf("· %s\n", truncateForPrompt(in.ChatTail[i], 60)))
		}
	}

	sb.WriteString("\n【输出格式示例】\n")
	sb.WriteString("【阵营胜负】好人阵营以 4:3 票差获胜。\n")
	sb.WriteString("【关键翻盘点】第三天预言家查验 5 号为狼,扭转局势。\n")
	sb.WriteString("【角色操作时间线】D1 狼刀 7 号平民,D2 女巫毒 4 号,D3 狼自爆。\n")
	sb.WriteString("【MVP 玩家】9 号预言家,3 次正确查验。\n")
	sb.WriteString("【狼人悍跳记录】2 号狼冒充预言家,被 11 号真预言家对跳戳穿。\n")
	sb.WriteString("【高光时刻】[{\"seat\":1,\"moment\":\"被全场票出时亮明女巫身份\",\"quote\":\"我还有一瓶毒\"}]\n")
	return sb.String()
}

func ParseSummary(raw string) SummarySections {
	var s SummarySections
	if raw == "" {
		return s
	}

	// §20260810-11 H1 — 6 段(扩 1 段「【高光时刻】」)。LLM 旧输出仅 5 段时,
	// Highlight 留空(omitempty),前端 JSON.parse 失败静默。
	titles := []string{"【阵营胜负】", "【关键翻盘点】", "【角色操作时间线】", "【MVP 玩家】", "【狼人悍跳记录】", "【高光时刻】"}
	out := make([]string, len(titles))

	type posText struct {
		pos int
		txt string
	}
	hits := make([]posText, 0, len(titles))
	for _, t := range titles {
		idx := strings.Index(raw, t)
		if idx < 0 {
			continue
		}
		hits = append(hits, posText{pos: idx, txt: t})
	}
	if len(hits) == 0 {
		out[0] = truncateForPrompt(strings.TrimSpace(raw), 200)
		return toSections(out)
	}
	for i, h := range hits {
		contentStart := h.pos + len(h.txt)
		var contentEnd int
		if i+1 < len(hits) {
			contentEnd = hits[i+1].pos
		} else {
			contentEnd = len(raw)
		}
		segment := strings.TrimSpace(raw[contentStart:contentEnd])
		segment = truncateForPrompt(segment, 200)
		for j, t := range titles {
			if t == h.txt {
				out[j] = segment
				break
			}
		}
	}
	return toSections(out)
}

func toSections(arr []string) SummarySections {
	for len(arr) < 6 { // §20260810-11 H1:6 段
		arr = append(arr, "")
	}
	return SummarySections{
		Outcome:       strings.TrimSpace(arr[0]),
		TurningPoint:  strings.TrimSpace(arr[1]),
		RoleTimeline:  strings.TrimSpace(arr[2]),
		MVP:           strings.TrimSpace(arr[3]),
		WolfDeception: strings.TrimSpace(arr[4]),
		Highlight:     strings.TrimSpace(arr[5]), // §20260810-11 H1
	}
}

func FallbackSummary(in SummaryInput, errMsg string) string {
	if in.Winner == "" {
		return fmt.Sprintf("第 %d 夜结束(LLM 总结失败: %s)", in.DayNumber, errMsg)
	}
	mvp := "无明显 MVP"
	if in.WinnerSeat >= 0 && in.WinnerSeat < 13 {
		mvp = fmt.Sprintf("%d号(%s)", in.WinnerSeat+1, roleChinese(in.Roles[in.WinnerSeat]))
	}
	return fmt.Sprintf("第 %d 夜结束,胜方:%s;MVP:%s",
		in.DayNumber, winnerChinese(in.Winner), mvp)
}

func FlattenSummary(s SummarySections) string {
	var sb strings.Builder
	parts := []struct {
		title string
		body  string
	}{
		{"【阵营胜负】", s.Outcome},
		{"【关键翻盘点】", s.TurningPoint},
		{"【角色操作时间线】", s.RoleTimeline},
		{"【MVP 玩家】", s.MVP},
		{"【狼人悍跳记录】", s.WolfDeception},
	}
	for i, p := range parts {
		if i > 0 {
			sb.WriteString(" | ")
		}
		sb.WriteString(p.title)
		sb.WriteString(p.body)
	}
	out := sb.String()
	if utf8.RuneCountInString(out) > 1024 {
		runes := []rune(out)
		out = string(runes[:1024]) + "…(truncated)"
	}
	return out
}

func winnerChinese(w string) string {
	switch w {
	case "good":
		return "好人阵营"
	case "wolf":
		return "狼人阵营"
	case "tie":
		return "平局"
	default:
		if w == "" {
			return "未知"
		}
		return w
	}
}

func roleChinese(r string) string {
	switch r {
	case "werewolf":
		return "狼"
	case "seer":
		return "预言家"
	case "witch":
		return "女巫"
	case "hunter":
		return "猎人"
	case "idiot":
		return "白痴"
	case "villager":
		return "平民"
	default:
		if r == "" {
			return "?"
		}
		return r
	}
}

func verdictChinese(v string) string {
	switch v {
	case "death":
		return "死亡"
	case "execution":
		return "处决"
	default:
		if v == "" {
			return "?"
		}
		return v
	}
}

func truncateForPrompt(s string, max int) string {
	if max <= 0 || s == "" {
		return s
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max]) + "…"
}

// JudgeSummaryBridge 房间侧的总结回调接口(2026-07-10 §125 增强)。
type JudgeSummaryBridge interface {
	GenerateSummary(in SummaryInput) (SummarySections, string)
	PersistSummary(modelKey string, sections SummarySections)
}

var (
	summaryBridgeMu sync.RWMutex
	summaryBridge   JudgeSummaryBridge
)

func SetSummaryBridge(b JudgeSummaryBridge) {
	summaryBridgeMu.Lock()
	defer summaryBridgeMu.Unlock()
	summaryBridge = b
}

func getSummaryBridge() JudgeSummaryBridge {
	summaryBridgeMu.RLock()
	defer summaryBridgeMu.RUnlock()
	return summaryBridge
}

// handleGameOverSummaryInternal 2026-07-10 §125 增强 — 整局总结路径(judge goroutine 内部调用)。
func (j *AgentJudge) handleGameOverSummaryInternal(evt JudgeEvent) {
	in, _ := evt.Extra["summary_input"].(SummaryInput)
	modelKey, _ := evt.Extra["model_key"].(string)
	if modelKey == "" {
		modelKey = j.ModelKey
	}
	bridge := getSummaryBridge()
	if bridge == nil {
		fallbackText := FallbackSummary(in, "no bridge")
		j.recordSummaryInternal(fallbackText, SummarySections{Outcome: fallbackText}, "fallback_no_bridge")
		return
	}
	sections, errMsg := bridge.GenerateSummary(in)
	if sections.IsEmpty() || !sections.AllSectionsFilled() {
		fallbackText := FallbackSummary(in, errMsg)
		if fallbackText != "" {
			j.recordSummaryInternal(fallbackText, SummarySections{Outcome: fallbackText}, "fallback_llm_failed")
		}
		bridge.PersistSummary(modelKey, SummarySections{Outcome: fallbackText})
		return
	}
	flat := FlattenSummary(sections)
	j.recordSummaryInternal(flat, sections, "llm")
	bridge.PersistSummary(modelKey, sections)
}

// recordSummaryInternal 写入最近总结摘要。
func (j *AgentJudge) recordSummaryInternal(flatText string, sections SummarySections, toolName string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.transcript.LastSummary = flatText
	j.transcript.LastSummaryAt = time.Now().UnixMilli()
	j.transcript.LastSummarySections = sections.ToJSON(j.ModelKey)
	j.transcript.RecentAnnouncements = appendRecentString(j.transcript.RecentAnnouncements, "[整局总结] "+flatText, 10)
	j.transcript.ToolCalls = appendRecentString(j.transcript.ToolCalls, toolName+": game_over_summary", 5)
	j.transcript.LastUpdatedAt = time.Now().UnixMilli()
}

// EmitGameOverSummary 触发整局总结事件(2026-07-10 §125 增强)。
func EmitGameOverSummary(j *AgentJudge, modelKey string, in SummaryInput) bool {
	if j == nil {
		return false
	}
	if in.RoomID == "" {
		in.RoomID = j.RoomID
	}
	if in.DayNumber < 1 {
		in.DayNumber = 1
	}
	evt := JudgeEvent{
		Kind: JudgePendingGameOverSummary,
		Extra: map[string]any{
			"summary_input": in,
			"model_key":     modelKey,
		},
		At: time.Now(),
	}
	select {
	case j.events <- evt:
		return true
	default:
		return false
	}
}

// LastGameMemoryBlock 2026-07-10 §125 增强 — 把上一局法官总结格式化为 system prompt 段。
func LastGameMemoryBlock(modelKey string, memories []string) string {
	if modelKey == "" || len(memories) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("\n\n【上一局记忆(%s)】\n", modelKey))
	for _, m := range memories {
		if m == "" {
			continue
		}
		sb.WriteString(fmt.Sprintf("- %s\n", m))
	}
	if sb.Len() == 0 {
		return ""
	}
	return sb.String()
}
