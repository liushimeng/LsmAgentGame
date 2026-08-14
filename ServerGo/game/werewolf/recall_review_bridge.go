// Package werewolf — recall_review_bridge.go: 个人复盘的 Manager 层接线
// (2026-08-14 §20260814-01 U1)。
//
// # 缺陷
//
// `recall_aggregate.go` 自 §20260812-01 U1 落地起就是**整个模块零生产调用**:
// `ComputeReviewFromInputs` / `CacheReview` / `LookupReview` /
// `ClearReviewCacheForRoom` 与 30 分钟 TTL 缓存全部写好且有单测，
// 前端 `PersonalReviewPanel.tsx`（198 行）也写好了并 fetch
// `GET /api/games/werewolf/rooms/:id/review/:userId` ——
// 但那条路由从不存在，组件也从未被任何文件 import。
//
// 最能说明问题的是 `recall_aggregate.go:11` 的自检条款：
//
//	§130 接线:ComputeReviewFromInputs 必须被 ComputeReviewForUser(Manager) 调一次
//
// 而 `ComputeReviewForUser` **从来没有被实现过**。这是 §20260812-04 教训 (1)
// 「注释里的自检条款不会被执行，必须转化为测试断言」的又一次印证 ——
// 作者写下了正确的接线要求，然后没有接线。
//
// # 本文件补齐的两件事
//
//  1. `recordVoteHistoryLocked` —— 逐日票型采集（复盘的投票准确率维度需要
//     整局每一天的票型，而 State.LastDayVoteMap 只存最后一天）。
//  2. `ComputeReviewForUser` —— 缺失的 Manager 入口，§92a 快照范式。
package werewolf

import (
	"context"
	"errors"
	"time"
)

// reviewVoteHistoryMaxDays 限制 voteHistory 的长度上限。
//
// 13 人局正常在 6~10 天内结束；取 16 留足余量的同时防止异常长局
// （或 restart 未清零的极端场景）无界增长。超限时丢弃**最旧**一天
// —— 复盘看的是整局表现，越近的天数信息价值越高。
const reviewVoteHistoryMaxDays = 16

// ErrReviewNotOver 对局未结束时拒绝出复盘。
//
// 与 ErrRecallNotOver 分开定义而非复用：两者语义相同但面向不同 API，
// 将来若复盘要放宽到「冷却期也可看」而 recall 不放宽，共用哨兵会互相绑死。
var ErrReviewNotOver = errors.New("review: game not over yet")

// ErrReviewForbidden 只能查看自己的复盘（§135 身份公开公平性）。
var ErrReviewForbidden = errors.New("review: may only view your own review")

// recordVoteHistoryLocked 把「本日票型 + 放逐结果」追加到 r.voteHistory。
//
// # 调用契约（三条 FinishVote 路径必须全部接上）
//
//   - 必须持 r.mu（§92a）。
//   - 必须在 `State.FinishVote` **之后**调用 —— DayEliminated 由 FinishVote
//     填写，在它之前调用会把每天都记成「无人出局」。
//   - tally 必须在 FinishVote **之前**抓取并传入 —— FinishVote 之后
//     TallyVotes 的输入状态已被改写，不可重现（room_action.go:320 的注释
//     早就记录了这个约束）。
//
// 这个「快照在前、写入在后」的错位是本函数必须接受 tally 参数而不能自己
// 计算的唯一原因。三条路径分别是：
//
//	room_action.go:333            Action_FinishVote（人类/正常路径）
//	room_quarantine_skip_locked.go:428  driver auto-tally（全员投完自动结算）
//	room_quarantine_skip_locked.go:458  finishVoteLocked（quarantine/watchdog 救援）
//
// 漏任何一条 = 该天票型静默丢失，复盘的投票准确率偏低且无人察觉
// —— 正是 §132 教训 (3)「同一语义在 manager/agent 双路径必须同步」的形态。
func (r *WerewolfRoom) recordVoteHistoryLocked(tally map[Seat]int) {
	if r == nil || r.State == nil {
		return
	}
	// 票型最大集合（可能多人并列 —— 平票时 DayEliminated 为 NoSeat，
	// 但「与最高票同票型」仍是复盘的 0.5 分档，故必须保留）。
	top := 0
	for _, c := range tally {
		if c > top {
			top = c
		}
	}
	tallyMax := make([]int, 0, 2)
	if top > 0 {
		for s, c := range tally {
			if c == top {
				tallyMax = append(tallyMax, int(s))
			}
		}
	}

	// 全场票型快照：index=voter seat，value=投票目标（-1 = 弃权/未投）。
	votes := make([]int, MaxPlayers)
	for i := range votes {
		votes[i] = -1
	}
	for voter, target := range r.State.LastDayVoteMap {
		if voter >= 0 && int(voter) < MaxPlayers && target >= 0 && int(target) < MaxPlayers {
			votes[int(voter)] = int(target)
		}
	}

	rec := VoteReviewRecord{
		DayEliminated: int(r.State.DayEliminated),
		Votes:         votes,
		TallyMax:      tallyMax,
	}
	// DayEliminated 的 NoSeat 哨兵是 -1，与 VoteReviewRecord 的
	// 「<0 则当日无人出局」约定一致，无需转换。

	r.voteHistory = append(r.voteHistory, rec)
	if len(r.voteHistory) > reviewVoteHistoryMaxDays {
		// 丢最旧一天（保留近期）。
		r.voteHistory = r.voteHistory[len(r.voteHistory)-reviewVoteHistoryMaxDays:]
	}
}

