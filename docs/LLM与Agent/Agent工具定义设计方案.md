# Agent 工具定义 — 解决和设计方案 (20260804-01)

> **主题**：狼人杀 13 人局 Agent 的 Anthropic 协议层 `emotion_switch` 工具重构为合并工具 `emotion_switch_speak`。
> **触发问题**：线上抓包显示 LLM 在单次响应中连续产生 **10 次** `emotion_switch` tool_use（`emotion_switch_ResponseBody_01.json:1-100`），reason 文本多为「调 speak 补发言下限」「稳了」等空话，无任何决策意义，既消耗 output_tokens 又拖慢响应。
> **目标读者**：参与狼人杀 Agent 维护的 backend-dev / frontend-dev / integration-tester / game-designer。
> **日期**：2026-08-04。

---

## 1. 背景与诊断

### 1.1 抓包证据

`emotion_switch_ResponseBody_01.json` 抓取自狼人杀 13 人 AI 局第 2 天下午·投票放逐阶段，Kimi 模型（8 号玩家）。单次响应里：

```
[
  {"type": "tool_use", "name": "emotion_switch", "input": {"emotion": "confident", "reason": "投票阶段票型已基本成形,11号出局在即"}},
  ...  // 再连续 9 次 emotion_switch,emotion 都是 confident,reason 都是「调 speak 补发言下限」
]
```

服务端日志记录：

- 该房间 588 次 emotion_switch 拒绝中 **475 次**来自 `night_wolves` 的非狼座位（自相矛盾契约）。
- 输出 token 大量浪费在重复 tool_use 块结构上。

### 1.2 当前实现的核心缺陷

1. **§124 自相矛盾契约** — `ServerGo/agent/tools.go:634-642` 的修复已经承认：当 phase+role 没有行动工具时仍下发 `emotion_switch` schema，LLM 只能反复单独调用并被服务端拒绝。
2. **LLM 不会自动合并** — 把"必须合并"硬编码到 schema description 仍不能阻止 LLM 单独调用。三次重试机制（`ServerGo/agent/run.go:1347-1379`）每次重试都消耗一轮 LLM 调用 + token。
3. **状态泄漏风险** — 旧 emotion_switch 不消耗限流但会立即修改 emotion state；如果未来扩展到死亡场景，独立切情绪工具会让 bot 在死前"挣扎"地改 10 次 emotion，制造无意义的下游状态写入。
4. **API 调用时间差异公平性违反** — §120 约束慢模型单 tool_use。10 次连续 emotion_switch 是单 LLM 响应内的多次 tool_use，违反公平性原则。

### 1.3 与 §119 speak_with_thought 的对比

`speak_with_thought`(2026-07-10 §119) 是同类型合并工具的成功范式：把「公开话 + 内心独白」绑定为一次 tool_use，LLM 必须同时给两段，否则拒绝。这个范式可以原样搬到情绪切换上：**「发言 + 切情绪」绑定为一次 tool_use**。

---

## 2. 解决方案：合并工具 `emotion_switch_speak`

### 2.1 设计原则

- **合并而非并列** — text、emotion、reason 三个字段绑定在同一个 tool_use 中，强制 LLM 在一次响应内同时给出。
- **原子回滚** — speak 失败（被限流/拒绝/去重为空）时不切 emotion，保留上一状态。
- **优雅降级** — emotion 字段可省略（"我只发言不动情绪"），reason 仅在 emotion 指定时生效。
- **接口收紧** — 删除旧 `emotion_switch` 工具的所有接线（schema / dispatch / handler），不留 stub。LLM 用旧名会得到 `unknown tool` 错误。
- **schema enum 收敛** — 删除 `random` 占位（避免 LLM 偷懒让系统随机选）。

### 2.2 新工具 schema

