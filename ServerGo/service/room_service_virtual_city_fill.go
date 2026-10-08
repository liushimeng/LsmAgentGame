// Package service — room_service_wealth_fill.go: 虚拟城市居民座位接线
// (2026-09-22 §17-CityHuman 全民驱动,契约 03 §3.2;2026-09-24 重构;
// 2026-09-26 §批次25 居民-Agent 统一)。
//
// 背景:「焦点居民数 1–12 档(已退役)」建房逻辑随精选卡层整体退役。虚拟城市建房时
// 服务端合成池驱动居民座位(ModelKey="" = LLM 线路池分配);
// 传入的 agent_seats 一律忽略。
//
// 批次 25(25-居民Agent统一与LLM调用节流 §3.2):座位数与背景居民规模统一 ——
// resident_count=N ⇒ 常驻 City-Human Agent 数 = clamp(N,1,12);
// N>12 时 12 个座位 Agent 代表全城,其余居民由驱动层按月抽样。
// 2026-10-08 §背景居民下限降至1:座位数下限由 10 降到 1 —— 小于 10 居民的城市
// 「一个居民一个常驻座位 Agent」,开局门槛同步由房间级 minSeats(=N)兜住。
//
// 合成发生在 DB 落库之前(CreateRoomWithAgents 校验段之前替换 agentSeats),
// 使 bot 用户行、FullAgentMode 判定、ws 层 RegisterBotSeats、自动开局看到
// 同一座位集;ws 层 registerVirtualCityAgentSeats 另有防御性补填(roomSvc 可用时)。
package service

// 虚拟城市常驻座位档位(= game/virtual_city.MaxSeats 上限 + 居民数下限 1;
// service 层不 import wealth 包,独立常量防漂移)。2026-09-26 §批次25 +
// 2026-10-08 §背景居民下限降至1:座位数 = clamp(resident_count, 1, 12),
// 不再是恒 12;下限 1 与前端 Stepper 的 RESIDENT_MIN 对齐。
const (
	wealthDeepMinSeatCount = 1
	wealthDeepSeatCount    = 12
)

// wealthDeepSeats 服务端合成池驱动居民座位({seat: 0..s-1, model_key: ""})。
// 批次 25(25 文档 §3.2)+ 2026-10-08:s = clamp(residentCount, 1, MaxSeats=12);
// residentCount<=0 时保持 12(兼容未提供居民规模的旧路径/内部调用)。
// ModelKey 空串 = LLM 线路池驱动,不绑定具体模型;Role 空 = 不注入角色偏好。
func wealthDeepSeats(residentCount int) []AgentSeatConfig {
	n := residentCount
	if n <= 0 {
		n = wealthDeepSeatCount
	}
	if n < wealthDeepMinSeatCount {
		n = wealthDeepMinSeatCount
	}
	if n > wealthDeepSeatCount {
		n = wealthDeepSeatCount
	}
	seats := make([]AgentSeatConfig, 0, n)
	for seat := 0; seat < n; seat++ {
		seats = append(seats, AgentSeatConfig{Seat: seat, ModelKey: ""})
	}
	return seats
}
