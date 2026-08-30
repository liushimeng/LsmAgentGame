package werewolf

import (
	"time"
	"LsmAgentGame/agent/wwjudge"
	"LsmAgentGame/config"
)

// ─── §4 行数治理搬移(2026-08-30 §20260830-01 同批):以下 doc 注释
// 原漂浮在 room.go,现搬至各自实现所在文件,零逻辑改动。 ───

// cfgWerewolfHumanWaitSec 读取人类等待窗口秒数。
// 0 = 禁用等待窗口(默认全 AI 房间);60(默认) = 混合房间等待 60s。

// quarantineSkipDepthLimit is the maximum number of recursive
// tryDispatchQuarantinedActingSkip calls allowed in a single lock-held chain
// before we bail out. Empirically a healthy chain dispatches at most a
// handful of skips before the phase transitions; 50 is well above normal
// traffic but still bounded — every recursive call was identical (same seat,
// same skip) meaning a self-loop, which is exactly what we want to break.
// BUG-WEREWOLF-P0-NEW-43.

// cfgWerewolfRoomLLMConcurrency 读取房间级 LLM 并发上限(BUG-R242-P1-01)。
// 0 / 负值 = 禁用(完全并发,§130 行为,仅用于调试)。见 room_config.go。

// cfgWerewolfJudgeMode 2026-07-10 §125 增强 — 读取法官模式。

// cfgWerewolfJudgeModelKey 2026-07-10 §125 增强 — 法官使用的 LLM model_key。

// judgeKindForPhase 把对局 phase 映射为法官唤醒事件 kind(对齐 docs/狼人杀-重构方案/主持人Agent重构设计.md
// §6.3 映射表)。秘密阶段(NightWolves/Seer/Witch)返回空字符串 → phaseWatchdogTick 不调
// wake,法官在夜间静默观察。

// cfgWerewolfEnableModelMemoryRecap 2026-07-10 §125 增强 — 是否注入上一局记忆。

func cfgWerewolfHumanWaitSec() int {
	defer func() { _ = recover() }()
	return config.Load().Werewolf.HumanWaitSec
}

const quarantineSkipDepthLimit = 50

func cfgWerewolfJudgeMode() string {
	defer func() { _ = recover() }()
	mode := config.Load().Werewolf.JudgeMode
	if mode == "" {
		return "agent"
	}
	// 2026-07-30 §重构:旧 cfg 中残留的 "ai" 归一化为 "agent"(新契约主名),
	// "off" 保持原义(运维 kill switch);其他未知值回退 "agent" 保持启用。
	switch mode {
	case "agent", "human", "off":
		return mode
	case "ai":
		return "agent"
	default:
		return "agent"
	}
}

func cfgWerewolfJudgeModelKey() string {
	defer func() { _ = recover() }()
	return config.Load().Werewolf.JudgeModelKey
}

// cfgWerewolfModelSelfPortraitEnabled 安全读取 §20260810-10 U2 自画像开关。
// 默认 true;测试环境 config.Load() panic 时按"关闭"兜底(避免无配置环境
// 误触发 DB 查询)。config.WerewolfConfig 无对应字段时(旧 conf)同样走默认 true。
func cfgWerewolfModelSelfPortraitEnabled() (enabled bool) {
	defer func() {
		if recover() != nil {
			enabled = false
		}
	}()
	v := config.Load().Werewolf.ModelSelfPortraitEnabled
	if !v {
		return false
	}
	return true
}

func judgeKindForPhase(p Phase) string {
	switch p {
	case PhaseFilling:
		return wwjudge.JudgePendingFillingWelcome
	case PhasePreWolves:
		return wwjudge.JudgePendingPreWolves
	case PhaseDawn:
		return wwjudge.JudgePendingDawnAnnounce
	case PhaseSheriff:
		return wwjudge.JudgePendingSheriffStart
	case PhaseSpeak:
		return wwjudge.JudgePendingSpeakStart
	case PhaseVote:
		return wwjudge.JudgePendingVoteStart
	case PhaseIdiotReveal:
		return wwjudge.JudgePendingIdiotReveal
	case PhaseHunterShoot:
		return wwjudge.JudgePendingHunterShoot
	case PhaseDeathLyric:
		return wwjudge.JudgePendingLastWords
	case PhaseRestartVote:
		return wwjudge.JudgePendingRestartVoteResult
	case PhaseGameOver:
		return wwjudge.JudgePendingGameOver
	default:
		// PhaseNightWolves / PhaseNightSeer / PhaseNightWitch 等秘密阶段 → 静默。
		return ""
	}
}

