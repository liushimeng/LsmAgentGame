# 德州扑克 Agent 工具协议（Anthropic Wire）

_实现状态（2026-08-21 更新）_

| 章节 | 状态 | 实现位置 |
|------|------|---------|
| §1 工具形状（poker_action + poker_chat wire 格式） | ✅ 已完成 | `ServerGo/agent/thpagent/tools.go` |
| §2 工具注册（BuildTools） | ✅ 已完成 | `ServerGo/agent/thpagent/tools.go::BuildTools` |
| §3 消息流时序（单轮强制 1 次 tool_use） | ✅ 已完成 | `ServerGo/agent/thpagent/driver.go` + `dispatch.go` |
| §4 tool_result 回包格式 | ✅ 已完成 | `ServerGo/agent/thpagent/dispatch.go::DispatchPokerAction` |
| §5 poker_chat 公屏广播路径 | ✅ 已完成 | `ServerGo/ws/game_service_texas_bot.go` |
| §6 限流与去重 | ✅ 已完成 | `ServerGo/agent/thpagent/dispatch.go` |
| §8 Anthropic 协议合规检查清单 | ✅ 已完成 | 与狼人杀共用 `llm/types/types.go::MarshalJSON` |
| §9 测试用例（4 项） | ✅ 已完成 | `ServerGo/agent/thpagent/tools_wire_test.go` 等 |



> 本文定义德州扑克 Bot 在 Anthropic Messages API 上的 **tool 形状**、
> **消息流时序**、**与狼人杀 Agent 的差异点**。代码落地前请先阅读本文档 +
> [德州扑克Agent设计.md §4](./德州扑克Agent设计.md) + [CLAUDE.md §14.1](../../CLAUDE.md)。

## 1. 工具形状（Anthropic wire 格式）

### 1.1 `poker_action`（核心决策，必填）

按 CLAUDE.md §14.1 **ContentBlock wire 形状约束** —— `tool_use` 块**只允许**三键
`{"type","id","name","input"}`，本工具严格遵守：

```json
{
  "name": "poker_action",
  "description": "出牌：fold / check / call / bet / raise / allin。每次决策必填 internal_thought。",
  "input_schema": {
    "type": "object",
    "properties": {
      "action": {
        "type": "string",
        "enum": ["fold", "check", "call", "bet", "raise", "allin"]
      },
      "amount": {
        "type": "integer",
        "minimum": 0,
        "description": "bet/raise 的目标绝对金额（与 engine.Action.Amount 一致）"
      },
      "internal_thought": {
        "type": "string",
        "maxLength": 200,
        "description": "仅 Agent 自己可见的内心独白（协议层隔离，不入 chat_message 表）"
      }
    },
    "required": ["action", "internal_thought"]
  }
}
```

**ContentBlock wire 形状（Agent 出站 tool_use）**：

```json
{
  "type": "tool_use",
  "id": "toolu_poker_action_<uuid>",
  "name": "poker_action",
  "input": {
    "action": "raise",
    "amount": 400,
    "internal_thought": "BTN 位偷盲，对手弃牌率 65%，Bluff 频率建议 25%，目前手牌 7♠6♠ 有同花潜力..."
  }
}
```

**注意**（CLAUDE.md §14.1 wire 形状）：
- ✅ `{"type","id","name","input"}` 四键全有
- ❌ 切勿带 `text` / `content` / `is_error`（属于 text / tool_result 块的字段）

### 1.2 `poker_chat`（可选，公屏发言）

```json
{
  "name": "poker_chat",
  "description": "在公屏发言。每手牌最多 2 次，相邻 30s 节流。",
  "input_schema": {
    "type": "object",
    "properties": {
      "text": { "type": "string", "minLength": 1, "maxLength": 80 },
      "internal_thought": { "type": "string", "maxLength": 200 }
    },
    "required": ["text"]
  }
}
```

**ContentBlock wire 形状（Agent 出站 tool_use）**：

```json
{
  "type": "tool_use",
  "id": "toolu_poker_chat_<uuid>",
  "name": "poker_chat",
  "input": {
    "text": "BTN 偷盲，对手黏池...",
    "internal_thought": "对手很黏，我应该谨慎..."
  }
}
```

### 1.3 `poker_read_state` —— 不挂载

按设计 §4.3「强制只读」，**服务端自动注入 GameContext 到 system prompt**，Agent
不需主动触发读。**绝不在 Tool 列表中暴露**（避免 LLM 把决策时机浪费在 read state）。

