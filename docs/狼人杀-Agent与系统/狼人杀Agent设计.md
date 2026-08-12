# 狼人杀 12 人标准竞技局 Agent 架构设计

> 本文档描述「狼人杀 12 人标准竞技局 Agent」的设计契约。Agent 是一个纯 Go 的 in-process 驱动，
> 每个 bot 座位一个 goroutine，通过 LLM + 工具定义自动打牌。
> 模块位于 `ServerGo/agent/`，依赖 `ServerGo/llm/` 与 `ServerGo/game/werewolf/`。
> 上游规则文档：[`docs/狼人杀13人标准局规则.md`](狼人杀13人标准局规则.md)（本文档不再重复规则细节，只列 Agent 侧映射）。

## 1. 设计目标与约束

| 目标 | 实现 |
|------|------|
| 智能 | 系统提示词内嵌完整 12 人标准竞技局规则 + 胜负目标；多轮对话记忆 |
| 健壮 | 工具调用 schema 严格校验；非法调用返回 tool_result 错误让模型重试；单轮最多 5 次 tool_use |
| 公平 | 所有 Agent 代码相同，仅模型不同（不同大模型 API） |
| 可观测 | 人类玩家与观战者能看到每个 Agent 的 Memory / Context / 最近工具调用 |
| 限流 | 发言 ≤ 2 次/分钟（令牌桶 30s 间隔,pre_wolves 缩到 30s）；每次输出 ≤ 100 字（截断 + 系统提示词约束） |
| 混合房间 | 支持全人类 / 全 Agent / 混合（部分座位 bot，部分人类） |

## 2. 目录与分层

```
ServerGo/agent/
├── agent.go            # Agent 结构体 + Run(ctx) 主循环
├── memory.go           # Memory（多轮历史）+ Context（当前快照）
├── tools.go            # 工具定义生成 + DispatchTool 派发
├── prompt.go           # 系统提示词 + 用户提示词构建
├── ratelimit.go        # 令牌桶限流 + 文本截断
├── parallel_think.go   # 2026-07-10 §122 单 bot 内多线程 LLM 调用（只读 worker）
└── agent_test.go       # mock provider 验证工具派发、限流、截断
```

## 3. 生命周期

```
房间创建（含 agent_seats）
   ↓
StartGame（7 人坐满）
   ↓ 对每个 bot 座位
go Agent.Run(ctx)
   ↓
观察 room.agentEvents channel（phase 切换 / 新消息 / 轮到该座位）
   ↓ 轮到我
构建 system + messages + tools → provider.Chat → 解析 tool_use
   ↓
DispatchTool → WerewolfManager.Action_* / ChatService.SendFromBot / WhisperFromBot
   ↓
把 tool_result 写回 memory，继续循环（直到 stop_reason != tool_use）
   ↓
房间结束 → ctx 取消 → goroutine 退出
```

**关键**：Agent 不直接持有 `ws.Client`，而是通过 `ChatService.SendFromBot` / `WhisperFromBot` 发送发言，复用现有广播路径。

## 4. 数据结构

```go
// Agent 是一个 bot 座位的驱动。
type Agent struct {
    RoomID    string
    Seat      werewolf.Seat
    UserID    string          // bot 对应的虚拟 user id（落库用）
    ModelKey  string          // 如 "MeiTuan-model"
    Provider  llm.LLMProvider
    Registry  *llm.Registry
    Mgr       *werewolf.WerewolfManager
    Chat      chat.Sender     // SendFromBot / WhisperFromBot 接口

    Memory    *Memory
    Context   *Context
    Limiter   *SpeakLimiter
    MaxToolUse int            // 单轮最多 tool_use 次数，默认 5
}

type ToolRecord struct {
    Name      string
    Input     map[string]any
    Result    string         // 成功/错误描述
    At        time.Time
}
```

### 4.1 Memory（多轮记忆）

