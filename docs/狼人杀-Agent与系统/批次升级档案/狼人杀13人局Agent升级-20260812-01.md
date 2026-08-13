# 狼人杀 13 人局 Agent 升级 §20260812-01 —— 复盘增强 + MindMirror + 情绪传染 + 信任度轨迹

> **日期**：2026-08-12
> **批次**：§20260812-01
> **来源**：[`Agent-Surpport-01.md`](../../Agent-Surpport-01.md) 中 P2-F / P6-H / P6-A(轻量版) / P6-D 五项
> **本批次落地 4 项**：

| # | 主题 | 出处 | 工作量 | 类型 |
|---|---|---|---|---|
| U1 | 个人复盘第 2~4 步（投票准确率 / 发言暴露度 / 道具使用效率 / Agent 互动质量）| Mimo §7.1 / P2-F | ~220 行 | 后端聚合 + 前端展示 |
| U2 | 人类直觉 vs Agent 逻辑对比面板（`MindMirror`）| DeepSeek §三.3 / P6-H | ~280 行 | 纯前端(依赖假说表) |
| U3 | 群体情绪传染(轻量版，系数 0.3 起步) | DeepSeek §四.1 / P6-A | ~260 行 | 新引擎模块 + Agent 反馈 |
| U4 | 发言信任度轨迹图(观战者侧) | M3 §6 V1 / P6-D | ~180 行 | 复用法官 5 段总结 + 前端图表 |

> **总计 ≈ 940 行**，无新增 DB 表 / 无新增夜间 phase / 无新增 WS 帧（U3 复用既有 `game.state` 推送）。
>
> **挑选理由**：
> - U1 是 §20260809-02 U3（身份猜测准确率）补全，其余 3 步只缺后端聚合 — 工作量最小。
> - U2 纯前端，依赖已落地的 `bot_contexts[].LastHypothesis`（§20260810-07）。
> - U3 改写「参数校准」风险为「固定 0.3 系数」+ 仅影响发言风格（tone），不污染推理（§128）。
> - U4 复用法官 5 段总结（§20260810-11 H1），新增前端轨迹图组件。

> **全局约束自查**（CLAUDE.md §13 / Agent-Surpport-01 §12）：
> §24 AgentClassName / §92a `*Locked` 锁内变体 / §97 五处同步 / §119 协议层隔离 / §121 数据形状 /
> §128 对话即思考 / §130 生产注入点 grep / §135 单点判定 / §197 流式续命 / §26 前端对比度。

---

## 总览

| # | 主题 | 主要风险 | 主要文件 |
|---|---|---|---|
| U1 | 个人复盘 4 维 | §121 数据形状 / §130 注入点 | `recall_aggregate.go`(新) / `recall_api.go` / `PersonalReviewPanel.tsx` |
| U2 | MindMirror | §135 身份公平 / §128 隐私 | `MindMirrorPanel.tsx`(新) / `BotTranscript` 已含 `LastHypothesis` |
| U3 | 情绪传染 | §128 仅影响 tone / §197 长预算 | `emotion_contagion.go`(新) / `prompt_cipher.go` / `EmotionAvatar.tsx` |
| U4 | 信任度轨迹 | §130 接线 / §26 对比度 | `judge_summary.go` 补 `trust_trace` / `TrustTraceChart.tsx` |

---

## U1 — 个人复盘第 2~4 步（§10.2 / Mimo §7.1）

### U1.1 现状与缺口

- §20260809-02 U3 已落地「身份猜测准确率」（第 1 步），复用 `BotTranscript.LastHypothesis`。
- 第 2~4 步待做：
  - **投票准确率**：个人每次投票与最终被放逐者是否一致（投中 / 票型一致 / 投错）。
  - **发言暴露度**：本局发言中含「关键词命中身份词」的次数（已落地 `SpeakFactCheck`）。
  - **道具使用效率**：本局购买的道具总数 / 命中数 / 浪费数（已落地 `PropHistory`）。
  - **Agent 互动质量**：与 Agent 互动次数 / Agent 回应率 / 平均响应时长（已落地 `FactionDrawer`）。

