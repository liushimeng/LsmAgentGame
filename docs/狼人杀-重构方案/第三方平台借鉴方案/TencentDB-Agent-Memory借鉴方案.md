# 狼人杀 Agent 优化和解决方案 — 基于 TencentDB-Agent-Memory 架构分析

> 日期：2026-08-12 | 代码基准：`0fad5b3`
> 参考源：`/usr/local/LsmGitOpenSource/TencentDB-Agent-Memory`（Tencent 开源 Agent Memory Hub）
> 分析文档：[记忆管理](../其他Agent代码分析/TencentDB-Agent-Memory_记忆管理分析.md) ·
> [Context 管理](../其他Agent代码分析/TencentDB-Agent-Memory_Context管理分析.md) ·
> [意图识别与任务分解](../其他Agent代码分析/TencentDB-Agent-Memory_意图识别与任务分解分析.md)
> 前序方案：[基于 PI Agent 的优化方案](狼人杀Agent_根据_PI_优化和解决方案_20260811.md)（6 项已全部落地）

---

## 0. 执行摘要

本次审计对比 TencentDB-Agent-Memory 与本仓库狼人杀 Agent，发现 **7 项 P0 功能性缺陷**
与 **6 项 P1 架构性短板**。

**最重要的单一发现**：

> **AI 预言家查完人永远不知道查验结果；AI 女巫永远不知道今晚谁被刀。**
> 引擎算对了、字段填对了、UI 显示对了、活动流也发了，**唯独没有人把它渲染进 prompt**。
> 而人类玩家走 `BuildSeerInform` 完全正常 —— 这直接违反 §15「公平性」与 §120。

这一条的修复收益超过过去两个月所有特性之和。

### 全局观察：为什么会漏成这样

近 60 天 272 次提交（占全仓 41%）集中在 `agent/` + `game/werewolf/`，
标题几乎都是「§日期 4 项升级」。这是一个**以「每日 N 项特性打包」为节奏、
高速堆叠 2 个月**的系统。所有架构性短板的根因都可回溯到这个节奏：

> **特性入口做得很实，特性到 LLM 的「最后一公里」反复失守。**

CLAUDE.md 已把「声明了却从不接线」记为 §130/§134/§135/§20260811-08 **四次教训**
并写入自检条款，本次审计**仍新发现 12 处**。说明单点修复无法根治，
必须**机制化**（见 §4 U6）。

---

## 1. 两个系统的对照

| 维度 | TencentDB-Agent-Memory | 狼人杀 Agent（现状） |
|---|---|---|
| 记忆分层 | L0/L1/L2/L3 四层，各有独立载体 | **单层**：一个 model_key 一份 Markdown |
| 记忆检索 | RRF 混合检索（BM25 + 向量），top-K | **无检索**，4000 runes 全量注入 |
| 记忆隔离 | team/user/agent 三维 + 跨 session | 仅 model_key，**角色不隔离** |
| 冲突消解 | 4 动作分类器（store/update/merge/skip） | LLM 自由重写，无结构化决策 |
| 注入策略 | 分层差异化（直注/索引/工具） | 一律全量拼到 user prompt 末尾 |
| Token 预算 | tiktoken + 4 级压缩阶梯 + 保护区 | **字节**预算，且只覆盖 messages |
| 块间优先级 | HOOK_PRIORITY + 语义槽 | **无**，16 个块无条件 `s +=` |
| Prompt cache | 架构第一约束，stable/dynamic 二分 | **从未启用**（字段存在，从不赋值） |
| 失败降级 | 每层 fail-soft，注入失败=裸转发 | 有 quarantine，但**不可恢复** |

---

## 2. P0 功能性缺陷（7 项，全部已 grep 验证）

### P0-1 ★ 预言家查验结果与女巫狼刀目标从未进入 LLM 上下文

**证据链**（每一条都独立验证过）：

