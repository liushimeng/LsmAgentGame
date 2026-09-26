// Package virtual_city — room_llm_stats.go: 房间级 LLM 调用计数与速率观测
// (2026-09-26 §批次25 §3.3,对齐 wwplayer/agent_llm_stats.go 形态)。
//
// 全库虚拟城市仅 3 个 LLM 调用点:座位决策(agent/vcplayer/run_llm.go
// callProvider)、居民驱动层(city/driver.go runOne)、城市之声
// (city/voice.go speakOne)。三处经 hook 回调本文件的 note* 方法;
// 每 60s 汇总打一行 `virtual_city llm rate` zap 日志(累计值 + 本窗口
// 每分钟速率 + 每座位最近窗口速率 + 按 model_key 分布),供 E2E 断言
// 「每居民 1–2 次/分、全房 ≤ 座位数×2 次/分」。
package virtual_city

import (
	"sync"
	"time"

	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// llmStatsLogInterval 汇总日志周期(批次 25:每 60s 一行)。
const llmStatsLogInterval = 60 * time.Second

// roomLLMStats 房间级 LLM 调用计数器。独立互斥(不嵌套 r.mu —— 调用点可能
// 在 r.mu 锁内/锁外,一律只持本锁)。
type roomLLMStats struct {
	mu          sync.Mutex
	seatCalls   [MaxSeats]int64 // 累计:每座位 LLM 调用数
	driverCalls int64           // 累计:驱动层条目调用数
	voiceCalls  int64           // 累计:城市之声调用数
	byModel     map[string]int64
	// win* 最近 60s 窗口计数(每轮日志后清零)。
	winSeat   [MaxSeats]int64
	winDriver int64
	winVoice  int64
	started   bool // 汇总 goroutine 已启动(防重)
}

// noteSeatLLMCall 座位 Agent 一次 LLM 调用(vcplayer hook 回调)。
func (r *VirtualCityRoom) noteSeatLLMCall(seat int, modelKey string) {
	r.llmStats.mu.Lock()
	defer r.llmStats.mu.Unlock()
	if seat >= 0 && seat < MaxSeats {
		r.llmStats.seatCalls[seat]++
		r.llmStats.winSeat[seat]++
	}
	r.llmStats.noteModelLocked(modelKey)
}

// noteDriverLLMCall 驱动层条目一次 LLM 调用(city.ResidentDriver hook)。
func (r *VirtualCityRoom) noteDriverLLMCall(modelKey string) {
	r.llmStats.mu.Lock()
	defer r.llmStats.mu.Unlock()
	r.llmStats.driverCalls++
	r.llmStats.winDriver++
	r.llmStats.noteModelLocked(modelKey)
}

// noteVoiceLLMCall 城市之声一次 LLM 调用(city.VoiceScheduler hook)。
func (r *VirtualCityRoom) noteVoiceLLMCall(modelKey string) {
	r.llmStats.mu.Lock()
	defer r.llmStats.mu.Unlock()
	r.llmStats.voiceCalls++
	r.llmStats.winVoice++
	r.llmStats.noteModelLocked(modelKey)
}

func (s *roomLLMStats) noteModelLocked(modelKey string) {
	if modelKey == "" {
		modelKey = "(pool)"
	}
	if s.byModel == nil {
		s.byModel = make(map[string]int64)
	}
	s.byModel[modelKey]++
}

// startLLMStatsLoop 启动 60s 汇总 goroutine(Start 时调用一次;房间 Close 后
// 经 r.done 退出)。零调用也照打(速率 0 —— 便于发现「Agent 静默」)。
func (r *VirtualCityRoom) startLLMStatsLoop() {
	r.llmStats.mu.Lock()
	if r.llmStats.started {
		r.llmStats.mu.Unlock()
		return
	}
	r.llmStats.started = true
	r.llmStats.mu.Unlock()
	go func() {
		t := time.NewTicker(llmStatsLogInterval)
		defer t.Stop()
		for {
			select {
			case <-r.done:
				return
			case <-t.C:
				r.logLLMStats()
			}
		}
	}()
}

// logLLMStats 打一行汇总日志并清零窗口计数。
func (r *VirtualCityRoom) logLLMStats() {
	r.llmStats.mu.Lock()
	winSeat := r.llmStats.winSeat
	winDriver := r.llmStats.winDriver
	winVoice := r.llmStats.winVoice
	seatCalls := r.llmStats.seatCalls
	driverCalls := r.llmStats.driverCalls
	voiceCalls := r.llmStats.voiceCalls
	byModel := make(map[string]int64, len(r.llmStats.byModel))
	for k, v := range r.llmStats.byModel {
		byModel[k] = v
	}
	r.llmStats.winSeat = [MaxSeats]int64{}
	r.llmStats.winDriver = 0
	r.llmStats.winVoice = 0
	r.llmStats.mu.Unlock()

	// 窗口 = 60s,win 计数即「次/分」。
	winTotal := winDriver + winVoice
	seatTotal := int64(0)
	winSeats := make(map[int]int64)
	for seat := 0; seat < MaxSeats; seat++ {
		winTotal += winSeat[seat]
		seatTotal += seatCalls[seat]
		if winSeat[seat] > 0 {
			winSeats[seat] = winSeat[seat]
		}
	}
	logger.L().Info("virtual_city llm rate",
		zap.String("room_id", r.RoomID),
		zap.Int64("per_min_total", winTotal),
		zap.Int64("seat_calls_total", seatTotal),
		zap.Int64("driver_calls_total", driverCalls),
		zap.Int64("voice_calls_total", voiceCalls),
		zap.Int64("seat_per_min", winTotal-winDriver-winVoice),
		zap.Int64("driver_per_min", winDriver),
		zap.Int64("voice_per_min", winVoice),
		zap.Any("seat_per_min_by_seat", winSeats),
		zap.Any("by_model", byModel))
}
