# 德州扑克 Agent 金币设计（v1.0）

_实现状态（2026-08-21 更新）_

| 章节 | 状态 | 实现位置 |
|------|------|---------|
| §2 结算规则（净筹码盈亏 + 抽水） | ✅ 已完成 | `ServerGo/agent/thpagent/econ_tier.go` + `driver.go::onHandOverLocked` |
| §3 EconTier 经济档位联动 | ✅ 已完成 | Health 5% / Caution 7% / Danger 10% |
| §4 Bot 经济行为约束（clamp） | ✅ 已完成 | 单手 ±5000 / 单局 ±30000 / 单房间 ±100000 |
| §5 金币变动日志 | ✅ 已完成 | t_lsm_game_wallet_log（reason 枚举 5 种） |
| §6 Bot 金币统计与画像 | ⏳ 部分完成 | 基础统计字段已定义，Profile 迭代 v1.1 |
| §7 与 wallet_service 集成 | ✅ 已完成 | Credit/Debit/Rake 三路径 |
| §8 测试用例 | ✅ 已完成 | econtier_test.go + wallet_thp_test.go + clamp_test.go |



> 本文定义德州扑克 Bot 在**手牌结算**时的金币变动规则、与狼人杀的差异点、
> 经济档位联动、EconTier 反通胀机制。代码落地前请先阅读本文档 +
> [德州扑克金币设计.md](./德州扑克金币设计.md) + [CLAUDE.md §132 §133](../../CLAUDE.md)。

## 1. 与狼人杀金币设计的差异

| 维度 | 狼人杀 13 人局 | 德州扑克 6-max |
|---|---|---|
| 结算时机 | 一局结束（按阵营分胜负） | **每手牌结束**（按净盈亏分胜负） |
| 结算金额 | 固定 ±100 金币 | **按筹码盈亏**（赢家 +净筹码，输家 -净筹码） |
| 抽水 | 无 | **5% 底池抽水**（operator 收入） |
| 失败惩罚 | 无（输家不扣） | **有**（输家按净筹码扣金币） |
| 平局 | 极少见 | **常见**（平局 split pot，每家 +N/2） |

**核心差异**：狼人杀是「**阵营**经济」—— 同一阵营共享胜负；德州扑克是「**个人**经济」——
每个玩家独立盈亏。这导致：

1. **结算频率高**：狼人杀一局 ≈ 30 分钟结算一次；德州扑克一手牌 ≈ 1-3 分钟结算一次
2. **金额波动大**：狼人杀 ±100 固定；德州扑克 -3000 到 +5000 不等（按底池大小）
3. **金币反馈感强**：玩家能立刻看到自己「这手牌赢/输了多少钱」

## 2. 结算规则（v1.0）

### 2.1 净筹码盈亏

每手牌结束（无论是摊牌还是弃牌胜出），赢家获得 `net_chip_delta = +(won_chips - committed_chips)` 金币，
输家获得 `net_chip_delta = -(committed_chips - won_chips)` 金币（实际 = `-committed_chips`，因没赢到任何筹码）。

**公式**：

```
my_net_chip_delta = sum(committed_to_pots) - sum(refund_from_pots)
```

按手牌结束时的实际筹码流向计算（已在 `texasholdem.GameState.showdown()` 内部分配，无需额外计算）。

### 2.2 抽水（5%）

底池分账前，先抽 5% 进入「系统池」（operator 收入）：

```
rake = floor(pot * 0.05)
winners_share = (pot - rake) / len(winners)
```

**示例**：底池 1000 → rake=50 → winners 各拿 475（2 人）或 316.67（3 人，奇数筹码给最近 button 的赢家）

### 2.3 边界

- **筹码归零淘汰**：玩家 `stack <= 0` 时自动从座位移除（与狼人杀的「死亡即公开身份」不同——德州扑克无身份概念），
  金币按 `NetChipDelta` 结算，但下一手牌不再参与。
- **重开局**：玩家可以金币从钱包补筹码回座位（手动操作，不自动）。
- **底池过大限制**：单手牌底池上限 = `cfg.MaxPotPerHand = 100,000` 筹码，超过则拒绝开局（防恶意刷金币）。

## 3. EconTier 经济档位联动

沿用 [CLAUDE.md §133](../../CLAUDE.md) EconTier 机制（Health / Caution / Danger 三档）：

```
Health: 房间总金币 ≥ 50,000
Caution: 房间总金币 10,000-50,000
Danger: 房间总金币 < 10,000
```

**与德州扑克的差异**：

| 档位 | 道具系统销毁率（§133） | 德州扑克抽水率 |
|---|---|---|
| Health | 30% | **5%（标准）** |
| Caution | 40% | **7%** |
| Danger | 50% | **10%** |

**抽水率提高的理由**：当玩家总金币低（多数是输家），提高抽水率抑制「低筹码翻盘」刷钱；
当玩家总金币高（多数是赢家），降低抽水率保持游戏体验（赢家少被抽水）。

**EconTier 计算时机**：每手牌结算后实时更新；`ComputeEconTier(roomTotalCoin)` 与 §133 同源。

