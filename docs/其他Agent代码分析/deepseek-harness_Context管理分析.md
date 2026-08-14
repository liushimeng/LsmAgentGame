# DeepSeek Harness — Context 管理源码分析

> 分析日期: 2026-08-14
> 源码路径: `/usr/local/LsmGitOpenSource/deepseek-harness/`
> 版本: `@deepseek-ai/dsh-root` v0.1.0-rc.5 (commit `47f943859b Merge pull request #2519 from deepseek-harness/feat/npm-public`)
> 项目性质: DeepSeek 团队开源的 AI Agent harness，覆盖 LLM 抽象、Provider 适配、重试、Token 计量、上下文压缩、流式、子代理隔离全链路。

---

## 0. 全文导读与关键结论

DeepSeek Harness（以下简称 DSH）是一个**多 Provider、可插拔、强不变量**的 Agent 执行体。它处理"调用 LLM"问题的核心架构决策可总结为以下 7 条：

1. **Provider 抽象与消息词汇完全分离** —— `LlmAdapter` 抽象类（`packages/llm/llm/src/index.ts:180`）只承诺 `stream(options): AsyncIterable<StreamChunk>`，所有 wire 翻译由 `llm-deepseek` / `llm-pi-ai` 自行负责（`packages/llm/llm-deepseek/src/translate.ts:86`、`packages/llm/llm-pi-ai/src/adapter.ts`）。
2. **请求是事件日志的纯函数** —— Agent 每次 dispatch 的 `GenerateOptions` 必须**冻结**、必须**源自日志派生**，且 invariant companion 会在 `llm/stream` 触发前用 `session.deriveMessages()` 对账（`packages/core/agent-loop/src/invariant.ts:39-42`）。这是"reconstructability"原则 —— 重放日志必须能一字不差地重建出请求。
3. **上下文组装有 4 层，3 段优先级** —— System 段位（`-100` 锚点 / `0` 部署人格 / `100-199` 工具） + 动态 Context 段位（按 `order` 升序） + 工具列表（按 `toolOrder` 或字典序） + 变量（`{{var}}` 严格插值），经 `system-prompt/assemble` waterfall 收口（`packages/core/system-prompt/src/index.ts:467-542`）。
4. **Context 预算 = provider 锚点 + 表面增量** —— `tokenMeter.measure()` 用 `usage.inputTokens + cacheRead + cacheWrite` 作锚点，**之后**对未锚定的 surface 用 `CHARS_PER_TOKEN=4` 启发式做带符号 delta 累加，因此 O(1) replay 即可给出当前 `totalTokens`（`packages/llm/token-meter/src/usage-projection.ts:168-205`）。
5. **预算阈值与 post-error 触发并存** —— `compaction-basic` 走 80% `thresholdRatio` + 16% `retainRatio` 的双阈值（`packages/compaction/compaction-basic/src/config.ts:144-148`），pre-step `agent/pre-step` 拦截主动压缩；同时在 `agent/request-error` 监听 `CONTEXT_WINDOW_EXCEEDED_CODE` 做 post-error 兜底（`packages/compaction/compaction-basic/src/index.ts:179-223`）。
6. **Prompt cache 完全靠 provider 自动断点** —— DeepSeek 是自动 server-side cache，`llm-deepseek/translate.ts:53-62` 的关键就是 `prompt_tokens - cached_tokens` 的 disjoint 拆分；Harness 不下任何 `cache_control` 标记，唯一需要做的是让"前 N 轮"消息保持字节稳定以最大化命中（由 `request/header` 折叠保证）。
7. **子代理隔离有 4 个独立维度** —— session 隔离（独立 `SessionId`）+ scope 隔离（agent 平面挂在 `agent.ctx`）+ permission 隔离（`toolFilter.allow/deny` + `'never'` 审批 pin）+ 上下文隔离（`subagent:delegation` runtime-context 注入，子永远不可能改宽自己）。详见 §8。

下文按数据流自顶向下展开。

---

## 1. Context 全景数据流图

```text
                 ┌────────────────────────────────────────────────────────┐
                 │            Agent / Session Event Log (single source)    │
                 │  turn/start  step/start  user/message                  │
                 │  assistant/chunk  assistant/message                    │
                 │  request/header  request/context                       │
                 │  tool/result  tool/execution  tool/error               │
                 │  subagent/descriptor  sandbox/mode  approval/policy    │
                 └───────────────────────┬────────────────────────────────┘
                                         │ session.deriveMessages()
                                         ▼
┌────────────────────────────────────────────────────────────────────────┐
│  ReactLoopAgent.step(assembly)  (packages/core/agent-loop/src/agent.ts)│
│   1. preStep():  inbox.claim + systemPrompt.assemble + runtimeContext  │
│   2. buildRequest(): snapshot request/header, prepareCall(...)        │
│   3. stream loop: BlockAssembler 累积 → assistant/message 事件          │
│   4. tool calls: executeToolCalls(...)                                 │
│   5. on error: waterfall agent/request-error → compactIfNeeded retry   │
└────────────────────────────────────────────────────────────────────────┘
            │                                  │
            │ GenerateOptions (frozen)         │ runtime context snapshot
            ▼                                  ▼
┌────────────────────────┐         ┌─────────────────────────────────────┐
│  LlmAdapter.stream()   │         │  plugin pre-step listeners (prepend)│
│  ↓ SSE parse            │         │  - time-context                    │
│  StreamChunk (typed)    │         │  - tmux-context                    │
│  ↓ BlockAssembler       │         │  - agent-instructions              │
│  Message (immutable)    │         │  - session-reference               │
└────────────────────────┘         └─────────────────────────────────────┘
            │
            ▼
   ┌────────────────────────────────────────────────────────┐
   │  Side effects: session events, log projections          │
   │  - tokenUsage  (Σ buckets)                              │
   │  - contextPressure (provider-anchored, O(1) replay)      │
   │  - contextBreakdown (per-section)                       │
   └────────────────────────────────────────────────────────┘
            │
            ▼
   ┌────────────────────────────────────────────────────────┐
   │  compaction-basic                                       │
   │  trigger A: pre-step  (pressure)                         │
   │  trigger B: request-error (context-overflow)             │
   │  → compaction / tool-result-pruner                      │
   │  → shadow-price event → meter surfaceTokens 下调         │
   └────────────────────────────────────────────────────────┘
```

**关键不变量**：event log 是 single source of truth；每条 LLM 请求都从它派生，replay 即可重建（`agent-loop/src/invariant.ts:40-42` 的 `JSON.stringify(expected) !== JSON.stringify(options.messages)` 失败即 invariant violation）。

---

## 2. LLM 抽象层

### 2.1 模块职责

| 文件 | 行数 | 职责 |
|------|------|------|
| `packages/llm/llm/src/types.ts` | 356 | 词汇定义：`ContentBlockMap` 合并可扩展、`FinishReasonMap` 终止原因、`StreamChunk` 6 种 wire 帧 |
| `packages/llm/llm/src/message.ts` | 261 | `Message` 不可变身份 + 6 类 `ContextForm`（`instructions/catalog/snapshot/notice/relay/recall`），语义而非视觉 |
| `packages/llm/llm/src/index.ts` | 947 | `LlmRuntime` Service、`LlmAdapter` 抽象类、注册与替换语义、`LlmError` 错误码表 |
| `packages/llm/llm/src/assembler.ts` | 164 | `BlockAssembler` —— 把 `StreamChunk` 流还原为 `ContentBlock[]` 的单源算法 |
| `packages/llm/llm/src/invariant.ts` | 112 | 流协议语法不变量（open block index、usage 唯一、finish 终结） |
| `packages/llm/llm/src/retry-policy.ts` | 191 | 错误码白名单 + 指数退避 + 对称抖动 + `providerRetryAfterMs` |
| `packages/llm/llm/src/error.ts` | 163 | `HarnessError` 基类 + `CONTEXT_WINDOW_EXCEEDED/EMPTY_RESPONSE/QUOTA/INVALID_CREDENTIAL` 错误码 + 5 类正则识别 context-overflow |
| `packages/llm/llm/src/call-config.ts` | 117 | `LlmCallConfig` 7 字段（provider/model/reasoningEffort/temperature/maxTokens/stop）+ `markAgentLoopRequest` |
| `packages/llm/llm-deepseek/src/{translate.ts,serialize.ts,adapter.ts}` | 187+187+346 | DeepSeek Chat Completions wire 翻译 |
| `packages/llm/llm-pi-ai/src/{adapter.ts,stream.ts,catalog.ts,convert.ts}` | 358+208+546+~280 | pi-ai 库后端适配（统一多 provider 库） |
| `packages/llm/llm-retry/src/index.ts` | 226 | 在 `agent/request-error` 触发重试；normal/always 两模式 + jitter |

### 2.2 `LlmAdapter` 抽象（`packages/llm/llm/src/index.ts:180-233`）

```typescript
export abstract class LlmAdapter {
  providerInfo(provider: string): LlmProviderInfo { return { id: provider, name: provider } }
  providerRetryPolicy(_provider: string): ResolvedRetryPolicy | undefined { return undefined }
  listModels(_provider: string): Promise<readonly LlmModelInfo[]> { return Promise.resolve([]) }
  resolveModel(provider, model, signal?): Promise<LlmResolvedModelInfo> { /* default */ }
  abstract stream(options: GenerateOptions): AsyncIterable<StreamChunk>
}
```

- 抽象类而非 interface（`index.ts:180`），因为**所有 provider 都需要一个 4 方法的默认基线**而非 1 方法。
- `stream()` 是**唯一**抽象方法，其余皆可由基类默认满足。
- Provider 元数据（`LlmModelInfo` 中的 `contextWindow`、`defaultMaxTokens`、`reasoning.efforts`）由 adapter 在 `resolveModel` 时**异步**返回。

### 2.3 `LlmRuntime` 的注册与替换语义（`index.ts:284-484`）

注册时要求 `providers.length > 0` 且每条路由全局唯一（`index.ts:346/379`）：

```typescript
registerAdapter(providers: string[], adapter: LlmAdapter): AdapterRegistrationHandle {
  // ... validateRoutes for full candidate set first, then commit atomically
  // 返回的 handle 既能 dispose(), 也能 .replace(newProviders)
  handle.replace = (next) => {
    if (released) throw new LlmError('...', 'REGISTRATION_DISPOSED')
    this.commitRoutes(owned, this.prepareRoutes(next, adapter, owned))
  }
}
```

- `prepareRoutes` 验证**完整候选集**才提交，故失败时旧路由完全保留（`index.ts:374-396`）—— 这是"swap 而非 delete-then-add"语义的来源。
- 同样的"原子提交"模式用于 `registerConfigurableProviders`（`index.ts:441-461`）。
- 路由变更广播 `llm/adapters-updated` 事件，listener 失败被**独立 contain** 而不 veto 提交（`index.ts:296-322`）—— 这是"Cordis emit 用 `Array.map`，单 listener 同步抛出会饿死后续"的对策。

### 2.4 `BlockAssembler` 单源算法（`packages/llm/llm/src/assembler.ts`）

每个 `StreamChunk` 都通过 `push` 进入同一个 `Map<index, PartialBlock>` 累加器：

| Chunk 类型 | 处理 |
|---|---|
| `block-start` | 若新 index，记入 `order[]` 并初始化 partial |
| `text-delta`/`reasoning-delta`/`tool-call-delta` | 累加对应 partial 的 text/arguments；**已 `block-end` 的 index 静默忽略**（malformed stream 容错，`assembler.ts:62-64/69-72`） |
| `block-end` | **首次关闭胜出**，忽略重复 close（`assembler.ts:77-82`） |
| `usage` | 覆盖最新一份（`assembler.ts:83-86`） |
| `finish` | 锁存 `finish` 与 `replayState`（`assembler.ts:87-91`） |

调用方通过 `blocks()` 取得按 `order` 排序的最终块，**`max-tokens` 时主动过滤 `tool-call` 块**（`assembler.ts:136-138`）—— 这是"被截断的 tool_call 不可执行"安全约束。

**为什么是单源**：agent loop、session log、admin console 都需要还原"模型实际说了什么"，单源算法保证三者完全一致。

### 2.5 `StreamChunk` 流协议不变量（`packages/llm/llm/src/invariant.ts:36-83`）

`validateStream` 在 `llm/stream` waterfall **最前**拦截并强制执行：

1. `block-end` 必须有对应的 open block，且 close 的 `type` 与 open 一致。
2. `usage` 帧**只能出现一次**。
3. `finish` 后不能再发任何帧（除 `error`/`aborted` 等终止帧）。
4. 除 `error`/`aborted` 外，`finish` 时**不能有未关闭的 block**。

任何违规抛 `INVARIANT` 码失败。这把"流协议是 ad-hoc"的脏活固化为可测的协议层。

### 2.6 重试：错误码白名单 + 指数退避 + jitter

`retry-policy.ts:14-24` 默认白名单（关键观察：默认**不**含 `INVALID_CREDENTIAL`）：

