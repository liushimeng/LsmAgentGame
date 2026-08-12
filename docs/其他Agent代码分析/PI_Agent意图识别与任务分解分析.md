# PI Agent 意图识别与任务分解分析

> 分析日期: 2026-08-11 | 源码路径: `/usr/local/LsmGitOpenSource/pi/packages/agent/`

## 1. 架构概览

PI Agent 的任务处理分为四个层次:

```
┌─────────────────────────────────────────────┐
│  Intent Layer (意图识别)                     │
│  - Skills 系统: 按任务描述匹配专用指令       │
│  - Prompt Templates: 预定义提示词模板        │
│  - Steering Messages: 运行时意图修正         │
└──────────────┬──────────────────────────────┘
               │
┌──────────────▼──────────────────────────────┐
│  Planning Layer (任务规划)                   │
│  - Agent Loop 内置多轮工具调用               │
│  - shouldStopAfterTurn: 自主决定是否继续     │
│  - Follow-up Queue: 追加未完成任务           │
└──────────────┬──────────────────────────────┘
               │
┌──────────────▼──────────────────────────────┐
│  Execution Layer (任务执行)                  │
│  - Tool Dispatch: 并行/串行工具执行          │
│  - beforeToolCall/afterToolCall Hooks        │
│  - File Mutation Queue: 文件操作队列         │
└──────────────┬──────────────────────────────┘
               │
┌──────────────▼──────────────────────────────┐
│  Reflection Layer (反思与修正)               │
│  - Post-Run Pipeline: 自动重试/压缩/跟进     │
│  - Auto-compaction: 上下文溢出时自动压缩     │
│  - Error Recovery: 瞬态错误自动重试          │
└─────────────────────────────────────────────┘
```

## 2. 意图识别机制

### 2.1 Skills 系统

PI 使用文件系统上的 `.pi/skills/` 目录存放专用技能指令:

```markdown
# .pi/skills/add-llm-provider.md
当用户要求添加新的 LLM Provider 时:
1. 读取配置文件结构
2. 添加 provider 配置
3. 更新类型定义
4. 运行测试
```

**意图匹配方式**: LLM 根据 `<available_skills>` 中的 description 自主判断何时加载哪个 skill。这是 **LLM 驱动的意图识别**，不是规则匹配。

### 2.2 Prompt Templates

PI 预定义了多个提示词模板 (`.pi/prompts/`):

| 模板 | 用途 |
|---|---|
| `pr.md` | Pull Request 审查 |
| `wr.md` | Code Review |
| `sa.md` | Security Audit |
| `cl.md` | Changelog 生成 |
| `is.md` | Issue 分析 |

### 2.3 Steering Messages (运行时意图修正)

```typescript
agent.steer(message);  // 注入到下一轮 LLM 调用前
```

允许在 agent 运行中 **修正意图**:
- 用户输入新指令
- 环境变化触发新目标
- 中途追加约束条件

### 2.4 与狼人杀 Agent 的对比

| 维度 | PI Agent | 狼人杀 Agent |
|---|---|---|
| 意图来源 | 用户自然语言 | 游戏阶段 + 事件类型 |
| 意图识别 | LLM 自主 (skills description 匹配) | 硬编码 (switch phase) |
| 运行时修正 | ✅ steering queue | ❌ |
| 追加任务 | ✅ follow-up queue | ❌ |
| 技能系统 | ✅ 文件系统 .pi/skills/ | ❌ (内嵌在 tools.go switch) |

## 3. 任务分解机制

### 3.1 Agent Loop 内置分解

PI 的 agent loop 本身就是一个 **自动任务分解器**:

```
用户: "给项目添加 dark mode 支持"
    ↓
Agent (LLM): 分析项目结构 → 读取相关文件 → 识别需要修改的组件
    ↓ tool_use: Read("src/styles/theme.ts")
    ↓ tool_use: Glob("src/components/**/*.tsx")
    ↓ tool_use: Read("src/App.tsx")
    ↓
Agent (LLM): 决定修改方案 → 逐步执行
    ↓ tool_use: Edit("src/styles/theme.ts", ...)
    ↓ tool_use: Write("src/styles/dark-theme.ts", ...)
    ↓ tool_use: Edit("src/App.tsx", ...)
    ↓
Agent (LLM): shouldStopAfterTurn? → false (需要测试)
    ↓ tool_use: Bash("npm test")
    ↓
Agent (LLM): shouldStopAfterTurn? → true (任务完成)
```

**关键**: 任务分解完全由 LLM 驱动，agent loop 只提供:
- 工具执行框架
- 停止判定 (`shouldStopAfterTurn`)
- 消息注入 (steering/follow-up)

### 3.2 多轮工具调用

PI 的 inner loop 支持 **连续工具调用**:

```typescript
while (hasMoreToolCalls || pendingMessages.length > 0) {
  // 1. 注入 pending messages
  // 2. 调用 LLM
  // 3. 解析 assistant response 中的 tool calls
  // 4. 执行工具 (parallel/sequential)
  // 5. 注入 tool results
  // 6. 检查是否还有更多 tool calls
}
```

### 3.3 Tool Execution Mode

```typescript
type ToolExecutionMode = "parallel" | "sequential";

// 默认 parallel: 多个 tool call 同时执行
// sequential: 按顺序执行，前一个结果影响后一个
```

### 3.4 与狼人杀 Agent 的对比

