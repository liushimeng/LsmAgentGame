// Package agent — speak_factcheck.go: 反死亡信息幻觉事实校验。
//
// BUG-R79 P1-NEW (2026-07-10): MiniMax M3 (Seat 3) 在 Day 1~Day 2 之间
// 多次在公屏发言中**编造未发生的死亡**:
//   - 05:46 "4号走了,我先不参选警长了" → 实际 Night 1 死的是 5号 Qwen,4号 豆包 仍存活
//   - 05:53 "6号 和 7号 都没了,神职可能都活着" → 7号 MeiTuan Night 2 死(事实),但 6号 Kimi 仍存活
//
// 根因分析(已写入 R79 报告 §P1-NEW):
//   - system prompt 里 hardBans 写的是 "❌ 不得编造未收到的信息(查验结果、用药
//     情况、私聊内容、昨夜死亡)",但 LLM 仍偶发违反。
//   - MiniMax 对夜死结果的推理不准确,可能因为死亡信息在 Day 1 投票后才
//     在公屏广播,模型基于"上轮 chat history 推测"而非"最新 user 消息的
//     authoritative 字段"。
//
// 廉价修复 (defense-in-depth,不动 prompt 不动 LLM 调用):
//   1. **句法检测**:扫描 speak text 匹配 "X号走/死/没/倒/被刀/被杀/出局/牺牲"
//      等死亡动词 + 1-7 号座位。
//   2. **事实校验**:对每个 claim 的 seat,查 authoritative 已公开死亡名单
//      (LastNightDeaths — 已通过 dawn 阶段广播 + 之前 vote 处决的座位)。
//      若 seat 不在 authoritative 列表中 → 把 claim 替换成模糊表达
//      "听说/据说/可能"。如果连模糊也做不到就保留 hint 标识,让 dispatcher
//      返回 result 字符串追加 hint,促使 LLM 下一轮收敛。
//   3. **不在 prompt 里猜**:仅在 dispatcher 派发到 runner 之前过这一层,
//      不动 BuildSystemPrompt,不动 BuildUserPrompt。
//
// 与 dedupSpeakText 的区别:dedup 处理的是"重复内容",factcheck 处理的是
// "编造事实"。两者完全独立,可以叠加使用。
package wwplayer

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// knownDeathVerbs 触发 fact-check 的"死亡动词 + 短语"。匹配规则:整词短语
// (按行切),命中后扫描前后 8 字内的 "X号" / "X 位"。
//
// 优先级:长短语优先(避免 "X号没了" 比 "没了" 先短路)。后续如需扩展,
// 直接追加一行即可。
var knownDeathVerbs = []string{
	"走了", "死了", "没了", "倒下了", "倒下", "被刀", "被杀了", "被投出局",
	"被票出", "被票", "出局了", "出局", "牺牲了", "牺牲", "不在了",
	"被放逐", "被投死", "被投", "已经走了", "已经死了", "已经没了",
	// R88 (2026-07-10) P1-NEW: 主观评价短语间接泄露死亡信息,例如
	//   "4号 真可惜,昨天他发言确实不多" → "可惜" 紧邻 seat 即可推断死亡
	// 加入这些短语后,findNearestDeathVerb 也能命中,触发整段替换。
	// 注意:这些是"间接泄露",如果 seat 在 deadSet(权威已知死亡)内则
	// 保留;仅当 seat 在 aliveSet 时强制 filter,与现有 verdict 树一致。
	"可惜", "惋惜", "可怜", "遗憾", "哀悼", "沉痛", "默哀",
	"永别了", "永别", "再也回不来了", "回不来了",
}

