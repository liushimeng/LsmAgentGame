# TencentDB-Agent-Memory — Agent 调用大模型 API 时的 Context 管理分析

> 分析对象：`/usr/local/LsmGitOpenSource/TencentDB-Agent-Memory`（Tencent 开源 Agent Memory Hub，
> TypeScript，约 166K LOC，4 模块：MemoryCore / MemoryKnowledge / MemoryPanel / MemoryProxy）
> 分析日期：2026-08-12 | 分析范围：仅「Agent 调 LLM 时的 context 组装 / 预算 / 压缩 / 拦截」这一条线
> 姊妹文档：[记忆管理分析](TencentDB-Agent-Memory_记忆管理分析.md) ·
> [意图识别与任务分解分析](TencentDB-Agent-Memory_意图识别与任务分解分析.md)

---

## 0. 最重要的前置结论：这个项目有**两套完全独立**的 Context 管理系统

读代码前必须先分清，否则所有细节都会串味。它们**不共享任何代码**，解决的也不是同一个问题：

| | **系统 A：Injection Pipeline** | **系统 B：Offload / Compaction** |
|---|---|---|
| 位置 | `MemoryProxy/src/injection/` | `MemoryCore/src/offload*/`（三个目录） |
| 解决 | 往 context **加**东西（记忆/skill/wiki） | 从 context **减**东西（长对话压缩） |
| 触发 | 每个 LLM 请求，在 proxy 里 | 客户端 ContextEngine，token 超阈值时 |
| Token 预算 | **完全没有**（零 token 计数代码） | 完整的 tiktoken + 4 级压缩阶梯 |
| 协议 | Anthropic / OpenAI 双 adapter | 消息数组（格式无关） |
| 部署 | MemoryProxy 服务 | MemoryCore 插件 + offload_server |

**关键**：这两套系统在生产里不必然同时启用。MemoryProxy 拦截 Claude Code 时走系统 A（无 token 预算）；
OpenClaw 插件走系统 B。所以「Token 预算管理」这个问题的答案是**分裂的**——见 §2。

---

## 1. Context 组装架构

### 1.1 系统 A：MemoryProxy 的 Hook Pipeline

核心类 `InjectionPipeline`，`MemoryProxy/src/injection/pipeline.ts:55`。一次请求的流程
（`pipeline.ts:80-147`）：

```
raw body → Adapter.parse() → AgentContext → executeHooks() → Adapter.serialize() → modified body
```

`AgentContext` 是协议无关中间表示（`injection/types.ts:134-146`）：
`messages[] / tools[] / requestParams / metadata`。

拼装顺序由**两个维度**决定 —— 这是本项目最值得学的设计。

**维度一：9 个注入点的固定执行顺序**（`pipeline.ts:165-175`）：

```ts
const executionOrder: InjectionPoint[] = [
  "system.prefix",
  "system.before_tools",
  "system.after_tools",
  "system.suffix",
  "tools.prepend",
  "tools.append",
  "user.first_turn",
  "user.before",
  "user.after",
];
```

**维度二：同一注入点内按 priority 排序**（`injection/types.ts:242-253`）：

```ts
export const HOOK_PRIORITY = {
  SYSTEM: 0,      // 系统级注入，最先
  MEMORY: 100,    // 记忆
  SKILL: 200,     // skill
  WIKI: 300,      // 知识库
  CUSTOM: 1000,   // 自定义，最后
} as const;
```

### 1.2 实际注册的「块」清单

注册逻辑在 `injection/index.ts:199-336` 的 `buildPipelineBundle`。**实际会进 context 的块只有 6 个**：

| 块 | Injector | 注入点 / anchor | priority | cacheStrategy | 文件 |
|---|---|---|---|---|---|
| `<available_skills>` | `SkillInjector` | `system.before_tools` / slot `skills` before | 200 | `session_init` | `injectors/skill-injector.ts:107` |
| `<skill_tools>` | `SkillToolsInjector` | system | — | `session_init` | `injectors/skill-tools-injector.ts:181` |
| `<knowledge_tools>` | `KnowledgeToolsInjector` | system | 300 | — | `injectors/knowledge-tools-injector.ts:164` |
| `<tdai_memory_tools>` | `TdaiMemoryToolsInjector` | `system.suffix` / slot `memory` before | 105 | `session_init` | `injectors/tdai-tools-injector.ts:141` |
| `<tdai_profile_memory>` + `<memory-tools-guide>` | `TdaiProfileMemoryInjector` | `system.suffix` / slot `memory` inside_append | 110 | `session_init` | `injectors/tdai-profile-memory-injector.ts:25` |
| `<asset_reflection>` | `AssetReflectionInjector` | `system.suffix` | 1000 | `none` | `injectors/asset-reflection-injector.ts:73` |

另有一条**不走 pipeline** 的旁路：`<session_context>`（Agent persona + Task 描述），
由 `session/context-injector.ts:106` 注入。它的注释明确说明了为什么绕开 pipeline
（`context-injector.ts:5-10`）：

> Agent/Task context is session identity, not optional enrichment, so it lives in the
> session module and bypasses that gating.

### 1.3 重大发现：召回记忆块已被**有意下线**

`TdaiL1RecallInjector`（`injectors/tdai-l1-recall-injector.ts:21`）实现完整——并发跨 agent 召回、
按 score 排序、取 top-K、渲染成 `<tdai_recalled_l1_memories>`——但 grep 全仓库只有 export，
**没有任何 register**。

而且这**不是遗漏，是有意为之**。`injection/index.ts:299-303` 写明：

