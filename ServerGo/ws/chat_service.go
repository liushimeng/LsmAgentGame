// Chat service: persists chat messages to MySQL via GORM and fans them out
// through the WebSocket hub. The on-wire protocol is a JSON envelope so it
// works with the existing ws.Hub without bringing in proto codegen.
//
// Frame types (all JSON envelopes):
//
//   client → server
//     chat.subscribe   { scope: "lobby"|"room", room_id?: string }
//     chat.unsubscribe { scope: "lobby"|"room", room_id?: string }
//     chat.send        { scope: "lobby"|"room", room_id?: string, text: string }
//     chat.history     { scope: "lobby"|"room", room_id?: string, limit?: int, before_id?: number }
//
//   server → client
//     chat.message     { id, scope, room_id?, from_user_id, from_account, text, ts }
//     chat.history     { scope, room_id?, messages: [...], has_more: boolean, next_cursor: number|null }
//     chat.error       { code, message }
//     chat.subscribed  { scope, room_id? }
//     chat.unsubscribed{ scope, room_id? }
//
// chat.history protocol notes (kept here for single-source lookup):
//   - `before_id` (optional): return rows with `id < before_id`. Omit to read the
//     most recent messages (the default).
//   - `has_more` (response): true when the page returned `limit` rows, hinting
//     that older messages exist.
//   - `next_cursor` (response): the id of the last message in the page, to be
//     passed back as `before_id` to fetch the previous page. null when the
//     page is empty.
//   Backwards compatibility: a client that omits `before_id` gets the original
//     semantics, just with two extra fields in the envelope (`has_more`,
//     `next_cursor`).
package ws

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"LsmAgentGame/agent/wwplayer"
	"LsmAgentGame/agent/core"
	"LsmAgentGame/llm"

	"LsmAgentGame/logger"
	"LsmAgentGame/models"

	"go.uber.org/zap"
	"gorm.io/gorm"

	// §20260810-03 F1:狼人杀 whisper 阵营守卫需要查询 game_kind 与阵营。
	// chat_service.go 不能 import werewolf 包(循环依赖),由注入的
	// roomSvc + factionLookup 间接完成。
	"LsmAgentGame/service"
)

const (
	maxChatTextLen  = 1024
	defaultHistoryN = 50
	maxHistoryN     = 200
)

// truncateChatText clamps text to maxChatTextLen runes (the varchar width of
// t_lsm_game_chat_message.text). The human chat.send/chat.whisper paths reject
// oversize input with an error; the bot paths (LLM-generated) silently truncate
// instead, so a transformed/mystery-masked utterance that happens to exceed the
// column width is persisted + broadcast rather than dropped on a DB 1406.
func truncateChatText(text string) string {
	if utf8.RuneCountInString(text) <= maxChatTextLen {
		return text
	}
	return string([]rune(text)[:maxChatTextLen])
}

// isEmojiOnlyMessage returns true when `text`, after stripping whitespace,
// contains at most 5 runes and every rune is a Unicode emoji (symbol/pictograph
// or regional-indicator) and ≤ 2 bytes per rune. §20260810-03 F2 — used by
// Whisper to allow players to send pure-emoji "soft" whispers (e.g. 😂 😂 😂)
// without tripping the general length / content checks. The function is
// intentionally lenient — it does NOT validate emoji well-formedness (ZWJ
// sequences etc.); the goal is "no ASCII, no CJK, no Latin" gating, not a
// full Unicode emoji classifier.
func isEmojiOnlyMessage(text string) bool {
	const maxEmojiRunes = 5
	count := 0
	for _, r := range text {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			continue
		}
		count++
		if count > maxEmojiRunes {
			return false
		}
		if utf8.RuneLen(r) > 2 {
			return false
		}
		// reject ASCII printable (letters/digits/punct), CJK, Hangul, etc.
		// accept emoji-range symbols & pictographs.
		if r < 0x200 {
			return false
		}
		// Reject common non-emoji symbol ranges (currency, math, arrows,
		// box-drawing). Only emoji-block ranges pass through:
		//   0x1F300..0x1FAFF (Misc Symbols & Pictographs .. Symbols & Pictographs Ext-A)
		//   0x2600..0x27BF     (Misc Symbols .. Dingbats)
		//   0x1F000..0x1F1FF   (Mahjong/Playing-Card/Regional-Indicator)
		//   0x1F900..0x1F9FF   (Supplemental Symbols & Pictographs)
		//   0x2300..0x23FF     (Misc Technical, ⏰⏳✅ etc.)
		emoji := (r >= 0x1F000 && r <= 0x1FAFF) ||
			(r >= 0x2600 && r <= 0x27BF) ||
			(r >= 0x2300 && r <= 0x23FF)
		if !emoji {
			return false
		}
	}
	return count > 0
}

// ChatMessage is the on-wire payload for a single chat message.
//
// FromRole is "" for legacy clients and is added when the server can determine
// the sender's relationship to the room ("player" | "spectator").
//
// FromAgentName is populated when FromRole == "bot" (werewolf LLM agent) and
// the bot has a configured model_key in the LLM registry. Surfaced in the
// room chat panel as e.g. "Bot 2 · 美团 LongCat-2.0" so spectators can tell
// which model produced which utterance. Empty when unavailable.
//
// Whisper fields (ToUserID, ToAccount, Whisper) are non-empty only for private
// messages.  The server broadcasts whisper frames to all room subscribers; the
// frontend filters visibility based on user role (sender, recipient, admin).
//
// IsInterject is true for "non-current-speaker" bot chat (the werewolf agent
// driver calls Interject() to push a follow-up question / banter / mild
// challenge during the speak phase). The UI marks these messages with 💬 so
// they read as "chat" rather than the formal "speech" — the distinction helps
// players track whose turn it is and prevents confusion when a bot drops a
// quick aside while someone else is formally speaking. omitempty keeps the
// wire shape backward-compatible for legacy clients (R38).
type ChatMessage struct {
	ID            uint64 `json:"id"`
	Scope         string `json:"scope"`
	RoomID        string `json:"room_id,omitempty"`
	FromUserID    string `json:"from_user_id"`
	FromAccount   string `json:"from_account"`
	FromRole      string `json:"from_role,omitempty"`
	FromAgentName string `json:"from_agent_name,omitempty"`
	ToUserID      string `json:"to_user_id,omitempty"`
	ToAccount     string `json:"to_account,omitempty"`
	Whisper       bool   `json:"whisper,omitempty"`
	IsInterject   bool   `json:"is_interject,omitempty"`
	Text          string `json:"text"`
	TS            int64  `json:"ts"` // unix milliseconds
}

