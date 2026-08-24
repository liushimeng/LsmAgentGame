// Package thpagent — memory_compact.go: LLM 驱动的德扑记忆压缩(2026-08-24)。
//
// 移植自 wwplayer/memory_compact.go(8 段结构化摘要 + 视角隔离),但压缩
// 对象不同:
//   - 狼人杀: LLM messages 数组(对话历史)
//   - 德扑:  Memory 内的 HandRecord[] + OpponentStats(本局事实累计)
//
// 4 段 schema(对齐 memory_persist.go 两段语义,但用词精确区分):
//   ## S1 风格画像      (本局打法风格与教训)
//   ## S2 对手笔记      (按座位归档的对手画像与针对性策略)
//   ## S3 关键决策与理由 (摊牌/关键 bluff/被诈唬时的判断与教训)
//   ## S4 当前局势提示   (存活筹码分布 + 大盲位置)
//
// 公平性:每 bot 单独压缩,仅写入自己的 lastCompactSummary,不互相共享;
// 信息账本隔离天然到位(德扑无神职私有情报)。
//
// 触发点(§3.4 压缩梯度 Tier100 之后):driver.go::applyDecisionCompression
// 在 prompt >= budget 时先 ApplyPromptCompression(Tier100);若仍超预算
// (chat_window 大段未压缩完),再走 Memory.CompactWithLLM 把 HandRecord
// 压缩为 4 段摘要挂回 lastCompactSummary,buildRecentHandHistoryBlock
// 下次渲染优先读摘要。
//
// 失败回退:IsValidCompactSummary 校验失败 → 不写入 lastCompactSummary,
// 保留旧 HandRecord 列表(BuildRecentHandHistoryBlock 仍渲染历史手牌)。
package thpagent

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	agentroot "LsmAgentGame/agent"
	"LsmAgentGame/llm/types"
)

// AgentClassTexasHoldemMemoryCompact 与 class_names.go 同名常量(§130 防
// 「声明了却从不接线」复发)集中管理在 class_names.go;此处仅引用,不改值。
var _ = agentroot.AgentClassTexasHoldemMemoryCompact

// CompactConfig 是 LLM 压缩任务的配置(对齐 wwplayer::CompactConfig)。
// driver 在初始化阶段构造;CompactWithLLM 按 Enabled 决定是否跑。
type CompactConfig struct {
	Enabled     bool
	MinHands    int           // 触发阈值(默认 4 手)
	TimeoutSec  int           // LLM 调用超时(默认 60s,§197 长预算兜底)
	MaxTokens   int           // LLM 输出 token 上限(默认 1024)
	MaxInputKB  int           // 压缩输入字节上限(默认 8KB,超则截断)
}

// DefaultTexasCompactConfig 返回默认配置。
func DefaultTexasCompactConfig() CompactConfig {
	return CompactConfig{
		Enabled:    true,
		MinHands:   4,
		TimeoutSec: 60,
		MaxTokens:  1024,
		MaxInputKB: 8,
	}
}

// CompactResult 是 CompactWithLLM 的返回结构(对齐 wwplayer::CompactResult)。
type CompactResult struct {
	Success       bool
	Summary       string
	SectionCount  int
	HandsBefore   int
	HandsAfter    int
	BytesSaved    int
	LatencyMS     int64
	Error         error
}

// ─── 4 段 schema system + user prompt 模板 ───

