// Package werewolf — emotion_contagion_test.go: §20260812-01 U3 测试用例。
//
// 覆盖:
//   - 4 类情绪传染半径
//   - 强度硬上限 0.45
//   - 半径硬上限 2
//   - 同源同情绪 1 轮内仅注入 1 次
//   - 真人源不传染
//   - 死亡 / 真人目标不接收
//   - 清除房间传染
package werewolf

import (
	"testing"
)

func TestRadiusForEmotion(t *testing.T) {
	cases := []struct {
		kind EmotionContagionKind
		want int
	}{
		{ContagionConfident, 2},
		{ContagionCalm, 2},
		{ContagionNervous, 1},
		{ContagionAngry, 1},
		{EmotionContagionKind("unknown"), 0},
	}
	for _, c := range cases {
		got := RadiusForEmotion(c.kind)
		if got != c.want {
			t.Errorf("RadiusForEmotion(%s) = %d, want %d", c.kind, got, c.want)
		}
	}
}

func TestPromptBlockForContagion(t *testing.T) {
	e := EmotionContagionEntry{
		SourceSeat: 4, // 5 号
		Kind:       ContagionConfident,
		Strength:   0.3,
		Distance:   1,
	}
	got := PromptBlockForContagion(e)
	if got == "" {
		t.Error("confident should yield non-empty block")
	}
	if !containsString(got, "5") {
		t.Errorf("block should mention seat 5, got: %s", got)
	}

	// Nervous
	e.Kind = ContagionNervous
	got = PromptBlockForContagion(e)
	if !containsString(got, "紧张") {
		t.Errorf("nervous block should contain 紧张, got: %s", got)
	}

	// Angry
	e.Kind = ContagionAngry
	got = PromptBlockForContagion(e)
	if !containsString(got, "愤怒") {
		t.Errorf("angry block should contain 愤怒, got: %s", got)
	}

	// Calm
	e.Kind = ContagionCalm
	got = PromptBlockForContagion(e)
	if !containsString(got, "平静") {
		t.Errorf("calm block should contain 平静, got: %s", got)
	}

	// Unknown
	e.Kind = EmotionContagionKind("unknown")
	got = PromptBlockForContagion(e)
	if got != "" {
		t.Errorf("unknown should yield empty, got: %s", got)
	}
}

func TestSpreadContagion_Basic(t *testing.T) {
	// 清理
	roomID := "test-room-1"
	ColorTestContagionRoom(roomID)
	defer ClearContagionForRoom(roomID)

	numSeats := 13
	isAlive := make([]bool, numSeats)
	for i := range isAlive {
		isAlive[i] = true
	}
	isBot := make([]bool, numSeats)
	for i := range isBot {
		isBot[i] = true
	}

	// 7 号(seat=6) 触发 confident
	SpreadContagion(roomID, numSeats, 6, ContagionConfident, 1, isAlive, isBot, false)

	// 1 轮内:邻近 2 座接收(±2 = seats 4,5,7,8)
	// 注:±radius 都不包括自己,所以 7 号不接收
	for _, seat := range []int{4, 5, 7, 8} {
		// 这些座位应该出现传染
		_, _, has := ContagionBuffForView(roomID, numSeats, seat, 1)
		if !has {
			t.Errorf("seat %d should have contagion", seat)
		}
	}

	// 远端座位不应接收
	_, _, has := ContagionBuffForView(roomID, numSeats, 0, 1)
	if has {
		t.Errorf("seat 0 (远端) should NOT have contagion")
	}
}

func TestSpreadContagion_NervousRadius1(t *testing.T) {
	roomID := "test-room-2"
	defer ClearContagionForRoom(roomID)

	numSeats := 13
	isAlive := make([]bool, numSeats)
	isBot := make([]bool, numSeats)
	for i := range isAlive {
		isAlive[i] = true
		isBot[i] = true
	}

	// 7 号(seat=6) 触发 nervous (radius=1)
	SpreadContagion(roomID, numSeats, 6, ContagionNervous, 1, isAlive, isBot, false)

	// 6/8 号接收
	_, _, has6 := ContagionBuffForView(roomID, numSeats, 5, 1)
	_, _, has8 := ContagionBuffForView(roomID, numSeats, 7, 1)
	if !has6 || !has8 {
		t.Errorf("nervous radius=1 should reach 6/8, got 6=%v 8=%v", has6, has8)
	}

	// 5/9 号(seat=4/8) 为 radius=2 的目标—nervous radius=1 不覆盖
	_, _, has5 := ContagionBuffForView(roomID, numSeats, 4, 1)
	_, _, has9 := ContagionBuffForView(roomID, numSeats, 8, 1)
	if has5 || has9 {
		t.Errorf("nervous radius should NOT reach 5/9, got 5=%v 9=%v", has5, has9)
	}
}

