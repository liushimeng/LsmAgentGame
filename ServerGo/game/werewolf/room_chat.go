package werewolf

import (
	"strconv"
	"strings"
	"time"

	"LsmWebGame/agent/core"
	"LsmWebGame/agent/wwtypes"
	"LsmWebGame/logger"

	"go.uber.org/zap"
)

type streamChatSvc interface {
	SendBotStreamStart(roomID string, seat int, streamID string) error
	SendBotStreamDelta(roomID, streamID, delta string) error
	SendBotStreamEnd(roomID, streamID, fullText string) error
}

type ChatMessageLike struct {
	FromUserID    string
	FromAccount   string
	FromRole      string // "bot" | "player" | "spectator" | ""
	FromAgentName string
	ToUserID      string
	ToAccount     string
	Whisper       bool
	Text          string
	TS            int64
}

type ChatActivityEvent struct {
	RoomID        string
	EventKind     string
	Text          string
	Phase         string
	RoundNumber   int
	Severity      string
	Icon          string
	RefSeat       int
	RefSeat2      int
	SilentForBots bool
	TS            int64
}

func (m *WerewolfManager) RecordRoomMessage(roomID string, msg ChatMessageLike) {
	if roomID == "" {
		return
	}
	m.mu.RLock()
	r, ok := m.rooms[roomID]
	m.mu.RUnlock()
	if !ok || r == nil {
		return
	}
	r.appendRoomMessage(msg)
	// 2026-07-08 §13: 观战者公开消息触发 Agent wake(节流 + 阶段白名单)
	if msg.FromRole == "spectator" && !msg.Whisper {
		// BUG-R218 (2026-07-31): 观战者公屏发言链路日志 — 在调
		// maybeSpectatorWake 之前 Info 级打印,确保 chat.send → Send →
		// emitRoomMessage → RecordRoomMessage → maybeSpectatorWake 的每一跳
		// 都可观测。配合 ws/chat_service.go 的两处 Info 日志,缺陷
		// 定位可直接二分到具体跳。
		logger.L().Info("werewolf: RecordRoomMessage entering maybeSpectatorWake",
			zap.String("room_id", roomID),
			zap.String("from_role", msg.FromRole),
			zap.String("from_account", msg.FromAccount),
			zap.Bool("whisper", msg.Whisper),
			zap.Int("text_len", len(msg.Text)))
		m.maybeSpectatorWake(r)
	}
}

func (m *WerewolfManager) RecordRoomActivity(ev *ChatActivityEvent) {
	if ev == nil || ev.RoomID == "" {
		return
	}
	m.mu.RLock()
	r, ok := m.rooms[ev.RoomID]
	m.mu.RUnlock()
	if !ok || r == nil {
		return
	}
	if ev.SilentForBots {
		// 纯 UI 提示:不进入 500K 队列(避免污染 LLM 上下文)。
		// 不需要 appendToChatQueueLocked。
		return
	}
	// 非 silent 事件:把结构化事件以 ChatMessage 形式注入 500K 队列。
	// LLM 通过 system prompt 的 chatHistory 段看到"阶段切换/投票结果"等元信息。
	r.appendActivityToChatQueueLocked(ev)

	// 2026-08-10 §20260810-05 — 信息账本接入:死亡事件(不含身份,§135)。
	// 全房存活座位知情;身份明文由 redactLedgerFact 在写入侧剔除。
	if ev.EventKind == "player_died" {
		r.ledgerAppendLocked(InfoSourceDeathEvent, ev.Text, aliveKnowerSetLocked(r), ev.TS)
	}
}

