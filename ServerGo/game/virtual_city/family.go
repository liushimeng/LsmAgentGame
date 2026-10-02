// Package virtual_city — family.go: 代际财富转移引擎(批次52 §20261002-01)。
//
// 契约: lag_docs/虚拟城市/已实现/52-代际财富转移引擎/
// 虚拟城市-批次52-代际财富转移引擎-实施设计-v1.md(裁决表 D1–D16,下称「批次52」)
// 上游: lag_docs/虚拟城市/已实现/05-P1扩展/虚拟城市-P1-代际财富转移引擎-v1.md
// (P1-3 契约,数值与算法权威;与批次52 裁决冲突以批次52 为准)。
//
// 四条代际财富通道:
//   - 赡养支出(批次52 §2 裁决 D10):月结自动刚性,seat→family CatFamilySupport;
//   - 教育投入(D11):一次性私立升级 + 月度教育费,seat→family CatEduTuition;
//   - 子女回流(D12):子女 ≥22 岁按月反哺,family→seat CatChildSupport;
//   - 遗产继承(D6/D8/D9):HandleDeath 理赔后立即清算,seat→family CatInheritance。
//
// 派生口径(裁决 D1–D5):父母/子女状态由本文件纯函数按 (Player, World.Month, seat)
// 推导,禁用 w.Rand(不扰动既有事件序列);需要跨月累积的量落 Player 持久字段。
package virtual_city

import (
	"fmt"
	"hash/fnv"
	"strings"

	"LsmAgentGame/errcode"
)

// ── 数值常量(批次52 §3.2;数值权威 = P1-3 契约 §4.2/§5.2)──
const (
	ParentsAgeOffset   = 25     // 父母 = 玩家年龄 + 25(裁决 D4)
	ParentsSupportFrom = 60     // 赡养起始父母年龄(D10)
	FamilySupportBase  = 1500   // 基础赡养(元/月)
	FamilySupportFair  = 1.5    // 健康 fair 赡养倍率(D5)
	FamilySupportPoor  = 2.5    // 健康 poor 赡养倍率(D5)
	FamilyAbilityCut   = 0.5    // NetWorth < 全房存活中位数 → ×0.5(D10)
	PrivateEduInitCNY  = 200000 // 私立升级一次性(D11)
	PrivateEduMonthly  = 3000   // 私立月度教育费(≤18 岁;D11)
	EduPrivateROI      = 0.2    // 子女回流加成(私立;D12)
	ChildSupportBase   = 1500   // 子女 ≥22 岁月度回流(D12)
	ChildAdultFrom     = 22     // 子女成年回流起始年龄(D12)
	FuneralCNY         = 5000   // 丧葬费(遗产先扣;D8)

	// 教育档位与年龄窗(裁决 D11:年龄 ∈ [3,18] 且未私立才可升;19 岁起停月费)。
	privateEduAgeFrom = 3
	privateEduAgeTo   = 18
	// 卡面初始子女开局年龄散列域(裁决 D3:seedChildAge ∈ [2,12],长幼有序递减 2 岁)。
	seedChildAgeMin = 2
	seedChildAgeMax = 12
	// 父母寿命散列扫描窗(裁决 D4:min{A ≥ 50};对局 35 年 × 父母 +25 ⇒ A ≤ 130 足够)。
	deathAgeMin     = 50
	deathAgeScanMax = 130
	// InheritanceLog 保留条数(裁决 D15:最近 5 条)。
	inheritLogKeep = 5
	// seedChildAge 的散列盐(与死亡年龄散列域隔离)。
	seedSaltChild = 1000
)

// deathHashThreshold 父母离世年散列阈值 = 0.05×65536(裁决 D4:每年 5% 离世)。
// 整数域取 floor(3276.8) = 3276(hash16 < 阈值 命中)。
const deathHashThreshold = uint32(65536 * 5 / 100) // 3276

