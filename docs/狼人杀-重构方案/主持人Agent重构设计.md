# 主持人 Agent(法官)重构设计 — §13 v2026-07-16

> 生效日期:2026-07-16
> 适用版本:LsmAgentGame ServerGo + ClientWeb(狼人杀 13 人标准竞技局 / 12 人 / 7 人)
> 关联文档:
>   - [`docs/狼人杀-重构方案/主持人Agent重构设计.md`](狼人杀-重构方案/主持人Agent重构设计.md) — 初版设计(愿望清单,多处未实现)
>   - [`docs/狼人杀13人标准局规则.md`](狼人杀13人标准局规则.md)
>   - [`docs/狼人杀-Agent与系统/狼人杀Agent设计.md`](狼人杀Agent设计.md)
>   - [`docs/狼人杀-角色设计/狼人杀死亡语义设计.md`](狼人杀死亡语义设计.md)
>   - [`docs/狼人杀-重构方案/主持人Agent重构技术实现.md`](主持人Agent重构技术实现.md) — 配套技术实现细节

## 0. 背景与审计结论

2026-07-16 对主持人 Agent 全面审计发现**三个阻塞性缺陷**:

| # | 缺陷 | 位置 | 现象 |
|---|------|------|------|
| 🔴1 | `j.Provider`/`j.apiKey` 从未注入 | `judge_summary_bridge.go:startJudgeGoroutine` 与 `agent/judge.go:NewAgentJudge` | 法官 LLM 路径入口守卫 `if j.Provider==nil\|\|j.apiKey==""` 永远成立 → 永远走 `JudgeFallbackText` 硬编码兜底。**逐阶段旁白从未真正调过 LLM**,只有整局总结路径(绕过 j.Provider 直连 registry)正常工作 |
| 🟡2 | 法官启动不依赖 Agent 数量 | `startJudgeGoroutine` 仅检查全局 `cfgWerewolfJudgeMode()=="ai"` | 全人类房(0 个 Agent)也会启动法官 goroutine + 渲染面板,违反"有 Agent 才有法官"的产品语义 |
| 🟡3 | 各阶段 `JudgePending*` 事件未真实发出 | `wakeJudgeLocked` 已定义,调用点仅 `wakeJudgeLockedForSummaryLocked`(`room.go:2385`)一处 | 设计中的「黎明/警长/发言/投票/翻牌/猎人/遗言」全程旁白全部缺失,法官只在游戏结束时总结一次 |

此外产品层面:创建界面无法官开关、前端 `JudgePanel` 未展示法官"一举一动"(缺 `ToolCalls` 活动流)、`JudgeMode` 缺少设计文档承诺的 `human` 模式。

**本次重构目标**:修复三个阻塞缺陷 + 落地「有 Agent 就有主持人」+ 前端展示法官一举一动 + 让法官在各阶段发挥公平/引导/戏剧化作用,成为游戏的"灵魂角色"。

---

## 1. 产品定位

### 1.1 主持人 = 游戏的「灵魂角色」,非玩家身份牌

主持人(法官)是**身份牌之外的系统角色**,由 LLM 驱动(或真人担任),职责是让一局狼人杀**公平、有趣、流畅、有戏剧张力**。13 人局仍发 4 狼+4 神+5 民,不发"法官"身份牌,法官永远在场到结束。

### 1.2 存在性规则(产品硬约束)

> **只要房间里 ≥1 个 Agent,主持人 Agent 就必须存在;创建界面在 Agent>0 时显式展示主持人信息。**

| 场景 | 主持人存在? | 说明 |
|------|------------|------|
| 13 个 Agent(全 AI 房) | ✅ | 默认 AI 法官 |
| 1 个 Agent + 12 真人 | ✅ | **新增**:混合房也启用 |
| 7 Agent + 6 真人 | ✅ | **新增** |
| 0 Agent(全人类房) | ❌ | 不启动法官 |
| 全局 `JudgeMode="off"` | ❌ | 运维关闭 |

