# 狼人杀 13 人局 Agent 工具集完整分析

> 本文档完整分析 `ServerGo/agent/` 下狼人杀（13 人标准竞技局）Agent 的 Anthropic 协议 `tools` 字段：**协议层 wire 类型**、`tools` 是固定还是动态生成、各工具在什么阶段被暴露给 LLM、在什么条件下可调用、映射到哪个引擎方法、结果如何回写 Memory、以及定义在哪些代码文件中。
>
> 上游规则文档：`docs/狼人杀13人标准局规则.md` / `docs/狼人杀-Agent与系统/狼人杀Agent设计.md`。

---

## 一、核心结论：`tools` 字段是「动态生成」的，不是固定的

工具集 **不是** 静态枚举，而是由 `ServerGo/agent/tools.go::BuildTools(phase, role, seat, alive, speakTurn, gc)` 在每轮 LLM 调用前 **实时构造**。LLM 只能从系统给出的工具清单里挑选——这是 Agent 设计的关键安全约束。

### 1.1 协议层 wire 类型（Anthropic `tools[]` 的真实形状）

`BuildTools` 返回的不是自定义结构，而是直接序列化为 Anthropic Messages API `tools[]` 数组的 `llm.ToolDef`：

`ServerGo/llm/types/types.go:120-126`：
```go
// ToolDef is an Anthropic `tools[]` entry. `InputSchema` must be a JSON Schema
// object (i.e. {"type":"object","properties":{...},"required":[...]}).
type ToolDef struct {
	Name        string         `json:"name" yaml:"name"`
	Description string         `json:"description" yaml:"description"`
	InputSchema map[string]any `json:"input_schema" yaml:"input_schema"`
}
```

`ServerGo/llm/types/types.go:136-140` — 挂在请求体上的字段：
```go
type LLMRequest struct {
	Model     string        `json:"model"`
	System    []SystemBlock `json:"system,omitempty"`
	Messages  []Message     `json:"messages"`
	Tools     []ToolDef     `json:"tools,omitempty"`   // ← 这里
	MaxTokens int           `json:"max_tokens"`
	Metadata  Metadata      `json:"metadata,omitempty"`
	...
}
```

> 因此每个工具暴露给 LLM 的契约 = `name`（工具名）+ `description`（自然语言描述，含策略提示）+ `input_schema`（JSON Schema 对象，`properties` + `required`）。LLM 看到的 `tools[]` 数组长度随当前阶段动态变化（最少 1 个 `emotion_switch`，最多 10+ 个在 `speak` 阶段）。

### 1.2 调用入口（共两处，均在 `run.go` 主循环内）

| 文件 | 行号 | 场景 |
|---|---|---|
| `ServerGo/agent/run.go` | `:510` | 主决策循环 `handleEvent` 内，正常 wake 事件 → 构造 LLM 请求 |
| `ServerGo/agent/run.go` | `:1246` | `handleSpeakFloorTick` 内，发言下限 watchdog 强提醒路径（跳过 `SpeakLimiter`） |

两处都走 `req := llm.LLMRequest{ ..., Tools: tools, MaxTokens: 1024, ... }`（`:531-535`、`:1260-1264`），经 `callProvider`（`:1400`）流式累加后，由 Provider 发到 Anthropic。

### 1.3 动态维度（共 6 个）

| 维度 | 作用 |
|---|---|
| `phase`（当前阶段） | 决定暴露哪一批工具（`switch phase` 主分支） |
| `role`（werewolf / seer / witch / hunter / idiot / villager） | 夜间阶段按角色二次过滤（如 `night_wolves` 仅 werewolf 见到 `wolf_kill`） |
| `seat`（0-12） | `speak` / `finish_speak` / `wolf_suicide` 仅对 `speakTurn == seat` 暴露 |
| `alive []int`（存活座位） | `target` 字段的 JSON Schema `enum` 由存活玩家实时生成（动态枚举） |
| `speakTurn` | 决定「当前发言座位」专属工具 |
| `gc *GameContext` | 警长座位、警徽流状态、`VoteProposed` 标志等上下文开关（`gc==nil` 时 sheriff_stream / propose_vote 不暴露，测试安全） |

---

## 二、完整工具清单（按阶段分组）

> 以下 12 个阶段分组 + 1 个「隐藏工具组」覆盖 `BuildTools` 与 `dispatchToolInner` 的全部工具名。
>
> 可用身份列：未标注 = 「所有存活玩家」；标注「仅 X」= 仅该身份或条件成立时可用。
>
> 映射列：`SendFromBot / SpeakWithThought / IdleSilent` 等是 `ToolRunner` 接口方法；`Action_*` 是 `WerewolfManager` 引擎方法。
>
> ⚠️ **易错点**：`death_lyric`（遗言）阶段在 `BuildTools` 主 switch 中 **没有分支**——遗言是 prompt 驱动的（`prompt.go` + `DeathLyricCurrent`），不是工具驱动。该阶段仅暴露 `emotion_switch`。详见 §2.11。

### 2.1 `PhasePreWolves` / `pre_wolves` — 首夜缓冲期

