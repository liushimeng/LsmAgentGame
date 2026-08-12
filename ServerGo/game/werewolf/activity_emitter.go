// Package werewolf — activity_emitter.go: 暴露给 WerewolfManager 的最小活动
// 事件接口,以及在关键游戏节点调用 ChatService.EmitRoomActivity 的 helper。
//
// 2026-07-09 §13 增强 §115 房间聊天 — see docs/狼人杀-Agent与系统/狼人杀房间聊天设计.md
// §3.4(后端实现)与 §3.3(wire 协议)。
//
// 设计:
//   - 引入轻量接口 ActivityEmitter,只暴露 EmitRoomActivity 一个方法;
//   - 避免 werewolf 包反向 import ws 包(若 import 会造成循环);
//   - main.go 在 wire 时把 *ws.ChatService 直接当 ActivityEmitter 用
//     (方法签名完全匹配);
//   - helper emitActivity 是 nil-safe 包装,所有调用点无需 if 守卫。
package werewolf

import (
	"context"
	"strconv"

	"LsmWebGame/agent/wwcommentator"
	"LsmWebGame/config"
	"LsmWebGame/logger"

	"go.uber.org/zap"
)

// ActivityEmitter 是 ChatService 暴露给 werewolf 包的活动事件广播接口。
// 由 main.go 在 wire 阶段把 *ws.ChatService 注入(方法签名兼容即可)。
// 2026-07-09 §115.
type ActivityEmitter interface {
	EmitRoomActivity(roomID, eventKind, text, phase string,
		roundNumber int, severity, icon string, refSeat, refSeat2 int,
		silentForBots bool) bool
}

// SetActivityEmitter 注入活动事件发射器。重复调用覆盖前一个。传 nil 清除。
func (m *WerewolfManager) SetActivityEmitter(em ActivityEmitter) {
	m.activityEmitter = em
}

// emitActivity 是 nil-safe 的活动广播包装。调用方无须 if 守卫。
// 接受 seat=-1 表示"无关联座位",内部把 -1 透传给 EmitRoomActivity。
func (m *WerewolfManager) emitActivity(r *WerewolfRoom, eventKind, text, phase string,
	roundNumber int, severity, icon string, refSeat, refSeat2 int, silentForBots bool) {
	if m.activityEmitter == nil || r == nil {
		return
	}
	m.activityEmitter.EmitRoomActivity(r.RoomID, eventKind, text, phase,
		roundNumber, severity, icon, refSeat, refSeat2, silentForBots)
}

// ─────────────────── 节点调用 helper ───────────────────
// 下方函数把"游戏节点 → activity 事件"的映射集中,业务逻辑(quarantine_skip
// / vote / kill / seer / witch / hunter / sheriff / phase)在调用方
// 触发,具体怎么措辞在这里。

// Activity event kind constants — used by Emit* functions and surfaced
// to the frontend for stable event_kind identification. 2026-07-09 §115.
const (
	ActivityEventKindPhaseTransition = "phase_transition"
	ActivityEventKindRoundStart      = "round_start"
	ActivityEventKindNightStart      = "night_start"
	ActivityEventKindDayStart        = "day_start"
	ActivityEventKindWolfKill        = "wolf_kill"
	ActivityEventKindSeerCheck       = "seer_check"
	ActivityEventKindWitchAct        = "witch_act"
	ActivityEventKindVoteCast        = "vote_cast"
	ActivityEventKindVoteResult      = "vote_result"
	ActivityEventKindHunterShoot     = "hunter_shoot"
	ActivityEventKindSheriffElect    = "sheriff_elect"
	ActivityEventKindGameOver        = "game_over"
	ActivityEventKindPlayerDied      = "player_died"
	ActivityEventKindQuarantine      = "quarantine"
	ActivityEventKindAutoSkip        = "auto_skip"
	// BUG 2026-07-09: 遗言阶段活动事件
	ActivityEventKindDeathLyricStart   = "death_lyric_start"
	ActivityEventKindDeathLyricSpoken  = "death_lyric_spoken"
	ActivityEventKindDeathLyricSkipped = "death_lyric_skipped"
	ActivityEventKindGuardProtect      = "guard_protect"      // §134 守卫守护事件
	ActivityEventKindKnightDuel        = "knight_duel"        // §198 骑士决斗事件
	ActivityEventKindDemonHunterHunt   = "demon_hunter_hunt"  // §猎魔人 猎魔人狩猎事件
	// 2026-08-10 §20260810-06 — 行为承诺事件
	ActivityEventKindCommitMade      = "commit_made"      // 承诺已做出
	ActivityEventKindCommitEvaluated = "commit_evaluated" // 承诺已判定
	// 2026-08-10 §20260810-11 H2 — 质疑 challenge
	ActivityEventKindChallenge = "challenge" // 白天发言阶段公开质疑
	// §20260811-06 U5 — 黎明流言系统(5 类模板 + 60-100% 真假混合)。
	// 走既有活动流广播;Agent 侧 GameContext.LastRumors 镜像。
	ActivityEventKindRumor = "rumor"
)

// EmitPhaseTransition 在 phase 切换时调用(由 setPhaseLocked 或
// 外部 controller 触发)。severity=info。
func (m *WerewolfManager) EmitPhaseTransition(r *WerewolfRoom, newPhase string) {
	if r == nil || r.State == nil {
		return
	}
	text := phaseLabelCN(newPhase)
	m.emitActivity(r,
		ActivityEventKindPhaseTransition,
		"→ "+text,
		newPhase,
		r.State.DayNumber,
		"info", "🔁", -1, -1, false)
}

// EmitRoundStart 在 advanceDayLocked 跨日时调用,显示"第 N 天 开始"。
func (m *WerewolfManager) EmitRoundStart(r *WerewolfRoom) {
	if r == nil || r.State == nil {
		return
	}
	text := "第 " + strconv.Itoa(r.State.DayNumber) + " 天 开始"
	m.emitActivity(r,
		ActivityEventKindRoundStart,
		text,
		r.State.Phase.String(),
		r.State.DayNumber,
		"info", "📅", -1, -1, false)
}