// ActivityEvent represents a structured game event broadcast into the room
// chat stream alongside regular chat messages (speak / interject / whisper).
//
// 2026-07-09 §13 增强 (§115 房间聊天) — see docs/狼人杀-Agent与系统/狼人杀房间聊天设计.md
// for the full schema. Activity events are not persisted to
// t_lsm_game_chat_message; they are transient UI cues for "phase change",
// "vote ended", "wolf kill", etc. The chat history replay uses
// game.state.phase + round_number + players[].alive as the source of truth,
// so the absence of activity events in the history is by design.
//
// Frontend renders activity events as a coloured chip inserted between
// regular chat messages, distinguished by FromRole == "activity".
//
// RefSeat / RefSeat2 use pointer types so we can serialize "no seat" (-1)
// as omitted rather than 0 — important for the vote_cast event where the
// actor's seat is meaningful but no target is set.
type ActivityEvent struct {
	ID            uint64 `json:"id,omitempty"`
	RoomID        string `json:"room_id"`
	EventKind     string `json:"event_kind"`
	Text          string `json:"text"`
	Phase         string `json:"phase,omitempty"`
	RoundNumber   int    `json:"round_number,omitempty"`
	Severity      string `json:"severity,omitempty"` // "info" | "success" | "warn" | "error"
	Icon          string `json:"icon,omitempty"`
	RefSeat       *int   `json:"ref_seat,omitempty"`
	RefSeat2      *int   `json:"ref_seat_2,omitempty"`
	SilentForBots bool   `json:"silent_for_bots,omitempty"`
	TS            int64  `json:"ts"` // unix milliseconds
}

// ChatService persists and dispatches chat messages over the WS hub.
type ChatService struct {
	db       *gorm.DB
	hub      *Hub
	llmReg   *llm.Registry // optional; used to surface bot model name in chat frames

	// roomSvc 用于狼人杀房间内 whisper 阵营守卫(§20260810-03 F1)。
	// nil 时不做阵营校验(向后兼容:旧部署/测试桩)。
	roomSvc *service.RoomService

	// factionLookup 接收 (roomID, userID) → faction 字符串("werewolf"|"good"|"unknown")
	// + alive bool + isSpectator bool。由 ChatService.SetFactionLookup 注入。
	// §20260810-03 F1:狼人杀 whisper 阵营守卫复用此回调,避免 chat_service 直接
	// 依赖 werewolf 包造成循环 import。
	factionLookup func(roomID, userID string) (faction string, alive, isSpectator bool)

	// onRoomMessage is an optional hook fired by Send/Whisper/SendFromBot/
	// WhisperFromBot after a room-scoped message is persisted. The werewolf
	// manager registers a callback here so it can stream recent speeches
	// and whispers into the per-agent GameContext (BUG: 狼人杀 7 人局 Agent
	// 多轮上下文). Hook is invoked synchronously and MUST be non-blocking.
	// A nil hook is a no-op so non-werewolf deployments keep working.
	onRoomMessage func(msg *ChatMessage)

	// onRoomActivity is an optional hook fired by EmitRoomActivity after
	// the activity envelope is broadcast. The werewolf manager registers
	// a callback to decide whether to push the event into the per-bot
	// 500K chat queue (events with silent_for_bots=true skip this). Hook
	// is invoked synchronously and MUST be non-blocking; a nil hook is a
	// no-op. 2026-07-09 §13 增强 §115 房间聊天.
	onRoomActivity func(ev *ActivityEvent)

	// 2026-07-10 §4: 模型对局日志 hook
	// recordLog 注入 RecordLogService(2026-07-10 §4),SendFromBot /
	// WhisperFromBot / SendInterjectFromBot 在 broadcast 之前调
	// RecordChatMessage 写 t_lsm_game_model_chat_message。
	// nil-safe:nil 时 no-op,旧部署不影响。
	recordLog *agentcore.RecordLogService
	// recordLogProviderID 是当前游戏使用的 provider_id(全局默认),
	// 用于 model_chat_message.provider_id 字段填充。无 provider 时为 ""。
	recordLogProviderID string

	// 2026-07-12 §127 增强 — bot LLM 流式输出的前端实时 token 瀑布流。
	// streamDeltaIndex 是 stream 内单调递增的 delta 序号(map[streamID]index)。
	streamDeltaIndex   map[string]int
	streamDeltaIndexMu sync.Mutex
}

// ResetBotStreamDeltaIndex 在 stream_end 时调用,释放已完成 streamIndex 槽位。
// 不在 SendBotStreamEnd 内部调用以免多 stream 并发时误删。
func (s *ChatService) ResetBotStreamDeltaIndex(streamID string) {
	if streamID == "" {
		return
	}
	s.streamDeltaIndexMu.Lock()
	delete(s.streamDeltaIndex, streamID)
	s.streamDeltaIndexMu.Unlock()
}

// nextBotStreamDeltaIndex 返回 stream 内下一个单调递增的 delta 序号(0-based)。
func (s *ChatService) nextBotStreamDeltaIndex(streamID string) int {
	if streamID == "" {
		return 0
	}
	s.streamDeltaIndexMu.Lock()
	defer s.streamDeltaIndexMu.Unlock()
	if s.streamDeltaIndex == nil {
		s.streamDeltaIndex = make(map[string]int)
	}
	idx := s.streamDeltaIndex[streamID]
	s.streamDeltaIndex[streamID] = idx + 1
	return idx
}

// NewChatService wires the service to a GORM DB and the WS Hub. llmReg is
// optional; pass nil in tests or non-werewolf deployments.
func NewChatService(db *gorm.DB, hub *Hub, llmReg *llm.Registry) *ChatService {
	return &ChatService{db: db, hub: hub, llmReg: llmReg}
}

// SetRoomMessageHook installs a callback fired after every successful
// room-scoped Send/Whisper/SendFromBot/WhisperFromBot. Used by the
// werewolf manager to capture room transcripts for the per-agent
// GameContext (BUG: 狼人杀 7 人局 Agent 多轮上下文). Only one hook is
// supported; calling SetRoomMessageHook again overwrites the previous
// hook. Pass nil to clear the hook.
func (s *ChatService) SetRoomMessageHook(fn func(msg *ChatMessage)) {
	s.onRoomMessage = fn
}

// SetRoomService wires the RoomService so Whisper can query the room's
// game_kind to decide whether to apply the §20260810-03 F1 cross-faction
// guard. Pass nil to disable the guard (default for tests / non-werewolf
// deployments).
func (s *ChatService) SetRoomService(svc *service.RoomService) {
	s.roomSvc = svc
}

// SetFactionLookup wires the werewolf-side helper that resolves a
// user_id within a werewolf room into (faction, alive, isSpectator).
// §20260810-03 F1 — used by Whisper to apply the same faction guard that
// the Agent driver uses internally. nil disables the guard.
func (s *ChatService) SetFactionLookup(fn func(roomID, userID string) (faction string, alive, isSpectator bool)) {
	s.factionLookup = fn
}

// SetRoomActivityHook installs a callback fired by EmitRoomActivity after
// the activity envelope is broadcast. The werewolf manager uses this to push
// non-silent events into the per-bot 500K chat queue. Pass nil to clear.
// See docs/狼人杀-Agent与系统/狼人杀房间聊天设计.md §3.4.
func (s *ChatService) SetRoomActivityHook(fn func(ev *ActivityEvent)) {
	s.onRoomActivity = fn
}

// SetRecordLog 注入 RecordLogService。SendFromBot / WhisperFromBot /
// SendInterjectFromBot 在 broadcast 之前会调 RecordChatMessage 异步写
// t_lsm_game_model_chat_message。providerID 缺省传 ""(从全局默认模型
// 派生;按需在 main.go 注入精确值)。2026-07-10 §4.
//
// 旧部署 / 测试桩可以保持 nil(无 RecordChatMessage 调用)。
func (s *ChatService) SetRecordLog(rec *agentcore.RecordLogService, providerID string) {
	s.recordLog = rec
	s.recordLogProviderID = providerID
}

