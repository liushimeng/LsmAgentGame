package ws

import (
	"encoding/json"

	"LsmWebGame/errcode"
	"LsmWebGame/game/chess"
	"LsmWebGame/game/xiangqi"
	"LsmWebGame/logger"

	"go.uber.org/zap"
)

func (s *GameService) handleJoin(c *Client, env Envelope) {
	var req struct {
		RoomID   string `json:"room_id"`
		GameKind string `json:"game_kind"`
		Mode     string `json:"mode"` // for junqi: "hidden" or "open"
	}
	if err := json.Unmarshal(env.Payload, &req); err != nil || req.RoomID == "" {
		s.sendError(c, env.Seq, 20001, "invalid game.join payload")
		return
	}
	gk := gameKindFromPayload(env.Payload)

	switch gk {
	case "xiangqi":
		s.handleXiangqiJoin(c, env, req.RoomID)
	case "chess":
		s.handleChessJoin(c, env, req.RoomID)
	case "junqi":
		s.handleJunqiJoin(c, env, req.RoomID, req.Mode)
	case "doudizhu":
		s.handleDoudizhuJoin(c, env, req.RoomID)
	case "texasholdem":
		s.handleTexasHoldemJoin(c, env, req.RoomID)
	case "werewolf":
		s.handleWerewolfJoin(c, env, req.RoomID)
	default:
		s.sendError(c, env.Seq, 20001, "unsupported game_kind: "+gk)
	}
}

func (s *GameService) handleXiangqiJoin(c *Client, env Envelope, roomID string) {
	room := s.xiangqiMgr.CreateGame(roomID, c.UserID)
	if room != nil {
		s.hub.SubscribeRoom(roomID, c)
		if room.State != nil {
			color, _ := room.PlayerColor(c.UserID)
			s.sendOK(c, env.Seq, "game.started", map[string]any{
				"room_id":  roomID,
				"my_color": color.String(),
				"ready":    true,
				"red_id":   room.RedID,
				"black_id": room.BlackID,
				"board":    room.State.BoardJSON(),
				"turn":     room.State.Turn.String(),
				"status":   room.State.Status.String(),
			})
			return
		}
		s.sendOK(c, env.Seq, "game.joined", map[string]any{
			"room_id":   roomID,
			"game_kind": "xiangqi",
			"my_color":  "red",
			"ready":     false,
		})
		return
	}

	joined, started, e := s.xiangqiMgr.JoinGame(roomID, c.UserID)
	if e != nil {
		s.sendError(c, env.Seq, e.Code, e.Message)
		return
	}
	s.hub.SubscribeRoom(roomID, c)
	if joined.State != nil {
		// Direct ack to the joiner (always).
		s.sendOK(c, env.Seq, "game.started", map[string]any{
			"room_id":    roomID,
			"game_kind":  "xiangqi",
			"my_color":   "black",
			"ready":      true,
			"red_id":     joined.RedID,
			"black_id":   joined.BlackID,
			"board":      joined.State.BoardJSON(),
			"turn":       joined.State.Turn.String(),
			"status":     joined.State.Status.String(),
			"move_count": len(joined.State.MoveHistory),
		})
		if started {
			s.hub.BroadcastRoom(roomID, Envelope{
				Type: "game.started",
				Payload: mustMarshal(map[string]any{
					"room_id":   roomID,
					"game_kind": "xiangqi",
					"ready":     true,
					"red_id":    joined.RedID,
					"black_id":  joined.BlackID,
					"board":     joined.State.BoardJSON(),
					"turn":      "red",
					"status":    "playing",
				}),
			})
			logger.L().Info("game started via ws",
				zap.String("kind", "xiangqi"),
				zap.String("room_id", roomID),
				zap.String("red", joined.RedID),
				zap.String("black", joined.BlackID))
		} else {
			logger.L().Info("xiangqi player reconnected",
				zap.String("room_id", roomID),
				zap.String("user_id", c.UserID))
		}
	} else {
		color, _ := joined.PlayerColor(c.UserID)
		s.sendOK(c, env.Seq, "game.joined", map[string]any{
			"room_id":   roomID,
			"game_kind": "xiangqi",
			"my_color":  color.String(),
			"ready":     false,
		})
	}
}