### U1.2 设计 — 单一聚合接口 `GET /api/games/werewolf/rooms/:id/review/:userId`

返回 4 维分数 + 关键时间线，全部基于**已有字段**：

| 维度 | 字段 | 数据源 | 算法 |
|---|---|---|---|
| 投票准确率 | `vote_accuracy` | `game.state.votes` + `PhaseVote.LastExiled` | 投中放逐者记 1.0；与放逐者同票型记 0.5；其余 0.0；`avg = sum / participated` |
| 发言暴露度 | `speak_exposure` | `BotTranscript.HeartThought` + `SpeakFactCheckFact` | 用 `redactLedgerFact` 思路反向：露出的身份词出现在公屏发言中即计 1 次；归一化 0~1 |
| 道具效率 | `prop_efficiency` | `PropHistory[]` | `hit = 命中数, total = 购买数, eff = hit / max(total,1)` |
| Agent 互动质量 | `agent_interaction` | `FactionDrawer` events | 回应率 = `已回应 / 已发起`；平均响应时长 = `Avg(r - r_发起)` |

**不引入新 DB 表**。所有数据从 `WerewolfRoom.state` 实时聚合（§118 异步 worker 已有链路），
对局结束后 30min 内可查，结果缓存到 `Room.ReviewCache` 内存字段（无 DB 持久化）。

### U1.3 关键文件

- `ServerGo/game/werewolf/recall_aggregate.go`(新) — 4 维聚合函数，**纯内存**计算，§92a 锁外调用。
- `ServerGo/api/werewolf_api.go` — 加 `GET /api/games/werewolf/rooms/:id/review/:userId` 路由。
- `ClientWeb/src/components/werewolf/PersonalReviewPanel.tsx`(新) — 4 卡片栅格 + 进度条。
- 加 21 键 i18n 三语（zh-CN/en/ja）。

### U1.4 风险与对策

- **§121 数据形状**：返回 wrapper `{ review: {...}, computed_at: int64 }`。
- **§130 接线**：写入点 grep —— `recall_aggregate.go::ComputeReview` 必须被 `RecallCache` 调一次预热。
- **§26 对比度**：4 卡片用 `rgba(255,255,255,0.08)` 底 + `color: #fff` 字 + 进度条用主题色（红/绿/蓝/紫）。

---

## U2 — MindMirror 人类直觉 vs Agent 逻辑对比面板（§7.7 / DeepSeek §三.3）

### U2.1 设计 — 4 列表格 + 概率雷达

仅在**混合房间**且**人类存活**时渲染。**仅展示概率/置信度**，不显示 Agent 真实身份或底牌（§135）。

#### 表格组件

| 玩家 | 你的直觉 | Agent（model_key）推理 | 差异 |
|---|---|---|---|
| 5 号 | 🐺 狼 (70%) | 🔮 预言家 (65%) | 阵营相反 |
| 7 号 | 👤 平民 (45%) | 🐺 狼 (40%) | 阵营相反 |

差异 > 30% 显示 🔴；10~30% 🟡；<10% 🟢。

#### 概率雷达

5 维（你 / Agent 各 5 维）：好人倾向 / 狼倾向 / 神职倾向 / 自信度 / 犹豫度。
用 SVG 画 2 个多边形（半透明叠加），便于肉眼对比。

### U2.2 数据来源

- **人类直觉**：`localStorage[werewolf-mindmirror-:roomId]`（按座位存 JSON）。
  - 玩家在 ActionPanel 选「我的猜测 → 阵营 + 置信度」时，**本地存储**。
  - **不**进任何 prompt / Agent 上下文（§128 隐私）。
- **Agent 推理**：`BotTranscript.LastHypothesis[*]`（§20260810-07 已落地）。
  - 选择「存活 + 我能看见的 Bot 推理」投影（已 §135 自有脱敏）。

