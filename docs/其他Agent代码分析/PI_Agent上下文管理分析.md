# PI Agent 上下文 (Context) 管理分析

> 分析日期: 2026-08-11 | 源码路径: `/usr/local/LsmGitOpenSource/pi/packages/agent/`

## 1. 上下文生命周期

```
┌──────────┐    ┌──────────┐    ┌──────────┐    ┌──────────┐
│ 用户输入  │───▶│ Context  │───▶│ LLM 调用  │───▶│ 工具执行  │
│ Prompt   │    │ 构建     │    │ 流式响应  │    │ 结果回注  │
└──────────┘    └──────────┘    └──────────┘    └────┬─────┘
                                                     │
                    ┌──────────┐    ┌──────────┐     │
                    │ Compaction│◀──│ 检查阈值  │◀────┘
                    │ 压缩     │    │ 决策     │
                    └──────────┘    └──────────┘
```

## 2. AgentContext 结构

```typescript
interface AgentContext {
  messages: AgentMessage[];   // 完整消息历史
  systemPrompt: string;       // 系统提示词
  tools: AgentTool[];         // 可用工具列表
  model: Model;               // 当前模型
  thinkingLevel: ThinkingLevel; // 推理级别
}
```

### 2.1 AgentMessage 类型体系

PI 定义了丰富的消息角色，超越了标准的 user/assistant/toolResult:

| 角色 | 用途 | 对 LLM 可见 |
|---|---|---|
| `user` | 用户输入 | ✅ |
| `assistant` | 模型响应 (含 text/toolCall/thinking) | ✅ |
| `toolResult` | 工具执行结果 | ✅ |
| `bashExecution` | Shell 命令执行 | ❌ (转换为 user) |
| `branchSummary` | 分支切换摘要 | ❌ (转换为 user) |
| `compactionSummary` | 压缩摘要 | ❌ (转换为 user) |
| `custom` | 自定义消息 | ❌ |

**关键**: PI 在 `convertToLlm` 边界将非标准角色转换为 LLM 可理解的格式。

### 2.2 消息边界转换

```typescript
function defaultConvertToLlm(messages: AgentMessage[]): Message[] {
  return messages.filter(
    (m) => m.role === "user" || m.role === "assistant" || m.role === "toolResult"
  );
}
```

非 LLM 消息 (bashExecution, branchSummary, compactionSummary) 在转换时被:
- 过滤掉 (默认)
- 或由 `transformContext` hook 转换为 user 消息

## 3. System Prompt 构建

### 3.1 模块化组合

PI 的 system prompt 由多个独立模块组合:

```
System Prompt = Base Instructions
              + Skills Section (格式化技能列表)
              + Custom Instructions (用户自定义)
              + Environment Context (工作目录、OS、shell)
```

### 3.2 Skills 系统

```typescript
function formatSkillsForSystemPrompt(skills: Skill[]): string {
  // 输出:
  // <available_skills>
  //   <skill>
  //     <name>...</name>
  //     <description>...</description>
  //     <location>...</location>
  //   </skill>
  // </available_skills>
}
```

Skills 是 **按需加载** 的: 只有 `disableModelInvocation !== true` 的 skill 才进入 system prompt。

### 3.3 与狼人杀 Agent 的对比

| 维度 | PI Agent | 狼人杀 Agent |
|---|---|---|
| System prompt 构建 | 模块化组合 (base + skills + env) | 单一函数 `BuildSystemPrompt()` |
| 动态性 | 每轮可变 (`prepareNextTurn` hook) | 静态 (每局构建一次) |
| 工具描述 | 嵌入 system prompt skills 段 | 独立 `tools[]` 数组 |
| 上下文感知 | `prepareNextTurnWithContext` 按轮更新 | GameContext 注入 user prompt |

## 4. Token 预算管理

### 4.1 多级预算

```
contextWindow (模型上限)
├── reserveTokens: 16384 (compaction 预留)
├── keepRecentTokens: 20000 (近端保留)
└── 可用空间 = contextWindow - reserveTokens - keepRecentTokens
```

### 4.2 Provider Usage 追踪

PI 利用 LLM 响应中的 `usage` 字段做精确 token 计数:

```typescript
function calculateContextTokens(usage: Usage): number {
  return usage.totalTokens || usage.input + output + cacheRead + cacheWrite;
}

function estimateContextTokens(messages): ContextUsageEstimate {
  // 1. 找到最后一条 assistant 消息的 usage
  // 2. usageTokens = 上次 LLM 报告的总 token
  // 3. trailingTokens = 之后新增消息的估算 token
  // 4. 总计 = usageTokens + trailingTokens
}
```