func (r *WerewolfRoom) appendRoomMessage(msg ChatMessageLike) {
	ts := msg.TS
	if ts == 0 {
		ts = time.Now().UnixMilli()
	}
	isBot := msg.FromRole == "bot"
	isSpectator := msg.FromRole == "spectator"

	if msg.Whisper {
		// Resolve recipient seat from ToUserID.
		toSeat := -1
		for i, u := range r.Seats {
			if u != "" && u == msg.ToUserID {
				toSeat = i
				break
			}
		}
		if toSeat < 0 {
			// Whisper to a non-player (e.g. spectator); we still record
			// for that seat's inbox only if they're a known player.
			return
		}
		fromSeat := -1
		for i, u := range r.Seats {
			if u != "" && u == msg.FromUserID {
				fromSeat = i
				break
			}
		}
		ev := wwtypes.WhisperEvent{
			FromSeat:    fromSeat,
			From:        msg.FromAccount,
			AgentName:   msg.FromAgentName,
			IsBot:       isBot,
			IsSpectator: isSpectator,
			Text:        msg.Text,
			Ts:          ts,
		}
		if r.whisperInbox == nil {
			r.whisperInbox = make(map[int][]wwtypes.WhisperEvent, MaxPlayers)
		}
		cap := whisperInboxSize
		buf := r.whisperInbox[toSeat]
		buf = append(buf, ev)
		if len(buf) > cap {
			buf = buf[len(buf)-cap:]
		}
		r.whisperInbox[toSeat] = buf
		// 2026-07-09 §13 增强 — 同步到 500K 队列:whisper 双方(sender + recipient)
		r.appendToChatQueueLocked(fromSeat, toSeat, msg, isBot, isSpectator, true, ts)
		// 2026-08-10 §20260810-05 — 信息账本接入:私聊仅收发双方知情(§119 隔离)。
		r.ledgerAppendLocked(InfoSourceWhisper, msg.Text, pairKnowerSet(fromSeat, toSeat), ts)
		logger.L().Debug("werewolf: whisper recorded for agent inbox",
			zap.String("room_id", r.RoomID),
			zap.Int("to_seat", toSeat),
			zap.Int("from_seat", fromSeat),
			zap.Bool("is_bot", isBot),
			zap.String("agent_name", msg.FromAgentName),
			zap.Int("inbox_len", len(buf)))
		// BUG-R213-P1-01 (2026-07-31): 私聊投递成功必须留下 Info 级日志。
		// 自动化测试报告 2026-07-31 08:17:05 §3.3 实测观战者私聊后无法从
		// 日志确认投递(Debug 级被生产日志级别屏蔽);改为 Info 并补齐
		// from_role/is_spectator 字段,便于排查「UI 显示私聊已发但 bot 未
		// 引用」类问题时区分 (a) 投递未发生 (b) 已投递但 LLM 选择不引用。
		logger.L().Info("werewolf: whisper delivered to agent inbox",
			zap.String("room_id", r.RoomID),
			zap.Int("to_seat", toSeat),
			zap.Int("from_seat", fromSeat),
			zap.String("from_account", msg.FromAccount),
			zap.String("from_role", msg.FromRole),
			zap.Bool("is_bot", isBot),
			zap.Bool("is_spectator", isSpectator),
			zap.Int("inbox_len", len(buf)))
		return
	}

	// Public speech: resolve sender seat. -1 if the sender is a spectator
	// (their chat still shows in the room transcript so the bots can
	// react; the seat label will render as "观战-昵称").
	seat := -1
	for i, u := range r.Seats {
		if u != "" && u == msg.FromUserID {
			seat = i
			break
		}
	}
	ev := wwtypes.SpeechEvent{
		Seat:        seat,
		Account:     msg.FromAccount,
		AgentName:   msg.FromAgentName,
		IsBot:       isBot,
		IsSpectator: isSpectator,
		Text:        msg.Text,
		Ts:          ts,
	}
	if r.recentSpeeches == nil {
		r.recentSpeeches = make([]wwtypes.SpeechEvent, 0, recentSpeechBufferSize)
	}
	r.recentSpeeches = append(r.recentSpeeches, ev)
	if len(r.recentSpeeches) > recentSpeechBufferSize {
		r.recentSpeeches = r.recentSpeeches[len(r.recentSpeeches)-recentSpeechBufferSize:]
	}
	// 2026-08-05 §02 — 座位级「最后一次公开发言」,人机统一。
	// 只在**公开分支**记录:whisper 分支已在上方 return,私聊原文永不进入
	// 全房可见的 PlayerJSON.last_speech。
	// 观战者(seat<0 / isSpectator)不占座位卡,同样跳过。
	// 截断复用同包 agent_runner.go 的 truncate()(rune 安全),不另造轮子。
	if seat >= 0 && !isSpectator && strings.TrimSpace(msg.Text) != "" {
		if r.lastSpeechBySeat == nil {
			r.lastSpeechBySeat = make(map[int]seatSpeech, MaxPlayers)
		}
		r.lastSpeechBySeat[seat] = seatSpeech{
			Text: truncate(msg.Text, lastSpeechRuneLimit),
			AtMs: ts,
		}
	}
	// 2026-07-09 §13 增强 — 同步到 500K 队列:公开 chat 同步到所有 alive bot
	r.appendToChatQueueLocked(seat, -1, msg, isBot, isSpectator, false, ts)
	// 2026-08-10 §20260810-05 — 信息账本接入:公开发言全房存活座位知情。
	r.ledgerAppendLocked(InfoSourcePublicSpeech, msg.Text, aliveKnowerSetLocked(r), ts)
	logger.L().Debug("werewolf: speech recorded in room transcript",
		zap.String("room_id", r.RoomID),
		zap.Int("seat", seat),
		zap.Bool("is_bot", isBot),
		zap.String("agent_name", msg.FromAgentName),
		zap.Int("buf_len", len(r.recentSpeeches)))
}