func (s *GameService) handleChessJoin(c *Client, env Envelope, roomID string) {
	room := s.chessMgr.CreateGame(roomID, c.UserID)
	if room != nil {
		s.hub.SubscribeRoom(roomID, c)
		if room.State != nil {
			color, _ := room.PlayerColor(c.UserID)
			s.sendOK(c, env.Seq, "game.started", map[string]any{
				"room_id":    roomID,
				"game_kind":  "chess",
				"my_color":   color.String(),
				"ready":      true,
				"white_id":   room.WhiteID,
				"black_id":   room.BlackID,
				"board":      room.State.BoardJSON(),
				"turn":       room.State.Turn.String(),
				"status":     room.State.Status.String(),
				"move_count": len(room.State.MoveHistory),
			})
			return
		}
		s.sendOK(c, env.Seq, "game.joined", map[string]any{
			"room_id":   roomID,
			"game_kind": "chess",
			"my_color":  "white",
			"ready":     false,
		})
		return
	}

	joined, started, e := s.chessMgr.JoinGame(roomID, c.UserID)
	if e != nil {
		s.sendError(c, env.Seq, e.Code, e.Message)
		return
	}
	s.hub.SubscribeRoom(roomID, c)
	if joined.State != nil {
		// Direct ack to the joiner (always, reconnection or fresh).
		s.sendOK(c, env.Seq, "game.started", map[string]any{
			"room_id":    roomID,
			"game_kind":  "chess",
			"my_color":   "black",
			"ready":      true,
			"white_id":   joined.WhiteID,
			"black_id":   joined.BlackID,
			"board":      joined.State.BoardJSON(),
			"turn":       joined.State.Turn.String(),
			"status":     joined.State.Status.String(),
			"move_count": len(joined.State.MoveHistory),
		})
		// Broadcast game.started to the room ONLY on fresh game start.
		// On reconnection the reconnecting player already receives the correct
		// state via the direct ack above; broadcasting here would overwrite the
		// opponent's live state with stale move_count=0 (BUG-CHESS-WS-SYNC).
		if started {
			s.hub.BroadcastRoom(roomID, Envelope{
				Type: "game.started",
				Payload: mustMarshal(map[string]any{
					"room_id":   roomID,
					"game_kind": "chess",
					"ready":     true,
					"white_id":  joined.WhiteID,
					"black_id":  joined.BlackID,
					"board":     joined.State.BoardJSON(),
					"turn":      "white",
					"status":    "playing",
				}),
			})
			logger.L().Info("game started via ws",
				zap.String("kind", "chess"),
				zap.String("room_id", roomID),
				zap.String("white", joined.WhiteID),
				zap.String("black", joined.BlackID))
		} else {
			logger.L().Info("chess player reconnected",
				zap.String("room_id", roomID),
				zap.String("user_id", c.UserID))
		}
	} else {
		color, _ := joined.PlayerColor(c.UserID)
		s.sendOK(c, env.Seq, "game.joined", map[string]any{
			"room_id":   roomID,
			"game_kind": "chess",
			"my_color":  color.String(),
			"ready":     false,
		})
	}
}

func (s *GameService) handleMove(c *Client, env Envelope) {
	gk := gameKindFromPayload(env.Payload)
	switch gk {
	case "xiangqi":
		s.handleXiangqiMove(c, env)
	case "chess":
		s.handleChessMove(c, env)
	case "junqi":
		s.handleJunqiMove(c, env)
	default:
		s.sendError(c, env.Seq, 20001, "unsupported game_kind: "+gk)
	}
}

