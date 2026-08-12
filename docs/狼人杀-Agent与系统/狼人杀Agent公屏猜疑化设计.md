# 狼人杀 13 人局 Agent「公屏猜疑化」重构设计(R132)

> 本文对当前 agent 公屏发言的「整段替换为 `[已过滤]`」机制做完整剖析与重构设计。
> 触发背景:R125–R131 期间 observe 到 Bot 在公屏出现"虚张声势/玩心理战/故意混淆
> 视听"等真实狼人杀行为时,**整段被 ScrubIdentityLeak 替换为 "[已过滤]"**,
> 让真人观众看不到完整上下文,反而产生"奇怪为什么这句话是空的"的体验;
> **更糟的是**:真人玩家/真人观众无法识破对手在**玩心理战** — 因为他们永远
> 看不到"原文经过去敏感处理"的具体内容,无法利用"对手段子里有可疑宣言"
> 这一线索反推对方阵营。本文档给出 R132 重构方案:**从「禁词清洗」转向
> 「猜疑型公屏」**。
>
> 上游规则文档:[`docs/狼人杀13人标准局规则.md`](狼人杀13人标准局规则.md) /
> [`docs/狼人杀-Agent与系统/狼人杀Agent设计.md`](狼人杀Agent设计.md) /
> [`docs/狼人杀-Agent与系统/狼人杀对话即思考设计.md`](狼人杀对话即思考设计.md)。

---

## 一、问题分析:`[已过滤]` 整段替换的失败路径

### 1.1 当前机制概览

`ServerGo/game/werewolf/speak_filter.go::ScrubIdentityLeak` 在 `agentRunner.Speak`
/ `.SpeakAuto` / `.SpeakWithThought` / `.Interject` 四个公屏入口(broadcast 前)统一执行,
匹配 22+ 类高风险模板(自报身份 / 公开点名 / 狼队友披露 / 警徽流泄露 / LLM 元信息 / ...),
匹配命中后**整段替换**为 `"[已过滤]"`,保留前后其他文本。

22 条模式 anchor 表(摘自 `speak_filter.go` 与本仓库历次 R74/R91/R119/R122/R125 commits):

| 类别 | 引入版本 |
|------|----------|
| 自报身份 「我是狼人」「作为预言家」「我的身份是女巫」 | R74/R121 |
| 点名他人身份 「3 号是真预言家」「他是假猎人」 | R86/R119 |
| 狼队友披露 「我的同伙有 2、3、8」「3 号是狼队友」「我是 7 号狼同伴」 | R91/R119/R125 |
| 狼阵营人数 「我们三个狼都还在」 | R125 |
| 击杀意图/战术 「今晚先刀 5 号」 | R91 |
| 狼队黑话 养刀/倒钩/悍跳/垫飞/屠边 | R92 |
| 查验/警徽流泄露 「我昨晚查了 4 号是金水」「警徽流先摸 8 再压 5」 | R122 |
| LLM 元信息泄露 「我还剩 271 秒」「系统提示投票阶段」「让我用 idle 工具」 | R119/R121 |
| 0-indexed 座位号 「座位号 3」(暴露服务端内部编号) | R121 |
| 女巫用药自报 「我用了药自救」 | R121 |

(完整 22 条见 `speak_filter.go:45-203`。)

### 1.2 谁会看到 `[已过滤]`

| 角色 | 看到吗 | 看到的版本 |
|------|--------|-----------|
| **真人玩家(在座)** | ✅ 看到 | 原文 → `[已过滤]` 整段替换后的版本 |
| **真人观战者** | ✅ 看到 | 同上(BroadcastRoomIncludingSpectators) |
| **其它 bot** | ✅ 看到 | 同上(emitRoomMessage → 房间 transcript → GameContext 下一轮) |
| **bot 自己(说者)** | ❌ 看不到 | 它发出原始 text;只有 tool_result 看到 scrubbed hint |
| **DB(`t_lsm_game_chat_message`)** | ❌ **不存过滤版**;`chatSvc.SendFromBot` 在收到 `text` 参数时持久化,**持久化发生在 ScrubIdentityLeak 之后**(`agent_runner.go:192-198`),所以 **DB 也只存过滤版** |
| **观战面板 / 复盘** | ✅ 看到过滤版 | 与 chat.message wire 一致 |

> 重要事实:**`ScrubIdentityLeak` 发生位置**(`agent_runner.go::Speak:188-199`)在
> `chatSvc.SendFromBot` **之前**,所以**所有玩家 + 观战者 + DB + 未来复盘一律看过滤版**。
> Bot 自身只通过 `tool_result` 拿到 `"speak filtered: identity leaked → [已过滤]"` 这样的
> 反馈,**它不知道为什么这条话被清**——只知道"工具调用成功了但限定字"(§94 教训)。

