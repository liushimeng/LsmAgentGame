// Package virtual_city — insurance.go: 商业保险与风险转移引擎(2026-09-19 §财商流P1-4)。
//
// 契约: lag_docs/虚拟城市/已实现/10-P1保险系统/虚拟城市-P1-商业保险与风险转移引擎-v1.md。
// 落地《规则》§6.7 四险种(重疾/百万医疗/定期寿险/意外)完整链路:
// 投保(动作) → 月结保费(断缴/宽限/失效) → 事件理赔(医疗/重疾/意外) → 身故赔付入遗产池。
// 核心原则(《规则》§6.7):保险不产生收益,只转移风险。
//
// Ledger 方向(P1-4 激活 insurer 实体,ledger.go §6.1):
//
//	保费 seat→insurer(CatPremium)/ 理赔 insurer→seat(CatClaim);均走 World.Pay,
//	I1 座位守恒由构造保证;insurer 恒为无限偿付系统实体(不做破产模拟,§6.2)。
package virtual_city

import (
	"encoding/json"
	"fmt"
	"strings"

	"LsmAgentGame/errcode"
)

// 险种 kind 常量(§2.1)。
const (
	InsCriticalIllness = "critical_illness" // 重疾险
	InsMedicalMillion  = "medical_million"  // 百万医疗险
	InsTermLife        = "term_life"        // 定期寿险
	InsAccident        = "accident"         // 意外险
)

// insuranceKindOrder 固定展示顺序(view / 月结扣缴 / prompt 共用,保证确定性)。
var insuranceKindOrder = []string{InsCriticalIllness, InsMedicalMillion, InsTermLife, InsAccident}

// policyDef 险种静态定义(§2.1,常量即契约)。
type policyDef struct {
	BasePremiumCNY int64   // 基准年保费(25 岁档)
	LowCNY         int64   // 规则区间下限(clamp 用)
	HighCNY        int64   // 规则区间上限(clamp 基数,随 CPIFactor 放宽)
	CoverageCNY    int64   // 定额给付保额(名义锁定;medical_million 为 0=比例报销)
	ReimburseRatio float64 // 报销比例(仅 medical_million=0.9,其余 0)
	WaitingMonths  int     // 等待期(游戏月)
}

var policyDefs = map[string]policyDef{
	"critical_illness": {BasePremiumCNY: 5000, LowCNY: 3000, HighCNY: 8000, CoverageCNY: 400000, WaitingMonths: 3},
	"medical_million":  {BasePremiumCNY: 1000, LowCNY: 500, HighCNY: 1500, ReimburseRatio: 0.9, WaitingMonths: 1},
	"term_life":        {BasePremiumCNY: 2000, LowCNY: 1000, HighCNY: 3000, CoverageCNY: 1000000, WaitingMonths: 3},
	"accident":         {BasePremiumCNY: 350, LowCNY: 200, HighCNY: 500, CoverageCNY: 500000, WaitingMonths: 0},
}

// 意外事件参数(§5.2,P1-4 新定)。
const (
	accidentMonthlyP        = 0.0012 // 每月基础概率(≈1.4%/年;不乘 negativeGuard)
	accidentDeathRatio      = 0.30   // 触发后 30% 意外身故 / 70% 意外伤残
	accidentInjuryLowCNY    = 20000  // 伤残医疗支出 U(20,000, 50,000)
	accidentInjurySpanCNY   = 30001
	accidentDisabilityRatio = 0.5 // 意外伤残给付 = 保额 50%(250,000)
	insuranceAgeGate        = 55  // 主时钟年龄 > 55 禁止新投保(§3.1)
)

// kindCN 险种中文名。
func kindCN(kind string) string {
	switch kind {
	case InsCriticalIllness:
		return "重疾险"
	case InsMedicalMillion:
		return "百万医疗险"
	case InsTermLife:
		return "定期寿险"
	case InsAccident:
		return "意外险"
	default:
		return kind
	}
}

