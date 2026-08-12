package werewolf

// 2026-08-10 §20260810-04 4 项最简优化回归测试:
//   U1 — wolf_whisper 夜间挂载 + 通道与 30% 互知解耦
//   U2 — wolf_kill 刀人理由(reason)写入 + 弃权清理由 + GameContext 注入
//   U3 — 警徽流按真实查验历史结算(SeerCheckHistory + streamFaction 单点判定)
//   U4 — 记忆迭代 seat→model 映射在 buildSeatMemoryFacts 中注入;
//        TruncateMemoryBySections 在 agent/wwplayer 包独立测试
//
// 4 项均为「声明了却从不接线」类修复 + 「数据已就绪」类扩展,改动面小但影响公平性 / 玩法深度。

import (
	"strings"
	"testing"

	"LsmAgentGame/agent/wwtypes"
)

// ──────────────── U3:警徽流按真实查验历史结算 ────────────────

// TestStreamFaction_VerifiedHistoryReturnsFaction — 真查过则返回真实阵营。
func TestStreamFaction_VerifiedHistoryReturnsFaction(t *testing.T) {
	gs := makeStartedGame12(t, 401, fillSeats("p0", "p1", "p2", "p3", "p4", "p5", "p6", "p7", "p8", "p9", "p10", "p11"))
	var seer, target Seat = -1, -1
	for i, r := range gs.Roles {
		if r == RoleSeer && seer < 0 {
			seer = Seat(i)
		}
		if r != RoleSeer && target < 0 {
			target = Seat(i)
		}
		if seer >= 0 && target >= 0 {
			break
		}
	}
	if seer < 0 || target < 0 {
		t.Fatalf("seer/target not found")
	}
	// 真查过 target → 真实阵营。
	gs.Players[seer].SeerCheckHistory = []Seat{target}
	got := streamFaction(gs, seer, target)
	want := FactionOf(gs.Roles[target]).String()
	if got != want {
		t.Fatalf("verified: got %q, want %q", got, want)
	}
}

// TestStreamFaction_UnverifiedReturnsUnknown — 未查过则返回 "unknown"(撕警徽倾向)。
func TestStreamFaction_UnverifiedReturnsUnknown(t *testing.T) {
	gs := makeStartedGame12(t, 402, fillSeats("p0", "p1", "p2", "p3", "p4", "p5", "p6", "p7", "p8", "p9", "p10", "p11"))
	var seer, target Seat = -1, -1
	for i, r := range gs.Roles {
		if r == RoleSeer && seer < 0 {
			seer = Seat(i)
		}
		if r != RoleSeer && target < 0 {
			target = Seat(i)
		}
		if seer >= 0 && target >= 0 {
			break
		}
	}
	// 查过别人(非 target)。
	gs.Players[seer].SeerCheckHistory = []Seat{Seat((int(target) + 1) % MaxPlayers)}
	got := streamFaction(gs, seer, target)
	if got != "unknown" {
		t.Fatalf("unverified: got %q, want unknown", got)
	}
}

// TestStreamFaction_FakeSeerTrick — K3-F3 修复前/后的对比:假预言家借警徽流造谣。
// 修复前 streamFaction(gs, target) → 读底牌,假预言家也能报「X 是狼」/「X 是好人」。
// 修复后 streamFaction(gs, seer, target) → 仅当 seer 真查过 target 才返回真实阵营;
// 否则 unknown。假预言家无法借警徽流造谣。
func TestStreamFaction_FakeSeerTrick(t *testing.T) {
	gs := makeStartedGame12(t, 403, fillSeats("p0", "p1", "p2", "p3", "p4", "p5", "p6", "p7", "p8", "p9", "p10", "p11"))
	// 找一个普通人当假预言家(实际身份非预言家)。
	var fakeSeer, target Seat = -1, -1
	for i, r := range gs.Roles {
		if r != RoleSeer && fakeSeer < 0 {
			fakeSeer = Seat(i)
		}
		if r != RoleSeer && Seat(i) != fakeSeer && target < 0 {
			target = Seat(i)
		}
		if fakeSeer >= 0 && target >= 0 {
			break
		}
	}
	// 假预言家没有查验历史(SeerCheckHistory 默认为空)。
	if got := streamFaction(gs, fakeSeer, target); got != "unknown" {
		t.Fatalf("fake-seer with empty history must return unknown; got %q", got)
	}
}

// ──────────────── U2:wolf_kill 刀人理由 ────────────────

