# JiuwenSwarm Context 管理分析

> 分析日期: 2026-08-13
> 源码路径: `/usr/local/LsmGitOpenSource/jiuwenswarm`
> 分析目的: 提取 Context 管理设计模式,为狼人杀 Agent prompt 预算优化提供参考

## 1. 架构总览

JiuwenSwarm 的 Context 管理采用**多处理器流水线架构**:

```
用户输入 ──► [消息收集] ──► [上下文引擎] ──► [LLM 调用]
                              │
                    ┌─────────┼─────────┐
                    ↓         ↓         ↓
              ┌─────────┐ ┌────────┐ ┌──────────┐
              │消息摘要  │ │对话压缩│ │轮次级压缩 │
              │卸载器    │ │器      │ │器        │
              └─────────┘ └────────┘ └──────────┘
                    │         │         │
                    └─────────┼─────────┘
                              ↓
                    ┌──────────────────┐
                    │  合并后的上下文   │
                    │  (token 预算内)   │
                    └──────────────────┘
```

## 2. 四级压缩处理器

### 2.1 消息摘要卸载器 (Message Summary Offloader)

**触发条件**: token 数 > 20000 或大消息 > 1000 token
**处理对象**: 工具调用返回结果(`tool` 类型)
**策略**: 将冗长的工具结果替换为摘要 + `[[OFFLOAD:...]]` 占位符

```
原始: Tool Response: { 2000 行 JSON 数据... }
压缩: Tool Response: 查询完成,返回用户列表(200条记录)
      [[OFFLOAD: user_list_query_result]]
```

**关键特性**:
- `keep_last_round: true` — 最新一轮对话不压缩
- `messages_to_keep` — 可配置保留最新 N 条
- `offload_message_type: ["tool"]` — 只压缩工具结果

### 2.2 对话压缩器 (Dialogue Compressor)

**触发条件**: token 数 > 10000
**处理对象**: 整个对话历史
**策略**: LLM 生成对话摘要,替换旧消息

**配置参数**:
- `compression_token_limit: 2000` — 摘要上限 2000 token
- `keep_last_round: true` — 保留最新一轮
- 支持自定义压缩提示词

### 2.3 当前轮次压缩器 (Current Round Compressor)

**触发条件**: 当前轮次 token > 10000
**处理对象**: 当前轮次内的多条消息
**策略**: 压缩当前轮次中的冗余内容

**特殊参数**:
- `large_message_threshold: 1000` — 大消息阈值
- `single_multi_compression: false` — 整块压缩模式

### 2.4 轮次级压缩器 (Round Level Compressor)

**触发条件**: 连续同级别对话轮次 > 10 轮
**处理对象**: 多轮对话的高级压缩
**策略**: 识别重复模式,折叠相似轮次

### 2.5 循环压缩 (Reasoning Tool Loop Compact)

**触发条件**: 连续相同工具调用 > 3 次 或 相同参数 > 5 次
**策略**: 折叠重复的推理-工具循环

```yaml
reasoning_tool_loop_compact_config:
  consecutive_threshold: 3          # 连续相同工具 3 次触发
  tool_args_consecutive_threshold: 5 # 相同参数 5 次触发
  reasoning_min_chars: 4             # 推理文本最小长度
  reasoning_preview_max_chars: 512   # 预览最大字符数
  bailout_threshold: 3               # 退出阈值
```

## 3. 上下文组装流程

### 3.1 System Prompt 构建

```
System Prompt = 固定规则段
              + 角色技能说明
              + 博弈框架
              + 系统硬约束
              + 工具清单
              + 可选:模型自画像
              + 可选:人设参数
              + 可选:难度指令
```

**缓存优化**: 固定部分保持不变,利用 Anthropic prompt cache 前缀命中。

### 3.2 User Prompt 组装

```
User Prompt = 游戏状态快照(GameContext)
            + 近期发言(RecentSpeeches)
            + 私聊收件箱(WhisperInbox)
            + 聊天历史(ChatHistory)
            + 道具状态(PropSnapshot)
            + 经济信息(WalletBalance/EconTier)
            + 影响力分数(InfluenceScores)
            + 假说表(HypothesisTable)
            + 承诺系统(MyCommitments)
            + 行为一致性检查(LastConsistencyCheck)
            + 流言系统(LastRumors)
            + 可选:长期记忆注入(MemoryMD)
```

### 3.3 Context 预算管理

**双层预算**:
1. **条数预算**: `DefaultPruneTurns = 80` 轮(160 条消息)
2. **字节预算**: `DefaultMaxPromptBytes = 200KB`

**剪枝策略**:
```
messages + system + tools = totalPayload
if totalPayload > maxPromptBytes:
    从最旧轮次开始淘汰
    直到 totalPayload <= maxPromptBytes
    始终保留 1 轮(user+assistant)
```

## 4. 与狼人杀 Agent 的对比

| 维度 | JiuwenSwarm | 狼人杀 Agent |
|------|-------------|--------------|
| **压缩处理器数** | 4+1 个独立处理器 | 2 个(CompressHistory + Prune) |
| **触发机制** | token 数 + 消息数 + 轮次数 | 条数 + 字节预算 |
| **工具结果处理** | 摘要 + OFFLOAD 占位 | 原样保留(100 条上限) |
| **循环检测** | 连续相同工具/参数折叠 | 无(靠 maxInnerRounds 硬限) |
| **缓存优化** | System prompt 分段缓存 | Anthropic cache_control |
| **预算粒度** | 按处理器独立配置 | 统一 maxPromptBytes |

## 5. 可借鉴的设计模式

### 5.1 工具结果摘要 → 狼人杀「道具列表压缩」

**问题**: 道具快照(PropSnapshot)每轮注入 ~500 字,50 轮 = 25KB 纯重复。
**借鉴**: 对 > 1000 字的 tool_result 做摘要,保留「本轮可用道具」而非全量列表。

### 5.2 OFFLOAD 占位符 → 狼人杀「按需召回」

**问题**: 聊天历史 500K 队列的 WindowFor 可能返回大量旧消息。
**借鉴**: 旧消息替换为 `[[OFFLOAD:chat_round_N]]`,Agent 需要时可通过工具召回。

### 5.3 循环压缩 → 狼人杀「重复行动折叠」

**问题**: Agent 可能连续 3 次调 wolf_kill(被拒后重试),占满内层循环。
**借鉴**: 检测到连续相同工具+参数时,自动折叠为「已尝试 N 次,结果:XXX」。

### 5.4 分层缓存 → 狼人杀「GameContext 分层」

**问题**: GameContext 每轮重建,大量字段不变(SheriffSeat/IdiotRevealedSeats 等)。
**借鉴**: 将 GameContext 分为「静态层」(整局不变)和「动态层」(每轮变化),
静态层可缓存,动态层每轮重建。

## 6. 关键教训

1. **压缩是流水线** —— 多个处理器各自独立触发,互不依赖,按序执行
2. **保留最后轮次** —— 所有处理器都有 `keep_last_round`,确保最新信息不丢
3. **OFFLOAD 优于删除** —— 压缩后的内容可通过占位符按需召回
4. **循环检测是独立维度** —— 与条数/字节数无关,检测的是「重复模式」
5. **配置要分层** —— 每个处理器独立配置阈值,适应不同场景
