// Package wwplayer — prompt_portrait.go: §20260810-10 U2 模型自画像文案模板。
//
// 数据来源:service.ModelLogService.SelfPortraits(基于 t_lsm_game_model_game_log
// 聚合);本文件只负责「聚合统计 → 注入 system 末尾的文案」的纯函数渲染,
// 不触碰 DB / 网络,方便单测。
//
// 公平性约束(§120):小样本(Games < 8)一律走通用自画像,避免 2~3 局偶然结果
// 被当成"模型特点";所有 bot 同局都能拿到自己的自画像 → 对称信息。
// 隐私约束(§135):文案只含聚合胜率,不含单局聊天原文/对手身份。
package wwplayer

import (
	"fmt"
	"strings"
)

// SelfPortraitStats 是渲染自画像所需的最小统计集(与 service.ModelSelfPortrait
// 字段一一对应;wwplayer 不 import service,由装配层做 struct 拷贝)。
type SelfPortraitStats struct {
	Games         int64
	WinRate       float64 // 0..1
	WolfGames     int64
	WolfWinRate   float64
	GoodGames     int64
	GoodWinRate   float64
	AvgWinRateAll float64
	SampleOK      bool
}

// SelfPortraitMinGames 与 service.SelfPortraitMinGames 保持一致的文案阈值。
// (渲染层不强制,信任装配层已设置 SampleOK;此处仅文档化。)
const SelfPortraitMinGames = 8

// BuildSelfPortraitText 把聚合统计渲染为注入 system 末尾的「🪞 模型自画像」段。
// stats == nil 或 !SampleOK → 通用自画像基线。
// 返回值不含首尾多余空行(BuildSystemPrompt 负责拼接间距)。
func BuildSelfPortraitText(stats *SelfPortraitStats) string {
	if stats == nil || !stats.SampleOK {
		return "【🪞 模型自画像 — 通用基线】\n" +
			"- 本平台对局数据不足(<8 局),使用通用策略基线:\n" +
			"- 狼人: 优先悍跳预言家争夺警徽,发言自信;\n" +
			"- 好人: 少下结论,多交叉验证他人发言;\n" +
			"- 发言控制: ≤80 字,先给结论再给依据。"
	}
	var sb strings.Builder
	sb.WriteString("【🪞 模型自画像 — 你在本平台的历史表现】\n")
	sb.WriteString(fmt.Sprintf("- 总对局: %d 局, 综合胜率 %.1f%%(全模型平均 %.1f%%)\n",
		stats.Games, stats.WinRate*100, stats.AvgWinRateAll*100))
	if stats.WolfGames > 0 {
		hint := "拿狼可更主动悍跳"
		if stats.WolfWinRate < stats.AvgWinRateAll {
			hint = "拿狼时悍跳需更谨慎,优先深水/倒钩"
		}
		sb.WriteString(fmt.Sprintf("- 狼人胜率: %.1f%%(%d 局) — %s\n",
			stats.WolfWinRate*100, stats.WolfGames, hint))
	}
	if stats.GoodGames > 0 {
		hint := "好人侧发挥稳定"
		if stats.GoodWinRate < stats.AvgWinRateAll {
			hint = "当好人时容易被骗,投票前多交叉验证"
		}
		sb.WriteString(fmt.Sprintf("- 好人胜率: %.1f%%(%d 局) — %s\n",
			stats.GoodWinRate*100, stats.GoodGames, hint))
	}
	// 策略建议:按狼/好人胜率差给出差异化定位。
	switch {
	case stats.WolfGames > 0 && stats.GoodGames > 0 &&
		stats.WolfWinRate > stats.GoodWinRate+0.10:
		sb.WriteString("- 建议: 你狼人侧显著强于好人侧 — 发挥欺骗与伪装优势;好人侧对「一个人的孤证」保持怀疑\n")
	case stats.WolfGames > 0 && stats.GoodGames > 0 &&
		stats.GoodWinRate > stats.WolfWinRate+0.10:
		sb.WriteString("- 建议: 你好人侧显著强于狼人侧 — 好人时带节奏找狼;拿狼时以深水/倒钩为主,避免强行悍跳\n")
	default:
		sb.WriteString("- 建议: 两侧表现均衡 — 按当前局势灵活选择策略\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}
