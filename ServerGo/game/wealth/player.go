// Package wealth — player.go: 玩家状态 + 资产/负债科目 + 三表推导(2026-09-14 §财商流P0)。
//
// 结构契约: 后端架构文档 §9.1。净资产/FI 等推导只读函数在此;
// 月结(settlement.go)与动作(actions.go)负责状态变更。
package wealth

import (
	"fmt"

	"LsmAgentGame/game/wealth/profession"
)

// 资产 kind 常量(后端架构 §6)。
const (
	AssetStockIndex   = "stock_index"
	AssetBond         = "bond"
	AssetGold         = "gold"
	AssetSideBusiness = "side_business"
	AssetPension      = "pension"
	// AssetHouse / AssetShop 形如 "house:<district>" / "shop:<district>"。
)

// 房产持有上限(P0 新定)。
const (
	MaxHouses = 4
	MaxShops  = 4
)

// AssetKindHouse / AssetKindShop 拼装/解包 "house:<d>" / "shop:<d>"。
func AssetKindHouse(districtID string) string { return "house:" + districtID }
func AssetKindShop(districtID string) string  { return "shop:" + districtID }

// Asset 是单笔持仓(后端架构 §6)。
type Asset struct {
	Kind      string         // stock_index|bond|gold|house:<d>|shop:<d>|side_business|pension
	Units     float64        // 份/克/套/间(side_business=1)
	CostCNY   int64          // 买入总成本
	OpenMonth int            // 开仓月(满 5 年判定用)
	Extra     map[string]any // bond:{"rate":float64}; house:{"self_occupied":bool,"district":string}
}

// AssetRate 返回债券锁定年化(非债券返回 0)。
func (a *Asset) AssetRate() float64 {
	if r, ok := a.Extra["rate"].(float64); ok {
		return r
	}
	return 0
}

// AssetDistrict 返回 house/shop 的城区 id。
func (a *Asset) AssetDistrict() string {
	if s, ok := a.Extra["district"].(string); ok {
		return s
	}
	return ""
}

// IsSelfOccupied 自住房标记。
func (a *Asset) IsSelfOccupied() bool {
	if v, ok := a.Extra["self_occupied"].(bool); ok {
		return v
	}
	return false
}

// SetSelfOccupied 设置自住标记。
func (a *Asset) SetSelfOccupied(v bool) {
	if a.Extra == nil {
		a.Extra = map[string]any{}
	}
	a.Extra["self_occupied"] = v
}

// 贷款 kind(后端架构 §7.1)。
const (
	LoanMortgage   = "mortgage"     // 房贷 LPR+0.5%,360 期等额本息
	LoanConsumer   = "consumer"     // 消费贷 10% 年化,36 期等额本息
	LoanCreditT1   = "credit_tier1" // 信用贷 tier1 月息 0.8%,到期一次性还本
	LoanCreditT2   = "credit_tier2" // tier2 月息 1.2%
	LoanCreditT3   = "credit_tier3" // tier3 月息 1.8%
	LoanBusiness   = "business"     // 经营贷 LPR+2%,60 期先息后本
)

// Loan 是单笔负债。
type Loan struct {
	ID             string  // "L1"…
	Kind           string
	Principal      int64   // 放款本金
	Balance        int64   // 当前余额
	AnnualRate     float64 // 年化(消费贷/房贷随信用等级加点;信用贷按月息折算年化)
	MonthlyPayment int64   // 等额本息月供;先息后本 = 月息;到期还本 = 月息
	TermN          int     // 总期数
	MonthsLeft     int     // 剩余期数
	InterestOnly   bool    // true = 先息后本/到期还本(月付息,期末还本)
	LumpAtMaturity bool    // true = 信用贷(到期一次性还本)
	FreeInterest   bool    // true = 破产重组 36 期免息分期(Balance 按期递减)
	// P1: LPR 重定价用(v2.60 N12-3)。
	OrigSpread float64 // 发放时锁定加点(= 总利率 − 发放时 LPR),P0 遗留贷款默认 0
	RateFixed  bool    // true = 固定利率(P0 遗留兼容,false = 浮动 LPR 参与重定价)
}

