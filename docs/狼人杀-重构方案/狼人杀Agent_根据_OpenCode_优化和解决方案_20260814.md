# 狼人杀 Agent 根据 OpenCode 优化和解决方案 (2026-08-14)

> **本文件**：基于 OpenCode Agent 源码分析（`docs/其他Agent代码分析/OpenCode_*.md` 三份） + 本仓库狼人杀 Agent 现状分析，给出新一轮的优化方案与实施计划。
> **编号**：§20260814-01（接续 §20260814-01 双协议 LLM 接入、§20260813-04 Hermes 优化、§20260812-04 wiring lint）
> **作者**：AI Agent 持续维护，依据 CLAUDE.md §13.1 职责线 7 (`llm-provider`) + 8 (`werewolf-agent`)
> **落地承诺**：本文件含 6 条 P0/P1 改造项 + 落地代码改动清单 + 单元测试断言

---

## 1. 定位与边界

### 1.1 OpenCode 对本仓库的可借鉴面

| OpenCode 设计点 | 是否适合狼人杀 Agent |
|---|---|
| Plan/Build 同 session 通过 agent 字段切换 | ❌ 不适合（狼人杀没有"先 plan 后 build"语义；但玩家 + 法官分两类 agent 可以借鉴此思想） |
| Plan agent 把 `edit: *` deny 落到 permissions 层 | ✅ **本仓库已有 `deriveSubagentSessionPermission` 类似思想**（如 BotUser 的权限分层），但狼队 wolf_whisper 工具未做权限派生，须补 |
| Sub-agent 独立 session + parentID 树 + task_id resume | ❌ 不适合（狼人杀没有"分叉多任务"语义） |
| `general` agent `todowrite: deny` 防止子 agent 篡改主 agent 的 todo | ✅ 借鉴 —— **本仓库多假说推演 / 公开质疑 / 道具策略不应污染主 agent 战略** |
| `MemoryInjectRunes` 等难度档位按角色注入 | ✅ 已经在 §131 / §20260812-04 U4 落地，借鉴已通过 |
| Provider `CachePolicy` "auto" 3 breakpoint | ✅ **本仓库 Anthropic provider 已实现 `SystemBlock.CacheControl`**（§14.1），借鉴已通过 |
| `protocols/anthropic-messages.ts` `ANTHROPIC_BREAKPOINT_CAP = 4` | ✅ 借鉴已通过 |
| `compaction.ts:PRUNE_PROTECT=40_000 / PRUNE_MINIMUM=20_000` | ✅ §20260813-04 U6 `PruneToolResultsOnly` 已实现类似机制（`keepRecent=12, truncTo=512`），借鉴已通过 |
| `LLMEvent` 协议不可知事件流 | ✅ 本仓库 `llm.LLMResponse` 已统一跨 Anthropic / OpenAI，借鉴已通过 |
| Doom Loop threshold=3 触发权限询问 | ❌ **不直接适合** —— 狼人杀 Agent 出错是游戏机制问题，应 quarantine 而非弹权限 |
| Effect-TS `Service / Layer / LayerNode.make` DI 模式 | ❌ Go 项目用不到，但 **interface 抽象 + Locked 变体的双变体模式可以借鉴** |

### 1.2 狼人杀 Agent 对 OpenCode 的 6 项可落地改造

经过逐点对比，本文件归纳出 6 项对当前狼人杀 Agent 有直接价值的改造：

| # | 改造 | OpenCode 启发 | 类型 |
|---|---|---|---|
| **U1** | **Provider `CachePolicy` "auto" 4-breakpoint 显式调度** | `cache-policy.ts:99-111` 三个断点（tools / system / last user） | P0（cache hit rate +25% 预期） |
| **U2** | **WolfWhisper `derivePermission` 工具层权限派生** | `subagent-permissions.ts` 默认 deny todowrite/task 的派生模式 | P1（防止身份未确认狼 Agent 误用） |
| **U3** | **`LLMSlot` 容量上报到 Provider（透明背压）** | `LLMClient` 的 `RequestExecutor` 5xx 重试透明化 | P1（提升慢模型房间稳定性） |
| **U4** | **BotTranscript 字段按 6 类语义分桶 + 跨轮保留** | `ToolsCache` shapeExtra 哈希模式 | P0（5.0 期许：将 LastSpeech / HeartThought / LastDecision 等拆桶） |
| **U5** | **`AgentRunTrace` 跨调用追踪 ID + 流式断点续传 marker** | `steering_queue.go` 实时事件注入通道；OpenCode trace 模型 | P1（前端 AgentThoughtPanel 折叠断点可视化） |
| **U6** | **机制化 `AgentWiringLint` —— AST 提取 Agent struct 字段并断言"每个字段都被构造时赋值或接入"** | OpenCode 无 AST 级 lint（Python ABC 强约束，Go 缺） —— 这是本仓库比 OpenCode 更进一步的点 | P0（§130 第七次复发根治） |

