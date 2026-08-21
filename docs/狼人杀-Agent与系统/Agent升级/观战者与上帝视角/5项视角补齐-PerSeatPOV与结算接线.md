# 狼人杀 13 人局 Agent 升级 §20260811-08

> **批次**：§20260811-08
> **来源**：[`Agent-Surpport-01.md`](../../Agent-Surpport-01.md)（合并版第三方 LLM 建议）§8.6 / §10.3 / §8.7 / §11.5 T1 / §8.13
> **本批次落地 5 项**：
> - **U1** `PerSeatPOV` 占位字段真实填充（§8.6 全视角读心观战 — DouBao §四.2 的**未完成部分**）
> - **U2** 结算奖励 `grantSettlementRewardsLocked` 接线补齐 + 死亡玩家同样发奖（§10.3 — M3 §4 C2 的**未完成部分**）
> - **U3** `GodModeSnapshot` 补猎人开枪 / 骑士决斗 / 猎魔人狩猎 / 白痴翻牌四类事件（§8.7 夜间血迹图数据地基 — M3 §3 S2）
> - **U4** `DayNightOverlay` 补 `night_guard` / `night_demon_hunter` / `sheriff_order` 三个未覆盖阶段（§11.5 T1 阶段切换动画 — Mimo §8.1）
> - **U5** 模型风格标识符 `ModelStyle`（§8.13 — M3 §5 P2）
>
> **定位**：本批次是「§130 已声明却从不接线」模式的**第 N 次复现集中清算**。U1/U2/U3
> 三项全部是「结构体字段/函数已存在，生产写入点缺失或不完整」，与 §130（法官 Provider
> 从不注入）/ §132（`WolfTeammateHint` 从不调用）/ §134（守卫在卡池却无引擎）/ §135
> （`HunterPendingFrom=="wolf"` 死代码）同源。U4/U5 是低风险纯前端增量。
>
> **审计依据**：CLAUDE.md §13 教训清单 —— §87（处理报告前必须 git log/grep 验证缺陷
> 仍存在）+ §92a（锁内变体）+ §118（异步持久化不阻塞游戏流）+ §119（协议层隔离）+
> §121（数据形状契约）+ §128（对话即思考）+ §130（接线验证）+ §135（身份公开单点判定）+
> §26（前端对比度规范）。
>
> **§87 前置验证**：本文档 5 项缺陷**全部经过 grep 复核确认当前 main HEAD 仍然存在**，
> 验证命令与输出见各项 §x.1「缺陷证据」小节，不是照抄建议文档的「按惯例」判断。

---

## 总览

| # | 主题 | 类型 | 影响面 | 主要风险 |
|---|---|---|---|---|
| U1 | `PerSeatPOV` 五字段真实填充 | §130 接线修复 | ~140 行（后端为主）| §119 协议层隔离 / §135 单点判定 |
| U2 | 结算奖励接线补齐 + 死者发奖 | §130 接线修复 | ~90 行（纯后端）| §92a 锁内变体 / 幂等重复发放 |
| U3 | GodMode 补 4 类公开事件 | §130 数据补全 | ~120 行（后端 + TS 类型）| §121 数据形状契约 |
| U4 | DayNightOverlay 补 3 阶段 | 前端增量 | ~60 行（前端 + CSS）| §124 `pointer-events:none` |
| U5 | 模型风格标识符 | 前端新功能 | ~180 行（前端 + i18n）| §26 对比度阈值 |

**总计** ≈ 590 行，无新增 DB 表、无新增 WS 帧、无新增 phase、无新增 Agent 工具。

---

## U1 — `PerSeatPOV` 占位字段真实填充（§8.6 / DouBao §四.2 未完成部分）

### 1.1 缺陷证据（§87 验证）

`§20260810-11 V1`「全视角读心观战」声称已落地，`GodModeSnapshot.PerSeatPOV` 结构体
定义了 10 个字段（`view.go:1405-1417`），但 `populateGodModeLocked` 的填充代码
（`view_godmode.go:114-123`）只赋了 3 个：

```go
pov := PerSeatPOV{
    Role:             gs.Roles[i].String(),
    RoleRevealed:     gs.Status == "over" || (... HunterFired || ... IdiotRevealed),
    Faction:          FactionOf(gs.Roles[i]).String(),
    HeartThought:     "",          // ← 硬编码空
    LastDecision:     "",          // ← 硬编码空
    NightActions:     []string{},  // ← 硬编码空
    PublicCommitments: []string{}, // ← 硬编码空
}
// ToolCallCount / LLMCallCount / LastEmotion / ChallengeCount 从未被赋值(零值)
```

