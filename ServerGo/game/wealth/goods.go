// Package wealth — goods.go: 消费品市场 + 内生 CPI + 恩格尔分配(2026-09-16 §财商流P1-2)。
//
// 契约: lag_docs/虚拟城市/已实现/05-P1扩展/虚拟城市-P1-真实经济循环引擎-v1.md §2/§3。
// 纯引擎层:无锁、无 goroutine、无 IO;所有随机性经 w.Rand 驱动(确定性单测)。
// 月度调用方(SettleMonth)持房间锁。
package wealth

import (
	"fmt"
	"math"
	"sort"
)

// ── 八大类消费篮子(§2.1,权重和恒为 1.00)──
//
// goodsOrder 是篮子下发/遍历固定顺序(§2.1 表序;map 迭代序不确定,视图层依赖此序)。
var goodsOrder = []string{"food", "clothing", "housing", "household", "transport", "education", "healthcare", "misc"}

// goodsWeights 篮子权重(§2.1;统计局八大类口径)。
var goodsWeights = map[string]float64{
	"food":       0.30, // 食品烟酒(猪周期扰动 seed)
	"clothing":   0.06, // 衣着
	"housing":    0.20, // 居住(价格向 Market 区租金/房价指数联动)
	"household":  0.06, // 生活用品及服务
	"transport":  0.13, // 交通通信(油价扰动 seed)
	"education":  0.11, // 教育文化娱乐(繁荣期需求旺)
	"healthcare": 0.09, // 医疗保健(随年龄结构上移)
	"misc":       0.05, // 其他用品及服务
}

// goodsCN 八大类中文名(EconomyBrief / query_economy 摘要用)。
var goodsCN = map[string]string{
	"food":       "食品",
	"clothing":   "衣着",
	"housing":    "居住",
	"household":  "生活用品",
	"transport":  "交通",
	"education":  "教育",
	"healthcare": "医疗",
	"misc":       "其他",
}

// 价格更新常量(§2.3,P1 新定)。
const (
	goodsBaselineDemandCNY = 102000 // 基准需求:12 人 × 均值月支出 8500
	goodsSeedProb          = 0.15   // food 猪周期 / transport 油价扰动触发概率
	goodsSeedSwing         = 0.04   // 扰动幅度 ±4%
	goodsPriceRatioMin     = 0.96   // 单月环比下限
	goodsPriceRatioMax     = 1.05   // 单月环比上限
	goodsHousingLinkage    = 0.3    // 居住类 CPI 对房价指数环比的锚定系数
)

// goodsPhaseBeta 产能随周期扩张/收缩系数(§2.3:recovery+0.4/boom+1.0/
// recession−0.6/depression−1.2)。
func goodsPhaseBeta(p CyclePhase) float64 {
	switch p {
	case PhaseRecovery:
		return 0.4
	case PhaseBoom:
		return 1.0
	case PhaseRecession:
		return -0.6
	case PhaseDepression:
		return -1.2
	}
	return 0
}

// GoodsItem 单类消费品市场状态(§2.2 契约,字段名/JSON tag 不可改)。
type GoodsItem struct {
	ID        string  `json:"id"`         // food|clothing|housing|household|transport|education|healthcare|misc
	Weight    float64 `json:"weight"`     // 篮子权重(§2.1)
	PriceIdx  float64 `json:"price_idx"`  // 基期=100
	MomChange float64 `json:"mom_change"` // 上月环比(小数,0.01=+1%)
}