### 1.4 `poker_history` —— 不挂载

历史手牌回顾由服务端**自动追加**到 user prompt 末尾（`RecentHandHistoryBlock`）。

## 2. 工具注册（Agent 构造期）

`ServerGo/agent/thpagent/tools.go`：

```go
// BuildTools 返回固定 2 个 tool（poker_action + poker_chat）。
// 与狼人杀的 BuildTools(phase, role, seat, alive) 不同,
// 德州扑克所有玩家对称,只需 seat 参数裁剪可见对手。
func BuildTools(seat int, gs *texasholdem.GameState) []llm.Tool {
    return []llm.Tool{
        {
            Name:        "poker_action",
            Description: "出牌(必填 internal_thought)",
            InputSchema: pokerActionSchema,
        },
        {
            Name:        "poker_chat",
            Description: "公屏发言(每手最多 2 次)",
            InputSchema: pokerChatSchema,
        },
    }
}
```

## 3. 消息流时序

### 3.1 一手牌决策的完整消息流

```
第 1 轮 LLM 调用 — 仅 system prompt + 状态描述:
  system: BuildSystemPrompt(seat, gs)           // ~12K tokens (身份+规则+状态+数学)
  user:   "请基于当前状态做出决策"
  → assistant (tool_use: poker_action{...})
  → user (tool_result: {action applied, current state, time remaining})

(可选) 第 2 轮 — LLM 自检:
  user:   "请基于刚才的结果调整决策(可选)"
  → assistant (tool_use: poker_action{...} 或 text{})
```

**与狼人杀的差异**：

| 维度 | 狼人杀 | 德州扑克 |
|---|---|---|
| 单轮 tool_use 上限 | 5 次（speak/vote/skill/etc.） | **1 次**（一次决策 = 一次 tool_use） |
| 自检轮数 | 0..5 轮 | **0..1 轮**（LLM 可选二次确认） |
| 退出条件 | LLM 主动不调 tool_use | **强制 1 次 tool_use 必须出现**（否则默认 fold） |

**强制 1 次 tool_use 必须出现** 是 v1.0 的硬约束——LLM 想「保持沉默」时直接超时 → 服务端
兜底 fold（不浪费 token 在「我要不要说」上）。这是德州扑克与狼人杀的关键设计差异。

### 3.2 ContentBlock 顺序

按 CLAUDE.md §14.1：**messages 数组 user/assistant 严格交替**，禁止连续 2 条
`role=user` 的消息。

典型一手牌决策的 messages 流：

```json
[
  // system: 已拼装到顶层 system[]
  {"role": "user",      "content": [{"type": "text", "text": "请基于当前状态做出决策"}]},
  {"role": "assistant", "content": [
    {"type": "thinking", "thinking": "..."},
    {"type": "tool_use", "id": "toolu_xxx", "name": "poker_action", "input": {...}}
  ]},
  {"role": "user",      "content": [
    {"type": "tool_result", "tool_use_id": "toolu_xxx", "content": "applied: raise 400", "is_error": false}
  ]}
]
```

**ContentBlock wire 形状（按 Type 收敛）**：
- `thinking` 块：`{"type","thinking"}` 两键
- `tool_use` 块：`{"type","id","name","input"}` 四键
- `tool_result` 块：`{"type","tool_use_id","content","is_error"}` 四键
- `text` 块：`{"type","text"}` 两键（不带 id/name/input/is_error）

**代码层强制**：`ServerGo/llm/types/types.go::ContentBlock.MarshalJSON()` 按 Type 分支产出，
与狼人杀共用同一序列化逻辑（避免 §14.1 列出的「协议层物理隔离」失效）。

## 4. tool_result 回包格式

`poker_action` 的 tool_result **内容**是服务端生成的 JSON 字符串（**不是** nested content blocks）：

```json
{
  "applied": true,
  "action": "raise",
  "amount": 400,
  "new_pot": 700,
  "new_stack": 9600,
  "current_bet": 400,
  "street": "preflop",
  "next_actor_seat": 1,
  "round_completed": false
}
```

**Linter 守卫**：每个 `tool_result` 必须有 `tool_use_id` 配对（同 §82b），`SanitizeMessagesForAnthropic`
末尾会校验配对完整性。

## 5. poker_chat 公屏广播路径

