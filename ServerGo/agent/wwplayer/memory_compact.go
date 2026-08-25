// Package wwplayer — memory_compact.go: LLM 驱动的记忆压缩。
// 灵感来源: PI Agent 的 compaction/compaction.ts — 结构化摘要 + 增量更新。
//
// 与 PI 的差异:
//   - PI 用独立 LLM 调用做摘要;狼人杀复用 bot 自己的 provider (节省 HTTP)
//   - PI 增量更新 (previousSummary + new);狼人杀全量重建 (单局生命周期短)
//   - PI 追踪文件操作;狼人杀追踪游戏事实 (已确认/待验证)
//   - PI 使用 contextWindow - reserveTokens 阈值;狼人杀用消息数 + 字节数
//
// 2026-08-20 §20260820-01 — 8 段结构化摘要 + 视角隔离改造。
// 旧 4 段(本局概况/已确认信息/关键决策/待验证信息)粒度过粗,13 人局多
// 神职(女巫/猎人/白痴/守卫/骑士/猎魔人)混进同一段,可读性差。本版改为:
//   S1 我的私有情报 — 神职/狼人专属(role 路由)
//   S2 已确认事实 — 场上公开发生且无歧义
//   S3 我的关键决策与理由
//   S4 玩家公开行为(按玩家编号归档)
//   S5 我对各玩家的阵营判断
//   S6 待验证信息
//   S7 当前局势提示(存活 + 屠边数)
//   S8 上次压缩以来的新增(仅增量模式)
//
// 公平性硬约束:prompt 内置「身份锁定声明」,引导 LLM 只输出该 bot 视角
// 可见的事实;新增 IsValidCompactSummary 校验关键词黑名单(预言家不得
// 出现"女巫用药"等其它神职私有信息),违例 → fallback。
package wwplayer