| 环节 | 位置 | 状态 |
|---|---|---|
| 引擎写入 | `room_agent.go:766` `gc.MySeerCheck = int(gs.Players[seat].LastSeerCheck)` | ✅ 有 |
| 引擎写入 | `room_agent.go:770` `gc.WolfTarget = int(gs.WolfKillTarget)` | ✅ 有 |
| 字段定义 | `wwtypes/context.go:25-26` | ✅ 有 |
| **Agent 侧读取** | 全仓 grep `agent/` 目录 | ❌ **0 处** |
| **prompt 渲染** | `prompt.go` | ❌ **0 处** |
| 工具返回值 | `agent_runner.go:264` `SeerCheck` → `return "ok", nil` | ❌ 不含结果 |
| 人类玩家路径 | `view.go:1285` `BuildSeerInform` → `LastResultFaction` | ✅ **正常** |

**更严重的是**：`MySeerCheck` 只存了**查验的座位号**，连阵营结果都没存
—— 即便现在去渲染它，也只能说「你昨晚查了 4 号」，说不出「4 号是狼人」。
`FactionOf(gs.Roles[t])` 这个结果**只在 `BuildSeerInform` 里算过，从未进过 GameContext**。

`witch_act` 的 tool schema（`tools.go:312`）只说「救活今晚被狼杀的玩家」，
**不告知是谁**。`EmitSeerCheck`（`activity_emitter.go:165`）文案仅
「🔮 预言家查验 N号」不含阵营，且 `silentForBots=true`（`:177`）。

**影响**：两个核心神职的技能对 Agent **完全失效**，只能靠 tool 描述里的策略话术瞎猜。
人类玩家不受影响 ⇒ 直接违反 §15 公平性。同时使 §135「身份公开公平性」的大量工作
在 AI 侧失去意义。

**修复**：新增 `MySeerCheckFaction` 字段 + `NightPrivateInfoBlock` 渲染块。

---

### P0-2 `EmitSeerCheck` 注释与实参相反（P0-1 潜伏的原因）

`activity_emitter.go:163-164` 注释写
「silentForBots=false(LLM 不知情…其他存活 bot 看到会改变策略)」，
而 `:177` 实参是 **`true`**。

读代码的人会以为 bot 能从活动流看到查验结果 —— 这很可能就是 P0-1
长期未被发现的直接原因。

---

### P0-3 endpoint breaker 未列为 transient ⇒ 上游抖动导致全房批量 quarantine

`run_llm.go:63-64` 构造：

```go
Source:  "breaker",
Message: "anthropic: endpoint breaker open (all endpoints); short-circuited before send (§20260810-15)",
```

`run_helpers.go:99-108` 的 transient 子串表（`connection refused` / `connection reset` /
`reset by peer` / `broken pipe` / `no such host` / `tls handshake timeout` /
`use of closed network connection` / `context canceled`）**无一匹配**。

而 400/429 熔断在 `run.go:1094,1102` 有显式特判。

**影响**：endpoint breaker 是 60s 自愈的上游级故障，却是唯一计入 `consecutiveFailures`
的熔断信号。一次上游抖动 ⇒ 13 个 bot 同时累积 ⇒ 批量 quarantine ⇒ 10 分钟后判和。

**修复成本：一行。**

---

### P0-4 quarantine 单向不可恢复（且这是无意的）

`agent.go:1154` 明写 `a.quarantined = false`，但两个调用点
（`run.go:1270` / `1833`）**都在 `handleEvent` 内**，
而 `run.go:570` 入口第一行就是 `if a.IsQuarantined() { return }`；
`speak_floor.go:174` 同样跳过 quarantined。

⇒ **代码写了恢复逻辑但路径不可达**。而 `room_config.go:191` 的注释
还依赖「任一 bot 恢复会让计数器清零」这条**不存在的路径**。

---

### P0-5 `transient` 语义使 cf 可能永远为 0，同时禁用 quarantine 与 auto-skip

`RecordFailure`（`agent.go:1208-1213`）transient 只滑窗口不递增；
auto-skip 触发条件是 `ConsecutiveFailures() >= 1`（`run.go:1172`）；
`isAnthropicTimeout`（`run_helpers.go:82`）用**裸 `"timeout"` 子串**匹配。

