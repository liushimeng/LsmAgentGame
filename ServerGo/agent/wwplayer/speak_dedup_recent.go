// Package agent — speak_recent_dedup.go: 跨消息级别的发言去重。
//
// BUG-R70-P2 (2026-07-09): Bot 2号 DouBao 在 Day 1 内连续发出多条内容雷同的
// "我是 X 号"消息(典型: 9 条/6min,前 5 条完全相同),真人旁观者吐槽"怎么就
// 你一个人说话?"。SpeakLimiter 只能按时间间隔节流(45s/条),无法识别
// "30s 内说了相同主题"。
//
// 修复策略(廉价、不动 prompt):
//   - 每个 Agent 维护 recentSpeakTexts 环形缓冲(默认 5 条,最多 80 字)
//   - 新发言文本与缓冲任一条 Jaccard 相似度 ≥ 0.6 → 拒绝,返回 hint 提示 LLM
//   - 时间窗口: 默认 90s 内(超过窗口的旧发言不再参与比较,允许新一轮重复)
//   - 不动 BotTranscript / ChatService; 纯字符串 + 时间窗口过滤
package wwplayer

import (
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// defaultSpeakRecentWindow 默认去重时间窗口。
//
// R93 P2 (2026-07-11) 调整: 从 90s → 300s(5 分钟)。
// 旧 90s 窗口在 13 人局跨场景(如 Day 2 → Day 3 重启发言)时,
// 同一个 bot 会在相隔 1~2 分钟内说出"我是 X 号,大家先聊聊"等
// 完全相同 intro。真人观众吐槽"bot 怎么只会复读自己介绍"。
// 5 分钟覆盖一次完整 speak 阶段 + 一次 vote 阶段,足以捕捉
// 跨场景的复读,同时不会因为长 session 的误报抑制 bot 发言。
const defaultSpeakRecentWindow = 300 * time.Second

// defaultSpeakRecentCap 环形缓冲容量。
//
// R93 P2: 从 5 → 8,以覆盖较长的 5 分钟窗口内可能累积的发言数。
// 13 人局平均 1.3 回合/5 分钟,每回合单 bot 至少 1 条,留有冗余。
const defaultSpeakRecentCap = 8

// defaultSpeakJaccardThreshold Jaccard 相似度阈值;>= 该值视为重复。
//
// R93 P2: 从 0.6 → 0.5,放宽判定。旧 0.6 在短句(intro 类 10~20 字)
// 上,因为字符集合小,单字差异就让 Jaccard 跌到 0.55 而漏报。
// 0.5 仍然要求显著重合,但对短句更宽松。
const defaultSpeakJaccardThreshold = 0.5

// RecentSpeakDedup 是每个 Agent 一份的发言去重器。
type RecentSpeakDedup struct {
	mu        sync.Mutex
	cap       int
	window    time.Duration
	threshold float64
	texts     []string
	times     []time.Time
}

// NewRecentSpeakDedup 使用默认参数构造,供 game/werewolf.agentRunner 注入。
func NewRecentSpeakDedup() *RecentSpeakDedup {
	return &RecentSpeakDedup{
		cap:       defaultSpeakRecentCap,
		window:    defaultSpeakRecentWindow,
		threshold: defaultSpeakJaccardThreshold,
	}
}

// CheckAndRecord 判定新发言是否与最近 N 条重复;不重复则加入缓冲,返回
// (allowed, hint)。hint 是给 LLM 的反馈(下一轮可见),便于其收敛。
func (r *RecentSpeakDedup) CheckAndRecord(text string, now time.Time) (allowed bool, hint string) {
	if r == nil || text == "" {
		return true, ""
	}
	normalized := normalizeSpeakForCompare(text)

	r.mu.Lock()
	defer r.mu.Unlock()

	// 淘汰窗口外的旧条目
	cutoff := now.Add(-r.window)
	keptTexts := r.texts[:0]
	keptTimes := r.times[:0]
	for i := range r.times {
		if r.times[i].After(cutoff) {
			keptTexts = append(keptTexts, r.texts[i])
			keptTimes = append(keptTimes, r.times[i])
		}
	}

	// 比对
	for i := range keptTexts {
		sim := jaccardSimilarity(normalized, normalizeSpeakForCompare(keptTexts[i]))
		if sim >= r.threshold {
			return false, buildDupHint(keptTexts[i], sim)
		}
	}

	// 加入缓冲(环形)
	keptTexts = append(keptTexts, normalized)
	keptTimes = append(keptTimes, now)
	if len(keptTexts) > r.cap {
		keptTexts = keptTexts[len(keptTexts)-r.cap:]
		keptTimes = keptTimes[len(keptTimes)-r.cap:]
	}
	r.texts = keptTexts
	r.times = keptTimes
	return true, ""
}

// normalizeSpeakForCompare 标准化发言文本便于相似度比较:
//   - 转小写
//   - 全角字母数字转半角(ASCII 范围内手动处理)
//   - 移除所有空白字符
//   - 截断到 80 字(与 speak_dedup 上限一致)
//   - 标点保留(影响句意判断)
func normalizeSpeakForCompare(text string) string {
	if text == "" {
		return ""
	}
	// 截断
	if utf8.RuneCountInString(text) > 80 {
		runes := []rune(text)
		text = string(runes[:80])
	}
	var b strings.Builder
	b.Grow(len(text))
	for _, r := range text {
		// 全角空格 → 半角空格(后再剥离)
		if r == '　' {
			continue
		}
		// 全角 ASCII 字符(0xFF01..0xFF5E) → 半角
		if r >= 0xFF01 && r <= 0xFF5E {
			r -= 0xFEE0
		}
		// 剥离空白
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			continue
		}
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
}

// jaccardSimilarity 计算 Jaccard 相似度: |A ∩ B| / |A ∪ B|。
// 用 rune set 而非 n-gram,避免中文分词差异。
func jaccardSimilarity(a, b string) float64 {
	if a == "" || b == "" {
		return 0
	}
	if a == b {
		return 1
	}
	setA := runeSet(a)
	setB := runeSet(b)
	intersect := 0
	for r := range setA {
		if _, ok := setB[r]; ok {
			intersect++
		}
	}
	union := len(setA) + len(setB) - intersect
	if union == 0 {
		return 0
	}
	return float64(intersect) / float64(union)
}

func runeSet(s string) map[rune]struct{} {
	out := make(map[rune]struct{}, len(s))
	for _, r := range s {
		out[r] = struct{}{}
	}
	return out
}

// buildDupHint 构造给 LLM 的提示语,告知其当前发言与已有发言重复。
func buildDupHint(prev string, sim float64) string {
	pct := int(sim * 100)
	// 截断 prev 到 30 字避免 hint 过长
	prevRunes := []rune(prev)
	if len(prevRunes) > 30 {
		prev = string(prevRunes[:30]) + "..."
	}
	// 复用 memory.go 中已有的 itoa;这里直接用 strconv 简化
	return formatDupHint(pct, prev)
}

func formatDupHint(pct int, prev string) string {
	// 构造 "speak rejected: similarity NN% with recent message (\"...\"); please say something new on a different angle"
	var b strings.Builder
	b.WriteString("speak rejected: similarity ")
	b.WriteString(intToStr(pct))
	b.WriteString("% with recent message (\"")
	b.WriteString(prev)
	b.WriteString("\"); please say something new on a different angle")
	return b.String()
}

func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	negative := false
	if n < 0 {
		negative = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if negative {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}