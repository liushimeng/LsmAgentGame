# 狼人杀 13 人局 Agent 升级 — 20260810-06（行为承诺与兑现追踪 CommitmentLedger）

> **文档版本**：2026-08-10
> **采纳来源**：[`DeepSeek-Surpport-01.md`](../../../DeepSeek-Surpport-01.md) §一.3「行为承诺与兑现追踪（`public_commit` + `CommitmentLedger`）」
> **采纳理由（为什么选它）**：
> 1. **7 份知识库文档中复杂度与价值综合最高的单体功能**：DeepSeek 将其列为 P1-A 最高优先级（独立模块 + 高传播价值 + 低风险），横跨 Agent 工具 / 服务端账本 / 人类 WS 帧 / 前端 UI / 结算页 5 个系统。
> 2. **直击 LLM 狼人杀 Agent 的核心能力短板**：跨轮次行为一致性是 Agent 最难的挑战——承诺系统把这种一致性变成可量化的游戏机制，同时也是 LLM 长期规划能力的最佳展示。
> 3. **形成完整博弈闭环**：「承诺→兑现/违背→信任变化→下一轮策略调整」，承诺本身既是社交资本也是把柄，真预言家敢承诺查验结果，悍跳狼也敢承诺（但兑现不了）。
> 4. **工程风险可控**：纯增量模块，不修改既有博弈逻辑；承诺账本独立存储，兑现判定为纯函数；与 §119 协议层隔离 / §92a 锁内变体 / §130 接线验证 / §135 身份公开 四大硬约束全部有清晰的落地方案。

---

## 0. 与现有实现对照表

| 承诺类型 | 服务端可验证 | Agent 工具 | 人类 UI | 结算展示 |
|---|---|---|---|---|
| 预言家查验承诺 | ✅ 基于 `SeerCheckHistory` | ✅ `public_commit` | ✅ 承诺按钮 | ✅ 兑现率排行 |
| 投票目标承诺 | ✅ 基于 `LastDayVoteMap` | ✅ `public_commit` | ✅ 承诺按钮 | ✅ 兑现率排行 |
| 不投票承诺 | ✅ 基于 `LastDayVoteMap` | ✅ `public_commit` | ✅ 承诺按钮 | ✅ 兑现率排行 |
| 不使用技能承诺 | ✅ 基于夜间行动记录 | ✅ `public_commit` | ✅ 承诺按钮 | ✅ 兑现率排行 |
| 赛后道歉承诺 | ✅ 基于终局阵营 | ✅ `public_commit` | ✅ 承诺按钮 | ✅ 兑现率排行 |

---

## 1. 核心设计

### 1.1 承诺账本（CommitmentLedger）

新增文件 `ServerGo/game/werewolf/commitment_ledger.go`（≤400 行）。

```go
// CommitTemplate 承诺模板类型（封闭枚举，禁止自由字符串）
type CommitTemplate string

const (
    CommitSeerCheck      CommitTemplate = "seer_check"       // 「如果我是预言家，今晚验 N 号」
    CommitVoteTarget     CommitTemplate = "vote_target"      // 「如果 N 号是狼，我明天投票放逐他」
    CommitNoVoteFor      CommitTemplate = "no_vote_for"      // 「本轮我不会投票给 N 号」
    CommitNoUseSkill     CommitTemplate = "no_use_skill"     // 「我今晚不会使用技能」
    CommitApologyIfGood  CommitTemplate = "apology_if_good"  // 「N 号如果是好人，我公开道歉」
)

// CommitStatus 承诺状态
type CommitStatus string

const (
    CommitStatusPending   CommitStatus = "pending"    // 等待验证
    CommitStatusFulfilled CommitStatus = "fulfilled"  // 已兑现
    CommitStatusBroken    CommitStatus = "broken"     // 已违背
    CommitStatusExpired   CommitStatus = "expired"    // 条件不成立，过期无效
)

// Commitment 一条承诺
type Commitment struct {
    ID        int64          `json:"id"`
    Seat      int            `json:"seat"`        // 承诺者座位
    Round     int            `json:"round"`       // 承诺发生的天数
    Template  CommitTemplate `json:"template"`
    ParamSeat int            `json:"param_seat"`  // 参数：目标座位号（多数模板用）
    ParamText string         `json:"param_text"`  // 参数：补充文本（可选，≤30 字）
    Status    CommitStatus   `json:"status"`
    VerifiedAt int64         `json:"verified_at,omitempty"` // 验证时间（UnixMilli）
    CreatedAt  int64         `json:"created_at"`
}

// CommitmentLedger 房间级承诺账本。
// 所有方法均为 *Locked 语义：调用方必须已持有 r.mu（§92a）。
type CommitmentLedger struct {
    nextID int64
    items  []*Commitment
}
```

