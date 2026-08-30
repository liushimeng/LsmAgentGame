// Package werewolf — wolfpack_cipher.go: 狼队暗号系统 CipherProtocol（§20260811-04 U1）。
//
// 设计动机（Agent-Surpport-01.md §3.7 暗号系统 / Gemini §三.3）：
//   - 已落地的 wolf_whisper 工具让狼队可以在 GameContext 中互通「今晚刀 X」。
//   - 但狼队 GameContext 的 wolfpack 历史在观战者第一视角（§20260810-11 V1）下完全可见。
//   - 暗号系统让狼队可以把敏感行动编码到「公开的、带修辞外衣的发言」中,
//     队友解析、暗线对手看不懂——这是真实狼人杀的核心博弈能力。
//
// 4 种基础暗号模板（仅编码"今晚行动"的二元或三元决策，不编码完整长文本）：
//   - target_position    公屏发言中提到「3号/第3个位置/顺位3」类词汇
//   - sentiment_word     用一个当日约定的关键词（"清爽"）正面/负面使用
//   - vote_target        投票前的发言里以「我倾向 X」形式表态
//   - fake_seer_posture  悍跳位在发言中刻意使用「查/验/金水」类词汇的密度
//
// §119 协议层隔离（与 §133 WolfPackRoom msgs 同款）：
//   - CipherBundle 不写 chat_message / chat_history / BotTranscript.HeartThought。
//   - 仅狼 bot 的 GameContext 注入。
//   - 玩家页不显示暗号 UI；观战者页可看（§119 spectator 视角聚合）。
//
// §92a 锁约束：所有方法为 *Locked 语义；调用方持 r.mu。
// §128 对话即思考：仅承载运行时中转，不增加 wire 字段。
package werewolf

import (
	"LsmAgentGame/agent/wwplayer"
	"LsmAgentGame/agent/wwtypes"
	"sort"
	"strconv"
	"strings"
)

// CipherTemplateKey 是暗号模板的预定义 key（防 LLM/前端拼写漂移）。
const (
	CipherKeyTargetPosition  = "target_position"
	CipherKeySentimentWord   = "sentiment_word"
	CipherKeyVoteTarget      = "vote_target"
	CipherKeyFakeSeerPosture = "fake_seer_posture"
)

// CipherSeverity 三档信号强度。
const (
	CipherSeverityNone CipherSeverity = 0 // 无信号
	CipherSeverityWeak CipherSeverity = 1 // 弱信号
	CipherSeverityStrong = 2          // 强信号
)

// CipherSeverity 枚举字符串便于前端调试显示。
type CipherSeverity int

func (s CipherSeverity) String() string {
	switch s {
	case CipherSeverityWeak:
		return "弱"
	case CipherSeverityStrong:
		return "强"
	default:
		return "无"
	}
}

// CipherTemplate 是单个暗号模板的元数据（仅狼 bot prompt 与前端调试可见）。
type CipherTemplate struct {
	Key         string          // "target_position" 等枚举 key
	Label       string          // 中文展示名（前端调试）
	Description string          // 注释（仅狼 bot prompt 可见）
	Keyword     string          // 示例关键词（公屏发言包含此关键词即命中信号）
	Severity    CipherSeverity  // 0/1/2 三档
}

// CipherBundle 是某狼座位当日（按 DayNumber 计）的暗号模板集合。
// 持久化到 WolfPackRoom（§133 同款：协议层隔离，不进 chat 表）。
type CipherBundle struct {
	Seat      int              // 持有者座位（0-indexed）
	Day       int              // 当 DayNumber（每日重置）
	Templates []CipherTemplate // 0~4 条
}

// CipherMode 是 wolfpack_assign 的可选 cipher_mode 参数。
const (
	CipherModeOff      = ""        // 默认：纯 wolf_whisper，不装暗号模板
	CipherModeStarter  = "starter" // 装 2 模板：target_position + fake_seer_posture
	CipherModeAdvanced = "advanced" // 装 4 模板（全部）
)

// CipherTemplatesStarter 装 starter 模式（2 模板）— 悍跳位/冲锋位默认。
var CipherTemplatesStarter = []CipherTemplate{
	{
		Key: CipherKeyTargetPosition, Label: "目标位置", Keyword: "X号", Severity: CipherSeverityStrong,
		Description: "在公屏发言中以「3号」「顺位3」「第3个位置」形式提及今晚刀人目标；队友按你的发言抽取座位号",
	},
	{
		Key: CipherKeyFakeSeerPosture, Label: "假预言家强度", Keyword: "查", Severity: CipherSeverityWeak,
		Description: "悍跳位发言中刻意提高「查/验/金水/昨夜」类词汇密度；队友按密度等级解读悍跳强度",
	},
}

