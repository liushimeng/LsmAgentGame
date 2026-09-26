// Package virtual_city — manager.go: VirtualCityManager(房间注册表 + 装配入口)
// 2026-09-14 §财商流P0。
//
// 契约: 后端架构文档 §15。Manager 持 rooms map + llm registry + profession
// loader + chat 注入;agentSeater / gameJoiner 由 ws 层持有接口并调用,
// 以规避 service → game/virtual_city 的反向依赖。
package virtual_city

import (
	"context"
	"sync"
	"time"

	"LsmAgentGame/agent/vcplayer"
	"LsmAgentGame/errcode"
	"LsmAgentGame/game/virtual_city/city"
	"LsmAgentGame/game/virtual_city/profession"
	"LsmAgentGame/llm"
	"LsmAgentGame/llm/types"
	"LsmAgentGame/logger"
	"LsmAgentGame/service"

	"go.uber.org/zap"
)

// professionLoader 别名(避免 profession.Loader 在多处重写长名字)。
type professionLoader = profession.Loader

// professionLoaderNew 是 profession.Loader 构造函数的薄包装(供 manager 文件顶端引用)。
func professionLoaderNew(root string) *profession.Loader {
	return profession.NewLoader(root)
}

// VirtualCityRoomOptions 透传(manager 文件引用 service 包的同名类型)。
type VirtualCityRoomOptions = service.VirtualCityRoomOptions

// Config 是 Manager 装配参数(避免直接 import config 包;ws 层转换传入)。
type Config struct {
	MonthMs                 int
	AgentEnabled            bool
	AgentDecisionTimeoutSec int
	BotMaxActionsPerMonth   int
	Seed                    int64
	// AgentConcurrency 房间级 LLM 并发信号量容量;0 = DefaultAgentConcurrency(8)。
	// 2026-09-16 §12 座扩容 新增:10+ 抽样居民同月 LLM 调用时,4 并发会把
	// 12 人压成 4 批串行,月窗口(默认 8s)内后几批来不及跑。8 是 Provider
	// 配额与房间并发间的平衡(详见 engine.go DefaultAgentConcurrency 注释)。
	AgentConcurrency int
	// P1(2026-09-16 §财商流P1-2 §6.5):真实经济循环 / 社会调研总开关。
	// NewManager 归一:零值 → true(false 回退 P0 行为;与 cfg.VirtualCity 同名键)。
	EconomyEnabled bool
	SurveyEnabled  bool
	// P1-4(2026-09-19 §财商流P1-4 §11):商业保险与风险转移引擎总开关。
	// NewManager 归一:零值 → true(false 时投保/退保返回 35041、月结不扣缴、
	// 意外事件不掷骰)。
	InsuranceEnabled bool
	// CivicElectionEnabled 批次20(文档3 A2):市长选举启用。
	// **零值 = false —— 有意区别于 InsuranceEnabled/EconomyEnabled 家族的
	// 「零值→true」归一化**(它们靠 NewManager 兜底默认开;选举是 R8-2
	// 定义的「完全可选」机制,默认关闭保持旧行为零偏移)。
	// ⚠️ 后人勿按惯性在 NewManager 里加 `if !cfg.CivicElectionEnabled { = true }`。
	CivicElectionEnabled bool
	// 2026-09-21 §虚拟城市(契约 03 §7)— 城市背景层配置。
	// MaxResidents resident_count 上限(service 层 clamp 用);零值 → 100000。
	MaxResidents int
	// CityVoiceEnabled 城市之声开关;零值 → true。
	CityVoiceEnabled bool
	// CityVoicePerMonth 每月抽样条数;零值 → 4(房间层 clamp [0,32])。
	CityVoicePerMonth int
	// CityCalibSampleSize 校准表抽样卡数;零值 → 512。
	CityCalibSampleSize int
	// 2026-09-22 §17-CityHuman(契约 02 §7)— 居民驱动层配置。
	// CityDriverEnabled 驱动层总开关(false → Start 回退 VoiceScheduler;
	// 零值 = false,默认 true 由 config 层 CityDriverEnabledResolved 提供)。
	CityDriverEnabled bool
	// CityDriverWorkers 线程池 worker 数(使用点 NewResidentDriver clamp
	// [1,16],0 → 4)。
	CityDriverWorkers int
	// CityDriverPerMonth 每月驱动居民数(clamp [0,64];0 = 缺省 8,无「仅抽样层」语义)。
	CityDriverPerMonth int
	// AgentLLMMinIntervalMs 2026-09-26 §批次25(§3.3)— 每个 City-Human 的
	// LLM 调用令牌桶补充间隔(容量 2;0 = 缺省 30000,clamp [5000,300000];
	// config 层 virtual_city.agent_llm_min_interval_ms 同源归一)。
	AgentLLMMinIntervalMs int
}

