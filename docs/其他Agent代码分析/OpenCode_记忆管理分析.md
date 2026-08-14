# OpenCode 记忆管理与 Memory.md 持久化文件管理 — 深度分析

> **分析对象**：`/usr/local/LsmGitOpenSource/opencode`
> **分析角度**：记忆管理、Memory.md 持久化、会话 vs 用户/项目记忆分层、上下文压缩、消息历史持久化、工具调用历史存储。
> **框架**：代码路径 → 实现细节 → 设计动机 / 教训
> **作者注**：本报告基于 2026-08-14 阅读 OpenCode 主分支源码（TypeScript、Bun runtime、Effect-TS）。报告不含「建议本仓库狼人杀 Agent 学什么」 — 那部分见《狼人杀Agent_根据_OpenCode_优化和解决方案_20260814.md》。

---

## 0. 顶层结论

OpenCode 是一个面向"软件工程"的 AI Agent CLI/IDE 项目。它的记忆管理体系采用**独特的"分层 + 显式持久化 + 自动压缩"**设计 —— 与传统 RAG / Vector-DB 式"隐式"记忆不同：

1. **持久化指令文件（AGENTS.md / CLAUDE.md）** 是显式文件系统操作，通过 `Instruction` Service 加载并注入 system prompt。
2. **会话级长期记忆**依靠 Context Epoch（V2） + Compaction（压缩）的机制实现 —— 在 SQLite 落地，可观测可回放。
3. **跨会话记忆**完全靠"文件系统 + git + share_url"，**没有 vector store / embedding** —— 即没有"agent 自动检索历史会话"这条隐式路径。

整套设计的核心思想：**用文件系统做长期记忆，用 SQLite 做短期消息历史，用 schema + Effect Layer 做边界隔离，用 System Context + Context Epoch 做 token 预算管理。**

---

## 1. 核心文件清单与职责

### 1.1 持久化指令文件（Memory.md）的读取与注入

| 文件路径 | 职责 |
|---|---|
| `packages/opencode/src/session/instruction.ts` (V1) | AGENTS.md / CLAUDE.md / CONTEXT.md 解析服务；按 worktree 向上查找、加载全局配置下的 AGENTS.md；按需在工具调用时挂载"邻近"指令文件 |
| `packages/opencode/src/session/system.ts` | System Prompt 组装器：将 environment / skills / MCP / `instruction.system()` 拼成 system messages 数组 |
| `packages/core/src/instruction-context.ts` (V2) | `InstructionContext` System Context Source，作为可独立 refresh 的 typed source 注册到 SystemContextRegistry；只读"项目 AGENTS.md + global AGENTS.md" |
| `packages/core/src/system-context/index.ts` | "System Context"框架核心：定义 Source / Generation / Snapshot / reconcile / replace 等基础类型 |
| `packages/core/src/system-context/registry.ts` | 注册各 System Context Source 的服务；按 key 去重、按需并发加载 |
| `packages/core/src/system-context/builtins.ts` | 内置 System Context Source（env、date 等）实现 |

### 1.2 会话、消息与 Compaction（压缩）

| 文件路径 | 职责 |
|---|---|
| `packages/opencode/src/session/compaction.ts` (V1) | compact 处理服务：`select()` / `prune()` / `process()` / `create()`；通过 SessionProcessor 调用 compact agent |
| `packages/opencode/src/session/overflow.ts` | `isOverflow()` / `usable()` 根据模型 context window 计算可用 token；判断何时超限 |
| `packages/core/src/session/compaction.ts` (V2) | `SessionCompaction.make()` 工厂；构造 `compactIfNeeded()` / `compactAfterOverflow()` 两个 Effect 函数；含 `buildPrompt()` 模板 |
| `packages/opencode/src/session/prompt.ts` | V1 主循环 `runLoop()` 编排 compact / prune / overflow / 步骤上限等 |
| `packages/opencode/src/session/message-v2.ts` | `WithParts` 类型、`toModelMessagesEffect` 把消息投影为 LLM 输入；`filterCompacted` 把压缩后的 messages 重排成 `[compaction-user, summary, ...retained tail, continue-user]` |
| `packages/opencode/src/session/processor.ts` | V1 的 SessionProcessor：把 LLMEvent 流解析为 message parts；step-finish 触发 summary；step-finish 时若 overflow 则置 `needsCompaction` |
| `packages/opencode/src/session/summary.ts` | 把 step-finish 处的代码 patch 算成 diff、写回 session.summary |
| `packages/opencode/src/session/session.ts` | Session CRUD + `updateMessage` / `updatePart`（只发事件，不直写 DB） |
| `packages/opencode/src/session/revert.ts` | 快照 revert：revert 到某个 messageID，重建历史，写入 `session_diff/<id>` 持久化 diff |
| `packages/opencode/src/session/reminders.ts` | Plan ↔ Build 切换的合成提示注入 |
| `packages/core/src/session/runner/llm.ts` (V2) | Session Runner：`runTurn()` 单 turn 编排；`compactIfNeeded` / `compactAfterOverflow` 接入；用 `TurnTransition` Error 表示 "压缩后回到主循环" |
| `packages/core/src/session/runner/index.ts` (V2) | Runner 入口 Interface（`run({sessionID, force})`） |
| `packages/core/src/session/context-epoch.ts` | "Context Epoch"：每次 turn 维护 baseline + snapshot；初次观察失败则"阻塞初始化"；compaction 后下次重渲染 baseline；Location move 时清空 |
| `packages/core/src/session/history.ts` | 从 SessionMessageTable 加载当前可用的 history；按 latestCompaction + baselineSeq 过滤 |
| `packages/core/src/session/message-updater.ts` | 把 `session.next.*` 事件投影成 `SessionMessage.Message[]`，形成 in-memory MemoryState；`memory()` 工厂模式让上层可注入不同 adapter |
| `packages/core/src/session/sql.ts` | SQLite 表定义：`SessionTable` / `MessageTable` / `PartTable` / `TodoTable` / `SessionMessageTable` / `SessionInputTable` / `SessionContextEpochTable` |
| `packages/core/src/database/database.ts` | SQLite 实例工厂；开启 WAL、NORMAL synchronous、busy_timeout 等 PRAGMA |
| `packages/core/src/tool-output-store.ts` | V2 工具输出存储（fiber-based） |
| `packages/core/src/snapshot.ts` | Git 快照（track / capture / restore / diff） |
| `packages/schema/src/v1/session.ts` | V1 Session 的 Effect Schema：`User` / `Assistant` / `Part` 联合（TextPart / ToolPart / SubtaskPart / ReasoningPart / FilePart / StepStartPart / StepFinishPart / SnapshotPart / PatchPart / AgentPart / RetryPart / CompactionPart） / `Event` |
| `packages/opencode/src/session/message.ts` | AI SDK 风格的 `MessagePart` |
| `packages/opencode/src/session/schema.ts` | 复导出 schema |
| `packages/opencode/src/session/run-state.ts` | 每 session 一个 Runner；保证 busy 互斥；interrupt 时调用 onInterrupt |

