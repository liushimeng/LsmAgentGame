// Package service — room_service_wealth_fill.go: 虚拟城市焦点居民自动填充
// (2026-09-22 §CityHuman重构,前端联调)。
//
// 背景:前端建房弹窗放开「焦点居民数」1–12 档(默认 12),但引擎
// MinSeats=10 会拦截 <10 座开局(35003)。城市房恒为全 Agent 模式
// (FullAgent=true),没有人类入座来凑齐座位 —— 1–9 档必须在建房时由
// 服务端自动填充池驱动居民(ModelKey="" = LLM 线路池分配)至 MinSeats,
// 否则房间永远停在 open。
//
// 填充发生在 DB 落库之前(CreateRoomWithAgents 校验段之后),使 bot 用户行、
// FullAgentMode 判定、ws 层 RegisterBotSeats、自动开局看到同一座位集;
// ws 层 registerWealthAgentSeats 另有防御性补填(roomSvc 可用时)。
package service

// wealthMaxAgentSeats wealth 房间座位上限(与 game/wealth.MaxSeats=12 对齐;
// service 层不 import wealth 包,与 maxAgentSeats=12 同值,独立常量防漂移)。
const wealthMaxAgentSeats = 12

// padWealthAgentSeats 把 wealth 房间的 agentSeats 填充到 wealthMinAgentSeats:
// 仅当显式请求了 agent_seats(0 < len < MinSeats)时,按座位号升序挑空闲座位,
// 以池驱动居民(ModelKey="")补齐;agentSeatSet 同步更新(调用方后续不再
// 用旧集合做判定 —— creatorShouldBeSpectator / 落库循环都消费填充后的切片)。
//
// 纯函数:不触 DB,不分配新身份;人类可入座房(agentSeats 为空)原样返回。
func padWealthAgentSeats(agentSeats []AgentSeatConfig, agentSeatSet map[int]struct{}) []AgentSeatConfig {
	if len(agentSeats) == 0 || len(agentSeats) >= wealthMinAgentSeats {
		return agentSeats
	}
	out := make([]AgentSeatConfig, len(agentSeats), wealthMinAgentSeats)
	copy(out, agentSeats)
	for seat := 0; seat < wealthMaxAgentSeats && len(out) < wealthMinAgentSeats; seat++ {
		if _, taken := agentSeatSet[seat]; taken {
			continue
		}
		// 池驱动:ModelKey="" 由 LLM 线路池在决策时分配线路(契约 04 §1.1)。
		out = append(out, AgentSeatConfig{Seat: seat, ModelKey: ""})
		agentSeatSet[seat] = struct{}{}
	}
	return out
}
