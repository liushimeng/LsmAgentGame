// Package virtual_city — mc_resident.go: 居民级采集(批次73 O1,
// 框架 §4.2 十二字段)+ residents.jsonl 落盘。
//
// 硬纪律(方案 §3):
//   - **零 rand**:采集是纯读快照,绝不调用 w.Rand;
//   - **零账目语义改动**:不改 ApplyAction/SettleMonth/Ledger 任何语义;
//   - **零 panic**:落盘失败 zap warn(calibration_runs.go 同款),采集失败
//     静默降级为空聚合。
//
// ⚠ 口径标注(方案 §4.1 权威口径):
//   - five_resources_end[4](时间)= **当月已用动作数**(monthlyActionBudget −
//     ActionBudget;引擎无时间存量字段,以动作消耗代理);
//   - passive_income_end / expense_end = 最近一次月结派生值(p.Monthly);
//   - auction_participation_rate = auctions_joined ÷ 本局总拍卖场次
//     (StartAuction 计数 MCAuctionCount;单局无拍卖 ⇒ 0 + no_sample 位);
//   - category_returns = 期末持仓市值 − 累计买入成本 ÷ 成本(p.Assets.CostCNY;
//     savings 无成本字段 ⇒ 无键)。
package virtual_city

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// ── 居民级记录(§4.2 十二字段,json tags 与设计逐字对齐)─────────────────

// MCResidentRecord 单座位单局采集记录(residents.jsonl 每居民 1 行)。
type MCResidentRecord struct {
	// Seat 座位号(聚合/分类器内部用;json:"-" 不落盘 —— resident_id 已含)。
	Seat int `json:"-"`
	ResidentID  string `json:"resident_id"` // "CR{n}-S{seat}"
	Portrait    string `json:"portrait"`    // 职业卡 id
	FiveResourcesEnd [5]int `json:"five_resources_end"` // 金钱/精力/人脉/认知/时间(已用动作数)
	NetWorthEnd      int64  `json:"net_worth_end"`      // recomputeNetWorth 口径
	FIIndexEnd       float64 `json:"fi_index_end"`      // 既有 FI 公式(AnchorFinal.FI 同口径)
	PassiveIncomeEnd int64  `json:"passive_income_end"` // 最近月结被动收入
	ExpenseEnd       int64  `json:"expense_end"`        // 最近月结总支出
	TradesMade       int    `json:"trades_made"`        // 成交计数器
	AuctionsJoined   int    `json:"auctions_joined"`    // 拍卖有效出价计数器
	// AuctionParticipationRate = auctions_joined ÷ 本局总拍卖场次。
	AuctionParticipationRate float64 `json:"auction_participation_rate"`
	// AuctionNoSample 本局无拍卖 ⇒ 参与率 0 且记 no_sample(单局口径)。
	AuctionNoSample bool `json:"auction_no_sample,omitempty"`
	BiasEventsHit   int  `json:"bias_events_hit"` // BiasHits 同口径
	// InvestedCategories 期末持仓品类去重集合(8 类口径,升序;恒非 null)。
	InvestedCategories []string `json:"invested_categories"`
	// CategoryReturns 期末品类收益率(市值−成本 ÷ 成本;无持仓品类无键)。
	CategoryReturns map[string]float64 `json:"category_returns"`
	LoanMax         int64              `json:"loan_max"`  // 终身贷款余额峰值
	CreditMin       int                `json:"credit_min"` // 终身信用分最低值
}

