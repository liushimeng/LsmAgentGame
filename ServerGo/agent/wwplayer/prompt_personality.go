// Package wwplayer — prompt_personality.go: 人设化 Agent + 性格倾向参数（§20260811-04 U2）。
//
// 设计动机（Agent-Surpport-01.md §9.4 / §4.4）：
//   - 当前所有 Agent 都被默认训练成"理性最大化"风格 → 打法同质化。
//   - Personality 注入 system 末尾(与 SelfPortrait 同款位置)，前缀字节级别不变
//     → Anthropic prompt cache 命中，零额外 LLM 成本。
//
// 五维向量（连续 0~1 浮点）：
//   - Aggressiveness    攻击性   0=划水,1=抢麦
//   - TrustTendency     信任倾向  0=多疑,1=轻信
//   - BluffFrequency    欺骗频率  0=诚实,1=满口跑火车
//   - CollaborationStyle 协作风格 0=独狼,1=拉同盟
//   - RiskTolerance     风险承受  0=保守,1=激进梭哈
//
// 5 种预设人设对应 5 维固定向量，用户可在 RoomCreateModal 自定义滑块。
//
// §128 对话即思考：仅注入 system 末尾，不污染对话。
// §119 协议层隔离：纯 system prompt 注入，无 chat 写入路径。
// §121 数据形状：PersonalityVector 是 5 float64，跨端严格对齐。
package wwplayer

import "strings"

// PersonalityVector 是 5 维性格倾向参数。
// 与 §20260810-10 U2 SelfPortrait 同款 system 末尾注入策略。
type PersonalityVector struct {
	Aggressiveness     float64 `json:"aggressiveness"`
	TrustTendency      float64 `json:"trust_tendency"`
	BluffFrequency     float64 `json:"bluff_frequency"`
	CollaborationStyle float64 `json:"collaboration_style"`
	RiskTolerance      float64 `json:"risk_tolerance"`
}

// 5 种预设人设的固定向量值（与 docs/狼人杀-Agent与系统/狼人杀13人局Agent升级-20260811-04.md §U2 表对齐）。
var PersonalityPresets = map[string]PersonalityVector{
	// 逻辑流:发言引用编号/概率/对仗,投票只看逻辑证据。
	"logical": {
		Aggressiveness: 0.4, TrustTendency: 0.3, BluffFrequency: 0.2, CollaborationStyle: 0.5, RiskTolerance: 0.3,
	},
	// 情绪流:发言注重语气/阵营氛围,投票看"我觉得"。
	"emotional": {
		Aggressiveness: 0.5, TrustTendency: 0.6, BluffFrequency: 0.3, CollaborationStyle: 0.7, RiskTolerance: 0.4,
	},
	// 激进冲锋:主动带节奏、抢警徽、攻击发言漏洞,高发言密度。
	"aggressive": {
		Aggressiveness: 0.9, TrustTendency: 0.2, BluffFrequency: 0.7, CollaborationStyle: 0.6, RiskTolerance: 0.8,
	},
	// 稳健守卫:划水、跟随、关键时刻才发言,低发言密度。
	"cautious": {
		Aggressiveness: 0.2, TrustTendency: 0.5, BluffFrequency: 0.1, CollaborationStyle: 0.4, RiskTolerance: 0.2,
	},
	// 戏精型:编故事、表演、戏剧化指控,高情绪切换频率。
	"showman": {
		Aggressiveness: 0.7, TrustTendency: 0.4, BluffFrequency: 0.9, CollaborationStyle: 0.8, RiskTolerance: 0.7,
	},
}

// PersonalityPresetLabels 是预设 key → 中文展示名的查表（前端 i18n 复用）。
var PersonalityPresetLabels = map[string]string{
	"logical":    "逻辑流",
	"emotional":  "情绪流",
	"aggressive": "激进冲锋",
	"cautious":   "稳健守卫",
	"showman":    "戏精型",
}

// LookupPersonalityPreset 根据预设 key 返回固定向量；未知 key 回退到 logical。
// §121 数据形状:前端传入空字符串/"random"/"custom" 时,room 层应已转写,
// 此处仅兜底处理 unknown preset 防御。
func LookupPersonalityPreset(presetKey string) PersonalityVector {
	if v, ok := PersonalityPresets[presetKey]; ok {
		return v
	}
	return PersonalityPresets["logical"]
}

// PersonalityLabelFor 返回预设 key 的展示名。
func PersonalityLabelFor(presetKey string) string {
	if label, ok := PersonalityPresetLabels[presetKey]; ok {
		return label
	}
	return ""
}

