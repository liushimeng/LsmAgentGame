# OpenClaw 意图识别与任务分解分析

> 分析日期: 2026-08-13
> 源码路径: `/usr/local/LsmGitOpenSource/openclaw`
> 项目定位: 开源个人 AI 助手 —— 多渠道消息接入(Telegram/Discord/Slack 等 40+ 渠道)、自主执行系统与日常任务,TypeScript / pnpm monorepo
> 分析目的: 提取意图识别、任务分解、路由分发、子代理机制的设计模式,为狼人杀 Agent 的事件驱动/工具派发/多 Agent 协作提供对标参考

**总体架构一句话**:OpenClaw 是以 **Gateway 单进程**为核心的「多渠道消息 → 会话路由 → Agent 运行时 → 工具执行」管线。**意图识别基本不依赖独立分类器模型**,而是「规则前置(命令检测/绑定路由/提及门控)+ LLM 主循环自主决策(工具即任务类型)」的双层架构;任务分解交给 LLM 通过 `update_plan` + `sessions_spawn` 工具在运行时自主完成,**子代理(subagent/swarm)与 cron 自动化是其两大招牌机制**。

## 1. 消息入口与意图识别

### 1.1 多渠道消息归一化

- 渠道实现全部是 `extensions/` 下的插件(telegram、discord、slack、whatsapp 等 40+),通过 `src/channels/plugins/` 注册进活跃渠道插件注册表(`src/plugins/runtime.ts`)。
- 归一化核心类型是 **`MsgContext`**(`src/auto-reply/templating.ts`):携带 `SessionKey`/`OriginatingChannel`/`AccountId`/`From`/`To`/`ChatType`(direct/group/thread)/媒体理解结果/`MentionSource` 等统一字段。
- 入站可靠性与幂等由 `src/channels/message/` 负责:`durable-receive.ts`(持久化入站日志,平台事件去重)、`receive.ts`(ack/nack 策略状态机)、`ingress-queue.ts`/`ingress-drain.ts`/`ingress-retry-policy.ts`(入站队列、排空、重试)。

### 1.2 意图识别:规则为主,LLM 仅在两个狭窄场景

OpenClaw **没有通用的「意图分类器」**。意图分流按以下优先级用确定性规则完成:

1. **控制命令检测(规则)** — `src/auto-reply/command-detection.ts`:`hasControlCommand(text, cfg)` 剥离入站元数据后与命令注册表的 `textAliases` 前缀/精确匹配(`/new`、`/reset`、`/status`);`isAbortTrigger` 检测中止指令;`isSessionBoundaryCommandText` 区分会话边界命令。
2. **命令注册表(规则路由表)** — `src/auto-reply/commands-registry.data.ts`:内置命令 + 渠道插件自动生成 `dock:<pluginId>` 切换命令;按插件注册表版本号缓存失效。
3. **原生命令 vs 文本命令裁决** — `commands-text-routing.ts::shouldHandleTextCommands()` 避免双重触发。
4. **群组激活与提及门控(规则)** — `group-activation.ts`(`/activation mention|always`)+ `mention-gating.ts`(`InboundMentionFacts × InboundMentionPolicy → InboundMentionDecision`,支持隐式提及 reply_to_bot/quoted_bot/bot_thread_participant)。

**仅有的两个 LLM 意图分类场景**(都集中在 `src/system-agent/`,即配置/设置向导的系统 Agent):

- **助手规划器** `src/system-agent/assistant.ts`:`planSystemAgentCommand()` 把模糊自然语言经 LLM 转成**一条**安全命令,responseFormat 受 JSON Schema 约束(`{reply, command?}`)。系统 Agent 的操作面是封闭语法(`operations-parse.ts`:config-set/doctor/plugin-install 等),确定性解析优先,LLM 规划器兜底。
- **批准意图分类器** `src/system-agent/approval-intent.ts`:3 分类(approve/decline/other),**封闭词表短路优先**(字面 "yes" 直接命中,不给对话模型 reinterpret 的机会),含糊回复才走独立单次小模型调用(`APPROVAL_INTENT_MAX_TOKENS = 8`,10s 超时,只看用户消息与提案描述,**从不看工具输出**),不可用模型时默认 "other"(安全默认,保持 pending)。注释明确设计动机:"The host — not the conversation model — decides whether a turn is armed, so the agent loop can never self-approve."