// COMPACT_SYSTEM_PROMPT 德扑版压缩 system prompt(2026-08-24 §3.4)。
// 与 wwplayer 8 段版本相比,德扑无神职私有情报,改为 4 段结构化摘要。
const COMPACT_SYSTEM_PROMPT = `你是德州扑克 AI 玩家的记忆压缩助手。你的任务是把给定的本局事实序列(手牌回顾 + 对手累计统计)压缩为 4 段结构化摘要,供后续 LLM 决策使用。

不要继续对话。不要回答对话中的问题。只输出结构化摘要。

**身份锁定声明**:
- 当前玩家: 德州扑克 Bot(6-max No-Limit Hold'em,公开信息对所有玩家可见)
- 你只能输出以下 4 段,**禁止** 4 段以外的小标题、前言或结语

**摘要必须包含以下 4 段**(段名固定,便于增量 diff;每段 ≤ 250 字):
## S1 风格画像
## S2 对手笔记
## S3 关键决策与理由
## S4 当前局势提示

S1 写你自己的打法风格与教训(基于本局统计:VPIP/弃牌率/虚张命中率等)。
S2 按座位号归档每个对手的画像(弃牌率/加注频率/净盈亏)与针对性策略。
S3 摊牌手/关键 bluff/被诈唬时的判断与教训,挑最重要的 2-3 条。
S4 当前存活筹码分布、大盲位置、连败/连胜势头提示。`

// COMPACT_UPDATE_PROMPT 增量更新模式(已有 prevSummary)user prompt。
const COMPACT_UPDATE_PROMPT = `你是德州扑克 AI 玩家的记忆压缩助手。下面是你【上一次摘要】(本局已积累的经验)+ 【本局新增事实】(最近一手牌回顾 + 累计对手统计)。请基于新增事实更新你的经验库,输出一份全新的、完整的 4 段摘要(不是 diff)。

**硬性格式要求**:
- 必须严格包含以下 4 段,顺序不可调换,缺一段视为失败:
## S1 风格画像
## S2 对手笔记
## S3 关键决策与理由
## S4 当前局势提示
- 每段 ≤ 250 字;无内容时填"暂无",不要省略标题
- 不要输出 4 段以外的解释、前言或结语

【上一次摘要】
%s

【本局新增事实】
%s`

// COMPACT_PROMPT 全量模式(首次压缩)user prompt。
const COMPACT_PROMPT = `你是德州扑克 AI 玩家的记忆压缩助手。下面是【本局所有事实】(最近一手牌回顾 + 累计对手统计)。请基于这些信息输出你的经验库首版,4 段结构化摘要。

**硬性格式要求**:
- 必须严格包含以下 4 段,顺序不可调换,缺一段视为失败:
## S1 风格画像
## S2 对手笔记
## S3 关键决策与理由
## S4 当前局势提示
- 每段 ≤ 250 字;无内容时填"暂无",不要省略标题
- 不要输出 4 段以外的解释、前言或结语

【本局所有事实】
%s`

// ─── Memory 字段扩展 + 访问方法 ───

// lastCompactSummary 是 Memory 新增字段,本文件内通过 setter/getter 维护
// (Memory struct 在 memory.go,本文件不持有其字段;此处通过独立函数封
// 装压缩产物到 memory.go 的现有字段:LastDecisionSummary)。
//
// 设计决定:复用 LastDecisionSummary 字段而非扩 Memory struct,
// 与"风格画像 + 对手笔记"持久化层(MEMORY.md)语义解耦 —
// LastCompactSummary 是**本局**压缩产物(给下次决策用),
// LastDecisionSummary 是**最近一次决策**摘要(给历史回顾用)。

// SetLastCompactSummary 写入最近一次成功压缩的摘要(仅成功路径调用)。
// 复用 LastDecisionSummary 字段(见文件头注释)。
func (m *Memory) SetLastCompactSummary(s string) {
	m.SetLastDecisionSummary(s)
}

// LastCompactSummary 返回上一次 LLM 压缩产出的摘要(空 = 从未成功压缩)。
func (m *Memory) LastCompactSummary() string {
	return m.GetLastDecisionSummary()
}

// ─── 段数校验 ───

// compactSectionRegex 匹配 `## S[1-4]` 或 `### S[1-4]` 段头。
var compactSectionRegex = regexp.MustCompile(`(?m)^#{2,3}\s+S[1-4]\b`)

