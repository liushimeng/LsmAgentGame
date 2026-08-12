package doudizhu

import (
	"math/rand"

	"LsmWebGame/errcode"
)

// Phase 游戏阶段。
type Phase int

const (
	PhaseBidding Phase = iota // 叫地主
	PhasePlaying              // 出牌
	PhaseOver                 // 结束
)

func (p Phase) String() string {
	switch p {
	case PhaseBidding:
		return "bidding"
	case PhasePlaying:
		return "playing"
	default:
		return "over"
	}
}

// Status 对局结果。
type Status int

const (
	StatusPlaying      Status = iota // 进行中
	StatusLandlordWin                // 地主胜
	StatusFarmerWin                  // 农民胜
)

func (s Status) String() string {
	switch s {
	case StatusLandlordWin:
		return "landlord_win"
	case StatusFarmerWin:
		return "farmer_win"
	default:
		return "playing"
	}
}

const noSeat = -1

// LastPlay 记录上一手有效出牌（用于跟牌比较）。
type LastPlay struct {
	Seat  int    // 出牌座位
	Cards []Card // 出的牌
	combo *Combo // 解析后的牌型（内部用，不导出 JSON）
}

// GameState 是一局斗地主的权威状态。所有规则判断都基于它，且只在服务端持有。
type GameState struct {
	Hands        [3][]Card // 三家手牌
	Bottom       []Card    // 3 张底牌
	Phase        Phase
	Turn         int    // 当前应行动的座位 0/1/2
	Status       Status

	// 叫地主阶段
	FirstBidder  int     // 首叫座位
	Bids         [3]int  // 各座位叫分（-1=未叫，0=不叫，1/2/3=叫分）
	BidsMade     int     // 已表态人数
	CurrentBid   int     // 当前最高叫分
	LandlordSeat int     // 地主座位（确定前为 noSeat）

	// 出牌阶段
	Last         *LastPlay // 上一手有效出牌（nil 表示当前自由出牌）
	PassCount    int       // 自上一手有效出牌以来连续过牌数
	Multiplier   int       // 倍数（基础 = CurrentBid，炸弹/火箭翻倍）
	BombCount    int       // 已出炸弹/火箭次数
	Plays        int       // 有效出牌手数（用于判春天/反春，预留）
}

// NewGame 用给定种子发牌：每家 17 张，3 张底牌，进入叫地主阶段。
// 注入 seed 便于测试可复现；生产环境由 manager 传入时间种子。
func NewGame(seed int64, firstBidder int) *GameState {
	rng := rand.New(rand.NewSource(seed))
	deck := NewDeck()
	Shuffle(deck, rng)

	gs := &GameState{
		Phase:        PhaseBidding,
		Status:       StatusPlaying,
		FirstBidder:  firstBidder % 3,
		Bids:         [3]int{-1, -1, -1},
		CurrentBid:   0,
		LandlordSeat: noSeat,
		Multiplier:   1,
	}
	for seat := 0; seat < 3; seat++ {
		gs.Hands[seat] = make([]Card, 17)
		copy(gs.Hands[seat], deck[seat*17:seat*17+17])
		SortCards(gs.Hands[seat])
	}
	gs.Bottom = make([]Card, 3)
	copy(gs.Bottom, deck[51:54])
	SortCards(gs.Bottom)
	gs.Turn = gs.FirstBidder
	return gs
}

// Bid 处理叫分。score ∈ {0,1,2,3}，0 表示不叫。
// 规则：按座位轮流，每人叫一次；叫 3 直接定地主；一轮结束最高分者为地主。
// 全部不叫则返回 needRedeal=true（由上层重新发牌）。
func (gs *GameState) Bid(seat, score int) (needRedeal bool, e *errcode.Error) {
	if gs.Phase != PhaseBidding {
		return false, errcode.CodeMsg(errcode.ErrValidationFailed, "not in bidding phase")
	}
	if seat != gs.Turn {
		return false, errcode.Code(errcode.ErrNotYourTurn)
	}
	if score < 0 || score > 3 {
		return false, errcode.CodeMsg(errcode.ErrValidationFailed, "bid score must be 0..3")
	}
	if score != 0 && score <= gs.CurrentBid {
		return false, errcode.CodeMsg(errcode.ErrValidationFailed, "bid must be higher than current")
	}

	gs.Bids[seat] = score
	gs.BidsMade++
	if score > gs.CurrentBid {
		gs.CurrentBid = score
		gs.LandlordSeat = seat
	}

	// 叫 3 分直接成为地主。
	if score == 3 {
		gs.assignLandlord()
		return false, nil
	}

	// 一轮（三人）都已表态。
	if gs.BidsMade >= 3 {
		if gs.CurrentBid == 0 {
			// 全部不叫，需重发。
			return true, nil
		}
		gs.assignLandlord()
		return false, nil
	}

	// 轮到下一家。
	gs.Turn = (gs.Turn + 1) % 3
	return false, nil
}