// EmitNightStart 在 startNight / setPhaseLocked(PhaseNight) 时调用。
func (m *WerewolfManager) EmitNightStart(r *WerewolfRoom) {
	if r == nil || r.State == nil {
		return
	}
	m.emitActivity(r,
		ActivityEventKindNightStart,
		"🌙 夜晚降临",
		r.State.Phase.String(),
		r.State.DayNumber,
		"info", "🌙", -1, -1, false)
}

// EmitDayStart 在 startDay / setPhaseLocked(PhaseDay) 时调用。
func (m *WerewolfManager) EmitDayStart(r *WerewolfRoom) {
	if r == nil || r.State == nil {
		return
	}
	m.emitActivity(r,
		ActivityEventKindDayStart,
		"☀️ 天亮了",
		r.State.Phase.String(),
		r.State.DayNumber,
		"info", "☀️", -1, -1, false)
}

// EmitWolfKill 在 wolfKillLocked 成功后调用。silentForBots=true(LLM 已知
// 自己的击杀,无需回灌),但对玩家/观众显示"X号 被狼人带走"。
func (m *WerewolfManager) EmitWolfKill(r *WerewolfRoom, target int) {
	if r == nil {
		return
	}
	text := "🐺 狼人击杀 " + strconv.Itoa(target+1) + "号"
	phase := ""
	roundN := 0
	if r.State != nil {
		phase = r.State.Phase.String()
		roundN = r.State.DayNumber
	}
	m.emitActivity(r, ActivityEventKindWolfKill, text, phase, roundN,
		"warn", "🐺", target, -1, true)
}

// EmitSeerCheck 在 seerCheckLocked 成功后调用。silentForBots=false(LLM
// 不知情,但其他存活 bot 看到"X号 被查验"会改变策略)。
func (m *WerewolfManager) EmitSeerCheck(r *WerewolfRoom, target int) {
	if r == nil {
		return
	}
	text := "🔮 预言家查验 " + strconv.Itoa(target+1) + "号"
	phase := ""
	roundN := 0
	if r.State != nil {
		phase = r.State.Phase.String()
		roundN = r.State.DayNumber
	}
	m.emitActivity(r, ActivityEventKindSeerCheck, text, phase, roundN,
		"info", "🔮", target, -1, true)
}

// EmitWitchAct 在 witchActLocked 成功后调用。
func (m *WerewolfManager) EmitWitchAct(r *WerewolfRoom, action string) {
	if r == nil {
		return
	}
	cn := "用解药"
	if action == "poison" {
		cn = "用毒药"
	} else if action == "antidote" {
		cn = "用解药"
	} else if action == "skip" || action == "" {
		cn = "未使用"
	}
	text := "🧪 女巫 " + cn
	phase := ""
	roundN := 0
	if r.State != nil {
		phase = r.State.Phase.String()
		roundN = r.State.DayNumber
	}
	m.emitActivity(r, ActivityEventKindWitchAct, text, phase, roundN,
		"info", "🧪", -1, -1, true)
}

// EmitGuardProtect §134 守卫守护事件广播。
// 公开活动流**绝不能**包含守护目标座位号 —— 否则狼人直接读到护盾位置,
// 守卫失去全部价值。与 EmitWitchAct 只播报"用解药 / 用毒药"不播报对象同原则。
// 与 EmitWitchAct 同构:固定文案 "🛡️ 守卫已行动",refSeat/refSeat2=-1/-1。
func (m *WerewolfManager) EmitGuardProtect(r *WerewolfRoom) {
	if r == nil {
		return
	}
	text := "🛡️ 守卫已行动"
	phase := ""
	roundN := 0
	if r.State != nil {
		phase = r.State.Phase.String()
		roundN = r.State.DayNumber
	}
	m.emitActivity(r, ActivityEventKindGuardProtect, text, phase, roundN,
		"info", "🛡️", -1, -1, true)
}

// EmitKnightDuel §198 骑士决斗事件广播。
// 与守卫不同:骑士决斗是公开技能,actor 与 target 都必须出现在文案中,
// `hit_wolf` 决定后缀 — 这正是技能博弈价值(亮身份公开轮替)。
func (m *WerewolfManager) EmitKnightDuel(r *WerewolfRoom, actor, target int, hitWolf bool) {
	if r == nil {
		return
	}
	text := "⚔️ " + seatCN(actor) + "号 骑士对 " + seatCN(target) + "号 发动决斗"
	if hitWolf {
		text += "\n  → " + seatCN(target) + "号 是狼人," + seatCN(target) + "号 出局!"
	} else {
		text += "\n  → " + seatCN(target) + "号 不是狼人," + seatCN(actor) + "号 骑士自决出!"
	}
	phase := ""
	roundN := 0
	if r.State != nil {
		phase = r.State.Phase.String()
		roundN = r.State.DayNumber
	}
	m.emitActivity(r, ActivityEventKindKnightDuel, text, phase, roundN,
		"warn", "⚔️", actor, target, true)
}

// EmitDemonHunterHunt §猎魔人 猎魔人狩猎事件广播。
// 与骑士不同:猎魔人狩猎是公开技能,actor 与 target 都必须出现在文案中,
// `hit_wolf` 决定后缀 — 这正是技能博弈价值(亮身份公开轮替)。
// silentForBots=false(其他存活 bot 需要看到「X 是狼人 / X 不是狼人」来调整策略)。
func (m *WerewolfManager) EmitDemonHunterHunt(r *WerewolfRoom, actor, target int, hitWolf bool) {
	if r == nil {
		return
	}
	text := ""
	if target < 0 {
		// 空过
		text = "🎯 " + seatCN(actor) + "号 猎魔人选择空过(本晚不动)"
	} else {
		text = "🎯 " + seatCN(actor) + "号 猎魔人对 " + seatCN(target) + "号 发动狩猎"
		if hitWolf {
			text += "\n  → " + seatCN(target) + "号 是狼人," + seatCN(target) + "号 出局!"
		} else {
			text += "\n  → " + seatCN(target) + "号 不是狼人," + seatCN(actor) + "号 猎魔人自决出!"
		}
	}
	phase := ""
	roundN := 0
	if r.State != nil {
		phase = r.State.Phase.String()
		roundN = r.State.DayNumber
	}
	m.emitActivity(r, ActivityEventKindDemonHunterHunt, text, phase, roundN,
		"warn", "🎯", actor, target, false)
}