// IsValidTexasCompactSummary 校验摘要是否合格(段数 + 长度 + 段名清单)。
// 校验失败 → 走 fallback,保留旧 HandRecord 列表。
func IsValidTexasCompactSummary(summary string) (bool, string) {
	if summary == "" {
		return false, "summary empty"
	}
	summaryLen := len([]rune(summary))
	if summaryLen < 80 {
		return false, fmt.Sprintf("summary too short: %d chars (< 80)", summaryLen)
	}
	missing := []string{}
	for _, sec := range []string{"S1 风格画像", "S2 对手笔记", "S3 关键决策与理由", "S4 当前局势提示"} {
		if !strings.Contains(summary, sec) {
			missing = append(missing, sec)
		}
	}
	if len(missing) > 0 {
		return false, fmt.Sprintf("missing sections: %v", missing)
	}
	sectionCount := len(compactSectionRegex.FindAllString(summary, -1))
	if sectionCount < 4 {
		return false, fmt.Sprintf("section count too few: %d (< 4)", sectionCount)
	}
	return true, ""
}

// ─── 序列化辅助 ───

// serializeMemoryForCompact 把 HandRecord[] + OpponentStats 序列化为
// LLM 可读的文本(纯函数,便于单测)。bytes 超 MaxInputKB 时截断。
func serializeMemoryForCompact(hands []HandRecord, stats map[string]*OpponentStat, maxKB int) string {
	var b strings.Builder
	b.WriteString("【最近手牌回顾】(共 ")
	str2 := itoaSmall(len(hands))
	b.WriteString(str2)
	b.WriteString(" 手)\n")
	for _, h := range hands {
		winner := "无人"
		if len(h.Winners) > 0 {
			winner = fmt.Sprintf("%v", h.Winners)
		}
		fmt.Fprintf(&b, "  #%d: 我的底牌=[%d,%d], 社区=%d张(净盈亏%+d, 赢家=%s)\n",
			h.HandNumber, h.MyHole[0], h.MyHole[1], h.CommunityLen, h.NetChipDelta, winner)
	}
	if len(stats) == 0 {
		b.WriteString("\n【对手统计】(无)\n")
	} else {
		b.WriteString("\n【对手统计】\n")
		for _, st := range stats {
			fmt.Fprintf(&b, "  座位%d: 共%d手 弃牌率%.0f%% 净盈亏%+d (fold=%d call=%d raise=%d allin=%d)\n",
				st.Seat+1, st.HandsPlayed, st.FoldRate*100, st.NetChips,
				st.TotalFold, st.TotalCall, st.TotalRaise, st.TotalAllIn)
		}
	}
	out := b.String()
	if maxKB <= 0 {
		maxKB = 8
	}
	maxBytes := maxKB * 1024
	if len(out) > maxBytes {
		// 字节截断(rune 边界尽量保留),防止 LLM 输入超限
		out = out[:maxBytes] + "\n...(截断)"
	}
	return out
}

// itoaSmall 是 strconv.Itoa 的内联小整数版,避免在本文件 import strconv。
func itoaSmall(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [12]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	out := string(buf[pos:])
	if neg {
		return "-" + out
	}
	return out
}

// buildCompactUserPrompt 构造 user prompt(全量 / 增量)。
func buildCompactUserPrompt(prevSummary, facts string) string {
	if prevSummary != "" {
		return fmt.Sprintf(COMPACT_UPDATE_PROMPT, prevSummary, facts)
	}
	return fmt.Sprintf(COMPACT_PROMPT, facts)
}

// ─── 主入口:CompactWithLLM ───