技术映射:`JudgeEnabled = true` **iff** `cfgWerewolfJudgeMode() != "off"` **且** `房间 Agent 数 ≥ 1`。

### 1.3 设计哲学(公平·有趣·可玩·复杂)

| 维度 | 法官行为 | 实现要点 |
|------|---------|---------|
| **公平性** | 不泄露任何身份(狼座仅法官内部可见)、不偏袒任何阵营、死因宣告严格区分「处决/死亡」、不用权威字段外的事实 | prompt 注入 `WolfSeats` 但显式标注"不可对外宣布";仅用 AliveSeats/DeadSeats/SheriffSeat/Votes/Winner 权威字段;`declare_cause.verdict∈{execution,death}` |
| **有趣性** | 口语化、有情绪起伏、黎明/翻牌/猎人等节点制造戏剧感、平安夜/多死伤语气不同、禁用 Markdown/JSON 纯中文 | system prompt 给「语气模板」+ 阶段专属「应宣告」指令 |
| **可玩性** | 阶段切换清晰播报、`prompt_actor` 催人发言防冷场、deadline 倒计时提醒、平票/翻牌等节点明确宣读规则要点 | 全阶段 hook + 60s/座位 prompt_actor 限流 |
| **复杂性** | 多阶段多事件类型、5 工具分工、记忆复盘(`modelMemories`)、5 段整局总结 | 事件种类 12 种 + 5 工具 + §125 总结 |

---

## 2. 主持人三种运行模式

| 模式 | 来源 | 配置 | 默认 |
|------|------|------|------|
| **AI 法官** | 服务端 LLM 驱动 | 房间设置 `judge.mode="ai"` + `judge.model_key` | ✅(有 Agent 时) |
| **真人法官** | 真人用户以"法官"身份入房(不入座) | `judge.mode="human"` | 可选 |
| **关闭** | 无法官 | 全局 `JudgeMode="off"` 或 0 Agent | 全人类房默认 |

> 房间级 `judge.mode` 覆盖全局默认,让创建者可自选。后端 `startJudgeGoroutine` 在 `judge.mode!="ai"` 时跳过 AI 路径(真人/关闭走各自分支)。

---

## 3. 主持人各阶段作用设计(核心)

> 规则:每个 **phase 切换点** + 每个 **公开事件点**(黎明/警徽流/翻牌/猎人/胜负)都唤醒一次;**夜间秘密阶段法官静默观察不发言**;阶段**不等法官**(watchdog deadline 不变)。

| Phase | 法官动作 | 工具 | 触发事件 kind |
|-------|---------|------|--------------|
| `PhaseFilling` | 欢迎 + 规则简介 + 等待玩家 | `announce` | `judge_filling_welcome` |
| `PhasePreWolves` | 🌙「天黑请闭眼」 | `announce` | `judge_pre_wolves` |
| `PhaseNightWolves` | 🤫 静默观察(不播报) | `idle_silent` | (不唤醒) |
| `PhaseNightSeer` | 🤫 静默 | `idle_silent` | (不唤醒) |
| `PhaseNightWitch` | 🤫 静默 | `idle_silent` | (不唤醒) |
| `PhaseDawn` | 🌅 公布死亡(含 verdict 分色)+ 警徽流结算 | `announce` + `declare_cause` | `judge_dawn_announce` |
| `PhaseSheriff` | 🎙 警长竞选开场 | `announce` + `prompt_actor` | `judge_sheriff_start` |
| `PhaseSpeak` | 🗣 发言开始 + 每人发言前 `prompt_actor` + 发言结束一句话总结 | `announce` + `prompt_actor` + `summary` | `judge_speak_start` |
| `PhaseVote` | 🗳 投票开始 + 公布结果 + 死因/verdict 宣告 | `announce` + `declare_cause` | `judge_vote_start` / `judge_vote_result` |
| `PhaseIdiotReveal` | 🃏 白痴翻牌宣读 | `announce` | `judge_idiot_reveal` |
| `PhaseHunterShoot` | 🔫 猎人开枪节点 | `announce` + `prompt_actor` | `judge_hunter_shoot` |
| `PhaseDeathLyric` | 📜 遗言阶段开场 | `announce` | `judge_last_words` |
| `PhaseRestartVote` | 🔄 重局投票结果 | `announce` | `judge_restart_vote_result` |
| `PhaseGameOver` | 🏆 宣布胜负 + 触发整局总结 | `announce` + `summary`(LLM) | `judge_game_over` / `judge_game_over_summary` |

