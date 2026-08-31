// Package debate — DebateManager 房间池 + 生命周期管理。
//
// 2026-08-31 §20260831-01 — DebateManager 首期实现:
//
//   - NewDebateManager():空房间池
//   - NewDebateManagerWithRegistry(registry):注入 LLM Registry 用于驱动 Agent
//   - CreateRoom():基于 RoomConfig 创建房间
//   - Get(id) / Room(id):查询房间
//   - List():列出所有房间(供 lobby)
//   - Remove(id):强制删除(运维 / 解散)
//   - StartGame():房主点击开始 → 触发引擎
//   - WipeAllRooms():清空全部(进程关闭兜底)
//
// 线程安全:由 mu 保护 rooms map。
//
// 设计模式对齐:
//   - 狼人杀 WerewolfManager / 德州扑克 TexasHoldemManager(同目录同 package)
//   - 由 service.RoomService 通过 SetGameServiceHook / SetRoomJoiner 注入
package debate

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"LsmAgentGame/agent/debatecommentator"
	"LsmAgentGame/errcode"
	"LsmAgentGame/llm"
)

// DebateManager 辩论房间池。
type DebateManager struct {
	mu sync.RWMutex

	rooms map[string]*DebateRoom

	// 配置注入(由 main.go 调用 NewDebateManagerWithRegistry 注入)
	registry *llm.Registry

	// 房间生命周期钩子(由 service.RoomService 注入)
	onGameStart    func(roomID string)
	onGameOver     func(roomID string)
	onRoomRemove   func(roomID string)
	onPhaseChange  func(roomID string, phase Phase)
	// §20260831-12 — 超管强制解散钩子(带 reason,用于 WS 广播 debate.room_removed 帧)。
	// 与 onRoomRemove 正交:房主 Disband 触发 onRoomRemove,超管 ForceDisband 同时触发两者。
	onForceRemove func(roomID string, reason string)

	// §20260831-02 — 比赛事件广播钩子(由 main.go 接到 ws.DebateService)。
	// game/debate 包不得 import ws(循环引用),所有实时推送经此回调外抛。
	onSpeech    func(roomID string, speech Speech)
	onCrossExam func(roomID string, entry CrossExamEntry)
	onJudgeScore func(roomID string, score JudgeScore)
	onResult    func(roomID string, result *DebateResult)

	// agentStarter Agent 启动器(由 main.go 注入)。
	// 签名:room + engine + registry → 返回任意可 Stop() 的对象。
	// 该函数由 agent/debaterun 包提供,实际启动辩方 + 裁判 + 解说 Agent goroutine。
	// §20260831-09 — 接口增 BotStats / JudgeStats 方法(房间级 Token 聚合用)。
	// 设计动机:避免 debate 包 → agent/debateplayer → debate 包循环引用。
	agentStarter func(room *DebateRoom, engine *DebateEngine, registry *llm.Registry) interface {
		Stop()
		BotStats() []AgentTokenSnapshot
		JudgeStats() []JudgeTokenSnapshot
	}

	// 引擎实例(per room):每个 DebateRoom 对应一个 DebateEngine。
	// 引擎常驻,负责阶段推进 + watchdog。
	engines map[string]*DebateEngine

	// 全局 LLM 并发上限(防止一时刻占用太多上游)。
	llmSema chan struct{}

	// §20260831-03 — 解说 Agent 配置。
	// commentatorModelKey 解说使用的 LLM 模型 key(空 = 不启用解说)。
	commentatorModelKey string

	// commentatorBroadcast 解说广播回调(由 main.go 注入,走 spectator-only 通道)。
	commentatorBroadcast func(roomID, text, style string)

	// §20260831-06 — 裁判公开宣告广播钩子(裁判 announce 工具不再是空操作:
	// docs/辩论比赛/05 §1.2 裁判全阶段持有 announce 工具)。
	onJudgeAnnounce func(roomID string, judgeID int, text string)

	// §20260831-06 — 裁判回答观众提问广播钩子(answer_spectator 工具成功后)。
	onSpectatorAnswer func(roomID string, q SpectatorQuestion)

	// §20260831-06 — 模型胜率统计(进程内,§06 §9)。
	stats *statsStore

	// §20260831-09 — Agent 统计增量帧广播钩子(debate.stats_update)。
	// 触发时机:阶段切换、每 10s ticker、Agent goroutine 任意 LLM 调用后回调。
	// 由 main.go 注入到 ws.DebateService.BroadcastAgentStats。
	onAgentStats func(roomID string, detail *DebateAgentStatsDetail)

	// §20260831-09 — 裁判阶段打分广播钩子(debate.stage_score / debate.judge_scoreboard)。
	onStageScore func(roomID string, ss *StageScore)
}

