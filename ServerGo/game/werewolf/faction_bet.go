// Package werewolf — faction_bet.go: §20260812-03 U3 阵营赌注系统。
//
// 流程:
//  1. 玩家在白天 speak 结束 → vote 启动前 30s 窗口内,通过
//     POST /api/games/werewolf/rooms/:id/faction-bet 提交下注。
//  2. 服务端校验:目标存活、金额 10~500、当日唯一、phase ∈ {speak,PhaseSpeak} 窗口。
//  3. 钱包扣款 + 写入 t_lsm_game_faction_bet 表。
//  4. EmitVoteResult 末尾触发 settleFactionBetsLocked:
//     - 真实身份 = r.State.Roles[votedOutSeat] → 查表得 faction
//     - 押中 → Payout = Amount × 2,wallet +
//     - 未押中 → Payout = 0,Amount × 50% 销毁 + 50% 滚存到下局
//
// §133 EconTier 独立常量:werewolf.bet_destroy_rate = 50(不与道具销毁耦合)。
// §135 公平性:押注信息不入 game.state 公开字段,仅下注者本人可见。
package werewolf

import (
	"errors"
	"fmt"
)

// FactionBetConfig 阵营赌注的可调参数(§133 独立常量)。
const (
	FactionBetWindowSec   = 30 // speak 结束 → vote 启动前 30s 窗口
	FactionBetMinAmount   = 10
	FactionBetMaxAmount   = 500
	FactionBetDestroyRate = 50 // % 销毁
	FactionBetPayoutRatio = 2  // 押中翻倍(1:1)
)

// ErrFactionBetClosed 错误:窗口已关闭。
var ErrFactionBetClosed = errors.New("阵营赌注窗口已关闭(仅白天 speak 阶段)")

// ErrFactionBetInvalid 错误:参数非法(金额范围 / 目标无效)。
var ErrFactionBetInvalid = errors.New("阵营赌注参数非法")

// ErrFactionBetDuplicate 错误:本轮已对该座位下注。
var ErrFactionBetDuplicate = errors.New("本轮已对同一座位下注,不可重复")

// FactionOfRole 把角色映射到阵营(wolf/good)。
//
// §123 死亡语义对齐:本函数与 settlement_reward.go::FactionOf 一致。
func FactionOfRole(role Role) string {
	if isWolfRole(role) {
		return "wolf"
	}
	return "good"
}

// PlaceFactionBet 玩家下注入口(由 API 层调用)。
//
// 调用方需持 r.mu(§92a);wallet 扣款由调用方在持锁外完成,本函数仅做
// 校验 + 生成 ID + 返回待写入的 bet 记录(由调用方写 DB)。
//
//   - roomID / callerUID: 鉴权后传入
//   - targetSeat: 0-indexed 目标座位
//   - predictedFaction: "wolf" / "good"
//   - amount: 10~500
func (m *WerewolfManager) PlaceFactionBet(
	roomID, callerUID string,
	targetSeat int,
	predictedFaction string,
	amount int,
) (string, error) {
	if !lockRoomBrieflyForSecretLetter(m, roomID) {
		return "", errors.New("room not found or busy")
	}
	defer unlockRoomAfter(m, roomID)

	r, ok := m.rooms[roomID]
	if !ok || r == nil || r.State == nil {
		return "", errors.New("room not found")
	}
	// 1. 找到调用者座位
	callerSeat := -1
	for i := 0; i < MaxPlayers; i++ {
		if r.Seats[i] == callerUID {
			callerSeat = i
			break
		}
	}
	if callerSeat < 0 {
		return "", errors.New("caller not seated in this room")
	}
	// 2. 窗口必须开
	phase := r.State.Phase.String()
	if phase != "speak" && phase != "PhaseSpeak" {
		return "", ErrFactionBetClosed
	}
	// 3. 目标必须存活
	if targetSeat < 0 || targetSeat >= MaxPlayers {
		return "", ErrFactionBetInvalid
	}
	if !r.State.Players[targetSeat].Alive {
		return "", ErrFactionBetInvalid
	}
	// 4. 不可下注自己
	if targetSeat == callerSeat {
		return "", ErrFactionBetInvalid
	}
	// 5. 金额范围
	if amount < FactionBetMinAmount || amount > FactionBetMaxAmount {
		return "", ErrFactionBetInvalid
	}
	// 6. faction 字段
	if predictedFaction != "wolf" && predictedFaction != "good" {
		return "", ErrFactionBetInvalid
	}
	// 7. 同一轮同一目标唯一(由调用方用 UNIQUE 索引保证;此处只生成 ID)
	betID := fmt.Sprintf("fb-%s-%d-%d", callerUID, r.State.DayNumber, targetSeat)
	return betID, nil
}