func cfgWerewolfEnableModelMemoryRecap() bool {
	defer func() { _ = recover() }()
	return config.Load().Werewolf.EnableModelMemoryRecap
}

// cfgWerewolfRoomLLMConcurrency 读取房间级 LLM HTTP 调用并发上限(BUG-R242-P1-01)。
// 默认 4;0 / 负值 = 禁用(§130 完全并发,仅用于调试)。槽位满时 bot 短暂等待后
// reWake(瞬态,不计入 consecutiveFailures),避免慢模型阻塞快模型的同时把在途
// 调用数限制在代理可承受范围内。
func cfgWerewolfRoomLLMConcurrency() int {
	defer func() { _ = recover() }()
	return config.Load().Werewolf.RoomLLMConcurrency
}

// ensureLLMSemaphoreLocked 懒创建房间级 LLM 并发信号量。
//
// 2026-08-14 §20260814-01 U3 —— 从 StartAgentsLocked(room_agent.go:279)
// 抽出为单一事实来源。
//
// # 为什么必须抽出来
//
// U3 要把法官(startJudgeGoroutine)也纳入同一个信号量,而:
//
//   - startJudgeGoroutine 有 **5 个调用点**(room_action.go:1106 /
//     room_manage.go:238,505,574 / room_agent.go:1837);
//   - 它与 StartAgentsLocked 的先后顺序**不保证** —— 其中
//     room_manage.go 的三处在建房/恢复路径上可能先于 bot 启动。
//
// 若两处各写一份 `if r.llmSema == nil { make(...) }`,任一处漏改或参数写错
// 就会出现「法官拿到 nil、bot 拿到有界 chan」的半失效状态 —— 正是 §92a /
// §132 反复强调的「同一语义在多路径必须完全一致」。
//
// # 幂等 + cap 语义
//
//   - 已创建 → 直接返回(不重建,房间复用期间 cap 不变)。
//   - cap <= 0 → **不创建**,r.llmSema 保持 nil ⇒ 所有调用方的
//     AcquireLLMSlot 恒返回 true = 完全并发(§130 调试行为,零回归)。
//
// 必须在持 r.mu 时调用(§92a):两个调用方 StartAgentsLocked /
// startJudgeGoroutine 都已持锁。
func (r *WerewolfRoom) ensureLLMSemaphoreLocked() {
	if r == nil || r.llmSema != nil {
		return
	}
	if n := cfgWerewolfRoomLLMConcurrency(); n > 0 {
		r.llmSema = make(chan struct{}, n)
	}
}

// cfgWerewolfDeathRevealDelayMinDefault 读取 §20260810-12 D2 死者身份
// 「终局延时揭晓」默认值(分钟)。可选 0/5/15;0 = 立即揭晓(零回归)。
// RoomService.CreateRoomWithAgents 在请求未指定时按此兜底。
func cfgWerewolfDeathRevealDelayMinDefault() int {
	defer func() { _ = recover() }()
	return config.Load().Werewolf.DeathRevealDelayMinDefault
}

// cfgWerewolfRevealRoleOnDeathDefault 读取 §20260830-01 「死亡亮身份」建房默认值。
// conf 未显式写 false 时一律 true(默认开启,旧客户端/旧 conf 零回归面为「新默认
// 行为」);测试环境 config.Load() panic 时兜底 true(照抄
// cfgWerewolfDeathRevealDelayMinDefault 的 recover 模式,布尔语义取「开启」)。
// 由 WerewolfRoom.revealRoleOnDeathEffectiveLocked(未显式配置的房间)与
// service 层建房解析共同消费 —— 两侧读同一 cfg,单一事实来源。
func cfgWerewolfRevealRoleOnDeathDefault() (enabled bool) {
	enabled = true
	defer func() {
		if recover() != nil {
			enabled = true
		}
	}()
	if v := config.Load().Werewolf.RevealRoleOnDeathDefault; v != nil {
		enabled = *v
	}
	return enabled
}

// cfgWerewolfDeathRevealDelayMinAllowed 返回合法值集合,前端下拉与后端校验共用。
// 默认 {0, 5, 15};若 cfg 中配置了非此三值,自动归一化为 0(避免前端下拉越界)。
// 测试环境 config.Load() panic 时返回默认三档(避免无配置环境空切片)。
func cfgWerewolfDeathRevealDelayMinAllowed() (out []int) {
	defer func() {
		if recover() != nil {
			out = []int{0, 5, 15}
		}
	}()
	out = []int{0, 5, 15}
	v := config.Load().Werewolf.DeathRevealDelayMinDefault
	switch v {
	case 0, 5, 15:
		return out
	default:
		return out
	}
}