// agePremiumMult 年龄加价系数(§3.3,P1 新定;真实医疗险自然费率的简化)。
func agePremiumMult(age int) float64 {
	switch {
	case age < 35:
		return 1.0
	case age < 45:
		return 1.25
	case age < 50:
		return 1.6
	default:
		return 2.0 // 50–55
	}
}

// priceAnnual 年保费定价(§3.3):clamp(Base×ageMult×cpiFactor, Low, round(High×cpiFactor))。
// 规则区间随 CPI 同步放宽(通胀下名义保费允许突破 2025 锚点)。
func priceAnnual(def policyDef, age int, cpiFactor float64) int64 {
	annual := int64(float64(def.BasePremiumCNY)*agePremiumMult(age)*cpiFactor + 0.5)
	high := int64(float64(def.HighCNY)*cpiFactor + 0.5)
	if annual < def.LowCNY {
		annual = def.LowCNY
	}
	if annual > high {
		annual = high
	}
	return annual
}

// monthlyPremium 月缴 = round(年保费 / 12)(《规则》§5.1「保险费 = 年缴保费 ÷ 12」)。
func monthlyPremium(annualCNY int64) int64 {
	if annualCNY <= 0 {
		return 0
	}
	return int64(float64(annualCNY)/12 + 0.5)
}

// Status 保单展示状态(§2.3;由 Active/PaidMonths/GraceActive/StartMonth 派生,不写库)。
func (p *Policy) Status(nowMonth int) string {
	if !p.Active {
		return "lapsed" // 失效(断缴超宽限/退保/重疾赔付终止)
	}
	if p.GraceActive {
		return "grace" // 宽限期(保障仍有效)
	}
	if nowMonth-p.StartMonth < policyDefs[p.Kind].WaitingMonths {
		return "waiting" // 等待期(不赔;保费照扣)
	}
	return "active" // 有效且可理赔
}

// claimable 理赔资格(§5.1):active / grace 可赔(宽限期保障有效);
// waiting(等待期)/ lapsed(失效)不赔不退(消费型)。
func (p *Policy) claimable(nowMonth int) bool {
	st := p.Status(nowMonth)
	return st == "active" || st == "grace"
}

// waitingLeft 等待期剩余月(0 = 已过)。
func (p *Policy) waitingLeft(nowMonth int) int {
	left := policyDefs[p.Kind].WaitingMonths - (nowMonth - p.StartMonth)
	if left < 0 {
		return 0
	}
	return left
}

// ── 月结保费流(§4) ──

// SettlePremiums 单座位月保费扣缴(月结步骤3 末尾调用;逐保单处理,§4.2)。
// 返回本月实缴合计(settlement 层计入 Monthly.Expense + detail{key:"insurance"})。
//
//	monthly = round(AnnualPremiumCNY / 12)
//	现金充足 → Pay(seat→insurer, monthly, CatPremium);PaidMonths++;GraceActive=false
//	现金不足 → 本月不扣:
//	  首次断缴 → GraceActive=true(宽限期 1 个月,保障继续有效,事件提示)
//	  宽限期次月仍不足 → Active=false(失效 lapse,事件提示;下月可重新投保)
//
// 现金允许为负的既有逻辑不变(保费不足时不强扣,不新增 NegativeCashMonths 触发路径);
// 投保当月(StartMonth == w.Month)首月保费已在动作时扣缴,此处跳过防双扣;
// 破产停赛(StoppedMonths>0)/出局(!Alive)座位不扣缴、不宽限推进,保单原样冻结(P1 新定)。
func (w *World) SettlePremiums(p *Player) int64 {
	if !w.InsuranceEnabled || p == nil || !p.Alive || p.StoppedMonths > 0 {
		return 0
	}
	var total int64
	for _, kind := range insuranceKindOrder {
		pol := p.Policies[kind]
		if pol == nil || !pol.Active {
			continue
		}
		if pol.StartMonth == w.Month {
			continue // 投保当月首月保费已由 buy_insurance 动作即时扣缴
		}
		monthly := monthlyPremium(pol.AnnualPremiumCNY)
		if monthly <= 0 {
			continue
		}
		if p.Cash < monthly {
			if pol.GraceActive {
				pol.Active = false // 宽限期次月仍不足 → 失效
				w.emitEvent("life", p.Seat, fmt.Sprintf("%d 号位%s断缴超宽限期,保单失效(可重新投保,等待期重算)", p.Seat, kindCN(kind)))
			} else {
				pol.GraceActive = true // 首次断缴 → 宽限期(保障仍有效)
				w.emitEvent("life", p.Seat, fmt.Sprintf("%d 号位现金不足,%s保费断缴,进入宽限期(保障仍有效,请尽快补足现金)", p.Seat, kindCN(kind)))
			}
			continue
		}
		w.Pay(p.Seat, SeatEntity(p.Seat), EntityInsurer, monthly, CatPremium, kindCN(kind)+"保费")
		pol.PaidMonths++
		pol.GraceActive = false
		total += monthly
	}
	return total
}