```ts
// 注意：L0/L1 不再每轮自动召回注入到 user prompt（会破坏 KV/prompt cache）。
// 改为只在 system prompt 暴露只读工具（见 TdaiToolsInjector），借助 system
// prompt cache 复用。L1 recall injector 已下线，recallL1 配置保留但不再注册。
```

**这是全项目最重要的架构决策**：为了保住 Anthropic prompt cache，宁可放弃「每轮自动召回」，
改成「注入工具说明 + 让 LLM 自己 curl 检索」。代价是多一轮 tool call，收益是 system prompt 完全稳定。

### 1.4 语义槽（Semantic Slot）—— 跨 Agent 可移植的锚点

这是比「注入点」更高级的一层抽象。Hook 声明「我要落在**记忆区**之前」，而不是「我要落在第 N 个字符」：

`injection/types.ts:192-200`：

```ts
export type SemanticSlot =
  | "persona" | "tools" | "skills" | "memory"
  | "knowledge" | "rules" | "task_context"
  | (string & {});
```

每个 Agent 用自己的 `AgentProfile` 把语义槽翻译成具体结构键。Claude Code 的映射表
（`injection/agents/claude-code/index.ts:38-46`）——注意这是**对真实 CC system prompt 抓包**得到的：

```ts
const CLAUDE_CODE_SLOT_MAP: Record<string, string | null> = {
  persona: null,                       // 第一个 plain 段（# Harness 之前）
  tools: null,                         // CC 的 tools 在 request body，不在 system
  skills: "Session-specific guidance",
  memory: "Memory",                    // # Memory
  knowledge: null,
  rules: "Harness",                    // # Harness
  task_context: "Environment",         // # Environment
};
```

落地时 `applyInjection`（`pipeline.ts:340-381`）优先走 anchor，解析不到再**降级**回粗粒度 `point`：

```ts
if (key && segments.some((s) => s.key === key)) {
  const newSegments = profile.applyAnchor(segments, { key, relation: hook.anchor.relation }, text);
  sysMsg.blocks = [{ type: "text", content: profile.rebuild(newSegments) }];
  return; // anchor hit — done
}
console.warn(`[injection] anchor slot "..." unresolved on agent "${profile.id}", fallback to point "${point}"`);
```

Claude Code profile 的 parse→rebuild 有**无损往返**硬保证（`claude-code/index.ts:7-15`）：
不做任何 trim，segments 用 `"\n"` join 还原原文逐字节一致。这对 prompt cache 是必要条件。

### 1.5 系统 B：`auto-recall.ts` 的 stable / dynamic 二分

这才是真正做「召回记忆 → 拼 context」的地方（`MemoryCore/src/core/hooks/auto-recall.ts`，999 行）。
核心是 stable / dynamic 二分（`auto-recall.ts:258-290`）：

```ts
// Split recall context into stable and dynamic parts to optimize prompt caching.
//
// appendSystemContext (system prompt end — stable, cacheable):
//   persona, scene navigation, memory tools guide
//   These change infrequently; when content is identical across turns,
//   providers with prompt caching (Anthropic/OpenAI) can cache this region.
//
// prependContext (user prompt prefix — dynamic, per-turn):
//   L1 relevant memories — different every turn, moved out of system prompt
//   so it doesn't bust the system prompt cache.
const stableParts: string[] = [];
if (personaContent)  stableParts.push(`<user-persona>\n${personaContent}\n</user-persona>`);
if (sceneNavigation) stableParts.push(`<scene-navigation>\n${sceneNavigation}\n</scene-navigation>`);

let prependContext: string | undefined;
if (memoryLines.length > 0) {
  prependContext =
    `<relevant-memories>\n以下是当前对话召回的相关记忆，不代表当前任务进程，仅作为参考：\n\n${memoryLines.join(RECALL_LINE_SEPARATOR)}\n</relevant-memories>`;
}
```

注意 `<relevant-memories>` 里那句 **"不代表当前任务进程，仅作为参考"**——这是防止召回记忆被 LLM
误当成当前任务状态的 prompt 级护栏。

三路数据**并发**拉取（`openclaw-plugin/src/hooks/recall.ts:36-40`），任一失败不影响其他：

```ts
const [searchResult, persona, scenarios] = await Promise.allSettled([
  client.searchAtomic({ query: opts.query, limit: opts.maxResults }),
  opts.includePersona ? client.readCore() : Promise.resolve(null),
  opts.includeSceneNav ? client.listScenarios({}) : Promise.resolve(null),
]);
```

### 1.6 分层记忆的差异化注入策略（最实用的一条）

四层记忆**注入方式完全不同**（`tdai-profile-memory-injector.ts:11-23`）：

| 层 | 内容 | 注入方式 | 理由 |
|---|---|---|---|
| L3 Persona | 长期画像 | **全文直注 system**（截断 6000 字符） | 稳定且短 |
| L2 Scenario | 场景知识块 | **只注入 path 索引 + summary(200 字符)** | 全文常上千字符 × N |
| L1 Atom | 原子事实 | **不注入**，暴露 `tdai_memory_search` 工具 | 每轮变化，会破 cache |
| L0 Conversation | 原始对话 | **不注入**，暴露 `tdai_conversation_search` 工具 | 同上 |

原文注释（`tdai-profile-memory-injector.ts:16-19`）：

> 这样可以：1. 大幅降低首轮 token 消耗（L2 全文经常上千 chars × N 个）
> 2. 让 LLM 按需取文，而不是被无关的场景污染上下文

L3 注入前还要**剥掉尾部的 Scene Navigation 段**避免与 `<l2_scene_index>` 重复
（`stripSceneNavigation`，`tdai-profile-memory-injector.ts:236-243`）。