```json
{
  "name": "emotion_switch_speak",
  "description": "【合并发言 + 情绪切换】在同一 tool_use 中同时完成发言 + 切情绪。\n"
                 "═══════════════════════════════════════════════════════════════\n"
                 "【硬约束 — 2026-08-04 重构】\n"
                 "  • 单次响应只能有 0 或 1 次 emotion_switch_speak（不可并发多次）\n"
                 "  • 服务端先校验发言(text)，通过后原子切换情绪；发言被拒绝/截断/\n"
                 "    去重为空时不切情绪,emotion 保持上一状态\n"
                 "  • 与 speak 不能同响应并存（避免双发言）;与其他行动工具(vote 等)\n"
                 "    可以并存\n"
                 "  • 只想静默时用 idle_silent;只想投票用 vote;**已删除独立 emotion_switch**\n"
                 "═══════════════════════════════════════════════════════════════\n"
                 "【10 类情绪速查】\n"
                 "  • confident(自信从容) / excited(亢奋得意) — 积极·决策果断\n"
                 "  • calm(冷静平淡) — 中性·客观中立\n"
                 "  • panic(紧张恐慌) / wary(疑虑警惕) — 消极·决策保守\n"
                 "  • irritated(恼怒急躁) / grievance(委屈不满) — 消极·情绪化\n"
                 "  • confused(困惑茫然) — 消极·划水\n"
                 "  • guilty(心虚愧疚) — 狼人撒谎时心虚\n"
                 "  • tired(懈怠疲惫) — 消极·低唤醒划水",
  "input_schema": {
    "type": "object",
    "properties": {
      "text": {
        "type": "string",
        "description": "公开发言内容,≤80字,对所有玩家可见"
      },
      "emotion": {
        "type": "string",
        "enum": ["confident","excited","calm","panic","wary","irritated","grievance","confused","guilty","tired"],
        "description": "目标情绪;省略=保持当前"
      },
      "reason": {
        "type": "string",
        "description": "切换原因(≤80字,仅在 emotion 指定时生效)"
      }
    },
    "required": ["text"]
  }
}
```

### 2.3 关键约束

| # | 约束 | 服务端实现位置 |
|---|------|--------------|
| 1 | `text` 必填 | schema `required: ["text"]` |
| 2 | emotion 可省略 | schema `required: ["text"]` 仅 text |
| 3 | reason 仅在 emotion 提供时生效 | handler 内 `if emotion != ""` 才记 reason |
| 4 | 不允许 emotion="random" | schema enum 删除 random |
| 5 | 单响应最多 1 次 emotion_switch_speak | dispatcher 末尾遍历 ToolUses(),仅最后一次生效,前面的 result 拼 `[superseded]` |
| 6 | speak 失败时不切 emotion | handler 内先调 Speak → 校验 err → 仅在 nil 时 SwitchEmotion |
| 7 | 复用 Speak 限流/去重/广播链 | handler 直接 `r.Speak(cleaned)` |

---

## 3. 完整修改面

### 3.1 后端 Go

| 文件 | 改动 |
|------|------|
| `ServerGo/agent/tools.go` | (1) ToolRunner 接口加 `EmotionSwitchSpeak(text, emotion, reason string) (string, error)`; (2) `BuildTools` 删除 emotion_switch phase 注册块（行 616-697），新增 emotion_switch_speak 注册块放在 §119 speak_with_thought 之后; (3) `DispatchTool` 删除 `case "emotion_switch"`（行 1036-1050），新增 `case "emotion_switch_speak"`; (4) action log 抽取逻辑自动适配 |
| `ServerGo/game/werewolf/agent_runner.go` | (1) 新增 `EmotionSwitchSpeak` 方法：先 dedupSpeakText → r.Speak(cleaned) → 校验 err → 仅在成功时 r.agent.SwitchEmotion; (2) 删除 `EmotionSwitch`(1506) 与 `EmotionSwitchRandom`(1521) 两个方法 |
| `ServerGo/agent/agent.go` | (1) `BotTranscript` 加 `EmotionReason string` 字段; (2) `BotTranscript()` 在 emotion 字段拷贝后追加 `bt.EmotionReason = a.emotion.reason`; (3) `recordTranscript` 在 `a.lastTranscript = bt` 前保留前次 HeartThought 与对应时间戳; (4) 删除 `emotionSwitchAloneCount` 字段与所有引用 |
| `ServerGo/agent/run.go` | 删除 emotion-only 三次重试逻辑（行 1347-1379）；相应清理 `a.emotionSwitchAloneCount` 引用 |
| `ServerGo/agent/emotion.go` | `SwitchEmotion` / `CurrentEmotion` / `EmotionMeta` 等核心 API 保持不变；`MyEmotionBlock` 不变；新增 `EmotionSwitchSpeakWriteRule()` 函数返回写规则 prompt 段 |
| `ServerGo/agent/prompt.go` | (1) 系统 prompt 工具清单（行 121）改为 `emotion_switch_speak / speak / speak_with_thought / ...`; (2) `BuildSystemPrompt` 末尾（在 §119/§120/§123 段后）注入 `EmotionSwitchSpeakWriteRule()` 输出 |

