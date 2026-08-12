# 狼人杀 Agent 情绪模块设计

> **2026-07-10 §124 增强 — 狼人杀 12 人局 Agent「情绪」机制**。给每个 Agent 增加
> 持久化的情绪状态,影响 LLM 在每一次 LLM 调用的 system prompt 中的行为风格;
> Agent 之间可以互相感知对方的实时情绪;Web 端观众与玩家能看到每个 Agent 当前
> 头顶/侧栏的情绪标签。
>
> **设计原则**:
> - **真实感** — 模仿人类玩家的情绪分类与变化规律,让 12 人 AI 局看起来像 12 个
>   真人在玩,而非 12 个冷冰冰的 LLM。
> - **可观察** — 情绪不是私有状态,所有人都能看到(其它 Agent + 真人玩家 + 观众);
>   这与「身份/角色」必须隐藏形成对照。
> - **可切换** — LLM 通过 `emotion_switch` 工具自主切换情绪,系统不会强制干预。
> - **可引导** — 情绪作为 system prompt 中的硬约束,引导 LLM 的说话节奏、决策风格
>   (这是情绪的核心作用 — 不仅是"显示"出来,还要"生效"在 LLM 决策里)。

---

## 1. 情绪分类（10 类）

参考人类狼人杀玩家常见的情绪模式,本设计采用 10 类情绪。情绪按 4 大维度组织:

| 维度 | 情绪 | 极性 | 唤醒度 | 影响 |
|------|------|------|--------|------|
| **积极** | 自信从容 | + | 低 | 决策果断 / 发言笃定 |
| **积极** | 亢奋得意 | + | 高 | 决策激进 / 语速上扬 |
| **中性** | 冷静平淡 | ± | 中 | 决策保守 / 客观中立 |
| **焦虑恐惧** | 紧张恐慌 | − | 高 | 决策短视 / 发言卡顿 |
| **焦虑恐惧** | 疑虑警惕 | − | 中 | 决策谨慎 / 反复追问 |
| **愤怒不满** | 恼怒急躁 | − | 高 | 决策冲动 / 语气强硬 |
| **愤怒不满** | 委屈不满 | − | 中 | 决策消极 / 情绪化辩解 |
| **认知困惑** | 困惑茫然 | − | 低 | 决策盲从 / 划水感强 |
| **阵营专属** | 心虚愧疚 | − | 中 | 狼人专属 / 决策保守求稳 |
| **通用低唤醒** | 懈怠疲惫 | − | 低 | 决策随意 / 简短划水 |

