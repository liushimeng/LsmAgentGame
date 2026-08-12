package werewolf

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"LsmAgentGame/agent/wwplayer"
	"LsmAgentGame/errcode"
	"LsmAgentGame/llm"
	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

func (m *WerewolfManager) GetState(roomID, userID string) (*ClientGameState, *errcode.Error) {
	r := m.getRoom(roomID)
	if r == nil {
		return nil, errcode.Code(errcode.ErrRoomNotFound)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var seat Seat = NoSeat
	if r.State != nil {
		seat = r.State.SeatOf(userID)
	} else {
		// 入座但 State 还没创建
		for i, u := range r.Seats {
			if u == userID {
				seat = Seat(i)
				break
			}
		}
	}
	if seat == NoSeat {
		return nil, errcode.Code(errcode.ErrRoomNotIn)
	}
	cs := BuildClientStateWithRoom(roomID, r, int(seat))
	r.populateBotContexts(cs)
	r.populateAgentNames(cs, m.registry)
	// BUG-R227-P2-01: enrich 必须**在** populateAgentNames 之后,
	// 否则 cs.Players[].AgentName 还是空串,bot 昵称无法升级为
	// "agent_name #N号" 格式。
	r.enrichDeadListAccountsLocked(cs)
	return cs, nil
}

func (m *WerewolfManager) StateForSeat(roomID string, seat Seat) *ClientGameState {
	r := m.getRoom(roomID)
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	cs := BuildClientStateWithRoom(roomID, r, int(seat))
	r.populateBotContexts(cs)
	r.populateAgentNames(cs, m.registry)
	// BUG-R227-P2-01: 同上,必须在 populateAgentNames 之后 enrich。
	r.enrichDeadListAccountsLocked(cs)
	return cs
}

type PublicState struct {
	Phase  string // "filling"|"night_wolves"|"night_seer"|"night_witch"|"dawn"|"sheriff"|"speak"|"vote"|"hunter_shoot"|"over"
	Day    int    // 当前 DayNumber(狼人杀称为「第 N 天」,0 = 未开局)
	Status string // "open"|"playing"|"over"|"closed"
	Winner string // "wolf"|"good"|""
	// BUG-R200-P2-04 (2026-07-30): true 表示该快照来自 publicStateCache 兜底
	// 而非实时锁内读取,前端可据此渲染「数据稍滞后」提示(参考时钟界面风格)。
	Stale bool `json:"stale,omitempty"`
}

type PublicPlayerState struct {
	Seat         int    `json:"seat"`
	UserID       string `json:"user_id"`
	Alive        bool   `json:"alive"`                   // 存活标记
	RoleRevealed bool   `json:"role_revealed"`           // 死亡/GameOver 后角色自动揭示
	Role         string `json:"role,omitempty"`          // 仅 RoleRevealed=true 时填充(枚举字符串)
	Faction      string `json:"faction,omitempty"`       // "wolf"|"good"|"third"
	DeathCause   string `json:"death_cause,omitempty"`   // wolf/vote/hunter/witch_poison/suicide;未死亡为空
	DeathVerdict string `json:"death_verdict,omitempty"` // execution/death;未死亡为空
	IsSheriff    bool   `json:"is_sheriff"`
}

func (m *WerewolfManager) GetPublicState(roomID string) (PublicState, bool) {
	r := m.getRoom(roomID)
	if r == nil {
		return PublicState{}, false
	}
	// BUG-WEREWOLF-P1-LOCK (Round 26): REST handlers used to block on r.mu.Lock()
	// for the full duration of any in-progress engine op (LLM retries,
	// auto-skip dispatch, quarantine). When the engine is mid-loop, that could
	// hold the lock long enough to starve /api/games/werewolf/rooms and
	// /api/rooms/{id} indefinitely (curl --max-time 20 showed no response and
	// no log line). Use a non-blocking snapshot under a 200ms deadline: if we
	// can't grab the lock in time, return the last-known cached snapshot so
	// the REST path never hangs.
	type snapshot struct {
		phase   string
		day     int
		status  string
		winner  string
		filling bool
	}
	get := func() (snapshot, bool) {
		s := snapshot{}
		if !lockRoomBriefly(r, 200*time.Millisecond) {
			return s, false
		}
		defer r.mu.Unlock()
		if r.State == nil {
			s.phase = PhaseFilling.String()
			s.filling = true
			s.status = "open"
			return s, true
		}
		s.phase = r.State.Phase.String()
		s.day = r.State.DayNumber
		s.status = r.State.Status
		s.winner = r.State.Winner
		return s, true
	}
	s, ok := get()
	if !ok {
		// Lock contended: emit the last cached snapshot (or "filling" if we
		// never got a chance to seed it) so the REST caller doesn't hang.
		// BUG-R200-P2-04 (2026-07-30): 在缓存兜底响应上标记 stale=true,
		// 让前端可感知「数据稍滞后」并避免显示与 WS game.state 长时间矛盾。
		if cached, present := m.publicStateCache.Load(roomID); present {
			if ps, ok2 := cached.(PublicState); ok2 {
				ps.Stale = true
				return ps, true
			}
		}
		return PublicState{Phase: PhaseFilling.String(), Status: "open", Stale: true}, true
	}
	ps := PublicState{Phase: s.phase, Day: s.day, Status: s.status, Winner: s.winner}
	m.publicStateCache.Store(roomID, ps)
	return ps, true
}

func (m *WerewolfManager) GetPublicPlayerStates(roomID string) []PublicPlayerState {
	r := m.getRoom(roomID)
	if r == nil {
		return nil
	}
	type snapshot struct {
		players []PublicPlayerState
	}
	get := func() (snapshot, bool) {
		var s snapshot
		if !lockRoomBriefly(r, 200*time.Millisecond) {
			return s, false
		}
		defer r.mu.Unlock()
		// 把 [MaxPlayers] 数组按占用顺序投影成切片,调用方按 seat 排序即可
		out := make([]PublicPlayerState, 0, MaxPlayers)
		for i := 0; i < MaxPlayers; i++ {
			uid := r.Seats[i]
			if uid == "" {
				continue
			}
			pj := PublicPlayerState{
				Seat:   i,
				UserID: uid,
			}
			if r.State == nil {
				// 未开局:占位数据,全部视为存活,角色不揭示
				pj.Alive = true
			} else {
				p := &r.State.Players[i]
				pj.Alive = p.Alive
				pj.IsSheriff = Seat(i) == r.State.SheriffSeat
				pj.DeathCause = p.DeathCause
				pj.DeathVerdict = p.DeathVerdict
				// §135 角色揭示规则与 view.go::BuildClientState 一致,统一走
				// RolePubliclyRevealed 白名单(终局 / 白痴翻牌 / 狼自爆 / 猎人开枪)。
				// 这里**不**暴露给"自己" —— 调用方是 REST 详情接口,而非某一座位
				// 的玩家视图。
				//
				// ⚠️ 历史违规:`if !p.Alive` 使任何登录用户都能用一个 REST 请求
				// 拿到全部死者身份,绕过 WS 视图的全部脱敏。切勿恢复。
				if r.State.RolePubliclyRevealed(Seat(i)) {
					pj.RoleRevealed = true
					pj.Role = r.State.Roles[i].String()
					pj.Faction = FactionOf(r.State.Roles[i]).String()
				}
			}
			out = append(out, pj)
		}
		s.players = out
		return s, true
	}
	if snap, ok := get(); ok {
		return snap.players
	}
	// Lock contended:fall back to a minimal placeholder so the REST caller
	// never hangs. 没有 cache 可回退时返回 nil(由调用方决定降级策略)。
	return nil
}

func (m *WerewolfManager) SpectatorRoomStatus(roomID string) (exists, live bool, phase string) {
	r := m.getRoom(roomID)
	if r == nil {
		return false, false, "gone"
	}
	if !lockRoomBriefly(r, 200*time.Millisecond) {
		// Contended: assume live=true so the caller's existing broadcast
		// behavior is preserved unchanged under lock contention. Spectator
		// view stays responsive; lock briefness was added for the REST
		// pull path, the WS broadcast is best-effort anyway.
		return true, true, ""
	}
	defer r.mu.Unlock()
	if r.State == nil {
		return true, false, "gone"
	}
	phase = r.State.Phase.String()
	seatsFilled := 0
	for _, s := range r.Seats {
		if s != "" {
			seatsFilled++
		}
	}
	playersAlive := 0
	for _, p := range r.State.Players {
		if p.UserID != "" {
			playersAlive++
		}
	}
	live = playersAlive > 0 || seatsFilled > 0
	return true, live, phase
}

func (r *WerewolfRoom) populateBotContexts(cs *ClientGameState) {
	if cs == nil || r == nil {
		return
	}
	// 合并两个数据源:seatModelKeys(注册) ∪ BotAgents(已启动)。
	// seatModelKeys 是「这个房间有多少 bot 座位」的真实来源,
	// BotAgents 是「agent goroutine 是否已 spawn」的子集。
	seats := make(map[int]string, MaxPlayers)
	for seat, model := range r.seatModelKeys {
		if model == "" {
			continue
		}
		seats[seat] = model
	}
	for seat, ag := range r.BotAgents {
		if ag == nil {
			continue
		}
		if ag.ModelKey != "" {
			seats[seat] = ag.ModelKey
		}
	}
	if len(seats) == 0 {
		// 既无 seatModelKeys 也无 BotAgents —— 真正的纯人类房间,
		// 不需要填充 bot_contexts 字段。
		logger.L().Debug("populateBotContexts: no bot seats registered",
			zap.String("room_id", r.RoomID),
			zap.Int("my_seat", cs.MySeat),
			zap.Bool("is_spectator", cs.MySeat < 0 || cs.MySeat >= MaxPlayers))
		return
	}
	isSpectator := cs.MySeat < 0 || cs.MySeat >= MaxPlayers
	out := make([]wwplayer.BotTranscript, 0, len(seats))
	for seat, modelKey := range seats {
		var ag *wwplayer.Agent
		if r.BotAgents != nil {
			ag = r.BotAgents[seat]
		}
		// 如果 agent 还没启动 (PhaseFilling / registry nil),ag 为 nil,
		// 也输出占位——前端面板至少显示 N 个 tab,提示「这些座位是 bot」。
		if ag == nil {
			out = append(out, wwplayer.BotTranscript{
				Seat:      seat,
				Model:     modelKey,
				LastTool:  "",
				ToolCalls: []string{},
			})
			continue
		}
		bt := ag.BotTranscript()
		if bt == nil {
			// 2026-07-09 §13-bugfix: if the agent is currently mid-LLM-call,
			// emit a "calling" transcript instead of the static placeholder
			// so the frontend can render a live timer.
			if inProgress, startedAt := ag.IsLLMCallInProgress(); inProgress {
				calling := wwplayer.BotTranscript{
					Seat:                seat,
					Model:               modelKey,
					LastDecisionSummary: "🤖 正在调用大模型,请稍候…",
					ToolCalls:           []string{},
					UpdatedAt:           time.Now().UnixMilli(),
					LLMCallInProgress:   true,
					LLMCallStartedAt:    startedAt,
				}
				if cq := ag.ChatQueue(); cq != nil {
					bytes, _, _, _ := cq.Stats()
					calling.ChatHistoryBytes = int(bytes)
					calling.ChatHistoryCap = ag.ChatCap()
				}
				out = append(out, calling)
				continue
			}
			// Agent wired but no decision completed yet — emit a placeholder
			// so the panel shows the seat + model rather than under-counting.
			// §128 对话即思考重构:LastThinking / FullThinking / RecentMessages 已删除。
			placeholder := wwplayer.BotTranscript{
				Seat:                seat,
				Model:               modelKey,
				LastDecisionSummary: "（等待决策）等待本座位轮到行动后,LLM 决策将自动显示在这里。",
				ToolCalls:           []string{},
				UpdatedAt:           ag.StartedAt().UnixMilli(),
			}
			// 把 500K 队列字节数也带上,即使从未发言也能看到队列在累积
			if cq := ag.ChatQueue(); cq != nil {
				bytes, _, _, _ := cq.Stats()
				placeholder.ChatHistoryBytes = int(bytes)
				placeholder.ChatHistoryCap = ag.ChatCap()
			}
			out = append(out, placeholder)
			continue
		}
		// Sanitize before surfacing:
		//   - human player in mixed mode → strip everything private that
		//     leaks strategy (whispers, sensitive tool targets, CoT, and
		//     HeartThought which carries the bot's real identity/plan);
		//   - spectator → keep HeartThought (§119 dictates it is "仅自己 +
		//     观战者可见") but still mask sensitive tool targets in
		//     LastDecisionSummary / tool_calls (wolf_kill → 7号 would
		//     otherwise hand spectators a wall-hack). R87 P0-1 / P0-3.
		//
		// 2026-08-10 §20260810-07 — 多假说并行推演:在 sanitize 之前先把
		// LastDecisionSummary 末尾的「📊 [...]」JSON 段解析写回房间级
		// HypothesisStore,再把 HypothesisEntryJSON 列表挂到 bt.HypothesisSummary
		// 供前端 HistoryDrawer 第 5 sub-tab「🔮 假说」渲染;§135 spectator 隔离
		// 由 sanitizeBotTranscript 兜底(玩家侧清空 bt.HypothesisSummary)。
		populateHypothesisSummary(r, bt)
		sanitized := sanitizeBotTranscript(*bt, isSpectator)
		out = append(out, sanitized)
	}
	// Deterministic seat order (map iteration is randomized) so the UI is
	// stable across broadcasts.
	sort.Slice(out, func(i, j int) bool { return out[i].Seat < out[j].Seat })
	cs.BotContexts = out
	logger.L().Debug("populateBotContexts: populated bot contexts",
		zap.String("room_id", r.RoomID),
		zap.Int("count", len(out)),
		zap.Int("seat_model_keys", len(r.seatModelKeys)),
		zap.Int("bot_agents", len(r.BotAgents)),
		zap.Bool("is_spectator", isSpectator))
}

func (r *WerewolfRoom) populateAgentNames(cs *ClientGameState, registry *llm.Registry) {
	if cs == nil || registry == nil {
		return
	}
	for seat, modelKey := range r.seatModelKeys {
		if seat < 0 || seat >= MaxPlayers || modelKey == "" {
			continue
		}
		if info, ok := registry.GetInfo(modelKey); ok {
			cs.Players[seat].AgentName = info.AgentName
		} else {
			cs.Players[seat].AgentName = modelKey
		}
	}
}

// enrichDeadListAccountsLocked 把 cs.AllDeadListVerbose / LastNightDeathsVerbose /
// phase_extra.DeadList 三处 DeadPlayerJSON.Account 从 seatDisplayAccount 兜底
// ("Bot #N号"/"玩家N号")升级为含 agent_name 的完整昵称("DeepSeek V4-Pro #2号")。
//
// BUG-R227-P2-01 (2026-08-01): 自动化测试报告 R227 §2 实测历史抽屉 ⚰ 死亡 /
// ⏱ 时间轴渲染原始 UserID UUID,既丑又构成不必要的用户标识符暴露。修复
// 链:buildDeadList*Locked 改用 seatDisplayAccount → 本函数用
// cs.Players[].AgentName 把 bot 昵称升级为 "agent_name #N号" → 与
// GameChatPanel.toRoomPlayers 的命名策略严格对齐("DeepSeek V4-Pro #2号" 而
// 非 "Bot #2号")。
//
// 必须**在** populateAgentNames 之后调用,否则 cs.Players[].AgentName 尚未填充;
// 调用方(BuildClientStateWithRoom 走 r.populateAgentNames)已持 r.mu,
// 因此函数名带 Locked 后缀(§92a 守门)。
func (r *WerewolfRoom) enrichDeadListAccountsLocked(cs *ClientGameState) {
	if cs == nil {
		return
	}
	for i := range cs.AllDeadListVerbose {
		cs.AllDeadListVerbose[i].Account = upgradeDeadAccount(cs, i, cs.AllDeadListVerbose[i].Account)
	}
	for i := range cs.LastNightDeathsVerbose {
		cs.LastNightDeathsVerbose[i].Account = upgradeDeadAccount(cs, cs.LastNightDeathsVerbose[i].Seat, cs.LastNightDeathsVerbose[i].Account)
	}
	if cs.PhaseExtra != nil {
		for i := range cs.PhaseExtra.DeadList {
			cs.PhaseExtra.DeadList[i].Account = upgradeDeadAccount(cs, cs.PhaseExtra.DeadList[i].Seat, cs.PhaseExtra.DeadList[i].Account)
		}
	}
}

// upgradeDeadAccount 把 DeadPlayerJSON.Account 从 seatDisplayAccount 兜底升级为
// 含 agent_name 的完整昵称。seat 越界 / 真人座位 / agent_name 缺失时直接回退
// 兜底值不变。GameChatPanel.toRoomPlayers 是同一策略的"前端实现",本函数为
// "后端实现",保证对局所有通道昵称一致。
//
// bot 判定走 UserID 前缀("bot_"),与 GameChatPanel.toRoomPlayers
// (ClientWeb/src/components/werewolf/GameChatPanel.tsx:41) 完全一致 —
// 单源约定,不依赖 PlayerJSON 新增 IsBot 字段(避免改 wire 格式)。
func upgradeDeadAccount(cs *ClientGameState, seat int, fallback string) string {
	if seat < 0 || seat >= MaxPlayers {
		return fallback
	}
	p := cs.Players[seat]
	if !strings.HasPrefix(p.UserID, "bot_") || p.AgentName == "" {
		return fallback
	}
	return fmt.Sprintf("%s #%d号", p.AgentName, seat+1)
}

// sensitiveToolName 表收口说明(BUG-R226-P1-01, 2026-08-01):
// 原实现两处各维护一份清单(sensitiveToolNames 管"目标参数脱敏",
// publicToolNameForWire 管"工具名抽象化"),新增角色时极易加进 A 表却忘
// 加进 B 表 —— 这正是本缺陷的结构性成因:seer_check/wolf_kill 在
// sensitiveToolNames 里但 publicToolNameForWire 不覆盖,观战者座位卡显示
// `📤 seer_check → [已隐藏]` 可直接锁定预言家/狼人(13 人局胜负核心)。
// 现收口为同一张表:任何 key 自动获得"目标脱敏 + 工具名抽象"双重保护,
// publicToolNameForWire 内部直接查此表。
var sensitiveToolNames = map[string]string{
	"wolf_kill":         "night_act",
	"seer_check":        "night_act",
	"witch_act":         "night_act",
	"witch_act_skip":    "night_act",
	"hunter_shoot":      "day_act",
	// BUG-R213-P3-01 (2026-07-31): 守卫/猎魔人/骑士工具名本身就是身份
	// 信号 —— 观战者/玩家在座位卡看到 `guard_protect → [已隐藏]` 就能
	// 锁定该座位是守卫,直接摧毁 §134 盲守 / §猎魔人 隐身狩猎的博弈
	// 价值。与 wolf_kill/seer_check 同级处理:tool_calls 结果隐藏 +
	// LastDecisionSummary 目标脱敏 + 工具名抽象化(publicToolNameForWire)。
	"guard_protect":     "night_act",
	"demon_hunter_hunt": "night_act",
	"knight_duel":       "day_act",
}

// publicToolNameForWire 把身份敏感工具名改写为对玩家/观战者无身份信号的
// 抽象名(BUG-R213-P3-01, 2026-07-31)。背景:自动化测试报告
// 2026-07-31 05:43:32 §4.3/§8.3 实测观战者座位卡显示 `📤 guard_protect`,
// 即便目标脱敏也足以嗅探"该座位是守卫"。
//
// 仅作用于下发给**非本人**的 BotTranscript(bot 自己在调试页仍可见真实
// 工具名);抽象粒度对齐「wolf_kill/seer_check 也只保留动作名」的既有
// 语义 —— 非敏感工具原样返回。
//
// BUG-R226-P1-01 (2026-08-01): 改查 sensitiveToolNames 单表,确保
// sensitiveToolNames 中**每一个** key 的工具名都被抽象化 ——
// seer_check/wolf_kill/witch_act/hunter_shoot 与 R213 三角色同级。
func publicToolNameForWire(name string) string {
	if abstract, ok := sensitiveToolNames[name]; ok {
		return abstract
	}
	return name
}

// publicDecisionSummaryForWire 把身份敏感工具的 LastDecisionSummary 改写为
// 不含身份信号的抽象文案(BUG-R213-P3-01)。先做目标脱敏(→ [已隐藏]),
// 再把工具名前缀替换为抽象名,避免玩家/观战者从工具名锁定身份。
func publicDecisionSummaryForWire(tool, summary string) string {
	masked := maskSensitiveDecisionTarget(summary)
	pub := publicToolNameForWire(tool)
	if pub == tool {
		return masked
	}
	// summary 形如 "guard_protect → [已隐藏]" / "guard_protect(空守)";
	// 统一前缀替换。
	return strings.Replace(masked, tool, pub, 1)
}

func sanitizeBotTranscript(bt wwplayer.BotTranscript, isSpectator bool) wwplayer.BotTranscript {
	// Drop LLM CoT / role-inference narrative(2026-07-09 §重构 后已经置空,
	// §128 对话即思考重构:LastThinking / FullThinking / RecentMessages 字段已物理删除。
	// Drop upstream HTTP error stacks; reveal nothing about LLM health or
	// proxy address to plain players.
	bt.QuarantineReason = ""
	// R87 P0-3: HeartThought 是 bot 真实内心独白(身份/策略),人类玩家读取等同
	// 全图挂。仅对人类玩家清空;观战者保留(§119 设计契约:internal_thought
	// "仅自己 + 观战者可见")。
	if !isSpectator {
		bt.HeartThought = ""
		bt.HeartThoughtAt = 0
		// BUG-R238-P0-1 (2026-08-04): emotion_reason / emotion_caption /
		// emotion_history[].reason 均为 LLM 自由文本,无任何身份过滤链
		// (对照 HeartThought 走 MysteryMaskText + ScrubIdentityLeak)。
		// 实测 LLM 在 reason 中写出「继续隐藏预言家身份」直接暴露神职,
		// 通过 bot_contexts → WS 推送给人类玩家,破坏隐藏信息机制。
		// emotion / emotion_effect / emotion_intensity 是服务端归一化封闭枚举,
		// 表情本身是公开行为,保留。
		bt.EmotionReason = ""
		bt.EmotionCaption = ""
		for i := range bt.EmotionHistory {
			bt.EmotionHistory[i].Reason = ""
		}
	}
	// Walk tool_calls; drop results for sensitive tools, mask the rest.
	// BUG-R213-P3-01: 身份敏感工具名先经 publicToolNameForWire 抽象化
	// (guard_protect → night_act),再隐藏结果,避免工具名本身泄露身份。
	filtered := make([]string, 0, len(bt.ToolCalls))
	for _, tc := range bt.ToolCalls {
		// Each tool_call string has format "tool_name: result". The name
		// is everything before the first ": ".
		name := tc
		if idx := strings.Index(tc, ": "); idx >= 0 {
			name = tc[:idx]
		}
		if _, sensitive := sensitiveToolNames[name]; sensitive {
			// Replace with a non-leaking placeholder.
			filtered = append(filtered, publicToolNameForWire(name)+": [已隐藏]")
			continue
		}
		filtered = append(filtered, tc)
	}
	bt.ToolCalls = filtered
	// 2026-07-09 §重构 - 新字段脱敏:LastToolInput 是 JSON 字符串,
	// 解析后按 sensitiveToolInputs 替换敏感字段。简化:用 Re-parse
	// SanitizeToolInput 的同款逻辑(再调一次)。
	if bt.LastToolInput != "" {
		bt.LastToolInput = reSanitizeToolInput(bt.LastTool, bt.LastToolInput)
	}
	// R87 P0-1: LastDecisionSummary 含敏感工具( wolf_kill / seer_check /
	// witch_act / hunter_shoot )的具体目标("wolf_kill → 7号"),观战者可见
	// 等于开全图。对敏感工具统一脱敏:保留动作名,目标替换为 [已隐藏]。
	// BUG-R213-P3-01: 守卫/猎魔人/骑士进一步把工具名抽象化
	// (guard_protect → night_act),杜绝"工具名=身份"侧信道。
	if _, sensitive := sensitiveToolNames[bt.LastTool]; sensitive {
		bt.LastDecisionSummary = publicDecisionSummaryForWire(bt.LastTool, bt.LastDecisionSummary)
		bt.LastTool = publicToolNameForWire(bt.LastTool)
	}
	// DecisionInputs / LastToolResult / LastOutcome 是数字摘要 + 动作摘要,
	// 不暴露具体身份,无需进一步脱敏。

	// 2026-08-10 §20260810-07 — 多假说并行推演(§119/§135):
	// 1) 玩家侧(非 spectator)永远不收假说视图 → 清空 HypothesisSummary 字段;
	// 2) 同时把 LastDecisionSummary 末尾的「📊 [...]」JSON 段剥离 — 任何观众
	//    都不能直接拿到 LLM 自由拼装的 JSON(避免 LLM 编造出非合规字段).
	if !isSpectator {
		bt.HypothesisSummary = nil
	}
	bt.LastDecisionSummary = StripFromDecisionSummary(bt.LastDecisionSummary)

	// 2026-08-10 §20260810-12 D1 — 决策留痕运行时回放(§119/§135):
	// 玩家侧清空 DecisionTrail;DecisionEntry 不直接揭晓身份但阶段/工具名组合
	// 可推断玩家是否狼人/守卫/女巫,故与 HypothesisSummary 同样按 spectator 隔离。
	if !isSpectator {
		bt.DecisionTrail = nil
	}

	// §20260811-06 U3 — 公开推理链(reasoning_chain)同样按 spectator 隔离。
	// ReasoningChains 含 topic / steps / evidence / conclusion,LLM 自由文本中
	// 可能泄露自己/他人的真实身份(§135 红线)。玩家分支清空。
	if !isSpectator {
		bt.ReasoningChains = nil
	}

	return bt
}

// populateHypothesisSummary 把 bot.LastDecisionSummary 末尾的「📊 [...]」段解析后:
//   1) 写入房间级 HypothesisStore(影响 buildAgentContextLocked 的下一次 LLM prompt);
//   2) 写到 bt.HypothesisSummary[] 供前端 HistoryDrawer 第 5 sub-tab「🔮 假说」渲染。
//
// **§119 协议层隔离**:解析失败静默,不写入,不计入 consecutiveFailures(§120 公平性)。
// **调用前置**:必须已持 r.mu(§92a),且 bt 为非 nil。
func populateHypothesisSummary(r *WerewolfRoom, bt *wwplayer.BotTranscript) {
	if r == nil || bt == nil {
		return
	}
	summary := bt.LastDecisionSummary
	matches := hypothesisSummaryRe.FindStringSubmatch(summary)
	if len(matches) < 2 {
		return
	}
	var entries []HypothesisEntry
	if err := json.Unmarshal([]byte(matches[1]), &entries); err != nil {
		return
	}
	for i := range entries {
		if entries[i].Confidence < 0 {
			entries[i].Confidence = 0
		}
		if entries[i].Confidence > 100 {
			entries[i].Confidence = 100
		}
		entries[i].Supporting = truncateHypothesisText(entries[i].Supporting, 40)
		entries[i].Refuting = truncateHypothesisText(entries[i].Refuting, 40)
	}
	// 写回 HypothesisStore,后续 buildAgentContextLocked 会读到。
	if r.State != nil {
		r.hypothesisStoreLocked().UpdateFromDecisionSummary(
			bt.Seat, r.State.DayNumber, summary)
	}
	// 把解析结果转成对外 wire 类型,挂到 BotTranscript。
	out := make([]wwplayer.HypothesisEntryJSON, 0, len(entries))
	for _, e := range entries {
		out = append(out, wwplayer.HypothesisEntryJSON{
			TargetSeat: e.TargetSeat,
			RoleGuess:  e.RoleGuess,
			Confidence: e.Confidence,
			Supporting: e.Supporting,
			Refuting:   e.Refuting,
			UpdatedAt:  e.UpdatedAt,
		})
	}
	bt.HypothesisSummary = out
}

var sensitiveDecisionTargetRe = regexp.MustCompile(`→ \d+号`)


func maskSensitiveDecisionTarget(summary string) string {
	if summary == "" {
		return summary
	}
	return sensitiveDecisionTargetRe.ReplaceAllString(summary, "→ [已隐藏]")
}

func reSanitizeToolInput(toolName, toolInputJSON string) string {
	if toolInputJSON == "" {
		return ""
	}
	// 解析
	var parsed map[string]any
	if err := json.Unmarshal([]byte(toolInputJSON), &parsed); err != nil {
		// 解析失败原样返回(已脱敏过的 input 不应回不来)
		return toolInputJSON
	}
	// 调原版 SanitizeToolInput(等价于敏感表过滤后再次序列化)
	return wwplayer.SanitizeToolInput(toolName, parsed)
}

func (m *WerewolfManager) Seats(roomID string) ([MaxPlayers]string, bool) {
	r := m.getRoom(roomID)
	if r == nil {
		return [MaxPlayers]string{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Seats, true
}

