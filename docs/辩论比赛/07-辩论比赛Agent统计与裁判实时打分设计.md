# 辩论比赛 Agent 统计与裁判实时打分设计

> 2026-08-31 §20260831-09 — 第九期增补。本文档定义:
>
> 1. 每个 Agent 的 LLM 调用次数 / Token 使用量统计(辩方 + 裁判 + 解说);
> 2. 整间房所有 Agent 的聚合统计;
> 3. 整间房每小时消耗 Token 的实时速率;
> 4. 裁判对各队 5 维度实时打分的累计面板(每段对话/每阶段更新);
> 5. 前端 UI 设计与 WS 帧协议。

## 1. 设计动机

参考狼人杀 13 人局 `GameStatusHeader` 已实现的「Agent 调用次数 / Token 消耗 / 每小时 Token 速率」(§17 §20260817-03 U3、§20260810-05、§20260830-02),辩论比赛作为同级别多人 AI 游戏,缺少等价的实时观测手段:

| 维度 | 狼人杀现状 | 辩论比赛现状 |
|------|-----------|-------------|
| 每 bot 调用次数 | ✅ BotTranscript.TotalLLMCalls | ❌ 无 |
| 每 bot Token 输入/输出/总 | ✅ agent_stats.total_*_tokens | ❌ 无 |
| 房间级聚合 | ✅ WerewolfRoom.AggregateAgentStats | ❌ 无 |
| 法官 Token 统计 | ✅ judge_total_*_tokens | ❌ 无 |
| 每小时速率 | ✅ ww-token-rate chip | ❌ 无 |
| 裁判对每队每维度累计分 | n/a(无 5 维度评分) | ❌ 仅最终结果可见 |

**本设计目标**:让观众/房主在一场比赛进行中即可观察到:

- 「正方一辩已经调了 LLM 12 次 / 输入 8.5K / 输出 2.1K / 总 10.6K Token」
- 「整间房目前累计 87K Token / 已经运行 5 分 12 秒 / 速率 ≈ 1M Token/小时」
- 「裁判 1 已经对正方提交了:论证质量 8,逻辑严谨 7,...」

提升透明度的同时,也便于房主/观众评估"是否值得继续看这场辩论"。

## 2. 数据结构

### 2.1 Bot Token 统计(辩方 + 解说)

**文件**: `ServerGo/agent/debateplayer/agent_llm_stats.go`(新增)

参考 `ServerGo/agent/wwplayer/agent_llm_stats.go` 的同款设计:

```go
// agentTokenStats 单 Agent Token + API 统计快照(跨包传递)。
type agentTokenStats struct {
    TotalInputTokens  int
    TotalOutputTokens int
    TotalAPITokens    int
    APICallCount      int
    APISuccessCount   int
    APIFailCount      int
    LastInputTokens   int
    LastOutputTokens  int
    LastAPITokens     int
}
```

在 `debateplayer.Agent` 上加锁保护 9 个累计字段,新增以下方法:

- `MarkLLMCallStart()` — 调 Chat 前调用,记录 llmCallInProgress
- `MarkLLMCallEndWithUsage(usage llm.LLMUsage)` — 调 Chat 成功后调用,累加 Token
- `RecordAPIFailure()` — 失败路径调用,+1 apiFailCount
- `AgentTokenStats() agentTokenStats` — 返回快照
- `TotalLLMCalls() int` — 返回本 bot 本局累计 LLM 调用次数

### 2.2 Judge Token 统计(裁判)

**文件**: `ServerGo/agent/debatejudge/judge_llm_stats.go`(新增)

参考 `ServerGo/agent/wwjudge/judge.go::judgeTokenStats` 同款设计:

```go
type judgeTokenStats struct {
    TotalInputTokens  int
    TotalOutputTokens int
    TotalAPITokens    int
    APICallCount      int
    APISuccessCount   int
    APIFailCount      int
    LastInputTokens   int
    LastOutputTokens  int
    LastAPITokens     int
}
```