> **关于 §124 Plan/Build 同 session 切换**：本仓库狼人杀与 OpenCode 不存在"先规划后实施"语义。狼人杀的"规划"等价于**夜间狼队 wolf_whisper**（队内策略协商），它本身就是协议层隔离（§119 §133）；不需要 Plan/Build 切换。
>
> 关于 §125 Do/Verify/Recall 三阶段 workflow（OpenCode workflow + LabVIEW CGen 启发）：本仓库狼人杀每夜有 NightGuardProtect → NightWolves → NightSeerCheck → NightWitchAct → NightDemonHunterHunt 等 5 个明确阶段，每阶段有"前置检查 → 行动 → 校验"三段式（已在 §197 流式续命 + §20260812-04 U5 wiring lint 落地）。本文件不重复。

---

## 2. U1: Provider CachePolicy "auto" 4-breakpoint 显式调度

### 2.1 OpenCode 启发点
- `packages/llm/src/cache-policy.ts:6-15` 注释明确"auto 三个断点"：tools 末尾 + system 末尾 + messages 最后一条 user
- `protocols/anthropic-messages.ts:238` `ANTHROPIC_BREAKPOINT_CAP = 4` 守护
- Anthropic 5m-cache 写 1.25x 读 0.1x

### 2.2 本仓库现状
- `llm/anthropic/anthropic.go` 已实现 `cache_control` 标记
- `llm/types/types.go::SystemBlock.CacheControl` 字段已声明
- **但目前 system prompt 仅**有 `lastSystemBlock` 一处 `cache_control`（§14.1 §20260813-04 U6）
- **tools 段**、**user message 最后一条**都没有显式 cache breakpoint
- **cache hit rate 实测**（§20260813-04 U3 diff 报告）: 13 人局 60-70%，低于 Anthropic 8x 倍受期望

### 2.3 改造内容

在 `llm/types/types.go` 已有的 `CacheControl` 上做 **"auto" 三断点 + cap=4** ：

1. **`llm/anthropic/cache_breakpoint.go`** 新文件
   - `cacheBreakpointResolver(req *LLMRequest) cachePlan`
   - 显式 4 个插入点：`lastSystem / lastTool / lastUser / lastToolResult`
   - 在 `proto/system.txt` / `proto/tools.txt` / `proto/messages.txt` 三个模板上 Apply

2. **`llm/anthropic/cache_breakpoint_test.go`** 新文件
   - 测试 4 个 cap 不超 4
   - 测试 tools 升序稳定 (`request.ts:184 sort by key`)
   - 测试 last user message breakpoint 命中
   - 测试 user message + cache breakpoint 同位置时去重（关键：`SanitizeMessagesForAnthropic` 不破坏）

3. **`llm/anthropic/anthropic.go::ChatStreamAccumulate` 接线**
   - 在 `doRequest` 前调 `cacheBreakpointResolver(req)` → 写回 `req` 的 `CacheControl` 字段
   - 已有 `SystemBlock.CacheControl` 字段不变
   - **新增** `Message.CacheControl`（每条 user/assistant message 可选）+ `ToolDefinition.CacheControl`

4. **`agent/wwplayer/prompt.go::BuildSystemPrompt`** 同时**显式**给 system 中段打 1 个临时 breakpoint 而非末尾（"常用注入块" + "静态身份段"分开），让缓存精度更高：
   - 静态身份段 (前 4 KB) 永久 cache 标记
   - 动态注入段 (后 ~6 KB) cache 标记（每个 bot 一变就换 cache key）
   - **要求**：本仓库 system prompt 7 段固定顺序（§119），两段拆分不能破坏现有顺序

5. **新增配置** `LsmAgentGame.conf → werewolf.cache.breakpoints = "auto|static|dynamic|none"`（默认 auto）

