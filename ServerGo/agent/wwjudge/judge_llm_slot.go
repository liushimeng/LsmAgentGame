// Package wwjudge — judge_llm_slot.go: 法官 LLM 调用的房间级并发信号量
// (2026-08-14 §20260814-01 U3，对应待实施项 §10.4 T10「Agent 并发调优」)。
//
// # 缺陷
//
// 房间级信号量 `WerewolfRoom.llmSema`（cap 默认 4，config.go:872）此前**只**
// 注入 player bot（room_agent.go:282 的 `ag.SetLLMSemaphore(r.llmSema)`）。
// 法官与解说各自直连 `Provider.Chat`：
//
//	agent/wwjudge/judge.go:411        resp, err := j.Provider.Chat(cctx, ...)
//	agent/wwcommentator/commentator.go:262  resp, err := provider.Chat(ctx, ...)
//
// 二者只受**时间间隔**限流（announceLimiter 15s / summaryLimiter 60s /
// 解说 limiter），完全不受**并发**约束。所以一个 13 人局在 cap=4 的配置下，
// 实际在飞的 LLM HTTP 调用可达 **6**（4 bot + 法官 + 解说）——
// 超出配置值 50%。
//
// config.go:91-97 记录了 §130 移除信号量导致的级联故障：
// 「6min 2.5% → 27min 66% 失败率」。信号量被恢复为有界后，这条敞口一直存在。
//
// # 为什么 wait 预算与 player bot 不同
//
// player bot 用 `llmSlotAcquireWait = 5s`（run.go:346），抢不到就
// `scheduleReWake` 稍后重试 —— 因为 bot 的 LLM 调用**推进游戏**，不能丢。
//
// 法官/解说是**旁路装饰性**调用：法官旁白失败有 `JudgeFallbackText` 硬编码
// 兜底（judge.go:418 返回 false 即走该路径），解说失败则本轮静默。
// 二者都**不推进 phase**（phase 由 watchdog 驱动，与法官解耦 ——
// judge.go:406-407 的注释明确写了这一点）。
//
// 因此这里用更短的 `JudgeSlotAcquireWait = 2s`，且**抢不到即放弃本次播报**：
//
//   - 不重试（不占用第二次机会）
//   - 不计入任何失败计数器（法官没有 quarantine 机制，但保持语义一致）
//   - 不阻塞调用方 goroutine 超过 2s
//
// 这与 run.go:1952-1958 的 `speak_floor_tick`「槽位满则静默跳过本 tick」
// 是同一条纪律：**装饰性 LLM 调用必须给推进游戏的调用让路**。
//
// # 为什么不复用 wwplayer 的实现
//
// wwplayer 与 wwjudge 是平级包，`wwplayer.Agent` 的三个方法挂在 Agent 上
// 无法跨类型复用。抽公共包只为三个 ~8 行方法反而增加一层间接。
// 这里逐字对照 wwplayer/agent.go:457-496 复制，语义完全一致 ——
// 包括 ReleaseLLMSlot 的「无匹配 acquire 也安全」的 select/default 形态。
package wwjudge

import "time"

// JudgeSlotAcquireWait 是法官/解说等待 LLM 槽位的上限。
//
// 取 2s（player bot 是 5s）：见文件头「为什么 wait 预算不同」。
// 导出以便解说包复用同一预算，避免两处漂移。
const JudgeSlotAcquireWait = 2 * time.Second

// SetLLMSemaphore 安装房间级 LLM 并发闸门。
//
// 传 nil = 不限流（向后兼容：既有测试桩 / 未注入信号量的房间构造路径
// 行为完全不变）。必须在 goroutine 启动前调用（由 startJudgeGoroutine 注入），
// goroutine 内只读 —— 与 SetProvider（judge.go:167）同款生命周期约束。
func (j *AgentJudge) SetLLMSemaphore(sema chan struct{}) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.llmSema = sema
}

// AcquireLLMSlot 尝试取得一个房间级 LLM 槽位，最多等待 wait。
//
// 返回 true = 已取得（调用方**必须** defer ReleaseLLMSlot）；
// false = 等待超时，调用方应当**放弃本次播报**而非重试（见文件头）。
//
// llmSema == nil → 恒 true（不限流）。
func (j *AgentJudge) AcquireLLMSlot(wait time.Duration) bool {
	j.mu.Lock()
	sema := j.llmSema
	j.mu.Unlock()

	if sema == nil {
		return true
	}
	if wait <= 0 {
		select {
		case sema <- struct{}{}:
			return true
		default:
			return false
		}
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case sema <- struct{}{}:
		return true
	case <-timer.C:
		return false
	}
}

// ReleaseLLMSlot 归还一个槽位。
//
// 与 wwplayer.ReleaseLLMSlot 同款：无匹配 acquire 时也安全（select/default
// 不阻塞）。但**调用方仍应只在 Acquire 返回 true 后 defer 本方法** ——
// 否则会吞掉别人的令牌（§20260812-04 U5 defer-in-loop 泄漏的镜像风险）。
func (j *AgentJudge) ReleaseLLMSlot() {
	j.mu.Lock()
	sema := j.llmSema
	j.mu.Unlock()

	if sema == nil {
		return
	}
	select {
	case <-sema:
	default:
	}
}
