# Agent 调用大模型 API 优化方案 §20260810-13

> 文档编号: §20260810-13
> 触发: 2026-08-10 用户反馈 MeiTuan-model 在 16:34:31 ~ 16:38:44（约 4 分钟）内产生 **23 次重复 LLM 调用**，全部来自同一 session、同一 night_wolves 阶段，工具列表固定为 `wolf_kill,emotion_switch_speak`，平均单次耗时 11.3 秒，累计浪费约 260 秒 API 时间。
> 目标: 在不影响 Agent 功能和游戏流程的前提下，从架构层面减少无意义的重复 LLM 调用。

---

## 1. 根因分析

### 1.1 现象数据（MCP 接口实测）

| 指标 | 值 |
|------|----|
| 时间窗口 | 16:34:31 ~ 16:38:44（231 秒） |
| 调用次数 | 23 次 |
| 调用频率 | 5.75 次/分钟 |
| 模型 | MeiTuan-model → LongCat-2.0 |
| 阶段 | night_wolves（狼人夜间投票） |
| 工具列表 | `wolf_kill, emotion_switch_speak` |
| 平均单次耗时 | 11.3 秒 |
| 累计 API 耗时 | 259.5 秒 |
| 并发因子 | 1.12x（单 bot 串行内循环） |
| 成功率 | 100%（23/23 200 OK） |

> 数据来源: `http://localhost:9101/ChatAnalysisInterface`，user=`liusm191`，model=`MeiTuan-model`，session=`59db905c-6ece-40cf-a2aa-2231a849459a:...`。

### 1.2 核心根因：内层决策循环无上限

`ServerGo/agent/wwplayer/run.go:634` 的内层 `for` 循环自 §130 重构后**取消了 MaxToolUse 硬上限**（原为 4，现为 0 = 无限），完全依赖 LLM 输出 `end_turn` / `max_tokens` / 纯 text 来退出。

```
handleEvent → for {
    1. BuildTools()
    2. 调 LLM（~11s for MeiTuan）
    3. resp.StopReason != "tool_use" → auto-skip + return ← 纯 text 会走这里
    4. 遍历 tool_use 块 → DispatchTool()
    5. 若 saidSomething == false → 循环继续（回到第 1 步）
}
```

**问题链**:

1. LLM 调用 `wolf_kill(target=X)` → 成功 → `WolfVoteCast[seat]=true`
2. `wolf_kill` **不在 `saidSomething=true` 列表中** → 循环继续
3. 下一轮 LLM 可能再调 `wolf_kill`（失败：already voted）、或 `emotion_switch_speak`、或其他工具
4. 每失败/重试一轮，又多一次 LLM 调用（11.3 秒）
5. 没有任何轮次上限 → 23 次调用 = 浪费约 22 次

### 1.3 同类风险（全阶段审计）

以下阶段的"行动类工具"同样不触发 `saidSomething=true`，存在相同的循环浪费风险：

| 阶段 | 行动工具 | 是否触发 saidSomething | 风险等级 |
|------|---------|----------------------|---------|
| night_wolves | `wolf_kill` | ❌ | 🔴 高（已触发） |
| night_seer | `seer_check` | ❌ | 🟠 中 |
| night_witch | `witch_act` | ❌ | 🟠 中 |
| night_guard | `guard_protect` | ❌ | 🟠 中 |
| night_demon_hunter | `demon_hunter_hunt` | ❌ | 🟠 中 |
| vote | `vote` | ❌ | 🟠 中 |
| sheriff | `vote_sheriff` / `run_sheriff` | ❌ | 🟡 低 |
| hunter_shoot | `hunter_shoot` | ❌ | 🟡 低 |
| 所有阶段 | `wolf_whisper` | ❌ | 🟠 中（可反复调用） |
| 所有阶段 | `use_prop` / `prop_inspect` 等 | ❌ | 🟡 低 |

夜间阶段风险最高，因为：
- 夜间工具（wolf_kill / seer_check / witch_act / guard_protect / demon_hunter_hunt）**每局每角色只应调用 1 次**
- 调用成功后继续循环完全无意义
- 慢模型（MeiTuan 11.3s/次）× 多轮 = 大量 API 浪费

---

## 2. 优化策略（6 条防线）

### 策略 1（P0）：单次行动后强制退出内循环

**核心改动**: 单次行动工具（一次性完成本阶段核心动作的工具）成功调用后，**强制 break 内层循环**。