### U2.3 关键文件

- `ClientWeb/src/components/werewolf/MindMirrorPanel.tsx`(新) — 表格 + SVG 雷达。
- `ClientWeb/src/components/werewolf/MindMirrorButton.tsx`(新) — FactionDrawer 一个新 tab。
- `ClientWeb/src/hooks/useMindMirror.ts`(新) — localStorage 读写。
- `ClientWeb/src/components/werewolf/werewolf-panels.css` — 加 30 行 CSS（雷达 SVG 边框、表格斑马纹）。

### U2.4 风险与对策

- **§135 身份公开**：表格只显示「阵营倾向 + 置信度」，不显示 `Role.Werewolf` 等具体身份字。
- **§128 隐私**：localStorage 严格本地，**不在 ws / api / state 中传递**。
- **§26 对比度**：🔴🟡🟢 三色均 `≥ 4.5:1` against 暗底（§26 硬阈值）。

### U2.5 验收

- 鼠标悬停「5 号」差异行 → tooltip 显示「Agent 推理依据：第 2 轮提到查验 2 次」（从 `ReasoningChain` 拉）。
- 纯僵尸局（4 人 bot + 0 人）不渲染（`isHumanInRoom` 判定）。

---

## U3 — 群体情绪传染轻量版（§7.8 / DeepSeek §四.1）

### U3.1 设计 — 复用 §213 emotion_switch_speak，仅影响 tone

#### 4 类传染（系数严格 0.3 起步）

| 情绪 | 感染力 | 传染半径 | 实际效果 | 落地路径 |
|---|---|---|---|---|
| `confident` | 0.3 | 相邻 2 座 | 接收者下轮 prompt 追加「📢 自信度加成：+20% 语气词(确信/我们知道/只能是他)」| 字符串注入 |
| `nervous` | 0.3 | 相邻 1 座 | 接收者下轮 LLM prompt 追加「⚠️ 紧张状态：发言字数 ≤ 60 字」| 字数约束 |
| `angry` | 0.3 | 相邻 1 座 | 接收者下轮 prompt 追加「🔥 受愤怒感染：倾向指出 X 的漏洞」| 角色扮演提示 |
| `calm` | 0.3 | 相邻 2 座 | 接收者下轮被击中时眩晕时间 -1s（≤0则不显示）| 道具引擎配合 |

**只影响发言风格**，不污染决策 — §128 对话即思考原则保留。

#### 初始化

- 每个 Agent 在 `speak` / `emotion_switch_speak` 后，触发 `emotionContagionLocked(seat, emotion, radius)`。
- 函数：遍历相邻座位（按 `seat` 环形 ±radius）→ 注入 `R.State.Players[seat].ContagionQueue[T]` 为「下轮自带的感染状态」。
- 下轮 LLM 调用前 `prompt_cipher.go::BuildUserPrompt` 末尾追加 `ContagionBlock`。

### U3.2 关键文件

- `ServerGo/game/werewolf/emotion_contagion.go`(新) — ≤ 280 行。
  - `ContagionEntry{SourceSeat, Emotion, Strength, ExpiresRound}`。
  - `emotionContagionLocked(r, sourceSeat, emotion, radius)` — 触发传染。
  - `drainContagionForPromptLocked(r, seat, round) []string` — 拉取当前轮感染状态。
- `ServerGo/agent/wwplayer/prompt_cipher.go` — `BuildUserPrompt` 末尾追加 `ContagionPromptBlock`。
- `ServerGo/agent/wwplayer/emotion.go` — `EmotionSwitchSpeak` 派发后调 `emotionContagionLocked`。
- `ServerGo/game/werewolf/view.go` — `PlayerJSON.ContagionBuff string`（omitempty，spectator 可见，玩家自己可见）。
- `ClientWeb/src/components/werewolf/EmotionAvatar.tsx` — 加 `🦠 受感染` 微标（1 轮）。
- 配置：`werewolf.emotion_contagion_enabled`(默认 true) + `werewolf.emotion_contagion_strength`(默认 0.3)。