### 3.2 前端 TS/TSX

| 文件 | 改动 |
|------|------|
| `ClientWeb/src/lib/werewolf/emotion.ts` | **新建**：导出 `EmotionMeta` 类型 + `EMOTION_META` 常量表 + `getEmotionMeta(key)` 函数; emoji/label 通过 `t('werewolf.emotion.' + key)` 取 |
| `ClientWeb/src/components/werewolf/WerewolfTable.tsx` | (1) 删本地 emotion metadata 表(行 69-90); (2) 改 import 用 `getEmotionMeta`; (3) badge(行 296-313) 与 compact emoji(行 347-357) 增加 `title={botCtx?.emotion_reason}` 悬停显示"为什么突然变成紧张" |
| `ClientWeb/src/components/werewolf/FactionDrawer.tsx` | 行 138-195 改用 `getEmotionMeta(b.emotion)` 显示 emoji+label,reason 作为 hint 拼接 |
| `ClientWeb/src/components/werewolf/HistoryDrawer.tsx` | 🤖 独白 sub-tab 末尾追加 `<details>` "情绪变化曲线"折叠展示,遍历 `b.emotion_history` |
| `ClientWeb/src/i18n/locales/zh-CN.ts` | 加 10 个 `werewolf.emotion.*` 键 |
| `ClientWeb/src/i18n/locales/en.ts` | 同上 |
| `ClientWeb/src/i18n/locales/ja.ts` | 同上 |

### 3.3 数据库

无变化。emotion_state 与 emotion_history 仅在 `Agent` 内存中维护（§124 设计意图"可以后写 DB"至今未实现）。重构后 wire 协议多一个 `emotion_reason` 字段，但 `BotTranscript` 本来就全量下发到 game.state.bot_contexts，无需新表。

### 3.4 配置文件

无变化。`LsmAgentGame.conf` 不需要新键。

### 3.5 文档

| 文件 | 操作 |
|------|------|
| `docs/狼人杀-Agent与系统/狼人杀Agent情绪模块设计.md` | 重写"工具调用"章节为 emotion_switch_speak;删除"§124 自相矛盾契约"段落 |
| `docs/werewolf-protocol.md` | 工具表更新 |
| `docs/狼人杀13人局Agent工具集完整分析.md` | 同步工具清单与触发规则 |
| `docs/狼人杀Agent对话即思考设计.md` | §126 节删除旧 emotion_switch 描述 |
| `CLAUDE.md` | §15 加 1-2 行新工具;§13 §124 教训改为引用新工具 |

---

## 4. Handler 实现细节

### 4.1 `agentRunner.EmotionSwitchSpeak` 关键代码

```go
// EmotionSwitchSpeak 2026-08-04 §重构 — 合并发言 + 切情绪。
//
// 顺序：先走完整 speak 限流/去重/身份脱敏链 → 广播成功后才切情绪。
// speak 失败(被服务端拒绝/cooldown/去重为空)时 emotion 不动,reason 忽略。
//
// 复用 r.Speak(cleaned) 是为了避免重写 150+ 行 speak 过滤链。
func (r *agentRunner) EmotionSwitchSpeak(text, emotion, reason string) (string, error) {
    if r.agent == nil {
        return "", nil
    }
    cleaned, wasDeDuped, wasTruncated := dedupSpeakText(text)
    if cleaned == "" {
        return "emotion_switch_speak rejected: empty text after dedup (no emotion change)", nil
    }
    result, err := r.Speak(cleaned)
    if err != nil {
        return result + " (no emotion change)", err
    }
    if strings.HasPrefix(result, "speak rejected") || strings.HasPrefix(result, "speak_rate_limited") {
        return result + " (no emotion change)", nil
    }
    emotionChanged := false
    if emotion != "" && agent.IsValidEmotion(emotion) {
        r.agent.SwitchEmotion(emotion, reason)
        emotionChanged = true
    }
    suffix := ""
    if wasDeDuped {
        suffix += " [deduped adjacent repeats]"
    }
    if wasTruncated {
        suffix += " [truncated to 80 chars]"
    }
    if emotionChanged {
        meta := agent.EmotionMeta(emotion)
        suffix += fmt.Sprintf(" [emotion→%s(%s) %s]", meta.Name, emotion, meta.Emoji)
    }
    return result + suffix, nil
}
```