在 `debatejudge.AgentJudge` 上加锁保护,新增 `recordJudgeAPIStat(usage, success)` + `JudgeTokenStats() judgeTokenStats`。

### 2.3 房间级聚合

**文件**: `ServerGo/game/debate/view.go` + 新增 `aggregate_stats.go`

```go
// DebateRoomAgentStats 房间级 Agent 统计聚合(对外下发的 wire 类型)。
type DebateRoomAgentStats struct {
    // 辩方 Agent 聚合
    BotCount           int `json:"bot_count"`
    BotTotalInputTokens  int `json:"bot_total_input_tokens"`
    BotTotalOutputTokens int `json:"bot_total_output_tokens"`
    BotTotalAPITokens    int `json:"bot_total_api_tokens"`
    BotAPICallCount      int `json:"bot_api_call_count"`
    BotAPISuccessCount   int `json:"bot_api_success_count"`
    BotAPIFailCount      int `json:"bot_api_fail_count"`

    // 裁判 Agent 聚合
    JudgeCount           int `json:"judge_count"`
    JudgeTotalInputTokens  int `json:"judge_total_input_tokens"`
    JudgeTotalOutputTokens int `json:"judge_total_output_tokens"`
    JudgeTotalAPITokens    int `json:"judge_total_api_tokens"`
    JudgeAPICallCount      int `json:"judge_api_call_count"`
    JudgeAPISuccessCount   int `json:"judge_api_success_count"`
    JudgeAPIFailCount      int `json:"judge_api_fail_count"`

    // 全房间总聚合(辩方 + 裁判)
    TotalInputTokens  int `json:"total_input_tokens"`
    TotalOutputTokens int `json:"total_output_tokens"`
    TotalAPITokens    int `json:"total_api_tokens"`
    TotalAPICallCount int `json:"total_api_call_count"`

    // 房间运行
    ElapsedSec      int64 `json:"elapsed_sec"`      // 已运行秒数(从 startedAt 计)
    TokensPerHour   int64 `json:"tokens_per_hour"`  // 派生:total_api_tokens / (elapsed_sec / 3600)
    ShowTokenRate   bool  `json:"show_token_rate"`  // 守卫:elapsed_sec < 60 时 false
}
```

**采集机制**:

- 通过 `DebateRoom.agentRegistry` interface 增加 `BotStats()` / `JudgeStats()` 方法。
- 房间级聚合函数 `r.AggregateAgentStats()`(参考 `WerewolfRoom.AggregateAgentStats` 的 §92a 范式):
  - 公开变体自己取 `r.mu`;
  - 锁内变体 `aggregateAgentStatsLocked()` 仅读 `r.agentRegistry`(registry 内部取 Bot.mu / Judge.mu,层级不同,无锁序倒置)。
- `BuildClientState` 中调用 `aggregateAgentStatsLocked()` 填充 `cs.AgentStats`。

### 2.4 每个 Agent 详细统计(用于 UI 卡片)

```go
// AgentTokenSnapshot 单 Agent 详细统计(用于前端每个 Bot 卡片)。
type AgentTokenSnapshot struct {
    TeamID         int    `json:"team_id"`
    Seat           int    `json:"seat"`
    Role           Role   `json:"role"`
    ModelKey       string `json:"model_key"`
    LLMCallCount   int    `json:"llm_call_count"`
    InputTokens    int    `json:"input_tokens"`
    OutputTokens   int    `json:"output_tokens"`
    APITokens      int    `json:"api_tokens"`
    APISuccessCount int   `json:"api_success_count"`
    APIFailCount    int   `json:"api_fail_count"`
}

// JudgeTokenSnapshot 单裁判详细统计。
type JudgeTokenSnapshot struct {
    JudgeID         int    `json:"judge_id"`
    ModelKey        string `json:"model_key"`
    LLMCallCount    int    `json:"llm_call_count"`
    InputTokens     int    `json:"input_tokens"`
    OutputTokens    int    `json:"output_tokens"`
    APITokens       int    `json:"api_tokens"`
    APISuccessCount int    `json:"api_success_count"`
    APIFailCount    int    `json:"api_fail_count"`
}

// DebateAgentStatsDetail 单房间聚合 + 每个 Agent 详细。
type DebateAgentStatsDetail struct {
    Aggregate DebateRoomAgentStats  `json:"aggregate"`
    Bots      []AgentTokenSnapshot  `json:"bots"`
    Judges    []JudgeTokenSnapshot  `json:"judges"`
}
```

