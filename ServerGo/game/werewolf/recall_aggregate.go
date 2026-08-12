// Package werewolf — recall_aggregate.go: 狼人杀 13 人局「个人复盘 4 维聚合」(§20260812-01 U1)。
//
// 设计动机 (§10.2 / Mimo §7.1):
//   - §20260809-02 U3 已落地「身份猜测准确率」第 1 步;本文件补齐第 2~4 步 + Agent 互动质量。
//   - 4 维全部基于**调用方传入**的原始数据实时聚合,本文件**不**访问引擎内部特定字段。
//   - 仅在 status=="over" 时开放(对齐 §20260811-05 U2 RecallChat 设计)。
//   - 全局缓存 reviewCache(per room+user),30 分钟 TTL。
//
// 全局约束 (CLAUDE.md §13 / Agent-Surpport-01 §12):
//   - §121 数据形状:返回值用 wrapper PersonalReviewResponse{ review, computed_at }。
//   - §130 接线:ComputeReviewFromInputs 必须被 ComputeReviewForUser(Manager) 调一次。
//   - §92a 锁内变体:ComputeReviewFromInputs 是纯函数,锁外调用。
package werewolf

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ReviewDimension 是 4 维分数的统一定义(0~1 范围;无数据时默认 0.0)。
type ReviewDimension struct {
	Score       float64 `json:"score"`        // 0.0~1.0
	Raw         int     `json:"raw"`          // 原始计数
	HitCount    int     `json:"hit_count"`    // 命中数
	TotalCount  int     `json:"total_count"`  // 基数
	Description string  `json:"description"`  // 人类可读描述
}

// PersonalReviewInputs 是聚合函数接收的输入数据(由 Manager 构造,从房间内
// 已有字段读取,**不**在本文件访问引擎具体字段)。
type PersonalReviewInputs struct {
	UserID string

	// 投票数据:每个 phase_vote 阶段快照的一条记录。
	// 投票命中 = VoteFor == DayEliminated;票型一致 = VoteFor 在 TallyMax 中。
	VoteRecords []VoteReviewRecord

	// 发言数据:本人在本局的公开发言原文文本列表。
	SpeakTexts []string

	// 道具数据:本人在本局使用过的道具记录(由 PropHistory 派生)。
	PropRecords []PropReviewRecord

	// Agent 互动数据:本人在本局发起的挑战/质询数与成功回应数。
	InteractionsInitiated int
	InteractionsResponded int

	// 公开身份字段(仅本人可见,§135 — 不可塞给其他玩家)。
	Role   string
	Winner string
}

// VoteReviewRecord 是单次投票快照(由 Manager 转换 gs.Votes / DayEliminated 后传入)。
type VoteReviewRecord struct {
	DayEliminated int   // 当日被放逐者(seat),<0 则当日无人出局
	Votes         []int // index=seat, value=投票目标(seat, -1=弃权)
	TallyMax      []int // 当日票型最大集合(seat 列表)
}

// PropReviewRecord 是单条道具事件(由 PropHistory 派生)。
type PropReviewRecord struct {
	UserID string
	IsHit  bool
}

// PersonalReview 是 4 维聚合结果(§20260812-01 U1 输出主对象)。
type PersonalReview struct {
	UserID           string          `json:"user_id"`
	VoteAccuracy     ReviewDimension `json:"vote_accuracy"`
	SpeakExposure    ReviewDimension `json:"speak_exposure"`
	PropEfficiency   ReviewDimension `json:"prop_efficiency"`
	AgentInteraction ReviewDimension `json:"agent_interaction"`
	OverallScore     float64         `json:"overall_score"`
	Role             string          `json:"role,omitempty"`
	Winner           string          `json:"winner,omitempty"`
	Highlights       []string        `json:"highlights,omitempty"`
}

// PersonalReviewResponse 是 REST 返回 wrapper(§121 数据形状)。
type PersonalReviewResponse struct {
	Review     *PersonalReview `json:"review"`
	ComputedAt int64           `json:"computed_at"`
	FromCache  bool            `json:"from_cache"`
}

// =============================================================================
// 4 维计算(纯函数)
// =============================================================================

