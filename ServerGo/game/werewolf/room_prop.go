package werewolf

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"LsmAgentGame/agent/wwplayer"
	"LsmAgentGame/agent/wwtypes"
	"LsmAgentGame/config"
	"LsmAgentGame/service"
)

type PropHistoryRecord struct {
	FromSeat   int    `json:"from_seat"`
	ToSeat     int    `json:"to_seat"`
	PropKey    string `json:"prop_key"`
	PropNameZh string `json:"prop_name_zh"`
	Hit        bool   `json:"hit"`
	EffectHint string `json:"effect_hint"`
	Phase      string `json:"phase"`
	Round      int    `json:"round"`
	CreatedAt  int64  `json:"created_at"` // unix seconds
}

// propHistoryCap 是道具历史环形缓冲的容量。
//
// 2026-08-14 §20260814-01 U1:原先 20 这个魔数在本函数里硬编码了两次
// (len < 20 与 % 20),而 recall_review_bridge.go 的
// propHistorySnapshotLocked 也需要它来还原环绕顺序 —— 三处漂移就会读出
// 错位数据。提为常量作单一事实来源。
const propHistoryCap = 20

func (r *WerewolfRoom) recordPropHistoryLocked(rec PropHistoryRecord) {
	if r == nil {
		return
	}
	if len(r.propHistory) < propHistoryCap {
		r.propHistory = append(r.propHistory, rec)
		return
	}
	// 环形写入
	r.propHistory[r.propHistoryHead] = rec
	r.propHistoryHead = (r.propHistoryHead + 1) % propHistoryCap
}

func (r *WerewolfRoom) GetPropHistoryLocked(limit int) []PropHistoryRecord {
	if r == nil || len(r.propHistory) == 0 {
		return nil
	}
	if limit <= 0 || limit > 20 {
		limit = 20
	}
	if limit > len(r.propHistory) {
		limit = len(r.propHistory)
	}
	out := make([]PropHistoryRecord, 0, limit)
	// 环形 buffer 顺序读取：新 → 旧
	start := len(r.propHistory) - limit
	for i := start; i < len(r.propHistory); i++ {
		out = append(out, r.propHistory[i])
	}
	return out
}

