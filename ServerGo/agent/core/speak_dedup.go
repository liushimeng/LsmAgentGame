// Package agent — speak_dedup.go: LLM 发言去重/截断。
//
// Round 39+ 报告 N1 (轻微): 部分 LLM (典型: DouBao/豆包、Kimi 等) 会在
// 单次 speak 工具调用的 text 字段里输出一段重复的话,典型表现:
//   "我是2号。首夜还没刀人,大家先聊聊身份吧。1号和3号谁先说?
//    我是2号。首夜还没刀人,大家先聊聊身份吧。1号和3号谁先说?"
// 复制粘贴习惯 + 缺乏 review step 导致,直接对玩家展示观感差。
//
// 修复策略 (廉价,不动 prompt):
//   1. 重复段检测 — 把 text 按"句子/分号/问号/句号/换行"切成 chunk,顺序
//      去重相邻重复段。重复比例 > 50% 时合并为单段。
//   2. 整体长度硬截断 — 80 字封顶(与 BuildTools speak 描述对齐)。
//   3. 返回 (cleanedText, wasDeDuped) — caller 据此打 Debug 日志。
//
// 注意:这个 helper 不动 ChatService / BotTranscript / 限流桶;纯字符串
// 变换,所有"已发"语义由 runner.Speak 维护。
package agentcore

import (
	"strings"
	"unicode/utf8"
)

// DedupSpeakText 对 LLM 输出的 speak 文本做去重 + 长度截断,返回清理后的
// 文本与是否触发了去重 / 截断的标志。
//
// 实现策略:
//   - 切分:按中英文混合标点(。!?！？\n;；)切 chunk。
//   - 相邻重复:任何 chunk[i] == chunk[i-1] (trim 后) 直接丢弃。
//   - 重复比例:若 distinct chunks 数 / 总 chunks 数 < 0.5,触发合并。
//   - 长度截断:utf8 RuneCount < 80 字,溢出则按最后一个完整句子截断。
//   - 全空白输入:返回空串 + wasDeDuped=false,让 caller 决定是否警告 LLM。
func DedupSpeakText(text string) (cleaned string, wasDeDuped bool, wasTruncated bool) {
	if text == "" {
		return "", false, false
	}

	// 切分到 chunks
	chunks := splitSpeakChunks(text)
	if len(chunks) == 0 {
		return strings.TrimSpace(text), false, false
	}

	// 相邻去重
	deduped := make([]string, 0, len(chunks))
	for _, c := range chunks {
		tc := strings.TrimSpace(c)
		if tc == "" {
			continue
		}
		if n := len(deduped); n > 0 && deduped[n-1] == tc {
			wasDeDuped = true
			continue
		}
		deduped = append(deduped, tc)
	}

	// 整体重复检测:
	//   1) "整段复读" — distinct/total < 50% 时,只保留每个 chunk 的首次出现。
	//   2) "序列复读" — N 个连续 chunk 出现 >= 2 次(N = 序列长度),整段只留一次。
	//   两种情况通常互相独立,任一命中都触发 wasDeDuped。
	if len(deduped) > 1 {
		distinct := make(map[string]struct{}, len(deduped))
		for _, c := range deduped {
			distinct[c] = struct{}{}
		}
		// 1) 整段复读比例
		if len(distinct)*2 < len(deduped) {
			seen := make(map[string]struct{}, len(distinct))
			merged := make([]string, 0, len(distinct))
			for _, c := range deduped {
				if _, ok := seen[c]; ok {
					continue
				}
				seen[c] = struct{}{}
				merged = append(merged, c)
			}
			deduped = merged
			wasDeDuped = true
		} else {
			// 2) 序列复读:对每个起始位置 i,看 [i..i+n] 与 [i+n..i+2n] 是否一致
			//    只在 N=1..len/2 范围里找,O(n^2) 但 chunk 数本来就小(<=20)。
			deduped, wasDeDuped = collapseRepeatedSequence(deduped)
		}
	}

	joined := strings.Join(deduped, "")
	cleaned = strings.TrimSpace(joined)

	// 长度截断
	const maxRunes = 80
	if utf8.RuneCountInString(cleaned) > maxRunes {
		runes := []rune(cleaned)
		truncated := string(runes[:maxRunes])
		// 找最后一个标点(中英文混合:。!?！？;;,,~空格)做柔和截断
		// 用 strings.ContainsRune 避免 byte literal 不支持中文标点的限制。
		cutSet := "。！!？?；;，,~ 　"
		for i := len([]rune(truncated)) - 1; i > 0; i-- {
			rs := []rune(truncated)
			if i >= len(rs) {
				continue
			}
			if strings.ContainsRune(cutSet, rs[i]) {
				truncated = string(rs[:i+1])
				break
			}
		}
		cleaned = truncated
		wasTruncated = true
	}

	return cleaned, wasDeDuped, wasTruncated
}

