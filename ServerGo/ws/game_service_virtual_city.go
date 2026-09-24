// Package ws — game_service_virtual_city.go: 虚拟城市 WS 帧处理
// (2026-09-14 §财商流P0)。
//
// 契约: 协议契约文档 §1–§2(帧总表)+ §3(game.state 载荷)+ §4(动作语义);
// §19.5 观战者硬拒;handler 在锁内做轻量校验,把引擎动作委派给
// GameService.vcMgr(房间 → engine in-process 路径)。
package ws

import (
	"encoding/json"
	"strconv"
	"time"

	"LsmAgentGame/errcode"
	"LsmAgentGame/game/virtual_city"
	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// ─────────────────── GameService 上的财富流状态 ───────────────────

// handleVirtualCityJoin 处理 game.join 帧(wealth;动作 semantics 在房间层)。
func (s *GameService) handleVirtualCityJoin(c *Client, env Envelope, roomID string) {
	r := s.vcMgr.Get(roomID)
	if r == nil {
		r = s.vcMgr.CreateRoom(roomID)
	}
	// 自动开局判定:满 8 座即开。
	started := false
	seat, full, e := r.JoinGame(c.UserID, "")
	if e != nil {
		s.sendError(c, env.Seq, e.Code, e.Message)
		return
	}
	s.hub.SubscribeRoom(roomID, c)
	// 自动开:8 满或 room.Status==open → Start(Start 内检查 ≥3)。
	if full || (r.Occupied() >= virtual_city.MinSeats && r.GetStatus() == virtual_city.StatusOpen) {
		if e := s.startVirtualCityRoom(roomID); e == nil {
			started = true
		} else {
			logger.L().Warn("wealth auto start failed",
				zap.String("room_id", roomID), zap.Error(e))
		}
	}

	snap := r.SnapshotStatus()
	respPayload := map[string]any{
		"room_id":   roomID,
		"game_kind": "virtual_city",
		"my_seat":   seat,
		"month":     snap.Month,
		"phase":     snap.Phase,
	}
	if started {
		snap := r.SnapshotStatus()
		respPayload["phase"] = snap.Phase
		respPayload["month"] = snap.Month
		respPayload["status"] = virtual_city.StatusPlaying
	}
	s.sendOK(c, env.Seq, "game.joined", respPayload)

	// 状态推送(若已开始,推 game.state 单发)。
	if started {
		s.broadcastVirtualCityState(roomID)
	}
}

// handleVirtualCityAction 处理 game.virtual_city_action。
func (s *GameService) handleVirtualCityAction(c *Client, env Envelope) {
	var req struct {
		RoomID string        `json:"room_id"`
		Action virtual_city.Action `json:"action"`
	}
	if err := json.Unmarshal(env.Payload, &req); err != nil || req.RoomID == "" {
		s.sendError(c, env.Seq, errcode.ErrValidationFailed, "invalid game.virtual_city_action payload")
		return
	}
	if s.rejectIfSpectator(c, env, req.RoomID) {
		return
	}
	r := s.vcMgr.Get(req.RoomID)
	if r == nil {
		s.sendError(c, env.Seq, errcode.ErrVirtualCityRoomNotFound, "")
		return
	}
	seat, ok := r.SeatOf(c.UserID)
	if !ok {
		s.sendError(c, env.Seq, errcode.ErrRoomNotIn, "")
		return
	}
	// 锁内校验/执行(房间层 ApplyActionForSeat 包装)。
	text, e := s.applyVirtualCityAction(r, seat, req.Action)
	if e != nil {
		// 失败:游戏内事件 error 通告(seat-scoped)。
		r.EnqueueEventForUI(virtual_city.EventRecord{Month: r.Month(), Type: "error", Seat: seat, Text: e.Message})
		s.hub.SendToUser(c.UserID, wsEnvelope("game.error", env.Seq, map[string]any{"code": e.Code, "message": e.Message}))
		return
	}
	// 成功广播 game.event(action)+ game.state 单发刷新。
	if text != "" {
		s.hub.BroadcastRoomIncludingSpectators(req.RoomID, wsEnvelope("game.event", 0, map[string]any{
			"room_id":   req.RoomID,
			"game_kind": "virtual_city",
			"month":     r.Month(),
			"seat":      seat,
			"type":      "action",
			"text":      text,
		}))
	}
	s.broadcastVirtualCityState(req.RoomID)
}

// applyVirtualCityAction 应用动作并发送 bot_contexts 更新(锁定释放,§92a)。
func (s *GameService) applyVirtualCityAction(r *virtual_city.VirtualCityRoom, seat int, a virtual_city.Action) (string, *errcode.Error) {
	// 房间持锁校验/执行;返回文本 + 错误。
	// 2026-09-14 §财商流P0-bugfix: 持锁期间严禁调 GetStatus()/GetPhase()/Engine()
	// (三者内部各自再锁 r.mu,sync.Mutex 不可重入 → 自死锁,曾致整房卡死)。
	// Status/Phase 是导出字段,持锁下直接读;引擎用 EngineLocked()。
	r.MuLock()
	defer r.MuUnlock()
	// 通用校验(房间层做;错误码统一)。
	if r.Status != virtual_city.StatusPlaying {
		return "", errcode.Code(errcode.ErrVirtualCityNotPlaying)
	}
	if r.Phase != virtual_city.PhaseActing {
		return "", errcode.Code(errcode.ErrVirtualCityWrongPhase)
	}
	text, e := r.EngineLocked().ApplyAction(seat, a)
	if e != nil {
		return "", e
	}
	// 提交检查。
	if a.Type == virtual_city.ActSubmitMonth {
		_ = r.SubmitMonthLocked(seat)
		if r.AllSubmittedLocked() {
			r.SignalSettle()
		}
	}
	// 同步 bot_contexts(若 seat 实际由 bot 接管 — 此路径人类玩家不进)。
	return text, nil
}

// handleVirtualCityStart 处理 game.virtual_city_start 帧(房主提前开局)。
func (s *GameService) handleVirtualCityStart(c *Client, env Envelope) {
	var req struct {
		RoomID string `json:"room_id"`
	}
	if err := json.Unmarshal(env.Payload, &req); err != nil || req.RoomID == "" {
		s.sendError(c, env.Seq, errcode.ErrValidationFailed, "invalid game.virtual_city_start payload")
		return
	}
	r := s.vcMgr.Get(req.RoomID)
	if r == nil {
		s.sendError(c, env.Seq, errcode.ErrVirtualCityRoomNotFound, "")
		return
	}
	if e := s.startVirtualCityRoom(req.RoomID); e != nil {
		s.sendError(c, env.Seq, e.Code, e.Message)
		return
	}
	logger.L().Info("virtual_city room started by owner",
		zap.String("room_id", req.RoomID), zap.String("owner", c.UserID))
}

// startVirtualCityRoom 内部入口(autostart / 房主 start / 全 agent 开局)。
func (s *GameService) startVirtualCityRoom(roomID string) *errcode.Error {
	r := s.vcMgr.Get(roomID)
	if r == nil {
		return errcode.Code(errcode.ErrVirtualCityRoomNotFound)
	}
	// 注入 ws 广播钩子(锁外回调,§92a)。
	r.SetHooks(virtual_city.BroadcastHooks{
		OnEvent:  s.broadcastVirtualCityEvent,
		OnMonth:  s.broadcastVirtualCityMonthly,
		OnState:  s.broadcastVirtualCityState,
		OnOver:   s.broadcastVirtualCityOver,
		OnSurvey: s.broadcastVirtualCitySurvey,
		OnStarted: func(rid string, payload map[string]any) {
			s.hub.BroadcastRoomIncludingSpectators(rid, wsEnvelope("game.started", 0, payload))
		},
	})
	// chat sender 注入。
	r.SetChatSender(&wealthChatSender{chat: s.chatSvc})
	// 先装配 bot agents,再 Start。Start() 末尾会立即 wakeBots;若装配晚于
	// Start,首月 wake 时 agents map 仍为空,12 个 bot 将错过 M1 决策。
	s.vcMgr.EnsureAgents(r)
	if err := r.Start(s.vcLoader); err != nil {
		return err
	}
	// 标记 DB 房间状态为 playing(开局成功回调路径,锁外执行 §92a)。
	// 此前仅 werewolf/debate 接线;P1-Ghost-03: wealth 永远 open,大厅
	// 在终局内存房被清理后仍显示可加入,触发「幽灵房」重建。
	if s.roomSvc != nil {
		if e := s.roomSvc.UpdateRoomStatus(roomID, "playing"); e != nil {
			logger.L().Warn("virtual_city room status -> playing failed",
				zap.String("room_id", roomID), zap.Error(e))
		}
	}
	// 启动 runLoop goroutine(单房间)。
	go r.RunLoop(func(rid string) {
		// 60s 后清理(终局 cleanup,§14 game.removed)。
		time.AfterFunc(60*time.Second, func() {
			s.vcMgr.RemoveRoom(rid)
			s.hub.UnsubscribeRoomAll(rid)
			s.BroadcastRoomRemoved(rid, "game-over")
			// 同步 DB 状态为 over(§P1-Ghost-03:终局后 DB 残留 'open',
			// 大厅列表误显可加入)。锁外执行,失败仅记日志不中断 cleanup。
			if s.roomSvc != nil {
				if e := s.roomSvc.UpdateRoomStatus(rid, "over"); e != nil {
					logger.L().Warn("virtual_city room status -> over failed",
						zap.String("room_id", rid), zap.Error(e))
				}
			}
		})
	})
	// 启动 watchdog。
	r.StartWatchdog(func() {
		r.SignalSettle()
	})
	// 广播 game.started(通过 OnStarted hook — 已在线 ws hub 不直接接收,
	// 由房间广播 game.state 时连带 game.started 帧)。
	s.hub.BroadcastRoomIncludingSpectators(roomID, wsEnvelope("game.started", 0, map[string]any{
		"room_id":   roomID,
		"game_kind": "virtual_city",
		"month":     1,
		"age":       r.Age(),
		"start_age": r.Age(),
		"phase":     virtual_city.PhaseActing,
		"ready":     true,
	}))
	s.broadcastVirtualCityState(roomID)
	return nil
}

// handleVirtualCityPause 处理 game.virtual_city_pause(月结边界生效,§14 B8)。
func (s *GameService) handleVirtualCityPause(c *Client, env Envelope) {
	var req struct {
		RoomID string `json:"room_id"`
		Pause  bool   `json:"pause"`
		Reason string `json:"reason,omitempty"`
	}
	if err := json.Unmarshal(env.Payload, &req); err != nil || req.RoomID == "" {
		s.sendError(c, env.Seq, errcode.ErrValidationFailed, "invalid game.virtual_city_pause payload")
		return
	}
	r := s.vcMgr.Get(req.RoomID)
	if r == nil {
		s.sendError(c, env.Seq, errcode.ErrVirtualCityRoomNotFound, "")
		return
	}
	if err := r.Pause(c.UserID, req.Pause); err != nil {
		s.sendError(c, env.Seq, err.Code, err.Message)
		return
	}
	s.hub.BroadcastRoomIncludingSpectators(req.RoomID, wsEnvelope("game.virtual_city_paused", 0, map[string]any{
		"room_id": req.RoomID, "pause": req.Pause, "reason": req.Reason,
	}))
}

// broadcastVirtualCityState 逐座位单发(脱敏)+ 观战者推送(协议 §3,§7)。
func (s *GameService) broadcastVirtualCityState(roomID string) {
	r := s.vcMgr.Get(roomID)
	if r == nil {
		return
	}
	seats := r.SnapshotSeats()
	nicks := r.SnapshotNicknames()
	bots := r.SnapshotBotSeats()
	models := r.SnapshotModelKeys()
	transcripts := r.SnapshotTranscripts()
	world := r.Engine()
	gameStartedAt := r.GameStartedAtUnix()
	nextMonth := r.NextMonthAtUnix()
	citySnap := r.CitySnapshotView() // 2026-09-21 §虚拟城市:城市背景层快照(未建城 nil)
	// 玩家座位单发。
	for seat := 0; seat < virtual_city.MaxSeats; seat++ {
		uid := seats[seat]
		if uid == "" {
			continue
		}
		cs := virtual_city.BuildClientState(roomID, seat, world, seats, nicks, bots, models, transcripts, gameStartedAt, nextMonth, citySnap)
		s.hub.BroadcastTo(uid, wsEnvelope("game.state", 0, cs))
	}
	// 观战者(viewer = -1)。
	cs := virtual_city.BuildClientState(roomID, -1, world, seats, nicks, bots, models, transcripts, gameStartedAt, nextMonth, citySnap)
	for _, uid := range s.hub.connectedSpectatorUserIDs(roomID) {
		s.hub.BroadcastTo(uid, wsEnvelope("game.state", 0, cs))
	}
}

// broadcastVirtualCityMonth 广播 game.month(全房同帧,§2)。
func (s *GameService) broadcastVirtualCityMonth(roomID string, res *virtual_city.SettleResult) {
	if res == nil {
		return
	}
	summaries := make([]map[string]any, 0, len(res.Summaries))
	for _, s := range res.Summaries {
		summaries = append(summaries, map[string]any{
			"seat": s.Seat, "cash_delta": s.CashDelta, "net_worth": s.NetWorth,
			"fi_index": s.FIIndex, "note": s.Note,
		})
	}
	payload := map[string]any{
		"room_id":   roomID,
		"game_kind": "virtual_city",
		"month":     res.Month,
		"age":       res.Age,
		"summaries": summaries,
		"market_changes": map[string]any{
			"stock_index": res.StockIndex, "gold_price": res.GoldPrice,
			"bond_rate": res.BondRate, "house_idx": res.HouseIdx,
			// P1(§财商流P1-2 §6.3):cpi = 篮子 CPIYoY(回退时 CB 理论值)。
			"cpi": res.CPI, "unemployment_rate": res.UnemploymentRate,
		},
		"events": res.Events,
	}
	s.hub.BroadcastRoomIncludingSpectators(roomID, wsEnvelope("game.month", 0, payload))
}

// broadcastVirtualCityOver 广播 game.over。
func (s *GameService) broadcastVirtualCityOver(roomID string, scores []virtual_city.FinalScore) {
	out := make([]map[string]any, 0, len(scores))
	reports := make(map[int]string, len(scores))
	for _, s := range scores {
		out = append(out, map[string]any{
			"seat": s.Seat, "fi_score": s.FIScore,
			"life_score": s.LifeScore, "social_score": s.SocialScore,
			"total": s.Total, "ending": s.Ending,
		})
		reports[s.Seat] = s.Report
	}
	s.hub.BroadcastRoomIncludingSpectators(roomID, wsEnvelope("game.over", 0, map[string]any{
		"room_id":   roomID,
		"game_kind": "virtual_city",
		"scores":    out,
		"reports":   reports,
	}))
}

// broadcastVirtualCityEvent 广播 game.event 单条事件。
func (s *GameService) broadcastVirtualCityEvent(roomID string, ev virtual_city.EventRecord) {
	s.hub.BroadcastRoomIncludingSpectators(roomID, wsEnvelope("game.event", 0, map[string]any{
		"room_id": roomID, "game_kind": "virtual_city",
		"month": ev.Month, "seat": ev.Seat, "type": ev.Type, "text": ev.Text,
	}))
}

// broadcastVirtualCitySurvey 广播 game.survey_result(P1 §财商流P1-2 调研契约 §4.4;
// 由 BroadcastHooks.OnSurvey 在锁外触发:deadline 到期 / 全员已答提前关闭)。
func (s *GameService) broadcastVirtualCitySurvey(roomID string, sv *virtual_city.Survey) {
	if sv == nil {
		return
	}
	s.hub.BroadcastRoomIncludingSpectators(roomID, wsEnvelope("game.survey_result", 0, map[string]any{
		"room_id": roomID,
		"survey":  virtual_city.SurveyJSONFrom(sv),
	}))
}

// wealthChatSender 把 ChatService.SendFromBot 适配到 virtual_city.ChatSender。
type wealthChatSender struct {
	chat *ChatService
}

// SendFromBot 调 chat.SendFromBot。
func (s *wealthChatSender) SendFromBot(roomID, botUserID, botAccount, modelKey, text string) error {
	if s.chat == nil {
		return nil
	}
	_, err := s.chat.SendFromBot(roomID, botUserID, botAccount, modelKey, text)
	return err
}

// WhisperFromBot 调 chat.WhisperFromBot(2026-09-22 §CityHuman重构:
// speak scope=private 定向耳语,仅目标座位与观战者可见)。
func (s *wealthChatSender) WhisperFromBot(roomID, botUserID, botAccount, modelKey, toUserID, toAccount, text string) error {
	if s.chat == nil {
		return nil
	}
	_, err := s.chat.WhisperFromBot(roomID, botUserID, botAccount, modelKey, toUserID, toAccount, text)
	return err
}

// wsEnvelope 是发送工具(短名)。
func wsEnvelope(frameType string, seq int64, payload any) Envelope {
	return Envelope{Type: frameType, Seq: seq, Payload: mustMarshal(payload)}
}

// JoinVirtualCity 为人类玩家提供 WS game.join 入座便捷(被 xiangqi.go handleJoin 复用)。
func (s *GameService) joinVirtualCityInternal(c *Client, env Envelope, roomID string) {
	s.handleVirtualCityJoin(c, env, roomID)
}

// strconvItoa helper.
func strconvItoa(n int) string { return strconv.Itoa(n) }