---

## 2. Token 预算管理

### 2.1 系统 A（MemoryProxy Injection）：**完全没有 token 预算**

这是必须明确指出的事实。grep 整个 `MemoryProxy/src/injection/`：

```
src/injection/types.ts:141:  /** Original request parameters (model, temperature, max_tokens, etc.) */
```

**除了这行注释，injection 目录零 token 相关代码。** 没有 tiktoken、没有估算、没有配额、
没有超预算降级。

存在的只是**硬编码字符截断**：

- L3 persona：`truncate(g.l3.content, 6000)`（`tdai-profile-memory-injector.ts:107`）
- L2 summary：`truncate(e.summary, 200)`（`:115`）
- 截断函数本身（`:245-247`）：

```ts
function truncate(s: string, max: number): string {
  return s.length > max ? `${s.slice(0, max)}\n...[truncated ${s.length - max} chars]` : s;
}
```

- Skill listing 字符预算 `charBudget` 默认 8000（`MemoryCore/src/gateway/skill-handlers.ts:619-628`）

**块之间没有配额分配，没有优先级降级顺序。** 每个 hook 独立产出，失败就返回 `[]`
（graceful degradation），但不会因为「预算不够」而主动牺牲某个块。唯一的「优先级」是
`HOOK_PRIORITY`，但它只决定**拼接顺序**，不决定**谁被裁掉**。

### 2.2 系统 B（Offload）：完整的 token 预算体系

**Token 计数：tiktoken `o200k_base` BPE，带三层降级。**
`MemoryCore/src/offload-client/token-estimator.ts:113-120`：

```ts
function _computeMessageTokens(msg: any): number {
  try {
    const text = extractLlmVisibleText(msg);
    return getEncoder().encode(text).length + 4; // +4 for message framing overhead
  } catch {
    return heuristicTokens(extractLlmVisibleText(msg));
  }
}
```

**只数 LLM 可见内容**（`extractLlmVisibleText`，`:36-71`）：role + content，展平
text / tool_use(name+input) / tool_result(content)，**排除** model / usage / timestamp 等元数据。

**性能优化：precise / heuristic 混合 + 自校准**（`:143-203`）。消息 ≤ 200 条全部精确；
超过则近期 200 条精确、老消息用启发式再乘校准因子：

```ts
const PRECISE_BUDGET = 200;
...
const calibFactor = heuristicSumForRecent > 0 ? preciseSum / heuristicSumForRecent : 1;
raw[i] = Math.max(1, Math.round(heuristicMessageTokens(messages[i]) * calibFactor));
```

CJK 感知的启发式（`:222-227`）——中文 1.7 字符/token，其他 4 字符/token：

```ts
return Math.max(1, Math.ceil(cjk / 1.7 + rest / 4));
```

**预算阈值（4 级阶梯）** —— `MemoryCore/src/offload_server/types.ts:139-141`：

```ts
mildOffloadRatio: 0.5,
aggressiveCompressRatio: 0.85,
emergencyCompressRatio: 0.95,
```

判级函数（`offload_server/compact/compressor.ts:115-127`）：

```ts
export function resolveLevel(ratio, config?): CompactionLevel {
  const mild = config?.mildRatio ?? 0.5;
  const aggressive = config?.aggressiveRatio ?? 0.85;
  const emergency = config?.emergencyRatio ?? 0.95;
  if (ratio >= emergency) return "emergency";
  if (ratio >= aggressive) return "aggressive";
  if (ratio >= mild) return "mild";
  return "fastpath";
}
```

**牺牲顺序（明确的降级序列）** —— `compaction-handler.ts:123-224`：

1. **fast-path**：重放已确认的 offload 状态（把已总结的 tool_result 换成 summary、删已删的）
2. **mild (≥50%)**：按 LLM 打的 `score` 降序，把 tool_result 换成摘要，只扫前 70%
   （`MILD_SCAN_RATIO = 0.7`），score < 4 停止
3. **aggressive (≥85%)**：先截断最大的 tool_result 到 2000 字符；不够再从**头部**删消息——
   **先删 tool_result，再删其他**
4. **emergency (≥95%)**：同上但更狠，目标降到 `aggressiveRatio - 0.10`

**永不牺牲的保护区**（`compressor.ts:732-733, 784-790`）：

- 最后一条 user message 及其之后（`findLastUserMessageIndex`）
- `role === "system"`
- MMD 任务图消息（emergency 里甚至先摘出来、压完再放回，`:1029-1033`）
- system-reminder 消息（`isSystemReminder`，`:183-198`）
- 最少保留 10 条（`AGGRESSIVE_MIN_KEEP = 10`）

**「谁说了算」的分离设计**（`compressor.ts:711-729`）——很精妙：

```ts
// clientTotalTokens (from API usage) is the authoritative token count.
// tokenArray (local tiktoken estimate) is only used for relative weighting
// between messages — it decides "which message to delete" not "how many tokens".
const currentTokens = clientTotalTokens && clientTotalTokens > 0 ? clientTotalTokens : tokenArraySum;
const scale = tokenArraySum > 0 ? currentTokens / tokenArraySum : 1;
```

上游 API 返回的 usage 是**绝对真值**，本地 tiktoken 只用来算**相对权重**，两者用 `scale` 映射。

**不可见开销的反推**（`compaction-handler.ts:71-77`）：

```ts
// Compute fixed overhead: system prompt + tool schemas + message framing.
// These are included in clientTotalTokens but NOT in messages.
const messagesTokenSum = messages.reduce((s, msg) => s + preciseMessageTokens(msg), 0);
const fixedOverhead = Math.max(0, totalTokens - messagesTokenSum);
```