// hash16 FNV-1a 32 位折叠到 0..65535(批次52 §3.2)。确定性、禁用 w.Rand ——
// 不消耗 World.Rand 流,固定种子存量对局事件序列零偏移(裁决 D4)。
func hash16(seat, age int) uint32 {
	h := fnv.New32a()
	var b [8]byte
	u := uint64(uint32(seat))<<32 | uint64(uint32(age))
	for i := 0; i < 8; i++ {
		b[i] = byte(u >> (8 * i))
	}
	_, _ = h.Write(b[:])
	v := h.Sum32()
	return (v >> 16) ^ (v & 0xFFFF) // 高 16 位异或折叠
}

// deathAgeOf 父母寿数 = min{A ≥ 50 : hash16(seat, A) < 0.05×65536}(裁决 D4)。
// 「最小 A」保证单调(死了不会复活);130 岁内未命中视为对局期内不离世。
func deathAgeOf(seat int) int {
	for a := deathAgeMin; a <= deathAgeScanMax; a++ {
		if hash16(seat, a) < deathHashThreshold {
			return a
		}
	}
	return deathAgeScanMax + 1
}

// playerAgeNow 玩家当前年龄(主时钟口径)。
// 注:Player.Age 为卡面开局年龄(engine.go newPlayerFromCard 置定后不随主钟推进,
// view 亦按开局年龄展示),故在此补已历年数 —— 批次52 §2 裁决 D4「p.Age 已含
// 主时钟推进」与代码现状的实现偏差注记,父母/子女年龄一律走本函数口径。
func (w *World) playerAgeNow(p *Player) int {
	month := w.Month
	if month < 1 {
		month = 1
	}
	return p.Age + (month-1)/12
}

// parentsAgeOf 父母年龄 = 玩家当前年龄 + 25(裁决 D4)。
func (w *World) parentsAgeOf(p *Player) int {
	return w.playerAgeNow(p) + ParentsAgeOffset
}

// parentsAliveOf 父母在世判定(裁决 D4):ParentsAlive ⟺ ParentsAge < deathAge(seat)。
// 返回 (是否在世, 寿数)。
func (w *World) parentsAliveOf(p *Player) (alive bool, deathAge int) {
	if p == nil {
		return false, 0
	}
	deathAge = deathAgeOf(p.Seat)
	return w.parentsAgeOf(p) < deathAge, deathAge
}

// parentsHealthOf 父母健康档(裁决 D5):good(<62) / fair(62–71) / poor(≥72)。
func parentsHealthOf(parentsAge int) string {
	switch {
	case parentsAge < 62:
		return "good"
	case parentsAge < 72:
		return "fair"
	default:
		return "poor"
	}
}

// parentsHealthCN 健康档中文(view / Agent 查询共用)。
func parentsHealthCN(h string) string {
	switch h {
	case "good":
		return "健康良好"
	case "fair":
		return "健康一般"
	case "poor":
		return "健康欠佳"
	default:
		return h
	}
}

// childCountOf 子女数(裁决 D2:Family.Children 为单一事实来源 = 卡面初始
// Card.ChildrenCount + events.go 生育事件 BirthMonths 追加,不再二次抽样)。
func (w *World) childCountOf(p *Player) int {
	if p == nil {
		return 0
	}
	if p.Family.Children < 0 {
		return 0
	}
	return p.Family.Children
}

// seedChildAge 卡面初始第 idx 个子女的开局年龄(裁决 D3):基准确定性散列
// ∈ [2,12],第 i 个递减 2 岁(长幼有序),下限 0。当前年龄见 childAgeOf。
func seedChildAge(seat, idx int) int {
	base := seedChildAgeMin + int(hash16(seat, seedSaltChild)%(seedChildAgeMax-seedChildAgeMin+1))
	age := base - 2*idx
	if age < 0 {
		age = 0
	}
	return age
}

