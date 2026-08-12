package werewolf

// 2026-08-10 §20260810-05 — 信息账本(Information Ledger)一期回归测试。
// 设计文档:docs/狼人杀-Agent与系统/狼人杀13人局Agent升级-20260810-05.md §2.6。
//
// 覆盖:L-01 懒初始化 / L-02 公开+私聊 / L-03 夜间技能隔离 / L-04 盲守不变式 /
// L-05 道具注入 / L-06 容量淘汰 / L-07 redact / L-08 重开清零 /
// L-09 写入点注册表 / L-10 观战者快照脱敏。

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// newLedgerTestRoom 构造一个最小可用的测试房间(13 座位全占,State 已发牌)。
// 不走 StartGame(避免引擎完整流程依赖),手工填 Seats/Alive/Roles。
func newLedgerTestRoom() *WerewolfRoom {
	r := &WerewolfRoom{RoomID: "ledger-test"}
	gs := NewGame(42)
	// 13 座位全占;前 4 个是狼,5 号位预言家,6 号位女巫,7 号位守卫,其余平民。
	for i := 0; i < MaxPlayers; i++ {
		gs.Seats[i] = "user-" + strings.Repeat("x", 1) + string(rune('a'+i))
		gs.Players[i].Alive = true
	}
	for i := 0; i < 4; i++ {
		gs.Roles[i] = RoleWerewolf
	}
	gs.Roles[4] = RoleSeer
	gs.Roles[5] = RoleWitch
	gs.Roles[6] = RoleGuard
	for i := 7; i < MaxPlayers; i++ {
		gs.Roles[i] = RoleVillager
	}
	gs.Phase = PhaseSpeak
	gs.DayNumber = 1
	r.State = gs
	for i := 0; i < MaxPlayers; i++ {
		r.Seats[i] = gs.Seats[i]
	}
	return r
}

// L-01 懒初始化 + Knows 基本行为。
func TestLedger_L01_LazyInit(t *testing.T) {
	r := newLedgerTestRoom()
	if r.infoLedger != nil {
		t.Fatal("L-01: 新房间 infoLedger 应为 nil(懒初始化)")
	}
	r.mu.Lock()
	r.ledgerAppendLocked(InfoSourcePublicSpeech, "hello", aliveKnowerSetLocked(r), time.Now().UnixMilli())
	got := r.ledgerLocked().Len()
	r.mu.Unlock()
	if r.infoLedger == nil || got != 1 {
		t.Fatalf("L-01: 懒初始化后期望 1 条, got %d", got)
	}
}

// L-02 公开发言全房知情;whisper 仅收发双方知情,第三座不知情。
func TestLedger_L02_PublicAndWhisper(t *testing.T) {
	r := newLedgerTestRoom()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ledgerAppendLocked(InfoSourcePublicSpeech, "大家好", aliveKnowerSetLocked(r), time.Now().UnixMilli())
	if !r.infoLedger.AssertKnows(9, InfoSourcePublicSpeech) {
		t.Fatal("L-02: 公开发言 9 号位应知情")
	}
	r.ledgerAppendLocked(InfoSourceWhisper, "悄悄话", pairKnowerSet(2, 5), time.Now().UnixMilli())
	if !r.infoLedger.AssertKnows(2, InfoSourceWhisper) || !r.infoLedger.AssertKnows(5, InfoSourceWhisper) {
		t.Fatal("L-02: whisper 收发双方应知情")
	}
	if r.infoLedger.AssertKnows(7, InfoSourceWhisper) {
		t.Fatal("L-02: whisper 第三方 7 号位不应知情")
	}
}

