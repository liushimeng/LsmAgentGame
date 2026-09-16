# 财商流游戏 · P1 扩展：明斯基金融不稳定引擎 + LPR 重定价与提前还款

> **版本**：v1 ｜ **日期**：2026-09-16 ｜ **game_kind**: `wealth`
> 本文件是 P1 阶段的**实现契约**——明斯基三阶段融资/时刻 + LPR 年度重定价/提前还款决策。
> 上游设计来源：《财商流游戏设计优化与机制增补_v2.60》§4.4(N11-4)/§4.5(N11-5)/§5.3(N12-3)/§5.5(N12-5)。
> 实现以本文件为唯一事实来源；与 P0 契约冲突时以本文件为准（P0 已有字段以 view.go 现状为准）。

---

## 1. 设计目标

财商流游戏要「完全模拟真实世界的财富流动」。P0 已实现央行货币传导、四周期市场、信贷约束。P1 本次新增**两项真实金融机制**：

| 机制 | 来源 | 核心教育价值 |
|------|------|------------|
| **明斯基三阶段 + 明斯基时刻** | v2.60 N11-4/N11-5 | 系统性风险微观基础：个体杠杆决策 → 集体崩盘（2008 金融危机原理）|
| **LPR 重定价 + 提前还款** | v2.60 N12-3/N12-5 | 中国利率市场化核心机制：房贷利率不是固定的、机会成本决策（2023 提前还款潮）|

两项机制**互相联动**：明斯基时刻触发时，所有杠杆资产价格暴跌，持有高 LPR 房贷的玩家「资不抵债」触发破产；玩家可通过「提前还款」降低杠杆、规避明斯基清算。

---

## 2. 明斯基三阶段融资引擎（N11-4 + N11-5）

### 2.1 概念映射

明斯基「金融不稳定假说」把融资分为三级（v2.60 N11-4）：

| 等级 | 收入 vs 债务 | 游戏内映射 | 效果 |
|------|-------------|-----------|------|
| **Hedge 对冲性** | 月收入 > 月供 + 到期本金 | 月供 ≤ 月收入 × 40% | 正常利率 |
| **Speculative 投机性** | 月收入 ≈ 利息，依赖再融资 | 月供 占收入 40%-70% | 利率 +0.5%，可滚动借新还旧 |
| **Ponzi 庞氏性** | 月收入 < 利息，必须借更多 | 月供 占收入 > 70% | 利率优惠 -0.5%，强制平仓风险 |

> ⚠️ 简化假设：游戏内所有贷款均为等额本息，故「月供/月收入」是明斯基分级的唯一判据。

### 2.2 数据结构（player.go）

```go
// MinskyTier 明斯基融资等级。
type MinskyTier string

const (
    MinskyHedge      MinskyTier = "hedge"      // 对冲性
    MinskySpeculative MinskyTier = "speculative" // 投机性
    MinskyPonzi      MinskyTier = "ponzi"      // 庞氏性
)

// MinskyStatus 单笔贷款的明斯基分级状态（含触发源）。
type MinskyStatus struct {
    Tier       MinskyTier `json:"tier"`        // hedge/speculative/ponzi
    DebtToIncome float64  `json:"debt_to_income"` // 月供/月收入(0-1+)
    IsRolling    bool     `json:"is_rolling"`    // 投机/庞氏是否已滚动借新还旧
}

// Player 新增字段（player.go 结构体追加）:
//   MinskyByLoan   map[string]*MinskyStatus  // loanID -> 分级
//   MinskyMomentTriggered bool                 // 本回合明斯基时刻是否已触发
```

### 2.3 分级算法（新增 `minsky.go`）

