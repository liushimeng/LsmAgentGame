// Package virtual_city — view_calibration.go: game.state.calibration 视图
// (批次71 §9.1 契约;批次72 §6.3 红线扩为 7 条 + runs 历史;
// 批次73 O0 自 view.go 纯搬移 —— 只搬移不改逻辑/签名/导出)。
//
// ⚠ 形状纪律(契约 §9.2):
//   - invariants[] **恒 7 条**(I1/I2/I3/I4/I5/I6/I8),顺序固定;
//   - redlines[] **恒 10 条**(批次73:R-E3/R-E4/R-E5/R-E6/R-B1/R-E1/R-E2
//     /R-B2/R-B3/R-B4),顺序固定;
//   - indicators 无值键为 **null**(不得省略键);
//   - history[] / runs[] **恒为数组**(空 = [] 而非 null)。
package virtual_city

// CalibrationJSON 是 game.state.calibration(批次71 §9.1)。
type CalibrationJSON struct {
	Month      int                      `json:"month"`
	StateHash  string                   `json:"state_hash"`
	Invariants []InvariantCheck         `json:"invariants"`
	Indicators CalibrationIndicators    `json:"indicators"`
	Redlines   []RedlineHit             `json:"redlines"`
	History    []CalibrationHistoryJSON `json:"history"`
	// CategoryAnnualized 当期局品类年化(批次72 §6.2;月数<12 或无数据 → null)。
	CategoryAnnualized map[string]float64 `json:"category_annualized"`
	// Runs 最近 12 局完成运行(新→旧;批次72 §8.1;恒为数组,空 = [])。
	Runs []CalibrationRunJSON `json:"runs"`
	// ── 批次73 §9.1(全部可选,omitempty 向后兼容)──
	// OutcomeCauses 结局成因 8 类 + undetermined 直方图(本局;O3)。
	OutcomeCauses map[string]int `json:"outcome_causes,omitempty"`
	// EventCounts 事件级采集六类 + 未识别桶聚合(本局;O2)。
	EventCounts map[string]int `json:"event_counts,omitempty"`
	// AuctionParticipationMedian 本局 12 座拍卖参与率中位数(O4 R-B3)。
	AuctionParticipationMedian float64 `json:"auction_participation_median,omitempty"`
	// ProfessionOutcomes 各职业卡良好结局占比分子分母([good,total];O4 R-B2)。
	ProfessionOutcomes map[string][2]int `json:"profession_outcomes,omitempty"`
}

// CalibrationRunJSON 是 calibration.runs 单项(批次72 §8.1 字段逐字)。
type CalibrationRunJSON struct {
	RunID             string         `json:"run_id"`
	Seed              int64          `json:"seed"`
	Strategy          string         `json:"strategy"`
	TotalSteps        int            `json:"total_steps"`
	MoneyExpansion    float64        `json:"money_expansion"`
	BankruptcyRate    float64        `json:"bankruptcy_rate"`
	OutcomeCounts     map[string]int `json:"outcome_counts"`
	InvariantFails    []string       `json:"invariant_fails"`
	RedlinesTriggered []string       `json:"redlines_triggered"`
	StateHash         string         `json:"state_hash"`
	FinishedAt        int64          `json:"finished_at"`
}

// CalibrationHistoryJSON 是 calibration.history 单项(最近 12 月)。
type CalibrationHistoryJSON struct {
	Month int    `json:"month"`
	Hash  string `json:"hash"`
	OK    bool   `json:"ok"`
}