⇒ 持续 timeout 的 bot：cf 恒 0 ⇒ **既不 quarantine 也不 auto-skip**，
每轮烧满退避后无限 reWake。这是「13 人局慢模型拖垮整局」的机理。

---

### P0-6 `defer` 写在 `for` 循环体内 ⇒ 单 bot 一次 wake 吃满全房信号量

已验证缩进：`run.go:740` 是 `for {`，而
`run.go:851 defer a.ReleaseLLMSlot()`、`:869`、`:908 defer parentCancel()`
**均在循环体内**。

⇒ 5 轮内层循环 = acquire 信号量 5 次，直到 `handleEvent` 返回才释放。
**cap=4 的房间信号量被单个 bot 一次 wake 吃满**，其余 12 bot 全部 5s 等待失败 → reWake。
与 `run.go:317` 注释宣称的「不让慢模型阻塞快模型」**完全相反**。

---

### P0-7 发言下限(2/60s) 与同座位冷却(60s) 数学上互斥

已验证默认值：`cfgWerewolfMinSpeaks`=2（`engine_state.go:282`）、
`cfgWerewolfSameSeatSpeakCooldownSec`=60（`room_prop.go:134`）。

每座位每分钟最多 1 次公开发言 ⇒ floor 的 `count>=2` 对每个 bot **永远不满足**
⇒ 每 20s 给每个 alive bot 推 floor wake ⇒ 13 人局 **39 次注定失败的 LLM 调用/分钟**，
且挤占 cap=4 信号量。

---

## 3. P1 架构性短板（6 项）

### P1-1 prompt 层零 token 预算、零块间优先级

- `BuildUserPrompt`（`prompt.go:200-670`）是 **41 个块的无条件顺序 `s +=` 链**
  （其中 16 个是 `XxxBlock()` 函数）
- **没有任何一处是「预算不足所以跳过」** —— 门控全是业务条件（`if ctx.MyTurn`）
- **块内部也无上限**：`HypothesisTableBlock` 无条数限制；
  `RumorBlock` 注释说「最近 5 条」但代码无 5 条限制
- `context_budget_test.go`（255 行）**全部测 Memory 层，无一测 prompt 长度**

**实测数据**（本次审计新增测量）：

| 项 | 字节 | ≈tokens |
|---|---|---|
| `BuildSystemPrompt` | **13,853** | ~3,500 |
| `BuildUserPrompt`（最小上下文） | 3,815 | ~1,000 |
| tools JSON schema | ~10,000 | ~2,500 |
| **单次调用固定开销** | **~28KB** | **~7,000** |

`MaxTokens: 2048`（`run.go:817`）—— **输出侧有上限，输入侧没有**。

唯一的自适应路径是 `run.go:1073`：等 provider 返回 400
「exceed max message tokens」后才 `PruneByBytesAggressive()`。**无 pre-flight 检查**。

> 现有 `getModelContextBudget`（`agent.go:1944`）是**字节**不是 token，
> 中文 UTF-8 3 bytes/字，与真实 token 只有粗略相关；且是 8 键硬编码 map，
> **第 9 个模型静默 fallback 到 200KB**。

### P1-2 prompt cache 从未启用（投入产出比最高的优化）

`llm/types/types.go:149` `SystemBlock.CacheControl` 字段**存在**，
但 `grep -c "CacheControl" agent/wwplayer/prompt.go` = **0**。

`prompt.go:33/38/191` 三处注释**宣称**命中 Anthropic prompt cache 前缀，
`memory.go:392` 甚至为 `CacheControl` 写了字节估算分支 —— 但从未赋值。

⇒ **~14KB × 每次调用 × 每 bot 全额计费**。

### P1-3 记忆全量注入无检索（对照 TencentDB 最大的差距）

