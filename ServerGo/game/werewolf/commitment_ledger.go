package werewolf

// commitment_ledger.go — 行为承诺与兑现追踪（§20260810-06）。
//
// 玩家（含 Agent 和人类）在白天发言阶段公开做出可验证的行为承诺，
// 服务端追踪兑现情况并在结算时展示「承诺兑现率排行榜」。
//
// 硬约束对照：
//   - §92a 锁内变体：所有方法均为 *Locked 语义，调用方必须已持有 r.mu。
//   - §119 协议层隔离：承诺兑现状态对其他玩家保密（仅本人+观战者可见真实状态）。
//   - §135 身份公开公平性：seer_check 等模板的兑现判定不能暴露预言家身份。

import (
	"fmt"
	"time"
)

// CommitTemplate 承诺模板类型（封闭枚举，禁止自由字符串）。
type CommitTemplate string

const (
	// 「如果我是预言家，我今晚验 N 号」
	CommitSeerCheck CommitTemplate = "seer_check"
	// 「如果 N 号是狼，我明天投票放逐他」
	CommitVoteTarget CommitTemplate = "vote_target"
	// 「本轮我不会投票给 N 号」
	CommitNoVoteFor CommitTemplate = "no_vote_for"
	// 「我今晚不会使用技能」
	CommitNoUseSkill CommitTemplate = "no_use_skill"
	// 「N 号如果是好人，我公开道歉」
	CommitApologyIfGood CommitTemplate = "apology_if_good"
)

// CommitStatus 承诺状态。
type CommitStatus string

const (
	CommitStatusPending   CommitStatus = "pending"   // 等待验证
	CommitStatusFulfilled CommitStatus = "fulfilled" // 已兑现
	CommitStatusBroken    CommitStatus = "broken"    // 已违背
	CommitStatusExpired   CommitStatus = "expired"   // 条件不成立，过期无效
)

// Commitment 一条承诺记录。
type Commitment struct {
	ID         int64          `json:"id"`
	Seat       int            `json:"seat"`         // 承诺者座位（0-indexed）
	Round      int            `json:"round"`        // 承诺发生的天数
	Template   CommitTemplate `json:"template"`
	ParamSeat  int            `json:"param_seat"`   // 参数：目标座位号（-1 = 无）
	ParamText  string         `json:"param_text"`   // 参数：补充文本（≤30 字）
	Reason     string         `json:"reason"`       // 公开理由（≤30 字）
	Status     CommitStatus   `json:"status"`
	VerifiedAt int64          `json:"verified_at,omitempty"` // 验证时间（UnixMilli）
	CreatedAt  int64          `json:"created_at"`
}

// CommitmentLedger 房间级承诺账本。
// 所有方法均为 *Locked 语义：调用方必须已持有 r.mu（§92a），本结构自身不再加锁。
type CommitmentLedger struct {
	nextID int64
	items  []*Commitment
}

// NewCommitmentLedger 创建空账本。
func NewCommitmentLedger() *CommitmentLedger {
	return &CommitmentLedger{
		nextID: 1,
		items:  make([]*Commitment, 0, 32),
	}
}

// AddCommitmentLocked 添加一条承诺（§92a 锁内语义）。
// paramSeat 为 -1 表示无目标座位（如 no_use_skill）。
// 每人每天最多 maxCommitsPerDay 条（默认 3）。
func (cl *CommitmentLedger) AddCommitmentLocked(seat, round int, template CommitTemplate, paramSeat int, paramText, reason string, maxPerDay int) (*Commitment, error) {
	if maxPerDay <= 0 {
		maxPerDay = 3
	}
	// 检查当日已承诺数量
	count := 0
	for _, c := range cl.items {
		if c.Seat == seat && c.Round == round {
			count++
		}
	}
	if count >= maxPerDay {
		return nil, fmt.Errorf("每日最多承诺 %d 次", maxPerDay)
	}

	// 校验模板合法性
	if !isValidCommitTemplate(template) {
		return nil, fmt.Errorf("无效的承诺模板: %s", template)
	}

	// 校验 paramSeat
	if template != CommitNoUseSkill && (paramSeat < 0 || paramSeat >= MaxPlayers) {
		return nil, fmt.Errorf("承诺必须指定有效目标座位")
	}

	// 截断文本
	if len(paramText) > 30 {
		paramText = paramText[:30]
	}
	if len(reason) > 30 {
		reason = reason[:30]
	}

	c := &Commitment{
		ID:        cl.nextID,
		Seat:      seat,
		Round:     round,
		Template:  template,
		ParamSeat: paramSeat,
		ParamText: paramText,
		Reason:    reason,
		Status:    CommitStatusPending,
		CreatedAt: time.Now().UnixMilli(),
	}
	cl.nextID++
	cl.items = append(cl.items, c)
	return c, nil
}

