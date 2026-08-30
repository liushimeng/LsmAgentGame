package werewolf

// 2026-08-10 §20260810-05 — 信息账本(Information Ledger)一期。
//
// 设计文档:docs/狼人杀-Agent与系统/狼人杀13人局Agent升级-20260810-05.md
//
// 核心思想:把「谁知道什么」从散落在十几个字段/通道的隐式状态,收敛为
// 单一数据结构 InformationLedger —— 每条信息记 {事实, 知情座位集合, 获得时刻, 来源类型}。
// 一期「只写不读」:账本仅做记录 + 断言 + 观战者脱敏快照下发,
// 不改变 buildAgentContextLocked 的 prompt 组装逻辑(二期再切)。
//
// 硬约束对齐:
//   - §92a: 本结构全部方法为 *Locked 语义,自身不加锁;caller 必须持有 r.mu。
//   - §111: 单源 entries + 知情集合 map,杜绝 per-seat fan-out 内存爆炸。
//   - §119/§135: Fact 入帐前经 redactLedgerFact 剔除身份明文;账本不进
//     chat_message / chat_history / BotTranscript;纯内存,随房间 GC 回收。
//   - §130: 每个 InfoSource 常量在 information_ledger_test.go 断言至少一个写入点。

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

// InfoSource 信息来源类型(封闭枚举,禁止自由字符串)。
type InfoSource string

const (
	InfoSourcePublicSpeech  InfoSource = "public_speech"   // 公开发言/插话
	InfoSourceWhisper       InfoSource = "whisper"         // 私聊
	InfoSourceWolfPack      InfoSource = "wolf_pack"       // 狼队密语
	InfoSourceNightSeer     InfoSource = "night_seer"      // 预言家查验结果
	InfoSourceNightWitch    InfoSource = "night_witch"     // 女巫见刀/用药
	InfoSourceNightGuard    InfoSource = "night_guard"     // 守卫守护
	InfoSourceNightWolfVote InfoSource = "night_wolf_vote" // 狼刀投票(含理由)
	InfoSourceDayVoteMap    InfoSource = "day_vote_map"    // 白天票型(谁投了谁)
	InfoSourceSheriffStream InfoSource = "sheriff_stream"  // 警徽流结算
	InfoSourceSheriffElect  InfoSource = "sheriff_elect"   // 警长当选
	InfoSourcePropInject    InfoSource = "prop_inject"     // 道具注入文本
	InfoSourceDeathEvent    InfoSource = "death_event"     // 死亡事件(不含身份)
	InfoSourceHunterShot    InfoSource = "hunter_shot"     // 猎人开枪(公开)
	InfoSourceKnightDuel    InfoSource = "knight_duel"     // 骑士决斗(公开)
	InfoSourceIdiotReveal   InfoSource = "idiot_reveal"    // 白痴翻牌(公开)
	InfoSourceDemonHunter   InfoSource = "demon_hunter"    // 猎魔人狩猎(公开)
	InfoSourceRoleDeal      InfoSource = "role_deal"       // 开局发牌(仅本人)
	// §20260830-02 — 自爆带走(全房公开事件;与 hunter_shot 同级公开)。
	InfoSourceSuicideTake InfoSource = "suicide_take"
)

// AllInfoSources 返回全部来源常量(注册表),供测试断言「每个来源至少一个写入点」。
func AllInfoSources() []InfoSource {
	return []InfoSource{
		InfoSourcePublicSpeech, InfoSourceWhisper, InfoSourceWolfPack,
		InfoSourceNightSeer, InfoSourceNightWitch, InfoSourceNightGuard,
		InfoSourceNightWolfVote, InfoSourceDayVoteMap, InfoSourceSheriffStream,
		InfoSourceSheriffElect, InfoSourcePropInject, InfoSourceDeathEvent,
		InfoSourceHunterShot, InfoSourceKnightDuel, InfoSourceIdiotReveal,
		InfoSourceDemonHunter, InfoSourceRoleDeal,
	}
}

