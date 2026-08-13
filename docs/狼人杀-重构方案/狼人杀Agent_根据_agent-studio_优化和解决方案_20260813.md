# 狼人杀 Agent — 基于 agent-studio 优化和解决方案（2026-08-13）

> **方案日期**: 2026-08-13
> **参考来源**: `docs/其他Agent代码分析/agent-studio_*.md` 三份分析文档
> **适用范围**: 狼人杀 13 人局 Agent 系统（`ServerGo/agent/` + `ServerGo/game/werewolf/` 中 Agent 接入部分）
> **目标**: 借鉴 openJiuwen Studio 的多存储记忆、LLM Client 缓存、Context 池、工具调度等设计思想,系统性提升狼人杀 Agent 的可观测性、可维护性与策略智能度

---

## 1. 现状评估

### 1.1 狼人杀 Agent 已有能力

经过 30+ 轮迭代(R30–R250),狼人杀 Agent 已具备:

| 维度 | 现状 | 主要文档/章节 |
|------|------|---------------|
| **身份与记忆** | MEMORY.md 跨局迭代(§131) + 4 段固定标题 + 角色差异化 | `wwplayer/agent.go::Memory` |
| **工具集** | 阶段化 + 角色化 + 存活过滤(`BuildTools`) | `wwplayer/tools.go` |
| **思考循环** | 一次响应多 tool_use,内层循环上限(§20260810-13) | `wwplayer/run.go` |
| **流式响应** | `ChatStreamAccumulate` + 长预算(§197) | `llm/anthropic/stream.go` |
| **协议合规** | ContentBlock 按 type 收敛(§14.1) | `llm/types/types.go::MarshalJSON` |
| **消息配对** | 末尾合并相邻 user 消息(§82b) | `Memory.SanitizeMessagesForAnthropic` |
| **法官 Agent** | LLM 主持人(§123/§130),独立 tool 集 | `wwjudge/judge.go` |
| **持久化记忆** | `t_lsm_game_agent_memory` 表 + version 乐观锁(§131) | `game/werewolf/agent_memory_bridge.go` |
| **Token 公平** | `ModelName/AvgLLMLatencyMs` 注入(§120) | `wwplayer/agent.go::GameContext` |
| **难度档位** | 4 档(§20260811-09) | `game/werewolf/difficulty.go` |
| **记忆注入预算** | `MemoryInjectMaxRunes=4000` + 角色分子段(§20260812-04 U4) | `wwplayer/agent.go` |
| **持久化日志** | 异步 worker(`RecordLogService` §118) | `agent/core/record_log.go` |

### 1.2 主要差距(对照 agent-studio)

| 维度 | 狼人杀现状 | agent-studio 做法 | 优化方向 |
|------|------------|-------------------|----------|
| **LLM Client 缓存** | 每次 `registry.Get()` 新建 `*Provider` | `@lru_cache(maxsize=32)` 缓存 `(base_url, api_key) → OpenAI` | 减少 client 重建开销 |
| **统一异常处理** | 散落各方法,无装饰器模式 | `@with_exception_handling` 统一收口 | 引入 `WithErrorResponse` 装饰器 |
| **多类型记忆** | 单一 MEMORY.md(4 段硬编码) | `LongTermMemory` + `Variables` 双通道 | 拆"长记忆 + 短记忆 + 用户档案" |
| **作用域隔离** | 单 `model_key` 全局 | `scope_id` (mdb_id) 多空间 | 引入 `room_id` 作用域,按局隔离 |
| **trace 标准化** | `BotTranscript` + `LLMCallPhase` 5 态 | `agent_trace_utils` KB retrieval span | 引入 span 概念,KB 检索/工具调用/重试各为独立 span |
| **session_id 全链路追踪** | 散落 logger, 无统一 ID | `set_session_id` 全链路 | 引入 `request_id` UUID,所有日志带同一 ID |
| **Context 池** | 无(per-game instance) | `_context_pool` 复用,重编译保留 | 借鉴但非首要 |
| **工具去重** | `BuildTools` 每次重建 | 工具 `plugin_key` 去重缓存 | 缓存 `(phase, role) → []ToolDef` |
| **watchdog 日志** | 仅有 logger | `TriggerExecutionLogDB` 表 | 加 ring buffer 内存最近 100 条 |
| **Cron 兼容归一化** | 部分配置有 | `_normalize_posix_cron` 显式 | 已对齐(§198) |
| **API key 加密** | AES-256-GCM(§118) | `decrypt_api_key` | 对齐 |

