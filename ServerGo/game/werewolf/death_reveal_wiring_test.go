// Package werewolf — death_reveal_wiring_test.go: §20260830-01 房间级「死亡亮身份」
// 接线与配置单测(设计文档 §10.2 W 系 + J-01 适配 + 视图下发)。
//
// 覆盖:
//
//	W-01  syncDeathRevealPriorsLocked 幂等(连调 3 次仅一条 1.0 条目)
//	W-02  端到端:killPlayer → wakeAllAgentsLocked → 存活 bot 先验 hard-set
//	      death_revealed(§130 行为级守护,防 ApplyDeathRevealPriorLocked 再被拔线)
//	W-03  关闭时 sync 仅对 ②~⑥ 白名单死者改写,普通死亡零改写
//	W-04  newGameStateLocked 拷贝开关 + resetDeathRevealBookkeepingLocked 清零簿记
//	W-05  revealedRolesSnapshotLocked 仅含单点判定命中座位,不泄漏 gs.Roles
//	J-01(适配)buildDeadRoleFactsLocked 开/关填充正确(wwjudge 侧字段由
//	      werewolf-agent 职责线接线,见 death_reveal.go TODO)
//	V-01  BuildClientStateWithRoom 恒定下发 reveal_role_on_death(false 有语义)
//	V-02  JSON wire 形状:false 显式出现(无 omitempty)
//	T-01/T-02 房间开关三态:SetRevealRoleOnDeath 显式 true/false;未配置走 cfg 默认
package werewolf

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"LsmAgentGame/agent/wwtypes"
)

// newDeathRevealRoom 起一间已开局并可选开启「死亡亮身份」的房间(自包含,不依赖 manager)。
func newDeathRevealRoom(t *testing.T, seed int64, revealOn bool) (*WerewolfRoom, *GameState) {
	t.Helper()
	gs := newFairnessGame(t, seed)
	gs.RevealRoleOnDeath = revealOn
	r := &WerewolfRoom{RoomID: "death-reveal-test", State: gs}
	r.Seats = gs.Seats
	return r, gs
}

// seedPriorTable 给一个 bot 座位建立初始先验表(sync 的改写目标)。
func seedPriorTable(r *WerewolfRoom, botSeat int) {
	now := time.Now()
	r.rolePriorStoreLocked().ComputeRolePriorForSeatLocked(botSeat, 0.5, now)
}

// deathRevealedEntryCount 统计 bot 表中 target 座位的条目数与 1.0 hard-set 条目。
func deathRevealedEntry(tbl *RolePriorTable, target int) (entries int, hard *RolePriorSingle) {
	if tbl == nil {
		return 0, nil
	}
	for i := range tbl.Entries {
		if tbl.Entries[i].TargetSeat != target {
			continue
		}
		entries++
		if tbl.Entries[i].EvidenceKind == "death_revealed" && tbl.Entries[i].PriorProb == 1.0 {
			h := tbl.Entries[i]
			hard = &h
		}
	}
	return entries, hard
}

// TestDeathReveal_W01_SyncIdempotent 连调 3 次幂等:target+role 仅一条 1.0 条目。
func TestDeathReveal_W01_SyncIdempotent(t *testing.T) {
	r, gs := newDeathRevealRoom(t, 4301, true)
	victim := firstLivingNonWolfNonSeer(gs)
	if victim == NoSeat {
		t.Skip("no suitable victim")
	}
	seedPriorTable(r, 0)
	if e := gs.killPlayer(victim, DeathCauseWolf); e != nil {
		t.Fatalf("kill: %v", e)
	}
	for i := 0; i < 3; i++ {
		r.syncDeathRevealPriorsLocked()
	}
	tbl := r.rolePriorStoreLocked().GetLocked(0)
	entries, hard := deathRevealedEntry(tbl, int(victim))
	if entries != 1 || hard == nil {
		t.Fatalf("幂等 drain 后 target 应仅剩 1 条 1.0 death_revealed 条目, got entries=%d hard=%v", entries, hard)
	}
	if hard.RoleGuess != gs.Roles[victim].String() {
		t.Fatalf("hard-set 角色 = %q, want %q", hard.RoleGuess, gs.Roles[victim].String())
	}
	if !r.deathRevealEmitted[victim] {
		t.Fatalf("deathRevealEmitted 簿记应置位")
	}
}