// recordBotChat 异步写一条 bot 聊天到 t_lsm_game_model_chat_message。
// 2026-07-10 §4 — 不阻塞游戏流,失败仅 log。
//
// 设计权衡:ChatService 不知道 seat / phase 细节;但有
// (roomID, botUserID) 对。简单做法:从 recordLog 的 cache 反查 gameLogID
// (走 RecordLogService.GameLogIDByBotUser);cache miss 时静默跳过
// (测试 / 老代码路径)。
func (s *ChatService) recordBotChat(roomID, botUserID, role, phase, text string) {
	if s.recordLog == nil {
		return
	}
	gameLogID := s.recordLog.GameLogIDByBotUser(roomID, botUserID)
	if gameLogID == "" {
		return
	}
	s.recordLog.RecordChatMessage(
		gameLogID,
		botUserID,
		s.recordLogProviderID,
		roomID,
		role,
		phase,
		0, // Seq:0 = 暂未跟踪 message-level seq
		text,
		"", // Thinking:聊天原文不含 thinking
		"", // ToolName:N/A
		"", // ToolInput:N/A
		"", // StopReason:N/A
		0,  // LatencyMs:0(无 LLM 计时)
	)
}

// emitRoomMessage invokes the registered hook (if any) for a room-scoped
// message. Hooks MUST be non-blocking — this is called inline from the
// chat send path which holds the WS send channel. Hooks that do heavy
// work should hand off to a goroutine.
func (s *ChatService) emitRoomMessage(msg *ChatMessage) {
	if s.onRoomMessage == nil || msg == nil {
		return
	}
	if msg.Scope != "room" || msg.RoomID == "" {
		return
	}
	// BUG-R218 (2026-07-31): hook 入站日志,确认 onRoomMessage 实际被调用。
	// 自动化测试报告 20260731_194909 §4.2 报告"未观察到 werewolf: spectator
	// wake fired",但当时无法确认是 hook 没跑到还是 hook 跑到了但 wake
	// 被阶段白名单 / 限流挡掉。此日志与 maybeSpectatorWake 内的 Info
	// 形成完整链路,可二分定位缺陷位置。
	logger.L().Info("chat: emitRoomMessage hook invoked",
		zap.String("room_id", msg.RoomID),
		zap.String("from_user_id", msg.FromUserID),
		zap.String("from_role", msg.FromRole),
		zap.Bool("whisper", msg.Whisper))
	defer func() {
		// Hooks that panic must not break the chat broadcast path.
		if r := recover(); r != nil {
			logger.L().Warn("chat onRoomMessage hook panicked",
				zap.Any("recover", r),
				zap.String("room_id", msg.RoomID),
				zap.String("from", msg.FromUserID))
		}
	}()
	s.onRoomMessage(msg)
}

// emitRoomActivity invokes the registered activity hook (if any). Mirrors
// emitRoomMessage's panic-recover + skip-on-empty behaviour.
// 2026-07-09 §13 增强 §115 房间聊天.
func (s *ChatService) emitRoomActivity(ev *ActivityEvent) {
	if s.onRoomActivity == nil || ev == nil {
		return
	}
	if ev.RoomID == "" {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			logger.L().Warn("chat onRoomActivity hook panicked",
				zap.Any("recover", r),
				zap.String("room_id", ev.RoomID),
				zap.String("event_kind", ev.EventKind))
		}
	}()
	s.onRoomActivity(ev)
}

// HandleClientFrame routes a chat.* envelope originating from a client.
// It is called by Client.ReadPump.
func (s *ChatService) HandleClientFrame(c *Client, env Envelope) {
	switch env.Type {
	case "chat.subscribe":
		var p struct {
			Scope  string `json:"scope"`
			RoomID string `json:"room_id"`
		}
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			s.sendError(c, 20001, "bad chat.subscribe payload")
			return
		}
		if err := s.subscribe(c, p.Scope, p.RoomID); err != nil {
			s.sendError(c, 20001, err.Error())
			return
		}
		ack, _ := json.Marshal(map[string]any{"scope": p.Scope, "room_id": p.RoomID})
		c.send <- Envelope{Type: "chat.subscribed", Seq: env.Seq, Payload: ack}

	case "chat.unsubscribe":
		var p struct {
			Scope  string `json:"scope"`
			RoomID string `json:"room_id"`
		}
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			s.sendError(c, 20001, "bad chat.unsubscribe payload")
			return
		}
		s.unsubscribe(c, p.Scope, p.RoomID)
		ack, _ := json.Marshal(map[string]any{"scope": p.Scope, "room_id": p.RoomID})
		c.send <- Envelope{Type: "chat.unsubscribed", Seq: env.Seq, Payload: ack}

	case "chat.send":
		var p struct {
			Scope  string `json:"scope"`
			RoomID string `json:"room_id"`
			Text   string `json:"text"`
		}
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			s.sendError(c, 20001, "bad chat.send payload")
			return
		}
		text := strings.TrimSpace(p.Text)
		if text == "" {
			s.sendError(c, 20001, "empty message")
			return
		}
		if utf8.RuneCountInString(text) > maxChatTextLen {
			s.sendError(c, 20001, "message too long")
			return
		}
		msg, err := s.Send(c, p.Scope, p.RoomID, text)
		if err != nil {
			s.sendError(c, 20001, err.Error())
			return
		}
		_ = msg // fan-out already done in Send

	case "chat.whisper":
		var p struct {
			Scope     string `json:"scope"`
			RoomID    string `json:"room_id"`
			ToUserID  string `json:"to_user_id"`
			ToAccount string `json:"to_account"`
			Text      string `json:"text"`
		}
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			s.sendError(c, 20001, "bad chat.whisper payload")
			return
		}
		text := strings.TrimSpace(p.Text)
		if text == "" {
			s.sendError(c, 20001, "empty message")
			return
		}
		if utf8.RuneCountInString(text) > maxChatTextLen {
			s.sendError(c, 20001, "message too long")
			return
		}
		if p.Scope != "room" || p.RoomID == "" {
			s.sendError(c, 20001, "whisper only supported in room scope")
			return
		}
		if p.ToUserID == "" {
			s.sendError(c, 20001, "to_user_id is required for whisper")
			return
		}
		if p.ToUserID == c.UserID {
			s.sendError(c, 20001, "cannot whisper to yourself")
			return
		}
		msg, err := s.Whisper(c, p.RoomID, p.ToUserID, p.ToAccount, text)
		if err != nil {
			s.sendError(c, 20001, err.Error())
			return
		}
		_ = msg

	case "chat.history":
		var p struct {
			Scope    string `json:"scope"`
			RoomID   string `json:"room_id"`
			Limit    int    `json:"limit"`
			BeforeID uint64 `json:"before_id"`
		}
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			s.sendError(c, 20001, "bad chat.history payload")
			return
		}
		msgs, hasMore, err := s.History(p.BeforeID, p.Scope, p.RoomID, p.Limit)
		if err != nil {
			s.sendError(c, 40002, err.Error())
			return
		}
		// next_cursor is the id of the last (oldest) message in the page; the
		// client passes it back as `before_id` to fetch the previous page.
		var nextCursor *uint64
		if len(msgs) > 0 {
			last := msgs[len(msgs)-1].ID
			nextCursor = &last
		}
		payload, _ := json.Marshal(map[string]any{
			"scope":       p.Scope,
			"room_id":     p.RoomID,
			"messages":    msgs,
			"has_more":    hasMore,
			"next_cursor": nextCursor,
		})
		c.send <- Envelope{Type: "chat.history", Seq: env.Seq, Payload: payload}

	default:
		// unknown chat frame
		s.sendError(c, 20001, "unknown chat type: "+env.Type)
	}
}