// isValidCommitTemplate 校验模板是否为合法枚举值。
func isValidCommitTemplate(t CommitTemplate) bool {
	switch t {
	case CommitSeerCheck, CommitVoteTarget, CommitNoVoteFor, CommitNoUseSkill, CommitApologyIfGood:
		return true
	}
	return false
}

// CommitFacts 评估时的事实输入。
type CommitFacts struct {
	SeerSeat         int              // 真预言家座位（-1 = 无/未知）
	SeerCheckTarget  int              // 昨夜查验目标
	DayVoteMap       map[int]int      // seat -> target（本日白天投票）
	PlayerRoles      map[int]Role     // 座位→角色（终局才可用）
	PlayerFactions   map[int]Faction  // 座位→阵营（终局才可用）
	SkillUsedTonight map[int]bool     // 今夜是否使用了技能
	CurrentDay       int              // 当前天数
	IsGameOver       bool             // 是否终局
}

// EvaluateForTriggerLocked 批量评估特定触发点的所有 pending 承诺（§92a 锁内语义）。
// 返回状态发生变化的承诺列表。
func (cl *CommitmentLedger) EvaluateForTriggerLocked(trigger CommitTemplate, facts CommitFacts) []*Commitment {
	changed := make([]*Commitment, 0, 4)
	now := time.Now().UnixMilli()

	for _, c := range cl.items {
		if c.Status != CommitStatusPending {
			continue
		}
		if c.Template != trigger {
			continue
		}

		newStatus := evaluateOne(c, facts)
		if newStatus != CommitStatusPending {
			c.Status = newStatus
			c.VerifiedAt = now
			changed = append(changed, c)
		}
	}
	return changed
}

// evaluateOne 评估单条承诺（纯函数，可单元测试）。
func evaluateOne(c *Commitment, facts CommitFacts) CommitStatus {
	switch c.Template {
	case CommitSeerCheck:
		return evaluateSeerCheck(c, facts)
	case CommitVoteTarget:
		return evaluateVoteTarget(c, facts)
	case CommitNoVoteFor:
		return evaluateNoVoteFor(c, facts)
	case CommitNoUseSkill:
		return evaluateNoUseSkill(c, facts)
	case CommitApologyIfGood:
		return evaluateApologyIfGood(c, facts)
	}
	return CommitStatusPending
}

// evaluateSeerCheck 评估预言家查验承诺。
// 规则：承诺者是真预言家 ∧ 实际查验目标 = ParamSeat → fulfilled
//      承诺者是真预言家 ∧ 实际查验目标 ≠ ParamSeat → broken
//      承诺者不是预言家 → expired
func evaluateSeerCheck(c *Commitment, facts CommitFacts) CommitStatus {
	if facts.SeerSeat < 0 {
		return CommitStatusPending // 无法判定
	}
	if c.Seat != facts.SeerSeat {
		return CommitStatusExpired // 承诺者不是预言家
	}
	if facts.SeerCheckTarget == c.ParamSeat {
		return CommitStatusFulfilled
	}
	return CommitStatusBroken
}

