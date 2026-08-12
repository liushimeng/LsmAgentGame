# 狼人杀 Agent 持久化记忆（MEMORY.md）设计

> 2026-07-20 §131 新增。目标：让每个 LLM 模型（Agent）拥有一份**跨局、跨进程**的
> 持久化记忆（类比 Claude Code 的 MEMORY.md），每局结束后自我迭代总结，下一局
> LLM 调用时注入，实现"记住其他模型特点、记住自己的失误"的迭代学习。

## 1. 现状与差距

| 已有机制 | 范围 | 持久化 | 差距 |
|---|---|---|---|
| `Memory.messages`（对话历史） | 单局单 Agent | ❌ 进程内存 | 局间/重启即丢 |
| `r.modelMemories`（法官总结） | 单房间单局 | ❌ 进程内存 | 房间销毁即丢；且是法官视角而非"自我"视角 |
| `t_lsm_game_model_game_log` 等 | 审计/回放 | ✅ DB | 是流水账，非提炼后的"经验教训"，不回注 prompt |

**本设计新增**：按 `model_key`（LLM 模型）维度持久化一份 ≤100KB 的 Markdown 记忆，
每局结束后由该模型自己的 LLM 自我迭代更新，之后每局每次 LLM 调用注入。

## 2. 数据库表

新表 `t_lsm_game_agent_memory`（一模型一行）：

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | char(36) PK | UUID |
| `model_key` | varchar(64) UNIQUE | 对应 `t_lsm_game_llm_provider.model`（如 `DeepSeek-model`） |
| `memory_md` | mediumtext | MEMORY.md 内容（Markdown），硬上限 100KB |
| `version` | int unsigned | 乐观锁版本号，每次写回 +1，防多房间并发覆盖 |
| `game_count` | int unsigned | 已累计总结的局数 |
| `last_game_id` | varchar(64) | 最近一次总结的 roomID |
| `last_iterated_at` | datetime NULL | 最近一次自我迭代时间 |
| `created_at` / `updated_at` | datetime | GORM 自动维护 |

- GORM 模型文件：`ServerGo/models/t_lsm_game_agent_memory.go`（遵循 §3 命名规范）。
- `db.go` `AutoMigrate` 追加该模型，无需手工迁移。
- 不建外键：provider 可被软删/重建，记忆按 `model_key` 独立存活。

## 3. 记忆内容结构（MEMORY.md 格式）

自我迭代 prompt 要求模型输出固定段落的 Markdown，便于人读与前端展示：

```markdown
# Agent 长期记忆（模型: DeepSeek-model · 已总结 12 局 · 更新于 2026-07-20 01:23）

## 战绩与趋势
- 近 12 局: 胜 5 / 负 7；狼人胜率 40%，好人胜率 55% …

## 我的失误与教训
- 2026-07-19: 作为女巫第一晚用药救狼自刀，导致解药浪费 → 教训：首夜不救人优先…

## 其他模型特点分析
- Qwen-model: 发言快且多，常悍跳预言家；其第 2 天起的高频指控可信度高…
- GLM-model: 响应慢但投票准，跟随它投票胜率 +20%…

## 决策策略迭代
- 投票前先核对警徽流与查验播报是否自洽，避免 R151 式冤杀…
```

硬约束：
- 段落标题固定 4 个（战绩 / 失误 / 其他模型 / 策略迭代），顺序不可调换 —— 与法官
  5 段总结的解析方式一致，便于 `ParseAgentMemory` 校验与截断。
- 单段 ≤ 2000 字，全文 ≤ 100KB；空段写"暂无"。

## 4. 大小控制：80K 触发压缩，100K 硬上限

| 阈值 | 行为 |
|---|---|
| `memory_md` ≤ 80KB | 正常：新总结直接合并（由 LLM 在迭代时融合，非纯 append） |
| > 80KB（`MemoryCompressThresholdBytes = 81920`） | 迭代 prompt 额外要求"压缩历史：删除 3 局以前的细节，每模型特点保留 ≤3 条"，由 LLM 输出精简版 |
| > 100KB（`MemoryMaxBytes = 102400`） | 服务端硬截断：按段落从旧到新淘汰（保留标题段 + 最近 N 段），保证 UTF-8 rune 边界安全，并记 `logger.Warn` |

压缩不是纯字符串截断：截断只做最后兜底，主路径是"LLM 迭代时主动瘦身"，
避免关键信息（最近战绩、最新教训）被硬切。

## 5. 自我迭代时机与流程

**触发点**：每局游戏结束，法官整局总结生成完成后（`handleGameOverSummaryInternal`
成功路径），由房间侧 `WerewolfRoom` 对本局**每个 bot 模型**异步发起一次自我迭代：

```
phaseWatchdogTick (Status=over)
  → wakeJudgeLockedForSummaryLocked()
    → judge goroutine: handleGameOverSummaryInternal → PersistSummary
  → 【新增】m.iterateAgentMemoriesAsync(r)
      for each unique modelKey in r.seatModelKeys:
        goroutine:
          1. db 读旧 memory_md（无则空）
          2. 构造迭代 prompt：旧记忆 + 本局该座位的角色/胜负/发言摘录/法官总结
          3. 用该模型自己的 provider.Chat 生成新 MEMORY.md（MaxTokens 2048）
          4. 校验 4 段标题齐全 → 不全则走规则压缩合并兜底（旧记忆 + 追加本局一段）
          5. >80K 已在 prompt 要求压缩；>100K 服务端硬截断
          6. db 写回（version 乐观锁：冲突则重读合并重试 1 次，再失败仅 log）
```

- **异步且不阻塞**：每模型一次 LLM 调用走 goroutine，失败仅 `logger.Warn`，
  不影响冷却期/重开投票/关门流程（对齐 §118"异步持久化不阻塞游戏流"）。