// subscribe registers the client on the requested scope.
func (s *ChatService) subscribe(c *Client, scope, roomID string) error {
	switch scope {
	case "lobby":
		s.hub.SubscribeLobby(c)
		return nil
	case "room":
		if roomID == "" {
			return errRoomIDRequired
		}
		s.hub.SubscribeRoom(roomID, c)
		return nil
	default:
		return errUnknownScope
	}
}

// unsubscribe removes the client from the requested scope.
func (s *ChatService) unsubscribe(c *Client, scope, roomID string) {
	switch scope {
	case "lobby":
		s.hub.UnsubscribeLobby(c)
	case "room":
		if roomID != "" {
			s.hub.UnsubscribeRoom(roomID, c)
		}
	}
}

// Send persists a message and broadcasts it to the relevant scope.
func (s *ChatService) Send(c *Client, scope, roomID, text string) (*ChatMessage, error) {
	switch scope {
	case "lobby":
	case "room":
		if roomID == "" {
			return nil, errRoomIDRequired
		}
	default:
		return nil, errUnknownScope
	}

	account := s.lookupAccount(c.UserID)
	role := s.senderRole(scope, roomID, c)

	row := models.TLsmGameChatMessage{
		Scope:       scope,
		RoomID:      roomID,
		FromUserID:  c.UserID,
		FromAccount: account,
		FromRole:    role,
		Text:        text,
	}
	if err := s.db.Create(&row).Error; err != nil {
		logger.L().Warn("chat persist failed", zap.Error(err))
		return nil, errDB
	}

	msg := &ChatMessage{
		ID:          row.ID,
		Scope:       row.Scope,
		RoomID:      row.RoomID,
		FromUserID:  row.FromUserID,
		FromAccount: row.FromAccount,
		FromRole:    role,
		Text:        row.Text,
		TS:          row.CreatedAt.UnixMilli(),
	}
	if row.CreatedAt.IsZero() {
		msg.TS = time.Now().UnixMilli()
	}

	payload, _ := json.Marshal(msg)
	env := Envelope{Type: "chat.message", Payload: payload}
	switch scope {
	case "lobby":
		s.hub.BroadcastLobby(env)
	case "room":
		// Fan out to BOTH players and spectators so either side sees the
		// other's messages. Spectators and chat-only subscribers are pulled
		// from disjoint hub sets; the helper unifies them.
		s.hub.BroadcastRoomIncludingSpectators(roomID, env)
		// BUG: 狼人杀 7 人局 Agent 多轮上下文 — notify the werewolf manager
		// (if wired) so it can append the message to per-agent
		// RecentSpeeches / WhisperInbox for the next decision turn.
		s.emitRoomMessage(msg)
		// BUG-R218 (2026-07-31): 观战者公开发言链路日志 — 自动化测试报告
		// 20260731_194909 §4.2 实测 UI 显示已发但服务端日志无 wake 痕迹。
		// 在 Send 收尾 + emitRoomMessage 之后 Info 级打印,便于排查
		// (a) chat 持久化是否成功 (b) hook 是否被调用 (c) 角色是否识别为 spectator。
		if scope == "room" && role == "spectator" {
			logger.L().Info("chat: spectator public message send pipeline complete",
				zap.String("room_id", roomID),
				zap.String("from_user_id", c.UserID),
				zap.String("from_account", account),
				zap.String("role", role),
				zap.Bool("hook_wired", s.onRoomMessage != nil),
				zap.Int("text_len", len(text)))
		}
	}
	logger.L().Debug("chat message sent",
		zap.String("scope", scope),
		zap.String("room_id", roomID),
		zap.String("from", c.UserID),
		zap.String("role", role),
		zap.Int("len", len(text)))
	return msg, nil
}

// Whisper persists a private message and broadcasts it to the room.
// The frame is sent to ALL room subscribers (players + spectators); the
// frontend filters visibility: only sender, recipient, and admin/superadmin
// users see the message.  This avoids the Hub needing to track admin status.
//
// §20260810-03 F1:在狼人杀房间内,跨阵营 whisper 与观众 whisper 会被硬性拒绝
//(与 Agent whisper 同等待遇);§20260810-03 F2:纯 emoji 短消息(≤5 emoji)被
// 接受并走与普通 whisper 同样的入库/广播路径。
func (s *ChatService) Whisper(c *Client, roomID, toUserID, toAccount, text string) (*ChatMessage, error) {
	account := s.lookupAccount(c.UserID)
	role := s.senderRole("room", roomID, c)

	// §20260810-03 F1 — 狼人杀房间 whisper 阵营守卫。
	// 规则:狼人杀房间(game_kind=="werewolf")内,如果发送者是 spectator,
	// 整条 whisper 直接拒绝(观众通过 whisper 通道向玩家传信息 = 作弊通道,
	// 与 LongCat D5 原文对齐)。如果是存活玩家,查询发送者与目标阵营,
	// 同阵营放行,跨阵营拒绝。
	if roomID != "" && s.roomSvc != nil && s.factionLookup != nil &&
		s.roomSvc.GameKindOf(roomID) == "werewolf" {
		sFaction, sAlive, sSpectator := s.factionLookup(roomID, c.UserID)
		if sSpectator {
			return nil, errSpectatorWhisperForbidden
		}
		if sAlive && toUserID != "" && toUserID != c.UserID {
			tFaction, tAlive, _ := s.factionLookup(roomID, toUserID)
			if tAlive && tFaction != "" && sFaction != "" &&
				sFaction != tFaction && sFaction != "unknown" && tFaction != "unknown" {
				return nil, errCrossFactionWhisper
			}
		}
	}

	// Resolve toAccount if not provided by the client.
	if toAccount == "" {
		toAccount = s.lookupAccount(toUserID)
	}

	row := models.TLsmGameChatMessage{
		Scope:       "room",
		RoomID:      roomID,
		FromUserID:  c.UserID,
		FromAccount: account,
		FromRole:    role,
		ToUserID:    toUserID,
		ToAccount:   toAccount,
		Text:        text,
	}
	if err := s.db.Create(&row).Error; err != nil {
		logger.L().Warn("chat whisper persist failed", zap.Error(err))
		return nil, errDB
	}

	msg := &ChatMessage{
		ID:          row.ID,
		Scope:       row.Scope,
		RoomID:      row.RoomID,
		FromUserID:  row.FromUserID,
		FromAccount: row.FromAccount,
		FromRole:    role,
		ToUserID:    row.ToUserID,
		ToAccount:   row.ToAccount,
		Whisper:     true,
		Text:        row.Text,
		TS:          row.CreatedAt.UnixMilli(),
	}
	if row.CreatedAt.IsZero() {
		msg.TS = time.Now().UnixMilli()
	}

	payload, _ := json.Marshal(msg)
	// BUG-WEREWOLF-R55-WHISPER: 复述段落已压缩 — git blame 与 docs/ 索引可还原

	redactedMsg := *msg
	redactedMsg.Text = "[私聊]"
	redactedPayload, _ := json.Marshal(&redactedMsg)
	exclude := []string{c.UserID}
	if toUserID != "" && toUserID != c.UserID {
		exclude = append(exclude, toUserID)
	}
	s.hub.BroadcastRoomIncludingSpectatorsExcluding(roomID,
		Envelope{Type: "chat.whisper", Payload: redactedPayload},
		exclude...,
	)
	// Now deliver the full text only to participants.
	s.sendWhisperDirect(c, toUserID, Envelope{Type: "chat.whisper", Payload: payload})
	// BUG: 狼人杀 7 人局 Agent 多轮上下文 — see Send() above.
	s.emitRoomMessage(msg)

	logger.L().Debug("chat whisper sent",
		zap.String("room_id", roomID),
		zap.String("from", c.UserID),
		zap.String("to", toUserID),
		zap.String("role", role),
		zap.Int("len", len(text)))

	return msg, nil
}

