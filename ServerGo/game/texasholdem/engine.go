package texasholdem

import (
	"fmt"
	"math/rand"

	"LsmWebGame/errcode"
)

// Phase 游戏阶段。
type Phase int

const (
	PhaseWaiting  Phase = iota // 不足 2 人，等待开始
	PhasePreflop               // 翻牌前（底牌已发，下注中）
	PhaseFlop                  // 翻牌（3 张公共牌亮出，下注中）
	PhaseTurn                  // 转牌（第 4 张公共牌亮出，下注中）
	PhaseRiver                 // 河牌（第 5 张公共牌亮出，下注中）
	PhaseShowdown              // 摊牌（亮底牌比大小）
	PhaseOver                  // 手牌结束（进入下一手或房间关闭）
)

func (p Phase) String() string {
	switch p {
	case PhaseWaiting:
		return "waiting"
	case PhasePreflop:
		return "preflop"
	case PhaseFlop:
		return "flop"
	case PhaseTurn:
		return "turn"
	case PhaseRiver:
		return "river"
	case PhaseShowdown:
		return "showdown"
	default:
		return "over"
	}
}

// Status 对局结果。
type Status int

const (
	StatusPlaying   Status = iota // 进行中
	StatusShowdown                // 摊牌
	StatusOver                    // 结束
)

func (s Status) String() string {
	switch s {
	case StatusShowdown:
		return "showdown"
	case StatusOver:
		return "over"
	default:
		return "playing"
	}
}

// ActionType 玩家动作类型。
type ActionType int

const (
	ActFold  ActionType = iota // 弃牌
	ActCheck                   // 过牌
	ActCall                    // 跟注
	ActBet                     // 下注（翻前/翻后首次主动下注）
	ActRaise                   // 加注
	ActAllIn                   // 全押
)

func (a ActionType) String() string {
	switch a {
	case ActFold:
		return "fold"
	case ActCheck:
		return "check"
	case ActCall:
		return "call"
	case ActBet:
		return "bet"
	case ActRaise:
		return "raise"
	case ActAllIn:
		return "allin"
	default:
		return "unknown"
	}
}

// Action 玩家动作。
type Action struct {
	Type   ActionType `json:"type"`
	Amount int        `json:"amount,omitempty"` // 用于 bet/raise 的目标金额
}

// MaxPlayers 德州扑克最大座位数（6 人）。
const MaxPlayers = 6
const noSeat = -1

// Player 单个玩家的对局状态。
type Player struct {
	UserID    string // 用户 ID
	Hole      [2]Card
	Folded    bool
	AllIn     bool
	Stack     int // 剩余筹码（手牌开始后不再变化，除非下注）
	Seat      int // 座位号 0..5

	// 本手本轮累计下注（用于计算 call amount）
	RoundCommitted int
	// 本手总累计下注（用于最终分池）
	TotalCommitted int
	// 本轮是否已行动过
	HasActed bool
}

// GameState 是一局德州扑克的权威状态。
type GameState struct {
	Deck      []Card // 剩余未发牌（倒序消耗）
	Community [5]Card
	CommunityShown int // 已亮出公共牌数 0/3/4/5

	Players [6]Player
	NumSeat int // 已入座玩家数
	Button  int // 庄家座位（-1 = 未定）

	Street         Phase
	Turn           int // 当前应行动座位
	CurrentBet     int // 本轮最高下注额（绝对值）
	MinRaise       int // 最小加注增量（上一次加注量）
	LastAggressor  int // 最后一个加注/下注者
	Pot            int // 主池总额
	RoundCommitted [6]int // 本轮各座位累计下注（冗余，同步于 Player）

	BigBlind    int
	HandNumber  int

	ShowdownHands [6]HandRank
	Winners       []int
	Status        Status
}

// NewGame 创建新的对局（尚未发牌，等待足够玩家后调 StartHand）。
func NewGame(seed int64, bigBlind int) *GameState {
	rng := rand.New(rand.NewSource(seed))
	deck := NewDeck()
	Shuffle(deck, rng)

	return &GameState{
		Deck:      deck,
		Button:    noSeat,
		Street:    PhaseWaiting,
		Turn:      noSeat,
		BigBlind:  bigBlind,
		Status:    StatusPlaying,
	}
}