**采集路径**:
- `DebateRoom.agentRegistry` interface 增 `BotStats() []AgentTokenSnapshot` / `JudgeStats() []JudgeTokenSnapshot`;
- `debaterun.Registry` 实现这两个方法:遍历 reg.bots / reg.judges 调用对应 Bot/Judge 的 TokenStats 方法。

## 3. 裁判实时打分累计

### 3.1 现有机制

当前 `AddJudgeScore(score JudgeScore)` 仅在 `PhaseJudging` 阶段调用一次 `submit_score` 工具时记录,然后通过 `emitJudgeScore(score)` 推送 `debate.judge_vote` 帧。

**问题**:观众在评审阶段开始时**看不到**裁判"实时"评审过程,只能看到最终一次性提交。

### 3.2 新增阶段式实时打分

为了让裁判在每个阶段(立论后 / 驳论后 / 质询后 / 自由辩后)就给出**当前阶段的临时打分**,引入:

**新工具** `submit_stage_score`(仅裁判在评审阶段可用):

```go
// ToolJudgeSubmitStageScore 裁判提交"阶段性实时打分"。
// 区别于 submit_score:不锁定为最终提交,可在后续阶段继续更新。
// 字段:current_phase(阶段名)、dimensions(5 维度分)、comment(评语)
ToolJudgeSubmitStageScore ToolName = "submit_stage_score"
```

**新增 WS 帧**: `debate.stage_score`(由 SubmitStageScore 触发)。

### 3.3 数据结构

```go
// StageScore 单裁判在某个阶段的临时打分(可被同裁判后续阶段覆盖)。
type StageScore struct {
    JudgeID        int               `json:"judge_id"`
    ModelKey       string            `json:"model_key"`
    Phase          Phase             `json:"phase"`           // 提交打分时所在的阶段
    PhaseCN        string            `json:"phase_cn"`
    TeamScores     []TeamRanking     `json:"team_scores"`     // 每队 5 维度 + comment + best_debater
    WinnerTeamID   int               `json:"winner_team_id"`  // 当前阶段倾向胜方(可被后续覆盖)
    OverallComment string            `json:"overall_comment"`
    SubmittedAtMS  int64             `json:"submitted_at_ms"`
    IsFinal        bool              `json:"is_final"`        // true = submit_score 的最终版本
}

// JudgeScoreboard 实时打分看板:某裁判对各队的累计打分。
// 字段按 5 维度 + 总分累加;每收到一份 stage_score,增量更新本裁判分数。
type JudgeScoreboard struct {
    JudgeID        int                          `json:"judge_id"`
    ModelKey       string                       `json:"model_key"`
    TeamScores     map[int]*AccumulatedTeamScore `json:"team_scores"` // team_id → 累计
    StageHistory   []StageScore                 `json:"stage_history"` // 该裁判的提交历史(限最近 10 条)
}

type AccumulatedTeamScore struct {
    TeamID                 int                `json:"team_id"`
    ArgumentQuality        float64            `json:"argument_quality"`        // 5 维度(累计平均)
    LogicRigor             float64            `json:"logic_rigor"`
    LanguageExpression     float64            `json:"language_expression"`
    TeamCoordination       float64            `json:"team_coordination"`
    RebuttalEffectiveness  float64            `json:"rebuttal_effectiveness"`
    TotalScore             float64            `json:"total_score"`             // 0-50
    LatestComment          string             `json:"latest_comment"`
    LatestPhase            Phase              `json:"latest_phase"`
    LatestPhaseCN          string             `json:"latest_phase_cn"`
    SubmissionCount        int                `json:"submission_count"`        // 该裁判对该队的累计提交次数
}
```

