// Package virtual_city — mc_resident_test.go: 批次73 O1 居民级采集测试
// (方案 §10:计数器逐字段;整局采集 12 行;residents.jsonl 写盘 + 轮转)。
//
// 硬纪律:零真实 LLM / 零网络 / 零 DB;写盘目录由 TestMain 重定向到 tmp。
package virtual_city

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// mcResidentTradeWorld 构造 2 座交易场景世界:座 0 持黄金(成本 ¥5,000),
// 座 1 现金 ¥100,000。
func mcResidentTradeWorld() *World {
	w := calibTestWorld(7, 2)
	w.Players[0].Assets = append(w.Players[0].Assets,
		Asset{Kind: AssetGold, Units: 100, CostCNY: 5000, OpenMonth: 1})
	w.Players[1].Cash = 100000
	return w
}

// TestMCResidentCounters_Trade P2P 成交:TradesMade 双方 +1、已实现价差
// 卖方按成本口径、trade 事件 1 条。
func TestMCResidentCounters_Trade(t *testing.T) {
	w := mcResidentTradeWorld()
	l, e := w.CreateListing(0, ListingAsset, ListingPayload{
		Asset: &AssetPayload{Asset: w.Players[0].Assets[0], MinCNY: 5000},
	}, 6000)
	if e != nil {
		t.Fatalf("CreateListing: %v", e)
	}
	neg, e := w.NegotiateStart(1, l.ID, 6000)
	if e != nil {
		t.Fatalf("NegotiateStart: %v", e)
	}
	if _, e := w.NegotiateRespond(0, neg.ID, NegotiateActAccept, 0, ""); e != nil {
		t.Fatalf("NegotiateRespond accept: %v", e)
	}
	if got := w.Players[0].TradesMade; got != 1 {
		t.Errorf("卖方 TradesMade = %d,期望 1", got)
	}
	if got := w.Players[1].TradesMade; got != 1 {
		t.Errorf("买方 TradesMade = %d,期望 1", got)
	}
	if got := w.Players[0].TradeRealizedCNY; got != 1000 { // 6000 − 5000
		t.Errorf("卖方 TradeRealizedCNY = %d,期望 1000", got)
	}
	if got := w.Players[1].TradeRealizedCNY; got != 0 {
		t.Errorf("买方 TradeRealizedCNY = %d,期望 0(成本重置)", got)
	}
	// trade 事件恰好 1 条。
	n := 0
	for _, ev := range w.MCEvents {
		if ev.EventType == MCEventTrade {
			n++
			if ev.Price != 6000 || len(ev.Actors) != 2 {
				t.Errorf("trade 事件 price/actors 不合规:%+v", ev)
			}
		}
	}
	if n != 1 {
		t.Errorf("trade 事件数 = %d,期望 1", n)
	}
}

// TestMCResidentCounters_Auction 拍卖有效出价:AuctionsJoined +1、
// MCAuctionCount +1;成交后双方 TradesMade +1。
func TestMCResidentCounters_Auction(t *testing.T) {
	w := mcResidentTradeWorld()
	a, e := w.StartAuction(0, AuctionEnglish, w.Players[0].Assets[0], 5000)
	if e != nil {
		t.Fatalf("StartAuction: %v", e)
	}
	if w.MCAuctionCount != 1 {
		t.Errorf("MCAuctionCount = %d,期望 1", w.MCAuctionCount)
	}
	if e := w.PlaceBid(1, a.ID, 6000); e != nil {
		t.Fatalf("PlaceBid: %v", e)
	}
	if got := w.Players[1].AuctionsJoined; got != 1 {
		t.Errorf("AuctionsJoined = %d,期望 1", got)
	}
	// 低价无效出价不计。
	if e := w.PlaceBid(1, a.ID, 1); e == nil {
		t.Error("低于起拍出价应被拒绝")
	}
	if got := w.Players[1].AuctionsJoined; got != 1 {
		t.Errorf("无效出价后 AuctionsJoined = %d,期望仍为 1", got)
	}
	if _, e := w.EndAuction(a.ID); e != nil {
		t.Fatalf("EndAuction: %v", e)
	}
	if w.Players[0].TradesMade != 1 || w.Players[1].TradesMade != 1 {
		t.Errorf("拍卖成交后 TradesMade = %d/%d,期望 1/1",
			w.Players[0].TradesMade, w.Players[1].TradesMade)
	}
	n := 0
	for _, ev := range w.MCEvents {
		if ev.EventType == MCEventAuction {
			n++
		}
	}
	if n != 1 {
		t.Errorf("auction 事件数 = %d,期望 1", n)
	}
}

// TestMCResidentCounters_Bias 偏差实际生效:BiasHits +1(m≠1 分支);
// 免疫(乘子 1)不计。
func TestMCResidentCounters_Bias(t *testing.T) {
	w := calibTestWorld(7, 2)
	p := w.Players[0]
	p.BiasProfile = []string{"overconfidence"}
	p.Cognition = 0 // 未通过 ⇒ 全额生效(Exaggerated 1.30)
	mul, _, _ := p.biasEffectMulOf(EffectBuyAmount, nil)
	if mul <= 1 {
		t.Fatalf("认知 0 时过度自信乘子应 > 1,got %v", mul)
	}
	if got := p.BiasHits; got != 1 {
		t.Errorf("BiasHits = %d,期望 1", got)
	}
	// 回合免疫 ⇒ 乘子 1 ⇒ 不计数。
	p.BiasImmuneThisTurn = true
	if _, _, _ = p.biasEffectMulOf(EffectBuyAmount, nil); p.BiasHits != 1 {
		t.Errorf("免疫回合不应计数,BiasHits = %d", p.BiasHits)
	}
	p.BiasImmuneThisTurn = false
	// 免疫进度达标(≥0.80)⇒ 不生效不计数。
	p.BiasImmunity = map[string]float64{"overconfidence": 0.9}
	if _, _, _ = p.biasEffectMulOf(EffectBuyAmount, nil); p.BiasHits != 1 {
		t.Errorf("免疫进度达标不应计数,BiasHits = %d", p.BiasHits)
	}
}

