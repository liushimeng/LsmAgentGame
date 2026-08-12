package werewolf

import (
	"fmt"
	"strings"
)

// §20260810-09 — 上帝视角观战快照填充函数。
//
// 设计文档: docs/狼人杀-Agent与系统/狼人杀13人局Agent升级-20260810-09.md §2.1
//
// 核心约束:
//   - §135 公平性:仅 spectator(viewer<0)下发;玩家与 REST 永远 omitempty。
//   - §92a 锁内变体:BuildClientStateWithRoom 全部 4 个调用点(GetState / StateForSeat
//     / SpectatorState / SpectatorView)已持有 r.mu;本函数严禁再次 Lock,直接
//     锁内直读 r.State / r.infoLedger 等字段。
//   - §119 协议层隔离:历史夜间行动通过 InformationLedger 聚合(已有 spectator-only
//     快照路径),不重新引入私有字段,避免第 5 条身份下发通道诞生。

// populateGodModeLocked 构造当前房间的上帝视角快照。
//
// 调用方必须已持有 r.mu(§92a)。仅在 BuildClientStateWithRoom 的 spectator
// 分支(viewer<0)调用,玩家视图与 REST 房间视图不会进入本函数。
//
// 数据来源(全部为既有字段,无新增存储):
//   - Roles / Factions  : gs.Roles[seat] + FactionOf
//   - WolfKillTarget    : gs.WolfKillTarget(已被守卫/女巫结算覆盖)
//   - WolfVotes         : gs.WolfVotes[wolf_seat]
//   - SeerChecks        : r.infoLedger 中 source=InfoSourceNightSeer 的条目
//   - WitchDecisions    : r.infoLedger 中 source=InfoSourceNightWitch 的条目
//   - GuardProtects     : r.infoLedger 中 source=InfoSourceNightGuard 的条目
func (r *WerewolfRoom) populateGodModeLocked() *GodModeSnapshot {
	if r.State == nil {
		return nil
	}
	gs := r.State
	snap := &GodModeSnapshot{
		Enabled:        true, // 字段恒为 true;前端 localStorage 控制是否渲染
		Roles:          make(map[int]string, MaxPlayers),
		Factions:       make(map[int]string, MaxPlayers),
		WolfKillTarget: int(gs.WolfKillTarget),
		WolfVotes:      make(map[int]int, MaxPlayers),
		SeerChecks:     []SeerCheckEntry{},
		WitchDecisions: []WitchDecision{},
		GuardProtects:  []int{},
		PublicActions:  []PublicActionEntry{}, // §20260811-08 U3
	}
	// 全量身份 + 阵营(§135 spectator-only)
	for i := 0; i < MaxPlayers; i++ {
		if r.Seats[i] == "" {
			continue
		}
		snap.Roles[i] = gs.Roles[i].String()
		snap.Factions[i] = FactionOf(gs.Roles[i]).String()
	}
	// 狼刀投票快照
	for i := 0; i < MaxPlayers; i++ {
		if gs.WolfVoteCast[i] {
			snap.WolfVotes[i] = int(gs.WolfVotes[i])
		}
	}
	// §20260810-09 历史聚合 —— 直接走 InformationLedger(§20260810-05/08 已落地)。
	// InformationLedger 是当前唯一事实来源,不在此新增镜像字段(避免 §130 接线漂移)。
	if r.infoLedger != nil {
		entries := r.infoLedger.entriesSnapshot()
		for _, e := range entries {
			switch e.Source {
			case InfoSourceNightSeer:
				// ledgerAppendLocked format:"seer_check seat=S target=T"
				// (S 预言家座位,T 查验目标)。简单解析,失败跳过。
				seat, target := parseSeatTargetPair(e.Fact, "seer_check")
				if seat >= 0 && target >= 0 {
					// 真实阵营(仅 spectator):从 gs.Roles 直接查。
					result := "good"
					if FactionOf(gs.Roles[Seat(target)]) == FactionWolf {
						result = "werewolf"
					}
					snap.SeerChecks = append(snap.SeerChecks, SeerCheckEntry{
						Day:    e.Round,
						Seat:   seat,
						Target: target,
						Result: result,
					})
				}
			case InfoSourceNightWitch:
				seat, antidote, poison := parseWitchTriple(e.Fact)
				if seat >= 0 {
					snap.WitchDecisions = append(snap.WitchDecisions, WitchDecision{
						Day:         e.Round,
						Seat:        seat,
						AntidoteUse: antidote,
						PoisonUse:   poison,
					})
				}
			case InfoSourceNightGuard:
				_, target := parseSeatTargetPair(e.Fact, "guard_protect")
				if target >= 0 {
					snap.GuardProtects = append(snap.GuardProtects, target)
				}

			// §20260811-08 U3 — 4 类已公开技能行动。写入点早已存在
			// (room_action.go:110/238/275/625),此前从未被 GodMode 消费。
			case InfoSourceHunterShot:
				s, t := parseSeatTargetPair(e.Fact, "hunter_shot")
				if s >= 0 {
					snap.PublicActions = append(snap.PublicActions, PublicActionEntry{
						Day: e.Round, Kind: "hunter_shot", Seat: s, Target: t,
					})
				}
			case InfoSourceKnightDuel:
				s, t, hit := parseSeatTargetHitWolf(e.Fact, "knight_duel")
				if s >= 0 {
					snap.PublicActions = append(snap.PublicActions, PublicActionEntry{
						Day: e.Round, Kind: "knight_duel", Seat: s, Target: t, HitWolf: hit,
					})
				}
			case InfoSourceDemonHunter:
				s, t, hit := parseSeatTargetHitWolf(e.Fact, "demon_hunter")
				if s >= 0 {
					snap.PublicActions = append(snap.PublicActions, PublicActionEntry{
						Day: e.Round, Kind: "demon_hunter", Seat: s, Target: t, HitWolf: hit,
					})
				}
			case InfoSourceIdiotReveal:
				if s := parseSeatOnly(e.Fact, "idiot_reveal"); s >= 0 {
					snap.PublicActions = append(snap.PublicActions, PublicActionEntry{
						Day: e.Round, Kind: "idiot_reveal", Seat: s, Target: -1,
					})
				}
			}
		}
	}

	// §20260810-11 V1 + §20260811-08 U1 — PerSeatPOV 填充(全 13 座位「第一视角」快照)。
	//
	// §20260811-08 U1 修复:旧版 7 个字段硬编码为空/零值,注释自述「实际数据由前端
	// 通过单独的 spectator-only endpoint 拉取」—— 该 endpoint 从未存在
	// (grep "per_seat_pov" ServerGo/api/ ServerGo/ws/ 零命中),导致前端视角切换
	// 面板永远只显示角色+阵营。这是 §130「声明了却从不接线」的又一次复现,且注释
	// 把缺陷伪装成「后续 V2」的既定设计(同 §134 prompt.go「暂无独立工具」)。
	//
	// §119 协议层隔离:HeartThought 在此填充**不**构成新的泄漏通道 —— 本函数只在
	// BuildClientStateWithRoom 的 spectator 分支(viewer<0)被调用,与既有
	// sanitizeBotTranscript(玩家分支清空 HeartThought)语义一致。
	// §92a:全程锁内直读,不新增任何 Lock(R212 教训:新增只读 getter 引入自死锁)。
	snap.PerSeatPOV = make(map[int]PerSeatPOV, MaxPlayers)
	// 承诺账本按座位预分组,避免 13 座位 × 全量遍历。
	// CommitmentLedger 的方法均为 *Locked 语义(自身不加锁),caller 已持 r.mu。
	commitsBySeat := make(map[int][]string, MaxPlayers)
	if r.commitmentLedger != nil {
		for _, c := range r.commitmentLedger.GetAllLocked() {
			if c == nil || c.Seat < 0 || c.Seat >= MaxPlayers {
				continue
			}
			commitsBySeat[c.Seat] = append(commitsBySeat[c.Seat],
				fmt.Sprintf("%s(%s)", string(c.Template), string(c.Status)))
		}
	}
	// 夜间行动按行动者座位预分组(复用 §20260810-05 信息账本,不新增镜像字段)。
	nightBySeat := r.buildNightActionsBySeatLocked()
	for i := 0; i < MaxPlayers; i++ {
		if r.Seats[i] == "" {
			continue
		}
		pov := PerSeatPOV{
			Role: gs.Roles[i].String(),
			// §135 单点判定:必须走 RolePubliclyRevealed,不得手写条件。
			// 旧版手写的 `Status=="over" || HunterFired || IdiotRevealed` **漏了狼自爆**
			// (DeathCause == DeathCauseSuicide) —— 正是 §135 教训 (1) 所说的「第 5 处」,
			// 它在 §135 落地之后又诞生了。
			RoleRevealed:      gs.RolePubliclyRevealed(Seat(i)),
			Faction:           FactionOf(gs.Roles[i]).String(),
			NightActions:      nightBySeat[i],
			PublicCommitments: commitsBySeat[i],
		}
		if pov.NightActions == nil {
			pov.NightActions = []string{}
		}
		if pov.PublicCommitments == nil {
			pov.PublicCommitments = []string{}
		}
		// §20260811-08 U1 — 被质疑态。引擎只有 LastChallengedBy「最近一次」字段
		// (engine.go:162,每轮在 engine_day.go:337 重置),**没有本局累计计数器**。
		// 故此处填 0/1 表示「当前是否处于被质疑态」,并已同步修正 view.go 上该
		// 字段的注释 —— 不能一边修 §130 一边制造新的「注释承诺 X 代码做 Y」。
		if i < len(gs.Players) && gs.Players[i].LastChallengedBy >= 0 {
			pov.ChallengeCount = 1
		}
		// Bot 座位:从 BotTranscript 取内心独白 / 决策摘要 / 调用统计 / 情绪。
		// 真人座位无 agent,以上字段保持零值(前端渲染「—」)。
		if r.BotAgents != nil {
			if ag := r.BotAgents[i]; ag != nil {
				if bt := ag.BotTranscript(); bt != nil {
					pov.HeartThought = truncateRunes(bt.HeartThought, povTextMaxRunes)
					pov.LastDecision = truncateRunes(bt.LastDecisionSummary, povTextMaxRunes)
					pov.ToolCallCount = len(bt.ToolCalls)
					pov.LLMCallCount = bt.TotalLLMCalls
					pov.LastEmotion = bt.Emotion
				}
			}
		}
		snap.PerSeatPOV[i] = pov
	}
	return snap
}

