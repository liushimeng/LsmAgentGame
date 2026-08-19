package thptypes

// BuildEmptyContext 返回 GameContext 的零值。用于测试 / 单元测试夹具。
func BuildEmptyContext(roomID, userID, modelKey string, seat int) *GameContext {
	return &GameContext{
		RoomID:     roomID,
		GameKind:   "texasholdem",
		MySeat:     seat,
		MyUserID:   userID,
		ModelKey:   modelKey,
		Street:     "waiting",
		ButtonSeat: -1,
		MyHole:     [2]int{},
		Community:  [5]int{},
		Opponents:  []OpponentBrief{},
		ActionHistory: []ActionRecord{},
		ChatHistory:   nil,
		RecentHands:   []HandRecord{},
		HandOverPlayers: []PlayerNetChip{},
		BotIdentity: BotIdentityBrief{
			UserID:     userID,
			ModelKey:   modelKey,
			AgentClass: "LsmAgentGame-TexasHoldem-Player",
		},
		EconTier:         "health",
		MyTurn:           false,
		TimeRemainingSec: 30,
	}
}