# Agent 交互模块设计(替代 Agent 思考)

> **日期**: 2026-07-09
> **状态**: 已落地 + 已端到端验证(2026-07-10)
> **范围**: 狼人杀 7 人局 Agent 决策可观测性的概念、数据流、契约与渲染
> **关联**: `docs/狼人杀-Agent与系统/狼人杀Agent设计.md`、CLAUDE.md §15(狼人杀 7 人局 Agent 驱动)
> **提交**: `1297f1e 重构: 狼人杀 Agent 思考 → Agent 交互(去 CoT 噪声, 改用决策可观测性)`

---

## 0. 一句话产品视角(给非技术读者)

> 7 个 bot 打狼人杀时,观众在侧边栏能看到的不是 LLM 的"思考过程"(噪声大、还容易暴露身份),
> 而是 LLM **实际收到了什么数据 + 实际做了什么决定**。这是一个**"输入 → 输出"**的
> 可观测面板:左边显示 4 个数字(阶段 / 存活 / 收到发言 / 500K 队列),右边显示
> **动作 + 入参 + 结果 + 成功/失败**。

为什么观众之前要看"思考"?
- 我们曾经假设 LLM 的 CoT(Chain of Thought,思考链)能体现推理质量。
- 但 R50+ 多轮观察发现:大多数模型的 CoT 是 "让我看看情况" 这种空话;
  少数有意义的 CoT 又会泄露身份(违反身份保密硬约束)。

为什么现在只看"交互"?
- LLM 的输入已经包含 **500K 队列 + 玩家档案 + RecentSpeeches + WhisperInbox** 等全部上下文。
- LLM 的输出就是一个 tool_use(`speak / wolf_kill / vote` 等结构化调用)。
- **输入数据全 + 输出是结构化动作 → 中间的"思考"对观众没有任何信息增量**。

技术收益:
- 单 bot wire payload 由 ~2KB(1200 字 CoT) 降到 ~0.4KB(决策字段),
  7 bot 房间总推送 -80%。
- 移除 LLM 身份泄露路径(敏感字段在脱敏层加 `sensitiveToolInputs` 表,
  LLM 即使在 input 里塞敏感字段名,前端也只能看到 `[已隐藏]`)。
- 旧 `AgentThoughtPanel.tsx` 仍保留(deprecated),新 `AgentInteractionPanel.tsx`
  上线后,`WerewolfGamePage.tsx` 一行 import 切换即可回滚。

---

## 1. 重构动机(为什么不再做"思考")

旧 `Agent 思考` 面板(`AgentThoughtPanel.tsx` + `BotTranscript.LastThinking / FullThinking / RecentMessages`)
的设计假设是:**LLM 的 CoT(reasoning text)是给观众看的"思考过程"**。

但 R50+ 多轮观察发现:

1. **CoT 几乎全是噪声** —— ClaudeCode / DeepSeek / GLM / Kimi / Qwen / DouBao 在 `speak / wolf_kill / vote / seer_check` 这类
   结构化决策下的 CoT 主要包含:
   - "我需要做出选择"、"让我看看其他玩家"、"我要根据情况行动"—— 完全没有可观察价值。
   - 个别 CoT 含身份推理("我猜 4 号是预言家")—— 这是**身份泄露**(违反 R73 §硬约束、违反 §身份保密),
     必须脱敏(已在 `sanitizeBotTranscript` 中清空,代价是观众什么也看不到)。
2. **观众真正想看的是"决策"而非"思考"** —— 7 bot 房间里观察者真正关心的:
   - 上一轮 LLM 收到了什么(玩家发言、whisper、phase 信息、500K 队列摘要)
   - LLM 做了什么决策(工具名 + 目标 seat + 文本)
   - 决策结果成功/失败、为什么失败
3. **数据"全"就不需要思考** —— 当前 LLM 已经能拿到完整的 500K 队列 + RecentSpeeches + WhisperInbox + 玩家档案,
   没有任何"思考过程"是对观众有信息增量的;而脱敏后观众看到的内容**比真实 CoT 还失真**。
4. **5/7 模型不再发 thinking 块** —— R50+ 切换到 Anthropic 协议 + `thinking` 块默认关闭,大部分模型的 CoT 长度 ≤ 20 字;
   继续展示 1200 字"思考"框严重浪费侧边栏空间。