func (r *WerewolfRoom) GetPropHistoryForAPI(limit int) []PropHistoryRecord {
	if r == nil {
		return nil
	}
	if limit <= 0 {
		limit = 10
	}
	if limit > 20 {
		limit = 20
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.GetPropHistoryLocked(limit)
}

type PropInjectEntry struct {
	FromSeat   int
	PropKey    string
	InjectText string // 注入文本（已生成）
	// EffectTypes 是命中后应用的效果类型列表（逗号分隔解析为切片）。
	// 可选值：expose_identity / attention_scatter / target_twist / emotion_disturb / confuse_seer。
	EffectTypes string
	// TwistSeat 是 target_twist/confuse_seer 效果使用的引导座位（0-indexed, -1=无）。
	TwistSeat    int
	Hit          bool // 是否中招
	ExpiresAfter int  // 经过 N 轮 LLM 调用后过期（默认 1 轮）

	// v4 链式效果（R176 P2 补缺）：
	//   - Steps 非空时,ApplyPropEffectChain 走链式路径（按 DelayTurns 调度）
	//   - 留空时,保持 v3 行为（按 EffectTypes 逗号分隔一次性应用）
	Steps []PropEffectStep `json:"steps,omitempty"`
	// ScheduleKey 用于把 DelayTurns>0 的 step 排入 r.propEffectSchedule（与 PropInjectQueue 分开,
	// 避免 LLM 提示注入和效果落地路径相互干扰）。
	ScheduleKey string `json:"-"`
}

func (e PropInjectEntry) ParseEffectTypes() []string {
	if len(e.Steps) > 0 {
		out := make([]string, 0, len(e.Steps))
		for _, st := range e.Steps {
			if st.EffectType != "" {
				out = append(out, st.EffectType)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	if e.EffectTypes == "" {
		return nil
	}
	parts := strings.Split(e.EffectTypes, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

type PropEffectScheduledItem struct {
	DueAfterCalls int
	TargetSeat    int
	FromSeat      int
	PropKey       string
	Step          PropEffectStep
	CreatedAtCall int
}

func cfgWerewolfSameSeatSpeakCooldownSec() int {
	defer func() { _ = recover() }()
	v := config.Load().Werewolf.SameSeatSpeakCooldownSec
	if v <= 0 {
		return 60
	}
	return v
}

func cfgWerewolfMaxSpeaksPerPhasePerSeat() int {
	defer func() { _ = recover() }()
	v := config.Load().Werewolf.MaxSpeaksPerPhasePerSeat
	if v <= 0 {
		return 3
	}
	return v
}

func cfgWerewolfRoomPropBudget() int64 {
	defer func() { _ = recover() }()
	v := config.Load().Werewolf.RoomPropBudget
	if v < 0 {
		return 0
	}
	return v
}

func cfgWerewolfWolfTeammateHintRate() int {
	defer func() { _ = recover() }()
	v := config.Load().Werewolf.WolfTeammateHintRate
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

func cfgWerewolfWolfTeammateHintMaxPairs() int {
	defer func() { _ = recover() }()
	v := config.Load().Werewolf.WolfTeammateHintMaxPairs
	if v <= 0 {
		return 1
	}
	return v
}

func collectWolfSeatsLocked(r *WerewolfRoom) []int {
	if r == nil || r.State == nil {
		return nil
	}
	out := make([]int, 0, 4)
	for i, role := range r.State.Roles {
		if role == RoleWerewolf {
			out = append(out, i)
		}
	}
	return out
}

func (r *WerewolfRoom) allowSeatSpeakThisPhase(seat Seat) bool {
	if r == nil {
		return true
	}
	if !lockRoomBriefly(r, 200*time.Millisecond) {
		return true // 锁竞争:宽松放行,由 speakLimiter / dedup 兜底
	}
	defer r.mu.Unlock()
	max := cfgWerewolfMaxSpeaksPerPhasePerSeat()
	if max <= 0 {
		return true // 0 = 不限制
	}
	currentTag := ""
	if r.State != nil {
		currentTag = r.State.Phase.String()
	}
	// 阶段切换 → 自动重置计数
	if r.seatSpeakCountPhaseTag != currentTag {
		r.seatSpeakCountThisPhase = make(map[int]int)
		r.seatSpeakCountPhaseTag = currentTag
	}
	if r.seatSpeakCountThisPhase == nil {
		r.seatSpeakCountThisPhase = make(map[int]int)
	}
	return r.seatSpeakCountThisPhase[int(seat)] < max
}

func (r *WerewolfRoom) bumpSeatSpeakCountThisPhase(seat Seat) {
	if r == nil {
		return
	}
	if !lockRoomBriefly(r, 200*time.Millisecond) {
		return
	}
	defer r.mu.Unlock()
	if r.seatSpeakCountThisPhase == nil {
		r.seatSpeakCountThisPhase = make(map[int]int)
	}
	currentTag := ""
	if r.State != nil {
		currentTag = r.State.Phase.String()
	}
	// 阶段切换 → 重置后再累加
	if r.seatSpeakCountPhaseTag != currentTag {
		r.seatSpeakCountThisPhase = make(map[int]int)
		r.seatSpeakCountPhaseTag = currentTag
	}
	r.seatSpeakCountThisPhase[int(seat)]++
}

// ─── §4 行数治理搬移(2026-08-30 §20260830-01 同批):以下「道具系统方法」
// doc 注释原漂浮在 room.go,现搬至实现所在文件,零逻辑改动。 ───

// ─── 道具系统方法 (2026-07-21) ───

// propCooldownRemainLocked 返回座位距离下次可使用道具的剩余秒数（0 = 可用）。
// 必须在 r.mu 已持锁状态下调用。

// isPropCooldownLocked 检查座位是否在道具冷却中。

// propCountForSeatLocked 返回座位本局已使用道具次数。
// 必须在 r.mu 已持锁状态下调用。

// PropPerSeatSnapshotLocked 在 r.mu 已持锁状态下,回填某座位的道具 per-room 状态:
//   - *remaining = MaxPerGame - 已用次数(负值截断到 0)
//   - *cooldownSec = 距离下次可使用道具的剩余秒数(0 = 立即可用)
//
// 是读取侧的"前端 PropPanel / REST ListProps"权威数据源;无副作用。
//
// R173 之前 ListProps 只返 {props,total},前端 PropPanel 的余额/剩余/冷却全部
// 显示 0 — 修复:ListProps 内先调 RoomPropPerSeatSnapshot(短线持锁)进入本方法。

// RoomPropPerSeatSnapshot 是 PropPerSeatSnapshotLocked 的导出版 — 供 api 包
// (等外部包)短线持锁后回填 per-seat 道具状态。
// 持锁失败(例如 200ms 超时)时,*remaining/*cooldownSec 不被修改,返回 false。

// recordPropUseLocked 记录一次道具使用（冷却重置 + 计数累加 + 彩池累加 + 预算累加）。
// price 是本次消耗的道具完整价格（用于 v2 全局/个人预算累加）；potReturn
// 是回滚到彩池的部分（price 的 50%）。必须在 r.mu 已持锁状态下调用。

// enqueuePropInjectLocked 把道具注入文本加入目标座位的注入队列。
// buildAgentContextLocked 在构造 GameContext 时消费此队列。
// 必须在 r.mu 已持锁状态下调用。

// schedulePropEffectStepLocked 把一条 v4 链式效果 step 加入延迟调度表。
// 仅在 PropEffectStep.DelayTurns > 0 时调用,即时 step 走 ApplyEffects 直接落地。
// R176 P2 补缺：补回 v4 commit 描述的"效果链"延迟调度路径。
// 必须在 r.mu 已持锁状态下调用。

// tickPropEffectScheduleLocked 把到期的链式效果应用到目标 GameContext。
// 由 buildAgentContextLocked 入口处调用(增加 propEffectRoundCounter 并检查到期项)。
// 返回:已应用的 step 数量(用于日志)。必须在 r.mu 已持锁状态下调用。

// evaluatePropStepCondition 评估 v4 chain step 的 Condition 字符串。
//   - "always" / "" → 始终应用
//   - "target_alive" → 仅当目标在 ctx.AliveSeats 中
//   - "target_in_speak" → 暂等同于 always(发言阶段已在 buildAgentContextLocked 触发,
//     延迟 step 落地的具体 phase 校验留作 v4.1)
//   - 其他 / 不识别 → 默认 always(允许宽松扩展)

// drainPropInjectQueueLocked 消费并返回座位的待注入道具队列（取出后清空）。
// 必须在 r.mu 已持锁状态下调用。

// resetPropStateLocked 在 restartGameLocked / 房间重置时清零道具系统状态。
// 必须在 r.mu 已持锁状态下调用。

// roomTotalCoin 返回房间内所有存活玩家的钱包余额总和（v4 §13.2 经济档位判定入参）。
// 必须在 r.mu 已持锁状态下调用。返回 0 表示无钱包服务或房间内无余额数据。
//
// 实现：仅累加存活玩家的余额；人类 + Bot 都包含。r.propEngine 提供 walletSvc 句柄;
// 若 walletSvc 为 nil（如测试桩）则返回 0 → ComputeEconTier → EconDanger（最严档）。

// enqueuePropHitLocked 把一次命中的道具效果入队（注入文本 + 干扰信号）。
// 由 agent_runner.UseProp / ws handleWerewolfUseProp 在持锁后调用。
// effectTypes: 逗号分隔的效果类型列表；twistSeat: target_twist 引导座位（-1=无）。
// 必须在 r.mu 已持锁状态下调用。

// computeTwistSeatLocked 按道具的 TwistSeatSrc 计算 target_twist 的引导座位（v2）。
// 返回 -1 表示不引导。必须在 r.mu 已持锁状态下调用。
//   - "from_seat": 引导目标打使用者（fromSeat）。
//   - "random_enemy": 引导打目标所在阵营的随机敌对阵营玩家；找不到敌人 → -1。
//   - "most_trusted": 不指定具体座位（返回 -1），由注入文本的隐藏任务引导
//     "做决策时最想选的那个"（注意力失焦专用，实现"杀错人"）。

func (r *WerewolfRoom) propCooldownRemainLocked(seat int, cooldownSec int) int {
	if r.propCooldown == nil {
		return 0
	}
	last, ok := r.propCooldown[seat]
	if !ok {
		return 0
	}
	elapsed := time.Since(last).Seconds()
	remain := int64(cooldownSec) - int64(elapsed)
	if remain <= 0 {
		return 0
	}
	return int(remain)
}

func (r *WerewolfRoom) isPropCooldownLocked(seat int, cooldownSec int) bool {
	return r.propCooldownRemainLocked(seat, cooldownSec) > 0
}

func (r *WerewolfRoom) propCountForSeatLocked(seat int) int {
	if r.propCount == nil {
		return 0
	}
	return r.propCount[seat]
}

func (r *WerewolfRoom) PropPerSeatSnapshotLocked(seat int, remaining, cooldownSec *int) bool {
	if r == nil || remaining == nil || cooldownSec == nil {
		return false
	}
	maxPerGame := defaultPropMaxPerGame(r)
	*remaining = maxPerGame - r.propCountForSeatLocked(seat)
	if *remaining < 0 {
		*remaining = 0
	}
	// 冷却权威值:所有已启用道具的最严格冷却(取最大 cooldown_sec)。
	// 单程 UI 显示仅需要一个总值;后端冷却校验按 prop_key 独立。
	maxCooldown := 0
	if r.propCatalog != nil {
		for _, e := range r.propCatalog.ListEnabled() {
			if e.CooldownSec > maxCooldown {
				maxCooldown = e.CooldownSec
			}
		}
	}
	if maxCooldown <= 0 {
		maxCooldown = defaultPropCooldownSec(r)
	}
	*cooldownSec = r.propCooldownRemainLocked(seat, maxCooldown)
	return true
}

func RoomPropPerSeatSnapshot(r *WerewolfRoom, seat int, remaining, cooldownSec *int) bool {
	if r == nil || remaining == nil || cooldownSec == nil {
		return false
	}
	if !lockRoomBriefly(r, 200*time.Millisecond) {
		return false
	}
	defer r.mu.Unlock()
	return r.PropPerSeatSnapshotLocked(seat, remaining, cooldownSec)
}

func (r *WerewolfRoom) recordPropUseLocked(seat int, price, potReturn int64) {
	if r.propCooldown == nil {
		r.propCooldown = make(map[int]time.Time)
	}
	if r.propCount == nil {
		r.propCount = make(map[int]int)
	}
	if r.propSeatBudget == nil {
		r.propSeatBudget = make(map[int]int64)
	}
	r.propCooldown[seat] = time.Now()
	r.propCount[seat]++
	r.propPotBonus += potReturn
	// v2：全局/个人道具预算累加。
	r.propSeatBudget[seat] += price
	r.roomPropBudgetUsed += price
}

func (r *WerewolfRoom) enqueuePropInjectLocked(seat int, entry PropInjectEntry) {
	if r.propInjectQueue == nil {
		r.propInjectQueue = make(map[int][]PropInjectEntry)
	}
	r.propInjectQueue[seat] = append(r.propInjectQueue[seat], entry)
}

func (r *WerewolfRoom) schedulePropEffectStepLocked(targetSeat, fromSeat int, propKey string, step PropEffectStep) {
	if r.propEffectSchedule == nil {
		r.propEffectSchedule = make(map[string]PropEffectScheduledItem)
	}
	createdAt := r.propEffectRoundCounter
	key := fmt.Sprintf("%s|%d|%d|%d|%s", propKey, targetSeat, createdAt, step.DelayTurns, step.EffectType)
	r.propEffectSchedule[key] = PropEffectScheduledItem{
		DueAfterCalls: step.DelayTurns,
		TargetSeat:    targetSeat,
		FromSeat:      fromSeat,
		PropKey:       propKey,
		Step:          step,
		CreatedAtCall: createdAt,
	}
}

func (r *WerewolfRoom) tickPropEffectScheduleLocked(gcBySeat map[int]*wwtypes.GameContext) int {
	if len(r.propEffectSchedule) == 0 {
		return 0
	}
	r.propEffectRoundCounter++
	applied := 0
	for key, item := range r.propEffectSchedule {
		elapsed := r.propEffectRoundCounter - item.CreatedAtCall
		if elapsed < item.DueAfterCalls {
			continue
		}
		// 到期:评估 Condition（target_alive / target_in_speak / always）。
		if !evaluatePropStepCondition(item.Step.Condition, gcBySeat[item.TargetSeat], item.TargetSeat) {
			delete(r.propEffectSchedule, key)
			continue
		}
		// 应用单个 step:走 ApplyEffects 路径(只跑一个 effect)。
		ctx := EffectApplyContext{FromSeat: item.FromSeat, Entry: PropInjectEntry{
			PropKey:      item.PropKey,
			FromSeat:     item.FromSeat,
			EffectTypes:  item.Step.EffectType,
			TwistSeat:    -1,
			Hit:          true,
			ExpiresAfter: 1,
		}}
		if gc := gcBySeat[item.TargetSeat]; gc != nil {
			ApplyEffects(gc, item.TargetSeat, ctx.Entry, ctx)
			applied++
		}
		delete(r.propEffectSchedule, key)
	}
	return applied
}

func evaluatePropStepCondition(cond string, gc *wwtypes.GameContext, seat int) bool {
	switch cond {
	case "", "always":
		return true
	case "target_alive":
		if gc == nil {
			return false
		}
		for _, s := range gc.AliveSeats {
			if s == seat {
				return true
			}
		}
		return false
	case "target_in_speak":
		// v4 链式 step 的延迟落地通常跨多 phase,发言阶段校验留作 v4.1 细化;
		// 此处先放行,避免误杀延迟 step。
		return true
	default:
		return true
	}
}

func (r *WerewolfRoom) drainPropInjectQueueLocked(seat int) []PropInjectEntry {
	if r.propInjectQueue == nil {
		return nil
	}
	entries := r.propInjectQueue[seat]
	if len(entries) == 0 {
		return nil
	}
	// 2026-08-07 §20260807-04 P1-2:原实现 `for _, e := range entries` 中
	// `e.ExpiresAfter--` 只作用在值拷贝上,原切片中的 ExpiresAfter 从未递减 —
	// ExpiresAfter>1 的条目(如 v4 链式效果)会永远不过期。改为索引遍历。
	valid := make([]PropInjectEntry, 0, len(entries))
	for i := range entries {
		if entries[i].ExpiresAfter > 0 {
			entries[i].ExpiresAfter--
			valid = append(valid, entries[i])
		}
	}
	r.propInjectQueue[seat] = nil
	return valid
}

// setHumanDebuffLocked 把人类反制道具的 debuff 写到目标座位的 Player.HumanDebuff。
// §92a:持锁内调用方使用;目标必须是真人座位(非 bot)。
func (r *WerewolfRoom) setHumanDebuffLocked(seat int, spec wwtypes.HumanDebuffSpec) {
	if r == nil || r.State == nil {
		return
	}
	if seat < 0 || seat >= len(r.State.Players) {
		return
	}
	r.State.Players[seat].HumanDebuff = &spec
}

// buildHumanDebuffSpecLocked 按人类反制道具的 EffectSpec 构造 HumanDebuffSpec。
// 从 EffectTypes 解析出第一个 human_* 效果类型;vote_suggest 时按
// TwistSeatSrc 计算 SuggestSeat(默认引导到使用者座位)。
func buildHumanDebuffSpecLocked(catEntry *PropCatalogEntry, fromSeat, toSeat int) *wwtypes.HumanDebuffSpec {
	if catEntry == nil {
		return nil
	}
	spec := &wwtypes.HumanDebuffSpec{
		SuggestSeat: -1,
		Duration:    1,
		PropNameZh:  catEntry.NameZh,
	}
	for _, et := range catEntry.EffectSpec.EffectTypeToList() {
		switch et {
		case "human_announce_prefix":
			spec.Type = et
			return spec
		case "human_vote_suggest":
			spec.Type = et
			if ts := computeTwistSeatForSpec(catEntry.EffectSpec.TwistSeatSrc, fromSeat, toSeat); ts >= 0 {
				spec.SuggestSeat = ts
			}
			return spec
		case "human_char_garble":
			spec.Type = et
			return spec
		}
	}
	return nil
}

// computeTwistSeatForSpec 是 computeTwistSeatLocked 的无房间依赖降级版
// (buildHumanDebuffSpecLocked 被调用时可能无完整 State 语境)。
func computeTwistSeatForSpec(twistSeatSrc string, fromSeat, toSeat int) int {
	switch twistSeatSrc {
	case "from_seat":
		return fromSeat
	default:
		return fromSeat
	}
}

// propHitSummary 构造「上一轮被道具击中」的摘要文案(2026-08-07 §20260807-04 P2-2)。
// 由 buildAgentContextLocked 在防御性重置前把上一轮 PropLastEffect 转存到房间字段,
// 本轮填入 gc.PropHitLastRound,PropEffectSignalBlock 渲染成「📌 上一轮你被击中」。
func propHitSummary(propKey string) string {
	if propKey == "" {
		return ""
	}
	return fmt.Sprintf("「%s」(%s)", PropKeyToName(propKey), propKey)
}

func (r *WerewolfRoom) resetPropStateLocked() {
	r.propPotBonus = 0
	r.propCooldown = make(map[int]time.Time)
	r.propCount = make(map[int]int)
	r.propSeatBudget = make(map[int]int64)
	r.roomPropBudgetUsed = 0
	r.propInjectQueue = make(map[int][]PropInjectEntry)
	r.lastPropHitEffect = make(map[int]string)
	// v4 §13.1：重置狼小队通道(成员清空+留言清空,保留对象引用)。
	if r.wolfPack != nil {
		r.wolfPack.SetMembers(nil)
		// 通过 Append/PurgeByDeath 路径清空留言;这里直接重置对象。
		r.wolfPack = NewWolfPackRoom(r.RoomID, 50)
	}
}

func (r *WerewolfRoom) roomTotalCoin() int64 {
	if r == nil {
		return 0
	}
	if r.propEngine == nil || r.propEngine.walletSvc == nil {
		return 0
	}
	if r.State == nil {
		return 0
	}
	walletSvc := r.propEngine.walletSvc
	var total int64
	for seat, uid := range r.Seats {
		if uid == "" {
			continue
		}
		if seat < len(r.State.Players) && !r.State.Players[seat].Alive {
			continue
		}
		bal, err := walletSvc.GetBalance(context.Background(), uid)
		if err != nil {
			continue
		}
		if bal > 0 {
			total += bal
		}
	}
	return total
}

func (r *WerewolfRoom) enqueuePropHitLocked(target int, entry PropInjectEntry) {
	if r.propInjectQueue == nil {
		r.propInjectQueue = make(map[int][]PropInjectEntry)
	}
	r.propInjectQueue[target] = append(r.propInjectQueue[target], entry)
	// 2026-08-10 §20260810-05 — 信息账本:道具注入仅被击中者知情。
	// manager/agent 双路径已汇于此单一函数(room_action.go / agent_runner.go),
	// 在此处登记一次即覆盖两条路径(§132 教训:双路径必须同源)。
	r.ledgerAppendLocked(InfoSourcePropInject,
		fmt.Sprintf("prop_inject from=%d target=%d prop=%s hit=%v", entry.FromSeat, target, entry.PropKey, entry.Hit),
		singleKnowerSet(target), time.Now().UnixMilli())

	// 2026-08-13 §20260813-04 U1 — 实时注入到目标 bot 的 SteeringQueue。
	//
	// 此前道具命中只能等目标 bot 下一轮 handleEvent 入口经
	// PropInjectPromptBlock 感知;若目标正卡在一次慢模型 LLM 调用中(§197 可达
	// 1-3 分钟),命中信号要滞后整整一轮。SteeringQueue 让内层循环下一轮就看到。
	//
	// 本函数已持 r.mu(manager/agent 双路径都在锁内调用),而 SteeringQueue
	// 内部只用自己的 mutex + channel,不触碰 r.mu,无锁序风险(§92a)。
	// Enqueue 非阻塞:满队列丢弃最旧,不会阻塞持锁路径。
	if entry.Hit {
		if ag := r.BotAgents[target]; ag != nil {
			if q := ag.SteeringQueue(); q != nil {
				q.Enqueue(wwplayer.AgentSteerMsg{
					Kind:    wwplayer.SteerPropHit,
					Content: fmt.Sprintf("你被 %s 击中(来自 %d 号)。", entry.PropKey, entry.FromSeat),
				})
			}
		}
	}
}

func (r *WerewolfRoom) computeTwistSeatLocked(twistSeatSrc string, fromSeat, toSeat int) int {
	if r.State == nil || toSeat < 0 || toSeat >= len(r.State.Roles) {
		return -1
	}
	switch twistSeatSrc {
	case "from_seat":
		return fromSeat
	case "most_trusted":
		// 由注入文本的隐藏任务引导，服务端不指定具体座位。
		return -1
	case "random_enemy":
		toFaction := FactionOf(r.State.Roles[toSeat])
		// 找随机敌对阵营的存活玩家。
		candidates := make([]int, 0, len(r.State.Players))
		for i := 0; i < len(r.State.Players); i++ {
			if !r.State.Players[i].Alive || i == toSeat {
				continue
			}
			if FactionOf(r.State.Roles[i]) != toFaction {
				candidates = append(candidates, i)
			}
		}
		if len(candidates) == 0 {
			return -1
		}
		return candidates[rand.Intn(len(candidates))]
	default:
		return fromSeat
	}
}

func mgrPropEngineWalletSvc(r *WerewolfRoom) *service.WalletService {
	if r == nil || r.propEngine == nil {
		return nil
	}
	return r.propEngine.walletSvc
}

func werewolfAnteAmountLocked(r *WerewolfRoom) int64 {
	if r == nil || r.State == nil {
		return 100
	}
	// 13 人局标准 ante=100；可被 propBudgetOverride 类似机制覆盖（TODO: 配置化）。
	switch r.State.SeatCount {
	case 13:
		return 100
	case 12:
		return 90
	case 7:
		return 50
	}
	return 100
}

func buildPropSnapshotLocked(r *WerewolfRoom, gc wwtypes.GameContext) []wwtypes.PropSnapshot {
	enabled := r.propCatalog.ListEnabled()
	out := make([]wwtypes.PropSnapshot, 0, len(enabled))
	budgetRemain := r.roomPropBudget() - r.roomPropBudgetUsed
	for _, e := range enabled {
		if gc.PropUsedThisGame >= e.MaxPerGame {
			continue // 个人上限
		}
		if gc.PropCooldownRemainingSec > 0 {
			continue // 冷却中
		}
		if r.roomPropBudget() > 0 && e.Price > budgetRemain {
			continue // 全局预算不足买这个
		}
		out = append(out, wwtypes.PropSnapshot{
			PropKey:      e.PropKey,
			NameZh:       e.NameZh,
			NameEn:       e.NameEn,
			NameJa:       e.NameJa,
			Description:  e.Description,
			Price:        e.Price,
			BaseHitRate:  e.BaseHitRate,
			IsAOE:        e.IsAOE,
			InjectGenKey: e.InjectGenKey,
		})
	}
	return out
}

func (r *WerewolfRoom) roomPropBudget() int64 {
	if r.roomPropBudgetOverride > 0 {
		return r.roomPropBudgetOverride
	}
	v := cfgWerewolfRoomPropBudget()
	if v < 0 {
		v = 0
	}
	return v
}

func (r *WerewolfRoom) propSeatBudgetUsedLocked(seat int) int64 {
	if r.propSeatBudget == nil {
		return 0
	}
	return r.propSeatBudget[seat]
}

func defaultPropCooldownSec(r *WerewolfRoom) int {
	_ = r
	return 30
}

func defaultPropMaxPerGame(r *WerewolfRoom) int {
	_ = r
	return 3
}

