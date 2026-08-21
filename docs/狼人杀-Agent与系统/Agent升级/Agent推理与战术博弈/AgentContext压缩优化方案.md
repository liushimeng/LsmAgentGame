# 狼人杀 Agent Context 压缩优化方案（20260820-01）

> 提案人：Claude Code · 主 Agent
> 关联模块：`ServerGo/agent/wwplayer/memory_compact.go`（`LsmAgentGame-Werewolf-MemoryCompact`）
> 触发器：玩家 Bot（`LsmAgentGame-Werewolf-Player`）多轮上下文超过 13 人局长对局窗口阈值
> §编号：本文件本身定稿后登记为 `§20260820-01`

## 一、背景与问题诊断

### 1.1 现有压缩链路

| 链路 | 文件 | 行为 |
|------|------|------|
| LLM 语义压缩 | `memory_compact.go::CompactWithLLM` | 把前 1/3 旧消息发给一个 LLM 调用，让其返回 4 段（`本局概况`/`已确认信息`/`关键决策`/`待验证信息`）摘要，写回 `m.messages` |
| 增量更新 | `buildCompactUserPrompt` | 有上次摘要时切到 `PRESERVE+ADD` 模式（OpenClaw UPDATE_SUMMARIZATION_PROMPT 思想） |
| 失败回退 | `run_compact.go::maybeCompactMemory` | provider 失败 → 显式回退 `CompressHistoryLocked` + BotTranscript 留 `compactFallback=true` |
| 字节兜底 | `preflight_compress.go::preflightCompressLoop` | payload > 80% 预算 → `CompactWithLLM` + `CompressAndPrune`；400 后 → `PruneByBytesAggressive` |

### 1.2 现状缺陷（影响公平性与可观测性）

1. **身份信息被稀释** —— `serializeMessagesForCompact` 把所有 user/assistant 文本压成 `[role] text...`，但狼人杀 bot 视角里**「3 号玩家昨晚说了什么」**才决定决策。对话是「13 人公共广播 + 我自己/队友的工具调用」，现在混成一段流水。
2. **4 段式摘要粒度太粗** —— `已确认信息` 一段混进了「预言家金水」「女巫用药」「守卫守人」「警徽流」多个语义层级的确认；13 人局多神职（女巫/猎人/白痴/守卫/骑士/猎魔人 6 神职）单段装不下。
3. **跨 bot 公平性不对齐** —— 7 个 bot 各自走自己的 provider 调压缩，结论是「每个 bot 视角的事实片段」；当前 prompt 没有引导 LLM 「**只压缩属于你视角的事实**」（预言家可见金水历史、女巫可见用药历史、狼人可见狼队暗号 / 刀人历史），结果往往把不可见信息也"合理化"出来。
4. **增量更新的「PRESERVE」边界模糊** —— `COMPACT_UPDATE_PROMPT` 只说"保留旧要点/追加新事实"，没区分"已确认事实"vs"已被推翻的事实"vs"我方独有线索"。bot 视角下"队友死了"和"对手死了"语义完全不同。
5. **失忆风险** —— `MinMessages=40` 时 LLM 一次最多接收约前 13 条消息（`splitIdx = msgCount / 3`，下限 5），晚对局第 3-5 天的关键发言/投票常常在前 1/3 中被裁掉；压缩粒度过粗。
6. **没有「阶段边界」概念** —— 13 人局狼人杀一局分 8-12 个阶段（night_wolves → night_seer → night_witch → dawn → sheriff → speak → vote → death_lyric 等）。压缩窗口按条数切分，把"狼人 night_wolves 阶段的战术沟通"和"白天 speak 阶段的发散推演"混在一起，摘要可读性差。

## 二、设计目标

