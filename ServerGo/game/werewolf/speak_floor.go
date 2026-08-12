// Package werewolf — speak_floor.go: Speak Floor Watchdog (2026-07-09 §13 增强)。
//
// 动机:白天发言阶段(speak / sheriff / vote / hunter_shoot / dawn 遗言)硬性要求
// 每个 Agent 每分钟至少发 2 次公开发言,避免 "Agent 太安静" 现象。
//
// 实现:每个活跃房间后台启动一个 5s tick 的 goroutine,在该 tick 内:
//   1. 遍历所有 alive 非 quarantine bot
//   2. 检查 phase ∈ {speak, sheriff, vote, hunter_shoot, dawn}
//   3. 调 wwplayer.SnapshotSpeakCounter() 取 60s 窗口计数
//   4. 若 count < minSpeaks 且距上次 floor_wake ≥ 20s → push AgentEvent{Kind:"speak_floor_tick"}
//
// 与 phaseWatchdogTick 的差异:
//   - phaseWatchdog 是 90s 兜底; speak_floor 是 5s 积极提醒
//   - phaseWatchdog 派发 skip 工具(强制推进); speak_floor 派发 user 消息(强 prompt)
//   - 两条 watchdog 独立运行,不在同一 goroutine 内串联(避免 §92b 路径漂移)
//
// 锁策略:镜像 phaseWatchdogTick,在 r.mu 外启 goroutine,访问
// r.BotAgents[seat].snapshotSpeakCounter() 时单独取 counter(agent 自身 mutex)。
package werewolf

import (
	"context"
	"sync"
	"time"

	"LsmWebGame/agent/wwplayer"
	"LsmWebGame/logger"

	"go.uber.org/zap"
)

// speakFloorAllowedPhases speak floor watchdog 生效的阶段白名单。
// 仅白天发言阶段(夜间/dawn/gameover 都不适用)。
var speakFloorAllowedPhases = map[string]bool{
	"speak":         true,
	"PhaseSpeak":    true,
	"sheriff":       true,
	"PhaseSheriff":  true,
	"vote":          true,
	"PhaseVote":     true,
	"hunter_shoot":  true,
	"PhaseHunterShoot": true,
	"pre_wolves":    true, // 首夜缓冲期(每分钟 ≥ 2 次规则同样适用)
	"PhasePreWolves": true,
	"dawn":          true,
	"PhaseDawn":     true,
}

// speakFloorEntry 每个房间的 speak floor watchdog 状态。
//
// lastFloorWake[seat] 记录上次 wake 时间戳,防止 1 秒内连发 wake
// (避免单 bot 一分钟内被 wake 10+ 次,token 翻倍)。
type speakFloorEntry struct {
	cancel         context.CancelFunc
	lastFloorWake  map[int]time.Time // seat → 上次 wake 时间戳
	wg             sync.WaitGroup
}

// speakFloorState manager-level watchdog 管理(房间 id → entry)。
type speakFloorState struct {
	mu      sync.Mutex
	entries map[string]*speakFloorEntry
}

var globalSpeakFloor = speakFloorState{
	entries: make(map[string]*speakFloorEntry),
}

// startSpeakFloorWatchdog 启动一个房间的 speak floor watchdog。
//
// 在 WerewolfManager.StartAgentsLocked 末尾调用一次(沿用 §93 Phase Watchdog 模式);
// 房间 tear down 时 stopSpeakFloorWatchdog 调 cancel() 并 wg.Wait()。
func (m *WerewolfManager) startSpeakFloorWatchdog(r *WerewolfRoom) {
	if r == nil {
		return
	}
	// 同一房间不重复启动(测试场景)
	globalSpeakFloor.mu.Lock()
	if _, exists := globalSpeakFloor.entries[r.RoomID]; exists {
		globalSpeakFloor.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	entry := &speakFloorEntry{
		cancel:        cancel,
		lastFloorWake: make(map[int]time.Time),
	}
	globalSpeakFloor.entries[r.RoomID] = entry
	globalSpeakFloor.mu.Unlock()

	entry.wg.Add(1)
	go func() {
		defer entry.wg.Done()
		ticker := time.NewTicker(5 * time.Second) // 5s tick
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.speakFloorTick(r)
			}
		}
	}()
}