// povTextMaxRunes 是 PerSeatPOV 文本字段的截断上限(rune 安全)。
// 与 view.go 上 PerSeatPOV.HeartThought 的「截断到 200 字」注释保持一致。
// 截断复用 information_ledger.go 的 truncateRunes(rune 安全)。
const povTextMaxRunes = 200

// buildNightActionsBySeatLocked 从 InformationLedger 聚合「该座位作为行动者」的
// 夜间行动摘要,供 PerSeatPOV.NightActions 使用(§20260811-08 U1)。
//
// caller 必须已持 r.mu(§92a)。数据源是 §20260810-05 已落地的信息账本,
// **不新增镜像字段** —— 避免 §130 接线漂移(同 populateGodModeLocked 既有做法)。
//
// 解析失败静默跳过,与本文件既有 parseSeatTargetPair / parseWitchTriple 行为一致。
func (r *WerewolfRoom) buildNightActionsBySeatLocked() map[int][]string {
	out := make(map[int][]string, MaxPlayers)
	if r == nil || r.infoLedger == nil {
		return out
	}
	for _, e := range r.infoLedger.entriesSnapshot() {
		seat, text := -1, ""
		switch e.Source {
		case InfoSourceNightSeer:
			s, t := parseSeatTargetPair(e.Fact, "seer_check")
			if s >= 0 && t >= 0 {
				seat, text = s, fmt.Sprintf("D%d 查验 %d 号", e.Round, t+1)
			}
		case InfoSourceNightGuard:
			s, t := parseSeatTargetPair(e.Fact, "guard_protect")
			if s >= 0 && t >= 0 {
				seat, text = s, fmt.Sprintf("D%d 守护 %d 号", e.Round, t+1)
			}
		case InfoSourceNightWitch:
			s, anti, poison := parseWitchTriple(e.Fact)
			if s >= 0 {
				parts := []string{}
				if anti >= 0 {
					parts = append(parts, fmt.Sprintf("解药救 %d 号", anti+1))
				}
				if poison >= 0 {
					parts = append(parts, fmt.Sprintf("毒药毒 %d 号", poison+1))
				}
				if len(parts) == 0 {
					parts = append(parts, "未用药")
				}
				seat, text = s, fmt.Sprintf("D%d %s", e.Round, strings.Join(parts, " + "))
			}
		}
		if seat >= 0 && seat < MaxPlayers && text != "" {
			out[seat] = append(out[seat], text)
		}
	}
	return out
}

