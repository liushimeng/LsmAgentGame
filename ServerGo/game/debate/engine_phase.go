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
		askerSeat := findFirstRole(askerTeam, RoleThird)
		if askerSeat < 0 {
			continue
		}

		// 清上一轮残留(未回答的旧 pair 不得影响本轮 target 判定)
		e.room.ClearCrossExamActive()

		// 触发提问方 Bot 发起 cross_exam_question(等待 LLM 返回并落库)
		e.driveSpeaker(PhaseCrossExamination, askerTeam.TeamID, askerSeat)

		// §20260831-02 — 回答方以提问工具实际指定的 target 为准
		// (LLM 选的 target_seat 与预选可能不同;以 CrossExamActive 为权威)。
		_, answererKey, ok := e.room.CrossExamActive()
		if !ok {
			continue
		}
		at, as, parsed := ParseSeatKey(answererKey)
		// target_seat=-1(任意)或越界时回退到对方二辩,再退三辩
		_, hasAgent := e.room.AgentByTeamSeat(at, as)
		if !parsed || as < 0 || !hasAgent {
			if s := findFirstRole(findTeam(e.room, at), RoleSecond); s >= 0 {
				as = s
			} else if s := findFirstRole(findTeam(e.room, at), RoleThird); s >= 0 {
				as = s
			} else {
				e.room.ClearCrossExamActive()
				continue
			}
		}
		e.driveSpeaker(PhaseCrossExamination, at, as)
		e.room.ClearCrossExamActive()
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

	// §20260831-02 — 聚合裁判评分 → 写入最终结果。
	// 首期版本漏了这一步:BuildResult 从未被调用,result 恒为 nil,
	// PhaseResult 阶段前端永远拿不到胜负与分数。
	scores := e.room.JudgeScores()
	if len(scores) > 0 {
		res := BuildResult(scores, e.room.TeamCount())
		// §20260831-02 — 胜方名/最佳辩手名用真实队伍立场与辩手名
		// (BuildResult 只有 teamCount,拿不到名字;此前显示「队伍N」占位)。
		if t := findTeam(e.room, res.WinnerTeamID); t != nil {
			res.WinnerTeamName = StanceLabel(t.Stance)
		}
		res.BestDebater.Name = e.room.SpeakerName(res.BestDebater.TeamID, res.BestDebater.Seat)
		if res.BestDebater.Name == "" {
			// 座位越界兜底(裁判提交非法值且未被执行前钳制时)
			if t := findTeam(e.room, res.BestDebater.TeamID); t != nil && len(t.Agents) > 0 {
				res.BestDebater.Seat = t.Agents[0].SeatID
				res.BestDebater.Name = e.room.SpeakerName(res.BestDebater.TeamID, res.BestDebater.Seat)
			}
		}
		for i := range res.TeamScores {
			if t := findTeam(e.room, res.TeamScores[i].TeamID); t != nil {
				res.TeamScores[i].TeamName = StanceLabel(t.Stance)
			}
		}
		e.room.SetResult(res)
		// §20260831-06 — 累加模型胜率统计(§06 §9 历史统计)。
		// 与 resultHook 同一时机;进程内统计,无 IO 不阻塞阶段推进。
		e.manager.RecordGameResult(e.room, res)
		if fn := e.manager.resultHook(); fn != nil {
			go fn(e.room.RoomID, res)
		}
	}
	e.advanceTo(PhaseResult)
}

// runResultPhase 公布结果。
//
// §20260831-11 R8 P1-B:deadline 驱动 + advanceTo 广播。旧实现两处缺陷:
//   - 固定计数循环(for i < showSec)与 room.PhaseDeadline()(SetPhase 时按
//     PhaseDurationSec 写入)脱钩 —— 进入 result 前若存在延迟(评审聚合 /
//     goroutine 调度),会出现「deadline 已过(remaining=0)但函数仍在数秒」
//     的窗口,任何未预期路径都会让 phase 永远停留 result;
//   - 裸 room.SetPhase(PhaseGameOver) 不触发 onPhaseChange WS 广播
//     (debate.phase 帧)与 emitAgentStatsIfPossible,前端会话内收不到
//     终局 phase 帧(R8 报告 P1-A phase 标签卡死的同源因素)。
//
// 现改为对齐 runPreparationPhase 的 deadline 驱动写法 + advanceTo 推进;
// onGameOver 回调与 ctx.Done 提前返回语义保留。
func (e *DebateEngine) runResultPhase() {
	cfg := e.room.Config.PhaseConfig
	showSec := cfg.ResultShowSec
	if showSec <= 0 {
		showSec = 30
	}
	deadline := e.room.PhaseDeadline()
	if deadline <= 0 {
		// 异常兜底:deadline 未初始化(未经 SetPhase 进入 result)时按 showSec 重算
		deadline = WallNow() + int64(showSec)
	}
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-time.After(time.Second):
			if WallNow() >= deadline {
				e.advanceTo(PhaseGameOver)
				if mgr := e.manager; mgr != nil && mgr.onGameOver != nil {
					go mgr.onGameOver(e.room.RoomID)
				}
				return
			}
		}
	}
}

// driveSpeaker 触发指定 Bot 发言并阻塞等待其本轮完成(§20260831-02)。
//
// 必须:触发前 beginTurn 换新 done chan → TriggerBot → waitTurnDone。
// 若不等,Bot 的 LLM 调用(数秒~数十秒)尚未返回,引擎已 advanceTo 下一
// 阶段,SubmitSpeech 会被阶段校验全部拒绝 —— 首期版本实测即此缺陷。
func (e *DebateEngine) driveSpeaker(phase Phase, teamID, seat int) {
	if e.ctx.Err() != nil {
		return
	}
	key := SeatKey(teamID, seat)
	done := e.beginTurn(key)
	e.room.SetCurrentSpeaker(key)
	e.TriggerBot(teamID, seat)
	e.waitTurnDone(done)
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