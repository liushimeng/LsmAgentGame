package wealth

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestClientState_NoNullArrays 回归: 前端 render 崩溃(null.map/forEach) ——
// game.state 所有数组字段对玩家(viewer=0)与观战者(viewer=-1)都必须序列化
// 为 [] 而非 null。
func TestClientState_NoNullArrays(t *testing.T) {
	r := NewWealthRoom("room-null", 3000, "curated", 7, 4)
	r.RegisterBotSeats(map[int]string{1: "b1", 2: "b2"}, map[int]string{1: "MA", 2: "MB"}, nil)
	if _, _, e := r.JoinGame("h0", "human"); e != nil {
		t.Fatal(e)
	}
	if e := r.Start(nil); e != nil {
		t.Fatalf("start: %v", e)
	}
	fields := []string{"players", "districts", "assets", "loans", "goals",
		"ledger_recent", "events_recent", "bot_contexts", "detail", "summaries", "history"}
	for _, viewer := range []int{0, -1} {
		cs := BuildClientState("room-null", viewer, r.Engine(), r.SnapshotSeats(), r.SnapshotNicknames(), r.SnapshotBotSeats(), r.SnapshotModelKeys(), r.SnapshotTranscripts(), r.GameStartedAtUnix(), r.NextMonthAtUnix())
		raw, err := json.Marshal(cs)
		if err != nil {
			t.Fatal(err)
		}
		s := string(raw)
		for _, field := range fields {
			if strings.Contains(s, `"`+field+`":null`) {
				t.Errorf("viewer=%d: field %q serialized as null", viewer, field)
			}
		}
	}
}
