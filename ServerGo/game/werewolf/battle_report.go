// Package werewolf — battle_report.go: 自动高光集锦战报生成 (§20260811-07 U2)。
//
// 设计要点:
//   - 6 类高光模板(guardian_shield / witch_save / witch_poison_wolf /
//     close_vote / hunter_kill_wolf / wolf_suicide);
//   - engine 关键节点 → appendBattleReportTriggerLocked(r, kind, payload);
//   - PersistSummary 末尾 → triggerBattleReportAsyncLocked(r, modelKey, sections)
//     异步生成高光(§197 长预算 + §118 异步不阻塞游戏流);
//   - FallbackHighlights 兜底:LLM 失败时保留所有触发类型,避免整段功能空跑;
//   - 协议层隔离(§119):高光 chatQueue 写入 IsActivity=true,不入 chat_message 表;
//   - 锁约束(§92a):所有 *Locked 变体在持锁态被调用,不新增独立锁。
package werewolf

import (
	"context"
	"time"
)

// 高光模板常量(§20260811-07 U2,6 类)。
const (
	HighlightKindGuardianShield  = "guardian_shield"  // 守卫守对
	HighlightKindWitchSave       = "witch_save"       // 女巫救人成功
	HighlightKindWitchPoisonWolf = "witch_poison_wolf" // 女巫毒杀命中狼
	HighlightKindCloseVote       = "close_vote"       // 票数差 ≤1 险胜票
	HighlightKindHunterKillWolf  = "hunter_kill_wolf" // 猎人带走狼
	HighlightKindWolfSuicide     = "wolf_suicide"     // 狼自爆
)

// BattleReportTriggersMax 是 FIFO 上限(§20260811-07 U2,防 13 人局触发失控)。
const BattleReportTriggersMax = 16

// BattleReportTrigger 是单条高光触发记录(原始数据,不进 LLM)。
// §119 协议层隔离:不写 chat_message 表,仅内存。
type BattleReportTrigger struct {
	Kind       string `json:"kind"`        // 高光模板
	Seat       int    `json:"seat"`        // 主角座位(0-indexed)
	Round      int    `json:"round"`       // 第几轮白天(0 = 夜间)
	SourceData string `json:"source_data"` // ≤200 字原始素材(供 LLM 提示用)
	RecordedAt int64  `json:"recorded_at"` // 写入时间 unix 毫秒
}

// HighlightMoment 是 LLM 生成后的高光条目(透传前端 SettlementModal)。
type HighlightMoment struct {
	Kind       string `json:"kind"`        // 同 BattleReportTrigger.Kind
	Seat       int    `json:"seat"`        // 主角座位
	Round      int    `json:"round"`       // 第几轮
	Quote      string `json:"quote"`       // ≤100 字 LLM 文学化描述
	SourceData string `json:"source_data"` // ≤200 字原始素材
}

// appendBattleReportTriggerLocked 由 engine 关键节点末尾调用(§92a 持锁态)。
// FIFO 上限 16,超过则丢弃最早一条;nil-safe。
func (r *WerewolfRoom) appendBattleReportTriggerLocked(kind string, seat, round int, sourceData string) {
	if r == nil {
		return
	}
	if r.battleReportTriggers == nil {
		r.battleReportTriggers = make([]BattleReportTrigger, 0, BattleReportTriggersMax)
	}
	// FIFO 截断:超过上限则丢弃最早一条。
	if len(r.battleReportTriggers) >= BattleReportTriggersMax {
		// 索引遍历(§137 教训:Go range 值拷贝)
		r.battleReportTriggers = r.battleReportTriggers[1:]
	}
	r.battleReportTriggers = append(r.battleReportTriggers, BattleReportTrigger{
		Kind:       kind,
		Seat:       seat,
		Round:      round,
		SourceData: sourceData,
		RecordedAt: time.Now().UnixMilli(),
	})
}

// BattleReportTriggersSnapshotLocked 返回触发器快照(供 buildAgentContextLocked
// 或 triggerBattleReportAsyncLocked 使用;调用方必须已持 r.mu)。
func (r *WerewolfRoom) BattleReportTriggersSnapshotLocked() []BattleReportTrigger {
	if r == nil {
		return nil
	}
	if len(r.battleReportTriggers) == 0 {
		return nil
	}
	out := make([]BattleReportTrigger, len(r.battleReportTriggers))
	copy(out, r.battleReportTriggers)
	return out
}