func (s *GameService) handleXiangqiMove(c *Client, env Envelope) {
	var req struct {
		RoomID string           `json:"room_id"`
		From   xiangqi.Position `json:"from"`
		To     xiangqi.Position `json:"to"`
	}
	if err := json.Unmarshal(env.Payload, &req); err != nil || req.RoomID == "" {
		s.sendError(c, env.Seq, 20001, "invalid game.move payload")
		return
	}
	if s.rejectIfSpectator(c, env, req.RoomID) {
		return
	}
	result, e := s.xiangqiMgr.MakeMove(req.RoomID, c.UserID, req.From, req.To)
	if e != nil {
		s.sendError(c, env.Seq, e.Code, e.Message)
		return
	}
	moveData := map[string]any{
		"room_id":   req.RoomID,
		"game_kind": "xiangqi",
		"move":      result.Move,
		"turn":      result.Turn.String(),
		"status":    result.Status.String(),
		"check":     result.Check,
		"board":     result.Board,
	}
	s.hub.BroadcastRoom(req.RoomID, Envelope{Type: "game.moved", Payload: mustMarshal(moveData)})
	s.broadcastXiangqiSpectatorState(req.RoomID)
	if result.Status != xiangqi.StatusPlaying {
		winner := ""
		if result.Status == xiangqi.StatusRedWin {
			winner = "red"
		} else if result.Status == xiangqi.StatusBlackWin {
			winner = "black"
		}
		s.hub.BroadcastRoom(req.RoomID, Envelope{
			Type: "game.over",
			Payload: mustMarshal(map[string]any{
				"room_id":   req.RoomID,
				"game_kind": "xiangqi",
				"winner":    winner,
				"reason":    "checkmate",
				"status":    result.Status.String(),
			}),
		})
	}
}

func (s *GameService) handleChessMove(c *Client, env Envelope) {
	var req struct {
		RoomID    string          `json:"room_id"`
		From      chess.Position  `json:"from"`
		To        chess.Position  `json:"to"`
		Promotion chess.PieceType `json:"promotion,omitempty"`
	}
	if err := json.Unmarshal(env.Payload, &req); err != nil || req.RoomID == "" {
		s.sendError(c, env.Seq, 20001, "invalid game.move payload")
		return
	}
	if s.rejectIfSpectator(c, env, req.RoomID) {
		return
	}
	result, e := s.chessMgr.MakeMove(req.RoomID, c.UserID, req.From, req.To, req.Promotion)
	if e != nil {
		s.sendError(c, env.Seq, e.Code, e.Message)
		return
	}
	moveData := map[string]any{
		"room_id":   req.RoomID,
		"game_kind": "chess",
		"move":      result.Move,
		"turn":      result.Turn.String(),
		"status":    result.Status.String(),
		"check":     result.Check,
		"board":     result.Board,
		"reason":    result.Reason,
	}
	s.hub.BroadcastRoom(req.RoomID, Envelope{Type: "game.moved", Payload: mustMarshal(moveData)})
	s.broadcastChessSpectatorState(req.RoomID)
	if result.Status != chess.StatusPlaying {
		winner := ""
		if result.Status == chess.StatusWhiteWin {
			winner = "white"
		} else if result.Status == chess.StatusBlackWin {
			winner = "black"
		}
		s.hub.BroadcastRoom(req.RoomID, Envelope{
			Type: "game.over",
			Payload: mustMarshal(map[string]any{
				"room_id":   req.RoomID,
				"game_kind": "chess",
				"winner":    winner,
				"reason":    result.Reason,
				"status":    result.Status.String(),
			}),
		})
	}
}

func (s *GameService) handlePromote(c *Client, env Envelope) {
	var req struct {
		RoomID    string          `json:"room_id"`
		Promotion chess.PieceType `json:"promotion"`
	}
	if err := json.Unmarshal(env.Payload, &req); err != nil || req.RoomID == "" {
		s.sendError(c, env.Seq, 20001, "invalid game.promote payload")
		return
	}
	if s.rejectIfSpectator(c, env, req.RoomID) {
		return
	}
	// Delegate to chess manager via the existing MakeMove path (a pending
	// promotion move is stored on the GameState). We just acknowledge.
	s.sendOK(c, env.Seq, "game.promoted", map[string]any{
		"room_id":   req.RoomID,
		"game_kind": "chess",
		"promotion": req.Promotion,
	})
	logger.L().Info("chess promotion confirmed",
		zap.String("room_id", req.RoomID),
		zap.Int("promotion", int(req.Promotion)))
}