import (
	agentroot "LsmAgentGame/agent"
	"LsmAgentGame/agent/wwtypes"
	"LsmAgentGame/llm"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// COMPACT_SYSTEM_PROMPT_V2 压缩任务的 system prompt(2026-08-20 §20260820-01)。
// 与 PI 的 SUMMARIZATION_SYSTEM_PROMPT 对齐,但适配狼人杀游戏 8 段 schema + 视角隔离。
// 通过 buildCompactSystemPrompt(role, faction) 按 bot 身份动态注入 S1 字段名清单。
const COMPACT_SYSTEM_PROMPT_V2_TEMPLATE = `你是狼人杀游戏的记忆压缩助手。你的任务是把给定的对话历史压缩为 8 段结构化摘要,供后续 LLM 决策使用。

不要继续对话。不要回答对话中的问题。只输出结构化摘要。

**身份锁定声明**:
- 当前玩家: %s(%s/%s)
- **严格只输出你作为该身份能看见的事实**:
  - 预言家: 只输出 MySeerCheckHistory(每条: 夜 N 查 X 号 = 阵营)
  - 女巫: 只输出 WitchAntidoteUsed/PoisonUsed + 你的用药决策
  - 守卫: 只输出 GuardLastProtect + 你守过的人清单
  - 狼人: 只输出 WolfTeammateSeat + wolf_kill 历史 + 狼队暗号
  - 猎人/白痴/骑士/猎魔人/平民: 填"无独立私有情报"
- **禁止**基于其他神职的私有情报做推断(如预言家不应输出"女巫用了什么药")

**摘要必须包含以下 8 段**(段名固定,便于增量 diff;每段 ≤ 100 字):
## S1. 我的私有情报
## S2. 已确认事实
## S3. 我的关键决策与理由
## S4. 玩家公开行为
## S5. 我对各玩家的阵营判断
## S6. 待验证信息
## S7. 当前局势提示
## S8. 上次压缩以来的新增(仅增量模式)

**强制格式要求**:
- 玩家引用**必须**用"玩家编号#X"(X=1..13)格式,不允许"我"/"某人"/"该玩家"等指代不清的措辞
- S4 必须按玩家编号归档,每人一行
- 保持简洁,总长度 ≤ 800 字;关键决策/事实**不允许**省略`

// COMPACT_PROMPT_V2 压缩任务的 user prompt 模板(全量模式)。
const COMPACT_PROMPT_V2 = `以下是狼人杀游戏第 %d 局、玩家编号#%d(%s/%s)的对话历史。请压缩为 8 段结构化摘要。

<conversation>
%s
</conversation>

请按照 system prompt 中的格式输出 8 段摘要。重点关注:
1. S1: 你的私有情报(神职/狼人专属,严格只输出该 bot 视角可见的事实)
2. S2: 已发生的场上事件(夜 N 死 X、投票放逐 Y、警徽流等)
3. S3: 你做出的决策及理由(截取 speak_with_thought.internal_thought 的 3 条核心)
4. S4: 其他玩家发言/投票,按玩家编号#X 逐行归档
5. S5: 你的阵营判断(标注置信度:高/中/低)
6. S6: 仍存疑但需关注的信息
7. S7: 存活人数 + 神职/平民/狼 + 距胜利差几个`

// COMPACT_UPDATE_PROMPT_V2 压缩任务的 user prompt 模板(增量更新模式)。
//
// 2026-08-20 §20260820-01 — 在旧 PRESERVE+ADD 基础上增加「按段分桶」:
// 旧要点按 S1-S7 分类保留(便于 diff),新增内容按同样分类追加到 S8 子
// 桶,避免反复压扁/重建整篇摘要。
const COMPACT_UPDATE_PROMPT_V2 = `以下是狼人杀游戏第 %d 局、玩家编号#%d(%s/%s)的对话历史,以及你上一次压缩得到的 8 段摘要。

<previous_summary>
%s
</previous_summary>

<conversation>
%s
</conversation>

请输出一份**更新后**的 8 段摘要(格式同 system prompt),要求:

1. **PRESERVE 优先级**(从高到低):
   - S1 你的私有情报(神职/狼人独有,不可丢失)
   - S2 已确认事实(场上公开事件)
   - S3 你的关键决策与理由
   - S4/S5/S7 玩家行为 + 阵营判断 + 局势
2. **ADD**: 把 <conversation> 中的新事实**按 S1-S7 分类**追加,新增内容同时写入 S8 "上次压缩以来的新增"段(便于审计)
3. **DELETE/REWRITE**: 仅在旧要点被新事实明确推翻时才删除/改写它(例如某玩家已被确认死亡/放逐)
4. **保持简洁**: 总长度 ≤ 800 字;玩家引用必须用"玩家编号#X"格式`

// ─── 旧 4 段 schema 兼容常量(2026-08-20 §20260820-01 保留用于灰度回退) ───
//
// 当 cfg.EightSectionsEnabled=false 时,系统仍可走旧 prompt 模板。
// 本节常量与原有 §20260813-02 U1 完全一致;新增 EightSectionsEnabled 开关
// 在 game/werewolf/agent_compact_config.go 注入。

// COMPACT_SYSTEM_PROMPT 压缩任务的 system prompt(旧 4 段 schema,向后兼容)。
const COMPACT_SYSTEM_PROMPT = `你是一个游戏历史摘要助手。你的任务是将狼人杀游戏的对话历史压缩为结构化摘要,供后续 LLM 决策使用。

不要继续对话。不要回答对话中的问题。只输出结构化摘要。

摘要必须包含以下段落(可为空段写"暂无"):
## 本局概况
## 已确认信息
## 关键决策
## 待验证信息

保持简洁。保留具体的座位号、角色名、行动时间点。`

// COMPACT_PROMPT 旧 4 段 user prompt 模板。
const COMPACT_PROMPT = `以下是狼人杀游戏第 %d 局、座位 %d 号(%s/%s)的对话历史。请压缩为结构化摘要。

<conversation>
%s
</conversation>

请按照 system prompt 中的格式输出摘要。重点关注:
1. 本局基本态势(已知角色、存活人数、当前阶段)
2. 已确认的事实(查验结果、已知身份、已发生的行动)
3. 关键决策及其理由
4. 需要继续关注的疑点`

// COMPACT_UPDATE_PROMPT 旧 4 段增量更新 user prompt 模板。
const COMPACT_UPDATE_PROMPT = `以下是狼人杀游戏第 %d 局、座位 %d 号(%s/%s)的对话历史,以及你上一次压缩得到的摘要。

<previous_summary>
%s
</previous_summary>

<conversation>
%s
</conversation>

请输出一份**更新后**的结构化摘要(格式同 system prompt),要求:
1. PRESERVE — 保留上一次摘要中仍然有效的要点(已确认信息、关键决策);
2. ADD — 把 <conversation> 中的新事实追加到对应段落;
3. 仅在旧要点被新事实明确推翻时才删除/改写它;
4. 保留具体的座位号、角色名、行动时间点,保持简洁。`

// visiblePrivateFieldsByRole 是各 role 可见的私有情报字段名清单(用于 system prompt
// 注入 + IsValidCompactSummary 校验黑名单)。"none" 表示该身份无独立私有情报。
var visiblePrivateFieldsByRole = map[string][]string{
	"werewolf":      {"WolfTeammateSeat", "WolfKillHistory", "WolfPackCipher"},
	"seer":          {"MySeerCheckHistory"},
	"witch":         {"WitchAntidoteUsed", "WitchPoisonUsed", "WitchActHistory"},
	"guard":         {"GuardLastProtect", "GuardProtectHistory"},
	"hunter":        {"none"},
	"idiot":         {"none"},
	"knight":        {"KnightDuelHistory"},
	"demon_hunter":  {"DemonHunterHuntHistory"},
	"villager":      {"none"},
}

// privateInfoBlacklist 是「任何 bot 摘要都不应出现」的神职敏感字段黑名单(用于
// IsValidCompactSummary 校验)。当 bot 身份不是该字段的拥有者时,出现对应关键词
// 视为视角隔离违例 → 校验失败 → 触发 fallback。
//
// 黑名单逻辑: 字段名 → 仅当 bot.role != 字段拥有者时,该字段名在摘要中视为违规。
// 例如 "MySeerCheckHistory" 仅当 bot.role != "seer" 时是违规词。
var privateInfoBlacklist = map[string]string{
	"MySeerCheckHistory":    "seer",
	"WitchAntidoteUsed":     "witch",
	"WitchPoisonUsed":       "witch",
	"WitchActHistory":       "witch",
	"GuardLastProtect":      "guard",
	"GuardProtectHistory":   "guard",
	"WolfPackCipher":        "werewolf",
	"KnightDuelHistory":     "knight",
	"DemonHunterHuntHistory": "demon_hunter",
}

// CompactConfig 控制记忆压缩行为。
type CompactConfig struct {
	// Enabled 是否启用 LLM 压缩。
	Enabled bool
	// MinMessages 触发压缩的最小消息数。
	MinMessages int
	// MaxTokens LLM 压缩调用的 max_tokens。
	MaxTokens int
	// TimeoutSec 压缩调用的超时秒数。
	TimeoutSec int
	// EightSectionsEnabled 2026-08-20 §20260820-01 — 8 段结构化摘要开关。
	// true(默认) 使用 8 段 schema;false 退回旧 4 段(向后兼容,允许灰度)。
	EightSectionsEnabled bool
}

// DefaultCompactConfig 返回默认压缩配置。
//
// 2026-08-20 §20260820-01: MaxTokens 从 1024 提到 2048(8 段 × ~80 字 = 640 字
// + system 缓冲);新增 EightSectionsEnabled=true 默认值。
func DefaultCompactConfig() CompactConfig {
	return CompactConfig{
		Enabled:             true,
		MinMessages:         40,
		MaxTokens:           2048,
		TimeoutSec:          30,
		EightSectionsEnabled: true,
	}
}

// CompactResult 压缩操作的结果。
type CompactResult struct {
	Success        bool
	Summary        string
	MessagesBefore int
	MessagesAfter  int
	TokensUsed     int
	DurationMs     int64
	// Incremental 2026-08-13 §20260813-02 U1 — 本次是否走了增量更新模式
	// (上次摘要非空 → PRESERVE+ADD);供测试断言与日志观测。
	Incremental bool
	// 2026-08-20 §20260820-01 — 摘要质量校验字段。
	// SummaryLen 摘要字符串 rune 长度;SectionCount 实际解析到的 ## 段数。
	SummaryLen   int
	SectionCount int
	Error        error
}

// ─── 2026-08-13 §20260813-02 U1 — 上次摘要存取(Memory 字段的访问方法) ───

// LastCompactSummary 返回上一次 LLM 压缩产出的摘要(空 = 从未成功压缩)。
// 供 CompactWithLLM 决定走全量还是增量更新 prompt。
func (m *Memory) LastCompactSummary() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastCompactSummary
}

