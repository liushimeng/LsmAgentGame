# 狼人杀 13 人局 Agent 升级 — 20260810-05(信息账本 Information Ledger 一期)

> **文档版本**:2026-08-10
> **采纳来源**:[`LongCat-Surpport-01.md`](../../../LongCat-Surpport-01.md) §2 F1「信息账本(Information Ledger)——本文最推荐的新功能」
> **采纳理由(为什么选它)**:
> 1. **它是 LongCat 全文的核心论点与最推荐项**,也是 7 份知识库文件中「复杂度 × 地基性」综合最高的一条——其余候选要么已被 §20260809-02 / §20260810-01~04 采纳(票型回灌、wolf_whisper、警徽流查验历史、法官多轮记忆、模型天梯…),要么是纯外围观赏功能(独白剧场、高光卡片、押注)。
> 2. **它是 D1 类「信息流失控」缺陷的结构性根治**:LongCat 审计出 6 项「注释/签名承诺了 X、代码没做 X」的信息流缺陷(D1~D6),根因是「谁知道什么」散落在十几个字段/通道,没有单一视图。账本是唯一能**在结构上**防止第 7 个 D1 诞生的方案——漏登记 = 该信息对谁都不存在,会在测试中立刻暴露,而不是静默偏袒某一方。
> 3. **本期只做一期(纯后端)**:账本存储 + 写入接入 + 断言 + 观战者脱敏快照下发。**不让** `buildAgentContextLocked` 改读账本(避免一次 PR 同时改 prompt 组装逻辑的风险),前端可视化(信息传播时序图)留二期。

---

## 1. 现状审计依据

「谁知道什么」当前散落在至少 12 个互不统辖的地方:

| 信息种类 | 当前载体 | 消费方 |
|---|---|---|
| 公开发言 | `r.recentSpeeches` + `r.chatQueue` | 全房 |
| 私聊 whisper | `r.whisperInbox[seat]` + chatQueue 双写 | 收发双方 |
| 狼队密语 | `r.wolfPack`(WolfPackRoom FIFO) | 存活狼 |
| 狼刀投票/理由 | `gs.WolfVotes` / `gs.WolfVoteReasons` → `gc.WolfVotes` | 存活狼 |
| 预言家查验 | `gs.Players[seat].LastSeerCheck` / `SeerCheckHistory` | 预言家本人 |
| 女巫见刀/用药 | `gs.WolfKillTarget` → `gc.WolfTarget` | 女巫本人 |
| 守卫守护 | `gs.GuardProtectTarget` / `GuardLastProtect` | 守卫本人 |
| 白天票型 | `gs.LastDayVoteMap` → `gc.LastDayVoteMap` | 全房(§20260809-02 U2 回灌) |
| 警徽流 | `gs.SheriffStreams` + `streamFaction` | 全房(公开结算) |
| 警长当选 | `gs.SheriffSeat` | 全房 |
| 道具注入 | `r.propInjectQueue[seat]` → `gc.PropInjectText` | 被击中者 |
| 死亡事件 | activity 流 + `all_dead_list` | 全房(身份按 §135 脱敏) |

没有任何单一数据结构能回答「**第 3 天开始时,5 号位掌握哪些信息?**」。

---

## 2. 设计

### 2.1 数据结构(新文件 `ServerGo/game/werewolf/information_ledger.go`,≤1800 行硬约束下 ≤300 行)

