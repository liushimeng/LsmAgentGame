package ws

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"LsmAgentGame/agent/thpagent"
	"LsmAgentGame/errcode"
	"LsmAgentGame/game/chess"
	"LsmAgentGame/game/doudizhu"
	"LsmAgentGame/game/junqi"
	"LsmAgentGame/game/texasholdem"
	"LsmAgentGame/game/werewolf"
	"LsmAgentGame/game/xiangqi"
	"LsmAgentGame/llm"
	"LsmAgentGame/logger"
	"LsmAgentGame/service"
	"LsmAgentGame/util"

	"go.uber.org/zap"
)

// GameService handles game.* WebSocket frames.
//
// Each supported game kind has its own Manager (xiangqi.XiangqiManager,
// chess.ChessManager, junqi.JunqiManager, ...). The service routes game.* frames
// to the appropriate manager based on a `game_kind` field in payload.
//
// Backward compatibility: payloads without `game_kind` default to "xiangqi".
type GameService struct {
	hub            *Hub
	roomSvc        *service.RoomService
	xiangqiMgr     *xiangqi.XiangqiManager
	chessMgr       *chess.ChessManager
	junqiMgr       *junqi.JunqiManager
	doudizhuMgr    *doudizhu.DoudizhuManager
	texasHoldemMgr *texasholdem.TexasHoldemManager
	werewolfMgr    *werewolf.WerewolfManager
	// 2026-08-19 §德州扑克Agent — Bot 驱动器
	thpDriver      *thpagent.Driver
}

// NewGameService builds a GameService with the default set of managers.
//
// registry is the LLM provider registry used to drive agent seats in werewolf
// rooms. It may be nil — the game still runs, but agent seats behave as
// placeholders (no in-process bot). The registry is forwarded to the werewolf
// manager so Phase 4 can construct agent.Agent drivers per seat.
func NewGameService(hub *Hub, roomSvc *service.RoomService, registry *llm.Registry) *GameService {
	gs := &GameService{
		hub:            hub,
		roomSvc:        roomSvc,
		xiangqiMgr:     xiangqi.NewXiangqiManager(),
		chessMgr:       chess.NewChessManager(),
		junqiMgr:       junqi.NewJunqiManager(),
		doudizhuMgr:    doudizhu.NewDoudizhuManager(),
		texasHoldemMgr: texasholdem.NewTexasHoldemManager(),
		werewolfMgr:    werewolf.NewWerewolfManagerWithRegistry(registry),
	}
	// 2026-08-19 §德州扑克Agent — 初始化 Bot 驱动器
	gs.initTexasHoldemBotDriver(registry)
	return gs
}

// XiangqiManager exposes the xiangqi manager (used by other services).
func (s *GameService) XiangqiManager() *xiangqi.XiangqiManager {
	return s.xiangqiMgr
}

// ChessManager exposes the chess manager.
func (s *GameService) ChessManager() *chess.ChessManager {
	return s.chessMgr
}

// JunqiManager exposes the junqi manager.
func (s *GameService) JunqiManager() *junqi.JunqiManager {
	return s.junqiMgr
}

// DoudizhuManager exposes the doudizhu manager.
func (s *GameService) DoudizhuManager() *doudizhu.DoudizhuManager {
	return s.doudizhuMgr
}

// TexasHoldemManager exposes the texasholdem manager.
func (s *GameService) TexasHoldemManager() *texasholdem.TexasHoldemManager {
	return s.texasHoldemMgr
}

// WerewolfManager exposes the werewolf manager.
func (s *GameService) WerewolfManager() *werewolf.WerewolfManager {
	return s.werewolfMgr
}

// BroadcastCommentarySpectator 2026-08-11 §20260811-09 U1 — 把解说
// 帧(已序列化的 chat.commentary payload)推送给房间观战者,玩家收不到。
// 内部走 Hub.BroadcastRoomSpectators;由 werewolf 包经全局钩子调用。
func (s *GameService) BroadcastCommentarySpectator(roomID string, payload []byte) {
	if s.hub == nil {
		return
	}
	s.hub.BroadcastRoomSpectators(roomID, Envelope{
		Type:    "chat.commentary",
		Payload: payload,
	})
}

// WipeAllWerewolfRooms wipes the entire in-memory werewolf state on a clean
// shutdown / explicit kill and returns the list of room IDs that were torn
// down. The matching DB rows are NOT touched here — callers (main.go's
// shutdown handler or a boot cleanup pass) must invoke
// RoomService.ForceDisbandRoom per id to drop the t_lsm_game_player and
// t_lsm_game_room rows atomically. Returns an empty slice when the manager
// has no live rooms.
//
// BUG-WEREWOLF-RESTART-CLEANUP (Round 34): prior to this hook the SIGTERM
// path simply discarded the entire process; in-flight LLM calls (up to 8s)
// plus 1s/2s/4s backoff retries kept firing until the OS killed the
// process. Calling WipeAllRooms before httpsSrv.Shutdown lets cancel()
// reach every agent goroutine inside stopAgentsLocked (5s hard cap) so the
// process exits cleanly with no orphan LLM traffic.
func (s *GameService) WipeAllWerewolfRooms() []string {
	if s.werewolfMgr == nil {
		return nil
	}
	return s.werewolfMgr.WipeAllRooms()
}

// WerewolfRoomIDs returns the in-memory werewolf room IDs snapshot. Shutdown
// uses this to fan `game.removed` out to every still-connected client before
// the process exits, so they do not reconnect into a stale GameState.
func (s *GameService) WerewolfRoomIDs() []string {
	if s.werewolfMgr == nil {
		return nil
	}
	return s.werewolfMgr.RoomIDs()
}

// WerewolfPublicState adapts WerewolfManager.GetPublicState to the
// service.WerewolfStateHook interface, so RoomService.GetRoomDetail can
// surface phase/day/status/winner for REST clients (Round 23 P1 BUG FIX + R66
// defect 3.2 winner 透传). Returns ("", 0, "", "", false) when the room has no
// live in-memory state.
//
// 2026-07-16 BUG-R128-03 修复: 新增 status 返回值。in-memory Status 是权威
// 状态机视图(DB status 仅在 onGameOver 回调时同步,冷却期+restart_vote 阶段
// 会滞后),房间列表与详情接口必须使用 in-memory 值。
func (s *GameService) WerewolfPublicState(roomID string) (string, int, string, string, bool) {
	if s.werewolfMgr == nil {
		return "", 0, "", "", false
	}
	ps, ok := s.werewolfMgr.GetPublicState(roomID)
	if !ok {
		return "", 0, "", "", false
	}
	return ps.Phase, ps.Day, ps.Status, ps.Winner, true
}

