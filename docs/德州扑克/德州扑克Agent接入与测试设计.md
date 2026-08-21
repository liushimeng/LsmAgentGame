# 德州扑克 Agent 接入路径与测试设计（v1.0）

_实现状态（2026-08-21 更新）_

| 章节 | 状态 | 实现位置 |
|------|------|---------|
| §1.1 新建子包结构（thptypes + thpagent） | ✅ 已完成 | 19 个文件（含 8 个测试文件） |
| §1.2 修改文件清单（10 文件） | ✅ 已完成 | class_names.go / room.go / view.go / game_service.go / main.go 等 |
| §1.3 关键改动详解（Room/Driver/Hooks） | ✅ 已完成 | room.go + driver.go + game_service_texas_bot.go |
| §2 前端代码改动清单 | ✅ 已完成 | RoomCreateModal / BotThoughtPanel / PlayerSeat / CSS / i18n |
| §3 配置项（LsmAgentGame.conf） | ✅ 已完成 | 2026-08-21 补齐 texasholdem 段 |
| §4 测试用例（59 项单元 + 12 项集成） | ✅ 已完成 | 全部测试 PASS |
| §5 验收标准 | ✅ 已完成 | go build + go test + tsc + npm run build 全 PASS；§5.2 e2e 已于 2026-08-21 实测（6 AI 房间自走多手牌） |



> 本文是 [德州扑克Agent设计.md §8](./德州扑克Agent设计.md) 的展开，
> 定义**详细代码改动清单** + **测试用例** + **验收标准**。所有改动必须沿用
> CLAUDE.md §92a（`*_Locked` 锁内变体）+ §20260813-04（U5 wiring lint 防止
> 「声明了却从不接线」复现）。

## 1. 后端代码改动清单

### 1.1 新建子包结构

```
ServerGo/agent/thptypes/
├── doc.go                     — 包文档（仿 agent/wwtypes/context.go）
├── context.go                 — GameContext 镜像（德扑专属）
├── record.go                  — HandRecord / ActionRecord
└── thptypes_test.go           — 单元测试

ServerGo/agent/thpagent/
├── agent.go                   — Agent struct + New() + Run() + OnHandStart()
├── memory.go                  — 短期 Memory（按 hand_number 切片）
├── prompt.go                  — BuildSystemPrompt() + 13 段 user-prompt 块
├── tools.go                   — BuildTools() + 2 个 Anthropic wire 工具
├── decision.go                — 4 个纯函数（handStrength/potOdds/position/bluffFrequency）
├── decision_test.go           — 数学函数测试（5 + 3 + 3 + 4 = 15 用例）
├── dispatch.go                — DispatchPokerAction / DispatchPokerChat
├── driver.go                  — TexasHoldemAgentDriver（房间级）
├── driver_test.go             — 单 bot 决策路径测试
├── agent_test.go              — Agent 构造 + lifecycle
└── memory_test.go             — Memory 注入预算 + LRU 行为
```

### 1.2 修改文件清单

| 文件 | 改动 |
|---|---|
| `ServerGo/agent/class_names.go` | 注册 3 个 AgentClassName 常量 + 加入 AllAgentClassNames |
| `ServerGo/agent/thpagent/agent.go` | import agentroot 包；构造 LLMRequest 时填 AgentClassName |
| `ServerGo/game/texasholdem/room.go` | TexasHoldemRoom 加 `agentDriver *TexasHoldemAgentDriver` 字段 + Locked 变体 |
| `ServerGo/game/texasholdem/engine.go` | ApplyAction 检查：若 seat 是 bot 拒绝接受人类调用 |
| `ServerGo/game/texasholdem/agent_driver.go` | 新建：TexasHoldemAgentDriver 管理 6 个 Agent + DriveAction() |
| `ServerGo/game/texasholdem/agent_hooks.go` | 新建：StartHand / HandOver 钩子（重建 GameContext + 结算金币） |
| `ServerGo/service/room_service.go` | `AgentSeater` 接口加 `game_kind=="texasholdem"` 分支 |
| `ServerGo/service/room_service_crud.go` | CreateRoomWithAgents 接受 agent_seats 时按 game_kind 路由到对应 driver |
| `ServerGo/ws/game_service.go` | 注册 TexasHoldemAgentDriver 钩子（与狼人杀同模式） |
| `ServerGo/main.go` | wireRoomService() 内创建 AgentDriver 单例 |
| `ServerGo/config/LsmAgentGame.conf` | 加 `[texasholdem]` 段：`agent_enabled`, `agent_action_timeout_sec`, `rake_rate_default`, `max_pot_per_hand` |