### 4.2 Dispatcher 单响应最多 1 次约束

```go
// 2026-08-04 §重构 — 单响应最多 1 次 emotion_switch_speak。
// 由 run.go 在调用 DispatchTool 之前先聚合 ToolUses()：
//   - 多次 emotion_switch_speak → 仅最后一次生效,前面的 result 拼 "[superseded]"
//   - emotion_switch_speak 与 speak 不能同响应 → 整组合并工具冲突,提示"don't combine"
```

实现方式：在 `ServerGo/agent/run.go` 的 `for _, tu := range resp.ToolUses()` 循环之前加聚合逻辑：

```go
// 单响应最多 1 次 emotion_switch_speak 的聚合器
var lastESS *toolUse  // 指向最后一次 emotion_switch_speak
var hasSpeak bool     // 是否包含 speak / speak_with_thought
for i := range resp.toolUses {
    tu := &resp.toolUses[i]
    if tu.Name == "emotion_switch_speak" {
        if lastESS != nil {
            tu.Skip = true  // 前面的标注跳过
            tu.Result = "[superseded: only last emotion_switch_speak in response is dispatched]"
        }
        lastESS = tu
    }
    if tu.Name == "speak" || tu.Name == "speak_with_thought" {
        hasSpeak = true
    }
}
if lastESS != nil && hasSpeak {
    lastESS.Result = "emotion_switch_speak rejected: cannot coexist with speak/speak_with_thought in same response"
    lastESS.Skip = true
}
```

### 4.3 Prompt 注入

`ServerGo/agent/prompt.go:121` 行（系统 prompt 工具清单）：

```
"speak / speak_with_thought / interject / whisper / emotion_switch_speak / vote / skip / wolf_kill / seer_check / witch_act / sheriff_candidate / sheriff_elect / sheriff_stream / hunter_shoot / wolf_suicide / idiot_reveal / finish_speak / finish_vote / start_day / idle_silent / last_words(last_words_skip) / ...\n"
```

`BuildSystemPrompt` 末尾追加（`ServerGo/agent/emotion.go` 新函数）：

```go
// EmotionSwitchSpeakWriteRule — 系统 prompt 中关于新合并工具的硬约束段。
// 注入位置：BuildSystemPrompt 末尾。
func EmotionSwitchSpeakWriteRule() string {
    return "\n【合并发言 + 切情绪(emotion_switch_speak) — 2026-08-04 重构】\n" +
        "  • 想发言时必须用 emotion_switch_speak(text=..., emotion=..., reason=...)\n" +
        "  • text 必填(≤80字);emotion 省略=保持当前;reason 仅在 emotion 指定时生效\n" +
        "  • 单次响应只允许 1 次 emotion_switch_speak;多次以最后一次为准,前面的会被服务端丢弃\n" +
        "  • emotion_switch_speak 与 speak / speak_with_thought 不能同响应(避免双发言)\n" +
        "  • 只想静默时用 idle_silent;只想投票用 vote\n" +
        "  • **已删除独立 emotion_switch 工具** — 不要再调\n"
}
```

---

## 5. 前端契约与渲染

### 5.1 wire 协议（不变）

`game.state.bot_contexts[i]` 中 emotion 相关字段（已有，前端已读）：

```ts
{
  emotion?: string;             // confident/excited/...
  emotion_reason?: string;      // ★ 后端补齐,从 emotion_state.reason 拷贝
  emotion_updated_at?: number;  // unix ms
  emotion_history?: Array<{ emotion: string; reason: string; at_ms: number }>;
}
```

### 5.2 新建 `ClientWeb/src/lib/werewolf/emotion.ts`

