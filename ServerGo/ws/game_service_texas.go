package ws

import (
	"encoding/json"

	"LsmWebGame/game/texasholdem"
	"LsmWebGame/logger"

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
}

func (s *GameService) sendTexasHoldemState(c *Client, seq int64, state *texasholdem.ClientGameState) {
	c.send <- Envelope{Type: "game.state", Seq: seq, Payload: mustMarshal(state)}
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
		s.hub.BroadcastTo(userID, Envelope{Type: "game.state", Payload: mustMarshal(state)})
	}
	// 同时向观察者推送他们可见的状态（所有玩家 Hole 隐藏）。
	s.broadcastTexasHoldemSpectatorState(roomID)
}

func (s *GameService) broadcastTexasHoldemOver(roomID string, room *texasholdem.TexasHoldemRoom) {
	if room == nil || room.State == nil {
		return
	}
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
}

func (s *GameService) broadcastTexasHoldemSpectatorState(roomID string) {
	uids := s.hub.connectedSpectatorUserIDs(roomID)
	state := s.texasHoldemMgr.SpectatorView(roomID)
	if state == nil {
		return
	}
	for _, uid := range uids {
		s.hub.BroadcastTo(uid, Envelope{Type: "game.state", Payload: mustMarshal(state)})
	}
}


