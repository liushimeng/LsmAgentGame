package werewolf

import (
	"time"

	"LsmAgentGame/config"
)

func (gs *GameState) LastNightDeathsCopy() []Seat {
	out := make([]Seat, len(gs.LastNightDeaths))
	copy(out, gs.LastNightDeaths)
	return out
}

func (gs *GameState) idiotRevealedSeats() []int {
	out := make([]int, 0, 1)
	for i := 0; i < MaxPlayers; i++ {
		if gs.Roles[i] == RoleIdiot && gs.Players[i].IdiotRevealed {
			out = append(out, i)
		}
	}
	return out
}

func (gs *GameState) Snapshot() map[string]any {
	return map[string]any{
		"phase":             gs.Phase.String(),
		"day":               gs.DayNumber,
		"sheriff":           int(gs.SheriffSeat),
		"wolf_alive":        gs.WolfAliveCnt,
		"good_alive":        gs.GoodAliveCnt,
		"divine_alive":      gs.DivineCnt,
		"plain_alive":       gs.PlainCnt,
		"idiot_revealed":    gs.idiotRevealedSeats(),
		"sheriff_streams":   [2]int{int(gs.SheriffStreams[0]), int(gs.SheriffStreams[1])},
		"winner":            gs.Winner,
		"status":            gs.Status,
		"last_night_deaths": gs.LastNightDeathsCopy(),
	}
}

func cfgWerewolfGraceSec() int {
	defer func() { _ = recover() }()
	return config.Load().Werewolf.FirstNightGraceSec
}

func cfgWerewolfForcedRounds() int {
	defer func() { _ = recover() }()
	return config.Load().Werewolf.FirstNightForcedSpeakRounds
}

func cfgWerewolfSpeakMinInterval() time.Duration {
	defer func() { _ = recover() }()
	n := config.Load().Werewolf.FirstNightSpeakMinIntervalSec
	if n <= 0 {
		return 30 * time.Second
	}
	return time.Duration(n) * time.Second
}

func (gs *GameState) SetPhaseDeadline(phase string, secs int) {
	if gs == nil {
		return
	}
	if secs <= 0 {
		gs.PhaseDeadlineAt = time.Time{}
		return
	}
	gs.PhaseDeadlineAt = time.Now().Add(time.Duration(secs) * time.Second)
}

func cfgAgentLLMCallTimeoutSec(seatCount int) int {
	defer func() { _ = recover() }()
	base := 300
	lenientSeatCount := 13
	scalePercent := 150
	if c := config.Load(); c != nil {
		if c.Werewolf.LLMCallTimeoutSec > 0 {
			base = c.Werewolf.LLMCallTimeoutSec
		}
		if c.Werewolf.LenientModeForSeatCount > 0 {
			lenientSeatCount = c.Werewolf.LenientModeForSeatCount
		}
		if c.Werewolf.LLMTimeoutScalePercent > 0 {
			scalePercent = c.Werewolf.LLMTimeoutScalePercent
		}
		if seatCount >= lenientSeatCount && scalePercent > 100 {
			scaled := base * scalePercent / 100
			if scaled > 480 {
				scaled = 480
			}
			return scaled
		}
	}
	return base
}

func cfgPhaseDeadlineSec(phase string, seatCount int, isHuman ...bool) int {
	var c *config.Config
	func() {
		defer func() { _ = recover() }()
		c = config.Load()
	}()
	human := len(isHuman) > 0 && isHuman[0]
	var base int
	if c == nil {
		base = defaultPhaseDeadlineSec(phase, human)
	} else {
		base = c.PhaseDeadlineSec(phase)
		if human {
			// §127: 真人房间使用更紧凑的默认 deadline(若配置未显式指定则覆盖)。
			if base == defaultPhaseDeadlineSec(phase, false) {
				base = defaultPhaseDeadlineSec(phase, true)
			}
		}
	}
	// Acting-phase floor: deadline MUST be >= 单次 LLM 调用总超时 + 30s buffer。
	// R131: 大房间额外增加 buffer,避免 13 并发时阶段被误 skip。
	if isActingPhase(phase) {
		llmTimeoutSec := 600 // 2026-07-24: 与 config.LLM.TimeoutMs 新默认值 600000 对齐
		if c != nil && c.LLM.TimeoutMs > 0 {
			llmTimeoutSec = c.LLM.TimeoutMs / 1000
		}
		// 使用 agent 层单次 LLM 调用总超时作为 floor 基准(含 lenient 缩放)。
		callTimeoutSec := cfgAgentLLMCallTimeoutSec(seatCount)
		if callTimeoutSec <= 0 {
			callTimeoutSec = llmTimeoutSec
		}
		// R131: 基础 buffer 30s + 每多 1 人 25s,13 人局额外 +150s;上限为 callTimeout+200s。
		floor := callTimeoutSec + 30
		if seatCount > 7 {
			floor += (seatCount - 7) * 25
			if floor > callTimeoutSec+200 {
				floor = callTimeoutSec + 200
			}
		}
		if base < floor {
			base = floor
		}
	}
	return base
}

