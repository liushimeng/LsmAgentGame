# 狼人杀 13 人局 — 游戏信息与 Agent 现状综合文档

> **文档版本**：2026-08-08（基于 main 分支 HEAD `f3e916d`）
> **目的**：为 AI Agent Tools（Claude Code / Kilo Code / OpenCode / pi / OpenClaw）提供狼人杀 13 人局的完整游戏规则、角色定义、Agent 实现现状、道具系统、前后端架构、关键设计决策、Bug 教训等一站式参考。
> **关联规则文件**：[`CLAUDE.md`](CLAUDE.md) §15–§25

---

## 1. 游戏基本规则

### 1.1 13 人标准竞技局配置

| 阵营 | 角色 | 人数 |
|------|------|------|
| 狼人阵营 | 普通狼人 | 4 |
| 好人阵营·神职 | 预言家 | 1 |
| 好人阵营·神职 | 女巫 | 1 |
| 好人阵营·神职 | 猎人 | 1 |
| 好人阵营·神职 | 白痴 | 1 |
| 好人阵营·神职 | 随机神职 2~3 个（从 godRolePool 抽取） | 2~3 |
| 好人阵营·平民 | 普通平民 | 5~6 |

**总计**：4 狼 + 1 预言家 + 2~3 神职 + 5~6 平民 = 13 人

> 历史兼容：12 人局（4 狼 + 1 预言家 + 1 女巫 + 1 猎人 + 1 白痴 + 4 平民）、7 人局（2 狼 + 1 预言家 + 1 女巫 + 1 猎人 + 2 平民）

### 1.2 胜负规则（屠边）

- **狼人阵营胜利**：杀光所有神职（4 名）或杀光所有平民（5 名）
- **好人阵营胜利**：放逐全部 4 名狼人

### 1.3 阶段流转

```
PhaseFilling（等待入座）
  ↓ 13 人坐满
PhasePreWolves（首夜发言缓冲期）
  ↓ 缓冲结束
PhaseNightGuard（守卫守护·盲守，在狼刀之前）
  ↓ 无守卫或守卫已死则自动跳过
PhaseNightWolves（狼人协商刀人）
  ↓
PhaseNightSeer（预言家查验）
  ↓
PhaseNightWitch（女巫用药）
  ↓
PhaseNightDemonHunter（猎魔人狩猎·仅第 2 晚起）
  ↓ 无猎魔人或猎魔人已死则跳过
PhaseDawn（黎明：公布死亡 + 警徽流结算）
  ↓
PhaseSheriff（警长竞选·仅第一天）
  ↓
PhaseSpeak（白天轮流发言）
  ↓
PhaseVote（白天投票放逐）
  ↓ 若最高票为白痴
PhaseIdiotReveal（白痴翻牌结算）
  ↓ 若猎人出局
PhaseHunterShoot（猎人开枪）
  ↓ 若有人出局
PhaseDeathLyric（遗言：前 2 轮出局者有遗言）
  ↓ 回到 PhaseNightGuard（循环）
PhaseRestartVote（重开局投票·5 分钟窗口）
  ↓
PhaseGameOver（对局结束 → 冷却期 30 分钟）
```

### 1.4 特殊规则

- **警长**：1.5 票，平票默认不投死（无人出局直接进黑天）
- **警徽流**：预言家警长夜间死亡后按真实验人结果结算金水/查杀/撕警徽
- **白痴翻牌**：仅白天投票放逐时触发免死，失去投票权但仍存活发言
- **猎人开枪**：被狼刀/被投票出局时可开枪带人；被女巫毒死不能开枪
- **狼人自爆**：白天发言阶段自爆，直接进黑天，出局且无遗言
- **遗言**：前 2 轮（DayNumber ≤ 2）出局者有遗言，之后夜死无遗言
- **同守同救**：守卫与女巫同时保护同一人 → 该玩家仍死亡
- **盲守**：守卫看不到当晚狼刀目标，必须预判狼意图
- **死亡语义二分**：`execution`（处决：投票/自爆/骑士自决/猎魔人误杀）vs `death`（死亡：狼刀/毒杀/猎人反杀）

---

## 2. 角色定义与实现状态

### 2.1 Role 枚举（`ServerGo/game/werewolf/cards.go:49-71`）

| 枚举值 | 英文名 | 阵营 | 技能 | 实现状态 |
|--------|--------|------|------|---------|
| `RoleWerewolf` | werewolf | 狼人 | 夜间协商刀人 | ✅ 全链路 |
| `RoleSeer` | seer | 好人·神职 | 每晚查验一名玩家 | ✅ 全链路 |
| `RoleWitch` | witch | 好人·神职 | 解药+毒药各一次 | ✅ 全链路 |
| `RoleHunter` | hunter | 好人·神职 | 出局可开枪带人 | ✅ 全链路 |
| `RoleIdiot` | idiot | 好人·神职 | 被投票放逐可翻牌免死 | ✅ 全链路 |
| `RoleVillager` | villager | 好人·平民 | 无特殊技能 | ✅ 全链路 |
| `RoleGuard` | guard | 好人·神职 | 每晚守护一人·不可连守·盲守 | ✅ 全链路（§134） |
| `RoleKnight` | knight | 好人·神职 | 白天翻牌决斗·每局限一次 | ✅ 全链路（§198） |
| `RoleDemonHunter` | demon_hunter | 好人·神职 | 第 2 晚起每晚狩猎·发动即公开身份 | ✅ 全链路（§猎魔人） |
| `RoleMagician` | magician | 好人·神职 | 每晚交换两人号码牌 | ⚠️ 已退役（2026-07-29） |
| `RoleMerchant` | merchant | 好人·神职 | 每晚赋予幸运儿技能 | ⚠️ 已退役 |
| `RoleDreamer` | dreamer | 好人·神职 | 每晚选梦游者免疫夜间伤害 | ⚠️ 已退役 |
| `RoleCrow` | crow | 好人·神职 | 每晚诅咒一人多一票 | ⚠️ 已退役 |
| `RoleScarecrow` | scarecrow | 好人·神职 | 每晚得知被放逐者身份 | ⚠️ 已退役 |
| `RolePrince` | prince | 好人·神职 | 被放逐时可翻牌逆转投票 | ⚠️ 已退役 |
| `RolePureWhite` | pure_white | 好人·神职 | 查到狼人则其出局 | ⚠️ 已退役 |

