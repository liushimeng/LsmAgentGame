# 狼人杀 13 人局 Agent 升级 §20260811-06

> **批次**：§20260811-06
> **来源**：[`Agent-Surpport-01.md`](../../Agent-Surpport-01.md)（合并版第三方 LLM 建议）§12 优先级总表
> **本批次落地 5 项**：U1 Knight 决斗 ConfirmModal UI 升级 / U2 EmotionAvatar 密集布局碰撞修复 /
> U3 Agent 推理链可视化（`reasoning_chain`）/ U4 Agent 行为一致性校验 /
> U5 黎明流言系统
> **定位**：性价比最高、低风险、纯增量；不破坏既有接线、不增加新 phase、不触动 §134/§135/§119 核心约束。
>
> **审计依据**：CLAUDE.md §13（8 条职责线）+ §92a（锁内变体）+ §97（五处同步）+
> §119（协议层隔离）+ §128（对话即思考）+ §130（接线验证）+ §134（卡池完整实现）+
> §135（身份公开单点）+ §197（长预算）+ §24（AgentClassName）。

---

## U1 — Knight 决斗 ConfirmModal UI 升级（P1-E / Mimo T4）

### 1.1 问题
`ClientWeb/src/components/werewolf/DayControlPanel.tsx:355-393` 的骑士决斗面板
**没有任何二次确认**：
- 用户选中目标 → 点击"⚔️ 发动决斗"按钮 → **直接触发** `onDuel(target)` 走 WS `knight_duel`。
- §198 规则：发动后**立即翻牌公开身份 + 命中狼 → 对方出局 / 否则 → 骑士自己出局**。
- 这是不可逆操作，但前端完全没有"是否确认"环节，容易误点。
- §27 教训：所有不可逆动作（退出房间/认输/确认删除等）必须经 `ConfirmModal`。

### 1.2 设计
- 引入既有 `ClientWeb/src/components/ui/ConfirmModal.tsx`（已挂 portal，CDP 友好）。
- `DayControlPanel.tsx` 加 `useState<{pending: boolean; target: number | null}>` —
  当用户点击"⚔️ 发动决斗"按钮，**先 setState({pending:true, target})**，
  渲染 `<ConfirmModal danger={true} ...>`，
  用户在 modal 里点"确认"才真正调 `onDuel(target)`；点"取消"关闭 modal 不动。
- "放弃本轮"按钮不需确认（无害）。
- 风险提示文案走 i18n：`werewolf.knight.confirmDialog`（zh-CN/en/ja 三语 + types）。

### 1.3 实施面
| 文件 | 改动 |
|---|---|
| `ClientWeb/src/components/werewolf/DayControlPanel.tsx` | 加 `confirmDuelTarget` state + 渲染 `ConfirmModal` + 把"发动"按钮的 onClick 从 `target !== null && onDuel(target)` 改为 `target !== null && setConfirmDuelTarget(target)` |
| `ClientWeb/src/i18n/locales/zh-CN.ts` | 新增 `werewolf.knight.confirmDialog` |
| `ClientWeb/src/i18n/locales/en.ts` | 同上 |
| `ClientWeb/src/i18n/locales/ja.ts` | 同上 |
| `ClientWeb/src/i18n/types.ts` | 加 `'werewolf.knight.confirmDialog': string` |

预计 ~30 行改动，纯前端。

---

## U2 — EmotionAvatar 密集布局碰撞修复（P1-F / Mimo T5）

### 2.1 问题
`EmotionAvatar.tsx` 的 hover popover 在 13 人局（2 列网格 + 4 行堆叠）下：
- popover 默认绝对定位 `bottom: 100%`（向上展开），
  当头像处于**最上两行**时，popover 被裁剪到 viewport 外，玩家看不到。
- popover 宽度 ~280px，**横跨相邻座位** —— 当鼠标 hover 5 号头像，popover 同时盖在 5/6 号上，
  想看 6 号 hover 内容会被遮挡。