```ts
import { t } from '@/i18n';

export interface EmotionMeta {
  key: string;
  emoji: string;
  /** 通过 t() 动态获取,避免硬编码 */
  label: string;
}

const KEYS = ['confident','excited','calm','panic','wary','irritated','grievance','confused','guilty','tired'] as const;
export type EmotionKey = typeof KEYS[number];

const EMOJI: Record<EmotionKey, string> = {
  confident: '😌', excited: '🤩', calm: '😐', panic: '😨', wary: '🤔',
  irritated: '😤', grievance: '🥺', confused: '😵', guilty: '😬', tired: '😴',
};

export function getEmotionMeta(key?: string): EmotionMeta | null {
  if (!key || !KEYS.includes(key as EmotionKey)) return null;
  const k = key as EmotionKey;
  return { key: k, emoji: EMOJI[k], label: t(`werewolf.emotion.${k}`) };
}
```

### 5.3 WerewolfTable 渲染示例

```tsx
const meta = getEmotionMeta(botCtx?.emotion);
{meta && (
  <span className="ww-emotion-badge" title={botCtx?.emotion_reason ?? meta.label}>
    {meta.emoji} {meta.label}
  </span>
)}
```

### 5.4 FactionDrawer 渲染示例

```tsx
const meta = getEmotionMeta(b.emotion);
{!meta ? null : (
  <div className="faction-drawer__agent-emotion">
    <span className="emoji">{meta.emoji}</span>
    <span className="label">{meta.label}</span>
    {b.emotion_reason ? <span className="hint">— {b.emotion_reason}</span> : null}
  </div>
)}
```

### 5.5 HistoryDrawer 情绪变化曲线

```tsx
{spectator && b.emotion_history && b.emotion_history.length > 0 && (
  <details className="faction-drawer__emotion-history">
    <summary>🎭 情绪变化曲线 ({b.emotion_history.length})</summary>
    <ul>
      {b.emotion_history.map((h, i) => {
        const m = getEmotionMeta(h.emotion);
        return (
          <li key={i}>
            [{new Date(h.at_ms).toLocaleTimeString()}] {m?.emoji ?? '🎭'} {m?.label ?? h.emotion}
            {h.reason ? ` — ${h.reason}` : ''}
          </li>
        );
      })}
    </ul>
  </details>
)}
```

### 5.6 i18n 键

| key | zh-CN | en | ja |
|---|---|---|---|
| `werewolf.emotion.confident` | 自信从容 | Confident | 自信 |
| `werewolf.emotion.excited` | 亢奋得意 | Excited | 興奮 |
| `werewolf.emotion.calm` | 冷静平淡 | Calm | 冷静 |
| `werewolf.emotion.panic` | 紧张恐慌 | Panicked | パニック |
| `werewolf.emotion.wary` | 疑虑警惕 | Wary | 警戒 |
| `werewolf.emotion.irritated` | 恼怒急躁 | Irritated | いら立ち |
| `werewolf.emotion.grievance` | 委屈不满 | Grievance | 不満 |
| `werewolf.emotion.confused` | 困惑茫然 | Confused | 困惑 |
| `werewolf.emotion.guilty` | 心虚愧疚 | Guilty | 後ろめたさ |
| `werewolf.emotion.tired` | 懈怠疲惫 | Tired | 疲労 |

---

## 6. 测试与回归

### 6.1 修改现有测试

- `ServerGo/agent/emotion_switch_tools_test.go` → **改名** `emotion_switch_speak_tools_test.go`
  - 删除 "emotion_switch 单独可调用" 断言
  - 新增 "emotion_switch_speak schema 字段" 断言
  - 新增 "text 必填 / emotion 可省略" 断言
  - 新增 "未知 emotion 静默忽略" 断言
- `ServerGo/agent/agent_test.go` → 删除 `emotionSwitchAloneCount` 相关断言（行 1041-1080）
- `ServerGo/agent/guard_tools_test.go` → 删除 emotion_switch 旧名引用（行 128-130）
- `ServerGo/agent/run_r85_regression_test.go` → 删除 emotion-only 三次重试相关断言
- `ServerGo/agent/decision_summary_test.go` → 检查是否有 emotion 工具名引用并更新

### 6.2 新增测试

`ServerGo/agent/emotion_switch_speak_tools_test.go`（替换 emotion_switch_tools_test.go）：

