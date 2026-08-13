// Package werewolf — chat_recall_test.go: §20260813-02 U3 chat_recall 工具测试。
//
// 覆盖(≥6 项):
//
//	U3-01 闭集钳制:seat 越界忽略 / day 钳到 [1,currentDay] / query 截 50 字
//	U3-02 打分规则:关键词×2 + 座位×3 + 天数×2 + 类型加权(公开>私聊>活动)
//	U3-03 可见域:他人 whisper 物理剔除,发给自己的 whisper 保留(§119 双保险)
//	U3-04 端到端:manager.ChatRecall 命中 + 文本截 120 字 + Top5 [已截断] 标记
//	U3-05 冷却:agentRunner 60s 内第二次调用返回 rate-limited(不计失败)
//	U3-06 空结果降级文案 + query 必填校验
package werewolf

import (
	"strings"
	"testing"
	"time"

	agentcore "LsmAgentGame/agent/core"
)

// ── U3-01 闭集钳制 ────────────────────────────────────────────────────────

func TestChatRecall_U3_01_ClampClosedSet(t *testing.T) {
	// seat 越界(负数 / ≥ maxPlayers)→ 忽略(-1)。
	_, fs, _ := clampChatRecallParams("q", 99, 0, 3, 13)
	if fs != -1 {
		t.Fatalf("seat=99 must be clamped to -1, got %d", fs)
	}
	_, fs2, _ := clampChatRecallParams("q", -5, 0, 3, 13)
	if fs2 != -1 {
		t.Fatalf("seat=-5 must be clamped to -1, got %d", fs2)
	}
	_, fs3, _ := clampChatRecallParams("q", 5, 0, 3, 13)
	if fs3 != 5 {
		t.Fatalf("seat=5 must survive, got %d", fs3)
	}
	// day 钳到 [1, currentDay]。
	_, _, d1 := clampChatRecallParams("q", -1, 99, 3, 13)
	if d1 != 3 {
		t.Fatalf("day=99 with currentDay=3 must clamp to 3, got %d", d1)
	}
	_, _, d2 := clampChatRecallParams("q", -1, 0, 3, 13)
	if d2 != 0 {
		t.Fatalf("day=0 must mean 'no filter', got %d", d2)
	}
	_, _, d3 := clampChatRecallParams("q", -1, -7, 3, 13)
	if d3 != 0 {
		t.Fatalf("day=-7 must mean 'no filter', got %d", d3)
	}
	// query 截 50 rune。
	long := strings.Repeat("检", 80)
	q, _, _ := clampChatRecallParams(long, -1, 0, 3, 13)
	if got := len([]rune(q)); got > 51 { // 50 + "…"
		t.Fatalf("query must truncate to ≤51 runes (50+…), got %d", got)
	}
}

// ── U3-02 打分规则 ────────────────────────────────────────────────────────

func TestChatRecall_U3_02_Scoring(t *testing.T) {
	base := agentcore.ChatMessage{Text: "我昨晚查了 5 号 是金水", FromSeat: 2, Round: 1}
	// 关键词命中 1 次 ×2 + 座位 ×3 + 天数 ×2 + 公开 +2 = 9。
	got := scoreChatRecallEntry(base, "金水", 2, 1)
	if got != 2+3+2+2 {
		t.Fatalf("score = %d, want 9", got)
	}
	// 关键词命中 2 次。
	multi := agentcore.ChatMessage{Text: "金水金水", Round: 2}
	if got := scoreChatRecallEntry(multi, "金水", -1, 0); got != 4+2 {
		t.Fatalf("score = %d, want 6 (2 hits ×2 + public +2)", got)
	}
	// 类型加权:公开 2 > 私聊 1 > 活动 0。
	pub := scoreChatRecallEntry(agentcore.ChatMessage{Text: "x"}, "", -1, 0)
	whi := scoreChatRecallEntry(agentcore.ChatMessage{Text: "x", IsWhisper: true, ToSeat: 1}, "", -1, 0)
	act := scoreChatRecallEntry(agentcore.ChatMessage{Text: "x", IsActivity: true}, "", -1, 0)
	if !(pub > whi && whi > act) {
		t.Fatalf("type weight order broken: public=%d whisper=%d activity=%d", pub, whi, act)
	}
}