因此本次重构将"Agent 思考"**改造为"Agent 交互"** —— 观众只看到 LLM 真正产生的
**输入(数据) + 输出(动作)**;**思考过程完全不上 wire 协议**。

---

## 2. 新概念:`AgentInteractionEvent`

### 2.1 一句话定义

`AgentInteractionEvent` = "一次 LLM 决策轮的可观测轨迹",由 4 段组成:

| 段 | 含义 | 数据源 | 大小约束 |
|---|------|--------|---------|
| `input` | LLM 这次调用收到了什么 | `GameContext` + 500K 队列 + Memory.Snapshot | ≤ 200 字摘要(不复制原文) |
| `tool_use` | LLM 决定调用的工具 + 入参 | `resp.Content` 中所有 `tool_use` 块 | 入参按敏感表脱敏 |
| `tool_result` | 工具返回结果 | `recordToolResult` / DispatchTool | 前 80 字截断 |
| `outcome` | 成功 / 失败 / 跳过 / Quarantine | `err != nil` / 业务层结果 | 12 字以内 |

### 2.2 与旧 `BotTranscript` 的字段映射

| 旧字段(删除/降级) | 新字段 | 备注 |
|---|---|---|
| `LastThinking`  (CoT 摘要) | `LastDecisionSummary` (一句话) | 仅记录"狼人 X 号 → 杀 Y 号"这种动词+目标 |
| `FullThinking`  (无截断 CoT) | **删除** | 永远不下发 |
| `RecentMessages` (LLM 输入摘要) | `DecisionInputs` (输入摘要) | 提取 GameContext 关键段,不复制 CoT |
| `ToolCalls`     (tool 名+结果) | `LastTool` + `LastToolInput` + `LastToolResult` | 拆分入参和结果 |
| `LastTool`      (上一条工具) | `LastTool` | 不变 |
| `UpdatedAt`     (毫秒) | `UpdatedAt` | 不变 |
| `Quarantined`   (布尔) | `Quarantined` | 不变 |
| `QuarantineReason` | `QuarantineReason` | 不变 |
| `ChatHistoryBytes / Cap / LastCompressionAt` | 保留 | 500K 队列统计,与决策正交 |
| `SpeakCountLastMin` | 保留 | 发言计数 |
| `LLMCallInProgress / LLMCallStartedAt` | 保留 | LLM 调用实时指示器 |

**核心变化**: wire 协议上**不再出现 LLM 的 CoT 文本**;只出现"输入摘要 + 工具调用 + 结果"。

### 2.3 新增字段语义

```go
type BotTranscript struct {
    Seat              int    `json:"seat"`
    Model             string `json:"model"`
    UpdatedAt         int64  `json:"updated_at"`

    // ==== 决策可观测性(替代旧"思考") ====
    LastDecisionSummary string `json:"last_decision_summary,omitempty"` // 1 句话
    LastTool            string `json:"last_tool,omitempty"`              // tool name
    LastToolInput       string `json:"last_tool_input,omitempty"`        // JSON 字符串(脱敏后)
    LastToolResult      string `json:"last_tool_result,omitempty"`       // 前 80 字
    LastOutcome         string `json:"last_outcome,omitempty"`           // "OK" / "FAIL" / "skip" / "idle" / "quarantine"
    DecisionInputs      string `json:"decision_inputs,omitempty"`        // 决策输入摘要(玩家发言数 / whisper 数 / 工具结果数)

    // ==== 旧字段,保留 wire 兼容但前端不再渲染 ====
    LastThinking   string   `json:"last_thinking,omitempty"`   // 置空
    FullThinking   string   `json:"full_thinking,omitempty"`   // 置空
    RecentMessages []string `json:"recent_messages"`           // 仍返回空切片
    ToolCalls      []string `json:"tool_calls"`                // 仍返回空切片

    // ==== 状态字段 ====
    Quarantined        bool   `json:"quarantined,omitempty"`
    QuarantineReason   string `json:"quarantine_reason,omitempty"`
    ChatHistoryBytes   int    `json:"chat_history_bytes"`
    ChatHistoryCap     int    `json:"chat_history_cap"`
    LastCompressionAt  int64  `json:"last_compression_at,omitempty"`
    SpeakCountLastMin  int    `json:"speak_count_last_min"`
    LLMCallInProgress  bool   `json:"llm_call_in_progress,omitempty"`
    LLMCallStartedAt   int64  `json:"llm_call_started_at,omitempty"`
}
```