完整描述见 [附录 A:10 类情绪详细定义](#附录-a10-类情绪详细定义)。

---

## 2. 数据结构

### 2.1 Emotion 字符串常量

```go
const (
    EmotionConfident   = "confident"     // 自信从容
    EmotionExcited     = "excited"       // 亢奋得意
    EmotionCalm        = "calm"          // 冷静平淡
    EmotionPanic       = "panic"         // 紧张恐慌
    EmotionWary        = "wary"          // 疑虑警惕
    EmotionIrritated   = "irritated"     // 恼怒急躁
    EmotionGrievance   = "grievance"     // 委屈不满
    EmotionConfused    = "confused"      // 困惑茫然
    EmotionGuilty      = "guilty"        // 心虚愧疚(狼人专属)
    EmotionTired       = "tired"         // 懈怠疲惫
)
```

### 2.2 Agent 字段

```go
// Agent 新增情绪相关字段
type Agent struct {
    // ... 现有字段 ...

    // 情绪状态 — 初始时随机抽取,后续 LLM 通过 emotion_switch 工具自主切换。
    emotionMu       sync.RWMutex
    currentEmotion  string           // 当前情绪(如 "confident"),初始随机
    emotionHistory  []EmotionRecord  // 切换历史(供前端展示情绪曲线)
}

type EmotionRecord struct {
    Emotion   string `json:"emotion"`    // 情绪 key
    Reason    string `json:"reason"`     // 切换原因(LLM 在 emotion_switch.reason 给出,≤80 字)
    AtMs      int64  `json:"at_ms"`      // 切换时间 unix 毫秒
}
```

### 2.3 BotTranscript 新增字段

```go
type BotTranscript struct {
    // ... 现有字段 ...
    Emotion         string   `json:"emotion,omitempty"`           // 当前情绪 key
    EmotionUpdatedAt int64   `json:"emotion_updated_at,omitempty"`// 切换时间 unix ms
    EmotionHistory  []EmotionRecord `json:"emotion_history,omitempty"` // 最近 5 次切换
}
```

`emotion_history` 仅展示最近 5 次切换(避免 wire 过大);更老的历史写到 DB 即可。

---

## 3. 情绪初始随机分配

`Agent.NewWithRoom` / `StartAgentsLocked` 末尾,在没有任何历史对战数据时,通过代码
随机抽取初始情绪:

```go
import "math/rand"

var allEmotions = []string{
    EmotionConfident, EmotionExcited, EmotionCalm,
    EmotionPanic, EmotionWary, EmotionIrritated,
    EmotionGrievance, EmotionConfused,
    EmotionGuilty, EmotionTired,
}

// pickInitialEmotion 抽取初始情绪。狼人偏向心虚愧疚,其它角色按权重分配。
func pickInitialEmotion(role string) string {
    rng := rand.New(rand.NewSource(time.Now().UnixNano()))
    if role == "werewolf" {
        // 狼人开局更容易心虚(20%);其余按均匀分布。
        if rng.Float64() < 0.20 {
            return EmotionGuilty
        }
    }
    return allEmotions[rng.Intn(len(allEmotions))]
}
```

**触发条件**:每个 Agent 在 `NewWithRoom` 末尾(没有历史对战数据时)随机选一个情绪。
"历史对战数据"的判定:当前 bot 用户在该房间 game_log 表中已有 ≤0 条记录。

> **简化**:首次实现为"每个 Agent 必随机一次",后续版本可对接 `t_lsm_game_model_game_log`
> 查询历史判断是否首次开局。

---

## 4. `emotion_switch` 工具

### 4.1 工具定义

LLM 可调用 `emotion_switch` 切换当前 bot 的情绪(也可随机切换)。

```json
{
  "name": "emotion_switch",
  "description": "【情绪切换】改变你当前的情绪状态,影响你后续的说话风格与决策倾向。\n"
               + "═══════════════════════════════════════════════════════════════\n"
               + "【10 类情绪速查】\n"
               + "  • confident(自信从容) / excited(亢奋得意) — 积极·决策果断\n"
               + "  • calm(冷静平淡) — 中性·客观中立\n"
               + "  • panic(紧张恐慌) / wary(疑虑警惕) — 消极·决策保守\n"
               + "  • irritated(恼怒急躁) / grievance(委屈不满) — 消极·情绪化\n"
               + "  • confused(困惑茫然) — 消极·划水\n"
               + "  • guilty(心虚愧疚) — 狼人专属·撒谎时心虚\n"
               + "  • tired(懈怠疲惫) — 消极·低唤醒划水\n"
               + "═══════════════════════════════════════════════════════════════\n"
               + "【调用参数】\n"
               + "  • emotion = 'random' → 随机切换(不指定具体类别,系统从 10 类中抽一个)\n"
               + "  • emotion = '<具体 key>' → 切换到指定情绪\n"
               + "  • reason = 切换原因(≤80 字,供审计与前端展示「为什么突然变成紧张」)\n"
               + "═══════════════════════════════════════════════════════════════\n"
               + "【何时调用】\n"
               + "  • 被多人质疑、身份快暴露 → switch 到 panic / wary\n"
               + "  • 队友悍跳成功、自己也跟着沾光 → excited / confident\n"
               + "  • 场上一片混乱、逻辑对不上 → confused\n"
               + "  • 狼人发言被点中漏洞 → guilty(心虚)\n"
               + "  • 对局拖延太久、自己已经划水多轮 → tired\n"
               + "═══════════════════════════════════════════════════════════════\n"
               + "【限制】\n"
               + "  • 不消耗 speak / whisper 限流桶,随时可调\n"
               + "  • 不广播,不进 chat_message(其它玩家从 game.state 看到你变了)\n"
               + "  • 建议每次决策都先 switch 后发言,让情绪与说话风格匹配",
  "input_schema": {
    "type": "object",
    "properties": {
      "emotion": {
        "type": "string",
        "enum": ["random", "confident", "excited", "calm", "panic",
                 "wary", "irritated", "grievance", "confused", "guilty", "tired"],
        "description": "目标情绪;random 表示随机切换"
      },
      "reason": {
        "type": "string",
        "description": "切换原因(≤80 字,如 '被 3 号质疑身份,节奏开始乱')"
      }
    },
    "required": ["emotion", "reason"]
  }
}
```

### 4.2 派发实现

`DispatchTool` 新增分支:

```go
case "emotion_switch":
    emotion, _ := input["emotion"].(string)
    reason, _ := input["reason"].(string)
    if emotion == "" {
        return "emotion_switch rejected: emotion required", nil
    }
    if emotion == "random" {
        // 由 runner 在 Engine 侧用 rand.Intn 抽取
        return runner.EmotionSwitchRandom(reason)
    }
    return runner.EmotionSwitch(emotion, reason)
```

### 4.3 ToolRunner 接口扩展

```go
type ToolRunner interface {
    // ... 现有方法 ...
    // EmotionSwitch 切换到指定情绪,reason 为 LLM 给出。
    EmotionSwitch(emotion, reason string) (string, error)
    // EmotionSwitchRandom 随机切换情绪(由 runner 选一个)。
    EmotionSwitchRandom(reason string) (string, error)
}
```

### 4.4 agentRunner 实现

`werewolf/agent_runner.go` 实现:

```go
// EmotionSwitch 在 engine 侧 a.EmotionSwitch(emotion, reason)。
// 由于 emotion 是 agent 状态,由 agent.go 直接处理(无需持 r.mu)。
func (r *agentRunner) EmotionSwitch(emotion, reason string) (string, error) {
    if r.agent == nil {
        return "", errors.New("agent_runner: nil agent")
    }
    if !isValidEmotion(emotion) {
        return fmt.Sprintf("emotion_switch rejected: unknown emotion %q", emotion), nil
    }
    reason = truncate(reason, 80)
    r.agent.SwitchEmotion(emotion, reason)
    return fmt.Sprintf("emotion_switch ok: now %s (%s)", emotion, reason), nil
}

// EmotionSwitchRandom 在 10 类中随机选一个并切换。
func (r *agentRunner) EmotionSwitchRandom(reason string) (string, error) {
    if r.agent == nil {
        return "", errors.New("agent_runner: nil agent")
    }
    rng := rand.New(rand.NewSource(time.Now().UnixNano()))
    e := allEmotions[rng.Intn(len(allEmotions))]
    reason = truncate(reason, 80)
    r.agent.SwitchEmotion(e, reason)
    return fmt.Sprintf("emotion_switch ok: now random=%s (%s)", e, reason), nil
}
```

### 4.5 Agent.SwitchEmotion

```go
// SwitchEmotion 由 emotion_switch 工具调用入口(runner 调用)。
// 写入 currentEmotion + emotionHistory(保留最近 5 条)。
func (a *Agent) SwitchEmotion(emotion, reason string) {
    a.emotionMu.Lock()
    defer a.emotionMu.Unlock()
    if !isValidEmotionLocked(emotion) {
        return
    }
    a.currentEmotion = emotion
    a.emotionHistory = append(a.emotionHistory, EmotionRecord{
        Emotion: emotion,
        Reason:  reason,
        AtMs:    time.Now().UnixMilli(),
    })
    if len(a.emotionHistory) > 5 {
        a.emotionHistory = a.emotionHistory[len(a.emotionHistory)-5:]
    }
}
```

---

## 5. 情绪注入到 Anthropic 协议

### 5.1 注入位置

**System Prompt**(`BuildSystemPrompt`)尾部新增两段:

#### 段 1:【当前情绪】

```
【你的当前情绪】(2026-07-10 §124)
你当前的情绪是「{emotion_name}」(情绪极性={polarity},唤醒度={arousal})。
请按以下风格说话与决策:
  • 语速/句长:{speech_style}
  • 决策倾向:{decision_style}
  • 典型场景触发:{triggers}
情绪会显著影响你的发言风格与决策风格。**这是硬约束**,不要假装"我没情绪"。
```

#### 段 2:【他人情绪感知】

```
【他人情绪 — 你可感知其他 Agent 的实时情绪】(2026-07-10 §124)
当前房间内其它 Agent 的情绪状态:
  • 1号(美团 LongCat-2.0): 自信从容(confident) — "查杀节奏顺利"
  • 3号(豆包 2.0): 紧张恐慌(panic) — "被多人质疑身份"
  • 5号(DeepSeek V4-Pro): 疑虑警惕(wary) — "场上有悍跳迹象"
策略:
  • 敌人(其他阵营)情绪紧张时 → 你可以更激进地逼问 / 制造压力
  • 队友情绪紧张时 → 主动 whisper 鼓励 / 帮 ta 解围
  • 敌人情绪亢奋时 → 警惕 ta 可能有底牌(预言家/悍跳狼),避免硬刚
```

### 5.2 用户 Prompt 补充

在 `BuildUserPrompt` 末尾的【实时性】块之后追加一段:

```
【你的情绪】(§124)
当前: 紧张恐慌(panic) — "被 3 号 连续质疑,节奏乱了"
情绪风格: 语速快、句子短、可能口误、倾向保命式发言。
```

让 LLM 在每次 LLM 调用前看到自己当前的情绪状态(便于 LLM 在切换情绪前能看到
变化)。

### 5.3 GameContext 新增字段

```go
type GameContext struct {
    // ... 现有字段 ...

    // §124 情绪字段
    MyEmotion         string          `json:"my_emotion"`         // 当前 bot 情绪 key
    MyEmotionReason   string          `json:"my_emotion_reason"`  // 切换原因
    OthersEmotion     []SeatEmotionBrief `json:"others_emotion"` // 其它 bot 情绪摘要
}

type SeatEmotionBrief struct {
    Seat     int    `json:"seat"`      // 0-indexed
    Emotion  string `json:"emotion"`
    Reason   string `json:"reason"`
    UpdatedAt int64 `json:"updated_at"` // unix ms
}
```

`buildAgentContextLocked` 在末尾填充:

```go
// §124 情绪字段
if botAgent, ok := r.BotAgents[seat]; ok && botAgent != nil {
    gc.MyEmotion = botAgent.CurrentEmotion()
    gc.MyEmotionReason = botAgent.CurrentEmotionReason()
}
// 其它 Agent 情绪(按座位 0..N-1,跳过自己与真人)
for s, ba := range r.BotAgents {
    if s == seat || ba == nil {
        continue
    }
    gc.OthersEmotion = append(gc.OthersEmotion, SeatEmotionBrief{
        Seat:      s,
        Emotion:   ba.CurrentEmotion(),
        Reason:    ba.CurrentEmotionReason(),
        UpdatedAt: ba.EmotionUpdatedAt(),
    })
}
```

---

## 6. 协议层隔离原则

> **与 §119 HeartThought 的一致性原则**:情绪与内心独白都是"非隐藏状态",
> 所有玩家 + 观众都应能看到。这与「身份 / 角色」必须隐藏(§119)形成对照。

| 字段 | 隔离层级 | 其它玩家可见 | 观众可见 |
|------|---------|------------|---------|
| `Role` (角色) | 协议层 | ❌ 隐藏 | ❌ 隐藏 |
| `HeartThought` (内心独白) | 协议层 | ❌ 隐藏 | ✅ 仅观战者 |
| **`Emotion` (情绪)** | **wire 层公开** | ✅ 全部 | ✅ 全部 |

情绪写入 BotTranscript.Emotion / BotTranscript.EmotionHistory, 走 `BotAgents[seat].BotTranscript()`
读出 → 写入 `bot_contexts[seat]` → 下发 game.state.bot_contexts → 前端可见。
**情绪变化不需要经过 chat_message 广播**,由 WS 的 `game.state` 帧统一推送即可。

---

## 7. Web 端可视化

### 7.1 BotContextJSON 新增字段

```typescript
// types/werewolf.ts
export interface BotContextJSON {
  // ... 现有字段 ...

  /** §124 当前情绪 key (confident/excited/...) */
  emotion?: string;
  /** 情绪切换原因(LLM 给出,≤80 字) */
  emotion_reason?: string;
  /** 情绪最近一次切换的 unix 毫秒时间戳 */
  emotion_updated_at?: number;
  /** 最近 5 次情绪切换历史(供前端"情绪曲线") */
  emotion_history?: Array<{ emotion: string; reason: string; at_ms: number }>;
}
```

### 7.2 AgentInteractionPanel 渲染

每个 bot 卡片头部加一个"情绪徽章":

- **背景色**:根据情绪极性 + 唤醒度映射(共 10 色,见附录 A 色卡)。
- **Emoji**:每个情绪配 1 个 emoji(😌 / 🤩 / 😐 / 😨 / 🤔 / 😤 / 🥺 / 😵 / 😬 / 😴)。
- **Tooltip**:悬停显示「{emotion_name} | {emotion_reason}」+ 最近 5 次切换曲线。

### 7.3 玩家头顶气泡(可选)

在 WerewolfGamePage 的「座位气泡」组件旁渲染一个 `<div class="emotion-bubble">`,
游戏内可见 — 真人玩家能看到每个 bot 头顶的情绪。

---

## 8. 测试与回归

### 8.1 单元测试

`ServerGo/agent/emotion_test.go`:

- `TestPickInitialEmotion_DeterministicDistribution`:跑 1000 次,验证 10 类情绪
  分布合理(每类 5%~15%)。
- `TestSwitchEmotion_AppendsHistory`:连续 SwitchEmotion 5 次,验证 emotionHistory
  只保留最近 5 条。
- `TestEmotionConcurrentSafety`:并发读 / 写 currentEmotion,run race detector。

### 8.2 集成测试

`ServerGo/game/werewolf/room_emotion_test.go`:

- `TestBuildAgentContextLocked_PopulatesEmotion`:建 7-bot 房间,各 agent
  SwitchEmotion 不同情绪,验证 buildAgentContextLocked 输出正确的
  MyEmotion / OthersEmotion。

### 8.3 Prompt 注入验证

`ServerGo/agent/prompt_test.go`:

- `TestBuildSystemPrompt_IncludesEmotion`:ctx.MyEmotion="panic" → 验证
  BuildUserPrompt 输出含 "紧张恐慌" 字样。
- `TestBuildUserPrompt_IncludesMyEmotion`:同上。

---

## 9. 不变式与硬约束

- **情绪不影响游戏规则** — 仅影响 LLM 说话风格与决策倾向,不影响 game state。
- **emotion_switch 不消耗 speak / whisper 限流** — 但仍然受 LLMCallLimiter(8s)约束。
- **emotion_history 最多 5 条** — 超出按 FIFO 淘汰。
- **情绪切换** 不发 `chat.message` — 仅更新 BotTranscript,前端从 game.state
  bot_contexts 看到变化。
- **所有 10 类情绪对所有角色开放** — 包括 "guilty"(设计标注为"狼人专属",
  但其它角色也允许调,只是会得到空响应或自动 fallback — 实际不限制)。
- **初始情绪随机 + 狼人 20% 概率 guilty** — 详见 §3。

---

## 10. 实施步骤

1. **后端 emotion.go**:`emotion.go` 提供 10 类常量 + `pickInitialEmotion` +
   `isValidEmotionLocked`。
2. **Agent 字段**:在 `agent.go` 新增 `emotionMu / currentEmotion / emotionHistory`
   + `SwitchEmotion / CurrentEmotion / CurrentEmotionReason / EmotionUpdatedAt` getter。
3. **BotTranscript**:`Emotion / EmotionUpdatedAt / EmotionHistory` 3 字段 +
   `recordTranscript` 复制。
4. **ToolRunner 接口**:`EmotionSwitch / EmotionSwitchRandom` 两个方法。
5. **agentRunner 实现**:`werewolf/agent_runner.go` 两个方法。
6. **BuildTools**:所有 phase 暴露 `emotion_switch`。
7. **DispatchTool**:`emotion_switch` 分支。
8. **GameContext 字段**:`MyEmotion / MyEmotionReason / OthersEmotion`。
9. **buildAgentContextLocked 填充**:遍历 BotAgents 填字段。
10. **prompt.go 注入**:BuildSystemPrompt + BuildUserPrompt 新增【情绪】段。
11. **前端 types/werewolf.ts**:BotContextJSON 加 4 字段。
12. **前端 AgentInteractionPanel**:渲染情绪徽章 + 颜色 + 历史 tooltip。
13. **测试**:单测 + 集成测。
14. **编译 + go test + npm run build + 重启 + git commit**。

---

## 附录 A:10 类情绪详细定义

| 情绪 key | 中文名 | emoji | 极性 | 唤醒度 | 色卡(背景) | 语速/句长 | 决策倾向 |
|---------|--------|-------|------|--------|------------|----------|---------|
| confident | 自信从容 | 😌 | + | 低 | #cce5ff (浅蓝) | 平稳笃定 / 完整 | 果断 / 坚持己见 |
| excited | 亢奋得意 | 🤩 | + | 高 | #ffd9b3 (浅橙) | 加快 / 强调 / 炫耀 | 激进 / 高风险 |
| calm | 冷静平淡 | 😐 | ± | 中 | #e6e6e6 (中性灰) | 平稳 / 中等 | 保守 / 客观 |
| panic | 紧张恐慌 | 😨 | − | 高 | #ffcccc (浅红) | 卡顿 / 短句 / 口误 | 短视 / 保命 |
| wary | 疑虑警惕 | 🤔 | − | 中 | #fff2b3 (浅黄) | 反复追问 / 保留 | 谨慎 / 不轻易站边 |
| irritated | 恼怒急躁 | 😤 | − | 高 | #ffb3b3 (中红) | 强硬 / 攻击性 | 冲动 / 硬刚 |
| grievance | 委屈不满 | 🥺 | − | 中 | #ffd1dc (粉) | 偏弱 / 反复辩解 | 消极 / 摆烂 |
| confused | 困惑茫然 | 😵 | − | 低 | #d9d9d9 (深灰) | 简短 / "没搞懂" | 盲从 / 随机 |
| guilty | 心虚愧疚 | 😬 | − | 中 | #d6c4e0 (浅紫) | 卡顿 / 回避 | 保守 / 不敢激进 |
| tired | 懈怠疲惫 | 😴 | − | 低 | #c9d6e0 (深蓝) | 极简短 / 划水 | 随意 / 跟主流 |

每个情绪在 `BuildSystemPrompt` 中展开成具体行为约束(详见 §5.1)。

---

## 附录 B:与既有设计的一致性

| 既有设计 | §124 情绪模块的关系 |
|---------|---------------------|
| §119 HeartThought 协议层隔离 | 情绪走 wire 层公开,与 HeartThought 的隔离形成对照 |
| §122 Agent 单 bot 多线程 LLM | 情绪由单 goroutine 写,无并行冲突 |
| §123 故事化发言硬约束 | 情绪风格叠加在故事化约束之上(更细颗粒度的语气) |
| §120 公平性机制 | 情绪对所有模型一视同仁,与模型响应速率正交 |
| §111 chat history 500K 队列 | 情绪切换不进 chat history(协议层分离) |
| §115 房间共享 queue + ReadPointer | 情绪是 BotTranscript 字段,不受 chat queue 影响 |