func hasHumanPlayer(gs *GameState) bool {
	if gs == nil {
		return false
	}
	for i, uid := range gs.Seats {
		if uid == "" {
			continue
		}
		if !gs.Players[i].IsBot {
			return true
		}
	}
	return false
}

func isActingPhase(phase string) bool {
	switch phase {
	case "pre_wolves", "PhasePreWolves",
		"night_guard", "PhaseNightGuard",
		"night_wolves", "PhaseNightWolves",
		"night_seer", "PhaseNightSeer",
		"night_witch", "PhaseNightWitch",
		"night_demon_hunter", "PhaseNightDemonHunter", // §猎魔人
		"sheriff", "PhaseSheriff",
		"sheriff_order", "PhaseSheriffOrder", // §20260810-09 — 警长定序阶段
		"speak", "PhaseSpeak",
		"vote", "PhaseVote",
		"idiot_reveal", "PhaseIdiotReveal",
		"hunter_shoot", "PhaseHunterShoot",
		"suicide_take", "PhaseSuicideTake": // §20260830-02 — 自爆带走
		return true
	}
	return false
}

func defaultPhaseDeadlineSec(phase string, human bool) int {
	switch phase {
	case "pre_wolves", "PhasePreWolves":
		if human {
			return 180
		}
		return 480
	case "speak", "PhaseSpeak":
		if human {
			return 180
		}
		return 420
	case "vote", "PhaseVote":
		if human {
			return 120
		}
		return 360
	case "sheriff", "PhaseSheriff":
		if human {
			return 120
		}
		return 300
	case "sheriff_order", "PhaseSheriffOrder":
		// §20260810-09 — 警长定序阶段 deadline:与 sheriff 竞选一致。
		// 30s 已足够 LLM 调用,真人 30s / 全 AI 30s(不区分 —— 该阶段简单决策,
		// 真人只需选 2 个单选框;watchdog 兜底走默认值不需要更长 deadline)。
		return 30
	case "hunter_shoot", "PhaseHunterShoot":
		if human {
			return 120
		}
		return 300
	case "suicide_take", "PhaseSuicideTake":
		// §20260830-02 — 自爆带走 deadline 与 hunter_shoot 对齐(单决策,
		// 真人 120s / 全 AI 300s 覆盖慢模型 LLM + 重试)。
		if human {
			return 120
		}
		return 300
	case "night_wolves", "PhaseNightWolves":
		if human {
			return 120
		}
		return 300
	case "night_guard", "PhaseNightGuard":
		// §134 守卫守护阶段 deadline:与其它夜间阶段一致(human=120 / 全 AI=300)。
		if human {
			return 120
		}
		return 300
	case "night_seer", "PhaseNightSeer":
		if human {
			return 120
		}
		return 300
	case "night_witch", "PhaseNightWitch":
		if human {
			return 120
		}
		return 300
	case "night_demon_hunter", "PhaseNightDemonHunter":
		// §猎魔人 猎魔人狩猎阶段 deadline:与其它夜间阶段一致(human=120 / 全 AI=300)。
		if human {
			return 120
		}
		return 300
	case "dawn", "PhaseDawn":
		return 8
	case "death_lyric", "PhaseDeathLyric":
		// BUG-R11 (2026-07-30): 真人玩家死亡时,death_lyric 阶段至少给 60s
		// 留遗言时间。原 30s 对真人太短(R11 报告 seat 1 真人死亡后 30s 即被
		// 强制跳过,无遗言机会)。全 AI 房间保持 30s(bot 响应快)。
		if human {
			return 60
		}
		return 30
	case "idiot_reveal", "PhaseIdiotReveal":
		if human {
			return 120
		}
		return 300
	default:
		if human {
			return 150 // acting phases with human players: tighter
		}
		return 360 // acting phases full-AI: >= llm_timeout(300) + 60s buffer
	}
}

func setPhaseAndDeadline(gs *GameState, p Phase, isHuman ...bool) {
	if gs == nil {
		return
	}
	gs.Phase = p
	var humanFlag bool
	if len(isHuman) > 0 {
		humanFlag = isHuman[0]
		// 调用方显式传入时同步到 snapshot,保持后续 setPhaseAndDeadline 一致。
		gs.hasHumanSnapshot = isHuman[0]
	} else if gs.hasHumanSnapshot {
		humanFlag = true
	} else {
		humanFlag = hasHumanPlayer(gs)
	}
	gs.SetPhaseDeadline(p.String(), cfgPhaseDeadlineSec(p.String(), gs.SeatCount, humanFlag))
}

func cfgWerewolfMinSpeaks() int {
	defer func() { _ = recover() }()
	n := config.Load().Werewolf.MinSpeaksPerMinute
	if n <= 0 {
		return 2
	}
	return n
}

func cfgWerewolfSpectatorFullWake() bool {
	defer func() { _ = recover() }()
	return config.Load().Werewolf.SpectatorFullWake
}

func cfgWerewolfChatHistoryBytes() int {
	defer func() { _ = recover() }()
	n := config.Load().Werewolf.ChatHistoryBytes
	if n <= 0 {
		return 500 * 1024
	}
	return n
}