```go
type Memory struct {
    Messages []llm.Message   // 完整多轮历史（role=user/assistant）
    mu       sync.Mutex
}

func (m *Memory) Push(msg llm.Message)
func (m *Memory) Prune(maxRounds int)   // 保留最近 N 轮，避免 context 爆炸
func (m *Memory) Snapshot() []llm.Message
```

- 初始：`user` 消息 = 「你是 X 号玩家，身份是 Y，游戏目标是 Z」。
- 每轮决策：把 `Context` 打包成 `user` 消息追加。
- 模型回复：`assistant` 消息追加。
- 工具结果：`user` 消息（content type=tool_result）追加。

### 4.2 Context（当前可见上下文）

```go
type Context struct {
    GameState      *werewolf.ClientGameState   // 当前游戏快照（按座位过滤）
    RecentSpeeches []SpeechEvent               // 最近 N 条发言（含其他玩家）
    LastNightResult *NightResult               // 上一晚死亡/查验/用药结果（仅自己可见部分）
    WhisperInbox   []WhisperEvent              // 发给我的私聊
    Round          int                         // 第几天

    // 12 人局新增字段（详见上游规则文档 §7 / §3.5）
    SheriffSeat        int          // 当前警长座位（-1 无警长）
    SheriffStream      [2]int       // 第一/第二警徽流目标（-1 未声明），预言家警长可见全部，他人可见声明摘要
    IdiotRevealedSeats []int       // 已翻牌的白痴座位（全场公开）
    DivineCnt          int          // 当前存活神职数（预+女巫+猎+白痴）
    PlainCnt           int          // 当前存活平民数
    WolfAliveCnt       int          // 当前存活狼人屠边参考
}

type SpeechEvent struct {
    Seat    int
    Account string
    Text    string
    Ts      int64
}
```

Context 在每次决策时序列化为 `user` 消息（文本），让模型「看到」当前局面。

### 4.3 BotTranscript（对外可见的摘要）

```go
// 挂在 WerewolfRoom 上，随 game.state 广播
type BotTranscript struct {
    Seat          int       `json:"seat"`
    Model         string    `json:"model"`
    LastThinking  string    `json:"last_thinking"`   // 最近一次 text 输出（截断）
    LastTool      string    `json:"last_tool"`       // 最近一次工具调用描述
    RecentMessages []string `json:"recent_messages"` // 最近 20 条消息摘要
    ToolCalls     []string `json:"tool_calls"`      // 最近 5 次工具调用摘要
    UpdatedAt     int64     `json:"updated_at"`
}
```

**大小控制**：`RecentMessages` ≤ 20 条、`ToolCalls` ≤ 5 条、每条 ≤ 200 字符，避免 `game.state` payload 过大。

## 5. 工具定义（与引擎方法一一映射）

`BuildTools(phase, seat, role) []llm.ToolDef` 按当前阶段 + 身份返回可用工具：