**适用工具清单**:
- `wolf_kill`（狼人投票后不应再循环）
- `seer_check`（预言家查验后不应再循环）
- `witch_act`（女巫用药后不应再循环）
- `guard_protect`（守卫守护后不应再循环）
- `demon_hunter_hunt`（猎魔人狩猎后不应再循环）
- `hunter_shoot`（猎人开枪后不应再循环）

**实现方式**: 在 `run.go` 的工具派发循环中，增加 `actionDone := false` 标志位。若命中上述工具且**调用成功**（`derr == nil`），设置 `actionDone = true`。循环末尾检查 `actionDone` 即 return。

**为什么不直接 saidSomething=true**：
- `saidSomething` 语义是"发了公开消息"，用于控制发言限流、auto-speech 兜底等
- 夜间行动工具不涉及公开发言，不应污染 `saidSomething` 的语义
- 新增独立标志位更清晰、可审计

### 策略 2（P0）：内层循环最大轮次上限（Phase-Specific）

**核心改动**: 恢复 MaxToolUse，但改为**按阶段差异化配置**，而非全局统一值。

| 阶段类型 | 最大轮次 | 说明 |
|---------|---------|------|
| 夜间行动类（wolf/seer/witch/guard/demon_hunter） | **3** | 1 次主行动 + 最多 2 次修正（看错/改主意） |
| 投票类（vote / sheriff） | **3** | 1 次投票 + 最多 2 次思考 |
| 发言类（speak / death_lyric） | **5** | 发言 + 1-2 个道具/情绪 + 收尾 |
| hunter_shoot / idiot_reveal | **2** | 一次性行动 |

**默认兜底**: 若某阶段未配置，使用全局默认值 **5**（§130 前是 4，略放宽）。

**实现位置**: `run.go:634` 内层循环计数器 + `run.go` 顶部 phase → maxRounds 映射表。

**与策略 1 的关系**: 双重保险。策略 1 是"成功即退"的正向路径，策略 2 是"失败/重复尝试"的兜底上限。即使 LLM 反复调用失败的工具（如 wolf_kill 报错"already voted"），也不会超过 N 轮。

### 策略 3（P1）：wolf_whisper 调用次数软上限

**核心改动**: `wolf_whisper` 是狼队内协调工具，理论上 1-2 条留言足够，但 LLM 可能陷入"反复讨论不决策"的循环。增加**每阶段最多 N 次 wolf_whisper** 的软限制（在 prompt 中告知，在 tool dispatch 中强制）。

- **硬上限**: 每 night_wolves 阶段最多 **3 次** wolf_whisper
- 超限后工具返回 "wolf_whisper limit reached, please wolf_kill now" 错误
- 错误明确提示 LLM 立即调用 `wolf_kill`

**同步修改**: 工具 description 中加入"最多调用 3 次，之后必须 wolf_kill 投票"的硬约束。

### 策略 4（P1）：工具描述去冗余 + 明确"调用后即结束"

**核心改动**: 在所有单次行动工具的 description 末尾追加统一声明：

```
⚠️ 【硬约束】本工具是本阶段的唯一行动工具，调用成功后本阶段你的行动即结束，系统将推进游戏流程。
调用后不要再尝试调用其他工具，也不要反复调用本工具。
```

并删除 description 中可能诱发"多轮协商"的措辞（如 wolf_kill 描述中的"与队友协商统一目标"改为"参考队友投票，一次性投出你的决定"）。

**为什么重要**: 慢模型 + 长 prompt = 容易陷入"先讨论再行动"的思维惯性。明确告知"调一次就完事"能显著降低犹豫循环。

### 策略 5（P2）：投票后自动关闭 MyTurn 刷新内循环上下文

**核心改动**: 在 `wolf_kill` 成功后、内层循环继续之前，**重新从 rp() 拉取 live GameContext**，更新 `evt.Context.MyTurn`（应为 false，因为已投票）。这样下一轮 `BuildTools()` 时：
- `wolf_kill` 仍在（因为工具列表只看 phase+role）
- 但 prompt 中的 `【轮到你行动】` 标记消失，降低 LLM 继续行动的倾向

> 注：此策略为补充优化，不能替代策略 1/2。即使 MyTurn=false，LLM 仍可能继续调工具。

### 策略 6（P2）：成功行动后工具列表降级

**核心改动**: 单次行动工具成功调用后，下一轮 `BuildTools` 时将该工具从列表中移除（在 tool 的 `MountIf` 中增加"是否已行动"的检查）。

- 优点: LLM 根本看不到工具，物理上不可能再调用
- 缺点: 需要在 GameContext 中传递"已行动"状态，改动面较大
- 优先级: P2，在策略 1-4 验证有效后再考虑

---

