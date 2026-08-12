# 主持人 Agent 重构技术实现 — §13 v2026-07-16

> 配套 [`docs/狼人杀-重构方案/主持人Agent重构设计.md`](主持人Agent重构设计.md)。本文档是实现的唯一技术依据,给出文件/字段/函数/锁不变式/测试的具体改动点。
> 审计日期:2026-07-16。三个阻塞缺陷见设计文档 §0。

## T0. 现状速览(改动基线)

| 文件 | 现状 |
|------|------|
| `agent/judge.go` | `AgentJudge`(L62)含 `Provider`/`apiKey`/`Registry` 字段,但 `NewAgentJudge`(L97)与 `startJudgeGoroutine` 都不赋值 |
| `agent/judge_tools.go` | `BuildJudgeTools`(L18)已 5 工具;`DispatchJudgeTool`(L89)只写 transcript |
| `agent/judge_prompt.go` | system/user prompt 完整 |
| `agent/judge_summary.go` | 整局总结 LLM 路径正常(绕过 j.Provider) |
| `game/werewolf/judge_summary_bridge.go` | `startJudgeGoroutine`(L113)仅检查全局 JudgeMode;`wakeJudgeLocked`(L349)已定义但除 summary 外无调用 |
| `game/werewolf/room.go` | 法官字段 L172;启动点 L1231/L4271;停止 L2272;game_over_summary 唯一真实发出 L2385 |
| `game/werewolf/engine.go` | GameState 法官字段 L266;阶段枚举 L17-34;`setPhaseAndDeadline` 阶段切换 |
| `game/werewolf/view.go` | `BuildClientStateWithRoom`(L738) `r.judge!=nil`→`JudgeEnabled` |
| `api/room_api.go` | `createRoomRequest`(L18) 仅 `name`+`agent_seats` |
| `service/room_service.go` | `CreateRoomWithAgents`(L554);`AgentSeatConfig`(L27) |
| `config/config.go` | `JudgeMode`/`JudgeModelKey`(L125),默认 `"ai"`(L549) |
| `ClientWeb/.../RoomCreateModal.tsx` | 无法官开关 |
| `ClientWeb/.../JudgePanel.tsx` | 缺 ToolCalls 活动流 |
| `ClientWeb/src/types/werewolf.ts` | `JudgeContextJSON`(L152) 缺 activities |

---

## T1. 后端改动

### 1.1 修复缺陷 🔴1 — Provider 注入

**文件**:`ServerGo/game/werewolf/judge_summary_bridge.go` `startJudgeGoroutine`

在 `j := agent.NewAgentJudge(...)` 之后、`ctx, cancel := ...` 之前插入:

```go
if provider, key, err := m.registry.Get(modelKey); err != nil {
    logger.L().Warn("judge: registry.Get failed, judge will use rule fallback",
        zap.String("room_id", r.RoomID), zap.String("model_key", modelKey), zap.Error(err))
} else {
    j.SetProvider(provider, key) // 封装赋值,避免外部直接写字段
}
```

**文件**:`ServerGo/agent/judge.go` — 新增 setter(goroutine 启动前调用,之后只读):

```go
func (j *AgentJudge) SetProvider(p llm.LLMProvider, key string) {
    j.mu.Lock()
    defer j.mu.Unlock()
    j.Provider = p
    j.apiKey = key
}
```

> 与 `agent.NewWithRoom`(:558 `provider,key,err:=registry.Get(modelKey)`)一致。`registry.Get` 已拒绝占位 key。

### 1.2 修复缺陷 🟡2 — 依赖 Agent 数量启动

**文件**:`ServerGo/game/werewolf/judge_summary_bridge.go` `startJudgeGoroutine`,在现有全局 JudgeMode 检查(L114)之后加:

```go
if !r.JudgeDesired {
    logger.L().Info("werewolf: judge disabled by room setting", zap.String("room_id", r.RoomID))
    return
}
if len(r.seatModelKeys) == 0 {
    logger.L().Info("werewolf: no agents, skipping judge goroutine", zap.String("room_id", r.RoomID))
    return
}
```

