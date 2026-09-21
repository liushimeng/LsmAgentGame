// Package wealth — industrial_cluster.go: 产业集群子系统(阶段5)
// 2026-09-21 §城市扩张v2.12 阶段5。
//
// 6 个产业集群对应 16 城区中的产业型城区(districts.go v2.12 阶段2 扩展区):
// 入驻企业数随经济周期波动(繁荣 +5%/月、萧条 −3%/月,clamp);集群内企业
// 增值税减免 20%(月上限 = TaxBreakCap/12)。
//
// R5-3 减免上限:TaxBreakMonths ≥ 12(年上限)或累计减免 > TaxBreakCap
// (总额上限,默认 50 万元)时本年停止减免;每年 1 月额度重置。
//
// Treasury 联动(SettleMonth ⑨F,位于 SettleTreasuryMonth **之后**):
// 减免额从 CollectMonthTax 已汇总的本月 VatTotal 中**负向扣除** —— 不直接动
// Treasury.Cash(政府让利记为收入减项;与 treasury.go「宏口径课税只入国库、
// 不逐笔回溯」的聚合清算口径一致)。
//
// 纯引擎层:无锁、无 goroutine、无 IO、**零 rand 消费**(企业数波动走周期
// 阶段确定性公式)—— 固定种子存量对局回归零偏移(§197/treasury.go 同款纪律)。
package wealth

import "fmt"

// 产业集群常量(阶段5 新定)。
const (
	// ClusterTaxBreakRate 集群内企业增值税减免比例(20%)。
	ClusterTaxBreakRate = 0.20
	// ClusterTaxBreakCapDefault 单集群年度减免总额上限(万元,默认 50)。
	ClusterTaxBreakCapDefault = 50.0
	// ClusterTaxBreakMonthsCap 年度减免月数上限(R5-3:≥12 停止本年减免)。
	ClusterTaxBreakMonthsCap = 12
	// ClusterFirmsMin / ClusterFirmsMax 入驻企业数 clamp 边界。
	ClusterFirmsMin = 10
	ClusterFirmsMax = 500
	// ClusterBoomGrowth / ClusterDepressionShrink 企业数月度波动率
	// (繁荣 +5%/月、萧条 −3%/月;复苏/衰退持平 —— 仅两端相位有迁徙信号)。
	ClusterBoomGrowth       = 0.05
	ClusterDepressionShrink = -0.03
)

// IndustrialCluster 产业集群(对应 16 城区中的产业型城区)。
type IndustrialCluster struct {
	ID             string   // 集群 id(如 "hightech_park")
	Name           string   // 中文名
	DistrictID     string   // 所在城区(id 与 districts.go DistrictDefs 对齐)
	Industries     []string // 主导产业(26 L1 域规范名)
	TaxBreakMonths int      // 已享税收减免月数(年上限 12,R5-3;每年 1 月重置)
	TaxBreakCap    float64  // 总减免金额上限(万元,默认 50)
	Firms          int      // 入驻企业数

	// ── 动态状态(MonthlyClusterStep 维护;快照/测试用)──
	TaxBreakUsed float64 // 累计已减免(万元;≥ TaxBreakCap 停止,R5-3)
	LastBreakCNY int64   // 上月实际减免额(元;VatTotal 负向扣除量)
	capNotified  bool    // 本年额度用尽事件是否已播报(防每月重复刷屏)
}

// industrialClusterSpecs 6 集群静态表(DistrictID 与 districts.go 阶段2
// 扩展区一一对应;Industries 用 26 L1 域规范名,与 supply_chain.go 节点
// 行业同源 —— 链上虚拟企业按主导产业归入对应集群辖区)。
var industrialClusterSpecs = []struct {
	id, name, district string
	industries         []string
	firms              int
}{
	{"hightech_park", "高新园区集群", "hightech_park",
		[]string{"I-电子半导体与仪器仪表", "P-信息与通信技术"}, 120},
	{"industrial_park", "工业园区集群", "industrial_park",
		[]string{"H-金属制品与通用机械", "G-化工与新材料"}, 150},
	{"logistics_port", "物流港集群", "logistics_port",
		[]string{"N-交通运输与物流仓储", "M-批发零售与商贸流通"}, 100},
	{"edu_district", "教育园区集群", "edu_district",
		[]string{"T-教育与培训", "S-科学研究与技术服务"}, 60},
	{"medical_city", "医疗城集群", "medical_city",
		[]string{"F-医药与生物制造", "U-医疗健康与社会照护"}, 80},
	{"cultural_creative", "文创区集群", "cultural_creative",
		[]string{"V-文化传媒体育与娱乐", "Z-新兴交叉职业与其他"}, 50},
}

// NewIndustrialClusters 构造 6 集群(顺序与 industrialClusterSpecs 固定)。
func NewIndustrialClusters() []*IndustrialCluster {
	out := make([]*IndustrialCluster, 0, len(industrialClusterSpecs))
	for _, s := range industrialClusterSpecs {
		out = append(out, &IndustrialCluster{
			ID: s.id, Name: s.name, DistrictID: s.district,
			Industries:  append([]string{}, s.industries...),
			TaxBreakCap: ClusterTaxBreakCapDefault,
			Firms:       s.firms,
		})
	}
	return out
}

