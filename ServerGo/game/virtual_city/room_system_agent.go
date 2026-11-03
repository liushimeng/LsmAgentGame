// Package virtual_city — room_system_agent.go: 批次81 City-System 主 Agent 的
// 只读桥(城市状态摘要 / 世界事件日志快照 / 房间级系统 Agent 线路策略)。
//
// 契约: lag_docs/虚拟城市/已实现/81-CitySystem-Agent与系统对话/01-方案设计.md §3.4。
//
// 消费方: ServerGo/agent/citysystem 的 query_city_state / query_world_events
// 工具(经房间指针只读调用)。
//
// 纪律:
//   - **只读**:本文件全部方法持 r.mu 读取既有状态,**禁止回调房间**(§92a),
//     不得调用 GetStatus()/Engine() 等会再取锁的方法;
//   - **nil 安全**:World / 各子系统 / 房间未开局均返回可读文本,不 panic;
//   - **格式化风格**:复用 agent_runner.go::QueryEconomy 的「指标:值|值」
//     单行摘要口径,不引入平行账本。
package virtual_city

import (
	"fmt"
	"strings"
	"time"
)

// SystemSummaryText 返回城市状态摘要文本(供 LsmAgentGame-City-System 的
// query_city_state 工具消费)。
//
// scope: overview(缺省)| market | labor | treasury | election | all。
// 未知 scope 按 overview 处理。持 r.mu 只读;World 为 nil(未开局)返回提示文本。
func (r *VirtualCityRoom) SystemSummaryText(scope string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	w := r.World
	if w == nil {
		return "城市未开局(World 为空);请先开局后再查询。"
	}
	scope = strings.ToLower(strings.TrimSpace(scope))
	if scope == "" {
		scope = "overview"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "城市状态(M%d):", w.Month)
	b.WriteString("\n")
	// 头部:背景人口 / 座位居民 / 城市时钟(仅 overview|all 展示)。
	if scope == "overview" || scope == "all" {
		clock := "未开局"
		if ms := r.cityClockMsLocked(time.Now()); ms > 0 {
			clock = time.UnixMilli(ms).In(cityEpochTZ).Format("2006-01")
		}
		fmt.Fprintf(&b, "人口 %d · 居民 %d · 月 clock %s",
			r.ResidentCount, r.aliveSeatsLocked(), clock)
		b.WriteString("\n")
	}

	// ── 经济总览(CPI / 失业 / 工资)──
	if scope == "overview" || scope == "all" || scope == "market" || scope == "labor" {
		if w.Goods != nil {
			fmt.Fprintf(&b, "CPI 同比 %.1f%% 环比 %.1f%%", w.Goods.CPIYoY*100, w.Goods.CPIMom*100)
		} else {
			b.WriteString("CPI 未启用")
		}
		if w.Labor != nil {
			fmt.Fprintf(&b, " | 失业率 %.1f%% | 工资增长 %.1f%%",
				w.Labor.Unemployment*100, w.Labor.WageGrowthYoY*100)
		}
		b.WriteString("\n")
	}

	// ── 市场(股指 / 黄金 / 房价)──
	if scope == "overview" || scope == "all" || scope == "market" {
		if w.Market != nil {
			stockChg := ""
			if w.Market.LastStockRawDelta != 0 {
				stockChg = fmt.Sprintf("(%+.1f%%)", w.Market.LastStockRawDelta*100)
			}
			fmt.Fprintf(&b, "股指 %.1f%s 黄金 %.1f 八区房价 平均 %.2f",
				w.Market.StockIndex, stockChg, w.Market.GoldPrice, w.avgDistrictIdxLocked())
		} else {
			b.WriteString("市场未启用")
		}
		b.WriteString("\n")
	}

	// ── 国库 / 货币政策 ──
	if scope == "overview" || scope == "all" || scope == "market" || scope == "treasury" {
		if w.Treasury != nil {
			fmt.Fprintf(&b, "国库 ¥%d(上月收 %d / 支 %d,连续赤字 %d 月)",
				w.Treasury.Cash, w.Treasury.LastMonthRevenue,
				w.Treasury.LastMonthExpense, w.Treasury.DeficitRun)
		} else {
			b.WriteString("国库未启用")
		}
		if w.CB != nil {
			fmt.Fprintf(&b, " | 政策利率 %.2f%% | 信贷窗口 %.2f",
				w.CB.PolicyRate*100, w.CB.CreditWindowFactor)
		}
		b.WriteString("\n")
	}

	// ── 劳动力市场细节 ──
	if scope == "labor" {
		if w.Labor != nil {
			fmt.Fprintf(&b, "就业率 %.1f%% | 平均工资 ¥%d | 企业营收 ¥%d | 裁员潮档位 %d",
				w.Labor.Employment*100, w.Labor.AvgWageCNY,
				w.Labor.RevenueCNY, w.Labor.LayoffWave)
		} else {
			b.WriteString("劳动力市场未启用")
		}
		b.WriteString("\n")
	}

	// ── 选举 ──
	if scope == "election" || scope == "all" {
		if w.Election != nil {
			state := "未启用"
			if w.Election.Enabled {
				state = fmt.Sprintf("已启用(每 %d 月一届,上次 M%d,现任市长座位 %d)",
					w.Election.IntervalMonths, w.Election.LastElectionMonth, w.Election.MayorSeat)
			}
			fmt.Fprintf(&b, "市长选举:%s | 满意度偏置 %+.1f", state, w.Election.SentimentBias)
		} else {
			b.WriteString("市长选举:未启用")
		}
		b.WriteString("\n")
	}

	// ── 活动场景 ──
	if scope == "overview" || scope == "all" {
		n := len(w.Scenarios)
		if n == 0 {
			b.WriteString("活动场景 0 个")
		} else {
			fmt.Fprintf(&b, "活动场景 %d 个:", n)
			for i, sc := range w.Scenarios {
				if sc == nil {
					continue
				}
				name := sc.Kind
				if def, ok := scenarioDefFor(sc.Kind); ok {
					name = def.spec.Name
				}
				if i > 0 {
					b.WriteString(";")
				}
				fmt.Fprintf(&b, " %s L%d(剩 %d 月)", name, sc.Magnitude, sc.RemainingMonths)
			}
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// WorldEventLogView 返回批次80 世界事件日志快照(新→旧,副本)。
// 持 r.mu 只读;World 为 nil 返回空切片(非 nil,JSON 序列化为 [])。
func (r *VirtualCityRoom) WorldEventLogView() []WorldEventRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.World == nil || len(r.World.WorldEventLog) == 0 {
		return []WorldEventRecord{}
	}
	out := make([]WorldEventRecord, len(r.World.WorldEventLog))
	copy(out, r.World.WorldEventLog)
	return out
}

// CitySystemLine 返回房间级「系统 Agent 线路」策略("" | "random" | 固定 key)。
// 持 r.mu 只读。
func (r *VirtualCityRoom) CitySystemLine() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.citySystemLine
}

// SetCitySystemLine 设置房间级线路策略(applyOpts 调用;"" = 沿用服务端配置
// city_system.model_key,"random" = 每轮随机取线,其余 = 固定 model key)。
// 与批次79 的 CityChromeLine 同构,两者互不覆盖。
func (r *VirtualCityRoom) SetCitySystemLine(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.citySystemLine = strings.TrimSpace(line)
}

// aliveSeatsLocked 返回存活座位数(持锁只读)。
func (r *VirtualCityRoom) aliveSeatsLocked() int {
	if r.World == nil {
		return 0
	}
	n := 0
	for _, p := range r.World.Players {
		if p != nil && p.Alive {
			n++
		}
	}
	return n
}

// avgDistrictIdxLocked 八区(全部城区)房价指数平均值(持锁只读;nil → 0)。
func (w *World) avgDistrictIdxLocked() float64 {
	if w == nil || w.Market == nil || len(w.Market.DistrictIdx) == 0 {
		return 0
	}
	sum := 0.0
	for _, idx := range w.Market.DistrictIdx {
		sum += idx
	}
	return sum / float64(len(w.Market.DistrictIdx))
}
