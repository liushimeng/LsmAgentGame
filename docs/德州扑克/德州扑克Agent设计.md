# 德州扑克 Agent 设计（v1.0，2026-08-19）

_实现状态（2026-08-21 更新）_

| 章节 | 状态 | 实现位置 |
|------|------|---------|
| §3 AgentClassName 注册 | ✅ 已完成 | `ServerGo/agent/class_names.go` — 3 个常量 + AllAgentClassNames |
| §4 工具集（poker_action + poker_chat） | ✅ 已完成 | `ServerGo/agent/thpagent/tools.go` |
| §5 Prompt 结构（5 段 System + 13 块 User） | ✅ 已完成 | `ServerGo/agent/thpagent/prompt.go` |
| §6 决策引擎（4 个纯函数） | ✅ 已完成 | `ServerGo/agent/thpagent/decision.go` + `hand_eval.go` |
| §8.1 后端接入路径（全部 9 步） | ✅ 已完成 | thptypes/ + thpagent/ + room.go + view.go + game_service_texas_bot.go |
| §8.2 前端接入（6 项） | ✅ 已完成 | components/texasholdem/ 全部组件 |
| §9 验收硬约束 | ✅ 已完成 | go build + go test + tsc + npm run build 全 PASS |
| §10 后续路线（v1.1+） | ⏳ 预留 | 法官 Bot / ProfileIter / 记忆持久化 / 复盘 PDF |



> 本文档定义德州扑克（Texas Hold'em）AI 玩家 Agent 的**架构设计**、**工具调用协议**、
> **Prompt 结构**、**与狼人杀 Agent 的差异点**与**接入路径**。代码落地前请先阅读本文档 +
> [德州扑克规则与协议.md](./德州扑克规则与协议.md) + [德州扑克金币设计.md](./德州扑克金币设计.md)。

## 1. 设计目标

狼人杀 Agent 已经验证：**LLM 驱动的 Bot 在多人博弈类游戏里既能产生娱乐性，也能形成
可持续的「打法画像 → 迭代学习 → 行为可观测」闭环**。德州扑克的博弈密度与决策频度
（每手 4 个押注轮 × 6 个玩家）比狼人杀还高（狼人杀每局约 30 个发言 + 13 次投票），
是 Agent 化收益最大的第二款游戏。

具体目标：

1. **可玩性**：6 房间内任意 0..6 个座位可由 Agent 接管；与人类混坐时必须遵守 WS 协议节流与思考时限。
2. **公平性**：所有 Agent 共享同一套工具定义 + Prompt 模板，**只**模型不同（沿用 §15 「公平性」原则）。
3. **数学性**：Agent 决策必须以**牌力计算（Hand Strength）** + **底池赔率（Pot Odds）** 为基础，避免 LLM 拍脑袋。
4. **娱乐性**：Agent 必须保留「虚张声势（Bluff）」与「慢打（Slow Play）」能力，且 UI 可观测其内心独白。
5. **金币闭环**：与现有 `service/wallet_service.go` 集成（与狼人杀一致），手牌结算 ±N 金币。

## 2. 架构总览

```
                     ┌──────────────────────────┐
                     │   TexasHoldemManager     │
                     │  (game/texasholdem/)     │
                     │  - Room.State            │
                     │  - Action(userID, action)│
                     └────────────┬─────────────┘
                                  │ Hook
                                  ▼
                     ┌──────────────────────────┐
                     │ TexasHoldemAgentDriver   │  ← 新增
                     │  - agentBySeat[6]*Agent  │
                     │  - Drive(seat, ctx)      │
                     │  - onHandStart()         │
                     └────────────┬─────────────┘
                                  │ 委托
                                  ▼
              ┌─────────────────────────────────────┐
              │ ServerGo/agent/thpagent/             │  ← 新增子包
              │  - Agent（每座位一个 goroutine）     │
              │  - Memory（短期 + 持久化）            │
              │  - Tools（action / chat / think）    │
              │  - Prompt（BuildSystemPrompt 等）    │
              │  - Decision（牌力 + 赔率 + 虚张）    │
              │  - Engine（thpengine.go,牌力 + 胜率）│
              └─────────────┬───────────────────────┘
                            │
                            ▼
              ┌─────────────────────────────────────┐
              │ ServerGo/agent/core/                │  ← 复用
              │  - chat_history / speak_dedup       │
              │  - ratelimit / llm_helpers          │
              │  - record_log                       │
              └─────────────────────────────────────┘
```