// AddPlayer 在空座位上添加玩家。返回分配的座位号或错误。
func (gs *GameState) AddPlayer(userID string, startStack int) (int, *errcode.Error) {
	if gs.NumSeat >= MaxPlayers {
		return 0, errcode.Code(errcode.ErrRoomFull)
	}
	// 检查是否已在座
	for i := 0; i < MaxPlayers; i++ {
		if gs.Players[i].UserID == userID {
			return i, errcode.CodeMsg(errcode.ErrValidationFailed, "already seated")
		}
	}
	seat := -1
	for i := 0; i < MaxPlayers; i++ {
		if gs.Players[i].UserID == "" {
			seat = i
			break
		}
	}
	gs.Players[seat] = Player{
		UserID: userID,
		Stack:  startStack,
		Seat:   seat,
	}
	gs.NumSeat++
	return seat, nil
}

// RemovePlayer 移除玩家（弃牌并标记为空位）。
func (gs *GameState) RemovePlayer(userID string) {
	for i := 0; i < MaxPlayers; i++ {
		if gs.Players[i].UserID == userID {
			gs.Players[i].UserID = ""
			gs.Players[i].Folded = true
			gs.NumSeat--
			break
		}
	}
}

// GetSeat 返回 userID 所在座位，未入座返回 (-1, false)。
func (gs *GameState) GetSeat(userID string) (int, bool) {
	for i := 0; i < MaxPlayers; i++ {
		if gs.Players[i].UserID == userID {
			return i, true
		}
	}
	return -1, false
}

// activePlayers 返回未弃牌且还有筹码的玩家数（或全押但未弃牌的）。
func (gs *GameState) activePlayers() int {
	n := 0
	for i := 0; i < MaxPlayers; i++ {
		if gs.Players[i].UserID != "" && !gs.Players[i].Folded {
			n++
		}
	}
	return n
}

// nonFoldedNonAllIn 返回既未弃牌也未全押的玩家数。
func (gs *GameState) nonFoldedNonAllIn() int {
	n := 0
	for i := 0; i < MaxPlayers; i++ {
		p := &gs.Players[i]
		if p.UserID != "" && !p.Folded && !p.AllIn {
			n++
		}
	}
	return n
}

// StartHand 发牌、设置盲注、进入翻前阶段。
// 自动旋转庄家（顺时针）。
func (gs *GameState) StartHand() *errcode.Error {
	if gs.NumSeat < 2 {
		return errcode.CodeMsg(errcode.ErrGameNotStarted, "need at least 2 players")
	}

	gs.HandNumber++
	// 庄家顺时针旋转
	gs.Button = gs.nextActiveSeat(gs.Button)

	// 重置公共牌与计数 —— 必须清零，否则下一手 advanceToNextStreet 会在
	// CommunityShown=5 时 dealCommunity 写入越界（BUG-TEXAS-DEALCOMMUNITY-OOB）。
	// 历史 Hand 结束后 Community[] 残留 + CommunityShown=5，下手 preflop
	// 直接 PANIC（runtime error: index out of range [5] with length 5）。
	gs.Community = [5]Card{}
	gs.CommunityShown = 0

	// 重置玩家状态
	for i := 0; i < MaxPlayers; i++ {
		p := &gs.Players[i]
		if p.UserID == "" {
			continue
		}
		p.Hole = [2]Card{}
		p.Folded = false
		p.AllIn = false
		p.RoundCommitted = 0
		p.TotalCommitted = 0
		p.HasActed = false
	}

	// 设置盲注座位
	numActive := gs.activePlayerSeats()
	if len(numActive) == 2 {
		// 单挑：庄家 = SB，对手 = BB
		gs.Players[gs.Button].Seat = gs.Button // no-op for clarity
		// SB = button, BB = 对手
		sb := gs.Button
		bb := gs.nextActiveSeat(sb)
		gs.postBlind(sb, gs.BigBlind/2)
		gs.postBlind(bb, gs.BigBlind)
		gs.Turn = sb // 单挑时 SB（庄家）先动
	} else {
		sb := gs.nextActiveSeat(gs.Button)
		bb := gs.nextActiveSeat(sb)
		gs.postBlind(sb, gs.BigBlind/2)
		gs.postBlind(bb, gs.BigBlind)
		gs.Turn = gs.nextActiveSeat(bb) // UTG 先动
	}

	// 发底牌
	gs.dealHoleCards()

	gs.CurrentBet = gs.BigBlind
	gs.MinRaise = gs.BigBlind
	gs.Street = PhasePreflop
	gs.Status = StatusPlaying
	gs.Winners = nil

	return nil
}

