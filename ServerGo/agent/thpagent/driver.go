// Package thpagent — driver.go: 房间级 Agent 驱动器（2026-08-19）。
//
// 职责：
//   - 维护 roomID → agents[6]*Agent 映射
//   - 启动/停止 Agent goroutines
//   - 提供 DecideAction / RegisterAgents / UnregisterAgents 公开 API
//   - **所有** 修改 r/agents 的方法必须有 *Locked 锁内变体（§92a）
package thpagent

import (
	"context"
	"sync"
	"time"

	agentroot "LsmAgentGame/agent"
	"LsmAgentGame/llm/types"
	"LsmAgentGame/logger"
	"go.uber.org/zap"
)

// Registry is a thin interface over llm.Registry so the thpagent package
// doesn't import llm (avoids circular dependency risk). The driver only
// uses Get(); concrete wiring is done from ws/game_service.go.
type Registry interface {
	Get(modelKey string) (any, string, error)
}

// Driver 是房间级 Agent 驱动器（每个 TexasHoldemManager 一个）。
type Driver struct {
	mu sync.RWMutex

	// roomID -> roomAgents
	rooms map[string]*RoomAgents

	// 配置（注入）
	maxActionTimeoutSec int
	registry            Registry

	// wallet/profile 服务（占位 — v1.1 注入）
	walletCb  func(userID string, delta int64, reason string) error
	profileCb func(modelKey string) error
}

// RoomAgents 是单房间的 Agent 集合。
type RoomAgents struct {
	roomID   string
	agents   [6]*Agent           // 6 个座位(空座位 nil)
	dispatch [6]*Dispatcher      // 每个 Agent 一个 dispatcher
	memories [6]*Memory
	models   [6]string           // seat -> model_key
}

// NewDriver 构造一个空 Driver。
func NewDriver() *Driver {
	return &Driver{
		rooms:                make(map[string]*RoomAgents),
		maxActionTimeoutSec:   30,
	}
}

// SetRegistry 注入 LLM Registry（必须在 DecideAction 之前调用）。
func (d *Driver) SetRegistry(r Registry) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.registry = r
}

// RegisterAgents 注册 Agent 座位。必须在 TexasHoldemManager.StartHand 之前调用。
//
// 参数：
//   - roomID: 房间 ID
//   - seatConfigs: 座位配置（每个含 seat + model_key + agent_name）
//
// 锁内变体（§92a）：调用方必须已持有 room 锁。
func (d *Driver) RegisterAgents(roomID string, seatConfigs []SeatConfig) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	ra, ok := d.rooms[roomID]
	if !ok {
		ra = &RoomAgents{roomID: roomID}
		d.rooms[roomID] = ra
	}

	for _, cfg := range seatConfigs {
		if cfg.Seat < 0 || cfg.Seat >= 6 {
			continue
		}
		a := NewAgent(roomID, cfg.UserID, cfg.ModelKey, cfg.Seat)
		a.ModelName = cfg.ModelName
		if d.registry != nil {
			// 注入 Provider（§20260813-04 U1 wiring）
			if p, _, err := d.registry.Get(cfg.ModelKey); err == nil && p != nil {
				if prov, ok := p.(types.LLMProvider); ok {
					a.SetProvider(prov, cfg.ModelKey)
				}
			}
		}
		ra.agents[cfg.Seat] = a
		ra.dispatch[cfg.Seat] = NewDispatcher()
		ra.memories[cfg.Seat] = NewMemory()
		ra.models[cfg.Seat] = cfg.ModelKey

		// 启动 Agent goroutine
		go a.Run()

		logger.L().Info("texasholdem agent registered",
			zap.String("room_id", roomID),
			zap.Int("seat", cfg.Seat),
			zap.String("model", cfg.ModelKey))
	}

	return nil
}

// SeatConfig 是单个 Agent 座位配置。
type SeatConfig struct {
	Seat      int
	UserID    string
	ModelKey  string
	ModelName string
}

// UnregisterAgents 注销房间的所有 Agent。
func (d *Driver) UnregisterAgents(roomID string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	ra, ok := d.rooms[roomID]
	if !ok {
		return
	}
	for i := 0; i < 6; i++ {
		if ra.agents[i] != nil {
			ra.agents[i].Cancel()
			ra.agents[i] = nil
		}
		ra.dispatch[i] = nil
		ra.memories[i] = nil
	}
	delete(d.rooms, roomID)
	logger.L().Info("texasholdem agents unregistered", zap.String("room_id", roomID))
}

// DecideAction 让指定 bot 座位做决策。返回 (action, error)。
//
// 阻塞调用，超时由 ctx 控制（默认 30s）。
// 锁内变体（§92a）：调用方必须已持有 room 锁；此函数**不**修改房间状态,仅读 + 调 LLM。
func (d *Driver) DecideAction(ctx context.Context, roomID string, seat int, promptContext *GameContextForAgent) (Action, error) {
	d.mu.RLock()
	ra, ok := d.rooms[roomID]
	if !ok {
		d.mu.RUnlock()
		return Action{}, ErrRoomNotRegistered
	}
	if seat < 0 || seat >= 6 || ra.agents[seat] == nil {
		d.mu.RUnlock()
		return Action{}, ErrSeatNotRegistered
	}
	a := ra.agents[seat]
	disp := ra.dispatch[seat]
	mem := ra.memories[seat]
	d.mu.RUnlock()

	if a.IsCancelled() {
		return Action{}, ErrAgentCancelled
	}

	// 调用 LLM（v1.0 简化版: 直接返回 fold 占位, 真实 LLM 接入待 v1.1）
	// 这里保留接口,实际 LLM 接入在 Run() 内异步触发后通过 channel 回调。
	// 当前实现走「超时即 fold」兜底。
	_ = a
	_ = disp
	_ = mem
	_ = promptContext

	// 简化版:返回 fold（v1.0 暂不接入真实 LLM,等 v1.1）
	// 这与设计文档一致——「真实 LLM 接入待 v1.1」是 §1.0 范围声明。
	// v1.1 将在此实现 BuildLLMRequest + callProvider 完整链路。
	actionTimeout := time.Duration(d.maxActionTimeoutSec) * time.Second
	timer := time.NewTimer(actionTimeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		// 上层 ctx 超时 → fold 兜底
		return Action{Type: ActFold, Thought: "decision timeout, forced fold"}, nil
	case <-timer.C:
		// Driver 内部超时 → fold 兜底
		return Action{Type: ActFold, Thought: "internal timeout, forced fold"}, nil
	}
}

// 错误定义
var (
	ErrRoomNotRegistered   = driverError("room not registered with driver")
	ErrSeatNotRegistered   = driverError("seat not registered with driver")
	ErrAgentCancelled      = driverError("agent cancelled")
)

type driverError string

func (e driverError) Error() string { return string(e) }

// GetAgentCountForRoom 返回房间的 bot 数量（供测试 / 调试）。
func (d *Driver) GetAgentCountForRoom(roomID string) int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	ra, ok := d.rooms[roomID]
	if !ok {
		return 0
	}
	count := 0
	for _, a := range ra.agents {
		if a != nil {
			count++
		}
	}
	return count
}

// SuppressUnusedImport agentroot 用于跨包 lint（防止后续重构误删）。
var _ = agentroot.AgentClassTexasHoldemPlayer