| 工具名 | 参数 | 映射引擎方法 | 可用阶段 | 可用身份 |
|---|---|---|---|---|
| `wolf_kill` | `{target: int}` | `Action_WolfKill` | night_wolves | 狼人（target=-1 表空刀） |
| `seer_check` | `{target: int}` | `Action_SeerCheck` | night_seer | 预言家 |
| `witch_act` | `{action: "none"\|"antidote"\|"poison", target?: int}` | `Action_Witch` | night_witch | 女巫 |
| `speak` | `{text: string}` | `ChatService.SendFromBot` | speak | 当前发言座位 |
| `speak_with_thought` | `{text: string, internal_thought: string}` | `SpeakWithThought` | speak / pre_wolves | 当前发言座位（§119 心口不一） |
| `finish_speak` | `{}` | `Action_FinishSpeak` | speak | 当前发言座位 |
| `vote` | `{target: int}` | `Action_DayVote` | vote | 存活玩家（target=-1 弃权） |
| `finish_vote` | `{tied_round?: int}` | `Action_FinishVote` | vote 结束 | 系统/平票 |
| `start_day` | `{}` | `Action_StartDay` | dawn | 任意（触发推进） |
| `sheriff_candidate` | `{target: int}` | `Action_SheriffCandidate` | sheriff | 参选玩家 |
| `sheriff_elect` | `{}` | `Action_SheriffElect` | sheriff 投票后 | 系统 |
| `sheriff_stream` | `{slot: 1\|2, target: -1\|0..11}` | `Action_SheriffStream` | speak / sheriff / dawn | 警长（预言家）【新增】 |
| `hunter_shoot` | `{target: int}` | `Action_HunterShoot` | hunter_shoot | 猎人（target=-1 不开枪） |
| `idiot_reveal` | `{choice: "reveal"\|"skip"}` | `Action_IdiotReveal` | idiot_reveal | 白痴（最高票时）【新增】 |
| `wolf_suicide` | `{}` | `Action_WolfSuicide` | speak | 狼人（自爆） |
| `whisper` | `{to_seat: int, text: string}` | `WhisperFromBot` | 任意 | 任意（限流） |
| `interject` | `{text: string}` | `SendFromBot`（插话） | 非发言阶段 | 任意（speak 桶） |
| `idle_think` | `{reason: string}` | （零工具） | 白名单 acting phase | 任意 |
| `restart_vote` | `{choice: "yes"\|"no"\|"abstain"}` | `CastRestartVote` | restart_vote | 存活玩家 |

**input_schema** 用 JSON Schema 描述：
- `target`：integer，枚举当前存活玩家座位（动态生成 `enum`）
- `action`：string，枚举 `["none","antidote","poison"]`
- `text`：string，maxLength=100

## 6. 系统提示词（`prompt.go`）

`BuildSystemPrompt(role, rules string) []llm.SystemBlock`：

```
你是一个狼人杀 12 人标准竞技局的 AI 玩家。

【游戏规则摘要】
- 12 人局：4 狼人 + 1 预言家 + 1 女巫 + 1 猎人 + 1 白痴 + 4 平民
- 狼人阵营胜：屠边——杀光所有神职（预言家+女巫+猎人+白痴=4），或杀光所有平民（4 名）
- 好人阵营胜：放逐全部 4 名狼人
- 阶段：夜晚（狼人刀人→预言家查验→女巫用药）→ 黎明（公布死亡 + 警徽流结算）→ 警长竞选（仅 Day1）→ 发言 → 投票 → 白痴翻牌（若最高票）→ 遗言 → 猎人开枪
- 警长 1.5 票，预言家警长通过警徽流在夜间死亡后传递验人信息（金水/查杀）
- 白痴被白天投票放逐可翻牌免死（失去投票权但仍存活发言）

【你的身份】{role}
【你的座位号】{seat}（0-11）
【你的玩家编号】{seat+1}（1-12）
【你的目标】{win_condition}

【操作规则】
- 你只能通过调用工具来操作游戏，不能直接输出游戏指令
- 每次发言不超过 100 字（utf8 字符）
- 发言频率不超过 2 次/分钟
- 私聊（whisper）仅在需要协商时使用，同样限流
- 警徽流（sheriff_stream）：你作为预言家警长时，应在白天声明验人秩序（第一/第二警徽流目标）

【当前局面】
{game_state_summary}
```

**胜负目标**：
- 狼人：「你的目标是狼人阵营获胜。你需要杀光所有神职（4 神）或所有平民（4 民），同时隐藏身份、嫁祸好人、与 3 名队友夜间协商刀人。」
- 好人：「你的目标是好人阵营获胜。你需要找出并放逐全部 4 名狼人，合理使用技能。」
- 预言家警长：「你当选警长后应规划警徽流，在夜间死亡时通过警徽向好人传递验人结果。」

## 7. 用户提示词

