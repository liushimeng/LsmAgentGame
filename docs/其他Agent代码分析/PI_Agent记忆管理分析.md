# PI Agent 记忆管理分析

> 分析日期: 2026-08-11 | 源码路径: `/usr/local/LsmGitOpenSource/pi/packages/agent/`

## 1. 架构概览

PI Agent 的记忆管理分为三层:

```
┌─────────────────────────────────────────┐
│  Session Layer (持久化会话)              │
│  session/session.ts + session/memory.ts  │
│  - SessionRepo / SessionStorage 接口     │
│  - InMemory / SQLite 后端                │
│  - Entry tree (消息 + compaction + 分支) │
└──────────────┬──────────────────────────┘
               │
┌──────────────▼──────────────────────────┐
│  Compaction Layer (上下文压缩)           │
│  compaction/compaction.ts               │
│  - LLM 驱动的结构化摘要                  │
│  - 增量更新 (previousSummary + new)      │
│  - 文件操作追踪                          │
└──────────────┬──────────────────────────┘
               │
┌──────────────▼──────────────────────────┐
│  Agent Loop Layer (运行时消息)           │
│  agent-loop.ts + agent.ts               │
│  - AgentMessage[] 消息数组               │
│  - Steering queue (中途注入)             │
│  - Follow-up queue (追加任务)            │
│  - convertToLlm 边界转换                 │
└─────────────────────────────────────────┘
```

## 2. Session 层 — 结构化会话树

### 2.1 核心抽象

```typescript
// session/types.ts
interface SessionStorage {
  appendEntry<T>(entry: ProvisionedEntry<T>, lane: string): Promise<T>;
  findEntries(query: EntryQuery): Promise<Entry[]>;
  findEntriesOnBranch(query: EntryQuery & BranchBounds): Promise<Entry[]>;
  // ... CRUD + 导航
}

interface SessionRepo {
  create(options: SessionCreateOptions): Promise<Session>;
  open(metadata: SessionMetadata): Promise<Session>;
  fork(source: SessionMetadata, options: ForkOptions): Promise<Session>;
  list(): Promise<SessionMetadata[]>;
}
```

### 2.2 Entry 类型体系

PI 使用 **类型化 Entry** 替代简单的消息数组:

| Entry 类型 | 说明 |
|---|---|
| `message` | 单条对话消息 (user/assistant/toolResult/bashExecution) |
| `compaction` | 压缩摘要 + 保留尾部消息 + 文件操作记录 |
| `branch_summary` | 分支跳转时的历史摘要 |
| `thinking_level_change` | thinking 级别切换 |
| `model_change` | 模型切换 |
| `active_tools_change` | 工具集变更 |

**关键设计**: 每个 Entry 都有唯一 ID、parent ID、seq 序号、timestamp，形成 **树状结构** 而非线性数组。这使得:
- 分支 (fork) 天然支持
- 历史导航 (navigate) 不丢失上下文
- compaction 后旧历史仍可追溯

### 2.3 持久化后端

| 后端 | 实现 | 特性 |
|---|---|---|
| `InMemorySessionStorage` | `session/memory.ts` | 开发/测试用，全内存 |
| `SQLite` | `session/jsonl/` | WAL 模式、FTS5 全文搜索、writer-lease fencing (TTL 30s)、branch 查询缓存 |

SQLite 后端使用 8 张表存储会话数据，支持 crash recovery 和并发写入。

## 3. Compaction 层 — LLM 驱动的结构化压缩

### 3.1 触发条件

```typescript
function shouldCompact(contextTokens, contextWindow, settings): boolean {
  return contextTokens > contextWindow - settings.reserveTokens;
}

// 默认配置
const DEFAULT_COMPACTION_SETTINGS = {
  enabled: true,
  reserveTokens: 16384,     // 为摘要 prompt 和输出保留
  keepRecentTokens: 20000,  // 压缩后保留的近端 token 数
};
```

### 3.2 Token 估算

PI 使用 **字符/4** 的保守启发式:

```typescript
function estimateTokens(message: AgentMessage): number {
  // text: chars / 4
  // thinking: chars / 4
  // toolCall: (name.length + args.length) / 4
  // toolResult: content.length / 4
  // image: 4800 chars (固定估算)
  return Math.ceil(chars / 4);
}
```

### 3.3 压缩流程 (5 步)