**JSON 兼容**:
- 旧字段保留 → 旧前端不会崩(`last_thinking` 变空,触发 §4 兼容路径)
- 新字段添加 → 新前端 `AgentInteractionPanel.tsx` 渲染

---

## 3. 4 阶段交互流水线(后端生产路径)

LLM 一次决策轮分 4 个**生产路径阶段**(不是时间阶段),每阶段都向 `BotTranscript` 注入一个数据点:

```
┌────────────────────────────────────────────────────────────────────┐
│  handleEvent()                                                      │
│  ┌──────────────┐                                                  │
│  │ STAGE 1 收集 │ BuildUserPrompt(ctx) → GameContext               │
│  │   输入       │ → 玩家档案 / RecentSpeeches / WhisperInbox      │
│  │   情报       │ → 500K 队列 tail(共享 ReadPointer)               │
│  └─────┬────────┘                                                  │
│        ▼                                                           │
│  ┌──────────────┐                                                  │
│  │ STAGE 2 工具 │ BuildTools(phase, role, seat, alive, …)         │
│  │   调色板     │ → LLM 可选工具集                                │
│  └─────┬────────┘                                                  │
│        ▼                                                           │
│  ┌──────────────┐    ┌─────────────────────────┐                   │
│  │ STAGE 3 LLM  │───▶│ Provider.Chat(Streaming)│                   │
│  │   决策       │    │   → resp.Content[]      │                   │
│  │              │    │     {type:text|tool_use}│                   │
│  └─────┬────────┘    └────────────┬────────────┘                   │
│        │                          │                                │
│        │                          ▼                                │
│        │                ┌──────────────────┐                       │
│        │                │ STAGE 4 派发     │                       │
│        │                │   DispatchTool   │                       │
│        │                │   → tool_result  │                       │
│        │                └────────┬─────────┘                       │
│        │                         ▼                                  │
│        │                recordTranscript()                         │
│        │                → BotTranscript{...}                       │
│        ▼                                                           │
│  ResetConsecutiveFailures / quarantine / reWake                   │
└────────────────────────────────────────────────────────────────────┘
```

### 3.1 STAGE 1:收集输入情报

入口:`run.go:374-378` 的 `a.Memory.Push(BuildUserPrompt(evt.Context))`。

**生产路径**:
1. `BuildUserPrompt(ctx)` 把 ctx 渲染成 user message 文本(Memory 留全量,prompt 给 LLM)。
2. **新增** `BuildInputSummary(ctx)` 提取关键字段(只读 GameContext)生成 ≤ 200 字摘要,供 BotTranscript 用:
   - 玩家编号 + 角色
   - 阶段 + 轮数
   - 存活玩家数
   - 收到的发言数(RecentSpeeches 长度)
   - 收到的 whisper 数
   - 500K 队列新增条数(ReadPointer 增量)
   - **不含** CoT、**不含** LLM 输入的全文

**契约**:
- `BuildInputSummary(ctx) string` — 纯函数,无副作用
- 单测覆盖:ctx 为零值 / 长 RecentSpeeches / 空 WhisperInbox / 大 500K 队列

### 3.2 STAGE 2:工具调色板

入口:`run.go:399` 的 `BuildTools(phase, role, seat, alive, …)`。

**生产路径**(基本不变):按 phase + role 过滤出 LLM 可调的工具集;不修改。

**新增**:`BuildToolPaletteSummary(tools []llm.ToolDef) string` 生成 ≤ 80 字的工具列表摘要(逗号分隔)供 BotTranscript 用:

```
[night_wolves] wolf_kill / finish_speak / idle_think
[speak] speak / finish_speak / vote / sheriff_candidate / idle_think
```

### 3.3 STAGE 3:LLM 决策

入口:`run.go:499` 的 `resp, err := a.callProvider(ctx, req)`。

**生产路径**:
1. LLM 返回 `resp.Content[]`,可能含 `text` 块(CoT)+ `tool_use` 块。
2. **本重构关键**:Memory 仍记录完整 LLM 响应(`a.recordAssistant(resp)`,供下一轮 LLM 多轮),
   但 **BotTranscript 不再下发展示 text 块**。
3. 抓取最后一个 `tool_use` 块(本轮决策输出),提取 name / input / id,暂存到
   `decisionState.lastToolUse`,由 STAGE 4 消费。

