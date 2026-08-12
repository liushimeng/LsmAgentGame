// Package models — spectator bet table (§20260812-02 U3).
//
// Per the "观众押注竞猜系统" design, each row records one spectator's bet
// on which seat will be voted out in a given round. Bets are placed during
// PhaseVote (30s window) and settled after EmitVoteResult.
//
// §119 协议层隔离: bet data is only visible to spectators, never to players.
// §133 EconTier: independent constant werewolf.bet_destroy_rate = 50.
package models

import "time"

// TLsmGameSpectatorBet is one spectator's bet on one round.
type TLsmGameSpectatorBet struct {
	ID         string     `gorm:"type:char(36);primaryKey"                                    json:"id"`
	RoomID     string     `gorm:"type:char(36);not null;index:idx_bet_room_round,priority:1"  json:"room_id"`
	UserID     string     `gorm:"type:char(36);not null"                                      json:"user_id"`
	Round      int        `gorm:"not null;index:idx_bet_room_round,priority:2"                json:"round"`
	TargetSeat int        `gorm:"not null"                                                    json:"target_seat"`
	Amount     int        `gorm:"not null"                                                    json:"amount"`
	Settled    bool       `gorm:"not null;default:false"                                      json:"settled"`
	Result     string     `gorm:"type:varchar(16);not null;default:''"                        json:"result"`
	Payout     int        `gorm:"not null;default:0"                                          json:"payout"`
	CreatedAt  time.Time  `gorm:"autoCreateTime"                                              json:"created_at"`
}

// TableName pins the SQL table name.
func (TLsmGameSpectatorBet) TableName() string { return "t_lsm_game_spectator_bet" }