// evaluateVoteTarget 评估投票目标承诺。
// 规则：承诺者投了 ParamSeat ∧ ParamSeat 是狼人 → fulfilled
//      承诺者投了 ParamSeat ∧ ParamSeat 是好人 → broken
//      承诺者未投 ParamSeat → 保持 pending（等下一天）
func evaluateVoteTarget(c *Commitment, facts CommitFacts) CommitStatus {
	target, voted := facts.DayVoteMap[c.Seat]
	if !voted || target != c.ParamSeat {
		return CommitStatusPending // 承诺者没投目标，等待下一天
	}
	// 需要 PlayerFactions 来判断目标是狼还是好人
	if facts.PlayerFactions == nil {
		return CommitStatusPending
	}
	faction, ok := facts.PlayerFactions[c.ParamSeat]
	if !ok {
		return CommitStatusPending
	}
	if faction == FactionWolf {
		return CommitStatusFulfilled
	}
	return CommitStatusBroken
}

// evaluateNoVoteFor 评估不投票承诺。
// 规则：承诺者未投 ParamSeat → fulfilled；投了 → broken
func evaluateNoVoteFor(c *Commitment, facts CommitFacts) CommitStatus {
	target, voted := facts.DayVoteMap[c.Seat]
	if !voted {
		return CommitStatusPending // 承诺者还没投票
	}
	if target == c.ParamSeat {
		return CommitStatusBroken
	}
	return CommitStatusFulfilled
}

// evaluateNoUseSkill 评估不使用技能承诺。
// 规则：承诺者当夜未使用技能 → fulfilled；使用了 → broken
func evaluateNoUseSkill(c *Commitment, facts CommitFacts) CommitStatus {
	used, ok := facts.SkillUsedTonight[c.Seat]
	if !ok {
		return CommitStatusPending
	}
	if used {
		return CommitStatusBroken
	}
	return CommitStatusFulfilled
}

// evaluateApologyIfGood 评估赛后道歉承诺。
// 规则：终局时，ParamSeat 是好人 → fulfilled（前提：承诺者公开道歉，此处简化为好人即兑现）
//      ParamSeat 是狼人 → expired（条件不成立）
func evaluateApologyIfGood(c *Commitment, facts CommitFacts) CommitStatus {
	if !facts.IsGameOver {
		return CommitStatusPending
	}
	if facts.PlayerFactions == nil {
		return CommitStatusPending
	}
	faction, ok := facts.PlayerFactions[c.ParamSeat]
	if !ok {
		return CommitStatusExpired
	}
	if faction == FactionGood {
		return CommitStatusFulfilled
	}
	return CommitStatusExpired
}

// GetCommitmentsForViewerLocked 返回按视角脱敏后的承诺列表（§92a 锁内语义）。
// viewerSeat == -1 表示观战者（可见全部状态）。
// viewerSeat == -2 表示"他人 pending 视图"（仅返回所有 pending 承诺，隐藏真实状态）。
// 其他玩家只能看到 pending 状态的承诺 + 自己的承诺（含真实状态）。
func (cl *CommitmentLedger) GetCommitmentsForViewerLocked(viewerSeat int) []*Commitment {
	if viewerSeat == -1 {
		// 观战者：全部可见
		return cl.items
	}
	if viewerSeat == -2 {
		// 他人 pending 视图：仅 pending 承诺
		out := make([]*Commitment, 0, len(cl.items))
		for _, c := range cl.items {
			if c.Status == CommitStatusPending {
				out = append(out, c)
			}
		}
		return out
	}
	out := make([]*Commitment, 0, len(cl.items))
	for _, c := range cl.items {
		if c.Seat == viewerSeat {
			// 自己的承诺：全量可见
			out = append(out, c)
		} else if c.Status == CommitStatusPending {
			// 他人的 pending 承诺：可见（隐藏真实状态）
			masked := *c
			masked.Status = CommitStatusPending
			out = append(out, &masked)
		}
		// 他人的非 pending 承诺：终局前不可见（§135）
	}
	return out
}

// GetAllLocked 返回全部承诺（终局结算用，含真实状态）。
func (cl *CommitmentLedger) GetAllLocked() []*Commitment {
	return cl.items
}