## 4. Bot 经济行为约束

### 4.1 不刷钱（BOT 公平性）

- Agent 不能利用「金币少 → Danger 档 → 高抽水」机制主动输金币再换金币（同 §118 「公平性」）。
- 服务端在 `Action_AgentDecision` 校验：若 Agent 异常频繁 fold → consecutiveFailures++（防止 LLM 故意不作为）。

### 4.2 Bot 盈亏范围

为防止金币爆炸/归零，每手牌 Bot 盈亏做硬限制：

| 维度 | 上限 | 下限 |
|---|---|---|
| 单手牌净盈亏 | ±5,000 筹码 | -5,000 筹码 |
| 单局总盈亏 | ±30,000 筹码 | -30,000 筹码 |
| 单房间累计盈亏 | ±100,000 筹码 | -100,000 筹码 |

**超限**：服务端在 `Action_AgentDecision` 校验时强制 clamp：
- 赢太多了 → `actual_payout = min(requested_payout, +5000)`
- 输太多了 → `actual_loss = max(requested_loss, -5000)`（差额补回玩家钱包）

### 4.3 Bot 不能「allin 全部筹码」自杀

- Agent 调 `poker_action{action:"allin"}` 时，服务端校验 `amount > stack * 0.9` 强制 break：
  「allin 必须 >90% 筹码」 才允许；<90% 改为 raise 到 90% 筹码。

**例外**：bot 筹码 < 200（大盲的 1 倍）时允许 allin（别无选择）。

## 5. 金币变动日志

每手牌结算后写 `t_lsm_game_wallet_log`（沿用狼人杀同表）：

```sql
INSERT INTO t_lsm_game_wallet_log
  (user_id, room_id, delta, reason, game_kind, hand_number, created_at)
VALUES
  ('user1', 'room1', +350, 'texasholdem_hand_win', 'texasholdem', 5, NOW());
```

`reason` 枚举：
- `texasholdem_hand_win` —— 摊牌/弃牌胜出
- `texasholdem_hand_loss` —— 摊牌/弃牌输
- `texasholdem_hand_draw` —— 平局（split pot）
- `texasholdem_rake` —— 抽水扣款（赢家扣）
- `texasholdem_rebuy` —— 手动补充筹码

## 6. Bot 金币统计与画像

沿用狼人杀的 `t_lsm_game_agent_player_profile` 表，新增 4 个统计字段（每个 bot model_key × 人类 user_id 组合）：

| 字段 | 类型 | 说明 |
|---|---|---|
| `thp_hands_played` | int | 累计参与手牌数 |
| `thp_hands_won` | int | 累计胜出手牌数 |
| `thp_net_chips` | bigint | 累计净盈亏（筹码数） |
| `thp_avg_pot_size` | int | 平均底池大小 |
| `thp_fold_rate` | float | 弃牌率（0.0-1.0） |
| `thp_bluff_rate` | float | 虚张成功率（成功偷盲次数 / 总偷盲次数） |

**Profile 更新时机**：每手牌结算后由 `AgentProfileService` 异步更新（不阻塞游戏流）。

## 7. 与 wallet_service 集成

沿用 `service/wallet_service.go` 的 4 个核心 API：

| API | 用途 |
|---|---|
| `WalletService.GetBalance(userID)` | 查询余额 |
| `WalletService.Credit(userID, amount, reason)` | 加金币 |
| `WalletService.Debit(userID, amount, reason)` | 扣金币 |
| `WalletService.Transfer(from, to, amount, reason)` | 玩家间转账 |

**Bot 调用限制**：

```go
// agentdriver.go:onHandOverLocked
func (d *TexasHoldemAgentDriver) onHandOverLocked(r *texasholdem.Room, winners []int, payouts []int) {
    for seat, payout := range payouts {
        userID := r.State.Players[seat].UserID
        if payout == 0 { continue }
        reason := "texasholdem_hand_win"
        if payout < 0 {
            reason = "texasholdem_hand_loss"
        }
        if err := d.walletSvc.Credit(userID, int64(payout), reason); err != nil {
            logger.L().Warn("agent wallet credit failed", zap.Error(err))
        }
        // 抽水（赢家扣）
        if payout > 0 {
            rake := int64(float64(payout) * d.econTier.RakeRate())
            d.walletSvc.Debit(userID, rake, "texasholdem_rake")
        }
    }
}
```

**幂等保护**：通过 `t_lsm_game_wallet_log.hand_number` UNIQUE 约束防止重复结算（同手牌两
次结算会抛 DB 错误，由 caller 兜底 log + 跳过）。

## 8. 测试用例

- `wallet_thp_test.go` — Credit/Debit/Rake 各 3 项（标准 / Caution / Danger 档）
- `econtier_test.go` — ComputeEconTier 边界（49,999 / 50,000 / 10,001 / 9,999）
- `agent_profile_test.go` — Profile 统计准确性（100 手牌模拟）
- `clamp_test.go` — Bot 盈亏超限 clamp（赢太多了 + 输太多了）