### 2.2 活动卡池 godRolePool（`cards.go:340-347`）

当前活动神职池包含 **6 个完整实现的角色**，每次 13 人局随机抽取 2~3 个：

```go
var godRolePool = []Role{
    RoleWitch,        // 女巫
    RoleHunter,       // 猎人
    RoleIdiot,        // 白痴
    RoleGuard,        // 守卫（§134）
    RoleKnight,       // 骑士（§198 重新加入）
    RoleDemonHunter,  // 猎魔人（§猎魔人 重新加入）
}
```

**退役角色**（仅保留 wire 兼容，不再发牌）：魔术师/奇迹商人/射梦人/乌鸦/稻草人/定序王子/纯白之女

### 2.3 发牌逻辑（`RandomDeck13`）

1. **固定**：4 狼人 + 1 预言家（必出）
2. **随机**：从 godRolePool 抽取 2~3 个神职
3. **补齐**：剩余全部平民（≥ 5 名），第 13 人强制为平民
4. **座位置换**：支持 `PreferredRoles` 自选角色（§20260806-03）

---

## 3. 后端引擎架构

### 3.1 文件清单（`ServerGo/game/werewolf/`）

| 文件 | 行数 | 职责 |
|------|------|------|
| `engine.go` | 1054 | Phase 枚举、Player 结构、GameState、DeathCause/Verdict、RolePubliclyRevealed |
| `engine_night.go` | — | 夜晚阶段逻辑（startNight/endWolfPhase/endSeerPhase/endWitchPhase/endGuardPhase） |
| `engine_day.go` | 598 | 白天阶段逻辑（advanceDay/tallyVotes/checkVictory） |
| `engine_state.go` | ~200 | GameState 辅助方法、配置读取（cfgPhaseDeadlineSec/isActingPhase） |
| `engine_restart_vote.go` | — | 重开局投票逻辑 |
| `engine_death_lyric.go` | — | 遗言阶段逻辑 |
| `cards.go` | ~400 | Role/Faction 枚举、godRolePool、RandomDeck13、AssignRoles |
| `room.go` | 1599 | WerewolfRoom 核心结构、生命周期 |
| `room_agent.go` | 1694 | Agent 与引擎的桥接（buildAgentContextLocked/StartAgentsLocked/stopAgentsLocked） |
| `room_action.go` | 992 | 玩家动作入口（Action_WolfKill/Action_SeerCheck/Action_Witch/Action_GuardProtect 等） |
| `room_watchdog.go` | 919 | Phase Watchdog（5s 轮询、90s 兜底、speak_floor、phase deadline） |
| `room_manage.go` | 581 | 房间管理（CreateRoomWithAgents/ForceStartIfReady） |
| `room_chat.go` | — | 聊天队列（ChatHistoryQueue + ReadPointer） |
| `room_cooling.go` | — | 冷却期（30 分钟冷却 watchdog） |
| `room_restart_vote.go` | — | 重开局投票房间级逻辑 |
| `room_quarantine_skip_locked.go` | — | Quarantine skip 逻辑 |
| `room_prop.go` | 652 | 道具房间级逻辑（propInjectQueue/drainPropInjectQueueLocked） |
| `room_filling_reaper.go` | — | filling 房间回收器 |
| `room_config.go` | — | 房间配置 |
| `view.go` | 1174 | ClientGameState 构建（BuildClientStateWithRoom/SpectatorView） |
| `agent_runner.go` | 1758 | Agent runner（agentRunner 串行派发/quarantine/限流） |
| `activity_emitter.go` | 883 | 活动事件发射（EmitVoteResult/EmitWolfKill/EmitPropUse 等） |
| `speak_filter.go` | — | 发言过滤（ScrubIdentityLeak/StripLLMInternalTags） |
| `speak_floor.go` | — | 发言下限 watchdog |
| `speak_mystery.go` | — | 公屏猜疑化 |
| `sheriff_stream.go` | — | 警徽流逻辑 |
| `wolfpack_room.go` | — | 狼队交流（WolfPackRoom + wolf_whisper） |
| `econ_tier.go` | — | 经济档位（Health/Caution/Danger） |
| `prop_catalog.go` | ~400 | 道具目录与注册表 |
| `prop_engine.go` | — | 道具引擎（UseProp/enqueuePropHitLocked） |
| `prop_effect.go` | — | 道具效果落地 |
| `prop_inject.go` | 611 | 道具注入文本生成（PropInjectPromptBlock/roleSpecificInduction） |
| `prop_service.go` | — | 道具 REST 服务 |
| `judge_summary_bridge.go` | 575 | 法官总结桥接（PersistSummary） |
| `agent_memory_bridge.go` | — | Agent 持久化记忆桥接（IterateAgentMemoriesAsync） |

**代码规模**：36,505 行（含测试），56 个 `.go` 文件

### 3.2 GameState 核心字段（`engine.go:260-399`）