// childAgeOf 第 idx 个子女当前年龄(裁决 D3):
//   - 卡面初始(idx < Card.ChildrenCount):seedChildAge + 已历年数;
//   - 事件生育:age = (Month − BirthMonths[j]) / 12(既有口径)。
func (w *World) childAgeOf(p *Player, idx int) int {
	if p == nil || idx < 0 {
		return 0
	}
	month := w.Month
	if month < 1 {
		month = 1
	}
	if idx < p.Card.ChildrenCount {
		return seedChildAge(p.Seat, idx) + (month-1)/12
	}
	j := idx - p.Card.ChildrenCount
	if j >= 0 && j < len(p.BirthMonths) {
		m := p.BirthMonths[j]
		if m > month {
			m = month
		}
		return (month - m) / 12
	}
	return 0
}

// childEduOf 第 idx 个子女教育档(裁决 D11:PrivateEduMask 与子女序对齐,
// 卡面初始在前、事件生育在后;未置位 = "public")。
func childEduOf(p *Player, idx int) string {
	if p == nil || idx < 0 || idx >= len(p.PrivateEduMask) || !p.PrivateEduMask[idx] {
		return "public"
	}
	return "private"
}

// childEduCN 教育档中文。
func childEduCN(edu string) string {
	if edu == "private" {
		return "私立"
	}
	return "公立"
}

// netWorthMedianAlive 全房存活座位 NetWorth 中位数(裁决 D10 AbilityAdjust 判据;
// 偶数取两中间值均值;无存活 → 0)。
func (w *World) netWorthMedianAlive() int64 {
	var vals []int64
	for _, seat := range w.alivePlayers() {
		if p := w.Players[seat]; p != nil {
			vals = append(vals, p.NetWorth(w.Market))
		}
	}
	n := len(vals)
	if n == 0 {
		return 0
	}
	// 插入排序(n ≤ 12)。
	for i := 1; i < n; i++ {
		v := vals[i]
		j := i - 1
		for j >= 0 && vals[j] > v {
			vals[j+1] = vals[j]
			j--
		}
		vals[j+1] = v
	}
	if n%2 == 1 {
		return vals[n/2]
	}
	return (vals[n/2-1] + vals[n/2]) / 2
}

// monthlyFamilySupport 刚性赡养额(裁决 D10;月结自动,非动作):
// 父母在世且年龄 ≥60 → 1500 × 健康倍率(fair1.5/poor2.5) × 能力倍率(NetWorth <
// 存活中位数 → 0.5)。仅计算金额,扣款/记账在 settleFamily。
func (w *World) monthlyFamilySupport(p *Player) int64 {
	if p == nil {
		return 0
	}
	alive, _ := w.parentsAliveOf(p)
	if !alive {
		return 0
	}
	parentsAge := w.parentsAgeOf(p)
	if parentsAge < ParentsSupportFrom {
		return 0
	}
	mult := 1.0
	switch parentsHealthOf(parentsAge) {
	case "fair":
		mult = FamilySupportFair
	case "poor":
		mult = FamilySupportPoor
	}
	if p.NetWorth(w.Market) < w.netWorthMedianAlive() {
		mult *= FamilyAbilityCut
	}
	return int64(float64(FamilySupportBase)*mult + 0.5)
}

// monthlyEduTuition 私立月度教育费合计(裁决 D11):PrivateEduMask 且子女年龄
// ≤18 → ¥3,000/孩(19 岁起停止)。
func (w *World) monthlyEduTuition(p *Player) int64 {
	if p == nil {
		return 0
	}
	var total int64
	n := w.childCountOf(p)
	for i := 0; i < n; i++ {
		if childEduOf(p, i) != "private" {
			continue
		}
		if w.childAgeOf(p, i) <= privateEduAgeTo {
			total += PrivateEduMonthly
		}
	}
	return total
}

// monthlyChildSupportIn 子女成年回流(裁决 D12):子女年龄 ≥22 后按月回流
// 1500 × (1 + 0.2 × 私立) 给玩家(收入侧)。
func (w *World) monthlyChildSupportIn(p *Player) int64 {
	if p == nil {
		return 0
	}
	var total int64
	n := w.childCountOf(p)
	for i := 0; i < n; i++ {
		if w.childAgeOf(p, i) < ChildAdultFrom {
			continue
		}
		mult := 1.0
		if childEduOf(p, i) == "private" {
			mult += EduPrivateROI
		}
		total += int64(float64(ChildSupportBase)*mult + 0.5)
	}
	return total
}

