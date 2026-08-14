// Package werewolf — recall_review_bridge_test.go: §20260814-01 U1 回归测试。
//
// 覆盖:
//   - R01 recordVoteHistoryLocked 正确记录票型 + 放逐者
//   - R02 平票(DayEliminated=NoSeat)也入历史且 TallyMax 保留并列者
//   - R03 上限裁剪保留**最近** N 天
//   - R04 resetVoteHistoryLocked 跨局清零(含 judgeTrustTrace)
//   - R05 ★ 端到端:voteHistory → PersonalReviewInputs → 聚合产物**非空**
//     (§20260811-08 教训 (5):只测转换函数不测转换结果等于没测)
//   - R06 未终局房间被拒
//   - R07 缓存命中
//   - R08 propHistorySnapshotLocked 环绕后顺序正确
package werewolf

import (
	"context"
	"testing"

	"LsmAgentGame/agent/wwjudge"
)

// newVoteRoom 构造一个最小可用的终局房间。
func newVoteRoom(roomID string) *WerewolfRoom {
	r := &WerewolfRoom{RoomID: roomID}
	r.State = NewGame(20260814)
	r.State.Phase = PhaseGameOver
	r.State.Winner = "good"
	return r
}

func TestReview_R01_RecordVoteHistory(t *testing.T) {
	r := newVoteRoom("room-r01")
	// 1 号投 3 号、2 号投 3 号;3 号被放逐。
	r.State.LastDayVoteMap = map[Seat]Seat{1: 3, 2: 3}
	r.State.DayEliminated = 3
	r.recordVoteHistoryLocked(map[Seat]int{3: 2})

	if len(r.voteHistory) != 1 {
		t.Fatalf("voteHistory 长度 = %d, want 1", len(r.voteHistory))
	}
	h := r.voteHistory[0]
	if h.DayEliminated != 3 {
		t.Errorf("DayEliminated = %d, want 3", h.DayEliminated)
	}
	if h.Votes[1] != 3 || h.Votes[2] != 3 {
		t.Errorf("Votes[1]=%d Votes[2]=%d, want 3/3", h.Votes[1], h.Votes[2])
	}
	if h.Votes[0] != -1 {
		t.Errorf("未投票座位应为 -1, got %d", h.Votes[0])
	}
	if len(h.TallyMax) != 1 || h.TallyMax[0] != 3 {
		t.Errorf("TallyMax = %v, want [3]", h.TallyMax)
	}
}

// TestReview_R02_TiedVoteKeepsTallyMax 平票时 DayEliminated 是 NoSeat,
// 但「与最高票同票型」仍是复盘的 0.5 分档,故 TallyMax 必须保留全部并列者。
func TestReview_R02_TiedVoteKeepsTallyMax(t *testing.T) {
	r := newVoteRoom("room-r02")
	r.State.LastDayVoteMap = map[Seat]Seat{1: 4, 2: 5}
	r.State.DayEliminated = NoSeat
	r.recordVoteHistoryLocked(map[Seat]int{4: 1, 5: 1})

	if len(r.voteHistory) != 1 {
		t.Fatalf("平票也必须入历史, got %d 条", len(r.voteHistory))
	}
	h := r.voteHistory[0]
	if h.DayEliminated >= 0 {
		t.Errorf("平票 DayEliminated = %d, want <0", h.DayEliminated)
	}
	if len(h.TallyMax) != 2 {
		t.Errorf("TallyMax = %v, want 两个并列者", h.TallyMax)
	}
}

