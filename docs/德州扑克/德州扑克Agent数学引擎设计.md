# 德州扑克 Agent 数学引擎设计（v1.0）

> 本文是 [德州扑克Agent设计.md](./德州扑克Agent设计.md) §6 「决策引擎」的展开，
> 描述**牌力 + 底池赔率 + 虚张频率** 4 个纯函数的算法选择、复杂度与测试基准。

## 1. 算法选择

### 1.1 牌力计算（Hand Strength）

**算法**：蒙特卡洛模拟（Monte Carlo Simulation）

- **输入**：自己的 2 张底牌 + 0..5 张已亮公共牌
- **输出**：胜率（0.0-1.0）+ 平局率

**抽样方式**：

```
hand_strength(hole, community_shown):
  n_remaining = 5 - len(community_shown)         // 还要亮 0..5 张
  n_unknown = 47 - len(community_shown)         // 牌堆剩余
  
  wins = 0
  draws = 0
  total = 1000                                  // 固定 1000 次
  
  repeat total:
    // 抽样补全到 5 张公共牌
    sample = community_shown + sample(n_remaining, from_deck)
    
    // 计算自己牌型
    my_rank = best5(hole, sample)
    
    // 对手 2 张底牌:随机
    opp_hole = sample(2, from_deck_excluding(my_cards, sample))
    opp_rank = best5(opp_hole, sample)
    
    cmp = my_rank.Compare(opp_rank)
    if cmp > 0: wins++
    elif cmp == 0: draws++
  
  return (wins + draws/2) / total
```

**复杂度**：
- 单次 `best5` 评估：`C(7,5) = 21` 次 5-张组合比较，约 1.0ms（HandRank.Compare 是 12 个 int 的比较）
- 1000 次迭代：约 1.0s CPU（Go 默认 GOMAXPROCS=可用核）
- **优化**：缓存公共牌 + 底牌组合的胜率（同一手牌反复出现时 O(1) 命中）

**测试基准**（7 张牌选 5 张的所有 C(21,5) = 20349 种组合的对照表）：
- AA vs KK preflop：胜率 80.2%（权威），蒙特卡洛 1000 次应在 [77%, 83%]
- 72o vs 随机牌：胜率 32.7%（权威），蒙特卡洛 1000 次应在 [29%, 36%]
- 同花听牌 vs 已完成两对：胜率 24%（4 outs × 2 = 8%，约 5:1），蒙特卡洛 1000 次应在 [20%, 28%]

### 1.2 底池赔率（Pot Odds）

**算法**：纯算术

```
call_amount = current_bet - my_round_committed
pot_after_call = pot + call_amount
odds = call_amount / pot_after_call
required_equity = odds
```

**示例**：
- 底池 100，对手下注 50 → call_amount=50，pot_after_call=150，odds=50/150=33%，required_equity=33%
- 即：**只要我的胜率 ≥ 33%，跟注 EV ≥ 0**（不计算位置 + 翻牌后的隐含赔率）

### 1.3 位置评估

**6-max No-Limit Hold'em 位置映射**（按 button 顺时针）：

```
        Button(BTN)
       /            \
    SB ←            ← BB
    |                |
   UTG ← MP ← CO ← BTN
```

| 相对 button 偏移 | 位置标签 | 行动顺序（preflop） |
|---|---|---|
| +0 | BTN | 5 |
| +1 | SB | 0 |
| +2 | BB | 1 |
| +3 | UTG | 2 |
| +4 | MP | 3 |
| +5 | CO | 4 |

**v1.0 简化**：直接返回 label（`UTG/MP/CO/BTN/SB/BB`），不输出行动顺序号；
LLM 在 prompt 中阅读「你是 CO 位」即可推断自己行动顺序（CO 倒数第二个 preflop 行动）。

### 1.4 虚张频率（Bluff Frequency）

**算法**：基于对手历史弃牌率反推

**输入**：`opponentFoldRate`（0.0-1.0，最近 N 手牌对手在押注轮中弃牌的比例）

**映射**：

| 弃牌率 | 建议 Bluff 频率 | 理由 |
|---|---|---|
| ≥ 70% | 35% | 高弃牌率对手 — 用「半诈唬」+「空气牌偷」高频压制 |
| 50-70% | 25% | 中高弃牌率 — 标准偷盲 |
| 30-50% | 15% | 中性 — 偶尔偷盲 |
| 10-30% | 8% | 低弃牌率 — 黏池对手，少偷 |
| ≤ 10% | 3% | 极黏池 — 几乎不偷 |

**说明**：Bluff 频率**仅**用于系统 prompt 的「决策策略」段，**LLM 最终决定**
是否在当前手牌使用 bluff（结合自己底牌 + 公共牌）。Bluff Frequency > 35% 容易「被读穿」，
频率 < 3% 失去娱乐性，**平衡点**在 15-25% 区间。

## 2. 数据结构

### 2.1 Card → wire 字段

