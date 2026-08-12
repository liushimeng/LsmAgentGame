// Package agent — speak_whisper_factcheck.go: 反私聊内容幻觉事实校验。
//
// BUG-R151-FAIRNESS-001 (2026-07-18): R151 全 AI 狼人杀 13 人局端到端报告
// 暴露的严重公平性缺陷:Bot 8号 (美团 LongCat-2.0) 在公屏发言中**捏造**了一条
// "9号 私聊告诉我 12号 是悍跳狼队友"的私聊内容,并以此为「铁证」诱导其他
// Bot 投票,最终导致真实预言家 Bot 12号 (Seat 11, seer) 被冤杀。后续
// Bot 2号 又声称「12号 昨晚私聊我让我帮他拉票」—— 同类性质的捏造。
//
// 根因(同 R79 P1-NEW 的死亡信息幻觉):
//   - system prompt 里 hardBans 已有 "严禁编造未收到的私聊信息" 字样,但 LLM
//     (尤其长上下文 + 中段投票混乱) 仍频繁违反,且模型对"我收到了谁发来的
//     私聊"这类事实型声明有强烈的"先验补全"冲动(典型 hallucination 模式)。
//   - LLM 的 prompt context 末尾有「发给你的私聊」段(WhisperInbox),正常 Bot
//     应该严格按 inbox 内容引用,但模型在公屏"讲故事"压力下倾向捏造证据链。
//
// 廉价修复 (defense-in-depth, 复用 R79 死亡 factcheck 的成功模式):
//   1. **句法检测**:扫描 speak text 匹配「X号 私聊 / 悄悄话 / 私信 / 跟我说 /
//      告诉我」+ 1-13 号座位的归因模式。
//   2. **事实校验**:对每个 claim 的 seat,查 authoritative WhisperInbox 中
//      FromSeat 是否真实存在该座位→我 的 whisper 事件:
//      - 若 seat 0-indexed 对应的 1-indexed 编号在 inbox 集合 → 保留原文;
//      - 若不在 → 把整段「X号 私聊告诉我」改写为 "[已过滤:无可证实的私聊]",
//        避免 LLM 用不存在的事件污染公开信息池;
//   3. **不在 prompt 里猜**:仅在 dispatcher 派发到 runner 之前过这一层,
//     不动 BuildSystemPrompt(已在 §2.2 加 hard ban 提示);复用 agentRunner
//     .Speak 已有的 factcheck 链路,R93 同款 shouldReject=true 模式。
//
// 与 FactCheckDeathClaims 的区别:
//   - 死亡 factcheck 用已知死亡/存活列表(权威 list),本模块用 WhisperInbox
//     (per-seat 元组列表)做权威 — 同一思想(LLM 声明 vs authoritative 列表),
//     不同数据源。
//   - 死亡是全局权威(所有玩家都收到了 death 公告),私聊是 per-seat 权威
//     (只有接收方知道有没有收到),所以这里需要 caller 传入具体的 inbox。
//
// 设计权衡:
//   - hard-reject vs inline: 同 R93 P1 — inline "[已过滤]" 会暴露过滤机制给
//     真人观众,被识别为 silent discriminator → 直接 drop,让 LLM 在下一轮
//     重新组织发言(不要在公屏引用未收到的私聊)。
package wwplayer

import "LsmAgentGame/agent/wwtypes"

import (
	"regexp"
	"strconv"
	"strings"
)