// calibrationJSON 组装 game.state.calibration(批次71 §9.1)。
//
// 数据源 = World.Audits(逐月审计快照;calibration.go::auditMonth 写入)。
// ⚠ **不重算**(每次 game.state 推送都重算 I8 全扫会拖慢帧率)——
// 取最近一条审计快照。
//
// ⚠ **顶层 month = 该审计快照所属月(= 最近一次结算的月),不是当前
// `game.state.month`** —— 两者相差一拍是**刻意**的:审计只在月结末尾
// 产生(w.Month++ 之前),故 state_hash / invariants / indicators 与顶层
// month **同源**。前端面板若同时显示「当前月」与「审计月」,用审计月即可,
// 不要混用。
//
// 形状纪律:invariants 恒 7 条 / redlines 恒 10 条(批次73)/ history 与
// runs 恒为数组。(正常路径下 auditMonth 必写满 7 + 10 条,占位项仅作防御。)
func calibrationJSON(w *World) *CalibrationJSON {
	if w == nil || len(w.Audits) == 0 {
		return nil
	}
	latest := w.Audits[len(w.Audits)-1]
	out := &CalibrationJSON{
		Month:      latest.Month,
		StateHash:  latest.StateHash,
		Invariants: make([]InvariantCheck, 0, 7),
		Indicators: latest.Indicators,
		Redlines:   make([]RedlineHit, 0, 10),
		History:    make([]CalibrationHistoryJSON, 0, calibrationHistoryLen),
	}
	out.Invariants = append(out.Invariants, latest.Invariants...)
	for len(out.Invariants) < 7 { // 防御:审计快照缺条时补占位(恒 7 条)
		out.Invariants = append(out.Invariants, InvariantCheck{
			ID: "", Name: "未采集", Passed: false, Severity: InvariantSeverityFatal,
			Detail: "审计快照缺失该不变量",
		})
	}
	out.Invariants = out.Invariants[:7]
	out.Redlines = append(out.Redlines, latest.Redlines...)
	for len(out.Redlines) < 10 { // 防御:恒 10 条(批次73 O4:R-B2/R-B3/R-B4)
		out.Redlines = append(out.Redlines, RedlineHit{ID: "", Name: "未采集", Detail: "审计快照缺失该红线"})
	}
	out.Redlines = out.Redlines[:10]

	// 历史:最近 12 月(旧 → 新)。
	start := len(w.Audits) - calibrationHistoryLen
	if start < 0 {
		start = 0
	}
	for _, a := range w.Audits[start:] {
		ok := true
		for _, inv := range a.Invariants {
			if !inv.Passed {
				ok = false
				break
			}
		}
		out.History = append(out.History, CalibrationHistoryJSON{
			Month: a.Month, Hash: a.StateHash, OK: ok,
		})
	}

	// 批次72 §5.4/§8.1:当期局品类年化 + 最近 12 局完成运行(新→旧);
	// 批次73 §9.1:结局成因 / 事件计数 / 拍卖参与率中位数 / 职业结局。
	if w.Calibration != nil {
		if w.Calibration.Current != nil {
			out.CategoryAnnualized = w.Calibration.Current.CategoryAnnualized
		} else if n := len(w.Calibration.Runs); n > 0 {
			out.CategoryAnnualized = w.Calibration.Runs[n-1].CategoryAnnualized
		}
		src := w.Calibration.Current
		if src == nil && len(w.Calibration.Runs) > 0 {
			src = &w.Calibration.Runs[len(w.Calibration.Runs)-1]
		}
		if src != nil {
			out.OutcomeCauses = src.OutcomeCauses
			out.EventCounts = src.EventCounts
			out.AuctionParticipationMedian = src.AuctionParticipationMedian
			out.ProfessionOutcomes = src.ProfessionOutcomes
		}
		n := len(w.Calibration.Runs)
		if n > calibrationHistoryLen {
			n = calibrationHistoryLen
		}
		out.Runs = make([]CalibrationRunJSON, 0, n)
		for i := len(w.Calibration.Runs) - 1; i >= len(w.Calibration.Runs)-n; i-- {
			r := &w.Calibration.Runs[i]
			out.Runs = append(out.Runs, CalibrationRunJSON{
				RunID:             r.RunID,
				Seed:              r.Seed,
				Strategy:          r.Strategy,
				TotalSteps:        r.TotalSteps,
				MoneyExpansion:    r.MoneyExpansion,
				BankruptcyRate:    r.BankruptcyRate,
				OutcomeCounts:     r.OutcomeCounts,
				InvariantFails:    nonNilStrings(r.InvariantFails),
				RedlinesTriggered: nonNilStrings(r.RedlinesTriggered),
				StateHash:         r.StateHash,
				FinishedAt:        r.FinishedAt,
			})
		}
	}
	if out.Runs == nil {
		out.Runs = []CalibrationRunJSON{} // 恒为数组(空 ≠ null)
	}
	return out
}

// nonNilStrings nil 切片 → 空数组(契约「恒为数组」口径)。
func nonNilStrings(xs []string) []string {
	if xs == nil {
		return []string{}
	}
	return xs
}
