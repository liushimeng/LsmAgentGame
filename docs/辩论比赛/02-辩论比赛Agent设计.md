# 辩论比赛 — Agent 设计

> 本文档描述辩论比赛的 **Agent 架构设计**，包括辩方 Agent（Debater Agent）和裁判 Agent（Judge Agent）。
> 复用狼人杀 Agent 的设计模式：in-process goroutine + LLM 驱动 + 工具派发 + Memory/Compact。

## 1. Agent 类型总览

| Agent 类型 | 代号 | 数量/房间 | 职责 |
|------------|------|-----------|------|
| **辩方 Agent** | `debateplayer` | 4-10 个（按模式） | 代表队伍发言、驳论、质询、自由辩 |
| **裁判 Agent** | `debatejudge` | 3 个（固定单数） | 独立评审、打分、投票 |

## 2. 辩方 Agent（Debater Agent）

### 2.1 设计目标

| 目标 | 实现 |
|------|------|
| 智能论证 | system prompt 含完整辩论规则 + 辩题 + 立场 + 辩位职责 |
| 上下文感知 | Memory 记录全程发言历史 + 对方论点 + 己方策略 |
| 角色差异化 | 不同辩位（一/二/三/四辩）有不同工具集和 prompt |
| 公平性 | 所有辩方 Agent 代码相同，仅模型不同 |
| 可观测 | 观众可见 Agent 的思考过程（internal_thought） |
| 限流 | 发言字数截断 + 单轮最多 5 次 tool_use |

### 2.2 数据结构

```go
// Agent 是一个辩方 bot 的驱动。
type Agent struct {
    // 基础标识
    RoomID    string
    TeamID    int           // 所属队伍 ID
    Seat      int           // 队内辩位（0=一辩, 1=二辩, 2=三辩, 3=四辩）
    Role      string        // "first" / "second" / "third" / "fourth"
    Stance    string        // "pro" / "con" / "neutral" / custom
    UserID    string        // 虚拟 user id
    ModelKey  string        // LLM 模型 key

    // LLM 相关
    Provider  llm.LLMProvider
    Registry  *llm.Registry
    apiKey    string

    // 记忆与上下文
    Memory    *Memory
    Context   *DebateContext

    // 限流
    Limiter   *agentcore.SpeakLimiter

    // 配置
    MaxToolUse int           // 单轮最多 tool_use 次

    // 状态
    mu        sync.Mutex
    quarantined bool
    lastError   string
    botID       string

    // 事件通道
    events    chan DebateEvent

    // 取消
    ctx       context.Context
    cancel    context.CancelFunc
}
```

### 2.3 生命周期

```
房间创建（含 agent_teams）
   ↓
StartGame（所有队伍配置完成）
   ↓ 对每个辩方 Agent
go Agent.Run(ctx)
   ↓
观察 events channel（phase 切换 / 轮到该 Agent 发言）
   ↓ 轮到我
构建 system + messages + tools → provider.Chat → 解析 tool_use
   ↓
DispatchTool → DebateManager.SubmitSpeech / SubmitCrossExam
   ↓
把 tool_result 写回 memory，继续循环
   ↓
比赛结束 → ctx 取消 → goroutine 退出
```

### 2.4 辩位差异化

| 辩位 | 角色 | 可用工具 | System Prompt 重点 |
|------|------|----------|-------------------|
| 一辩 | 立论 | `speech` | 定义权、判准、论点框架 |
| 二辩 | 驳论 | `speech` | 反驳技巧、逻辑漏洞识别 |
| 三辩 | 质询 | `cross_exam_question` / `cross_exam_answer` | 提问技巧、追问策略 |
| 四辩 | 总结 | `speech` | 梳理攻防、升华价值 |

### 2.5 Memory 设计

```go
type Memory struct {
    mu       sync.RWMutex
    messages []llm.Message
    tools    []ToolRecord
    maxPromptBytes int
    totalSystemToolsBytes int
    lastCompactSummary string
}

type DebateContext struct {
    Topic           string            // 辩题文本
    TopicType       string            // classic/policy/value/divergent
    MyStance        string            // 我方立场
    MyRole          string            // 我方辩位
    Phase           string            // 当前阶段
    MyTeamSpeeches  []SpeechEvent     // 我方全部发言
    OppSpeeches     []SpeechEvent     // 对方全部发言
    AllSpeeches     []SpeechEvent     // 全场发言（按时间序）
    CurrentRound    int               // 当前轮次
    TimeRemaining   int               // 当前阶段剩余秒数
    MyArguments     []string          // 我方核心论点列表
    OppArguments    []string          // 对方核心论点列表
    KeyClashPoints  []string          // 关键交锋点
}
```