// seatPatternInDeathClaim 匹配 "X号" (0-13号) 或 "X位" 紧邻死亡动词。
// 允许 "3 号"(带空格)以兼容 LLM 偶尔插入空白。
//
// BUG-R80 P1-NEW (2026-07-10): Bot 4 MiniMax 实际输出过 "0号(7号玩家)"
// — LLM 泄露了 internal 0-indexed 表达。把匹配范围从 [1-7] 扩到 [0-7]
// 以 catch 这种 leak,即使内部 seat 0 = "1号" (1-indexed) 也属于错
// 误表达,因为 LLM 不应该对玩家说 "0号"。
//
// R88 (2026-07-10) P1-NEW: 13 人局时代号 8-13 也应被匹配,旧 [0-7] 让
// "8号 真可惜" 整段 leak。扩展为 [0-9] + "1[0-3]" 两位数,覆盖 0-13。
// 实际游戏中合法的 1-indexed 编号 = 1..SeatCount,SeatCount ≤ 13
// (MaxPlayers=13 from werewolf/cards.go),因此 "0号" 仍是 leak,而
// "1号" ~ "13号" 是合法。verdict 树里的 aliveSet/deadSet 决定最终
// 是否 filter,与旧路径一致。
//
// 用 (?i) 忽略大小写以兜底英文 "Seat 3" 之类的极端情况,但中文场景下大小
// 写不影响。匹配座位号 0-13(对应 internal 0-indexed 0-12 + 显式 "0号"
// leak + 13 人局 1-indexed 1-13)。
var seatPatternInDeathClaim = regexp.MustCompile(`(1[0-3]|[0-9])\s*号|(1[0-3]|[0-9])\s*位`)

// FactCheckDeathClaims scans `text` for death claims about seats NOT in
// `knownDead` and **strips** the false claim by replacing the whole
// "X号 <death-verb>" span with "[已过滤:无可证实的死亡信息]". If
// `knownDead` is empty (e.g. mid-night, no public announcement yet) then
// ANY claim about a specific seat's death is filtered — this protects
// against LLM asserting kills before dawn broadcasts them.
//
// R93 P1 (2026-07-11): 旧实现把可读标记 "[已过滤:无可证实的死亡信息]" 内联到
// 公屏 text,虽然让真人观众知道这是 bot 误传,但**同时也暴露了过滤机制的
// 存在**,且整段被替换成标记后发言长度大幅缩短、观感突兀。本轮改为:
//   - shouldReject=true 时(hard-mode),把整段失败段标记为 "应直接拒绝发言",
//     caller 应整体 drop 这条 speak,改走 reject hint 路径把错误反馈给 LLM。
//     真人观众**完全看不到**任何 marker 或残留标记。
//   - shouldReject=false(默认,向后兼容),仍走旧 inline 替换路径,
//     保留 "[已过滤:无可证实的死亡信息]" 标记以辅助调试。
//
// 推荐:call site 全部传 shouldReject=true,让 spectator 零感知。
//
// BUG-R80 P1-NEW (2026-07-10): MiniMax M3 (Seat 3) 在 R80 Day 1 仍然编造
// "听说2号走了" 等虚假死亡声明。R79 的 "prepend 听说" 修复被 LLM 自带的
// hedge 表达稀释("听说2号走了" → 仍然在公屏断言 #2 已死);人类观众读到
// 仍然会被误导。Defense-in-depth 升级:
//   - 不再 prepend "听说",而是 **整段删除** "X号...走了/死了/..." span,
//     替换为显式标记 "[已过滤:无可证实的死亡信息]",让人类观众立刻识别
//     这段话是 bot 误传,不是游戏事实。
//   - 同时识别 LLM 偶发泄露的 **0-indexed "0号"** 表达(R80 Bot 4 实际
//     写出 "0号（7号玩家）走了" — 把 internal seat 0/7 直接暴露),改写
//     时同样过滤,避免 0-indexed 数字被误读为新事实。seatPatternInDeathClaim
//     范围从 [1-7] 扩到 [0-7] 以 catch leak;seat=0 在 aliveSet 检查时一律
//     视为 "未在存活名单中"(0-indexed 的 0 不应出现在玩家可见发言中),
//     强制走 filter 路径。
//
// Parameters:
//   - text: the LLM's speak tool text (already deduped / truncated by dedupSpeakText)
//   - knownDead: the authoritative list of seats already publicly announced dead
//     in this game (LastNightDeaths + 之前 vote 处决). Use GetAuthoritativeDeaths()
//     in runner.Speak to compute this.
//   - alive: current alive seats. If a "death claim" is about a seat NOT in
//     alive AND NOT in knownDead, it's referencing a past public death
//     announcement — leave it alone (don't over-flag).
//
// Returns:
//   - cleaned: the (possibly rewritten) text. If `wasFactChecked` is true,
//     the text had at least one claim stripped. The function never REJECTS the
//     text — the worst case is rewriting "听说4号死了" → "[已过滤:无可证实的死亡信息]".
//   - wasFactChecked: true if at least one claim was stripped.
//
// Cost: O(text_length + knownDead_len^2). For a typical 80-char speak text
// and a 7-seat game, this is < 1µs. No LLM, no allocation pressure.
func FactCheckDeathClaims(text string, knownDead []int, alive []int) (cleaned string, wasFactChecked bool) {
	return FactCheckDeathClaimsWithReject(text, knownDead, alive, false)
}

