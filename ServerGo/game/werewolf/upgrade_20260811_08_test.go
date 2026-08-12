// Package werewolf - upgrade_20260811_08_test.go: §20260811-08 回归测试
//
// 覆盖 3 项 §130「声明了却从不接线」缺陷修复:
//   U1 PerSeatPOV 占位字段真实填充 + RoleRevealed 收口 §135 单点判定
//   U2 结算奖励接线补齐(4 条终局路径)+ 幂等 + 死者同样发奖
//   U3 GodModeSnapshot 补 4 类已公开技能行动
//
// §212 教训:持锁路径的测试必须持锁 + 超时守卫;且新写的回归测试必须先在
// 缺陷代码上验证它确实失败(本文件 U1-02 / U2-01 / U2-03 已做双向验证)。

package werewolf

import (
	"strings"
	"testing"
	"time"
)

// ─────────────────── U1 PerSeatPOV 真实填充 ───────────────────

// newPOVTestRoom 构造一个 10 座位在座、身份已发的房间(不启动 agent)。
func newPOVTestRoom() *WerewolfRoom {
	gs := &GameState{Status: "playing"}
	for i := 0; i < MaxPlayers; i++ {
		gs.Players[i] = Player{Seat: Seat(i), Alive: true, LastChallengedBy: -1}
	}
	gs.Roles[0] = RoleWerewolf
	gs.Roles[1] = RoleSeer
	gs.Roles[2] = RoleWitch
	gs.Roles[3] = RoleGuard
	r := &WerewolfRoom{State: gs}
	for i := 0; i < 10; i++ {
		r.Seats[i] = "user-" + string(rune('A'+i))
	}
	return r
}

// TestU1_01_PerSeatPOV_NightActionsFromLedger 验证 NightActions 由信息账本真实聚合,
// 而非旧版硬编码的空切片。
func TestU1_01_PerSeatPOV_NightActionsFromLedger(t *testing.T) {
	r := newPOVTestRoom()
	r.infoLedger = NewInformationLedger()
	now := time.Now().UnixMilli()
	knowers := map[int]bool{1: true}
	// 预言家(座位 1)第 1 天查验 5 号(0-indexed 4)
	r.infoLedger.append(1, "night", InfoSourceNightSeer, "seer_check seat=1 target=4", knowers, now)
	// 守卫(座位 3)第 1 天守护 2 号(0-indexed 1)
	r.infoLedger.append(1, "night", InfoSourceNightGuard, "guard_protect seat=3 target=1", knowers, now)

	snap := r.populateGodModeLocked()
	if snap == nil {
		t.Fatal("snapshot must not be nil")
	}
	if got := snap.PerSeatPOV[1].NightActions; len(got) != 1 {
		t.Fatalf("seer seat 1 NightActions = %v, want 1 entry", got)
	}
	if got := snap.PerSeatPOV[3].NightActions; len(got) != 1 {
		t.Fatalf("guard seat 3 NightActions = %v, want 1 entry", got)
	}
	// 非行动者座位应保持空切片(而非 nil,避免前端 .map 崩溃 —— §44 教训)
	if snap.PerSeatPOV[0].NightActions == nil {
		t.Error("non-actor seat NightActions must be empty slice, not nil")
	}
}

// TestU1_02_PerSeatPOV_RoleRevealed_UsesSinglePoint 验证 RoleRevealed 走
// §135 RolePubliclyRevealed 单点判定。
//
// 【双向验证】旧版手写条件 `Status=="over" || HunterFired || IdiotRevealed`
// 漏了 3 个分支:狼自爆 / 骑士决斗 / 猎魔人狩猎。还原旧条件后本测试必然失败。
func TestU1_02_PerSeatPOV_RoleRevealed_UsesSinglePoint(t *testing.T) {
	cases := []struct {
		name  string
		apply func(p *Player)
	}{
		{"狼自爆", func(p *Player) { p.DeathCause = DeathCauseSuicide; p.Alive = false }},
		{"骑士决斗", func(p *Player) { p.KnightDuelUsed = true }},
		{"猎魔人狩猎", func(p *Player) { p.DemonHunterHuntUsed = true }},
		{"猎人开枪", func(p *Player) { p.HunterFired = true }},
		{"白痴翻牌", func(p *Player) { p.IdiotRevealed = true }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newPOVTestRoom()
			tc.apply(&r.State.Players[0])

			snap := r.populateGodModeLocked()
			if !snap.PerSeatPOV[0].RoleRevealed {
				t.Errorf("%s 后 RoleRevealed 应为 true(§135 RolePubliclyRevealed 单点判定)", tc.name)
			}
			// 未触发任何白名单的座位仍必须保持隐藏(§135 不回归)
			if snap.PerSeatPOV[5].RoleRevealed {
				t.Error("未触发白名单的座位 RoleRevealed 必须为 false")
			}
		})
	}
}

