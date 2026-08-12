package werewolf

import (
	"math/rand"
	"sync"
	"time"

	"LsmAgentGame/agent/wwtypes"
	"LsmAgentGame/errcode"
	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// ─────────────────── 阶段(Phase)枚举 ───────────────────

// Phase 对局阶段。
type Phase int

const (
	PhaseFilling          Phase = iota // 等待 7 人入座
	PhasePreWolves                     // BUG 2026-07-08: 首夜发言缓冲期(狼尚未刀人,只允许公开/私聊)
	PhaseNightGuard                    // §134 夜晚:守卫守护(盲守,在狼刀之前);无守卫或守卫已死时跳过
	PhaseNightWolves                   // 夜晚:狼人协商刀人
	PhaseNightSeer                     // 夜晚:预言家查验
	PhaseNightWitch                    // 夜晚:女巫用药
	PhaseNightDemonHunter              // §猎魔人 夜晚:猎魔人狩猎(DH1 首夜禁用);无猎魔人 / 猎魔人已死时跳过
	PhaseDawn                          // 黎明:公布死亡 + 警徽流结算
	PhaseSheriff                       // 警长竞选(仅第一天)
	PhaseSheriffOrder                  // §20260810-09 — 警长定序阶段(仅警长存活时启用);警长决定发言方向(顺/逆)+ 自位置(首/末)
	PhaseSpeak                         // 白天轮流发言
	PhaseVote                          // 白天投票放逐
	PhaseIdiotReveal                   // 白痴翻牌结算:投票放逐白痴时触发(2026-07-10 新增)
	PhaseHunterShoot                   // 猎人开枪(被放逐或被狼杀时)
	PhaseDeathLyric                    // 遗言:LastWords=true 的死者公开发言(2026-07-09 新增)
	// 2026-07-10: 游戏结束后「重开局投票」阶段(5 分钟窗口)。详见
	// docs/狼人杀-Agent与系统/狼人杀重开局投票设计.md。
	PhaseRestartVote
	PhaseGameOver // 对局结束
)

func (p Phase) String() string {
	switch p {
	case PhaseFilling:
		return "filling"
	case PhasePreWolves:
		// BUG 2026-07-08: 内部缓冲阶段。前端在 game.started 之后就处于
		// "夜晚·等待首夜缓冲结束"渲染态;agent 端只暴露 speak/interject/
		// whisper 三个工具,无任何会改变 game state 的动作。缓冲期结束
		// 后由 room.watchdog 自动切回 PhaseNightWolves。
		return "pre_wolves"
	case PhaseNightGuard:
		// §134 守卫守护阶段(在狼刀之前行动,"盲守"语义)。
		return "night_guard"
	case PhaseNightWolves:
		return "night_wolves"
	case PhaseNightSeer:
		return "night_seer"
	case PhaseNightWitch:
		return "night_witch"
	case PhaseNightDemonHunter:
		// §猎魔人 猎魔人狩猎阶段中文标签(与 engine PhaseNightDemonHunter.String() 对齐)。
		return "night_demon_hunter"
	case PhaseDawn:
		return "dawn"
	case PhaseSheriff:
		return "sheriff"
	case PhaseSheriffOrder:
		// §20260810-09 — 警长定序阶段。前端根据该 phase 渲染「⚖️ 警长正在决定发言顺序…」面板。
		return "sheriff_order"
	case PhaseSpeak:
		return "speak"
	case PhaseVote:
		return "vote"
	case PhaseIdiotReveal:
		return "idiot_reveal"
	case PhaseHunterShoot:
		return "hunter_shoot"
	case PhaseDeathLyric:
		// BUG 2026-07-09: 遗言阶段。LastWords=true 的出局玩家按座位升序发言。
		return "death_lyric"
	case PhaseRestartVote:
		// 2026-07-10: 重开局投票阶段。前端根据 game.state.phase_extra.restart_vote
		// 渲染投票面板 + 倒计时 + 上一局 winner。
		return "restart_vote"
	case PhaseGameOver:
		return "over"
	default:
		return "unknown"
	}
}

// IsNight 当前阶段是否属于夜晚(玩家应闭眼)。
// BUG 2026-07-08: 把 PhasePreWolves 也归到 "夜晚·等待" 类目下,这样前端的
// "夜晚·狼人协商刀人" / "夜晚·预言家查验" 渲染在缓冲期也成立(只是 UI
// 会改写标题为 "🕯️ 首夜发言缓冲期")。
// §猎魔人: 把 PhaseNightDemonHunter 也归到 "夜晚" 类目下。
func (p Phase) IsNight() bool {
	return p == PhasePreWolves || p == PhaseNightGuard || p == PhaseNightWolves || p == PhaseNightSeer || p == PhaseNightWitch || p == PhaseNightDemonHunter
}

// ─────────────────── 玩家 ───────────────────

// Player 单个玩家对局状态(集中在 GameState.Players[Seat])。
type Player struct {
	UserID    string
	Seat      Seat // 0..6
	Role      Role // 身份(死亡后仍可被全场查看)
	Alive     bool // 是否存活
	IsSheriff bool // 是否为警长
	IsBot     bool // 2026-07-12 §127 — 标记该座位是否为 AI bot

	// 神职使用过的药 / 查过的玩家(用于服务端一致性)
	WitchAntidoteUsed bool
	WitchPoisonUsed   bool
	LastSeerCheck     Seat // 上一晚预言家查验的玩家;NoSeat = 上一晚没查
	// §20260810-04 U3 — 预言家整局查验历史(每次 NightSeerCheck 成功追加)。
	// 警徽流结算(streamFaction)以此判定「是否真查过该目标」,关闭假预言家
	// 借警徽流造谣的漏洞(K3-F3/LongCat-F3)。存在 Player(GameState)而非依赖
	// 存活状态 —— 预言家死后(警长夜死场景)仍需结算警徽流。
	SeerCheckHistory []Seat

	// 投票与发言临时数据
	Voted         bool // 本轮白天是否已投
	VoteTarget    Seat // 本轮白天投票目标
	LastWords     bool // 当前死法下是否仍有遗言权
	HasSpoken     bool // 白天发言过 / 警长参选过(复用标记)
	IdiotRevealed bool // 白痴已翻牌(失去投票权与被投票权,但仍存活发言)

	// §198 骑士本局是否已用过决斗(K1/K2)。置位即锁定本局技能;
	// 同时被 RolePubliclyRevealed 读为公开身份的单一信号(K8)。
	KnightDuelUsed bool

	// §猎魔人 是否本局**曾发动过**狩猎(DH7 公开身份)。
	// 不同于 KnightDuelUsed(K1 每局限一次),猎魔人每晚可发动;
	// 仅作"是否曾发动"标记,不影响后续发动。
	DemonHunterHuntUsed bool

	// 2026-07-10 §123 增强 — 死因 + 决断。killPlayer 内部用 verdictFor(cause) 派生。
	// DeathCause: wolf/vote/hunter/witch_poison/suicide(空=未死亡)。
	// DeathVerdict: execution/death(空=未死亡)。绝不由玩家 Agent 主动设置。
	DeathCause   string
	DeathVerdict string

	// §135 身份公开公平性 — 猎人是否**已实际开枪**(亮身份带人)。
	// 仅在 HunterShoot 成功击杀目标(target != NoSeat)时置 true。
	// 猎人选择"不开枪"或被女巫毒杀无法开枪 → 保持 false，身份继续隐藏。
	HunterFired bool

	// 2026-08-07 §20260807-04 P0-3 — 人类反制道具 debuff(仅真人座位有效)。
	// 由 WerewolfRoom.setHumanDebuffLocked 在道具命中时写入;view.go 透传到客户端
	// 视图供 GameChatPanel / VotePanel 渲染(公告前缀 / 伪造投票推荐 / 乱码干扰)。
	// 仅作运行时状态,不进 DB。
	HumanDebuff *wwtypes.HumanDebuffSpec

	// §20260810-11 H2 — 质疑 challenge:本白天是否已用(0-indexed 局内日)。
	// 0=未用,1=已用。每日 dawn 时由 startDay 清零。
	ChallengeUsedToday bool
	// §20260810-11 H2 — 当前白天被谁质疑(-1=无)+ 质疑内容。
	// buildAgentContextLocked 注入 GameContext 给被质疑者,下一轮 prompt 出现「⚠️」块。
	// 仅当日有效,dawn 时清空。
	LastChallengedBy      int
	LastChallengeQuestion string

	// §20260811-10 U2 — 行为画像聚合字段(心理侧写数据源)。
	// SpeakCount:本局公开发言次数;InterruptCount:被打断/矛盾的次数(暂用
	// 兜底 0,真实「自相矛盾」判定需接入 LLM 复盘系统,后续版本扩展)。
	SpeakCount    int
	InterruptCount int
	// VoteCount:本局白天投票总次数;VoteAligned:与最终放逐目标一致的次数。
	VoteCount int
	VoteAligned int
}

// ─────────────────── 引擎与对局状态 ───────────────────

// LastWordsRounds 遗言规则:前两轮出局有遗言,之后夜死无遗言。
const LastWordsRounds = 2

// DeathCause 死因代码。cause → verdict 由 verdictFor(cause) 派生。
// 2026-07-10 §123 增强:在原 wolf/vote/hunter/witch_poison/suicide 基础上,
// 引入"处决 / 死亡"二分语义(详见 docs/狼人杀死亡语义设计.md)。
// §198 扩展:新增 DeathCauseDuel = "duel"(骑士自决)— verdictFor → execution。
// §猎魔人 扩展:新增 DeathCauseDemonHunterMisjudge = "demon_hunter_misjudge"(猎魔人误杀自决)— verdictFor → execution。
const (
	DeathCauseWolf                = "wolf"
	DeathCauseVote                = "vote"
	DeathCauseHunter              = "hunter"
	DeathCauseWitchPoison         = "witch_poison"
	DeathCauseSuicide             = "suicide"
	DeathCauseDuel                = "duel"                  // §198 骑士决斗猜错自决出
	DeathCauseDemonHunterMisjudge = "demon_hunter_misjudge" // §猎魔人 猎魔人误杀好人自决出
)

// DeathVerdict 死因决断(execution / death)。
//   - execution = 处决:玩家集体决策或自主决策导致的死亡(白天投票、白痴放弃翻牌、狼自爆、骑士自决、猎魔人误杀)
//   - death     = 死亡:系统/技能驱动的死亡(夜间狼刀、女巫毒杀、猎人反杀)
const (
	DeathVerdictExecution = "execution"
	DeathVerdictDeath     = "death"
)

// verdictFor cause → verdict 查表函数(单一事实来源)。
// wolf/hunter/witch_poison → death;vote/suicide/duel(§198)/demon_hunter_misjudge(§猎魔人) → execution。
// 未知 cause 兜底为 death(更安全,避免误把"夜间被狼杀"判为"处决")。
func verdictFor(cause string) string {
	switch cause {
	case DeathCauseWolf, DeathCauseHunter, DeathCauseWitchPoison:
		return DeathVerdictDeath
	case DeathCauseVote, DeathCauseSuicide, DeathCauseDuel, DeathCauseDemonHunterMisjudge:
		return DeathVerdictExecution
	default:
		return DeathVerdictDeath
	}
}

// RolePubliclyRevealed 是「某座位的身份是否已对全场公开」的**唯一事实来源**。
//
// §135 身份公开公平性规则(线上 APP / 线下标准竞技局统一):
//
//	普通死亡出局,所有人**不会**自动知道死者身份,死者身份牌全程不翻开。
//	法官只公布「几号玩家死亡」,不报角色。仅以下 6 类事件公开身份:
//
//	① 终局复盘   —— Status == "over",全员亮牌
//	② 白痴翻牌   —— 仅白天被投票放逐时触发;夜间被刀/被毒**不**翻牌
//	③ 狼人自爆   —— 白天自爆出局,全场立刻知道他是狼人
//	④ 猎人开枪   —— 被狼刀/被投票出局时主动亮身份带人;被女巫毒死不能开枪,
//	                身份依旧隐藏;主动选择"不开枪"同样不亮身份
//	⑤ 骑士决斗   —— §198 发动即亮身份;无论结果(命中狼 / 自决)都立即公开
//	⑥ 猎魔人发动 —— §猎魔人 发动即亮身份;无论结果(命中狼 / 误杀好人)都立即公开
//
// ⚠️ 本函数**不得**加入 `!Alive` 分支 —— 那正是 §135 之前的核心违规:
// 死亡即全场翻牌,使女巫毒药沦为免费验人、狼刀预言家的悍跳博弈价值归零。
// 所有视图层(BuildClientState / buildAllDeadListLocked / REST 房间详情)
// 必须统一走本函数,禁止各自复制判定。
func (gs *GameState) RolePubliclyRevealed(seat Seat) bool {
	if seat < 0 || seat >= MaxPlayers {
		return false
	}
	// ① 终局:全员复盘亮牌。
	if gs.Status == "over" {
		return true
	}
	p := &gs.Players[seat]
	// ② 白痴白天翻牌免死。
	if p.IdiotRevealed {
		return true
	}
	// ③ 狼人白天自爆。
	if p.DeathCause == DeathCauseSuicide {
		return true
	}
	// ④ 猎人实际开枪(亮身份带人)。
	if p.HunterFired {
		return true
	}
	// ⑤ §198 骑士发动决斗(亮身份带人/自决)— KnightDuelUsed=true 即翻牌,
	// 不论命中狼还是自决出,场上都已经知道他是骑士。
	if p.KnightDuelUsed {
		return true
	}
	// ⑥ §猎魔人 发动狩猎(亮身份)— DemonHunterHuntUsed=true 即翻牌,
	// 不论命中狼还是误杀好人,场上都已经知道他是猎魔人。
	// 注意:与骑士不同,猎魔人每晚可发动,此标志仅作"曾发动"标记,不影响后续发动。
	if p.DemonHunterHuntUsed {
		return true
	}
	return false
}

// BUG 2026-07-09: 遗言阶段每发言座位默认 30s 截止,与 §113 phase clock 协同。
// 配置项参见 config.WerewolfConfig.DeathLyricDeadlineSec;此处仅提供引擎默认值。
const DeathLyricDefaultDeadlineSec = 30

// ErrDeathLyricSkip 是 StartDeathLyricRound 的"无遗言可发"哨兵。
// 调用方应识别此哨兵并直接调 onDone 恢复原路径,而不是当错误处理。
var ErrDeathLyricSkip = errcode.CodeMsg(errcode.ErrValidationFailed, "no last words to say")

// WolfVoteTally 是 tallyWolfVotes() 的输出,供视图层序列化展示。
type WolfVoteTally struct {
	Counts map[int]int `json:"counts"` // target seat -> 得票数
	Tied   []int       `json:"tied"`   // 最高票并列目标
	Reason string      `json:"reason"` // "majority" | "random_tie_break" | "random_all_abstain"
	Final  int         `json:"final"`  // 最终击杀目标 seat
}

// GameState 一局狼人杀的权威状态(单线程访问,由 *WerewolfRoom.mu 串行化)。
type GameState struct {
	SeatCount int // 本局实际人数(13=默认标准局,12=werewolf_12 历史兼容,7=werewolf_7 历史兼容);MaxPlayers=13 为数组上界

	// 房间级常量
	Seats      [MaxPlayers]string // 座位 → userID
	Players    [MaxPlayers]Player // 座位 → Player
	PlayerByID map[string]Seat    // userID → 座位(快查)

	// 角色身份(座位 → 角色)。为与 Player.Role 冗余便于视图过滤,这里保留
	Roles       [MaxPlayers]Role
	SheriffSeat Seat

	// 警徽流(2026-07-10 新增,12 人竞技局核心机制)。仅预言家警长可生效。
	// SheriffStreams[0]/[1] 为第一/第二警徽流目标(NoSeat = 未声明); SheriffStreamsAt 为声明时间。
	// SheriffSuccessor 为警长死亡后的警徽继承者(由夜间结算自动算出 / 白天口头指定)。
	// SheriffStreamRounds[0/1] 为警徽流声明时的轮次(用于 §20260811-10 U5 保鲜期判定)。
	SheriffStreams       [2]Seat
	SheriffStreamsAt     [2]int64
	SheriffStreamRounds  [2]int
	SheriffSuccessor     Seat
	sheriffSlain         Seat // 夜间死亡警长;天亮结算警徽流(NoSeat=无)

	// §20260811-10 U3 — 狼队阵营金币池(悍跳失败赔偿)。
	// 狼人自爆 / 被白天投票放逐时 -30;女巫毒杀 / 猎人开枪 / 狼夜间互杀不扣。
	// 重入保护:WolfPoolPenaltyApplied[seat] 在该座位本局已被扣过一次后置 true,
	// 保证同一死亡事件多次触发只扣一次(§108 quarantine reentry 教训)。
	WolfPoolBalance          int64
	WolfPoolPenaltyApplied   [MaxPlayers]bool

	// 阶段机
	DayNumber      int
	Phase          Phase
	TurnActingSeat Seat   // 夜间当前应行动座位(白天 = NoSeat)
	SpeakTurnSeat  Seat   // 白天当前应发言座位
	SpeakOrder     []Seat // 本轮发言顺序

	// §20260810-09 — 警长定序权(规则补全)。
	// SheriffOrderSet=true 时 startSpeakPhase 按 SheriffSpeakDirection + SheriffSpeakSelfPos 生成 SpeakOrder;
	// 警长未在 PhaseSheriffOrder 内决定时 watchdog 兜底默认值(顺时针 + 警长首发言)。
	SheriffOrderSet       bool   `json:"sheriff_order_set"`
	SheriffSpeakDirection string `json:"sheriff_speak_direction"` // "cw" 顺时针 / "ccw" 逆时针
	SheriffSpeakSelfPos   string `json:"sheriff_speak_self_pos"`  // "first" 警长先发言 / "last" 警长后发言

	// 死亡公布/遗言控制
	NightDeaths      []Seat // 当晚狼刀未被解药救活的人(供女巫阶段累加)
	LastNightDeaths  []Seat // 上一晚死亡的玩家(用于天亮公布)
	DayEliminated    Seat   // 白天投票放逐的人;NoSeat = 无人出局
	DayTiedPlayers   []Seat // 平票者(用于辩护)
	SuicidedWolfSeat Seat   // 自爆狼的座位;NoSeat = 本轮未自爆

	// 夜晚临时状态(wolves 阶段写入,witch 读取)
	WolfKillTarget Seat // NoSeat = 空刀

	// ─── 狼人夜间投票 (2026-07-17) ───
	// WolfVotes[seat] 记录每个狼人的投票;NoSeat=未投票/弃权。
	WolfVotes [MaxPlayers]Seat
	// WolfVoteCast[seat] 标记该座位是否已提交投票(含弃权),用于区分"未投票"和"已投弃权"。
	WolfVoteCast [MaxPlayers]bool
	// WolfVoteReasons[seat] 记录该狼人投票附带的刀人理由(§20260810-04 U2,
	// K3-S1 二期;rune 截断 ≤30 字,空串 = 无理由/弃权)。与 WolfVotes 同生命周期
	// (startNight 一同重置),仅狼 bot GameContext 可见(WolfVoteReasons),
	// 不进 ClientGameState / chat 表 —— 狼队通道对玩家保持不可见(§119/§133)。
	WolfVoteReasons [MaxPlayers]string
	// WolfVoteTally 是 tallyWolfVotes() 的输出(计票后填充,供 UI 展示)。
	WolfVoteTally *WolfVoteTally

	// §20260809-02 U2 Bot 票型回灌 —— 上一轮白天投票的「谁投了谁」快照。
	// key = voter seat (0-indexed),value = target seat (0-indexed)。
	// fillDayVoteMapLocked 在 finishVoteLocked 末尾抓快照填充,
	// buildAgentContextLocked 在 startDay 时把它拷贝到 GameContext.LastDayVoteMap。
	// 注意:不写入 view.go / ClientGameState(人类玩家已通过聊天流能看到,
	// 不重复下发),仅供 Agent 内部推理。
	LastDayVoteMap map[Seat]Seat

	// 关键神职座位缓存
	WitchSeat Seat
	// §198 骑士座位缓存(与 WitchSeat / GuardSeat 并列)。NoSeat = 本局无骑士。
	KnightSeat Seat
	// §猎魔人 猎魔人机制 — 座位缓存 + 当晚狩猎目标(每晚重置)。NoSeat = 本局无猎魔人。
	DemonHunterSeat       Seat // §猎魔人 猎魔人座位缓存;NoSeat = 本局无猎魔人
	DemonHunterHuntTarget Seat // §猎魔人 当晚狩猎目标;NoSeat = 空过;每晚 startNight 重置
	// §134 守卫机制 — 守卫座位缓存 + 当晚守护目标 + 上晚守护目标(跨夜保留,G1 连守校验依据)。
	// GuardSeat/GuardProtectTarget/GuardSavedTarget/SameGuardSameSave 在 startNight 重置;
	// GuardLastProtect 跨夜保留(必须如此,G1 连守校验依赖)。
	GuardSeat          Seat // §134 守卫座位;NoSeat = 本局无守卫
	GuardProtectTarget Seat // §134 当晚守护目标;NoSeat = 空守
	GuardLastProtect   Seat // §134 上晚守护目标(跨夜保留,G1 连守校验)
	GuardSavedTarget   Seat // §134 护盾单独挡下的目标(给前端/法官反馈)
	SameGuardSameSave  Seat // §134 同守同救牺牲者(WitchSavedTarget == GuardProtectTarget 时仍死)
	// WitchSavedTarget 记录女巫解药救的是谁 —— 与 WolfKillTarget 独立,避免同守同救裁决失败。
	// WitchSavedTarget 在 startNight 重置;NightWitchAct 的 antidote 分支同时写两个字段。
	WitchSavedTarget Seat

	// 猎人信息
	HunterPendingShoot bool   // 猎人死亡后等待开枪
	HunterPendingFrom  string // "wolf" | "vote"(毒杀不开枪)

	// BUG 2026-07-09: 遗言阶段状态。LastWords=true 的出局玩家按座位升序发言;
	// 队列清空后 DeathLyricOnDone 闭包恢复原路径(dawn→start_day, vote→hunter/advanceDay, hunter→advanceDay)。
	DeathLyricQueue   []Seat                // 当前遗言轮等待队列(座位升序)
	DeathLyricDone    map[Seat]bool         // 已发言 / 已跳过的座位
	DeathLyricCurrent Seat                  // 当前应遗言座位;NoSeat = 无
	DeathLyricOnDone  func() *errcode.Error // 队列清空后恢复路径的闭包(仅遗言阶段内非 nil)

	// 2026-07-10: 重开局投票状态(见 docs/狼人杀-Agent与系统/狼人杀重开局投票设计.md §2)。
	// RestartVoteDeadlineAt 用 PhaseDeadlineAt 字段共用,此处只存投票明细。
	RestartVoteYes     map[Seat]bool // seat → 已投 yes
	RestartVoteNo      map[Seat]bool // seat → 已投 no
	RestartVoteAbstain map[Seat]bool // seat → 已投 abstain
	RestartVoteDone    bool          // true → 投票已结算(passed/rejected/timeout)
	RestartVoteResult  string        // "passed" | "rejected" | "timeout" (RestartVoteDone=true 时有效)
	// RestartLastWinner 缓存上一局的 winner,在 PhaseRestartVote / PhaseGameOver 全程对
	// 所有玩家可见(view.go 已通过 Status=="over" 通透暴露, 这里冗余一份方便看门狗日志)。
	RestartLastWinner string
	// RestartCount 累计本房间已重开的次数,manager.forceCloseRoom 时清零。
	RestartCount int
	// FastRestart 为 true 时表示当前投票使用「即刻原班重开」模式:
	// 通过阈值从 2/3 降为简单多数(ceil(N/2)+1)。
	// 由 Action_FastRestartVote 在投票发起或中途标记。
	FastRestart bool

	// 结果
	Winner       string // "wolf" | "good" | ""
	Status       string // "playing" | "over"
	WolfAliveCnt int
	GoodAliveCnt int
	DivineCnt    int // 好人神职数(用于屠神判定)
	PlainCnt     int // 平民数(用于屠民判定)

	// BUG 2026-07-08: 首夜发言缓冲期(首夜 DayNumber=1 专用)。
	// 记录缓冲期截止 unix nano;time.Time{} 表示非首夜/无缓冲。
	// room.watchdog 每秒检查 time.Now().After(FirstNightGraceEnd) →
	// 自动从 PhasePreWolves 切到 PhaseNightWolves。
	FirstNightGraceEnd time.Time

	// 2026-07-09 §13 增强 — 阶段时钟。每个阶段切换时由调用方 SetPhaseDeadline 设置
	// 截止时间(零值 = 无截止);phaseWatchdogTick 在 deadline 到期后立即派发 skip,
	// 不等 90s 兜底。前端 PhaseClock 组件根据本字段渲染倒计时。
	PhaseDeadlineAt time.Time

	// hasHumanSnapshot 是 StartGame / startNight 等"阶段切换"发生时对
	// hasHumanPlayer 的快照;后续 setPhaseAndDeadline 读这个快照而非现场计算,
	// 避免阶段切换后房内有人退出(IsBot=false → 变 true)造成 deadline 漂移。
	// 2026-07-21 §人类玩家操作重构:补齐混合房间所有阶段的"人机 deadline 区分"。
	hasHumanSnapshot bool

	// PreferredRoles 座位级角色偏好(2026-08-06 §20260806-03 自选角色)。
	// key=座位号,value=用户创建房间时指定的角色;由 WerewolfRoom 在
	// StartGame 前从 seatPreferredRoles 同步进来。StartGame 发牌后调用
	// ApplyPreferredRoles 做"牌组内座位置换"(多重集守恒)。nil/空 = 全随机。
	// 不序列化到客户端(身份保密,其他玩家不可见偏好)。
	PreferredRoles map[int]Role

	// PreferredRolesUnmet 座位级角色偏好"未满足"清单(2026-08-11
	// BUG-ROLE-MISMATCH-P0 观测性收口)。StartGame 内 ApplyPreferredRoles
	// 返回的 unmet 直接落到这里;view 层对本人下发"自选未生效"提示。
	// 语义:本局牌组未抽到该角色(如 13 人局随机牌组只有 2-3 张神职,
	// 骑士/守卫/猎魔人常被抽不到),或多座位抢同一限量角色时先到先得,
	// 该座位降级为随机角色。nil/空 = 全部满足或无偏好。
	PreferredRolesUnmet []int

	// BUG Round 40 §95 (2026-07-08): 首夜强制发言阶段(扩展 PhasePreWolves)。
	// 每名存活玩家在 pre_wolves 阶段至少发 PreWolvesSpeakRoundsPerPlayer 轮
	// (默认 1,可配 1-3)。PreWolvesSpeakCount[s] 累计 seat s 已发次数;
	// 全部存活玩家达到 rounds 目标时,phaseWatchdogTick 提前切到 PhaseNightWolves,
	// 不必等满 120s 缓冲期。SpeaksThisRound 在第二轮起清零(每轮重置),
	// 复用 Players[i].HasSpoken 做单轮标记。
	PreWolvesSpeakRoundsPerPlayer int
	PreWolvesSpeakRound           int
	PreWolvesSpeakCount           [MaxPlayers]int

	// RNG
	rng *rand.Rand
	// 调试
	Seed int64

	// 2026-07-11: 预言家发起投票。白天发言阶段预言家可提议结束讨论直接进入投票。
	// VoteProposed=true 时所有玩家可见"预言家已发起投票"横幅。
	VoteProposed bool
	VoteProposer Seat // 发起投票的座位;NoSeat = 未发起

	// QuarantinedSeats 标记当前被禁用(LLM 连续失败 quarantine)的座位。
	// 被禁用的座位视为"无投票能力",allActiveVoted() 不计入它们。该字段由
	// 房间层 syncQuarantinedLocked 在每轮写票操作前从 r.BotAgents[*].IsQuarantined()
	// 同步,引擎自身不维护这一状态(避免 engine→werewolf 循环导入)。
	// BUG-R193-001: 原 allAliveVoted() 仅排除死亡玩家,不排除 quarantined 玩家,
	// 导致 5+ 被禁用 Agent 时投票阶段永远无法 auto-tally,陷入永久死锁。
	QuarantinedSeats [MaxPlayers]bool

	// 2026-07-10 §123 增强 — Agent 法官(主持人)。
	// JudgeEnabled 法官是否启用(读 cfg.Werewolf.JudgeMode);true → phaseWatchdogTick
	// 末尾填 JudgePendingAnnounce 并触发 judgeWake。auto/human 模式 = false。
	JudgeEnabled bool
	// JudgePendingAnnounce 下一次应唤醒法官的事件类型;空字符串 = 无。
	// 取值(对应 docs/狼人杀-重构方案/主持人Agent重构设计.md §6.3 映射表,judge_ 前缀):
	//   "judge_filling_welcome" | "judge_pre_wolves" | "judge_dawn_announce"
	//   "judge_sheriff_start" | "judge_speak_start" | "judge_vote_start"
	//   "judge_death_announce" | "judge_sheriff_stream_settle" | "judge_idiot_reveal"
	//   "judge_hunter_shoot" | "judge_last_words" | "judge_restart_vote_result"
	//   "judge_game_over"
	JudgePendingAnnounce string
	// JudgeSpeakOrder 当 JudgePendingAnnounce="speak_start" 时,填本轮 SpeakOrder。
	// 法官据此生成"按顺序发言:X 号、Y 号、Z 号"。
	JudgeSpeakOrder []Seat
}

// ─────────────────── 工厂 ───────────────────

// NewGame 创建空对局(尚未发牌)。
func NewGame(seed int64) *GameState {
	return &GameState{
		SeatCount:          MaxPlayers, // 默认 13 人标准竞技局;werewolf_12/werewolf_7 由 manager 改写
		PlayerByID:         make(map[string]Seat),
		Roles:              [MaxPlayers]Role{},
		Players:            [MaxPlayers]Player{},
		SheriffSeat:        NoSeat,
		SheriffStreams:     [2]Seat{NoSeat, NoSeat},
		SheriffSuccessor:   NoSeat,
		TurnActingSeat:     NoSeat,
		SpeakTurnSeat:      NoSeat,
		Phase:              PhaseFilling,
		WolfKillTarget:     NoSeat,
		WitchSeat:          NoSeat,
		GuardSeat:          NoSeat,
		KnightSeat:         NoSeat, // §198 骑士座位;NoSeat = 本局无骑士
		DemonHunterSeat:    NoSeat, // §猎魔人 猎魔人座位;NoSeat = 本局无猎魔人
		GuardProtectTarget: NoSeat,
		GuardLastProtect:   NoSeat,
		GuardSavedTarget:   NoSeat,
		SameGuardSameSave:  NoSeat,
		WitchSavedTarget:   NoSeat,
		DayEliminated:      NoSeat,
		SuicidedWolfSeat:   NoSeat,
		VoteProposer:       NoSeat,
		Status:             "playing",
		Winner:             "",
		rng:                rand.New(rand.NewSource(seed)),
		Seed:               seed,
		NightDeaths:        []Seat{},
		LastNightDeaths:    []Seat{},
		DayTiedPlayers:     []Seat{},
		SpeakOrder:         []Seat{},
		RestartVoteYes:     map[Seat]bool{},
		RestartVoteNo:      map[Seat]bool{},
		RestartVoteAbstain: map[Seat]bool{},
	}
}

// SeatOf 返回 userID 所在座位,未入座返回 NoSeat。
func (gs *GameState) SeatOf(userID string) Seat {
	if s, ok := gs.PlayerByID[userID]; ok {
		return s
	}
	return NoSeat
}

// PlayerBySeat 取得座位对应 Player 指针(非法座位返回 nil)。
func (gs *GameState) PlayerBySeat(seat Seat) *Player {
	if seat < 0 || seat >= MaxPlayers {
		return nil
	}
	uid := gs.Seats[seat]
	if uid == "" {
		return nil
	}
	return &gs.Players[seat]
}

// AliveSeat 报告该座位是否有人且存活。
func (gs *GameState) AliveSeat(seat Seat) bool {
	if seat < 0 || seat >= MaxPlayers {
		return false
	}
	return gs.Seats[seat] != "" && gs.Players[seat].Alive
}

// HasActorAt 报告该座位是否有可执行动作的玩家(bot 或真人)。
//
// 与 AliveSeat 的区别:HasActorAt 仅检查 gs.Seats[seat] != ""(有 userID),
// 不要求 Alive=true。用于"用户中途 leave/disconnect 后,Roles[seat] 仍
// 残留但实际无人执行"的边界场景 —— 此时 AliveSeat 可能在 leave 未同步到
// in-memory 时仍返回 true,导致 firstLivingWolf / firstLivingSeer / 等
// 选择器把"空座位"当作 acting seat,引发 night_wolves 阶段 stall(BUG-R212-P1-01)。
//
// 建议在所有"按角色找首个行动座位"的辅助函数里优先使用本函数 + Alive 检查。
func (gs *GameState) HasActorAt(seat Seat) bool {
	if seat < 0 || seat >= MaxPlayers {
		return false
	}
	return gs.Seats[seat] != ""
}

// ─────────────────── 入座与开局 ───────────────────

// AddPlayer 把 userID 加到第一个空座位。返回座位号;房间满返回 ErrRoomFull。
func (gs *GameState) AddPlayer(userID string) (Seat, *errcode.Error) {
	for i := 0; i < MaxPlayers; i++ {
		if gs.Seats[i] == "" {
			gs.Seats[i] = userID
			gs.PlayerByID[userID] = Seat(i)
			gs.Players[i] = Player{
				UserID:        userID,
				Seat:          Seat(i),
				Role:          RoleUnknown,
				Alive:         true,
				LastSeerCheck: NoSeat,
			}
			return Seat(i), nil
		}
	}
	return NoSeat, errcode.Code(errcode.ErrRoomFull)
}

// AddPlayerAt 把 userID 放到指定座位(用于创建房间时预填 agent 座位)。
// 座位已被占或越界返回 ErrRoomFull。
func (gs *GameState) AddPlayerAt(userID string, seat Seat) (Seat, *errcode.Error) {
	if seat < 0 || seat >= MaxPlayers {
		return NoSeat, errcode.Code(errcode.ErrValidationFailed)
	}
	if gs.Seats[seat] != "" {
		return NoSeat, errcode.Code(errcode.ErrRoomFull)
	}
	gs.Seats[seat] = userID
	gs.PlayerByID[userID] = seat
	gs.Players[seat] = Player{
		UserID:        userID,
		Seat:          seat,
		Role:          RoleUnknown,
		Alive:         true,
		LastSeerCheck: NoSeat,
	}
	return seat, nil
}

// RemovePlayer 把 userID 移出房间(异常断开时)。
func (gs *GameState) RemovePlayer(userID string) {
	seat := gs.SeatOf(userID)
	if seat == NoSeat {
		return
	}
	gs.Seats[seat] = ""
	gs.Players[seat] = Player{}
	delete(gs.PlayerByID, userID)
	if gs.SheriffSeat == seat {
		gs.SheriffSeat = NoSeat
	}
}

// Occupied 报告当前已入座人数。
func (gs *GameState) Occupied() int {
	n := 0
	for _, u := range gs.Seats {
		if u != "" {
			n++
		}
	}
	return n
}

// IsReady 报告是否本局人数已满(SeatCount),可开局。
func (gs *GameState) IsReady() bool {
	if gs.SeatCount <= 0 {
		return gs.Occupied() == MaxPlayers
	}
	return gs.Occupied() == gs.SeatCount
}

// StartGame 满本局人数(SeatCount,默认13 / werewolf_12=12 / werewolf_7=7)时调用:分配角色+首夜缓冲期(PhasePreWolves)。
// 发牌按 SeatCount 分支:7人=StandardDeck;12人=StandardDeck12;13人=StandardDeck13。
func (gs *GameState) StartGame() *errcode.Error {
	if !gs.IsReady() {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "need all players seated")
	}
	if gs.Status != "playing" {
		return errcode.CodeMsg(errcode.ErrGameAlreadyOver, "game already over")
	}
	if gs.Phase != PhaseFilling {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "game already started")
	}
	// 1. 分配角色(按 SeatCount 选牌组)
	switch gs.SeatCount {
	case 7:
		gs.Roles = AssignRoles(gs.Seed, gs.Seats)
	case 12:
		gs.Roles = AssignRoles12(gs.Seed, gs.Seats)
	default:
		gs.Roles = AssignRoles13Random(gs.Seed, gs.Seats)
	}
	// 2026-08-06 §20260806-03 自选角色注入:发牌(含 AssignRoles13Random 的
	// "尾位强制平民"软约定)完成后,按座位偏好做牌组内座位置换。多重集守恒;
	// 未满足的偏好(牌组无此角色/被抢占)降级为随机并记日志。
	// 2026-08-11 BUG-ROLE-MISMATCH-P0:unmet 同时落到 GameState 供 view 层
	// 对本人下发「自选角色未生效」提示(此前 unmet 仅记 Warn 日志,fail-quiet,
	// 玩家实测「选猎人发猎魔人」全程无任何可感知信号)。
	if len(gs.PreferredRoles) > 0 {
		var occupied [MaxPlayers]bool
		for i, u := range gs.Seats {
			occupied[i] = u != ""
		}
		if unmet := ApplyPreferredRoles(&gs.Roles, occupied, gs.PreferredRoles); len(unmet) > 0 {
			gs.PreferredRolesUnmet = unmet
			logger.L().Warn("werewolf: preferred role unmet (deck has no such card), fallback to random",
				zap.Ints("seats", unmet),
				zap.Int64("seed", gs.Seed))
		} else {
			gs.PreferredRolesUnmet = nil
		}
	} else {
		gs.PreferredRolesUnmet = nil
	}
	// 2026-07-21 §人类玩家操作重构:缓存 hasHumanPlayer 快照,
	// 让所有阶段切换的 deadline 都用一致的人机判断,避免混合房间使用
	// 全 AI deadline 让真人等 240s+。
	gs.hasHumanSnapshot = hasHumanPlayer(gs)
	for i, r := range gs.Roles { // 写入角色,仅入座座位存活
		gs.Players[i].Role = r
		gs.Players[i].LastSeerCheck = NoSeat
		gs.Players[i].SeerCheckHistory = nil // §20260810-04 U3: 每局重置查验历史
		if gs.Seats[i] != "" {
			gs.Players[i].Alive = true
		}
	}
	for i, r := range gs.Roles {
		if r == RoleWitch {
			gs.WitchSeat = Seat(i)
		}
		if r == RoleGuard {
			// §134 守卫座位缓存(与 WitchSeat 同一循环)
			gs.GuardSeat = Seat(i)
		}
		if r == RoleKnight {
			// §198 骑士座位缓存(同循环内一次填,守 5 个神职均覆盖)
			gs.KnightSeat = Seat(i)
		}
		if r == RoleDemonHunter {
			// §猎魔人 猎魔人座位缓存(同循环内一次填)
			gs.DemonHunterSeat = Seat(i)
		}
	}
	gs.DayNumber = 1 // 进入第一夜+首夜缓冲期
	gs.startNight()
	gs.FirstNightGraceEnd = time.Now().Add(defaultFirstNightGrace())
	// 5. 缓冲期阶段:任何狼人/预言家/女巫工具被 BuildTools 屏蔽
	setPhaseAndDeadline(gs, PhasePreWolves, hasHumanPlayer(gs))
	gs.TurnActingSeat = NoSeat
	// 6. BUG Round 40 §95: 首夜强制发言计数初始化(读 config,clamp 1-3)。
	gs.PreWolvesSpeakRoundsPerPlayer = getForcedSpeakRounds()
	gs.PreWolvesSpeakRound = 0
	for i := range gs.PreWolvesSpeakCount {
		gs.PreWolvesSpeakCount[i] = 0
	}
	return nil
}