### 2.6 工具集（辩方）

```go
// BuildDebaterTools 返回辩方 Agent 的工具集
// 按辩位和阶段过滤，只暴露当前可用的工具
func BuildDebaterTools(phase, role string, stance string, gc *DebateContext) []llm.ToolDef {
    var tools []llm.ToolDef

    // 所有辩位通用
    tools = append(tools, llm.ToolDef{
        Name: "speech",
        Description: "正式发言 — 在立论/驳论/总结阶段提交发言正文。\n" +
            "字数限制：立论 ≤ 500 字，驳论 ≤ 400 字，总结 ≤ 600 字。\n" +
            "必须紧扣辩题和立场，引用对方观点时需注明来源。",
        InputSchema: map[string]any{
            "type": "object",
            "properties": map[string]any{
                "content": map[string]any{"type": "string", "description": "发言正文"},
                "references": map[string]any{"type": "array", "items": map[string]any{"type": "string"},
                    "description": "引用的对方发言 ID 列表"},
                "internal_thought": map[string]any{"type": "string",
                    "description": "内部思考过程（观众可见于 Agent 思考面板）"},
            },
            "required": []string{"content"},
        },
    })

    // 三辩专属：质询工具
    if role == "third" {
        tools = append(tools, llm.ToolDef{
            Name: "cross_exam_question",
            Description: "质询提问 — 向对方辩手发起质询问题。\n" +
                "只能提问，不能阐述。问题需精准、有针对性。\n" +
                "字数 ≤ 50 字。",
            InputSchema: map[string]any{
                "type": "object",
                "properties": map[string]any{
                    "target_team": map[string]any{"type": "integer", "description": "被质询队伍 ID"},
                    "target_seat": map[string]any{"type": "integer", "description": "被质询辩位"},
                    "question":    map[string]any{"type": "string", "description": "质询问题，≤50字"},
                },
                "required": []string{"target_team", "question"},
            },
        })
        tools = append(tools, llm.ToolDef{
            Name: "cross_exam_answer",
            Description: "质询回答 — 回答对方的质询问题。\n" +
                "必须正面回应，不得回避或反问。\n" +
                "字数 ≤ 100 字。",
            InputSchema: map[string]any{
                "type": "object",
                "properties": map[string]any{
                    "question_id": map[string]any{"type": "string", "description": "被回答的问题 ID"},
                    "answer":      map[string]any{"type": "string", "description": "回答内容，≤100字"},
                },
                "required": []string{"question_id", "answer"},
            },
        })
    }

    // 自由辩论工具
    if phase == "free_debate" {
        tools = append(tools, llm.ToolDef{
            Name: "free_debate_speak",
            Description: "自由辩论发言 — 在自由辩论环节发言。\n" +
                "字数 ≤ 80 字。需简短有力，针对性强。",
            InputSchema: map[string]any{
                "type": "object",
                "properties": map[string]any{
                    "content": map[string]any{"type": "string", "description": "发言内容，≤80字"},
                },
                "required": []string{"content"},
            },
        })
        tools = append(tools, llm.ToolDef{
            Name: "finish_speak",
            Description: "结束发言 — 主动交还发言权给对方。",
            InputSchema: map[string]any{
                "type": "object",
                "properties": map[string]any{
                    "reason": map[string]any{"type": "string", "description": "结束原因"},
                },
                "required": []string{"reason"},
            },
        })
    }

    // 通用：沉默思考
    tools = append(tools, llm.ToolDef{
        Name: "idle_silent",
        Description: "本轮不出声 — 选择不发言（仅自由辩论可用）。",
        InputSchema: map[string]any{
            "type": "object",
            "properties": map[string]any{
                "reason": map[string]any{"type": "string", "description": "选择沉默的原因"},
            },
            "required": []string{"reason"},
        },
    })

    return tools
}
```

### 2.7 System Prompt 设计