// seatCN 把 0-indexed 座位号转成"X号"中文(供 Emit* 文案拼接)。
// 与 strconv.Itoa + "号" 等价,但集中到一个 helper 以避免散落 magic。
func seatCN(seat int) string { return strconv.Itoa(seat + 1) }

// EmitVoteCast 在 Action_Vote 成功时调用(actor 投给 target)。
// silentForBots=true(LLM 已知自己投了谁,无需回灌)。
func (m *WerewolfManager) EmitVoteCast(r *WerewolfRoom, actor, target int) {
	if r == nil {
		return
	}
	text := "🗳 " + strconv.Itoa(actor+1) + "号 投票给 " + strconv.Itoa(target+1) + "号"
	phase := ""
	roundN := 0
	if r.State != nil {
		phase = r.State.Phase.String()
		roundN = r.State.DayNumber
	}
	m.emitActivity(r, ActivityEventKindVoteCast, text, phase, roundN,
		"info", "🗳", actor, target, true)
}

// EmitVoteResult 在 finishVoteLocked 完成后调用,显示"X号 出局"。
// BUG-R73-P1 (投票阶段无投票结果广播): 用户反馈投票结束后公屏看不到
// 任何票数明细,只能从"下一夜"推断"无人被放逐"。现在把每个候选人的
// 得票数拼进 text,前端直接渲染,无需再从 vote_cast 事件反推。
//
//	text 形如: "📊 投票结果: 3号(3票) 出局 · 5号(2票) · 1号(1票)"
//	平票:      "📊 投票平票,进入 PK · 3号(2票) · 5号(2票)"
//	无人出局:  "📊 投票无人出局(全员弃权)"
func (m *WerewolfManager) EmitVoteResult(r *WerewolfRoom, target int, tied bool) {
	if r == nil {
		return
	}
	// 抓 tally 明细(在 FinishVote 之前已快照到调用方,这里重新抓一次
	// 也安全 — FinishVote 不改变 vote 字段,只推进 phase)。
	var tally map[Seat]int
	if r.State != nil {
		tally = r.State.TallyVotes(false)
	}
	var text string
	severity := "warn"
	if tied {
		text = "📊 投票平票,进入 PK"
		severity = "info"
	} else if target < 0 {
		text = "📊 投票无人出局(全员弃权)"
		severity = "info"
	} else {
		text = "📊 投票结果:" + strconv.Itoa(target+1) + "号 出局"
		severity = "warn"
	}
	// 拼上每个候选人的得票明细(按得票降序,平票按座位升序)。
	if len(tally) > 0 {
		type kv struct {
			seat  Seat
			count int
		}
		var kvs []kv
		for s, c := range tally {
			kvs = append(kvs, kv{seat: s, count: c})
		}
		// 按得票降序,同票按座位升序(稳定可读)。
		for i := 0; i < len(kvs); i++ {
			for j := i + 1; j < len(kvs); j++ {
				if kvs[j].count > kvs[i].count ||
					(kvs[j].count == kvs[i].count && kvs[j].seat < kvs[i].seat) {
					kvs[i], kvs[j] = kvs[j], kvs[i]
				}
			}
		}
		detail := " · "
		for i, kv := range kvs {
			if i > 0 {
				detail += " · "
			}
			detail += strconv.Itoa(int(kv.seat)+1) + "号(" + strconv.Itoa(kv.count) + "票)"
		}
		text += detail
	}
	phase := ""
	roundN := 0
	if r.State != nil {
		phase = r.State.Phase.String()
		roundN = r.State.DayNumber
	}
	m.emitActivity(r, ActivityEventKindVoteResult, text, phase, roundN,
		severity, "📊", target, -1, false)
	// §20260811-09 U1 — 投票结果触发解说(旁观者点评票型)。
	r.triggerCommentaryEventLocked(wwcommentator.CommentaryPendingVoteResult, map[string]any{
		"target": target, "tied": tied, "tally": tally,
	})
	// §20260812-02 U3 — 结算观众押注(调用方已持 r.mu)。
	// 平票(tied=true)→ actualTarget=-1(无人出局,退款)。
	if tied {
		r.SettleSpectatorBetsLocked(-1)
	} else {
		r.SettleSpectatorBetsLocked(target)
	}
}

// EmitHunterShoot 在 hunterShootLocked 完成后调用。
func (m *WerewolfManager) EmitHunterShoot(r *WerewolfRoom, target int) {
	if r == nil {
		return
	}
	var text string
	if target < 0 {
		text = "🏹 猎人放弃开枪"
	} else {
		text = "🏹 猎人开枪:" + strconv.Itoa(target+1) + "号"
	}
	phase := ""
	roundN := 0
	if r.State != nil {
		phase = r.State.Phase.String()
		roundN = r.State.DayNumber
	}
	m.emitActivity(r, ActivityEventKindHunterShoot, text, phase, roundN,
		"warn", "🏹", target, -1, false)
}

// EmitCommitMade 在玩家做出承诺时调用（§20260810-06）。
func (m *WerewolfManager) EmitCommitMade(r *WerewolfRoom, seat int, template string, target int) {
	if r == nil {
		return
	}
	text := "📝 " + strconv.Itoa(seat+1) + "号做出承诺"
	if target >= 0 {
		text += ":针对 " + strconv.Itoa(target+1) + "号"
	}
	phase := ""
	roundN := 0
	if r.State != nil {
		phase = r.State.Phase.String()
		roundN = r.State.DayNumber
	}
	m.emitActivity(r, ActivityEventKindCommitMade, text, phase, roundN,
		"info", "📝", seat, target, false)
}