func (s *GameService) handleResign(c *Client, env Envelope) {
	var req struct {
		RoomID   string `json:"room_id"`
		GameKind string `json:"game_kind"`
	}
	if err := json.Unmarshal(env.Payload, &req); err != nil || req.RoomID == "" {
		s.sendError(c, env.Seq, 20001, "invalid game.resign payload")
		return
	}
	if s.rejectIfSpectator(c, env, req.RoomID) {
		return
	}
	gk := gameKindFromPayload(env.Payload)

	switch gk {
	case "xiangqi":
		result, e := s.xiangqiMgr.Resign(req.RoomID, c.UserID)
		if e != nil {
			s.sendError(c, env.Seq, e.Code, e.Message)
			return
		}
		s.hub.BroadcastRoom(req.RoomID, Envelope{
			Type: "game.over",
			Payload: mustMarshal(map[string]any{
				"room_id":   req.RoomID,
				"game_kind": "xiangqi",
				"winner":    result.Winner,
				"reason":    result.Reason,
				"status":    result.Status.String(),
			}),
		})
		s.leaveRoomQuiet(req.RoomID, c.UserID)
	case "chess":
		result, e := s.chessMgr.Resign(req.RoomID, c.UserID)
		if e != nil {
			s.sendError(c, env.Seq, e.Code, e.Message)
			return
		}
		s.hub.BroadcastRoom(req.RoomID, Envelope{
			Type: "game.over",
			Payload: mustMarshal(map[string]any{
				"room_id":   req.RoomID,
				"game_kind": "chess",
				"winner":    result.Winner,
				"reason":    result.Reason,
				"status":    result.Status.String(),
			}),
		})
		s.leaveRoomQuiet(req.RoomID, c.UserID)
	case "junqi":
		result, e := s.junqiMgr.Resign(req.RoomID, c.UserID)
		if e != nil {
			s.sendError(c, env.Seq, e.Code, e.Message)
			return
		}
		s.hub.BroadcastRoom(req.RoomID, Envelope{
			Type: "game.over",
			Payload: mustMarshal(map[string]any{
				"room_id":   req.RoomID,
				"game_kind": "junqi",
				"winner":    result.Winner,
				"reason":    result.Reason,
				"status":    result.Status.String(),
			}),
		})
		s.leaveRoomQuiet(req.RoomID, c.UserID)
	case "doudizhu":
		room, e := s.doudizhuMgr.Resign(req.RoomID, c.UserID)
		if e != nil {
			s.sendError(c, env.Seq, e.Code, e.Message)
			return
		}
		s.broadcastDoudizhuOver(req.RoomID, room)
		s.broadcastDoudizhuState(req.RoomID)
		s.leaveRoomQuiet(req.RoomID, c.UserID)
	case "texasholdem":
		room, e := s.texasHoldemMgr.Resign(req.RoomID, c.UserID)
		if e != nil {
			s.sendError(c, env.Seq, e.Code, e.Message)
			return
		}
		s.broadcastTexasHoldemOver(req.RoomID, room)
		s.broadcastTexasHoldemState(req.RoomID)
		s.broadcastTexasHoldemSpectatorState(req.RoomID)
		s.leaveRoomQuiet(req.RoomID, c.UserID)
	}
}