| 工具名 | 参数 | 映射 | 可用身份 / 条件 | 关键描述 |
|---|---|---|---|---|
| `speak` | `{text}` | `ChatService.SendFromBot` | 所有存活 | 🕯️ 强制发言（≤80 字），不绑定 SpeakTurn 座位，任何存活玩家可抢身份 |
| `speak_with_thought` | `{text, internal_thought}` | `SpeakWithThought` | 所有存活 | §119 心口不一，pre_wolves 专用「悍跳/挡刀」故事模板；text ≤80，thought ≤120 |
| `interject` | `{text}` | `SendFromBot(is_interject=true)` | 所有存活 | 插话/追问/制造话题，≤80 字；**不计入强制发言次数** |
| `whisper` | `{to_seat, text}` | `WhisperFromBot` | 所有存活（filterSelf） | 私聊 ≤1 次/60s；**不计入强制发言次数** |
| `idle_silent` | `{reason, role}` | `IdleSilent(role="player")` | 已发过言者 | 强约束：`PreWolvesCountForMySeat ≥ 1` 才能调；role ∈ {player, judge} |
| `emotion_switch` | `{emotion, reason}` | `EmotionSwitch` / `EmotionSwitchRandom` | 所有存活 | §124 10 类情绪，不消耗 speak/whisper 桶 |

### 2.2 `PhaseNightWolves` / `night_wolves` — 狼人夜间

| 工具名 | 参数 | 映射 | 可用身份 / 条件 |
|---|---|---|---|
| `wolf_kill` | `{target}` | `Action_WolfKill` | 仅 role=werewolf；target=-1 空刀；enum 主动追加 -1 哨兵 |
| `emotion_switch` | `{emotion, reason}` | 同上 | 所有存活 |

### 2.3 `PhaseNightSeer` / `night_seer` — 预言家夜间

| 工具名 | 参数 | 映射 | 可用身份 / 条件 |
|---|---|---|---|
| `seer_check` | `{target}` | `Action_SeerCheck` | 仅 role=seer；enum = filterSelf(alive, seat) |
| `emotion_switch` | `{emotion, reason}` | 同上 | 所有存活 |

### 2.4 `PhaseNightWitch` / `night_witch` — 女巫夜间

| 工具名 | 参数 | 映射 | 可用身份 / 条件 |
|---|---|---|---|
| `witch_act` | `{action, target}` | `Action_Witch` | 仅 role=witch；action ∈ {none, antidote, poison} |
| `emotion_switch` | `{emotion, reason}` | 同上 | 所有存活 |

### 2.5 `PhaseDawn` / `dawn` — 黎明阶段

| 工具名 | 参数 | 映射 |
|---|---|---|
| `start_day` | `{}` | `Action_StartDay` |
| `emotion_switch` | `{emotion, reason}` | 同上 |

### 2.6 `PhaseSpeak` / `speak` — 白天发言阶段（最丰富）

| 工具名 | 参数 | 映射 | 可用身份 / 条件 |
|---|---|---|---|
| `speak` | `{text}` | `SendFromBot` | **仅 `speakTurn == seat`** |
| `finish_speak` | `{}` | `Action_FinishSpeak` | **仅 `speakTurn == seat`** |
| `speak_with_thought` | `{text, internal_thought}` | `SpeakWithThought` | **仅 `speakTurn == seat`**；§119 心口不一 |
| `wolf_suicide` | `{}` | `Action_WolfSuicide` | role=werewolf 且 **speakTurn==seat**；慎用，不可逆 |
| `sheriff_stream` | `{slot, target}` | `Action_SheriffStream` | **gc.SheriffSeat==seat 且 role=seer**；slot ∈ {1,2}，target=-1 撤回 |
| `propose_vote` | `{}` | `ProposeVote` | role=seer 且 **!gc.VoteProposed**；预言家发起投票 |
| `interject` | `{text}` | `SendFromBot` | 所有存活 |
| `whisper` | `{to_seat, text}` | `WhisperFromBot` | 所有存活（filterSelf） |
| `emotion_switch` | `{emotion, reason}` | 同上 | 所有存活 |

### 2.7 `PhaseVote` / `vote` — 投票阶段

| 工具名 | 参数 | 映射 |
|---|---|---|
| `vote` | `{target}` | `Action_DayVote`（enum = filterSelf） |
| `finish_vote` | `{tied_round}` | `Action_FinishVote`；tied_round 可选，平票轮次 |
| `emotion_switch` | `{emotion, reason}` | 同上 |

### 2.8 `PhaseSheriff` / `sheriff` — 警长竞选阶段

| 工具名 | 参数 | 映射 | 可用身份 / 条件 |
|---|---|---|---|
| `sheriff_candidate` | `{target}` | `Action_SheriffCandidate` | target 只能是自己（`onlySelf(seat)` 单元素 enum） |
| `sheriff_elect` | `{}` | `Action_SheriffElect` | 系统调用 |
| `sheriff_stream` | `{slot, target}` | `Action_SheriffStream` | gc.SheriffSeat==seat 且 role=seer |
| `emotion_switch` | `{emotion, reason}` | 同上 | |

### 2.9 `PhaseHunterShoot` / `hunter_shoot` — 猎人开枪阶段

| 工具名 | 参数 | 映射 | 可用身份 / 条件 |
|---|---|---|---|
| `hunter_shoot` | `{target}` | `Action_HunterShoot` | 仅 role=hunter；target=-1 放弃；被毒杀不能开枪 |
| `emotion_switch` | `{emotion, reason}` | 同上 | |

### 2.10 `PhaseIdiotReveal` / `idiot_reveal` — 白痴翻牌阶段

| 工具名 | 参数 | 映射 |
|---|---|---|
| `idiot_reveal` | `{choice}` | `Action_IdiotReveal`（choice ∈ {reveal, skip}） |
| `emotion_switch` | `{emotion, reason}` | 同上 |

### 2.11 `PhaseDeathLyric` / `death_lyric` — 遗言阶段 ⚠️ 易错

| 工具名 | 参数 | 映射 |
|---|---|---|
| `emotion_switch` | `{emotion, reason}` | 同上 |