### 1.2 兑现判定时机与规则

| 模板 | 判定时机 | 兑现条件 | 违背条件 | 过期条件 |
|---|---|---|---|---|
| `seer_check` | 次夜预言家行动后 | 承诺者是真预言家 ∧ 实际查验目标 = ParamSeat | 承诺者是真预言家 ∧ 实际查验目标 ≠ ParamSeat | 承诺者不是预言家 |
| `vote_target` | 次日投票结算后 | 承诺者投了 ParamSeat ∧ ParamSeat 是狼人 | 承诺者投了 ParamSeat ∧ ParamSeat 是好人 | 承诺者未投 ParamSeat |
| `no_vote_for` | 当日投票结算后 | 承诺者未投 ParamSeat | 承诺者投了 ParamSeat | — |
| `no_use_skill` | 次夜行动结束后 | 承诺者当夜未使用技能 | 承诺者当夜使用了技能 | 承诺者无技能 |
| `apology_if_good` | 终局时 | ParamSeat 是好人 ∧ 承诺者公开发言道歉 | ParamSeat 是狼人 | 承诺者已死亡 |

**关键约束（§135 身份公开公平性）**：
- 终局前，承诺的兑现状态**仅对承诺者本人 + 观战者可见**，对其他玩家标记为 `pending`。
- 前端 `ClientGameState` 中，承诺列表按视角脱敏：本人和 spectator 看全量 `status`，其他玩家只看 `status=pending` 的承诺 + 自己的承诺。

### 1.3 服务端 API

#### Agent 工具：`public_commit`

```json
{
  "name": "public_commit",
  "description": "公开做出一个可验证的行为承诺。所有玩家都会看到你做出了承诺，但兑现状态只有你自己和观战者知道（终局时全部公开）。",
  "parameters": {
    "template": "string (seer_check|vote_target|no_vote_for|no_use_skill|apology_if_good)",
    "target_seat": "int (1-13，承诺涉及的目标座位号)",
    "reason": "string (≤30字，你做出这个承诺的理由，公开发布)"
  }
}
```

- 注册在 **speak phase**（白天发言阶段）。
- 计入 `speakLimiter`（与发言共享令牌桶，避免刷屏）。
- 成功后通过 `emitActivity(kind=commit_made)` 广播到全房。

#### 人类 WS 帧：`game.werewolf_commit`

```json
{
  "template": "seer_check",
  "target_seat": 3,
  "reason": "我认为3号身份可疑"
}
```

- 严格 JSON 校验（`DisallowUnknownFields`，§84b）。
- 走 `WerewolfManager.Action_PublicCommit` 单一真相源。

---

## 2. 兑现判定实现

### 2.1 判定触发器位置

| 模板 | 触发点 | 文件 |
|---|---|---|
| `seer_check` | `endSeerPhase`（预言家阶段结束） | `engine_night.go` |
| `vote_target` | `EmitVoteResult`（投票结果广播） | `activity_emitter.go` |
| `no_vote_for` | `EmitVoteResult`（投票结果广播） | `activity_emitter.go` |
| `no_use_skill` | `advanceDay`（白天开始，汇总夜间行动） | `engine_day.go` |
| `apology_if_good` | 终局结算 + 最后遗言阶段 | `checkVictory` / `engine_game_over.go` |

