# Agent 猜疑链 — 现状分析与优化方向

> 创建日期：2026-08-21
> 归属：狼人杀 13 人局 Agent 升级
> 关联：`docs/狼人杀-Agent与系统/狼人杀Agent公屏猜疑化设计.md`（R132 设计文档）

---

## 一、什么是"猜疑链"

在经典狼人杀博弈论中，**猜疑链**（Suspicion Chain）是指：玩家 A 怀疑玩家 B，玩家 C 看到 A 的怀疑后对 B 产生新的判断，玩家 D 再根据 C 的反应调整自己的立场——如此链式传播，形成一张动态的「谁怀疑谁」的公开博弈网络。

在 AI Agent 狼人杀中，猜疑链不是单一数据结构，而是 **7 个子系统协作产生的涌现行为（emergent behavior）**。没有任何一个叫 `SuspicionChain` 的 struct——它是公屏猜疑化过滤、Prompt 博弈框架、工具约束、假说表、公开质疑、情绪系统、投票反馈这 7 个子系统共同驱动，在游戏过程中自然形成的。

---

## 二、7 个子系统详解

### 2.1 R132「公屏猜疑化」—— 基础设施（最关键）

**文件**：`ServerGo/game/werewolf/speak_mystery.go`（553 行）

**问题起源**：R125–R131 期间，旧版 `ScrubIdentityLeak` 把心理战发言整段替换为 `[已过滤]`，导致：
- 真人玩家看不到狼人悍跳预言家的完整发言，无法识破心理战
- Agent 无法学习有效表达策略，只能猜测什么会被过滤
- 数据库只保存过滤后版本，原始内容永久丢失
- 真人观战者看到"半句话"，无法理解上下文

**解决方案**：三种处理模式取代二元过滤：

| 模式 | 行为 | 适用场景 |
|---|---|---|
| `MysteryAllow` | 原文发出（心理战合法） | 身份自报、阵营叙事、公开点名、第三人称 |
| `MysteryDeferToGame` | 原文发出 + 风险反馈给 LLM | 隐晦身份暴露（"我用了药"、"我查了金水"） |
| `MysteryFuzzyIntent` | 系统改写为模糊版本 | 硬底线（0-indexed 座号、倒计时、工具元认知） |

**22+ 条正则规则**，按类别组织：

| 类别 | 示例 | 处理 |
|---|---|---|
| A. 身份自报 | "我是狼人/预言家/女巫..." | `MysteryAllow` |
| B. 阵营叙事 | "我们N个狼"/"狼队友" | `MysteryAllow` |
| C. 公开点名 | "X号是真预言家" | `MysteryAllow` |
| D. 公私聊 | "我查验了X号" | `MysteryAllow` |
| E. 第三人称 | "他是真预言家" | `MysteryAllow` |
| F. 隐晦身份暴露 | "我用了药自救" | `MysteryDeferToGame` |
| H. 极端硬底线 | 0-indexed 座号、剩余秒数 | `MysteryFuzzyIntent` |

**接入点**：所有 4 条公开发言路径都经过 `MysteryMaskText`：
- `Speak()`（`agent_runner.go:351-373`）
- `SpeakAuto()`（`agent_runner.go:568-581`）
- `SpeakWithThought()`（`agent_runner.go:691+`）
- `Interject()`（`agent_runner.go:653+`）

**LLM 反馈**：`ComposeMysteryHint()` 生成结构化风险提示，附加到 tool result 中，指导 LLM 下次用更铺垫化的表达替代直白暴露。

---

### 2.2 Prompt 博弈框架 —— 驱动猜疑的引擎

**文件**：`ServerGo/agent/wwplayer/prompt.go`

#### 系统 Prompt 中的博弈/心理战框架（`prompt.go:71-96`）

```
③ 整个房间是「充满猜忌、试探、博弈」的：
   别 Agent 可能悍跳预言家、装无辜、装神职、放烟雾弹；
   真人玩家可能故意钓鱼、反串、说反话。
   任何一句「我是 X」都不一定是真的 — 你要串联全轮证据再升/降信任度。

④ 心理战合法化(R132 起)：你的发言原文会被玩家完整看到。
   这意味着你可以悍跳预言家、可以装女巫/猎人、可以放任何心理战
   ——只要你的「证据链表演」能撑住。

⑦ 反事实推理：在做出关键决策前，融入 2~3 条「如果 X 则 Y」的可能性路径。
   「如果 5 号是狼人，他第 2 轮的发言策略就说得通了」
```

#### User Prompt 中的博弈状态块（`prompt.go:641-647`）