**契约**:
- `a.recordAssistant(resp)` 不变
- `BotTranscript.LastThinking` 置空(原来调 `a.Memory.LastThinking(1200)` 现在改 `""`)
- `BotTranscript.FullThinking` 置空(原来调 `a.Memory.LastThinking(0)` 现在改 `""`)

### 3.4 STAGE 4:派发与结果

入口:`run.go:733-740` 的 `recordAssistant → recordTranscript → 各工具 DispatchTool`。

**生产路径**:
1. `recordTranscript()` 在每次 `recordAssistant` 之后调用,**重写内部字段填充**:
   - `LastDecisionSummary` = `toolName + " → " + targetSeat` (≤ 30 字)
   - `LastTool` = tool name
   - `LastToolInput` = input 的 JSON 序列化字符串(经 `sanitizeToolInput` 脱敏)
   - `LastToolResult` = 工具结果前 80 字
   - `LastOutcome` = "OK" / "FAIL" / "skip" / "idle" / "quarantine"
   - `DecisionInputs` = STAGE 1 的摘要
2. 工具 dispatcher `DispatchTool(name, input, runner)` 不变;结果经
   `recordToolResult` 反馈给 LLM(`Memory.Push(tool_result)`)。
3. 如果 LLM end_turn 无工具调用,`run.go:914` 的 `appendIdleAuditLine` 仍记录到
   `RecentMessages`(供前端统计 idle 次数),但**不在 BotTranscript 下发**。

---

## 4. 兼容策略(灰度 & 回滚)

### 4.1 wire 协议兼容

- **保留** 旧字段 `last_thinking / full_thinking / recent_messages / tool_calls`,**置空** 即可。
  - 旧 `AgentThoughtPanel.tsx` 仍能读 `last_thinking` 字段(空字符串走"暂无"占位)。
  - 旧 `ChatQueueModal.tsx` 不读 `BotTranscript`,无影响。
- **新增** 字段 `last_decision_summary / last_tool_input / last_tool_result / last_outcome / decision_inputs`。
  - 旧前端不识别 → JSON 序列化时这些字段会被忽略(多余字段不报错)。
  - 新前端 `AgentInteractionPanel.tsx` 优先读新字段;若不存在,回退读旧字段(纯前端兜底)。

### 4.2 前端兼容

- 新建 `AgentInteractionPanel.tsx`,数据契约对齐新字段。
- **保留** `AgentThoughtPanel.tsx` 文件(不删除),`WerewolfGamePage.tsx` 切换 import
  路径到 `AgentInteractionPanel`;旧文件加 deprecated 注释作为兜底。
- 若 7 人局回归测试发现新面板信息不足,可一行回滚 import 路径。

### 4.3 后端兼容

- `BotTranscript` 字段顺序不影响 JSON 序列化;新增字段不需要序列化顺序保证。
- `recordTranscript()` 改造是**单点修改**(`agent.go:595-643`);测试用例
  `agent_test.go` 已覆盖 `LastThinking / RecentMessages`,需要新增 5 个新字段的
  单测 + 1 个 wire 协议 JSON 字段快照测试。

---

## 5. 脱敏边界(与 sanitizeBotTranscript 协调)

### 5.1 服务端 → 客户端的脱敏点

| 字段 | 观战者 | 人类玩家(混合模式) |
|------|--------|-------------------|
| `LastDecisionSummary` | 完整 | 完整(动作+目标不泄露身份) |
| `LastTool` | 完整 | 完整 |
| `LastToolInput` | **脱敏**(`seer_check target=2` → `seer_check target=[已隐藏]`) | **脱敏**(同上) |
| `LastToolResult` | 前 80 字 | 前 80 字(可能含"X 是好人阵营",观战者可见 / 玩家可见) |
| `LastOutcome` | 完整 | 完整 |
| `DecisionInputs` | 数字摘要(无身份) | 数字摘要(无身份) |
| `QuarantineReason` | 完整(`403 / context canceled` 等) | **空**(`sanitizeBotTranscript` 早已清空,R55 P2) |

### 5.2 敏感工具表(扩展自 `sensitiveToolNames`)