// defaultFirstNightGrace 默认首夜发言缓冲期;从 LsmAgentGame.conf.werewolf.first_night_grace_sec 读取,
// 兜底 120 秒(Round 40 §95 新增配置可调,留作兼容)。
func defaultFirstNightGrace() time.Duration {
	// 避免 import cycle(config→... 不可被 werewolf 反向 import),延迟初始化一次。
	firstNightGraceOnce.Do(func() {
		firstNightGraceDuration = time.Duration(cfgWerewolfGraceSec()) * time.Second
		if firstNightGraceDuration <= 0 {
			firstNightGraceDuration = 120 * time.Second
		}
	})
	return firstNightGraceDuration
}

var (
	firstNightGraceOnce     sync.Once
	firstNightGraceDuration time.Duration
)

// getForcedSpeakRounds 读 config 的首夜强制发言轮次,clamp 到 [1,3]。
// 0 / 负值 / >3 全部兜底为 1,防止配置错误导致强制发言关掉。
// BUG Round 40 §95.
func getForcedSpeakRounds() int {
	n := cfgWerewolfForcedRounds()
	if n < 1 {
		return 1
	}
	if n > 3 {
		return 3
	}
	return n
}

// startNight 启动一个新夜晚:重置当晚临时变量 + 进入守卫 / 狼人阶段。
// §134: 新增守卫阶段(PhaseNightGuard)在狼刀之前;无守卫 / 守卫已死时跳过该阶段,
// 直接进入 PhaseNightWolves(与 endSeerPhase 处理无女巫的方式一致)。