// LLMRegistry 窄接口(llm.Registry 满足;避免 manager 包 import llm)。
type LLMRegistry interface {
	Get(modelKey string) (types.LLMProvider, string, error)
	GetThinkingEnabled(modelKey string) (bool, int)
}

// SeatRestoreInfo 是 Manager 从外部源(DB)恢复 in-memory 房间时的单座位数据。
type SeatRestoreInfo struct {
	Seat     int
	UserID   string
	IsBot    bool
	ModelKey string
}

// NewManager 构造空 Manager(loader 由 SetLoader 注入;不在构造期读取磁盘)。
func NewManager(cfg Config, reg LLMRegistry) *Manager {
	if cfg.BotMaxActionsPerMonth <= 0 {
		cfg.BotMaxActionsPerMonth = 3
	}
	if cfg.AgentDecisionTimeoutSec <= 0 {
		cfg.AgentDecisionTimeoutSec = 20
	}
	if cfg.MonthMs <= 0 {
		cfg.MonthMs = 8000
	}
	if cfg.AgentConcurrency <= 0 {
		cfg.AgentConcurrency = DefaultAgentConcurrency
	}
	// 批次 25:LLM 调用节流间隔归一(0/缺省 → 30000ms;clamp [5000,300000])。
	if cfg.AgentLLMMinIntervalMs <= 0 {
		cfg.AgentLLMMinIntervalMs = 30000
	}
	if cfg.AgentLLMMinIntervalMs < 5000 {
		cfg.AgentLLMMinIntervalMs = 5000
	}
	if cfg.AgentLLMMinIntervalMs > 300000 {
		cfg.AgentLLMMinIntervalMs = 300000
	}
	// P1(§6.5):零值 → true(默认开启;false 回退 P0)。
	if !cfg.EconomyEnabled {
		cfg.EconomyEnabled = true
	}
	if !cfg.SurveyEnabled {
		cfg.SurveyEnabled = true
	}
	// P1-4(§财商流P1-4 §11):零值 → true(默认开启)。
	if !cfg.InsuranceEnabled {
		cfg.InsuranceEnabled = true
	}
	// 2026-09-21 §虚拟城市:城市背景层默认值(契约 03 §7)。
	if cfg.MaxResidents <= 0 {
		cfg.MaxResidents = 100000
	}
	if !cfg.CityVoiceEnabled {
		cfg.CityVoiceEnabled = true
	}
	if cfg.CityVoicePerMonth == 0 {
		cfg.CityVoicePerMonth = 4
	}
	if cfg.CityCalibSampleSize == 0 {
		cfg.CityCalibSampleSize = 512
	}
	return &Manager{
		cfg:      cfg,
		registry: reg,
		rooms:    make(map[string]*VirtualCityRoom),
	}
}

// SetSeatHydrator 注入 DB 座位恢复回调(对齐德扑 BUG-WEREWOLF-P0-7)。
// 服务重启后内存房间 Seats/BotSeats 全空,必须靠 hydrator 从 t_lsm_game_player
// 拉人类 + agent_seats 拉 bot 才能让 Start() 的 occupiedLocked ≥ MinSeats。
func (m *Manager) SetSeatHydrator(h func(roomID string) ([]SeatRestoreInfo, error)) {
	m.seatHydrator = h
}

// SetLoader 注入文档池加载器(由 ws 层装配时传入)。
func (m *Manager) SetLoader(l *Loader) { m.loader = l }

// Loader 是 profession.Loader 的包内别名(避免 manager 文件顶端重复长 import)。
type Loader = professionLoader

// NewLoader 是 profession.Loader 的薄包装(供 main.go 装配)。
func NewLoader(root string) *Loader {
	return professionLoaderNew(root)
}

