# 狼人杀 Agent 根据 JiuwenSwarm 优化方案

> 文档日期: 2026-08-13
> 分析来源: `docs/其他Agent代码分析/jiuwenswarm_*.md`
> 当前实现: `ServerGo/agent/wwplayer/` (§Agent 重构)
> 目标: 提取 JiuwenSwarm 的记忆管理/Context 管理/任务分解/Skill 系统的设计模式,
>       优化狼人杀 Agent 的决策质量、上下文效率、跨局学习能力

## 1. 现状分析

### 1.1 当前架构

```
ServerGo/agent/wwplayer/
├── agent.go              # Agent 结构体 + 生命周期
├── run.go                # 主决策循环(Phase 4 主循环)
├── run_llm.go            # LLM 调用 + 重试
├── run_rewake.go         # 唤醒机制
├── prompt.go             # System + User Prompt 构建
├── prompt_budget.go      # 预算控制
├── prompt_night_private.go # 夜间私有信息注入
├── prompt_personality.go # 人设参数
├── prompt_portrait.go    # 模型自画像
├── prompt_difficulty.go  # 难度档位
├── memory.go             # 会话内记忆(多轮对话历史)
├── memory_compact.go     # 历史压缩
├── memory_iterate.go     # 跨局记忆迭代(§131)
├── memory_role_select.go # 按角色选取记忆(§20260812-04 U4)
├── tools.go              # 工具定义
├── tools_registry.go     # 工具注册表
├── tools_anthropic_wire.go # Anthropic 协议对齐
├── tools_prop.go         # 道具工具
├── tools_wolf.go         # 狼人工具
├── commitment_tools.go   # 承诺系统工具
├── decision_summary.go   # 决策摘要
├── decision_trail.go     # 决策追踪
├── reasoning_chain.go    # 推理链
├── consistency_check.go  # 行为一致性校验
├── speak_dedup_recent.go # 发言去重
├── speak_factcheck.go    # 发言事实校验
├── emotion.go            # 情绪模块
├── emotion_switch_speak_tools.go # 情绪切换工具
├── prop_blocks.go        # 道具 prompt 块
├── prop_inspect.go       # 道具检查
├── whisper_factcheck.go  # 私聊事实校验
├── steering_queue.go     # 引导队列
├── tool_hooks.go         # 工具钩子
├── quarantine.go         # 隔离机制
├── retry_config.go       # 重试配置
├── phase_config.go       # 阶段配置
└── wiring_lint_test.go   # 接线 lint 测试
```

### 1.2 核心问题

| 问题 | 现状 | 影响 |
|------|------|------|
| **P1: 决策无规划** | 每轮单步决策,无多步策略链 | 无法执行「先建立信任→再投票」等策略 |
| **P2: 上下文浪费** | GameContext 每轮全量重建,大量字段不变 | 每轮多消耗 2-5KB token |
| **P3: 记忆注入粗糙** | 全量注入 4000 runes,无关信息占比高 | 每轮浪费 ~1500 token |
| **P4: 工具结果无压缩** | tool_result 原样保留(100 条上限) | 道具列表/聊天历史膨胀 |
| **P5: 跨局模式无识别** | §131 记忆迭代只记录单局事实 | 无法识别「某模型在 Day3+ 总是保守」等跨局模式 |
| **P6: 策略无演进** | 工具集固定,无策略模板机制 | 每局从零开始,无法积累有效策略 |

## 2. 优化方案

### 2.1 U1: 决策规划系统 (Decision Planning)

**借鉴**: JiuwenSwarm 的 `TodoToolkit` 动态待办清单

**设计**: 在 `GameContext` 新增 `DecisionPlan` 字段,Agent 在首轮 LLM 调用时生成多步计划:

