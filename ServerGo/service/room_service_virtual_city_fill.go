// Package service — room_service_wealth_fill.go: 虚拟城市抽样展示层居民接线
// (2026-09-22 §17-CityHuman 全民驱动,契约 03 §3.2;2026-09-24 重构)。
//
// 背景:「焦点居民数 1–12 档（已退役）」建房逻辑随精选卡层整体退役。虚拟城市建房时
// 服务端为 UI 展示合成 12 名抽样展示居民(ModelKey="" = LLM 线路池分配),
// 12 名展示居民 = 当月从 Backdrop 中 cursor 随机均匀抽样的代表,
// 无固定/特权/抽样层;传入的 agent_seats 一律忽略。
//
// 合成发生在 DB 落库之前(CreateRoomWithAgents 校验段之前替换 agentSeats),
// 使 bot 用户行、FullAgentMode 判定、ws 层 RegisterBotSeats、自动开局看到
// 同一座位集;ws 层 registerVirtualCityAgentSeats 另有防御性补填(roomSvc 可用时)。
package service

// virtualCityDeepSeatCount virtual_city 房间 UI 展示位数
// (= game/virtual_city.MaxSeats=12;service 层不 import wealth 包,
// 独立常量防漂移)。2026-09-24 重构:12 = 当月从 Backdrop 居民中 cursor
// 随机均匀抽样的展示代表数,无抽样层概念。
const wealthDeepSeatCount = 12

// wealthDeepSeats 服务端合成 12 名池驱动抽样展示居民
// ({seat: 0..11, model_key: ""};契约 03 §3.2;2026-09-24 重构)。
// ModelKey 空串 = LLM 线路池驱动,不绑定具体模型;Role 空 = 不注入角色偏好。
func wealthDeepSeats() []AgentSeatConfig {
	seats := make([]AgentSeatConfig, 0, wealthDeepSeatCount)
	for seat := 0; seat < wealthDeepSeatCount; seat++ {
		seats = append(seats, AgentSeatConfig{Seat: seat, ModelKey: ""})
	}
	return seats
}