// mcCollectResidents 纯函数:整局 12 座居民级采集(终局/提前终局皆可用;
// nil World ⇒ 空切片)。totalAuctions = 本局 StartAuction 总场次。
func mcCollectResidents(w *World, runID string, totalAuctions int) []MCResidentRecord {
	out := make([]MCResidentRecord, 0, MaxSeats)
	if w == nil {
		return out
	}
	for seat := 0; seat < MaxSeats; seat++ {
		p := w.Players[seat]
		if p == nil {
			continue
		}
		rec := MCResidentRecord{
			Seat:             seat,
			ResidentID:       fmt.Sprintf("%s-S%d", runID, seat),
			Portrait:         p.Card.ID,
			NetWorthEnd:      recomputeNetWorth(p, w.Market),
			FIIndexEnd:       p.FIIndex(w.Market, w.Age()),
			PassiveIncomeEnd: p.Monthly.PassiveIncome,
			ExpenseEnd:       p.Monthly.Expense,
			TradesMade:       p.TradesMade,
			AuctionsJoined:   p.AuctionsJoined,
			BiasEventsHit:    p.BiasHits,
			LoanMax:          p.LoanMaxCNY,
			CreditMin:        p.CreditMin,
		}
		// 五维资源:金钱/精力/人脉/认知/时间(已用动作数代理口径)。
		used := monthlyActionBudget - p.ActionBudget
		if used < 0 {
			used = 0
		}
		rec.FiveResourcesEnd = [5]int{int(p.Cash), p.Energy, p.Network, p.Cognition, used}
		// 拍卖参与率:单局无拍卖 ⇒ 0 + no_sample。
		if totalAuctions > 0 {
			rec.AuctionParticipationRate = float64(p.AuctionsJoined) / float64(totalAuctions)
		} else {
			rec.AuctionNoSample = true
		}
		rec.InvestedCategories, rec.CategoryReturns = mcResidentCategories(p, w.Market)
		out = append(out, rec)
	}
	return out
}

// mcResidentCategories 期末持仓品类去重集合(8 类口径:stock/bond/gold/
// house/shop/side_business/pension/savings;升序恒非 null)+ 品类收益率
// (市值 − 累计成本 ÷ 成本;成本 ≤0 或无持仓品类无键;savings 无成本字段)。
func mcResidentCategories(p *Player, m *MarketState) ([]string, map[string]float64) {
	cats := map[string]bool{}
	cost := map[string]int64{}
	value := map[string]int64{}
	add := func(cat string, v int64, c int64) {
		cats[cat] = true
		value[cat] += v
		cost[cat] += c
	}
	for i := range p.Assets {
		a := &p.Assets[i]
		switch {
		case a.Kind == AssetStockIndex:
			add("stock", AssetValue(a, m), a.CostCNY)
		case a.Kind == AssetBond:
			add("bond", AssetValue(a, m), a.CostCNY)
		case a.Kind == AssetGold:
			add("gold", AssetValue(a, m), a.CostCNY)
		case isHouseKind(a.Kind):
			add("house", AssetValue(a, m), a.CostCNY)
		case isShopKind(a.Kind):
			add("shop", AssetValue(a, m), a.CostCNY)
		case a.Kind == AssetSideBusiness:
			add("side_business", 0, a.CostCNY) // 残值恒 0
		case a.Kind == AssetPension:
			add("pension", 0, a.CostCNY)
		}
	}
	if p.SavingsDeposit > 0 {
		cats["savings"] = true // 无成本字段 ⇒ 只进集合,不进收益率
	}
	out := make([]string, 0, len(cats))
	for c := range cats {
		out = append(out, c)
	}
	sort.Strings(out)
	returns := map[string]float64{}
	for cat, v := range value {
		if c := cost[cat]; c > 0 {
			returns[cat] = float64(v-c) / float64(c)
		}
	}
	return out, returns
}

// mcAggregateResidents 聚合出口(O4 R-B2/R-B3 输入):
//
//   - ProfessionOutcomes:portrait → [good,total](good = 结局 winner/affluent,
//     与 OutcomeStat.Good 同口径,经 outcomes 按座位对齐);
//   - AuctionParticipationMedian:12 座参与率中位数(mcMedian 口径)。
func mcAggregateResidents(residents []MCResidentRecord, outcomes []OutcomeStat) (map[string][2]int, float64) {
	goodBySeat := make(map[int]bool, len(outcomes))
	for _, o := range outcomes {
		goodBySeat[o.Seat] = o.Good
	}
	profession := map[string][2]int{}
	rates := make([]float64, 0, len(residents))
	for _, r := range residents {
		gt := profession[r.Portrait]
		gt[1]++
		if goodBySeat[r.Seat] {
			gt[0]++
		}
		profession[r.Portrait] = gt
		rates = append(rates, r.AuctionParticipationRate)
	}
	return profession, mcMedian(rates)
}

