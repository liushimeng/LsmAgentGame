// Package wealth — settlement.go: 月结 7 步 + 年结 + 破产清算 + 终局评分
// (2026-09-14 §财商流P0)。
//
// 契约: 后端架构文档 §9.2(月结 7 步,顺序锁定)/ §9.3(年度调整)/
// §11(破产与出局)/ §12(终局评分与结局)。I1 守恒:所有现金变动走 World.Pay。
package wealth

import (
	"fmt"
	"strings"
)

// P16 波动带(《规则》§10.1.1):奇数月 U(3000,8000)、偶数月 U(5000,25000)。
// 2026-09-22 §17-CityHuman 全民驱动:原 profession/curated.go 整文件退役,
// 四常量迁入本文件就近持有(消费点 :333/:335/:708-709;注释标「原 curated.go」)。
const (
	P16BandLowOdd  = 3000 // 原 curated.go
	P16BandHighOdd = 8000 // 原 curated.go
	P16BandLowEven = 5000 // 原 curated.go
	P16BandHighEvt = 25000
)

// 月结常量(§9.2 P0 新定)。
const (
	livingSpouseCNY     = 2000 // 已婚配偶月支出
	livingChildCNY      = 5000 // 每孩月支出
	livingElderCNY      = 1000 // 赡养老人每位
	propertyFeeCNY      = 500  // 物业费(有房者)
	overtimeBonusRate   = 0.3  // 加班奖金 = 当月工资 × 0.3
	sideEnergyCost      = 2    // 副业月耗精力
	overdueCreditHit    = 50   // 逾期信用分惩罚
	creditCap           = 850
	bankruptStopMonths  = 3
	liquidationDiscount = 0.7 // 破产清算房产/商铺 70% 变现
)

// SettleMonth 结算当前月(§14 时序:①月度事件 → ②逐座位月结 → ③市场漂移 →
// ④月份+1/年调整/钟声)。返回 (是否终局, 各座位月度摘要)。
type MonthSummary struct {
	Seat      int     `json:"seat"`
	CashDelta int64   `json:"cash_delta"`
	NetWorth  int64   `json:"net_worth"`
	FIIndex   float64 `json:"fi_index"`
	Note      string  `json:"note"`
}

// SettleResult 单月结算产出(广播 game.month 用)。
type SettleResult struct {
	Month     int
	Age       int
	Summaries []MonthSummary
	Events    []EventRecord // 本月新增事件
	// MarketChanges 本月漂移后的市场快照(game.month.market_changes)。
	StockIndex, GoldPrice, BondRate float64
	HouseIdx                        map[string]float64
	// P1(§财商流P1-2 §6.3):market_changes 追加 cpi / unemployment_rate。
	CPI              float64 // 本月篮子 CPIYoY(economy_enabled=false 时 = CB.CPI)
	UnemploymentRate float64 // 本月内生失业率(回退时 0)
	// ClosedSurveys 本月结算关闭的调研(调研契约 §4.4;房间层锁外逐个触发
	// hooks.OnSurvey → game.survey_result)。
	ClosedSurveys []*Survey
}

