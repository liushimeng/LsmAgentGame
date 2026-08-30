package werewolf

// view_dead_list.go — §4 单文件 ≤1800 行治理:view.go 死亡列表构造器整段搬移
// (2026-08-30 §20260830-01 同批纯代码搬移;零逻辑改动,函数体逐字节保留)。
// 三个构造器统一经 publicRoleName → RolePubliclyRevealed 单点判定脱敏 ——
// §20260830-01 死亡亮身份开启后自动生效(第 ⑦ 分支),本文件零改动。

// buildDeadListLocked 构造已死亡玩家列表(含遗言状态)。全部玩家 + 观战者可见。
//
// BUG-R227-P2-01 (2026-08-01): 历史抽屉 ⚰ 死亡 / ⏱ 时间轴渲染的
// DeadPlayerJSON.Account 字段原先填的是 Player.UserID (即
// t_lsm_game_user.id UUID),玩家看到的是
// `#2 ea9587d5-ffe0-4b17-b2b2-534aac5164df`,既丑陋又构成不必要的
// 用户标识符暴露。修复:在三个 buildDeadList*Locked 函数里把 Account
// 改成**座位派生昵称**(bot → "Bot #N号",人类 → "玩家N号"),
// 然后 BuildClientStateWithRoom::enrichDeadListAccountsLocked 在
// populateAgentNames 之后用 cs.Players[i].AgentName 把 bot 昵称升级为
// "agent_name #N号" (与 GameChatPanel.toRoomPlayers 完全一致的策略)。
// 单一事实来源在 enrichDeadListAccountsLocked,此处仅保证 Account 不是 UUID。
func buildDeadListLocked(gs *GameState) []DeadPlayerJSON {
	out := make([]DeadPlayerJSON, 0, MaxPlayers)
	// 当天号 → 死因:辅助判断(死亡顺序的近似)。
	for i := 0; i < MaxPlayers; i++ {
		p := &gs.Players[i]
		if p.Alive || gs.Seats[i] == "" {
			continue
		}
		status := "ineligible"
		if p.LastWords {
			// LastWords 仍为 true 表示还未消费(仍在队列或待发言)。
			if gs.DeathLyricDone[Seat(i)] {
				// 防御性:已完成但 LastWords 未清(不应发生)。
				status = "spoken"
			} else {
				status = "pending"
			}
		} else {
			// LastWords=false:可能已发言/跳过,也可能 ineligible(毒杀/自爆/Day≥3)。
			if gs.DeathLyricDone[Seat(i)] {
				// 在 done map 中,但需区分 spoken / skipped。引擎未分开记录,
				// 统一标 spoken(前端显示"已发言/跳过"通用徽章)。
				status = "spoken"
			} else {
				status = "ineligible"
			}
		}
		out = append(out, DeadPlayerJSON{
			Seat:            i,
			Account:         seatDisplayAccount(p),
			Role:            publicRoleName(gs, Seat(i)),
			LastWordsStatus: status,
			Cause:           p.DeathCause,
			Verdict:         p.DeathVerdict,
			Day:             gs.DayNumber,
		})
	}
	return out
}

// buildAllDeadListLocked 构造全阶段可用的"全部历史死亡"列表(2026-07-11 R96-P1)。
//
// 与 buildDeadListLocked(仅 PhaseDeathLyric)、buildDeadListForSeatsLocked(仅 LastNightDeaths 涉及的座位)
// 不同:本函数扫描 gs.Players 全表,纳入所有 !p.Alive 的座位,**不依赖** LastNightDeaths(每晚重置)
// 与 DeathLyricDone(仅死亡时更新),让 day2/3/4 已死座位始终带 §123 verdict 字段。
//
// LastWordsStatus 留空(本字段专为遗言进度设计,与 verdict 徽章无关)。
//
// BUG-R227-P2-01: Account 走 seatDisplayAccount 而非 UserID(详见 buildDeadListLocked 注释)。
func buildAllDeadListLocked(gs *GameState) []DeadPlayerJSON {
	out := make([]DeadPlayerJSON, 0, MaxPlayers)
	for i := 0; i < MaxPlayers; i++ {
		p := &gs.Players[i]
		if p.Alive || gs.Seats[i] == "" {
			continue
		}
		out = append(out, DeadPlayerJSON{
			Seat:    i,
			Account: seatDisplayAccount(p),
			Role:    publicRoleName(gs, Seat(i)),
			Cause:   p.DeathCause,
			Verdict: p.DeathVerdict,
			Day:     gs.DayNumber,
		})
	}
	return out
}

// buildDeadListForSeatsLocked 构造指定座位列表的死亡信息(用于 LastNightDeathsVerbose)。
// 2026-07-10 §123: 即使座位仍存活(理论上不应发生),也返回空 verdict,便于前端容错。
//
// BUG-R227-P2-01: Account 走 seatDisplayAccount 而非 UserID(详见 buildDeadListLocked 注释)。
func buildDeadListForSeatsLocked(gs *GameState, seats []Seat) []DeadPlayerJSON {
	out := make([]DeadPlayerJSON, 0, len(seats))
	for _, s := range seats {
		if s < 0 || s >= MaxPlayers || gs.Seats[s] == "" {
			continue
		}
		p := &gs.Players[s]
		out = append(out, DeadPlayerJSON{
			Seat:    int(s),
			Account: seatDisplayAccount(p),
			Role:    publicRoleName(gs, s),
			Cause:   p.DeathCause,
			Verdict: p.DeathVerdict,
			Day:     gs.DayNumber,
		})
	}
	return out
}