---

## 2. Memory.md 持久化文件管理

### 2.1 文件加载路径（V1）

代码路径：`packages/opencode/src/session/instruction.ts`。

**关键数据结构（lines 34-44）**：
```ts
export interface Interface {
  readonly clear: (messageID: MessageID) => Effect.Effect<void>
  readonly systemPaths: () => Effect.Effect<Set<string>, FSUtil.Error>
  readonly system: () => Effect.Effect<string[], FSUtil.Error>
  readonly find: (dir: string) => Effect.Effect<string | undefined, FSUtil.Error>
  readonly resolve: (
    messages: SessionV1.WithParts[],
    filepath: string,
    messageID: MessageID,
  ) => Effect.Effect<{ filepath: string; content: string }[], FSUtil.Error>
}
```

**实现细节**：
1. `globalFiles`（lines 60-63）：优先匹配 `global.config + "/AGENTS.md"`，可选用 `~/.claude/CLAUDE.md`（除非 `disableClaudeCodePrompt` flag）。
2. `instructionFiles`（lines 64-68）：项目级查找顺序：`AGENTS.md` → `CLAUDE.md` → `CONTEXT.md`（已废弃）。
3. `systemPaths()`（lines 110-153）：先确定"全局 AGENTS.md"（第一个 match 即 break，避免累加所有祖先的同名文件）；再在 `worktree` 内向上找项目级文件，**第一个有命中的类型就 break**（注释明确："The first project-level match wins so we don't stack AGENTS.md/CLAUDE.md from every ancestor"）；最后合并 `config.instructions` 中的额外指令。
4. `system()`（lines 155-169）：把所有路径读成字符串数组，输出 `Instructions from: /abs/path\n<content>`。
5. `resolve()`（lines 179-221）：**lazy 加载的"邻近"指令文件** —— 在工具调用读某个文件时，沿"被读文件"路径向 root 方向回溯，逐级找指令文件并挂载到该轮 messages。每条指令对每个 message 只挂一次（`claims: Map<MessageID, Set<string>>` 在 InstanceState 中追踪）。

**设计动机**：OpenCode 不想一次性把所有祖先 AGENTS.md 都塞进 system prompt（"避免 stack"），只在系统层挂"全局 + 项目级最近一个"，剩余的仅在**按需读文件**时挂载。

### 2.2 文件加载路径（V2 / System Context 体系）

代码路径：`packages/core/src/instruction-context.ts`。

**关键实现**：
```ts
const discovered = new Set(
  yield* Effect.forEach(
    Flag.OPENCODE_DISABLE_PROJECT_CONFIG || !insideProject ? []
      : yield* fs.up({ targets: ["AGENTS.md"], start, stop }),
    fs.resolve,
  ),
)
const paths = Array.dedupe([yield* fs.resolve(join(global.config, "AGENTS.md")), ...discovered])
```

**实现细节**：
1. V2 用 SystemContext 框架（`packages/core/src/system-context/index.ts`）统一管理所有"privileged system context"。
2. 每次 turn 通过 `loadSystemContext()` 重新 `observe()` 所有 source。
3. `instruction-context` 的 `key = "core/instructions"`。
4. 输出经过 `baseline()` / `update()` / `removed()` 三种渲染函数。
5. ContextEpoch（`packages/core/src/session/context-epoch.ts`）持久化 baseline 字符串与 structured snapshot；reconcile / replace 算法维护"更新时输出 chronological update 文本 + atomic 推进 snapshot"。

**设计动机**：V1 的 AGENTS.md 加载是 stateless 的（每次都现找、现读）；V2 把它升级成 SystemContext source 后，可在 ContextEpoch 中记录"上次 observe 到的状态"，从而在文件**变化**时只输出"diff 风格的 chronological System message"，而不是每次重新全量注入 —— 这能节省大量 token。

