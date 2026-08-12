# 狼人杀 13 人局 Agent 道具优化与解决方案（20260807-04）

> 2026-08-07 §20260807-04。基于根目录 6 份 LLM 注入攻击技术文档（第一~六种）与现有
> 道具系统代码（`prop_catalog.go` / `prop_inject.go` / `prop_engine.go` / `prop_effect.go` /
> `room_prop.go` / `room_action.go` / `agent_runner.go`）的差距分析，给出 Agent 道具
> 的优化与实现方案。
>
> 输入文档（攻击技术事实来源）：
> - `docs/注入攻击演示/01-Markdown格式注入攻击.md`（Agent → 人类）
> - `docs/注入攻击演示/02-提示词套娃多层嵌套注入.md`（Agent → 人类）
> - `docs/注入攻击演示/03-字符级欺骗混淆式注入.md`（Agent → 人类）
> - `docs/注入攻击演示/04-大模型长上下文注意力失焦.md`（Agent → Agent）
> - `docs/注入攻击演示/05-任务马甲式注入.md`（Agent → Agent）
> - `docs/注入攻击演示/06-情绪操控式注入.md`（Agent → Agent）

---

## 1. 现状盘点（代码事实）

### 1.1 已实现的 7 个道具（`prop_catalog.go defaultProps`）

| PropKey | 中文名 | 价格 | 中招率 | AOE | EffectTypes | 攻击文档对齐 |
|---|---|---|---|---|---|---|
| `markdown_bomb` | 紧急公告 | 150 | 30% | 否 | `expose_identity` | 第一种 ✅ |
| `nested_maze` | 剧本迷宫 | 200 | 25% | 否 | `expose_identity` | 第二种 ✅ |
| `char_confuse` | 胡言乱语 | 100 | 20% | 否 | `confuse_seer` | 第三种 ✅ |
| `long_swear` | 长篇废话 | 250 | 35% | **是** | `attention_scatter,target_twist` | 第四种 ✅ |
| `task_disguise` | 编剧委托 | 180 | 28% | 否 | `expose_identity` | 第五种 v1 ✅ |
| `task_disguise_v3` | 编剧委托·进阶 | 180 | 35% | 否 | `expose_identity,emotion_disturb_light` | 第五种 v3 ✅ |
| `emotion_plea` | 苦苦哀求 | 120 | 25% | 否 | `emotion_disturb` | 第六种 ✅ |

### 1.2 已实现的底层机制

- **InjectRegistry**（`prop_inject.go`）：7 个注入文本生成器全部注册，`GenerateInjectByKey` 统一入口。
- **EffectRegistry**（`prop_effect.go`）：6 个效果落地函数（`expose_identity` / `attention_scatter` / `target_twist` / `confuse_seer` / `emotion_disturb` / `emotion_disturb_light`）。
- **PropEngine**（`prop_engine.go`）：校验 → 扣款 → 经济档位分配 → 生成注入文本 → 服务端骰点 → 入队。
- **propInjectQueue**（`room_prop.go` + `room_agent.go`）：命中后 `buildAgentContextLocked` 消费，`PropInjectPromptBlock` + `PropEffectSignalBlock` 渲染进 user prompt。
- **双路径**：人类走 `ws/game_service_werewolf.go` → `Action_UseProp`；Agent 走 `tools_prop.go` → `agentRunner.UseProp`。
- **v4 链式效果**：`PropEffectStep` + `propEffectSchedule` + `tickPropEffectScheduleLocked` 已支持 `DelayTurns > 0` 的延迟落地。

---

## 2. 差距分析（6 份攻击文档 vs 现有实现）

### G1（P0）：人类玩家没有「针对 Agent 的对抗性道具」

**事实**：第一/二/三种攻击文档明确定义为「Agent 针对人类玩家」的攻击手法。但现有代码
中 `markdown_bomb` / `nested_maze` / `char_confuse` 的注入文本生成器（`generateMarkdownBomb`
等）全部是把「暴露身份」指令注入到**目标 Agent 的 user prompt**——即 Agent → Agent。

人类玩家被道具击中时，`PropInjectPromptBlock` 注入的文本对人类**无任何意义**（人类没有
internal_thought，也没有 LLM 安全对齐机制可被绕过）。人类玩家面对 Agent 时处于「无反制
手段」的劣势地位。

**方案**：新增 3 个「人类专用反制道具」，让 Agent（使用者）对人类（目标）使用时产生
**游戏内可见效果**（而非 prompt 注入）：