`BuildUserPrompt(ctx Context) string` 把 Context 打包成文本：

```
第 {round} 天 · 当前阶段：{phase_desc}
存活玩家：{alive_players}
当前发言座位：{speak_turn_seat}
上一晚结果：{last_night_result}
最近发言：
- {seat}号（{account}）：{text}
...
发给你的私聊：
- {from_seat}号：{text}
...

现在轮到你行动。请根据当前局面，调用合适的工具。
```

## 8. 限流（`ratelimit.go`）

```go
type SpeakLimiter struct {
    interval time.Duration // 默认 30s（保证 ≤2 次/分钟）
    burst    int           // 默认 1
    // 底层：golang.org/x/time/rate 或自实现令牌桶
}

func (l *SpeakLimiter) Allow() bool   // 能否发言
func (l *SpeakLimiter) Wait(ctx) error // 阻塞直到可发言
func (l *SpeakLimiter) Mark()         // 已发言一次
```

- `speak` 工具调用前必须 `Limiter.Wait(ctx)`，保证间隔。
- `whisper` 共用同一个 limiter。
- 文本截断：`Truncate(text, 100)` 在 `speak` 写入前截断。

## 9. 主循环（`agent.go`）

```go
func (a *Agent) Run(ctx context.Context) {
    for {
        select {
        case evt := <-a.room.AgentEvents(a.Seat):
            if !a.isMyTurn(evt) { continue }
            a.Memory.Push(userTurn(evt))
            a.Context.Update(evt)
            for i := 0; i < a.MaxToolUse; i++ {
                resp, err := a.Provider.Chat(ctx, a.key(), a.buildRequest())
                if err != nil { break }
                a.Memory.Push(assistantTurn(resp))
                if resp.StopReason != "tool_use" { break }
                for _, tu := range toolUses(resp) {
                    result := a.DispatchTool(tu)
                    a.Memory.Push(toolResult(tu.ID, result))
                    a.recordTool(tu, result)
                    if tu.Name == "speak" || tu.Name == "whisper" {
                        a.Limiter.Mark()
                        goto nextEvent  // 发言后让出，等下一轮事件
                    }
                }
            }
        case <-ctx.Done():
            return
        }
    }
}
```

**唤醒机制**：`room.AgentEvents(seat)` 是一个 per-seat channel，房间状态变化（phase 切换、新消息、轮到该座位）时，manager 向该 channel 推事件。避免忙轮询。

## 10. 工具派发（`tools.go`）

```go
func (a *Agent) DispatchTool(tu llm.ContentBlock) (string, error) {
    switch tu.Name {
    case "wolf_kill":
        target := int(tu.Input["target"].(float64))
        _, err := a.Mgr.Action_WolfKill(a.RoomID, a.UserID, werewolf.Seat(target))
        return errString(err), err
    case "seer_check":
        ...
    case "witch_act":
        ...
    case "speak":
        text := truncate(tu.Input["text"].(string), 100)
        err := a.Chat.SendFromBot(a.RoomID, a.Seat, text)
        return errString(err), err
    case "whisper":
        toSeat := int(tu.Input["to_seat"].(float64))
        text := truncate(tu.Input["text"].(string), 100)
        err := a.Chat.WhisperFromBot(a.RoomID, a.Seat, werewolf.Seat(toSeat), text)
        return errString(err), err
    ...
    default:
        return "", fmt.Errorf("unknown tool: %s", tu.Name)
    }
}
```

**错误处理**：引擎返回的 `*errcode.Error` 转字符串作为 tool_result 内容，让模型看到错误并重试。

## 11. 可见性（人类/观战者看到 Agent 思考）

- `BotTranscript` 挂在 `WerewolfRoom.BotTranscripts[seat]`。
- `view.go` 的 `ClientGameState` 新增 `BotContexts []BotContextJSON`。
- `broadcastWerewolfState` 时附带。
- 前端渲染为「Agent 思考」折叠面板（Phase 4）。

