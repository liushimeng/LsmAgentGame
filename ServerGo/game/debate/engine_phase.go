// Package debate — 阶段机具体实现(增强版)。
//
// 2026-08-31 §20260831-01 — engine_phase.go v2:
//
//   - 每个 Phase 都有完整的发言顺序与流转逻辑
//   - 通过 SetCurrentSpeaker 标识当前发言者,Agent driver 监听后发起 LLM
//   - 收到 speech 提交后自动推进到下一位 / 下一阶段
//
// 阶段推进逻辑(详见 docs/辩论比赛/01 §3.1):
//
//	PhaseFilling → PhasePreparation (timer)
//
//	→ PhaseOpeningArgument:按队伍顺序遍历一辩 → 全部发言完 → PhaseRebuttal
//	→ PhaseRebuttal:反方二辩 → 正方二辩 → 全部完 → PhaseCrossExamination
//	→ PhaseCrossExamination:正方三辩 → 反方二/三辩(交替)→ PhaseCrossExamSummary
//	→ PhaseCrossExamSummary:一辩或二辩 → PhaseFreeDebate
//	→ PhaseFreeDebate:按 freeDebateTurnOwner 流转 → PhaseClosingArgument
//	→ PhaseClosingArgument:反方四辩 → 正方四辩 → PhaseJudging
//	→ PhaseJudging:3 裁判评分 → PhaseResult
//	→ PhaseResult:30s 展示 → PhaseGameOver
package debate

import (
	"time"
)

// SpeakingOrderForPhase 返回指定阶段的发言顺序(返回 team_id 列表)。
//
// 设计依据:docs/辩论比赛/01 §2.3-2.8 + §3.1 阶段内轮换规则。
func SpeakingOrderForPhase(phase Phase, room *DebateRoom) []int {
	teams := room.Config.Teams
	teamCount := len(teams)
	if teamCount == 0 {
		return nil
	}
	order := make([]int, teamCount)
	for t := 0; t < teamCount; t++ {
		order[t] = teams[t].TeamID
	}

	// 阶段偏移轮换(避免总是同一方先)
	switch phase {
	case PhaseOpeningArgument:
		// 正方 / 政府 / 角度1 先
		order = sortByStancePriority(order, teams, []Stance{StancePro, StanceGovUpper, StanceGovLower, StanceAngle1})
	case PhaseRebuttal:
		// 反方 / 反对党 / 角度2 先
		order = sortByStancePriority(order, teams, []Stance{StanceCon, StanceOppUpper, StanceOppLower, StanceAngle2})
	case PhaseClosingArgument:
		// 反方先
		order = sortByStancePriority(order, teams, []Stance{StanceCon, StanceOppUpper, StanceOppLower})
	default:
		// 其他阶段:保持顺序
	}
	return order
}

// sortByStancePriority 按给定立场优先级对发言队伍排序(命中排在前)。
func sortByStancePriority(order []int, teams []TeamConfig, priority []Stance) []int {
	// 给每个 team 打优先级分
	scores := make(map[int]int, len(teams))
	for _, t := range teams {
		for i, p := range priority {
			if t.Stance == p {
				scores[t.TeamID] = i
				break
			}
		}
		if _, ok := scores[t.TeamID]; !ok {
			scores[t.TeamID] = 99
		}
	}
	out := make([]int, len(order))
	copy(out, order)
	// 简单插入排序(按 scores 升序,小 = 优先级高)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0; j-- {
			if scores[out[j-1]] > scores[out[j]] {
				out[j-1], out[j] = out[j], out[j-1]
			} else {
				break
			}
		}
	}
	return out
}

// RoleSeatForPhase 返回指定 phase 下,指定 team 的发言辩位。
func RoleSeatForPhase(phase Phase, team *TeamConfig) (int, bool) {
	if team == nil {
		return 0, false
	}
	var want Role
	switch phase {
	case PhaseOpeningArgument:
		want = RoleFirst
	case PhaseRebuttal:
		want = RoleSecond
	case PhaseCrossExamSummary:
		// 一辩或二辩均可,优先一辩
		for _, a := range team.Agents {
			if a.Role == RoleFirst {
				return a.SeatID, true
			}
		}
		want = RoleSecond
	case PhaseClosingArgument:
		want = RoleFourth
	default:
		return 0, false
	}
	for _, a := range team.Agents {
		if a.Role == want {
			return a.SeatID, true
		}
	}
	return 0, false
}

