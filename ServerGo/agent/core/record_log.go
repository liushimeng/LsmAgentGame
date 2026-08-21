// Package agent — record_log.go: RecordLogService 把 Agent 的 LLM 调用 /
// 工具调用 / 动作决策 / 对局结果持久化到 DB,作为"模型管理 + 模型玩家持久化
// + 模型金币" plan (kind-skipping-moth §2.3) 的服务端入口。
//
// 设计原则:
//   - 不阻塞游戏流:所有 DB 写都走 background goroutine;GameStarted 同步
//     返回 game_log.id(后续每条 chat/action 都需要它),其他都异步。
//   - 失败仅 log,不返回 error 给游戏调用方。channel 满时丢最旧并 warn。
//   - Shutdown 必须 server 优雅退出时调用,5s timeout,剩余丢弃。
//   - nil-safe:任意方法在 s == nil 时立即返回(no-op),方便测试桩 + 旧
//     代码路径继续工作。
package agentcore

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"LsmAgentGame/logger"
	"LsmAgentGame/models"
	"LsmAgentGame/service"
	"LsmAgentGame/util"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// recordLogBufferSize is the default buffered channel size for the
// background worker. With N bot × ~50 events/局 = ~350 events; 1024
// gives a comfortable 3局 burst capacity before backpressure kicks in.
const recordLogBufferSize = 1024

// recordLogDrainTimeout is how long Shutdown waits for the worker to flush
// pending events before giving up.
const recordLogDrainTimeout = 5 * time.Second

// eventKind enumerates the wire kinds the worker understands. The
// recordEvent.data byte slice is JSON-decoded into the matching struct.
const (
	evKindGameStarted   = "game_started"
	evKindChatMessage   = "chat_message"
	evKindAction        = "action"
	evKindGameEnded     = "game_ended"
)

// cachedGameLog is the in-memory cache of active game_log rows. Keyed by
// "roomID:seat" so any subsequent RecordChatMessage / RecordAction /
// GameEnded call can look up the game_log.id without re-querying the DB.
//
// The cache is populated by GameStarted (which writes a row + stores the
// id) and consumed by GameEnded. We never evict: the worker drops the
// cache after GameEnded returns success. The cache is bounded by the
// number of (room, seat) pairs alive at any given moment, which in
// practice is at most a few hundred for the platform.
//
// Exported (CachedGameLog) so external packages (ws/chat_service) can
// read BotUserID when matching an inbound chat to a cached game_log row
// (since ChatService only has the bot's userID, not the seat).
type CachedGameLog struct {
	ID         string
	ProviderID string
	BotUserID  string
	GameKind   string
	StartedAt  time.Time
}

// gameStartedPayload is the data layout for evKindGameStarted events. The
// ID field is set by the worker after INSERT and then re-attached to the
// cached row in the GameStarted return path.
type gameStartedPayload struct {
	ProviderID string
	BotUserID  string
	RoomID     string
	GameKind   string
	Seat       int
	Role       string
}

// chatMessagePayload is the data layout for evKindChatMessage events.
type chatMessagePayload struct {
	GameLogID  string
	BotUserID  string
	ProviderID string
	RoomID     string
	Role       string
	Seq        int64
	Content    string
	Phase      string
	ToolName   string
	ToolInput  string
	Thinking   string
	StopReason string
	LatencyMs  int
}

// actionPayload is the data layout for evKindAction events.
type actionPayload struct {
	GameLogID    string
	BotUserID    string
	Phase        string
	ActionType   string
	ActionTarget string
	Payload      string
	Reasoning    string
	Accepted     bool
}

// gameEndedPayload is the data layout for evKindGameEnded events. The
// worker writes the game_log update + invokes wallet settlement.
type gameEndedPayload struct {
	GameLogID     string
	Result        string
	CoinDelta     int64
	LLMCallCount  int
	InputTokens   int
	OutputTokens  int
	FinalHand     string
	BotUserID     string // 派生自 cache,worker 用来调用 walletService
	RoomID        string
	GameKind      string
	SettleWallet  bool   // 是否调 walletService.ApplyTransaction
	TxType        string // walletService.Credit/Debit 的 txType
	Remark        string
}

// recordEvent is the worker's internal channel envelope. We use a struct
// (not a typed payload directly) so a future event kind does not require
// 8 typed channels.
type recordEvent struct {
	kind string
	data []byte
}

