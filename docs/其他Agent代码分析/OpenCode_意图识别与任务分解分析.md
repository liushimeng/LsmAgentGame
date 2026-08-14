# OpenCode Agent 意图识别、任务分解、规划/执行分离 — 深度分析

> **分析对象**：`/usr/local/LsmGitOpenSource/opencode`（TypeScript / Effect-TS / Bun，单仓库 monorepo，主包 `packages/opencode`）
> **分析维度**：意图识别 → 任务分解 → 任务分类 → 规划/执行分离 → 子 Agent 派发 → TodoList 管理 → 全链路自动化
> **报告体例**：代码路径 → 实现细节 → 设计动机 / 教训
> **作者注**：本报告基于 2026-08-14 阅读 OpenCode 主分支源码。报告不含「建议本仓库狼人杀 Agent 学什么」 —— 那部分见《狼人杀Agent_根据_OpenCode_优化和解决方案_20260814.md》。

---

## 0. 顶层架构鸟瞰

OpenCode 是一个 **Effect-TS** 化非常彻底的 AI Agent runtime。整个进程按 service / layer / deps 的依赖图装配，所有状态（agent、tool、session、permission、todo、skill…）都是 `Context.Service`，通过 `LayerNode.make({ service, layer, deps })` 在 Node 入口拼装。

**关键源码路径**：
- `packages/opencode/src/agent/agent.ts` —— agent 注册表 + 7 个内建 agent 定义
- `packages/opencode/src/session/prompt.ts` —— agent 循环（runLoop）的实现
- `packages/opencode/src/session/processor.ts` —— 单个 assistant 消息的流式处理（含 doom loop 检测、tool 派发）
- `packages/opencode/src/tool/registry.ts` —— tool 注册表（14+ 内建工具 + 插件工具）
- `packages/opencode/src/tool/task.ts` —— **子 agent 派发器**
- `packages/opencode/src/tool/plan.ts` —— **Plan 退出工具**
- `packages/opencode/src/tool/todo.ts` —— TodoWrite 工具
- `packages/opencode/src/session/todo.ts` —— Todo 数据库 service
- `packages/opencode/src/session/reminders.ts` —— Plan ↔ Build 切换的 system reminder 注入
- `packages/opencode/src/session/compaction.ts` —— 上下文压缩
- `packages/opencode/src/agent/prompt/explore.txt` —— explore 子 agent 的系统 prompt
- `packages/opencode/src/agent/generate.txt` —— **「agent 生成 agent」的元 agent prompt**
- `packages/opencode/src/agent/subagent-permissions.ts` —— 子 agent 权限推导

---

## 1. 意图识别 (Intent Classification)

### 1.1 OpenCode 是否做了 LLM 级的「先分类再执行」？

**代码路径**：`packages/opencode/src/agent/agent.ts:64-80`、`packages/opencode/src/agent/generate.txt`

**实现细节**：
OpenCode **没有像 Claude Code 那样在前置 prompt 里显式做一次 "intent classification → route to sub-agent" 的 LLM 级分类**。它的「分类」是 **声明式的、配置驱动的**：

```ts
const agents: Record<string, Info> = {
  build: { mode: "primary", native: true, ... },     // 默认执行者
  plan: { mode: "primary", native: true, ... },      // 规划模式
  general: { mode: "subagent", native: true, ... },  // 通用并行子 agent
  explore: { mode: "subagent", native: true, ... },  // 只读搜索子 agent
  compaction / title / summary: hidden utility agents
}
```

每个 agent 的 `description` 字段是给主 LLM 看的「触发条件」 —— LLM 自己读 description，决定要不要派 sub-agent（`prompt.ts` 第 1226 行 `SessionTools.resolve` → registry 的 `describeTask` 会拼出 task tool 的 description，把所有 sub-agent 列表追加到 task 工具描述里）：

```ts
const describeTask = Effect.fn("ToolRegistry.describeTask")(function* (agent) {
  const items = (yield* agents.list()).filter((item) => item.mode !== "primary")
  ...
  return ["Available agent types and the tools they have access to:", description].join("\n")
})
```

也就是说：**「意图分类」是隐式地通过 LLM 看 tool description + sub-agent description 自主决策**。但 plan vs build 的分流是显式的：
1. 用户用 TUI 的 `shift+tab` 或 CLI `--plan` 切换 primary agent
2. 由 `SessionReminders.apply`（`reminders.ts`）根据当前 session 的 agent 名动态注入系统级 reminder

### 1.2 「生成 agent」的元 agent —— `Agent.generate`

**代码路径**：`agent.ts:368-435`、`generate.txt`

**实现细节**：
这是 OpenCode 里**唯一一次显式调用 `generateObject` / `streamObject` 做结构化输出**：

```ts
const params = {
  temperature: 0.3,
  messages: [{
    role: "user",
    content: `Create an agent configuration based on this request: "${input.description}".
    IMPORTANT: The following identifiers already exist and must NOT be used: ${existing.map(...)}`,
  }],
  schema: Schema.toStandardSchemaV1(GeneratedAgent),  // {identifier, whenToUse, systemPrompt}
}
```

它让 LLM 严格输出一个 `identifier / whenToUse / systemPrompt` 三元组，并强制避开已存在的 agent 名。`temperature: 0.3` 是为了控制发散。

