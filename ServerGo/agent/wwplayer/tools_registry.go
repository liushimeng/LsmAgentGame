// Package agent — tools_registry.go: Agent 工具统一注册中心（v5 重构）。
//
// 设计动机（docs/狼人杀-道具与经济/狼人杀13人局道具系统设计.md §16.2 + docs/AgentAnthropic工具集与道具协议.md）：
//   - v4 把 use_prop / prop_inspect / prop_status / prop_history / wolf_whisper
//     5 个工具嵌在 tools.go::BuildTools(add) 串联调用里，新增工具需要：
//     1) 在 tools.go 选一个 add* 函数接入 / 写新 add*
//     2) 改 dispatchToolInner 的 switch case
//     3) 手写 schema（容易漏 required/enum → Anthropic 上游 400）
//   - v5 把每个工具迁出到分类文件（prop_tools.go / wolf_tools.go），每个工具有
//     独立的 ToolSpec{Name,Phase,Category,Builder,MountIf,Dispatcher} 元数据
//     通过包级 init() 注册到 toolRegistry。
//   - BuildTools 改为遍历 registry 装配，dispatchToolInner 改为查派发表；
//     公开 API 签名不变（向后兼容 v3/v4 所有测试）。
//
// 线程安全：RegisterTool 在 init() 或单线程启动期调用，无需锁保护。
// MountTools/DispatchToolByName 用 RLock 保护；MountTools 可被多 goroutine 并发调用
// （race detector 安全）。
//
// 2026-07-21 道具系统 v5 重构。
package wwplayer

import (
	"LsmAgentGame/agent/wwtypes"
	"sync"
)

// ToolPhase 标识工具挂载的阶段。"any" 代表任何阶段都挂载。
type ToolPhase string

const (
	ToolPhaseAny   ToolPhase = "any"
	ToolPhaseSpeak ToolPhase = "speak"
	ToolPhaseNight ToolPhase = "night"
	ToolPhaseVote  ToolPhase = "vote"
)

// ToolSpec 描述一个 Agent 工具的完整契约。注册后由 BuildTools/DispatchTool
// 自动装配（无需手动接入 add* 函数 / switch case）。
//
// 字段：
//   - Name            工具名（Anthropic wire "name" 字段，必须全局唯一）
//   - Description     工具描述（Anthropic wire "description" 字段，≤ 几千字）。
//                     与 BuildDescription 二选一;BuildDescription 优先。
//   - BuildDescription 可选:返回动态描述(如 use_prop 按 wwtypes.PropSnapshot 拼每个
//                     道具的价格/中招率)。mountFromRegistry 优先用它。
//   - Phase           单一挂载阶段（向后兼容旧注册;新工具推荐用 Phases）
//   - Phases          可选:多阶段挂载列表（§20260810-04 U1,K3-F1 修复）。
//                     非空时优先于 Phase：phase ∈ Phases 才挂载。用于 wolf_whisper
//                     这种需在 speak + night_wolves 两阶段都挂载的工具。
//                     Phase 与 Phases 同时设置时,以 Phases 为准。
//   - Category        分类（"prop" / "wolf" / "core" / "judge" 等，用于诊断/统计）
//   - Builder         返回 Anthropic input_schema map（不含 name/description 外壳）
//   - MountIf         可选：额外的 wwtypes.GameContext 谓词。返回 false 时 BuildTools 不挂载。
//                     nil 等价于恒 true。
//   - Dispatcher      派发器。BuildTools 不挂载时不会被调用。
type ToolSpec struct {
	Name              string
	Description       string
	BuildDescription  func(gc *wwtypes.GameContext) string
	Phase             ToolPhase
	Phases            []ToolPhase // §20260810-04 U1 — 可选多阶段挂载
	Category          string
	Builder           func(gc *wwtypes.GameContext) map[string]any
	MountIf           func(gc *wwtypes.GameContext) bool
	Dispatcher        func(args map[string]any, gc *wwtypes.GameContext, runner ToolRunner) (string, error)
}

// specMatchesPhase 返回工具 t 是否应在 phase 挂载(§20260810-04 U1 单点判定,
// 避免 §135 式复制判定)。优先级:Phases(非空时)> Phase > ToolPhaseAny。
func specMatchesPhase(t *ToolSpec, phase ToolPhase) bool {
	if len(t.Phases) > 0 {
		for _, p := range t.Phases {
			if p == phase || p == ToolPhaseAny {
				return true
			}
		}
		return false
	}
	return t.Phase == phase || t.Phase == ToolPhaseAny
}

// toolRegistry 全局注册表。包级变量，init() 阶段写入；运行时只读。
var (
	toolRegistryMu sync.RWMutex
	toolRegistry   []*ToolSpec
)

