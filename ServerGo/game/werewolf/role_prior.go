// Package werewolf — role_prior.go: 狼人杀 13 人局 Agent 身份偏见（§20260826-01 U1）。
//
// 设计动机（Agent-Surpport-01 §9.4 / M3 §5 P1 / DeepSeek §一.5）：
//   - 真人玩家对每个座位天然有「角色先验分布」（神职轮转、身份排除、自保先验）。
//   - 现有假说表 HypothesisTable 是 LLM 跨轮主观推断，无**初始先验**锚点。
//   - 引入 RolePriorTable 给每个 bot 维护「开局初始印象」+「死亡公开」硬锚点，
//     避免 LLM 初期对所有座位均匀分布怀疑（违反人类直觉）。
//
// 与既有组件的关系：
//   - 与 HypothesisTable 正交：prior 是「开局先验」，hypothesis 是「动态猜测」。
//   - 与 InformationLedger 互补：账本是事实层，prior 是概率先验层。
//   - 与 AgentReputation 不同：prior 是**本局内**的，reputation 是**跨局**的。
//
// 数据流：
//   - StartAgentsLocked 末尾调 ComputeRolePriorForSeatLocked(r, seat)
//     计算每个存活 bot 对其他 12 个座位的先验分布。
//   - 死亡公开时（emitDeathRevealLocked）调 ApplyDeathRevealPriorLocked
//     把死亡座位 hard-set 为 1.0/0.0。
//   - buildAgentContextLocked 末尾调 GetRolePriorLocked 填到 gc.RolePrior。
//   - BuildClientStateWithRoom(viewer==-1) 末尾调 populateRolePriors
//     下发 cs.BotRolePriors[]（§135 spectator 隔离）。
//
// §92a 锁约束：所有 *Store 方法均为 *Locked 变体；公开方法不导出。
// §119 协议层隔离：RolePriorTable **不**写 chat_message / chat_history / HeartThought。
// §135 spectator 隔离：viewer>=0 时 omitempty；viewer==-1 全量下发。
// §128 对话即思考：不新独立 LLM 调用；纯服务端计算。
package werewolf

import (
	"sort"
	"time"
)

// rolePriorStoreCap 房间级 RolePriorStore 的 per-bot 上限。
// 13 人局 × 12 目标 × 13 个角色 = 2028；本表只存「prior > 0 的稀疏条目」，
// 通常每个目标只 3~5 个非零 role（其他角色 prior = 0 不存），实际占用 ≈ 13 × 12 × 4 = 624。
const rolePriorStoreCap = 700

// rolePriorRoleList 是封闭枚举（与 HypothesisEntry.RoleGuess 对齐 + villager）。
// 不含 king/deceiver/collector/tanner（这些是终局身份，本局 prior 始终 = 0）。
var rolePriorRoleList = []string{
	"werewolf",
	"seer",
	"witch",
	"guard",
	"villager",
	"idiot",
	"knight",
	"hunter",
	"demon_hunter",
	"unknown",
}

// RolePriorSingle 一条身份先验条目（§20260826-01 U1 数据结构）。
type RolePriorSingle struct {
	TargetSeat   int     `json:"target_seat"`     // 0-indexed
	RoleGuess    string  `json:"role_guess"`      // 封闭枚举
	PriorProb    float32 `json:"prior_prob"`      // 0~1
	EvidenceKind string  `json:"evidence_kind"`   // "uniform" | "historical" | "death_revealed" | "self_exclude"
	Note         string  `json:"note"`            // ≤40 字来源
	ComputedAt   int64   `json:"computed_at"`     // UnixMilli
}

// RolePriorTable per bot 的身份先验表。
type RolePriorTable struct {
	Seat       int               `json:"seat"`
	Entries    []RolePriorSingle `json:"entries"`
	ComputedAt int64             `json:"computed_at"`
}

// RolePriorStore 房间级先验存储。所有方法 *Locked 语义：调用方必须已持 r.mu（§92a）。
type RolePriorStore struct {
	tables map[int]*RolePriorTable
}

// NewRolePriorStore 构造空 store。
func NewRolePriorStore() *RolePriorStore {
	return &RolePriorStore{tables: make(map[int]*RolePriorTable, MaxPlayers)}
}

// ComputeRolePriorForSeatLocked 计算 seat bot 对其他 12 个座位的先验分布。
//
// 调用前置：必须已持 r.mu。
//
// 算法（纯计算，不调 LLM）：
//   1. 均匀初始化：13 role × 12 target，每个 (target, role) prior = 1/13 ≈ 0.077
//      （unknown 不参与均匀分布；保留为「兜底 0.0」除非 LLM 主动填）。
//   2. 死亡公开：若 target 已死且 RolePubliclyRevealed → 该 role hard-set 1.0，其他 → 0.0。
//   3. self-exclude：target == seat → 所有 role 按已知身份填（自己 100%/其他 0）。
//   4. 人格加成：TrustTendency < 0.4（多疑者）→ 全表 prior × 1.10。
//   5. L2 归一化：保证 Σ prior = 1（每个 target 维度的所有 role 求和 = 1）。
//
// 输出：RolePriorTable（稀疏，只存 prior > 0.01 的条目）。
func (s *RolePriorStore) ComputeRolePriorForSeatLocked(seat int, trustTendency float32, now time.Time) *RolePriorTable {
	if s == nil || seat < 0 || seat >= MaxPlayers {
		return nil
	}
	// 防止 nil store 静默吞写入
	if s.tables == nil {
		s.tables = make(map[int]*RolePriorTable, MaxPlayers)
	}
	out := &RolePriorTable{
		Seat:       seat,
		Entries:    make([]RolePriorSingle, 0, rolePriorStoreCap/MaxPlayers),
		ComputedAt: now.UnixMilli(),
	}
	// 当前简化版：均匀分布 + self-exclude + 死亡公开兜底
	// 真实「历史轮转」需要跨局数据，本期先不实现
	uniformProb := float32(1.0) / float32(len(rolePriorRoleList)-1) // 不计 unknown
	for target := 0; target < MaxPlayers; target++ {
		if target == seat {
			// self-exclude — 不存（玩家不需要自己的先验；GameContext 用 self_role 字段）
			continue
		}
		for _, role := range rolePriorRoleList {
			if role == "unknown" {
				continue
			}
			prob := uniformProb
			// 多疑者加成
			if trustTendency > 0 && trustTendency < 0.4 {
				prob *= 1.10
			}
			if prob < 0.01 {
				continue
			}
			out.Entries = append(out.Entries, RolePriorSingle{
				TargetSeat:   target,
				RoleGuess:    role,
				PriorProb:    prob,
				EvidenceKind: "uniform",
				Note:         "开局均匀分布（§20260826-01）",
				ComputedAt:   now.UnixMilli(),
			})
		}
	}
	// L2 归一化（每 target 维度）
	normalizeRolePriorEntries(out)
	s.tables[seat] = out
	return out
}