```typescript
const DEFAULT_RETRYABLE_CODES = Object.freeze([
  EMPTY_RESPONSE_CODE,  // "EMPTY_RESPONSE" - 模型正常结束但 0 块（退化完成）
  'RATE_LIMIT',
  'SERVER',
  'TIMEOUT',
  'TRANSPORT',
])
```

`EMPTY_RESPONSE_CODE` 的存在（`error.ts:39-48`）很有意思 —— 即"模型成功完成但 0 块"被显式分类为**可重试**，因为它语义上等价于"什么都没产出"，对调用方与用户而言都是"什么都没发生"。

退避公式（`llm-retry/src/index.ts:101`）：

```typescript
const exponent = Math.min(retry - 1, 1024)
const exponential = Math.min(config.initialDelayMs * 2 ** exponent, config.maxDelayMs)
const jitter = 1 - config.jitterRatio + 2 * config.jitterRatio * random()
return Math.min(exponential * jitter, config.maxDelayMs)
```

- 指数：起点 `initialDelayMs`（默认 500），上界 `maxDelayMs`（默认 10000）。
- 对称 jitter：`[1-r, 1+r]` 区间（默认 r=0.1），防止 7 房间同时重试的雷击群。

**重试挂在 `agent/request-error` 而非 `llm/stream`**（`llm-retry/src/index.ts:177`），意味着 stream 帧直接落地 session log；重试之间不会"覆盖"已经记录的部分。这是"日志即真相"的延伸。

### 2.7 Provider cache：Harness 不下断点，只对账

`llm-deepseek/translate.ts:53-62` 的 `mapUsage` 揭示了关键设计：

```typescript
export function mapUsage(usage: WireUsage): TokenUsage {
  const cacheRead = usage.prompt_tokens_details?.cached_tokens ?? usage.prompt_cache_hit_tokens
  const reasoning = usage.completion_tokens_details?.reasoning_tokens
  return {
    inputTokens: usage.prompt_tokens - (cacheRead ?? 0),  // disjoint!
    outputTokens: usage.completion_tokens,
    ...cacheRead !== undefined ? { cacheReadTokens: cacheRead } : {},
    ...reasoning !== undefined ? { reasoningTokens: reasoning } : {},
  }
}
```

DeepSeek 的 `prompt_tokens = prompt_cache_hit_tokens + prompt_cache_miss_tokens`（wire 文档），Harness 规范要求**disjoint 计数**（`types.ts:131-133`），所以这里必须**减出来**。这意味着所有下游（token-meter 的累加、UI 展示、计费）都自动尊重 cache 命中。

`llm-pi-ai` 通过库抽象也不下 `cache_control` —— 整仓库搜不到 `cache_control`/`cachePolicy` 关键字。**结论**：Harness 把缓存完全交给 provider 的自动 KV 缓存机制，自身只保证：

1. `request/header` 在路由不切换时**不写新事件**（`agent.ts:465-467`），前 N 轮历史字节稳定 → 缓存自动命中。
2. `toolOrder` 字典序（`system-prompt/src/index.ts:181-183`）→ tool schema 顺序稳定。
3. `[as]sistantMessage` 走"重建而非原样取"的 `replayState`（`message.ts:14-19`，见 `replay.ts`）→ provider 实例内"我的回复"也被自动缓存。

---

## 3. Context 组装管线

### 3.1 4 层输入，3 段优先级

SystemPrompt 服务在 `system-prompt/src/index.ts:467-542` 的 `assemble()` 中按以下顺序合并：

| 维度 | 段位约定 | 来源 |
|---|---|---|
| **System Section** | `-100` 锚点（`harness:identity`） / `0` 部署人格（`deployment:persona`） / `100-199` 工具 | `ctx.systemPrompt.section({ name, order, text })` |
| **Dynamic Context** | 任意 `order` 升序；同名 scoped 覆盖 global | `ctx.systemPrompt.context({ name, order, text })` |
| **Tool Schema** | `toolOrder` 显式 + `<unlisted-tools>` 占位符，或字典序 | `ctx.systemPrompt.tools(provider)` |
| **Variable** | `{{name}}` 严格插值；不支持 `{{{name}}}` | `ctx.systemPrompt.variable(name, provider)` |

**`complete: true` section 是全量接管**：1 个有效 complete section → assembly waterfall 之后**它独占**所有 section（`system-prompt/src/index.ts:537-541`）；多 complete 同时存在则 `assemble` 抛错（`system-prompt/src/index.ts:505-508`）。这是"插件 A 想换 persona、插件 B 想换 identity"时的硬约束。

**`toolOrder` 的 `<unlisted-tools>` 哨兵**（`system-prompt/src/index.ts:140, 153-156`）：要求配置中**恰好**出现一次。`unlisted` 工具按字典序插入此占位符位置。这把"工具顺序"从每个插件自己决定变成中央配置驱动 —— 后续插件不会无意重排。

### 3.2 严格 `{{variable}}` 插值（`system-prompt/src/index.ts:258-295`）

```typescript
const VARIABLE_NAME = /^[a-z][a-z0-9_]*$/
const GROUP_AT = /^\{\{([^{}]*)\}\}/

// 三类都 throw — "fail loud beats shipping a malformed prompt"
if (!GROUP_AT.test) throw new Error(`malformed prompt variable reference...`)
if (!VARIABLE_NAME.test(name)) throw new Error(`malformed prompt variable "{{${name}}}"...`)
if (!Object.hasOwn(variables, name)) throw new Error(`unknown prompt variable "{{${name}}}"...`)  // prototype 防护
if (value === undefined) throw new Error(`prompt variable "{{${name}}}" has no value...`)
```

**关键不变量**：

- `Object.hasOwn` 替代 `in` —— 防止 `{{constructor}}` / `{{toString}}` 这类原型链名字被"莫名通过"。
- **递归不再扫描** —— 已替换的值不会被二次解析（`system-prompt/src/index.ts:212-217` 的注释明确）。
- 单独 `{{` 不闭合 = 字面量（`GROUP_AT` 返回 null 且无 `}}` 时视为 prose），但**有 `}}` 却不构成完整 group = 错**（抛 malformed）。

### 3.3 Dynamic Context 与 Snapshot Form

Dynamic Context **不**进 system prompt；它转成 user-role 消息进入消息历史（`runtime-context.ts:64-74`）：

```typescript
project(current: string, sections: readonly ContextSnapshotSection[]): UserMessage | undefined {
  if (this.retained === undefined && current.length === 0) return
  const snapshot = current.length === 0 ? CLEARED : current
  if (this.retained?.text === snapshot) return   // 去重关键
  return createUserMessage({
    content: [{ type: 'text', text: snapshot }],
    source: sections.length === 0
      ? { kind: 'plugin', plugin: SOURCE }
      : { kind: 'plugin', plugin: SOURCE, form: 'snapshot', sections },
  })
}
```

`runtime-context.ts:46-55` 的 session event 订阅做**保留快照状态**：

- 见到 `user/message` 且 `source.plugin === 'dsh-system-prompt'`（标识其来源）→ 更新 `retained = { seq, text }`。
- 见到 `isReplacementSurfaceEvent(event)` 且 `sourceEventSeqs` 包含 `retained.seq` → 置 `retained = null`（即"上游被替换了，本快照失效"）。

这给出 §2 节中"runtime context 是 user message 而非 system section"的副作用：**每次重发都会多一条 user message**。`project()` 的"内容未变就返回 `undefined`"避免无限累积。

### 3.4 4 款 Context Provider 详解

#### 3.4.1 `time-context`（`packages/context/time-context/src/index.ts`）

注册到 `agent/pre-step` waterfall 的 `prepend: true` listener（`index.ts:170-208`）：

```typescript
ctx.on('agent/pre-step', async ({ agent, turn, step, signal }, next) => {
  const decision = await next()  // 链式让后注册的 listener 跑
  if (decision.kind === 'reject' || signal.aborted) return decision
  const now = Date.now()
  if (refreshIntervalMs && (now - lastInjection) < refreshIntervalMs) return decision  // 节流
  const previous = step === 1 ? precedingMessageTime(agent) : precedingStepContextTime(agent, turn)
  const text = renderText(now, turn, step, previous, ...)
  return { kind: 'enter', messages: [...decision.messages, createUserMessage({...})] }
}, { prepend: true })
```

- 步骤 1 引用"上一条 model-visible message"；步骤 2+ 引用"本 turn 内上一条 step context"——上下文是**步骤内累加**的，不是会话级。
- `refreshIntervalMs`（默认 0）允许节流：高频对局可设 5s/次避免消息膨胀。
- 渲染格式（`index.ts:110-125`）：

```text
Time sampled while preparing turn 5, step 3: 2026-08-14T11:37:43.123+08:00
[Browser time zone context — <browser/UTC/system-unavailable>]
Elapsed since the preceding step context: 12s.
```

#### 3.4.2 `tmux-context`（`packages/context/tmux-context/src/index.ts`）

只**第一次 step**（`step !== 1` 立即返回，`index.ts:223`）拉取一次 tmux 状态：

1. `bash.resolve({ command, signal })` 跑 `[ -n "$TMUX_PANE" ] || exit 1` + `ps -o tty=` + `tmux display-message -p #{pane_tty}` 三段（`index.ts:114-121`）。
2. **真伪验证**：`pane_tty` 必须等于本进程 `tty`，否则认作"继承自祖先终端"（VS Code 集成终端的典型情况），读为"not in tmux"。
3. **变更检测**：与上次保留的 `state` 字符串对比；未变则不发（`index.ts:233-234`）。

输出格式：

```text
tmux location (turn 5):
session main, window 1 "editor", pane 0 %5
window active=1, pane active=1, layout bb94,...
```

**亮点**：对 `prepend: true` 顺序敏感，time-context 在它之前运行 → tmux 不会污染"上一步距今"语义。

#### 3.4.3 `agent-instructions`（`packages/context/agent-instructions/src/`）

1863 行，是 4 款 context provider 中**最大**也最复杂的。其设计目标：

- 启动时载入 `$DSH_HOME/AGENTS.md` + 项目根向下找到的 `AGENTS.md` / `CLAUDE.md` 链（`files.ts`）。
- **每条用户消息在 pre-step 时**对照"已加载指令"与"工作区真实文件"做对账（`index.ts:322-348` 的 `agent/pre-step` listener + `index.ts:350-366` 的 `tools/result` listener）。
- 字节预算下做**逐步降级**（`render.ts:249-273` 的 `truncateToFit` 二分搜索）：
  1. 完整装下 → 全发。
  2. 装不下 → 尝试去掉**最不具体的**（broader）scope，从最具体的开始反向丢弃（`render.ts:289-294`）。
  3. 仍装不下 → 二分截断**最具体**那条（`render.ts:302-315`）。
  4. 仍装不下 → 只发"[... truncated to budget ...]"标记（`render.ts:317-331`）。

**降级必须留可观测标记**（`render.ts:215-225` 的 `markerText`）：

```text
Workspace instruction budget 5000 bytes: omitted CLAUDE.md, ~/.dsh/AGENTS.md; truncated ./.claude/AGENTS.md from 8400 to 3200 bytes
```

—— 这是 §6 不可变式"pruning 永远要写 marker"在 instruction context 的体现。

变更订阅（`index.ts:350-366`）监听 `tools/result` 中对 `read/write/edit` 工具的 `file_path` 参数，**在 step 结束时**才重投影（`index.ts:294-320` 的 `projectTouch` 把 step open/close 作为 commit 边界）—— 避免同 step 内并发投影的竞态。

#### 3.4.4 `session-reference`（`packages/context/session-reference/src/projection.ts`）

跨 session 引入历史快照。`retainReferencedSession`（`projection.ts:69-138`）是 2 步降级：

1. **丢消息**：循环查找"非 checkpoint 且非最新"的消息删除，直到 `JSON.stringify(data) <= maxBytes`（`projection.ts:87-98`）。
2. **截消息**：循环找最长那条，二分 head/tail 保留（`projection.ts:100-122` + `truncateWithNotice` 的二分算法）。

特殊点：**checkpoint 消息绝不丢**（`projection.ts:89` 的 `!item.checkpoint` 守卫）—— 因为 checkpoint 是压缩边界，丢了会破坏后续 replay。**最新一条也绝不丢**（`index !== newestIndex` 守卫）—— 永远是 active context。

统计输出（`projection.ts:124-137`）：

```typescript
stats: {
  compacted: original.some(item => item.checkpoint),
  originalMessages: number,
  retainedMessages: number,
  omittedMessages: number,
  omittedBytes: number,
  truncated: bool,
}
```

`compacted` 这个布尔为 UI 提供"被引入的历史**已经是压缩过的**"的可视提示。

### 3.5 `system-prompt/assemble` Waterfall（`system-prompt/src/index.ts:532-535`）

```typescript
const transformed = await this.ctx.waterfall(
  scopeTarget(this, scope), 'system-prompt/assemble', assembly, context,
  () => Promise.resolve(assembly),
)
if (completeSection === undefined && !runtimeContextSuppressed) return transformed
return {
  ...transformed,
  sections: completeSection === undefined ? transformed.sections : [completeSection],
  contexts: runtimeContextSuppressed ? [] : transformed.contexts,
}
```