// RegisterTool 注册一个工具。重复注册覆盖（便于测试桩）。
// 推荐在分类文件的 init() 里调用。
func RegisterTool(t *ToolSpec) {
	if t == nil || t.Name == "" || t.Builder == nil || t.Dispatcher == nil {
		// 静默忽略非法注册 — 测试桩可能临时构造空 spec。
		return
	}
	toolRegistryMu.Lock()
	defer toolRegistryMu.Unlock()
	// 覆盖现有同名条目（按 Name 去重）。
	for i, existing := range toolRegistry {
		if existing.Name == t.Name {
			toolRegistry[i] = t
			return
		}
	}
	toolRegistry = append(toolRegistry, t)
}

// UnregisterAll 仅供测试使用 — 清空注册表（不会触发 hook）。
func UnregisterAll() {
	toolRegistryMu.Lock()
	defer toolRegistryMu.Unlock()
	toolRegistry = nil
}

// MountTools 按 phase 与 MountIf 谓词从 registry 过滤出当前轮可挂载的工具。
// gc 可为 nil：nil 时忽略所有依赖 gc 的 MountIf 谓词（仅按 Phase 过滤）。
//
// 返回的 []*ToolSpec 不可被调用方修改（共享底层 registry 元素指针）。
func MountTools(phase ToolPhase, gc *wwtypes.GameContext) []*ToolSpec {
	toolRegistryMu.RLock()
	defer toolRegistryMu.RUnlock()
	out := make([]*ToolSpec, 0, len(toolRegistry))
	for _, t := range toolRegistry {
		// §20260810-04 U1 — 多阶段挂载统一走 specMatchesPhase(单点判定)。
		if !specMatchesPhase(t, phase) {
			continue
		}
		if t.MountIf != nil && gc != nil && !t.MountIf(gc) {
			continue
		}
		out = append(out, t)
	}
	return out
}

// DispatchToolByName 按工具名查找派发器。
// 未注册返回 (nil, false)；调用方走 fallback（switch default）。
func DispatchToolByName(name string) (func(args map[string]any, gc *wwtypes.GameContext, runner ToolRunner) (string, error), bool) {
	toolRegistryMu.RLock()
	defer toolRegistryMu.RUnlock()
	for _, t := range toolRegistry {
		if t.Name == name {
			return t.Dispatcher, true
		}
	}
	return nil, false
}

// FindTool 按名字查找完整 ToolSpec（测试用）。
func FindTool(name string) *ToolSpec {
	toolRegistryMu.RLock()
	defer toolRegistryMu.RUnlock()
	for _, t := range toolRegistry {
		if t.Name == name {
			return t
		}
	}
	return nil
}

// mountFromRegistry 从 registry 中按 phase 与 MountIf 谓词过滤出可挂载的工具，
// 逐个调 Builder 后通过 add 闭包写入 BuildTools 的工具列表。
// 是 v5 下 `add(name, desc, schema)` 装配的标准入口,取代 v4 的逐个 add*
// 串联调用。未注册任何工具时是 no-op。
//
// 约束:
//   - 仅挂载 phase 匹配(或 ToolPhaseAny)且 MountIf 返回 true 的工具。
//   - Builder() 返回 nil 视为 schema 异常,跳过本工具(不阻塞其它工具)。
//   - Description 为空时退化为工具名(至少让 Anthropic 不出现空 desc)。
//
// 调用方:BuildTools(phase) 内按实际需求传入 phase 常量。
func mountFromRegistry(add func(name, desc string, schema map[string]any), phase ToolPhase, gc *wwtypes.GameContext) {
	toolRegistryMu.RLock()
	defer toolRegistryMu.RUnlock()
	for _, t := range toolRegistry {
		// §20260810-04 U1 — 多阶段挂载统一走 specMatchesPhase(单点判定)。
		if !specMatchesPhase(t, phase) {
			continue
		}
		if t.MountIf != nil && gc != nil && !t.MountIf(gc) {
			continue
		}
		if t.Builder == nil {
			continue
		}
		schema := t.Builder(gc)
		if schema == nil {
			continue
		}
		desc := t.Description
		if t.BuildDescription != nil {
			if dyn := t.BuildDescription(gc); dyn != "" {
				desc = dyn
			}
		}
		if desc == "" {
			desc = t.Name
		}
		add(t.Name, desc, schema)
	}
}

// AllRegistered 返回注册表当前所有 spec（测试用，仅用于审计/统计）。
func AllRegistered() []*ToolSpec {
	toolRegistryMu.RLock()
	defer toolRegistryMu.RUnlock()
	out := make([]*ToolSpec, len(toolRegistry))
	copy(out, toolRegistry)
	return out
}
