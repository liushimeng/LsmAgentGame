// §20260812-03 U2 — 私下通道(SecretLetterRoom)单测。
package werewolf

import "testing"

// TestSecretLetterRoom_Basic 验证基本发送/收件流程。
func TestSecretLetterRoom_Basic(t *testing.T) {
	sl := newSecretLetterRoomLocked("room-1")
	alive := map[int]bool{0: true, 1: true, 2: true}

	// 1. 正常发送(0→1)
	l1, err := sl.Send("speak", 0, 1, "你好 1 号,我觉得 2 号是狼人", 1, alive)
	if err != nil {
		t.Fatalf("send 0→1 failed: %v", err)
	}
	if l1.FromSeat != 0 || l1.ToSeat != 1 {
		t.Errorf("letter from/to wrong: %d→%d", l1.FromSeat, l1.ToSeat)
	}

	// 2. 收件箱 1 号应该有 1 封信
	inbox := sl.Inbox(1)
	if len(inbox) != 1 {
		t.Fatalf("seat 1 inbox should have 1 letter, got %d", len(inbox))
	}
	if inbox[0].Body != "你好 1 号,我觉得 2 号是狼人" {
		t.Errorf("body mismatch: %q", inbox[0].Body)
	}

	// 3. 收件箱 0 号(没收到信)应该空
	inbox0 := sl.Inbox(0)
	if len(inbox0) != 0 {
		t.Errorf("seat 0 inbox should be empty, got %d letters", len(inbox0))
	}
}

// TestSecretLetterRoom_Self 验证不可发给自己。
func TestSecretLetterRoom_Self(t *testing.T) {
	sl := newSecretLetterRoomLocked("room-1")
	alive := map[int]bool{0: true, 1: true}
	_, err := sl.Send("speak", 0, 0, "自言自语", 1, alive)
	if err != ErrSecretLetterSelf {
		t.Errorf("expected ErrSecretLetterSelf, got %v", err)
	}
}

// TestSecretLetterRoom_DeadTarget 验证不可发给死亡玩家。
func TestSecretLetterRoom_DeadTarget(t *testing.T) {
	sl := newSecretLetterRoomLocked("room-1")
	alive := map[int]bool{0: true} // 1 号已死
	_, err := sl.Send("speak", 0, 1, "hi", 1, alive)
	if err != ErrSecretLetterDead {
		t.Errorf("expected ErrSecretLetterDead, got %v", err)
	}
}

// TestSecretLetterRoom_WindowClosed 验证仅白天 speak 阶段可发。
func TestSecretLetterRoom_WindowClosed(t *testing.T) {
	sl := newSecretLetterRoomLocked("room-1")
	alive := map[int]bool{0: true, 1: true}
	for _, badPhase := range []string{"vote", "night_wolves", "over", "filling"} {
		_, err := sl.Send(badPhase, 0, 1, "hi", 1, alive)
		if err != ErrSecretLetterClosed {
			t.Errorf("phase %s should reject, got %v", badPhase, err)
		}
	}
}

// TestSecretLetterRoom_DailyLimit 验证每日 5 条上限。
func TestSecretLetterRoom_DailyLimit(t *testing.T) {
	sl := newSecretLetterRoomLocked("room-1")
	alive := map[int]bool{0: true, 1: true, 2: true, 3: true, 4: true, 5: true}
	// 0 号给 1/2/3/4/5 各发一封 = 5 封
	for target := 1; target <= 5; target++ {
		_, err := sl.Send("speak", 0, target, "hi", 1, alive)
		if err != nil {
			t.Fatalf("send #%d failed: %v", target, err)
		}
	}
	// 第 6 封应该被拒
	alive[6] = true
	_, err := sl.Send("speak", 0, 6, "hi", 1, alive)
	if err != ErrSecretLetterLimit {
		t.Errorf("6th send should be rejected with ErrSecretLetterLimit, got %v", err)
	}
}

// TestSecretLetterRoom_Body 验证内容长度 1~200 字。
func TestSecretLetterRoom_Body(t *testing.T) {
	sl := newSecretLetterRoomLocked("room-1")
	alive := map[int]bool{0: true, 1: true}
	// 空内容
	_, err := sl.Send("speak", 0, 1, "", 1, alive)
	if err != ErrSecretLetterBody {
		t.Errorf("empty body should be rejected, got %v", err)
	}
	// 201 字
	longBody := make([]rune, 201)
	for i := range longBody {
		longBody[i] = 'x'
	}
	_, err = sl.Send("speak", 0, 1, string(longBody), 1, alive)
	if err != ErrSecretLetterBody {
		t.Errorf("201-rune body should be rejected, got %v", err)
	}
}

// TestSecretLetterRoom_MarkRead 验证 MarkRead 行为。
func TestSecretLetterRoom_MarkRead(t *testing.T) {
	sl := newSecretLetterRoomLocked("room-1")
	alive := map[int]bool{0: true, 1: true}
	l1, _ := sl.Send("speak", 0, 1, "test", 1, alive)
	// 初始未读
	inbox := sl.Inbox(1)
	if inbox[0].IsRead {
		t.Errorf("newly sent letter should be unread")
	}
	// 标记已读
	sl.MarkRead(1, l1.ID)
	inbox = sl.Inbox(1)
	if !inbox[0].IsRead {
		t.Errorf("after MarkRead, should be read")
	}
}

// TestSecretLetterRoom_Reset 验证 reset() 跨局清空。
func TestSecretLetterRoom_Reset(t *testing.T) {
	sl := newSecretLetterRoomLocked("room-1")
	alive := map[int]bool{0: true, 1: true}
	sl.Send("speak", 0, 1, "test", 1, alive)
	if len(sl.Inbox(1)) != 1 {
		t.Fatalf("inbox should have 1 before reset")
	}
	sl.reset()
	if len(sl.Inbox(1)) != 0 {
		t.Errorf("inbox should be empty after reset, got %d", len(sl.Inbox(1)))
	}
	if sl.sentToday[0] != 0 {
		t.Errorf("sentToday should be reset, got %d", sl.sentToday[0])
	}
}
