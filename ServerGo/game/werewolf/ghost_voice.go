// Package werewolf — ghost_voice.go: 死后幽灵语音 (§20260811-07 U1)。
//
// 设计要点:
//   - 死亡瞬间一次性推送一条结构化 Activity 事件(§119 协议层隔离);
//   - Bot 死者取 HeartThought 截断 80 字,做 4 项关键词 redact;
//   - 人类死者仅推送 "进入幽灵模式"(无 HeartThought 来源不自动生成);
//   - 同座位 1 次上限(防御性兜底,理论上 EmitPlayerDied 仅在 Alive 切换时触发);
//   - 全部调用方在持锁态被触发(§92a),不新增独立锁,反向依赖 r.mu。
//
// 风险约束:
//   §119 协议层隔离:活动事件走 chatQueue.IsActivity=true,不入 chat_message 表;
//   §135 身份公开:身份未公开座位 → "我是 X" 句式替换为 [身份隐去];
//   §130 接线验证:仅 1 个生产注入点(EmitPlayerDied 末尾)。
package werewolf

import (
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// ActivityEventKindGhostVoice 是 §20260811-07 U1 新增的活动事件类型。
// 前端 GameChatPanel 据此分支渲染紫色背景 + 👻 icon tooltip。
const ActivityEventKindGhostVoice = "ghost_voice"

// MaxGhostVoiceRunes 是幽灵语音最大 rune 数(§20260811-07 U1 设计 80 字)。
const MaxGhostVoiceRunes = 80

// ghostVoiceBannedKeywords 是 redactGhostVoice 必须移除的 4 项关键词子串。
// §119 + §135:这些字符串在 HeartThought 中泄漏会让观众/玩家绕过 BotTranscript
// 的协议层隔离或推断工具调用/身份细节。
var ghostVoiceBannedKeywords = []string{
	"internal_thought",
	"speak_with_thought",
	"heart_thought",
	"tool_use",
	"tool_result",
	"id=",
	"name=",
}

// ghostVoiceRoleLeakRegex 匹配"我是 X"句式(X = 已知身份)。
// §135 身份公平性:身份未公开座位不允许在幽灵语音中暴露身份。
var ghostVoiceRoleLeakRegex = regexp.MustCompile(`我是[\s\S]{0,6}(?:预言家|女巫|猎人|守卫|白痴|村民|狼王|普通狼)`)

// EmitGhostVoiceLocked 在 EmitPlayerDied 末尾追加调用(持锁态,§92a)。
//
// 输入参数:
//   - r: 房间;若为 nil 直接返回(nil-safe);
//   - seat: 死亡座位(0-indexed);越界直接返回;
//
// 行为:
//   - 若座位已在 ghostVoiceEmitted 集合中(防御性兜底),直接返回(幂等);
//   - 若 Bot 死者(r.botContexts[seat] 有 HeartThought):截断 + redact + emit;
//   - 若人类死者:仅推送"👻 N号 进入幽灵模式"提示;
//
// 不广播 chat_message 表;仅 emitActivity 走 chatQueue.IsActivity=true 路径。
func (m *WerewolfManager) EmitGhostVoiceLocked(r *WerewolfRoom, seat int) {
	if m == nil || m.activityEmitter == nil || r == nil || r.State == nil {
		return
	}
	if seat < 0 || seat >= MaxPlayers {
		return
	}
	// 幂等兜底:同座位不重复推送。
	if r.ghostVoiceEmitted == nil {
		r.ghostVoiceEmitted = make(map[int]bool, MaxPlayers)
	}
	if r.ghostVoiceEmitted[seat] {
		return
	}
	r.ghostVoiceEmitted[seat] = true

	raw := extractHeartThoughtForGhostLocked(r, seat)
	icon := "👻"
	label := "幽灵语音"
	text := ""
	if raw == "" {
		// 人类死者(无 HeartThought)或 Bot 无内心独白 → 仅推送进入幽灵模式。
		text = strconv.Itoa(seat+1) + "号 进入幽灵模式"
		label = "幽灵模式"
	} else {
		text = redactGhostVoice(raw, seat, r)
	}
	fullText := icon + " " + strconv.Itoa(seat+1) + "号 " + label + ": " + text
	m.emitActivity(r, ActivityEventKindGhostVoice, fullText,
		r.State.Phase.String(), r.State.DayNumber,
		"info", icon, seat, -1, false)
}

// extractHeartThoughtForGhostLocked 取 Bot 死者的 HeartThought(锁内读)。
// 人类死者或越界返回 ""。
//
// §92a:调用方必须已持 r.mu(EmitPlayerDied 链路);不获取新锁。
//
// 数据来源:r.BotAgents[seat].BotTranscript.HeartThought(§119 协议层隔离
// 已经在源头控制,这里仅做读)。
func extractHeartThoughtForGhostLocked(r *WerewolfRoom, seat int) string {
	if r == nil || seat < 0 || seat >= MaxPlayers {
		return ""
	}
	ag, ok := r.BotAgents[seat]
	if !ok || ag == nil {
		return ""
	}
	// wwplayer.Agent.BotTranscript() 返回实时结构体指针,字段读取无并发问题
	// (内部受 a.mu 保护,这里是快照式读取)。
	return ag.BotTranscript().HeartThought
}

// redactGhostVoice 把可能泄漏身份/工具调用细节的子串移除 + 截断到 80 字。
// §119 + §135:身份公开判定走 RolePubliclyRevealed 单点;此处做防御性 redact。
func redactGhostVoice(raw string, seat int, r *WerewolfRoom) string {
	if raw == "" {
		return ""
	}
	// 1. 截断 80 字(按 rune 计数,避免中文字符被截半)。
	if utf8.RuneCountInString(raw) > MaxGhostVoiceRunes {
		runes := []rune(raw)
		raw = string(runes[:MaxGhostVoiceRunes]) + "…"
	}
	// 2. 移除工具名/internal 关键词(全部用 · 替换,保留可读性)。
	for _, b := range ghostVoiceBannedKeywords {
		raw = strings.ReplaceAll(raw, b, "·")
	}
	// 3. §135:身份未公开座位 → "我是 X" 句式替换为 [身份隐去]。
	if r != nil && r.State != nil && !r.State.RolePubliclyRevealed(Seat(seat)) {
		raw = ghostVoiceRoleLeakRegex.ReplaceAllString(raw, "[身份隐去]")
	}
	return strings.TrimSpace(raw)
}

// ResetGhostVoiceEmittedLocked 清空 ghostVoiceEmitted(restartGameLocked 重开局时调用)。
// §20260811-07 U1 — 同款 cipher/rumor 重置模式,防止跨局残留。
//
// §92a:调用方必须已持 r.mu。
func (r *WerewolfRoom) ResetGhostVoiceEmittedLocked() {
	if r == nil {
		return
	}
	r.ghostVoiceEmitted = nil
}

// HasGhostVoiceEmittedLocked 返回 seat 是否已推送幽灵语音(测试用/debug)。
// §92a:调用方必须已持 r.mu。
func (r *WerewolfRoom) HasGhostVoiceEmittedLocked(seat int) bool {
	if r == nil || r.ghostVoiceEmitted == nil {
		return false
	}
	return r.ghostVoiceEmitted[seat]
}

// GhostVoiceEmittedSnapshot 返回 ghostVoiceEmitted 的快照(非锁内路径,仅供测试/debug)。
func (r *WerewolfRoom) GhostVoiceEmittedSnapshot() map[int]bool {
	if r == nil || r.ghostVoiceEmitted == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[int]bool, len(r.ghostVoiceEmitted))
	for k, v := range r.ghostVoiceEmitted {
		out[k] = v
	}
	return out
}

// ghostVoiceTimestamp 仅用于日志/测试断言的占位字段,避免 import 抖动。
// §20260811-07 U1 — 显式占位说明此文件是 §92a 持锁路径的纯增量,
// 与既有 activity_emitter.go 共用 emitActivity nil-safe 包装。
var _ = time.Now
