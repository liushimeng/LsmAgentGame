// Package debate — 房间级 Action 接口(供 Agent driver 调用的引擎入口)。
//
// 2026-08-31 §20260831-01 — Action_* 方法集合:
//
//   - SubmitSpeech:提交正式发言(立论/驳论/小结/总结)
//   - SubmitCrossExamQuestion:提交质询提问
//   - SubmitCrossExamAnswer:提交质询回答
//   - SubmitFreeDebateSpeech:提交自由辩论发言
//   - FinishSpeak:主动交还发言权
//   - IdleSilent:沉默
//
// 所有 Action 走 debateRoom mutex,线程安全。
// 与狼人杀 Action_* 风格一致(见 werewolf/room_action.go),便于 WS handler 复用派发模式。
//
// 详细设计见 docs/辩论比赛/02-辩论比赛Agent设计.md §2.6 + 05 §2。
package debate

import (
	"fmt"
	"strings"
	"time"
)

// SpeechParams 发言参数(由 Agent driver 解析 tool_use.input 后传入)。
type SpeechParams struct {
	Content         string
	References      []string
	InternalThought string
}

// CrossExamQuestionParams 质询提问参数。
type CrossExamQuestionParams struct {
	TargetTeam int
	TargetSeat int // -1 = 任意
	Question   string
}

// CrossExamAnswerParams 质询回答参数。
type CrossExamAnswerParams struct {
	QuestionID string
	Answer     string
}

// FreeDebateParams 自由辩论发言参数。
type FreeDebateParams struct {
	Content string
}

// ActionResult 动作返回值(供 LLM tool_result 回填)。
type ActionResult struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
	// SpeechID 等附加字段(成功时填充)
	SpeechID  string `json:"speech_id,omitempty"`
	Phase     Phase  `json:"phase,omitempty"`
	NextPhase string `json:"next_phase,omitempty"` // 提示 driver 下一阶段
}

// ============================================================================
// SubmitSpeech — 正式发言
// ============================================================================

// SubmitSpeech 提交一次正式发言(立论/驳论/小结/总结)。
//
// 校验:
//   - 房间存在 + 未关闭
//   - 当前阶段 ∈ {立论/驳论/小结/总结}
//   - 辩位匹配(立论必须一辩,驳论必须二辩,小结可一辩或二辩,总结必须四辩)
//   - 字数 ≤ 当前阶段上限
//
// 副作用:
//   - 追加到 DebateRoom.speeches
//   - 广播 debate.speech 帧(由 manager 钩子完成)
//   - 通知下一发言者(若有)或推进阶段
func (r *DebateRoom) SubmitSpeech(teamID, seat int, params SpeechParams) ActionResult {
	if r.IsClosed() {
		return ActionResult{OK: false, Message: "room closed"}
	}
	phase := r.Phase()
	if !isSpeechPhase(phase) {
		return ActionResult{OK: false, Message: fmt.Sprintf("phase %s not allow speech", phase)}
	}

	// 校验辩位
	role, ok := r.lookupRole(teamID, seat)
	if !ok {
		return ActionResult{OK: false, Message: "agent not found"}
	}
	if !roleMatchesPhase(role, phase) {
		return ActionResult{OK: false, Message: fmt.Sprintf("role %s cannot speak in phase %s", role, phase)}
	}

	// 校验字数
	maxChars := MaxSpeechCharsForPhase(phase, r.Config.PhaseConfig)
	content := params.Content
	if CountRune(content) > maxChars {
		// 截断 + 标记截断
		content = TruncateRune(content, maxChars)
	}

	if strings.TrimSpace(content) == "" {
		return ActionResult{OK: false, Message: "empty speech content"}
	}

	speech := Speech{
		ID:              newSpeechID(),
		Phase:           phase,
		TeamID:          teamID,
		Seat:            seat,
		SpeakerName:     r.SpeakerName(teamID, seat),
		Stance:          r.teamStance(teamID),
		Role:            role,
		Content:         content,
		WordCount:       CountRune(content),
		DurationSec:     0,
		Timestamp:       WallNowMS(),
		References:      params.References,
		InternalThought: params.InternalThought,
		ModelKey:        r.agentModelKey(teamID, seat),
	}
	r.AppendSpeech(speech)

	// §20260831-02 — 实时推送 debate.speech 帧(经 manager 钩子外抛)
	r.emitSpeech(speech)

	return ActionResult{
		OK:       true,
		Message:  "speech accepted",
		SpeechID: speech.ID,
		Phase:    phase,
	}
}

// newSpeechID 生成发言 ID("sp_<unix_ms>_<rand>)"。
func newSpeechID() string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 6)
	for i := range b {
		b[i] = letters[time.Now().UnixNano()%int64(len(letters))]
		time.Sleep(time.Microsecond) // 保证种子递增
	}
	return "sp_" + fmtInt64(time.Now().UnixNano()/1000000) + "_" + string(b)
}