// Manager 是所有 VirtualCityRoom 的注册表。
type Manager struct {
	mu          sync.RWMutex
	cfg         Config
	registry    LLMRegistry
	loader      *Loader // profession.Loader(包内别名)
	rooms       map[string]*VirtualCityRoom
	pendingOpts map[string]*VirtualCityRoomOptions // roomSvc → Start 时取用
	// seatHydrator 服务重启后从 DB 恢复 in-memory 房间的座位信息(对齐德扑
	// BUG-WEREWOLF-P0-7 修复方案)。nil-safe,未设置时 Get/CreateRoom 跳过恢复。
	seatHydrator func(roomID string) ([]SeatRestoreInfo, error)
	// linePoolSource(2026-09-21 §虚拟城市 B3)LLM 线路池来源,由 main 经
	// SetLinePoolSource 注入(llmRegistry.LinePool)。池驱动座位(ModelKey=="")
	// 与城市之声共用;nil = 无池,EnsureAgents 只为显式 model_key 座位建 Agent。
	linePoolSource func() *llm.LinePool
}

// SetLinePoolSource 注入 LLM 线路池来源(Registry.Reload 换池后经函数现取
// 自动生效)。新房间在 CreateRoom 时继承;存量房间不回填(建 Agent 的时机
// 已过,由 EnsureAgents 幂等覆盖)。
func (m *Manager) SetLinePoolSource(fn func() *llm.LinePool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.linePoolSource = fn
}

// WarmCityCalibration 启动城市校准表后台预热(manager 初始化后调用一次;
// sync.Once + goroutine,不阻塞启动;契约 03 §3.2)。
func (m *Manager) WarmCityCalibration() {
	m.mu.RLock()
	loader, size := m.loader, m.cfg.CityCalibSampleSize
	m.mu.RUnlock()
	city.WarmUpCalibration(loader, size)
}

// ApplyRoomOptions 是 roomSvc.SetVirtualCityRoomConfigurer 的回调:
// 按房间 ID 应用房间级 wealth 配置(month_ms / pool / seed)。
func (m *Manager) ApplyRoomOptions(roomID string, opts *VirtualCityRoomOptions) {
	if opts == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.rooms[roomID]; ok {
		r.applyOpts(opts)
		return
	}
	if m.pendingOpts == nil {
		m.pendingOpts = make(map[string]*VirtualCityRoomOptions)
	}
	m.pendingOpts[roomID] = opts
}

// pendingOptsApply 在 CreateRoom 时取用(若有)。
func (m *Manager) pendingOptsApply(roomID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if opts, ok := m.pendingOpts[roomID]; ok {
		if r, ok2 := m.rooms[roomID]; ok2 {
			r.applyOpts(opts)
		}
		delete(m.pendingOpts, roomID)
	}
}

// Get 返回房间;不存在返回 nil。
func (m *Manager) Get(roomID string) *VirtualCityRoom {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.rooms[roomID]
}

