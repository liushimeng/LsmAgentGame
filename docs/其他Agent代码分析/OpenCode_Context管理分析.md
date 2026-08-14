# OpenCode LLM Context 管理深度分析

> **分析对象**：`/usr/local/LsmGitOpenSource/opencode`
> **分析角度**：Agent 调用大模型 API 时的 Context 管理（system prompt 构造、messages 数组、token 预算、缓存策略、流式响应、provider 抽象层、重试/限流）。
> **框架**：代码路径 → 实现细节 → 设计动机 / 教训
> **作者注**：本报告基于 2026-08-14 阅读 OpenCode 主分支源码（TypeScript、Bun runtime、Effect-TS）。报告不含「建议本仓库狼人杀 Agent 学什么」 — 那部分见《狼人杀Agent_根据_OpenCode_优化和解决方案_20260814.md》。

---

## 0. 项目背景与代码规模

OpenCode 是一个 TypeScript (Bun runtime) 的开源 AI Agent CLI/IDE 插件。它把"调用任意 LLM + 任意工具"这条链路拆成了两层：

- **`packages/llm`** (约 7,500 行)：provider-neutral 的"协议无关"核心 —— schema、route、protocol、protocol-independent 抽象
- **`packages/opencode/src/session`** (约 8,100 行)：会话/循环/工具/重试/压缩 —— 实际驱动 LLM 的胶水层
- **共享底层**：`packages/core` 中的 `token.ts`、`model.ts`、`provider.ts`、`util/token.ts`

这种分层让"context 怎么构造"和"怎么用 HTTP 把它发出去"解耦得很彻底。

---

## 1. 核心文件职责与接口

### 1.1 `packages/llm` — 协议无关核心

| 文件 | 职责 |
|---|---|
| `schema/messages.ts` | `LLMRequest` / `Message` / `ToolDefinition` / `ToolChoice` / `SystemPart` 的 Effect Schema 定义；`Message.content` 是 `ContentPart` 标签联合（text / media / tool-call / tool-result / reasoning）；`LLMRequest.system` 是 `SystemPart[]`；每个 part 都可以带 `cache: CacheHint` |
| `schema/options.ts` | `Model`、`ModelLimits`（`context` / `output`）、`GenerationOptions`、`ProviderOptions`、`HttpOptions`、`CachePolicy`（"auto" / "none" / object）、`CacheHint`（ephemeral/persistent） |
| `schema/events.ts` | `LLMEvent` 标签联合：step-start / text-start-delta-end / reasoning-*-* / tool-input-start-delta-end / tool-call / tool-result / tool-error / step-finish / finish / provider-error；`Usage` 类（inputTokens / outputTokens / nonCachedInputTokens / cacheReadInputTokens / cacheWriteInputTokens / reasoningTokens / totalTokens） |
| `schema/errors.ts` | `LLMError` tagged error 体系：`InvalidRequest` / `NoRoute` / `Authentication` / `RateLimit` / `QuotaExceeded` / `ContentPolicy` / `ProviderInternal` / `Transport` / `InvalidProviderOutput` / `UnknownProvider` —— 每个都有 `retryable` getter |
| `cache-policy.ts` | 在编译请求时把 `CachePolicy` 翻译成 `CacheHint` 注入到 tool / system / message 末尾 |
| `provider-error.ts` | 26 条正则 `isContextOverflow()` 用于从错误体识别"上下文超限" |
| `tool.ts` | `Tool.make` / `toDefinitions` / `ToolSchema<T>` 类型化工具；带 `_definition` 缓存 + `_decode` / `_encode` / `_project` 内部接口 |
| `tool-runtime.ts` | `ToolRuntime.dispatch(tools, call)`：单次解码 + execute + encode + project，并发 `ToolFailure` → `LLMEvent.toolError` |
| `route/client.ts` | `LLMClient` Effect Service：`generate` / `stream` / `prepare`；`compile()` = `applyCachePolicy → body.from → validate body → prepareTransport`；`Route.make(...)` 把 `Protocol` + `Endpoint` + `Auth` + `Framing` 组合成可运行 route |
| `route/executor.ts` | `RequestExecutor`：HTTP transport + 重试 + 限流头解析 + 响应清理（敏感信息 redact + 16 KB body 截断） |
| `route/auth.ts` | `Auth` 抽象：`bearer` / `header` / `config` / `effect` / `orElse` / `andThen` —— 跟 Effect Config 集成 |
| `route/endpoint.ts` | 声明式 URL 构造（baseURL + path 函数 + query） |
| `route/framing.ts` | `Framing<Frame>` 接口：`sse`（JSON-SSE）/ `bedrock-event-stream`（binary frames） |
| `route/transport/http.ts` | `HttpTransport.httpJson` + `sseJson` —— 准备 + 解析字节流为 `Frame` |
| `route/transport/websocket.ts` | `WebSocketExecutor` + `WebSocketTransport.json` —— 用于 DWS workflow 等长连接 |
| `route/protocol.ts` | `Protocol<Body, Frame, Event, State>` 接口 + `Protocol.make` + `jsonEvent(schema)` |
| `protocols/shared.ts` | 跨 provider 共享的 lower 工具：`parseJson` / `parseToolInput` / `validateMedia` / `wrapSystemUpdate` (XSS-safe) |
| `protocols/anthropic-messages.ts` | Anthropic Messages API 完整实现：4 个 cache breakpoint 限额、`ANTHROPIC_BREAKPOINT_CAP` 守护、native system update (Opus 4.8 only) |
| `protocols/openai-chat.ts` / `openai-responses.ts` | OpenAI Chat Completions + Responses 协议 |
| `protocols/gemini.ts` | Gemini generateContent + CachedContent |
| `protocols/bedrock-converse.ts` / `bedrock-event-stream.ts` | AWS Bedrock Converse API + binary event stream framing |
| `providers/*.ts` | 工厂：`Anthropic` / `AmazonBedrock` / `Azure` / `Cloudflare` / `GitHubCopilot` / `Google` / `OpenAI` / `OpenAICompatible` / `OpenRouter` / `XAI` —— 每个都是 `Route.make` + `route.model(id)` 工厂 |