```go
// ClassifyMinsky 根据贷款月供与借款人月收入划分明斯基等级。
// 参数: monthlyPayment(月供), monthlyIncome(月收入+配偶+副业), rolling(是否滚动借新还旧)。
func ClassifyMinsky(monthlyPayment, monthlyIncome int64, rolling bool) MinskyTier {
    if monthlyIncome <= 0 {
        return MinskyPonzi
    }
    ratio := float64(monthlyPayment) / float64(monthlyIncome)
    switch {
    case ratio <= 0.40:
        return MinskyHedge
    case ratio <= 0.70:
        return MinskySpeculative
    default:
        return MinskyPonzi
    }
}

// MinskyRateAdjustment 明斯基等级利率调整(小数)。
func MinskyRateAdjustment(tier MinskyTier) float64 {
    switch tier {
    case MinskyHedge:
        return 0
    case MinskySpeculative:
        return 0.005 // +0.5%
    case MinskyPonzi:
        return -0.005 // 市场奖励高风险 -0.5%（诱人陷阱）
    }
    return 0
}
```

**调用时机**：在 `actTakeLoan` 发放贷款时立即分级并写入 `MinskyByLoan`；月结时重算（工资/收入可能变化）。

### 2.4 明斯基利率加点发放（actions.go actTakeLoan）

在现有 `actTakeLoan` 末尾追加：

```go
tier := ClassifyMinsky(loan.MonthlyPayment, p.monthlyIncomeEstimate(), false)
p.MinskyByLoan[loan.ID] = &MinskyStatus{
    Tier:         tier,
    DebtToIncome: float64(loan.MonthlyPayment) / max1(float64(p.monthlyIncomeEstimate())),
    IsRolling:    false,
}
// 庞氏等级:月供实际减免 0.5%(陷阱)。
if tier == MinskyPonzi {
    loan.AnnualRate = max(0.001, loan.AnnualRate-0.005)
    // 重算月供
    loan.MonthlyPayment = computeMonthlyPayment(loan.Principal, loan.AnnualRate, loan.TermN)
}
```

### 2.5 明斯基时刻触发（settlement.go SettleMonth）

在月结 ③ 与 ④ 之间（市场漂移之前）插入明斯基时刻判定：

```go
// ⑥ 明斯基时刻判定（v2.60 N11-5）。
if ponziCount := w.minskyPonziCount(); ponziCount > 0 {
    ponziRatio := float64(ponziCount) / float64(len(w.alivePlayers()))
    if ponziRatio > 0.30 && !w.MinskyMomentCooldown {
        w.triggerMinskyMoment(res)
    }
}
```

**triggerMinskyMoment** 实现（新增 `minsky.go`）：

```go
// triggerMinskyMoment 触发明斯基时刻（v2.60 N11-5）。
// 效果：
//   - 所有杠杆资产（stock_index/side_business 持仓）立即 -50%
//   - 庞氏玩家强制平仓所有杠杆资产，损失 100% 头寸
//   - 投机玩家损失 50% 杠杆头寸
//   - 对冲玩家不受影响
//   - 触发后进入 12 月冷却期（避免连续触发）
func (w *World) triggerMinskyMoment(res *SettleResult) {
    w.MinskyMomentCooldown = 12
    w.MinskyMomentCount++
    w.emitEvent("minsky", -1, fmt.Sprintf(
        "🚨 明斯基时刻！庞氏玩家占比 %.0f%% > 30%%，全场杠杆资产价格腰斩！",
        float64(w.minskyPonziCount())/float64(len(w.alivePlayers()))*100))

    multiplier := 0.5 // 资产价格乘数
    for seat, p := range w.Players {
        if !p.Alive {
            continue
        }
        tier := w.playerDominantMinskyTier(p)
        switch tier {
        case MinskyPonzi:
            // 强制平仓所有杠杆资产
            w.forceLiquidate(p, seat, 1.0) // 100% 头寸
        case MinskySpeculative:
            w.forceLiquidate(p, seat, 0.5) // 50% 头寸
        // Hedge 不受影响
        }
        _ = multiplier
    }
    // 市场指数立即 -50%
    w.Market.StockIndex *= 0.5
    for _, d := range DistrictDefs {
        w.Market.DistrictIdx[d.ID] *= 0.5
    }
}

// forceLiquidate 强制变现指定比例杠杆资产。
func (w *World) forceLiquidate(p *Player, seat int, ratio float64) {
    leveragedKinds := []string{AssetStockIndex, AssetSideBusiness}
    if ratio >= 1.0 {
        leveragedKinds = append(leveragedKinds, "house:") // 庞氏清仓房产
    }
    newAssets := p.Assets[:0]
    for i := range p.Assets {
        a := p.Assets[i]
        if shouldLiquidate(a, leveragedKinds) {
            sellUnits := a.Units * ratio
            a.Units -= sellUnits
            proceeds := int64(float64(AssetValue(&a, w.Market)) * ratio)
            w.Pay(p, seat, proceeds, "minsky_liquidation")
        }
        if a.Units > 0.001 {
            newAssets = append(newAssets, a)
        }
    }
    p.Assets = newAssets
}

// playerDominantMinskyTier 返回玩家最差的明斯基等级。
func (w *World) playerDominantMinskyTier(p *Player) MinskyTier {
    worst := MinskyHedge
    for _, ms := range p.MinskyByLoan {
        if ms == nil { continue }
        if ms.Tier == MinskyPonzi {
            return MinskyPonzi
        }
        if ms.Tier == MinskySpeculative {
            worst = MinskySpeculative
        }
    }
    return worst
}

// minskyPonziCount 统计庞氏玩家数。
func (w *World) minskyPonziCount() int {
    n := 0
    for _, p := range w.Players {
        if !p.Alive { continue }
        if w.playerDominantMinskyTier(p) == MinskyPonzi {
            n++
        }
    }
    return n
}
```