// ── U3-03 可见域(§119) ───────────────────────────────────────────────────

func TestChatRecall_U3_03_Visibility(t *testing.T) {
	// 公开发言与活动事件对所有人可见。
	if !chatRecallEntryVisible(agentcore.ChatMessage{FromSeat: 5}, 1) {
		t.Fatal("public chat must be visible")
	}
	if !chatRecallEntryVisible(agentcore.ChatMessage{FromSeat: -1, IsActivity: true}, 1) {
		t.Fatal("activity event must be visible")
	}
	// 他人之间的 whisper 不可见(双保险:这里 + 端到端)。
	if chatRecallEntryVisible(agentcore.ChatMessage{IsWhisper: true, FromSeat: 2, ToSeat: 3}, 1) {
		t.Fatal("whisper between OTHER seats must be excluded (§119)")
	}
	// 发给我的 / 我发出的 whisper 可见。
	if !chatRecallEntryVisible(agentcore.ChatMessage{IsWhisper: true, FromSeat: 2, ToSeat: 1}, 1) {
		t.Fatal("whisper TO me must be visible")
	}
	if !chatRecallEntryVisible(agentcore.ChatMessage{IsWhisper: true, FromSeat: 1, ToSeat: 2}, 1) {
		t.Fatal("whisper FROM me must be visible")
	}
}

// ── 端到端辅助 ─────────────────────────────────────────────────────────────

func newRecallTestRoom(t *testing.T, roomID string, dayNumber int) (*WerewolfManager, *WerewolfRoom) {
	t.Helper()
	m := NewWerewolfManager()
	r := &WerewolfRoom{RoomID: roomID}
	r.chatQueue = agentcore.NewChatHistoryQueue(0)
	r.State = &GameState{DayNumber: dayNumber}
	m.rooms[roomID] = r
	return m, r
}

func appendRecallMsg(r *WerewolfRoom, m agentcore.ChatMessage) {
	r.chatQueue.Append(m)
}

// ── U3-04 端到端:命中 + 截断 + Top5 标记 ──────────────────────────────────

func TestChatRecall_U3_04_EndToEnd(t *testing.T) {
	m, r := newRecallTestRoom(t, "recall-room-1", 3)
	appendRecallMsg(r, agentcore.ChatMessage{FromSeat: 1, FromAccount: "Bot 2号", Text: "我是预言家,昨晚查了 4 号 是金水", Round: 1})
	appendRecallMsg(r, agentcore.ChatMessage{FromSeat: 4, FromAccount: "Bot 5号", Text: "跟票 2 号", Round: 2})
	appendRecallMsg(r, agentcore.ChatMessage{FromSeat: 2, FromAccount: "Bot 3号", Text: "无关发言", Round: 3})

	out := m.ChatRecall("recall-room-1", 0, "预言家", -1, 0)
	if !strings.Contains(out, "命中 1 条") {
		t.Fatalf("expect 1 hit, got:\n%s", out)
	}
	if !strings.Contains(out, "我是预言家") {
		t.Fatalf("hit content missing:\n%s", out)
	}
	if strings.Contains(out, "无关发言") {
		t.Fatalf("irrelevant message leaked into results:\n%s", out)
	}
	if strings.Contains(out, "已截断") {
		t.Fatalf("single hit must not carry truncated marker:\n%s", out)
	}

	// 长文本截 120 rune。
	m2, r2 := newRecallTestRoom(t, "recall-room-2", 1)
	appendRecallMsg(r2, agentcore.ChatMessage{FromSeat: 1, Text: "预言家" + strings.Repeat("长", 300), Round: 1})
	out2 := m2.ChatRecall("recall-room-2", 0, "预言家", -1, 0)
	if strings.Contains(out2, strings.Repeat("长", 200)) {
		t.Fatalf("text must truncate at 120 runes:\n%.200s", out2)
	}

	// Top5 + [已截断] 标记。
	m3, r3 := newRecallTestRoom(t, "recall-room-3", 1)
	for i := 0; i < 7; i++ {
		appendRecallMsg(r3, agentcore.ChatMessage{FromSeat: i, Text: "跳预言家的第" + strings.Repeat("x", i+1) + "个人", Round: 1})
	}
	out3 := m3.ChatRecall("recall-room-3", 0, "预言家", -1, 0)
	if !strings.Contains(out3, "命中 7 条(返回前 5)") {
		t.Fatalf("expect '命中 7 条(返回前 5)', got:\n%s", out3)
	}
	if !strings.Contains(out3, "[已截断") {
		t.Fatalf("truncated marker missing:\n%s", out3)
	}
}