### 1.2 `packages/opencode/src/session` — Agent 循环核心

| 文件 | 职责 |
|---|---|
| `prompt.ts` | **主循环** `runLoop`：创建 user message → 解析 system / skills / env / mcp / instructions → 序列化 messages → LLM 调用 → 工具 dispatch → 退出判定 |
| `system.ts` | `SystemPrompt` Service：`environment(model)` / `skills(agent)` / `mcp(agent, perms)` |
| `llm.ts` | LLM runtime 选择层：默认走 `ai-sdk` (`streamText` + `wrapLanguageModel`)，opt-in 走 `LLMNativeRuntime` (`@opencode-ai/llm` 原生) |
| `llm/request.ts` | `LLMRequestPrep.prepare`：把"prompt + agent + user.system"拼成一个 system string；触发 `chat.system.transform` / `chat.params` / `chat.headers` 插件钩子；处理 OAuth + workflow 模式 system-into-messages 的不同走法 |
| `llm/ai-sdk.ts` | `LLMAISDK.toLLMEvents`：把 AI SDK fullStream 事件流归一为 `LLMEvent` |
| `llm/native-runtime.ts` | `LLMNativeRuntime.stream` / `status`：原生 runtime 准入判定，委托给 `LLMClient` |
| `llm/native-request.ts` | `LLMNative.request`：把 session 数据降为 `LLMRequest` |
| `processor.ts` | `SessionProcessor`：单次 LLM turn 的事件流 → 持久化；doom-loop 检测（3 次同 tool+input → 弹权限询问）；`isOverflow` 后置检测 |
| `compaction.ts` | 上下文超限处理：`isOverflow` → `prune` → `process` → 写新 assistant message |
| `overflow.ts` | `usable(cfg, model)` = `context - max(20000, max_output_tokens) - reserved` |
| `retry.ts` | 5xx/429/网络错误退避 + 文本模式 retryable 匹配 + Go/免费层 upsell 转化 |
| `reminders.ts` | plan/build agent 切换的合成提示注入 |
| `tools.ts` | `SessionTools.resolve`：把 ToolRegistry + MCP 工具按 agent / 权限筛选并包装为 `ai.Tool` |
| `run-state.ts` | 单 session 互斥，确保不会并发 prompt |
| `revert.ts` | 撤销到 checkpoint 时清理孤儿 tool parts |
| `summary.ts` | 异步把 user message 内容写一个 low-cost summary |

---

## 2. 七大要素详细分析

### 2.1 System Prompt 构造

**代码路径**：`packages/opencode/src/session/llm/request.ts` (lines 56-78) → `packages/opencode/src/session/prompt.ts` (lines 1257-1271) → `packages/opencode/src/session/system.ts` → `packages/llm/src/cache-policy.ts`

**实现细节**：

System prompt 沿四层顺序拼接：

1. **`agent.prompt`**：每个 agent 自己的 custom system prompt
2. **`SystemPrompt.provider(model)`**：根据 model id 后缀路由到不同的 base prompt：
   - `claude-*` → `PROMPT_ANTHROPIC` (`prompt/anthropic.txt`)
   - `gpt-4*` / `o1` / `o3` → `PROMPT_BEAST`
   - `gpt-5-codex` → `PROMPT_CODEX`
   - `gpt-*` → `PROMPT_GPT`
   - `gemini-*` → `PROMPT_GEMINI`
   - `kimi-*` / `moonshot*` → `PROMPT_KIMI`
   - `trinity` → `PROMPT_TRINITY`
   - `muse-*` → `PROMPT_META`（替换 `{{MODEL_NAME}}`）
   - 其他 → `PROMPT_DEFAULT`
3. **运行时注入** (`SessionPrompt.runLoop` 中 `Effect.all` 并行取 5 个 system 块)：
   - `sys.environment(model)`：`<env>` 块含 cwd / worktree / is-git-repo / platform / date
   - `sys.skills(agent)`：verbose 格式的 skill 列表
   - `sys.mcp(agent, perms)`：被权限允许的 MCP server 的 instructions
   - `instruction.system()`：custom instructions（来自 `.opencode/AGENTS.md` 等）