// setLastCompactSummary 写入最近一次成功压缩的摘要(仅成功路径调用)。
func (m *Memory) setLastCompactSummary(s string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastCompactSummary = s
}

// buildCompactSystemPrompt 2026-08-20 §20260820-01 — 按 bot 身份路由的 system prompt。
// 注入「身份锁定声明」与「视角隔离硬约束」;S1 字段名清单按 role 渲染差异。
//
// 入参:
//   - role     bot 身份("werewolf"/"seer"/"witch"/...)
//   - faction  bot 阵营("wolf"/"good"/...)
//
// 返回:完整 system prompt(已替换角色/阵营占位符)。
func buildCompactSystemPrompt(role, faction string) string {
	roleStr := role
	if roleStr == "" {
		roleStr = "unknown"
	}
	factionStr := faction
	if factionStr == "" {
		factionStr = "unknown"
	}

	// 注入该身份可见的私有情报字段清单。
	var privateBullets string
	if fields, ok := visiblePrivateFieldsByRole[role]; ok {
		if len(fields) == 1 && fields[0] == "none" {
			privateBullets = "  - 该身份无独立私有情报(填\"无\")"
		} else {
			for _, f := range fields {
				privateBullets += fmt.Sprintf("  - %s\n", f)
			}
		}
	} else {
		privateBullets = "  - 未知身份,无独立私有情报清单"
	}

	// 直接用 fmt.Sprintf 渲染整个模板。模板含 3 个 %s 占位符:
	//   第一个: role(玩家名,如 "seer")
	//   第二个: role(身份标记)
	//   第三个: faction(阵营标记)
	return fmt.Sprintf(COMPACT_SYSTEM_PROMPT_V2_TEMPLATE, roleStr, roleStr, factionStr) +
		"\n\n**该身份可见私有情报字段清单**:\n" + privateBullets
}