// WerewolfPublicPlayerStates adapts WerewolfManager.GetPublicPlayerStates
// to the service.WerewolfStateHook interface, so RoomService.GetRoomDetail
// can merge in-memory per-seat public state (alive / role_revealed /
// death_cause / death_verdict / is_sheriff) into REST /api/rooms/{id}
// players[] response (R100 P1 BUG FIX). Returns nil when werewolfMgr is
// nil or the room has no live in-memory state — caller falls back to
// DB-only view (legacy behaviour).
//
// The service.WerewolfPublicPlayerState mirror exists because service
// cannot import werewolf (cycle: service ← werewolf ← service via
// record_log). We project the werewolf struct into the service-package
// shape field-by-field here.
func (s *GameService) WerewolfPublicPlayerStates(roomID string) []service.WerewolfPublicPlayerState {
	if s.werewolfMgr == nil {
		return nil
	}
	src := s.werewolfMgr.GetPublicPlayerStates(roomID)
	if len(src) == 0 {
		return nil
	}
	out := make([]service.WerewolfPublicPlayerState, 0, len(src))
	for _, p := range src {
		out = append(out, service.WerewolfPublicPlayerState{
			Seat:         p.Seat,
			UserID:       p.UserID,
			Alive:        p.Alive,
			RoleRevealed: p.RoleRevealed,
			Role:         p.Role,
			Faction:      p.Faction,
			DeathCause:   p.DeathCause,
			DeathVerdict: p.DeathVerdict,
			IsSheriff:    p.IsSheriff,
		})
	}
	return out
}

// RegisterAgentSeats mirrors the requested agent bot seats into the in-memory
// werewolf manager before the human creator joins. For each requested seat it
// (a) records the per-seat LLM model_key via SetSeatModelKey and (b) pre-fills
// the seat via ManagerAddPlayerAt so the engine's GameState.Players reflects
// "agent seats filled" before the human creator's SyncSeat call triggers
// auto-start at 7/7.
//
// This is the Phase 4 wiring that closes the P0 chain break reported in
// TestReport/自动化测试报告_20260706_161542.md: previously CreateRoomWithAgents
// persisted the DB rows but never called SetSeatModelKey / ManagerAddPlayerAt,
// so BotAgents stayed empty and Agent.Run was never started.
func (s *GameService) RegisterAgentSeats(gameKind, roomID string, seats []service.AgentSeatConfig) *errcode.Error {
	// 2026-08-19 §德州扑克Agent: texasholdem 分支
	if gameKind == "texasholdem" {
		return s.registerTexasHoldemAgentSeats(roomID, seats)
	}
	if gameKind != "werewolf" {
		return nil
	}
	if s.werewolfMgr == nil {
		return nil
	}
	// We need the bot userIDs that were just created in the DB. Re-derive them
	// from the room's player rows via the room service's DB handle. To avoid a
	// second DB round-trip, we accept the bot userIDs as part of the seat
	// config — but AgentSeatConfig only carries (seat, model_key). Instead we
	// look up the bot userIDs from the werewolf manager's Seats snapshot,
	// which is empty at this point (the manager hasn't seen the bots yet).
	//
	// Resolution: re-read the DB rows for this room to find the bot userIDs
	// matching the requested seats. This is a single SELECT and only runs at
	// room-creation time.
	for _, seatCfg := range seats {
		botUserID, err := s.botUserIDForSeat(roomID, seatCfg.Seat)
		if err != nil {
			logger.L().Warn("RegisterAgentSeats: resolve bot user failed",
				zap.String("room_id", roomID),
				zap.Int("seat", seatCfg.Seat),
				zap.Error(err))
			continue
		}
		if _, e := s.werewolfMgr.ManagerAddPlayerAt(roomID, botUserID, werewolf.Seat(seatCfg.Seat)); e != nil {
			logger.L().Warn("RegisterAgentSeats: ManagerAddPlayerAt failed",
				zap.String("room_id", roomID),
				zap.Int("seat", seatCfg.Seat),
				zap.Int("code", e.Code), zap.String("msg", e.Message))
			continue
		}
		s.werewolfMgr.SetSeatModelKey(roomID, seatCfg.Seat, seatCfg.ModelKey)
	}
	// 2026-07-10: 13 人标准竞技局兼容。按 game_kind 设置本局人数(默认 13;
	// werewolf_12 走 12 人;werewolf_7 走 7 人),驱动发牌 / IsReady 选择。
	//
	// §20260812-04 自动化补充:当 game_kind="werewolf"(默认 13 人)但恰好只注册了
	// 7 个 Agent 座位时,自动降级为 7 人局,否则 7 个 Agent 永远填不满 13 座、
	// ForceStartIfReady 永不触发,房间卡在 filling 阶段(自动化测试/截图 P0)。
	// 用户若想开 13 人局,请显式注册 ≥8 个 Agent 或让人类玩家补足。
	switch gameKind {
	case "werewolf_7":
		s.werewolfMgr.SetSeatCount(roomID, 7)
	case "werewolf_12":
		s.werewolfMgr.SetSeatCount(roomID, 12)
	default: // "werewolf" / "werewolf_13"
		if len(seats) == 7 {
			s.werewolfMgr.SetSeatCount(roomID, 7)
		} else {
			s.werewolfMgr.SetSeatCount(roomID, 13)
		}
	}
	// BUG-WEREWOLF-P0-1 FIX: when the agents collectively fill all 7 seats
	// (creator was downgraded to spectator), ManagerAddPlayerAt doesn't
	// auto-start the game the way JoinGame does. Kick the engine here so the
	// 7-AI "watch-only" room actually progresses.
	if started, _ := s.werewolfMgr.ForceStartIfReady(roomID); started {
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
		logger.L().Info("werewolf auto-started via HTTP room creation (full AI)",
			zap.String("room_id", roomID))
	}

	// 2026-08-05 §表情实时同步:agent 每次发布新 transcript(含 emotion_switch_speak
	// 切换情绪 + fx)后,经房间回调 broadcast game.state,前端座位卡 EmotionAvatar
	// 即时刷新表情 + fx 特效,无需等到下一阶段广播。该回调绑定房间对象,life-cycle
	// 跟随房间;房间解散时自动失效。
	if wr, ok := s.werewolfMgr.Room(roomID); ok {
		wr.SetOnTranscriptPublished(func(rid string) {
			s.broadcastWerewolfState(rid)
		})
	}
	return nil
}