| 维度 | PI Agent | 狼人杀 Agent |
|---|---|---|
| 分解方式 | LLM 自主分解 | 阶段驱动 (switch phase → tools) |
| 多轮调用 | ✅ inner loop 连续 | ✅ inner loop (maxInnerRounds=5) |
| 并行执行 | ✅ parallel mode | ❌ 串行 |
| 停止判定 | `shouldStopAfterTurn` hook | `actionDone` flag + maxRounds |
| 追加任务 | follow-up queue | ❌ |

## 4. 工具定义与调度

### 4.1 Tool 定义接口

```typescript
interface AgentTool<T = any> {
  name: string;
  description: string;
  parameters: JSONSchema;
  execute: (args: T, signal?: AbortSignal) => Promise<AgentToolResult>;
  renderCall?: (args: T) => ReactNode;   // TUI 渲染
  renderResult?: (result: AgentToolResult) => ReactNode;
  operations?: ToolOperations;           // 可插拔 I/O 操作
}
```

### 4.2 Pluggable Operations

每个工具暴露一个 `*Operations` 接口:

```typescript
interface BashOperations {
  execute(command: string, options: BashOptions): Promise<BashResult>;
}
interface EditOperations {
  applyEdit(file: string, edit: EditSpec): Promise<void>;
}
```

**默认**: 本地文件系统操作。**可替换**: SSH 远程、容器内执行等。

### 4.3 File Mutation Queue

```typescript
// file-mutation-queue.ts
class FileMutationQueue {
  enqueue(mutation: FileMutation): void;
  flush(): Promise<void>;           // 批量写入
  rollback(): Promise<void>;        // 回滚
  getPending(): FileMutation[];     // 查看待写入
}
```

文件操作被排队而非立即执行，支持:
- 批量提交 (减少 I/O)
- 失败回滚
- 冲突检测

### 4.4 与狼人杀 Agent 的对比

| 维度 | PI Agent | 狼人杀 Agent |
|---|---|---|
| 工具定义 | `AgentTool` 接口 (name+desc+params+execute) | `ToolDef` (name+desc+params) + `DispatchTool` switch |
| 工具注册 | 动态 (Agent.state.tools = [...]) | 静态 (`BuildTools(phase, role, seat, alive)`) |
| 扩展机制 | `operations` 接口可替换 I/O | `ToolRegistry` + `ToolSpec` + `MountIf` guard |
| 操作队列 | ✅ FileMutationQueue | ❌ 直接执行 |
| 渲染 | ✅ renderCall/renderResult | ❌ (前端独立渲染 BotTranscript) |

## 5. 反思与修正机制

### 5.1 Post-Run Pipeline

PI 在 agent 运行结束后执行三个检查:

```
AgentRun 完成
    ↓
1. Auto-Retry: 瞬态错误自动重试
    ↓
2. Auto-Compaction: 上下文超阈值自动压缩
    ↓
3. Queued Follow-ups: 有排队的追加任务则继续
```

### 5.2 错误恢复策略

| 错误类型 | PI 处理 | 狼人杀处理 |
|---|---|---|
| 瞬态网络错误 | Auto-retry (provider retry policy) | Linear backoff 2/4/6/8/8s |
| 上下文溢出 | Auto-compaction + 重试 | PruneByBytesAggressive() + 重试 |
| 工具执行失败 | 返回 error result 给 LLM | 返回 error result 给 LLM |
| 持续失败 | 无 quarantine (单用户场景) | quarantine + auto-skip + manager rescue |

### 5.3 与狼人杀 Agent 的对比

| 维度 | PI Agent | 狼人杀 Agent |
|---|---|---|
| 自动重试 | ✅ provider retry policy | ✅ linear backoff (5 次) |
| 自动压缩 | ✅ post-run auto-compaction | ✅ post-LLM CompressAndPrune |
| 跟进任务 | ✅ follow-up queue | ❌ |
| 隔离机制 | ❌ (单用户) | ✅ quarantine (per-bot) |
| 自救机制 | ❌ (用户手动 steer) | ✅ scheduleReWake + speak_floor |

## 6. 关键设计模式总结

### 6.1 LLM 驱动的意图识别

PI 不用规则引擎判断意图，而是将任务描述放在 skills 中，让 LLM 自主判断何时使用哪个技能。这比硬编码 switch-case 更灵活，但依赖 LLM 的理解能力。

### 6.2 自主任务分解

任务分解完全委托给 LLM: 它读取上下文 → 选择工具 → 执行 → 检查结果 → 决定下一步。Agent loop 只提供执行框架和停止条件。

### 6.3 Post-Run 三步管道

运行结束后自动: 重试 → 压缩 → 跟进。这确保了:
- 瞬态错误不会中断任务
- 上下文不会溢出
- 未完成的任务自动继续

### 6.4 Pluggable Operations

工具的 I/O 操作可替换 (本地/远程/容器)，使得同一个工具定义可以在不同环境运行。这对狼人杀 Agent 的多环境测试有参考价值。

## 7. 对狼人杀 Agent 的启示

| PI 模式 | 狼人杀适用性 | 优先级 |
|---|---|---|
| Skills 系统 | 阶段特定指令可以抽为独立 skill 文件 | ★★☆ |
| Steering Queue | 游戏中途事件注入 (观众消息、阶段变化) | ★★★ |
| Follow-up Queue | 夜间多行动串联 (守卫→狼人→预言家) | ★★☆ |
| Tool Hooks | 道具使用前校验、发言后记录 | ★★★ |
| Pluggable Operations | AgentRunner 已有 ToolRunner 接口，类似 | ★☆☆ |
| File Mutation Queue | 不适用 (游戏状态非文件) | ☆☆☆ |
| Post-Run Pipeline | 已有 (quarantine + reWake)，可增强 | ★★☆ |
