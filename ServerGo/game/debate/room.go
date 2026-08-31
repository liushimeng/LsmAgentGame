// Package debate — DebateRoom 房间 + 状态机核心结构。
//
// 2026-08-31 §20260831-01 — 房间首期实现。
//
// DebateRoom 代表一个辩论比赛房间:
//   - 持有完整的 RoomConfig + 发言历史 + 评审记录
//   - 提供线程安全的状态查询与变更
//   - 与 DebateManager(房间池)协作完成生命周期管理
//
// 设计要点:
//   - 房间对象在内存中常驻,DB 行仅作持久化备份
//   - 状态变更通过 DebateManager 集中调度,DebateRoom 自身不启动 goroutine
//   - 线程安全由 debateRoom.mu 保护(粒度=整个房间)
//
// 详细设计见 docs/辩论比赛/00-辩论比赛总体架构设计.md §3.1。
package debate

import (
	"errors"
	"sync"
)

// ErrRoomNotFound 房间不存在错误(用于 manager 返回值)。
var ErrRoomNotFound = errors.New("debate: room not found")

// ErrRoomClosed 房间已关闭错误。
var ErrRoomClosed = errors.New("debate: room already closed")

// ErrInvalidPhase 阶段不匹配错误。
var ErrInvalidPhase = errors.New("debate: invalid phase transition")

// ErrSpeechTooLong 发言超长错误(用于前端 toast)。
type ErrSpeechTooLong struct{ Got, Max int }

func (e *ErrSpeechTooLong) Error() string {
	return "debate: speech too long"
}

// DebateRoom 辩论房间核心结构。
type DebateRoom struct {
	// RoomID 由 DebateManager.NewRoomID 生成("debate_<uuid 风格>").
	RoomID string

	// Config 不可变快照,创建后只读。
	Config RoomConfig

	// State 动态状态(被 mu 保护)。
	mu sync.RWMutex

	// currentPhase 当前阶段。
	currentPhase Phase

	// phaseStartedAt 当前阶段开始 unix秒;用于 watchdog 计算超时。
	phaseStartedAt int64

	// phaseDeadline 当前阶段截止 unix秒。
	phaseDeadline int64

	// currentSpeaker 当前发言者(单一发言阶段如立论/驳论/小结/总结)。
	// key = "<team_id>:<seat>",如 "0:0" = 0 队一辩。
	currentSpeaker string

	// freeDebateTurnOwner 自由辩论发言权归属。
	// "team:<id>" = 该队当前拥有发言权;"" = 自由模式(任一方可抢)。
	freeDebateTurnOwner string

	// freeDebateTimeUsed 每队累计使用时间(秒)。
	freeDebateTimeUsed map[int]int

	// crossExamTurn 质询轮次计数。
	crossExamTurn int

	// crossExamActive 质询状态:当前质询方与被质询方。
	crossExamActive *crossExamPair

	// speeches 发言记录。
	speeches *speechStore

	// crossExam 质询记录。
	crossExam *crossExamStore

	// judgeScores 收到的裁判评分(0-3 份)。
	judgeScores []JudgeScore

	// result 最终结果(评审完成后填充)。
	result *DebateResult

	// startedAt 比赛开始 unix秒(对局阶段机启动那一刻)。
	startedAt int64

	// finishedAt 比赛结束 unix秒。
	finishedAt int64

	// closed 房间是否已关闭(对局结束后保留 60s 再清)。
	closed bool

	// subscribers WS 客户端订阅集合(由 DebateManager 注入)。
	// DebateRoom 不直接持有 hub,通过 manager 间接广播。
	manager *DebateManager

	// viewers 观战者集合(详见 room_spectator.go)。
	viewers *spectatorRegistry

	// agentRegistry Agent 句柄集合(详见 agent/debaterun/starter.go)。
	// 实际类型为 *debaterun.Registry;为避免 debate 包循环引用,这里用 interface。
	agentRegistry interface {
		Stop()
	}
}

// crossExamPair 质询双方记录。
type crossExamPair struct {
	Questioner string // "<team_id>:<seat>"
	Answerer   string // "<team_id>:<seat>"
	StartedAt  int64
}

