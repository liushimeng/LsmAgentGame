// Package werewolf — judge_summary_bridge.go: 法官「整局总结」房间侧集成。
//
// 2026-07-10 §125 增强。独立于 room.go / judge.go,集中实现以下内容:
//   - (r *WerewolfRoom).GenerateSummary / PersistSummary 实现 wwjudge.JudgeSummaryBridge
//   - (r *WerewolfRoom).BuildSummaryInputLocked: 把 room 状态压缩成 SummaryInput
//   - appendJudgeSummaryLocked / PersistSummaryLocked / ModelMemoryLocked / LastSummaryLocked
//   - SetSummaryManagerInstance / GetSummaryManagerInstance:全局 manager 单例
//   - (m *WerewolfManager).startJudgeGoroutine: 法官 goroutine 生命周期
//
// 集成点:
//   - 房间创建时: m.startJudgeGoroutine(r) 由 room.go patch 调用
//   - phaseWatchdogTick 检测到 Status="over" 时: wwjudge.EmitGameOverSummary(j, modelKey, in)
//   - BuildSystemPrompt 注入「上一局记忆」段: wwplayer.LastGameMemoryBlock(modelKey, r.ModelMemoryLocked(modelKey))
package werewolf

import (
	"context"
	"fmt"
	"sync"
	"time"

	"LsmWebGame/agent/wwjudge"
	"LsmWebGame/agent/core"
	"LsmWebGame/llm"
	"LsmWebGame/logger"
	"go.uber.org/zap"
)

// ─────────────────── 全局 manager 单例注入 ───────────────────

var (
	summaryManagerOnce sync.Once
	summaryManagerInst *WerewolfManager
)

// SetSummaryManagerInstance 由 main.go 在启动时调用。
func SetSummaryManagerInstance(m *WerewolfManager) {
	summaryManagerOnce.Do(func() {
		summaryManagerInst = m
	})
}

// GetSummaryManagerInstance 获取注入的 manager 单例。
func GetSummaryManagerInstance() *WerewolfManager {
	return summaryManagerInst
}

// ─────────────────── Bridge 接口实现 ───────────────────

// GenerateSummary 实现 wwjudge.JudgeSummaryBridge 接口。
//
// 模型解析优先级(R137 / 2026-07-22):
//  1. r.JudgeModelKey(房间级显式,创建者从 UI 选定或 service 随机填入);
//  2. cfgWerewolfJudgeModelKey()(LsmWebGame.conf 全局兜底);
//  3. **recovery only**:遍历 r.seatModelKeys 取首个非空 key —— 仅当 1+2
//     都没填时触发(典型场景:老房间从磁盘恢复,rooms[roomID].JudgeModelKey
//     字段为零值)。正常 CreateRoomWithAgents 路径下,service 已经在 SetJudgeConfig
//     阶段把显式或随机挑出的 judgeModelKey 落到了 r.JudgeModelKey,所以不会
//     走到 3) —— 依赖 map 任意首项的"脆弱回退"被消除。
//  4. "judge-default" 字面量 —— 仅注册表里有同名 provider 时才能工作,
//     否则 registry.Get 返回 error 走 FallbackSummary。
func (r *WerewolfRoom) GenerateSummary(in wwjudge.SummaryInput) (wwjudge.SummarySections, string) {
	if !lockRoomBriefly(r, 500*time.Millisecond) {
		return wwjudge.SummarySections{}, "lock contention"
	}
	defer r.mu.Unlock()
	in = r.BuildSummaryInputLocked()
	prompt := wwjudge.BuildSummaryPrompt(in)
	mgr := GetSummaryManagerInstance()
	if mgr == nil || mgr.registry == nil {
		return wwjudge.SummarySections{Outcome: wwjudge.FallbackSummary(in, "no manager")}, ""
	}
	// 1) 房间级显式 / service 随机挑出的法官模型(权威值)。
	modelKey := r.JudgeModelKey
	// 2) 全局兜底。
	if modelKey == "" {
		modelKey = cfgWerewolfJudgeModelKey()
	}
	// 3) Recovery-only:遍历 seatModelKeys 取首个非空 key。仅当 1+2 都未配置
	//    时触发,例如老房间从磁盘恢复且 JudgeModelKey 字段未落库。正常建房
	//    路径下不会走到这一步。
	if modelKey == "" && len(r.seatModelKeys) > 0 {
		for _, k := range r.seatModelKeys {
			if k != "" {
				modelKey = k
				logger.L().Warn("GenerateSummary: recovery-only seatModelKeys fallback used (no room/global judge model set)",
					zap.String("room_id", r.RoomID),
					zap.String("model_key", k))
				break
			}
		}
	}
	// 4) 终末兜底(注册表里可能没有同名 provider,会走 error → FallbackSummary)。
	if modelKey == "" {
		modelKey = "judge-default"
	}
	provider, key, err := mgr.registry.Get(modelKey)
	if err != nil {
		return wwjudge.SummarySections{Outcome: wwjudge.FallbackSummary(in, err.Error())}, err.Error()
	}
	req := llm.LLMRequest{
		Model: modelKey,
		Messages: []llm.Message{
			{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: prompt}}},
		},
		MaxTokens: 1024,
		// 协议对齐:与普通 bot / 法官逐阶段旁白共用 buildJudgeMetadataUserID
		// 形态,便于上游 proxy 与审计聚合。
		Metadata: llm.Metadata{
			UserID: wwjudge.BuildJudgeMetadataUserID(r.RoomID, modelKey),
		},
	}
	resp, err := provider.Chat(context.Background(), key, req)
	if err != nil {
		return wwjudge.SummarySections{Outcome: wwjudge.FallbackSummary(in, err.Error())}, err.Error()
	}
	text := resp.Text()
	if text == "" {
		return wwjudge.SummarySections{Outcome: wwjudge.FallbackSummary(in, "empty response")}, "empty response"
	}
	return wwjudge.ParseSummary(text), ""
}