**结论**:用户面向的「任务意图」识别 = 规则前置 → 其余全部交给 Agent 主循环的 LLM 自主决策(通过工具选择表达意图),这是与「独立意图分类模型」路线完全不同的设计。

## 2. 任务路由与分发

### 2.1 会话路由:`src/routing/`

- **`resolve-route.ts`** 是路由中枢。`resolveAgentRoute(input)` 输入 `{cfg, channel, accountId, peer, dmScope, guildId, teamId, memberRoleIds}`,输出 `ResolvedAgentRoute`:
  ```ts
  { agentId, channel, accountId, dmScope, sessionKey, mainSessionKey,
    lastRoutePolicy: "main"|"session",
    matchedBy: "binding.peer" | "binding.peer.parent" | "binding.peer.wildcard"
             | "binding.guild+roles" | "binding.guild" | "binding.team"
             | "binding.account" | "binding.channel" | "default" }
  ```
- **路由表 = 配置中的绑定列表(bindings)**:`bindings.ts` 匹配优先级从 peer 精确 → 父 thread → 通配 → guild+成员角色(Discord role 路由)→ guild → team → account → channel → default agent,共 **9 级显式枚举**。
- **会话键即路由结果**:`session-key.ts::buildAgentSessionKey()` 把 `(agentId, channel, accountId, peerKind, peerId, dmScope, identityLinks)` 编码成稳定 sessionKey;DM 有 4 种作用域(`main | per-peer | per-channel-peer | per-account-channel-peer`)控制会话合并粒度;`isSubagentSessionKey / isAcpSessionKey / isCronRunSessionKey` 同族函数判定子代理/cron 会话。

### 2.2 回复分发管线:`src/auto-reply/`

- 顶层编排 `dispatch.ts`,管线拆成 gather → prepare-delivery → prepare-context → prepare-operation → **choose-route** → execute → finalize 的显式阶段。
- 并发/一致性机制:
  - `reply-admission-ticket.ts`:按 `[SessionKey, CommandTargetSessionKey]` 预留入场券(会话级串行化)。
  - `foreground-reply-fence-state.ts`:按复合键做代际(generation)管理,防止旧代回复覆盖新代。
  - `inbound-dedupe.ts` + `inbound-debounce.ts`:入站去重与防抖。
- 会话内消息排队:`agent-session-prompting.ts` —— streaming 中消息经 `steer()`(插队引导)或 `followUp()`(排队跟进)入队。

### 2.3 taxonomy.yaml 的真实身份(重点澄清)

**`taxonomy.yaml`(11,618 行)不是任务分类器/意图分类表**,而是 **QA 成熟度记分卡的覆盖分类学**:

- 顶层 `title: Maturity scorecard`;结构 `profiles`(smoke-ci/personal-agent/observability/release 等测试剖面)+ `levels`(planned/experimental/alpha/beta/stable/clawesome)+ `surfaces`(gateway/cli/plugins/agent-runtime/session-memory/channels/security/automation 等 26 个面)。
- 唯一消费者:`scripts/qa/render-maturity-docs.ts` 与 `extensions/qa-lab/src/scorecard-taxonomy.ts`,用于从覆盖率证据生成成熟度文档。**对运行时任务路由零影响**。

## 3. 任务分解与规划

OpenClaw **没有独立的 planner 服务或 plan-execute 框架**;分解由 LLM 在主循环内通过三个内置工具自主完成:

1. **`update_plan` 工具**(`src/agents/tools/update-plan-tool.ts`)—— todo list 机制:
   - Schema:`{explanation?, plan: [{step, status: "pending"|"in_progress"|"completed"}]}`,**硬约束「最多一个 in_progress」**(校验失败抛 `ToolInputError`)。
   - 实现是**纯展示型**:校验后存进 tool details 供 UI/transcript 消费 —— **计划不驱动执行**,执行仍是 LLM 的下一步工具调用。
2. **目标工具** `goal-tools.ts`:`create_goal / get_goal / update_goal`(会话级 objective,`MODEL_UPDATABLE_SESSION_GOAL_STATUSES` 限制模型可改的状态),持久化在 session store。
3. **`suggest_task` / `dismiss_task`**(`task-suggestion-tools.ts`):模型向操作员**提案**后续工作卡片(title ≤60 字动词开头 / prompt ≤32K 自包含 / tldr ≤1K),**操作员批准后才真正启动会话** —— 「建议-批准」两级分解,防止 Agent 自作主张扩散任务。

