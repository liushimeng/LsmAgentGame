# 狼人杀 Agent 根据 Hermes 的优化和解决方案（§20260813-04）

> **日期**：2026-08-13
> **依据文档**：[`docs/其他Agent代码分析/Hermes_Agent_源码分析.md`](../其他Agent代码分析/Hermes_Agent_源码分析.md)
> **审计范围**：`ServerGo/agent/`（4 子包 37+ 文件）、`ServerGo/game/werewolf/` Agent 接入层
> **对标对象**：Nous Research `hermes-agent`（持久记忆 + 自我进化自主智能体）

---

## 0. 摘要

本次审计以 Hermes Agent 的 10 项设计模式为标尺，对照狼人杀 Agent 现状，
发现 **3 项 P0 接线缺陷** + **4 项 P1 架构短板**。

**P0 的共同模式是 §130「声明了却从不接线」的第六次复发** ——
本项目已在 §130 / §134 / §135 / §20260811-08 / §20260812-04 记录五次，
并在 §20260812-04 U6 写了 `wiring_lint_test.go`，
但那 6 条断言**不覆盖「Agent 私有字段声明了但无生产 setter」**这一最高频模式。

Hermes 用 `ContextEngine` 的 `@abstractmethod` 在**语言层面**杜绝了这类缺陷：
不实现就无法实例化。Go 没有 abstract，因此本方案的 **U5 是把 lint 补成
「私有字段必须有生产 setter」的通用断言** —— 这是唯一能阻止第七次复发的手段。

| 编号 | 项目 | 优先级 | 类型 | Hermes 对应模式 |
|---|---|---|---|---|
| U1 | `steeringQueue` 接线（实时事件注入） | **P0** | 接线 | — |
| U2 | `toolHooks` 接线（工具前后置钩子） | **P0** | 接线 | — |
| U3 | `difficulty.MaxToolUse` 接线（难度档位生效） | **P0** | 接线 | — |
| U4 | pre-flight token 预检 + 有效输入预算 | **P1** | 新增 | `_compute_threshold_tokens` |
| U5 | wiring lint 扩展「私有字段必须有 setter」 | **P1** | 机制化 | `@abstractmethod` |
| U6 | 工具结果独立剪枝（无 LLM 确定性） | **P1** | 新增 | `prune_tool_results_only` |
| U7 | 陈旧注释清理（`AgentThoughtPanel` 等） | **P2** | 清理 | — |

---

## 1. 现状与 Hermes 的差距矩阵

| 维度 | Hermes 做法 | 狼人杀 Agent 现状 | 差距 |
|---|---|---|---|
| **记忆持久化** | `MEMORY.md`(自我) + `USER.md`(用户) 双文件正交 | `MEMORY.md` 单文件 4 段 + 角色分子段（§20260812-04 U4）| ✅ 已够用（狼人杀无长期"用户"概念）|
| **记忆上限单位** | **字符数**（模型无关） | `MemoryMaxBytes=100KB` 字节 + `MemoryInjectMaxRunes=4000` rune | ✅ 注入侧已用 rune |
| **记忆更新与 prompt cache** | **冻结快照**：落盘即时 + prompt 整 session 稳定 | `MemoryMD` 每局开始读一次，局内不变 | ✅ 等价（对局粒度 = session 粒度）|
| **prompt cache** | provider 特定重装饰 | §20260812-04 U3 已打 1 个 ephemeral breakpoint | ✅ 已做 |
| **Context 组装抽象** | `ContextEngine` ABC + 配置驱动 | 91 处 `s +=`，尾部 13 块有预算 | ⚠️ 无抽象层（本次不动，见 §5）|
| **压缩触发阈值** | 减 `max_tokens` + 小窗口 85% 保护 | `getModelContextBudget` 8 键硬编码，**无 pre-flight** | ❌ **U4 修** |
| **工具结果剪枝** | `prune_tool_results_only` 独立无 LLM 触发 | 只有整体 `Prune`/`CompressAndPrune` | ❌ **U6 修** |
| **规划状态穿越压缩** | Todo 重新注入 + 稳定 header | 无等价机制（狼人杀无多步规划）| ✅ 不适用 |
| **实时事件注入** | — | `steeringQueue` **声明了但恒为 nil** | ❌ **U1 修** |
| **工具前后置钩子** | — | `toolHooks` **零读取零 setter** | ❌ **U2 修** |
| **难度档位工具上限** | — | `MaxToolUse` **4 赋值 0 生效** | ❌ **U3 修** |
| **接线保证** | `@abstractmethod` 语言层强制 | `wiring_lint_test.go` 6 条断言，不覆盖字段 setter | ❌ **U5 修** |
| **副作用不阻塞主流程** | 每轮失败上限 + `done:True` | 速率类失败不计 `cf`（§120）| ✅ 已做 |
| **降级留可观测标记** | `[memory provider context truncated]` | `[本轮因上下文预算省略 N 块]`（§20260812-04 U2）| ✅ 已做 |