// SideBusiness 副业状态。
type SideBusiness struct {
	Kind         string  // delivery|content|tutoring|freelance
	BaseIncome   int64   // 档位基准(展示)
	OpenedMonth  int
}

// FlowItem 月结损益明细单行(my.monthly.detail)。
type FlowItem struct {
	Key       string `json:"key"`
	AmountCNY int64  `json:"amount_cny"` // 正=收入,负=支出
	Text      string `json:"text"`
}

// MonthlyResult 最近一次月结快照(后端架构 §9.1)。
type MonthlyResult struct {
	Income        int64
	Expense       int64
	Net           int64
	Tax           int64
	Social        int64
	PassiveIncome int64
	SideIncome    int64
	OvertimeBonus int64
	Detail        []FlowItem
}

// Family 家庭状态。
type Family struct {
	Marital      string // single | married
	SpouseIncome int64  // 税后月入(结婚事件置 6000)
	Children     int
}

// Player 单座位玩家全量状态(后端架构 §9.1)。
type Player struct {
	Seat     int
	UserID   string
	IsBot    bool
	ModelKey string
	Nickname string // 展示名(bot 为 model 显示名)

	Card profession.Card // 职业卡(结构见 profession/card.go;引擎内仅读字段)

	Age int // 个人年龄(展示;主时钟在 World)

	Cash int64

	SavingsDeposit int64 // 定期存款(M2 但非 M1),利率 1.5%/年,提前支取损失全部利息(P1)

	Energy    int // E -3..10
	Network   int // N 0..10
	Cognition int // K 0..10

	CreditScore int

	District     string // 当前所在区
	HomeDistrict string // 住房所在区(租房 = 当前区)

	Assets []Asset
	Loans  []Loan

	PensionCNY int64

	Family Family

	UnemployedMonths   int
	RehireSalaryRatio  float64

	SalaryBase   int64 // 当前基准月薪(年增长累积;P16 为波动带中值)
	SalaryLow     int64 // P16 波动带下界(非波动卡 = SalaryBase)
	SalaryHigh    int64 // P16 波动带上界
	SalaryVolatile bool

	SideBusiness *SideBusiness

	OvertimeThisMonth bool // work_overtime 置位,月结发放工资×0.3

	ActionBudget    int
	SpokenThisMonth bool
	Submitted       bool
	Alive           bool
	StoppedMonths   int // 破产停赛剩余月

	DonationTotalCNY int64
	DonationCount    int

	OverdueCount    int
	OnTimeStreak    int // 连续按时还款月数(满 12 期 +20 信用分)
	BankruptCount   int
	MajorIllness    bool
	HadIllnessYear  bool // 当年有大病 → 生日不加精力

	NegativeCashMonths int // 连续现金 < 0 月数(破产触发)
	InReorganization   bool

	// P1: 明斯基金融不稳定引擎(v2.60 N11-4 / N11-5)。
	MinskyByLoan          map[string]*MinskyStatus // loanID -> 分级
	MinskyMomentTriggered bool                     // 本回合明斯基时刻是否已触发(用于 UI 高亮)

	// P1: 真实经济循环(2026-09-16 §财商流P1-2 契约 §3.1)。
	ConsumptionLevel   int                // 0 节俭/1 标准/2 精致/3 奢侈;默认 1(newPlayerFromCard 显式置 1)
	ConsumptionByGoods map[string]float64 // 上月消费结构(元,id→金额;nil=未初始化,视为档位 1)

	Monthly         MonthlyResult
	NetWorthHistory []int64
	LastActionText  string // 本月最近动作(公开字段 last_action)
	StatusIcon      string // working|idle|trading|resting|moved
	Ending          string // 终局结局 id
}

// monthlyActionBudget 每月动作预算(人类/Agent 同规则 3)。
const monthlyActionBudget = 3