// dealHoleCards 给每个活跃玩家发 2 张底牌。
func (gs *GameState) dealHoleCards() {
	for i := 0; i < MaxPlayers; i++ {
		p := &gs.Players[i]
		if p.UserID == "" || p.Stack == 0 {
			continue
		}
		p.Hole[0] = gs.drawCard()
		p.Hole[1] = gs.drawCard()
	}
}

// drawCard 从牌堆抽一张牌。
func (gs *GameState) drawCard() Card {
	n := len(gs.Deck)
	c := gs.Deck[n-1]
	gs.Deck = gs.Deck[:n-1]
	return c
}

// postBlind 扣除盲注（可能全押）。
func (gs *GameState) postBlind(seat, amount int) {
	p := &gs.Players[seat]
	if amount > p.Stack {
		amount = p.Stack
		p.AllIn = true
	}
	p.Stack -= amount
	p.RoundCommitted = amount
	p.TotalCommitted = amount
	gs.Pot += amount
	gs.RoundCommitted[seat] = amount
}

// activePlayerSeats 返回所有非空位、未弃牌的座位列表（顺时针顺序）。
func (gs *GameState) activePlayerSeats() []int {
	var seats []int
	for i := 0; i < MaxPlayers; i++ {
		if gs.Players[i].UserID != "" && !gs.Players[i].Folded {
			seats = append(seats, i)
		}
	}
	return seats
}

// nextActiveSeat 返回从 start 顺时针的下一个非空未弃牌座位。
func (gs *GameState) nextActiveSeat(start int) int {
	for step := 1; step <= MaxPlayers; step++ {
		next := (start + step) % MaxPlayers
		if gs.Players[next].UserID != "" && !gs.Players[next].Folded {
			return next
		}
	}
	return start
}