限流(防刷屏):`announce` 15s/条、`prompt_actor` 60s/座位、`summary` 60s/条、`declare_cause` 10s/条。

---

## 4. 工具集(`BuildJudgeTools`)

| 工具 | 参数 | 行为 | 变更 |
|------|------|------|------|
| `announce` | `{kind, text}` | 公开宣告,记入 transcript + 广播至公屏 | 新增 `kind` 枚举约束 |
| `prompt_actor` | `{seat, hint?}` | 「请 X 号发言」 | 不变 |
| `summary` | `{text}` | 一句话总结前 N 条发言 | 不变 |
| `declare_cause` | `{seat, cause, verdict∈{execution,death}, text}` | 死因宣告 | 不变 |
| `idle_silent` | `{reason}` | 静默(夜间/无需播报时) | 不变 |

> 法官工具**只产生 transcript + 广播**,不调任何玩家工具(§2.2 边界继承)。`text` maxLength=80。

---

## 5. 前端 UI 设计

### 5.1 创建界面(`RoomCreateModal`)

当 `agentCount > 0` 时,在 AI 座位区下方展开**主持人配置卡**:

```
┌──────────────────────────────────────────────────────┐
│ ⚖️ 主持人 Agent (法官)                                │
│ 模式: (•) AI 法官 ( ) 真人法官 ( ) 关闭               │
│ 模型: [美团 LongCat-2.0 ▼]  (仅 AI 法官时显示)        │
│ 提示:主持人负责阶段播报/死因宣告/流程引导,不影响玩家   │
└──────────────────────────────────────────────────────┘
```

- `agentCount==0` 时不渲染此卡(全人类房无法官);
- 模式默认「AI 法官」;选「关闭」则该房间无法官;
- 模型下拉复用 `/api/llm/models`,留空=跟随首个 Agent 模型;
- 提交时随 `agent_seats` 一并发送 `{ judge: { mode, model_key } }`。

### 5.2 法官一举一动活动流(`JudgePanel` 重构)

`JudgePanel` 顶部保留当前宣告,下方新增**活动时间线**(ToolCalls 活动流):

```
┌──────────────────────────────────────────────────────┐
│ ⚖️ 法官 · 美团 LongCat-2.0          [待宣告] [历史▼] │
│ 「黎明已至,3 号、7 号死亡。现在进入第 2 天·白天。」    │
├──────────────────────────────────────────────────────┤
│ 📋 一举一动                              [展开▼]      │
│ 12:05:03 ⚙️ announce(kind=judge_dawn_announce)       │
│          → 「黎明已至,3 号、7 号死亡...」             │
│ 12:05:03 ⚙️ declare_cause(seat=3, verdict=death)    │
│          → 「3 号被狼刀死亡,身份是预言家」            │
│ 12:04:50 🤫 idle_silent(夜间观察)                    │
│ 12:04:45 🟢 LLM 调用 1.2s                            │
└──────────────────────────────────────────────────────┘
```

- 每条工具调用 = 一行:时间戳 + 工具 emoji + 工具名(参数摘要) + 结果/产物;
- 支持展开 transcript 详情(调用了哪个工具、输入参数、输出文本、LLM 耗时);
- 在线状态:🟢 已就绪 / 🟡 思考中(调用 LLM 时) / ⚠️ quarantine 兜底。