**多步任务流(task flow)**:`task-flow-registry.types.ts` 定义 `TaskFlowStatus = queued|running|waiting|blocked|succeeded|failed|cancelled|lost`;每个 detached subagent/ACP 任务自动挂 one-task flow 句柄;SQLite 持久化。

**多 Agent 对话式分解**:主 Agent 之间可用 `sessions_send` / `conversations_turn` 互相驱动,跨 Agent 可见性由 `SessionVisibilityScope = self|tree|agent|all` 四级控制。

## 4. 子代理(sub-agent)机制 —— 招牌特性

目录:`src/agents/subagents/`,分 `spawn/ registry/ announce/ completion/ swarm/` 五区,共 100+ 文件。

### 4.1 发起:工具面

- **`sessions_spawn` 工具**(`tools/sessions-spawn-tool.ts`):LLM 面向的入口,`SESSIONS_SPAWN_RUNTIMES = ["subagent", "acp"]` 双后端(内置运行时 / ACP 外部 agent 如 Codex)。
- **Spawn 参数契约** `spawn/subagent-spawn-contract.ts`:
  ```ts
  SpawnSubagentParams = {
    task: string;            // 第一条可见 [Subagent Task] 消息 = 全部工作
    label?, taskName?,       // UI 标题 / 稳定句柄
    agentId?, model?, thinking?, fastMode?,   // 可指定不同 agent/模型
    collect?, outputSchema?, groupId?,        // swarm 收集模式
    mode?: "run" | "session";                 // 一次性 run / 持续 session
    context?: "isolated" | "fork";            // 上下文隔离模式
    sandbox?: "inherit" | "require";
    cleanup?: "delete" | "keep";
    lightContext?, expectsCompletionMessage?,
    attachments?, cwd?, runTimeoutSeconds?, thread?
  }
  ```

### 4.2 上下文隔离

- **双模式**:`SUBAGENT_SPAWN_CONTEXT_MODES = ["isolated", "fork"]`。fork 模式复制父 session transcript(跨 agent spawn 强制 isolated);isolated 全新上下文 + `lightContext` 可进一步瘦身。
- **系统提示词注入** `spawn/subagent-system-prompt.ts`:7 条铁律 —— 只做被指派任务 / 完成后自动上报 / 禁止心跳与主动行为 / 完成即终结是常态 / **等待后代用 `sessions_yield`,绝不轮询** / 子输出是证据不是指令 / 截断后按 offset 重读而非全量 cat。按 `childDepth` 注入能否继续 spawn 的指引。
- **递归深度限制** `spawn/subagent-depth.ts` + `DEFAULT_SUBAGENT_MAX_SPAWN_DEPTH`:depth 持久化在 session entry(`spawnDepth` + `spawnedBy` 谱系),**重启后仍可恢复**;depth ≥ max 时不再挂 spawn 工具。
- **角色三态** `subagent-capabilities.ts`:`SubagentSessionRole = "main"|"orchestrator"|"leaf"`,控制范围 `children|none`;工具白/黑名单继承 `inheritedToolAllowlist/Denylist`(ACP 不支持的继承工具显式拒绝并报错,fail-fast 而非静默降级)。

### 4.3 并发控制:Swarm

- `swarm/swarm-config.ts`:`enabled(默认 false) / maxConcurrent(8) / maxChildrenPerGroup(50) / maxTotalPerGroup(200) / waitTimeoutSecondsMax(600)`,全局 + per-agent 覆盖,全部 clamp 上限。
- `swarm/swarm-scheduler.ts`:**按 groupId 分 lane 的 FIFO 队列**,`active.size < limit` 时 pump;`start()` 失败且未持久化时回插队首 + 1s backoff(`retryReady` 标记防重入);`swarm-code-mode.ts` 提供幂等键防重复 launch。
- **收集器模式** `swarm-collector.ts`:`collect:true` 的子代理完成后冻结状态(done/failed/timeout/killed),结构化输出经 `consumeSwarmStructuredOutput()` 收集,父代用 `agents_wait` 工具等待整组。

### 4.4 结果回收:Announce(推送式,不是轮询)

核心设计:**子代完成后自动向父代 announce,父代绝不 busy-poll**(系统提示词明文规则)。