```go
// InfoSource 信息来源类型(封闭枚举,禁止自由字符串)。
type InfoSource string

const (
    InfoSourcePublicSpeech  InfoSource = "public_speech"   // 公开发言/插话
    InfoSourceWhisper       InfoSource = "whisper"         // 私聊
    InfoSourceWolfPack      InfoSource = "wolf_pack"       // 狼队密语
    InfoSourceNightSeer     InfoSource = "night_seer"      // 预言家查验结果
    InfoSourceNightWitch    InfoSource = "night_witch"     // 女巫见刀/用药
    InfoSourceNightGuard    InfoSource = "night_guard"     // 守卫守护
    InfoSourceNightWolfVote InfoSource = "night_wolf_vote" // 狼刀投票(含理由)
    InfoSourceDayVoteMap    InfoSource = "day_vote_map"    // 白天票型(谁投了谁)
    InfoSourceSheriffStream InfoSource = "sheriff_stream"  // 警徽流结算
    InfoSourceSheriffElect  InfoSource = "sheriff_elect"   // 警长当选
    InfoSourcePropInject    InfoSource = "prop_inject"     // 道具注入文本
    InfoSourceDeathEvent    InfoSource = "death_event"     // 死亡事件(不含身份)
    InfoSourceHunterShot    InfoSource = "hunter_shot"     // 猎人开枪(公开)
    InfoSourceKnightDuel    InfoSource = "knight_duel"     // 骑士决斗(公开)
    InfoSourceIdiotReveal   InfoSource = "idiot_reveal"    // 白痴翻牌(公开)
    InfoSourceDemonHunter   InfoSource = "demon_hunter"    // 猎魔人狩猎(公开)
    InfoSourceRoleDeal      InfoSource = "role_deal"       // 开局发牌(仅本人)
)

// InfoEntry 一条信息账本记录:某事实 + 知情座位集合 + 获得时刻 + 来源。
// KnowerSeats 采用 map[int]bool(而非 []int)以 O(1) 支持 Knows(seat) 判定。
type InfoEntry struct {
    Seq         int64         `json:"seq"`          // 房间内单调递增序号(1 起)
    Round       int           `json:"round"`        // DayNumber
    Phase       string        `json:"phase"`        // Phase.String()
    Source      InfoSource    `json:"source"`
    Fact        string        `json:"fact"`         // ≤120 rune 截断,脱敏后文本
    KnowerSeats map[int]bool  `json:"knower_seats"` // 0-indexed 座位集合
    TS          int64         `json:"ts"`           // UnixMilli
}

// InformationLedger 房间级信息账本。所有方法均为 *Locked 语义:
// 调用方必须已持有 r.mu(§92a),本结构自身不再加锁。
type InformationLedger struct {
    seq     int64
    entries []InfoEntry
}
```

**硬约束**:
- 环形容量 `informationLedgerCap = 400`(13 人局单局信息条数实测上限 ~250;超出时淘汰最旧)。
- `Fact` 入帐前必须经 `redactLedgerFact()`:**剔除 role/faction 明文**(防止写入侧把「3 号是狼人」这类身份结论直接落账;§119/§135)。
- 账本**不进** DB、**不进** `chat_message` / `chat_history` / `BotTranscript`(纯内存,随房间 GC 回收;§111 教训——per-seat fan-out 内存爆炸,这里用单源 + 知情集合)。
- `restartGameLocked` 原地重开时**清零**(新一局信息重新累计;与 `propInjectQueue`/`recentSpeeches` 语义对齐——`recentSpeeches` 在 restart 时保留,账本则按"局"隔离,见 §4 决策记录 D-02)。

### 2.2 生命周期与懒初始化(§130「声明了却从不接线」防线)

房间有 6 处 `&WerewolfRoom{...}` 字面量构造点(`room_action.go:20` / `room_agent.go:1348,1574,1615,1647` / `room_manage.go:162`)。为避免 6 处同步遗漏,采用与 `wolfPack` 一致的**懒初始化**模式:

```go
// ledgerLocked 返回房间信息账本,懒初始化。caller 必须持 r.mu(§92a)。
func (r *WerewolfRoom) ledgerLocked() *InformationLedger {
    if r.infoLedger == nil {
        r.infoLedger = NewInformationLedger()
    }
    return r.infoLedger
}
```

`WerewolfRoom` 新增字段 `infoLedger *InformationLedger`(room.go,紧邻 `wolfPack` 字段并加注释放语义注释)。

### 2.3 写入接入点(全部一行调用,均已在持锁路径)

