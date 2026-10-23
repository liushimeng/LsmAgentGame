// Package virtual_city — world_event_log.go: 批次80 世界事件反馈层
// (事件 → 即时效果 → 城市结果 因果链留痕)。
//
// 契约出处:lag_docs/虚拟城市/已实现/80-真实世界事件自动注入引擎/01-方案设计.md
// §3.4(反馈层)。
//
// 三个世界状态(挂 World,惰性初始化,nil 安全):
//   - WorldEventLog  事件日志环(≤50,新→旧;含未注入事件);
//   - WorldEventSeen 幂等指纹集合(同一事件只处理一次:注入或被安全阀拦下);
//   - 其余(WorldEventAutoEnabled / MaxPerMonth / MaxMagnitude)为房间级安全阀
//     配置,由房间层 Start 时回写(见 room_world_event.go)。
//
// 纪律:零 rand —— 本文件绝不触碰 w.Rand(存量对局回归零偏移,§197)。
package virtual_city

// WorldEventOutcome 次月城市结果快照(因果链终点)。
type WorldEventOutcome struct {
	Month             int     `json:"month"`
	CPIYoY            float64 `json:"cpi_yoy"`
	Unemployment      float64 `json:"unemployment_rate"`
	StockIndex        float64 `json:"stock_index"`
	GoldPrice         float64 `json:"gold_price"`
	GoodsPriceIdx     float64 `json:"goods_price_index"`
	ResidentCashTotal float64 `json:"resident_cash_total"`
}

// WorldEventRecord 事件记录(World.WorldEventLog 环 ≤50,新→旧)。
type WorldEventRecord struct {
	Key           string             `json:"key"`
	Rule          string             `json:"rule"`
	Kind          string             `json:"kind"`
	Severity      int                `json:"severity"`
	Direction     string             `json:"direction"`
	Scope         string             `json:"scope"`
	Title         string             `json:"title"`
	Detail        string             `json:"detail"`
	Magnitude     float64            `json:"magnitude"`
	ScenarioKind  string             `json:"scenario_kind"`
	ScenarioMag   int                `json:"scenario_magnitude"`
	Source        string             `json:"source"`
	URL           string             `json:"url"`
	ObservedAt    int64              `json:"observed_at"`
	Injected      bool               `json:"injected"`
	InjectedMonth int                `json:"injected_month"`
	Effects       []ScenarioEffect   `json:"effects"` // 即时参数变化(批次 68 结构复用)
	Outcome       *WorldEventOutcome `json:"outcome"` // 次月结果快照(待回填)
}

// worldEventLogAdd 追加一条事件记录(新→旧;环上限 worldEventLogCap)。
// 零 rand。
func (w *World) worldEventLogAdd(rec WorldEventRecord) {
	if w == nil {
		return
	}
	if rec.Effects == nil {
		rec.Effects = make([]ScenarioEffect, 0)
	}
	out := make([]WorldEventRecord, 0, len(w.WorldEventLog)+1)
	out = append(out, rec)
	out = append(out, w.WorldEventLog...)
	if len(out) > worldEventLogCap {
		out = out[:worldEventLogCap]
	}
	w.WorldEventLog = out
}

// worldEventSeen 幂等闸门:该指纹是否已处理过(nil 安全)。
func (w *World) worldEventSeen(key string) bool {
	if w == nil || w.WorldEventSeen == nil {
		return false
	}
	return w.WorldEventSeen[key]
}

// worldEventMarkSeen 标记指纹已处理(注入或被安全阀拦下 ⇒ 只留痕一次)。
func (w *World) worldEventMarkSeen(key string) {
	if w == nil || key == "" {
		return
	}
	if w.WorldEventSeen == nil {
		w.WorldEventSeen = make(map[string]bool)
	}
	w.WorldEventSeen[key] = true
}

// worldEventEffectiveMonth 事件**生效月**(= 触发注入的那次月结之后进入的
// 月份)。注入发生在 trySettle 的月结**之前**(此时 w.Month = 正在结算的月 M)
// ⇒ 生效月 = M+1。
//
// 于是:
//   - 记录的 InjectedMonth = 生效月 ⇒ 月结后(w.Month = M+1)该记录即属
//     「本月注入」;
//   - 安全阀计数器归属生效月 ⇒ 跨月自然归零 ⇒「每月 ≤N 条」「同 kind 每月
//     至多 1 次」按生效月独立生效;
//   - 回填 outcome 的时刻 = 注入后的下一次月结结束 ⇒ Outcome.Month = 生效月,
//     与 InjectedMonth 一致(因果链两端同月)。
//
// 零 rand。
func (w *World) worldEventEffectiveMonth() int {
	if w == nil {
		return 0
	}
	return w.Month + 1
}

// worldEventAutoCounterReset 跨月重置安全阀计数器(月度口径)。
//
// 零 rand。
func (w *World) worldEventAutoCounterReset() {
	if w == nil {
		return
	}
	month := w.worldEventEffectiveMonth()
	if w.WorldEventAutoMonth == month {
		return // 本生效月已计数,不重置
	}
	w.WorldEventAutoMonth = month
	w.WorldEventAutoCount = 0
	w.WorldEventAutoKinds = make(map[string]bool)
}

// worldEventAutoCount 本生效月已自动注入条数(安全阀用)。零 rand。
func (w *World) worldEventAutoCount() int {
	if w == nil {
		return 0
	}
	return w.WorldEventAutoCount
}