// SetJudgeConfig 2026-07-16 主持人重构 — 实现 service.AgentSeater 接口,把房间级
// 法官设置透传到 in-memory WerewolfRoom。非 werewolf 房间忽略。
// 调用方必须在 RegisterAgentSeats 之前调用(BUG-R136-RACE-001)。
//
// 2026-07-30 §重构:加 mode 参数透传 JudgeConfig.Mode("agent"/"human");
// WerewolfRoom 内部归一化,两值都启用 AgentJudge LLM。
func (s *GameService) SetJudgeConfig(gameKind, roomID string, desired bool, mode string, modelKey string) *errcode.Error {
	if gameKind != "werewolf" {
		return nil
	}
	if s.werewolfMgr == nil {
		return nil
	}
	s.werewolfMgr.SetJudgeConfig(roomID, desired, mode, modelKey)
	return nil
}

// SetAgentDifficulty 2026-08-11 §20260811-09 U2 — 实现 service.AgentSeater 接口,
// 把房间级 Agent 难度分级透传到 in-memory WerewolfRoom。非 werewolf 房间忽略。
// 调用方必须在 RegisterAgentSeats 之前调用(同 SetJudgeConfig 时序约束)。
// difficulty: easy/normal/hard/hell;空 / 未知值由 manager 归一化为 normal(零回归)。
func (s *GameService) SetAgentDifficulty(gameKind, roomID string, difficulty string) *errcode.Error {
	if gameKind != "werewolf" {
		return nil
	}
	if s.werewolfMgr == nil {
		return nil
	}
	s.werewolfMgr.SetAgentDifficulty(roomID, difficulty)
	return nil
}

// SetCommentaryConfig 2026-08-11 §20260811-09 U1 — 实现 service.AgentSeater
// 接口,把房间级 AI 解说配置透传到 in-memory WerewolfRoom。非 werewolf 房间忽略。
// 调用方必须在 RegisterAgentSeats 之前调用(同 SetJudgeConfig 时序约束)。
// 内部把 service.CommentaryConfig(本包 ws 适配)投影到 werewolf.CommentaryConfig
// 后再调 manager,避免 service → werewolf 直接 import 循环。
func (s *GameService) SetCommentaryConfig(gameKind, roomID string, cfg *service.CommentaryConfig) *errcode.Error {
	if gameKind != "werewolf" {
		return nil
	}
	if s.werewolfMgr == nil || cfg == nil {
		return nil
	}
	s.werewolfMgr.SetCommentaryConfig(roomID, &werewolf.CommentaryConfig{
		Enabled:  cfg.Enabled,
		Style:    cfg.Style,
		ModelKey: cfg.ModelKey,
	})
	return nil
}

// SetSeatRolePrefs 2026-08-06 §20260806-03 — 实现 service.AgentSeater 接口,
// 把座位级角色偏好透传到 in-memory WerewolfRoom。非 werewolf 房间忽略。
// 调用方必须在 RegisterAgentSeats 之前调用(同 SetJudgeConfig 的时序约束)。
func (s *GameService) SetSeatRolePrefs(gameKind, roomID string, prefs map[int]string, creatorPref string) *errcode.Error {
	if gameKind != "werewolf" {
		return nil
	}
	if s.werewolfMgr == nil {
		return nil
	}
	s.werewolfMgr.SetSeatRolePrefs(roomID, prefs, creatorPref)
	return nil
}

// botUserIDForSeat looks up the bot user row for a given room+seat. Bot users
// have account="bot_<room>_<seat>" per service.getOrCreateBotUserID.
func (s *GameService) botUserIDForSeat(roomID string, seat int) (string, error) {
	// Lazy import to avoid a cycle: service.RoomService is the owner of the DB
	// handle. We reach it through s.roomSvc via a small accessor.
	return s.roomSvc.BotUserIDForSeat(roomID, seat)
}

// ValidateAgentSeats implements service.AgentSeater. It checks every requested
// agent seat's model_key against the live LLM registry (the same registry that
// agent.NewWithRoom consults at werewolf agent-start time). Any key that is
// unknown, disabled, or still carrying the placeholder api_key fails the whole
// room-create request so the caller gets a precise 400 instead of a room where
// that seat silently degrades to human — the "11/13 seats unplayable" pattern
// reported in TestReport/自动化测试报告_20260712_000132 (R106 P0).
//
// Failure modes rejected:
//   - unknown model_key → registry has no such entry (caller typo or unregistered model)
//   - disabled model → admin turned the provider off via /api/admin/llm/providers/:id
//   - placeholder api_key → admin never replaced API-KEY-PLACEHOLDER
//
// A nil registry (legacy GameService constructed without LLM) skips validation
// and returns nil — this is the same degenerate state as
// WerewolfManager.Registry()==nil, where StartAgentsLocked is a no-op and the
// room is already understood to be placeholder-only. Callers that pass agent
// seats without wiring a registry get a loud startup log from
// service.CreateRoomWithAgents.
func (s *GameService) ValidateAgentSeats(seats []service.AgentSeatConfig) *errcode.Error {
	if len(seats) == 0 {
		return nil
	}
	if s.werewolfMgr == nil {
		return nil
	}
	reg := s.werewolfMgr.Registry()
	if reg == nil {
		return nil
	}
	var invalid []string
	for i := range seats {
		// R187-2: a client that copy-pasted a displayed model key containing
		// an invisible Cf rune (ZWSP/ZWNJ/ZWJ/BOM/soft hyphen) should still
		// match the registry's clean key. Sanitize in place so downstream
		// consumers (agent seat allocation, error message) see the clean key.
		seats[i].ModelKey = util.SanitizeModelKey(seats[i].ModelKey)
		if !reg.IsAvailable(seats[i].ModelKey) {
			invalid = append(invalid, seats[i].ModelKey)
		}
	}
	if len(invalid) > 0 {
		// R187-2: also list the currently available model keys so API clients
		// (and test automation) can auto-retry with a corrected model_key
		// instead of guessing. errcode.Error is code+message only (no extra
		// data field), so the list is appended to the message string in a
		// machine-parseable `available_models: [...]` tail.
		//
		// Semantics must match IsAvailable exactly (enabled && available &&
		// non-placeholder api_key). ListEnabled only filters `enabled` and
		// List includes everything, so neither is sufficient on its own —
		// we re-filter ListEnabled's key-free ModelInfo rows through
		// IsAvailable. ModelInfo is key-free by contract (agent_name /
		// model / provider_type only), so no api_key can leak.
		available := make([]string, 0, 8)
		for _, info := range reg.ListEnabled() {
			if reg.IsAvailable(info.Model) {
				available = append(available, info.Model)
			}
		}
		return errcode.CodeMsg(errcode.ErrValidationFailed,
			fmt.Sprintf("agent seat model_key not available (unknown / disabled / placeholder api_key): %s; available_models: [%s]",
				strings.Join(invalid, ", "), strings.Join(available, ", ")))
	}
	return nil
}