// SettleMonth 执行一次完整月结。调用方持房间锁。
func (w *World) SettleMonth() (finished bool, res *SettleResult) {
	res = &SettleResult{Month: w.Month, Age: w.Age(), HouseIdx: map[string]float64{}}

	// ① 央行月度决策(P1:在月度事件之前,内生 CPI/LPR/信贷约束)。
	if w.CB != nil {
		w.CB.MonthlyDecision(w, w.Rand)
	}

	// ②.0 劳动力市场月度更新(P1 §财商流P1-2 §4.2):在 MonthlyEvents 之前,
	// 个体失业概率消费最新 Unemployment。economy_enabled=false 跳过。
	w.LaborMonthStep()

	// ②B 产业链月度调度(阶段5,2026-09-21 §城市扩张v2.12):需求自下而上
	// 传导 → 生产/利用率/库存(R5-1 断供防护)→ 价格逐层传导 → 消费品价格
	// 联动(在 ④.5 GoodsMonthStep 之前调整 PriceIdx,链条冲击进当月环比)。
	// 零 rand;economy_enabled=false 完整跳过。
	if w.EconomyEnabled && w.SupplyChain != nil {
		w.SupplyChain.MonthlyTick(w)
	}

	// ② 月度事件(失业判定)。
	w.MonthlyEvents()

	age := w.Age()

	// ③ 逐座位月结 7 步 + 破产检查 + P1 月度明斯基重分级。
	for seat, p := range w.Players {
		if p == nil {
			continue
		}
		cashBefore := p.Cash
		if p.Alive {
			w.settlePlayer(p, age)
			// P1: 月结后按最新收入重算明斯基分级(工资/收入可能变化)(v2.60 N11-4)。
			w.reclassifyPlayerMinsky(p)
			// P1: 重置明斯基时刻触发标记。
			p.MinskyMomentTriggered = false
		}
		net := p.NetWorth(w.Market)
		p.NetWorthHistory = append(p.NetWorthHistory, net)
		sum := MonthSummary{
			Seat: seat, CashDelta: p.Cash - cashBefore, NetWorth: net,
			FIIndex: p.FIIndex(w.Market, age),
		}
		if p.StoppedMonths > 0 {
			sum.Note = "停赛恢复中"
		}
		// P1-4(§财商流P1-4 §5.3): 意外身故座位摘要 note(身故赔付已入遗产池)。
		if !p.Alive && p.Ending == EndingAccidentDeath {
			sum.Note = "意外身故"
		}
		res.Summaries = append(res.Summaries, sum)
	}

	// ③.5 明斯基时刻判定(v2.60 N11-5):庞氏玩家占比 > 30% 时触发(有冷却)。
	if w.MinskyMomentCooldown > 0 {
		w.MinskyMomentCooldown--
	}
	if ponziCount := w.minskyPonziCount(); ponziCount > 0 {
		alive := len(w.alivePlayers())
		if alive > 0 {
			ponziRatio := float64(ponziCount) / float64(alive)
			if ponziRatio > 0.30 && w.MinskyMomentCooldown == 0 {
				w.triggerMinskyMoment(res)
			}
		}
	}

	// ④ 市场漂移 + 阶段到期重掷。
	p0 := w.Market.Params()
	res.StockIndex, res.GoldPrice, res.BondRate = w.Market.StockIndex, w.Market.GoldPrice, p0.BondRate
	if w.Market.CycleStep(w.Rand) {
		np := w.Market.Params()
		res.StockIndex, res.GoldPrice, res.BondRate = w.Market.StockIndex, w.Market.GoldPrice, np.BondRate
		w.emitEvent("market", -1, fmt.Sprintf("市场周期切换:%s(LPR %.1f%%, CPI %.1f%%)",
			cyclePhaseCN(w.Market.CyclePhase), np.LPR*100, np.CPI*100))
	}
	w.Market.MonthStep(w.Rand)
	for _, d := range DistrictDefs {
		res.HouseIdx[d.ID] = w.Market.DistrictIdx[d.ID]
	}

	// ④.5 消费品市场价格更新(P1 §财商流P1-2 §2.3):在市场漂移之后 ——
	// housing 联动需要本月最新 DistrictIdx。economy_enabled=false 跳过。
	w.GoodsMonthStep()

	// ④.6 社会结构统计(P1 §5.1,月度计算后缓存,view 层直读 World.Society)。
	if w.EconomyEnabled {
		w.Society = ComputeSociety(w)
	}

	// ④.6B 社会结构快照(阶段7,2026-09-21 §城市扩张v2.12): 财富/收入基尼
	// (R7-1 剔除 top/bottom 0.1%,12 人不足 1 人 → 全量)、Top1/Top10/Bottom50
	// 占比、财富五等分、4 层绝对门槛金字塔、<30 岁月度流动性;入 24 月环形
	// 历史(World.SocietyHist,view 下发当前快照 + 最近 12 月趋势)。
	// 零 rand(纯排序+算术),固定种子存量对局回归零偏移;
	// economy_enabled=false 完整跳过;nil 惰性初始化(防直接构造的 World 解引用)。
	if w.EconomyEnabled {
		if w.SocietyHist == nil {
			w.SocietyHist = &SocietyHistory{}
		}
		w.SocietyHist.Push(ComputeSocietySnapshot(w))
	}

	// ⑥B 金融市场月度引擎(阶段6,2026-09-21 §城市扩张v2.12):量化基金
	// (情绪源,首位 —— F05 评级联动消费本月量化收益)→ 同业存单(利率锚
	// SHIBOR + 到期兑付)→ 可转债(估值 + 强赎/回售)→ 融券(利息 + 强平)→
	// 基金评级(末位)。零 rand 消费(确定性公式),固定种子存量对局回归
	// 零偏移(treasury/供应链同款纪律);economy_enabled=false 完整跳过;
	// nil 惰性初始化(防旧档/异常路径 nil 解引用)。
	if w.EconomyEnabled {
		if w.QuantEngine == nil {
			w.QuantEngine = NewQuantFundEngine()
		}
		if w.CDMarket == nil {
			w.CDMarket = NewCDMarket()
			w.CDMarket.refreshRates(w)
		}
		if w.ShortBook == nil {
			w.ShortBook = NewShortBook()
		}
		w.QuantEngine.MonthlyStep(w)
		w.CDMarket.CDMonthlyStep(w)
		stepConvertibleBonds(w)
		w.ShortBook.ShortMonthlyStep(w)
		rateFundsMonthly(w)
	}

	// ⑨ 阶段4 政府财政月结(2026-09-21 §城市扩张v2.12;在 RecordFlowStat
	// 之前执行,使转移支付进入本月资金流向)。内部顺序锁定(treasury.go):
	//   ⑨A CollectMonthTax 税收汇总 → ⑨B PayTransferPayments 转移支付 →
	//   ⑨C ExecuteFiscalSpending 财政支出 → ⑨D MonthlyBondStep 国债月度
	//   处理 → ⑨E History 记录 + DeficitRun 更新。
	// economy_enabled=false 完整跳过;零 rand 消费,固定种子存量对局回归一致。
	if w.EconomyEnabled && w.Treasury != nil {
		w.Treasury.SettleTreasuryMonth(w)
	}

	// ⑨F 产业集群月度调度(阶段5,2026-09-21 §城市扩张v2.12):位于财政月结
	// **之后** —— 增值税减免从本月 VatTotal 负向扣除(R5-3 月/年/总额三重
	// 上限),不直接动 Treasury.Cash。零 rand;economy_enabled=false 跳过
	// (ClusterMonthStep 内部守卫)。
	w.ClusterMonthStep()

	// ⑨G 公共服务与监管月步(阶段8,2026-09-21 §城市扩张v2.12,最终阶段):
	// 公共服务五件套(质量累积/住房可负担性/就业)→ 四监管(证监会内幕
	// 检测/反垄断分拆/消协预警/隐私审计)→ 市长选举(R8-2 默认关闭 no-op;
	// 开启时津贴走 gov:treasury→seat CatWelfare)。位于 ⑨F 之后:
	// 消费 Treasury.LastMonthExpense(⑨E 已记)与当月 Ledger(⑨C 前的
	// 动作/结算流水)。零 rand;economy_enabled=false 完整跳过;nil 惰性
	// 初始化(防直接构造的 World 解引用)。
	if w.EconomyEnabled {
		if w.PublicSvc == nil {
			w.PublicSvc = NewPublicServices()
		}
		if w.Regulators == nil {
			w.Regulators = NewRegulatorBundle()
		}
		if w.Election == nil {
			w.Election = NewCivicElection()
		}
		w.PublicSvc.MonthlyStep(w)
		w.Regulators.MonthlyStep(w)
		w.Election.MonthlyStep(w) // Enabled=false 时 no-op(R8-2)
	}

	// ④.65 资金流向统计(P2 v2 §13.2.5,2026-09-19 §P2-可视化):
	// 在 ④.6 之后立即聚合本月 Ledger,刷新 World.LastFlowStat 供 view 下发。
	w.RecordFlowStat()

	// P2: 玩家间交易与财富流动系统(2026-09-16 §财商流P2)。
	// ④.7 借贷月结:逐笔 P2PLoan 月供扣款/逾期判定/担保代偿。
	w.SettleP2PLoans(res)
	// ④.8 挂单过期清理:ExpireMonth < w.Month 的 open/negotiating 挂单 → expired。
	w.ExpiredListingsCleanup()
	// ④.9 议价过期清理:ExpireMonth < w.Month 的 active 议价 → expired。
	w.ExpiredNegotiatesCleanup()
	// ④.10 拍卖到期处理:到期未成交 → 流拍/荷兰式降价处理。
	w.EndDueAuctions()

	// market_changes 追加(P1 §6.3):cpi = 篮子 CPIYoY(回退时 CB 理论值)。
	if w.EconomyEnabled && w.Goods != nil {
		res.CPI = w.Goods.CPIYoY
	} else if w.CB != nil {
		res.CPI = w.CB.CPI
	}
	if w.EconomyEnabled && w.Labor != nil {
		res.UnemploymentRate = w.Labor.Unemployment
	}

	// ⑤ 月份 +1;年调整 / 钟声。
	isYearEnd := w.Month%12 == 0
	isBell := w.Month%60 == 0
	w.Month++

	// P1: 年初(每年 1 月)LPR 重定价(v2.60 N12-3)。
	if w.Month > 0 && w.Month%12 == 1 {
		if records := w.RepriceMortgageLPR(); len(records) > 0 {
			w.emitEvent("lpr_reprice", -1, fmt.Sprintf("LPR 重定价:共 %d 笔房贷月供调整", len(records)))
		}
	}

	if isYearEnd {
		w.AnnualAdjust()
	}
	if isBell {
		w.Market.RerollPhase(w.Rand) // 钟声强制重掷(与阶段时长并存,先到者生效)
		w.BellEvents()
		w.emitEvent("market", -1, "人生钟声:市场周期重掷")
	}

	// P1: 调研到期检查(调研契约 §4.3 入口①):w.Month++ 后逐个检查,
	// w.Month > DeadlineMonth 的 open → 关闭聚合;房间层锁外广播 game.survey_result。
	res.ClosedSurveys = w.CloseSurveyIfDue()

	// 终局判定:主时钟到 60 岁。
	finished = w.Age() >= TerminalAge || len(w.alivePlayers()) == 0
	if finished {
		w.Status = StatusOver
	}
	return finished, res
}

