// game_service_texas_bot_normalize_test.go — §B4 动作规范化回归测试(2026-08-20)。
//
// 对齐 [德州扑克Agent工具协议.md] §10 + [德州扑克Agent金币设计.md] §4.3。
// normalizeBotAction 是纯函数,直接构造 BotGameContextSnapshot 测试。
package ws

import (
	"testing"

	"LsmAgentGame/agent/thpagent"
	"LsmAgentGame/game/texasholdem"
)

func normSnap() *texasholdem.BotGameContextSnapshot {
	return &texasholdem.BotGameContextSnapshot{
		RoomID:           "room-norm",
		MySeat:           0,
		MyStack:          10000,
		MyRoundCommitted: 0,
		Pot:              300,
		CurrentBet:       200,
		MinRaise:         200,
		BigBlind:         200,
	}
}

// TestB4_RaiseBelowMinLifted: LLM raise amount < min_raise → 自动抬到
// CurrentBet+MinRaise(协议 §10,不是报错后 fold)。
func TestB4_RaiseBelowMinLifted(t *testing.T) {
	out := normalizeBotAction(normSnap(), thpagent.Action{Type: thpagent.ActRaise, Amount: 300})
	if out.Type != texasholdem.ActRaise || out.Amount != 400 {
		t.Errorf("raise 300 → got (%s, %d), want (raise, 400)", out.Type, out.Amount)
	}
}

// TestB4_RaiseAboveStackBecomesAllIn: amount > 可动用筹码 → 改 allin(协议 §10)。
func TestB4_RaiseAboveStackBecomesAllIn(t *testing.T) {
	out := normalizeBotAction(normSnap(), thpagent.Action{Type: thpagent.ActRaise, Amount: 99999})
	if out.Type != texasholdem.ActAllIn {
		t.Errorf("raise 99999 with stack 10000 → got %s, want allin", out.Type)
	}
}

// TestB4_RaiseUnaffordableMinBecomesAllIn: 剩余筹码连最小加注都不够 → 改 allin。
func TestB4_RaiseUnaffordableMinBecomesAllIn(t *testing.T) {
	snap := normSnap()
	snap.MyStack = 150 // stackTotal=150 < minRaiseTotal=400
	out := normalizeBotAction(snap, thpagent.Action{Type: thpagent.ActRaise, Amount: 400})
	if out.Type != texasholdem.ActAllIn {
		t.Errorf("unaffordable min raise → got (%s, %d), want allin", out.Type, out.Amount)
	}
}

// TestB4_BetBelowBigBlindLifted: bet < bigBlind → 抬到 bigBlind。
func TestB4_BetBelowBigBlindLifted(t *testing.T) {
	snap := normSnap()
	snap.CurrentBet = 0 // 无人下注时才允许 bet
	out := normalizeBotAction(snap, thpagent.Action{Type: thpagent.ActBet, Amount: 100})
	if out.Type != texasholdem.ActBet || out.Amount != 200 {
		t.Errorf("bet 100 → got (%s, %d), want (bet, 200)", out.Type, out.Amount)
	}
}

// TestB4_BetWholeStackBecomesAllIn: bet ≥ 全部可动用筹码 → 改 allin。
func TestB4_BetWholeStackBecomesAllIn(t *testing.T) {
	snap := normSnap()
	snap.CurrentBet = 0
	out := normalizeBotAction(snap, thpagent.Action{Type: thpagent.ActBet, Amount: 10000})
	if out.Type != texasholdem.ActAllIn {
		t.Errorf("bet = whole stack → got %s, want allin", out.Type)
	}
}

// TestB4_AllInSmallIntentBecomesRaise90: 金币设计 §4.3 —— allin 声明 amount
// < 90% 筹码总量 且 剩余筹码 ≥ 2×bigBlind → 改 raise 到 90% 筹码总量。
func TestB4_AllInSmallIntentBecomesRaise90(t *testing.T) {
	out := normalizeBotAction(normSnap(), thpagent.Action{Type: thpagent.ActAllIn, Amount: 5000})
	if out.Type != texasholdem.ActRaise || out.Amount != 9000 {
		t.Errorf("allin intent 5000 → got (%s, %d), want (raise, 9000)", out.Type, out.Amount)
	}
}