### 5.3 阶段宣告横幅(`JudgeActionBar` 保留 + 增强)

顶部 sticky 横幅保留,新增** pulse 动画**:新宣告到达时边框高亮 2s 后消退,吸引玩家注意。`待宣告`徽章在 judge_pending_announce 非空时显示。

### 5.4 整局总结(保留 §125)游戏结束时展开 5 段总结(阵营胜负/关键翻点数/角色操作时间线/MVP/狼人悍跳记录)。

### 5.5 死因 verdict 分色(对齐死亡语义设计)

- **死亡**(夜间):灰色「💀 N 号 · 死亡 · 狼刀」
- **处决**(白天投票):橙色「⚖️ N 号 · 处决」
- **反杀**(猎人):红色「🔫 N 号 · 反杀」
- **毒杀**:紫色「☠️ N 号 · 毒杀」

---

## 6. 技术设计(摘要)

> 详细实现(文件/字段/锁不变式/测试)见配套文档 [`docs/狼人杀-重构方案/主持人Agent重构技术实现.md`](主持人Agent重构技术实现.md)。此处仅摘要重构要点。

### 6.1 修复缺陷 🔴1 — Provider 注入

`startJudgeGoroutine` 在 `agent.NewAgentJudge` 之后、`j.Run` 之前,用与玩家 Agent 相同的 `registry.Get(modelKey)` 解析 provider + key 并赋值给 `j.Provider` / `j.apiKey`:

```go
provider, key, err := m.registry.Get(modelKey)
if err != nil {
    logger.L().Warn("judge: registry.Get failed, judge will use fallback", zap.Error(err))
} else {
    j.Provider = provider
    j.apiKey = key
}
```

> 与 `agent.NewWithRoom`(:558) 完全一致的解析路径,拒绝占位 key。

### 6.2 修复缺陷 🟡2 — 依赖 Agent 数量启动

`startJudgeGoroutine` 头部加 Agent 存在性守卫(在全局 JudgeMode 检查之后):

```go
if len(r.seatModelKeys) == 0 {
    logger.L().Info("werewolf: no agents, skipping judge goroutine", zap.String("room_id", r.RoomID))
    return
}
```

两处调用点(`room.go:1231`、`:4271`)自动继承。同时支持房间级 `r.JudgeDesired`(创建者显式选「关闭」时 `JudgeDesired=false`,即使有 Agent 也不启动)。

### 6.3 修复缺陷 🟡3 — 各阶段事件接通

在 `phaseWatchdogTick` 或 `setPhaseAndDeadline` 阶段切换集中点,对比上一 tick 的 phase;phase 变化时调 `m.wakeJudgeLocked(judgeKindForPhase(newPhase), nil)`。映射表:

| phase → | kind |
|---------|------|
| PhaseFilling | `judge_filling_welcome` |
| PhasePreWolves | `judge_pre_wolves` |
| PhaseDawn | `judge_dawn_announce` |
| PhaseSheriff | `judge_sheriff_start` |
| PhaseSpeak | `judge_speak_start` |
| PhaseVote | `judge_vote_start` |
| PhaseIdiotReveal | `judge_idiot_reveal` |
| PhaseHunterShoot | `judge_hunter_shoot` |
| PhaseDeathLyric | `judge_last_words` |
| PhaseRestartVote | `judge_restart_vote_result` |
| PhaseGameOver | `judge_game_over`(+ 总结) |

秘密阶段(NightWolves/Seer/Witch)不调 wake → 法官 `idle_silent`。

### 6.4 房间级法官设置透传

