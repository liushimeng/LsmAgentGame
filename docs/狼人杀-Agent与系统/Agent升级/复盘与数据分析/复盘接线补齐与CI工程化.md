# 狼人杀 13 人局 Agent 升级 — §20260814-01

> **批次**：第 29 批（§20260814-01）
> **来源**：`狼人杀13人局-Agent多模型建议合并-待实施项-20260813.md`
> §10.4 T10「Agent 并发调优」/ §11 P8-C「个人复盘完整版（含后端聚合）」/
> §1「CI 自定义 lint 工程化改进」+ 审计中新发现的 3 项 §130 接线缺陷
> **范围**：4 项升级（U1 复盘与面板接线 / U2 难度发言节奏 / U3 法官解说信号量 / U4 CI 与拆分）
> **实际代码量**：新增 11 个文件 1641 行 + 修改 26 个文件（+522 / −494）

---

## 选型说明：为什么不是按文档字面挑

按待实施项文档的优先级表，`P5-F 双过程思维模型` 标注「工作量 2w，
§122 ParallelThink 复用，**不新建模块**」——看起来是高价值中等成本项。

**但它已经不可行**：`ParallelThink` 在 §128「对话即思考」重构中被**完全删除**，
现在只剩两处墓碑注释：

```go
// agent/wwplayer/run.go:754
// §128 对话即思考重构:ParallelThink 调用已删除(原 §122)。

// agent/wwplayer/agent.go:423
// §128 对话即思考重构:ParallelThink 相关字段已删除(原 §122)。
```

优先级表里「复用现有并行 worker」的成本假设因此失效——它现在是从零实现，
而且与 §128 确立的「对话即思考，LLM 不在思考、它在响应」哲学直接冲突。

> **教训（新增）**：**待实施项文档的「复用 X」成本估算会随 X 被删除而静默失效。**
> 优先级表是**快照**，不是当前事实。实施前必须 grep 确认它依赖的基础设施还活着——
> 这与 §134「凡遇『暂无独立工具』必须 grep 验证」、§20260811-08 教训 (1)
> 「凡遇『后续 V2 补』必须立刻 grep X 是否存在」是同一条纪律，
> 只是这次对象换成了**成本估算的前提**。

改为选取审计中发现的 **3 项新 §130「声明了却从不接线」实例** + 文档 §1 自留的
CI 工程化项。四项全部满足：① 复用既有基础设施；② 不新增 game phase（避开 §97
六处同步）；③ 不新增 LLM 调用（避开 §197 长预算与 token 成本）；④ 以修复接线为主。

---

## U1：个人复盘后端补齐 + 5 个孤儿面板接线（P8-C + §126/§130 清算）

### 动机

审计发现 **5 个前端分析面板共 ~782 行零 import**：

| 面板 | 行数 | 后端状态（审计时） |
|---|---|---|
| `SecretLetterPanel` | 161 | ✅ 路由 `router.go:197-198` + i18n 7 键 + CSS |
| `FactionBetPanel` | 145 | ✅ 路由 `router.go:200-201` + i18n 7 键 + CSS |
| `MindMirrorPanel` | 164 | ✅ localStorage（`hooks/useMindMirror`）+ i18n 8 键 + CSS |
| `TrustTraceChart` | 114 | ❌ `ParseTrustTrace` **零生产调用**；`gameState` 无 `trust_trace` |
| `PersonalReviewPanel` | 198 | ❌ `GET .../review/:userId` **路由不存在** |

前三个「万事俱备，只差一个 import」——正是 §126 的原话：
**组件存在但未被 import 等于不存在**。

后两个更严重，**后端整个模块都是死的**：

- `game/werewolf/recall_aggregate.go`（335 行，含 4 维聚合 + 30min TTL 缓存 +
  单测）**零生产调用**。而它的文件头 `:11` 自己写着：

  > `§130 接线:ComputeReviewFromInputs 必须被 ComputeReviewForUser(Manager) 调一次`

  **而 `ComputeReviewForUser` 从来没有被实现过。**
- `agent/wwjudge/judge_trust_trace.go` 的 `ParseTrustTrace` 同样零调用点，
  文件头 `:14` 也写着「本文件由 ParseSummary 调用一次生产注入点」。