```
• 你的发言完整广播给全房(含观战者)；他们会从你的发言 + 投票 + 行为全链证据反推你的身份。
• 别人说 '我是预言家 / 我是女巫' 不一定是真的 — 一律打 70% 折扣，
  等 ta 的查验/用药行动/全链证据再升/降信任度。
• 心理战合法：悍跳预言家、装好人、装神职、放烟雾弹都可玩。
```

#### 投票模式反馈（`prompt.go:648-672`）

```
【上一轮投票结果 — 谁投了谁】
→ 票型分析是最核心的推理素材：跟票者通常是同阵营，倒戈者大概率变节。
```

---

### 2.3 工具约束 —— 让猜疑"故事化"表达

**文件**：`ServerGo/agent/wwplayer/tools.go`

| 工具 | 猜疑功能 | 关键约束 |
|---|---|---|
| `speak` | 公开表达怀疑 | "❶ 严禁直接说「X号是狼人」❷ 必须用「基于行为+反事实+分段叙述」包装 ❸ 留反转余地" |
| `speak_with_thought` | 公开 text + 私人 internal_thought | text=公开发言（可以是谎言）；internal_thought=真实怀疑 |
| `interject` | 非发言轮追问/质疑 | "追问上一位的矛盾…推动自己的怀疑：「我倾向X是悍跳，有谁同感？」" |
| `last_words` | 遗言指认怀疑对象 | "指认你最怀疑的人，给出后续归票建议" |
| `chat_recall` | 回溯早期发言构建怀疑链 | "R1谁跳过预言家"、"3号之前投过谁" |

**工具层面的"故事化包装"硬约束**：Agent 不能直接说"X号是狼人"，必须用行为描述包装：
- ✅ "X号发言节奏像悍跳"
- ✅ "X号票型异常，跟了3号的节奏"
- ✅ "如果X是预言家，为什么他不先查Y那个明显可疑的人？"
- ❌ "X号是狼人"（会被 `ScrubIdentityLeak` 过滤）

---

### 2.4 假说表（HypothesisTable）—— 持久化怀疑状态

**文件**：`ServerGo/game/werewolf/hypothesis_tracker.go`（156 行）

**解决的根本问题**：LLM 没有跨轮持久化机制。每次 LLM 调用是独立的，没有内置的"记住上一轮我怀疑谁"能力。假说表让怀疑状态跨轮存活。

#### 数据结构

```go
type HypothesisEntry struct {
    TargetSeat int    `json:"target_seat"`  // 被怀疑的玩家
    RoleGuess  string `json:"role_guess"`   // werewolf/seer/witch/.../unknown
    Confidence int    `json:"confidence"`   // 0~100
    Supporting string `json:"supporting"`   // ≤40字支撑依据
    Refuting   string `json:"refuting"`     // ≤40字反证
    UpdatedAt  int64  `json:"updated_at"`   // UnixMilli
}

type HypothesisTable struct {
    Seat      int               `json:"seat"`
    Entries   []HypothesisEntry `json:"entries"`
    Round     int               `json:"round"`
    UpdatedAt int64             `json:"updated_at"`
}
```

#### 持久化机制

LLM 在 `LastDecisionSummary` 末尾追加 `📊 [...]` JSON 段：
```
📊 [{"target_seat":2,"role_guess":"werewolf","confidence":72,
     "supporting":"R2票型倒戈","refuting":"R3替9号辩护","updated_at":1724000000000}]
```

引擎通过正则 `hypothesisSummaryRe` 解析，写入 `HypothesisStore`——无需额外 LLM 调用（§128"对话即思考"）。

#### 渲染到 Prompt（`prompt.go:1045-1081`）

```
【📊 你的当前假说(第 N 天)】
- 5号 → werewolf (72%) 支撑:R2 票型倒戈 / 反证:R3 替9号辩护
- 8号 → seer (45%) 支撑:R1 查杀播报 / 反证:票型跟随狼队
```

#### 隔离规则

- §119 协议层隔离：假说表**不**写入 `chat_message` / `chat_history`
- §135 spectator 隔离：玩家侧 `BotTranscript.HypothesisSummary` 被清空，只有 spectator 可见
- 容量上限：`hypothesisStoreCap = 230`（13人局 × 12目标 × 1.5 富余）

---

### 2.5 公开质疑（Challenge）系统

**文件**：`ServerGo/game/werewolf/room_action.go:178-189`