```go
type GameState struct {
    SeatCount    int                    // 本局实际人数（13/12/7）
    Seats        [13]string             // 座位 → userID
    Players      [13]Player             // 座位 → Player
    Roles        [13]Role               // 座位 → 角色
    SheriffSeat  Seat                   // 当前警长座位
    Phase        Phase                  // 当前阶段
    DayNumber    int                    // 第几天
    Winner       string                 // "wolf" | "good" | ""
    Status       string                 // "playing" | "over"

    // 夜晚状态
    WolfKillTarget     Seat             // 狼刀目标（NoSeat = 空刀）
    GuardProtectTarget Seat             // 守卫守护目标
    GuardLastProtect   Seat             // 上晚守护目标（跨夜保留）
    SameGuardSameSave  Seat             // 同守同救牺牲者
    WitchSavedTarget   Seat             // 女巫解药救的人
    DemonHunterHuntTarget Seat          // 猎魔人狩猎目标

    // 狼人夜间投票
    WolfVotes     [13]Seat              // 每个狼人的投票
    WolfVoteCast  [13]bool              // 是否已提交
    WolfVoteTally *WolfVoteTally        // 计票结果

    // 警徽流
    SheriffStreams   [2]Seat            // 第一/第二警徽流目标
    SheriffSuccessor Seat               // 警长死亡后的继承者

    // 遗言
    DeathLyricQueue   []Seat            // 遗言等待队列
    DeathLyricCurrent Seat              // 当前遗言座位

    // 重开局投票
    RestartVoteYes/No/Abstain map[Seat]bool
    RestartVoteDone    bool
    RestartVoteResult  string

    // 计数
    WolfAliveCnt / GoodAliveCnt / DivineCnt / PlainCnt int

    // 阶段时钟
    PhaseDeadlineAt time.Time           // 阶段截止时间

    // 首夜缓冲期
    FirstNightGraceEnd time.Time
}
```

### 3.3 Player 结构（`engine.go:101-146`）

```go
type Player struct {
    UserID / Seat / Role / Alive / IsSheriff / IsBot
    WitchAntidoteUsed / WitchPoisonUsed / LastSeerCheck
    Voted / VoteTarget / LastWords / HasSpoken / IdiotRevealed
    KnightDuelUsed / DemonHunterHuntUsed     // 技能使用标记
    DeathCause / DeathVerdict                // 死因 + 决断
    HunterFired                              // §135 猎人是否已开枪
    HumanDebuff *wwtypes.HumanDebuffSpec     // §20260807-04 人类反制道具 debuff
}
```

### 3.4 身份公开规则（§135 RolePubliclyRevealed）

仅以下 6 类事件公开身份，**死亡不自动翻牌**：

1. 终局复盘（`Status == "over"`）
2. 白痴翻牌（`IdiotRevealed`）
3. 狼人自爆（`DeathCause == "suicide"`）
4. 猎人开枪（`HunterFired`）
5. 骑士决斗（`KnightDuelUsed`）
6. 猎魔人发动（`DemonHunterHuntUsed`）

---

## 4. Agent 系统架构

### 4.1 包结构（2026-08-06 §Agent 重构）

```
ServerGo/agent/
├── class_names.go          AgentClassName 常量（3 个已注册）
├── doc.go                  包文档
├── core/                   通用基础设施
│   ├── chat_history.go     ChatHistoryQueue + ReadPointer（§111）
│   ├── llm_helpers.go      LLM 辅助函数
│   ├── ratelimit.go        令牌桶限流
│   ├── record_log.go       RecordLogService（模型对局日志）
│   └── speak_dedup.go      发言去重
├── wwtypes/                狼人杀共享类型
│   ├── context.go          GameContext 契约（253 行）
│   └── types.go            HumanDebuffSpec 等类型（111 行）
├── wwplayer/               玩家 Bot 实现
│   ├── agent.go            Agent 结构体 + 主循环（1795 行）
│   ├── run.go              agentRunner 串行派发（1956 行）
│   ├── run_llm.go          LLM 调用路径
│   ├── run_helpers.go      运行辅助
│   ├── run_rewake.go       重唤醒逻辑
│   ├── memory.go           Memory 多轮记忆（987 行）
│   ├── memory_iterate.go   持久化记忆迭代（§131）
│   ├── prompt.go           系统/用户提示词构建（714 行）
│   ├── tools.go            工具定义 + DispatchTool（1371 行）
│   ├── tools_prop.go       道具工具
│   ├── tools_registry.go   工具注册表（v5）
│   ├── tools_wolf.go       狼人工具
│   ├── tools_anthropic_wire.go  Anthropic 协议 wire 格式
│   ├── emotion.go          情绪模块（553 行）
│   ├── decision_summary.go 决策摘要
│   ├── speak_factcheck.go  发言事实检查
│   ├── speak_dedup_recent.go  近期发言去重
│   ├── whisper_factcheck.go  私聊事实检查
│   ├── prop_blocks.go      道具 prompt 块（339 行）
│   ├── prop_inspect.go     道具检查
│   └── retry_config.go     重试配置
└── wwjudge/                法官 Bot 实现
    ├── judge.go            AgentJudge 结构体 + 主循环（626 行）
    ├── judge_tools.go      法官工具定义（5 个工具）
    ├── judge_prompt.go     法官提示词
    ├── judge_summary.go    整局总结（425 行）
    ├── judge_summary.go    整局总结
    ├── judge_helpers.go    法官辅助
    └── judge_metadata.go   法官元数据
```

**代码规模**：~22,000 行（含测试），65 个 `.go` 文件

### 4.2 三种 AgentClassName（`class_names.go`）