// settleFamily 月结代际三通道(批次52 §5;② MonthlyEvents 之后、⑤ LivingExpense
// 之前调用)。family_enabled=false 时零接线(回滚阀)。返回 (赡养, 教育, 回流)。
// 现金流:赡养/教育 seat→family(支出侧),回流 family→seat(收入侧)。
func (w *World) settleFamily(p *Player) (support, edu, childIn int64) {
	if !w.FamilyEnabled || p == nil {
		return 0, 0, 0
	}
	seat := p.Seat
	if s := w.monthlyFamilySupport(p); s > 0 {
		w.Pay(seat, SeatEntity(seat), EntityFamily, s, CatFamilySupport, "赡养父母")
		p.FamilySupportTotalCNY += s
		support = s
	}
	if e := w.monthlyEduTuition(p); e > 0 {
		w.Pay(seat, SeatEntity(seat), EntityFamily, e, CatEduTuition, "子女教育")
		p.EducationTotalCNY += e
		edu = e
	}
	if c := w.monthlyChildSupportIn(p); c > 0 {
		w.Pay(seat, EntityFamily, SeatEntity(seat), c, CatChildSupport, "子女回流")
		p.ChildSupportReceivedCNY += c
		childIn = c
	}
	return support, edu, childIn
}

// FamilyScore 代际贡献分(裁决 D14):
// min(100, (EducationTotalCNY + FamilySupportTotalCNY×0.3) / 20000)。
func (w *World) FamilyScore(p *Player) float64 {
	if p == nil {
		return 0
	}
	raw := (float64(p.EducationTotalCNY) + float64(p.FamilySupportTotalCNY)*0.3) / 20000.0
	if raw > 100 {
		raw = 100
	}
	if raw < 0 {
		raw = 0
	}
	return raw
}

// ── 遗产继承(批次52 §4;裁决 D6/D8/D9) ──

// InheritanceEvent 单条遗产分配记录(裁决 D6/D15)。ToSeats 恒空数组 ——
// 继承人为板外实体(配偶/子女非座位),协议字段保留(裁决 D7)。
type InheritanceEvent struct {
	FromSeat  int   `json:"from_seat"`
	ToSeats   []int `json:"to_seats"`
	AmountCNY int64 `json:"amount_cny"`
	Month     int   `json:"month"`
}

// estateOf 遗产池 = max(0, Cash + ΣAssets.Value − ΣLoans.Balance − 丧葬费)(裁决 D8)。
func (w *World) estateOf(p *Player) int64 {
	if p == nil {
		return 0
	}
	gross := p.Cash
	for i := range p.Assets {
		gross += AssetValue(&p.Assets[i], w.Market)
	}
	for i := range p.Loans {
		gross -= p.Loans[i].Balance
	}
	gross -= FuneralCNY
	if gross < 0 {
		return 0
	}
	return gross
}