// FactCheckDeathClaimsWithReject 是 R93 P1 后的升级版。与基础版的区别:
//   - shouldReject=true: 当检测到任何编造死亡声明,整段发言应被 caller 拒绝
//     (而不是内联替换 marker)。调用方应在 broadcast 前检查 wasFactChecked &&
//     shouldReject,整体 drop 这条 speak 并把 hint 返回给 LLM,让真人观众
//     完全看不到过滤痕迹。
//   - shouldReject=false: 走旧 inline 替换路径("[已过滤:无可证实的死亡信息]"),
//     用于 shouldReject 路径的回归测试中验证底层 fact-check 仍然正确。
//
// 设计权衡:
//
//   reject 是「换条更短更 hedge 的发言」语义(speak 频率损失) vs inline 是
//   「保留发言但把错误段挖空」语义(filter 透明度损失)。R93 P1 报告证实
//   inline 路径把过滤机制本身暴露给观战者,这是 silent discriminator
//   (人类能识别"这段不对劲"),所以改 hard reject。
func FactCheckDeathClaimsWithReject(text string, knownDead []int, alive []int, shouldReject bool) (cleaned string, wasFactChecked bool) {
	if text == "" {
		return text, false
	}
	// Quick filter: no death verb present → nothing to do.
	if !containsAnyDeathVerb(text) {
		return text, false
	}

	// Build a set of knownDead seats (1-indexed). Empty set is fine; we'll
	// filter every claim.
	deadSet := make(map[int]bool, len(knownDead))
	for _, s := range knownDead {
		// External callers pass 0-indexed seats; convert to 1-indexed for
		// matching the "X号" pattern.
		if s >= 0 {
			deadSet[s+1] = true
		}
	}
	// aliveSet is used to distinguish "definitely alive" vs "unknown state".
	// If a claim is about a seat in `alive`, the claim is definitely wrong.
	// If a claim is about a seat NOT in alive AND NOT in knownDead, the seat
	// might be a past public death we didn't include (defensive) — leave it.
	aliveSet := make(map[int]bool, len(alive))
	for _, s := range alive {
		if s >= 0 {
			aliveSet[s+1] = true
		}
	}
	// BUG-R80 P1-NEW (2026-07-10): 显式标记 seat=0 (0-indexed) 为"非法
	// 玩家发言" — LLM 不应该对玩家说"0号",这是 internal representation
	// leak。把 aliveSet[1] 的合法含义(1-indexed 1号 = 0-indexed seat 0)
	// 与 "LLM 说 0号"区分开:后者一定走 filter。
	//
	// 这里用 zeroIndexLeak 标记:在 aliveSet 中,但当 seat=0 命中时,仍
	// 视为 leak。处理放在主循环里检查。

	// Find all (seat, verb) tuples in text. We rewrite per occurrence to
	// preserve sentence structure.
	matches := seatPatternInDeathClaim.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return text, false
	}

	// BUG-R80 P1-NEW (2026-07-10): We need to capture the *range* of the
	// claim (seat + adjacent verb) and replace the whole thing. The seat
	// match itself doesn't include the verb; we must expand the range to
	// cover verb + any trailing punctuation. To avoid overlapping edits,
	// we process matches in right-to-left order, expanding each match's
	// range greedily forward to include the verb and any following
	// commas/periods.
	type editSpan struct {
		start, end int    // byte indices into text (half-open)
		repl       string // replacement text
	}
	var edits []editSpan

	// Process right-to-left to avoid offset drift on later matches.
	for i := len(matches) - 1; i >= 0; i-- {
		m := matches[i]
		seatStr := text[m[2]:m[3]]
		seat, err := strconv.Atoi(strings.TrimSpace(seatStr))
		if err != nil {
			continue
		}

		// Look for a death verb within ±8 chars around the seat match.
		verbStart, verbEnd := findNearestDeathVerb(text, m[0], m[1])
		if verbStart < 0 {
			continue
		}

		// Determine whether to filter this claim.
		// - seat=0: BUG-R80 LLM 0-indexed leak,一律 filter(玩家不会说 0 号)
		// - seat in deadSet: claim matches authoritative death → keep.
		// - seat in aliveSet: claim contradicts alive list → MUST filter.
		// - seat in neither (past death not in our authoritative list, or
		//   hypothetical future): leave alone (could be a legitimate past
		//   reference we didn't import).
		if seat == 0 {
			// 0-indexed leak — always filter,don't even check deadSet.
		} else if deadSet[seat] {
			continue
		} else if !aliveSet[seat] {
			// Not in alive AND not in knownDead — could be past public
			// death we don't track. Keep.
			continue
		}

		// Expand the deletion range to include the seat AND verb. If verb
		// comes before seat, expand backwards from m[0] to verbStart; if
		// after, expand forwards from m[1] to verbEnd.
		spanStart := verbStart
		if m[0] < spanStart {
			spanStart = m[0]
		}
		spanEnd := verbEnd
		if m[1] > spanEnd {
			spanEnd = m[1]
		}
		// Trim leading hedge words ("听说/据说/似乎/好像/可能") that are
		// immediately before the span — they belong to the same clause
		// and look awkward if left dangling.
		spanStart = trimLeadingHedge(text, spanStart)
		// Also consume trailing punctuation/whitespace (e.g. "。" ", ")
		// up to but not including the next Chinese character that would
		// be a new sentence. We only consume commas/periods/顿号.
		spanEnd = consumeTrailingPunct(text, spanEnd)

		edits = append(edits, editSpan{
			start: spanStart,
			end:   spanEnd,
			repl:  "[已过滤:无可证实的死亡信息]",
		})
	}

	if len(edits) == 0 {
		return text, false
	}
	wasFactChecked = true

	// Apply edits right-to-left so earlier offsets stay valid.
	var sb strings.Builder
	prev := 0
	for _, e := range edits {
		if e.start > prev {
			sb.WriteString(text[prev:e.start])
		}
		sb.WriteString(e.repl)
		prev = e.end
	}
	if prev < len(text) {
		sb.WriteString(text[prev:])
	}
	cleaned = sb.String()
	return cleaned, true
}