// NewDebateManager 创建一个空 DebateManager。
//
// 不注入 LLM Registry:Bot 发言将降级为占位/fallback 文本。
func NewDebateManager() *DebateManager {
	return &DebateManager{
		rooms:   make(map[string]*DebateRoom),
		engines: make(map[string]*DebateEngine),
		llmSema: make(chan struct{}, 8), // 默认 8 路并发
		stats:   newStatsStore(),        // §20260831-06 模型胜率统计
	}
}

// NewDebateManagerWithRegistry 注入 LLM Registry。
func NewDebateManagerWithRegistry(registry *llm.Registry) *DebateManager {
	m := NewDebateManager()
	m.registry = registry
	return m
}

// SetLLMRegistry 后注入 Registry(允许 nil,纯 fallback)。
func (m *DebateManager) SetLLMRegistry(r *llm.Registry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.registry = r
}

// SetOnGameStart 注入比赛开始钩子(由 service.RoomService 注入)。
func (m *DebateManager) SetOnGameStart(fn func(roomID string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onGameStart = fn
}

// SetOnGameOver 注入比赛结束钩子。
func (m *DebateManager) SetOnGameOver(fn func(roomID string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onGameOver = fn
}

// SetOnRoomRemove 注入房间移除钩子。
func (m *DebateManager) SetOnRoomRemove(fn func(roomID string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onRoomRemove = fn
}

// SetOnPhaseChange 注入阶段切换钩子(由 DebateEngine.advanceTo 触发)。
func (m *DebateManager) SetOnPhaseChange(fn func(roomID string, phase Phase)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onPhaseChange = fn
}

// SetOnSpeech 注入发言广播钩子(SubmitSpeech / SubmitFreeDebateSpeech 成功后触发)。
func (m *DebateManager) SetOnSpeech(fn func(roomID string, speech Speech)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onSpeech = fn
}

// SetOnCrossExam 注入质询广播钩子(提问 / 回答提交后触发)。
func (m *DebateManager) SetOnCrossExam(fn func(roomID string, entry CrossExamEntry)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onCrossExam = fn
}

// SetOnJudgeScore 注入单裁判评分广播钩子(AddJudgeScore 后触发)。
func (m *DebateManager) SetOnJudgeScore(fn func(roomID string, score JudgeScore)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onJudgeScore = fn
}

// SetOnResult 注入最终结果广播钩子(BuildResult 后触发)。
func (m *DebateManager) SetOnResult(fn func(roomID string, result *DebateResult)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onResult = fn
}

// SetCommentatorModelKey 设置解说 Agent 使用的 LLM 模型 key(空 = 不启用解说)。
func (m *DebateManager) SetCommentatorModelKey(modelKey string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.commentatorModelKey = modelKey
}

// SetCommentatorBroadcast 注入解说广播回调(走 spectator-only 通道)。
func (m *DebateManager) SetCommentatorBroadcast(fn func(roomID, text, style string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.commentatorBroadcast = fn
}

// CommentatorModelKey 锁内读取解说模型 key(供 starter 使用)。
func (m *DebateManager) CommentatorModelKey() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.commentatorModelKey
}

// CommentatorBroadcast 锁内读取解说广播回调(供 starter 使用)。
func (m *DebateManager) CommentatorBroadcast() func(roomID, text, style string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.commentatorBroadcast
}

// LLMSemaChannel 返回 LLM 并发信号量(供解说 Agent 使用)。
func (m *DebateManager) LLMSemaChannel() chan struct{} {
	return m.llmSema
}

// CommentatorSnapProvider 返回解说快照提供者(闭包,锁内构造)。
func (m *DebateManager) CommentatorSnapProvider(room *DebateRoom) func() *debatecommentator.CommentarySnapshot {
	return func() *debatecommentator.CommentarySnapshot {
		return buildCommentarySnapshot(room)
	}
}

// SetOnJudgeAnnounce 注入裁判公开宣告广播钩子(§20260831-06)。
//
// 裁判 announce 工具派发成功后由 EmitJudgeAnnounce 触发,
// ws 层转成 debate.judge_announce 帧推给房间全体观众。
func (m *DebateManager) SetOnJudgeAnnounce(fn func(roomID string, judgeID int, text string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onJudgeAnnounce = fn
}

// SetOnSpectatorAnswer 注入「裁判回答观众提问」广播钩子(§20260831-06)。
func (m *DebateManager) SetOnSpectatorAnswer(fn func(roomID string, q SpectatorQuestion)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onSpectatorAnswer = fn
}

// SetOnAgentStats 注入 Agent 统计增量广播钩子(§20260831-09)。
//
// 触发时机:阶段切换 / 10s ticker / Agent LLM 调用结束。由 DebateService
// 把 payload 推成 debate.stats_update 帧给房间全员。
func (m *DebateManager) SetOnAgentStats(fn func(roomID string, detail *DebateAgentStatsDetail)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onAgentStats = fn
}

// SetOnStageScore 注入裁判阶段打分广播钩子(§20260831-09)。
func (m *DebateManager) SetOnStageScore(fn func(roomID string, ss *StageScore)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onStageScore = fn
}

// EmitAgentStats 外抛 Agent 统计增量帧(§20260831-09)。
//
// 由 DebateEngine 阶段切换 + ticker 触发,DebateService 收到后转
// debate.stats_update 帧广播给全员。nil-safe;无订阅者时静默。
func (m *DebateManager) EmitAgentStats(roomID string, detail *DebateAgentStatsDetail) {
	if m == nil || detail == nil {
		return
	}
	m.mu.RLock()
	fn := m.onAgentStats
	m.mu.RUnlock()
	if fn != nil {
		fn(roomID, detail)
	}
}

// EmitStageScore 外抛裁判阶段打分帧(§20260831-09)。
func (m *DebateManager) EmitStageScore(roomID string, ss *StageScore) {
	if m == nil || ss == nil {
		return
	}
	m.mu.RLock()
	fn := m.onStageScore
	m.mu.RUnlock()
	if fn != nil {
		fn(roomID, ss)
	}
}

// Stats 返回模型胜率统计快照(§20260831-06,§06 §9)。
//
// 供 GET /api/games/debate/stats 使用;按胜率降序。
func (m *DebateManager) Stats() []ModelStats {
	if m.stats == nil {
		return []ModelStats{}
	}
	return m.stats.snapshot()
}

// RecordGameResult 按一局评审结果累加模型胜率统计(§20260831-06)。
//
// 由 DebateEngine.runJudgingPhase 在 SetResult 之后调用(与 resultHook 同一时机)。
func (m *DebateManager) RecordGameResult(room *DebateRoom, res *DebateResult) {
	if m.stats == nil {
		return
	}
	m.stats.recordGameResult(room, res)
}

// EmitJudgeAnnounce 裁判公开宣告外抛(供 agent/debatejudge 调用)。
//
// §20260831-06:首期 announce 工具派发只返回 "announce received",
// 文本被吞掉;现在经此钩子广播给观众。
func (m *DebateManager) EmitJudgeAnnounce(roomID string, judgeID int, text string) {
	text = TruncateRune(text, 100)
	if CountRune(text) == 0 {
		return
	}
	m.mu.RLock()
	fn := m.onJudgeAnnounce
	m.mu.RUnlock()
	if fn != nil {
		fn(roomID, judgeID, text)
	}
}

// emitSpectatorAnswer 安全外抛 onSpectatorAnswer 钩子(§20260831-06)。
//
// 由 DebateRoom.AnswerSpectatorQuestion 在锁外调用。
func (r *DebateRoom) emitSpectatorAnswer(q SpectatorQuestion) {
	if r.manager == nil {
		return
	}
	r.manager.mu.RLock()
	fn := r.manager.onSpectatorAnswer
	r.manager.mu.RUnlock()
	if fn != nil {
		fn(r.RoomID, q)
	}
}

// resultHook 锁内读取 onResult 钩子(供 engine_judge / engine_phase 调用)。
func (m *DebateManager) resultHook() func(roomID string, result *DebateResult) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.onResult
}

// emitSpeech 安全外抛 onSpeech 钩子(房间未挂 manager 或钩子未注入时静默跳过)。
// 钩子函数在 manager.mu 读锁内取值,与 SetOnSpeech 写入互斥,无数据竞争。
func (r *DebateRoom) emitSpeech(sp Speech) {
	if r.manager == nil {
		return
	}
	r.manager.mu.RLock()
	fn := r.manager.onSpeech
	r.manager.mu.RUnlock()
	if fn != nil {
		fn(r.RoomID, sp)
	}
}

// emitCrossExam 安全外抛 onCrossExam 钩子。
func (r *DebateRoom) emitCrossExam(entry CrossExamEntry) {
	if r.manager == nil {
		return
	}
	r.manager.mu.RLock()
	fn := r.manager.onCrossExam
	r.manager.mu.RUnlock()
	if fn != nil {
		fn(r.RoomID, entry)
	}
}

// emitJudgeScore 安全外抛 onJudgeScore 钩子。
func (r *DebateRoom) emitJudgeScore(score JudgeScore) {
	if r.manager == nil {
		return
	}
	r.manager.mu.RLock()
	fn := r.manager.onJudgeScore
	r.manager.mu.RUnlock()
	if fn != nil {
		fn(r.RoomID, score)
	}
}

// SetAgentStarter 注入 Agent 启动器(由 main.go 调用 agent/debaterun.StartAgents)。
//
// 参数 fn 返回任意可 Stop() 的对象;DebateRoom.agentRegistry 持有其引用,
// 房间被 Remove/StopGame 时调用 Stop() 关闭所有 Bot + 裁判 goroutine。
// §20260831-09 — 返回对象额外实现 BotStats / JudgeStats(房间级 Token 聚合用)。
func (m *DebateManager) SetAgentStarter(fn func(room *DebateRoom, engine *DebateEngine, registry *llm.Registry) interface {
	Stop()
	BotStats() []AgentTokenSnapshot
	JudgeStats() []JudgeTokenSnapshot
}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.agentStarter = fn
}

// NewRoomID 生成新的辩论房间 ID(纯客户端占位;真 ID 由 service 层分配)。
func NewRoomID() string {
	// 简洁随机 ID,避免和 service.RoomService 的 16-byte UUID 冲突。
	// 实际生产中,房间 ID 由 service.RoomService.CreateRoom 分配,这里仅备用。
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 10)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return "debate_" + string(b)
}

// ============================================================================
// 房间 CRUD
// ============================================================================

// CreateRoom 基于 RoomConfig 创建房间。
//
// 校验项:
//   - Topic 必填
//   - Mode ∈ {2,3,4,5}
//   - Teams 长度 == teamCount;每队 Agents 长度 ∈ [2,4]
//   - Judges 长度 >= 1
//   - 创建者 CreatedBy 非空
//
// 校验通过 → 返回房间;失败 → 返回错误。
func (m *DebateManager) CreateRoom(cfg RoomConfig) (*DebateRoom, *errcode.Error) {
	if cfg.CreatedBy == "" {
		return nil, errcode.CodeMsg(errcode.ErrValidationFailed, "debate: createdBy is required")
	}
	if cfg.Topic.Text == "" {
		return nil, errcode.CodeMsg(errcode.ErrValidationFailed, "debate: topic.text is required")
	}
	teamCount := len(cfg.Teams)
	if teamCount < 2 || teamCount > 5 {
		return nil, errcode.CodeMsg(errcode.ErrValidationFailed,
			fmt.Sprintf("debate: invalid team count %d (must be 2..5)", teamCount))
	}
	for i, team := range cfg.Teams {
		if len(team.Agents) < 2 || len(team.Agents) > 4 {
			return nil, errcode.CodeMsg(errcode.ErrValidationFailed,
				fmt.Sprintf("debate: team %d has %d agents (must be 2..4)", i, len(team.Agents)))
		}
	}
	if len(cfg.Judges) < 1 {
		return nil, errcode.CodeMsg(errcode.ErrValidationFailed,
			"debate: at least 1 judge is required (default: 3)")
	}
	if cfg.CreatedAt == 0 {
		cfg.CreatedAt = WallNow()
	}

	roomID := NewRoomID()
	room := NewDebateRoom(roomID, cfg, m)

	m.mu.Lock()
	m.rooms[roomID] = room
	m.mu.Unlock()

	return room, nil
}

// Get 返回房间指针(读锁保护)。
func (m *DebateManager) Get(roomID string) (*DebateRoom, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.rooms[roomID]
	return r, ok
}

// Room Get 的别名(对齐 werewolf 接口)。
func (m *DebateManager) Room(roomID string) (*DebateRoom, bool) {
	return m.Get(roomID)
}

// List 返回所有 DebateRoom 副本(供大厅列表)。
func (m *DebateManager) List() []*DebateRoom {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*DebateRoom, 0, len(m.rooms))
	for _, r := range m.rooms {
		out = append(out, r)
	}
	return out
}

// ListByFilter 返回筛选后的房间列表(给 lobby 用)。
//
// 过滤维度:topic_type / mode / status(进行中/等待)。
func (m *DebateManager) ListByFilter(topicType, mode, status string) []*DebateRoom {
	all := m.List()
	out := make([]*DebateRoom, 0, len(all))
	for _, r := range all {
		if topicType != "" && r.Config.Topic.Type != topicType {
			continue
		}
		if mode != "" && string(r.Config.Mode) != mode {
			continue
		}
		if status != "" && string(r.Phase()) != status {
			continue
		}
		out = append(out, r)
	}
	return out
}

// RoomIDs 返回所有房间 ID 快照。
func (m *DebateManager) RoomIDs() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.rooms))
	for id := range m.rooms {
		out = append(out, id)
	}
	return out
}