### 2.3 AGENTS.md → System Prompt 注入路径

代码路径：`packages/opencode/src/session/prompt.ts` `runLoop`（lines 1257-1270）：

```ts
const [skills, env, instructions, mcpInstructions, modelMsgs] = yield* Effect.all([
  sys.skills(agent),
  sys.environment(model),
  instruction.system().pipe(Effect.orDie),
  sys.mcp(agent, session.permission),
  MessageV2.toModelMessagesEffect(msgs, model),
])
const system = [
  ...env,
  ...instructions,
  ...(mcpInstructions ? [mcpInstructions] : []),
  ...(skills ? [skills] : []),
]
```

末尾追加 `STRUCTURED_OUTPUT_SYSTEM_PROMPT`（仅当该 user message 要求 json_schema 输出）。

**同时** `provider(model)` 按模型 ID 选择不同的 base prompt 文件（`prompt/anthropic.txt` / `gpt.txt` / `beast.txt` / `gemini.txt` / `kimi.txt` / `trinity.txt` / `codex.txt` / `meta.txt` / `default.txt`），拼到 system 最前面。

V2 对应路径在 `packages/core/src/session/runner/llm.ts` lines 197-214：组装 `LLM.request({ model, system: [agent.info?.system, system.baseline], messages: [...toLLMMessages(context, model), ...], tools })`。

**教训**：system 顺序是 `provider-base + env + instructions(AGENTS.md) + mcp + skills`。CLAUDE.md 风格的隐式指令被显式摆到 system 中段，前后分别是"环境"和"工具域"，确保 LLM 知道**用户的项目约定**，而不是只看到原始的 provider 指令。

---

## 3. 作用域拆分（5 个作用域）

OpenCode 的"记忆"实际上有 **5 个作用域**，分层组合后注入到 system prompt：

| 作用域 | 存储位置 | 加载路径 | 范围 |
|---|---|---|---|
| Provider 基线 | `packages/opencode/src/session/prompt/*.txt` | `system.ts: provider(model)` | 全部 session，按 model.id 选 |
| 全局指令 | `<global.config>/AGENTS.md` 或 `~/.claude/CLAUDE.md` | `instruction.ts: systemPaths()` / `instruction-context.ts: observe()` | 当前 instance 跨 project |
| 项目级指令 | `<worktree>/.../AGENTS.md` | 同上 | 整个 worktree |
| 工作区级 | `WorkspaceV2.ID` | `Session.Info.workspaceID` | 当前 workspace |
| Session 级 | `Session.Info.metadata` / `summary` / `revert` | `session.ts` | 仅当前 session |

**合并策略（V1）**：
- Global first（systemPaths 返回顺序：globalFiles → project files → config.instructions），**全局只取首个 match**；
- 项目级只取首个 match（注释："The first project-level match wins"）；
- `config.instructions` 是数组，逐个 glob 解析。

**合并策略（V2）**：
- 全部以 typed source 形式注册到 `SystemContextRegistry`；
- `SystemContext.combine(values)` 拒绝重复 key，但本身是并集；
- `observe()` → `reconcile(value, previous)` 或 `replace(value, previous)`；
- 输出顺序：在 systemContextRegistry 中按 key 字典序排序加载；ContextEpoch 的 baseline 由各 source 的 `baseline()` 渲染串接；
- 各 source 互相**独立**，可以独立 unavailable；unavailable ≠ removed（unavailable 保留上次 snapshot，removed 触发 supersession text）。

**教训**：V1 把"指令"看作一次性的字符串列表，V2 把它升级为"可 reconcile 的 typed source"。这是从"文档型 prompt"到"系统上下文"的演进 —— 把变化管理变成可观察、可 diff、可回滚的事务。

---

## 4. 上下文压缩（Compaction）机制

### 4.1 触发条件

**V1 双触发路径**（`packages/opencode/src/session/prompt.ts` `runLoop`）：

1. **常规 overflow 检测**：在 `step-finish` 事件中（`processor.ts` lines 477-483），`isOverflow({cfg, tokens, model})` 为 true 时设 `ctx.needsCompaction = true`；下一轮主循环 lines 1320-1328 `compaction.create({...})` 创建 compaction user message + compaction part。
2. **手动 / 任务触发**：通过 Subtask / CompactionPart 在 user message 中携带，触发 lines 1149-1159 的 `compaction.process`。
3. **循环结束后 prune**（`prompt.ts` line 1338）：`compaction.prune({sessionID})`，清理超出 `PRUNE_PROTECT` token 的旧 tool result。

**V2 触发路径**（`packages/core/src/session/runner/llm.ts`）：

1. **turn 内的 estimate**（line 215）：`compaction.compactIfNeeded({sessionID, entries, model, request})`：若 `estimate(system + messages + tools) > context - max(output, buffer)`，触发自动压缩。
2. **overflow 触发**（lines 280-288）：当 provider 返回 `context_overflow` 且 assistant 还没开始时，调用 `recoverOverflow = compaction.compactAfterOverflow`；成功后再次发起 provider turn。

### 4.2 压缩算法（V1：手动 compaction）

`packages/opencode/src/session/compaction.ts`：