---

## 2. 优化目标

### 2.1 三大主线

| 编号 | 目标 | 价值 | 工作量 |
|------|------|------|--------|
| **O1** | 引入 LLM Client LRU 缓存,降低高频调用 client 重建开销 | 性能 +10~20% | 中 |
| **O2** | 拆分记忆为"长记忆 + 短记忆 + 行为档案"三通道 | 策略智能 +30% | 中 |
| **O3** | 引入 `request_id` 全链路追踪 + span 化 trace | 可观测性 质的提升 | 中 |

### 2.2 五大增强

| 编号 | 增强 | 价值 | 工作量 |
|------|------|------|--------|
| **E1** | 工具集缓存 (`(phase, role) → []ToolDef`) | 减少重复构造 | 小 |
| **E2** | watchdog 日志 ring buffer | 调试可见性 | 小 |
| **E3** | `WithErrorResponse` 统一异常装饰器 | 代码收敛 | 小 |
| **E4** | GameContext 增量补丁模式(只追加本轮变化) | token 节省 +5~10% | 中 |
| **E5** | 短期记忆 ring buffer(本局本 bot 关键事件) | 决策回溯 | 小 |

---

## 3. 详细方案

### 3.1 O1 — LLM Client LRU 缓存

**问题**: 当前 `llm.Registry.Get()` 每次都构造 `&anthropic.Provider{...}`,虽然构造本身廉价,但高频场景(13 bot 房间,每 bot 1 次/30s 调用)累计浪费。

**方案**: 在 `ServerGo/llm/anthropic/anthropic.go` 引入 `sync.Map` 缓存 `(baseURL, apiKey) → *Provider` 实例。

```go
// anthropic.go 新增
var (
    clientCache sync.Map  // key: baseURL + "|" + maskedKey, value: *Provider
    maxClientCache = 32   // 同 LRU 容量
)

func getOrCreateProvider(baseURL, apiKey string) *Provider {
    cacheKey := baseURL + "|" + maskKey(apiKey)
    if v, ok := clientCache.Load(cacheKey); ok {
        return v.(*Provider)
    }
    p := &Provider{
        httpClient: &http.Client{Timeout: 60 * time.Second},
        // ...
    }
    clientCache.Store(cacheKey, p)
    return p
}
```

**要点**:
- 用 masked key(只保留前 4 字符)做 cache key,避免 key 全文内存泄漏
- `sync.Map` 而非 `lru_cache`,Go 无内置 LRU
- maxClientCache 用 atomic counter 控制
- Provider 的可配置字段(超时、endpoint)若变化,需 invalidate

**回归测试**: `anthropic_client_cache_test.go`
- 同一 (baseURL, apiKey) 多次获取 → 返回同一指针
- 不同 (baseURL, apiKey) → 不同实例
- 容量上限 32 → 超出后 LRU 淘汰
- 改 baseURL → invalidate

**预期收益**: 高频房间下 client 构造耗时归零,GC 压力下降。

---

### 3.2 O2 — 三通道记忆架构

**问题**: 当前单一 MEMORY.md(4 段硬编码标题)把所有信息混在一起,LLM 难以快速定位"本局关键事件"vs"跨局经验"。

**方案**: 拆分三类记忆 + 作用域隔离