// ============================================================================
// 阶段实现
// ============================================================================

// runOpeningArgumentPhase 立论阶段 — 按发言顺序依次通知一辩发言。
func (e *DebateEngine) runOpeningArgumentPhase() {
	order := SpeakingOrderForPhase(PhaseOpeningArgument, e.room)
	for _, tid := range order {
		team := findTeam(e.room, tid)
		if team == nil {
			continue
		}
		seat, ok := RoleSeatForPhase(PhaseOpeningArgument, team)
		if !ok {
			continue
		}
		e.driveSpeaker(PhaseOpeningArgument, tid, seat)
	}
	e.advanceTo(PhaseRebuttal)
}

// runRebuttalPhase 驳论阶段 — 按发言顺序通知二辩。
func (e *DebateEngine) runRebuttalPhase() {
	order := SpeakingOrderForPhase(PhaseRebuttal, e.room)
	for _, tid := range order {
		team := findTeam(e.room, tid)
		if team == nil {
			continue
		}
		seat, ok := RoleSeatForPhase(PhaseRebuttal, team)
		if !ok {
			continue
		}
		e.driveSpeaker(PhaseRebuttal, tid, seat)
	}
	e.advanceTo(PhaseCrossExamination)
}

// runCrossExamPhase 质询阶段 — 三辩发起提问 + 对方回答(最多 N 轮)。
func (e *DebateEngine) runCrossExamPhase() {
	deadline := e.room.PhaseDeadline()
	teams := e.room.Config.Teams
	if len(teams) < 2 {
		e.advanceTo(PhaseCrossExamSummary)
		return
	}

	// 简化策略:每队各提 1 轮问 + 1 轮答(总 2 轮);超时则提前结束。
	for round := 0; round < 2; round++ {
		if WallNow() >= deadline || isCtxDone(e.ctx) {
			break
		}
		// 提问方:round 0 = 队 0, round 1 = 队 1
		askerTeam := &teams[round%len(teams)]
		answererTeam := &teams[(round+1)%len(teams)]

		askerSeat := findFirstRole(askerTeam, RoleThird)
		answererSeat := findFirstRole(answererTeam, RoleSecond)
		if answererSeat < 0 {
			answererSeat = findFirstRole(answererTeam, RoleThird)
		}
		if askerSeat < 0 || answererSeat < 0 {
			continue
		}

		// 触发提问方 Bot 发起 cross_exam_question
		e.driveSpeaker(PhaseCrossExamination, askerTeam.TeamID, askerSeat)

		// 触发回答方 Bot 回答
		e.driveSpeaker(PhaseCrossExamination, answererTeam.TeamID, answererSeat)
	}
	e.advanceTo(PhaseCrossExamSummary)
}

// runCrossExamSummaryPhase 质询小结。
func (e *DebateEngine) runCrossExamSummaryPhase() {
	teams := e.room.Config.Teams
	for i := range teams {
		t := &teams[i]
		seat, ok := RoleSeatForPhase(PhaseCrossExamSummary, t)
		if !ok {
			continue
		}
		e.driveSpeaker(PhaseCrossExamSummary, t.TeamID, seat)
	}
	e.advanceTo(PhaseFreeDebate)
}

// runFreeDebatePhase 自由辩论。
func (e *DebateEngine) runFreeDebatePhase() {
	// 初始化发言权
	if e.room.FreeDebateTurnOwner() == "" {
		e.room.SetFreeDebateTurnOwner("team:0")
	}
	deadline := e.room.PhaseDeadline()
	teams := e.room.Config.Teams

	// 交替发言:每队每次发言 = 5s,直到 deadline 或任一队时间用尽
	for {
		if WallNow() >= deadline || isCtxDone(e.ctx) {
			break
		}
		// 任一队时间用尽就退出
		anyExhausted := false
		for i := range teams {
			if e.room.FreeDebateTimeUsed(teams[i].TeamID) >= e.room.Config.PhaseConfig.FreeDebateSec {
				anyExhausted = true
				break
			}
		}
		if anyExhausted {
			break
		}
		// 当前拥有发言权的队伍选一名 Bot 发言(优先三辩)
		ownerStr := e.room.FreeDebateTurnOwner()
		tid := parseTeamIDFromOwner(ownerStr)
		if tid < 0 {
			break
		}
		team := findTeam(e.room, tid)
		if team == nil {
			break
		}
		// 选任意辩位发言(优先三辩,其次任意)
		seat := findFirstRole(team, RoleThird)
		if seat < 0 {
			for _, a := range team.Agents {
				seat = a.SeatID
				break
			}
		}
		if seat < 0 {
			break
		}

		e.driveSpeaker(PhaseFreeDebate, tid, seat)
		time.Sleep(500 * time.Millisecond) // 简短间隔,避免风暴
	}
	e.advanceTo(PhaseClosingArgument)
}