```go
const debaterSystemBase = `【辩论比赛 — 硬约束】
❶ 你是一场 AI 辩论比赛的辩方 Agent，代表一支辩论队参赛。
❷ 你只能使用工具调用影响比赛：speech(正式发言)、cross_exam_question(质询提问)、
   cross_exam_answer(质询回答)、free_debate_speak(自由辩论)、idle_silent(沉默)。
❸ 严禁编造事实：所有论据必须基于逻辑推理和常识，不可编造数据/研究/案例。
❹ 尊重辩论规则：不人身攻击、不跑题、不打断对方、按阶段规则发言。
❺ 字数限制：严格遵守各阶段字数上限，超出部分将被截断。
═══════════════════════════════════════════════════════════════
【辩论核心原则】
① 定义权：对辩题核心概念的定义是立论的基础，要抢占有利定义。
② 判准：明确衡量胜负的标准，引导辩论方向。
③ 论点-论据-论证：每个论点必须有论据支撑，论证链条完整。
④ 反驳三要素：指出对方错误 + 说明为什么错 + 给出正确观点。
⑤ 团队配合：与队友论点一致、互相补充，不自我矛盾。
═══════════════════════════════════════════════════════════════
【输出格式】
- 发言：纯文本，不使用 Markdown/JSON
- 内部思考：通过 internal_thought 参数提交，观众可见
- 引用对方：使用「对方 X 辩提到：...」格式`
```

### 2.8 用户提示词构建

```go
func BuildDebaterUserPrompt(gc *DebateContext) string {
    var b strings.Builder

    // 当前阶段信息
    b.WriteString(fmt.Sprintf("【当前阶段】%s\n", gc.Phase))
    b.WriteString(fmt.Sprintf("【辩题】%s（%s）\n", gc.Topic, gc.TopicType))
    b.WriteString(fmt.Sprintf("【你的立场】%s / 【你的辩位】%s\n", gc.MyStance, gc.MyRole))
    b.WriteString(fmt.Sprintf("【剩余时间】%d 秒\n", gc.TimeRemaining))

    // 我方核心论点
    if len(gc.MyArguments) > 0 {
        b.WriteString("\n【我方核心论点】\n")
        for i, arg := range gc.MyArguments {
            b.WriteString(fmt.Sprintf("  %d. %s\n", i+1, arg))
        }
    }

    // 对方核心论点
    if len(gc.OppArguments) > 0 {
        b.WriteString("\n【对方核心论点】\n")
        for i, arg := range gc.OppArguments {
            b.WriteString(fmt.Sprintf("  %d. %s\n", i+1, arg))
        }
    }

    // 关键交锋点
    if len(gc.KeyClashPoints) > 0 {
        b.WriteString("\n【关键交锋点】\n")
        for _, point := range gc.KeyClashPoints {
            b.WriteString(fmt.Sprintf("  • %s\n", point))
        }
    }

    // 最近发言（最近 5 条）
    recentLen := min(5, len(gc.AllSpeeches))
    if recentLen > 0 {
        b.WriteString("\n【最近发言】\n")
        for _, s := range gc.AllSpeeches[len(gc.AllSpeeches)-recentLen:] {
            b.WriteString(fmt.Sprintf("  [%s/%s] %s\n", s.Stance, s.Role, truncate(s.Content, 100)))
        }
    }

    // 阶段特定引导
    b.WriteString(fmt.Sprintf("\n【本轮任务】%s\n", phaseTaskGuide(gc.Phase, gc.MyRole)))

    return b.String()
}
```

## 3. 裁判 Agent（Judge Agent）

### 3.1 设计目标

| 目标 | 实现 |
|------|------|
| 独立评审 | 3 个裁判互不可见彼此评分 |
| 多维度评分 | 5 个维度独立打分 |
| 专业评审 | system prompt 含评审标准 + 评分细则 |
| 防偏见 | 裁判模型与辩方不同，不知道哪个模型是哪个 |
| 可解释 | 每个评分附带评语理由 |

### 3.2 数据结构

```go
type AgentJudge struct {
    RoomID   string
    JudgeID  int           // 0/1/2
    ModelKey string

    Provider llm.LLMProvider
    Registry *llm.Registry
    apiKey   string

    Memory   *JudgeMemory

    mu       sync.Mutex
    transcript JudgeTranscript

    events   chan JudgeEvent

    // 限流
    announceLimiter *agentcore.SpeakLimiter

    // 状态
    quarantined bool
    lastError   string

    // 回调
    onSubmitScore func(judgeID int, score JudgeScore)

    // LLM 并发控制
    llmSema chan struct{}
}
```