// insuranceCPIYoY 年结重定价用 CPI(§4.3):Goods 篮子 CPIYoY 就绪时取内生值;
// 未就绪 / economy_enabled=false → 0(保费不变 —— 固定种子回归确定性优先,
// 见 §12 单测「CPI 未就绪/economy 关闭 → 保费不变」;与 §4.3 文字中「回退
// CB.CPI」的差异见实现偏差记录:CB 外生 CPI 会破坏该回归锚点)。
func (w *World) insuranceCPIYoY() float64 {
	if w.EconomyEnabled && w.Goods != nil && w.Goods.CPIYoYReady() {
		return w.Goods.CPIYoY
	}
	return 0
}

// RepricePolicies 年结保费重定价(每 12 月,AnnualAdjust 既有循环内追加,§4.3)。
//
//	每张 Active 保单:
//	  CPIFactor ×= (1 + clamp(cpiYoY, 0, 0.10))
//	  AnnualPremiumCNY = clamp(round(Base×ageMult(w.Age())×CPIFactor), Low, round(High×CPIFactor))
//
// 保费随年龄档上浮 + 随 CPI 累积上浮(1 年期消费型短险续保重定价,现实常态);
// 保额名义固定(CoverageCNY 投保锁定,通胀侵蚀保额是教育点,§4.3 取舍)。
func (w *World) RepricePolicies(p *Player) {
	if !w.InsuranceEnabled || p == nil || len(p.Policies) == 0 {
		return
	}
	cpi := clampF(w.insuranceCPIYoY(), 0, 0.10)
	for _, kind := range insuranceKindOrder {
		pol := p.Policies[kind]
		if pol == nil || !pol.Active {
			continue
		}
		pol.CPIFactor *= 1 + cpi
		pol.AnnualPremiumCNY = priceAnnual(policyDefs[kind], w.Age(), pol.CPIFactor)
	}
}

// ── 理赔流(§5) ──

