// Package wwplayer — agent_speak.go: 发言计数器/私聊限流/插话限流/聊天队列方法。
// 从 agent.go 拆分出来,单文件 ≤ 1800 行硬约束(CLAUDE.md §4)。
package wwplayer

import (
	"sync"
	"time"

	"LsmAgentGame/agent/core"
)

// speakCounterState 60s 滑动窗口状态,内部 mutex 与 a.mu 分离。
//
// 每条窗口独立的 mutex 防止与 events channel 切换争抢。
type speakCounterState struct {
	mu          sync.Mutex
	windowStart time.Time // 60s 窗口起点
	count       int       // 窗口内 speak/interject 累计
}
// ---------- 2026-07-09 §13 增强:Speak Counter + Chat History Queue 接入 ----------

// ChatQueue 返回 agent 的 agentcore.ChatHistoryQueue(由 WerewolfRoom.StartAgentsLocked
// 注入)。调用方(appendRoomMessage)据此 push 消息。返回 nil 表示 chatQueue 未启用。
func (a *Agent) ChatQueue() *agentcore.ChatHistoryQueue {
	return a.chatQueue
}

// ChatCap 返回 chatQueue 的容量上限(由 SetChatQueue 时锁定);若 chatQueue
// 尚未注入,返回 0。BUG FIX 2026-07-09 §13.6: 占位场景需要该值以便前端
// summary 行展示 "0KB / 500KB"。
func (a *Agent) ChatCap() int {
	return a.chatCap
}

// StartedAt 返回 Agent 的构造时间(由 NewWithRoom 打点)。BUG FIX
// 2026-07-09 §13.6: 占位场景(BotTranscript() == nil)使用该时间戳作为
// UpdatedAt,使前端能区分"刚启动未发言"和"长时间未刷新"。
func (a *Agent) StartedAt() time.Time {
	return a.startedAt
}
// SetChatQueue 注入 agentcore.ChatHistoryQueue 并记录容量上限。在 NewWithRoom 之后由
// WerewolfRoom.StartAgentsLocked 调用一次,设置后整个房间生命周期不变。
func (a *Agent) SetChatQueue(q *agentcore.ChatHistoryQueue) {
	a.chatQueue = q
	if q != nil {
		a.chatCap = q.CapBytes()
	}
}
// recordSpeakDaytime 把当前时间戳记录进 60s 滑动窗口(speak / interject
// 成功后由 Run 调用)。窗口超过 60s 时自动重置。
func (a *Agent) recordSpeakDaytime(now time.Time) {
	a.speakCounter.mu.Lock()
	defer a.speakCounter.mu.Unlock()
	if a.speakCounter.windowStart.IsZero() || now.Sub(a.speakCounter.windowStart) > 60*time.Second {
		a.speakCounter.windowStart = now
		a.speakCounter.count = 0
	}
	a.speakCounter.count++
}

// allowSpeakDaytime 报告当前窗口是否允许再发一次,并返回当前累计次数。
//
// 返回 (allowed, currentCount):
//   - allowed = true → 当前可继续 speak(interject/whisper 走自己的 limiter)
//   - allowed = false → 已达上限;但 speak floor watchdog 在 ≤ 强制唤醒时
//     可以绕过此判断(speak_floor_tick 路径强制要求 LLM 调一次 speak)
//
// 调用方负责决定是否放行(speak_floor_tick 路径会忽略返回值强制调 LLM)。
func (a *Agent) allowSpeakDaytime(now time.Time) (allowed bool, currentCount int) {
	a.speakCounter.mu.Lock()
	defer a.speakCounter.mu.Unlock()
	if a.speakCounter.windowStart.IsZero() || now.Sub(a.speakCounter.windowStart) > 60*time.Second {
		// 窗口已过期 → 等价于"全新窗口,可发"
		return true, 0
	}
	return true, a.speakCounter.count // 总是 allowed=true;上限检查由 manager watchdog 处理
}

// NoteIfSpeaking 是 recordSpeakDaytime 的便捷包装,使用当前时间戳。
// BUG 2026-07-09: 遗言(last_words)视为一次公开发言,计入 speakCounter 滑动窗口,
// 避免 speak floor watchdog 在遗言阶段误 wake 正在发言的 bot。
func (a *Agent) NoteIfSpeaking() {
	a.recordSpeakDaytime(time.Now())
}

// AllowSpeakDaytimePublic 公开版本,供外部包(werewolf/speak_floor.go)调用,
// 避免反向依赖。语义与 allowSpeakDaytime 完全一致。
func (a *Agent) AllowSpeakDaytimePublic(now time.Time) (allowed bool, currentCount int) {
	return a.allowSpeakDaytime(now)
}

// snapshotSpeakCounter 返回当前窗口累计次数(用于 BotTranscript 展示)。
func (a *Agent) snapshotSpeakCounter() int {
	a.speakCounter.mu.Lock()
	defer a.speakCounter.mu.Unlock()
	if a.speakCounter.windowStart.IsZero() || time.Since(a.speakCounter.windowStart) > 60*time.Second {
		return 0
	}
	return a.speakCounter.count
}

// AllowInterject R76 P1-3 (2026-07-10): 检查单 bot 是否可以继续插话。
// 双层门:
//  1. InterjectLimiter 60s 最小间隔(> speak 45s,确保插话比正式发言慢)
//  2. 5 分钟滑动窗口 ≤ interjectMaxPer5Min(默认 4) 条/窗,防"程序化刷屏"
//
// 调用方在调 `interject` 工具前先 check;不通过时 runner 返回带 reason 的
// result(类似 BUG-R74-1 的 rate-limited 文案),LLM 在下一轮收敛。
func (a *Agent) AllowInterject(now time.Time) bool {
	if a.InterjectLimiter != nil && !a.InterjectLimiter.Allow() {
		return false
	}
	a.interjectMu.Lock()
	defer a.interjectMu.Unlock()
	if a.interjectWindowStart.IsZero() || now.Sub(a.interjectWindowStart) > 5*time.Minute {
		return true // 窗口已过期,等价全新窗口
	}
	return a.interjectWindowCount < interjectMaxPer5Min
}

// MarkInterject R76 P1-3 (2026-07-10): 配合 AllowInterject,登记一次插话。
func (a *Agent) MarkInterject(now time.Time) {
	if a.InterjectLimiter != nil {
		a.InterjectLimiter.Mark()
	}
	a.interjectMu.Lock()
	defer a.interjectMu.Unlock()
	if a.interjectWindowStart.IsZero() || now.Sub(a.interjectWindowStart) > 5*time.Minute {
		a.interjectWindowStart = now
		a.interjectWindowCount = 1
		return
	}
	a.interjectWindowCount++
}