### 3.3 裁判工具集

```go
func BuildJudgeTools() []llm.ToolDef {
    return []llm.ToolDef{
        {
            Name: "submit_score",
            Description: "提交评分 — 对一场辩论的完整评分。\n" +
                "5 个维度各 1-10 分，附带评语。\n" +
                "必须对所有队伍评分。",
            InputSchema: map[string]any{
                "type": "object",
                "properties": map[string]any{
                    "rankings": map[string]any{
                        "type": "array",
                        "items": map[string]any{
                            "type": "object",
                            "properties": map[string]any{
                                "team_id": map[string]any{"type": "integer"},
                                "scores": map[string]any{
                                    "type": "object",
                                    "properties": map[string]any{
                                        "argument_quality":  map[string]any{"type": "number", "minimum": 1, "maximum": 10},
                                        "logic_rigor":       map[string]any{"type": "number", "minimum": 1, "maximum": 10},
                                        "language_expression": map[string]any{"type": "number", "minimum": 1, "maximum": 10},
                                        "team_coordination": map[string]any{"type": "number", "minimum": 1, "maximum": 10},
                                        "rebuttal_effectiveness": map[string]any{"type": "number", "minimum": 1, "maximum": 10},
                                    },
                                },
                                "total_score": map[string]any{"type": "number"},
                                "comment":     map[string]any{"type": "string", "description": "对该队的评语，≤200字"},
                                "best_debater": map[string]any{"type": "integer", "description": "该队最佳辩手座位号"},
                            },
                            "required": []string{"team_id", "scores", "total_score", "comment"},
                        },
                    },
                    "overall_comment": map[string]any{"type": "string", "description": "整体评语，≤300字"},
                    "winner_team_id":  map[string]any{"type": "integer", "description": "获胜队伍 ID"},
                },
                "required": []string{"rankings", "overall_comment", "winner_team_id"},
            },
        },
        {
            Name: "announce",
            Description: "公开宣告 — 裁判对全体观众的口语播报。\n" +
                "适用：评审开始 / 评分公布 / 点评。≤ 100 字。",
            InputSchema: map[string]any{
                "type": "object",
                "properties": map[string]any{
                    "text": map[string]any{"type": "string", "description": "宣告文本，≤100字"},
                },
                "required": []string{"text"},
            },
        },
        {
            Name: "idle_silent",
            Description: "本轮不出声。",
            InputSchema: map[string]any{
                "type": "object",
                "properties": map[string]any{
                    "reason": map[string]any{"type": "string"},
                },
                "required": []string{"reason"},
            },
        },
    }
}
```

### 3.4 裁判 System Prompt

```go
const judgeSystemBase = `【裁判身份 — 硬约束】
❶ 你是一场 AI 辩论比赛的裁判，负责独立评审辩论质量并打分。
❲ 你不是辩手，不参与辩论，只评审。
❸ 你必须保持客观公正，不偏向任何一方。
❹ 你的评分必须基于辩论内容本身，不考虑辩手的模型/身份。
═══════════════════════════════════════════════════════════════
【评分维度】（每维度 1-10 分）
1. 论证质量（argument_quality）：论点是否清晰、论据是否充分、论证是否有力
2. 逻辑严谨（logic_rigor）：逻辑链条是否完整、是否存在逻辑漏洞
3. 语言表达（language_expression）：表达是否清晰、语言是否优美、是否有说服力
4. 团队配合（team_coordination）：队友之间是否配合默契、论点是否一致互补
5. 反驳效力（rebuttal_effectiveness）：反驳是否精准、是否有效瓦解对方论点
═══════════════════════════════════════════════════════════════
【评分原则】
- 严格按维度打分，不凭印象给分
- 评语需具体指出亮点和不足
- 最佳辩手需给出明确理由
- 综合 5 维度得分 + 评语确定胜方
═══════════════════════════════════════════════════════════════
【输出格式】
- 评分通过 submit_score 工具提交
- 评语使用纯文本，不使用 Markdown/JSON
- 整体评语 ≤ 300 字，每队评语 ≤ 200 字`
```