const spectatorWakeInterval = 15 * time.Second

var spectatorWakeAllowedPhases = map[string]bool{
	"pre_wolves":     true,
	"PhasePreWolves": true,
	"sheriff":        true,
	"PhaseSheriff":   true,
	"speak":          true,
	"PhaseSpeak":     true,
	"vote":           true,
	"PhaseVote":      true,
	// BUG 2026-07-09: 遗言阶段加入白名单,让观战者公开发言能 wake 存活 bot
	// (遗言 actor 已有 MyTurn,不会重复 wake)。
	"death_lyric":     true,
	"PhaseDeathLyric": true,
	// 以下阶段故意不在白名单:
	//   night_wolves / night_seer / night_witch / PhaseNight*
	//   dawn / PhaseDawn
	//   hunter_shoot / PhaseHunterShoot
	//   gameover / PhaseGameOver
	//   filling / PhaseFilling
}

const phaseWatchdogTickInterval = 5 * time.Second

func phaseWatchdogDeadlineFor(seatCount int) time.Duration {
	base := 240 * time.Second
	if seatCount > 7 {
		extra := (seatCount - 7) * 30
		if extra > 120 {
			extra = 120
		}
		return base + time.Duration(extra)*time.Second
	}
	return base
}

const phaseWatchdogWarningInterval = 120 * time.Second

// phaseWatchdogHunterDeadline BUG-R10-P0-3 (2026-07-29):PhaseHunterShoot
// 的提前兜底 deadline。PhaseHunterShoot 是单座位 + 单工具阶段,正常 LLM
// 响应 30-60s 内即可完成 hunter_shoot 调用;若 120s 仍无动作,系统代打
// 派发 hunter_shoot(-1) 推进阶段,避免再次出现"6 分钟 stuck"事故
// (R10 seat 6 Qwen 3.7-Max)。低于通用 phaseWatchdogDeadlineFor(420s),
// 提前 5 分钟兜底。
const phaseWatchdogHunterDeadline = 120 * time.Second

// phaseWatchdogSingleActorDeadline BUG-R11-P0 (2026-07-30):推广 d7d3558
// 的 120s 早期兜底到所有"单座位 + 单工具"夜间阶段 (night_guard /
// night_seer / night_witch)。这些阶段与 hunter_shoot 同构:只有 1 个特定
// 角色行动,1 次 tool_use 即可完成。若该角色 LLM 永久故障 (400 circuit
// open / 配额耗尽),等通用 360s 兜底会让整个对局停滞 6 分钟(R11 报告
// seat 2 GLM-model 在 night_wolves 循环 stuck 的同源问题)。
const phaseWatchdogSingleActorDeadline = 120 * time.Second

// allQuarantinedTripTicks BUG-R221 (2026-08-01):房间级熔断阈值 —— 连续多少个
// phaseWatchdogTick 观察到"所有存活 bot 全部 quarantine 且无存活真人可行动"
// 后强制结束对局。
//
// 一个所有 Agent 都被 quarantine 的房间没有任何推进动力:每个 bot 的 wake 都被
// IsQuarantined guard 短路,watchdog 只能靠 dispatchQuarantinedSkipLocked 一格一格
// 硬推,遇到 vote / night_wolves 这类"多座位投票"阶段甚至完全推不动。R221 观测到
// 房间 ce288893 保持"游戏中" 15+ 小时,持续占用大厅席位。
//
// 120 tick × phaseWatchdogTickInterval(5s) = 10 分钟。取 10 分钟而非更短,是为了
// 给上游 LLM 代理瞬断留出恢复窗口:任一 bot 恢复(ResetConsecutiveFailures 清掉
// quarantine)都会让计数器清零,熔断不触发。
const allQuarantinedTripTicks = 120

// cfgWerewolfSheriffPersistRounds 是 §20260811-10 U5 警徽流保鲜期阈值。
//
// 默认 2;测试环境 config.Load() panic 时兜底 2,确保 view.go 渲染 stale。
// 0 或负值视为"永不 stale"(向旧行为兼容,§20260811-10 U5 S-02)。
//
// 真实配置项:werewolf.sheriff_persist_rounds(若 config.WerewolfConfig
// 后续扩展该字段,只需在此函数内补一行读取)。
func cfgWerewolfSheriffPersistRounds() int {
	defer func() { _ = recover() }()
	// 默认 2;无配置项时直接返回常量(测试环境亦然)。
	return 2
}