// CompactWithLLM 使用 LLM 把 Memory 内的 HandRecord[] + OpponentStats
// 压缩为 4 段结构化摘要。
//
// 流程:
//  1. 检查 HandRecord 数是否达到 MinHands(默认 4 手)
//  2. 序列化 HandRecord + OpponentStats(超 MaxInputKB 截断)
//  3. 构造 prompt(全量 / 增量,看 prevSummary)
//  4. LLM 调用(短超时)
//  5. IsValidTexasCompactSummary 校验(段数 + 段名清单) → 不合格走 fallback
//  6. 成功则写入 Memory.LastCompactSummary
//
// 失败时 Success=false,调用方忽略即可;不影响正常决策链。
func (m *Memory) CompactWithLLM(
	ctx context.Context,
	provider types.LLMProvider,
	apiKey string,
	modelKey string,
	cfg CompactConfig,
) CompactResult {
	if !cfg.Enabled {
		return CompactResult{Success: false, Error: fmt.Errorf("compact disabled")}
	}
	if provider == nil || apiKey == "" {
		return CompactResult{Success: false, Error: fmt.Errorf("provider or apiKey empty")}
	}

	m.mu.Lock()
	handCount := len(m.RecentHands)
	handsCopy := make([]HandRecord, len(m.RecentHands))
	copy(handsCopy, m.RecentHands)
	statsCopy := make(map[string]*OpponentStat, len(m.OpponentStats))
	for k, v := range m.OpponentStats {
		c := *v
		statsCopy[k] = &c
	}
	prevSummary := m.LastDecisionSummary
	m.mu.Unlock()

	if handCount < cfg.MinHands {
		return CompactResult{
			Success:     false,
			HandsBefore: handCount,
			Error:       fmt.Errorf("hands %d < min %d", handCount, cfg.MinHands),
		}
	}

	startTime := time.Now()

	facts := serializeMemoryForCompact(handsCopy, statsCopy, cfg.MaxInputKB)
	userPrompt := buildCompactUserPrompt(prevSummary, facts)

	req := types.LLMRequest{
		Model:    modelKey,
		MaxTokens: cfg.MaxTokens,
		Messages: []types.Message{
			{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: userPrompt}}},
		},
		System:         []types.SystemBlock{{Type: "text", Text: COMPACT_SYSTEM_PROMPT}},
		// §24 出站 UA:德扑记忆压缩专属 AgentClassName
		AgentClassName: string(agentroot.AgentClassTexasHoldemMemoryCompact),
	}

	timeout := cfg.TimeoutSec
	if timeout <= 0 {
		timeout = 60
	}
	compactCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	resp, err := provider.Chat(compactCtx, apiKey, req)
	if err != nil {
		return CompactResult{
			Success:     false,
			HandsBefore: handCount,
			LatencyMS:   time.Since(startTime).Milliseconds(),
			Error:       fmt.Errorf("compact LLM call failed: %w", err),
		}
	}

	summary := strings.TrimSpace(resp.Text())
	if summary == "" {
		return CompactResult{
			Success:     false,
			HandsBefore: handCount,
			LatencyMS:   time.Since(startTime).Milliseconds(),
			Error:       fmt.Errorf("compact LLM returned empty summary"),
		}
	}

	summaryLen := len([]rune(summary))
	_ = summaryLen
	sectionCount := len(compactSectionRegex.FindAllString(summary, -1))
	if valid, reason := IsValidTexasCompactSummary(summary); !valid {
		return CompactResult{
			Success:      false,
			Summary:      summary, // 保留供日志观测
			SectionCount: sectionCount,
			HandsBefore:  handCount,
			LatencyMS:    time.Since(startTime).Milliseconds(),
			Error:        fmt.Errorf("compact summary invalid: %s", reason),
		}
	}

	// 成功路径:写入 LastCompactSummary(复用 LastDecisionSummary 字段)
	m.SetLastCompactSummary(summary)

	bytesSaved := len(facts) - len(summary)
	if bytesSaved < 0 {
		bytesSaved = 0
	}

	return CompactResult{
		Success:      true,
		Summary:      summary,
		SectionCount: sectionCount,
		HandsBefore:  handCount,
		HandsAfter:   handCount, // 摘要不删 HandRecord,仅挂摘要
		BytesSaved:   bytesSaved,
		LatencyMS:    time.Since(startTime).Milliseconds(),
	}
}