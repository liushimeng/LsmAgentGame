package virtual_city

// view_json_tags_test.go — game.state 视图层 JSON tag 契约锁定(2026-10-21 批次69)。
//
// 背景:批次 62 遗留 `MinskyOverview.PonziRatio` 的 tag 拼作 `ponzy_ratio`
// (前端读 `ponzi_ratio`)⇒ 庞氏族占比恒为 undefined,批次 69 城市总览聚合时
// 才暴露。本文件把「后端 tag ⇄ 前端类型字段」的对账结论固化成可执行断言,
// 防止同类拼写漂移再次发生(§130「声明了却从不接线」的前置防线)。
//
// 断言分三类:
//  1. 关键键名精确锁定(小结构体:键集合全等)—— 改名即红;
//  2. 大结构体键名包含性锁定(子集)—— 加字段不红、改名即红;
//  3. 遗留错拼键全局缺席 —— `ponzy_ratio` 等错拼不得出现在任何 game.state 帧。

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"LsmAgentGame/game/virtual_city/profession"
)

// jsonTagKeysOf 反射读取结构体**声明的** JSON 键集合(排序后)。
//
// 用反射而非序列化零值:带 omitempty 的可选字段在零值时不下发,序列化取不到
// 键集;而本文件的断言目标正是「声明了哪些键」(契约面),与取值无关。
func jsonTagKeysOf(t *testing.T, v any) []string {
	t.Helper()
	typ := reflect.TypeOf(v)
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		t.Fatalf("jsonTagKeysOf: 非结构体 %T", v)
	}
	out := make([]string, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		tag := f.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.Split(tag, ",")[0]
		if name == "" {
			name = f.Name // 无具名 tag ⇒ 沿用字段名
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// assertKeysExact 断言实际键集合与期望**完全一致**(键集合全等)。
func assertKeysExact(t *testing.T, label string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s: 键数 got=%d want=%d\n got=%v\nwant=%v", label, len(got), len(want), got, want)
		return
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("%s: 键名不一致 got=%v want=%v", label, got, want)
			return
		}
	}
}

// assertKeysContain 断言实际键集合**包含**全部期望键(允许有额外键)。
func assertKeysContain(t *testing.T, label string, got, want []string) {
	t.Helper()
	have := make(map[string]bool, len(got))
	for _, k := range got {
		have[k] = true
	}
	for _, k := range want {
		if !have[k] {
			t.Errorf("%s: 缺少键 %q(got=%v)", label, k, got)
		}
	}
}

// ── 1. 关键键名精确锁定 ────────────────────────────────────────────────