---

## 2. P0 缺陷详述与修复方案

### U1 — `steeringQueue` 接线（实时事件注入通道）

#### 现状（已 grep 验证）

```
agent/wwplayer/agent.go:260        steeringQueue *SteeringQueue     ← 字段声明
agent/wwplayer/run.go:684-685      if a.steeringQueue != nil {...}  ← 唯一读取点
                                   零 setter → 恒为 nil → 该分支永不执行
agent/wwplayer/steering_queue.go   149 行完整实现，从未执行过
```

`run.go:682` 的注释写着「灵感来源: PI Agent 的 steeringQueue.drain() 机制」，
`steering_queue.go` 文件头详细描述了它解决的痛点：

> *"Agent 在 handleEvent 内循环执行 LLM 调用时，新到达的观众消息/道具命中/
> 阶段提示只能等下一次 handleEvent 入口才能感知。"*

这个痛点是**真实存在**的：慢模型单次 LLM 调用可达 1-3 分钟（§197），
期间到达的观众提问要等下一轮 wake 才被感知。

#### 修复

**(a) 补 setter**（`agent.go`）：

```go
// SetSteeringQueue 注入实时事件队列。room manager 在 StartAgentsLocked 时调用。
// 传 nil 可显式关闭（drain 逻辑跳过）。
func (a *Agent) SetSteeringQueue(q *SteeringQueue) {
    a.mu.Lock()
    defer a.mu.Unlock()
    a.steeringQueue = q
}

// SteeringQueue 返回实时事件队列（room manager 入队用）。可能为 nil。
func (a *Agent) SteeringQueue() *SteeringQueue {
    a.mu.RLock()
    defer a.mu.RUnlock()
    return a.steeringQueue
}
```

**(b) 生产注入点**（`game/werewolf/room_agent.go` 的 `StartAgentsLocked`）：

```go
ag.SetSteeringQueue(wwplayer.NewSteeringQueue(10))
```

**(c) 生产入队点** —— 三条已有的实时事件路径：

| 事件 | 现有位置 | 入队 Kind |
|---|---|---|
| 观众消息 | `wakeAllAgentsLocked` 前 | `SteerSpectatorInquiry` |
| 道具命中 | `enqueuePropHitLocked` | `SteerPropHit` |
| 私聊到达 | `WhisperFromBot` 接收侧 | `SteerWhisper` |

> **§92a 约束**：入队必须是**非阻塞**的（`Enqueue` 已满足），
> 且这三处都在持 `r.mu` 状态下调用，`SteeringQueue` 内部只用自己的
> `sync.Mutex` + channel，**不触碰 `r.mu`**，无锁序风险。

**(d) 生命周期**：`Shutdown` 时 `steeringQueue.Close()`。

> ⚠️ **Close 后再 Enqueue 会 panic**（向 closed channel 发送）。
> 因此 `Close` 必须先把字段置 nil 再 close，且入队方走
> `if q := ag.SteeringQueue(); q != nil` 守卫。

