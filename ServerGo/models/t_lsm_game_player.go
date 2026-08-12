package models

import "time"

// PlayerRole discriminates a row in t_lsm_game_player.
//
//   - "player"    : a seat-taking participant; counts against room capacity and
//                   contributes to CurrentCount on the parent t_lsm_game_room row.
//   - "spectator" : an observer; does NOT count against capacity and does NOT
//                   reserve a seat. Multiple spectators per room are allowed.
//   - "agent"      : an AI-driven bot seat. Behaves like a player for capacity
//                   purposes and is driven in-process by the werewolf Agent
//                   runner rather than a websocket client. The backing UserID
//                     references a bot user row (account prefixed bot_).
const (
	PlayerRolePlayer    = "player"
	PlayerRoleSpectator = "spectator"
	PlayerRoleAgent     = "agent"
)

// TLsmGamePlayer is a (room, user) join row for per-room state.
//
// A given (room_id, user_id) pair may carry either a player row OR a spectator
// row, never both — enforced by RoomService. AutoMigrate adds the Role column
// and the (room_id, user_id) unique index; existing rows default to "player".
type TLsmGamePlayer struct {
	ID           string    `gorm:"type:char(36);primaryKey"      json:"id"`
	RoomID       string    `gorm:"type:char(36);not null"        json:"room_id"`
	UserID       string    `gorm:"type:char(36);not null"        json:"user_id"`
	Role         string    `gorm:"type:varchar(16);not null;default:'player'" json:"role"`
	Seat         int       `gorm:"not null;default:0"            json:"seat"`
	Score        int       `gorm:"not null;default:0"            json:"score"`
	LastInputSeq int64     `gorm:"not null;default:0"            json:"last_input_seq"`
	// ModelKey is the LLM model id (e.g. "MeiTuan-model") this agent seat uses.
	// Empty for human players and spectators. Must reference a configured provider
	// in cfg.LLM.Providers (validated at room creation time).
	ModelKey     string    `gorm:"type:varchar(64);not null;default:''" json:"model_key"`
	BotMemoryHash string   `gorm:"type:char(16);not null;default:''" json:"bot_memory_hash,omitempty"`
	JoinedAt     time.Time `gorm:"autoCreateTime"                json:"joined_at"`
}

// TableName pins the SQL table name.
func (TLsmGamePlayer) TableName() string { return "t_lsm_game_player" }

// IsSpectator reports whether this row is an observer (not a seat-taking player).
func (p *TLsmGamePlayer) IsSpectator() bool { return p.Role == PlayerRoleSpectator }

// IsAgent reports whether this row is an AI-driven bot seat (behaves like a
// player for capacity purposes but is driven in-process rather than by a
// websocket client).
func (p *TLsmGamePlayer) IsAgent() bool { return p.Role == PlayerRoleAgent }