注意：waterfall 收口后，**runtime context suppression 与 complete section 是"后处理硬覆盖"**。这意味着：

- 想"加一个 dynamic context" 的 listener → 必须在 `assemble` 之前注册（`context()` 入口），不能在 waterfall 中塞。
- 想要"替换整个 system prompt"的 listener → 改 `complete: true` 即可，waterfall 仍能修改 tools / variables / contexts。

### 3.6 与 agent-loop 装配的对接

`ReactLoopAgent.preStep()`（`agent-loop/src/agent.ts:225-243`）串联 5 步：

```typescript
async preStep(target, position): Promise<PreparedStep> {
  const claimed = this.inbox.claim(target, position.turn)
  const assembly = await this.loopCtx.systemPrompt.assemble(assembleContextFor(this, signal))
  const sections = renderContextSections(assembly)
  const context = this.runtimeContext.project(joinContextSections(sections), sections)
  const decision = await this.dispatch.waterfall(
    'agent/pre-step', { messages: claimed, ...position, signal },
    (): Promise<PreStepDecision> => Promise.resolve<PreStepDecision>({
      kind: 'enter',
      messages: context === undefined ? claimed : [...claimed, context],
    }),
  )
  // ...
  return decision.kind === 'reject' ? decision : { ...decision, assembly }
}
```

1. `claimed` = 从 inbox 取出本步骤要送入模型的消息。
2. `assemble` = 一次 system prompt 组装（含 sections/contexts/tools/variables）。
3. `renderContextSections` = 渲染 dynamic context 为有名字的 section 列表。
4. `project` = 去重后生成 user-role snapshot 消息。
5. `agent/pre-step` waterfall 允许插件继续 mutate `messages`（如 `agent-instructions`）。

最终 `step()` 内 `buildRequest(turn, step, assembly.tools, system, this.session.deriveMessages(), signal)` 把 `assembly.tools` 与渲染好的 `system` 串入 `GenerateOptions`。

---

## 4. Token 预算与窗口管理

### 4.1 模块职责

| 文件 | 行数 | 职责 |
|---|---|---|
| `packages/llm/token-meter/src/index.ts` | ~370 | `TokenMeter.measure()` 单例；O(1) replay 折算当前 totalTokens |
| `packages/llm/token-meter/src/estimate.ts` | ~100 | `CHARS_PER_TOKEN=4` 启发式；按 `text`/`tool-call`/`tool-result` 三类 block 分估 |
| `packages/llm/token-meter/src/usage-projection.ts` | 206 | `tokenUsageProjection` (Σ buckets) + `contextPressureProjection` (provider 锚 + 表面 delta) |
| `packages/compaction/compaction-basic/src/config.ts` | 310 | 双阈值：`thresholdRatio=0.8` / `retainRatio=0.16`；按 provider/model 路由 |
| `packages/compaction/compaction-basic/src/index.ts` | 431 | `_registerAutomaticCompaction` —— pre-step pressure + request-error overflow 双触发 |
| `packages/compaction/compaction-tool-result-pruner/src/index.ts` | 187 | 确定性 head/middle/tail 截断；插 `PRUNE_MARKER` 占位 |

### 4.2 估算器：CHARS_PER_TOKEN=4 + BLOCK_OVERHEAD（`packages/llm/token-meter/src/estimate.ts`）

```typescript
const CHARS_PER_TOKEN = 4
const BLOCK_OVERHEAD = 2
const ROLE_OVERHEAD = 3

export function estimateContent(blocks: readonly ContentBlock[]): number {
  let tokens = 0
  for (const block of blocks) {
    switch (block.type) {
      case 'text': case 'reasoning':
        tokens += Math.ceil(block.text.length / CHARS_PER_TOKEN) + BLOCK_OVERHEAD
      case 'tool-call':
        tokens += Math.ceil(block.name.length / CHARS_PER_TOKEN)
                + Math.ceil(block.arguments.length / CHARS_PER_TOKEN)
      case 'tool-result':
        tokens += estimateContent(block.content) + BLOCK_OVERHEAD
      case 'image':
        tokens += BLOCK_OVERHEAD + Math.ceil(JSON.stringify(block).length / CHARS_PER_TOKEN)
    }
  }
  return tokens
}
```

**特点**：

- 文本估算用 `Math.ceil(text.length / 4)`（字符长度而非 rune 长度，**故意低估** CJK 字符的 token 密度）。
- 不调真实 tokenizer（避免 BPE 引擎启动成本与异步等待）。
- `image` 块走 JSON 字符串估算 —— 因为真实字节大小未知，**故意高估** 给系统"容错空间"。

### 4.3 Provider 锚点 + Surface Delta（`usage-projection.ts:163-206`）

`contextPressureProjection` 维护 3 个独立状态：

```typescript
interface ContextPressureState {
  contextWindow?: number              // provider 下发的窗口大小
  pressureTokens?: number             // provider 报告的 prompt + cache
  surfaceTokens: number               // 启发式计算的当前 surface 总量（O(1) running）
  sampledSurfaceTokens?: number       // 上次采到 usage 时的 surfaceTokens
  claim?: ShadowPriceClaim            // compaction/prune 留的"已 shadowed"账本
}
```

`view()` 输出（`usage-projection.ts:198-205`）：

```typescript
view: ({ contextWindow, pressureTokens, surfaceTokens, sampledSurfaceTokens }) => ({
  ...contextWindow === undefined ? {} : { contextWindow },
  ...pressureTokens === undefined ? {} : { pressureTokens },
  ...pressureTokens === undefined || sampledSurfaceTokens === undefined ? {}
    : { projectedTokens: Math.max(0, pressureTokens + surfaceTokens - sampledSurfaceTokens) },
})
```

**`projectedTokens` 是关键设计**：provider 报告的 `pressureTokens` 是"上次请求时"的 prompt 大小，**那之后**又入了新消息；用当前 surface 减去采样时的 surface 拿到**增量**，加到 pressure 上 = "下一次请求的预测 prompt 大小"。这避免了"必须等 400 才知道超了"的问题。

`meter.measure()`（`index.ts:116-141`）则更精细：

```typescript
measure(session, requestHeader?): TokenMeasurement {
  // 1. 找最近一次 provider-anchored 报告
  const baseline: TokenMeasurementBaseline =
    lastUsage ? { kind: 'usage', tokens: providerTokens, usage } :
    lastHeader && !newerHeader ? { kind: 'estimated', tokens: estimateHeader(lastHeader) } :
    { kind: 'none', tokens: 0 }
  // 2. 算 current surface vs baseline surface 的 signed delta
  const surfaceDeltaTokens = currentSurfaceEstimate - baselineSurfaceEstimate
  // 3. total = max(0, baseline + delta)
  return { logRevision, baseline, surfaceDeltaTokens, totalTokens: Math.max(0, baseline.tokens + surfaceDeltaTokens), ... }
}
```

**3 选 1 的基线选择**：

1. **provider usage**（最权威）—— 走 `lastUsage` 路径。
2. **estimated header** —— 若有 `request/header` 但更新过路由，用估计补位。
3. **none** —— 全新会话，无锚点。

### 4.4 双阈值 + 双触发（`compaction-basic/src/config.ts:144-148`）

```typescript
const DEFAULT_THRESHOLD_RATIO = 0.8
const DEFAULT_RETAIN_RATIO = 0.16

export function resolveCompactSpec(policy, contextWindow) {
  const thresholdTokens = Math.floor(contextWindow * policy.thresholdRatio)
  const retainTokens = policy.retainTokens === undefined
    ? Math.floor(contextWindow * policy.retainRatio)
    : policy.retainTokens
  if (retainTokens >= thresholdTokens) throw new TargetPressureConfigError(...)
  return { contextWindow, thresholdRatio, thresholdTokens, retainTokens, ... }
}
```

| 比率 | 含义 | 默认 |
|---|---|---|
| `thresholdRatio` | "开始考虑压缩" | 0.8 = 80% |
| `retainRatio` | "压缩后保留最近" | 0.16 = 16% |

**校验硬约束**：`retainTokens < thresholdTokens`（`config.ts:148-154`），保留量不能比阈值还大否则触发条件就永不到。

> **关键事实**：**没有 output-token 预留**从 `contextWindow` 减去 —— `thresholdTokens = floor(window * 0.8)` **原样**。唯一的 headroom 机制是 verbatim-tail `retainTokens` 预算（`selectCompactableRange` 中用）。

触发路径（`compaction-basic/src/index.ts:137-223`）：

```typescript
ctx.on('agent/pre-step', async ({ agent, signal }, next) => {
  if (!signal.aborted) {
    try { await this.compactIfNeeded(agent, 'pressure', signal) } catch (e) { ... }
  }
  return next()
})

ctx.on('agent/request-error', async ({ agent, failure, signal }, next) => {
  if (failure.code !== CONTEXT_WINDOW_EXCEEDED_CODE || signal.aborted) return next()
  const retries = this.overflowRetries.get(agent) ?? 0
  if (retries >= policy.maxOverflowRetries) return next()
  const result = await this.compactIfNeeded(agent, 'context-overflow', signal)
  if (result !== null) this.overflowRetries.set(agent, retries + 1)
  return { kind: 'retry' }
})
```

**pre-step 主动 + request-error 兜底**是经典"fail-soft + fail-fast"组合：

- **正常路径**（80% 阈值）—— `compactIfNeeded(agent, 'pressure')` 先看 `totalTokens < thresholdTokens` 直接返回（`index.ts:304`），再尝试 `toolResultPruner.pruneSession()`（**模型自由**的剪枝），再 remeasure，仍超就进入 `compactRegion` 跑 LLM 摘要（`index.ts:308-326`）。重试 `compactionRetries` 次。
- **兜底路径**（provider 400）—— `compactIfNeeded(agent, 'context-overflow')` **跳过阈值检查**、强制压缩（`index.ts:283-291` 的 `retainTokens=0` → 找最大 head-anchored balanced prefix），**最多重试 `maxOverflowRetries` 次**。`index.ts:167-169` 的 `agent/status === idle` 监听器清空 `overflowRetries`，让"成功一次响应就重置"成为不变量。

**bracket-first ordering**（`compaction-basic/src/region.ts:189-217`）—— 一次压缩事务 6 步：

1. `compaction/start` **先** append（**synchronously** before await）→ 持久化 marker 即 lock，并发 attempt 不能交错。
2. prepare（select range + validate balanced boundary）。
3. LLM summarize（reuses system+tools+region messages → cache 友好）。
4. stability check（`whole-surface` 时整 priced surface `deep-equal`；`selected-span` 时只检查 shadowed seqs）。
5. `compaction/summary` + `user/message`(surface `replace`) **同事务**追加；summary 帧携带 `shadowedRange/shadowedSeqs/shadowedTokenCount/provider/model/usage`。
6. `compaction/end`；manual 模式额外 `flush`。

**任何一步失败**：写**仅**一个 `compaction/end { error: errorChain(error) }`，**不**写 `user/message`（`region.ts:218-228`）—— 表面未动；但未闭合的 `compaction/start` 阻塞后续 attempts（`invariant.ts:162` 报错"start while ${owner} is still compacting"），必须 `session/end-seed` 边界或下次成功 attempt 让它失效。

### 4.5 `selectCompactableRange` —— tail-anchored + balanced boundary（`compaction-basic/src/region.ts:98-134`）

```typescript
export function selectCompactableRange(session, measurement, retainTokens) {
  const pricedNodes = measurement.nodes
  if (pricedNodes.length === 0) return null
  // ... verify surface/parity ...
  let accumulated = 0
  let keepFromIdx = pricedNodes.length
  for (let index = pricedNodes.length - 1; index >= 0; index -= 1) {
    accumulated += pricedNodes[index]!.tokens
    keepFromIdx = index
    if (accumulated >= retainTokens) break
  }
  if (keepFromIdx === 0) return null

  // 推前直到 balanced boundary（不切分 open tool-call pair）
  while (keepFromIdx > 0) {
    if (toolPairingBalancedBefore(session, surfaceNodes[keepFromIdx]!)) break
    keepFromIdx -= 1
  }
  if (keepFromIdx === 0) return null

  const first = surfaceNodes[0]!
  const cutoff = surfaceNodes[keepFromIdx - 1]!
  return { start: first, end: cutoff }
}
```

**关键设计**：

- **tail-anchored** —— 从尾部开始累加 priced tokens 直到 ≥ `retainTokens`，得到 `keepFromIdx`。
- **balanced-boundary correction** —— 若 `keepFromIdx` 落在 open `tool-call` 中间，向前推直到 `toolPairingBalancedBefore` 为真。**这一推可能把 `retainTokens` 实际保留量推大**（设计接受）。
- **head 全取** —— 从 `surfaceNodes[0]` 到 `surfaceNodes[keepFromIdx-1]` 全部入 summary 区。

`validateSurfaceRegion`（`region.ts:315-336`）在 commit 入口强制**双端** balanced：