### 2.2 设计
- **垂直方向**：popover 默认朝下展开（`top: 100%`），但用 JS 测 `getBoundingClientRect()`，
  当 `top + popoverHeight > viewportHeight` 时自动翻转到朝上展开（`bottom: 100%`）。
- **水平方向**：popover 居中 `left: 50% / translateX(-50%)`；
  当 `left - popoverWidth/2 < 0` 时贴左（`left: 0`）；
  当 `right + popoverWidth/2 > viewportWidth` 时贴右（`right: 0`）。
- 不引入新 CSS 框架，纯 inline style + JS 测量（~40 行）。
- 保持现有 `data-open` 属性驱动的 hover 显示逻辑不变（鼠标从头像移到 popover 时不闪断）。

### 2.3 实施面
| 文件 | 改动 |
|---|---|
| `ClientWeb/src/components/werewolf/EmotionAvatar.tsx` | 新增 `popoverPlacement` state + useEffect 测量 + inline style `top/left/bottom/right/transform` |
| `ClientWeb/src/styles/werewolf-emotion.css` | popover 容器增加 `max-width: 280px; pointer-events: auto; z-index: 5` |

预计 ~50 行改动，纯前端。

---

## U3 — Agent 推理链可视化 `reasoning_chain`（P5-E / Mimo §2.1）

### 3.1 问题
- 当前 Agent 的 `LastDecisionSummary` 字段（§128 对话即思考）只记录**最终决策**，不展示中间推理步骤。
- 玩家希望看到"为什么 Agent 投 X 而不是 Y"，但 LLM 不会自动暴露推理。
- 现有 `speak_with_thought` 的 `internal_thought`（§119 协议层隔离）**只有本人 + 观战者可见**，玩家看不到。
- 需要一个**公开可辩论**的推理结构，区别于 `internal_thought`。

### 3.2 设计
- **新 Agent 工具**：`reasoning_chain(thoughts: [{step, evidence, conclusion, confidence}])`
  - **仅 speak / vote / night_action 三个阶段可调**（其余阶段挂载即返回 `tool not available`）。
  - 计入 `speakLimiter`（复用现有令牌桶）。
  - LLM 调用后**只写入 `BotTranscript.ReasoningChains []ReasoningChainEntry`**
    （新字段，FIFO 上限 10 条），不写 chat_message / chat_history（§119 隔离原则延伸）。
  - 与 `speak` / `speak_with_thought` 不可同响应（防越权）。
- **数据结构**（`ServerGo/agent/wwplayer/wwtypes.ReasoningChainEntry`）：
  ```go
  type ReasoningChainEntry struct {
      Round     int      `json:"round"`
      Phase     string   `json:"phase"`
      Topic     string   `json:"topic"`        // 推理主题,如"投票给 5 号"
      Steps     []string `json:"steps"`        // 1-3 步推理,每步 ≤30 字
      Evidence  []string `json:"evidence"`     // 1-3 条证据,每条 ≤30 字
      Conclusion string  `json:"conclusion"`   // 最终结论,≤40 字
      Confidence int     `json:"confidence"`   // 0-100
      CreatedAt int64    `json:"created_at"`
  }
  ```
- **前端**：HistoryDrawer 第 7 sub-tab「🧩 推理链」渲染（**仅 spectator 可见**，玩家分支 `sanitizeBotTranscript` 清空）。
- **协议**：服务端 `view.go` 透传 `BotTranscript.ReasoningChains []ReasoningChainEntry` 给 spectator；
  玩家分支 §135 隔离保持不变。