```
┌──────────────────────────────────────────────────────────┐
│  Agent 记忆系统                                          │
│                                                          │
│  ┌────────────────────┐  ┌────────────────────────┐   │
│  │  长记忆(LONG_TERM) │  │  短记忆(SHORT_TERM)    │   │
│  │  - MEMORY.md       │  │  - 本局 ring buffer    │   │
│  │  - scope: model_key│  │    ≤ 50 条事件         │   │
│  │  - 跨局持久        │  │  - scope: room_id+seat │   │
│  │  - 4 段标题结构    │  │  - 局内 GC 回收        │   │
│  └────────────────────┘  └────────────────────────┘   │
│                                                          │
│  ┌────────────────────────────────────────────────┐    │
│  │  行为档案(BEHAVIOR_PROFILE)                    │    │
│  │  - 投票模式 / 发言节奏 / 表情切换频率         │    │
│  │  - scope: model_key                            │    │
│  │  - 跨局增量更新                                 │    │
│  └────────────────────────────────────────────────┘    │
└──────────────────────────────────────────────────────────┘
```

**短记忆数据模型**:

```go
// ServerGo/agent/wwtypes/short_memory.go 新增
package wwtypes

type ShortMemoryEvent struct {
    At        int64  `json:"at"`        // unix ms
    Kind      string `json:"kind"`      // "vote" / "speak" / "death" / "tool_use" / "witness"
    Actor     int    `json:"actor"`     // seat
    Target    int    `json:"target"`    // seat, if applicable
    Phase     string `json:"phase"`     // game phase
    Round     int    `json:"round"`
    Summary   string `json:"summary"`   // ≤80 字摘要
    Tool      string `json:"tool,omitempty"`
    Hit       bool   `json:"hit,omitempty"` // 道具命中
}

type ShortMemoryBuffer struct {
    Seat    int
    RoomID  string
    Events  []ShortMemoryEvent // 循环, 容量 50
    Head    int                // 下一个写入位置
    Size    int                // 当前条数
    mu      sync.Mutex
}
```

**实现要点**:
- `WerewolfRoom.ShortMemory[seat]` 13 个 buffer,局内常驻
- 局开始时(`StartAgentsLocked`)初始化
- 局结束(`EmitGameOver`)后 GC 回收
- 注入 prompt 时按优先级裁剪:本局本 bot > 本局全房间 > 跨局长记忆

**回归测试**: `short_memory_test.go`
- AddEvent 超过容量 → 覆盖最旧
- GameContext 注入顺序正确
- 跨局不串数据
- 并发安全(13 bot 同时 add)

---

### 3.3 O3 — request_id 全链路追踪 + span 化

**问题**: 当前 logger 多处有 `botID` 字段但缺乏全局 request_id,排查"为什么这次 LLM 调用被 cancel"很难还原全链路。

**方案**: 引入 `request_id` + span 化 trace

```go
// ServerGo/agent/wwplayer/run.go 新增
type TraceSpan struct {
    RequestID   string                 // UUID, 一次 LLM 调用一个
    BotID       string                 // bot 标识
    Phase       string                 // 当时 phase
    StartMs     int64                  // 开始 ms
    EndMs       int64                  // 结束 ms
    LLMMs       int64                  // LLM 耗时
    Retries     int                    // 重试次数
    SpanName    string                 // "llm_call" / "tool_dispatch" / "memory_retrieve"
    Children    []TraceSpan            // 子 span
    Attributes  map[string]interface{} // 任意键值
    Error       string                 // 错误信息
}

// 注入 ctx: ctx = context.WithValue(ctx, traceKey, span)
type traceKey struct{}
```

**全链路 trace 层次**:
```
llm_call (主)
├── memory_inject_block (子: 注入短期记忆)
├── memory_inject_md (子: 注入 MEMORY.md)
├── tool_dispatch (子: tool_use 派发)
│   ├── wolf_kill
│   ├── speak
│   └── ...
├── response_process (子: 处理流式响应)
└── retry (子: 重试, 多次)
```

**实现要点**:
- 每次 `callProvider` 分配 `uuid.New().String()` 作 request_id
- ctx 传 `*TraceSpan`,子函数可 append children
- trace 落 `BotTranscript.TraceSpans[]`(只保留最近 5 次调用的 span,避免内存爆)
- 前端调试面板可拉取 trace 树