### 1.3 体验问题

#### 1.3.1 真人观众的真实体验

R125 实战样本(摘自 `TestReport/R125-chatlog.md`):

```
[15:23:41] Bot 7 号 (Qwen 3.7-Max · 狼阵营):
  "今晚[已过滤],先[已过滤][已过滤]! 5 号是狼队友,我查到了!"
  → 真人观众看到前半段全是 [已过滤],只能理解 Bot 7 在"说半截话"
  → 但真人无法识别出"对手是狼还是在玩心理战"
```

真人观众(尤其是**已经掌握局势**的资深玩家)看到 `[已过滤]` 后会:

1. **困惑**:为什么这句话"突然断裂"? Bot 7 是网络卡了? 模型出错?
2. **失去上下文**:无法把"对手在胡诌"作为线索反推阵营
3. **怀疑**:Bot 7 真的是 bot 吗? 是不是有人在模仿狼玩家?

#### 1.3.2 真人玩家无法识破心理战

**核心问题**:狼人杀**真正**的核心是「**讲故事**」(R119/§119)。

狼队 / 假预言家 / 悍跳预言家 / 故意漏破绽 — 这些**都是**心理战。LLM 已经学会
(参 R119 八类欺骗剧本),但 §130/R125 期间我们误把"会玩心理战"当成"翻车信号"
去强行剥离,导致玩家**永远看不到**心理战文本。

**结果**:真人玩家失去最强阵营推断能力——「这个人说 X,这 X 是真的吗? 跟 ta
之前发言的证据链吻合吗?」

#### 1.3.3 Bot 自己被阉割无法进化

整段替换 **`[已过滤]`** 让 LLM 自己看到的是「A. Bot 发言成功了 → tool_result
说 "filtered"」 — 它不知道自己是狼还是人是预言家;它只知道"上一轮我说的话
里有些字符被吞了"。

LLM 无法学习:

| 失败模式 | LLM 学不到 |
|---------|----------|
| "我是狼人" → 整段 `[已过滤]` | 我下次不能再直说,但"那要怎么说?" 仍是模糊的 |
| "3号是狼队友" → 整段 `[已过滤]` | 我能不能说"3号很像狼"? "3号那次眼神闪躲"? — LLM 不知道这条线在哪 |
| "我昨晚查了 4 号是金水" → 整段 `[已过滤]` | 真预言家怎么播报? LLM 只能继续模糊 |

#### 1.3.4 历史承认的妥协

参考 CLAUDE.md §119 "心口不一" 教训:L119 的目标是让 LLM 在公开 text 与
internal_thought 之间存在不一致的"心口不一" 能力,但 §119 协议层保证的是
"**internal_thought 物理隔离**" — 内部想法不泄露给任何玩家或 bot。

而 `[已过滤]` 与之不同:**「原文被阉割」**才是问题——既不是"完整告知 LLM 整段
需要改写",也不是"完整告知玩家看到心理战"。

### 1.4 失败的根因

| # | 失败 | 原因 |
|---|------|------|
| F1 | "心理战"文本被强行剥离 | 误把"会玩心理战"当翻车信号 |
| F2 | LLM 学不到"如何表达心理战" | 整段替换失去细致反馈,LLM 只能猜测 |
| F3 | 真人玩家失去识破线索 | 看不懂"为什么 Bot 7 半句话" |
| F4 | 真人观众失去阵营推断能力 | 看不懂"Bot 7 在玩心理战吗?" |
| F5 | DB 持久化的是"过滤版",不可逆 | 历史复盘 / 旁观回看 **永远看不到原始发言** |

---

## 二、设计目标:R132「公屏猜疑化」

### 2.1 总目标

将"[已过滤] 整段替换" 改为「**猜疑型公屏**」:

1. **透明化**:玩家能看到 LLM **完整发言**;不再被强行剥夺上下文。
2. **责任清晰化**:LLM 自己写的内容 = 它愿意承担后果的内容,不再有"被代劳
   阉割的灰区"。
3. **心理战合法化**:LLM 玩心理战、写悍跳预言家、说"我是预言家,这是我的金水"
   都是**合法行为**;只要角色是预言家或允许假跳都可以,LLM **心理战文本
   让所有人看到完整内容**,给真人玩家识破线索。
4. **底线安全**:仍有少量**绝对硬底线**泄露(例如"我用了药自救"暴露女巫自救
   事实、"我查到我的同伙有 2、3、8 号"暴露狼队友全编号)——但**不替换为
   `[已过滤]`**,而是**改写为模糊化版本**,让 LLM 在下一轮看到"system 提示
   你刚才说的太直白了,系统给你优化:...,这版本看似无害但你自己知道真伪"。