| # | 目标 | 度量 |
|---|------|------|
| G1 | **身份信息显式标注** —— 每条被压缩的消息必须显式归属 `玩家编号#X` 或「我自己」，**不允许**出现"我"或"某人"指代不清 | 压缩后 prompt 中 `玩家编号#X` token 覆盖率 ≥ 95% |
| G2 | **8 段式结构化摘要** —— 取代原 4 段，按神职 + 阵营维度拆分，每段 ≤ 80 字 | 摘要分段总数 ≥ 6 段；总长度 ≤ 800 字 |
| G3 | **按视角隔离** —— system prompt 显式声明"**只**记录你作为 `{role}/{faction}` 视角可见的事实"；不可见事实宁可空段也不要脑补 | 预言家 bot 摘要不出现女巫用药细节；女巫 bot 摘要不出现预言家金水身份 |
| G4 | **增量更新 PRESERVE 优先级** —— 显式列出 PRESERVE 4 级优先级（神职私有 > 阵营事实 > 玩家公开行为 > 闲聊） | prompt 含 4 级清单 |
| G5 | **阶段边界切分** —— 压缩窗口按"夜/日"二等分优先；窗口刚好覆盖完整日夜周期 | 压缩边界 ≥ 1 个完整日夜周期 |
| G6 | **失败可观测** —— 维持「禁止假成功」语义（OpenClaw Context §6.2）；新增摘要质量校验（长度 < 100 字视为失败 → 走 fallback） | CompactResult.SummaryLen < 100 → Success=false |
| G7 | **公平性** —— 所有 player agent 共用同一份压缩逻辑；只把"身份/阵营/角色"作为 prompt 变量注入 | 代码 100% 共用，仅 prompt 模板按 `role/faction` 渲染差异 |

## 三、压缩 Schema（8 段结构化摘要）

替换现有 4 段，新 schema 如下（**段名固定**，便于增量更新 diff）：

```
【本局 玩家编号#X / {role} / {faction} · 第 R 轮 · 阶段 {phase_desc} 摘要】

## S1. 我的私有情报
   （神职/狼人专属可见，按 role 路由：
   - 预言家：MySeerCheckHistory（每条: 夜 N 查 X 号 = 阵营）
   - 女巫：WitchAntidoteUsed/PoisonUsed + 我的用药决策
   - 守卫：GuardLastProtect + 我守过的人清单
   - 狼人：WolfTeammateSeat（开局已知）+ 历晚 wolf_kill 目标 + 狼队暗号）

## S2. 已确认事实（场上公开发生且无歧义）
   - 第 N 晚：X 号死亡（刀）/Y 号死亡（毒）
   - 第 M 轮投票：X 号被放逐（得 A 票）
   - 第 K 轮：Z 号翻牌白痴 / 猎人开枪带 Y 号
   - 警长徽流向：S → T1 → T2

## S3. 我的关键决策与理由
   - 夜 N：我 wolf_kill → X 号（理由：3 条核心）
   - 日 M：我 vote → Y 号（理由：3 条核心，截取自 speak_with_thought.internal_thought）

## S4. 玩家公开行为（按玩家编号归档，每人一行）
   - #1：日 M 发言 "..."，投票 Y
   - #3：日 M 发言 "..."，投票 X
   - #5：未发言 / 已死亡 / 已被放逐

## S5. 我对各玩家的阵营判断（仅我方视角，区分置信度）
   - #X：判定为狼（高置信度，因为：...）
   - #Y：判定为好人（中置信度，因为：...）
   - #Z：未定（缺少证据）

## S6. 待验证信息（仍存疑但需关注）
   - #A 自称预言家但与我的查验冲突 → 待验证
   - #B 第 N 晚被预言家金水但我未查 → 待验证

## S7. 当前局势提示（剩余座位 + 屠边数）
   - 存活：#1 #3 #5 #7 #9 #11（6 人）
   - 神职：1 / 平民：3 / 狼：2
   - 距胜利：好人需放逐全部 2 狼 / 狼需再刀 1 神职即屠边达标

## S8. 上次压缩以来的新增（仅增量模式下存在）
   - <新事件按 S1-S7 分类追加>
```

## 四、压缩窗口策略

替换现有 `splitIdx = msgCount / 3`（粗暴切分）：

```go
// 新策略（按"夜/日"切分）：
// 1. 找到 m.messages 中第 1 个 PhaseNight* 标记（按 steer message 中的 phase marker）；
//    若无标记，按 msgCount / 3 兜底。
// 2. 取该标记到 splitIdx 之间的消息 + splitIdx 之后 ≥ 1 个完整夜/日 → 作为 oldMsgs。
// 3. recentMsgs 保留最近 10-15 条（不变）。
```

简化方案（首版实现，避免引入 phase 标记解析）：**保持前 1/3 切分逻辑，但摘要 prompt 引导 LLM 按夜/日组织内容**，不改变窗口大小。这一项作为后续优化路线，本期不落地（避免过度工程化）。