```go
// sensitiveToolInputs: 工具名 → 哪些 input 字段必须脱敏
var sensitiveToolInputs = map[string]map[string]bool{
    "wolf_kill":    {"target": true},
    "seer_check":   {"target": true},
    "witch_act":    {"target": true, "action": true}, // action=poison 揭示意图
    "hunter_shoot": {"target": true},
    "vote":         {"target": true}, // 投票目标公开(已经在 Votes map 公开),但 vote_skip 不脱敏
    "whisper":      {"text": true, "to_seat": true}, // whisper 全文对观战者隐藏
}
```

### 5.3 sanitizeBotTranscript 改造

`ServerGo/game/werewolf/room.go:3046` 函数改造:

1. 保留对 `LastThinking / QuarantineReason / RecentMessages` 的清空(不变)。
2. 对 `LastToolInput` 按 `sensitiveToolInputs` 替换敏感字段为 `[已隐藏]`。
3. 对 `LastToolResult` 仍前 80 字(可能含阵营信息,但观战者可见 / 玩家也可见 —— 阵营信息在
   `Status==over` 或 `RoleRevealed==true` 时本就可广播)。

---

## 6. 前端渲染:AgentInteractionPanel

### 6.1 与 AgentThoughtPanel 的对比

| 区块 | AgentThoughtPanel(旧) | AgentInteractionPanel(新) |
|------|----------------------|--------------------------|
| 顶部 toggle | "🤖 Agent 思考 (N)▸" | "🤖 Agent 交互 (N)▸" |
| Summary 行 | 观众消息 / 已思考 / idle / 500K / 60s 发言 | **保留**(数字不变) |
| tabs | 1号~7号 + 模型名 + LLM 调用中圆点 | **保留** |
| Quarantine 徽章 | "已禁用 · 5连失败" | **保留** |
| LLM 调用中指示器 | "🤖 正在调用大模型…" | **保留** |
| **决策输入区**(新) | (无) | 4 列:阶段 / 轮数 / 发言数 / whisper 数 / 500K 队列增量 |
| **决策输出区**(新) | (无) | 1 行:`{summary}  →  {LastTool}({LastToolInput 摘要})  →  {outcome}` |
| 工具结果区 | "最近工具"(单行 LastTool) | "工具调用"(`tool: result` 列表,前 80 字,3-5 条) |
| 最近思考 | `last_thinking` 长文 | **删除** |
| 完整思考折叠 | `<details>` 展开完整 CoT | **删除** |
| 消息摘要 | `recent_messages[]` 滚动列表 | **删除**(改由 DecisionInputs 数字摘要) |
| 工具调用 | `tool_calls[]` 完整列表 | **保留,精简为 5 条** |
| 更新时间 | 保留 | 保留 |

### 6.2 决策输入区布局(每列宽 50%)

```
┌─────────────────────────────┬─────────────────────────────┐
│ 📥 决策输入                   │ 📤 决策输出                   │
├─────────────────────────────┼─────────────────────────────┤
│ 阶段: speak (第 3 天)       │ 动作: speak                  │
│ 存活: 5 / 7                 │ 入参: text=…前 30 字         │
│ 收到发言: 12 条              │ 结果: ✅ OK(80 字)            │
│ 收到 whisper: 2 条           │ 状态: ✅ 发言成功              │
│ 500K 队列: +5 条             │                              │
│ 工具调色板: speak/vote/...  │                              │
└─────────────────────────────┴─────────────────────────────┘
```

### 6.3 文件改动

- **新建** `ClientWeb/src/components/werewolf/AgentInteractionPanel.tsx`(替代 AgentThoughtPanel)
- **保留** `ClientWeb/src/components/werewolf/AgentThoughtPanel.tsx`(deprecated 注释)
- **修改** `ClientWeb/src/pages/WerewolfGamePage.tsx:12,199-213` 切换 import + 标题文案
- **保留** `ClientWeb/src/types/werewolf.ts BotContextJSON` 新增 5 字段
- **保留** `ClientWeb/src/styles/globals.css` 旧样式,新增 `ww-agent-interaction-*` 样式

---

## 7. 与现有架构的协同

### 7.1 观战者互动 / 发言下限 / Quarantine

- **观战者互动 summary**(`computeSpectatorStats`):仅读 `recent_messages` 中的 `[idle_thinking]`
  审计行和 `[HH:MM:SS] 观战-` 前缀,**不受影响**(recent_messages 仍下发空切片,这些统计改由
  `LastOutcome == "idle"` + 500K 队列 IsSpectator 计数兜底)。