4. **`user.system`**（per-user-message override）
5. **`STRUCTURED_OUTPUT_SYSTEM_PROMPT`**：仅当 `lastUser.format.type === "json_schema"` 时追加

最后 plugin 钩子 `experimental.chat.system.transform` 注入 plugin 改写。

最终在 `request.ts` 里 `.filter(Boolean).join("\n")` —— 注意：是**单字符串**而不是分块。`LLMRequest.system` 在 schema 里是 `SystemPart[]`，但 OpenCode 端是合并成一条 string 然后塞进 `LLMRequest.system = [SystemPart.make(text)]`（native runtime）或 `messages: [{ role: "system", content: text }]`（AI SDK runtime）。

**Token 预算分配**：
- 不在 system prompt 阶段做 token 预算 —— 只在 `usable(cfg, model)` 处用 `context - max(20000, max_output_tokens) - reserved` 留出整段 buffer
- `compaction.ts:PRUNE_PROTECT=40_000` / `PRUNE_MINIMUM=20_000` 决定 tool output 清理阈值
- `compaction.ts:MIN_PRESERVE_RECENT_TOKENS=2_000` / `MAX_PRESERVE_RECENT_TOKENS=15_000` 决定 tail 保留窗口

**Cache breakpoint 设计**：
- `CachePolicy` 默认 `"auto"`（`cache-policy.ts:resolve()`）
- 三个断点：`tools` 末尾、`system` 末尾、`messages` 最后一条 user message
- 算法通过 `markLastTool` / `markLastSystem` / `markMessageAt` 三个 helper 注入 `CacheHint`
- **重要约束**：`RESPECTS_INLINE_HINTS` 只包含 `anthropic-messages` 和 `bedrock-converse`（`cache-policy.ts:42`），其他 provider（OpenAI / Gemini）跳过 —— 因为它们走隐式 prefix caching

**Anthropic-specific 限额**：
- `ANTHROPIC_BREAKPOINT_CAP = 4`（`anthropic-messages.ts:238`）
- `cacheControl()` 用 `breakpoints: Cache.Breakpoints` 计数器，超过 4 的 marker 静默丢弃并 `Effect.logWarning`
- TTL 桶：`Cache.ttlBucket()` 决定 5m vs 1h ephemeral

**教训 / 设计动机**：
- "auto 三个断点"的 rationale 在 `cache-policy.ts:6-15` 注释里写明 —— provider invalidation 是 tools → system → messages 顺序，Anthropic/Bedrock 提供 20-block lookback，三个 trailing 断点足够覆盖"static prefix"
- 注释坦承"不精确但生产可用" —— Anthropic 5m-cache 写 1.25x 读 0.1x，单次重用即回本

### 2.2 Messages 上下文窗口管理

**代码路径**：`packages/opencode/src/session/prompt.ts:runLoop` → `MessageV2.filterCompactedEffect` → `MessageV2.toModelMessagesEffect` → `LLMRequestPrep.prepare` → AI SDK `wrapLanguageModel({ middleware: transformParams })`

**实现细节**：

**构造**：
- `MessageV2.toModelMessagesEffect(messages, model)` 把内部 `SessionV1.WithParts` 转为 AI SDK `ModelMessage[]`
- 中间步骤会：image normalization / MCP resource 拉取 / 文件读取（按 mime 走 `read` tool 的 path）/ tool attachment 解析
- 最终在 `request.ts:101-112`，system 块被插到 messages 头部（除非 OAuth/Workflow 模式把 system 塞进 provider options）

**滑动窗口 / Pruning**：
- 没有传统 sliding window。OpenCode 用 **pruning**（`SessionCompaction.prune`）：
  - 从会话末尾倒着扫描 assistant 消息
  - 对每个 `tool` part：估算 `Token.estimate(part.state.output)` 累计
  - 一旦累计超过 `PRUNE_PROTECT=40_000` 停止
  - 把更早的 tool parts 标 `time.compacted = Date.now()` —— 不是删除，是"被压缩"标记
  - 触发条件：`pruned > PRUNE_MINIMUM=20_000` 才执行
  - 保护工具：`PRUNE_PROTECTED_TOOLS = ["skill"]` 不被 prune

**Summarization**：
- `SessionCompaction.process`：当 `isOverflow({ tokens, model })` 触发，spawn 一个 `compaction` agent 跑一次 LLM
- 头/尾分割（`SessionCompaction.select`）：
  - 先按 user message 切 `Turn[]`
  - 取最后 `cfg.compaction?.tail_turns ?? all` 个 turn 作为候选
  - 从后往前装入 `preserveRecentBudget`（默认 `Math.min(15_000, max(2_000, usable * 0.25))`）
  - 单 turn 装不下时用 `splitTurn` 找 sub-slice