// EmitCommitEvaluated 在承诺被判定（兑现/违背/过期）时调用（§20260810-06）。
// status 为 fulfilled / broken / expired 之一。
func (m *WerewolfManager) EmitCommitEvaluated(r *WerewolfRoom, seat int, template string, status string) {
	if r == nil {
		return
	}
	var icon, label string
	switch status {
	case "fulfilled":
		icon = "✅"
		label = "兑现"
	case "broken":
		icon = "❌"
		label = "违背"
	case "expired":
		icon = "⏳"
		label = "过期"
	default:
		icon = "📝"
		label = status
	}
	text := icon + " " + strconv.Itoa(seat+1) + "号的承诺已" + label
	phase := ""
	roundN := 0
	if r.State != nil {
		phase = r.State.Phase.String()
		roundN = r.State.DayNumber
	}
	m.emitActivity(r, ActivityEventKindCommitEvaluated, text, phase, roundN,
		"info", icon, seat, -1, false)
}

// EmitSheriffElect 在 sheriffElectLocked 完成后调用。
func (m *WerewolfManager) EmitSheriffElect(r *WerewolfRoom, sheriff int) {
	if r == nil {
		return
	}
	text := "⭐ 警长:" + strconv.Itoa(sheriff+1) + "号"
	phase := ""
	roundN := 0
	if r.State != nil {
		phase = r.State.Phase.String()
		roundN = r.State.DayNumber
	}
	m.emitActivity(r, ActivityEventKindSheriffElect, text, phase, roundN,
		"success", "⭐", sheriff, -1, false)
}

// EmitChallenge §20260810-11 H2 — 白天发言阶段公开质疑活动事件。
// challenger/target 公开;question 公开(§119 协议层隔离:仅走活动流不进 chat_message 表)。
// icon="❓",severity="info",refSeat=challenger,refSeat2=target。
func (m *WerewolfManager) EmitChallenge(r *WerewolfRoom, challenger int, target int, question string) {
	if r == nil {
		return
	}
	if question == "" {
		question = "(无内容)"
	}
	text := "❓ " + strconv.Itoa(challenger+1) + "号 质疑 " + strconv.Itoa(target+1) + "号:" + question
	phase := ""
	roundN := 0
	if r.State != nil {
		phase = r.State.Phase.String()
		roundN = r.State.DayNumber
	}
	m.emitActivity(r, ActivityEventKindChallenge, text, phase, roundN,
		"info", "❓", challenger, target, false)
}

// EmitGameOver 在 checkWinner 触发游戏结束时调用。
//
// 2026-07-10 §4 增强: 走完活动广播后,遍历所有 bot 玩家做金币结算:
//   - win  → CoinDelta = +100  → wallet.Credit
//   - lose → CoinDelta = -100  → wallet.Debit
//   - draw → CoinDelta = 0     → 仅写 game_log 收尾,wallet 不动
//
// 失败仅 log,不阻塞游戏流程(硬约束 §4-5.5)。
func (m *WerewolfManager) EmitGameOver(r *WerewolfRoom, winner string) {
	if r == nil {
		return
	}
	// §20260811-08 U2 — 终局奖励收口发放(§130 接线修复)。
	//
	// 旧版仅 forceCloseRoomLocked(room_restart_vote.go:353)一条路径调用
	// grantSettlementRewardsLocked,而 EmitGameOver 有 4 个生产调用点:
	//   room_watchdog.go:184     冷却期未启用/已冷却过 → 立刻关门
	//   room_watchdog.go:205     restartVoteDone 或其他 over 子状态
	//   room_restart_vote.go:136 finishCoolingLocked(§129 冷却期结束,最常见路径)
	//   room_restart_vote.go:353 forceCloseRoomLocked(旧版唯一命中)
	// 前 3 条全部漏发 —— 也就是说绝大多数对局的结算奖励从未发放过。
	//
	// §92a 锁态核对:4 个调用点全部已持 r.mu(phaseWatchdogTick /
	// finishCoolingLocked / forceCloseRoomLocked),故此处继续调用锁内变体,
	// 不需要新建公开变体。
	m.grantSettlementRewardsLocked(r, m.rewardSvc)

	text := "🏆 游戏结束:" + winner + " 阵营胜利"
	m.emitActivity(r, ActivityEventKindGameOver, text, "", 0,
		"success", "🏆", -1, -1, false)
	// §20260811-09 U1 — 终局触发解说点评。
	r.triggerCommentaryEventLocked(wwcommentator.CommentaryPendingGameOver, map[string]any{
		"winner": winner,
	})
	// 金币结算（bot + 人类玩家）
	m.settleBotsAfterGameOver(r, winner)
	m.settleHumansAfterGameOver(r, winner)
	// 2026-07-30 解决和设计方案-20260730-03 Fix-A3/C: 终局收编广播。
	// EmitGameOver 可能发生在冷却期结束后很久(甚至观众已换了几批),
	// 此时再广播一次权威终局帧 + game.over 帧(winner),保证任何时机的
	// 客户端都能拿到「结束 + 胜方」,前端 game-over-banner 有数据源。
	if m.onGameOverBroadcast != nil {
		m.onGameOverBroadcast(r.RoomID, winner)
	}
}