func (r *WerewolfRoom) appendToChatQueueLocked(fromSeat, toSeat int, msg ChatMessageLike, isBot, isSpectator, isWhisper bool, ts int64) {
	if r.chatQueue == nil {
		return
	}
	tsTime := time.UnixMilli(ts)
	prefix := "p"
	if isWhisper {
		prefix = "w"
	}
	// whisper 不再需要 from-seat-based ID;使用 toSeat 标识收信方更稳定。
	idSeat := toSeat
	if !isWhisper || toSeat < 0 {
		idSeat = fromSeat
	}
	r.chatQueue.Append(agentcore.ChatMessage{
		ID:          msgIDForChat(prefix, r.RoomID, idSeat, ts),
		FromSeat:    fromSeat,
		FromID:      msg.FromUserID,
		AgentName:   msg.FromAgentName,
		FromAccount: msg.FromAccount,
		IsBot:       isBot,
		IsSpectator: isSpectator,
		IsWhisper:   isWhisper,
		ToSeat:      toSeat,
		Text:        msg.Text,
		Timestamp:   tsTime,
		// 2026-07-11 §126 增强:标注消息所属轮次,让 ChatHistoryQueue
		// 按 round 聚合压缩(早期消息淘汰时优先淘汰最早 round)。
		// GameState 用 DayNumber(不是 Round)表示当前是第几天。
		Round: r.State.DayNumber,
	})
}

func (r *WerewolfRoom) appendActivityToChatQueueLocked(ev *ChatActivityEvent) {
	if r.chatQueue == nil || ev == nil {
		return
	}
	ts := ev.TS
	if ts == 0 {
		ts = time.Now().UnixMilli()
	}
	tsTime := time.UnixMilli(ts)
	fromAccount := ev.Icon + " " + ev.Text
	if ev.Icon == "" {
		fromAccount = ev.Text
	}
	fromID := "system:" + ev.EventKind
	r.chatQueue.Append(agentcore.ChatMessage{
		ID:           msgIDForChat("a", r.RoomID, 0, ts),
		FromSeat:     -1,
		FromID:       fromID,
		AgentName:    ev.EventKind,
		FromAccount:  fromAccount,
		IsBot:        false,
		IsSpectator:  false,
		IsWhisper:    false,
		Text:         ev.Text,
		Timestamp:    tsTime,
		IsActivity:   true,
		EventKind:    ev.EventKind,
		ActivityIcon: ev.Icon,
		// 2026-07-11 §126 增强:活动事件也带轮次(同 chat 消息)。
		// GameState 用 DayNumber(不是 Round)表示当前是第几天。
		Round: r.State.DayNumber,
	})
}

func msgIDForChat(prefix, roomID string, seat int, ts int64) string {
	return prefix + ":" + roomID + ":" + strconv.Itoa(seat) + ":" + strconv.FormatInt(ts, 10)
}