```typescript
if (!toolPairingBalancedBefore(session, nodes[startIdx]!)) {
  throw new Error(`compactRegion: start seq ${start} is not a balanced boundary (would split a step's tool-call/result pair)`)
}
if (!toolPairingBalancedAfter(session, nodes[endIdx]!)) {
  throw new Error(`compactRegion: end seq ${end} is not a balanced boundary (would split a step, or the step is still open)`)
}
```

### 4.6 `compaction-tool-result-pruner`（`compaction-tool-result-pruner/src/index.ts`）

**模型自由的确定性剪枝**，不被 LLM 决策污染：

- 触发：`compactIfNeeded` 在进入 LLM 摘要之前先 `pruneSession(session)`（`compaction-basic/src/index.ts:283-287`）。
- 阈值：`thresholdChars`（默认 8192）+ `headChars`（4096）+ `tailChars`（1024）。
- **head + marker + tail 必须 ≤ thresholdChars** —— 在 plugin load 时校验（`config.ts:55-62`）。

**算法**（`index.ts:78-115`）：

```typescript
pruneContent(blocks): ContentBlock[] | null {
  const totalChars = this.measureContent(blocks)
  if (totalChars <= this.config.thresholdChars) return null
  // ... code-point-based head/tail 切分 ...
  for (const block of blocks) {
    if (block.type !== 'text') { pruned.push(block); continue }   // rich blocks 保持位置
    const points = Array.from(block.text)                          // 按 code point 切（不切 surrogate）
    // 计算 head/tail 在该 block 的 [headEnd, tailStart) 区间
    const intersectsRemoved = blockStart < removedEnd && blockEnd > removedStart
    const marker = intersectsRemoved && !markerInserted ? PRUNE_MARKER : ''
    if (marker.length > 0) markerInserted = true
    const text = points.slice(0, headEnd).join('') + marker + points.slice(tailStart).join('')
    if (text.length > 0) pruned.push({ ...block, text })
  }
  if (!markerInserted) throw new Error('tool-result prune: failed to locate the removed text span')
  // ...
}
```

- `measureContent` **只数 text 块的 code points**（`index.ts:68-74`）—— image/rich block 计 0。
- 按 **Unicode code point** 而非 UTF-16 code unit 切，避免 surrogate pair 切分（**grapheme cluster 仍可能切**，注释明确）。
- **`PRUNE_MARKER = '\n\n[... tool result middle pruned ...]\n\n'`** 是唯一 inline 可见信号（`config.ts:7`）。
- rich block（image 等）保留原位置。

**shadow-price 协议**（`index.ts:144-184`）：

```typescript
session.append('compaction/prune', {
  shadowedRange: { start: seq, end: seq },
  shadowedSeqs: [seq],
  shadowedTokenCount: this.ctx.tokenMeter.estimateMessage(event.data.message),  // 原始 message 的 priced tokens
})
const replacement = session.append('tool/result', {
  ...event.data,            // 保留 turn/step/callId/error/meta
  message,
}, {
  surfaceOp: { op: 'replace', start: seq, end: seq },
  sourceEventSeqs: [seq],
})
```

- `compaction/prune` 是 `compaction/*` 的**log-only**事件（无 `surfaceOp`），承载 `shadowedTokenCount`。
- 替换的 `tool/result` `surfaceOp: {op:'replace', start, end}` 替换原位置。
- `usage-projection.ts:188-192` 读 `compaction/prune` 的 `shadowedTokenCount` → `meter.claim` → `surfaceTokens` 下调。
- **不破坏 tool pair**：`tool/result` 的 `source.callId`、`turn`/`step`/`isError`/`error`/`meta` 全部 verbatim 保留（`tool-result-pruner/tests/tool-result-pruner.spec.ts:172-204` 钉这一性质）。

### 4.7 压缩摘要的 cache 友好性

`BasicCompactionEngine.summarize()` 注释（`compaction-basic/src/index.ts:227-235`）：

```typescript
/**
 * Summarize the replayed conversation region through a direct one-shot
 * `ctx.llm.stream()` call whose prefix reuses the conversation's own system
 * prompt, tools, and messages so the provider's KV cache is not invalidated.
 */