> **⚠️ 关键纠正**：`BuildTools` 的主 switch（`tools.go:161-419`）**没有** `death_lyric` 分支。遗言阶段 **不暴露** `speak` / `speak_with_thought` / `interject` / `whisper` 工具。遗言是 **prompt 驱动** 的：`prompt.go` 在 `ctx.Phase=="death_lyric"` 且 `DeathLyricCurrent==seat` 时注入遗言指令，LLM 通过 text-block 自动发言（`SpeakAuto`）或 `last_words_skip`（隐藏工具）处理。`death_lyric` 仅在第二 switch（`tools.go:428-440` 的 `emotion_switch` 白名单）出现。

### 2.12 `PhaseRestartVote` / `restart_vote` — 重开局投票阶段（§117）

| 工具名 | 参数 | 映射 |
|---|---|---|
| `restart_vote` | `{choice}` | `RestartVote` → `Action_RestartVote`（choice ∈ {yes, no, abstain}） |

> ⚠️ `restart_vote` **唯一工具**：没有 speak / interject / whisper / emotion_switch，避免 LLM 在投票阶段继续聊天气泡。

---

## 三、「隐藏」工具（LLM 不可见，仅 driver / watchdog / quarantine 路径派发）

以下名字在 `dispatchToolInner`（`tools.go:600-795`）注册，但 **不通过 `BuildTools` 暴露给 LLM**，仅由 `run.go` 的 auto-skip 路径或 `WerewolfManager` 的 quarantine 救援路径直接调用：

| 工具名 | 派发位置 | 实际映射 | 作用 |
|---|---|---|---|
| `vote_skip` | `SkipPhaseAction` → `runner.Vote(-1)` | `Action_DayVote(NoSeat)` | 弃权投票；每 bot 自救路径 |
| `witch_act_skip` | `SkipPhaseAction` → `runner.WitchAct("none", -1)` | `Action_Witch(none)` | 不用药；每 bot 自救路径 |
| `sheriff_stream_skip` | quarantine 路径 → `IdleSilent("player", "...")` | 留审计行 | 放弃警徽流声明（持锁路径） |
| `idiot_reveal_skip` | quarantine 路径 → `runner.IdiotReveal("skip")` | `Action_IdiotReveal(skip)` | 放弃翻牌，正常放逐 |
| `last_words_skip` | `SkipPhaseAction` → `runner.LastWordsSkip()` | `Action_SkipLastWords` | 放弃遗言（R91-P1-1） |

### 3.1 `SkipPhaseAction(phase, role)` 完整映射表（`run.go:168-218`）

auto-skip 路径（`consecutiveFailures >= failAutoSkipThreshold` 或 watchdog 兜底）按阶段派发：

| phase | 派发工具名 | 参数 |
|---|---|---|
| `night_wolves` | `wolf_kill` | target=-1（空刀） |
| `night_seer` | `seer_check` | target=-1 |
| `night_witch` | `witch_act_skip` | — |
| `speak` | `finish_speak` | — |
| `vote` | `vote_skip` | — |
| `sheriff` | `sheriff_elect` | — |
| `dawn` | `start_day` | — |
| `hunter_shoot` | `hunter_shoot` | target=-1（放弃） |
| `death_lyric` | `last_words_skip` | — |
| `idiot_reveal` | `idiot_reveal_skip` | — |
| `restart_vote` | （无） | — |

---

## 四、跨阶段通用工具（具体条件暴露）

以下工具在多个阶段出现，合并说明：

| 工具名 | 出现阶段 | 关键约束 |
|---|---|---|
| `emotion_switch` | PhasePreWolves / PhaseNightWolves / PhaseNightSeer / PhaseNightWitch / PhaseDawn / PhaseSpeak / PhaseVote / PhaseSheriff / PhaseHunterShoot / PhaseIdiotReveal / **PhaseDeathLyric** | 不在 gameover / filling / restart_vote 暴露；不消耗 speak/whisper 桶；不广播 chat_message |
| `whisper` | PhasePreWolves / PhaseSpeak | ≤1 次/60s，WhisperLimiter |
| `interject` | PhasePreWolves / PhaseSpeak | 走 30s speak 桶；与 speak 相同限流 |
| `sheriff_stream` | PhaseSpeak / PhaseSheriff | 双 slot（1/2）+ 撤回（-1） |
| `speak_with_thought` | PhasePreWolves / PhaseSpeak | 共享 speak 30s 桶；internal_thought 仅进 BotTranscript |

> ⚠️ 注意：`whisper` / `interject` / `speak_with_thought` **不在** `death_lyric` 暴露（详见 §2.11）。

---

## 五、限流与工具调用的交互

`run.go` 主循环在 `DispatchTool` 之后，按工具名做限流标记（`:1002-1028`）：

| 工具名 | 限流动作 | 限流参数 |
|---|---|---|
| `speak` | `a.Limiter.Mark()` | **30s** 令牌桶（R81 P0-1 修复: 45s→30s，`agent.go:577`） |
| `speak_with_thought` | `a.Limiter.Mark()` | 同上（共享桶，防绕过） |
| `interject` | `a.Limiter.Mark()` | 同上 |
| `whisper` | `a.MarkWhisper()` | **60s** 独立桶（WhisperLimiter，R81: 90s→60s，`agent.go:578`） |
| `idle_silent` | 无限流 | 视为「已主动沉默」 |
| 其它（vote/wolf_kill/...） | 无限流 | 由引擎内部限流 / 单轮单次约束 |