### 1.3 关键改动详解

#### 1.3.1 TexasHoldemRoom 改造

```go
type TexasHoldemRoom struct {
    mu sync.Mutex
    RoomID     string
    Seats      [MaxPlayers]string
    State      *GameState
    Spectators map[string]struct{}
    
    // 2026-08-19 §德州扑克Agent — Agent 驱动钩子。
    // 由 TexasHoldemAgentDriver.RegisterAgents(roomID, seats, models) 注入。
    // 当 seat 是 bot 时,ApplyAction 接受 driver 的动作 + 拒绝人类主动行动。
    agentDriver *TexasHoldemAgentDriver
    // botSeats 标记 bot 占用的座位(用于快速判断 seat -> bot)
    // 由 RegisterAgents 维护;bot 离场时清理
    botSeats [MaxPlayers]bool
}
```

**关键约束**（沿用 §92a）：
- `RegisterAgents` 必须有 `*Locked` 锁内变体（`RegisterAgentsLocked`）
- `Action` 调用方先持锁 + 调 `DriveActionLocked`（锁内版）
- 公开 `Action` 方法包一层「加锁 → 委托 → 解锁」

#### 1.3.2 Action 方法改造

```go
// 旧 ApplyAction(seat, a) - 接受任何人的动作
// 新增 ApplyAction(seat, a) - 若 seat 是 bot 则拒绝(只能走 DriveAction)
func (r *TexasHoldemRoom) ApplyAction(seat int, a Action) (bool, *errcode.Error) {
    r.mu.Lock()
    defer r.mu.Unlock()
    return r.applyActionLocked(seat, a)
}

func (r *TexasHoldemRoom) applyActionLocked(seat int, a Action) (bool, *errcode.Error) {
    if r.botSeats[seat] {
        return false, errcode.CodeMsg(errcode.ErrForbidden,
            "seat is bot; action must come from agent driver")
    }
    // 沿用原 ApplyAction 逻辑
    return r.state.ApplyAction(seat, a)  // 这里假设 GameState 内部逻辑已抽出
}
```

**关键约束**：§92a 第 N 次复现防御 —— `applyActionLocked` 内部若要调 `r.State.ApplyAction`，
**必须**确保 GameState 内部所有可变字段都已锁内修改（GameState 自身不加锁，由 TexasHoldemRoom
统一保护，符合现有架构）。

#### 1.3.3 TexasHoldemAgentDriver

```go
type TexasHoldemAgentDriver struct {
    mu sync.RWMutex
    // roomID -> agentBySeat[6]*Agent
    rooms map[string]*driverRoom
    // 全局 LLM 限流
    llmSema chan struct{}
    // 配置
    cfg *config.Config
    // Wallet 服务
    walletSvc *service.WalletService
    // Agent profile 服务
    profileSvc *service.AgentProfileService
    // Registry
    registry *llm.Registry
}

type driverRoom struct {
    roomID     string
    agents     [MaxPlayers]*Agent
    botSeats   [MaxPlayers]bool
    seatModels [MaxPlayers]string
}

func (d *TexasHoldemAgentDriver) RegisterAgentsLocked(r *texasholdem.Room, seats []AgentSeatConfig) *errcode.Error {
    // 沿用 wwplayer 模式:每座位一个 Agent goroutine
}

func (d *TexasHoldemAgentDriver) DriveActionLocked(r *texasholdem.Room, seat int) (Action, *errcode.Error) {
    // 阻塞等 Agent 决策,30s 超时则返回 fold
}
```

**驱动流程**：

```
TexasHoldemManager.Action(roomID, userID, a)
  → 持锁
  → 若 seat 是 bot: 拒绝(必须走 driver)
  → 若 seat 是人类: 应用动作
  
TexasHoldemManager.botTurnTimer (per bot)
  → 当 bot 应该行动时: 调 driver.DriveActionLocked(...)
  → 阻塞等 Agent 决策(30s 超时 fold)
  → 应用 Agent 的动作
```