### 2.4 关键代码路径
- `llm/types/types.go::SystemBlock` —— 新增 `CacheControl *CacheControl` 字段已存在
- `llm/anthropic/cache_breakpoint.go` (新) —— `cacheBreakpointResolver`
- `llm/anthropic/anthropic.go::ChatStreamAccumulate` —— 替换 `req.CachingPlan` 为 `cacheBreakpointResolver(req)`

### 2.5 验收
- 同一房间 7 bot 6 轮对局 cache hit rate ≥ 80%（双协议已有共享 Provider，breakpoint 共享）
- token 计费 `cache_write` 稳定每对局 1 次，`cache_read` 占总 input > 50%
- `go test ./llm/anthropic/... -count=1` 4 项全 PASS

---

## 3. U2: WolfWhisper 工具层权限派生

### 3.1 OpenCode 启发点
- `subagent-permissions.ts:deriveSubagentSessionPermission` 默认 deny todowrite + task
- `task.ts:131` 通过 `childToolDenies.filter(...)` 二次去重防冲突

### 3.2 本仓库现状
- `agent/wwplayer/tools_wolf.go::addWolfWhisperTool` 已校验 `faction=="wolf" && WolfTeammateSeat>=0`
- 服务端 `agent_runner.go::WolfWhisper` 二次校验 `State.Roles[seat]==RoleWerewolf`（§133 教训 4）
- **但**： `WolfPackCipher`（暗号系统）目前**没有**工具层权限派生 —— 任何身份的 Agent 都可看到 cipher 模板
- **`HypothesisTracker`**（多假说推演）当前把任何身份的假说都广播 —— 应只允许公开发言阶段的 Agent 写入
- **`ReasoningChain`**（推理链）当前对所有 Agent 开放 —— 应仅 `phase == speak|voting` 时开放

### 3.3 改造内容

1. **`agent/wwplayer/tools_wolf.go::addWolfWhisperTool`** 增补工具层权限派生：
   - `faction == RoleWerewolf` 才挂载
   - `WolfTeammateSeat >= 0` 才挂载（已做）
   - `>= WolfPackCipherLevel` 才挂载 `wolf_pack_cipher`
   - **新增**派生函数 `deriveWolfToolPermission(agent Agent, gc GameContext) []ToolDef`

2. **`agent/wwplayer/tools_hypothesis.go` (新)** 提取 `HypothesisTracker` 工具层入口
   - 仅 `phase in {Speak, Voting, NightWolves, NightSeer}` 4 个阶段挂载
   - 非神职身份不可写 "seer 假说" 类目（权限派生）

3. **`agent/wwplayer/tools_reasoning.go`** 改造：
   - `derivePhasePermission` 集中管控 11 类工具的 phase × role × seat 维度挂载

4. **服务端兜底**：`agent_runner.go::HypothesisLock`** 与 `agent_runner.go::ReasoningChainLock` 也加同样派生校验（与 §133 wolf_whisper 双重防御一致）

### 3.4 关键代码路径
- `agent/wwplayer/tools_wolf.go::addWolfWhisperTool`
- `agent/wwplayer/tools_registry.go::MountTools` —— 增补 `DerivePermission` 调用链

### 3.5 验收
- 已死亡狼 Agent 调用 `wolf_whisper` 后服务端拒绝（§133 教训 4）
- Cipher / Hypothesis / Reasoning 三个工具按 phase × role 派生挂载，新增 `agent/wwplayer/tools_permission_test.go`

---

## 4. U3: LLMSlot 容量上报到 Provider（透明背压）

### 4.1 OpenCode 启发点
- `packages/llm/src/route/executor.ts:rateLimitDetails` 解析 `anthropic-ratelimit-*` / `x-ratelimit-*` headers
- `effect/cron` 流式监控

### 4.2 本仓库现状
- `Agent.acquireLLMSlot(wait=5s)` 房间级 cap=4 信号量（`roomLLMConcurrency`）
- **没有**把上游 provider 返回的 rate-limit headers 反压到本地信号量
- 13 bot × 内层 6 轮 × 重试链打满时（Tencent 16:32 BUG-R220），本地信号量全满，但上游已被 ratelimit
- §20260812-04 U5 仅修了一个 bug：defer ReleaseLLMSlot 在 for 体内。**没有修"上游 429 时本地信号量不感知"**

### 4.3 改造内容

1. **`agent/core/ratelimit_slot.go` (新)** 透明背压接收器：
   - `type LLMSlot struct { current, capacity, inflightWindow, predWindow []time.Time }`
   - `func (s *LLMSlot) ReportRateLimit(headers http.Header)`
   - 解析 `anthropic-ratelimit-tokens-remaining: 0` → 短时降级本地信号量