#### 验收

- 新增测试：入队 → `DrainAndFormat` 返回带【观众提问】前缀的文本
- 新增测试：nil queue 时 `handleEvent` 不 panic（回归保护）
- wiring lint：`steeringQueue` 有生产 setter 调用点

---

### U2 — `toolHooks` 接线（工具前后置钩子）

#### 现状（已 grep 验证）

```
agent/wwplayer/agent.go:265     toolHooks *ToolHooks   ← 唯一出现点
                                零读取、零 setter、完全孤立
agent/wwplayer/tools.go:846     return DispatchToolWithHooks(name, input, runner, nil)
                                                                            ↑ 永远传 nil
agent/wwplayer/tool_hooks.go    完整实现（含 hookLogToolCall/hookLogToolResult）
```

#### 决策：接线而非删除

`DispatchToolWithHooks` 已经是 `DispatchTool` 的实现体，
`ToolHooks` 提供的 before-hook 校验能力对**道具系统**有直接价值
（§20260807-04 的 6 类注入攻击需要工具级配额检查）。

#### 修复

**(a) 补 setter**（`agent.go`），同 U1 形状。

**(b) 让 `DispatchTool` 走 Agent 持有的 hooks** ——
这里有个**签名障碍**：`DispatchTool` 是**包级函数**（`tools.go:845`），
不是 `Agent` 方法，拿不到 `a.toolHooks`。

**方案**：在 `run.go` 的 tool dispatch 处（:1443 附近）改调
`DispatchToolWithHooks(name, input, runner, a.ToolHooks())`，
保留包级 `DispatchTool(..., nil)` 供测试桩使用。

> **为什么不改 `DispatchTool` 签名**：
> §130 防御原则 —— `ToolRunner` 被大量测试桩实现，
> 改包级函数签名会让所有调用点编译失败。
> 只在**生产调用点**（run.go）传入 hooks，测试路径不变。

**(c) 生产注入点**：`StartAgentsLocked` 中 `ag.SetToolHooks(wwplayer.NewToolHooks())`。

#### 验收

- 测试：before-hook 返回 error 时工具不执行
- 测试：after-hook 的 error 被忽略（best-effort）
- 测试：nil hooks 时行为与旧路径完全一致

---

### U3 — `difficulty.MaxToolUse` 接线（难度档位生效）

#### 现状（已 grep 验证）

```
game/werewolf/difficulty.go:53/64/75/89   MaxToolUse: 3 / 6 / 8 / 0   ← easy/normal/hard/hell
agent/wwplayer/agent.go:826               MaxToolUse: 0,  ← 硬设 0
agent/wwplayer/agent.go:131-134           "§130 重构(2026-07-13):MaxToolUse 字段保留但不再使用。"
```

**难度档位对工具调用上限完全无效。**

这与 §20260812-04 U4 修的 `MemoryInjectRunes`（同为 4 赋值 0 读取）
是**同一模式，修了一个漏了另一个**。

#### 决策：接线到 `maxInnerRoundsForPhase`，而非复活旧全局上限

§130 废弃 `MaxToolUse` 的理由是正确的：**全局硬上限**会截断
正常的多轮 tool_use（如 `speak` 前先 `chat_recall`）。
真正生效的机制是 `maxInnerRoundsForPhase(phase)`（`run.go:43`）。

因此**不复活旧语义**，而是让难度档位**调制内层循环上限**：

```go
// maxInnerRoundsForPhase 返回该阶段的内层循环上限。
// §20260813-04 U3: 难度档位通过 difficultyRoundCap 收紧上限
// （0 = 不限，走 phase 默认值）。
func (a *Agent) maxInnerRoundsForPhase(phase string) int {
    base := phaseMaxInnerRounds[phase]
    if base <= 0 {
        base = defaultMaxInnerRounds
    }
    if cap := a.DifficultyRoundCap(); cap > 0 && cap < base {
        return cap
    }
    return base
}
```