// Remove 强制删除房间(供 admin/解散)。
//
// 返回是否真正删除(房间不存在返回 false)。
func (m *DebateManager) Remove(roomID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rooms[roomID]
	if !ok {
		return false
	}
	r.cancelSetClosed() // 先取消 ctx,再清 map
	// 取消 Agent 句柄
	if r.agentRegistry != nil {
		r.agentRegistry.Stop()
		r.agentRegistry = nil
	}
	delete(m.rooms, roomID)
	if eng, ok2 := m.engines[roomID]; ok2 {
		eng.Stop()
		delete(m.engines, roomID)
	}
	if m.onRoomRemove != nil {
		go m.onRoomRemove(roomID)
	}
	return true
}

// WipeAllRooms 清空全部房间(进程退出兜底)。返回被清理的 ID 列表。
func (m *DebateManager) WipeAllRooms() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]string, 0, len(m.rooms))
	for id, r := range m.rooms {
		r.cancelSetClosed()
		if r.agentRegistry != nil {
			r.agentRegistry.Stop()
		}
		if eng, ok := m.engines[id]; ok {
			eng.Stop()
		}
		ids = append(ids, id)
	}
	m.rooms = make(map[string]*DebateRoom)
	m.engines = make(map[string]*DebateEngine)
	return ids
}