> 这是 §20260812-04 教训 (1)「注释里的自检条款不会被执行，必须转化为测试断言」
> 的又一次印证——**两个作者都写下了正确的接线要求，然后都没有接线。**

### 落地

**后端 — 新增 `game/werewolf/recall_review_bridge.go`（~270 行）**

1. `ComputeReviewForUser(ctx, roomID, userID)` — 补齐那个缺失的入口。
   §92a 严格照抄 `recall_chat.go:189` 的快照范式：
   `m.mu.RLock` 取房间 → `lockRoomBriefly(500ms)` → 校验 `PhaseGameOver` →
   **锁内**构造 `PersonalReviewInputs` → `Unlock` → **锁外**调纯函数聚合 → 写缓存。

2. `recordVoteHistoryLocked(tally)` — 逐日票型采集。

   **为什么需要新字段**：`State.LastDayVoteMap`（`engine.go:360`）只保留
   **最后一天**（每轮 `fillDayVoteMapLocked` 整体覆盖），而复盘要算的是整局每天
   是否投中被放逐者。新增 `WerewolfRoom.voteHistory []VoteReviewRecord`（上限 16 天）。

   **三条 FinishVote 路径全部接上**（漏一条 = 该天票型静默丢失）：

   | 路径 | 文件:行 | tally 快照来源 |
   |---|---|---|
   | 人类/正常 | `room_action.go:333` 后 | 复用既有 `prevTally` |
   | driver auto-tally | `room_quarantine_skip_locked.go:428` | **新抓**（原先没有） |
   | quarantine/watchdog 救援 | `room_quarantine_skip_locked.go:458` 后 | 复用既有 `preTally` |

   **时序约束（本项最容易写错的地方）**：tally 必须在 `FinishVote` **之前**抓
   （之后 `TallyVotes` 输入状态已被改写，不可重现），而历史必须在 `FinishVote`
   **之后**写（`DayEliminated` 才有值）。这个错位是 `recordVoteHistoryLocked`
   必须接受 `tally` 参数、不能自己计算的唯一原因。

3. **座位过滤的坑**：`computeVoteAccuracy`（`recall_aggregate.go:94`）用的是
   「取第一个非 -1 且非自投的票当作『我』的票」这一启发式。该启发式**只在输入
   已按座位过滤时才正确**——直接塞全场快照会把别人的票算成我的。故
   `ComputeReviewForUser` 显式构造「只含我一票」的切片。回归测试 R05 通过
   「注入全表快照 → HitCount 从 2 变 3」双向验证了这一点。

4. **诚实的已知缺口**：`InteractionsInitiated/Responded` 保持 0——引擎目前
   **不按 userID 累计**质询次数（`ChallengeCount` 只在 `view_godmode.go:231`
   被硬编码为 1）。该维度显示 0 分而非编造数字；待 §4.1「定向质询机制」落地后接上。
   **宁可显示 0 也不编造——复盘一旦给出假分数就失去全部价值。**

**后端 — 信任度轨迹（零新增 LLM 调用）**

`judge_summary_bridge.go:121` 在解析法官总结时**复用同一份响应 text** 调
`ParseTrustTrace`，存入 `WerewolfRoom.judgeTrustTrace`，经 `view.go` 下发
`ClientGameState.TrustTrace`。

**§135 核对**：`TrustTraceEntry` 只有 `{seat, day, score}`，**不含 Role/Faction**
（`judge_trust_trace.go:15` 已声明该约束），故可对**全员**下发，不需要
spectator 隔离。前端也**刻意不传** `factionBySeat`——那会把阵营染色泄漏给所有人。

**API** — `GET /api/games/werewolf/rooms/:roomId/review/:userId`

路径与前端 `PersonalReviewPanel.tsx:81` 已写死的 URL **逐字符对齐**。权限：
JWT + `isRoomMember`（照抄 RecallChat）+ **`:userId` 必须等于调用者**。