2. **`llm/anthropic/anthropic.go::doRequest`** ：
   - HTTP 响应 200 走 `ParseRateLimitHeaders(resp.Header) → llmSlot.ReportRateLimit`
   - HTTP 响应 429 / 529 / 503 走 `llmSlot.MarkSoftLimit(duration)`
   - **`endpoint_breaker.go`** 已记录 endpoint 失败，再加 `rateLimitHeaders` 通道

3. **`agent/wwplayer/agent.go::acquireLLMSlot`** 行为变化：
   - 正常路径不变
   - **新增**：当 `llmSlot.IsDegraded()` 时 `WaitForImprovement(deadline)` 而非 `Wait(ctx, 5s)`
   - 与 `endpoint_breaker.go` 已经在做的 60s 自愈形成两级降级：endpoint 失败 → 60s 不可用，rate-limit 软上限 → 自适应 backoff

4. **配置** `LsmAgentGame.conf → werewolf.llm_slot.adaptive_backoff = true|false`（默认 true）

### 4.4 关键代码路径
- `agent/core/ratelimit_slot.go` (新)
- `llm/anthropic/anthropic.go::ChatStreamAccumulate` —— 加重试响应处理
- `agent/wwplayer/agent.go::acquireLLMSlot`

### 4.5 验收
- 13 人局 Kimi 双 bot 同时打满 → 上游 429 → 第三个 bot 立即感知降级，等 5-15s 而非 `Wait(5s)` 超时重试
- 不引入新的 cf++ 路径（与 §108 retry-cooldown-window 30s + §120 transient 不计 cf 一致）

---

## 5. U4: BotTranscript 字段按 6 类语义分桶 + 跨轮保留

### 5.1 OpenCode 启发点
- `ToolsCache` 的 `shapeExtra` 哈希模式（`tools_cache.go`）—— 把影响工具 schema 的所有入参打包成 key
- 把"transcript 字段之间不能覆盖"的设计变成 shape 可见

### 5.2 本仓库现状
- `agent/wwplayer/agent.go::BotTranscript` 60+ JSON 字段
- **§127 教训**： `recordTranscript` 末尾整体替换 `a.lastTranscript`，覆盖风险：
  - `LastSpeech`（speech 成功后由 chatSvc 写入）
  - `LastThought` / `HeartThought`（speech 期间）
  - `LastDecisionSummary`（§128 重构新增）
  - `LastEmotion*`（emotion 更新）
- **§213 教训**： `emotion_switch_speak` 与 `speak_with_thought` 在同响应中调时，前者会覆盖后者的 `HeartThought`

### 5.3 改造内容

将 `BotTranscript` 60+ 字段拆为 6 类语义桶：

| 桶 | 字段 | 写入时机 | 跨轮保留 |
|---|---|---|---|
| **A. 身份 / 静态** | `Seat`, `Role`, `Faction`, `IsAlive`, `WolfTeammateSeat` | 仅 StartAgentsLocked | 整局不变 |
| **B. 决策可观测** | `LastDecisionSummary`, `LastToolInput`, `LastToolResult`, `LastOutcome` | 每 LLM 调用后 | 仅本轮 |
| **C. 情绪** | `Emotion`, `EmotionReason`, `EmotionFx` | 每次 emotion 变化 | 仅本轮（但前次保留到下次切换） |
| **D. 行为可观测** | `HeartThought`, `LastSpeech`, `LastInterject` | 每次 speak | 跨轮保留前 3 轮 |
| **E. 死亡 / 公开** | `DeathCause`, `DeathVerdict`, `LastWords`, `RolePubliclyRevealed` | 仅死亡事件 | 仅死亡后留 |
| **F. 统计 / 元** | `AvgLLMLatencyMs`, `LastLLMLatencyMs`, `TotalCalls`, `APITokens`, `MemoryMDVersion` | 每 LLM 调用后 | 整局累计 |

**不变量**：每个桶独立更新，**绝不**整体替换。

1. **`agent/wwplayer/transcript.go` (新)** —— 按 6 桶管理 transcript：
   - `type BotTranscript struct { A Identity; B Decision; C Emotion; D Behavior; E Death; F Stats }`
   - 6 个独立 setter，每个 setter 只改一个桶
   - `recordTranscript()` 改为不构造新整体，按 6 桶分别序列化

