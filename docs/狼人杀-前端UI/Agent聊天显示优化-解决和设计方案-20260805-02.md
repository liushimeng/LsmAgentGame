# Agent 聊天显示优化 — 解决和设计方案 (20260805-02)

> 本文**修订并取代** `Agent聊天显示优化-解决和设计方案-20260805-01.md` 的 §3.5 与 P0-1 结论。
> 01 号方案已实现的其余部分(P0-2 / P0-3 / P1-1 / P1-2 / P1-3 / P1-4 + 座位卡气泡)全部保留。
> 触发原因:用户评审指出「发言时间线」与「房间聊天」内容重复,属过度设计。

---

## 1. 对 01 号方案的自我批判

### 1.1 错在哪

01 号方案把 `SpeechTimeline` 定性为「P0 级缺陷:死组件」并予以**复活**。

这个判断只做对了一半:
- ✅ **事实判断正确** —— 前端组件已实现并挂载,后端从未下发 `speeches`,组件恒返回 `null`,这确实是 §130「声明了却从不接线」的复现;
- ❌ **价值判断缺失** —— 我发现它「不工作」,就直接去让它「工作」,**从未追问它是否应该存在**。

`SpeechTimeline` 渲染的是「谁 / 什么时间 / 说了什么」的倒序列表。
`房间聊天`(`GameChatPanel`)渲染的也是「谁 / 什么时间 / 说了什么」的列表。
两者数据同源(都来自 `chat.message` 公开发言),信息**完全重复**,且同屏并列 ——
中栏底部一份、右栏一份,用户视线被迫在两个等价列表间来回,是**净负收益**。

### 1.2 教训(应写入 §13 lessons)

> **「死代码」的正确处置有两个方向:接线,或删除。默认选接线是一种思维惰性。**
> 判定顺序必须是:先问「这个功能是否应该存在」,再问「它为什么不工作」。
> 尤其当死组件与一个**成熟且在用**的模块信息同源时,删除几乎总是正解 ——
> 它当初之所以死掉没人察觉,恰恰证明了没有人需要它。

这一条与 §130/§134/§135 的「声明未接线」系列**互补**:那几条讲「半实现是最坏状态」,
本条补充「消灭半实现的手段不止补全,还有砍掉」。

---

## 2. 修订后的设计目标

**唯一目标**:在 `Agent / 玩家` 座位卡上,用一个冒泡控件显示**最后一次发言**。

- `房间聊天` = 完整历史流(时间维度,可回溯);
- `座位卡气泡` = 每人此刻状态(空间维度,一眼扫全场)。

两者**职责正交,不重复**。除此之外不增加任何发言展示模块。

---

## 3. 本次改动(相对当前 HEAD)

### 3.1 删除 `发言时间线` 模块

| # | 动作 | 位置 |
|---|---|---|
| D1 | 删除组件文件 | `ClientWeb/src/components/werewolf/SpeechTimeline.tsx`(69 行) |
| D2 | 删除挂载点与 import | `ClientWeb/src/pages/WerewolfGamePage.tsx:14, 590` |
| D3 | 删除样式 | `ClientWeb/src/styles/werewolf-agent.css` 的 `.ww-timeline*` 区块(532–618) |
| D4 | 删除前端类型 | `types/werewolf.ts`:`speeches` 字段 + `SpeechEventJSON` 接口 |
| D5 | 删除 wire 字段与投影 | `ServerGo/game/werewolf/view.go`:`ClientGameState.Speeches`、`SpeechEventJSON`、`buildSpeechesWireLocked`、`recentSpeechesWireLimit`、`cs.Speeches` 赋值 |

**必须保留**(易误删,重点提示):
- `room.recentSpeeches` 滚动缓冲 —— 它是 **Agent prompt 的 `GameContext.RecentSpeeches` 数据源**
  (`room_agent.go:730`)与**法官整局总结**的输入(`judge_summary_bridge.go:372`)。
  本次删的只是「向前端 wire 的投影」,**不是**缓冲本身。删错会导致 Agent 失去发言上下文。
- `agent.SpeechEvent` 结构体(`agent/prompt.go:946`)—— 同上,LLM 侧仍在用。

### 3.2 补齐:真人玩家座位卡也要有气泡

01 号方案的气泡数据源是 `BotContextJSON.last_speech`,而 `bot_contexts` 由
`populateBotContexts`(`room_state.go:238`)构造,其座位集合是
`seatModelKeys ∪ BotAgents` —— **只含 bot 座位**。

⇒ 当前实现下,**真人玩家的座位卡永远不会有气泡**,与用户「`Agent / 玩家` 游戏角色卡牌」的要求不符。

