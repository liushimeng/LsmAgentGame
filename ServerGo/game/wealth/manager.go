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
	MonthMs                int
	AgentEnabled           bool
	AgentDecisionTimeoutSec int
	BotMaxActionsPerMonth  int
	PoolDefault            string // "curated" | "docs"
	Seed                   int64
}

// LLMRegistry 窄接口(llm.Registry 满足;避免 manager 包 import llm)。
type LLMRegistry interface {
	Get(modelKey string) (types.LLMProvider, string, error)
	GetThinkingEnabled(modelKey string) (bool, int)
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
	return &Manager{
		cfg:      cfg,
		registry: reg,
		rooms:    make(map[string]*WealthRoom),
	}
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
	defer m.mu.Unlock()
	r, ok := m.rooms[roomID]
	if ok {
		m.mu.Unlock()
		m.pendingOptsApply(roomID)
		m.mu.Lock()
		return r
	}
	r = NewWealthRoom(roomID, m.cfg.MonthMs, m.cfg.PoolDefault, m.cfg.Seed, 4)
	if m.loader != nil {
		r.mu.Lock()
		r.docLoader = m.loader
		r.mu.Unlock()
	}
	m.rooms[roomID] = r
	logger.L().Info("wealth room created",
		zap.String("room_id", roomID),
		zap.Int("month_ms", m.cfg.MonthMs),
		zap.String("pool", m.cfg.PoolDefault))
	m.pendingOptsApply(roomID)
	return r
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