### 2.6 World 结构体新增字段（room.go / engine.go）

```go
// World 新增:
MinskyMomentCooldown  int  // 明斯基时刻冷却剩余月(触发后置 12)
MinskyMomentCount     int  // 累计触发次数(展示/评分用)
```

### 2.7 视图下发（view.go）

```go
// CycleJSON 追加:
MinskyMomentCount int    `json:"minsky_moment_count,omitempty"`
PonziCount        int    `json:"ponzi_count"`
PonziRatio        float64 `json:"ponzi_ratio"`
// PlayerState 追加:
MinskyTier        string  `json:"minsky_tier"` // hedge/speculative/ponzi
DebtToIncome      float64 `json:"debt_to_income"`
```

### 2.8 房间级统计广播

在 `BuildRoomState` / `BuildClientState` 追加明斯基概览字段：

```go
// GameState 追加:
MinskyOverview struct {
    PonziCount   int     `json:"ponzi_count"`
    SpecCount    int     `json:"spec_count"`
    HedgeCount   int     `json:"hedge_count"`
    PonziRatio   float64 `json:"ponzy_ratio"`
    CooldownLeft int     `json:"cooldown_left"`
} `json:"minsky_overview"`
```

---

## 3. LPR 年度重定价引擎（N12-3）

### 3.1 机制

中国房贷利率 = 5Y LPR + 加点（加点锁定）。每年 1 月按最新 5Y LPR 重算月供（v2.60 N12-3）。

**现有状态**：P0 房贷利率 = LPR + 0.5% + CreditMarkup，但 LPR 随央行月度决策变化后，已发放贷款月供不变（因为月供在发放时锁定）。

**P1 改动**：每年 1 月（month % 12 == 1）对所有 `Kind == LoanMortgage` 的贷款按「最新 5Y LPR + 原加点 + 原信用加点」重算剩余月供。

### 3.2 数据结构

Loan 结构体无需新增字段。需要记录「原始加点」（发放时的 LPR 锁定值用于计算加点）：

```go
// Loan 结构体追加:
OrigSpread    float64 `json:"-"` // 发放时锁定加点(5Y_LPR 差值,用于 LPR 重定价时保留加点)
RateFixed     bool    `json:"-"` // true = 固定利率(P0 遗留兼容,false = 浮动 LPR)
```

> ⚠️ 兼容性：P0 已发放的贷款 `OrigSpread = 0`（默认固定利率兼容）。P1 及之后发放的贷款按浮动 LPR 处理。

### 3.3 重定价逻辑（新增 `lpr_reprice.go`）