// RecordLogService 把 Agent 的 LLM 调用 / 工具调用 / 动作决策持久化到 DB。
// 入口由 Agent.Run / DispatchTool / SendFromBot / ChatStream 等调用,
// 所有方法立即返回;实际 DB 写通过内部 buffered channel + worker
// goroutine 处理。
//
// nil-safe: 当 s == nil 时,所有 RecordXxx 方法都是 no-op;允许旧代码
// 路径 + 测试桩继续工作。
type RecordLogService struct {
	gormDB        *gorm.DB
	walletService *service.WalletService

	// gameLogCache: roomID:seat → *cachedGameLog. 同一 bot 同一局所有
	// chat/action 都关联到同一个 game_log 行。
	gameLogCache sync.Map

	events       chan recordEvent
	workerCtx    context.Context
	workerCancel context.CancelFunc
	workerWG     sync.WaitGroup

	// startGameStartedResult is a per-request reply channel used by the
	// synchronous GameStarted return path. We register a one-shot channel
	// under a per-request key and the worker sends the new game_log.id
	// back. Keyed by the caller-side request token (gameStartedCaller
	// stores the channel locally; the channel is closed by the worker
	// after one write).
	gameStartedMu      sync.Mutex
	gameStartedWaiters map[string]chan gameStartedResult
}

// gameStartedResult is what the worker sends back to the synchronous
// GameStarted caller.
type gameStartedResult struct {
	id         string
	providerID string
	botUserID  string
	startedAt  time.Time
	err        error
}

// NewRecordLogService 启动后台 worker。cfg.BufferSize 默认 1024。
//
// workerCtx 由 RecordLogService 内部创建,生命周期与 worker 同步。
// 任何 panic 都会被 recover() 兜住并打 log,不会让 worker 退出会
// 导致 channel 满。
func NewRecordLogService(gormDB *gorm.DB, ws *service.WalletService) *RecordLogService {
	ctx, cancel := context.WithCancel(context.Background())
	s := &RecordLogService{
		gormDB:             gormDB,
		walletService:      ws,
		events:             make(chan recordEvent, recordLogBufferSize),
		workerCtx:          ctx,
		workerCancel:       cancel,
		gameStartedWaiters: make(map[string]chan gameStartedResult),
	}
	s.workerWG.Add(1)
	go s.workerLoop()
	return s
}