// sendWhisperDirect is paired with BroadcastRoomIncludingSpectators to deliver
// the *full* (un-redacted) whisper envelope to the sender and recipient only.
// BUG-WEREWOLF-R55-WHISPER.
func (s *ChatService) sendWhisperDirect(sender *Client, toUserID string, env Envelope) {
	if sender != nil {
		select {
		case sender.send <- env:
		default:
		}
	}
	if toUserID == "" || toUserID == sender.UserID {
		return
	}
	s.hub.SendToUser(toUserID, env)
}

// senderRole resolves "player" | "spectator" | "" for the given chat sender.
// An empty result means "unknown relationship" (e.g. lobby); the chat panel
// then renders the message without the role chip.
func (s *ChatService) senderRole(scope, roomID string, c *Client) string {
	if scope != "room" || roomID == "" {
		return ""
	}
	if s.hub.IsSpectatorOf(roomID, c) {
		return "spectator"
	}
	// Players are anyone subscribed to the player-side broadcast set; in
	// practice this matches anyone who has called game.join for this room or
	// subscribed to chat via chat.subscribe (which is shared).
	return "player"
}

// SendFromBot is the bot-equivalent of Send: persists and broadcasts a chat
// message attributed to a bot user (caller passes the bot's userID + account /
// display name). The broadcast envelope is identical to a human player's;
// the frontend distinguishes bot speech via an optional `from_role="bot"`
// field we don't emit here (engine chat panels treat bot speech as regular
// player speech). Used by the werewolf Agent driver so all speech goes
// through one broadcast path.
//
// modelKey is the bot's LLM model_key (e.g. "MeiTuan-model"). When the
// service has an LLM registry wired, it surfaces the matching AgentName in
// the ChatMessage so the room chat panel can render "Bot 2 · 美团 LongCat-2.0".
//
// BUG-WEREWOLF-P1-NEW-32 (Round 33): the AgentName leaks the bot's underlying
// LLM identity to spectators, who can use the leak to game-meta-strategize
// (e.g. "豆包 tends to overclaim seer"). We split the broadcast — players see
// the AgentName badge, spectators see a neutral label. The DB row keeps the
// AgentName for replay; only the live spectator envelope is filtered.
func (s *ChatService) SendFromBot(roomID, botUserID, botAccount, modelKey, text string) (*wwplayer.BotChatSendResult, error) {
	if roomID == "" {
		return nil, errRoomIDRequired
	}

	row := models.TLsmGameChatMessage{
		Scope:       "room",
		RoomID:      roomID,
		FromUserID:  botUserID,
		FromAccount: botAccount,
		// 2026-08-01 BUG-R225-P2-02: 必须显式 FromRole:"bot",否则模型层
		// default:'human' 会让落库行错标为人类,清理 player 后历史消息永久
		// 渲染为人类玩家。
		FromRole: "bot",
		Text:     truncateChatText(text),
	}
	if err := s.db.Create(&row).Error; err != nil {
		logger.L().Warn("bot chat persist failed", zap.Error(err))
		return nil, errDB
	}

	display := botAccount
	if display == "" {
		display = s.lookupAccount(botUserID)
	}

	// BUG-WEREWOLF-P1-NEW-32: 复述段落已压缩 — git blame 与 docs/ 索引可还原

	playerMsg := &ChatMessage{
		ID:            row.ID,
		Scope:         row.Scope,
		RoomID:        row.RoomID,
		FromUserID:    botUserID,
		FromAccount:   display,
		FromRole:      "bot",
		FromAgentName: s.lookupAgentName(modelKey), // §115 翻转 R34:恢复模型名公开
		Text:          row.Text,
		TS:            row.CreatedAt.UnixMilli(),
	}
	if row.CreatedAt.IsZero() {
		playerMsg.TS = time.Now().UnixMilli()
	}

	payload, _ := json.Marshal(playerMsg)
	s.hub.BroadcastRoomIncludingSpectators(roomID, Envelope{Type: "chat.message", Payload: payload})
	// BUG: 狼人杀 7 人局 Agent 多轮上下文 — feed the bot's own speech back
	// into the room transcript so other agents see it.
	s.emitRoomMessage(playerMsg)
	// 2026-07-10 §4: 异步写 bot 聊天到 t_lsm_game_model_chat_message。
	s.recordBotChat(roomID, botUserID, "broadcast_bot", "", text)
	logger.L().Debug("bot chat sent",
		zap.String("room_id", roomID), zap.String("from", botUserID), zap.Int("len", len(text)))
	return &wwplayer.BotChatSendResult{}, nil
}

// SendInterjectFromBot is SendFromBot's "non-current-speaker" sibling.
//
// BUG-WEREWOLF-AGENT-INTERJECT: during the speak phase any alive bot can
// voluntarily chime in (follow-up question, banter, mild challenge) without
// being the formal speaker. The wire envelope carries is_interject=true so
// the UI can render it as 💬插话 distinct from the formal speak broadcast.
// Persistence + broadcast path is otherwise identical to SendFromBot — same
// DB row shape, same hub fan-out (incl. spectators), same transcript feed
// back into the room's RecentSpeeches. The DB row's IsInterject column
// (added in R38) is set so replay surfaces the same distinction.
//
// Throttle is the agent's responsibility (Limiter 30s interval, shared with
// speak). This method intentionally does NOT re-check the throttle — if a
// driver/race lets two interjects through in 30s the worst case is two
// extra chat lines, not a phase stall.
func (s *ChatService) SendInterjectFromBot(roomID, botUserID, botAccount, modelKey, text string) (*wwplayer.BotChatSendResult, error) {
	if roomID == "" {
		return nil, errRoomIDRequired
	}

	row := models.TLsmGameChatMessage{
		Scope:       "room",
		RoomID:      roomID,
		FromUserID:  botUserID,
		FromAccount: botAccount,
		// 2026-08-01 BUG-R225-P2-02: 同 SendFromBot,显式 FromRole:"bot" 避免
		// 模型层 default:'human' 错标。
		FromRole: "bot",
		Text:     truncateChatText(text),
	}
	if err := s.db.Create(&row).Error; err != nil {
		logger.L().Warn("bot interject persist failed", zap.Error(err))
		return nil, errDB
	}

	display := botAccount
	if display == "" {
		display = s.lookupAccount(botUserID)
	}

	msg := &ChatMessage{
		ID:            row.ID,
		Scope:         row.Scope,
		RoomID:        row.RoomID,
		FromUserID:    botUserID,
		FromAccount:   display,
		FromRole:      "bot",
		FromAgentName: s.lookupAgentName(modelKey), // §115 翻转 R34:恢复模型名公开
		IsInterject:   true,
		Text:          row.Text,
		TS:            row.CreatedAt.UnixMilli(),
	}
	if row.CreatedAt.IsZero() {
		msg.TS = time.Now().UnixMilli()
	}

	payload, _ := json.Marshal(msg)
	s.hub.BroadcastRoomIncludingSpectators(roomID, Envelope{Type: "chat.message", Payload: payload})
	// Feed interject into the room transcript so other agents see it.
	// markRecentSpeechIsInterject=false here is fine: the recipient agent
	// reads is_interject from the wire payload when it builds its
	// GameContext; the in-memory slice just stores the raw events.
	s.emitRoomMessage(msg)
	// 2026-07-10 §4: 异步写 bot 插话到 model chat log(role=interject_bot)。
	s.recordBotChat(roomID, botUserID, "interject_bot", "", text)
	logger.L().Debug("bot interject sent",
		zap.String("room_id", roomID), zap.String("from", botUserID), zap.Int("len", len(text)))
	return &wwplayer.BotChatSendResult{}, nil
}