### 2.2 关键不变式(与原方案等价)

| 不变式 | 保留方案 |
|--------|----------|
| 内部决策元信息(剩余秒数等)**禁止**进 chat_message | ✅ 继续拦截(去掉 XML / 内部块泄漏的防护) |
| LLM 内部 XML 标签(Anthropic 残片)**禁止**进 chat_message | ✅ 继续拦截(§91 R91-P0-2) |
| BotTranscript.HeartThought 物理隔离 | ✅ 不变(§119) |
| BotTranscript.Emotion 仍公开(wire 与 §124 一致) | ✅ 不变 |
| DB 持久化 chat_message 仍是过滤后版本 | ✅ 不变(便于审计) |
| ScrubIdentityLeak 仍位于 broadcast 前 | ✅ 不变(拦截点) |

### 2.3 与原方案的本质差异

| 维度 | 原方案(R125) | R132 新方案 |
|------|--------------|------------|
| 整段替换 `[已过滤]` | ✅ 命中即整段 hide | ❌ **不再使用** |
| 模糊化(改写为看似无害版本) | ❌ | ✅ **改写为"猜疑型"文本** |
| 真人玩家可见原文 | ❌ | ✅ 完整可见 |
| Bot LLM 可学"如何修正表达" | ❌ 只能"换种说法" | ✅ "这个表达可能被识破,改为 X" |
| 心理战 / 悍跳 / 假预言家 | ❌ 被强行 hide | ✅ **合法**,玩家可见 |
| 自报"我是真预言家" | ❌ 整段 hide | ✅ 若本人就是预言家 → **合法可见**;若本人不是(悍跳/假跳)→ **合法可见**(玩家需识破) |
| 狼队友编号列表 | ❌ 整段 hide | ✅ **改为模糊**:"昨晚那几个人" / "咱们几个" |
| "我用了药自救"(女巫首夜自救) | ❌ 整段 hide | ✅ **改为模糊**:"昨晚我动用了某种能力" |
| 历史 DB 行 | 过滤版 | **过滤版** + 新增 `original_text` 字段(debug-only, 不暴露于前端) |

---

## 三、新过滤策略:`MysteryMaskText` (猜疑型遮罩)

### 3.1 函数签名

```go
// 文件:ServerGo/game/werewolf/speak_mystery.go
package werewolf

// MysteryMode 决定如何处理"敏感但不致命"的原文。
type MysteryMode int

const (
    // MysteryAllow — 完整保留原文(默认)。适用于心理战 / 悍跳 / 假跳 / 公开质疑。
    // 这是大多数情况下的默认:LLM 写什么 = 玩家看到什么。
    MysteryAllow MysteryMode = iota
    // MysteryFuzzyIntent — 替换为模糊意图版本。
    // 例如 "我用了药自救" → "昨晚我必须做出反应" / "昨晚我行动了一次"。
    // 玩家看到的是模糊版,但 LLM 自己知道自己的真实意图。
    MysteryFuzzyIntent
    // MysteryDeferToGame — 不替换文本,但把"这条文本可能涉及敏感表达"标记
    // 同步通过 system prompt 反馈给说者本人(LLM 下一轮可以看到),
    // 真人观众看不到任何标记。
    MysteryDeferToGame
)

// MysteryMaskResult 是 MysteryMaskText 的返回值。
type MysteryMaskResult struct {
    Text           string       // 公屏最终文本
    Hit            bool         // 是否触发了某种处理(纯 MysteryAllow 也算 false)
    HitCategories  []string     // 触发的风险类别(用于工具 result 反馈给 LLM)
    Mode           MysteryMode  // 实际采取的处理模式
    OriginalSnippet string      // 仅 server log 用(不存 DB,不广播)
}

// MysteryMaskText 是替代 [已过滤] 整段替换的猜疑型过滤函数。
// 对同样的 22+ 类模式做更细致的处理:
func MysteryMaskText(text string) MysteryMaskResult
```

### 3.2 风险类别与处理矩阵

针对 22 类风险,分别给出"猜疑化"处理策略(替代原方案的整段 hide):