// NewDebateRoom 创建一个辩论房间(由 DebateManager 调用)。
//
// 参数:
//   - roomID: 由 manager 生成
//   - cfg: 完整房间配置(已校验)
func NewDebateRoom(roomID string, cfg RoomConfig, mgr *DebateManager) *DebateRoom {
	if cfg.PhaseConfig.MaxSpeechChars == 0 {
		cfg.PhaseConfig = DefaultPhaseConfig()
	}
	return &DebateRoom{
		RoomID:             roomID,
		Config:             cfg,
		currentPhase:       PhaseFilling,
		phaseStartedAt:     WallNow(),
		speeches:           &speechStore{},
		crossExam:          &crossExamStore{},
		freeDebateTimeUsed: make(map[int]int, len(cfg.Teams)),
		manager:            mgr,
		viewers:            newSpectatorRegistry(),
	}
}

// ============================================================================
// 线程安全访问器
// ============================================================================

// Phase 返回当前阶段(读锁)。
func (r *DebateRoom) Phase() Phase {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.currentPhase
}

// SetPhase 切换当前阶段(写锁,内部用)。
func (r *DebateRoom) SetPhase(p Phase) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.currentPhase = p
	r.phaseStartedAt = WallNow()
	r.phaseDeadline = r.phaseStartedAt + int64(PhaseDurationSec(p, r.Config.PhaseConfig))
	if p == PhaseGameOver {
		r.finishedAt = WallNow()
	}
}

// PhaseDeadline 返回当前阶段截止时间(unix 秒)。
func (r *DebateRoom) PhaseDeadline() int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.phaseDeadline
}

// PhaseTimeRemainingSec 当前阶段剩余秒数。
func (r *DebateRoom) PhaseTimeRemainingSec() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	deadline := r.phaseDeadline
	if deadline == 0 {
		return 0
	}
	rem := deadline - WallNow()
	if rem < 0 {
		return 0
	}
	return int(rem)
}

// CurrentSpeaker 返回当前发言者(读锁)。
func (r *DebateRoom) CurrentSpeaker() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.currentSpeaker
}

// SetCurrentSpeaker 设置当前发言者(写锁,内部用)。
func (r *DebateRoom) SetCurrentSpeaker(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.currentSpeaker = s
}

// AppendSpeech 追加一条发言(写锁)。
func (r *DebateRoom) AppendSpeech(s Speech) {
	r.speeches.Append(s)
}

// Speeches 返回全部发言副本(读锁)。
func (r *DebateRoom) Speeches() []Speech {
	return r.speeches.All()
}

// SpeechesByPhase 按阶段筛选发言(读锁)。
func (r *DebateRoom) SpeechesByPhase(p Phase) []Speech {
	return r.speeches.ByPhase(p)
}

// RecentSpeeches 返回最近 N 条发言(读锁)。
func (r *DebateRoom) RecentSpeeches(n int) []Speech {
	return r.speeches.lastN(n)
}

// AppendCrossExam 追加一条质询(写锁)。
func (r *DebateRoom) AppendCrossExam(e CrossExamEntry) {
	r.crossExam.Append(e)
}

// CrossExamEntries 返回全部质询记录。
func (r *DebateRoom) CrossExamEntries() []CrossExamEntry {
	return r.crossExam.All()
}

// AddJudgeScore 追加一份裁判评分。
func (r *DebateRoom) AddJudgeScore(s JudgeScore) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.judgeScores = append(r.judgeScores, s)
}

// JudgeScores 返回已收集的裁判评分副本。
func (r *DebateRoom) JudgeScores() []JudgeScore {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]JudgeScore, len(r.judgeScores))
	copy(out, r.judgeScores)
	return out
}

// SetResult 设置最终结果。
func (r *DebateRoom) SetResult(res *DebateResult) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.result = res
}

// Result 返回最终结果(可能 nil)。
func (r *DebateRoom) Result() *DebateResult {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.result == nil {
		return nil
	}
	res := *r.result
	return &res
}

// StartedAt 返回比赛开始时间(unix 秒,未开始为 0)。
func (r *DebateRoom) StartedAt() int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.startedAt
}

// FinishedAt 返回比赛结束时间(unix 秒,未结束为 0)。
func (r *DebateRoom) FinishedAt() int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.finishedAt
}