### 3.3 实施面
| 文件 | 改动 |
|---|---|
| `ServerGo/agent/wwplayer/wwtypes/types.go` 或 `game/werewolf/wwtypes/` | 新增 `ReasoningChainEntry` struct |
| `ServerGo/agent/wwplayer/tools.go` | 新增 `reasoning_chain` 工具定义（输入 schema + 描述）|
| `ServerGo/agent/wwplayer/tools_registry.go` | 挂载到 speak/vote/night_action 三阶段 |
| `ServerGo/game/werewolf/agent_runner.go` | 处理 `reasoning_chain` tool_use → 写 `BotTranscript.ReasoningChains` |
| `ServerGo/game/werewolf/view.go` | `BotTranscriptJSON.ReasoningChains` 字段 + 玩家分支 `sanitizeBotTranscript` 清空 |
| `ClientWeb/src/components/werewolf/HistoryDrawer.tsx` | 加第 7 sub-tab「🧩 推理链」+ 卡片渲染 |
| `ClientWeb/src/i18n/locales/{zh-CN,en,ja}.ts` + `types.ts` | 加 `werewolf.history.reasoning.title/empty/item` 4 键 |

预计 ~250 行改动，后端 ~180 + 前端 ~70。

---

## U4 — Agent 行为一致性校验（P5-D / DouBao §五.2）

### 4.1 问题
- Agent 可能在不同轮次对同一玩家**身份声明自相矛盾**：
  例：第 1 轮认自己是"平民" → 第 2 轮跳"预言家"。
- 现有 `speak_factcheck.go` 只校验**已死亡玩家引用**（§79 教训 R79），
  不校验身份声明一致性。
- 玩家报告："Agent 跳神后没说金水，被质疑就改口"，影响游戏公平性。

### 4.2 设计
- **轻量校验模块**：`ServerGo/agent/wwplayer/consistency_check.go`（≤200 行）
- **数据源**：每 bot 维护 `RoleClaims map[int]string`（key=round，value=声明的身份），
  从 `BotTranscript.SpeakHistory` 末尾追加扫描。
- **检测规则**（3 类）：
  | # | 规则 | 严重度 | 触发 |
  |---|---|---|---|
  | R1 | **身份反复跳变**：同 bot 同 round 内出现 ≥2 次不同身份声明 | high | 触发：跳过本轮（不计入 consecutiveFailures，§120 公平性），记 warn 日志 |
  | R2 | **认平民后又跳神**：本 bot 在更早 round 声明 X → 本 round 声明 Y（神职）| medium | 触发：把检测结果写入 `BotTranscript.LastConsistencyCheck`，prompt 追加 ⚠️ 块 |
  | R3 | **投票自相矛盾**：本 bot 在同一 phase 内对同一目标改投 ≥2 次 | low | 仅记日志，不触发动作 |
- **执行时机**：每个 Agent speak 工具调用**返回前**（在 `recordTranscript` 之后），
  扫描最近 5 轮发言，跑 3 类规则。
- **§120 公平性**：一致性校验**不**走 LLM 调用（纯规则），**不**计入 `consecutiveFailures`，
  误判也不影响 quarantine。
- **§128 对话即思考**：校验结果写 `LastConsistencyCheck`（新增字段），不新建独立决策字段。

### 4.3 实施面
| 文件 | 改动 |
|---|---|
| `ServerGo/agent/wwplayer/consistency_check.go`（新） | `ConsistencyChecker` struct + 3 类规则 + `RunCheckLocked` |
| `ServerGo/agent/wwplayer/wwtypes/types.go` | `BotTranscript` 加 `LastConsistencyCheck *ConsistencyCheckResult` |
| `ServerGo/agent/wwplayer/agent_runner.go` | speak 工具返回前调 `RunCheckLocked` |
| `ServerGo/agent/wwplayer/prompt.go` | `BuildUserPrompt` 末尾追加 �️ 块（仅当 `LastConsistencyCheck` 有 warning 时）|
| `ServerGo/agent/wwplayer/consistency_check_test.go`（新） | 3 类规则单元测试 |

预计 ~250 行改动，纯后端，无 LLM 调用增量。

---

## U5 — 黎明流言系统（P4-C / DouBao §三.2）