func TestReview_R03_HistoryCapKeepsRecentDays(t *testing.T) {
	r := newVoteRoom("room-r03")
	// 写入上限 + 5 天,每天放逐者 = 天序号(便于识别保留了哪些)。
	total := reviewVoteHistoryMaxDays + 5
	for d := 0; d < total; d++ {
		r.State.LastDayVoteMap = map[Seat]Seat{1: Seat(d % 13)}
		r.State.DayEliminated = Seat(d % 13)
		r.recordVoteHistoryLocked(map[Seat]int{Seat(d % 13): 1})
	}
	if len(r.voteHistory) != reviewVoteHistoryMaxDays {
		t.Fatalf("长度 = %d, want %d(上限裁剪)", len(r.voteHistory), reviewVoteHistoryMaxDays)
	}
	// 最后一条必须是最后写入的那天(保留最近,丢弃最旧)。
	wantLast := (total - 1) % 13
	if got := r.voteHistory[len(r.voteHistory)-1].DayEliminated; got != wantLast {
		t.Errorf("末条 DayEliminated = %d, want %d —— 裁剪方向错了(丢了最近而非最旧)", got, wantLast)
	}
}

func TestReview_R04_ResetClearsBothFields(t *testing.T) {
	r := newVoteRoom("room-r04")
	r.State.LastDayVoteMap = map[Seat]Seat{1: 2}
	r.State.DayEliminated = 2
	r.recordVoteHistoryLocked(map[Seat]int{2: 1})
	r.judgeTrustTrace = append(r.judgeTrustTrace, wwjudge.TrustTraceEntry{Seat: 1, Day: 1, Score: 0.5})

	r.resetVoteHistoryLocked()

	if len(r.voteHistory) != 0 {
		t.Errorf("voteHistory 未清零: %d 条 —— 第二局复盘会算进第一局的票", len(r.voteHistory))
	}
	if len(r.judgeTrustTrace) != 0 {
		t.Errorf("judgeTrustTrace 未清零: %d 条", len(r.judgeTrustTrace))
	}
}

// TestReview_R05_EndToEndAggregationNonEmpty 是本组**最重要**的断言。
//
// §20260811-08 教训 (5):「只测转换函数、不测转换结果,等于没测」——
// 那次 redactLedgerFact 的 P0 之所以潜伏整整一个版本,正是因为原测试只断言
// 「身份词被剔除」,从未断言「聚合结果非空」。
//
// 本条走完整链路:voteHistory 采集 → ComputeReviewForUser 构造 inputs →
// ComputeReviewFromInputs 聚合 → 断言**产物非零**。
// 若中间任何一环把数据丢了(如座位过滤写错、Votes 切片构造错),
// VoteAccuracy.TotalCount 会是 0,本条立即失败。
func TestReview_R05_EndToEndAggregationNonEmpty(t *testing.T) {
	const uid = "user-alice"
	m := &WerewolfManager{rooms: map[string]*WerewolfRoom{}}
	r := newVoteRoom("room-r05")
	r.Seats[4] = uid // alice 坐 4 号位

	// 三天票型:alice(4号) 依次投 7 / 7 / 9;被放逐者 7 / 2 / 9。
	// ⇒ 第 1 天投中、第 2 天投错、第 3 天投中 ⇒ 准确率 2/3。
	type day struct {
		aliceVote, eliminated int
	}
	for _, d := range []day{{7, 7}, {7, 2}, {9, 9}} {
		r.State.LastDayVoteMap = map[Seat]Seat{4: Seat(d.aliceVote), 1: Seat(d.eliminated)}
		r.State.DayEliminated = Seat(d.eliminated)
		r.recordVoteHistoryLocked(map[Seat]int{Seat(d.eliminated): 2})
	}
	m.rooms[r.RoomID] = r

	// 清掉可能的跨测试缓存污染。
	ClearReviewCacheForRoom(r.RoomID)

	resp, err := m.ComputeReviewForUser(context.Background(), r.RoomID, uid)
	if err != nil {
		t.Fatalf("ComputeReviewForUser 失败: %v", err)
	}
	if resp == nil || resp.Review == nil {
		t.Fatal("返回 nil review —— 聚合链路断了")
	}

	// ★ 核心断言:聚合产物非空。
	if resp.Review.VoteAccuracy.TotalCount == 0 {
		t.Fatal("VoteAccuracy.TotalCount == 0 —— voteHistory 采集到聚合之间数据丢失" +
			"(§20260811-08 教训 5:必须断言解析产物非空)")
	}
	if got := resp.Review.VoteAccuracy.TotalCount; got != 3 {
		t.Errorf("VoteAccuracy.TotalCount = %d, want 3(三天各一票)", got)
	}
	if got := resp.Review.VoteAccuracy.HitCount; got != 2 {
		t.Errorf("VoteAccuracy.HitCount = %d, want 2(第 1、3 天投中)", got)
	}
	if resp.Review.UserID != uid {
		t.Errorf("UserID = %q, want %q", resp.Review.UserID, uid)
	}
	if resp.Review.Winner != "good" {
		t.Errorf("Winner = %q, want good", resp.Review.Winner)
	}
	if resp.Review.Role == "" {
		t.Error("Role 为空 —— 座位定位失败(Seats[4] 未被识别)")
	}
	if resp.FromCache {
		t.Error("首次计算不应标记 from_cache")
	}
}