源码注释自述「**当前仅做占位**，实际数据由前端通过单独的 spectator-only endpoint
拉取（避免 view.go 通道膨胀）」—— 但该 endpoint **从未存在**（`grep -rn
"per_seat_pov\|PerSeatPOV" ServerGo/api/ ServerGo/ws/` 零命中）。结果：前端
`GodModeView.tsx` 的视角切换面板永远只显示角色 + 阵营，7 个信息位全空。

**这与 §130 法官 `Provider` 字段同构**：声明了字段、写了消费方、注释解释了设计，
唯独没有生产写入点，且注释把缺陷伪装成「后续 V2」的既定设计。

### 1.2 关键修正：`RoleRevealed` 绕开了 §135 单点判定

`view_godmode.go:116` 手写了身份公开条件：

```go
RoleRevealed: gs.Status == "over" || (i < len(gs.Players) && (gs.Players[i].HunterFired || gs.Players[i].IdiotRevealed)),
```

而 §135 规定身份公开判定**只能**走 `RolePubliclyRevealed(seat)` 单一事实来源
（4 类白名单：终局 / 白痴翻牌 / 狼自爆 / 猎人实际开枪）。手写版**漏了狼自爆**
（`DeathCause == DeathCauseSuicide`）—— 这正是 §135 教训 (1)「修复的关键不是改
4 处 if，而是建立单点判定让第 5 处无法诞生」中所说的**第 5 处**，它在 §135 落地
**之后**又诞生了。本批次收口为 `gs.RolePubliclyRevealed(Seat(i))`。

### 1.3 设计 — 五个数据源全部已存在，只需接线

| 字段 | 数据源 | 约束 |
|---|---|---|
| `HeartThought` | `r.BotAgents[seat].BotTranscript().HeartThought` | §119 仅 spectator（本函数已在 `viewer<0` 分支）；截断 200 rune |
| `LastDecision` | 同上 `.LastDecisionSummary` | 截断 200 rune |
| `NightActions` | `r.infoLedger` 中该 seat 为**行动者**的夜间条目 | 复用 §20260810-05 账本，不新增镜像字段 |
| `ToolCallCount` / `LLMCallCount` | `.ToolCalls` 长度 / `.TotalLLMCalls` | 纯计数 |
| `LastEmotion` | `.LastEmotionZh` | — |
| `ChallengeCount` | `gs.Players[i].LastChallengedBy >= 0` 计 1 | 引擎仅保留「最近一次」，无累计计数器 |
| `PublicCommitments` | `r.commitmentLedger.GetAllLocked()` 过滤 `Seat==i` | 渲染为 `模板名(状态)` 字符串 |

**§92a**：`populateGodModeLocked` 的唯一调用点 `BuildClientStateWithRoom` 已持
`r.mu`，本函数继续保持锁内直读语义，**不新增任何 `Lock()`**（R212 教训：新增
只读 getter 引入自死锁）。`CommitmentLedger` 的方法全部是 `*Locked` 语义
（`commitment_ledger.go:59` 明示「调用方必须已持有 r.mu，本结构自身不再加锁」），
可直接调用。

**§119 协议层隔离不被破坏**：`HeartThought` 进入 `PerSeatPOV` 后仍然只走
`viewer<0` 的 spectator 分支下发 —— 与既有 `sanitizeBotTranscript`（玩家分支
清空 `HeartThought`）语义一致，不新增第二条泄漏通道。

### 1.4 `ChallengeCount` 的诚实取值

引擎只有 `Player.LastChallengedBy` / `LastChallengeQuestion` 两个「最近一次」
字段（`engine.go:162-163`），且在 `engine_day.go:337` 每轮被重置为 -1。**没有
本局累计质疑计数器**。

两种选择：(a) 新增 `Player.ChallengeRecvCount` 累加字段；(b) 按当前可得语义填 0/1。
本批次选 **(b)**，并把 JSON 字段语义注释从「本局被质疑次数」修正为「当前是否处于
被质疑态（0/1）」—— 理由：新增引擎字段需同步 `startNight` / `advanceDay` 的重置
语义（§134 教训 (7)「`GuardLastProtect` 绝不能在 `startNight()` 重置」类风险），
性价比不足；**注释与实现对齐**本身就是本批次要清算的病根，不能一边修 §130 一边
制造新的「注释承诺 X 代码做 Y」。