// CipherTemplatesAdvanced 装 advanced 模式（4 模板）— 狼王全套餐。
var CipherTemplatesAdvanced = []CipherTemplate{
	{
		Key: CipherKeyTargetPosition, Label: "目标位置", Keyword: "X号", Severity: CipherSeverityStrong,
		Description: "在公屏发言中以「3号」「顺位3」形式提及刀人目标",
	},
	{
		Key: CipherKeySentimentWord, Label: "情感关键词", Keyword: "清爽", Severity: CipherSeverityWeak,
		Description: "用约定关键词正面/负面表态（正面=想刀,负面=今晚保留）",
	},
	{
		Key: CipherKeyVoteTarget, Label: "投票目标", Keyword: "我倾向", Severity: CipherSeverityWeak,
		Description: "在投票前发言里以「我倾向 X」形式提前公开投票意图",
	},
	{
		Key: CipherKeyFakeSeerPosture, Label: "假预言家强度", Keyword: "查", Severity: CipherSeverityWeak,
		Description: "悍跳位发言中刻意提高「查/验/金水」类词汇密度",
	},
}

// CipherTemplatesForMode 返回 mode 对应的模板切片（mode 不合法时返回空切片）。
func CipherTemplatesForMode(mode string) []CipherTemplate {
	switch mode {
	case CipherModeStarter:
		out := make([]CipherTemplate, len(CipherTemplatesStarter))
		copy(out, CipherTemplatesStarter)
		return out
	case CipherModeAdvanced:
		out := make([]CipherTemplate, len(CipherTemplatesAdvanced))
		copy(out, CipherTemplatesAdvanced)
		return out
	default:
		return nil
	}
}

// WolfPackCipher 是房间级暗号配置（按 seat -> day -> bundle 索引）。
// 与 WolfPackRoom 同生命周期（房间销毁时 GC 回收）。
type WolfPackCipher struct {
	bundles map[int]map[int]CipherBundle // [seat][day] -> CipherBundle
}

// NewWolfPackCipher 构造空索引。
func NewWolfPackCipher() *WolfPackCipher {
	return &WolfPackCipher{
		bundles: make(map[int]map[int]CipherBundle),
	}
}

// Set 写入/覆盖某 seat+day 的暗号模板集合。
// 线程安全：调用方持 r.mu（与 WolfPackRoom 同款锁内调用约定）。
func (c *WolfPackCipher) Set(seat int, bundle CipherBundle) {
	if c == nil || c.bundles == nil {
		return
	}
	if _, ok := c.bundles[seat]; !ok {
		c.bundles[seat] = make(map[int]CipherBundle)
	}
	c.bundles[seat][bundle.Day] = bundle
}

// Get 读取某 seat+day 的暗号模板集合（不存在返回零值 CipherBundle{}）。
func (c *WolfPackCipher) Get(seat int, day int) CipherBundle {
	if c == nil || c.bundles == nil {
		return CipherBundle{}
	}
	if byDay, ok := c.bundles[seat]; ok {
		return byDay[day]
	}
	return CipherBundle{}
}

// PurgeByDeath 清理死亡狼人的全部暗号 bundle。
// 返回清理数量。
func (c *WolfPackCipher) PurgeByDeath(deadSeats []int) int {
	if c == nil || c.bundles == nil || len(deadSeats) == 0 {
		return 0
	}
	deadSet := make(map[int]bool, len(deadSeats))
	for _, s := range deadSeats {
		deadSet[s] = true
	}
	purged := 0
	for s := range c.bundles {
		if deadSet[s] {
			delete(c.bundles, s)
			purged++
		}
	}
	return purged
}

// SnapshotForSeat 返回某座位当前 day 的暗号 bundle 拷贝（不可修改内部）。
// 调用方（buildAgentContextLocked）持锁；调用前已确认 seat 是狼。
func (c *WolfPackCipher) SnapshotForSeat(seat int, day int) CipherBundle {
	if c == nil || c.bundles == nil {
		return CipherBundle{}
	}
	return c.Get(seat, day)
}

