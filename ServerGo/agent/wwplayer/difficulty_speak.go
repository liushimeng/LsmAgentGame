// Package wwplayer — difficulty_speak.go: 难度档位的「发言节奏」执行器
// (2026-08-14 §20260814-01 U2)。
//
// # 为什么需要这个文件
//
// `game/werewolf/difficulty.go` 的 DifficultyProfile.SpeakLimiterScale 自
// §20260811-09 U2 落地起就是**死字段** —— 4 处赋值（easy 1.5 / normal 1.0 /
// hard 1.0 / hell 0.8）、**0 处生产读取**。难度分级对外宣称能调节 Agent 的
// 「发言节奏」，实际这一维度完全无效。
//
// 这是同一个 struct 内的**第三个**同病字段：
//
//	MemoryInjectRunes  → §20260812-04 U4 已修
//	MaxToolUse         → §20260813-04 U3 已修（改名 difficultyRoundCap）
//	SpeakLimiterScale  → 本次
//
// 正是 §20260813-04 教训 (3)「修完一个缺陷必须 grep 同 struct 其他字段是否
// 同病」所指的情形 —— 前两次修复都没有回头扫一遍这个 struct。
//
// # 为什么需要 SpeakLimiter.SetInterval
//
// 三个 limiter 的 interval 在 NewWithRoom 构造期即固定（speak 30s /
// whisper 60s / interject 60s），而难度档位要到 StartAgentsLocked 才注入。
// 因此无法在构造期把 scale 乘进去，只能事后重算 —— 这就是
// agent/core/ratelimit.go 新增 SetInterval 的唯一动机。
//
// # 关键约束：normal 档必须逐字节零回归
//
// §20260811-09 U2 的设计纪律是「normal 与旧版逐字节一致」（prompt cache 命中）。
// 本文件对 scale==1.0 的处理是**完全不调用 SetInterval**，三个 limiter
// 保持构造期的原值 —— 而不是「乘 1.0 后写回同一个值」。二者行为等价，
// 但前者在语义上明确表达「normal 档不参与本机制」，也避免将来
// SetInterval 增加副作用时 normal 档被牵连。
package wwplayer

import (
	"time"

	agentcore "LsmAgentGame/agent/core"
)

// 难度发言缩放的合法区间。
//
// 上下限的用途不是「防止配置写错」（difficulty.go 的 4 个值都是硬编码字面量），
// 而是**防止将来把 scale 提升为可配置项时**（如 admin 面板 / 房间创建参数）
// 出现极端值：
//
//	scale=0.01 → speak interval 0.3s，13 bot 会把公屏刷爆并打爆 LLM 配额；
//	scale=100  → speak interval 50min，bot 整局一言不发，等价于 quarantine。
//
// clamp 而非报错：与 NormalizeAgentDifficulty（difficulty.go:102）/
// cfgWerewolfJudgeMode（§198）的「未知值回退默认，不报错」宽松兼容策略一致。
const (
	difficultySpeakScaleMin = 0.5
	difficultySpeakScaleMax = 3.0
)

// SetDifficultySpeakScale 注入难度档位的发言节奏缩放系数，并**立即**重算
// 三个 limiter 的最小间隔。
//
// 由 StartAgentsLocked 紧邻 SetDifficultyRoundCap / SetMemoryInjectRunes
// 调用（三者同源于 difficulty.ProfileFor，是同一批 §130 死字段的修复）。
//
// 参数语义：
//
//	f <= 0   → 视为 1.0（未注入 / 配置缺失 → 不缩放）
//	f == 1.0 → **不触碰 limiter**（normal 档零回归，见文件头）
//	f  < 1.0 → 间隔缩短，发言更密集（hell 0.8 → speak 30s→24s）
//	f  > 1.0 → 间隔放长，发言更稀疏（easy 1.5 → speak 30s→45s）
//
// 三个 limiter 同步缩放（而非只缩放 speak）：whisper / interject 与 speak
// 的相对比例是 §R76 P1-3 反刷屏设计的一部分（interject 60s > speak 30s，
// 让插话严于发言）。只缩放其中之一会破坏这个不变量 —— easy 档若只放长
// speak 到 45s 而 interject 仍 60s，两者就从 2:1 变成 4:3，bot 会显得
// 「正经发言少、插话多」，与 easy 档「保守、发言简短」的 directive 相悖。
func (a *Agent) SetDifficultySpeakScale(f float64) {
	if f <= 0 {
		f = 1.0
	}
	if f < difficultySpeakScaleMin {
		f = difficultySpeakScaleMin
	}
	if f > difficultySpeakScaleMax {
		f = difficultySpeakScaleMax
	}

	a.Lock()
	a.difficultySpeakScale = f
	a.Unlock()

	// normal 档：不触碰任何 limiter（零回归）。
	if f == 1.0 {
		return
	}

	// limiter 自带 mutex，故在 a 的锁外调用（避免锁嵌套）。
	//
	// ⚠️ 形参必须是**具体类型** *agentcore.SpeakLimiter，不能用
	// interface{ Interval(); SetInterval() }。用 interface 时，传入一个
	// nil 的 *SpeakLimiter 会得到「类型非 nil、值为 nil」的接口，
	// `l == nil` 恒为 false，随后 l.SetInterval 解引用空指针 panic
	// —— 这是 Go 的 typed-nil 陷阱，本项目的 D08 测试即为此而写。
	scaleLimiter := func(l *agentcore.SpeakLimiter, base time.Duration) {
		if l == nil {
			return
		}
		l.SetInterval(time.Duration(float64(base) * f))
	}

	scaleLimiter(a.Limiter, difficultySpeakBaseInterval)
	scaleLimiter(a.WhisperLimiter, difficultyWhisperBaseInterval)
	scaleLimiter(a.InterjectLimiter, difficultyInterjectBaseInterval)
}

// 三个 limiter 的**构造期基准间隔**，与 NewWithRoom（agent.go:869-874）
// 的 NewSpeakLimiter 实参一一对应。
//
// 为什么复制常量而不读 l.Interval()：SetDifficultySpeakScale 必须**幂等** ——
// 若以「当前值 × f」重算，重复注入同一档位会累积缩放（easy 调两次 → 2.25×）。
// 以固定基准 × f 计算则任意次调用结果恒定。
//
// ⚠️ 修改 agent.go:869-874 的 NewSpeakLimiter 实参时必须同步这三个常量，
// 否则难度缩放会以错误的基准计算。difficulty_speak_test.go 有一条断言
// 直接比对「新建 Agent 的 limiter 实际 interval == 这三个常量」，
// 漂移会立即失败。
const (
	difficultySpeakBaseInterval     = 30 * time.Second
	difficultyWhisperBaseInterval   = 60 * time.Second
	difficultyInterjectBaseInterval = 60 * time.Second
)

// DifficultySpeakScale 返回当前发言节奏缩放系数（0 = 未注入，等价 1.0）。
func (a *Agent) DifficultySpeakScale() float64 {
	a.Lock()
	defer a.Unlock()
	return a.difficultySpeakScale
}