> 历史：`LLMCallLimiter`（单 bot 3s/8s 间隔）已于 §130 重构（2026-07-13）删除（`agent.go:62`、`run.go:560-561`），每个 bot 按模型自身速率调 LLM，不再被全局 LLM 令牌桶误伤。

---

## 六、工具派发链路总览（调用栈）

```
Agent.Run (run.go:305)
  └─ runLoop (run.go:338)
      └─ select { case evt := <-a.events: a.handleEvent(...) }
      └─ handleEvent (run.go:371)
          ├─ 前置守卫：
          │    ├─ a.IsQuarantined() → return
          │    ├─ evt.Context.Phase == "" → return
          │    ├─ !containsSeat(alive, seat) → return  (dead bot)
          │    └─ evt.Kind == "speak_floor_tick" → handleSpeakFloorTick (独立 fast-path)
          │
          ├─ 主决策循环 for {
          │    ├─ rp() 读 live phase / role / seat / alive / speakTurn / turnActing
          │    │
          │    ├─ BuildTools(phase, role, seat, alive, speakTurn, gc)   ← tools.go:123（动态构造）
          │    ├─ BuildSystemPrompt(role)                                ← prompt.go
          │    ├─ Memory.Snapshot + SanitizeMessagesForAnthropic(msgs)   ← memory.go
          │    │
          │    ├─ req := llm.LLMRequest{ Model, System, Messages, Tools, MaxTokens=1024, Metadata }
          │    │
          │    ├─ MarkLLMCallStart + SetLLMCallPhase(PhaseCalling)
          │    ├─ a.callProvider(ctx, req, streamProgress)              ← run.go:1400
          │    ├─ MarkLLMCallEnd (deferred)
          │    │
          │    ├─ resp.ToolUses() 遍历 {
          │    │      DispatchTool(tu.Name, tu.Input, runner)            ← tools.go:491
          │    │        └─ dispatchToolInner (tools.go:600) switch → runner 方法
          │    │            └─ dispatchToolRecordAction 异步写 action 日志
          │    │      PushTool(tu.Name, tu.Input, Result)               ← Memory（BotTranscript.ToolCalls）
          │    │      Mark limiter (speak/whisper/interject)
          │    │      if saidSomething → return (让出到下一事件)
          │    │    }
          │    │
          │    ├─ 无 tool_use → text-block 自动发言 (§130 SpeakAuto)
          │    │
          │    └─ MaxToolUse 用尽 → auto-skip (SkipPhaseAction)
          │    }
          └─ 结束
```

### 6.1 text-block 自动发言（§130 重构，2026-07-13）

LLM 在 assistant content 数组中既可能输出 `tool_use` 块（功能性操作），也可能输出纯 `text` 块（自然语言发言）。Claude Code Agent 的实际 wire 格式以 text 块为主、tool_use 为辅。

`run.go:1081-1115`：若 LLM 在同一轮 assistant content 里输出了非空 text 但没调任何 speak 类 tool，且 `phaseAllowsPublicSpeech(phase)` 为真（白名单：发言阶段 / 强制发言 / 投票前自由发言等），则自动把 text 作为公开发言广播：

```go
if phaseAllowsPublicSpeech(phase) {
    if autoText := resp.Text(); autoText != "" && !saidSomething {
        if autoAR, ok := runner.(interface{ SpeakAuto(text string) (string, error) }); ok {
            ar, aerr := autoAR.SpeakAuto(autoText)  // ← 完整过滤链
            ...
        }
    }
}
```

`SpeakAuto`（`agent_runner.go:326`）走与 `Speak` 完全相同的过滤链（rate-limit / ScrubIdentityLeak / FactCheckDeathClaims / StripLLMInternalTags / chatSvc.SendFromBot / chatQueue.Append），但 **不经 `DispatchTool`**，直接由 runner 接口调用。

> 意义：LLM 不再被强制浪费一次 tool_use round-trip 才能开口，符合 Claude Code 范式；同时保留 `speak` / `speak_with_thought` / `interject` tool 供 LLM 显式调用（如需要心口不一或插话语义）。

### 6.2 工具结果回写 Memory（tool_use ↔ tool_result 配对）

`agent.go:1351-1366`：
- `recordAssistant(resp)` 推送原始 `resp.Content`（含 tool_use 块）到 Memory。
- `recordToolResult(toolUseID, content, isErr)` 推送 `role=user` 消息，内含 `tool_result` ContentBlock：
```go
func (a *Agent) recordToolResult(toolUseID, content string, isErr bool) {
	a.Memory.Push(llm.Message{
		Role: "user",
		Content: []llm.ContentBlock{{
			Type:      "tool_result",
			ToolUseID: toolUseID,
			Content:   []llm.ContentBlock{{Type: "text", Text: content}},
			IsError:   isErr,
		}},
	})
}
```

`SanitizeMessagesForAnthropic(msgs)`（`memory.go:450-566`）在每次 LLM 调用前做请求级两遍扫描（不修改 Memory）：
1. 收集所有 assistant `tool_use` 的 id。
2. 丢弃 **orphan tool_result**（id 从未在 tool_use 中公告的），并为缺少后续 tool_result 的 tool_use **合成错误 tool_result**。
3. `mergeConsecutiveUserMessages` 合并相邻 `role=user` 轮次为一条（Anthropic 要求严格 user/assistant 交替）。

### 6.3 重试 / 冷却 / quarantine 阈值（`run.go:94-135`）