// TestMinskyOverview_JSONTagPonziRatio 锁定 `ponzi_ratio` 键名
// (批次 62 遗留错拼 `ponzy_ratio` 已修,见 view.go::MinskyOverview)。
func TestMinskyOverview_JSONTagPonziRatio(t *testing.T) {
	m := MinskyOverview{
		PonziCount: 3, SpecCount: 2, HedgeCount: 5,
		PonziRatio: 0.3, CooldownLeft: 7, MomentCount: 1,
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(raw)
	if !strings.Contains(s, `"ponzi_ratio":0.3`) {
		t.Errorf("缺少正确键 ponzi_ratio:%s", s)
	}
	if strings.Contains(s, "ponzy_ratio") {
		t.Errorf("仍出现错拼键 ponzy_ratio:%s", s)
	}
	assertKeysExact(t, "MinskyOverview", jsonTagKeysOf(t, m), []string{
		"cooldown_left", "hedge_count", "moment_count",
		"ponzi_count", "ponzi_ratio", "spec_count",
	})
}

// TestCycleJSON_LPR5YContractKey 5Y LPR 契约正名 `lpr_5y` 单键下发:
//
// 批次71 L1:前端已统一读 `lpr_5y`(批次69 双键下发期的兼容别名 `lpr5y`
// 已下线)⇒ 别名删除后前端零影响,且消除「两处事实来源」(§130)。
func TestCycleJSON_LPR5YContractKey(t *testing.T) {
	c := CycleJSON{Phase: "boom", LPR: 0.035, CPI: 0.02, MonthsLeft: 4, LPR5Y: 0.04}
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(raw)
	if !strings.Contains(s, `"lpr_5y":0.04`) {
		t.Errorf("缺少契约键 lpr_5y:%s", s)
	}
	if strings.Contains(s, `"lpr5y"`) {
		t.Errorf("仍出现已下线的兼容键 lpr5y:%s", s)
	}
	assertKeysExact(t, "CycleJSON", jsonTagKeysOf(t, c), []string{
		"cpi", "lpr", "lpr_5y", "minsky_moment_count", "months_left",
		"phase", "ponzi_count", "ponzi_ratio",
	})
}

// TestViewLeafJSONTags 叶子视图结构体的键集合全等锁定(改名即红)。
func TestViewLeafJSONTags(t *testing.T) {
	cases := []struct {
		label string
		value any
		want  []string
	}{
		{"CentralBankJSON", CentralBankJSON{}, []string{
			"cpi", "credit_tightness", "loan_quota_factor", "lpr", "m0_cny", "m1_cny",
			"m2_cny", "mb_cny", "money_multiplier", "policy_rate",
		}},
		{"PredictionSummary", PredictionSummary{}, []string{
			"active_scenarios", "months_observed", "run_id", "start_month", "tier",
		}},
		{"FamilyStatsJSON", FamilyStatsJSON{}, []string{
			"children_count", "parents_alive_count", "private_edu_count",
		}},
		{"InheritanceEventJSON", InheritanceEventJSON{}, []string{
			"amount_cny", "from_seat", "month", "to_seats",
		}},
		{"SideMarketRowJSON", SideMarketRowJSON{}, []string{"seat", "share", "tier"}},
		{"BubbleSnapshot", BubbleSnapshot{}, []string{
			"cash_ratio", "leverage_ratio", "message", "months_in_state",
			"state", "turnover_ratio",
		}},
		{"LiquiditySpreadSnapshot", LiquiditySpreadSnapshot{}, []string{
			"bond", "collectible", "equity", "gold", "house", "phase_multiplier", "stock",
		}},
		{"ElectionVote", ElectionVote{}, []string{
			"network_score", "satisfaction", "score", "seat", "wealth_score",
		}},
		{"SocietyTrendJSON", SocietyTrendJSON{}, []string{
			"gini_income", "gini_wealth", "mobility_young", "month", "top10_pct",
		}},
		{"WealthLayerJSON", WealthLayerJSON{}, []string{
			"avg_wealth", "count", "name", "total_wealth", "wealth_pct",
		}},
		// 批次70:挂单簿视图(前端 VirtualCityListingBook 逐字对齐)。
		{"ListingBookJSON", ListingBookJSON{}, []string{
			"auctions", "listings", "negotiates", "p2p_loans",
		}},
		{"ListingJSON", ListingJSON{}, []string{
			"ask_cny", "create_month", "expire_month", "id", "payload", "seat", "status", "type",
		}},
		{"ListingPayloadJSON", ListingPayloadJSON{}, []string{
			"asset", "info", "loan",
		}},
		{"TradeAssetPayloadJSON", TradeAssetPayloadJSON{}, []string{
			"kind", "min_cny", "name", "price", "units", "value_cny",
		}},
		{"TradeLoanPayloadJSON", TradeLoanPayloadJSON{}, []string{
			"direction", "max_rate", "need_guarantee", "principal_cny", "term_n",
		}},
		{"TradeInfoPayloadJSON", TradeInfoPayloadJSON{}, []string{
			"category", "detail_hash", "min_bid_cny", "title",
		}},
		{"NegotiateSessionJSON", NegotiateSessionJSON{}, []string{
			"create_month", "expire_month", "id", "last_offer_by", "last_offer_cny",
			"listing_id", "proposer_seat", "respond_seat", "status", "turns",
		}},
		{"NegotiateTurnJSON", NegotiateTurnJSON{}, []string{
			"action", "comment", "from", "offer_cny", "rate",
		}},
		{"P2PLoanJSON", P2PLoanJSON{}, []string{
			"annual_rate", "balance_cny", "borrower_seat", "create_month", "guarantor_seat",
			"id", "lender_seat", "listing_id", "monthly_payment", "months_left", "overdue",
			"principal_cny", "term_n",
		}},
		{"AuctionJSON", AuctionJSON{}, []string{
			"bids", "current_cny", "current_seat", "end_month", "id", "kind",
			"listing_id", "reserve_cny", "seat", "start_cny", "status",
		}},
		{"AuctionBidJSON", AuctionBidJSON{}, []string{
			"amount_cny", "month", "revealed", "seat",
		}},
		// 批次70:感知富形状(前端 VirtualCitySenseResult 对齐)。
		{"SenseEntry", SenseEntry{}, []string{
			"district", "events", "kind", "month", "people", "smells", "sounds",
			"text", "things", "utterances",
		}},
		{"SenseNeighborBrief", SenseNeighborBrief{}, []string{
			"card_id", "district", "kind", "mood_hint", "name", "occupation", "seat",
		}},
		// 批次71 §9:校准与验证契约(前端 VirtualCityCalibration 逐字对齐)。
		{"OffLedgerEntry", OffLedgerEntry{}, []string{
			"amount", "month", "note", "reason", "seat",
		}},
		{"MonthlyAudit", MonthlyAudit{}, []string{
			"indicators", "invariants", "month", "redlines", "state_hash",
		}},
		{"InvariantCheck", InvariantCheck{}, []string{
			"actual", "detail", "expected", "id", "name", "no_sample", "passed", "severity",
		}},
		{"RedlineHit", RedlineHit{}, []string{
			"actual", "detail", "id", "name", "threshold", "triggered",
		}},
		{"CalibrationIndicators", CalibrationIndicators{}, []string{
			"alive_seats", "bankruptcy_rate", "cpi", "gini", "money_expansion",
			"off_ledger_cny", "total_seats", "total_wealth", "unemployment",
		}},
		{"CalibrationJSON", CalibrationJSON{}, []string{
			"auction_participation_median", "category_annualized", "event_counts",
			"history", "indicators", "invariants", "month", "outcome_causes",
			"profession_outcomes", "redlines", "runs", "state_hash",
		}},
		{"CalibrationRunJSON", CalibrationRunJSON{}, []string{
			"bankruptcy_rate", "finished_at", "invariant_fails", "money_expansion",
			"outcome_counts", "redlines_triggered", "run_id", "seed", "state_hash",
			"strategy", "total_steps",
		}},
		{"CalibrationHistoryJSON", CalibrationHistoryJSON{}, []string{
			"hash", "month", "ok",
		}},
		{"RunRecord", RunRecord{}, []string{
			"auction_participation_median", "bankruptcy_rate", "category_annualized",
			"event_counts", "finished_at", "invariant_fails", "invariants_ok",
			"money_expansion", "off_ledger_cny", "outcome_causes", "outcome_counts",
			"phase", "population_size", "profession_outcomes", "redlines_triggered",
			"run_id", "seed", "state_hash", "strategy", "total_assets_end",
			"total_assets_start", "total_steps",
		}},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			assertKeysExact(t, c.label, jsonTagKeysOf(t, c.value), c.want)
		})
	}
}

