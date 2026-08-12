package werewolf

// 2026-08-10 §20260810-08 — 信息账本二期 L2-01..L2-11 回归测试。

import (
	"strings"
	"testing"
	"time"
)

// newR212MinimalRoom 复用 R212 测试中的最小房间构造器。
func newR212MinimalRoom() *WerewolfRoom {
	r := &WerewolfRoom{RoomID: "phase2-min"}
	r.State = NewGame(20260810)
	r.State.SeatCount = MaxPlayers
	return r
}

func appendL2(l *InformationLedger, source InfoSource, fact string, knowers map[int]bool, round int) {
	l.append(round, "speak", source, fact, knowers, time.Now().UnixMilli())
}

func TestInformationLedgerPhase2_L2_01_DigestFiltersByKnower(t *testing.T) {
	l := NewInformationLedger()
	appendL2(l, InfoSourceWhisper, "seat=3 私聊", singleKnowerSet(1), 1)
	appendL2(l, InfoSourceNightSeer, "seat=7 查验", singleKnowerSet(2), 1)
	d := l.DigestForSeat(1, 2)
	if d == nil || d.TotalKnown != 1 || len(d.Entries) != 1 || d.Entries[0].Source != InfoSourceWhisper {
		t.Fatalf("L2-01 digest 过滤错误: %#v", d)
	}
}

func TestInformationLedgerPhase2_L2_02_DigestSortAndLimit(t *testing.T) {
	l := NewInformationLedger()
	sources := AllInfoSources()
	for i, source := range sources[:8] {
		for n := 0; n < i+1; n++ {
			appendL2(l, source, "seat=1", singleKnowerSet(0), 1)
		}
	}
	d := l.DigestForSeat(0, 2)
	if len(d.Entries) != 6 {
		t.Fatalf("L2-02 应限制 6 组,got=%d", len(d.Entries))
	}
	for i := 1; i < len(d.Entries); i++ {
		if d.Entries[i-1].Count < d.Entries[i].Count {
			t.Fatalf("L2-02 未按 Count 降序:%v", d.Entries)
		}
	}
}

func TestInformationLedgerPhase2_L2_03_HighlightsRecentAndTruncated(t *testing.T) {
	l := NewInformationLedger()
	appendL2(l, InfoSourcePublicSpeech, "旧内容", singleKnowerSet(0), 1)
	appendL2(l, InfoSourcePublicSpeech, "中内容", singleKnowerSet(0), 2)
	long := strings.Repeat("新", 70)
	appendL2(l, InfoSourcePublicSpeech, long, singleKnowerSet(0), 3)
	d := l.DigestForSeat(0, 2)
	h := d.Entries[0].Highlights
	if len(h) != 2 || h[0] != "中内容" || !strings.HasSuffix(h[1], "…") || len([]rune(h[1])) != 61 {
		t.Fatalf("L2-03 highlights 错误:%q", h)
	}
}

func TestInformationLedgerPhase2_L2_04_ExtractSeatRefs(t *testing.T) {
	// R213 修复后：人类可读形式（`N号` / `第N位`）按 1-indexed 解析后 -1；
	// 结构化形式（`seat=N`）按 0-indexed 原值保留。两者在内部都归一化到 0-indexed。
	refs := extractSeatRefs("3号讨论 seat=7 与第5位，seat=13 越界")
	// 「3号」→ 内部座位 2；「seat=7」→ 内部座位 7；「第5位」→ 内部座位 4。
	for _, seat := range []int{2, 4, 7} {
		if !refs[seat] {
			t.Fatalf("L2-04 未识别 seat=%d: %#v", seat, refs)
		}
	}
	if refs[13] {
		t.Fatal("L2-04 不应接受 13")
	}
}

func TestInformationLedgerPhase2_L2_05_PrivateSourceExhaustive(t *testing.T) {
	expected := map[InfoSource]bool{
		InfoSourcePublicSpeech: false, InfoSourceWhisper: true, InfoSourceWolfPack: true,
		InfoSourceNightSeer: true, InfoSourceNightWitch: true, InfoSourceNightGuard: true,
		InfoSourceNightWolfVote: true, InfoSourceDayVoteMap: false, InfoSourceSheriffStream: false,
		InfoSourceSheriffElect: false, InfoSourcePropInject: true, InfoSourceDeathEvent: false,
		InfoSourceHunterShot: false, InfoSourceKnightDuel: false, InfoSourceIdiotReveal: false,
		InfoSourceDemonHunter: false, InfoSourceRoleDeal: true,
	}
	all := AllInfoSources()
	if len(all) != len(expected) {
		t.Fatalf("L2-05 注册表与明确分类数量不一致 all=%d expected=%d", len(all), len(expected))
	}
	seen := map[InfoSource]bool{}
	for _, source := range all {
		want, ok := expected[source]
		if !ok {
			t.Fatalf("L2-05 新枚举未明确归类:%q", source)
		}
		seen[source] = true
		if got := isPrivateSource(source); got != want {
			t.Fatalf("L2-05 %q private=%v want=%v", source, got, want)
		}
	}
	for source := range expected {
		if !seen[source] {
			t.Fatalf("L2-05 明确分类未进入 AllInfoSources:%q", source)
		}
	}
}