// DistributeInheritance 遗产分配(批次52 §4)。HandleDeath 理赔完成后立即调用
// (裁决 D9:唯一死亡入口;破产出局不触发)。
//
//   - estate = max(0, Cash + ΣAssets − ΣLoans − 丧葬费 ¥5,000)(裁决 D8)
//   - 已婚有子女:配偶 50%,剩余 50% 子女均分;已婚无子女:配偶 100%
//   - 无配偶有子女:100% 子女均分;无配偶无子女:100% EntityWorld(遗产充公)
//   - 继承人为板外实体 → Ledger seat→EntityFamily(CatInheritance)(裁决 D6)
//   - Pay 前 clamp 到死者 Cash 可付额度(防透支);金额恒 ≥ 0
//   - 分配完成后清零死者 Cash/Assets/Loans(防资产双花)
//   - InheritanceLog 保留最近 5 条(裁决 D15)
//   - 幂等:账簿已清零时二次调用直接返回,且仅记一次 log
func (w *World) DistributeInheritance(deadSeat int) error {
	p := w.Players[deadSeat]
	if p == nil {
		return fmt.Errorf("distribute inheritance: seat %d not occupied", deadSeat)
	}
	// 幂等哨兵:账簿已清零 → 直接返回(不重复记 log)。
	if p.Cash == 0 && len(p.Assets) == 0 && len(p.Loans) == 0 {
		return nil
	}
	estate := w.estateOf(p)

	// 丧葬费先扣(有多少付多少;入消费侧,保持 I1 现金守恒)。
	cashAvail := p.Cash
	if cashAvail < 0 {
		cashAvail = 0
	}
	funeral := int64(FuneralCNY)
	if funeral > cashAvail {
		funeral = cashAvail
	}
	if funeral > 0 {
		w.Pay(deadSeat, SeatEntity(deadSeat), w.consumerPayTo(), funeral, CatLiving, "丧葬费")
		cashAvail -= funeral
	}

	// 分配方案(裁决 D6:D8 三分支)。
	type share struct {
		to   string
		note string
		amt  int64
	}
	var shares []share
	if estate > 0 {
		n := w.childCountOf(p)
		married := p.Family.Marital == "married"
		switch {
		case married && n == 0:
			shares = append(shares, share{EntityFamily, "遗产继承-配偶", estate})
		case married && n > 0:
			spouseAmt := estate / 2
			shares = append(shares, share{EntityFamily, "遗产继承-配偶", spouseAmt})
			rest := estate - spouseAmt
			for i := 0; i < n; i++ {
				shares = append(shares, share{EntityFamily, fmt.Sprintf("遗产继承-子女%d", i+1), rest / int64(n)})
			}
		case !married && n > 0:
			for i := 0; i < n; i++ {
				shares = append(shares, share{EntityFamily, fmt.Sprintf("遗产继承-子女%d", i+1), estate / int64(n)})
			}
		default: // 无配偶无子女 → EntityWorld(裁决 D6「遗产充公」)。
			shares = append(shares, share{EntityWorld, "遗产充公", estate})
		}
	}

	// 实付:顺序 clamp 到死者 Cash 可付额度(防透支,批次52 §4)。
	var paid int64
	for _, s := range shares {
		amt := s.amt
		if amt > cashAvail {
			amt = cashAvail
		}
		if amt <= 0 {
			continue
		}
		w.Pay(deadSeat, SeatEntity(deadSeat), s.to, amt, CatInheritance, s.note)
		cashAvail -= amt
		paid += amt
	}

	// 清零死者账簿(裁决 D8 步骤 4:防资产被后续逻辑双花)。
	p.Cash = 0
	p.Assets = nil
	p.Loans = nil

	if estate > 0 {
		// InheritanceLog:最近 5 条,降序下发(裁决 D15);ToSeats 恒空(板外,D6)。
		w.InheritanceLog = append(w.InheritanceLog, InheritanceEvent{
			FromSeat: deadSeat, ToSeats: []int{}, AmountCNY: estate, Month: w.Month,
		})
		if len(w.InheritanceLog) > inheritLogKeep {
			w.InheritanceLog = w.InheritanceLog[len(w.InheritanceLog)-inheritLogKeep:]
		}
		toText := "配偶/子女"
		if len(shares) == 1 && shares[0].to == EntityWorld {
			toText = "世界(无继承人,遗产充公)"
		}
		w.emitEvent("life", deadSeat,
			fmt.Sprintf("%d 号位身故,遗产 ¥%d 由%s继承(实付 ¥%d)", deadSeat, estate, toText, paid))
	}
	return nil
}