// IsModelAvailable implements service.ModelAvailabilityHook (R187-2). It
// reports whether a model_key is currently usable per the live LLM registry
// (registered && enabled && non-placeholder api_key) so RoomService's
// random duplicate-reassignment allocator never picks a runtime-disabled
// model. Returns false when no registry is wired (legacy/test wiring) —
// RoomService treats a nil hook differently (skips the filter entirely),
// whereas a wired-but-registry-less GameService conservatively reports
// unavailable.
func (s *GameService) IsModelAvailable(modelKey string) bool {
	if s.werewolfMgr == nil {
		return false
	}
	reg := s.werewolfMgr.Registry()
	if reg == nil {
		return false
	}
	return reg.IsAvailable(modelKey)
}

// WerewolfFillingRoomSnapshot implements service.GameServiceAPI (R187-1).
// Projects the in-memory werewolf manager's filling-phase room snapshot
// into the service-package mirror shape.
func (s *GameService) WerewolfFillingRoomSnapshot() []service.WerewolfFillingRoomInfo {
	if s.werewolfMgr == nil {
		return nil
	}
	src := s.werewolfMgr.FillingRoomSnapshot()
	if len(src) == 0 {
		return nil
	}
	out := make([]service.WerewolfFillingRoomInfo, 0, len(src))
	for _, info := range src {
		out = append(out, service.WerewolfFillingRoomInfo{
			RoomID:        info.RoomID,
			Phase:         info.Phase,
			CreatedAt:     info.CreatedAt,
			OccupiedSeats: info.OccupiedSeats,
			Spectators:    info.Spectators,
		})
	}
	return out
}

// ForceCloseWerewolfFillingRoom implements service.GameServiceAPI (R187-1).
func (s *GameService) ForceCloseWerewolfFillingRoom(roomID string) bool {
	if s.werewolfMgr == nil {
		return false
	}
	return s.werewolfMgr.ForceCloseFillingRoom(roomID)
}

// SyncSeat mirrors a DB-level seat (from HTTP CreateRoom/JoinRoom) into the
// matching in-memory game manager so the auto-deal / auto-start path fires
// even when players join via HTTP instead of WS game.join. For 2-player
// board games this is a no-op (their managers key off WS join only); for
// doudizhu (3) and texasholdem (6) this is what triggers the deal when the
// room reaches capacity.
//
// Returns (true, nil) when this seat caused the game to auto-start.
func (s *GameService) SyncSeat(gameKind, roomID, userID string) (bool, *errcode.Error) {
	switch gameKind {
	case "doudizhu":
		room, started, e := s.doudizhuMgr.JoinGame(roomID, userID)
		if e != nil {
			return false, e
		}
		if started {
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
			logger.L().Info("doudizhu auto-started via HTTP join",
				zap.String("room_id", roomID))
		} else {
			s.hub.BroadcastRoom(roomID, Envelope{
				Type: "game.peer_joined",
				Payload: mustMarshal(map[string]any{
					"room_id":      roomID,
					"game_kind":    "doudizhu",
					"ready":        room.IsReady(),
					"player_count": room.Occupied(),
				}),
			})
		}
		return started, nil
	case "texasholdem":
		room, started, e := s.texasHoldemMgr.JoinGame(roomID, userID)
		if e != nil {
			return false, e
		}
		if started {
			s.hub.BroadcastRoom(roomID, Envelope{
				Type: "game.started",
				Payload: mustMarshal(map[string]any{
					"room_id":   roomID,
					"game_kind": "texasholdem",
					"phase":     "preflop",
					"ready":     true,
				}),
			})
			s.broadcastTexasHoldemState(roomID)
			logger.L().Info("texasholdem auto-started via HTTP join",
				zap.String("room_id", roomID))
		} else {
			s.hub.BroadcastRoom(roomID, Envelope{
				Type: "game.peer_joined",
				Payload: mustMarshal(map[string]any{
					"room_id":      roomID,
					"game_kind":    "texasholdem",
					"ready":        room.IsReady(),
					"player_count": room.Occupied(),
				}),
			})
		}
		return started, nil
	case "werewolf":
		// 狼人杀在 WS join 时入座(同上,需要 7 人满才自动 StartGame)
		room, started, e := s.werewolfMgr.JoinGame(roomID, userID)
		if e != nil {
			return false, e
		}
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
			logger.L().Info("werewolf auto-started via HTTP join",
				zap.String("room_id", roomID))
		} else {
			s.hub.BroadcastRoom(roomID, Envelope{
				Type: "game.peer_joined",
				Payload: mustMarshal(map[string]any{
					"room_id":      roomID,
					"game_kind":    "werewolf",
					"ready":        room.IsReady(),
					"player_count": room.Occupied(),
				}),
			})
		}
		return started, nil
	}
	// xiangqi / chess / junqi：WS join 才触发对局，HTTP join 无需同步。
	return false, nil
}

// RemoveRoomState clears in-memory game state for the given room across all
// game managers. Called by the Hub when a room is auto-deleted after a long
// vacancy, and is safe to call when no game is active for the room.
func (s *GameService) RemoveRoomState(roomID string) {
	s.xiangqiMgr.RemoveGame(roomID)
	s.chessMgr.RemoveGame(roomID)
	s.junqiMgr.RemoveGame(roomID)
	s.doudizhuMgr.RemoveGame(roomID)
	s.texasHoldemMgr.RemoveGame(roomID)
	s.werewolfMgr.RemoveGame(roomID)
}

// BroadcastRoomRemoved fans a single `game.removed` envelope out to every
// player AND every spectator currently subscribed to `roomID`, then
// detaches them from the per-room broadcast sets so the client-side WS
// layer drops the room from its local state.
//
// Used by ForceDisbandRoom (admin "kill switch") and
// BootCleanupOrphanedAgentRooms (orphan-agent reconciler). The payload
// is intentionally tiny — clients only need to know the room is gone
// and why, not the full RoomDetail snapshot.
//
// Safe to call when nobody is subscribed (the two broadcast helpers are
// no-ops on empty sets). Safe to call when the room never existed in the
// hub (same: empty sets).
//
// Detach order matters: the broadcasts are enqueued FIRST (so subscribers
// receive the frame before being unsubscribed), then the unsubscribes
// happen. If we reversed the order, a client could receive the next
// `game.state` push mid-teardown and overwrite the `game.removed` signal
// with stale data.
func (s *GameService) BroadcastRoomRemoved(roomID, reason string) {
	if s.hub == nil || roomID == "" {
		return
	}
	env := Envelope{
		Type: "game.removed",
		Payload: mustMarshal(map[string]any{
			"room_id":    roomID,
			"reason":     reason,
			"removed_at": time.Now().UTC().Format(time.RFC3339Nano),
		}),
	}
	// Both helpers run under hub.mu.RLock; they hold no write lock, so we
	// can safely call them back-to-back.
	s.hub.BroadcastRoomIncludingSpectators(roomID, env)
	s.hub.UnsubscribeRoomAll(roomID)
}

