// Package virtual_city — statehash.go: 引擎状态哈希(批次71 §5)
//
// 契约(方案 §5.1):
//
//	StateHash 返回引擎状态的 64 位 FNV-1a 摘要(16 位十六进制字符串)。
//	覆盖范围:玩家账目 / 市场 / 央行 / 国库 / 商品 / 劳动力 / 社会结构 /
////	          产业链 / 集群 / 账本摘要 / 账外摘要。
//	排除范围:transcript / 事件文本 / 观测时间戳 / 任何 LLM 决策产物。
//
// 用途:I5(同 seed 双跑逐月对拍)、D9(运行级采集 state_hash)、
//
//	R0 决策重放缓存的一致性判据(批次 73+)。
//
// ⚠ **零 rand 消费**:本文件绝不触碰 w.Rand —— 新增哈希代码若消费随机数,
// 会移动既有随机序列,破坏存量对局回归(§197 反例教训;方案 §10.1 B10 守护)。
//
// 稳定性纪律(方案 §5.3,六条,逐条遵守):
//  1. 禁止 map 迭代(map 迭代序随机)—— 哈希 map 派生量时只取**标量**
//     (rate / district / self_occupied),或把 key 收集后**排序**再哈希。
//  2. 浮点用 math.Float64bits 取 8 字节,禁止 fmt.Sprintf("%v")。
//  3. 整数用定长小端字节(binary.LittleEndian.PutUint64),禁止字符串拼接。
//  4. 切片顺序:资产按 Kind+district、贷款按 ID、商品按 id、城区按 id
//     **排序后**再哈希。
//  5. 哈希算法固定 FNV-1a 64(不用 hash/maphash —— 它按进程随机播种,
//     跨进程不可比)。
//  6. Ledger.Digest 在 Record 内增量更新(与 Entries 同序),不重算全表。
package virtual_city

import (
	"encoding/binary"
	"fmt"
	"math"
	"sort"
)

// FNV-1a 64 参数(固定;跨进程/跨版本可比)。
const (
	fnvOffset64 uint64 = 14695981039346656037
	fnvPrime64  uint64 = 1099511628211
)

// fnvWriteUint64 把 uint64 按定长小端字节并入摘要。
func fnvWriteUint64(h uint64, v uint64) uint64 {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], v)
	for i := 0; i < 8; i++ {
		h ^= uint64(buf[i])
		h *= fnvPrime64
	}
	return h
}

// fnvWriteInt64 把 int64 按定长小端字节并入摘要。
func fnvWriteInt64(h uint64, v int64) uint64 {
	return fnvWriteUint64(h, uint64(v))
}

// fnvWriteFloat64 把 float64 按 math.Float64bits 的 8 字节并入摘要。
//
// ⚠ 禁止 fmt.Sprintf("%v") 浮点格式化(格式随精度/平台漂移)。
func fnvWriteFloat64(h uint64, v float64) uint64 {
	return fnvWriteUint64(h, math.Float64bits(v))
}

// fnvWriteBool 把 bool(1 字节)并入摘要。
func fnvWriteBool(h uint64, v bool) uint64 {
	if v {
		return fnvWriteUint64(h, 1)
	}
	return fnvWriteUint64(h, 0)
}

// fnvWriteString 把字符串并入摘要(先写长度再写字节,避免拼接歧义)。
func fnvWriteString(h uint64, s string) uint64 {
	h = fnvWriteUint64(h, uint64(len(s)))
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= fnvPrime64
	}
	return h
}

// StateHash 返回引擎状态的 64 位 FNV-1a 摘要(16 位十六进制字符串)。
//
// nil World → 空串(调用方 nil 安全)。零 rand 消费。
func (w *World) StateHash() string {
	if w == nil {
		return ""
	}
	h := fnvNew()
	h = w.hashWorldScope(h)
	h = w.hashPlayersScope(h)
	h = w.hashMarketScope(h)
	h = w.hashCentralBankScope(h)
	h = w.hashTreasuryScope(h)
	h = w.hashGoodsScope(h)
	h = w.hashLaborScope(h)
	h = w.hashSocietyScope(h)
	h = w.hashSupplyChainScope(h)
	h = w.hashLedgerScope(h)
	return fmt.Sprintf("%016x", h)
}