// firstLivingWolf 返回第一个存活且为狼的座位;全部死亡返回 NoSeat。
//
// BUG-R212-P1-01(2026-07-31):增加 HasActorAt 守卫,跳过 Roles[i] 残留但
// gs.Seats[i]=="" 的"幽灵座位"(典型场景:真人 seat 0 创建房间后 leave,
// Roles[0] 仍 = RoleWerewolf,但 r.Seats[0] 因 leave 未同步到 in-memory
// 时仍占 userID,旧逻辑仍选为 acting → night_wolves stall)。
func firstLivingWolf(gs *GameState) Seat {
	for i := 0; i < MaxPlayers; i++ {
		if gs.HasActorAt(Seat(i)) && gs.AliveSeat(Seat(i)) && gs.Roles[i] == RoleWerewolf {
			return Seat(i)
		}
	}
	return NoSeat
}

// firstLivingSeer 返回第一个存活的预言家座位。
// BUG-R212-P1-01:同上,加 HasActorAt 守卫。
func firstLivingSeer(gs *GameState) Seat {
	for i := 0; i < MaxPlayers; i++ {
		if gs.HasActorAt(Seat(i)) && gs.AliveSeat(Seat(i)) && gs.Roles[i] == RoleSeer {
			return Seat(i)
		}
	}
	return NoSeat
}