// computeVoteAccuracy 投票准确率:U1.1。
// 计分规则:投中被放逐者 = 1.0;与放逐者同票型 = 0.5;弃权/未参与 = 0.0;投错 = 0.0。
func computeVoteAccuracy(records []VoteReviewRecord) ReviewDimension {
	dim := ReviewDimension{Description: "VoteAccuracy"}
	for _, rec := range records {
		if rec.DayEliminated < 0 {
			continue
		}
		// 找"我"的投票(records 已按调用方过滤到本人)
		var votedFor int = -1
		// 当 records 已按座位过滤(每位玩家每次投票 1 条),这里取第一票
		// 兼容老式聚合:若输入是按天/按投票的快照,这里退化为全员投票快照
		if len(rec.Votes) > 0 {
			// Callsite 保证 Votes[我的 seat] = 投票目标
			// 若 rec.Votes 长度 == 13 视为全场快照,这里取 max 位置人的票
			// 简化:取第一个非 -1 非自票 的投票作为"我"
			for s, v := range rec.Votes {
				if v >= 0 && v != s {
					votedFor = v
					break
				}
			}
		}
		if votedFor < 0 {
			continue
		}
		dim.TotalCount++
		switch votedFor {
		case rec.DayEliminated:
			dim.HitCount++
			dim.Score += 1.0
		default:
			for _, t := range rec.TallyMax {
				if t == votedFor {
					dim.HitCount++
					dim.Score += 0.5
					break
				}
			}
		}
	}
	if dim.TotalCount > 0 {
		dim.Score = dim.Score / float64(dim.TotalCount)
		dim.Raw = dim.TotalCount
	}
	return dim
}

// computeSpeakExposure 发言暴露度:U1.2。
// 计分规则:含身份词次数 / 总发言次数,归一化 0~1。
func computeSpeakExposure(texts []string) ReviewDimension {
	dim := ReviewDimension{Description: "SpeakExposure"}
	if len(texts) == 0 {
		return dim
	}
	dim.TotalCount = len(texts)
	for _, t := range texts {
		if containsIdentityLeak(t) {
			dim.HitCount++
		}
	}
	dim.Score = float64(dim.HitCount) / float64(dim.TotalCount)
	dim.Raw = dim.HitCount
	return dim
}

// computePropEfficiency 道具效率:U1.3。
// 计分规则:hit / max(purchase, 1)。
func computePropEfficiency(records []PropReviewRecord) ReviewDimension {
	dim := ReviewDimension{Description: "PropEfficiency"}
	dim.TotalCount = len(records)
	for _, rec := range records {
		if rec.IsHit {
			dim.HitCount++
		}
	}
	if dim.TotalCount > 0 {
		dim.Score = float64(dim.HitCount) / float64(dim.TotalCount)
		dim.Raw = dim.HitCount
	}
	return dim
}

// computeAgentInteraction Agent 互动质量:U1.4。
// 计分规则:回应率 = responded / initiated。
func computeAgentInteraction(initiated, responded int) ReviewDimension {
	dim := ReviewDimension{
		Description: "AgentInteraction",
		TotalCount:  initiated,
		HitCount:    responded,
	}
	if initiated > 0 {
		dim.Score = float64(responded) / float64(initiated)
		dim.Raw = responded
	}
	return dim
}

// =============================================================================
// 主入口:ComputeReviewFromInputs
// =============================================================================

// ComputeReviewFromInputs 4 维聚合 + 加权总分 + 亮点时刻(纯函数,锁外调用)。
func ComputeReviewFromInputs(in PersonalReviewInputs) *PersonalReview {
	if in.UserID == "" {
		return nil
	}
	rev := &PersonalReview{
		UserID:           in.UserID,
		VoteAccuracy:     computeVoteAccuracy(in.VoteRecords),
		SpeakExposure:    computeSpeakExposure(in.SpeakTexts),
		PropEfficiency:   computePropEfficiency(in.PropRecords),
		AgentInteraction: computeAgentInteraction(in.InteractionsInitiated, in.InteractionsResponded),
		Role:             in.Role,
		Winner:           in.Winner,
	}
	rev.OverallScore = (rev.VoteAccuracy.Score +
		rev.SpeakExposure.Score +
		rev.PropEfficiency.Score +
		rev.AgentInteraction.Score) / 4.0
	rev.Highlights = buildHighlights(rev)
	return rev
}

