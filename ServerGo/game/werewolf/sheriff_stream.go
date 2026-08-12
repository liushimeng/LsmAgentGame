// Package werewolf — sheriff_stream.go: 警徽流核心机制。
//
// 警徽流是 13 人标准竞技局预言家在无遗言的夜间死亡后,向全场传递验人信息的
// 唯一方式。详见 docs/狼人杀13人标准局规则.md §7。
//
// 数据承载在 GameState 字段(SheriffStreams / SheriffStreamsAt / SheriffSuccessor /
// sheriffSlain),本文件提供结算逻辑 SettleSheriffOnDeathLocked。
//
// 关键约束(CLAUDE.md §92a):
//   - SettleSheriffOnDeathLocked 假定 caller 已持 r.mu。
//   - 本文件不调用任何会反向 Lock() 的公共 Action_*。
package werewolf

// streamFaction 返回警徽流目标的结算阵营 —— 「是否真查过」的**单点判定**
// (§20260810-04 U3,修复 K3-F3 / LongCat-F3)。
//
// 仅当目标存活 **且** 出现在声明警徽流的预言家(seer)的 SeerCheckHistory
// (整局真实查验历史)中时,返回其真实阵营;否则返回 "unknown"。
//
// 修复前的缺陷:直接读 gs.Roles[target] 底牌,不校验是否真查验过 ——
// 假预言家当警长后可借警徽流「验」任何人且服务端权威为真,狼队可无限造谣。
// 修复后:没真查过的目标按「声明无效」处理(unknown → 倾向撕警徽),
// 真预言家的警徽流含金量上升(必须真查才能留流)。
//
// 历史存在 Player.SeerCheckHistory(GameState 内),不依赖存活状态 ——
// 预言家死后(警长夜死场景)仍可结算。
func streamFaction(gs *GameState, seer Seat, target Seat) string {
	if target < 0 || target >= MaxPlayers || !gs.AliveSeat(target) {
		return "unknown"
	}
	if seer < 0 || seer >= MaxPlayers {
		return "unknown"
	}
	for _, checked := range gs.Players[seer].SeerCheckHistory {
		if checked == target {
			return FactionOf(gs.Roles[target]).String()
		}
	}
	return "unknown"
}

// isGoldWater 判断「目标是否为金水(好人)」 — 仅当预言家真实查过且阵营为好人。
// 未在真实验人记录中的目标视为 unknown(按撕警徽倾向处理),避免假预言家报流获利。
func isGoldWater(gs *GameState, seer Seat, target Seat) bool {
	return streamFaction(gs, seer, target) == FactionGood.String()
}

// isWolfKill 判断「目标是否为查杀(狼人)」。
func isWolfKill(gs *GameState, seer Seat, target Seat) bool {
	return streamFaction(gs, seer, target) == FactionWolf.String()
}

// SettleSheriffOnDeathLocked 在白天 dawn 阶段结算夜间死亡的警徽流。
// 规则(docs/狼人杀13人标准局规则.md §7.3):
//   - 死者非警长 → (successor=NoSeat, ripped=false),本函数不处理。
//   - 死者为警长且为预言家 → 按双警徽流结算:
//     双金水 → 移交第一警徽流目标; 一金一查杀 → 移交金水目标; 双查杀 → 撕警徽。
//   - 死者为警长但非预言家 → 走 SheriffSuccessor(生前口头指定);无指定则撕。
//   - 警徽流目标已提前死亡 / 无声明 → 外置位公认好人;无声明则按撕警徽处理。
//
// 结算后写 gs.SheriffSuccessor。caller 持 r.mu。返回 (successor, ripped)。
func (r *WerewolfRoom) SettleSheriffOnDeathLocked(gs *GameState) (successor Seat, ripped bool) {
	if gs == nil {
		return NoSeat, false
	}
	deadSeat := gs.sheriffSlain
	if deadSeat == NoSeat {
		// 本夜无警长死亡,无需结算。
		return NoSeat, false
	}
	// 结算完毕,防止重复结算。
	gs.sheriffSlain = NoSeat

	// 非预言家警长:看 SheriffSuccessor(生前口头指定),无则撕。
	if gs.Roles[deadSeat] != RoleSeer {
		if gs.SheriffSuccessor != NoSeat && gs.AliveSeat(gs.SheriffSuccessor) {
			return gs.SheriffSuccessor, false
		}
		return NoSeat, true
	}

	// 预言家警长:按双警徽流结算。
	s1, s2 := gs.SheriffStreams[0], gs.SheriffStreams[1]
	s1Valid := s1 != NoSeat && gs.AliveSeat(s1)
	s2Valid := s2 != NoSeat && gs.AliveSeat(s2)

	// §20260810-04 U3 — 查验历史结算以「死去的警长(即声明警徽流的预言家)」为 seer。
	seer := deadSeat
	switch {
	case s1Valid && s2Valid:
		s1Gold := isGoldWater(gs, seer, s1)
		s2Gold := isGoldWater(gs, seer, s2)
		s1Kill := isWolfKill(gs, seer, s1)
		s2Kill := isWolfKill(gs, seer, s2)
		switch {
		case s1Gold && s2Gold:
			// 双金水 → 移交第一警徽流目标。
			gs.SheriffSuccessor = s1
			return s1, false
		case s1Gold && s2Kill:
			return s1, false
		case s2Gold && s1Kill:
			return s2, false
		default:
			// 双查杀(或含 unknown) → 撕警徽。
			return NoSeat, true
		}
	case s1Valid:
		// 单警徽(仅第一槽有声明):金水移交,查杀/unknown(未真查过)撕。
		if isGoldWater(gs, seer, s1) {
			gs.SheriffSuccessor = s1
			return s1, false
		}
		return NoSeat, true
	case s2Valid:
		if isGoldWater(gs, seer, s2) {
			gs.SheriffSuccessor = s2
			return s2, false
		}
		return NoSeat, true
	default:
		// 双槽均无声明 → 撕警徽。
		return NoSeat, true
	}
}