### 2.2 判定函数（纯函数，可单元测试）

```go
// EvaluateCommitmentsForTrigger 批量评估特定触发点的所有 pending 承诺
func (cl *CommitmentLedger) EvaluateForTrigger(
    trigger CommitTemplate,
    facts CommitFacts,
) []*Commitment { ... }

// CommitFacts 评估时的事实输入
type CommitFacts struct {
    SeerSeat          int              // 真预言家座位
    SeerCheckTarget   int              // 昨夜查验目标
    DayVoteMap        map[int]int      // seat -> target
    PlayerRoles       map[int]Role     // 座位→角色（终局才可用）
    PlayerFactions    map[int]Faction  // 座位→阵营（终局才可用）
    SkillUsedTonight  map[int]bool     // 今夜是否使用了技能
    CurrentDay        int
}
```

### 2.3 锁内变体（§92a 合规）

所有账本方法：
- `AddCommitmentLocked(...)` — 在发言阶段调用
- `EvaluateForTriggerLocked(...)` — 在阶段切换时调用
- `GetCommitmentsForSeatLocked(seat, viewerSeat) []*Commitment` — 视图脱敏
- `GetAllForSpectatorLocked() []*Commitment` — 观战者全量视图
- `GetFulfillmentRateLocked(seat) float64` — 兑现率（结算用）

---

## 3. 前端实现

### 3.1 新增状态字段

`ClientGameState`（`view.go`）新增：
```go
Commitments []CommitmentJSON `json:"commitments,omitempty"`
```

`CommitmentJSON` 按视角脱敏后下发（§135）。

### 3.2 前端组件

| 组件 | 位置 | 说明 |
|---|---|---|
| `CommitmentPanel.tsx` | `components/werewolf/` | 承诺列表（折叠面板，白天发言阶段展开） |
| `CommitmentButton.tsx` | `components/werewolf/` | 「做出承诺」按钮 + 弹窗选择模板 |
| `SeatCell` 扩展 | `components/werewolf/SeatCell.tsx` | 座位旁显示承诺数量徽章（📝 N） |
| `SettlementModal` 扩展 | `components/werewolf/SettlementModal.tsx` | 「🏅 承诺兑现率」段落（排行榜 + 每条承诺的状态） |
| `HistoryDrawer` 扩展 | `components/werewolf/HistoryDrawer.tsx` | 时间轴中显示承诺事件 |

### 3.3 i18n 三语种

新增 `werewolf.commitment.*` 约 25 个键（zh-CN / en / ja）：
- 模板名称（5 个）
- 状态标签（4 个）
- 承诺弹窗文案
- 结算排行榜标题
- 操作按钮

---

## 4. Agent 集成

### 4.1 工具注册

- `MountTools` 注册 `public_commit`（phase 过滤：仅 speak 阶段）
- `DispatchTool` 派发 `public_commit` → `ToolRunner.PublicCommit`
- `BuildTools` 文档同步
- 三语 i18n（§97 四处同步 + i18n = 五处）

### 4.2 GameContext 注入

`GameContext` 新增：
```go
MyCommitments    []CommitmentInfo  // 我自己的承诺（含真实状态）
PublicCommitments []CommitmentInfo // 公开可见的他人承诺（仅 pending 状态）
```

System prompt 追加「承诺系统」段：
- 说明 5 种承诺模板及其兑现条件
- 强调「承诺是博弈武器：高兑现率 = 高信任度，低兑现率 = 被怀疑」
- 建议策略：真预言家应善用查验承诺建立信任，狼人应谨慎承诺避免暴露

### 4.3 ToolRunner 接口

`ToolRunner` 新增方法：
```go
PublicCommit(template CommitTemplate, targetSeat int, reason string) error
```

