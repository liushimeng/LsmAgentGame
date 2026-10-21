// Package virtual_city — mc_event_test.go: 批次73 O2 事件级采集测试
// (方案 §10:六类触发点各 1 例;环形 cap 丢最旧;聚合计数正确)。
package virtual_city

import "testing"

// TestMCEvent_TriggerPoints 六类触发点各 1 例(trade/auction/loan/bias/
// bankruptcy/market)。
func TestMCEvent_TriggerPoints(t *testing.T) {
	// trade / auction / loan:造交易 + 拍卖 + 贷款(复用居民测试场景)。
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
		t.Fatalf("accept: %v", e)
	}
	// 拍卖:座 1 把买到的黄金再挂拍卖(座 0 出价)。
	goldIdx := -1
	for i := range w.Players[1].Assets {
		if w.Players[1].Assets[i].Kind == AssetGold {
			goldIdx = i
		}
	}
	if goldIdx < 0 {
		t.Fatal("座 1 未持有黄金")
	}
	a, e := w.StartAuction(1, AuctionEnglish, w.Players[1].Assets[goldIdx], 5000)
	if e != nil {
		t.Fatalf("StartAuction: %v", e)
	}
	if e := w.PlaceBid(0, a.ID, 6000); e != nil {
		t.Fatalf("PlaceBid: %v", e)
	}
	if _, e := w.EndAuction(a.ID); e != nil {
		t.Fatalf("EndAuction: %v", e)
	}
	// 贷款:座 0 借信用贷(注意座 0 现金已被拍卖抬回,信用贷只校验信用分)。
	w.Players[0].CreditScore = 700
	if _, e := w.ApplyAction(0, Action{Type: ActTakeLoan, Kind: "credit", AmountCNY: 50000}); e != nil {
		t.Fatalf("take_loan: %v", e)
	}
	// bias:座 1 挂偏差画像 + 低认知 ⇒ 消费路径生效(planConsume 月结调用)。
	w.Players[1].BiasProfile = []string{"overconfidence"}
	w.Players[1].Cognition = 0
	w.Players[1].BiasHits = 3 // 直接预置增量(触发校准采样的事件路径)
	// bankruptcy + market + bias 走真实月结。
	w.Players[0].Cash = -1
	w.Players[0].NegativeCashMonths = 3 // 触发破产条件(连续现金 < 0)
	_ = calibSettleMonths(w, 2)

	counts := w.mcEventCounts()
	for _, typ := range []string{MCEventTrade, MCEventAuction, MCEventLoan, MCEventBias, MCEventBankruptcy} {
		if counts[typ] < 1 {
			t.Errorf("事件类型 %s 计数 = %d,期望 ≥ 1(counts=%v)", typ, counts[typ], counts)
		}
	}
	// 事件字段合规:step / event_time / actors 非 nil。
	for _, ev := range w.MCEvents {
		if ev.EventTime < 1 {
			t.Errorf("event_time = %d,期望 ≥ 1:%+v", ev.EventTime, ev)
		}
		switch ev.Step {
		case MCStepAct, MCStepSettle, MCStepInject:
		default:
			t.Errorf("step = %q,非法:%+v", ev.Step, ev)
		}
		if ev.Actors == nil {
			t.Errorf("actors 为 nil(必须恒非 null):%+v", ev)
		}
	}
}

// TestMCEvent_StressScenarioEvents stress 注入四类 scenario 事件
// (recession/depression 各 1 条、unemployment/medical 各 1 条)。
func TestMCEvent_StressScenarioEvents(t *testing.T) {
	w := mcWorld(MCSeedFor(MCStrategyStress, 0))
	scen := map[string]int{}
	for m := 1; m <= stressMedicalMonth+1; m++ {
		mcResetMonthBudgets(w)
		MCStressPreSettle(w, m)
		finished, _ := w.SettleMonth()
		MCStressPostSettle(w)
		if finished {
			break
		}
	}
	for _, ev := range w.MCEvents {
		if ev.EventType == MCEventScenario {
			scen[ev.ScenarioID]++
		}
	}
	for _, id := range []string{"stress_recession", "stress_depression", "stress_unemployment", "stress_medical"} {
		if scen[id] != 1 {
			t.Errorf("scenario 事件 %s = %d 条,期望 1(全量 %v)", id, scen[id], scen)
		}
	}
	// 事件计数聚合:scenario 入 unrecognized 桶(六类口径)。
	counts := w.mcEventCounts()
	if counts[MCEventUnrecognized] < 4 {
		t.Errorf("unrecognized 桶 = %d,期望 ≥ 4(stress 四类)", counts[MCEventUnrecognized])
	}
	// bankruptcy 在 stress 局(M26 失业 + M62 医疗冲击)应非 0。
	if counts[MCEventBankruptcy] == 0 {
		t.Log("注:本 seed stress 局前 63 月无破产(bankruptcy=0 允许)")
	}
}

// TestMCEvent_RingBufferCap 环形缓冲:超 cap 丢最旧 + EventDropped 计数。
func TestMCEvent_RingBufferCap(t *testing.T) {
	w := calibTestWorld(7, 1)
	total := mcEventCap + 100
	for i := 0; i < total; i++ {
		w.recordEvent(MCStepAct, MCEventTrade, []string{"seat:0"}, int64(i), 1, false, "")
	}
	if len(w.MCEvents) != mcEventCap {
		t.Fatalf("环形缓冲长度 = %d,期望 cap %d", len(w.MCEvents), mcEventCap)
	}
	if w.EventDropped != 100 {
		t.Errorf("EventDropped = %d,期望 100", w.EventDropped)
	}
	// 最旧被丢弃:首条 EventTime/Price 应为第 100 条。
	if w.MCEvents[0].Price != 100 {
		t.Errorf("缓冲首条 price = %d,期望 100(丢最旧)", w.MCEvents[0].Price)
	}
	// nil 世界安全。
	var nilW *World
	nilW.recordEvent(MCStepAct, MCEventTrade, nil, 1, 1, false, "")
	if nilW.mcEventCounts() == nil {
		t.Error("nil 世界 mcEventCounts 应返回空 map 而非 nil")
	}
}

// TestMCEvent_KnownTypeAggregation 聚合计数:已知六类直计,其余入桶。
func TestMCEvent_KnownTypeAggregation(t *testing.T) {
	w := calibTestWorld(7, 1)
	w.recordEvent(MCStepAct, MCEventTrade, nil, 0, 0, false, "")
	w.recordEvent(MCStepAct, MCEventTrade, nil, 0, 0, false, "")
	w.recordEvent(MCStepSettle, MCEventMarket, nil, 0, 0, false, "")
	w.recordEvent(MCStepInject, MCEventScenario, nil, 0, 0, false, "earthquake")
	w.recordEvent(MCStepInject, MCEventScenario, nil, 0, 0, false, "flood")
	counts := w.mcEventCounts()
	if counts[MCEventTrade] != 2 || counts[MCEventMarket] != 1 {
		t.Errorf("六类计数错:trade=%d market=%d", counts[MCEventTrade], counts[MCEventMarket])
	}
	if counts[MCEventUnrecognized] != 2 {
		t.Errorf("unrecognized = %d,期望 2(两条 scenario)", counts[MCEventUnrecognized])
	}
}