// buildCompactSystemPromptLegacy 旧 4 段 schema 的 system prompt(向后兼容)。
// EightSectionsEnabled=false 时使用。
func buildCompactSystemPromptLegacy() string {
	return COMPACT_SYSTEM_PROMPT
}

// buildCompactUserPrompt 构造压缩 user prompt(纯函数,便于单测直接断言)。
// 2026-08-20 §20260820-01: 8 段 schema 版本,prevSummary 非空 → 增量更新模式
// (PRESERVE+ADD);空 → 全量模式。
func buildCompactUserPrompt(eightSectionsEnabled bool, prevSummary string, round, seat int, role, faction, conversationText string) string {
	if eightSectionsEnabled {
		if prevSummary != "" {
			return fmt.Sprintf(COMPACT_UPDATE_PROMPT_V2, round, seat, role, faction, prevSummary, conversationText)
		}
		return fmt.Sprintf(COMPACT_PROMPT_V2, round, seat, role, faction, conversationText)
	}
	// 旧 4 段 schema
	if prevSummary != "" {
		return fmt.Sprintf(COMPACT_UPDATE_PROMPT, round, seat, role, faction, prevSummary, conversationText)
	}
	return fmt.Sprintf(COMPACT_PROMPT, round, seat, role, faction, conversationText)
}

// CountCompactSections 统计摘要中实际的 ## 段数。
// 兼容多种段头格式:`## S1.` / `### S1.` / `## S1 我的私有情报`。
// 2026-08-20 §20260820-01:8 段 schema 期望 ≥ 6 段(允许 S8 在全量模式下省略)。
var compactSectionRegex = regexp.MustCompile(`(?m)^#{2,3}\s+S[1-8]\.`)