// SettleMedicalClaims 医疗事件理赔(§5.1)。isMajor = cost ≥ 100000
// (与 events.go 既有重疾判定阈值一致,不改事件生成本身)。
//
//	百万医疗(claimable):claim = round(cost × 0.9)
//	  → Pay(insurer→seat, claim, CatClaim, "百万医疗报销")
//	  → 玩家实际自付 = cost × 10%(规则「自付降至 5 千–2 万」的报销制实现)
//	重疾险(claimable 且 isMajor):一次性给付 CoverageCNY(400,000)
//	  → 赔付后保单终止(Active=false,定额给付型单次赔付合同终止,现实一致)
//	等待期(waiting)/失效(lapsed)/未投保:不赔,维持全额自付(事件文本提示等待期)。
func (w *World) SettleMedicalClaims(seat int, cost int64, isMajor bool) {
	if !w.InsuranceEnabled || cost <= 0 {
		return
	}
	p := w.Players[seat]
	if p == nil {
		return
	}
	var texts []string
	// 百万医疗:比例报销 90%,无次数上限。
	if pol := p.Policies[InsMedicalMillion]; pol != nil {
		if pol.claimable(w.Month) {
			claim := int64(float64(cost)*policyDefs[InsMedicalMillion].ReimburseRatio + 0.5)
			if claim > 0 {
				w.Pay(seat, EntityInsurer, SeatEntity(seat), claim, CatClaim, "百万医疗报销")
				pol.ClaimsTotalCNY += claim
				texts = append(texts, fmt.Sprintf("百万医疗报销 ¥%d(自付降至 ¥%d)", claim, cost-claim))
			}
		} else if pol.Status(w.Month) == "waiting" {
			texts = append(texts, "百万医疗险尚在等待期,本次不赔不退")
		}
	}
	// 重疾:确诊(isMajor)一次性定额给付,赔付后合同终止(终身限赔 1 次)。
	if isMajor {
		if pol := p.Policies[InsCriticalIllness]; pol != nil {
			if pol.claimable(w.Month) {
				pay := pol.CoverageCNY
				if pay > 0 {
					w.Pay(seat, EntityInsurer, SeatEntity(seat), pay, CatClaim, "重疾理赔")
					pol.ClaimsTotalCNY += pay
				}
				pol.Active = false
				texts = append(texts, fmt.Sprintf("重疾险一次性给付 ¥%d(保单终止)", pay))
			} else if pol.Status(w.Month) == "waiting" {
				texts = append(texts, "重疾险尚在等待期,本次不赔不退")
			}
		}
	}
	if len(texts) > 0 {
		w.emitEvent("life", seat, fmt.Sprintf("%d 号位保险理赔:%s", seat, strings.Join(texts, ";")))
	}
}

// rollAccident 意外事件掷骰(§5.2,MonthlyEvents 尾部逐存活玩家调用)。
// insurance_enabled=false 时不掷骰(调用方已 gate,零 rand 消费,固定种子对局回归零偏移)。
//
//	触发后:70% 意外伤残 / 30% 意外身故。
//	伤残:医疗支出 U(20,000, 50,000)(CatMedical + §5.1 报销管线,可触发百万医疗)
//	     + 意外险给付 Coverage×0.5 = 250,000(Status=="active" 才赔)
//	     + 精力 −3(clamp ≥ −3) + StoppedMonths 至少 1(与重疾同款近似)。
//	身故:→ HandleDeath(seat, "意外身故")。
func (w *World) rollAccident(seat int) {
	p := w.Players[seat]
	if p == nil || !p.Alive {
		return
	}
	if w.Rand.Float64() >= accidentMonthlyP {
		return
	}
	if w.Rand.Float64() < accidentDeathRatio {
		w.HandleDeath(seat, "意外身故")
		return
	}
	// 意外伤残。
	cost := int64(accidentInjuryLowCNY + w.Rand.Intn(accidentInjurySpanCNY))
	if cost > p.Cash {
		cost = p.Cash // 与健康事件同款:现金不为负(有多少花多少)
	}
	if cost > 0 {
		w.Pay(seat, SeatEntity(seat), w.consumerPayTo(), cost, CatMedical, "意外医疗支出")
	}
	w.SettleMedicalClaims(seat, cost, false)
	var paid int64
	if pol := p.Policies[InsAccident]; pol != nil && pol.Status(w.Month) == "active" {
		paid = int64(float64(pol.CoverageCNY)*accidentDisabilityRatio + 0.5)
		if paid > 0 {
			w.Pay(seat, EntityInsurer, SeatEntity(seat), paid, CatClaim, "意外伤残给付")
			pol.ClaimsTotalCNY += paid
		}
	}
	if paid > 0 {
		w.emitEvent("life", seat, fmt.Sprintf("%d 号位意外伤残,医疗支出 ¥%d,意外险给付 ¥%d,休养 1 个月", seat, cost, paid))
	} else {
		w.emitEvent("life", seat, fmt.Sprintf("%d 号位意外伤残,医疗支出 ¥%d,休养 1 个月", seat, cost))
	}
	p.Energy -= 3
	if p.Energy < -3 {
		p.Energy = -3
	}
	if p.StoppedMonths < 1 {
		p.StoppedMonths = 1
	}
}