// parseSeatTargetPair 解析 "seer_check seat=S target=T" / "guard_protect seat=G target=T"。
// 返回 (seat, target);任一缺失返回 (-1, -1)。
func parseSeatTargetPair(fact, prefix string) (int, int) {
	if !strings.HasPrefix(fact, prefix+" ") {
		return -1, -1
	}
	var seat, target int
	_, err := fmt.Sscanf(fact, prefix+" seat=%d target=%d", &seat, &target)
	if err != nil {
		return -1, -1
	}
	return seat, target
}

// parseSeatTargetHitWolf 解析 "knight_duel seat=S target=T hit_wolf=B" /
// "demon_hunter seat=S target=T hit_wolf=B"(§20260811-08 U3)。
//
// 返回 (seat, target, hitWolf);解析失败返回 (-1, -1, nil)。
// hitWolf 用指针以区分「没打中狼(false)」与「不适用(nil)」。
func parseSeatTargetHitWolf(fact, prefix string) (int, int, *bool) {
	if !strings.HasPrefix(fact, prefix+" ") {
		return -1, -1, nil
	}
	var seat, target int
	var hit bool
	_, err := fmt.Sscanf(fact, prefix+" seat=%d target=%d hit_wolf=%t", &seat, &target, &hit)
	if err != nil {
		return -1, -1, nil
	}
	return seat, target, &hit
}