> **权限守卫的位置很关键**：`targetUID != uid` 的检查必须在调用 Manager
> **之前**。否则 `ComputeReviewForUser` 会把他人的 `Role` 算进 `PersonalReview`
> 并**写进 30min 缓存**——即便 handler 随后拒绝返回，缓存里也已留下一份
> 可被后续请求命中的越权数据。

**前端 — 5 个面板全部接线**

- `SecretLetterPanel` + `FactionBetPanel` → `WerewolfGamePage.tsx` 中栏动作区
  （与 `CommitmentPanel` / `PropPanel` 同门禁：`!spectator && !iAmDead`，
  `windowOpen = phase === 'speak'`，与后端校验窗口一致）。
- `MindMirrorPanel` / `TrustTraceChart` / `PersonalReviewPanel`
  → `HistoryDrawer` 第 13/14/15 sub-tab。三者**均非** spectator-only。
- `MindMirror` 的 Agent 侧输入由 `bot_hypotheses` 折叠而来，
  **只导出 faction + confidence，绝不透传 `role_guess` 明文**（§135）。

**顺手修的既有 bug — HistoryDrawer `sub` 状态从不重置**

`HistoryDrawer.tsx:384` 的注释声称「sub 状态若玩家切换前是 infoflow，
会落到第一个可见 tab」，**但代码里从来没有那个 fallback**。实际行为：
非观战者停留在 spectator-only tab 时，tab 条无 `is-active` 项，而 body 每个
渲染分支都带 `&& spectator` 守卫 ⇒ **整片空白**，用户以为抽屉坏了。

> 又一例「注释描述了一个不存在的行为」（§20260812-04 教训 2：
> **注释与实现不符应当像编译错误一样对待**）。现已用 `useEffect` 真正实现。

**CSS** — 新建 `styles/werewolf-20260814-01.css`。`werewolf-v2.css` 已 1752 行
（距 §4 上限仅 48 行），按 §20260812-03 / §20260813-02 建立的「新特性开新日期文件」
约定插在 `werewolf-20260813-02.css` 之后、`werewolf-speech.css` 之前，
不触碰 `.werewolf-seat` 覆盖链。内容：12→15 tab 后的排版收紧
（**不降低 `--ww-touch-target`**，只收 padding/字号 + 补选中态内发光，§26.2 反模式 4）。

---

## U2：难度档位接上发言节奏（§130 同 struct 第三个死字段）

### 动机

`DifficultyProfile.SpeakLimiterScale`（`difficulty.go:34`）自 §20260811-09 U2
落地起 **4 处赋值、0 处生产读取**。难度分级对外宣称能调节「发言节奏」，
这一维度实际完全无效。

这是**同一个 struct 内的第三个**同病字段：

| 字段 | 修复批次 |
|---|---|
| `MemoryInjectRunes` | §20260812-04 U4 |
| `MaxToolUse` | §20260813-04 U3 |
| `SpeakLimiterScale` | **本次** |

> 正是 §20260813-04 教训 (3)「修完一个缺陷必须 grep 同 struct 其他字段是否同病」
> 所指的情形——**前两次修复都没有回头扫一遍这个 struct。**

### 落地

- `agent/core/ratelimit.go` 新增 `SetInterval` / `Interval`（持 limiter 自身
  mutex；所有 `interval` 读取本来就在 `l.mu` 下，故无需额外同步）。
- 新建 `agent/wwplayer/difficulty_speak.go`（~135 行）：
  `SetDifficultySpeakScale` clamp 到 `[0.5, 3.0]`，重算三个 limiter。
- `room_agent.go` 在既有 `SetDifficultyRoundCap` 旁一行注入。

**效果**：easy 1.5× → speak 30s→45s；hell 0.8× → 24s；
**normal 1.0× 直接 return，三个 limiter 保持构造期原值 = 逐字节零回归**
（守 §20260811-09 的 prompt-cache 纪律）。

**三个 limiter 同步缩放而非只缩放 speak**：whisper/interject 与 speak 的
相对比例是 §R76 P1-3 反刷屏设计的一部分（interject 60s > speak 30s）。
只缩放 speak 会让 easy 档从 2:1 变成 4:3，bot 显得「正经发言少、插话多」，
与 easy 档「保守、发言简短」的 directive 相悖。