// PersistSummary 实现 wwjudge.JudgeSummaryBridge 接口。
func (r *WerewolfRoom) PersistSummary(modelKey string, sections wwjudge.SummarySections) {
	if sections.IsEmpty() {
		return
	}
	if !lockRoomBriefly(r, 500*time.Millisecond) {
		logger.L().Warn("werewolf: PersistSummary lock contention, dropping",
			zap.String("room_id", r.RoomID))
		return
	}
	r.appendJudgeSummaryLocked(modelKey, sections)
	r.persistSummaryLocked(modelKey, sections)
	// §20260811-07 U2 — 战报异步生成触发器。锁内仅入队内存快照,
	// goroutine 内部走 §197 长预算 + §118 异步不阻塞游戏流。
	r.triggerBattleReportAsyncLocked(modelKey)
	r.mu.Unlock()
	// 2026-07-20 §131 新增 — 总结落地后触发各 bot 模型的持久化记忆自我迭代。
	// 异步、不阻塞(对齐 §118);开关关闭 / store 未注入时 IterateAgentMemoriesAsync 内 no-op。
	// 注意必须锁外触发(r.mu 已在上方释放),迭代 goroutine 内部会自行 lockRoomBriefly 快照。
	if mgr := GetSummaryManagerInstance(); mgr != nil {
		mgr.IterateAgentMemoriesAsync(r, wwjudge.FlattenSummary(sections))
		// 2026-08-11 §20260811-05 U1 — 同点触发 (bot × 人类) 玩家行为画像迭代。
		// 同样异步不阻塞;开关关闭 / store 未注入 / 全 AI 房间时 no-op。
		mgr.IteratePlayerProfilesAsync(r)
		// §20260811-10 U4 — 同点触发 WerewolfIQ 异步评估。
		// 与记忆迭代 + 画像迭代同模式:异步、不阻塞、失败仅 logger.Warn。
		// reputationSvc 未注入时整链 no-op,测试环境 / 老部署零感知。
		mgr.ComputeWerewolfIQAsync(r)
	}
}

// ─────────────────── 法官 goroutine 生命周期 ───────────────────