### 2.3 缺陷：tiktoken 编码不一致

`MemoryCore/src/offload/types.ts:247` 插件默认 `l3TiktokenEncoding: "cl100k_base"`，
而 `offload-client/token-estimator.ts:18`、`offload_server/compact/compressor.ts:21`、
`offload/context-token-tracker.ts:9` 全部硬编码 `o200k_base`。
**客户端和服务端用不同 BPE 表数同一批消息**——`compressor.ts` 里那套 `scale`/校准机制
有一部分就是在吸收这个偏差。

### 2.4 召回字符预算默认关闭（文档与实现的落差）

`MemoryCore/src/config.ts:574-575`：

```ts
maxCharsPerMemory: num(recallGroup, "maxCharsPerMemory") ?? 0,
maxTotalRecallChars: num(recallGroup, "maxTotalRecallChars") ?? 0,
```

`applyRecallBudget`（`auto-recall.ts:835-843`）在两者都为 0 时直接 `return lines` 原样返回。
**开箱即用状态下唯一生效的限制是 `maxResults: 5`。**

README 声称「结果还会经过条数、字符预算和超时限制」——条数和超时是真的，
**字符预算默认是关的**。

预算实现本身质量不错，有 surrogate pair 安全截断（`auto-recall.ts:906-916`）：

```ts
// Count and slice by code point, not UTF-16 code unit, so a cut never lands
// between the halves of a surrogate pair
const cps = Array.from(line);
```

---

## 3. 历史消息压缩：摘要 / 滑窗 / 分层三者组合

### 3.1 摘要压缩（mild）—— LLM 生成 + score 驱动

不是压缩整段对话，而是**逐个替换 tool_result**。这是本项目和常见做法最不同的一点。

摘要由 L1 prompt 生成（`MemoryCore/src/offload_server/prompts/l1-prompt.ts:10`），
关键是它**强制 LLM 自评可替代性**：

```
- "score"（**必填**）: 结合信息密度和任务目的分析summary对于原文的可替代性，
  范围在0-10之间，越接近10表示summary越能替代原文。
```

这个 score 直接驱动压缩顺序（`compressor.ts:163-176`）：

```ts
// Sort by score descending (higher score = more suitable for replacement)
candidates.sort((a, b) => b.entry.score - a.entry.score);
for (const c of candidates) {
  if (c.entry.score < scoreFloor) break;          // MILD_SCORE_FLOOR = 4
  // Skip if summary would be larger than original content
  const originalLen = getTextContent(c.msg).length;
  const summaryLen = (c.entry.summary ?? "").length + 50;
  if (summaryLen >= originalLen) continue;
  replaceWithSummary(c.msg, c.entry);
```

注意那个「摘要比原文还长就跳过」的守卫——很实用的细节。

### 3.2 滑动窗口（fallback）

服务端压缩不可用时的兜底，`offload-client/context-engine.ts:229-306`：

```ts
const targetTokens = Math.floor(contextWindow * COMPACT_TARGET_RATIO);  // 0.5
// Step 1: scan from tail, find the cut index
for (let i = n - 1; i >= 0; i--) {
  cumTokens += perMessage[i];
  if (cumTokens > targetTokens) { cutIdx = i + 1; break; }
  cutIdx = i;
}
if (cutIdx <= 0) cutIdx = 0;   // Never delete the very first user message
// 2a: If msg at cutIdx is a tool_result, its paired assistant+tool_use is before cutIdx
while (cutIdx < n && isToolResult(messages[cutIdx])) cutIdx++;
// 2b: If msg at cutIdx-1 is assistant+tool_use, its tool_result would be orphaned
while (cutIdx > 0 && cutIdx < n && isAssistantWithToolUse(messages[cutIdx - 1])) cutIdx--;
const retained = messages.slice(cutIdx);
if (retained.length === 0) return [...messages];  // safety: don't delete everything
```

### 3.3 分层：MMD 任务图（最有特色的部分）

被删掉的历史不是凭空消失，而是被**升维成一张 Mermaid 流程图**重新注入。

L2 prompt（`offload_server/prompts/l2-prompt.ts:6`）的自我定位：

> 你是一个究极实用主义的 AI 任务拓扑架构师与视觉叙事者。你的核心逻辑是用尽量少的字符
> 表达尽量多的信息，**让LLM模型能看懂，不是为人类服务**，尽量减少无用的视觉符号。

节点格式：
`NodeID["阶段名: 宏观动作简述<br/>status: done|doing|paused|blocked<br/>summary: 核心结论摘要<br/>Timestamp: ISO8601"]`

有个很妙的概念叫**「认知墓碑」**（`l2-prompt.ts` 高阶指南第 2 条）：

> 遇到彻底走不通的死胡同或引发严重报错的废弃方案，可以建立警示节点（status: blocked）
> —— 防重蹈覆辙

注入文本（`offload_server/compact/mmd-injector.ts:303-326`）：

```ts
return [
  `<current_task_context>`,
  `【当前活跃任务的mermaid流程图】这是你最近正在执行的任务的阶段性记录。`,
  taskGoal ? `**任务目标:** ${taskGoal}` : "",
  `**任务文件:** ${filename}`,
  "```mermaid", mmdContent, "```",
  `标记为 "doing" 的节点是近期焦点，"done" 的已完成。请参考此保持方向感，避免重复已完成的工作。`,
  `</current_task_context>`,
].filter((line) => line !== "").join("\n");
```