### 1.3 设计动机 / 教训

| 决策 | 动机 / 教训 |
| --- | --- |
| 放弃 LLM 级 intent classifier | 维护成本高、泛化差。LLM 已经在 tool description 上下文里能完成同等判断 |
| `mode: "subagent"` 作为一阶语义 | 让 primary / subagent 在 dispatch 时就能直接报错 |
| 给 generate 专门用结构化输出（schema） | 创建 agent 是低频、强契约场景，比"prompt 里写 JSON"鲁棒得多 |
| OpenAI OAuth 走 `streamObject`，否则走 `generateObject` | 不同 provider 的结构化输出行为不一致，runtime 兜底 |

---

## 2. 任务分解 (Task Decomposition / Planning)

### 2.1 分解机制不是 LLM 单步决策，而是 **LLM + tool + system reminder** 的多轮循环

**代码路径**：`session/prompt.ts:1081-1341`（`runLoop`）、`session/processor.ts`（流式处理）

**实现细节**：
`runLoop` 是一个 `while (true)` 的循环，每一轮：

```ts
while (true) {
  yield* status.set(sessionID, { type: "busy" })
  let msgs = yield* MessageV2.filterCompactedEffect(sessionID)
  const { user: lastUser, assistant: lastAssistant, finished: lastFinished, tasks } = MessageV2.latest(msgs)

  if (lastAssistant?.finish && !["tool-calls"].includes(...) && !hasToolCalls) break

  step++
  if (step === 1) yield* title({...}).pipe(Effect.forkIn(scope))

  const model = yield* getModel(...)
  const task = tasks.pop()
  if (task?.type === "subtask") {
    yield* handleSubtask({...})
    continue
  }
  if (task?.type === "compaction") { ... continue }

  if (lastFinished && (yield* compaction.isOverflow(...))) {
    yield* compaction.create({...})
    continue
  }

  const result = yield* handle.process({ user, agent, tools, ... })
  if (result === "stop") break
}
```

关键点：
- **分解发生在 LLM 的思考过程中**，通过 tools 暴露 `task`（派 sub-agent）、`todowrite`（维护 todo list）、`plan_enter` / `plan_exit`（plan mode 切换）等实现。
- **每一步都是原子 commit 到 DB**，即使中途崩溃也能 replay。
- **maxSteps 兜底**：`prompt.ts:1178-1179` 读 `agent.steps`，最后一步注入 `MAX_STEPS_PROMPT`，强制让 LLM 收敛。

### 2.2 Plan Mode 是 Plan/Execute 分离的主载体

**代码路径**：
- `tool/plan-enter.txt`、`tool/plan-exit.txt`
- `tool/plan.ts`（**只有 `plan_exit` 这个 tool 实际注册了**）
- `session/prompt/plan-mode.txt`、`session/prompt/plan.txt`、`session/prompt/build-switch.txt`
- `session/reminders.ts`
- `session/session.ts:331-336`

**实现细节**：

```ts
export function plan(input, instance) {
  const base = instance.project.vcs
    ? path.join(instance.worktree, ".opencode", "plans")
    : path.join(Global.Path.data, "plans")
  return path.join(base, [input.time.created, input.slug].join("-") + ".md")
}
```

Plan 文件落地在：
- git 仓库内：`<worktree>/.opencode/plans/<timestamp>-<slug>.md`
- 非 git：`<XDG data>/plans/<timestamp>-<slug>.md`

**plan agent 的权限矩阵**（`agent.ts:156-181`）：
```ts
plan: {
  permission: Permission.merge(
    defaults,
    {
      question: "allow",
      plan_exit: "allow",
      task: { general: "deny" },                              // ← plan agent 不能派 general sub-agent
      external_directory: {
        [path.join(Global.Path.data, "plans", "*")]: "allow",
      },
      edit: {
        "*": "deny",                                           // ← 禁止一切 edit
        [path.join(".opencode", "plans", "*.md")]: "allow",    // ← 唯一允许写 plan 文件
        [path.relative(worktree, path.join(Global.Path.data, "plans", "*.md"))]: "allow",
      },
    },
    user,
  ),
}
```

**plan_exit 的实现**（`tool/plan.ts`）：
```ts
const answers = yield* question.ask({
  questions: [{
    question: `Plan at ${plan} is complete. Would you like to switch to the build agent and start implementing?`,
    options: [
      { label: "Yes", description: "Switch to build agent..." },
      { label: "No",  description: "Stay with plan agent..." },
    ],
  }],
})
if (answers[0]?.[0] === "No") yield* new Question.RejectedError()

// 关键：往同一个 session 注入「synthetic user message」
const msg: SessionV1.User = {
  ...,
  agent: "build",    // ← agent 切换的核心
  model,
}
yield* session.updateMessage(msg)
yield* session.updatePart({
  type: "text",
  text: `The plan at ${plan} has been approved, you can now edit files. Execute the plan`,
  synthetic: true,
})
```

**这是非常巧妙的设计**：plan agent 不是另一个 session；plan/build **共用同一个 session**，只通过「注入 synthetic user message，user message 携带 agent 字段」来实现切换。