func TestInformationLedgerPhase2_L2_06_PublicPriorSuppressesLeak(t *testing.T) {
	l := NewInformationLedger()
	appendL2(l, InfoSourcePublicSpeech, "大家已讨论 5号", map[int]bool{0: true, 1: true}, 1)
	appendL2(l, InfoSourceWhisper, "秘密提及 5号", singleKnowerSet(1), 1)
	appendL2(l, InfoSourcePublicSpeech, "我也说 5号", singleKnowerSet(1), 1)
	if leaks := DetectLeaks(l); len(leaks) != 0 {
		t.Fatalf("L2-06 公开先出现不应泄漏:%#v", leaks)
	}
}

func TestInformationLedgerPhase2_L2_07_PrivateReferenceLeaks(t *testing.T) {
	l := NewInformationLedger()
	appendL2(l, InfoSourceWolfPack, "今晚关注 seat=5", singleKnowerSet(1), 1)
	appendL2(l, InfoSourcePublicSpeech, "我认为 seat=5 可疑", singleKnowerSet(1), 2)
	leaks := DetectLeaks(l)
	if len(leaks) != 1 || leaks[0].Seat != 1 || leaks[0].HintSeat != 5 || leaks[0].FromSource != InfoSourceWolfPack {
		t.Fatalf("L2-07 泄漏记录错误:%#v", leaks)
	}
}

func TestInformationLedgerPhase2_L2_08_NonKnowerDoesNotLeak(t *testing.T) {
	l := NewInformationLedger()
	appendL2(l, InfoSourceWhisper, "秘密 seat=5", singleKnowerSet(1), 1)
	appendL2(l, InfoSourcePublicSpeech, "seat=5 可疑", singleKnowerSet(2), 2)
	if leaks := DetectLeaks(l); len(leaks) != 0 {
		t.Fatalf("L2-08 非知情者不应触发:%#v", leaks)
	}
}

func TestInformationLedgerPhase2_L2_09_LeakCacheBySeq(t *testing.T) {
	r := &WerewolfRoom{infoLedger: NewInformationLedger()}
	appendL2(r.infoLedger, InfoSourceWhisper, "秘密 seat=5", singleKnowerSet(1), 1)
	appendL2(r.infoLedger, InfoSourcePublicSpeech, "seat=5", singleKnowerSet(1), 2)
	first := r.detectLeaksLocked()
	if len(first) != 1 {
		t.Fatalf("L2-09 前置检测失败:%#v", first)
	}
	first[0].Excerpt = "缓存标记"
	second := r.detectLeaksLocked()
	if len(second) != 1 || second[0].Excerpt != "缓存标记" {
		t.Fatalf("L2-09 seq 未变应复用缓存:%#v", second)
	}
}

func TestInformationLedgerPhase2_L2_10_InfoLeaksSpectatorOnly(t *testing.T) {
	r := newR212MinimalRoom()
	r.infoLedger = NewInformationLedger()
	appendL2(r.infoLedger, InfoSourceWhisper, "秘密 seat=5", singleKnowerSet(1), 1)
	appendL2(r.infoLedger, InfoSourcePublicSpeech, "seat=5", singleKnowerSet(1), 2)
	player := BuildClientStateWithRoom(r.RoomID, r, 1)
	if len(player.InfoLeaks) != 0 {
		t.Fatalf("L2-10 玩家不应收到 info_leaks:%#v", player.InfoLeaks)
	}
	spectator := BuildClientStateWithRoom(r.RoomID, r, -1)
	if len(spectator.InfoLeaks) != 1 {
		t.Fatalf("L2-10 观战者应收到一条:%#v", spectator.InfoLeaks)
	}
}