MMD 有**版本去重**（内容 hash，`mmd-injector.ts:60-80`）：内容没变就只调整位置不重新插入。
历史 MMD 有独立预算 `mmdBudget = Math.floor(contextWindow * 0.1 / 4)`
（`compaction-handler.ts:195`），即上下文的 10%。

### 3.4 触发时机：客户端判定，服务端执行

`offload-client/context-engine.ts:465-496`：

```ts
const contextWindow = params.tokenBudget ?? DEFAULT_CONTEXT_WINDOW;  // 128000
// Don't use framework's knownTokens for calibration — our tiktoken is already precise.
const { total, perMessage } = estimateAllTokens(messages);
const ratio = total / contextWindow;
if (ratio < this.config.compactionRatio) { ...skip... }
const result = await this.client.compaction({ sessionId, messages, ratio, contextWindow, totalTokens: total, messageTokens: perMessage });
```

摘要生成本身是**异步离线**的（`/v2/offload/ingest` 由 after_tool_call hook 触发），
压缩时只是**查表替换**——所以压缩路径不含 LLM 调用，很快。
这是「摘要生成」与「摘要应用」解耦的典型做法。

---

## 4. Proxy 层拦截机制

### 4.1 怎么插进去的：**URL 路径前缀 + 反向代理**

不是 MITM，不是 SDK patch，就是让客户端把 base URL 指向 proxy。路径形如：

```
/{agentSource}/{spaceId}/[analyse|cost-guard]/v1/messages
例：/codebuddy/default/analyse/v1/messages
```

`agentSource`（`claude-code` / `codebuddy`）从路径解析，直接用于 AgentProfile 查表
（`pipeline.ts:109-112`）——零成本，不需要扫 system prompt：

```ts
// ① Fast path: URL-path-based lookup (zero cost, no string scanning)
if (this.agentProfiles) {
  profile = this.agentProfiles.get(metadata.agentSource) ?? null;
}
// ② Legacy fallback: scan system prompt text (for paths without agent prefix)
if (!profile && this.detectAgent) { ... }
```

拦截点：`anthropicHandler.ts:993-1013`（Anthropic）/ `handler.ts:858`（OpenAI）。

### 4.2 拦截了哪些字段

`AnthropicAdapter.parse`（`injection/adapters/anthropic.ts:23-54`）：

- `body.system`（string 或 ContentBlock[]）→ 一条 `role:"system"` 的 ContextMessage
- `body.messages[]` → ContextMessage[]，content block 按 type 解析
- `body.tools[]` → AgentTool[]
- **其余所有字段原样进 `requestParams`**（`:46-51`）：

```ts
for (const [key, value] of Object.entries(body)) {
  if (key !== "messages" && key !== "system" && key !== "tools") requestParams[key] = value;
}
```

serialize 时 `const body = { ...ctx.requestParams }` 先展开——**未知字段透传，不丢失**。

### 4.3 怎么保证不破坏协议

**(a) tool_use / tool_result 配对：结构上不可能破坏。**
Injection pipeline **只做两件事**：`prependTextToMessage` / `appendTextToMessage`
（`injection/context.ts:80-89`）——都是往已有 message 的 `blocks` 数组里塞一个 `{type:"text"}`。
它**从不删除消息、从不新增消息、从不改 role**。所以 messages 交替和 tool 配对天然保持。

**(b) content block 的 wire 形状精确重建**（`adapters/anthropic.ts:212-276`）。
`serializeContentBlockInner` 按 type 分支产出：

- `text` → `{type, text}`
- `tool_use` → `{type, id, name, input}`
- `tool_result` → `{type, tool_use_id, content}`，`is_error` 仅在真为 true 时才加
- `thinking` → `{type, thinking, signature}`

> 这一点与本仓库 CLAUDE.md §14.1 的「ContentBlock wire 形状必须按 Type 收敛」是同一条教训，
> 两个项目独立踩过同一个坑。

**(c) `cache_control` 全链路保真**（`adapters/anthropic.ts:110-114, 214-218, 190-193, 284-286`）：

```ts
// Preserve prompt-cache breakpoint marker across the round-trip.
if (block.cache_control !== undefined && parsed.type !== "custom") {
  parsed.metadata = { ...parsed.metadata, cache_control: block.cache_control };
}
```

**(d) 一处隐患**：`serializeSystemMessage`（`adapters/anthropic.ts:198-205`）：

```ts
private serializeSystemMessage(msg: ContextMessage): unknown {
  const textBlocks = msg.blocks.filter((b) => b.type === "text");
  if (textBlocks.length === 1) return textBlocks[0].content;   // ← 退化成 string
  return msg.blocks.map((b) => this.serializeContentBlock(b));
}
```

如果原始 `system` 是**带 cache_control 的单元素数组**，anchor 命中路径会把整个 system
合并成一个 block（`pipeline.ts:368`），就会被序列化成**纯字符串**——`cache_control` 丢失。

**(e) 消息交替校验：没有。** grep `alternat|consecutive|merge.*user|sanitize.*messages`
在 `MemoryProxy/src` 零命中。当前安全（因为 pipeline 不增删消息），但缺护栏。

> 对比：本仓库因为 Memory 会增删消息，必须有 `SanitizeMessagesForAnthropic`
> 合并相邻 user 消息（CLAUDE.md §14.1）。**是否需要 sanitize，取决于你的层会不会动消息数组。**

**(f) 真正做 tool 配对修复的是 MemoryCore 的压缩层**，因为它会**删消息**。三道防线：

`fast-path.ts:118-186` 完整孤儿清理；
`compressor.ts:624-669` `ensureToolPairIntegrity` 用 **while(changed) 循环到不动点**
——因为删 assistant 可能级联出新的孤儿：