// parseSeatOnly 解析 "idiot_reveal seat=S"(§20260811-08 U3)。
// 返回 seat;解析失败返回 -1。
func parseSeatOnly(fact, prefix string) int {
	if !strings.HasPrefix(fact, prefix+" ") {
		return -1
	}
	var seat int
	if _, err := fmt.Sscanf(fact, prefix+" seat=%d", &seat); err != nil {
		return -1
	}
	return seat
}

// parseWitchTriple 解析 "witch_act seat=W action=A target=T"。
// 返回 (seat, antidote, poison);action=antidote 时 target 写入 antidote;
// action=poison 时 target 写入 poison;action=none 时两个均为 -1。
func parseWitchTriple(fact string) (int, int, int) {
	if !strings.HasPrefix(fact, "witch_act ") {
		return -1, -1, -1
	}
	var seat, target int
	var action string
	_, err := fmt.Sscanf(fact, "witch_act seat=%d action=%s target=%d", &seat, &action, &target)
	if err != nil {
		return -1, -1, -1
	}
	antidote, poison := -1, -1
	switch action {
	case "antidote":
		antidote = target
	case "poison":
		poison = target
	case "none", "":
		// 两个保持 -1
	default:
		// 未知 action 也保持 -1,不污染观战者视图。
	}
	return seat, antidote, poison
}