**字段重命名**：`MaxToolUse` → `DifficultyRoundCap`（语义准确），
`difficulty.go` 的 4 处赋值同步改名，并更新注释说明它现在调制内层循环。

> **为什么保留 hell=0**：地狱档位不设上限，让强模型充分发挥。
> easy=3 让弱模型/新手房间的 bot 更快收敛（少 tool_use = 更快出手）。

#### 验收

- 测试：`DifficultyRoundCap=3` 时 `maxInnerRoundsForPhase("speak")` 返回 3（而非默认 5）
- 测试：`DifficultyRoundCap=0` 时返回 phase 默认值
- 测试：`DifficultyRoundCap=8 > base=3` 时返回 3（cap 只收紧不放宽）
- wiring lint：`DifficultyRoundCap` 有读取点

---

## 3. P1 架构短板

### U4 — pre-flight token 预检 + 有效输入预算

#### 现状

`prompt_budget.go:19-20` 自述：**「没有任何 pre-flight 检查」**。
唯一的自适应路径是等上游返回 400 `exceed max message tokens`
之后才 `PruneByBytesAggressive`（`run.go` 的 `isContextExceededError` 分支）。

对慢模型（首字节 1-3 min），**一次 400 = 浪费数分钟**。

`getModelContextBudget`（`agent.go:1999-2026`）是 8 键硬编码 map，
第 9 个模型静默 fallback 到 `DefaultMaxPromptBytes`(200KB)。

#### Hermes 的三个洞察（`_compute_threshold_tokens`）

1. **必须减去 `max_tokens`**（issue #43547）——
   provider 从同一窗口预留输出空间，可用输入预算 = `窗口 - max_tokens`
2. **地板值在小窗口退化**（issue #14690）——
   地板 ≥ 有效窗口时阈值等于整个窗口，压缩永不触发；改在 85% 触发
3. **`max_tokens=None` 保守假设无预留**

#### 修复

新增 `agent/wwplayer/preflight.go`：

```go
// preflightBudgetBytes 返回本次请求的有效输入字节预算。
// §20260813-04 U4，借鉴 Hermes _compute_threshold_tokens：
//   ① 从模型窗口减去 max_tokens 输出预留（Hermes #43547）
//   ② 预留不合理（≥窗口）时退化为窗口的 85%（Hermes #14690）
func preflightBudgetBytes(modelKey string, maxTokens int) int {
    window := getModelContextBudget(modelKey)
    // maxTokens 是 token 数，按中文 UTF-8 保守折算 3 bytes/token
    reserve := maxTokens * 3
    effective := window - reserve
    if effective <= 0 {
        // 预留吃掉整个窗口 → 退化为 85%（Hermes #14690 同源）
        effective = window * preflightDegradeRatioPct / 100
    }
    if effective < preflightMinBudgetBytes {
        effective = preflightMinBudgetBytes
    }
    return effective
}

// shouldPreflightPrune 在发 HTTP 前判断是否需要主动裁剪。
// 返回 (需要裁剪, 目标字节数, 原因)。
func shouldPreflightPrune(payloadBytes, budget int) (bool, int, string) { ... }
```

**接入点**：`run.go` 内层循环，`Snapshot + Sanitize` 之后、
`AcquireLLMSlot` 之前 —— 即**发 HTTP 前**。

超预算时走已有的 `PruneByBytes(target)`，并**留可观测标记**
（对齐 §20260812-04 教训 4）：

```go
a.SetLastPreflightNote(fmt.Sprintf(
    "[pre-flight 裁剪] payload %dKB > 预算 %dKB(窗口 %dKB - 输出预留 %dKB)，已裁剪至 %dKB",
    ...))
```

> **护栏宁松勿紧**（§20260812-04 教训 5）：
> `preflightDegradeRatioPct=85`，且只在**确实超预算**时裁剪。
> 正常对局 payload 远低于预算，该路径不触发。