```go
// RepriceMortgageLPR 年度 1 月 LPR 重定价(v2.60 N12-3)。
// 对每个玩家的房贷按最新 5Y LPR 重算月供;返回变更摘要。
func (w *World) RepriceMortgageLPR() []LPRRepriceRecord {
    if w.CB == nil { return nil }
    newLPR5Y := w.CB.ComputeL5Y() // 5Y LPR(央行新增方法)
    var records []LPRRepriceRecord
    for seat, p := range w.Players {
        for i := range p.Loans {
            loan := &p.Loans[i]
            if loan.Kind != LoanMortgage || loan.RateFixed {
                continue
            }
            if loan.MonthsLeft <= 0 { continue }
            oldPayment := loan.MonthlyPayment
            // 新利率 = 最新 5Y LPR + 原始加点(锁定) + 信用加点(按最新信用等级)
            newRate := newLPR5Y + loan.OrigSpread + p.CreditMarkup()
            // 剩余期数
            remaining := loan.MonthsLeft
            // 重算等额本息月供
            loan.AnnualRate = newRate
            loan.MonthlyPayment = computeAnnuityPayment(loan.Balance, newRate, remaining)
            records = append(records, LPRRepriceRecord{
                Seat:      seat,
                LoanID:    loan.ID,
                OldRate:   loan.AnnualRate,
                NewRate:   newRate,
                OldPayment: oldPayment,
                NewPayment: loan.MonthlyPayment,
                LPR5Y:     newLPR5Y,
            })
        }
    }
    return records
}

// LPRRepriceRecord LPR 重定价变更记录。
type LPRRepriceRecord struct {
    Seat       int
    LoanID     string
    OldRate    float64
    NewRate    float64
    OldPayment int64
    NewPayment int64
    LPR5Y      float64
}

// computeAnnuityPayment 等额本息月供(标准公式)。
func computeAnnuityPayment(principal int64, annualRate float64, months int) int64 {
    if months <= 0 { return 0 }
    if annualRate <= 0 {
        return principal / int64(months)
    }
    r := annualRate / 12
    // M = P * r(1+r)^n / ((1+r)^n - 1)
    factor := math.Pow(1+r, float64(months))
    m := float64(principal) * r * factor / (factor - 1)
    return int64(m + 0.5)
}
```

### 3.4 调用时机（settlement.go AnnualAdjust 首行插入）

```go
// AnnualAdjust 追加 LPR 重定价(每年 1 月)。
func (w *World) AnnualAdjust() {
    // ① LPR 重定价(v2.60 N12-3):年初房贷重算。
    if w.Month > 0 && w.Month%12 == 0 {
        // month%12==0 是 12 月结,重定价在次年 1 月发薪前。
        // 实际触发:月结后 month++,若新 month % 12 == 1 则重定价。
    }
}
```

> ⚠️ 实际触发时机：在 `SettleMonth` 的 `w.Month++` 后检查 `if w.Month%12 == 1 { w.RepriceMortgageLPR() }`。这与央行年初调息节奏一致。

### 3.5 央行 5Y LPR 方法（central_bank.go 追加）

```go
// ComputeL5Y 计算 5Y LPR = 1Y LPR + 期限溢价 + CPI 调整。
func (cb *CentralBankState) ComputeL5Y() float64 {
    base := cb.PolicyRate + TermPremium
    // CPI 高于目标时 5Y 加点更高
    if cb.M2 > 0 && cb.LastCPI > TargetCPI {
        base += (cb.LastCPI - TargetCPI) * 0.5
    }
    return base
}
```

### 3.6 发放贷款时锁定加点（actions.go actTakeLoan 房贷分支）

```go
// 在 actTakeLoan mortgage 分支追加:
loan.OrigSpread = rate - w.Market.Params().LPR // 锁定加点 = 总利率 - 发放时 LPR
loan.RateFixed = false                          // P1 起房贷按浮动 LPR
```

---

## 4. 提前还款决策（N12-5）

### 4.1 触发条件（v2.60 N12-5）

当玩家持有房贷且满足以下条件时，月度弹窗提示「提前还款决策」：
- 房贷利率（年化）> 5% 且 理财收益率 < 3%
- 或：玩家现金 > 房贷余额 × 2（流动性过剩）

### 4.2 新动作 `early_repay`（actions.go）