---

## U2 — 结算奖励接线补齐 + 死亡玩家同样发奖（§10.3 / M3 §4 C2 未完成部分）

### 2.1 缺陷证据（§87 验证）

```
$ grep -rn "grantSettlementRewardsLocked" ServerGo/ --include=*.go | grep -v _test
settlement_reward.go:10://   - §130 接线验证:grantSettlementRewardsLocked 必须在终局路径**实际调用**
settlement_reward.go:219:func (m *WerewolfManager) grantSettlementRewardsLocked(...)
room_restart_vote.go:352:	m.grantSettlementRewardsLocked(r, m.rewardSvc)     ← 唯一生产调用点
```

而 `EmitGameOver` 的生产调用点有 **4 条**：

| 调用点 | 场景 | 是否发奖 |
|---|---|---|
| `room_watchdog.go:184` | 冷却期未启用 / 已冷却过 → 立刻关门 | ❌ 漏 |
| `room_watchdog.go:205` | `restartVoteDone` 或其他 over 子状态 | ❌ 漏 |
| `room_restart_vote.go:136` | 冷却期结束 `finishCoolingLocked` | ❌ 漏 |
| `room_restart_vote.go:353` | `forceCloseRoomLocked`（重开投票被拒/超时）| ✅ 唯一命中 |

**最讽刺的是** `settlement_reward.go:10` 的文件头注释白纸黑字写着「§130 接线验证：
必须在终局路径**实际调用**」—— 作者意识到了这个风险、写下了自检条款，然后仍然只
接了 1/4 条路径。§129 冷却期（默认 30 分钟）是**最常见**的终局路径，也就是说
**绝大多数对局的结算奖励从未发放过**。

### 2.2 缺陷二：只发给存活玩家

`settlement_reward.go:238` `if !p.Alive { continue }` —— 胜方阵营里被刀/被毒/被票
出局的人类玩家拿不到折扣券，败方死者也拿不到安慰包。狼人杀里「阵营胜利」与「个人
存活」是两个正交概念（`computeCoinDelta` 的金币结算就**不**看 alive），此处
alive 过滤既无设计文档依据，也与同文件的胜负判定语义矛盾。

按用户决策：**去掉 alive 限制**，死亡玩家按其阵营同样发奖。

### 2.3 设计 — 收口到单一发放点 + 幂等守卫

**不**在 4 处 `EmitGameOver` 前各抄一遍调用（那正是 §135 教训 (1)「同一逻辑被独立
复制 4 遍」的复现）。改为**收口进 `EmitGameOver` 自身**：

```go
func (m *WerewolfManager) EmitGameOver(r *WerewolfRoom, winner string) {
    if r == nil { return }
    // §20260811-08 U2 — 终局奖励收口发放(§130 接线修复)。
    // 旧版仅 forceCloseRoomLocked 一条路径调用,冷却期/watchdog 三条路径漏发。
    // 移到此处后 4 条终局路径自动覆盖;settlementRewarded 保证幂等。
    m.grantSettlementRewardsLocked(r, m.rewardSvc)
    ...
}
```

**§92a 锁态核对（关键）**：4 个 `EmitGameOver` 调用点是否都持 `r.mu`？

| 调用点 | 所在函数 | 锁态 |
|---|---|---|
| `room_watchdog.go:184` | `phaseWatchdogTick` | ✅ 持 `r.mu` |
| `room_watchdog.go:205` | `phaseWatchdogTick` | ✅ 持 `r.mu` |
| `room_restart_vote.go:136` | `finishCoolingLocked` | ✅ 持 `r.mu`（`*Locked` 命名） |
| `room_restart_vote.go:353` | `forceCloseRoomLocked` | ✅ 持 `r.mu`（注释明示） |

四条全部持锁，`grantSettlementRewardsLocked` 保持锁内变体语义不变，**不需要**
新建公开变体。`room_restart_vote.go:352` 的原调用**必须删除**，否则
`forceCloseRoomLocked` 路径会连调两次（幂等守卫会挡住，但留着是误导）。