// GoodsMarket 全城消费品市场(挂 World.Goods;引擎纯状态,无锁)。
type GoodsMarket struct {
	Items     map[string]*GoodsItem // 8 类,id → 状态
	CPIYoY    float64               // 12 月滚动年化(小数)
	CPIMom    float64               // 上月环比(小数)
	SupplyIdx map[string]float64    // 产能指数 base=100
	history   []float64             // 月度 CPI 环比历史(内部;len<12 时 CPIYoY 未就绪)
	// lastDistrictIdx 上月末各区房价指数快照(housing 环比联动基准;内部)。
	lastDistrictIdx map[string]float64
	// baselineDemand 各类「正常需求」基准(元/月,内部):首月以契约常量
	// 102000×weight 按房间存活人数折算播种,此后 EMA(α=0.1)跟随房间实际
	// 消费结构 —— 恩格尔份额(§3.4)与篮子权重(§2.1)结构性不一致,固定
	// 基准会让多数品类恒触 0.96 下限(月月 −4% 单边通缩);改为「需求热度 =
	// 当月消费 ÷ 近期常态」后,均衡时 DemandIdx≈100(ratio≈1),档位骤变
	// (全员节俭/奢侈)仍产生 ±clamp 的价格响应,契约 §2.3 的机制形状不变。
	baselineDemand map[string]float64
}

// NewGoodsMarket 构造初始市场(8 类 PriceIdx/SupplyIdx=100,权重 §2.1)。
func NewGoodsMarket() *GoodsMarket {
	g := &GoodsMarket{
		Items:     make(map[string]*GoodsItem, len(goodsOrder)),
		SupplyIdx: make(map[string]float64, len(goodsOrder)),
	}
	for _, id := range goodsOrder {
		g.Items[id] = &GoodsItem{ID: id, Weight: goodsWeights[id], PriceIdx: 100}
		g.SupplyIdx[id] = 100
	}
	return g
}

// CPIYoYReady 是否已积累 12 个月环比(央行混合 CPI 的前置条件,§2.4)。
func (g *GoodsMarket) CPIYoYReady() bool {
	return g != nil && len(g.history) >= 12
}

// goodsPriceRatio 单月价格比 = clamp((DemandIdx/SupplyIdx)^0.4, 0.96, 1.05)。
func goodsPriceRatio(demandIdx, supplyIdx float64) float64 {
	if supplyIdx <= 0 {
		supplyIdx = 1 // 防御:供给指数异常时退回基准
	}
	return clampF(math.Pow(demandIdx/supplyIdx, 0.4), goodsPriceRatioMin, goodsPriceRatioMax)
}

// goodsMoneyEffect 货币传导 = clamp((M2GrowthYoY−RealGDPGrowth)×0.05, −0.005, +0.01)。
// CB 为 nil(P0 回退)时恒 0。
func goodsMoneyEffect(cb *CentralBankState) float64 {
	if cb == nil {
		return 0
	}
	return clampF((cb.M2GrowthYoY-cb.RealGDPGrowth)*0.05, -0.005, 0.01)
}

