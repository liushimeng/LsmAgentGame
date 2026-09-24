package virtual_city

import (
	"encoding/json"
	"strings"
	"testing"

	"LsmAgentGame/game/virtual_city/profession"
)

// TestBuildClientState_Desensitization §7:玩家只见自己 my;他人 my=nil。
func TestBuildClientState_Desensitization(t *testing.T) {
	w := NewWorld(1, [MaxSeats]profession.Card{{ID: "P01"}, {ID: "P02"}})
	w.Players[0] = newPlayerFromCard(0, w.Players[0].Card)
	w.Players[0].Cash = 10000
	w.Players[1] = newPlayerFromCard(1, w.Players[1].Card)
	w.Players[1].Cash = 20000
	w.StartGame()
	// 给 bot_contexts 注入测试内容(2026-09-22 §17 契约 03 §4:夹具改内联卡面)。
	w.Players[0].Card = profession.Card{
		ID: "T03", Title: "测试程序员", Salary: 15000, Expense: 10000, Savings: 40000,
		StartAge: 25, Energy: 6, Network: 4, Cognition: 6, CreditScore: 650,
		HomeDistrict: "tech", RiskPreference: "balanced",
		HealthGrade: "B", Marital: "single",
		OpeningHook: "这是一张内联测试程序员卡,用于 view 反脱敏断言的固定夹具。",
	}
	w.Players[1].Card = profession.Card{
		ID: "T04", Title: "测试医生", Salary: 25000, Expense: 18000, Savings: 100000,
		StartAge: 25, Energy: 5, Network: 5, Cognition: 7, CreditScore: 700,
		HomeDistrict: "residential", RiskPreference: "conservative",
		HealthGrade: "B", Marital: "single",
		OpeningHook: "这是一张内联测试医生卡,用于 view 反脱敏断言的固定夹具。",
	}

	// 模拟座位 0 玩家的 view(只看自己 my;bot_contexts 包含自己 + 观战者视角)。
	cs := BuildClientState("room-1", 0, w, [MaxSeats]string{"u:0", "u:1", "", "", "", "", "", ""},
		[MaxSeats]string{"玩家0", "玩家1", "", "", "", "", "", ""},
		[MaxSeats]bool{false, false, false, false, false, false, false, false},
		[MaxSeats]string{"", "", "", "", "", "", "", ""},
		[MaxSeats]BotTranscript{},
		0, 0,
		nil,
	)
	if cs == nil {
		t.Fatalf("nil state")
	}
	if cs.My == nil {
		t.Errorf("viewer 0 should have my filled")
	}
	if cs.MySeat != 0 {
		t.Errorf("my_seat: got %d, want 0", cs.MySeat)
	}
	if cs.My != nil && cs.My.Cash != 10000 {
		t.Errorf("my cash: got %d, want 10000", cs.My.Cash)
	}
}

// TestBuildClientState_SpectatorView 观战者(viewer=-1)my=nil, my_seat=-1,bot_contexts 全可见。
func TestBuildClientState_SpectatorView(t *testing.T) {
	w := NewWorld(1, [MaxSeats]profession.Card{{ID: "P01"}})
	w.Players[0] = newPlayerFromCard(0, w.Players[0].Card)
	w.Players[0].Cash = 10000
	w.StartGame()
	cs := BuildClientState("room-1", -1, w, [MaxSeats]string{"u:0"},
		[MaxSeats]string{"玩家0"},
		[MaxSeats]bool{true},
		[MaxSeats]string{"MeiTuan-model"},
		[MaxSeats]BotTranscript{},
		0, 0,
		nil,
	)
	if cs.My != nil {
		t.Errorf("spectator my should be nil")
	}
	if cs.MySeat != -1 {
		t.Errorf("spectator my_seat: got %d, want -1", cs.MySeat)
	}
	if len(cs.BotContexts) == 0 {
		t.Errorf("spectator should see bot contexts")
	}
}