// planInheritance 遗产分配预览(工具 plan_inheritance;现算不落账,裁决 D13)。
func (w *World) planInheritance(p *Player) string {
	if p == nil {
		return "无座位"
	}
	estate := w.estimateEstate(p)
	if estate <= 0 {
		return "当前无遗产可分配(净资产不足以覆盖丧葬费)。"
	}
	n := w.childCountOf(p)
	var b strings.Builder
	fmt.Fprintf(&b, "遗产预览:净遗产 ¥%d(现金+资产-负债-丧葬费 ¥%d)。", estate, FuneralCNY)
	married := p.Family.Marital == "married"
	switch {
	case married && n == 0:
		fmt.Fprintf(&b, "配偶 100%% = ¥%d。", estate)
	case married && n > 0:
		spouse := estate / 2
		fmt.Fprintf(&b, "配偶 50%% = ¥%d;其余 ¥%d 由 %d 名子女均分(每人约 ¥%d)。",
			spouse, estate-spouse, n, (estate-spouse)/int64(n))
	case !married && n > 0:
		fmt.Fprintf(&b, "无配偶,%d 名子女均分 100%%(每人约 ¥%d)。", n, estate/int64(n))
	default:
		b.WriteString("无配偶无子女,遗产归世界(充公)。")
	}
	b.WriteString("继承人为板外家人,不入座位账簿;实付以身故当月可付现金为限。")
	return b.String()
}

// estimateEstate 遗产预览口径(不扣丧葬费先付动作;与 estateOf 同公式)。
func (w *World) estimateEstate(p *Player) int64 {
	return w.estateOf(p)
}

// ── 动作(裁决 D13:pay_support_extra / upgrade_education) ──

// actPaySupportExtra 自愿加赡养(耗 1 动作预算 + 精力 −1;裁决 D13):
// amount>0 且 ≤ NetWorth×30%;入账 seat→family(CatFamilySupport);
// 累计 FamilySupportTotalCNY / FamilySupportExtraCNY。
func (w *World) actPaySupportExtra(p *Player, a Action) (string, *errcode.Error) {
	if !w.FamilyEnabled {
		return "", errcode.Code(errcode.ErrVirtualCityFamilyDisabled)
	}
	if p.ActionBudget <= 0 {
		return "", errcode.Code(errcode.ErrVirtualCityActionBudgetExhausted)
	}
	if a.AmountCNY <= 0 {
		return "", errcode.CodeMsg(errcode.ErrVirtualCityFamilyInvalid, "加赡养金额必须 > 0")
	}
	cap64 := int64(float64(p.NetWorth(w.Market)) * 0.30)
	if cap64 < 0 {
		cap64 = 0
	}
	if a.AmountCNY > cap64 {
		return "", errcode.CodeMsg(errcode.ErrVirtualCityFamilyInvalid,
			fmt.Sprintf("加赡养超出上限(≤净资产30%%,当前上限 ¥%d)", cap64))
	}
	if p.Cash < a.AmountCNY {
		return "", errcode.Code(errcode.ErrVirtualCityInsufficientCash)
	}
	w.Pay(p.Seat, SeatEntity(p.Seat), EntityFamily, a.AmountCNY, CatFamilySupport, "自愿加赡养")
	p.FamilySupportTotalCNY += a.AmountCNY
	p.FamilySupportExtraCNY += a.AmountCNY
	p.Energy = clamp(p.Energy-1, -3, 10)
	text := fmt.Sprintf("自愿加赡养父母 ¥%d(上限 ¥%d)", a.AmountCNY, cap64)
	w.spendBudget(p, "trading", text)
	return text, nil
}