// L-03 夜间技能信息隔离:查验/用药/守护仅本人;狼刀投票全狼知情、平民不知情。
func TestLedger_L03_NightSkillIsolation(t *testing.T) {
	r := newLedgerTestRoom()
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UnixMilli()
	r.ledgerAppendLocked(InfoSourceNightSeer, "seer_check", singleKnowerSet(4), now)
	r.ledgerAppendLocked(InfoSourceNightWitch, "witch_act", singleKnowerSet(5), now)
	r.ledgerAppendLocked(InfoSourceNightGuard, "guard_protect", singleKnowerSet(6), now)
	r.ledgerAppendLocked(InfoSourceNightWolfVote, "wolf_vote", aliveWolfKnowerSetLocked(r), now)

	if !r.infoLedger.AssertKnows(4, InfoSourceNightSeer) || r.infoLedger.AssertKnows(8, InfoSourceNightSeer) {
		t.Fatal("L-03: 查验结果应仅预言家(4)知情")
	}
	if !r.infoLedger.AssertKnows(5, InfoSourceNightWitch) || r.infoLedger.AssertKnows(8, InfoSourceNightWitch) {
		t.Fatal("L-03: 女巫信息应仅女巫(5)知情")
	}
	if !r.infoLedger.AssertKnows(6, InfoSourceNightGuard) || r.infoLedger.AssertKnows(8, InfoSourceNightGuard) {
		t.Fatal("L-03: 守护信息应仅守卫(6)知情")
	}
	for _, wolf := range []int{0, 1, 2, 3} {
		if !r.infoLedger.AssertKnows(wolf, InfoSourceNightWolfVote) {
			t.Fatalf("L-03: 狼 %d 应对狼刀投票知情", wolf)
		}
	}
	if r.infoLedger.AssertKnows(9, InfoSourceNightWolfVote) {
		t.Fatal("L-03: 平民 9 不应对狼刀投票知情")
	}
}

// L-04 盲守不变式(§134):狼刀目标结算后,守卫座位对其不知情。
func TestLedger_L04_BlindGuardInvariant(t *testing.T) {
	r := newLedgerTestRoom()
	r.mu.Lock()
	defer r.mu.Unlock()
	// 狼刀投票入账(全狼知情)。
	r.ledgerAppendLocked(InfoSourceNightWolfVote, "wolf_vote target=9", aliveWolfKnowerSetLocked(r), time.Now().UnixMilli())
	// 守卫(6)绝不能对狼刀目标知情(盲守语义:守卫看不到狼刀目标)。
	if r.infoLedger.AssertKnows(6, InfoSourceNightWolfVote) {
		t.Fatal("L-04: §134 盲守不变式被破坏 —— 守卫不应对狼刀投票知情")
	}
}

// L-05 道具注入仅被击中者知情;账本记「曾经知情」不因 ExpiresAfter 递减而消失。
func TestLedger_L05_PropInject(t *testing.T) {
	r := newLedgerTestRoom()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.enqueuePropHitLocked(8, PropInjectEntry{
		FromSeat: 1, PropKey: "markdown_bomb", InjectText: "x",
		EffectTypes: "expose_identity", Hit: true, ExpiresAfter: 1,
	})
	if !r.infoLedger.AssertKnows(8, InfoSourcePropInject) {
		t.Fatal("L-05: 被击中者 8 应对道具注入知情")
	}
	if r.infoLedger.AssertKnows(3, InfoSourcePropInject) {
		t.Fatal("L-05: 未命中者 3 不应对道具注入知情")
	}
	if len(r.propInjectQueue[8]) != 1 {
		t.Fatal("L-05: propInjectQueue 应有 1 条")
	}
}

// L-06 容量淘汰:写入 cap+50 条后 len==cap,最旧 seq 被淘汰。
func TestLedger_L06_CapEviction(t *testing.T) {
	l := NewInformationLedger()
	knowers := map[int]bool{0: true}
	for i := 0; i < informationLedgerCap+50; i++ {
		l.append(1, "speak", InfoSourcePublicSpeech, "msg", knowers, int64(i))
	}
	if l.Len() != informationLedgerCap {
		t.Fatalf("L-06: 期望 cap=%d, got %d", informationLedgerCap, l.Len())
	}
	if l.entries[0].Seq != 51 { // 前 50 条被淘汰,首条 seq=51
		t.Fatalf("L-06: 最旧条目 seq 期望 51, got %d", l.entries[0].Seq)
	}
}

// L-07 redact:身份明文被剔除。
func TestLedger_L07_Redact(t *testing.T) {
	cases := []struct{ in, mustNotContain string }{
		{"3 号是狼人", "狼人"},
		{"我是预言家,验了 5 号", "预言家"},
		{"witch used poison", "witch"},
		{"seer check result", "seer"},
	}
	for _, c := range cases {
		out := redactLedgerFact(c.in)
		if strings.Contains(out, c.mustNotContain) {
			t.Fatalf("L-07: redact(%q) 仍含 %q → %q", c.in, c.mustNotContain, out)
		}
	}
	// 非身份文本不受影响。
	if redactLedgerFact("今天天气不错") != "今天天气不错" {
		t.Fatal("L-07: 非身份文本不应被改动")
	}
}