**reminder 注入**（`reminders.ts`）：
- 旧路径（非 `experimentalPlanMode`）：
  - 检测 `assistantMessage?.info.agent === "plan"` 且当前 agent 是 build → 注入 `BUILD_SWITCH` reminder
  - 检测 agent 是 plan → 注入 `PROMPT_PLAN`
- 新路径（`experimentalPlanMode`）：
  - 从 plan 文件存在与否动态拼接 `PLAN_MODE.replace("${planInfo}", ...)`（`reminders.ts:81`）

### 2.3 Plan mode 的 5 阶段流程（`plan-mode.txt`）

1. **Phase 1 - Initial Understanding**：只能用 `explore` sub-agent（最多 3 个并行）读代码
2. **Phase 2 - Design**：用 `general` agent 出方案
3. **Phase 3 - Review**：自己读关键文件、问用户澄清
4. **Phase 4 - Final Plan**：写入 plan 文件
5. **Phase 5 - Exit**：调 `plan_exit`

### 2.4 设计动机 / 教训

| 决策 | 动机 / 教训 |
| --- | --- |
| Plan 与 Build 共用同一 session | 避免来回切换上下文；同一份 message history |
| Plan agent 把 `edit: "*": deny` 落到 permissions 层 | 不依赖 LLM 自律，安全边界在 system 层 |
| plan 文件 = 单一可编辑文件 | 把 plan 文档化、可审计、可分享 |
| 5 阶段 SOP 写死在 prompt | 让 sub-agent 调度也有「仪式感」 |
| 用 `task.txt` 禁止 LLM 把 `task` 工具用在已知单文件查询 | 反例教育，把"什么时候不该派 sub-agent"显式说清楚 |

---

## 3. 子 Agent 派发 (Sub-agent Dispatch)

### 3.1 核心入口：`tool/task.ts`

**实现细节（`task.ts:81-348`）**：

```ts
export const TaskTool = Tool.define("task", Effect.gen(function* () {
  const run = Effect.fn("TaskTool.execute")(function* (params, ctx) {
    // 1. 校验 subagent_depth
    const parent = yield* sessions.get(ctx.sessionID)
    let depth = 0
    while (current.parentID) { depth++; current = yield* sessions.get(current.parentID) }
    if (depth >= (cfg.subagent_depth ?? 1)) {
      return yield* Effect.fail(new Error(`Subagent depth limit reached...`))
    }

    // 2. 权限弹窗
    if (!ctx.extra?.bypassAgentCheck) {
      yield* ctx.ask({ permission: id, patterns: [params.subagent_type], always: ["*"] })
    }

    // 3. 取出 subagent 配置
    const next = yield* agent.get(params.subagent_type)
    if (!next) return yield* Effect.fail(new Error(`Unknown agent type: ...`))

    // 4. 复用 session 还是开新 session
    const session = params.task_id ? yield* sessions.get(...).pipe(Effect.catchCause(...)) : undefined

    // 5. 推导子 session 的 permission
    const childPermission = deriveSubagentSessionPermission({
      parentSessionPermission: parent.permission ?? [],
      subagent: next,
    })
    const childToolDenies = [
      ...(next.permission.some(r => r.permission === "todowrite") ? [] : [{ todowrite: "deny" }]),
      ...(next.permission.some(r => r.permission === "task")      ? [] : [{ task:      "deny" }]),
      ...(cfg.experimental?.primary_tools?.map(p => ({ [p]: "deny" })) ?? []),
    ]

    // 6. 创建子 session
    const nextSession = session ?? yield* sessions.create({
      parentID: ctx.sessionID,
      title: `${params.description} (@${next.name} subagent)`,
      agent: next.name,
      permission: [...childPermission, ...childToolDenies.filter(...)],
    })

    // 7. 注入到 background job 系统
    const info = yield* background.start({
      id: nextSession.id, type: id, title: params.description, metadata,
      onPromote: Effect.all([ctx.metadata(...), notify(nextSession.id)]),
      run: runTask().pipe(Effect.onInterrupt(() => ops.cancel(nextSession.id))),
    })
  }))
}))
```

关键设计点：
- **`subagent_depth`**：默认 1 层。这是深度限制 + 子 session 不能再 spawn 的硬约束。
- **`childPermission` 推导**（`subagent-permissions.ts`）：
  ```ts
  // 继承父 session 的 deny 规则 + external_directory 规则
  // 子 agent 自己的 permission 决定其能力
  // 默认 deny todowrite + task（除非子 agent 自己允许）
  ```
- **`task_id` 用于 resume**：传 `task_id` 时复用旧 sub-session，可继续之前的消息历史。
- **background 模型**：`OPENCODE_EXPERIMENTAL_BACKGROUND_SUBAGENTS=true` 时 `background: true`，sub-agent 异步运行、结束后把结果作为 synthetic text 注入父 session。
- **abort 链路**：`ctx.abort` + `runCancel` + `background.cancel` 三层联动。

### 3.2 prompt.ts 的 `handleSubtask` —— SubtaskPart 触发子 agent

`prompt.ts:255-449` 解释了 `SubtaskPart`（来自 command 系统，比如 `/init`、`/review` 触发）如何走 task tool 的同一条路径：