// HandleDeath 身故结算唯一入口(§5.3;未来新增死因一律走这里)。
//
//  1. p.Alive=false;p.Ending="accident_death"(结局 id 新增,前端结局表追加)
//  2. 定期寿险(Status=="active"):Pay(insurer→seat, CoverageCNY, CatClaim, "身故理赔-定期寿险")
//     意外险(Status=="active" 且死因含"意外"):Pay(insurer→seat, CoverageCNY, CatClaim, "身故理赔-意外险")
//     等待期(waiting)内身故:不赔不退(消费型,§2.3)。
//     赔付入死者 Cash = 计入遗产池(代际引擎契约 §3.2:总遗产 = Cash+ΣAssets−ΣLoans)。
//  3. 对接代际财富转移引擎:DistributeInheritance(seat)。
//  4. game.event{type:"life"} 公告(game.month 摘要 note 由 settlement 层补)。
//
// 破产出局(eliminate)不是身故,不触发任何寿险赔付(防道德风险套利,P1 新定)。
func (w *World) HandleDeath(seat int, cause string) {
	p := w.Players[seat]
	if p == nil || !p.Alive {
		return
	}
	p.Alive = false
	p.Ending = EndingAccidentDeath
	p.StatusIcon = "idle"
	var texts []string
	var total int64
	if pol := p.Policies[InsTermLife]; pol != nil && pol.Status(w.Month) == "active" {
		w.Pay(seat, EntityInsurer, SeatEntity(seat), pol.CoverageCNY, CatClaim, "身故理赔-定期寿险")
		pol.ClaimsTotalCNY += pol.CoverageCNY
		total += pol.CoverageCNY
		texts = append(texts, fmt.Sprintf("定期寿险给付 ¥%d", pol.CoverageCNY))
	}
	if strings.Contains(cause, "意外") {
		if pol := p.Policies[InsAccident]; pol != nil && pol.Status(w.Month) == "active" {
			w.Pay(seat, EntityInsurer, SeatEntity(seat), pol.CoverageCNY, CatClaim, "身故理赔-意外险")
			pol.ClaimsTotalCNY += pol.CoverageCNY
			total += pol.CoverageCNY
			texts = append(texts, fmt.Sprintf("意外险给付 ¥%d", pol.CoverageCNY))
		}
	}
	// 【代际引擎对接点】《虚拟城市-P1-代际财富转移引擎-v1.md》§3 的
	// DistributeInheritance(seat)(50% 配偶 + 50% 均分子女,丧葬费 ¥5,000 先扣)
	// 尚未接线 —— 赔付留存死者 Cash(insurer→seat 已双式入账,I1 守恒不受影响),
	// 代际引擎落地后在下方调用即可自动纳入遗产池(总遗产 = Cash+ΣAssets−ΣLoans)。
	if len(texts) > 0 {
		w.emitEvent("life", seat, fmt.Sprintf("%d 号位%s,身故理赔合计 ¥%d 已计入遗产", seat, cause, total))
	} else {
		w.emitEvent("life", seat, fmt.Sprintf("%d 号位%s", seat, cause))
	}
}

// ── view / Agent 查询共用(§8.2 / §7.3) ──

// insuredKindsOf Active 保单险种列表(固定顺序;insured_kinds 公开字段;
// insurance_enabled=false 时恒空,§8.2)。
func (w *World) insuredKindsOf(p *Player) []string {
	if !w.InsuranceEnabled || p == nil {
		return nil
	}
	var out []string
	for _, kind := range insuranceKindOrder {
		if pol := p.Policies[kind]; pol != nil && pol.Active {
			out = append(out, kind)
		}
	}
	return out
}

// insuranceQuote 未投保险种当前报价(§7.3:CPIFactor=1 现价,即新投保实付价)。
func (w *World) insuranceQuote(kind string) (annual, coverage int64, ok bool) {
	def, ok := policyDefs[kind]
	if !ok {
		return 0, 0, false
	}
	return priceAnnual(def, w.Age(), 1.0), def.CoverageCNY, true
}