**幂等**：以**固定基准常量** × f 计算，而非「当前值 × f」。后者会让重复注入
同一档位累积缩放（easy 调两次 → 2.25×）。测试 D07 专门锚定这一点。

### 发现的真实缺陷（测试先抓到）

D08（nil limiter 不 panic）在首次运行时**直接 SIGSEGV**：我原先把
`scaleLimiter` 的形参写成 `interface{ Interval(); SetInterval() }`，
传入 nil 的 `*SpeakLimiter` 会得到「类型非 nil、值为 nil」的接口，
`l == nil` 恒为 false → 解引用空指针。这是 Go 的 **typed-nil 陷阱**。
改为具体类型 `*agentcore.SpeakLimiter` 后修复。

> **教训（新增）**：**nil 守卫写在 interface 形参上等于没写。** 凡「可选依赖」
> 的 helper，形参用具体指针类型；用 interface 时 nil 检查必须
> `reflect.ValueOf(x).IsNil()` 或在调用方拦截。

### wiring lint 联动（本项最重要的机制化收获）

`difficultySpeakScale` 是**值类型 `float64`**，而 `wiring_lint_field_test.go:166`
的 `isRefType` 只覆盖指针/map/chan/func/interface/slice，**对值类型返回 false**
——那条 AST 泛化 lint **抓不到它**。

> **这正是 `SpeakLimiterScale` 能在 §20260812-04 U4 与 §20260813-04 U3
> 两轮同 struct 修复中都活下来的原因：两条 lint 的交集之外存在盲区。**

故显式加入 `wiring_lint_test.go` 的 `mustWire` 表（该断言校验 setter 存在
**且有生产调用点**）。已双向验证：注释掉注入 → lint FAIL；恢复 → PASS。

**顺手清理过期白名单**：`knownDeadFields` 里的 `steeringQueue` / `toolHooks`
两项已由 §20260813-04 U1/U2 真实接线，白名单条目自那次起就是过期的。
**过期白名单比没有白名单更危险**——它让已修好的字段看起来仍是已知缺陷，
下一个审计者会跳过它们。

---

## U3：法官/解说纳入房间级 LLM 信号量（§10.4 T10）

### 动机

房间级信号量 `WerewolfRoom.llmSema`（cap 默认 4）此前**只**注入 player bot
（`room_agent.go:282`）。法官与解说各自直连 `Provider.Chat`：

```
agent/wwjudge/judge.go:411              resp, err := j.Provider.Chat(cctx, ...)
agent/wwcommentator/commentator.go:262  resp, err := provider.Chat(ctx, apiKey, req)
```

二者只受**时间间隔**限流（announceLimiter 15s / summaryLimiter 60s / 解说 45s），
完全不受**并发**约束。所以 cap=4 的 13 人局实际在飞 LLM 调用可达 **6**
（4 bot + 法官 + 解说）——**超配置值 50%**。

而 `config.go:91-97` 记录了 §130 移除信号量导致的级联故障：
**「6min 2.5% → 27min 66% 失败率」**。信号量恢复为有界后，这条敞口一直存在。

### 落地

- `wwjudge` 新建 `judge_llm_slot.go`；`wwcommentator` 内联同款三方法
  （逐字复用 `wwplayer/agent.go:457-496` 的语义，**不新建机制**）。
- **抽出 `ensureLLMSemaphoreLocked()` 作单一事实来源**（`room_config.go`）。
  原先懒创建逻辑写在 `StartAgentsLocked` 内，而 `startJudgeGoroutine` 有
  **5 个调用点**且与它的先后顺序不保证。两处各写一份 `if nil { make }`
  迟早漂移出「法官拿到 nil、bot 拿到有界 chan」的半失效状态。
- 注入点：`judge_summary_bridge.go`（法官）+ `commentary_room.go`（解说）。

### 关键设计决策：wait 预算差异化

| 调用方 | 预算 | 抢不到时 |
|---|---|---|
| player bot | `llmSlotAcquireWait = 5s` | `scheduleReWake` 稍后重试 |
| 法官 / 解说 | `2s` | **直接放弃本次播报**，不重试、不计失败 |