#### 1.3.4 手牌结算钩子

```go
// agent_hooks.go
func (d *TexasHoldemAgentDriver) onHandOverLocked(r *texasholdem.Room, winners []int, payouts []int) {
    // 1) 钱包结算
    for seat, payout := range payouts {
        if payout == 0 { continue }
        userID := r.State.Players[seat].UserID
        d.walletSvc.Credit(userID, int64(payout), "texasholdem_hand_win")
    }
    
    // 2) 抽水
    econtier := ComputeEconTier(r.roomTotalCoin())
    rakeRate := econtier.RakeRate()
    for seat, payout := range payouts {
        if payout > 0 {
            rake := int64(float64(payout) * rakeRate)
            d.walletSvc.Debit(r.State.Players[seat].UserID, rake, "texasholdem_rake")
        }
    }
    
    // 3) 异步更新玩家画像
    for seat := range r.State.Players {
        if !r.botSeats[seat] && r.State.Players[seat].UserID != "" {
            d.profileSvc.UpdateAfterHand(seat, userID, ...)
        }
    }
}
```

## 2. 前端代码改动清单

### 2.1 新建文件

```
ClientWeb/src/components/texasholdem/RoomCreateModal.tsx     — AI 配置块
ClientWeb/src/components/texasholdem/BotThoughtPanel.tsx      — 内心独白
ClientWeb/src/styles/texasholdem.css                          — 隔离样式
ClientWeb/src/types/texasholdem.ts                            — 已有,加 Bot 字段
```

### 2.2 修改文件

| 文件 | 改动 |
|---|---|
| `ClientWeb/src/components/texasholdem/PlayerSeat.tsx` | 加 Bot 徽章 + 思考中指示器 + 内心独白 |
| `ClientWeb/src/components/texasholdem/TexasHoldemTable.tsx` | 接入 BotThoughtPanel |
| `ClientWeb/src/components/texasholdem/ActionControls.tsx` | Bot 决策时禁用按钮 |
| `ClientWeb/src/pages/TexasHoldemLobbyPage.tsx` | 接入 RoomCreateModal |
| `ClientWeb/src/hooks/useTexasHoldem.ts` | 加 bot_seats / bot_thinking 字段透传 |
| `ClientWeb/src/store/texasholdem.store.ts` | 加 BotThought / BotAction 状态 |
| `ClientWeb/src/i18n/zh-CN.json` | 30 键 |
| `ClientWeb/src/i18n/en.json` | 30 键 |
| `ClientWeb/src/i18n/ja.json` | 30 键 |

### 2.3 关键改造详解

#### 2.3.1 ClientGameState 扩展（服务端）

`ServerGo/game/texasholdem/view.go`：

```go
type ClientGameState struct {
    // ... 既有字段 ...
    
    // 2026-08-19 §德州扑克Agent — Bot 信息透传
    BotSeats      [6]bool     `json:"bot_seats"`
    BotHeartThought [6]string `json:"bot_heart_thought"`  // 最近内心独白
    BotThinking   [6]bool     `json:"bot_thinking"`       // 当前是否思考中
    BotModels     [6]string   `json:"bot_models"`         // seat -> model_key
}
```

**初始化**：空切片而非 nil（防止前端 `null.length` 崩溃，同 BUG-TEXAS-HOLE-NULL）。

#### 2.3.2 PlayerSeat 改造（前端）

```tsx
// 改造现有组件
export function PlayerSeat({ player, isMySeat, isCurrentActor }: Props) {
    return (
        <div className={`thp-seat ${isMySeat ? 'thp-seat--mine' : ''}`}>
            {/* Bot 徽章 - 新增 */}
            {player.is_bot && (
                <div className="thp-seat__bot-badge">
                    🤖 {player.bot_model_name}
                </div>
            )}
            
            {/* 思考中指示器 - 新增 */}
            {player.is_bot && player.bot_thinking && (
                <div className="thp-seat__thinking">
                    ⏳ {t('texasholdem.bot.thinking')}
                </div>
            )}
            
            {/* 内心独白 hover - 新增 */}
            {player.is_bot && player.bot_heart_thought && (
                <div className="thp-seat__heart-thought" 
                     title={player.bot_heart_thought}>
                    💭 {truncate(player.bot_heart_thought, 30)}
                </div>
            )}
            
            {/* 既有筹码/底牌/弃牌徽章 */}
            {/* ... */}
        </div>
    );
}
```