2. **`agent/wwplayer/record_transcript.go` (新)** —— 拆分 `recordTranscript` 为 4 个原子操作：
   - `recordDecisionTranscript(evt)` — 仅桶 B
   - `recordEmotionTranscript(evt)` — 仅桶 C
   - `recordBehaviorTranscript(evt)` — 仅桶 D
   - `recordDeathTranscript(evt)` — 仅桶 E
   - `recordStatsTranscript(usage)` — 仅桶 F
   - 桶 A 仅 `StartAgentsLocked` 一次性写入

3. **`agent/wwplayer/transcript_cross_round.go` (新)** —— 跨轮保留：
   - `BevHistory []DecisionRecord` (最近 5 轮)
   - `BehaviorHistory []BehaviorRecord` (最近 3 轮)
   - `EmotionHistory []EmotionRecord` (最近 5 轮)
   - 历史时长由 `cfgWerewolfTranscriptHistory = 5/3/5` 控制

4. **`game/werewolf/view.go::emitBotContextsLocked`** —— 序列化按 6 桶分别读，**前端不破坏**（JSON 字段平铺）

5. **`agent/wwplayer/transcript_wiring_test.go` (新)** —— 断言：
   - `speak_with_thought` 后再 `emotion_switch_speak`，`HeartThought` 保留
   - `WolfKill` 后 `LastSpeech` 不被覆盖
   - 同 turn 内 `LastDecisionSummary` + `HeartThought` 各自独立

### 5.4 关键代码路径
- `agent/wwplayer/agent.go::BotTranscript` —— 6 桶重构
- `agent/wwplayer/recordTranscript` —— 拆分为 6 个原子 setter
- `agent/wwplayer/run.go` —— 在每条事件分发到对应 bucket setter

### 5.5 验收
- 同 turn `emotion_switch_speak` + `speak_with_thought` 调一次 → 桶 C + 桶 D 各自保留
- `go test ./agent/wwplayer/... -count=1` 新增 6 项 transcript 隔离断言全 PASS

---

## 6. U5: AgentRunTrace 跨调用追踪 ID + 流式断点续传 marker

### 6.1 OpenCode 启发点
- `session/processor.ts:5` 维护 `assistantMessage.id`，用于跨 tool call ID 关联
- `package.json:88` `@opencode-ai/core/util/trace` 全链路 trace-id
- `packages/llm/src/tool-runtime.ts:ToolRuntime.dispatch` 单 tool ID 流式拼接

### 6.2 本仓库现状
- `agent/wwplayer/trace_id.go` 已声明 `TraceID`，但**实际从未注入到 logs 或 LLMRequest headers**
- `llm/anthropic/anthropic.go` 没有 `x-request-id` header（ClaudeCode 有 `metadata.user_id` 但不含 trace）
- 慢模型流式 5-15min 期间，前端 AgentThoughtPanel 看不到进度（仅 `lastUpdateTime`）
- §R232 教训：46 条日志需要靠 PID 4023 才能定位到具体 bot

### 6.3 改造内容

1. **`agent/wwplayer/trace_id.go::NewTrace`** —— 改为真正生成 16 字节 UUID：
   - `AgentRunTrace struct{ AgentRunID string; RunRootSeat int; StartedAt time.Time }`
   - 每 `Run()` 启动一个 `AgentRunID`
   - 每条 LLM call / tool dispatch 复用此 ID
   - 注入到 `logger.With("agent_run_id", id)`

2. **`llm/anthropic/anthropic.go::doRequest`** —— 新增 request header `X-Agent-Run-ID`：
   - 与 `metadata.user_id` 同级（不进 user_id 内，避免改 wire 形状）
   - 与 §130 教训一致：**仅作为出站 header 注入，不进 messages[]**

3. **流式断点续传 marker**：`onLLMStreamDelta` callback 推送 `(traceId, seqInBlock, blockKind)`：
   - 前端 `AgentThoughtPanel` 用 seq + kind 显示进度
   - **离线测试可用**：通过 `ReplayStream(traceID, offset)` API 复盘流

4. **`/api/admin/llm/agents/:seat/run/:traceID`** —— admin 端复盘最近 24h 流
   - 与 §R232 教训对应：审计员可拉该 trace 完整还原

