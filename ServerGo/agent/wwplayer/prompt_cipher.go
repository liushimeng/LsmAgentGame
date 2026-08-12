// Package wwplayer — prompt_cipher.go: 狼队暗号系统 prompt 渲染层（§20260811-04 U1）。
//
// 设计动机(Agent-Surpport-01.md §3.7 暗号系统):
//   - wolf_whisper 走 GameContext 注入,狼队可见;
//   - 暗号协议把"今晚刀谁"等敏感行动编码到公屏发言里,本模块渲染解码提示给队友;
//   - 仅当 gc.WolfPackCipher 非 nil 且 Templates 非空时渲染。
//
// §119 协议层隔离:wolfpack msgs / cipher bundle 都不进 chat_message / chat_history。
// §128 对话即思考:cipher 是「工具说明」而非「强制逻辑」,LLM 自行决定嵌入强度。
package wwplayer

import (
	"strings"

	"LsmWebGame/agent/wwtypes"
)

// BuildCipherProtocolBlock 把 WolfPackCipherBundle 渲染为注入狼 bot user prompt
// 末尾的「🔐 狼队暗号协议」块。
//
// 零值 bundle（Templates 为空）返回空串,调用方据此跳过注入（cache 命中零成本）。
func BuildCipherProtocolBlock(b *wwtypes.WolfPackCipherBundle) string {
	if b == nil || len(b.Templates) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n【🔐 狼队暗号协议 — 第 ")
	sb.WriteString(itoaCipher(b.Day))
	sb.WriteString(" 天】\n")
	sb.WriteString("今夜你拥有以下暗号模板（队友解码用）：\n")
	for _, t := range b.Templates {
		sb.WriteString("  - ")
		sb.WriteString(t.Label)
		sb.WriteString("（")
		sb.WriteString(cipherSeverityLabel(t.Severity))
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

func cipherSeverityLabel(sev int) string {
	switch sev {
	case 1:
		return "弱"
	case 2:
		return "强"
	default:
		return "无"
	}
}

func itoaCipher(n int) string {
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