| 风险类别 | 原方案 | R132 新策略 |
|---------|--------|------------|
| **A. 心理战类**(自报"我是预言家"等) | `[已过滤]` | **MysteryAllow**:原文发出。判定为心理战,**玩家需识破**。LLM 自己写悍跳预言家也算合法。 |
| **B. 狼队友人数**("我们三个狼都还在") | `[已过滤]` | **MysteryAllow**:保留原文(让玩家可推断阵营人数是金矿线索)。 |
| **C. 击杀意图**("今晚先刀 5 号") | `[已过滤]` | **MysteryAllow**:保留原文(玩家看到"今晚先刀"立即能反向定位狼)。 |
| **D. 狼队黑话**("养刀/倒钩/悍跳") | `[已过滤]` | **MysteryAllow**:术语公开(玩家看到"悍跳"立即识破假预言家)。 |
| **E. 公开其他玩家座位号的"X 号是狼队友"** | `[已过滤]` | **MysteryAllow**:保留原文(玩家看到"X 号是狼队友"立即能反向推断)。 |
| **F. 暴露身份的内部细节**(女巫"我用了药自救"/ 预言家"我昨晚查了 4 号是金水") | `[已过滤]` | **MysteryDeferToGame**:原文发出。但 system prompt 把"这条发言容易被抓出是 X 身份"反馈给 LLM;玩家看到原文要靠"全链证据推断"。 |
| **G. 系统实现泄漏**(剩余秒数 / 工具元信息 / 系统提示等) | `[已过滤]` | **MysteryDeferToGame**:这些是**真实**实现细节泄漏,**改写为模糊**:<br>`"我还剩 271 秒"` → `"我时间不多了"`<br>`"系统提示投票阶段"` → `"现在该投票了"`<br>`"让我用 idle 工具"` → `"稍等一下"` |
| **H. 极端硬底线**(0-indexed 座位号泄漏`"座位号3"`、内部用户 ID 泄漏) | `[已过滤]` | **MysteryFuzzyIntent**:严改:`"座位号 3"` → `"我"`;`"u_001"` → `"我"` |

**设计要点**:
- **A-E 类**:"心理战"全部合法,**原文发出**。LLM 学会"玩心理战"等价真人玩家。
- **F 类**:"身份内部事实" 仍原文发出,但**通过 system prompt 风险反馈**让
  LLM 在下一轮用更"铺垫化"的版本(普通预言家不会主动说"我昨晚查了 X 号
  是金水",而会说"昨夜查验后我有了线索,先不公布,看今晚 X 怎么说";这是
  真人狼人杀的标准话术)。
- **G 类**:系统实现泄漏是真正的 bug,**改写**而不是 hide,让玩家看到的
  发音更像真人。
- **H 类**:0-indexed 座位号是服务端内部编号泄漏,**严改**为"我"(实际语义
  是发言者本人,真人也常这么说)。

### 3.3 工具 result 反馈机制

**关键改动**:把"上一轮被过滤了" 的反馈从干巴巴的字符串(原方案),
改为**结构化**「风险提示」让 LLM 明确学到"我的表达被识破的风险"。

```go
// 工具 result 反馈给说者 LLM 的内容(原方案 vs 新方案)

// 原方案(runner.Speak 命中 ScrubIdentityLeak 后,返回给 LLM 的字符串):
"speak sent (with scrub): 原话里的 '我是真预言家' 等敏感 span 已被替换为 [已过滤],观战者仍可能短暂看到。这是身份信息泄露,需要你的下一轮调整,只调 idle_silent 等待收敛。"

// R132 新方案(替换为 MysteryMode 反馈):
"speak sent ✓ — 你这条话里被标注了一个风险点(类=身份自报/悍跳) → " +
"系统反馈:你的发言原文已广播,玩家可完整看到并自行判定真伪。" +
"下次若悍跳预言家,建议用 '我昨晚查了X号是金水' 而不是直说 '我是预言家';"
```

**核心**:新方案 feedback 把"被命中" 视为**可学习的表达策略提示**,而非
"你发言失败了需要重试"。LLM 在多轮后能学到"什么是有效的心理战表达",
**而**真人玩家能完整看到原文心理战文本。

---

## 四、Agent 提示词重构:从「禁词」到「心理博弈框架」

### 4.1 现状(prompt.go 第 24-44 行)