// assignLandlord 把底牌并入地主手牌、设置倍数基数、进入出牌阶段。
func (gs *GameState) assignLandlord() {
	ls := gs.LandlordSeat
	gs.Hands[ls] = append(gs.Hands[ls], gs.Bottom...)
	SortCards(gs.Hands[ls])
	gs.Phase = PhasePlaying
	gs.Turn = ls // 地主首先出牌
	gs.Multiplier = gs.CurrentBid
	gs.Last = nil
	gs.PassCount = 0
}

// IsLandlord 报告座位是否为地主。
func (gs *GameState) IsLandlord(seat int) bool {
	return seat == gs.LandlordSeat
}

// Play 处理出牌。校验轮次、牌型合法、是否在手牌中、能否压过上家。
// 返回是否结束游戏。
func (gs *GameState) Play(seat int, cards []Card) (gameOver bool, e *errcode.Error) {
	if gs.Phase != PhasePlaying {
		return false, errcode.CodeMsg(errcode.ErrValidationFailed, "not in playing phase")
	}
	if seat != gs.Turn {
		return false, errcode.Code(errcode.ErrNotYourTurn)
	}
	if len(cards) == 0 {
		return false, errcode.CodeMsg(errcode.ErrValidationFailed, "no cards to play")
	}
	if !containsAll(gs.Hands[seat], cards) {
		return false, errcode.CodeMsg(errcode.ErrInvalidMove, "cards not in hand")
	}

	combo, ok := ParseCombo(cards)
	if !ok {
		return false, errcode.CodeMsg(errcode.ErrInvalidMove, "invalid card combination")
	}

	// 非自由出牌时必须能压过上家。
	var prev *Combo
	if gs.Last != nil {
		prev = gs.Last.combo
	}
	if !CanBeat(prev, combo) {
		return false, errcode.CodeMsg(errcode.ErrInvalidMove, "cannot beat the last play")
	}

	// 扣牌。
	remaining, _ := removeCards(gs.Hands[seat], cards)
	gs.Hands[seat] = remaining

	// 炸弹/火箭翻倍。
	if combo.IsBomb() {
		gs.BombCount++
		gs.Multiplier *= 2
	}

	played := make([]Card, len(cards))
	copy(played, cards)
	gs.Last = &LastPlay{Seat: seat, Cards: played, combo: combo}
	gs.PassCount = 0
	gs.Plays++

	// 出完即胜。
	if len(gs.Hands[seat]) == 0 {
		gs.Phase = PhaseOver
		if gs.IsLandlord(seat) {
			gs.Status = StatusLandlordWin
		} else {
			gs.Status = StatusFarmerWin
		}
		return true, nil
	}

	gs.Turn = (gs.Turn + 1) % 3
	return false, nil
}

// Pass 处理过牌（要不起）。自由出牌时不可过。
// 两家连续过牌后，上一手出牌者重新获得自由出牌权。
func (gs *GameState) Pass(seat int) *errcode.Error {
	if gs.Phase != PhasePlaying {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "not in playing phase")
	}
	if seat != gs.Turn {
		return errcode.Code(errcode.ErrNotYourTurn)
	}
	if gs.Last == nil {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "must play when free to lead")
	}

	gs.PassCount++
	// 另外两家都过了 → 上家重获自由出牌权。
	if gs.PassCount >= 2 {
		gs.Turn = gs.Last.Seat
		gs.Last = nil
		gs.PassCount = 0
		return nil
	}
	gs.Turn = (gs.Turn + 1) % 3
	return nil
}

// Winner 返回胜方描述（"landlord" / "farmer" / ""）。
func (gs *GameState) Winner() string {
	switch gs.Status {
	case StatusLandlordWin:
		return "landlord"
	case StatusFarmerWin:
		return "farmer"
	default:
		return ""
	}
}

// Score 返回本局基础分 × 倍数（不含座位归属，由上层按地主/农民分配）。
func (gs *GameState) Score() int {
	base := gs.CurrentBid
	if base == 0 {
		base = 1
	}
	return base * gs.Multiplier
}
