package doudizhu

// 本文件负责把权威 GameState 投影成「某个座位可见」的客户端视图。
// 隐藏信息规则：玩家只见自己完整手牌，其余座位仅见张数；底牌在叫地主结束后亮出。

// CardJSON 是发送给客户端的牌（与 Card 同构，独立类型便于未来扩展）。
type CardJSON struct {
	Rank int `json:"rank"`
	Suit int `json:"suit"`
}

func cardsToJSON(cards []Card) []CardJSON {
	out := make([]CardJSON, 0, len(cards))
	for _, c := range cards {
		out = append(out, CardJSON{Rank: c.Rank, Suit: c.Suit})
	}
	return out
}

// LastPlayJSON 是出牌区展示的上一手。
type LastPlayJSON struct {
	Seat  int        `json:"seat"`
	Cards []CardJSON `json:"cards"`
}

// ClientGameState 是发送给某个座位的对局视图。
type ClientGameState struct {
	RoomID       string        `json:"room_id"`
	Seats        []string      `json:"seats"`         // 三个座位的 userID（空串=空位）
	MySeat       int           `json:"my_seat"`       // 接收方座位
	Ready        bool          `json:"ready"`         // 是否满 3 人
	Phase        string        `json:"phase"`         // bidding / playing / over
	Turn         int           `json:"turn"`          // 当前行动座位
	Status       string        `json:"status"`        // playing / landlord_win / farmer_win
	MyHand       []CardJSON    `json:"my_hand"`       // 自己手牌（完整）
	HandCounts   []int         `json:"hand_counts"`   // 三家剩余张数
	FirstBidder  int           `json:"first_bidder"`  // 首叫座位
	Bids         []int         `json:"bids"`          // 各座位叫分（-1 未叫）
	CurrentBid   int           `json:"current_bid"`   // 当前最高叫分
	LandlordSeat int           `json:"landlord_seat"` // 地主座位（-1 未定）
	Bottom       []CardJSON    `json:"bottom"`        // 底牌（叫地主结束后亮出，否则空）
	LastPlay     *LastPlayJSON `json:"last_play"`     // 上一手有效出牌（nil 表示自由出牌）
	Multiplier   int           `json:"multiplier"`    // 当前倍数
	BombCount    int           `json:"bomb_count"`    // 炸弹/火箭次数
	Winner       string        `json:"winner"`        // landlord / farmer / ""
	Score        int           `json:"score"`         // 本局基础分×倍数（结束后有意义）
}

// BuildClientState 构造座位 viewer 的可见视图。seats 为房间三座位 userID。
//
// 当 viewer == -1 时，调用方表示"我是一名观察者，没有座位"；此时 MySeat
// 设为 -1 且 MyHand 不填充（观察者看不到任何玩家的手牌）。HandCounts 和
// 底牌等公开信息仍照常填充。
func BuildClientState(roomID string, seats [3]string, viewer int, gs *GameState) *ClientGameState {
	cs := &ClientGameState{
		RoomID:       roomID,
		Seats:        seats[:],
		MySeat:       viewer,
		LandlordSeat: noSeat,
		HandCounts:   []int{0, 0, 0},
		Bids:         []int{-1, -1, -1},
		Multiplier:   1,
		Status:       StatusPlaying.String(),
	}

	ready := seats[0] != "" && seats[1] != "" && seats[2] != ""
	cs.Ready = ready

	if gs == nil {
		cs.Phase = PhaseBidding.String()
		return cs
	}

	cs.Phase = gs.Phase.String()
	cs.Turn = gs.Turn
	cs.Status = gs.Status.String()
	cs.FirstBidder = gs.FirstBidder
	cs.Bids = gs.Bids[:]
	cs.CurrentBid = gs.CurrentBid
	cs.LandlordSeat = gs.LandlordSeat
	cs.Multiplier = gs.Multiplier
	cs.BombCount = gs.BombCount
	cs.Winner = gs.Winner()

	// 自己手牌完整可见——但仅在确实有一把自己的手牌时填充（观察者 viewer == -1 时跳过）。
	if viewer >= 0 && viewer < 3 {
		cs.MyHand = cardsToJSON(gs.Hands[viewer])
	}
	for s := 0; s < 3; s++ {
		cs.HandCounts[s] = len(gs.Hands[s])
	}

	// 底牌：叫地主结束（进入出牌或结束）后对所有人（含观察者）亮出。
	if gs.Phase == PhasePlaying || gs.Phase == PhaseOver {
		cs.Bottom = cardsToJSON(gs.Bottom)
		cs.Score = gs.Score()
	}

	// 上一手有效出牌——观察者也能看到。
	if gs.Last != nil {
		cs.LastPlay = &LastPlayJSON{Seat: gs.Last.Seat, Cards: cardsToJSON(gs.Last.Cards)}
	}

	return cs
}
