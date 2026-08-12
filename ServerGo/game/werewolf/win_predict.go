// Package werewolf — win_predict.go: §20260812-03 U1 阵营胜率热力图启发式算法。
//
// 设计动机:观战者需要直观看到每个玩家"是狼人"的概率分布。
// §13 跨职责约束要求胜率算在 game/werewolf 而非 agent 包;§120 公平性要求
// 公式仅使用客观行为信号(存活率/投票集中度),不含 LLM 决策。
//
// 启发式公式(纯客观,无 LLM 决策污染):
//
//	wolf_prob[seat] = base_alive_rate
//	                + public_vote_suspicion * 0.3
//	                + silence_penalty     * 0.05
//	                - over_speak_bonus    * 0.05
//
// 其中:
//   - base_alive_rate: 当前活人局狼占比(13 人局标准开局 4 狼 ≈ 0.30)
//   - public_vote_suspicion: 被本轮投票集中的座位加分(单点得票率 × 0.3)
//   - silence_penalty: 完全沉默 +0.05;发言 ≥5 次 -0.05(好人在悍跳/辩护)
//
// §135 隐私约束: 预言家查验结果(seer_check)是 LLM 私有信息,绝不能进入
// 公开胜率热力图(否则 §135 公平性破洞)。仅死后 §135 公开的 wolf_count
// 进入基线,其他位置完全靠行为信号。
//
// 数据源(全部为公开 game.state 字段,绝不读 PrivateKnowledge):
//   - r.State.Players[].Alive / Role(仅死亡 + §135 公开) / RolePubliclyRevealed
//   - r.voteTally (本轮投票集中度,公开)
//   - r.recentSpeeches[] (发言次数,公开)
//
// 输出:长度为 13 的 []float64,下标 0..12 对应 1..13 号位;值 ∈ [0.02,0.98] 归一化后。
//
// §92a 锁内变体约束:本文件函数命名 computeWinRateProbabilityLocked(*** r),
// 调用方必须已持有 r.mu(由 view.go BuildClientStateWithRoom 持锁入口调用)。
package werewolf

// computeWinRateProbabilityLocked §20260812-03 U1 — 计算 13 座位的"狼人概率"数组。
//
// 调用方必须持有 r.mu(§92a)。
// 返回值:长度 13 的 []float64,值已归一化到 0.02~0.98。
// 若房间状态异常(无存活玩家 / 阶段尚未到首日)返回均匀分布。
func computeWinRateProbabilityLocked(r *WerewolfRoom) []float64 {
	if r == nil || r.State == nil {
		return uniformProb13()
	}
	gs := r.State

	// 1. 统计基线(只用已公开身份)
	aliveCount := 0
	wolfCount := 0 // 通过公开的 role 字段(死亡 + §135 公开)
	for i := 0; i < MaxPlayers; i++ {
		p := &gs.Players[i]
		if !p.Alive {
			continue
		}
		aliveCount++
		if gs.RolePubliclyRevealed(Seat(i)) && p.Role == RoleWerewolf {
			wolfCount++
		}
	}
	if aliveCount == 0 {
		return uniformProb13()
	}

	// 2. 基线 = 当前公开狼人数 / 存活数;若完全未知,用 13 人局默认 4/13 ≈ 0.31
	var baseRate float64
	if wolfCount > 0 {
		baseRate = float64(wolfCount) / float64(aliveCount)
	} else {
		baseRate = 4.0 / 13.0
	}

	// 3. 投票集中度(仅 vote 阶段,调引擎已有的 TallyVotes)
	var voteTally map[Seat]int
	if gs.Phase == PhaseVote {
		voteTally = gs.TallyVotes(false)
	}

	// 4. 计算每个座位的概率
	probs := make([]float64, MaxPlayers)
	for seat := 0; seat < MaxPlayers; seat++ {
		p := &gs.Players[seat]
		prob := baseRate

		if !p.Alive && gs.RolePubliclyRevealed(Seat(seat)) {
			// (a) 已死亡且身份公开:身份直接定档(§135 允许)
			if p.Role == RoleWerewolf {
				prob = 0.98
			} else {
				prob = 0.02
			}
		} else if !p.Alive {
			// 已死亡但身份未公开(§135 公平性:普通人死不翻牌)
			prob = baseRate
		} else {
			// (b) 投票集中度修正(仅白天 vote 阶段)
			if voteTally != nil {
				voteShare := voteShareForSeatLocked(voteTally, Seat(seat))
				prob += voteShare * 0.3
			}

			// (c) 发言次数修正(沉默=嫌疑略升;过于活跃=稍降)
			speakCount := countSpeechesForSeatLocked(r, seat)
			switch {
			case speakCount == 0:
				prob += 0.05
			case speakCount >= 5:
				prob -= 0.05
			}
		}

		// 5. 钳制到 [0.02, 0.98],避免 0/1 极端(§135 隐私:不暴露确定身份)
		if prob < 0.02 {
			prob = 0.02
		}
		if prob > 0.98 {
			prob = 0.98
		}
		probs[seat] = prob
	}

	// 6. 归一化:让所有存活座位的概率和 ≈ 当前公开狼人数期望
	sumAliveProb := 0.0
	aliveSeats := 0
	for seat := 0; seat < MaxPlayers; seat++ {
		if gs.Players[seat].Alive {
			sumAliveProb += probs[seat]
			aliveSeats++
		}
	}
	if aliveSeats == 0 || sumAliveProb == 0 {
		return uniformProb13()
	}
	targetSum := float64(wolfCount)
	if targetSum == 0 {
		targetSum = 4.0 // 13 人局标准开局 4 狼
	}
	scale := targetSum / sumAliveProb
	for seat := 0; seat < MaxPlayers; seat++ {
		if gs.Players[seat].Alive {
			probs[seat] *= scale
			// 再次钳制(归一化后可能越界)
			if probs[seat] < 0.02 {
				probs[seat] = 0.02
			}
			if probs[seat] > 0.98 {
				probs[seat] = 0.98
			}
		}
	}
	return probs
}

// uniformProb13 返回 13 长度均匀分布(0.5)。用于房间状态异常 fallback。
func uniformProb13() []float64 {
	p := make([]float64, MaxPlayers)
	for i := range p {
		p[i] = 0.5
	}
	return p
}

// voteShareForSeatLocked 计算座位 seat 在 voteTally 中的得票占比。
func voteShareForSeatLocked(voteTally map[Seat]int, seat Seat) float64 {
	if voteTally == nil {
		return 0
	}
	total := 0
	for _, v := range voteTally {
		total += v
	}
	if total == 0 {
		return 0
	}
	return float64(voteTally[seat]) / float64(total)
}

// countSpeechesForSeatLocked 统计座位 seat 的累计发言次数(最近 20 轮窗口)。
func countSpeechesForSeatLocked(r *WerewolfRoom, seat int) int {
	if r == nil {
		return 0
	}
	cnt := 0
	for _, sp := range r.recentSpeeches {
		if sp.Seat == seat {
			cnt++
		}
	}
	return cnt
}
