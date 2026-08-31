// Package ws — 辩论比赛 WS 帧派发(2026-08-31 §20260831-01)。
//
// 端点对齐 docs/辩论比赛/00 §4.2:
//
//	C → S: debate.subscribe / debate.unsubscribe / debate.spectator_question / debate.like
//	S → C: debate.state / debate.phase / debate.speech / debate.cross_exam / debate.score
//	      debate.judge_vote / debate.game_over / debate.agent_thought / debate.error
//
// 派发:本服务只关心订阅/取消订阅与少量旁路帧;agent action 由 Agent driver
// 走 in-process 通道(与狼人杀一致,见 werewolf game_service.go §"Phase 4 wiring")。
//
// 设计参考:
//   - ws/game_service_werewolf.go(WerewolfManager Action_* 派发)
//   - ws/handler.go(WS 升级)
//   - ws/hub.go(BroadcastRoom / BroadcastRoomSpectators)
package ws

import (
	"encoding/json"
	"sync"

	"LsmAgentGame/errcode"
	"LsmAgentGame/game/debate"
	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// DebateService 处理 debate.* WS 帧。
//
// 设计:本服务只解析客户端订阅动作,真正发言由 Bot driver 内部触发;
// 观众可发送 chat.* / debate.like / debate.spectator_question(供裁判参考)。
type DebateService struct {
	hub *Hub
	mgr *debate.DebateManager

	mu       sync.RWMutex
	enabled  bool
}

// NewDebateService 创建 DebateService。
func NewDebateService(hub *Hub, mgr *debate.DebateManager) *DebateService {
	return &DebateService{
		hub:     hub,
		mgr:     mgr,
		enabled: true,
	}
}

// Manager 返回 manager(供外部 Agent driver 触发 Bot 派发)。
func (s *DebateService) Manager() *debate.DebateManager { return s.mgr }

// SetEnabled 启用 / 禁用(供测试)。
func (s *DebateService) SetEnabled(v bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.enabled = v
}

// HandleClientFrame 派发客户端 → 服务端 debate.* 帧。
func (s *DebateService) HandleClientFrame(c *Client, env Envelope) {
	s.mu.RLock()
	enabled := s.enabled
	s.mu.RUnlock()
	if !enabled {
		s.sendError(c, env.Seq, errcode.ErrInternal, "debate service disabled")
		return
	}

	switch env.Type {
	case "debate.subscribe":
		s.handleSubscribe(c, env)
	case "debate.unsubscribe":
		s.handleUnsubscribe(c, env)
	case "debate.spectator_question":
		s.handleSpectatorQuestion(c, env)
	case "debate.like":
		s.handleLike(c, env)
	default:
		// 未知帧:不处理(由 hub 兜底)
	}
}

// handleSubscribe 客户端订阅某房间辩论流。
func (s *DebateService) handleSubscribe(c *Client, env Envelope) {
	var payload struct {
		RoomID string `json:"room_id"`
	}
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		s.sendError(c, env.Seq, errcode.ErrValidationFailed, "invalid payload")
		return
	}
	if payload.RoomID == "" {
		s.sendError(c, env.Seq, errcode.ErrValidationFailed, "room_id required")
		return
	}

	r, ok := s.mgr.Get(payload.RoomID)
	if !ok {
		s.sendError(c, env.Seq, errcode.ErrRoomNotFound, "debate room not found")
		return
	}

	// 标记为该房间的观战者
	r.AddSpectator(c.UserID, debate.SpectatorKindViewer)

	// 加入 hub 的房间订阅集合
	s.hub.SubscribeRoom(payload.RoomID, c)

	// 立即推送一次 state
	state := r.BuildClientState(c.UserID, true, r.IsGameOver())
	payloadBytes, _ := json.Marshal(state)
	s.hub.SendToUser(c.UserID, Envelope{
		Type:    "debate.state",
		Payload: payloadBytes,
	})

	s.sendOK(c, env.Seq, "debate.subscribed", map[string]any{"room_id": payload.RoomID})
}

// handleUnsubscribe 取消订阅。
func (s *DebateService) handleUnsubscribe(c *Client, env Envelope) {
	var payload struct {
		RoomID string `json:"room_id"`
	}
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		s.sendError(c, env.Seq, errcode.ErrValidationFailed, "invalid payload")
		return
	}
	if payloadRoomID := payload.RoomID; payloadRoomID != "" {
		s.hub.UnsubscribeRoom(payloadRoomID, c)
		if r, ok := s.mgr.Get(payloadRoomID); ok {
			r.RemoveSpectator(c.UserID)
		}
	}
	s.sendOK(c, env.Seq, "debate.unsubscribed", nil)
}

