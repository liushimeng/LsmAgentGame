# 德州扑克 Agent 聊天系统设计与实现方案

> 2026-08-23。对齐狼人杀 13 人局 Agent 聊天体系(`ServerGo/agent/wwplayer/` + `ServerGo/agent/core/chat_history.go` + `ServerGo/ws/chat_service.go`),
> 补齐德州扑克(`ServerGo/agent/thpagent/`)的聊天互动闭环。
> 事实来源:狼人杀实现见 `docs/狼人杀-Agent与系统/狼人杀Agent设计.md`;本文档是德扑侧的唯一事实来源。

## 1. 现状与差距分析(2026-08-23 核实)

### 1.1 已存在(不重做)

| 能力 | 实现位置 |
|---|---|
| `agent_seats` 建房(≤6 座) | `service/room_service_crud.go:208-221` → `ws/game_service_texas_bot.go:107 registerTexasHoldemAgentSeats` |
| LLM 决策 + `poker_chat` 工具 | `agent/thpagent/driver.go:403-423`(§B5:去重+限流后挂 `Action.ChatText`) |
| 公屏发言广播 + 落库 | `ws/game_service_texas_bot.go:394 sendBotChat` → `ChatService.SendFromBot`(`ws/chat_service.go:821`,写 `t_lsm_game_chat_message` + 广播 `chat.message`) |
| 发言限流 | `agent/thpagent/dispatch.go`:每手 ≤2 次 + 相邻 ≥30s token bucket |
| 前端聊天面板 | `components/chat/GameChatPanel.tsx` 已挂于 `TexasHoldemGamePage.tsx:251-253` |
| Bot 内心独白(脱敏) | `game/texasholdem/view.go:24 sanitizeBotThought` + `components/texasholdem/BotThoughtPanel.tsx` |
| Bot 回合 watchdog | `ws/game_service_texas_bot.go:429`(超时强制 fold) |

### 1.2 三大缺口(本方案交付)

1. **Agent 看不到聊天**(P0):thpagent 决策上下文不注入任何玩家/bot 公屏消息。Bot 只会自言自语、每手最多 2 条,且无法回应人类发言 —— 用户感知即"Agent 不能发言/不会聊天"。
2. **无流式发言帧**(P1):缺少狼人杀的 `chat.stream_start / chat.stream_delta / chat.stream_end` 三帧,前端无打字机效果,长发言体验差。
3. **无上下文压缩体系**(P1):thpagent Memory(`memory.go`)仅 5 手 `HandRecord` + `OpponentStats`,无字节预算、无 60%/80%/100%/400% 四梯度压缩、无 LLM 语义压缩、无跨局 MEMORY.md 迭代(MemoryIter)。

## 2. 目标架构

```
玩家/Bot 发言 ──► ChatService.SendFromBot / SendRoom
                    │ 落库 t_lsm_game_chat_message(已有)
                    │ 广播 chat.message(已有)
                    ▼
        ChatHistoryQueue(per-room, 500KB, agent/core/chat_history.go 复用)
                    │ WindowFor(seat) —— ReadPointer 增量窗口
                    ▼
        thpagent.Driver.DecideAction 决策上下文新增「牌桌闲聊」段
                    │ LLM 输出 poker_chat tool_use
                    ▼
        Dispatcher.DispatchPokerChat(去重+限流) → Action.ChatText
                    ▼
   SendBotStreamStart/Delta/End(流式三帧) + SendFromBot(终帧落库)
```

## 3. 后端设计

### 3.1 共享 500K 聊天队列注入(复用 agent/core)

- **完全复用** `agent/core/chat_history.go` 的 `ChatHistoryQueue`(cap 500KB,四步压缩:相邻同 sender 合并 / 单条 >1KB 截断 / 超 cap 从队首 pop 至 80% / >100 条压最旧 30 条)。
- 德扑侧新增 per-room 队列持有:挂在 `ws/game_service_texas_bot.go` 的 `thpChatQueues sync.Map[roomID]*agentcore.ChatHistoryQueue`(与狼人杀一致:队列是内存态、随房间生命周期,落库线独立)。
- **写入点**:
  - bot 发言:`sendBotChat` 成功后 `queue.Append(...)`(与狼人杀 `emitRoomMessage` 对齐);
  - 人类发言:`ChatService` 房间消息回调已 `emitRoomMessage` 到狼人杀队列 —— 德扑通过 `chatSvc` 注册的房间回调或 `game_service` 侧订阅同一消息流写入德扑队列(见 §3.4 接线清单)。
- **读取点**:`BotGameContext`(或 Driver 新参)注入 `ChatWindow string` —— `queue.WindowFor(seat)` 取"自上次消费后"的增量并 `Advance(seat)`。注入到 `prompt.go` 决策上下文,段名「牌桌闲聊(增量)」,并给 LLM 提示:*可以在 poker_chat 中回应他人的发言,但注意不要泄露自己的底牌信息*。
- **公平性硬约束**:队列窗口只含公屏消息(私聊不进队列);窗口文本经现有脱敏路径(不包含任何 Hole 卡信息 —— 队列里本来就没有)。

### 3.2 发言策略放宽(修复"不能发言"感知)

- `dispatch.go` 限流调整:每手 ≤2 → **每手 ≤3**,相邻间隔 30s → **20s**(德扑一手多街,多阶段都有决策触发点,原参数过紧)。
- prompt(`prompt.go:74`)同步更新,并新增指引:被点名/被挑衅时应优先考虑回应;win/loss 关键手(摊牌后)鼓励一句情绪化短评。
- 新增触发点(不改引擎):`runHandOverEpilogue` → `onHandOver` 后,若本手有摊牌且 bot 参与,允许一次「结算闲聊」(同样走 DispatchPokerChat 限流;复用现有 LLM 决策通道的 summary 上下文,失败静默)。