// ApplyAction 处理玩家动作。返回 (手牌是否结束, 错误)。
func (gs *GameState) ApplyAction(seat int, a Action) (bool, *errcode.Error) {
	if gs.Status != StatusPlaying {
		return false, errcode.CodeMsg(errcode.ErrGameAlreadyOver, "hand not in progress")
	}
	if seat != gs.Turn {
		return false, errcode.Code(errcode.ErrNotYourTurn)
	}

	p := &gs.Players[seat]
	if p.UserID == "" || p.Folded {
		return false, errcode.CodeMsg(errcode.ErrInvalidMove, "player not active")
	}

	switch a.Type {
	case ActFold:
		p.Folded = true
		p.HasActed = true
	case ActCheck:
		if gs.CurrentBet > p.RoundCommitted {
			return false, errcode.CodeMsg(errcode.ErrInvalidMove, "cannot check when there is a bet to call")
		}
		p.HasActed = true
	case ActCall:
		callAmt := gs.CurrentBet - p.RoundCommitted
		if callAmt <= 0 {
			// 已跟注，check 代替
			p.HasActed = true
			break
		}
		gs.commitChips(seat, callAmt)
		p.HasActed = true
	case ActBet:
		if gs.CurrentBet > 0 {
			return false, errcode.CodeMsg(errcode.ErrInvalidMove, "cannot bet when there is a current bet; use raise")
		}
		if a.Amount < gs.BigBlind {
			return false, errcode.CodeMsg(errcode.ErrInvalidMove, fmt.Sprintf("minimum bet is %d", gs.BigBlind))
		}
		gs.commitChips(seat, a.Amount)
		gs.CurrentBet = a.Amount
		gs.MinRaise = a.Amount
		gs.LastAggressor = seat
		gs.resetHasActed(seat) // 其他玩家需重新行动
		p.HasActed = true
	case ActRaise:
		newTotal := a.Amount // raise to this absolute amount
		if newTotal < gs.CurrentBet+gs.MinRaise {
			return false, errcode.CodeMsg(errcode.ErrInvalidMove,
				fmt.Sprintf("minimum raise to %d", gs.CurrentBet+gs.MinRaise))
		}
		inc := newTotal - p.RoundCommitted
		gs.commitChips(seat, inc)
		gs.MinRaise = newTotal - gs.CurrentBet
		gs.CurrentBet = newTotal
		gs.LastAggressor = seat
		gs.resetHasActed(seat)
		p.HasActed = true
	case ActAllIn:
		allInAmt := p.Stack
		gs.commitChips(seat, allInAmt)
		newBet := p.RoundCommitted
		if newBet > gs.CurrentBet {
			// 如果全押量构成完整加注
			raiseInc := newBet - gs.CurrentBet
			if raiseInc >= gs.MinRaise {
				gs.MinRaise = raiseInc
			}
			gs.CurrentBet = newBet
			gs.LastAggressor = seat
			gs.resetHasActed(seat)
		}
		p.HasActed = true
	default:
		return false, errcode.CodeMsg(errcode.ErrInvalidMove, "unknown action type")
	}

	// 检查是否只剩 1 人（其余全弃牌）
	if gs.activePlayers() <= 1 {
		gs.endHandFold()
		return true, nil
	}

	// 检查本轮是否结束
	if gs.isBettingRoundComplete() {
		return gs.advanceToNextStreet()
	}

	// 移到下一位
	gs.Turn = gs.nextActiveSeat(seat)
	return false, nil
}

// commitChips 扣除筹码并记录到 pot。
func (gs *GameState) commitChips(seat, amount int) {
	p := &gs.Players[seat]
	if amount >= p.Stack {
		amount = p.Stack
		p.AllIn = true
	}
	p.Stack -= amount
	p.RoundCommitted += amount
	p.TotalCommitted += amount
	gs.Pot += amount
	gs.RoundCommitted[seat] += amount
}

// resetHasActed 除指定座位外，清除所有活跃玩家的已行动标记。
func (gs *GameState) resetHasActed(except int) {
	for i := 0; i < MaxPlayers; i++ {
		if i != except && gs.Players[i].UserID != "" && !gs.Players[i].Folded && !gs.Players[i].AllIn {
			gs.Players[i].HasActed = false
		}
	}
}

// isBettingRoundComplete 判断当前下注轮是否结束。
// 条件：所有未弃牌且未全押的玩家都已行动过，且下注额一致。
func (gs *GameState) isBettingRoundComplete() bool {
	if gs.nonFoldedNonAllIn() == 0 {
		return true // 全部弃牌或全押，无下注可言
	}
	for i := 0; i < MaxPlayers; i++ {
		p := &gs.Players[i]
		if p.UserID == "" || p.Folded || p.AllIn {
			continue
		}
		if !p.HasActed {
			return false
		}
		if p.RoundCommitted != gs.CurrentBet {
			return false
		}
	}
	return true
}