**累计算法**(每次收到 stage_score 时):

```
对每个 team:
  旧 total = TeamScores[team].TotalScore
  新维度分 = (旧维度分 × 旧 submissionCount + 本次维度分) / (旧 submissionCount + 1)
  新 total = 5 个新维度分之和
  submissionCount += 1
  LatestComment = 本次 comment
  LatestPhase = 本次 phase
```

**最终得分**(`submit_score` 工具调用):把 `IsFinal=true` 写入 `StageScore`,且 scoreboard 标记 `finalized=true`,后续同裁判提交只追加 `stage_history`,不再覆盖累计。

### 3.4 前端展示

**新组件** `DebateJudgeScoreboardPanel.tsx`:

- 每个裁判一个卡片(展开/折叠);
- 卡片内按队伍分 5 列(或 2/3/4/5 列响应式);
- 每列显示该裁判对当前队的累计 `TotalScore`(大字)+ 5 维度小条;
- 下方 `📜 阶段历史` 折叠区:按提交时间倒序展示最近 10 条 stage_score,每条带阶段标签 + 评语摘要。

## 4. WS 帧协议

### 4.1 阶段打分帧 `debate.stage_score`

```json
{
  "room_id": "debate_abc123",
  "judge_id": 1,
  "model_key": "Claude-model",
  "phase": "opening_argument",
  "phase_cn": "开篇立论",
  "team_scores": [
    {
      "team_id": 0,
      "scores": { "argument_quality": 8, "logic_rigor": 7, ... },
      "total_score": 38.0,
      "comment": "立论结构清晰,...",
      "best_debater": 0
    }
  ],
  "winner_team_id": 0,
  "overall_comment": "开篇阶段正方略占上风",
  "submitted_at_ms": 1725123456789,
  "is_final": false
}
```

**触发时机**:`debatejudge.AgentJudge.dispatchSubmitStageScore` 派发成功后。

### 4.2 Agent 统计帧 `debate.stats_update`

```json
{
  "room_id": "debate_abc123",
  "aggregate": { /* DebateRoomAgentStats */ },
  "bots":      [ /* AgentTokenSnapshot[] */ ],
  "judges":    [ /* JudgeTokenSnapshot[] */ ],
  "elapsed_sec": 312,
  "tokens_per_hour": 1014231
}
```

**触发时机**:
- 阶段切换时(每次 `engine.advanceTo`);
- 每 10 秒定时推送(由 DebateEngine 维护 ticker);
- WS 客户端首次 `debate.subscribe` 时下发一次(初始化)。

### 4.3 裁判实时打分帧 `debate.judge_scoreboard`

```json
{
  "room_id": "debate_abc123",
  "judge_id": 1,
  "model_key": "Claude-model",
  "team_scores": {
    "0": { "team_id": 0, "total_score": 38.0, "submission_count": 2, ... },
    "1": { "team_id": 1, "total_score": 35.5, "submission_count": 2, ... }
  },
  "stage_history": [
    { "phase": "opening_argument", "phase_cn": "开篇立论", "submitted_at_ms": 1725123456789, ... }
  ]
}
```

**触发时机**:`submit_stage_score` 派发成功后,与 `debate.stage_score` 同时推送(同一 hook 串联)。

## 5. 前端组件

### 5.1 新增组件