// startJudgeGoroutine 启动 AgentJudge goroutine。
//
// 2026-07-16 主持人重构 — 三处修复:
//   - 🔴1 Provider 注入:此前 j.Provider/j.apiKey 从未赋值 → judgeChatOrFallback
//     入口守卫永远成立 → 旁白永不调 LLM。现用 registry.Get(modelKey) 解析后注入;
//   - 🟡2 Agent 数量守卫:此前全人类房(0 Agent)也启动法官。现要求房间级
//     JudgeDesired=true 且 len(seatModelKeys)≥1 才启动;
//   - 广播回调 onAnnounce 注入:announce/declare_cause 成功后经 chatSvc 送公屏。
func (m *WerewolfManager) startJudgeGoroutine(r *WerewolfRoom) {
	// 2026-07-30 §重构:全局 cfg.Werewolf.JudgeMode 现在支持 "agent"(默认)/"human"/"off"。
	// "off" 仍是运维级别的全局 kill switch;房间级 r.JudgeMode 不再被读(后端两种
	// 房间级 mode 都启用 AgentJudge),但保留 r.JudgeDesired 字段作为
	// 「未来真人法官 WS 契约落地后」的分流钩子。
	if cfgWerewolfJudgeMode() == "off" {
		logger.L().Info("werewolf: JudgeMode=off (global cfg), skipping judge goroutine",
			zap.String("room_id", r.RoomID))
		return
	}
	// 🟡2: 房间级关闭(预留,目前创建者 UI 不暴露)或全人类房(0 Agent)→不启动法官。
	if !r.JudgeDesired {
		logger.L().Info("werewolf: judge disabled by room setting, skipping judge goroutine",
			zap.String("room_id", r.RoomID))
		return
	}
	if len(r.seatModelKeys) == 0 {
		logger.L().Info("werewolf: no agents, skipping judge goroutine",
			zap.String("room_id", r.RoomID))
		return
	}
	if r.judge != nil {
		return
	}
	modelKey := cfgWerewolfJudgeModelKey()
	// 房间级指定模型(创建者选 judge.model_key)优先。
	if r.JudgeModelKey != "" {
		modelKey = r.JudgeModelKey
	}
	if modelKey == "" && len(r.seatModelKeys) > 0 {
		for _, k := range r.seatModelKeys {
			if k != "" {
				modelKey = k
				break
			}
		}
	}
	if modelKey == "" {
		modelKey = "judge-default"
	}
	j := wwjudge.NewAgentJudge(r.RoomID, modelKey)
	// 🔴1: 注入 provider + apiKey(与 wwplayer.NewWithRoom 同路径,拒绝占位 key)。
	// 必须在 j.Run 启动前完成,goroutine 内只读 j.Provider/j.apiKey。
	if m.registry != nil {
		if provider, key, err := m.registry.Get(modelKey); err != nil {
			logger.L().Warn("judge: registry.Get failed, judge will use rule fallback",
				zap.String("room_id", r.RoomID), zap.String("model_key", modelKey), zap.Error(err))
		} else {
			j.SetProvider(provider, key)
		}
		// §R224 (2026-08-01) — 注入 registry 引用,供 judgeChatOrFallback 通过
		// GetThinkingEnabled(modelKey) 决定是否在 req.Thinking 上挂 extended
		// thinking 配置(与 agent.callProvider 同形状,见 BUG-NEW-1)。
		j.Registry = m.registry
	} else {
		logger.L().Warn("judge: manager registry nil, judge will use rule fallback",
			zap.String("room_id", r.RoomID))
	}
	// 注入广播回调:announce/declare_cause 成功后送公屏(goroutine 内只读)。
	if m.chatSvc != nil {
		j.SetOnAnnounceBroadcast(func(roomID, text, kind string) {
			fromAccount := "[法官·" + modelKey + "]"
			if _, err := m.chatSvc.SendFromJudge(roomID, fromAccount, modelKey, text, kind); err != nil {
				logger.L().Warn("judge: SendFromJudge failed",
					zap.String("room_id", roomID), zap.String("kind", kind), zap.Error(err))
			}
		})
	}
	r.judgeEvents = j.Events()
	r.judge = j
	// 注入 per-room bridge(judge goroutine 通过全局 summaryBridge 反查)。
	// 由于全局 bridge 单实例,我们用当前房间的 r 作为 source;多房间时
	// "后启动的覆盖先启动的" — 这是 §125 的简化设计,生产场景单 manager
	// 通常 ≤ 几十个并发房间,LLM 调用本来就限频,无冲突风险。
	wwjudge.SetSummaryBridge(r)
	ctx, cancel := context.WithCancel(context.Background())
	r.judgeCancel = cancel
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				logger.L().Warn("werewolf: judge goroutine panicked",
					zap.String("room_id", r.RoomID),
					zap.Any("recover", rec))
			}
		}()
		j.Run(ctx)
	}()
	logger.L().Info("werewolf: judge goroutine started",
		zap.String("room_id", r.RoomID),
		zap.String("model_key", modelKey))
}

// ─────────────────── chatQueue + modelMemories 持久化 ───────────────────