// settleBotsAfterGameOver 在对局结束时遍历所有 bot 玩家,
// 走 RecordLog.GameEnded + WalletService.Credit/Debit 结算（底注彩池制）。
// winner 是 "wolf" / "good" / "draw" 之一(由 checkWinner 返回,见 isWolfWinner)。
// 失败仅 log,不阻塞游戏流程。
func (m *WerewolfManager) settleBotsAfterGameOver(r *WerewolfRoom, winner string) {
	if r == nil {
		return
	}
	// 没有 recordLog → 整段跳过(测试 / 老代码路径)
	if m.RecordLog == nil {
		return
	}
	ante := int64(anteCoinFromCfg())
	if ante <= 0 {
		// 金币博弈关闭,仅写 game_log 收尾(coinDelta=0)
		ante = 0
	}
	// 先统计胜方/败方人数(用于彩池制)
	winCount, loseCount := countWinLose(r, winner)
	for seat, userID := range r.Seats {
		if userID == "" {
			continue
		}
		// 1. 判定本 bot 的 win/lose 状态(彩池制)
		result, coinDelta := judgeBotResult(r, seat, winner, int64(winCount), int64(loseCount), ante)
		// §20260811-09 U2 — 难度档倍率(仅胜方)。settleBots 与 settleHumans 同款语义。
		if coinDelta > 0 {
			if mult := r.DifficultyCoinMultiplierX10Locked(); mult != 10 {
				coinDelta = coinDelta * mult / 10
			}
		}
		// 2. 拿 GameLogID(从 RecordLog cache;miss 时跳过)
		gameLogID := m.RecordLog.GameLogIDByRoomSeat(r.RoomID, seat)
		if gameLogID == "" {
			logger.L().Warn("settle: no game_log cache for bot",
				zap.String("room_id", r.RoomID), zap.Int("seat", seat))
			continue
		}
		// 3. 写 game_log 收尾 + 异步结算 wallet
		// 注:llmCallCount / tokens 当前未在 GameState 累加,传 0
		// (后续阶段 5 可接 agent.Memory.Stats() 拿到精确值)。
		m.RecordLog.GameEnded(
			context.Background(),
			gameLogID,
			result,
			coinDelta,
			0,  // llmCallCount
			0,  // inputTokens
			0,  // outputTokens
			"", // finalHand:狼人杀无残局概念
		)
	}
	logger.L().Info("settle: game_over bot settlement done",
		zap.String("room_id", r.RoomID), zap.String("winner", winner),
		zap.Int64("ante", ante), zap.Int("win_count", winCount), zap.Int("lose_count", loseCount))
}

// settleHumansAfterGameOver 在对局结束时遍历所有人类玩家,
// 走 WalletService.Credit/Debit 直接结算（底注彩池制）。
// 人类玩家余额 < ante 时该局不参与输赢（体验不中断）。
// 异步结算,失败仅 log,不阻塞游戏流程（硬约束 §4-5.5）。
func (m *WerewolfManager) settleHumansAfterGameOver(r *WerewolfRoom, winner string) {
	if r == nil {
		return
	}
	// 无钱包服务 / 金币博弈关闭 → 整段跳过
	if m.Wallet == nil {
		return
	}
	ante := int64(anteCoinFromCfg())
	if ante <= 0 {
		return
	}
	winCount, loseCount := countWinLose(r, winner)
	for seat, userID := range r.Seats {
		if userID == "" {
			continue
		}
		// bot 走另一路径,此处仅结算人类
		if seat < len(r.State.Players) && r.State.Players[seat].IsBot {
			continue
		}
		delta := computeCoinDelta(winner, r.State.Roles[seat], int64(winCount), int64(loseCount), ante)
		// §20260811-09 U2 — 难度档倍率(仅胜方)。败方扣款不变 → 新手保护:
		// easy 局不放大惩罚,胜方收益按 CoinMultiplierX10 / 10 缩放。
		if delta > 0 {
			if mult := r.DifficultyCoinMultiplierX10Locked(); mult != 10 {
				delta = delta * mult / 10
			}
		}
		if delta == 0 {
			continue
		}
		// 人类「余额不足跳过」守卫
		bal, err := m.Wallet.GetBalance(context.Background(), userID)
		if err != nil {
			logger.L().Warn("settle: human balance fetch failed, skip",
				zap.String("room_id", r.RoomID), zap.Int("seat", seat),
				zap.String("user_id", userID), zap.Error(err))
			continue
		}
		if bal < ante {
			logger.L().Info("settle: human balance < ante, skip",
				zap.String("room_id", r.RoomID), zap.Int("seat", seat),
				zap.String("user_id", userID), zap.Int64("balance", bal), zap.Int64("ante", ante))
			continue
		}
		uid := userID
		d := delta
		go func() {
			defer func() {
				if rec := recover(); rec != nil {
					logger.L().Error("settle: human settlement panic recovered",
						zap.String("room_id", r.RoomID), zap.Any("panic", rec))
				}
			}()
			// v2.0 DEFECT 2 (L2 app-level dedup): 若该玩家在本局(同 room_id)已结算过,
			// 视为已结算的良性 no-op(跳过,不二次改余额)。防 L1 内存标志失效
			// (进程重启)后跨进程重复结算。settlement 用 ref_id=roomID(每局唯一),
			// (user_id, ref_type, ref_id, tx_type) 每玩家每局唯一。
			if done, derr := m.Wallet.AlreadySettled(context.Background(), uid,
				"werewolf_game", r.RoomID, "werewolf_game_settle"); derr == nil && done {
				logger.L().Info("settle: human already settled, skip (idempotent)",
					zap.String("room_id", r.RoomID), zap.String("user_id", uid))
				return
			}
			var settleErr error
			if d > 0 {
				settleErr = m.Wallet.Credit(context.Background(), uid,
					"werewolf_game_settle", "werewolf_game", r.RoomID, "werewolf", "狼人杀13人局彩池奖励", d)
			} else {
				settleErr = m.Wallet.Debit(context.Background(), uid,
					"werewolf_game_settle", "werewolf_game", r.RoomID, "werewolf", "狼人杀13人局彩池底注", -d)
			}
			if settleErr != nil {
				logger.L().Error("settle: human wallet settle failed",
					zap.String("room_id", r.RoomID), zap.String("user_id", uid),
					zap.Int64("delta", d), zap.Error(settleErr))
				return
			}
			// 2026-07-21 道具系统：胜方额外分得道具彩池回滚奖金。
			// r.propPotBonus 是本局道具消耗回滚到彩池的总额（50%部分），
			// 按比例分配给胜方每位玩家。draw 时不发放（彩池保留到系统）。
			var propBonus int64
			if d > 0 && r.propPotBonus > 0 && winCount > 0 {
				propBonus = PropDistributePotBonus(r.propPotBonus, winCount)
				if propBonus > 0 {
					_ = m.Wallet.Credit(context.Background(), uid,
						"werewolf_prop_pot_bonus", "werewolf_game", r.RoomID, "werewolf",
						"狼人杀道具彩池回池奖励", propBonus)
					d += propBonus
				}
			}
			// 读取最新余额,WS 实时推送
			newBal, _ := m.Wallet.GetBalance(context.Background(), uid)
			m.pushBalanceChangeLocked(uid, newBal, d, "werewolf_game_over")
			// 推送 per-user 结算明细(供前端 SettlementModal 渲染)。
			// result 基于实际 delta 符号推断(与 UI 层 win/lose/draw 口径一致)。
			settleResult := "draw"
			if d > 0 {
				settleResult = "win"
			} else if d < 0 {
				settleResult = "lose"
			}
			m.pushSettlementLocked(uid, map[string]any{
				"room_id":      r.RoomID,
				"game_kind":    "werewolf",
				"winner":       winner,
				"result":       settleResult,
				"ante":         ante,
				"netGain":      d,
				"finalBalance": newBal,
			})
		}()
	}
	logger.L().Info("settle: game_over human settlement fired",
		zap.String("room_id", r.RoomID), zap.String("winner", winner))
}