// ApplyDeathRevealPriorLocked 死亡身份公开后，把 target 的所有 role 概率硬改写。
//
// 调用前置：必须已持 r.mu。
//
// 入参：revealedRole = 死者公开身份（werewolf / villager / seer / ...）。
//      效果：所有持有此 target 的 bot 表中，target+revealedRole → 1.0，其他 role → 0.0。
func (s *RolePriorStore) ApplyDeathRevealPriorLocked(targetSeat int, revealedRole string, now time.Time) {
	if s == nil || targetSeat < 0 || targetSeat >= MaxPlayers || revealedRole == "" {
		return
	}
	if s.tables == nil {
		return
	}
	for botSeat, tbl := range s.tables {
		if tbl == nil {
			continue
		}
		// 移除 target 的所有条目
		filtered := tbl.Entries[:0]
		for _, e := range tbl.Entries {
			if e.TargetSeat != targetSeat {
				filtered = append(filtered, e)
			}
		}
		// 追加 hard-set 条目
		tbl.Entries = append(filtered, RolePriorSingle{
			TargetSeat:   targetSeat,
			RoleGuess:    revealedRole,
			PriorProb:    1.0,
			EvidenceKind: "death_revealed",
			Note:         "死亡公开（§135 公平性）",
			ComputedAt:   now.UnixMilli(),
		})
		_ = botSeat
	}
}

// GetLocked 返回 seat 的当前 RolePriorTable 副本（无 → nil）。
// 调用前置：必须已持 r.mu。
func (s *RolePriorStore) GetLocked(seat int) *RolePriorTable {
	if s == nil {
		return nil
	}
	t, ok := s.tables[seat]
	if !ok || t == nil {
		return nil
	}
	cp := *t
	cp.Entries = make([]RolePriorSingle, len(t.Entries))
	copy(cp.Entries, t.Entries)
	return &cp
}

// SnapshotAllLocked 返回全 bot 先验表快照（viewer==-1 spectator 用）。
// 调用前置：必须已持 r.mu。
func (s *RolePriorStore) SnapshotAllLocked() []RolePriorTable {
	if s == nil || len(s.tables) == 0 {
		return nil
	}
	out := make([]RolePriorTable, 0, len(s.tables))
	for _, t := range s.tables {
		cp := *t
		cp.Entries = make([]RolePriorSingle, len(t.Entries))
		copy(cp.Entries, t.Entries)
		// 按 target_seat 排序（便于前端展示）
		sort.SliceStable(cp.Entries, func(i, j int) bool {
			if cp.Entries[i].TargetSeat != cp.Entries[j].TargetSeat {
				return cp.Entries[i].TargetSeat < cp.Entries[j].TargetSeat
			}
			return cp.Entries[i].PriorProb > cp.Entries[j].PriorProb
		})
		out = append(out, cp)
	}
	return out
}

// normalizeRolePriorEntries L1 归一化：每个 target 维度的所有 role 概率求和 = 1。
func normalizeRolePriorEntries(t *RolePriorTable) {
	if t == nil {
		return
	}
	// 按 target 分组
	byTarget := make(map[int][]int) // target → index in t.Entries
	for i, e := range t.Entries {
		byTarget[e.TargetSeat] = append(byTarget[e.TargetSeat], i)
	}
	for _, idxs := range byTarget {
		var sum float32
		for _, i := range idxs {
			sum += t.Entries[i].PriorProb
		}
		if sum <= 0 {
			continue
		}
		// 防止 over-1.10 加成导致 sum > 1
		inv := float32(1.0) / sum
		for _, i := range idxs {
			t.Entries[i].PriorProb *= inv
		}
	}
}

// rolePriorStoreLocked 是 WerewolfRoom 的懒初始化 getter。
// 调用方必须已持 r.mu（§92a）。
func (r *WerewolfRoom) rolePriorStoreLocked() *RolePriorStore {
	if r.rolePriorStore == nil {
		r.rolePriorStore = NewRolePriorStore()
	}
	return r.rolePriorStore
}

// truncatePriorNote 防御性截断 Note 字段 ≤40 字。
func truncatePriorNote(s string) string {
	if s == "" {
		return s
	}
	runes := []rune(s)
	if len(runes) <= 40 {
		return s
	}
	return string(runes[:40])
}