// ── U3-05 冷却(agentRunner) ───────────────────────────────────────────────

func TestChatRecall_U3_05_Cooldown(t *testing.T) {
	m, _ := newRecallTestRoom(t, "recall-room-4", 1)
	runner := newAgentRunner(m, "recall-room-4", Seat(0), "u1", "Bot 1号", "fake-model", nil)
	first := runner.ChatRecall("预言家", -1, 0)
	if strings.Contains(first, "rate-limited") {
		t.Fatalf("first call must not be rate-limited, got: %s", first)
	}
	second := runner.ChatRecall("预言家", -1, 0)
	if !strings.Contains(second, "rate-limited") {
		t.Fatalf("second call within 60s must be rate-limited, got: %s", second)
	}
	// 模拟冷却过期。
	runner.lastChatRecallAt = time.Now().Add(-61 * time.Second)
	third := runner.ChatRecall("预言家", -1, 0)
	if strings.Contains(third, "rate-limited") {
		t.Fatalf("call after cooldown must succeed, got: %s", third)
	}
}

// ── U3-06 空结果与参数校验 ────────────────────────────────────────────────

func TestChatRecall_U3_06_EmptyAndValidation(t *testing.T) {
	m, _ := newRecallTestRoom(t, "recall-room-5", 2)
	if out := m.ChatRecall("recall-room-5", 0, "不存在的关键词", -1, 0); !strings.Contains(out, "未找到匹配") {
		t.Fatalf("no-hit must return 未找到匹配, got: %s", out)
	}
	if out := m.ChatRecall("recall-room-5", 0, "", -1, 0); !strings.Contains(out, "query 不能为空") {
		t.Fatalf("empty query with no filters must be rejected, got: %s", out)
	}
	if out := m.ChatRecall("no-such-room", 0, "x", -1, 0); !strings.Contains(out, "房间不存在") {
		t.Fatalf("missing room must return 房间不存在, got: %s", out)
	}
}

// ── U3-07 端到端隐私:他人 whisper 不泄漏(§119 双保险) ─────────────────────

func TestChatRecall_U3_07_PrivacyEndToEnd(t *testing.T) {
	m, r := newRecallTestRoom(t, "recall-room-6", 2)
	// 座位 2 → 座位 3 的 whisper(对我 seat=0 不可见)。
	// 文本刻意包含查询关键词「可疑」—— 若可见域过滤失效,该条目必然命中并泄漏。
	appendRecallMsg(r, agentcore.ChatMessage{IsWhisper: true, FromSeat: 2, ToSeat: 3, Text: "秘密:4号可疑,今晚刀 5 号", Round: 1})
	// 座位 2 → 我(seat=0)的 whisper(可见)。
	appendRecallMsg(r, agentcore.ChatMessage{IsWhisper: true, FromSeat: 2, ToSeat: 0, Text: "悄悄话:我觉得 4 号 可疑", Round: 1})
	// 公开发言(可见)。
	appendRecallMsg(r, agentcore.ChatMessage{FromSeat: 4, Text: "我觉得 4 号 也可疑", Round: 1})

	out := m.ChatRecall("recall-room-6", 0, "可疑", -1, 0)
	if strings.Contains(out, "刀 5 号") || strings.Contains(out, "秘密") {
		t.Fatalf("other-seat whisper leaked into recall results (§119 violation):\n%s", out)
	}
	if !strings.Contains(out, "悄悄话") {
		t.Fatalf("whisper addressed to me must be searchable:\n%s", out)
	}
	if !strings.Contains(out, "也可疑") {
		t.Fatalf("public chat must be searchable:\n%s", out)
	}
}