// gameKindFromPayload extracts the `game_kind` field from a JSON payload.
// Returns ("xiangqi", true) by default when not specified or invalid.
func gameKindFromPayload(payload json.RawMessage) string {
	if len(payload) == 0 {
		return "xiangqi"
	}
	var probe struct {
		GameKind string `json:"game_kind"`
	}
	if err := json.Unmarshal(payload, &probe); err != nil {
		return "xiangqi"
	}
	gk := strings.ToLower(strings.TrimSpace(probe.GameKind))
	switch gk {
	case "chess", "international_chess":
		return "chess"
	case "junqi", "army_chess", "luezhanqi":
		return "junqi"
	case "doudizhu", "landlord":
		return "doudizhu"
	case "texasholdem", "poker", "holdem":
		return "texasholdem"
	case "werewolf", "wolf", "mafia":
		return "werewolf"
	}
	return "xiangqi"
}

// resolveSpectateGameKind 决定观战(game.spectate)路由时应使用的权威
// game_kind。优先级:
//  1. payload 显式携带的 game_kind(规范化后);
//  2. 反查 t_lsm_game_room.game_kind(roomSvc.GameKindOf);
//  3. 兜底 "xiangqi"(payload 无 game_kind 且房间不存在/无记录)。
//
// BUG-R118-02 (2026-07-14): 旧逻辑在 payload 未带 game_kind 时直接回退到
// "xiangqi",即使房间实际是狼人杀;导致 `game.spectated` 响应内 game_kind
// 错乱、前端加载错误 UI。反查 DB 获取权威 game_kind 是根本修复。
func resolveSpectateGameKind(roomID string, payload json.RawMessage, roomSvc *service.RoomService) string {
	gk := strings.ToLower(strings.TrimSpace(gameKindFromCanonical(payload)))
	if gk != "" && gk != "xiangqi" {
		// payload 显式指明了具体游戏(非默认占位),直接使用。
		return gk
	}
	// payload 未带 game_kind 或仅含不可靠默认值:反查房间记录获取权威值。
	if fromDB := roomSvc.GameKindOf(roomID); fromDB != "" {
		return strings.ToLower(strings.TrimSpace(fromDB))
	}
	return "xiangqi"
}

// gameKindFromCanonical 从 payload 提取并规范化 game_kind:显式指定时返回
// 对应规范 id,否则返回空字符串(区别于 gameKindFromPayload 的 "xiangqi"
// 默认)。用于 resolveSpectateGameKind 区分"显式指定"与"未指定"。
func gameKindFromCanonical(payload json.RawMessage) string {
	if len(payload) == 0 {
		return ""
	}
	var probe struct {
		GameKind string `json:"game_kind"`
	}
	if err := json.Unmarshal(payload, &probe); err != nil {
		return ""
	}
	gk := strings.ToLower(strings.TrimSpace(probe.GameKind))
	switch gk {
	case "xiangqi":
		return "xiangqi"
	case "chess", "international_chess":
		return "chess"
	case "junqi", "army_chess", "luezhanqi":
		return "junqi"
	case "doudizhu", "landlord":
		return "doudizhu"
	case "texasholdem", "poker", "holdem":
		return "texasholdem"
	case "werewolf", "wolf", "mafia":
		return "werewolf"
	}
	return ""
}

// roomOnlyPayload extracts just the room_id field for operations that don't
// require game-specific routing (e.g., resign, leave, get-state).
type roomOnlyPayload struct {
	RoomID   string `json:"room_id"`
	GameKind string `json:"game_kind,omitempty"`
}

// HandleClientFrame routes a game.* frame to the appropriate handler.
func (s *GameService) HandleClientFrame(c *Client, env Envelope) {
	switch env.Type {
	case "game.join":
		s.handleJoin(c, env)
	case "game.leave":
		s.handleLeave(c, env)
	case "game.move":
		s.handleMove(c, env)
	case "game.resign":
		s.handleResign(c, env)
	case "game.state":
		s.handleGetState(c, env)
	case "game.promote":
		s.handlePromote(c, env)
	case "game.layout":
		s.handleJunqiLayout(c, env)
	case "game.bid":
		s.handleDoudizhuBid(c, env)
	case "game.play":
		s.handleDoudizhuPlay(c, env)
	case "game.pass":
		s.handleDoudizhuPass(c, env)
	case "game.action":
		s.handleTexasHoldemAction(c, env)
	case "game.werewolf_action":
		s.handleWerewolfAction(c, env)
	case "game.werewolf_vote":
		s.handleWerewolfVote(c, env)
	case "game.werewolf_suicide":
		s.handleWerewolfSuicide(c, env)
	case "game.werewolf_shoot":
		s.handleWerewolfShoot(c, env)
	case "game.werewolf_sheriff":
		s.handleWerewolfSheriff(c, env)
	case "game.werewolf_finish":
		s.handleWerewolfFinish(c, env)
	case "game.werewolf_restart_vote":
		s.handleWerewolfRestartVote(c, env)
	case "game.werewolf_fast_restart":
		s.handleWerewolfFastRestart(c, env)
	case "game.werewolf_sheriff_stream":
		// 13 人标准竞技局:预言家警长声明 / 撤回警徽流(docs/狼人杀13人标准局规则.md §7)。
		s.handleWerewolfSheriffStream(c, env)
	case "game.werewolf_idiot_reveal":
		// 13 人标准竞技局:白痴翻牌结算(docs/狼人杀13人标准局规则.md §3.5)。
		s.handleWerewolfIdiotReveal(c, env)
	case "game.werewolf_propose_vote":
		// 2026-07-11: 预言家发起投票。
		s.handleWerewolfProposeVote(c, env)
	case "game.werewolf_last_words":
		// 2026-07-21 §人类玩家操作重构: 人类遗言提交 / 放弃。
		// bot 仍走 agentRunner.LastWords / LastWordsSkip 路径;人类通过此帧入口。
		s.handleWerewolfLastWords(c, env)
	case "game.werewolf_use_prop":
		// 2026-07-21 §道具系统: 人类玩家道具使用入口。
		// 严格 JSON 校验 (§84b DisallowUnknownFields),bot 仍走 agentRunner.UseProp 路径。
		s.handleWerewolfUseProp(c, env)
	case "game.werewolf_pause":
		// 2026-07-24 优化:UI 暂停/恢复房间。
		// 仅房主可调用;pause=true 暂停(不调 LLM/watchdog skip),pause=false 恢复。
		// reason 可选(给其他玩家解释为什么暂停)。
		s.handleWerewolfPause(c, env)
	case "game.werewolf_commit":
		// 2026-08-10 §20260810-06: 人类玩家公开承诺入口。
		s.handleWerewolfCommit(c, env)
	case "game.werewolf_bet":
		// §20260812-02 U3: 观众押注竞猜。
		s.handleWerewolfBet(c, env)
	case "game.spectate":
		s.handleSpectate(c, env)
	case "game.unspectate":
		s.handleUnspectate(c, env)
	case "game.spectators":
		s.handleSpectatorsList(c, env)
	default:
		s.sendError(c, env.Seq, 20001, "unknown game message type: "+env.Type)
	}
}

