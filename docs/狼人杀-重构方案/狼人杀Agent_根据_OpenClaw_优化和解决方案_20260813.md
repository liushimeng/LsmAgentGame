# 狼人杀 Agent 根据 OpenClaw 优化和解决方案(§20260813-02)

> 日期: 2026-08-13
> 依据文档:
> - [`docs/其他Agent代码分析/OpenClaw_记忆管理分析.md`](../其他Agent代码分析/OpenClaw_记忆管理分析.md)
> - [`docs/其他Agent代码分析/OpenClaw_Context管理分析.md`](../其他Agent代码分析/OpenClaw_Context管理分析.md)
> - [`docs/其他Agent代码分析/OpenClaw_意图识别与任务分解分析.md`](../其他Agent代码分析/OpenClaw_意图识别与任务分解分析.md)
> 分析对象: OpenClaw(开源个人 AI 助手,多渠道接入,自主任务执行)
> 目标: 将 OpenClaw 的记忆管理 / Context 管理 / 任务处理三类设计模式,映射为狼人杀 Agent 的可落地升级

## 1. 现状诊断(狼人杀 Agent 短板)

基于对 `ServerGo/agent/`(~25.7K 行)与 `ServerGo/game/werewolf/`(~54.3K 行)的全面审计,当前实现是**事件驱动、单 goroutine 反应式、工具完备、安全网极厚**的游戏 Agent。与 OpenClaw 对照,最薄弱的环节按优先级:

| # | 短板 | 现状 | OpenClaw 对照 |
|---|------|------|---------------|
| S1 | **局内 LLM 语义压缩「已写未接线」** | `memory_compact.go::CompactWithLLM` 完整实现(PI Agent 借鉴),但 `compactConfig`(agent.go:270)**无任何 setter,Enabled 恒 false**,run.go:693 判断永不触发;局内压缩退化为规则式 `buildHistorySummary`(提取前 30 字拼接) | OpenClaw compaction 是一等公民:多入口触发 + 迭代式摘要 + 事务化执行 + 显式失败禁止假成功 |
| S2 | **工具定义每轮重建,prompt cache 前缀漂移** | run.go 每轮直接调 `BuildTools` 全量重建 ~30 个工具定义;`tools_cache.go::BuildToolsCached`(按 phase+role+aliveHash 缓存)20260813 新增但**零生产调用点** | OpenClaw 全部缓存设计围绕「前缀字节稳定」:system prompt 分 stable/dynamic 两段 + sha256 LRU + 只给最后一个工具打 cache breakpoint |
| S3 | **被折叠的早期发言不可召回** | bot 只能看到 500K chatQueue 窗口内**最近 60 条**;早期关键发言(如 R1 预言家跳身份)被 4 级压缩折叠后**永久不可召回**;`RecapLastNRounds` 存在但无 Agent 侧调用 | OpenClaw Lane-1:`memory_search` 工具让模型**主动检索**情景层(日记/转录),零模型调用、闭集校验、冷却限流、截断留可观测标记 |
| S4 | **跨局记忆迭代链路脆弱** | `IterateAgentMemoriesAsync` 用非流式 `provider.Chat` + 90s 硬超时,慢模型频繁走 FallbackMerge;**不走 §197 parentCtx+extendedTimeout 长预算**;`ValidateMemorySections` 只校验 4 段标题存在,无内容丢失比例校验 | OpenClaw consolidation:逐条结构化校验 + `maxPriorEntryLossFraction(0.25)` 丢失比例上限 + 任一失败回退 append-only + preimage 留痕 |
| S5 | **steeringQueue/toolHooks 两件套死代码** | §20260811-01 借鉴 PI Agent 的 steeringQueue(实时引导注入)与 toolHooks(FactCheck/Quota hook)**均无 setter、零生产接线** | OpenClaw transformContext 是每轮模型调用前的统一扩展点,所有在线修剪/引擎 hook 挂在同一 wrapper 链 |
| S6 | **wiring lint 覆盖不全** | §20260812-04 U6 的 `wiring_lint_test.go` 只覆盖块函数/SkipPhaseAction/夜间私有字段三类,**不覆盖「新增缓存/新增缓冲/新增配置」类死代码** | OpenClaw 无对应物,但这是本项目 §130 六次复发的专属病 |

**优势项(不应削弱)**:watchdog/skip/quarantine/熔断/冷却窗/重试分类这套 10+ 轮事故驱动迭代出的安全网,是本项目相对外部框架的核心优势。所有升级必须遵守:§92a 锁内变体、§119 协议层隔离、§197 流式长预算、§135 隐私护栏。