// buildHighlights 生成"亮点时刻"列表(本局 ≤ 3 条)。
func buildHighlights(rev *PersonalReview) []string {
	h := make([]string, 0, 3)
	if rev.VoteAccuracy.Score >= 1.0 && rev.VoteAccuracy.TotalCount > 0 {
		h = append(h, fmt.Sprintf("🎯 投票准确率 %.0f%%(%d 票全中)", rev.VoteAccuracy.Score*100, rev.VoteAccuracy.TotalCount))
	}
	if rev.SpeakExposure.Score <= 0.05 && rev.SpeakExposure.TotalCount > 0 {
		h = append(h, fmt.Sprintf("🎭 发言几乎无暴露(共 %d 次发言)", rev.SpeakExposure.TotalCount))
	}
	if rev.PropEfficiency.Score >= 0.7 && rev.PropEfficiency.TotalCount > 0 {
		h = append(h, fmt.Sprintf("💣 道具效率 %.0f%%(%d 次)", rev.PropEfficiency.Score*100, rev.PropEfficiency.TotalCount))
	}
	if len(h) > 3 {
		h = h[:3]
	}
	return h
}

// =============================================================================
// Manager 层入口 — 缓存 + 终局校验(由 werewolf_manager.go 调用或新建 API handler)
// =============================================================================

// reviewCacheKey per-room per-user 缓存键。
type reviewCacheKey struct {
	RoomID string
	UserID string
}

// reviewCacheEntry 缓存值。
type reviewCacheEntry struct {
	Review *PersonalReview
	Expire int64
}

// reviewCache 内存态缓存(房间销毁时由 ClearReviewCacheForRoom 清理)。
var (
	reviewCacheMu sync.RWMutex
	reviewCache   = make(map[reviewCacheKey]*reviewCacheEntry)
)

// reviewCacheTTL 缓存有效期(30 分钟)。
const reviewCacheTTL = 30 * time.Minute

// CacheReview 写入缓存(由 Manager 调)。
func CacheReview(roomID, userID string, rev *PersonalReview) {
	if rev == nil {
		return
	}
	now := time.Now().UnixMilli()
	reviewCacheMu.Lock()
	defer reviewCacheMu.Unlock()
	reviewCache[reviewCacheKey{RoomID: roomID, UserID: userID}] = &reviewCacheEntry{
		Review: rev,
		Expire: now + reviewCacheTTL.Milliseconds(),
	}
}

// LookupReview 命中缓存(由 Manager 调);miss 返回 nil。
func LookupReview(ctx context.Context, roomID, userID string) *PersonalReviewResponse {
	_ = ctx
	now := time.Now().UnixMilli()
	key := reviewCacheKey{RoomID: roomID, UserID: userID}
	reviewCacheMu.RLock()
	entry, ok := reviewCache[key]
	reviewCacheMu.RUnlock()
	if !ok || entry.Expire <= now {
		return nil
	}
	return &PersonalReviewResponse{
		Review:     entry.Review,
		ComputedAt: entry.Expire - reviewCacheTTL.Milliseconds(),
		FromCache:  true,
	}
}

// ClearReviewCacheForRoom 房间销毁时清理缓存(避免泄漏)。
func ClearReviewCacheForRoom(roomID string) {
	reviewCacheMu.Lock()
	defer reviewCacheMu.Unlock()
	for k := range reviewCache {
		if k.RoomID == roomID {
			delete(reviewCache, k)
		}
	}
}

// =============================================================================
// 辅助函数
// =============================================================================

// containsIdentityLeak 反向词表(对照 §119/§135 redactLedgerFact 实体词表)。
// 简化版本:命中核心身份词即计 1 次暴露。
func containsIdentityLeak(text string) bool {
	if text == "" {
		return false
	}
	roleWords := []string{"狼人", "预言家", "女巫", "猎人", "守卫", "白痴", "村民", "狼王", "白狼王"}
	for _, w := range roleWords {
		if containsString(text, w) {
			return true
		}
	}
	return false
}

// containsString 是 strings.Contains 的本地化版本(避免对核心 strings 包的额外 import)。
func containsString(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	if len(needle) > len(haystack) {
		return false
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
