// Package city — calibration.go: 城市校准表(职业卡抽样 → 26 域分布参数)。
//
// 契约: lag_docs/财商流游戏/已实现/03-城市背景模拟/
// 虚拟城市-大规模城市居民背景模拟设计-v1.md §3(2026-09-21)。
//
// CalibTable 是背景居民合成(Backdrop)的数值来源:把 10 万级职业卡池抽样
// 成 26 个 L1 行业域 × 8 城区的紧凑分布参数。构建兜底链 docs → curated →
// synthetic;后台 goroutine 预热(sync.Once,不阻塞启动),房间创建不等预热
// —— 未 Ready 时用合成默认即时建城,Ready 后下一房生效(进行中房间不回填,
// 避免中途分布漂移)。
package city

import (
	"math"
	"math/rand"
	"sync"
	"sync/atomic"

	"LsmAgentGame/game/wealth/profession"
	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// 契约常量(03 §3)。
const (
	domainCount   = 26 // L1 行业域 A..Z
	districtCount = 8  // 城区
	// defaultEmployment 基线就业率(契约默认 0.94)。
	defaultEmployment = 0.94
	// defaultCalibSampleSize 校准抽样卡数默认值(契约 512)。
	defaultCalibSampleSize = 512
	// minDomainSamples docs 模式下进入域统计的最小有效样本数;不足视为
	// docs 不可用,降到 curated。
	minDomainSamples = 16
	// calibSeed 校准抽样固定种子:全进程一次、结果可复现(重启不漂移)。
	calibSeed int64 = 20260921
)

// l1DomainNames 26 个 L1 行业域规范名(与 lag_docs/虚拟城市/玩家职业设计/
// 目录名一一对应;顺序 = 域索引 0..25,首字符即域字母)。
var l1DomainNames = [domainCount]string{
	"A-农林牧渔", "B-采矿与冶金", "C-食品饮料与烟草", "D-纺织服装与鞋帽",
	"E-木材家具与造纸印刷", "F-医药与生物制造", "G-化工与新材料", "H-金属制品与通用机械",
	"I-电子半导体与仪器仪表", "J-汽车与交通装备", "K-能源与电力", "L-建筑与房地产",
	"M-批发零售与商贸流通", "N-交通运输与物流仓储", "O-住宿与餐饮", "P-信息与通信技术",
	"Q-金融与保险", "R-专业服务", "S-科学研究与技术服务", "T-教育与培训",
	"U-医疗健康与社会照护", "V-文化传媒体育与娱乐", "W-公共管理与国防", "X-社会组织与公益慈善",
	"Y-居民生活服务", "Z-新兴交叉职业与其他",
}

// domainIndex 域名 → 0..25;非 26 域名(空串/未知)返回 -1。
// L1 域目录名首字符即大写字母,O(1) 计算,不查表。
func domainIndex(name string) int {
	if len(name) < 2 || name[0] < 'A' || name[0] > 'Z' || name[1] != '-' {
		return -1
	}
	return int(name[0] - 'A')
}

// districtIDIndex 卡 HomeDistrict id → 城区索引(profession.DistrictIDs 顺序,
// 与 game/wealth DistrictDefs 顺序对齐;两处同步由 loader_test 覆盖)。
var districtIDIndex = func() map[string]int {
	ids := profession.DistrictIDs()
	m := make(map[string]int, len(ids))
	for i, id := range ids {
		m[id] = i
	}
	return m
}()

// DomainStat 单个 L1 行业域的分布参数(契约 03 §3.1)。
type DomainStat struct {
	Name             string  // L1 域名(如 "A-农林牧渔")
	Weight           float64 // 域人口权重(全表和为 1)
	IncomeMean       float64 // 月收入均值(元)
	IncomeStd        float64 // 月收入标准差(描述性;合成走 lognormal)
	ExpenseRatioMean float64 // 月支出/月收入
	SavingsMonths    float64 // 储蓄 ≈ 月收入 × 该系数
	AgeMean          float64 // 平均年龄
}

// CalibTable 城市背景居民分布校准表(契约 03 §3.1)。
type CalibTable struct {
	Domains    []DomainStat           // 26 个,按 A..Z 序
	Districts  [districtCount]float64 // 城区人口权重(和为 1)
	Employment float64                // 基线就业率(默认 0.94)
	Source     string                 // "docs" | "curated" | "synthetic"
	Ready      bool                   // 后台预热完成标志
}

// SyntheticCalibTable 内置合成默认(契约 03 §3.2:Weight 均匀、IncomeMean=6800、
// ExpenseRatio=0.72、SavingsMonths=6、AgeMean=36;IncomeStd 无契约约束取
// 0.55×mean 与合成 lognormal σ 同量级)。Ready=false。
func SyntheticCalibTable() *CalibTable {
	t := &CalibTable{
		Domains:    make([]DomainStat, domainCount),
		Employment: defaultEmployment,
		Source:     "synthetic",
		Ready:      false,
	}
	for i := range t.Domains {
		t.Domains[i] = DomainStat{
			Name:             l1DomainNames[i],
			Weight:           1.0 / domainCount,
			IncomeMean:       6800,
			IncomeStd:        6800 * 0.55,
			ExpenseRatioMean: 0.72,
			SavingsMonths:    6,
			AgeMean:          36,
		}
	}
	for i := range t.Districts {
		t.Districts[i] = 1.0 / districtCount
	}
	return t
}

// domainAcc 单域统计累加器。
type domainAcc struct {
	n          int
	sal, salSq float64 // 收入与收入²(方差 = E[X²]−E[X]²)
	ratio      float64 // Σ expense/salary
	savM       float64 // Σ savings/salary
	age        float64 // Σ age
}

// finish 把累加器收敛为 DomainStat;sampleCount==0 时回落 fallback。
func (a *domainAcc) finish(name string, weight float64, fallback DomainStat) DomainStat {
	if a.n == 0 {
		f := fallback
		f.Name = name
		f.Weight = weight
		return f
	}
	mean := a.sal / float64(a.n)
	variance := a.salSq/float64(a.n) - mean*mean
	if variance < 0 {
		variance = 0 // 浮点误差防御
	}
	return DomainStat{
		Name:             name,
		Weight:           weight,
		IncomeMean:       mean,
		IncomeStd:        math.Sqrt(variance),
		ExpenseRatioMean: a.ratio / float64(a.n),
		SavingsMonths:    a.savM / float64(a.n),
		AgeMean:          a.age / float64(a.n),
	}
}

// calibFromSamples 由抽样卡统计构建校准表(docs / curated 共用)。
// 域样本足够的域取实测统计;零样本域回落「全域聚合统计 × 均匀权重」;
// curated 模式(全部 Domain=="")即聚合 × 均匀。城区权重取卡 HomeDistrict
// 频次,零样本回落均匀。Employment 恒为基线默认(卡池无就业状态维度)。
func calibFromSamples(samples []profession.DomainCard, source string) *CalibTable {
	base := SyntheticCalibTable()
	t := &CalibTable{
		Domains:    make([]DomainStat, domainCount),
		Employment: defaultEmployment,
		Source:     source,
		Ready:      true,
	}

	var agg domainAcc
	per := make([]domainAcc, domainCount)
	distCount := make([]int, districtCount)
	distTotal := 0

	for _, s := range samples {
		sal := float64(s.Salary)
		if sal <= 0 {
			continue
		}
		ratio := float64(s.Expense) / sal
		if s.Expense <= 0 {
			ratio = 0.6 // 与 loader 兜底消费率一致
		}
		savM := float64(s.Savings) / sal
		if s.Savings < 0 {
			savM = 0
		}
		age := float64(s.StartAge)
		if age <= 0 {
			age = 25
		}
		acc := func(a *domainAcc) {
			a.n++
			a.sal += sal
			a.salSq += sal * sal
			a.ratio += ratio
			a.savM += savM
			a.age += age
		}
		acc(&agg)
		if di := domainIndex(s.Domain); di >= 0 {
			acc(&per[di])
		}
		if idx, ok := districtIDIndex[s.HomeDistrict]; ok && idx >= 0 && idx < districtCount {
			distCount[idx]++
			distTotal++
		}
	}

	domainSamples := 0
	for i := range per {
		domainSamples += per[i].n
	}
	// 域权重:有域样本按频次归一;否则(curated / 域全不可解析)均匀。
	uniform := false
	if domainSamples == 0 {
		uniform = true
		domainSamples = domainCount // 使下面权重计算走均匀分支
	}
	// 聚合统计作零样本域的回落源。
	aggStat := agg.finish("aggregate", 0, base.Domains[0])
	for i := range t.Domains {
		w := float64(per[i].n) / float64(domainSamples)
		if uniform {
			w = 1.0 / domainCount
		}
		t.Domains[i] = per[i].finish(l1DomainNames[i], w, aggStat)
	}
	if distTotal > 0 {
		for i := range t.Districts {
			t.Districts[i] = float64(distCount[i]) / float64(distTotal)
		}
	} else {
		copy(t.Districts[:], base.Districts[:])
	}
	return t
}

// BuildCalibTable 同步构建校准表,兜底链 docs → curated → synthetic
// (契约 03 §3.2)。导出供测试与预热路径共用。
func BuildCalibTable(loader *profession.Loader, sampleSize int) *CalibTable {
	if loader == nil {
		return SyntheticCalibTable()
	}
	if sampleSize <= 0 {
		sampleSize = defaultCalibSampleSize
	}
	loader.ForceIndex() // 后台路径:walk 全池(以 PoolSize() 实测为准,2026-09-21 实测 100,267)≈2~6s,不阻塞房间创建
	avail, total, _, _ := loader.PoolInfo()
	if avail && total > 0 {
		rng := rand.New(rand.NewSource(calibSeed))
		samples := loader.DrawWithDomain(sampleSize, rng)
		n := 0
		for _, s := range samples {
			if s.Domain != "" {
				n++
			}
		}
		if n >= minDomainSamples {
			return calibFromSamples(samples, "docs")
		}
		logger.L().Warn("city calibration: docs pool samples lack L1 domains, falling back to curated",
			zap.Int("sampled", len(samples)), zap.Int("with_domain", n))
	}
	// curated 兜底:14 张精选卡(无域信息 → 均匀权重 + 聚合统计)。
	curated := profession.CuratedCards()
	pairs := make([]profession.DomainCard, 0, len(curated))
	for _, c := range curated {
		pairs = append(pairs, profession.DomainCard{Card: c})
	}
	return calibFromSamples(pairs, "curated")
}

// ── 进程级全局校准表(后台预热,契约 03 §3.2)──

var (
	globalCalib atomic.Pointer[CalibTable]
	warmOnce    sync.Once
)

// WarmUpCalibration 后台预热全局校准表(sync.Once;不阻塞启动)。
// wealth manager 初始化后由 main 调用一次。
func WarmUpCalibration(loader *profession.Loader, sampleSize int) {
	warmOnce.Do(func() {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logger.L().Error("city calibration warm-up panic", zap.Any("panic", r))
				}
			}()
			t := BuildCalibTable(loader, sampleSize)
			globalCalib.Store(t)
			logger.L().Info("city calibration table ready",
				zap.String("source", t.Source),
				zap.Int("domains", len(t.Domains)),
				zap.Float64("employment", t.Employment))
		}()
	})
}

// CurrentCalibration 返回当前校准表;预热未完成时返回合成默认(Ready=false),
// 已创建的 Backdrop 持有自己的表引用,不受后续预热影响(进行中房间不回填)。
func CurrentCalibration() *CalibTable {
	if t := globalCalib.Load(); t != nil {
		return t
	}
	return SyntheticCalibTable()
}