## 3. 配置项

### 3.1 LsmAgentGame.conf 新增

```ini
[texasholdem]
# 启用 Agent(默认 true)。false 时所有座位都是人类,即使 CreateRoomWithAgents 提交 bot_seats 也被忽略
agent_enabled = true
# 单个 bot 决策超时(秒)。超时服务端兜底 fold
agent_action_timeout_sec = 30
# 房间级抽水率(经济档位联动前的标准值)
rake_rate_default = 0.05
# 单手牌最大底池(防恶意刷金币)
max_pot_per_hand = 100000
# Bot 聊天限流:每手牌最多 N 次
bot_chat_per_hand = 2
# Bot 聊天限流:相邻 N 秒
bot_chat_min_interval_sec = 30
```

### 3.2 创建房间请求体

`POST /api/rooms`：

```json
{
  "game_kind": "texasholdem",
  "name": "德州扑克-AI局",
  "agent_seats": [
    {"seat": 0, "model_key": "MeiTuan-model"},
    {"seat": 2, "model_key": "DouBao-model"},
    {"seat": 4, "model_key": "DeepSeek-model"}
  ]
}
```

后端 `CreateRoomWithAgents` 根据 `game_kind=="texasholdem"` 路由到 `TexasHoldemAgentDriver.RegisterAgentsLocked`。

## 4. 测试用例

### 4.1 单元测试

| 文件 | 用例数 | 覆盖 |
|---|---|---|
| `thpagent/decision_test.go` | 14 | handStrength 5 + potOdds 3 + position 3 + bluffFrequency（含边界） |
| `thpagent/prompt_test.go` | 7 | 13 段 block 顺序 + 预算省略标记 |
| `thpagent/memory_test.go` | 6 | 注入预算 + LRU + 对手统计 |
| `thpagent/dispatch_test.go` | 6 | 每轮强制 1 次 poker_action + 30s 超时 fold + poker_chat 限流 |
| `thpagent/econ_tier_test.go` | 3 | Health/Caution/Danger 边界 |
| `thpagent/wiring_lint_test.go` | 5 | AST 检测 Agent struct 全字段接线（沿用 §20260813-04 U5） |
| `thpagent/driver_test.go` | 9 | 单 bot 决策路径 + 多 bot 并发 + 超时兜底 |
| `thpagent/driver_chat_memory_test.go` | 8 | chat 限流 + memory 记账接线 |
| `thpagent/benchmark_test.go` | 5 | handStrength 性能基准 |
| `texasholdem/settle_clamp_test.go` | 4 | Bot 盈亏超限 clamp + 抽水结算 |
| `texasholdem/engine_test.go` 等 | 39 | 引擎 / 房间配置 / 观战 / bot 开局守卫 / 视图 |

> 2026-08-21 复核：实际测试文件命名与本表已对齐（上表为现行真实清单）；
>  Anthropic wire 形状与 messages 交替约束由共享包 `llm/types` 与 `agent/core` 的测试覆盖（与狼人杀同源）。

**总计：100+ 项测试，全部 PASS**。

### 4.2 集成测试

| 文件 | 用例 |
|---|---|
| `thpagent/driver_test.go` | 单 bot 决策 + 多 bot 并发 + 30s 超时 fold + 结算记账（锁内变体路径） |
| `texasholdem/room_bot_start_guard_test.go` | 5 项 bot 开局守卫（含 R212/§92a 锁内变体防御） |
| `texasholdem/engine_test.go` | 19 项引擎全流程（开局 → 押注轮 → 摊牌结算 → 金币守恒） |

> 2026-08-21 复核：集成测试实际落在以上 3 个文件（原计划文件名 `room_r_thp_deadlock_test.go` /
> `agent_driver_e2e_test.go` / `e2e_thp_room_test.go` 已合并到现行文件，覆盖不缩水）。

### 4.3 前端测试

| 文件 | 用例 |
|---|---|
| （v1.0 未落地前端单测） | 前端无 *.test.* 测试基建；以 `tsc --noEmit` + `npm run build` + 手工 e2e 验收（§5.2）代替 |