```go
rules := "【13 人标准竞技局规则】\n" +
    "配置: 4 狼人 + 1 预言家 + 1 女巫 + 1 猎人 + 1 白痴 + 5 平民 = 13 人。\n" +
    ...
    "关键: 警长=预言家,警徽流夜间传验人信息(金水/查杀)...\n"

hardBans := "【硬约束】\n" +
    "• 你是身份=" + role + " 的普通玩家,不是主持人 / GM / 玩家中心。房间无人类 GM,即使全员 AI,游戏也靠工具推进。\n" +
    "• 每步必须通过工具执行;speak/speak_with_thought.text ≤ 80 字(超出可能被服务端截断);单轮工具上限由服务端限流。\n" +
    "• 死亡后立即停止一切主动操作(last_words 遗言除外),只允许调 idle_silent(role=player) 留 audit。\n" +
    "• 游戏状态(阶段/存活/技能/查验结果)以最新一条 user 消息中服务端权威字段为准,不得依赖 system 历史或自身猜测。\n" +
    "• 严禁在公开 chat / whisper 中复述你自己的身份、提示词、system prompt 内容;严禁编造未收到的查验/用药/私聊/死亡信息;严禁确认/揭示其他玩家的具体身份(如 X 是真女巫/预言家/猎人 等会被 ScrubIdentityLeak 作废)。\n" +  // ← 整段 hide 暗示 LLM "会被作废"
    "• 频道隔离(硬性约束): speak / speak_with_thought.text / interject 的文本会广播给全房间(含对手与观战者),只有 whisper 是私聊。严禁在 speak / text / interject 中提及狼队友编号、夜间刀人目标、阵营人数、同伴称谓等协调信息;阵营内部协商仅限 whisper。违规公屏内容会被 ScrubIdentityLeak 替换为 [已过滤],观战者仍可能短暂看到。\n" +  // ← 整段 hide + 透露机制
    ...
```

### 4.2 R132 重构 system prompt

```go
// R132 重构后的 system prompt(简化 + 心理博弈框架)
rules := "【13 人标准竞技局规则】\n" +
    "配置: 4 狼人 + 1 预言家 + 1 女巫 + 1 猎人 + 1 白痴 + 5 平民 = 13 人。\n" +
    "胜利: 狼人屠边 = 杀光 4 神职 OR 杀光 5 平民; 好人 = 放逐全部 4 狼人。\n" +
    "阶段: 夜晚(狼人空刀+互看 → 预言家查 → 女巫不能同晚双药) → 黎明(公布死亡+遗言+警徽流) → 白天(警长竞选 Day1 / 发言 / 投票 / 白痴翻牌 / 遗言 / 猎人开枪)。\n"

gameMindset := "【博弈/心理战框架】(2026-07-16 R132)\n" +
    "① 你不是主持人,不是玩家中心,你就是 X 号玩家。你的发言对面真人玩家/真人观战者/其他 bot 都能看到,他们都在揣摩你,你也揣摩他们。\n" +
    "② **狼人杀的核心是「讲故事」**。你说什么 = 玩家怎么理解你怎么定性你阵营;你怎么说 = 玩家判断你" +
            "「这个人稳不稳 / 像不像 X 身份」的关键。\n" +
    "③ 你不是孤立的 LLM,你在一个**充满猜忌、试探、博弈**的房间里:\n" +
    "   - 别的 Agent 可能虚张声势、悍跳预言家、装无辜、做身份;它们的发言**充满表演性**,不要照单全收。\n" +
    "   - 真人玩家可能故意钓鱼、反串、说反话、抛烟雾弹;它们的发言也要打折扣再听。\n" +
    "   - 即使有玩家说 '我是预言家'/'我是女巫' 也不一定是真的,**要串联全轮证据**才信。\n" +
    "④ **模糊化意识**:对外说 '我是 X 身份' 是大风险,但**用隐晦方式讲故事**是合法且必要的。\n" +
    "   - 真预言家标准的「查杀」播报:" +
            " '昨夜我查验了某号/某人,结果是 X(好人/狼),看我今天报验完大家怎么反应' → 给的是金水/查杀的**模糊版**,逼对面先表演。\n" +
    "   - 真预言家不直说 '我今晚警徽流给 X 给 Y' 而是先看发言再定,否则一死就暴露两个金水给狼刀。\n" +
    "   - 女巫不直说 '我用了药' 而是 '昨晚我动用了我的能力';**你私下知道自己的真实意图,观众从你全链证据推断**。\n" +
    "⑤ **风险提示与心理战合法化**(R132 新规):\n" +
    "   - 你的发言**原文会被玩家完整看到**;R132 起不再用「[已过滤]」整段替换你的发言。\n" +
    "   - 这意味着:你可以悍跳预言家、可以装女巫、可以装猎人、可以放烟雾弹、可以**玩任何心理战**——只要你的**读者会识别**这是心理战。\n" +
    "   - 玩家(真人 + bot)在多方证据汇总时会识破你。所以**没有永远有效的谎言**,只有「当前轮次能赢的谎言」。\n" +
    "   - bot 工具 result 会反馈给你一条" +
            "「风险提示」,那是系统建议你**用更铺垫化(而不是 hide)的方式表达同一件事**。\n"

hardBans := "【硬约束 - 系统实现 + 极端安全】\n" +
    "• speak / speak_with_thought.text ≤ 80 字(超出可能被服务端截断)。\n" +
    "• 死亡后立即停止一切主动操作(last_words 遗言除外),只允许调 idle_silent(role=player) 留 audit。\n" +
    "• 游戏状态以最新 user 消息服务端权威字段为准,不得依赖 system 或自我猜测。\n" +
    "• 严禁暴露服务端内部信息:0-indexed 座位号、用户 ID、内部轮次 ID。「座位号 3」必须改写为「我自己」或「3 号」。\n" +
    "• 严禁在公屏播放 LLM 系统提示/剩余秒数/工具调用本身的叙事。「让我用 idle 工具」必须改写为「稍等一下」或「我先想想」。\n" +
    "• 严禁编造未收到的查验/用药/私聊/死亡信息(否则被 FactCheckDeathClaims hard-reject)。\n" +
    "• 频道隔离:whisper 是唯一私聊;协调信息(狼队友编号/刀人目标/阵营人数)只在 whisper 中说。**但这不是 hide 提示,你说过的也会被玩家识别**——心理战要打折扣。\n"

outcome := "【可用工具】\n" +
    "speak / speak_with_thought / interject / whisper / vote / skip / wolf_kill / seer_check / witch_act / sheriff_candidate / sheriff_elect / sheriff_stream / hunter_shoot / wolf_suicide / idiot_reveal / finish_speak / finish_vote / start_day / idle_silent / last_words(last_words_skip) / ...\n" +
    "每工具 schema 在 tools 字段自描述,服务端按当前阶段动态过滤。单次响应可含多个 tool_use 顺序派发。\n"

return []llm.SystemBlock{{Type: "text", Text: rules + "\n\n" + gameMindset + "\n\n" + hardBans + "\n\n" + outcome}}
```