| 常量 | 值 | 说明 |
|---|---|---|
| `maxConsecutiveFailures` | **10**（R81: 5→10） | 连续失败多少次触发 quarantine |
| `permanentQuarantineThreshold` | **4**（R131: 2→4） | 永久错误（401/403）触发 quarantine 的阈值 |
| `failCooldownWindow` | **60s** | 两次失败间隔 < 60s 不重复计数（防瞬断密集重试） |
| `failAutoSkipThreshold` | **1** | 连续失败 ≥ 1 次即派发 auto-skip |
| `defaultLLMCallTimeoutSec` | **120** | 单次 LLM 调用超时 |

`thresholdForSeatCount`（`run.go:96-105`）在 13 人局动态放宽：`maxFail = 10 + min(seatCount-7, 6)`，`permThresh = 4 + extra/2`。

重试逻辑（`handleEvent` `:637-868`）：
1. 分类错误：`*anthropic.Error.Retryable==false` → 永久；429 → "429"；timeout → "timeout"；else "5xx"。
2. 可重试 → 指数退避 `1<<attempt` 秒（上限 8s），最多 `maxRetries = a.MaxLLMRetries()`（默认 5，R131）。
3. 耗尽重试后：网络/超时瞬断 → 仅 auto-skip，**不计入** consecutiveFailures；否则若 `now - lastFailureTime < 60s` → 不递增；否则 `consecutiveFailures++`。
4. 达到阈值 → `a.SetQuarantined()` 并 return。
5. 达到 `failAutoSkipThreshold` → 派发 `SkipPhaseAction(phase, role)`，8s 后 `scheduleReWake`。

`ResetConsecutiveFailures()`（`agent.go:847-865`）在任何成功 LLM 响应后清零计数器 + quarantine + lastError。

---

## 七、`ToolRunner` 接口完整方法列表（`tools.go:24-112`，共 25 个方法）

| 方法 | 对应工具名 | 备注 |
|---|---|---|
| `RecordLog() *RecordLogService` | — | 日志 hook getter |
| `GameLogID() string` | — | 日志 hook getter |
| `WolfKill(target int)` | `wolf_kill` | |
| `SeerCheck(target int)` | `seer_check` | |
| `WitchAct(action string, target int)` | `witch_act` | |
| `Speak(text string)` | `speak` | |
| `SpeakAuto(text string)` | —（text-block 自动发言） | §130 重构，不经 DispatchTool |
| `SpeakWithThought(publicText, internalThought string)` | `speak_with_thought` | §119 心口不一 |
| `FinishSpeak()` | `finish_speak` | |
| `Vote(target int)` | `vote` / `vote_skip` | target=-1 弃权 |
| `FinishVote(tiedRound int)` | `finish_vote` | |
| `StartDay()` | `start_day` | |
| `SheriffCandidate(target int)` | `sheriff_candidate` | |
| `SheriffElect()` | `sheriff_elect` | |
| `HunterShoot(target int)` | `hunter_shoot` | |
| `LastWordsSkip()` | `last_words_skip` | R91-P1-1 |
| `SheriffStream(slot int, target int)` | `sheriff_stream` | §7/§12 警徽流 |
| `IdiotReveal(choice string)` | `idiot_reveal` / `idiot_reveal_skip` | §3.5/§12 白痴翻牌 |
| `WolfSuicide()` | `wolf_suicide` | |
| `Whisper(toSeat int, text string)` | `whisper` | |
| `Interject(text string)` | `interject` | BUG-WEREWOLF-AGENT-INTERJECT |
| `RestartVote(choice string)` | `restart_vote` | §117 重开局投票 |
| `ProposeVote()` | `propose_vote` | 预言家发起投票 |
| `EmotionSwitch(emotion, reason string)` | `emotion_switch` | §124 |
| `EmotionSwitchRandom(reason string)` | `emotion_switch` (random) | §124 |

### 7.1 子接口

- **`IdleSilentRunner.IdleSilent(role, reason)`**（`agent.go:520-522`）：由 `idle_silent` / `sheriff_stream_skip` 派发。
- **`BotRunner.BotUserID() / CurrentPhase()`**（`tools.go:528-532`）：由 `dispatchToolRecordAction` 用于 action 日志（nil-safe）。

### 7.2 实现位置

`ToolRunner` 由 `game/werewolf/agent_runner.go:30` 的 `agentRunner` 结构体实现。每个 action 方法（`WolfKill` `:138`、`SeerCheck` `:147`、`WitchAct` `:156`、`Speak` `:165`、`SpeakAuto` `:326`、`SpeakWithThought` `:423`、`FinishSpeak` `:656`、`Vote` `:665`、`FinishVote` `:674`、`StartDay` `:683`、`SheriffCandidate` `:692`、`SheriffElect` `:708`、`HunterShoot` `:717`、`SheriffStream` `:735`、`IdiotReveal` `:774`、`WolfSuicide` `:787`、`Whisper` `:796`、`Interject` `:827`、`RestartVote` `:909`、`ProposeVote` `:901`、`LastWords` `:887`、`LastWordsSkip` `:892`、`IdleSilent` `:931`、`EmotionSwitch` `:961`、`EmotionSwitchRandom` `:978`）内部调用 `r.mgr.Action_*`，由 manager 的 `r.mu` 保护。

> **锁模型**：`agentRunner` 自身 **不持有** `r.mu`（`agent_runner.go:7-9` 注释：「The runner is safe to call from the agent goroutine because all state mutations go through the manager lock.」）。所有状态变更走 `mgr.Action_*`，由 manager 锁 + ChatService 锁保护。`CurrentPhase()` 通过 `lockRoomBriefly(mgrRoom, 100ms)` 短暂持锁（`agent_runner.go:109-122`）。

### 7.3 `*Locked` 变体（quarantine 路径，§92a 自死锁防护）