#### 验收

- 测试：`maxTokens` 预留正确扣减
- 测试：预留 ≥ 窗口时退化为 85%
- 测试：未知模型 fallback 到默认预算
- 测试：payload 在预算内时不裁剪（不误杀）

---

### U5 — ★ wiring lint 扩展「私有字段必须有生产 setter」

#### 这是本方案**最重要**的一项

§130「声明了却从不接线」已复发**六次**：

| 次数 | 编号 | 缺陷 |
|---|---|---|
| 1 | §130 | `AgentJudge.Provider` 字段声明但从不注入 |
| 2 | §132 | `WolfTeammateHint` 定义了却从未被调用 |
| 3 | §134 | `RoleGuard` 在卡池但引擎/工具/UI 三层缺失 |
| 4 | §135 | `HunterPendingFrom=="wolf"` 分支无生产置位点 |
| 5 | §20260811-08 | `PerSeatPOV` 7 字段硬编码为空 + 结算奖励只接 1/4 路径 |
| 6 | §20260812-04 | `SystemBlock.CacheControl` 从未赋值 + `MemoryInjectRunes` 4 赋值 0 读取 |
| **7** | **本次** | **`steeringQueue` / `toolHooks` / `MaxToolUse`** |

§20260812-04 U6 写了 `wiring_lint_test.go`（6 条断言），
但它们是**逐个缺陷的专项断言**（块函数有调用点 / SkipPhaseAction 有 case /
夜间私有字段有读取点），**不覆盖通用模式**。

本次三处缺陷的**共同形状**是：
> `Agent` struct 声明了私有字段 → 有读取点（或连读取点都没有）→ **但没有生产 setter**

#### 修复：通用 AST lint

新增 `agent/wwplayer/wiring_lint_field_test.go`：

```go
// TestWiringLint_PrivateFieldsHaveProductionSetter 断言 Agent struct 的每个
// 引用类型私有字段（指针/接口/map/slice/chan）都有生产 setter 或构造注入。
//
// §20260813-04 U5 —— §130「声明了却从不接线」第七次复发的机制化防御。
// Hermes 用 ContextEngine 的 @abstractmethod 在语言层杜绝此类缺陷；
// Go 无 abstract，只能靠 lint。
//
// 判据：字段 F 必须满足其一
//   ① 存在 func (a *Agent) SetF(...) 或 WithF(...)
//   ② 在 NewWithRoom / New 构造函数内被赋值
//   ③ 在 exemptFields 白名单内（附理由）
func TestWiringLint_PrivateFieldsHaveProductionSetter(t *testing.T) {
    // go/parser 解析 agent.go，提取 Agent struct 字段
    // 对每个引用类型私有字段，grep 同包内的 setter/构造赋值
}
```

**白名单**（必须附理由）：

```go
var exemptFields = map[string]string{
    "mu":          "sync 原语，零值可用",
    "events":      "构造函数内 make",
    "memory":      "构造函数内 NewMemory",
    // ... 每条都要写清为什么不需要 setter
}
```

> **能咬到作者本人的 lint 才是有效的 lint**（§20260812-04 教训 3）。
> 本 lint 在编写时会立刻报出 U1/U2 两处缺陷 —— 这正是它有效的证明。
> 修复 U1/U2 后 lint 转绿；未来任何人新增字段忘了 setter，CI 立刻失败。

#### 验收

- lint 在**未修复** U1/U2 时**失败**（先验证它会咬）
- lint 在修复后**通过**
- 白名单每条都有非空理由字符串

---

### U6 — 工具结果独立剪枝（无 LLM 确定性）

#### Hermes 的做法

`ContextEngine.prune_tool_results_only()`：

> *"Deterministically trim old tool-result payloads without an LLM call.
> Runs on a low, cost-oriented trigger independent of `should_compress`
> so large-window engines can reclaim re-sent tool output long before
> full compaction would fire."*

#### 狼人杀 Agent 现状