| | TencentDB | 狼人杀 |
|---|---|---|
| 检索 | RRF 混合，top-5 | **无** |
| 隔离 | team/user/agent 三维 | 仅 model_key |
| 角色隔离 | — | **无**（预言家学的教训坐狼人时照样注入） |
| 分层注入 | L3 直注/L2 索引/L1 工具 | 一律全量 |

`run.go:664` 无条件 `InjectBlock(a.MemoryMD)`，4000 runes（≈3200 token）
**每次 LLM 调用都全量重发**。13 人局单 bot 一局 50+ 次调用 = **16 万 token 纯重复**。

且 `difficulty.MemoryInjectRunes`（1500/4000/6000 三档）
**4 处赋值、0 处读取** —— 难度分级对记忆注入量完全无效。

**没有质量评估、没有矛盾消解**：迭代 prompt 只让 LLM「更新」，
无胜率回归、无「这条教训是否真的提升了表现」的度量 ⇒ 记忆可能单调累积噪声，
**且没有任何机制能发现这件事**。

### P1-4 死代码与「声明了却从不接线」（本次新发现 12 处）

| 项 | 位置 | 状态 |
|---|---|---|
| `EmotionStyleBlock` | `emotion.go:457` | 零调用 |
| `OthersEmotionBlock` | `emotion.go:490` | 零调用 ⇒ **「感知他人情绪」整个特性未接线** |
| `LastGameMemoryBlock` | `wwjudge/judge_summary.go:435` | 零调用 |
| `PhasePromptHint` | `phase_config.go:211` | 13 阶段中文提示写好，零调用 |
| `WolfTeammateHint` | `prop_blocks.go:419` | 零调用 |
| `tool_hooks.go` | 220 行 | `Agent.toolHooks` 无赋值无读取 |
| `phase_config.go` | 240 行 | 且 `ToolKeys` 已与 `BuildTools` 漂移 |
| `MaxToolUse` ×2 | `agent.go:128` / `difficulty.go:30` | 均零消费者 |
| `MemoryInjectRunes` | `difficulty.go:31` | 4 赋值 0 读取 |
| `SpeakLimiterScale` | `difficulty.go:34` | 4 赋值 0 读取 |
| `demon_hunter_hunt_skip` | `run.go:373` 产出 | `dispatchToolInner` **缺 case** |
| `BuildSystemPromptWithEmotion` | `wwtypes/context.go:108` 引用 | **函数不存在** |

另：`Phase.String()` 只返回小写，而 `tools.go`/`run.go` 有 **100 处 PascalCase
`"PhaseXxx"` 分支是死代码**。

### P1-5 五个文件超出 §4 的 1800 行硬上限

| 文件 | 行数 | 超限 |
|---|---|---|
| `agent/wwplayer/run.go` | 1988 | +188 |
| `agent/wwplayer/agent.go` | 1971 | +171 |
| `game/werewolf/agent_runner.go` | 1948 | +148 |
| `game/werewolf/room_agent.go` | 1903 | +103 |
| `game/werewolf/room.go` | 1862 | +62 |

且 `handleEvent` 单函数 1120 行、`buildAgentContextLocked` 单函数 646 行。
逼近上限的还有 `view.go` 1592 / `anthropic.go` 1576 / `tools.go` 1504。

### P1-6 常量注释与实现大面积不符

| 项 | 实际 | 注释说 |
|---|---|---|
| `failCooldownWindow` | 90s | 30s / 60s |
| `Agent.Limiter` | 30s | 45s（6 处） |
| `WhisperLimiter` | 60s，**且只 Mark 不 Allow ⇒ 完全不限流** | 90s |
| watchdog deadline | 240s/360s | 90s / 365s / 420s / 630s（**五种说法**） |
| `maxRetries` | 7 | 5 |

---

## 4. 优化方案（本次实施 6 项 U1–U6）

按「收益/成本比」排序，全部为**低风险、可独立验证**的改动。

### U1 ★ 夜间私有信息块（修 P0-1 / P0-2）

> 对应 TencentDB 经验：**「分层记忆要有分层的注入策略」** —— 私有信息是最高价值、
> 最稳定的一层，必须直注。