// cyclePhaseCN 阶段中文名。
func cyclePhaseCN(p CyclePhase) string {
	switch p {
	case PhaseBoom:
		return "繁荣"
	case PhaseRecession:
		return "衰退"
	case PhaseDepression:
		return "萧条"
	default:
		return "复苏"
	}
}

// settlePlayer 单座位月结 7 步(§9.2,顺序锁定)。
// 步骤 3–5 的房租/物业/税/社保/生活支出为强制扣款(现金可为负);
// 仅贷款月供允许「不足 → 逾期」(§8.4:罚息 5%、信用分 −50)。
func (w *World) settlePlayer(p *Player, age int) {
	stopped := p.StoppedMonths > 0
	seat := p.Seat
	var income, expense, passive int64
	detail := []FlowItem{}

	// P1: 汇总 M0/M1/M2(央行货币统计)。
	if w.CB != nil {
		w.CB.UpdateMoneyStats(w)
	}

	addIncome := func(amount int64, key, text string, isPassive bool) {
		if amount == 0 {
			return
		}
		income += amount
		if isPassive {
			passive += amount
		}
		detail = append(detail, FlowItem{Key: key, AmountCNY: amount, Text: text})
	}
	addExpense := func(amount int64, key, text string) {
		if amount == 0 {
			return
		}
		expense += amount
		detail = append(detail, FlowItem{Key: key, AmountCNY: -amount, Text: text})
	}

	w.advanceUnemployment(p)

	// ── 步骤1 主动收入:工资(失业期 0;P16 波动带)+ 配偶 + 副业(精力<0 → 0)。
	var salary, side int64
	if !stopped {
		if p.UnemployedMonths <= 0 {
			if p.SalaryVolatile {
				lo, hi := P16BandLowOdd, P16BandHighOdd
				if w.Month%2 == 0 {
					lo, hi = P16BandLowEven, P16BandHighEvt
				}
				salary = int64(lo + w.Rand.Intn(hi-lo+1))
			} else {
				salary = p.SalaryBase
			}
		}
	}
	if salary > 0 {
		w.Pay(seat, EntityBank, SeatEntity(seat), salary, CatSalary, "工资")
		addIncome(salary, "salary", "工资", false)
		if p.OvertimeThisMonth {
			bonus := int64(float64(salary)*overtimeBonusRate + 0.5)
			if bonus > 0 {
				w.Pay(seat, EntityBank, SeatEntity(seat), bonus, CatOvertime, "加班奖金")
				addIncome(bonus, "overtime", "加班奖金", false)
			}
			p.OvertimeThisMonth = false
		}
	}
	if !stopped {
		if p.Family.Marital == "married" && p.Family.SpouseIncome > 0 {
			w.Pay(seat, EntityBank, SeatEntity(seat), p.Family.SpouseIncome, CatSpouse, "配偶收入")
			addIncome(p.Family.SpouseIncome, "spouse", "配偶收入", false)
		}
		if p.SideBusiness != nil {
			if p.Energy >= 0 {
				base := float64(p.SideBusiness.BaseIncome) * p.BrassSideFactor()
				variance := 0.85 + w.Rand.Float64()*0.3
				side = int64(base*variance + 0.5)
				if side > 0 {
					w.Pay(seat, EntityMarket, SeatEntity(seat), side, CatSide, "副业收入")
					addIncome(side, "side", "副业收入", false)
				}
			}
			p.Energy -= sideEnergyCost
		}
	}

	// ── 步骤2 被动收入:住宅租金(非自住)+ 商铺租金 + 债券利息(60 岁后 + 养老金)。
	for i := range p.Assets {
		a := &p.Assets[i]
		switch {
		case isHouseKind(a.Kind) && !a.IsSelfOccupied():
			rent := w.Market.HouseRent(a.AssetDistrict())
			if rent > 0 {
				w.Pay(seat, EntityMarket, SeatEntity(seat), rent, CatRent, "住宅租金")
				addIncome(rent, "rent", "住宅租金 "+DistrictCN(a.AssetDistrict()), true)
			}
		case isShopKind(a.Kind):
			rent := w.Market.ShopRent(a.AssetDistrict())
			if rent > 0 {
				w.Pay(seat, EntityMarket, SeatEntity(seat), rent, CatRent, "商铺租金")
				addIncome(rent, "rent", "商铺租金 "+DistrictCN(a.AssetDistrict()), true)
			}
		case a.Kind == AssetBond:
			interest := int64(a.Units * a.AssetRate() / 12)
			if interest > 0 {
				w.Pay(seat, EntityMarket, SeatEntity(seat), interest, CatBondInterest, "债券利息")
				addIncome(interest, "bond_interest", "债券利息", true)
			}
		}
	}
	if age >= 60 {
		pension := int64(float64(p.PensionCNY) * 0.04 / 12)
		if pension > 0 {
			w.Pay(seat, EntityGov, SeatEntity(seat), pension, CatPension, "养老金")
			addIncome(pension, "pension", "养老金", true)
		}
	}

	// ── 步骤3 固定支出:房租(未自住)+ 物业 500(有房者)先扣;贷款月供允许逾期。
	if p.selfOccupiedHouse() == nil {
		rentPay := w.Market.HouseRent(p.HomeDistrict)
		if rentPay > 0 {
			w.Pay(seat, SeatEntity(seat), w.consumerPayTo(), rentPay, CatRentPay, "房租")
			addExpense(rentPay, "rent_pay", "房租")
		}
	}
	if p.ownsProperty() {
		w.Pay(seat, SeatEntity(seat), w.consumerPayTo(), propertyFeeCNY, CatProperty, "物业费")
		addExpense(propertyFeeCNY, "property", "物业费")
	}

	// P1-4(§财商流P1-4 §4.1): 步骤3 末尾追加保费扣缴(《规则》§3.5 保险费属
	// 保障支出;断缴/宽限期/失效语义在 insurance.go::SettlePremiums)。
	if prem := w.SettlePremiums(p); prem > 0 {
		addExpense(prem, "insurance", "保险费")
	}

	// ── 步骤4 税 + 社保(仅工资;8% 养老金个人账户 gov→seat 回流)。
	tax := MonthlyIncomeTax(salary)
	social := SocialSecurity(salary)
	pensionPart := PensionContribution(salary)
	if tax > 0 {
		w.Pay(seat, SeatEntity(seat), EntityGov, tax, CatTax, "个税")
		addExpense(tax, "tax", "个税")
	}
	if social > 0 {
		w.Pay(seat, SeatEntity(seat), EntityGov, social, CatSocial, "社保")
		addExpense(social, "social", "社保")
		if pensionPart > 0 {
			w.Pay(seat, EntityGov, SeatEntity(seat), pensionPart, CatPension, "养老金缴存")
			p.PensionCNY += pensionPart
		}
	}

	// ── 步骤5 生活支出:职业基数 × 通胀因子 × 档位乘数(P1 §3.3)+ 配偶 2000 +
	// 每孩 5000 + 赡养;economy_enabled 时归入 firms 钱流并按恩格尔曲线拆 8 类。
	infl := InflationFactorCB(w)
	level := p.ConsumptionLevelSafe()
	if p.ConsumptionByGoods == nil {
		level = 1 // 存量兜底:map 未初始化视为标准档(与 ConsumptionLevelSafe 双保险)
	}
	// 强制降档(流动性约束,P1 新定):现金 < 2×月生活支出基准(不含档位乘数)
	// → 强制降为 0 档(节俭)。
	baseline := int64(float64(p.Card.Expense)*infl + 0.5)
	if p.Cash < 2*baseline && level != 0 {
		p.ConsumptionLevel = 0
		level = 0
		w.emitEvent("life", seat, fmt.Sprintf("%d 号位现金不足,本月强制节俭档(生活支出×0.6)", seat))
	}
	living := int64(float64(p.Card.Expense)*infl*consumptionLevelMult[level] + 0.5)
	if living > 0 {
		w.Pay(seat, SeatEntity(seat), w.consumerPayTo(), living, CatLiving, "生活支出")
		addExpense(living, "living", "生活支出")
	}
	familyLiving := int64(0)
	if p.Family.Marital == "married" {
		familyLiving += livingSpouseCNY
	}
	familyLiving += int64(p.Family.Children) * livingChildCNY
	familyLiving += int64(p.Card.EldersDependent) * livingElderCNY
	if familyLiving > 0 {
		// 家庭支出金额不变,但归入篮子计数(§3.4)且 to=firms。
		w.Pay(seat, SeatEntity(seat), w.consumerPayTo(), familyLiving, CatLiving, "家庭支出")
		addExpense(familyLiving, "family", "家庭支出(配偶/子女/赡养)")
	}

	// 档位精力效果(clamp [-3,10],与 events.go 精力边界同款)。
	p.Energy = clamp(p.Energy+consumptionLevelEnergy[level], -3, 10)

	// 恩格尔分配:living+familyLiving 拆 8 类写入 p.ConsumptionByGoods(覆盖上月)。
	p.ConsumptionByGoods = splitEngel(living+familyLiving, p, age)

	// ── 步骤6 债务:月供足够 → 正常扣款;不足 → 逾期(罚息 5%、信用分 −50)。
	type dueItem struct {
		loan                *Loan
		interest, principal int64
	}
	var dues []dueItem
	dueTotal := int64(0)
	for i := range p.Loans {
		loan := &p.Loans[i]
		var interest, principal int64
		if loan.FreeInterest {
			principal = loan.Balance / int64(max(loan.MonthsLeft, 1))
			if principal < 1 {
				principal = 1
			}
			if principal > loan.Balance {
				principal = loan.Balance
			}
		} else {
			rate := loan.AnnualRate
			if w.CB != nil {
				// P1: 浮动利率贷款(mortgage/business)按当月 LPR 重算利率。
				switch loan.Kind {
				case LoanMortgage:
					rate = w.CB.MortgageRate()
				case LoanBusiness:
					rate = w.CB.BusinessRate()
				}
			}
			interest = int64(float64(loan.Balance)*rate/12 + 0.5)
			if loan.InterestOnly {
				// 先息后本:月付息;末期(business)还本;信用贷到期一次性还本。
				if loan.MonthsLeft <= 1 && !loan.LumpAtMaturity {
					principal = loan.Balance
				}
			} else {
				// 等额本息:按当月 LPR 重算月供(不写回 loan.AnnualRate,避免累积漂移)。
				payment := loan.MonthlyPayment
				if rate != loan.AnnualRate {
					payment = AnnuityPayment(loan.Balance, rate, loan.MonthsLeft)
				}
				principal = payment - interest
				if principal < 0 {
					principal = 0
				}
				if principal > loan.Balance {
					principal = loan.Balance
				}
			}
		}
		dues = append(dues, dueItem{loan: loan, interest: interest, principal: principal})
		dueTotal += interest + principal
	}
	overdue := dueTotal > 0 && p.Cash < dueTotal
	if !overdue {
		for _, d := range dues {
			if d.interest > 0 {
				w.Pay(seat, SeatEntity(seat), EntityBank, d.interest, CatInterest, "贷款利息 "+d.loan.ID)
			}
			if d.principal > 0 {
				w.Pay(seat, SeatEntity(seat), EntityBank, d.principal, CatPrincipal, "还本 "+d.loan.ID)
			}
			d.loan.Balance -= d.principal
			d.loan.MonthsLeft--
			addExpense(d.interest+d.principal, "loan", "贷款月供 "+d.loan.ID)
			if d.loan.MonthsLeft <= 0 || d.loan.Balance <= 0 {
				w.removeLoan(p, d.loan.ID)
			}
		}
		p.OnTimeStreak++
	} else {
		p.OverdueCount++
		p.OnTimeStreak = 0
		if p.CreditScore >= overdueCreditHit {
			p.CreditScore -= overdueCreditHit
		} else {
			p.CreditScore = 0
		}
		penalty := int64(float64(dueTotal) * 0.05)
		if pay := min(p.Cash, penalty); pay > 0 {
			w.Pay(seat, SeatEntity(seat), EntityBank, pay, CatInterest, "逾期罚息")
			addExpense(pay, "penalty", "逾期罚息")
		}
		addExpense(dueTotal, "loan_overdue", "本月月供逾期未付(顺延)")
		w.emitEvent("settle", seat, fmt.Sprintf("%d 号位月供不足,逾期(应付 ¥%d,信用分 −%d)",
			seat, dueTotal, overdueCreditHit))
	}

	// ── 步骤7 精力自然恢复 +1(≤10)。
	if p.Energy < 10 {
		p.Energy++
	}

	p.Monthly = MonthlyResult{
		Income: income, Expense: expense, Net: income - expense,
		Tax: tax, Social: social, PassiveIncome: passive, SideIncome: side,
		Detail: detail,
	}

	// ── 破产检查:现金连续 3 月 < 0(月末,§11)。
	if p.Cash < 0 {
		p.NegativeCashMonths++
		if p.NegativeCashMonths >= 3 && !p.InReorganization {
			w.liquidate(p)
		}
	} else {
		p.NegativeCashMonths = 0
		if p.InReorganization {
			p.InReorganization = false
			w.emitEvent("settle", seat, fmt.Sprintf("%d 号位重整成功,重返赛道", seat))
		}
	}

	// 停赛倒数。
	if p.StoppedMonths > 0 {
		p.StoppedMonths--
	}
}