// IsValidCompactSummary 2026-08-20 §20260820-01 — 摘要质量 + 视角隔离校验。
//
// 校验项:
//   - G6 摘要长度:SummaryLen >= 100(过短视为 LLM 输出不合格)
//   - G6 段数:SectionCount >= 6(全量模式期望 ≥ 7,允许 S8 省略)
//   - G3 视角隔离:检查 privateInfoBlacklist 中的关键词,若 bot.role !=
//     关键词拥有者,出现该关键词 → 违例
//
// 返回: (是否有效, 失败原因)。失败原因非空时表示校验失败,调用方应走 fallback。
func IsValidCompactSummary(summary string, botRole string, summaryLen, sectionCount int) (bool, string) {
	if summary == "" {
		return false, "summary empty"
	}
	if summaryLen < 100 {
		return false, fmt.Sprintf("summary too short: %d chars (< 100)", summaryLen)
	}
	if sectionCount < 6 {
		return false, fmt.Sprintf("section count too few: %d (< 6)", sectionCount)
	}
	// 视角隔离校验:仅当 botRole != 字段拥有者时,关键词违例。
	lowerSummary := strings.ToLower(summary)
	for keyword, ownerRole := range privateInfoBlacklist {
		if botRole == ownerRole {
			continue
		}
		if strings.Contains(lowerSummary, strings.ToLower(keyword)) {
			return false, fmt.Sprintf("perspective isolation violated: %q mentioned but bot role is %q (owner=%q)",
				keyword, botRole, ownerRole)
		}
	}
	return true, ""
}