// actUpgradeEducation 教育升级(耗 1 动作预算;裁决 D11/D13):
// child_idx 合法(年龄 ∈[3,18] 且 public 且 Cash ≥ ¥200,000)→ 一次性扣款、
// 置私立、EducationTotalCNY 累加;参数错/余额不足 → 拒绝并返回原因。
func (w *World) actUpgradeEducation(p *Player, a Action) (string, *errcode.Error) {
	if !w.FamilyEnabled {
		return "", errcode.Code(errcode.ErrVirtualCityFamilyDisabled)
	}
	if p.ActionBudget <= 0 {
		return "", errcode.Code(errcode.ErrVirtualCityActionBudgetExhausted)
	}
	idx := a.ChildIdx
	n := w.childCountOf(p)
	if idx < 0 || idx >= n {
		return "", errcode.CodeMsg(errcode.ErrVirtualCityFamilyInvalid,
			fmt.Sprintf("child_idx 越界(当前 %d 名子女,合法 0..%d)", n, n-1))
	}
	age := w.childAgeOf(p, idx)
	if age < privateEduAgeFrom || age > privateEduAgeTo {
		return "", errcode.CodeMsg(errcode.ErrVirtualCityFamilyInvalid,
			fmt.Sprintf("子女年龄 %d 岁不在教育升级窗(%d–%d 岁)", age, privateEduAgeFrom, privateEduAgeTo))
	}
	if childEduOf(p, idx) == "private" {
		return "", errcode.CodeMsg(errcode.ErrVirtualCityFamilyInvalid, "该子女已是私立")
	}
	if p.Cash < PrivateEduInitCNY {
		return "", errcode.Code(errcode.ErrVirtualCityInsufficientCash)
	}
	if idx >= len(p.PrivateEduMask) {
		// 与子女序对齐扩展(卡面初始在前、事件生育在后;裁决 D11)。
		grown := make([]bool, n)
		copy(grown, p.PrivateEduMask)
		p.PrivateEduMask = grown
	}
	p.PrivateEduMask[idx] = true
	w.Pay(p.Seat, SeatEntity(p.Seat), EntityFamily, PrivateEduInitCNY, CatEduTuition, "子女教育升级-私立")
	p.EducationTotalCNY += PrivateEduInitCNY
	text := fmt.Sprintf("子女教育升级到私立(一次性 ¥%d,月费 ¥%d 至 %d 岁)",
		PrivateEduInitCNY, PrivateEduMonthly, privateEduAgeTo)
	w.spendBudget(p, "trading", text)
	return text, nil
}

// ── 查询文本(工具 family query / plan_inheritance;裁决 D13) ──

// FamilyStatusText 家庭/代际状态人读摘要(工具 family(op=query);裁决 D13)。
func (w *World) FamilyStatusText(p *Player) string {
	if p == nil {
		return "无座位"
	}
	var b strings.Builder
	alive, deathAge := w.parentsAliveOf(p)
	parentsAge := w.parentsAgeOf(p)
	if alive {
		fmt.Fprintf(&b, "父母:在世,年龄 %d 岁,健康%s(%s)。",
			parentsAge, parentsHealthCN(parentsHealthOf(parentsAge)), parentsHealthOf(parentsAge))
	} else {
		fmt.Fprintf(&b, "父母:已故(寿数 %d)。", deathAge)
	}
	n := w.childCountOf(p)
	if n == 0 {
		b.WriteString(" 子女:无。")
	} else {
		fmt.Fprintf(&b, " 子女 %d 名:", n)
		for i := 0; i < n; i++ {
			fmt.Fprintf(&b, " %d 岁(%s)", w.childAgeOf(p, i), childEduCN(childEduOf(p, i)))
		}
		b.WriteString("。")
	}
	fmt.Fprintf(&b, " 本月赡养 ¥%d,本月教育费 ¥%d,本月子女回流 ¥%d。",
		w.monthlyFamilySupport(p), w.monthlyEduTuition(p), w.monthlyChildSupportIn(p))
	fmt.Fprintf(&b, " 累计:赡养 ¥%d(含自愿加赡养 ¥%d)、教育 ¥%d、回流 ¥%d。",
		p.FamilySupportTotalCNY, p.FamilySupportExtraCNY,
		p.EducationTotalCNY, p.ChildSupportReceivedCNY)
	return b.String()
}

// ── 视图(裁决 D15 / 批次52 §7;类型名与 JSON 键见 view.go) ──