// fnvNew 新摘要初值。
func fnvNew() uint64 { return fnvOffset64 }

// hashWorldScope 主时钟与全局开关。
func (w *World) hashWorldScope(h uint64) uint64 {
	h = fnvWriteInt64(h, int64(w.Month))
	h = fnvWriteInt64(h, int64(w.Age()))
	h = fnvWriteBool(h, w.EconomyEnabled)
	h = fnvWriteBool(h, w.InsuranceEnabled)
	h = fnvWriteBool(h, w.FamilyEnabled)
	h = fnvWriteString(h, string(w.BubbleState))
	h = fnvWriteFloat64(h, w.BubbleLeverage)
	return h
}

// hashPlayersScope 逐座位账目(按 seat 升序;空座写哨兵值)。
//
// 资产按 (Kind, district) 排序、贷款按 ID 排序后再哈希(纪律 4)。
func (w *World) hashPlayersScope(h uint64) uint64 {
	for seat := 0; seat < MaxSeats; seat++ {
		p := w.Players[seat]
		if p == nil {
			h = fnvWriteUint64(h, 0xFFFFFFFFFFFFFFFF) // 空座哨兵
			continue
		}
		h = fnvWriteUint64(h, uint64(seat))
		h = fnvWriteInt64(h, p.Cash)
		h = fnvWriteInt64(h, p.SavingsDeposit)
		h = fnvWriteInt64(h, p.PensionCNY)
		h = fnvWriteInt64(h, int64(p.CreditScore))
		h = fnvWriteInt64(h, int64(p.Energy))
		h = fnvWriteInt64(h, int64(p.Network))
		h = fnvWriteInt64(h, int64(p.Cognition))
		h = fnvWriteInt64(h, int64(p.UnemployedMonths))
		h = fnvWriteBool(h, p.Alive)
		h = fnvWriteString(h, p.Ending)
		h = fnvWriteInt64(h, int64(p.BankruptCount))
		h = fnvWriteString(h, p.District)
		h = fnvWriteInt64(h, int64(p.SalaryBase))

		// 资产:按 (Kind, district) 稳定排序(纪律 1/4)。
		idx := make([]int, len(p.Assets))
		for i := range p.Assets {
			idx[i] = i
		}
		sort.SliceStable(idx, func(a, b int) bool {
			ia, ib := &p.Assets[idx[a]], &p.Assets[idx[b]]
			if ia.Kind != ib.Kind {
				return ia.Kind < ib.Kind
			}
			return ia.AssetDistrict() < ib.AssetDistrict()
		})
		h = fnvWriteUint64(h, uint64(len(idx)))
		for _, i := range idx {
			a := &p.Assets[i]
			h = fnvWriteString(h, a.Kind)
			h = fnvWriteFloat64(h, a.Units)
			h = fnvWriteInt64(h, a.CostCNY)
			h = fnvWriteInt64(h, int64(a.OpenMonth))
			// Extra map 只取派生标量,绝不遍历 map(纪律 1)。
			h = fnvWriteFloat64(h, a.AssetRate())
			h = fnvWriteString(h, a.AssetDistrict())
			h = fnvWriteBool(h, a.IsSelfOccupied())
		}

		// 贷款:按 ID 稳定排序(纪律 4)。
		lidx := make([]int, len(p.Loans))
		for i := range p.Loans {
			lidx[i] = i
		}
		sort.SliceStable(lidx, func(a, b int) bool {
			return p.Loans[lidx[a]].ID < p.Loans[lidx[b]].ID
		})
		h = fnvWriteUint64(h, uint64(len(lidx)))
		for _, i := range lidx {
			l := &p.Loans[i]
			h = fnvWriteString(h, l.ID)
			h = fnvWriteString(h, l.Kind)
			h = fnvWriteInt64(h, l.Principal)
			h = fnvWriteInt64(h, l.Balance)
			h = fnvWriteFloat64(h, l.AnnualRate)
			h = fnvWriteInt64(h, int64(l.MonthsLeft))
		}
	}
	return h
}