- **发言下限**(`speak_floor.go`):不动,与决策可观测性正交。
- **Quarantine**(`SetQuarantined` + `publishQuarantineTranscript`):不动;新面板只是把
  `Quarantined / QuarantineReason` 字段改放到"决策输出区"顶部。
- **500K 队列 ReadPointer**:不动;只是从 `RecentMessages[]` 文本摘要改为
  `DecisionInputs.队列增量` 数字。

### 7.2 LLM 调用实时指示器

`LLMCallInProgress / LLMCallStartedAt` 字段保留,新面板继续渲染"🤖 正在调用大模型…已等待 Ns"。

### 7.3 500K 队列 Modal

`ChatQueueModal.tsx` 不读 `BotTranscript`,无影响。

### 7.4 自动化测试

- `go test ./ServerGo/agent/...` 已有 `agent_test.go / round26_test.go / chat_history_test.go / speak_dedup_test.go`。
  - 新增 `agent_interaction_test.go`:`TestRecordTranscript_NewFields / TestBuildInputSummary / TestSanitizeToolInput`
- `go test ./ServerGo/game/werewolf/...` 已有 `room_test.go` 覆盖 `sanitizeBotTranscript`。
  - 新增 `room_sanitize_interaction_test.go`:`TestSanitizeBotTranscript_NewFields / TestLastToolInput_Leakage`

---

## 8. 实施步骤(对照 CLAUDE.md 13 流程)

1. ✅ **game-designer**(本文件)— 产出设计文档
2. **backend-dev** — 改 `ServerGo/agent/agent.go` 的 `BotTranscript` 结构 + `recordTranscript`,
   加 `BuildInputSummary` + `BuildDecisionSummary` + `sanitizeToolInput`;改
   `ServerGo/game/werewolf/room.go` 的 `sanitizeBotTranscript`。
3. **frontend-dev** — 新建 `AgentInteractionPanel.tsx`,改 `WerewolfGamePage.tsx` 切换 import,
   新增 5 字段到 `types/werewolf.ts`。
4. **integration-tester** — `go test ./...` + `tsc --noEmit` + `npm run build` +
   e2e:启动 7 bot 房间,验证侧边栏"🤖 Agent 交互 (7)"展开后 7 个 tab 都有
   "决策输入 / 决策输出"区块。
5. **git 中文提交** — `git add + git commit -m "重构: 狼人杀 Agent 思考 → Agent 交互 (去 CoT 噪声, 改用决策可观测性)"`

---

## 9. 验收清单

- [ ] `go build -o LsmAgentGame main.go` 通过
- [ ] `go test ./...` 通过(包含新增 `agent_interaction_test.go` + `room_sanitize_interaction_test.go`)
- [ ] `tsc --noEmit` 通过
- [ ] `npm run build` 通过
- [ ] 7 bot 房间启动后,观战者侧边栏 "🤖 Agent 交互 (7)" 展开 → 7 个 tab → 每个 tab 看到
      "📥 决策输入" + "📤 决策输出" + 精简 "工具调用" 列表
- [ ] wire payload 大小对比:旧版每 bot ~2KB(1200 字 CoT) → 新版每 bot ~0.4KB
      (`last_decision_summary(50) + last_tool_input(60) + decision_inputs(100) + 5 tool calls(50) ≈ 260 字节`)
- [ ] 人类玩家(混合模式)面板:敏感工具的 `last_tool_input.target` 字段全部为 `[已隐藏]`
- [ ] Quarantine bot 仍显示"已禁用 · 5连失败"徽章,不在决策输出区出现工具调用
- [ ] LLM 调用中:仍显示"🤖 正在调用大模型…已等待 Ns"指示器
- [ ] 500K 队列 modal 不受影响(数据源正交)

---

## 10. 关联索引

