package werewolf

// influence_tracker.go — 发言影响力生态(§20260811-02 U1)
//
// 起源:Agent-Surpport-01.md §3.1(DeepSeek §二.1)。
//
// 设计要点:当前系统里「发言」的唯一后果是「被别人读到」——一个 Agent 说了极有洞察力的话,
// 和说了一句废话,在系统层面完全等价。本模块把「发言的后果」量化为 0~100 的公开分数,
// 让社交资本成为「第二血量」。
//
// 四个信号(权重合计 100),**全部基于公开信息,人人可复算**:
//   - 跟票率  Persuasion 40 — 上一轮白天有多少人的投票目标与本座位一致(LastDayVoteMap)
//   - 关注度  Attention  25 — 本座位作为 whisper 收件人 / 道具目标被指向的次数(对数归一)
//   - 发言参与 Presence  20 — 本座位在 recentSpeeches 滚动缓冲中的占比
//   - 存活加成 Survival  15 — 存活轮数占比;死亡后冻结不再增长
//
// 硬约束对照:
//   - §92a 锁内变体:所有方法均为 *Locked 语义(调用方必须已持 r.mu)。
//     **绝不**提供会自行加锁的公开变体 —— R212 的教训:被 BuildClientState* 家族
//     调用的方法默认必须是 *Locked,否则第二次 Lock() 永久阻塞。
//   - §119 协议层隔离:影响力是**公开**信息(与 HeartThought 相反),刻意进 wire,
//     全房玩家 + 观战者均可见。
//   - §120 公平性:公式不含任何 LLM 判断信号(如"发言质量"),13 个座位在同一把尺子下
//     被度量;公式随分数一起写进 prompt,避免 Agent 把它当黑箱而产生迷信行为。
//   - §135 身份公开:影响力只反映公开行为(票型/发言/被指向),不含任何角色信息。

import "math"

// 五个信号的权重上限。合计 100。修改这里即修改公式,
// 同时必须同步更新 agent/wwplayer/prompt.go 的 InfluenceBlock 公式说明文案。
//
// §20260812-02 U2 — 新增第 5 维度 Insight（洞察力）,从原有四维度让渡 15 分。
// Insight 基于客观行为信号(发言长度 > 50 字的比例 + 平均发言长度),
// 不含任何 LLM 判断(§120 公平性)。
const (
	influenceMaxPersuasion = 35
	influenceMaxAttention  = 20
	influenceMaxPresence   = 18
	influenceMaxSurvival   = 12
	influenceMaxInsight    = 15
)

// influenceAttentionSaturation 是关注度信号的对数归一饱和点:
// 被指向 8 次即拿满 Attention 分。取 8 是因为 13 人局单轮 whisper + 道具
// 指向同一座位超过 8 次已属极端集火。
const influenceAttentionSaturation = 8.0

// InfluenceScore 是一个座位的影响力明细。
// 分项公开下发,便于前端 tooltip 展示与玩家自行复算(公式透明是本机制的前提)。
//
// §20260812-02 U2 — 新增 Insight 维度(洞察力,0~15),基于发言长度等客观信号。
type InfluenceScore struct {
	Seat       int `json:"seat"`
	Total      int `json:"total"`      // 0~100
	Persuasion int `json:"persuasion"` // 0~35 跟票率
	Attention  int `json:"attention"`  // 0~20 关注度
	Presence   int `json:"presence"`   // 0~18 发言参与
	Survival   int `json:"survival"`   // 0~12 存活加成
	Insight    int `json:"insight"`    // 0~15 洞察力(§20260812-02 U2)
}

// InfluenceTracker 房间级影响力存储。
// 懒初始化(influenceTrackerLocked),与 infoLedger / commitmentLedger / hypothesisStore 同模式,
// 避免 6 处 WerewolfRoom 构造点同步遗漏。
type InfluenceTracker struct {
	scores map[int]*InfluenceScore
	round  int
}

// NewInfluenceTracker 构造空 tracker。
func NewInfluenceTracker() *InfluenceTracker {
	return &InfluenceTracker{scores: make(map[int]*InfluenceScore, MaxPlayers)}
}