```
1. prepareCompaction(entries, settings)
   ├─ 查找上一次 compaction 的 previousSummary
   ├─ findCutPoint() — 从末尾累积 token 直到 >= keepRecentTokens
   ├─ 确定 cut point (必须在 turn 边界)
   └─ 分离: messagesToSummarize / turnPrefixMessages / retainedTail

2. extractFileOperations(messages, entries, prevCompactionIndex)
   ├─ 追踪 read/edited 文件集合
   └─ 跨 compaction 累积 (从上一次 compaction 的 details 继承)

3. generateSummary(messages, model, reserveTokens, previousSummary?)
   ├─ 无 previousSummary → 首次全量摘要
   │   System: "You are a context summarization assistant..."
   │   User: <conversation>...</conversation> + SUMMARIZATION_PROMPT
   └─ 有 previousSummary → 增量更新
       User: <conversation>...</conversation>
             <previous-summary>...</previous-summary>
             UPDATE_SUMMARIZATION_PROMPT

4. 摘要格式 (固定 7 段):
   ## Goal
   ## Constraints & Preferences
   ## Progress (Done / In Progress / Blocked)
   ## Key Decisions
   ## Next Steps
   ## Critical Context
   + 文件操作列表 (读/修改)

5. CompactResult:
   - summary: 摘要文本
   - tokensBefore: 压缩前 token 数
   - retainedTail: 保留的近端消息
   - details: { readFiles, modifiedFiles }
```

### 3.4 Split Turn 处理

当 cut point 落在某个 turn 中间时:

```
messagesToSummarize: [turn 1..N-1 的消息]
turnPrefixMessages:  [turn N 的前半部分]
retainedTail:        [turn N 的后半部分 + 最新消息]
```

- turnPrefixMessages 单独用 `TURN_PREFIX_SUMMARIZATION_PROMPT` 压缩
- 最终 summary = historySummary + "\n---\nTurn Context (split turn):" + prefixSummary

### 3.5 与狼人杀 Agent 的对比

| 维度 | PI Agent | 狼人杀 Agent |
|---|---|---|
| 压缩方式 | **LLM 结构化摘要** (7 段格式) | 简单截断 + 字符串拼接 (前 40 chars + tool names + errors) |
| 增量更新 | ✅ previousSummary + new messages | ❌ 每次重新截断 |
| 文件追踪 | ✅ read/edited 集合跨 compaction 累积 | ❌ 无 |
| Split turn | ✅ turn-aware cut point | ❌ 按 turn 数粗暴截断 |
| Token 估算 | 精确 (chars/4 + provider usage) | 粗略 (byte-based) |
| 摘要质量 | 高 (Goal/Progress/Decisions 结构化) | 低 (仅 500 chars / 30 lines) |

## 4. Agent Loop 层 — 运行时消息管理

### 4.1 Steering Queue (中途注入)

```typescript
class PendingMessageQueue {
  enqueue(message: AgentMessage): void;
  drain(): AgentMessage[];  // mode: "all" | "one-at-a-time"
  clear(): void;
}

// Agent 类
agent.steer(message);     // 当前 turn 结束后注入
agent.followUp(message);  // agent 即将停止时追加
```

**运行时行为**:
- 每轮 LLM 调用前检查 `config.getSteeringMessages()`
- steering 消息注入到 context.messages 尾部
- follow-up 消息在 agent 自然停止后触发新轮次

### 4.2 Context 转换管道

```
AgentMessage[] (运行时)
    ↓ convertToLlm()
Message[] (LLM 协议)
    ↓ transformContext() [可选]
Message[] (过滤/增强后)
    ↓ LLM API call
```

`convertToLlm` 在 LLM 调用边界执行，过滤掉 bashExecution/branchSummary/compactionSummary 等非 LLM 消息类型。

### 4.3 Hooks 体系

| Hook | 调用时机 | 返回值影响 |
|---|---|---|
| `beforeToolCall` | 工具执行前 | `{skip, replacement, modifiedArgs}` |
| `afterToolCall` | 工具执行后 | `{modifiedResult}` |
| `shouldStopAfterTurn` | 每轮结束后 | `true` = 停止 agent |
| `prepareNextTurn` | 下一轮开始前 | `{updatedSystemPrompt, updatedTools}` |
| `prepareNextTurnWithContext` | 下一轮开始前 (带 context) | 同上 |

## 5. 关键设计模式总结

### 5.1 结构化摘要 > 简单截断

PI 不是简单丢弃旧消息，而是用 LLM 将历史压缩为 **7 段结构化摘要**:
- Goal: 用户意图
- Progress: 已完成/进行中/阻塞
- Key Decisions: 重要决策
- Next Steps: 下一步
- Critical Context: 关键上下文

### 5.2 增量更新 > 全量重建

有 previousSummary 时使用 UPDATE_SUMMARIZATION_PROMPT，只合并新信息，保留旧摘要中的关键内容。

### 5.3 类型化 Entry 树 > 线性消息数组

Entry 树天然支持分支、导航、压缩，且每种 Entry 类型有独立的序列化/反序列化逻辑。

### 5.4 Hooks 管道 > 硬编码流程

工具执行、上下文转换、停止判定等全部通过 hooks 可插拔，不修改核心循环。