// collapseRepeatedSequence 检测 [a,b,c] [a,b,c] 形式的整段复读;找到最长的
// 重复子序列后,只保留第一次出现。O(n^2) 暴力,n 通常 <= 20,可以接受。
//
// 关键点:比对子序列相等时直接比 string 切片(每 chunk 长度通常 <= 80,
// 总字符数 ~ 千级,O(n^3) 也不会真的慢)。
func collapseRepeatedSequence(chunks []string) ([]string, bool) {
	n := len(chunks)
	if n < 2 {
		return chunks, false
	}
	// 找最大重复段长度:从 n/2 向下枚举
	for seqLen := n / 2; seqLen >= 1; seqLen-- {
		// 检查 chunks[0..seqLen] 是否与 chunks[seqLen..2*seqLen] 相同
		// 然后再检查 chunks[2*seqLen..3*seqLen] ... 一直重复到末尾
		if !equalChunks(chunks, 0, seqLen, seqLen) {
			continue
		}
		// 至少重复一次;把后续所有完整重复块都吃掉
		end := 2 * seqLen
		for end+seqLen <= n && equalChunks(chunks, end, end+seqLen, seqLen) {
			end += seqLen
		}
		return chunks[:seqLen], true
	}
	return chunks, false
}

func equalChunks(chunks []string, a, b, length int) bool {
	if b+length > len(chunks) {
		return false
	}
	for i := 0; i < length; i++ {
		if chunks[a+i] != chunks[b+i] {
			return false
		}
	}
	return true
}

// splitSpeakChunks 按中英文混合标点切分,保留分隔符在 chunk 末尾。
// 例: "你好。我是AI。" → ["你好。", "我是AI。"]
// 用 strings.ContainsRune 而不是 case 多字符,绕开 Go 字符字面量不支持
// 多字节 rune 的限制。
func splitSpeakChunks(text string) []string {
	if text == "" {
		return nil
	}
	const cutSet = "。！!？?；;\n，,"
	var (
		out []string
		buf strings.Builder
	)
	flush := func() {
		s := strings.TrimSpace(buf.String())
		if s != "" {
			out = append(out, s)
		}
		buf.Reset()
	}
	for _, r := range text {
		if strings.ContainsRune(cutSet, r) {
			buf.WriteRune(r)
			flush()
		} else {
			buf.WriteRune(r)
		}
	}
	flush()
	return out
}

// ─────────────────── 死亡语义规范化(§123)───────────────────

// normalizeDeathTerms 把 LLM 输出中混用的「杀 / 放逐」等模糊词规范化为
// 「处决 / 死亡」标准术语(详见 docs/狼人杀死亡语义设计.md §1.2)。
//
// 设计原则:
//   - 「被 X 杀」中 X 为「狼 / 投票 / 毒 / 枪」时,统一为「被 X 死亡」
//   - 「被放逐杀」/「被投票杀」一律转为「被处决」
//   - 「自爆」原样保留(不是模糊词,是合法术语)
//   - 「出 X 出局」中 X 为「局」时,改为「被处决」(白话补充)
//
// 实现:纯字符串替换,不动语义;规则可能存在过度替换,但游戏对局容错率较高,
// 不会因为偶尔的"过度规范化"破坏体验,反而能消灭术语混乱。
func normalizeDeathTerms(text string) string {
	if text == "" {
		return text
	}
	// 高优先级:复合词先替换,避免被单字替换破坏。
	replacements := []struct{ old, new string }{
		// 「被放逐杀」「被投票杀」「被投票出局」 → 「被处决」
		{"被放逐杀", "被处决"},
		{"被投票杀", "被处决"},
		{"被投票出局", "被处决"},
		// 「被狼杀」「被女巫杀」「被毒杀」「被猎人杀」「被枪杀」 → 「被 X 死亡」
		{"被狼杀", "被狼刀死亡"},
		{"被女巫杀", "被女巫毒杀"},
		{"被毒杀", "被女巫毒杀死亡"},
		{"被猎人杀", "被猎人反杀死亡"},
		{"被枪杀", "被猎人反杀死亡"},
		// 「X 死亡」→「X 死亡」(保留),但「X 死了」→「X 死亡」
		{"死了", "死亡"},
		// 「放逐出局」→「处决」
		{"放逐出局", "处决"},
	}
	out := text
	for _, r := range replacements {
		out = strings.ReplaceAll(out, r.old, r.new)
	}
	return out
}