// liquidate 破产清算 + 债务重组(§11)。
func (w *World) liquidate(p *Player) {
	seat := p.Seat
	w.emitEvent("settle", seat, fmt.Sprintf("%d 号位触发破产清算!", seat))

	// 流动资产按市价卖出;房产/商铺按当前价 × 70% 强制变现(先偿房贷,余额入现金)。
	for i := len(p.Assets) - 1; i >= 0; i-- {
		a := &p.Assets[i]
		switch {
		case a.Kind == AssetStockIndex:
			gross := int64(a.Units*w.Market.StockIndex + 0.5)
			if gross > 0 {
				w.Pay(seat, EntityMarket, SeatEntity(seat), gross, CatSell, "清算卖出基金")
			}
		case a.Kind == AssetBond:
			if a.Units > 0 {
				w.Pay(seat, EntityMarket, SeatEntity(seat), int64(a.Units+0.5), CatSell, "清算赎回债券")
			}
		case a.Kind == AssetGold:
			gross := int64(a.Units*w.Market.GoldPrice + 0.5)
			if gross > 0 {
				w.Pay(seat, EntityMarket, SeatEntity(seat), gross, CatSell, "清算卖出黄金")
			}
		case isHouseKind(a.Kind) || isShopKind(a.Kind):
			price := w.Market.HousePrice(a.AssetDistrict())
			forced := int64(float64(price) * liquidationDiscount)
			if forced > 0 {
				w.Pay(seat, EntityMarket, SeatEntity(seat), forced, CatSell, "强制变现房产")
			}
		}
	}
	p.Assets = nil
	p.SideBusiness = nil

	// 先偿房贷(变现款已在现金),再重组其余贷款。
	for i := len(p.Loans) - 1; i >= 0; i-- {
		loan := &p.Loans[i]
		if loan.Kind == LoanMortgage {
			pay := loan.Balance
			if pay > p.Cash {
				pay = p.Cash
			}
			if pay > 0 {
				w.Pay(seat, SeatEntity(seat), EntityBank, pay, CatRepay, "清算偿房贷")
			}
			loan.Balance -= pay
			if loan.Balance <= 0 {
				w.removeLoan(p, loan.ID)
			}
		}
	}
	// 债务重组:消费贷/信用贷本金减 50%,分 36 期免息;经营贷余额保留。
	for i := range p.Loans {
		loan := &p.Loans[i]
		if loan.Kind == LoanConsumer || strings.HasPrefix(loan.Kind, "credit") {
			loan.Balance /= 2
			loan.FreeInterest = true
			loan.InterestOnly = false
			loan.LumpAtMaturity = false
			loan.AnnualRate = 0
			loan.TermN = 36
			loan.MonthsLeft = 36
			loan.MonthlyPayment = loan.Balance / 36
		}
	}

	p.CreditScore = 0
	p.Energy -= 5
	if p.Energy < -3 {
		p.Energy = -3
	}
	p.StoppedMonths = bankruptStopMonths
	p.InReorganization = true
	p.BankruptCount++
	p.NegativeCashMonths = 0

	// 重组后首个月结仍 < 0 → 出局(下月检查由 InReorganization + Cash<0 承担:
	// 若 3 月内再次连续 3 月为负则二次清算;P0 简化:重组后现金仍 < 0 即刻出局)。
	if p.Cash < 0 {
		w.eliminate(p, "破产出局")
	}
}