// buildInsuranceJSON 构造 my.insurance 视图段(view.go BuildClientState 调用;
// insurance_enabled=false 时调用方整体 omit)。
func (w *World) buildInsuranceJSON(p *Player) *InsuranceJSON {
	if p == nil {
		return nil
	}
	out := &InsuranceJSON{
		Policies: make([]PolicyJSON, 0, len(p.Policies)),
		Quotes:   make([]QuoteJSON, 0),
	}
	for _, kind := range insuranceKindOrder {
		pol := p.Policies[kind]
		if pol == nil {
			continue
		}
		out.Policies = append(out.Policies, PolicyJSON{
			Kind:              pol.Kind,
			AnnualPremiumCNY:  pol.AnnualPremiumCNY,
			MonthlyPremiumCNY: monthlyPremium(pol.AnnualPremiumCNY),
			CoverageCNY:       pol.CoverageCNY,
			StartMonth:        pol.StartMonth,
			PaidMonths:        pol.PaidMonths,
			WaitingLeft:       pol.waitingLeft(w.Month),
			Status:            pol.Status(w.Month),
			ClaimsTotalCNY:    pol.ClaimsTotalCNY,
		})
		if pol.Active {
			out.MonthlyPremium += monthlyPremium(pol.AnnualPremiumCNY)
		} else if annual, coverage, ok := w.insuranceQuote(kind); ok {
			// 已失效险种同时给报价,引导重新投保。
			out.Quotes = append(out.Quotes, QuoteJSON{Kind: kind, AnnualPremiumCNY: annual, CoverageCNY: coverage})
		}
	}
	for _, kind := range insuranceKindOrder {
		if pol := p.Policies[kind]; pol != nil && pol.Active {
			continue
		}
		if annual, coverage, ok := w.insuranceQuote(kind); ok {
			out.Quotes = append(out.Quotes, QuoteJSON{Kind: kind, AnnualPremiumCNY: annual, CoverageCNY: coverage})
		}
	}
	// 最近 8 张上限(§8.2;map 按 kind 键最多 4 张,防御性截断)。
	if len(out.Policies) > 8 {
		out.Policies = out.Policies[len(out.Policies)-8:]
	}
	return out
}

// InsuranceStatusText Agent get_insurance_status 工具的返回文本
// (§7.3:人读文本 + 结构化 JSON 字符串;4 险种各一行,未投保给当前年龄档报价)。
func (w *World) InsuranceStatusText(p *Player) string {
	var b strings.Builder
	b.WriteString("商业保险:")
	for _, kind := range insuranceKindOrder {
		def := policyDefs[kind]
		pol := p.Policies[kind]
		if pol == nil {
			annual, _, _ := w.insuranceQuote(kind)
			fmt.Fprintf(&b, "\n- %s:未投保(当前报价 年缴¥%d/月缴¥%d)", kindCN(kind), annual, monthlyPremium(annual))
			continue
		}
		fmt.Fprintf(&b, "\n- %s:%s|年缴¥%d(月缴¥%d)|保额¥%d|已缴%d月|累计赔付¥%d",
			kindCN(kind), policyStatusCN(pol.Status(w.Month), pol.waitingLeft(w.Month)),
			pol.AnnualPremiumCNY, monthlyPremium(pol.AnnualPremiumCNY),
			pol.CoverageCNY, pol.PaidMonths, pol.ClaimsTotalCNY)
		if def.ReimburseRatio > 0 {
			fmt.Fprintf(&b, "|报销%.0f%%", def.ReimburseRatio*100)
		}
	}
	if js, ok := json.Marshal(w.buildInsuranceJSON(p)); ok == nil {
		fmt.Fprintf(&b, "\n月缴合计 ¥%d\n%s", w.MonthlyInsurancePremium(p), js)
	}
	return b.String()
}

// MonthlyInsurancePremium 当前 Active 保单月缴合计。
func (w *World) MonthlyInsurancePremium(p *Player) int64 {
	if p == nil {
		return 0
	}
	var total int64
	for _, kind := range insuranceKindOrder {
		if pol := p.Policies[kind]; pol != nil && pol.Active {
			total += monthlyPremium(pol.AnnualPremiumCNY)
		}
	}
	return total
}

