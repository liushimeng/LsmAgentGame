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
	// 2026-09-16 §12 座扩容:MinSeats=10,注册 10 个 bot 座位开局(测试只关心
	// BuildClientState 不输出 null 数组,不关心具体人数)。
	botUsers := make(map[int]string, 10)
	botModels := make(map[int]string, 10)
	for seat := 0; seat < 10; seat++ {
		botUsers[seat] = "b" + string(rune('0'+seat))
		botModels[seat] = "M" + string(rune('A'+seat))
	}
	r.RegisterBotSeats(botUsers, botModels, nil)
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