### 4.3 Compaction 决策

```typescript
function shouldCompact(contextTokens, contextWindow, settings): boolean {
  return contextTokens > contextWindow - settings.reserveTokens;
}
```

**精确 vs 粗略**: PI 优先使用 provider 报告的精确 usage，fallback 到 chars/4 估算。

### 4.4 与狼人杀 Agent 的对比

| 维度 | PI Agent | 狼人杀 Agent |
|---|---|---|
| Token 计量 | provider usage + chars/4 估算 | byte-based (approxPayloadBytes) |
| 预算模型 | contextWindow - reserve - keepRecent | per-model maxPromptBytes (200-600KB) |
| 压缩触发 | token 阈值 (contextWindow - 16K) | turn 数 (80 turns) + byte 阈值 |
| 压缩策略 | LLM 结构化摘要 (7 段) | 字符串截断 (前 40 chars + tool names) |
| 增量更新 | ✅ (previousSummary + new) | ❌ |
| Per-model 适配 | contextWindow 来自模型元数据 | 硬编码 map (DouBao: 400KB, Kimi: 400KB...) |

## 5. 消息注入机制

### 5.1 Steering Queue (实时注入)

```
用户在 agent 运行中输入新消息
    ↓
agent.steer(message) → PendingMessageQueue
    ↓
下一轮 LLM 调用前 drain() 注入到 context.messages
    ↓
Agent 继续处理，不中断当前工具执行
```

### 5.2 Follow-up Queue (追加任务)

```
Agent 自然停止 (无更多工具调用)
    ↓
检查 followUpQueue.hasItems()
    ↓ (有排队消息)
注入 follow-up 消息，启动新轮次
    ↓
Agent 继续执行追加任务
```

### 5.3 与狼人杀 Agent 的对比

| 维度 | PI Agent | 狼人杀 Agent |
|---|---|---|
| 中途注入 | ✅ steering queue | ❌ 只能在 handleEvent 入口注入 |
| 追加任务 | ✅ follow-up queue | ❌ 无 |
| 注入模式 | "all" / "one-at-a-time" | N/A |
| 清空机制 | clearSteeringQueue() | N/A |

**狼人杀痛点**: 当 agent 正在处理夜间行动时，新的观众消息或阶段变化无法实时注入，只能等待下一次 handleEvent。

## 6. 上下文转换管道详解

### 6.1 三级转换

```
Level 1: AgentMessage[] (运行时)
  ↓ convertToLlm()
Level 2: Message[] (LLM 协议层)
  ↓ transformContext() [可选 hook]
Level 3: Message[] (最终发送)
```

### 6.2 transformContext Hook

```typescript
transformContext?: (messages: AgentMessage[], signal?: AbortSignal) => Promise<AgentMessage[]>;
```

用于:
- 过滤敏感信息
- 注入运行时上下文 (如环境变量)
- 消息重排或合并

### 6.3 与狼人杀 Agent 的对比

| 维度 | PI Agent | 狼人杀 Agent |
|---|---|---|
| 转换层数 | 3 层 (AgentMessage → LLM → transform) | 2 层 (Message → SanitizeMessagesForAnthropic) |
| 转换时机 | 每次 LLM 调用前 | 每次 LLM 调用前 |
| 自定义 hook | ✅ transformContext | ❌ 硬编码 |
| 转换内容 | 过滤/注入/重排 | orphan cleanup + thinking strip + user merge |

## 7. 关键设计模式总结

### 7.1 边界转换 (Boundary Transformation)

PI 在 AgentMessage 和 LLM Message 之间有明确的转换层，使得运行时消息类型可以丰富 (bashExecution, compactionSummary 等) 而 LLM 只看到标准格式。

### 7.2 双队列注入 (Dual Queue Injection)

Steering (实时) + Follow-up (追加) 双队列提供了灵活的消息注入时机，避免了 agent 循环中的"盲区"。

### 7.3 Provider Usage 驱动 (Provider-Driven Budget)

优先使用 LLM 响应报告的精确 token 数，fallback 到字符估算。这比纯 byte-based 估算更准确。

### 7.4 Hooks 管道 (Hooks Pipeline)

`beforeToolCall` / `afterToolCall` / `shouldStopAfterTurn` / `prepareNextTurn` 四个 hook 点覆盖了 agent 循环的所有关键决策点，使得行为可插拔而不修改核心循环。