**依赖方向严格 acyclic**（沿用 §13.2 原则）：

```
thpagent → agent/core (通用基础)
thpagent → thptypes (德州扑克专属契约)
thpagent ← game/texasholdem (引擎反向依赖 Agent)
thptypes ← game/texasholdem (只读 GameState 镜像)
```

**禁止反向依赖**：`agent/core` 不得 import `thpagent/thptypes`；`thptypes` 不得 import
`thpagent` 或 `game/*`；`thpagent` 与 `wwplayer/wwjudge` 完全平行（互不影响）。

## 3. AgentClassName 注册

按 §24.2 命名规则，新增 3 个常量：

| AgentClassName | 实现 | 调用 LLM 的场景 |
|---|---|---|
| `LsmAgentGame-TexasHoldem-Player` | `ServerGo/agent/thpagent.Agent` | 玩家 Bot 决策（每轮押注 + 聊天） |
| `LsmAgentGame-TexasHoldem-Judge` | 预留，v1.0 不实现 | （未来）摊牌讲解 / 旁观解说 |
| `LsmAgentGame-TexasHoldem-ProfileIter` | 预留，v1.0 不实现 | （未来）玩家行为画像迭代 |

**v1.0 仅实现 Player**，其他两个占位常量先行登记（避免后期 §130 「声明了却从不接线」复现）。

## 4. 工具集（Anthropic Protocol Tools）

每个 Bot Agent 在同一 LLM 调用里暴露**固定 4 个 tool**：

### 4.1 `poker_action`（核心决策，必填）

```json
{
  "name": "poker_action",
  "description": "出牌：弃牌 / 过牌 / 跟注 / 加注 / 全押。可选附加内心独白。",
  "input_schema": {
    "type": "object",
    "properties": {
      "action": {
        "type": "string",
        "enum": ["fold", "check", "call", "bet", "raise", "allin"],
        "description": "动作类型"
      },
      "amount": {
        "type": "integer",
        "description": "bet/raise 的目标绝对金额（与 engine.Action.Amount 字段一致）"
      },
      "internal_thought": {
        "type": "string",
        "description": "仅 Agent 自己可见的内心独白（不会广播给其他玩家）",
        "maxLength": 200
      }
    },
    "required": ["action", "internal_thought"]
  }
}
```

**与狼人杀的差异**：
- 狼人杀每轮最多 5 次 tool_use + `internal_thought` 与 `speak_with_thought` 二选一；德州扑克**每轮仅 1 次 tool_use**
  （一次决策就是一次 tool_use），不允许多次「试探 + 反悔」（扑克不允许）。
- 必填 `internal_thought` 让 Bot 决策可观测（UI 侧 BotThoughtPanel 渲染）。

### 4.2 `poker_chat`（可选，公屏发言）

```json
{
  "name": "poker_chat",
  "description": "在公屏发言（与狼人杀 speak 类似，但限流更严格：每手牌最多 2 次）。",
  "input_schema": {
    "type": "object",
    "properties": {
      "text": { "type": "string", "maxLength": 80 },
      "internal_thought": { "type": "string", "maxLength": 200 }
    },
    "required": ["text"]
  }
}
```

**限流**：每手牌 2 次；相邻 30s 节流；与狼人杀 speak_floor 同源。

### 4.3 `poker_read_state`（强制只读）