### 5.1 问题
- 当前游戏信息噪声小，玩家容易"背公式"（永远信预言家 → 永远信警徽流）。
- §119 协议层隔离让 Agent 推理更精密，但**信息源丰富度**没跟上：
  玩家公屏信息只来自发言 + 公开活动流 + 警徽流。
- 需要人为引入"系统流言"作为**公共信息噪声**，考验 Agent 甄别能力。

### 5.2 设计
- **触发时机**：每天 `PhaseDawn` 切换到白天时，**额外广播 1-2 条系统流言**。
- **流言种类**（5 类模板，§135 不揭身份）：
  | 模板 | 文案 |
  |---|---|
  | `rumor_village_idle` | "📰 今晨村口空无一人,守卫昨夜未出门。" |
  | `rumor_witch_used` | "📰 昨夜药瓶发出响声,有人用过药。" |
  | `rumor_no_kill` | "📰 今晨平安无事,昨晚平安夜。" |
  | `rumor_mystic_kill` | "📰 村东头出现奇异光芒,有人施展了神秘力量。" |
  | `rumor_hunter_alive` | "� 5号 今日神色慌张,像有武器在身。"（**§135 注**：仅说"神色"，不揭猎人身份）|
- **真假机制**：5 类模板里 `rumor_village_idle`/`rumor_witch_used` 100% 真（基于当晚真实事件），
  `rumor_no_kill`/`rumor_mystic_kill` 60% 真，`rumor_hunter_alive` 40% 真。
  - 真假判定基于当晚 `gs.AliveSeat / WitchActedTonight / WolfKillTarget / MysticEvents` 等既有权威字段。
- **公开广播**：新增 `ActivityEventKindRumor = "rumor"`，走既有 `emitActivity` 链路。
  `severity="info"`,`emoji="📰"`,`silentForBots=true`（Agent 不直接收到，但能从 `gc.LastRumor` 拿到，
  §119 延伸：rumor 本身是公开信息）。
- **Agent 注入**：每个 bot `GameContext.LastRumor` 字段（最新 3 条 FIFO），
  prompt 末尾追加「📰 今日流言」块，让 Agent 决定信/不信。
- **配置**：`werewolf.rumor_enabled`(默认 true) + `werewolf.rumor_count_per_day`(默认 2)。

### 5.3 实施面
| 文件 | 改动 |
|---|---|
| `ServerGo/game/werewolf/rumor_system.go`（新） | 5 类模板 + 真假生成器 + `GenerateRumorLocked` + `EmitDayRumorsLocked` |
| `ServerGo/game/werewolf/activity_emitter.go` | 新增 `ActivityEventKindRumor = "rumor"` |
| `ServerGo/game/werewolf/engine_day.go` | `startDay` / `resumeAfterHunterShoot` 末尾调 `EmitDayRumorsLocked` |
| `ServerGo/game/werewolf/wwtypes/` | `GameState.LastRumors []RumorEntry`（3 条 FIFO）+ `WerewolfRoom.RumorsEnabled bool` |
| `ServerGo/game/werewolf/agent_runner.go` | `buildAgentContextLocked` 末尾追加 `RumorPromptBlock` |
| `ServerGo/agent/wwplayer/prompt.go` | `BuildUserPrompt` 末尾追加流言块 |
| `ServerGo/config/LsmAgentGame.conf.example` | 新增 `werewolf.rumor_enabled=true` + `rumor_count_per_day=2` |
| `ClientWeb/src/components/werewolf/GameChatPanel.tsx` | rumor 活动流图标 📰（已在通用活动流渲染内） |
| `ClientWeb/src/i18n/locales/{zh-CN,en,ja}.ts` + `types.ts` | 加 `werewolf.rumor.{villager_idle,witch_used,no_kill,mystic_kill,hunter_alive,label}` 6 键 |

预计 ~300 行改动，后端 ~220 + 前端 ~30 + 配置 + i18n。

