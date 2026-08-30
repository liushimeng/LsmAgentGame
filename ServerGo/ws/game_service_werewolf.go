package ws

import (
	"encoding/json"

	"LsmAgentGame/agent/wwtypes"
	"LsmAgentGame/errcode"
	"LsmAgentGame/game/werewolf"
	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

func (s *GameService) handleWerewolfJoin(c *Client, env Envelope, roomID string) {
	room, started, e := s.werewolfMgr.JoinGame(roomID, c.UserID)
	if e != nil {
		// BUG-WEREWOLF-FULL-AI-WAITING FIX: in a full-AI room (creator downgraded
		// to a spectator row by §65a `CreateRoomWithAgents`), the room has
		// already been force-started and is now Phase != PhaseFilling. A late
		// `game.join` from the creator's freshly-opened WS — which is racing
		// the HTTP response to the room-creation POST — finds Phase != Filling
		// and gets ErrRoomFull. Instead of bouncing that back as
		// `sendError` (which leaves the lobby page stuck on
		// "🐺 等待 7 位玩家入座…" forever), silently downgrade to the
		// spectate path: same client, same room, same eventual live view.
		// We also tolerate ErrGameNotStarted here as defensive coverage —
		// §66's State != Filling is the canonical signal, but if some other
		// future code path raises ErrGameNotStarted for a not-yet-started
		// werewolf room we'd rather err on the side of showing the user a
		// live view than leaving them stuck.
		if e.Code == errcode.ErrRoomFull || e.Code == errcode.ErrGameNotStarted {
			logger.L().Info("werewolf join downgraded to spectate (full-AI room or late join)",
				zap.String("room_id", roomID),
				zap.String("user_id", c.UserID),
				zap.Int("err_code", e.Code))
			// We still acknowledge the original game.join envelope with a
			// synthetic OK so the client's caller (typically the page-level
			// useEffect in WerewolfGamePage) doesn't sit waiting on the
			// request's seq indefinitely. The real state arrive via the
			// subsequent `game.spectate` round trip inside handleSpectate.
			s.sendOK(c, env.Seq, "game.joined", map[string]any{
				"room_id":                roomID,
				"game_kind":              "werewolf",
				"my_seat":                -1,
				"phase":                  "filling",
				"ready":                  true,
				"downgraded_to_spectate": true,
			})
			// hand off to the standard spectator handler — it will rewrite
			// the env payload with the proper werewolf spectate state.
			spectateEnv := Envelope{
				Type:    "game.spectate",
				Seq:     env.Seq,
				Payload: mustMarshal(spectatePayload{RoomID: roomID}),
			}
			s.handleSpectate(c, spectateEnv)
			return
		}
		s.sendError(c, env.Seq, e.Code, e.Message)
		return
	}
	s.hub.SubscribeRoom(roomID, c)

	seat, _ := room.SeatOf(c.UserID)
	phase := "filling"
	ready := room.IsReady()
	if room.State != nil && room.State.Phase != 0 {
		phase = room.State.Phase.String()
	}
	s.sendOK(c, env.Seq, "game.joined", map[string]any{
		"room_id":   roomID,
		"game_kind": "werewolf",
		"my_seat":   int(seat),
		"phase":     phase,
		"ready":     ready,
	})
	if started {
		s.hub.BroadcastRoom(roomID, Envelope{
			Type: "game.started",
			Payload: mustMarshal(map[string]any{
				"room_id":   roomID,
				"game_kind": "werewolf",
				"phase":     "night_wolves",
				"ready":     true,
			}),
		})
		s.broadcastWerewolfState(roomID)
		logger.L().Info("werewolf game started via ws",
			zap.String("room_id", roomID),
			zap.Int64("seed", room.State.Seed))
	} else {
		s.hub.BroadcastRoom(roomID, Envelope{
			Type: "game.peer_joined",
			Payload: mustMarshal(map[string]any{
				"room_id":      roomID,
				"game_kind":    "werewolf",
				"ready":        ready,
				"player_count": room.Occupied(),
			}),
		})
		// BUG-WEREWOLF-P0-2 FIX: for reconnect / late-join into an already-started
		// game, broadcastWerewolfState is only fired inside the `started` branch.
		// Push a per-seat view directly to this client so they immediately see
		// the live game state instead of being stuck on the waiting board.
		if room.State != nil && room.State.Phase != werewolf.PhaseFilling {
			if state := s.werewolfMgr.StateForSeat(roomID, seat); state != nil {
				s.hub.BroadcastTo(c.UserID, Envelope{
					Type:    "game.state",
					Payload: mustMarshal(state),
				})
			}
		}
	}
}

type werewolfActionPayload struct {
	RoomID string `json:"room_id"`
	Action string `json:"action"` // "wolf_kill" | "seer_check" | "witch_act" | "guard_protect" | "demon_hunter_hunt"
	// wolf_kill: target = 被刀的人;-1=空刀
	// seer_check: target = 被查验的人
	// witch_act: witch_action / witch_target
	// guard_protect (§134): target = 被守护的人;-1=空守
	// demon_hunter_hunt (§猎魔人): target = 被狩猎的人;-1=空过
	WitchAction string `json:"witch_action,omitempty"` // "none"|"antidote"|"poison"
	WitchTarget *int   `json:"witch_target,omitempty"`
	Target      *int   `json:"target,omitempty"`
	// §20260810-04 U2 — wolf_kill 可选刀人理由(≤30 字,仅狼 bot GameContext 可见)。
	// 旧客户端不传即为 "",向后兼容。
	Reason string `json:"reason,omitempty"`
}

func (s *GameService) handleWerewolfAction(c *Client, env Envelope) {
	var req werewolfActionPayload
	if err := json.Unmarshal(env.Payload, &req); err != nil || req.RoomID == "" {
		s.sendError(c, env.Seq, 20001, "invalid game.werewolf_action payload")
		return
	}
	if s.rejectIfSpectator(c, env, req.RoomID) {
		return
	}

	switch req.Action {
	case "wolf_kill":
		var t werewolf.Seat = werewolf.NoSeat
		if req.Target != nil {
			t = werewolf.Seat(*req.Target)
		}
		// §20260810-04 U2 — 人类狼人的刀人理由(可选,旧客户端为空)。
		if _, e := s.werewolfMgr.Action_WolfKill(req.RoomID, c.UserID, t, req.Reason); e != nil {
			s.sendError(c, env.Seq, e.Code, e.Message)
			return
		}
	case "guard_protect":
		// §134 守卫夜间守护。复用 req.Target 字段(与 seer_check 一致);
		// target=-1 表示空守(NoSeat)。
		var t werewolf.Seat = werewolf.NoSeat
		if req.Target != nil {
			t = werewolf.Seat(*req.Target)
		}
		if _, e := s.werewolfMgr.Action_GuardProtect(req.RoomID, c.UserID, t); e != nil {
			s.sendError(c, env.Seq, e.Code, e.Message)
			return
		}
	case "knight_duel":
		// §198 骑士白天决斗。复用 req.Target 字段(对齐 seer_check / guard_protect)。
		// target=-1 表示"本轮放弃,不消耗机会"(枚举保留,详见 tools.go BuildTools)。
		var t werewolf.Seat = werewolf.NoSeat
		if req.Target != nil {
			t = werewolf.Seat(*req.Target)
		}
		if _, e := s.werewolfMgr.Action_KnightDuel(req.RoomID, c.UserID, t); e != nil {
			s.sendError(c, env.Seq, e.Code, e.Message)
			return
		}
	case "demon_hunter_hunt":
		// §猎魔人 夜间狩猎。复用 req.Target 字段(对齐 seer_check / guard_protect)。
		// target=-1 表示空过(NoSeat)。
		var t werewolf.Seat = werewolf.NoSeat
		if req.Target != nil {
			t = werewolf.Seat(*req.Target)
		}
		if _, e := s.werewolfMgr.Action_DemonHunterHunt(req.RoomID, c.UserID, t); e != nil {
			s.sendError(c, env.Seq, e.Code, e.Message)
			return
		}
	case "seer_check":
		if req.Target == nil {
			s.sendError(c, env.Seq, 20001, "missing target")
			return
		}
		if _, e := s.werewolfMgr.Action_SeerCheck(req.RoomID, c.UserID, werewolf.Seat(*req.Target)); e != nil {
			s.sendError(c, env.Seq, e.Code, e.Message)
			return
		}
	case "witch_act":
		if req.WitchAction == "" {
			s.sendError(c, env.Seq, 20001, "missing witch_action")
			return
		}
		var t werewolf.Seat = werewolf.NoSeat
		if req.WitchTarget != nil {
			t = werewolf.Seat(*req.WitchTarget)
		}
		if _, e := s.werewolfMgr.Action_Witch(req.RoomID, c.UserID, req.WitchAction, t); e != nil {
			s.sendError(c, env.Seq, e.Code, e.Message)
			return
		}
	default:
		s.sendError(c, env.Seq, 20001, "unknown werewolf action: "+req.Action)
		return
	}
	s.hub.BroadcastRoom(req.RoomID, Envelope{
		Type: "game.action_accepted",
		Payload: mustMarshal(map[string]any{
			"room_id":   req.RoomID,
			"game_kind": "werewolf",
			"by":        c.UserID,
			"action":    req.Action,
		}),
	})
	s.broadcastWerewolfState(req.RoomID)
	s.broadcastWerewolfSpecial(req.RoomID, c.UserID, req.Action)
	s.checkWerewolfGameOver(req.RoomID)
	// BUG-WEREWOLF-P0-3 FIX: wake all bot agents after every phase transition
	// (wolf_kill / seer_check / witch_act) so they react to the new state.
	// broadcastWerewolfState also wakes, but we call again here explicitly so
	// the intent is clear at the action site (in case the broadcast path is
	// ever changed to skip the wake).
	s.wakeWerewolfAgents(req.RoomID, "state_change")
}

func (s *GameService) wakeWerewolfAgents(roomID, kind string) {
	if s.werewolfMgr == nil {
		return
	}
	snap := wwtypes.GameContext{}
	// BUG-WEREWOLF-EMPTY-CONTEXT (Round 21): previously we always passed an
	// empty GameContext. WakeAllAgents builds per-seat contexts from live state,
	// but when state is unavailable mid-teardown it falls back to this snap.
	// Seed the snap with the current phase so the fallback still carries enough
	// information for agents to act, and so the WakeActingAgents path can tell
	// which phase it is waking into.
	if view := s.werewolfMgr.SpectatorView(roomID); view != nil {
		snap.Phase = view.Phase
	}
	s.werewolfMgr.WakeAllAgents(roomID, kind, snap)
}

func (s *GameService) handleWerewolfVote(c *Client, env Envelope) {
	var req struct {
		RoomID string `json:"room_id"`
		Target *int   `json:"target"` // -1 = 弃票
	}
	if err := json.Unmarshal(env.Payload, &req); err != nil || req.RoomID == "" {
		s.sendError(c, env.Seq, 20001, "invalid game.werewolf_vote payload")
		return
	}
	if s.rejectIfSpectator(c, env, req.RoomID) {
		return
	}
	var t werewolf.Seat = werewolf.NoSeat
	if req.Target != nil {
		t = werewolf.Seat(*req.Target)
	}
	if _, e := s.werewolfMgr.Action_DayVote(req.RoomID, c.UserID, t); e != nil {
		s.sendError(c, env.Seq, e.Code, e.Message)
		return
	}
	s.hub.BroadcastRoom(req.RoomID, Envelope{
		Type: "game.action_accepted",
		Payload: mustMarshal(map[string]any{
			"room_id":   req.RoomID,
			"game_kind": "werewolf",
			"by":        c.UserID,
			"action":    "vote",
			"target":    req.Target,
		}),
	})
	s.broadcastWerewolfState(req.RoomID)
}

func (s *GameService) handleWerewolfSuicide(c *Client, env Envelope) {
	var req struct {
		RoomID string `json:"room_id"`
	}
	if err := json.Unmarshal(env.Payload, &req); err != nil || req.RoomID == "" {
		s.sendError(c, env.Seq, 20001, "invalid game.werewolf_suicide payload")
		return
	}
	if s.rejectIfSpectator(c, env, req.RoomID) {
		return
	}
	if _, e := s.werewolfMgr.Action_WolfSuicide(req.RoomID, c.UserID); e != nil {
		s.sendError(c, env.Seq, e.Code, e.Message)
		return
	}
	s.hub.BroadcastRoom(req.RoomID, Envelope{
		Type: "game.action_accepted",
		Payload: mustMarshal(map[string]any{
			"room_id":   req.RoomID,
			"game_kind": "werewolf",
			"by":        c.UserID,
			"action":    "suicide",
		}),
	})
	s.broadcastWerewolfState(req.RoomID)
	s.checkWerewolfGameOver(req.RoomID)
}

// handleWerewolfSuicideTake §20260830-02 — 自爆带走(人类玩家路径)。
// payload { room_id, target? };target 缺省/-1 = 放弃带走。
// 与 Agent 工具 wolf_suicide_take 同源进 Action_SuicideTake(公平性不变式 3)。
func (s *GameService) handleWerewolfSuicideTake(c *Client, env Envelope) {
	var req struct {
		RoomID string `json:"room_id"`
		Target *int   `json:"target"`
	}
	if err := json.Unmarshal(env.Payload, &req); err != nil || req.RoomID == "" {
		s.sendError(c, env.Seq, 20001, "invalid game.werewolf_suicide_take payload")
		return
	}
	if s.rejectIfSpectator(c, env, req.RoomID) {
		return
	}
	var t werewolf.Seat = werewolf.NoSeat
	if req.Target != nil {
		t = werewolf.Seat(*req.Target)
	}
	if _, e := s.werewolfMgr.Action_SuicideTake(req.RoomID, c.UserID, t); e != nil {
		s.sendError(c, env.Seq, e.Code, e.Message)
		return
	}
	s.hub.BroadcastRoom(req.RoomID, Envelope{
		Type: "game.action_accepted",
		Payload: mustMarshal(map[string]any{
			"room_id":   req.RoomID,
			"game_kind": "werewolf",
			"by":        c.UserID,
			"action":    "suicide_take",
			"target":    req.Target,
		}),
	})
	s.broadcastWerewolfState(req.RoomID)
	s.checkWerewolfGameOver(req.RoomID)
}

func (s *GameService) handleWerewolfShoot(c *Client, env Envelope) {
	var req struct {
		RoomID string `json:"room_id"`
		Target *int   `json:"target"`
	}
	if err := json.Unmarshal(env.Payload, &req); err != nil || req.RoomID == "" {
		s.sendError(c, env.Seq, 20001, "invalid game.werewolf_shoot payload")
		return
	}
	if s.rejectIfSpectator(c, env, req.RoomID) {
		return
	}
	var t werewolf.Seat = werewolf.NoSeat
	if req.Target != nil {
		t = werewolf.Seat(*req.Target)
	}
	if _, e := s.werewolfMgr.Action_HunterShoot(req.RoomID, c.UserID, t); e != nil {
		s.sendError(c, env.Seq, e.Code, e.Message)
		return
	}
	s.hub.BroadcastRoom(req.RoomID, Envelope{
		Type: "game.action_accepted",
		Payload: mustMarshal(map[string]any{
			"room_id":   req.RoomID,
			"game_kind": "werewolf",
			"by":        c.UserID,
			"action":    "shoot",
			"target":    req.Target,
		}),
	})
	s.broadcastWerewolfState(req.RoomID)
	s.checkWerewolfGameOver(req.RoomID)
}

func (s *GameService) handleWerewolfSheriff(c *Client, env Envelope) {
	var req struct {
		RoomID string `json:"room_id"`
		Action string `json:"action"` // "candidate" | "elect" | "vote"(等同于普通投票但仅警长阶段)
		Target *int   `json:"target,omitempty"`
	}
	if err := json.Unmarshal(env.Payload, &req); err != nil || req.RoomID == "" {
		s.sendError(c, env.Seq, 20001, "invalid game.werewolf_sheriff payload")
		return
	}
	if s.rejectIfSpectator(c, env, req.RoomID) {
		return
	}
	switch req.Action {
	case "candidate":
		if _, e := s.werewolfMgr.Action_SheriffCandidate(req.RoomID, c.UserID); e != nil {
			s.sendError(c, env.Seq, e.Code, e.Message)
			return
		}
	case "vote":
		var t werewolf.Seat = werewolf.NoSeat
		if req.Target != nil {
			t = werewolf.Seat(*req.Target)
		}
		if _, e := s.werewolfMgr.Action_DayVote(req.RoomID, c.UserID, t); e != nil {
			s.sendError(c, env.Seq, e.Code, e.Message)
			return
		}
	case "elect":
		// §报告-20260804-03 BUG-06: 传 c.UserID 让引擎校验发起者为存活入座玩家。
		// 此前不传 userID,任何人都能抢在全员投票前结算,打断竞选。
		if _, e := s.werewolfMgr.Action_SheriffElect(req.RoomID, c.UserID); e != nil {
			s.sendError(c, env.Seq, e.Code, e.Message)
			return
		}
	default:
		s.sendError(c, env.Seq, 20001, "unknown werewolf sheriff action: "+req.Action)
		return
	}
	s.hub.BroadcastRoom(req.RoomID, Envelope{
		Type: "game.action_accepted",
		Payload: mustMarshal(map[string]any{
			"room_id":   req.RoomID,
			"game_kind": "werewolf",
			"by":        c.UserID,
			"action":    "sheriff_" + req.Action,
		}),
	})
	s.broadcastWerewolfState(req.RoomID)
}

func (s *GameService) handleWerewolfFinish(c *Client, env Envelope) {
	var req struct {
		RoomID    string `json:"room_id"`
		Action    string `json:"action"`               // "vote" | "speak" | "start_day"
		TiedRound *int   `json:"tied_round,omitempty"` // 仅 vote
	}
	if err := json.Unmarshal(env.Payload, &req); err != nil || req.RoomID == "" {
		s.sendError(c, env.Seq, 20001, "invalid game.werewolf_finish payload")
		return
	}
	// §20260810-01 D6: 补齐观众拒绝守卫(原 LongCat §1 D6 缺陷)。
	// 所有其它 werewolf handler 均有 rejectIfSpectator,此处是唯一遗漏。
	// vote / start_day 分支不带 actor 校验,观众可任意触发阶段推进。
	if s.rejectIfSpectator(c, env, req.RoomID) {
		return
	}
	switch req.Action {
	case "vote":
		t := 1
		if req.TiedRound != nil {
			t = *req.TiedRound
		}
		if _, e := s.werewolfMgr.Action_FinishVote(req.RoomID, t); e != nil {
			s.sendError(c, env.Seq, e.Code, e.Message)
			return
		}
	case "speak":
		if _, e := s.werewolfMgr.Action_FinishSpeak(req.RoomID, c.UserID); e != nil {
			s.sendError(c, env.Seq, e.Code, e.Message)
			return
		}
	case "start_day":
		if _, e := s.werewolfMgr.Action_StartDay(req.RoomID); e != nil {
			s.sendError(c, env.Seq, e.Code, e.Message)
			return
		}
	default:
		s.sendError(c, env.Seq, 20001, "unknown finish action: "+req.Action)
		return
	}
	s.broadcastWerewolfState(req.RoomID)
	s.checkWerewolfGameOver(req.RoomID)
}

func (s *GameService) handleWerewolfRestartVote(c *Client, env Envelope) {
	var req struct {
		RoomID string `json:"room_id"`
		Choice string `json:"choice"` // "yes" | "no" | "abstain"
	}
	if err := json.Unmarshal(env.Payload, &req); err != nil || req.RoomID == "" {
		s.sendError(c, env.Seq, 20001, "invalid game.werewolf_restart_vote payload")
		return
	}
	if s.rejectIfSpectator(c, env, req.RoomID) {
		return
	}
	if _, e := s.werewolfMgr.Action_RestartVote(req.RoomID, c.UserID, req.Choice); e != nil {
		s.sendError(c, env.Seq, e.Code, e.Message)
		return
	}
	// 通知房间: action_accepted + 全员 broadcast state (含 RestartVote extra)。
	s.hub.BroadcastRoom(req.RoomID, Envelope{
		Type: "game.action_accepted",
		Payload: mustMarshal(map[string]any{
			"room_id":   req.RoomID,
			"game_kind": "werewolf",
			"by":        c.UserID,
			"action":    "restart_vote",
			"choice":    req.Choice,
		}),
	})
	s.broadcastWerewolfState(req.RoomID)
	// 同时也推送 restart_vote_update 帧(独立 from game.state),便于前端做轻量级
	// 投票进度动画,不必解析整个 ClientGameState。payload 仅含增量字段。
	if update, ok := s.werewolfMgr.RestartVoteSnapshot(req.RoomID); ok {
		s.hub.BroadcastRoom(req.RoomID, Envelope{
			Type:    "game.restart_vote_update",
			Payload: mustMarshal(update),
		})
	}
}

func (s *GameService) handleWerewolfFastRestart(c *Client, env Envelope) {
	var req struct {
		RoomID string `json:"room_id"`
	}
	if err := json.Unmarshal(env.Payload, &req); err != nil || req.RoomID == "" {
		s.sendError(c, env.Seq, 20001, "invalid game.werewolf_fast_restart payload")
		return
	}
	if s.rejectIfSpectator(c, env, req.RoomID) {
		return
	}
	if e := s.werewolfMgr.Action_FastRestartVote(req.RoomID, c.UserID); e != nil {
		s.sendError(c, env.Seq, e.Code, e.Message)
		return
	}
	s.hub.BroadcastRoom(req.RoomID, Envelope{
		Type: "game.action_accepted",
		Payload: mustMarshal(map[string]any{
			"room_id":   req.RoomID,
			"game_kind": "werewolf",
			"by":        c.UserID,
			"action":    "fast_restart",
		}),
	})
	s.broadcastWerewolfState(req.RoomID)
	if update, ok := s.werewolfMgr.RestartVoteSnapshot(req.RoomID); ok {
		s.hub.BroadcastRoom(req.RoomID, Envelope{
			Type:    "game.restart_vote_update",
			Payload: mustMarshal(update),
		})
	}
}

func (s *GameService) handleWerewolfSheriffStream(c *Client, env Envelope) {
	var req struct {
		RoomID string `json:"room_id"`
		Slot   int    `json:"slot"`   // 1 或 2
		Target *int   `json:"target"` // -1 撤回 | 0..11 声明目标
	}
	if err := json.Unmarshal(env.Payload, &req); err != nil || req.RoomID == "" {
		s.sendError(c, env.Seq, 20001, "invalid game.werewolf_sheriff_stream payload")
		return
	}
	if s.rejectIfSpectator(c, env, req.RoomID) {
		return
	}
	t := werewolf.NoSeat
	if req.Target != nil {
		t = werewolf.Seat(*req.Target)
	}
	if _, e := s.werewolfMgr.Action_SheriffStream(req.RoomID, c.UserID, req.Slot, t); e != nil {
		s.sendError(c, env.Seq, e.Code, e.Message)
		return
	}
	s.hub.BroadcastRoom(req.RoomID, Envelope{
		Type: "game.action_accepted",
		Payload: mustMarshal(map[string]any{
			"room_id":   req.RoomID,
			"game_kind": "werewolf",
			"by":        c.UserID,
			"action":    "sheriff_stream",
			"slot":      req.Slot,
			"target":    int(t),
		}),
	})
	s.broadcastWerewolfState(req.RoomID)
}

func (s *GameService) handleWerewolfIdiotReveal(c *Client, env Envelope) {
	var req struct {
		RoomID string `json:"room_id"`
		Choice string `json:"choice"` // "reveal" | "skip"
	}
	if err := json.Unmarshal(env.Payload, &req); err != nil || req.RoomID == "" {
		s.sendError(c, env.Seq, 20001, "invalid game.werewolf_idiot_reveal payload")
		return
	}
	if s.rejectIfSpectator(c, env, req.RoomID) {
		return
	}
	if _, e := s.werewolfMgr.Action_IdiotReveal(req.RoomID, c.UserID, req.Choice); e != nil {
		s.sendError(c, env.Seq, e.Code, e.Message)
		return
	}
	s.hub.BroadcastRoom(req.RoomID, Envelope{
		Type: "game.action_accepted",
		Payload: mustMarshal(map[string]any{
			"room_id":   req.RoomID,
			"game_kind": "werewolf",
			"by":        c.UserID,
			"action":    "idiot_reveal",
			"choice":    req.Choice,
		}),
	})
	s.broadcastWerewolfState(req.RoomID)
	s.checkWerewolfGameOver(req.RoomID)
}

func (s *GameService) handleWerewolfProposeVote(c *Client, env Envelope) {
	var req struct {
		RoomID string `json:"room_id"`
	}
	if err := json.Unmarshal(env.Payload, &req); err != nil || req.RoomID == "" {
		s.sendError(c, env.Seq, 20001, "invalid game.werewolf_propose_vote payload")
		return
	}
	if s.rejectIfSpectator(c, env, req.RoomID) {
		return
	}
	if _, e := s.werewolfMgr.Action_ProposeVote(req.RoomID, c.UserID); e != nil {
		s.sendError(c, env.Seq, e.Code, e.Message)
		return
	}
	s.hub.BroadcastRoom(req.RoomID, Envelope{
		Type: "game.action_accepted",
		Payload: mustMarshal(map[string]any{
			"room_id":   req.RoomID,
			"game_kind": "werewolf",
			"by":        c.UserID,
			"action":    "propose_vote",
		}),
	})
	s.broadcastWerewolfState(req.RoomID)
}

func (s *GameService) handleWerewolfLastWords(c *Client, env Envelope) {
	var req struct {
		RoomID string `json:"room_id"`
		Choice string `json:"choice"`         // "speak" | "skip"
		Text   string `json:"text,omitempty"` // 仅 speak 时需要
	}
	if err := json.Unmarshal(env.Payload, &req); err != nil || req.RoomID == "" {
		s.sendError(c, env.Seq, 20001, "invalid game.werewolf_last_words payload")
		return
	}
	if s.rejectIfSpectator(c, env, req.RoomID) {
		return
	}
	var (
		r  *werewolf.WerewolfRoom
		em *errcode.Error
	)
	switch req.Choice {
	case "speak":
		if req.Text == "" {
			s.sendError(c, env.Seq, errcode.ErrValidationFailed, "last_words text required")
			return
		}
		r, em = s.werewolfMgr.Action_LastWords(req.RoomID, c.UserID, req.Text)
	case "skip":
		r, em = s.werewolfMgr.Action_SkipLastWords(req.RoomID, c.UserID)
	default:
		s.sendError(c, env.Seq, errcode.ErrValidationFailed, "unknown last_words choice: "+req.Choice)
		return
	}
	if em != nil {
		s.sendError(c, env.Seq, em.Code, em.Message)
		return
	}
	_ = r // 未直接使用,广播时由 broadcastWerewolfState 拉最新视图。
	s.hub.BroadcastRoom(req.RoomID, Envelope{
		Type: "game.action_accepted",
		Payload: mustMarshal(map[string]any{
			"room_id":   req.RoomID,
			"game_kind": "werewolf",
			"by":        c.UserID,
			"action":    "last_words_" + req.Choice,
		}),
	})
	s.broadcastWerewolfState(req.RoomID)
	s.checkWerewolfGameOver(req.RoomID)
}

func (s *GameService) handleWerewolfUseProp(c *Client, env Envelope) {
	var req struct {
		RoomID  string `json:"room_id"`
		PropKey string `json:"prop_key"`
		Target  int    `json:"target"`
		Payload string `json:"payload,omitempty"`
	}
	if err := decodeJSONStrictFromBytes(env.Payload, &req); err != nil || req.RoomID == "" || req.PropKey == "" {
		s.sendError(c, env.Seq, errcode.ErrValidationFailed, "invalid game.werewolf_use_prop payload: room_id and prop_key required")
		return
	}
	if s.rejectIfSpectator(c, env, req.RoomID) {
		return
	}
	// payload 长度截断(对齐 agent_runner 路径的 200 字限制)。
	if len(req.Payload) > 200 {
		req.Payload = req.Payload[:200]
	}
	_, result, em := s.werewolfMgr.Action_UseProp(req.RoomID, c.UserID, req.PropKey, req.Target, req.Payload)
	if em != nil {
		s.sendError(c, env.Seq, em.Code, em.Message)
		return
	}
	s.hub.BroadcastRoom(req.RoomID, Envelope{
		Type: "game.action_accepted",
		Payload: mustMarshal(map[string]any{
			"room_id":    req.RoomID,
			"game_kind":  "werewolf",
			"by":         c.UserID,
			"action":     "use_prop",
			"prop_key":   req.PropKey,
			"target":     req.Target,
			"hit":        result.Hit,
			"price_paid": result.PricePaid,
		}),
	})
	s.sendOK(c, env.Seq, "use_prop", map[string]any{
		"prop_key":   req.PropKey,
		"hit":        result.Hit,
		"price_paid": result.PricePaid,
		"pot_return": result.PotReturn,
		"target":     req.Target,
	})
	s.broadcastWerewolfState(req.RoomID)
}

func (s *GameService) handleWerewolfPause(c *Client, env Envelope) {
	var req struct {
		RoomID string `json:"room_id"`
		Pause  bool   `json:"pause"`
		Reason string `json:"reason,omitempty"`
	}
	if err := decodeJSONStrictFromBytes(env.Payload, &req); err != nil || req.RoomID == "" {
		s.sendError(c, env.Seq, errcode.ErrValidationFailed, "invalid game.werewolf_pause payload: room_id required")
		return
	}
	if s.rejectIfSpectator(c, env, req.RoomID) {
		return
	}
	_, em := s.werewolfMgr.Action_Pause(req.RoomID, c.UserID, req.Pause, req.Reason)
	if em != nil {
		s.sendError(c, env.Seq, em.Code, em.Message)
		return
	}
	// Action_Pause 内部已调 broadcastGameStateLocked + broadcastWerewolfState,
	// 这里再保险推一次,确保真人 / 观战者立刻看到 ⏸ 标记。
	s.broadcastWerewolfState(req.RoomID)
	s.sendOK(c, env.Seq, "pause", map[string]any{
		"paused": req.Pause,
		"reason": req.Reason,
	})
}

// handleWerewolfCommit 处理人类玩家的公开承诺（§20260810-06）。
func (s *GameService) handleWerewolfCommit(c *Client, env Envelope) {
	var req struct {
		RoomID    string `json:"room_id"`
		Template  string `json:"template"`
		Target    int    `json:"target"`
		ParamText string `json:"param_text,omitempty"`
		Reason    string `json:"reason,omitempty"`
	}
	if err := decodeJSONStrictFromBytes(env.Payload, &req); err != nil || req.RoomID == "" || req.Template == "" {
		s.sendError(c, env.Seq, errcode.ErrValidationFailed, "invalid game.werewolf_commit payload: room_id and template required")
		return
	}
	if s.rejectIfSpectator(c, env, req.RoomID) {
		return
	}
	// 校验模板合法性
	template := werewolf.CommitTemplate(req.Template)
	if template != werewolf.CommitSeerCheck && template != werewolf.CommitVoteTarget &&
		template != werewolf.CommitNoVoteFor && template != werewolf.CommitNoUseSkill &&
		template != werewolf.CommitApologyIfGood {
		s.sendError(c, env.Seq, errcode.ErrValidationFailed, "invalid commitment template: "+req.Template)
		return
	}
	_, em := s.werewolfMgr.Action_PublicCommit(req.RoomID, c.UserID, template, req.Target, req.ParamText, req.Reason)
	if em != nil {
		s.sendError(c, env.Seq, em.Code, em.Message)
		return
	}
	s.hub.BroadcastRoom(req.RoomID, Envelope{
		Type: "game.action_accepted",
		Payload: mustMarshal(map[string]any{
			"room_id":   req.RoomID,
			"game_kind": "werewolf",
			"by":        c.UserID,
			"action":    "commit",
			"template":  req.Template,
			"target":    req.Target,
		}),
	})
	s.broadcastWerewolfState(req.RoomID)
}

func (s *GameService) broadcastWerewolfState(roomID string) {
	seats, ok := s.werewolfMgr.Seats(roomID)
	if !ok {
		return
	}
	for seat := 0; seat < werewolf.MaxPlayers; seat++ {
		userID := seats[seat]
		if userID == "" {
			continue
		}
		state := s.werewolfMgr.StateForSeat(roomID, werewolf.Seat(seat))
		if state == nil {
			continue
		}
		s.hub.BroadcastTo(userID, Envelope{Type: "game.state", Payload: mustMarshal(state)})
	}
	s.broadcastWerewolfSpectatorState(roomID)
	// BUG-WEREWOLF-P0-3 FIX: wake up bot agents so they react to the new phase.
	s.wakeWerewolfAgents(roomID, "state_change")
}

func (s *GameService) broadcastWerewolfSpectatorState(roomID string) {
	// BUG-WEREWOLF-P0-NEW-38 (Round 36): SpectateGame recreates an empty room
	// (r.State = NewGame(...) @ PhaseFilling) when the admin has force-disbanded
	// the live room and a spectator reconnects. Without this guard the
	// broadcastWerewolfSpectatorState path would push a fake
	// {phase:"filling", seats:[""x7]} frame to any spectator still subscribed —
	// directly contradicting GET /api/rooms/{id} which already reports the
	// latest live snap (phase=speak, current_count=7). The phantom "filling"
	// frame forces the spectator UI back onto the waiting-board spinner.
	//
	// The fix: only surface a game.state frame when the room actually hosts a
	// live game (≥1 seated player). Room-gone → skip (SpectatorView would
	// return nil anyway). Empty-recreated → stay silent; keep the last good
	// view in the spectator's browser.
	exists, live, phase := s.werewolfMgr.SpectatorRoomStatus(roomID)
	if !exists {
		logger.L().Debug("spectator state skipped: room not found",
			zap.String("room_id", roomID))
		return
	}
	if !live {
		logger.L().Debug("spectator state skipped: room not live (empty / recreated post-disband)",
			zap.String("room_id", roomID), zap.String("phase", phase))
		return
	}
	state := s.werewolfMgr.SpectatorView(roomID)
	if state == nil {
		logger.L().Debug("spectator state skipped: SpectatorView returned nil",
			zap.String("room_id", roomID))
		return
	}
	s.hub.BroadcastRoomSpectators(roomID, Envelope{Type: "game.state", Payload: mustMarshal(state)})
}

func (s *GameService) broadcastWerewolfSpecial(roomID, actorID, action string) {
	// 当前实现:夜晚信息已经通过 game.state 推送给每个座位(view 过滤)。
	// 客户端收到 game.state 后根据 my_role/my_turn 判断 UI 显示。
	// 保留此 hook 供将来扩展(比如夜间专属 inform 帧推送)。
	_ = roomID
	_ = actorID
	_ = action
}

func (s *GameService) checkWerewolfGameOver(roomID string) {
	room := s.werewolfMgr.SpectatorView(roomID) // 用 spectator 视图作快照
	if room == nil || room.Status != "over" {
		return
	}
	s.hub.BroadcastRoom(roomID, Envelope{
		Type: "game.over",
		Payload: mustMarshal(map[string]any{
			"room_id":   roomID,
			"game_kind": "werewolf",
			"winner":    room.Winner,
			"status":    room.Status,
			"phase":     room.Phase,
		}),
	})
}

// BroadcastWerewolfGameOverFinal 2026-07-30 解决和设计方案-20260730-03
// Fix-A1/A3/C: manager 经 onGameOverBroadcast 回调触发的终局收编广播。
//   1. per-seat game.state(phase=over, status=over, bot_contexts 已清场) +
//      观众 game.state;
//   2. game.over 帧(winner) — 让前端 gameOver.winner 有数据源,
//      game-over-banner 在所有终局路径都能渲染(此前 checkWerewolfGameOver
//      只在 6 个玩家动作路径被调,引擎 checkWinner + watchdog 推进的终局
//      从不广播 game.over)。
// 锁安全: 本函数在 manager 持 r.mu 的回调里被调;broadcastWerewolfState
// 内部走 StateForSeat/SpectatorView(lockRoomBriefly + cache fallback,
// 有界延迟,不会反向死锁),hub.Broadcast* 不碰引擎锁。
func (s *GameService) BroadcastWerewolfGameOverFinal(roomID, winner string) {
	s.broadcastWerewolfState(roomID)
	s.hub.BroadcastRoom(roomID, Envelope{
		Type: "game.over",
		Payload: mustMarshal(map[string]any{
			"room_id":   roomID,
			"game_kind": "werewolf",
			"winner":    winner,
			"status":    "over",
			"phase":     "over",
		}),
	})
	s.hub.BroadcastRoomSpectators(roomID, Envelope{
		Type: "game.over",
		Payload: mustMarshal(map[string]any{
			"room_id":   roomID,
			"game_kind": "werewolf",
			"winner":    winner,
			"status":    "over",
			"phase":     "over",
		}),
	})
}

// handleWerewolfBet 处理 game.werewolf_bet —— 观众押注竞猜(§20260812-02 U3)。
// 仅观战者可押注;玩家调用返回错误。
func (s *GameService) handleWerewolfBet(c *Client, env Envelope) {
	var req struct {
		RoomID     string `json:"room_id"`
		TargetSeat int    `json:"target_seat"`
		Amount     int    `json:"amount"`
	}
	if err := decodeJSONStrictFromBytes(env.Payload, &req); err != nil || req.RoomID == "" {
		s.sendError(c, env.Seq, errcode.ErrValidationFailed, "invalid game.werewolf_bet payload")
		return
	}
	// 获取座位数
	room, _ := s.werewolfMgr.Room(req.RoomID)
	if room == nil {
		s.sendError(c, env.Seq, errcode.ErrRoomNotFound, "room not found")
		return
	}
	seatCount := room.SeatCount
	if seatCount <= 0 {
		seatCount = werewolf.MaxPlayers
	}

	betID, betErr := s.werewolfMgr.PlaceSpectatorBet(req.RoomID, c.UserID, req.TargetSeat, req.Amount, seatCount)
	if betErr != nil {
		errMsg := "bet failed"
		if bErr, ok := betErr.(interface{ Error() string }); ok {
			errMsg = bErr.Error()
		}
		s.sendError(c, env.Seq, errcode.ErrValidationFailed, errMsg)
		return
	}
	s.sendOK(c, env.Seq, "bet_ack", map[string]any{
		"bet_id":      betID,
		"target_seat": req.TargetSeat,
		"amount":      req.Amount,
	})
}