`dispatchQuarantinedSkipLocked` 在 `r.mu` 持锁路径调用，而 Go 的 `sync.Mutex` 不可重入，因此所有被 quarantine 路径调用的 `Action_*` 都有对应的 `*Locked` 变体（调用方已持 `r.mu`）：

- `room_quarantine_skip_locked.go`：`wolfKillLocked`、`seerCheckLocked`、`witchLocked`、`dayVoteLocked`、`finishVoteLocked`、`sheriffElectLocked`、`startDayLocked`、`sayLastWordsLocked`、`skipLastWordsLocked`
- `room.go`：`finishSpeakLocked`（`:3167`）、`hunterShootLocked`（`:3215`）、`idiotRevealLocked`（`:3235`）、`sheriffStreamDeclareLocked`（`:3257`）

> 教训（§92a）：「锁内路径调用的 Action_* 必须全部建 `*Locked` 锁内变体，漏一个即自死锁」。

---

## 八、法官工具集（与玩家工具集分离）

`ServerGo/agent/judge_tools.go:18-82` — `BuildJudgeTools()` 构造固定工具集（无 phase/role 参数，法官工具集不随阶段变化），由 `DispatchJudgeTool`（`:90`）派发。

| 工具名 | 必填字段 | 说明 |
|---|---|---|
| `announce` | `kind`, `text` | 广播到房间；设置 `LastAnnouncement` |
| `declare_cause` | `seat`, `verdict`, `text`（+ 可选 `cause`） | `verdict` ∈ {`execution`, `death`}；广播 |
| `prompt_actor` | `seat`, `text` | 提醒当前行动 bot |
| `summary` | `outcome`, `key_moments`, `timeline`, `mvp`, `wolf_decoy_log` | 5 段式整局总结；真实生成在 `judge_summary.go::GenerateSummary` |
| `idle_silent` | `reason` | 法官选择沉默；仅 transcript |

法官限流（`judge.go:126-127`）：`announceLimiter` 15s、`summaryLimiter` 60s。

法官事件 `Kind`（`judge.go:32-48`）：`judge_filling_welcome`、`judge_pre_wolves`、`judge_dawn_announce`、`judge_sheriff_start`、`judge_speak_start`、`judge_vote_start`、`judge_death_announce`、`judge_sheriff_stream_settle`、`judge_idiot_reveal`、`judge_hunter_shoot`、`judge_last_words`、`judge_restart_vote_result`、`judge_game_over`、`game_over_summary`。

> 法官工具 **永不** 修改游戏状态（驱动分层：watchdog → host driver → judge）。

---

## 九、阶段枚举速查表（phase 字符串 → 工具集）

| phase 字符串 | 暴露工具（不含 emotion_switch） |
|---|---|
| `PhasePreWolves` / `pre_wolves` | speak, speak_with_thought, interject, whisper, idle_silent |
| `PhaseNightWolves` / `night_wolves` | wolf_kill (werewolf only) |
| `PhaseNightSeer` / `night_seer` | seer_check (seer only) |
| `PhaseNightWitch` / `night_witch` | witch_act (witch only) |
| `PhaseDawn` / `dawn` | start_day |
| `PhaseSpeak` / `speak` | speak, finish_speak, speak_with_thought (speakTurn==seat); wolf_suicide (werewolf 且 speakTurn==seat); sheriff_stream (sheriff+seer); propose_vote (seer 且 !VoteProposed); interject; whisper |
| `PhaseVote` / `vote` | vote, finish_vote |
| `PhaseSheriff` / `sheriff` | sheriff_candidate, sheriff_elect, sheriff_stream (sheriff+seer) |
| `PhaseHunterShoot` / `hunter_shoot` | hunter_shoot (hunter only) |
| `PhaseIdiotReveal` / `idiot_reveal` | idiot_reveal |
| `PhaseDeathLyric` / `death_lyric` | **（无工具，遗言 prompt 驱动）** |
| `PhaseRestartVote` / `restart_vote` | restart_vote |

> 以上所有阶段（除 `PhaseRestartVote`）均额外暴露 `emotion_switch`。`PhaseFilling` / `PhaseGameOver` 不暴露任何工具。

---

## 十、`dispatchToolInner` switch 完整 case 列表（`tools.go:600-795`，共 26 个 case）

按字母序便于检索：

```
emotion_switch
finish_speak
finish_vote
hunter_shoot
idiot_reveal
idiot_reveal_skip
idle_silent
interject
last_words_skip
propose_vote
restart_vote
seer_check
sheriff_candidate
sheriff_elect
sheriff_stream
sheriff_stream_skip
speak
speak_with_thought
start_day
vote
vote_skip
whisper
witch_act
witch_act_skip
wolf_kill
wolf_suicide
```

共 **26** 个 case（含 5 个 skip 隐藏工具）。

---

## 十一、关键代码文件索引

