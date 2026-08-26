// Package werewolf — impression_memory.go: 狼人杀 13 人局 Agent 记忆印象（§20260826-01 U2）。
//
// 设计动机（DeepSeek §一.1 / Gemini §一.2 / M3 §5 P1）：
//   - 假说表 HypothesisTable 是「身份层」(ta 是狼还是预言家);
//   - 印象表 ImpressionMemory 是「人格层」(ta 这个人可信吗、危险吗)。
//   - 两者正交不可替代:同一人,身份是 villager 但印象是 high_threat(因为反复发起质疑)。
//
// 数据流:
//   - 每次游戏事件(E1~E5,见 impression_aggregator.go)调
//     impressionStore.AddOrUpdateDimLocked 自动累加/衰减。
//   - buildAgentContextLocked 末尾调 GetImpressionLocked 填到 gc.ImpressionMemory。
//   - BuildClientStateWithRoom(viewer==-1) 末尾调 populateImpressions 下发
//     cs.BotImpressions[]（§135 spectator 隔离）。
//
// §92a 锁约束：所有 *Store 方法均为 *Locked 变体。
// §119 协议层隔离：ImpressionMemory **不**写 chat_message / chat_history / HeartThought。
// §135 spectator 隔离：viewer>=0 时 omitempty；viewer==-1 全量下发。
// §128 对话即思考：不新独立 LLM 调用；服务端聚合纯计算。
package werewolf

import (
	"math"
	"sort"
	"time"
)

// ImpressionDecayHalfLifeHours 信任/真诚/合作 维度的衰减半衰期（48h）。
// 衰减公式: dims[i] = (dims[i] - 0.5) * 0.5^(elapsed_h / half_life_h) + 0.5
// （向 0.5 中性值收敛，而非 0;这样「强烈信任」会随时间变淡成「中性」而非「强烈不信任」）。
const ImpressionDecayHalfLifeHours = 48.0

// ImpressionCompetenceHalfLifeHours 能力维度的衰减半衰期（24h，能力观察易过期）。
const ImpressionCompetenceHalfLifeHours = 24.0

// ImpressionThreatNoDecay = true 表示 Threat 维度**不衰减**(危险感累积)。
const ImpressionThreatNoDecay = true

// impressionNeutralAnchor 衰减的锚点（中性值）。
const impressionNeutralAnchor = float32(0.5)

// ImpressionDims 5 维印象评分（0~1）。
type ImpressionDims struct {
	Trust       float32 `json:"trust"`        // ta 说的话我信多少
	Competence  float32 `json:"competence"`   // ta 推理是否到位
	Sincerity   float32 `json:"sincerity"`    // ta 是不是在玩心理战
	Cooperation float32 `json:"cooperation"`  // ta 是否愿意站我这边
	Threat      float32 `json:"threat"`       // ta 对我多危险
}

// ImpressionEntry 单条印象（per bot × per target）。
type ImpressionEntry struct {
	TargetSeat   int            `json:"target_seat"`
	Dims         ImpressionDims `json:"dims"`
	LastUpdateMS int64          `json:"last_update_ms"`
	EventCount   int            `json:"event_count"`   // 累积观察次数
	SampleEvents []string       `json:"sample_events"` // ≤5 条最近观察的简短描述
}

// ImpressionMemory per bot 的印象集合。
type ImpressionMemory struct {
	Seat      int               `json:"seat"`
	Entries   []ImpressionEntry `json:"entries"`
	UpdatedAt int64             `json:"updated_at"`
}

// ImpressionStore 房间级印象存储。所有方法 *Locked 语义（§92a）。
type ImpressionStore struct {
	tables map[int]*ImpressionMemory
}

// impressionEntrySampleLimit 单条 SampleEvents 上限。
const impressionEntrySampleLimit = 5

// NewImpressionStore 构造空 store。
func NewImpressionStore() *ImpressionStore {
	return &ImpressionStore{tables: make(map[int]*ImpressionMemory, MaxPlayers)}
}

// AddOrUpdateDimLocked 把 (delta Trust/Competence/Sincerity/Cooperation/Threat) 累加到
// seat bot 对 target 的印象。delta ∈ [-1, +1];实际应用时已经按事件权重缩放。
//
// 调用前置：必须已持 r.mu（§92a）。
//
// 副作用：
//   - 在 entries 中找/建 targetSeat 条目
//   - 累加各 dim
//   - 钳制到 [0, 1]
//   - EventCount++,若 SampleEvents 未满则追加
func (s *ImpressionStore) AddOrUpdateDimLocked(
	seat, targetSeat int,
	delta ImpressionDims,
	sampleEvent string,
	now time.Time,
) {
	if s == nil || seat < 0 || seat >= MaxPlayers {
		return
	}
	if targetSeat < 0 || targetSeat >= MaxPlayers || seat == targetSeat {
		return
	}
	if s.tables == nil {
		s.tables = make(map[int]*ImpressionMemory, MaxPlayers)
	}
	mem, ok := s.tables[seat]
	if !ok || mem == nil {
		mem = &ImpressionMemory{
			Seat:      seat,
			Entries:   make([]ImpressionEntry, 0, MaxPlayers),
			UpdatedAt: now.UnixMilli(),
		}
		s.tables[seat] = mem
	}
	// 找/建 target 条目
	var entry *ImpressionEntry = nil
	for i := range mem.Entries {
		if mem.Entries[i].TargetSeat == targetSeat {
			entry = &mem.Entries[i]
			break
		}
	}
	if entry == nil {
		mem.Entries = append(mem.Entries, ImpressionEntry{
			TargetSeat:   targetSeat,
			Dims:         neutralImpressionDims(),
			LastUpdateMS: now.UnixMilli(),
			EventCount:   0,
			SampleEvents: make([]string, 0, impressionEntrySampleLimit),
		})
		entry = &mem.Entries[len(mem.Entries)-1]
	}
	// 先做衰减（仅对 LastUpdateMS 之前 → now 的时间段生效）
	applyImpressionDecay(entry, entry.LastUpdateMS, now.UnixMilli())
	// 累加
	entry.Dims.Trust = clampImpression(entry.Dims.Trust + delta.Trust)
	entry.Dims.Competence = clampImpression(entry.Dims.Competence + delta.Competence)
	entry.Dims.Sincerity = clampImpression(entry.Dims.Sincerity + delta.Sincerity)
	entry.Dims.Cooperation = clampImpression(entry.Dims.Cooperation + delta.Cooperation)
	entry.Dims.Threat = clampImpression(entry.Dims.Threat + delta.Threat)
	entry.LastUpdateMS = now.UnixMilli()
	entry.EventCount++
	if sampleEvent != "" && len(entry.SampleEvents) < impressionEntrySampleLimit {
		entry.SampleEvents = append(entry.SampleEvents, truncateImpressionSample(sampleEvent))
	}
	mem.UpdatedAt = now.UnixMilli()
}

