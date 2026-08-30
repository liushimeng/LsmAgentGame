package werewolf

// death_reveal.go — §20260830-01 房间级「死亡亮身份」(reveal_role_on_death)引擎侧核心。
//
// 设计事实来源: docs/狼人杀-角色设计/狼人杀死亡身份公开设计-20260830-01.md §4。
// 本文件集中承载全部新逻辑(engine.go 的 ⑦ 分支与 room.go 的字段声明除外),
// 以满足 §4 对 room.go / view.go / room_agent.go 三个超限文件的净增约束。
//
// 数据流(开启时):
//
//	killPlayer(seat, cause) → RolePubliclyRevealed(⑦ 分支)== true
//	  ├─► BuildClientState 按座位广播(dead_list / players[].role / 历史抽屉 ⚱)
//	  ├─► REST 房间详情 PublicPlayerState(同一判定,自动生效)
//	  ├─► wakeJudgeLocked → 法官宣告(公屏,LLM 挂掉走服务端拼装 fallback)
//	  └─► syncDeathRevealPriorsLocked → 全部 bot 的 RolePriorStore hard-set
//	        (修复 §130: ApplyDeathRevealPriorLocked 此前全库零调用)

import (
	"time"

	"LsmAgentGame/agent/wwtypes"
	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// ─────────────────── 房间级开关 setter(§92a 成对) ───────────────────

// SetRevealRoleOnDeath 设置 §20260830-01 房间级「死亡亮身份」开关。
// 由 RoomService.CreateRoomWithAgents 在房间创建时一次性调用(RegisterAgentSeats /
// SyncSeat → ForceStartIfReady 发牌之前);局中不可修改。锁内变体,公开入口包锁委托。
func (r *WerewolfRoom) SetRevealRoleOnDeath(enabled bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.setRevealRoleOnDeathLocked(enabled)
}

// setRevealRoleOnDeathLocked §92a 锁内变体。调用方必须已持 r.mu。
func (r *WerewolfRoom) setRevealRoleOnDeathLocked(enabled bool) {
	v := enabled
	r.revealRoleOnDeath = &v
}

// revealRoleOnDeathEffectiveLocked 解析开关生效值:显式配置(非 nil)优先,
// 未配置(纯人类直建房 / 观战者懒建房等未走 API 配置链路的房间)走
// cfgWerewolfRevealRoleOnDeathDefault(默认 true)。
// 调用方必须已持 r.mu。
func (r *WerewolfRoom) revealRoleOnDeathEffectiveLocked() bool {
	if r.revealRoleOnDeath != nil {
		return *r.revealRoleOnDeath
	}
	return cfgWerewolfRevealRoleOnDeathDefault()
}

// ─────────────────── 发牌拷贝点(§130 单一入口) ───────────────────

// newGameStateLocked 创建本房间的 GameState,并把房间级「死亡亮身份」开关
// 一次性解析拷贝进去(显式配置 > cfg 默认)。
//
// §130: 全库所有房间侧 NewGame( 调用点必须走本 helper,而不是直接 NewGame ——
// 否则任一路径漏拷开关,就会出现「同一房间两个判定语义」的分裂。实现核对命令:
//
//	git grep -n "NewGame(" ServerGo/game/werewolf -- ':!*_test.go'
//
// 必须在持 r.mu 时调用(§92a);restartGameLocked 原地重开同样经本 helper。
func (r *WerewolfRoom) newGameStateLocked(seed int64) *GameState {
	gs := NewGame(seed)
	gs.RevealRoleOnDeath = r.revealRoleOnDeathEffectiveLocked()
	return gs
}

// resetDeathRevealBookkeepingLocked 重开一局时清零死亡公开幂等簿记
// (deathRevealEmitted 随 restartGameLocked 的 newGameStateLocked 替换点一并清零)。
// 调用方必须已持 r.mu。
func (r *WerewolfRoom) resetDeathRevealBookkeepingLocked() {
	r.deathRevealEmitted = [MaxPlayers]bool{}
}

// ─────────────────── RolePrior 幂等 drain(修复 §130) ───────────────────

// syncDeathRevealPriorsLocked 幂等 drain:扫描全部座位,把「身份已对全场公开
// 且已死亡、但尚未写入先验表」的座位 hard-set 进全部 bot 的 RolePriorTable
// (ApplyDeathRevealPriorLocked,此前全库零调用 —— §130 清单项)。
//
// 不在 10 个 killPlayer 调用点逐点接线(必然漏点),改为在两个汇聚点冗余调用:
//  1. wakeJudgeLocked(法官唤醒,先于 buildJudgeSnapshotLocked);
//  2. wakeAllAgentsLocked(任何玩家 Agent 唤醒前)。
//
// 调用前置:必须已持 r.mu(§92a)。
// 幂等性:deathRevealEmitted 簿记保证重复调用零副作用,因此可在多个汇聚点
// 冗余调用(漏一个还有另一个兜底)。
func (r *WerewolfRoom) syncDeathRevealPriorsLocked() {
	if r == nil {
		return
	}
	gs := r.State
	if gs == nil {
		return
	}
	store := r.rolePriorStoreLocked()
	now := time.Now()
	for seat := 0; seat < MaxPlayers; seat++ {
		if r.deathRevealEmitted[seat] {
			continue
		}
		if gs.Players[seat].Alive || gs.Players[seat].DeathCause == "" {
			continue // 未死亡(含白痴翻牌免死)不进先验改写
		}
		if !gs.RolePubliclyRevealed(Seat(seat)) {
			continue // 开关关闭:仅 ②~⑥ 白名单命中者进入
		}
		store.ApplyDeathRevealPriorLocked(seat, gs.Roles[seat].String(), now)
		r.deathRevealEmitted[seat] = true
	}
}

// ─────────────────── 已公开身份快照(视图 / Agent 数据准备) ───────────────────

// revealedRolesSnapshotLocked 返回「已对全场公开身份」的座位 → 角色名 map。
// §135 单点判定:仅 RolePubliclyRevealed(含第 ⑦ 分支)命中的座位进入;
// 未公开座位不在 map 中 —— 禁止直接读 gs.Roles 原始数组推导。
//
// TODO(werewolf-agent 职责线,§20260830-01 §6.1): agent/wwtypes.GameContext
// 增加 RevealRoleOnDeath / RevealedRoles 字段后,在 room_agent.go
// buildAgentContextLocked 末尾接线:
//
//	gc.RevealRoleOnDeath = gs.RevealRoleOnDeath
//	gc.RevealedRoles = revealedRolesSnapshotLocked(gs)
func revealedRolesSnapshotLocked(gs *GameState) map[int]string {
	if gs == nil {
		return nil
	}
	var out map[int]string
	for i := 0; i < MaxPlayers; i++ {
		if !gs.RolePubliclyRevealed(Seat(i)) {
			continue
		}
		if out == nil {
			out = make(map[int]string, MaxPlayers)
		}
		out[i] = gs.Roles[i].String()
	}
	return out
}

// DeadRoleFact §20260830-01 — 一条已公开的死亡身份事实(法官宣告与 prompt 用)。
// 与设计文档 §5.1 的 wwjudge.DeadRoleFact 同形;数据由本包准备,接线点见
// buildJudgeSnapshotLocked 的 TODO。
type DeadRoleFact struct {
	Seat    int    // 座位号(0-indexed)
	Role    string // 角色名(werewolf/seer/witch/hunter/idiot/guard/knight/demon_hunter/villager)
	Cause   string // wolf/vote/witch_poison/hunter/suicide/duel/demon_hunter_misjudge/disconnected
	Verdict string // execution / death
}

// buildDeadRoleFactsLocked 收集全部「已对全场公开身份的死亡座位」事实(按座位序)。
// 开关关闭时仅含 ②~⑥ 白名单命中者(自爆/猎人开枪/骑士决斗/猎魔人/白痴翻牌后死亡);
// 开启时含全部确已死亡座位。
//
// TODO(werewolf-agent 职责线,§20260830-01 §5.1): agent/wwjudge.GameSnapshot
// 增加 RevealRoleOnDeath / RevealedDeadRoles []wwjudge.DeadRoleFact 字段后,
// 在 judge_summary_bridge.go buildJudgeSnapshotLocked 末尾接线:
//
//	snap.RevealRoleOnDeath = gs.RevealRoleOnDeath
//	snap.RevealedDeadRoles = <本函数结果投影为 wwjudge.DeadRoleFact>
func buildDeadRoleFactsLocked(gs *GameState) []DeadRoleFact {
	if gs == nil {
		return nil
	}
	var out []DeadRoleFact
	for i := 0; i < MaxPlayers; i++ {
		if gs.Players[i].Alive || gs.Players[i].DeathCause == "" {
			continue
		}
		if !gs.RolePubliclyRevealed(Seat(i)) {
			continue
		}
		out = append(out, DeadRoleFact{
			Seat:    i,
			Role:    gs.Roles[i].String(),
			Cause:   gs.Players[i].DeathCause,
			Verdict: gs.Players[i].DeathVerdict,
		})
	}
	return out
}

// ─────────────────── Manager 入口(service.AgentSeater → ws 层) ───────────────────

// SetRevealRoleOnDeath §20260830-01 — 把房间级「死亡亮身份」开关落到 in-memory
// WerewolfRoom。同 SetJudgeConfig 时序约束:必须在 RegisterAgentSeats / SyncSeat
// (→ ForceStartIfReady 发牌,newGameStateLocked 要读到它)之前调用。
//
// 与 SetAgentDifficulty 不同,这里房间不存在时**创建**而非静默返回:纯人类
// 狼人杀房间(无 agent_seats)的 in-memory 对象要等到创建者 SyncSeat 才惰性创建,
// 而本调用必须先于 SyncSeat —— SetJudgeConfig 同款 create-if-absent 范式。
func (m *WerewolfManager) SetRevealRoleOnDeath(roomID string, enabled bool) {
	m.mu.Lock()
	r, ok := m.rooms[roomID]
	if !ok {
		r = &WerewolfRoom{
			RoomID:         roomID,
			createdAt:      time.Now(),
			recentSpeeches: make([]wwtypes.SpeechEvent, 0, recentSpeechBufferSize),
			whisperInbox:   make(map[int][]wwtypes.WhisperEvent, MaxPlayers),
		}
		m.rooms[roomID] = r
	}
	m.mu.Unlock()

	r.mu.Lock()
	defer r.mu.Unlock()
	r.setRevealRoleOnDeathLocked(enabled)
	logger.L().Info("werewolf: reveal-role-on-death configured",
		zap.String("room_id", roomID),
		zap.Bool("enabled", enabled))
}