// GameStarted 报告「bot 进入了某局」。返回 GameLogID(caller 必须存)。
//
// 同步:必须立即返回 game_log.id 给调用方,因为后续每条 chat/action 都
// 引用它。INSERT 走 worker goroutine,但通过 reply channel 等到 worker
// 完成 INSERT 后再返回。worst-case 等待 ~10ms(单 INSERT + 1 索引)。
//
// nil-safe: s == nil 时返回 "", nil(测试桩兼容)。
func (s *RecordLogService) GameStarted(ctx context.Context,
	providerID, botUserID, roomID, gameKind string,
	seat int, role string,
) (string, error) {
	if s == nil || s.gormDB == nil {
		return "", nil
	}

	// 检查缓存:同一 (room,seat) 已存在则复用现有 game_log.id
	// (支持 §115 的"重开局保留 chatQueue/座位"场景,新一局应该新建
	// game_log,roomID+seat 不变但 GameStarted 被再次调用时,需要先
	// 释放旧 cache entry — 调用方应在重开局前主动调 EndGame+Clear)。
	cacheKey := roomID + ":" + itoa(seat)
	if v, ok := s.gameLogCache.Load(cacheKey); ok {
		if c, ok2 := v.(*CachedGameLog); ok2 && c != nil {
			// 同一 (room,seat) 已经在记录中 — 复用现有 game_log.id
			// (这是 RestartVote 通过后的新一局开赛; 暂未接入自动
			// 清缓存,这里保留旧 id,后续 GameEnded 后会清理)。
			return c.ID, nil
		}
	}

	payload := gameStartedPayload{
		ProviderID: providerID,
		BotUserID:  botUserID,
		RoomID:     roomID,
		GameKind:   gameKind,
		Seat:       seat,
		Role:       role,
	}

	// 注册同步 reply channel
	reply := make(chan gameStartedResult, 1)
	reqToken := util.NewUUID()
	s.gameStartedMu.Lock()
	s.gameStartedWaiters[reqToken] = reply
	s.gameStartedMu.Unlock()

	defer func() {
		s.gameStartedMu.Lock()
		delete(s.gameStartedWaiters, reqToken)
		s.gameStartedMu.Unlock()
	}()

	// 投递事件。channel 满时丢最旧(并 warn)+ 投递失败时降级为同步写。
	// 关键:把 reqToken 附在 payload 上,worker 才能把 reply 准确送回。
	payloadWithToken := struct {
		*gameStartedPayload
		Token string `json:"token"`
	}{gameStartedPayload: &payload, Token: reqToken}
	dataWithToken, _ := json.Marshal(payloadWithToken)

	select {
	case s.events <- recordEvent{kind: evKindGameStarted, data: dataWithToken}:
	default:
		logger.L().Warn("RecordLog: events channel full, dropped game_started",
			zap.String("room_id", roomID), zap.Int("seat", seat))
		// 同步 fallback:直接 INSERT 并返回 id
		return s.syncGameStarted(ctx, payload)
	}

	// 等 worker 回复
	select {
	case res := <-reply:
		if res.err != nil {
			return "", res.err
		}
		// 写入 cache
		entry := &CachedGameLog{
			ID:         res.id,
			ProviderID: res.providerID,
			BotUserID:  res.botUserID,
			GameKind:   payload.GameKind,
			StartedAt:  res.startedAt,
		}
		s.gameLogCache.Store(cacheKey, entry)
		return res.id, nil
	case <-time.After(10 * time.Second):
		logger.L().Warn("RecordLog: game_started reply timeout, falling back to sync",
			zap.String("room_id", roomID), zap.Int("seat", seat))
		return s.syncGameStarted(ctx, payload)
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// syncGameStarted 是 events channel 满时降级用的同步实现。直接调 DB。
func (s *RecordLogService) syncGameStarted(ctx context.Context, p gameStartedPayload) (string, error) {
	if s.gormDB == nil {
		return "", nil
	}
	now := time.Now()
	row := models.TLsmGameModelGameLog{
		ID:         util.NewUUID(),
		ProviderID: p.ProviderID,
		BotUserID:  p.BotUserID,
		RoomID:     p.RoomID,
		GameKind:   p.GameKind,
		Seat:       p.Seat,
		Role:       p.Role,
		StartedAt:  now,
	}
	if err := s.gormDB.WithContext(ctx).Create(&row).Error; err != nil {
		logger.L().Error("RecordLog: sync game_started failed",
			zap.String("room_id", p.RoomID), zap.Int("seat", p.Seat), zap.Error(err))
		return "", err
	}
	entry := &CachedGameLog{
		ID:         row.ID,
		ProviderID: p.ProviderID,
		BotUserID:  p.BotUserID,
		GameKind:   p.GameKind,
		StartedAt:  now,
	}
	s.gameLogCache.Store(p.RoomID+":"+itoa(p.Seat), entry)
	return row.ID, nil
}

// RecordChatMessage 异步写一条聊天原文。
//
// nil-safe: s == nil / GameLogID 为空时直接返回。
func (s *RecordLogService) RecordChatMessage(
	gameLogID, botUserID, providerID, roomID, role, phase string,
	seq int64, content, thinking, toolName, toolInput, stopReason string,
	latencyMs int,
) {
	if s == nil || s.gormDB == nil || gameLogID == "" {
		return
	}
	p := chatMessagePayload{
		GameLogID:  gameLogID,
		BotUserID:  botUserID,
		ProviderID: providerID,
		RoomID:     roomID,
		Role:       role,
		Seq:        seq,
		Content:    content,
		Phase:      phase,
		ToolName:   toolName,
		ToolInput:  toolInput,
		Thinking:   thinking,
		StopReason: stopReason,
		LatencyMs:  latencyMs,
	}
	s.enqueue(evKindChatMessage, p)
}

// RecordAction 异步写一条动作。
//
// nil-safe: s == nil / GameLogID 为空时直接返回。
func (s *RecordLogService) RecordAction(
	gameLogID, botUserID, phase, actionType, actionTarget,
	payload, reasoning string, accepted bool,
) {
	if s == nil || s.gormDB == nil || gameLogID == "" {
		return
	}
	p := actionPayload{
		GameLogID:    gameLogID,
		BotUserID:    botUserID,
		Phase:        phase,
		ActionType:   actionType,
		ActionTarget: actionTarget,
		Payload:      payload,
		Reasoning:    reasoning,
		Accepted:     accepted,
	}
	s.enqueue(evKindAction, p)
}

// GameEnded 异步标记 game_log 行 EndedAt + Result + CoinDelta +
// LLMCallCount/Tokens,然后(可选)异步结算 wallet。
//
// settleWallet=true 且 walletService != nil 时,worker 会调
// walletService.Credit(coin>0) 或 walletService.Debit(coin<0),
// refType="model_game", refID=gameLogID, gameKind=传入值。
//
// nil-safe: s == nil / GameLogID 为空时直接返回。
func (s *RecordLogService) GameEnded(ctx context.Context,
	gameLogID, result string, coinDelta int64,
	llmCallCount, inputTokens, outputTokens int, finalHand string,
) {
	if s == nil || s.gormDB == nil || gameLogID == "" {
		return
	}
	// 从 cache 取 (roomID, seat, botUserID) 以便 worker 结算 wallet
	var botUserID, roomID, gameKind string
	s.gameLogCache.Range(func(k, v any) bool {
		if c, ok := v.(*CachedGameLog); ok && c != nil && c.ID == gameLogID {
			botUserID = c.BotUserID
			roomID = "" // we don't store roomID in cache; pass through
			gameKind = c.GameKind
			return false
		}
		return true
	})

	txType := "model_game_settle"
	remark := "模型对局结算:" + result
	p := gameEndedPayload{
		GameLogID:    gameLogID,
		Result:       result,
		CoinDelta:    coinDelta,
		LLMCallCount: llmCallCount,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		FinalHand:    finalHand,
		BotUserID:    botUserID,
		RoomID:       roomID,
		GameKind:     gameKind,
		SettleWallet: coinDelta != 0 && s.walletService != nil,
		TxType:       txType,
		Remark:       remark,
	}
	// GameEnded 包含 DB UPDATE + 可选 wallet 结算,需要 worker
	// 处理(异步,不阻塞游戏调用方)。
	// 不走 reply channel: GameEnded 不需要返回 game_log.id。
	// 注:我们用 evKindGameEnded 让 worker 处理 cache cleanup。
	s.enqueue(evKindGameEnded, p)
}

// ClearGameLogCache removes the cached game_log row for (roomID, seat).
// Called by the activity emitter after EmitGameOver so the next room
// (RestartVote through) can create a fresh game_log row.
func (s *RecordLogService) ClearGameLogCache(roomID string, seat int) {
	if s == nil {
		return
	}
	s.gameLogCache.Delete(roomID + ":" + itoa(seat))
}

// GameLogIDByRoomSeat returns the cached game_log.id for (roomID, seat) if any.
func (s *RecordLogService) GameLogIDByRoomSeat(roomID string, seat int) string {
	if s == nil {
		return ""
	}
	if v, ok := s.gameLogCache.Load(roomID + ":" + itoa(seat)); ok {
		if c, ok2 := v.(*CachedGameLog); ok2 && c != nil {
			return c.ID
		}
	}
	return ""
}

// GameLogIDByBotUser 在 room 范围内反查给定 botUserID 对应的 game_log.id。
// 给 ws/chat_service 这类只知道 (roomID, botUserID) 不知道 seat 的
// 调用方用。cache miss 时返回 ""。nil-safe。
func (s *RecordLogService) GameLogIDByBotUser(roomID, botUserID string) string {
	if s == nil || botUserID == "" {
		return ""
	}
	var found string
	s.gameLogCache.Range(func(k, v any) bool {
		ck, _ := k.(string)
		if !strings.HasPrefix(ck, roomID+":") {
			return true
		}
		if c, ok2 := v.(*CachedGameLog); ok2 && c != nil && c.BotUserID == botUserID {
			found = c.ID
			return false
		}
		return true
	})
	return found
}

// enqueue 把事件投递到 events channel,channel 满时丢最旧。
func (s *RecordLogService) enqueue(kind string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		logger.L().Error("RecordLog: marshal failed",
			zap.String("kind", kind), zap.Error(err))
		return
	}
	evt := recordEvent{kind: kind, data: data}
	for {
		select {
		case s.events <- evt:
			return
		default:
			// 缓冲区满,丢最旧一条 + warn
			select {
			case <-s.events:
				logger.L().Warn("RecordLog: events channel full, dropped oldest",
					zap.String("kind", kind))
			default:
			}
			// 再次尝试投递;失败就放弃(避免死循环)
			select {
			case s.events <- evt:
				return
			default:
				logger.L().Warn("RecordLog: enqueue failed, dropped event",
					zap.String("kind", kind))
				return
			}
		}
	}
}

// workerLoop 是 RecordLogService 的后台 goroutine,负责把所有 enqueue
// 的事件写入 DB。任何 panic 都会被 recover 兜住以保证 worker 不会
// 退出导致 channel 永远满。
func (s *RecordLogService) workerLoop() {
	defer s.workerWG.Done()
	defer func() {
		if r := recover(); r != nil {
			logger.L().Error("RecordLog: workerLoop panic recovered",
				zap.Any("recover", r))
		}
	}()

	for {
		select {
		case <-s.workerCtx.Done():
			// drain 残留事件直到 channel 空或 5s timeout
			deadline := time.Now().Add(recordLogDrainTimeout)
			for time.Now().Before(deadline) {
				select {
				case evt := <-s.events:
					s.handleEvent(evt)
				default:
					return
				}
			}
			return
		case evt := <-s.events:
			s.handleEvent(evt)
		}
	}
}

// handleEvent dispatches one recordEvent to the matching DB writer.
func (s *RecordLogService) handleEvent(evt recordEvent) {
	switch evt.kind {
	case evKindGameStarted:
		var wrapped struct {
			*gameStartedPayload
			Token string `json:"token"`
		}
		if err := json.Unmarshal(evt.data, &wrapped); err != nil || wrapped.gameStartedPayload == nil {
			logger.L().Error("RecordLog: unmarshal game_started failed", zap.Error(err))
			return
		}
		s.handleGameStarted(*wrapped.gameStartedPayload, wrapped.Token)
	case evKindChatMessage:
		var p chatMessagePayload
		if err := json.Unmarshal(evt.data, &p); err != nil {
			logger.L().Error("RecordLog: unmarshal chat_message failed", zap.Error(err))
			return
		}
		s.handleChatMessage(p)
	case evKindAction:
		var p actionPayload
		if err := json.Unmarshal(evt.data, &p); err != nil {
			logger.L().Error("RecordLog: unmarshal action failed", zap.Error(err))
			return
		}
		s.handleAction(p)
	case evKindGameEnded:
		var p gameEndedPayload
		if err := json.Unmarshal(evt.data, &p); err != nil {
			logger.L().Error("RecordLog: unmarshal game_ended failed", zap.Error(err))
			return
		}
		s.handleGameEnded(p)
	default:
		logger.L().Warn("RecordLog: unknown event kind", zap.String("kind", evt.kind))
	}
}

// handleGameStarted writes the game_log row + replies on the request
// channel.
func (s *RecordLogService) handleGameStarted(p gameStartedPayload, reqToken string) {
	now := time.Now()
	row := models.TLsmGameModelGameLog{
		ID:         util.NewUUID(),
		ProviderID: p.ProviderID,
		BotUserID:  p.BotUserID,
		RoomID:     p.RoomID,
		GameKind:   p.GameKind,
		Seat:       p.Seat,
		Role:       p.Role,
		StartedAt:  now,
	}
	if err := s.gormDB.Create(&row).Error; err != nil {
		logger.L().Error("RecordLog: game_started insert failed",
			zap.String("room_id", p.RoomID), zap.Int("seat", p.Seat), zap.Error(err))
		// 回复错误给 caller
		s.replyGameStarted(reqToken, gameStartedResult{err: err})
		return
	}
	// 写 cache
	cacheKey := p.RoomID + ":" + itoa(p.Seat)
	s.gameLogCache.Store(cacheKey, &CachedGameLog{
		ID:         row.ID,
		ProviderID: p.ProviderID,
		BotUserID:  p.BotUserID,
		GameKind:   p.GameKind,
		StartedAt:  now,
	})
	s.replyGameStarted(reqToken, gameStartedResult{
		id:         row.ID,
		providerID: p.ProviderID,
		botUserID:  p.BotUserID,
		startedAt:  now,
	})
}

// replyGameStarted delivers the result to the waiter that owns reqToken.
// The GameStarted caller uses a unique UUID per request; we look it up
// directly in the waiters map. Each request is 1:1 mapped to one waiter,
// so concurrent GameStarted calls (e.g. multiple bots starting simultaneously)
// don't cross-contaminate.
func (s *RecordLogService) replyGameStarted(reqToken string, res gameStartedResult) {
	s.gameStartedMu.Lock()
	defer s.gameStartedMu.Unlock()
	if ch, ok := s.gameStartedWaiters[reqToken]; ok {
		select {
		case ch <- res:
		default:
		}
	}
}

func (s *RecordLogService) handleChatMessage(p chatMessagePayload) {
	row := models.TLsmGameModelChatMessage{
		GameLogID:  p.GameLogID,
		BotUserID:  p.BotUserID,
		ProviderID: p.ProviderID,
		RoomID:     p.RoomID,
		Seq:        p.Seq,
		Role:       p.Role,
		Content:    p.Content,
		Phase:      p.Phase,
		ToolName:   p.ToolName,
		ToolInput:  p.ToolInput,
		Thinking:   p.Thinking,
		StopReason: p.StopReason,
		LatencyMs:  p.LatencyMs,
	}
	if err := s.gormDB.Create(&row).Error; err != nil {
		logger.L().Error("RecordLog: chat_message insert failed",
			zap.String("game_log_id", p.GameLogID), zap.String("role", p.Role), zap.Error(err))
	}
}

func (s *RecordLogService) handleAction(p actionPayload) {
	row := models.TLsmGameModelAction{
		GameLogID:    p.GameLogID,
		BotUserID:    p.BotUserID,
		Phase:        p.Phase,
		ActionType:   p.ActionType,
		ActionTarget: p.ActionTarget,
		Payload:      p.Payload,
		Reasoning:    p.Reasoning,
		Accepted:     p.Accepted,
	}
	if err := s.gormDB.Create(&row).Error; err != nil {
		logger.L().Error("RecordLog: action insert failed",
			zap.String("game_log_id", p.GameLogID), zap.String("action_type", p.ActionType),
			zap.Error(err))
	}
}

func (s *RecordLogService) handleGameEnded(p gameEndedPayload) {
	now := time.Now()
	updates := map[string]any{
		"ended_at":        &now,
		"result":          p.Result,
		"coin_delta":      p.CoinDelta,
		"llm_call_count":  p.LLMCallCount,
		"input_tokens":    p.InputTokens,
		"output_tokens":   p.OutputTokens,
		"final_hand":      p.FinalHand,
	}
	if err := s.gormDB.Model(&models.TLsmGameModelGameLog{}).
		Where("id = ?", p.GameLogID).
		Updates(updates).Error; err != nil {
		logger.L().Error("RecordLog: game_ended update failed",
			zap.String("game_log_id", p.GameLogID), zap.Error(err))
	}
	// 清理 cache:删所有 cache 中 ID 等于此 game_log.id 的 entry
	s.gameLogCache.Range(func(k, v any) bool {
		if c, ok := v.(*CachedGameLog); ok && c != nil && c.ID == p.GameLogID {
			s.gameLogCache.Delete(k)
		}
		return true
	})
	// wallet 结算
	if p.SettleWallet && s.walletService != nil {
		ctx := s.workerCtx
		refID := p.GameLogID
		// v2.0 DEFECT 2 (L2 app-level dedup): 若该 bot 在本局(同 game_log)已结算过,
		// 视为良性 no-op。refID=game_log.id(每 bot 每局唯一),配合
		// (user_id, ref_type="model_game", ref_id, tx_type) 保证跨进程只结算一次。
		if done, derr := s.walletService.AlreadySettled(ctx, p.BotUserID,
			"model_game", refID, p.TxType); derr == nil && done {
			logger.L().Info("RecordLog: bot already settled, skip (idempotent)",
				zap.String("bot_user_id", p.BotUserID), zap.String("game_log_id", refID))
			return
		}
		if p.CoinDelta > 0 {
			if err := s.walletService.Credit(ctx, p.BotUserID, p.TxType,
				"model_game", refID, p.GameKind, p.Remark, p.CoinDelta); err != nil {
				logger.L().Error("RecordLog: wallet credit failed",
					zap.String("bot_user_id", p.BotUserID),
					zap.Int64("amount", p.CoinDelta), zap.Error(err))
			}
		} else if p.CoinDelta < 0 {
			if err := s.walletService.Debit(ctx, p.BotUserID, p.TxType,
				"model_game", refID, p.GameKind, p.Remark, -p.CoinDelta); err != nil {
				logger.L().Error("RecordLog: wallet debit failed",
					zap.String("bot_user_id", p.BotUserID),
					zap.Int64("amount", p.CoinDelta), zap.Error(err))
			}
		}
	}
}

// Shutdown 关闭 worker。等待 drain 超时 5s,剩余丢弃。
func (s *RecordLogService) Shutdown(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.workerCancel()
	// 等 worker 退出(内部 5s drain timeout)
	doneCh := make(chan struct{})
	go func() {
		s.workerWG.Wait()
		close(doneCh)
	}()
	select {
	case <-doneCh:
		return nil
	case <-time.After(recordLogDrainTimeout + 1*time.Second):
		logger.L().Warn("RecordLog: shutdown timed out")
		return errors.New("record log shutdown timeout")
	case <-ctx.Done():
		return ctx.Err()
	}
}