- 头部 `selected.head` 用 `serialize()` 拼成 `[User]: ... [Assistant]: ... [Assistant tool call]: ... [Tool result]: ...` 文本
- 喂给 compaction agent 的 prompt（`buildPrompt({ previousSummary, context: [conversation] })`），让它产出新 summary
- 新 summary 写入一条 `summary: true` 的 assistant message，并发 `SessionCompactionEvent.Compacted`

**Token 计数**：
- **客户端估算**：`@opencode-ai/core/util/token.ts:Token.estimate(s)` 是个简单字符启发式（4 字符 ≈ 1 token）
- **服务端精确**：每次 `step-finish` 把 provider 返回的 `usage` 累计到 `assistantMessage.tokens`（`{ input, output, reasoning, cache: { read, write } }`）
- **双源求和**：`overflow.ts:isOverflow` 取 `tokens.total || tokens.input + tokens.output + tokens.cache.read + tokens.cache.write`

**按 role 分桶 / 合并**：
- 不显式分桶，但 `LLMRequest.messages` 是 `Message[]`，每个 `Message.role ∈ { system, user, assistant, tool }`
- "merge 连续 user message" 在 Anthropic 路径里做（`anthropic-messages.ts:418-421`：如果上一条是 user，把 system update 的 text block 追加到上一个 user content 而不是新开一条）
- "merge 跨 session" 是不可能的 —— 每次 loop 都是新 `MessageV2.toModelMessagesEffect(msgs)`

### 2.3 Tools 工具定义

**代码路径**：`packages/opencode/src/session/tools.ts:SessionTools.resolve` → `packages/opencode/src/session/llm/request.ts:resolveTools` → `ToolRegistry.tools(...)` → `ai.tool({ description, inputSchema: jsonSchema(...) })` → LLM wire

**实现细节**：

**Schema 形态**：
- 内部 `Tool`（`packages/core/src/tool/tool.ts`）描述：`description` / `parameters` (Effect Schema) / `execute` / `metadata` / `id`
- 包装为 `ai.Tool`（Vercel AI SDK）：`tool({ description, inputSchema: jsonSchema(toJsonSchema(EffectSchema)), execute: async (args, options) => ... })`
- AI SDK `jsonSchema` helper 把 Effect Schema 转 JSON Schema 7
- `ProviderTransform.schema(model, toolJsonSchema)` 做 per-model 投影（gemini / moonshot 兼容）

**Token 节省**：
- `request.ts:184` 把 tools 按 key 排序（`toSorted(([a], [b]) => a.localeCompare(b))`）—— 保证 wire format 字节稳定，cache 命中率最大
- AI SDK 默认 `strict: true` (JSON Schema strict mode) —— OpenCode 对 `@ai-sdk/openai` / `@ai-sdk/azure` / `@ai-sdk/amazon-bedrock/mantle` 显式 `strict: false`（`request.ts:152-158`）
- 不传 description 重复 —— 每个 tool 一份 description

**描述合并 / 缓存策略**：
- Tools 整段作为 `tools` 字段，cache breakpoint 打在最后一个 tool 上（`cache-policy.ts:markLastTool`）
- 跳过策略：`RESPECTS_INLINE_HINTS` 只对 `anthropic-messages` / `bedrock-converse` 注入
- OpenAI 路径靠 `promptCacheKey`（`openai-options.ts`）做隐式 prefix caching

**GitHub Copilot 特殊处理**：
- `request.ts:159-175`：如果 history 中已有 tool call 但当前 turn 没工具，注入 `_noop` 占位 tool —— Copilot API 不允许 tools 字段为空 + messages 含 tool-call

**Permission-aware filtering**：
- `SessionTools.resolve` 内 `for (const item of registry.tools({ modelID, providerID, agent, permission }))` + `resolveTools` 用 `Permission.disabled(...)` 过滤
- 用户可在 `user.tools` map 显式开启/关闭单个 tool

**运行时 dispatch**：
- AI SDK 路径：AI SDK 自身执行 tool → 触发 `experimental_repairToolCall` fallback（`llm.ts:296-312`）
- Native runtime 路径：`ToolRuntime.dispatch(tools, call)`（`packages/llm/src/tool-runtime.ts`）

### 2.4 Token 计数与限制

**代码路径**：`packages/core/src/util/token.ts` → `packages/opencode/src/session/overflow.ts` → `packages/opencode/src/session/compaction.ts:isOverflow` → `processor.ts` (`step-finish` 后)

**实现细节**：

**模型 context window 查询**：
- `model.limit.context` 和 `model.limit.input`（`Provider.Model.limit`）由 `models-dev.ts` + `ProviderTransform` 填充
- `OUTPUT_TOKEN_MAX` 常量 + `ProviderTransform.maxOutputTokens(model, flags.outputTokenMax)` 处理 output 上限

**当前 prompt 占用**：
- `session.message.tokens.{input, output, reasoning, cache.{read, write}}` 是累计值（每次 step 累加）
- 单次 `step-finish` 后 `processor.ts:482` 调用 `isOverflow` 判定
- 简单累加：`input + output + cache.read + cache.write`（`overflow.ts:32`）