// isSpectatorOf reports whether the client is currently registered as a
// spectator of the room via the hub's separate spectator set. The check is
// the single source of truth for "may this client send game input?"
func (s *GameService) isSpectatorOf(roomID string, c *Client) bool {
	return s.hub.IsSpectatorOf(roomID, c)
}

// rejectIfSpectator returns a permission-denied error if the calling client
// is currently registered as a spectator of the room. Use at the top of every
// game input handler (move / bid / play / pass / action / layout / resign /
// promote).
func (s *GameService) rejectIfSpectator(c *Client, env Envelope, roomID string) bool {
	if s.isSpectatorOf(roomID, c) {
		s.sendError(c, env.Seq, errcode.ErrSpectatorInputForbidden, "")
		return true
	}
	return false
}

// ─────────────────── join ───────────────────




// ─────────────────── move ───────────────────




// ─────────────────── promote ───────────────────


// ─────────────────── resign / leave / state ───────────────────




// ─────────────────── junqi (中国军棋) ───────────────────

// handleJunqiLayout handles a game.layout frame — a player submits their 25-piece layout.



// ─────────────────── doudizhu (斗地主) ───────────────────

// handleDoudizhuJoin 入座并按座位推送可见状态。





// sendDoudizhuState 向单个客户端发送该座位可见的对局状态。

// broadcastDoudizhuState 向房间内每个玩家单独推送该座位的可见状态（手牌按人过滤）。

// broadcastDoudizhuOver 广播对局结束。

// ─────────────────── texasholdem (德州扑克) ───────────────────

// handleTexasHoldemJoin 入座并按座位推送可见状态。

// handleTexasHoldemAction 处理 game.action 帧。

// sendTexasHoldemState 向单个客户端发送该座位可见的对局状态。

// broadcastTexasHoldemState 向房间内每个玩家单独推送该座位的可见状态（手牌按人过滤）。

// broadcastTexasHoldemOver 广播对局结束。

// ─────────────────── Spectate / unspectate / list ───────────────────

// spectatePayload is parsed once by every per-kind handler.
type spectatePayload struct {
	RoomID   string `json:"room_id"`
	GameKind string `json:"game_kind,omitempty"`
	// Mode is honored by junqi only; "hidden" (default) or "open".
	Mode string `json:"mode,omitempty"`
}

func (s *GameService) handleSpectate(c *Client, env Envelope) {
	var req spectatePayload
	if err := json.Unmarshal(env.Payload, &req); err != nil || req.RoomID == "" {
		s.sendError(c, env.Seq, 20001, "invalid game.spectate payload")
		return
	}
	gk := resolveSpectateGameKind(req.RoomID, env.Payload, s.roomSvc)

	switch gk {
	case "xiangqi":
		room, e := s.xiangqiMgr.SpectateGame(req.RoomID, c.UserID)
		if e != nil {
			s.sendError(c, env.Seq, e.Code, e.Message)
			return
		}
		s.hub.SpectateRoom(req.RoomID, c)
		s.sendOK(c, env.Seq, "game.spectated", map[string]any{
			"room_id":   req.RoomID,
			"game_kind": "xiangqi",
			"role":      "spectator",
			"ready":     room.IsReady(),
		})
		s.broadcastXiangqiSpectatorState(req.RoomID)
		logger.L().Info("spectator attached", zap.String("kind", "xiangqi"),
			zap.String("room_id", req.RoomID), zap.String("user_id", c.UserID))

	case "chess":
		room, e := s.chessMgr.SpectateGame(req.RoomID, c.UserID)
		if e != nil {
			s.sendError(c, env.Seq, e.Code, e.Message)
			return
		}
		s.hub.SpectateRoom(req.RoomID, c)
		s.sendOK(c, env.Seq, "game.spectated", map[string]any{
			"room_id":   req.RoomID,
			"game_kind": "chess",
			"role":      "spectator",
			"ready":     room.IsReady(),
		})
		s.broadcastChessSpectatorState(req.RoomID)
		logger.L().Info("spectator attached", zap.String("kind", "chess"),
			zap.String("room_id", req.RoomID), zap.String("user_id", c.UserID))

	case "junqi":
		mode := junqi.ParseVisibilityMode(req.Mode)
		room, e := s.junqiMgr.SpectateGame(req.RoomID, c.UserID, mode)
		if e != nil {
			s.sendError(c, env.Seq, e.Code, e.Message)
			return
		}
		s.hub.SpectateRoom(req.RoomID, c)
		s.sendOK(c, env.Seq, "game.spectated", map[string]any{
			"room_id":   req.RoomID,
			"game_kind": "junqi",
			"role":      "spectator",
			"mode":      room.Mode.String(),
			"ready":     room.IsReady(),
		})
		s.broadcastJunqiSpectatorState(req.RoomID)
		logger.L().Info("spectator attached", zap.String("kind", "junqi"),
			zap.String("room_id", req.RoomID), zap.String("user_id", c.UserID))

	case "doudizhu":
		room, e := s.doudizhuMgr.SpectateGame(req.RoomID, c.UserID)
		if e != nil {
			s.sendError(c, env.Seq, e.Code, e.Message)
			return
		}
		s.hub.SpectateRoom(req.RoomID, c)
		phase := PhaseBiddingStr
		if room.State != nil {
			phase = room.State.Phase.String()
		}
		s.sendOK(c, env.Seq, "game.spectated", map[string]any{
			"room_id":   req.RoomID,
			"game_kind": "doudizhu",
			"role":      "spectator",
			"phase":     phase,
			"ready":     room.IsReady(),
		})
		s.broadcastDoudizhuSpectatorState(req.RoomID)
		logger.L().Info("spectator attached", zap.String("kind", "doudizhu"),
			zap.String("room_id", req.RoomID), zap.String("user_id", c.UserID))

	case "texasholdem":
		room, e := s.texasHoldemMgr.SpectateGame(req.RoomID, c.UserID)
		if e != nil {
			s.sendError(c, env.Seq, e.Code, e.Message)
			return
		}
		s.hub.SpectateRoom(req.RoomID, c)
		phase := "waiting"
		if room.State != nil {
			phase = room.State.Street.String()
		}
		s.sendOK(c, env.Seq, "game.spectated", map[string]any{
			"room_id":   req.RoomID,
			"game_kind": "texasholdem",
			"role":      "spectator",
			"phase":     phase,
			"ready":     room.IsReady(),
		})
		s.broadcastTexasHoldemSpectatorState(req.RoomID)
		logger.L().Info("spectator attached", zap.String("kind", "texasholdem"),
			zap.String("room_id", req.RoomID), zap.String("user_id", c.UserID))

	case "werewolf":
		room, e := s.werewolfMgr.SpectateGame(req.RoomID, c.UserID)
		if e != nil {
			s.sendError(c, env.Seq, e.Code, e.Message)
			return
		}
		s.hub.SpectateRoom(req.RoomID, c)
		phase := "filling"
		if room.State != nil {
			phase = room.State.Phase.String()
		}
		s.sendOK(c, env.Seq, "game.spectated", map[string]any{
			"room_id":   req.RoomID,
			"game_kind": "werewolf",
			"role":      "spectator",
			"phase":     phase,
			"ready":     room.IsReady(),
		})
		s.broadcastWerewolfSpectatorState(req.RoomID)
		logger.L().Info("spectator attached", zap.String("kind", "werewolf"),
			zap.String("room_id", req.RoomID), zap.String("user_id", c.UserID))

	default:
		s.sendError(c, env.Seq, 20001, "unsupported game_kind: "+gk)
	}
}