```go
// wwtypes/context.go 新增
type DecisionStep struct {
    ID       int    `json:"id"`        // 步骤序号
    Action   string `json:"action"`    // 行动类型(speak/vote/wolf_kill/seer_check/...)
    Target   int    `json:"target"`    // 目标座位(-1=无)
    Reason   string `json:"reason"`    // 决策理由(≤60字)
    Status   string `json:"status"`    // pending/in_progress/completed/skipped
}

type DecisionPlan struct {
    Phase   string        `json:"phase"`   // 所属阶段
    Steps   []DecisionStep `json:"steps"` // 步骤列表
    Round   int           `json:"round"`   // 生成轮次
}
```

**触发时机**:
- 每阶段首轮唤醒时,Agent 生成计划
- 后续轮次按计划执行,完成一步更新一步

**Prompt 注入**:
```
【你的决策计划(本阶段)】
第 1 步 [pending]: speak → "我是预言家,查验了 5 号是金水" (理由:建立信任)
第 2 步 [pending]: vote → 3 号 (理由:5 号查杀)
```

**工具扩展**:
- 新增 `update_decision_plan` 工具,允许 Agent 动态调整计划

### 2.2 U2: GameContext 分层缓存

**借鉴**: JiuwenSwarm 的「静态层 + 动态层」分层

**设计**: 将 GameContext 分为三层:

```go
// wwtypes/context.go 新增
type GameContext struct {
    // ─── 静态层(整局不变,缓存一次) ───
    Static *StaticContext `json:"static,omitempty"`
    // ─── 阶段层(阶段内不变,阶段切换时更新) ───
    PhaseState *PhaseStateContext `json:"phase_state,omitempty"`
    // ─── 动态层(每轮变化) ───
    // ... 现有字段 ...
}

// StaticContext 整局不变
type StaticContext struct {
    SeatCount       int      `json:"seat_count"`
    MySeat          int      `json:"my_seat"`
    Role            string   `json:"role"`
    Faction         string   `json:"faction"`
    WinCondition    string   `json:"win_condition"`
    AllPlayers      []PlayerBrief `json:"all_players"` // 座位列表(不含状态)
    GodRolePool     []string `json:"god_role_pool"`     // 本局神职池
}

// PhaseStateContext 阶段内不变
type PhaseStateContext struct {
    Phase            string   `json:"phase"`
    PhaseDeadlineSec int      `json:"phase_deadline_sec"`
    SheriffSeat      int      `json:"sheriff_seat"`
    IdiotRevealedSeats []int  `json:"idiot_revealed_seats"`
    DivineCnt        int      `json:"divine_cnt"`
    PlainCnt         int      `json:"plain_cnt"`
    WolfAliveCnt     int      `json:"wolf_alive_cnt"`
}
```

**实现**: 在 `buildAgentContextLocked` 中:
1. 检查静态层缓存,未命中则构建
2. 检查阶段层缓存,phase 变化时重建
3. 动态层每轮重建

**收益**: 每轮减少 2-5KB 重复信息。

### 2.3 U3: 记忆按需注入

**借鉴**: JiuwenSwarm 的「混合检索 + 分层注入」

**设计**: 将记忆注入分为三层:

| 层级 | 内容 | 注入条件 |
|------|------|----------|
| **L1 核心** | 战绩趋势 + 最近失误 | 始终注入(≤1000 runes) |
| **L2 角色相关** | 当前角色子段 | 始终注入(≤1500 runes) |
| **L3 情境相关** | 其他模型特点 + 策略 | 仅在关键决策阶段注入 |

**实现**: 修改 `InjectBlockForRole`:

```go
// memory_role_select.go 新增
func InjectMemoryByLevel(md, role, phase string, isCritical bool) string {
    // L1: 战绩趋势(始终注入)
    l1 := extractSection(md, "战绩与趋势", 500)
    
    // L2: 角色相关(始终注入)
    l2 := SelectMemoryForRole(md, role)
    l2 = extractSection(l2, "我的失误与教训", 1500)
    
    // L3: 情境相关(关键决策时注入)
    var l3 string
    if isCritical {
        l3 = extractSection(md, "其他模型特点分析", 1000)
        l3 += extractSection(md, "决策策略迭代", 1000)
    }
    
    return formatMemoryBlock(l1, l2, l3)
}
```