| AgentClassName | 实现 | 场景 |
|---|---|---|
| `LsmAgentGame-Werewolf-Player` | `wwplayer.Agent` | 玩家 Bot 主对话 + speak_floor_tick |
| `LsmAgentGame-Werewolf-Judge` | `wwjudge.AgentJudge` | 法官宣告/prompt_actor/summary/declare_cause |
| `LsmAgentGame-Werewolf-MemoryIter` | `agent_memory_bridge.go` | 整局总结后异步自我迭代 MEMORY.md |

**User-Agent 出站格式**：`<AgentClassName>/<AppVersion> <buildDateTime>`

### 4.3 玩家 Bot 工具清单（ToolRunner 接口，`tools.go:26-120`）

| 工具名 | 参数 | 可用阶段 | 可用身份 |
|---|---|---|---|
| `wolf_kill` | `{target: int}` | night_wolves | 狼人（-1 空刀） |
| `seer_check` | `{target: int}` | night_seer | 预言家 |
| `witch_act` | `{action, target?}` | night_witch | 女巫 |
| `guard_protect` | `{target: int}` | night_guard | 守卫（§134） |
| `knight_duel` | `{target: int}` | speak | 骑士（§198） |
| `demon_hunter_hunt` | `{target: int}` | night_demon_hunter | 猎魔人 |
| `speak` | `{text: string}` | speak | 当前发言座位 |
| `speak_with_thought` | `{text, internal_thought}` | speak/pre_wolves | §119 心口不一 |
| `emotion_switch_speak` | `{text, emotion?, reason?}` | speak | §213 合并工具 |
| `finish_speak` | `{}` | speak | 当前发言座位 |
| `vote` | `{target: int}` | vote/sheriff | 存活玩家 |
| `finish_vote` | `{tied_round?}` | vote | 系统/平票 |
| `start_day` | `{}` | dawn | 任意（触发推进） |
| `sheriff_candidate` | `{target: int}` | sheriff | 参选玩家 |
| `sheriff_elect` | `{}` | sheriff | 系统 |
| `sheriff_stream` | `{slot, target}` | speak/sheriff/dawn | 警长（预言家） |
| `idiot_reveal` | `{choice: "reveal"/"skip"}` | idiot_reveal | 白痴 |
| `hunter_shoot` | `{target: int}` | hunter_shoot | 猎人（-1 不开枪） |
| `last_words` | `{text: string}` | death_lyric | 遗言座位 |
| `last_words_skip` | `{}` | death_lyric | 遗言座位 |
| `wolf_suicide` | `{}` | speak | 狼人（自爆） |
| `whisper` | `{to_seat, text}` | 任意 | 任意（限流） |
| `interject` | `{text: string}` | 非发言阶段 | 任意（插话） |
| `wolf_whisper` | `{text: string}` | 任意 | 狼人（§133 狼队交流） |
| `use_prop` | `{prop_key, target?}` | speak | 存活玩家（§132 道具） |
| `restart_vote` | `{choice}` | restart_vote | 存活玩家 |
| `idle_silent` | `{reason}` | 白名单 acting phase | 任意 |

### 4.4 法官 Bot 工具清单（`judge_tools.go`）

| 工具名 | 参数 | 用途 |
|---|---|---|
| `announce` | `{kind, text}` | 公开宣告（阶段切换开场白/黎明公布死亡） |
| `declare_cause` | `{seat, cause, verdict, text}` | 宣告死因 + 决断 |
| `prompt_actor` | `{seat, text}` | 强提示当前 acting bot |
| `summary` | `{outcome, key_moments, timeline, mvp, wolf_decoy_log}` | 整局 5 段式总结 |
| `idle_silent` | `{reason}` | 本阶段不出声 |

### 4.5 关键技术约束

| 编号 | 约束 | 来源 |
|------|------|------|
| §92a | `sync.Mutex` 不可重入，`WerewolfRoom` 方法必须提供 `*Locked` 锁内变体 | R212 自死锁 |
| §97 | 新增阶段必须同步更新 SkipPhaseAction / watchdogActingSeat / dispatchQuarantinedSkipLocked / isActingPhase / defaultPhaseDeadlineSec 五处 | 守卫/猎魔人 |
| §119 | `internal_thought` 协议层物理隔离，不进 chat_message 表/队列 | 心口不一 |
| §130 | 「两段式」：锁内记 `judgeWakeKind` 字符串，defer 在 Unlock 之后调 `wakeJudgeLocked` | 法官唤醒 |
| §197 | 流式续命：`parentCtx = WithTimeout(callTimeout + extendedTimeout)`，首字节后不杀 | 慢模型 |
| §134 | 守卫盲守：`GameContext.WolfTarget` 恒为 -1 | 守卫看不到狼刀 |
| §135 | 身份公开走 `RolePubliclyRevealed` 单一事实来源，禁止各自复制判定 | 死亡不翻牌 |

---

## 5. 道具系统（LLM 注入攻击游戏化）

### 5.1 6 类 LLM 注入攻击映射

| 攻击文档 | 攻击类型 | 已实现道具 |
|---|---|---|
| 第一种：Markdown 格式注入 | Agent→人类 | `markdown_bomb` + `md_bomb_human` |
| 第二种：提示词套娃 | Agent→人类 | `nested_maze` + `nested_maze_human` |
| 第三种：字符级欺骗 | Agent→人类 | `char_confuse` + `char_confuse_human` |
| 第四种：长上下文注意力失焦 | Agent→Agent | `long_swear`（AOE） |
| 第五种：任务马甲 | Agent→Agent | `task_disguise` + `task_disguise_v3` |
| 第六种：情绪操控 | Agent→Agent | `emotion_plea` |

### 5.2 道具清单（10 种，`prop_catalog.go`）

