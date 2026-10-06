// Package werewolf — room_judge_qa_test.go: 法官问答 + 遗言聊天可见性单测。
//
// 2026-10-06 §法官对话 B13。设计文档 §7.1:
//   - TestJudgeQAContext_NoSecrets        问答上下文不含任何未公开信息(F-5)
//   - TestJudgeQA_CooldownAndPublicPrefix 私聊冷却拒绝 + @法官 前缀检测(全角变体)
//   - TestDeathLyricStartEmitted_OnPhaseEnter  phase 进入 death_lyric → start 事件
//   - TestPhaseLabelCN_DeathLyric         "death_lyric" → "遗言"(D2 修复)
//   - TestSayLastWords_HumanSeatBubble    真人座位遗言写 lastSpeechBySeat(D3 修复)
package werewolf

import (
	"strings"
	"testing"

	"LsmAgentGame/agent/wwjudge"
	"LsmAgentGame/agent/wwtypes"
	"LsmAgentGame/errcode"
)

// newJudgeQARoom 构造一个挂了法官 goroutine 桩的测试房间(已开局 13 人)。
// 返回 (manager, room, gameState)。房间已注册进 manager.rooms(RecordJudgeQuestion
// 的 getRoom 查找依赖)。
func newJudgeQARoom(t *testing.T, seed int64) (*WerewolfManager, *WerewolfRoom, *GameState) {
	t.Helper()
	gs := makeStartedGame13(t, seed, fillSeats("u1", "u2", "u3", "u4", "u5", "u6", "u7", "u8", "u9", "u10", "u11", "u12", "u13"))
	r := &WerewolfRoom{RoomID: "qa-room-1", State: gs}
	r.Seats = gs.Seats
	m := NewWerewolfManager()
	j := wwjudge.NewAgentJudge("qa-room-1", "TestJudgeModel")
	r.judge = j
	r.judgeEvents = j.Events()
	m.rooms["qa-room-1"] = r
	return m, r, gs
}

// flattenExtra 把 buildJudgeQAContextLocked 产物里所有 string 值拼成一个
// 大字符串(供「不含秘密」断言)。
func flattenExtra(extra map[string]any) string {
	var b strings.Builder
	for k, v := range extra {
		if s, ok := v.(string); ok {
			b.WriteString(k + "=" + s + "\n")
		} else {
			b.WriteString(k + "\n")
		}
	}
	return b.String()
}

// TestJudgeQAContext_NoSecrets — F-5:房间含已知存活狼座 + 夜间行动字段,
// buildJudgeQAContextLocked 产物**不含**狼角色名(werewolf/狼人)与任何
// 座位↔狼身份的关联标记。
//
// 口径说明(与设计 §7.1 的对齐):存活座位**列表**必然包含狼座位号 —— 「谁活着」
// 本身是全员公开信息(公屏/座位卡同口径),泄露判定看的是「座位号与狼身份的
// 关联」:断言产物中不出现 RoleWerewolf.String()(=“werewolf”)与中文名
// “狼人”,即不存在任何把某个座位标记为狼的字符串。
func TestJudgeQAContext_NoSecrets(t *testing.T) {
	m, r, gs := newJudgeQARoom(t, 20261006)
	wolfSeats := []int{}
	for i := 0; i < MaxPlayers; i++ {
		if gs.Roles[i] == RoleWerewolf && gs.AliveSeat(Seat(i)) {
			wolfSeats = append(wolfSeats, i)
		}
	}
	if len(wolfSeats) == 0 {
		t.Skip("no living wolf in this seed; rerun with another seed")
	}
	// 夜间行动字段故意填上显眼值:若上下文构建读了它们,断言立即失败。
	gs.WolfKillTarget = Seat(wolfSeats[0])
	// 公屏发言放几条安全文本(不含角色词)。
	r.recentSpeeches = []wwtypes.SpeechEvent{
		{Seat: 0, Account: "u1", Text: "大家好,我是普通玩家"},
		{Seat: 5, Account: "u6", Text: "我觉得局势还不明朗"},
	}

	extra := m.buildJudgeQAContextLocked(r, "u1", "u1", "现在到什么阶段了?", false)
	out := flattenExtra(extra)

	if strings.Contains(out, "werewolf") {
		t.Fatalf("QA context leaks wolf role key \"werewolf\":\n%s", out)
	}
	if strings.Contains(out, "狼人") {
		t.Fatalf("QA context leaks wolf role CN name \"狼人\":\n%s", out)
	}
	// 公开状态段必须仍然存在(能力隔离 ≠ 零信息)。
	if !strings.Contains(out, "存活座位") {
		t.Fatalf("QA context missing public alive list:\n%s", out)
	}
	// 提问与 asker 信息在场。
	if extra["question"] != "现在到什么阶段了?" {
		t.Fatalf("question = %v", extra["question"])
	}
	if extra["asker_display"] != "1号(u1)" {
		t.Fatalf("asker_display = %v, want 1号(u1)", extra["asker_display"])
	}
	if v, _ := extra["asker_is_spectator"].(bool); v {
		t.Fatalf("seated asker misclassified as spectator")
	}
}