protected async summarize(input, agent, signal?) { ... }
```

—— 摘要调用**复用** system + tools + 头部 messages，所以 provider KV 缓存继续命中。这是 §2.7 "Harness 不下 cache 断点"的延伸：让历史前 N 轮对所有调用方（普通对话、压缩、标题生成）**都稳定**，自动获得最大 cache 命中。

### 4.8 Summary 构造（`compaction-basic/src/summarizer.ts:189-224`）

```typescript
export function frameSummary(summary: readonly ContentBlock[]): ContentBlock[] {
  return [
    { type: 'text', text: `${CHECKPOINT_PREAMBLE}\n\n${SUMMARY_OPEN_TAG}` },  // <compacted-summary>
    ...summary,
    { type: 'text', text: SUMMARY_CLOSE_TAG },                                  // </compacted-summary>
  ]
}
```

替换 `user/message` 的 `source` 携带 `compactCheckpointSource(compactionId, sourceCommandId)`（`compaction/src/checkpoint.ts:33`）—— `isCompactCheckpointSource` 谓词让跨 session 引入快照（`session-reference`）能识别"这是压缩 checkpoint 而不是真实 user 消息"。

**安全性**：

- 摘要输出**只接受 text 块**（`summarizer.ts:217-224`），image 输出抛 `UNSUPPORTED_CONTENT`。
- **摘要必须真小**：`framedSummaryTokenCount >= prepared.shadowedTokenCount` 即 `Error('summary is not smaller than the shadowed content')`（`region.ts:368-378`）—— 防"压缩反而更长的退化"。
- summarization prompt 是 8 段 Markdown 模板（`Primary Request` / `Key Concepts` / `Files and Code` / `Errors and Fixes` / `Pending Jobs` / `Current Work` / `Next Step` / `Critical Context`），且**显式指令合并旧 summary**而非搬运（`summarizer.ts:31-66`）。
- target 解析顺序：`configured` (LLM 配置) ?? `latest` (session.requestHeader 末次) ?? `agentTarget` (agent.options) —— 提供三层 fallback（`summarizer.ts:128-138`）。
- `ctx.llm.stream(options)` 调用时 `purpose: 'compaction'` —— Adapter 端可借此选不同采样参数（DeepSeek 的 `serializeRequest` 在 `purpose === 'session-title'` 时强制 `thinking: 'disabled'`，`serialize.ts:38`）。

### 4.9 预算决策表

| 场景 | 决策 |
|---|---|
| `measurement.totalTokens < thresholdTokens` | 不压缩，正常发 |
| `pruneSession()` 后仍 < threshold | 仍不压缩（prune 本身降低了 total） |
| `pruneSession()` 后 ≥ threshold，compactionRetries 内每次 `compactRegion` 仍 ≥ threshold | 抛错（不可能压缩到阈值以下） |
| 收到 400 `CONTEXT_WINDOW_EXCEEDED_CODE` | 强制压缩（跳过阈值） + 重试 `maxOverflowRetries` 次 |
| 同一对话每响应成功一次 | `overflowRetries` 清空 |
| 路由改变 provider/model | `resolveTargetPolicy` 按 `provider+model` 查覆盖 |

---

## 5. 消息投影与配对

### 5.1 模块职责

> ⚠️ **概念修正**：下文初稿把"事件 → messages"投影描述为 `session-projection/` 的工作，这是误判。真实情况是：
> - `packages/session/session-projection/` 与 `session-projection-cache/` 是**领域无关的 state-fold 注册表**（如 `todo/tool-todo`、`interaction/permission-presets`），**不是** LLM messages 投影器（其 `SessionProjectionMap` 不含 `messages` 键）。
> - 真正的 LLM `messages[]` 投影在 `packages/core/session/src/surface.ts` 的 `SurfaceManager` 与 `packages/core/session/src/index.ts:702-747` 的 `Session.deriveMessages()` 内。
> - Agent loop 唯一读 `messages[]` 的入口是 `packages/core/agent-loop/src/agent.ts:341`。

| 文件 | 行数 | 职责 |
|---|---|---|
| `packages/core/session/src/types.ts` | 400+ | `SessionEventMap` 合并可扩展、3 类 `SurfaceEventType`（`user/message` / `assistant/message` / `tool/result`）、`SurfaceOp` |
| `packages/core/session/src/surface.ts` | ~460 | `SurfaceManager` 增量折叠 + `deriveEventMessage` 单一投影规则 |
| `packages/core/session/src/index.ts` | 700+ | `Session.deriveMessages()` 缓存版（按 `replaceGeneration` 失效） |
| `packages/core/session/src/repair.ts` | ~150 | `interruptedTurnClosers` 崩溃修复：孤儿 tool_call 注入合成 `tool/result` |
| `packages/compaction/compaction/src/tool-pairing.ts` | 131 | `toolPairingBalancedBefore/After` —— compaction 切点必须平衡 |
| `packages/llm/llm/src/message.ts` | 261 | `Message` 不可变身份 + 6 类 `ContextForm` + `source` 7 字段 |
| `packages/session/session-projection/src/index.ts` | 428 | **非** LLM 投影：领域无关 state-fold registry（todo / permission-presets 等） |
| `packages/session/session-projection-cache/src/index.ts` | 300 | **非** LLM 投影：上述 fold 的 per-session 持久化快照（whole-record 写 + ver/seq/identity 3 维失效） |

### 5.2 `SurfaceManager` —— 唯一可投影事件的折叠器（`packages/core/session/src/surface.ts`）

`SurfaceEventType` 是 3 类事件的闭合联合（`types.ts:343-346`）。`SurfaceManager._processDelta` 增量折叠：

```typescript
function applySurfacePlan(state, plan) {
  if (plan?.kind === 'append') {
    state.nodes.push(plan.seq)            // 直接加
  } else if (plan?.kind === 'replace') {
    state.nodes.splice(plan.startIdx, plan.endIdx - plan.startIdx + 1, plan.seq)
    state.replaceGeneration += 1          // 缓存失效信号
  }
}
```

`planSurfaceEvent` 在 append 前对每个事件做 3 类验证（`surface.ts:321-347`）：

1. `event.seq === expectedSeq`（事件日志必须严格连续）。
2. 仅 3 类 `SurfaceEventType` 可携带 `surfaceOp`；其他类型若带 `surfaceOp` 即拒。
3. `replace` 调 `replacementRange()` 查 `startIdx/endIdx`，调 `assertProvenance()` 验证 `sourceEventSeqs` 覆盖所有被 shadow 节点，调用 `assertToolResultRewrite()` 守更窄的 tool/result 重写规则。

### 5.3 `deriveEventMessage()` —— 单一投影规则（`surface.ts:83-114`）

```typescript
export function deriveEventMessage(event: SessionEvent): Message | null {
  switch (event.type) {
    case 'user/message':     return event.data                      // 整体透传
    case 'assistant/message':
      if (event.data.message.content.length === 0) return null     // 0 块不入对话
      return event.data.message
    case 'tool/result':      return event.data.message              // tool_result 整体透传
    default:                  return null                            // 其它全部返 null
  }
}
```

**3 条关键不变量**：

- **3 类事件按 verbatim 投影**（无字段重映射，无重排），保证重放可字节重建。
- **`turn/start`/`step/start`/`assistant/chunk`/`request/header`/`todo/write`/`request/context`/`session/end-seed`/`compaction/*` 全部返回 `null`** —— 它们是 bracket/log-only 元事件，从不直接进 LLM messages。
- **空内容 assistant/message 也返 null**（`surface.ts:96-98`）—— 即"`max-tokens` 步骤的 `usage` 宿主消息"绝不能以"空 assistant 轮"出现在 provider transcript。

### 5.4 `Session.deriveMessages()` —— 缓存版派生（`packages/core/session/src/index.ts:702-747`）

```typescript
private derived: Message[] = []
private derivedNodes = 0
private derivedGeneration = 0

deriveMessages(): Message[] {
  const surface = this.surface
  const nodes = surface.nodes
  const generation = surface.replaceGeneration
  if (generation !== this.derivedGeneration) {
    this.derived = []; this.derivedNodes = 0; this.derivedGeneration = generation
  }
  for (const seq of nodes.slice(this.derivedNodes)) {
    const msg = this.deriveEventMessage(this.log[seq]!)
    if (msg) this.derived.push(msg)
  }
  this.derivedNodes = nodes.length
  return [...this.derived]
}
```

**关键设计**：

- **以 `replaceGeneration` 做缓存键**（不是 seq 数量）—— 任何 `replace` 触发 `replaceGeneration += 1`，缓存整体清空重算；纯 `append` 不重算。
- **从 `derivedNodes` 增量 push** —— 单次 `deriveMessages()` 实际只跑 N 节点（新增部分），不会重排历史。
- **返回 `[...this.derived]` 防御性拷贝** —— 防止外部修改污染缓存。

### 5.5 Agent loop 唯一读 messages 的入口（`packages/core/agent-loop/src/agent.ts:341`）

```typescript
const { request, preparedCall } = await this.buildRequest(
  turn, step, assembly.tools, system, this.session.deriveMessages(), signal,
)
```

`buildRequest` 立即 `deepFreeze` 整组 messages（`agent.ts:486-494`），并把 `markAgentLoopRequest` 标识写进 `WeakSet`。invariant companion 在 `llm/stream` waterfall 头部校验：

```typescript
// agent-loop/src/invariant.ts:39-42
const expected = session.deriveMessages()
if (JSON.stringify(options.messages) !== JSON.stringify(expected)) {
  fail(`llm request for session "${session.id}" diverges from the dispatch-time durable derivation (log-reconstruction desync)`)
}
```

这是 §8.3 请求重建不变量在线的护栏 —— **任何想塞私货的 listener 必失败，只能通过改事件间接改 messages**。

### 5.6 `Message` 不可变性（`packages/llm/llm/src/message.ts:128-186`）

```typescript
export interface Message {
  readonly id: MessageId
  readonly role: 'system' | 'user' | 'assistant'
  readonly content: ContentBlock[]
  readonly source: MessageSource
}
```

- **每次创建都 deepFreeze**（`createMessage` 调 `freezeMessage({...})`，`message.ts:178-185`）。
- `deepFreeze` 用**迭代遍历**而非递归（`call-config.ts:88-117`），避免 stack overflow。
- **显式跳过 `AbortSignal`**（`call-config.ts:104`）—— 信号是 live cancellation channel，冻结会破坏 abort。
- `MessageId` 用 `crypto.randomUUID()`，跨表示边界稳定（同一条消息在 logs / delivery / request 三处身份一致）。

### 5.7 Tool Call / Tool Result 配对 —— 5 道防线

DSH 不用 `tool-call-id` 做消息配对键，配对是**协议级事件序列约束**。下表 5 道防线，任何单点失效都有兜底：

| # | 防线 | 文件:行 | 失效场景 |
|---|---|---|---|
| 1 | **scheduler 永不产孤儿** —— 在 dispatch 前 abort 的 call 自动追加 `TOOL_ABORTED_BEFORE_DISPATCH` 合成 result | `core/agent-loop/src/tool-calls.ts:248-258` | 用户主动 cancel / step 中断 |
| 2 | **session invariant 在 append 时校验** —— orphan result 必 `fail`（仅豁免 `TOOL_NOT_STARTED`） | `core/session/src/invariant.ts:122-143` | 编程错误导致 `tool/result` 缺 `tool/call` |
| 3 | **崩溃修复** —— `interruptedTurnClosers` 扫 `pendingCalls`，2 类合成 result（`TOOL_NOT_STARTED` / `TOOL_OUTCOME_UNKNOWN`），并补 `step/end` + `turn/end { kind: 'interrupted' }` | `core/session/src/repair.ts:91-132` | 进程被杀、日志不完整 |
| 4 | **`max-tokens` 时 `BlockAssembler` 主动过滤** —— 半截 `tool_call` 块根本不进 messages | `llm/llm/src/assembler.ts:136-138` | 模型生成长 tool_call 触顶 |
| 5 | **compaction 切点必须 balanced** —— `toolPairingBalancedBefore/After` 沿 `surface.nodes` 跑累计 delta，验证 `eventDelta` 不为负 | `compaction/compaction/src/tool-pairing.ts` + `compaction-basic/src/region.ts:315-336` | compaction 切错位置 |

**防线 3 典型代码**（`core/session/src/repair.ts:91-132`）：

```typescript
for (const [callId, { step, callSeq }] of pendingCalls) {
  const started = callSeq !== undefined
  const message: ToolResultMessage = freezeMessage({
    id: MessageId(`interrupted-tool-result-${callId}-${seq}`),
    role: 'user', source: { kind: 'tool', callId },
    content: [{ type: 'tool-result', toolCallId: callId, isError: true, content: [{ type: 'text', text: started
      ? 'The tool call was interrupted after it was recorded, but no result was durably recorded. ...'
      : 'The tool call was interrupted before the Harness recorded it as started. Retry it if it is still needed.' }] }],
  })
  closers.push({ type: 'tool/result', ..., data: { turn: openTurn, step, message, error: started
    ? { name: 'ToolOutcomeUnknownError', code: TOOL_OUTCOME_UNKNOWN }
    : { name: 'ToolNotStartedError', code: TOOL_NOT_STARTED } },
    surfaceOp: 'append', ...started ? { sourceEventSeqs: [callSeq] } : {} })
}
if (openStep !== null) closers.push({ type: 'step/end', ..., data: { turn: openTurn, step: openStep } })
closers.push({ type: 'turn/end', ..., data: { turn: openTurn, reason: { kind: 'interrupted' } } })
```

### 5.8 投影顺序

- **顺序 = 日志序**（`event.seq` 严格单调，invariant `core/session/src/invariant.ts:60-62` 保证）。
- **`replace` 后 `nodes` 不再数字单调** —— 一次 `replace(start,end)` 把 `seq=N` splice 进 `[seq_i, ..., seq_j]`，表面序与日志序分叉。compaction 选点用**位置索引** `keepFromIdx`（`compaction-basic/src/region.ts:98-134`）而非 seq。
- **agent loop 消费点**：`packages/core/agent-loop/src/agent.ts:341` 唯一。

### 5.9 SessionReference 的轻量投影语义（`packages/context/session-reference/src/projection.ts:36-60`）

跨 session 引入快照时 `projectSessionConversation` 的规则可参考：

```typescript
function projectSessionConversation(snapshot: SessionSurfaceSnapshot): ProjectedItem[] {
  for (const event of snapshot.events) {
    switch (event.type) {
      case 'user/message': {
        const checkpoint = isCompactCheckpointSource(event.data.source)
        if (!checkpoint && event.data.source.kind !== 'user') break
        const text = textContent(event.data.content)
        if (text !== '') conversation.push({ role: 'user', text, checkpoint, originalText: text, omittedBytes: 0 })
        break
      }
      case 'assistant/message': {
        const text = textContent(event.data.message.content)
        if (text !== '') conversation.push({ role: 'assistant', text, checkpoint: false, originalText: text, omittedBytes: 0 })
        break
      }
      case 'tool/result': break  // 显式跳过 — 与 §5.3 主规则一致
    }
  }
}
```

**3 条规则**（与 §5.3 主投影规则完全一致）：

1. **tool/result 不入对话** —— 它是 tool_call 的依附，前一轮 assistant 的 `tool-call` 块已隐含其结构。
2. **非 user 写的 user message = checkpoint 保留** —— `isCompactCheckpointSource` 让压缩摘要消息可以出现（参考 §6.5）。
3. **assistant message 必有 `source.kind === 'model'` + `provider/model`** —— replay 必要条件。

### 5.10 `session-projection/` vs `session-projection-cache/` —— 它们做什么

这两个包**不是** LLM messages 投影器。其自描述（`session-projection/README.md:58`）：

> *"The registry only computes client-facing read models of already-logged session state and touches no prompt, message, schema, stream, or tool result."*

它们是**领域无关的 state-fold 注册表**：

- `SessionProjectionMap` 是空表，领域包（`todo/tool-todo`、`interaction/permission-presets`）通过 `declare module` merge 自己的键。
- `ProjectionDefinition<K, S>` 是单 fold 单元的契约：`{ key, schema (zod), init, apply, view, stateVersion }`。
- `SessionProjectionCache` 提供 per-session 持久化快照（`CheckpointRecord` = `{identity, rows: Map<key, {ver, seq, val}>}`），按 `ver`/`seq`/`identity` 3 维失效。

| 事件 | 谁在写入/读出 | LLM 影响 |
|---|---|---|
| `tokenUsage` | token-meter | 监控/计费 |
| `contextPressure` | token-meter | pre-step 主动压缩判断（§4.4） |
| `contextBreakdown` | token-meter | UI 三类拆分（system/tools/messages） |
| `subagent/descriptor` | subagent | 子代理枚举/恢复（§7.5） |
| `permissionPresets` | interaction | tool approval UI |

两者的 `invariant.ts` 都是**空**（`install: () => {}`），其官方理由："re-running the fold to verify the cached row IS the fold at its `seq` watermark would be duplicating the implementation rather than detecting drift" —— 即"重复实现来验证缓存一致性"违反 DRY。

---

## 6. 流式、超时与背压

### 6.1 流协议（已在 §2.4 详述）

简表：

| 帧 | 来源 | 触发 | 终结 |
|---|---|---|---|
| `block-start` | adapter | 流开始或新 block | 必有 `block-end` 配对 |
| `{text,reasoning,tool-call}-delta` | adapter | 收到增量 | 流继续 |
| `block-end` | adapter | block 完成 | 不可再发该 index 的 delta（stragglers 静默忽略，`assembler.ts:77-82`） |
| `usage` | adapter | 流终止前 | 至多 1 次（invariant） |
| `finish` | adapter | 流的最后一帧 | 携带 `FinishReason` + `replayState`；后续帧违规（`error`/`aborted` 除外） |

**DeepSeek 翻译器**（`llm-deepseek/translate.ts:86-185`）的特别设计：

- `block-end` / `usage` / `finish` **全部延迟到 `[DONE]` 哨兵** 才发 —— 保证不会"块结束前发出 finish"。
- `block-index` 单调递增（`nextIndex++` 每次 `open()`），text/reasoning/tool-call 共享同一 index 空间。
- 0 块 `stop` 终止映射为 `{kind:'error', code:'EMPTY_RESPONSE'}`（`translate.ts:107-115`）—— 属默认重试白名单，等价"什么都没发生"。

**pi-ai 翻译器**（`llm-pi-ai/stream.ts:124-208`）从库内 typed `AssistantMessageEvent` 桥接：

- `text_start/_delta/_end` → `block-start`/`text-delta`/`block-end({type:'text', text:event.content})`。
- `thinking_*` → reasoning 块。
- `toolcall_*` → tool-call 块；最终 `arguments` 用 `JSON.stringify(event.toolCall.arguments)` 还原为 wire 原始 JSON 字符串。
- 缺终止事件 → 抛 `STREAM_CLOSED`（**不**走 `TIMEOUT`，**不**在默认重试白名单 —— 这是 `STREAM_CLOSED` 与 idle `TIMEOUT` 的关键语义分界）。
- 内容溢出通过 `isContextOverflow(message, contextWindow)` + 文案 `CONTEXT_WINDOW_EXCEEDED_CODE` 检测。

### 6.2 idle timeout —— 共享 `@deepseek-ai/dsh-timeout` 的 `idleWatchdog`

`packages/util/timeout/src/index.ts:126-173` 暴露 `idleWatchdog(upstream, timeoutMs, code)`：每个 outstanding `iterator.next()` arm 一次 `setTimeout`，每次上游产出字节都 `pulse()` 刷新；超时即 `AbortController.abort(new TimeoutReason(code, timeoutMs))`。**DeepSeek + pi-ai adapter 都用这一份实现**（`llm-deepseek/src/adapter.ts:227-247` + `llm-pi-ai/src/adapter.ts:294-356`）：

```typescript
const consumer = new AbortController()
const upstream = options.signal === undefined
  ? consumer.signal
  : AbortSignal.any([options.signal, consumer.signal])
using watchdog = idleWatchdog(upstream, connection.streamIdleTimeoutMs, STREAM_IDLE_TIMEOUT_CODE)
const iterator = this.request(...)[Symbol.asyncIterator]()
let exhausted = false
try {
  while (true) {
    const result = await watchdog.next(iterator)   // arm → next() → next 完 clear
    if (result.done) { exhausted = true; return }
    yield result.value
  }
} catch (error: unknown) {
  if (timeoutOf(watchdog.signal, STREAM_IDLE_TIMEOUT_CODE) !== undefined) {
    throw new LlmError(`DeepSeek stream idle timeout after ${connection.streamIdleTimeoutMs}ms`, 'TIMEOUT', { cause: error })
  }
  if (options.signal?.aborted) {
    throw new LlmError('DeepSeek request aborted by caller', 'ABORTED', { cause: error })
  }
  // ...
} finally {
  consumer.abort('DeepSeek stream consumer stopped')
  if (!exhausted && iterator.return !== undefined) {
    try { await iterator.return() } catch (_abortedTransportTeardown) { /* ... */ }
  }
}
```

**关键设计**：

- `streamIdleTimeoutMs` 默认 **5 分钟**（`llm-deepseek/src/index.ts:77` + `llm-pi-ai/src/config.ts:35`）。
- **idle per-read timeout**（非总请求 timeout）—— 上游每发一字节就 `pulse()` 重置定时器；SSE `:` 注释行也走 `pulse()`（`llm-deepseek/src/adapter.ts:234` 传 `() => watchdog.pulse()` 作为 `parseSse` 的 `onComment`）。
- `using` 语法（`Symbol.dispose`）保证异常路径也清理 timer。
- `consumer.abort(...)` 切断"上游继续生产但 caller 不读"的反向传播 → `iterator.return()` 触发 transport teardown。
- 错误码语义分界：`STREAM_CLOSED`（库/协议错误）**不**在默认重试白名单；`TIMEOUT`（idle 超时）**是**（`llm-retry` 默认白名单）。`llm-retry/tests/transport-recovery.spec.ts:174-198` 钉了这一点："clean partial EOF → STREAM_CLOSED → no retry"。

**pi-ai 端**：除了 idle watchdog，pi-ai SDK 还有 `timeoutMs` / `websocketConnectTimeoutMs`（`llm-pi-ai/src/config.ts:133-138`），是 SDK 级的 fetch timeout，与 idle watchdog 是两层独立防护。

`idleWatchdog`（`packages/util/timeout/`）是一个**单定时器** —— 每次上游发出字节时**重置**定时器；空闲超过 `streamIdleTimeoutMs`（默认 5 分钟，见 `llm-deepseek/src/index.ts:77`）即抛 `TIMEOUT`。

- 这与 §4.4 的 `TIMEOUT` 错误码**直通** retry 路径 → 7 bot 房间不会出现"一个慢模型拖垮全员"。
- `using` 语法保证异常路径也清理 watchdog timer（`Symbol.dispose`）。

### 6.3 AbortSignal 全链路

`abort` 在 DSH 中是**一阶公民**：

| 节点 | 信号源 |
|---|---|
| 用户主动 cancel | `agent.cancel({ kind: 'user' })` → `phase.abort.abort()`（`agent.ts:134-140`） |
| Lifecycle dispose | `prepare()` 内 `abort.abort(new Error('agent "..." lifecycle disposed'))`（`index.ts:497-520`） |
| Owner fiber unload | `unfollowOwner` 触发 `ownerAbort.abort()`（`index.ts:672-680`） |
| 配置热改 | `agent-loop` settings 改 maxParallelToolCalls 不影响 abort |
| Stream 上游 | `options.signal` 透传到 adapter fetch 与 reader（`adapter.ts:155-156`） |

**3 个 abort 源 fuse 成一个 controller**：

```typescript
// agent-loop/src/agent.ts:479-487
const abort = new AbortController()
const onCallerAbort = () => abort.abort(...)  // caller 取消
const onFactoryTeardown = () => abort.abort(...)  // factory 卸载
callerSignal?.addEventListener('abort', onCallerAbort, { once: true })
this.ownership.signal.addEventListener('abort', onFactoryTeardown, { once: true })
```

`onCallerAbort` / `onFactoryTeardown` 都把 `reason` 包成 `Error`（`agent.ts:481-486`），避免非 Error reason 在 `abort.signal.reason` 上类型失配。

### 6.4 Adapter 终止 → finish chunk 协议

`LlmRuntime.adapterStream`（`llm/src/index.ts:843-900`）的 catch 分支把"adapter 抛错"转为终止 `finish` chunk：

```typescript
} catch (error: unknown) {
  yield adapterFailureChunk(error, options.signal)
  return
}