- 幂等键 `buildAnnounceIdFromChildRun`;静默 token `SILENT_REPLY_TOKEN`/`NO_REPLY`/`isAnnounceSkip` 处理「无需上报」。
- `waitForSubagentRunOutcome()` + `readLatestSubagentOutputWithRetry()`,输出截断常量(finding ≤4K chars,单条 ≤512),结构化结果经 `wrapPromptDataBlock` **防注入包裹**后进入父代 prompt。
- **`sessions_yield` 工具**:父代需要等待时的唯一合法方式 —— 让出执行,等 runtime 事件唤醒。
- 兜底:`subagents` 工具(list/cancel)只做按需状态查看与取消,按会话树可见性过滤。

### 4.5 生命周期与恢复:Registry

- run 注册、liveness、超时、代际、清扫器(`subagent-registry-sweeper.ts`)。
- **重启恢复**:Gateway 重启后从 session store 恢复 spawn 谱系、检测 wedged run、恢复会话 admission。
- 持久化 `subagent-registry.store.sqlite.ts`(队列化子代理 launch 可跨进程恢复,与 swarm-scheduler 的内存队列互补)。

## 5. 技能/Skills 系统

### 5.1 结构

- 仓库根 `skills/`:40+ 个技能目录,**每个技能 = 一个目录 + `SKILL.md`**(YAML frontmatter + markdown 指令体),可选 `scripts/` 等附属。
- frontmatter 契约(`src/skills/loading/frontmatter.ts`):
  ```yaml
  name: github
  description: "GitHub CLI for issues, PRs, ..."
  metadata: { openclaw: { emoji: "🐙", requires: { bins: ["gh"] },
    install: [{ id, kind: brew|npm|go|uv, formula/bins, label }] } }
  ```
  安装 spec 有严格安全校验(brew formula / npm spec / go module 正则白名单,拒绝 `..`、`-` 前缀、反斜杠 —— 防供应链注入)。

### 5.2 发现、匹配、执行

- **发现** `src/skills/discovery/`:索引 + 过滤(`resolveEffectiveAgentSkillsLimits` per-agent 技能上限)+ 按 `requires.bins` 探测本机可用性 + 技能可注册 `/skill-name` 聊天命令。
- **注入方式 = prompt 内目录,而非工具调用**:`workspace-skill-prompt.ts` 把符合条件的技能渲染进 system prompt 的技能目录段,硬上限 `DEFAULT_MAX_SKILLS_IN_PROMPT = 150` / `DEFAULT_MAX_SKILLS_PROMPT_CHARS = 18_000`,超限自动切 compact 格式(描述截断 220 chars)并插入 `⚠️ Skills truncated: included N of M` **可观测标记**。
- **匹配 = LLM 读目录自选用**:没有检索器/embedding 匹配,完全靠 frontmatter 的 `name + description` 让模型自己决定何时遵循该技能;技能体主要是 CLI 工具用法手册,执行走 Agent 的 exec 工具。
- **生命周期**:clawhub(技能市场)安装/卸载、`skill-tree-digest.ts`(技能树摘要变更检测)+ `skill-change-hook.ts`(变更钩子)。
- 另有 skill workshop —— 模型可自建技能(`createConfiguredSkillWorkshopTool`)。

## 6. 定时任务/自主任务:`src/cron/`(90+ 文件)

### 6.1 调度模型(5 种形态)