// eliminate 出局(§11)。
func (w *World) eliminate(p *Player, reason string) {
	p.Alive = false
	p.Ending = EndingBankrupt
	p.StatusIcon = "idle"
	w.emitEvent("settle", p.Seat, fmt.Sprintf("%d 号位%s,移出棋盘", p.Seat, reason))
}

// AnnualAdjust 年度调整(§9.3):工资增长(Brass 改写)+ 信用分按时还款 +20。
// P1 §4.4(§财商流P1-2):economy_enabled 时增长率改走 Phillips 曲线 ——
// g = phillipsBase + (BrassSalaryGrowth − 3%),自然失业率下与 P0 数值完全一致
// (回归零差异),Brass「信用贷压低增长」语义叠加在 Phillips 基线之上。
// 名义工资粘性:工资只在年度调整,CPI 每月变 —— 高通胀期实际工资自动缩水。
func (w *World) AnnualAdjust() {
	if w.EconomyEnabled && w.Labor != nil {
		w.Labor.WageGrowthYoY = w.phillipsBase()
	}
	for _, seat := range w.alivePlayers() {
		p := w.Players[seat]
		g := p.BrassSalaryGrowth()
		if w.EconomyEnabled && w.Labor != nil {
			g = w.phillipsBase() + p.BrassSalaryGrowth() - 0.03
		}
		p.SalaryBase = int64(float64(p.SalaryBase)*(1+g) + 0.5)
		if p.SalaryVolatile {
			// 年增长作用于带边界。
			lo := P16BandLowOdd
			hi := P16BandHighEvt
			if p.SalaryLow > 0 {
				lo = int(p.SalaryLow)
			}
			if p.SalaryHigh > 0 {
				hi = int(p.SalaryHigh)
			}
			lo = int(float64(lo)*(1+g) + 0.5)
			hi = int(float64(hi)*(1+g) + 0.5)
			p.SalaryLow, p.SalaryHigh = int64(lo), int64(hi)
		}
		if p.OnTimeStreak >= 12 && p.CreditScore+20 <= creditCap {
			p.CreditScore += 20
		}
		p.OnTimeStreak = 0 // 年度滚动
		// P1-4(§财商流P1-4 §4.3): 年结保费重定价(年龄档上浮 + CPI 累积上浮)。
		w.RepricePolicies(p)
		w.emitEvent("settle", seat, fmt.Sprintf("%d 号位年度调整:工资 %+d%%", seat, int(g*100)))
	}

	// 阶段8(2026-09-21 §城市扩张v2.12):教育代际效应年度步(月收入 > 门槛的
	// 座位累积 0.1/年,满 1.0 → Cognition +1;§130 接线:economy_enabled 才生效,
	// PublicSvc nil 惰性初始化与 ⑨G 同款守卫)。
	if w.EconomyEnabled {
		if w.PublicSvc == nil {
			w.PublicSvc = NewPublicServices()
		}
		w.PublicSvc.AnnualEduStep(w)
	}
}