// IsRoomActive 报告房间是否处于活跃状态(供 RoomService.JanitorSweepStale)。
func (m *DebateManager) IsRoomActive(roomID string) bool {
	r, ok := m.Get(roomID)
	if !ok {
		return false
	}
	p := r.Phase()
	return p != PhaseFilling && p != PhaseGameOver
}

// Engine 返回房间对应的引擎(可能 nil,房间刚创建还未启动比赛时)。
func (m *DebateManager) Engine(roomID string) (*DebateEngine, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.engines[roomID]
	return e, ok
}

// ============================================================================
// 比赛启动 / 关闭
// ============================================================================

// StartGame 房主点击开始 → 启动引擎。
//
// 启动失败回滚(回 PhaseFilling)。
func (m *DebateManager) StartGame(roomID, callerUserID string) *errcode.Error {
	r, ok := m.Get(roomID)
	if !ok {
		return errcode.CodeMsg(errcode.ErrRoomNotFound, "debate: room not found")
	}
	if !r.IsOwner(callerUserID) {
		return errcode.CodeMsg(errcode.ErrPermissionDenied, "debate: only owner can start the game")
	}
	if r.IsGameStarted() {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "debate: game already started")
	}

	// 启动引擎
	eng := NewDebateEngine(r, m)
	m.mu.Lock()
	m.engines[roomID] = eng
	m.mu.Unlock()

	// 启动 Agent(辩方 + 裁判);由 main.go 通过 SetAgentStarter 注入的函数完成
	if m.agentStarter != nil {
		r.agentRegistry = m.agentStarter(r, eng, m.registry)
	}

	r.MarkStarted()
	r.SetPhase(PhasePreparation)

	// 触发 onGameStart 钩子
	if m.onGameStart != nil {
		go m.onGameStart(roomID)
	}

	// 异步启动引擎 goroutine
	ctx, cancel := context.WithCancel(context.Background())
	eng.ctx = ctx
	eng.cancel = cancel
	go eng.Run()

	return nil
}

