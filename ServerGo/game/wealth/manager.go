// Package wealth — manager.go: WealthManager(房间注册表 + 装配入口)
// 2026-09-14 §财商流P0。
//
// 契约: 后端架构文档 §15。Manager 持 rooms map + llm registry + profession
// loader + chat 注入;agentSeater / gameJoiner 由 ws 层持有接口并调用,
// 以规避 service → game/wealth 的反向依赖。
package wealth

import (
	"context"
	"sync"
	"time"

	"LsmAgentGame/agent/wealthplayer"
	"LsmAgentGame/errcode"
	"LsmAgentGame/game/wealth/profession"
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

// WealthRoomOptions 透传(manager 文件引用 service 包的同名类型)。
type WealthRoomOptions = service.WealthRoomOptions

// Config 是 Manager 装配参数(避免直接 import config 包;ws 层转换传入)。
type Config struct {
	MonthMs                 int
	AgentEnabled            bool
	AgentDecisionTimeoutSec int
	BotMaxActionsPerMonth   int
	PoolDefault             string // "curated" | "docs"
	Seed                    int64
	// AgentConcurrency 房间级 LLM 并发信号量容量;0 = DefaultAgentConcurrency(8)。
	// 2026-09-16 §12 座扩容 新增:10+ bot 同月决策时,4 并发会把 12 人压成
	// 4 批串行,月窗口(默认 8s)内后几批来不及跑。8 是 Provider 配额与房间
	// 并发间的平衡(详见 engine.go DefaultAgentConcurrency 注释)。
	AgentConcurrency int
	// P1(2026-09-16 §财商流P1-2 §6.5):真实经济循环 / 社会调研总开关。
	// NewManager 归一:零值 → true(false 回退 P0 行为;与 cfg.Wealth 同名键)。
	EconomyEnabled bool
	SurveyEnabled  bool
	// P1-4(2026-09-19 §财商流P1-4 §11):商业保险与风险转移引擎总开关。
	// NewManager 归一:零值 → true(false 时投保/退保返回 35041、月结不扣缴、
	// 意外事件不掷骰)。
	InsuranceEnabled bool
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
	return &Manager{
		cfg:      cfg,
		registry: reg,
		rooms:    make(map[string]*WealthRoom),
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

// Manager 是所有 WealthRoom 的注册表。
type Manager struct {
	mu          sync.RWMutex
	cfg         Config
	registry    LLMRegistry
	loader      *Loader // profession.Loader(包内别名)
	rooms       map[string]*WealthRoom
	pendingOpts map[string]*WealthRoomOptions // roomSvc → Start 时取用
	// seatHydrator 服务重启后从 DB 恢复 in-memory 房间的座位信息(对齐德扑
	// BUG-WEREWOLF-P0-7 修复方案)。nil-safe,未设置时 Get/CreateRoom 跳过恢复。
	seatHydrator func(roomID string) ([]SeatRestoreInfo, error)
}

// ApplyRoomOptions 是 roomSvc.SetWealthRoomConfigurer 的回调:
// 按房间 ID 应用房间级 wealth 配置(month_ms / pool / seed)。
func (m *Manager) ApplyRoomOptions(roomID string, opts *WealthRoomOptions) {
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
		m.pendingOpts = make(map[string]*WealthRoomOptions)
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
func (m *Manager) Get(roomID string) *WealthRoom {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.rooms[roomID]
}

// CreateRoom 新建房间(默认不开局;Start 触发)。
func (m *Manager) CreateRoom(roomID string) *WealthRoom {
	m.mu.Lock()
	r, ok := m.rooms[roomID]
	if ok {
		m.mu.Unlock()
		// 已存在：释放锁后再 apply pending opts（applyOpts 自身不持 m.mu）。
		m.pendingOptsApply(roomID)
		return r
	}
	r = NewWealthRoom(roomID, m.cfg.MonthMs, m.cfg.PoolDefault, m.cfg.Seed, m.cfg.AgentConcurrency)
	// P1(§6.5):economy/survey 房间级开关接线(Start 前回写)。
	r.SetEconomyFlags(m.cfg.EconomyEnabled, m.cfg.SurveyEnabled)
	// P1-4(§财商流P1-4 §11):保险引擎开关接线(Start 前回写)。
	r.SetInsuranceEnabled(m.cfg.InsuranceEnabled)
	if m.loader != nil {
		r.mu.Lock()
		r.docLoader = m.loader
		r.mu.Unlock()
	}
	// 重启后第一次访问:从 DB 把座位恢复回来,否则 Start() 的 occupiedLocked
	// 永远 < MinSeats → ErrWealthNotEnoughPlayers(35003),游戏永远无法开局。
	if m.seatHydrator != nil {
		seats, err := m.seatHydrator(roomID)
		if err == nil && len(seats) > 0 {
			r.mu.Lock()
			hydratedBots := 0
			for _, s := range seats {
				if s.Seat < 0 || s.Seat >= MaxSeats || s.UserID == "" {
					continue
				}
				if r.Seats[s.Seat] == "" {
					r.Seats[s.Seat] = s.UserID
				}
				if s.IsBot {
					r.BotSeats[s.Seat] = true
					if s.ModelKey != "" {
						r.SeatModelKeys[s.Seat] = s.ModelKey
						hydratedBots++
					}
				} else if s.ModelKey != "" {
					// 允许人类玩家保留 ModelKey 字段(暂不影响 EnsureAgents)。
					r.SeatModelKeys[s.Seat] = s.ModelKey
				}
			}
			r.mu.Unlock()
			// 重启恢复的 10/11 bot 房同样具有全 Agent 语义。必须在房间锁
			// 释放后、房间登记可见前恢复标记,剩余 1-2 物理空位不得重新
			// 接受人类创建者/加入者。
			if hydratedBots >= MinSeats {
				r.SetFullAgentMode(true)
			}
			logger.L().Info("wealth room seats hydrated from DB",
				zap.String("room_id", roomID),
				zap.Int("restored", len(seats)))
			// P1-Ghost-03 修复:重建路径(服务重启后首次访问)在 EnsureAgents 之前
			// 先把座位与 SeatModelKeys/BotSeats 还原回内存房;此处立刻装配 bot
			// agent,即便后续 startWealthRoom 路径再次 EnsureAgents,也是幂等的
			// (agents map 在 EnsureAgents 内整体覆盖)。
			if hydratedBots > 0 {
				m.EnsureAgents(r)
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
	// 否则 3000/curated 房间会被误记为 8000/docs。
	monthMs, pool := r.roomConfigForLog()
	logger.L().Info("wealth room created",
		zap.String("room_id", roomID),
		zap.Int("month_ms", monthMs),
		zap.String("pool", pool))
	return r
}

// roomConfigForLog 返回房间级配置快照,专供创建日志使用。
func (r *WealthRoom) roomConfigForLog() (monthMs int, pool string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.MonthMs, r.pool
}

// RemoveRoom 移除房间(终局清理时由 ws 层调用)。
func (m *Manager) RemoveRoom(roomID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.rooms[roomID]; ok {
		r.Close()
		delete(m.rooms, roomID)
		logger.L().Info("wealth room removed", zap.String("room_id", roomID))
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

// EnsureAgents 给房间的 bot 座位装配 wealthplayer.Agent(由 ws 层在开局后调用)。
func (m *Manager) EnsureAgents(r *WealthRoom) {
	if !m.cfg.AgentEnabled || m.registry == nil {
		return
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
		if modelKey == "" {
			continue
		}
		agent := wealthplayer.NewAgent(r.RoomID, r.Seats[seat], modelKey,
			ModelDisplayName(modelKey), seat,
			m.cfg.BotMaxActionsPerMonth,
			time.Duration(m.cfg.AgentDecisionTimeoutSec)*time.Second)
		agent.BindRegistry(m.registry)
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

// wakeBots 唤醒全部 bot 座位决策(在锁外由房间调用,本方法在 manager 上;
// 房间自身有 wakeBots 间接调用)。
func (m *Manager) wakeBots(r *WealthRoom) {
	r.mu.Lock()
	agents := make(map[int]*wealthplayer.Agent, len(r.agents))
	for s, a := range r.agents {
		agents[s] = a
	}
	closed := r.closed
	r.mu.Unlock()
	if closed {
		return
	}
	for seat, agent := range agents {
		go m.wakeOne(r, seat, agent)
	}
}

func (m *Manager) wakeOne(r *WealthRoom, seat int, agent *wealthplayer.Agent) {
	// 房间级并发信号量(默认 4;NewWealthRoom 已设)。
	select {
	case r.agentSem <- struct{}{}:
		defer func() { <-r.agentSem }()
	default:
		// 满槽:稍后再 wake。
		go func() {
			r.agentSem <- struct{}{}
			defer func() { <-r.agentSem }()
			m.runAgentMonth(r, seat, agent)
		}()
		return
	}
	m.runAgentMonth(r, seat, agent)
}

func (m *Manager) runAgentMonth(r *WealthRoom, seat int, agent *wealthplayer.Agent) {
	// 房间持锁构造 GameContext 快照,锁外消费。
	ctx, ok := BuildContextForAgent(r, seat)
	if !ok || ctx == nil {
		return
	}
	agent.OnMonthStart(context.Background(), ctx)
}

// context alias for cleanup.
var _ = context.Background
var _ = errcode.ErrWealthActionBudgetExhausted
