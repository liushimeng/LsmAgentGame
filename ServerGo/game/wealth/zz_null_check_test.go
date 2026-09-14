package wealth

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestClientState_NoNullArrays 回归: 前端 render 崩溃(null.map) ——
// game.state 所有数组字段必须序列化为 [] 而非 null。
func TestClientState_NoNullArrays(t *testing.T) {
	r := NewWealthRoom("room-null", 3000, "curated", 7, 4)
	r.RegisterBotSeats(map[int]string{1: "b1", 2: "b2"}, map[int]string{1: "MA", 2: "MB"}, nil)
	if _, _, e := r.JoinGame("h0", "human"); e != nil {
		t.Fatal(e)
	}
	if e := r.Start(nil); e != nil {
		t.Fatalf("start: %v", e)
	}
	cs := BuildClientState("room-null", 0, r.Engine(), r.SnapshotSeats(), r.SnapshotNicknames(), r.SnapshotBotSeats(), r.SnapshotModelKeys(), r.SnapshotTranscripts(), r.GameStartedAtUnix(), r.NextMonthAtUnix())
	raw, err := json.Marshal(cs)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, field := range []string{"players", "districts", "assets", "loans", "goals", "ledger_recent", "events_recent", "bot_contexts", "detail", "summaries", "history"} {
		if strings.Contains(s, `"`+field+`":null`) {
			t.Errorf("field %q serialized as null", field)
		}
	}
	t.Logf("payload sample: %s", s[:600])
}