// worldEventAutoKindInjected 本生效月是否已注入过该 scenario_kind(安全阀用)。零 rand。
func (w *World) worldEventAutoKindInjected(kind string) bool {
	if w == nil || w.WorldEventAutoKinds == nil {
		return false
	}
	return w.WorldEventAutoKinds[kind]
}

// worldEventAutoMarkInjected 记入本生效月注入(条数 +1;kind 去重集合)。零 rand。
func (w *World) worldEventAutoMarkInjected(kind string) {
	if w == nil {
		return
	}
	w.WorldEventAutoCount++
	if kind == "" {
		return
	}
	if w.WorldEventAutoKinds == nil {
		w.WorldEventAutoKinds = make(map[string]bool)
	}
	w.WorldEventAutoKinds[kind] = true
}

// worldEventInjectedEffectiveThisMonth 看板「本月自动注入」口径:生效于当前月
// (w.Month)的自动注入条数(日志环回查 —— 计数器只保留当前生效月一份,
// 而看板可能在月结后、下一次触发前读取)。零 rand。
func (w *World) worldEventInjectedEffectiveThisMonth() int {
	if w == nil {
		return 0
	}
	month := w.Month
	if month <= 0 {
		return 0
	}
	if w.WorldEventAutoMonth == month {
		return w.WorldEventAutoCount
	}
	n := 0
	for i := range w.WorldEventLog {
		r := w.WorldEventLog[i]
		if r.Injected && r.InjectedMonth == month {
			n++
		}
	}
	return n
}

// fillWorldEventOutcomes 回填「注入后下一次月结」的因果链终点(契约 §3.4)。
//
// 调用点:SettleMonth 末尾(此时 w.Month 已 +1 ⇒ Outcome.Month = 次月编号,
// 语义与「次月城市结果快照」一致)。
//
// 零 rand;无待回填记录 ⇒ 立即返回(零开销、零偏移)。
func (w *World) fillWorldEventOutcomes() {
	if w == nil || len(w.WorldEventLog) == 0 {
		return
	}
	pending := false
	for i := range w.WorldEventLog {
		r := w.WorldEventLog[i]
		if r.Injected && r.Outcome == nil {
			pending = true
			break
		}
	}
	if !pending {
		return
	}
	outcome := w.worldEventOutcomeSnapshot()
	for i := range w.WorldEventLog {
		r := &w.WorldEventLog[i]
		if !r.Injected || r.Outcome != nil {
			continue
		}
		cp := outcome
		r.Outcome = &cp
	}
}

// worldEventOutcomeSnapshot 取当前城市状态快照(因果链终点指标)。
// 零 rand。
func (w *World) worldEventOutcomeSnapshot() WorldEventOutcome {
	out := WorldEventOutcome{Month: w.Month}
	if w.Goods != nil {
		out.CPIYoY = w.Goods.CPIYoY
		out.GoodsPriceIdx = worldEventGoodsPriceIdx(w)
	}
	if w.Labor != nil {
		out.Unemployment = w.Labor.Unemployment
	}
	if w.Market != nil {
		out.StockIndex = w.Market.StockIndex
		out.GoldPrice = w.Market.GoldPrice
	}
	total := int64(0)
	for _, p := range w.Players {
		if p == nil || !p.Alive {
			continue
		}
		total += p.Cash
	}
	out.ResidentCashTotal = float64(total)
	return out
}

// worldEventGoodsPriceIdx 商品价格指数(八大类按篮子权重加权;基期=100)。
// 零 rand。
func worldEventGoodsPriceIdx(w *World) float64 {
	if w == nil || w.Goods == nil {
		return 0
	}
	sum, wsum := 0.0, 0.0
	for _, id := range goodsOrder {
		it := w.Goods.Items[id]
		if it == nil {
			continue
		}
		sum += it.Weight * it.PriceIdx
		wsum += it.Weight
	}
	if wsum <= 0 {
		return 0
	}
	return sum / wsum
}

// worldEventBriefLocked 居民 prompt「■ 世界事件」小节正文(批次 80)。
// 调用方持房间锁;无已注入事件 ⇒ 返回空串 ⇒ prompt 整段省略
// (与批次 66 exoBriefLocked / 批次 27 天气零回归范式一致)。
// 最多列 worldEventBriefMax 条已注入事件(新→旧)。
func worldEventBriefLocked(w *World) string {
	if w == nil || len(w.WorldEventLog) == 0 {
		return ""
	}
	parts := make([]string, 0, worldEventBriefMax)
	n := 0
	for i := range w.WorldEventLog {
		r := w.WorldEventLog[i]
		if !r.Injected {
			continue
		}
		parts = append(parts, worldEventBriefLine(r))
		n++
		if n >= worldEventBriefMax {
			break
		}
	}
	if len(parts) == 0 {
		return ""
	}
	out := "真实世界事件已注入本城(由系统按阈值自动触发,居民可据此调整决策):"
	for _, p := range parts {
		out += "\n- " + p
	}
	return out
}

// worldEventBriefLine 单条事件的简报文本。
func worldEventBriefLine(r WorldEventRecord) string {
	line := r.Title
	if r.ScenarioKind != "" {
		line += "(" + r.ScenarioKind + " L" + itoaInt(r.ScenarioMag) + ")"
	}
	if r.Detail != "" {
		line += ":" + r.Detail
	}
	if len(r.Effects) > 0 {
		line += " — " + scenarioEventEffectSummary(r.Effects)
	}
	return line
}
