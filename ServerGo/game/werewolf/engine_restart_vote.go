// Package werewolf — engine_restart_vote.go: 狼人杀一局结束后"重开局投票"
// 的引擎辅助函数。详见 docs/狼人杀-Agent与系统/狼人杀重开局投票设计.md。
//
// 设计要点:
//   - checkWinner 把 Status="over" + Phase=PhaseGameOver;manager 持有 r.mu
//     期间会进一步判定是否进入 PhaseRestartVote (基于 cfg.Werewolf.RestartVote.Enabled
//     与座位数),从而引入"原地重开"窗口。
//   - 所有 engine-side helper 都假定已持有 r.mu。lock-held 派发路径在
//     room_restart_vote.go 实现。
//
// 投票语义(由 docs §2 + §3 决定):
//   - seat 必须在原 7 入座列表内(无论 alive);每 seat 一次,覆盖写。
//   - 同意比例: yesCount ≥ floor(N_eligible * num/den) + 1 — 即达到 num/den 的
//     "ceil" 含义 + 1 票缓冲(防止 1:1 平局永不结束)。
//   - deadline 到时 → 默认 outcome=timeout,管理路径走 forceCloseRoomLocked。
package werewolf

import (
	"LsmWebGame/config"
	"LsmWebGame/errcode"
	"LsmWebGame/logger"
	"go.uber.org/zap"
)

// ─────────────────── 投票辅助 ───────────────────

// restartVoteEligibleSeatsLocked 返回有资格投票的座位(已经在 r.Seats 内,
// 即参与过本局 — 不管 alive / dead)。caller 必须持 r.mu。
//
// 规则:
//   - 只看 r.Seats 中非空位;观战者(从未入座)无投票权。
//   - 强制至少 2 名(由 cfg.RestartVote.MinPlayers 保证;若调用方已经过滤,这里
//     只做计数)。
func restartVoteEligibleSeatsLocked(r *WerewolfRoom) []Seat {
	out := make([]Seat, 0, MaxPlayers)
	for i, uid := range r.Seats {
		if uid != "" {
			out = append(out, Seat(i))
		}
	}
	return out
}

// restartVoteQuorumFromConfig 安全读取通过比例(num/den),clamp 默认 2/3。
func restartVoteQuorumFromConfig() (num, den int) {
	num, den = 2, 3
	defer func() { _ = recover() }()
	c := config.Load()
	if c == nil {
		return num, den
	}
	if c.Werewolf.RestartVote.YesQuorumNumerator > 0 {
		num = c.Werewolf.RestartVote.YesQuorumNumerator
	}
	if c.Werewolf.RestartVote.YesQuorumDenominator > 0 {
		den = c.Werewolf.RestartVote.YesQuorumDenominator
	}
	if num >= den {
		// 防呆: 等于或超过 denominator 时退化为需全票。
		num = den
	}
	return num, den
}

// restartVoteMinPlayersFromConfig 安全读取最小玩家数。
func restartVoteMinPlayersFromConfig() int {
	defer func() { _ = recover() }()
	c := config.Load()
	if c != nil && c.Werewolf.RestartVote.MinPlayers > 0 {
		return c.Werewolf.RestartVote.MinPlayers
	}
	return 2
}

// shouldEnterRestartVoteLocked 在 Status="over" 后由 manager 调用,判定是否进入
// PhaseRestartVote。caller 必须持 r.mu。本函数纯查 config + 统计,不修改状态。
//
// 配置读取容错:config.Load() 在配置文件缺失时会 panic,由 defer recover()
// 兜底。此时视为 config=nil,即 RestartVote Enabled(与生产默认一致)。这保证
// 单元测试在任意 cwd 下都能正常运行,不依赖 LsmWebGame.conf 的物理位置。
func shouldEnterRestartVoteLocked(r *WerewolfRoom) bool {
	if r == nil || r.State == nil {
		return false
	}
	if r.State.Status != "over" {
		return false
	}
	if r.State.Phase == PhaseRestartVote || r.State.RestartVoteDone {
		// 已经在投票中或已结算,直接跳过(让 manager 走对应路径)
		return false
	}
	// 安全读取 config:config.Load() 在配置文件缺失时 panic,此时视为
	// config=nil(即 RestartVote 开启)。返回 (enabled, minPlayers)。
	enabled, minPlayers := func() (enabled bool, minPlayers int) {
		defer func() {
			if recover() != nil {
				// config.Load() panic = 配置文件缺失,视为 enabled + 默认 minPlayers。
				enabled = true
				minPlayers = 2
			}
		}()
		c := config.Load()
		if c != nil && !c.Werewolf.RestartVote.Enabled {
			return false, 0
		}
		return true, restartVoteMinPlayersFromConfig()
	}()
	if !enabled {
		return false
	}
	eligible := restartVoteEligibleSeatsLocked(r)
	if len(eligible) < minPlayers {
		return false
	}
	return true
}