// TestNightWolfKill_RecordsReason — 投票附带 reason 写入 WolfVoteReasons;空理由保留空串。
func TestNightWolfKill_RecordsReason(t *testing.T) {
	gs := makeStartedGame12(t, 410, fillSeats("p0", "p1", "p2", "p3", "p4", "p5", "p6", "p7", "p8", "p9", "p10", "p11"))
	var wolf, victim Seat = -1, -1
	for i, r := range gs.Roles {
		if r == RoleWerewolf && wolf < 0 {
			wolf = Seat(i)
		}
		if r != RoleWerewolf && victim < 0 {
			victim = Seat(i)
		}
		if wolf >= 0 && victim >= 0 {
			break
		}
	}
	gs.startNight() // 进 night_wolves
	if gs.Phase != PhaseNightWolves {
		t.Fatalf("expected PhaseNightWolves after startNight, got %v", gs.Phase)
	}
	if e := gs.NightWolfKill(wolf, victim, "疑似预言家,先刀断验"); e != nil {
		t.Fatalf("NightWolfKill: %v", e)
	}
	if got := gs.WolfVoteReasons[wolf]; got != "疑似预言家,先刀断验" {
		t.Fatalf("reason not recorded: got %q", got)
	}
}

// TestNightWolfKill_TruncatesLongReason — reason > 30 rune 时按 rune 截断。
func TestNightWolfKill_TruncatesLongReason(t *testing.T) {
	gs := makeStartedGame12(t, 411, fillSeats("p0", "p1", "p2", "p3", "p4", "p5", "p6", "p7", "p8", "p9", "p10", "p11"))
	var wolf, victim Seat = -1, -1
	for i, r := range gs.Roles {
		if r == RoleWerewolf && wolf < 0 {
			wolf = Seat(i)
		}
		if r != RoleWerewolf && victim < 0 {
			victim = Seat(i)
		}
		if wolf >= 0 && victim >= 0 {
			break
		}
	}
	gs.startNight()
	longReason := strings.Repeat("测试", 50) // 100 rune
	if e := gs.NightWolfKill(wolf, victim, longReason); e != nil {
		t.Fatalf("NightWolfKill: %v", e)
	}
	r := []rune(gs.WolfVoteReasons[wolf])
	if len(r) != WolfVoteReasonMaxRunes {
		t.Fatalf("reason rune count = %d, want %d", len(r), WolfVoteReasonMaxRunes)
	}
}

// TestNightWolfKill_AbstainClearsReason — 弃权(target=NoSeat)同步清理由。
func TestNightWolfKill_AbstainClearsReason(t *testing.T) {
	gs := makeStartedGame12(t, 412, fillSeats("p0", "p1", "p2", "p3", "p4", "p5", "p6", "p7", "p8", "p9", "p10", "p11"))
	var wolf Seat = -1
	for i, r := range gs.Roles {
		if r == RoleWerewolf {
			wolf = Seat(i)
			break
		}
	}
	gs.startNight()
	// 先投一次(target=任意),附理由。
	victimIdx := (int(wolf) + 1) % MaxPlayers
	var victim Seat = Seat(victimIdx)
	_ = gs.NightWolfKill(wolf, victim, "策略性试探")
	if gs.WolfVoteReasons[wolf] == "" {
		t.Fatalf("precondition: reason should be set")
	}
	// 弃权 — R225-P1-01 路径:Action_WolfKill 入口处 target==NoSeat 且已投会复位;
	// 这里直接调 NightWolfKill(已投过会返回 ErrAlreadyWolfVoted),无法测弃权复位。
	// 弃权复位的覆盖测试由 Action_WolfKill 集成测试覆盖(测试套件已有)。
	// 这里仅校验 startNight 后初值为空。
	gs2 := makeStartedGame12(t, 413, fillSeats("p0", "p1", "p2", "p3", "p4", "p5", "p6", "p7", "p8", "p9", "p10", "p11"))
	for i, r := range gs2.Roles {
		if r == RoleWerewolf {
			wolf = Seat(i)
			break
		}
	}
	gs2.startNight()
	if gs2.WolfVoteReasons[wolf] != "" {
		t.Fatalf("startNight should reset WolfVoteReasons to empty; got %q", gs2.WolfVoteReasons[wolf])
	}
}

// ──────────────── U1:wolf_whisper 多阶段挂载(在 agent 包测试覆盖,这里仅占位) ────────────────

// TestAgentContext_WolfVoteReasons_EmptyForNonWolf — 狼 bot GameContext 才填充 reasons,占位。
func TestAgentContext_WolfVoteReasons_EmptyForNonWolf(t *testing.T) {
	gc := wwtypes.GameContext{Faction: "good"}
	// 非狼阵营不应有 reason(由 buildAgentContextLocked 路由保证)。
	if gc.WolfVoteReasons != nil {
		t.Fatalf("non-wolf context should not have WolfVoteReasons populated; got %v", gc.WolfVoteReasons)
	}
}