### 6.4 关键代码路径
- `agent/wwplayer/trace_id.go::AgentRunTrace` (扩展)
- `llm/anthropic/anthropic.go::ChatStreamAccumulate` —— 加 X-Agent-Run-ID header + 流式 marker
- `game/werewolf/bot_transcript_view.go::AddTraceLog` (新) —— 把 trace 写入房间级 audit

### 6.5 验收
- 同一 trace 内任意子系统出错时 `kill -QUIT <pid>` 抓 stack 能直接 grep 出 trace → 主体定位
- 前端 AgentThoughtPanel 折叠展开后看到 `seq 12 / 35` 字样（流式进度）

---

## 7. U6 ★ 机制化 AgentWiringLint —— AST 提取 Agent 字段接线检测

### 7.1 起源
- §20260812-04 U6 / §20260813-04 U5 已写"wiring lint"
- 但当前实现是**手工** grep 几个特定字段名（`MemoryInjectRunes`、`MaxToolUse`、`SteeringQueue`）
- §130 第七次复发再次证明：**手工 lint 漏得快、长不快**

### 7.2 OpenCode 启发点
- OpenCode 使用 Effect-TS 的 `Service / Layer` 系统，interface 是**类型级契约**
- 但 **struct 私有字段** 没有编译期约束（Python ABC 才能约束，TS 不行）
- **本仓库比 OpenCode 更进一步**：用 Go `reflect + AST` 对 Agent struct 做全字段扫描

### 7.3 改造内容

1. **`cmd/wiring-lint/wiringlint.go` (新)** —— AGENT 级 lint 工具：
   - 读取 `agent/wwplayer/agent.go::Agent struct` (用 `go/parser` + `go/ast`)
   - 对每个**非可忽略**字段（如 `_` 开头 / `//nolint:wiringlint` 注释 / `sync.Mutex` / `time.Time`）：
     - `grep -l "a.<field>" ServerGo/` 至少 1 个 setter 或构造赋值
     - setter 必须经过 wire 路径（`room_agent.go::StartAgentsLocked` 或类似）
   - 输出表格：`FIELD | SETTER | CALL SITE | NOTE`

2. **`cmd/wiring-lint/wiringlint_test.go`** —— 测试：
   - 解析 `agent/wwplayer/agent.go` 不报错
   - 列出所有字段数 == grep 源码字段数
   - 每个字段都被 setter 覆盖 ≥ 1 次
   - 已退役字段（如 §128 删的 `LastThinking` / `FullThinking` 等） grep 出现"no setter" 则标红 + fail

3. **集成**： `go test ./cmd/wiring-lint/...` 与 CI 集成；任何新 `Agent` 字段如果没 setter，CI fail

4. **与 `wiring_lint_test.go` (§20260812-04) 协同**：
   - 旧版手工 lint 检查「某些特定字段」
   - 新版自动 lint 检查**所有**字段
   - 旧版作为 demo / smoke 保留；新版作为完整 lint

### 7.4 关键代码路径
- `cmd/wiring-lint/wiringlint.go` (新)
- `.github/workflows/ci.yml` —— 加 wiring-lint step

### 7.5 验收
- 加一个新 `Agent` 字段（如 `RecordLog *core.RecordLogService`）但忘记在 `NewWithRoom` / `StartAgentsLocked` 设置 → `wiringlint` 输出 `FIELD has zero setters` 且 CI fail
- 已存在的 18+ 字段（§118 §131 §133 教训）全部 PASS
- 与 §20260813-04 U5 类似，先**反向验证 lint 真的咬到自己**：临时删除 `SetSteeringQueue` 应报红；恢复后绿

---

## 8. 落地优先级与改造顺序

按风险/价值比排序，建议分两次 commit：

### 第一次 commit: U6 + U4（§130 复发根治 + transcript 整体替换风险）
- **U6 wiring lint** —— Go AST 工具，先建立 CI 守门人
- **U4 transcript 6 桶** —— 解决 §127 教训的 transcript 整体替换 bug

### 第二次 commit: U1 + U2 + U3 + U5（性能 + 公平性 + 可观测）
- **U1 cache breakpoint** —— 直接降低 token 计费
- **U2 wolf 工具权限** —— 端到端补 §133 教训 5
- **U3 LLMSlot 背压** —— §R220 教训的根治
- **U5 AgentRunTrace** —— 排查工具

---

## 9. 新引入文档 + CLAUDE.md 新增 lesson