实现委托给 `WerewolfManager.Action_PublicCommit`（双变体：`Action_PublicCommit` + `Action_PublicCommitLocked`，§92a）。

---

## 5. 结算展示

### 5.1 结算页新增段落

在 `SettlementModal` 的金币结算下方新增「🏅 承诺兑现榜」段落：

| 排名 | 玩家 | 承诺数 | 兑现 | 违背 | 兑现率 |
|---|---|---|---|---|---|
| 1 | 5 号 · DeepSeek | 3 | 2 | 0 | 67% |
| 2 | 3 号 · 豆包 | 5 | 3 | 2 | 60% |
| ... | ... | ... | ... | ... | ... |

每条承诺展开可看详情（模板 / 参数 / 发生轮次 / 状态）。

### 5.2 数据来源

`game.settlement` 帧新增 `commitments_summary` 字段，由 `checkVictory` 处组装（持锁态调用 `CommitmentLedger.GetFulfillmentRateLocked`）。

---

## 6. 风险与约束对照

| 硬约束 | 落地方案 |
|---|---|
| **§92a 锁内变体** | `CommitmentLedger` 所有方法均为 `*Locked`；公开 API 包一层加锁；被 `phaseWatchdogTick` / `EmitVoteResult` 等持锁态调用的路径直接用锁内变体 |
| **§97 五处同步** | 新增 `public_commit` 工具不需要新增 phase，但需要同步：`MountTools` / `DispatchTool` / `BuildTools` 文档 / 三语 i18n / `SkipPhaseAction`（不需要，因为不新增 phase） |
| **§119 协议层隔离** | 承诺兑现状态对其他玩家保密（仅本人 + spectator 可见真实状态）；承诺本身是公开信息（活动流广播）；不进 `chat_message` 表（走 Activity 帧） |
| **§130 接线验证** | 5 个模板的兑现判定必须各自有对应的触发点（`grep "EvaluateForTrigger"` 验证）；`public_commit` 工具必须 `grep "public_commit"` 在 `MountTools` / `DispatchTool` / `BuildTools` 三处命中 |
| **§135 身份公开公平性** | 终局前兑现状态仅本人 + spectator 可见；`seer_check` 模板的兑现判定不能暴露预言家身份给其他玩家；视图层按 `viewerSeat` 脱敏 |
| **§128 对话即思考** | 承诺理由（reason）是公开发言的一部分，走 `speakLimiter`；内心思考仍走 `HeartThought` |

---

## 7. 文件清单

### 后端（ServerGo/）

| 文件 | 操作 | 预估行数 |
|---|---|---|
| `game/werewolf/commitment_ledger.go` | 新建 | ~350 |
| `game/werewolf/commitment_ledger_test.go` | 新建 | ~300 |
| `game/werewolf/room_action.go` | 修改：加 `Action_PublicCommit` 双变体 | ~60 |
| `game/werewolf/view.go` | 修改：加 `Commitments` 视图脱敏 | ~80 |
| `game/werewolf/activity_emitter.go` | 修改：加 `commit_made` / `commit_evaluated` 活动 | ~40 |
| `game/werewolf/engine_day.go` | 修改：投票后触发 `EvaluateForTrigger(vote_target/no_vote_for)` | ~20 |
| `game/werewolf/engine_night.go` | 修改：预言家阶段后触发 `EvaluateForTrigger(seer_check)`；夜末触发 `no_use_skill` | ~40 |
| `game/werewolf/engine_game_over.go` | 修改：终局触发 `apology_if_good` + 结算数据组装 | ~30 |
| `agent/wwplayer/tools.go` | 修改：注册 `public_commit` 工具 + 派发 | ~80 |
| `agent/wwplayer/prompt.go` | 修改：system prompt 追加承诺系统段 | ~30 |
| `agent/wwtypes/context.go` | 修改：`GameContext` 加承诺字段 | ~15 |
| `agent/wwplayer/agent_runner.go` | 修改：`buildAgentContextLocked` 注入承诺数据 | ~20 |
| `ws/game_service_werewolf.go` | 修改：加 `game.werewolf_commit` WS 帧处理 | ~40 |
| `game/werewolf/room_state.go` | 修改：REST 房间详情加承诺字段（脱敏） | ~15 |