// SendFromJudge 是法官(主持人)宣告的广播路径(对齐 SendFromBot)。
//
// 2026-07-16 主持人重构:法官 announce/declare_cause 成功后由 AgentJudge.onAnnounce
// 回调调本方法,把宣告送进房间公屏。FromRole="judge" + FromAccount="[法官·{model}]"
// 让前端 GameChatPanel 以特殊样式(⚖️ 前缀 + 金底)渲染。
//
// 流程:写 chat_message 表 + BroadcastRoomIncludingSpectators + feed room transcript
// (经 emitRoomMessage 让房间内其他玩家/观战者在 chat_history 队列里见到)。
// kind 是事件类型字符串(仅供日志,不影响广播/落库)。
func (s *ChatService) SendFromJudge(roomID, fromAccount, modelKey, text, kind string) (*wwplayer.BotChatSendResult, error) {
	if roomID == "" {
		return nil, errRoomIDRequired
	}

	row := models.TLsmGameChatMessage{
		Scope:       "room",
		RoomID:      roomID,
		// 2026-07-17 R139 修复:FromUserID 必须 ≤ 36 字符;法官此前用
		// "judge:"+roomID(总 42 字符)超过列宽度 → Error 1406。现改用 zero-uuid
		// 占位 + FromRole="judge" 区分;前端 GameChatPanel 按 FromRole 走 ⚖️ 渲染。
		FromUserID:  "00000000-0000-0000-0000-000000000000",
		FromAccount: fromAccount,
		FromRole:    "judge",
		Text:        truncateChatText(text),
	}
	if err := s.db.Create(&row).Error; err != nil {
		logger.L().Warn("judge chat persist failed", zap.Error(err))
		return nil, errDB
	}

	display := fromAccount
	if display == "" {
		display = s.lookupAccount(row.FromUserID)
	}

	playerMsg := &ChatMessage{
		ID:            row.ID,
		Scope:         row.Scope,
		RoomID:        row.RoomID,
		FromUserID:    row.FromUserID,
		FromAccount:   display,
		FromRole:      "judge",
		FromAgentName: s.lookupAgentName(modelKey),
		Text:          row.Text,
		TS:            row.CreatedAt.UnixMilli(),
	}
	if row.CreatedAt.IsZero() {
		playerMsg.TS = time.Now().UnixMilli()
	}

	payload, _ := json.Marshal(playerMsg)
	s.hub.BroadcastRoomIncludingSpectators(roomID, Envelope{Type: "chat.message", Payload: payload})
	// feed 进房间 transcript,让 chat_history 队列收录法官播报。
	s.emitRoomMessage(playerMsg)
	logger.L().Debug("judge chat sent",
		zap.String("room_id", roomID), zap.String("kind", kind), zap.Int("len", len(text)))
	return &wwplayer.BotChatSendResult{}, nil
}

// §127: 复述段落已压缩 — git blame 与 docs/ 索引可还原


// SendBotStreamStart 发送 chat.stream_start 帧,标记 bot 开始流式输出。
func (s *ChatService) SendBotStreamStart(roomID string, seat int, streamID string) error {
	if roomID == "" || streamID == "" {
		return nil
	}
	payload, _ := json.Marshal(map[string]any{
		"stream_id": streamID,
		"seat":      seat,
		"ts":        time.Now().UnixMilli(),
	})
	s.hub.BroadcastRoomIncludingSpectators(roomID,
		Envelope{Type: "chat.stream_start", Payload: payload})
	return nil
}

// SendBotStreamDelta 发送 chat.stream_delta 帧,实时追加文本 token。
func (s *ChatService) SendBotStreamDelta(roomID, streamID, delta string) error {
	if roomID == "" || streamID == "" || delta == "" {
		return nil
	}
	index := s.nextBotStreamDeltaIndex(streamID)
	payload, _ := json.Marshal(map[string]any{
		"stream_id": streamID,
		"delta":     delta,
		"index":     index,
		"ts":        time.Now().UnixMilli(),
	})
	s.hub.BroadcastRoomIncludingSpectators(roomID,
		Envelope{Type: "chat.stream_delta", Payload: payload})
	return nil
}

// SendBotStreamEnd 发送 chat.stream_end 帧,标记 bot 流式输出完成。
func (s *ChatService) SendBotStreamEnd(roomID, streamID, fullText string) error {
	if roomID == "" || streamID == "" {
		return nil
	}
	payload, _ := json.Marshal(map[string]any{
		"stream_id": streamID,
		"full_text": fullText,
		"ts":        time.Now().UnixMilli(),
	})
	s.hub.BroadcastRoomIncludingSpectators(roomID,
		Envelope{Type: "chat.stream_end", Payload: payload})
	// 释放 stream 内序号槽位,避免 map 无限增长。
	s.ResetBotStreamDeltaIndex(streamID)
	return nil
}