**回归测试**: `trace_span_test.go`
- request_id 唯一性
- span 父子关系正确
- 异常时 Error 字段填充
- 内存不超过限制

---

### 3.4 E1 — 工具集缓存

**问题**: `BuildTools(phase, role, seat, alive)` 每次调用都重新构造 `[]ToolDef`,13 bot 房间 + 4 角色 + 6 phase = 312 次/局,大部分是重复构造。

**方案**: `WerewolfRoom.toolsCache map[string][]ToolDef`,key = `(phase + "|" + role)`,value = 已构造的 tools。

```go
// game/werewolf/tools_cache.go 新增
type ToolsCache struct {
    mu    sync.RWMutex
    items map[string][]ToolDef
}

func (r *WerewolfRoom) buildToolsWithCache(phase string, role string, seat int, alive bool) []ToolDef {
    key := phase + "|" + role
    r.toolsCache.mu.RLock()
    if cached, ok := r.toolsCache.items[key]; ok {
        r.toolsCache.mu.RUnlock()
        return cached
    }
    r.toolsCache.mu.RUnlock()

    tools := BuildTools(phase, role, seat, alive)
    r.toolsCache.mu.Lock()
    r.toolsCache.items[key] = tools
    r.toolsCache.mu.Unlock()
    return tools
}
```

**注意**: `seat` 和 `alive` 在同一 `(phase, role)` 下不影响工具**定义**,只影响 target enum 字段(由 BuildTools 内部动态生成)。所以 cache key 仅需 `(phase, role)`。

**回归测试**: `tools_cache_test.go`
- 同 (phase, role) 多次 → 同一切片
- 不同 (phase, role) → 不同切片
- 房间销毁时 cache 清空

---

### 3.5 E2 — watchdog 日志 ring buffer

**问题**: `phaseWatchdogTick` 5s 一轮,logger 输出大量日志,但排查"为什么这个 bot 被 quarantine"时,只能 grep logger,无法快速回放 watchdog 历史。

**方案**: 内存 ring buffer,最近 100 条 tick 记录。

```go
// game/werewolf/watchdog_log.go 新增
type WatchdogLogEntry struct {
    At       int64  `json:"at"`
    Phase    string `json:"phase"`
    ActingSeat int  `json:"acting_seat"`
    Action   string `json:"action"`   // "tick" / "skip" / "wake" / "quarantine"
    Reason   string `json:"reason"`
    BotID    string `json:"bot_id,omitempty"`
}

type WatchdogLogBuffer struct {
    Entries [100]WatchdogLogEntry
    Head    int
    Size    int
    mu      sync.RWMutex
}

func (b *WatchdogLogBuffer) Append(e WatchdogLogEntry) { ... }
func (b *WatchdogLogBuffer) Snapshot() []WatchdogLogEntry { ... }
```

**集成**:
- `phaseWatchdogTick` 内每条关键事件 `Append` 一次
- 房间销毁时随 WerewolfRoom 一起 GC
- 新增 REST 端点 `GET /api/games/werewolf/rooms/:id/watchdog_log`(调试用,需 isAdmin)

**回归测试**: `watchdog_log_test.go`
- 容量上限 → 覆盖最旧
- Snapshot 顺序正确
- 并发安全

---

### 3.6 E3 — WithErrorResponse 装饰器

**问题**: 狼人杀内部方法(如 `Action_*`)每处都写 try/return 错误,代码冗余。

**方案**: 借鉴 agent-studio `@with_exception_handling` 模式,引入函数装饰(Go 无内置装饰器,但可用 helper func)。