- 任何存活玩家可在白天发言阶段公开质疑另一玩家（每天限 1 次，不可自质疑）
- 被质疑者收到高优先级 Prompt 注入：`【公开质疑】X号在白天发言阶段公开质疑你：<question>`
- 质疑行为通过 `ActivityEventKindChallenge` 广播给所有玩家——**质疑行为本身就是猜疑链的一个显式节点**
- 被质疑时不回应会显得心虚（Prompt 中标注为 High 优先级）

---

### 2.6 情绪系统 —— 情绪化的猜疑表达

**文件**：`ServerGo/agent/wwplayer/emotion.go`（554 行）

10 类情绪中与猜疑直接相关的：

| 情绪 | 对猜疑表达的影响 |
|---|---|
| `wary`（疑虑警惕） | "发言以质疑为主，反复追问细节，对他人表述持保留态度" |
| `panic`（紧张恐慌） | "逻辑断层、发言卡顿"——容易被怀疑 |
| `guilty`（心虚愧疚） | "发言卡顿、回避关键问题"——狼人撒谎信号 |
| `irritated`（恼怒急躁） | "语气强硬带攻击性，会指责其他玩家"——激进怀疑 |
| `confused`（困惑茫然） | "多人对跳身份真假难辨"——无法形成判断 |

**情绪对猜疑链的影响**：
- `wary` 情绪的 Agent 发言天然带有怀疑色彩，会触发其他 Agent 的防御性反应
- `panic` 情绪的 Agent 容易被集体怀疑，形成"围攻"式猜疑链
- 情绪状态本身是公开信息（写入 BotTranscript），一个 `wary` 徽章本身就是猜疑链的可见信号

**人格维度**：`TrustTendency`（信任倾向 0=多疑, 1=轻信），影响 Agent 对他人发言的初始信任度。

---

### 2.7 投票模式反馈循环

- `LastDayVoteMap` 注入下一轮 Prompt：谁投了谁（`prompt.go:648-672`）
- `public_vote_suspicion`（`win_predict.go`）：投票集中度 = 量化集体怀疑
- 投票结果 → 下一轮 Prompt → 新的猜疑表达 → 新的投票 → 循环

---

## 三、猜疑链的完整数据流

```
┌─────────────────────────────────────────────────────────────────┐
│                     游戏事件触发（发言轮次）                      │
└──────────────────────────┬──────────────────────────────────────┘
                           ▼
┌─────────────────────────────────────────────────────────────────┐
│  handleEvent() → BuildUserPrompt() 构建游戏状态                  │
│  包含：RecentSpeeches / LastDayVoteMap / HypothesisTable /       │
│        Emotion / LastChallenge / ChatHistory(500K)               │
└──────────────────────────┬──────────────────────────────────────┘
                           ▼
┌─────────────────────────────────────────────────────────────────┐
│  LLM 调用（system + tools + messages）                           │
│  模型返回 tool_use: speak_with_thought                           │
│    text: "3号跳预言家的速度像悍跳"（公开怀疑）                    │
│    internal_thought: "我是预言家，3号是狼人"（真实想法）          │
└──────────────────────────┬──────────────────────────────────────┘
                           ▼
┌─────────────────────────────────────────────────────────────────┐
│  DispatchTool → SpeakWithThought(cleaned, thought)               │
│    ├─ publicText → MysteryMaskText() → chatSvc.SendFromBot()    │
│    │   → 广播给所有玩家（猜疑链的公开传播）                      │
│    └─ internalThought → agent.RecordLastThought()                │
│        → 仅写入 BotTranscript.HeartThought（物理隔离）          │
└──────────────────────────┬──────────────────────────────────────┘
                           ▼
┌─────────────────────────────────────────────────────────────────┐
│  其他 Agent 在下一轮 GameContext 中看到：                         │
│    ├─ RecentSpeeches（公开发言记录）                             │
│    ├─ LastDayVoteMap（投票模式）                                 │
│    ├─ LastChallenge（公开质疑）                                  │
│    └─ HypothesisTable（自己的假说表）                            │
│  → 更新自己的怀疑 → 再次表达 → 链式传播                         │
└─────────────────────────────────────────────────────────────────┘
```

---

## 四、当前实现的优势

1. **物理隔离保证公平性**：`internalThought` 永远不进入公屏，玩家只能通过公开证据推理
2. **故事化包装提升可读性**：Agent 不能直接贴标签，必须用行为描述，让发言更像真人
3. **情绪化表达增加博弈深度**：不同情绪下的怀疑表达风格不同，增加了判断难度
4. **假说表提供跨轮记忆**：解决了 LLM 无状态的核心缺陷
5. **投票反馈闭环**：票型分析是最强的推理素材，形成自我强化的博弈循环
6. **公开质疑制造冲突节点**：显式的质疑行为让猜疑链有了可追踪的锚点