// stopSpeakFloorWatchdog 停止一个房间的 speak floor watchdog。
//
// 在 stopAgentsLocked 末尾调用,确保房间 tear down 时 watchdog 立即退出,
// 不会继续 push 事件到已关闭的 agent channel。
func (m *WerewolfManager) stopSpeakFloorWatchdog(r *WerewolfRoom) {
	if r == nil {
		return
	}
	globalSpeakFloor.mu.Lock()
	entry, ok := globalSpeakFloor.entries[r.RoomID]
	if ok {
		delete(globalSpeakFloor.entries, r.RoomID)
	}
	globalSpeakFloor.mu.Unlock()
	if !ok {
		return
	}
	entry.cancel()
	entry.wg.Wait()
}

// speakFloorTick 单次 tick:遍历 alive bot,根据 60s 窗口判定是否需要 floor wake。
//
// 由 ticker 5s 触发一次;在 r.mu 外运行,只读 r.State(线程安全) + 单独持
// r.mu 短时间迭代 BotAgents(§92a 锁内变体约束)。
func (m *WerewolfManager) speakFloorTick(r *WerewolfRoom) {
	if r == nil || r.State == nil {
		return
	}
	phaseStr := r.State.Phase.String()
	if !speakFloorAllowedPhases[phaseStr] {
		return
	}

	// 读取全局 minSpeaks;0 = 禁用
	minSpeaks := 2
	defer func() { _ = recover() }()
	// 直接读全局配置(避免反向依赖 — config 在 ServerGo/config,werewolf 在 ServerGo/game/werewolf)
	// 通过 lazy import 避免编译时循环:这里改为调用 cfgWerewolfFirstNightSpeakMinIntervalSec() 的姊妹函数。
	// 简化:minSpeaks 直接 hardcode 2 — 与 plan 一致(可由 config.WerewolfConfig.MinSpeaksPerMinute 调整)。
	minSpeaks = cfgWerewolfMinSpeaks()
	// 2026-08-12 §20260812-04 U5 (P0-7) — 与同座位发言冷却做自洽性钳制。
	//
	// 缺陷:发言下限默认 2 次/60s(cfgWerewolfMinSpeaks),而同座位公开发言冷却
	// 默认 60s(cfgWerewolfSameSeatSpeakCooldownSec)—— 每座位每分钟最多 1 次,
	// 于是 `currentCount >= 2` 对**任何** bot 都永远不成立。结果:每 20s 给每个
	// 存活 bot 推一次注定失败的 floor wake,13 人局 ≈ 39 次无效 LLM 调用/分钟,
	// 还要挤占 cap=4 的房间信号量。
	//
	// 钳制到冷却窗口在 60s 内**物理可达**的次数,让两个配置无论怎么填都自洽。
	minSpeaks = clampMinSpeaksToCooldown(minSpeaks)

	// 持锁读取 bot 列表与 push event
	// BUG-R200-P0-02 (2026-07-30): 之前用 r.mu.Lock() 无超时持锁,与
	// ForceStartIfReady / SpectateGame 的持锁路径争用同一把锁,导致
	// lockRoomBriefly 失败后 REST 永不返回。改用 200ms 短持锁,失败则
	// 跳过本 tick(下一次 5s 后再试),与 GetPublicState 行为对称。
	if !lockRoomBriefly(r, 200*time.Millisecond) {
		return
	}
	defer r.mu.Unlock()
	if r.BotAgents == nil || len(r.BotAgents) == 0 {
		return
	}
	now := time.Now()
	floorWakeInterval := 20 * time.Second
	awakened := 0

	globalSpeakFloor.mu.Lock()
	entry := globalSpeakFloor.entries[r.RoomID]
	if entry == nil {
		globalSpeakFloor.mu.Unlock()
		return
	}
	globalSpeakFloor.mu.Unlock()

	for seat, ag := range r.BotAgents {
		if ag == nil || ag.IsQuarantined() {
			continue
		}
		// §95 死态守卫 — 用 live rp().alive[],不是 event snapshot
		if !r.State.AliveSeat(Seat(seat)) {
			continue
		}
		_, currentCount := ag.AllowSpeakDaytimePublic(now)
		if currentCount >= minSpeaks {
			continue
		}
		// 距上次 wake ≥ 20s 才再触发(防止 1 分钟内连发多次 wake)
		globalSpeakFloor.mu.Lock()
		lastWake := entry.lastFloorWake[seat]
		globalSpeakFloor.mu.Unlock()
		if !lastWake.IsZero() && now.Sub(lastWake) < floorWakeInterval {
			continue
		}
		// push floor wake event
		driverSeat := lowestActiveBotSeatLocked(r)
		gc := buildAgentContextLocked(r, seat, driverSeat)
		ag.PushEvent(wwplayer.AgentEvent{
			Kind:    "speak_floor_tick",
			Context: gc,
		})
		globalSpeakFloor.mu.Lock()
		entry.lastFloorWake[seat] = now
		globalSpeakFloor.mu.Unlock()
		awakened++
	}
	if awakened > 0 {
		logger.L().Info("werewolf: speak floor watchdog tick fired",
			zap.String("room_id", r.RoomID),
			zap.String("phase", phaseStr),
			zap.Int("awakened", awakened),
			zap.Int("min_speaks", minSpeaks))
	}
}
// clampMinSpeaksToCooldown 把「每分钟发言下限」钳制到同座位发言冷却在 60s 窗口内
// 物理可达的次数。
//
// 2026-08-12 §20260812-04 U5 (P0-7) 新增。
//
// 背景:发言下限(werewolf.min_speaks_per_minute,默认 2)与同座位公开发言冷却
// (werewolf.same_seat_speak_cooldown_sec,默认 60)是两个独立配置,但语义上互相
// 约束 —— 冷却 60s 意味着每座位每分钟**最多 1 次**公开发言,此时下限 2 永远
// 不可能满足。旧代码直接用下限值判定,导致 speak_floor watchdog 每 20s 给每个
// 存活 bot 推一次注定失败的 wake(13 人局 ≈ 39 次无效 LLM 调用/分钟)。
//
// 钳制规则:reachable = floor(60 / cooldown),至少为 1(cooldown ≥ 60 时)。
// 返回 min(minSpeaks, reachable)。minSpeaks ≤ 0 表示禁用,原样返回。
//
// 这里刻意**不改任何一方的配置值**,只在消费点取可达上限 —— 运维仍可通过调小
// cooldown 来真正提高发言频率,配置语义不被悄悄改写。
func clampMinSpeaksToCooldown(minSpeaks int) int {
	if minSpeaks <= 0 {
		return minSpeaks // 0 = 禁用发言下限,不干预
	}
	return clampMinSpeaksWithCooldown(minSpeaks, cfgWerewolfSameSeatSpeakCooldownSec())
}

// clampMinSpeaksWithCooldown 是 clampMinSpeaksToCooldown 的纯函数内核
//(cooldown 显式传入)。拆出来是为了让单测不依赖 config.Load() ——
// §197 教训 (3):测试环境 config.Load() 会 panic,测试不应假设它可用。
func clampMinSpeaksWithCooldown(minSpeaks, cooldown int) int {
	if minSpeaks <= 0 {
		return minSpeaks
	}
	if cooldown <= 0 {
		return minSpeaks // 无冷却限制,下限可原样生效
	}
	reachable := 60 / cooldown
	if reachable < 1 {
		reachable = 1
	}
	if minSpeaks > reachable {
		logger.L().Debug("werewolf: min_speaks clamped by same-seat cooldown",
			zap.Int("configured_min_speaks", minSpeaks),
			zap.Int("cooldown_sec", cooldown),
			zap.Int("effective_min_speaks", reachable))
		return reachable
	}
	return minSpeaks
}