func (s *GameService) handleLeave(c *Client, env Envelope) {
	var req roomOnlyPayload
	if err := json.Unmarshal(env.Payload, &req); err != nil || req.RoomID == "" {
		s.sendError(c, env.Seq, 20001, "invalid game.leave payload")
		return
	}
	// Spectators leave via game.unspectate, not game.leave. Refuse the latter
	// to avoid double-handling (game.leave triggers a resign).
	if s.isSpectatorOf(req.RoomID, c) {
		s.sendError(c, env.Seq, errcode.ErrSpectatorInputForbidden,
			"spectators should use game.unspectate")
		return
	}
	gk := gameKindFromPayload(env.Payload)

	switch gk {
	case "xiangqi":
		result, _ := s.xiangqiMgr.Resign(req.RoomID, c.UserID)
		if result != nil {
			s.hub.BroadcastRoom(req.RoomID, Envelope{
				Type: "game.over",
				Payload: mustMarshal(map[string]any{
					"room_id":   req.RoomID,
					"game_kind": "xiangqi",
					"winner":    result.Winner,
					"reason":    result.Reason,
					"status":    result.Status.String(),
				}),
			})
		}
		s.hub.UnsubscribeRoom(req.RoomID, c)
		s.xiangqiMgr.RemoveGame(req.RoomID)
		s.leaveRoomQuiet(req.RoomID, c.UserID)
		s.sendOK(c, env.Seq, "game.left", map[string]any{"room_id": req.RoomID, "game_kind": "xiangqi"})
		logger.L().Info("player left xiangqi game", zap.String("room_id", req.RoomID), zap.String("user_id", c.UserID))
	case "chess":
		result, _ := s.chessMgr.Resign(req.RoomID, c.UserID)
		if result != nil {
			s.hub.BroadcastRoom(req.RoomID, Envelope{
				Type: "game.over",
				Payload: mustMarshal(map[string]any{
					"room_id":   req.RoomID,
					"game_kind": "chess",
					"winner":    result.Winner,
					"reason":    result.Reason,
					"status":    result.Status.String(),
				}),
			})
		}
		s.hub.UnsubscribeRoom(req.RoomID, c)
		s.chessMgr.RemoveGame(req.RoomID)
		s.leaveRoomQuiet(req.RoomID, c.UserID)
		s.sendOK(c, env.Seq, "game.left", map[string]any{"room_id": req.RoomID, "game_kind": "chess"})
		logger.L().Info("player left chess game", zap.String("room_id", req.RoomID), zap.String("user_id", c.UserID))
	case "junqi":
		result, _ := s.junqiMgr.Resign(req.RoomID, c.UserID)
		if result != nil {
			s.hub.BroadcastRoom(req.RoomID, Envelope{
				Type: "game.over",
				Payload: mustMarshal(map[string]any{
					"room_id":   req.RoomID,
					"game_kind": "junqi",
					"winner":    result.Winner,
					"reason":    result.Reason,
					"status":    result.Status.String(),
				}),
			})
		}
		s.hub.UnsubscribeRoom(req.RoomID, c)
		s.junqiMgr.RemoveGame(req.RoomID)
		s.leaveRoomQuiet(req.RoomID, c.UserID)
		s.sendOK(c, env.Seq, "game.left", map[string]any{"room_id": req.RoomID, "game_kind": "junqi"})
		logger.L().Info("player left junqi game", zap.String("room_id", req.RoomID), zap.String("user_id", c.UserID))
	case "doudizhu":
		// 玩家中途退出：若对局进行中，按认输处理并广播结果。
		if room, e := s.doudizhuMgr.Resign(req.RoomID, c.UserID); e == nil && room != nil {
			s.broadcastDoudizhuOver(req.RoomID, room)
			s.broadcastDoudizhuState(req.RoomID)
		}
		s.hub.UnsubscribeRoom(req.RoomID, c)
		s.doudizhuMgr.RemoveGame(req.RoomID)
		s.leaveRoomQuiet(req.RoomID, c.UserID)
		s.sendOK(c, env.Seq, "game.left", map[string]any{"room_id": req.RoomID, "game_kind": "doudizhu"})
		logger.L().Info("player left doudizhu game", zap.String("room_id", req.RoomID), zap.String("user_id", c.UserID))
	case "texasholdem":
		if room, e := s.texasHoldemMgr.Resign(req.RoomID, c.UserID); e == nil && room != nil {
			s.broadcastTexasHoldemOver(req.RoomID, room)
			s.broadcastTexasHoldemState(req.RoomID)
		}
		s.hub.UnsubscribeRoom(req.RoomID, c)
		s.texasHoldemMgr.RemoveGame(req.RoomID)
		s.leaveRoomQuiet(req.RoomID, c.UserID)
		s.sendOK(c, env.Seq, "game.left", map[string]any{"room_id": req.RoomID, "game_kind": "texasholdem"})
		logger.L().Info("player left texasholdem game", zap.String("room_id", req.RoomID), zap.String("user_id", c.UserID))
	}
}

