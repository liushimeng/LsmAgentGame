// Package wwplayer — agent_quarantine.go: 隔离区(quarantine)相关方法。
// 从 agent.go 拆分出来,单文件 ≤ 1800 行硬约束(CLAUDE.md §4)。
package wwplayer

import (
	"fmt"
	"time"
)

// IsQuarantined reports whether this agent has been permanently quarantined
// (its LLM provider is broken for the rest of the game). Theroom manager uses
// this to emit a silent skip action for the bot's turn without waking the
// agent goroutine — avoiding infinite auto-skip / scheduleReWake loops.
// BUG-WEREWOLF-P0-NEW-3.
func (a *Agent) IsQuarantined() bool {
	a.Lock()
	defer a.Unlock()
	return a.quarantined
}

// SetQuarantined marks this agent permanently broken for the rest of the
// game AND cancels any pending scheduleReWake timer. After this returns the
// agent will not receive further re-wake events for prior failures, even if
// the reWake goroutine already fired a time.After. Safe to call from any
// goroutine; idempotent. BUG-WEREWOLF-P0-NEW-4 (Round 24): without the
// cancel, a quarantined agent kept receiving delayed reWakes that re-entered
// handleEvent, called the LLM again, failed again, and re-set quarantine —
// logging duplicate "quarantined" lines and flooding the in-memory
// BotAgents map with dead wakes until the goroutine finally timed out.
//
// BUG-WEREWOLF-P0-NEW-27 (Round 34): after setting quarantine, fire the
// onQuarantine callback (in a goroutine) so the room manager can immediately
// dispatch the phase's safe skip for a quarantined acting bot. Without this
// notification, the manager only learns about quarantine on the next wake
// event — which may never come if the bot is the current speaker and no
// other event source pushes a wake.
func (a *Agent) SetQuarantined() {
	a.Lock()
	prev := a.reWakeCancel
	a.quarantined = true
	a.reWakeCancel = nil
	cb := a.onQuarantine
	// 2026-07-12 §127 — 生成一条系统广播消息,让人类/观众立即看到该 bot 被禁用。
	a.quarantineBroadcast = fmt.Sprintf("⚠️ %d号Agent(%s)因连续调用失败被禁用,后续由系统代为操作。", a.Seat+1, a.ModelKey)
	a.Unlock()
	if prev != nil {
		prev() // cancel any in-flight scheduleReWake
	}
	// BUG-WEREWOLF-P1-NEW-46 (Round 39): the bot just stopped calling the
	// LLM forever. Publish a refreshed BotTranscript so the spectator
	// HistoryDrawer(🤖独白 sub-tab) shows the "已禁用" badge instead of going blank
	// until the next memory-driven snapshot fires (which it never will).
	a.publishQuarantineTranscript()
	if cb != nil {
		go cb()
	}
}

// SetOnQuarantine registers a callback that fires (in a goroutine) when the
// agent transitions to quarantined state. Called once at agent construction
// by the room manager. BUG-WEREWOLF-P0-NEW-27.
func (a *Agent) SetOnQuarantine(cb func()) {
	a.Lock()
	a.onQuarantine = cb
	a.Unlock()
}

// SetOnTranscriptPublished registers a callback that fires (in a goroutine)
// after recordTranscript publishes a fresh BotTranscript snapshot. The room
// manager wires it to broadcast game.state for real-time emotion sync.
// Called once at agent construction by the room manager.
func (a *Agent) SetOnTranscriptPublished(cb func()) {
	a.Lock()
	a.onTranscriptPublished = cb
	a.Unlock()
}