### 9.1 新文档
- `docs/其他Agent代码分析/OpenCode_记忆管理分析.md`
- `docs/其他Agent代码分析/OpenCode_Context管理分析.md`
- `docs/其他Agent代码分析/OpenCode_意图识别与任务分解分析.md`
- `docs/狼人杀-重构方案/狼人杀Agent_根据_OpenCode_优化和解决方案_20260814.md`（本文件）

### 9.2 CLAUDE.md 新增 §20260814-02 教训条目

> §20260814-02 OpenCode 启示的 6 项升级（§U1-U6）将由 §U6 机制化 wiring lint 守门，根治 §130 第七次复发；§U4 transcript 6 桶修复 §127 整体替换 bug；§U1 cache breakpoint 降 token 计费；§U2 wolf 工具权限补 §133；§U3 LLMSlot 背压根治 §R220；§U5 AgentRunTrace 提升可观测。
>
> **教训**：(1) **手工 grep 永远漏得快**：§20260812-04 U6 + §20260813-04 U5 的手工 lint 在 §130 第七次复发时**未**咬出 SteeringQueue 与 ToolHooks —— 改用 Go AST 全字段扫描才能根治；(2) **Provider cache hit rate 与 anthropic-ratelimit-* headers 必须透明反馈到本地 signal 量** —— 否则本地信号量永远不知道 provider 已满载；(3) **transcript 字段严禁整体替换**：本仓库 §127 教训第 N 次复现，按 6 类语义分桶才能保证同 turn 内多工具调用不互覆盖；(4) **OpenCode 的 plan/build 同 session 切换不适合狼人杀语义**：狼人杀没有先 plan 后 build 拆解，但 wolf whisper 协议层隔离 + cipher 工具权限派生是借鉴的关键。

---

## 10. 风险与回退

| 风险 | 等级 | 回退方案 |
|---|---|---|
| U1 cache breakpoint 写错位置破坏现有 Anthropic cache 命中 | 中 | `cfgWerewolfCacheBreakpoints = "none"` 关闭；保留旧逻辑回退 |
| U2 权限派生错导致狼队 voice tool 失灵 | 中 | 新增 `cfgWerewolfWolfToolOverride = "open|derive"` 双开关；derive 关掉回退原逻辑 |
| U3 rate limit 解析错导致本地永远降级 | 中 | 解析失败 fail-soft，默认 `IsDegraded=false`；与 endpoint_breaker 解耦 |
| U4 transcript 6 桶拆分破坏前端 `game.state.bot_contexts[]` JSON 形状 | 高 | 严格保留 60+ 字段 JSON 形状不变；前端零改动 |
| U5 trace 注入 header 导致上游 400 | 低 | 与 §R232 教训一致：FAIL-SOFT，注入失败仅 `logger.Warn` |
| U6 wiring lint 误报（slice / chan / func 字段） | 中 | 白名单（`//nolint:wiringlint` 注释 + 默认 rule 跳过 6 类） |

---

## 11. 验收 checklist

- [ ] `go build ./...` 通过
- [ ] `go test ./...` 通过（含 4 项新增 transcript 隔离 + wiring lint 测试）
- [ ] `tsc --noEmit` + `npm run build` 前端无变化
- [ ] `cmd/wiring-lint` 在 CI 集成
- [ ] 新增 `t_lsm_game_agent_run_trace` 表（U5）
- [ ] 前端 `AgentThoughtPanel` 显示 `seq 12 / 35` 流式进度（U5）
- [ ] 13 人局 cache hit rate ≥ 80%（U1）
- [ ] Wolf whisper 工具层派生 100% 阻止非狼 Agent（U2）
- [ ] Kimi 双 bot 打满时第三个 bot 等待 < 30s（U3）

---

## 12. 改动文件清单（预估）