// ClusterMonthStep 全部集群月度调度(SettleMonth ⑨F 钩子)。
// 零 rand;economy_enabled=false 或无集群时整体跳过。totalFirms 为 6 集群
// 企业数合计(集群增值税份额分母),由本方法先聚合再逐个调度。
func (w *World) ClusterMonthStep() {
	if w == nil || !w.EconomyEnabled || len(w.Clusters) == 0 {
		return
	}
	totalFirms := 0
	for _, c := range w.Clusters {
		totalFirms += c.Firms
	}
	for _, c := range w.Clusters {
		c.MonthlyClusterStep(w, totalFirms)
	}
}

// MonthlyClusterStep 单集群月度调度:
//
//	① 年初重置(w.Month%12==1):TaxBreakMonths/TaxBreakUsed/capNotified
//	   清零(R5-3 年度额度:月数与金额上限均按年滚动)。
//	② 企业数随周期:繁荣 +5%/月、萧条 −3%/月、其余持平,clamp [10,500]。
//	③ 税收减免:集群份额 = Firms/totalFirms;gross = VatTotal×份额×20%;
//	   relief = min(gross, 月上限 Cap×1e4/12, 年剩余额度);R5-3 触顶 → 0。
func (c *IndustrialCluster) MonthlyClusterStep(w *World, totalFirms int) {
	if c == nil || w == nil {
		return
	}
	// ① 年初重置(R5-3 年度额度:月数 + 金额上限均按年滚动;否则累计上限
	// 一旦触顶,集群整个 420 月对局再也不享受减免,失去「年度税收优惠」语义)。
	if w.Month > 0 && w.Month%12 == 1 {
		c.TaxBreakMonths = 0
		c.TaxBreakUsed = 0
		c.capNotified = false
	}

	// ② 企业数随经济周期波动(确定性,零 rand)。
	rate := 0.0
	switch w.Market.CyclePhase {
	case PhaseBoom:
		rate = ClusterBoomGrowth
	case PhaseDepression:
		rate = ClusterDepressionShrink
	}
	if rate != 0 {
		c.Firms = clamp(int(float64(c.Firms)*(1+rate)+0.5), ClusterFirmsMin, ClusterFirmsMax)
	}

	// ③ 税收减免(从本月 VatTotal 负向扣除,不动 Treasury.Cash)。
	c.LastBreakCNY = 0
	vat := int64(0)
	if w.Treasury != nil && w.Treasury.MonthlyCounter != nil {
		vat = w.Treasury.MonthlyCounter.VatTotal
	}
	if vat <= 0 || totalFirms <= 0 {
		return // 无税基/无企业:本月零减免。
	}
	gross := float64(vat) * (float64(c.Firms) / float64(totalFirms)) * ClusterTaxBreakRate
	relief := gross
	if monthlyCap := c.TaxBreakCap * 10000 / float64(ClusterTaxBreakMonthsCap); relief > monthlyCap {
		relief = monthlyCap // 月上限 = Cap/12(万元 → 元)。
	}
	if remain := (c.TaxBreakCap - c.TaxBreakUsed) * 10000; relief > remain {
		relief = remain // 年剩余额度(万元 → 元)。
	}
	if relief < 0 {
		relief = 0
	}
	// R5-3:年 12 月上限 / 总额上限触顶 → 本年停止减免(一次性播报)。
	if c.TaxBreakMonths >= ClusterTaxBreakMonthsCap || c.TaxBreakUsed >= c.TaxBreakCap {
		if !c.capNotified {
			w.emitEvent("policy", -1, fmt.Sprintf(
				"%s税收减免额度用尽(已减免 %d 个月 / %.1f 万元),本年停止新增减免",
				c.Name, c.TaxBreakMonths, c.TaxBreakUsed))
			c.capNotified = true
		}
		return
	}
	if relief <= 0 {
		return
	}
	amt := int64(relief + 0.5)
	if vatr := w.Treasury.MonthlyCounter.VatTotal; amt > vatr {
		amt = vatr // 防御:负向扣除不把 VatTotal 扣成负数。
	}
	if amt <= 0 {
		return
	}
	w.Treasury.MonthlyCounter.VatTotal -= amt
	c.TaxBreakMonths++
	c.TaxBreakUsed += float64(amt) / 10000.0
	c.LastBreakCNY = amt
}

// ClustersJSONFrom 集群数组 → 视图 JSON(view.go BuildClientState 调用)。
func ClustersJSONFrom(clusters []*IndustrialCluster) []ClusterJSON {
	out := make([]ClusterJSON, 0, len(clusters))
	for _, c := range clusters {
		if c == nil {
			continue
		}
		out = append(out, ClusterJSON{
			ID: c.ID, Name: c.Name, DistrictID: c.DistrictID,
			DistrictName:    DistrictCN(c.DistrictID),
			Industries:      append([]string{}, c.Industries...),
			Firms:           c.Firms,
			TaxBreakMonths:  c.TaxBreakMonths,
			TaxBreakUsedWan: c.TaxBreakUsed,
			TaxBreakCapWan:  c.TaxBreakCap,
			LastBreakCNY:    c.LastBreakCNY,
		})
	}
	return out
}
