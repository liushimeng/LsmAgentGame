package texasholdem

// view.go — 把权威 GameState 投影成「某个座位可见」的客户端视图。
// 隐藏信息规则：玩家只见自己完整底牌，其余座位仅见张数；摊牌后亮出未弃牌者手牌。

// CardJSON 是发送给客户端的牌。
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

// PlayerJSON 是单个座位的公开信息。
//
// 注意:Hole / ShowdownHands / Community / MyHole 字段必须保持"空切片而非 nil",
// 否则 JSON 序列化会输出 null,前端 `array.length` 访问即抛
// `Cannot read properties of null (reading 'length')`(BUG-TEXAS-HOLE-NULL)。
// 初始化时通过 cardsToJSON 返回的 make() 切片已经天然非 nil;但其他需要
// 兜底的字段在客户端再做一次 Array.isArray 兜底。
type PlayerJSON struct {
	UserID         string     `json:"user_id"`
	Hole           []CardJSON `json:"hole"`       // 仅自己可见完整；摊牌后可见未弃牌者。其余座位 / 观察者统一为空数组 `[]`,绝不为 null。
	HoleCount      int        `json:"hole_count"` // 其余玩家只见张数
	Folded         bool       `json:"folded"`
	AllIn          bool       `json:"all_in"`
	Stack          int        `json:"stack"`
	ChipsCommitted int        `json:"chips_committed"`  // 本手总投入（用于 UI 显示"$已下注"）
	RoundCommitted int        `json:"round_committed"`  // 本街累计下注（用于 canCheck 计算）
	Seat           int        `json:"seat"`
	HasPlayer      bool       `json:"has_player"` // 该座位是否有人
}

// ClientGameState 是发送给某个座位的完整对局视图。
type ClientGameState struct {
	RoomID         string         `json:"room_id"`
	GameKind       string         `json:"game_kind"`
	Seats          []string       `json:"seats"`          // 6 个座位的 userID
	MySeat         int            `json:"my_seat"`
	Ready          bool           `json:"ready"`
	Phase          string         `json:"phase"`
	Turn           int            `json:"turn"`
	Pot            int            `json:"pot"`
	CurrentBet     int            `json:"current_bet"`
	BigBlind       int            `json:"big_blind"`
	Button         int            `json:"button"`
	Community      []CardJSON     `json:"community"`       // 已亮出的公共牌
	CommunityCount int            `json:"community_count"` // 已亮出张数
	Players        []PlayerJSON   `json:"players"`         // 6 个座位的信息
	MyHole         []CardJSON     `json:"my_hole"`         // 自己的底牌
	HandNumber     int            `json:"hand_number"`
	Winners        []int          `json:"winners,omitempty"`
	ShowdownHands  [][]CardJSON   `json:"showdown_hands,omitempty"`
	Status         string         `json:"status"`
}

// BuildClientState 构造座位 viewer 的可见视图。
//
// 当 viewer == -1 时表示"观察者"：MySeat 设为 -1，不填充任何玩家的 Hole
//（即使在摊牌阶段也保持底牌隐藏），同时不暴露 MyHole。 公共牌、玩家栈、
// 行动状态、胜负结果等公开信息仍照常填充。
func BuildClientState(roomID string, seats [MaxPlayers]string, viewer int, gs *GameState) *ClientGameState {
	cs := &ClientGameState{
		RoomID:     roomID,
		GameKind:   "texasholdem",
		Seats:      seats[:],
		MySeat:     viewer,
		Phase:      PhaseWaiting.String(),
		BigBlind:   200,
		Button:     -1,
		Status:     StatusPlaying.String(),
		Players:    make([]PlayerJSON, MaxPlayers),
		// Community / MyHole / ShowdownHands 初始为空切片（非 nil），保证 JSON 输出 `[]` 而非 `null`。
		Community: []CardJSON{},
		MyHole:    []CardJSON{},
	}

	// Ready = 至少 2 人入座
	occupied := 0
	for _, u := range seats {
		if u != "" {
			occupied++
		}
	}
	cs.Ready = occupied >= 2

	if gs == nil {
		return cs
	}

	cs.Phase = gs.Street.String()
	cs.Turn = gs.Turn
	cs.Pot = gs.Pot
	cs.CurrentBet = gs.CurrentBet
	cs.BigBlind = gs.BigBlind
	cs.Button = gs.Button
	cs.HandNumber = gs.HandNumber
	cs.Status = gs.Status.String()
	cs.Winners = gs.Winners

	// 公共牌：只亮出已显示的——观察者也可见。
	if gs.CommunityShown > 0 {
		cs.Community = cardsToJSON(gs.Community[:gs.CommunityShown])
		cs.CommunityCount = gs.CommunityShown
	}

	// 玩家视图
	isSpectator := viewer < 0 || viewer >= MaxPlayers
	for i := 0; i < MaxPlayers; i++ {
		p := &gs.Players[i]
		// 始终把 Hole 初始化为空切片（不是 nil），JSON 序列化输出 `[]` 而非 `null`，
		// 前端就不会因为 `null.length` 崩溃（BUG-TEXAS-HOLE-NULL）。
		pj := PlayerJSON{
			Seat:           i,
			UserID:         p.UserID,
			Hole:           []CardJSON{},
			HasPlayer:      p.UserID != "",
			Folded:         p.Folded,
			AllIn:          p.AllIn,
			Stack:          p.Stack,
			ChipsCommitted: p.TotalCommitted,
			RoundCommitted: p.RoundCommitted,
			HoleCount:      2,
		}

		if p.UserID != "" && !p.Folded {
			if !isSpectator && i == viewer {
				// 自己的底牌完整可见（仅玩家视角）
				pj.Hole = cardsToJSON(p.Hole[:])
			}
			// 摊牌阶段：所有未弃牌玩家的手牌对双方可见；观察者仍看不到。
			if (gs.Street == PhaseShowdown || gs.Street == PhaseOver) && !isSpectator {
				pj.Hole = cardsToJSON(p.Hole[:])
			}
		}
		cs.Players[i] = pj
	}

	// 自己的底牌快捷字段——仅玩家视角填充。
	if !isSpectator && viewer >= 0 && viewer < MaxPlayers {
		self := &gs.Players[viewer]
		if self.UserID != "" {
			cs.MyHole = cardsToJSON(self.Hole[:])
		}
	}

	// 摊牌手牌（仅赢家，供 UI 展示）——只对玩家视角暴露，观察者始终看不到。
	if !isSpectator && gs.Street == PhaseOver && len(gs.Winners) > 0 {
		cs.ShowdownHands = make([][]CardJSON, 0, len(gs.Winners))
		for _, w := range gs.Winners {
			cs.ShowdownHands = append(cs.ShowdownHands, cardsToJSON(gs.Players[w].Hole[:]))
		}
	}

	return cs
}