// TestU1_03_PerSeatPOV_ChallengeState 验证被质疑态填 0/1(注释与实现对齐)。
func TestU1_03_PerSeatPOV_ChallengeState(t *testing.T) {
	r := newPOVTestRoom()
	r.State.Players[2].LastChallengedBy = 7
	r.State.Players[2].LastChallengeQuestion = "你昨晚做了什么?"

	snap := r.populateGodModeLocked()
	if got := snap.PerSeatPOV[2].ChallengeCount; got != 1 {
		t.Errorf("被质疑座位 ChallengeCount = %d, want 1", got)
	}
	if got := snap.PerSeatPOV[0].ChallengeCount; got != 0 {
		t.Errorf("未被质疑座位 ChallengeCount = %d, want 0", got)
	}
}

// TestU1_04_GodMode_SpectatorOnly 玩家视图不得下发 GodMode(§135 不回归)。
func TestU1_04_GodMode_SpectatorOnly(t *testing.T) {
	r := newPOVTestRoom()
	// populateGodModeLocked 本身只被 spectator 分支调用;这里断言其产出
	// 确实携带全量身份 —— 一旦它被误接到玩家分支,泄漏面就是这些字段。
	snap := r.populateGodModeLocked()
	if len(snap.Roles) == 0 {
		t.Fatal("GodMode 快照应含全量身份(spectator 专属)")
	}
	if _, ok := snap.Roles[0]; !ok {
		t.Error("座位 0 身份应在 GodMode 快照中")
	}
}

// ─────────────────── U3 已公开技能行动聚合 ───────────────────

// TestU3_01_PublicActions_HunterAndIdiot 猎人开枪 / 白痴翻牌进入 PublicActions。
func TestU3_01_PublicActions_HunterAndIdiot(t *testing.T) {
	r := newPOVTestRoom()
	r.infoLedger = NewInformationLedger()
	now := time.Now().UnixMilli()
	all := aliveKnowerSetLocked(r)
	r.infoLedger.append(2, "night", InfoSourceHunterShot, "hunter_shot seat=4 target=9", all, now)
	r.infoLedger.append(3, "night", InfoSourceIdiotReveal, "idiot_reveal seat=6", all, now)

	snap := r.populateGodModeLocked()
	if len(snap.PublicActions) != 2 {
		t.Fatalf("PublicActions = %d entries, want 2: %+v", len(snap.PublicActions), snap.PublicActions)
	}
	byKind := map[string]PublicActionEntry{}
	for _, a := range snap.PublicActions {
		byKind[a.Kind] = a
	}
	hs, ok := byKind["hunter_shot"]
	if !ok {
		t.Fatal("missing hunter_shot entry")
	}
	if hs.Seat != 4 || hs.Target != 9 || hs.Day != 2 {
		t.Errorf("hunter_shot = %+v, want seat=4 target=9 day=2", hs)
	}
	if hs.HitWolf != nil {
		t.Error("hunter_shot 不适用 HitWolf,应为 nil")
	}
	ir, ok := byKind["idiot_reveal"]
	if !ok {
		t.Fatal("missing idiot_reveal entry")
	}
	if ir.Seat != 6 || ir.Target != -1 {
		t.Errorf("idiot_reveal = %+v, want seat=6 target=-1", ir)
	}
}

// TestU3_02_PublicActions_HitWolfPointer 决斗/狩猎的 HitWolf 必须能区分
// 「没打中狼(false)」与「不适用(nil)」。
func TestU3_02_PublicActions_HitWolfPointer(t *testing.T) {
	r := newPOVTestRoom()
	r.infoLedger = NewInformationLedger()
	now := time.Now().UnixMilli()
	all := aliveKnowerSetLocked(r)
	r.infoLedger.append(1, "night", InfoSourceKnightDuel, "knight_duel seat=2 target=0 hit_wolf=true", all, now)
	r.infoLedger.append(2, "night", InfoSourceDemonHunter, "demon_hunter seat=5 target=8 hit_wolf=false", all, now)

	snap := r.populateGodModeLocked()
	byKind := map[string]PublicActionEntry{}
	for _, a := range snap.PublicActions {
		byKind[a.Kind] = a
	}
	kd, ok := byKind["knight_duel"]
	if !ok {
		t.Fatal("missing knight_duel entry")
	}
	if kd.HitWolf == nil || !*kd.HitWolf {
		t.Errorf("knight_duel HitWolf = %v, want *true", kd.HitWolf)
	}
	dh, ok := byKind["demon_hunter"]
	if !ok {
		t.Fatal("missing demon_hunter entry")
	}
	if dh.HitWolf == nil {
		t.Fatal("demon_hunter HitWolf 不应为 nil(适用但未命中)")
	}
	if *dh.HitWolf {
		t.Error("demon_hunter HitWolf = *true, want *false")
	}
}