// trimLeadingHedge 把 spanStart 之前连续的 hedge 词 ("听说/据说/似乎/好像/
// 可能/大约/也许") 一起吃进 span,避免留下 "听说 [已过滤]" 这种语法奇怪
// 的片段。仅当 hedge 词紧邻 spanStart(中间无其他汉字)时吃。
func trimLeadingHedge(text string, spanStart int) int {
	if spanStart <= 0 {
		return spanStart
	}
	hedgeWords := []string{"听说", "据说", "似乎", "好像", "可能", "大约", "也许"}
	for {
		// Find longest hedge word that ends exactly at spanStart.
		matched := false
		for _, w := range hedgeWords {
			wlen := len(w)
			if spanStart >= wlen && text[spanStart-wlen:spanStart] == w {
				// Confirm the hedge is attached (next char is space/comma/
				// nothing) — we already know it ends at spanStart, that's
				// enough.
				spanStart -= wlen
				matched = true
				break
			}
		}
		if !matched {
			break
		}
	}
	return spanStart
}

// consumeTrailingPunct 把 spanEnd 之后的标点(逗号/顿号/句号/分号)一起
// 吃进 span,避免留下 "[已过滤]。" 这种半句话。仅消费紧邻的标点,不跨越
// 中文字符。
func consumeTrailingPunct(text string, spanEnd int) int {
	for spanEnd < len(text) {
		c := text[spanEnd]
		if c == ',' || c == ';' || c == ' ' || c == '\t' {
			spanEnd++
			continue
		}
		// Multi-byte UTF-8 chars: Chinese punctuation
		if spanEnd+3 <= len(text) {
			three := text[spanEnd : spanEnd+3]
			if three == "，" || three == "、" || three == "；" || three == "。" || three == " " {
				spanEnd += 3
				continue
			}
		}
		break
	}
	return spanEnd
}