---

## §13 全局约束自查清单

| 约束 | U1 | U2 | U3 | U4 | U5 |
|---|---|---|---|---|---|
| §92a 锁内变体 | N/A (前端) | N/A | ✓ 工具派发改 `*Locked` | ✓ `RunCheckLocked` | ✓ `GenerateRumorLocked` |
| §97 五处同步 | N/A | N/A | N/A (不加 phase) | N/A | N/A (不发新 phase) |
| §119 协议层隔离 | N/A | N/A | ✓ ReasoningChains 仅 spectator 可见 | ✓ 不写 chat 表 | ✓ rumor 走公开 Activity |
| §128 对话即思考 | N/A | N/A | ✓ ReasoningChains 写 BotTranscript | ✓ 写 LastConsistencyCheck | ✓ 写 LastRumors |
| §130 接线验证 | N/A | N/A | ✓ tools → runner → view 链 | ✓ runner → prompt 链 | ✓ engine_day → emit → context → prompt 链 |
| §134 完整实现 | N/A (无新角色) | N/A | N/A | N/A | N/A |
| §135 身份公开 | N/A | N/A | ✓ 玩家分支 sanitize | N/A | ✓ rumor_hunter_alive 仅说"神色",不揭身份 |
| §197 长预算 | N/A | N/A | ✓ reasoning_chain 计入 speakLimiter,不计 consecutiveFailures | ✓ 不走 LLM | N/A |
| §24 AgentClassName | N/A | N/A | N/A (复用 Player) | N/A | N/A |
| §121 数据形状 | ✓ i18n 类型同步 | ✓ i18n 类型同步 | ✓ i18n 类型同步 | N/A | ✓ i18n 类型同步 |

---

## §14 相关文档索引

- 综合现状：[`../狼人杀/00-游戏信息与Agent现状综合文档.md`](../狼人杀/00-游戏信息与Agent现状综合文档.md)
- 骑士角色：[`../狼人杀-角色设计/狼人杀守卫角色设计.md`](../狼人杀-角色设计/狼人杀守卫角色设计.md)（同 §134 §198 链）
- DayNightOverlay：[`../../ClientWeb/src/components/werewolf/DayNightOverlay.tsx`](../../ClientWeb/src/components/werewolf/DayNightOverlay.tsx)
- 对话即思考：[`./狼人杀对话即思考设计.md`](./狼人杀对话即思考设计.md)
- 协议层隔离：[`./狼人杀Agent设计.md`](./狼人杀Agent设计.md)
- 反死亡信息幻觉（§79）：[`../../ServerGo/agent/wwplayer/speak_factcheck.go`](../../ServerGo/agent/wwplayer/speak_factcheck.go)
- 知识库来源：[`../../Agent-Surpport-01.md`](../../Agent-Surpport-01.md)

---

## §15 验收清单

- [ ] `go build ./...` 通过
- [ ] `go test ./game/werewolf/... ./agent/... -count=1` 通过（含新增 consistency_check_test）
- [ ] `tsc --noEmit` 通过
- [ ] `npm run build` 通过
- [ ] 5 个新功能可在房间内端到端跑通：
  - [ ] 选骑士 → 选目标 → 点发动 → ConfirmModal 弹出 → 确认 → 真实决斗
  - [ ] hover 13 人局最上排头像 → popover 向下展开不被裁剪
  - [ ] spectator 在结算页 HistoryDrawer 第 7 sub-tab 看到 bot 的 reasoning_chain
  - [ ] 制造 Agent 身份反复跳变 → BotTranscript.LastConsistencyCheck 有 warning + prompt 末尾 ⚠️ 块
  - [ ] 第 1 天黎明阶段 → 聊天面板出现 1-2 条 📰 流言

---

> **文档维护说明**：本文档对应 §20260811-06 批次落地 5 项。
> 任何修改同步更新 [§14 相关文档索引] + [§15 验收清单]。