| PropKey | 中文名 | 价格 | 中招率 | 目标 | 效果类型 |
|---|---|---|---|---|---|
| `markdown_bomb` | 紧急公告 | 150 | 30% | any | expose_identity |
| `nested_maze` | 剧本迷宫 | 200 | 25% | any | expose_identity |
| `char_confuse` | 胡言乱语 | 100 | 20% | any | confuse_seer |
| `long_swear` | 长篇废话 | 250 | 35% | any（AOE） | attention_scatter + target_twist |
| `task_disguise` | 编剧委托 | 180 | 28% | any | expose_identity |
| `task_disguise_v3` | 编剧委托·进阶 | 180 | 35% | any | expose_identity + emotion_disturb_light |
| `emotion_plea` | 苦苦哀求 | 120 | 25% | any | emotion_disturb |
| `md_bomb_human` | 公告轰炸 | 130 | 30% | human | human_announce_prefix |
| `nested_maze_human` | 剧本迷宫·人 | 160 | 25% | human | human_vote_suggest |
| `char_confuse_human` | 乱码干扰 | 90 | 22% | human | human_char_garble |

### 5.3 经济模型（§133 EconTier）

| 档位 | 条件 | 系统销毁 | 彩池返还 | 目标补偿 |
|---|---|---|---|---|
| Health | ≥ 50K 金币 | 30% | 50% | 20% |
| Caution | ≥ 10K 金币 | 40% | 40% | 20% |
| Danger | < 10K 金币 | 50% | 30% | 20% |

### 5.4 狼人互知（§132）

- 触发条件：`len(agent_seats) > 1` 且 `len(providers) > 1`
- 概率：`werewolf.wolf_teammate_hint_rate`（默认 30%）
- 机制：`StartAgentsLocked` → `PickWolfTeammateHint` → `SetWolfTeammateSeat` → `Memory.ReplaceIdentity`

---

## 6. 前端组件架构

### 6.1 组件清单（`ClientWeb/src/components/werewolf/`）

| 组件 | 行数 | 职责 |
|---|---|---|
| `WerewolfTable.tsx` | 789 | 主桌面布局（13 座位 4 行堆叠 + 座位旋转） |
| `RoomCreateModal.tsx` | 486 | 创建房间弹窗（agent_seats / 法官配置 / 自选角色） |
| `NightActionPanel.tsx` | 446 | 夜间操作面板（4 形态：守卫/狼刀/预言家/女巫） |
| `DayControlPanel.tsx` | 440 | 白天控制面板（发言/投票/猎人开枪/白痴翻牌） |
| `PropPanel.tsx` | 376 | 道具面板（购买/使用/余额/经济档位） |
| `HistoryDrawer.tsx` | 352 | 历史抽屉（4 sub-tab：时间轴/独白/死亡/总结） |
| `GameStatusHeader.tsx` | 349 | 顶部状态栏（阶段/倒计时/存活统计/运行时间） |
| `FactionDrawer.tsx` | 337 | 阵营抽屉（emoji+label+reason 渲染） |
| `GameInfoPanel.tsx` | 263 | 房间信息面板 |
| `ChatQueueModal.tsx` | 247 | 聊天队列弹窗 |
| `EmotionAvatar.tsx` | 207 | 情绪头像（emotion metadata） |
| `WerewolfRestartVotePanel.tsx` | 192 | 重开局投票面板 |
| `PropUseOverlay.tsx` | 186 | 道具使用视觉叠加层 |
| `BotPhaseIndicator.tsx` | 181 | Bot 阶段指示器（"响应中"） |
| `ThinkingDots.tsx` | — | 思考动画 |
| `DayNightOverlay.tsx` | — | 天黑/天亮 CSS 特效（§124） |
| `SheriffElectedOverlay.tsx` | — | 警长当选叠加层 |
| `SheriffStreamPanel.tsx` | — | 警徽流面板 |
| `AgentCallTimeBadge.tsx` | — | Agent LLM 调用耗时徽章 |
| `IdentityGuessBadge.tsx` | — | 身份猜测徽章 |
| `IdiotRevealPanel.tsx` | — | 白痴翻牌面板 |
| `JudgePanel.tsx` | — | 法官活动流面板 |
| `LastWordsPanel.tsx` | — | 遗言面板 |
| `MyTurnIndicator.tsx` | — | 我的回合指示器 |
| `PhaseClock.tsx` | — | 阶段倒计时 |
| `RoomRunningClock.tsx` | — | 房间运行时间 |
| `GameChatPanel.tsx` | — | 适配器（87 行薄映射 `gameState.players → roomPlayers`） |

**前端代码规模**：6,296 行（TSX/TS）

### 6.2 样式文件（`ClientWeb/src/styles/`）

| 文件 | 行数 | 用途 |
|---|---|---|
| `werewolf.css` | 1056 | 基础样式 |
| `werewolf-v2.css` | 1631 | v2 增强样式 |
| `werewolf-agent.css` | 1086 | Agent 相关样式 |
| `werewolf-panels.css` | 504 | 面板样式 |
| `werewolf-modal.css` | 225 | 弹窗样式 |
| `werewolf-emotion.css` | 321 | 情绪样式（从 werewolf-v2.css 拆出，§136） |
| `werewolf-speech.css` | 172 | 发言样式 |

**CSS 总计**：4,995 行，`@import` 顺序不可调整（同优先级选择器覆盖链）

### 6.3 适配器模式

`components/werewolf/GameChatPanel.tsx` 是 87 行薄适配器，映射 `gameState.players → roomPlayers`，复用 `components/chat/GameChatPanel.tsx` 共享基座。推荐范式：游戏私有 props 映射 + 共享组件零改动。

---

## 7. WebSocket 协议

### 7.1 狼人杀相关帧