法官旁白失败有 `JudgeFallbackText` 硬编码兜底，解说失败则本轮静默；
二者都**不推进 phase**（phase 由 watchdog 驱动，`judge.go:406-407` 的注释
明确写了这一点）。这与 `run.go:1952-1958` 的 `speak_floor_tick`
「槽位满则静默跳过本 tick」是同一条纪律：
**装饰性 LLM 调用必须给推进游戏的调用让路。**

### 解说侧的一个陷阱（差点写成 bug）

我最初把 `acquireLLMSlot` 放进 `chatOrFallback` 并让它返回 `ok=false`。
但 `handleEvent:264` 对 `!ok` 的处理是 `c.consecutive++`，**连续 5 次即
quarantine**。槽位繁忙是**瞬态资源竞争，不是解说自身故障**——13 人局高峰期
5 次抢不到轻易发生，会把解说**永久打死**。

改为在 `chatOrFallback` **之外**获取，失败直接 `return`（不经过失败计数路径）。
这与 §112「speak_floor 失败不计入 consecutiveFailures，否则误 quarantine」
及 §20260812-04 U5「endpoint breaker 必须列为 transient」是同一条教训。

---

## U4：CI workflow（文档 §1 自留项）+ run.go 拆分

### 动机

待实施项文档 §1 原文：

> CI 加 `unparam` + 自定义 lint（检测「声明未用的函数参数」与「零写入点的
> struct 字段」）可自动化拦截此类缺陷——**此项工程化改进本身待做**。

而 `.github/` 下**零 workflow**（只有 dependabot / FUNDING / 模板）。也就是说
仓库里那 3 个专为根治 §130 而写的 lint 测试文件
（`wiring_lint_test.go` / `wiring_lint_field_test.go` / `invariant_wiring_test.go`）
**从未在 CI 里跑过一次**。

> CLAUDE.md 已四次记录「注释里的自检条款不会被执行」。
> 同理：**lint 写了不跑，与没写等价。**

### 落地

**`.github/workflows/ci.yml`（144 行，3 个 job）**

| job | 内容 |
|---|---|
| `backend` | `go build` + `go vet` + `go test ./...`（含 3 个接线 lint） |
| `frontend` | `npm install` + `tsc -b --noEmit` + `npm run build` |
| `project-checks` | 符号链接自检（§10.2）+ CLAUDE.md 行数（§3）+ 单文件行数（§4） |

- **符号链接自检**正是 CLAUDE.md §10.2 建议了却一直没加的那一步。
  `AGENTS.md` 一旦变成普通文件副本，两份规则会各自漂移，
  **而漂移不会报错**，只会让不同 Agent 工具看到不同的项目规则。
- **用 `npm install` 而非 `npm ci`**：本仓库**没有提交 `package-lock.json`**
  （只 gitignore 了 `node_modules/`）。`npm ci` 与 `setup-node` 的
  `cache: npm` 都要求 lockfile 存在，会直接失败。提交 lockfile 是更好的做法，
  但会改变依赖解析结果、需要独立验证，留作独立提交。

**`scripts/check_line_limit.sh`（99 行）— 带棘轮的 §4 检查**

引入时已有 4 个文件超限。若不设白名单，CI 从第一天就是红的——
**而长期红的 CI 等于没有 CI**，团队会学会忽略它。

故白名单记录**当时行数作为允许上限**：文件可以变短，**一旦变长就 fail**
（棘轮效应）。纪律与 `wiring_lint_test.go:83` 的既有约定一致：**只允许变短**。
已验证：给 `room.go` 加 3 行 → 立即 `❌ baseline 恶化`。

**run.go 拆分：2154 → 1684 行**

搬出 `run.go:30-499` 到新文件 `run_config.go`（509 行）：内层轮次上限 /
超时与预算 / `SkipPhaseAction` / `ShouldAutoSkip`。纯代码搬移，
**不改逻辑、不改签名、不改导出命名**（§4）。

**边界为什么划在这里**：那一整段是**无状态判定与配置读取**，与 run.go 剩余
部分（Run / runLoop / handleEvent 事件驱动主循环）的耦合只有函数调用，
没有共享局部状态。反过来**不能**把 `handleEvent` 一起搬走——它是单个约
1240 行的函数，搬移它等于把主循环挪走而 run.go 只剩壳。