// IsClosed 判断房间是否已关闭。
func (r *DebateRoom) IsClosed() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.closed
}

// SetClosed 标记房间已关闭。
func (r *DebateRoom) SetClosed() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
}

// IsOwner 判断 userID 是否为房主(创建者)。
func (r *DebateRoom) IsOwner(userID string) bool {
	return r.Config.CreatedBy == userID
}

// IsGameStarted 判断比赛是否已开始(进入 PhasePreparation 之后)。
func (r *DebateRoom) IsGameStarted() bool {
	p := r.Phase()
	return p != PhaseFilling
}

// IsGameOver 判断比赛是否已结束。
func (r *DebateRoom) IsGameOver() bool {
	p := r.Phase()
	return p == PhaseGameOver || p == PhaseResult
}

// TeamCount 队伍数。
func (r *DebateRoom) TeamCount() int { return len(r.Config.Teams) }

// JudgeCount 裁判数。
func (r *DebateRoom) JudgeCount() int { return len(r.Config.Judges) }

// Find TeamIndex 返回指定 TeamID 的索引位置(用于发言顺序查找)。
// 找不到返回 -1。
func (r *DebateRoom) TeamIndex(teamID int) int {
	for i, t := range r.Config.Teams {
		if t.TeamID == teamID {
			return i
		}
	}
	return -1
}

// AgentByTeamSeat 查找指定 (teamID, seatID) 的 Agent 配置。
func (r *DebateRoom) AgentByTeamSeat(teamID, seat int) (AgentConfig, bool) {
	for _, t := range r.Config.Teams {
		if t.TeamID != teamID {
			continue
		}
		for _, a := range t.Agents {
			if a.SeatID == seat {
				return a, true
			}
		}
	}
	return AgentConfig{}, false
}

// JudgeByIndex 返回第 idx 个裁判。
func (r *DebateRoom) JudgeByIndex(idx int) (JudgeConfig, bool) {
	if idx < 0 || idx >= len(r.Config.Judges) {
		return JudgeConfig{}, false
	}
	return r.Config.Judges[idx], true
}

// MarkStarted 标记比赛开始(由 DebateManager.StartGame 调用)。
func (r *DebateRoom) MarkStarted() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.startedAt = WallNow()
}

// TeamStances 收集所有队伍的立场(用于"按立场匹配发言顺序")。
func (r *DebateRoom) TeamStances() []Stance {
	out := make([]Stance, len(r.Config.Teams))
	for i, t := range r.Config.Teams {
		out[i] = t.Stance
	}
	return out
}

// MarkFreeDebateUsed 增加指定队伍的自由辩论累计用时。
func (r *DebateRoom) MarkFreeDebateUsed(teamID, sec int) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.freeDebateTimeUsed[teamID] += sec
	return r.freeDebateTimeUsed[teamID]
}

// FreeDebateTimeUsed 查询指定队伍自由辩论累计用时。
func (r *DebateRoom) FreeDebateTimeUsed(teamID int) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.freeDebateTimeUsed[teamID]
}

// SetFreeDebateTurnOwner 设置当前发言权归属。
func (r *DebateRoom) SetFreeDebateTurnOwner(team string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.freeDebateTurnOwner = team
}

// FreeDebateTurnOwner 查询当前发言权归属。
func (r *DebateRoom) FreeDebateTurnOwner() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.freeDebateTurnOwner
}

// SetCrossExamActive 标记质询双方。
func (r *DebateRoom) SetCrossExamActive(q, a string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.crossExamActive = &crossExamPair{
		Questioner: q, Answerer: a, StartedAt: WallNow(),
	}
	r.crossExamTurn++
}

// ClearCrossExamActive 清空质询双方(质询结束 / 切换下一对)。
func (r *DebateRoom) ClearCrossExamActive() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.crossExamActive = nil
}

// CrossExamActive 查询质询双方。
func (r *DebateRoom) CrossExamActive() (q, a string, ok bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.crossExamActive == nil {
		return "", "", false
	}
	return r.crossExamActive.Questioner, r.crossExamActive.Answerer, true
}

// CrossExamTurn 返回质询轮次计数。
func (r *DebateRoom) CrossExamTurn() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.crossExamTurn
}