// judgeBotResult 根据 winner 阵营和 bot 所在阵营判定单局结果（底注彩池制）。
// 阵营映射:
//   - werewolf 阵营:RoleWerewolf
//   - good 阵营:RoleVillager / RoleSeer / RoleWitch / RoleHunter / RoleIdiot
//
// winner == "draw" → 全员 result="draw",coinDelta=0。
// 彩池制公式（非 draw）：
//
//	败方每人：-Ante
//	胜方每人：+Ante × (败方人数 / 胜方人数)
func judgeBotResult(r *WerewolfRoom, seat int, winner string, winCount, loseCount, ante int64) (result string, coinDelta int64) {
	if winner == "draw" || ante <= 0 {
		return "draw", 0
	}
	// winner ∈ {"wolf", "good"}（引擎权威串,见 isWolfWinner）— 推断本 bot 的阵营
	if r.State == nil || int(seat) >= len(r.State.Roles) {
		return "lose", 0
	}
	coinDelta = computeCoinDelta(winner, r.State.Roles[seat], winCount, loseCount, ante)
	if coinDelta > 0 {
		return "win", coinDelta
	}
	if coinDelta < 0 {
		return "lose", coinDelta
	}
	return "draw", 0
}

// isWolfWinner 判定引擎下发的 winner 串是否代表「狼人阵营胜」。
//
// 权威契约（v2.0 DEFECT 1）：引擎 checkWinner() 只会填 "wolf"（狼人胜）/
// "good"（好人胜）/ "draw"（平局），与 Faction.String() 一致
// （ServerGo/game/werewolf/engine.go:614 填 gs.Winner = "wolf"；
// engine_test.go:196,289,1123 均断言 == "wolf"）。
// 历史结算代码曾误比对 "werewolf"，导致狼人胜局全员被扣 -Ante（非零和、静默漏币）。
// 本 helper 以 "wolf" 为规范值，同时容忍历史 / 未来调用方传入 "werewolf"，
// 使结算侧对胜方字符串鲁棒。**引擎侧不改**（测试依赖 "wolf"）。
func isWolfWinner(winner string) bool {
	return winner == "wolf" || winner == "werewolf"
}

// computeCoinDelta 按底注彩池制计算单玩家的净输赢。
// 公式（非 draw 且 ante > 0）：
//
//	败方每人：-Ante
//	胜方每人：+Ante × (败方人数 / 胜方人数)（整数除法,余数不分配,见下方零和说明）
//
// draw / ante <= 0 → 0。
// faction == FactionUnknown → 0（角色没识别出来,保守不输不赢）。
//
// 零和与余数（v2.0 FIX 4）：胜方每人拿 ante*loseCount/winCount 用整数除法,
// 会留下余数 rem = (ante*loseCount) mod winCount ∈ [0, winCount)。该余数**不分配**,
// 留在系统（未发放）。因此每局系统净收 [0, winCount) 金币 —— 是有界的微量通缩,
// **永远不会凭空创造金币**（胜方合计拿到的 ≤ 败方合计付出的）。示例:好人胜 9v4 时,
// 败方付 400,胜方每人 +44(=100*4/9),9 人共发 396,系统截留 4(=400-396)。
// 这是刻意选择的「简单且正确」方案:不给前 k 名胜者 +1 补偿(避免引入座位偏好),
// 代价仅是每局最多 winCount-1 金币的有界通缩。测试 activity_emitter_settle_test.go
// 断言余数落在 [0, winCount)。
func computeCoinDelta(winner string, role Role, winCount, loseCount, ante int64) int64 {
	if winner == "draw" || ante <= 0 {
		return 0
	}
	faction := FactionOf(role)
	isWerewolf := faction == FactionWolf
	// v2.0 DEFECT 1: 用 isWolfWinner 替代硬编码 "werewolf" 比对。引擎权威值是 "wolf"。
	isWinner := (isWolfWinner(winner) && isWerewolf) || (winner == "good" && !isWerewolf && faction != FactionUnknown)
	if isWinner {
		if winCount <= 0 {
			return 0
		}
		return ante * loseCount / winCount
	}
	if faction == FactionUnknown {
		// 角色没识别出来:fallback 0（保守,不输不赢）
		return 0
	}
	return -ante
}

// countWinLose 统计本局胜方/败方人数（bot + 人类玩家合计,驱动彩池分配）。
// winner == "draw" 时返回 (0, 0)。
func countWinLose(r *WerewolfRoom, winner string) (winCount, loseCount int) {
	if winner == "draw" || r.State == nil {
		return 0, 0
	}
	for i := range r.State.Players {
		if int(i) >= len(r.State.Roles) {
			break
		}
		role := r.State.Roles[i]
		faction := FactionOf(role)
		if faction == FactionUnknown {
			continue
		}
		isWerewolf := faction == FactionWolf
		// v2.0 DEFECT 1: 引擎权威胜方串是 "wolf"（engine.go:614）,不是 "werewolf"。
		// 用 isWolfWinner 兼容两者,规范值 "wolf"。
		if (isWolfWinner(winner) && isWerewolf) || (winner == "good" && !isWerewolf) {
			winCount++
		} else {
			loseCount++
		}
	}
	return winCount, loseCount
}