func (s *GameService) handleGetState(c *Client, env Envelope) {
	var req roomOnlyPayload
	if err := json.Unmarshal(env.Payload, &req); err != nil || req.RoomID == "" {
		s.sendError(c, env.Seq, 20001, "invalid game.state payload")
		return
	}
	gk := gameKindFromPayload(env.Payload)

	// BUG-R244-P1-01 (2026-08-06): Frontend WerewolfGamePage caches the
	// caller's room role in sessionStorage and, on cached "player" hits,
	// skips `game.join` (which would otherwise be rejected with ErrRoomFull
	// 30001 by an already-playing room). Instead it calls `requestState()`
	// → `game.state` directly. Without this auto-subscribe, the WS hub never
	// places the client into `h.rooms[roomID]`, so any subsequent
	// `BroadcastRoomIncludingSpectators` (chat fan-out, peer events …) hits
	// an empty set and the human player's outbound chat messages, peer-join
	// notifications, and game.state pushes are silently lost.
	//
	// SubscribeRoom is idempotent (set[c] = struct{}{}) so this is safe to
	// call from both spectator and player paths; spectators will still be
	// routed to the sanitized view by the `isSpectatorOf` check below.
	s.hub.SubscribeRoom(req.RoomID, c)

	// Spectators are routed to the sanitized view automatically — they never
	// see a player-private state even when polling on demand.
	if s.isSpectatorOf(req.RoomID, c) {
		switch gk {
		case "xiangqi":
			state, e := s.xiangqiMgr.SpectatorState(req.RoomID, c.UserID)
			if e != nil {
				s.sendError(c, env.Seq, e.Code, e.Message)
				return
			}
			s.sendOK(c, env.Seq, "game.state", state)
			return
		case "chess":
			state, e := s.chessMgr.SpectatorState(req.RoomID, c.UserID)
			if e != nil {
				s.sendError(c, env.Seq, e.Code, e.Message)
				return
			}
			s.sendOK(c, env.Seq, "game.state", state)
			return
		case "junqi":
			state, e := s.junqiMgr.SpectatorState(req.RoomID, c.UserID)
			if e != nil {
				s.sendError(c, env.Seq, e.Code, e.Message)
				return
			}
			s.sendOK(c, env.Seq, "game.state", state)
			return
		case "doudizhu":
			state := s.doudizhuMgr.SpectatorView(req.RoomID)
			if state == nil {
				s.sendError(c, env.Seq, errcode.ErrRoomNotFound, "")
				return
			}
			s.sendOK(c, env.Seq, "game.state", state)
			return
		case "texasholdem":
			state := s.texasHoldemMgr.SpectatorView(req.RoomID)
			if state == nil {
				s.sendError(c, env.Seq, errcode.ErrRoomNotFound, "")
				return
			}
			s.sendOK(c, env.Seq, "game.state", state)
			return
		case "werewolf":
			// BUG-WEREWOLF-P0-2 FIX: werewolf has no separate spectator manager,
			// but a spectator polling on demand should still get the sanitized
			// view rather than the per-player one.
			state, e := s.werewolfMgr.SpectatorState(req.RoomID, c.UserID)
			if e != nil {
				s.sendError(c, env.Seq, e.Code, e.Message)
				return
			}
			s.sendOK(c, env.Seq, "game.state", state)
			return
		}
	}

	switch gk {
	case "xiangqi":
		state, e := s.xiangqiMgr.GetState(req.RoomID, c.UserID)
		if e != nil {
			s.sendError(c, env.Seq, e.Code, e.Message)
			return
		}
		// Include game_kind in the response.
		payload := map[string]any{
			"room_id":    state.RoomID,
			"game_kind":  "xiangqi",
			"red_id":     state.RedID,
			"black_id":   state.BlackID,
			"ready":      state.Ready,
			"my_color":   state.MyColor,
			"turn":       state.Turn,
			"status":     state.Status,
			"check":      state.Check,
			"move_count": state.MoveLen,
		}
		if state.Board != nil {
			payload["board"] = state.Board
		}
		s.sendOK(c, env.Seq, "game.state", payload)
	case "chess":
		state, e := s.chessMgr.GetState(req.RoomID, c.UserID)
		if e != nil {
			s.sendError(c, env.Seq, e.Code, e.Message)
			return
		}
		payload := map[string]any{
			"room_id":    state.RoomID,
			"game_kind":  "chess",
			"white_id":   state.WhiteID,
			"black_id":   state.BlackID,
			"ready":      state.Ready,
			"my_color":   state.MyColor,
			"turn":       state.Turn,
			"status":     state.Status,
			"check":      state.Check,
			"move_count": state.MoveLen,
			"reason":     state.Reason,
		}
		if state.Board != nil {
			payload["board"] = state.Board
		}
		s.sendOK(c, env.Seq, "game.state", payload)
	case "junqi":
		state, e := s.junqiMgr.GetState(req.RoomID, c.UserID)
		if e != nil {
			s.sendError(c, env.Seq, e.Code, e.Message)
			return
		}
		payload := map[string]any{
			"room_id":    state.RoomID,
			"game_kind":  "junqi",
			"red_id":     state.RedID,
			"black_id":   state.BlackID,
			"ready":      state.Ready,
			"my_color":   state.MyColor,
			"mode":       state.Mode,
			"phase":      state.Phase,
			"turn":       state.Turn,
			"status":     state.Status,
			"move_count": state.MoveLen,
		}
		if len(state.BoardView) > 0 {
			payload["board_view"] = state.BoardView
		}
		s.sendOK(c, env.Seq, "game.state", payload)
	case "doudizhu":
		state, e := s.doudizhuMgr.GetState(req.RoomID, c.UserID)
		if e != nil {
			s.sendError(c, env.Seq, e.Code, e.Message)
			return
		}
		s.sendDoudizhuState(c, env.Seq, state)
	case "texasholdem":
		state, e := s.texasHoldemMgr.GetState(req.RoomID, c.UserID)
		if e != nil {
			s.sendError(c, env.Seq, e.Code, e.Message)
			return
		}
		s.sendTexasHoldemState(c, env.Seq, state)
	case "werewolf":
		// BUG-WEREWOLF-P0-2 FIX: werewolf was missing from handleGetState — the
		// front-end useWerewolf.requestState() 8s poll fell into the default
		// branch and got an error frame, leaving the UI stuck in waiting.
		state, e := s.werewolfMgr.GetState(req.RoomID, c.UserID)
		if e != nil {
			s.sendError(c, env.Seq, e.Code, e.Message)
			return
		}
		s.sendOK(c, env.Seq, "game.state", state)
	}
}

func (s *GameService) broadcastXiangqiSpectatorState(roomID string) {
	uids := s.hub.connectedSpectatorUserIDs(roomID)
	for _, uid := range uids {
		state, e := s.xiangqiMgr.SpectatorState(roomID, uid)
		if e != nil {
			continue
		}
		s.hub.BroadcastTo(uid, Envelope{Type: "game.state", Payload: mustMarshal(state)})
	}
}