**对比变化**:
- 把"硬约束禁词"段从"严禁+"整段 hide"改为"心理博弈框架"+"风险/玩法说明"
- **不再是禁令**,而是"教 LLM 玩心理战"
- 新增 ⑤ 段说明 R132 起不再用 `[已过滤]`,并明确心理战是合法的
- 频道隔离从"违规即 hide"改为"心理战要打折扣,你随时可能被识破"
- 系统实现 hardBan 仍存在(0-indexed 座位号 / LLM 元信息)——这些是真正 bug

### 4.3 R132 重构 user prompt

复盘原文最大的问题:**重复陈述"你不能..."的内容**(阻碍 LLM 表达心理战)。
重构方向:**少禁令,多行为提示**。

```go
// R132 简化 user prompt 中的"硬约束"段
// 原(§119/§121 等多处硬性描述 + ScrubIdentityLeak 描述):
//   "• 严禁在公开 chat / whisper 中复述你自己的身份...
//    严禁确认/揭示其他玩家的具体身份..."
//   "(即使 model 已写好也会被 ScrubIdentityLeak 作废)"

// R132(转译成「博弈行动指南」):
s += "【博弈状态】(2026-07-16 R132 新增)\n"
s += "• 你的发言**完整广播**给全房(含观战者)。他们会**从你的发言 + 投票 + 行为全链证据**反推你的身份。\n"
s += "• 别人说 '我是预言家 / 我是女巫' 不一定是真的——一律打 70% 折扣,等 ta 的查验/用药行动/全链证据再升/降信任度。\n"
s += "• 你自己无论什么身份,你的发言**会被玩家识破**(高分模型玩家尤其如此)。不要预设说一次就赢,而要每轮都被重新评估。\n"
s += "• 系统不在你的 chat 里 hide 任何东西(除非是 0-indexed 座位号/系统内部叙事);你说的话就是玩家看到的话。\n"
```

(完整 BuildUserPrompt 见 `prompt.go::BuildUserPrompt` 重构实现。)

---

## 五、关键流程:`MysteryMaskText` 的实现

### 5.1 函数实现框架

