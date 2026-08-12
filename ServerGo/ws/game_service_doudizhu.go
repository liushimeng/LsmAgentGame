package ws

import (
	"encoding/json"

	"LsmAgentGame/game/doudizhu"
	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

func (s *GameService) handleDoudizhuJoin(c *Client, env Envelope, roomID string) {
	room, started, e := s.doudizhuMgr.JoinGame(roomID, c.UserID)
	if e != nil {
		s.sendError(c, env.Seq, e.Code, e.Message)
		return
	}
	s.hub.SubscribeRoom(roomID, c)

	seat, _ := room.SeatOf(c.UserID)
	phase := PhaseBiddingStr
	ready := room.IsReady()
	if room.State != nil {
		phase = room.State.Phase.String()
	}

	s.sendOK(c, env.Seq, "game.joined", map[string]any{
		"room_id":   roomID,
		"game_kind": "doudizhu",
		"my_seat":   seat,
		"phase":     phase,
		"ready":     ready,
	})

	if started {
		// 满员发牌：向每个座位推送各自可见状态（手牌不同）。
		s.hub.BroadcastRoom(roomID, Envelope{
			Type: "game.started",
			Payload: mustMarshal(map[string]any{
				"room_id":   roomID,
				"game_kind": "doudizhu",
				"phase":     "bidding",
				"ready":     true,
			}),
		})
		s.broadcastDoudizhuState(roomID)
		logger.L().Info("doudizhu game started via ws", zap.String("room_id", roomID))
	} else {
		s.hub.BroadcastRoom(roomID, Envelope{
			Type: "game.peer_joined",
			Payload: mustMarshal(map[string]any{
				"room_id":      roomID,
				"game_kind":    "doudizhu",
				"ready":        ready,
				"player_count": room.Occupied(),
			}),
		})
	}
}

func (s *GameService) handleDoudizhuBid(c *Client, env Envelope) {
	var req struct {
		RoomID string `json:"room_id"`
		Score  int    `json:"score"`
	}
	if err := json.Unmarshal(env.Payload, &req); err != nil || req.RoomID == "" {
		s.sendError(c, env.Seq, 20001, "invalid game.bid payload")
		return
	}
	if s.rejectIfSpectator(c, env, req.RoomID) {
		return
	}
	room, redeal, e := s.doudizhuMgr.Bid(req.RoomID, c.UserID, req.Score)
	if e != nil {
		s.sendError(c, env.Seq, e.Code, e.Message)
		return
	}
	s.sendOK(c, env.Seq, "game.bidded", map[string]any{
		"room_id":   req.RoomID,
		"game_kind": "doudizhu",
	})
	if redeal {
		s.hub.BroadcastRoom(req.RoomID, Envelope{
			Type: "game.redealt",
			Payload: mustMarshal(map[string]any{
				"room_id":   req.RoomID,
				"game_kind": "doudizhu",
				"phase":     "bidding",
			}),
		})
	} else if room.State != nil && room.State.Phase == doudizhu.PhasePlaying {
		s.hub.BroadcastRoom(req.RoomID, Envelope{
			Type: "game.started",
			Payload: mustMarshal(map[string]any{
				"room_id":   req.RoomID,
				"game_kind": "doudizhu",
				"phase":     "playing",
				"landlord":  room.State.LandlordSeat,
			}),
		})
	}
	s.broadcastDoudizhuState(req.RoomID)
}

func (s *GameService) handleDoudizhuPlay(c *Client, env Envelope) {
	var req struct {
		RoomID string          `json:"room_id"`
		Cards  []doudizhu.Card `json:"cards"`
	}
	if err := json.Unmarshal(env.Payload, &req); err != nil || req.RoomID == "" {
		s.sendError(c, env.Seq, 20001, "invalid game.play payload")
		return
	}
	if s.rejectIfSpectator(c, env, req.RoomID) {
		return
	}
	room, gameOver, e := s.doudizhuMgr.Play(req.RoomID, c.UserID, req.Cards)
	if e != nil {
		s.sendError(c, env.Seq, e.Code, e.Message)
		return
	}
	s.hub.BroadcastRoom(req.RoomID, Envelope{
		Type: "game.played",
		Payload: mustMarshal(map[string]any{
			"room_id":   req.RoomID,
			"game_kind": "doudizhu",
		}),
	})
	s.broadcastDoudizhuState(req.RoomID)
	if gameOver {
		s.broadcastDoudizhuOver(req.RoomID, room)
		s.broadcastDoudizhuState(req.RoomID)
	}
}

func (s *GameService) handleDoudizhuPass(c *Client, env Envelope) {
	var req struct {
		RoomID string `json:"room_id"`
	}
	if err := json.Unmarshal(env.Payload, &req); err != nil || req.RoomID == "" {
		s.sendError(c, env.Seq, 20001, "invalid game.pass payload")
		return
	}
	if s.rejectIfSpectator(c, env, req.RoomID) {
		return
	}
	if _, e := s.doudizhuMgr.Pass(req.RoomID, c.UserID); e != nil {
		s.sendError(c, env.Seq, e.Code, e.Message)
		return
	}
	s.hub.BroadcastRoom(req.RoomID, Envelope{
		Type: "game.passed",
		Payload: mustMarshal(map[string]any{
			"room_id":   req.RoomID,
			"game_kind": "doudizhu",
		}),
	})
	s.broadcastDoudizhuState(req.RoomID)
}

const PhaseBiddingStr = "bidding"

func (s *GameService) sendDoudizhuState(c *Client, seq int64, state *doudizhu.ClientGameState) {
	c.send <- Envelope{Type: "game.state", Seq: seq, Payload: mustMarshal(state)}
}

func (s *GameService) broadcastDoudizhuState(roomID string) {
	seats, ok := s.doudizhuMgr.Seats(roomID)
	if !ok {
		return
	}
	for seat := 0; seat < 3; seat++ {
		userID := seats[seat]
		if userID == "" {
			continue
		}
		state := s.doudizhuMgr.StateForSeat(roomID, seat)
		if state == nil {
			continue
		}
		// Payload 需要加 game_kind，BuildClientState 没有这个字段。
		payload := map[string]any{
			"room_id":       state.RoomID,
			"game_kind":     "doudizhu",
			"seats":         state.Seats,
			"my_seat":       state.MySeat,
			"ready":         state.Ready,
			"phase":         state.Phase,
			"turn":          state.Turn,
			"status":        state.Status,
			"my_hand":       state.MyHand,
			"hand_counts":   state.HandCounts,
			"first_bidder":  state.FirstBidder,
			"bids":          state.Bids,
			"current_bid":   state.CurrentBid,
			"landlord_seat": state.LandlordSeat,
			"bottom":        state.Bottom,
			"last_play":     state.LastPlay,
			"multiplier":    state.Multiplier,
			"bomb_count":    state.BombCount,
			"winner":        state.Winner,
			"score":         state.Score,
		}
		s.hub.BroadcastTo(userID, Envelope{Type: "game.state", Payload: mustMarshal(payload)})
	}
	// 同时向观察者推送他们可见的状态（手牌全隐藏）。
	s.broadcastDoudizhuSpectatorState(roomID)
}

func (s *GameService) broadcastDoudizhuOver(roomID string, room *doudizhu.DoudizhuRoom) {
	if room == nil || room.State == nil {
		return
	}
	s.hub.BroadcastRoom(roomID, Envelope{
		Type: "game.over",
		Payload: mustMarshal(map[string]any{
			"room_id":    roomID,
			"game_kind":  "doudizhu",
			"winner":     room.State.Winner(),
			"reason":     "game_end",
			"status":     room.State.Status.String(),
			"multiplier": room.State.Multiplier,
			"score":      room.State.Score(),
		}),
	})
}

func (s *GameService) broadcastDoudizhuSpectatorState(roomID string) {
	uids := s.hub.connectedSpectatorUserIDs(roomID)
	// Doudizhu's sanitized view doesn't depend on which spectator is asking
	// (all spectators see the same redacted state), so we just build once.
	state := s.doudizhuMgr.SpectatorView(roomID)
	if state == nil {
		return
	}
	for _, uid := range uids {
		s.hub.BroadcastTo(uid, Envelope{Type: "game.state", Payload: mustMarshal(state)})
	}
}