**客户端 → 服务端**：
- `game.join` / `game.leave` / `game.state` / `game.resign`
- `game.werewolf_use_prop`（`{prop_key, target}`）

**服务端 → 客户端**：
- `game.started` / `game.state` / `game.over` / `game.error`
- `game.settlement`（结算弹窗数据）

**聊天帧**：
- `chat.subscribe` / `chat.unsubscribe` / `chat.send` / `chat.history`
- `chat.message` / `chat.history` / `chat.subscribed` / `chat.unsubscribed` / `chat.error`

### 7.2 REST API

| 端点 | 方法 | 用途 |
|---|---|---|
| `/api/games/werewolf/rooms` | POST | 创建狼人杀房间（含 agent_seats + judge_config） |
| `/api/games/werewolf/rooms/:id` | GET | 获取房间详情（REST 快照） |
| `/api/rooms/:id/spectate` | POST | 进入观战 |
| `/api/rooms/:id/leave_spectate` | POST | 退出观战 |
| `/api/games/werewolf/props` | GET | 获取道具列表 |
| `/api/llm/models` | GET | 获取可用 LLM 模型列表（需登录） |
| `/api/admin/llm/providers` | GET/POST | LLM Provider 管理（admin） |
| `/api/admin/llm/providers/:id/memory` | GET/DELETE | 查看/清空 Agent 持久化记忆 |

---

## 8. 数据库模型

### 8.1 狼人杀相关表

| 表名 | 文件 | 用途 |
|---|---|---|
| `t_lsm_game_user` | `t_lsm_game_user.go` | 用户（含 `IsBot`/`BotProviderID`/`LinkedProviderAccount`） |
| `t_lsm_game_room` | `t_lsm_game_room.go` | 房间 |
| `t_lsm_game_player` | `t_lsm_game_player.go` | 玩家座位 |
| `t_lsm_game_wallet` | `t_lsm_game_wallet.go` | 钱包 |
| `t_lsm_game_wallet_tx` | `t_lsm_game_wallet_tx.go` | 钱包交易记录 |
| `t_lsm_game_llm_provider` | `t_lsm_game_llm_provider.go` | LLM Provider（含 AES-256-GCM 加密 API Key） |
| `t_lsm_game_model_game_log` | `t_lsm_game_model_game_log.go` | 模型对局日志 |
| `t_lsm_game_model_action` | `t_lsm_game_model_action.go` | 模型动作日志 |
| `t_lsm_game_model_chat_message` | `t_lsm_game_model_chat_message.go` | 模型聊天消息 |
| `t_lsm_game_agent_memory` | `t_lsm_game_agent_memory.go` | Agent 持久化记忆（MEMORY.md，§131） |
| `t_lsm_game_prop` | `t_lsm_game_prop.go` | 道具目录 |
| `t_lsm_game_prop_usage_log` | `t_lsm_game_prop_usage_log.go` | 道具使用日志 |
| `t_lsm_game_chat_message` | `t_lsm_game_chat_message.go` | 聊天消息持久化 |
| `t_lsm_game_kv` | `t_lsm_game_kv.go` | KV 存储（主密钥等） |
| `t_lsm_game_daily_reward` | `t_lsm_game_daily_reward.go` | 每日奖励 |
| `t_lsm_game_admin_grant` | `t_lsm_game_admin_grant.go` | 管理员发放 |

### 8.2 8 个默认 LLM 模型

| AgentName | Model | ProviderType |
|---|---|---|
| 美团 LongCat-2.0 | `MeiTuan-model` | anthropic |
| 豆包 2.0 | `DouBao-model` | anthropic |
| DeepSeek V4-Pro | `DeepSeek-model` | anthropic |
| 智谱 GLM-5.2 | `GLM-model` | anthropic |
| Kimi 2.7 | `Kimi-model` | anthropic |
| MiniMax M3 | `MinMax-model` | anthropic |
| Qwen 3.7-Plus-and-Max | `Qwen-model` | anthropic |
| Xiaomi mimo-v2.5-pro | `Xiaomi-model` | anthropic |

---

## 9. 测试覆盖

### 9.1 单元测试文件清单

**引擎层**（`ServerGo/game/werewolf/`）：56 个测试文件，覆盖：
- `engine_test.go`（1429 行）：核心引擎逻辑
- `engine_guard_test.go`（855 行）：守卫角色全链路
- `engine_reveal_fairness_test.go`：身份公开公平性（§135，R-01~R-14）
- `engine_death_semantic_test.go`：死亡语义（§123）
- `engine_knight_test.go`：骑士角色（§198）
- `engine_demon_hunter_test.go`（573 行）：猎魔人角色
- `engine_sheriff_test.go`：警长竞选
- `engine_hunter_acting_seat_test.go`：猎人 acting seat
- `engine_13p_deadline_test.go`：13 人局 deadline
- `room_r212_deadlock_test.go`：§92a 自死锁回归（R212-A01~A05/B01~B02）
- `room_r243_test.go`：零投票狼 stall（R243）
- `room_r231_test.go` / `room_r232_test.go` / `room_r228_test.go`：各类回归
- `prop_aoe_test.go`：AOE 道具（§20260807-04，8 项测试）
- `prop_test.go` / `prop_v3_test.go` / `prop_service_test.go`：道具系统
- `econ_tier_test.go`：经济档位
- `wolfpack_room_test.go`：狼队交流
- `speak_filter_test.go`（999 行）：发言过滤
- `speak_mystery_test.go`：猜疑化
- `room_restart_vote_test.go`：重开局投票
- `room_cooling_final_broadcast_test.go`：冷却期
- `room_full_ai_test.go`：全 AI 房间
- `room_human_input_test.go` / `room_human_wait_test.go`：人类玩家
- `room_judge_test.go`：法官
- `agent_memory_bridge_test.go`：持久化记忆
- 多个 `room_round*_test.go`：各轮次回归