// GetFulfillmentRateLocked 计算指定座位的兑现率（§92a 锁内语义）。
// 返回 (兑现数, 违背数, 总数, 兑现率 0.0-1.0)。
// 兑现率 = fulfilled / (fulfilled + broken)；无承诺时返回 0。
func (cl *CommitmentLedger) GetFulfillmentRateLocked(seat int) (fulfilled, broken, total int, rate float64) {
	for _, c := range cl.items {
		if c.Seat != seat {
			continue
		}
		total++
		switch c.Status {
		case CommitStatusFulfilled:
			fulfilled++
		case CommitStatusBroken:
			broken++
		}
	}
	denominator := fulfilled + broken
	if denominator > 0 {
		rate = float64(fulfilled) / float64(denominator)
	}
	return
}

// GetFulfillmentRatesLocked 返回所有座位的兑现率（结算排行榜用）。
func (cl *CommitmentLedger) GetFulfillmentRatesLocked() []CommitmentRateInfo {
	type seatStats struct {
		fulfilled, broken, total int
	}
	stats := make(map[int]*seatStats, MaxPlayers)
	for _, c := range cl.items {
		s, ok := stats[c.Seat]
		if !ok {
			s = &seatStats{}
			stats[c.Seat] = s
		}
		s.total++
		switch c.Status {
		case CommitStatusFulfilled:
			s.fulfilled++
		case CommitStatusBroken:
			s.broken++
		}
	}

	out := make([]CommitmentRateInfo, 0, len(stats))
	for seat, s := range stats {
		rate := 0.0
		denom := s.fulfilled + s.broken
		if denom > 0 {
			rate = float64(s.fulfilled) / float64(denom)
		}
		out = append(out, CommitmentRateInfo{
			Seat:      seat,
			Total:     s.total,
			Fulfilled: s.fulfilled,
			Broken:    s.broken,
			Rate:      rate,
		})
	}
	return out
}

// CommitmentRateInfo 结算排行榜条目。
type CommitmentRateInfo struct {
	Seat      int     `json:"seat"`
	Total     int     `json:"total"`
	Fulfilled int     `json:"fulfilled"`
	Broken    int     `json:"broken"`
	Rate      float64 `json:"rate"` // 0.0-1.0
}

// CommitmentJSON 视图层脱敏后的承诺结构（下发给前端）。
type CommitmentJSON struct {
	ID         int64          `json:"id"`
	Seat       int            `json:"seat"`
	Round      int            `json:"round"`
	Template   CommitTemplate `json:"template"`
	ParamSeat  int            `json:"param_seat"`
	ParamText  string         `json:"param_text"`
	Reason     string         `json:"reason"`
	Status     CommitStatus   `json:"status"`
	CreatedAt  int64          `json:"created_at"`
}

// ToJSON 转换为视图层结构。
func (c *Commitment) ToJSON() CommitmentJSON {
	return CommitmentJSON{
		ID:        c.ID,
		Seat:      c.Seat,
		Round:     c.Round,
		Template:  c.Template,
		ParamSeat: c.ParamSeat,
		ParamText: c.ParamText,
		Reason:    c.Reason,
		Status:    c.Status,
		CreatedAt: c.CreatedAt,
	}
}

// ---------------------------------------------------------------------------
// WerewolfRoom 接入层（以下方法 caller 必须持 r.mu —— §92a）
// ---------------------------------------------------------------------------

// commitmentLedgerLocked 返回房间承诺账本，懒初始化。与 infoLedger/wolfPack 同模式，
// 避免 6 处 &WerewolfRoom{} 字面量构造点同步遗漏（§130「声明了却从不接线」防线）。
func (r *WerewolfRoom) commitmentLedgerLocked() *CommitmentLedger {
	if r.commitmentLedger == nil {
		r.commitmentLedger = NewCommitmentLedger()
	}
	return r.commitmentLedger
}

// addCommitmentLocked 添加一条承诺。
// caller 必须持 r.mu。活动广播由调用方（manager 层）负责。
func (r *WerewolfRoom) addCommitmentLocked(seat int, template CommitTemplate, paramSeat int, paramText, reason string) (*Commitment, error) {
	if r.State == nil {
		return nil, fmt.Errorf("game not started")
	}
	round := r.State.DayNumber
	maxPerDay := 3 // 默认每人每天最多 3 条
	return r.commitmentLedgerLocked().AddCommitmentLocked(seat, round, template, paramSeat, paramText, reason, maxPerDay)
}

