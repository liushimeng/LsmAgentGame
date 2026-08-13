# OpenClaw Context 管理分析

> 分析日期: 2026-08-13
> 源码路径: `/usr/local/LsmGitOpenSource/openclaw`
> 项目定位: 开源个人 AI 助手 —— 多渠道消息接入(Telegram/Discord/Slack 等 40+ 渠道)、自主执行系统与日常任务,TypeScript / pnpm monorepo
> 分析目的: 提取 LLM Context 管理(上下文组装/压缩/预算/缓存)设计模式,为狼人杀 Agent 的 Memory.Prune、SanitizeMessagesForAnthropic、prompt 注入链提供对标参考

## 1. 总体架构地图

Context 管理相关代码分布在四层:

| 层 | 位置 | 职责 |
|---|---|---|
| 运行编排层 | `src/agents/embedded-agent-runner/`(约 90 个非测试文件) | 一次 LLM run 的完整生命周期:准备 → 组装历史 → 预算预检 → 派发 → 压缩 → 终态 |
| 可插拔上下文引擎 | `src/context-engine/`(`types.ts` / `registry.ts` / `legacy.ts` / `delegate.ts`) | `ContextEngine` 插件契约:`bootstrap/ingest/afterTurn/assemble/compact/maintain` |
| Agent 内核 | `packages/agent-core/src/agent-loop.ts`、`packages/agent-core/src/harness/compaction/compaction.ts` | 与 provider 无关的 agentic loop 与压缩核心算法(Result 风格 API) |
| Provider 传输层 | `packages/ai/src/providers/anthropic.ts`、`packages/ai/src/transports/anthropic-payload-policy.ts` | wire 转换、`cache_control` 打点、SSE 流解析 |

辅助层:`src/agents/sessions/`(SessionManager、SQLite transcript、`agent-session-compaction.ts`)、`src/agents/system-prompt.ts`(1612 行的 system prompt 渲染器)。

**总体评价**:OpenClaw 的 context 管理呈现「估算器保守化(SAFETY_MARGIN 1.2 + 分角色 chars/token)→ 预检路由化(truncate/compact/混合三路)→ 压缩事务化(checkpoint + 迭代摘要 + 配对原子性三防线)→ 缓存工程化(边界标记 + 前缀稳定 + break 归因)」的完整闭环。

## 2. LLM 调用整体链路(收到消息 → 发出 API 请求)

1. **渠道接入**:渠道消息进入 `src/auto-reply/`,经 `resolveSessionKey`(`src/config/sessions/session-key.ts`)映射到 session bucket,交给 agent runner。
2. **Run 编排**:`src/agents/embedded-agent-runner/run-loop.ts::runPreparedEmbeddedLoop` 是主循环。先 `prepareEmbeddedRunRuntime`(模型/鉴权/harness 准备),创建 ContextEngine logical turn lease,进入 `while(true)` attempt 重试循环。
3. **每轮 attempt**:`run/attempt-history.ts` 组装历史 → `run/attempt-prompt-preflight.ts` 做 **pre-prompt 预算预检**(`preemptive-compaction.ts::shouldPreemptivelyCompactBeforePrompt`),必要时先压缩,再 `attempt-prompt-submit.ts` 提交。
4. **内核 loop**:`packages/agent-core/src/agent-loop.ts` 双层 `while`。每次调模型前依次执行 `config.transformContext(messages, signal)`(**关键扩展点**)→ `normalizeCoreContextMessages` → `config.convertToLlm` → 构造 `Context{systemPrompt, messages, tools}` → `streamFn(model, context, options)` 发起 SSE 流式请求。
5. **传输层**:`packages/ai/src/providers/anthropic.ts` 把内部 `Message[]` 转 Anthropic wire(含 thinking 块签名回放、连续 toolResult 合并为单条 user message、cache_control 打点)。

**责任模块划分**:历史选择与清洗在 `run/attempt-history.ts`,预算裁决在 `run/preemptive-compaction.ts`,工具循环中的动态修剪在 `tool-result-context-guard.ts`,wire 级组装在 `packages/ai/src/providers/*`。

## 3. System Prompt 构建:stable prefix + dynamic suffix 两段式

`src/agents/system-prompt.ts::buildAgentSystemPrompt`(796–1535 行)是最精彩的部分。prompt 被显式切成两段,中间插入**字面标记**:

```ts
// packages/ai/src/utils/system-prompt-cache-boundary.ts
export const SYSTEM_PROMPT_CACHE_BOUNDARY = "\n<!-- OPENCLAW_CACHE_BOUNDARY -->\n";
```