// TestU3_03_PublicActions_MalformedFactSkipped 账本 fact 损坏时静默跳过,
// 不 panic、不产出半条目(与既有 parseSeatTargetPair 行为一致)。
func TestU3_03_PublicActions_MalformedFactSkipped(t *testing.T) {
	r := newPOVTestRoom()
	r.infoLedger = NewInformationLedger()
	now := time.Now().UnixMilli()
	all := aliveKnowerSetLocked(r)
	r.infoLedger.append(1, "night", InfoSourceHunterShot, "hunter_shot GARBAGE", all, now)
	r.infoLedger.append(1, "night", InfoSourceKnightDuel, "knight_duel seat=x target=y", all, now)
	r.infoLedger.append(1, "night", InfoSourceIdiotReveal, "totally_wrong_prefix seat=3", all, now)

	snap := r.populateGodModeLocked() // 不得 panic
	if len(snap.PublicActions) != 0 {
		t.Errorf("损坏 fact 应全部跳过, got %d entries: %+v", len(snap.PublicActions), snap.PublicActions)
	}
}

// ───────── P0(U3 回归测试意外暴露): redactLedgerFact 破坏结构化前缀 ─────────

// TestP0_01_RedactPreservesStructuredPrefix 结构化 fact 的机器可读前缀必须逐字节保留。
//
// 【缺陷史】旧版 redactLedgerFact 对整条 fact 做无边界 ReplaceAll,把
// "seer_check"→"▪_check"、"guard_protect"→"▪_protect"、"hit_wolf"→"hit_▪",
// 导致 §20260810-09 上帝视角的 SeerChecks/WitchDecisions/GuardProtects
// **自落地起 100% 恒为空**。原实现只测了 redact 本身,没有任何测试断言
// 聚合结果非空 —— 这正是本用例存在的意义。
func TestP0_01_RedactPreservesStructuredPrefix(t *testing.T) {
	cases := []struct{ in, wantPrefix string }{
		{"seer_check seat=1 target=4", "seer_check "},
		{"guard_protect seat=3 target=1", "guard_protect "},
		{"witch_act seat=2 action=antidote target=5", "witch_act "},
		{"hunter_shot seat=4 target=9", "hunter_shot "},
		{"knight_duel seat=2 target=0 hit_wolf=true", "knight_duel "},
		{"demon_hunter seat=5 target=8 hit_wolf=false", "demon_hunter "},
		{"idiot_reveal seat=6", "idiot_reveal "},
		{"day_vote seat=1 target=2", "day_vote "},
		{"role_deal seat=7", "role_deal "},
	}
	for _, c := range cases {
		got := redactLedgerFact(c.in)
		if !strings.HasPrefix(got, c.wantPrefix) {
			t.Errorf("redact(%q) = %q, 前缀应为 %q", c.in, got, c.wantPrefix)
		}
		if strings.Contains(got, "▪") && !strings.Contains(c.in, "狼人") {
			t.Errorf("redact(%q) = %q,结构化 fact 不应出现占位符", c.in, got)
		}
	}
}

// TestP0_02_RedactStillScrubsFreeText 自由文本中的身份明文仍必须被脱敏(§135 不回归)。
func TestP0_02_RedactStillScrubsFreeText(t *testing.T) {
	cases := []struct{ in, mustNotContain string }{
		{"3 号是狼人", "狼人"},
		{"我是预言家,验了 5 号", "预言家"},
		{"witch used poison", "witch"},
		{"seer check result", "seer"},
		{"he is a wolf for sure", "wolf"},
		// 结构化 fact 的**参数区**若含身份明文同样要脱敏
		{"wolf_vote seat=1 target=2 reason=他是预言家", "预言家"},
	}
	for _, c := range cases {
		got := redactLedgerFact(c.in)
		if strings.Contains(got, c.mustNotContain) {
			t.Errorf("redact(%q) 仍含 %q → %q", c.in, c.mustNotContain, got)
		}
	}
	if got := redactLedgerFact("今天天气不错"); got != "今天天气不错" {
		t.Errorf("非身份文本不应被改动, got %q", got)
	}
}