// TestBuildClientState_NonViewerBotFiltered 他人玩家视角时,其他 bot_contexts 被过滤。
func TestBuildClientState_NonViewerBotFiltered(t *testing.T) {
	w := NewWorld(1, [MaxSeats]profession.Card{{ID: "P01"}, {ID: "P02"}})
	w.Players[0] = newPlayerFromCard(0, w.Players[0].Card)
	w.Players[1] = newPlayerFromCard(1, w.Players[1].Card)
	w.StartGame()
	transcripts := [MaxSeats]BotTranscript{
		{LastDecisionSummary: "我做了某事"},
		{LastDecisionSummary: "另一个人做了某事"},
		{}, {}, {}, {}, {}, {},
	}
	// viewer=0:应只看到 0 的 bot_contexts。
	cs := BuildClientState("room-1", 0, w,
		[MaxSeats]string{"u:0", "u:1"},
		[MaxSeats]string{"", ""},
		[MaxSeats]bool{true, true},
		[MaxSeats]string{"model1", "model2"},
		transcripts, 0, 0,
		nil,
	)
	if len(cs.BotContexts) != 1 {
		t.Errorf("non-spectator viewer 0 should see 1 bot ctx, got %d", len(cs.BotContexts))
	}
	if len(cs.BotContexts) > 0 && cs.BotContexts[0].Seat != 0 {
		t.Errorf("filtered bot ctx seat: got %d, want 0", cs.BotContexts[0].Seat)
	}
}

// TestBuildClientState_JSONEncodable 顶层 ClientGameState 应能被 encoding/json 序列化。
func TestBuildClientState_JSONEncodable(t *testing.T) {
	w := NewWorld(1, [MaxSeats]profession.Card{{ID: "P01"}})
	w.Players[0] = newPlayerFromCard(0, w.Players[0].Card)
	w.Players[0].Cash = 50000
	w.StartGame()
	cs := BuildClientState("room-1", 0, w,
		[MaxSeats]string{"u:0"},
		[MaxSeats]string{"玩家0"},
		[MaxSeats]bool{false},
		[MaxSeats]string{""},
		[MaxSeats]BotTranscript{},
		0, 0,
		nil,
	)
	if _, err := json.Marshal(cs); err != nil {
		t.Errorf("json marshal: %v", err)
	}
}
// ── 批次20(view 扩字段)──

// viewBatch20World 定价/微观/选举三特性齐备的夹具世界。
func viewBatch20World(t *testing.T) *World {
	t.Helper()
	w := NewWorld(31, [MaxSeats]profession.Card{})
	for s := 0; s < 3; s++ {
		w.Players[s] = newPlayerFromCard(s, synthCard(s, 100000))
		w.Players[s].ActionBudget = 3
	}
	w.StartGame()
	// 副业:seat0 高价 delivery、seat1 中价 delivery(同品类竞争);
	// seat2 content 独占。
	w.Players[0].SideBusiness = &SideBusiness{Kind: "delivery", BaseIncome: 3250, OpenedMonth: 1, PriceTier: PriceTierHigh}
	w.Players[1].SideBusiness = &SideBusiness{Kind: "delivery", BaseIncome: 3250, OpenedMonth: 1}
	w.Players[2].SideBusiness = &SideBusiness{Kind: "content", BaseIncome: 4000, OpenedMonth: 1, PriceTier: PriceTierLow}
	// 微观:boom 价差 20bp + 熔断记录。
	w.Market.CyclePhase = PhaseBoom
	w.Market.StockIndex = 4.00
	w.Market.BreakerUntilMonth = 9
	// 选举启用。
	w.Election.Enabled = true
	w.Election.LastElectionMonth = 1
	w.Election.MayorSeat = 2
	return w
}

func viewBatch20State(w *World, viewer int) *ClientGameState {
	return BuildClientState("room-b20", viewer, w, [MaxSeats]string{"u0", "u1", "u2"},
		[MaxSeats]string{"", "", ""}, [MaxSeats]bool{}, [MaxSeats]string{},
		[MaxSeats]BotTranscript{}, 0, 0, nil)
}

