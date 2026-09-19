// Package ws — game_service_wealth_bot.go: 财商流游戏 Agent 座位注册
// (2026-09-14 §财商流P0-bugfix)。
//
// 背景: P0 骨架把 `RegisterAgentSeats` 的非 werewolf/texasholdem 分支直接
// return nil,导致 wealth 房间的 agent_seats 只落了 DB 行(bot 用户已建),
// in-memory WealthRoom 从未登记 bot 座位 —— Start 发卡跳过 bot、
// Manager.EnsureAgents 因 Seats[seat]=="" 跳过,bot 永远不上场。
// 本文件补齐该链路,对齐德扑 registerTexasHoldemAgentSeats 的顺序约束:
// 先解析全部 bot userID → 一次性 RegisterBotSeats(标记 + 入住)→
// 人类创建者随后的 SyncSeat/WS game.join 的 JoinGame 只会挑「非 bot 空位」。
package ws

import (
	"LsmAgentGame/errcode"
	"LsmAgentGame/game/wealth"
	"LsmAgentGame/logger"
	"LsmAgentGame/service"

	"go.uber.org/zap"
)

// registerWealthAgentSeats 把建房请求中的 agent_seats 注册进 wealth 内存房间。
// 幂等: RegisterBotSeats 只写空座位,重复调用(建房 + 重试)安全。
func (s *GameService) registerWealthAgentSeats(roomID string, seats []service.AgentSeatConfig) *errcode.Error {
	if s.wealthMgr == nil {
		return nil
	}
	r := s.wealthMgr.Get(roomID)
	if r == nil {
		r = s.wealthMgr.CreateRoom(roomID)
	}
	seatUsers := make(map[int]string, len(seats))
	seatModels := make(map[int]string, len(seats))
	for _, seatCfg := range seats {
		if seatCfg.Seat < 0 || seatCfg.Seat >= wealth.MaxSeats {
			logger.L().Warn("registerWealthAgentSeats: seat out of range",
				zap.String("room_id", roomID),
				zap.Int("seat", seatCfg.Seat))
			continue
		}
		botUserID, err := s.botUserIDForSeat(roomID, seatCfg.Seat)
		if err != nil {
			logger.L().Warn("registerWealthAgentSeats: resolve bot user failed",
				zap.String("roomID", roomID),
				zap.Int("seat", seatCfg.Seat),
				zap.Error(err))
			continue
		}
		seatUsers[seatCfg.Seat] = botUserID
		seatModels[seatCfg.Seat] = seatCfg.ModelKey
	}
	if len(seatUsers) == 0 {
		return nil
	}
	r.RegisterBotSeats(seatUsers, seatModels, nil)
	logger.L().Info("wealth bot seats registered",
		zap.String("room_id", roomID),
		zap.Int("bot_seats", len(seatUsers)))

	// 2026-09-16 §12 座扩容:全 Agent 房(创建者降级为观战者)在
	// CreateRoomWithAgents 里跳过 SyncSeat,此处必须兜底自动开局 —— 否则
	// 10-11 bot 房注册完 bot 后永远停在 open。满 MinSeats(10) 即开,与
	// JoinGame 的 full 语义一致;若人类创建者随后 SyncSeat 会再触发一次
	// startWealthRoom,但 r.Start 在 Status!=Open 时幂等返回错误(仅日志),不重复开局。
	if r.GetStatus() == wealth.StatusOpen && r.Occupied() >= wealth.MinSeats {
		if e := s.startWealthRoom(roomID); e != nil {
			logger.L().Warn("registerWealthAgentSeats: auto-start failed",
				zap.String("room_id", roomID), zap.Error(e))
		}
	}
	// 2026-09-19 §全Agent模式: bot 数量 >= MinSeats 时自动置位
	if r.Occupied() >= wealth.MinSeats {
		r.SetFullAgentMode(true)
	}
	return nil
}
