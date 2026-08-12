// Package agent — run.go: the Agent.Run decision loop.
//
// Implements the Phase 4 main loop from docs/狼人杀-Agent与系统/狼人杀Agent设计.md §9:
// block on the per-seat events channel, build an LLM request (system + memory +
// current wwtypes.GameContext + phase-appropriate tools), send it, and dispatch any
// tool_use blocks via ToolRunner. Loop runs until the room tears down (ctx
// cancel) or the game ends (PhaseGameOver event).
//
// `toolsFor` narrows the offered tool set to what's legal for this seat right
// now, mirroring the phase-visibility rules in tools.go::BuildTools.
package wwplayer

import (
	"LsmWebGame/config"
	agentroot "LsmWebGame/agent"
	"LsmWebGame/agent/core"
	"LsmWebGame/agent/wwtypes"
	"LsmWebGame/llm"
	"LsmWebGame/llm/anthropic"
	llmtypes "LsmWebGame/llm/types"
	"LsmWebGame/logger"
	"context"
	"errors"
	"fmt"
	"go.uber.org/zap"
	"strings"
	"time"
)

// ─── §20260810-13 内层循环上限 + 单次行动退出 ───
//
// phaseMaxInnerRounds 按阶段配置内层决策循环的最大轮次，防止 LLM 陷入无意义
// 工具调用循环（如 wolf_kill 成功后仍反复尝试、wolf_whisper 不停协商等）。
// 每轮 = 一次 LLM 调用。默认值 defaultMaxInnerRounds = 5。
//
// 设计原则:
//   - 夜间行动类（一次性动作）: 3 轮（1 次主行动 + 最多 2 次修正/讨论）
//   - 投票类: 3 轮
//   - 发言类: 5 轮（发言 + 道具 + 情绪 + 收尾）
//   - 其他阶段: 默认 5 轮
const defaultMaxInnerRounds = 5

var phaseMaxInnerRounds = map[string]int{
	// 夜间行动类（一次性）
	"PhaseNightWolves":     3,
	"night_wolves":         3,
	"PhaseNightSeer":       3,
	"night_seer":           3,
	"PhaseNightWitch":      3,
	"night_witch":          3,
	"PhaseNightGuard":      3,
	"night_guard":          3,
	"PhaseNightDemonHunter": 3,
	"night_demon_hunter":    3,
	// 投票类
	"PhaseVote":    3,
	"vote":         3,
	"PhaseSheriff": 3,
	"sheriff":      3,
	// 单次行动
	"PhaseHunterShoot": 2,
	"hunter_shoot":     2,
	"PhaseIdiotReveal": 2,
	"idiot_reveal":     2,
	"PhaseDeathLyric":  3,
	"death_lyric":      3,
}

func maxInnerRoundsForPhase(phase string) int {
	if n, ok := phaseMaxInnerRounds[phase]; ok {
		return n
	}
	return defaultMaxInnerRounds
}

// isSingleActionTool 判断工具是否为"单次行动工具"——调用成功后本阶段行动即结束，
// 应立即退出内层循环以避免无意义的后续 LLM 调用。
// §20260810-13: 这些工具不触发 saidSomething（语义是"公开发言"），所以原本
// 的 inner loop 会继续下一轮；通过 actionDone 标志位强制退出。
func isSingleActionTool(name string) bool {
	switch name {
	case "wolf_kill",
		"seer_check",
		"witch_act",
		"guard_protect",
		"demon_hunter_hunt",
		"hunter_shoot":
		return true
	}
	return false
}

// maxWait caps how long Run waits for the speak limiter before giving up and
// skipping the speak attempt. Prevents the agent goroutine from blocking
// indefinitely on a congested limiter.
const maxSpeakWait = 5 * time.Second

// 2026-07-15 R131 修复: 三处(注释/applyDefaults/fallback)统一为 120s。
// 12s 慢模型(DeepSeek/GLM)典型 + 5 次外层重试(1+2+4+8+8 cap=23s)
// + 5 次内层 backoff(0.5+1+2 = 3.5s) ≈ 30s 累计仍有 90s 余量给真正 LLM 调用。
// 2026-07-24 优化: 120 → 300 (5 min)。慢模型(Kimi/GLM)单次响应 2-5 分钟,
// 120s 把正常慢调用 cancel 推入 quarantine;lenient ×150% = 450s ≈ 7.5min,
// 仍在 llm.timeout_ms=600s 预算内。
const defaultLLMCallTimeoutSec = 300

// 2026-07-24 §流式续命 — 首字节到达后,作为最终兜底的总超时(默认 15 min)。
// 取代旧"硬上限"为"分阶段预算":
//   - 首字节前(cfgLLMCallTimeoutSec):300s 基础 / 480s 上限,慢启动熔断。
//   - 首字节后(cfgStreamExtendedTimeoutSec):默认 900s,作为整次 LLM 调用的最终截止,
//     给慢模型(Kimi/GLM/DeepSeek 典型首字节 1-3min + 长 thinking + tool_use
//     总耗时 5-15min)足够预算,避免外层 ctx cancel → consecutiveFailures++ → 误 quarantine。
//
// 0 / 未配置 = 走代码内常量 defaultStreamExtendedTimeoutSec(900)。
const defaultStreamExtendedTimeoutSec = 900

// cfgStreamExtendedTimeoutSec 读取首字节后总超时(秒),配置缺失或 panic 时兜底到常量。
// 2026-07-24 §流式续命:与 cfgLLMCallTimeoutSec 配对使用,前者是首字节前短预算,
// 后者是首字节后长预算。两者相加 = 单次 LLM 调用的最终最大耗时上限。
func cfgStreamExtendedTimeoutSec() int {
	defer func() { _ = recover() }()
	v := config.Load().Werewolf.LLMStreamExtendedTimeoutSec
	if v > 0 {
		return v
	}
	return defaultStreamExtendedTimeoutSec
}

// cfgLLMCallTimeoutSec 读取单次 LLM 调用总超时(秒,含所有重试)。
// 2026-07-15 R131 修复: 默认 120s;大房间(>=LenientModeForSeatCount)启用
// LLMTimeoutScalePercent 缩放,默认 150% => 180s,给 13 并发/慢模型更久等待。
// 2026-07-24 优化: 默认 300s,lenient ×150% => 450s(上限同步 300→480,
// 留出 30s buffer 给 ctx 传递)。
// 2026-08-01 修复(报告 20260801_083235):**永不返回 0**。
// 旧写法用匿名返回值 + `defer recover()`,config.Load() panic 时函数静默
// 返回零值 0,而调用方把 0 解读为"不强制超时"→ 每一次 LLM 调用都变成
// 无界调用(既不超时也不进重试/quarantine,goroutine 永久挂住)。
// 现改为**具名返回值** out,recover 时显式回填 defaultLLMCallTimeoutSec,
// 与同族的 cfgStreamExtendedTimeoutSec 兜底语义对齐;末尾再兜一次
// `out <= 0` 防止未来 cfgLLMCallTimeoutSecScaled 返回非正值。
func cfgLLMCallTimeoutSec(seatCount int) (out int) {
	defer func() {
		if r := recover(); r != nil {
			out = defaultLLMCallTimeoutSec
			return
		}
		if out <= 0 {
			out = defaultLLMCallTimeoutSec
		}
	}()
	v := config.Load().Werewolf.LLMCallTimeoutSec
	base := defaultLLMCallTimeoutSec
	if v > 0 {
		base = v
	}
	cfg := config.Load().Werewolf
	return cfgLLMCallTimeoutSecScaled(base, seatCount, cfg.LenientModeForSeatCount, cfg.LLMTimeoutScalePercent)
}

// cfgLLMCallTimeoutSecWithFallback 提供无 seatCount 时的兜底调用(默认 13)。
// 2026-08-01 修复:与 cfgLLMCallTimeoutSec 同源缺陷(recover 后返回 0 =
// "无超时"),同样改为具名返回值 + 兜底 defaultLLMCallTimeoutSec。
func cfgLLMCallTimeoutSecWithFallback(seatCount int) (out int) {
	defer func() {
		if r := recover(); r != nil {
			out = defaultLLMCallTimeoutSec
			return
		}
		if out <= 0 {
			out = defaultLLMCallTimeoutSec
		}
	}()
	if seatCount <= 0 {
		seatCount = 13
	}
	v := config.Load().Werewolf.LLMCallTimeoutSec
	base := defaultLLMCallTimeoutSec
	if v > 0 {
		base = v
	}
	cfg := config.Load().Werewolf
	return cfgLLMCallTimeoutSecScaled(base, seatCount, cfg.LenientModeForSeatCount, cfg.LLMTimeoutScalePercent)
}

// llmCallBudgetSec 把两段预算(首字节前 callTimeout / 首字节后 extendedTimeout)
// 归一化并求和,产出单次 LLM 调用的**最终总预算**(秒),恒为正。
//
// 2026-08-01 修复(报告 20260801_083235):这是 run.go 构造 parentCtx 的唯一
// 事实来源。抽成函数有两个目的:
//   - 把"0 绝不等于无超时"的兜底收口到一处,避免调用点漏写守卫;
//   - 让回归测试能直接驱动生产逻辑,而不是在测试里复刻一份 if。
func llmCallBudgetSec(callTimeout, extendedTimeout int) int {
	if callTimeout <= 0 {
		callTimeout = defaultLLMCallTimeoutSec
	}
	if extendedTimeout <= 0 {
		extendedTimeout = defaultStreamExtendedTimeoutSec
	}
	return callTimeout + extendedTimeout
}

// cfgLLMCallTimeoutSecScaled 纯函数,便于 engine.go 复用而不重复解析 config。
// 2026-07-24 优化: 上限 300 → 480s。默认 base=300s、lenient ×150% = 450s
// 必须在 cap 内生效(旧 300s cap 会把 lenient 缩放裁掉 150s);480s 为
// 450s + 30s buffer,仍在 llm.timeout_ms=600s 预算内。
func cfgLLMCallTimeoutSecScaled(base, seatCount, lenientSeatCount, scalePercent int) int {
	if seatCount >= lenientSeatCount && scalePercent > 100 {
		scaled := base * scalePercent / 100
		if scaled > 480 {
			scaled = 480 // 上限 8min,避免无限等待;且 < llm.timeout_ms(600s)
		}
		return scaled
	}
	return base
}

// 2026-07-24 优化:线性退避表 — 取代原指数 1s/2s/4s/8s。
// 用户反馈 13 人局连续 5+ 次 LLM 失败被批量 quarantine,根因之一是
// 指数退避前 2 次间隔仅 1s/2s,上游代理判定为"热循环"持续 4xx/429 拒绝。
// 改为线性 2s/4s/6s/8s/8s:
//   - 第 1 次重试 2s:立即让上游喘息,避开 retry-storm 标签
//   - 第 2 次重试 4s:渐进,继续避开 hot-loop
//   - 第 3 次重试 6s:更长间隔,避免同秒并发回填触发限流
//   - 第 4 次起封顶 8s:不再继续加长,等 60s failCooldownWindow 接管
//
// 5 次累计 2+4+6+8+8 = 28s,远小于 cfgLLMCallTimeoutSec(2026-07-24 起默认 300s)。
var llmRetryLinearBackoff = []time.Duration{
	2 * time.Second,
	4 * time.Second,
	6 * time.Second,
	8 * time.Second,
	8 * time.Second,
}

// llmRetryMaxBackoff 单次 backoff 上限(8s),防止 retry-loop 累计耗时爆炸。
// 配合 5 次重试上限,即使走到 attempt 8+ 也稳定在 8s。
const llmRetryMaxBackoff = 8 * time.Second

// llmBackoffForAttempt 根据 attempt(1-based)查表返回线性 backoff;越界回退 cap。
// 取代旧公式 llmRetryBaseDelay * (1 << uint(attempt-1))。
// 显式查表比 1<<n 更可预测,也避免 1s 起步太快被上游代理拒绝。
func llmBackoffForAttempt(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	idx := attempt - 1
	if idx >= len(llmRetryLinearBackoff) {
		return llmRetryMaxBackoff
	}
	return llmRetryLinearBackoff[idx]
}

// thresholdForSeatCount 根据房间人数动态放宽 quarantine 阈值。
// 7 人局保持 maxConsecutiveFailures/permanentQuarantineThreshold;
// 13 人局每多 1 人增加约 1 次容忍,但设置上限防止无限宽松。
func thresholdForSeatCount(seatCount int) (maxFail, permanentFail int) {
	if seatCount <= 7 {
		return maxConsecutiveFailures, permanentQuarantineThreshold
	}
	extra := seatCount - 7
	if extra > 6 {
		extra = 6
	}
	return maxConsecutiveFailures + extra, permanentQuarantineThreshold + extra/2
}

// maxConsecutiveFailures caps how many wake cycles an agent tolerates before
// it gets quarantined for the rest of the game. A single permanently-broken
// LLM (e.g. 403 usage limit, 401 invalid key) used to loop forever: each
// wake → auto-skip dispatch failed (race with phase transition) → reWake →
// retry → failCount increments without bound (observed 40+ in Round 15).
//
// BUG-WEREWOLF-P0-NEW-3: once an agent hits this ceiling we mark it
// quarantined — the room manager then skips its turn in every subsequent
// phase (no LLM call, no reWake, no dispatch_noise). A quarantined agent
// emits the phase's transparent "skip" action when it's still the bot's
// turn so the engine keeps moving; after phase changes it simply yields
// without any further wakeup scheduling.
const maxConsecutiveFailures = 10 // R81 P0-1 修复: 5→10,大幅提高 quarantine 阈值，减少 transient error 导致的误 quarantine

