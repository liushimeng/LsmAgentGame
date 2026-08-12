// §20260811-03 U1 — RumorGraph 单元测试。
//
// 覆盖矩阵(5 项):
//   R-01: AddRumorEdgeLocked 基础路径
//   R-02: Hop 衰减(二次传播)
//   R-03: 死亡玩家清理(PurgeByDeathLocked)
//   R-04: Daily cap 拦截
//   R-05: RumorPrefixForHop 渲染

package werewolf

import (
	"strings"
	"testing"
	"time"
)

// R-01: AddRumorEdgeLocked 基础路径
func TestRumorGraph_AddRumorEdge(t *testing.T) {
	r := newTestRoom(13)
	r.State.Players[0].Alive = true
	r.State.Players[3].Alive = true

	edge, err := r.AddRumorEdgeLocked(0, 3, "听说 5 号是预言家", 0, 0.8, 1, 1)
	if err != nil {
		t.Fatalf("AddRumorEdge: %v", err)
	}
	if edge.ID != 1 || edge.FromSeat != 0 || edge.ToSeat != 3 {
		t.Fatalf("unexpected edge: %+v", edge)
	}
}

// R-02: Hop 衰减
func TestRumorGraph_HopIncrement(t *testing.T) {
	r := newTestRoom(13)
	for i := range r.State.Players {
		r.State.Players[i].Alive = true
	}

	// 0 → 1 (hop=0)
	e1, _ := r.AddRumorEdgeLocked(0, 1, "A", 0, 0.9, 1, 1)
	// 1 → 2 (hop=1)
	e2, _ := r.AddRumorEdgeLocked(1, 2, "A", 1, 0.9, 1, 1)
	// 2 → 3 (hop=2)
	e3, _ := r.AddRumorEdgeLocked(2, 3, "A", 2, 0.9, 1, 1)

	if e1.Hop != 0 || e2.Hop != 1 || e3.Hop != 2 {
		t.Fatalf("hop mismatch: %d/%d/%d", e1.Hop, e2.Hop, e3.Hop)
	}

	// 验证 inbox
	inbox := r.GetRumorInboxLocked(3)
	if len(inbox) != 1 || inbox[0].Hop != 2 {
		t.Fatalf("inbox mismatch: %+v", inbox)
	}
}

// R-03: 死亡清理
func TestRumorGraph_PurgeByDeath(t *testing.T) {
	r := newTestRoom(13)
	for i := range r.State.Players {
		r.State.Players[i].Alive = true
	}
	r.AddRumorEdgeLocked(0, 5, "msg", 0, 0.5, 1, 1)

	// 杀 5 号
	r.State.Players[5].Alive = false
	r.PurgeByDeathLocked(5)

	inbox := r.GetRumorInboxLocked(5)
	if len(inbox) != 0 {
		t.Fatalf("dead player inbox should be empty: %+v", inbox)
	}
}

// R-04: Daily cap 拦截
func TestRumorGraph_DailyCap(t *testing.T) {
	r := newTestRoom(13)
	for i := range r.State.Players {
		r.State.Players[i].Alive = true
	}

	// 第一条成功
	_, err := r.AddRumorEdgeLocked(0, 1, "msg1", 0, 0.5, 1, 1)
	if err != nil {
		t.Fatalf("first add: %v", err)
	}
	// 第二条同一天应被拒
	_, err = r.AddRumorEdgeLocked(0, 2, "msg2", 0, 0.5, 1, 1)
	if err == nil {
		t.Fatal("expected daily cap error, got nil")
	}
	if !strings.Contains(err.Error(), "daily cap") {
		t.Fatalf("unexpected error: %v", err)
	}
	// 第二天允许
	_, err = r.AddRumorEdgeLocked(0, 3, "msg3", 0, 0.5, 1, 2)
	if err != nil {
		t.Fatalf("day 2 add: %v", err)
	}
}

// R-05: RumorPrefixForHop 渲染
func TestRumorGraph_PrefixRender(t *testing.T) {
	cases := []struct {
		hop      int
		expected string
	}{
		{-1, ""},
		{0, ""},
		{1, "[传闻] "},
		{2, "[传闻×2] "},
		{3, "[传闻·来源不可考] "},
		{4, "[传闻·来源不可考] "},
	}
	for _, c := range cases {
		got := RumorPrefixForHop(c.hop)
		if got != c.expected {
			t.Errorf("hop=%d: got %q, want %q", c.hop, got, c.expected)
		}
	}
}

// 辅助函数:构造一个最小测试房间(固定 13 座位,符合 werewolf 13 人局规模)
func newTestRoom(seatCount int) *WerewolfRoom {
	_ = seatCount // 13 人局固定规模,参数保留仅为可读
	var players [13]Player
	r := &WerewolfRoom{
		RoomID:     "test",
		State:      &GameState{Players: players},
		rumorGraph: NewRumorGraph(),
	}
	for i := range r.State.Players {
		r.State.Players[i].Alive = false
	}
	return r
}

// 防止 time.Now() 被频繁调用引起的测试抖动
func TestRumorGraph_PromptBlockEmpty(t *testing.T) {
	r := newTestRoom(13)
	for i := range r.State.Players {
		r.State.Players[i].Alive = true
	}
	block := r.RumorInboxPromptBlock(0)
	if block != "" {
		t.Fatalf("expected empty prompt block, got %q", block)
	}
}

func TestRumorGraph_PromptBlockNonEmpty(t *testing.T) {
	r := newTestRoom(13)
	for i := range r.State.Players {
		r.State.Players[i].Alive = true
	}
	r.AddRumorEdgeLocked(0, 1, "test rumor", 1, 0.9, 1, 1)
	block := r.RumorInboxPromptBlock(1)
	if !strings.Contains(block, "test rumor") || !strings.Contains(block, "[传闻]") {
		t.Fatalf("prompt block missing key parts: %q", block)
	}
}

// 时间常量引用,防止编译器抱怨 unused import
var _ = time.Second