// TestB4_AllInShortStackAllowed: 剩余筹码 < 2×bigBlind → 允许真 allin(§4.3 例外)。
func TestB4_AllInShortStackAllowed(t *testing.T) {
	snap := normSnap()
	snap.MyStack = 300 // < 2*200
	out := normalizeBotAction(snap, thpagent.Action{Type: thpagent.ActAllIn, Amount: 100})
	if out.Type != texasholdem.ActAllIn {
		t.Errorf("short-stack allin → got (%s, %d), want allin", out.Type, out.Amount)
	}
}

// TestB4_AllInFullIntentKept: 声明 amount ≥ 90% 筹码总量 → 保持 allin。
func TestB4_AllInFullIntentKept(t *testing.T) {
	out := normalizeBotAction(normSnap(), thpagent.Action{Type: thpagent.ActAllIn, Amount: 9500})
	if out.Type != texasholdem.ActAllIn {
		t.Errorf("allin intent 9500 (>=90%%) → got (%s, %d), want allin", out.Type, out.Amount)
	}
}

// TestB4_PassThrough: fold/call 透传不被改动。
// 注意: check 在 callAmount=0 时透传,callAmount>0 时转为 call(见
// TestR22_CheckIllegalWhenBetToCall)。
func TestB4_PassThrough(t *testing.T) {
	for _, typ := range []string{thpagent.ActFold, thpagent.ActCall} {
		out := normalizeBotAction(normSnap(), thpagent.Action{Type: typ})
		want := convertToEngineAction(thpagent.Action{Type: typ})
		if out != want {
			t.Errorf("%s should pass through unchanged, got %+v want %+v", typ, out, want)
		}
	}
	// check 在 callAmount=0 时透传
	snap := normSnap()
	snap.CurrentBet = 0 // callAmount = 0 - 0 = 0
	snap.CallAmount = 0
	out := normalizeBotAction(snap, thpagent.Action{Type: thpagent.ActCheck})
	if out.Type != texasholdem.ActCheck {
		t.Errorf("check when callAmount=0 should pass through, got %s", out.Type)
	}
}

// TestR22_CheckIllegalWhenBetToCall: §R22-P1 —— LLM 在有 bet-to-call 时输出
// check,normalizeBotAction 必须自动转为 call,避免 engine 拒绝(30007)后
// bot driver 停止驱动致游戏卡死。
func TestR22_CheckIllegalWhenBetToCall(t *testing.T) {
	// 场景: BB 已投 $50,Button call $50 → CurrentBet=50,BB RoundCommitted=50,
	// CallAmount=0 → check 合法(不应转换)
	snap := &texasholdem.BotGameContextSnapshot{
		RoomID:           "room-r22",
		MySeat:           2,
		MyStack:          2450,
		MyRoundCommitted: 50,
		Pot:              125,
		CurrentBet:       50,
		CallAmount:       0, // 50 - 50 = 0,check 合法
		MinRaise:         50,
		BigBlind:         50,
	}
	out := normalizeBotAction(snap, thpagent.Action{Type: thpagent.ActCheck})
	if out.Type != texasholdem.ActCheck {
		t.Errorf("BB check when callAmount=0 should remain check, got %s", out.Type)
	}

	// 场景: BB 已投 $50,Button raise 到 $100 → CurrentBet=100,
	// CallAmount=50 → check 非法,必须转 call
	snap2 := &texasholdem.BotGameContextSnapshot{
		RoomID:           "room-r22",
		MySeat:           2,
		MyStack:          2400,
		MyRoundCommitted: 50,
		Pot:              200,
		CurrentBet:       100,
		CallAmount:       50, // 100 - 50 = 50,check 非法
		MinRaise:         50,
		BigBlind:         50,
	}
	out2 := normalizeBotAction(snap2, thpagent.Action{Type: thpagent.ActCheck})
	if out2.Type != texasholdem.ActCall {
		t.Errorf("BB check when callAmount=50 should convert to call, got %s", out2.Type)
	}
	if out2.Amount != 0 {
		t.Errorf("converted call amount should be 0, got %d", out2.Amount)
	}
}