```json
{
  "name": "poker_read_state",
  "description": "读取当前对局状态（座位、筹码、底池、公共牌、对手动作历史）。每轮首次 LLM 调用前服务端自动注入，无需 Agent 显式调用。",
  "input_schema": { "type": "object", "properties": {} }
}
```

**实际不挂载**——Engine 端在 `BuildSystemPrompt` 阶段已经把状态拼到 system prompt，Agent
直接消费，**不**让 LLM 主动触发读（避免工具滥用）。

### 4.4 `poker_history`（只读，注入 user prompt）

历史手牌回顾由服务端**自动追加**到 user prompt 末尾（每手牌 1 条 `HandRecord`，
含自己的两张底牌 / 摊牌手牌 / 净盈亏），Agent 不需主动调用。

### 4.5 工具注册表

`ServerGo/agent/thpagent/tools.go` 注册 4 个 tool 到 `llm.LLMRequest.Tools`，
由 `BuildTools(seat int, gs *texasholdem.GameState)` 动态构造（按座位裁剪可见对手）。

**与狼人杀同源对比**：狼人杀注册表 `BuildTools(phase, role, seat, alive)` 接收 4 个参数，
德州扑克只需 seat + gs 两个（无 phase 概念，每轮独立决策；无 role 概念，所有玩家对称）。

## 5. Prompt 结构

`BuildSystemPrompt(seat int, gs *GameState, mem *Memory) string` 拼装 5 段：

```
[Identity]            你是 {model_name}，坐在德州扑克房间 {room_id} 的 {seat+1} 号位。
                      你面对 {n_active} 个对手。
[GameRules]           6-max No-Limit Hold'em 规则概要（1200 字内）
[CurrentState]        手牌 {N}: 你的底牌是 {c1}, {c2}。公共牌 {n} 张: ...
                      底池 {pot}，当前最高注 {current_bet}，你的剩余筹码 {stack}，
                      你的本轮已下注 {round_committed}。
[MathHelpers]         Hand Strength: 牌力 0.0-1.0，由服务端注入
                      Pot Odds: 跟注赔率
                      Required Equity: 跟注所需最低胜率
                      Position: BTN/SB/BB/UTG/MP/CO
                      Bluff Frequency: 虚张建议频率（按对手弃牌率反推）
[StyleGuide]          你可使用 poker_chat 在公屏发言,但每手牌最多 2 次。
                      poker_action 必须填 internal_thought(必填),
                      描述你看到牌面/赔率/对手风格后的真实思考。
```

**末尾追加 13 段 user-prompt 块**（与狼人杀同源 `prompt_budget.go::AssembleWithBudget`）：

1. `BotIdentityBlock`（model_key / 累计盈亏 / 手牌数）
2. `CurrentHandBlock`（hand_number / street / button / sb / bb）
3. `MyHandBlock`（自己两张底牌）
4. `CommunityCardsBlock`（已亮公共牌）
5. `PotOddsBlock`（跟注所需金额 / 底池 / Required Equity）
6. `OpponentsBlock`（每个对手 seat / stack / folded / 最近 N 手盈亏）
7. `ActionHistoryBlock`（本手牌前 N 个动作）
8. `HandStrengthBlock`（服务端计算的 7-牌 5-选最优牌型 + 胜率 vs 随机牌 + 胜率 vs 顶 20% 牌力范围）
9. `BluffHintBlock`（按对手弃牌率计算的建议 Bluff 频率）
10. `ReputationBlock`（其他模型对自己的「松紧度」评价，按 t_lsm_game_agent_player_profile 加载）
11. `MemoryMDBlock`（持久化 MEMORY.md，注入到末尾，最多 4000 字）
12. `RecentHandHistoryBlock`（最近 5 手牌结果）
13. `WalletBlock`（当前金币余额 + 本局累计盈亏）

**优先级 + 预算保护**：13 块按 Critical / Important / Optional 三档分类，`AssembleWithBudget`
丢弃 Optional 块时**必须**留可观测标记 `[预算省略: ...]`（同 §20260812-04 U2）。