// GetLocked 返回某座位的影响力明细;未计算过时返回 nil。
// **调用前置**:必须已持 r.mu(§92a)。
func (t *InfluenceTracker) GetLocked(seat int) *InfluenceScore {
	if t == nil || seat < 0 || seat >= MaxPlayers {
		return nil
	}
	return t.scores[seat]
}

// SnapshotLocked 返回全部座位的影响力快照,按座位升序(稳定输出)。
// **调用前置**:必须已持 r.mu(§92a)。
func (t *InfluenceTracker) SnapshotLocked() []InfluenceScore {
	if t == nil || len(t.scores) == 0 {
		return nil
	}
	out := make([]InfluenceScore, 0, len(t.scores))
	for seat := 0; seat < MaxPlayers; seat++ {
		if s := t.scores[seat]; s != nil {
			out = append(out, *s)
		}
	}
	return out
}

// RoundLocked 返回上次重算时的天数。
// **调用前置**:必须已持 r.mu(§92a)。
func (t *InfluenceTracker) RoundLocked() int {
	if t == nil {
		return 0
	}
	return t.round
}

// influenceTrackerLocked 懒初始化访问器(同 ledgerLocked / hypothesisStoreLocked 模式)。
// **调用前置**:必须已持 r.mu(§92a)。
func (r *WerewolfRoom) influenceTrackerLocked() *InfluenceTracker {
	if r.influenceTracker == nil {
		r.influenceTracker = NewInfluenceTracker()
	}
	return r.influenceTracker
}