// CompactWithLLM 使用 LLM 将旧消息压缩为结构化游戏摘要。
//
// 流程:
//  1. 检查消息数是否达到阈值
//  2. 分离: 旧消息 (待压缩,前 1/3) + 近端消息 (保留)
//  3. 构建压缩 prompt (旧消息序列化 + 游戏上下文 + 8 段 schema)
//  4. LLM 调用 (短超时,低 token)
//  5. 校验摘要质量(§20260820-01)+ 视角隔离 → 不合格走 fallback 标记
//  6. 替换: identity + compact摘要 + 近端消息
//
// 失败时不影响正常流程 (返回 error,调用方忽略即可)。
func (m *Memory) CompactWithLLM(
	ctx context.Context,
	provider llm.LLMProvider,
	apiKey string,
	modelKey string,
	gc *wwtypes.GameContext,
	cfg CompactConfig,
) CompactResult {
	if !cfg.Enabled {
		return CompactResult{Success: false, Error: fmt.Errorf("compact disabled")}
	}

	m.mu.RLock()
	msgCount := len(m.messages)
	m.mu.RUnlock()

	if msgCount < cfg.MinMessages {
		return CompactResult{Success: false, Error: fmt.Errorf("messages %d < min %d", msgCount, cfg.MinMessages)}
	}

	startTime := time.Now()

	// 1. 分离: 旧消息 (前 1/3) + 近端消息 (后 2/3)
	m.mu.Lock()
	msgs := make([]llm.Message, len(m.messages))
	copy(msgs, m.messages)
	m.mu.Unlock()

	splitIdx := msgCount / 3
	if splitIdx < 5 {
		splitIdx = 5 // 至少保留 5 条旧消息用于压缩
	}
	if splitIdx > msgCount-10 {
		splitIdx = msgCount - 10 // 至少保留 10 条近端消息
	}

	oldMsgs := msgs[1:splitIdx] // 跳过 identity (index 0)
	recentMsgs := msgs[splitIdx:]

	// 2. 序列化旧消息(2026-08-20 §20260820-01:按 mySeat 标注身份)
	seat := 0
	role := "unknown"
	faction := "unknown"
	round := 0
	if gc != nil {
		seat = gc.MySeat
		round = gc.Round
		if gc.Role != "" {
			role = gc.Role
		}
		if gc.Faction != "" {
			faction = gc.Faction
		}
	}
	conversationText := serializeMessagesForCompact(oldMsgs, seat)
	if len(conversationText) > 8000 {
		conversationText = conversationText[:8000] + "\n...(截断)"
	}

	// 3. 构建压缩 prompt(2026-08-20 §20260820-01:8 段 schema + 视角隔离)
	prevSummary := m.LastCompactSummary()
	incremental := prevSummary != ""
	userPrompt := buildCompactUserPrompt(cfg.EightSectionsEnabled, prevSummary, round, seat+1, role, faction, conversationText)

	// system prompt 按身份路由
	var sysPrompt string
	if cfg.EightSectionsEnabled {
		sysPrompt = buildCompactSystemPrompt(role, faction)
	} else {
		sysPrompt = buildCompactSystemPromptLegacy()
	}

	// 4. LLM 调用
	compactCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.TimeoutSec)*time.Second)
	defer cancel()

	req := llm.LLMRequest{
		Model:     modelKey,
		MaxTokens: cfg.MaxTokens,
		Messages: []llm.Message{
			{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: userPrompt}}},
		},
		System:         []llm.SystemBlock{{Type: "text", Text: sysPrompt}},
		AgentClassName: string(agentroot.AgentClassWerewolfMemoryCompact),
	}

	resp, err := provider.Chat(compactCtx, apiKey, req)
	if err != nil {
		return CompactResult{
			Success:        false,
			MessagesBefore: msgCount,
			Error:          fmt.Errorf("compact LLM call failed: %w", err),
		}
	}

	// 5. 提取摘要文本
	summary := extractCompactSummary(&resp)
	if summary == "" {
		return CompactResult{
			Success:        false,
			MessagesBefore: msgCount,
			Error:          fmt.Errorf("compact LLM returned empty summary"),
		}
	}

	// 6. 2026-08-20 §20260820-01 — 摘要质量 + 视角隔离校验。
	// 校验失败时:不写入 m.messages,也不写 lastCompactSummary;
	// 返回 Success=false(供调用方走 fallback)。
	summaryLen := len([]rune(summary))
	sectionCount := len(compactSectionRegex.FindAllString(summary, -1))
	if valid, reason := IsValidCompactSummary(summary, role, summaryLen, sectionCount); !valid {
		return CompactResult{
			Success:        false,
			Summary:        summary, // 保留供日志观测
			SummaryLen:     summaryLen,
			SectionCount:   sectionCount,
			MessagesBefore: msgCount,
			Error:          fmt.Errorf("compact summary invalid: %s", reason),
		}
	}

	// 7. 构建压缩摘要消息
	compactMsg := llm.Message{
		Role: "user",
		Content: []llm.ContentBlock{{
			Type: "text",
			Text: fmt.Sprintf("【游戏历史摘要·%d段】\n%s", sectionCount, summary),
		}},
	}

	// 8. 替换: identity + compact + recent
	// 2026-08-13 §20260813-02 U1 — 配对原子:压缩裁剪以 tool_use/tool_result
	// 配对为原子单位(OpenClaw Context §6.4)。recentMsgs 的头部可能是悬空的
	// tool_result(user 消息,其配对 tool_use 落在了被压缩的 oldMsgs 里),
	// 直接拼回会让严格代理(DouBao/DeepSeek)400 拒绝(§82b)。强制
	// dropLeadingOrphans 保证配对完整。
	recentMsgs = dropLeadingOrphans(recentMsgs)
	m.mu.Lock()
	m.messages = append([]llm.Message{msgs[0], compactMsg}, recentMsgs...)
	newCount := len(m.messages)
	m.mu.Unlock()
	// 成功路径记录摘要,供下一次增量更新(OpenClaw 迭代式摘要)。
	m.setLastCompactSummary(summary)

	duration := time.Since(startTime).Milliseconds()

	return CompactResult{
		Success:        true,
		Summary:        summary,
		SummaryLen:     summaryLen,
		SectionCount:   sectionCount,
		MessagesBefore: msgCount,
		MessagesAfter:  newCount,
		TokensUsed:     resp.Usage.InputTokens + resp.Usage.OutputTokens,
		DurationMs:     duration,
		Incremental:    incremental,
	}
}