// runClosingArgumentPhase 总结陈词。
func (e *DebateEngine) runClosingArgumentPhase() {
	order := SpeakingOrderForPhase(PhaseClosingArgument, e.room)
	for _, tid := range order {
		team := findTeam(e.room, tid)
		if team == nil {
			continue
		}
		seat, ok := RoleSeatForPhase(PhaseClosingArgument, team)
		if !ok {
			continue
		}
		e.driveSpeaker(PhaseClosingArgument, tid, seat)
	}
	e.advanceTo(PhaseJudging)
}

// runJudgingPhase 评审阶段 — 触发 3 个裁判 Agent 并发评分。
func (e *DebateEngine) runJudgingPhase() {
	judgeCount := e.room.JudgeCount()
	if judgeCount == 0 {
		e.advanceTo(PhaseResult)
		return
	}

	// 唤醒所有裁判
	for i := 0; i < judgeCount; i++ {
		e.TriggerJudge(i)
	}

	deadline := e.room.PhaseDeadline()
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-time.After(time.Second):
			if len(e.room.JudgeScores()) >= judgeCount {
				break
			}
			if WallNow() >= deadline {
				// 超时:用现有评分
				break
			}
			continue
		}
		break
	}
	e.advanceTo(PhaseResult)
}

// runResultPhase 公布结果。
func (e *DebateEngine) runResultPhase() {
	cfg := e.room.Config.PhaseConfig
	showSec := cfg.ResultShowSec
	if showSec <= 0 {
		showSec = 30
	}
	for i := 0; i < showSec; i++ {
		select {
		case <-e.ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
	e.room.SetPhase(PhaseGameOver)
	if mgr := e.manager; mgr != nil && mgr.onGameOver != nil {
		go mgr.onGameOver(e.room.RoomID)
	}
}

// driveSpeaker 触发指定 Bot 发言(由 driver 监听 BotEventChan)。
func (e *DebateEngine) driveSpeaker(phase Phase, teamID, seat int) {
	if e.ctx.Err() != nil {
		return
	}
	e.room.SetCurrentSpeaker(SeatKey(teamID, seat))
	e.TriggerBot(teamID, seat)
}

// ============================================================================
// helpers
// ============================================================================

// findTeam 查指定 team_id 的队伍配置。
func findTeam(room *DebateRoom, teamID int) *TeamConfig {
	for i, t := range room.Config.Teams {
		if t.TeamID == teamID {
			return &room.Config.Teams[i]
		}
	}
	return nil
}

// findFirstRole 返回指定队伍中指定 Role 的 SeatID,找不到返回 -1。
func findFirstRole(team *TeamConfig, role Role) int {
	if team == nil {
		return -1
	}
	for _, a := range team.Agents {
		if a.Role == role {
			return a.SeatID
		}
	}
	return -1
}

// parseTeamIDFromOwner 从 "team:N" 中解析 N,失败返回 -1。
func parseTeamIDFromOwner(s string) int {
	if len(s) < 6 || s[:5] != "team:" {
		return -1
	}
	v, ok := parseInt(s[5:])
	if !ok {
		return -1
	}
	return v
}

// isCtxDone 检查 ctx 是否已取消。
func isCtxDone(ctx contextLike) bool {
	if ctx == nil {
		return false
	}
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

// contextLike 是 ctx.Done() 的最小接口(context.Context 已满足)。
type contextLike interface {
	Done() <-chan struct{}
}