// Package ws — game_service_wealth_bot.go: 虚拟城市 Agent 座位注册
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
		// 2026-09-21 §虚拟城市(契约 04 §1.4):空 model_key = 线路池驱动座位,
		// 照常建 bot 用户/注册座位;SeatModelKeys 存空串(下方 RegisterBotSeats
		// 对空串跳过显式 key 写入,昵称走 AI·居民<seat>号 → Start 抽卡升级)。
		// 全 Agent 模式判定(len(seatUsers) ≥ MinSeats)不受空 key 影响。
		seatModels[seatCfg.Seat] = seatCfg.ModelKey
	}
	// 2026-09-22 §17-CityHuman(契约 03 §3.2): 深度层固定 12 —— 正常路径下
	// service 层已在落库前合成 wealthDeepSeats()(12 池驱动座位);若注册进来
	// 的座位仍不足 MinSeats(例如旧链路/重启恢复绕过了 service 层),用池驱动
	// bot(ModelKey="")防御性补填空闲座位至 MinSeats,幂等无害。roomSvc 不可用
	// (纯内存测试夹具)时跳过补填,不 panic。
	if len(seatUsers) > 0 && len(seatUsers) < wealth.MinSeats && s.roomSvc != nil {
		for seat := 0; seat < wealth.MaxSeats && len(seatUsers) < wealth.MinSeats; seat++ {
			if _, taken := seatUsers[seat]; taken {
				continue
			}
			botUserID, err := s.botUserIDForSeat(roomID, seat)
			if err != nil {
				continue // 无 DB 行(非全 Agent 路径):跳过该座位
			}
			seatUsers[seat] = botUserID
			seatModels[seat] = ""
		}
	}
	if len(seatUsers) == 0 {
		return nil
	}
	// 必须在 RegisterBotSeats / 自动开局前置位。否则全 Agent 房会在
	// FullAgentMode=false 的窗口内自动开局,创建者或并发人类仍可能尝试入座。
	if len(seatUsers) >= wealth.MinSeats {
		r.SetFullAgentMode(true)
	}
	r.RegisterBotSeats(seatUsers, seatModels)
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
	return nil
}