**usable() 算法**：
```
context = model.limit.context
reserved = cfg.compaction.reserved ?? min(20000, maxOutputTokens)
if model.limit.input: usable = max(0, model.limit.input - reserved)
else: usable = max(0, context - maxOutputTokens)
```
（`overflow.ts:10-20`）

**超限处理（fail-soft）**：
- `isOverflow` 返回 `true` → `processor` 标 `ctx.needsCompaction = true`
- 步骤结束后 `SessionCompaction.create()` 写一个 `compaction` 类型的 user message
- 下次 `runLoop` 触发 `compaction.process()` 跑 summary agent
- **注意**：是 fail-soft（自动压缩），不是 fail-fast。但 `compaction.auto === false` 或 summary 模式时直接 `ContextOverflowError` 抛出

**isContextOverflow 正则**（`provider-error.ts:1-32`）：
- 26 个错误识别 pattern：涵盖 Anthropic / OpenAI / Google / Mistral / Cohere / Azure / OpenRouter 等等
- 排除 pattern：`/rate limit/i` / `/too many requests/i` / `/throttling error|service unavailable/i` —— 限流不是 token 超限

### 2.5 流式响应处理

**代码路径**：`packages/llm/src/route/framing.ts:Framing.sse` → `protocols/anthropic-messages.ts:step`（state machine）→ `protocols/shared.ts:sseFraming` → `schema/events.ts:LLMResponse.reduce`

**SSE 解析**：
- `effect/unstable/encoding/Sse` 的 SSE channel decoder 在 `protocols/shared.ts:sseFraming` 包装
- 过滤 `[DONE]` keep-alive 和空行
- 每个 frame 是 `data: {...}` 的 JSON 字符串

**Event 类型**：
- `protocols/anthropic-messages.ts:AnthropicEvent` 用 `Schema.Struct({ type, index, message, content_block, delta, usage, error })`
- Provider state machine 把 wire event 翻译成 `LLMEvent`：
  - `message_start` → 抓 `usage`
  - `content_block_start(type: text)` → `text-start` + `Lifecycle.textDelta(...)` 预占位
  - `content_block_delta(type: text_delta)` → `text-delta`
  - `content_block_delta(type: input_json_delta)` → `tool-input-delta`（累积到 `ToolStream`）
  - `content_block_stop` → `tool-input-end` + 调 `ToolStream.finish` 解析 JSON
  - `message_delta` → `finish` + 合并 usage
  - `error` → `provider-error`

**tool_use 块的累积**：
- `ToolStream`（`protocols/utils/tool-stream.ts`）用 `Map<Index, ToolStream.State>` 累积
- `partial_json` 一段段 `appendExisting`，最终 `finish` 时 `JSON.parse`
- 如果解析失败，emit `tool-error` 事件并保留 `raw` 字符串

**event reduction**：
- `schema/events.ts:LLMResponse.reduce(state, event)` 是纯函数
- 维护 `textParts` / `reasoningParts` / `toolInputs` 三张增量表
- `LLMResponse.complete(state)` 仅在 `finishReason !== undefined` 时产出 `LLMResponse`
- 整条 stream fold 通过 `Stream.runFold(LLMResponse.empty, LLMResponse.reduce)`（`client.ts:384`）

**两个 runtime 的 event 流**：
- **AI SDK**：`ai-sdk.ts:toLLMEvents(state, event)` 把 `fullStream` 的 Vercel 事件归一到 `LLMEvent`
- **Native**：`LLMNative` 直接 emit `LLMEvent`，无转换

### 2.6 Provider 抽象层

**代码路径**：`packages/llm/src/route/{client, auth, endpoint, framing, protocol, transport}.ts` → `packages/llm/src/protocols/*.ts` → `packages/llm/src/providers/*.ts`

**核心抽象 —— 四轴正交**：

```
Protocol  = "什么 wire format"
Endpoint  = "URL 在哪"
Auth      = "怎么认证"
Framing   = "字节流怎么切"
```

加上 `Transport` (HTTP/WS) 和 `Headers` (per-deployment 固定头)，可以组装任意 provider。`Route.make(...)` 是 canonical constructor。

**Wire format 差异如何隐藏**：

所有协议都暴露相同的 `LLMEvent` 标签联合 —— 上层（`Processor`）只看 `LLMEvent`，不知道下面是 Anthropic 还是 Gemini。差异处理点：

- **System prompt 角色**：
  - Anthropic 用 `system` 顶层数组
  - OpenAI Chat 用 `messages[0].role: "system"`
  - OpenAI Responses 用 `system` 顶层数组（但结构不同）
  - Gemini 用 `systemInstruction.parts[]`
  - 各 protocol 的 `body.from` 独立处理

- **Tool choice**：
  - Anthropic `{type: "auto" | "any" | "tool", name?}`
  - OpenAI `"auto" | "required" | "none" | {type: "function", function: {name}}`
  - 通过 `ProviderShared.matchToolChoice("Anthropic Messages", tc, { auto, none, required, tool })` 统一分桶（`anthropic-messages.ts:268`）