// TestDeathReveal_W02_PriorHardSetOnDeath 端到端:killPlayer 后经
// wakeAllAgentsLocked(汇聚点 2)唤醒,存活 bot 先验表 hard-set death_revealed。
func TestDeathReveal_W02_PriorHardSetOnDeath(t *testing.T) {
	r, gs := newDeathRevealRoom(t, 4302, true)
	victim := firstLivingNonWolfNonSeer(gs)
	if victim == NoSeat {
		t.Skip("no suitable victim")
	}
	seedPriorTable(r, 0)
	seedPriorTable(r, 1)
	if e := gs.killPlayer(victim, DeathCauseWitchPoison); e != nil {
		t.Fatalf("kill: %v", e)
	}
	m := &WerewolfManager{}
	m.wakeAllAgentsLocked(r, "state_change", wwtypes.GameContext{})
	for _, bot := range []int{0, 1} {
		tbl := r.rolePriorStoreLocked().GetLocked(bot)
		entries, hard := deathRevealedEntry(tbl, int(victim))
		if entries != 1 || hard == nil {
			t.Fatalf("bot %d 先验表应 hard-set death_revealed, got entries=%d hard=%v", bot, entries, hard)
		}
	}
}

// TestDeathReveal_W03_Disabled_SyncOnlyWhitelist 关闭时 sync 仅对 ②~⑥ 白名单
// 死者改写;普通死亡(狼刀/毒杀)零改写。
func TestDeathReveal_W03_Disabled_SyncOnlyWhitelist(t *testing.T) {
	r, gs := newDeathRevealRoom(t, 4303, false)
	normal := firstLivingNonWolfNonSeer(gs)
	if normal == NoSeat {
		t.Skip("no suitable victim")
	}
	wolf := anyLivingWolf(gs)
	if wolf == NoSeat || wolf == normal {
		t.Skip("no living wolf distinct from victim")
	}
	seedPriorTable(r, 0)
	// 观察座位选一个非死者(self-exclude:bot 表不含自己的 target 条目)。
	observer := 0
	for i := 0; i < MaxPlayers; i++ {
		if Seat(i) != normal && Seat(i) != wolf && gs.HasActorAt(Seat(i)) {
			observer = i
			break
		}
	}
	seedPriorTable(r, observer)
	if e := gs.killPlayer(normal, DeathCauseWolf); e != nil {
		t.Fatalf("kill normal: %v", e)
	}
	// 自爆狼(③ 白名单)死亡。
	if e := gs.killPlayer(wolf, DeathCauseSuicide); e != nil {
		t.Fatalf("kill wolf: %v", e)
	}
	r.syncDeathRevealPriorsLocked()

	tbl := r.rolePriorStoreLocked().GetLocked(observer)
	if _, hard := deathRevealedEntry(tbl, int(normal)); hard != nil {
		t.Fatalf("关闭开关:普通死亡不得 hard-set")
	}
	entries, hard := deathRevealedEntry(tbl, int(wolf))
	if entries != 1 || hard == nil {
		t.Fatalf("关闭开关:白名单(自爆狼)死者应 hard-set, got entries=%d hard=%v", entries, hard)
	}
	if !r.deathRevealEmitted[wolf] || r.deathRevealEmitted[normal] {
		t.Fatalf("簿记应仅覆盖白名单死者")
	}
}

