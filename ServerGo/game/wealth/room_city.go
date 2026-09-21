// Package wealth — room_city.go: 城市背景层房间接线(2026-09-21 §虚拟城市
// 契约 03 §6 + 01 §3.2 B5)。
//
// WealthRoom 持有 ResidentCount(建房参数)与 City(Backdrop);Start 时按
// resident_count>0 建城(rng 由房间 seed 派生,与引擎 rng 流分离 → 同 seed
// 同城);月结顺序 SettleMonth() → City.TickMonth(cpi, rng) → 广播(cpi 取
// 引擎 P1 月环比通胀,缺省 0.002);月结后异步触发城市之声(goroutine,绝不
// 阻塞月结)。
//
// 持久化说明(现状遵循):wealth 房间级选项(month_ms/pool/seed/resident_count)
// 经 Manager.pendingOpts 仅存内存 —— 服务重启后不恢复,本文件不为
// resident_count 新建持久化(不新建表,遵循既有机制);重启 hydrate 路径
// 重建的房间无城市层,契约 03 §6 的「确定性重建」在持久化机制补齐后自动成立
// (Backdrop 本身由 ResidentCount+seed 确定性可重建)。
package wealth

import (
	"fmt"
	"math/rand"
	"runtime/debug"

	"LsmAgentGame/game/wealth/city"
	"LsmAgentGame/llm"
	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// citySeedSalt 城市 rng 派生盐:城市演化流 = seed ^ salt,与引擎 rng
// (NewWorld(seed))流完全分离,互不扰动确定性。
const citySeedSalt int64 = -7046029254386353131 // 0x9E3779B97F4A7C15 的 int64 表示

// defaultCityCPI 引擎查不到月环比通胀时的缺省(契约 03 §6:0.002)。
const defaultCityCPI = 0.002

// PoolModelDisplay 池驱动座位(model_key=="")的展示名(契约 04 §1.3)。
const PoolModelDisplay = "LLM线路池"

// SetLinePoolSource 注入 LLM 线路池来源(Manager.CreateRoom / main 装配;
// nil = 无池,座位必须显式 model_key)。
func (r *WealthRoom) SetLinePoolSource(fn func() *llm.LinePool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.linePoolSource = fn
}

// SetCityVoiceConfig 城市之声开关与每月条数(Manager.CreateRoom 注入;
// perMonth 使用点 clamp [0,32])。
func (r *WealthRoom) SetCityVoiceConfig(enabled bool, perMonth int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cityVoiceEnabled = enabled
	r.cityVoicePerMonth = clampInt(perMonth, 0, 32)
}

// SetResidentCount 设置城市背景居民数(建房链路;负数防御为 0)。
// 注意:仅 Start 前生效 —— 城市在 Start 时一次性合成。
func (r *WealthRoom) SetResidentCount(n int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n < 0 {
		n = 0
	}
	r.ResidentCount = n
}

// startCityLocked Start 时建城(锁内;ResidentCount>0 时)。
// 校准表取构建时刻的 CurrentCalibration()(未预热 → 合成默认;进行中房间
// 持引用不回填,契约 03 §3.2)。
func (r *WealthRoom) startCityLocked() {
	if r.ResidentCount <= 0 {
		return
	}
	r.cityRng = rand.New(rand.NewSource(r.seed ^ citySeedSalt))
	r.City = city.NewBackdrop(r.ResidentCount, r.cityRng, city.CurrentCalibration())
	r.cityVoice = city.NewVoiceScheduler(r.cityVoiceEnabled, r.cityVoicePerMonth, r.linePoolSource)
	poolLines := 0
	if r.linePoolSource != nil {
		if p := r.linePoolSource(); p != nil {
			poolLines = p.Total()
		}
	}
	logger.L().Info("wealth city backdrop created",
		zap.String("room_id", r.RoomID),
		zap.Int("residents", r.ResidentCount),
		zap.Int64("seed", r.seed),
		zap.Int("pool_lines", poolLines))
}

// resizeAgentSemLocked 线路池可用时把房间信号量容量抬到总线路数
// (契约 01 §3.2 B5:LinePool 可用且 Total()>0 → 容量 = Total();否则保持
// AgentConcurrency 默认)。仅在 Start 锁内调用 —— 此时 wakeBots 尚未运行,
// 无并发 runOneBot 读 r.agentSem,替换通道无数据竞争。
func (r *WealthRoom) resizeAgentSemLocked() {
	if r.linePoolSource == nil {
		return
	}
	pool := r.linePoolSource()
	if pool == nil || pool.Total() <= 0 {
		return
	}
	r.agentSem = make(chan struct{}, pool.Total())
}

// currentCPILocked 返回月环比通胀(城市 tick 用):P1 消费篮子 CPIMom
// (economy_enabled)优先,缺省 defaultCityCPI。
func (r *WealthRoom) currentCPILocked() float64 {
	if r.World != nil && r.World.EconomyEnabled && r.World.Goods != nil && r.World.Goods.CPIMom != 0 {
		return r.World.Goods.CPIMom
	}
	return defaultCityCPI
}

// tickCityLocked 月结后演化城市(锁内;trySettle 在 SettleMonth 之后调用)。
func (r *WealthRoom) tickCityLocked() {
	if r.City == nil || r.cityRng == nil {
		return
	}
	r.City.TickMonth(r.currentCPILocked(), r.cityRng)
}

// launchCityVoices 月结后触发城市之声(锁外;goroutine 异步,绝不阻塞月结)。
// voiceMonth 为结算后的当前月(供 VoiceRecord.Month)。
func (r *WealthRoom) launchCityVoices(voiceMonth int) {
	r.mu.Lock()
	b := r.City
	sched := r.cityVoice
	closed := r.closed
	playing := r.Status == StatusPlaying
	r.mu.Unlock()
	if closed || !playing || b == nil || sched == nil {
		return
	}
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				logger.L().Error("wealth city voice panic recovered",
					zap.String("room_id", r.RoomID),
					zap.Any("panic", rec),
					zap.String("stack", string(debug.Stack())))
			}
		}()
		sched.Run(b, voiceMonth, func(vr city.VoiceRecord) {
			b.AppendVoice(vr)
			r.emitCityVoiceEvent(vr)
		})
	}()
}

// emitCityVoiceEvent 把一条城市之声追加进房间事件流并广播 game.event
// (EventRecord type="city_voice",契约 04 §1.3)。追加在锁内、广播在锁外(§92a)。
func (r *WealthRoom) emitCityVoiceEvent(vr city.VoiceRecord) {
	ev := EventRecord{
		Month: vr.Month,
		Type:  "city_voice",
		Seat:  -1,
		Text:  fmt.Sprintf("%s:%s", vr.Name, vr.Text),
	}
	r.mu.Lock()
	inGame := !r.closed && r.Status == StatusPlaying && r.World != nil
	if inGame {
		r.World.Events = append(r.World.Events, ev)
	}
	hooks := r.hooks
	r.mu.Unlock()
	if inGame && hooks.OnEvent != nil {
		hooks.OnEvent(r.RoomID, ev)
	}
}

// CitySnapshotView 返回城市快照(锁内;未建城返回 nil)。ws 层 view 路径用。
func (r *WealthRoom) CitySnapshotView() *city.Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.City == nil {
		return nil
	}
	s := r.City.Snapshot()
	return &s
}

// ResidentCountView 返回建房 resident_count(锁内;大厅列表/详情 🏙 徽标用)。
func (r *WealthRoom) ResidentCountView() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ResidentCount
}