只有整体压缩（`Prune` / `CompressHistoryLocked` / `CompressAndPrune`），
且局内 LLM 语义压缩**每局最多一次**（`run_compact.go:67`）。

工具结果（`tool_result` 块）是 payload 的大头 ——
`chat_recall` / `prop_inspect` / `prop_history` 的返回值都不小，
且**每轮都被重发**。

#### 修复

`memory.go` 新增：

```go
// PruneToolResultsOnly 确定性裁剪旧的 tool_result 载荷，不调 LLM。
// §20260813-04 U6，借鉴 Hermes prune_tool_results_only：
// 独立于整体压缩的低成本触发器，在完整压缩触发之前很久就回收重发的工具输出。
//
// 策略：保留最近 keepRecent 轮的 tool_result 原文，
// 更早的截断到 truncTo 字节并附 [已裁剪] 标记。
// 返回被裁剪的块数。
func (m *Memory) PruneToolResultsOnly(keepRecent, truncTo int) int
```

**触发点**：`run.go` 每轮 tool dispatch 完成后（对齐 Hermes 的
「post-tool-call prune path」），条件是 payload > 预算的 60%。

> **不能破坏 tool_use/tool_result 配对**（§82b）——
> 只截断 `tool_result` 的 **content 文本**，
> 绝不删除整个块，`tool_use_id` 保持不变。

#### 验收

- 测试：`tool_use`/`tool_result` 配对完整性不变（Anthropic 400 回归保护）
- 测试：最近 N 轮不被裁剪
- 测试：裁剪留 `[已裁剪]` 可观测标记
- 测试：无 tool_result 时返回 0（no-op 安全）

---

### U7 — 陈旧注释清理

`AgentThoughtPanel.tsx` **在前端已不存在**（§128 删除），
但后端 6+ 处注释仍引用：

```
agent/wwplayer/agent.go:231, 234
agent/wwplayer/tools.go:78
agent/wwplayer/memory.go:333, 356
agent/wwplayer/run.go:1312
```

按 §20260812-04 教训 2「**写反的注释是缺陷的最佳伪装** ——
注释与实参不符应当像编译错误一样对待」，这类漂移应清理。

**修复**：改为指向真实渲染方 `HistoryDrawer`（🤖独白 sub-tab）。

---

## 4. 实施顺序与验收矩阵

```
Phase 1（P0 接线，可独立验收）
  U5 lint 先写 ──► 验证它咬到 U1/U2 ──► U1 ──► U2 ──► U3 ──► lint 转绿
  ↑ 这个顺序是刻意的：先让 lint 失败，证明它有效（§20260812-04 教训 3）

Phase 2（P1 能力，独立于 Phase 1）
  U4 pre-flight ──► U6 工具结果剪枝

Phase 3（清理）
  U7 注释
```

| 编号 | 新增文件 | 改动文件 | 新增测试 |
|---|---|---|---|
| U1 | — | `agent.go` / `room_agent.go` | 3 |
| U2 | — | `agent.go` / `run.go` / `room_agent.go` | 3 |
| U3 | — | `agent.go` / `run.go` / `difficulty.go` | 3 |
| U4 | `preflight.go` | `run.go` | 4 |
| U5 | `wiring_lint_field_test.go` | — | 1（含白名单）|
| U6 | — | `memory.go` / `run.go` | 4 |
| U7 | — | 6 处注释 | 0 |

**全局验收**：
- `go build ./...` 通过
- `go test ./agent/... ./game/werewolf/... ./llm/...` 全绿
- `./rebuild_restart_app.sh` 编译并重启成功

---

## 5. 刻意不做的事（范围控制）

以下短板**本次不动**，理由如下：