### 3.1 Stable prefix(cache boundary 之前,1210–1432 行)

- 内容:身份行、`## Tooling`(固定 `toolOrder` 排序的工具清单 + 一句话 summary)、Tool Call Style、Execution Bias、Safety、Skills、Memory、Workspace、Sandbox、Bootstrap、`# Project Context`(只放 `contextFiles.stable`,即 AGENTS.md/SOUL.md/IDENTITY.md/USER.md 等,排序权重表 `CONTEXT_FILE_ORDER` 在 83–91 行)。
- **进程内 LRU 缓存**:`cacheStablePromptPrefix`(154–166 行)对全部输入做 sha256 哈希(`hashStablePromptInput`),命中直接复用字符串,上限 64 条。目标是让 prefix **跨 turn 字节一致**。

### 3.2 Dynamic suffix(cache boundary 之后,1434–1534 行)

- 内容:日期/时区(注释:"Local date and timezone can change between turns. Keep them at the front of the volatile suffix so rollover is visible without invalidating the stable prefix")、Dynamic Project Context、exec 审批指引、owner 身份行、渠道 messaging 段、provider dynamic suffix、heartbeat 段、`## Runtime` 行。
- **分档渲染**:`promptMode: "full" | "minimal" | "none"` —— 主 agent 用 full,subagent 用 minimal(只 Tooling/Workspace/Runtime),none 只剩一行身份。
- **Provider 贡献**:`ProviderSystemPromptContribution` 允许 provider 注入 `stablePrefix`/`dynamicSuffix`/`sectionOverrides`(按 section id 覆盖)。
- **引擎追加**:`ContextEngine.assemble` 返回的 `systemPromptAddition` 经 `prependSystemPromptAdditionAfterCacheBoundary` **插在 boundary 之后、dynamic suffix 之前**——引擎追加内容默认不进缓存区。
- hook 覆盖 prompt 时 `ensureSystemPromptCacheBoundary` 兜底追加边界,使任何动态注入都路由到未缓存后缀(issue #85203)。

## 4. 消息历史的选择与排序

`run/attempt-history.ts` 固定四段式管线(注释原话:"sanitize → validate → limit → repair pipeline"):

1. `sanitizeSessionHistory`(`replay-history.ts`,996 行):按 provider/model API 清洗 transcript(strip 特殊 token、图片消毒、tool call 参数修复)。
2. `validateReplayTurns`:校验回放轮次结构。
3. `limitHistoryTurns`(`history.ts:32-90`):按 **user turn 数**裁剪(per-channel/per-DM 配置 `historyLimit`/`dmHistoryLimit`)。头部非对话消息(compactionSummary/branchSummary,`SESSION_HISTORY_PRELUDE` symbol 标记)**永远保留**。
4. `repairToolUseResultPairing`(`session-transcript-repair.ts:461`):裁剪后重修 tool_use/tool_result 配对。

非 legacy ContextEngine 存在时调 `engine.assemble({messages, tokenBudget, model, availableTools...})`,引擎可返回窗口化后的消息 + `promptAuthority`(`"assembled"` 或 `"preassembly_may_overflow"` —— 后者表示「我窗口化后的视图可能掩盖了底层 transcript 的溢出,预检请取两者最大值」)。

## 5. Token 预算管理

### 5.1 Token 估算(全部是基于字符的启发式,无真 tokenizer)

两套估算器并存:

- **压缩规划用**:`compaction.ts::estimateTokens`(253–309 行)—— 按 role 分派,CJK 感知;图片块固定 4800 字符;toolCall = name + 序列化参数。
- **预检压强估算**:`run/preemptive-compaction.ts`(26–243 行)—— 更细的常量表:`ESTIMATED_CHARS_PER_TOKEN=4`、`TOOL_RESULT_CHARS_PER_TOKEN=2`(tool result 更保守)、`JSON_PAYLOAD_CHARS_PER_TOKEN=3`、`MESSAGE_BOUNDARY_OVERHEAD_TOKENS=12`、`CONTENT_BLOCK_OVERHEAD_TOKENS=6`、`IMAGE_BLOCK_TOKENS=2000`,最后整体乘 `SAFETY_MARGIN=1.2`。
- **真实 usage 校准**:`compaction.ts::estimateContextTokens`(194–222 行)优先取**最后一条 assistant 消息的 provider-reported usage**,只对其后的 trailing 消息用估算器补齐 —— 「真实计数 + 尾部估算」混合模型。
- **压缩后防误触发**:`src/agents/compaction-usage.ts::stripStaleAssistantUsageBeforeLatestCompaction` 把最近一次 compactionSummary 之前的 assistant usage 全部清零 —— 否则保留的旧消息带着旧(更大的)usage 会立刻再次触发压缩。

### 5.2 上下文窗口解析(多级覆盖)

`src/agents/context-window-guard.ts::resolveContextWindowInfo`:优先级 `models.providers[].models[].contextTokens`(配置)→ 模型元数据 → 默认值;`agentContextTokens`(per-agent 配置)可**向下封顶**大窗口。守卫阈值:`hardMin = max(4000, window*0.1)`(低于则 block),`warnBelow = max(8000, window*0.2)`。

### 5.3 超预算三路路由

`run/preemptive-compaction.ts::shouldPreemptivelyCompactBeforePrompt`(274–355 行):

- `promptBudgetBeforeReserve = contextTokenBudget - effectiveReserveTokens`;reserve 被钳制,保证 prompt 至少拿到 `min(8000, budget*0.5)` —— 「reserve 再大也不许吃掉一半窗口」。
- 超预算后三路路由:
  - `truncate_tool_results_only`:tool result 可裁剪量 ≥ max(overflow+512tok, overflow×1.5)(**裁剪量须舒适地超过溢出量 1.5 倍才走纯裁剪**);
  - `compact_only`:tool result 没有可裁量;
  - `compact_then_truncate`:介于两者之间。
- 决策连同 `estimatedPromptTokens / overflowTokens / toolResultReducibleChars / pressureSource` 持久化为 `SessionContextBudgetStatus`,并打结构化日志 `[context-overflow-precheck]`。

### 5.4 工具循环中的在线修剪(两个 transformContext 包装器)

`tool-result-context-guard.ts`:

- `installToolResultContextGuard`:每条 tool result 硬上限 `max(1024, contextWindow × CHARS_PER_TOKEN × 0.5)`(单条最多占窗口一半)。超限走 head+tail 保留、中间省略,marker `⚠️ [... middle content omitted — showing head and tail ...]`;tail 是否「重要」由正则判断(含 error/traceback/exit code/JSON 结尾/summary 则保留尾部最多 30% 或 4000 字符)。
- **mid-turn precheck**:工具循环中每轮新增 tool result 后重跑预检,溢出抛 `MidTurnPrecheckSignal` 触发**循环中压缩**。
- `installContextEngineLoopHook`:对 ownsCompaction 的引擎,每轮工具循环都调 `engine.afterTurn + engine.assemble`;失败静默回落原始消息(注释:"Best-effort: any engine failure falls through to the raw source messages so the tool loop still makes forward progress")。

## 6. Compaction 压缩机制

### 6.1 触发时机(多入口)

| 触发 | 位置 | 条件 |
|---|---|---|
| 阈值 | `agent-session-compaction.ts:377` | `contextTokens > contextWindow - reserveTokens`(默认 reserve=16384) |
| 溢出恢复 | 同上 Case 1 | stopReason=error/length 的 overflow 后裁掉最后一条 assistant 再压缩重试 |
| Pre-prompt 预检 | `run/preemptive-compaction.ts` | §5.3 三路路由 |
| Mid-turn 预检 | `tool-result-context-guard.ts` | 工具循环中溢出抛 signal |
| 手动 | `compact.ts` / `compact.queued.ts` | `/compact` 命令排队执行 |
| 引擎后台 | `context-engine/types.ts::maintain/afterTurn` | 引擎自己决定 |

所有压缩都有 safety timeout(`compaction-safety-timeout.ts`),引擎契约要求 honor `abortSignal`。

### 6.2 压缩算法(LLM 摘要,多阶段 + 多 fallback)

核心在 `packages/agent-core/src/harness/compaction/compaction.ts`(946 行):

1. **findCutPoint**(395–465 行):从尾部向前累加 token,直到 ≥ `keepRecentTokens`(默认 20000,按 usage/estimate 比例归一化);切点只能落在 user/assistant/summary 等类型,**toolResult 永不为切点**;切在 turn 中间时 turn prefix 单独摘要(专用 `TURN_PREFIX_SUMMARIZATION_PROMPT`)。
2. **generateSummary**:整个 `<conversation>` 交给**同一个模型**摘要,固定输出格式(Goal / Constraints & Preferences / Progress(Done·In Progress·Blocked) / Key Decisions / Next Steps / Critical Context,要求保留精确文件路径/函数名/错误消息)。有 `previousSummary` 时换 `UPDATE_SUMMARIZATION_PROMPT` —— **迭代式摘要**(PRESERVE 旧的 + ADD 新的 + In Progress→Done 迁移)而非每次全量重来。
3. **大历史分块**(`compaction-planning.ts`):`buildSummaryChunks` 按 maxChunkTokens 分块,**分块绝不拆散未完成 tool_call/tool_result 批次**(`groupCompactionMessages` 追踪 pending call 队列,未清零不闭组);逐块滚动摘要(前一块 summary 作为下一块输入);`summarizeInStages` 段间合并用专用 MERGE prompt,每段打 UTC 时间范围标签;`computeAdaptiveChunkRatio` 自适应块比。
4. **Oversized fallback**:单消息 > 窗口 50% 的剔除并留 `[Large assistant (~NK tokens) omitted from summary]` 占位;部分块失败携带 partial summary 抛出逐层降级;**全部失败必须抛 `CompactionError("summarization_failed")` —— 注释明确防止「报告压缩成功但一个 token 都没回收」的静默死循环**。
5. **标识符保留**:`IDENTIFIER_PRESERVATION_INSTRUCTIONS`(UUID/hash/URL/文件名不得缩短改写)。
6. **安全约束**:`toolResult.details` 和 runtime-context 自定义消息**永不进入摘要 prompt**(SECURITY 注释)。

### 6.3 压缩执行的事务化(`compaction-session-execution.ts`,563 行)

一次压缩是一个完整「子会话」:打开 SessionManager(SQLite)→ **capture checkpoint 快照**(失败可回滚)→ 重建 embedded session(**同一套 system prompt、transport/payload shaping、工具 allowlist**)→ sanitize/validate/dedupe/limit/repair 历史 → `compactWithSafetyTimeout` 执行 → `persistCompactionCheckpoint`(记录 summary、firstKeptEntryId、tokensBefore/After)→ 失败按更低 thinking level 整体重试。

### 6.4 tool_use/tool_result 配对完整性(三道独立防线)

1. **分块/裁剪不拆对**:`groupCompactionMessages` 追踪 pending tool call occurrence 队列,非空不闭组。
2. **replay 前修复**:`repairToolUseResultPairing`(461 行起)—— 匹配的 toolResult **移动**到其 assistant toolCall 之后;缺失的插入合成 error toolResult;重复的丢弃;孤儿 toolResult 丢弃并计数。`limitHistoryTurns` 之后必须再跑一次(注释:"truncation can orphan tool_result blocks by removing the assistant message that contained the matching tool_use")。
3. **wire 层归并**:`anthropic.ts::convertMessages` 把连续多条 toolResult 合并为**一条 user message 的多个 tool_result block**;thinking 块缺签名时降级为 text 块避免 API 拒绝。

### 6.5 压缩结果在 transcript 中的形态

摘要作为 `compactionSummary` role 的消息放在历史**头部**;`firstKeptEntryId` 标记保留区起点;transcript 是 entry DAG(支持 branching),压缩是「branch-and-reappend」而非原地破坏(`rewriteTranscriptEntries` 由 runtime 拥有、引擎只能请求 —— 职责分离)。

## 7. Prompt Cache 利用

### 7.1 打点策略(Anthropic 系)

`anthropic-payload-policy.ts::applyAnthropicCacheControlToMessages`(147–220 行):

- 总计不超过 4 个 marker(system+tools 用掉的从 messages 额度里扣)。
- **system**:按 `SYSTEM_PROMPT_CACHE_BOUNDARY` 拆分 —— stablePrefix 块打 `cache_control: {type:"ephemeral", ttl?}`,dynamicSuffix 块不打。
- **tools**:只给**最后一个工具**打,缓存整个工具数组前缀。
- **messages**:从尾部向前找**最后一条稳定的 user 消息**的最后一个 text/image block 打「最深断点」;`cacheBreakpointOptOutParamIndexes` 把易变 carrier 消息排除在断点选择外 —— 「最深的 breakpoint 锚在最后一个稳定 user turn,而不是它后面追加的易变 carrier」。
- TTL:`resolveCacheRetention` 返回 `"none"|"short"|"long"`,映射 5m/1h ephemeral ttl;不支持缓存的 provider(Bedrock nova 等)被家族门槛挡住一律不打点。

### 7.2 缓存命中优化设计(围绕「前缀字节稳定」)

- system prompt 分 stable/dynamic 两段 + stable prefix 的 sha256 LRU 复用;
- `limitHistoryTurns` 的 **1.5x cushion + 批量驱逐**(`history.ts:67-75`):超过上限 50% 才一次性裁掉一批,注释明言 "trades strictness for amortized cache reuse" —— 避免每轮都裁一条导致前缀逐条漂移、缓存全失效;
- `prompt-cache-observability.ts`:每轮对 provider/model/retention/streamStrategy/systemPromptDigest/toolDigest 做快照,检测 cache break(cacheRead 骤降)并归因(change code:cacheRetention/model/streamStrategy/systemPrompt/tools/transport)—— **缓存命中不是「相信它有」,而是持续度量并诊断**;
- Google 系单独路径 `google-prompt-cache.ts` 显式调 Gemini `cachedContent` API;OpenAI completions 兼容层 `supportsPromptCacheKey` 传 cache key 给 llama.cpp 类后端。

## 8. 多轮对话状态与跨渠道隔离

- **Session key**(`session-key.ts::resolveSessionKey`):直聊折叠为 agent 的 canonical main bucket;群/频道保持 `agent:<id>:<provider>:group:<id>` 形态 —— 同一 agent 下 DM 合一、群各自隔离、多 agent 互不串。
- **Transcript 存储**:SQLite-backed entry DAG,`SessionManager` 管理 entries/branching/persistence;每条 assistant 消息挂 provider usage。
- **历史深度按渠道配置**:DM 用 `dmHistoryLimit`(支持 per-DM 覆盖),群/频道用 `historyLimit` —— 注释动机 "prevents context overflow in long-running channel sessions"。
- system prompt 的渠道段放在 cache boundary 之下,**渠道切换不污染跨 session 共享的稳定前缀**。

## 9. 流式响应与 agentic loop 终止

### 9.1 Agentic loop 结构(`packages/agent-core/src/agent-loop.ts`)

- **双层循环**:外层 `while(true)` 处理「agent 本要停但又来了 follow-up 消息」;内层 `while(hasMoreToolCalls || pendingMessages.length>0)` 是标准 assistant→toolUse→toolResult 循环。
- **没有固定最大工具轮数上限**;终止靠条件组合:
  1. `stopReason !== "toolUse"` 或 toolCalls 为空 → 自然结束;只有 `stopReason==="toolUse"` 才派发工具。
  2. `stopReason==="error"|"aborted"` → 立即 turn_end + agent_end。
  3. **工具死循环检测**:`tool-loop-detection.ts`(结果哈希 + noProgress 证据)+ `TOOL_LOOP_WARNING_THRESHOLD=10`;严重循环 `terminateRun` 合成终态消息退出。
  4. **压缩后循环守卫**:`post-compaction-loop-guard.ts` —— 压缩成功后 arm,若压缩后仍原地打转则 abort(#77474)。
  5. steering/follow-up 注入:用户在等待时发的新消息在 checkpoint 处取回注入下一轮。
- **Run 级重试预算**(区别于工具轮数):`RunRetryBudget` = base + per-profile 增量,钳在 [MIN, MAX];超限可 failover 到 fallback model。另有 empty-response / reasoning-only / idle-timeout 重试计数器与 cost-runaway breaker。

## 10. 值得借鉴的设计决策(12 条)

1. **System prompt 显式分 stable/dynamic 两段,中间埋字面 cache boundary 标记**。边界本身是 prompt 文本里的注释字符串,provider 层按它拆 block 只给 stable 段打 cache_control;任何动态注入都路由到未缓存后缀。配套 stable prefix 的 sha256 输入哈希 + 64 条 LRU 保证跨 turn 字节一致。(`system-prompt-cache-boundary.ts`、`system-prompt.ts:1430`)
2. **「真实 usage + 尾部估算」的混合 token 计数**:信任最后一条 assistant 的 provider usage 为已发生事实,只估算其后 trailing 消息;压缩后清零旧 usage 防止立即误触发。(`compaction.ts::estimateContextTokens`、`compaction-usage.ts`)
3. **超预算三路路由而非一刀切压缩**:先算 tool result 可裁剪量,可裁量 ≥ 1.5×溢出量才走纯截断(便宜、无 LLM 调用),否则只压缩或先压后裁;reserve 钳制保证 prompt 至少拿到窗口一半或 8000 token。(`run/preemptive-compaction.ts`)
4. **裁剪/分块以 tool_call↔tool_result 批次为原子单位**:分块追踪 pending call 队列不闭组;裁剪后统一修复(移动、合成缺失 error result、丢弃孤儿);三处独立防线(分块、replay 修复、wire 归并),任何单点失效都不产生非法 transcript。(`compaction-planning.ts`、`session-transcript-repair.ts`)
5. **迭代式摘要而非全量重摘要**(`UPDATE_SUMMARIZATION_PROMPT`):previousSummary 存在时只做增量更新,显著降低压缩成本与信息漂移;分块滚动传递 + 段间 MERGE + UTC 时间标签。
6. **压缩失败必须显式失败,禁止「假成功」**:所有摘要路径失败抛 `CompactionError("summarization_failed")`,因为静默成功会导致「报告已压缩但 token 未回收」的无限重试循环。配套 partial-summary 降级链。(`src/agents/compaction.ts:254-262`)
7. **压缩是带 checkpoint 的事务,且复用与正常 turn 完全相同的管线**:压缩 LLM 看到的上下文形态与主会话一致,避免「压缩模型读到不一样的 prompt 形状」。(`compaction-session-execution.ts`)
8. **历史裁剪为缓存友好做批量驱逐**:1.5× cushion + 整批驱逐,以牺牲严格性换取 prompt-cache 前缀的摊销稳定性。(`history.ts:67-75`)
9. **transformContext 作为每轮模型调用前的统一扩展点**:工具结果在线截断、mid-turn 溢出预检、ContextEngine afterTurn/assemble 全部挂在同一 wrapper 链,失败一律静默回落原始消息保证工具循环前进。(`agent-loop.ts:539-545`)
10. **cache break 可观测性内建**:每轮快照 systemPromptDigest/toolDigest/retention,cacheRead 骤降时给出归因 change code。(`prompt-cache-observability.ts`)
11. **截断策略的「重要尾部」启发式**:head+tail 保留中间省略,尾部保留由正则判定(error/traceback/exit code/JSON 收尾),截断永远留下机器可读的占位说明而非静默丢内容。(`tool-result-truncation.ts:348-400`)
12. **ContextEngine 插件契约把上下文管理本身产品化**:`assemble` 可声明 `promptAuthority:"preassembly_may_overflow"` 防止宿主预检被引擎窗口化视图欺骗;`compact` 契约强制 safety timeout + abortSignal;`rewriteTranscriptEntries` 由 runtime 拥有、引擎只能请求(职责分离);legacy 引擎用同一接口包住旧管线保证 100% 兼容。(`src/context-engine/types.ts`)

## 11. 关键文件速查表

| 主题 | 文件 |
|---|---|
| 压缩核心算法 | `packages/agent-core/src/harness/compaction/compaction.ts` |
| 分块/降级/fallback | `src/agents/compaction.ts`、`src/agents/compaction-planning.ts` |
| 压缩执行事务 | `src/agents/embedded-agent-runner/compaction-session-execution.ts`、`compaction-checkpoint.ts`、`compaction-safety-timeout.ts` |
| 预算预检与路由 | `src/agents/embedded-agent-runner/run/preemptive-compaction.ts`、`run/midturn-precheck.ts`、`src/agents/agent-compaction-constants.ts` |
| 在线截断 | `src/agents/embedded-agent-runner/tool-result-context-guard.ts`、`tool-result-truncation.ts` |
| System prompt | `src/agents/system-prompt.ts`、`packages/ai/src/utils/system-prompt-cache-boundary.ts` |
| 历史选择 | `src/agents/embedded-agent-runner/run/attempt-history.ts`、`history.ts`、`replay-history.ts` |
| 配对修复 | `src/agents/session-transcript-repair.ts`、`packages/agent-core/src/harness/session/tool-result-pairing.js` |
| Prompt cache | `packages/ai/src/transports/anthropic-payload-policy.ts`、`packages/ai/src/providers/anthropic.ts`、`prompt-cache-retention.ts`、`google-prompt-cache.ts`、`prompt-cache-observability.ts` |
| 引擎契约 | `src/context-engine/types.ts`、`registry.ts`、`legacy.ts`、`delegate.ts` |
| Agent loop | `packages/agent-core/src/agent-loop.ts`、`src/agents/embedded-agent-runner/run-loop.ts`、`src/agents/tool-loop-detection.ts`、`post-compaction-loop-guard.ts` |
| Session/隔离 | `src/config/sessions/session-key.ts`、`src/agents/sessions/session-manager-*.ts`、`src/agents/compaction-usage.ts` |
| 窗口解析 | `src/agents/context-window-guard.ts`、`src/agents/context-cache*.ts` |
