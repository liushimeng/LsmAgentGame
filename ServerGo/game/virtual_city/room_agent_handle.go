// Package virtual_city — room_agent_handle.go: 批次85 §3.3.1 座位 Agent 只读
// 句柄访问器(装配层共享 LLM 令牌桶与调用钩子)。
//
// 契约: lag_docs/虚拟城市/已实现/85-全维度状态视图与物体级显示控制/01-方案设计.md §3.3.1。
//
// 消费方: ServerGo/main.go 的 cityHumanAnswerer(ask_agent → City-Human 对话
// 问答)—— 优先取游戏内实例发问,使对话消耗与月度决策**共用令牌桶与
// BindLLMCallHook**,计入房间 LLM 统计(§1.3:批次84 缓存实例与游戏内实例
// 各持一套桶,双份口径)。
//
// 纪律:
//   - **只读**:仅取既有句柄 —— 不新建 / 不启动 / 不停止 Agent、不改
//     `r.agents` 生命周期、不改任何游戏状态;调用方**不得**用返回的句柄改
//     游戏状态(只可调用问答/观测类方法;`vcplayer.Agent` 的方法自带锁)。
//   - **禁止回调房间**(§92a):本方法只持 `r.mu` 读 `r.agents`,不做任何
//     二次取锁的回调(不调 GetStatus()/Engine() 等);返回后调用方在锁外
//     使用句柄是安全的。
//   - **nil 安全**:nil 房间 / 房间未启动(World == nil)/ 越界座位 /
//     非居民(人类座位、空位、Agent 尚未装配)一律返回 nil,不 panic。
//   - 零新增依赖方向:`vcplayer` 已是本包既有依赖(room.go 已 import)。
package virtual_city

import "LsmAgentGame/agent/vcplayer"

// AgentForSeat 返回座位上的 City-Human Agent(未启动/越位/非居民返回 nil)。
//
// **只读句柄访问器**:仅取既有实例指针(两次调用同一座位返回**同一指针**,
// 不新建),不改任何游戏状态、不启动/停止 Agent、不持有引用计数。调用方
// **不得**用它改游戏状态 —— 供装配层共享既有 LLM 令牌桶与 BindLLMCallHook
// (批次85 §3.3:对话消耗与月度决策同桶同钩子,计入房间 LLM 统计)。
//
// 持 r.mu 只读(§92a:本方法不回调房间)。返回 nil 的边界(不 panic):
//   - 房间未启动(r.World == nil,含 hydrate 装配后未开局的窗口)—— 未开局
//     不放行句柄;
//   - seat 越界;
//   - 非居民/空位/Agent 尚未装配(r.agents 无该座位条目)。
func (r *VirtualCityRoom) AgentForSeat(seat int) *vcplayer.Agent {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.World == nil {
		return nil
	}
	if seat < 0 || seat >= MaxSeats {
		return nil
	}
	return r.agents[seat]
}