// L-08 重开清零:restartGameLocked 语义 —— infoLedger 重置后 seq 重新从 1 起。
// (不直接调 restartGameLocked 以避免整局引擎依赖;直接验证重置语义。)
func TestLedger_L08_RestartReset(t *testing.T) {
	r := newLedgerTestRoom()
	r.mu.Lock()
	r.ledgerAppendLocked(InfoSourcePublicSpeech, "第一局消息", aliveKnowerSetLocked(r), time.Now().UnixMilli())
	// 模拟 restartGameLocked 中的重置动作。
	r.infoLedger = NewInformationLedger()
	r.ledgerRegisterRoleDealLocked()
	got := r.infoLedger.Len()
	r.mu.Unlock()
	// 重置后只剩 role_deal 条目(13 座位)。
	if got != MaxPlayers {
		t.Fatalf("L-08: 重开后应仅 %d 条 role_deal, got %d", MaxPlayers, got)
	}
	if r.infoLedger.entries[0].Seq != 1 {
		t.Fatalf("L-08: 重开后 seq 应从 1 起, got %d", r.infoLedger.entries[0].Seq)
	}
	if r.infoLedger.entries[0].Source != InfoSourceRoleDeal {
		t.Fatalf("L-08: 重开后首条应为 role_deal, got %s", r.infoLedger.entries[0].Source)
	}
	// role_deal 仅本人知情:每条 role_deal 的知情集合必须恰为 1 个座位,
	// 且座位号与 fact 中的 seat=N 一致(Knows 语义是「最新一条匹配」,
	// 13 条同 source 条目下只能看到最后一条,故改为逐条目校验)。
	for _, e := range r.infoLedger.entries {
		if e.Source != InfoSourceRoleDeal {
			t.Fatalf("L-08: 重开后应只有 role_deal 条目, got %s", e.Source)
		}
		if len(e.KnowerSeats) != 1 {
			t.Fatalf("L-08: role_deal 知情集合应恰为 1 座位, got %v", e.KnowerSeats)
		}
		for seat := range e.KnowerSeats {
			wantSuffix := "seat=" + strconv.Itoa(seat)
			if !strings.HasSuffix(e.Fact, wantSuffix) {
				t.Fatalf("L-08: role_deal fact %q 与知情座位 %d 不匹配", e.Fact, seat)
			}
		}
	}
}

// L-09 写入点注册表:每个 InfoSource 在本测试文件的模拟流中至少被产生一次。
// (AST 级「生产调用点」检查不可行;以「该 source 在账本中出现过」兜底 §130。)
func TestLedger_L09_AllSourcesWritable(t *testing.T) {
	r := newLedgerTestRoom()
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UnixMilli()
	all := aliveKnowerSetLocked(r)
	// 逐 source 写一条,验证注册表无「定义了却无法写入」的死枚举。
	for _, src := range AllInfoSources() {
		r.ledgerAppendLocked(src, "probe", all, now)
	}
	if r.infoLedger.Len() != len(AllInfoSources()) {
		t.Fatalf("L-09: 期望 %d 条, got %d", len(AllInfoSources()), r.infoLedger.Len())
	}
	seen := map[InfoSource]bool{}
	for _, e := range r.infoLedger.entries {
		seen[e.Source] = true
	}
	for _, src := range AllInfoSources() {
		if !seen[src] {
			t.Fatalf("L-09: source %s 注册但未能写入", src)
		}
	}
}

// L-10 观战者快照脱敏:knower_seats 有序、无身份明文。
func TestLedger_L10_SpectatorSnapshot(t *testing.T) {
	r := newLedgerTestRoom()
	r.mu.Lock()
	r.ledgerAppendLocked(InfoSourcePublicSpeech, "我觉得 3 号是狼人", map[int]bool{5: true, 2: true, 9: true}, time.Now().UnixMilli())
	snap := r.infoLedger.SnapshotJSON(200)
	r.mu.Unlock()
	if len(snap) != 1 {
		t.Fatalf("L-10: 期望 1 条快照, got %d", len(snap))
	}
	// knower_seats 有序。
	got := snap[0].KnowerSeats
	if len(got) != 3 || got[0] != 2 || got[1] != 5 || got[2] != 9 {
		t.Fatalf("L-10: knower_seats 应有序 [2 5 9], got %v", got)
	}
	// 身份明文已被 redact。
	if strings.Contains(snap[0].Fact, "狼人") {
		t.Fatalf("L-10: 快照仍含身份明文 → %q", snap[0].Fact)
	}
}