**零回归证明**：`go build ./...` + 全量 `go test ./...` 通过
（纯搬移的正确性由编译器 + 既有测试保证，与 §136 用「构建产物 CSS 字节一致」
证明 CSS 拆分零回归是同一思路）。

**拆分让一条 lint 真的咬了一口**：`TestWiring_U6_L2` 硬编码
`os.ReadFile("run.go")` 找 `SkipPhaseAction`，搬移后立即 `t.Fatal「lint 失效」`。
这次是好事（lint 咬到了真实变化），但**把「函数住在哪个文件」当成契约是脆的**：
下一次拆分又会报假失效，而修的人很可能直接换文件名甚至删掉断言。
已改为按符号在**本包全文件**搜索，让 lint 只关心真正的不变量。

---

## 验收

| 项 | 结果 |
|---|---|
| `go build -o LsmAgentGame main.go` | ✅ |
| `go vet ./...` | ✅ |
| `go test ./...` | ✅ 全 PASS |
| `npx tsc -b --noEmit` | ✅ |
| `npm run build` | ✅ 209.12 kB CSS / 1880.56 kB JS |
| `bash scripts/check_line_limit.sh` | ✅（4 个 baseline 债务，均未恶化） |
| 5 个孤儿面板 import 数 | 0 → **全部 ≥1** |

**新增 27 项单测**，全部经**双向验证**（§20260812-04 U6 纪律：
先在「还原缺陷」状态确认 FAIL，再恢复确认 PASS）：

| 测试文件 | 数量 | 双向验证的缺陷 |
|---|---|---|
| `difficulty_speak_test.go` | 9 | D08 抓到 typed-nil panic（真实缺陷，测试先发现） |
| `judge_llm_slot_test.go` | 7 | J03 注入「删掉 Release」→ 槽位泄漏被抓 |
| `llm_semaphore_test.go` | 4 | S04 锚定三方共享同一 chan |
| `recall_review_bridge_test.go` | 8 | R05 注入「全表快照」→ HitCount 2→3 被抓 |
| `wiring_lint_test.go`（mustWire 新项） | — | 注释掉注入 → FAIL；恢复 → PASS |

---

## 遗留与已知缺口

1. **复盘的 Agent 互动维度恒为 0** — 引擎不按 userID 累计质询次数。
   待 §4.1「定向质询机制」落地后在 `recall_review_bridge.go:3d` 处接上。
2. **4 个文件仍超 §4 上限**（`agent.go` 2111 / `room_agent.go` 2105 /
   `agent_runner.go` 1982 / `room.go` 1896）。已进 CI baseline 棘轮（只能变短），
   留待独立重构提交——纯搬移的大 diff 不应与功能改动混在一起。
3. **`package-lock.json` 未入库** — CI 暂用 `npm install`，依赖版本不完全可复现。
4. **gofmt 漂移** — 仓库有 55 个文件存在 import 排序漂移（早于本批次）。
   本次未纳入 CI 检查，也未顺手 `gofmt -w`（§132 教训 3：
   全树 gofmt 会污染 120+ 无关文件，掩盖真实语义改动）。

---

## 相关文档

- 待实施项：[`狼人杀13人局-Agent多模型建议合并-待实施项-20260813.md`](狼人杀13人局-Agent多模型建议合并-待实施项-20260813.md)
- 上一批次：[`胜率趋势道具分析阶段动画夜间血迹.md`](胜率趋势道具分析阶段动画夜间血迹.md)
- 个人复盘初版（§20260812-01 U1）：[`复盘增强MindMirror情绪传染信任轨迹.md`](复盘增强MindMirror情绪传染信任轨迹.md)
- 暗线信件 / 阵营赌注（§20260812-03 U2/U3）：[`胜率热力图暗线信件阵营赌注3条理由.md`](胜率热力图暗线信件阵营赌注3条理由.md)
- 难度分级（§20260811-09 U2）：[`../Agent交互体验与解说/AI实时解说与Agent难度分级.md`](../Agent交互体验与解说/AI实时解说与Agent难度分级.md)