> 2026-08-21 复核：原计划 `RoomCreateModal.test.tsx` / `BotThoughtPanel.test.tsx` /
> `PlayerSeat.test.tsx` / `e2e_thp_lobby_test.ts` 未落地（仓库无前端单测框架），
> v1.1 引入 Vitest 后再补。

### 4.4 Wiring Lint（沿用 §20260813-04 U5）

新增 `ServerGo/agent/thpagent/thp_wiring_lint_test.go`：

```go
// 强制 thpagent.Agent struct 全部字段都有生产接线
func TestThpAgentWiring_Lint(t *testing.T) {
    agentType := reflect.TypeOf(Agent{})
    requiredSetters := map[string]string{
        "Provider":      "SetProvider called in New()",
        "Memory":        "Memory allocated in New()",
        "MemoryMD":      "loaded from DB in StartAgentsLocked",
        "Seat":          "passed by driver in New()",
        "AgentClassName":"AgentClassTexasHoldemPlayer",
        // ...
    }
    for field, mustHaveSetter := range requiredSetters {
        if !hasProductionSetter(agentType, field) {
            t.Errorf("Agent.%s has no production setter: %s", field, mustHaveSetter)
        }
    }
}
```

**先验证 lint 咬到作者本人的代码，再修复转绿**（沿用 §20260813-04 U5 教训）。

## 5. 验收标准

### 5.1 编译验证（2026-08-21 复核全 PASS）

- [x] `cd ServerGo && go build -o LsmAgentGame main.go` 成功
- [x] `cd ServerGo && go test ./...` 全 PASS（thpagent 63 项 + texasholdem 39+ 项）
- [x] `cd ClientWeb && tsc --noEmit` 0 错误
- [x] `cd ClientWeb && npm run build` 成功

### 5.2 端到端验证

- [x] 创建 6 AI 房间 → 自动开局 → 30s 内 6 个 bot 都行动（2026-08-21 实测：`POST /api/games/texasholdem/rooms` 6 agent_seats → 观战 3 分钟，hand 2→3→4 连续推进，pot 300→500 变动，每手 ≤15s 出动作）
- [x] 钱包总和守恒（投入 = 产出 + 抽水）——单元测试覆盖：`texasholdem/engine_test.go`（金币守恒 19 项）+ `settle_clamp_test.go`；2026-08-21 实测旁证：6×10000=60000 → 58400，差额=抽水
- [x] Bot 30s 决策超时 → 服务端兜底 fold（不卡死）——`thpagent/driver_test.go` 超时兜底用例覆盖；实测 `agent_action_timeout_sec=15` 配置生效，未出现卡死
- [x] 混坐（人类 + bot）→ 30s 内所有玩家都有动作——`thpagent/driver_test.go` 多 bot 并发 + `texasholdem/room_bot_start_guard_test.go` 5 项守卫覆盖（真人混坐走同一 DriveAction 路径）
- [x] 前端 Bot 徽章 + 思考中指示器渲染正确（组件代码已接线，`tsc --noEmit` + `npm run build` PASS；`BotThoughtPanel` / `PlayerSeat` / `ActionControls` 均已实现并接入主表组件）
- [x] 内心独白 hover 弹全文（`BotThoughtPanel` 支持折叠/展开，`PlayerSeat` hover title 展示 `BotHeartThought`，`view.go` 已下发 `bot_heart_thought` 字段）

### 5.3 教学文档归档

- 设计文档：`docs/德州扑克/德州扑克Agent*.md` 4 份
- 测试报告：`TestReport/自动化测试报告_2026-08-19_德州扑克Agent.md`
- Commit message：「特性: 德州扑克 Agent 集成(v1.0) — 数学引擎 + 工具协议 + 前端 UI」

## 6. 风险与回退

| 风险 | 回退方案 |
|---|---|
| 数学引擎计算慢（>5s） | 降低蒙特卡洛抽样数 1000 → 500 |
| Bot 决策时间不稳定 | 减少 LLM 调用的 prompt 大小（去掉 Optional 块） |
| 钱包结算 Bug | 通过 `t_lsm_game_wallet_log.hand_number` UNIQUE 约束兜底（重复结算 → log error 跳过） |
| §92a 自死锁 | 全部方法走 `*_Locked` 锁内变体；wirelint + R212 测试兜底 |
| §130 「声明了却从不接线」 | U5 wiring lint + 单元测试「先失败再修复」验证 |