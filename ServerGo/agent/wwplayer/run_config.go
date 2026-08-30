// Package wwplayer — run_config.go: Agent 决策循环的**配置与判定** helper 群。
//
// 2026-08-14 §20260814-01 U4 —— 从 run.go 纯搬移而来（不改逻辑 / 不改签名 /
// 不改导出命名），以满足 CLAUDE.md §4「单文件 ≤ 1800 行」硬上限：
// run.go 拆分前 2154 行，搬出本文件后降至约 1690 行。
//
// # 搬移边界为什么划在这里
//
// 原 run.go:30-499 是一整段**无状态判定与配置读取**：给定 phase / role /
// seat / 座位数，算出「内层循环几轮」「超时多少秒」「退避多久」「该派发哪个
// skip 动作」。它们与 run.go 剩余部分（Run / runLoop / handleEvent 的事件
// 驱动主循环）的耦合只有函数调用，没有共享局部状态，因此是天然的切分线。
//
// 反过来说，**不能**把 handleEvent 一起搬走：它是单个约 1240 行的函数
// （run.go:600-1841），搬移它等于把主循环挪到另一个文件而 run.go 只剩壳，
// 对可读性没有帮助。剩余 4 个超限文件（agent.go / room_agent.go /
// agent_runner.go / room.go）同理留待独立重构提交，本批次只保证不加剧。
//
// # 本文件包含
//
//	内层轮次上限     phaseMaxInnerRounds / maxInnerRoundsForPhase /
//	                maxInnerRoundsFor / isSingleActionTool
//	超时与预算       cfgStreamExtendedTimeoutSec / cfgLLMCallTimeoutSec* /
//	                llmCallBudgetSec / cfgLLMCallTimeoutSecScaled /
//	                llmBackoffForAttempt / thresholdForSeatCount
//	阶段动作判定     SkipPhaseAction / ShouldAutoSkip
//
// 零回归验证：`go build ./...` + 全量 `go test ./...` 通过即证明搬移无损
// （纯搬移的正确性由编译器 + 既有测试保证，与 §136 用「构建产物 CSS 字节
// 一致」证明 CSS 拆分零回归是同一思路）。
package wwplayer

import (
	"LsmAgentGame/config"
	"LsmAgentGame/agent/wwtypes"
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

// maxInnerRoundsFor 返回本 Agent 在该阶段的内层循环上限，
// 在 phase 基线之上叠加难度档位的收紧。
//
// 2026-08-13 §20260813-04 U3 —— 接线 difficulty.DifficultyRoundCap。
//
// # 修的是什么
//
// difficulty.go 为 easy/normal/hard/hell 设了 MaxToolUse: 3/6/8/0，
// 但 agent.go 把 Agent.MaxToolUse 硬设 0 且注释明写「§130 重构：保留但不再使用」，
// 于是**难度档位对工具调用上限完全无效**（4 处赋值 0 处生效）。
// 这与 §20260812-04 U4 修的 MemoryInjectRunes（同为 difficulty.go 4 赋值 +
// agent 侧 0 读取）是同一模式 —— 修了一个漏了另一个。
//
// # 为什么不复活旧的全局 MaxToolUse 语义
//
// §130 废弃它的理由是对的：全局硬上限会截断正常的多轮 tool_use
// （如 speak 前先 chat_recall）。真正生效的机制是 phaseMaxInnerRounds。
// 因此难度档位改为**调制**该基线，而非另立一套计数。
//
// # cap 只收紧不放宽
//
// 难度值（easy=3）大多比 phase 基线（夜间 3 / 投票 3 / 发言 5）更宽或相等，
// 直接取 min 意味着只有 easy 会真正收紧发言阶段（5 → 3）。这是刻意的：
// 弱模型/新手房间的 bot 更快收敛（少一轮 tool_use = 快一次 LLM 往返），
// 而 hard/hell 保持 phase 基线不被放宽 —— 放宽会破坏 §197 的慢模型预算假设。
func (a *Agent) maxInnerRoundsFor(phase string) int {
	base := maxInnerRoundsForPhase(phase)
	if cap := a.DifficultyRoundCap(); cap > 0 && cap < base {
		return cap
	}
	return base
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
	case "sheriff_order", "PhaseSheriffOrder":
		// §20260810-09 — 警长定序兜底:watchdog 代打,使用默认值
		//(顺时针 + 警长先发言)。与 room_agent.go:634 的 agent 路径
		//及 dispatchQuarantinedSkipLocked 同源。
		//§97 六处同步表:此处为 SkipPhaseAction 的遗漏项;缺失时警长
		//被 quarantine 阶段会卡 90s 且永不推进到 PhaseSpeak。
		return "sheriff_set_speak_order", 0
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
	case "suicide_take", "PhaseSuicideTake":
		// §20260830-02 — 自爆带走兜底**永远放弃**(target=-1)而非随机带走:
		// 随机带走期望收益为负(场上好人占多数时误带队友概率高),与猎魔人
		// 兜底空过(demon_hunter_hunt_skip)同一决策原则。放弃后直接入夜。
		return "wolf_suicide_take", -1
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
