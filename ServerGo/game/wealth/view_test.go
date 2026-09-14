package wealth

import (
	"encoding/json"
	"testing"

	"LsmAgentGame/game/wealth/profession"
)

// TestBuildClientState_Desensitization §7:玩家只见自己 my;他人 my=nil。
func TestBuildClientState_Desensitization(t *testing.T) {
	w := NewWorld(1, [MaxSeats]profession.Card{{ID: "P01"}, {ID: "P02"}})
	w.Players[0] = newPlayerFromCard(0, w.Players[0].Card)
	w.Players[0].Cash = 10000
	w.Players[1] = newPlayerFromCard(1, w.Players[1].Card)
	w.Players[1].Cash = 20000
	w.StartGame()
	// 给 bot_contexts 注入测试内容。
	w.Players[0].Card = *profession.CuratedByID("P09")
	w.Players[1].Card = *profession.CuratedByID("P10")

	// 模拟座位 0 玩家的 view(只看自己 my;bot_contexts 包含自己 + 观战者视角)。
	cs := BuildClientState("room-1", 0, w, [MaxSeats]string{"u:0", "u:1", "", "", "", "", "", ""},
		[MaxSeats]string{"玩家0", "玩家1", "", "", "", "", "", ""},
		[MaxSeats]bool{false, false, false, false, false, false, false, false},
		[MaxSeats]string{"", "", "", "", "", "", "", ""},
		[MaxSeats]BotTranscript{},
		0, 0,
	)
	if cs == nil {
		t.Fatalf("nil state")
	}
	if cs.My == nil {
		t.Errorf("viewer 0 should have my filled")
	}
	if cs.MySeat != 0 {
		t.Errorf("my_seat: got %d, want 0", cs.MySeat)
	}
	if cs.My != nil && cs.My.Cash != 10000 {
		t.Errorf("my cash: got %d, want 10000", cs.My.Cash)
	}
}

// TestBuildClientState_SpectatorView 观战者(viewer=-1)my=nil, my_seat=-1,bot_contexts 全可见。
func TestBuildClientState_SpectatorView(t *testing.T) {
	w := NewWorld(1, [MaxSeats]profession.Card{{ID: "P01"}})
	w.Players[0] = newPlayerFromCard(0, w.Players[0].Card)
	w.Players[0].Cash = 10000
	w.StartGame()
	cs := BuildClientState("room-1", -1, w, [MaxSeats]string{"u:0"},
		[MaxSeats]string{"玩家0"},
		[MaxSeats]bool{true},
		[MaxSeats]string{"MeiTuan-model"},
		[MaxSeats]BotTranscript{},
		0, 0,
	)
	if cs.My != nil {
		t.Errorf("spectator my should be nil")
	}
	if cs.MySeat != -1 {
		t.Errorf("spectator my_seat: got %d, want -1", cs.MySeat)
	}
	if len(cs.BotContexts) == 0 {
		t.Errorf("spectator should see bot contexts")
	}
}

// TestBuildClientState_NonViewerBotFiltered 他人玩家视角时,其他 bot_contexts 被过滤。
func TestBuildClientState_NonViewerBotFiltered(t *testing.T) {
	w := NewWorld(1, [MaxSeats]profession.Card{{ID: "P01"}, {ID: "P02"}})
	w.Players[0] = newPlayerFromCard(0, w.Players[0].Card)
	w.Players[1] = newPlayerFromCard(1, w.Players[1].Card)
	w.StartGame()
	transcripts := [MaxSeats]BotTranscript{
		{LastDecisionSummary: "我做了某事"},
		{LastDecisionSummary: "另一个人做了某事"},
		{}, {}, {}, {}, {}, {},
	}
	// viewer=0:应只看到 0 的 bot_contexts。
	cs := BuildClientState("room-1", 0, w,
		[MaxSeats]string{"u:0", "u:1"},
		[MaxSeats]string{"", ""},
		[MaxSeats]bool{true, true},
		[MaxSeats]string{"model1", "model2"},
		transcripts, 0, 0,
	)
	if len(cs.BotContexts) != 1 {
		t.Errorf("non-spectator viewer 0 should see 1 bot ctx, got %d", len(cs.BotContexts))
	}
	if len(cs.BotContexts) > 0 && cs.BotContexts[0].Seat != 0 {
		t.Errorf("filtered bot ctx seat: got %d, want 0", cs.BotContexts[0].Seat)
	}
}

// TestBuildClientState_JSONEncodable 顶层 ClientGameState 应能被 encoding/json 序列化。
func TestBuildClientState_JSONEncodable(t *testing.T) {
	w := NewWorld(1, [MaxSeats]profession.Card{{ID: "P01"}})
	w.Players[0] = newPlayerFromCard(0, w.Players[0].Card)
	w.Players[0].Cash = 50000
	w.StartGame()
	cs := BuildClientState("room-1", 0, w,
		[MaxSeats]string{"u:0"},
		[MaxSeats]string{"玩家0"},
		[MaxSeats]bool{false},
		[MaxSeats]string{""},
		[MaxSeats]BotTranscript{},
		0, 0,
	)
	if _, err := json.Marshal(cs); err != nil {
		t.Errorf("json marshal: %v", err)
	}
}