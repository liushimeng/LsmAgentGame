// Chat message persistence model. Per CLAUDE.md §3, models in this directory
// use the t_lsm_game_*.go prefix; all other Go files use snake_case.
package models

import "time"

// Scope of a chat message. Stored as a short string for forward compatibility
// ("lobby", "room:<id>", future "team:<id>", "dm:<pair>", etc.).
//
// For whisper (private) messages, ToUserID and ToAccount are non-empty;
// the message is only visible to sender, recipient, and admin/superadmin users.
//
// ## Sharding strategy (future-proof)
//
// The current schema keeps all rows in a single `t_lsm_game_chat_message`
// table. When monthly volume grows beyond the comfort zone of a single InnoDB
// table, the operator can shard by month into
//
//     t_lsm_game_chat_message_YYYYMM   (e.g. ..._202607)
//
// At that point `TableName()` should be switched to a function of the row's
// `CreatedAt` (UTC month), and all writes must populate `CreatedAt` BEFORE
// the create call so the ORM can pick the right physical table. Reads that
// span multiple months must issue per-month queries and merge.
//
// For now this comment is just documentation — no code path branches on the
// month because the table is a single one.
type TLsmGameChatMessage struct {
	ID          uint64    `gorm:"primaryKey;autoIncrement"                          json:"id"`
	Scope       string    `gorm:"type:varchar(32);not null;index:idx_scope_room_ts"  json:"scope"`
	RoomID      string    `gorm:"type:varchar(64);index:idx_scope_room_ts"           json:"room_id,omitempty"`
	FromUserID  string    `gorm:"type:char(80);not null;index"                       json:"from_user_id"`
	FromAccount string    `gorm:"type:varchar(64);not null"                           json:"from_account"`
	// FromRole 标识发送者角色。实际契约(与前端
	// ClientWeb/src/types/api.ts 对齐):"player"(房间内人类玩家)/
	// "spectator"(观战者)/ "bot"(AI Agent)/ "judge"(主持人 Agent)/
	// "activity"(活动流)/ ""(未分类)。BUG-R226-P3-01 (2026-08-01):
	// 本注释此前写 "human"/"system",但代码中从不写入这两个值 ——
	// 人类玩家消息落库为 "player"(聊天服务 Send 路径按 scope 分流)。
	// 2026-07-17 R139 修复:法官此前在 FromUserID 用 "judge:<room_id>" 拼接绕过 36 字符
	// 限制(总长度 42 > char(36)),仍会触发 Error 1406;现改用独立 FromRole 字段,
	// FromUserID 仍保留 36 字符 UUID(法官填 "00000000-0000-0000-0000-000000000000"
	// 这种 zero-uuid 占位,前端 GameChatPanel 按 FromRole="judge" 走 ⚖️ 渲染)。
	FromRole    string    `gorm:"type:varchar(16);not null;default:'human';index"     json:"from_role,omitempty"`
	ToUserID    string    `gorm:"type:char(36);index"                                 json:"to_user_id,omitempty"`
	ToAccount   string    `gorm:"type:varchar(64)"                                    json:"to_account,omitempty"`
	Text        string    `gorm:"type:varchar(1024);not null"                         json:"text"`
	// CreatedAt is covered by the composite `idx_scope_room_ts`
	// (scope, room_id, created_at) index for time-scoped queries. A separate
	// time-based cleanup janitor also scans `created_at < ?`; the composite
	// index's trailing created_at column is used by MySQL when the scope and
	// room_id are not in the predicate, so the cleanup query stays fast.
	CreatedAt   time.Time `gorm:"autoCreateTime;index:idx_scope_room_ts"              json:"created_at"`
}

// TableName pins the SQL table name.
func (TLsmGameChatMessage) TableName() string { return "t_lsm_game_chat_message" }