## 12. 混合房间

- DB `t_lsm_game_room_players`（或对应关联表）新增 `is_agent TINYINT(1) DEFAULT 0`、`model_key VARCHAR(64) DEFAULT ''`。
- 创建房间时指定 `agent_seats: [{seat, model_key}]`。
- `StartGame` 时，对 `is_agent` 座位启动 `Agent.Run`，人类座位等真实玩家加入。
- 房间满 7 人（人类 + bot）即开局。

### 12.1 AI 玩家模型随机分配

> 当 `len(agent_seats) > 1` 时，服务端 `RoomService::CreateRoomWithAgents`
> 会自动把重复的 `model_key` 改写成其他可用模型，避免 7 个 bot 全用同一模型。

- **触发条件**：`len(agent_seats) > 1` 且 `len(cfg.LLM.Providers) > 1`
- **保留策略**：用户主动挑选的不同 model 保持不变；只有重复项被改写
- **随机源**：`time.Now().UnixNano()` 种子 + `math/rand.Fisher-Yates`，每次请求顺序不同
- **占位 key 过滤**：`APIKey == "API-KEY-PLACEHOLDER"` 或空字符串的 provider 不进入候选池
- **降级**：候选池不足以覆盖重复项时回退到候选池的随机轮询
- **日志**：每次改写打一条 `agent seat duplicate model reassigned` Warn
- **单元测试**：`ServerGo/service/room_service_alternate_test.go` 覆盖 placeholder 过滤、shuffle 多样性、空配置退化

## 13. 测试策略

`ServerGo/agent/agent_test.go`：
- mock `llm.LLMProvider` 返回预设 `tool_use`（如 `speak`、`vote`）。
- 验证：
  1. `DispatchTool("vote", {target:3})` 调用 `Action_DayVote`。
  2. `DispatchTool("speak", {text:"..."})` 调用 `SendFromBot`。
  3. 限流：连续两次 `speak`，第二次被阻塞/拒绝。
  4. 截断：100+ 字输入截为 100 字。
  5. 单轮 tool_use 超 5 次强制退出。
- 集成测试：7 个 mock-provider Agent 跑完整对局到 `game.over`。

## 14. 不做的事（边界）

- ❌ 不做 Agent 跨房间持久化 Memory（单局内存，房间结束释放）。
- ❌ 不做 Agent 图像/语音等多模态。
- ❌ 不做 Agent 之间的「私下协商协议」——仅通过游戏内 whisper 工具。
- ❌ 不做 Agent 难度分级（所有 Agent 同等智能，差异来自模型本身）。

## 15. 单 bot 内多线程 LLM 调用（2026-07-10 §122）

> 8 个 LLM 提供商 API 平均响应时间差异巨大（Kimi/DouBao/Qwen 0.8-2.5s，
> MiniMax/Xiaomi/MeiTuan 2-5s，DeepSeek/GLM 4-12s）。让单 bot 串行 LLM
> 调用意味着：主 LLM 响应期间该 bot 完全空闲，对局节奏受尾部延迟支配。

### 15.1 设计目标

- **加速单 bot 多轮决策**：让单个 Agent 在 handleEvent 决策周期内并行发起 N 个
  独立推理查询（默认 N=2），把结果作为"辅助思考"段注入主 LLM 的 prompt。
- **不破坏游戏流程**：阶段顺序、tool dispatch 原子性、quarantine 计时、
  `agentRunner` 单 goroutine 调用约定全部保留。
- **向后兼容**：默认 `EnableParallelThink=false`，行为等价旧版；显式开启后
  才在白名单 phase（`PhaseSpeak`/`PhaseVote`/`PhasePreWolves` 等）触发。

### 15.2 关键不变式