- **并发**：同一模型同时只在一个房间（房间级 seats 独占），但重开投票原地复用时
  新旧两局可能相邻触发 —— 用 `version` 乐观锁 + 单飞（per-model sync.Mutex
  存于 manager 级 `memoryMu map[string]*sync.Mutex`）双保险。
- **法官模型**若也是座位模型则同样迭代；纯法官（非玩家）不生成玩家记忆。
- 配置开关：`werewolf.agent_memory_enabled`（默认 true）；迭代额外预算
  `agent_memory_max_tokens`（默认 2048）。0/false 时整链 no-op。

## 6. 记忆注入：每次 LLM 调用参与

**注入点**：`agent.Run` 每次构造 LLM 请求前（`run.go` 两处 `BuildUserPrompt` 之后），
若 `a.MemoryMD != ""`，在 user prompt 末尾追加：

```
【你的长期记忆（跨局积累）】
<memory_md 全文，注入时硬截断到 InjectMaxRunes=4000 字>
（以上是你过去多局的经验；本局信息以上方实时状态为准）
```

- **加载点**：`StartAgentsLocked` 中 `agent.NewWithRoom` 成功后，按 `modelKey`
  从 DB 读 `memory_md` 赋给 `a.MemoryMD`（一次性，失败仅 log，不阻塞启动）。
- **不进入 system prompt**：对齐现有瘦身原则（§13 prompt.go 注释），记忆作为
  user turn 数据注入，避免 system 膨胀且天然享受"最新消息优先"。
- **注入截断**：`InjectMaxRunes = 4000`（约 2K token），保证 13 并发调用不爆窗口；
  全文 100K 是存储上限，注入只带"最相关头部"（段落顺序即重要性：战绩→失误→模型→策略）。
- **与现有 `EnableModelMemoryRecap`（法官总结回顾）关系**：两者并存 —— 法官回顾是
  "上一局发生了什么"（客观），本记忆是"我学会了什么"（主观）。本功能落地后，
  原 in-memory `modelMemories` 保留不动（HistoryDrawer 仍展示）。

## 7. 公平性与隔离

- 记忆按 `model_key` 而非按座位：同一模型无论坐几号位共享一份记忆 —— 这正是
  "迭代学习"语义（模型积累经验）。§15 公平性不变式保持：所有 Agent 代码相同，
  仅模型与各自记忆不同。
- 真人不产生/不读取记忆；观众不可见记忆原文（`memory_md` 不下发 `game.state`，
  仅通过 admin API 可读，见 §8）。
- 记忆**不含本局隐藏信息残留风险**：迭代 prompt 只喂该座位本局可见信息 +
  公开法官总结（法官总结本身含全身份，但那是局后复盘，下一局注入是合法经验）。

## 8. 管理接口（最小集）

复用 §118 admin 体系，追加 2 个端点（`api/llm_api.go`）：

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/admin/llm/providers/:id/memory` | 查看该模型 MEMORY.md 原文 + version/game_count |
| DELETE | `/api/admin/llm/providers/:id/memory` | 清空记忆（软重置，version+1，memory_md=""） |

前端模型管理页后续可挂"查看记忆"按钮（本迭代只做后端 + 类型契约，前端 UI 另立任务）。

## 9. 测试要点

- `t_lsm_game_agent_memory` AutoMigrate 建表。
- 压缩逻辑：构造 85KB 记忆 → 迭代 prompt 含压缩指令；构造 105KB → 硬截断 ≤100KB 且 rune 安全。
- 4 段标题缺失 → 走规则兜底合并而非丢弃。
- version 冲突重试一次后放弃并 log，不 panic。
- `agent_memory_enabled=false` 时全链 no-op。
- 注入截断：100KB 记忆注入后 user prompt 增量 ≤ InjectMaxRunes。
- `StartAgentsLocked` DB 读取失败（如测试环境无 DB）不阻塞 agent 启动。

## 10. 改动清单（实现映射）

| 层 | 文件 | 改动 |
|---|---|---|
| model | `models/t_lsm_game_agent_memory.go` | 新增 GORM 模型 |
| db | `db/db.go` | AutoMigrate 追加 |
| config | `config/config.go` | `WerewolfConfig` + `AgentMemoryEnabled` / `AgentMemoryMaxTokens`；默认值回填 |
| service | `service/agent_memory_service.go` | 新增：Load/UpsertWithRetry/CompressHard 截断 |
| agent | `agent/agent.go` | `Agent` + `MemoryMD` 字段 + setter |
| agent | `agent/agent_memory.go` | 新增：迭代 prompt 构造 / 4 段解析校验 / 注入块格式化 / 常量（80K/100K/4000） |
| agent | `agent/run.go` | 两处 LLM 调用前注入记忆段 |
| werewolf | `game/werewolf/agent_memory_bridge.go` | 新增：`iterateAgentMemoriesAsync` + per-model 单飞 + prompt 组装（复用 BuildSummaryInputLocked 产出） |
| werewolf | `game/werewolf/room.go` | `StartAgentsLocked` 加载记忆；`wakeJudgeLockedForSummaryLocked` 后挂迭代触发 |
| werewolf | `game/werewolf/judge_summary_bridge.go` | 总结成功后触发记忆迭代（替代在 room.go 直接挂，保证"总结→迭代"顺序） |
| manager | `game/werewolf/room.go` / `ws/game_service.go` / `main.go` | `WerewolfManager.SetDB(*gorm.DB)` 注入链 |
| api | `api/llm_api.go` | GET/DELETE memory 端点 |
| 测试 | `agent/agent_memory_test.go` + `service/agent_memory_service_test.go` | 单测覆盖 §9 |