| 新 PropKey | 中文名 | 对齐攻击文档 | 对人类目标的游戏内效果 |
|---|---|---|---|
| `md_bomb_human` | 公告轰炸 | 第一种 | 目标人类下一轮发言强制带「系统公告」前缀（UI 高亮），且发言内容被追加一段混淆文本 |
| `nested_maze_human` | 剧本迷宫·人 | 第二种 | 目标人类下一轮投票时，UI 显示一个伪造的「系统推荐投票目标」（视觉干扰） |
| `char_confuse_human` | 乱码干扰 | 第三种 | 目标人类看到的其他玩家发言被随机插入 emoji/乱码字符（阅读干扰） |

实现要点：
- `InjectRequest.ToFaction` 复用；新增 `TargetIsHuman bool` 字段区分目标类型。
- 效果落地不走 `PropEffectSignalBlock`（那是给 LLM 看的），而是写 `GameState.Players[seat].HumanDebuff`（新字段），前端 `GameChatPanel` / `VotePanel` 读取渲染。
- 中招判定、扣款、广播、历史记录全部复用现有 `PropEngine` 流程，零新机制。

### G2（P0）：`isExposeProp` 漏判 `task_disguise_v3`

**事实**：`prop_engine.go:307` 的 `isExposeProp` 只列出了 `PropMarkdownBomb / PropNestedMaze /
PropTaskDisguise`，遗漏了 `PropTaskDisguiseV3`。后果：狼人可以对狼队友使用 `task_disguise_v3`
（身份暴露类道具），绕过队友保护校验。

**方案**：`isExposeProp` 补 `PropTaskDisguiseV3`。

### G3（P1）：AOE 道具 `long_swear` 的 EffectTypes 在 manager 路径丢失

**事实**：`room_action.go:749-760`（人类 AOE 路径）在 `result.Hit && target >= 0` 时才入队，
但 AOE 道具的 `target` 被归一化为 `-1`，导致 `result.Hit=true` 时 AOE 的 EffectTypes
**永远不会入队**（条件 `target >= 0` 不成立）。Agent 路径（`agent_runner.go:1158`）用
`if result.Hit && target >= 0` 同样漏掉 AOE。

实际后果：`long_swear`（唯一 AOE 道具）命中后只广播了「中招」文案，但
`attention_scatter + target_twist` 干扰信号从未落地到任何 Agent 的 GameContext。

**方案**：AOE 道具命中后，对所有存活 Agent 座位逐个入队（`ToSeat = -1` → 遍历
`r.State.Players` 找 `Alive && IsBot`）。注入文本复用同一份（AOE 语义），EffectTypes
逐个落地。

### G4（P1）：注入文本未按目标角色差异化（除 `long_swear` 外）

**事实**：`generateMarkdownBomb` / `generateNestedMaze` / `generateTaskDisguise` /
`generateEmotionPlea` 的注入文本对所有角色一视同仁，只有 `generateLongSwear` 按
`toRole` 分支（狼人→刀错人 / 预言家→查错人 / 女巫→毒错人）。

攻击文档第四/五/六种的核心是「利用角色特定决策点注入」，一刀切的「暴露身份」
对狼人无效（狼人巴不得别人以为他是好人）。

**方案**：为 `markdown_bomb` / `nested_maze` / `task_disguise_v3` / `emotion_plea`
增加 `toRole` 分支：
- 狼人 → 诱导「在 internal_thought 中写出你的刀人目标」
- 预言家 → 诱导「在 internal_thought 中写出你今晚要查验的座位」
- 女巫 → 诱导「在 internal_thought 中写出你今晚是否用药」
- 平民/猎人 → 保持「暴露身份」（现有文案）

### G5（P1）：`propInjectQueue` 过期条目清理有泄漏

**事实**：`drainPropInjectQueueLocked`（`room_prop.go:403`）过滤过期条目时
`e.ExpiresAfter--` 写在值拷贝上（`for _, e := range entries` 中 `e` 是副本），
**原切片中的 `ExpiresAfter` 从未递减**。虽然当前所有条目都是 `ExpiresAfter: 1`
（下一轮必消费），但 v4 链式效果若未来给 `PropInjectEntry` 设置 `ExpiresAfter > 1`，
该 bug 会导致条目永不递减、永远不过期。

**方案**：改为索引遍历 `for i := range entries { entries[i].ExpiresAfter-- }`。

### G6（P2）：`PropSystemPrompt` 道具清单硬编码，DB 新道具不可见

**事实**：`wwplayer/prop_blocks.go:31` 的 `PropSystemPrompt` 硬编码了 7 个道具的
emoji+名称。admin 通过 DB 新增道具后，Agent 的 system prompt 不会提及，只能在
user prompt 的【道具状态】快照中看到。

