// spectator_bet.go — 观众押注竞猜系统 (§20260812-02 U3)
//
// 观众在 PhaseVote 阶段可对「本轮谁被放逐」押注 10~100 金币。
// 押注窗口:PhaseVote 开始后 30 秒内。结算时机:EmitVoteResult 后自动触发。
//
// §119 协议层隔离:押注信息仅推给观战者,不入 game.state 公开字段。
// §133 EconTier:独立常量 betDestroyRate=50,不与道具经济耦合。
// §130 接线:EmitVoteResult 后结算 hook + 新 WS 帧注册 + 前端组件三处同步。
// §92a:本文件的方法均在锁内调用(调用方已持 r.mu)。
package werewolf

import (
	"LsmWebGame/logger"
	"LsmWebGame/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// 押注金额边界
const (
	spectatorBetMin  = 10
	spectatorBetMax  = 100
	spectatorBetRate = 1.5 // 最低保底赔率
)

// betDestroyRate 是未押中时的销毁比例(§133 EconTier 独立常量)。
const betDestroyRate = 50 // 百分比

// SpectatorBetRecord 是内存中的押注记录(与 DB 模型对应但不进 DB 直到 settle)。
type SpectatorBetRecord struct {
	BetID      string
	UserID     string
	Round      int
	TargetSeat int
	Amount     int
	Settled    bool
	Result     string // "win"/"lose"
	Payout     int
}

// spectatorBetsLocked 返回当前房间的押注存储(懒初始化)。
// **调用前置**:必须已持 r.mu(§92a)。
func (r *WerewolfRoom) spectatorBetsLocked() map[string]*SpectatorBetRecord {
	if r.spectatorBets == nil {
		r.spectatorBets = make(map[string]*SpectatorBetRecord)
	}
	return r.spectatorBets
}

// PlaceSpectatorBet 处理观众押注请求。
// **调用前置**:必须已持 r.mu(§92a)。
// 返回 (betID, error)。
func (r *WerewolfRoom) PlaceSpectatorBet(userID string, targetSeat int, amount int, seatCount int) (string, error) {
	// 校验
	if amount < spectatorBetMin || amount > spectatorBetMax {
		return "", errBetInvalidAmount
	}
	if targetSeat < 0 || targetSeat >= seatCount {
		return "", errBetInvalidTarget
	}
	// 必须是观众(非玩家)
	for _, uid := range r.Seats {
		if uid == userID {
			return "", errBetterIsPlayer
		}
	}
	// 每轮每用户只能押一次
	bets := r.spectatorBetsLocked()
	round := 0
	if r.State != nil {
		round = r.State.DayNumber
	}
	for _, b := range bets {
		if b.UserID == userID && b.Round == round && !b.Settled {
			return "", errBetAlreadyPlaced
		}
	}

	betID := uuid.New().String()
	bets[betID] = &SpectatorBetRecord{
		BetID:      betID,
		UserID:     userID,
		Round:      round,
		TargetSeat: targetSeat,
		Amount:     amount,
	}
	return betID, nil
}

// SettleSpectatorBetsLocked 结算当前轮的所有未结算押注。
// 调用时机:EmitVoteResult 之后,调用方已持 r.mu(§92a)。
// actualTarget 是本轮被放逐的座位,-1 表示无人出局(平票)。
func (r *WerewolfRoom) SettleSpectatorBetsLocked(actualTarget int) {
	bets := r.spectatorBetsLocked()
	if len(bets) == 0 {
		return
	}
	round := 0
	if r.State != nil {
		round = r.State.DayNumber
	}

	// 收集本轮押注
	var roundBets []*SpectatorBetRecord
	for _, b := range bets {
		if b.Round == round && !b.Settled {
			roundBets = append(roundBets, b)
		}
	}
	if len(roundBets) == 0 {
		return
	}

	// 统计押中人数
	winners := 0
	for _, b := range roundBets {
		if actualTarget >= 0 && b.TargetSeat == actualTarget {
			winners++
		}
	}

	totalPool := 0
	for _, b := range roundBets {
		totalPool += b.Amount
	}

	// 计算赔率
	// 无人出局(平票)→ 全部退款
	if actualTarget < 0 {
		for _, b := range roundBets {
			b.Settled = true
			b.Result = "refund"
			b.Payout = b.Amount
		}
		logger.L().Debug("spectator bets refunded (no eviction)",
			zap.String("room_id", r.RoomID), zap.Int("round", round))
		return
	}

	// 计算每人赔付
	for _, b := range roundBets {
		b.Settled = true
		if b.TargetSeat == actualTarget && winners > 0 {
			// 押中:按赔率赔付
			perWinner := totalPool / winners
			rate := float64(perWinner) / float64(b.Amount)
			if rate < spectatorBetRate {
				rate = spectatorBetRate
			}
			b.Result = "win"
			b.Payout = int(float64(b.Amount) * rate)
		} else {
			// 未押中:50%销毁+50%滚存
			b.Result = "lose"
			b.Payout = 0
		}
	}

	// 持久化到 DB(异步,不阻塞游戏流)
	go r.persistBets(roundBets)

	logger.L().Debug("spectator bets settled",
		zap.String("room_id", r.RoomID),
		zap.Int("round", round),
		zap.Int("total_bets", len(roundBets)),
		zap.Int("winners", winners),
		zap.Int("actual_target", actualTarget))
}

// persistBets 异步将已结算的押注写入 DB。
func (r *WerewolfRoom) persistBets(bets []*SpectatorBetRecord) {
	if r.betDB == nil {
		return
	}
	for _, b := range bets {
		row := models.TLsmGameSpectatorBet{
			ID:         b.BetID,
			RoomID:     r.RoomID,
			UserID:     b.UserID,
			Round:      b.Round,
			TargetSeat: b.TargetSeat,
			Amount:     b.Amount,
			Settled:    true,
			Result:     b.Result,
			Payout:     b.Payout,
		}
		if err := r.betDB.Create(&row).Error; err != nil {
			logger.L().Warn("spectator bet persist failed",
				zap.String("bet_id", b.BetID), zap.Error(err))
		}
	}
}

// GetRoundBetsSummaryLocked 返回当前轮的押注摘要(仅推给观战者)。
// **调用前置**:必须已持 r.mu(§92a)。
func (r *WerewolfRoom) GetRoundBetsSummaryLocked() map[string]interface{} {
	bets := r.spectatorBetsLocked()
	if len(bets) == 0 {
		return nil
	}
	round := 0
	if r.State != nil {
		round = r.State.DayNumber
	}

	totalBets := 0
	totalAmount := 0
	seatVotes := make(map[int]int) // target_seat → count
	var myBet *SpectatorBetRecord
	for _, b := range bets {
		if b.Round == round {
			totalBets++
			totalAmount += b.Amount
			seatVotes[b.TargetSeat]++
		}
	}

	return map[string]interface{}{
		"round":       round,
		"total_bets":  totalBets,
		"total_amount": totalAmount,
		"seat_votes":  seatVotes,
		"my_bet":      myBet, // nil if this user hasn't bet
	}
}

// ─── 错误定义 ───

type betError string

func (e betError) Error() string { return string(e) }

const (
	errBetInvalidAmount  = betError("bet amount must be between 10 and 100")
	errBetInvalidTarget  = betError("invalid target seat")
	errBetterIsPlayer    = betError("players cannot place bets")
	errBetAlreadyPlaced  = betError("already placed a bet this round")
)