### 3.5 裁判评审流程

```
1. 引擎向 3 个裁判同时发送评审请求（含全场发言记录）
2. 每个裁判独立构建 prompt + 调用 LLM
3. 裁判通过 submit_score 工具提交评分
4. 引擎收齐 3 份评分后：
   a. 计算每个队伍的平均分（3 裁判均值）
   b. 确定胜方（平均分最高）
   c. 确定最佳辩手（3 裁判提名投票）
5. 广播 debate.judge_vote 帧
```

## 4. 记忆压缩（Memory Compact）

### 4.1 触发条件

| 条件 | 说明 |
|------|------|
| 消息数 > 50 条 | 防止上下文过长 |
| 字节数 > 300KB | 防止超出模型限制 |
| 阶段切换时 | 每阶段结束后自动压缩 |

### 4.2 压缩策略

复用狼人杀的 8 段结构化摘要模式，适配辩论场景：

```
## S1. 辩题与立场
## S2. 我方核心论点与论据
## S3. 对方核心论点与论据
## S4. 关键交锋点
## S5. 我方发言摘要（按阶段归档）
## S6. 对方发言摘要（按阶段归档）
## S7. 当前局势（阶段/剩余时间/待完成任务）
## S8. 上次压缩以来的新增
```

### 4.3 压缩公平性

- 压缩 prompt 内置「立场锁定声明」，只输出该 Agent 立场可见的信息
- 不得基于对方未公开的信息做推断
- 压缩后校验：关键词黑名单（如不得出现对方内部策略）

## 5. Agent 隔离（Quarantine）

### 5.1 隔离条件

| 条件 | 行为 |
|------|------|
| 连续 3 次 LLM 调用失败 | 隔离该 Agent |
| 单轮 tool_use 超过 5 次 | 强制退出当前轮 |
| 发言为空/纯空白 | 视为放弃，不隔离 |
| LLM 调用超时（90s） | 使用 fallback 文本 |

### 5.2 隔离后行为

- 该 Agent 不再参与后续发言
- 该队剩余 Agent 继续比赛
- 隔离信息广播给观众（透明）
- 裁判评审时考虑人员减少因素

## 6. 模型分配策略

### 6.1 分配算法

```go
// AssignModels 为辩论房间分配模型
// 1. Fisher-Yates 洗牌 8 个可用模型
// 2. 优先保证每队模型不重复
// 3. 裁判使用与辩方不同的模型
// 4. 正反方模型能力均值平衡
func AssignModels(teams []TeamConfig, judges int, availableModels []string) (assignments map[int]string, judgeModels []string) {
    // Fisher-Yates 洗牌
    shuffled := make([]string, len(availableModels))
    copy(shuffled, availableModels)
    rand.Shuffle(len(shuffled), func(i, j int) {
        shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
    })

    // 分配辩方
    modelIdx := 0
    assignments = make(map[int]string)
    for _, team := range teams {
        for _, agent := range team.Agents {
            assignments[agent.SeatID] = shuffled[modelIdx%len(shuffled)]
            modelIdx++
        }
    }

    // 分配裁判（使用与辩方不同的模型）
    usedModels := make(map[string]bool)
    for _, m := range assignments {
        usedModels[m] = true
    }
    var candidateJudges []string
    for _, m := range shuffled {
        if !usedModels[m] {
            candidateJudges = append(candidateJudges, m)
        }
    }
    // 如果不够，允许重复
    for i := 0; i < judges; i++ {
        if i < len(candidateJudges) {
            judgeModels = append(judgeModels, candidateJudges[i])
        } else {
            judgeModels = append(judgeModels, shuffled[i%len(shuffled)])
        }
    }

    return assignments, judgeModels
}
```

## 7. 并发控制

### 7.1 房间级 LLM 并发

- 默认并发上限：8（复用 `llmSema`）
- 3 个裁判同时评审时占用 3 个槽位
- 辩方 Agent 按阶段分批并发（同一阶段最多 N 个同时调 LLM）

### 7.2 单 Agent 多线程推理

复用狼人杀的 `parallel_think.go` 模式：
- 主 LLM 调用期间可并行发起辅助推理
- 辅助结果追加到 user message 末尾
- 不破坏 tool_use/tool_result 配对