| 文件 | 改动 | 行数估计 |
|---|---|---|
| `docs/其他Agent代码分析/OpenCode_记忆管理分析.md` | 新增 | ~600 |
| `docs/其他Agent代码分析/OpenCode_Context管理分析.md` | 新增 | ~500 |
| `docs/其他Agent代码分析/OpenCode_意图识别与任务分解分析.md` | 新增 | ~600 |
| `docs/狼人杀-重构方案/狼人杀Agent_根据_OpenCode_优化和解决方案_20260814.md` | 新增（本文件） | ~500 |
| `CLAUDE.md` | 新增 §20260814-02 lesson 摘要 | ~30 |
| `llm/anthropic/cache_breakpoint.go` | 新增 (U1) | ~250 |
| `llm/anthropic/cache_breakpoint_test.go` | 新增 (U1) | ~200 |
| `llm/anthropic/anthropic.go` | 接线 cache_breakpoint_resolver | +50 |
| `agent/wwplayer/tools_wolf.go` | 加 cipher 工具权限派生 | +80 |
| `agent/wwplayer/tools_hypothesis.go` | 新增 + 阶段派生 | ~200 |
| `agent/wwplayer/tools_reasoning.go` | 新增 + 阶段派生 | ~200 |
| `agent/wwplayer/tools_registry.go` | 加 DerivePermission 调用链 | +30 |
| `agent/wwplayer/tools_permission_test.go` | 新增测试 | ~150 |
| `agent/core/ratelimit_slot.go` | 新增 (U3) | ~200 |
| `agent/core/ratelimit_slot_test.go` | 新增测试 | ~150 |
| `agent/wwplayer/agent.go` | 加 LLMSlot 改造 | +50 |
| `llm/anthropic/anthropic.go` | 加 rate limit header 解析 | +50 |
| `agent/wwplayer/transcript.go` | 6 桶重构 (U4) | ~250 |
| `agent/wwplayer/transcript_cross_round.go` | 新增 (U4) | ~150 |
| `agent/wwplayer/record_transcript.go` | 新增 (U4) | ~150 |
| `agent/wwplayer/transcript_wiring_test.go` | 新增测试 | ~200 |
| `agent/wwplayer/trace_id.go` | 新增 RunTrace 字段 | +50 |
| `llm/anthropic/anthropic.go` | 加 X-Agent-Run-ID header | +30 |
| `api/admin_llm_agents_run.go` (新) | 复盘 API (U5) | ~150 |
| `cmd/wiring-lint/wiringlint.go` | 新增 (U6) | ~300 |
| `cmd/wiring-lint/wiringlint_test.go` | 新增测试 | ~200 |
| `models/t_lsm_game_agent_run_trace.go` | 新增 GORM 模型 (U5) | ~50 |
| `migrations/000300_agent_run_trace.sql` | 新增迁移 | ~10 |
| `.github/workflows/ci.yml` | 加 wiring-lint step | +30 |

**总估算**: ~5,300 行新增 + 改造

---

## 13. 与历史升级的连贯性

| 本次 §20260814-02 | 上一次 §20260813-04（Hermes） |
|---|---|
| U1 cache breakpoint "auto" 4 | Hermes `Cache.Breakpoints` 思路一致 |
| U2 工具权限派生 | Hermes `_compute_threshold_tokens` 思路一致 |
| U3 LLMSlot 背压 | Hermes `rate-limit-handling` 思路一致 |
| U4 transcript 6 桶 | 本次新点 |
| U5 AgentRunTrace | Hermes trace 模型增强 |
| U6 wiring lint AST | §20260812-04 / §20260813-04 手工 lint 的根治 |
| **机制化守住**：wiring lint + cache breakpoint auto + tensor breakdown | 保持 §118 / §131 / §133 教训不复发 |

---

## 14. 总结

OpenCode 启发本仓库 6 项升级中：

- **U1 / U2** 与本仓库已有工作（小同）一致，是补强
- **U3 / U5** 是本仓库独立推进项（OpenCode 类似机制不直接适用狼人杀）
- **U4 / U6** 是本仓库**比 OpenCode 更进一步**：OpenCode 没有 transcript 分桶，也没有 Go AST 级别的 lint

整个 §20260814-02 升级的核心理念：**借鉴 OpenCode 的"分层 + 显式 + 持久化"思想，但不复制 OpenCode 的"plan/build 拆解"等不适用语义**。在 WIRING LINT 这件事上，本仓库用 Go AST 全字段扫描比 OpenCode 的手工 lint 更彻底。

---

## 附录 A：本文件引用的 OpenCode 源码路径
（详见 `docs/其他Agent代码分析/OpenCode_*.md` 三份分析报告）

## 附录 B：本文件引用的本仓库历史教训
- §127 `LastSpeech / HeartThought` 与 `recordTranscript` 整体替换
- §130 "声明了却从不接线" 第五/六/七次复发
- §133 WolfPack 协议层隔离 + 权限派生
- §135 死亡即翻牌 bug 修复
- §20260812-04 wiring lint
- §20260813-04 Hermes 优化
- §U4 transcript 6 桶拆分解决 §127 整体替换
- §R220 / §R232 endpoint failover + retry cooldown 教训
- §197 流式续命