```ts
const handleSubtask = Effect.fn(...)(function* ({ task, model, lastUser, ... }) {
  const taskArgs = { prompt: task.prompt, description: task.description, subagent_type: task.agent, command: task.command }
  const result = yield* taskTool.execute(taskArgs, {
    ...
    extra: { bypassAgentCheck: true, promptOps },
  })
  // 如果是 command 触发的，结尾注入 "Summarize the task tool output above..."
})
```

### 3.3 explore vs general vs plan vs build

| Agent | mode | 工具 | 用途 |
|---|---|---|---|
| `build` | primary | 默认全部 edit 类工具 + `task` / `plan_enter` / `question` / `todowrite` | 默认执行 |
| `plan` | primary | **所有 edit deny** + `task(general: deny)` + `plan_exit` / `question` | 只读探索 + 写 plan 文件 |
| `general` | subagent | 全套工具但 `todowrite: deny` | **并行多任务执行** |
| `explore` | subagent | 只有 `grep / glob / list / bash / webfetch / websearch / read`，其他 deny | **只读代码搜索** |

`explore.txt` 的核心约束：
```
You are a file search specialist...
- Use Glob for broad file pattern matching
- Use Grep for searching file contents with regex
- Use Read when you know the specific file path you need to read
- Use Bash for file operations like copying, moving, or listing directory contents
- ...return file paths as absolute paths... avoid emojis... do not create any files
```

`general` 的 description（`agent.ts:184`）：
```
General-purpose agent for researching complex questions and executing multi-step tasks.
Use this agent to execute multiple units of work in parallel.
```

### 3.4 设计动机 / 教训

| 决策 | 动机 / 教训 |
| --- | --- |
| sub-agent 创建独立 session（parentID 链表） | 隔离 token context、独立 compaction、独立 cancel |
| `task_id` 复用旧 session | 支持「继续之前 sub-agent 的工作」 |
| 默认禁止 sub-agent 再派 sub-agent | 防止 token 爆炸 + 递归权限漏洞 |
| explore agent 的 prompt 完全套用 Claude Code 的 sub-agent 模板 | OpenCode 直接借鉴 Claude Code 的 prompt 模式 |
| `childToolDenies.filter(...)` 二次去重 | 防止父权限 + 子权限重复声明导致 merge 冲突 |
| `bypassAgentCheck` 标志 | 让 SubtaskPart 走命令系统时不必二次弹权限对话框 |

---

## 4. TodoList 管理

### 4.1 工具层：`tool/todo.ts`（TodoWriteTool）

```ts
export const TodoWriteTool = Tool.define("todowrite", Effect.gen(function* () {
  const todo = yield* Todo.Service
  return {
    description: DESCRIPTION_WRITE,   // todowrite.txt
    parameters: Parameters,           // { todos: [{content, status, priority}] }
    execute: (params, ctx) => Effect.gen(function* {
      yield* ctx.ask({ permission: "todowrite", patterns: ["*"], always: ["*"] })
      yield* todo.update({ sessionID: ctx.sessionID, todos: params.todos })
      return { title: `${filter(x => x.status !== "completed").length} todos`, output: JSON.stringify(todos, null, 2), metadata: { todos } }
    }),
  }
}))
```

**`todowrite.txt` 的设计哲学**（非常关键）：
- 状态机：`pending → in_progress → completed/cancelled`，**exactly ONE in_progress at a time**
- 触发条件：3+ 步骤、用户给多条任务、显式 ask for todo list
- 反例：单行 trivial、纯问答、单条命令
- **Mark completed only after work is actually done, including verification** —— 反"intent-based completion"
- 部分完成 → 保持 `in_progress`，添加 follow-up 描述 blocker

### 4.2 数据层：`session/todo.ts`（Todo service）

```ts
const update = Effect.fn(...)(function* ({ sessionID, todos }) {
  yield* db.transaction(tx =>
    Effect.gen(function* () {
      yield* tx.delete(TodoTable).where(eq(TodoTable.session_id, sessionID)).run()  // ← 先清空
      if (todos.length === 0) return
      yield* tx.insert(TodoTable).values(todos.map((todo, position) => ({
        session_id, content, status, priority, position,
      }))).run()
    })
  ).pipe(Effect.orDie)
  yield* events.publish(Event.Updated, input)
})
```

特点：
- **整个 todo list 整体替换**（delete + insert），不存增量
- `position` 列保证排序，`get` 时按 `position asc` 还原
- 通过 `EventV2Bridge.publish(Event.Updated, ...)` 推事件给前端/TUI

### 4.3 sub-agent 默认禁用 todowrite

在 `subagent-permissions.ts`：
```ts
const canTodo = input.subagent.permission.some(rule => rule.permission === "todowrite")
return [
  ...input.parentSessionPermission.filter(...),
  ...(canTodo ? [] : [{ permission: "todowrite", pattern: "*", action: "deny" }]),
  ...
]
```

外加 `general` agent 的 permission 也显式 deny `todowrite`：
```ts
general: {
  permission: Permission.merge(defaults, Permission.fromConfig({ todowrite: "deny" }), user),
  mode: "subagent",
}
```

**这是为了避免「主 agent 的 todo list 被 sub-agent 偷偷修改」** —— todo list 应当只反映主 agent 自己规划的状态。