**幂等守卫**：新增 `WerewolfRoom.settlementRewarded bool` 字段。虽然
`gameOverNotified` 已在多数路径上做了一层保护，但 `EmitGameOver` 自身没有该守卫
（`room_watchdog.go:205` 分支先置 `gameOverNotified=true` 再调用），且 §129 冷却期
+ 重开局会让同一 room 对象经历多局 —— `restartGameLocked` 必须一并重置该标志
（对照 `resetCoolingStateLocked` 的既有模式）。

### 2.4 §118 边界

`GrantVictoryDiscount` / `GrantConsolationProp` 内部走 `t_lsm_game_kv` 表写入，
失败仅返回 error 且调用方 `_ =` 丢弃 —— 保持「异步持久化不阻塞游戏流」的既有约束
（§118）。本批次**不**改变该错误处理策略，但补一条 `logger.Warn` 让失败可观测
（当前完全静默，是这个 bug 拖到今天才被发现的次要原因）。

---

## U3 — `GodModeSnapshot` 补 4 类公开事件（§8.7 数据地基 / M3 §3 S2）

### 3.1 缺陷证据（§87 验证）

`populateGodModeLocked` 从 `InformationLedger` 聚合了 3 类夜间行动
（`night_seer` / `night_witch` / `night_guard`），但账本里另有 **4 类已写入却从未
被 GodMode 消费**的公开事件：

```
$ grep -rn "InfoSourceHunterShot\|InfoSourceKnightDuel\|InfoSourceDemonHunter\|InfoSourceIdiotReveal" \
    ServerGo/game/werewolf/*.go | grep -v _test | grep -v information_le
room_action.go:110:  r.ledgerAppendLocked(InfoSourceIdiotReveal,  "idiot_reveal seat=%d")
room_action.go:238:  r.ledgerAppendLocked(InfoSourceKnightDuel,   "knight_duel seat=%d target=%d hit_wolf=%v")
room_action.go:275:  r.ledgerAppendLocked(InfoSourceDemonHunter,  "demon_hunter seat=%d target=%d hit_wolf=%v")
room_action.go:625:  r.ledgerAppendLocked(InfoSourceHunterShot,   "hunter_shot seat=%d target=%d")
```

四类事件**全部有写入点**（§20260810-05 一期已接线），但 `view_godmode.go` 的
`switch e.Source` 只有 3 个 case。上帝视角面板因此看不到「谁开了枪 / 谁决斗了 /
谁狩猎了 / 谁翻了白痴牌」—— 而这四类恰恰是 §135 白名单里**身份已公开**的事件，
观战者本就有权看到，隐藏它们没有任何公平性收益。

同时这是 §8.7「夜间血迹图」的数据地基：血迹图需要「猎魔人狩猎目标箭头」，而
`GodModeSnapshot` 当前根本不携带该数据。

### 3.2 设计 — 统一 `PublicActions` 列表，不做 4 个平行字段

**不**新增 `HunterShots []` / `KnightDuels []` / `DemonHunts []` / `IdiotReveals []`
四个平行切片（那会让 `GodModeSnapshot` 从 9 字段膨胀到 13 字段，且前端要写 4 段
几乎相同的渲染）。改为单一：

```go
// PublicActionEntry §20260811-08 U3 — 已公开的技能行动条目。
// 涵盖 §135 身份公开白名单中的 4 类事件(猎人开枪/骑士决斗/猎魔人狩猎/白痴翻牌),
// 这些事件本就全房可见,GodMode 聚合它们不构成新的身份下发通道。
type PublicActionEntry struct {
    Day    int    `json:"day"`
    Kind   string `json:"kind"`             // "hunter_shot"/"knight_duel"/"demon_hunter"/"idiot_reveal"
    Seat   int    `json:"seat"`             // 行动者
    Target int    `json:"target"`           // 目标(-1 = 无,如白痴翻牌)
    HitWolf *bool `json:"hit_wolf,omitempty"` // 仅决斗/狩猎有;nil = 不适用
}
```

`GodModeSnapshot` 新增 `PublicActions []PublicActionEntry \`json:"public_actions"\``。

**解析复用**：`parseSeatTargetPair(fact, prefix)` 已支持 `"<prefix> seat=%d target=%d"`
格式，`hunter_shot` 直接可用。`knight_duel` / `demon_hunter` 多一个 `hit_wolf=%v`
后缀，新增 `parseSeatTargetHitWolf` helper；`idiot_reveal` 只有 `seat=%d`，新增
`parseSeatOnly`。三个 helper 与既有 `parseWitchTriple` 同风格，**解析失败静默跳过**
（与既有行为一致，见 `view_godmode.go` 注释「解析失败跳过」）。