| 短板 | 为什么不动 |
|---|---|
| **`BuildUserPrompt` 91 处 `s +=` → 声明式 block 注册表** | 这是**架构级重写**，涉及 41 个块的优先级重排，回归面覆盖全部 7 个 bot 的 prompt 形状。应作为独立提交，且需要「构建产物 prompt 字节一致」级别的验证（对齐 §136 教训 5 的 CSS 拆分方法论）|
| **`agent.go`/`run.go` 拆分至 §4 合规（现 2026/2027 行）** | 纯代码搬移，但与本次 U1-U3 的改动在同一批文件上，同时做会让 diff 无法审查。应在本次落地后单独提交 |
| **真实 tokenizer 替代字节近似** | 需引入 tokenizer 依赖（各家模型不同），且 U4 的保守折算（3 bytes/token）已能解决「等 400 才压缩」的核心问题。真 tokenizer 是精度优化，不是缺陷修复 |
| **双轨工具装配收口（switch 28 + Registry 7）** | 需逐个迁移 28 个工具，每个都要验证 schema 字节一致。独立提交 |
| **`GameContext` 90 字段瘦身** | 涉及 wire 契约，需前后端同步。独立提交 |

> **范围控制的理由**：本次的 P0 是**功能性缺陷**（3 项能力完全失效），
> P1 是**成本/稳定性改进**。上述 5 项是**结构性债务** ——
> 它们不影响功能正确性，但重写风险高。混在一个提交里会让
> 「哪个改动引入了回归」无法定位。

---

## 6. 教训预登记（落地后写入 CLAUDE.md §20260813-04）

1. **§130 第七次复发，且这次是「完整实现 + 完整注释 + 零接线」** ——
   `steering_queue.go`(149 行) 与 `tool_hooks.go` 都是**可用的完整实现**，
   文件头详细描述了设计动机，`run.go:682` 甚至写了「灵感来源」，
   唯独没有 setter。**注释的详尽程度与接线的完整性无关** ——
   写得越像已完成，越容易被当作已完成。

2. **专项 lint 追不上通用模式** ——
   §20260812-04 U6 的 6 条断言是针对当时 6 个具体缺陷的，
   本次 3 处缺陷一条都没咬到。**lint 必须断言「模式」而非「实例」**。

3. **同一模式的两个实例，修一个漏一个** ——
   `MemoryInjectRunes`（§20260812-04 U4 已修）与 `MaxToolUse`（本次）
   都是「difficulty.go 4 赋值 + agent.go 0 读取」。
   **修复一个缺陷后必须 grep 同 struct 的其他字段是否同病**。

4. **Hermes 的 `@abstractmethod` 是 Go 缺失的能力** ——
   Python ABC 让「声明了不实现」无法实例化。
   Go 只有 interface（不强制字段）+ 编译期未使用变量检查（不覆盖 struct 字段）。
   **语言不保证的，必须靠 CI 保证**。

5. **pre-flight 与 post-error 是两个独立机制，不能互相替代** ——
   现状只有 post-error（等 400 后激进压缩）。
   Hermes 的 `_compute_threshold_tokens` 减 `max_tokens` 这一条，
   本项目完全没有对应逻辑：**provider 从同一窗口预留输出空间**
   这个事实从未进入预算计算。

---

## 7. 参考

| 主题 | 位置 |
|---|---|
| Hermes 源码分析 | [`docs/其他Agent代码分析/Hermes_Agent_源码分析.md`](../其他Agent代码分析/Hermes_Agent_源码分析.md) |
| 冻结快照模式 | `hermes-agent/tools/memory_tool.py:11-14, 148-157` |
| Context 引擎 ABC | `hermes-agent/agent/context_engine.py` |
| 阈值计算（4 个 issue） | `hermes-agent/agent/context_compressor.py:2472-2512` |
| 工具结果独立剪枝 | `hermes-agent/agent/context_engine.py:189-212` |
| Todo 穿越压缩 | `hermes-agent/tools/todo_tool.py:24-43` |
| 本项目 §130 首次记录 | `CLAUDE.md` lessons 表 §130 |
| 本项目 wiring lint | `ServerGo/agent/wwplayer/wiring_lint_test.go` |