// informationLedgerCap 环形容量:13 人局单局信息条数实测上限 ~250,留 60% 余量。
const informationLedgerCap = 400

// ledgerFactMaxRunes 单条 Fact 文本最大 rune 数(截断防超长发言撑爆内存)。
const ledgerFactMaxRunes = 120

// ledgerDigestMaxSources 控制单个 bot prompt 中最多展示的信息来源组数。
const ledgerDigestMaxSources = 6

// ledgerDigestHighlightMaxRunes 控制每条知情要点的二次截断长度。
const ledgerDigestHighlightMaxRunes = 60

// SeatDigestEntry 是某座位按来源聚合后的知情摘要。
// 2026-08-10 §20260810-08 信息账本二期 —— 行为侧消费链路。
type SeatDigestEntry struct {
	Source     InfoSource
	Count      int
	LastRound  int
	Highlights []string
}

// SeatDigest 是某座位的知情清单派生视图，不新增 per-seat 持久存储。
type SeatDigest struct {
	Seat        int
	TotalKnown  int
	TotalInRoom int
	Entries     []SeatDigestEntry
}

// InfoEntry 一条信息账本记录:某事实 + 知情座位集合 + 获得时刻 + 来源。
// KnowerSeats 采用 map[int]bool(而非 []int)以 O(1) 支持 Knows(seat) 判定。
type InfoEntry struct {
	Seq         int64        `json:"seq"`
	Round       int          `json:"round"`
	Phase       string       `json:"phase"`
	Source      InfoSource   `json:"source"`
	Fact        string       `json:"fact"`
	KnowerSeats map[int]bool `json:"-"` // JSON 输出走 InfoEntryJSON(有序数组)
	TS          int64        `json:"ts"`
}

// InfoEntryJSON 是 InfoEntry 的脱敏/有序 JSON 投影(观战者快照用)。
// KnowerSeats 序列化为排序后的 []int,避免 map 序随机导致前端渲染抖动。
type InfoEntryJSON struct {
	Seq         int64      `json:"seq"`
	Round       int        `json:"round"`
	Phase       string     `json:"phase"`
	Source      InfoSource `json:"source"`
	Fact        string     `json:"fact"`
	KnowerSeats []int      `json:"knower_seats"`
	TS          int64      `json:"ts"`
}

// InformationLedger 房间级信息账本。
// 所有方法均为 *Locked 语义:调用方必须已持有 r.mu(§92a),本结构自身不再加锁。
type InformationLedger struct {
	seq     int64
	entries []InfoEntry
}

func NewInformationLedger() *InformationLedger {
	return &InformationLedger{entries: make([]InfoEntry, 0, 64)}
}

// append 追加一条记录。Fact 在入帐前经 redactLedgerFact + rune 截断。
// knowerSeats 传入 nil/空时视为「无明确知情者」(合法但无意义,调用方应避免)。
func (l *InformationLedger) append(round int, phase string, source InfoSource, fact string, knowerSeats map[int]bool, ts int64) {
	if l == nil {
		return
	}
	l.seq++
	entry := InfoEntry{
		Seq:         l.seq,
		Round:       round,
		Phase:       phase,
		Source:      source,
		Fact:        truncateRunes(redactLedgerFact(fact), ledgerFactMaxRunes),
		KnowerSeats: knowerSeats,
		TS:          ts,
	}
	l.entries = append(l.entries, entry)
	if len(l.entries) > informationLedgerCap {
		l.entries = l.entries[len(l.entries)-informationLedgerCap:]
	}
}

// Knows 判定某座位是否对「最近一条匹配 source 的账本条目」知情。
// 供开发期断言与二期「说漏嘴检测」使用。
func (l *InformationLedger) Knows(seat int, source InfoSource) bool {
	if l == nil {
		return false
	}
	for i := len(l.entries) - 1; i >= 0; i-- {
		if l.entries[i].Source == source {
			return l.entries[i].KnowerSeats[seat]
		}
	}
	return false
}