| # | 接入点(文件:函数) | Source | KnowerSeats |
|---|---|---|---|
| 1 | `restartGameLocked`(room_restart_vote.go,`StartGame()` 成功后) | `role_deal` ×每座位一条 | `{i}`(仅本人)|
| 2 | `appendRoomMessage` 公开分支(room_chat.go,`appendToChatQueueLocked` 之后) | `public_speech` | 全部存活座位 |
| 3 | `appendRoomMessage` whisper 分支(同上,whisper return 之前) | `whisper` | `{fromSeat, toSeat}` |
| 4 | `Action_DayVote` 成功路径(room_action.go) | `day_vote_map` | 全部存活座位 |
| 5 | `Action_SheriffElect` 成功且 `SheriffSeat != NoSeat`(room_action.go) | `sheriff_elect` | 全部存活座位 |
| 6 | `maybeSettleSheriffStreamLocked` 结算点(room_action.go) | `sheriff_stream` | 全部存活座位 |
| 7 | `Action_SeerCheck` 成功路径 | `night_seer` | `{actor}` |
| 8 | `Action_Witch` 成功路径 | `night_witch` | `{actor}` |
| 9 | `Action_GuardProtect` 成功路径 | `night_guard` | `{actor}` |
| 10 | `Action_WolfKill` 成功路径 | `night_wolf_vote` | 全部存活狼座位 |
| 11 | `Action_HunterShoot` 成功路径 | `hunter_shot` | 全部存活座位 |
| 12 | `Action_KnightDuel` 成功路径 | `knight_duel` | 全部存活座位 |
| 13 | `Action_IdiotReveal` choice=="reveal" 成功路径 | `idiot_reveal` | 全部存活座位 |
| 14 | `Action_DemonHunterHunt` 成功路径 | `demon_hunter` | 全部存活座位 |
| 15 | `enqueuePropHitLocked`(room_prop.go,manager/agent 双路径已汇于此) | `prop_inject` | `{target}` |
| 16 | `RecordRoomActivity` 中 `EventKind=="player_died"`(room_chat.go) | `death_event` | 全部存活座位 |
| 17 | `agentRunner.WolfWhisper` 成功后(agent_runner.go,`wolfPack.Append` 之后) | `wolf_pack` | 全部存活狼座位 |

> bot 公开/私聊发言(`SendFromBot`/`WhisperFromBot`)**不经** 2/3 重复登记——它们最终都汇入 `emitRoomMessage → RecordRoomMessage → appendRoomMessage`(ws/chat_service.go:860 + whisper 同构路径),单一接入点已覆盖人机两侧。**注册表驱动防漏**:每个 `InfoSource` 常量在测试中被断言「至少存在一个写入点」(grep 级 CI 护栏的单测版)。

### 2.4 断言(结构性防 D1 复现)

账本提供开发期断言,**仅在 debug 构建/tag 下启用**,生产零开销:

```go
// AssertKnows 断言 seat 对「最近一条匹配 source 的账本条目」知情。
// 仅 go test / -tags werewolf_debug 下编译进断言逻辑;生产构建为空操作。
func (l *InformationLedger) AssertKnows(seat int, source InfoSource) bool
```

配套单测模拟完整对局流,断言:狼人知道狼刀目标、女巫知道狼刀目标、守卫**不**知道狼刀目标(§134 盲守)、平民不知道查验结果、死亡后不再获得新信息。任何未来改动导致「某座位看到了他不该看到的信息」或「该看到的信息没登记」都会在测试中立刻失败。

### 2.5 观战者脱敏快照下发(一期唯一对外输出)

一期不做前端组件,但把账本快照**下发到 `game.state` 的观战者视图**,为二期可视化提供数据通道,并当场验证脱敏正确性:

- `view.go::SpectatorView` 在 `Status=="playing"/"over"` 时追加 `info_ledger []InfoEntryJSON` 字段(`omitempty`,玩家视图与 REST 房间视图**不下发**)。
- 脱敏规则(§135 镜像):
  - `night_seer`/`night_witch`/`night_guard`/`prop_inject`/`role_deal`/`wolf_pack`/`whisper` 类条目对观战者**可见**(观战者本就享有上帝视角数据:HeartThought / WolfPack 判例已在 HistoryDrawer 存在),但 `Fact` 文本已在写入侧 redact 身份明文。
  - 终局前(**与 §135 一致**)观战者可见账本,但任何条目均不含「某座位真实身份」——账本只记**信息流动**,不记**身份结论**。