// advanceToNextStreet 进入下一个阶段。
func (gs *GameState) advanceToNextStreet() (bool, *errcode.Error) {
	// 重置本轮下注记录
	for i := 0; i < MaxPlayers; i++ {
		gs.Players[i].RoundCommitted = 0
		gs.Players[i].HasActed = false
		gs.RoundCommitted[i] = 0
	}
	gs.CurrentBet = 0
	gs.MinRaise = gs.BigBlind

	// 如果全部全押，可自动展开后续阶段
	switch gs.Street {
	case PhasePreflop:
		gs.Street = PhaseFlop
		gs.dealCommunity(3)
	case PhaseFlop:
		gs.Street = PhaseTurn
		gs.dealCommunity(1)
	case PhaseTurn:
		gs.Street = PhaseRiver
		gs.dealCommunity(1)
	case PhaseRiver:
		// 摊牌
		return gs.showdown()
	default:
		return false, errcode.CodeMsg(errcode.ErrValidationFailed, "cannot advance from this street")
	}

	// 烧牌
	gs.drawCard()

	// 翻后先行动者 = 庄家之后第一个活跃玩家
	gs.Turn = gs.nextActiveSeat(gs.Button)

	// 如果只剩 1 人或全部全押，继续推进（但要等河牌后摊牌）
	if gs.activePlayers() <= 1 || gs.nonFoldedNonAllIn() == 0 {
		// 自动推进（带摊牌检查）
		if gs.Street == PhaseRiver {
			return gs.showdown()
		}
		return gs.advanceToNextStreet()
	}

	return false, nil
}

// dealCommunity 亮出 n 张公共牌。
//
// 防御性边界：上限 5 张（数组固定 [5]Card）。即使 StartHand 漏清零或
// 递归 advance 走到不应当走的 Phase，也不会 PANIC，最多 no-op。
// 历史事故：BUG-TEXAS-DEALCOMMUNITY-OOB —— Hand #1 结束时 CommunityShown=5，
// Hand #2 StartHand 未清零，preflop 收尾调用 dealCommunity(3) →
// Community[5] 越界，整服务进程退出。
func (gs *GameState) dealCommunity(n int) {
	for i := 0; i < n; i++ {
		if gs.CommunityShown >= len(gs.Community) {
			// 防御性 no-op：避免 panic 拖垮进程。正常路径不可能触发。
			break
		}
		gs.Community[gs.CommunityShown] = gs.drawCard()
		gs.CommunityShown++
	}
}

// endHandFold 当只剩 1 人时，该玩家赢得底池。
func (gs *GameState) endHandFold() {
	for i := 0; i < MaxPlayers; i++ {
		if gs.Players[i].UserID != "" && !gs.Players[i].Folded {
			gs.Winners = []int{i}
			gs.Players[i].Stack += gs.Pot
			break
		}
	}
	gs.Pot = 0
	gs.Street = PhaseOver
	gs.Status = StatusOver
}

// showdown 摊牌比大小。
func (gs *GameState) showdown() (bool, *errcode.Error) {
	gs.Street = PhaseShowdown
	gs.Status = StatusShowdown

	// 评估所有未弃牌玩家的最佳 5 张牌
	bestRank := HandRank{Category: -1}
	var winners []int

	for i := 0; i < MaxPlayers; i++ {
		p := &gs.Players[i]
		if p.UserID == "" || p.Folded {
			continue
		}
		rank := EvaluateBest5(p.Hole, gs.Community)
		gs.ShowdownHands[i] = rank
		cmp := rank.Compare(bestRank)
		if cmp > 0 {
			bestRank = rank
			winners = []int{i}
		} else if cmp == 0 {
			winners = append(winners, i)
		}
	}

	gs.Winners = winners

	// 分配底池（v1：平分，奇数筹码给离庄家最近的赢家）
	share := gs.Pot / len(winners)
	remainder := gs.Pot % len(winners)
	for _, w := range winners {
		gs.Players[w].Stack += share
	}
	// 奇数筹码给离 Button 最近的赢家
	if remainder > 0 {
		bestOffset := MaxPlayers + 1
		bestWinner := winners[0]
		for _, w := range winners {
			offset := (w - gs.Button + MaxPlayers) % MaxPlayers
			if offset < bestOffset {
				bestOffset = offset
				bestWinner = w
			}
		}
		gs.Players[bestWinner].Stack += remainder
	}

	gs.Pot = 0
	gs.Street = PhaseOver
	gs.Status = StatusOver

	return true, nil
}

// CanStartHand 报告是否满足开新一手的条件。
func (gs *GameState) CanStartHand() bool {
	// 至少 2 个有筹码的玩家
	count := 0
	for i := 0; i < MaxPlayers; i++ {
		if gs.Players[i].UserID != "" && gs.Players[i].Stack > 0 {
			count++
		}
	}
	return count >= 2
}
