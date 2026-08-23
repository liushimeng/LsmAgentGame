// Package thpagent — chat_window.go: 牌桌闲聊窗口渲染(§3.1 德州扑克Agent聊天系统设计)。
//
// 德扑侧完全复用 agent/core.ChatHistoryQueue(500K per-room,四步压缩),本文件
// 只负责把 WindowFor(seat) 返回的增量消息渲染成注入 prompt 的文本段。
// 公平性硬约束:队列只收公屏消息(whisper 不进队列),窗口文本天然不含
// 任何 Hole 卡信息 —— 无需额外脱敏路径。
package thpagent

import (
	"fmt"
	"strings"

	agentcore "LsmAgentGame/agent/core"
)

// FormatChatWindow 把 WindowFor(seat) 的增量消息渲染为「牌桌闲聊(增量)」文本。
// 每条一行:"<发送者>: <文本>";bot 用 "Bot N号(model)",人类用昵称,活动事件跳过。
// 空/全空输入返回空串(调用方据此不注入该段)。
func FormatChatWindow(msgs []agentcore.ChatMessage) string {
	var b strings.Builder
	for _, m := range msgs {
		if m.IsWhisper {
			continue // 防御:德扑队列本不收 whisper,双保险跳过
		}
		text := strings.TrimSpace(m.Text)
		if text == "" {
			continue
		}
		who := m.FromAccount
		if who == "" {
			who = m.AgentName
		}
		if who == "" {
			if m.FromSeat >= 0 {
				who = fmt.Sprintf("%d号位", m.FromSeat+1)
			} else {
				who = "?"
			}
		}
		b.WriteString(fmt.Sprintf("%s: %s\n", who, text))
	}
	return b.String()
}

// TruncateChatWindowLines 把 FormatChatWindow 产出的多行文本截断为末尾 maxLines 行。
// maxLines ≤ 0 返回原文。供 §3.4 压缩梯度 Tier60(20 行)/Tier100(5 行)使用。
func TruncateChatWindowLines(w string, maxLines int) string {
	if maxLines <= 0 || w == "" {
		return w
	}
	lines := strings.Split(strings.TrimRight(w, "\n"), "\n")
	if len(lines) <= maxLines {
		return w
	}
	return strings.Join(lines[len(lines)-maxLines:], "\n") + "\n"
}