### 4.4 设计动机 / 教训

| 决策 | 动机 / 教训 |
| --- | --- |
| 状态机三态 + exactly one in_progress | 与 Anthropic 的 TodoWrite 工具设计保持一致，LLM 训练数据里见过 |
| 整体替换而非增量 | DB schema 简单、前端无需做 diff/event sourcing |
| sub-agent 强制 deny todowrite | 保持 todo list 作为「主 agent 视角」的单一真相 |
| `priority` 字段保留但 prompt 里没强调 | 给将来 LLM 自动 reorder 留口子 |
| `mark completed only after verification` | 防止「我假设我跑过了所以完成」的幻觉 |

---

## 5. 工具注册 / 路由 (Tool Registry)

### 5.1 代码路径：`tool/registry.ts:86-249`

**实现细节**：
`ToolRegistry.state` 在 `InstanceState.make` 里构建一次：
- 内建工具 14 个：`invalid / shell / read / glob / grep / edit / write / task / fetch / todo / search / skill / patch / question / lsp / plan`
- 自定义工具：扫 `<configDir>/tool/*.{js,ts}`（plugin 风格）+ `plugin.list()` 的 `tool` 字段
- `codeMode`（experimentalCodeMode）下额外加 `execute` 工具
- `experimentalLspTool` flag 决定是否暴露 `lsp` 工具
- **`experimentalPlanMode && flags.client === "cli"` 才暴露 `plan` 工具**（plan_exit）

然后在 `tools(model)` 里做最终过滤（`registry.ts:286-335`）：
- `webSearchTool` 只对 opencode provider 或开了 exa/parallel 才返回
- `apply_patch` vs `edit` / `write` 的二选一：根据 `modelID.includes("gpt-")` 决定
- 每个 tool 的 description 在返回前会被 `plugin.trigger("tool.definition", ...)` 注入修改机会

### 5.2 tool 的统一签名：`Tool.define`

每个 tool 都是 `Tool.define<Parameters, Metadata, Service>(id, genFn)`，返回 `{ id, description, parameters, execute, ... }`。

`execute` 收到 `Tool.Context`：
- `sessionID / messageID / callID / agent / abort` —— 元数据
- `extra.model` —— 工具可见的当前 model
- `extra.bypassAgentCheck / extra.promptOps` —— 内部 sub-task 调用机制
- `messages` —— 当前 history
- `ask(req)` —— 弹权限
- `metadata({title, metadata})` —— 推送 tool 调用元信息

### 5.3 设计动机 / 教训

| 决策 | 动机 / 教训 |
| --- | --- |
| 工具按 `id` 字符串注册 + 单例 state | 避免 hot-reload 时工具重复 |
| 插件 vs 内建统一进 `builtin` 列表 | 让 LLM 看到的工具集合不区分来源 |
| GPT 用 apply_patch、Claude 用 edit/write | provider-specific 行为差异应该 hide 在 registry 而不是散到调用方 |
| `experimentalPlanMode && client === "cli"` 才注册 plan tool | plan_exit 是只在 CLI 才需要的弹窗工具 |

---

## 6. 全链路工程自动化：build / test / debug / commit

### 6.1 OpenCode **没有内置 "自动 build → test → commit" 的硬编码流程**

它的策略是：

1. **Agent 不主动 commit** —— 多处 prompt 显式禁止 git mutations：
   ```ts
   // session/prompt/kimi.txt:45
   "DO NOT run `git commit`, `git push`, `git reset`, `git rebase`...
   Ask for confirmation each time when you need to do git mutations..."
   ```
2. **`/init` 命令（`initialize.txt`）** —— 让 LLM 主动写出 `AGENTS.md` 来**告诉未来的 agent**「本仓库的 build / test / lint / typecheck 命令是 xxx」。
3. **`/review` 命令（`review.txt`）** —— 内建的 code review SOP。

### 6.2 内建命令系统：`command/template/*.txt`

- `command/template/initialize.txt`：让 agent 调研 build/test/lint/typecheck 配置，写入 `AGENTS.md`
- `command/template/review.txt`：code review SOP

**这两个命令都通过 `prompt.ts` 的 `command()` 入口触发**（`prompt.ts:1356-1481`），并通过 `isSubtask` 决定是以 sub-agent 方式还是直接 inline 跑：

```ts
const isSubtask = (agent.mode === "subagent" && cmd.subtask !== false) || cmd.subtask === true
const parts = isSubtask
  ? [{ type: "subtask", agent: agent.name, description: cmd.description ?? "", command: input.command, prompt: ... }]
  : [...uniqueTemplateParts, ...(input.parts ?? [])]
```

### 6.3 SessionLoop 的"自动重试 / 自动 compact"

虽然没有「自动 commit」，但 runLoop 里有几个**自动恢复机制**：

- **Context overflow** → 自动 compaction（`compaction.isOverflow`，`prompt.ts:1164-1168`）
- **`result === "compact"`** → 触发 compaction（`prompt.ts:1320-1328`）
- **Doom loop detection**（`processor.ts:29, 356-373`）：
  ```ts
  const DOOM_LOOP_THRESHOLD = 3
  // 如果最近 3 个 part 都是同一 tool、同一 input，则触发 "doom_loop" 权限询问
  yield* permission.ask({ permission: "doom_loop", patterns: [value.name], ... })
  ```