```go
// util/error_decorator.go 新增
type ErrorResponse struct {
    Code    int    `json:"code"`
    Message string `json:"message"`
    Cause   string `json:"cause,omitempty"`
}

// WrapError 统一捕获 panic → 返回 ErrorResponse
func WrapError(fn func() error) (resp ErrorResponse) {
    defer func() {
        if r := recover(); r != nil {
            stack := debug.Stack()
            logger.Error("panic in wrapped fn", zap.Any("panic", r), zap.ByteString("stack", stack))
            resp = ErrorResponse{
                Code:    500,
                Message: "internal error",
                Cause:   fmt.Sprintf("%v", r),
            }
        }
    }()
    if err := fn(); err != nil {
        return ErrorResponse{
            Code:    classifyError(err),
            Message: err.Error(),
        }
    }
    return ErrorResponse{Code: 0, Message: "ok"}
}

func classifyError(err error) int {
    if errors.Is(err, ErrInvalidInput) { return 400 }
    if errors.Is(err, ErrNotFound) { return 404 }
    if errors.Is(err, ErrPermission) { return 403 }
    return 500
}
```

**应用**: 在 manager 层的 Action_* 入口处 `WrapError`,减少样板。

**回归测试**: `error_decorator_test.go`
- 正常路径 → Code=0
- 已知错误 → Code 对应
- panic → Code=500, Cause 含 panic 信息
- 性能开销 < 5%(与直接调用对比)

---

### 3.7 E4 — GameContext 增量补丁

**问题**: 每轮 LLM 调用都注入完整 GameContext(13 人完整状态 + 历史 + 工具),占 token 很大,但实际"上一轮 → 这一轮"的变化只有几条。

**方案**: 引入"增量快照"模式 — 每轮只追加本轮新事件,prompt 中用 `本轮变化` 块 + `前 N 轮摘要` 替代全量。

```go
// agent/wwtypes/incremental.go 新增
type GameContextDelta struct {
    BaseSnapshot  GameContext // 上一轮完整快照(≤ N 轮前的)
    NewEvents     []ContextEvent // 本轮新增事件
    RemovedEvents []ContextEvent // 本轮移除/死亡(可选)
}

type ContextEvent struct {
    Kind    string      // "speech" / "vote" / "death" / "tool_use" / "phase_change"
    Payload interface{}
    At      int64
}
```

**注入策略**:
- 第 1 轮:全量
- 第 2-N 轮:BaseSnapshot(3 轮前) + Delta(本轮 + 2 轮)
- 第 N+1 轮:重新生成 BaseSnapshot,清空 Delta

**回归测试**: `incremental_context_test.go`
- token 节省 ≥ 20%
- 信息完整无丢失
- 边界:阶段切换时必须重建 BaseSnapshot

**注意**: §128「对话即思考」明确反对"显式 thinking 阶段",此优化与 §128 兼容 — 思考仍由 LLM 一次响应内完成,只是 prompt 注入更精简。

---

### 3.8 E5 — 短期记忆 ring buffer(本局本 bot 关键事件)

**问题**: 狼人 bot 在 13 人局 60+ 分钟对局中,本局发生的关键事件(谁投票给谁、谁暴露了、谁被道具击中)很难全部记在 MEMORY.md(那是跨局经验),但局内又必须能回溯。

**方案**: 整合进 O2 短记忆,本节强调"事件压缩策略":

- 容量 50 条(超过则 LLM 主动压缩)
- 事件类型:投票/发言/死亡/道具/工具使用/私人通信
- 注入 prompt 时按相关度排序(本 bot 行动过的 > 涉及本 bot 阵营的 > 其他)

**触发压缩**:
- 每 10 条自动压缩:用 LLM 摘要 10 条 → 1 条
- 关键事件(死亡、暴露)永不压缩
- 局结束时压缩入 MEMORY.md

---

## 4. 实施计划

### 4.1 优先级矩阵

| 任务 | 价值 | 工作量 | 风险 | 优先级 |
|------|------|--------|------|--------|
| **O1 LLM Client LRU** | 中 | 中 | 低 | **P0** |
| **O3 request_id + span** | 高 | 中 | 低 | **P0** |
| **E1 工具集缓存** | 中 | 小 | 极低 | **P0** |
| **E5 短期记忆 ring buffer** | 高 | 中 | 中 | **P0** |
| **E2 watchdog log ring buffer** | 中 | 小 | 极低 | **P1** |
| **E3 错误装饰器** | 低 | 小 | 低 | **P1** |
| **O2 三通道记忆(完整)** | 高 | 大 | 中 | **P1** |
| **E4 增量 GameContext** | 中 | 中 | 中 | **P2** |