// ConsumptionLevelSafe 档位兜底:map 未初始化(nil)或档位越界 → 1(标准)。
// ⚠️ 零值陷阱:0 是合法档位(节俭),不能以零值判默认;存量对局玩家以
// ConsumptionByGoods == nil 判「未初始化」,首次月结按档位 1 处理(§3.1)。
func (p *Player) ConsumptionLevelSafe() int {
	if p.ConsumptionByGoods == nil {
		return 1
	}
	if p.ConsumptionLevel < 0 || p.ConsumptionLevel > 3 {
		return 1
	}
	return p.ConsumptionLevel
}

// houseCount / shopCount 持仓计数。
func (p *Player) houseCount() int {
	n := 0
	for i := range p.Assets {
		if isHouseKind(p.Assets[i].Kind) {
			n++
		}
	}
	return n
}

func (p *Player) shopCount() int {
	n := 0
	for i := range p.Assets {
		if isShopKind(p.Assets[i].Kind) {
			n++
		}
	}
	return n
}

func isHouseKind(kind string) bool { return len(kind) > 6 && kind[:6] == "house:" }
func isShopKind(kind string) bool  { return len(kind) > 5 && kind[:5] == "shop:" }

// assetOf 找到第一笔指定 kind 的持仓。
func (p *Player) assetOf(kind string) *Asset {
	for i := range p.Assets {
		if p.Assets[i].Kind == kind {
			return &p.Assets[i]
		}
	}
	return nil
}

// findAsset 找到第 n 笔(0 起)指定 kind 的持仓。
func (p *Player) findAsset(kind string, n int) *Asset {
	seen := 0
	for i := range p.Assets {
		if p.Assets[i].Kind == kind {
			if seen == n {
				return &p.Assets[i]
			}
			seen++
		}
	}
	return nil
}

// selfOccupiedHouse 返回自住房(至多 1 套;无则 nil)。
func (p *Player) selfOccupiedHouse() *Asset {
	for i := range p.Assets {
		a := &p.Assets[i]
		if isHouseKind(a.Kind) && a.IsSelfOccupied() {
			return a
		}
	}
	return nil
}

// ownsProperty 是否持有房产/商铺(物业费判定)。
func (p *Player) ownsProperty() bool {
	return p.houseCount()+p.shopCount() > 0
}

// loanByID 按id查贷款。
func (p *Player) loanByID(id string) *Loan {
	for i := range p.Loans {
		if p.Loans[i].ID == id {
			return &p.Loans[i]
		}
	}
	return nil
}

// nextLoanID 生成下一个贷款 id("L1","L2",…)。
func (p *Player) nextLoanID() string {
	return fmt.Sprintf("L%d", len(p.Loans)+1)
}

// removeLoan 原地移除指定 id 的贷款(避免内存泄漏)。
func (p *Player) removeLoan(id string) {
	for i := range p.Loans {
		if p.Loans[i].ID == id {
			p.Loans = append(p.Loans[:i], p.Loans[i+1:]...)
			return
		}
	}
}

// brassTier 返回最高未清偿信用贷档位(0=无;1/2/3)。
func (p *Player) brassTier() int {
	tier := 0
	for i := range p.Loans {
		switch p.Loans[i].Kind {
		case LoanCreditT1:
			if tier < 1 {
				tier = 1
			}
		case LoanCreditT2:
			if tier < 2 {
				tier = 2
			}
		case LoanCreditT3:
			if tier < 3 {
				tier = 3
			}
		}
	}
	return tier
}

// BrassSalaryGrowth 工资年增长率改写(后端架构 §7.2):基础 +3%。
func (p *Player) BrassSalaryGrowth() float64 {
	switch p.brassTier() {
	case 1:
		return 0.01
	case 2:
		return 0.0
	case 3:
		return -0.01
	default:
		return 0.03
	}
}

// BrassSideFactor 副业月收入系数(后端架构 §7.2)。
func (p *Player) BrassSideFactor() float64 {
	switch p.brassTier() {
	case 2:
		return 0.8
	case 3:
		return 0.6
	default:
		return 1.0
	}
}