### 3.3 §121 数据形状契约

`view.go:1382` 的注释声称「前端 `types/werewolf.ts` 必须显式声明此 struct，后端
`DisallowUnknownFields` 校验已就位」—— **这句话是错的**：`DisallowUnknownFields`
只出现在入站解码器（`api/model_admin_api.go` / `api/room_api.go` / `ws/user_service.go`），
出站 `ClientGameState` 没有任何 schema 校验。本批次顺手修正该注释为实情
（「手工双文件维护，无编译期保障」），避免下一个人依赖一个不存在的护栏。

前端 `ClientWeb/src/types/werewolf.ts` 的 `GodModeSnapshot` 同步新增
`public_actions?: PublicActionEntry[]`，`GodModeView.tsx` 增加一个「⚔️ 公开技能」
分区。

---

## U4 — `DayNightOverlay` 补 3 个未覆盖阶段（§11.5 T1 / Mimo §8.1）

### 4.1 缺陷证据（§87 验证）

`DayNightOverlay.tsx:24-38` 的两个 Set 覆盖了 10 个 phase，但引擎
（`engine.go:42-90` `Phase.String()`）实际有 15 个：

| phase | 归类 | 覆盖 |
|---|---|---|
| `pre_wolves` / `night_wolves` / `night_seer` / `night_witch` | 夜 | ✅ |
| **`night_guard`** | 夜（§134 守卫，狼刀**之前**）| ❌ 漏 |
| **`night_demon_hunter`** | 夜（§猎魔人）| ❌ 漏 |
| `dawn` | 黎明 | ✅ |
| `speak` / `vote` / `sheriff` / `death_lyric` / `hunter_shoot` / `idiot_reveal` | 昼 | ✅ |
| **`sheriff_order`** | 昼（§20260810-09 警长定序）| ❌ 漏 |
| `filling` / `restart_vote` / `over` | 非对局态 | ✅ 有意不覆盖 |

引擎的 `IsNight()`（`engine.go:99`）已经把 `night_guard` 与 `night_demon_hunter`
正确归入夜晚，**前端的 `NIGHT_PHASES` 是引擎 `IsNight()` 的一份手工副本，且落后了
两个版本** —— §134 守卫与猎魔人上线时漏改了这里（§134 教训 (6)「新增夜间阶段必须
同步五处」清单里没有前端这一处）。

后果：13 人局带守卫时，`night_guard` 阶段没有「天黑请闭眼」遮罩；从 `night_guard`
切到 `night_wolves` 时才第一次触发夜晚遮罩，**时机整整晚了一个阶段**。

### 4.2 设计

- `NIGHT_PHASES` 补 `night_guard`、`night_demon_hunter`（与引擎 `IsNight()` 逐项对齐）。
- `DAY_PHASES` 补 `sheriff_order`。
- 在文件头加一条**契约注释**指向 `engine.go:99 IsNight()`，并把 §134 教训 (6) 的
  「五处同步」清单**扩充为六处**（新增「前端 `DayNightOverlay.NIGHT_PHASES`」），
  同步写回 CLAUDE.md §13 lesson 134 —— 这是本批次唯一的规则文件改动，属于 §13
  教训表补充，不违反 §22（那条禁止的是**自动修复流程**写规则文件）。
- 保持既有 `pointer-events: none` + `setTimeout` 自动消失（§124），不改 CSS。
- 保持硬编码中文（该组件刻意不走 `t()`，见其文件头与 `SheriffElectedOverlay.tsx:12`），
  本批次不顺手改 i18n —— 那是独立议题，混进来会让 diff 失焦。

---

## U5 — 模型风格标识符 `ModelStyle`（§8.13 / M3 §5 P2）

### 5.1 现状

座位卡（`WerewolfTable.tsx:508-518`）只渲染 `player.agent_name` 纯文本
（如 `Xiaomi mimo-v2.5-pro`，10px、`max-width:70%`、ellipsis）。13 人局 4 行密集
布局下，8 个模型名互相之间几乎无视觉区分度，观战者无法一眼看出「谁投了谁」。

### 5.2 设计 — 纯前端派发表，零后端改动

新增 `ClientWeb/src/components/werewolf/modelStyle.ts`（≤120 行），按 `agent_name`
子串匹配派发 emoji + 色相：

