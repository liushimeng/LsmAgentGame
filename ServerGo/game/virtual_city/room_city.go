// Package virtual_city — room_city.go: 城市背景层房间接线(2026-09-21 §虚拟城市
// 契约 03 §6 + 01 §3.2 B5)。
//
// VirtualCityRoom 持有 ResidentCount(建房参数)与 City(Backdrop);Start 时按
// resident_count>0 建城(rng 由房间 seed 派生,与引擎 rng 流分离 → 同 seed
// 同城);月结顺序 SettleMonth() → City.TickMonth(cpi, rng) → 广播(cpi 取
// 引擎 P1 月环比通胀,缺省 0.002);月结后异步触发城市之声(goroutine,绝不
// 阻塞月结)。
//
// 持久化说明(现状遵循):wealth 房间级选项(month_ms/seed/resident_count)
// 经 Manager.pendingOpts 仅存内存 —— 服务重启后不恢复,本文件不为
// resident_count 新建持久化(不新建表,遵循既有机制);重启 hydrate 路径
// 重建的房间无城市层,契约 03 §6 的「确定性重建」在持久化机制补齐后自动成立
// (Backdrop 本身由 ResidentCount+seed 确定性可重建)。2026-09-22 §17-CityHuman
// 契约 02 §5:Driver 无持久状态(cursor 重建从 0 起,漏几个月轮转可接受);
// Backdrop 确定性重建不变。
package virtual_city