## 五、视图字段 & 公平性约束

### 5.1 Prompt 内置硬约束（system + user 双层注入）

```
[system prompt 末尾追加]
你正在为 {role}/{faction} 视角的 玩家编号#{my_seat+1} 压缩记忆。
**严格只输出你作为该身份能看见的事实**：
- 预言家只输出 MySeerCheckHistory；
- 女巫只输出 WitchAntidoteUsed/PoisonUsed + 你的用药决策；
- 狼人只输出 WolfTeammateSeat + wolf_kill 历史 + 狼队暗号；
- 守卫只输出 GuardLastProtect + 你守过的人清单；
- 猎人/白痴/骑士/猎魔人：填"无独立私有情报"。
**禁止**基于其他神职的私有情报做推断（如预言家不应输出"女巫用了什么药"）。

[user prompt 末尾追加]
**身份锁定声明**：本局身份 {role}/{faction}，玩家编号 #{my_seat+1}，
全程视角锁定。不接受任何"如果我是预言家..."的假设性叙述。
```

### 5.2 全 Player Agent 共用同一函数

| 函数 | 行为 |
|------|------|
| `Memory.CompactWithLLM` | 复用现有签名，新增 `Role/Faction/MySeat` 从 `gc` 自动取 |
| `buildCompactSystemPrompt(role, faction string) string` | 按 role 路由，生成专属私有情报段落 S1 的字段名清单 |
| `buildCompactUserPrompt(prevSummary, round, seat, role, faction, conversationText) string` | 复用，按 role 渲染 S1 schema |
| `serializeMessagesForCompact(msgs, mySeat int)` | **新增**：消息按"说话人座位"标注 `玩家编号#X`。来源： |
|   | • assistant text → 默认按 mySeat（自方）； |
|   | • user text 含 `玩家编号#X` 模式 → 提取座位号； |
|   | • user text 含 `[来自 座位N]` 模式 → 提取； |
|   | • 其它 → 标 "玩家编号#?"。 |
|   | tool_use → 标注 `玩家编号#{mySeat+1} 决策`； |
|   | tool_result → 标注 `玩家编号#? 返回`（除非 Content 含可解析的 seat 标识）。 |

## 六、失败回退 & 质量校验

### 6.1 维持现有规则式 fallback

- `CompactWithLLM` 失败 → `CompressHistoryLocked` → BotTranscript 留 `compactFallback=true`（不变）。
- preflight 失败 → `PruneByBytesAggressive`（不变）。

### 6.2 新增摘要质量校验

```go
type CompactResult struct {
    // ... 现有字段
    SummaryLen int  // 摘要字符串 rune 长度，便于测试断言
    SectionCount int  // 实际解析到的 ## 段数
}

// 校验：SummaryLen < 100 或 SectionCount < 4 → 视作 LLM 输出不合格，
// 当作失败处理（保留 fallback 语义）。
```

### 6.3 wiring 测试强化

新增 3 个测试用例：

| 测试名 | 断言 |
|------|------|
| `TestCompact_Schema_EightSections` | 验证 fake provider 返回 8 段摘要 → Success=true 且 SectionCount ≥ 6 |
| `TestCompact_Fairness_PerspectiveIsolation` | 验证预言家 fake provider 返回含"女巫用药" → Success=false（视角隔离违例） |
| `TestCompact_QualityGuard_TooShort` | 验证 fake provider 返回 30 字"无内容" → Success=false（质量校验触发） |

## 七、配置 & 默认值

新增 `WerewolfConfig` 字段（沿用 §14.1 默认 true 模式）：

```go
// 2026-08-20 §20260820-01 — 8 段式压缩 + 视角隔离。
// CompactEightSectionsEnabled: true(默认) 启用 8 段结构化摘要;
//   false 退回旧 4 段（CompactSystemPrompt 旧版本）。
AgentCompactEightSectionsEnabled bool `json:"agent_compact_eight_sections_enabled"`
```

`MaxTokens` 默认值从 `1200` 提到 `2048`（8 段 × ~80 字 = 640 字 + system 缓冲）。
`MinMessages` 默认值保持 `40`（不变，避免频繁触发）。

## 八、文件改动清单