```ts
let changed = true;
while (changed) {
  changed = false;
  for (const [toolUseId, resultIdx] of toolResultIdToIdx) {
    if (deleteIndices.has(resultIdx) && !deleteIndices.has(assistantIdx)) {
      const allResultsDeleted = allUseIds.every((id) => { ... });
      if (allResultsDeleted) { deleteIndices.add(assistantIdx); changed = true; }
    }
  }
}
```

MMD 插入位置也有配对守卫 `adjustForToolCallPair`（`mmd-injector.ts:379-422`）：
绝不插在 assistant(tool_use) 和它的 tool_result 之间。

**(g) 全局兜底：hook 失败不致命**（`pipeline.ts:229-250`）。任何 hook 抛异常都只是
log + continue；整个 pipeline 抛异常在 handler 层被 catch，请求照常转发
（`anthropicHandler.ts:1010-1012`）。即注入失败 → 退化成裸转发，**永不 500**。

### 4.4 请求三分类：Claude Code 专属优化

`common/cc-request-classifier.ts:30-52` 基于 `cache_control` marker 位置把 CC 请求分成
main / fork / sidequery：

```ts
if (markerIdx >= 0) {
  if (markerIdx === n - 2) return "fork";   // skipCacheWrite=true 强制挪到 n-2
  return "main";
}
const toolsEmpty = !Array.isArray(body.tools) || body.tools.length === 0;
const thinkingOff = (body.thinking as {type?:string})?.type === "disabled";
if (toolsEmpty && thinkingOff) return "sidequery";
return "main";
```

三类的注入策略完全不同（`anthropicHandler.ts:985-989`）：

- **sidequery**（标题生成/verify_api_key）：**完全跳过注入**——自带短 prompt，不共享 cache
- **fork**（SUGGESTION/RECAP/COMPACT）：走 pipeline 但 `readOnly=true`
- **main**：完整 pipeline + cache self-heal

`readOnly` 的语义很讲究（`injection/types.ts:118-125`、`pipeline.ts:298-315`）：
fork 请求 cache miss 时**不自愈写回**，因为写入内容可能与 main 那次不 byte-level 一致，
反而破坏后续主对话的 cache。

### 4.5 凭据隔离

`<tdai_memory_tools>` 教 LLM 用 Bash + curl 调 `<proxy>/memory-bridge/v3/*`，
proxy 反向代理时才注入身份和 Bearer（`tdai-tools-injector.ts:8-13`）：

> proxy 端反向代理到 tdai gateway，期间注入 IdFields + Bearer，
> rules out LLM 伪造身份 + 防止 token 进入 prompt。

bridge 白名单只放行三个只读端点（`memory/memory-bridge.ts:48-53`），
**L3 的 `core/read` 明确不放行**，因为已经直注 system 了。

---

## 5. 多 Agent 隔离

### 5.1 隔离维度：五元组

Hook cache 的键（`storage/key-utils.ts:7`）：

```
<ttl|nottl>/<spaceId>/<userId>/<agentSource>/<sessionId>/<data-type>[/subpath]
```

SQLite 后端用复合字符串（`db/hookCacheRepo.ts:63-71`）：

```ts
function compositeSid(spaceId, userId, agentSource, sessionId): string {
  const sp = spaceId || "_default";
  return `${sp}:${userId}:${agentSource}:${sessionId}`;
}
```

`userId` 缺省 fallback 到 `"anonymous"`，注释说明了原因（`pipeline.ts:178-184`）：

> 缺省时 fallback 到 "anonymous"（与 handler 层一致，防止未鉴权请求撞到已鉴权用户的缓存）

路径段有**注入防护**（`key-utils.ts:13-15, 18, 28`）：校验非空 + 不含 `/` + 不含 `..`，
`agentSource` 还要匹配 `/^[a-z0-9-]+$/`。

### 5.2 per-agent 配置：两层

**(a) AgentProfile 层（per-agent-type）**：不同 Agent 类型有不同的 system prompt 结构解析器。
注册表就一行一个（`injection/index.ts:360-364`）：

```ts
agentProfiles: new Map<string, AgentProfile>([
  ["codebuddy", new CodeBuddyProfile()],
  ["claude-code", new ClaudeCodeProfile()],
  // ["cursor", new CursorProfile()],
]),
```

加一个新 Agent 只需加一行 + 一个 Profile 实现，**hook 侧零改动**——这是语义槽设计的直接收益。

**(b) Agent 实例层**：`assetCapabilities` 四个开关（`injection/types.ts:283-288`）：

```ts
export interface AssetCapabilityFlags {
  skill: boolean;
  llm_wiki: boolean;
  code_graph: boolean;
  chat_memory: boolean;
}
```

每个 injector 开头自检（如 `tdai-profile-memory-injector.ts:47-48`）：

```ts
const caps = ctx.metadata.custom?.assetCapabilities as { chat_memory?: boolean } | undefined;
if (caps?.chat_memory === false) return [];
```

**(c) 跨 Agent 记忆借用**：`resolveFixedAssetCtxs` 返回 `[self, ...借入≤2]`，
每个 agent 的记忆在 prompt 里**分段标注来源**（`tdai-profile-memory-injector.ts:102-104`）：

```ts
const tag = g.ctx.isSelf ? "self" : "imported_from";
lines.push(`<agent name=${JSON.stringify(g.ctx.agentName)} role=${JSON.stringify(tag)} agent_id=${JSON.stringify(g.ctx.agentId)}>`);
```

**(d) L1 记忆是 agent 维度跨 session 的**（`MemoryCore/src/gateway/v2-router.ts:1152-1161`）：