**关键决策阶段**: `night_seer` / `night_witch` / `vote` / `sheriff`

### 2.4 U4: 工具结果摘要

**借鉴**: JiuwenSwarm 的 `Message Summary Offloader`

**设计**: 对 > 1000 字的 tool_result 做摘要:

```go
// run.go 新增
func summarizeToolResult(name string, result string) string {
    if len([]rune(result)) < 1000 {
        return result
    }
    // 调用 LLM 生成摘要(异步,不阻塞)
    // 或使用规则摘要(更快)
    return ruleBasedSummary(name, result)
}

func ruleBasedSummary(name, result string) string {
    switch name {
    case "prop_list":
        // 道具列表摘要: "可用道具: [📰公告轰炸(130金) / 🎭剧本迷宫(160金)]"
        return summarizePropList(result)
    case "chat_history":
        // 聊天历史摘要: "近 20 条发言,涉及 5 号/7 号/9 号"
        return summarizeChatHistory(result)
    default:
        return result[:500] + "...[已截断]"
    }
}
```

### 2.5 U5: 跨局模式识别 (Dreaming 借鉴)

**借鉴**: JiuwenSwarm 的 `Dreaming` 离线整理

**设计**: 在 §131 记忆迭代中新增「跨局模式识别」段:

```go
// memory_iterate.go 新增
const memoryCrossPatternSection = "## 跨局模式识别"

func BuildIterationPromptWithPatterns(oldMD, seatFacts, judgeSummary string, compress bool) string {
    // 在原有 prompt 基础上,追加:
    // "5. 在「## 跨局模式识别」段内,识别跨越多局的模式:"
    // "   - 某模型在特定局面下的行为模式(如:DeepSeek 在 Day3+ 总是投票保守)"
    // "   - 某角色的常见失误模式(如:守卫连守同一人)"
    // "   - 某阵营的常见策略模式(如:狼人第 2 夜总是刀预言家)"
    // "   只记录你观察到的、跨越多局的有效模式,单局偶然事件不记录。"
}
```

**触发时机**: 当 `coolingWatchdog` 检测到无人类在线时,触发 Dreaming 整理。

### 2.6 U6: 策略模板系统

**借鉴**: JiuwenSwarm 的 `Skill` 自演进

**设计**: 将成功策略封装为模板,按局面匹配注入:

```go
// strategy_template.go 新增
type StrategyTemplate struct {
    ID          string   `json:"id"`
    Name        string   `json:"name"`        // 模板名称
    Role        string   `json:"role"`        // 适用角色
    Phase       string   `json:"phase"`       // 适用阶段
    Condition   string   `json:"condition"`   // 触发条件(自然语言)
    Content     string   `json:"content"`     // 策略内容
    SuccessRate float64  `json:"success_rate"` // 成功率(0-1)
    LastUsed    int64    `json:"last_used"`   // 最后使用时间
}

// 策略模板注册表
var strategyTemplates = []StrategyTemplate{
    {
        ID: "seer_day1_announce",
        Name: "预言家 Day1 公开查验",
        Role: "seer",
        Phase: "speak",
        Condition: "Day1 发言阶段,已有查验结果",
        Content: "公开你的查验结果,用'我查验了 X 号,结果是 Y 色'的模糊话术,不直说'我是预言家'。",
        SuccessRate: 0.72,
    },
    {
        ID: "wolf_fake_seer",
        Name: "狼人悍跳预言家",
        Role: "werewolf",
        Phase: "speak",
        Condition: "Day1 发言阶段,无人跳预言家",
        Content: "假装预言家,给出一个虚假查验(建议给好人发金水,降低暴露风险)。",
        SuccessRate: 0.45,
    },
    // ... 更多模板
}

// 匹配当前局面的策略模板
func MatchStrategies(role, phase string, ctx *wwtypes.GameContext) []StrategyTemplate {
    var matched []StrategyTemplate
    for _, t := range strategyTemplates {
        if t.Role == role && t.Phase == phase {
            if evaluateCondition(t.Condition, ctx) {
                matched = append(matched, t)
            }
        }
    }
    // 按成功率排序
    sort.Slice(matched, func(i, j int) bool {
        return matched[i].SuccessRate > matched[j].SuccessRate
    })
    return matched[:min(len(matched), 3)] // 最多 3 个
}
```