func (s *GameService) handleUnspectate(c *Client, env Envelope) {
	var req struct {
		RoomID   string `json:"room_id"`
		GameKind string `json:"game_kind,omitempty"`
	}
	if err := json.Unmarshal(env.Payload, &req); err != nil || req.RoomID == "" {
		s.sendError(c, env.Seq, 20001, "invalid game.unspectate payload")
		return
	}
	gk := gameKindFromPayload(env.Payload)

	switch gk {
	case "xiangqi":
		_ = s.xiangqiMgr.UnspectateGame(req.RoomID, c.UserID)
	case "chess":
		_ = s.chessMgr.UnspectateGame(req.RoomID, c.UserID)
	case "junqi":
		_ = s.junqiMgr.UnspectateGame(req.RoomID, c.UserID)
	case "doudizhu":
		_ = s.doudizhuMgr.UnspectateGame(req.RoomID, c.UserID)
	case "texasholdem":
		_ = s.texasHoldemMgr.UnspectateGame(req.RoomID, c.UserID)
	case "werewolf":
		_ = s.werewolfMgr.UnspectateGame(req.RoomID, c.UserID)
	}
	s.hub.UnspectateRoom(req.RoomID, c)
	s.sendOK(c, env.Seq, "game.unspectated", map[string]any{
		"room_id":   req.RoomID,
		"game_kind": gk,
	})
	logger.L().Info("spectator detached",
		zap.String("room_id", req.RoomID),
		zap.String("user_id", c.UserID))
}

func (s *GameService) handleSpectatorsList(c *Client, env Envelope) {
	var req struct {
		RoomID   string `json:"room_id"`
		GameKind string `json:"game_kind,omitempty"`
	}
	if err := json.Unmarshal(env.Payload, &req); err != nil || req.RoomID == "" {
		s.sendError(c, env.Seq, 20001, "invalid game.spectators payload")
		return
	}
	gk := gameKindFromPayload(env.Payload)
	var ids []string
	switch gk {
	case "xiangqi":
		ids = s.xiangqiMgr.SpectatorList(req.RoomID)
	case "chess":
		ids = s.chessMgr.SpectatorList(req.RoomID)
	case "junqi":
		ids = s.junqiMgr.SpectatorList(req.RoomID)
	case "doudizhu":
		ids = s.doudizhuMgr.SpectatorList(req.RoomID)
	case "texasholdem":
		ids = s.texasHoldemMgr.SpectatorList(req.RoomID)
	case "werewolf":
		ids = s.werewolfMgr.SpectatorList(req.RoomID)
	}
	ws := s.hub.RoomSpectatorCount(req.RoomID)
	// Reconcile: the in-memory list is the authoritative "subscribed" set; the
	// registered list is a superset of currently-connected users. We expose the
	// connected count + IDs to the caller; the registered-IDs list is kept
	// for future auditing.
	_ = ids
	payload, _ := json.Marshal(map[string]any{
		"room_id":   req.RoomID,
		"game_kind": gk,
		"count":     ws,
	})
	c.send <- Envelope{Type: "game.spectators_resp", Seq: env.Seq, Payload: payload}
}

// ─────────────────── Per-game spectator state broadcast ───────────────────
//
// These helpers are called whenever a game's authoritative state changes
// (move / layout / bid / play / pass / action / end-of-hand). They rebuild the
// sanitized view for every currently-connected spectator and fan it out via
// Hub.BroadcastRoomSpectators — which never reaches players.


func (s *GameService) broadcastChessSpectatorState(roomID string) {
	uids := s.hub.connectedSpectatorUserIDs(roomID)
	for _, uid := range uids {
		state, e := s.chessMgr.SpectatorState(roomID, uid)
		if e != nil {
			continue
		}
		s.hub.BroadcastTo(uid, Envelope{Type: "game.state", Payload: mustMarshal(state)})
	}
}




// ─────────────────── Helpers ───────────────────

func (s *GameService) sendOK(c *Client, seq int64, msgType string, payload any) {
	c.send <- Envelope{Type: msgType, Seq: seq, Payload: mustMarshal(payload)}
}

func (s *GameService) sendError(c *Client, seq int64, code int, msg string) {
	c.send <- Envelope{Type: "game.error", Seq: seq, Payload: mustMarshal(map[string]any{
		"code": code, "message": msg,
	})}
}

func mustMarshal(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return data
}

// MustMarshal 是 mustMarshal 的导出版本,供 ws 包外的接线代码(main.go 钩子)使用。
// 失败时返回空 JSON 对象,避免接线处二次处理错误。
func MustMarshal(v any) json.RawMessage {
	return mustMarshal(v)
}