---

## 五、当前实现的不足

### 5.1 假说表依赖 LLM 自觉输出

当前假说表的更新完全依赖 LLM 在 `LastDecisionSummary` 末尾追加 `📊 [...]` JSON 段。如果 LLM 忘记输出或输出格式错误，假说表就不会更新。虽然有 `hypothesisSummaryRe` 容错（静默丢弃），但没有补偿机制。

### 5.2 猜疑链缺乏全局视图

每个 Agent 只能看到自己的 `HypothesisTable` 和公屏发言，无法看到其他 Agent 的假说表。这意味着：
- Agent 无法知道"谁在怀疑我"
- 无法形成"我知道你在怀疑我，所以我调整策略"的高阶博弈
- 观战者虽然能看到所有 Agent 的假说表，但没有聚合视图

### 5.3 情绪系统与猜疑链的耦合不够深

当前情绪切换是 LLM 通过 `emotion_switch_speak` 工具自主决定的，但：
- 情绪对猜疑表达的影响仅限于 Prompt 中的风格描述，没有硬性约束
- Agent 可能在 `wary` 情绪下依然做出激进的怀疑行为（Prompt 约束不够强）
- 情绪触发条件没有与猜疑链事件（被质疑、票型异常等）自动关联

### 5.4 公开质疑机制使用率低

`Challenge` 系统虽然实现完整，但 Agent 对其使用不够主动：
- 每天限 1 次的限制合理，但 Prompt 中对何时使用质疑的指导不够具体
- 被质疑者的回应缺乏强制性（可以完全忽略）
- 质疑链的传播效果依赖其他 Agent 主动在发言中引用

### 5.5 猜疑链缺乏"信息污染"机制

当前所有公屏信息都是"可信的"，没有"谣言"或"误导信息"的概念：
- Agent 无法故意传播假信息来污染对手的推理
- 没有"信息衰减"机制（早期信息的权重不会随时间降低）
- 缺乏"信息来源可信度"的概念（谁说的比说了什么更重要）

### 5.6 猜疑链的"断裂"处理

当一个 Agent 被投票出局后，其猜疑链应该有"遗产效应"（遗言指认）：
- `last_words` 工具支持指认怀疑对象，但仅限于死亡时触发
- 存活 Agent 缺乏"继承"已死 Agent 推理的机制
- 没有"遗产分析"——根据死者身份验证其生前怀疑的准确性

---

## 六、未来优化方向

### 6.1 假说表自动推断（优先级：高）

**目标**：减少 LLM 自觉输出假说表的依赖，增加引擎侧自动推断。

**方案**：
- 基于 Agent 的公开发言（`LastSpeech`）、投票目标（`LastDayVoteMap`）、情绪状态自动推断基础假说
- LLM 的 `📊 [...]` 输出作为"精炼"覆盖，引擎推断作为"兜底"
- 新增 `HypothesisAutoInfer()` 函数，解析发言中的关键词（"怀疑"、"像悍跳"、"票型异常"等）生成初始假说

### 6.2 猜疑链全局视图（优先级：高）

**目标**：为观战者提供完整的猜疑链可视化。

**方案**：
- 新增 `SuspicionChainSnapshot` 结构，聚合所有 Agent 的 `HypothesisTable`
- 在 `view.go` 中新增 `SuspicionChainView`，生成"谁怀疑谁"的有向图
- 前端新增"猜疑链"Tab，展示：节点=玩家，边=怀疑关系，权重=置信度，颜色=情绪状态
- 支持时间轴回放：按轮次展示猜疑链的演化过程

### 6.3 信息污染与谣言系统（优先级：中）

**目标**：增加"假信息"维度，让猜疑链更接近真人博弈。

**方案**：
- 实现 `RumorGraph`（参考 `docs/狼人杀-Agent与系统/Agent升级/信息账本与行为追踪/信息污染链RumorGraph与跨局声誉.md`）
- 新增 `rumor` 工具：Agent 可以主动传播可疑信息
- 引入"信息可信度"衰减：`veracity = base_trust * decay_factor^hop_count`
- 公屏分为"公开信息"和"私下流言"两个通道

### 6.4 情绪自动触发机制（优先级：中）

**目标**：让情绪切换与猜疑链事件自动关联，减少 LLM 自主决策的随机性。