func TestInformationLedgerPhase2_L2_11_ContextDigestOwnSeatOnly(t *testing.T) {
	r := newR212MinimalRoom()
	r.infoLedger = NewInformationLedger()
	appendL2(r.infoLedger, InfoSourceWhisper, "seat=3 给 0", singleKnowerSet(0), 1)
	appendL2(r.infoLedger, InfoSourceNightSeer, "seat=7 给 1", singleKnowerSet(1), 1)
	gc := buildAgentContextLocked(r, 0, 0)
	if gc.KnowledgeDigest == nil || gc.KnowledgeDigest.TotalKnown != 1 || len(gc.KnowledgeDigest.Entries) != 1 || gc.KnowledgeDigest.Entries[0].Source != string(InfoSourceWhisper) {
		t.Fatalf("L2-11 context digest 跨 seat 污染:%#v", gc.KnowledgeDigest)
	}
}

func TestInformationLedgerPhase2_L2_12_KnowledgeDigestBlockNil(t *testing.T) {
	t.Skip("L2-12 在 agent/wwplayer 包内验证，见 prompt_phase2_test.go")
}

// R213 缺陷 1：真实混用形态 — 私密条目用 0-indexed（`seat=N` / `target=N` / `from=N`），
// 公开发言用 1-indexed（人类可读 `N号` / `第N位`）。两条应能在同一账本内交叉命中。
func TestInformationLedgerPhase2_R213_D1_MixedNumbering(t *testing.T) {
	l := NewInformationLedger()
	// 座位 2 通过夜视查验得知座位 4（私有 0-indexed `seat=2 target=4`）。
	appendL2(l, InfoSourceNightSeer, "seer_check seat=2 target=4", singleKnowerSet(2), 1)
	// 座位 2 公开说「5号有问题」（人类可读 1-indexed，对应内部座位 4）。
	appendL2(l, InfoSourcePublicSpeech, "我觉得5号有问题", map[int]bool{2: true}, 2)
	leaks := DetectLeaks(l)
	if len(leaks) != 1 {
		t.Fatalf("R213-D1 应命中 1 条泄漏：%#v", leaks)
	}
	if leaks[0].Seat != 2 || leaks[0].HintSeat != 4 || leaks[0].FromSource != InfoSourceNightSeer {
		t.Fatalf("R213-D1 归一化错误：%#v", leaks[0])
	}
}

// R213 缺陷 2：座位 N 公开说自己的座位（自指）不触发 leak。
// role_deal 是 `role_deal seat=N`、knower={N}，每个座位都「从私密渠道知道自己的座位号」，
// 但这不构成信息优势，必须跳过自指。
// 测试用 0-indexed 路径：座位 N 公开说 `seat=N`（LLM 有时输出 raw 形式），
// 修复前会触发 leak；修复后 ref==speaker 直接跳过。
func TestInformationLedgerPhase2_R213_D2_SelfReferenceSuppressed(t *testing.T) {
	l := NewInformationLedger()
	// 座位 5 通过 role_deal 知道自己是 5（0-indexed 内部 `seat=5`）。
	appendL2(l, InfoSourceRoleDeal, "role_deal seat=5", singleKnowerSet(5), 0)
	// 座位 5 公开说 `seat=5`（人类/真人也可能输出「我是 seat=5」这种 raw 形式）。
	appendL2(l, InfoSourcePublicSpeech, "seat=5 是我", map[int]bool{5: true}, 1)
	if leaks := DetectLeaks(l); len(leaks) != 0 {
		t.Fatalf("R213-D2 自指不应触发 leak（got %d 条）:%#v", len(leaks), leaks)
	}
}

// R213 缺陷 3：`wolf_pack` 单条 fact 内嵌的 `from=` 是 0-indexed（结构化），
// 内嵌的 `text` 是人类可读 1-indexed（狼人写「今晚刀5号」）。两个分支应分别归一化到 0-indexed。
func TestInformationLedgerPhase2_R213_D3_WolfPackMixedFact(t *testing.T) {
	l := NewInformationLedger()
	// 座位 0 在狼队频道写：`from=0 text="今晚刀5号"`（内部座位 0 写「今晚刀内部座位 4」）。
	appendL2(l, InfoSourceWolfPack, "wolf_pack from=0 text=今晚刀5号", singleKnowerSet(0), 1)
	// 座位 0 公开说「5号」（人类可读对应内部座位 4）。
	appendL2(l, InfoSourcePublicSpeech, "我猜5号有问题", map[int]bool{0: true}, 2)
	leaks := DetectLeaks(l)
	if len(leaks) != 1 || leaks[0].Seat != 0 || leaks[0].HintSeat != 4 || leaks[0].FromSource != InfoSourceWolfPack {
		t.Fatalf("R213-D3 wolf_pack 混用形态未命中:%#v", leaks)
	}
}