- `InfoEntryJSON`:`knower_seats` 序列化为排序后的 `[]int`(JSON 稳定输出,避免 map 序随机)。

### 2.6 测试(`game/werewolf/information_ledger_test.go`)

| 用例 | 断言 |
|---|---|
| L-01 懒初始化 | 新房间 `infoLedger==nil`;首次写入后非 nil;`Knows` 行为正确 |
| L-02 公开/私聊 | 公开发言 → 全部存活知情;whisper → 仅双方知情;第三座 `Knows==false` |
| L-03 夜间技能隔离 | seer 查验仅本人知情;witch 见刀仅本人;guard 守护仅本人;狼刀投票全狼知情且平民不知情 |
| L-04 盲守不变式(§134) | `night_wolf` 结算后,守卫座位对狼刀目标 `Knows==false` |
| L-05 道具注入 | `enqueuePropHitLocked` 后仅 target 知情;`ExpiresAfter` 递减不影响账本(账本记"曾经知情") |
| L-06 容量淘汰 | 写入 cap+50 条后 len==cap 且最旧 seq 被淘汰 |
| L-07 redact | 写入含「狼人/预言家/女巫」等身份明文的 fact → 输出中不含身份词 |
| L-08 重开清零 | `restartGameLocked` 后账本为空且 seq 重新从 1 起 |
| L-09 写入点注册表 | 遍历全部 17 个 InfoSource 常量,断言每个都有 ≥1 个生产调用点(用 `runtime.Caller` 或 AST 级不可行 → 改用「每个 source 在测试模拟流中至少被产生一次」覆盖断言) |
| L-10 观战者快照脱敏 | `SpectatorView` 输出中不含身份明文,`knower_seats` 为有序数组 |

---

## 3. 与既有教训的对齐自查

- **§92a**:账本全部方法 `*Locked` 语义,自身无锁;`WerewolfRoom` 唯一新增方法 `ledgerLocked()` 标注 caller-holds-r.mu。
- **§97**:不新增 phase、不新增 skip 动作 → 五处同步**不适用**(本期纯信息记录,不动阶段机)。
- **§119**:账本**不进** chat_message / chat_history / HeartThought;whisper/wolfpack 条目只记「发生过一条私聊/密语 + 来源座位」,**不记内容原文**进任何公屏通道(Fact 文本仅观战者快照可见)。
- **§130**:接入点已在 §2.3 逐一 grep 核实存在(见每条「文件:函数」);L-09 测试兜底「注册了但无人写入」。
- **§135**:账本不存身份结论;观战者快照复用既有 spectator 通道,不新增玩家侧下发。
- **§111**:单源 + 知情集合,杜绝 per-seat fan-out。
- **§134**:`role_deal` 条目 KnowerSeats 仅本人,保证「守卫看不到狼刀」这类既有隔离不被账本意外打通。
- **§128**:不新增 BotTranscript 兼容字段;账本独立于对话即思考体系。

---

## 4. 决策记录

- **D-01 为什么不一期就让 `buildAgentContextLocked` 改读账本**:prompt 组装是高危路径(直接影响所有 bot 行为),账本接入应先「只写不读」稳定一周,二期再切读取侧。LongCat 原文也明确建议「分两期:一期只做后端账本,二期做消费与可视化」。
- **D-02 账本在 restart 时清零而 recentSpeeches 保留**:账本按「局」隔离(信息知情集合跨局无意义,角色已重发);recentSpeeches 保留是 §117 连续游戏语义的既定行为,不冲突。
- **D-03 Fact 文本脱敏在写入侧而非读取侧**:与 §135「单点判定」同哲学——脱敏越靠近数据源越不可能被绕过。

---

## 5. 二期展望(不在本期范围)

1. `buildAgentContextLocked` 改从账本查询「本座位应知信息」组装 prompt(结构性消除 D1 类缺陷)。
2. 前端「信息传播时序图」:横轴时间、纵轴座位、连线表示信息流动(观战者专属,消费 §2.5 已下发的 `info_ledger`)。
3. 「说漏嘴检测」:发言内容与账本交叉,产出终局复盘硬证据(如「5 号白天说的这条,账本里只在狼队频道出现过」)。