沿用 `texasholdem.Card{Rank int, Suit int}`（Rank 2-14, Suit 1-4）。

### 2.2 HandRecord

`ServerGo/agent/thptypes/record.go`：

```go
type HandRecord struct {
    HandNumber  int
    MySeat      int
    MyHole      [2]texasholdem.Card
    Community   [5]texasholdem.Card
    Winners     []int
    NetChipDelta int       // 本手净盈亏（+N 或 -N）
    MyFinalRank texasholdem.HandRank
    Actions     []ActionRecord  // 本手所有动作
}

type ActionRecord struct {
    Seat       int
    ActionType string  // "fold"/"check"/"call"/"bet"/"raise"/"allin"
    Amount     int
    Pot        int     // 动作后底池
    Street     string  // "preflop"/"flop"/"turn"/"river"
}
```

### 2.3 BotHandStrength（缓存键）

`ServerGo/agent/thpagent/decision.go`：

```go
type handStrengthKey struct {
    hole [2]texasholdem.Card
    community [5]texasholdem.Card   // 未亮部分用 Card{} 占位
    nShown int                      // 0..5,实际亮出的张数
}

var handStrengthCache sync.Map      // handStrengthKey → float64
```

**LRU 容量**：同一局德州扑克房间内最多 ~100 个唯一 key（C(13,2) = 78 种起手牌 × 翻牌/转牌/河牌组合 ≈ 数千），用 sync.Map 即可，不引入额外 LRU 库。

## 3. 测试基准（v1.0 必跑）

| 测试 ID | 场景 | 期望 | 误差允许 |
|---|---|---|---|
| HS-01 | AA vs KK preflop 胜率 | 80% | ±3% |
| HS-02 | 72o vs 随机胜率 | 33% | ±3% |
| HS-03 | 同花听牌（4 outs）vs 完成两对 | 19% | ±3% |
| HS-04 | A♠K♠ vs 7♥2♦ preflop | 65% | ±3% |
| HS-05 | 公共牌完成同花 vs 公共牌完成顺子 | 40% | ±3% |

| 测试 ID | 场景 | 期望 |
|---|---|---|
| PO-01 | 底池 100, 对手下注 50 → odds=33% | == |
| PO-02 | 底池 50, 对手下注 50 → odds=50% | == |
| PO-03 | 我已下注 50, 底池 200, 对手下注 100 → call_amount=50, odds=20% | == |

| 测试 ID | 场景 | 期望位置 |
|---|---|---|
| POS-01 | button=0, seat=0 | BTN |
| POS-02 | button=0, seat=1 | SB |
| POS-03 | button=0, seat=3 | UTG |

| 测试 ID | 场景 | 期望 bluff_freq |
|---|---|---|
| BF-01 | 对手弃牌率 75% | 0.35 |
| BF-02 | 对手弃牌率 50% | 0.25 |
| BF-03 | 对手弃牌率 20% | 0.08 |
| BF-04 | 对手弃牌率 5% | 0.03 |

## 4. 性能与缓存策略

| 函数 | 单次耗时 | 调用频度（一手牌） | 缓存 |
|---|---|---|---|
| handStrength | 1.0s (1000 次蒙特卡洛) | 1 次（决策前） | sync.Map (key = handStrengthKey) |
| potOdds | <0.1ms | 1 次 | 不需缓存 |
| position | <0.1ms | 1 次 | 不需缓存 |
| bluffFrequency | <0.1ms | 1 次 | 不需缓存（按对手每次新查 DB） |

**一手牌决策总开销**：约 1.5s（蒙特卡洛 1s + LLM 调用 0.5s + 其他 0.05s），落在 30s 决策时限内，
**无锁竞争**（每个 bot goroutine 独立）。

## 5. 蒙特卡洛实现要点

**采样 PRNG**：

```go
// 沿用 crypto/rand 而不是 math/rand — 扑克抽样的均匀分布很重要
import "crypto/rand"

func sampleN(n int, exclude []texasholdem.Card) []texasholdem.Card {
    deck := fullDeck()
    for _, c := range exclude {
        removeFromDeck(deck, c)
    }
    // Fisher-Yates with crypto/rand
    for i := len(deck) - 1; i > 0; i-- {
        j, _ := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
        deck[i], deck[j] = deck[j], deck[i]
    }
    return deck[:n]
}
```

**early termination**：当 handStrength 已确定 = 1.0（皇家同花顺已凑齐）直接返回，
节省大量计算。

## 6. 不实现的部分（v1.0 边界）

- ❌ 完整 `C(47,5) = 1,533,939` 组合遍历（20s CPU 太慢，不必要）
- ❌ ICM 独立筹码模型（v1.2 锦标赛模式）
- ❌ 翻牌后 SPR（Stack-to-Pot Ratio）建议
- ❌ 隐含赔率（Implied Odds）建议（LLM 在 prompt 中自行判断）
- ❌ 反向隐含赔率（Reverse Implied Odds）建议