- **工具结果内容**：
  - Anthropic 接受 string | 数组（text/image blocks）
  - OpenAI 接受 string
  - 通过 `lowerToolResultContent` Effect 程序做格式协商

- **Token usage 字段归一**：
  - Anthropic 原生 breakdown（`input_tokens` 不含 cache），`mapUsage` 求和
  - OpenAI 原生 inclusive total，`subtractTokens` 拆出 cache
  - Gemini 同样
  - 都进入 `Usage.{inputTokens, nonCachedInputTokens, cacheReadInputTokens, cacheWriteInputTokens}` 四个独立字段

- **特殊 provider 行为**：
  - **OpenAI OAuth**：把 system prompt 塞进 `options.instructions` 字段，不走 `messages[0]`
  - **GitHub Copilot**：注册 `_noop` tool 当历史有 tool-call 但当前无 tool
  - **OpenCode Cloud (`@opencode-ai/*`)**：用 `x-opencode-{project,session,request,client}` headers
  - **Workflow (GitLab DWS)**：开 `wrapLanguageModel` 中间件
  - **Anthropic `claude-opus-4-8`**：唯一支持原生 system update (mid-conversation `role: "system"`)
  - **Anthropic**: 显式 `cache_control` 标记，cap = 4
  - **OpenAI**: `promptCacheKey` 隐式 caching
  - **Gemini**: `CachedContent` out-of-band 机制（无 inline 标记）

### 2.7 重试与限流

**代码路径**：
- HTTP 层：`packages/llm/src/route/executor.ts:retryStatusFailures`
- 应用层：`packages/opencode/src/session/retry.ts:policy`

**HTTP 层重试**：
- `retryableStatus(429, 503, 504, 529)` 重试
- `MAX_RETRIES = 2`（仅 2 次）
- 退避：`exponential(attempt) = base * 2^attempt * (0.8 ~ 1.2)`，cap 10s
- 优先用 provider 返回的 `Retry-After` / `Retry-After-Ms` header
- 重试条件：`error.retryable === true` —— 来自 `LLMErrorReason.retryable` getter：
  - `RateLimitReason` → true
  - `ProviderInternalReason` (5xx) → true
  - 其他（Auth / Quota / ContentPolicy / Transport）→ false

**限流头解析**（`executor.ts:rateLimitDetails`）：
- OpenAI 风格：`x-ratelimit-limit-{requests,tokens}` / `x-ratelimit-remaining-{requests,tokens}` / `x-ratelimit-reset-{requests,tokens}`
- Anthropic 风格：`anthropic-ratelimit-{requests,tokens}-{limit,remaining,reset}`
- 全部归一为 `HttpRateLimitDetails: { retryAfterMs, limit, remaining, reset }`

**应用层重试**（`retry.ts`）：
- `RETRY_MAX_RETRIES = 5`
- 22 条 `RETRYABLE_MESSAGE_PATTERNS`：覆盖 429/500/502/503/504/524、rate limit、overloaded、connection errors、timeout、resource_exhausted
- **不重试**：`SessionV1.ContextOverflowError`（避免无效循环）
- **退避**：`exponential(attempt) = 2_000 * 2^(attempt-1)`，加 ±25% jitter，cap 30s（无 header 时）
- **Go upsell 转化**：
  - 错误体含 `FreeUsageLimitError` → 推 `{reason: "free_tier_limit"}` 升级提示
  - 含 `GoUsageLimitError` → 解析 workspace / limitName / retryAfter，推 `account_rate_limit`
- `policy()` 返回 Effect `Schedule.fromStepWithMetadata`

**断路 / 超时**：
- 没有传统 circuit breaker
- 超时由 `Effect.timeout` / `AbortSignal` 处理（`prompt.ts:573` 等）
- `MAX_RETRIES = 2` 较保守 —— 防止把已经超限的请求反复打到 provider

**敏感数据 redact**：
- `executor.ts:redactBody` 两遍：结构性 redact（字段名匹配）+ 字面量 redact（实际 secret 值）
- `REDACTED = "<redacted>"`，body 截断到 16 KB
- 适配 OpenAI 风格 + Anthropic 风格 + AWS SigV4 + Bearer Token

---

## 3. 架构特点

### 3.1 整体架构图