// appendJudgeSummaryLocked 把法官生成的 5 段总结以 IsActivity=true 形式注入 chatQueue。
func (r *WerewolfRoom) appendJudgeSummaryLocked(modelKey string, sections wwjudge.SummarySections) {
	if r.chatQueue == nil {
		return
	}
	if sections.IsEmpty() {
		return
	}
	flat := wwjudge.FlattenSummary(sections)
	ts := time.Now().UnixMilli()
	tsTime := time.UnixMilli(ts)
	fromAccount := "[法官总结]"
	if modelKey != "" {
		fromAccount = "[法官总结·" + modelKey + "]"
	}
	r.chatQueue.Append(agentcore.ChatMessage{
		ID:           fmt.Sprintf("js:%s:%d", r.RoomID, ts),
		FromSeat:     -1,
		FromID:       "judge:summary:" + modelKey,
		AgentName:    modelKey,
		FromAccount:  fromAccount,
		IsBot:        false,
		IsSpectator:  false,
		IsWhisper:    false,
		Text:         flat,
		Timestamp:    tsTime,
		IsActivity:   true,
		EventKind:    "judge_summary",
		ActivityIcon: "📜",
	})
}

// persistSummaryLocked 把总结 append 到 modelMemories(按 modelKey 索引) + lastSummary。
func (r *WerewolfRoom) persistSummaryLocked(modelKey string, sections wwjudge.SummarySections) {
	if sections.IsEmpty() {
		return
	}
	r.memoryMutex.Lock()
	defer r.memoryMutex.Unlock()
	if r.modelMemories == nil {
		r.modelMemories = make(map[string][]string, len(r.seatModelKeys))
	}
	flat := wwjudge.FlattenSummary(sections)
	r.modelMemories[modelKey] = []string{flat}
	r.lastSummary = flat
}

// ModelMemoryLocked 取指定 modelKey 的"上一局记忆"切片(防御性 copy)。
func (r *WerewolfRoom) ModelMemoryLocked(modelKey string) []string {
	if modelKey == "" {
		return nil
	}
	r.memoryMutex.Lock()
	defer r.memoryMutex.Unlock()
	if r.modelMemories == nil {
		return nil
	}
	src, ok := r.modelMemories[modelKey]
	if !ok || len(src) == 0 {
		return nil
	}
	out := make([]string, len(src))
	copy(out, src)
	return out
}

// LastSummaryLocked 返回最近一次法官总结。
func (r *WerewolfRoom) LastSummaryLocked() string {
	r.memoryMutex.Lock()
	defer r.memoryMutex.Unlock()
	return r.lastSummary
}

// ModelMemoriesSnapshotLocked 返回全模型记忆快照。
func (r *WerewolfRoom) ModelMemoriesSnapshotLocked() map[string][]string {
	r.memoryMutex.Lock()
	defer r.memoryMutex.Unlock()
	if r.modelMemories == nil {
		return nil
	}
	out := make(map[string][]string, len(r.modelMemories))
	for k, v := range r.modelMemories {
		cp := make([]string, len(v))
		copy(cp, v)
		out[k] = cp
	}
	return out
}

// BuildSummaryInputLocked 把当前 GameState 压缩成 wwjudge.SummaryInput。
func (r *WerewolfRoom) BuildSummaryInputLocked() wwjudge.SummaryInput {
	in := wwjudge.SummaryInput{
		RoomID:    r.RoomID,
		DayNumber: r.State.DayNumber,
		Winner:    r.State.Winner,
	}
	if r.State.SeatCount > 0 {
		in.DayNumber = r.State.DayNumber + 1
	}
	in.Roles = make(map[int]string, MaxPlayers)
	for i := 0; i < MaxPlayers; i++ {
		if r.State.Seats[i] == "" {
			continue
		}
		in.Roles[i] = r.State.Roles[i].String()
		if r.State.AliveSeat(Seat(i)) {
			in.AliveSeats = append(in.AliveSeats, i)
		} else {
			in.DeadSeats = append(in.DeadSeats, i)
		}
	}
	for i := 0; i < MaxPlayers; i++ {
		if r.State.Seats[i] != "" && !r.State.AliveSeat(Seat(i)) {
			p := r.State.Players[i]
			in.Deaths = append(in.Deaths, wwjudge.DeathEvent{
				Seat:    i,
				Cause:   p.DeathCause,
				Verdict: p.DeathVerdict,
				Day:     r.State.DayNumber,
				Round:   fmt.Sprintf("D%d·%d号(%s)", r.State.DayNumber, i+1, p.DeathCause),
			})
		}
	}
	maxSpeech := 30
	if len(r.recentSpeeches) < maxSpeech {
		maxSpeech = len(r.recentSpeeches)
	}
	for i := len(r.recentSpeeches) - maxSpeech; i < len(r.recentSpeeches); i++ {
		if i < 0 {
			continue
		}
		sp := r.recentSpeeches[i]
		in.Speeches = append(in.Speeches, fmt.Sprintf("%d号(?): %s",
			sp.Seat+1, truncateForSummary(sp.Text, 100)))
	}
	if r.chatQueue != nil {
		tail := r.chatQueue.Tail(80)
		for _, m := range tail {
			if m.Text == "" {
				continue
			}
			in.ChatTail = append(in.ChatTail, m.Text)
		}
	}
	in.WinnerSeat = inferMVPSeat(r)
	return in
}