// WhisperFromBot is the bot-equivalent of Whisper: persists and broadcasts a
// private chat message from a bot to a seat's userID. The client filters
// visibility by role (sender / recipient / admin).
func (s *ChatService) WhisperFromBot(roomID, botUserID, botAccount, modelKey, toUserID, toAccount, text string) (*wwplayer.BotChatSendResult, error) {
	if roomID == "" {
		return nil, errRoomIDRequired
	}
	display := botAccount
	if display == "" {
		display = s.lookupAccount(botUserID)
	}
	toDisplay := toAccount
	if toDisplay == "" {
		toDisplay = s.lookupAccount(toUserID)
	}

	row := models.TLsmGameChatMessage{
		Scope:       "room",
		RoomID:      roomID,
		FromUserID:  botUserID,
		FromAccount: display,
		ToUserID:    toUserID,
		ToAccount:   toDisplay,
		// 2026-08-01 BUG-R225-P2-02: WhisperFromBot 同样需显式 FromRole:"bot"。
		FromRole: "bot",
		Text:     text,
	}
	if err := s.db.Create(&row).Error; err != nil {
		logger.L().Warn("bot whisper persist failed", zap.Error(err))
		return nil, errDB
	}

	msg := &ChatMessage{
		ID:            row.ID,
		Scope:         row.Scope,
		RoomID:        row.RoomID,
		FromUserID:    botUserID,
		FromAccount:   display,
		FromRole:      "bot",
		FromAgentName: s.lookupAgentName(modelKey),
		ToUserID:      row.ToUserID,
		ToAccount:     row.ToAccount,
		Whisper:       true,
		Text:          row.Text,
		TS:            row.CreatedAt.UnixMilli(),
	}
	if row.CreatedAt.IsZero() {
		msg.TS = time.Now().UnixMilli()
	}
	payload, _ := json.Marshal(msg)
	// BUG-WEREWOLF-R55-WHISPER: 复述段落已压缩 — git blame 与 docs/ 索引可还原

	redactedMsg := *msg
	redactedMsg.Text = "[私聊]"
	redactedPayload, _ := json.Marshal(&redactedMsg)
	exclude := []string{botUserID}
	if toUserID != "" && toUserID != botUserID {
		exclude = append(exclude, toUserID)
	}
	s.hub.BroadcastRoomIncludingSpectatorsExcluding(roomID,
		Envelope{Type: "chat.whisper", Payload: redactedPayload},
		exclude...,
	)
	if toUserID != "" && toUserID != botUserID {
		s.hub.SendToUser(toUserID, Envelope{Type: "chat.whisper", Payload: payload})
	}
	// BUG: 狼人杀 7 人局 Agent 多轮上下文 — see SendFromBot above.
	s.emitRoomMessage(msg)
	logger.L().Debug("bot whisper sent",
		zap.String("room_id", roomID), zap.String("from", botUserID), zap.String("to", toUserID))
	// 2026-07-10 §4: 异步写 bot 私聊到 model chat log(role=whisper_bot)。
	s.recordBotChat(roomID, botUserID, "whisper_bot", "", text)
	return &wwplayer.BotChatSendResult{}, nil
}

// lookupAgentName resolves a bot's model_key to its display AgentName from
// the LLM registry. Returns empty string if the registry is absent or the
// model is unknown — callers must treat empty as "no agent name available".
func (s *ChatService) lookupAgentName(modelKey string) string {
	if s.llmReg == nil || modelKey == "" {
		return ""
	}
	for _, info := range s.llmReg.List() {
		if info.Model == modelKey {
			return info.AgentName
		}
	}
	return ""
}

// History returns the most recent messages for a scope, oldest first.
//
// beforeID > 0 activates keyset pagination: rows with `id < beforeID` are
// returned (useful for loading older messages relative to the last message
// the client has already seen). beforeID == 0 returns the latest limit
// messages.
//
// For room scope, bot messages (FromUserID starting with `bot_`) get their
// FromRole="bot" and FromAgentName populated so the room chat panel can
// render the AI model badge consistently with live broadcast frames. The
// FromAccount is also rewritten to a friendly "Bot N号" form using the
// bot's seat number from t_lsm_game_player. Falls back gracefully when
// the player row is missing or the bot prefix doesn't match.
//
// The second return value `hasMore` is true when the page returned exactly
// `limit` rows — a hint that older messages exist. The client should use
// `messages[len-1].id` as the next page's `before_id`.
func (s *ChatService) History(beforeID uint64, scope, roomID string, limit int) ([]ChatMessage, bool, error) {
	switch scope {
	case "lobby":
	case "room":
		if roomID == "" {
			return nil, false, errRoomIDRequired
		}
	default:
		return nil, false, errUnknownScope
	}
	if limit <= 0 || limit > maxHistoryN {
		limit = defaultHistoryN
	}

	q := s.db.Model(&models.TLsmGameChatMessage{}).
		Where("scope = ?", scope).
		Order("id DESC").
		Limit(limit)
	if scope == "room" {
		q = q.Where("room_id = ?", roomID)
	}
	if beforeID > 0 {
		q = q.Where("id < ?", beforeID)
	}
	var rows []models.TLsmGameChatMessage
	if err := q.Find(&rows).Error; err != nil {
		return nil, false, err
	}

	// For room scope, build a (user_id → seat/model_key) map for bots so we
	// can decorate history frames with role + agent name. Built lazily so
	// the lobby-scope path keeps its previous cost.
	var botSeat map[string]int
	var botModel map[string]string
	if scope == "room" && roomID != "" {
		var bots []models.TLsmGamePlayer
		if err := s.db.Select("user_id, seat, model_key").
			Where("room_id = ? AND role = ?", roomID, models.PlayerRoleAgent).
			Find(&bots).Error; err == nil {
			if len(bots) > 0 {
				botSeat = make(map[string]int, len(bots))
				botModel = make(map[string]string, len(bots))
				for _, b := range bots {
					botSeat[b.UserID] = b.Seat
					botModel[b.UserID] = b.ModelKey
				}
			}
		}
	}

	hasMore := len(rows) == limit

	// Reverse so callers see oldest-first.
	out := make([]ChatMessage, 0, len(rows))
	for i := len(rows) - 1; i >= 0; i-- {
		r := rows[i]
		displayAccount := r.FromAccount
		role := ""
		agentName := ""
		if seat, ok := botSeat[r.FromUserID]; ok {
			role = "bot"
			// §115 翻转 R34 BUG-WEREWOLF-P1-NEW-32: history replay 恢复
			// AgentName 字段,与 live broadcast 保持一致。模型名是
			// LLM 服务侧标识,7-AI 房辨识度 + 测试报告归类需要它。
			if mk, ok2 := botModel[r.FromUserID]; ok2 {
				agentName = s.lookupAgentName(mk)
			}
			// Override the raw "bot_<8chars>_<seat>" account name with a
			// human-readable label that matches the lobby/table UI:
			//   - if we know the seat number, show "Bot N号"
			//   - else fall back to the persisted account
			displayAccount = fmt.Sprintf("Bot %d号", seat+1)
		}
		out = append(out, ChatMessage{
			ID:            r.ID,
			Scope:         r.Scope,
			RoomID:        r.RoomID,
			FromUserID:    r.FromUserID,
			FromAccount:   displayAccount,
			FromRole:      role,
			FromAgentName: agentName,
			ToUserID:      r.ToUserID,
			ToAccount:     r.ToAccount,
			Whisper:       r.ToUserID != "",
			Text:          r.Text,
			TS:            r.CreatedAt.UnixMilli(),
		})
	}
	return out, hasMore, nil
}

// lookupAccount resolves a userID to its display name (nickname preferred,
// falling back to account). Falls back to the userID when the row is missing
// so we never leak an empty sender name.
func (s *ChatService) lookupAccount(userID string) string {
	var u models.TLsmGameUser
	if err := s.db.Select("nickname, account").Where("id = ?", userID).Take(&u).Error; err != nil {
		return userID
	}
	if u.Nickname != "" {
		return u.Nickname
	}
	if u.Account != "" {
		return u.Account
	}
	return userID
}