## 6. 决策引擎（Decision Engine）

`ServerGo/agent/thpagent/decision.go` 提供 4 个纯函数（无 LLM 调用，纯数学）：

### 6.1 牌力计算 `handStrength(hole [2]Card, community [5]Card) float64`

基于蒙特卡洛模拟：从剩余 47..45 张牌中**随机抽样 1000 次**，每轮补全到 5 张公共牌 + 7 张
牌选 5 张，对照 `texasholdem.HandRank.Compare`。返回胜率（0.0-1.0）+ 平局率。

**预算**：单次决策需调一次 handStrength ≈ 1000 次 HandRank 评估 ≈ **10ms CPU**。
（参考：手牌有 47 choose 5 = 1,533,939 种组合，完整遍历 200ms+，蒙特卡洛 1000 次足够精确到 ±3%。）

### 6.2 底池赔率 `potOdds(callAmount, pot, myStack) (odds float64, requiredEquity float64)`

```
odds = callAmount / (pot + callAmount)
requiredEquity = odds
```

### 6.3 位置评估 `position(seat, button, gs *GameState) (label string)`

返回 `BTN / SB / BB / UTG / MP / CO`，6-max 默认 `UTG=2, MP=3, CO=4, BTN=5/0`。
（v1.0 简化版：只按 button 偏移给标签，不做 LLM 坐标映射。）

### 6.4 虚张频率 `bluffFrequency(opponentFoldRate float64) float64`

按对手历史弃牌率反推：
- 弃牌率 ≥ 70% → Bluff 频率 35%（高弃牌率对手多偷）
- 弃牌率 30-70% → Bluff 频率 15%（中性）
- 弃牌率 ≤ 30% → Bluff 频率 5%（黏池对手少偷）

**LLM 角色**：LLM **不**直接拍出 fold/call/raise，而是接收服务端计算好的
`hand_strength + required_equity + bluff_hint`，并按 prompt 中的「决策策略」给出最终动作。
LLM 真正决定的是「考虑位置 + 对手风格 + 手牌可玩性」后的**意图**；服务端兜底
（LLM 选 raise 但 amount < min_raise → 自动调高）。

## 7. 与狼人杀 Agent 的差异点

| 维度 | 狼人杀 | 德州扑克 |
|---|---|---|
| 决策频度 | 每局 30+ 次发言 + 13 次投票 | 每手牌 4 个押注轮 × 最多 1 次行动 |
| 信息可见 | 公开（身份除外）+ 夜间私有 | **手牌完全私有**（对手不可见底牌） |
| 决策时限 | 发言 60s + 投票 30s | 每轮 30s（与狼人杀投票同档） |
| 工具调用 | 每轮最多 5 次 | **每轮仅 1 次**（一次决策 = 一次 tool_use） |
| Chat 限流 | 发言下限 2 次/60s | **每手牌最多 2 次公屏发言** |
| 持久化 | `t_lsm_game_agent_memory` (MEMORY.md) | 同（共享表，按 model_key 隔离） |
| 玩法画像 | t_lsm_game_agent_player_profile | 同（共享表） |
| 金币 | 结算 ±100 | **结算 ±筹码×1**（按底池大小，详见金币设计 §3.2） |
| 法官 Agent | 必须（有 Agent 即有法官） | **v1.0 不实现**（无需公开宣告） |
| 观众唤醒 | 移除 15s 节流，每条观众消息触发全部 bot wake | v1.0 不实现观众唤醒（手牌节奏紧凑） |
| Lock 变体 | `*_Locked` 锁内变体（§92a） | 同（沿用） |
| 阶段 watchdog | 90s 同 phase+actingSeat 强制 skip | **30s 同 actingSeat 强制 fold**（超时即弃牌，简化版） |

## 8. 接入路径（v1.0）

### 8.1 后端