```ts
// L1 召回为 agent 维度（跨 session）：filter 只取 team/user/agent/task，
// **不带 sessionId**，否则会把其它 session 写入的 L1 记忆过滤掉
```

---

## 6. 缓存：Prompt Cache 是第一约束

### 6.1 Prompt Cache（Anthropic `cache_control`）

**这不是一个「特性」，而是驱动了前面几乎所有设计决策的核心约束**：

1. L1/L2 召回被下线，改成工具按需检索（`injection/index.ts:299-303`）
2. `auto-recall` 的 stable/dynamic 二分（`auto-recall.ts:258-268`）
3. sidequery 完全跳过注入、fork 用 readOnly（`anthropicHandler.ts:985-1010`）
4. `cache_control` 全链路保真（`adapters/anthropic.ts`）
5. `<asset_reflection>` 默认不注册，注释写明「保证 KV cache 前缀不受影响」
   （`asset-reflection-injector.ts:89`）

**不主动加 cache breakpoint 的克制**（`session/context-injector.ts:236-243`）：

> Cache-control preservation: when `system` is an array whose last block carries
> `cache_control` ... we keep that marker where it is and append a *plain* text block
> after it. ... Adding a second `cache_control` on the new block would create an
> unrequested breakpoint (Anthropic charges for those), so we deliberately do not.

**多节点部署的 cache 陷阱**（`injection/index.ts:210-216`）——很有实战价值：

> ⚠️ 多节点部署必须显式配 `injection.externalGatewayUrl` —— 否则每个 pod 会把
> 自己的 host:port 嵌进 `<skill_tools>` / `<tdai_memory_tools>` 文本，
> pods 互相覆盖 hook cache，同时上游 KV cache 每次 miss。

即：**注入文本里嵌了本机 IP → 每个 pod 产出的 system prompt 不同 → cache 全 miss**。

### 6.2 Hook Cache（增量拼装 / 预热）

三种策略（`injection/types.ts:256-272`）：

| 策略 | 行为 |
|---|---|
| `none` | 每请求都 `execute(ctx)` |
| `session_init` | session 建立时 `prewarm()` 一次，后续请求直接读 cache，**跳过 execute** |
| `hybrid` | prewarm + 每请求 execute，两者并集按 `cacheKey ?? content` 去重 |

预热在 `prewarm.ts:74-174`，20s 总超时，`Promise.allSettled` + 全局 deadline 双保险，
**永不抛异常**。

**Cache miss 自愈**（`pipeline.ts:291-319`）。`SkillInjector.execute()` 有一大段注释解释
为什么它必须能复现 prewarm 的产物（`skill-injector.ts:123-142`）：

> Historically this returned `[]` unconditionally, which meant a miss on any node other
> than the one that ran prewarm silently dropped `<available_skills>` from the system
> prompt for the entire session.

为保证自愈写入不与预热条目冲突，两条路径共用一个 renderer 且共享 `cacheKey`
（`skill-injector.ts:252-254`）。

### 6.3 Token 计数缓存

`WeakMap<object, number>`（`offload-client/token-estimator.ts:75`）——消息对象被 GC 时自动释放。

---

## 7. 可观测性

### 7.1 Injection 层：有结构化 observer，但**不统计 token**

`InjectionObserver` 接口（`injection/observer.ts:63-80`）5 个回调，三种实现：

| 实现 | 启用条件 | 行为 |
|---|---|---|
| `LangfuseInjectionObserver` | `langfuse.enabled` | 每个 hook 一条 span |
| `LoggingInjectionObserver` | log level ≤ info | 结构化日志 |
| `NoopInjectionObserver` | 否则 | 空 |

**每个 hook 记录的字段**（`observer.ts:193-200`）：
`hookId / point / blockCount / durationMs / cacheStrategy / blocks[]`，
每块含 `type / source / preview`（截断 200-300 字符）。

**没有 tokenCount 字段。** 无法回答「各块占了多少 token」。

Langfuse trace ID 是**确定性派生**的（`observer.ts:227-229`），保证注入 span 和上游
LLM generation 挂在同一 trace：

```ts
function deriveLangfuseTraceId(sessionKey: string, turnSeq: number): string {
  return createHash("sha256").update(`${sessionKey}:${turnSeq}`).digest("hex").slice(0, 32);
}
```

**observer 绝不影响主流程**：所有调用包在 `safeCall`（`pipeline.ts:506-512`）。

### 7.2 压缩层的可观测性（明显更好）

`CompactionReport`（`compaction-handler.ts:28-38`）8 个计数器，单行汇总日志
（`compaction-handler.ts:256-261`）：

```
compaction done: 320→180 msgs, level=aggressive, tokens=190000→95000 (0.48),
fp=12r/5d, mild=8, agg=115, em=0, mmd=2
```

### 7.3 专门的 Cache 命中调试工具（很有借鉴价值）

`PROXY_DEBUG_DUMP_OUTBOUND_MD5=1` 开启后打三段 md5（`anthropicHandler.ts:385-432`）：

```
//   1. sysFullMd5    — md5(JSON.stringify(body.system))  全 system 序列化
//   2. sysStrMd5     — md5(body.system 拉平成字符串)    仅文本内容（对比用）
//   3. msgsPrefixMd5 — 找到 messages 里最后一个带 cache_control 的位置 N，
//                      md5(JSON.stringify(messages[0..N]))，即真正的 cache 前缀
//   4. msgsAnchorIdx — 上面那个 N（帮助定位命中长度）
// 任何一个 md5 变了都意味着 Anthropic 会 cache miss。
```

把「cache 为什么没命中」从玄学变成可 diff 的确定性问题。

