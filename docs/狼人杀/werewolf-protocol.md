# 狼人杀 (Werewolf) WebSocket 协议字段说明

> **2026-07-10 §重构**。本文档列出 BotTranscriptJSON 的所有字段,特别是
> §重构 新增的 LLM 调用相位状态机字段。

## 1. BotTranscriptJSON 字段表

`BotTranscriptJSON` 是 `game.state.bot_contexts[]` 数组元素的类型,后端由
`ServerGo/agent/agent.go::BotTranscript` 定义,前端由
`ClientWeb/src/types/werewolf.ts::BotContextJSON` 镜像。两个文件必须保持字段名一致
(json tag = key)。

| 字段 | 类型 | 说明 | §重构 新增 |
|------|------|------|-----------|
| `seat` | number | 0-indexed 座位 | |
| `model` | string | 当前 LLM Provider 的 model key | |
| `last_thinking` | string | 兼容字段,§重构后置空 | |
| `last_tool` | string | 最近工具名 | |
| `recent_messages` | string[] | 兼容字段,§重构后 [] | |
| `tool_calls` | string[] | 兼容字段,§重构后 [] | |
| `updated_at` | number | unix millis | |
| `quarantined` | boolean? | 是否被永久禁用 | |
| `quarantine_reason` | string? | 禁用原因(≤200 字) | |
| `last_decision_summary` | string? | ≤50 字决策摘要 | |
| `last_tool_input` | string? | 工具入参 JSON(脱敏后) | |
| `last_tool_result` | string? | 工具结果前 80 字 | |
| `last_outcome` | string? | OK/FAIL/skip/idle/quarantine | |
| `decision_inputs` | string? | ≤200 字决策输入摘要 | |
| `full_thinking` | string? | 兼容字段,§重构后置空 | |
| `chat_history_bytes` | number? | 500K 队列当前字节数 | |
| `chat_history_cap` | number? | 500K 队列容量 | |
| `last_compression_at` | number? | 上次压缩 unix ms | |
| `speak_count_last_min` | number? | 60s 窗口内 speak 累计 | |
| `llm_call_in_progress` | boolean? | 是否正在调用 LLM | |
| `llm_call_started_at` | number? | 调用开始 unix ms | |
| `heart_thought` | string? | LLM 内心独白(§119) | |
| `heart_thought_at` | number? | 内心独白写入 unix ms | |
| `last_llm_latency_ms` | number? | §120 上次耗时 ms | |
| `avg_llm_latency_ms` | number? | §120 滑动平均 ms | |
| `total_llm_calls` | number? | §120 累计调用次数 | |
| `is_sheriff` | boolean? | 是否警长 | |
| `sheriff_stream` | number[]? | 警徽流目标 | |
| `idiot_revealed` | boolean? | 是否白痴翻牌 | |
| `emotion` | string? | §124 当前情绪 key | |
| `emotion_reason` | string? | §124 切换原因 | |
| `emotion_updated_at` | number? | §124 切换时间 ms | |
| `emotion_history` | EmotionRecord[]? | §124 最近 5 次切换 | |
| `last_death_verdict` | string? | §123 execution/death | |
| `last_death_cause` | string? | §123 wolf/vote/hunter/... | |
| `last_death_seat` | number? | §123 死者座位 | |
| `last_death_round` | number? | §123 死亡轮次 | |
| **`llm_call_phase`** | string? | §重构 **新增**:5 态相位 | ✅ |
| **`retry_attempt`** | number? | §重构 **新增**:retry 轮次(1-based) | ✅ |
| **`retry_max_attempts`** | number? | §重构 **新增**:retry 上限 | ✅ |
| **`next_retry_at_ms`** | number? | §重构 **新增**:下次 retry unix ms | ✅ |
| **`last_error_class`** | string? | §重构 **新增**:5xx/429/timeout/permanent | ✅ |

## 2. LLM 调用相位状态机 (§重构)

5 态枚举,字符串值必须与 `ServerGo/agent.Phase*` 常量完全一致:

| 常量 | 字符串值 | 含义 |
|------|----------|------|
| `PhaseIdle` | `idle` | 未调 / 已完成 / 占位 |
| `PhaseCalling` | `calling` | HTTP 调用中,首 token 未到 |
| `PhaseStreaming` | `streaming` | 流式首 token 已到达(可选) |
| `PhaseRetrying` | `retrying` | retry loop 内,等待 backoff |
| `PhaseQuarantined` | `quarantined` | 永久禁用 |

### 状态转移

```
idle ──→ calling ──→ streaming ──→ idle
   ▲         │            ▲
   │         ▼            │
   └── retrying[1..4] ─────┘
              │
              ▼ (consecutive ≥ 5)
         quarantined
```

### 字段组合示例

| 场景 | llm_call_phase | retry_attempt | retry_max_attempts | next_retry_at_ms | last_error_class |
|------|----------------|---------------|--------------------|---------------------|-------------------|
| 正在调用 | `calling` | 0 | 0 | 0 | `none` |
| 流式生成 | `streaming` | 0 | 0 | 0 | `none` |
| 5xx 重试 | `retrying` | 1 | 1 | 1699999999000 | `5xx` |
| 429 限流 | `retrying` | 1 | 1 | 1699999999000 | `429` |
| 永久禁用 | `quarantined` | 0 | 0 | 0 | `permanent` |

## 3. 客户端必读字段

对于前端的最小可工作集合,BotPhaseIndicator 仅依赖以下字段:

- `seat` (key)
- `quarantined` (PhaseQuarantined 优先)
- `quarantine_reason` (PhaseQuarantined 文案)
- `llm_call_in_progress` (兼容旧逻辑)
- `llm_call_started_at` (倒计时起点)
- `llm_call_phase` (新逻辑主键)
- `retry_attempt` / `retry_max_attempts` (N/M 徽章)
- `next_retry_at_ms` (retry 倒计时)
- `last_error_class` (cooling/reconnecting/retry 分支选择)

其余字段(Q120 性能 / §124 情绪 / §123 死亡)由 Header 与 SeatCell 其他部分消费。

## 4. 写入责任

| 字段 | 写入位置 | 锁 |
|------|----------|-----|
| `llm_call_phase` | `run.go` 6 个写点 (safety-net / limiter / semaphore / MarkLLMCallStart / retry loop / SetQuarantined) | `a.mu` |
| `retry_attempt` / `retry_max_attempts` / `next_retry_at_ms` | `run.go` retry loop 入口 | `a.mu` |
| `last_error_class` | `run.go` 失败分类 (SetLastError 之后) | `a.mu` |

所有字段都在 `Agent` struct 上的同名字段,`BotTranscript()` 读方法在锁内拷贝到
`*BotTranscript` 返回值。