// CastRestartVoteLocked 把 seat 的投票记入 GameState;若投票结果已达成 quorum
// 则在投票内同时结算(passed/rejected),但**不直接调 restartGameLocked**,该
// 路径由 manager 派发(避免 engine → manager 反向耦合)。
//
//   - seat: 投票者座位,必须已在 r.Seats 内。
//   - choice: "yes" | "no" | "abstain"。
//
// 返回 *errcode.Error 仅在参数非法时非 nil。caller 必须持 r.mu。
func CastRestartVoteLocked(r *WerewolfRoom, seat Seat, choice string) *errcode.Error {
	if r == nil || r.State == nil {
		return errcode.Code(errcode.ErrGameNotStarted)
	}
	if r.State.Phase != PhaseRestartVote {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "not in restart_vote phase")
	}
	if r.State.RestartVoteDone {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "restart vote already decided")
	}
	// 校验 seat 必须是 r.Seats 中的入座者
	if seat < 0 || seat >= MaxPlayers || r.Seats[seat] == "" {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "seat not occupied")
	}

	// 覆盖写 — 同一 seat 多次投票时把上一票从对应 map 删掉
	delete(r.State.RestartVoteYes, seat)
	delete(r.State.RestartVoteNo, seat)
	delete(r.State.RestartVoteAbstain, seat)

	switch choice {
	case "yes":
		r.State.RestartVoteYes[seat] = true
	case "no":
		r.State.RestartVoteNo[seat] = true
	case "abstain":
		r.State.RestartVoteAbstain[seat] = true
	default:
		return errcode.CodeMsg(errcode.ErrValidationFailed, "choice must be yes|no|abstain")
	}

	return nil
}

// EvaluateRestartVoteLocked 由 manager 在以下时机调用:
//   - 每次新投票写入后(检查是否已达到 passed 阈值)
//   - deadline tick 每 5s 检查(检查是否 timeout)
//   - watchdog 在 PhaseRestartVote 一切异常时回退(目前不实现,留扩展)
//
// 返回 outcome ∈ {"pending","passed","rejected","timeout"};passed/rejected/timeout
// 任一非 pending 时内部置 RestartVoteDone=true 并把 Phase 切回 PhaseGameOver。
//
// passed: yesCount ≥ ceil(eligible * num/den) + 1 (≥ quorum+1)
// rejected: noCount ≥ ceil(eligible/2) + 1
// timeout: deadline 到点且未 passed
//
// caller 必须持 r.mu。
func EvaluateRestartVoteLocked(r *WerewolfRoom, deadlineExpired bool) string {
	if r == nil || r.State == nil {
		return "pending"
	}
	if r.State.RestartVoteDone {
		return r.State.RestartVoteResult
	}

	eligible := restartVoteEligibleSeatsLocked(r)
	N := len(eligible)
	if N == 0 {
		// 无人有资格投票 → 直接关闭
		r.State.RestartVoteResult = "rejected"
		r.State.RestartVoteDone = true
		r.State.Phase = PhaseGameOver
		return "rejected"
	}

	yesCount := len(r.State.RestartVoteYes)
	noCount := len(r.State.RestartVoteNo)

	num, den := restartVoteQuorumFromConfig()
	// FastRestart 模式: 简单多数 ceil(N/2)+1 而非 2/3 超级多数。
	if r.State.FastRestart {
		num, den = 1, 2
	}
	yesQuota := (N*num + den - 1) / den // ceil(N * num/den)
	if yesQuota < 1 {
		yesQuota = 1
	}
	// 通过标准: yesCount ≥ yesQuota + 1  (quorum+1 缓冲,挡平局)
	if yesCount >= yesQuota+1 {
		r.State.RestartVoteResult = "passed"
		r.State.RestartVoteDone = true
		r.State.Phase = PhaseGameOver
		logger.L().Info("werewolf: restart vote passed",
			zap.String("room_id", r.RoomID),
			zap.Int("yes", yesCount), zap.Int("no", noCount),
			zap.Int("eligible", N),
			zap.Int("yes_quota", yesQuota))
		return "passed"
	}
	// 拒绝标准: noCount ≥ ceil(N/2)+1 (简单多数反对即终止)
	noQuota := (N + 1) / 2 // ceil(N/2)
	if noQuota < 1 {
		noQuota = 1
	}
	if noCount >= noQuota+1 {
		r.State.RestartVoteResult = "rejected"
		r.State.RestartVoteDone = true
		r.State.Phase = PhaseGameOver
		logger.L().Info("werewolf: restart vote rejected",
			zap.String("room_id", r.RoomID),
			zap.Int("yes", yesCount), zap.Int("no", noCount),
			zap.Int("eligible", N))
		return "rejected"
	}
	if deadlineExpired {
		r.State.RestartVoteResult = "timeout"
		r.State.RestartVoteDone = true
		r.State.Phase = PhaseGameOver
		logger.L().Info("werewolf: restart vote timeout",
			zap.String("room_id", r.RoomID),
			zap.Int("yes", yesCount), zap.Int("no", noCount),
			zap.Int("eligible", N))
		return "timeout"
	}
	return "pending"
}