| 文件 | 职责 |
|---|---|
| `ServerGo/agent/tools.go` | `ToolRunner` 接口（25 个方法）+ `BuildTools`（动态工具构造）+ `DispatchTool` / `dispatchToolInner`（派发 switch）+ `BotRunner` / `IdleSilentRunner` 子接口 |
| `ServerGo/agent/run.go` | `Run` / `runLoop` / `handleEvent` / `handleSpeakFloorTick` / `SkipPhaseAction` / `ShouldAutoSkip` / 重试+quarantine 阈值 |
| `ServerGo/agent/agent.go` | `Agent` 结构体 + `IdleSilentRunner` 接口 + `BotRunner` 接口 |
| `ServerGo/agent/prompt.go` | `BuildSystemPrompt` / `BuildUserPrompt` |
| `ServerGo/agent/judge_tools.go` | 法官工具集（announce / prompt_actor / summary / declare_cause / idle_silent）— 与玩家工具集分离 |
| `ServerGo/agent/judge.go` | 法官 Agent 主循环 |
| `ServerGo/agent/ratelimit.go` | `SpeakLimiter`（发言令牌桶） |
| `ServerGo/agent/memory.go` | `Memory`（多轮历史 + Prune / Compress / SanitizeMessagesForAnthropic） |
| `ServerGo/agent/emotion.go` | 10 类情绪状态机 |
| `ServerGo/agent/speak_dedup.go` | `dedupSpeakText`（相邻复读清理）+ `normalizeDeathTerms` |
| `ServerGo/agent/speak_recent_dedup.go` | 跨消息级发言去重 |
| `ServerGo/agent/speak_factcheck.go` | 死亡声明事实核查 |
| `ServerGo/agent/decision_summary.go` | 决策摘要 |
| `ServerGo/agent/record_log.go` | 异步 action 日志 worker |
| `ServerGo/agent/parallel_think.go` | §122 并行推理（默认关闭，§128 后整套机制保留但默认关闭） |
| `ServerGo/agent/judge_summary.go` | 法官「整局总结」解析 |
| `ServerGo/agent/judge_prompt.go` | 法官系统提示词 |
| `ServerGo/llm/types/types.go` | `ToolDef`（Anthropic `tools[]` wire 类型）+ `LLMRequest` |
| `ServerGo/game/werewolf/agent_runner.go` | `ToolRunner` 实现（`agentRunner`，25 个方法） |
| `ServerGo/game/werewolf/engine.go` | `Phase` 枚举 + 引擎级状态变更（`NightWolfKill` / `NightSeerCheck` / ...） |
| `ServerGo/game/werewolf/cards.go` | `Role` 枚举 + `StandardDeck13`（4 狼 + 预/女/猎/白痴 + 5 民） |
| `ServerGo/game/werewolf/room.go` | `Action_*` 公共方法（自持 `r.mu`）+ `*Locked` 变体 |
| `ServerGo/game/werewolf/room_quarantine_skip_locked.go` | quarantine 持锁路径的 `*Locked` 变体 |
| `ServerGo/game/werewolf/speak_filter.go` | 发言过滤链（rate-limit / identity-leak / death-fact / XML strip） |
| `ServerGo/game/werewolf/speak_mystery.go` | R132 公屏猜疑化（`MysteryMaskText`） |
| `ServerGo/game/werewolf/speak_floor.go` | 发言下限 watchdog（≥2 条/分钟） |

---

## 十二、重要设计教训（来自 CLAUDE.md §13 系列）

| # | 教训 | 关联工具 |
|---|---|---|
| 1 | 工具集必须 phase-visibility 过滤：给 LLM 看到不该用的工具会浪费 tool_use round 并泄露「轮到谁」信息 | 全部 |
| 2 | `required` 字段必须是非 null 数组（BUG-WEREWOLF-P0-8）：DeepSeek/DouBao 等严格代理会因 `required: null` 返回 400 | 全部 |
| 3 | `speak` 与 `speak_with_thought` 共用 30s 令牌桶（`run.go:1002-1010`）：防止 LLM 用两条工具绕过限流刷屏 | speak / speak_with_thought |
| 4 | per-bot 自救 vs manager/watchdog 救援走不同路径（§92a/§97）：`SkipPhaseAction` 是 bot 自身路径，`dispatchQuarantinedSkipLocked` 是 manager 持锁路径 | vote_skip / witch_act_skip / ... |
| 5 | quarantined agent 不调 LLM（`run.go:371` 守卫）：避免永久坏模型空转浪费配额 | — |
| 6 | dead agent 不调 LLM（`run.go` 守卫）：避免「系统说我死了但又让我发言」幻觉 | — |
| 7 | 警徽流 `sheriff_stream` 的 enum 要包含 -1 撤回哨兵 + 当前已声明目标（`tools.go:312`） | sheriff_stream |
| 8 | `sheriff_candidate` 用 `onlySelf(seat)`（`tools.go:383`）：防止 LLM 代他人参选 | sheriff_candidate |
| 9 | `wolf_kill` 主动追加 -1 空刀哨兵（`tools.go:216`）：4 狼协商空刀是合理战术 | wolf_kill |
| 10 | `idle_silent` 强约束：`PreWolvesCountForMySeat ≥ 1` 才能调（`run.go` 检查） | idle_silent |
| 11 | `speak_with_thought` 的 `internal_thought` 必须协议层物理隔离（§119）：不进 chat_message 表 / chat_history 队列 | speak_with_thought |
| 12 | `emotion_switch` 不在 gameover / filling / restart_vote 暴露（`tools.go:428-440` 白名单） | emotion_switch |
| 13 | `death_lyric` 阶段 **无工具分支**，遗言 prompt 驱动；勿在工具表中为该阶段列 speak/whisper | — |
| 14 | `LLMCallLimiter` 已删除（§130）：每个 bot 按模型自身速率调 LLM，不再被全局令牌桶误伤 | — |
| 15 | text-block 自动发言（§130）：LLM 输出纯 text 未调 speak 类 tool 时，`SpeakAuto` 走完整过滤链广播 | — |

---

## 十三、与上游文档的对应关系