// AssertKnows 与 Knows 同义,显式命名供测试断言可读性(§20260810-05 §2.4)。
func (l *InformationLedger) AssertKnows(seat int, source InfoSource) bool {
	return l.Knows(seat, source)
}

// Len 返回当前账本条目数(测试与调试观测用)。
func (l *InformationLedger) Len() int {
	if l == nil {
		return 0
	}
	return len(l.entries)
}

// entriesSnapshot 返回账本条目的只读切片快照。
// 2026-08-10 §20260810-08：仅供纯派生计算使用；caller 必须持 r.mu，
// 不得修改返回切片或其中的 KnowerSeats。
func (l *InformationLedger) entriesSnapshot() []InfoEntry {
	if l == nil {
		return nil
	}
	return l.entries
}

// DigestForSeat 聚合某座位知情的全部条目。
// 2026-08-10 §20260810-08：本方法为 *Locked 语义，自身不加锁；
// caller 必须持 r.mu。每组保留最近 maxHighlights 条要点，最多返回 6 组。
func (l *InformationLedger) DigestForSeat(seat int, maxHighlights int) *SeatDigest {
	if l == nil || len(l.entries) == 0 || seat < 0 || seat >= MaxPlayers {
		return nil
	}
	if maxHighlights <= 0 {
		maxHighlights = 2
	}
	groups := make(map[InfoSource]*SeatDigestEntry)
	digest := &SeatDigest{Seat: seat, TotalInRoom: len(l.entries)}
	for _, e := range l.entries {
		if !e.KnowerSeats[seat] {
			continue
		}
		digest.TotalKnown++
		group := groups[e.Source]
		if group == nil {
			group = &SeatDigestEntry{Source: e.Source}
			groups[e.Source] = group
		}
		group.Count++
		group.LastRound = e.Round
		group.Highlights = append(group.Highlights, truncateRunes(e.Fact, ledgerDigestHighlightMaxRunes))
		if len(group.Highlights) > maxHighlights {
			group.Highlights = group.Highlights[len(group.Highlights)-maxHighlights:]
		}
	}
	if digest.TotalKnown == 0 {
		return nil
	}
	for _, group := range groups {
		digest.Entries = append(digest.Entries, *group)
	}
	sort.Slice(digest.Entries, func(i, j int) bool {
		if digest.Entries[i].Count != digest.Entries[j].Count {
			return digest.Entries[i].Count > digest.Entries[j].Count
		}
		return digest.Entries[i].Source < digest.Entries[j].Source
	})
	if len(digest.Entries) > ledgerDigestMaxSources {
		digest.Entries = digest.Entries[:ledgerDigestMaxSources]
	}
	return digest
}

// SnapshotJSON 输出脱敏后的有序 JSON 投影(观战者快照用)。
// 一期不做 per-seat 过滤——观战者享有上帝视角数据(HeartThought/WolfPack 判例已在
// HistoryDrawer 存在);身份明文已在写入侧 redact,读取侧无需二次脱敏(§135 单点)。
func (l *InformationLedger) SnapshotJSON(limit int) []InfoEntryJSON {
	if l == nil || len(l.entries) == 0 {
		return nil
	}
	entries := l.entries
	if limit > 0 && len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}
	out := make([]InfoEntryJSON, 0, len(entries))
	for _, e := range entries {
		seats := make([]int, 0, len(e.KnowerSeats))
		for s := range e.KnowerSeats {
			seats = append(seats, s)
		}
		sort.Ints(seats)
		out = append(out, InfoEntryJSON{
			Seq:         e.Seq,
			Round:       e.Round,
			Phase:       e.Phase,
			Source:      e.Source,
			Fact:        e.Fact,
			KnowerSeats: seats,
			TS:          e.TS,
		})
	}
	return out
}