```
┌──────────────────────────────────────────────────────────────────────┐
│                     packages/opencode (Session Loop)                 │
│                                                                      │
│  prompt.ts (runLoop)                                                 │
│    ├── system.ts (env / skills / mcp / instructions)                 │
│    ├── reminders.ts (plan/build switch)                              │
│    ├── tools.ts (SessionTools.resolve → ai.Tool)                     │
│    ├── MessageV2.toModelMessagesEffect                               │
│    ├── overflow.ts + compaction.ts (isOverflow / prune / summary)    │
│    └── processor.ts (events → DB persist + doom-loop guard)          │
│           │                                                          │
│           ▼                                                          │
│    llm.ts (StreamInput)                                              │
│      ├─ request.ts (LLMRequestPrep: system + tools + params + hdrs)  │
│      ├─ retry.ts (5 attempts, jitter, Go upsell)                     │
│      └─ ai-sdk.ts | native-runtime.ts ─────┐                         │
└────────────────────────────────────────────│─────────────────────────┘
                                             │
                  ┌──────────────────────────┴─────────────┐
                  │                                        │
                  ▼                                        ▼
      ┌────────────────────────┐         ┌─────────────────────────────┐
      │  AI SDK runtime        │         │  Native runtime             │
      │  Vercel AI SDK         │         │  @opencode-ai/llm           │
      │  - @ai-sdk/openai      │         │  (openai/opencode/anthropic) │
      │  - @ai-sdk/anthropic   │         │                             │
      │  - @ai-sdk/google      │         │                             │
      │  - @ai-sdk/bedrock     │         │                             │
      │  + GitLab workflow     │         │                             │
      └────────────────────────┘         └─────────────────────────────┘
                                                       │
                                                       ▼
              ┌─────────────────────────────────────────────────────────┐
              │           packages/llm (Provider-Neutral Core)         │
              │                                                          │
              │  llm.ts (request / generate / generateObject)            │
              │  schema/{messages, options, events, errors}.ts           │
              │  tool.ts (Tool.make / toDefinitions)                      │
              │  tool-runtime.ts (ToolRuntime.dispatch)                   │
              │  cache-policy.ts (auto-3-breakpoint placement)           │
              │  route/                                                 │
              │   ├─ client.ts (LLMClient Service)                       │
              │   ├─ auth.ts (Auth: bearer/header/config/effect)         │
              │   ├─ endpoint.ts (URL: path + baseURL + query)           │
              │   ├─ framing.ts (SSE / binary event stream)              │
              │   ├─ protocol.ts (Protocol<Body,Frame,Event,State>)      │
              │   ├─ transport/ (http + websocket)                       │
              │   └─ executor.ts (RequestExecutor: HTTP + retry + redact)│
              │  protocols/                                              │
              │   ├─ anthropic-messages.ts (cache 4-cap, native system)  │
              │   ├─ openai-chat.ts                                      │
              │   ├─ openai-responses.ts                                 │
              │   ├─ gemini.ts                                           │
              │   ├─ bedrock-converse.ts + bedrock-event-stream.ts        │
              │   └─ shared.ts (parseJson, validateMedia, ...)           │
              │  providers/ (Anthropic, OpenAI, Azure, Bedrock, etc.)    │
              └─────────────────────────────────────────────────────────┘
```

### 3.2 关键 interface 定义

```typescript
// packages/llm/src/route/client.ts
interface Interface {
  prepare<Body>(req: LLMRequest): Effect<PreparedRequestOf<Body>, LLMError>
  stream(req: LLMRequest): Stream<LLMEvent, LLMError>
  generate(req: LLMRequest): Effect<LLMResponse, LLMError>
}

interface Route<Body, Prepared> {
  id: string; provider?: ProviderID; protocol: ProtocolID
  endpoint: Endpoint<Body>
  auth: Auth
  transport: Transport<Body, Prepared, unknown>
  body: { schema: Schema.Codec<Body>; from: (req) => Effect<Body> }
  prepareTransport: (body, req) => Effect<Prepared>
  streamPrepared: (prep, req, runtime) => Stream<LLMEvent>
}

// packages/llm/src/route/protocol.ts
interface Protocol<Body, Frame, Event, State> {
  id: ProtocolID
  body: { schema: Schema.Codec<Body>; from: (req) => Effect<Body> }
  stream: {
    event: Schema.Codec<Event, Frame>
    initial: (req) => State
    step: (state, event) => Effect<[State, LLMEvent[]]>
    terminal?: (event) => boolean
    onHalt?: (state) => LLMEvent[]
  }
}

// packages/llm/src/schema/messages.ts
class LLMRequest {
  id?: string
  model: Model
  system: SystemPart[]
  messages: Message[]
  tools: ToolDefinition[]
  toolChoice?: ToolChoice
  generation?: GenerationOptions
  providerOptions?: ProviderOptions
  http?: HttpOptions
  responseFormat?: ResponseFormat
  cache?: CachePolicy
}
```

### 3.3 模块依赖关系

- **`packages/llm` 不依赖 `packages/opencode`** —— 它是纯 protocol-neutral 核心
- **`packages/opencode/session/llm`** 桥接 AI SDK + native runtime
- **`packages/core`** 提供底层数据 (token, schema, model, provider, database)
- **`packages/effect/*`** 共享 Effect-based services
- 跨包引用：`@opencode-ai/schema/llm` 是 zod-like schema 子集（被 `@opencode-ai/llm` 引用为 `Json` 等）

### 3.4 关键设计决策

1. **"不传递性"`CacheHint` 注入**：cache marker 写在 LLMRequest 的 part 上（`system[i].cache` / `messages[j].content[k].cache`），由 `cache-policy.ts` 在编译时自动填充，由各 protocol 在 `body.from` 阶段读出并翻译到 wire format。这样"是否 cache"、"cache 在哪"、"cache 什么"三个维度是解耦的。

