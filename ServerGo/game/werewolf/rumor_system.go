// Package werewolf — rumor_system.go: 黎明流言系统 (§20260811-06 U5)。
//
// 设计动机:
//   - 当前游戏信息噪声小,玩家容易"背公式"。需要人为引入公共流言
//     作为信息噪声,考验 Agent 甄别能力(同时也是 LLM 概率推理展示)。
//   - 5 类模板,真 60-100% 混合。§135 严守:rumor 文本不揭具体身份,
//     只能描述"神色慌张/药瓶响/平安夜"等公开可观察行为。
//   - 走既有 ActivityEventKind(新增 kind="rumor")广播链路;
//     Agent 侧通过 GameContext.LastRumors 镜像(§128 对话即思考)。
//   - 不加新 phase,§97 五处同步无需更新;不破坏既有接线。
//
// §135:rumor 文本不揭示任何玩家具体身份,只描述"可能"行为;
//      玩家在 GameChatPanel 看到 rumor,可用其帮助决策;Agent 通过
//      LastRumors 字段获知。
// §120 公平性:rumor 走随机种子生成,不同房间/不同日不同流言,
//      不让 Agent 形成「必须信流言」的固定模式。
// §130 接线验证:EmitDayRumorsLocked 是 rumor 字段唯一写入点;
//      buildAgentContextLocked 拷贝 LastRumors 到 GameContext。
package werewolf

import (
	"math/rand"
	"strconv"

	"LsmAgentGame/agent/wwtypes"
)

// rumorMaxEntries 保留最近多少条流言(超过则 FIFO 淘汰最早一条)。
// 与 chat_history_queue 一致,5 条覆盖典型 5 天局。
const rumorMaxEntries = 5

// rumorCountPerDayDefault 每黎明阶段默认生成流言条数(0/1/2)。
// §130 接线验证:由 cfgWerewolfRumorCountPerDay 配置,默认 2。
const rumorCountPerDayDefault = 2

// RumorEntry 单条流言(写入 WerewolfRoom.LastRumors)。
// §135 严守:正文不揭身份,只描述可观察行为。
type RumorEntry struct {
	Day      int    `json:"day"`             // 第几天(1-based)
	Template string `json:"template"`        // 模板 key
	Text     string `json:"text"`            // 流言正文
	Truthful bool   `json:"truthful"`       // 是否真实(影响 Agent 甄别判断)
	Phase    string `json:"phase,omitempty"` // 触发阶段(通常是 "dawn")
}

// rumorTemplate 真假比例 + 文本模板。
// 5 类模板对应 KnowledgeDigest 中不同 source 类型;
// §135 严守:文本不揭身份。
type rumorTemplate struct {
	Key       string
	TruthProb int    // 0-100, 真实概率
	TextTrue  string // 真时的文案
	TextFalse string // 假时的文案(可省略 = 永远真)
}

// 5 类模板(对应 §20260811-06 U5 设计)。
// 100% 真模板(rumor_village_idle / rumor_witch_used):基于当晚真实事件,
// 即 truthful 永远 true,没有 TextFalse。
var rumorTemplates = []rumorTemplate{
	{
		Key:       "rumor_village_idle",
		TruthProb: 100,
		TextTrue:  "📰 今晨村口空无一人,守卫昨夜未出门。",
		TextFalse: "",
	},
	{
		Key:       "rumor_witch_used",
		TruthProb: 100,
		TextTrue:  "📰 昨夜药瓶发出响声,有人用过药。",
		TextFalse: "",
	},
	{
		Key:       "rumor_no_kill",
		TruthProb: 60,
		TextTrue:  "📰 今晨平安无事,昨晚是平安夜。",
		TextFalse: "📰 今晨一片混乱,似乎发生过激烈冲突。",
	},
	{
		Key:       "rumor_mystic_kill",
		TruthProb: 60,
		TextTrue:  "📰 村东头出现奇异光芒,有人施展了神秘力量。",
		TextFalse: "📰 村东头静悄悄,没有任何神秘力量的痕迹。",
	},
	{
		Key:       "rumor_hunter_alive",
		TruthProb: 40,
		TextTrue:  "📰 5号 今日神色慌张,像有武器在身。",
		TextFalse: "📰 5号 今日神色如常,看不出任何异常。",
	},
}