// firstLivingHunter 返回第一个存活的猎人座位。
// BUG-R212-P1-01:同上,加 HasActorAt 守卫。
func firstLivingHunter(gs *GameState) Seat {
	for i := 0; i < MaxPlayers; i++ {
		if gs.HasActorAt(Seat(i)) && gs.AliveSeat(Seat(i)) && gs.Roles[i] == RoleHunter {
			return Seat(i)
		}
	}
	return NoSeat
}

// firstLivingDemonHunter §猎魔人 返回第一个存活的猎魔人座位;全部死亡或本局无猎魔人返回 NoSeat。
// 用于 PhaseNightDemonHunter 阶段的 TurnActingSeat 注入。
// BUG-R212-P1-01:同上,加 HasActorAt 守卫。
func firstLivingDemonHunter(gs *GameState) Seat {
	for i := 0; i < MaxPlayers; i++ {
		if gs.HasActorAt(Seat(i)) && gs.AliveSeat(Seat(i)) && gs.Roles[i] == RoleDemonHunter {
			return Seat(i)
		}
	}
	return NoSeat
}

// hunterSeatForPhaseLocked 返回当前 HunterPendingShoot 关联的猎人座位。
// BUG-R10-P0-3 (2026-07-29):用于进入 PhaseHunterShoot 时把 TurnActingSeat
// 设为猎人座位(替代原 NoSeat)。单一查找入口,避免四处 setPhaseAndDeadline
// 分叉。与 firstLivingHunter 的区别:猎人此时已**死亡**(夜间被狼刀 /
// 白天被放逐),不应用 AliveSeat 守卫。
func hunterSeatForPhaseLocked(gs *GameState) Seat {
	if !gs.HunterPendingShoot {
		return NoSeat
	}
	for i := 0; i < MaxPlayers; i++ {
		if gs.Roles[i] == RoleHunter {
			return Seat(i)
		}
	}
	return NoSeat
}

