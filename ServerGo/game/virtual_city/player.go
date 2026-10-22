// Package virtual_city — player.go: 玩家状态 + 资产/负债科目 + 三表推导(2026-09-14 §财商流P0)。
//
// 结构契约: 后端架构文档 §9.1。净资产/FI 等推导只读函数在此;
// 月结(settlement.go)与动作(actions.go)负责状态变更。
package virtual_city

import (
	"fmt"

	"LsmAgentGame/game/virtual_city/profession"
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
	LoanMortgage = "mortgage"     // 房贷 LPR+0.5%,360 期等额本息
	LoanConsumer = "consumer"     // 消费贷 10% 年化,36 期等额本息
	LoanCreditT1 = "credit_tier1" // 信用贷 tier1 月息 0.8%,到期一次性还本
	LoanCreditT2 = "credit_tier2" // tier2 月息 1.2%
	LoanCreditT3 = "credit_tier3" // tier3 月息 1.8%
	LoanBusiness = "business"     // 经营贷 LPR+2%,60 期先息后本
)

// Loan 是单笔负债。
type Loan struct {
	ID             string // "L1"…
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
// 批次20(文档2 §2)追加定价档字段;JSON 兼容旧档(零值 = 中价 = 旧行为)。
type SideBusiness struct {
	Kind         string // delivery|content|tutoring|freelance
	BaseIncome   int64  // 档位基准(展示)
	OpenedMonth  int
	PriceTier    int // 0=中价(默认,兼容旧档零值) 1=低价 2=高价(常量见 side_market.go)
	TierSetMonth int // 最近改档月(0=未改过;set_side_price 每月限 1 次)
}

// Policy 一张有效/历史保单(2026-09-19 §财商流P1-4 §2.2)。
// 每位玩家每险种最多 1 张有效保单(硬约束);失效/终止后允许重新投保
// (等待期重新计算,P1 新定)。
type Policy struct {
	Kind             string  `json:"kind"`               // 4 险种之一
	AnnualPremiumCNY int64   `json:"annual_premium_cny"` // 当前年保费(年结按年龄档×CPI 重定价)
	CoverageCNY      int64   `json:"coverage_cny"`       // 保额(名义,投保时锁定;医疗险 0)
	StartMonth       int     `json:"start_month"`        // 生效月(投保当月)
	PaidMonths       int     `json:"paid_months"`        // 已缴月数
	GraceActive      bool    `json:"grace_active"`       // 处于宽限期(上月断缴,本月兜底)
	Active           bool    `json:"active"`             // 有效中(含宽限期);失效/退保/赔付终止 → false
	CPIFactor        float64 `json:"cpi_factor"`         // 投保时 1.0,年结 ×(1+clamp(CPIYoY,0,0.10))
	ClaimsTotalCNY   int64   `json:"claims_total_cny"`   // 累计已赔付(展示用)
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

	// Domain 开局发卡时携带的 L1 行业域名(如 "A-农林牧渔";合成兜底卡 = "")。
	// 批次35 §4.1 座位居民外观推导用:city.DomainIndexOf(Domain) → avatar.domain
	// / ArchetypeFor 原型判据(开局卡面 ⇒ 整局稳定,不随运行时净资产闪变)。
	Domain string

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

	UnemployedMonths  int
	RehireSalaryRatio float64

	// 阶段4(2026-09-21 §城市扩张v2.12):政府财政子系统依赖字段
	// (transfer_payment.go 消费)。
	UnemployedAccumMonths int   // 终生累计失业月数(月末仍失业 +1;救济等待期/递减系数/低保门槛)
	BirthMonths           []int // 每次生育的主钟月份(儿童津贴 6 岁以下判定;events.go 生育时追加)

	// 批次52 §20261002-01 代际财富转移(P1-3 落地):需要跨月累积/决策落库的量。
	// 派生口径(父母在世/年龄/健康、子女年龄/教育档)在 family.go 纯函数现算,不落库。
	PrivateEduMask          []bool // 与子女序对齐:卡面初始在前、事件生育在后;true=私立
	FamilySupportTotalCNY   int64  // 累计赡养总支出(含自愿加赡养)—— 代际贡献分输入
	EducationTotalCNY       int64  // 累计教育总投入(升级一次性 + 月度教育费)—— 代际贡献分输入
	ChildSupportReceivedCNY int64  // 累计子女成年后回流(批次52 §2 裁决 D12)
	FamilySupportExtraCNY   int64  // 累计自愿加赡养(展示;计入 FamilySupportTotalCNY)

	SalaryBase     int64 // 当前基准月薪(年增长累积;P16 为波动带中值)
	SalaryLow      int64 // P16 波动带下界(非波动卡 = SalaryBase)
	SalaryHigh     int64 // P16 波动带上界
	SalaryVolatile bool

	SideBusiness *SideBusiness

	// StockT1Locked 当月新买入的股票份数(批次20 文档3 B2-3 T+1:月度节拍
	// 下的「当日」= 当月投影 —— 当月买入不可当月卖出;月结开头清零解冻)。
	// 可卖量 = 持仓 stock 总份数 − StockT1Locked;不足卖出 → 35044。
	StockT1Locked int64

	OvertimeThisMonth bool // work_overtime 置位,月结发放工资×0.3

	ActionBudget int
	// SpeakCountThisMonth 本月发言次数(2026-09-22 §CityHuman重构:
	// bool → int,speak area+private 合计 ≤2,原 SpokenThisMonth bool 升级)。
	SpeakCountThisMonth int
	Submitted           bool
	Alive               bool
	StoppedMonths       int // 破产停赛剩余月

	// LocalPos 区内归一化坐标 [0,1]²(2026-09-22 §CityHuman重构:
	// see/hear/smell 感知排序 + walk/run 区内移动用,不上 2.5D 地图逐人渲染)。
	LocalPos [2]float64

	DonationTotalCNY int64
	DonationCount    int

	OverdueCount   int
	OnTimeStreak   int // 连续按时还款月数(满 12 期 +20 信用分)
	BankruptCount  int
	MajorIllness   bool
	HadIllnessYear bool // 当年有大病 → 生日不加精力

	NegativeCashMonths int // 连续现金 < 0 月数(破产触发;批次74 旧口径)
	InReorganization   bool // 批次74 旧口径重组标记(BalanceV2Enabled=false)

	// ── 批次75 三阶段破产(H1/R1/B1,方案 §3.6;仅 BalanceV2Enabled=true 使用)──
	InHardship           bool // H1 困难期中(不记破产)
	HardshipMonths       int  // H1 剩余月数(倒数;到期仍负 ⇒ R1)
	HardshipCount        int  // H1 累计触发次数
	LastHardshipEndMonth int  // 上次 H1 结束月(24 月内重复触发 ⇒ 3 月)
	HardshipUnpaidMonths int  // H1 内连续无法付息月数(≥2 ⇒ R1)
	InR1                 bool // R1 协助重组中
	R1Months             int  // R1 重组分期数(首次 60 / 重复 36)
	LastR1EndMonth       int  // 上次 R1 结束月(24 月内重复重组 ⇒ 36 期)
	R1FreezeMonths       int  // R1 动作冻结剩余月数(仅禁非还款/非保险)

	// P1: 明斯基金融不稳定引擎(v2.60 N11-4 / N11-5)。
	MinskyByLoan          map[string]*MinskyStatus // loanID -> 分级
	MinskyMomentTriggered bool                     // 本回合明斯基时刻是否已触发(用于 UI 高亮)

	// ── 批次62 行为金融偏差引擎(《行为金融与投资心理机制设计》§3/§4)──
	// BiasProfile 本局偏差画像(2~3 个 code;开局按 seat 确定性采样,整局稳定)。
	BiasProfile []string
	// ResolvedBiases 已被"主动反思"解除的偏差 code(§4.3;解除后不再参与 hook)。
	ResolvedBiases map[string]bool
	// BiasImmunity 每类偏差的免疫进度 0~1(§4.4.2;≥0.80 ⇒ 该偏差本回合不生效)。
	BiasImmunity map[string]float64
	// BiasImmuneThisTurn 本回合免疫(深度反思,§4.3)。
	BiasImmuneThisTurn bool
	// BiasPreviewNextTurn 下一回合偏差一律视为"通过"(复盘预习,§4.3)。
	BiasPreviewNextTurn bool
	// BiasPreviewNextTurnMonth 设置"复盘预习"的月份(用于到期清理:设置月的
	// 月结不清理,下一月的月结才清理 —— 预习作用覆盖的是**下一个**行动回合)。
	BiasPreviewNextTurnMonth int
	// ReflectCountThisMonth 本月反思次数(每回合最多 1 次,§4.3)。
	ReflectCountThisMonth int
	// LastReflect 最近一次反思模式(none|light|deep|preview)。
	LastReflect string
	// DeferredPayments 延期支付明细(§3.3.1;本月免付,3 个月后 ×1.3)。
	DeferredPayments []DeferredPayment
	// DelayedConsumeCount / ImmediateConsumeCount 延迟/即时消费次数(§3.3.3 延迟满足指数)。
	DelayedConsumeCount   int
	ImmediateConsumeCount int
	// DeferredDefaultCount 延期支付违约次数(现金不足扣到 0)。
	DeferredDefaultCount int
	// MentalAccount 心理账户资金来源标签累计金额(§3.2.1;不影响购买力)。
	MentalAccount map[string]int64
	// MentalAccountMerged 是否已"合并账户"(合并后心理账户放大效果消失,§3.2.3)。
	MentalAccountMerged bool
	// AnchorSamples 锚定样本(滚动窗口;§3.1.4 锚定指数)。
	AnchorSamples []AnchorSample
	// AnchorMarks 资产 → 上月末观测价(买入时的"决策价"来源;§3.1)。
	AnchorMarks map[string]float64
	// HerdFollowCount / HerdCounterCount / HerdNeutralCount 羊群指数计数(§3.4.4)。
	HerdFollowCount  int
	HerdCounterCount int
	HerdNeutralCount int
	// HerdCounterMovesThisMonth 本月逆势操作次数(逆势奖励判定,§3.4.3)。
	HerdCounterMovesThisMonth int
	// SavingsStreakMonths 连续正储蓄月数(储蓄纪律奖励,§3.3.2)。
	SavingsStreakMonths int
	// LastMonthSurplusCNY 上月月结余(储蓄纪律奖励的计奖基数,§3.3.2)。
	LastMonthSurplusCNY int64
	// DelayedPayIndex 年度延迟满足指数(§3.3.3)。
	DelayedPayIndex float64
	// LastConsecutiveLossCount 连续投资亏损次数(赌徒谬误触发条件,§1 表第 10 项)。
	LastConsecutiveLossCount int
	// LastFinancialAssetValue 上月末金融类资产(股/债/金)市值 —— 投资表现的
	// 跨月比较基准(批次62;不含现金 ⇒ 不受收支影响,纯价格损益)。
	LastFinancialAssetValue int64

	// P1: 真实经济循环(2026-09-16 §财商流P1-2 契约 §3.1)。
	ConsumptionLevel   int                // 0 节俭/1 标准/2 精致/3 奢侈;默认 1(newPlayerFromCard 显式置 1)
	ConsumptionByGoods map[string]float64 // 上月消费结构(元,id→金额;nil=未初始化,视为档位 1)
	// ConsumptionLevelForced 批次74:档位 0 由**引擎强制**(新手期/储备不足/失业期)
	// 而非玩家选择;条件解除后据此恢复 1 并清标记(方案 §3.4)。不参与 JSON 下发。
	ConsumptionLevelForced bool `json:"-"`

	// P1-4: 商业保险保单(2026-09-19 §财商流P1-4 §2.2)。key=kind;nil=从未投保。
	Policies map[string]*Policy `json:"-"`

	Monthly         MonthlyResult
	NetWorthHistory []int64
	LastActionText  string // 本月最近动作(公开字段 last_action)
	StatusIcon      string // working|idle|trading|resting|moved
	Ending          string // 终局结局 id

	// ── 批次73 校准采集计数器(纯旁路观测:零 rand、零账目语义改动,
	// 不参与 StateHash;常驻生效 —— 真实对局同样累计,为批次74+ 线上观测留面;
	// 见 mc_resident.go / calibration.go::calibrationSamplePlayers)──
	TradesMade    int   // P2P/拍卖成交次数(买卖双方各 +1;CatTrade 结算处)
	AuctionsJoined int  // 拍卖有效出价次数(流拍/无效出价不计)
	BiasHits      int   // 行为偏差实际生效次数(biasEffectMulOf m≠1 分支)
	LoanMaxCNY    int64 // 终身贷款余额峰值(ΣBalance,月结采样)
	CreditMin     int   // 终身信用分最低值(月结采样;0 = 开局值即最低或暂未采样)
	TradeRealizedCNY int64 // 累计已实现价差盈亏(成交结算处分买卖记账)
	// biasHitsSampled 上次审计节拍已入账的 BiasHits(事件增量口径;
	// 未导出,不参与任何序列化)。
	biasHitsSampled int

	// ── 批次73 修正:破产时刻上下文快照(结局成因 NEG-1/2/4 输入)──
	// 破产清算(liquidate)**清减贷款/资产之前**采样一次(首次破产才写;
	// 二次破产不改写首因上下文)。零 rand、零账目语义、不参与 StateHash。
	// ⚠ 必须在 liquidate 的清偿/重组动作之前取值,否则月供/资产被清掉,
	// NEG 规则恒落空(gate 千局实测 NEG-1/2/4 全 0 的根因)。
	BankruptMonth        int     // 首次破产月(0 = 未破产)
	BankruptLoanPayment  int64   // 破产月存续贷款月供合计(Σ MonthlyPayment)
	BankruptExpense      int64   // 破产月最近月结总支出
	BankruptCash         int64   // 破产月现金
	BankruptNonCash      int64   // 破产月非现金资产(recomputeNetWorth − Cash 口径)
	BankruptTopCatShare  float64 // 破产月最大单一品类市值 ÷ 净资产(>1 clamp 1;NW≤0 且市值>0 ⇒ 1)
	BankruptTopCatDD     float64 // 该品类回撤(1 − 市值/买入成本;成本 ≤0 ⇒ 0,市值波动代理口径)
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

// stockSellableUnits 当前可卖股票份数(批次20 文档3 B2-3 T+1:持仓 − 当月
// 买入冻结量;负数 clamp 0)。卖出与挂牌(ListingAsset)共用同一口径。
func (p *Player) stockSellableUnits() float64 {
	total := 0.0
	for i := range p.Assets {
		if p.Assets[i].Kind == AssetStockIndex {
			total += p.Assets[i].Units
		}
	}
	sellable := total - float64(p.StockT1Locked)
	if sellable < 0 {
		sellable = 0
	}
	return sellable
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