**文件**:`ServerGo/game/werewolf/room.go` `WerewolfRoom` 加字段:

```go
JudgeDesired  bool   // 创建者是否启用法官(默认 true;仅当 judge.mode=="off" 时为 false)
JudgeModelKey string // 创建者指定的法官模型 key;空=跟随首个 Agent
```

两处调用点(`room.go:1231`、`:4271`)自动继承,无需改动。

### 1.3 修复缺陷 🟡3 — 各阶段事件接通

**文件**:`ServerGo/game/werewolf/room.go` — 在 `phaseWatchdogTick` 内追踪上一 phase,变化时调 wake。方案:引入包级辅助 `judgeKindForPhase(phase) string`:

```go
// 映射表(秘密阶段返回 "" → 不调 wake)
func judgeKindForPhase(p Phase) string {
    switch p {
    case PhaseFilling:      return "judge_filling_welcome"
    case PhasePreWolves:    return "judge_pre_wolves"
    case PhaseDawn:         return "judge_dawn_announce"
    case PhaseSheriff:      return "judge_sheriff_start"
    case PhaseSpeak:        return "judge_speak_start"
    case PhaseVote:         return "judge_vote_start"
    case PhaseIdiotReveal:  return "judge_idiot_reveal"
    case PhaseHunterShoot:  return "judge_hunter_shoot"
    case PhaseDeathLyric:   return "judge_last_words"
    case PhaseRestartVote:  return "judge_restart_vote_result"
    case PhaseGameOver:     return "judge_game_over"
    default:                return "" // NightWolves/Seer/Witch 静默
    }
}
```

`phaseWatchdogTick` 在锁内(不持 r.mu 的部分)对比 `r.state.Phase` 与 `r.lastJudgePhase`;变化且 kind 非空时调 `m.wakeJudgeLocked(kind, nil)` 并更新 `r.lastJudgePhase`。(具体插入点见 `room.go::phaseWatchdogTick`,在 phase 已经稳定之后。)

**WerewolfRoom** 加字段 `lastJudgePhase Phase`(初值 `PhaseFilling-1` 哨兵避免首 tick 误触发)。

### 1.4 广播路径(设计愿望落地)

**文件**:`ServerGo/ws/chat_service.go` — 新增:

```go
func (s *ChatService) SendFromJudge(roomID, text, kind string, fromModel string) error {
    // 写 chat_message(is_judge=true) + chatQueue 广播,FromAccount="[法官·{model}]"
}
```

**文件**:`agent/judge_tools.go` `DispatchJudgeTool` — `announce`/`summary`/`declare_cause` 成功后调 `BroadcastJudge(roomID, text, kind)`(ToolRunner 接口新增方法或全局回调)送公屏。注入方式:给 `AgentJudge` 加 `broadcast fn(text, kind string)` 回调字段,在 `startJudgeGoroutine` 注入。

### 1.5 JudgeTranscript 扩展 + 活动流

**文件**:`ServerGo/agent/judge.go`:

```go
type JudgeActivity struct {
    At    int64  `json:"at"`
    Tool  string `json:"tool"`
    Input string `json:"input"`   // 参数摘要 ≤120 字符
    Out   string `json:"out"`     // 产物文本
    LLMMs int64  `json:"llm_ms,omitempty"`
}
type JudgeTranscript struct {
    // ...现有字段...
    Activities []JudgeActivity `json:"activities"` // 最近 30 条
    LastLLMMs  int64           `json:"last_llm_ms"`
}
```

`DispatchJudgeTool` 成功后 append Activity(超 30 队首淘汰)。`judgeChatOrFallback` 记 `LastLLMMs`。

### 1.6 房间级法官设置透传

**文件**:`ServerGo/api/room_api.go` `createRoomRequest` 加:

```go
Judge *struct {
    Mode     string `json:"mode"`      // "ai"|"human"|"off"
    ModelKey string `json:"model_key"` // 可选
} `json:"judge,omitempty"`
```

透传到 `CreateRoomWithAgents` → `WerewolfRoom{JudgeDesired: req.Judge.Mode!="off", JudgeModelKey: req.Judge.ModelKey}`。

**文件**:`ServerGo/service/room_service.go` — `CreateRoomWithAgents` 参数新增 `judgeDesired bool, judgeModelKey string`,落到房间。

### 1.7 view.go(不变)

`BuildClientStateWithRoom`(L738) 仍由 `r.judge!=nil` 决定 `JudgeEnabled`——逻辑已正确,无需改动。新增 `activities`/`last_llm_ms` 会自动随 `JudgeTranscript()` 复制反射到 `JudgeContext`(确认 `cs.JudgeContext = &tr` 全字段拷贝)。

---

## T2. 前端改动

### 2.1 创建界面(`RoomCreateModal.tsx`)

- 当 `agentCount > 0` 时在座位区下渲染主持人卡;
- 3 个模式单选(ai/human/off)+ 模型下拉(仅 ai 模式,留空=跟随首个 Agent);
- `onSubmit` 扩展为 `{ name, agent_seats, judge: { mode, model_key } }`;
- `types/api.ts` `CreateRoomOptions` 加 `judge?`。

### 2.2 `JudgePanel.tsx` 重构

- 保留当前宣告 + 总结;
- 新增「一举一动」活动流(Activities 列表):时间戳 + 工具 + 输入摘要 + 产物 + LLM 耗时;
- 在线状态 emoji(🟢/🟡/⚠️);
- 死因 verdict 分色徽章。

### 2.3 `JudgeActionBar.tsx` 增强

- 新宣告 pulse 动画(2s 边框高亮);
- 状态 emoji;维持现有 pending/quarantined/summary 徽章。

### 2.4 类型 + i18n

- `types/werewolf.ts` `JudgeContextJSON` 加 `activities?`/`last_llm_ms?`;`JudgeActivityJSON`;
- `types/api.ts` `CreateRoomOptions` 加 `judge?`;
- `i18n/locales/{zh-CN,en,ja}.ts` 加 `werewolf.judge.activity.*` 与模式选项词条。

---

## T3. 锁不变式(§92a)

- `wakeJudgeLocked` 在 `r.mu` 内建快照 + 非阻塞 channel send;`j.Run` 不持 `r.mu`;
- Provider/回调注入在 `go j.Run(ctx)` 之前完成,goroutine 内只读;
- `j.mu` 仅保护 transcript/activities 读写,与 `r.mu` 不重叠;
- `phaseWatchdogTick` 调 `wakeJudgeLocked` 的位置**不持 r.mu**(在已释放锁之后),避免 §92a 自死锁。

---

## T4. 测试

### 4.1 `ServerGo/agent/judge_test.go`(扩展)

- `TestJudge_ProviderInjection`
- `TestJudge_ActivitiesAppended`
- `TestJudge_BroadcastCalledOnAnnounce`

### 4.2 `ServerGo/game/werewolf/room_judge_test.go`(新建)

- `TestRoomJudgeWake_PerPhase`(11 类 kind)
- `TestRoomJudge_SilentAtNight`
- `TestRoomJudge_NoAgentsNoJudge`
- `TestRoomJudge_JudgeDesiredFalse`

### 4.3 构建验证

```bash
go build -o LsmAgentGame main.go
go test ./...
cd ClientWeb && tsc --noEmit && npm run build
```

### 4.4 运行 + 提交

```bash
./rebuild_restart_app.sh
git add -A && git commit -m "中文提交信息"
```

---

> **变更记录**
> - 2026-07-10:初版。
> - 2026-07-16:重写。基于审计三缺陷,新增 Provider 注入、Agent 数量守卫、阶段事件接通、广播进公屏、活动流、房间级设置透传的具体实现点。