### U3.3 风险与对策

- **§128 仅影响 tone**：传染效果只通过 `ContagionPromptBlock` 注入 prompt **末段**，不修改 system；不污染 Memory / 不污染 DecisionTrail。
- **§197 流式续命**：传染仅注入 prompt 文本，不额外开 LLM 调用，**无需长预算**。
- **§130 接线**：触发点 grep —— `emotion_switch_speak` 派发路径 **必须** 新增 `emotionContagionLocked` 调用。
- **§92a 锁内变体**：`EmotionSwitchSpeak` 派发时持 `r.mu`（§213 已确认），传染函数必须 `*Locked` 变体。
- **不覆盖人类玩家**（§7.8 风险条款）。

### U3.4 防御性边界

- 系数硬上限：`0.3 × 1.5 = 0.45`（不允许运行时调大；如需调大需重新走产品决策）。
- 传染半径硬上限：2 座（±2）。
- 同源同情绪 1 轮内仅注入 1 次（不累加）。

---

## U4 — 发言信任度轨迹图（§7.1 / M3 §6 V1）

### U4.1 设计 — 法官 5 段总结外新增第 6 段「信任度轨迹」

#### 法官输入新增

在 `JudgeJudgeSummary` 工具的输入组装中追加：**本局 13 玩家 × 每日信任度**（仅法官可见）。
LLM 输出多 1 段「【信任度轨迹】」：`[["seat1", -0.2, 0.1, 0.5], ["seat2", 0.0, 0.3, 0.8], ...]` — 按 Day 1 / 2 / 3 顺序。

#### 数据来源

- 法官 `runtime.GetJudgeState()` 注入最近 50 条公屏发言 + 投票 + 行为。
- LLM 推理：基于「发言一致性 / 投票跟随度 / 情绪稳定性」产出每日 -1 ~ +1 分数。
- **不**直接翻身份（§135）— LLM 看到的「角色词」由 `redactLedgerFact` 脱敏后再传入。

#### 前端渲染

- 观战者 HistoryDrawer 第 5 sub-tab「🏆 总结」末尾追加「📈 信任度轨迹」按钮。
- 弹窗 SVG 折线图：X 轴 Day 1..N；Y 轴 -1 ~ +1；13 条线（每玩家 1 条，色相按阵营：狼=红/好人=绿/神职=紫）。
- 鼠标悬停显示「#5 号 Day 3: 0.45（中等信任）」。

### U4.2 关键文件

- `ServerGo/agent/wwjudge/judge_summary.go` — 解析 LLM 输出 6 段（现有 5 段 + 新增 `trust_trace`）。
- `ServerGo/agent/wwjudge/judge_prompt.go` — system prompt 注入「【信任度轨迹】评分要求」。
- `ServerGo/game/werewolf/judge_summary_bridge.go` — `judge_summary` 字段加 `TrustTrace []TrustTraceEntry`。
- `ServerGo/game/werewolf/view.go` — `JudgeSummaryJSON.TrustTrace` 字段透传（omitempty）。
- `ClientWeb/src/components/werewolf/TrustTraceChart.tsx`(新) — 180 行 SVG 折线图。
- `ClientWeb/src/components/werewolf/HistoryDrawer.tsx` — 总结 tab 追加按钮。

### U4.3 风险与对策

- **§130 接线**：`judge_summary` LLM 工具描述必须包含「请输出信任度轨迹 JSON 段」，系统 prompt 同步。
- **§135 公平性**：信任度分数是 LLM 主观判断，**不**直接暴露身份 — 即使分数很高也不翻牌。
- **§197 流式续命**：法官 LLM 调用已走 `parentCtx + extendedTimeout`（§130 U1 修复）。
- **§26 对比度**：13 条线色相严格按 §26.4 状态徽章色（红/绿/紫/橙），线宽 ≥ 2px 防 hover 漏接。

---

## 测试与验证