// StopGame 强制结束比赛(运维 / 房主解散)。
func (m *DebateManager) StopGame(roomID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rooms[roomID]
	if !ok {
		return false
	}
	if r.agentRegistry != nil {
		r.agentRegistry.Stop()
	}
	if eng, ok2 := m.engines[roomID]; ok2 {
		eng.Stop()
	}
	r.SetPhase(PhaseGameOver)
	r.SetClosed()
	if m.onGameOver != nil {
		go m.onGameOver(roomID)
	}
	return true
}

// SetOnForceRemove 注入超管强制解散钩子(§20260831-12)。
//
// 与 SetOnRoomRemove 的区别:onRoomRemove 只有 roomID,用于通用生命周期通知;
// onForceRemove 带 reason 参数,用于 WS 广播 debate.room_removed 帧,
// 让前端能展示「房间被管理员解散,原因:xxx」的提示。
// ForceDisband 会同时触发 onRoomRemove + onForceRemove;房主 Disband 只触发 onRoomRemove。
func (m *DebateManager) SetOnForceRemove(fn func(roomID string, reason string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onForceRemove = fn
}

// Disband 房主解散房间(彻底删除,不同于 StopGame 仅结束比赛)。
// 仅房主可操作,房间存在且未在删除中时返回 true。
func (m *DebateManager) Disband(roomID, callerUserID string) (*errcode.Error, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rooms[roomID]
	if !ok {
		return errcode.CodeMsg(errcode.ErrRoomNotFound, "debate: room not found"), false
	}
	if !r.IsOwner(callerUserID) {
		return errcode.CodeMsg(errcode.ErrPermissionDenied, "debate: only owner can disband"), false
	}
	// 停止 Agent + 引擎
	if r.agentRegistry != nil {
		r.agentRegistry.Stop()
		r.agentRegistry = nil
	}
	if eng, ok2 := m.engines[roomID]; ok2 {
		eng.Stop()
		delete(m.engines, roomID)
	}
	r.SetPhase(PhaseGameOver)
	r.SetClosed()
	delete(m.rooms, roomID)
	if m.onRoomRemove != nil {
		go m.onRoomRemove(roomID)
	}
	return nil, true
}

// ForceDisband 超级管理员强制解散房间(§20260831-12)。
//
// 与 Disband 的区别:
//   - Disband: 仅房主可调用, 校验 IsOwner
//   - ForceDisband: 超管可调用, 不校验身份, 带 reason 参数
//
// 资源释放顺序(与 Disband 一致, 额外触发 onForceRemove 钩子):
//   1. 停止 Agent goroutine(辩手 + 裁判 + 解说)
//   2. 停止引擎阶段机
//   3. 设 PhaseGameOver + closed
//   4. 从 rooms map 删除
//   5. 触发 onRoomRemove 钩子(DB 清理等)
//   6. 触发 onForceRemove 钩子(WS 广播 debate.room_removed 帧)
//
// 返回值: spectatorCount = 解散时观战者人数(含房主); ok = 是否真正执行了删除
// 房间不存在时 ok=false, 调用方应返回 200 + "room already absent"。
func (m *DebateManager) ForceDisband(roomID, reason string) (spectatorCount int, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rooms[roomID]
	if !ok {
		return 0, false
	}
	// 记录观战者数(返回给前端作审计)
	specCount := r.SpectatorCount()
	// 停止 Agent + 引擎
	if r.agentRegistry != nil {
		r.agentRegistry.Stop()
		r.agentRegistry = nil
	}
	if eng, ok2 := m.engines[roomID]; ok2 {
		eng.Stop()
		delete(m.engines, roomID)
	}
	r.SetPhase(PhaseGameOver)
	r.SetClosed()
	delete(m.rooms, roomID)
	// 触发通用移除钩子(DB 清理)
	if m.onRoomRemove != nil {
		go m.onRoomRemove(roomID)
	}
	// 触发超管强制移除钩子(WS 广播 debate.room_removed 帧)
	if m.onForceRemove != nil {
		go m.onForceRemove(roomID, reason)
	}
	return specCount, true
}

// Registry 返回 LLM Registry(供 Agent / 裁判使用)。
func (m *DebateManager) Registry() *llm.Registry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.registry
}