// TestDeathReveal_W04_NewGameCopiesFlag newGameStateLocked 拷贝开关(显式 true /
// false / 未配置走 cfg 默认)+ resetDeathRevealBookkeepingLocked 清零簿记。
func TestDeathReveal_W04_NewGameCopiesFlag(t *testing.T) {
	for _, tc := range []struct {
		set   *bool
		name  string
		check func(t *testing.T, got, cfgDefault bool)
	}{
		{nil, "unset", func(t *testing.T, got, cfgDefault bool) {
			if got != cfgDefault {
				t.Fatalf("未配置房间应走 cfg 默认(%v), got %v", cfgDefault, got)
			}
		}},
		{boolPtr(true), "explicit-true", func(t *testing.T, got, _ bool) {
			if !got {
				t.Fatalf("显式 true 应拷贝为 true")
			}
		}},
		{boolPtr(false), "explicit-false", func(t *testing.T, got, _ bool) {
			if got {
				t.Fatalf("显式 false 应拷贝为 false(§135 竞技规则)")
			}
		}},
	} {
		r := &WerewolfRoom{RoomID: "w04-" + tc.name}
		if tc.set != nil {
			r.SetRevealRoleOnDeath(*tc.set)
		}
		gs := r.newGameStateLocked(99)
		tc.check(t, gs.RevealRoleOnDeath, cfgWerewolfRevealRoleOnDeathDefault())
	}

	// 重开清零:簿记置位后 reset 应全清。
	r, _ := newDeathRevealRoom(t, 4304, true)
	r.deathRevealEmitted[3] = true
	r.deathRevealEmitted[7] = true
	r.resetDeathRevealBookkeepingLocked()
	for seat := 0; seat < MaxPlayers; seat++ {
		if r.deathRevealEmitted[seat] {
			t.Fatalf("reset 后簿记应为全零, seat %d 仍置位", seat)
		}
	}
}

// TestDeathReveal_W05_GameContextRevealedRoles revealedRolesSnapshotLocked 仅含
// 单点判定命中座位,不泄漏 gs.Roles 原始值(关闭 + 普通死亡 → 不在 map)。
func TestDeathReveal_W05_GameContextRevealedRoles(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		gs := newFairnessGame(t, 4305)
		gs.RevealRoleOnDeath = enabled
		victim := firstLivingNonWolfNonSeer(gs)
		if victim == NoSeat {
			t.Skip("no suitable victim")
		}
		if e := gs.killPlayer(victim, DeathCauseWolf); e != nil {
			t.Fatalf("kill: %v", e)
		}
		snap := revealedRolesSnapshotLocked(gs)
		_, hit := snap[int(victim)]
		if hit != enabled {
			t.Fatalf("enabled=%v: victim 命中=%v", enabled, hit)
		}
		if enabled && snap[int(victim)] != gs.Roles[victim].String() {
			t.Fatalf("公开角色名不符: %q", snap[int(victim)])
		}
		// 未死亡座位绝不在 map(无论开关)。
		for i := 0; i < MaxPlayers; i++ {
			if gs.AliveSeat(Seat(i)) {
				if _, ok := snap[i]; ok {
					t.Fatalf("enabled=%v: 存活座位 %d 不得出现在已公开 map", enabled, i)
				}
			}
		}
	}
}

// TestDeathReveal_J01_DeadRoleFactsFilled buildDeadRoleFactsLocked(设计文档
// §5.1 数据准备,wwjudge.GameSnapshot 接线见 werewolf-agent 职责线 TODO):
// 开启 → 全部死亡座位;关闭 → 仅白名单命中者;字段 seat/role/cause/verdict 正确。
func TestDeathReveal_J01_DeadRoleFactsFilled(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		gs := newFairnessGame(t, 4306)
		gs.RevealRoleOnDeath = enabled
		normal := firstLivingNonWolfNonSeer(gs)
		wolf := anyLivingWolf(gs)
		if normal == NoSeat || wolf == NoSeat || wolf == normal {
			t.Skip("no distinct victims")
		}
		if e := gs.killPlayer(normal, DeathCauseVote); e != nil {
			t.Fatalf("kill: %v", e)
		}
		if e := gs.killPlayer(wolf, DeathCauseSuicide); e != nil {
			t.Fatalf("kill wolf: %v", e)
		}
		facts := buildDeadRoleFactsLocked(gs)
		var normalFact, wolfFact *DeadRoleFact
		for i := range facts {
			switch facts[i].Seat {
			case int(normal):
				normalFact = &facts[i]
			case int(wolf):
				wolfFact = &facts[i]
			}
		}
		if enabled {
			if normalFact == nil {
				t.Fatalf("enabled=true: 普通死亡必须入 facts")
			}
			if normalFact.Role != gs.Roles[normal].String() || normalFact.Cause != DeathCauseVote ||
				normalFact.Verdict != DeathVerdictExecution {
				t.Fatalf("fact 字段不符: %+v", *normalFact)
			}
		} else if normalFact != nil {
			t.Fatalf("enabled=false: 普通死亡不得入 facts")
		}
		if wolfFact == nil {
			t.Fatalf("自爆狼(③ 白名单)必须入 facts(无论开关)")
		}
		if wolfFact.Role != "werewolf" || wolfFact.Cause != DeathCauseSuicide {
			t.Fatalf("wolf fact 字段不符: %+v", *wolfFact)
		}
	}
}