// policyStatusCN 保单状态中文。
func policyStatusCN(status string, waitingLeft int) string {
	switch status {
	case "active":
		return "有效"
	case "waiting":
		return fmt.Sprintf("等待期剩%d月", waitingLeft)
	case "grace":
		return "宽限期(保障有效)"
	default:
		return "已失效"
	}
}

// ── 动作(§3;actBuyInsurance / actCancelInsurance,与人类按钮同一 ApplyAction 路径) ──

// actBuyInsurance 投保(§3.1)。校验链顺序锁定:
// 35041(disabled)→ 35006(budget,通用②③由 ApplyAction/房间层前置)→
// 35037(kind)→ 35038(重复投保)→ 35040(年龄门)→ 35007(现金不足)。
// 效果:创建保单 + 当月立即扣缴首月保费(PaidMonths=1,月结跳过当月防双扣),
// 年保费按主时钟年龄档定价(CPIFactor=1.0,年结重定价)。
func (w *World) actBuyInsurance(p *Player, a Action) (string, *errcode.Error) {
	if !w.InsuranceEnabled {
		return "", errcode.Code(errcode.ErrVirtualCityInsuranceDisabled) // 35041
	}
	if p.ActionBudget <= 0 {
		return "", errcode.Code(errcode.ErrVirtualCityActionBudgetExhausted) // 35006
	}
	def, ok := policyDefs[a.Kind]
	if !ok {
		return "", errcode.Code(errcode.ErrVirtualCityInsuranceKindInvalid) // 35037
	}
	if pol := p.Policies[a.Kind]; pol != nil && pol.Active {
		return "", errcode.Code(errcode.ErrVirtualCityInsuranceExists) // 35038
	}
	if w.Age() > insuranceAgeGate {
		return "", errcode.Code(errcode.ErrVirtualCityInsuranceAgeGate) // 35040
	}
	annual := priceAnnual(def, w.Age(), 1.0)
	monthly := monthlyPremium(annual)
	if p.Cash < monthly {
		return "", errcode.Code(errcode.ErrVirtualCityInsufficientCash) // 35007
	}
	if p.Policies == nil {
		p.Policies = make(map[string]*Policy)
	}
	p.Policies[a.Kind] = &Policy{
		Kind:             a.Kind,
		AnnualPremiumCNY: annual,
		CoverageCNY:      def.CoverageCNY,
		StartMonth:       w.Month,
		PaidMonths:       1, // 首月保费即时扣缴
		Active:           true,
		CPIFactor:        1.0,
	}
	w.Pay(p.Seat, SeatEntity(p.Seat), EntityInsurer, monthly, CatPremium, kindCN(a.Kind)+"保费")
	text := fmt.Sprintf("投保%s(年缴 ¥%d,月缴 ¥%d)", kindCN(a.Kind), annual, monthly)
	w.spendBudget(p, "trading", text)
	return text, nil
}

// actCancelInsurance 退保(§3.2)。消费型零现金价值:不退还任何已缴保费;
// 立即 Active=false,次月起不再扣缴。失效(lapsed)保单不可退(35039)。
func (w *World) actCancelInsurance(p *Player, a Action) (string, *errcode.Error) {
	if !w.InsuranceEnabled {
		return "", errcode.Code(errcode.ErrVirtualCityInsuranceDisabled) // 35041
	}
	if p.ActionBudget <= 0 {
		return "", errcode.Code(errcode.ErrVirtualCityActionBudgetExhausted) // 35006
	}
	if _, ok := policyDefs[a.Kind]; !ok {
		return "", errcode.Code(errcode.ErrVirtualCityInsuranceKindInvalid) // 35037
	}
	pol := p.Policies[a.Kind]
	if pol == nil || !pol.Active {
		return "", errcode.Code(errcode.ErrVirtualCityInsuranceNotFound) // 35039
	}
	pol.Active = false
	pol.GraceActive = false
	text := fmt.Sprintf("退保%s(消费型,不退已缴保费 ¥%d)", kindCN(a.Kind), pol.AnnualPremiumCNY*int64(pol.PaidMonths)/12)
	w.spendBudget(p, "trading", text)
	return text, nil
}