// ResetConsecutiveFailures clears the failure counter after a successful LLM
// call — also clears quarantine if it was set on a transient blip that the
// model recovered from. Safe to call from handleEvent after any successful
// provider.Chat response. BUG-WEREWOLF-P0-NEW-3.
func (a *Agent) ResetConsecutiveFailures() {
	a.Lock()
	defer a.Unlock()
	a.consecutiveFailures = 0
	a.quarantined = false
	// BUG-WEREWOLF-P1-NEW-46 (Round 39): the model came back; clear the
	// stale error so a subsequent quarantine (if any) records the new
	// failure, not the long-cleared previous one.
	a.lastError = ""
	// BUG-R48-P0-1: 重置失败时间, 让下次失败重新开始冷却窗口计时。
	a.lastFailureTime = time.Time{}
	// 2026-07-10 §重构 — 成功调用后清场相位/retry/error class。
	// phase 留 "idle",retry 计数清零,lastErrorClass 重置为 "none"。
	a.llmCallPhase = PhaseIdle
	a.retryAttempt = 0
	a.retryMaxAttempts = 0
	a.nextRetryAtMs = 0
	a.lastErrorClass = "none"
	// 2026-07-29 优化:成功调用后重置 speak idle 计数器。
	// 2026-08-04 §重构 — emotionSwitchAloneCount 字段已删除。
	a.speakTurnIdleSilentCount = 0
	// BUG-R232-P1-02 (2026-08-02): 模型恢复后清零熔断失败计数器,
	// 下次熔断打开时重新从 0 开始计数与日志降噪。
	a.circuitOpenFailureCount = 0
}

// ConsecutiveFailures 返回当前连续 LLM 调用失败次数。
// 道具系统用此判断目标是否"心态崩了"（>2 时中招率 +10%）。
// 并发安全：持 a.mu 读。
func (a *Agent) ConsecutiveFailures() int {
	a.Lock()
	defer a.Unlock()
	return a.consecutiveFailures
}

// RecordFailure 处理一次 LLM 调用失败，更新 consecutiveFailures 与
// lastFailureTime（持 a.mu 写）。返回 (新的 consecutiveFailures,
// 是否处于 cooldown 窗口内 —— 即本次未递增 / 未更新 lft 但被窗口吸收)。
//
// 语义：
//   - network/timeout transient：bump lastFailureTime 滑动 cooldown 窗口，
//     不递增 consecutiveFailures（避免慢模型/上游抖动被永久 quarantine）。
//     但 transient 仍算"进入冷却"，返回 inCooldown=true。
//   - 其它错误且在 cooldown 窗口内：既不递增、也不动 lastFailureTime
//     （与原行为兼容）。inCooldown=true。
//   - 其它错误且超出 cooldown：递增 + 重置 lastFailureTime。inCooldown=false。
//
// FIX (R186-A): 之前 run.go:746-762 直接 `a.consecutiveFailures++` /
// `a.lastFailureTime = now`，与其它路径（ResetConsecutiveFailures / ConsecutiveFailures /
// SetQuarantined / recordTranscript）持 a.mu 形成 data race，Go race detector
// 会 flag。本次修复把所有 mutation 收敛到持锁的 helper。
//
// 同时修正一个语义 bug：transient 错误之前完全不更新 lastFailureTime，导致一个
// bot 持续 timeout 5 分钟后再撞 403 时 cooldown 早已过期、单次 403 立即计数。
// 现在 transient 也滑动 cooldown 窗口，让失败序列在窗口内持续累计。
func (a *Agent) RecordFailure(now time.Time, transient bool, window time.Duration) (newCF int, inCooldown bool) {
	a.Lock()
	defer a.Unlock()
	if transient {
		// Transient 错误只滑动 cooldown 窗口,不递增计数。但视作已进入
		// 冷却(下一次非 transient 失败会被吸收)。
		a.lastFailureTime = now
		return a.consecutiveFailures, true
	}
	if !a.lastFailureTime.IsZero() && now.Sub(a.lastFailureTime) < window {
		return a.consecutiveFailures, true
	}
	a.consecutiveFailures++
	a.lastFailureTime = now
	return a.consecutiveFailures, false
}

// FailureSnapshot 持 a.mu 一次性读 consecutiveFailures 与 quarantined，
// 用于 run.go:798-801 quarantine 检查时不被 ResetConsecutiveFailures 并发清零撕裂。
func (a *Agent) FailureSnapshot() (int, bool) {
	a.Lock()
	defer a.Unlock()
	return a.consecutiveFailures, a.quarantined
}

// SetLastError records the most recent LLM provider error so the
// BotTranscript.QuarantineReason field can show it on the spectator panel.
// Truncated to 240 chars here so a runaway upstream error string cannot
// blow up the wire payload. BUG-WEREWOLF-P1-NEW-46 (Round 39).
func (a *Agent) SetLastError(msg string) {
	a.Lock()
	defer a.Unlock()
	a.lastError = truncate(msg, 240)
}