## 2. OpenClaw 模式 → 狼人杀映射总表

| OpenClaw 模式 | 出处(分析文档) | 狼人杀落点 | 本期实施 |
|---|---|---|---|
| 迭代式摘要(previousSummary 增量更新,非全量重来) | Context §6.2 | U1 CompactWithLLM 接线 + 增量摘要 prompt | ✅ |
| 压缩失败必须显式失败,禁止假成功 | Context §6.2 | U1 失败回退规则压缩 + 可观测标记 | ✅ |
| 裁剪以 tool_use/tool_result 配对为原子单位 | Context §6.4 | U1 复用既有 dropLeadingOrphans + 新增断言 | ✅ |
| 前缀字节稳定(sha256 LRU + 批量驱逐) | Context §3/§7 | U2 ToolsCache 接线;Prune 阈值化批量驱逐 | ✅ |
| Lane-1 主动检索工具(memory_search) | 记忆 §4 | U3 `chat_recall` 工具(关键词/座位/天数过滤) | ✅ |
| 闭集参数校验(模型不能扩大召回边界) | 记忆 §4.4 | U3 corpus 等价物:seat/day 范围服务端钳制 | ✅ |
| 工具冷却 + 截断可观测标记 | 记忆 §4.4 | U3 per-bot 冷却 60s + `[已截断]` 标记 | ✅ |
| maxPriorEntryLossFraction 丢失比例上限 | 记忆 §2.4c | U4 记忆迭代校验增强 | ✅ |
| 压缩/迭代走与主会话相同的长预算管线 | Context §6.3 | U4 迭代改流式 + §197 parentCtx | ✅ |
| 高敏决策「封闭词表短路 + 小模型兜底 + 安全默认」 | 任务 §1.2 | U4 迭代失败永远 FallbackMerge(已有) | ✅(强化) |
| wiring lint 机制化(声明未接线 = 编译级失败) | 任务 §12(技能接线校验思想) | U5 wiring_lint 扩展三类断言 | ✅ |
| update_plan 展示型 todo | 任务 §3 | 狼人杀局内节奏不适用(单轮反应式) | ❌ 不引入 |
| subagent spawn/swarm | 任务 §4 | 游戏内无多代理编排需求 | ❌ 不引入 |
| standing intents 前瞻记忆 | 记忆 §6.3 | 跨局记忆已有;前瞻意图无游戏场景 | ❌ 不引入 |
| embedding 向量检索 | 记忆 §3.3 | 需引入 embedding infra,成本远超收益;规则检索已够 | ⏸ P2 再议 |
| steeringQueue/toolHooks 接线 | — | 观众消息已有 §112 全频唤醒通道,价值重复 | ⏸ P1 评估删除 |
| ShortMemoryBuffer 接线 | — | §20260813-01 新增,意图待确认 | ⏸ P1 跟进 |

## 3. 本期实施项(P0,U1–U5)

### U1: 激活 LLM 语义压缩(CompactWithLLM 接线)

**OpenClaw 依据**:compaction 多入口触发 + 迭代式摘要 + 显式失败 + 配对原子(Context §6)。

**设计**:

1. **配置**:新增 `werewolf.agent_compact_enabled`(默认 true)、`agent_compact_max_tokens`(默认 1200)。`cfgAgentCompactConfig()` 读取 config,代码内常量兜底(config.Load panic 安全,§197 教训 3)。
2. **接线**:`StartAgentsLocked` 为每个 bot `ag.SetCompactConfig(CompactConfig{Enabled, MaxTokens, Provider: a.Provider, APIKey: ...})`(新增 setter);法官与记忆迭代不挂(它们有自己的压缩语义)。
3. **触发点**:run.go:693 的既有判断真正生效 —— `CompressAndPrune` 时若 `compactConfig.Enabled` 且消息数 > 阈值,先走 `CompactWithLLM`(LLM 语义摘要替换最早 N 轮),**失败必须显式回退到规则式 `CompressHistoryLocked`** 并在 BotTranscript 留 `LastCompactFallback=true` 可观测标记(禁止假成功)。
4. **迭代式摘要**:Memory 新增 `lastCompactSummary string` 字段;`CompactWithLLM` 的 prompt 在已有上次摘要时切换为「增量更新」模式(PRESERVE 旧摘要要点 + ADD 新增内容),对齐 OpenClaw `UPDATE_SUMMARIZATION_PROMPT` 思想。
5. **配对原子**:压缩后强制 `dropLeadingOrphans` + 断言无悬空 tool_use(复用 §82b 机制)。
6. **预算**:压缩 LLM 调用走房间 `llmSema` 信号量(不占 speak 限流器);失败**不计入 consecutiveFailures**(§112 speak_floor 同款约束);超时 60s,慢模型降级规则压缩。
7. **测试**:`memory_compact_wiring_test.go` —— setter 接线断言 / 触发路径断言 / 失败回退断言 / 增量摘要 prompt 断言 / 配对完整性断言。经「还原未接线 → 测试失败 → 接线 → 测试通过」双向验证。