**Agent 层**（`ServerGo/agent/`）：32 个测试文件，覆盖：
- `wwplayer/agent_test.go`（1192 行）：Agent 核心逻辑
- `wwplayer/run_stream_extend_test.go`：流式续命（§197，4 项不变式）
- `wwplayer/round26_test.go`（476 行）：第 26 轮回归
- `wwplayer/quarantine_round24_test.go`：quarantine 回归
- `wwplayer/memory_iterate_test.go`：记忆迭代
- `wwplayer/emotion_switch_speak_tools_test.go`（420 行）：情绪工具
- `wwplayer/guard_tools_test.go`：守卫工具
- `wwplayer/prop_v2_test.go` / `prop_v4_test.go` / `prop_v5_test.go`：道具各版本
- `wwplayer/speak_factcheck_test.go` / `whisper_factcheck_test.go`：事实检查
- `wwjudge/judge_test.go` / `judge_summary_test.go`：法官
- `core/chat_history_test.go` / `speak_dedup_test.go`：核心基础设施

**总计**：88 个测试文件

---

## 10. 关键 Bug 与教训速览

### 10.1 高频复现教训

| 教训 | 含义 | 复现次数 |
|------|------|---------|
| §92a | `sync.Mutex` 不可重入，被持锁函数调用的方法必须是 `*Locked` 变体 | ≥5 次（R212 最严重） |
| §130 | 「声明了却从不接线」— 字段/枚举/分支齐全但无生产写入点 | ≥3 次（法官/守卫/遗言） |
| §134 | 进卡池的角色要么完整实现，要么移出卡池 | 1 次（守卫→骑士→猎魔人） |
| §135 | 脱敏必须堵住所有下发通道，不能只修主路径 | 1 次（4 处独立复制） |
| §197 | 分阶段预算优于硬上限（首字节前 callTimeout + 首字节后 extendedTimeout） | 1 次（慢模型误杀） |

### 10.2 最近关键修复（2026-08-01 ~ 08-07）

| Bug ID | 日期 | 严重性 | 描述 | 修复 Commit |
|---|---|---|---|---|
| BUG-HUNTER2-P0-01 | 08-07 | P0 | 警长竞选 watchdog 错误跳过 | `fa88180` |
| BUG-WITCH-P2-01 | 08-07 | P2 | 公聊泄露成对 tool_call 块 | `7f1b15e` |
| §20260807-04 | 08-07 | P0-P2 | Agent 道具对齐 6 类 LLM 注入攻击（7 项差距） | `f3e916d` |
| BUG-R245-P0-01 | 08-06 | P0 | Agent 身份直陈 | `2a48b9e` |
| BUG-R244-P1-01 | 08-06 | P1 | 人类公聊/私聊消息不显示 | `78a6655` |
| BUG-R243-P1-01 | 08-06 | P1 | night_wolves 零投票 120s 早期 force-tally | `64dcb7b` |
| BUG-R242-P1-01 | 08-05 | P1 | 恢复房间级 LLM 并发信号量 | `46d277b` |
| BUG-R241-P1-01 | 08-05 | P1 | 字节预算剪枝 + 道具浮层真人 UUID | `ba1b84e` |
| BUG-R212 | 07-31 | P0 | §92a 自死锁致弹窗卡死 + 永久同步 | `8c64432` |

---

## 11. 设计文档索引

### 11.1 核心设计文档

| 文档 | 路径 | 主题 |
|---|---|---|
| 狼人杀 Agent 设计 | `docs/狼人杀-Agent与系统/狼人杀Agent设计.md` | Agent 架构、生命周期、工具、提示词 |
| 狼人杀 13 人标准局规则 | `docs/狼人杀13人标准局规则.md` | 完整游戏规则 |
| 狼人杀 Agent 法官设计 | `docs/狼人杀-重构方案/主持人Agent重构设计.md` | 法官系统（§123/§130） |
| 主持人 Agent 重构设计 | `docs/狼人杀-重构方案/主持人Agent重构设计.md` | 法官重构（§130） |
| 狼人杀持久化记忆设计 | `docs/狼人杀-Agent与系统/狼人杀Agent持久化记忆设计.md` | MEMORY.md 迭代学习（§131） |
| 狼人杀守卫角色设计 | `docs/狼人杀-角色设计/狼人杀守卫角色设计.md` | 守卫全链路（§134） |
| 狼人杀骑士角色设计 | `docs/狼人杀骑士角色设计.md` | 骑士全链路（§198） |
| 狼人杀猎魔人角色设计 | `docs/狼人杀猎魔人角色设计.md` | 猎魔人全链路 |
| 狼人杀死亡语义设计 | `docs/狼人杀-角色设计/狼人杀死亡语义设计.md` | execution vs death（§123） |
| 狼人杀重开局投票设计 | `docs/狼人杀-Agent与系统/狼人杀重开局投票设计.md` | 重开局投票（§117） |
| 狼人杀对话即思考设计 | `docs/狼人杀-Agent与系统/狼人杀对话即思考设计.md` | R128 重构 |
| 狼人杀 Agent 情绪模块设计 | `docs/狼人杀-Agent与系统/狼人杀Agent情绪模块设计.md` | 情绪系统 |
| 狼人杀 Agent 公屏猜疑化设计 | `docs/狼人杀-Agent与系统/狼人杀Agent公屏猜疑化设计.md` | 猜疑化发言 |
| 狼人杀 13 人局道具系统设计 | `docs/狼人杀-道具与经济/狼人杀13人局道具系统设计.md` | 道具系统 v1-v5 |
| 狼人杀 13 人局 Agent 道具 | `docs/狼人杀-道具与经济/狼人杀13人局-Agent道具-20260807-04.md` | §20260807-04 修复 |
| Agent 工具定义解决方案 | `docs/LLM与Agent/Agent工具定义-解决和设计方案-20260804-01.md` | emotion_switch_speak |
| LLM 供应商设计 | `docs/LLM与Agent/LLM供应商设计.md` | LLM Provider 模块 |
| 狼人杀观众交互设计 | `docs/狼人杀观众交互设计.md` | 观战者交互 |
| 狼人杀观众唤醒设计 | `docs/狼人杀观众唤醒设计.md` | 观众唤醒 |
| 狼人杀房间聊天设计 | `docs/狼人杀房间聊天设计.md` | 聊天系统 |
| 狼人杀遗言设计 | `docs/狼人杀遗言设计.md` | 遗言系统 |
| 模型管理与持久化玩家设计 | `docs/狼人杀-道具与经济/模型管理与持久化玩家设计.md` | §118 模型管理 |
| 模型玩家金币设计 | `docs/狼人杀-道具与经济/模型玩家金币设计.md` | 金币系统 |