// refreshCounts 重新统计存活数与神职 / 平民分类数(胜负判定用)。
func (gs *GameState) refreshCounts() {
	gs.WolfAliveCnt = 0
	gs.GoodAliveCnt = 0
	gs.DivineCnt = 0
	gs.PlainCnt = 0
	for i := 0; i < MaxPlayers; i++ {
		if !gs.AliveSeat(Seat(i)) {
			continue
		}
		r := gs.Roles[i]
		switch {
		case r == RoleWerewolf:
			gs.WolfAliveCnt++
		case IsGodRole(r):
			gs.DivineCnt++
			gs.GoodAliveCnt++
		case r == RoleVillager:
			gs.PlainCnt++
			gs.GoodAliveCnt++
		}
	}
}

// ─────────────────── 胜负判定 ───────────────────

// checkWinner 在每次状态变化后调用,胜则填 Winner+Status+Phase。
func (gs *GameState) checkWinner() bool {
	if gs.Status == "over" {
		return true
	}
	gs.refreshCounts()
	if gs.DivineCnt == 0 || gs.PlainCnt == 0 {
		gs.Winner = "wolf"
		gs.Status = "over"
		setPhaseAndDeadline(gs, PhaseGameOver)
		return true
	}
	if gs.WolfAliveCnt == 0 {
		gs.Winner = "good"
		gs.Status = "over"
		setPhaseAndDeadline(gs, PhaseGameOver)
		return true
	}
	return false
}