// buildMyFamilyJSON 构造 my.family(批次52 §7)。family_enabled=false 或
// insurance_enabled=false 时仅下发基础婚育字段,新增父母/子女明细段 omit
// (渐进增强,旧前端零破坏)。
func (w *World) buildMyFamilyJSON(p *Player) MyFamilyJSON {
	out := MyFamilyJSON{Marital: "single", Children: 0}
	if p == nil {
		return out
	}
	out.Marital = p.Family.Marital
	if out.Marital == "" {
		out.Marital = "single"
	}
	out.Children = p.Family.Children
	if !w.FamilyEnabled || !w.InsuranceEnabled {
		return out
	}
	alive, _ := w.parentsAliveOf(p)
	parentsAge := w.parentsAgeOf(p)
	health := parentsHealthOf(parentsAge)
	out.Parents = &ParentsJSON{
		Alive: alive, Age: parentsAge,
		Health: health, HealthCN: parentsHealthCN(health),
	}
	n := w.childCountOf(p)
	out.Kids = make([]KidJSON, 0, n)
	for i := 0; i < n; i++ {
		edu := childEduOf(p, i)
		out.Kids = append(out.Kids, KidJSON{
			Age: w.childAgeOf(p, i), Education: edu, EducationCN: childEduCN(edu),
		})
	}
	out.SupportCNY = w.monthlyFamilySupport(p)
	out.EduCNY = w.monthlyEduTuition(p)
	out.ChildInCNY = w.monthlyChildSupportIn(p)
	out.Totals = &FamilyTotals{
		Support:       p.FamilySupportTotalCNY,
		Education:     p.EducationTotalCNY,
		ChildReceived: p.ChildSupportReceivedCNY,
	}
	return out
}

// buildFamilyStatsJSON 房间级家庭汇总(裁决 D15;观战者可见)。
// ParentsAliveCount = 父母仍在世的存活玩家户数(裁决 D15「全房仍在世的父母」
// 按户计 —— 父母模型为户级单一 alive 标志,D4)。
func (w *World) buildFamilyStatsJSON() *FamilyStatsJSON {
	if !w.FamilyEnabled {
		return nil
	}
	out := &FamilyStatsJSON{}
	for _, seat := range w.alivePlayers() {
		p := w.Players[seat]
		if p == nil {
			continue
		}
		if alive, _ := w.parentsAliveOf(p); alive {
			out.ParentsAliveCount++
		}
		out.ChildrenCount += w.childCountOf(p)
		for i := 0; i < w.childCountOf(p); i++ {
			if childEduOf(p, i) == "private" {
				out.PrivateEduCount++
			}
		}
	}
	return out
}

// inheritanceLogJSON 遗产日志下发(最近 5 条,降序 = 最新在前;裁决 D15)。
// 双保险:append 侧已截断(DistributeInheritance),此处再 cap 防外部直改。
func (w *World) inheritanceLogJSON() []InheritanceEventJSON {
	if !w.FamilyEnabled || len(w.InheritanceLog) == 0 {
		return nil
	}
	entries := w.InheritanceLog
	if len(entries) > inheritLogKeep {
		entries = entries[len(entries)-inheritLogKeep:]
	}
	out := make([]InheritanceEventJSON, 0, len(entries))
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		toSeats := e.ToSeats
		if toSeats == nil {
			toSeats = []int{}
		}
		out = append(out, InheritanceEventJSON{
			FromSeat: e.FromSeat, ToSeats: toSeats,
			AmountCNY: e.AmountCNY, Month: e.Month,
		})
	}
	return out
}

// familySupportRatioNote 代际贡献分构成摘要(FinalScore report 附注用)。
func familySupportRatioNote(p *Player) string {
	if p == nil || (p.EducationTotalCNY == 0 && p.FamilySupportTotalCNY == 0) {
		return "代际贡献 ¥0。"
	}
	// 避免浮点显示抖动:两位小数。
	return fmt.Sprintf("代际投入 教育 ¥%d + 赡养 ¥%d(×0.3)。",
		p.EducationTotalCNY, p.FamilySupportTotalCNY)
}