// goodsHousingMom 八区房价指数环比均值(ΔIdx/Idx;首月无快照 → 0)。
func goodsHousingMom(m *MarketState, g *GoodsMarket) float64 {
	if g == nil || len(g.lastDistrictIdx) == 0 || m == nil {
		return 0
	}
	var sum float64
	n := 0
	for _, d := range DistrictDefs {
		prev, ok := g.lastDistrictIdx[d.ID]
		if !ok || prev <= 0 {
			continue
		}
		cur := m.DistrictIdx[d.ID]
		sum += (cur - prev) / prev
		n++
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

// GoodsMonthStep 月度消费品价格更新(§2.3,P1 新定)。
// 调用时机:SettleMonth ④ 市场漂移之后(插入点 ④.5)——housing 联动需要本月最新
// DistrictIdx。所有随机性经 w.Rand 驱动(确定性单测)。economy_enabled=false 时跳过。
func (w *World) GoodsMonthStep() {
	if !w.EconomyEnabled {
		return
	}
	if w.Goods == nil {
		w.Goods = NewGoodsMarket()
	}
	g := w.Goods
	moneyEffect := goodsMoneyEffect(w.CB)

	// 基准需求:首月播种 = 契约常量(12 人 × 均值月支出 8500)× weight 按房间
	// 存活人数折算;此后 EMA(α=0.1)跟随房间实际消费结构(见 GoodsMarket
	// baselineDemand 注释)。
	if g.baselineDemand == nil {
		alive := 0
		for _, p := range w.Players {
			if p != nil && p.Alive {
				alive++
			}
		}
		if alive <= 0 {
			alive = 12
		}
		g.baselineDemand = make(map[string]float64, len(goodsOrder))
		for _, id := range goodsOrder {
			g.baselineDemand[id] = goodsBaselineDemandCNY * goodsWeights[id] * float64(alive) / 12.0
		}
	}

	for _, id := range goodsOrder {
		it := g.Items[id]
		if it == nil { // 防御:外部注入的残缺市场
			it = &GoodsItem{ID: id, Weight: goodsWeights[id], PriceIdx: 100}
			g.Items[id] = it
		}

		// SupplyIdx = 100 + phaseBeta×12 + N(0,2);food/transport 各 15% 概率 ±4% 扰动。
		supply := 100 + goodsPhaseBeta(w.Market.CyclePhase)*12 + 2*normFloat64(w.Rand)
		if (id == "food" || id == "transport") && w.Rand.Float64() < goodsSeedProb {
			if w.Rand.Float64() < 0.5 {
				supply *= 1 + goodsSeedSwing
			} else {
				supply *= 1 - goodsSeedSwing
			}
		}
		g.SupplyIdx[id] = supply

		// DemandIdx = 当月该类实际消费额 ÷ 基准需求,放大为基期 100 的指数
		// (与 SupplyIdx 同量纲;常态消费 → DemandIdx ≈ SupplyIdx → ratio≈1)。
		var consumed float64
		for _, p := range w.Players {
			if p == nil || !p.Alive {
				continue
			}
			consumed += p.ConsumptionByGoods[id]
		}
		baseline := g.baselineDemand[id]
		if baseline <= 0 {
			baseline = 1
		}
		demandIdx := 100 * consumed / baseline
		// EMA 慢速跟随(α=0.1):常态需求按当期产能水平折算(收敛目标
		// consumed×100/supply)—— 稳态时 DemandIdx→SupplyIdx → priceRatio→1
		// (篮子 CPI 稳定在 ±clamp 内);相位切换/扰动产生有界瞬态,消费档位
		// 骤变(全员节俭/奢侈)产生持续 ±clamp 响应直到新常态收敛。
		g.baselineDemand[id] = 0.9*baseline + 0.1*(100*consumed/supply)

		if id == "housing" {
			// housing 特例:锚定八区房价指数环比均值 × 0.3(租金涨幅通常低于房价)。
			mom := goodsHousingLinkage * goodsHousingMom(w.Market, g)
			next := it.PriceIdx * (1 + mom) * (1 + moneyEffect)
			it.MomChange = next/it.PriceIdx - 1
			it.PriceIdx = next
			continue
		}
		ratio := goodsPriceRatio(demandIdx, supply)
		next := it.PriceIdx * ratio * (1 + moneyEffect)
		it.MomChange = next/it.PriceIdx - 1
		it.PriceIdx = next
	}

	// CPIMom = Σ weight × MomChange;滚动 12 月年化。
	mom := 0.0
	for _, id := range goodsOrder {
		mom += g.Items[id].Weight * g.Items[id].MomChange
	}
	g.CPIMom = mom
	g.history = append(g.history, mom)
	if len(g.history) > 420 {
		g.history = g.history[len(g.history)-420:]
	}
	yoy := 1.0
	start := len(g.history) - 12
	if start < 0 {
		start = 0
	}
	for _, m := range g.history[start:] {
		yoy *= 1 + m
	}
	g.CPIYoY = yoy - 1

	// 快照本月末各区房价指数(下月 housing 环比基准)。
	g.lastDistrictIdx = make(map[string]float64, len(DistrictDefs))
	for _, d := range DistrictDefs {
		g.lastDistrictIdx[d.ID] = w.Market.DistrictIdx[d.ID]
	}
}

// ── 消费档位(§3.2,P1 新定)──

// consumptionLevelMult 档位支出乘数:0 节俭 0.6 / 1 标准 1.0 / 2 精致 1.5 / 3 奢侈 2.2。
var consumptionLevelMult = [4]float64{0.6, 1.0, 1.5, 2.2}

// consumptionLevelEnergy 档位精力效果:节俭 −1 / 标准 0 / 精致 +1 / 奢侈 +2。
var consumptionLevelEnergy = [4]int{-1, 0, 1, 2}

// consumptionLevelCN 档位中文名。
func consumptionLevelCN(level int) string {
	switch level {
	case 0:
		return "节俭"
	case 2:
		return "精致"
	case 3:
		return "奢侈"
	default:
		return "标准"
	}
}

// consumerPayTo 消费类支出的目标实体:economy_enabled=true → firms(企业部门),
// false → world(P0 回退路径,§6.5)。
func (w *World) consumerPayTo() string {
	if w.EconomyEnabled {
		return EntityFirms
	}
	return EntityWorld
}

// ── 恩格尔曲线分配(§3.4,P1 新定)──

// engelShares 恩格尔份额(和恒为 1):
//
//	foodShare       = clamp(0.30 − 0.002×((income−8000)/100), 0.15, 0.45)
//	housingShare    = 0.22 + 0.03×(家庭人数−1)                    // 上限 0.40
//	educationShare  = 0.08 + 0.04×Children                        // 上限 0.25
//	healthcareShare = 0.05 + 0.003×max(0, age−35)                 // 上限 0.15
//	Σ四类 > 0.90 → 等比缩放至 0.90;其余 4 类按默认权重瓜分 remaining。
func engelShares(income int64, familySize, children, age int) map[string]float64 {
	foodShare := clampF(0.30-0.002*(float64(income-8000)/100.0), 0.15, 0.45)
	housingShare := 0.22 + 0.03*float64(familySize-1)
	if housingShare > 0.40 {
		housingShare = 0.40
	}
	eduShare := 0.08 + 0.04*float64(children)
	if eduShare > 0.25 {
		eduShare = 0.25
	}
	healthShare := 0.05 + 0.003*float64(max(0, age-35))
	if healthShare > 0.15 {
		healthShare = 0.15
	}
	if sum := foodShare + housingShare + eduShare + healthShare; sum > 0.90 {
		scale := 0.90 / sum
		foodShare *= scale
		housingShare *= scale
		eduShare *= scale
		healthShare *= scale
	}
	remaining := 1 - (foodShare + housingShare + eduShare + healthShare)
	shares := map[string]float64{
		"food": foodShare, "housing": housingShare,
		"education": eduShare, "healthcare": healthShare,
	}
	for id, def := range map[string]float64{
		"clothing": 0.06, "household": 0.06, "transport": 0.13, "misc": 0.05,
	} {
		shares[id] = def * remaining / 0.30
	}
	return shares
}

// familyBasket 家庭支出的篮子拆分比例(§3.4 表:配偶偏食品/居住、孩偏教育、老人偏医疗)。
var familyBasket = []struct {
	kind  string // spouse | child | elder
	split map[string]float64
}{
	{"spouse", map[string]float64{"food": 0.45, "housing": 0.35, "misc": 0.20}},
	{"child", map[string]float64{"education": 0.55, "food": 0.30, "clothing": 0.15}},
	{"elder", map[string]float64{"healthcare": 0.60, "food": 0.30, "misc": 0.10}},
}

// splitEngel 把总消费额(living+familyLiving)按恩格尔曲线拆到八大类。
// 家庭各项按 §3.4 表拆分(金额口径不变),本人生活按 engelShares 份额分摊;
// 金额 round 到元,凑整残差计入 food —— 保证 Σ ConsumptionByGoods == totalCNY(金额守恒)。
func splitEngel(totalCNY int64, p *Player, age int) map[string]float64 {
	out := map[string]float64{}
	if totalCNY <= 0 || p == nil {
		return out
	}
	// 家庭构成(与 settlement 步骤5 同口径:配偶 2000/每孩 5000/每位老人 1000)。
	spouseCNY, childCNY, elderCNY := 0.0, 0.0, 0.0
	if p.Family.Marital == "married" {
		spouseCNY = float64(livingSpouseCNY)
	}
	childCNY = float64(p.Family.Children) * float64(livingChildCNY)
	elderCNY = float64(p.Card.EldersDependent) * float64(livingElderCNY)
	familyTotal := spouseCNY + childCNY + elderCNY
	own := float64(totalCNY) - familyTotal
	if own < 0 {
		// 防御:外部以小于家庭基数的总额调用 → 家庭拆分等比缩放,保持守恒。
		s := float64(totalCNY) / familyTotal
		spouseCNY *= s
		childCNY *= s
		elderCNY *= s
		own = 0
	}

	shares := engelShares(p.monthlyIncomeEstimate(),
		1+boolToInt(p.Family.Marital == "married")+p.Family.Children,
		p.Family.Children, age)
	for id, share := range shares {
		out[id] = own * share
	}
	add := func(amt float64, split map[string]float64) {
		for id, w := range split {
			out[id] += amt * w
		}
	}
	if spouseCNY > 0 {
		add(spouseCNY, familyBasket[0].split)
	}
	if childCNY > 0 {
		add(childCNY, familyBasket[1].split)
	}
	if elderCNY > 0 {
		add(elderCNY, familyBasket[2].split)
	}

	// round 到元 + 残差计入 food(金额守恒)。
	var sum int64
	for id, v := range out {
		r := math.Round(v)
		out[id] = r
		sum += int64(r)
	}
	if residual := totalCNY - sum; residual != 0 {
		out["food"] += float64(residual)
	}
	return out
}

// economyBrief 一行价格涨跌摘要(GameContext.EconomyBrief / prompt 经济环境段)。
// 形如 "CPI同比2.3% 环比0.2% 失业5.1% 涨幅前二:食品+1.2% 交通+0.8%"。
func economyBrief(g *GoodsMarket, unemployment float64) string {
	if g == nil {
		return ""
	}
	type kv struct {
		id  string
		mom float64
	}
	items := make([]kv, 0, len(goodsOrder))
	for _, id := range goodsOrder {
		if it := g.Items[id]; it != nil {
			items = append(items, kv{id, it.MomChange})
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].mom > items[j].mom })
	b := "CPI同比" + pctCN(g.CPIYoY) + " 环比" + pctCN(g.CPIMom) +
		" 失业" + pctCN(unemployment) + " 涨幅前二:"
	n := 0
	for _, it := range items {
		if n >= 2 {
			break
		}
		if n > 0 {
			b += " "
		}
		// 环比带符号("食品+1.2%" / "交通-0.3%",契约 §7.2 示例样式)。
		b += fmt.Sprintf("%s%+.1f%%", goodsCN[it.id], it.mom*100)
		n++
	}
	return b
}

// pctCN 小数 → "2.3%" 形式(带符号仅用于 MomChange 场景由调用方拼接)。
func pctCN(v float64) string {
	return trimTrailingZeros(fmt.Sprintf("%.1f%%", v*100))
}

// trimTrailingZeros 去掉 "2.0%" 的尾零 → "2%"(摘要更紧凑)。
func trimTrailingZeros(s string) string {
	for len(s) > 1 && s[len(s)-2] == '0' && s[len(s)-1] == '%' {
		s = s[:len(s)-2] + "%"
	}
	return s
}

// boolToInt 布尔转整型(泛型 max/min 与 bool 混用的辅助)。
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// clampF 浮点夹取(§2.4 央行混合 CPI / 恩格尔份额共用)。
func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
