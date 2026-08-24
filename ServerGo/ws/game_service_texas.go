package ws

import (
	"encoding/json"

	"LsmAgentGame/game/texasholdem"
	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

func (s *GameService) handleTexasHoldemJoin(c *Client, env Envelope, roomID string) {
	room, started, e := s.texasHoldemMgr.JoinGame(roomID, c.UserID)
	if e != nil {
		s.sendError(c, env.Seq, e.Code, e.Message)
		return
	}
	s.hub.SubscribeRoom(roomID, c)

	seat, _ := room.SeatOf(c.UserID)
	phase := "waiting"
	ready := room.IsReady()
	if room.State != nil {
		phase = room.State.Street.String()
	}

	s.sendOK(c, env.Seq, "game.joined", map[string]any{
		"room_id":   roomID,
		"game_kind": "texasholdem",
		"my_seat":   seat,
		"phase":     phase,
		"ready":     ready,
	})

	if started {
		s.hub.BroadcastRoom(roomID, Envelope{
			Type: "game.started",
			Payload: mustMarshal(map[string]any{
				"room_id":     roomID,
				"game_kind":   "texasholdem",
				"phase":       "preflop",
				"ready":       true,
				"hand_number": room.State.HandNumber,
			}),
		})
		s.broadcastTexasHoldemState(roomID)
		logger.L().Info("texasholdem game started via ws", zap.String("room_id", roomID))
		// 2026-08-19 §德州扑克Agent: 游戏开始后检查是否 bot 先行动
		// 2026-08-20 §B6: 异步 + per-room 串行守卫(LLM 调用最坏 30s×6,不得阻塞 WS handler)
		go s.ProcessBotTurn(roomID)
	} else {
		s.hub.BroadcastRoom(roomID, Envelope{
			Type: "game.peer_joined",
			Payload: mustMarshal(map[string]any{
				"room_id":      roomID,
				"game_kind":    "texasholdem",
				"ready":        ready,
				"player_count": room.Occupied(),
			}),
		})
		// P0-3: 游戏已开始但玩家迟到(或 WS 重连)时,单独给该玩家推一份完整状态,
		// 否则前端 my_hole 为 [{rank:0,suit:0},{rank:0,suit:0}]。
		// 仅当游戏已开局(phase != waiting)且该玩家有座位时才推送。
		if room.State != nil && room.State.Street != texasholdem.PhaseWaiting && seat >= 0 {
			if st := s.texasHoldemMgr.StateForSeat(roomID, seat); st != nil {
				s.hub.BroadcastTo(c.UserID, Envelope{Type: "game.state", Payload: mustMarshal(st)})
			}
		}
	}
}

func (s *GameService) handleTexasHoldemAction(c *Client, env Envelope) {
	var req struct {
		RoomID string `json:"room_id"`
		Type   string `json:"type"`   // fold/check/call/bet/raise/allin
		Amount int    `json:"amount"` // 用于 bet/raise
	}
	if err := json.Unmarshal(env.Payload, &req); err != nil || req.RoomID == "" {
		s.sendError(c, env.Seq, 20001, "invalid game.action payload")
		return
	}
	if s.rejectIfSpectator(c, env, req.RoomID) {
		return
	}

	// 映射字符串到 ActionType
	var actionType texasholdem.ActionType
	switch req.Type {
	case "fold":
		actionType = texasholdem.ActFold
	case "check":
		actionType = texasholdem.ActCheck
	case "call":
		actionType = texasholdem.ActCall
	case "bet":
		actionType = texasholdem.ActBet
	case "raise":
		actionType = texasholdem.ActRaise
	case "allin":
		actionType = texasholdem.ActAllIn
	default:
		s.sendError(c, env.Seq, 20001, "unknown action type: "+req.Type)
		return
	}

	action := texasholdem.Action{Type: actionType, Amount: req.Amount}
	room, handOver, e := s.texasHoldemMgr.Action(req.RoomID, c.UserID, action)
	if e != nil {
		s.sendError(c, env.Seq, e.Code, e.Message)
		return
	}

	// 2026-08-20 §B2: 人类动作应用成功 → 所有 bot 的 Memory 记录行动者统计。
	// 取行动者座位与 street(动作后 street 可能已推进,取快照以「动作发生时的阶段」为准
	// 代价高;此处直接用房间当前 street,统计口径为「动作入账时的阶段」)。
	if s.thpDriver != nil {
		seat, seated := room.SeatOf(c.UserID)
		street := ""
		if room.State != nil {
			street = room.State.Street.String()
		}
		if seated {
			s.thpDriver.RecordPlayerAction(req.RoomID, seat, c.UserID, req.Type, req.Amount, street)
		}
	}

	s.hub.BroadcastRoom(req.RoomID, Envelope{
		Type: "game.action_accepted",
		Payload: mustMarshal(map[string]any{
			"room_id":   req.RoomID,
			"game_kind": "texasholdem",
			"by":        c.UserID,
			"type":      req.Type,
			"amount":    req.Amount,
		}),
	})
	s.broadcastTexasHoldemState(req.RoomID)
	if handOver {
		s.broadcastTexasHoldemOver(req.RoomID, room)
		s.broadcastTexasHoldemState(req.RoomID)
	}
	// 2026-08-19 §德州扑克Agent: 人类行动后检查是否轮到 bot
	// 2026-08-20 §B6: 异步 + per-room 串行守卫(LLM 调用最坏 30s×6,不得阻塞 WS handler)
	go s.ProcessBotTurn(req.RoomID)
}

func (s *GameService) sendTexasHoldemState(c *Client, seq int64, state *texasholdem.ClientGameState) {
	c.send <- Envelope{Type: "game.state", Seq: seq, Payload: mustMarshal(state)}
}

// HandleTexasPlayerRemoved 断线超时(15s)被踢出的人类玩家的德州扑克收尾
// (2026-08-24 BUG-TEXAS-DISCARD-STALL,R17 P1)。
//
// 由 main.go 的 hub.SetLeaveRoomFunc 包装器在 RoomService.LeaveRoom 之前调用。
// 此前断线移除路径只清 DB 行,in-memory 对局的回合仍停在已离线的人类座位上
// —— bot 回合 watchdog 只管 bot 座位,手牌因此永久停滞(实测 ≥10min)。
// 这里强制 fold 并摘除该座位,随后广播最新状态并链式驱动 bot 继续行动。
// 非德扑房间 / 该用户不在座 / bot 座位时 ForceRemovePlayer 返回 removed=false,
// 本函数 no-op。
func (s *GameService) HandleTexasPlayerRemoved(roomID, userID string) {
	if s.texasHoldemMgr == nil {
		return
	}
	removed, handOver, handNum, delta := s.texasHoldemMgr.ForceRemovePlayer(roomID, userID)
	if !removed {
		return
	}
	// 被摘除的玩家已不在 SettleHandCoins 快照内,单独结算其本手净输赢,
	// 保持「赢家 credit ↔ 输家 debit」守恒。
	if delta != 0 {
		s.texasHoldemMgr.SettlePlayerDelta(roomID, userID, handNum, delta)
	}
	s.broadcastTexasHoldemState(roomID)
	if handOver {
		s.broadcastTexasHoldemOver(roomID, nil)
	}
	// 回合可能落在 bot 座位上 —— 链式驱动(§B6: 异步 + 串行守卫)。
	go s.ProcessBotTurn(roomID)
}

func (s *GameService) broadcastTexasHoldemState(roomID string) {
	seats, ok := s.texasHoldemMgr.Seats(roomID)
	if !ok {
		return
	}
	for seat := 0; seat < texasholdem.MaxPlayers; seat++ {
		userID := seats[seat]
		if userID == "" {
			continue
		}
		state := s.texasHoldemMgr.StateForSeat(roomID, seat)
		if state == nil {
			continue
		}
		// 2026-08-23 §3.5: 附带最近 5 条公屏聊天预览(500K 队列 Tail)。
		state.ChatWindowPreview = s.TexasChatPreview(roomID)
		s.hub.BroadcastTo(userID, Envelope{Type: "game.state", Payload: mustMarshal(state)})
	}
	// 同时向观察者推送他们可见的状态（所有玩家 Hole 隐藏）。
	s.broadcastTexasHoldemSpectatorState(roomID)
}

func (s *GameService) broadcastTexasHoldemOver(roomID string, room *texasholdem.TexasHoldemRoom) {
	if room != nil && room.State != nil {
		s.hub.BroadcastRoom(roomID, Envelope{
			Type: "game.over",
			Payload: mustMarshal(map[string]any{
				"room_id":     roomID,
				"game_kind":   "texasholdem",
				"winners":     room.State.Winners,
				"reason":      "hand_end",
				"status":      room.State.Status.String(),
				"hand_number": room.State.HandNumber,
			}),
		})
		return
	}
	// 2026-08-20 §B6/B8 修复:bot 路径 / watchdog 强制 fold 只有 roomID,
	// 旧实现 room==nil 直接 return → game.over 从未下发(前端不知手牌结束)。
	// 现在回退到 manager 的锁内快照 OverSummary。
	winners, status, handNumber, ok := s.texasHoldemMgr.OverSummary(roomID)
	if !ok {
		return
	}
	s.hub.BroadcastRoom(roomID, Envelope{
		Type: "game.over",
		Payload: mustMarshal(map[string]any{
			"room_id":     roomID,
			"game_kind":   "texasholdem",
			"winners":     winners,
			"reason":      "hand_end",
			"status":      status,
			"hand_number": handNumber,
		}),
	})
}

func (s *GameService) broadcastTexasHoldemSpectatorState(roomID string) {
	uids := s.hub.connectedSpectatorUserIDs(roomID)
	state := s.texasHoldemMgr.SpectatorView(roomID)
	if state == nil {
		return
	}
	// 2026-08-23 §3.5: 观战者同样可见公屏聊天预览。
	state.ChatWindowPreview = s.TexasChatPreview(roomID)
	for _, uid := range uids {
		s.hub.BroadcastTo(uid, Envelope{Type: "game.state", Payload: mustMarshal(state)})
	}
}