import (
	"fmt"
	"math/rand"
	"runtime/debug"

	"LsmAgentGame/game/virtual_city/city"
	"LsmAgentGame/game/virtual_city/profession"
	"LsmAgentGame/llm"
	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// citySeedSalt 城市 rng 派生盐:城市演化流 = seed ^ salt,与引擎 rng
// (NewWorld(seed))流完全分离,互不扰动确定性。
const citySeedSalt int64 = -7046029254386353131 // 0x9E3779B97F4A7C15 的 int64 表示

// cityProfileSeedSalt 档案锚定 rng 派生盐(2026-09-21 §档案锚定契约 §5:
// rng2 独立流,与合成/演化/voice 流分离 → 同 seed 同路径集 → 同档案)。
// 取 ASCII "CITYPF" 的 int64 编码,与 citySeedSalt 无碰撞。
const cityProfileSeedSalt int64 = 0x4349545950524F46

// defaultCityCPI 引擎查不到月环比通胀时的缺省(契约 03 §6:0.002)。
const defaultCityCPI = 0.002

// PoolModelDisplay 池驱动座位(model_key=="")的展示名(契约 04 §1.3)。
const PoolModelDisplay = "LLM线路池"

// SetLinePoolSource 注入 LLM 线路池来源(Manager.CreateRoom / main 装配;
// nil = 无池,座位必须显式 model_key)。
func (r *VirtualCityRoom) SetLinePoolSource(fn func() *llm.LinePool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.linePoolSource = fn
}

// SetCityVoiceConfig 城市之声开关与每月条数(Manager.CreateRoom 注入;
// perMonth 使用点 clamp [0,32])。
func (r *VirtualCityRoom) SetCityVoiceConfig(enabled bool, perMonth int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cityVoiceEnabled = enabled
	r.cityVoicePerMonth = clampInt(perMonth, 0, 32)
}

// SetCityDriverConfig 居民驱动层开关与线程池/月预算(2026-09-22 §17-CityHuman
// 契约 02 §5;Manager.CreateRoom 注入,须在 Start 之前)。enabled=false 时
// Start 装配 VoiceScheduler(回退既有城市之声路径,零回归)。
func (r *VirtualCityRoom) SetCityDriverConfig(enabled bool, workers, perMonth int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cityDriverEnabled = enabled
	r.cityDriverWorkers = workers
	r.cityDriverPerMonth = perMonth
}

// SetResidentCount 设置城市背景居民数(建房链路;负数防御为 0)。
// 注意:仅 Start 前生效 —— 城市在 Start 时一次性合成。
func (r *VirtualCityRoom) SetResidentCount(n int) {
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
// 2026-09-22 §17-CityHuman(契约 02 §5):驱动层启用 → 装配 ResidentDriver
// (与 VoiceScheduler 互斥,speak 产出即城市之声);关闭 → 回退旧调度器。
func (r *VirtualCityRoom) startCityLocked() {
	if r.ResidentCount <= 0 {
		return
	}
	r.cityRng = rand.New(rand.NewSource(r.seed ^ citySeedSalt))
	r.City = city.NewBackdrop(r.ResidentCount, r.cityRng, city.CurrentCalibration())
	if r.cityDriverEnabled {
		r.cityDriver = city.NewResidentDriver(city.DriverConfig{
			Enabled:  true,
			Workers:  r.cityDriverWorkers,
			PerMonth: r.cityDriverPerMonth,
		}, r.linePoolSource)
		r.cityDriver.SetAmbianceSource(r.cityAmbianceSource)
		r.City.SetDriver(r.cityDriver)
		r.cityVoice = nil
	} else {
		r.cityDriver = nil
		r.cityVoice = city.NewVoiceScheduler(r.cityVoiceEnabled, r.cityVoicePerMonth, r.linePoolSource)
	}
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
		zap.Int("pool_lines", poolLines),
		zap.Bool("driver_enabled", r.cityDriverEnabled))
	// 2026-09-21 §档案锚定(契约 §5):文档池注入时后台异步把人物卡档案锚定
	// 到居民(不阻塞开局/月结,期间合成数值兜底)。2026-09-22 §17-CityHuman
	// (契约 03 §2.1):pool=="docs" 条件删除 —— 没有非 docs 模式,锚定流水线
	// 恒启动。锁纪律:Start(锁内)在此把全部入参拷贝进 goroutine 参数(§11:
	// goroutine 不读 r.mu 保护的可变字段);goroutine 只拿 Backdrop.mu,绝不
	// 触碰 r.mu(§92a)。
	if r.docLoader != nil {
		loader, n, seed, b := r.docLoader, r.ResidentCount, r.seed, r.City
		go r.anchorCityProfiles(b, loader, n, seed)
	}
}

// cityAmbianceSource 驱动层氛围来源(自取房间锁的薄包装;driver worker 在
// Backdrop.mu / r.mu 之外调用,无嵌套 —— §92a)。
func (r *VirtualCityRoom) cityAmbianceSource() map[string]city.AmbianceTags {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cityAmbianceLocked()
}

// anchorCityProfiles 档案锚定流水线(后台 goroutine 锁外执行;契约 §5)。
// 1. DrawPaths 抽 n 条不重复路径(零 IO)→ 2. 置 hydrating 进度 →
// 3. HydrateBatch 并行水合(进度每 512 张经 SetProfileProgress 回写,随
// Snapshot 自然下发,无额外广播)→ 4. AnchorProfiles 一次性热替换 →
// 5. Info 日志 + 终态 city_profiles 事件(BroadcastHooks 锁外)。
func (r *VirtualCityRoom) anchorCityProfiles(b *city.Backdrop, loader *profession.Loader, n int, seed int64) {
	defer func() {
		if rec := recover(); rec != nil {
			logger.L().Error("wealth city profile anchor panic recovered",
				zap.String("room_id", r.RoomID),
				zap.Any("panic", rec),
				zap.String("stack", string(debug.Stack())))
		}
	}()
	if b == nil || loader == nil || n <= 0 {
		return
	}
	rng2 := rand.New(rand.NewSource(seed ^ cityProfileSeedSalt))
	paths := loader.DrawPaths(n, rng2)
	poolSize := loader.PoolSize()
	if len(paths) == 0 {
		// 根目录缺失/索引空:failed + 合成兜底(契约 §10),REST progress
		// 如实披露,终态事件照发(前端灰字提示)。
		b.SetProfileProgress(city.ProfFailed, 0, 0, poolSize)
		logger.L().Warn("wealth city profile anchor failed: docs pool empty",
			zap.String("room_id", r.RoomID),
			zap.String("pool_root", loader.Root()),
			zap.Int("residents", n))
		r.emitCityProfilesEvent(0, 0, false)
		return
	}
	b.SetProfileProgress(city.ProfHydrating, 0, len(paths), poolSize)
	cards, failed := loader.HydrateBatch(paths, 0, func(done, total int) {
		b.SetProfileProgress(city.ProfHydrating, done, total, poolSize)
	})
	// 成功路径集 = paths − failed(与 cards 按序一一对应,供 SourceFile 审计)。
	successPaths := paths
	if len(failed) > 0 {
		bad := make(map[string]struct{}, len(failed))
		for _, p := range failed {
			bad[p] = struct{}{}
		}
		successPaths = make([]string, 0, len(cards))
		for _, p := range paths {
			if _, isBad := bad[p]; !isBad {
				successPaths = append(successPaths, p)
			}
		}
	}
	anchored := b.AnchorProfiles(cards, successPaths)
	b.SetProfileProgress(city.ProfReady, len(paths), len(paths), poolSize)
	logger.L().Info("wealth city profiles anchored",
		zap.String("room_id", r.RoomID),
		zap.Int("anchored", anchored),
		zap.Int("hydrated", len(paths)),
		zap.Int("parse_failed", len(failed)),
		zap.Int("pool_size", poolSize),
		zap.Int64("seed", seed))
	r.emitCityProfilesEvent(anchored, len(paths), true)
}

// emitCityProfilesEvent 档案锚定终态事件(契约 §5:仅终态发一条)。追加在
// 锁内、BroadcastHooks 回调在锁外(§92a,与 emitCityVoiceEvent 同款)。
// 进度明细经 game.state.city.profiles 下发,事件 Text 只携带人类可读摘要。
func (r *VirtualCityRoom) emitCityProfilesEvent(anchored, total int, ok bool) {
	text := fmt.Sprintf("城市人物档案锚定完成 %d/%d", anchored, total)
	if !ok {
		text = "城市人物档案锚定失败,居民数值走合成兜底"
	}
	r.mu.Lock()
	inGame := !r.closed && r.Status == StatusPlaying && r.World != nil
	var ev EventRecord
	if inGame {
		ev = EventRecord{Month: r.World.Month, Type: EventCityProfiles, Seat: -1, Text: text}
		r.World.Events = append(r.World.Events, ev)
	}
	hooks := r.hooks
	roomID := r.RoomID
	r.mu.Unlock()
	if inGame && hooks.OnEvent != nil {
		hooks.OnEvent(roomID, ev)
	}
}

// CityProfilePage 分页居民档案(2026-09-21 §档案锚定契约 §7 REST 薄代理)。
// 先短锁取 City 指针,查询在 r.mu 锁外执行(Backdrop 自带互斥)—— 10 万级
// 线性扫绝不持 r.mu;未建城返回 idle 空页(不算错误)。
func (r *VirtualCityRoom) CityProfilePage(offset, limit int, q string) ([]city.ResidentProfile, int, city.ProfileProgress) {
	r.mu.Lock()
	b := r.City
	r.mu.Unlock()
	if b == nil {
		return nil, 0, city.ProfileProgress{Status: "idle"}
	}
	return b.ProfilesPage(offset, limit, q)
}

// CityProfileOf 按人物卡编号查单份档案(§7;未建城/未锚定/卡号未命中 → false)。
func (r *VirtualCityRoom) CityProfileOf(cardID string) (city.ResidentProfile, bool) {
	r.mu.Lock()
	b := r.City
	r.mu.Unlock()
	if b == nil {
		return city.ResidentProfile{}, false
	}
	return b.ProfileByCardID(cardID)
}

// resizeAgentSemLocked 线路池可用时把房间信号量容量抬到总线路数
// (契约 01 §3.2 B5:LinePool 可用且 Total()>0 → 容量 = Total();否则保持
// AgentConcurrency 默认)。仅在 Start 锁内调用 —— 此时 wakeBots 尚未运行,
// 无并发 runOneBot 读 r.agentSem,替换通道无数据竞争。
func (r *VirtualCityRoom) resizeAgentSemLocked() {
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
func (r *VirtualCityRoom) currentCPILocked() float64 {
	if r.World != nil && r.World.EconomyEnabled && r.World.Goods != nil && r.World.Goods.CPIMom != 0 {
		return r.World.Goods.CPIMom
	}
	return defaultCityCPI
}

// tickCityLocked 月结后演化城市(锁内;trySettle 在 SettleMonth 之后调用)。
func (r *VirtualCityRoom) tickCityLocked() {
	if r.City == nil || r.cityRng == nil {
		return
	}
	r.City.TickMonth(r.currentCPILocked(), r.cityRng)
}

// launchCityDriver 月结后触发居民驱动层(2026-09-22 §17-CityHuman 契约 02
// §5;原名 launchCityVoices):driver 启用走 ResidentDriver.RunMonth(线程池
// + 线路池,speak 产出即城市之声);关闭回退旧 VoiceScheduler.Run(零回归)。
// 锁外 goroutine 异步,绝不阻塞月结;voiceMonth 为结算后的当前月。
func (r *VirtualCityRoom) launchCityDriver(voiceMonth int) {
	r.mu.Lock()
	b := r.City
	drv := r.cityDriver
	sched := r.cityVoice
	closed := r.closed
	playing := r.Status == StatusPlaying
	r.mu.Unlock()
	if closed || !playing || b == nil {
		return
	}
	onRecord := func(vr city.VoiceRecord) {
		b.AppendVoice(vr)
		r.emitCityVoiceEvent(vr)
	}
	if drv != nil {
		go func() {
			defer func() {
				if rec := recover(); rec != nil {
					logger.L().Error("wealth city driver panic recovered",
						zap.String("room_id", r.RoomID),
						zap.Any("panic", rec),
						zap.String("stack", string(debug.Stack())))
				}
			}()
			drv.RunMonth(b, voiceMonth, onRecord)
		}()
		return
	}
	if sched == nil {
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
		sched.Run(b, voiceMonth, onRecord)
	}()
}

// emitCityVoiceEvent 把一条城市之声追加进房间事件流并广播 game.event
// (EventRecord type=EventCityVoice,契约 04 §1.3)。追加在锁内、广播在锁外(§92a)。
func (r *VirtualCityRoom) emitCityVoiceEvent(vr city.VoiceRecord) {
	ev := EventRecord{
		Month: vr.Month,
		Type:  EventCityVoice,
		Seat:  -1,
		Text:  fmt.Sprintf("%s:%s", vr.Name, vr.Text),
	}
	r.mu.Lock()
	inGame := !r.closed && r.Status == StatusPlaying && r.World != nil
	if inGame {
		r.World.Events = append(r.World.Events, ev)
		// hear 感知数据源:城市之声全城可闻(District 空 = 全城,2026-09-22 §CityHuman重构)。
		ar := &AgentRunner{room: r}
		ar.appendUtteranceLocked(UtteranceRecord{Month: vr.Month, Seat: -1, District: "", Text: ev.Text})
	}
	hooks := r.hooks
	r.mu.Unlock()
	if inGame && hooks.OnEvent != nil {
		hooks.OnEvent(r.RoomID, ev)
	}
}

// CitySnapshotView 返回城市快照(锁内;未建城返回 nil)。ws 层 view 路径用。
func (r *VirtualCityRoom) CitySnapshotView() *city.Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.City == nil {
		return nil
	}
	s := r.City.Snapshot()
	// 2026-09-22 §CityHuman重构:随快照下发各城区当月气味/声响标签(§6 city.ambiance)。
	amb := r.cityAmbianceLocked()
	if amb != nil {
		s.Ambiance = amb
	}
	return &s
}

// ResidentCountView 返回建房 resident_count(锁内;大厅列表/详情 🏙 徽标用)。
func (r *VirtualCityRoom) ResidentCountView() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ResidentCount
}