// TestDeathReveal_V01_ViewFlagDownstream BuildClientStateWithRoom 恒定下发
// reveal_role_on_death(玩家/观战者视角一致,false 有语义不省略)。
func TestDeathReveal_V01_ViewFlagDownstream(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		r, gs := newDeathRevealRoom(t, 4307, enabled)
		victim := firstLivingNonWolfNonSeer(gs)
		if victim == NoSeat {
			t.Skip("no suitable victim")
		}
		if e := gs.killPlayer(victim, DeathCauseWolf); e != nil {
			t.Fatalf("kill: %v", e)
		}
		for _, viewer := range []int{-1, 0, 1} {
			cs := BuildClientStateWithRoom(r.RoomID, r, viewer)
			if cs == nil {
				t.Fatalf("viewer=%d 视图为 nil", viewer)
			}
			if cs.RevealRoleOnDeath != enabled {
				t.Fatalf("viewer=%d reveal_role_on_death=%v, want %v", viewer, cs.RevealRoleOnDeath, enabled)
			}
			// 公平性:不同 viewer 的死者角色一致性(单点判定派生,与开关一致)。
			wantRole := ""
			if enabled {
				wantRole = gs.Roles[victim].String()
			}
			if int(victim) != viewer && cs.Players[victim].Role != wantRole {
				t.Fatalf("viewer=%d 死者 role=%q, want %q", viewer, cs.Players[victim].Role, wantRole)
			}
		}
	}
}

// TestDeathReveal_V02_WireShapeFalseExplicit JSON wire:false 显式出现(无 omitempty)。
func TestDeathReveal_V02_WireShapeFalseExplicit(t *testing.T) {
	r, _ := newDeathRevealRoom(t, 4308, false)
	cs := BuildClientStateWithRoom(r.RoomID, r, 2)
	if cs == nil {
		t.Fatal("view nil")
	}
	raw, err := json.Marshal(cs)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"reveal_role_on_death":false`) {
		t.Fatalf("false 必须显式下发(无 omitempty), raw=%s", string(raw)[:200])
	}
}

// TestDeathReveal_T01_RoomSetterTriState 房间开关三态:setter 显式 true/false;
// 未配置走 cfg 默认(revealRoleOnDeathEffectiveLocked)。
func TestDeathReveal_T01_RoomSetterTriState(t *testing.T) {
	r := &WerewolfRoom{RoomID: "t01"}
	// 未配置 → cfg 默认。
	if got, want := r.revealRoleOnDeathEffectiveLocked(), cfgWerewolfRevealRoleOnDeathDefault(); got != want {
		t.Fatalf("未配置生效值=%v, want cfg 默认 %v", got, want)
	}
	r.SetRevealRoleOnDeath(false)
	if r.revealRoleOnDeathEffectiveLocked() {
		t.Fatalf("显式 false 应生效为 false")
	}
	r.SetRevealRoleOnDeath(true)
	if !r.revealRoleOnDeathEffectiveLocked() {
		t.Fatalf("显式 true 应生效为 true")
	}
}

// --- helpers ---

func boolPtr(b bool) *bool { return &b }