### U2: ToolsCache 生产接线(工具定义字节稳定)

**OpenClaw 依据**:前缀字节稳定是 prompt cache 命中的前提(Context §7.2);只给最后一个工具打 cache breakpoint 的前提是工具数组字节稳定。

**设计**:

1. run.go 内层循环的 `BuildTools(phase, role, seat, alive, speakTurn, gc)` 调用替换为 `BuildToolsCached(...)`;cache key 已有(phase+role+aliveHash+speakTurn)。
2. **失效正确性**:aliveHash 必须覆盖存活位图变化;speakTurn 变化必须换 key —— 新增单测断言「同一 key 两次调用返回同一 slice 头指针(或深等于)」「alive 变化后 key 不同」。
3. **可观测**:BotTranscript 增加 `ToolsCacheHit bool`(每轮记录),便于事后验证命中率。
4. **wiring lint**:U5 新增「导出构造函数必须有非测试调用点」断言,使 ToolsCache/ShortMemoryBuffer 类死代码下次直接咬人。
5. **测试**:缓存命中/失效/枚举剔除(死亡座位)正确性;`go test` 全绿。

### U3: `chat_recall` 主动检索工具(被折叠历史可召回)

**OpenClaw 依据**:Lane-1 `memory_search` —— 模型主动检索情景层;闭集参数校验;60s per-agent 冷却;截断留可观测标记(记忆 §4.4)。

**设计**:

1. **工具定义**(挂接 `BuildTools`,仅在白天 speak/vote 阶段挂载,夜间不挂 —— 夜间行动靠私有信息块,检索无意义且拖慢节奏):
   ```
   chat_recall(query: string 必填≤50字, seat?: int, day?: int) → {results: [{seq, day, speaker_seat, speaker_name, text}], truncated: bool}
   ```
2. **检索实现**(`game/werewolf/chat_recall.go`):
   - 数据源:房间 `chatQueue` 全量快照(500K 窗口内的**全部**条目,包括已被 4 级压缩折叠的 —— 压缩只影响 prompt 渲染窗口,队列本体保留原始条目;若压缩已物理淘汰,则在结果中如实返回 `truncated:true`)。
   - 打分(零模型调用,规则式):关键词命中次数×2 + 座位匹配×3 + 天数匹配×2 + 发言人类型(公开发言 > 私聊 > 活动事件);取 Top 5,每条截 120 字。
   - **闭集钳制**:`seat` 越界 → 忽略(不报错);`day` 钳到 [1, currentDay];query 超 50 字截断 —— 模型自撰参数不能扩大召回边界(OpenClaw `readCorpusParam` 思想)。
   - **隐私护栏**:只能检索**公开**条目(该 bot 的 ReadPointer 可见域 = 公开 chat + 发给自己的 whisper + 活动事件);狼人 bot 不可借检索看到夜间私密信息(chatQueue 本就不含,§119)。检索结果过 `redactLedgerFact` 同款身份词脱敏?—— 不需要,chatQueue 条目本来就是 bot 可见的公开信息;但**检索结果不得包含其他 bot 的 HeartThought**(物理上不在 chatQueue,双保险断言)。
3. **限流**:per-bot 60s 冷却(复用 ratelimit 令牌桶模式);单响应最多 1 次调用;超限返回友好错误(不进 consecutiveFailures)。
4. **prompt 接线**:speak 阶段 user prompt 追加一行提示「可用 chat_recall 检索早期发言(如'R1 谁跳过预言家')」。
5. **测试**:打分排序 / 闭集钳制 / 隐私(检索结果不含 whisper 给他人/夜间信息)/ 冷却 / 截断标记,共 ≥6 项。

### U4: 跨局记忆迭代加固(流式 + 长预算 + 丢失比例校验)

