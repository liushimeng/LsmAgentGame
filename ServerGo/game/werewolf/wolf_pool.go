// Package werewolf — wolf_pool.go: §20260811-10 U3 狼队阵营金币池。
//
// 设计动机:教育玩家「悍跳是高风险」。
// 触发条件(狼阵亡时):
//   - 自爆 (DeathCauseSuicide + Faction==wolf)        → WolfPoolBalance -= 30
//   - 白天投票放逐 (DayEliminated == seat + wolf)     → WolfPoolBalance -= 30
//
// 不触发:
//   - 夜间狼刀互杀(自己狼刀狼队友) → 不扣
//   - 女巫毒杀狼                  → 不扣
//   - 猎人开枪狼                  → 不扣
//   - 狼自爆后被结算再算一次 vote → 不重复扣(重入保护)
//
// 设计约束(CLAUDE.md §92a):
//   - ApplyWolfPoolPenaltyLocked 假定 caller 已持 r.mu(由 killPlayer /
//     投票放逐结算的持锁路径调用);不重入锁。
//   - 同一座位本局只扣一次:WolfPoolPenaltyApplied[seat] 置位后忽略后续触发。
//   - 简化实现:不引入 wallet service;WolfPoolBalance 仅是 GameState 字段,
//     通过 view.go 下发到 ClientGameState.WolfPoolBalance(全场可见)。
//
// 历史参考:docs/狼人杀-Agent与系统/狼人杀13人局Agent升级-20260811-10.md §U3
package werewolf

// wolfPoolPenaltyCoin 是每次悍跳失败从阵营金币池扣除的固定金额。
// 教学成本:30 金币 = 一次普通道具价格的一半,既不致命又能形成"亏过"
// 的体感;过高会让狼队失去悍跳动机,过低则失去教育意义。
const wolfPoolPenaltyCoin int64 = 30

// ApplyWolfPoolPenaltyLocked 在狼人因自爆或白天投票放逐死亡时,从阵营
// 金币池扣除固定金额。同一座位本局只扣一次(重入保护)。
//
// 调用方必须已持 r.mu(§92a)。seat 必须在 [0, MaxPlayers) 范围内且
// gs.Roles[seat] 属于狼阵营。cause 决定是否扣款:
//   - DeathCauseSuicide("suicide"):狼人自爆 → 扣
//   - DeathCauseVote("vote"):白天投票放逐 → 扣
//   - 其他(夜间狼刀互杀/女巫毒杀/猎人开枪/决斗自决等):不扣
//
// 返回值:实际扣除的金额(0 = 未触发)。调用方记日志用。
func (gs *GameState) ApplyWolfPoolPenaltyLocked(seat Seat, cause string) int64 {
	if gs == nil || seat < 0 || seat >= MaxPlayers {
		return 0
	}
	if seat >= 0 && int(seat) < len(gs.WolfPoolPenaltyApplied) && gs.WolfPoolPenaltyApplied[seat] {
		return 0
	}
	role := gs.Roles[seat]
	if FactionOf(role) != FactionWolf {
		return 0
	}
	switch cause {
	case DeathCauseSuicide, DeathCauseVote:
		// 触发
	default:
		return 0
	}
	if int(seat) < len(gs.WolfPoolPenaltyApplied) {
		gs.WolfPoolPenaltyApplied[seat] = true
	}
	gs.WolfPoolBalance -= wolfPoolPenaltyCoin
	return wolfPoolPenaltyCoin
}

// WolfPoolBalanceOf 返回当前阵营金币池余额(锁内直读)。外部调用方必须
// 已持 r.mu 或接受数据快照。-∞ 不可能(扣除受重入保护限制),最大扣除数
// 等于狼人数 × wolfPoolPenaltyCoin。
func (gs *GameState) WolfPoolBalanceOf() int64 {
	if gs == nil {
		return 0
	}
	return gs.WolfPoolBalance
}

// ResetWolfPoolForNewGameLocked 在 NewGame 末尾 / restartGameLocked 路径
// 调用,把阵营金币池与重入保护位清零。便于跨局测试与原地重开。
func (gs *GameState) ResetWolfPoolForNewGameLocked() {
	if gs == nil {
		return
	}
	gs.WolfPoolBalance = 0
	gs.WolfPoolPenaltyApplied = [MaxPlayers]bool{}
}