// CreditGrade 信用等级(后端架构 §7.3)。
func (p *Player) CreditGrade() string {
	switch s := p.CreditScore; {
	case s >= 700:
		return "A"
	case s >= 600:
		return "B"
	case s >= 500:
		return "C"
	case s >= 400:
		return "D"
	default:
		return "E"
	}
}

// CreditMarkup 信用等级利率上浮(小数;E 档不可贷款由调用方判定)。
func (p *Player) CreditMarkup() float64 {
	switch p.CreditGrade() {
	case "A":
		return -0.005
	case "B":
		return 0
	case "C":
		return 0.01
	case "D":
		return 0.025
	default:
		return 0.025
	}
}

// monthlyIncomeEstimate 月收入估计(消费贷额度上限用):工资基准 + 配偶 + 副业基准。
func (p *Player) monthlyIncomeEstimate() int64 {
	inc := p.SalaryBase
	if p.UnemployedMonths > 0 {
		inc = 0
	}
	inc += p.Family.SpouseIncome
	if p.SideBusiness != nil {
		inc += p.SideBusiness.BaseIncome
	}
	return inc
}

// AssetValue 单笔资产当前市值(后端架构 §6 净资产公式)。
func AssetValue(a *Asset, m *MarketState) int64 {
	switch {
	case a.Kind == AssetStockIndex:
		return int64(float64(a.Units)*m.StockIndex + 0.5)
	case a.Kind == AssetBond:
		return int64(a.Units + 0.5) // 面值 1.00 元/份
	case a.Kind == AssetGold:
		return int64(a.Units*m.GoldPrice + 0.5)
	case isHouseKind(a.Kind):
		return m.HousePrice(a.AssetDistrict())
	case isShopKind(a.Kind):
		return m.ShopPrice(a.AssetDistrict())
	default:
		return 0 // side_business 残值 0;pension 单列
	}
}

// NetWorth 净资产 = 现金 + Σ资产市值 + 养老金余额 − Σ贷款余额(后端架构 §6)。
func (p *Player) NetWorth(m *MarketState) int64 {
	total := p.Cash + p.PensionCNY
	for i := range p.Assets {
		total += AssetValue(&p.Assets[i], m)
	}
	for i := range p.Loans {
		total -= p.Loans[i].Balance
	}
	return total
}

// MonthlyPassiveIncome 当月被动收入(租金 + 债券利息 + 60 岁养老金;不含 side)。
// FI 分母用最近一次月结总支出(见 settlement.go)。
func (p *Player) MonthlyPassiveIncome(m *MarketState, age int) int64 {
	var total int64
	for i := range p.Assets {
		a := &p.Assets[i]
		switch {
		case isHouseKind(a.Kind) && !a.IsSelfOccupied():
			total += m.HouseRent(a.AssetDistrict())
		case isShopKind(a.Kind):
			total += m.ShopRent(a.AssetDistrict())
		case a.Kind == AssetBond:
			total += int64(float64(a.Units) * a.AssetRate() / 12)
		}
	}
	if age >= 60 {
		total += int64(float64(p.PensionCNY) * 0.04 / 12)
	}
	return total
}

// FIIndex 财务自由指数 = 月被动收入 ÷ 月总支出(支出 ≤0 → 2.0 封顶,I6)。
func (p *Player) FIIndex(m *MarketState, age int) float64 {
	expense := p.Monthly.Expense
	if expense <= 0 {
		return 2.0
	}
	fi := float64(p.MonthlyPassiveIncome(m, age)) / float64(expense)
	if fi > 2.0 {
		fi = 2.0
	}
	if fi < 0 {
		fi = 0
	}
	return fi
}

// IncomeBand 月总收入档(《规则》§5.3):<8000 low / 8000–20000 mid / 20000–50000 high / >50000 top。
func (p *Player) IncomeBand() string {
	inc := p.monthlyIncomeEstimate()
	switch {
	case inc < 8000:
		return "low"
	case inc < 20000:
		return "mid"
	case inc < 50000:
		return "high"
	default:
		return "top"
	}
}