1. `{kind:"at", at}` — 一次性
2. `{kind:"every", everyMs, anchorMs}` — 固定间隔
3. `{kind:"cron", expr, tz, staggerMs}` — cron 表达式 + 确定性错峰
4. `{kind:"on-exit", command}` — **事件驱动**:gateway 托管 watcher 进程退出时触发(挂在 ProcessSupervisor 下,不会被 agent turn 的进程树清理杀掉,issue #71662)
5. `{kind:"stream", command[], mode:"line"|"match", match, batchMs, maxBatchBytes}` — 流式源,按行/正则批处理触发

- 会话目标 `CronSessionTarget = "main"|"isolated"|"current"|"session:<key>"`;唤醒策略 `CronWakeMode = "next-heartbeat"|"now"`。
- **投递** `CronDelivery`:`mode: none|announce|webhook`,`completionDestination` 与 `failureDestination` **分离**(失败可单独通知到别处);`CronDeliveryStatus` 与 `CronRunStatus` **执行结果与投递结果分离记账**。

### 6.2 心跳 = 系统持有的 cron job

- 心跳本质上是系统持有的 cron 监控 job(`declarationKey = "heartbeat:<agentId>"`),默认 30 分钟。
- `HEARTBEAT_PROMPT` 要求模型「无事回 `HEARTBEAT_OK`」;`heartbeat_respond` 工具让模型显式声明 `notify:true/false` 决定是否打扰用户。
- `isHeartbeatContentEffectivelyEmpty()` 解析 heartbeat scratch(忽略注释/空 checkbox),空则跳过 API 调用省钱。
- 重要约束文案 `HEARTBEAT_CRON_TASK_GUIDANCE`:「周期任务必须用 automations(cron)工具建,不要在 heartbeat scratch 里编造」—— 把心跳(检查)与 cron(调度)职责切干净,**职责切分写在 prompt 常量里**。

### 6.3 isolated 执行

`src/cron/isolated-agent/run.ts`:独立 cron turn 全流程,结束后 `disposeCronRunContext()` **主动释放大对象**(issue #85019:113k 份 skill prompt 字符串曾因 run context 不释放堆积)。

## 7. 任务状态管理:`src/tasks/`(70+ 文件)

- **统一任务登记处** `task-registry.ts`:
  ```ts
  TaskRuntime = "subagent" | "acp" | "cli" | "cron"
  TaskStatus  = "queued" | "running" | "succeeded" | "failed"
              | "timed_out" | "cancelled" | "lost"
  TaskDeliveryStatus = "pending"|"delivered"|"session_queued"|"failed"
                     | "dismissed"|"parent_missing"|"not_applicable"
  TaskNotifyPolicy   = "done_only" | "state_changes" | "silent"
  ```
  持久化值全部经 `parsePersistedTaskValue` **白名单校验**(非法值直接 throw,不静默吞);`lost` 是一等状态(进程丢失 run 的显式表达)。
- **执行器** `task-executor.ts`:通过各 runtime 执行 TaskRecord 并回写 registry;`cancelDetachedTaskRunById` 取消。
- **状态展示** `task-status.ts`:`TASK_STATUS_DETAIL_MAX_CHARS` 上限 + `sanitizeTaskStatusText()` 清洗错误文本(隐藏 provider/runtime 上下文)后才暴露给模型 —— **错误信息对 LLM 是「消毒过的」**,防止 provider 内部细节泄漏进 prompt。
- **投递独立状态机**:delivery 与执行解耦(parent_missing / session_queued 都是合法终态);进程重启后 reconcilation。

## 8. 值得借鉴的设计决策(12 条)

1. **意图识别「规则前置 + LLM 自主」双层,不建独立分类模型**。控制命令用词表匹配(可测、零延迟),其余全部交给主循环 LLM 通过工具选择表达意图。唯一两个 LLM 分类器都严格限定在配置向导的封闭操作面上,且输出受 JSON Schema 约束。(`command-detection.ts`、`system-agent/assistant.ts`)
2. **高敏决策用「封闭词表短路 + 小模型兜底 + 安全默认值」三段式**。批准分类器先看字面 yes/no,含糊才走 8-token 上限的单次小模型,模型不可用默认 "other"(保持 pending)。注释明确:"host — not the conversation model — decides whether a turn is armed, so the agent loop can never self-approve"。(`approval-intent.ts`)
3. **会话键 = 路由结果的物化**。`buildAgentSessionKey` 把 agent/channel/account/peer/dmScope 编码进稳定字符串,子代理/cron/ACP 会话可用同一函数族判定;spawn 深度与谱系持久化在 session entry,**重启后可恢复路由语义**。(`session-key.ts`、`subagent-depth.ts`)
4. **路由表优先级显式枚举 + 可观测 matchedBy**。9 级匹配每级都是 `matchedBy` 字面量,调试/日志直接可见命中路径;Discord 成员角色可参与路由。(`resolve-route.ts`)
5. **子代理上下文隔离做成一等枚举,且递归有硬深度**。`context: "isolated"|"fork"` + `SUBAGENT_MAX_SPAWN_DEPTH` + 角色三态(main/orchestrator/leaf);工具继承白/黑名单显式登记,不支持的继承工具 fail-fast 报错而非静默降级。(`subagent-spawn.types.ts`、`inherited-tool-deny.ts`)
6. **父子代理通信是「推送式 announce + sessions_yield 让出」,轮询被系统提示词明文禁止**。完成事件带幂等键、静默 token、结构化截断(≤4K)与防注入包裹(`wrapPromptDataBlock`);等待唯一合法姿势是 yield 等 runtime 事件唤醒。这从协议层消灭了 N×轮询放大。(`subagent-announce*.ts`、`subagent-system-prompt.ts` 规则 5)
7. **LLM 产出的计划是「展示型」而非「驱动型」**。`update_plan` 只校验(最多一个 in_progress)并给 UI 消费,执行仍是模型下一步自主选择 —— 计划不绑架执行,避免 plan-execute 框架的「计划腐烂」问题;`suggest_task` 走「提案→人类批准→才启动」的反向安全阀。(`update-plan-tool.ts`、`task-suggestion-tools.ts`)
8. **并发控制按业务键分 lane 而不是全局信号量**。swarm 按 `groupId` 建 lane(limit/active/queue 三元组),失败回插队首 + 1s backoff + `retryReady` 防重入;额度全部 clamp。(`swarm-scheduler.ts`)
9. **cron 调度统一 5 形态(含事件驱动),执行与投递分离记账**。`on-exit`/`stream` 把「事件触发」纳入与定时同一管线;watcher 挂 ProcessSupervisor 而非 agent turn 进程树(#71662);执行 ok 但投递失败是独立状态,失败可路由到独立 `failureDestination`。(`src/cron/types.ts`)
10. **心跳 = 系统持有的 cron job + 显式 notify 决策**。模型必须经 `heartbeat_respond` 声明是否打扰用户;scratch 空则跳过 LLM 调用;心跳职责被 prompt 约束为「检查」,周期任务必须走 automations 工具。(`heartbeat.ts`、`heartbeat-monitor.ts`)
11. **任务登记表把所有持久化枚举做白名单 parse,失败即 throw**;错误文本经 `sanitizeTaskStatusText` 消毒+限长后才给模型看,防止 provider 内部细节泄漏进 prompt。(`task-registry.types.ts`、`task-status.ts`)
12. **技能系统的「廉价匹配」路线:frontmatter 目录进 prompt,LLM 自选,执行复用 exec**。没有 embedding 检索,靠 `name+description` + 150 条/18K 字符硬上限 + compact 降级 + 截断可观测标记;技能安装 spec 用正则白名单防供应链注入。(`workspace-skill-prompt.ts`、`frontmatter.ts`)

## 9. 关键文件速查表

| 主题 | 路径 |
|---|---|
| 消息归一化 | `src/auto-reply/templating.ts`、`src/channels/message/{receive,durable-receive,ingress-queue}.ts` |
| 命令/意图规则 | `src/auto-reply/command-detection.ts`、`commands-registry.data.ts`、`commands-text-routing.ts`、`group-activation.ts`、`src/channels/mention-gating.ts` |
| LLM 意图(仅系统 Agent) | `src/system-agent/assistant.ts`、`approval-intent.ts`、`operations-parse.ts` |
| 路由 | `src/routing/resolve-route.ts`、`bindings.ts`、`session-key.ts` |
| 分发管线 | `src/auto-reply/dispatch.ts`、`reply/dispatch-from-config*.ts`、`reply-admission-ticket.ts`、`foreground-reply-fence-state.ts` |
| 任务分解 | `src/agents/tools/update-plan-tool.ts`、`goal-tools.ts`、`task-suggestion-tools.ts`、`src/tasks/task-flow-registry*.ts` |
| 子代理 | `src/agents/subagents/{spawn,registry,announce,completion,swarm}/`、`src/agents/tools/{sessions-spawn-tool,sessions-yield-tool,subagents-tool}.ts` |
| 技能 | `skills/<name>/SKILL.md`、`src/skills/{discovery,loading,lifecycle,runtime}/` |
| 定时/自主 | `src/cron/`(types.ts、service.ts、isolated-agent/run.ts、heartbeat-monitor.ts)、`src/auto-reply/heartbeat.ts`、`src/agents/tools/cron-tool.ts` |
| 任务状态 | `src/tasks/task-registry*.ts`、`task-executor.ts`、`task-status.ts` |
| taxonomy.yaml 真相 | `scripts/qa/render-maturity-docs.ts`、`extensions/qa-lab/src/scorecard-taxonomy.ts`(QA 成熟度记分卡,**非任务分类**) |