// ── 2. 大结构体键名包含性锁定(改字段名即红) ───────────────────────────

// TestViewContainerJSONTags 容器级视图结构体:期望键必须齐备
// (前端 `ClientWeb/src/types/virtualCity.ts` 对应 interface 逐字对齐)。
func TestViewContainerJSONTags(t *testing.T) {
	cases := []struct {
		label string
		value any
		want  []string
	}{
		{"ClientGameState", ClientGameState{}, []string{
			"room_id", "game_kind", "status", "month", "age", "phase", "cycle", "market",
			"central_bank", "max_seat", "next_month_at", "game_started_at", "city_clock_ms",
			"time_ratio", "season", "weather", "weather_intensity", "city_name", "city_info",
			"players", "my_seat", "my", "bot_contexts", "ledger_recent", "events_recent",
			"minsky_overview", "consumer_market", "labor_market", "society", "surveys",
			"scenarios_recent", "prediction", "flow_stat", "treasury", "supply_chain",
			"industrial_clusters", "fin_market", "public_services", "city", "side_market",
			"family", "inheritance_log", "bubble", "liquidity_spreads",
			// 批次70 §契约补全:挂单簿 + 提前还款资格。
			"listing_book", "early_repay_eligible",
			// 批次71 §9:校准快照(状态哈希 + 不变量 + D 指标 + 红线 + 历史)。
			"calibration",
		}},
		{"MyJSON", MyJSON{}, []string{
			"cash", "savings_deposit", "salary", "spouse_income", "side_income",
			"side_business", "passive_income", "monthly", "resources", "assets", "loans",
			"pension_cny", "credit_score", "family", "fi_index", "net_worth", "goals",
			"consumption_by_goods", "insurance", "local_pos", "stock_t1_locked",
			"inheritance_cny", "behavior",
		}},
		{"PlayerJSON", PlayerJSON{}, []string{
			"seat", "account", "nickname", "is_bot", "model_display", "profession",
			"district", "home_district", "alive", "retired", "age", "resources",
			"net_worth", "fi_index", "income_band", "status_icon", "last_action", "ending",
			"minsky_tier", "debt_to_income", "consumption_level", "herding_index",
			"insured_kinds", "local_pos", "avatar",
		}},
		{"SocietyJSON", SocietyJSON{}, []string{
			"gini", "quintiles", "circles", "total_wealth", "median_wealth", "mean_wealth",
			"percentiles", "lorenz_points", "pyramid_layers", "gini_income", "top1_pct",
			"top10_pct", "bottom50_pct", "wealth_quintiles", "pyramid", "mobility_young",
			"trend",
		}},
		{"PublicServicesJSON", PublicServicesJSON{}, []string{
			"edu_quality", "med_quality", "pension_level", "housing_afford", "employment",
			"edu_stock_wan", "med_stock_wan", "insider_fines_total", "insider_cases",
			"antitrust_splits", "election_enabled", "mayor_seat", "last_votes",
			"next_election_month", "stipend_stopped",
		}},
		{"MarketJSON", MarketJSON{}, []string{
			"stock_index", "gold_price", "bond_yield", "districts", "stock_buy_unit",
			"stock_sell_unit", "spread_bps", "breaker_until", "exo_anchor",
		}},
		{"BotCtxJSON", BotCtxJSON{}, []string{
			"month", "last_decision_month", "updated_at", "active", "seat",
			"last_decision_summary", "last_tool_input", "last_tool_result",
			"heart_thought", "last_senses", "local_pos",
		}},
		{"BehaviorJSON", BehaviorJSON{}, []string{
			"cognition", "bias_profile", "bias_immunity", "anchoring_index",
			"herding_index", "savings_streak_months", "deferred_payments",
			"delayed_pay_index", "mental_accounts", "mental_account_merged", "last_reflect",
		}},
		{"FlowStatJSON", FlowStatJSON{}, []string{
			"period", "period_label", "nodes", "links", "total_in_cny", "total_out_cny",
		}},
		{"SurveyJSON", SurveyJSON{}, []string{
			"id", "question", "options", "launch_month", "deadline_month", "status",
			"answers_count", "result",
		}},
		{"MyFamilyJSON", MyFamilyJSON{}, []string{
			"marital", "children", "parents", "kids", "support_cny", "edu_cny",
			"child_in_cny", "totals",
		}},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			assertKeysContain(t, c.label, jsonTagKeysOf(t, c.value), c.want)
		})
	}
}