func TestSpreadContagion_HumanSource(t *testing.T) {
	roomID := "test-room-3"
	defer ClearContagionForRoom(roomID)

	numSeats := 13
	isAlive := make([]bool, numSeats)
	isBot := make([]bool, numSeats)
	for i := range isAlive {
		isAlive[i] = true
		isBot[i] = true
	}

	// 真人源不传染
	SpreadContagion(roomID, numSeats, 6, ContagionConfident, 1, isAlive, isBot, true)
	_, _, has := ContagionBuffForView(roomID, numSeats, 5, 1)
	if has {
		t.Error("human source should not propagate")
	}
}

func TestSpreadContagion_DeadSource(t *testing.T) {
	roomID := "test-room-4"
	defer ClearContagionForRoom(roomID)

	numSeats := 13
	isAlive := make([]bool, numSeats)
	isBot := make([]bool, numSeats)
	for i := range isAlive {
		isAlive[i] = true
		isBot[i] = true
	}
	isAlive[6] = false // 7 号死

	SpreadContagion(roomID, numSeats, 6, ContagionConfident, 1, isAlive, isBot, false)
	_, _, has := ContagionBuffForView(roomID, numSeats, 5, 1)
	if has {
		t.Error("dead source should not propagate")
	}
}

func TestSpreadContagion_HumanTarget(t *testing.T) {
	roomID := "test-room-5"
	defer ClearContagionForRoom(roomID)

	numSeats := 13
	isAlive := make([]bool, numSeats)
	isBot := make([]bool, numSeats)
	for i := range isAlive {
		isAlive[i] = true
		isBot[i] = true
	}
	isBot[5] = false // 6 号是真人

	SpreadContagion(roomID, numSeats, 6, ContagionConfident, 1, isAlive, isBot, false)
	_, _, has := ContagionBuffForView(roomID, numSeats, 5, 1)
	if has {
		t.Error("human target should not receive contagion")
	}
}

func TestSpreadContagion_SameRoundOnce(t *testing.T) {
	roomID := "test-room-6"
	defer ClearContagionForRoom(roomID)

	numSeats := 13
	isAlive := make([]bool, numSeats)
	isBot := make([]bool, numSeats)
	for i := range isAlive {
		isAlive[i] = true
		isBot[i] = true
	}

	// 同一轮同源同情绪触发 2 次
	SpreadContagion(roomID, numSeats, 6, ContagionConfident, 1, isAlive, isBot, false)
	SpreadContagion(roomID, numSeats, 6, ContagionConfident, 1, isAlive, isBot, false)

	// 5 号 queue 应只有 1 条
	queue := getContagionQueue(roomID, numSeats, 5)
	if len(queue) != 1 {
		t.Errorf("同源同情绪 1 轮内应只注入 1 次, got %d entries", len(queue))
	}

	// 下一轮允许再次注入
	SpreadContagion(roomID, numSeats, 6, ContagionConfident, 2, isAlive, isBot, false)
	queue = getContagionQueue(roomID, numSeats, 5)
	if len(queue) != 2 {
		t.Errorf("下一轮应允许再次注入, got %d entries", len(queue))
	}
}

func TestDrainContagionForPrompt(t *testing.T) {
	roomID := "test-room-7"
	defer ClearContagionForRoom(roomID)

	numSeats := 13
	isAlive := make([]bool, numSeats)
	isBot := make([]bool, numSeats)
	for i := range isAlive {
		isAlive[i] = true
		isBot[i] = true
	}

	SpreadContagion(roomID, numSeats, 6, ContagionConfident, 1, isAlive, isBot, false)
	got := DrainContagionForPrompt(roomID, numSeats, 5, 1)
	if got == "" {
		t.Error("expected non-empty prompt block")
	}
	if !containsString(got, "自信传染") {
		t.Errorf("prompt should contain 自信传染, got: %s", got)
	}

	// 轮次过期(r=3 时 r=1 注入的传染已失效,因 ExpiresRound=2)
	got = DrainContagionForPrompt(roomID, numSeats, 5, 3)
	if got != "" {
		t.Errorf("prompt should be empty after expiry, got: %s", got)
	}
}

func TestClearContagionForRoom(t *testing.T) {
	roomID := "test-room-8"
	numSeats := 13
	isAlive := make([]bool, numSeats)
	isBot := make([]bool, numSeats)
	for i := range isAlive {
		isAlive[i] = true
		isBot[i] = true
	}

	SpreadContagion(roomID, numSeats, 6, ContagionConfident, 1, isAlive, isBot, false)
	ClearContagionForRoom(roomID)

	_, _, has := ContagionBuffForView(roomID, numSeats, 5, 1)
	if has {
		t.Error("after ClearContagionForRoom, no contagion should remain")
	}
}

// ColorTestContagionRoom 测试 helper。
func ColorTestContagionRoom(roomID string) {
	ClearContagionForRoom(roomID)
}
