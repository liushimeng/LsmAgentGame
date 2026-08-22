// 2026-08-22 §BUG-TEXAS 公平性 — bot 思考气泡脱敏正则测试。
// 对局进行中 thought 的真实底牌与精确牌力必须被替换,摊牌/结束后保留原文。
package texasholdem

import "testing"

func TestSanitizeBotThought_StripsHoleCards(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		want   string
	}{
		{
			name:  "R16 真实泄露样例:底牌+蒙特卡洛牌力(0.286 是底池赔率公开盈亏比,保留)",
			input: "我的底牌是K♣J♦,蒙特卡洛牌力0.591,远高于底池赔率所需的0.286胜率",
			want:  "我的底牌是????,蒙特卡洛牌力***,远高于底池赔率所需的0.286胜率",
		},
		{
			name:  "转牌圈弱牌 check 推理",
			input: "底池120,当前免费check…牌力0.274偏弱…不宜主动下注暴露牌力",
			want:  "底池120,当前免费check…牌力***偏弱…不宜主动下注暴露牌力",
		},
		{
			name:  "多张公共牌+equity",
			input: "河牌4♠,我有J♥9♥,equity0.412跟注",
			want:  "河牌??,我有????,equity***跟注",
		},
		{
			name:  "10 点数牌张",
			input: "公共牌10♣6♦A♠,胜率0.733",
			want:  "公共牌??????,胜率***",
		},
		{
			name:  "空串原样返回",
			input: "",
			want:  "",
		},
		{
			name:  "无敏感信息保持不变",
			input: "底池120,选择跟注观察转牌",
			want:  "底池120,选择跟注观察转牌",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeBotThought(tc.input)
			if got != tc.want {
				t.Errorf("sanitizeBotThought(%q)\n got=%q\nwant=%q", tc.input, got, tc.want)
			}
		})
	}
}

// TestBuildClientStateWithRoom_ThoughtSanity 端到端验证:对局进行中
// BotHeartThought 被脱敏(对手/观战者视角),showdown/over 后保留原文。
func TestBuildClientStateWithRoom_ThoughtSanity(t *testing.T) {
	// 构造最小 GameState:座 0/1 占用,翻牌前(PhasePreflop)。
	gs := &GameState{Button: 0, BigBlind: 10, Street: PhasePreflop}
	gs.Players[0].UserID = "human"
	gs.Players[0].Seat = 0
	gs.Players[1].UserID = "bot"
	gs.Players[1].Seat = 1
	gs.Players[1].Hole = [2]Card{{Rank: 13, Suit: 1}, {Rank: 11, Suit: 2}} // K♣J♦

	botSeats := [MaxPlayers]bool{false, true}
	botHeartThought := [MaxPlayers]string{}
	botHeartThought[1] = "我的底牌是K♣J♦,牌力0.591"

	// 对局进行中(viewer=人类座位 0)→ 脱敏。
	cs := BuildClientStateWithRoom("room1", [MaxPlayers]string{"human", "bot"},
		botSeats, [MaxPlayers]string{}, 0, gs, botHeartThought, [MaxPlayers]bool{})
	if cs.BotHeartThought[1] == botHeartThought[1] {
		t.Errorf("preflop: thought 未脱敏,泄露底牌原文=%q", cs.BotHeartThought[1])
	}
	if cs.BotHeartThought[1] == "" {
		t.Errorf("preflop: thought 不应被清空")
	}

	// showdown 阶段 → 保留原文(底牌已公开)。
	gs.Street = PhaseShowdown
	cs2 := BuildClientStateWithRoom("room1", [MaxPlayers]string{"human", "bot"},
		botSeats, [MaxPlayers]string{}, 0, gs, botHeartThought, [MaxPlayers]bool{})
	if cs2.BotHeartThought[1] != botHeartThought[1] {
		t.Errorf("showdown: thought 应保留原文, got=%q", cs2.BotHeartThought[1])
	}

	// 观战者视角(viewer=-1)对局进行中 → 同样脱敏。
	gs.Street = PhasePreflop
	cs3 := BuildClientStateWithRoom("room1", [MaxPlayers]string{"human", "bot"},
		botSeats, [MaxPlayers]string{}, -1, gs, botHeartThought, [MaxPlayers]bool{})
	if cs3.BotHeartThought[1] == botHeartThought[1] {
		t.Errorf("spectator preflop: thought 未脱敏,泄露底牌原文=%q", cs3.BotHeartThought[1])
	}
}