- `select()`（lines 223-269）：先通过 `cfg.compaction?.tail_turns` 取最近 N 轮；从最新一轮往旧遍历，估计 tokens；若超 `preserveRecentBudget`（默认 `clamp(usable * 0.25, 2_000, 15_000)`），则尝试 `splitTurn()`；最终保留 head（待压缩）+ tail_start_id。
- `serialize()`（lines 54-85）：把所有 message parts 渲染成 `[User]: text` / `[Assistant]: text` / `[Assistant reasoning]: text` / `[Assistant tool call]: tool(input)` / `[Tool result]: output` / `[Tool error]: error`。
- `buildPrompt()`：V2 的 `packages/core/src/session/compaction.ts: buildPrompt()`（lines 160-174）输出：
  - 首次：`<conversation>...</conversation>` + 引导句 + `SUMMARY_TEMPLATE`（Objective / Important Details / Work State {Completed, Active, Blocked} / Next Move / Relevant Files）。
  - 后续：`<prior-summary>...</prior-summary>` + `SUMMARY_UPDATE_INSTRUCTIONS`（明确要求："Carry forward objectives... where they conflict, the conversation wins: state the corrected fact and drop the old claim"）。
- `process()`（lines 319-557）：复用 `agents.get("compaction")` 模型（可与 user 不同），创建一条 `role=assistant, summary: true` 的占位 assistant message；调用 `processor.process({...})` 生成摘要；根据 `result` 返回 `"continue" | "stop"`；如果是 auto，根据 plugin hook `experimental.compaction.autocontinue` 决定是否生成 "continue if you have next steps..." 的 synthetic user message。

### 4.3 压缩后的记忆注入路径

`packages/opencode/src/session/message-v2.ts` 的 `filterCompacted()`（lines 521-572）：

- 输入：完整的 `WithParts[]`（按 id 排序）；
- 算法：
  1. 找到最近的 compact assistant（`role==assistant && summary && finish && !error`），把它和它的 parent user message 一起"标 completed"；
  2. 找到最近 compaction user message 的 `tail_start_id`，从 compaction user 开始**重组顺序**为：`[compaction user, compaction summary assistant, ...tail messages starting at tail_start_id..., continue user]`；
  3. 这种顺序的目的是让模型看到 `[summary]` 紧跟"现状"提示，紧接可见 tail，避免"过时的旧 tool result"被插入到 LLM 输入中。

实际把过滤后的 messages 投给 LLM：`MessageV2.toModelMessagesEffect` 在 `prompt.ts` line 1262 调用；同时在 compaction part 处替换为 `text: "What did we do so far?"`（`message-v2.ts` lines 228-233）。

V2 对应注入（`packages/core/src/session/runner/llm.ts` lines 215-216）：压缩完通过 `Effect.die(continueAfterCompaction(currentStep))` 抛 TurnTransitionError，被外层 catch 后回到 runTurn；下次 runTurn 会重新走 `loadSystemContext()` → 渲染 fresh baseline → `SessionContextEpoch.prepare()` 持久化新 baseline。

### 4.4 Pruning（消息历史降级）

`packages/opencode/src/session/compaction.ts` 的 `prune()`（lines 273-317）：

- 从最新往旧遍历 messages（保留至少 2 轮 user turn 不动）；
- 累计 tool result 输出 tokens；
- 超过 `PRUNE_PROTECT`（40_000）后继续累积的 older tool result 加入 `toPrune`；
- 如果累计 pruned > `PRUNE_MINIMUM`（20_000），才真正执行：通过 `session.updatePart({...part, state: { ...state, time: { ...time, compacted: Date.now() } }})` 标记 `compacted` 时间戳；
- **写入数据库，但 tool output 字符串保留**（prune 只是标记"时间戳"），读取时由 `MessageV2.toModelMessagesEffect` lines 293-296 判断 `part.state.time.compacted` 时输出 `"[Old tool result content cleared]"`（即"软删除"）。

**教训**：prune 是"延迟丢弃" —— 不真的抹除数据（保持审计完整），只在投给 LLM 时换占位符。

### 4.5 Summary Token budget（V2）

`packages/core/src/session/compaction.ts` lines 12-15：
```ts
const DEFAULT_BUFFER = 20_000
const DEFAULT_KEEP_TOKENS = 8_000
const TOOL_OUTPUT_MAX_CHARS = 2_000
const SUMMARY_OUTPUT_TOKENS = 4_096
```

然后 `compactAfterOverflow`（lines 178-230）：用 `select(entries, config.tokens)`（lines 137-158）逆序累积，找到 head/recent 边界；发 `Started` 事件 → stream → 发 `Ended` 事件 → 同时写入 `compaction.summary` 与 `compaction.recent` 到 SessionMessage 持久层。

---

## 5. 消息历史持久化、Pruning 与 Aging

### 5.1 持久化层

**存储**：SQLite WAL（`packages/core/src/database/database.ts` lines 27-33：`journal_mode=WAL`, `synchronous=NORMAL`, `busy_timeout=5000`, `cache_size=-64000`, `foreign_keys=ON`）。

**位置**：`join(Global.Path.data, "opencode.db")`（或 `opencode-{channel}.db`，根据 InstallationChannel）。