| 模块 | 是否可改 | 原因 |
|------|----------|------|
| `Memory.Push` / `Memory.Prune` | ❌ 单 goroutine 写 | Anthropic 协议 tool_use/tool_result 配对 §82b |
| `agentRunner` 任意方法 | ❌ 单 goroutine 调用 | `agent_runner.go` 无内部锁；与 §92a 锁顺序冲突 |
| tool dispatch 串行 for-loop | ❌ 不可 fan-out | §92a 锁内变体约束 |
| `consecutiveFailures` 语义 | ❌ 不变 | §110 retryable 失败冷却必须保留 |
| 房间级 `llmSema`（默认 8） | ❌ 仍生效 | Σ bots × perBotParallel ≤ 8 |

### 15.3 实现组件

- **`parallel_think.go::ParallelThink`**（fan-out goroutine 池）
  - 启动 N 个 worker；每个 worker 走 `AcquireLLMSlot(5s)` + 独立 `llm.LLMRequest`
    （**不携带** Memory.messages，避免污染主对话上下文）。
  - 成功结果追加到 `a.parallelThoughts`（受 `a.mu` 保护）。
  - 失败**不计入** `consecutiveFailures`，与 §110 retryable 冷却一致。
  - `parallelThinkInFlight` 标志防止嵌套并行。
- **`run.go::handleEvent`** 集成
  - 主循环入口触发 `ParallelThink`（仅白名单 phase）。
  - 每轮 req 构造之前 `a.ParallelThoughts()` 取出 + `appendToLastUserMessage`
    把【辅助思考】段追加到最后一条 user message 的 text 块末尾。
  - **不创建新 user message**（避免破坏 `SanitizeMessagesForAnthropic`
    的 tool_use/tool_result 配对校验）。
- **`config.AgentParallel`** 配置段
  - `MaxParallelLLMCalls`（默认 2）
  - `ParallelThinkMaxWaitMs`（默认 8000ms）
  - `EnableParallelThink`（默认 false）
  - `ParallelThinkTriggers`（默认 `[PhaseSpeak, PhaseVote, PhasePreWolves, ...]`）

### 15.4 单元测试（`parallel_think_test.go`）

- 写入/取出/原子清空
- 失败不计入 quarantine
- semaphore 上限约束（`maxObserved ≤ 1`）
- 白名单判定 + 禁用语义（maxCalls=0 / enabled=false）
- 嵌套 fan-out 防护
- 超时释放
- 空 queries no-op
- `appendToLastUserMessage` 三种路径（正常 / 无 user / 纯 tool_result）
- `buildParallelThinkQueries` 9 种 phase 映射

### 15.5 教训要点（CLAUDE.md §122）

1. **只读并行边界**：`Memory.messages` / `agentRunner` 共享状态必须单 goroutine
   写入；任何"看似无害"的并发都会与 §92a 锁顺序或 `agentRunner` 单调用约定冲突。
2. **末尾追加**而非插入：辅助思考结果通过 prompt 末尾追加，符合 §111
   chatQueue 的"高注意力段"原则（LLM 对末尾段注意力最高）。
3. **room semaphore 总并发上限**：Σ bots × perBotParallel ≤ `roomLLMConcurrency`
   （默认 8），worker fan-out 仍受其约束，**不是无限并发**。
4. **失败不计入 quarantine**：与 §110 retryable 失败冷却一致；否则 watchdog
   反复 wake 反而导致误 quarantine。
5. **测试环境无 conf 文件**：必须 `defer recover` 兜住 `config.Load()` 的 panic，
   否则单测全部失败。

### 15.6 未来扩展（不在 §122 范围）

- 若需要**并行 tool dispatch**（多 tool_use fan-out 派发），应通过 ToolRunner
  接口新增原子批操作方法（如 `DispatchToolsBatch`），让 manager 内部串行调度。
  §123+ 进一步讨论。
- 若需要**speculative LLM**（多模型并行投票选最优），应新增 `MultiModelProvider`
  抽象,与 §122 的"只读并行推理"路径正交。