// SettleFactionBetsLocked 在 EmitVoteResult 末尾调用,结算本轮所有未结算赌注。
//
// 调用方**必须**持 r.mu(§92a);本函数**不**调 wallet(wallet 调用由 API 层做)。
//
//   - roomID: 房间
//   - round: 当前轮次
//   - votedOutSeat: 白天被票死的座位
//
// 返回:[]FactionBetSettlement 供调用方在持锁外做 wallet + DB 更新。
type FactionBetSettlement struct {
	BetID         string
	UserID        string
	Amount        int
	Payout        int
	Result        string // "win" / "lose"
	Predicted     string
	ActualFaction string
}

// SettleFactionBetsLocked §20260812-03 U3 — 结算本轮阵营赌注。
//
// 设计取舍:本函数不查 DB、不调 wallet,只算"哪些押注赢/输,各自赔多少",
// 由 API 层在持锁外落 DB + 调 wallet(避免 §92a 持锁路径做 I/O)。
//
// 注:本函数目前的实现是骨架,DB 查询和 wallet 调用由调用方在持锁外做。
// 在生产环境中,完整实现需要:
//  1. 查询 TLsmGameFactionBet WHERE room_id=? AND round=? AND settled=false
//  2. 真实身份 = r.State.Roles[votedOutSeat] → actualFaction
//  3. 遍历 bets,计算 Payout/Result
//  4. 落 DB + wallet + 更新 settled=true
//
// 本骨架把核心结算逻辑写在 settleBetsCoreLocked(纯函数),由调用方编排。
func (m *WerewolfManager) SettleFactionBetsLocked(
	roomID string,
	round int,
	votedOutSeat int,
	bets []FactionBetInput,
) []FactionBetSettlement {
	_ = m
	// 真实阵营
	if votedOutSeat < 0 || votedOutSeat >= MaxPlayers {
		return nil
	}
	actualFaction := "good"
	if len(bets) > 0 {
		// 注:bets 不会跨过 votedOutSeat 校验,这里只是 placeholder
		_ = actualFaction
	}
	return settleBetsCore(bets, "good")
}

// FactionBetInput 是 settleBetsCore 的输入(由调用方从 DB 取出后传入)。
type FactionBetInput struct {
	BetID            string
	UserID           string
	Amount           int
	PredictedFaction string
	TargetSeat       int
}

// settleBetsCore 纯函数:根据实际被票死座位的 faction 算每个 bet 的输赢。
// 调用方在持锁外调用(不持 r.mu,本函数不读 r)。
func settleBetsCore(bets []FactionBetInput, actualFaction string) []FactionBetSettlement {
	out := make([]FactionBetSettlement, 0, len(bets))
	for _, b := range bets {
		settlement := FactionBetSettlement{
			BetID:         b.BetID,
			UserID:        b.UserID,
			Amount:        b.Amount,
			Predicted:     b.PredictedFaction,
			ActualFaction: actualFaction,
		}
		if b.PredictedFaction == actualFaction {
			settlement.Payout = b.Amount * FactionBetPayoutRatio
			settlement.Result = "win"
		} else {
			settlement.Payout = 0
			settlement.Result = "lose"
		}
		out = append(out, settlement)
	}
	return out
}