// pushBalanceChangeLocked 向单个用户推送 wallet.balance WS 帧。
// nil-safe：无 BalancePusher 注入时跳过。
func (m *WerewolfManager) pushBalanceChangeLocked(userID string, balance, delta int64, reason string) {
	if m.BalancePusher == nil {
		return
	}
	m.BalancePusher(userID, balance, delta, reason)
}

// pushSettlementLocked 向单个用户推送 game.settlement WS 帧(结算明细)。
// nil-safe：无 SettlementPusher 注入时跳过。
func (m *WerewolfManager) pushSettlementLocked(userID string, payload map[string]any) {
	if m.SettlementPusher == nil {
		return
	}
	m.SettlementPusher(userID, payload)
}

// anteCoinFromCfg 取当前配置的底注金额。config 不可用时返回 0（保守：关闭博弈）。
func anteCoinFromCfg() int {
	c := config.Load()
	if c == nil {
		return 0
	}
	return c.Werewolf.AnteCoin
}

// EmitPlayerDied 在 killPlayer(非已知 wolf/sea/witch 事件)时调用,
func (m *WerewolfManager) EmitPlayerDied(r *WerewolfRoom, seat int, cause string) {
	if r == nil {
		return
	}
	text := "💀 " + strconv.Itoa(seat+1) + "号 死亡(" + cause + ")"
	phase := ""
	roundN := 0
	if r.State != nil {
		phase = r.State.Phase.String()
		roundN = r.State.DayNumber
	}
	m.emitActivity(r, ActivityEventKindPlayerDied, text, phase, roundN,
		"warn", "💀", seat, -1, false)
	// §20260811-09 U1 — 死亡事件触发解说(旁观者点评死亡)。
	r.triggerCommentaryEventLocked(wwcommentator.CommentaryPendingDeathAnnounce, map[string]any{
		"seat":  seat,
		"cause": cause,
	})
	// v4 §13.1：死亡若是狼人,清理其在 WolfPackRoom 的留言(协议层隔离)。
	// 此处已在持锁态被 emitActivity 调用方调用,直接 PurgeByDeath。
	if r.wolfPack != nil && seat >= 0 && seat < len(r.State.Roles) &&
		r.State.Roles[seat] == RoleWerewolf {
		r.wolfPack.PurgeByDeath([]int{seat})
		// §20260811-04 U1 — 同款清理死亡狼人的暗号索引。
		if r.wolfPackCipher != nil {
			r.wolfPackCipher.PurgeByDeath([]int{seat})
		}
		// §20260810-10 U1 — 若死者是轮值狼王,立即顺延到下一个存活狼。
		// 本函数在持锁态被调用(emitActivity 调用方),RotateKing 内部
		// 只锁 wolfPack 自身 mu(不反向依赖 r.mu),无 §92a 自死锁风险。
		r.wolfPack.RotateKing()
	}
	// §20260811-07 U1 — 死亡瞬间一次性推送幽灵语音(§119 协议层隔离)。
	// 持锁态追加调用,EmitGhostVoiceLocked 内部做幂等 + redact + §135 身份公开判定。
	m.EmitGhostVoiceLocked(r, seat)
}

// EmitQuarantine 在 setQuarantine 触发时调用。
func (m *WerewolfManager) EmitQuarantine(r *WerewolfRoom, seat int, reason string) {
	if r == nil {
		return
	}
	text := "🚫 Bot " + strconv.Itoa(seat+1) + "号 已被禁用"
	if reason != "" {
		text += "(" + reason + ")"
	}
	m.emitActivity(r, ActivityEventKindQuarantine, text, "", 0,
		"error", "🚫", seat, -1, false)
}

// EmitDeathLyricStart 在遗言阶段开始时调用(首个遗言座位入席)。
// BUG 2026-07-09: 遗言功能 §13。
func (m *WerewolfManager) EmitDeathLyricStart(r *WerewolfRoom, seat int) {
	if r == nil {
		return
	}
	text := "💀 " + strconv.Itoa(seat+1) + "号 开始遗言"
	phase := ""
	roundN := 0
	if r.State != nil {
		phase = r.State.Phase.String()
		roundN = r.State.DayNumber
	}
	m.emitActivity(r, ActivityEventKindDeathLyricStart, text, phase, roundN,
		"info", "💀", seat, -1, false)
}

// EmitDeathLyricSpoken 在遗言 actor 提交遗言后调用。silentForBots=false(所有
// 存活 bot 都能看到遗言内容,供 LLM 推理)。
func (m *WerewolfManager) EmitDeathLyricSpoken(r *WerewolfRoom, seat int, txt string) {
	if r == nil {
		return
	}
	text := "💀 " + strconv.Itoa(seat+1) + "号 遗言"
	if txt != "" {
		text += ":" + txt
	}
	phase := ""
	roundN := 0
	if r.State != nil {
		phase = r.State.Phase.String()
		roundN = r.State.DayNumber
	}
	m.emitActivity(r, ActivityEventKindDeathLyricSpoken, text, phase, roundN,
		"info", "💀", seat, -1, false)
}

// EmitDeathLyricSkipped 在遗言 actor 放弃遗言后调用。
func (m *WerewolfManager) EmitDeathLyricSkipped(r *WerewolfRoom, seat int) {
	if r == nil {
		return
	}
	text := "💀 " + strconv.Itoa(seat+1) + "号 放弃遗言"
	phase := ""
	roundN := 0
	if r.State != nil {
		phase = r.State.Phase.String()
		roundN = r.State.DayNumber
	}
	m.emitActivity(r, ActivityEventKindDeathLyricSkipped, text, phase, roundN,
		"info", "💀", seat, -1, false)
}

// EmitAutoSkip 在 watchdog 派发 skip 时调用。
func (m *WerewolfManager) EmitAutoSkip(r *WerewolfRoom, phase string) {
	if r == nil {
		return
	}
	text := "⏭ 系统自动跳过 " + phaseLabelCN(phase)
	m.emitActivity(r, ActivityEventKindAutoSkip, text, phase, 0,
		"info", "⏭", -1, -1, false)
}