| 上游文档 | 关联章节 |
|---|---|
| `docs/狼人杀-Agent与系统/狼人杀Agent设计.md` §5 工具定义 | 工具表原始版本（7 人局） |
| `docs/狼人杀-Agent与系统/狼人杀Agent设计.md` §9 主循环 | `Run` / `handleEvent` 伪代码 |
| `docs/狼人杀-Agent与系统/狼人杀Agent设计.md` §10 工具派发 | `DispatchTool` 伪代码 |
| `docs/狼人杀13人标准局规则.md` | 13 人局规则（4 狼 + 预/女/猎/白痴 + 5 民 + 警长） |
| `docs/狼人杀-重构方案/主持人Agent重构设计.md` | 法官工具集（与玩家工具集分离） |
| `docs/狼人杀-角色设计/狼人杀死亡语义设计.md` | `verdict`（execution / death）— 影响 `wolf_suicide` / `hunter_shoot` 描述 |
| `docs/狼人杀-Agent与系统/狼人杀重开局投票设计.md` | `restart_vote` 工具 quorum 评估 |
| `docs/狼人杀-Agent与系统/狼人杀对话即思考设计.md` | §128 重构：idle_think → idle_silent 合并；thinking 块删除 |
| `docs/狼人杀观众唤醒设计.md` | spectator_speech wake → interject 路径 |
| `docs/狼人杀遗言设计.md` | `last_words_skip` 工具 |
| `docs/狼人杀-Agent与系统/狼人杀Agent公屏猜疑化设计.md` | R132 `MysteryMaskText` 过滤链 |
| `CLAUDE.md` §119 | speak_with_thought 心口不一机制 |
| `CLAUDE.md` §124 | emotion_switch 10 类情绪 |
| `CLAUDE.md` §130 | text-block 自动发言（SpeakAuto）+ LLMCallLimiter 删除 |

---

## 十四、附：Phase / Role 枚举权威值（`engine.go` / `cards.go`）

### 14.1 Phase（`ServerGo/game/werewolf/engine.go:15-76`，iota 0-13）

| 常量 | String() | 含义 |
|---|---|---|
| `PhaseFilling` | `filling` | 等待入座 |
| `PhasePreWolves` | `pre_wolves` | 首夜发言缓冲 |
| `PhaseNightWolves` | `night_wolves` | 狼人夜间刀人 |
| `PhaseNightSeer` | `night_seer` | 预言家查验 |
| `PhaseNightWitch` | `night_witch` | 女巫用药 |
| `PhaseDawn` | `dawn` | 黎明公布死亡 + 警徽流结算 |
| `PhaseSheriff` | `sheriff` | 警长竞选（Day1） |
| `PhaseSpeak` | `speak` | 白天轮流发言 |
| `PhaseVote` | `vote` | 白天投票放逐 |
| `PhaseIdiotReveal` | `idiot_reveal` | 白痴翻牌 |
| `PhaseHunterShoot` | `hunter_shoot` | 猎人开枪 |
| `PhaseDeathLyric` | `death_lyric` | 遗言 |
| `PhaseRestartVote` | `restart_vote` | 重开局投票 |
| `PhaseGameOver` | `over` | 对局结束 |

`IsNight()` = PreWolves / NightWolves / NightSeer / NightWitch。

### 14.2 Role（`ServerGo/game/werewolf/cards.go:51-110`，iota 0-16）

| 常量 | String() | 阵营 |
|---|---|---|
| `RoleUnknown` | `unknown` | — |
| `RoleWerewolf` | `werewolf` | 狼 |
| `RoleSeer` | `seer` | 好 |
| `RoleWitch` | `witch` | 好 |
| `RoleHunter` | `hunter` | 好 |
| `RoleIdiot` | `idiot` | 好 |
| `RoleVillager` | `villager` | 好 |
| `RoleGuard` | `guard` | 好（扩展神职，新局随机牌组候选） |
| `RoleKnight` | `knight` | 好（扩展神职，新局随机牌组候选） |
| `RoleMagician` | `magician` | 好（扩展神职，新局随机牌组候选） |
| `RoleMerchant` | `merchant` | 好（扩展神职，新局随机牌组候选） |
| `RoleDreamer` | `dreamer` | 好（扩展神职，新局随机牌组候选） |
| `RoleCrow` | `crow` | 好（扩展神职，新局随机牌组候选） |
| `RoleScarecrow` | `scarecrow` | 好（仅历史兼容；不在新局活动池） |
| `RolePrince` | `prince` | 好（仅历史兼容；不在新局活动池） |
| `RoleDemonHunter` | `demon_hunter` | 好（扩展神职，新局随机牌组候选） |
| `RolePureWhite` | `pure_white` | 好（扩展神职，新局随机牌组候选） |

`MaxPlayers = 13`（`cards.go:150`）。`StandardDeck13()` = 4×Werewolf + Seer + Witch + Hunter + Idiot + 5×Villager。新局随机牌组的 `godRolePool` 为 Witch / Hunter / Idiot / Guard / Knight / Magician / Merchant / Dreamer / Crow / DemonHunter / PureWhite；其中后 8 名是扩展神职候选。`RoleScarecrow` / `RolePrince` 仅保留枚举、wire 字符串、阵营/神职判定和显示映射以兼容历史数据，已从活动池移除，`RandomDeck13()` / `AssignRoles13Random()` 不会再发出。

---

> 文档生成时间：2026-07-17。
> 覆盖代码版本：`ServerGo/agent/tools.go` + `ServerGo/agent/run.go` + `ServerGo/agent/agent.go` + `ServerGo/llm/types/types.go` + `ServerGo/game/werewolf/agent_runner.go` + `ServerGo/game/werewolf/engine.go` + `ServerGo/game/werewolf/cards.go` 当前 HEAD。