- `api/room_api.go:createRoomRequest` 加 `Judge json:"judge,omitempty"`(`mode`, `model_key`);
- `service/room_service.go:AgentSeatConfig` 透传到 `WerewolfRoom.JudgeDesired`(bool) + `JudgeModelKey`(string);
- `JudgeEnabled` 仍由 `r.judge != nil` 决定(`view.go:743` 不变)。

### 6.5 广播路径(对齐设计愿望)

法官发言进入公屏:`chat.SendFromJudge`(新增)写 `chat_message(is_judge=true)` + 经 chatQueue 广播;前端 GameChatPanel 以特殊样式(⚖️ 前缀 + 金底)渲染。**不再仅写在 transcript 里**,让玩家实时看到法官播报(修复"播报未送达公屏"的隐藏缺陷)。

### 6.6 锁不变式(§92a 继承)

- `wakeJudgeLocked` 在 `r.mu` 内建快照、channel 发送非阻塞;`j.Run` 主循环不持 `r.mu`;
- 法官读 `GameSnapshot`(快照)而非 live state,不触发 §92a 锁内变体;
- Provider 注入在 goroutine 启动前完成,goroutine 内只读 `j.Provider`。

---

## 7. 数据契约增量

### 7.1 后端 `JudgeTranscript` 扩展(`agent/judge.go`)

```go
type JudgeTranscript struct {
    // ... 现有字段 ...
    Activities []JudgeActivity `json:"activities"` // 新增:一举一动活动流
    LastLLMMs  int64           `json:"last_llm_ms"` // 新增:最近一次 LLM 耗时
}

type JudgeActivity struct {
    At    int64  `json:"at"`    // 毫秒时间戳
    Tool  string `json:"tool"`  // 工具名(announce/prompt_actor/...)
    Input string `json:"input"` // 参数摘要(≤120 字符)
    Out   string `json:"out"`   // 产物文本
    LLMMs int64  `json:"llm_ms,omitempty"` // 本次 LLM 耗时(可选)
}
```

### 7.2 前端 `JudgeContextJSON` 扩展(`types/werewolf.ts`)

```ts
activities?: JudgeActivityJSON[];  // 一举一动
last_llm_ms?: number;
```

### 7.3 请求契约

`POST /api/games/werewolf/rooms`:

```json
{
  "name": "可选房间名",
  "agent_seats": [{ "seat": 0, "model_key": "MeiTuan-model" }],
  "judge": { "mode": "ai", "model_key": "MeiTuan-model" }
}
```

### 7.4 法官模型选择契约

法官模型优先级按以下规则确定,确保创建者的显式选择始终被**原样尊重**,同时"留空 = 公平随机"的默认行为可追溯:

| 场景 | 模型选择 | 说明 |
|------|---------|------|
| **创建者显式指定** (`judge.model_key` 非空) | ✅ **原样尊重** | **原样传递,不自动改写**。`registry.Get(modelKey)` 失败 → 走现有规则 fallback(`JudgeFallbackText` 兜底 / 配置失败处理路径),服务端不做隐式改写 |
| **AI 法官留空** | 🎲 **随机** | 从**完整可用 provider 池** Fisher-Yates 随机选一个。**可用池定义**:① **全部 model 字段非空**;② API key 非空 **且 非占位**(`api_key != "API-KEY-PLACEHOLDER"`);③ **不排除 Agent 座位已用模型**(同一房间可重复,与其他 agent 独立决策);④ 若池为空则降级为 JudgeFallbackText 兜底 |
| **0 Agent** | ❌ **不启动** | 即使 `judge.mode="ai"` 且 `judge.model_key` 非空也跳过 |
| **全局 `JudgeMode="off"`** | ❌ **不选择** | 运维关闭,不选模型 |

**契约保证**:
- 创建者显式选择 → 服务端**原样尊重**,**不自动改写**为其他模型;若 `registry` 解析失败,走现有 fallback 规则(`JudgeFallbackText` 兜底),不做隐式随机改写;
- 留空 → 服务端在完整可用池(全部 model 非空、API key 非空非占位)内 Fisher-Yates 随机一个,不排除 Agent 已用模型,**不**用占位模型兜底;
- 0 Agent 或 off → 不选,不启动。