// structuredFactPrefixes 是「机器可读」账本 fact 的前缀白名单(§20260811-08 P0 修复)。
//
// 这些 fact 由 fmt.Sprintf 以固定 `<prefix> k=v k=v` 格式写入,下游由
// view_godmode.go 的 parseSeatTargetPair / parseWitchTriple / parseSeatTargetHitWolf
// 等 Sscanf 解析。前缀 token 本身**必须逐字节保留**,否则解析全部失败。
var structuredFactPrefixes = []string{
	"seer_check", "guard_protect", "witch_act", "hunter_shot",
	"knight_duel", "demon_hunter", "idiot_reveal", "day_vote",
	"wolf_vote", "wolf_pack", "wolf_role_assign", "role_deal",
	"sheriff_elect", "sheriff_stream", "prop_inject",
}

// isASCIIWordChar 判断是否为 ASCII 标识符字符([A-Za-z0-9_])。
func isASCIIWordChar(b byte) bool {
	return b == '_' ||
		(b >= '0' && b <= '9') ||
		(b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z')
}

// redactLedgerFact 剔除身份明文(§119/§135):账本只记「信息流动」,
// 不记「身份结论」。凡是直接点名「N号是狼人/预言家/女巫…」的文本,
// 统一替换角色词为占位符,防止账本成为绕过 §135 的第 5 条身份下发通道。
//
// §20260811-08 P0 修复 —— 旧版对整条 fact 做无边界的 strings.ReplaceAll,把
// **结构化 fact 的机器可读前缀一并打成了 ▪**:
//
//	"seer_check seat=1 target=4"   → "▪_check seat=1 target=4"
//	"guard_protect seat=3 target=1" → "▪_protect seat=3 target=1"
//	"knight_duel ... hit_wolf=true" → "▪_duel ... hit_▪=true"
//
// 后果:§20260810-09 上帝视角的 SeerChecks / WitchDecisions / GuardProtects
// 三个聚合字段自落地起 **100% 恒为空**(Sscanf 前缀匹配全部失败),观战面板
// 的夜间行动历史从未显示过任何数据。该缺陷由 §20260811-08 U3 的回归测试
// 意外暴露 —— 原实现没有任何测试断言聚合结果非空,只测了 redact 本身。
//
// 修复采用两条互补规则:
//  1. **结构化前缀白名单**:fact 首 token 命中 structuredFactPrefixes 时逐字节保留,
//     只对其后的参数区做脱敏(参数区仍可能含 reason= / text= 等自由文本)。
//  2. **ASCII 词边界**:角色词左右紧邻 [A-Za-z0-9_] 时不替换,保护 hit_wolf、
//     wolf_pack 一类 snake_case 键名。中文角色词不受影响(中文非 ASCII 词字符),
//     故「3 号是狼人」「我是预言家」等自由文本照常脱敏。
func redactLedgerFact(s string) string {
	if s == "" {
		return s
	}
	// 规则 1:保留结构化前缀 token。
	head := ""
	rest := s
	if i := strings.IndexByte(s, ' '); i > 0 {
		for _, p := range structuredFactPrefixes {
			if s[:i] == p {
				head, rest = s[:i+1], s[i+1:]
				break
			}
		}
	}
	// 身份词表(中文 + 英文 role key)。替换为占位符,保留「有一条信息」的语义。
	roleWords := []string{
		"狼人", "预言家", "女巫", "猎人", "守卫", "白痴", "骑士", "猎魔人", "平民",
		"werewolf", "seer", "witch", "hunter", "guard", "idiot", "knight",
		"demon_hunter", "villager", "wolf",
	}
	for _, w := range roleWords {
		rest = replaceWordBoundary(rest, w, "▪")
	}
	return head + rest
}

// replaceWordBoundary 替换 old→new,但跳过左右紧邻 ASCII 标识符字符的命中
// (规则 2)。这样 "hit_wolf" / "wolf_pack" 的键名不被破坏,而自由文本里的
// "wolf" / "狼人" 仍被脱敏。
func replaceWordBoundary(s, old, new string) string {
	if old == "" || !strings.Contains(s, old) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if strings.HasPrefix(s[i:], old) {
			leftOK := i == 0 || !isASCIIWordChar(s[i-1])
			endIdx := i + len(old)
			rightOK := endIdx >= len(s) || !isASCIIWordChar(s[endIdx])
			if leftOK && rightOK {
				b.WriteString(new)
				i = endIdx
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// truncateRunes rune 安全截断(复用 rune 计数,避免按字节切坏多字节字符)。
func truncateRunes(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "…"
}

// aliveKnowerSet 构造「当前全部存活座位」的知情集合(公开信息用)。
// caller 必须持 r.mu。
func aliveKnowerSetLocked(r *WerewolfRoom) map[int]bool {
	set := make(map[int]bool, MaxPlayers)
	if r == nil || r.State == nil {
		return set
	}
	for i := 0; i < MaxPlayers; i++ {
		if r.State.AliveSeat(Seat(i)) {
			set[i] = true
		}
	}
	return set
}

// aliveWolfKnowerSet 构造「当前全部存活狼座位」的知情集合(狼队频道用)。
// caller 必须持 r.mu。
func aliveWolfKnowerSetLocked(r *WerewolfRoom) map[int]bool {
	set := make(map[int]bool, 4)
	if r == nil || r.State == nil {
		return set
	}
	for i := 0; i < MaxPlayers; i++ {
		if r.State.AliveSeat(Seat(i)) && r.State.Roles[i] == RoleWerewolf {
			set[i] = true
		}
	}
	return set
}

// singleKnowerSet 构造单座位知情集合。
func singleKnowerSet(seat int) map[int]bool {
	if seat < 0 {
		return map[int]bool{}
	}
	return map[int]bool{seat: true}
}

// pairKnowerSet 构造双座位知情集合(whisper 收发双方)。
func pairKnowerSet(a, b int) map[int]bool {
	set := make(map[int]bool, 2)
	if a >= 0 {
		set[a] = true
	}
	if b >= 0 {
		set[b] = true
	}
	return set
}

// ---------------------------------------------------------------------------
// WerewolfRoom 接入层(以下方法 caller 必须持 r.mu —— §92a)
// ---------------------------------------------------------------------------

// ledgerLocked 返回房间信息账本,懒初始化。与 wolfPack 同模式,
// 避免 6 处 &WerewolfRoom{} 字面量构造点同步遗漏(§130「声明了却从不接线」防线)。
func (r *WerewolfRoom) ledgerLocked() *InformationLedger {
	if r.infoLedger == nil {
		r.infoLedger = NewInformationLedger()
	}
	return r.infoLedger
}

// ledgerRoundPhaseLocked 取当前 round/phase 快照(账本条目元信息)。
func (r *WerewolfRoom) ledgerRoundPhaseLocked() (int, string) {
	if r == nil || r.State == nil {
		return 0, ""
	}
	return r.State.DayNumber, r.State.Phase.String()
}

// ledgerAppendLocked 是全部接入点的统一入口。knowers 语义见各调用点。
func (r *WerewolfRoom) ledgerAppendLocked(source InfoSource, fact string, knowers map[int]bool, ts int64) {
	if r == nil {
		return
	}
	round, phase := r.ledgerRoundPhaseLocked()
	r.ledgerLocked().append(round, phase, source, fact, knowers, ts)
}

// ledgerRegisterRoleDealLocked 在 StartGame 发牌成功后登记每座位的 role_deal
// 条目(仅本人知情,§134/§135 身份隔离)。所有 StartGame 成功路径
// (room_manage / room_agent / room_action / room_restart_vote)必须调用本函数 ——
// 收敛为单点防止「某条启动路径漏登记」(§130 防线)。caller 必须持 r.mu。
func (r *WerewolfRoom) ledgerRegisterRoleDealLocked() {
	if r == nil || r.State == nil {
		return
	}
	nowMs := time.Now().UnixMilli()
	for i := 0; i < MaxPlayers; i++ {
		if r.State.Seats[i] == "" {
			continue
		}
		r.ledgerAppendLocked(InfoSourceRoleDeal,
			"role_deal seat="+strconv.Itoa(i),
			singleKnowerSet(i), nowMs)
	}
}
