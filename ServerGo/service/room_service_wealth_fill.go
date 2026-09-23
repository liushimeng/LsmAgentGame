// Package service — room_service_wealth_fill.go: 虚拟城市深度层座位合成
// (2026-09-22 §17-CityHuman 全民驱动,契约 03 §3.2)。
//
// 背景:「焦点居民数 1–12 档」建房逻辑随精选卡层整体退役。虚拟城市建房时
// 服务端**固定合成 12 名池驱动深度居民座位**(ModelKey="" = LLM 线路池
// 分配),引擎内部深度轨迹层;传入的 agent_seats 一律忽略(城市是全 Agent
// 城市模拟器,不存在人类可选的 bot 档位)。werewolf 等其他游戏路径零变化。
//
// 合成发生在 DB 落库之前(CreateRoomWithAgents 校验段之前替换 agentSeats),
// 使 bot 用户行、FullAgentMode 判定、ws 层 RegisterBotSeats、自动开局看到
// 同一座位集;ws 层 registerWealthAgentSeats 另有防御性补填(roomSvc 可用时)。
package service

// wealthDeepSeatCount wealth 房间深度层座位数(与 game/wealth.MaxSeats=12
// 对齐;service 层不 import wealth 包,与 maxAgentSeats=12 同值,独立常量
// 防漂移)。2026-09-22 §17-CityHuman(契约 03 §3.2):原 wealthMinAgentSeats
// (10)/wealthMaxAgentSeats 语义收敛为本常量 —— 深度层固定 12。
const wealthDeepSeatCount = 12

// wealthDeepSeats 服务端合成 12 名池驱动深度居民座位
// ({seat: 0..11, model_key: ""};契约 03 §3.2)。ModelKey 空串 = LLM 线路池
// 驱动,不绑定具体模型;Role 空 = 不注入角色偏好。
func wealthDeepSeats() []AgentSeatConfig {
	seats := make([]AgentSeatConfig, 0, wealthDeepSeatCount)
	for seat := 0; seat < wealthDeepSeatCount; seat++ {
		seats = append(seats, AgentSeatConfig{Seat: seat, ModelKey: ""})
	}
	return seats
}