// hashMarketScope 市场状态(城区指数按 id 排序)。
func (w *World) hashMarketScope(h uint64) uint64 {
	if w.Market == nil {
		return fnvWriteUint64(h, 0xFFFFFFFFFFFFFFFF)
	}
	m := w.Market
	h = fnvWriteString(h, string(m.CyclePhase))
	h = fnvWriteInt64(h, int64(m.CycleMonthsLeft))
	h = fnvWriteFloat64(h, m.StockIndex)
	h = fnvWriteFloat64(h, m.GoldPrice)
	h = fnvWriteFloat64(h, m.DriftMultiplier)
	h = fnvWriteFloat64(h, m.DriftExtra)
	h = fnvWriteInt64(h, int64(m.BreakerUntilMonth))
	ids := make([]string, 0, len(m.DistrictIdx))
	for id := range m.DistrictIdx {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	h = fnvWriteUint64(h, uint64(len(ids)))
	for _, id := range ids {
		h = fnvWriteString(h, id)
		h = fnvWriteFloat64(h, m.DistrictIdx[id])
	}
	return h
}

// hashCentralBankScope 央行状态(只取平铺字段,不调派生函数)。
func (w *World) hashCentralBankScope(h uint64) uint64 {
	if w.CB == nil {
		return fnvWriteUint64(h, 0xFFFFFFFFFFFFFFFF)
	}
	cb := w.CB
	h = fnvWriteFloat64(h, cb.M0)
	h = fnvWriteFloat64(h, cb.M1)
	h = fnvWriteFloat64(h, cb.M2)
	h = fnvWriteFloat64(h, cb.PolicyRate)
	h = fnvWriteFloat64(h, cb.ReserveRatio)
	h = fnvWriteFloat64(h, cb.RediscountRate)
	h = fnvWriteFloat64(h, cb.CreditTightness)
	h = fnvWriteFloat64(h, cb.LoanQuotaFactor)
	h = fnvWriteFloat64(h, cb.CreditSpread)
	h = fnvWriteFloat64(h, cb.BaseMoney)
	h = fnvWriteFloat64(h, cb.CPI)
	h = fnvWriteFloat64(h, cb.RealGDPGrowth)
	return h
}

// hashTreasuryScope 国库状态。
func (w *World) hashTreasuryScope(h uint64) uint64 {
	if w.Treasury == nil {
		return fnvWriteUint64(h, 0xFFFFFFFFFFFFFFFF)
	}
	t := w.Treasury
	h = fnvWriteInt64(h, t.Cash)
	h = fnvWriteInt64(h, t.BondsOutstanding)
	h = fnvWriteInt64(h, t.LastMonthRevenue)
	h = fnvWriteInt64(h, t.LastMonthExpense)
	h = fnvWriteInt64(h, t.PrevMonthCash)
	h = fnvWriteInt64(h, t.OtherExpense)
	h = fnvWriteInt64(h, t.EduCapital)
	h = fnvWriteInt64(h, t.HealthCapital)
	h = fnvWriteInt64(h, t.InfraCapital)
	return h
}

// hashGoodsScope 商品市场(品类按 id 排序)。
func (w *World) hashGoodsScope(h uint64) uint64 {
	if w.Goods == nil {
		return fnvWriteUint64(h, 0xFFFFFFFFFFFFFFFF)
	}
	g := w.Goods
	h = fnvWriteFloat64(h, g.CPIYoY)
	h = fnvWriteFloat64(h, g.CPIMom)
	ids := make([]string, 0, len(g.Items))
	for id := range g.Items {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	h = fnvWriteUint64(h, uint64(len(ids)))
	for _, id := range ids {
		it := g.Items[id]
		if it == nil {
			h = fnvWriteUint64(h, 0xFFFFFFFFFFFFFFFF)
			continue
		}
		h = fnvWriteString(h, id)
		h = fnvWriteFloat64(h, it.Weight)
		h = fnvWriteFloat64(h, it.PriceIdx)
		h = fnvWriteFloat64(h, it.MomChange)
	}
	sids := make([]string, 0, len(g.SupplyIdx))
	for id := range g.SupplyIdx {
		sids = append(sids, id)
	}
	sort.Strings(sids)
	h = fnvWriteUint64(h, uint64(len(sids)))
	for _, id := range sids {
		h = fnvWriteString(h, id)
		h = fnvWriteFloat64(h, g.SupplyIdx[id])
	}
	return h
}

// hashLaborScope 劳动力与企业部门。
func (w *World) hashLaborScope(h uint64) uint64 {
	if w.Labor == nil {
		return fnvWriteUint64(h, 0xFFFFFFFFFFFFFFFF)
	}
	l := w.Labor
	h = fnvWriteFloat64(h, l.Unemployment)
	h = fnvWriteFloat64(h, l.Employment)
	h = fnvWriteInt64(h, l.RevenueCNY)
	h = fnvWriteInt64(h, l.WageBillCNY)
	h = fnvWriteInt64(h, l.AvgWageCNY)
	h = fnvWriteFloat64(h, l.WageGrowthYoY)
	h = fnvWriteInt64(h, int64(l.LayoffWave))
	return h
}

// hashSocietyScope 社会结构统计(不含 LorenzPoints/PyramidLayers 切片 ——
// 顺序不保证稳定;核心口径已在 TotalWealth/MedianWealth 中体现)。
func (w *World) hashSocietyScope(h uint64) uint64 {
	if w.Society == nil {
		return fnvWriteUint64(h, 0xFFFFFFFFFFFFFFFF)
	}
	s := w.Society
	h = fnvWriteFloat64(h, s.Gini)
	for _, c := range s.Circles {
		h = fnvWriteInt64(h, int64(c))
	}
	h = fnvWriteInt64(h, s.TotalWealth)
	h = fnvWriteInt64(h, s.MedianWealth)
	h = fnvWriteInt64(h, s.MeanWealth)
	return h
}

// hashSupplyChainScope 产业链 + 产业集群(节点/集群按 id 排序)。
func (w *World) hashSupplyChainScope(h uint64) uint64 {
	if w.SupplyChain != nil {
		sc := w.SupplyChain
		for _, v := range sc.PriceIndex {
			h = fnvWriteFloat64(h, v)
		}
		ids := make([]string, 0, len(sc.Nodes))
		for id := range sc.Nodes {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		h = fnvWriteUint64(h, uint64(len(ids)))
		for _, id := range ids {
			n := sc.Nodes[id]
			if n == nil {
				h = fnvWriteUint64(h, 0xFFFFFFFFFFFFFFFF)
				continue
			}
			h = fnvWriteString(h, id)
			h = fnvWriteInt64(h, int64(n.Tier))
			h = fnvWriteFloat64(h, n.Capacity)
			h = fnvWriteFloat64(h, n.Inventory)
			h = fnvWriteFloat64(h, n.UnitCost)
			h = fnvWriteFloat64(h, n.Utilization)
			h = fnvWriteFloat64(h, n.LastDemand)
			h = fnvWriteFloat64(h, n.LastShipped)
			h = fnvWriteFloat64(h, n.FillRatio)
		}
	} else {
		h = fnvWriteUint64(h, 0xFFFFFFFFFFFFFFFF)
	}
	// 集群:按 ID 排序。
	cids := make([]string, 0, len(w.Clusters))
	byID := map[string]*IndustrialCluster{}
	for _, c := range w.Clusters {
		if c == nil {
			continue
		}
		cids = append(cids, c.ID)
		byID[c.ID] = c
	}
	sort.Strings(cids)
	h = fnvWriteUint64(h, uint64(len(cids)))
	for _, id := range cids {
		c := byID[id]
		h = fnvWriteString(h, c.ID)
		h = fnvWriteInt64(h, int64(c.Firms))
		h = fnvWriteFloat64(h, c.TaxBreakUsed)
		h = fnvWriteInt64(h, c.LastBreakCNY)
		h = fnvWriteInt64(h, int64(c.TaxBreakMonths))
	}
	return h
}

// hashLedgerScope 账本摘要 + 账外摘要。
func (w *World) hashLedgerScope(h uint64) uint64 {
	if w.Ledger != nil {
		h = fnvWriteUint64(h, w.Ledger.Digest)
		h = fnvWriteInt64(h, int64(len(w.Ledger.Entries)))
	} else {
		h = fnvWriteUint64(h, 0xFFFFFFFFFFFFFFFF)
	}
	if w.OffLedger != nil {
		h = fnvWriteUint64(h, w.OffLedger.Digest)
		h = fnvWriteInt64(h, w.OffLedger.Total())
	} else {
		h = fnvWriteUint64(h, 0xFFFFFFFFFFFFFFFF)
	}
	return h
}