// SnapshotAll 返回当前 day 全部存活狼的暗号 bundle（用于观战者展示）。
// 按座位升序排序便于前端稳定渲染。
func (c *WolfPackCipher) SnapshotAll(day int) []CipherBundle {
	if c == nil || c.bundles == nil {
		return nil
	}
	seats := make([]int, 0, len(c.bundles))
	for s := range c.bundles {
		seats = append(seats, s)
	}
	sort.Ints(seats)
	out := make([]CipherBundle, 0, len(seats))
	for _, s := range seats {
		if b, ok := c.bundles[s][day]; ok {
			out = append(out, b)
		}
	}
	return out
}

// Reset 清空全部暗号索引（restartGameLocked 重开局时调用）。
func (c *WolfPackCipher) Reset() {
	if c == nil {
		return
	}
	c.bundles = make(map[int]map[int]CipherBundle)
}

// CipherBundleToAgentSpec 把 werewolf.CipherBundle 转成 wwtypes 镜像（避免循环导入）。
// §133 教训(5):agent → werewolf 反向调用是循环导入的死穴，必须镜像。
func CipherBundleToAgentSpec(b CipherBundle) wwtypes.WolfPackCipherBundle {
	out := wwtypes.WolfPackCipherBundle{
		Seat: b.Seat,
		Day:  b.Day,
	}
	if len(b.Templates) > 0 {
		out.Templates = make([]wwtypes.CipherTemplateSpec, 0, len(b.Templates))
		for _, t := range b.Templates {
			out.Templates = append(out.Templates, wwtypes.CipherTemplateSpec{
				Key:         t.Key,
				Label:       t.Label,
				Description: t.Description,
				Keyword:     t.Keyword,
				Severity:    int(t.Severity),
			})
		}
	}
	return out
}

// CipherBundleForPrompt 把 bundle 渲染为注入狼 bot user prompt 的「🔐 狼队暗号协议」块。
// 仅当 bundle.Templates 非空时返回非空字符串（与 PersonalityBlock 同款零向量降级策略）。
func CipherBundleForPrompt(b CipherBundle) string {
	if len(b.Templates) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n【🔐 狼队暗号协议 — 第 ")
	sb.WriteString(strconv.Itoa(b.Day))
	sb.WriteString(" 天】\n")
	sb.WriteString("今夜你拥有以下暗号模板（队友解码用）：\n")
	for _, t := range b.Templates {
		sb.WriteString("  - ")
		sb.WriteString(t.Label)
		sb.WriteString("（")
		sb.WriteString(t.Severity.String())
		sb.WriteString("信号）关键词示例「")
		sb.WriteString(t.Keyword)
		sb.WriteString("」 — ")
		sb.WriteString(t.Description)
		sb.WriteString("\n")
	}
	sb.WriteString("你可以在公屏发言中自然嵌入这些暗号（不要破坏语义流畅度）。\n")
	sb.WriteString("队友会在自己的 prompt 末尾看到「🔐 暗号协议」块辅助识别。\n")
	sb.WriteString("暗号失败不扣分；过度使用反而会被识破。\n")
	return sb.String()
}

// 防御性引用 wwplayer 触发导入检查（即使 cipher 模块暂未直接使用，
// 未来扩展 AgentPersonality + Cipher 联动会需要；保留引用避免后续
// import 循环问题）。
var _ = wwplayer.PersonalityPresets
// ─── §4 行数治理搬移(2026-08-30 §20260830-01 同批):以下三个
// WerewolfRoom 暗号访问器原位于 room.go,纯代码搬移,零逻辑改动。 ───

// cipherLocked 返回房间暗号索引,懒初始化(与 wolfPack/informationLedger 同模式)。
// §92a 锁约束:调用方必须已持 r.mu。
func (r *WerewolfRoom) cipherLocked() *WolfPackCipher {
	if r.wolfPackCipher == nil {
		r.wolfPackCipher = NewWolfPackCipher()
	}
	return r.wolfPackCipher
}

// WolfPackCipherSnapshotLocked 返回暗号索引(供 buildAgentContextLocked / view.go 透传)。
// nil room 安全;非 nil room 返回懒初始化后的索引。
func (r *WerewolfRoom) WolfPackCipherSnapshotLocked() *WolfPackCipher {
	if r == nil {
		return nil
	}
	return r.cipherLocked()
}

// ResetWolfPackCipherLocked 清空暗号索引(restartGameLocked 重开局时调用)。
func (r *WerewolfRoom) ResetWolfPackCipherLocked() {
	if r == nil || r.wolfPackCipher == nil {
		return
	}
	r.wolfPackCipher.Reset()
}
