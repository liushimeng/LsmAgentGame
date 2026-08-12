package werewolf

// hypothesis_tracker.go — 多假说并行推演 + 信任矩阵(§20260810-07)
//
// 起源:Agent-Surpport-01.md §2.1(DeepSeek §一.1 多假说)+ §3.5(Gemini §一.2 信任矩阵)合并实现。
// §128 对话即思考:HypothesisTable 落地形态 = 嵌入 LastDecisionSummary 末尾「📊 [...]」JSON 段,
// 不新增独立 wire 字段;运行时结构体 HypothesisTable 仅承载服务端中转与 LLM 读取。
// §119 协议层隔离:HypothesisTable **不**写 chat_message / chat_history;
// 仅 BotTranscript(观战者可见)+ GameContext.HypothesisTable(本人 bot 可见)。
// §135 spectator 隔离:BuildClientStateWithRoom viewer==-1 时填充 BotHypotheses[];
// 真人玩家 viewer>=0 时 omitempty,前端按 RoleCheck.isSpectator 渲染。
// §92a 锁约束:所有 HypothesisStore 方法均为 *Locked 变体(调用方必须已持 r.mu);
// 公开变体仅在测试需要时存在,本文件不导出任何持锁包装。
// §120 公平性:解析失败 → 静默丢弃,**不**计入 consecutiveFailures。

import (
	"encoding/json"
	"regexp"
	"time"
)

// hypothesisStoreCap 房间级 HypothesisStore 的 per-bot 上限。13 人局 × 12 个目标 = 156,
// 留 1.5× 富余 = 230 条 bot 假说;每 bot 维护 ~12 个目标的滚动窗口。
const hypothesisStoreCap = 230

// HypothesisEntry 一条身份假说。"我对 X 号身份的最佳猜测 + 置信度 + 支撑/反驳"。
type HypothesisEntry struct {
	TargetSeat int    `json:"target_seat"`  // 0-indexed 被猜测的玩家
	RoleGuess  string `json:"role_guess"`   // 封闭枚举:werewolf/seer/witch/guard/villager/idiot/knight/hunter/demon_hunter/unknown
	Confidence int    `json:"confidence"`   // 0~100
	Supporting string `json:"supporting"`   // ≤40 字,公开支撑依据
	Refuting   string `json:"refuting"`     // ≤40 字,公开反证
	UpdatedAt  int64  `json:"updated_at"`   // UnixMilli
}

// HypothesisTable 是某个 bot 座位维护的"我对其他玩家的假说集"。
type HypothesisTable struct {
	Seat      int               `json:"seat"`
	Entries   []HypothesisEntry `json:"entries"`
	Round     int               `json:"round"`
	UpdatedAt int64             `json:"updated_at"`
}

// HypothesisStore 房间级假说存储。所有方法 *Locked 语义:调用方必须已持 r.mu(§92a)。
type HypothesisStore struct {
	tables map[int]*HypothesisTable
}

// hypothesisSummaryRe 捕获 LastDecisionSummary 末尾的「📊 [...]」JSON 段。
// 非贪婪匹配,避免吞掉后续字符。失败静默 — 不返回 error,仅 ok=false。
var hypothesisSummaryRe = regexp.MustCompile(`\s*📊\s*(\[.*?\])\s*$`)

// NewHypothesisStore 构造空 store。延迟到 lazy init 以节省零 bot 房间的开销。
func NewHypothesisStore() *HypothesisStore {
	return &HypothesisStore{tables: make(map[int]*HypothesisTable, MaxPlayers)}
}

// UpdateFromDecisionSummary 解析 LLM 在 LastDecisionSummary 末尾追加的「📊 [...]」JSON 段,
// 成功时把 HypothesisTable 写回 store。失败时静默丢弃(不计入 consecutiveFailures,§120)。
//
// **调用前置**:必须已持 r.mu(§92a)。
func (s *HypothesisStore) UpdateFromDecisionSummary(seat, round int, lastDecisionSummary string) {
	if s == nil || seat < 0 || seat >= MaxPlayers {
		return
	}
	matches := hypothesisSummaryRe.FindStringSubmatch(lastDecisionSummary)
	if len(matches) < 2 {
		return
	}
	rawJSON := matches[1]
	var entries []HypothesisEntry
	if err := json.Unmarshal([]byte(rawJSON), &entries); err != nil {
		// LLM 偶尔输出非合规 JSON(§128 容忍)——静默丢弃,不 panic,不计入失败计数。
		return
	}
	// 防御:confidence 限幅 0~100,字符串 ≤40 字。
	for i := range entries {
		if entries[i].Confidence < 0 {
			entries[i].Confidence = 0
		}
		if entries[i].Confidence > 100 {
			entries[i].Confidence = 100
		}
		entries[i].Supporting = truncateHypothesisText(entries[i].Supporting, 40)
		entries[i].Refuting = truncateHypothesisText(entries[i].Refuting, 40)
	}
	s.tables[seat] = &HypothesisTable{
		Seat:      seat,
		Entries:   entries,
		Round:     round,
		UpdatedAt: time.Now().UnixMilli(),
	}
}

// GetLocked 返回 seat 的当前 HypothesisTable 副本(无 → nil)。
// **调用前置**:必须已持 r.mu。
func (s *HypothesisStore) GetLocked(seat int) *HypothesisTable {
	if s == nil {
		return nil
	}
	t, ok := s.tables[seat]
	if !ok || t == nil {
		return nil
	}
	// 防御性拷贝,避免下游修改穿透回 store。
	cp := *t
	cp.Entries = make([]HypothesisEntry, len(t.Entries))
	copy(cp.Entries, t.Entries)
	return &cp
}

// SnapshotAllLocked 返回全 bot 假说表快照(viewer==-1 spectator 用)。
// **调用前置**:必须已持 r.mu。
func (s *HypothesisStore) SnapshotAllLocked() []HypothesisTable {
	if s == nil || len(s.tables) == 0 {
		return nil
	}
	out := make([]HypothesisTable, 0, len(s.tables))
	for _, t := range s.tables {
		cp := *t
		cp.Entries = make([]HypothesisEntry, len(t.Entries))
		copy(cp.Entries, t.Entries)
		out = append(out, cp)
	}
	return out
}

// StripFromDecisionSummary 去掉 LastDecisionSummary 末尾的「📊 [...]」段。
// 用于 BotTranscript 下发前 sanitize — **玩家侧(非 spectator)** 永远收不到假说摘要,
// §135 spectator 隔离,只 spectator 才允许看假说内容。
func StripFromDecisionSummary(summary string) string {
	if summary == "" {
		return summary
	}
	return hypothesisSummaryRe.ReplaceAllString(summary, "")
}

func truncateHypothesisText(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes])
}

// hypothesisStoreLocked 是 WerewolfRoom 的懒初始化 getter。
// 调用方必须已持 r.mu(§92a)。
func (r *WerewolfRoom) hypothesisStoreLocked() *HypothesisStore {
	if r.hypothesisStore == nil {
		r.hypothesisStore = NewHypothesisStore()
	}
	return r.hypothesisStore
}