| 文件 | 功能 |
|------|------|
| `ClientWeb/src/components/debate/DebateAgentStatsPanel.tsx` | 房间级聚合 + 每小时速率(参考狼人杀 GameStatusHeader token chip) |
| `ClientWeb/src/components/debate/DebateBotTokenCard.tsx` | 单个 Bot 卡片:模型名 + 调用次数 + 输入/输出/总 token |
| `ClientWeb/src/components/debate/DebateJudgeScoreboardPanel.tsx` | 裁判实时打分看板(每队每维度累计) |
| `ClientWeb/src/components/debate/DebateStageScoreTimeline.tsx` | 阶段打分时间线(最近 N 条 stage_score) |

### 5.2 改造组件

| 文件 | 改造内容 |
|------|---------|
| `DebateGamePage.tsx` | 中列加入 `<DebateAgentStatsPanel/>`(房间顶部) + `<DebateJudgeScoreboardPanel/>`(裁判列下方) |
| `DebateStage.tsx` | sticky header 增加 ⏱ 运行时长 + ⚡ Token 速率(参考 ww-token-rate) |
| `ClientWeb/src/store/debate.store.ts` | 新增 `agentStats` / `botStats` / `judgeStats` / `stageScores` / `scoreboards` |
| `ClientWeb/src/types/debate.ts` | 新增对应 TS interface |
| `ClientWeb/src/hooks/useDebate.ts` | 订阅 `debate.stats_update` / `debate.stage_score` / `debate.judge_scoreboard` |

### 5.3 样式

新建 `ClientWeb/src/styles/debate-stats.css`,在 `globals.css` 末尾追加 `@import`:

```css
@import './debate-stats.css';
```

样式沿用狼人杀 chip 设计语言(`ww-game-status-header__chip--xxx`)的同款语义:`debate-stats__chip--tokenrate` / `debate-stats__chip--agent` / `debate-stats__chip--judge`。

## 6. 持久化

聚合统计(纯内存态)跟随 `DebateRoom` 生命周期,房间关闭后随 GC 释放。**不进 DB**:

- 与狼人杀 `WerewolfRoom.AggregateAgentStats` 行为一致;
- §20260831-06 `ModelStats` 是跨房间跨局的模型胜率统计(已落库),本设计的 Token 统计仅本局,不需持久化。

## 7. §130 自检

新增字段 / 新增 hook / 新增 WS 帧 **必须** 全链路接线:

```bash
# 后端
grep -rn "AgentTokenStats\|JudgeTokenStats\|AggregateAgentStats\|SubmitStageScore\|StageScore" ServerGo/

# 前端
grep -rn "AgentTokenSnapshot\|JudgeTokenSnapshot\|StageScore\|agentStats\|stageScores" ClientWeb/src/
```

零命中即 §130 复发,P1 缺陷。

## 8. 验收标准

1. 一场 2 队 3 裁判辩论比赛中:
   - 每个辩方 Bot 卡片显示调用次数 + 输入/输出 Token(实时滚动);
   - 顶部 chip 显示房间总 Token + 每小时速率(开局 ≥60s 后显示);
   - 裁判评分卡内每队 5 维度条形图 + 大字 total_score,阶段变化时实时更新;
   - 评审阶段结束后,`StageScore.IsFinal=true`,分数冻结。

2. `go build ./...` + `go test ./...` 通过;
   `tsc --noEmit` + `npm run build` 通过。

3. AutoTestAndSaveReport_Debate.sh 全流程通过,无 P0/P1 缺陷。

## 9. 文档索引

| 文档 | 章节 |
|------|------|
| `docs/辩论比赛/00-辩论比赛总体架构设计.md` | §3.3 view + §4.2 WS 帧 |
| `docs/辩论比赛/02-辩论比赛Agent设计.md` | §2.6 Action 集合 + §5.1 LLM 调用 |
| `docs/辩论比赛/06-辩论比赛公平性与评审系统设计.md` | §4 评审阶段 + §9 模型胜率 |
| `CLAUDE.md` §17 | 狼人杀 GameStatusHeader token chip 范式 |
| `CLAUDE.md` §92a | 锁内/锁外变体范式 |
| `CLAUDE.md` §130 | 「声明了却从不接线」自检 |