// RecalculateInfluenceLocked 重算全部座位的影响力分数。
//
// 调用时机:fillDayVoteMapLocked 末尾 —— 那里正好是「本轮票型刚落地」的时刻,
// 跟票率信号此刻最新鲜。该调用点已持 r.mu。
//
// **调用前置**:必须已持 r.mu(§92a)。本函数不加锁,也不提供公开变体。
func (r *WerewolfRoom) RecalculateInfluenceLocked() {
	if r == nil || r.State == nil {
		return
	}
	tracker := r.influenceTrackerLocked()
	tracker.round = r.State.DayNumber

	seatCount := r.State.SeatCount
	if seatCount <= 0 || seatCount > MaxPlayers {
		seatCount = MaxPlayers
	}

	aliveCount := 0
	for i := 0; i < seatCount; i++ {
		if r.State.AliveSeat(Seat(i)) {
			aliveCount++
		}
	}

	// ─── 预聚合:发言占比分母 + Insight(发言长度)───
	totalSpeeches := 0
	speechBySeat := make(map[int]int, seatCount)
	// §20260812-02 U2 — Insight 维度预聚合
	longSpeakBySeat := make(map[int]int, seatCount)   // 字数 > 50 的发言次数
	totalRunesBySeat := make(map[int]int, seatCount)   // 总 rune 数
	for _, ev := range r.recentSpeeches {
		if ev.Seat < 0 || ev.Seat >= seatCount || ev.IsSpectator {
			continue // 观战者发言不占座位影响力
		}
		speechBySeat[ev.Seat]++
		totalSpeeches++
		// Insight 信号:发言长度
		rLen := len([]rune(ev.Text))
		totalRunesBySeat[ev.Seat] += rLen
		if rLen > 50 {
			longSpeakBySeat[ev.Seat]++
		}
	}

	// ─── 预聚合:关注度(被 whisper 指向 + 被道具指向)───
	attentionBySeat := make(map[int]int, seatCount)
	for recipient, inbox := range r.whisperInbox {
		if recipient < 0 || recipient >= seatCount {
			continue
		}
		attentionBySeat[recipient] += len(inbox)
	}
	for _, rec := range r.GetPropHistoryLocked(0) {
		if rec.ToSeat < 0 || rec.ToSeat >= seatCount {
			continue // AOE 道具 ToSeat=-1,不计入任何单一座位
		}
		attentionBySeat[rec.ToSeat]++
	}

	// ─── 预聚合:跟票(有多少人与我投了同一目标)───
	// LastDayVoteMap 是 voter→target;我的目标是 T 时,
	// 「跟随我的人数」= 其他所有投了 T 的人数。
	followersBySeat := make(map[int]int, seatCount)
	if len(r.State.LastDayVoteMap) > 0 {
		targetCount := make(map[Seat]int, seatCount)
		for _, target := range r.State.LastDayVoteMap {
			targetCount[target]++
		}
		for voter, target := range r.State.LastDayVoteMap {
			if int(voter) < 0 || int(voter) >= seatCount {
				continue
			}
			// 减 1 排除自己这一票
			followersBySeat[int(voter)] = targetCount[target] - 1
		}
	}

	for seat := 0; seat < seatCount; seat++ {
		// 空座位不计分
		if r.State.Seats[seat] == "" {
			delete(tracker.scores, seat)
			continue
		}

		score := &InfluenceScore{Seat: seat}

		// ① 跟票率 Persuasion —— 有多少人跟我投了同一个目标
		if aliveCount > 1 {
			followers := followersBySeat[seat]
			if followers > 0 {
				ratio := float64(followers) / float64(aliveCount-1)
				score.Persuasion = clampInfluence(
					int(math.Round(ratio*float64(influenceMaxPersuasion))), influenceMaxPersuasion)
			}
		}

		// ② 关注度 Attention —— 对数归一,8 次指向即饱和
		if n := attentionBySeat[seat]; n > 0 {
			ratio := math.Log1p(float64(n)) / math.Log1p(influenceAttentionSaturation)
			score.Attention = clampInfluence(
				int(math.Round(ratio*float64(influenceMaxAttention))), influenceMaxAttention)
		}

		// ③ 发言参与 Presence —— 在滚动发言缓冲中的占比
		if totalSpeeches > 0 {
			ratio := float64(speechBySeat[seat]) / float64(totalSpeeches)
			score.Presence = clampInfluence(
				int(math.Round(ratio*float64(influenceMaxPresence))), influenceMaxPresence)
		}

		// ④ 存活加成 Survival —— 存活者拿满;死者冻结在 0(历史分项仍保留可见)
		if r.State.AliveSeat(Seat(seat)) {
			score.Survival = influenceMaxSurvival
		}

		// ⑤ 洞察力 Insight (§20260812-02 U2) —— 基于发言深度的客观信号:
		//   60% 长发言占比(字数 > 50 的发言次数 / 总发言次数,对数归一饱和 5 次)
		//   40% 平均发言长度(总 rune 数 / 发言次数,饱和 200 字 → 100%)
		if totalSpeeches > 0 {
			sCnt := speechBySeat[seat]
			if sCnt > 0 {
				// 长发言占比
				longRatio := math.Log1p(float64(longSpeakBySeat[seat])) / math.Log1p(5.0)
				if longRatio > 1 {
					longRatio = 1
				}
				// 平均发言长度
				avgLen := float64(totalRunesBySeat[seat]) / float64(sCnt)
				lenRatio := avgLen / 200.0
				if lenRatio > 1 {
					lenRatio = 1
				}
				combined := longRatio*0.6 + lenRatio*0.4
				score.Insight = clampInfluence(
					int(math.Round(combined*float64(influenceMaxInsight))), influenceMaxInsight)
			}
		}

		score.Total = clampInfluence(
			score.Persuasion+score.Attention+score.Presence+score.Survival+score.Insight, 100)
		tracker.scores[seat] = score
	}
}

// clampInfluence 把分数夹到 [0, max]。
func clampInfluence(v, max int) int {
	if v < 0 {
		return 0
	}
	if v > max {
		return max
	}
	return v
}

// resetInfluenceLocked 清零影响力状态。restartGameLocked 原地重开新一局时调用,
// 与 infoLedger / commitmentLedger / hypothesisStore 同处理。
// **调用前置**:必须已持 r.mu(§92a)。
func (r *WerewolfRoom) resetInfluenceLocked() {
	r.influenceTracker = nil
}