// leaveRoomQuiet synchronously removes the player's row from the DB after a
// game.leave or game.resign. Errors of code ErrRoomNotIn are swallowed (the
// player row was already cleaned up by the hub's 15s disconnect timer or the
// janitor — second removal is expected and benign). All other errors are
// logged but do not propagate to the WS client: leave/resign are best-effort
// cleanup, never a reason to fail the user's UI.
//
// service.LeaveRoom is idempotent on the (room, user) pair AND automatically
// deletes the room when current_count drops to <=1, so this single call covers
// the "delete player row" + "delete room row if last player" combo without
// needing any extra branch here.
func (s *GameService) leaveRoomQuiet(roomID, userID string) {
	if s.roomSvc == nil {
		return
	}
	if e := s.roomSvc.LeaveRoom(roomID, userID); e != nil && e.Code != errcode.ErrRoomNotIn {
		logger.L().Warn("game leave/resign → roomSvc.LeaveRoom failed",
			zap.String("room_id", roomID),
			zap.String("user_id", userID),
			zap.Int("code", e.Code))
		return
	}
	// 通知大厅:座位释放 / 房间可能已被删。
	if s.hub != nil {
		s.hub.NotifyRoomChanged(roomID, "player_left", userID)
	}
}

// ─────────────────── werewolf (狼人杀 7 人标准局) ───────────────────

// handleWerewolfJoin 入座并按座位推送可见状态。

// werewolfActionPayload 是 game.werewolf_action 的通用 payload。

// handleWerewolfAction 处理夜晚动作(狼刀 / 预言家查验 / 女巫用药)。

// wakeWerewolfAgents fans a wakeup event out to every bot agent in the room.
// The caller may pass a fallback phase; if the engine state is readable we use
// the real phase so each agent's GameContext.MyTurn is computed correctly.

// handleWerewolfVote 处理白天投票 / 警长投票。

// handleWerewolfSuicide 处理狼人自爆。

// handleWerewolfShoot 处理猎人开枪(target=-1 = 不开枪)。

// handleWerewolfSheriff 参选/选举(由 action 字段区分)。

// handleWerewolfFinish 触发投票完成 / 发言完成(由 GM/计时器触发)。

// handleWerewolfRestartVote 处理人类玩家的"重开局投票"输入 (2026-07-10)。
//
// 客户端→服务端:
//
//	{ "room_id": "uuid", "choice": "yes|no|abstain" }
//
// 通过 Action_RestartVote → manager.restartVoteLocked 内部已自动评估 quorum
// 并在 passed 时调用 restartGameLocked。所以这里只需要广播一次 state 即可,
// 后续由 watchdog 5s tick 维持 deadline。

// handleWerewolfSheriffStream 处理 13 人标准竞技局警徽流声明 / 撤回( docs/狼人杀13人标准局规则.md §7 )。
// 客户端→服务端:
//
//	{ "room_id": "uuid", "slot": 1|2, "target": -1|0..11 }
//
// 仅 seat==SheriffSeat 且 role==seer 的玩家可声明(动作服务端再校验);
// 观战者不可操作。target=-1 表示撤回该槽位。

// handleWerewolfIdiotReveal 处理 13 人标准竞技局白痴翻牌结算( docs/狼人杀13人标准局规则.md §3.5 )。
// 客户端→服务端:
//
//	{ "room_id": "uuid", "choice": "reveal" | "skip" }
//
// 仅 PhaseIdiotReveal 的最高票存活白痴可操作;观战者不可操作。
// reveal → 翻牌免死(失去投票权,继续发言);skip → 正常放逐(有遗言)。

// handleWerewolfProposeVote 2026-07-11: 预言家在白天发言阶段发起投票。

// handleWerewolfLastWords 2026-07-21 §人类玩家操作重构:
// 人类玩家在死亡遗言阶段(PhaseDeathLyric)提交遗言或放弃。
// 客户端→服务端:
//
//	{ "room_id": "uuid", "choice": "speak"|"skip", "text": "..." }
//
// 仅人类死者(DeathLyricCurrent==mySeat)可调用;观战者被 rejectIfSpectator 拒;
// bot 仍走 agentRunner.LastWords / LastWordsSkip 路径,不会经过此帧。
// 服务端校验在 Action_LastWords / Action_SkipLastWords 内部完成
// (死亡 / DeathLyricCurrent / text 非空 等),这里只做 WS 帧转 Action_* 翻译。

// handleWerewolfUseProp 处理 2026-07-21 §道具系统新增的人类玩家 WS 帧
// `game.werewolf_use_prop`。bot 仍走 agentRunner.UseProp 路径,通过
// agent 工具 `use_prop` 派发;人类通过此帧经 WerewolfManager.Action_UseProp
// 走完全一致的引擎 + 钱包 + 广播路径(单一真相源)。
//
// 严格 JSON 校验 (§84b):用 decodeJSONStrictFromBytes + DisallowUnknownFields
// 拒绝拼写错误字段(prop_id vs propID 等)以免静默丢字段。
// payload 字段:
//   - room_id  string(必填)
//   - prop_key string(必填)
//   - target   int(可选;>= 0 表示目标座位;-1 = AOE)
//   - payload  string(可选;自定义文本)
//
// 观战者由 rejectIfSpectator 统一拒绝(ErrSpectatorInputForbidden)。

// 2026-07-24 优化 — UI 暂停/恢复房间(仅房主)。
//
// 客户端→服务端: {"type":"game.werewolf_pause","payload":{"room_id":"...","pause":true,"reason":"..."}}
// 服务端→客户端: game.state 广播 + game.action_accepted 帧。
//
// 设计目标:防止上游 LLM 代理批量 4xx/429 时继续调 LLM 把所有 bot 送进
// quarantine。真人玩家在 ⏸ 后房间内所有 bot 停止调 LLM,阶段时钟冻结,
// watchdog 不再强制 skip。▶ 恢复后立即推进。

// broadcastWerewolfState 向房间每个玩家单独推送该座位的可见视图。
//
// 副作用：每次广播 state 都会唤醒该房间内的所有 bot agent。Agent.runLoop
// 阻塞在 evCh 上直到收到事件——少了这一步狼刀/预言家/女巫永远不会
// 行动（BUG-WEREWOLF-P0-3）。


// broadcastWerewolfSpecial 在夜晚动作后,向夜晚专属角色额外推送神职情报。

// checkWerewolfGameOver 检查对局是否结束,若是则广播 game.over。

// ─────────────────── Proto 消息注册 ───────────────────
//
// 游戏服务的 proto 消息注册在此。各游戏专属帧在各自的 game_service_*.go 中注册。
// 当前为占位实现，后续逐步迁移。

// registerProtoMessages 在 proto 路由器中注册游戏服务的通用 proto 消息
func (s *GameService) registerProtoMessages(reg *ProtoRegistry) {
	// TODO: 迁移 game.join / game.leave / game.resign / game.state 等通用帧
	// TODO: 各游戏专属帧在 game_service_xiangqi.go / game_service_werewolf.go 等文件中注册
}
