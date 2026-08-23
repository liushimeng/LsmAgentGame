// Package thpagent — memory_persist.go: 德扑版持久记忆(MEMORY.md)自我迭代
// 纯函数集(§3.4 MemoryIter,对齐 wwplayer memory_iterate.go 的校验/
// HardTruncate/FallbackMerge 思路,精简为「风格画像 + 对手笔记」两段式)。
//
// 本文件是 leaf 逻辑(纯函数 + 常量);LLM 编排 + DB 读写
// 在 ws/game_service_texas_memoryiter.go。
package thpagent

import (
	"fmt"
	"strings"
)

// 德扑持久记忆大小控制常量(§3.4)。
const (
	// TexasMemoryMaxBytes 是德扑 memory_md 的存储硬上限(100KB)。
	TexasMemoryMaxBytes = 102400
	// TexasMemoryCompressThresholdBytes 是迭代时主动瘦身的触发阈值(80KB)。
	TexasMemoryCompressThresholdBytes = 81920
	// TexasMemoryIterMaxTokens 是 MemoryIter LLM 调用的 max_tokens。
	TexasMemoryIterMaxTokens = 2048
)

// 德扑版固定两段标题(顺序不可调换),与 ValidateTexasMemorySections 对齐。
var texasMemorySectionTitles = []string{
	"## 风格画像",
	"## 对手笔记",
}

// BuildTexasMemoryIterPrompt 构造德扑版自我迭代 prompt:融合旧记忆与本局
// 事实(最近手牌净盈亏 + 对手画像摘要),输出全新 MEMORY.md(全文,非 diff)。
// compress=true(旧记忆 > 80K)时追加压缩指令。
func BuildTexasMemoryIterPrompt(oldMD, sessionFacts string, compress bool) string {
	var sb strings.Builder
	sb.WriteString("你是德州扑克 AI 玩家。下面是你的【长期记忆】(跨局积累的经验)与" +
		"【本局事实】。请基于这些信息迭代你的长期记忆,输出一份全新的、完整的 MEMORY.md(不是 diff,是全文)。\n\n")
	sb.WriteString("硬性格式要求(必须严格遵守):\n")
	sb.WriteString("1. 第一行是标题:# Texas Hold'em Agent 长期记忆(可附模型名 / 已总结局数 / 更新时间)\n")
	sb.WriteString("2. 正文必须恰好包含以下 2 个二级标题,顺序不可调换:\n")
	for _, t := range texasMemorySectionTitles {
		sb.WriteString("   " + t + "\n")
	}
	sb.WriteString("3. 「## 风格画像」写你自己的打法风格与教训;「## 对手笔记」写对常遇对手(按座位/昵称)的画像与针对性策略。\n")
	sb.WriteString("4. 单段不超过 2000 字;某段没有内容时写\"暂无\",不要省略标题。\n")
	sb.WriteString("5. 不要输出 2 段以外的解释、前言或结语。\n\n")
	if compress {
		sb.WriteString("【压缩指令】你的旧记忆全文已超过 80K 字节,本次迭代必须压缩历史:\n" +
			"删除 3 局以前的细节,每个对手的笔记最多保留 3 条,优先保留最近的风格结论。\n\n")
	}
	sb.WriteString("【你的旧长期记忆】\n")
	if strings.TrimSpace(oldMD) == "" {
		sb.WriteString("(空 — 这是你的第一局,直接基于本局事实创建记忆)\n\n")
	} else {
		sb.WriteString(oldMD + "\n\n")
	}
	sb.WriteString("【本局事实(你所在座位视角)】\n")
	if strings.TrimSpace(sessionFacts) == "" {
		sb.WriteString("(无)\n")
	} else {
		sb.WriteString(sessionFacts + "\n")
	}
	return sb.String()
}

// ValidateTexasMemorySections 校验 LLM 输出是否包含全部 2 个固定段落标题。
func ValidateTexasMemorySections(md string) bool {
	for _, t := range texasMemorySectionTitles {
		if !strings.Contains(md, t) {
			return false
		}
	}
	return true
}

// TexasFallbackMerge 是德扑版规则兜底:保留旧记忆全文,末尾追加一行本局
// note;旧记忆为空时返回含 2 段骨架 + note 的新文档。
func TexasFallbackMerge(oldMD, gameNote string) string {
	note := strings.TrimSpace(gameNote)
	if note == "" {
		note = "(本局迭代失败,仅记录局数)"
	}
	line := fmt.Sprintf("- %s", note)
	if strings.TrimSpace(oldMD) == "" {
		var sb strings.Builder
		sb.WriteString("# Texas Hold'em Agent 长期记忆\n\n")
		for _, t := range texasMemorySectionTitles {
			sb.WriteString(t + "\n")
			if t == "## 对手笔记" {
				sb.WriteString(line + "\n")
			} else {
				sb.WriteString("暂无\n")
			}
			sb.WriteString("\n")
		}
		return sb.String()
	}
	return strings.TrimRight(oldMD, "\n") + "\n\n" + line + "\n"
}

// TexasHardTruncate 把 md 硬截断到 maxBytes 以内(rune 边界安全,保留头部)。
// 这是最后兜底;主路径是 LLM 迭代时主动瘦身(compress 指令)。
func TexasHardTruncate(md string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(md) <= maxBytes {
		return md
	}
	b := []byte(md)
	end := maxBytes
	for end > 0 && end < len(b) && (b[end]&0xC0) == 0x80 {
		end--
	}
	return string(b[:end]) + "\n\n…(全文超长已截断)\n"
}