```go
// actEarlyRepay 提前还款(v2.60 N12-5)。
// 参数: loan_id, amount_cny("all"=全部, 数字=部分), penalty_waived(是否免除违约金,默认 false)
func (w *World) actEarlyRepay(p *Player, a Action) (string, *errcode.Error) {
    loanID := a.LoanID
    amount := a.AmountCNY
    loan := p.loanByID(loanID)
    if loan == nil {
        return "", errcode.New(35013, "贷款不存在")
    }
    if loan.Kind != LoanMortgage {
        return "", errcode.New(35014, "提前还款仅支持房贷")
    }
    // 1 年内提前还款罚息 1-3%
    monthsSinceOpen := loan.TermN - loan.MonthsLeft
    var penaltyRate float64 = 0
    if monthsSinceOpen < 12 {
        penaltyRate = 0.01 + float64(12-monthsSinceOpen)*0.00167 // 1%→3% 线性
    }
    // 还款金额
    var payAmount int64
    if amount <= 0 || amount >= loan.Balance {
        payAmount = loan.Balance // 全部还清
    } else {
        payAmount = amount
    }
    penalty := int64(float64(payAmount) * penaltyRate)
    totalPay := payAmount + penalty
    if p.Cash < totalPay {
        return "", errcode.New(35015, fmt.Sprintf("现金不足(需 %d 万元)", totalPay/10000))
    }
    w.Pay(p, p.Seat, -totalPay, "early_repay")
    loan.Balance -= payAmount
    if loan.Balance <= 0 {
        // 还清
        p.removeLoan(loanID)
        return fmt.Sprintf("🎉 房贷已结清！节省利息 %d 万元",
            w.estimateSavedInterest(loan)/10000), nil
    }
    return fmt.Sprintf("提前还款 %d 万元，违约金 %d 元，剩余贷款余额 %d 万元",
        payAmount/10000, penalty/10000, loan.Balance/10000), nil
}
```

### 4.3 视图层展示

前端 ActionPanel 当检测到 `early_repay_eligible == true` 时，弹出提前还款决策提示：
- 「您的房贷利率 X.X% 高于理财收益率 X.X%，建议提前还款」
- 三个按钮：全额还清 / 部分还清（50%）/ 维持不变

### 4.4 错误码追加（errcode/errcode.go）

```go
35013 ErrLoanNotFound        "贷款不存在"
35014 ErrEarlyRepayOnlyMortgage "提前还款仅支持房贷"
35015 ErrCashNotEnoughRepay  "现金不足以提前还款"
```

---

## 5. Agent Bot 行为适配（agent_runner.go + wealthplayer tools）

### 5.1 新增 Bot 工具

```go
// wealthplayer/tools.go 追加:
"early_repay"    ToolDef // 提前还款
"query_minsky"   ToolDef // 查询明斯基状态/风险
```

### 5.2 Bot 决策逻辑

在 Agent prompt 追加明斯基感知：
```
## 明斯基风险
- 当前庞氏玩家占比：{ponzi_ratio}%
- 您的融资等级：{minsky_tier}（hedge/speculative/ponzi）
- 若庞氏占比 > 30%，明斯基时刻将触发，杠杆资产价格腰斩
- 庞氏等级玩家会被强制平仓所有杠杆资产
- 建议：当您的月供超过月收入 70%，优先提前还款或变现资产降低杠杆

## LPR 重定价
- 每年 1 月房贷利率按最新 5Y LPR 重算
- 若理财收益率 < 房贷利率，建议提前还款减少利息支出
```

### 5.3 Bot 工具实现

```go
// QueryMinsky 查询明斯基全局状态(Agent 工具)。
func (a *AgentRunner) QueryMinsky(seat int) (string, error) {
    // 返回房间明斯基概览:庞氏/投机/对冲玩家数、占比、冷却剩余
}

// EarlyRepay 提前还款(Agent 工具)。
func (a *AgentRunner) EarlyRepay(seat int, loanID string, amountCNY int64) error {
    return a.apply(seat, wealthplayer.ToolEarlyRepay, "", func() (string, error) {
        return a.r.World().ApplyAction(seat, Action{
            Kind:     ActionEarlyRepay,
            LoanID:   loanID,
            AmountCNY: amountCNY,
        })
    })
}
```