// applyImpressionDecay 按时间衰减 4 个软维度（Trust/Competence/Sincerity/Cooperation）；
// Threat 维度若 ImpressionThreatNoDecay=true 则不衰减。
//
// 衰减公式: dims[i] = (dims[i] - 0.5) * 0.5^(elapsed_h / half_life_h) + 0.5
func applyImpressionDecay(entry *ImpressionEntry, lastMS, nowMS int64) {
	if entry == nil || nowMS <= lastMS {
		return
	}
	elapsedHours := float64(nowMS-lastMS) / 3600000.0
	if elapsedHours <= 0 {
		return
	}
	trustFactor := math.Pow(0.5, elapsedHours/ImpressionDecayHalfLifeHours)
	compFactor := math.Pow(0.5, elapsedHours/ImpressionCompetenceHalfLifeHours)
	entry.Dims.Trust = float32((float64(entry.Dims.Trust)-0.5)*trustFactor) + impressionNeutralAnchor
	entry.Dims.Competence = float32((float64(entry.Dims.Competence)-0.5)*compFactor) + impressionNeutralAnchor
	entry.Dims.Sincerity = float32((float64(entry.Dims.Sincerity)-0.5)*trustFactor) + impressionNeutralAnchor
	entry.Dims.Cooperation = float32((float64(entry.Dims.Cooperation)-0.5)*trustFactor) + impressionNeutralAnchor
	if !ImpressionThreatNoDecay {
		entry.Dims.Threat = float32((float64(entry.Dims.Threat)-0.5)*trustFactor) + impressionNeutralAnchor
	}
}

// GetLocked 返回 seat 的当前 ImpressionMemory 副本（已对所有条目按 now 做最新衰减）。
// 调用前置：必须已持 r.mu。
func (s *ImpressionStore) GetLocked(seat int, now time.Time) *ImpressionMemory {
	if s == nil {
		return nil
	}
	mem, ok := s.tables[seat]
	if !ok || mem == nil {
		return nil
	}
	cp := &ImpressionMemory{
		Seat:      mem.Seat,
		UpdatedAt: now.UnixMilli(),
		Entries:   make([]ImpressionEntry, len(mem.Entries)),
	}
	for i := range mem.Entries {
		e := mem.Entries[i]
		// 在 get 路径上对每个条目应用最新衰减
		applyImpressionDecay(&e, e.LastUpdateMS, now.UnixMilli())
		e.SampleEvents = append([]string(nil), mem.Entries[i].SampleEvents...)
		cp.Entries[i] = e
	}
	return cp
}

// SnapshotAllLocked 返回全 bot 印象快照（viewer==-1 spectator 用）。
// 调用前置：必须已持 r.mu。
func (s *ImpressionStore) SnapshotAllLocked(now time.Time) []ImpressionMemory {
	if s == nil || len(s.tables) == 0 {
		return nil
	}
	out := make([]ImpressionMemory, 0, len(s.tables))
	for _, mem := range s.tables {
		cp := *s.GetLocked(mem.Seat, now)
		// 按 target_seat 排序
		sort.SliceStable(cp.Entries, func(i, j int) bool {
			return cp.Entries[i].TargetSeat < cp.Entries[j].TargetSeat
		})
		out = append(out, cp)
	}
	return out
}

// neutralImpressionDims 5 维中性值（0.5）。
func neutralImpressionDims() ImpressionDims {
	return ImpressionDims{
		Trust:       impressionNeutralAnchor,
		Competence:  impressionNeutralAnchor,
		Sincerity:   impressionNeutralAnchor,
		Cooperation: impressionNeutralAnchor,
		Threat:      impressionNeutralAnchor,
	}
}

// clampImpression 把 v 钳制到 [0, 1]。
func clampImpression(v float32) float32 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// truncateImpressionSample 防御性截断 sample_event ≤ 30 字。
func truncateImpressionSample(s string) string {
	if s == "" {
		return s
	}
	runes := []rune(s)
	if len(runes) <= 30 {
		return s
	}
	return string(runes[:30])
}

// impressionStoreLocked 是 WerewolfRoom 的懒初始化 getter。
// 调用方必须已持 r.mu（§92a）。
func (r *WerewolfRoom) impressionStoreLocked() *ImpressionStore {
	if r.impressionStore == nil {
		r.impressionStore = NewImpressionStore()
	}
	return r.impressionStore
}