2. **`Usage` 四个独立字段**：不依赖减法 `nonCached = total - cached`，避免 provider 异常值导致负数。`subtractTokens()` 用 `Math.max(0, ...)` 兜底。

3. **Schema 化、Effect 化全栈**：用 `Schema.Class` + Effect `Service` / `Layer` 表达 DI。

4. **"运行时分流"AI SDK / native**：默认走 AI SDK，opt-in 走 native。runtime 切换写 log 便于诊断。

5. **Overflow 处理是 fail-soft 自动压缩**：把"压缩"作为一条新 user message 注入会话，让 compaction agent 跑完后再继续原任务。如果 auto=false 才 fail-fast。

6. **协议不可知的事件流**：`LLMEvent` 是全系统中所有 LLM 实现的共同契约。

---

## 4. 关键代码路径速查

| 功能 | 文件 | 行 |
|---|---|---|
| System prompt 拼接 | `packages/opencode/src/session/llm/request.ts` | 56-78 |
| System prompt 块组装 | `packages/opencode/src/session/prompt.ts` | 1257-1271 |
| System prompt 模板选择 | `packages/opencode/src/session/system.ts` | 27-49 |
| Cache breakpoint 注入 | `packages/llm/src/cache-policy.ts` | 99-111 |
| Cache 限额 (Anthropic 4) | `packages/llm/src/protocols/anthropic-messages.ts` | 238, 514 |
| 上下文超限检测 | `packages/opencode/src/session/overflow.ts` | 22-34 |
| Compaction 算法 | `packages/opencode/src/session/compaction.ts` | 122-269 (select) / 273-317 (prune) / 319-557 (process) |
| Context overflow 错误识别 | `packages/llm/src/provider-error.ts` | 4-32 |
| Messages 序列化 | `packages/opencode/src/session/message-v2.ts` (toModelMessagesEffect) | — |
| Tool schema 投影 | `packages/llm/src/protocols/utils/tool-schema.ts` | — |
| Tool 排序稳定 cache 命中 | `packages/opencode/src/session/llm/request.ts` | 184 |
| Provider 路由 | `packages/llm/src/route/client.ts` | 247-298 |
| HTTP transport + 重试 + redact | `packages/llm/src/route/executor.ts` | 60-205 (redact) / 353-364 (retry) |
| 应用层重试 + Go upsell | `packages/opencode/src/session/retry.ts` | 33-156 |
| 流式事件归约 | `packages/llm/src/schema/events.ts` | 368-559 (reduce) / 561-617 (LLMResponse) |
| Doom-loop 检测 | `packages/opencode/src/session/processor.ts` | 358-380 |
| Plugin 钩子 | `experimental.chat.system.transform` / `chat.params` / `chat.headers` / `experimental.chat.messages.transform` | — |

---

## 5. 设计动机 / 教训小结

1. **分层清晰带来灵活性**：`@opencode-ai/llm` 是"协议无关核心"，`packages/opencode/session` 是"产品级编排"。这允许未来客户端 / TUI / IDE 插件共享同一 LLM 协议栈而不重复实现。

2. **Cache breakpoint 显式可控**：自动放 3 个断点（工具 / 系统 / 最后 user）但允许 object-form 完全自定义。OpenCode 选择把 `cache_control` 当作"协议层概念"而非"应用层概念"。

3. **Token 超限的 fail-soft 处理**：`isOverflow` 后 `needsCompaction` 标记 + 注入 `compaction` user message + 跑 summary agent。

4. **Usage 四字段独立**：避免减法引起的 underflow —— 这是 production-grade agent 在与不同 provider 打交道时最容易踩的坑之一。

5. **Provider 抽象的四轴正交**（Protocol / Endpoint / Auth / Framing）：让 DeepSeek / Together / Cerebras 这类 OpenAI 兼容 provider 不需要 fork 300 行。

6. **Redact 两遍**：结构化（按字段名）+ 字面量（按 secret 值），body 16 KB 截断 —— 防止日志泄露 token 也不会让单条错误消息无限大。

7. **AI SDK 与 native 双 runtime**：默认 AI SDK（成熟、覆盖广），native 作为 opt-in（更快、可控）。

8. **Doom-loop 防护**：连续 3 次相同 tool + 相同 input 触发"无限循环"权限弹窗 —— OpenCode 不会让模型静默死循环。

9. **Tool pruning 而非 session wiping**：默认 `PRUNE_MINIMUM=20_000`、`PRUNE_PROTECT=40_000` 表明它的策略是"渐进式清理旧 tool output 而不是删消息"。

10. **未实现的传统 sliding window**：OpenCode 选择 compaction + pruning 的组合而非简单的 last-N-messages。这对"长程任务"更友好。

整体看，OpenCode 的 LLM Context 管理是一份**生产级 LLM 客户端**的范例：协议分层、缓存策略显式、超限自救、限流退避、敏感信息隔离、运行时可观察性都做到了。