**实现位置**: `service/room_service.go` 中的 `RoomService.CreateRoomWithAgents` 在调用 `SetJudgeConfig` 之前完成模型选择(而不是 `startJudgeGoroutine` 阶段),确保房间级 `r.JudgeModelKey` 在 `SetJudgeConfig` 时已确定再注入 goroutine。

---

## 8. 测试与验证

### 8.1 后端单测(`agent/judge_test.go` 扩展 + `game/werewolf/room_judge_test.go`)

- `TestJudge_ProviderInjection`:启动后 `j.Provider!=nil && j.apiKey!=""`,调 `announce` 进 transcript 并广播;
- `TestJudge_NoAgentsNoJudge`:0 Agent 房 `r.judge==nil`,`JudgeEnabled==false`;
- `TestJudge_PerPhaseWake`:mock phase 切换,验证 11 种 kind 各触发一次 `wakeJudgeLocked`;
- `TestJudge_SilentAtNight`:NightWolves/Seer/Witch 不调 wake;
- `TestJudge_BroadcastReachesChat`:法官 `announce` 后 chat_message 表有 `is_judge=true` 记录;
- `TestJudge_QuarantineFallback`:连续 LLM 失败 → quarantine → 兜底文本。

### 8.2 前端类型检查 + 构建

`tsc --noEmit` 通过 + `npm run build` 成功。

### 8.3 端到端(手动 + go-web-debug-tool)

- 创建 1 Agent 房 + 13 Agent 房:进游戏后法官面板可见,各阶段有播报,公屏可见 ⚖️ 样式法官消息;
- 创建 0 Agent 房:法官面板不渲染;
- 游戏结束:整局总结 5 段。

---

## 9. 边界(不做的事)

- ❌ 不引入"法官"身份牌;
- ❌ 法官不调任何玩家工具(投票/夜间行动/技能);
- ❌ 法官发言不改 phase 状态;
- ❌ 不动 LastWordsRounds=2 / 警徽流 / 白痴翻牌 / 重局投票底层逻辑(仅在这些节点加宣告);
- ❌ 法官不加私人记忆(无 Memory,仅 JudgeTranscript 摘要);
- ❌ 法官不与玩家私聊;
- ❌ 不改变现有 `hostDriverWake` / `phaseWatchdogTick` 兜底路径(法官是叠加层)。

---

## 10. 实施计划(任务化)

| 阶段 | 任务 | 产出 |
|------|------|------|
| T1 | 本文档 + 技术实现文档 | `docs/狼人杀-重构方案/主持人Agent重构设计.md` + `docs/狼人杀-重构方案/主持人Agent重构技术实现.md` |
| T2 | 后端修复(缺陷 1/2/3 + 广播 + 房间设置透传) | `agent/judge*.go` / `game/werewolf/{room,judge_summary_bridge,view}.go` / `api/room_api.go` / `service/room_service.go` |
| T3 | 前端 UI(创建界面开关 + JudgePanel 活动流 + 横幅增强 + 类型 + i18n) | `RoomCreateModal.tsx` / `JudgePanel.tsx` / `JudgeActionBar.tsx` / `types/werewolf.ts` / `i18n/locales/*.ts` |
| T4 | 测试 + 编译 + 构建 + 运行 + 提交 | `go test ./...` / `go build` / `tsc` / `npm run build` / `./rebuild_restart_app.sh` / `git commit` |

---

> **变更记录**
> - 2026-07-16:初版。基于审计发现的三个阻塞缺陷,设计「有 Agent 就有主持人 + 一举一动活动流 + 全程阶段播报 + 广播进公屏」重构方案;配套技术实现细节拆分到 `主持人Agent重构技术实现.md`。