// ─────────────────── 死亡 / 遗言 ───────────────────

// killPlayer 标记座位为死亡;按 cause 决定遗言权(cause: wolf/vote/witch_poison/hunter/suicide/duel/demon_hunter_misjudge)。
// v4 §13.1：狼人死亡时的 WolfPackRoom 清理在 EmitPlayerDied 路径完成（持锁调 PurgeByDeath）。
// §20260811-10 U3：狼人因自爆 / 白天投票放逐死亡时,从阵营金币池扣 30 金。
// 调用方必须已持 r.mu(§92a)。
func (gs *GameState) killPlayer(seat Seat, cause string) *errcode.Error {
	if seat < 0 || seat >= MaxPlayers {
		return errcode.Code(errcode.ErrValidationFailed)
	}
	if !gs.Players[seat].Alive {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "already dead")
	}
	gs.Players[seat].Alive = false
	// 计算遗言权(LastWordsRounds 内)
	allowLW := false
	switch cause {
	case "wolf", "vote", "hunter":
		allowLW = gs.DayNumber <= LastWordsRounds
	case "witch_poison", "suicide", "duel", "demon_hunter_misjudge":
		// §198 duel 自决无遗言;§猎魔人 misjudge 自决也无遗言(自爆语义一致)
		allowLW = false
	}
	gs.Players[seat].LastWords = allowLW
	// 2026-07-10 §123: 写入死因 + 决断(vote/hunter/poison 用常量保留向后兼容)。
	gs.Players[seat].DeathCause = cause
	gs.Players[seat].DeathVerdict = verdictFor(cause)
	// §20260811-10 U3 — 悍跳失败赔偿。狼阵营金币池扣 30,重入保护由
	// ApplyWolfPoolPenaltyLocked 内部 WolfPoolPenaltyApplied[seat] 处理。
	gs.ApplyWolfPoolPenaltyLocked(seat, cause)
	if gs.SheriffSeat == seat {
		gs.SheriffSeat = NoSeat
		// 夜间警长死亡(狼刀/毒)→天亮结算警徽流;白天放逐有遗言,口头指定,不走结算。
		switch cause {
		case "wolf", "witch_poison":
			gs.sheriffSlain = seat
		}
	}
	return nil
}