**表**（`packages/core/src/session/sql.ts`）：
- `session`：session 元数据（id, project_id, workspace_id, parent_id, slug, directory, path, title, version, share_url, summary_*, tokens_*, cost, metadata, revert, permission, agent, model, time_*）。
- `message`：每条 message（id, session_id, time_*, data json）。**`data` 列存 V1 风格 message**，包含 V1 全部内容。
- `part`：每个 part（id, message_id, session_id, data json）。**`data` 列存 V1 风格 part**。
- `session_message`（V2 event-sourced 表）：seq 序号、type（user/assistant/system/shell/synthetic/compaction/...）、data json。
- `session_input`：durable admission inbox（prompt, delivery, admitted_seq, promoted_seq）。
- `session_context_epoch`：baseline 字符串 + structured snapshot + baseline_seq。
- `todo`：session 内 todo 列表。

**索引**（sql.ts line 79）：`message_session_time_created_id_idx`，使分页查询非常快。

### 5.2 写入路径（事件溯源风格）

**V1**（`packages/opencode/src/session/session.ts`）：
- `updateMessage(msg)`（lines 631-635）：**只发事件** `events.publish(SessionV1.Event.MessageUpdated, { sessionID, info })`，不直写 DB。
- `updatePart(part)`（lines 637-645）：同样**只发事件** `SessionV1.Event.PartUpdated`。
- `getPart(input)`（lines 647-667）：直接从 `PartTable` 读。

写入由 `EventV2Bridge` 翻译成 `session.next.*` 事件，落到 `SessionMessageTable`（event-sourced 表）；read path 在 V1 中是 `MessageV2.page()`（`message-v2.ts` lines 425-467）直接读 `MessageTable`，所以 V1 实际上有"双表并行"。

**V2**（`packages/core/src/session/message-updater.ts`）：
- `update(adapter, event)`（lines 78-395）：是 `SessionEvent.All.match` 的模式匹配表，把 `session.next.*` 事件翻译为对 `MemoryState.messages` 的修改（用 immer produce）。
- `memory(state)` 工厂（lines 19-76）：把 `MemoryState`（含 messages 数组）包装成 `Adapter`（getCurrentAssistant / getAssistant / getCurrentShell / updateAssistant / updateShell / appendMessage）。

### 5.3 Pruning / Aging（显式机制）

OpenCode 的消息"老化"主要由以下机制驱动：

1. **`prune()`**（上面 4.4）：累计超过 `PRUNE_PROTECT` tokens 的更早 tool result 把 `compacted` 时间戳设上；展示时换占位符。
2. **Compaction**：把 head 转成 summary，保留 tail；`filterCompacted()` 重排。
3. **`filterCompactedEffect(sessionID)`**（`message-v2.ts` lines 574-576）：在每次 `runLoop` 开头读消息流时调用；这是把"压缩后的现实"渲染给 LLM 的核心入口。
4. **`title / summary` 删除**：session 删除（`session.ts: remove()`）会沿 `parent_id` 递归删 children、`session.next.deleted` 事件删除、并通过 `events.remove(sessionID)` 清理 V2 EventV2 投影。

**注意**：V1 没有"按 token 主动丢弃"消息，只通过 prune 标记 + compaction 压缩。如果既没到 overflow、也没手动请求 compaction，**整个历史会被原样发回 LLM**，受限于 input window 时才触发。

### 5.4 跨会话记忆

OpenCode **没有 vector RAG 检索式跨 session memory**：
- 历史会话的"项目级指令"通过 `AGENTS.md` 跨 session 生效（因为是文件）；
- 同一项目的 session 列表（`Session.list()`）可被查询，但只是 UI/管理用途，不参与 LLM prompt；
- 跨会话信息共享唯一的"主动渠道"是：① 工作区文件（git/快照），② 共享的 `AGENTS.md`，③ `share_url`（`Session.share`）导出的可分享会话。

**设计动机**：OpenCode 走"显式 + 文件系统优先"的路线，避免隐式 RAG 带来的"我上次说过什么"幻觉风险；用户被引导通过 `AGENTS.md` 这种可读、可 diff、可 git 化的文本显式沉淀知识。

---

## 6. 工具调用历史存储与查询

### 6.1 存储模型

每个 ToolPart：
```ts
{
  id, sessionID, messageID,
  type: "tool",
  callID: ulid(),
  tool: "<tool-name>",
  state: ToolState,
  metadata?: Record<string, any>
}
```

`ToolState` 按 `status` 区分：`pending` / `running` / `completed` / `error`。
`completed` 包含 `output: string`, `title: string`, `metadata: Record`, `attachments?: FilePart[]`, `time: { start, end, compacted? }`。

### 6.2 查询路径

- `session.getPart({sessionID, messageID, partID})`（`session.ts` lines 647-667）：单 part 查询。
- `MessageV2.parts(messageID)`（`message-v2.ts` lines 492-504）：某 message 的所有 part。
- `session.messages({sessionID, limit})`（`session.ts` lines 830-853）：用 `MessageV2.page()` 翻页拉到完整消息流。

### 6.3 Doom Loop 检测

`packages/opencode/src/session/processor.ts` line 29 `DOOM_LOOP_THRESHOLD = 3`：
- 在 `tool-call` 事件中（lines 331-381），取最近 3 个 parts，若全为同 tool、同 input，则触发 `permission.ask({ permission: "doom_loop", ... })`，让用户确认是否继续。
- 这是"防 AI agent 反复用同一个工具循环"的安全网。

### 6.4 工具执行跟踪