### 前端（ClientWeb/）

| 文件 | 操作 | 预估行数 |
|---|---|---|
| `components/werewolf/CommitmentPanel.tsx` | 新建 | ~200 |
| `components/werewolf/CommitmentButton.tsx` | 新建 | ~150 |
| `components/werewolf/SettlementModal.tsx` | 修改：加兑现率排行榜段 | ~100 |
| `components/werewolf/HistoryDrawer.tsx` | 修改：时间轴显示承诺事件 | ~60 |
| `components/werewolf/SeatCell.tsx` | 修改：加承诺数量徽章 | ~30 |
| `types/werewolf.ts` | 修改：加 `Commitment` 类型 + 扩展 `ClientGameState` | ~30 |
| `i18n/zh-CN/werewolf.ts` | 修改：加 ~25 键 | ~30 |
| `i18n/en/werewolf.ts` | 修改：加 ~25 键 | ~30 |
| `i18n/ja/werewolf.ts` | 修改：加 ~25 键 | ~30 |
| `styles/werewolf-panels.css` | 修改：加承诺相关样式 | ~80 |

### 总计

后端 ~1100 行 / 前端 ~740 行 / 合计 ~1840 行。

---

## 8. 测试计划

### 单元测试（`commitment_ledger_test.go`）

1. `TestAddCommitment_SeerCheck` — 添加预言家查验承诺
2. `TestEvaluate_SeerCheck_Fulfilled` — 真预言家兑现查验承诺
3. `TestEvaluate_SeerCheck_Broken` — 真预言家违背查验承诺
4. `TestEvaluate_SeerCheck_Expired` — 非预言家的查验承诺过期
5. `TestEvaluate_VoteTarget_Fulfilled` — 投票目标承诺兑现
6. `TestEvaluate_NoVoteFor_Broken` — 不投票承诺被违背
7. `TestViewDesensitization` — 视图脱敏：其他玩家只能看到 pending
8. `TestViewDesensitization_Spectator` — 观战者可见全部状态
9. `TestFulfillmentRate_Empty` — 无承诺时兑现率
10. `TestConcurrentAdd` — 并发添加承诺（持锁测试，超时守卫）

### 集成测试

11. `TestPublicCommit_ViaWS` — WS 帧 `game.werewolf_commit` → 账本写入 → Activity 广播
12. `TestAgentPublicCommit_Dispatch` — Agent 调 `public_commit` 工具 → 正确派发

---

## 9. 配置项

```
werewolf.commitments_enabled = true   # 全局开关，默认开启
werewolf.max_commits_per_day = 3      # 每人每天最多承诺次数（防刷屏）
```

---

## 10. 知识库文件清理项

本设计文档落地后，需从以下知识库文件中删除对应内容：

| 文件 | 删除章节 | 原因 |
|---|---|---|
| `DeepSeek-Surpport-01.md` | §一.3「行为承诺与兑现追踪（public_commit + CommitmentLedger）」 | 完整采纳 |
| `DeepSeek-Surpport-01.md` | §0 对照表中「§一.3 行为承诺与兑现追踪」行 | 同步更新状态为「✅ 已实现」 |
| `DeepSeek-Surpport-01.md` | §七「工程化优先级建议」中 P1-A 行 | 已完成 |
| `DouBao-Surpport-01.md` | §五.4「决策可解释性」中与承诺相关的交叉引用部分 | 部分关联，保留主体，删交叉引用 |
| `Mimo-Surpport-01.md` | §6.4「自我修正」中与承诺相关的部分 | 弱关联，保留 |

> **注意**：DeepSeek §一.1「多假说并行推演」、§一.2「反事实推理链」等其余 12 条建议均不涉及，保留不动。