// TestView_SideMarket 顶层 side_market:仅含有经营者品类;seat 升序;份额归一。
func TestView_SideMarket(t *testing.T) {
	w := viewBatch20World(t)
	cs := viewBatch20State(w, 0)
	if cs.SideMarket == nil {
		t.Fatal("side_market missing")
	}
	if len(cs.SideMarket) != 2 {
		t.Fatalf("categories: got %d, want 2 (delivery/content)", len(cs.SideMarket))
	}
	if _, ok := cs.SideMarket["tutoring"]; ok {
		t.Error("tutoring has no operator, must be absent")
	}
	dl := cs.SideMarket["delivery"]
	if len(dl) != 2 || dl[0].Seat != 0 || dl[1].Seat != 1 {
		t.Fatalf("delivery rows/seat-asc: %+v", dl)
	}
	if dl[0].Tier != PriceTierHigh || dl[1].Tier != PriceTierMid {
		t.Errorf("tiers: %+v", dl)
	}
	// Σ=1.0 且高:中 = 0.2:0.3 → 0.4/0.6。
	if !nearlyEq(dl[0].Share, 0.4) || !nearlyEq(dl[1].Share, 0.6) {
		t.Errorf("shares: got %f/%f, want 0.4/0.6", dl[0].Share, dl[1].Share)
	}
	ct := cs.SideMarket["content"]
	if len(ct) != 1 || !nearlyEq(ct[0].Share, 1.0) {
		t.Errorf("content monopoly: %+v", ct)
	}
	// 序列化后 JSON 键名钉死。
	raw, err := json.Marshal(cs.SideMarket)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"delivery"`, `"seat"`, `"tier"`, `"share"`} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("side_market json missing %s: %s", key, raw)
		}
	}
}

// TestView_MySideBusiness my.side_business 扩字段:price_tier / price_tier_cn /
// market_share(0..1);无副业座位 omit。
func TestView_MySideBusiness(t *testing.T) {
	w := viewBatch20World(t)
	cs := viewBatch20State(w, 0)
	if cs.My == nil || cs.My.SideBusiness == nil {
		t.Fatal("my.side_business missing for seat 0")
	}
	sb := cs.My.SideBusiness
	if sb.Kind != "delivery" || sb.KindCN != "跑腿配送" {
		t.Errorf("kind: %+v", sb)
	}
	if sb.PriceTier != PriceTierHigh || sb.PriceTierCN != "高价" {
		t.Errorf("tier: %+v", sb)
	}
	if !nearlyEq(sb.MarketShare, 0.4) {
		t.Errorf("market_share: got %f, want 0.4", sb.MarketShare)
	}
	// 独占座位 share=1.0。
	cs2 := viewBatch20State(w, 2)
	if cs2.My == nil || cs2.My.SideBusiness == nil || !nearlyEq(cs2.My.SideBusiness.MarketShare, 1.0) {
		t.Fatalf("monopoly share: %+v", cs2.My)
	}
	// 无副业座位 → nil(omit)。
	w.Players[1].SideBusiness = nil
	cs3 := viewBatch20State(w, 1)
	if cs3.My != nil && cs3.My.SideBusiness != nil {
		t.Errorf("no side business must omit, got %+v", cs3.My.SideBusiness)
	}
	// JSON 键名。
	raw, _ := json.Marshal(cs.My.SideBusiness)
	for _, key := range []string{`"price_tier"`, `"price_tier_cn"`, `"market_share"`, `"tier_set_month"`} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("my.side_business json missing %s: %s", key, raw)
		}
	}
}

// TestView_MyTierSetMonthAndT1Lock FE-2 集成字段:side_business.tier_set_month
// (同月改档禁用以服务端为准)与 my.stock_t1_locked(冻结角标数据源,0 omit)。
func TestView_MyTierSetMonthAndT1Lock(t *testing.T) {
	w := viewBatch20World(t)
	// 改档月:未改 = 0;set_side_price 后 = 当月。
	if cs := viewBatch20State(w, 0); cs.My.SideBusiness.TierSetMonth != 0 {
		t.Errorf("untouched tier_set_month: got %d, want 0", cs.My.SideBusiness.TierSetMonth)
	}
	w.Month = 7
	if _, e := w.ApplyAction(0, Action{Type: ActSetSidePrice, Tier: PriceTierLow}); e != nil {
		t.Fatalf("set price: %v", e)
	}
	cs := viewBatch20State(w, 0)
	if cs.My.SideBusiness.TierSetMonth != 7 {
		t.Fatalf("tier_set_month: got %d, want 7(当月权威禁改标记)", cs.My.SideBusiness.TierSetMonth)
	}
	// T+1 冻结份数。
	if viewBatch20State(w, 0).My.StockT1Locked != 0 {
		t.Error("initial t1 must be 0/omit")
	}
	w.Players[1].StockT1Locked = 1234
	cs1 := viewBatch20State(w, 1)
	if cs1.My.StockT1Locked != 1234 {
		t.Fatalf("my.stock_t1_locked: got %d, want 1234", cs1.My.StockT1Locked)
	}
	raw, _ := json.Marshal(cs1.My)
	if !strings.Contains(string(raw), `"stock_t1_locked":1234`) {
		t.Errorf("my json missing stock_t1_locked: %s", raw)
	}
}

// TestView_MarketMicrostructure market 快照补 stock_buy_unit / stock_sell_unit /
// spread_bps / breaker_until(批次20 文档3 B4)。
func TestView_MarketMicrostructure(t *testing.T) {
	w := viewBatch20World(t)
	cs := viewBatch20State(w, 0)
	mj := cs.Market
	if !nearlyEq(mj.StockBuyUnit, 4.0*(1+0.0020/2)) || !nearlyEq(mj.StockSellUnit, 4.0*(1-0.0020/2)) {
		t.Errorf("dual quotes: got %f/%f, want %f/%f", mj.StockBuyUnit, mj.StockSellUnit, 4.004, 3.996)
	}
	if mj.SpreadBps != 20 {
		t.Errorf("spread_bps: got %d, want 20", mj.SpreadBps)
	}
	if mj.BreakerUntil != 9 {
		t.Errorf("breaker_until: got %d, want 9", mj.BreakerUntil)
	}
	raw, _ := json.Marshal(mj)
	for _, key := range []string{`"stock_buy_unit"`, `"stock_sell_unit"`, `"spread_bps"`, `"breaker_until"`} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("market json missing %s: %s", key, raw)
		}
	}
}

// TestView_ElectionNextElectionMonth public_services 补 next_election_month;
// 未启用 → 0(omitempty 不出现)。
func TestView_ElectionNextElectionMonth(t *testing.T) {
	w := viewBatch20World(t)
	cs := viewBatch20State(w, 0)
	if cs.PublicSvc == nil {
		t.Fatal("public_services snapshot missing")
	}
	if cs.PublicSvc.NextElectionMonth != 1+ElectionIntervalMonths {
		t.Errorf("next_election_month: got %d, want %d", cs.PublicSvc.NextElectionMonth, 1+ElectionIntervalMonths)
	}
	raw, _ := json.Marshal(cs.PublicSvc)
	if !strings.Contains(string(raw), `"next_election_month":49`) {
		t.Errorf("json: %s", raw)
	}
	// 关闭态 omit。
	w.Election.Enabled = false
	cs2 := viewBatch20State(w, 0)
	raw2, _ := json.Marshal(cs2.PublicSvc)
	if strings.Contains(string(raw2), "next_election_month") {
		t.Errorf("disabled must omit: %s", raw2)
	}
}

// TestView_ElectionStipendStopped public_services 补 stipend_stopped(批次20
// 文档3 A3,FE-2 当选横幅徽标):津贴断发 → true 下发;正常发放(false)→
// omitempty 不下发。
func TestView_ElectionStipendStopped(t *testing.T) {
	w := viewBatch20World(t)
	// 正常态:CivicElection.StipendStopped=false → 字段不下发。
	cs := viewBatch20State(w, 0)
	if cs.PublicSvc == nil {
		t.Fatal("public_services snapshot missing")
	}
	if cs.PublicSvc.StipendStopped {
		t.Error("default StipendStopped should be false")
	}
	raw, _ := json.Marshal(cs.PublicSvc)
	if strings.Contains(string(raw), "stipend_stopped") {
		t.Errorf("normal (paying) must omit: %s", raw)
	}
	// 断发态:true → 显式下发。
	w.Election.StipendStopped = true
	cs2 := viewBatch20State(w, 0)
	if !cs2.PublicSvc.StipendStopped {
		t.Error("snapshot StipendStopped should mirror world.Election.StipendStopped")
	}
	raw2, _ := json.Marshal(cs2.PublicSvc)
	if !strings.Contains(string(raw2), `"stipend_stopped":true`) {
		t.Errorf("stopped must emit \"stipend_stopped\":true: %s", raw2)
	}
}