- `pending` / `running` 状态的 tool 在 `step-finish` 时（`processor.ts` lines 471-484）会触发 `summary.summarize()` 后台 fork。
- 如果模型又发出了 tool call，处理器从 `readToolCall` 中拿到 part 复用；如果 tool 在 `cleanup()` 时仍未完成（`processor.ts` lines 571-595），会标记 `interrupted: true` 并发到 V1 history。
- tool result 的 attachments（FilePart）支持 `mime` 和 `url`；`MessageV2.toModelMessagesEffect` 在 lines 290-323 根据 provider 是否支持决定保留为 tool-output attachment 还是抽出作为 user message（针对 OpenAI-compatible 只支持 string 的场景）。

---

## 7. 关键 Interface 定义与依赖关系

### 7.1 关键 Effect Schema 与 Service 定义

```ts
// packages/core/src/session/schema.ts
export * as SessionV1 from "./session"

// packages/schema/src/v1/session.ts
export const User = Schema.Struct({ id, role: "user", time, agent, model, ... })
export const Assistant = Schema.Struct({ id, role: "assistant", time, parentID, modelID, summary?, finish?, error?, tokens, cost, structured?, ... })
export const Part = Schema.Union([TextPart, SubtaskPart, ReasoningPart, FilePart, ToolPart, StepStartPart, StepFinishPart, SnapshotPart, PatchPart, AgentPart, RetryPart, CompactionPart])
export const CompactionPart = Schema.Struct({ id, sessionID, messageID, type: "compaction", auto, overflow?, tail_start_id? })
export const SessionInfo = Schema.Struct({ id, slug, projectID, workspaceID?, directory, path?, parentID?, summary?, cost?, tokens?, share?, title, agent?, model?, version, metadata?, time, permission?, revert? })
```

### 7.2 模块依赖关系（V1 视角）

```
SessionPrompt.prompt()
  ├── SystemPrompt.system() ─────┐
  │                              ├─ 拼装 system array
  ├── Instruction.system() ──────┤
  ├── SessionProcessor.process() ─┘
  │     ├── MessageV2.toModelMessagesEffect → filterCompacted
  │     └── LLM.Service.stream()
  ├── SessionCompaction.select / process / prune (overflow/loop 兜底)
  └── title/summary agents (fork, step===1)
```

### 7.3 模块依赖关系（V2 视角）

```
SessionExecution.resume(sessionID)
  → SessionStore.get → LocationServiceMap.get(location)
  → SessionRunner.run({sessionID, force})
    → SessionInput.promote (steer / queue)
    → SessionContextEpoch.initialize / prepare
         → SystemContextRegistry.load()
              → [InstructionContext, SkillGuidance, ReferenceGuidance, env builtins] (concurrent)
         → SystemContext.reconcile(value, snapshot) or replace(value, snapshot)
         → persisted: baseline + snapshot into SessionContextEpochTable
    → SessionHistory.entriesForRunner(db, sessionID, baselineSeq)
         → SessionMessageTable filtered by latestCompaction + baselineSeq
    → SessionCompaction.compactIfNeeded({entries, model, request})
         → maybe compactAfterOverflow()
         → throw TurnTransitionError → reload history → runTurn again
    → llm.stream(request) → publish session.next.* events
  → SessionMessageUpdater.update(adapter, event) → MemoryState.messages → read tool registry, settle tools, persist
  → reload history → runTurn again until needsContinuation == false
```

### 7.4 AGENTS.md 完整数据流（V1）

1. 用户在项目根放一份 `AGENTS.md`；
2. 启动 `SessionPrompt.runLoop`；
3. `instruction.system()` → `systemPaths()` → 在 `<global.config>/AGENTS.md` 与 `<worktree>/.../AGENTS.md` 中找到最近一份；
4. `systemPaths()` 返回路径集合 → 读 → 返回 `["Instructions from: <path>\n<content>"]` 数组；
5. system prompt 拼装时插入 `...env, ...instructions, ...mcpInstructions, ...skills`；
6. 当用户用 `read` 工具读某文件时，触发 `instruction.resolve()`，沿文件路径向 root 回溯，挂载每个还没在该 message 上挂过的 AGENTS.md/CLAUDE.md/CONTEXT.md（标记在 `claims: Map<MessageID, Set<string>>`）；
7. **V2 中**：把 `instruction-context` 注册到 `SystemContextRegistry`，每次 runTurn 由 `loadSystemContext` 重新 observe；ContextEpoch 持久化 baseline + snapshot；当文件改动时输出 chronological System message（diff 风格）。

---

## 8. 架构特点总结

### 8.1 双轨架构（V1 兼容 + V2 重写）

- **V1**（`packages/opencode/src/session/*`）：已稳定，老接口、丰富的 schema、`SessionPrompt` + `SessionProcessor` 编排；
- **V2**（`packages/core/src/session/*`）：Effect-native，正在演化，提供 `SessionRunner.runTurn`、System Context、Context Epoch、`SessionMessageUpdater`；
- 通过 `EventV2Bridge`（`packages/opencode/src/event-v2-bridge.ts`）双向桥接，新事件用 `session.next.*` 命名；
- `MessageTable`（V1）和 `SessionMessageTable`（V2）双表并存，读路径 `MessageV2.page()` 仍走 V1 表。

### 8.2 显式 vs 隐式

- **显式**：AGENTS.md / CLAUDE.md / CONTEXT.md / config.instructions — 全部由用户编辑，文件系统作为 source of truth；
- **隐式**：会话内 compaction 摘要（"模型自己写的"），但每个 summary 都对应一个 durable `session_message` event，可以 inspect；
- **没有 vector store / embedding**；跨 session 检索完全靠"文件系统 + git"。