// fmtInt64 把 int64 转 string。
func fmtInt64(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	buf := [20]byte{}
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// isSpeechPhase 是否是正式发言阶段。
func isSpeechPhase(p Phase) bool {
	switch p {
	case PhaseOpeningArgument, PhaseRebuttal, PhaseCrossExamSummary, PhaseClosingArgument:
		return true
	}
	return false
}

// roleMatchesPhase 辩位与阶段是否匹配(只允许该阶段的辩位发言)。
func roleMatchesPhase(role Role, phase Phase) bool {
	switch phase {
	case PhaseOpeningArgument:
		return role == RoleFirst
	case PhaseRebuttal:
		return role == RoleSecond
	case PhaseCrossExamSummary:
		return role == RoleFirst || role == RoleSecond
	case PhaseClosingArgument:
		return role == RoleFourth
	}
	return false
}

// lookupRole 查指定 (teamID, seat) 的辩位。
func (r *DebateRoom) lookupRole(teamID, seat int) (Role, bool) {
	for _, t := range r.Config.Teams {
		if t.TeamID != teamID {
			continue
		}
		for _, a := range t.Agents {
			if a.SeatID == seat {
				return a.Role, true
			}
		}
	}
	return "", false
}

// teamStance 查指定队伍的立场。
func (r *DebateRoom) teamStance(teamID int) Stance {
	for _, t := range r.Config.Teams {
		if t.TeamID == teamID {
			return t.Stance
		}
	}
	return ""
}

// agentModelKey 查指定 Bot 使用的 model_key。
func (r *DebateRoom) agentModelKey(teamID, seat int) string {
	for _, t := range r.Config.Teams {
		if t.TeamID != teamID {
			continue
		}
		for _, a := range t.Agents {
			if a.SeatID == seat {
				return a.ModelKey
			}
		}
	}
	return ""
}

// ============================================================================
// SubmitCrossExamQuestion — 质询提问
// ============================================================================

// SubmitCrossExamQuestion 三辩向对方辩手发起质询问题。
//
// 校验:
//   - 当前阶段 = PhaseCrossExamination
//   - 发言者角色 = third(三辩)
//   - target_team ≠ 自己队伍
//   - target_seat ∈ {0,1,2,3} 或 -1(任意)
//   - question ≤ MaxCrossExamQChars
func (r *DebateRoom) SubmitCrossExamQuestion(teamID, seat int, params CrossExamQuestionParams) ActionResult {
	if r.IsClosed() {
		return ActionResult{OK: false, Message: "room closed"}
	}
	if r.Phase() != PhaseCrossExamination {
		return ActionResult{OK: false, Message: "not in cross-exam phase"}
	}
	role, ok := r.lookupRole(teamID, seat)
	if !ok || role != RoleThird {
		return ActionResult{OK: false, Message: "only third speaker can ask"}
	}
	if params.TargetTeam == teamID {
		return ActionResult{OK: false, Message: "cannot cross-exam own team"}
	}
	if params.TargetSeat < -1 || params.TargetSeat > 3 {
		return ActionResult{OK: false, Message: "invalid target_seat"}
	}

	maxChars := r.Config.PhaseConfig.MaxCrossExamQChars
	question := params.Question
	if CountRune(question) > maxChars {
		question = TruncateRune(question, maxChars)
	}
	if strings.TrimSpace(question) == "" {
		return ActionResult{OK: false, Message: "empty question"}
	}

	qid := newSpeechID()
	entry := CrossExamEntry{
		ID:         qid,
		Questioner: SeatKey(teamID, seat),
		Answerer:   SeatKey(params.TargetTeam, params.TargetSeat),
		Question:   question,
		IsAnswer:   false,
		Timestamp:  WallNowMS(),
	}
	r.AppendCrossExam(entry)
	r.emitCrossExam(entry)

	// 标记质询对(下一对答方为 target)
	r.SetCrossExamActive(SeatKey(teamID, seat), SeatKey(params.TargetTeam, params.TargetSeat))

	return ActionResult{
		OK:       true,
		Message:  "question submitted",
		SpeechID: qid,
	}
}

// ============================================================================
// SubmitCrossExamAnswer — 质询回答
// ============================================================================

// SubmitCrossExamAnswer 回答质询问题。
//
// 校验:
//   - 当前阶段 = PhaseCrossExamination
//   - 发言者 = 当前质询对中的被质询方
//   - question_id 存在
//   - answer ≤ MaxCrossExamAChars
func (r *DebateRoom) SubmitCrossExamAnswer(seat int, params CrossExamAnswerParams) ActionResult {
	if r.IsClosed() {
		return ActionResult{OK: false, Message: "room closed"}
	}
	if r.Phase() != PhaseCrossExamination {
		return ActionResult{OK: false, Message: "not in cross-exam phase"}
	}
	q, a, ok := r.CrossExamActive()
	if !ok {
		return ActionResult{OK: false, Message: "no active cross-exam pair"}
	}
	// 校验发言者是 Answerer
	if _, mySeat, parsed := ParseSeatKey(a); parsed && mySeat != seat {
		return ActionResult{OK: false, Message: "you are not the answerer"}
	}
	_ = q // 暂未使用

	maxChars := r.Config.PhaseConfig.MaxCrossExamAChars
	answer := params.Answer
	if CountRune(answer) > maxChars {
		answer = TruncateRune(answer, maxChars)
	}
	if strings.TrimSpace(answer) == "" {
		return ActionResult{OK: false, Message: "empty answer"}
	}

	// 找当前发言者 team_id
	tid := -1
	for _, t := range r.Config.Teams {
		for _, ag := range t.Agents {
			if ag.SeatID == seat {
				tid = t.TeamID
				break
			}
		}
		if tid >= 0 {
			break
		}
	}
	if tid < 0 {
		return ActionResult{OK: false, Message: "speaker team not found"}
	}

	entry := CrossExamEntry{
		ID:         newSpeechID(),
		Questioner: q,
		Answerer:   SeatKey(tid, seat),
		Question:   "",
		Answer:     answer,
		IsAnswer:   true,
		Timestamp:  WallNowMS(),
	}
	r.AppendCrossExam(entry)
	r.emitCrossExam(entry)

	// 一轮结束:清空 active,让质询方可以再问或切换发言权
	r.ClearCrossExamActive()

	return ActionResult{
		OK:       true,
		Message:  "answer submitted",
		SpeechID: entry.ID,
	}
}

// ============================================================================
// SubmitFreeDebateSpeech — 自由辩论发言
// ============================================================================

// SubmitFreeDebateSpeech 自由辩论发言。
//
// 规则:
//   - 当前阶段 = PhaseFreeDebate
//   - 发言队伍累计用时 < FreeDebateSec(超时截断 + 失败)
//   - content ≤ MaxFreeDebateChars
func (r *DebateRoom) SubmitFreeDebateSpeech(teamID, seat int, params FreeDebateParams) ActionResult {
	if r.IsClosed() {
		return ActionResult{OK: false, Message: "room closed"}
	}
	if r.Phase() != PhaseFreeDebate {
		return ActionResult{OK: false, Message: "not in free-debate phase"}
	}

	maxChars := r.Config.PhaseConfig.MaxFreeDebateChars
	content := params.Content
	if CountRune(content) > maxChars {
		content = TruncateRune(content, maxChars)
	}
	if strings.TrimSpace(content) == "" {
		return ActionResult{OK: false, Message: "empty content"}
	}

	// 累计用时
	const usageSec = 5 // 简化:每次发言计 5 秒
	used := r.MarkFreeDebateUsed(teamID, usageSec)
	maxTotal := r.Config.PhaseConfig.FreeDebateSec
	if used > maxTotal {
		return ActionResult{OK: false, Message: "team free-debate time exhausted"}
	}

	// 写为 Speech(复用结构,Phase 标记)
	role, _ := r.lookupRole(teamID, seat)
	speech := Speech{
		ID:          newSpeechID(),
		Phase:       PhaseFreeDebate,
		TeamID:      teamID,
		Seat:        seat,
		SpeakerName: r.SpeakerName(teamID, seat),
		Stance:      r.teamStance(teamID),
		Role:        role,
		Content:     content,
		WordCount:   CountRune(content),
		Timestamp:   WallNowMS(),
		ModelKey:    r.agentModelKey(teamID, seat),
	}
	r.AppendSpeech(speech)
	r.emitSpeech(speech)

	// 切换发言权给对方队伍
	r.SetFreeDebateTurnOwner("team:" + fmtInt((teamID+1)%r.TeamCount()))

	return ActionResult{
		OK:       true,
		Message:  "free-debate speech accepted",
		SpeechID: speech.ID,
	}
}

// ============================================================================
// FinishSpeak — 主动交还发言权(自由辩论)
// ============================================================================

// FinishSpeak 主动结束本轮发言,把发言权交还对方。
//
// 仅在自由辩论阶段有意义;其他阶段忽略。
func (r *DebateRoom) FinishSpeak(teamID int, reason string) ActionResult {
	if r.Phase() != PhaseFreeDebate {
		return ActionResult{OK: true, Message: "not in free-debate phase, ignore"}
	}
	r.SetFreeDebateTurnOwner("team:" + fmtInt((teamID+1)%r.TeamCount()))
	return ActionResult{
		OK:      true,
		Message: "speak finished",
	}
}

// ============================================================================
// IdleSilent — 沉默
// ============================================================================

// IdleSilent 沉默放弃(不影响流程,主要给 Agent 一个"不做事"出口)。
func (r *DebateRoom) IdleSilent(_ int, _ string) ActionResult {
	return ActionResult{
		OK:      true,
		Message: "silent",
	}
}