// resetVoteHistoryLocked 清零逐日票型（重开局时调用，跨局隔离）。
// 与 §20260811-08 U2 的 settlementRewarded 跨局重置同款纪律：
// 任何「整局累积」状态都必须在 restartGameLocked 清零，否则第二局的复盘
// 会把第一局的票算进去。
func (r *WerewolfRoom) resetVoteHistoryLocked() {
	if r == nil {
		return
	}
	r.voteHistory = nil
	r.judgeTrustTrace = nil
}

// ComputeReviewForUser 是 recall_aggregate.go:11 声明却从未实现的 Manager 入口。
//
// 流程（§92a 严格照抄 recall_chat.go:189 的快照范式）：
//
//	1. 先查 30min 缓存（LookupReview）→ 命中直接返回；
//	2. m.mu.RLock 取房间 → lockRoomBriefly(500ms) 有界等待；
//	3. 校验 Phase == PhaseGameOver（复盘只在终局后开放）；
//	4. **锁内**构造 PersonalReviewInputs（只读 r 的字段，不调用 r 的方法）；
//	5. r.mu.Unlock() → **锁外**调纯函数 ComputeReviewFromInputs；
//	6. CacheReview 写回缓存。
//
// 权限校验（userID 是否本人 / 是否房间成员）由 API 层负责 —— 与
// RecallChat 的分层一致，Manager 只管数据。
func (m *WerewolfManager) ComputeReviewForUser(ctx context.Context, roomID, userID string) (*PersonalReviewResponse, error) {
	if userID == "" {
		return nil, errors.New("review: empty user id")
	}
	// 1. 缓存优先（复盘是纯只读聚合，30min 内重复请求无需重算）。
	if cached := LookupReview(ctx, roomID, userID); cached != nil {
		return cached, nil
	}

	m.mu.RLock()
	r := m.rooms[roomID]
	m.mu.RUnlock()
	if r == nil {
		return nil, ErrReviewNotOver
	}

	// 2. 有界等锁（§82c：被引擎长持的锁不能无限等）。
	if !lockRoomBriefly(r, 500*time.Millisecond) {
		return nil, errors.New("review: room busy, retry later")
	}
	if r.State == nil || r.State.Phase != PhaseGameOver {
		r.mu.Unlock()
		return nil, ErrReviewNotOver
	}

	// 3. 定位该 userID 的座位（Seats[seat] = userID）。
	seat := -1
	for i, uid := range r.Seats {
		if uid != "" && uid == userID {
			seat = i
			break
		}
	}

	in := PersonalReviewInputs{UserID: userID}

	// 3a. 投票记录 —— 只保留**该座位**的票。
	//
	// computeVoteAccuracy（recall_aggregate.go:94）对 Votes 切片的处理是
	// 「取第一个非 -1 且非自投的票」当作『我』的票。那个启发式只在输入已
	// 按座位过滤时才正确；直接塞全场快照会把别人的票算成我的。
	// 所以这里显式构造「只含我一票」的切片 —— 让那个启发式必然取到我的票。
	if seat >= 0 {
		for _, h := range r.voteHistory {
			mine := -1
			if seat < len(h.Votes) {
				mine = h.Votes[seat]
			}
			one := make([]int, MaxPlayers)
			for i := range one {
				one[i] = -1
			}
			// 自投会被那个启发式跳过（v != s），而狼人杀不允许自投，
			// 故这里无需特殊处理：mine 恒为他人座位或 -1。
			if mine >= 0 && mine < MaxPlayers {
				one[seat] = mine
			}
			in.VoteRecords = append(in.VoteRecords, VoteReviewRecord{
				DayEliminated: h.DayEliminated,
				Votes:         one,
				TallyMax:      h.TallyMax,
			})
		}
	}

	// 3b. 发言文本 —— 从房间共享 500K 队列按座位过滤。
	// 只取该座位发出的**非私聊、非活动事件**公开发言（复盘算的是
	// 「公开发言是否暴露身份」，whisper 与系统活动事件都不在其列）。
	if r.chatQueue != nil && seat >= 0 {
		for _, msg := range r.chatQueue.Tail(0) {
			if msg.FromSeat != seat || msg.IsWhisper || msg.IsActivity || msg.IsSpectator {
				continue
			}
			if msg.Text != "" {
				in.SpeakTexts = append(in.SpeakTexts, msg.Text)
			}
		}
	}

	// 3c. 道具记录 —— propHistory 是环形缓冲，按 FromSeat 过滤。
	for _, p := range r.propHistorySnapshotLocked() {
		if p.FromSeat == seat {
			in.PropRecords = append(in.PropRecords, PropReviewRecord{
				UserID: userID,
				IsHit:  p.Hit,
			})
		}
	}

	// 3d. Agent 互动质量 —— 目前引擎**不按 userID 累计**质询/挑战次数
	// （ChallengeCount 只在 view_godmode.go:231 被硬编码为 1）。
	// 故这两个计数保持 0，该维度得分为 0 —— 前端 4 维卡片会显示
	// 「暂无数据」而不是假数据。
	//
	// ⚠️ 这是**已知缺口**，不是遗漏：待实施项 §4.1「定向质询机制」落地后
	// （那里会引入真正的 per-user 质询计数），在此处接上即可。
	// 宁可显示 0 也不编造数字 —— 复盘一旦给出假分数就失去了全部价值。

	// 3e. 身份与胜负（§135：仅本人可见，API 层已校验 userID == 调用者）。
	if seat >= 0 {
		in.Role = r.State.Roles[seat].String()
	}
	in.Winner = r.State.Winner

	r.mu.Unlock()

	// 4. 锁外纯函数聚合。
	rev := ComputeReviewFromInputs(in)
	if rev == nil {
		return nil, errors.New("review: aggregation returned nil")
	}
	CacheReview(roomID, userID, rev)
	return &PersonalReviewResponse{
		Review:     rev,
		ComputedAt: time.Now().UnixMilli(),
		FromCache:  false,
	}, nil
}

// propHistorySnapshotLocked 返回 propHistory 的有序快照（最旧→最新）。
//
// propHistory 是容量 20 的环形缓冲（room_prop.go:28）：未满时 head 指向下一个
// 写入位、切片本身即顺序；满了之后 head 指向最旧一条。直接遍历原切片会在
// 环绕后拿到乱序结果 —— 对复盘的命中率统计（只数 Hit）无影响，但对将来
// 任何按时间顺序的消费就是缺陷，故在此统一还原顺序。
//
// 必须持 r.mu 调用。
func (r *WerewolfRoom) propHistorySnapshotLocked() []PropHistoryRecord {
	if r == nil || len(r.propHistory) == 0 {
		return nil
	}
	out := make([]PropHistoryRecord, 0, len(r.propHistory))
	if len(r.propHistory) < propHistoryCap {
		// 未满：切片本身就是时间顺序。
		out = append(out, r.propHistory...)
		return out
	}
	// 已满：从 head（最旧）开始环绕一圈。
	for i := 0; i < propHistoryCap; i++ {
		out = append(out, r.propHistory[(r.propHistoryHead+i)%propHistoryCap])
	}
	return out
}