// EmitRoomActivity broadcasts a structured game activity event into the room
// chat stream. Activity events are NOT persisted to t_lsm_game_chat_message —
// they are transient UI cues ("phase change", "vote ended", "wolf kill"). The
// game state itself remains the source of truth for replays (see
// game.state.phase, round_number, players[].alive).
//
// 2026-07-09 §13 增强 §115 房间聊天 — see docs/狼人杀-Agent与系统/狼人杀房间聊天设计.md.
//
// Parameters:
//   - roomID         : the room to broadcast to
//   - eventKind      : one of ActivityEventKind* constants
//   - text           : human-readable Chinese label
//   - phase          : current phase (optional, used for color hints)
//   - roundNumber    : current day number (optional)
//   - severity       : "info" | "success" | "warn" | "error" (default "info")
//   - icon           : emoji prefix (optional, e.g. "🌙")
//   - refSeat        : primary related seat (-1 = none; 0..6 otherwise)
//   - refSeat2       : secondary related seat (e.g. vote target; -1 = none)
//   - silentForBots  : true → do NOT push into 500K bot queues
//
// Returns true if the broadcast was enqueued, false if the room is missing
// or the input was invalid.
func (s *ChatService) EmitRoomActivity(roomID, eventKind, text, phase string,
	roundNumber int, severity, icon string, refSeat, refSeat2 int, silentForBots bool) bool {
	if roomID == "" || eventKind == "" {
		return false
	}
	if severity == "" {
		severity = "info"
	}
	ev := &ActivityEvent{
		RoomID:        roomID,
		EventKind:     eventKind,
		Text:          text,
		Phase:         phase,
		RoundNumber:   roundNumber,
		Severity:      severity,
		Icon:          icon,
		SilentForBots: silentForBots,
		TS:            time.Now().UnixMilli(),
	}
	if refSeat >= 0 {
		s := refSeat
		ev.RefSeat = &s
	}
	if refSeat2 >= 0 {
		s := refSeat2
		ev.RefSeat2 = &s
	}
	payload, _ := json.Marshal(ev)
	env := Envelope{Type: "chat.activity", Payload: payload}
	s.hub.BroadcastRoomIncludingSpectators(roomID, env)
	// Notify the werewolf manager (if wired) so it can decide whether to push
	// the event into the per-bot 500K chat queue.
	s.emitRoomActivity(ev)
	logger.L().Debug("chat activity emitted",
		zap.String("room_id", roomID),
		zap.String("kind", eventKind),
		zap.String("severity", severity),
		zap.Bool("silent_for_bots", silentForBots))
	return true
}

// Activity event kind constants. Kept here as a public API surface for
// callers (werewolf manager) and frontend parity. 2026-07-09 §115.
const (
	ActivityEventKindPhaseTransition = "phase_transition"
	ActivityEventKindRoundStart      = "round_start"
	ActivityEventKindNightStart      = "night_start"
	ActivityEventKindDayStart        = "day_start"
	ActivityEventKindWolfKill        = "wolf_kill"
	ActivityEventKindSeerCheck       = "seer_check"
	ActivityEventKindWitchAct        = "witch_act"
	ActivityEventKindVoteCast        = "vote_cast"
	ActivityEventKindVoteResult      = "vote_result"
	ActivityEventKindHunterShoot     = "hunter_shoot"
	ActivityEventKindSheriffElect    = "sheriff_elect"
	ActivityEventKindGameOver        = "game_over"
	ActivityEventKindPlayerDied      = "player_died"
	ActivityEventKindQuarantine      = "quarantine"
	ActivityEventKindAutoSkip        = "auto_skip"
)

// DeleteRoomMessages purges every chat message whose scope targets a specific
// room. Called as a cascade step when the room itself is deleted so that
// abandoning a room doesn't leave behind orphaned chat history in the shared
// table.
//
// This query benefits from `idx_scope_room_ts` (scope, room_id, created_at),
// which fully matches the predicate for scope == "room".
func (s *ChatService) DeleteRoomMessages(roomID string) error {
	if roomID == "" {
		return nil
	}
	scope := "room"
	res := s.db.Where("scope = ? AND room_id = ?", scope, roomID).
		Delete(&models.TLsmGameChatMessage{})
	if res.Error != nil {
		logger.L().Error("delete room chat messages failed",
			zap.String("room_id", roomID),
			zap.Error(res.Error))
		return res.Error
	}
	if res.RowsAffected > 0 {
		logger.L().Debug("room chat messages purged",
			zap.String("room_id", roomID),
			zap.Int64("rows", res.RowsAffected))
	}
	return nil
}

// DeleteMessagesBefore removes every chat message with `created_at < t`. It
// returns the number of rows deleted. Intended for time-based cleanup: callers
// (janitor, admin tools) pass in a cutoff (e.g. now.Add(-retention)) and each
// call sweeps a chunk.
//
// The `idx_scope_room_ts` composite index ends with `created_at`, so a full
// scan that does not filter on scope/room_id can still use the index for the
// time bound (and MySQL's optimizer often does).
//
// Note: the table is currently NOT sharded. When monthly sharding is enabled,
// the caller must invoke DeleteMessagesBefore once per active shard and sum
// the row counts.
func (s *ChatService) DeleteMessagesBefore(t time.Time) (int64, error) {
	if t.IsZero() {
		return 0, nil
	}
	res := s.db.Where("created_at < ?", t).
		Delete(&models.TLsmGameChatMessage{})
	if res.Error != nil {
		logger.L().Error("chat message time-based cleanup failed",
			zap.Time("before", t),
			zap.Error(res.Error))
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

func (s *ChatService) sendError(c *Client, code int, msg string) {
	payload, _ := json.Marshal(map[string]any{"code": code, "message": msg})
	select {
	case c.send <- Envelope{Type: "chat.error", Payload: payload}:
	default:
	}
}

// Sentinel errors returned to clients in chat.error frames. The numeric code
// matches errcode.go's validation bucket so the client can branch on it.
var (
	errUnknownScope            = chatErr("unknown scope (expected 'lobby' or 'room')")
	errRoomIDRequired          = chatErr("room_id is required for scope=room")
	errDB                      = chatErr("database error")
	// §20260810-03 F1 — 狼人杀房间 whisper 阵营守卫拒绝原因。
	errCrossFactionWhisper    = chatErr("cross-faction whisper forbidden in werewolf room")
	errSpectatorWhisperForbidden = chatErr("spectators cannot whisper in werewolf room")
)

type chatErr string

func (e chatErr) Error() string { return string(e) }

// ─────────────────── Proto 消息注册 ───────────────────
//
// 各 chat.* 帧的 proto 版本注册在此。
// 当前为占位实现，后续逐步迁移每个消息类型。
// 迁移策略：先在 proto 定义消息 → 在此注册 handler → handler 调用
// 与 JSON 路径共享的业务逻辑 → 完成迁移。

// registerProtoMessages 在 proto 路由器中注册聊天服务的所有 proto 消息
func (s *ChatService) registerProtoMessages(reg *ProtoRegistry) {
	// TODO: 逐个迁移 chat.subscribe / chat.unsubscribe / chat.send /
	//       chat.whisper / chat.history / chat.activity / chat.stream_*
	//
	// 示例（待实现）：
	//   reg.Register("chat.subscribe",
	//     func() proto.Message { return &chatpb.ChatSubscribe{} },
	//     func(c *Client, env *commonpb.Envelope, msg proto.Message) {
	//         s.handleProtoSubscribe(c, env, msg.(*chatpb.ChatSubscribe))
	//     },
	//   )
}