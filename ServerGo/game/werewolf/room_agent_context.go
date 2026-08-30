package werewolf

// room_agent_context.go — §4 单文件 ≤1800 行治理:room_agent.go 的
// GameContext 构建器(分层缓存 + winConditionFor + buildGodRolePoolLocked +
// buildAgentContextLocked)整段搬移(2026-08-30 §20260830-01 同批纯代码搬移;
// 零逻辑改动,函数体逐字节保留)。
// buildAgentContextLocked 是全部玩家 Agent 的上下文单一入口;§20260830-01 §6.1
// 的 RevealRoleOnDeath / RevealedRoles 填充点(TODO 接线)在本文件的
// buildAgentContextLocked 末尾 —— 数据由 death_reveal.go revealedRolesSnapshotLocked 备好。

import (
	"context"
	"fmt"
	"strings"
	"time"

	"LsmAgentGame/agent/wwtypes"
	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// ─── 2026-08-13 §20260813-01 U2 — GameContext 分层缓存 ───
//
// getStaticContext 获取 seat 的静态上下文缓存。未命中时调用 builder 构建并缓存。
// 静态上下文一局只构建一次(座位/角色/玩家列表等整局不变信息)。
func getStaticContext(r *WerewolfRoom, seat int, builder func() *wwtypes.StaticContext) *wwtypes.StaticContext {
	if r.staticContextCache == nil {
		r.staticContextCache = make(map[int]*wwtypes.StaticContext)
	}
	if sc, ok := r.staticContextCache[seat]; ok {
		return sc
	}
	sc := builder()
	r.staticContextCache[seat] = sc
	return sc
}

// getPhaseStateContext 获取 seat 的阶段状态缓存。phase 变化时自动失效重建。
// 阶段状态在阶段内不变(警长/屠边计数等),阶段切换时重建。
func getPhaseStateContext(r *WerewolfRoom, seat int, currentPhase string, builder func() *wwtypes.PhaseStateContext) *wwtypes.PhaseStateContext {
	if r.phaseStatePhase != currentPhase {
		// 阶段变化,失效旧缓存
		r.phaseStateCache = make(map[int]*wwtypes.PhaseStateContext)
		r.phaseStatePhase = currentPhase
	}
	if r.phaseStateCache == nil {
		r.phaseStateCache = make(map[int]*wwtypes.PhaseStateContext)
	}
	if psc, ok := r.phaseStateCache[seat]; ok {
		return psc
	}
	psc := builder()
	r.phaseStateCache[seat] = psc
	return psc
}

// invalidateContextCaches 失效所有上下文缓存(游戏重开时调用)。
func invalidateContextCaches(r *WerewolfRoom) {
	r.staticContextCache = make(map[int]*wwtypes.StaticContext)
	r.phaseStateCache = make(map[int]*wwtypes.PhaseStateContext)
	r.phaseStatePhase = ""
}

// winConditionFor 返回角色+阵营对应的胜利条件描述。
func winConditionFor(role Role, faction Faction) string {
	switch faction {
	case FactionWolf:
		return "狼人屠边(杀光全部神职或杀光全部平民)"
	case FactionGood:
		if role == RoleWerewolf {
			return "放逐全部 4 狼人(你被发到狼人身份但阵营为好人,这是异常状态)"
		}
		return "放逐全部 4 狼人"
	default:
		return "存活到最后"
	}
}

// buildGodRolePoolLocked 返回本局实际发牌的神职池(中文名称列表)。
// 从 GameState.Roles 中提取所有实际发牌的神职角色(去重),
// 转换为中文名称供 Agent prompt 使用。
func buildGodRolePoolLocked(r *WerewolfRoom) []string {
	if r.State == nil {
		return nil
	}
	// 角色名映射(英文 → 中文)
	roleNameCN := map[string]string{
		"seer":         "预言家",
		"witch":        "女巫",
		"hunter":       "猎人",
		"idiot":        "白痴",
		"guard":        "守卫",
		"knight":       "骑士",
		"demon_hunter": "猎魔人",
	}
	seen := make(map[string]bool)
	var pool []string
	for i := 0; i < r.State.SeatCount; i++ {
		role := r.State.Roles[i]
		if !IsGodRole(role) {
			continue
		}
		name := role.String()
		if seen[name] {
			continue
		}
		seen[name] = true
		if cn, ok := roleNameCN[name]; ok {
			pool = append(pool, cn)
		} else {
			pool = append(pool, name)
		}
	}
	return pool
}

func buildAgentContextLocked(r *WerewolfRoom, seat int, driverSeat int) wwtypes.GameContext {
	if r.State == nil || seat < 0 || seat >= MaxPlayers {
		return wwtypes.GameContext{}
	}
	gs := r.State
	gc := wwtypes.GameContext{
		Round:           gs.DayNumber,
		Phase:           gs.Phase.String(),
		Role:            gs.Roles[seat].String(),
		MySeat:          seat, // BUG-WEREWOLF-P0-NEW-10: prompt uses seat+1 as the 1-indexed 玩家编号
		SpeakTurn:       -1,
		MySeerCheck:     -1,
		WolfTarget:      -1,
		SuicideTakeSeat: -1, // §20260830-02 — 仅 suicide_take 阶段 >= 0
		IsDriver:        seat == driverSeat,
		GameStartedAt:   r.gameStartedAt,
		// 2026-07-10 §13: 本局实际人数(13/12/7),prompt.go 据此选择对应规则摘要渲染。
		SeatCount: gs.SeatCount,
	}

	// 2026-08-13 §20260813-01 U2 — 分层缓存:静态层一局构建一次,
	// 阶段层阶段切换时重建。减少每轮重复计算 ~3-5KB。
	gc.Static = getStaticContext(r, seat, func() *wwtypes.StaticContext {
		return &wwtypes.StaticContext{
			SeatCount:    gs.SeatCount,
			MySeat:       seat,
			Role:         gs.Roles[seat].String(),
			Faction:      FactionOf(gs.Roles[seat]).String(),
			WinCondition: winConditionFor(gs.Roles[seat], FactionOf(gs.Roles[seat])),
			AllPlayers:   buildAllPlayersLocked(r),
			GodRolePool:  buildGodRolePoolLocked(r),
		}
	})
	gc.PhaseState = getPhaseStateContext(r, seat, gs.Phase.String(), func() *wwtypes.PhaseStateContext {
		cands := gs.SheriffCandidates()
		sc := &wwtypes.PhaseStateContext{
			Phase:              gs.Phase.String(),
			SheriffSeat:        int(gs.SheriffSeat),
			SheriffStream:      [2]int{int(gs.SheriffStreams[0]), int(gs.SheriffStreams[1])},
			IdiotRevealedSeats: gs.idiotRevealedSeats(),
			DivineCnt:          gs.DivineCnt,
			PlainCnt:           gs.PlainCnt,
			WolfAliveCnt:       gs.WolfAliveCnt,
			VoteProposed:       gs.VoteProposed,
			VoteProposer:       int(gs.VoteProposer),
		}
		if len(cands) > 0 {
			sc.SheriffCandidates = make([]int, len(cands))
			for i, s := range cands {
				sc.SheriffCandidates[i] = int(s)
			}
		}
		return sc
	})
	if gs.SpeakTurnSeat != NoSeat {
		gc.SpeakTurn = int(gs.SpeakTurnSeat)
	}
	for i := 0; i < MaxPlayers; i++ {
		if gs.AliveSeat(Seat(i)) {
			gc.AliveSeats = append(gc.AliveSeats, i)
		}
	}
	for _, d := range gs.LastNightDeaths {
		gc.LastNightDeaths = append(gc.LastNightDeaths, int(d))
	}

	// BUG: 狼人杀 7 人局 Agent 多轮上下文 — feed the room transcript
	// (recentSpeeches / whisperInbox) into the per-agent GameContext. Each
	// agent sees the same public speech history; whisperInbox is per-seat
	// so a wolf's whispered plan is only visible to the recipient.
	if len(r.recentSpeeches) > 0 {
		// Defensive copy: the underlying slice is mutated in place as new
		// messages arrive, and the prompt builder may read it later.
		gc.RecentSpeeches = make([]wwtypes.SpeechEvent, len(r.recentSpeeches))
		copy(gc.RecentSpeeches, r.recentSpeeches)
	}
	if inbox, ok := r.whisperInbox[seat]; ok && len(inbox) > 0 {
		gc.WhisperInbox = make([]wwtypes.WhisperEvent, len(inbox))
		copy(gc.WhisperInbox, inbox)
	}
	// Role-private info.
	switch gs.Roles[seat] {
	case RoleSeer:
		if gs.Players[seat].LastSeerCheck != NoSeat {
			gc.MySeerCheck = int(gs.Players[seat].LastSeerCheck)
			// 2026-08-12 §20260812-04 U1 (P0-1) — 补算查验结果阵营。
			// 缺陷:此前只填座位号,LLM 拿到「我查了 4 号」却不知道结果,
			// 等于技能失效。人类玩家走 BuildSeerInform 一直能看到阵营。
			if FactionOf(gs.Roles[gs.Players[seat].LastSeerCheck]) == FactionWolf {
				gc.MySeerCheckFaction = "wolf"
			} else {
				gc.MySeerCheckFaction = "good"
			}
		}
		// 全量查验历史:预言家的核心价值是这张表(单看上一晚会丢失前几轮金水/查杀)。
		// 复用 InformationLedger 单一事实来源(§20260810-05/08),不新增镜像字段。
		gc.MySeerCheckHistory = r.buildSeerCheckHistoryLocked(seat)
	case RoleWitch:
		if gs.WolfKillTarget != NoSeat {
			gc.WolfTarget = int(gs.WolfKillTarget)
		}
		// §20260812-04 U1 — 女巫药剂剩余状态(此前仅由 tool enum 隐式表达)。
		gc.WitchAntidoteUsed = gs.Players[seat].WitchAntidoteUsed
		gc.WitchPoisonUsed = gs.Players[seat].WitchPoisonUsed
	case RoleGuard:
		// §134 守卫 GameContext:仅填 GuardLastProtect(让 LLM 知道 G1 连守约束);
		// 绝对不填 WolfTarget —— "盲守"语义:守卫看不到狼刀目标。
		gc.GuardLastProtect = int(gs.GuardLastProtect)
	case RoleWerewolf:
		// 2026-07-17: 狼人投票快照(仅 night_wolves 阶段)
		if gs.Phase == PhaseNightWolves {
			votes := map[int]int{}
			reasons := map[int]string{} // §20260810-04 U2 — 刀人理由快照
			cast := 0
			total := 0
			for i := 0; i < MaxPlayers; i++ {
				if gs.AliveSeat(Seat(i)) && gs.Roles[i] == RoleWerewolf {
					total++
					if gs.WolfVoteCast[i] {
						cast++
						if gs.WolfVotes[i] != NoSeat {
							votes[i] = int(gs.WolfVotes[i])
							if gs.WolfVoteReasons[i] != "" {
								reasons[i] = gs.WolfVoteReasons[i]
							}
						}
					}
				}
			}
			gc.WolfVotes = votes
			gc.WolfVoteReasons = reasons
			gc.WolfVotesCast = cast
			gc.WolfTotalWolves = total
			gc.WolfVoting = gs.WolfKillTarget == NoSeat
			// 2026-07-24 R196-P1: 把"我是否已投票"显式注入 bot 上下文,
			// 让 prompt 渲染时 LLM 看到自己已投票,不再循环调 wolf_kill。
			if gs.Roles[seat] == RoleWerewolf {
				gc.MyWolfVoteCast = gs.WolfVoteCast[seat]
			}
			if gs.WolfVoteTally != nil {
				gc.WolfVoteTally = &wwtypes.WolfVoteTally{
					Counts: gs.WolfVoteTally.Counts,
					Tied:   gs.WolfVoteTally.Tied,
					Reason: gs.WolfVoteTally.Reason,
					Final:  gs.WolfVoteTally.Final,
				}
			}
		}
	}

	// §20260809-02 U2 Bot 票型回灌 —— 把上一轮白天投票的「谁投了谁」
	// 快照注入 GameContext(供 BuildUserPrompt 渲染)。仅在非 night 阶段
	// 注入(狼人杀夜间不需要看白天票型,反而会污染推理);nil-guard 兜底。
	if gs.LastDayVoteMap != nil && len(gs.LastDayVoteMap) > 0 &&
		gs.Phase != PhaseNightWolves && gs.Phase != PhaseNightGuard &&
		gs.Phase != PhaseNightSeer && gs.Phase != PhaseNightWitch &&
		gs.Phase != PhaseNightDemonHunter {
		voteMap := make(map[int]int, len(gs.LastDayVoteMap))
		for f, t := range gs.LastDayVoteMap {
			voteMap[int(f)] = int(t)
		}
		gc.LastDayVoteMap = voteMap
	}

	alive := gs.AliveSeat(Seat(seat))
	switch gs.Phase {
	case PhasePreWolves:
		// BUG 2026-07-08: 缓冲期所有存活玩家都"可以说话",但严格说
		// 并不是"轮到他行动"——所有玩家可同时发 speak / interject / whisper。
		// 把 MyTurn 设为 false,prompt.go 会走"非发言轮"分支(不显示
		// 【现在轮到你行动】),改由驱动在 remaining 字段提示。
		gc.MyTurn = false
	case PhaseNightWolves:
		// 2026-07-17: 所有存活狼人都可以投票(未投票者 MyTurn=true)
		gc.MyTurn = alive && gs.Roles[seat] == RoleWerewolf && !gs.WolfVoteCast[seat]
	case PhaseNightGuard:
		// §134 守卫守护阶段:仅存活守卫 MyTurn=true。
		gc.MyTurn = alive && Seat(seat) == gs.GuardSeat
	case PhaseNightSeer, PhaseNightWitch:
		gc.MyTurn = alive && gs.TurnActingSeat == Seat(seat)
	case PhaseDawn:
		gc.MyTurn = alive && seat == driverSeat
	case PhaseSheriff:
		// BUG-08: 复述段落已压缩 — git blame 与 docs/ 索引可还原

		gc.MyTurn = alive && (!gs.Players[seat].HasSpoken || !gs.Players[seat].Voted || seat == driverSeat)
	case PhaseSpeak:
		gc.MyTurn = alive && gs.SpeakTurnSeat == Seat(seat)
	case PhaseVote:
		// BUG-R193-001: 同步 quarantine 状态,让 AllVoted(拼入 LLM prompt)与
		// MyTurn(驱动派发)都感知 quarantined bot。buildAgentContextLocked 是
		// 所有 wake 路径的公共入口,在此处单次同步成本可忽略。
		syncQuarantinedLocked(r)
		gc.MyVoted = gs.Players[seat].Voted
		gc.AllVoted = gs.allActiveVoted()
		// Non-driver votes once then yields; the driver keeps MyTurn so it
		// can call finish_vote once everyone has voted.
		// 注意: 此处 MyTurn 不禁用 quarantined bot —— 禁用的 bot 仍需 MyTurn=true
		// 以触发 tryDispatchQuarantinedActingSkip 派发 vote_skip(弃权),让阶段能
		// 推进。quarantined bot 的 MyTurn 由 PushEvent → IsQuarantined guard 短路,
		// 不会真正进入 LLM 调用。allActiveVoted() 已排除 quarantined 座位,故一旦
		// 所有活跃玩家投票,DayVote/dayVoteLocked 立即 auto-tally,触发阶段推进。
		gc.MyTurn = alive && (!gc.MyVoted || seat == driverSeat)
	case PhaseHunterShoot:
		// BUG-WEREWOLF-P0-2 (R42): hunter dies THEN shoots — the
		// ability triggers on death, so the hunter is already
		// dead (Alive=false) when PhaseHunterShoot starts. The
		// `alive` guard previously blocked MyTurn for the dead
		// hunter, leaving the room permanently stuck with no
		// acting seat and watchdogActingSeat returning -1.
		gc.MyTurn = gs.Roles[seat] == RoleHunter && gs.HunterPendingShoot
		// §20260830-02 — 死者行动白名单:run.go 死者守卫据 DeadActorTurn
		// 放行 LLM 调用(此前 MyTurn 修了、守卫没修,死亡 Agent 猎人
		// 仍永远开不出枪)。
		gc.DeadActorTurn = gc.MyTurn
	case PhaseDeathLyric:
		// BUG 2026-07-09: 遗言阶段。仅当前遗言座位(已是死者)可发遗言;
		// alive=false 不再 §86 acting-bot 逻辑,改用权威字段 DeathLyricCurrent。
		gc.MyTurn = Seat(seat) == gs.DeathLyricCurrent
		gc.DeathLyricCurrent = int(gs.DeathLyricCurrent)
		// §20260830-02 — 死者行动白名单(遗言当前座位放行)。
		gc.DeadActorTurn = gc.MyTurn
	case PhaseSuicideTake:
		// §20260830-02 — 自爆带走:仅自爆狼(已死)可行动。与遗言/猎枪
		// 同为「死亡触发的合法行动」,走死者行动白名单放行。
		gc.MyTurn = Seat(seat) == gs.SuicidedWolfSeat
		gc.SuicideTakeSeat = int(gs.SuicidedWolfSeat)
		gc.DeadActorTurn = gc.MyTurn
	case PhaseRestartVote:
		// 2026-07-10: 重开局投票阶段。每个有资格的入座座位都可以调
		// restart_vote (即使已死),因此 MyTurn=true 让 BuildUserPrompt
		// 渲染"现在轮到你行动"分支;具体工具暴露由 BuildTools("restart_vote",…)
		// 完成。
		gc.MyTurn = r.Seats[seat] != ""
		gc.LastWinner = gs.Winner
		if !gs.PhaseDeadlineAt.IsZero() {
			rem := time.Until(gs.PhaseDeadlineAt)
			if rem < 0 {
				rem = 0
			}
			gc.RestartVoteRemainingSec = int(rem.Seconds())
		}
		gc.RestartVoteDecided = gs.RestartVoteDone
		gc.RestartVoteResult = gs.RestartVoteResult
	case PhaseIdiotReveal:
		// 2026-07-10: 白痴翻牌阶段。仅最高票白痴(DayEliminated)可行动;
		// 翻牌或放弃后阶段即结束,故不全天 MyTurn。
		gc.MyTurn = Seat(seat) == gs.DayEliminated
	case PhaseGameOver, PhaseFilling:
		gc.MyTurn = false
	}

	// BUG 2026-07-08: 填充每玩家档案表(AllPlayers),让 LLM 每轮都看到
	// 完整的座位→(ID,昵称,AgentName)映射。座位顺序稳定(0..6),真人
	// 玩家昵称从 r.recentSpeeches 最近的同 seat speech 中取;bot 玩家
	// 昵称统一为 "Bot N号" + AgentName 来自 r.seatModelKeys(由 manager
	// 启动 Agent 时写入)。空座位显示 "(空)"。
	// 2026-08-13 §20260813-01 U2: 从静态缓存读取,避免每轮重复构建。
	gc.AllPlayers = gc.Static.AllPlayers

	// BUG 2026-07-08: 缓冲期剩余秒数,仅在 pre_wolves 阶段 > 0。
	if gs.Phase == PhasePreWolves && !gs.FirstNightGraceEnd.IsZero() {
		rem := time.Until(gs.FirstNightGraceEnd)
		if rem < 0 {
			rem = 0
		}
		gc.GraceRemainingSec = int(rem.Seconds())
		// BUG Round 40 §95: 强制发言进度字段。
		gc.PreWolvesRound = gs.PreWolvesSpeakRound
		gc.PreWolvesRoundsTotal = gs.PreWolvesSpeakRoundsPerPlayer
		gc.PreWolvesCountForMySeat = gs.PreWolvesSpeakCount[seat]
	}

	// 2026-07-09 §13-bugfix 改造 — 由 r.chatQueues[seat].Snapshot() 改为
	// r.chatQueue.WindowFor(seat)。WindowFor 返回自该 bot read pointer
	// 之后的所有新增消息(公平性保证);防御性 copy 由 Queue 内部完成,
	// 锁顺序 r.mu → q.mu 不反向,持锁调用是安全的。
	if r.chatQueue != nil {
		gc.ChatHistory = r.chatQueue.WindowFor(int(seat))
	}

	// 2026-07-10 §120 增强 — 模型响应速率公平性字段填充。
	// 1. 当前 bot 自己的耗时统计:从 r.BotAgents[seat] 取得 Agent,调 getter。
	//    Agent 在启动时由 StartAgentsLocked 注入 r.BotAgents 映射;
	//    nil-safe:human seat 没有 Agent → 跳过(真人玩家不需要这个块)。
	if botAgent, ok := r.BotAgents[seat]; ok && botAgent != nil {
		gc.ModelName = botAgent.ModelKey // 真实 key(例 "DeepSeek-model");AgentName 由前端格式化
		gc.MyAvgLLMLatencyMs = botAgent.AvgLLMLatencyMs()
		gc.MyLastLLMLatencyMs = botAgent.LastLLMLatencyMs()
		gc.MyTotalLLMCalls = botAgent.TotalLLMCalls()
	}
	// 2. 扫描所有 bot 计算房间最快/最慢模型(基于每个 bot 累计 ≥1 次的 LLM 调用)。
	//    跳过 0 平均耗时(尚未调过 LLM)的 bot。O(7) 遍历,锁内开销可忽略。
	{
		var fastestMs, slowestMs int64
		var fastestName, slowestName string
		// 初始化:首次遍历赋值,后续比较。
		for _, ba := range r.BotAgents {
			if ba == nil || ba.Seat < 0 {
				continue
			}
			avg := ba.AvgLLMLatencyMs()
			if avg <= 0 {
				continue
			}
			if fastestMs == 0 || avg < fastestMs {
				fastestMs = avg
				fastestName = ba.ModelKey
			}
			if slowestMs == 0 || avg > slowestMs {
				slowestMs = avg
				slowestName = ba.ModelKey
			}
		}
		gc.RoomFastestModel = fastestName
		gc.RoomFastestLatencyMs = fastestMs
		gc.RoomSlowestModel = slowestName
		gc.RoomSlowestLatencyMs = slowestMs
	}
	// 3. 是否房间里有真人玩家(非观战者):遍历 r.Seats 看是否有非 bot 占座
	//    的真人 userID(由 StartAgentsLocked 时填充的 BotAgents / botSeats 区分)。
	//    真人座位不在 BotAgents 映射中,简单遍历即可。
	for i := 0; i < MaxPlayers; i++ {
		if r.Seats[i] == "" {
			continue
		}
		if _, isBot := r.BotAgents[i]; !isBot {
			gc.IsHumanInRoom = true
			break
		}
	}
	// 2026-07-24 优化:下发房间暂停状态。Agent handleEvent 入口据此跳过 LLM,
	// 让真人玩家在 UI ⏸ 暂停时不消耗 LLM 配额 / 防批量 quarantine。
	gc.RoomPaused = r.paused

	// 2026-07-10: 12 人标准竞技局字段注入。
	// 2026-08-13 §20260813-01 U2: 从阶段状态缓存读取,避免每轮重复计算。
	gc.SheriffSeat = gc.PhaseState.SheriffSeat
	gc.SheriffStream = gc.PhaseState.SheriffStream
	gc.SheriffCandidates = gc.PhaseState.SheriffCandidates
	gc.IdiotRevealedSeats = gc.PhaseState.IdiotRevealedSeats
	gc.DivineCnt = gc.PhaseState.DivineCnt
	gc.PlainCnt = gc.PhaseState.PlainCnt
	gc.WolfAliveCnt = gc.PhaseState.WolfAliveCnt
	gc.VoteProposed = gc.PhaseState.VoteProposed
	gc.VoteProposer = gc.PhaseState.VoteProposer
	// MyCandidate 是 per-seat 字段,不在阶段状态缓存中。
	if gs.Phase == PhaseSheriff {
		gc.MyCandidate = gs.Players[seat].HasSpoken
	}

	// 2026-07-10 §124 增强 — 情绪字段填充。
	// 1. 当前 bot 自己的情绪:从 r.BotAgents[seat] 取得 Agent,调 getter。
	//    nil-safe:human seat 没有 Agent → 跳过(真人玩家不需要这个块)。
	if botAgent, ok := r.BotAgents[seat]; ok && botAgent != nil {
		gc.MyEmotion = botAgent.CurrentEmotion()
		gc.MyEmotionReason = botAgent.CurrentEmotionReason()
	}
	// 2. 其它 Agent 的情绪:遍历 r.BotAgents,跳过自己与 nil。
	for s, ba := range r.BotAgents {
		if s == seat || ba == nil {
			continue
		}
		gc.OthersEmotion = append(gc.OthersEmotion, wwtypes.SeatEmotionBrief{
			Seat:    s,
			Emotion: ba.CurrentEmotion(),
			// BUG-R238-P0-1 (2026-08-04): Reason 是 LLM 自由文本,可能包含身份自述
			// (如「继续隐藏预言家身份」)。emotion key 是封闭枚举、公开合理,但 reason
			// 不是 —— 填 "" 阻断 agent→agent 身份泄露潜伏路径
			// (对齐 Fix A 的人类侧脱敏;§130「声明了却从不接线」模式)。
			Reason:    "",
			UpdatedAt: ba.EmotionUpdatedAtMs(),
		})
	}

	// 2026-07-12 §127 — 阶段剩余秒数,从 gs.PhaseDeadlineAt 计算供 BuildUserPrompt 注入紧迫感提示。
	if !gs.PhaseDeadlineAt.IsZero() {
		rem := time.Until(gs.PhaseDeadlineAt)
		if rem < 0 {
			rem = 0
		}
		gc.PhaseDeadlineRemainingSec = int(rem.Seconds())
	}

	// 2026-07-21 道具系统 v2 — 填充 GameContext 道具字段（对齐设计文档 §4.1/§7）。
	// · speak 阶段:把可购买道具快照注入 gc.PropSnapshot，驱动 use_prop 工具 schema 动态生成。
	// · 消费 propInjectQueue：把命中者的注入文本 + 干扰信号落地到 GameContext。
	// · 填充个人/全局道具预算 + 冷却状态（供 PropUserPromptBlock 渲染决策辅助）。
	// 注意：gc.EffectXxx 干扰信号是"每轮 transient"——本轮已被消费，必须清零后再应用，
	// 避免上一轮命中信号跨轮残留（v2 防御性重置）。
	if r.propCatalog != nil {
		// personal budget used（全局 / 个人）
		gc.PropSeatBudgetUsed = r.propSeatBudgetUsedLocked(seat)
		gc.RoomPropBudget = r.roomPropBudget()
		gc.RoomPropBudgetUsed = r.roomPropBudgetUsed
		// 冷却剩余秒数
		gc.PropCooldownRemainingSec = r.propCooldownRemainLocked(seat, defaultPropCooldownSec(r))
		// 个人已用 / 上限（默认上限 3，与 defaultProps 对齐）
		gc.PropUsedThisGame = r.propCountForSeatLocked(seat)
		gc.PropMaxPerGame = defaultPropMaxPerGame(r)

		// 干扰信号防御性重置（命中效果仅存活 1 轮，本轮已被上一轮下面消费过）。
		// 2026-08-07 §20260807-04 P2-2:重置前把上一轮被击中的道具 key 转存到
		// r.lastPropHitEffect[seat],下一轮填入 gc.PropHitLastRound 形成反馈闭环。
		gc.EffectExpose = false
		gc.EffectAttentionScatter = false
		gc.ToolUseMaxOverride = 0
		gc.EffectTargetTwistSeat = -1
		gc.EffectForceEmotion = ""
		gc.HumanDebuff = nil
		if r.lastPropHitEffect != nil {
			gc.PropHitLastRound = r.lastPropHitEffect[seat]
		}
		// 本座位的 lastPropHitEffect 在本轮消费后清空(下一轮若未命中即不再提示)。
		if r.lastPropHitEffect == nil {
			r.lastPropHitEffect = make(map[int]string)
		} else {
			delete(r.lastPropHitEffect, seat)
		}

		// 消费注入队列，应用本轮命中效果 + 注入文本。
		if entries := r.drainPropInjectQueueLocked(seat); len(entries) > 0 {
			var injectBuf strings.Builder
			// 合并多个命中效果（任意一个命中即应用其效果；注入文本拼接）。
			for _, entry := range entries {
				if !entry.Hit {
					continue
				}
				if entry.InjectText != "" {
					injectBuf.WriteString(entry.InjectText)
					injectBuf.WriteString("\n")
				}
				// 落地干扰信号到 GameContext（EffectRegistry）。
				// effs 作用域提升到本轮 entry 循环，便于下方 last-effect 文案引用。
				effs := entry.ParseEffectTypes()
				if len(effs) > 0 {
					ApplyEffects(&gc, seat, entry, EffectApplyContext{
						Room: r, Entry: entry, FromSeat: entry.FromSeat,
					})
				}
				gc.PropLastEffect = fmt.Sprintf("%d号对%d号使用了%s(%s)",
					entry.FromSeat+1, seat+1, entry.PropKey, strings.Join(effs, "/"))
				// 2026-08-07 §20260807-04 P2-2:记录本轮被击中的道具 key,供下一轮
				// PropHitLastRound 渲染「上一轮被击中」提示。
				r.lastPropHitEffect[seat] = propHitSummary(entry.PropKey)
			}
			if injectBuf.Len() > 0 {
				gc.PropInjectText = injectBuf.String()
			}
		}

		// speak 阶段：生成可购买道具快照（动态生成 use_prop tool schema）。
		// 过滤：已达个人上限 / 冷却中 / 超全局预算 → 不出现在 snapshot。
		if gc.Phase == "PhaseSpeak" || gc.Phase == "speak" {
			gc.PropSnapshot = buildPropSnapshotLocked(r, gc)
		}

		// v4 §13.2 — 经济档位：按房间总金币存量动态判定(供 EconTierFeedbackBlock 渲染)。
		gc.EconTier = string(ComputeEconTier(r.roomTotalCoin()))

		// v4 §13.1 — 狼小队交流通道快照：仅狼 bot 注入 WolfPackSnapshot + Faction + WolfTeammateSeat。
		// wolf_whisper 工具在 BuildTools 阶段按 faction+WolfTeammateSeat 条件挂载。
		if seat >= 0 && seat < len(r.State.Roles) {
			if f := FactionOf(r.State.Roles[seat]); f != FactionUnknown {
				gc.Faction = f.String()
			}
			if r.State.Roles[seat] == RoleWerewolf {
				if ag, ok := r.BotAgents[seat]; ok && ag != nil {
					gc.WolfTeammateSeats = ag.WolfTeammateSeats
				}
				if r.wolfPack != nil {
					if rawMsgs := r.wolfPack.Snapshot(WolfPackSnapshotMax); len(rawMsgs) > 0 {
						snap := make([]wwtypes.WolfPackMsg, 0, len(rawMsgs))
						for _, m := range rawMsgs {
							snap = append(snap, wwtypes.WolfPackMsg{
								FromSeat:  m.FromSeat,
								Text:      m.Text,
								CreatedAt: m.CreatedAt.Unix(),
							})
						}
						gc.WolfPackSnapshot = snap
					}
					// §20260810-10 U1 — 战术分工 + 轮值狼王快照(仅狼 bot 可见)。
					roleTable, kingSeat := r.wolfPack.RoleSnapshot()
					if len(roleTable) > 0 {
						gc.WolfPackRoleTable = roleTable
						gc.WolfPackRole = roleTable[seat]
						gc.WolfKingSeat = kingSeat
					}
					// §20260811-04 U1 — 暗号系统快照(仅狼 bot 可见,§119 协议层隔离)。
					if r.wolfPackCipher != nil {
						bundle := r.wolfPackCipher.Get(seat, r.State.DayNumber)
						if len(bundle.Templates) > 0 {
							spec := CipherBundleToAgentSpec(bundle)
							gc.WolfPackCipher = &spec
						}
					}
				}
			}
		}

		// v3 §G3 增强 — 把 bot 金币余额 + 单局底注写入 GameContext,
		// 让 WalletSustainabilityBlock 能渲染"可承受局数 + 紧急度"。
		// 失败 / 玩家非 bot → 余额=0,WalletSustainabilityBlock 自然不渲染。
		if userID := r.Seats[seat]; userID != "" {
			if walletSvc := mgrPropEngineWalletSvc(r); walletSvc != nil {
				if bal, balErr := walletSvc.GetBalance(context.Background(), userID); balErr == nil {
					gc.WalletBalance = bal
				}
			}
		}
		gc.AnteAmount = werewolfAnteAmountLocked(r)

		// v3 §G5 — 把最近 20 条道具使用公开历史写入 GameContext,
		// 供 prop_history 工具查询（环形 buffer 由 room 维护）。
		// 类型转换 werewolf.PropHistoryRecord → wwtypes.PropHistoryRecord（字段同形）。
		if rawHist := r.GetPropHistoryLocked(20); len(rawHist) > 0 {
			snapHist := make([]wwtypes.PropHistoryRecord, 0, len(rawHist))
			for _, h := range rawHist {
				snapHist = append(snapHist, wwtypes.PropHistoryRecord{
					FromSeat:   h.FromSeat,
					ToSeat:     h.ToSeat,
					PropKey:    h.PropKey,
					PropNameZh: h.PropNameZh,
					Hit:        h.Hit,
					EffectHint: h.EffectHint,
					Phase:      h.Phase,
					Round:      h.Round,
					CreatedAt:  h.CreatedAt,
				})
			}
			gc.PropHistorySnapshot = snapHist
		}
	}

	// R176 P2 补缺(v4 链式效果):tick 调度表,把已到期的延迟 step 应用到本座位的 gc。
	// 单调用方构建的 gc 在持锁期内不会跨座位共享,所以这里仅 tick 本座位。
	if len(r.propEffectSchedule) > 0 {
		r.propEffectRoundCounter++
		for key, item := range r.propEffectSchedule {
			if item.TargetSeat != seat {
				continue
			}
			elapsed := r.propEffectRoundCounter - item.CreatedAtCall
			if elapsed < item.DueAfterCalls {
				continue
			}
			if !evaluatePropStepCondition(item.Step.Condition, &gc, seat) {
				delete(r.propEffectSchedule, key)
				continue
			}
			ApplyEffects(&gc, seat, PropInjectEntry{
				PropKey:     item.PropKey,
				FromSeat:    item.FromSeat,
				EffectTypes: item.Step.EffectType,
				TwistSeat:   -1,
				Hit:         true,
			}, EffectApplyContext{FromSeat: item.FromSeat, Entry: PropInjectEntry{
				PropKey:     item.PropKey,
				EffectTypes: item.Step.EffectType,
				TwistSeat:   -1,
			}})
			delete(r.propEffectSchedule, key)
		}
	}

	// 2026-08-10 §20260810-06 — 注入承诺数据到 GameContext。
	// MyCommitments: 本 bot 自己的承诺（含真实状态）。
	// PublicCommitments: 公开可见的他人承诺（仅 pending 状态）。
	if r.commitmentLedger != nil {
		myComms := r.commitmentLedger.GetCommitmentsForViewerLocked(seat)
		publicComms := r.commitmentLedger.GetCommitmentsForViewerLocked(-2) // -2 = 他人 pending 视图
		// 分离自己的承诺和他人的公开承诺
		for _, c := range myComms {
			if c.Seat == seat {
				gc.MyCommitments = append(gc.MyCommitments, wwtypes.CommitmentInfo{
					ID:        c.ID,
					Round:     c.Round,
					Template:  string(c.Template),
					ParamSeat: c.ParamSeat,
					Reason:    c.Reason,
					Status:    string(c.Status),
				})
			}
		}
		for _, c := range publicComms {
			if c.Seat != seat {
				gc.PublicCommitments = append(gc.PublicCommitments, wwtypes.CommitmentInfo{
					ID:        c.ID,
					Round:     c.Round,
					Template:  string(c.Template),
					ParamSeat: c.ParamSeat,
					Reason:    c.Reason,
					Status:    string(c.Status),
				})
			}
		}
	}

	// 2026-08-10 §20260810-08 — 信息账本二期行为侧消费。
	// 锁内从单一账本聚合本 seat 的知情清单；只注入 GameContext→prompt，
	// 不进入 chat_message / chat_history / BotTranscript（§119）。
	if digest := r.ledgerLocked().DigestForSeat(seat, 2); digest != nil {
		gc.KnowledgeDigest = &wwtypes.KnowledgeDigest{
			Seat:        digest.Seat,
			TotalKnown:  digest.TotalKnown,
			TotalInRoom: digest.TotalInRoom,
			Entries:     make([]wwtypes.KnowledgeDigestEntry, 0, len(digest.Entries)),
		}
		for _, entry := range digest.Entries {
			gc.KnowledgeDigest.Entries = append(gc.KnowledgeDigest.Entries, wwtypes.KnowledgeDigestEntry{
				Source: string(entry.Source), Count: entry.Count, LastRound: entry.LastRound,
				Highlights: append([]string(nil), entry.Highlights...),
			})
		}
	}

	// 2026-08-10 §20260810-07 — 多假说并行推演。
	// §128 对话即思考:bot 在 LastDecisionSummary 末尾的「📊 [...]」JSON 段
	// 提交假说更新,由 HypothesisStore 解析后填到 gc.HypothesisTable,LLM
	// 在 user prompt 末尾追加「📊 你的当前假说」块;§119 协议层隔离 —
	// 不进 chat_message / chat_history,仅本人 bot 可见。
	// §20260811-09 U2 — easy 档不注入假说表(降低 LLM 上下文复杂度,
	// 新手 Agent 不需要看自己的历史推理)。normal/hard/hell 维持现状。
	if !ProfileFor(AgentDifficulty(r.agentDifficulty)).InjectHypotheses {
		// skip
	} else if ht := r.hypothesisStoreLocked().GetLocked(seat); ht != nil {
		snap := &wwtypes.HypothesisTableSnapshot{
			Seat:      ht.Seat,
			Round:     ht.Round,
			UpdatedAt: ht.UpdatedAt,
			Entries:   make([]wwtypes.HypothesisEntrySnapshot, 0, len(ht.Entries)),
		}
		for _, e := range ht.Entries {
			snap.Entries = append(snap.Entries, wwtypes.HypothesisEntrySnapshot{
				TargetSeat: e.TargetSeat,
				RoleGuess:  e.RoleGuess,
				Confidence: e.Confidence,
				Supporting: e.Supporting,
				Refuting:   e.Refuting,
				UpdatedAt:  e.UpdatedAt,
			})
		}
		gc.HypothesisTable = snap
	}

	// §20260810-11 H2 — 公开质疑:本 bot 是被质疑者时,把 LastChallengedBy/Question
	// 拷贝到 GameContext.LastChallenge,ChallengeBlock 据此渲染。
	// 协议层:质疑内容已通过活动流公开(§119),本镜像仅做 LLM 上下文传递。
	if seat >= 0 && seat < len(r.State.Players) {
		p := r.State.Players[seat]
		if p.LastChallengedBy >= 0 && p.LastChallengeQuestion != "" {
			gc.LastChallenge = &wwtypes.LastChallengeSpec{
				BySeat:   p.LastChallengedBy,
				Question: p.LastChallengeQuestion,
			}
		}
		// §20260811-06 U4 — 行为一致性校验:
		// BotTranscript.LastConsistencyCheck 字段已在 recordTranscript 末尾自动
		// 填充;prompt 渲染链路仅在 BotTranscript→BotContextJSON 路径上读该字段。
		// 玩家侧 sanitizeBotTranscript 不清空 LastConsistencyCheck(本身不揭示身份)。

		// §20260811-06 U5 — 黎明流言:把房间级 LastRumors 拷贝到 GameContext,
		// prompt.go::RumorBlock 据此渲染给本 bot。
		if rumors := buildAgentContextRumorBlock(r); len(rumors) > 0 {
			gc.LastRumors = rumors
		}
	}

	// §20260811-02 U1 — 发言影响力生态:把全场公开影响力快照注入 GameContext。
	// tracker 为空(第 1 天首次投票结算前)时整段 omitempty,InfluenceBlock 渲染空串。
	if snap := r.influenceTrackerLocked().SnapshotLocked(); len(snap) > 0 {
		briefs := make([]wwtypes.InfluenceBrief, 0, len(snap))
		for _, s := range snap {
			b := wwtypes.InfluenceBrief{
				Seat:       s.Seat,
				Total:      s.Total,
				Persuasion: s.Persuasion,
				Attention:  s.Attention,
				Presence:   s.Presence,
				Survival:   s.Survival,
			}
			briefs = append(briefs, b)
			if s.Seat == seat {
				mine := b
				gc.MyInfluence = &mine
			}
		}
		gc.InfluenceScores = briefs
	}

	// 2026-08-26 §20260826-01 U1 — 身份偏见:把房间级 RolePriorStore 中本 bot 的
	// 先验表拷贝到 GameContext.RolePrior;prompt.go::RolePriorBlock 据此渲染。
	// §119 协议层隔离:不进 chat_message / chat_history / HeartThought。
	// §135:玩家仅看自己 bot 的(本函数 seat=playerSeat 即本人);spectator 走 view.go。
	// §128:不调 LLM,纯服务端计算。
	if prior := r.rolePriorStoreLocked().GetLocked(seat); prior != nil {
		snap := &wwtypes.RolePriorSnapshot{
			Seat:       prior.Seat,
			Entries:    make([]wwtypes.RolePriorSingleSnapshot, 0, len(prior.Entries)),
			ComputedAt: prior.ComputedAt,
		}
		for _, e := range prior.Entries {
			snap.Entries = append(snap.Entries, wwtypes.RolePriorSingleSnapshot{
				TargetSeat:   e.TargetSeat,
				RoleGuess:    e.RoleGuess,
				PriorProb:    e.PriorProb,
				EvidenceKind: e.EvidenceKind,
				Note:         e.Note,
				ComputedAt:   e.ComputedAt,
			})
		}
		gc.RolePrior = snap
	}

	// 2026-08-26 §20260826-01 U2 — 记忆印象:把 ImpressionStore.GetLocked(seat, now)
	// 拷贝到 GameContext.ImpressionMemory;ImpressionBlock 据此渲染。
	// §119 协议层隔离:不进 chat_message / chat_history / HeartThought。
	// §135:玩家仅看自己 bot 的(本函数已满足)。
	if mem := r.impressionStoreLocked().GetLocked(seat, time.Now()); mem != nil {
		snap := &wwtypes.ImpressionMemorySnapshot{
			Seat:      mem.Seat,
			Entries:   make([]wwtypes.ImpressionEntrySnapshot, 0, len(mem.Entries)),
			UpdatedAt: mem.UpdatedAt,
		}
		for _, e := range mem.Entries {
			snap.Entries = append(snap.Entries, wwtypes.ImpressionEntrySnapshot{
				TargetSeat: e.TargetSeat,
				Dims: wwtypes.ImpressionDimsSnapshot{
					Trust:       e.Dims.Trust,
					Competence:  e.Dims.Competence,
					Sincerity:   e.Dims.Sincerity,
					Cooperation: e.Dims.Cooperation,
					Threat:      e.Dims.Threat,
				},
				LastUpdateMS: e.LastUpdateMS,
				EventCount:   e.EventCount,
				SampleEvents: append([]string(nil), e.SampleEvents...),
			})
		}
		gc.ImpressionMemory = snap
	}

	// 2026-08-26 §20260826-01 U4 — 情绪→推理权重:根据当前 bot 情绪实时计算权重。
	// 仅 bot 座位(真人玩家无 BotTranscript);prompt.go::EmotionReasoningBlock 据此渲染。
	// Emotion 字段挂在 BotTranscript 上,经 r.BotAgents[seat] 访问。
	if seat >= 0 && seat < len(r.State.Players) && r.State.Players[seat].IsBot &&
		r.BotAgents != nil {
		emo := ""
		if ag, ok := r.BotAgents[seat]; ok && ag != nil {
			if bt := ag.BotTranscript(); bt != nil {
				emo = bt.Emotion
			}
		}
		w := weightsForEmotion(emo)
		gc.EmotionReasoningWeights = &wwtypes.EmotionReasoningWeightsSnapshot{
			HypothesisConfidenceFloor: w.HypothesisConfidenceFloor,
			HypothesisConfidenceCeil:  w.HypothesisConfidenceCeil,
			ThreatMultiplier:          w.ThreatMultiplier,
			TrustMultiplier:           w.TrustMultiplier,
			StabilityBias:             w.StabilityBias,
			SampleEvent:               w.SampleEvent,
		}
	}

	// 2026-08-11 §20260811-05 U1 — 玩家行为画像注入(纯内存读房间缓存,
	// 热路径零 DB 查询;缓存由 PrefetchPlayerProfiles 在开局/入座时预取)。
	// 仅人类座位有值;全 AI 房间或无画像时 nil,PlayerProfileBlock 渲染空串。
	fillPlayerProfilesLocked(r, seat, &gc)

	// §20260830-01 §6.1 — 死亡亮身份接线(werewolf-agent 职责线,death_reveal.go
	// 头注释 TODO 的落地):
	//   1. gc.RevealRoleOnDeath / gc.RevealedRoles 注入 GameContext —— 数据由
	//      revealedRolesSnapshotLocked 备好(§135 单点判定 RolePubliclyRevealed
	//      派生,禁止读 gs.Roles 原始数组);prompt.go 的 §135 规则段与
	//      【死亡白名单】据此双模式渲染。
	//   2. 玩家 Agent 的 system prompt 冻结快照按开关同步(SetRevealRoleOnDeath
	//      幂等,值未变零开销)—— 保证首次 wake 后 LLM 请求路径与冻结快照
	//      同源(invariant I11),prompt cache 字节稳定不受影响。
	// 公平性: RevealedRoles 与 BuildClientState / REST 详情同源(同一单点判定),
	// 玩家 Agent 不存在早于公共通道的抢先信息(§1.2 不变式 3)。
	// 锁: 本函数调用方已持 r.mu(§92a);setter 仅取 a.mu,r.mu → a.mu 与
	// 既有 AvgLLMLatencyMs 调用同序。
	gc.RevealRoleOnDeath = gs.RevealRoleOnDeath
	gc.RevealedRoles = revealedRolesSnapshotLocked(gs)
	if botAgent, ok := r.BotAgents[seat]; ok && botAgent != nil {
		botAgent.SetRevealRoleOnDeath(gs.RevealRoleOnDeath)
	}

	// 2026-08-13 §20260813-05 U2 — runtime invariant companion。
	// 检查 GameContext 字段值契约(12 条不变量之 I1-I6),失败 Debug 日志 +
	// 计数器(wwtypes.InvariantViolationCount),不阻塞当前帧。
	if violations := wwtypes.CheckGameContextInvariant(&gc); len(violations) > 0 {
		for _, v := range violations {
			logger.L().Debug("werewolf: invariant violation in buildAgentContextLocked",
				zap.String("code", v.Code),
				zap.String("kind", string(v.Kind)),
				zap.String("message", v.Message),
				zap.Int("seat", seat),
				zap.String("phase", gc.Phase),
				zap.Int("round", gc.Round))
		}
	}

	return gc
}
