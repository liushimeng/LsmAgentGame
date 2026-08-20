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

// isValidCard 报告 c 是否为有效牌（非零值）。
// 德州扑克牌的合法范围:Rank 2-14(Rank2..RankA),Suit 1-4(SuitSpade..SuitDiamond)。
// 零值 {rank:0,suit:0} 表示「未发牌」——典型场景:玩家迟到加入正在进行的手牌,
// 下个 StartHand 前不会参与本手,AddPlayer 只写了 UserID/Stack 而 Hole 保持零值。
func isValidCard(c Card) bool {
	return c.Rank >= Rank2 && c.Rank <= RankA && c.Suit >= SuitSpade && c.Suit <= SuitDiamond
}

// hasValidHoleCards 报告玩家是否已被发到底牌。
// 用于区分「已发底牌」与「迟到加入未发牌」——后者 Hole 为 [2]Card{},
// 不应渲染为 2 张牌背 / 填充 MyHole / HoleCount=2(R7 P0-1)。
func hasValidHoleCards(hole [2]Card) bool {
	return isValidCard(hole[0])
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

	// 2026-08-19 §德州扑克Agent — Bot 状态透传(前端渲染"🤖 AI"徽章 / 思考中指示器)。
	// BotSeats/BotModels 始终为长度 6 的数组(非 nil),避免 JSON null.length 崩溃(BUG-TEXAS-HOLE-NULL)。
	// BotHeartThought/BotThinking 同理。
	BotSeats        [6]bool   `json:"bot_seats"`
	BotModels       [6]string `json:"bot_models"`
	BotHeartThought [6]string `json:"bot_heart_thought"`
	BotThinking     [6]bool   `json:"bot_thinking"`
}

// BuildClientState 构造座位 viewer 的可见视图（向后兼容版本）。
//
// 内部委托到 BuildClientStateWithRoom 以支持 Bot 字段透传(2026-08-19 §德州扑克Agent)。
// 保留旧签名(seat 数组+viewer+gs)是为了不破坏 engine_test.go 等测试夹具。
// 生产路径统一走 BuildClientStateWithRoom(2026-08-19 起)。
func BuildClientState(roomID string, seats [MaxPlayers]string, viewer int, gs *GameState) *ClientGameState {
	return BuildClientStateWithRoom(roomID, seats, [MaxPlayers]bool{}, [MaxPlayers]string{}, viewer, gs, [MaxPlayers]string{}, [MaxPlayers]bool{})
}

// BuildClientStateWithRoom 构造座位 viewer 的可见视图（含 Bot 字段透传）。
//
// 当 viewer == -1 时表示"观察者"：MySeat 设为 -1，不填充任何玩家的 Hole
//（即使在摊牌阶段也保持底牌隐藏），同时不暴露 MyHole。 公共牌、玩家栈、
// 行动状态、胜负结果等公开信息仍照常填充。
//
// 2026-08-19 §德州扑克Agent — 新增 botSeats/botModels 参数,用于透传 Bot 状态
// 到前端。前端用 BotSeats/BotModels 渲染"🤖 AI"徽章。
//
// botHeartThought/botThinking 由 ws/game_service_texas_bot.go 写入(锁内调用
// SetBotHeartThoughtLocked / SetBotThinkingLocked),此处直接读字段填充
// ClientGameState.BotHeartThought/BotThinking — 不丢帧(已在 r.mu 保护下)。
func BuildClientStateWithRoom(roomID string, seats [MaxPlayers]string, botSeats [MaxPlayers]bool, botModels [MaxPlayers]string, viewer int, gs *GameState, botHeartThought [MaxPlayers]string, botThinking [MaxPlayers]bool) *ClientGameState {
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
			HoleCount:      0,
		}

		// R7 P0-1: 仅当玩家已被发到底牌(非零值)时才渲染 hole/hole_count。
		// 迟到加入的玩家(未参与本手)底牌为零值,不应显示 2 张牌背或填充 MyHole。
		if p.UserID != "" && hasValidHoleCards(p.Hole) {
			pj.HoleCount = 2
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
	// R7 P0-1: 增加 hasValidHoleCards 守卫,迟到加入(未发牌)时 MyHole 保持空切片,
	// 前端不会渲染出 rank:0/suit:0 占位牌。
	if !isSpectator && viewer >= 0 && viewer < MaxPlayers {
		self := &gs.Players[viewer]
		if self.UserID != "" && hasValidHoleCards(self.Hole) {
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

	// 2026-08-19 §德州扑克Agent — 透传 Bot 状态字段。
	// 字段始终初始化(默认零值),JSON 序列化不会出现 null。
	// BotHeartThought / BotThinking 由 ws/game_service_texas_bot.go 写入
	// SetBotHeartThoughtLocked / SetBotThinkingLocked,本函数直接读取后透传。
	for i := 0; i < MaxPlayers; i++ {
		cs.BotSeats[i] = botSeats[i]
		cs.BotModels[i] = botModels[i]
		cs.BotHeartThought[i] = botHeartThought[i]
		cs.BotThinking[i] = botThinking[i]
	}

	return cs
}
