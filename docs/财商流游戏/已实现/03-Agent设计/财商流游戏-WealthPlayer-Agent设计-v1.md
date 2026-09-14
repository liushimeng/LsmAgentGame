# 财商流游戏 — WealthPlayer Agent 设计 v1

> **定位**：`LsmAgentGame-Wealth-Player`（AgentClassName，CLAUDE.md §24）玩家 Agent 的实现契约：
> 包结构、GameContext 字段、17 工具签名与 JSON Schema、System/User prompt 模板全文、
> 月度决策循环、限流 / 超时 / 重试、与狼人杀 / 德扑 Agent 的差异表。
> 引擎侧 ToolRunner 实现见 [`../02-架构设计/财商流游戏-后端架构与经济引擎-v1.md`](../02-架构设计/财商流游戏-后端架构与经济引擎-v1.md) §15；
> 动作语义（校验 / 费用 / Ledger）唯一事实来源为协议文档 §4 动作语义表。

---

## 目录

1. [AgentClassName 登记](#1-agentclassname-登记)
2. [包结构](#2-包结构)
3. [GameContext 契约（agent/wealthtypes）](#3-gamecontext-契约)
4. [System Prompt 模板（全文）](#4-system-prompt-模板全文)
5. [User Prompt 模板（月度事件，全文）](#5-user-prompt-模板全文)
6. [月度决策循环（run.go）](#6-月度决策循环)
7. [工具集（17 个，tools.go）](#7-工具集17-个)
8. [记忆（memory.go）](#8-记忆)
9. [限流 / 超时 / 重试 / 流式](#9-限流--超时--重试--流式)
10. [与狼人杀 / 德扑 Agent 的差异表](#10-与狼人杀--德扑-agent-的差异表)

---

## 1. AgentClassName 登记

`agent/class_names.go` 追加（**仅此一个**——P0 不登记未接线的 Judge / MemoryIter / Commentator /
Coach，§130「声明了却从不接线」教训）：

```go
// AgentClassWealthPlayer 是财商流游戏玩家 Bot 的 AgentClassName（P0 v1）。
// 由 ServerGo/agent/wealthplayer/ 的 Agent struct 实现;驱动 game/wealth 引擎
// 按月度节拍参与人生沙盘(每月 ≤3 个动作工具 + ≤1 次 speak + submit_month)。
// 与狼人杀玩家的核心差异: 无阵营/隐藏身份博弈,决策对象是个人三表与市场周期;
// 与德扑的差异: 每月多次动作(德扑每轮仅 1 次 tool_use)。
// 详见 docs/财商流游戏/已实现/03-Agent设计/财商流游戏-WealthPlayer-Agent设计-v1.md。
AgentClassWealthPlayer AgentClassName = "LsmAgentGame-Wealth-Player"
```

`AllAgentClassNames()` 同步追加；`class_names_test.go` 增断言
`AgentClassWealthPlayer != ""` 且出现在 `AllAgentClassNames()`（防 §130 复发）。
出站 User-Agent：`LsmAgentGame-Wealth-Player/<AppVersion> <buildDateTime>`（§24 拼装规则，
经 `LLMRequest.AgentClassName` 在 `llm/anthropic` 出站前覆盖）。

## 2. 包结构

```
agent/wealthtypes/context.go     GameContext 契约（leaf 包：仅基本类型，无 llm/ws 依赖）
agent/wealthplayer/agent.go      Agent struct{ seat, modelKey, memory, runner ToolRunner, … } + 构造
agent/wealthplayer/run.go        OnMonthStart(ctx GameContext)：月度决策循环入口（事件驱动）
agent/wealthplayer/run_llm.go    Provider 小接口（仿 wwplayer/run_llm.go）：
                                 type llmCaller interface {
                                     Chat(ctx, key string, req llmtypes.LLMRequest) (llmtypes.LLMResponse, error)
                                     ChatStreamAccumulate(ctx, key string, req llmtypes.LLMRequest,
                                         onProgress func(llmtypes.StreamEvent) error) (llmtypes.LLMResponse, error)
                                 }
agent/wealthplayer/tools.go      BuildTools(role) []types.ToolDef —— 17 工具
agent/wealthplayer/prompt.go     SystemPrompt(card)/UserPrompt(ctx) 模板渲染
agent/wealthplayer/memory.go     Memory：月度决策滚动记录 + 摘要
game/wealth/agent_runner.go      ToolRunner 实现（in-process，不走 WS；发言走 chatSvc.SendFromBot）
```

依赖方向：`wealthplayer → wealthtypes`（契约）+ `llm/types`（wire 类型）；**不 import** `game/wealth`
（经 ToolRunner 接口反转，与狼人杀 ToolRunner 13 方法同构）。

## 3. GameContext 契约

`agent/wealthtypes/context.go`（引擎在持锁态构造快照，Agent 侧只读消费——同 thptypes 生命周期约定）：

```go
type GameContext struct {
    // 身份元数据
    RoomID, GameKind string          // GameKind 恒 "wealth"
    MySeat int                       // 0..7
    MyUserID, ModelKey string
    Month int                        // 1..420
    Age int                          // 主时钟年龄
    Phase string                     // "acting" | "settling"
    TimeRemainingSec int             // 本月窗口剩余秒（watchdog 用）

    // 市场快照
    Cycle  CycleBrief                // {Phase, LPR, CPI, MonthsLeft}
    Market MarketBrief               // {StockIndex, GoldPrice, BondRate, HouseIdx map[string]float64}

    // 本人（my.* 全量镜像，含三表/资产/贷款/资源/信用/家庭/FI）
    Me SelfBrief

    // 同场玩家（公开字段：职业/区/净资产/FI/资源/收入档）
    Peers []PeerBrief

    // 近期事件与流水（与 game.state 同源）
    RecentEvents []EventBrief        // 最近 30 条
    RecentLedger []LedgerBrief       // 本人最近 20 条

    BotIdentity BotIdentityBrief     // {UserID, ModelKey, ModelName, AgentClass:"LsmAgentGame-Wealth-Player"}
}
```

各 Brief 结构与协议文档 §3 字段一一对应（字段名同 JSON 键），此处不重复定义。

## 4. System Prompt 模板（全文）

`prompt.go::SystemPrompt(card)`——以下为**完整中文模板正文**（`{{}}` 为占位符，渲染时替换；
分段用 SystemBlock 数组，每段一个 `{"type":"text"}`，§14.1 wire 约束）：

```
【第 1 段 · 身份】
你是「{{card.Name}}」，今年 {{start_age}} 岁，职业是{{card.Title}}（职业卡 {{card.ID}}），生活在{{card.HomeDistrictCN}}。
财务起跑线：月薪 {{salary}} 元/月，月支出基数 {{expense}} 元，储蓄 {{savings}} 元，
精力 {{energy}}/10，人脉 {{network}}/10，认知 {{cognition}}/10，信用分 {{credit_score}}。
{{#if card.HealthGrade}}健康等级 {{health_grade}}。{{/if}}
{{#if card.FamilyInit}}家庭：{{family_init}}（婚姻 {{marital}}，子女 {{children_count}}，需赡养老人 {{elders_dependent}} 位）。{{/if}}

【第 2 段 · 性格与风险偏好】
你的人格标签：{{card.Personality | join "、"}}；行为特征：{{card.BehaviorTraits | join "、"}}；
风险偏好：{{card.RiskPreferenceLabel}}}（conservative=保守 / balanced=平衡 / aggressive=激进）。
请始终以这个人设做决策与发言：保守者重现金流与安全边际，激进者敢于加杠杆与逆周期抄底，
但你必须像真人一样有情绪、有偏好、会犯错，不要表现出完美的最优化计算。

【第 3 段 · 目标】
你的人生目标：
{{#each card.Goals}}- {{this}}
{{/each}}
终局评分 = 财务自由度 50% + 人生满意度 30% + 社会贡献 20%。财务自由指数 FI = 月被动收入 ÷ 月总支出。

【第 4 段 · 规则摘要】
- 市场周期四阶段：复苏（股+15%/房+5%/金-5%/LPR3.5%）、繁荣（+30%/+15%/-10%/4.5%）、
  衰退（-25%/-5%/+10%/5.8%）、萧条（-40%/-15%/+25%/2.8%）。每月有小幅随机漂移。
- 每月月初你最多执行 3 个动作工具（buy_asset / sell_asset / buy_house / take_loan / repay_loan /
  start_side_business / stop_side_business / study / socialize / rest / work_overtime / move_district /
  consume / donate），外加最多 1 次 speak。动作要付真实成本：
  study=2000元/精力-1/认知+1；socialize=1000元/人脉+1；rest=精力+2；work_overtime=精力-2/当月工资×0.3 奖金；
  move_district=3000元/精力-1；副业月入约 2000–6000 元但耗精力 2/月；
  买房首付 ≥30%，房贷 30 年等额本息（LPR+0.5%）；消费贷 10% 年化 3 年；
  信用贷 5/10/20 万三档会压低你未来的工资增长与副业收入（杠杆的隐性成本）。
- 月结顺序：工资→被动收入→固定支出→税+社保→生活支出→债务。个税 7 级累进（起征 5000），
  社保 10.5%（其中 8% 进你的养老金账户，60 岁才能领）。
- 危险信号：现金连续 3 个月为负 = 破产清算（资产七折变现、信用清零）；精力透支到 -3 = 健康危机。
- 人生事件不可控：结婚/生育/疾病/失业都会发生，留足应急现金（建议 3–6 个月支出）。

【第 5 段 · 输出纪律】
1. 每月先在内部想清楚：本月现金流是否健康？市场处于周期哪个位置？我的目标推进到哪了？
2. 每月最多 3 个动作工具 + 1 次 speak，然后必须调用 submit_month 结束本月；不调用也会被系统强制结束。
3. speak 的内容 ≤100 字，像真人在群里聊天：可以聊行情、吐槽生活、分享买卖心得；不要复述工具参数。
4. 不要每 3 个动作都全用满——没有好机会时，攒钱、休息、学习也是决策。
5. 一切金额单位是人民币元。你的决策会被记录在财富流水账中，终局会生成你的人生报告。
```

> 文档池（`cfg.Wealth.ProfessionDocsPath`，json 键 `profession_docs_path`）加载的卡片，
> 第 1/2/3 段字段映射规则见职业卡加载器文档 §3；骨架卡缺画像字段时按词库默认值渲染。

## 5. User Prompt 模板（全文）

`prompt.go::UserPrompt(ctx)`——每月 acting 开始推送一条 user 消息（content-block 数组）：

```
【第 {{Month}} 个月 ｜ {{Age}} 岁 ｜ 周期：{{Cycle.PhaseCN}}（LPR {{LPR}}%，CPI {{CPI}}%，预计还剩 {{MonthsLeft}} 个月）】

■ 市场行情
股票指数 {{StockIndex}} 元/份（{{StockDelta}}}）；黄金 {{GoldPrice}} 元/克；新购债券年化 {{BondRate}}%。
各区房价指数：{{#each HouseIdx}}{{Key}} {{printf "%.2f" .}} {{/each}}

■ 我的财务（三表摘要）
现金 {{Cash}} 元 ｜ 净资产 {{NetWorth}} 元 ｜ FI 指数 {{FI}}
上月：收入 {{Income}}（工资 {{Salary}} + 被动 {{Passive}}）→ 支出 {{Expense}} → 净现金流 {{Net}}
资产：{{#each Assets}}{{Name}}×{{Units}}（{{ValueCNY}} 元） {{/each}}
负债：{{#each Loans}}{{KindCN}} 余额 {{Balance}}（月供 {{MonthlyPayment}}，剩 {{MonthsLeft}} 期） {{/each}}
资源：精力 {{Energy}}/10 ｜ 人脉 {{Network}}/10 ｜ 认知 {{Cognition}}/10 ｜ 信用分 {{CreditScore}}
家庭：{{MaritalCN}}{{#if Children}}，{{Children}} 个孩子{{/if}} ｜ 本月剩余动作次数 {{ActionBudget}}

■ 最近发生
{{#each RecentEvents}}- {{Text}}
{{/each}}
{{#if RecentLedger}}■ 最近流水
{{#each RecentLedger}}- {{From}} → {{To}}：{{AmountCNY}} 元（{{Category}}）
{{/each}}{{/if}}

■ 同场玩家
{{#each Peers}}- {{Seat}} 号位 {{Nickname}}（{{ProfessionTitle}}，{{DistrictCN}}，净资产 {{NetWorth}}，FI {{FI}}）
{{/each}}

请决定本月怎么做（≤3 个动作 + 可选 1 次 speak），然后调用 submit_month。
```

## 6. 月度决策循环

`run.go::OnMonthStart`（由 `game/wealth/agent_runner.go` 在每月 acting 广播后按座位异步唤醒；
房间级并发信号量沿用狼人杀 `RoomLLMConcurrency` 经验，默认 4）：

```
1. 取 GameContext 快照（锁内构造，锁外消费）
2. 组 prompt：SystemPrompt(职业卡) + UserPrompt(月度事件) + Memory 历史窗口（见 §8）
3. 调 LLM（流式 ChatStreamAccumulate，§9）
4. 解析 tool_use 块 → 逐个 DispatchTool：
     · 动作类工具 ≤ BotMaxActionsPerMonth(3)；超出直接丢弃并记录
     · speak ≤1 次/月 + SpeakLimiter 节流（30s 令牌桶，复用 agent/core）
     · 每个工具结果作为 tool_result 块回填，进入下一轮 LLM（≤3 轮），直到模型调用 submit_month 或无工具
5. submit_month（或 watchdog 超时强制）：写 Memory，更新 BotTranscripts（last_decision_summary /
   last_tool_input / last_tool_result / heart_thought → game.state.bot_contexts）
6. 发言路径：speak 文本经 chatSvc.SendFromBot 走既有广播（不走 WS；与狼人杀一致）
```

## 7. 工具集（17 个）

`tools.go::BuildTools()` 返回 17 个 `types.ToolDef`（Anthropic wire；tool_use 块四键约束 §14.1）。
`InputSchema` 摘要如下（完整 JSON Schema 按此实现；`required` 列即必填）：

| # | 工具 | 参数（type, 约束） | required | 说明 |
|---|------|-------------------|----------|------|
| 1 | `check_state` | `{}` | — | 返回本人三表/资产/贷款/资源摘要文本 |
| 2 | `buy_asset` | `{asset:"stock_index"\|"bond"\|"gold", amount_cny:int≥1000}` | asset, amount_cny | 按市价整份买入 |
| 3 | `sell_asset` | `{asset:同上\|"house:<d>"\|"shop:<d>", units:number≥1}` | asset, units | 房产整售 units=1 |
| 4 | `buy_house` | `{district:8 区 id 之一, downpay_ratio:number∈[0.3,1.0]}` | district, downpay_ratio | 首付现金 + 30 年房贷 |
| 5 | `take_loan` | `{kind:"consumer"\|"credit"\|"business", amount_cny:int}` | kind, amount_cny | credit 需 50000/100000/200000 |
| 6 | `repay_loan` | `{loan_id:"L< n >", amount_cny:int}` | loan_id, amount_cny | 提前还本 ≥1 万或结清 |
| 7 | `start_side_business` | `{kind:"delivery"\|"content"\|"tutoring"\|"freelance"}` | kind | 门槛见协议 §4 |
| 8 | `stop_side_business` | `{}` | — | 停止副业 |
| 9 | `study` | `{}` | — | 2000 元 / 精力-1 / 认知+1 |
| 10 | `socialize` | `{}` | — | 1000 元 / 人脉+1 |
| 11 | `rest` | `{}` | — | 精力+2 |
| 12 | `work_overtime` | `{}` | — | 精力-2 / 当月工资×0.3 奖金 |
| 13 | `move_district` | `{district:8 区 id 之一}` | district | 3000 元 / 精力-1 / token 迁移 |
| 14 | `consume` | `{amount_cny:int≥1, reason:string≤40}` | amount_cny | 自由消费（记事） |
| 15 | `donate` | `{amount_cny:int≥1000}` | amount_cny | 社会贡献分 + 人脉（前 3 次） |
| 16 | `speak` | `{text:string 1–100, internal_thought:string≤200}` | text | 公屏发言；内心独白入 bot_contexts |
| 17 | `submit_month` | `{}` | — | 结束本月（必须调用） |

DispatchTool 的校验 / 费用 / Ledger 与协议文档 §4 **逐行相同**（同一 `actions.go` 代码路径，
人类与 Agent 无分叉）。工具失败（350xx）作为 tool_result（`is_error:true`）回喂 LLM，允许其改选。

## 8. 记忆

`memory.go`（P0 局内记忆，不做跨局持久化）：

```go
type Memory struct {
    Monthly []MonthDecision   // 最近 24 月滚动：{Month, Actions[], SpeakText, CashAfter, NetAfter, FI, Summary}
    KeyFacts []string         // 资产/贷款/婚姻等大事（买房/破产/结婚/失业），上限 20 条
    CompactSummary string     // 超过 24 月后由规则式压缩生成（不调 LLM，P0；LLM 压缩 P2）
}
```

组装 prompt 时注入：最近 6 条 MonthDecision 全文 + CompactSummary + KeyFacts。
`heart_thought`（speak 的 internal_thought）与 `last_decision_summary`（模型每轮 text 首段，
100 字截断）写 `BotTranscripts` 供前端思维面板渲染。

## 9. 限流 / 超时 / 重试 / 流式

| 项 | 值 | 出处 |
|----|----|------|
| 月动作上限 | `BotMaxActionsPerMonth=3`（cfg） | P0 新定 |
| speak | 每月 ≤1 次；SpeakLimiter 30s 令牌桶（复用 `agent/core.NewSpeakLimiter`） | §15 同款 |
| 文本截断 | speak 100 字 / thought 200 字 / summary 120 字 | 狼人杀同款纪律 |
| LLM 调用 | **流式优先**：`ChatStreamAccumulate`（§197「接收到字节即刷新超时」）；工具循环 ≤3 轮/月 | §197 |
| 决策超时 | `AgentDecisionTimeoutSec=20`；watchdog 强制 submit_month（该月无动作，summary="timeout"） | P0 新定 |
| Provider 重试 | 5xx/429 由 `llm/anthropic` 内建重试；连续失败沿用狼人杀熔断策略（consecutiveFailures） | §14 |
| room 级并发 | 房间信号量默认 4（`RoomLLMConcurrency` 经验值迁移） | BUG-R242 教训 |
| 公平性 | 所有 Agent 代码完全相同，仅 ModelKey 不同；Memory / Transcript 全程可审计 | §15 公平性 |

## 10. 与狼人杀 / 德扑 Agent 的差异表

| 维度 | 狼人杀 wwplayer | 德扑 thpagent | **财商流 wealthplayer** |
|------|----------------|---------------|------------------------|
| AgentClassName | LsmAgentGame-Werewolf-Player | LsmAgentGame-TexasHoldem-Player | **LsmAgentGame-Wealth-Player** |
| 驱动时机 | 阶段事件（发言/投票/夜间） | 押注轮到时 | **月度节拍**（每月 acting 一次） |
| 单决策工具数 | 多（发言+技能+道具） | 每轮 1 个 tool_use | **≤3 动作 + 1 speak + submit_month** |
| 决策对象 | 阵营博弈 / 隐藏身份 | 底牌 / 赔率 / 位置 | **个人三表 + 市场周期 + 人生事件** |
| System prompt | 角色+局势 | 牌桌数学 | **职业卡全息画像（65 字段子集）+ 风险偏好** |
| 记忆 | 跨局 MEMORY.md 迭代 | 跨局 + 风格画像 | **P0 局内 24 月滚动 + 规则式压缩**（跨局 P2） |
| 数学辅助 | 猜疑链/多假说 | HandStrength/PotOdds 蒙特卡洛 | 无（FI/月结由服务端权威计算，Agent 只读） |
| 发言 | 核心玩法（限流 2/分） | 辅助（4 次/手牌） | 辅助（1 次/月，人味聊天） |
| 压力测试 | 13 bot 并发 | 6 bot | 8 bot × 420 月 ≈ 3360 次决策/局（P3 规模化重点） |
| 裁判 Agent | Judge（已接线） | Judge（预留未实现） | **P0 不设**（引擎确定性结算，无需裁判） |

---

## 更新日志

- v1（2026-09-14）：首版。AgentClassName 登记 / 包结构 / GameContext / prompt 模板全文 / 17 工具 / 月度循环 / 限流超时 / 差异表。