**Prompt 注入**:
```
【推荐策略(基于当前局面)】
1. 预言家 Day1 公开查验 (成功率 72%)
   公开你的查验结果,用'我查验了 X 号,结果是 Y 色'的模糊话术...
```

**策略演进**: 每局结束后,根据胜负更新模板的 `SuccessRate`:
```go
func UpdateStrategySuccess(templateID string, won bool) {
    // 指数加权移动平均: new_rate = 0.9 * old_rate + 0.1 * (won ? 1 : 0)
}
```

## 3. 实施计划

### Phase 1: 基础优化 (U2 + U3)

**目标**: 减少上下文浪费,提升注入精度

**任务**:
1. [ ] 实现 `StaticContext` 缓存机制
2. [ ] 实现 `PhaseStateContext` 缓存机制
3. [ ] 修改 `buildAgentContextLocked` 使用分层缓存
4. [ ] 实现 `InjectMemoryByLevel` 按需注入
5. [ ] 修改 `run.go` 使用新的注入函数

**预期收益**: 每轮减少 3-5KB token,一局(50 轮)节省 ~200K token/bot

### Phase 2: 决策规划 (U1 + U4)

**目标**: 提升决策质量,减少重复工具调用

**任务**:
1. [ ] 新增 `DecisionPlan` 和 `DecisionStep` 类型
2. [ ] 新增 `update_decision_plan` 工具
3. [ ] 修改 `prompt.go` 注入决策计划
4. [ ] 实现 `summarizeToolResult` 工具结果摘要
5. [ ] 修改 `run.go` 使用摘要后的 tool_result

**预期收益**: 决策连贯性提升,重复工具调用减少 30%

### Phase 3: 跨局学习 (U5 + U6)

**目标**: 提升跨局学习能力,策略模板演进

**任务**:
1. [ ] 修改 `BuildIterationPrompt` 新增跨局模式识别段
2. [ ] 实现 `strategy_template.go` 策略模板系统
3. [ ] 修改 `prompt.go` 注入匹配的策略模板
4. [ ] 实现 `UpdateStrategySuccess` 策略演进
5. [ ] 在 `agent_memory_bridge.go` 中接入策略更新

**预期收益**: 跨局胜率提升 5-10%,策略多样性提升

## 4. 技术细节

### 4.1 静态层缓存实现

```go
// room_agent.go 新增
type StaticContextCache struct {
    mu      sync.RWMutex
    cache   map[int]*wwtypes.StaticContext // seat -> StaticContext
}

func (c *StaticContextCache) Get(seat int) *wwtypes.StaticContext {
    c.mu.RLock()
    defer c.mu.RUnlock()
    return c.cache[seat]
}

func (c *StaticContextCache) Set(seat int, sc *wwtypes.StaticContext) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.cache[seat] = sc
}

func (c *StaticContextCache) Invalidate(seat int) {
    c.mu.Lock()
    defer c.mu.Unlock()
    delete(c.cache, seat)
}
```

### 4.2 阶段层缓存实现