// AcquireLLM 占用全局 LLM 并发槽位(带 5s 超时)。
//
// 防止一时刻所有 bot 同时调用 LLM 把上游打挂。
func (m *DebateManager) AcquireLLM(ctx context.Context) bool {
	if m.llmSema == nil {
		return true
	}
	select {
	case m.llmSema <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	case <-time.After(5 * time.Second):
		return false
	}
}

// ReleaseLLM 释放 LLM 槽位。
func (m *DebateManager) ReleaseLLM() {
	if m.llmSema == nil {
		return
	}
	select {
	case <-m.llmSema:
	default:
	}
}

// ============================================================================
// §20260831-03 — 解说快照构造
// ============================================================================

// buildCommentarySnapshot 从 DebateRoom 构造解说快照(锁内调用)。
func buildCommentarySnapshot(room *DebateRoom) *debatecommentator.CommentarySnapshot {
	room.mu.RLock()
	defer room.mu.RUnlock()

	snap := &debatecommentator.CommentarySnapshot{
		RoomID:    room.RoomID,
		Phase:     string(room.currentPhase),
		PhaseCN:   PhaseCN(room.currentPhase),
		Topic:     room.Config.Topic.Text,
		TeamCount: len(room.Config.Teams),
	}

	// 最近 5 条发言
	recentSpeeches := room.speeches.lastN(5)
	for _, sp := range recentSpeeches {
		snap.RecentSpeeches = append(snap.RecentSpeeches, debatecommentator.SpeechSummary{
			SpeakerName: sp.SpeakerName,
			StanceLabel: string(sp.Stance),
			RoleCN:      RoleCN(sp.Role),
			Content:     sp.Content,
			PhaseCN:     PhaseCN(sp.Phase),
		})
	}

	// 当前比分(评审阶段后)
	if room.result != nil {
		for _, ts := range room.result.TeamScores {
			snap.TeamScores = append(snap.TeamScores, debatecommentator.TeamScoreSummary{
				TeamID:     ts.TeamID,
				TeamName:   ts.TeamName,
				TotalScore: ts.TotalScore,
			})
		}
	}

	return snap
}