// TestJudgeQAContext_SpectatorDisplay — 观战者/死亡座位的 asker_display 形态。
func TestJudgeQAContext_SpectatorDisplay(t *testing.T) {
	m, r, gs := newJudgeQARoom(t, 20261006)
	// 观战者:不在任何座位。
	extra := m.buildJudgeQAContextLocked(r, "ghost-99", "李四", "?", true)
	if extra["asker_display"] != "观战-李四" {
		t.Fatalf("spectator display = %v, want 观战-李四", extra["asker_display"])
	}
	// 死亡座位:找一个座位杀掉再问。
	seat := 4
	gs.Players[seat].Alive = false
	extra = m.buildJudgeQAContextLocked(r, gs.Seats[seat], "王五", "?", false)
	if extra["asker_display"] != "死亡-5号(王五)" {
		t.Fatalf("dead seat display = %v, want 死亡-5号(王五)", extra["asker_display"])
	}
}

// TestJudgeQA_CooldownAndPublicPrefix — 私聊 per-user 冷却拒绝(明确文案)+
// 公屏路径冷却跳过(不报错)+ "@法官" 前缀检测(含全角冒号变体)。
func TestJudgeQA_CooldownAndPublicPrefix(t *testing.T) {
	m, r, _ := newJudgeQARoom(t, 20261006)

	// 私聊第一次:通过(冷却游标推进)。
	if err := m.RecordJudgeQuestion("qa-room-1", "u1", "u1", "Q1"); err != nil {
		t.Fatalf("first private question rejected: %v", err)
	}
	// 私聊第二次(冷却期内):明确错误文案(→ ws 层 chat.error 20001)。
	err := m.RecordJudgeQuestion("qa-room-1", "u1", "u1", "Q2")
	if err == nil || !strings.Contains(err.Error(), "judge Q&A cooldown") {
		t.Fatalf("cooldown err = %v, want \"judge Q&A cooldown: ...\"", err)
	}

	// 公屏第一次:返回 extra(应唤醒);冷却期内第二次:返回 nil(静默跳过)。
	r.mu.Lock()
	extra := m.recordJudgePublicQuestionLocked(r, "u2", "u2", "@法官 白痴怎么翻牌")
	extra2 := m.recordJudgePublicQuestionLocked(r, "u2", "u2", "@法官 再问一个")
	r.mu.Unlock()
	if extra == nil {
		t.Fatalf("first public question must produce wake extra")
	}
	if extra["is_public"] != true {
		t.Fatalf("public extra is_public = %v, want true", extra["is_public"])
	}
	if extra2 != nil {
		t.Fatalf("cooldown-hit public question must be skipped silently (nil extra)")
	}

	// 前缀检测表(全角冒号 / 前导空白 / 非前缀)。
	cases := []struct {
		in   string
		want bool
	}{
		{"@法官 白痴怎么翻牌", true},
		{"@法官:警徽流规则", true},
		{"@法官:警徽流规则", true}, // 全角冒号同样是 "@法官" 前缀
		{"  @法官 先问一句", true},  // TrimSpace 后命中
		{"@ 法官", false},      // "@ " 带空格不构成前缀
		{"法官@你好", false},
		{"向大家@法官请教", false}, // 仅识别前缀
		{"", false},
	}
	for _, tc := range cases {
		if got := isJudgePublicQuestion(tc.in); got != tc.want {
			t.Fatalf("isJudgePublicQuestion(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// TestJudgeQA_EventCarriesNoSecrets — §3.1 能力隔离的结构性断言:
// RecordJudgeQuestion 投递的 JudgeEvent **Snap 必须为 nil**(wakeJudgeLocked
// 构建的全知快照含 WolfSeats,问答链路从头到尾不携带),Extra 内也不含
// 狼角色名。
func TestJudgeQA_EventCarriesNoSecrets(t *testing.T) {
	m, r, gs := newJudgeQARoom(t, 20261006)
	_ = gs
	if err := m.RecordJudgeQuestion("qa-room-1", "u1", "u1", "规则问题?"); err != nil {
		t.Fatalf("RecordJudgeQuestion: %v", err)
	}
	select {
	case evt := <-r.judgeEvents:
		if evt.Kind != wwjudge.KindJudgeQuestion {
			t.Fatalf("event kind = %q", evt.Kind)
		}
		if evt.Snap != nil {
			t.Fatalf("judge_question event must carry nil Snap (isolation), got %+v", evt.Snap)
		}
		if out := flattenExtra(evt.Extra); strings.Contains(out, "werewolf") || strings.Contains(out, "狼人") {
			t.Fatalf("event Extra leaks wolf identity:\n%s", out)
		}
	default:
		t.Fatalf("judge_question event not dispatched")
	}
}

// TestJudgeQA_Rejections — 房间不存在 / 法官未就绪 的错误文案。
func TestJudgeQA_Rejections(t *testing.T) {
	m, _, _ := newJudgeQARoom(t, 20261006)
	if err := m.RecordJudgeQuestion("no-such-room", "u1", "u1", "?"); err == nil {
		t.Fatalf("missing room must error")
	}
	// 法官未就绪(r.judge == nil,filling 期)。
	r2 := &WerewolfRoom{RoomID: "qa-room-2", State: nil}
	m.rooms["qa-room-2"] = r2
	err := m.RecordJudgeQuestion("qa-room-2", "u1", "u1", "?")
	if err == nil || !strings.Contains(err.Error(), "not ready") {
		t.Fatalf("judge-not-ready err = %v, want \"judge not ready yet\"", err)
	}
}

// TestJudgeQA_FallbackRefundCooldown — E2E 2026-10-06 精化:
//   - fallback=true 的 RecordJudgeAnswer:退还 per-user 冷却游标(用户可立即
//     重试,不被占位文案锁 20s)+ 不追加 judgeQAHistory(防兜底文案污染
//     后续 QA 上下文);
//   - fallback=false(成功):冷却游标保留 + 历史正常追加。
func TestJudgeQA_FallbackRefundCooldown(t *testing.T) {
	m, r, _ := newJudgeQARoom(t, 20261006)

	// 第一次提问:成功派发,冷却游标已写入。
	if err := m.RecordJudgeQuestion("qa-room-1", "u1", "u1", "Q1"); err != nil {
		t.Fatalf("first question: %v", err)
	}
	if _, ok := r.judgeQALastAsk["u1"]; !ok {
		t.Fatalf("cooldown cursor must be set after ask")
	}

	// 兜底回答(fallback=true):冷却退还 + 零历史。
	m.RecordJudgeAnswer("qa-room-1", "u1", "u1", "Q1", "法官暂时无法回答,请稍后再问。", false, true)
	if _, ok := r.judgeQALastAsk["u1"]; ok {
		t.Fatalf("fallback must refund the cooldown cursor")
	}
	if len(r.judgeQAHistory["u1"]) != 0 {
		t.Fatalf("fallback must NOT append QA history, got %d turns", len(r.judgeQAHistory["u1"]))
	}

	// 冷却已退还 → 同一用户可立即再问(不被 20s 锁)。
	if err := m.RecordJudgeQuestion("qa-room-1", "u1", "u1", "Q2"); err != nil {
		t.Fatalf("immediate retry after fallback refund: %v", err)
	}

	// 成功回答(fallback=false):冷却游标保留 + 历史追加。
	m.RecordJudgeAnswer("qa-room-1", "u1", "u1", "Q2", "这是真正的回答。", false, false)
	if _, ok := r.judgeQALastAsk["u1"]; !ok {
		t.Fatalf("success answer keeps cooldown cursor")
	}
	turns := r.judgeQAHistory["u1"]
	if len(turns) != 1 || turns[0].Answer != "这是真正的回答。" {
		t.Fatalf("success answer history = %+v, want 1 turn with real answer", turns)
	}
}
// recordingEmitter 是 ActivityEmitter 的计数桩。
type recordingEmitter struct {
	events []struct {
		kind string
		seat int
	}
}

func (e *recordingEmitter) EmitRoomActivity(roomID, eventKind, text, phase string,
	roundNumber int, severity, icon string, refSeat, refSeat2 int, silentForBots bool) bool {
	e.events = append(e.events, struct {
		kind string
		seat int
	}{eventKind, refSeat})
	return true
}

// TestDeathLyricStartEmitted_OnPhaseEnter — D1 接线:phaseWatchdogTick 观察到
// phase 进入 PhaseDeathLyric(且当前遗言座位有效)时,必须经 EmitDeathLyricStart
// 发出 death_lyric_start 活动事件(start→spoken 徽章链完整)。
func TestDeathLyricStartEmitted_OnPhaseEnter(t *testing.T) {
	m, r, gs := newJudgeQARoom(t, 20261006)
	em := &recordingEmitter{}
	m.SetActivityEmitter(em)

	// 模拟引擎进入遗言阶段:两个遗言座位,当前 4 号(0-indexed)。
	gs.Phase = PhaseDeathLyric
	gs.DeathLyricQueue = []Seat{4, 7}
	gs.DeathLyricCurrent = 4
	r.lastJudgePhase = PhaseDawn // phase 游标指向其它阶段 → 本 tick 检测到切换

	if err := m.phaseWatchdogTick(r); err != nil {
		t.Fatalf("phaseWatchdogTick: %v", err)
	}
	found := false
	for _, ev := range em.events {
		if ev.kind == ActivityEventKindDeathLyricStart {
			found = true
			if ev.seat != 4 {
				t.Fatalf("death_lyric_start refSeat = %d, want 4", ev.seat)
			}
		}
	}
	if !found {
		t.Fatalf("death_lyric_start not emitted on phase enter; events = %+v", em.events)
	}

	// 同 phase 第二个 tick(DeathLyricCurrent 仍 4):游标已对齐,不重发。
	em.events = nil
	if err := m.phaseWatchdogTick(r); err != nil {
		t.Fatalf("second tick: %v", err)
	}
	for _, ev := range em.events {
		if ev.kind == ActivityEventKindDeathLyricStart {
			t.Fatalf("death_lyric_start re-emitted within same phase")
		}
	}
}

// TestPhaseLabelCN_DeathLyric — D2 修复:实际 phase 串 "death_lyric" 必须映射
// 为中文 "遗言"(旧别名 "last_words" 保留)。
func TestPhaseLabelCN_DeathLyric(t *testing.T) {
	if got := phaseLabelCN("death_lyric"); got != "遗言" {
		t.Fatalf("phaseLabelCN(death_lyric) = %q, want 遗言", got)
	}
	if got := phaseLabelCN("last_words"); got != "遗言" {
		t.Fatalf("phaseLabelCN(last_words) = %q, want 遗言 (legacy key)", got)
	}
}

// TestSayLastWords_HumanSeatBubble — D3 修复:sayLastWordsLocked 成功后对任意
// 座位(含真人)写 lastSpeechBySeat,Kind="last_words"(与 bot 路径
// wwplayer.RecordLastSpeech 的 kind 口径对齐),Text 按 200 rune 截断。
func TestSayLastWords_HumanSeatBubble(t *testing.T) {
	m, r, gs := newJudgeQARoom(t, 20261006)
	em := &recordingEmitter{}
	m.SetActivityEmitter(em)

	// 走引擎正规入口进入遗言阶段(会 make DeathLyricDone / 设 queue+current)。
	// filterLastWords 只保留 LastWords=true 的座位,先显式置位。
	gs.Players[3].LastWords = true
	if e := gs.tryEnterDeathLyricRound([]Seat{3}, func() *errcode.Error { return nil }); e != nil {
		t.Fatalf("tryEnterDeathLyricRound: %v", e)
	}
	if gs.Phase != PhaseDeathLyric {
		t.Fatalf("phase = %v, want death_lyric", gs.Phase)
	}
	longText := strings.Repeat("遗", 500) // 超过 lastSpeechRuneLimit(200)

	if e := m.sayLastWordsLocked(r, 3, longText); e != nil {
		t.Fatalf("sayLastWordsLocked: %v", e)
	}
	sp, ok := r.lastSpeechBySeat[3]
	if !ok {
		t.Fatalf("human seat lastSpeechBySeat not written")
	}
	if sp.Kind != "last_words" {
		t.Fatalf("lastSpeechBySeat[3].Kind = %q, want last_words", sp.Kind)
	}
	if got := len([]rune(sp.Text)); got != lastSpeechRuneLimit {
		t.Fatalf("lastSpeechBySeat[3].Text runes = %d, want %d", got, lastSpeechRuneLimit)
	}
	if sp.AtMs == 0 {
		t.Fatalf("lastSpeechBySeat[3].AtMs not set")
	}
	// 遗言 spoken 活动事件同时发出(既有行为,回归红线)。
	spoken := false
	for _, ev := range em.events {
		if ev.kind == ActivityEventKindDeathLyricSpoken && ev.seat == 3 {
			spoken = true
		}
	}
	if !spoken {
		t.Fatalf("death_lyric_spoken not emitted; events = %+v", em.events)
	}
}