### 3.3 流式发言三帧(复用 ChatService)

- 德扑发言链路改为:`DispatchPokerChat` 通过 → `chatSvc.SendBotStreamStart(roomID, botUserID, modelKey)` → 逐段 `SendBotStreamDelta`(按 12~24 rune 分片,index 递增)→ `SendBotStreamEnd` + `SendFromBot` 终帧落库。
- 前端 `useStreamingMessages.ts` 已实现三帧消费,德扑无需新增前端帧处理。
- 流式失败(如 chatSvc nil)回退:直接 `SendFromBot` 一次性发送(保证不因流式挂掉而丢发言)。

### 3.4 上下文压缩体系(四梯度,对齐 wwplayer)

thpagent 的 Memory 与 wwplayer 结构不同(非 messages 队列),因此压缩目标对象是**决策 prompt 的字节量**(prompt 字节预算),四梯度:

| 梯度 | 触发 | 动作 | 实现 |
|---|---|---|---|
| 60% | `promptBytes > 0.6*budget` | 截断 ChatWindow 至最近 20 条 + RecentHands 保留 3 手 | 规则式,无 LLM |
| 80% | `> 0.8*budget` | 上述 + OpponentStats 仅保留同桌对手 + LLM 语义压缩 ChatWindow(摘要模板,走 llmSema,失败回退规则式) | 新增 `prompt_compress.go` |
| 100% | preflight | `PruneByBytes` 等价:ChatWindow 清空只留最近 5 条,HandRecord 只留 1 手 | 规则式 |
| 400%/context_exceeded 错误 | LLM 返回上下文超限 | Aggressive:全部可选段清空,仅保留当前街动作历史 | 规则式 |

- budget:`getModelContextBudget(modelKey)` 同款逻辑,默认 200KB(`DefaultMaxPromptBytes`)。
- **跨局 MEMORY.md 迭代(MemoryIter)**:新增 `AgentClassName = LsmAgentGame-TexasPoker-MemoryIter`(登记 `agent/class_names.go` + `AllAgentClassNames()` + 单测断言非空)。每局(房间打完/删除)后异步驱动:读取该 bot 的持久 Memory 文件,LLM 自我迭代(复用 wwplayer `memory_iterate.go` 的校验/HardTruncate/FallbackMerge 思路,德扑版精简为「风格画像 + 对手笔记」两段式);失败走 FallbackMerge。持久化沿用狼人杀的 memory 文件布局(`memory_persist.go` 德扑版,§130:写完必须 grep 接线验证)。

### 3.5 可见性(不动)

`sanitizeBotThought` 只脱敏 thought,聊天消息本就是公屏信息,不新增脱敏点。观战者能看到全部公屏聊天(现状)。

## 4. 前端设计

1. **GameChatPanel**:已支持 `chat.stream_*`(经 `useStreamingMessages`)。验证德扑房间流式三帧渲染;bot 消息的 model 徽章显示(现有 `from_role=bot` 逻辑)。
2. **BotThoughtPanel**:新增「本手闲聊」小节,数据源复用 500K 队列的房间级快照(随 `game.state` 下发的可选字段 `chat_window_preview`,仅最近 5 条、公屏、已脱敏)。
3. **PlayerSeat**:bot 发言瞬间座位气泡(≤3s)展示 ChatText(数据源 `chat.message` 帧,按 seat 匹配)。
4. 类型:`types/texasholdem.ts` 增加 `chat_window_preview?: string[]`;i18n zh-CN/en/ja 同步新增键。

## 5. 接线清单(§130 自检)

| 新增物 | grep 验证点 |
|---|---|
| `thpChatQueues` | `game_service_texas_bot.go` 写入点 + Driver 注入点 + 房间删除清理点 |
| `ChatWindow` 上下文字段 | `prompt.go` 渲染 + `driver_test.go` 断言包含 |
| `SendBotStreamStart/Delta/End` 德扑调用 | `game_service_texas_bot.go` + 前端 `useStreamingMessages`(已有) |
| `AgentClassTexasPokerMemoryIter` | `class_names.go` + `AllAgentClassNames()` + 单测 |
| 德扑 memory_persist | 房间生命周期触发点(局末/删除) |
| `chat_window_preview` | `view.go` 下发 + `types/texasholdem.ts` + BotThoughtPanel 渲染 |

## 6. 验收

1. `go test ./...`(ServerGo)全绿;`tsc --noEmit` + `npm run build`(ClientWeb)全绿。
2. 功能验收(2 bot + 1 人类):
   - 人类在房间聊天发言 → 下一个 bot 决策的 prompt 包含该消息(日志/prompt 测试断言);
   - bot 通过 `poker_chat` 发言 → 前端流式打字机 + 落库可在 `chat.history` 分页读回;
   - 构造超长 ChatWindow(>80% 预算)→ 触发 LLM 压缩,失败回退规则式(单测);
   - 局末触发 MemoryIter 异步迭代,生成/更新 MEMORY.md。
3. `./rebuild_restart_app.sh` 重启后,房间恢复(`rehydrateTexasHoldemAgents`)仍能发言。

## 7. 实施顺序

1. backend-dev:§3.1 队列注入 + §3.2 限流放宽 + §3.3 流式帧(一次提交)。
2. backend-dev:§3.4 压缩 + MemoryIter(一次提交)。
3. frontend-dev:§4 前端增强(一次提交)。
4. integration-tester:§6 验收 + `./rebuild_restart_app.sh`。