### 4.2 实施步骤

**第一步(本次实现,P0 全部)**:
1. O1 LLM Client LRU 缓存
2. O3 request_id + span 基础版
3. E1 工具集缓存
4. E5 短期记忆 ring buffer

**第二步(后续 PR)**:
- E2 watchdog log
- E3 错误装饰器
- O2 完整三通道记忆

**第三步(长期优化)**:
- E4 增量 GameContext

### 4.3 验证清单

每个 P0 任务必须满足:
- [ ] `go build ./...` 通过
- [ ] `go test ./...` 通过(含新增单测)
- [ ] 狼人杀实际跑一局 13 人 7 bot 60 分钟,无新增 panic
- [ ] 性能基线对比:client 构造次数、token 消耗、LLM 延迟
- [ ] 兼容性: §14.1 协议合规,§197 长超时,§128 对话即思考

---

## 5. 预期收益

| 维度 | 现状 | 优化后 | 提升 |
|------|------|--------|------|
| LLM Client 构造开销 | 13 bot × 100+ 次/局 = 1300+ 次 | ≤ 32 次 | **97%↓** |
| prompt token 消耗 | 完整 GameContext 13+ 人 | 增量 + ring buffer | **20~30%↓** |
| 调试效率 | grep logger | trace span 树形展示 | **质的提升** |
| 策略智能 | 单一 MEMORY.md | 三通道记忆 | **局内事件不丢** |
| 代码可维护性 | 散落 try/return | WrapError 装饰 | **样板减少 30%** |

---

## 6. 风险与回滚

| 风险 | 触发条件 | 缓解措施 |
|------|----------|----------|
| LRU 缓存命中错乱 | Provider 配置变更 | 每次 `Reload()` 自动 invalidate |
| request_id 误用 | 异步 goroutine 跨 ctx | 显式 ctx 传递,禁止裸 goroutine |
| 短记忆覆盖关键事件 | 容量过小 | 关键事件(死亡/暴露)永不压缩 |
| 工具缓存内存泄漏 | 房间未销毁 | `stopAgentsLocked` 必清 |
| watchdog log 性能 | 5s tick × 100 容量 | ring buffer 不分配新对象 |
| 错误装饰器开销 | 大量 Action_* 调用 | benchmark 验证 < 5% |

---

## 7. 与现有 lesson 的兼容性

| 现有 lesson | 兼容性 | 说明 |
|--------------|--------|------|
| §14.1 协议合规 | ✅ | LRU 缓存不影响 wire 形状 |
| §82b 消息配对 | ✅ | request_id 与 message 无关 |
| §92a 锁内变体 | ✅ | 短记忆 buffer 自带锁,无外部依赖 |
| §118 模型管理 | ✅ | O1 缓存的 client 由 registry 统一管理 |
| §128 对话即思考 | ✅ | O3 注入 trace 不影响 LLM 思考路径 |
| §131 持久化记忆 | ✅ 增强 | O2 三通道记忆是 §131 的扩展,不替换 |
| §197 长超时 | ✅ | O1 缓存与超时无关 |
| §198 归一化 | ✅ | 沿用现有 `cfgNormalize` 模式 |

---

## 8. 文档与培训

实施过程中需同步:
- [ ] `docs/狼人杀-Agent与系统/狼人杀Agent设计.md` — 增加 LRU 缓存 / 三通道记忆 / request_id 章节
- [ ] `docs/狼人杀-Agent与系统/狼人杀Agent持久化记忆设计.md` — 增加短记忆通道
- [ ] 增量 log/lesson 添加到 CLAUDE.md
- [ ] 单元测试文件命名: `<feature>_20260813_test.go`
