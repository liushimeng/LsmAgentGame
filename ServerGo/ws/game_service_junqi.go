package ws

import (
	"encoding/json"

	"LsmWebGame/game/junqi"
	"LsmWebGame/logger"

	"go.uber.org/zap"
)

func (s *GameService) handleJunqiLayout(c *Client, env Envelope) {
	var req struct {
		RoomID     string            `json:"room_id"`
		Placements []junqi.Placement `json:"placements"`
	}
	if err := json.Unmarshal(env.Payload, &req); err != nil || req.RoomID == "" {
		s.sendError(c, env.Seq, 20001, "invalid game.layout payload")
		return
	}
	if s.rejectIfSpectator(c, env, req.RoomID) {
		return
	}
	if e := s.junqiMgr.SubmitLayout(req.RoomID, c.UserID, req.Placements); e != nil {
		s.sendError(c, env.Seq, e.Code, e.Message)
		return
	}
	// Ack to the submitter.
	s.sendOK(c, env.Seq, "game.layout_accepted", map[string]any{
		"room_id":   req.RoomID,
		"game_kind": "junqi",
	})
	// Notify the other player.
	s.hub.BroadcastRoom(req.RoomID, Envelope{
		Type: "game.layout_submitted",
		Payload: mustMarshal(map[string]any{
			"room_id":   req.RoomID,
			"game_kind": "junqi",
			"by":        c.UserID,
		}),
	})
	// If both layouts are now submitted, transition to the playing phase.
	if s.junqiMgr.StartGameIfReady(req.RoomID) {
		s.hub.BroadcastRoom(req.RoomID, Envelope{
			Type: "game.started",
			Payload: mustMarshal(map[string]any{
				"room_id":   req.RoomID,
				"game_kind": "junqi",
				"phase":     "playing",
				"turn":      "red",
				"status":    "playing",
			}),
		})
		logger.L().Info("junqi game entered playing phase (via WS)", zap.String("room_id", req.RoomID))
	}
	s.broadcastJunqiSpectatorState(req.RoomID)
}

func (s *GameService) handleJunqiJoin(c *Client, env Envelope, roomID, mode string) {
	modeVal := junqi.ParseVisibilityMode(mode)
	if mode == "" {
		modeVal = junqi.ModeHidden // default to hidden (暗棋)
	}
	room := s.junqiMgr.CreateGame(roomID, c.UserID, modeVal)
	if room != nil {
		s.hub.SubscribeRoom(roomID, c)
		s.sendOK(c, env.Seq, "game.joined", map[string]any{
			"room_id":   roomID,
			"game_kind": "junqi",
			"my_color":  "red",
			"mode":      room.Mode.String(),
			"phase":     "layout",
			"ready":     false,
		})
		return
	}
	joined, _, e := s.junqiMgr.JoinGame(roomID, c.UserID)
	if e != nil {
		s.sendError(c, env.Seq, e.Code, e.Message)
		return
	}
	s.hub.SubscribeRoom(roomID, c)
	color, _ := joined.PlayerColor(c.UserID)
	myColorStr := "red"
	if color == junqi.Black {
		myColorStr = "black"
	}
	s.sendOK(c, env.Seq, "game.joined", map[string]any{
		"room_id":   roomID,
		"game_kind": "junqi",
		"my_color":  myColorStr,
		"mode":      joined.Mode.String(),
		"phase":     "layout",
		"ready":     true, // both players present, layout phase begins
	})
	// Notify the OTHER player (the creator) that the second player joined.
	s.hub.BroadcastRoom(roomID, Envelope{
		Type: "game.peer_joined",
		Payload: mustMarshal(map[string]any{
			"room_id":   roomID,
			"game_kind": "junqi",
			"phase":     "layout",
			"ready":     true,
		}),
	})
	logger.L().Info("junqi room now full, entering layout phase",
		zap.String("room_id", roomID),
		zap.String("red", joined.RedID),
		zap.String("black", joined.BlackID))
}

func (s *GameService) handleJunqiMove(c *Client, env Envelope) {
	var req struct {
		RoomID   string         `json:"room_id"`
		From     junqi.Position `json:"from"`
		To       junqi.Position `json:"to"`
		GameKind string         `json:"game_kind"`
	}
	if err := json.Unmarshal(env.Payload, &req); err != nil || req.RoomID == "" {
		s.sendError(c, env.Seq, 20001, "invalid game.move payload")
		return
	}
	if s.rejectIfSpectator(c, env, req.RoomID) {
		return
	}
	result, e := s.junqiMgr.MakeMove(req.RoomID, c.UserID, req.From, req.To)
	if e != nil {
		s.sendError(c, env.Seq, e.Code, e.Message)
		return
	}
	// Broadcast to all subscribers with the per-player view as seen by the
	// current user. The Hub delivers each envelope to the subscribed client,
	// but since the view is per-player we send two separate envelopes.
	// Easiest approach: include the move and a generic summary; let each
	// client call game.state to get its own view.
	moveData := map[string]any{
		"room_id":    req.RoomID,
		"game_kind":  "junqi",
		"move":       result.Move,
		"turn":       result.Turn,
		"status":     result.Status,
		"phase":      result.Phase,
		"my_color":   result.MyColor,
		"board_view": result.BoardView,
	}
	s.hub.BroadcastRoom(req.RoomID, Envelope{Type: "game.moved", Payload: mustMarshal(moveData)})
	s.broadcastJunqiSpectatorState(req.RoomID)

	if result.Status != "playing" {
		winner := ""
		if result.Status == "red_win" {
			winner = "red"
		} else if result.Status == "black_win" {
			winner = "black"
		}
		s.hub.BroadcastRoom(req.RoomID, Envelope{
			Type: "game.over",
			Payload: mustMarshal(map[string]any{
				"room_id":   req.RoomID,
				"game_kind": "junqi",
				"winner":    winner,
				"reason":    "checkmate",
				"status":    result.Status,
			}),
		})
	}
}

func (s *GameService) broadcastJunqiSpectatorState(roomID string) {
	uids := s.hub.connectedSpectatorUserIDs(roomID)
	for _, uid := range uids {
		state, e := s.junqiMgr.SpectatorState(roomID, uid)
		if e != nil {
			continue
		}
		s.hub.BroadcastTo(uid, Envelope{Type: "game.state", Payload: mustMarshal(state)})
	}
}