**改动**：

1. `wwtypes/context.go` 新增 3 字段：

```go
MySeerCheckFaction string // "wolf"/"good"/""，预言家上一晚查验结果阵营
MySeerCheckHistory []SeerCheckRecord // 全部查验历史（座位+阵营+轮次）
WitchWolfTargetKnown bool // 女巫是否已知今晚狼刀目标
```

2. `room_agent.go` 填充时补算 `FactionOf(gs.Roles[t])`
3. `prompt.go` 新增 `NightPrivateInfoBlock(ctx)`，**放在 user prompt 最前面**
   （最高注意力位），渲染：
   - 预言家：完整查验历史表（轮次 / 座位 / 金水 or 查杀）
   - 女巫：今晚狼刀目标 + 药剂剩余
   - 守卫：上晚守护目标（盲守语义，不给狼刀）
   - 猎魔人 / 骑士：可用状态

**不变式**：守卫的 `WolfTarget` 恒为 -1（§134 盲守语义不能破）。

### U2 Prompt Token 预算与块优先级（修 P1-1）

> 对应 TencentDB 经验：**「压缩必须分级，且每级都有明确的保护区」**。

新增 `agent/wwplayer/prompt_budget.go`：

```go
// BlockPriority 决定预算不足时的牺牲顺序（数值越小越不可牺牲）。
const (
    PriorityCritical = 0   // 夜间私有信息、身份、当前阶段指令 —— 永不牺牲
    PriorityHigh     = 100 // 存活列表、投票状态、发言历史
    PriorityMedium   = 200 // 假说表、知识摘要、影响力
    PriorityLow      = 300 // 流言、画像、一致性校验、道具反馈
)

type PromptBlock struct {
    Name     string
    Priority int
    Text     string
}

// AssembleWithBudget 按优先级拼装，超预算时从 PriorityLow 开始整块丢弃，
// 并在末尾追加一行 [已省略 N 个低优先级信息块] 保证可观测（对照 TencentDB
// 的教训：降级必须留下可观测标记，不能静默）。
func AssembleWithBudget(blocks []PromptBlock, maxRunes int) (string, []string)
```

**关键设计（学自 TencentDB）**：
- 整块丢弃而非截断 —— 半截的假说表比没有假说表更糟
- **丢弃必须留痕** —— 直接对应 TencentDB「L1 失败与无可抽取不可区分」的反面教材

### U3 Prompt Cache 启用（修 P1-2）

> 对应 TencentDB 经验：**「Prompt cache 应该是 context 架构的第一约束」**。

`BuildSystemPrompt` 返回的最后一个 SystemBlock 打上：

```go
CacheControl: map[string]string{"type": "ephemeral"}
```

**前提条件（必须同时满足，否则 cache 永远 miss）**：
system prompt 必须逐字节稳定。当前 `BuildSystemPrompt` 的入参
`selfPortrait` / `personality` / `difficultyDirective` 每局固定，满足条件。

**只打一个 breakpoint**（学自 TencentDB 的克制：多余 breakpoint 是要计费的）。

### U4 记忆分段检索注入（修 P1-3）

> 对应 TencentDB 经验：**「记忆按角色隔离」+「注入 ≠ 全量」**。

不引入 embedding（13 人局场景下过重），改为**结构化分段 + 按需选取**：

1. `memory_iterate.go` 的 4 段扩展为**按角色分段**：
   `## 我的失误与教训` 下增设 `### 作为预言家` / `### 作为狼人` / `### 作为女巫` 等子段
2. 新增 `SelectMemoryForRole(md string, role string, maxRunes int) string`：
   只注入「通用段 + 当前角色段」，其余角色段跳过
3. 接线 `difficulty.MemoryInjectRunes`（修死配置）

**预期收益**：4000 → ~1800 runes，且相关性显著提升。

### U5 三项一行修复（修 P0-3 / P0-6 / P0-7）