func truncateForSummary(s string, max int) string {
	if max <= 0 || s == "" {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

func inferMVPSeat(r *WerewolfRoom) int {
	if r.State == nil {
		return -1
	}
	if r.State.Winner == "good" {
		for i := MaxPlayers - 1; i >= 0; i-- {
			if r.State.AliveSeat(Seat(i)) && (r.State.Roles[i] == RoleSeer || r.State.Roles[i] == RoleWitch) {
				return i
			}
		}
	}
	if r.State.Winner == "wolf" {
		for i := MaxPlayers - 1; i >= 0; i-- {
			if r.State.AliveSeat(Seat(i)) && r.State.Roles[i] == RoleWerewolf {
				return i
			}
		}
	}
	return -1
}

// ─────────────────── phaseWatchdogTick 唤醒法官 ───────────────────

// wakeJudgeLocked 在 phase 切换或 game over 时把对应 JudgeEvent 投递给法官 goroutine。
// 非阻塞:events channel 满则丢弃。
//
// 2026-07-12 §127 增强 — 调用前在锁内构造 wwjudge.GameSnapshot(完整活/死/警长/胜方/
// 投票快照等),让 judge LLM 在 system prompt 之外也能拿到权威状态,生成的宣告与
// 实际场上状态一致;否则 LLM 容易"幻觉式"宣告(编造死人/警徽)。
func (r *WerewolfRoom) wakeJudgeLocked(kind string, extra map[string]any) {
	if r.judge == nil {
		return
	}
	var snap wwjudge.GameSnapshot
	if !lockRoomBriefly(r, 500*time.Millisecond) {
		// 锁争用 — 仍投递事件但快照为空,judge 会走 fallback 文本兜底。
	} else {
		snap = r.buildJudgeSnapshotLocked(kind)
		r.mu.Unlock()
	}
	evt := wwjudge.JudgeEvent{
		Kind:  kind,
		Snap:  &snap,
		Extra: extra,
		At:    time.Now(),
	}
	// 直接用 channel send(非阻塞);judge 包有缓冲 32 满则丢。
	select {
	case r.judgeEvents <- evt:
	default:
		logger.L().Warn("werewolf: judge events channel full, dropping event",
			zap.String("room_id", r.RoomID),
			zap.String("kind", kind))
	}
}

// buildJudgeSnapshotLocked 在 room.mu 持有下构造法官 GameSnapshot。
// 必须持锁调用,因为读取 Seats/Players/Roles/Sheriff 等共享可变状态。
//
// 字段填法(2026-07-12 §127):
//   - AliveSeats / DeadSeats:遍历 Seats,按 Players[i].Alive 分桶;
//   - SheriffSeat: 直接读 GameState.SheriffSeat 字段(0-indexed 座位 / NoSeat);
//   - WolfSeats: 仅法官可见(不能告诉真人玩家),用于 prompt 中给 LLM 提供阵营视角;
//   - Votes: 从 Players[i].VoteTarget 派生(仅当 PhaseVote 时);
//   - LastDeathCause / Verdict: 取任意一个已死亡玩家的 (DeathCause, DeathVerdict);
//     因为对局通常一轮最多死 1~2 人,LLM 不必死记 round 数;
//   - Winner: Status="over" 时 = GameState.Winner;
//   - IsHumanInRoom: 真人座位不在 BotAgents 中,真人存在 = r.Seats 中至少一个
//     ID 在 BotAgents keyset 之外(BotAgents 不含 judge);
//   - PhaseDeadlineSec: r.State.PhaseDeadlineAt 距离 now 的剩余秒数,负数=已过期。
//
// 调用方:必须已持有 r.mu 并由调用方负责 defer r.mu.Unlock();本函数不重新加锁。
func (r *WerewolfRoom) buildJudgeSnapshotLocked(eventKind string) wwjudge.GameSnapshot {
	snap := wwjudge.GameSnapshot{Phase: eventKind, SheriffSeat: int(NoSeat)}
	if r.State == nil {
		return snap
	}
	snap.Phase = r.State.Phase.String()
	snap.Day = r.State.DayNumber
	snap.Winner = r.State.Winner
	alive, dead := []int{}, []int{}
	wolves := []int{}
	for i, uid := range r.State.Seats {
		if uid == "" {
			continue
		}
		p := r.State.Players[i]
		if p.Alive {
			alive = append(alive, i)
		} else {
			dead = append(dead, i)
		}
		if r.State.Roles[i] == RoleWerewolf {
			wolves = append(wolves, i)
		}
	}
	snap.AliveSeats = alive
	snap.DeadSeats = dead
	snap.WolfSeats = wolves
	// Sheriff: 直接读 SheriffSeat 字段
	snap.SheriffSeat = int(r.State.SheriffSeat)
	// 投票快照:从 Players[i].VoteTarget 派生(当前轮所有已投票玩家的目标)。
	// 仅当 PhaseVote 时字段非空;其它阶段留空。
	if r.State.Phase == PhaseVote {
		votes := make([]string, 0, len(r.State.Seats))
		for i := range r.State.Seats {
			if r.State.Seats[i] == "" || !r.State.Players[i].Alive {
				continue
			}
			if !r.State.Players[i].Voted {
				continue
			}
			target := r.State.Players[i].VoteTarget
			if target == NoSeat {
				votes = append(votes, fmt.Sprintf("%d→弃权", i+1))
			} else {
				votes = append(votes, fmt.Sprintf("%d→%d", i+1, int(target)+1))
			}
		}
		snap.Votes = votes
	}
	// SpeakOrder 快照(§20260810-02 E2):此前 wwjudge.GameSnapshot.SpeakOrder 声明后
	// 零生产写入点(K3-Surpport-01 §1 F2),法官在 judge_speak_start 唤醒时看不到本轮
	// 发言顺序。数据源 r.State.SpeakOrder 由 engine_day.go 正常维护。
	//
	// 与 Votes 一致地限定在 PhaseSpeak:SpeakOrder 在平票 PK(engine_day.go:586)与
	// 警长竞选(engine_day.go:253)时会被复用为「参与者列表」,非发言阶段下发会让法官误读。
	if r.State.Phase == PhaseSpeak && len(r.State.SpeakOrder) > 0 {
		order := make([]int, 0, len(r.State.SpeakOrder))
		for _, s := range r.State.SpeakOrder {
			order = append(order, int(s))
		}
		snap.SpeakOrder = order
	}
	// 最近死亡原因 / verdict:取第一个有 DeathCause 的玩家(对局一轮最多死 1~2
	// 人,LLM 看到 cause 已足够;严格 round 排序需要 GameState 侧加 DeathRound,
	// 这里先取第一条).
	for _, p := range r.State.Players {
		if p.DeathCause == "" {
			continue
		}
		snap.LastDeathCause = p.DeathCause
		snap.LastDeathVerdict = p.DeathVerdict
		break
	}
	// 真人探测:BotAgents 含本局所有 bot(含 judge 之外);真人座位不在 BotAgents 中
	for i, uid := range r.State.Seats {
		if uid == "" {
			continue
		}
		if _, isBot := r.BotAgents[i]; !isBot {
			snap.IsHumanInRoom = true
			break
		}
	}
	// Deadline 剩余秒数
	if !r.State.PhaseDeadlineAt.IsZero() {
		remain := time.Until(r.State.PhaseDeadlineAt)
		snap.PhaseDeadlineSec = int(remain.Seconds())
	}
	return snap
}

// wakeJudgeLockedForSummaryLocked 触发法官「整局总结」(2026-07-10 §125 增强)。
// 由 phaseWatchdogTick 在 Status="over" 末尾调用。
// 非阻塞:events channel 满则丢。
func (r *WerewolfRoom) wakeJudgeLockedForSummaryLocked() {
	if r.judge == nil {
		return
	}
	if !lockRoomBriefly(r, 500*time.Millisecond) {
		return
	}
	in := r.BuildSummaryInputLocked()
	judgeModel := r.judge.ModelKey
	if judgeModel == "" {
		judgeModel = "judge-default"
	}
	r.mu.Unlock()
	wwjudge.EmitGameOverSummary(r.judge, judgeModel, in)
}