// extractCompactSummary 从 LLM 响应中提取摘要文本。
func extractCompactSummary(resp *llm.LLMResponse) string {
	if resp == nil {
		return ""
	}
	var parts []string
	for _, block := range resp.Content {
		if block.Type == "text" && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// seatLabelInText 2026-08-20 §20260820-01 — 从 user text 中提取玩家编号。
// 支持多种格式:「玩家编号#3」「3号玩家」「座位3」「#3」。
// 提取失败时返回 -1(调用方降级为「玩家编号#?」)。
func seatLabelInText(text string) int {
	if text == "" {
		return -1
	}
	// 优先级 1:「玩家编号#N」(本项目最常用)
	re := regexp.MustCompile(`玩家编号#(\d{1,2})`)
	if m := re.FindStringSubmatch(text); len(m) >= 2 {
		var n int
		if _, err := fmt.Sscanf(m[1], "%d", &n); err == nil && n >= 1 && n <= 13 {
			return n
		}
	}
	// 优先级 2:「N号玩家」
	re = regexp.MustCompile(`(\d{1,2})号玩家`)
	if m := re.FindStringSubmatch(text); len(m) >= 2 {
		var n int
		if _, err := fmt.Sscanf(m[1], "%d", &n); err == nil && n >= 1 && n <= 13 {
			return n
		}
	}
	// 优先级 3:「座位N」
	re = regexp.MustCompile(`座位\s*(\d{1,2})`)
	if m := re.FindStringSubmatch(text); len(m) >= 2 {
		var n int
		if _, err := fmt.Sscanf(m[1], "%d", &n); err == nil && n >= 1 && n <= 13 {
			return n
		}
	}
	// 优先级 4:「#N」(避免和「#13」类编号混淆,要求 # 后紧跟数字)
	re = regexp.MustCompile(`#(\d{1,2})\b`)
	if m := re.FindStringSubmatch(text); len(m) >= 2 {
		var n int
		if _, err := fmt.Sscanf(m[1], "%d", &n); err == nil && n >= 1 && n <= 13 {
			return n
		}
	}
	return -1
}

// formatSeatLabel 2026-08-20 §20260820-01 — 把座位号格式化为"玩家编号#N"。
// seat=-1 表示来源不明,降级为「玩家编号#?」。
func formatSeatLabel(seat int) string {
	if seat < 1 || seat > 13 {
		return "玩家编号#?"
	}
	return fmt.Sprintf("玩家编号#%d", seat)
}

// serializeMessagesForCompact 将消息序列化为可读文本 (用于压缩 prompt)。
// 2026-08-20 §20260820-01 — 按 mySeat 标注每条消息的归属身份:
//   - assistant text → 默认按 mySeat("我自己")
//   - assistant tool_use → "我自己 决策: tool_name(args)"
//   - user text 含座位标识 → 提取并标注
//   - user tool_result → "玩家编号#? 返回" (除非能从 Content 中解析出座位)
//   - 无任何标识 → "玩家编号#?"
//
// mySeat 是当前 bot 的 0-indexed 座位号;用于把 assistant turn 标为"我自己"。
func serializeMessagesForCompact(msgs []llm.Message, mySeat int) string {
	mySeatLabel := formatSeatLabel(mySeat + 1) // 1-indexed
	var sb strings.Builder
	for _, msg := range msgs {
		role := msg.Role
		for _, block := range msg.Content {
			switch block.Type {
			case "text":
				if block.Text != "" {
					seatLabel := mySeatLabel
					if role == "user" {
						// 从 user text 中提取发言玩家
						if n := seatLabelInText(block.Text); n > 0 {
							seatLabel = formatSeatLabel(n)
						}
					}
					sb.WriteString(fmt.Sprintf("[%s] %s: %s\n", role, seatLabel, truncateCompact(block.Text, 200)))
				}
			case "tool_use":
				inputStr := "{}"
				if block.Input != nil {
					if b, err := json.Marshal(block.Input); err == nil {
						inputStr = string(b)
					}
				}
				sb.WriteString(fmt.Sprintf("[%s] %s 决策: %s(%s)\n", role, mySeatLabel, block.Name, truncateCompact(inputStr, 100)))
			case "tool_result":
				// tool_result 的 Content 是 []ContentBlock,需要提取文本
				contentStr := extractTextFromBlocks(block.Content)
				if block.IsError {
					contentStr = "ERROR: " + contentStr
				}
				seatLabel := "玩家编号#?"
				if n := seatLabelInText(contentStr); n > 0 {
					seatLabel = formatSeatLabel(n)
				}
				sb.WriteString(fmt.Sprintf("[%s] %s 返回[%s]: %s\n", role, seatLabel, block.ToolUseID, truncateCompact(contentStr, 200)))
			}
		}
	}
	return sb.String()
}

// extractTextFromBlocks 从 ContentBlock 切片中提取所有文本。
func extractTextFromBlocks(blocks []llm.ContentBlock) string {
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "; ")
}

// truncateCompact 截断字符串到指定长度。
func truncateCompact(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}