// TestMCResidentCounters_Loan 放贷发放:loan 事件 +1;月结采样
// LoanMaxCNY / CreditMin。
func TestMCResidentCounters_Loan(t *testing.T) {
	w := calibTestWorld(7, 2)
	p := w.Players[0]
	p.CreditScore = 700
	if _, e := w.ApplyAction(0, Action{Type: ActTakeLoan, Kind: "credit", AmountCNY: 50000}); e != nil {
		t.Fatalf("take_loan: %v", e)
	}
	n := 0
	for _, ev := range w.MCEvents {
		if ev.EventType == MCEventLoan {
			n++
			if ev.Price < 50000 {
				t.Errorf("loan 事件 price = %d,期望 ≥ 50000", ev.Price)
			}
		}
	}
	if n != 1 {
		t.Errorf("loan 事件数 = %d,期望 1", n)
	}
	if len(p.Loans) != 1 {
		t.Fatalf("贷款笔数 = %d,期望 1", len(p.Loans))
	}
	// 月结采样:LoanMaxCNY = 余额峰值;CreditMin ≤ 当前信用分。
	calibSettleMonths(w, 3)
	if p.LoanMaxCNY < p.Loans[0].Balance {
		t.Errorf("LoanMaxCNY = %d,应 ≥ 当前余额 %d", p.LoanMaxCNY, p.Loans[0].Balance)
	}
	if p.CreditMin == 0 {
		t.Error("CreditMin 未采样(月结后应非 0 或已记录最低值)")
	}
}

// TestMCResidentCollect_WholeRun 整局采集:12 行、字段合规、
// auction_participation 无拍卖 ⇒ no_sample。
func TestMCResidentCollect_WholeRun(t *testing.T) {
	w := mcWorld(MCSeedFor(MCStrategyBalanced, 0))
	recs := mcCollectResidents(w, "CR1", w.MCAuctionCount)
	if len(recs) != MaxSeats {
		t.Fatalf("居民记录数 = %d,期望 %d", len(recs), MaxSeats)
	}
	for _, r := range recs {
		if r.ResidentID == "" || r.Portrait == "" {
			t.Errorf("resident_id/portrait 为空:%+v", r)
		}
		if r.FiveResourcesEnd[0] != int(w.Players[r.Seat].Cash) {
			t.Errorf("座 %d 金钱资源 = %d,期望 %d", r.Seat, r.FiveResourcesEnd[0], w.Players[r.Seat].Cash)
		}
		if r.InvestedCategories == nil {
			t.Errorf("座 %d InvestedCategories 为 nil(必须恒非 null)", r.Seat)
		}
		if r.CategoryReturns == nil {
			t.Errorf("座 %d CategoryReturns 为 nil(必须恒非 null map)", r.Seat)
		}
		if r.AuctionNoSample != (w.MCAuctionCount == 0) {
			t.Errorf("座 %d no_sample 位与总场次口径不一致", r.Seat)
		}
		if r.FIIndexEnd < 0 || r.FIIndexEnd > 2.0 {
			t.Errorf("座 %d FI = %f,越界 [0,2]", r.Seat, r.FIIndexEnd)
		}
	}
	// 聚合出口:ProfessionOutcomes 总数 = 12;中位数 ∈ [0,1]。
	profession, median := mcAggregateResidents(recs, outcomeStats(w))
	total := 0
	for _, gt := range profession {
		total += gt[1]
	}
	if total != MaxSeats {
		t.Errorf("ProfessionOutcomes 总样本 = %d,期望 %d", total, MaxSeats)
	}
	if median < 0 || median > 1 {
		t.Errorf("AuctionParticipationMedian = %f,越界 [0,1]", median)
	}
}

// TestMCResidentStore_WriteAndRotate residents.jsonl 写盘 + 轮转。
func TestMCResidentStore_WriteAndRotate(t *testing.T) {
	dir := t.TempDir()
	store := &ResidentStore{dir: dir}
	recs := func(n int) []MCResidentRecord {
		out := make([]MCResidentRecord, n)
		for i := range out {
			out[i] = MCResidentRecord{ResidentID: "CR1-S0", Portrait: "P01",
				InvestedCategories: []string{}, CategoryReturns: map[string]float64{}}
		}
		return out
	}
	// 灌到恰首次越过轮转阈值(1667 批 × 12 = 20004 > 20000)⇒ 保尾 10000,
	// 不再追加(防二次轮转后的线性增长混淆断言)。
	batches := maxResidentLines/12 + 1
	for i := 0; i < batches; i++ {
		if err := store.AppendResidents(recs(12)); err != nil {
			t.Fatalf("append batch #%d: %v", i, err)
		}
	}
	path := filepath.Join(dir, calibrationResidentsFile)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	lines := bytes.Split(bytes.TrimRight(data, "\n"), []byte{'\n'})
	if len(lines) != keepResidentLines {
		t.Fatalf("轮转后行数 = %d,期望 %d", len(lines), keepResidentLines)
	}
	var rec MCResidentRecord
	if err := json.Unmarshal(lines[0], &rec); err != nil {
		t.Fatalf("首行解析: %v", err)
	}
	// 逐行可解析。
	for i, line := range lines {
		var r MCResidentRecord
		if err := json.Unmarshal(line, &r); err != nil {
			t.Fatalf("第 %d 行解析: %v", i, err)
		}
	}
}