// containsAnyDeathVerb 快速预检:有死亡动词才走完整扫描。
func containsAnyDeathVerb(text string) bool {
	for _, v := range knownDeathVerbs {
		if strings.Contains(text, v) {
			return true
		}
	}
	return false
}

// findNearestDeathVerb 在 seat 匹配前后 ±8 char 窗口内找最近的死亡动词。
// 返回 (verbStart, verbEnd) 表示要替换的区间;未找到返回 (-1, 0)。
//
// 设计:中文/英文混合,LLM 输出可能在 seat 前后插入空格/标点;窗口 8 字
// 足够覆盖"3号 走了"、"3号已经死了"、"3号被刀"等典型句式。
//
// 注意:text 是 byte 切片,UTF-8 中文占 3 字节。直接按 byte 切片可能把
// 一个 rune 切到两个 window 之间 → strings.Index 失败。改用 rune 索引
// + utf8.RuneLen 转换,确保切片边界在 rune 边界上。
func findNearestDeathVerb(text string, seatStart, seatEnd int) (int, int) {
	// 把 byte 索引转 rune 索引:遍历 runes 数到 seatStart / seatEnd 对应的 rune。
	runes := []rune(text)
	runStart := byteIndexToRuneIndex(text, seatStart)
	runEnd := byteIndexToRuneIndex(text, seatEnd)

	winRuneStart := runStart - 8
	if winRuneStart < 0 {
		winRuneStart = 0
	}
	winRuneEnd := runEnd + 8
	if winRuneEnd > len(runes) {
		winRuneEnd = len(runes)
	}
	window := string(runes[winRuneStart:winRuneEnd])
	for _, v := range knownDeathVerbs {
		idx := strings.Index(window, v)
		if idx < 0 {
			continue
		}
		// idx 是 runes 的 byte 索引 — 重新定位到 runes 切片位置。
		verbRunes := []rune(v)
		verbRuneStart := runeCount(window[:idx])
		verbRuneEnd := verbRuneStart + len(verbRunes)
		absRuneStart := winRuneStart + verbRuneStart
		absRuneEnd := winRuneStart + verbRuneEnd
		// 把 rune 索引转回 byte 索引(用于 text 切片)。
		absByteStart := runeIndexToByteIndex(text, absRuneStart)
		absByteEnd := runeIndexToByteIndex(text, absRuneEnd)
		return absByteStart, absByteEnd
	}
	return -1, 0
}

// byteIndexToRuneIndex 把 byte 索引转换为 rune 索引。rune 索引 = 第几个字符。
// 越界时返回 len(runes)。必须在 UTF-8 rune 边界上调用 — caller 拿到的 byte
// 索引通常来自 regex match,已经是 rune 边界。
func byteIndexToRuneIndex(s string, byteIdx int) int {
	if byteIdx <= 0 {
		return 0
	}
	if byteIdx >= len(s) {
		return len([]rune(s))
	}
	runeIdx := 0
	for i := 0; i < byteIdx; {
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
		runeIdx++
	}
	return runeIdx
}

// runeIndexToByteIndex 把 rune 索引转换为 byte 索引。
// 越界时返回 len(s)。
func runeIndexToByteIndex(s string, runeIdx int) int {
	if runeIdx <= 0 {
		return 0
	}
	count := 0
	byteIdx := 0
	for byteIdx < len(s) {
		if count == runeIdx {
			return byteIdx
		}
		_, size := utf8.DecodeRuneInString(s[byteIdx:])
		byteIdx += size
		count++
	}
	return byteIdx
}

// runeCount 返回 s 中的 rune 数。空字符串返回 0。
func runeCount(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}