// knownWhisperAttributionVerbs 触发 fact-check 的「私聊归因」动词短语。
// 命中规则:短语出现在文本里,且前后 ±10 rune 窗口内出现 "X号" / "X 位"。
//
// 优先级:长短语优先(避免 "私聊" 比 "私下告诉我" 先短路)。后续如需扩展,
// 直接追加一行即可。
//
// BUG-R151-FAIRNESS-001:实测 Bot 8号 原话是 "9号 私聊告诉我" + "12号 跟
// 我说";Bot 2号 是 "12号 私聊我让我帮他拉票"。覆盖三类典型归因:
//   - 第三人称陈述:「X号 私聊告诉我 Y」/ 「X号 私下告诉我」
//   - 第二人称陈述:「X号 跟我说」/ 「X号 告诉我」
//   - 省略主语:「X号 私聊我...」(Bot 2号 形式)
var knownWhisperAttributionVerbs = []string{
	"私下告诉我", "悄悄话告诉我", "悄悄告诉我",
	"私信告诉我", "私聊告诉我",
	"私下跟我说", "悄悄跟我说", "悄悄话跟我说",
	"私信跟我说", "私聊跟我说",
	"私下说", "悄悄说", "悄悄话说", "悄悄话说",
	"跟我说", "告诉我", "对我说",
	"私聊我", "私信我",
}

// seatPatternInWhisperClaim 匹配 "X号" / "X位" (1-13号, 与 death factcheck
// 同样的范围)。允许 "3 号"(带空格)以兼容 LLM 偶尔插入空白。
//
// 与 FactCheckDeathClaims 共用 seatPatternInDeathClaim 也行,但语义上独立,
// 各保留一份以备后续单测与扩展。
var seatPatternInWhisperClaim = regexp.MustCompile(`(1[0-3]|[0-9])\s*号|(1[0-3]|[0-9])\s*位`)

// whisperInboxFromSeats 把 WhisperInbox 转成 1-indexed 发送者集合。
// 接收方只关心「谁发给我过」,不关心内容/时间戳。
//
// 防御性:FromSeat < 0(观战者/系统)跳过,只保留 0-indexed bot seat。
func whisperInboxFromSeats(inbox []wwtypes.WhisperEvent) map[int]bool {
	set := make(map[int]bool, len(inbox))
	for _, w := range inbox {
		if w.FromSeat < 0 {
			continue
		}
		// 1-indexed 转换:FromSeat=0 (Seat 0) 对应玩家编号 1号。
		set[w.FromSeat+1] = true
	}
	return set
}

// FactCheckWhisperAttribution scans `text` for claims that a specific player
// whispered something to me, and **strips** those claims that don't match the
// authoritative `inbox`.
//
// Returns the cleaned text and `wasFactChecked` = true when at least one
// claim was rewritten. If no claim is present, returns (text, false) unchanged.
//
// Cost: O(text_length + inbox_len). For a typical 80-char speak text and a
// 13-bot game with up to 50 inbox events, this is < 5µs. No LLM, no allocation
// pressure beyond a small map.
//
// Caller 责任(参考 FactCheckDeathClaims):
//   - 传 nil 或空 inbox 时,所有「X号 私聊告诉我」类 claim 都会被替换 —
//     因为 nothing legitimately whisper'd to me yet (典型 pre_wolves 早期)。
//   - 应在 broadcast 前调用;若 wasFactChecked=true,根据 shouldReject 选择
//     inline 替换 vs 整体 drop。
func FactCheckWhisperAttribution(text string, inbox []wwtypes.WhisperEvent) (cleaned string, wasFactChecked bool) {
	if text == "" {
		return text, false
	}
	// Quick filter: no attribution verb present → nothing to do.
	if !containsAnyWhisperVerb(text) {
		return text, false
	}

	// Build authoritative "who has whispered to me" set (1-indexed).
	whisperSenders := whisperInboxFromSeats(inbox)

	// Walk through the text, find every seat match + nearby attribution verb.
	// Reuse the byteIndexToRuneIndex / runeIndexToByteIndex helpers from
	// FactCheckDeathClaims so we stay consistent with that pipeline.
	type span struct{ start, end int } // byte offsets in `text`
	var hits []span

	// Find every "X号"/"X位" match.
	idxs := seatPatternInWhisperClaim.FindAllStringIndex(text, -1)
	for _, m := range idxs {
		seatStart, seatEnd := m[0], m[1]
		// Extract the seat number from the matched substring.
		seatStr := text[seatStart:seatEnd]
		// Strip trailing "号" / "位" / spaces to parse digits.
		seatStr = strings.TrimSpace(seatStr)
		seatStr = strings.TrimRight(seatStr, "号位")
		seatStr = strings.TrimSpace(seatStr)
		seatNum, err := strconv.Atoi(seatStr)
		if err != nil || seatNum < 1 || seatNum > 13 {
			continue
		}

		// Look AFTER the seat (within 8 rune) for a whisper attribution verb.
		// Only consider "X号 <verb>" pattern (natural Chinese phrasing),
		// NOT "<verb> X号" — the latter is rare and risks false-positive
		// when the verb belongs to a DIFFERENT seat mention earlier.
		verbStart, verbEnd := findNearestWhisperVerbAfter(text, seatStart, seatEnd)
		if verbStart < 0 {
			continue
		}

		// If this seat has whispered to me, the claim is legitimate — keep.
		if whisperSenders[seatNum] {
			continue
		}

		// Otherwise mark the span for stripping.
		// span covers from verb start to seat end, so we kill the whole
		// "X号 私聊告诉我" pattern. Trim trailing punctuation via the
		// shared helper.
		end := consumeTrailingPunct(text, verbEnd)
		hits = append(hits, span{start: seatStart, end: end})
	}

	if len(hits) == 0 {
		return text, false
	}

	// Sort hits by start ascending; assume non-overlapping (each seat match
	// produces an independent span). Merge adjacent spans defensively.
	sortedHits := hits
	for i := 1; i < len(sortedHits); i++ {
		if sortedHits[i].start < sortedHits[i-1].end {
			// Overlap: extend previous.
			if sortedHits[i].end > sortedHits[i-1].end {
				sortedHits[i-1].end = sortedHits[i].end
			}
			sortedHits = append(sortedHits[:i], sortedHits[i+1:]...)
			i--
		}
	}

	// Rebuild text by replacing each hit with "[已过滤:无可证实的私聊]".
	// Keep it short and uniform so callers can detect it.
	const marker = "[已过滤:无可证实的私聊]"
	var b strings.Builder
	prev := 0
	for _, h := range sortedHits {
		b.WriteString(text[prev:h.start])
		b.WriteString(marker)
		prev = h.end
	}
	b.WriteString(text[prev:])
	return b.String(), true
}