// handleSpectatorQuestion 观众向裁判提问。
//
// 本期实现:把提问追加到房间的"提问队列",由裁判 Agent 在评审阶段读取。
func (s *DebateService) handleSpectatorQuestion(c *Client, env Envelope) {
	var payload struct {
		RoomID string `json:"room_id"`
		Text   string `json:"text"`
	}
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		s.sendError(c, env.Seq, errcode.ErrValidationFailed, "invalid payload")
		return
	}
	if payload.Text == "" {
		s.sendError(c, env.Seq, errcode.ErrValidationFailed, "text required")
		return
	}
	r, ok := s.mgr.Get(payload.RoomID)
	if !ok {
		s.sendError(c, env.Seq, errcode.ErrRoomNotFound, "debate room not found")
		return
	}
	if !r.Config.SpectatorConfig.AllowSpectatorQuestion {
		s.sendError(c, env.Seq, errcode.ErrPermissionDenied, "spectator question disabled")
		return
	}

	// 广播给裁判(通过 hub)
	data, _ := json.Marshal(map[string]any{
		"room_id":   payload.RoomID,
		"user_id":   c.UserID,
		"text":      payload.Text,
		"timestamp": debate.WallNowMS(),
	})
	s.hub.BroadcastRoomSpectators(payload.RoomID, Envelope{
		Type:    "debate.spectator_question",
		Payload: data,
	})
	logger.L().Info("debate spectator question",
		zap.String("room_id", payload.RoomID),
		zap.String("user_id", c.UserID))
}

// handleLike 点赞发言(仅展示用,不影响比赛)。
func (s *DebateService) handleLike(c *Client, env Envelope) {
	var payload struct {
		RoomID   string `json:"room_id"`
		SpeechID string `json:"speech_id"`
	}
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		s.sendError(c, env.Seq, errcode.ErrValidationFailed, "invalid payload")
		return
	}
	// 广播给其他观众(展示用)
	data, _ := json.Marshal(map[string]any{
		"room_id":   payload.RoomID,
		"speech_id": payload.SpeechID,
		"user_id":   c.UserID,
	})
	s.hub.BroadcastRoomSpectators(payload.RoomID, Envelope{
		Type:    "debate.like",
		Payload: data,
	})
}

// ============================================================================
// 广播辅助(供 DebateManager / DebateEngine 调用)
// ============================================================================

// BroadcastSpeech 把发言广播给房间(由 DebateEngine 在 SubmitSpeech 后调用)。
func (s *DebateService) BroadcastSpeech(roomID string, speech debate.Speech) {
	payload, _ := json.Marshal(map[string]any{
		"room_id":  roomID,
		"speech":   speech,
		"phase":    speech.Phase,
		"team_id":  speech.TeamID,
		"seat":     speech.Seat,
		"stance":   speech.Stance,
		"speaker":  speech.SpeakerName,
		"content":  speech.Content,
		"timestamp": speech.Timestamp,
	})
	s.hub.BroadcastRoom(roomID, Envelope{
		Type:    "debate.speech",
		Payload: payload,
	})
}

// BroadcastCrossExam 广播质询条目。
func (s *DebateService) BroadcastCrossExam(roomID string, entry debate.CrossExamEntry) {
	payload, _ := json.Marshal(map[string]any{
		"room_id":   roomID,
		"cross_exam": entry,
	})
	s.hub.BroadcastRoom(roomID, Envelope{
		Type:    "debate.cross_exam",
		Payload: payload,
	})
}

// BroadcastPhase 广播阶段变化。
func (s *DebateService) BroadcastPhase(roomID string, phase debate.Phase, timeRemaining int) {
	payload, _ := json.Marshal(map[string]any{
		"room_id":         roomID,
		"phase":           phase,
		"phase_cn":        debate.PhaseCN(phase),
		"time_remaining_sec": timeRemaining,
	})
	s.hub.BroadcastRoom(roomID, Envelope{
		Type:    "debate.phase",
		Payload: payload,
	})
}

// BroadcastJudgeScore 广播单裁判评分。
func (s *DebateService) BroadcastJudgeScore(roomID string, score debate.JudgeScore) {
	payload, _ := json.Marshal(map[string]any{
		"room_id":     roomID,
		"judge_id":    score.JudgeID,
		"model_key":   score.ModelKey,
		"rankings":    score.Rankings,
		"winner_team_id": score.WinnerTeamID,
		"is_fallback": score.IsFallback,
		"overall_comment": score.OverallComment,
	})
	s.hub.BroadcastRoom(roomID, Envelope{
		Type:    "debate.judge_vote",
		Payload: payload,
	})
}

// BroadcastResult 广播最终结果。
func (s *DebateService) BroadcastResult(roomID string, result *debate.DebateResult) {
	if result == nil {
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"room_id": roomID,
		"result":  result,
	})
	s.hub.BroadcastRoom(roomID, Envelope{
		Type:    "debate.game_over",
		Payload: payload,
	})
}

// BroadcastState 广播房间 state(阶段切换、计时刷新时调用)。
func (s *DebateService) BroadcastState(roomID string, state *debate.ClientState) {
	if state == nil {
		return
	}
	payload, _ := json.Marshal(state)
	s.hub.BroadcastRoom(roomID, Envelope{
		Type:    "debate.state",
		Payload: payload,
	})
}

// ============================================================================
// helpers
// ============================================================================

func (s *DebateService) sendOK(c *Client, seq int64, msgType string, data any) {
	payload := []byte("{}")
	if data != nil {
		payload, _ = json.Marshal(data)
	}
	c.Send(Envelope{
		Seq:     seq,
		Type:    msgType,
		Payload: payload,
	})
}

func (s *DebateService) sendError(c *Client, seq int64, code int, msg string) {
	c.Send(Envelope{
		Seq:  seq,
		Type: "debate.error",
		Payload: mustMarshal(errcode.Error{
			Code:    code,
			Message: msg,
		}),
	})
}