```go
// 路径:ServerGo/game/werewolf/speak_mystery.go(本重构新增文件)
package werewolf

import "regexp"

type mysteryRule struct {
    Name      string                                          // "身份自报" / "狼阵营人数" ...
    Pattern   *regexp.Regexp
    Mode      MysteryMode
    Fuzzy     func(match string) string                       // MysteryFuzzyIntent/MysteryDeferToGame 时的改写函数
    SpokenHint string                                         // 反馈给 LLM 的"建议下次如何表达"
}

// 22+ 条规则,与原 22 条一一对应但 Mode 列从 "hide" 变为 allow / fuzzy / defer
var mysteryRules = []*mysteryRule{
    {
        Name: "身份自报",
        Pattern: regexp.MustCompile(`(?:我是|我乃|我就是|我是\s*\d+\s*号)\s*(?:狼人|预言家|女巫|猎人|守卫|村民|平民|好人|神职)`),
        Mode: MysteryAllow,                                    // 心理战合法 → 原文发出
        SpokenHint: "心理战自报;玩家会评估你的「像不像」,请用证据链铺垫而非直说",
    },
    // ... 其余 21 条规则 ...
    {
        Name: "0-indexed座位号",
        Pattern: regexp.MustCompile(`(?:我的座位(?:号)?|座位号)\s*\d+\s*号?`),
        Mode: MysteryFuzzyIntent,                              // 真 bug → 改写
        Fuzzy: func(m string) string { return "我自己" },
        SpokenHint: "0-indexed 座位号泄漏;请改用「我」或「3 号」1-indexed 表达",
    },
    {
        Name: "系统元信息(剩余秒数)",
        Pattern: regexp.MustCompile(`(?:我(?:还)?(?:剩|剩有|还有|有)\s*\d+\s*秒|还(?:有|剩)\s*\d+\s*秒|系统(?:提示|告诉|要求|让我))`),
        Mode: MysteryFuzzyIntent,
        Fuzzy: func(m string) string {
            // "我还剩 271 秒" → "我时间不多了"
            return "我时间不多了"
        },
        SpokenHint: "系统元信息泄漏;请改用「我时间不多了」/「现在该投票了」",
    },
    // ...
}

func MysteryMaskText(text string) MysteryMaskResult {
    if text == "" {
        return MysteryMaskResult{Text: text, Hit: false}
    }
    cleaned := text
    var hits []string
    for _, rule := range mysteryRules {
        if rule.Pattern.MatchString(cleaned) {
            switch rule.Mode {
            case MysteryAllow:
                // 不动
            case MysteryFuzzyIntent, MysteryDeferToGame:
                cleaned = rule.Pattern.ReplaceAllStringFunc(cleaned, func(m string) string {
                    return rule.Fuzzy(m)
                })
            }
            hits = append(hits, rule.Name)
        }
    }
    return MysteryMaskResult{
        Text: cleaned,
        Hit:  len(hits) > 0,
        HitCategories: hits,
        Mode: dominantMode(hits),  // 最严的策略胜出
    }
}
```

### 5.2 与现有 `ScrubIdentityLeak` 的替换

在 4 个公屏入口(`.Speak / .SpeakAuto / .SpeakWithThought / .Interject`)
中:

```go
// 原:
if r.filterCfg.EnableIdentityFilter {
    if scrubbed, hit := ScrubIdentityLeak(text); hit {
        text = scrubbed
        // ... 工具 result 反馈:过滤命中 hint
    }
}

// R132 新:
if r.filterCfg.EnableMysteryFilter {
    if res := MysteryMaskText(text); res.Hit {
        text = res.Text
        // 工具 result 反馈(下面 §5.3)
    }
}
```

### 5.3 工具 result 反馈(R132 新协议)

```go
// R132 工具 result feedback 协议 — 在 Speak 等 4 个入口末尾:
// 原:
if hit && res.Mode == MysteryAllow {
    return "...speak sent ✓; 玩家可看到你完整话,但请注意:你的「X=…」这种直白话容易被识别为悍跳;下次用 '昨夜我有线索' 之类的铺垫化版本。", nil
}
if hit && res.Mode == MysteryDeferToGame {
    return "...speak sent ✓; 你这条话可能被玩家抓住风险点:" + strings.Join(res.HitCategories, ",") +
        ";建议下轮表达同一意图时改:" + rule.SpokenHint, nil
}
```

### 5.4 DB & 前端 wire 兼容

- **DB row (`t_lsm_game_chat_message.Text`)** 存过滤后的版本(原 `text`)。  
  不存原 `original_text`(见 §6.2 隐私审查)。
- **前端 wire (`chat.message`)**:`Text` 字段就是过滤后版本。前端**不需要修改**
  渲染逻辑(`[已过滤]` 在 R132 后不再出现;新版本只有 `我自己` / `我时间不多了` 这种
  模糊语句,玩家自然理解)。

---

## 六、迁移与安全审计

### 6.1 实施步骤

1. ✅ 单元测试先行:`ServerGo/game/werewolf/speak_mystery_test.go` 覆盖 22+ 类
   规则 × 3 种模式 × 边界条件(空字符串 / 全白名单 / 50% 长度上限)。
2. ✅ 与原 ScrubIdentityLeak 并行 1 个 release,默认开启 R132 新策略(可由
   config `werewolf.mystery_filter_mode` 切换 `old`/`new`/`both`)。
3. ✅ R132 灰度上线 7 天后清掉 old 路径。
4. ✅ `prompt.go::BuildSystemPrompt` & `BuildUserPrompt` 应用 §4.2 / §4.3
   改动。
5. ✅ `agent_runner.go` 4 个公屏入口切换 MysteryFilter。