**修复**:把「最后一次发言」提升为**座位级**属性,挂到 `PlayerJSON` 上(人机统一)。

后端 `view.go` `PlayerJSON` 新增:

```go
// 2026-08-05 §02 — 座位级「最后一次公开发言」,人机统一。
// 数据源 room.lastSpeechBySeat,由 appendRoomMessage 在**公开发言**落库时写入,
// 因此 bot 与真人走同一条路径,无需分别接线(对照 bot_contexts 只覆盖 bot 座位)。
// 私聊(whisper)不写入 —— 私聊原文只对收发双方可见,而本字段全房可见。
LastSpeech   string `json:"last_speech,omitempty"`    // ≤200 rune
LastSpeechAt int64  `json:"last_speech_at,omitempty"` // unix ms
```

`WerewolfRoom` 新增字段(`room.go`):

```go
// lastSpeechBySeat[seat] = 该座位最后一次公开发言(人机统一)。
lastSpeechBySeat map[int]seatSpeech
```

写入点 `room_chat.go` `appendRoomMessage` 公开分支(~line 188,紧邻 `recentSpeeches` 追加处):
座位 `seat >= 0` 且非观战者时记录。**观战者(seat<0)不记录**。

**§92a 锁分析(关键)**:
- `appendRoomMessage` **不持 `r.mu`**(`room.go:727` 明确注释),与 `recentSpeeches` 的写入在同一函数、同一临界区语义下 —— 新字段沿用**完全相同**的并发约定,不新增任何锁,不改变现有锁序;
- 读取点 `BuildClientState` 系列**已持 `r.mu`**,为锁内直读,同样不新增锁。

> 说明:此处沿用 `recentSpeeches` 既有的并发约定(同函数、同字段族、同生命周期),
> 属最小变更;不在本次范围内重新审视该约定本身。

### 3.3 座位卡气泡改为「双源合并」

前端 `SeatCell` 气泡取数改为:

```
优先 botCtx?.last_speech (Agent:含 kind 分类 speak/emotion_speak/interject/whisper/last_words)
兜底 player?.last_speech  (真人:无 kind,按 speak 渲染)
```

- Agent 座位:保持 01 号方案的全部能力(kind 徽章 / 私聊占位 / streaming 光标);
- 真人座位:显示 💬 + 发言 + 相对时间,无 kind 徽章、无 streaming 光标(真人不调 LLM);
- 取时间戳较新者,避免两源打架。

### 3.4 保留不动(01 号方案已完成且经用户验收)

P0-2 发言实时发布(`RecordLastSpeech` + `SwitchEmotionFx` 触发推送)、P0-3 `last_speech*` 四字段、
P1-1 法官播报对称、P1-2 Whisper 过滤链、P1-3 四个 CSS 类补全、P1-4 字号提升、
座位卡「1 主区 + 5 次区」信息架构与 `werewolf-speech.css` —— **全部保留,本次不动**。

---

## 4. 明确不做

| 项 | 理由 |
|---|---|
| 保留 `发言时间线` | 与 `房间聊天` 信息重复,用户明确要求删除 |
| 气泡上加展开/历史/多条 | 用户明确「显示最后一次发言即可,不要有过多的设计」 |
| 删除 `recentSpeeches` 缓冲 | 仍是 Agent prompt + 法官总结的数据源,删则 Agent 失忆 |
| 为真人补 `bot_contexts` 条目 | 语义错位(真人无 LLM 指标),应走 `PlayerJSON` |

---

## 5. 验收标准

| # | 判据 |
|---|---|
| A1 | 页面上不再存在「💬 发言时间线」模块;`grep -r SpeechTimeline ClientWeb/src` 零命中 |
| A2 | `grep -rn "speeches" ClientWeb/src ServerGo --include=*.ts --include=*.tsx --include=*.go` 仅剩 `recentSpeeches`(Agent/法官用) |
| A3 | Agent 座位卡气泡功能不回归(kind 徽章 / 3s 高亮 / 相对时间 / 私聊占位) |
| A4 | **真人玩家**座位卡在公屏发言后出现气泡 |
| A5 | 私聊不写入 `PlayerJSON.last_speech`(公开面零泄露) |
| A6 | `go build` + `go test ./...` + `tsc --noEmit` + `npm run build` 全绿 |
| A7 | Agent prompt 的 `RecentSpeeches` 与法官总结不受影响 |

---

## 6. 实施顺序

1. 后端:D5 删 wire 投影 → §3.2 加 `PlayerJSON.last_speech*` + `lastSpeechBySeat` → `go build` + `go test ./...`
2. 前端:D1–D4 删除 → §3.3 气泡双源 → `tsc --noEmit` + `npm run build`
3. `./rebuild_restart_app.sh`
4. 中文 git 提交