| # | 文件 | 改动 |
|---|------|------|
| 1 | `agent/wwplayer/memory_compact.go` | 替换 4 段 → 8 段 schema；新增 `buildCompactSystemPrompt(role,faction)`；新增 `serializeMessagesForCompact(msgs, mySeat)`；新增 `CountCompactSections(summary)`；新增 `IsValidCompactSummary(summary)`；CompactResult 加 `SummaryLen/SectionCount` |
| 2 | `agent/wwplayer/run_compact.go` | 调用 `IsValidCompactSummary` 校验，失败 → 走 fallback 路径 |
| 3 | `agent/wwplayer/memory_compact_wiring_test.go` | 新增 3 个测试（schema 8 段 / 视角隔离 / 质量校验）；更新 fake provider 的默认 summary 为 8 段 |
| 4 | `game/werewolf/agent_compact_config.go` | 新增 `cfgAgentCompactEightSectionsEnabled()`；`MaxTokens` 默认提到 2048 |
| 5 | `config/config.go` | `WerewolfConfig` 加 `AgentCompactEightSectionsEnabled bool` 字段；`applyDefaults` 置 `true` |
| 6 | `LsmAgentGame.conf.example` | 加 `werewolf.agent_compact_eight_sections_enabled: true`（占位示例） |
| 7 | `CLAUDE.md` §17 索引 | 登记本文件为 `§20260820-01`；新增「LLM / Provider / Wiring 类」条目一行 |

## 九、验收清单

- [ ] `go build -o LsmAgentGame main.go` 通过
- [ ] `go test ./agent/wwplayer/... -count=1` 全 PASS（含新增 3 个测试）
- [ ] `go test ./game/werewolf/... -count=1` 全 PASS
- [ ] `tsc --noEmit` 不变（前端无影响）
- [ ] `npm run build` 不变（前端无影响）
- [ ] 真实 LLM 压缩 1 次：fake provider 模拟 8 段摘要返回，验证 `Memory.CompactWithLLM` 成功且 `SectionCount ≥ 6`
- [ ] 视角隔离校验：fake provider 返回混入不可见信息 → 校验失败 → fallback 触发 → `compactFallback=true`
- [ ] 质量校验：fake provider 返回 30 字 → 校验失败 → fallback
- [ ] BotTranscript.CompactNote 文案包含"8段"标识（增量 vs 全量均可识别）

## 十、风险评估

| 风险 | 缓解 |
|------|------|
| LLM 视角隔离违例（预言家输出女巫用药） | `IsValidCompactSummary` 关键词黑名单 + 校验失败 → fallback |
| 8 段 schema 让 LLM 输出变长 → MaxTokens 不够 | 提到 2048（充裕）；增量模式只追加 S8，分摊 token 压力 |
| 增量更新时旧摘要已含被推翻事实 | schema 强制"旧要点仅在 S1-S7 中保留，新增推翻走 S2 已确认事实段" |
| 配置开关不生效（§130「声明了却从不接线」复发） | `cfgAgentCompactEightSectionsEnabled()` 用 defer recover 模式（与 §82b 同款） |
| 真实 LLM 偶尔省略 ## 标记 | `CountCompactSections` 用 regex 多模匹配（`^## ` / `^### ` / `^S[1-8]\.`） |
| 玩家编号#X 提取失败（user text 格式多样） | 失败时标 `玩家编号#?`，不阻塞压缩；wiring 测试覆盖 3 种 fallback |

## 十一、参考与索引

- `agent/wwplayer/memory_compact.go:21-65` — 当前 system/user prompt
- `agent/wwplayer/run_compact.go:67-120` — 触发与 fallback 链路
- `agent/wwplayer/preflight_compress.go:66-90` — preflight 双触发
- `docs/狼人杀-Agent与系统/狼人杀Agent设计.md` — Agent 总设计
- `docs/狼人杀-Agent与系统/狼人杀对话即思考设计.md` — §128 thinking 替代
- `CLAUDE.md §20260813-02 U1` — 现有 LLM 压缩骨架
- `CLAUDE.md §20260814-02` — 6 项升级参考
- `CLAUDE.md §130` — 「声明了却从不接线」反模式（必须 wiring 测试锁住）
- `CLAUDE.md §135` — 身份公开公平性原则（沿用至 S1 视角隔离）