### 8.3 数据流总图

```
用户输入 prompt（含 @AgentName）
   │
   ▼
SessionPrompt.createUserMessage / loop
   → schema 校验 → EventV2 publish MessageUpdated
   │
   ▼
┌──────────────────────────┬──────────────────────────┐
│                          │                          │
▼                          ▼                          ▼
SystemPrompt         Instruction             SessionCompaction
  provider+env       AGENTS.md/CLAUDE.md      select+prune
  +skills+mcp         .md/CONTEXT.md          process+create
   │                       │                        │
   └─────────────────┬─────┴────────────────────────┘
                     ▼
           system prompt (array)
            + messages (filtered)
            + tools (filtered perms)
                     │
                     ▼
           LLM.stream()  (AI SDK / native runtime)
                     │
                     ▼
           LLMEvent stream
                     │
                     ▼
           SessionProcessor.handle  ← step-start/finish/tool-call/text
                                                  /reasoning/delta ...
           → Event.MessageUpdated → session.next.* events
                     │
                     ▼
           SQLite (WAL)
           MessageTable, PartTable,
           SessionMessageTable,
           SessionContextEpochTable,
           SessionInputTable
```

### 8.4 关键设计动机 / 教训

1. **AGENTS.md 是 first-class**：被独立服务 `Instruction` 管理；每个 model turn 都会重新加载，并在工具调用时按需追加邻近文件。设计动机：**让用户用熟悉的 git 化文本沉淀约定**，而不是靠 agent 自学的"隐式偏好"。
2. **Compaction 是"替换 model representation"而不是"删历史"**：`session_message` 表保留所有原始 turn + summary + recent tail，持久层永远是完整的；model 看到的只是 `filterCompacted()` 重排过的 `WithParts[]`。设计动机：**审计性 + 重放能力**。
3. **ContextEpoch = "system prompt 持久化"**：V2 把"环境 + 指令"做成 typed source，每次 turn reconcile 输出 chronological update；这是 OpenCode 对 Claude cache 命中率优化思路 —— **baseline 稳定就保 cache，context 变就发 minimal diff**。
4. **Prune 是"软标记"**：tool result 不真的删除，只设 `state.time.compacted` 时间戳，LLM 输入时换占位符。设计动机：**保留 audit trail，又能 drop token**。
5. **没有隐式跨会话 RAG**：跨会话信息只能通过文件系统（AGENTS.md、git）和 share_url。设计动机：**避免 agent "我记得你上周说过……"的幻觉**。
6. **Event Sourcing 双轨**：`session.next.*` 事件作为 source-of-truth（eventV2），`MessageTable` 作为 legacy 兼容；`SessionMessageUpdater.update()` 用 immer produce 把事件投影成内存 messages。设计动机：**既要稳定的 V1 行为，又要可演化的事件溯源**。
7. **Doom Loop 检测**：连续 3 次同 tool 同 input 触发 `doom_loop` 权限询问；这是给 coding agent 设计的"现实保险丝"。
8. **Overflow 后的 auto-compaction**：provider 真的返回 context_overflow 时不立即失败，而是尝试一次 compact + 重试一次；只有**再次失败**才放弃。设计动机：**给 agent 一次自愈机会**，但**绝不循环**。

---

## 9. 典型调用序列（一个完整的"用户→模型回复"轮次）

```
[用户] opencode> "add user authentication to my app"
   ↓
[SessionPrompt.prompt(input)]
   ├─ Session.get(sessionID)
   ├─ SessionRevert.cleanup (清除前次 revert 标记)
   ├─ createUserMessage
   │   ├─ agents.get("build") → Agent.Info
   │   ├─ provider.getModel / currentModel
   │   ├─ resolvePromptParts (解析 @AgentName / !`shell` / file:// attachments)
   │   ├─ plugin.trigger("chat.message", ...) → 可修改 info/parts
   │   ├─ schema 校验 → session.updateMessage(info)
   │   └─ session.updatePart(part) × N
   └─ loop({sessionID}) → state.ensureRunning → runLoop
       ↓
[runLoop]
   ├─ step0: MessageV2.filterCompactedEffect(sessionID) → 重排 messages
   ├─ step 1 (title fork): llm.stream(...).textDelta → setTitle
   ├─ step 1 (summary fork): snapshot 找 step-start/step-finish → summary.summarize
   ├─ getModel(lastUser) → Provider.Model
   ├─ check lastUser.parts → tasks.pop() (subtask / compaction / 跳过)
   ├─ if isOverflow(lastFinished.tokens, model) → compaction.create({auto:true}) → continue
   ├─ SessionReminders.apply(msgs, agent, session) (注入 plan/build-switch 提醒)
   ├─ session.updateMessage(assistant placeholder msg)
   ├─ processor.create({assistantMessage, model}) → SessionProcessor.Handle
   ├─ SessionTools.resolve (按 permissions + plugin + provider 过滤工具)
   ├─ sys.skills / sys.environment / instruction.system / sys.mcp (system prompt 拼装)
   ├─ MessageV2.toModelMessagesEffect (投 messages 给 model, 把 compaction part 替换为 "What did we do so far?")
   ├─ plugin.trigger("experimental.chat.messages.transform") → 可改写 msgs
   ├─ processor.process({model, system, messages, tools})
   │   ↓
   │   [LLMEvent stream]
   │   ├─ text-start → session.updatePart(textPart)
   │   ├─ text-delta → session.updatePartDelta(...)
   │   ├─ tool-input-start → ensureToolCall (新建 part.pending)
   │   ├─ tool-call → updateToolCall (set state.running)
   │   ├─ tool-result → completeToolCall (set state.completed + attachments)
   │   ├─ reasoning-start/delta/end → finishReasoning
   │   ├─ step-start → snapshot.track() + session.updatePart(step-start)
   │   ├─ step-finish → snapshot.track() + summary.summarize fork
   │   │           + isOverflow check → ctx.needsCompaction = true
   │   ├─ provider-error → throw
   │   └─ finish →
   ├─ if ctx.needsCompaction → return "compact"
   │       → compaction.create({auto:true, overflow})
   │       → session.updateMessage(user with compaction part)
   │       → continue (下轮 process 走 compaction branch)
   ├─ else return "continue" / "break"
   └─ loop 退出后: compaction.prune({sessionID}) (后台 fork) → 标记旧 tool result compacted
```