## 3. 实施路径

### Phase 1（P0，本提交）: 策略 1 + 策略 2

**文件清单**:

| 文件 | 改动 |
|------|------|
| `ServerGo/agent/wwplayer/run.go` | 1. 新增 `phaseMaxInnerRounds` 映射表；2. 内层循环加 round 计数器；3. 新增 `actionDone` 标志位，单次行动工具成功后强制退出 |
| `ServerGo/agent/wwplayer/tools.go` | 工具描述追加"调用后即结束"声明（策略 4 先行部分） |

**单次行动工具判定方式**: 新增 `isSingleActionTool(name, phase) bool` 函数，集中维护策略 1 适用工具清单。

### Phase 2（P1，后续提交）: 策略 3 + 策略 4 完整版

- wolf_whisper 每阶段上限
- 所有工具描述去冗余 + 明确行动边界

### Phase 3（P2，后续提交）: 策略 5 + 策略 6

- 内循环上下文刷新
- 已行动工具从列表移除

---

## 4. 验证方案

### 4.1 单元测试

新增 `ServerGo/agent/wwplayer/run_inner_loop_test.go`:

| 测试用例 | 预期 |
|---------|------|
| `TestInnerLoop_WolfKillExitsAfterSuccess` | wolf_kill 成功后内层循环立即退出（≤2 轮） |
| `TestInnerLoop_MaxRoundsEnforced` | LLM 反复调非退出工具时，达到 maxRounds 后强制退出 |
| `TestInnerLoop_WolfWhisperLimit` | wolf_whisper 超过 3 次后返回 limit 错误 |
| `TestInnerLoop_SpeakPhaseAllowsMoreRounds` | speak 阶段允许更多轮次（验证差异化配置） |

### 4.2 集成验证

1. 启动狼人杀 13 人局（7 bot + 配置 MeiTuan-model）
2. 观察 night_wolves 阶段的 LLM 调用次数
3. **预期**: 单 bot 单阶段 ≤ 3 次 LLM 调用（原为 23 次，减少 ~87%）
4. 同时验证游戏流程正常推进（白天发言、投票等阶段不受影响）

### 4.3 量化指标

- **夜间阶段单 bot LLM 调用次数**: 优化前 23 次 → 优化后 ≤ 3 次（减少 87%+）
- **整局游戏总 API 调用量**: 预计减少 30-50%（夜间阶段占大头）
- **游戏推进速度**: night_wolves 阶段耗时从 4+ 分钟降至 ~1 分钟

---

## 5. 不变式与安全约束

1. **不影响游戏逻辑**: 所有优化只减少 LLM 调用次数，不改变 Agent 决策结果
2. **不降低游戏质量**: 上限 3 轮足够 LLM 做"看信息 → 决策 → 行动"的完整链路
3. **watchdog 仍是最后兜底**: 内层循环上限 ≠ watchdog；watchdog 负责 phase-level stall 检测
4. **发言阶段保持宽松**: speak 阶段 5 轮上限确保正常发言不受影响
5. **失败路径不变**: LLM 调用失败的重试逻辑（7 次线性退避）完全保留

---

## 6. 扩展到其他 Agent

当前优化针对 `wwplayer.Agent`（狼人杀玩家 Bot）。以下 Agent 也需审计：

| Agent | 风险 | 优先级 |
|-------|------|--------|
| `wwjudge.AgentJudge` | 法官工具少（announce/prompt_actor/summary/declare_cause/idle_silent），单次工具后退出，风险低 | P3 |
| `wwplayer.Agent`（其他阶段） | 白天发言/投票阶段需审计 | P1（已覆盖） |
| 未来其他游戏 Bot | 新游戏接入时必须遵循本规范 | P2 |

**长期规则**: 所有 Agent 内层循环必须同时满足：
- 有最大轮次上限（配置化，不得为 0 / 无限）
- 单次行动工具成功后强制退出
- 工具描述明确说明"调用即结束本阶段行动"

---

## 7. 关联文档与教训

- §130 重构引入"取消 MaxToolUse 硬上限"——本次为回退 + 差异化优化
- §97 / §107: watchdog 是 phase-level 兜底，不解决 inner-loop-level 浪费
- §197: 慢模型 11.3s/次 × 多轮 = 巨大浪费，更凸显本优化的必要性
- §R243: 狼 bot 活跃但不投票（LLM 166/166 成功）= 内层循环无意义调用的同源问题
- 教训: **「无上限 + 依赖模型自觉」是性能浪费的温床**。Agent 循环必须有工程化的硬上限，不能完全信任 LLM 的自我约束。