// CreateRoom 新建房间(默认不开局;Start 触发)。
func (m *Manager) CreateRoom(roomID string) *VirtualCityRoom {
	m.mu.Lock()
	r, ok := m.rooms[roomID]
	if ok {
		m.mu.Unlock()
		// 已存在：释放锁后再 apply pending opts（applyOpts 自身不持 m.mu）。
		m.pendingOptsApply(roomID)
		return r
	}
	r = NewVirtualCityRoom(roomID, m.cfg.MonthMs, m.cfg.Seed, m.cfg.AgentConcurrency)
	// P1(§6.5):economy/survey 房间级开关接线(Start 前回写)。
	r.SetEconomyFlags(m.cfg.EconomyEnabled, m.cfg.SurveyEnabled)
	// P1-4(§财商流P1-4 §11):保险引擎开关接线(Start 前回写)。
	r.SetInsuranceEnabled(m.cfg.InsuranceEnabled)
	// 批次20(文档3 A2):市长选举开关接线(零值=false,不做归一化;
	// 建房 HTTP body civic_election_enabled 经 applyOpts 房间级覆盖)。
	r.SetElectionEnabled(m.cfg.CivicElectionEnabled)
	// 2026-09-21 §虚拟城市:线路池来源 + 城市之声配置接线(Start 前回写;
	// m.mu 写锁内调房间锁,锁序 m.mu → r.mu 全库一致,无反向路径)。
	r.SetLinePoolSource(m.linePoolSource)
	r.SetCityVoiceConfig(m.cfg.CityVoiceEnabled, m.cfg.CityVoicePerMonth)
	// 2026-09-22 §17-CityHuman(契约 02 §5):居民驱动层配置接线(与城市之声
	// 互斥,Start 时按开关二选一装配)。
	r.SetCityDriverConfig(m.cfg.CityDriverEnabled, m.cfg.CityDriverWorkers, m.cfg.CityDriverPerMonth)
	// 2026-09-26 §批次25(§3.3):LLM 令牌桶间隔接线(驱动层 RunMonth 共享桶用;
	// 座位 Agent 桶在 ensureAgentsWithPool 装配)。
	r.SetAgentLLMMinIntervalMs(m.cfg.AgentLLMMinIntervalMs)
	if m.loader != nil {
		r.mu.Lock()
		r.docLoader = m.loader
		r.mu.Unlock()
	}
	// 重启后第一次访问:从 DB 把座位恢复回来,否则 Start() 的 occupiedLocked
	// 永远 < MinSeats → ErrVirtualCityNotEnoughPlayers(35003),游戏永远无法开局。
	if m.seatHydrator != nil {
		seats, err := m.seatHydrator(roomID)
		if err == nil && len(seats) > 0 {
			r.mu.Lock()
			restoredBots := 0
			for _, s := range seats {
				if s.Seat < 0 || s.Seat >= MaxSeats || s.UserID == "" {
					continue
				}
				if r.Seats[s.Seat] == "" {
					r.Seats[s.Seat] = s.UserID
				}
				if s.IsBot {
					r.BotSeats[s.Seat] = true
					restoredBots++
					if s.ModelKey != "" {
						r.SeatModelKeys[s.Seat] = s.ModelKey
					}
					// 2026-09-21 §虚拟城市(契约 01 §4):空 model_key =
					// 池驱动座位,SeatModelKeys 存空串;EnsureAgents 按
					// 「非空 key 或池可用」建 Agent,两态幂等。
				} else if s.ModelKey != "" {
					// 允许人类玩家保留 ModelKey 字段(暂不影响 EnsureAgents)。
					r.SeatModelKeys[s.Seat] = s.ModelKey
				}
			}
			r.mu.Unlock()
			// 重启恢复的 10/11 bot 房同样具有全 Agent 语义。必须在房间锁
			// 释放后、房间登记可见前恢复标记,剩余 1-2 物理空位不得重新
			// 接受人类创建者/加入者。
			if restoredBots >= MinSeats {
				r.SetFullAgentMode(true)
			}
			logger.L().Info("virtual_city room seats hydrated from DB",
				zap.String("room_id", roomID),
				zap.Int("restored", len(seats)))
			// P1-Ghost-03 修复:重建路径(服务重启后首次访问)在 EnsureAgents 之前
			// 先把座位与 SeatModelKeys/BotSeats 还原回内存房;此处立刻装配 bot
			// agent,即便后续 startVirtualCityRoom 路径再次 EnsureAgents,也是幂等的
			// (agents map 在 EnsureAgents 内整体覆盖)。
			if restoredBots > 0 {
				// §92a(2026-09-21 线上 P0):此处仍持 m.mu 写锁,EnsureAgents
				// 内部再取 m.mu.RLock 会自死锁(Go RWMutex 不可重入,导致所有
				// 带 agent 座位的建房请求挂死)。改走锁内变体,poolSource 在
				// 写锁现场直读,装配时序与日志不变。
				m.ensureAgentsWithPool(m.linePoolSource, r)
			}
		}
	}
	m.rooms[roomID] = r
	// 把 pending opts 应用到新建房间；必须先把 m.rooms[roomID] 已登记再释放锁，
	// 避免与并发的 Get/CreateRoom 出现「先读 m.rooms、后补 opts」的可见性窗口。
	opts := m.pendingOpts[roomID]
	delete(m.pendingOpts, roomID)
	m.mu.Unlock()
	if opts != nil {
		r.applyOpts(opts)
	}
	// 日志必须读取 pending opts 应用后的房间最终配置,不能打印 Manager 默认值;
	// 否则 3000ms 房间会被误记为 8000ms。
	monthMs, residents := r.roomConfigForLog()
	logger.L().Info("virtual_city room created",
		zap.String("room_id", roomID),
		zap.Int("month_ms", monthMs),
		zap.Int("resident_count", residents))
	return r
}

// roomConfigForLog 返回房间级配置快照,专供创建日志使用(pool 字段随精选层
// 退役删除,2026-09-22 §17-CityHuman;改下发自建居民数)。
func (r *VirtualCityRoom) roomConfigForLog() (monthMs, residents int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.MonthMs, r.ResidentCount
}

// RemoveRoom 移除房间(终局清理时由 ws 层调用)。
func (m *Manager) RemoveRoom(roomID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.rooms[roomID]; ok {
		r.Close()
		delete(m.rooms, roomID)
		logger.L().Info("virtual_city room removed", zap.String("room_id", roomID))
	}
}

