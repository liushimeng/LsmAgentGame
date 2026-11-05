// Package virtual_city — room_metric_history.go: 批次84 城市指标历史环
// (2026-11-05 §批次84 全维度事件体系与 Agent 双向对话)。
//
// 契约: lag_docs/虚拟城市/已实现/84-全维度事件体系与Agent双向对话/01-方案设计.md §4.4。
//
// 定位:让 LsmAgentGame-City-System 的 query_metric_history 工具能看「趋势」
// (而非只有当前值)。月结成功后采样 CPI / 失业率 / 平均工资 / 股指 / 黄金 /
// 八区房价均值 / 国库现金 / 满意度偏置 8 项指标,存房间层环形缓冲(≤120 条)。
//
// ⚠ **不放进 World**(零新增持久化状态):本环是房间层内存态,
// 重启即清空;World 状态字段与月结语义零改动。
//
// 纪律(照抄 room_system_agent.go):
//   - **只读**:SystemMetricHistory 持 r.mu 只读、返回副本,
//     **禁止回调房间**(§92a),不得调用 GetStatus()/Engine() 等会再取锁的方法;
//   - **nil 安全**:World / 各子系统 / 未采样均返回空切片,不 panic;
//   - **参数夹取**:months 夹在 1..60;metric 非法 ⇒ 返回空切片。
package virtual_city

// SystemMetricSnapshot 单月城市指标快照(契约 §4.4)。
//
// 字段量纲(与 room_system_agent.go 的摘要口径一致):
//   - Month:主钟月份(World.Month);
//   - CPI:年化同比(小数,0.03 = 3%);
//   - Unemployment:失业率(小数,0.05 = 5%);
//   - Wage:平均工资(元);
//   - Stock:股指点位;
//   - Gold:黄金价格;
//   - House:八区房价指数均值;
//   - Treasury:国库现金(元);
//   - Satisfaction:选举满意度偏置(百分点)。
type SystemMetricSnapshot struct {
	Month         int     `json:"month"`
	CPI           float64 `json:"cpi"`
	Unemployment  float64 `json:"unemployment"`
	Wage          float64 `json:"wage"`
	Stock         float64 `json:"stock"`
	Gold          float64 `json:"gold"`
	House         float64 `json:"house"`
	Treasury      float64 `json:"treasury"`
	Satisfaction  float64 `json:"satisfaction"`
}

// 指标历史环常量(契约 §4.4)。
const (
	// systemMetricHistoryCap 环形缓冲容量(120 个月 ≈ 10 年)。
	systemMetricHistoryCap = 120
	// SystemMetricHistoryMaxMonths 查询月数上限(60)。
	SystemMetricHistoryMaxMonths = 60
)

// SystemMetricNames 合法 metric 名单(query_metric_history 工具的枚举同源)。
//
// 顺序即工具 schema 的展示顺序;**改动必须同步** agent/citysystem 的工具枚举。
var SystemMetricNames = []string{
	"cpi", "unemployment", "wage", "stock", "gold", "house", "treasury", "satisfaction",
}

// systemMetricNameValid 报告 metric 名是否合法(空串也非法)。
func systemMetricNameValid(metric string) bool {
	for _, m := range SystemMetricNames {
		if m == metric {
			return true
		}
	}
	return false
}

// sampleSystemMetricLocked 月结成功后采样一次(持锁;调用方持 r.mu)。
//
// 采样点:room.trySettle 月结完成之后(World.Month 已推进)。
// World 为 nil / 各子系统缺失时按 0 记录(不 panic)。
func (r *VirtualCityRoom) sampleSystemMetricLocked() {
	w := r.World
	if w == nil {
		return
	}
	snap := SystemMetricSnapshot{Month: w.Month}
	if w.Goods != nil {
		snap.CPI = w.Goods.CPIYoY
	}
	if w.Labor != nil {
		snap.Unemployment = w.Labor.Unemployment
		snap.Wage = float64(w.Labor.AvgWageCNY)
	}
	if w.Market != nil {
		snap.Stock = w.Market.StockIndex
		snap.Gold = w.Market.GoldPrice
		snap.House = w.avgDistrictIdxLocked()
	}
	if w.Treasury != nil {
		snap.Treasury = float64(w.Treasury.Cash)
	}
	if w.Election != nil {
		snap.Satisfaction = w.Election.SentimentBias
	}
	r.metricHistory = append(r.metricHistory, snap)
	if len(r.metricHistory) > systemMetricHistoryCap {
		r.metricHistory = r.metricHistory[len(r.metricHistory)-systemMetricHistoryCap:]
	}
}

// SystemMetricHistory 返回指定指标的历史时间序列(只读;返回副本)。
//
// 契约 §4.4 / §4.2 query_metric_history 工具的数据源:
//   - metric 必须 ∈ SystemMetricNames(非法 ⇒ 返回空切片);
//   - months 夹在 1..60(≤0 ⇒ 1;>60 ⇒ 60);
//   - 返回值按时间**从旧到新**排列,每个元素只填充请求的指标字段
//     (Month + 对应 metric 字段),其余字段为零值 —— 调用方按单指标序列消费;
//   - 持 r.mu 只读、返回副本;**不回调房间**(§92a)。
func (r *VirtualCityRoom) SystemMetricHistory(metric string, months int) []SystemMetricSnapshot {
	if r == nil || !systemMetricNameValid(metric) {
		return []SystemMetricSnapshot{}
	}
	if months <= 0 {
		months = 1
	}
	if months > SystemMetricHistoryMaxMonths {
		months = SystemMetricHistoryMaxMonths
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	n := len(r.metricHistory)
	if n == 0 {
		return []SystemMetricSnapshot{}
	}
	start := 0
	if n > months {
		start = n - months
	}
	out := make([]SystemMetricSnapshot, 0, n-start)
	for _, snap := range r.metricHistory[start:] {
		// 单指标视图:只保留请求的 metric 字段,其余置零。
		one := SystemMetricSnapshot{Month: snap.Month}
		switch metric {
		case "cpi":
			one.CPI = snap.CPI
		case "unemployment":
			one.Unemployment = snap.Unemployment
		case "wage":
			one.Wage = snap.Wage
		case "stock":
			one.Stock = snap.Stock
		case "gold":
			one.Gold = snap.Gold
		case "house":
			one.House = snap.House
		case "treasury":
			one.Treasury = snap.Treasury
		case "satisfaction":
			one.Satisfaction = snap.Satisfaction
		}
		out = append(out, one)
	}
	return out
}

// SystemMetricHistoryLen 返回已采样月份数(只读;调试/单测用)。
func (r *VirtualCityRoom) SystemMetricHistoryLen() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.metricHistory)
}