// clampPersonalityComponent 把浮点裁剪到 [0,1]（防止前端越界）。
func clampPersonalityComponent(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// Clamp 返回向量每个分量裁剪到 [0,1] 后的拷贝。
func (p PersonalityVector) Clamp() PersonalityVector {
	return PersonalityVector{
		Aggressiveness:     clampPersonalityComponent(p.Aggressiveness),
		TrustTendency:      clampPersonalityComponent(p.TrustTendency),
		BluffFrequency:     clampPersonalityComponent(p.BluffFrequency),
		CollaborationStyle: clampPersonalityComponent(p.CollaborationStyle),
		RiskTolerance:      clampPersonalityComponent(p.RiskTolerance),
	}
}

// IsZeroVector 判断向量是否全 0（空/未设置）。
func (p PersonalityVector) IsZeroVector() bool {
	return p.Aggressiveness == 0 && p.TrustTendency == 0 && p.BluffFrequency == 0 &&
		p.CollaborationStyle == 0 && p.RiskTolerance == 0
}

// BuildPersonalityText 把 5 维向量渲染为注入 system 末尾的「🎭 人设倾向」段。
//
// §128 对话即思考:性格约束是「你是谁」而非「策略建议」。
// 空向量(IsZeroVector)返回空串,调用方据此跳过注入(保留 cache 命中的零成本路径)。
func BuildPersonalityText(p PersonalityVector, presetKey string) string {
	if p.IsZeroVector() {
		return ""
	}
	label := PersonalityLabelFor(presetKey)
	if label == "" {
		label = "自定义"
	}
	p = p.Clamp()

	// 用 2 位小数渲染,避免 LLM 把浮点尾数当噪声;同时按区间附语义标签,
	// 让 LLM 在边界值上有清晰指引(<0.3=低,0.3-0.7=中,>0.7=高)。
	tendency := func(v float64) string {
		switch {
		case v < 0.3:
			return "低"
		case v > 0.7:
			return "高"
		default:
			return "中"
		}
	}

	var sb strings.Builder
	sb.WriteString("\n\n【🎭 人设倾向 — ")
	sb.WriteString(label)
	sb.WriteString("】\n")
	sb.WriteString("你的本次对局人格是: ")
	sb.WriteString(label)
	sb.WriteString("\n")
	sb.WriteString("- 攻击性 Aggressiveness=")
	sb.WriteString(formatFloat2(p.Aggressiveness))
	sb.WriteString(" (")
	sb.WriteString(tendency(p.Aggressiveness))
	sb.WriteString(" — 低=划水不发言,高=主动抢麦+每天指控)\n")
	sb.WriteString("- 信任倾向 TrustTendency=")
	sb.WriteString(formatFloat2(p.TrustTendency))
	sb.WriteString(" (")
	sb.WriteString(tendency(p.TrustTendency))
	sb.WriteString(" — 低=谁都怀疑,高=谁都是好人)\n")
	sb.WriteString("- 欺骗频率 BluffFrequency=")
	sb.WriteString(formatFloat2(p.BluffFrequency))
	sb.WriteString(" (")
	sb.WriteString(tendency(p.BluffFrequency))
	sb.WriteString(" — 低=从不说谎,高=满口跑火车)\n")
	sb.WriteString("- 协作风格 CollaborationStyle=")
	sb.WriteString(formatFloat2(p.CollaborationStyle))
	sb.WriteString(" (")
	sb.WriteString(tendency(p.CollaborationStyle))
	sb.WriteString(" — 低=独狼,高=必拉同盟)\n")
	sb.WriteString("- 风险承受 RiskTolerance=")
	sb.WriteString(formatFloat2(p.RiskTolerance))
	sb.WriteString(" (")
	sb.WriteString(tendency(p.RiskTolerance))
	sb.WriteString(" — 低=0% 风险行动,高=100% 激进梭哈)\n")
	sb.WriteString("请在所有发言/投票/技能使用中遵循此人格倾向 —— 这不是「策略建议」而是「你是谁」。\n")
	return sb.String()
}

// formatFloat2 渲染 2 位小数的浮点(避免引入 strconv 依赖 + 单元测试可读性)。
func formatFloat2(v float64) string {
	// 简化:整数补 .00,小数取 2 位
	whole := int(v * 100)
	if v >= 0 {
		return formatFixed2(whole)
	}
	return "-" + formatFixed2(-whole)
}

func formatFixed2(centi int) string {
	intPart := centi / 100
	fracPart := centi % 100
	if fracPart < 0 {
		fracPart = -fracPart
	}
	return itoa3(intPart) + "." + pad2(fracPart)
}

func itoa3(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func pad2(n int) string {
	if n < 10 {
		return "0" + string(byte('0'+n))
	}
	return string([]byte{byte('0' + n/10), byte('0' + n%10)})
}