### 11.2 相关注入攻击文档（仓库根目录）

- `docs/注入攻击演示/01-Markdown格式注入攻击.md`
- `docs/注入攻击演示/02-提示词套娃多层嵌套注入.md`
- `docs/注入攻击演示/03-字符级欺骗混淆式注入.md`
- `docs/注入攻击演示/04-大模型长上下文注意力失焦.md`
- `docs/注入攻击演示/05-任务马甲式注入.md`
- `docs/注入攻击演示/06-情绪操控式注入.md`

---

## 12. 金币系统

### 12.1 结算规则

- 狼人杀 13 人局结算：胜方 +100 金币，败方 -100 金币
- 道具购买扣款：50% 返彩池 / 30% 系统销毁 / 20% 中招补偿
- 每日登录奖励：`t_lsm_game_daily_reward`
- 管理员发放：`t_lsm_game_admin_grant`

### 12.2 经济档位（§133）

按房间总金币存量动态切档，反通胀 + 防无限刷道具。

---

## 13. Agent 持久化记忆（§131）

### 13.1 机制

- 表：`t_lsm_game_agent_memory`（一模型一行，`model_key` UNIQUE + `version` 乐观锁 + `memory_md` mediumtext ≤100KB）
- 触发：每局结束法官总结落地后，`IterateAgentMemoriesAsync` 对每个 bot 模型异步发起自我迭代
- 注入：`StartAgentsLocked` 时从 DB 读 `memory_md` → `InjectBlock` 注入 user prompt 末尾（截断到 4000 字 ≈ 2K token）

### 13.2 4 段固定标题

1. 战绩与趋势
2. 我的失误与教训
3. 其他模型特点分析
4. 决策策略迭代

### 13.3 角色差异化学习

迭代 prompt 注入"本局角色 X"，LLM 按角色分类记录经验。

---

## 14. 冷却期与重开局（§129/§117）

### 14.1 冷却期

- 默认 30 分钟（`werewolf.cooling_sec`）
- 有人类在线 → 延长窗口；无人类 → 倒计时
- 超时 → 进入重开局投票或关门

### 14.2 重开局投票

- 5 分钟窗口，≥ 2/3 同意即原地复用 7 座位
- 保留 chatQueue/ReadPointers 重开一局
- `restartGameLocked` 调 `NewGame+StartGame` 但保留 `r.Seats`

---

## 15. 代码规模统计

| 模块 | 文件数 | 代码行数 |
|---|---|---|
| `ServerGo/game/werewolf/`（含测试） | 56 | 36,505 |
| `ServerGo/agent/`（含测试） | 65 | ~22,000 |
| `ClientWeb/src/components/werewolf/` | 29 | 6,296 |
| `ClientWeb/src/styles/werewolf*.css` | 7 | 4,995 |
| **总计** | **157** | **~69,800** |

---

## 16. 配置项（`LsmAgentGame.conf` werewolf 段）

| 配置项 | 默认值 | 用途 |
|---|---|---|
| `werewolf.cooling_sec` | 1800 | 冷却期秒数 |
| `werewolf.judge_mode` | "agent" | 法官模式（agent/human/off） |
| `werewolf.agent_memory_enabled` | true | Agent 持久化记忆开关 |
| `werewolf.agent_memory_max_tokens` | 2048 | 记忆注入最大 token |
| `werewolf.wolf_teammate_hint_rate` | 30 | 狼人互知概率（%） |
| `werewolf.llm_call_timeout_sec` | 300 | 单次 LLM 调用超时 |
| `werewolf.llm_stream_extended_timeout_sec` | 900 | 流式续命超时 |
| `werewolf.lenient_mode_for_seat_count` | 13 | 宽容模式阈值 |
| `werewolf.llm_timeout_scale_percent` | 150 | 超时缩放百分比 |
| `werewolf.first_night_grace_sec` | — | 首夜缓冲期秒数 |
| `werewolf.first_night_forced_speak_rounds` | 1 | 首夜强制发言轮数 |
| `werewolf.spectator_full_wake` | true | 观众全频唤醒 |
| `werewolf.room_llm_concurrency` | 8 | 房间级 LLM 并发信号量 |

---

> **文档维护说明**：本文档应在以下事件发生时更新：
> 1. 新角色从「已退役」复活到 godRolePool
> 2. 新道具系统版本上线
> 3. 新 AgentClassName 注册
> 4. 重大 Bug 修复后教训更新
> 5. 新阶段/新工具接入