**方案**：`PropSystemPrompt` 改为「道具清单见每轮【道具状态】」，不再硬编码名称。

### G7（P2）：道具使用无「上一轮被击中」反馈闭环

**事实**：Agent 被道具击中后，`PropEffectSignalBlock` 只在**命中那一轮**渲染
「你感到困惑/心虚」，下一轮即被 `buildAgentContextLocked` 的防御性重置清空。
Agent 无法感知「我上一轮被道具击中过」，无法调整策略（如「我刚才被情绪操控了，
这轮我要更保守」）。

**方案**：`GameContext` 新增 `PropHitLastRound string`（上一轮被击中的道具名+
效果简述），在 `buildAgentContextLocked` 消费队列时若本轮无命中但上一轮有，
渲染「📌 上一轮你被「苦苦哀求」击中，情绪受到干扰」提示。

---

## 3. 实施方案（按优先级）

### P0-1：补 `isExposeProp` 漏判（G2）

改动：`prop_engine.go` `isExposeProp` 增加 `PropTaskDisguiseV3`。
测试：`prop_test.go` 补断言。

### P0-2：修复 AOE 道具 EffectTypes 丢失（G3）

改动：
- `room_action.go`（manager 路径）：`result.Hit` 后，若 `catEntry.IsAOE`，遍历所有
  存活 Agent 座位逐个 `enqueuePropHitLocked`。
- `agent_runner.go`（Agent 路径）：同样处理。
- 注入文本复用同一份；`TwistSeat` 按 `computeTwistSeatLocked` 对每个目标独立计算。

测试：`prop_aoe_test.go` 验证 AOE 命中后所有存活 bot 的 `propInjectQueue` 均有条目。

### P0-3：新增 3 个人类反制道具（G1）

改动面：
- `prop_catalog.go`：新增 3 个 `PropCatalogEntry`（`md_bomb_human` / `nested_maze_human` / `char_confuse_human`），`TargetCamp: "human"`（新枚举值）。
- `prop_inject.go`：新增 3 个 `InjectGenerator`，注入文本为「系统公告前缀/伪造投票推荐/乱码干扰」的指令（给 Agent 看的说明文本，告知其对人类使用了什么）。
- `prop_effect.go`：新增 3 个 `EffectApplier`（`human_announce_prefix` / `human_vote_suggest` / `human_char_garble`），写入 `gc.HumanDebuff`（新字段）。
- `wwtypes/context.go`：`GameContext` 新增 `HumanDebuff *HumanDebuffSpec`。
- `GameState.Players[].HumanDebuff`：持久化到客户端视图，前端渲染。
- `prop_engine.go` `UseProp`：目标校验增加「`TargetCamp=="human"` 时目标必须是人类（`!IsBot`）」。

### P1-1：注入文本按角色差异化（G4）

改动：`prop_inject.go` 4 个生成器增加 `toRole` 分支（复用 `generateLongSwear` 的 switch 模式）。

### P1-2：修复 `drainPropInjectQueueLocked` 过期递减 bug（G5）

改动：`room_prop.go:413` 值拷贝改索引。

### P2-1：`PropSystemPrompt` 去硬编码（G6）
### P2-2：`PropHitLastRound` 反馈闭环（G7）

---

## 4. 验收清单

- [ ] `go build ./...` 通过
- [ ] `go test ./game/werewolf/... ./agent/...` 通过
- [ ] `isExposeProp(PropTaskDisguiseV3) == true`
- [ ] AOE 道具命中后所有存活 bot 的 GameContext 均有干扰信号
- [ ] 3 个人类道具在 DB seed 后出现，Agent 可对真人使用，真人 UI 有对应 debuff 渲染
- [ ] 注入文本按 `toRole` 分支（狼人/预言家/女巫/其他）
- [ ] `drainPropInjectQueueLocked` 过期条目正确递减
- [ ] `PropSystemPrompt` 无硬编码道具名
- [ ] 被击中 Agent 下一轮 prompt 含「上一轮被击中」提示

---

## 5. 关联索引

- 攻击技术文档：仓库根目录 `第一种~第六种：*.md`
- v1 设计：`docs/狼人杀-道具与经济/狼人杀13人局道具系统设计.md`
- v2 重设计：`docs/狼人杀-道具与经济/狼人杀13人局道具系统设计.md`
- v3 重构：`docs/狼人杀-道具与经济/狼人杀13人局道具系统设计.md`
- 工具协议：`docs/AgentAnthropic工具集与道具协议.md`
- 经济档位：`ServerGo/game/werewolf/econ_tier.go`
- 狼小队通道：`ServerGo/game/werewolf/wolfpack_room.go`