| 测试名 | 断言 |
|---|---|
| `TestEmotionSwitchSpeakSchema_TextRequired` | schema `required` 包含 `text` |
| `TestEmotionSwitchSpeakSchema_EmotionOptional` | schema 不把 emotion 列入 required |
| `TestEmotionSwitchSpeakSchema_RandomRemoved` | schema enum 不含 `random` |
| `TestDispatch_EmotionSwitchSpeak_RoutesToRunner` | dispatcher 路由到 runner.EmotionSwitchSpeak |
| `TestDispatch_UnknownEmotionSwitch_NoMatch` | dispatcher 对 `emotion_switch` 旧名返回 "unknown tool" |

`ServerGo/agent/emotion_switch_speak_rollback_test.go`（新建）：

| 测试名 | 断言 |
|---|---|
| `TestRollback_SpeakRejected_NoEmotionChange` | mock Speak 返回 error → CurrentEmotion() 仍为初值 |
| `TestRollback_DedupEmpty_NoEmotionChange` | dedup 后空字符串 → CurrentEmotion() 不变 |
| `TestRollback_RateLimited_NoEmotionChange` | rate-limited → emotion 不变 |
| `TestRollback_ValidSpeak_EmotionChanges` | happy path → emotion 切换成功 |

`ServerGo/agent/emotion_switch_speak_limit_test.go`（新建）：

| 测试名 | 断言 |
|---|---|
| `TestLimit_SingleResponse_OnlyLastEffective` | 同响应 3 次 → 只第 3 次生效 |
| `TestLimit_CoexistWithSpeak_Rejected` | emotion_switch_speak + speak 同响应 → 整个合并工具被拒 |
| `TestLimit_CoexistWithVote_OK` | emotion_switch_speak + vote 同响应 → 两者都生效 |

### 6.3 前端测试

CLAUDE.md §13 没要求前端单元测试覆盖率，本期不新增。手工验证即可。

---

## 7. 实施顺序（最小风险路径）

1. 写方案文档（本文）。
2. `ServerGo/agent/emotion.go` 新增 `EmotionSwitchSpeakWriteRule()` 函数 → `go build`。
3. `ServerGo/agent/tools.go` 同时加新 + 删旧（接口 + BuildTools + Dispatcher）→ `go build` + `go test ./agent/`。
4. `ServerGo/game/werewolf/agent_runner.go` 新增 `EmotionSwitchSpeak` + 删除旧 `EmotionSwitch/EmotionSwitchRandom` → `go build` + `go test ./game/werewolf/`。
5. `ServerGo/agent/agent.go` 加 `EmotionReason` 字段 + `BotTranscript()` 拷贝 + HeartThought 保留 → `go build`。
6. `ServerGo/agent/run.go` 删 emotion-only 三次重试 + 删除 `emotionSwitchAloneCount` 字段 → `go test ./...`。
7. 改名 + 修改现有测试文件 → `go test ./...`。
8. 新增 3 个测试文件 → `go test ./...`。
9. `ServerGo/agent/prompt.go` 工具清单加 emotion_switch_speak + 注入新 prompt 段 → `go test ./...`。
10. 前端 `ClientWeb/src/lib/werewolf/emotion.ts` 新建 + WerewolfTable / FactionDrawer / HistoryDrawer 改造 + i18n 三语键 → `tsc --noEmit` + `npm run build`。
11. 文档同步（docs/狼人杀-Agent与系统/狼人杀Agent情绪模块设计.md / werewolf-protocol.md / CLAUDE.md 等）。
12. `./rebuild_restart_app.sh`。
13. 创建 13 人 AI 房间，抓包验证 wire 协议 + 单响应 1 次约束。
14. `git add . && git commit -m "重构: 狼人杀 Agent emotion_switch → emotion_switch_speak 合并工具 (20260804)"`。

---

## 8. 验收标准

### 8.1 自动测试

1. ✅ `cd ServerGo && go build -o LsmAgentGame main.go` 无错误。
2. ✅ `cd ServerGo && go test ./...` 全部通过（含 3 个新测试文件）。
3. ✅ `cd ClientWeb && npx tsc --noEmit` 无错误。
4. ✅ `cd ClientWeb && npm run build` 成功。

### 8.2 行为验证