function adapterFailureChunk(error: unknown, signal?: AbortSignal): StreamChunk {
  const failure = normalizeLlmFailure(error)
  return {
    type: 'finish',
    reason: signal?.aborted || failure.code === 'ABORTED'
      ? { kind: 'aborted', failure }
      : { kind: 'error', failure },
  }
}
```

**关键不变量**：

- Adapter 抛错**不**是错误向上传播 → caller 总能拿到终结 `finish` 帧。
- `signal.aborted` 优先于错误码 → abort 永远是"主动取消"语义。
- `normalizeLlmFailure`（`adapter-failure.ts`）把任意 Error / non-Error / AggregateError 收敛成 `{ message, code, status?, providerRetryAfterMs? }`，见 §2.6 的"重试白名单"。

### 6.5 Replay 友好的流：块结束才入库

`agent-loop/src/agent.ts:343-373` 钉住了 streaming 与 commit 的边界：

```typescript
const chunkSeqs: number[] = []
for await (const chunk of stream) {
  signal.throwIfAborted()
  chunkSeqs.push(this.session.append('assistant/chunk', { turn, step, chunk }).seq)
  assembler.push(chunk)
}
signal.throwIfAborted()
const finish = assembler.finish
if (finish.kind === 'error' || finish.kind === 'aborted') {
  // ... retry waterfall ...
  continue  // ← 关键：adapter 抛错/被 abort 时整轮重发，**不**写 assistant/message
}

const message = createAssistantMessage({ content: assembler.blocks(), source: { ... } })
this.session.append('assistant/message',
  { turn, step, message, ...assembler.usage === undefined ? {} : { usage: assembler.usage } },
  { surfaceOp: 'append', sourceEventSeqs: chunkSeqs },  // ← 关键：message 事件指向所有 chunks
)
```

**3 条不变量**：

- **每帧都 append** 到 session log —— 流式回放仍能逐帧复现。
- `chunkSeqs` 收集所有 chunk 序号，最终 `sourceEventSeqs: chunkSeqs` 让 `assistant/message` 事件**指向自己的 chunks** —— "替换 message 时同时替换它的 chunks"（**chunks 永不孤立**）。
- **`assistant/message` 仅在 `finish.kind === 'stop' \| 'max-tokens' \| 'tool-calls'` 时提交** —— 任何 error/aborted 都跳过 commit、走 retry 路径。

### 6.6 Transport Recovery —— 部分消费流的 retry 语义

`llm-retry/tests/transport-recovery.spec.ts`（245 行）钉了 6 种真实场景：

1. **Refused connection 恢复**（`:86-110`）：mock bind 端口后 first attempt 失败、retry 时 server 启动；assert `server.requests.length === 1`、`step/start` 计数 1（**未重 step**）、`llm/retry` failure code `'TRANSPORT'`、最终 text `'connected after retry'`。
2. **Stream disconnect / partial disconnect + 已 streaming chunks**（`:112-145`）：`server.requests.length === 2`、两次请求 body 相等、retry 事件前的 `assistant/chunk` (`seq < retryEvent.seq`) 数量 = `failedChunkCount`、**`assistant/message` 计数 = 1**（仅恢复后的 attempt 提交）、failure code `'TRANSPORT'`。**这是 "transport recovery" 关键证据：retry 可在部分流消费后发生；失败 attempt 的 chunks 留作孤儿（可见于 replay），但 `assistant/message` 仅成功 attempt 提交**。
3. **内容为空完成（`stop` + 0 块）**（`:147-172`）→ `EMPTY_RESPONSE` 码（默认重试白名单）；`assistant/message` 计数 = 1（恢复 attempt）；终态 `{kind:'completed'}`。
4. **Clean partial EOF**（`:174-198`）→ `STREAM_CLOSED` 码（**不**在白名单）；`server.requests.length === 1`、**不**发 `llm/retry`、终态 `{kind:'error', error:{code:'STREAM_CLOSED'}}` —— 钉住"协议错误 vs 瞬断"的语义分界。
5. **Stalled body → `TIMEOUT`**（`:200-219`）：`server.requests` map `['stall', 'success']`、failure code `'TIMEOUT'`、恢复后 text 正常。
6. **Retry budget exhausted**（`:221-244`）：3 次 `connection_reset` 后 `server.requests.length === 3`、`llm/retry` 计数 = `maxRetries`、终态 `{kind:'error', error:{code:'TRANSPORT', message:含原 transport 错误}}` —— 原 error 透传。

**给狼人杀的启示**：当 §20260813-04 §20260814-02 借鉴 Hermes 加 `preflight` 收口时，"快速模型空响应"(`EMPTY_RESPONSE`) 与"流截断"(`STREAM_CLOSED`) 必须分别处理 —— DSH 的 6 测试矩阵是验证任何 retry 白名单扩展的最小集。

### 6.7 Rate-Limit 反馈

仓库 grep `ratelimit` / `x-ratelimit` **零命中** —— provider 的剩余配额 headers **不**被解析或本地缓存。唯一被消费的信号是 `Retry-After` HTTP header（`llm-deepseek/src/adapter.ts:117-125`）：

```typescript
function providerRetryAfterMs(value: string | null): number | undefined {
  if (value === null) return undefined
  if (/^\d+$/.test(value)) {
    const delay = Number(value) * 1_000
    return Number.isFinite(delay) && delay > 0 ? delay : undefined
  }
  const delay = Date.parse(value) - Date.now()
  return Number.isFinite(delay) && delay > 0 ? delay : undefined
}
```

接受**秒**（整数）和 **HTTP-date** 两种格式。429 → `RATE_LIMIT`（`llm-deepseek/src/adapter.ts:142`），`Retry-After` → `LlmError.providerRetryAfterMs`（`llm-deepseek/src/adapter.ts:332-338`），最终被 `llm-retry` 在 `localDelay` 前优先消费（`llm-retry/src/index.ts:58-63`）。**没有 token-bucket 或 remaining-quota 本地状态** —— retry 完全是事件驱动的。

### 6.8 背压

`BlockAssembler` 没有显式 backpressure（消费者不消费 = adapter fetch 不暂停），但 `using` + 异常清理 + 5min `idleWatchdog` 提供**间接背压**：

- 消费者停止迭代 → 流式 fetch 仍在进行 → `idleWatchdog` 看到无新字节超时 → 抛 `TIMEOUT`。
- 整个 `agent-loop` 上限 `maxParallelToolCalls`（`agent-loop/src/index.ts:251`），是"显式节流阀"。

---

## 7. 子代理上下文隔离

### 7.1 模块职责

| 文件 | 行数 | 职责 |
|---|---|---|
| `packages/subagent/subagent/src/child-agent.ts` | 237 | 4 维隔离的"in-process child"组合 |
| `packages/subagent/subagent/src/descriptor.ts` | 314 | 持久化 `subagent/descriptor` 事件，versioned 可冷启动恢复 |
| `packages/subagent/subagent-fork-in-process/src/index.ts` | 99 | "fork" 模式：child 继承 parent 已完成 turn 的 prefix |
| `packages/subagent/tool-subagent/src/index.ts` | 467 | `delegate` 工具的 schema、toolFilter、上下文传递 |

### 7.2 4 维隔离

`child-agent.ts:135-175` 集中了所有隔离机制的实现：

```typescript
export const SUBAGENT_DELEGATION_CONTEXT
  = 'You are a delegated subagent: your permission scope was fixed when you were started and cannot be '
    + 'widened from inside this session — operations that require approval are rejected automatically. '
    + 'When the task needs access beyond that scope, do not retry the denied operation; state the '
    + 'limitation in your reply so the delegating agent can handle it.'

