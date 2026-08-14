# 狼人杀 Agent —— 根据 DeepSeek Harness 借鉴的优化与解决方案（§20260813-05）

| 项 | 值 |
|---|---|
| 借鉴对象 | [`/usr/local/LsmGitOpenSource/deepseek-harness`](https://github.com/deepseek-ai/deepseek-harness)（dsh v0.1.0-rc.5, 47f9438） |
| 借鉴分析文档 | [`deepseek-harness_记忆管理分析.md`](../其他Agent代码分析/deepseek-harness_记忆管理分析.md) 972 行 |
|  | [`deepseek-harness_Context管理分析.md`](../其他Agent代码分析/deepseek-harness_Context管理分析.md) 1598 行 |
|  | [`deepseek-harness_意图识别与任务分解分析.md`](../其他Agent代码分析/deepseek-harness_意图识别与任务分解分析.md) 2117 行 |
| 编写日期 | 2026-08-13 |
| 落地节奏 | 本次落地 3 项（U2 / U3 / U5），其余 4 项为后续 commit |

---

## 0. 摘要（TL;DR）

借鉴 DeepSeek Harness 的"事件溯源 + 投影 + 不变量 companion"哲学，结合狼人杀 Agent 现状（§15 §118 §128 §130 §134 §20260813-04 §20260814-02 等 200+ 条 lesson 沉淀），提出 **7 项升级（U1–U7）**：

| # | 名称 | 借鉴点 | 优先级 | 本次 |
|---|---|---|---|---|
| U1 | **ModelContext 持久化** | dsh `LlmModelContext` + `usage-projection` 三轴计数（input/output/cache）| **P0** | 留待 commit 2（与 OpenCode U1 cache breakpoint 共改面） |
| U2 | **Invariant Companion 自检（不变量持续守护）** | dsh `invariant.ts` 持续校验模式 + §134/§135/§130/§20260811-08 wiring lint | **P0** | ✅ 本次落地 |
| U3 | **Pre-step 主动压缩 + 双触发** | dsh `compaction-basic/src/index.ts:147-223` pre-step 80% + overflow 兜底 | **P0** | ✅ 本次落地 |
| U4 | **AbortSignal 三源 fuse + cancelCause 包装** | dsh `agent-loop/src/index.ts:479-487` + §92a §20260814-02 | P1 | 留待 commit 3 |
| U5 | **System Prompt 字节稳定 → Provider Cache Hit 提升** | dsh "Provider cache 不下断点，依赖 request bytes 稳定" + §14.1 `CacheControl` "声明了却从不接线" + §20260814-02 U1 | P0 | ✅ 本次落地 |
| U6 | **Tool-result 剪枝永不静默删** | dsh `compaction-tool-result-pruner` `PRUNE_MARKER` + §20260813-04 U6 + §137 道具 propInjectText 修剪 | P1 | 留待 commit 3 |
| U7 | **LLMProvider Refactor（attribution + 超时清理）** | dsh `attribution.ts` `using` idle watchdog + `api-key.ts` 归一化 | P2 | 留待后续 |

> 本次 commit 落地 **U2 / U3 / U5** 三项，其余 4 项各自独立 commit。
>
> **避重复声明**：§20260814-02 OpenCode 借鉴 6 项（U1 cache breakpoint / U2 wolf 权限派生 / U3 LLMSlot 背压 / U4 transcript 6 桶 / U5 AgentRunTrace / U6 wiring lint）尚未全部落地，本文件 U1/U5/U7 与之部分重叠但**作用域不同**（dsh 启发 vs opencode 启发），落地时严格区分：
>
> | 本文件项 | §20260814-02 项 | 区分 |
> |---|---|---|
> | U5 System Prompt 字节稳定 | §20260814-02 U1 cache breakpoint 多断点 | U5 走"不下断点依赖字节稳定"路线，§20260814-02 U1 走"显式 4 断点"路线；两条路正交，二者选其一即可，本次 U5 实施 |
> | U2 Invariant Companion | §20260814-02 U6 AST 全字段 lint | U2 是 runtime 持续校验（每次发请求前），U6 是 CI 时静态扫描（编译期）；两者互补 |
> | U7 LLMProvider Refactor | 无重叠 | dsh 启发独有 |

---

## 1. 背景：DSH 哲学 vs 狼人杀现状

### 1.1 DSH 三大哲学

1. **日志是 single source of truth** —— 任何模型可见输入必须从 session log 重放（`AGENTS.md:107`）。这是 event-sourcing + projection 范式，不是单纯的"持久化"。
2. **Provider 是黑盒，wire 协议由不变量守护** —— 7 套 LLM 适配器各有 quirks，但都被收敛到 `StreamChunk` 6 类 + invariant 持续校验（`llm/invariant.ts`）。
3. **Invariant companion 持续护栏** —— 每个关键包提供 `invariant.ts` 声明"owned relationships"（事件 + 数据），违规 fail-loud；比事后单元测试早一步。

### 1.2 狼人杀 Agent 现状对照

| 维度 | DSH | LsmAgentGame 狼人杀 | 差距 |
|---|---|---|---|
| **记忆来源** | session event log + projection | `agent/wwplayer/memory.go::Memory` 内存 `[]llm.Message`，跨局才入 DB | DSH 更具可重放性，但狼人杀"短会话高并发"场景内存足够 |
| **Provider 抽象** | `LlmAdapter` 抽象 + `BlockAssembler` 收敛 6 类流帧 + invariant 守护 wire | `llm.LLMProvider` 接口 + `anthropic.Provider` + `openai.Provider`；**无运行时不变量校验** | DSH 流协议不变量是 P0 防御，狼人杀仅有结构体字段约束 |
| **Context 组装** | dynamic context 走 user message（`runtime-context.ts:64-74`） + system section 各司其职 | `GameContext`（120+ 字段扁平） + 字符串拼接，无 dynamic / static 分离 | DSH 显式建模分离，狼人杀耦合度更高 |
| **Token 计量** | `TokenMeter` 三轴（input / output / cacheRead / cacheWrite） + 投影 | `approxPayloadBytes` 单字节数（§20260813-04 教训 6 已修但仍是单一数） | DSH 多维账本 vs 狼人杀单维估算 |
| **压缩** | pre-step 主动 + overflow 兜底双触发 | post-error 400 兜底（§20260813-04 U4 已加 preflight，**但仍是单触发点**） | DSH 双触发更鲁棒 |
| **子代理隔离** | session / scope / permission / context 四维 | 房间级 llmSema + per-bot memory + faction 隔离（§133 wolfpack） | 狼人杀"扁平"够用，子代理场景不重 |
| **Invariant 自检** | runtime invariant companion 持续 | wiring lint（手工 7 条 + AST `cmd/wiringlint`）+ 单元测试 | 狼人杀静态扫描强但 runtime 守护弱 |
| **Abort 协议** | 三源 fuse + reason 包装 Error | `context.CancelFunc` + watchdog | DSH 命名更规范 |
| **Cache hit rate** | DeepSeek 自动 server-side cache，靠 request bytes 稳定 | Anthropic `CacheControl` 字段**声明了却从未赋值**（§14.1） | 狼人杀该字段从未产生价值 |

### 1.3 最值得借鉴的 5 条（基于三份分析综合）

1. **Invariant companion 持续守护**（DSH `invariant.ts`）—— 狼人杀"声明了却从不接线"§130 已反复复发 7 次，目前只有 CI 时 lint；runtime 持续校验才能根治。**U2 落地**。
2. **Pre-step 主动压缩**（DSH `compaction-basic/src/index.ts:147-223`）—— 狼人杀只 post-error 兜底，慢模型数分钟白等（§197）；加 pre-step 80% 主动压缩触发。**U3 落地**。
3. **Provider cache 依赖字节稳定而非显式断点**（DSH `agent.ts:465-470` + `llm-deepseek/translate.ts:53-62`）—— 狼人杀 `CacheControl` 字段 200+ lesson 反复声明"可挂"但**从未赋任何值**；改为"deep freeze SystemBlock + 工具 schema + identity 提前入变量"确保跨轮字节稳定。**U5 落地**。
4. **Token 计量三轴化**（DSH `usage-projection.ts:198-205`）—— 狼人杀 `approxPayloadBytes` 单数无三轴账本，难诊断 cache hit 与压缩效果。**U1 留待**。
5. **Abort 三源 fuse + reason 包装**（DSH `agent-loop/src/index.ts:479-487`）—— 狼人杀 §92a §20260814-02 反复爆雷持锁路径与 cancel 时序。**U4 留待**。

---

## 2. U2：Invariant Companion 自检（不变量持续守护）

### 2.1 思路

DSH 每个关键包提供 `invariant.ts`：

```ts
// dsh packages/llm/llm/src/invariant.ts (节选)
export const name = 'llm-invariant'
export const inject = ['llm']  // 服务依赖
export function apply(ctx: Context) {
  // 注册持续校验钩子,在每次流帧/请求边界调用
  ctx.on('llm/stream', checkStreamFrames)
  ctx.on('agent/request', checkReconstructability)
}
```

每个 invariant 声明 `inject: [...]` 服务依赖，由 `dsh-invariants` 服务在生产中保留 + 持续校验。**"声明了但未接线"的缺陷无法躲过 invariant companion 校验**。

### 2.2 狼人杀现状

- **已有**：
  - 手工 wiring lint（`agent/wwplayer/wiring_lint*.go`）7 条断言
  - AST 自动 lint（`ServerGo/cmd/wiringlint`）§20260814-02 U6
- **缺失**：
  - **runtime 持续校验**：lint 只在 CI 跑一次；线上 `GameContext` 字段被错误赋值时无即时失败
  - **跨端契约**：前后端字段名拼错（§121 模型管理页崩溃）属于 wiring 错位但无 invariant
  - **消息配对保护**：`§82b` Anthropic 协议是 LLM-as-judge 最大长尾；`SanitizeMessagesForAnthropic` 在 lint 时不跑、运行时偶发跨 turn 配对错

### 2.3 实现

新增 `ServerGo/agent/wwtypes/invariant.go`（~200 行），按 dsh `invariant.ts` 模式提供三个 hook：

```go
// Package invariant — Agent 级 runtime invariant companion。
//
// 2026-08-13 §20260813-05 U2。借鉴 dsh invariant companion 模式
// (packages/llm/llm/src/invariant.ts:12 + packages/compaction/compaction-basic/src/invariant.ts)。
//
// 与 §20260814-02 U6 AST lint 互补：lint 是 CI 时静态扫描，
// invariant 是 runtime 持续校验 —— 每次发请求前 / 每次 LLM 响应后 /
// 每次消息配对时 fail-loud。
package wwtypes

// GameContextFieldSource 注册 GameContext 字段的"权威来源"。
// runtime 时填字段必须在该来源的 setter 路径上,否则 fail-loud。
type GameContextFieldSource struct {
    Field         string  // "MySeerCheck" / "WolfTarget" / ...
    Authority     string  // "engine.emitSeerCheck" / "witch.killPlayer" / ...
    Setter        func(gc *GameContext) // 唯一 setter(白名单)
}

// 13 类 GameContext 字段 × 来源映射(略)
var gameContextFieldSources = []GameContextFieldSource{ ... }

// CheckGameContextInvariant 在每次 buildAgentContextLocked 末尾调用。
// 失败 = Debug 日志 + 1 个原子计数器;Debug build panic + 报告路径。
func CheckGameContextInvariant(gc *GameContext) error { ... }

// CheckMessagePairingInvariant 在每次 LLM 请求前调用。
// 校验 tool_use ↔ tool_result 配对、user/assistant 严格交替、Sanitize 后 messages 合法。
func CheckMessagePairingInvariant(msgs []llm.Message) error { ... }

// CheckRequestReconstructabilityInvariant 在 anthropic.Provider.Chat 前调用。
// 校验 req.Messages == Memory.Snapshot() 后再 SanitizeMessagesForAnthropic 的产物。
// 任何 listener 想改 messages 必须改事件 (DSH §8.3 不变量)。
func CheckRequestReconstructabilityInvariant(req llm.LLMRequest, snap Memory) error { ... }
```

接线点：

1. `game/werewolf/agent_runner.go::buildAgentContextLocked` 末尾调 `CheckGameContextInvariant(gc)`（失败 Debug + 计数器）。
2. `agent/wwplayer/run.go::runLoop` 发请求前调 `CheckRequestReconstructabilityInvariant(req, a.Memory)`。
3. `agent/wwplayer/memory.go::SanitizeMessagesForAnthropic` 后调 `CheckMessagePairingInvariant(msgs)`（替代现有的人工 Sscanf 检查）。
4. **wiring lint 静态断言**（`agent/wwplayer/wiring_lint_field_test.go`）+ **runtime invariant 钩子**（新文件）双保险。

### 2.4 不变量清单（首批 12 条）

| # | 不变式 | 出处 | 校验点 |
|---|---|---|---|
| I1 | `MySeerCheck != -1` 必须有对应 `MySeerCheckFaction != ""` | §20260812-04 P0-1 | CheckGameContext |
| I2 | `WolfTarget != -1` 必须有对应 `Witch*Used` 状态一致 | §20260812-04 P0-1 | CheckGameContext |
| I3 | `WolfTeammateSeat >= 0` 必须满足 `Role == "werewolf"` | §133 | CheckGameContext |
| I4 | `WolfPackSnapshot` 仅在 `Faction == "wolf"` 时非空 | §133 协议层隔离 | CheckGameContext |
| I5 | `HumanDebuff` 仅在 `!IsBot` 时非空 | §20260807-04 | CheckGameContext |
| I6 | `MySeerCheckHistory` 长度 ≤ `Round + 1` | §134 守卫同步 | CheckGameContext |
| I7 | `req.Messages` 字节数 == `Memory.TotalPayloadBytes()` 估算 | §20260813-04 U6 教训 6 | CheckRequestReconstructability |
| I8 | `tool_use` ↔ `tool_result` 1:1 配对 | §82b | CheckMessagePairing |
| I9 | `role=user` 不可连续 2 条 | §14.1 | CheckMessagePairing |
| I10 | `tool_use.input == nil` 必须空对象 `{}` | §71a | CheckMessagePairing |
| I11 | `system[].CacheControl` 字段未赋值时整个 system 块字节稳定 | U5 | CheckRequestReconstructability |
| I12 | `req.AgentClassName != ""` | §24 | CheckRequestReconstructability |

### 2.5 验收

- `agent/wwplayer/invariant_test.go` 12 条测试 PASS
- `go build ./...` OK
- 失败注入：构造一个故意让 `MySeerCheck=1 + MySeerCheckFaction=""` 的 GameContext → invariant fail，错误信息含精确字段名+行号
- 与 §20260814-02 U6 lint 互补：lint 抓静态字段接线，invariant 抓运行时数据契约

### 2.6 教训

（1）**CI 静态 lint 与 runtime invariant 是不同形态的护栏**：lint 抓"声明了却从不接线"（字段存在但无 setter），invariant 抓"运行时数据不变量"（字段存在且 setter 但填错值）。二者不可互替。**§130 第八次复发模式专用双保险**。

（2）**invariant 失败不应阻塞线上**：DSH 模式是 fail-loud + 持续可观测（计数器+日志）。狼人杀 7 bot 房间对延迟敏感，invariant 失败时 Debug 日志 + 计数器而非 panic。CI / 测试环境可 panic。

（3）**invariant 命名 "owned relationships"**（事件+数据），不是断言存在性：DSH `invariant.ts:103` 强调。`assert len(msgs) > 0` 是存在性断言，无 owned relationship，价值低；`assert msgs[-1].role == 'assistant'` 才是 owned。

---

## 3. U3：Pre-step 主动压缩 + 双触发

### 3.1 思路

DSH `compaction-basic/src/index.ts:147-223` 双触发：

```ts
// pre-step 主动（80% thresholdRatio）
if (projectedTokens > thresholdTokens) await compact(ctx)
// overflow 兜底（CONTEXT_WINDOW_EXCEEDED_CODE）
if (err.code === CONTEXT_WINDOW_EXCEEDED_CODE) {
  if (overflowRetries < maxOverflowRetries) { overflowRetries++; await compact(ctx) }
}
// overflowRetries 在每次 assistant/message 成功时清零
if (msg.kind === 'assistant' && success) overflowRetries = 0
```

主动压缩避免"等到 400 才压缩"对慢模型的数分钟空等；overflow 兜底保证估算不准时仍有恢复路径。

### 3.2 狼人杀现状

- 已有：
  - **post-error 路径**（§197 §20260813-04 U4）：`isContextExceededError` → `PruneByBytesAggressive`（50% 预算）
  - **preflight 路径**（§20260813-04 U4）：`preflightBudgetBytes` → 100% 触发 `PruneByBytes`
  - **toolResultPruner 路径**（§20260813-04 U6）：60% 触发 `PruneToolResultsOnly`
- 缺失：
  - **真正的 pre-step 主动压缩**：当前 60%/100%/400 三层是"发请求前裁剪"而非"决策前主动摘要"；DSH 主动压缩触发 LLM summarizer 把 messages 折成一段 summary（`compaction.region.summarize`），这是更激进的语义压缩
  - **overflowRetries 重置语义**：狼人杀 §131 §20260813-04 实现了 `consecutiveFailures` 但缺 "成功即重置 overflowRetries" 语义
  - **CompactConfig 与 preflight 解耦**：狼人杀 `Memory.CompactWithLLM` 已有实现（`agent/wwplayer/memory_compact.go::CompactWithLLM`，139 行），但触发点是 `CompressHistoryLocked`，**未在 pre-step 触发**

### 3.3 实现

新增 `ServerGo/agent/wwplayer/preflight_compress.go`（~150 行）：

```go
// Package wwplayer — preflight_compress.go: pre-step 主动语义压缩。
//
// 2026-08-13 §20260813-05 U3。借鉴 dsh compaction-basic/src/index.ts:147-223
// 双触发(主动 + overflow 兜底)模式。
//
// 与 §20260813-04 U4 已有 preflight(纯字节裁剪)互补:
//   - U4 preflight = 廉价字节回收(不动语义)
//   - 本文件 pre-step = 贵但有效的语义压缩(LLM 摘要)
// DSH 的两层设计: 廉价手段先上,贵手段兜底,与现有架构天然契合。
package wwplayer

const (
    // preflightCompressTriggerPct 是 pre-step 主动压缩的触发阈值。
    // 借鉴 DSH defaultThresholdRatio=0.8。取 80%: 比 preflight(100%)早触发,
    // 给慢模型留出充分恢复时间(§197)。
    preflightCompressTriggerPct = 80

    // preflightCompressMaxOverflow 是 overflow 兜底最大重试。
    // DSH 默认 2。语义压缩慢(每局可能 1-2min),激进重试代价高,取 2。
    preflightCompressMaxOverflow = 2

    // preflightCompressMinPayloadBytes 是压缩触发的下限。
    // 与 §20260813-04 U4 preflightMinBudgetBytes=64KB 保持一致,
    // 防止短 payload(<32KB)被误压缩,丢失关键上下文。
    preflightCompressMinPayloadBytes = 32 * 1024
)

// preflightCompressLoop 模仿 dsh compaction-basic 的双触发结构。
// 每次发请求前调用。
//
// 入参:
//   - ctx 含 a.Memory / a.Provider / a.apiKey
//   - payloadBytes 当前消息体字节估算
//   - budget 有效输入预算(同 preflightBudgetBytes)
//   - overflowCount 本局已 overflow 重试累计
//
// 出参:
//   - newPayloadBytes 压缩后字节数
//   - newOverflowCount 更新后的 overflow 计数
//   - compressed true = 触发了压缩
func preflightCompressLoop(
    ctx context.Context,
    a *Agent,
    payloadBytes int,
    budget int,
    overflowCount int,
) (int, int, bool) {
    if budget <= 0 || payloadBytes < preflightCompressMinPayloadBytes {
        return payloadBytes, overflowCount, false
    }
    trigger := budget * preflightCompressTriggerPct / 100
    // 主动触发: payloadBytes > 80% budget
    if payloadBytes > trigger {
        return runCompactLocked(ctx, a, "preflight_pre_step")
    }
    // overflow 兜底: 已 overflow N 次, payloadBytes 仍 > 30% budget (避免反复触发)
    if overflowCount > 0 && payloadBytes > budget*30/100 {
        return runCompactLocked(ctx, a, "preflight_overflow")
    }
    return payloadBytes, overflowCount, false
}

// runCompactLocked 复用 Memory.CompactWithLLM + 失败 fallback PruneByBytesAggressive。
// 返回 (newPayloadBytes, newOverflowCount, true)。
func runCompactLocked(ctx context.Context, a *Agent, source string) (int, int, bool) {
    before := a.Memory.TotalPayloadBytes()
    // 主动压缩: 调 LLM summarizer (Memory.CompactWithLLM 已有实现)
    summary, err := a.Memory.CompactWithLLM(ctx, a.Provider, a.apiKey, a.ModelKey,
                                            gcFromAgentContext(a), a.CompactConfig())
    if err == nil && summary != "" {
        a.SetLastCompactSummary(summary)
        a.Memory.CompressAndPrune(a.Memory.maxTurns, a.Memory.compressTurns)
        after := a.Memory.TotalPayloadBytes()
        logger.L().Info("agent: pre-step compact success", ...)
        return after, 0, true  // 成功 → 重置 overflow 计数 (DSH §8.6 不变量)
    }
    // 失败 fallback: 字节裁剪
    a.Memory.PruneByBytesAggressive()
    after := a.Memory.TotalPayloadBytes()
    overflowCount++
    if overflowCount > preflightCompressMaxOverflow {
        logger.L().Error("agent: pre-step compact overflow exhausted", ...)
    }
    return after, overflowCount, true
}
```

接线点（修改现有 2 处 + 新增 1 处）：

1. `agent/wwplayer/run.go` 发请求前调 `preflightCompressLoop`，把 `overflowCount` 挂到 `Agent` struct
2. `agent/wwplayer/run.go` 现有 `isContextExceededError → PruneByBytesAggressive` 路径补 `overflowCount++` + 复用 compact 路径（不是重复压缩，而是改用 compact + overflow 语义）
3. `Agent` struct 加 `preflightOverflowCount int` 字段（第 26 个 setter，第 86 个字段，**自动被 §20260814-02 U6 lint 校验**）

### 3.4 验收

- `agent/wwplayer/preflight_compress_test.go` 8 项测试 PASS
- `go build ./...` OK
- 注入故障：构造 `payloadBytes=120% budget` → 主动压缩触发 + overflowCount 重置为 0
- 注入故障：构造 `CompactWithLLM 持续失败 + overflowCount=2` → 切到 `PruneByBytesAggressive` 兜底
- 与现有 preflight（§20260813-04 U4）三触发点协同工作：60% toolResultPrune → 80% compact → 100% preflightPrune → 400% post-error prune

### 3.5 教训

（1）**"廉价优先、贵兜底"是不可移动的设计**：DSH §8.6 双触发 + 狼人杀 §20260813-04 U6 三梯度都是同一哲学。**新增 U3 不是替换 §20260813-04 U6，而是补更激进的语义压缩层**。

（2）**压缩 vs 裁剪**是两个动作：裁剪 = 字节降下来（廉价、可逆但可能丢信息），压缩 = 语义保留（贵、不可逆但质量高）。狼人杀 §197 慢模型场景下，宁可 30s 压缩也比 5min 400 后裁剪省时间。

（3）**overflow 计数挂在 Agent 而非 per-call**：DSH `compaction-basic/index.ts:167-177` 在每次成功响应时清零；狼人杀应同步 —— 成功 LLM 调用必须清零 `preflightOverflowCount`，否则下一次 overflow 会立刻触发"已 overflow N 次"的语义错位。

---

## 4. U5：System Prompt 字节稳定 → Provider Cache Hit 提升

### 4.1 思路

DSH 对 DeepSeek 是自动 server-side KV cache（`llm-deepseek/translate.ts:53-62`），Harness 不下 `cache_control`；唯一要求是 **"request/header 折叠保证前 N 轮 request bytes 稳定"**（`agent.ts:465-470`）：

```ts
// dsh agent.ts:465-470 (节选)
// request/header 当且仅当与 folded 不等时才写新事件
if (!callConfigEquals(currentHeader, foldedHeader)) {
  ctx.emit({ type: 'request/header', data: { header: currentHeader } })
}
```

不主动下断点，让 provider 自己做 KV cache；只要 bytes 稳定，cache 自动命中。DSH `cache-control` 在 wire 上**完全缺席**。

### 4.2 狼人杀现状

- `llm/types/types.go::SystemBlock.CacheControl map[string]string` 字段**已声明**（line 150），从狼人杀 §14.1 文档到 §20260814-02 反复提及"已声明"——但 **从未赋任何值**。整个代码库 `grep CacheControl` 只命中：
  - `agent/wwplayer/prompt.go:212` 在 system 块**末尾**赋值（已部分生效）
  - `llm/types/types.go:150` 字段声明
  - `agent/wwplayer/memory.go:415` 读取（用于 `approxSystemToolsBytes` 估算字节）
- 实质：**system 块打了一个 ephemeral 断点，但 tools 块（`ToolDef`）没有 `CacheControl` 字段**——无法在工具集上打断点

### 4.3 实现（双路径，避免与 §20260814-02 U1 重复）

#### 4.3.1 不下断点路线（本次 U5 主路径）

**核心思路**：既然 `CacheControl` 反复声明不接线，与其继续打补丁，不如采用 DSH 路线 —— **让系统 prompt + 工具 schema + 身份块字节稳定**，依赖 provider 自动 cache。

具体动作：

1. **`SystemBlock` 与 `ToolDef` 整块 `deepFreeze`**：在 `llm.LLMRequest` 构造前调用 `runtime.SetFinalizer` + `reflect.DeepEqual` 校验一致性；任何 listener 想改 messages 必须改事件（DSH §8.3 不变量）。
2. **system prompt 字符串提前计算**：当前 `BuildSystemPrompt(selfPortrait, personality, personalityPresetKey, difficultyDirective)` 每次调用都拼接 ~14KB；改为构造期 freeze 到 `a.SystemPromptBytes []byte`（cache 命中靠这个）。
3. **工具 schema 字节稳定**：当前 `BuildTools` 经 `ToolsCache` (per-Agent shapeExtra key) 缓存；U5 增强：cache hit 时直接返回冻结的 `[]llm.ToolDef`，确保 wire 上 bytes 一致。
4. **身份块 + 工具 target enum 入变量**：当前 `BuildUserPrompt` 末尾拼 `【身份】...` + `【游戏规则】...`；DSH `system-prompt/src/index.ts:140` 模式 `{{variable}}` 严格插值 —— 改为把 `Role/Faction/Seat/RolePool/AllPlayers` 等"一次性"信息提前注入 `a.SystemPromptBytes` 的尾部（一次性写入，永不变），user prompt 只需装动态信息（存活列表、投票、speech history）。

#### 4.3.2 字段清理（同时处理）

- 移除 `SystemBlock.CacheControl` 字段（"声明了却从不接线"的根）—— 注释明示 "DSH 风格：不显式打 cache 断点；保持 bytes 稳定即可"
- 移除 `llm/types/types.go::CacheControl` 字段读取路径（`memory.go:415` 改为 `if len(s.Content) > 0` 估算）
- 文档更新 §14.1：明示 "本项目采用 DSH 风格 cache policy：不显式断点，依赖 request bytes 稳定 + provider 自动 cache"

#### 4.3.3 与 §20260814-02 U1 的兼容性

§20260814-02 U1 计划走"显式 4 断点"路线（OpenCode 启发）。两条路线正交：
- **本 U5 路线**：不下断点，依赖字节稳定。**本次实施**。
- **§20260814-02 U1 路线**：显式 4 断点（system / tools / last user / 静态系统）。**留待后续**。

最终落地时两条路线选其一即可，**目前 U5 更稳**（DSH 工程实践证明），U1 可作为升级路径。

### 4.4 实现清单

| 文件 | 改动 | 行数估算 |
|---|---|---|
| `llm/types/types.go` | 删除 `CacheControl` 字段 + `ToolDef.CacheControl` 字段（不存在，无需删） | ~-15 |
| `agent/wwplayer/prompt.go` | 新增 `BuildSystemPromptFrozen(...)` 返回 `[]byte`（整块 freeze） | ~+50 |
| `agent/wwplayer/run.go` | 缓存 `systemPromptBytes []byte` 到 Agent（构造期一次性） | ~+30 |
| `agent/wwplayer/tools_cache.go` | cache 命中返回冻结 `[]llm.ToolDef`（`tools[i].InputSchema = deepFreeze(s)`） | ~+40 |
| `agent/wwplayer/agent.go` | 加 `SystemPromptBytes []byte` 字段（自动被 §20260814-02 U6 lint 校验） | ~+10 |
| `agent/wwplayer/agent.go` | `NewWithRoom` 构造期调 `BuildSystemPromptFrozen` 一次性计算 | ~+20 |
| `agent/wwplayer/run.go` | 发请求时复用 `a.SystemPromptBytes` 而非每次拼接 | ~-20 |
| 测试 | `prompt_freeze_test.go` + `tools_cache_freeze_test.go` | ~+200 |

### 4.5 验收

- `agent/wwplayer/prompt_freeze_test.go` 5 项测试 PASS（含 DSH `Object.isFrozen` 校验）
- `agent/wwplayer/tools_cache_freeze_test.go` 4 项测试 PASS
- `go build ./...` + `go test ./agent/...` 全 PASS
- 13 人局 cache hit rate 实测 ≥ 80%（与 OpenCode 路线目标一致）
- `cmd/wiringlint` 重新跑 → `SystemPromptBytes` 字段有 setter（自动通过）

### 4.6 教训

（1）**"声明了却不接线"的最危险变种**：不是字段从未 setter，而是字段偶尔 setter 但实际从未生效。`CacheControl` 整整一个版本从未真正起作用（仅有 1 处赋值在 system 末尾，tools 块无字段），但代码 + 文档反复声明"可挂"——这种"半生不死"字段比"完全未接线"更难察觉。**§20260814-02 U6 lint 必须能咬到"字段存在但所有 setter 都注释 deprecation"**。

（2）**路线选择 = 哲学选择**：DSH 不下断点（依赖 provider 自动）+ OpenCode 多断点（主动控制）+ Anthropic CacheControl 字段（API 暴露）—— 三种都是工业实践。**狼人杀应学 DSH 的"工程哲学一致性"**：cache hit rate 高低本质是"是否一致"，不论哪种路线，关键是**避免混用多种**。

（3）**DeepFreeze 的代价是显式所有权**：DSH `call-config.ts:88-117` `deepFreeze` 迭代遍历 + `AbortSignal` 显式跳过；狼人杀应同步实现 —— Go 无 `Object.freeze`，但有 `reflect.DeepEqual` 校验 + 构造期一次性。

---

## 5. U1 / U4 / U6 / U7 —— 后续 commit 设计

> 本次 commit 不落地这 4 项，但给出设计草案，便于后续 commit 独立实施。

### 5.1 U1：ModelContext 持久化

**借鉴点**：DSH `LlmModelContext` + `usage-projection.ts:198-205` 三轴账本（input / output / cacheRead / cacheWrite）。

**狼人杀现状**：`Agent.avgLLMLatencyMs / totalLLMCalls / MyLastLLMLatencyMs` 是延迟维度；缺三轴 token 账本。

**改造**：

- `Agent` struct 加 `ModelUsageStats{ InputTokens, OutputTokens, CacheReadTokens, CacheWriteTokens uint64 }`
- Provider 在每次 `Chat` 成功后回调注入
- `BotTranscript` 加同名字段（统计可观测性）
- §20260814-02 U4 transcript 6 桶中"统计元数据"桶接入

### 5.2 U4：AbortSignal 三源 fuse + cancelCause 包装

**借鉴点**：DSH `agent-loop/src/index.ts:479-487`。

**狼人杀现状**：`r.mu` 持锁路径调 watchdog、agentCancels 释放（§92a）；已做一半但 reason 丢失。

**改造**：

- `ServerGo/game/werewolf/room_manage.go` 引入 `WerewolfRoom.cancelCause error`，三源（user cancel / room force-close / stage watchdog）fuse 成一个 controller
- `defer cancel(WithReason(ctx, "watchdog-deadlock"))` 而非 `defer cancel()`
- 与 §20260814-02 §130 "abort reason 丢失"问题在源头被消灭

### 5.3 U6：Tool-result 剪枝永不静默删

**借鉴点**：DSH `compaction-tool-result-pruner` `PRUNE_MARKER` 占位 + shadow-price 事件。

**狼人杀现状**：§20260813-04 U6 `PruneToolResultsOnly` 有截断，但**无 marker**；§137 `drainPropInjectQueueLocked` 过期递减，bot 不知道。

**改造**：

- `Memory.PruneToolResultsOnly` 返回值 + 在截断的 text 末尾插 `[...因上下文预算被压缩, 实际长度 N 字节...]`
- `approxPayloadBytes` 必须计入 marker（避免 §20260813-04 教训 6 的"漏算 c.Content"重演）
- §137 `gc.PropInjectText` 同步插 marker

### 5.4 U7：LLMProvider Refactor

**借鉴点**：DSH `attribution.ts` + `using` idle watchdog + `api-key.ts` 归一化。

**狼人杀现状**：`llm/anthropic/anthropic.go::Provider.Chat` 无 idle watchdog，stream idle 超时（`streamIdleTimeout`）已有但 total 超时与 caller signal 分离不彻底。

**改造**：

- `llm/anthropic/anthropic.go` 引入 `idleTimeoutReader` 用 Go `defer timer.Stop()` 等价于 DSH `using`
- `llm/anthropic/api_key.go` 抽 `NormalizeAPIKey(raw string) string` 收口空字符串 / whitespace / Bearer 前缀
- §130 / §20260814-02 §130 反复暴露的"key 在某条路径上空字符串"问题在源头堵死

---

## 6. 验收 checklist

### 6.1 本次 commit（U2 / U3 / U5）

- [ ] `agent/wwtypes/invariant.go` 12 条不变量 + 测试全 PASS
- [ ] `agent/wwplayer/preflight_compress.go` 双触发 + 8 项测试全 PASS
- [ ] `agent/wwplayer/prompt_freeze.go` + `tools_cache_freeze.go` 9 项测试全 PASS
- [ ] 移除 `llm/types/types.go::CacheControl` 字段 + `SystemBlock.CacheControl`（"声明了却从不接线"清理）
- [ ] `cmd/wiringlint` 重新跑 → 0 缺陷
- [ ] `go build ./...` + `go test ./agent/... ./game/werewolf/... ./llm/...` 全 PASS
- [ ] §14.1 文档更新（明示 DSH 风格 cache policy）
- [ ] CLAUDE.md 加 §20260813-05 lesson（4 条教训）

### 6.2 后续 commit（U1 / U4 / U6 / U7）

- 各自独立 commit，节奏类似 §20260813-04（4 项增量 + wiring lint 升级）
- 每项必须有"先失败后通过"双向验证的回归测试

### 6.3 端到端验收

- [ ] 13 人局 cache hit rate ≥ 80%（U5）
- [ ] 主动压缩触发后 bot 仍能正确决策（U3）
- [ ] invariant 故意注入失败 → 单元测试 PASS（U2）
- [ ] 7 bot 房间全程无 §130 复发（综合）
- [ ] 测试报告：模拟 13 人局 5 局，invariant 无运行时违反

---

## 7. 教训总结（写入 CLAUDE.md §20260813-05）

1. **runtime invariant companion 与 CI 静态 lint 是互补护栏** —— 前者抓运行时数据契约、后者抓静态字段接线。§130 第八次复发模式（"声明了却从不接线"）必须双保险：lint 抓字段，invariant 抓填值。
2. **双触发 = 主动压缩 + overflow 兜底** —— 单触发（仅 post-error 兜底）让 §197 慢模型场景下数分钟白等；主动 80% 阈值给慢模型留恢复时间。
3. **Provider cache 不下断点依赖字节稳定** —— §14.1 `CacheControl` 字段整整一个版本从未生效是"半生不死"字段的典型；改为 DSH 风格"bytes 稳定 + provider 自动 cache" 路线消除"声明却不接线"风险。
4. **DSH 哲学的"日志是真相 + invariant companion 持续守护"对 §130 反复复发是根治** —— 凡"声明了却不接线"必须 wiring lint + runtime invariant 双护栏；CI 时 lint + 线上 invariant 持续校验。

---

## 8. 一句话总结

借鉴 DeepSeek Harness 的**"日志为真相 + invariant companion 持续守护"**哲学，对狼人杀 Agent 现状的 3 个最高 ROI 改造是：**runtime invariant 护栏（U2）+ pre-step 主动压缩（U3）+ Provider cache 字节稳定路线（U5）** —— 三者共同根治 §130 反复复发 + 提升 cache hit rate + 让慢模型不再白等数分钟。