func TestReview_R06_NotOverIsRejected(t *testing.T) {
	m := &WerewolfManager{rooms: map[string]*WerewolfRoom{}}
	r := newVoteRoom("room-r06")
	r.State.Phase = PhaseSpeak // 对局进行中
	r.Seats[0] = "user-bob"
	m.rooms[r.RoomID] = r
	ClearReviewCacheForRoom(r.RoomID)

	if _, err := m.ComputeReviewForUser(context.Background(), r.RoomID, "user-bob"); err == nil {
		t.Fatal("对局未结束却返回了复盘 —— 会泄漏本人角色牌给进行中的对局")
	}
}

func TestReview_R07_CacheHit(t *testing.T) {
	const uid = "user-carol"
	m := &WerewolfManager{rooms: map[string]*WerewolfRoom{}}
	r := newVoteRoom("room-r07")
	r.Seats[2] = uid
	r.State.LastDayVoteMap = map[Seat]Seat{2: 5}
	r.State.DayEliminated = 5
	r.recordVoteHistoryLocked(map[Seat]int{5: 1})
	m.rooms[r.RoomID] = r
	ClearReviewCacheForRoom(r.RoomID)

	first, err := m.ComputeReviewForUser(context.Background(), r.RoomID, uid)
	if err != nil {
		t.Fatalf("首次计算失败: %v", err)
	}
	if first.FromCache {
		t.Error("首次不应命中缓存")
	}
	second, err := m.ComputeReviewForUser(context.Background(), r.RoomID, uid)
	if err != nil {
		t.Fatalf("二次计算失败: %v", err)
	}
	if !second.FromCache {
		t.Error("二次应命中 30min 缓存(LookupReview 未接线)")
	}
	ClearReviewCacheForRoom(r.RoomID)
}

// TestReview_R08_PropHistorySnapshotOrder 环形缓冲绕圈后必须还原时间顺序。
func TestReview_R08_PropHistorySnapshotOrder(t *testing.T) {
	r := newVoteRoom("room-r08")
	// 写入 cap + 3 条,Round 递增便于验证顺序。
	for i := 0; i < propHistoryCap+3; i++ {
		r.recordPropHistoryLocked(PropHistoryRecord{FromSeat: 1, Round: i})
	}
	snap := r.propHistorySnapshotLocked()
	if len(snap) != propHistoryCap {
		t.Fatalf("快照长度 = %d, want %d", len(snap), propHistoryCap)
	}
	// 绕圈后最旧一条应是第 3 条写入(Round=3),最新是 Round=cap+2。
	if snap[0].Round != 3 {
		t.Errorf("首条 Round = %d, want 3(最旧)", snap[0].Round)
	}
	if last := snap[len(snap)-1].Round; last != propHistoryCap+2 {
		t.Errorf("末条 Round = %d, want %d(最新)", last, propHistoryCap+2)
	}
	// 严格单调递增(顺序完全还原)。
	for i := 1; i < len(snap); i++ {
		if snap[i].Round <= snap[i-1].Round {
			t.Fatalf("顺序未还原: snap[%d].Round=%d <= snap[%d].Round=%d",
				i, snap[i].Round, i-1, snap[i-1].Round)
		}
	}
}