export function applyChildComposition(childCtx, parent, composition) {
  childCtx.get('agentPresets')?.composeFrom(childCtx, parent.ctx)        // 1. preset join
  childCtx.systemPrompt.context({                                       // 2. delegation context
    name: 'subagent:delegation', order: 120, text: SUBAGENT_DELEGATION_CONTEXT,
  })
  if (composition.persona !== undefined) {                              // 3. per-child persona
    childCtx.systemPrompt.section({ name: 'deployment:persona', order: 0, text: composition.persona })
  }
  if (composition.toolFilter !== undefined) childCtx.tools.restrict(composition.toolFilter)  // 4. tool restrict
}
```

| 隔离维度 | 实现 | 文件:行 |
|---|---|---|
| **Session 隔离** | 独立 `SessionId` + `parentSession` lineage 字段 | `child-agent.ts:107-119` |
| **Scope 隔离** | 自己的 agent 平面（`childCtx = scope.ctx.extend({ agent: this })`） | `agent-loop/src/agent.ts:94-95` |
| **Permission 隔离** | `toolFilter.allow/deny`（schema 过滤） + `approvalPolicy: 'never'`（审批全拒） | `child-agent.ts:215-225` |
| **Context 隔离** | `subagent:delegation` runtime-context 注入（order 120） | `child-agent.ts:170` |

### 7.3 上下文继承的"fork vs spawn"

#### Fork 模式（`subagent-fork-in-process/src/index.ts`）

```typescript
const completedTurnPrefix = (parent) => {
  // ...遍历 session.events，取最后 turn/end 之前的所有事件作为 seed
}
return { seed: completedTurnPrefix(parent) }
```

- **继承父级**已完成的 turn prefix（包括父级所有 system prompt 渲染、user/assistant 消息、tool results）。
- 适用场景：父级已经探索了 N 轮，子级从某个 checkpoint 继续。
- 注释明确 fork 只能 one-shot（`fork-in-process/src/index.ts:78-83`）—— continuable child 必须用更精确的 cold-resume。

#### Spawn 模式（默认 `subagent-spawn-in-process`）

- 子**不继承**任何父级 messages。
- 仅继承 `subagent:delegation` 上下文 + toolFilter + persona。
- 适用场景：父级把任务扔给子级，子级从零开始。

### 7.4 子级消息的反投影 —— 选定 assistant 输出 + `tool-subagent-report` relay

**两条独立路径**回父级：

| 路径 | 触发 | 选择规则 | 协议层隔离 |
|---|---|---|---|
| A: one-shot `tool-subagent` 结果 | one-shot 完成后 | `finalAssistantOutput(events)` —— 最后一条非空 assistant message 的 blocks；无则用 accumulated streamed text fallback | 仅 `output: ContentBlock[]` 入父工具结果 |
| B: `tool-subagent-report` | continuable 子级调 `report(args)` | `args.output` 即一切 | `source.form: 'relay'` 标记的合成 user message，**永不**带 transcript / tool raw output / reasoning |

**关键代码**：`deliverReport`（`subagent/src/continuation.ts:583-653`）构造合成 user message 注入父 inbox：

```typescript
const message = createUserMessage({
  content: [
    { type: 'text' as const, text: `Background subagent ${activation.childId} reported:` },
    ...content,  // 子级 args.output 的 blocks
  ],
  source: {
    kind: 'subagent-report' as const,
    form: 'relay' as const,            // §2.2 的 6 类 ContextForm 之一，UI 可据此渲染
    senderSessionId: activation.childId,
  },
})
if (delivery === 'wakeup') this.sendWaking(parent, message, () => this.sendReport(parent, message, delivery))
else this.sendReport(parent, message, delivery)
```

**关键不变量**：

- 父级**仅**收到子级 `report` 工具调用时显式提供的 `output` 文本 —— **永不**包含子级 transcript、tool 调用的 raw 输出、reasoning 链。
- `source.form: 'relay'` 让 6 类 `ContextForm` 中 `'relay'` 派上用场，UI 可据此把"来自子代理的报告"渲染为非默认样式。
- `senderSessionId` 留作审计 + 引用跟踪。
- `delivery: 'wakeup'` 时父级被显式唤醒；否则只是把 message 投递到父 inbox。
- `withPartialText` 保留非 `completed` stopReason 的部分回答（`tool-subagent/src/index.ts:149-155`）—— 子级"心口不一"（`heart_thought`，对应 §119）**永不入**这条 `report` 消息。

### 7.5 冷启动恢复：descriptor 持久化（`descriptor.ts`）

`subagent/descriptor` 事件是一次性写入的：

```typescript
declare module '@deepseek-ai/dsh-session/types' {
  interface SessionEventMap {
    'subagent/descriptor': SubagentDescriptorData  // 不入 surface，不入 model history
  }
}
```

`SubagentDescriptorBase` 强制 `version: number`（`descriptor.ts:53`），加载时 `version !== SUBAGENT_DESCRIPTOR_VERSION` 返回 `undefined`（`descriptor.ts:204`）—— 跨版本冷启动优雅退化。

**关键不变量**：

- descriptor 是 "model-hidden" —— 不带 `surfaceOp`，**不入 surface**、**不入 model history**（`descriptor.ts:36-39`）。
- descriptor **只写一次**：`foldSubagentDescriptor` 取第一个匹配事件（`descriptor.ts:308-313`），后续同型事件不能改写身份。
- descriptor 是 完整 schema：`assertKnownKeys` 拒绝未知字段（`descriptor.ts:148-152`）—— 防 "增加字段忘了写 schema 校验"。

### 7.6 子代理深度与递归

`child-agent.ts:30-57` 的 `resolveChildDepth` + `delegationDepthOf(parent) + 1`：

- 深度通过 `parentSession` 链递归计算。
- `maxDepth` 可选上限 → 抛 `SubagentDepthError`。
- **深度信息持久化**在 `meta.delegationDepth`（`child-agent.ts:117`），冷启动后仍是 monotone floor。

### 7.7 委托策略（policy seed）

`captureDelegatedPolicyOverrides(parent)`（`child-agent.ts:199-204`）：

```typescript
return {
  sandboxMode: parent.ctx.get('sandboxPolicy')?.overrideOf(parent.session),
  approvalPolicy: parent.ctx.get('approval') === undefined ? undefined : 'never',  // ← 强制 never
}
```

子级的 approval **永远**是 `'never'`（父级也救不了）—— 这是 "delegated 任务不该打扰用户审批" 的硬约束。

`appendDelegatedPolicyOverrides(childSession, overrides)` 在 child 创建窗口内**先** append（`child-agent.ts:215-225`），且 "appends land after any fork seed, so fresh policy wins stale seed state"（注释明确）—— 这是 §2.7 "后写覆盖" 的延伸：政策晚于历史。

### 7.8 共享 in-process driver（`subagent-in-process-driver/src/index.ts:111-148`）

fork 和 spawn 共享同一份 child 工厂（`startInProcessRun`），差异**仅**在 seed：

```typescript
// subagent-in-process-driver/src/index.ts:111-148
const childId = SessionId(randomUUID())
const seed = options.seed
const activationBoundary = seed?.length ?? 0
// Capture before the first await: a later parent switch belongs to the parent's future.
const inherited = captureDelegatedPolicyOverrides(parent)
const handle = await parent.ctx.agents.create({
  sessionId: childId,
  meta: childSessionMeta(parent, childDepth, activationBoundary),  // 写 activationBoundary
  ...seed !== undefined ? { seed } : {},
  agentOptions: resolveChildAgentOptions(parent, request.agentOptions, childDepth),
  signal: request.signal,
  setup,
})
```

`activationBoundary`（=`seed.length`）记入 child 的 `meta.seedLength`，fold 时：

```typescript
// subagent/src/continuation.ts:902-905
// Fold only the child's own suffix: a fork seed replays the parent's log,
// so a resumable cold load skips the seed prefix when reading descriptor.
const descriptor = foldSubagentDescriptor(loaded.events.slice(loaded.meta.seedLength ?? 0))
```

—— 子级 fold descriptor 时**只读自己后写的事件**，fork seed 里的父级事件被识别为"非子级原创"。

`attachDescriptorAppend`（`index.ts:79-89`）在子 agent 的**第一个 pre-step** append `subagent/descriptor` 事件：

```typescript
function attachDescriptorAppend(childCtx, descriptor) {
  let appended = false
  childCtx.on('agent/pre-step', async ({ agent }, next) => {
    const decision = await next()
    if (!appended && decision.kind === 'enter') {
      appended = true
      agent.session.append('subagent/descriptor', descriptor)
    }
    return decision
  })
}
```

—— descriptor 进入"first turn 之内、first step 之内"的窗口，与 §6.4 `compaction/start` 同样的"lock before first await"。

### 7.9 Subagent Invariants —— 唯一非空 companion 是 `subagent` 核心

| 文件 | 是否真校验 | 关键断言 |
|---|---|---|
| `subagent/src/invariant.ts` | ✅ **真校验** | provider-registry + start/end 配对 |
| `subagent-fork-in-process/src/invariant.ts` | ❌ `install = () => {}` | 包所有权占位 |
| `subagent-spawn-in-process/src/invariant.ts` | ❌ | 同上 |
| `subagent-in-process-driver/src/invariant.ts` | ❌ | 同上 |
| `tool-subagent/src/invariant.ts` | ❌ | 同上 |
| `tool-subagent-report/src/invariant.ts` | ❌ | 同上 |

`subagent/src/invariant.ts:14-84` 的真校验通过 `internal/dispatch` 拦截（`global: true`），先用 `WeakSet` 暂存 mutations、待正式 `ctx.on(...)` 提交，避免校验时自身的 emit 死循环：

```typescript
const install: InvariantInstaller = Object.assign((ctx, fail) => {
  const providers = new Set(ctx.subagents.list())
  const runs = new Map<string, SubagentRunInfo>()
  const stagedProviders = new WeakSet<SubagentProvider>()
  const stagedRemovals = new Set<string>()
  const stagedStarts = new WeakSet<SubagentRunInfo>()
  const stagedEnds = new WeakSet<SubagentRunEndInfo>()

  ctx.on('internal/dispatch', (_mode, eventName, args) => {
    if (eventName === 'subagent/provider-added') {
      if ((args[0] as SubagentProvider).name.length === 0) fail('subagent provider names must be non-empty')
      if (providers.has((args[0] as SubagentProvider).name)) fail(`subagent/provider-added repeated ${...}`)
      stagedProviders.add(args[0])
      return
    }
    // ... provider-removed / start / end 同样 staging
  }, { global: true })
  // ... regular ctx.on 提交
})
```

**5 类 fail 字符串**：

- `subagent provider names must be non-empty`
- `subagent/provider-added repeated ...`
- `subagent/provider-removed names unknown provider ...`
- `subagent/start provider, runId, and child id must be non-empty`
- `subagent/start repeated run id ...`
- `subagent/end has no matching subagent/start for run ...`
- `subagent/end identity diverges from subagent/start for run ...`（验证 `provider`/`id`/`local` 三元组相等）

**`validateRunEnd(start, end, fail)`**（`subagent/src/invariant.ts:15-19`）要求 start/end **完全一致**的 `provider`/`id`/`local` —— 防 cold resume 时把"fork child 的 fork 父"误接为另一 provider 的 start。

---

## 8. 关键设计不变式清单

> 这是本分析最具迁移价值的部分。每条都给出文件:行出处。

### 8.1 消息身份（Identity）

| 不变式 | 出处 |
|---|---|
| 每条 `Message` 有 `MessageId = UUID`，跨 logs/delivery/request 边界稳定 | `message.ts:178-185` |
| 创建即 `deepFreeze`，迭代遍历（防 stack overflow），`AbortSignal` 显式跳过 | `call-config.ts:88-117` |
| Adapter 抛错不传播：必产出终结 `finish` 帧 (`{kind:'error'\|'aborted'}`) | `llm/src/index.ts:867-870, 931-939` |

### 8.2 流协议（Stream Protocol）

| 不变式 | 出处 |
|---|---|
| `usage` 帧至多一次 | `invariant.ts:70-72` |
| `block-end` 必须有 open block 配对，重复 close 静默忽略（stragglers） | `invariant.ts:60-67`、`assembler.ts:77-82` |
| `finish` 后不再有帧（除 `error`/`aborted`） | `invariant.ts:43-44, 74-79` |
| `max-tokens` 时丢弃未完成 `tool-call` 块（防孤儿） | `assembler.ts:136-138` |

### 8.3 请求重建（Reconstructability）

| 不变式 | 出处 |
|---|---|
| 每条 LLM 请求的 `messages` 字段 = `session.deriveMessages()` | `agent-loop/src/invariant.ts:40-42` |
| `request/header` 当且仅当与 folded 不等时才写新事件 | `agent.ts:465-470` |
| `provider/model/reasoningEffort/temperature/maxTokens/stop` 任一变化都触发 header 重写 | `call-config.ts:23-30, 49-59` |
| 请求对象本身必须 `Object.isFrozen`（loop-built 标记） | `agent-loop/src/invariant.ts:23-25`、`call-config.ts:13, 66-78` |

### 8.4 上下文组装（Context Assembly）

| 不变式 | 出处 |
|---|---|
| 多个 `complete: true` section 同时存在 → `assemble` 抛错 | `system-prompt/src/index.ts:505-508` |
| `{{variable}}` 严格插值；未知 / 未赋值 / 形如 `{{{name}}}` 都 throw | `system-prompt/src/index.ts:258-295` |
| `Object.hasOwn` 替代 `in` 防原型链名字（`{{constructor}}`） | `system-prompt/src/index.ts:283-286` |
| Dynamic context 是 user-role message 而非 system section（每次重发都加一条） | `runtime-context.ts:64-74` |
| Dynamic context 走 `RuntimeContextProjection.project()` 去重，内容未变不发 | `runtime-context.ts:64-67` |
| 替换上游 surface 事件 → 保留快照自动失效 | `runtime-context.ts:46-55` |
| `toolOrder` 必须含 `<unlisted-tools>` 哨兵恰好 1 次 | `system-prompt/src/index.ts:140, 153-156` |

### 8.5 Token 计量（Token Metering）

| 不变式 | 出处 |
|---|---|
| Provider 报 `prompt_tokens` 含 cache 命中 → 必须 disjoint 拆出 `cacheReadTokens` | `llm-deepseek/translate.ts:53-62` |
| `usage.inputTokens` / `outputTokens` / `cacheRead` / `cacheWrite` disjoint 计数 | `types.ts:128-141` |
| `projectedTokens = pressure + surfaceDelta`（不是只看 latest usage） | `usage-projection.ts:198-205` |
| `retainTokens < thresholdTokens`（压缩保留量不能比阈值还大） | `compaction-basic/src/config.ts:148-154` |
| 文本估算用 `Math.ceil(text.length / 4)`，故意低估 CJK（容错优于精确） | `token-meter/estimate.ts:13-32` |
| `image` 块走 JSON 字符串估算，故意高估（无真实字节大小） | `estimate.ts:45` |

### 8.6 压缩（Compaction）

| 不变式 | 出处 |
|---|---|
| 双触发：pre-step 主动 (`pressure`) + `agent/request-error` 兜底 (`context-overflow`) | `compaction-basic/src/index.ts:147-223` |
| `overflowRetries` 在每次 `assistant/message` 成功时清空（响应成功即重置） | `compaction-basic/src/index.ts:167-177` |
| 摘要调用复用 system+tools+messages prefix，provider KV 缓存不失效 | `compaction-basic/src/index.ts:227-235` |
| Tool-result 剪枝插 `PRUNE_MARKER` 占位（永不静默删） | `compaction-tool-result-pruner/src/config.ts:7` |
| 剪枝按 code point 切（防 surrogate pair 断裂，grapheme cluster 仍可能断） | `compaction-tool-result-pruner/src/index.ts:78-118` |
| 剪枝只动 `tool_result.text` 内部，保留 `toolCallId` / `isError` | `compaction-tool-result-pruner/src/index.ts:80-90` |
| Compaction 失败后 durable surface 已前进 → 仍发 retry | `compaction-basic/src/index.ts:201-207` |

### 8.7 重试（Retry）

| 不变式 | 出处 |
|---|---|
| `INVALID_CREDENTIAL` 默认不在白名单（永久错误不浪费重试） | `retry-policy.ts:14-24` |
| `EMPTY_RESPONSE_CODE` 在白名单（"成功但 0 块" 等价"未发生"） | `retry-policy.ts:18, 39` + `error.ts:39-48` |
| 重试挂在 `agent/request-error` 而非 `llm/stream`（流帧已落日志） | `llm-retry/src/index.ts:177` |
| 退避：`exponential * (1-r+2r*rand)` 对称 jitter | `llm-retry/src/index.ts:101` |
| Abort 后 `cancellableDelay` 立即 resolve(false)，不耗尽 timer | `llm-retry/src/index.ts:108-117` |

### 8.8 子代理（Subagent）

| 不变式 | 出处 |
|---|---|
| 子级 `approvalPolicy` 永远 `'never'`，父级不可救 | `child-agent.ts:199-204` |
| 子级 `toolFilter` 来自父级调用时声明，运行时不可改宽 | `child-agent.ts:174` |
| `subagent/descriptor` 事件只写一次（首次为准），无 `surfaceOp` | `descriptor.ts:36-39, 308-313` |
| `descriptor.version !== SUBAGENT_DESCRIPTOR_VERSION` → cold-resume 优雅降级为 `undefined` | `descriptor.ts:204` |
| Delegation policy seed 在 fork seed 之后写（晚写覆盖早写） | `child-agent.ts:215-225` 注释 |
| 子级消息通过 report tool 回流父级，不直接 push（控制父级上下文增长） | `tool-subagent/src/index.ts:467` |

### 8.9 Abort 与生命周期

| 不变式 | 出处 |
|---|---|
| 3 个 abort 源（caller / owner / factory）fuse 成一个 controller | `agent-loop/src/index.ts:479-487` |
| 任何 abort reason 都包成 Error（防 `signal.reason: unknown` 类型失配） | `agent-loop/src/index.ts:481-486` |
| `deepFreeze` 显式跳过 `AbortSignal`（不能冻结 live channel） | `call-config.ts:104` |
| idle watchdog 用 `using` 语法（异常路径也清理 timer） | `llm-deepseek/src/adapter.ts:227` |

### 8.10 不变量自检（Self-Inspection）

DSH 用 **invariant companion 模式** 持续校验：每个关键包提供 `invariant.ts`：

- `packages/llm/llm/src/invariant.ts` —— 流协议语法
- `packages/compaction/compaction-basic/src/invariant.ts` —— compaction 边界
- `packages/compaction/compaction-tool-result-pruner/src/invariant.ts` —— 剪枝正确性
- `packages/core/agent-loop/src/invariant.ts` —— 请求重建一致性
- `packages/context/*/src/invariant.ts` × 4 —— 各自上下文完整性
- `packages/subagent/*/src/invariant.ts` × 多 —— 子代理协议

每个 invariant 注册时声明 `inject: [...]` 服务依赖（`llm/invariant.ts:12`），由 `dsh-invariants` 服务在生产中保留 + 持续校验。**"声明了但未接线"的缺陷无法躲过 invariant companion 校验**。

---

## 9. 对「多 Agent 博弈类游戏（狼人杀 7 bot）」的可借鉴点

> LsmAgentGame 当前 7 bot 房间共享一份系统 prompt + 各自独立 LLM 会话（参考 §15 §128 §130 §20260814-02）。DSH 给出 7 条**高 ROI**的可借鉴经验。

### 9.1 可借鉴 1：动态 Context 走 user message 而非 system section

DSH 模式：dynamic context = user-role `Message`（带 `source.form: 'snapshot'`）→ 自动去重（`runtime-context.ts:64-74`），不污染 system prompt。

**狼人杀改造**：

- 现在的 GameContext（`ServerGo/game/werewolf/agent_context.go`）把所有 stage 信息塞进 user prompt 尾部（如 §20260812-04 的 `NightPrivateInfoBlock`）。
- 借鉴 DSH：用 `session-projection` 把 stage info 落到 user message 而非 system，stage 切换时**只在变更处插入**新 message，replay 自然重建。
- 收益：system prompt 永远只装"游戏身份"（一次性写入），变化信息随对话流动 → §128 对话即思考。

### 9.2 可借鉴 2：每条请求 = session log 的纯函数（reconstructability）

DSH invariant：`JSON.stringify(options.messages) === JSON.stringify(session.deriveMessages())`（`agent-loop/src/invariant.ts:40-42`）。

**狼人杀改造**：

- 当前 LLM 调用在 `agent/wwplayer/agent.go` 内自由构造 `request.Messages`，无 reconstructability 校验。
- 加一个 invariant 校验函数：每次发起 LLM 请求前用 `tools.go::BuildAllMessagesFromLog(agentSession)` 重建，diff 一致才发。
- 收益：第 130 节 / §20260813-04 §20260814-02 中"声明了却从不接线"的缺陷能被 invariant 立刻咬出（参考 DSH §8.10 invariant companion 模式）。

### 9.3 可借鉴 3：AbortSignal 三源 fuse + Error reason 包装

DSH 模式（`agent-loop/src/index.ts:479-487`）：caller / owner / factory 三个 abort 源 fuse 为一个 `AbortController`，每源 reason 都包成 Error。

**狼人杀改造**：

- 当前 `werewolfRoom` 的 `r.mu` 持锁路径调 watchdog、agentCancels 释放（CLAUDE.md §92a），已经做了一半。
- 借鉴 DSH：
  - 三源（user cancel、room force-close、stage watchdog）fuse 成一个 controller。
  - `defer cancel(cancelCause.New("reason"))` 而非 `defer cancel()`。
- 收益：§20260814-02 §130 的"abort reason 丢失"问题在源头被消灭。

### 9.4 可借鉴 4：Provider cache 不下断点，依赖"前 N 轮字节稳定"

DSH 模式：DeepSeek 是自动 server-side KV cache（`llm-deepseek/translate.ts:53-62` 只对账 disjoint usage），Harness 不下 `cache_control`；唯一要求是 "request/header 折叠保证前 N 轮 request bytes 稳定"（`agent.ts:465-470`）。

**狼人杀改造**：

- 当前 §14.1 的 `CacheControl` 字段"声明了却从未赋值"是 §130 复发（§20260814-02 U3）。
- 借鉴 DSH：
  - 不再尝试"主动下断点"，承认 Anthropic 自动断点行为（`§14.1 cache_control` 实战中的 4-breakpoint 调度是 OpenCode 启发）。
  - 改为：每局/每轮开始时把 system prompt + 工具 schema + 玩家身份列表**深 freeze**，确保跨轮字节稳定。
  - 落地：把 `agent.go` 的 system prompt 字符串提前算好（不在 hot-path 拼），CWD/Persona 提前入变量（`systemPrompt.variable`）。
- 收益：cache hit 60-70% → 80%+，7 bot 房间成本下降 30%。

### 9.5 可借鉴 5：Pre-step 主动压缩 + request-error 兜底（双触发）

DSH 模式（`compaction-basic/src/index.ts:147-223`）：
- 80% `thresholdRatio` 主动压缩。
- `CONTEXT_WINDOW_EXCEEDED_CODE` 兜底（强制压缩，最多 `maxOverflowRetries` 次）。
- `overflowRetries` 在每次 `assistant/message` 成功时清空。

**狼人杀改造**：

- 当前 §20260813-04 借鉴 Hermes 加了 `PruneByBytesAggressive`（**仅** post-error 400 触发）。
- 借鉴 DSH 加 pre-step 主动压缩：
  - 仿 `agent/pre-step` listener，每 step 入口算 `approxPayloadBytes`。
  - 超阈值（建议 80% `contextWindow`）先跑 `toolResultPruner` 剪（model-free），再 LLM 摘要。
  - 收益：避免"等到 400 才压缩"的数分钟空等（§20260813-04 教训 6 提到的"低估 90×"问题）。

### 9.6 可借鉴 6：tool-result 剪枝永不静默删

DSH 模式（`compaction-tool-result-pruner/src/config.ts:7`）：剪枝必插 `PRUNE_MARKER` 占位 + `shadow-price` 事件 → token meter 可观测到"剪了多少"。

**狼人杀改造**：

- 当前 §137 道具系统剪枝走 `drainPropInjectQueueLocked`，无 marker。
- 借鉴 DSH：
  - 剪过的 `gc.PropInjectText` 末尾插 `[... 因上下文预算被压缩, 实际长度 5400B ...]` 标记。
  - `approxPayloadBytes` 必须计入 marker（避免 §20260813-04 教训 6 的"漏算 c.Content"重演）。
- 收益：bot 看到 marker 知道自己被"压过"，可调整策略（"这轮别说太多"）。

### 9.7 可借鉴 7：子代理上下文 = "context 消息 + scope 隔离 + permission 隔离" 三合一

DSH 模式（`child-agent.ts:135-175`）：`subagent:delegation` runtime-context（order 120）+ scoped `persona`（order 0）+ scoped `toolFilter`。

**狼人杀改造**：

- 狼人 Agent 内部"狼族"（faction === werewolf）的 whisper 不入 chat 表（§133 协议层隔离），当前是"tool 内部不写"—— 借鉴 DSH：
  - 改为：在 `faction="werewolf"` 的 `agentCtx` 上注册 `ctx.systemPrompt.context({ name: 'wolf:pack', order: 100, text: 实时 whisper })`。
  - 非狼 agent 的 scope 看不到这个 context（`systemPrompt.context` scoped to agent）。
- 收益：协议层隔离而非 UI 层隐藏，杜绝"误把 whisper 写入 broadcast"的可能。

### 9.8 整体启示

DSH 给出"Context 管理"的一种**工业级答案**：

1. **日志是 single source of truth**（不变量 §8.3）—— 7 bot 房间的"上帝视角"和"个人视角"差异由 projection 而非手工对齐。
2. **Provider 抽象 + 流协议不变量**（§2）—— 7 套模型各自有 quirks，但 `BlockAssembler` 把它们收敛为同一 wire。
3. **Context 是 4 维度**（系统段位 + 动态快照 + 工具 + 变量）而非单一字符串 —— 子代理隔离的"4 维"完全对仗。
4. **预算是 provider 锚点 + 表面增量**（§4.3）—— 不靠"全量 estimate"，靠"上次 reported 之后的 delta"。
5. **不变量自检作为持续护栏**（§8.10）—— 7 bot 房间最大的隐患"声明了却从不接线"是 lint 抓不全的，但 invariant companion 可以。

---

## 10. 总结

DeepSeek Harness 的 Context 管理是**多 Provider Agent**的成熟工程范本。核心思想可归纳为：

- **Provider 是黑盒**：所有 wire 适配收敛到 `StreamChunk` 6 类，词汇与协议统一由不变量守护。
- **日志是真相**：每条 LLM 请求是日志的纯函数，replay 即重建；任何 listener 想改 messages 必须改事件。
- **Context 是 4 维度**：系统段位 + 动态快照 + 工具 + 变量；dynamic context 走 user message 去重，不污染 system。
- **预算三明治**：provider 锚点 + 表面 delta（O(1) replay）+ 启发式估算（CJK 容错）+ 双阈值双触发。
- **隔离是 4 维**：session / scope / permission / context；子代理继承父级 preset 但不可改宽自己。

对 LsmAgentGame 狼人杀 7 bot 房间而言，最具迁移价值的不是任何单个 API，而是：

> **"让日志成为 single source of truth，让 invariant companion 持续守护"** —— 一旦这条原则确立，§130 反复复发的"声明了却从不接线"将无藏身之处。

---

## 附录：错误码表（部分）

| Code | 出处 | 含义 | 默认重试 |
|---|---|---|---|
| `EMPTY_RESPONSE` | `llm/error.ts:39` | 0 块响应 | ✅ |
| `RATE_LIMIT` | `retry-policy.ts:18` | 限流 | ✅ |
| `SERVER` | 同上 | 5xx | ✅ |
| `TIMEOUT` | 同上 | idle / total timeout | ✅ |
| `TRANSPORT` | 同上 | 网络断开 | ✅ |
| `ABORTED` | `llm/src/index.ts:931-939` | caller signal | ❌ |
| `CONTEXT_WINDOW_EXCEEDED` | `error.ts:25-86` | 5 种正则识别 | 仅在 overflow 路径 |
| `INVALID_CREDENTIAL` | `error.ts:48` | 凭证畸形 | ❌（不重试） |
| `NO_ADAPTER` | `index.ts:817-819` | 路由未注册 | ❌ |
| `UNSUPPORTED_REASONING_EFFORT` | `index.ts:748-763` | 模型不支持该 effort | ❌ |
| `MALFORMED_RESPONSE` | `llm-deepseek/translate.ts:124` | SSE JSON 解析失败 | ❌ |
| `STREAM_CLOSED` | `llm-deepseek/translate.ts:184` | SSE 缺 `[DONE]` | ❌ |
| `INVARIANT` | `llm/invariant.ts` | 流协议违规 | ❌ |