| 模型 | emoji | 色相 | 流派标签 |
|---|---|---|---|
| DeepSeek | 🐋 | 蓝 | 逻辑流 |
| 豆包 DouBao | 🥟 | 橙 | 情绪流 |
| 智谱 GLM | 🧠 | 紫 | 教科书 |
| Kimi | 🌙 | 靛 | 稳健守卫 |
| MiniMax | ⚡ | 黄 | 激进冲锋 |
| Qwen | 🦉 | 青 | 稳健守卫 |
| 美团 LongCat | 🐱 | 红 | 戏精型 |
| Xiaomi mimo | 🍚 | 灰蓝 | 冷面计算 |
| （未匹配）| 🤖 | 中性灰 | — |

**为什么放前端**：模型列表由 admin 可增删（§118 模型管理），后端加 `BrandStyleKey`
字段意味着每加一个模型都要改 DB + API + 前端三处；前端派发表 + `🤖` 兜底让新模型
零改动可用。这与 §137 教训 (1)「条件判断漏枚举是 P0 高发区」的规避思路一致 ——
**有兜底分支的派发表**比**必须穷举的枚举**更抗腐化。

**§26 对比度硬约束**：座位卡在 `.is-night` 下有 `brightness(0.4)` 滤镜，色相若只
改文字颜色会在夜晚不可辨。方案：emoji 前缀（不受 brightness 影响的字形）+
`box-shadow` 细光晕（§26.2 反模式 4 明确要求「必须加 box-shadow 光晕，不被 night
brightness 衰减」），**不**用低透明度背景色（§26.2 反模式 2）。

**§26.6 验收**：写完 JSX 立即 `grep -rn "ww-model-style" ClientWeb/src/styles/`
确认 CSS 命中（§26.5 三件套规约：JSX 拼接 + CSS 类规则 + reduced-motion 兜底）。

**CSS 落点**：`styles/werewolf-v2.css` 当前 1752 行，距 §4 硬上限 1800 仅剩 48 行。
新样式（~35 行）放入既有 `styles/werewolf-emotion.css`（355 行，与「座位卡视觉
标识」主题同族）而非 v2，避免逼近上限后被迫紧急拆分。**不**新建 CSS 文件 ——
`globals.css` 的 @import 顺序是级联优先级（§2.1 硬约束 4），新增一行就要重新
论证整条覆盖链。

**i18n**：流派标签 8 个 + 兜底 1 个 = 9 键，走 `werewolf.modelstyle.*`；按 §23 教训
必须**同时**进 `i18n/types.ts` + zh-CN/en/ja 三语文件（当前 417 个 `werewolf.*`
键三语完全同步，本批次后为 426）。

---

## 6. 实施顺序与验收

### 6.1 顺序

1. **U2**（纯后端、无前端依赖、修复面最大）→ `go build` + `go test ./game/werewolf/...`
2. **U1**（后端为主 + TS 类型）→ 同上 + `tsc --noEmit`
3. **U3**（后端 + TS 类型 + GodModeView 渲染）→ 同上
4. **U4**（纯前端，3 行 Set 改动）→ `tsc --noEmit` + `npm run build`
5. **U5**（前端 + i18n）→ 同上 + §26.6 grep 自检

### 6.2 回归测试

新增 `ServerGo/game/werewolf/upgrade_20260811_08_test.go`：

| ID | 断言 |
|---|---|
| U1-01 | `populateGodModeLocked` 后 `PerSeatPOV[seat].HeartThought` 非空（bot 座位有 HeartThought 时）|
| U1-02 | `PerSeatPOV[seat].RoleRevealed` 在**狼自爆**后为 true（旧手写判定会漏，反向验证）|
| U1-03 | `PerSeatPOV` 的 `ToolCallCount` / `LLMCallCount` 与 BotTranscript 一致 |
| U1-04 | 玩家视图（`viewer>=0`）`cs.GodMode == nil`（§135 不回归）|
| U2-01 | 4 条 `EmitGameOver` 路径调用后 `settlementRewarded == true` |
| U2-02 | 同一 room 连调两次 `EmitGameOver`，`GrantVictoryDiscount` 只发生 1 次（幂等）|
| U2-03 | **死亡**的胜方人类玩家同样收到 `victory_discount_voucher` |
| U2-04 | `restartGameLocked` 后 `settlementRewarded` 被重置为 false |
| U3-01 | 猎人开枪后 `PublicActions` 含 `kind=="hunter_shot"` 条目 |
| U3-02 | 骑士决斗后条目的 `HitWolf` 非 nil 且值正确 |
| U3-03 | 账本 fact 格式损坏时静默跳过，不 panic、不产出半条目 |