**OpenClaw 依据**:压缩/整合走与主会话相同的管线与预算(Context §6.3);`maxPriorEntryLossFraction=0.25` 丢失比例上限(记忆 §2.4c);显式失败禁止假成功。

**设计**:

1. **流式 + 长预算**:`iterateOneModelMemory` 的 `provider.Chat` 改为 `ChatStreamAccumulate`(与主对话一致);超时从 90s 硬编码改为 `cfgAgentMemoryIterTimeoutSec`(默认 480s,走 §197 extended timeout 思想:首字节前 120s 熔断 + 总预算 480s)。
2. **重试**:transient 失败重试 1 次(线性 backoff 5s);permanent(401/403)不重试直接 FallbackMerge。
3. **丢失比例校验**:`ValidateMemorySections` 增强 —— 校验通过后新增 `ValidateMemoryRetention(old, new string) error`:若新记忆相比旧记忆**丢失 rune 数 > 50%**(压缩指令场景除外:旧记忆 >80K 时允许到 70% 丢失),视为 LLM 截断事故,回退 `FallbackMerge`。对齐 OpenClaw `maxPriorEntryLossFraction`。
4. **可观测**:迭代结果写 `RecordLog`(成功/回退原因/新旧长度),`BotTranscript` 不动(迭代发生在局末,无观众价值)。
5. **测试**:保留率校验边界(49%/51%/80K 场景)/ transient 重试 / permanent 快速回退 / 流式累积正确性。

### U5: wiring lint 扩展(机制化防 §130 第七次复发)

**设计**(扩充 `wiring_lint_test.go`):

1. **断言 A(已有三类的补集)**:「`agent/wwplayer` 与 `game/werewolf` 中所有 `Build*Cached`/`*Buffer` 导出构造函数,必须有 ≥1 个非测试生产调用点」—— grep 式断言,覆盖 ToolsCache/ShortMemoryBuffer 形态。
2. **断言 B**:「`compactConfig`/`steeringQueue`/`toolHooks` 字段必须有 setter 且 setter 有生产调用点;若无,字段不得存在」—— 已知死代码白名单(steeringQueue/toolHooks/ShortMemoryBuffer 列入 P1 评估清单,lint 暂豁免但注释标注截止日期)。
3. **断言 C**:「新增 Agent 工具名必须同时出现在 BuildTools 与 DispatchTool」—— 把 §97/§130 的「双路径对照」从人工 checklist 变为测试(U3 的 chat_recall 即被此断言保护)。

## 4. 后续路线(P1/P2,本期不实施)

| 优先级 | 项 | 说明 |
|---|---|---|
| P1 | steeringQueue / toolHooks 处置 | §112 观众全频唤醒已覆盖 steering 价值,倾向**删除**而非接线(对齐 §128「兼容字段是技术债务」);下版本决断 |
| P1 | ShortMemoryBuffer 接线或删除 | §20260813-01 新增,与 GameContext.RecentSpeeches 语义重叠度高,待确认意图 |
| P1 | Prune 批量驱逐(1.5x cushion) | OpenClaw history.ts 缓存友好裁剪;当前 80 轮硬阈值每轮逐条漂移,改批量驱逐可提升 cache 命中 |
| P1 | 工具数组 cache breakpoint | U2 字节稳定后,给最后一个工具打 ephemeral breakpoint(需 anthropic provider 支持 tools 块打点) |
| P2 | 记忆检索语义化(embedding) | OpenClaw 向量+BM25 混合检索;需引入 embedding provider infra,成本远超收益,暂不做 |
| P2 | 局内自我反思闭环(Reflexion) | OpenClaw 无对应物;属 Gemini/DouBao 报告的 P5-F 双过程思维,已有待实施文档跟踪 |
| P2 | cache break 可观测性 | OpenClaw prompt-cache-observability.ts;需 provider 返回 cacheRead/cacheWrite usage 并落 BotTranscript |

## 5. 验收标准

1. `go build ./...` 通过;`go test ./agent/... ./game/werewolf/... -count=1` 全绿。
2. U1/U3/U4 均经「还原缺陷 → 测试失败 → 恢复修复 → 测试通过」双向验证(CLAUDE.md §20260812-04 验收惯例)。
3. wiring lint 新增 3 条断言生效(对已知死代码白名单豁免项有注释与截止日期)。
4. 不触碰:§92a 锁纪律(新增锁内函数一律 `*Locked` 变体)、§119 协议层隔离、§197 流式长预算、§135 隐私护栏、前端零改动(本期纯后端)。
5. `./rebuild_restart_app.sh` 编译重启成功,服务健康。