// ── 3. 遗留错拼键全局缺席 ──────────────────────────────────────────────

// TestBuildClientState_NoLegacyTypoKeys 全量 game.state 帧内不得出现
// 历史错拼键(前端读不到 ⇒ 静默 undefined ⇒ 界面恒「—」)。
func TestBuildClientState_NoLegacyTypoKeys(t *testing.T) {
	w := NewWorld(1, [MaxSeats]profession.Card{{ID: "P01"}})
	w.Players[0] = newPlayerFromCard(0, w.Players[0].Card)
	w.Players[0].Cash = 10000
	w.StartGame()
	cs := BuildClientState("room-1", 0, w,
		[MaxSeats]string{"u:0"},
		[MaxSeats]string{"玩家0"},
		[MaxSeats]bool{false},
		[MaxSeats]string{""},
		[MaxSeats]BotTranscript{},
		0, 0, 0,
		nil, nil,
	)
	raw, err := json.Marshal(cs)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(raw)
	// 错拼键:庞氏族占比(批次 62 遗留)。
	if strings.Contains(s, "ponzy_ratio") {
		t.Errorf("game.state 仍含错拼键 ponzy_ratio")
	}
	// 正确键必须存在(且带值)。
	if !strings.Contains(s, `"ponzi_ratio"`) {
		t.Errorf("game.state 缺少 minsky_overview.ponzi_ratio")
	}
}