---

## 8. 「真实实现」vs「只在 README 里说了」

### ✅ 真实实现且质量高

- Hook pipeline + 9 注入点 + priority（`pipeline.ts`）
- 语义槽 + AgentProfile 双 Agent 适配，无损 parse/rebuild
- cache_control 全链路保真 + hook cache 三策略 + 预热 + 自愈
- tiktoken 精确计数 + CJK 启发式 + 混合精度 + 自校准
- 4 级压缩阶梯 + score 驱动摘要 + MMD 任务图
- tool_use/tool_result 配对完整性三道防线
- CC 请求三分类、五元组隔离、Langfuse/Opik/ClickHouse 三路可观测

### ⚠️ 声明了但未接线（死代码）

1. **`TdaiL1RecallInjector`** —— 120 行完整实现，零注册。**但这是有意下线**（注释明确），
   属「代码未清理」而非 bug。
2. **`charBudgetPercent`** —— `MemoryCore/src/core/skill/types.ts:29,87` 定义、
   `skill-config.ts:171` 赋默认值 0.01，**无任何消费点**。
3. **`searchMemoriesWithDetails`**（`auto-recall.ts:397-422`）—— 定义并导出，无调用者。
4. **`recallL1` 配置项** —— 仍传入 tdaiBaseConfig，但对应 injector 已不注册。

### ⚠️ README 说了、代码打了折扣

- README_CN.md:234 称召回「经过条数、**字符预算**和超时限制」——条数(`maxResults:5`)
  和超时(5s)是真的；**字符预算 `maxCharsPerMemory`/`maxTotalRecallChars` 默认都是 0 即关闭**。

### 🐛 代码中的真实缺陷

1. **tiktoken 编码不一致**：插件 `cl100k_base` vs 其他三处 `o200k_base`。
2. **两套压缩实现的常量分叉**：`AGGRESSIVE_MIN_MESSAGES_TO_KEEP = 2`（插件内）
   vs `AGGRESSIVE_MIN_KEEP = 10`（服务端）。同名语义，差 5 倍。
3. **system 单块退化丢 cache_control**：anchor 命中路径下 system 被压成单个 text block
   并序列化为纯字符串。

---

## 9. 可迁移到其他 Agent 系统的 Context 管理设计经验

1. **把「注入什么」和「注入到哪」解耦成语义槽，而不是硬编码位置。**
   Hook 声明 `{slot:"memory", relation:"before"}`，各 Agent 用自己的 Profile 翻译成具体锚点
   ——新增一个 Agent 只加一行注册，所有 hook 零改动。

2. **Prompt cache 应该是 context 架构的第一约束，而不是事后优化。**
   本项目为了保 cache 直接砍掉了「每轮自动召回」这个看似核心的功能，改成注入工具让 LLM
   自己检索——因为**任何每轮变化的内容放进 system prompt，都会让整个前缀失效**。

3. **按「变化频率」而非「内容类型」切分 context 区域。**
   stable(persona/scene/tools-guide → system 尾) vs dynamic(L1 记忆 → user 首) 二分，
   让不变的部分吃满 cache，变的部分只污染 cache 边界之后。

4. **分层记忆要有分层的注入策略：稳定的直注、庞大的注索引、易变的给工具。**
   L3 全文直注 / L2 只注 path+summary / L1&L0 只给检索工具——因为「注入」和「可访问」
   是两回事，索引 + 按需读取比全量注入省一个数量级 token。

5. **让摘要生成器自己评估「可替代性」，用这个分数驱动压缩顺序。**
   L1 prompt 强制 LLM 输出 0-10 的 `score`，压缩时按 score 降序替换、低于阈值即停——
   比「按时间从旧到新删」精确得多，因为旧 ≠ 不重要。

6. **压缩必须分级，且每级都有明确的保护区和最小保留量。**
   0.5/0.85/0.95 三档 + 保护「最后一条 user message 之后 / system / 任务图 / 最少 10 条」
   ——单一阈值要么触发太早浪费算力，要么太晚直接爆窗口。

7. **删消息的地方必须有「循环到不动点」的 tool 配对修复。**
   `ensureToolPairIntegrity` 用 `while(changed)`，因为删 tool_result → 删对应 assistant
   → 可能又产生新孤儿，一遍扫不干净；漏一个就是上游 400。

8. **上游返回的 usage 是唯一真值，本地 token 估算只配决定「删谁」。**
   用 `scale = clientTotal / localSum` 把两个体系桥接——本地估算永远有偏差，
   但**相对大小关系**是可靠的，这个分工让偏差不累积。

9. **摘要「生成」与摘要「应用」必须解耦。**
   摘要由异步 ingest 离线产出、压缩时只查表替换，所以压缩路径**不含 LLM 调用**——
   否则每次接近窗口上限都要等一次 LLM，延迟不可接受。

10. **注入层的每个环节都要能失败，且失败即降级为「什么都不做」。**
    hook 抛异常 → log+continue；pipeline 抛异常 → 裸转发；prewarm 超时 → 无缓存；
    observer 抛异常 → 吞掉。记忆增强是**锦上添花**，绝不能成为主链路的故障点。

11. **给 cache miss 准备专门的 diff 工具。**
    打 system/messages-prefix 三段 md5，把「为什么没命中缓存」从玄学变成可比对的确定性问题。

12. **凭据永远不进 prompt，用反向代理换取身份注入。**
    教 LLM curl 一个 proxy 路径，proxy 转发时才注入 Bearer 和身份字段，
    既防 LLM 伪造身份，也防 token 泄漏进上下文/日志/训练数据。