`poker_chat` 工具的 tool_result 触发**公屏广播**（与狼人杀 `speak_with_thought` 一致）：

1. Agent 调 `poker_chat{text, internal_thought}`
2. 服务端 `DispatchPokerChat` 校验：每手牌 ≤ 2 次 + 相邻 ≥ 30s
3. 校验通过 → 写 `t_lsm_game_chat_message` (scope=room) + Hub.BroadcastRoom 推 `chat.message`
4. `internal_thought` **不入** chat 表/chat_history 队列（CLAUDE.md §119 协议层隔离）
5. 同时写 `BotTranscript.HeartThought`（前端 BotThoughtPanel 可读）

## 6. 限流与去重

### 6.1 poker_action 限流

- **每轮 1 次**（强制，由 `DispatchTool` 校验）
- **30s 超时**（`texasholdem.agent_action_timeout_sec` 配置，超时服务端兜底 fold）

### 6.2 poker_chat 限流（与狼人杀 speak_floor 同源）

- **每手牌最多 2 次**（`pokerChatLimiter` 按 hand_number 计数）
- **相邻 ≥ 30s**（`pokerChatLimiter` token bucket, 30s/次, burst=1）

### 6.3 发言去重（与狼人杀 speakDedupRecent 同源）

沿用 `agent/core/speak_dedup.go::recentSpeakDedup`：
- 最近 5 句发言 fingerprint（normalize 后）查重
- 命中 → 拒绝本次 tool_use + 返回「与上一句重复」错误
- 让 LLM 换一种说法重发

## 7. 与狼人杀的工具使用差异速览

| 工具 | 狼人杀（多 tool） | 德州扑克（少 tool） |
|---|---|---|
| `speak` | ✅ 每轮 1 次 | ❌（用 `poker_chat`） |
| `speak_with_thought` | ✅ 每轮 1 次 | ❌（用 `poker_chat` + internal_thought） |
| `vote` | ✅ | ❌（扑克无投票） |
| `skill_*` | ✅ 多工具（守卫/猎人/女巫...） | ❌（扑克无技能） |
| `use_prop` | ✅ | ❌（v1.0 不实现道具） |
| `poker_action` | ❌ | **✅ 每轮 1 次（必填 internal_thought）** |
| `poker_chat` | ❌ | ✅ 每手牌 ≤ 2 次 |

## 8. Anthropic 协议合规检查清单

- [x] ContentBlock 按 Type 分支序列化（与狼人杀共用 `types.go::MarshalJSON`）
- [x] messages 数组 user/assistant 严格交替（`SanitizeMessagesForAnthropic` 末尾合并）
- [x] tool_use 块必带 `{"type","id","name","input"}` 四键
- [x] tool_result 块必带 `{"type","tool_use_id","content","is_error"}` 四键
- [x] system 字段是 SystemBlock 数组（`[{"type","text"}]`）而非纯 string
- [x] `content` 字段是 content-block 数组而非纯 string
- [x] metadata.user_id 走 `buildMetadataUserID` 构造
- [x] 出站请求头：`Authorization` / `anthropic-version: 2023-06-01` / `User-Agent: <AgentClassName>/<ver> <time>` / `x-anthropic-billing-header` / `Content-Type`

## 9. 测试用例

- `tools_wire_test.go` — wire 形状测试（4 个 tool_use 块 / 4 个 tool_result 块序列化后字段名/键数精确）
- `messages_sanitize_test.go` — `SanitizeMessagesForAnthropic` 把 `[user,user]` 合并成 `[user]`（同狼人杀）
- `poker_action_test.go` — DispatchPokerAction 强制 1 次/轮；超 30s 兜底 fold
- `poker_chat_test.go` — 每手牌 2 次上限 + 30s 节流 + 去重

## 10. 失败模式与回退

| 失败 | 兜底 |
|---|---|
| LLM 调用超时（30s） | 服务端 fold |
| LLM 返回 0 个 tool_use | 服务端 fold（按 potOdds 检查，赔率太差直接 fold，赔率好随机 call/raise） |
| LLM 返回 ≥ 2 个 tool_use | 仅取第一条 poker_action；后续丢弃 |
| LLM 返回的 raise amount < min_raise | 服务端自动抬到 min_raise |
| LLM 返回的 raise amount > my_stack | 服务端改为 allin |
| Agent goroutine panic | `defer recover()` 捕获 + 标记 consecutiveFailures++ + quarantine（与狼人杀同） |