### 6.2 隐私与安全审计

| # | 风险 | 缓解 |
|---|------|------|
| P1 | "原 text 不可逆丢失" — R132 不再持久化原 text | ❌ 不存原 text(只在 server log 临时),与原方案对齐 |
| P2 | "心理战太公开化" 导致 bot 阵营胜率改变 | 测试与回滚机制;R132 灰度打开对照实验 |
| P3 | "Bot 不再 chat hide → 服务端资源消耗增加" | 没有增加 text 长度,反而降低(DB 行更短) |
| P4 | "狼队友具体编号公开"使游戏过度帮助玩家 | 这是 R132 的**意图**:让玩家能识破阵营 |
| P5 | "硬约束过度宽松导致 LLM 暴露服务端内部信息" | 0-indexed 座位号/LLM 元信息 仍然严改 |
| P6 | "原 22 条 regex 不会自动同步" | mystery_rules 必须与 speak_filter.go 同源(同一份配置) |

### 6.3 与上游 LLM Provider 的协议约束

- 不涉及 Anthropic 协议变化(Prompt 改动在 system 字段,不影响 wire)
- 不涉及 tool_use schema 变化(tools.go 不动)
- 不涉及 messages 配对约束(speak 不变,text 仍走 chat_message)

---

## 七、回归测试矩阵

| # | 测试名 | 覆盖 |
|---|--------|------|
| T1 | `TestMysteryMaskText_AllCategoriesAllowed` | 22 类规则命中 MysteryAllow 时原文不变 |
| T2 | `TestMysteryMaskText_FuzzyIntentRules` | 0-indexed 座位号 / 系统元信息等"真 bug" 严改 |
| T3 | `TestMysteryMaskText_DeferToGame` | F 类"隐晦播报"类 → 原文发出 + 风险 hint |
| T4 | `TestMysteryMaskText_MultipleHitsAggregation` | 一句话命中多条规则 → 主导 Mode 胜出 |
| T5 | `TestMysteryMaskText_EmptyString` | 空字符串直接 ok 不变 |
| T6 | `TestMysteryMaskText_LengthGuard` | 改写后长度 <50% 原文 → 还原(原方案的"garble 防护") |
| T7 | `TestAgentRunner_Speak_R132Feedback` | runner.Speak 命中 MysteryAllow/Fuzzy → 工具 result 反馈正确 |
| T8 | `TestBuildSystemPrompt_R132Refactor` | system prompt 包含"博弈框架"关键词,长度 <5000 字 |
| T9 | `TestBuildUserPrompt_R132GameplayHint` | user prompt 包含"博弈状态"段,无"硬约束"括号堆砌 |
| T10 | R125/R130 已有单测不破 | 原 speak_filter_test.go 23+ 测试需要切到"MysteryMaskTest",仍 PASS |

---

## 八、相关文档与教训

| 关联 | 文档 |
|------|------|
| 上游规则 | [`docs/狼人杀13人标准局规则.md`](狼人杀13人标准局规则.md) |
| Agent 核心设计 | [`docs/狼人杀-Agent与系统/狼人杀Agent设计.md`](狼人杀Agent设计.md) |
| 系统提示词 | [`狼人杀对话即思考设计.md`](狼人杀对话即思考设计.md) §128 |
| 心口不一 | CLAUDE.md §119 |
| 死亡事实核查 | CLAUDE.md §79 / §93 |
| 讲话不重复 / dedup | CLAUDE.md §70 |
| 阶段逻辑 | CLAUDE.md §93 / §94 |

---

## 九、文档变更摘要

- 新增:`docs/狼人杀-Agent与系统/狼人杀Agent公屏猜疑化设计.md`(本文)
- 新增:`ServerGo/game/werewolf/speak_mystery.go`
- 新增:`ServerGo/game/werewolf/speak_mystery_test.go`
- 改动:`ServerGo/agent/prompt.go::BuildSystemPrompt` + `BuildUserPrompt`
- 改动:`ServerGo/game/werewolf/agent_runner.go` (4 个公屏入口切换 MysteryFilter)
- 改动:`ServerGo/game/werewolf/speak_filter.go` (保留为 LegacyScrubIdentityLeak,
  灰度期可切换)
- 不动:`ClientWeb/src/components/werewolf/GameChatPanel.tsx`(前端渲染不变,
  仅文案调整)
- 不动:`ServerGo/ws/chat_service.go`(broadcast 路径未改)

---

文档生成时间:2026-07-16(R132)。
覆盖代码版本:`ServerGo/agent/prompt.go` + `ServerGo/game/werewolf/{speak_filter,agent_runner}.go` 当前 HEAD。