- **P0-3**：`run.go` 熔断分类补 `isBreakerErr(err) → transient = true`
- **P0-6**：`run.go:851/869/908` 的 `defer` 移出循环体，改循环末显式释放
- **P0-7**：`cfgWerewolfMinSpeaks` 与 `SameSeatSpeakCooldownSec` 自洽性校验，
  日志 warn 并自动取 `max(1, 60/cooldown)` 作为有效下限

### U6 ★ CI lint：根治「声明了却从不接线」（修 P1-4）

> 这是本方案**唯一的机制性改动**，也是最重要的一项。

CLAUDE.md 已四次记录该教训并写入自检条款，仍复发 12 处 ——
说明**注释里的自检条款不会被执行，必须转化为测试断言**
（这正是 §20260811-08 教训 (2) 说过、但它自己也没做到的事）。

新增 `agent/wwplayer/wiring_lint_test.go`：

```go
// TestWiring_AllBlockFuncsHaveProductionCaller 断言每个 XxxBlock() 函数
// 至少有一处非测试生产调用点。新增块函数忘记接线时立即失败。
// TestWiring_AllGameContextFieldsAreRead 断言 GameContext 每个字段
// 在 agent/ 下至少有一处读取点（白名单排除内部控制字段）。
// TestWiring_SkipActionsHaveDispatchCase 断言 SkipPhaseAction 返回的
// 每个动作名在 dispatchToolInner 有对应 case。
```

三条断言分别覆盖本次发现的三类漏接：块函数、Context 字段、派发表 case。

---

## 5. 不实施的项（明确排除，附理由）

| 项 | 理由 |
|---|---|
| 引入 embedding / 向量库召回 | 13 人局记忆总量 <100KB，BM25 都过重，分段选取足够 |
| 拆分 5 个超限文件 | 纯搬移改动面 5000+ 行，与本次功能改动混在一起会让 review 失效；应独立提交 |
| 删除 100 处 PascalCase 分支 | 同上，独立重构提交 |
| 统一 4 份工具名白名单 | 同上 |
| L0/L1/L2/L3 四层记忆重构 | 与游戏场景不匹配 —— 狼人杀记忆是「跨局经验」不是「对话事实」 |
| MMD 任务图 | 狼人杀有明确 phase 状态机，不需要 LLM 维护任务拓扑 |

---

## 6. 验收标准

- [ ] `go build ./...` 通过
- [ ] `go test ./agent/... ./game/werewolf/... -count=1` 全绿
- [ ] `go vet ./agent/... ./game/werewolf/...` 无新增告警
- [ ] 新增回归测试覆盖 U1（私有信息渲染）、U2（预算降级留痕）、U6（三条 wiring 断言）
- [ ] U6 的 lint 测试**必须先在缺陷代码上验证它确实失败**（§20260811-08 教训 5）
- [ ] `tsc --noEmit` + `npm run build` 通过（若涉及前端）
- [ ] `./rebuild_restart_app.sh` 成功

---

## 7. 从 TencentDB-Agent-Memory 学到的、写进本仓库的教训

1. **私有信息是最高价值的注入内容，必须直注且放最高注意力位**
   —— 对照 TencentDB「L3 全文直注」的分层策略。
2. **降级必须留下可观测标记** —— TencentDB 的 L1 静默失败潜伏整个版本，
   本仓库的 U2 从设计之初就带 `[已省略 N 块]` 标记。
3. **Prompt cache 是架构约束不是事后优化** —— TencentDB 为它砍掉了「每轮自动召回」；
   本仓库至少要先把 `cache_control` 打上。
4. **注释里的自检条款必须转化为测试断言** —— 四次教训 + 12 处复发是最硬的证据。
5. **fail-fast 与 fail-soft 按「失败后果」选，不按统一风格选**
   —— breaker 应 transient（后果是误杀 bot），400 超限应 fail-fast（后果是无限重试）。
6. **「声明了却从不实现」的枚举/字段是最贵的技术债**
   —— TencentDB 的 `mode:"hybrid"` 与本仓库的 `MemoryInjectRunes` 是同一种病。