- `ServerGo/agent/agent.go:260-298` — `BotTranscript` 结构定义
- `ServerGo/agent/agent.go:585-643` — `recordTranscript` 实现
- `ServerGo/agent/memory.go:200-292` — `LastThinking / RecentMessages` 旧实现
- `ServerGo/agent/run.go:733-741,1053-1054` — `recordTranscript` 调用点
- `ServerGo/game/werewolf/room.go:2877-3006` — `populateBotContexts` 广播路径
- `ServerGo/game/werewolf/room.go:3046-3078` — `sanitizeBotTranscript` 脱敏
- `ServerGo/game/werewolf/view.go:80-86` — `ClientGameState.BotContexts` 字段
- `ClientWeb/src/components/werewolf/AgentThoughtPanel.tsx` — **2026-07-10 已废弃**(throw stub)
- ~~`ClientWeb/src/components/werewolf/AgentInteractionPanel.tsx`~~ — **2026-07-10 已删除**
- **2026-07-10 §重构新组件**:
  - `ClientWeb/src/components/werewolf/BotPhaseIndicator.tsx` — 单 bot 多态指示器(5 态)
  - `ClientWeb/src/components/werewolf/WerewolfThinkingHeader.tsx` — 全局聚合 Header
  - `ClientWeb/src/components/werewolf/ThinkingDots.tsx` — 1.5s 错峰跳点
- `ClientWeb/src/components/werewolf/WerewolfTable.tsx:80-200` — SeatCell 集成 BotPhaseIndicator
- `ClientWeb/src/types/werewolf.ts:113-143` — `BotContextJSON` 类型
- `docs/狼人杀-Agent与系统/狼人杀对话即思考设计.md` — 5 态状态机详细设计
- `docs/werewolf-protocol.md` — BotTranscriptJSON 字段说明
- CLAUDE.md §15 — 狼人杀 7 人局 Agent 驱动
- CLAUDE.md §111-115 — 500K 队列 + 发言下限 + 阶段时钟 + 共享 ReadPointer

---

## 7. 2026-07-10 §重构 — 僵尸组件清理

### 7.1 背景

§重构 前:`AgentThoughtPanel.tsx`(deprecated)与 `AgentInteractionPanel.tsx`
**均未被任何 page import**。`WerewolfGamePage.tsx` 的 import 列表不含两者之一,
玩家实际看到的指示器是 `WerewolfTable.tsx::SeatCell` 内嵌的轻量版(单一
"🤖 思考中…"文案,无倒计时、无 phase 区分)。豪华面板的所有信息(决策输入/输出区、
500K 队列条、内心独白折叠)全部丢失。

### 7.2 处理

- **删除** `ClientWeb/src/components/werewolf/AgentInteractionPanel.tsx`(整个文件)
- **改写** `ClientWeb/src/components/werewolf/AgentThoughtPanel.tsx` 为 throw stub:
  任何外部代码若仍尝试 import,运行时立即抛错,避免静默渲染旧 UI

### 7.3 替代组件

§重构 后改用三个新组件,均内嵌到 `WerewolfTable.tsx` 不再依赖 sidebar 折叠面板:

| 旧组件 | 新组件 | 位置 |
|--------|--------|------|
| AgentInteractionPanel(豪华) | **删除** | — |
| AgentThoughtPanel(简单) | **废弃**(throw stub) | — |
| (无全局 Header) | **WerewolfThinkingHeader**(新建) | `WerewolfTable` 顶部 |
| SeatCell 内 "🤖 思考中…" | **BotPhaseIndicator**(新建) | SeatCell 内 |
| (无 ThinkingDots) | **ThinkingDots**(新建) | BotPhaseIndicator 内 |

### 7.4 视觉/功能增强

| 维度 | 旧 | 新(§重构) |
|------|----|-----------|
| 阶段状态 | 二态(in-progress / 否) | 5 态(idle/calling/streaming/retrying/quarantined) |
| 倒计时 | 仅 in-progress 时显示"已等待 Ns" | 任意 active phase 都显示 |
| 文案分档 | 单一文案 | 3 档(即将发言/思考中 Ns/深度思考中 Ns) |
| 重试反馈 | 不可见 | "重试 N/M" + 倒计时 + 5xx/429/timeout 分色 |
| 全局聚合 | 无 | "[●●●●●●●○○○○○○○ 7/13 思考中]" Header |
| a11y | 部分 | `prefers-reduced-motion` 全降级 + aria-live polite |

### 7.5 教训

> **设计文档不等于实际渲染**。`AgentInteractionPanel.tsx` 在代码层有完整的
> 设计与实现,但没人去 `WerewolfGamePage.tsx` 加 import,导致整面板在生产中
> 永远不被显示。**§重构后强制将所有面板逻辑内嵌到 `WerewolfTable.tsx`,消除
> "组件存在但未挂载"的盲点**。

