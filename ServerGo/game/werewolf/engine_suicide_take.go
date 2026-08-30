// engine_suicide_take.go — §20260830-02 自爆带走(引擎层)
//
// 设计文档:docs/狼人杀-角色设计/狼人杀自爆遗言与带走设计-20260830-02.md
//
// 链路(开关 suicide_take_enabled=true,默认):
//
//	PhaseSpeak(狼发言轮) → WolfSuicide(死亡,亮身份③白名单)
//	  → PhaseDeathLyric(自爆狼遗言,killPlayer 按 cause=suicide 给权)
//	  → PhaseSuicideTake(自爆狼选目标 / -1 放弃)
//	  → 被带走者死亡(cause=suicide_take, verdict=death, 身份不公开)
//	       ├─ Day≤2 → PhaseDeathLyric(被带走者遗言)
//	       └─ 被带走者是猎人 → PhaseHunterShoot(反枪, from="suicide_take")
//	  → advanceDay(DayNumber++) → startNight
//
// 开关 false:WolfSuicide 走旧路径(无遗言、直接 startNight),本文件所有
// 入口不再被触达。
package werewolf

import (
	"LsmAgentGame/config"
	"LsmAgentGame/errcode"
)

// isSuicideTakeEnabled 安全读取 config.Werewolf.SuicideTakeEnabled。
// 默认 true(含 config 未加载的测试环境);仅 operator 显式设 false 时回退
// 旧行为 —— 与 engine_death_lyric.go::isDeathLyricEnabled 同模式。
func isSuicideTakeEnabled() bool {
	defer func() { _ = recover() }()
	c := config.Load()
	if c == nil {
		return true
	}
	return c.Werewolf.SuicideTakeEnabled
}

// hasAliveOtherThan 返回除 exclude 外是否还有存活玩家(自爆狼已死必然被
// 排除;用于"无任何可带走目标"时跳过 suicide_take 阶段)。
func (gs *GameState) hasAliveOtherThan(exclude Seat) bool {
	for i := 0; i < MaxPlayers; i++ {
		if Seat(i) != exclude && gs.AliveSeat(Seat(i)) {
			return true
		}
	}
	return false
}

// startSuicideTake 进入自爆带走阶段(自爆狼遗言队列清空后的 onDone 入口)。
// 守卫:终局 / 无自爆狼记录 / 无存活目标 → 全部直通 advanceDay 入夜,
// 保证任何异常状态都不会把房间卡死在 suicide_take。
func (gs *GameState) startSuicideTake() *errcode.Error {
	if gs.Status == "over" {
		return nil
	}
	actor := gs.SuicidedWolfSeat
	if actor == NoSeat || gs.AliveSeat(actor) {
		// 防御:SuicidedWolfSeat 未置位或仍存活(不应发生)→ 按旧路径入夜。
		gs.advanceDay()
		return nil
	}
	if !gs.hasAliveOtherThan(actor) {
		// 没有可带走目标(其余玩家全灭,游戏基本已终局)→ 直接入夜。
		gs.advanceDay()
		return nil
	}
	setPhaseAndDeadline(gs, PhaseSuicideTake)
	// 与 PhaseHunterShoot 同约定:TurnActingSeat 指向行动者(虽已死亡),
	// watchdog / agent 侧据此派发(§BUG-R10-P0-3 同源教训)。
	gs.TurnActingSeat = actor
	return nil
}

// SuicideTake 自爆狼提交带走选择。actor 必须是本日自爆狼;target=NoSeat
// 表示放弃带走。人类(WS)与 Agent(工具)同源进此入口(公平性不变式 3)。
func (gs *GameState) SuicideTake(actor Seat, target Seat) *errcode.Error {
	if gs.Phase != PhaseSuicideTake {
		return errcode.CodeMsg(errcode.ErrNotYourTurn, "not suicide_take phase")
	}
	if actor != gs.SuicidedWolfSeat {
		return errcode.Code(errcode.ErrPermissionDenied)
	}
	if target == NoSeat {
		// 放弃带走(合法出口;watchdog 兜底也走这里)。
		gs.finishSuicideTake(nil)
		return nil
	}
	if target < 0 || target >= MaxPlayers {
		return errcode.Code(errcode.ErrValidationFailed)
	}
	if !gs.AliveSeat(target) {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "target dead")
	}
	if err := gs.killPlayer(target, DeathCauseSuicideTake); err != nil {
		return errcode.CodeMsg(errcode.ErrValidationFailed, err.Message)
	}
	if gs.checkWinner() {
		return nil
	}
	// 被带走者遗言(Day≤2 有权,killPlayer 已计算);队列清空后走收尾。
	gs.tryEnterDeathLyricRound([]Seat{target}, func() *errcode.Error {
		return gs.finishSuicideTake(&target)
	})
	return nil
}

// finishSuicideTake 自爆带走结算收尾。
//   - target=nil(放弃)→ advanceDay 入夜。
//   - 被带走者是猎人 → 置 HunterPendingShoot,from="suicide_take";
//     反枪结算由 HunterShoot → resumeAfterHunterShoot 走 advanceDay 分支
//     (from != "wolf" → DayNumber++ + startNight,白天已被自爆终止,正确)。
func (gs *GameState) finishSuicideTake(target *Seat) *errcode.Error {
	if gs.Status == "over" {
		return nil
	}
	if target != nil && gs.Roles[*target] == RoleHunter {
		gs.HunterPendingShoot = true
		gs.HunterPendingFrom = "suicide_take"
		setPhaseAndDeadline(gs, PhaseHunterShoot)
		gs.TurnActingSeat = hunterSeatForPhaseLocked(gs)
		return nil
	}
	gs.advanceDay()
	return nil
}