- **Auto title**（`prompt.ts:193-253`）—— 第一次 step fork 一个子任务用 `title` agent 生成对话标题
- **Auto summary**（`session/summary.ts`）—— 同样在第一次 step fork

### 6.4 设计动机 / 教训

| 决策 | 动机 / 教训 |
| --- | --- |
| 不内建 commit 流程 | commit 是不可逆的破坏性操作，必须经用户显式同意 |
| 把工程知识外推给 `/init` + `AGENTS.md` | 让用户 repo 成为「system prompt 的扩展」 |
| Doom loop 阈值 = 3 | 3 次同样的错误是「真的有 bug」的概率很高 |
| sub-task 时把 "Summarize and continue" 作为 synthetic user msg 注入 | 让主 LLM 把 sub-agent 的输出"内化" |
| /review 用 git diff + gh pr view 而非内建 | 利用 shell 调用而不是再做一套 git 抽象 |

---

## 7. Agent Loop / Processor 详解

### 7.1 `session/processor.ts:98-...` Handle 接口

```ts
export interface Handle {
  readonly message: SessionV1.Assistant
  readonly updateToolCall: (toolCallID, update) => Effect.Effect<...>
  readonly completeToolCall: (toolCallID, output) => Effect.Effect<void>
  readonly process: (streamInput: LLM.StreamInput) => Effect.Effect<Result>  // "compact" | "stop" | "continue"
}
```

`processor.create` 流程：
1. snapshot 当前文件系统状态（`snapshot.track()`），用于 revert
2. 初始化 `ProcessorContext = { toolcalls, blocked, needsCompaction, currentText, reasoningMap }`
3. 暴露 `updateToolCall / completeToolCall` 供 `session/tools.ts:67` 在 AI SDK 流式回调中调用
4. `process()` 启动 `llm.stream(...)` 并消费 `LLMEvent` 流，把每个 event 转成 `Part` 落库

### 7.2 runLoop ↔ processor ↔ tools.ts 的调用链

```
runLoop (prompt.ts:1081)
  └─ processor.create (processor.ts:98)
       └─ processor.process (streamInput)        // LLM streaming
            └─ llm.stream (llm.ts:357)
                 └─ streamText (AI SDK)
                      └─ tool.execute (每个 tool)
                           └─ session/tools.ts:99 tool({ execute }) wrap
                                └─ input.processor.updateToolCall / completeToolCall
                                     └─ session.updatePart
            └─ 处理 "compaction" / "stop" / "continue"
  └─ loop until assistant finish && !hasToolCalls
```

---

## 8. 关键 Interface 与子 Agent 系统设计

### 8.1 Agent.Info（`agent.ts:35-56`）

```ts
export const Info = Schema.Struct({
  name, description,
  mode: Schema.Literals(["subagent", "primary", "all"]),
  native?, hidden?, topP?, temperature?, color?,
  permission: PermissionV1.Ruleset,           // ← 一阶概念
  model?: { modelID, providerID },
  variant?, prompt?, options?, steps?,
})
```

`mode` 是 OpenCode agent 系统最核心的字段：
- `primary` —— 用户可直接选的 agent（出现在 TUI agent 切换器里）
- `subagent` —— 只能通过 `task` 工具派发
- `all` —— 两者皆可

### 8.2 Permission System（`@opencode-ai/core/v1/permission`）

Permission 是 `Ruleset = Rule[]`，每条 rule `{ permission: string, pattern: string, action: "allow" | "ask" | "deny" }`。`Permission.merge` + `Permission.evaluate`（wildcard match）实现「父权限 + 子权限 + 用户配置」三层合并。

每个 agent 自己持有一份 ruleset；sub-agent 派生时再走 `subagent-permissions.ts` 做一次过滤。

### 8.3 Session 树

`Session.Info.parentID` 形成 session 树：
- 顶层 session：用户与 build/plan 主 agent
- 子 session：通过 task 工具派出的 sub-agent
- `Session.plan()`（`session.ts:331`）算 plan 文件路径
- `cancelBackgroundJobs`（`run-state.ts:111-143`）：取消 session 时递归找所有 child session/job

### 8.4 Todo、Compaction、Revert 三件套

- **Todo** —— `todo.ts`：替换式整体更新
- **Compaction** —— `compaction.ts`：用 LLM 把 history 摘要 + 保留最近 N token
- **Revert** —— `revert.ts`：基于 snapshot（FS 层）+ DB message 删除

这三个 service 都在 `prompt.ts` runLoop 的不同分支被触发。

---

## 9. 整体调用关系图

```
User Prompt (CLI/TUI)
    │
    ▼
SessionPrompt.prompt (prompt.ts:1052)
    │
    ├─► createUserMessage (resolve @file / @agent 引用)
    │
    └─► SessionRunState.ensureRunning → runLoop (while true)
            │
            ├─► SessionTools.resolve → ToolRegistry.tools
            │       ├─ builtin (16)
            │       ├─ plugin tools
            │       └─ describeTask (动态列出 sub-agents)
            │
            ├─► SessionPrompt.handleSubtask (SubtaskPart)
            │       └─ TaskTool.execute → BackgroundJob
            │           ├─ sub-session create
            │           ├─ deriveSubagentSessionPermission
            │           └─ 复用/创建 session, run prompt()
            │
            ├─► SessionProcessor.create / process
            │       └─ LLM.stream → AI SDK streamText
            │             └─ tool.execute (读权限 ask → 改文件/DB)
            │
            ├─► SessionReminders.apply (plan/build switch 注入)
            │
            ├─► SessionCompaction.process (overflow)
            │
            └─► title/summary agents (fork, step===1)
```