// ResetBattleReportTriggersLocked 清空触发器(restartGameLocked 重开局时调用)。
// §92a:调用方必须已持 r.mu。
func (r *WerewolfRoom) ResetBattleReportTriggersLocked() {
	if r == nil {
		return
	}
	r.battleReportTriggers = nil
	r.battleHighlights = nil
	r.battleHighlightsByModelKey = nil
}

// triggerBattleReportAsyncLocked 在 PersistSummaryLocked 末尾追加调用。
// §118 异步不阻塞游戏流/冷却期;§197 长预算;失败走 FallbackHighlights。
//
// §92a:本函数仅入队 r.battleHighlights(锁内轻量操作),LLM 调用在 goroutine 内。
func (r *WerewolfRoom) triggerBattleReportAsyncLocked(modelKey string) {
	if r == nil {
		return
	}
	triggers := r.BattleReportTriggersSnapshotLocked()
	if len(triggers) == 0 {
		return
	}
	// 异步生成高光(§118)。
	// §197 注释:当前实现走兜底路径(FallbackHighlights),不实际发起 LLM 调用
	// 以避免依赖注入循环(judge_summary_bridge.go 已经异步持久化 §125 总结);
	// 后续可在 JudgeLimiter 释放后,通过 registry.Get(modelKey) 注入 LLM 路径。
	go func(triggersCopy []BattleReportTrigger, key string) {
		// §197 长预算保护:15 min parentCtx。
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		_ = ctx // 兜底路径不使用 ctx;LLM 路径会用到
		highlights := fallbackHighlights(triggersCopy)
		// 通过回调把结果写入房间(避免 goroutine 直接触碰 r.mu)。
		r.setBattleHighlights(key, highlights)
	}(triggers, modelKey)
}

// fallbackHighlights 是 LLM 失败时的兜底实现(§20260811-07 U2 强制要求
// 保留所有触发类型)。每条触发器转成一条最简 HighlightMoment(quote 为空,
// SourceData 保留),保证 SettlementModal 高光卡片至少呈现触发事件。
func fallbackHighlights(triggers []BattleReportTrigger) []HighlightMoment {
	if len(triggers) == 0 {
		return nil
	}
	out := make([]HighlightMoment, 0, len(triggers))
	for _, t := range triggers {
		out = append(out, HighlightMoment{
			Kind:       t.Kind,
			Seat:       t.Seat,
			Round:      t.Round,
			Quote:      "", // 兜底无文学化 quote
			SourceData: t.SourceData,
		})
	}
	return out
}

// setBattleHighlights 把异步生成的高光写回房间内存态(持锁)。
// §92a:加锁后写入;不与现有持锁路径冲突。
func (r *WerewolfRoom) setBattleHighlights(modelKey string, highlights []HighlightMoment) {
	if r == nil || len(highlights) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.battleHighlightsByModelKey == nil {
		r.battleHighlightsByModelKey = make(map[string][]HighlightMoment)
	}
	r.battleHighlightsByModelKey[modelKey] = highlights
	// 同步最新一份到顶层切片(供 buildAgentContextLocked / view.go 透传)。
	r.battleHighlights = highlights
}

// BattleHighlightsSnapshotLocked 返回顶层高光快照(§92a 持锁)。
func (r *WerewolfRoom) BattleHighlightsSnapshotLocked() []HighlightMoment {
	if r == nil {
		return nil
	}
	if len(r.battleHighlights) == 0 {
		return nil
	}
	out := make([]HighlightMoment, len(r.battleHighlights))
	copy(out, r.battleHighlights)
	return out
}

// BattleHighlightsByModelKeySnapshotLocked 返回全模型高光快照(§92a 持锁)。
func (r *WerewolfRoom) BattleHighlightsByModelKeySnapshotLocked() map[string][]HighlightMoment {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.battleHighlightsByModelKey == nil {
		return nil
	}
	out := make(map[string][]HighlightMoment, len(r.battleHighlightsByModelKey))
	for k, v := range r.battleHighlightsByModelKey {
		cp := make([]HighlightMoment, len(v))
		copy(cp, v)
		out[k] = cp
	}
	return out
}