// 2026-07-15 R131 修复: 永久错误(401/403)quarantine 阈值 2→4。
// 允许上游偶发 401/403(2 次瞬态 + 2 次真正挂)不立即 ban,
// 但 4 次仍失败 = 持续故障,值得 quarantine。永久错误也走 60s 冷却窗口(见下方 run.go:666-685)。
// 2026-07-24 优化:4→6,13 人局单局 7 次永久错误的概率极低,6 次阈值避免
// 单次上游抖动把整个房间 bot 批量送进 quarantine。配合 7 次重试 +
// 60s 冷却窗口,容忍窗口更宽。
const permanentQuarantineThreshold = 6

// BUG-R48-P0-1: 复述段落已压缩 — git blame 与 docs/ 索引可还原

const failCooldownWindow = 90 * time.Second

// reWakeDelay is how long the agent waits before self-re-waking after all LLM
// retries are exhausted. This prevents permanent deadlock when a transient
// failure (network blip, proxy hiccup) kills the only wake for this phase.
const reWakeDelay = 8 * time.Second

// circuitOpenMinReWakeDelay BUG-R232-P1-02 (2026-08-02): 当 LLM 调用失败是
// model_400_circuit 熔断器"开"状态(model 反复 400,window 内累计超阈值)
// 时,8s reWakeDelay 太短 —— R232 实测 seat 9 (Tencent-model) 在熔断期间
// 出现 106 次/分钟失败日志, 1 分钟内 7 次 scheduleReWake + 5 次内层 retry
// = 12 次 LLM 调用全部被熔断器快速失败, 日志淹没关键路径。
//
// 修复: 熔断"开"时把 reWake 间隔拉长到至少 30s(熔断 cooldown 是 120s,
// 30s × 4 ≈ 完整 cooldown 周期); 同时聚合日志(见 logEveryNCircuitOpen),
// 每 N 次失败合并成 1 条汇总 + 1 条 hint。
const circuitOpenMinReWakeDelay = 30 * time.Second

// circuitOpenLogEveryN BUG-R232-P1-02 (2026-08-02): 熔断"开"期间每 N 次失败
// 才输出一条 WARN 日志,其余 N-1 次降级到 Debug;避免 106 次/分钟的日志风暴
// 干扰关键路径排查(R232 报告里 P0 死锁日志被熔断日志淹没)。
const circuitOpenLogEveryN = 10

// llmSlotAcquireWait is the bounded wait for a room-level LLM concurrency
// slot (BUG-R242-P1-01). A bot that can't acquire a slot within this window
// backs off and reWakes (transient, no consecutiveFailures increment) rather
// than blocking indefinitely — so slow models holding all slots don't starve
// fast models. 5s catches slots freed by fast models finishing a call without
// making a fast model block for an entire slow-model response.
const llmSlotAcquireWait = 5 * time.Second

// failAutoSkipThreshold is the number of consecutive LLM failures (per agent,
// across wake cycles) before the agent auto-calls the phase's default skip
// action so the game can advance. BUG-WEREWOLF-P0-2 FIX: previously a broken
// LLM meant every wake cycle → retry exhaust → scheduleReWake → next wake →
// retry exhaust → ... forever, while the rest of the room sat idle. With
// this threshold the agent at least emits an empty wolf_kill /
// finish_speak / vote (whatever the phase's only legal action is) so the
// engine progresses. Reset to 0 on any successful LLM response.
//
// BUG-WEREWOLF-P0-NEW-1: previously 3, so a single broken model had to fully
// stall three consecutive wakes (~14min worst-case with the old 30s timeout +
// llmMaxRetries=3) before the phase advanced — the whole 7-seat game froze on
// one bot. Lowered to 1: the first fully-failed wake immediately dispatches the
// phase's safe skip action so the engine keeps moving while reWake still gives
// the model another chance next phase.
const failAutoSkipThreshold = 1

// SkipPhaseAction returns the (tool_name, target) tuple the agent (or room
// manager, for a quarantined bot) should call when the LLM is permanently
// broken and we need to keep the engine moving. Returns ("", 0) if no safe
// skip exists for the current phase (the caller yields and waits for the
// next wake instead of guessing).
//
// Exported (capital S) so the werewolf room manager can bypass a quarantined
// agent's stuck goroutine and dispatch the skip in-process.
// BUG-WEREWOLF-P0-NEW-3.
func SkipPhaseAction(phase, role string) (string, int) {
	switch phase {
	case "night_wolves", "PhaseNightWolves":
		// Empty kill (target = -1 is rejected by the engine as a wolf
		// strategy violation, so we pick seat 0 if alive — engine will treat
		// it as a regular kill, which is the safest fallback).
		return "wolf_kill", -1
	// 2026-07-29 §134 守卫守护 — quarantine / 沉默 / watchdog 兜底时,
	// 派发 guard_protect_skip → runner.GuardProtect(-1) = 空守。
	// 空守是合法出口(G4),引擎推进到下一阶段(PhaseNightWolves)。
	case "night_guard", "PhaseNightGuard":
		return "guard_protect_skip", 0
	case "night_seer", "PhaseNightSeer":
		// Seer can't easily skip — checking a random alive seat is acceptable.
		return "seer_check", -1
	case "night_witch", "PhaseNightWitch":
		// Witch's "pass" action: empty string.
		return "witch_act_skip", 0
	// §猎魔人 猎魔人狩猎 — quarantine / 沉默 / watchdog 兜底时,
	// 派发 demon_hunter_hunt_skip → runner.DemonHunterHunt(-1) = 空过。
	// 空过是合法出口(DH4),引擎推进到下一阶段(PhaseDawn)。
	// 关键决策:watchdog 兜底**永远空过**(target=-1)而非随机狩猎某个存活玩家 —
	// 随机狩猎的失败率约 80%(场上 12 个玩家,4 狼+8 好,盲射命中狼 = 4/12 = 33%),
	// 误杀好人会让猎魔人+1 个好人出局,直接屠边。
	// 空过只是「本晚不动」,第二天白天仍可基于讨论再次狩猎,代价最低。
	case "night_demon_hunter", "PhaseNightDemonHunter":
		return "demon_hunter_hunt_skip", 0
	case "speak", "PhaseSpeak":
		// Finish the speak turn without saying anything.
		return "finish_speak", 0
	case "vote", "PhaseVote":
		// Per-bot auto-skip (vote_skip = Vote(NoSeat) = abstain).
		// BUG-WEREWOLF-P0-NEW-35: 复述段落已压缩 — git blame 与 docs/ 索引可还原

		return "vote_skip", 0
	case "sheriff", "PhaseSheriff":
		// Sheriff elect (driver-only) advances even if no one campaigned.
		return "sheriff_elect", 0
	case "dawn", "PhaseDawn":
		// BUG-WEREWOLF-P1-NEW-1: dawn is a structural phase that only the
		// driver bot advances by calling start_day. If the driver is
		// quarantined (e.g. Kimi 403 quota) the room dead-locks here
		// because nothing else will ever call StartDay. SkipPhaseAction
		// now returns start_day so the manager can dispatch it on behalf
		// of a quarantined driver.
		return "start_day", 0
	case "hunter_shoot", "PhaseHunterShoot":
		// BUG-WEREWOLF-P0-2 (R42): hunter dies then shoots — the ability
		// triggers on death. When the dead hunter is quarantined, the
		// phase has no acting seat and permanently stalls. Skip via
		// hunter_shoot(-1) = "don't shoot", which advances the day.
		return "hunter_shoot", -1
	case "death_lyric", "PhaseDeathLyric":
		// BUG 2026-07-09: 遗言 actor 被 quarantine 时,watchdog / skip 路径
		// 派发 last_words_skip 放弃遗言,推进队列直至清空后恢复原路径。
		return "last_words_skip", 0
	case "idiot_reveal", "PhaseIdiotReveal":
		// 2026-07-10 §12.2:quarantined 白痴默认 skip 翻牌(放弃翻牌,正常放逐)。
		// 由 dispatchQuarantinedSkipLocked 派发,走 IdiotReveal("skip") 路径。
		return "idiot_reveal_skip", 0
	case "restart_vote", "PhaseRestartVote":
		// 2026-07-10: 重开局投票阶段 quorum 评估由 manager 决定
		// (EvaluateRestartVoteLocked),不需要 per-bot skip 路径;
		// 若被 quarantine,直接返回 ("", 0) 让 manager 走 no-op + deadline 兜底。
		return "", 0
	}
	return "", 0
}

// ShouldAutoSkip reports whether this agent is the correct one to fire the
// phase's skip action. In most phases any agent can safely skip (night phases
// only wake the acting seat; sheriff/dawn only wake the driver). But in the
// speak phase the engine's wakeAll() pings every alive agent for transcript
// sync — non-speaker agents that receive an end_turn response must NOT
// dispatch finish_speak because the engine will reject with [30008] "not
// current speaker". Only the current speaker should auto-skip.
//
// BUG-WEREWOLF-P0-SPEAK-AUTOSKIP (Round 28-29): every non-speaker agent
// whose LLM returned end_turn tried to dispatch finish_speak; all of them
// failed with [30008]. The resulting warn-storm flooded the log and,
// critically, when the ACTUAL speaker ALSO returned end_turn its skip was
// interleaved with the non-speaker failures and could get lost in the
// dispatch noise, leaving the phase permanently stuck.
//
// BUG-WEREWOLF-P0-NEW-33 (Round 33): the original guard relied on
// evtContext.SpeakTurn (the snapshot taken at wake time). But the LLM HTTP
// call can take 8+ seconds while another bot's earlier skip has already
// advanced SpeakTurnSeat past us; the agent is still the snapshot-stamped
// "speaker" but the engine no longer agrees. The fix uses a LIVE re-read of
// SpeakTurnSeat (currentSpeakTurn) to avoid the stale-snapshot window. Same
// race applies to TurnActingSeat for night phases.
func ShouldAutoSkip(phase string, seat int, evtContext wwtypes.GameContext, currentSpeakTurn int, currentTurnActing int) bool {
	switch phase {
	case "speak", "PhaseSpeak":
		// BUG-WEREWOLF-P0-NEW-33: trust live currentSpeakTurn over the
		// stale evtContext snapshot. Any non-negative currentSpeakTurn
		// from rp() is authoritative; if rp() failed / returned -1 we
		// fall back to the snapshot to keep the path covered.
		if currentSpeakTurn >= 0 {
			return currentSpeakTurn == seat
		}
		return evtContext.SpeakTurn == seat
	}
	// Night / sheriff / dawn: the live TurnActingSeat decides. If rp()
	// returned -1 (tearing down) we conservatively allow the skip —
	// the engine will still reject it if the bot is wrong, but most
	// paths already pre-filter via gc.MyTurn, so this is the last-mile
	// guard.
	if currentTurnActing >= 0 {
		return currentTurnActing == seat
	}
	return true
}

// SetEvents injects the per-seat wake channel produced by the room manager.
// Must be called before Run. The manager guarantees the channel is buffered
// and closed when the room is torn down, unblocking Run's select.
func (a *Agent) SetEvents(ch chan AgentEvent) {
	a.Lock()
	defer a.Unlock()
	a.events = ch
}