**§212 教训**：所有持锁调用的测试必须 **持 `r.mu` + 超时守卫**，否则在未持锁的
宽松环境下假通过。**且新写的回归测试必须先在缺陷代码上验证它确实失败**（U1-02 /
U2-01 / U2-03 三项做双向验证：还原缺陷 → 测试失败 → 恢复修复 → 测试通过）。

### 6.3 验收清单

- [ ] `cd ServerGo && go build -o LsmAgentGame main.go`
- [ ] `cd ServerGo && go test ./... `（全绿）
- [ ] `cd ClientWeb && npx tsc --noEmit`
- [ ] `cd ClientWeb && npm run build`
- [ ] `grep -rn "ww-model-style" ClientWeb/src/styles/` 至少 1 命中（§26.6 防「声明了却从不接线」）
- [ ] 三语 `werewolf.*` 键数一致（`grep -c "'werewolf\." zh-CN.ts en.ts ja.ts` 三值相等）
- [ ] `./rebuild_restart_app.sh` 启动无 panic
- [ ] 所有代码文件 ≤ 1800 行（§4）

---

## 7. 本批次沉淀的教训（拟写入 CLAUDE.md §13）

> **§20260811-08 —— §130「声明了却从不接线」模式的第 N 次集中复现**
>
> 1. **「后续 V2 补」注释是最有效的缺陷伪装**：`view_godmode.go:106` 用「实际数据由
>    前端通过单独 spectator-only endpoint 拉取」解释 7 个空字段，该 endpoint 从未
>    存在。审计时凡遇到「本字段先占位，后续 X 会补」，必须立刻 grep X 是否存在 ——
>    与 §134「prompt.go 主动声明『暂无独立工具』把缺陷伪装成设计」完全同构。
> 2. **作者写下的 §130 自检条款不等于执行了自检**：`settlement_reward.go:10` 明写
>    「必须在终局路径**实际调用**」，然后只接了 1/4 条路径。**注释里的自检条款必须
>    转化为测试断言**，否则它只是一句愿望。
> 3. **§135 单点判定会被后来者绕过**：`RolePubliclyRevealed` 落地后，
>    `view_godmode.go:116` 又手写了一份漏掉「狼自爆」的身份公开条件。单点判定不是
>    一次性工程 —— 新增任何「这个座位的身份能不能看」的判断前必须 grep
>    `RolePubliclyRevealed`。**建议 CI 加 lint：`view*.go` 中出现
>    `IdiotRevealed ||` / `HunterFired ||` 的裸组合即告警。**
> 4. **前端是「五处同步」清单里被系统性遗忘的第六处**：§134 守卫、§猎魔人、
>    §20260810-09 警长定序三次新增 phase，三次都漏改
>    `DayNightOverlay.NIGHT_PHASES`。凡引擎侧存在 `IsNight()` 这类**分类函数**，
>    前端的手工副本必须在函数注释里互相指认。
> 5. **派发表 + 兜底分支 > 穷举枚举**（U5）：模型列表 admin 可增删，前端 `🤖` 兜底
>    让新模型零改动可用；对照 §137 教训 (1) `isExposeProp` 漏枚举的 P0。

---

## 8. 相关文档

- 上一批次：[`§20260811-07`](死后幽灵语音与自动高光集锦.md) — 死后幽灵语音 / 自动高光集锦战报
- 上帝视角原始落地：[`§20260810-09`](上帝视角观战与警长定序权.md) / [`§20260810-11`](警徽流与投票节奏4项增强.md)
- 信息账本：[`§20260810-05`](信息账本一期-17个InfoSource后端落地.md) / [`§20260810-08`](信息账本二期-消费侧接入与说漏嘴检测.md)
- 前端 UI 对比度规范：[`前端UI颜色对比度与可读性规范`](../狼人杀-前端UI/前端UI颜色对比度与可读性规范.md)
- 守卫角色（§134 五处同步来源）：[`狼人杀守卫角色设计`](../狼人杀-角色设计/狼人杀守卫角色设计.md)