// ─────────────────── 终局评分与结局(§12) ───────────────────

// FinalScore 单座位三维评分。
type FinalScore struct {
	Seat        int     `json:"seat"`
	FIScore     float64 `json:"fi_score"`
	LifeScore   float64 `json:"life_score"`
	SocialScore float64 `json:"social_score"`
	Total       float64 `json:"total"`
	Ending      string  `json:"ending"`
	Report      string  `json:"report"`
}

// FinalScores 终局评分(引擎确定性计算,report 纯模板拼接,不调 LLM)。
func (w *World) FinalScores() []FinalScore {
	age := w.Age()
	var out []FinalScore
	for seat, p := range w.Players {
		if p == nil {
			continue
		}
		// 财务自由度(§12)。
		fi := p.FIIndex(w.Market, age)
		var fiScore float64
		switch {
		case fi >= 2.0:
			fiScore = 100
		case fi >= 1.5:
			fiScore = 80
		case fi >= 1.0:
			fiScore = 60
		case fi >= 0.5:
			fiScore = 30
		}
		nw := p.NetWorth(w.Market)
		if nw > 5000000 {
			fiScore += 20
		} else if nw > 1000000 {
			fiScore += 10
		}
		if fiScore > 110 {
			fiScore = 110
		}

		// 人生满意度(上限 100)。
		life := float64(clamp(p.Energy, 0, 10) * 3)
		if p.Family.Marital == "married" {
			life += 10
		}
		if p.Family.Children >= 1 && p.Family.Children <= 2 {
			life += 10
		}
		if p.Network >= 7 {
			life += 10
		}
		if !p.MajorIllness {
			life += 10
		}
		if p.DonationTotalCNY > 100000 {
			life += 10
		}
		if life > 100 {
			life = 100
		}

		// 社会贡献(裁剪后满分 50,归一化 ×2)。
		social := float64(0)
		donationPts := float64(p.DonationTotalCNY) / 10000.0
		if donationPts > 10 {
			donationPts = 10
		}
		social += donationPts
		if p.OverdueCount == 0 {
			social += 10
		}
		if p.BankruptCount == 0 {
			social += 10
		}
		if p.Alive {
			social += 10
		}
		social += 10 // 合规(P0 无处罚机制,未出局即默认合规)
		social = social / 50 * 100
		if social > 100 {
			social = 100
		}

		total := fiScore*0.5 + life*0.3 + social*0.2

		ending := EndingBankrupt
		switch {
		case total >= 85:
			ending = EndingWinner
		case total >= 70:
			ending = EndingAffluent
		case total >= 50:
			ending = EndingOrdinary
		case total >= 30:
			ending = EndingIndebted
		}
		if !p.Alive {
			ending = EndingBankrupt
		} else if fi >= 1.5 && life < 40 {
			ending = EndingLonelyRic
		}
		p.Ending = ending

		out = append(out, FinalScore{
			Seat: seat, FIScore: fiScore, LifeScore: life, SocialScore: social,
			Total: total, Ending: ending,
			Report: w.finalReport(p, nw, fi),
		})
	}
	return out
}

// finalReport 中文摘要(净资产曲线关键拐点 + 最大单笔盈亏 + 捐赠总额,模板拼接)。
func (w *World) finalReport(p *Player, netWorth int64, fi float64) string {
	peak, trough := int64(0), int64(0)
	for _, v := range p.NetWorthHistory {
		if v > peak {
			peak = v
		}
		if v < trough {
			trough = v
		}
	}
	maxGain, maxLoss := int64(0), int64(0)
	for _, e := range w.Ledger.Entries {
		if e.From == SeatEntity(p.Seat) && e.Category == CatBuy {
			if e.AmountCNY > maxGain {
				maxGain = e.AmountCNY
			}
		}
		if e.To == SeatEntity(p.Seat) && e.Category == CatSell && e.AmountCNY > maxLoss {
			maxLoss = e.AmountCNY
		}
	}
	return fmt.Sprintf("%s(%s)的 35 年:终局净资产 ¥%d(FI %.2f),峰值 ¥%d / 谷底 ¥%d;"+
		"最大单笔买入 ¥%d、最大单笔变现 ¥%d;累计捐赠 ¥%d。",
		p.Card.Title, p.Card.ID, netWorth, fi, peak, trough, maxGain, maxLoss, p.DonationTotalCNY)
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