---

## 10. 总结

OpenCode 的"记忆管理"是一个**严谨的多层架构**：

1. **最外层：用户可读写的 AGENTS.md / CLAUDE.md / CONTEXT.md** —— 持久化在文件系统，按工作区/project 全局作用；
2. **中间层：System Prompt** —— 由 provider base、env、AGENTS.md、MCP、skills 等拼装而成，每次 turn 都重新加载；
3. **会话层：完整消息历史** —— 全部存储在 SQLite（WAL），通过 MessageTable / SessionMessageTable 双轨持久化；
4. **压缩层：Compaction + Prune** —— 自动 / 手动 / overflow 触发；用 LLM 生成的"滚动 summary"替换 head 部分，用 `compacted` 时间戳软删除旧 tool result；
5. **跨会话层：Context Epoch + share_url** —— V2 引入 ContextEpoch 持久化 privileged system context 的 baseline + snapshot，便于 cache 命中与 chronological update；share_url 用于人工导出分享。

整套设计的核心思想可以浓缩为：**用文件系统做长期记忆，用 SQLite 做短期消息历史，用 schema + Effect Layer 做边界隔离，用 System Context + Context Epoch 做 token 预算管理**。它没有 RAG、没有 embedding、没有隐式记忆 —— 所有的"记忆"都是**显式、可审计、可 diff** 的。

这种设计对软件工程 agent 来说有几个明显好处：
- **与 git 工作流兼容**（AGENTS.md 可以进 commit review）；
- **易于调试**（每个 compaction 都是一个持久化 event）；
- **易于扩展**（plugin hook 可以在 prompt 组装的任意阶段插入）；
- 同时保持**对开发者的低门槛** —— 你只需要会写 Markdown。

---

## 附录：关键源码文件清单（绝对路径）

- `/usr/local/LsmGitOpenSource/opencode/packages/opencode/src/session/instruction.ts`
- `/usr/local/LsmGitOpenSource/opencode/packages/opencode/src/session/system.ts`
- `/usr/local/LsmGitOpenSource/opencode/packages/opencode/src/session/compaction.ts`
- `/usr/local/LsmGitOpenSource/opencode/packages/opencode/src/session/overflow.ts`
- `/usr/local/LsmGitOpenSource/opencode/packages/opencode/src/session/processor.ts`
- `/usr/local/LsmGitOpenSource/opencode/packages/opencode/src/session/prompt.ts`
- `/usr/local/LsmGitOpenSource/opencode/packages/opencode/src/session/message-v2.ts`
- `/usr/local/LsmGitOpenSource/opencode/packages/opencode/src/session/session.ts`
- `/usr/local/LsmGitOpenSource/opencode/packages/opencode/src/session/revert.ts`
- `/usr/local/LsmGitOpenSource/opencode/packages/opencode/src/session/reminders.ts`
- `/usr/local/LsmGitOpenSource/opencode/packages/opencode/src/session/summary.ts`
- `/usr/local/LsmGitOpenSource/opencode/packages/opencode/src/session/todo.ts`
- `/usr/local/LsmGitOpenSource/opencode/packages/opencode/src/session/run-state.ts`
- `/usr/local/LsmGitOpenSource/opencode/packages/opencode/src/session/schema.ts`
- `/usr/local/LsmGitOpenSource/opencode/packages/core/src/session/compaction.ts`
- `/usr/local/LsmGitOpenSource/opencode/packages/core/src/session/context-epoch.ts`
- `/usr/local/LsmGitOpenSource/opencode/packages/core/src/session/history.ts`
- `/usr/local/LsmGitOpenSource/opencode/packages/core/src/session/message-updater.ts`
- `/usr/local/LsmGitOpenSource/opencode/packages/core/src/session/run-coordinator.ts`
- `/usr/local/LsmGitOpenSource/opencode/packages/core/src/session/runner/llm.ts`
- `/usr/local/LsmGitOpenSource/opencode/packages/core/src/session/sql.ts`
- `/usr/local/LsmGitOpenSource/opencode/packages/core/src/instruction-context.ts`
- `/usr/local/LsmGitOpenSource/opencode/packages/core/src/system-context/index.ts`
- `/usr/local/LsmGitOpenSource/opencode/packages/schema/src/v1/session.ts`
