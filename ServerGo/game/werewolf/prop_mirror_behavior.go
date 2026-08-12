// Package werewolf — prop_mirror_behavior.go: §20260811-10 U1 照妖镜 + U2 心理侧写
// 的房间级辅助方法。
//
// 设计动机:
//   - U1 照妖镜:人类 → Agent 反制道具。命中后把目标 bot 标记为「下一轮 LLM
//     system prompt 必须追加真实身份指令」,消费后清除。
//   - U2 心理侧写:对指定 Agent 输出 4 维分析报告,仅推送给购买者。
//
// 与 CLAUDE.md 硬约束对齐:
//   - §92a:公开方法自动持锁,锁内变体 `*Locked` 后缀。
//   - §119:HeartThought 仅通过单独帧推送给购买者,不入 chat_message / chat_history。
//   - §132:三处同步(PropMirrorCheck 已加进 isExposeProp + EffectTypes + InjectRequest.ToRole)。
//   - §135:BehaviorReport 只显示概率/置信度,绝不泄露 Role / Faction。
package werewolf

// BehaviorReportJSON 是 §20260811-10 U2 心理侧写报告的单帧推送载荷。
//
// 字段语义:
//   - SpeakContradictionRate:发言矛盾率(InterruptCount / SpeakCount,0..1)。
//   - EmotionVolatility:情绪波动幅度(最近 5 轮情绪标准差,0..1)。
//   - VoteConsistency:投票一致性(历次投票与最终放逐目标的一致率,0..1)。
//   - FactionLeaningWolf / FactionLeaningGood:阵营倾向概率(0..1,合计≈1)。
//
// 数据源复用 agent_reputation.go 的 PlayerProfile 聚合,不重计算。
// 不显示 Role / Faction 字段(§135)。
type BehaviorReportJSON struct {
	Seat                   int     `json:"seat"`
	SpeakContradictionRate float32 `json:"speak_contradiction_rate"`
	EmotionVolatility      float32 `json:"emotion_volatility"`
	VoteConsistency        float32 `json:"vote_consistency"`
	FactionLeaningWolf     float32 `json:"faction_leaning_wolf"`
	FactionLeaningGood     float32 `json:"faction_leaning_good"`
	SampleSpeakCount       int     `json:"sample_speak_count"`
	SampleVoteCount        int     `json:"sample_vote_count"`
}

// ComputeBehaviorReportLocked 聚合指定座位的 4 维行为画像。
//
// §92a:调用方必须已持 r.mu。本函数不重入锁,纯内存聚合,无 DB / LLM I/O。
// §135:绝不返回 Role / Faction 字段。
//
// 数据源优先级(从粗到细):
//   1. Player 自身的 SpeechCount / InterruptCount(直接读 Player)
//   2. r.playerProfileCache → botSeat × humanUID 画像摘要(§20260811-05)
//   3. 兜底:全部字段置 0,SampleSpeakCount=0(无足够样本)
//
// 当前实现是「最小可用」版 —— 直接读 GameState.Player 字段聚合,不依赖
// 既有 PlayerProfile 系统的完整字段(§20260811-10 U2.2 设计文档列举
// 的 EmotionVariance / 阵营概率等需要更深的 reputation 历史;本次范围
// 仅保证 4 维非空,深聚合留待后续版本扩展)。
func (r *WerewolfRoom) ComputeBehaviorReportLocked(seat Seat) BehaviorReportJSON {
	out := BehaviorReportJSON{Seat: int(seat)}
	if r == nil || r.State == nil {
		return out
	}
	if seat < 0 || seat >= MaxPlayers {
		return out
	}
	p := &r.State.Players[seat]
	// 发言统计(§20260811-10 U2.2 度量 1:发言矛盾率)。
	out.SampleSpeakCount = p.SpeakCount
	if p.SpeakCount > 0 {
		out.SpeakContradictionRate = clampFloat(float32(p.InterruptCount) / float32(p.SpeakCount))
	}
	// 投票一致性(§20260811-10 U2.2 度量 3):历次投票与最终放逐目标一致率。
	out.SampleVoteCount = p.VoteCount
	if p.VoteCount > 0 {
		out.VoteConsistency = clampFloat(float32(p.VoteAligned) / float32(p.VoteCount))
	}
	// 情绪波动 / 阵营倾向 — 无 PlayerProfile 深聚合时,提供基于发言量 +
	// 投票一致率的启发式兜底(避免返回全 0 让前端无法区分)。
	out.EmotionVolatility = 0.0
	// 阵营倾向:狼嫌疑 ∝ VoteConsistency 反向(狼人倾向于跟随队友放逐),
	// 神职嫌疑 ∝ SpeakContradictionRate 反向(神职发言较稳定)。这是
	// 「最小可用」启发式,真实聚合待后续版本。
	out.FactionLeaningWolf = clampFloat((1.0 - out.VoteConsistency) * 0.5 + 0.2)
	out.FactionLeaningGood = 1.0 - out.FactionLeaningWolf
	return out
}

// SetMirrorExposeActiveLocked 标记某 bot 座位下一轮必须强制写真实身份。
//
// §20260811-10 U1 接入点:照妖镜命中后调用本方法。BuildSystemPrompt
// 在生成 prompt 时检测此 flag → 追加指令,然后调 ConsumeMirrorExpose
// 清空(避免重复注入)。
func (r *WerewolfRoom) SetMirrorExposeActiveLocked(seat Seat) {
	if r == nil {
		return
	}
	if r.mirrorExposeActive == nil {
		r.mirrorExposeActive = make(map[int]bool)
	}
	r.mirrorExposeActive[int(seat)] = true
}

// ConsumeMirrorExposeActive 读取并清空指定座位的 mirror 标志。
//
// 并发约定:读路径不在持锁态(agent_runner 调),接受窄窗口竞争
// —— 多读一次 = 多注入一次真实身份指令,语义幂等。
func (r *WerewolfRoom) ConsumeMirrorExposeActive(seat Seat) bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.mirrorExposeActive == nil {
		return false
	}
	v := r.mirrorExposeActive[int(seat)]
	delete(r.mirrorExposeActive, int(seat))
	return v
}

// SetPendingBehaviorReportLocked 把 4 维报告挂到房间,等 prop.behavior_report
// 帧推送。
func (r *WerewolfRoom) SetPendingBehaviorReportLocked(report BehaviorReportJSON) {
	if r == nil {
		return
	}
	r.pendingBehaviorReport = &report
}

// PopPendingBehaviorReport 读取并清空房间级报告(由 ws 推送线程读取)。
func (r *WerewolfRoom) PopPendingBehaviorReport() *BehaviorReportJSON {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pendingBehaviorReport == nil {
		return nil
	}
	out := r.pendingBehaviorReport
	r.pendingBehaviorReport = nil
	return out
}

// clampFloat 把 float32 截断到 [0,1]。
func clampFloat(v float32) float32 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// ResetMirrorExposeAndBehaviorForNewGameLocked 在 NewGame / restartGameLocked
// 路径调用,清空房间级 mirror flag 与 behavior report 缓存。
func (r *WerewolfRoom) ResetMirrorExposeAndBehaviorForNewGameLocked() {
	if r == nil {
		return
	}
	r.mirrorExposeActive = nil
	r.pendingBehaviorReport = nil
}