**方案**：
- 被公开质疑 → 自动触发 `panic` 或 `irritated`（概率性）
- 投票结果中自己被多人投 → 触发 `panic`
- 成功投出狼人 → 触发 `excited`
- 连续两轮被抗推 → 触发 `grievance`
- 新增 `autoEmotionTrigger()` 函数，在 Agent 事件处理前自动评估

### 6.5 遗产继承机制（优先级：中）

**目标**：让已死 Agent 的猜疑链推理能被存活 Agent 继承。

**方案**：
- 死者遗言中的怀疑对象自动注入存活 Agent 的 Prompt
- 新增 `LegacySuspicion` 结构：`{FromSeat, ToSeat, RoleGuess, Confidence, Cause}`
- 当死者身份被验证（如猎人开枪带走狼人），其生前怀疑的其他玩家自动获得"可信度加成"
- 反之，如果死者是狼人，其生前怀疑的对象自动获得"可信度折扣"

### 6.6 高阶博弈：反猜疑（优先级：低）

**目标**：让 Agent 能够主动操控猜疑链，实现更高级的欺骗。

**方案**：
- 新增 `redirect_suspicion` 工具：Agent 可以主动引导其他 Agent 怀疑特定目标
- 新增 `deflect` 工具：被怀疑时主动转移怀疑方向
- 在 Prompt 中增加"反猜疑"策略指导："如果你是狼人，可以用 interject 把怀疑引向好人"
- 引入"反间计"：Agent 可以假装同意对手的怀疑，然后在关键时刻反转

### 6.7 跨局声誉系统（优先级：低）

**目标**：让 Agent 在多局游戏中积累"信誉"，影响后续对局的初始信任度。

**方案**：
- 实现 `CrossGameReputation` 结构：记录每个 Agent 在历史对局中的表现
- 信誉维度：发言可信度（说的是否是真的）、推理准确率（怀疑的是否是狼人）、投票一致率
- 新对局开始时，高信誉 Agent 的发言获得更高的初始信任权重
- 参考 `docs/狼人杀-Agent与系统/Agent升级/信息账本与行为追踪/信息污染链RumorGraph与跨局声誉.md`

---

## 七、关键文件索引

| 关注点 | 文件路径 |
|---|---|
| R132 公屏猜疑化设计 | `docs/狼人杀-Agent与系统/狼人杀Agent公屏猜疑化设计.md` |
| 言论过滤实现 | `ServerGo/game/werewolf/speak_mystery.go` |
| 过滤接入 | `ServerGo/game/werewolf/agent_runner.go:351-399` |
| Prompt 博弈框架 | `ServerGo/agent/wwplayer/prompt.go:71-96` |
| 工具定义 | `ServerGo/agent/wwplayer/tools.go:385-516` |
| 假说表存储 | `ServerGo/game/werewolf/hypothesis_tracker.go` |
| 情绪系统 | `ServerGo/agent/wwplayer/emotion.go` |
| 公开质疑 | `ServerGo/game/werewolf/room_action.go:178-189` |
| 聊天广播 | `ServerGo/ws/chat_service.go:821-874` |
| 狼人概率热力图 | `ServerGo/game/werewolf/win_predict.go` |
| 假说表 Prompt 渲染 | `ServerGo/agent/wwplayer/prompt.go:1045-1081` |
| 信任矩阵设计 | `docs/狼人杀-Agent与系统/Agent升级/Agent推理与战术博弈/多假说并行推演与信任矩阵.md` |
| 信息污染链设计 | `docs/狼人杀-Agent与系统/Agent升级/信息账本与行为追踪/信息污染链RumorGraph与跨局声誉.md` |
| Agent 设计总览 | `docs/狼人杀-Agent与系统/狼人杀Agent设计.md` |

---

## 八、总结

狼人杀 Agent 的猜疑链是一个 **涌现行为**，由 7 个子系统协作驱动：

1. **R132 公屏猜疑化** 让心理战发言可见（基础）
2. **Prompt 博弈框架** 驱动 Agent 主动怀疑和推理（引擎）
3. **工具约束** 让怀疑表达"故事化"（表达层）
4. **假说表** 提供跨轮持久化记忆（记忆层）
5. **公开质疑** 制造显式的冲突节点（事件层）
6. **情绪系统** 让怀疑表达带有情感色彩（风格层）
7. **投票反馈** 形成自我强化的博弈循环（闭环）

当前实现已经能够产生复杂的猜疑链行为（如 README 中描述的"Qwen 逻辑梳理 → 小米灭口推理 → GLM 稳健派"），但仍有优化空间：假说表自动推断、全局视图可视化、信息污染机制、情绪自动触发、遗产继承等方向可以进一步提升博弈深度。
