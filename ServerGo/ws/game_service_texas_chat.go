// game_service_texas_chat.go — 德扑 per-room 500K 共享聊天队列(§3.1
// 德州扑克Agent聊天系统设计,2026-08-23)。
//
// 完全复用 agent/core.ChatHistoryQueue(cap 500KB,四步压缩)。
//   - 写入点 1: RecordTexasRoomChat — main.go SetRoomMessageHook 转发人类
//     房间公屏消息(whisper 不进队列);
//   - 写入点 2: RecordTexasBotChat — sendBotChat 成功后写入 bot 发言;
//   - 读取点:   TexasChatWindow / AdvanceTexasChat — ProcessBotTurn 注入
//     「牌桌闲聊(增量)」段(WindowFor(seat) + Advance);
//   - 可见性:   TexasChatPreview — view 层 chat_window_preview(最近 5 条公屏);
//   - 清理点:   cleanupTexasHoldemBotRuntime 房间删除时删除队列。
package ws

import (
	"fmt"
	"time"

	agentcore "LsmAgentGame/agent/core"
	"LsmAgentGame/agent/thpagent"
)

// texasChatQueue 返回(必要时创建)roomID 的共享聊天队列(500K 上限)。
func (s *GameService) texasChatQueue(roomID string) *agentcore.ChatHistoryQueue {
	v, _ := s.thpChatQueues.LoadOrStore(roomID, agentcore.NewChatHistoryQueue(agentcore.DefaultChatHistoryCapBytes))
	return v.(*agentcore.ChatHistoryQueue)
}

// RecordTexasRoomChat 把一条房间公屏消息(人类玩家/其他来源)写入德扑队列。
// 由 main.go 的 ChatService.SetRoomMessageHook 转发(§3.1 写入点 1)。
// whisper 一律不进队列(公平性硬约束:窗口只含公屏消息)。
//
// 只写**已存在**的队列(由 registerTexasHoldemAgentSeats 预热创建)——
// 该 hook 对全游戏房间消息触发,若此处 LoadOrStore 会在狼人杀/其他游戏房间
// 也创建德扑队列且无清理点,造成内存泄漏(§20260822-01 janitor 教训同类)。
func (s *GameService) RecordTexasRoomChat(roomID, fromUserID, fromAccount, text string, whisper bool) {
	if roomID == "" || whisper || text == "" {
		return
	}
	v, ok := s.thpChatQueues.Load(roomID)
	if !ok {
		return // 非德扑 bot 房间(或尚未注册 agent)— 不创建
	}
	v.(*agentcore.ChatHistoryQueue).Append(agentcore.ChatMessage{
		FromSeat:    -1,
		FromID:      fromUserID,
		FromAccount: fromAccount,
		IsBot:       false,
		Text:        text,
		Timestamp:   time.Now(),
	})
}

// RecordTexasBotChat 把 bot 公屏发言写入德扑队列(§3.1 写入点 2,
// 与狼人杀 emitRoomMessage 对齐 —— SendFromBot 自身也会触发 hook,但德扑
// hook 转发只投人类消息段;bot 消息在此显式写入,带座位号)。
func (s *GameService) RecordTexasBotChat(roomID string, seat int, botUserID, modelKey, text string) {
	if roomID == "" || text == "" {
		return
	}
	q := s.texasChatQueue(roomID)
	q.Append(agentcore.ChatMessage{
		FromSeat:    seat,
		FromID:      botUserID,
		AgentName:   modelKey,
		FromAccount: fmt.Sprintf("Bot %d号", seat+1),
		IsBot:       true,
		Text:        text,
		Timestamp:   time.Now(),
	})
}

// TexasChatWindow 取指定 bot 座位「自上次消费后」的增量公屏窗口并渲染。
// 队列不存在/无新消息返回空串(prompt 据此不注入该段)。
// 只读,不推进 read pointer —— AdvanceTexasChat 在决策完成后调用。
func (s *GameService) TexasChatWindow(roomID string, seat int) string {
	v, ok := s.thpChatQueues.Load(roomID)
	if !ok {
		return ""
	}
	q := v.(*agentcore.ChatHistoryQueue)
	return thpagent.FormatChatWindow(q.WindowFor(seat))
}

// AdvanceTexasChat 把指定座位的 read pointer 推到队列末尾(决策完成后调用,
// 下次 WindowFor 只返回更新的部分)。失败静默(队列已删/不存在)。
func (s *GameService) AdvanceTexasChat(roomID string, seat int) {
	v, ok := s.thpChatQueues.Load(roomID)
	if !ok {
		return
	}
	q := v.(*agentcore.ChatHistoryQueue)
	q.Advance(seat, q.SnapshotLastSeq())
}

// TexasChatPreview 返回最近 5 条公屏消息的预览(供 view 层
// chat_window_preview 字段,§3.5:公屏消息本就是公开信息,无敏感内容)。
// 队列不存在时返回 nil(字段 omitempty,不污染旧客户端)。
func (s *GameService) TexasChatPreview(roomID string) []string {
	v, ok := s.thpChatQueues.Load(roomID)
	if !ok {
		return nil
	}
	q := v.(*agentcore.ChatHistoryQueue)
	msgs := q.Tail(5)
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		if m.IsWhisper || m.Text == "" {
			continue
		}
		who := m.FromAccount
		if who == "" {
			who = m.AgentName
		}
		if who == "" {
			who = "?"
		}
		out = append(out, who+": "+m.Text)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// cleanupTexasChatQueue 房间删除时清理德扑聊天队列(§5 接线清单:
// thpChatQueues 房间删除清理点)。
func (s *GameService) cleanupTexasChatQueue(roomID string) {
	s.thpChatQueues.Delete(roomID)
}