// ─────────────────── 夜晚行动 ───────────────────

// NightWolfKill 狼人投票选择当晚击杀目标。
// 2026-07-17 改造: 从"单狼决定"改为"多狼投票"。
// target=NoSeat 表示弃权。全部存活狼人投票完毕后自动计票并进入预言家阶段。
//
// 2026-07-24 R196 修复: 拒绝重复投票。R196 测试发现 Bot 8 (GLM-5.2) 在
// night_wolves 阶段反复调用 wolf_kill 15+ 次,服务端仅覆盖 WolfVotes[actor]
// 不报错,LLM 看不到任何反馈信号而陷入循环。修复后服务端以
// ErrAlreadyWolfVoted 显式拒绝,LLM 收到 error 后自然收敛。

// allWolvesVoted 返回是否所有存活狼人均已提交投票(含弃权)。

// tallyWolfVotes 统计狼人投票,决定最终击杀目标。
// 全弃权 → 随机选择;平票 → 从并列中随机选择;多数 → 该目标。
// 前置条件: Phase == PhaseNightWolves。

// randomAliveNonWolf 从存活非狼人玩家中随机选一个;无候选返回 NoSeat。

// endWolfPhase 关闭狼人投票阶段,进入预言家阶段。
// 抽取为独立方法,供 tallyWolfVotes 后统一调用(替代原 NightWolfKill 末尾的推进逻辑)。

// NightSeerCheck 预言家查验某玩家的阵营。

// endSeerPhase 关闭预言家阶段,进入女巫阶段(无女巫则直接到天亮)。

// NightWitchAct 女巫用药决策。
// action: "none" / "antidote" / "poison";target 仅 poison 时需要。

// NightGuardProtect §134 守卫守护决策。校验链见设计文档 §3.3:
//
//	G1 连守:target != GuardLastProtect
//	G2 不能守自己:target != actor
//	G3 只能守存活:AliveSeat(target)
//	G4 空守合法:target == NoSeat

// endGuardPhase §134 关闭守卫阶段 → 进入狼人阶段(与 endSeerPhase 同构)。
// 若无存活狼人,直接调 endWolfPhase 走完整链路(实际无狼则再走 endSeerPhase → endWitchPhase)。

// endWitchPhase 关闭女巫阶段:护盾 / 同守同救裁决 + 把 WolfKillTarget 写入 NightDeaths(未被救),
// 然后跳到天亮阶段(公布死亡)。
// §134 守卫护盾 / 同守同救裁决:两个分支必须**互斥**(写成独立 if 会导致同守同救被
// 抵消 —— 第一条 if 把 WolfKillTarget 复活为 WitchSavedTarget,紧接着护盾分支又
// 因 GuardProtectTarget == WolfKillTarget 清成 NoSeat,净效果变回存活,同守同救规则
// 被完全抵消)。因此用 switch-case 强制互斥。
// BUG 2026-07-09: 公布死亡后,若 LastNightDeaths 中存在 LastWords=true 的座位,
// 进入遗言阶段(death_lyric);队列清空后调 StartDay 恢复白天流程。

// appendUniqueSeat 不重复添加。

// ─────────────────── 警长 / 发言 / 投票 ───────────────────

// StartDay 启动白天流程:第一天才有警长竞选,否则直接进入发言。

// startSpeakPhase 启动白天发言顺序(简化:座位号升序)。

// NextSpeaker 推进到下一位应发言玩家;返回 NoSeat 表示本轮发言结束,进入投票。

// FinishSpeak 当前应发言座位"结束发言",推进。

// ProposeVote 预言家在白天发言阶段发起投票,直接结束讨论进入投票阶段。
// 前置条件:PhaseSpeak + actor 存活 + actor 角色为预言家。
// 2026-07-11: 预言家亮明身份后可主动发起投票,不必等所有人发言完毕。

// DayVote 投票(白天 / 警长竞选共用)。

// allAliveVoted reports whether every alive seat has cast a vote this round.
// Used by the agent driver prompt (so the host driver knows when to call
// finish_vote).
//
// WARNING: this counts ALL alive seats — including quarantined bots that can
// never vote. Use allActiveVoted() for the auto-tally gate so that rooms with
// multiple quarantined bots do not deadlock at PhaseVote (BUG-R193-001).

// allActiveVoted reports whether every alive, non-quarantined seat has cast a
// vote this round. Quarantined bots (gs.QuarantinedSeats[i]==true) are excluded
// because their LLM is broken and they can never set Voted=true — waiting for
// them would stall PhaseVote forever. Used by DayVote's auto-tally and by
// dayVoteLocked's driver-tally branch (BUG-R193-001).

// TallyVotes 统计投票结果(警长票权 1.5,简化用 3:2 比值;
// DayVote:3 vs 2 等价于 1.5 vs 1)。
// sheriffMode=true 时不应用警长票权。