---

## 10. 关键设计哲学总结

| 主题 | OpenCode 的做法 | 设计动机 |
|---|---|---|
| 意图分类 | 隐式（LLM 读 tool/sub-agent description 自主路由） + 显式（agent.generate 结构化输出生成新 agent） | 维护成本低，让 LLM 自决；只在低频创建场景用 schema |
| 任务分解 | LLM 在循环中用 `task` / `todowrite` / `plan_enter` 工具主动拆 | 把分解责任留给最擅长分解的 LLM |
| Plan/Execute 分离 | 同一 session，agent 字段切换；权限层 deny all edit；plan 文件落盘 | 共享 history；plan 文档化可审计；安全边界在 permission 而非 prompt |
| 子 agent 派发 | 独立 session（parentID 树） + 独立 permission 派生 + depth 限制 + 复用 task_id resume | 隔离上下文、可独立 cancel、独立 compaction |
| TodoList | 状态机 3 态 + 整体替换 + sub-agent 默认 deny todowrite | 主 agent 视角的单一真相；防止 sub-agent 篡改 |
| 工程自动化 | 不内建 commit/build 流程；外推给 `/init` 写 `AGENTS.md`；通过 shell 调用 git/gh | commit 不可逆必须用户同意 |
| Doom loop | 阈值 3，弹 doom_loop 权限 | 3 次相同 = 大概率 bug |
| 安全 | Permission = 一阶概念，default deny *，子 session 拒绝 todowrite/task 除非显式允许 | 不依赖 LLM 自律 |
| 工具注册 | 内建 + 插件 + config dir 扫描三路合一 | 工具集按 runtime flag 动态裁剪 |
| Sub-agent 类型 | `mode: subagent/primary/all` 一阶字段 | dispatch 时即可校验 |

---

## 11. 我认为特别值得借鉴的细节

1. **`plan_exit` 不开新 session，只往同一 session 注入 synthetic user message**（`plan.ts:53-69`）—— 把「状态切换」做成「消息驱动」而非「会话切换」，比传统 fork 更轻量。
2. **plan agent 默认 deny `task(general)`**（`agent.ts:165-167`）—— 防止 plan agent 偷偷调 general agent 干活。
3. **`subagent_depth` 默认 = 1**（`task.ts:111`）—— 简单粗暴地防止 sub-agent 爆炸。
4. **`deriveSubagentSessionPermission` 默认 deny todowrite 和 task**（`subagent-permissions.ts`）—— todo list 锁定为主 agent 的真相。
5. **Doom loop 检测放在 `processor.ts`** 而非工具层 —— doom loop 是"跨 tool call 的模式"，必须在 stream 维度检测。
6. **GPT-4 之前用 edit/write，GPT-4 之后用 apply_patch**（`registry.ts:292-295`）—— provider 工具差异藏在 registry 不外泄。
7. **`/init` 命令让 LLM 自己写 AGENTS.md**（`initialize.txt`）—— 把工程知识沉淀到 repo。
8. **`/review` 命令明确写出「Use Explore agent to find existing patterns」**（`review.txt:86`）。
9. **`Permission.merge` 接受任意多 ruleset**（`agent.ts` 内多次出现）—— 让父权限 + 默认权限 + 用户权限可叠加而不冲突。
10. **`EXPERIMENTAL_BACKGROUND_SUBAGENTS` 环境变量开关** —— 让 sub-agent 后台化这一高风险特性有 release valve。

---

## 12. 局限与可改进点（基于代码观察）

| 局限 | 表现 | 建议 |
|---|---|---|
| 没有意图分类 | LLM 自己看 description 决定路由，可能误派 sub-agent | 显式 `intent_router` agent 做轻量分类 |
| plan agent 没有「只读 build 验证」环节 | plan 写完直接交用户审批，没法「dry-run」build | 加 `plan_dry_run` 工具 |
| Doom loop 阈值硬编码 3 | 不同任务容忍度不同 | 接受 agent config override |
| TodoList 没有 due time / 阻塞关系 | 无法表达依赖图 | position 已经有序，加 `dependsOn: ID[]` 即可 |
| Sub-agent 的输出完全靠 final text | 中间过程不可见 | `BackgroundJob.onPromote` 已经在做，可以扩到所有 sub-agent |
| `plan_enter` 工具只在 prompt.txt 描述，没有真实实现 | registry.ts 只注册 plan_exit | 或去掉 prompt 里的误导，或真的实现 plan_enter |
| `cfg.subagent_depth ?? 1` 决定后无法让某 agent 无限递归 | 总是单层 | 做成 per-agent depth，或基于 token usage 动态 |

---

## 13. 关键源码文件清单（绝对路径）