---

## 6. 前端 UI 适配

### 6.1 ActionPanel.tsx 新增

- 明斯基状态条（顶部提示条，颜色编码：绿 hedge/黄 spec/红 ponzi）
- 提前还款弹窗（条件触发时弹出）
- 明斯基时刻全屏动画（triggerMinskyMoment 时 3 秒闪烁红屏）

### 6.2 CyclePanel 追加

- 明斯基概览仪表盘（ponzi_ratio 温度计 + 倒计时）
- LPR 历史折线图（已有 LPR，加 5Y LPR 双线）

### 6.3 i18n 键追加

zh-CN:
```json
"minsky.hedge": "对冲性融资",
"minsky.speculative": "投机性融资",
"minsky.ponzi": "庞氏性融资",
"minsky.moment": "明斯基时刻！杠杆资产价格腰斩",
"minsky.cooldown": "距离下次明斯基时刻判定还有 {n} 月",
"earlyrepay.title": "提前还款决策",
"earlyrepay.trigger": "房贷利率 {rate}% 高于理财收益 {yield}%",
"earlyrepay.full": "全额还清",
"earlyrepay.partial": "部分还款(50%)",
"earlyrepay.hold": "维持不变",
"lpr.reprice_notice": "LPR 重定价：您的房贷月供从 {old} 元调整为 {new} 元"
```

---

## 7. 文件变更清单

| 文件 | 变更类型 | 内容 |
|------|---------|------|
| `ServerGo/game/wealth/minsky.go` | **新建** | 明斯基分级算法 + 时刻触发 + 清算 |
| `ServerGo/game/wealth/lpr_reprice.go` | **新建** | LPR 年度重定价 + 等额本息计算 |
| `ServerGo/game/wealth/player.go` | 修改 | MinskyByLoan / MinskyMomentTriggered 字段 |
| `ServerGo/game/wealth/actions.go` | 修改 | actTakeLoan 加点锁定 + actEarlyRepay 新增 |
| `ServerGo/game/wealth/settlement.go` | 修改 | 月结插入明斯基判定 + 年初 LPR 重定价 |
| `ServerGo/game/wealth/central_bank.go` | 修改 | ComputeL5Y 方法 |
| `ServerGo/game/wealth/view.go` | 修改 | 明斯基字段下发 |
| `ServerGo/game/wealth/room.go` | 修改 | World 新增字段 |
| `ServerGo/game/wealth/agent_runner.go` | 修改 | QueryMinsky/EarlyRepay 工具 |
| `ServerGo/agent/wealthplayer/tools.go` | 修改 | 工具定义追加 |
| `ServerGo/agent/wealthplayer/prompt.go` | 修改 | prompt 模板追加明斯基感知 |
| `ServerGo/errcode/errcode.go` | 修改 | 35013-35015 |
| `ClientWeb/src/components/wealth/MinskyPanel.tsx` | **新建** | 明斯基状态仪表盘 |
| `ClientWeb/src/components/wealth/EarlyRepayModal.tsx` | **新建** | 提前还款弹窗 |
| `ClientWeb/src/components/wealth/ActionPanel.tsx` | 修改 | 明斯基提示 + 还款触发 |
| `ClientWeb/src/types/wealth.ts` | 修改 | 明斯基类型 |
| `ClientWeb/src/styles/wealth.css` | 修改 | 明斯基红色闪烁动画 |
| `ClientWeb/src/i18n/*.json` | 修改 | 中文/英文/日文键 |
| `ServerGo/game/wealth/minsky_test.go` | **新建** | 明斯基单测 |
| `ServerGo/game/wealth/lpr_reprice_test.go` | **新建** | LPR 重定价单测 |

---

## 8. 测试验收

### 8.1 单元测试

- `minsky_test.go`：
  - ClassifyMinsky(3000, 10000, false) == Hedge
  - ClassifyMinsky(5000, 10000, false) == Speculative
  - ClassifyMinsky(8000, 10000, false) == Ponzi
  - triggerMinskyMoment：庞氏占比 > 30% 触发后，庞氏玩家杠杆资产归零
  - triggerMinskyMoment 冷却：触发后 12 月内不重复触发