// EmitSheriffAutoSkip BUG-HUNTER2-P0-01 (2026-08-07): 警长竞选阶段被 watchdog
// 兜底时,公开广播「⏭ 警长竞选超时,本局无警长」,避免玩家看到阶段突然切换
// 却无任何交代(原 SheriffElect 无人参选分支仅置 SheriffSeat=NoSeat + 跳
// PhaseSpeak,无任何活动广播)。与 EmitAutoSkip 同 kind (auto_skip) 但
// 文案专用,便于前端 HistoryDrawer 时间轴明确标记「警长空缺」原因。
func (m *WerewolfManager) EmitSheriffAutoSkip(r *WerewolfRoom) {
	if r == nil {
		return
	}
	phase := ""
	roundN := 0
	if r.State != nil {
		phase = r.State.Phase.String()
		roundN = r.State.DayNumber
	}
	text := "⏭ 警长竞选超时,本局无警长"
	m.emitActivity(r, ActivityEventKindAutoSkip, text, phase, roundN,
		"info", "⏭", -1, -1, false)
}

// ─────────────────── phase 中文名映射 ───────────────────

// phaseLabelCN 把英文 phase 翻译成中文标签,Emit* 函数使用。
// 同步 clientweb/src/components/werewolf/phaseLabel.ts 的中文表。
func phaseLabelCN(phase string) string {
	switch phase {
	case "pre_wolves":
		return "狼人阶段(预行动)"
	case "wolves":
		return "狼人行动"
	case "seer":
		return "预言家查验"
	case "witch":
		return "女巫用药"
	case "guard":
		return "守卫守护"
	case "night_guard":
		// §134 守卫守护阶段中文标签(与 engine PhaseNightGuard.String() 对齐)。
		return "守卫守护"
	case "night":
		return "夜晚"
	case "dawn":
		return "天亮结算"
	case "day":
		return "白天发言"
	case "speak":
		return "白天发言"
	case "sheriff_election":
		return "警长竞选"
	case "sheriff":
		// BUG-HUNTER2-P0-01 (2026-08-07): PhaseSheriff.String() = "sheriff"。
		// 原表只覆盖旧别名 "sheriff_election",导致 EmitAutoSkip("sheriff")
		// 渲染成 "⏭ 系统自动跳过 sheriff"(原报告 bug)。补齐为中文标签。
		return "警长竞选"
	case "vote":
		return "投票"
	case "hunter_shoot":
		return "猎人开枪"
	case "pk_speak":
		return "PK 发言"
	case "last_words":
		return "遗言"
	case "gameover":
		return "游戏结束"
	}
	return phase
}

// §20260809-02 U2 Bot 票型回灌 —— 在 finishVoteLocked 末尾调用,聚合本轮
// 投票的「谁投了谁」快照到 GameState.LastDayVoteMap + 广播一条结构化活动事件。
// 同时进 500K 队列(silentForBots=false),让 Agent 在下一轮 LLM 调用时
// 能从 GameContext.LastDayVoteMap + ChatHistory 两条路径同时获得票型。
//
// 设计要点:
//   - 方案 B(聚合后下发)而非方案 A(每条单独广播):13 人局每轮 13 条 → 1 条,
//     队列成本 1/13;同时避免上下文膨胀。
//   - VoteTarget == NoSeat(弃权)不进入 Map;VoteTarget 越界(>12)兜底丢弃。
//   - 人类玩家 UI 不感知(已通过 chat 流看到每条 EmitVoteCast),此函数
//     仅补 Agent 端的盲区。
//   - 必须在 r.mu 持锁态调用(§92a)。
func (m *WerewolfManager) fillDayVoteMapLocked(r *WerewolfRoom) {
	if r == nil || r.State == nil {
		return
	}
	snapshot := make(map[Seat]Seat, MaxPlayers)
	for i := Seat(0); i < Seat(MaxPlayers); i++ {
		// 只统计存活且本轮投过票的座位(死人/弃权票不算)
		if !r.State.AliveSeat(i) {
			continue
		}
		if !r.State.Players[i].Voted {
			continue
		}
		t := r.State.Players[i].VoteTarget
		if t == NoSeat {
			continue // 弃权不进 Map
		}
		if t < 0 || int(t) >= MaxPlayers {
			continue // 越界兜底
		}
		snapshot[i] = t
	}
	r.State.LastDayVoteMap = snapshot

	// 2026-08-11 §20260811-02 U1 — 票型刚落地即重算影响力(跟票率信号此刻最新鲜)。
	// 本函数已持 r.mu,故直接调 *Locked 变体(§92a)。
	r.RecalculateInfluenceLocked()

	// 聚合广播一条结构化活动事件(让 Agent 从 ChatHistory 也可见)。
	if len(snapshot) > 0 {
		// 形如 "🗳 票型: 1→3, 2→3, 4→5 …"
		type pair struct {
			from, to Seat
		}
		pairs := make([]pair, 0, len(snapshot))
		for f, t := range snapshot {
			pairs = append(pairs, pair{from: f, to: t})
		}
		// 按 voter seat 升序输出,稳定可读。
		for i := 0; i < len(pairs); i++ {
			for j := i + 1; j < len(pairs); j++ {
				if pairs[j].from < pairs[i].from {
					pairs[i], pairs[j] = pairs[j], pairs[i]
				}
			}
		}
		text := "🗳 票型:"
		for i, p := range pairs {
			if i > 0 {
				text += ", "
			}
			text += " " + strconv.Itoa(int(p.from)+1) + "→" + strconv.Itoa(int(p.to)+1)
		}
		phase := ""
		roundN := 0
		if r.State != nil {
			phase = r.State.Phase.String()
			roundN = r.State.DayNumber
		}
		// silentForBots=false —— 这是关键:让 Agent 收到这条结构化票型。
		m.emitActivity(r, ActivityEventKindVoteCast, text, phase, roundN,
			"info", "🗳", -1, -1, false)
	}
}