1. **新建子包** `ServerGo/agent/thptypes/` —— 德州扑克专属契约（`GameContext` / `ActionRecord` / `HandRecord`）。
2. **新建子包** `ServerGo/agent/thpagent/` —— Agent + Memory + Tools + Prompt + Decision。
3. **修改** `ServerGo/agent/class_names.go` —— 注册 3 个 AgentClassName。
4. **修改** `ServerGo/game/texasholdem/room.go` —— 加 `AgentDriver` 接口 + `DriveAction` 钩子；
   `ApplyAction(seat, a)` 持锁态检测若该 seat 是 bot 则转发到 AgentDriver（仅在非玩家主动行动路径）。
5. **新增** `ServerGo/game/texasholdem/agent_driver.go` —— `TexasHoldemAgentDriver` 维护 6 个
   `*thpagent.Agent` goroutine；`DriveAction(roomID, seat, ctx)` 阻塞等结果 → 持锁 apply `Engine.ApplyAction`。
6. **新增** `ServerGo/game/texasholdem/agent_hooks.go` —— `StartHand` 钩子通知 Driver 重建
   GameContext；`HandOver` 钩子通知 Driver 结算金币 + 写历史。
7. **修改** `ServerGo/service/room_service_crud.go` —— `CreateRoomWithAgents` 接受 `game_kind=="texasholdem"` 时，
   bot 座位下发给 `TexasHoldemAgentDriver.RegisterAgents(roomID, seats, models)`。
8. **修改** `ServerGo/ws/game_service.go` —— 注册 `game_kind=="texasholdem"` 的 AgentDriver
   钩子（与狼人杀 `ws/game_service.go::RegisterAgentSeats` 同模式）。
9. **修改** `ServerGo/main.go` —— 在 `wireRoomService()` 内创建 AgentDriver 单例并注入。

### 8.2 前端

1. **新增** `ClientWeb/src/components/texasholdem/BotThoughtPanel.tsx` —— 显示当前 Bot 内心独白
   （沿用狼人杀 AgentThoughtPanel 模式，简化版只显示最近 1 句）。
2. **修改** `ClientWeb/src/components/texasholdem/PlayerSeat.tsx` —— Bot 座位加「🤖 AI」徽章 + ⏱ 思考中指示器。
3. **修改** `ClientWeb/src/components/texasholdem/TexasHoldemTable.tsx` —— 接入 BotThoughtPanel。
4. **修改** `ClientWeb/src/pages/TexasHoldemLobbyPage.tsx` —— 创建房间表单加「AI 玩家数量」slider
   （沿用狼人杀 RoomCreateModal 模式，6-max 上限 6）。
5. **新增** `ClientWeb/src/components/texasholdem/RoomCreateModal.tsx` —— 创建房间弹窗（AI 配置块）。
6. **修改** `ClientWeb/src/types/api.ts` —— `CreateRoomOptions.agent_seats` 加 `game_kind` 路由（已存在则透传）。

## 9. 验收硬约束

- `go build ./...` 通过；`go test ./...` 全 PASS；新增 `thpagent` 子包 4 个单元测试
  （handStrength / potOdds / bluffFrequency / memory 注入预算）。
- `tsc --noEmit` + `npm run build` 通过；`BotThoughtPanel` 接线测试 1 项。
- `LsmAgentGame.conf` 加 `texasholdem.agent_enabled=true`（默认）+ `texasholdem.agent_action_timeout_sec=30`。
- 前端德州扑克 Lobby 页面加 [🤖 AI 房间] toggle 创建入口。
- 6 房间全 AI 自走 N 手不出 panic；混坐（3 人 + 3 bot）Bot 30s 内必出动作。

## 10. 后续路线（v1.1+）

| 版本 | 范围 |
|---|---|
| v1.1 | Bot 决策日志（PDF-style 复盘）+ 法官 Bot（摊牌讲解）+ 玩家画像迭代 |
| v1.2 | 多人锦标赛模式（多桌淘汰）+ ICM 独立筹码模型 |
| v1.3 | Agent v 人类 EV 统计（跨 1000+ 手牌） + 自适应难度调整 |