- `lpr_reprice_test.go`：
  - computeAnnuityPayment(1000000, 0.04, 360) ≈ 4774
  - RepriceMortgageLPR：LPR 上调后月供增加
  - RateFixed 贷款不参与重定价

### 8.2 编译门禁

- `go build -o LsmAgentGame main.go` 通过
- `go test ./...` 通过（含新增测试）
- 前端 `tsc --noEmit` 通过
- `npm run build` 通过

### 8.3 运行时验收

1. 启动 10 Agent 房间，观察：
   - 贷款发放时日志显示明斯基分级
   - 月结时日志显示明斯基占比
   - 当庞氏玩家 > 30%，触发明斯基时刻事件 + 红屏动画
   - 每年 1 月 LPR 重定价日志
   - Agent 主动发起 early_repay 动作

---

## 9. 实现优先级与 SubAgent 分工

| 阶段 | 工作面 | SubAgent | 产出 |
|------|--------|---------|------|
| 1 | 后端纯引擎 | backend-dev | minsky.go / lpr_reprice.go + 单测 |
| 2 | 后端集成 | backend-dev | actions.go / settlement.go / central_bank.go / room.go / view.go 修改 |
| 3 | Agent 适配 | backend-dev | agent_runner.go / tools.go / prompt.go 修改 |
| 4 | 前端 UI | frontend-dev | MinskyPanel.tsx / EarlyRepayModal.tsx / ActionPanel.tsx 修改 |
| 5 | 协议+错误码 | backend-dev | errcode.go + WS 帧文档 |
| 6 | 验收 | integration-tester | 编译 + 单测 + 端到端 |

阶段 1/2/3 可合并为单一 backend-dev 任务；阶段 4 独立 frontend-dev；阶段 5 串行在 2 后。

---

## 10. 设计取舍

1. **明斯基分级只用月供/收入比**：现实还应看资产端流动性，但游戏简化后仍保留核心教学价值。
2. **明斯基时刻资产腰斩 -50%**：与 v2.60 设计一致；实际现实危机跌幅不一，-50% 是教学显著性取值。
3. **庞氏利率优惠 -0.5%**：模拟「市场奖励高风险」（次贷低息引诱）；游戏中是陷阱。
4. **提前还款违约金 1-3%**：按真实银行规则线性化；1 年内 1%，1 月内 3%。
5. **LPR 重定价仅影响新增贷款**：P0 已发放房贷保持向后兼容（RateFixed=true）。

---

## 11. 与既有设计/实现的兼容性

| 既有契约 | 处理方式 |
|---------|---------|
| P0 `Loan` 结构体 | 追加 OrigSpread/RateFixed 字段，默认值向后兼容 |
| P0 `World` 结构体 | 追加 MinskyMomentCooldown/Count 字段 |
| P0 `Player` 结构体 | 追加 MinskyByLoan/MinskyMomentTriggered |
| P0 `actTakeLoan` 逻辑 | 尾部追加分级，不改变既有分支 |
| P0 `SettleMonth` 时序 | 在 ③ 与 ④ 之间插入明斯基判定（不影响既有 7 步）|
| P0 `AnnualAdjust` | 首行追加 LPR 重定价 |
| P0 央行引擎 | 追加 ComputeL5Y 方法，不改变既有 ComputeLPR |
| P0 前端 CyclePanel | 追加明斯基仪表盘，不改变既有周期渲染 |
| 错误码段 35001-35012 | 追加 35013-35015（wealth 专用段扩展）|

---

## 12. 维护规约

1. 本文件与代码同步演进：任何实现偏差先改本文档再改码（或同提交双改）。
2. 新增代码文件 ≤ 1800 行（CLAUDE.md §4）；新增 Markdown ≤ 800 行（§3）。
3. 数值引用格式：v2.60 §x ｜ P1 新定（本文档 §10 标注）。
4. 每次触发明斯基时刻必须 emit `minsky` 事件，前端据此渲染红屏动画。

---

## 13. 更新日志

### v1（2026-09-16）
- 初版：明斯基三阶段 + 明斯基时刻 + LPR 重定价 + 提前还款决策。