5. ✅ 启动应用，登录 `test_01`，创建 13 人 AI 房间。后端日志：单次 LLM 响应里 `emotion_switch_speak` 至多 1 次；`emotion_switch` 旧名再出现会被 "unknown tool" 拒绝。
6. ✅ F12 Network 抓取 /api/llm/.. 调用的 RequestBody，`tools` 数组中**不再**包含 `emotion_switch`，包含 `emotion_switch_speak`，其 schema 字段为 `text`（必填）+ `emotion`（enum 10 keys 无 random）+ `reason`。
7. ✅ 玩家页 emotion badge 鼠标悬停显示 reason 文案。
8. ✅ FactionDrawer 显示 emoji + label 而非裸 `confident` key。
9. ✅ HistoryDrawer 🤖 独白 sub-tab 末尾可展开"情绪变化曲线"，仅 spectator 可见。
10. ✅ 模拟 LLM 返回 `emotion_switch_speak` 三次：服务端日志显示前两次为 `[superseded]`，emotion 最终态为第三次指定的值。
11. ✅ 模拟 LLM 返回 `emotion_switch_speak` 但 text 为空：服务端返回 `rejected: empty text after dedup (no emotion change)`；emotion 保持上一状态。
12. ✅ 模拟 `emotion_switch_speak` + `speak` 同响应：服务端返回 `cannot coexist with speak/speak_with_thought`；emotion 不变；speak 也不生效（整组被拒）。

### 8.3 提交

13. ✅ `./rebuild_restart_app.sh` 成功。
14. ✅ `git add . && git commit -m "重构: 狼人杀 Agent emotion_switch → emotion_switch_speak 合并工具 (20260804)"` 中文 commit message。

---

## 9. 风险与缓解

| 风险 | 缓解 |
|------|------|
| LLM 仍在 prompt 中用 emotion_switch 旧名 | 系统 prompt 显式写 "已删除 emotion_switch, 使用 emotion_switch_speak";旧工具不再注册,LLM 调用会被 "unknown tool" 拒绝 |
| speak 路径返回 error 时 emotion 已切 | EmotionSwitchSpeak 顺序：先调 Speak → 校验 err → 仅在 nil 时切 emotion |
| BotTranscript.EmotionReason 为空导致前端样式坏 | 渲染加 `b.emotion_reason ? ... : null` 兜底 |
| 单响应多次 emotion_switch_speak | dispatcher 末尾遍历 ToolUses(),仅最后一次生效,前面的 result 拼 `[superseded]` |
| emotion_switch_speak + speak 同响应 | dispatcher 拒绝整组合并工具,提示 "cannot coexist" |
| 删除 emotionSwitchAloneCount 字段 → 老数据兼容 | 该字段仅运行时计数器,无 DB 持久化,重启即丢,不影响 |
| LLM 强烈依赖旧的"切情绪但不动"场景（如死亡前的挣扎） | 死亡前最后一轮已有 §130 法官接管的"遗言"语义,新工具完全覆盖；如未来真需要独立切情绪能力,再以新名字 `set_emotion` 加回 |
| 前端 i18n 三语漏掉一个键 → 运行时 t() 返回 key 字符串 | 实施时一次性写完三语文件,tsc 编译时强制 i18n 类型检查 |

---

## 10. 总结

- **核心改动**：`emotion_switch`（独立工具）→ `emotion_switch_speak`（合并工具）。
- **关键修复**：
  1. 删 emotion-only 三次重试逻辑
  2. 删 emotionSwitchAloneCount 字段
  3. BotTranscript 加 emotion_reason 字段填补后端契约 gap
  4. HeartThought 不被 recordTranscript 覆盖
  5. 抽 emotion metadata 表到 `lib/werewolf/emotion.ts`
  6. FactionDrawer / HistoryDrawer 用 emoji + label 渲染
  7. 三语 i18n 补 10 个 emotion 键
- **预期收益**：
  - 单次 LLM 响应中 emotion 切换从最多 10 次降到最多 1 次
  - 慢模型（Kimi/GLM/DeepSeek 等）的情绪相关 token 浪费减少 ~90%
  - §120 公平性约束不再被 emotion_switch 滥用触发
  - 前端 emotion UI 从"裸 key"升级为"emoji + label + reason tooltip"
- **文档同步**：`docs/狼人杀-Agent与系统/狼人杀Agent情绪模块设计.md` 重写 + `CLAUDE.md` §15/§13 教训同步 + `werewolf-protocol.md` 工具表同步。