- `/usr/local/LsmGitOpenSource/opencode/packages/opencode/src/agent/agent.ts`
- `/usr/local/LsmGitOpenSource/opencode/packages/opencode/src/agent/subagent-permissions.ts`
- `/usr/local/LsmGitOpenSource/opencode/packages/opencode/src/agent/generate.txt`
- `/usr/local/LsmGitOpenSource/opencode/packages/opencode/src/agent/prompt/explore.txt`
- `/usr/local/LsmGitOpenSource/opencode/packages/opencode/src/agent/prompt/compaction.txt`
- `/usr/local/LsmGitOpenSource/opencode/packages/opencode/src/agent/prompt/title.txt`
- `/usr/local/LsmGitOpenSource/opencode/packages/opencode/src/agent/prompt/summary.txt`
- `/usr/local/LsmGitOpenSource/opencode/packages/opencode/src/session/prompt.ts`
- `/usr/local/LsmGitOpenSource/opencode/packages/opencode/src/session/processor.ts`
- `/usr/local/LsmGitOpenSource/opencode/packages/opencode/src/session/llm.ts`
- `/usr/local/LsmGitOpenSource/opencode/packages/opencode/src/session/llm/request.ts`
- `/usr/local/LsmGitOpenSource/opencode/packages/opencode/src/session/todo.ts`
- `/usr/local/LsmGitOpenSource/opencode/packages/opencode/src/session/run-state.ts`
- `/usr/local/LsmGitOpenSource/opencode/packages/opencode/src/session/reminders.ts`
- `/usr/local/LsmGitOpenSource/opencode/packages/opencode/src/session/system.ts`
- `/usr/local/LsmGitOpenSource/opencode/packages/opencode/src/session/tools.ts`
- `/usr/local/LsmGitOpenSource/opencode/packages/opencode/src/session/session.ts`
- `/usr/local/LsmGitOpenSource/opencode/packages/opencode/src/session/compaction.ts`
- `/usr/local/LsmGitOpenSource/opencode/packages/opencode/src/session/prompt/plan.txt`
- `/usr/local/LsmGitOpenSource/opencode/packages/opencode/src/session/prompt/plan-mode.txt`
- `/usr/local/LsmGitOpenSource/opencode/packages/opencode/src/session/prompt/build-switch.txt`
- `/usr/local/LsmGitOpenSource/opencode/packages/opencode/src/tool/registry.ts`
- `/usr/local/LsmGitOpenSource/opencode/packages/opencode/src/tool/task.ts`
- `/usr/local/LsmGitOpenSource/opencode/packages/opencode/src/tool/task.txt`
- `/usr/local/LsmGitOpenSource/opencode/packages/opencode/src/tool/plan.ts`
- `/usr/local/LsmGitOpenSource/opencode/packages/opencode/src/tool/plan-enter.txt`
- `/usr/local/LsmGitOpenSource/opencode/packages/opencode/src/tool/plan-exit.txt`
- `/usr/local/LsmGitOpenSource/opencode/packages/opencode/src/tool/todo.ts`
- `/usr/local/LsmGitOpenSource/opencode/packages/opencode/src/tool/todowrite.txt`
- `/usr/local/LsmGitOpenSource/opencode/packages/opencode/src/tool/question.ts`
- `/usr/local/LsmGitOpenSource/opencode/packages/opencode/src/tool/shell/prompt.ts`
- `/usr/local/LsmGitOpenSource/opencode/packages/opencode/src/effect/runtime-flags.ts`
- `/usr/local/LsmGitOpenSource/opencode/packages/opencode/src/command/template/initialize.txt`
- `/usr/local/LsmGitOpenSource/opencode/packages/opencode/src/command/template/review.txt`
- `/usr/local/LsmGitOpenSource/opencode/packages/schema/src/v1/session.ts`（SubtaskPart / AgentPart schema）
- `/usr/local/LsmGitOpenSource/opencode/.opencode/agent/triage.md`（一个真实的 custom agent 定义示例）

---

## 14. 一句话总结

OpenCode 的 agent 系统**不是一个 intent classifier 加一个 dispatcher**；它是一个**「权限优先 + plan/build 共享 session + 工具即协议 + sub-agent 通过权限派生实现隔离」**的 runtime。

- **意图分类**：LLM 自决（看 sub-agent description + tool description），仅在「生成 agent」这种低频强契约场景用结构化输出。
- **任务分解**：不预设分解算法，靠 LLM 在 agent loop 中通过 `task` / `todowrite` / `plan_enter` / `plan_exit` 工具主动拆，runLoop 兜底（compaction / maxSteps / doom_loop）。
- **Plan/Execute 分离**：同一 session 通过 agent 字段切换；plan agent 把 `edit: *` 都 deny 掉；plan 文件落盘可审计；plan_exit 注入 synthetic user message 触发 build 切换。
- **子 Agent 派发**：独立 session（parentID 树）+ 派生 permission + subagent_depth 限制 + BackgroundJob 异步化 + task_id 复用机制。
- **TodoList**：3 状态机 + 整体替换 + sub-agent 默认 deny todowrite。
- **工程自动化**：把工程知识外推到 `/init` → `AGENTS.md`，不内建 commit 流程；Doom loop 用阈值 3 兜底；Context overflow 自动 compaction。