// evaluateCommitmentsForTriggerLocked 评估特定触发点的承诺。
// caller 必须持 r.mu。活动广播由调用方（manager 层）负责。
func (r *WerewolfRoom) evaluateCommitmentsForTriggerLocked(trigger CommitTemplate, facts CommitFacts) []*Commitment {
	return r.commitmentLedgerLocked().EvaluateForTriggerLocked(trigger, facts)
}

// buildCommitFactsLocked 组装当前时刻的评估事实。
// caller 必须持 r.mu。
func (r *WerewolfRoom) buildCommitFactsLocked() CommitFacts {
	if r.State == nil {
		return CommitFacts{}
	}
	facts := CommitFacts{
		SeerSeat:         -1,
		SeerCheckTarget:  -1,
		DayVoteMap:       make(map[int]int),
		PlayerRoles:      make(map[int]Role),
		PlayerFactions:   make(map[int]Faction),
		SkillUsedTonight: make(map[int]bool),
		CurrentDay:       r.State.DayNumber,
		IsGameOver:       r.State.Status == "over",
	}

	// 找预言家座位
	for i := 0; i < MaxPlayers; i++ {
		if r.State.Roles[i] == RoleSeer {
			facts.SeerSeat = i
			break
		}
	}

	// 昨夜查验目标（从预言家 Player 记录取）
	if facts.SeerSeat >= 0 {
		facts.SeerCheckTarget = int(r.State.Players[facts.SeerSeat].LastSeerCheck)
	}

	// 白天投票
	for i := 0; i < MaxPlayers; i++ {
		if r.State.Players[i].Voted && r.State.Players[i].VoteTarget != NoSeat {
			facts.DayVoteMap[i] = int(r.State.Players[i].VoteTarget)
		}
	}

	// 角色与阵营（通过 FactionOf 从 Role 推导）
	for i := 0; i < MaxPlayers; i++ {
		facts.PlayerRoles[i] = r.State.Roles[i]
		facts.PlayerFactions[i] = FactionOf(r.State.Roles[i])
	}

	// 今夜技能使用（简化：LastSeerCheck 非 NoSeat 表示预言家用了技能）
	if facts.SeerSeat >= 0 && r.State.Players[facts.SeerSeat].LastSeerCheck != NoSeat {
		facts.SkillUsedTonight[facts.SeerSeat] = true
	}
	if r.State.WitchSeat != NoSeat && r.State.WitchSeat >= 0 && int(r.State.WitchSeat) < MaxPlayers {
		if r.State.Players[r.State.WitchSeat].WitchAntidoteUsed || r.State.Players[r.State.WitchSeat].WitchPoisonUsed {
			facts.SkillUsedTonight[int(r.State.WitchSeat)] = true
		}
	}
	if r.State.GuardSeat != NoSeat && r.State.GuardSeat >= 0 && int(r.State.GuardSeat) < MaxPlayers && r.State.GuardProtectTarget != NoSeat {
		facts.SkillUsedTonight[int(r.State.GuardSeat)] = true
	}

	return facts
}

// getCommitmentsForViewerLocked 返回按视角脱敏后的承诺 JSON 列表。
// caller 必须持 r.mu。
func (r *WerewolfRoom) getCommitmentsForViewerLocked(viewerSeat int) []CommitmentJSON {
	items := r.commitmentLedgerLocked().GetCommitmentsForViewerLocked(viewerSeat)
	out := make([]CommitmentJSON, 0, len(items))
	for _, c := range items {
		out = append(out, c.ToJSON())
	}
	return out
}

// getCommitmentFulfillmentRatesLocked 返回所有座位的兑现率（结算用）。
// caller 必须持 r.mu。
func (r *WerewolfRoom) getCommitmentFulfillmentRatesLocked() []CommitmentRateInfo {
	return r.commitmentLedgerLocked().GetFulfillmentRatesLocked()
}