// IsDone reports whether Run should stop handling events. Used by the room
// manager's game-over path to short-circuit the loop.
func (a *Agent) IsDone(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

// Run is the per-seat decision goroutine. It blocks on `events` (set via
// SetEvents), then:
//  1. Records the latest context into memory (user turn).
//  2. Builds an LLM request (system + phase-filtered tools).
//  3. Sends to the provider; on success records assistant turn + dispatches
//     tool_use blocks. Loop until stop_reason != "tool_use" or MaxToolUse hit.
//
// Speak/whisper calls mark the limiter so the 30s/60s interval is enforced
// across turns. Errors from the provider are logged (via the runner's
// ChatService hook) and the agent yields for the next event.
//
// The function returns when ctx is cancelled, the events channel is closed +
// drained, or the game reaches PhaseGameOver.
func (a *Agent) Run(ctx context.Context, runner ToolRunner, phase RolePhase) {
	// runner is the engine-facing ToolRunner adaptor built by the room
	// manager when it constructs this Agent. phase is a callback
	// (func() (phase, role string, seat int, alive []int)) so the prompt +
	// tool catalog always reflect the *latest* game state, not whatever the
	// event snapshot captured.
	//
	// Note: we use a typed adapter (see runnerAdapter below) so this loop
	// doesn't need to know that ToolRunner is implemented in another package.
	a.runLoop(ctx, runner, phase)
}

// RolePhase is a callback into the engine that returns the data needed to
// build the prompt + tool list for the current decision turn. Kept as a
// function (not a struct) so the manager can read live state without an
// extra snapshot copy.
//
// The trailing speakTurn/turnActing fields reflect the LIVE engine state at
// the moment of the call (NOT a snapshot from the wake event). Three callers
// use them: (a) BuildTools/BuildUserPrompt live re-read, (b) the auto-skip
// guard, (c) the no-tool-use dispatch guard. Returning them in the same
// function keeps callers from having to plumb a second snapshot through the
// event channel — the live state may have moved several phases forward since
// the wake was queued.
//
// BUG-WEREWOLF-P0-NEW-33: R33 speak r2 deadlock — agent dispatched
// finish_speak with SpeakTurnSeat=4 (snapshot) but by the time the dispatch
// ran the engine had already auto-skipped the bot and moved on, returning
// [30008] "not current speaker". Cross-checking against live speakTurn in
// ShouldAutoSkip fixes this race window.
type RolePhase func() (phase, role string, seat int, alive []int, speakTurn int, turnActingSeat int, done bool)

// runLoop sits on the events channel and drives LLM decide-act iterations.
func (a *Agent) runLoop(ctx context.Context, runner ToolRunner, rp RolePhase) {
	for {
		a.Lock()
		evCh := a.events
		a.Unlock()
		if evCh == nil {
			// events not yet set; back off briefly.
			select {
			case <-ctx.Done():
				return
			case <-time.After(200 * time.Millisecond):
				continue
			}
		}

		select {
		case <-ctx.Done():
			return
		case evt, ok := <-evCh:
			if !ok {
				// channel closed — room torn down.
				return
			}
			if evt.Kind == "game_over" {
				return
			}
			a.handleEvent(ctx, runner, rp, evt)
		}
	}
}

// handleEvent turns one AgentEvent into a single decide/act cycle (possibly
// multiple LLM round-trips if the model keeps returning tool_use blocks).
func (a *Agent) handleEvent(ctx context.Context, runner ToolRunner, rp RolePhase, evt AgentEvent) {
	// BUG-WEREWOLF-P0-NEW-4 (Round 24): a quarantined agent must NOT call the
	// LLM, append to Memory, or dispatch any tool, even if a stale wake
	// event slipped into its channel. Before this guard, the goroutine
	// happily re-entered the decide/act loop on every delayed scheduleReWake,
	// burned an LLM round-trip on a permanently broken model, and re-set
	// quarantine (logging "agent: quarantined" 10+ times in a row) — adding
	// to the noise without ever advancing the phase. The manager's
	// WakeActingAgents path is responsible for emitting skip actions on our
	// behalf; we just yield.
	if a.IsQuarantined() {
		logger.L().Debug("agent: quarantined; ignoring wake",
			zap.Int("seat", a.Seat), zap.String("kind", evt.Kind))
		return
	}

	// 2026-07-24 优化:UI 暂停时跳过 LLM 调用(防批量 quarantine)。
	// 房间被真人玩家 ⏸ 暂停时,即使 wake 事件已进入队列也丢弃,继续推
	// 进得等 ▶ 恢复后下一次 wake;不需要主动 re-schedule。
	if evt.Context.RoomPaused {
		logger.L().Debug("agent: room paused; ignoring wake (no LLM call)",
			zap.Int("seat", a.Seat), zap.String("kind", evt.Kind),
			zap.String("phase", evt.Context.Phase))
		return
	}

	// BUG-WEREWOLF-P0-4 FIX: never feed the LLM a "stay silent" turn built
	// from an empty snapshot. An empty Phase means the wake carried no real
	// game context (snapshot build failed / room tearing down). Pushing it
	// would append BuildUserPrompt's "(暂未轮到你…保持沉默)" to Memory,
	// which previously locked every agent out of acting and stalled the
	// phase at night_wolves. Yielding is safe: the next real state push
	// re-wakes us.
	if evt.Context.Phase == "" {
		return
	}

	// BUG-WEREWOLF-P1-NEW-45: 复述段落已压缩 — git blame 与 docs/ 索引可还原

	_, _, mySeat, aliveList, _, _, doneCheck := rp()
	if doneCheck {
		return
	}
	if !containsSeat(aliveList, mySeat) {
		logger.L().Debug("agent: dead seat; ignoring wake (no LLM call, no tool dispatch)",
			zap.Int("seat", a.Seat),
			zap.Int("alive_count", len(aliveList)),
			zap.String("kind", evt.Kind),
			zap.String("phase", evt.Context.Phase))
		return
	}

	// BUG-R71-FLOOR-FASTPATH: speak_floor_tick 走独立 fast-path(在主循环
	// Memory.Push 之前 early-return),避免:
	//   1. 主循环下面 357-360 行的重复 Memory.Push(handleSpeakFloorTick
	//      内部 953-956 行已经 push 过一次 user 消息,加 floorHint);
	//   2. 被主循环的 LLMCallLimiter 8s 限流误伤(line 464)— 4-6 号非
	//      acting seat 的 bot 永远从空开始计时,5s tick 节奏 + 8s 间隔
	//      形成活锁,被 §13 的 watchdog 推 floor wake 也调不到 LLM。
	// 修复:把 floor tick 的 dispatch 前置到 handleEvent 入口,内部
	// handleSpeakFloorTick 已 bypass limiter / sema,直接走 callProvider。
	if evt.Kind == "speak_floor_tick" {
		phase, role, seat, _, _, _, done := rp()
		if done {
			return
		}
		a.handleSpeakFloorTick(ctx, runner, rp, evt, phase, role, seat)
		return
	}

	// 2026-07-08 §13.5: 观战者触发的 wake 事件需要"必须思考"审计。
	// trackedSpoken 在 LLM 决策循环中标记"该 bot 是否真的发声/主动选择沉默":
	//   - 调了 speak / whisper / interject → 视为已发声,无需审计。
	//   - 调了 idle_silent (role=player) → 视为已主动选择沉默,LLM 已留 audit,无需再补。
	//   - end_turn 且未调工具 → 在 §13.5 路径追加 [idle_silent] 审计行。
	// 仅在 evt.Kind == "spectator_speech" 时启用;其他 Kind 走原行为。
	trackedSpoken := false
	spectatorAwake := evt.Kind == "spectator_speech"

	// 1. Record the incoming context as a user turn.
	// BUG-R79 P1-NEW (2026-07-10): 连续失败 ≥2 次时,在 user prompt 末尾追
	// 加强提示,告诉 LLM "如不能正常回复请立即调 idle_silent / finish_speak
	// 跳过"。R79 观察到 6/7 bot 因 LLM 失败 stuck 在 >2min 的 retry 循环,
	// 仅 MiniMax (1 bot) 实际公屏发言 — 强化提示让 LLM 立即放弃,避免
	// 浪费 LLMCallLimiter 槽位 + 阻塞正常 bot 的发言节奏。
	basePrompt := BuildUserPrompt(evt.Context)
	// BUG-R82-P1-NEW (2026-07-10 §P0-NEW): 观战者公开消息触发 spectator_speech
	// wake 后,7 个 bot 同时被唤醒;但 §13.4 phase 白名单导致 wake 后 LLM 调用
	// 立即进入"phase moved on → yield"路径 → spectator 消息没有 bot 回应。
	// 在 user prompt 末尾追加【观众问询】块:即使本轮 phase 推进,bot 也会把
	// 这条 spectator 消息留在最近 prompt 里,下一轮 LLM 决策时仍可 interject/whisper。
	if spectatorAwake {
		basePrompt += "\n\n【观众问询】一位人类观战者刚在公屏发了消息(已记录到 recent_speeches)。" +
			"请你立刻调 interject(≤100 字)回应这条观众问询 — 如果当前不是 speak 阶段" +
			"也无妨,interject 在所有 phase 都可用。如果 LLM 觉得无需回应,可调 idle_silent (role=player) 留 audit。"
	}
	if a.ConsecutiveFailures() >= 2 && !a.IsQuarantined() {
		basePrompt += "\n\n【连续失败告警】你的 LLM 调用已连续失败 " +
			itoa(a.ConsecutiveFailures()) + " 次。本轮如不能正常回复," +
			"请立即调 idle_silent (role=player) 留 audit 记录," +
			"等待下一轮重试,不要再尝试 speak/vote/wolf_kill 等主动工具。"
	}
	// 2026-07-20 §131 新增 — 注入持久化记忆(MEMORY.md)。
	// 2026-08-12 §20260812-04 U4 — 改为**按角色选取**后再注入。
	//
	// 旧行为:无条件全量注入 4000 runes(≈3200 token),且记忆按 model_key 存储
	// 不按角色隔离 —— 同一模型坐预言家学到的教训,坐狼人时照样全量注入。
	// 13 人局单 bot 一局 50+ 次调用 = 约 16 万 token 纯重复。
	//
	// 新行为:只保留「通用内容 + 当前角色子段」。旧格式(无 `### 作为X` 子段)
	// 行为完全不变。maxRunes 走难度档位(a.memoryInjectRunes,0 = 用默认常量),
	// 顺带接线了此前 4 处赋值 0 处读取的 difficulty.MemoryInjectRunes。
	basePrompt += InjectBlockForRole(a.MemoryMD, evt.Context.Role, a.memoryInjectRunes)

	// §20260811-01 新增: drain steering queue,注入实时游戏事件。
	// 灵感来源: PI Agent 的 steeringQueue.drain() 机制。
	// 在 Memory.Push 前注入,确保本轮 LLM 能看到观众消息/道具命中等实时事件。
	if a.steeringQueue != nil {
		if steerText := a.steeringQueue.DrainAndFormat(); steerText != "" {
			basePrompt += steerText
		}
	}

	// §20260811-01 新增: LLM 记忆压缩 (每局最多一次)。
	// 灵感来源: PI Agent 的 compaction/compaction.ts — 结构化摘要。
	// 在第 40 条消息后的首次 LLM 调用前触发,将旧消息压缩为结构化游戏摘要。
	if !a.compactDone && a.compactConfig.Enabled && a.Provider != nil {
		if count := a.Memory.Len(); count >= a.compactConfig.MinMessages {
			a.compactDone = true
			go func() {
				rp2 := rp
				_, _, _, _, _, _, done2 := rp2()
				if done2 {
					return
				}
				result := a.Memory.CompactWithLLM(ctx, a.Provider, a.apiKey, a.ModelKey, &evt.Context, a.compactConfig)
				if result.Success {
					logger.L().Info("agent: memory compacted",
						zap.Int("seat", a.Seat),
						zap.Int("before", result.MessagesBefore),
						zap.Int("after", result.MessagesAfter),
						zap.Int64("ms", result.DurationMs))
				}
			}()
		}
	}

	a.Memory.Push(llm.Message{
		Role:    "user",
		Content: []llmtypes.ContentBlock{{Type: "text", Text: basePrompt}},
	})

	// 2026-07-09 §重构 - 决策可观测性 STAGE 1:把本轮 wwtypes.GameContext 压成
	// ≤ 200 字的输入摘要(数字 + 阶段,无 CoT),供 recordTranscript 填入
	// BotTranscript.DecisionInputs 并下发到 AgentInteractionPanel。
	a.MergeLastDecision(RecordDecisionState{
		DecisionInputs: BuildInputSummary(evt.Context),
	})

	phase, role, seat, alive, _, _, done := rp()
	if done {
		return
	}
	_ = evt // evt carries the snapshot; rp gives us the latest live phase/role.

	logger.L().Debug("agent: handling event",
		zap.Int("seat", a.Seat), zap.String("phase", phase),
		zap.String("role", role), zap.Bool("my_turn", evt.Context.MyTurn),
		zap.Bool("is_driver", evt.Context.IsDriver),
		zap.Int("alive", len(alive)))

	// §128 对话即思考重构:ParallelThink 调用已删除(原 §122)。
	// LLM API 输出的 text + tool_use 即是模型"思考"的产物,无需辅助并行 worker。

	// 2. Run decide->act rounds.
	// §20260810-13 重构:恢复内层循环硬上限(按阶段差异化配置,详见
	// maxInnerRoundsForPhase),替换 §130 的"完全无上限"方案。
	// 无上限导致 LLM 在 night_wolves 等阶段陷入"调 wolf_kill → 成功 → 继续循环
	// → 再调 wolf_kill → 失败 → 继续循环"的死循环(单 bot 4 分钟 23 次调用)。
	// 策略:
	//   - round 计数器:每阶段最多 N 轮 LLM 调用(阶段差异化)
	//   - actionDone 标志:单次行动工具成功后立即退出(见 run.go 工具派发处)
	//   - watchdog / phase deadline / consecutiveFailures 仍是 phase-level 兜底
	maxRounds := maxInnerRoundsForPhase(phase)
	round := 0
	// actionDone:单次行动工具(wolf_kill/seer_check 等)成功调用后置 true,
	// 内层循环立即退出,避免浪费 API 调用。
	actionDone := false
	// 2026-08-12 §20260812-04 U5 (P0-6) — 每轮释放房间级 LLM 信号量。
	//
	// 缺陷:`defer a.ReleaseLLMSlot()` 原本写在 for 循环体内(run.go:851),
	// Go 的 defer 是**函数级**不是块级 —— 5 轮内层循环 = acquire 5 次、
	// 直到 handleEvent 整个返回才一次性释放 5 次。cap=4 的房间信号量
	// (roomLLMConcurrency)被单个 bot 的一次 wake 吃满,其余 12 个 bot
	// 全部 5s 等待失败 → reWake,与 run.go:317 注释宣称的
	// 「不让慢模型阻塞快模型」完全相反。
	//
	// 修法:用「本轮是否持槽」的标志 + 轮末释放,并在函数级 defer 兜底
	// 任何 return 分支。releaseSlot 幂等,重复调用安全。
	slotHeld := false
	releaseSlot := func() {
		if slotHeld {
			slotHeld = false
			a.ReleaseLLMSlot()
		}
	}
	defer releaseSlot()
	for {
		// 上一轮若仍持有槽位(break/continue 前未显式释放),先归还再进入本轮,
		// 保证「一轮最多占一个槽」。
		releaseSlot()
		// Check between rounds so cancellation takes effect promptly.
		if err := ctx.Err(); err != nil {
			return
		}

		// §20260810-13: 单次行动完成 → 立即退出(正向路径)
		if actionDone {
			logger.L().Debug("agent: action completed, exiting inner loop",
				zap.Int("seat", a.Seat), zap.String("phase", phase),
				zap.Int("round", round))
			break
		}

		round++
		if round > maxRounds {
			logger.L().Warn("agent: inner loop max rounds reached, exiting",
				zap.Int("seat", a.Seat), zap.String("phase", phase),
				zap.Int("max_rounds", maxRounds), zap.String("model", a.ModelKey))
			break
		}

		tools := BuildTools(phase, role, seat, alive, evt.Context.SpeakTurn, &evt.Context)
		if len(tools) == 0 {
			// No legal tools (e.g. wrong turn). Yield to next event.
			logger.L().Debug("agent: no tools for phase, yielding",
				zap.Int("seat", a.Seat), zap.String("phase", phase), zap.String("role", role))
			return
		}

		msgs, _ := a.Memory.Snapshot()
		// BUG-WEREWOLF-P1-LLM-TOOL (Round 26): patch dangling tool_use ids
		// so DeepSeek/GLM strict proxies don't reject the request with
		// 400 "tool_use ids were found without tool_result blocks". This
		// is a defensive, request-scoped patch — Memory itself is left
		// intact for the UI AgentThoughtPanel.
		msgs, patched := SanitizeMessagesForAnthropic(msgs)
		if patched > 0 {
			logger.L().Info("agent: sanitized messages for anthropic protocol",
				zap.Int("seat", a.Seat), zap.Int("patched_tool_uses", patched),
				zap.String("model", a.ModelKey))
		}
		// §128 对话即思考重构:并行推理结果注入已删除。

		// 2026-08-10 §20260810-14 增强:记录 system + tools 的近似字节数到 Memory,
		// 供 enforceByteBudgetLocked 使用完整 payload 大小(messages + system + tools)
		// 作为剪枝基准。之前仅计算 messages,忽略了 system prompt(~2KB)和 tools
		// 定义(~5-10KB)。DouBao 等小窗口模型的上下文窗口只有 ~128K-256K,这部分
		// "隐形开销"在极端场景下可能导致实际 payload 超限但字节预算未触发
		// (如 ID 1225 的 651816 tokens)。
		system := BuildSystemPrompt(a.SelfPortraitText, a.Personality, a.PersonalityPresetKey, a.DifficultyDirective)
		a.Memory.SetSystemTools(system, tools)
		// §20260811-10 U1 — 照妖镜一次性强制真实身份:消费 flag 并追加指令。
		// flag 由 mirror_check 道具命中后 prop_engine.SetMirrorExposeActiveLocked
		// 在 r.mirrorExposeActive[seat] 置位;此处读取 + 清空,并把指令拼入 system。
		// BuildSystemPrompt 末尾追加(MirrorExposeDirective),仅本次 LLM 调用生效。
		if runner != nil && runner.ConsumeMirrorExposeFlag() {
			system = append(system, llm.SystemBlock{
				Type: "text",
				Text: "【照妖镜效果 §20260811-10】你正在被照妖镜照射。本轮 system prompt 优先级最高:" +
					"请在你的下一次发言 / 内心独白中,如实写下你当前的真实身份(阵营 + 角色)。",
			})
			a.Memory.SetSystemTools(system, tools)
		}

		req := llm.LLMRequest{
			Model:    resolveModelName(a.ModelKey),
			System:   system,
			Messages: msgs,
			Tools:    tools,
			// 2026-08-06 §AgentClassName 增强:让上游 / 网关 / 日志按"哪一类 Agent"
			// 区分调用方 — 玩家 Bot 用 AgentClassWerewolfPlayer,法官用
			// AgentClassWerewolfJudge,记忆迭代用 AgentClassWerewolfMemoryIter。
			// 常量集中在 ServerGo/agent/class_names.go。
			AgentClassName: string(agentroot.AgentClassWerewolfPlayer),
			// 2026-07-17 R139 修复:1024 token 在 Kimi-model 等中文模型 + wolf_kill
			// tool_use 路径下偶发 stop_reason=max_tokens 截断(长 thinking + 长
			// tool input 拼接超出),导致夜晚狼人座位失语、auto-skip 接管。提到
			// 2048 给模型思考 + 工具调用充分预算,§128 对话即思考后工具 schema
			// 文本较长,实际请求体已逼近 1024 上限。
			MaxTokens: 2048,
			// Per-call metadata mirrors ClaudeCode's `metadata.user_id` —
			// a stringified JSON blob identifying the call site. Anthropic
			// proxies / observability use this for traffic attribution and
			// abuse detection. Format parallels the reference payload in
			// `CluadeCode请求RequestBody的Anthropic协议定义数据用例.json`:
			// device_id + session_id, plus LsmWebGame-specific room/seat.
			Metadata: llmtypes.Metadata{
				UserID: buildMetadataUserID(a),
			},
		}
		// §128 对话即思考重构:thinking 注入与 auto-healing fallback 已删除。
		// LLM API 输出的 text + tool_use 即是模型"思考"的产物。

		// BUG-WEREWOLF-P0-NEW-31: 复述段落已压缩 — git blame 与 docs/ 索引可还原

		a.ResetLLMCallState()

		// BUG-R242-P1-01: 房间级 LLM 并发信号量。§130 删除后 13 bot × 内层重试
		// fully concurrent 打满上游代理 → 级联失败(实测 27min 66% 失败率)。
		// 恢复有界信号量(默认 cap=4):槽位满时短暂等待后 reWake(瞬态,不计入
		// consecutiveFailures),限制在途调用数的同时不让慢模型无限阻塞快模型。
		// 禁用时(llmSema == nil,§130 行为)AcquireLLMSlot 恒返回 true。
		if !a.AcquireLLMSlot(llmSlotAcquireWait) {
			logger.L().Warn("agent: LLM slot saturated; backing off (reWake, not counting toward quarantine)",
				zap.Int("seat", a.Seat), zap.String("phase", phase),
				zap.String("model", a.ModelKey))
			a.SetLastErrorClass("semaphore")
			go a.scheduleReWake(ctx, reWakeDelay)
			return
		}
		// §20260812-04 U5 (P0-6):标记本轮持槽。释放在轮首 releaseSlot() /
		// 函数级 defer releaseSlot() 两处,不再用循环体内 defer(会累积到函数结束)。
		slotHeld = true

		// 2026-07-09 §13-bugfix: mark LLM call in-progress so the spectator
		// panel can show "正在调用大模型…" with a live timer. MarkLLMCallEnd
		// is deferred so all exit paths (ctx cancel, quarantine, normal flow)
		// clear the flag. The safety-net MarkLLMCallEnd() at the top of the
		// next iteration also clears any stale state.
		a.MarkLLMCallStart()
		// 2026-07-10 §重构 — 进入 HTTP 调用阶段;前端 BotPhaseIndicator 据此
		// 渲染"即将发言/思考中 Ns"倒计时。MarkLLMCallEnd deferred 会负责
		// 复位到 idle(success)或触发 retrying(failure 在 retry loop 切换)。
		// 2026-07-30 §统计增强 — 成功时累加 Token（通过 retUsage/retErr 闭包透传）；
		// 失败路径（err != nil）调 RecordAPIFailure 单独计数，避免重复。
		a.SetLLMCallPhase(PhaseCalling)
		// retUsage 保存最后一次 callProvider 的 usage；retErr 保存最终错误。
		// defer 闭包仅在成功时累加 Token + 成功次数。
		var retUsage llmtypes.LLMUsage
		var retErr error
		defer func() {
			// 失败路径由下方 err != nil 分支调 RecordAPIFailure 单独计数；
			// 成功路径在此累加 Token + 成功次数。
			if retErr == nil {
				a.MarkLLMCallEndWithUsage(retUsage)
			}
		}()

		// §127: 启动聊天流式 stream(每个 LLM 调用唯一 ID = seat:unix_ms)。
		// 调到 MarkLLMCallStart 之后,保证 activeStreamID 在本轮 LLM 生命周期内有效。
		streamID := fmt.Sprintf("%d:%d", a.Seat, time.Now().UnixMilli())
		a.SetActiveStreamID(streamID)
		if a.onLLMStreamStart != nil {
			a.onLLMStreamStart(streamID)
		}
		// §127: 聊天 SSE 流式解析 — 实时 text_delta → 前端气泡 token 瀑布流。
		var streamText strings.Builder

		// BUG-R124-PERF-02: 复述段落已压缩 — git blame 与 docs/ 索引可还原

		callTimeout := cfgLLMCallTimeoutSec(evt.Context.SeatCount)
		if callTimeout <= 0 {
			callTimeout = defaultLLMCallTimeoutSec
		}
		extendedTimeout := cfgStreamExtendedTimeoutSec()
		if extendedTimeout <= 0 {
			extendedTimeout = defaultStreamExtendedTimeoutSec
		}
		totalBudgetSec := llmCallBudgetSec(callTimeout, extendedTimeout)
		streamStart := time.Now()

		// parentCtx:整次 LLM 调用的最终总预算 (callTimeout + extendedTimeout),
		// 默认 300 + 900 = 1200s = 20 min。即使 LLM 卡住/无限重试,parentCtx
		// 到期必然 cancel,确保不会无限挂起。
		// 2026-08-01 修复:删除旧的 `else { context.WithCancel(ctx) }` 分支。
		// callTimeout / extendedTimeout 上面都已兜底为正值,该分支已不可达;
		// 保留它等于留一条"配置异常 → 无界调用"的后门(§130 不留死代码)。
		parentCtx, parentCancel := context.WithTimeout(
			ctx, time.Duration(totalBudgetSec)*time.Second)
		defer parentCancel()

		// §流式续命(简化):首字节到达后打 Debug 日志,确认"最少 15 min"已生效。
		// idleTimeoutReader 已保证首字节前 300s 熔断、首字节后无 idle 计时器,
		// 外层 ctx 总预算 (callTimeout + extended) 提供最终兜底。
		firstByteExtended := false
		streamProgress := func(ev llmtypes.StreamEvent) error {
			if ev.Type == "_first_token" {
				a.SetLLMCallPhase(PhaseStreaming)
				if !firstByteExtended {
					firstByteExtended = true
					logger.L().Debug("agent: stream first-byte received; extended timeout active",
						zap.Int("seat", a.Seat),
						zap.String("phase", phase),
						zap.String("model", a.ModelKey),
						zap.Int("first_byte_budget_s", callTimeout),
						zap.Int("total_budget_s", callTimeout+extendedTimeout),
						zap.Int("min_post_first_byte_s", extendedTimeout),
						zap.Duration("first_byte_elapsed", time.Since(streamStart)),
					)
				}
			}
			// §127: text_delta 实时流式 → 前端气泡 token 瀑布流。
			if ev.Type == "content_block_delta" && ev.DeltaType == "text_delta" {
				if a.onLLMStreamDelta != nil {
					a.onLLMStreamDelta(a.ActiveStreamID(), ev.Delta)
				}
				streamText.WriteString(ev.Delta)
			}
			return nil
		}
		// §127: LLM 调用结束(成功 / 失败 / ctx cancel)时发 stream_end,清空 activeStreamID。
		// 2026-07-15 BUG-R124-SEC-001: 流式面板只展示最终选定的一条回复,过滤掉
		// LLM 在单次 speak 里输出的多条备选拼接(典型: "我是X号...我是X号...我是X号...")。
		// 复用 agentcore.DedupSpeakText 在 stream_end 时对累积文本做一次廉价去重,避免
		// 真人观众看到「策略生成中间过程」。仅做"序列复读"剔除,不做长度截断
		// (stream 预览允许较长,最终 chat.message 截断由 Speak/SpeakAuto 链做)。
		defer func() {
			finalText := streamText.String()
			if finalText != "" {
				if cleaned, wasDeDuped, _ := agentcore.DedupSpeakText(finalText); wasDeDuped && cleaned != "" {
					logger.L().Info("agent: stream-text multi-draft dedup (R124-SEC-001)",
						zap.Int("seat", a.Seat), zap.String("phase", phase),
						zap.String("model", a.ModelKey),
						zap.Int("orig_len", len(finalText)), zap.Int("cleaned_len", len(cleaned)))
					finalText = cleaned
				}
			}
			if a.onLLMStreamEnd != nil {
				a.onLLMStreamEnd(streamID, finalText)
			}
			a.SetActiveStreamID("")
		}()

		resp, err := a.callProvider(parentCtx, req, streamProgress)
		// 2026-07-30 §统计增强 — 保存首次调用的 usage，供成功时累加。
		retUsage = resp.Usage
		retErr = err
		if err != nil {
			// Provider-level error (network / 5xx exceeded retries / 429).
			// Retry with exponential backoff to handle transient failures
			// (proxy hiccups, network blips). After all retries exhausted,
			// self-re-wake after a delay so the game doesn't permanently
			// deadlock — a single failed wake should not block the phase.
			retryable := true
			if ae, ok := err.(*anthropic.Error); ok && !ae.Retryable {
				retryable = false
			}
			// §128 对话即思考重构:thinking auto-healing fallback 已删除。
			if err != nil && retryable {
				// 2026-07-12 §127 增强 — 外层重试上限可配置。
				// 2026-07-15 R131 修复: 默认 7→5(累计 backoff 1+2+4+8+8 cap=23s,
				// 配合 cfgLLMCallTimeoutSec=120s 有 97s 余量给 LLM 调用)。
				maxRetries := a.MaxLLMRetries()
				if maxRetries <= 0 {
					maxRetries = 5
				}
				for attempt := 1; attempt <= maxRetries; attempt++ {
					// 2026-07-24 优化:线性 2s/4s/6s/8s 退避,代替原指数 1s/2s/4s/8s。
					// 见 llmRetryLinearBackoff 注释。
					backoff := llmBackoffForAttempt(attempt)
					if backoff > llmRetryMaxBackoff {
						backoff = llmRetryMaxBackoff
					}
					// 2026-07-10 §重构 — 进入 retry loop 时切换 phase 到 retrying,
					// 并写入 attempt/max/nextRetryAtMs 供前端倒计时。
					nextAt := time.Now().Add(backoff).UnixMilli()
					a.SetRetryAttempt(attempt, maxRetries, nextAt)
					a.SetLLMCallPhase(PhaseRetrying)
					// 失败分类:5xx retryable → "5xx";其它 retryable 暂归类 "5xx"
					// (后续可细化,前端目前按 5xx/timeout/permanent 三种徽章处理)。
					if a.lastErrorClass == "" || a.lastErrorClass == "none" {
						a.SetLastErrorClass("5xx")
					}
					logger.L().Warn("agent: LLM retry",
						zap.Int("seat", a.Seat), zap.String("phase", phase),
						zap.String("model", a.ModelKey),
						zap.Int("attempt", attempt), zap.Int("max", maxRetries),
						zap.Duration("backoff", backoff))
					select {
					case <-parentCtx.Done():
						return
					case <-time.After(backoff):
					}
					// 重试前重新切回 calling(让前端看到"重试 N/M 即将重试" → "调用中"
					// 的过渡)。
					a.SetLLMCallPhase(PhaseCalling)
					resp, err = a.callProvider(parentCtx, req, streamProgress)
					// 2026-07-30 §统计增强 — 每次重试都更新 retUsage/retErr，
					// 使 defer 闭包读到最终结果（成功时 retUsage 含真实 Token）。
					retUsage = resp.Usage
					retErr = err
					if err == nil {
						break
					}
					logger.L().Warn("agent: LLM retry failed",
						zap.Int("seat", a.Seat), zap.Int("attempt", attempt),
						zap.Error(err))
				}
			}
			if err != nil {
				// 2026-07-30 §统计增强 — 失败路径单独计数（defer 不再累加）。
				a.RecordAPIFailure()
				logger.L().Warn("agent: provider chat failed (all retries exhausted)",
					zap.Int("seat", a.Seat), zap.String("phase", phase),
					zap.String("model", a.ModelKey), zap.Error(err))
				// BUG-WEREWOLF-P1-NEW-46: 复述段落已压缩 — git blame 与 docs/ 索引可还原

				a.SetLastError(err.Error())
				// 2026-07-10 §重构 — 写入失败分类供前端多态重试徽章区分:
				//   - 429 → "429"(上游限流,展示"冷却中"红脉冲)
				//   - retryable (5xx / 上游瞬断) → "5xx"(展示"连接中断,重试中"黄脉冲)
				//   - permanent (401/403/400 missing thinking) → "permanent"
				//   - timeout → "timeout"
				switch {
				case !retryable:
					a.SetLastErrorClass("permanent")
				case isAnthropic429(err):
					a.SetLastErrorClass("429")
				case isAnthropicTimeout(err):
					a.SetLastErrorClass("timeout")
				case errors.Is(err, context.DeadlineExceeded):
					// 2026-07-15 R131: 单次 LLM 调用总超时默认 300s(大房间可缩放至 450s),
					// 归类为 timeout 让前端展示「冷却中」徽章;不算 permanent。
					a.SetLastErrorClass("timeout")
				default:
					a.SetLastErrorClass("5xx")
				}
				a.Memory.Push(llm.Message{
					Role:    "user",
					Content: []llmtypes.ContentBlock{{Type: "text", Text: "provider_error: " + err.Error()}},
				})
				// BUG-R241-P1-01: 失败路径也要按字节预算剪枝。此前仅成功路径调
				// CompressAndPrune,失败时只推 provider_error 不剪枝 → payload 只增
				// 不减 → 小窗口模型(如 DouBao)一旦 400 "exceed max message tokens"
				// 就永远无法回落到限额以下,构造上不可恢复。现在每轮失败都强制把
				// 上下文压回预算,打破自强化死循环。
				a.Memory.PruneByBytes(0)

				// 2026-08-10 §20260810-14 增强:Context 超限错误时使用激进压缩。
				// 当 LLM 返回 400 "exceed max message tokens" 等 Context 超限错误时,
				// 普通 PruneByBytes 可能因预算设置过松而无法在单次调用中回落到安全范围。
				// 此时使用 PruneByBytesAggressive(50% 预算)确保快速回落。
				// 典型场景:DouBao 等小窗口模型(上下文窗口 ~128K-256K)累积大量历史后
				// 触发 400,需要激进压缩才能在下一次 LLM 调用前回落到安全范围。
				if isContextExceededError(err) {
					logger.L().Warn("agent: context exceeded error detected, applying aggressive compression",
						zap.String("bot_id", a.BotID()),
						zap.Int("seat", a.Seat),
						zap.String("model", a.ModelKey),
						zap.Error(err))
					a.Memory.PruneByBytesAggressive()
				}
				// 2026-07-15 R131 修复: 任何失败(永久 + retryable)都走 60s 冷却窗口。
				// 此前永久错误绕开 cooldown,2 次连续 401/403 就 ban;现在与 retryable
				// 一视同仁,防止上游抖动导致 1-2 秒内累计到 2 次永久错误。
				// BUG-R48-P0-1: 复述段落已压缩 — git blame 与 docs/ 索引可还原

				now := time.Now()
				transient := isNetworkOrTimeoutTransient(err)
				// 2026-07-24 优化: model_400_circuit 熔断快速失败按"等待恢复"处理,
				// 不计入 consecutiveFailures(与 speak_floor_tick 同级)。熔断器本身
				// 120s 后自动恢复(model400Cooldown),若把熔断期间的快速失败当永久
				// 错误累计,bot 会在熔断恢复前就被永久 quarantine — 这正是 13 人局
				// Kimi/GLM 批量"已禁用 · 连续失败,已停止"的主因。真正的永久性错误
				// (401/403 quota)仍走原有快速 quarantine 路径(见下方 isPermanent)。
				if isModel400CircuitErr(err) {
					transient = true
				}
				// §20260810-15: model_429_circuit 限流熔断同 transient 语义。
				// 上游明确告诉我们"等等再来",不应触发 quarantine;反而需要
				// 等熔断冷却(60s)后恢复。Tencent-model 16:32-16:34 4 次就停的
				// 根因之一就是 429 没单独熔断,每次都走完 retry chain,既浪费配额
				// 又累计 cf → 触发永久 quarantine。
				if isModel429CircuitErr(err) {
					transient = true
				}
				// 2026-08-12 §20260812-04 U5 (P0-3): endpoint breaker 同 transient 语义。
				//
				// 缺陷:model_400 / model_429 两个熔断都有显式特判,唯独 endpoint
				// breaker 没有 —— 而它的错误文案("endpoint breaker open (all
				// endpoints)")与 isNetworkOrTimeoutTransient 的子串表无一匹配,
				// 于是成为**唯一计入 consecutiveFailures 的熔断信号**。
				//
				// endpoint breaker 是 60s 自愈的上游级故障且**全房共享**:一次上游
				// 抖动会让 13 个 bot 同时累计 cf → 批量 quarantine → 10 分钟后判和。
				// 与 §108 retry-cooldown-window 的意图一致:等待恢复 ≠ 该 bot 有问题。
				if isEndpointBreakerErr(err) {
					transient = true
				}
				curCF, inCooldown := a.RecordFailure(now, transient, failCooldownWindow)
				if transient {
					// BUG-R226-P2-01 (2026-08-01): transient 大类内按
					// lastErrorClass 分支输出 msg —— 永久性 400
					// (class="permanent",例如协议格式错误)被旧文案
					// "timeout/network transient" 覆盖,1011/1026 条该日志
					// 名不副实,把运维引向"网络抖动"方向而错过协议真因。
					msg := "agent: LLM timeout/network transient; auto-skip only, not counting toward quarantine"
					switch {
					case isModel400CircuitErr(err):
						msg = "agent: LLM model-400 circuit open; waiting for cooldown, auto-skip only"
					case a.lastErrorClass == "permanent":
						msg = "agent: LLM permanent error absorbed as transient (cooldown); auto-skip only"
					}
					logger.L().Warn(msg,
						zap.Int("seat", a.Seat),
						zap.String("class", a.lastErrorClass),
						zap.Int("consecutive_failures", curCF),
						zap.Error(err))
				} else if inCooldown {
					logger.L().Debug("agent: failure within cooldown window; not incrementing consecutiveFailures",
						zap.Int("seat", a.Seat),
						zap.Bool("retryable", retryable),
						zap.Int("consecutive_failures", curCF))
				}

				// BUG-WEREWOLF-P0-NEW-3: 复述段落已压缩 — git blame 与 docs/ 索引可还原

				isPermanent := !retryable
				// BUG-R172-P2: 复述段落已压缩 — git blame 与 docs/ 索引可还原

				circuitOpen := false
				if isModel400CircuitErr(err) {
					circuitOpen = true
					logger.L().Warn("agent: model 400 circuit open; waiting for cooldown recovery (not counting toward quarantine)",
						zap.Int("seat", a.Seat), zap.String("phase", phase),
						zap.String("model", a.ModelKey),
						zap.Int("consecutive_failures", curCF))
				}
				maxThresh, permThresh := thresholdForSeatCount(evt.Context.SeatCount)
				// 2026-07-22 FIX (R186-A): 用 FailureSnapshot() 一次性读
				// consecutiveFailures + quarantined,避免与 ResetConsecutiveFailures
				// / SetQuarantined 等并发路径产生撕裂 read。
				cfSnapshot, alreadyQuarantined := a.FailureSnapshot()
				// 2026-07-24 优化: circuitOpen 不再单独触发 quarantine(熔断
				// 120s 后自动恢复,consecutiveFailures 已按 transient 不递增);
				// 仅当计数阈值(一般/永久)或已 quarantine 时才进入 quarantine。
				if cfSnapshot >= maxThresh ||
					(isPermanent && cfSnapshot >= permThresh) ||
					alreadyQuarantined {
					// 2026-07-10 §重构 — quarantine 时切到 PhaseQuarantined,前端
					// BotPhaseIndicator 据此渲染"已禁用 · N连失败"红徽章。
					a.SetLLMCallPhase(PhaseQuarantined)
					a.SetQuarantined()
					logger.L().Warn("agent: quarantined for rest of game (permanent LLM failure)",
						zap.String("bot_id", a.BotID()),
						zap.Int("seat", a.Seat), zap.String("phase", phase),
						zap.String("model", a.ModelKey),
						zap.Int("consecutive_failures", cfSnapshot),
						zap.Bool("permanent", isPermanent),
						zap.Bool("model_400_circuit", circuitOpen),
						zap.String("last_error", err.Error()))
					return
				}

				// BUG-WEREWOLF-P0-2: 复述段落已压缩 — git blame 与 docs/ 索引可还原

				if a.ConsecutiveFailures() >= failAutoSkipThreshold {
					currentPhase, currentRole, _, _, currentSpeakTurn, currentTurnActing, _ := rp()
					if currentPhase == "" || currentPhase == phase {
						// BUG-WEREWOLF-P0-SPEAK-AUTOSKIP: only the acting seat
						// should auto-skip. In speak phase non-speaker agents
						// get woken for sync; their LLM failure doesn't warrant
						// dispatching finish_speak on their behalf.
						if ShouldAutoSkip(phase, seat, evt.Context, currentSpeakTurn, currentTurnActing) {
							if skipName, skipArg := SkipPhaseAction(phase, role); skipName != "" {
								logger.L().Warn("agent: failure threshold exceeded; auto-skipping phase action",
									zap.String("bot_id", a.BotID()),
									zap.Int("seat", a.Seat), zap.String("phase", phase),
									zap.String("role", role), zap.String("model", a.ModelKey),
									zap.Int("failures", a.ConsecutiveFailures()),
									zap.String("skip_tool", skipName))
								input := map[string]any{}
								switch skipName {
								case "wolf_kill":
									input["target"] = skipArg
								case "seer_check":
									// Default to seat 0 (alive most of the time); engine
									// will reject if dead. Better than -1.
									target := 0
									for _, s := range alive {
										if s != seat {
											target = s
											break
										}
									}
									input["target"] = target
								}
								if result, derr := DispatchTool(skipName, input, runner); derr != nil {
									logger.L().Warn("agent: auto-skip dispatch failed",
										zap.Int("seat", a.Seat), zap.String("skip_tool", skipName),
										zap.Error(derr))
								} else {
									logger.L().Info("agent: auto-skip dispatched",
										zap.Int("seat", a.Seat), zap.String("skip_tool", skipName),
										zap.String("result", result))
								}
							}
						}
					} else {
						// Phase moved on while we were failing the LLM. The
						// next wake will handle the new phase; just log.
						logger.L().Info("agent: auto-skip skipped because phase moved on",
							zap.Int("seat", a.Seat),
							zap.String("expected_phase", phase),
							zap.String("now_phase", currentPhase),
							zap.String("expected_role", role),
							zap.String("now_role", currentRole))
					}
				}
				// Self-re-wake after delay so the agent gets another chance
				// when the transient failure clears. Without this, the agent
				// would be stuck forever — no new wake comes because no
				// action was taken, and no action can be taken without a wake.
				//
				// BUG-R232-P1-02: 复述段落已压缩 — git blame 与 docs/ 索引可还原

				reWakeDelayForThisCycle := reWakeDelay
				if circuitOpen {
					reWakeDelayForThisCycle = circuitOpenMinReWakeDelay
					if a.circuitOpenFailureCount%circuitOpenLogEveryN == 1 {
						logger.L().Warn("agent: circuit-open failure (rate-limited log; next in N cycles)",
							zap.Int("seat", a.Seat), zap.String("phase", phase),
							zap.String("model", a.ModelKey),
							zap.Int("circuit_open_failure_count", a.circuitOpenFailureCount),
							zap.Int("log_every_n", circuitOpenLogEveryN))
					} else {
						logger.L().Debug("agent: circuit-open failure (suppressed log)",
							zap.Int("seat", a.Seat), zap.String("phase", phase),
							zap.String("model", a.ModelKey),
							zap.Int("circuit_open_failure_count", a.circuitOpenFailureCount))
					}
					a.circuitOpenFailureCount++
				}
				go a.scheduleReWake(ctx, reWakeDelayForThisCycle)
				return
			}
			// 修复(2026-08-04)§计数器复位 — 原 a.ResetConsecutiveFailures()
			// 调用位于此处(retry-success 路径),已上提到 `if err != nil {}`
			// 块之后统一执行,避免"首次调用即成功"时永不复位。
			// §130: 复述段落已压缩 — git blame 与 docs/ 索引可还原

			a.Memory.CompressAndPrune(DefaultPruneTurns, DefaultCompressTurns)
		}

		// 修复(2026-08-04)§计数器复位 —— 走到这里 err 必为 nil(err != nil 的
		// 所有分支要么在内层重试后置 err=nil,要么已 return),即"本轮 LLM 调用成功"。
		//
		// 缺陷:ResetConsecutiveFailures()(同时清零 consecutiveFailures /
		// emotionSwitchAloneCount / speakTurnIdleSilentCount)原先只写在
		// `if err != nil { ... }` 块**内部**的末尾,也就是只有"重试之后才成功"
		// 才会执行。首次调用即成功(绝大多数情况)从不复位 →
		// emotionSwitchAloneCount 单调递增(实测某 agent 涨到 229),
		// 前 3 次之后 3-strike 守卫永久退化成"exceeded 3, allowing through"
		// (日志 462 次)。现在无论是否发生重试,成功路径都恰好复位一次。
		a.ResetConsecutiveFailures()

		a.recordAssistant(resp)

		// BUG-WEREWOLF-P0-NEW-2: publish this decision (thinking + recent
		// tools/messages) to the spectator AgentThoughtPanel. Captured right
		// after the assistant turn is recorded so last_thinking is always
		// fresh; tool_calls lag by at most one round (this round's tools are
		// dispatched below). Runs on every path (no-tool yield, tool dispatch,
		// speak yield) because it precedes all early returns.
		a.recordTranscript()

		// 2026-07-09 §13-bugfix — 推进共享 chatQueue 的 read pointer。
		// 把本 bot 的 read pointer 推到当前 queue 的最新 seq,确保下次
		// WindowFor(seat) 只返回这次 LLM 调用之前未看过的新消息(增量注入)。
		// 若 chatQueue 未启用(nil)则跳过,不影响主流程。
		if cq := a.ChatQueue(); cq != nil {
			cq.Advance(a.Seat, cq.SnapshotLastSeq())
		}

		logger.L().Debug("agent: llm responded",
			zap.Int("seat", a.Seat), zap.String("phase", phase),
			zap.String("stop_reason", resp.StopReason),
			zap.Int("tool_uses", len(resp.ToolUses())))

		if resp.StopReason != "tool_use" || len(resp.ToolUses()) == 0 {
			// LLM chose not to act (end_turn / refusal / max_tokens).
			// BUG-WEREWOLF-P0-SPEAK-IDLE: previously the agent simply yielded
			// here with no recovery. In action-required phases (speak, vote,
			// dawn, sheriff) this permanently deadlocked the phase because no
			// one would ever call finish_speak / finish_vote / start_day /
			// sheriff_elect to advance the engine. The auto-skip mechanism
			// only triggered on LLM *errors* (consecutiveFailures), not on
			// "success but idle" responses.
			//
			// Fix: re-fetch the live phase from rp() and, if it still
			// matches, auto-dispatch the phase's default skip action so
			// the engine keeps moving. This mirrors the error-path auto-skip
			// (lines 439-484) but fires on "success + no tool_use".
			currentPhase, _, _, _, currentSpeakTurn, currentTurnActing, _ := rp()
			if currentPhase == "" || currentPhase == phase {
				// BUG-WEREWOLF-P0-SPEAK-AUTOSKIP: in speak phase only the
				// current speaker should auto-skip. Non-speaker agents get
				// woken by wakeAll() for transcript sync and should just
				// yield silently on end_turn.
				if ShouldAutoSkip(phase, seat, evt.Context, currentSpeakTurn, currentTurnActing) {
					if skipName, skipArg := SkipPhaseAction(phase, role); skipName != "" {
						logger.L().Warn("agent: no tool_use in action-required phase; auto-skipping",
							zap.Int("seat", a.Seat), zap.String("phase", phase),
							zap.String("role", role), zap.String("model", a.ModelKey),
							zap.String("skip_tool", skipName),
							zap.String("stop_reason", resp.StopReason))
						input := map[string]any{}
						switch skipName {
						case "wolf_kill":
							input["target"] = skipArg
						case "seer_check":
							target := 0
							for _, s := range alive {
								if s != seat {
									target = s
									break
								}
							}
							input["target"] = target
						}
						if result, derr := DispatchTool(skipName, input, runner); derr != nil {
							logger.L().Warn("agent: no-tool-use auto-skip dispatch failed",
								zap.Int("seat", a.Seat), zap.String("skip_tool", skipName),
								zap.Error(derr))
						} else {
							logger.L().Info("agent: no-tool-use auto-skip dispatched",
								zap.Int("seat", a.Seat), zap.String("skip_tool", skipName),
								zap.String("result", result))
						}
					}
				} else {
					logger.L().Debug("agent: no tool_use, not acting seat; yielding",
						zap.Int("seat", a.Seat), zap.String("phase", phase),
						zap.Int("speak_turn", evt.Context.SpeakTurn),
						zap.String("stop_reason", resp.StopReason))
				}
			} else {
				logger.L().Debug("agent: no tool_use, yielding (phase moved on)",
					zap.Int("seat", a.Seat), zap.String("expected_phase", phase),
					zap.String("now_phase", currentPhase),
					zap.String("stop_reason", resp.StopReason))
			}
			// 2026-07-08 §13.5: spectator_speech 事件下 LLM end_turn 但没调任何工具,
			// 主动追加 [idle_silent] 审计行。auto-skip 已发生视为"已决策"不补。
			if spectatorAwake {
				a.appendIdleAuditLine(fmt.Sprintf("LLM end_turn 无工具调用(seat=%d, phase=%s, role=%s)",
					seat, phase, role))
			}
			return
		}

		// 3. Dispatch tool_use blocks. After speak/whisper, mark the limiter
		//    and exit the inner loop so we yield to the next event (avoid
		//    double-speaking within one event cycle).
		//    - speak → 30s 令牌桶 (Limiter)
		//    - whisper → 60s 令牌桶 (WhisperLimiter)，与发言独立计数，
		//      防止 Agent 用私聊绕过发言限流。
		saidSomething := false
		// 2026-08-04 §表情特效聚合约束 — 工具派发前预扫描:
		//  1. emotion_switch_speak 最多 1 次;多次以最后一次为准,前面的标记 [superseded] 并丢弃;
		//  2. emotion_switch_speak 不能与 speak / speak_with_thought 同响应,
		//     同现时整组 emotion_switch_speak 视为冲突,返回错误让 LLM 重跑;
		//  3. speak + speak_with_thought 同现时也视为冲突,丢弃 speak_with_thought。
		// 这是对 tools.go 描述中「[superseded] 聚合逻辑」注释的实现补全 —— 该注释
		// 此前长期没有对应代码。
		essLastIdx := -1
		essCount := 0
		speakCount := 0
		for i, tu := range resp.ToolUses() {
			switch tu.Name {
			case "emotion_switch_speak":
				essCount++
				essLastIdx = i
			case "speak", "speak_with_thought":
				speakCount++
			}
		}
		essConflict := essCount > 0 && speakCount > 0
		if essConflict {
			logger.L().Warn("agent: emotion_switch_speak 与 speak/speak_with_thought 同响应,整组视为冲突(将跳过)",
				zap.Int("seat", a.Seat), zap.String("phase", phase),
				zap.Int("ess_count", essCount), zap.Int("speak_count", speakCount))
		}
		supersededIDs := make(map[string]bool, essCount-1)
		for i, tu := range resp.ToolUses() {
			if tu.Name == "emotion_switch_speak" && i != essLastIdx {
				supersededIDs[tu.ID] = true
			}
		}
		for _, tu := range resp.ToolUses() {
			// 聚合丢弃:非最后一次的 emotion_switch_speak
			if supersededIDs[tu.ID] {
				a.recordToolResult(tu.ID, "[superseded] emotion_switch_speak superseded by later call", false)
				logger.L().Debug("agent: emotion_switch_speak superseded (later call wins)",
					zap.Int("seat", a.Seat), zap.String("tool_use_id", tu.ID))
				continue
			}
			// 冲突丢弃:emotion_switch_speak 与 speak/speak_with_thought 同响应
			if essConflict && tu.Name == "emotion_switch_speak" {
				a.recordToolResult(tu.ID, "emotion_switch_speak rejected: 与 speak/speak_with_thought 不能同响应,请单独使用", false)
				continue
			}
			if essConflict && tu.Name == "speak_with_thought" {
				// 保留 speak,丢弃 speak_with_thought(后者更容易因同响应被拒)
				a.recordToolResult(tu.ID, "speak_with_thought rejected: 与 speak/emotion_switch_speak 不能同响应", false)
				continue
			}
			// v3 §G2 — 在 DispatchTool 前同步设置 currentGC,
			// 让 prop_inspect/prop_status/prop_history 三个查询工具能拿到本轮数据。
			if setter, ok := runner.(interface{ SetCurrentGC(*wwtypes.GameContext) }); ok {
				setter.SetCurrentGC(&evt.Context)
			}
			result, derr := DispatchTool(tu.Name, tu.Input, runner)
			a.recordToolResult(tu.ID, result, derr != nil)
			// R85-P2: 记录本次工具调用到 Memory.tools,供 BotTranscript.ToolCalls
			// (前端"🔧 工具调用 最近 5 条"面板)经 RecentTools(5) 渲染。此前
			// PushTool 从未被调用,m.tools 恒为空 -> 面板永远显示"(暂无)",即使
			// bot 实际调用了 finish_vote/vote。敏感工具(wolf_kill/seer_check/
			// witch_act/hunter_shoot)结果由 sanitizeBotTranscript 替换为 [已隐藏]。
			if a.Memory != nil {
				a.Memory.PushTool(ToolRecord{Name: tu.Name, Input: tu.Input, Result: result})
			}
			logger.L().Debug("agent: tool dispatched",
				zap.Int("seat", a.Seat), zap.String("phase", phase),
				zap.String("tool", tu.Name), zap.Bool("err", derr != nil))
			// §20260810-13: 单次行动工具(wolf_kill/seer_check 等)成功后标记
			// actionDone,内层循环下一轮直接退出,避免无意义的后续 LLM 调用。
			// 失败时(derr != nil)不标记,让 LLM 有机会修正后重试(受 maxRounds 限制)。
			if derr == nil && isSingleActionTool(tu.Name) {
				actionDone = true
				logger.L().Debug("agent: single-action tool succeeded, will exit inner loop",
					zap.Int("seat", a.Seat), zap.String("phase", phase),
					zap.String("tool", tu.Name))
			}
			if tu.Name == "speak" {
				saidSomething = true
				a.Limiter.Mark()
			} else if tu.Name == "speak_with_thought" {
				// 2026-07-10 §119「心口不一」工具:与 speak 共用同一个 45s
				// 令牌桶,防止 LLM 用 speak + speak_with_thought 两条工具
				// 绕过限流刷屏。runner 侧 (agentRunner.SpeakWithThought) 已
				// 与 Speak 共享限流检查;此处再 Mark 一次保证 Agent.Limiter
				// 状态同步。
				saidSomething = true
				a.Limiter.Mark()
			} else if tu.Name == "whisper" {
				saidSomething = true
				a.MarkWhisper()
			} else if tu.Name == "interject" {
				// BUG-WEREWOLF-AGENT-INTERJECT: 插话走与 speak 相同的 30s
				// 令牌桶(≤2 次/分钟),防止 bot 用 interject 绕过发言限流。
				saidSomething = true
				a.Limiter.Mark()
			} else if tu.Name == "idle_silent" {
				// §128 对话即思考重构:LLM 主动调 idle_silent (合并了原 idle_think)
				// 视为"已主动选择沉默",由工具本身在 BotTranscript 留 audit,这里把
				// saidSomething 置 true 以便 §13.5 自动审计路径不再补行。
				saidSomething = true
			} else if tu.Name == "emotion_switch_speak" {
				// 2026-08-04 §表情特效:emotion_switch_speak 内部已走 r.Speak 完整过滤链
				// (rate-limit/dedup/identity-leak/chatSvc.SendFromBot),这里同步令牌桶 + 标记
				// saidSomething,避免下面 text-block auto-speech 路径把同一条发言再发一次。
				// 根因修复:此前该分支完全缺失,bot 调了此工具后仍走裸 text fallback,emotion
				// 特效从未真正生效,且令牌桶状态落后于实际发言,45s 限流失准。
				saidSomething = true
				a.Limiter.Mark()
			}
			// 2026-07-09 §重构 - 决策可观测性 STAGE 4:把"最后一个 tool_use"
			// 写入 RecordDecisionState,供 recordTranscript 下发到 wire。
			// 我们只追踪"最后一个有效决策",因为前端只展示 1 行(避免展示 5 行
			// 重复的 vote/speak 链)。使用 MergeLastDecision 保留 STAGE 1 设置的
			// DecisionInputs。
			outcome := "OK"
			if derr != nil {
				outcome = "FAIL"
			}
			a.MergeLastDecision(RecordDecisionState{
				LastToolName:   tu.Name,
				LastToolInput:  tu.Input,
				LastToolResult: result,
				LastOutcome:    outcome,
			})
		}
		// 2026-08-04 §重构 — emotion_switch 单独工具已删除(合并到 emotion_switch_speak)。
		// emotion-only 三次重试逻辑不再需要,见 docs/Agent工具定义-解决和设计方案-20260804-01.md。

		// 2026-07-29 修复:speak 阶段当前发言者不可仅调 idle_silent。
		// 若当前是 speakTurn 且 LLM 只调了 idle_silent,视为无效调用,让 LLM 重试。
		if !saidSomething && len(resp.ToolUses()) > 0 {
			if (phase == "speak" || phase == "PhaseSpeak") && evt.Context.SpeakTurn == seat {
				onlyIdleSilent := true
				for _, tu := range resp.ToolUses() {
					if tu.Name != "idle_silent" {
						onlyIdleSilent = false
						break
					}
				}
				if onlyIdleSilent {
					a.speakTurnIdleSilentCount++
					logger.L().Warn("agent: speak turn but only idle_silent; rejected (will retry with speak)",
						zap.Int("seat", a.Seat), zap.String("phase", phase),
						zap.Int("speak_turn_idle_count", a.speakTurnIdleSilentCount))
					if a.speakTurnIdleSilentCount < 3 {
						a.MergeLastDecision(RecordDecisionState{
							LastToolName:   "idle_silent",
							LastToolResult: "idle_silent rejected: 当前发言轮到你,请用 speak 或 speak_with_thought 发言,不可跳过",
							LastOutcome:    "REJECTED_SPEAK_TURN",
						})
						return
					}
					logger.L().Warn("agent: speak turn idle_silent count exceeded 3, allowing through",
						zap.Int("seat", a.Seat))
				} else {
					a.speakTurnIdleSilentCount = 0
				}
			}
		}

		if saidSomething {
			trackedSpoken = true
			return
		}

		// 2026-07-15 BUG-R124-PERF-01: 兜底 — 若 LLM 流式输出阶段已经产生了
		// 文本(streamText 非空)但 resp.Text() 返回空(典型:Provider 在
		// streaming 完成后没返回 aggregated text,或者 LLM 在 content 数组里
		// 只产生了 streaming chunk 但没有 final block),用 streamText 替代
		// 走 SpeakAuto,避免「流式面板看到内容但最终消息为空」。
		// 注意: 仅在 streamText 显著非空(> 6 字)时启用,避免噪声。
		if phaseAllowsPublicSpeech(phase) {
			// BUG-R233-P1-01 (2026-08-02): 同上,streamText-fallback 路径也必须 TrimSpace
			// —— 纯 6 个 newline 也能满足 `streamText.Len() > 6` 但实际不可读。
			if streamText.Len() > 6 && strings.TrimSpace(streamText.String()) != "" {
				// BUG-HEART-LEAK-P0: LLM 同时产出 text block + 非发言类 tool_use
				// (如 vote) 时,text 内容可能是推理/内心独白而非有意公开发言。
				// 原 !saidSomething 只追踪发言类工具,对 vote/seer_check 等不置位,
				// 导致 thinking text 经 SpeakAuto 泄漏到公聊(§119 协议层隔离被破坏)。
				// 修复:加 len(resp.ToolUses())==0 守卫 — 有任何 tool_use 说明 LLM
				// 已通过工具系统做出了决策,text block 仅是辅助推理,不应自动广播。
				if autoText := resp.Text(); autoText == "" && !saidSomething && len(resp.ToolUses()) == 0 {
					if autoAR, ok := runner.(interface {
						SpeakAuto(text string) (string, error)
					}); ok {
						fb := streamText.String()
						ar, aerr := autoAR.SpeakAuto(fb)
						logger.L().Info("agent: streamText-fallback auto-speech dispatched (R124-PERF-01)",
							zap.Int("seat", a.Seat), zap.String("phase", phase),
							zap.String("role", role), zap.String("model", a.ModelKey),
							zap.Int("stream_len", len(fb)), zap.String("result", ar),
							zap.Error(aerr))
						if aerr == nil && ar != "" && !strings.HasPrefix(ar, "speak_auto rate-limited") &&
							!strings.HasPrefix(ar, "speak_auto rejected") {
							trackedSpoken = true
							a.Limiter.Mark()
							return
						}
					}
				}
			}
		}

		// §130: 复述段落已压缩 — git blame 与 docs/ 索引可还原

		if phaseAllowsPublicSpeech(phase) {
			// BUG-R233-P1-01 (2026-08-02): 仅判 != "" 会放过 LLM 返回的纯空白字符
			// (GLM 在 19:11/19:45/20:08 等出现的 " " / "\n")—— 这些会走完整条过滤链
			// 然后发出空气泡,同时挤占 speakLimiter + 同座位冷却 + 阶段计数 3 类令牌,
			// 等同于用一条谁也看不见的消息挤掉真实发言机会。修复:提前 TrimSpace 判空,
			// 让空内容直接不进入 SpeakAuto,令牌不消耗。
			// BUG-HEART-LEAK-P0: 与上方 streamText-fallback 路径同源守卫。
			// LLM 产出 text + tool_use(如 vote/text_vote) 时,text 是推理不是发言。
			if autoText := strings.TrimSpace(resp.Text()); autoText != "" && !saidSomething && len(resp.ToolUses()) == 0 {
				if autoAR, ok := runner.(interface {
					SpeakAuto(text string) (string, error)
				}); ok {
					ar, aerr := autoAR.SpeakAuto(autoText)
					logger.L().Info("agent: text-block auto-speech dispatched",
						zap.Int("seat", a.Seat), zap.String("phase", phase),
						zap.String("role", role), zap.String("model", a.ModelKey),
						zap.Int("text_len", len(autoText)),
						zap.String("result", ar), zap.Error(aerr))
					if aerr == nil && ar != "" && !strings.HasPrefix(ar, "speak_auto rate-limited") &&
						!strings.HasPrefix(ar, "speak_auto rejected") {
						// 自动发言成功,触发 early-return + trackedSpoken,
						// 等同于 LLM 通过 tool_use speak 的语义。
						trackedSpoken = true
						a.Limiter.Mark()
						return
					}
				}
			}
		}
	}

	// §20260810-13 / BUG-WEREWOLF-P0-SPEAK-IDLE: 内层循环达到最大轮次仍未完成
	// 任何有意义的行动。与上方"no tool_use"路径相同的死锁风险:在需要主动行动
	// 的阶段,没有人调用终端动作。此处 auto-skip 作为安全网。
	// 历史:§130 前该段处理 MaxToolUse(旧全局硬上限)用尽的情况;
	// §20260810-13 后改为 maxRounds(阶段差异化上限)触发的同一兜底逻辑。
	// BUG-WEREWOLF-P0-SPEAK-AUTOSKIP: only the acting seat should fire the
	// skip (in speak phase, only the current speaker).
	currentPhase, _, _, _, currentSpeakTurn, currentTurnActing, _ := rp()
	if currentPhase == "" || currentPhase == phase {
		if ShouldAutoSkip(phase, seat, evt.Context, currentSpeakTurn, currentTurnActing) {
			if skipName, skipArg := SkipPhaseAction(phase, role); skipName != "" {
				logger.L().Warn("agent: inner loop max rounds reached with no action; auto-skipping",
					zap.Int("seat", a.Seat), zap.String("phase", phase),
					zap.String("role", role), zap.String("model", a.ModelKey),
					zap.Int("max_rounds", maxRounds),
					zap.String("skip_tool", skipName))
				input := map[string]any{}
				switch skipName {
				case "wolf_kill":
					input["target"] = skipArg
				case "seer_check":
					target := 0
					for _, s := range alive {
						if s != seat {
							target = s
							break
						}
					}
					input["target"] = target
				}
				if result, derr := DispatchTool(skipName, input, runner); derr != nil {
					logger.L().Warn("agent: max-rounds auto-skip dispatch failed",
						zap.Int("seat", a.Seat), zap.String("skip_tool", skipName),
						zap.Error(derr))
				} else {
					logger.L().Info("agent: max-rounds auto-skip dispatched",
						zap.Int("seat", a.Seat), zap.String("skip_tool", skipName),
						zap.String("result", result))
				}
			}
		}
	}

	// 2026-07-08 §13.5 "必须思考" 兜底:如果本轮 handleEvent 因 spectator_speech
	// 唤醒但 LLM end_turn 没调任何工具(speak/whisper/interject/idle_silent),
	// 主动追加一条 [idle_silent] 审计行,zero LLM token cost。
	// trackedSpoken 在循环内被置 true 表明 LLM 已做出显式决策;否则视为
	// "无操作退出"(典型:max_rounds 用尽 + no-tool-use 分支直接 return,
	// 或 dispatcher 错误导致所有 tu 都被 skip)。
	if spectatorAwake && !trackedSpoken {
		a.appendIdleAuditLine(fmt.Sprintf("LLM 未调任何工具(seat=%d, phase=%s, role=%s)",
			seat, phase, role))
	}
}

// handleSpeakFloorTick 是 2026-07-09 §13 增强路径 — 处理 WerewolfManager.speakFloorWatchdog
// 推送的 AgentEvent{Kind:"speak_floor_tick"} 事件。
//
// 与普通 handleEvent 的关键差异:
//   - **跳过 LLMCallLimiter**(8s 间隔):speak floor 是强提醒,不受 bot 自身限流影响。
//   - **跳过 agentcore.SpeakLimiter**(45s 间隔):不能让 cooldown 阻挡下限要求。
//   - **追加强 prompt**:user message 末尾注入【发言下限】块,LLM 应立即调 speak。
//   - **复用同一条 handleEvent 基础设施**:Memory push / recordTranscript / Prune。
//
// 与原 handleEvent 的"§13.5 audit 兜底"和"§83a auto-skip"协同:
//   - 如果 LLM 在 floor wake 中调 speak → recordSpeakDaytime + Limiter.Mark + 返回。
//   - 如果 LLM end_turn 无工具 → 走原 handleEvent 的 auto-skip 路径,manager 兜底。
//
// phase / role / seat 参数由 handleEvent 入口 rp() 调用提前解析(避免重复活锁)。
func (a *Agent) handleSpeakFloorTick(ctx context.Context, runner ToolRunner, rp RolePhase, evt AgentEvent, phase, role string, seat int) {
	// 重新 fetch live phase(防止 stale event)
	currentPhase, currentRole, currentSeat, alive, _, _, done := rp()
	if done || currentPhase == "" {
		return
	}
	// 用 live 覆盖参数
	if phase == "" {
		phase = currentPhase
	}
	if role == "" {
		role = currentRole
	}
	if seat < 0 {
		seat = currentSeat
	}

	// 死态守卫(§95)
	for _, s := range alive {
		if s == seat {
			goto alive
		}
	}
	logger.L().Debug("agent: speak_floor_tick ignored (dead seat)",
		zap.Int("seat", seat), zap.String("phase", phase))
	return
alive:

	// 1. 计算当前 60s 窗口计数,作为 prompt 强提示
	now := a.now()
	_, currentCount := a.allowSpeakDaytime(now)
	// 假设 minSpeaks = 2(默认);若已达 minSpeaks,直接返回(理论上 manager 不该 wake 我们)
	if currentCount >= 2 {
		logger.L().Debug("agent: speak_floor_tick ignored (already at floor)",
			zap.Int("seat", seat), zap.Int("count", currentCount))
		return
	}

	// §14: 复述段落已压缩 — git blame 与 docs/ 索引可还原

	tools := BuildTools(phase, role, seat, alive, evt.Context.SpeakTurn, &evt.Context)
	if len(tools) == 0 {
		logger.L().Debug("agent: speak_floor_tick no tools; yielding",
			zap.Int("seat", seat), zap.String("phase", phase))
		return
	}
	// 按优先级挑出 live phase 中真实存在的发言类工具。
	speechTool := ""
	for _, want := range []string{"speak", "speak_with_thought", "interject"} {
		for _, tl := range tools {
			if tl.Name == want {
				speechTool = want
				break
			}
		}
		if speechTool != "" {
			break
		}
	}
	if speechTool == "" {
		logger.L().Debug("agent: speak_floor_tick no speech tool in live phase; yielding",
			zap.Int("seat", seat), zap.String("phase", phase), zap.String("role", role))
		return
	}

	// 3. 构造 user prompt:复用 BuildUserPrompt 基础段 + 末尾注入【发言下限】块
	basePrompt := BuildUserPrompt(evt.Context)
	floorHint := "\n【发言下限】60s 内你已发 " + itoa(currentCount) + " 次,尚未达到本房间的每分钟发言下限。" +
		"请立即调 " + speechTool + " 工具补发(≤100 字即可),否则房间会强制推进。\n"
	// 2026-07-20 §131 新增 — 注入持久化记忆(MEMORY.md),与主 handleEvent 路径对称。
	// 2026-08-12 §20260812-04 U4 — 与主路径一致改为按角色选取 + 难度预算。
	floorHint += InjectBlockForRole(a.MemoryMD, evt.Context.Role, a.memoryInjectRunes)
	a.Memory.Push(llm.Message{
		Role:    "user",
		Content: []llmtypes.ContentBlock{{Type: "text", Text: basePrompt + floorHint}},
	})

	logger.L().Info("agent: speak_floor_tick fired",
		zap.Int("seat", seat), zap.String("phase", phase), zap.String("role", role),
		zap.String("speech_tool", speechTool),
		zap.Int("current_count", currentCount))

	// 4. Build request (跳过 LLMCallLimiter 检查 — 强提醒必须能调 LLM)
	msgs, _ := a.Memory.Snapshot()
	msgs, patched := SanitizeMessagesForAnthropic(msgs)
	if patched > 0 {
		logger.L().Info("agent: sanitized messages (speak_floor_tick)",
			zap.Int("seat", seat), zap.Int("patched_tool_uses", patched))
	}
	req := llm.LLMRequest{
		Model:    resolveModelName(a.ModelKey),
		System:   BuildSystemPrompt(a.SelfPortraitText, a.Personality, a.PersonalityPresetKey, a.DifficultyDirective),
		Messages: msgs,
		Tools:    tools,
		// 2026-08-06 §AgentClassName 增强:让上游 / 网关 / 日志按"哪一类 Agent"
		// 区分调用方 — 玩家 Bot 用 AgentClassWerewolfPlayer,法官用
		// AgentClassWerewolfJudge,记忆迭代用 AgentClassWerewolfMemoryIter。
		// 常量集中在 ServerGo/agent/class_names.go。
		AgentClassName: string(agentroot.AgentClassWerewolfPlayer),
		// 2026-07-17 R139 修复:与 handleEvent 主路径对称,从 1024 提到 2048,
		// 避免 Kimi-model 中文长 thinking + tool_use 拼接近 1024 token 上限。
		MaxTokens: 2048,
		Metadata:  llmtypes.Metadata{UserID: buildMetadataUserID(a)},
	}
	// §128 对话即思考重构:thinking 注入已删除。

	// BUG-R242-P1-01: 与主 handleEvent 路径对称 — 房间级 LLM 并发信号量。
	// speak_floor 是可选提醒(沉默 Agent 的额外发言机会),槽位满时直接放弃本轮
	// (manager 下个 5s tick 会再次 wake),不阻塞、不计入 consecutiveFailures。
	if !a.AcquireLLMSlot(llmSlotAcquireWait) {
		logger.L().Debug("agent: speak_floor_tick slot saturated; skipping this tick",
			zap.Int("seat", seat), zap.String("phase", phase),
			zap.String("model", a.ModelKey))
		return
	}
	defer a.ReleaseLLMSlot()

	// 6. 调用 LLM
	// 2026-07-09 §13-bugfix: mark in-progress so panel shows the live timer.
	a.MarkLLMCallStart()
	// 2026-07-30 §统计增强 — 成功时累加 Token；失败路径不计入（speak_floor 失败
	// 不应计入 consecutiveFailures，也不计入 apiFailCount，仅 log + return）。
	var floorUsage llmtypes.LLMUsage
	var floorErr error
	defer func() {
		// 失败路径 floorErr != nil 直接 return，defer 不累加（speak_floor 失败是可选提醒）。
		// 2026-08-01 BUG-R225-P2-03: 失败分支改用纯复位变体,不污染 apiSuccessCount。
		if floorErr == nil {
			a.MarkLLMCallEndWithUsage(floorUsage)
		} else {
			a.ResetLLMCallState()
		}
	}()

	streamProgressFloor := func(ev llmtypes.StreamEvent) error {
		if ev.Type == "_first_token" {
			a.SetLLMCallPhase(PhaseStreaming)
		}
		return nil
	}

	resp, floorErr := a.callProvider(ctx, req, streamProgressFloor)
	// 2026-07-30 §统计增强 — 保存 usage 供 defer 累加。
	if floorErr == nil {
		floorUsage = resp.Usage
	}
	if floorErr != nil {
		// 与主 handleEvent 不同 — speak_floor 是可选提醒,失败不应计入 consecutiveFailures
		// (只是少了一次提醒机会;manager 下个 5s tick 会再次 wake)。
		logger.L().Warn("agent: speak_floor_tick LLM call failed; will retry next tick",
			zap.Int("seat", seat), zap.Error(floorErr))
		a.SetLastError(floorErr.Error())
		return
	}
	a.ResetConsecutiveFailures()
	// §130 重构(2026-07-13):LLMCallLimiter.Mark 已删除 — speak_floor 路径不再有最小间隔锁定。
	// 2026-07-11 §126 增强: 压缩 + 修剪在一次锁内完成(speak_floor 路径)。
	a.Memory.CompressAndPrune(DefaultPruneTurns, DefaultCompressTurns)

	a.recordAssistant(resp)
	a.recordTranscript()

	// 2026-07-09 §13-bugfix — 与主 handleEvent 对称,推进 read pointer。
	if cq := a.ChatQueue(); cq != nil {
		cq.Advance(a.Seat, cq.SnapshotLastSeq())
	}

	logger.L().Debug("agent: speak_floor_tick llm responded",
		zap.Int("seat", seat), zap.String("phase", phase),
		zap.String("stop_reason", resp.StopReason),
		zap.Int("tool_uses", len(resp.ToolUses())))

	// 7. 派发工具 — 重点关注 speak;其他工具走正常路径
	for _, tu := range resp.ToolUses() {
		result, derr := DispatchTool(tu.Name, tu.Input, runner)
		a.recordToolResult(tu.ID, result, derr != nil)
		if derr != nil {
			logger.L().Warn("agent: speak_floor_tick tool dispatch failed",
				zap.Int("seat", seat), zap.String("tool", tu.Name), zap.Error(derr))
			continue
		}
		if tu.Name == "speak" {
			// BUG-R71-EMPTY-SPEAK: 复述段落已压缩 — git blame 与 docs/ 索引可还原

			if strings.TrimSpace(result) == "" {
				logger.L().Warn("agent: speak_floor_tick speak result empty after dedup; not counting",
					zap.Int("seat", seat), zap.String("phase", phase))
				return
			}
			// 关键路径 — speak 成功:同时 record 计数器 + Limiter(全局) + Limiter (floor)
			a.recordSpeakDaytime(now)
			a.Limiter.Mark()
			logger.L().Info("agent: speak_floor_tick → speak succeeded",
				zap.Int("seat", seat), zap.String("phase", phase),
				zap.Int("result_len", len(result)))
			return
		} else if tu.Name == "speak_with_thought" {
			// 2026-07-10 §119: 心口不一发言也算公开发言,计入 60s 窗口。
			if strings.TrimSpace(result) == "" {
				logger.L().Warn("agent: speak_floor_tick speak_with_thought empty; not counting",
					zap.Int("seat", seat), zap.String("phase", phase))
				return
			}
			a.recordSpeakDaytime(now)
			a.Limiter.Mark()
			logger.L().Info("agent: speak_floor_tick → speak_with_thought",
				zap.Int("seat", seat), zap.String("phase", phase))
			return
		} else if tu.Name == "interject" {
			a.recordSpeakDaytime(now)
			a.Limiter.Mark()
			logger.L().Info("agent: speak_floor_tick → interject",
				zap.Int("seat", seat), zap.String("phase", phase))
			return
		} else if tu.Name == "whisper" {
			a.MarkWhisper()
			logger.L().Info("agent: speak_floor_tick → whisper",
				zap.Int("seat", seat), zap.String("phase", phase))
			return
		} else if tu.Name == "idle_silent" {
			// §128 对话即思考重构:LLM 主动调 idle_silent (合并了原 idle_think)
			// 选择沉默;floor 目标未达成,manager 下个 tick 会再 wake。
			logger.L().Info("agent: speak_floor_tick → idle_silent (silence chosen)",
				zap.Int("seat", seat), zap.String("phase", phase))
			return
		}
	}
	// LLM end_turn 或没调任何工具 — 不强制补救,manager 下个 tick 会再 wake。
	logger.L().Debug("agent: speak_floor_tick yielded without action; manager will retry next tick",
		zap.Int("seat", seat), zap.String("phase", phase),
		zap.String("stop_reason", resp.StopReason))
}

// resolveModelName maps a registry model_key to the wire model id. For the
// default 8 providers the model_key IS the wire id (e.g. "MeiTuan-model"),
// so this is the identity function. Override here if a provider ever needs a
// different wire name.

// callProvider abstracts the Chat vs ChatStreamAccumulate dispatch. Both
// paths run the same pre-flight normalizations the anthropic package
// performs; the streaming wrapper also accumulates the SSE body back into a
// complete LLMResponse so the rest of handleEvent doesn't have to care
// whether the underlying call was streamed or not.
//
// §127 onProgress 用于流式首 token 到达时切换 Agent phase 到 streaming;
// 非 streaming provider / 测试桩忽略即可。

// buildMetadataUserID produces the stringified JSON `metadata.user_id` blob
// for an outbound LLM call. Mirrors ClaudeCode's wire shape (see
// `CluadeCode请求RequestBody的Anthropic协议定义数据用例.json`) so proxies
// that expect that exact field layout still recognize LsmWebGame traffic.
//
// Layout:
//
//	{ "device_id":"<bot-account>",
//	  "account_uuid":"<bot-user-id>",
//	  "session_id":"<room-id>:<seat>" }
//
// The Anthropic API caps user_id at 256 chars; the layout above is well under
// that limit even for long room IDs.

// scheduleReWake pushes a delayed "state_change" event onto the agent's
// event channel so it gets another chance to act after a transient LLM failure.
// This breaks the deadlock where: LLM fails → agent yields → no action → no
// phase advance → no new wake → agent stuck forever.
//
// BUG-WEREWOLF-P0-NEW-4 (Round 24): the timer is now built off a per-call
// cancellable context (ctxRW). The agent stores ctxRW's cancel on
// reWakeCancel so that SetQuarantined can interrupt an in-flight reWake
// (otherwise a quarantine set after scheduleReWake had already passed the
// time.After select would still push the wake event). The select branches
// over ctx.Done() (room torn down), ctxRW.Done() (reWake cancelled by
// SetQuarantined), and the timer itself.

// containsSeat is a tiny helper for the death-state guard in handleEvent.
// Returns true when `seat` appears in the alive list (positive seat number
// matches engine convention). An empty/nil list is treated as "nobody alive"
// → false, so a torn-down room short-circuits safely.
//
// BUG-WEREWOLF-P1-NEW-45 (Round 39).

// phaseAllowsPublicSpeech 标识哪些 phase 允许 LLM 用 text-block 自动发言。
//
// 2026-07-13 §130 重构:白名单 = 玩家可能"开口说话"的阶段。发言类阶段
// (speak / pre_wolves) / 投票 / 警长竞选 / 猎人开枪 / 白痴翻牌 — 这些阶段
// bot 可能输出大段自然语言,text-block 自动发言会合理触发。
//
// 黑名单 = 夜间行动 / 法官驱动阶段 / 游戏结束阶段。这些阶段 bot 输出 text
// 通常是思考残片或调试输出,绝不应该被广播到公屏。
//
// 设计原则:宁可漏发(LLM 可在下一轮用 speak tool 兜底),不可误发(夜间行动
// 的"自言自语"会让其他玩家听到,等于泄露身份)。

// 2026-07-10 §重构 — 错误分类 helper。
// 用于把 anthropic.Error / 网络错误分类成 5xx / 429 / timeout / permanent 4 类,
// run.go 在 SetLastError 后调 SetLastErrorClass 写入,前端 BotPhaseIndicator
// 据此渲染不同徽章。

// isAnthropic429 判断错误是否为上游 429 (Rate Limited)。

// isAnthropicTimeout 判断错误是否为客户端超时。

// isNetworkOrTimeoutTransient 判断错误是否为 timeout 或网络层瞬断。
// R131: 此类错误只触发 auto-skip,不计入 consecutiveFailures,避免慢模型或
// 上游抖动被永久 quarantine。

// isModel400CircuitErr 判断错误是否为 anthropic Provider 的 model-scoped
// 400 熔断(model_400_circuit)导致的快速失败(2026-07-24)。
// 首选判定是 anthropic.Error.Source;err 可能被 fmt.Errorf("...: %w") 包装,
// 因此用 errors.As 穿透;字符串包含作为兜底(包装路径丢失类型断言时)。