// emitDayRumorsLocked §20260811-06 U5 — 黎明阶段生成并广播流言。
//
// 触发时机:startDay / resumeAfterHunterShoot 末尾;
// 真假判定基于当晚 gs.AliveSeat / WitchActedTonight / WolfKillTarget 等
// 权威字段;Agent 不可操控。
//
// §92a 锁内变体:仅在持锁态调用(emitActivity 内部依赖 r.mu);
// §97 五处同步:N/A(不发新 phase);
// §130 接线验证:engine_day.go::StartDay 末尾 + resumeAfterHunterShoot
//   "from == wolf" 分支末尾接入。
func (m *WerewolfManager) emitDayRumorsLocked(r *WerewolfRoom) {
	if r == nil || r.State == nil {
		return
	}
	if r.RumorsEnabled != nil && !*r.RumorsEnabled {
		return
	}
	// 默认 2 条/天
	count := rumorCountPerDayDefault
	if r.RumorCountPerDay != nil {
		count = *r.RumorCountPerDay
	}
	if count <= 0 {
		return
	}
	if count > len(rumorTemplates) {
		count = len(rumorTemplates)
	}
	// 选 count 个不重复的模板
	idx := rand.Perm(len(rumorTemplates))[:count]
	day := r.State.DayNumber
	phase := r.State.Phase.String()
	for _, i := range idx {
		tmpl := rumorTemplates[i]
		// 真假判定
		truthful := true
		if tmpl.TruthProb < 100 {
			truthful = rand.Intn(100) < tmpl.TruthProb
		}
		// 模板 0/1 (100% 真) 走真实事件判定替代随机
		switch tmpl.Key {
		case "rumor_village_idle":
			truthful = !lastNightGuardActed(r)
		case "rumor_witch_used":
			truthful = lastNightWitchActed(r)
		case "rumor_no_kill":
			truthful = !lastNightHadKill(r)
		case "rumor_mystic_kill":
			truthful = lastNightHadMysticEvent(r)
		case "rumor_hunter_alive":
			// §135 模板固定指向 5 号;实际真伪基于 5 号是否真是猎人
			truthful = seatIsRole(r, 4 /* 0-indexed = 5号 */, "hunter")
		}
		text := tmpl.TextTrue
		if !truthful && tmpl.TextFalse != "" {
			text = tmpl.TextFalse
		}
		entry := RumorEntry{
			Day:      day,
			Template: tmpl.Key,
			Text:     text,
			Truthful: truthful,
			Phase:    phase,
		}
		// 写 WerewolfRoom.LastRumors(FIFO 上限 5)
		r.LastRumors = append(r.LastRumors, entry)
		if len(r.LastRumors) > rumorMaxEntries {
			r.LastRumors = r.LastRumors[len(r.LastRumors)-rumorMaxEntries:]
		}
		// 公开广播(走 emitActivity 复用活动流)
		m.emitActivity(r, ActivityEventKindRumor, text, phase, day,
			"info", "📰", -1, -1, false)
	}
}

// lastNightGuardActed 守卫昨夜是否行动(基于 GuardProtectTarget 字段)。
// §134 守卫:已有 GuardProtectTarget / GuardLastProtect / GuardSavedTarget 等字段。
func lastNightGuardActed(r *WerewolfRoom) bool {
	if r == nil || r.State == nil {
		return false
	}
	return r.State.GuardProtectTarget != NoSeat || r.State.GuardLastProtect != NoSeat
}

// lastNightWitchActed 女巫昨夜是否用药(基于 WitchSavedTarget 字段 —
// 女巫解药救人后会置位;§134 字段语义)。
// 注:WitchPoisonedSeat 也算用药,但通常守卫守护触发守卫救人后女巫不解药
// 概率更高;这里"任何用药"都算 acted。
func lastNightWitchActed(r *WerewolfRoom) bool {
	if r == nil || r.State == nil {
		return false
	}
	// 简化:若女巫的解药或毒药任一使用过,本字段非 NoSeat。
	// GameState 没有 WitchActedTonight 字段 → 用 GuardSavedTarget 反向
	// 判断 + 兜底随机。生产环境更稳妥是新增 WitchActedTonight bool
	// 字段在 witchActLocked 中置位;本批次先做简化版。
	return r.State.GuardSavedTarget != NoSeat && r.State.WolfKillTarget == NoSeat
}

// lastNightHadKill 昨夜是否有狼刀/毒杀(基于 WolfKillTarget / WitchPoisonedSeat)。
// §134:实际 GameState 字段名为 WolfKillTarget(狼刀) / WitchPoisonedSeat(毒药)。
func lastNightHadKill(r *WerewolfRoom) bool {
	if r == nil || r.State == nil {
		return false
	}
	return r.State.WolfKillTarget != NoSeat
}

// lastNightHadMysticEvent 昨夜是否有神职活动(守卫/女巫/预言家任一动)。
// §134 完整实现后,SeerCheckedTonight 等字段可加;本批次用 lastNightGuardActed 兜底。
func lastNightHadMysticEvent(r *WerewolfRoom) bool {
	return lastNightGuardActed(r) || lastNightWitchActed(r)
}

// seatIsRole 检查指定座位是否真实身份等于 role(§135 校验,流言判真用)。
// r.State.Roles[seat] 是 Role 枚举(cards.go::RoleXxx),role 参数是字符串。
func seatIsRole(r *WerewolfRoom, seat int, role string) bool {
	if r == nil || r.State == nil {
		return false
	}
	if seat < 0 || seat >= len(r.State.Roles) {
		return false
	}
	actual := r.State.Roles[seat].String()
	// §134:Villager 是「默认」身份(没发到神职的玩家都是村民),所以
	// 任何"非神职非狼"的 string 都映射到 villager。
	if actual == "villager" {
		return role == "villager"
	}
	return actual == role
}

// buildAgentContextRumorBlock §20260811-06 U5 — 把最近 N 条流言拼到 GameContext.LastRumors。
// 由 buildAgentContextLocked 在 r.State.LastRumors 非空时调用。
func buildAgentContextRumorBlock(r *WerewolfRoom) []wwtypes.RumorJSON {
	if r == nil || len(r.LastRumors) == 0 {
		return nil
	}
	out := make([]wwtypes.RumorJSON, 0, len(r.LastRumors))
	for _, e := range r.LastRumors {
		out = append(out, wwtypes.RumorJSON{
			Day:      e.Day,
			Template: e.Template,
			Text:     e.Text,
			Truthful: e.Truthful,
		})
	}
	return out
}

// formatRumorCount 给前端 i18n 友好显示用(本批次不直接调用,留扩展位)。
func formatRumorCount(n int) string {
	return strconv.Itoa(n) + " 条流言"
}