// RoomIDs 列出全部房间 id(shutdown/通知用)。
func (m *Manager) RoomIDs() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.rooms))
	for id := range m.rooms {
		out = append(out, id)
	}
	return out
}

// EnsureAgents 给房间的 bot 座位装配 vcplayer.Agent(由 ws 层在开局后调用)。
// 2026-09-21 §虚拟城市 B3:ModelKey==""(池驱动)且线路池可用(Total>0)的座位
// 同样建 Agent —— 调用时先 Acquire 线路,Acquire 失败走既有 submit_month 兜底;
// 池不可用时跳过(行为与旧版一致,防无 LLM 空转)。
//
// §92a(2026-09-21 线上 P0 复发):本方法取 m.mu.RLock,禁止在任何已持 m.mu
// (读/写)的调用路径内使用 —— CreateRoom hydrate 段持写锁,必须改走
// ensureAgentsWithPool(m.linePoolSource, r) 锁内变体。
func (m *Manager) EnsureAgents(r *VirtualCityRoom) {
	if !m.cfg.AgentEnabled || m.registry == nil {
		return
	}
	m.mu.RLock()
	poolSource := m.linePoolSource
	m.mu.RUnlock()
	m.ensureAgentsWithPool(poolSource, r)
}

// ensureAgentsWithPool 是 EnsureAgents 的锁内变体(§92a:自身不取 m.mu,可在
// Manager 写锁内安全调用;poolSource 由调用方在其持锁现场直读)。池驱动座位
// 语义(§虚拟城市 B3)在此实现,两个入口共用同一份逻辑。
func (m *Manager) ensureAgentsWithPool(poolSource func() *llm.LinePool, r *VirtualCityRoom) {
	if !m.cfg.AgentEnabled || m.registry == nil {
		return
	}
	poolAvailable := false
	if poolSource != nil {
		if pool := poolSource(); pool != nil && pool.Total() > 0 {
			poolAvailable = true
		}
	}
	r.mu.Lock()
	seats := make([]int, 0)
	for seat, isBot := range r.BotSeats {
		if isBot && r.Seats[seat] != "" {
			seats = append(seats, seat)
		}
	}
	r.mu.Unlock()
	for _, seat := range seats {
		modelKey := r.SeatModelKeys[seat]
		if modelKey == "" && !poolAvailable {
			continue
		}
		display := ModelDisplayName(modelKey)
		if modelKey == "" {
			display = PoolModelDisplay
		}
		agent := vcplayer.NewAgent(r.RoomID, r.Seats[seat], modelKey,
			display, seat,
			m.cfg.BotMaxActionsPerMonth,
			time.Duration(m.cfg.AgentDecisionTimeoutSec)*time.Second)
		agent.BindRegistry(m.registry)
		// 批次 25(§3.3):LLM 令牌桶(配置间隔)+ 调用计数钩子(房间统计)。
		agent.SetLLMRateLimit(time.Duration(m.cfg.AgentLLMMinIntervalMs) * time.Millisecond)
		agent.BindLLMCallHook(func(modelKey string) { r.noteSeatLLMCall(seat, modelKey) })
		if modelKey == "" {
			agent.BindLinePoolSource(poolSource)
		}
		runner := NewAgentRunner(r, seat)
		agent.BindRunner(runner)
		r.mu.Lock()
		r.agents[seat] = agent
		r.mu.Unlock()
	}
}

// ModelDisplayName 把 model key 渲染成"模型 名称";真实场景由 registry 注入,
// 此处 fallback 用 key 本身。
func ModelDisplayName(modelKey string) string {
	return "模型 " + modelKey
}

// (2026-09-26 §批次25 §130 死代码清算:Manager.wakeBots/wakeOne 已删除 ——
// 零生产调用点(房间侧 wakeBots/runOneBot 是唯一调度路径,且批次 25 已把
// 满槽无界排队 goroutine 改为 per-seat 单槽 pending);runAgentMonth 仍有
// agent_observability_test.go 消费,保留。)

func (m *Manager) runAgentMonth(r *VirtualCityRoom, seat int, agent *vcplayer.Agent) {
	// 房间持锁构造 GameContext 快照,锁外消费。
	ctx, ok := BuildContextForAgent(r, seat)
	if !ok || ctx == nil {
		return
	}
	agent.OnMonthStart(context.Background(), ctx)
}

// context alias for cleanup.
var _ = context.Background
var _ = errcode.ErrVirtualCityActionBudgetExhausted