// containsAnyWhisperVerb 快速预检:有归因动词才走完整扫描。
func containsAnyWhisperVerb(text string) bool {
	for _, v := range knownWhisperAttributionVerbs {
		if strings.Contains(text, v) {
			return true
		}
	}
	return false
}

// findNearestWhisperVerbAfter 在 seat 匹配的**之后** ±8 rune 窗口内找最近
// 的私聊归因动词。返回 (verbStart, verbEnd) 表示要替换的区间;未找到返回 (-1, 0)。
//
// 与 FactCheckDeathClaims.findNearestDeathVerb 不同:仅向后看,因为
// "X号 <verb>" 是中文自然语序;若前后都看会出现 "X号 ... <verb> ... Y号"
// 时 Y号 错配到 X号 的 verb 上的 false-positive(R151 单测发现)。
func findNearestWhisperVerbAfter(text string, seatStart, seatEnd int) (int, int) {
	runes := []rune(text)
	runEnd := byteIndexToRuneIndex(text, seatEnd)

	winRuneStart := runEnd
	winRuneEnd := runEnd + 8
	if winRuneEnd > len(runes) {
		winRuneEnd = len(runes)
	}
	if winRuneStart >= winRuneEnd {
		return -1, 0
	}
	window := string(runes[winRuneStart:winRuneEnd])
	for _, v := range knownWhisperAttributionVerbs {
		idx := strings.Index(window, v)
		if idx < 0 {
			continue
		}
		verbRunes := []rune(v)
		verbRuneStart := runeCount(window[:idx])
		verbRuneEnd := verbRuneStart + len(verbRunes)
		absRuneStart := winRuneStart + verbRuneStart
		absRuneEnd := winRuneStart + verbRuneEnd
		absByteStart := runeIndexToByteIndex(text, absRuneStart)
		absByteEnd := runeIndexToByteIndex(text, absRuneEnd)
		return absByteStart, absByteEnd
	}
	return -1, 0
}