// ── residents.jsonl 落盘(复用 RunStore 模式;方案 §4.3)────────────────

const (
	// calibrationResidentsFile 居民级采集文件名(每居民 1 行)。
	calibrationResidentsFile = "residents.jsonl"
	// maxResidentLines 追加后总行数超过此值 ⇒ 触发轮转。
	maxResidentLines = 20000
	// keepResidentLines 轮转保留的尾部行数(1000 局 gate ⇒ 12000 行,
	// 须大于单批 ⇒ keep 10000 < 12000? 见下方轮换论证:每局 12 行,
	// 20000 行触发 ⇒ 保尾 10000 ⇒ 可容 833 局;gate 单策略 200 局 ×12 =
	// 2400 行/批,安全)。
	keepResidentLines = 10000
)

// ResidentStore 居民记录写盘器(目录 + 互斥锁;零值可用,dir 空串回落
// defaultCalibrationRunsDir 同目录)。形态与 RunStore 完全同款。
type ResidentStore struct {
	mu  sync.Mutex
	dir string
}

// defaultResidentStore 包级单例(MCRunOneWith 出口接线消费)。
var defaultResidentStore = &ResidentStore{}

// AppendResidents 经包级单例追加一批居民记录(每居民 1 行;失败返回
// error,绝不 panic)。
func AppendResidents(recs []MCResidentRecord) error {
	return defaultResidentStore.AppendResidents(recs)
}

// AppendResidents 追加居民记录;超过 maxResidentLines 时保尾
// keepResidentLines 原子重写。全程持锁(并发安全)。
func (s *ResidentStore) AppendResidents(recs []MCResidentRecord) error {
	if len(recs) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := s.dir
	if dir == "" {
		dir = defaultCalibrationRunsDir
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("virtual_city calibration residents mkdir %s: %w", dir, err)
	}
	var buf bytes.Buffer
	for _, rec := range recs {
		line, err := json.Marshal(rec)
		if err != nil {
			return fmt.Errorf("virtual_city calibration residents marshal %s: %w", rec.ResidentID, err)
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}

	path := filepath.Join(dir, calibrationResidentsFile)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("virtual_city calibration residents open %s: %w", path, err)
	}
	if _, err := f.Write(buf.Bytes()); err != nil {
		_ = f.Close()
		return fmt.Errorf("virtual_city calibration residents write %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("virtual_city calibration residents close %s: %w", path, err)
	}
	return s.rotateIfFull(path)
}

// rotateIfFull 行数超限时保尾重写(临时文件 + os.Rename 原子替换)。
// 已持 s.mu(仅 AppendResidents 调用)。
func (s *ResidentStore) rotateIfFull(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("virtual_city calibration residents read %s: %w", path, err)
	}
	lines := bytes.Split(data, []byte{'\n'})
	if len(lines) > 0 && len(lines[len(lines)-1]) == 0 {
		lines = lines[:len(lines)-1]
	}
	if len(lines) <= maxResidentLines {
		return nil
	}
	keep := lines[len(lines)-keepResidentLines:]
	var buf bytes.Buffer
	for _, l := range keep {
		buf.Write(l)
		buf.WriteByte('\n')
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("virtual_city calibration residents tmp write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("virtual_city calibration residents rename %s: %w", path, err)
	}
	return nil
}

// appendResidentsLogging 居民记录写盘收口(失败 zap warn,绝不 panic、
// 不阻断 MC 收口)。由 MCRunOneWith 出口调用(§4.3)。
func appendResidentsLogging(recs []MCResidentRecord) {
	if err := AppendResidents(recs); err != nil {
		logger.L().Warn("virtual_city calibration residents append failed",
			zap.Int("count", len(recs)), zap.Error(err))
	}
}