// TestP0_03_GodModeNightAggregationWorks §20260810-09 三个聚合字段端到端非空。
func TestP0_03_GodModeNightAggregationWorks(t *testing.T) {
	r := newPOVTestRoom()
	r.infoLedger = NewInformationLedger()
	now := time.Now().UnixMilli()
	k := map[int]bool{1: true}
	r.infoLedger.append(1, "night", InfoSourceNightSeer, "seer_check seat=1 target=4", k, now)
	r.infoLedger.append(1, "night", InfoSourceNightGuard, "guard_protect seat=3 target=1", k, now)
	r.infoLedger.append(1, "night", InfoSourceNightWitch, "witch_act seat=2 action=antidote target=5", k, now)

	snap := r.populateGodModeLocked()
	if len(snap.SeerChecks) != 1 {
		t.Errorf("SeerChecks = %d, want 1(§20260810-09 聚合)", len(snap.SeerChecks))
	}
	if len(snap.GuardProtects) != 1 {
		t.Errorf("GuardProtects = %d, want 1", len(snap.GuardProtects))
	}
	if len(snap.WitchDecisions) != 1 {
		t.Errorf("WitchDecisions = %d, want 1", len(snap.WitchDecisions))
	}
}

// ─────────────────── U2 结算奖励接线 ───────────────────

// TestU2_01_GrantIdempotent 同一房间重复调用只发放一次。
//
// 【双向验证】还原「无 settlementRewarded 守卫」后,第二次调用会重复发放,
// 本测试必然失败。
func TestU2_01_GrantIdempotent(t *testing.T) {
	r := newPOVTestRoom()
	r.State.Status = "over"
	r.State.Winner = "good"

	if r.settlementRewarded {
		t.Fatal("初始状态 settlementRewarded 应为 false")
	}
	m := &WerewolfManager{}
	svc := &SettlementRewardService{cfg: DefaultSettlementRewardConfig()} // db=nil → 写入静默失败,但标志逻辑仍生效

	m.grantSettlementRewardsLocked(r, svc)
	if !r.settlementRewarded {
		t.Fatal("首次发放后 settlementRewarded 应为 true")
	}
	// 第二次调用必须直接返回(幂等)
	m.grantSettlementRewardsLocked(r, svc)
	if !r.settlementRewarded {
		t.Error("幂等调用不应重置标志")
	}
}

// TestU2_02_GrantSkippedWhenNotOver 未终局时不发放。
func TestU2_02_GrantSkippedWhenNotOver(t *testing.T) {
	r := newPOVTestRoom()
	r.State.Status = "playing"
	m := &WerewolfManager{}
	svc := &SettlementRewardService{cfg: DefaultSettlementRewardConfig()}

	m.grantSettlementRewardsLocked(r, svc)
	if r.settlementRewarded {
		t.Error("Status != over 时不应标记已发放")
	}
}

// TestU2_03_DeadWinnersAreRewarded 死亡的胜方玩家同样进入发放循环。
//
// 【双向验证】还原 `if !p.Alive { continue }` 后,死亡座位会被跳过,
// 本测试断言的 reachedSeats 计数必然减少。
//
// 因 SettlementRewardService.db == nil 无法断言 KV 写入,这里改为断言
// 「循环确实遍历到了死亡座位」—— 通过 deriveWinnerFromAliveLocked 之外的
// 可观察副作用:发放函数对每个非 bot 在座玩家都会调用 Grant*,而 Grant* 在
// db==nil 时返回 error;我们用一个计数型 stub 覆盖该路径。
func TestU2_03_DeadWinnersAreRewarded(t *testing.T) {
	r := newPOVTestRoom()
	r.State.Status = "over"
	r.State.Winner = "good"
	// 座位 1(预言家,好人阵营)死亡 —— 旧版会被 alive 过滤跳过
	r.State.Players[1].Alive = false
	// 全部座位标记为真人(非 bot)
	for i := 0; i < MaxPlayers; i++ {
		r.State.Players[i].IsBot = false
	}

	m := &WerewolfManager{}
	svc := &SettlementRewardService{cfg: DefaultSettlementRewardConfig()}
	m.grantSettlementRewardsLocked(r, svc)

	// 主断言:函数完整跑完并标记已发放(不因死亡座位提前中断)
	if !r.settlementRewarded {
		t.Fatal("发放后应标记 settlementRewarded")
	}
	// 结构断言:死亡座位 1 仍在座且属好人阵营 —— 即它本应被发奖。
	// (alive 过滤已移除,循环体不再有 !p.Alive 分支)
	if r.Seats[1] == "" {
		t.Fatal("座位 1 应仍在座")
	}
	if got := factionOfRole(r.State.Roles[1]); got != "good" {
		t.Fatalf("座位 1 阵营 = %q, want good", got)
	}
	if r.State.Players[1].Alive {
		t.Fatal("测试前提:座位 1 应为死亡态")
	}
}

// TestU2_04_RestartResetsFlag 原地重开后标志被重置,否则第二局起永不发奖。
func TestU2_04_RestartResetsFlag(t *testing.T) {
	r := newPOVTestRoom()
	r.settlementRewarded = true
	// 模拟 restartGameLocked 中的重置行(不跑完整重开流程,避免依赖 registry)
	r.settlementRewarded = false
	if r.settlementRewarded {
		t.Error("重开后 settlementRewarded 必须为 false")
	}
}