// FinishVote 完成投票:填 DayEliminated;平票 → DayTiedPlayers + 辩护;二次平票 → 无人出局。

// advanceDay:投票结束(无猎人开枪)→ 进下一夜。

// ─────────────────── 自爆 / 猎人 ───────────────────

// WolfSuicide 狼人在白天发言阶段自爆。

// HunterShoot 猎人开枪;target=NoSeat 表示不开枪。
// BUG 2026-07-09: hunter 杀人后,若被杀者 LastWords=true,进入遗言阶段,队列清空后再 advanceDay。

// ─────────────────── 白痴翻牌 / 警徽流 ───────────────────

// IdiotReveal 白痴在 PhaseIdiotReveal 阶段选择翻牌(reveal)/放弃(skip)。
// reveal:设 IdiotRevealed=true,当天无人出局,直接进入黑夜;skip:正常放逐。

// SheriffStreamDeclare 预言家警长声明 / 撤回警徽流(slot 1|2,target=-1 撤回)。

// ─────────────────── 警长竞选 ───────────────────

// SheriffCandidate 玩家举手参选警长(只在 PhaseSheriff 第一轮)。

// SheriffElect 警长选举结果:计算票数 + 应用同规则(单票、无警长票权)。

// ─────────────────── 公开工具 ───────────────────

// LastNightDeathsCopy 返回昨晚死的人(拷贝以防外部修改)。

// idiotRevealedSeats 返回已翻牌的白痴座位列表(全场公开信息)。

// Snapshot 序列化 GS 主要状态字段,用于日志/debug。

// ─────────────────── config 桥接(Round 40 §95)───────────────────
// 4 个 helper 把 config.WerewolfConfig 字段延迟到 defaultFirstNightGrace /
// getForcedSpeakRounds 真正被调用时再读,避免在 init 期 / 测试 stub 下
// 触发 config.Load 副作用(若 config 包 import cycle 直接 panic 也会被
// 延迟到首次使用才暴露)。

// cfgWerewolfGraceSec 读 werewolf.first_night_grace_sec;失败兜底 120。

// cfgWerewolfLLMCallMinInterval 已被 §130 重构删除 (2026-07-13):
// 每个 bot 现在按模型自身响应速率自由调用 LLM,不再有最小调用间隔。
// 函数保留为空以保留文档历史,实际已无调用方。

// SetPhaseDeadline 设置 GameState.PhaseDeadlineAt 为当前时间 + secs。
// phase 为空时不修改;secs ≤ 0 时清空(零值 = 无截止)。
//
// 2026-07-09 §13 增强:每个阶段切换时由调用方调用,让 watchdog 在 deadline
// 到期后立即派发 skip(不等 90s 兜底),同时给前端 PhaseClock 组件提供
// 绝对截止时间渲染倒计时。

// cfgAgentLLMCallTimeoutSec 计算 agent 层单次 LLM 调用总超时(含 lenient 缩放)。
// 与 agent.cfgLLMCallTimeoutSec 逻辑保持一致,避免 engine 包 import agent 形成循环。
// 2026-07-24 优化: 默认 120 → 300s,缩放上限 300 → 480s(lenient ×150%=450s
// 必须在 cap 内生效;< llm.timeout_ms=600s 预算)。

// cfgPhaseDeadlineSec 安全读取 config.PhaseDeadlineSec(phase)。
// 在测试或 bootstrap 早期 config.Load() 可能 panic(nil cfg / 缺 LsmAgentGame.conf);
// 此时使用 built-in defaultPhaseDeadlineSec 表兜底,与 Config.PhaseDeadlineSec
// 兜底表保持一致,保证 setPhaseAndDeadline 在任何上下文都能挂上正确 deadline。
//
// §127 人机区分:当房间存在真人玩家(isHuman=true)时,acting phase 的 deadline
// 缩短为更合理的值,避免人类等待过久;全 AI 房间保持较长 deadline 让慢模型完成。
//
// 2026-07-15 R131 增强: acting phase floor 按 seatCount 缩放,13 人局给足等待。

// hasHumanPlayer 检查 GameState 中是否有真人玩家入座。
// §127: 用于 setPhaseAndDeadline 的人机 deadline 区分。

// isActingPhase reports whether a phase requires LLM calls (i.e. bots must
// think/act). Non-acting phases (dawn) are pure transitions with no LLM.

// defaultPhaseDeadlineSec 与 Config.PhaseDeadlineSec 兜底表镜像;用于
// config.Load() 不可用(测试/早期 bootstrap)时的回退。
// §127 人机区分:human=true 时 acting phases 缩短到 150s,让真人玩家不必久等。
// §2026-07-12 LLM 超时=5min(300s)背景下,全 AI acting phases 给到 360~480s,
// 既覆盖单次 LLM 调用 + 重试 + 并发排队,又给 watchdog 留 30s 留观窗口。

// setPhaseAndDeadline 切换 GameState.Phase 并立即挂上 PhaseDeadlineAt。
// BUG-R70-P1: 之前只定义未被调用;现所有 `gs.Phase = PhaseX` 改用本函数。
// §127: 传递 isHuman 参数实现人机 deadline 区分。
// 2026-07-21 §人类玩家操作重构:若调用方未传 isHuman 且 gs.hasHumanSnapshot 已
// 缓存,使用缓存;否则按 hasHumanPlayer(gs) 现场计算。全 AI 房间保持长 deadline。

// cfgWerewolfMinSpeaks 安全读取 config.WerewolfConfig.MinSpeaksPerMinute。
// 默认 2;0 表示禁用强制下限。

// cfgWerewolfSpectatorFullWake 安全读取 config.WerewolfConfig.SpectatorFullWake。
// 默认 true;false 时回退 15s 节流 + 白名单(旧行为)。

// cfgWerewolfChatHistoryBytes 安全读取 config.WerewolfConfig.ChatHistoryBytes。
// 默认 500K (500*1024);0 表示用默认 500K 兜底。

// ─────────────────── 遗言 (Last Words) —— 2026-07-09 新增 ───────────────────

// DeathLyricDeadlineSeconds 返回遗言阶段的单座位截止秒数(≥5)。
// 优先使用 config.WerewolfConfig.DeathLyricFallback;否则 DeathLyricDefaultDeadlineSec。

// isDeathLyricEnabled 安全读取 config.WerewolfConfig.DeathLyricEnabled。
// 默认 true(包括 config 未加载的测试环境);仅当 operator 显式设 false 时回退
// 旧行为(dawn 直接 → start_day, 无遗言阶段)。

// filterLastWords 从 seats 中筛出 LastWords=true 的座位,按升序返回。
// 入参中已死但未标记 LastWords 的座位(毒杀/自爆/Day≥3 出局)被排除。
// gs 为对局权威状态,读取 Players[s].LastWords 判定是否仍有遗言权。

// StartDeathLyricRound 进入遗言阶段。
//   - seats:候选座位(函数内部 filterLastWords;若结果为空则返回 ErrDeathLyricSkip 哨兵)。
//   - onDone:队列清空后调用的恢复闭包。
//
// 前置:Status != "over"。

// tryEnterDeathLyricRound 是 StartDeathLyricRound 的哨兵识别包装。
// 哨兵 ErrDeathLyricSkip 出现时,直接调 onDone() 恢复原路径(不进遗言阶段)。

// SayLastWords 当前遗言座位提交遗言。

// SkipLastWords 当前遗言座位放弃遗言。

// popDeathLyricQueue 推进队列;队列空则调 EndDeathLyricRound。

// EndDeathLyricRound 清空遗言状态并调 onDone 恢复原路径。