```go
// room_agent.go 新增
type PhaseStateCache struct {
    mu        sync.RWMutex
    cache     map[int]*wwtypes.PhaseStateContext // seat -> PhaseStateContext
    phase     string // 当前阶段,用于检测变化
}

func (c *PhaseStateCache) GetOrBuild(seat int, phase string, builder func() *wwtypes.PhaseStateContext) *wwtypes.PhaseStateContext {
    c.mu.Lock()
    defer c.mu.Unlock()
    if c.phase != phase {
        // 阶段变化,清空缓存
        c.cache = make(map[int]*wwtypes.PhaseStateContext)
        c.phase = phase
    }
    if sc, ok := c.cache[seat]; ok {
        return sc
    }
    sc := builder()
    c.cache[seat] = sc
    return sc
}
```

### 4.3 记忆分层注入实现

```go
// memory_role_select.go 新增
func extractSection(md, sectionTitle string, maxRunes int) string {
    // 找到 sectionTitle 的位置
    idx := strings.Index(md, sectionTitle)
    if idx == -1 {
        return ""
    }
    // 找到下一段标题的位置
    nextIdx := strings.Index(md[idx+len(sectionTitle):], "\n## ")
    if nextIdx == -1 {
        content := md[idx:]
    } else {
        content = md[idx : idx+len(sectionTitle)+nextIdx]
    }
    // 截断
    r := []rune(content)
    if len(r) > maxRunes {
        return string(r[:maxRunes]) + "…"
    }
    return content
}

func formatMemoryBlock(l1, l2, l3 string) string {
    var parts []string
    if l1 != "" {
        parts = append(parts, "【战绩趋势】\n"+l1)
    }
    if l2 != "" {
        parts = append(parts, "【角色教训】\n"+l2)
    }
    if l3 != "" {
        parts = append(parts, "【情境策略】\n"+l3)
    }
    if len(parts) == 0 {
        return ""
    }
    return "\n\n【你的长期记忆(分层注入)】\n" + strings.Join(parts, "\n\n")
}
```

## 5. 测试计划

### 5.1 单元测试

- [ ] `TestStaticContextCache_HitMiss`
- [ ] `TestPhaseStateCache_InvalidateOnChange`
- [ ] `TestInjectMemoryByLevel_Critical`
- [ ] `TestInjectMemoryByLevel_Normal`
- [ ] `TestSummarizeToolResult_PropList`
- [ ] `TestSummarizeToolResult_ChatHistory`
- [ ] `TestMatchStrategies_RolePhase`
- [ ] `TestUpdateStrategySuccess_EMA`

### 5.2 集成测试

- [ ] 13 人局全流程,验证上下文大小减少
- [ ] 13 人局全流程,验证决策计划生成
- [ ] 13 人局全流程,验证策略模板注入

### 5.3 回归测试

- [ ] 既有测试全部通过(`go test ./agent/... ./game/werewolf/...`)
- [ ] 既有 prompt 输出字节一致(Anthropic cache 命中)

## 6. 风险与缓解

| 风险 | 影响 | 缓解措施 |
|------|------|----------|
| 缓存不一致 | Agent 看到过期状态 | 阶段切换时强制 invalidate |
| 记忆分层丢失信息 | Agent 决策质量下降 | L1 始终注入核心信息 |
| 策略模板误导 | Agent 执行不适用策略 | 成功率 < 0.3 的模板不注入 |
| 工具摘要失真 | Agent 误解工具结果 | 摘要保留关键数字和座位号 |

## 7. 验收标准

1. **上下文大小**: 每轮平均减少 30% token(从 ~15KB 降到 ~10KB)
2. **决策连贯性**: 多步策略执行率 > 60%(当前 0%)
3. **记忆注入精度**: 相关记忆占比 > 80%(当前 ~50%)
4. **跨局学习**: 同一模型连续 10 局胜率提升 > 5%
5. **编译通过**: `go build ./...` + `go test ./...` 全 PASS
6. **前端构建**: `tsc --noEmit` + `npm run build` 成功