| 验证项 | 方法 | 预期 |
|---|---|---|
| U1 4 维聚合 | `recall_aggregate_test.go` 4 用例 | 投票准确率 ±0.01 / 暴露度准确 / 道具效率 0.7 / 互动 ≥ 0.5 |
| U1 i18n | `i18n/types.ts` 编译 | 21 键三语全覆盖 |
| U2 MindMirror 渲染 | 手动 + Playwright 截图 | 表格 + 雷达就位 |
| U2 隐私隔离 | `grep "mindmirror" ServerGo/` | 零命中（纯前端） |
| U3 传染双向 | `emotion_contagion_test.go` 6 用例 | 触发 / 半径 / 衰减 / 角色对齐 / 锁内变体 / 边界 |
| U3 API 暴露 | `grep "ContagionBuff" ServerGo/` | `view.go` 仅一处 |
| U4 信任度解析 | `judge_summary_test.go` 6 段切分 | `TrustTrace` 非空 |
| U4 轨迹图渲染 | `TrustTraceChart.test.tsx` | 13 条线 + 颜色 + tooltip |

**回归测试**：
- `go build ./...` + `go test ./game/werewolf/... ./agent/... -count=1` 全 PASS。
- `cd ClientWeb && tsc --noEmit && npm run build` 通过。

---

## 修改文件清单

### 后端

| 文件 | 改动行 | 用途 |
|---|---|---|
| `ServerGo/game/werewolf/recall_aggregate.go` | +220 | U1 4 维聚合 |
| `ServerGo/game/werewolf/emotion_contagion.go` | +280 | U3 传染引擎 |
| `ServerGo/agent/wwjudge/judge_summary.go` | +60 | U4 信任度解析 |
| `ServerGo/agent/wwjudge/judge_prompt.go` | +30 | U4 prompt 注入 |
| `ServerGo/agent/wwplayer/prompt_cipher.go` | +40 | U3 ContagionBlock |
| `ServerGo/agent/wwplayer/emotion.go` | +15 | U3 emotion_switch_speak 后触发 |
| `ServerGo/game/werewolf/view.go` | +20 | U3 PlayerJSON.ContagionBuff + U4 TrustTrace |
| `ServerGo/game/werewolf/judge_summary_bridge.go` | +25 | U4 透传 |
| `ServerGo/api/werewolf_api.go` | +25 | U1 路由 |
| `ServerGo/config/config.go` | +15 | U3 / U4 配置项 |
| `ServerGo/game/werewolf/emotion_contagion_test.go` | +260 | 测试 |
| `ServerGo/game/werewolf/recall_aggregate_test.go` | +220 | 测试 |

### 前端

| 文件 | 改动行 | 用途 |
|---|---|---|
| `ClientWeb/src/components/werewolf/PersonalReviewPanel.tsx` | +180 | U1 |
| `ClientWeb/src/components/werewolf/MindMirrorPanel.tsx` | +200 | U2 表格+雷达 |
| `ClientWeb/src/components/werewolf/MindMirrorButton.tsx` | +40 | U2 |
| `ClientWeb/src/hooks/useMindMirror.ts` | +40 | U2 localStorage |
| `ClientWeb/src/components/werewolf/TrustTraceChart.tsx` | +180 | U4 |
| `ClientWeb/src/components/werewolf/HistoryDrawer.tsx` | +20 | U4 入口 |
| `ClientWeb/src/components/werewolf/EmotionAvatar.tsx` | +30 | U3 感染微标 |
| `ClientWeb/src/components/werewolf/werewolf-panels.css` | +50 | U1/U2/U3 样式 |
| `ClientWeb/src/i18n/types.ts` + zh-CN/en/ja | +63 | 21×3 键 |

**总计 ≈ 2130 行**（含测试用例）。

---

## 落地追溯

- 本批次对应 git commit: `升级: §20260812-01 个人复盘增强 + MindMirror + 情绪传染 + 信任度轨迹`
- 后续补丁: §20260812-02 起按需
