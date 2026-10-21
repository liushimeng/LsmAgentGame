// Package virtual_city — scenario.go: 场景推演与预测运行引擎
// (2026-10-21 §场景推演)。
//
// 定位:虚拟城市 = 全 Agent 驱动的城市模拟系统(非游戏);人类 = 观察者 +
// 条件触发者。本文件提供「条件注入」—— 把外部冲击(市场与经济 / 社会与
// 舆情 / 自然灾害 / 未知灾难)确定性注入正在运行的模拟,由既有 World 子系统
// 吸收并向居民 Agent 与观察者传导,用于预测运行:
//
//	基线快照 → 注入场景 → N 月演化(逐月衰减) → 指标对比 → 报告
//
// 纪律:
//   - **零 rand**:所有幅度由 kind / magnitude / target 线性派生,不消耗
//     w.Rand(固定种子存量模拟回归零偏移,与 exogenous.go 同款契约);
//   - **只改既有状态字段**:Goods.PriceIdx/SupplyIdx、Labor.Unemployment、
//     Market.StockIndex/DriftExtra/DistrictIdx、Treasury.Cash、居民 Cash、
//     CivicElection.SentimentBias —— 不引入平行账本;
//   - **锁纪律(§92a)**:本文件的 engine 级函数(applyScenarioToWorld /
//     scenarioMonthStep)全部由房间层持 r.mu 调用;广播由 room.go 在锁外
//     经 hooks.OnEvent 完成;
//   - **月度衰减**:注入记录进 World.Scenarios,SettleMonth ②.0(劳动力月步
//     之后)调用 scenarioMonthStep,强度按 remaining/total 线性衰减,到期
//     自动移除并播报「影响结束」;
//   - **事件**:注入 / 消退均经 w.emitEvent(EventScenario) 入流。
package virtual_city

import (
	"fmt"
	"math"
	"strings"

	"LsmAgentGame/errcode"
)

// 场景目录分组(仅前端展示分组;契约 §5.1)。
const (
	ScenarioGroupMarket  = "市场与经济"
	ScenarioGroupSociety = "社会与舆情"
	ScenarioGroupNature  = "自然灾害"
	ScenarioGroupUnknown = "未知灾难"
)

// 场景 kind 常量(契约 §5.1:10 个,前端按 kind 派发强度/参数控件)。
const (
	ScenarioGoodsPriceShock = "goods_price_shock"
	ScenarioPolicyShock     = "policy_shock"
	ScenarioMarketCrash     = "market_crash"
	ScenarioSurveyPulse     = "survey_pulse"
	ScenarioElectionMood    = "election_sentiment"
	ScenarioEarthquake      = "earthquake"
	ScenarioTsunami         = "tsunami"
	ScenarioExtremeDisaster = "extreme_disaster"
	ScenarioMeteorite       = "meteorite"
	ScenarioAlienInvasion   = "alien_invasion"
)

// 场景引擎常量。
const (
	// EventScenario 场景推演事件类型(EventRecord.Type;注入/消退两条)。
	EventScenario = "scenario"

	scenarioMinMagnitude = 1 // 强度下限 L1
	scenarioMaxMagnitude = 5 // 强度上限 L5

	// scenarioDefaultDurationMonths 未指定 duration_months 时的全局兜底;
	// 各场景可自带更合适的 defaultDurationMonths(例如海啸比舆情脉冲长)。
	scenarioDefaultDurationMonths = 6
	scenarioMaxDurationMonths     = 24

	// scenarioMaxActive 同时生效的场景上限(超出 → 35057)。限流目的:
	// 避免条件触发者刷屏式叠加导致指标失去可解释性(预测运行可比性)。
	scenarioMaxActive = 8

	// scenarioRecordCap 注入记录环形缓冲容量(= 契约「最近 20 条」)。
	scenarioRecordCap = 20

	scenarioNoteMaxRunes = 60
	// scenarioElectionBiasCap 选举满意度偏置上下限(与 ComputeVotes 的
	// satisfaction ∈ [0,100] 同量纲;±30 足以翻转边缘选举且不破坏口径)。
	scenarioElectionBiasCap = 30.0
	// scenarioPriceIdxMin/Max 商品价格指数安全区间(防极端叠加把篮子打穿)。
	scenarioPriceIdxMin = 10.0
	scenarioPriceIdxMax = 10000.0
)

// scenarioMagnitudes 强度档位 L1–L5(目录统一下发;ScenarioCatalog 每次
// 复制一份,调用方修改返回值不会污染目录)。
var scenarioMagnitudes = []int{1, 2, 3, 4, 5}

// ScenarioSpec 场景目录条目(契约 §5.1 GET scenario_catalog)。
type ScenarioSpec struct {
	Kind             string   `json:"kind"`
	Name             string   `json:"name"`
	Group            string   `json:"group"`
	Description      string   `json:"description"`
	Magnitudes       []int    `json:"magnitudes"`
	DefaultMagnitude int      `json:"default_magnitude"`
	Params           []string `json:"params"`
}

// ScenarioAppliedRecord 单条注入记录(World.ScenarioApplied 环形缓冲;
// view 层 game.state.scenarios_recent 与 GET .../scenarios 共用)。
//
// 批次71 §4.5(I6 场景注入留痕):补齐《场景推演》§7.3 要求的
// target / duration / seed / snapshot_id 四个字段 —— 缺任一项 ⇒
// 注入记录不可复现,I6 判定失败。
//
// ⚠ 新增字段**一律不带 omitempty**(缺省零值下发,旧前端忽略未知键即可)。
type ScenarioAppliedRecord struct {
	ID           string `json:"id"`            // "SC1"…(World.ScenarioSeq 自增)
	Kind         string `json:"kind"`          // 场景 kind
	Magnitude    int    `json:"magnitude"`     // 强度 L1–L5
	AppliedMonth int    `json:"applied_month"` // 注入时的主钟月份
	Note         string `json:"note"`          // 条件触发者备注(≤60 rune)
	At           int64  `json:"at"`            // 注入时刻 unix 毫秒
	// Target 作用范围(城区/行业/商品类目;空 = 全域)。
	Target string `json:"target"`
	// DurationMonths 持续月数。
	DurationMonths int `json:"duration_months"`
	// Seed 注入时的 run_seed(房间层 seed;0 = 未知)。
	Seed int64 `json:"seed"`
	// SnapshotID 关联的基线快照 id(预测运行 id;无基线 run = 空串)。
	SnapshotID string `json:"snapshot_id"`
}

// ScenarioEffect 单条即时效果(注入响应 data.effects)。
//
// Metric 取值:stock_index / gold_price / house_price_index /
// goods_price_index / goods_supply_index(百分比变化,%);
// unemployment_rate / policy_rate / election_sentiment(百分点,pp);
// credit_window_factor(乘数,无量纲);
// resident_cash / treasury_cash(元,负=损失)。
type ScenarioEffect struct {
	Metric string  `json:"metric"`
	Delta  float64 `json:"delta"`
	Text   string  `json:"text"`
}

// ScenarioActive 进行中的场景(World.Scenarios;月度衰减状态)。
type ScenarioActive struct {
	ID              string
	Kind            string
	Group           string
	Magnitude       int
	TotalMonths     int
	RemainingMonths int
	AppliedMonth    int
	Target          string
	Note            string
	At              int64
	// electionBiasBase 选举满意度偏置基数(注入时刻),月度按
	// remaining/total 线性衰减;仅在 Election 存在时使用。
	electionBiasBase float64
}

// scenarioProfile 单场景的确定性幅度表(全部 × magnitude;负数 = 下行)。
type scenarioProfile struct {
	CashDamageFrac      float64 // 居民现金损失比例(存活居民,按 seat 升序)
	StockShockPct       float64 // 股指即时冲击(一次性)
	GoldShockPct        float64 // 黄金即时冲击(避险)
	HouseShockPct       float64 // 八区房价指数即时冲击
	GoodsPriceShockPct  float64 // 商品价格指数即时冲击
	GoodsSupplyShockPct float64 // 供给指数即时冲击(负 = 供给受损)
	GoodsMonthlyDrift   float64 // 商品价格指数月度续涨(× decay)
	StockDriftMonthly   float64 // 股指月度漂移加成(× decay;叠加在泡沫漂移上)
	UnemploymentAdd     float64 // 失业率即时上行(百分点小数)
	UnemploymentMonthly float64 // 失业率月度上行(× decay)
	PolicyRateDelta     float64 // 政策利率即时调整(小数;方向由 stance 决定)
	CreditWindowDelta   float64 // 信贷窗口乘数即时调整(方向由 stance 决定)
	TreasurySpendCNY    int64   // 国库一次性支出(重建/救市/军费)
	TreasuryMonthlyCNY  int64   // 国库月度支出(× decay)
	ElectionBias        float64 // 选举满意度偏置(百分点;即时 + 月度线性衰减)
}

// scenarioDef 目录 + 幅度 + 默认持续月数。
type scenarioDef struct {
	spec            ScenarioSpec
	profile         scenarioProfile
	defaultDuration int
}

// scenarioDefs 场景目录唯一事实来源(顺序 = 前端分组展示顺序)。
var scenarioDefs = []scenarioDef{
	// ── 市场与经济 ──
	{
		spec: ScenarioSpec{
			Kind: ScenarioGoodsPriceShock, Name: "商品价格冲击", DefaultMagnitude: 3,
			Group:       ScenarioGroupMarket,
			Description: "上游成本/供给扰动推高消费品价格指数,经消费篮子传导至 CPI 与居民实际购买力。target 可指定单一品类(food/clothing/housing/household/transport/education/healthcare/misc),缺省 = 全品类。",
			Params:      []string{"goods"},
		},
		profile: scenarioProfile{
			GoodsPriceShockPct:  0.03,
			GoodsSupplyShockPct: -0.015,
			GoodsMonthlyDrift:   0.004,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioPolicyShock, Name: "政策冲击", DefaultMagnitude: 3,
			Group:       ScenarioGroupMarket,
			Description: "财政/货币政策突变:政策利率与信贷窗口调整,企业投资与就业随之收缩或扩张。target=tight(紧缩,缺省)|loose(宽松)。",
			Params:      []string{"stance"},
		},
		profile: scenarioProfile{
			PolicyRateDelta:     0.0025,
			CreditWindowDelta:   -0.10,
			StockDriftMonthly:   -0.002,
			UnemploymentMonthly: 0.003,
			GoodsMonthlyDrift:   0.001,
			ElectionBias:        -4,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioMarketCrash, Name: "市场崩盘", DefaultMagnitude: 3,
			Group:       ScenarioGroupMarket,
			Description: "股指一次性深跌 + 后续阴跌,房产与风险资产同步重估,失业率上行,国库启动救市支出。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			StockShockPct:       -0.06,
			GoldShockPct:        0.01,
			HouseShockPct:       -0.02,
			StockDriftMonthly:   -0.008,
			UnemploymentMonthly: 0.004,
			TreasurySpendCNY:    300000,
			ElectionBias:        -6,
		},
		defaultDuration: 4,
	},
	// ── 社会与舆情 ──
	{
		spec: ScenarioSpec{
			Kind: ScenarioSurveyPulse, Name: "调研问卷反馈推演", DefaultMagnitude: 2,
			Group:       ScenarioGroupSociety,
			Description: "把一次社会调研的舆情信号放大为消费/风险偏好脉冲,观察需求端对价格与股指的传导。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsPriceShockPct: 0.008,
			GoodsMonthlyDrift:  0.001,
			StockDriftMonthly:  0.001,
			ElectionBias:       3,
		},
		defaultDuration: 3,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioElectionMood, Name: "选举舆情", DefaultMagnitude: 3,
			Group:       ScenarioGroupSociety,
			Description: "选举前舆情偏置改变居民满意度与投票倾向,进而影响市长选举结果与后续财政取向。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			ElectionBias:      8,
			StockDriftMonthly: 0.0005,
		},
		defaultDuration: 6,
	},
	// ── 自然灾害 ──
	{
		spec: ScenarioSpec{
			Kind: ScenarioEarthquake, Name: "地震", DefaultMagnitude: 3,
			Group:       ScenarioGroupNature,
			Description: "城区建筑受损:居民现金损失、房价下修、供给链中断、国库重建支出,失业率上行。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			CashDamageFrac:      0.01,
			HouseShockPct:       -0.015,
			GoodsSupplyShockPct: -0.02,
			StockDriftMonthly:   -0.001,
			UnemploymentMonthly: 0.002,
			TreasurySpendCNY:    500000,
			TreasuryMonthlyCNY:  100000,
			ElectionBias:        -5,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioTsunami, Name: "海啸", DefaultMagnitude: 3,
			Group:       ScenarioGroupNature,
			Description: "沿江沿海城区被淹:住房与物流受损更重,重建周期更长。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			CashDamageFrac:      0.012,
			HouseShockPct:       -0.02,
			GoodsSupplyShockPct: -0.03,
			StockDriftMonthly:   -0.0015,
			UnemploymentMonthly: 0.003,
			TreasurySpendCNY:    800000,
			TreasuryMonthlyCNY:  150000,
			ElectionBias:        -6,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioExtremeDisaster, Name: "极端灾难事件", DefaultMagnitude: 4,
			Group:       ScenarioGroupNature,
			Description: "复合型极端灾难(基础设施瘫痪 + 供给中断 + 财政挤兑),多指标同时恶化。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			CashDamageFrac:      0.02,
			StockShockPct:       -0.05,
			HouseShockPct:       -0.03,
			GoodsPriceShockPct:  0.02,
			GoodsSupplyShockPct: -0.04,
			StockDriftMonthly:   -0.002,
			UnemploymentMonthly: 0.006,
			TreasurySpendCNY:    1500000,
			TreasuryMonthlyCNY:  300000,
			ElectionBias:        -10,
		},
		defaultDuration: 12,
	},
	// ── 未知灾难 ──
	{
		spec: ScenarioSpec{
			Kind: ScenarioMeteorite, Name: "陨石撞击", DefaultMagnitude: 4,
			Group:       ScenarioGroupUnknown,
			Description: "局部毁灭性撞击:资本市场恐慌、房产重估、供给链断裂与长期重建。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			CashDamageFrac:      0.025,
			StockShockPct:       -0.08,
			GoldShockPct:        0.03,
			HouseShockPct:       -0.05,
			GoodsPriceShockPct:  0.04,
			GoodsSupplyShockPct: -0.06,
			StockDriftMonthly:   -0.003,
			UnemploymentMonthly: 0.008,
			TreasurySpendCNY:    2500000,
			TreasuryMonthlyCNY:  500000,
			ElectionBias:        -12,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioAlienInvasion, Name: "外星人入侵", DefaultMagnitude: 5,
			Group:       ScenarioGroupUnknown,
			Description: "未知文明入侵:全社会不确定性飙升,股指崩塌、避险资产飙升、财政转入战时支出。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			CashDamageFrac:      0.01,
			StockShockPct:       -0.10,
			GoldShockPct:        0.05,
			HouseShockPct:       -0.04,
			GoodsPriceShockPct:  0.03,
			GoodsSupplyShockPct: -0.05,
			StockDriftMonthly:   -0.004,
			UnemploymentMonthly: 0.010,
			TreasurySpendCNY:    3000000,
			TreasuryMonthlyCNY:  600000,
			ElectionBias:        -8,
		},
		defaultDuration: 18,
	},
}

// ScenarioCatalog 返回场景目录(契约 §5.1)。每次调用返回独立副本 ——
// Magnitudes/Params 切片可安全被调用方修改。
func ScenarioCatalog() []ScenarioSpec {
	out := make([]ScenarioSpec, 0, len(scenarioDefs))
	for i := range scenarioDefs {
		spec := scenarioDefs[i].spec
		spec.Magnitudes = append([]int(nil), scenarioMagnitudes...)
		spec.Params = append([]string{}, spec.Params...)
		if spec.DefaultMagnitude == 0 {
			spec.DefaultMagnitude = 3
		}
		out = append(out, spec)
	}
	return out
}

// scenarioDefFor 按 kind 查目录(未知 → false)。
func scenarioDefFor(kind string) (*scenarioDef, bool) {
	for i := range scenarioDefs {
		if scenarioDefs[i].spec.Kind == kind {
			return &scenarioDefs[i], true
		}
	}
	return nil, false
}

// ─────────────────────────── 注入 ───────────────────────────

// applyScenarioToWorld 是场景注入的引擎入口(调用方持房间锁)。
//
// 返回:记录 / 即时效果 / 活动态(供房间层写入 World.Scenarios)/ 错误。
// 全部校验在此完成(未知 kind → 35055,参数非法 → 35056,活动上限 → 35057);
// 房间未运行由房间层返回 35002(与 LaunchSurvey 同口径)。
//
// ⚠ 批次71 §4.5:新增 seed 入参(注入记录的 Seed 字段 = 房间层 seed ⇒
// 注入可复现)。调用方(r)同步改。
func applyScenarioToWorld(w *World, kind string, magnitude, durationMonths int, target, note string, at int64, seed int64) (*ScenarioAppliedRecord, []ScenarioEffect, *ScenarioActive, *errcode.Error) {
	if w == nil {
		return nil, nil, nil, errcode.Code(errcode.ErrVirtualCityNotPlaying)
	}
	def, ok := scenarioDefFor(strings.TrimSpace(kind))
	if !ok {
		return nil, nil, nil, errcode.Code(errcode.ErrVirtualCityScenarioUnknown)
	}
	if magnitude == 0 {
		magnitude = def.spec.DefaultMagnitude
	}
	if magnitude < scenarioMinMagnitude || magnitude > scenarioMaxMagnitude {
		return nil, nil, nil, errcode.CodeMsg(errcode.ErrVirtualCityScenarioParamsInvalid,
			fmt.Sprintf("场景强度须为 %d-%d(收到 %d)", scenarioMinMagnitude, scenarioMaxMagnitude, magnitude))
	}
	if durationMonths == 0 {
		durationMonths = def.defaultDuration
	}
	if durationMonths < 1 || durationMonths > scenarioMaxDurationMonths {
		return nil, nil, nil, errcode.CodeMsg(errcode.ErrVirtualCityScenarioParamsInvalid,
			fmt.Sprintf("持续月数须为 1-%d(收到 %d)", scenarioMaxDurationMonths, durationMonths))
	}
	if len(w.Scenarios) >= scenarioMaxActive {
		return nil, nil, nil, errcode.Code(errcode.ErrVirtualCityScenarioActiveLimit)
	}

	target = strings.TrimSpace(target)
	goodsTargets, policyDir, e := scenarioValidateTarget(def.spec.Kind, target)
	if e != nil {
		return nil, nil, nil, e
	}
	note = truncateRunes(strings.TrimSpace(note), scenarioNoteMaxRunes)

	// 批次73 O2:真实场景注入事件(scenario_id = 场景 id;target 座位/实体
	// 进 actors;step 代理口径 inject)。
	actors := []string{}
	if target != "" {
		actors = append(actors, target)
	}
	w.recordEvent(MCStepInject, MCEventScenario, actors, 0, 0, false, kind)

	// 批次68 §预测运行:校验全部通过后、冲击生效**之前**拍 T0 基线
	// (无 active run 开新 run,有则并入;prediction.go)。零 rand。
	w.predictionBeginOrJoin()

	mag := float64(magnitude)
	prof := def.profile
	effects := make([]ScenarioEffect, 0, 8)

	// ① 居民现金损失(按 seat 升序 —— 确定性)。
	if prof.CashDamageFrac != 0 {
		if lost, n := w.scenarioCashDamage(prof.CashDamageFrac, mag); lost > 0 {
			effects = append(effects, ScenarioEffect{
				Metric: "resident_cash", Delta: float64(-lost),
				Text: fmt.Sprintf("居民现金损失 ¥%d(%d 人)", lost, n),
			})
		}
	}

	// ② 国库一次性支出(重建/救市/军费;国库不足按余额封顶,不透支)。
	if prof.TreasurySpendCNY != 0 {
		spent := w.scenarioTreasurySpend(int64(float64(prof.TreasurySpendCNY) * mag))
		if spent > 0 {
			effects = append(effects, ScenarioEffect{
				Metric: "treasury_cash", Delta: float64(-spent),
				Text: fmt.Sprintf("国库灾后支出 ¥%d", spent),
			})
		}
	}

	// ③ 股指冲击。
	if prof.StockShockPct != 0 && w.Market != nil {
		old := w.Market.StockIndex
		next := scenarioClampPrice(old * (1 + prof.StockShockPct*mag))
		w.Market.StockIndex = next
		if d := scenarioPctChange(old, next); d != 0 {
			effects = append(effects, ScenarioEffect{
				Metric: "stock_index", Delta: scenarioRound2(d),
				Text: fmt.Sprintf("股指 %+.2f%%", d),
			})
		}
	}

	// ④ 黄金避险冲击。
	if prof.GoldShockPct != 0 && w.Market != nil {
		old := w.Market.GoldPrice
		next := math.Max(1, old*(1+prof.GoldShockPct*mag))
		w.Market.GoldPrice = next
		if d := scenarioPctChange(old, next); d != 0 {
			effects = append(effects, ScenarioEffect{
				Metric: "gold_price", Delta: scenarioRound2(d),
				Text: fmt.Sprintf("黄金 %+.2f%%", d),
			})
		}
	}

	// ⑤ 八区房价指数冲击。
	if prof.HouseShockPct != 0 && w.Market != nil {
		sum := 0.0
		n := 0
		for _, d := range DistrictDefs {
			old := w.Market.DistrictIdx[d.ID]
			if old <= 0 {
				continue
			}
			next := math.Max(0.05, old*(1+prof.HouseShockPct*mag))
			w.Market.DistrictIdx[d.ID] = next
			sum += scenarioPctChange(old, next)
			n++
		}
		if n > 0 && sum != 0 {
			d := sum / float64(n)
			effects = append(effects, ScenarioEffect{
				Metric: "house_price_index", Delta: scenarioRound2(d),
				Text: fmt.Sprintf("八区房价指数 %+.2f%%", d),
			})
		}
	}

	// ⑥ 商品价格 / 供给冲击(target 为空 = 全品类)。
	if (prof.GoodsPriceShockPct != 0 || prof.GoodsSupplyShockPct != 0) && w.Goods != nil {
		priceSum, supplySum := 0.0, 0.0
		n := 0
		for _, id := range goodsTargets {
			it := w.Goods.Items[id]
			if it == nil {
				continue
			}
			if prof.GoodsPriceShockPct != 0 {
				old := it.PriceIdx
				next := clampF(old*(1+prof.GoodsPriceShockPct*mag), scenarioPriceIdxMin, scenarioPriceIdxMax)
				it.PriceIdx = next
				priceSum += scenarioPctChange(old, next)
			}
			if prof.GoodsSupplyShockPct != 0 {
				old := w.Goods.SupplyIdx[id]
				next := math.Max(5, old*(1+prof.GoodsSupplyShockPct*mag))
				w.Goods.SupplyIdx[id] = next
				supplySum += scenarioPctChange(old, next)
			}
			n++
		}
		if n > 0 && prof.GoodsPriceShockPct != 0 && priceSum != 0 {
			d := priceSum / float64(n)
			effects = append(effects, ScenarioEffect{
				Metric: "goods_price_index", Delta: scenarioRound2(d),
				Text: fmt.Sprintf("商品价格指数 %+.2f%%(%s)", d, scenarioGoodsLabel(goodsTargets)),
			})
		}
		if n > 0 && prof.GoodsSupplyShockPct != 0 && supplySum != 0 {
			d := supplySum / float64(n)
			effects = append(effects, ScenarioEffect{
				Metric: "goods_supply_index", Delta: scenarioRound2(d),
				Text: fmt.Sprintf("商品供给指数 %+.2f%%(%s)", d, scenarioGoodsLabel(goodsTargets)),
			})
		}
	}

	// ⑦ 失业率即时上行。
	if prof.UnemploymentAdd != 0 && w.Labor != nil {
		old := w.Labor.Unemployment
		w.scenarioSetUnemployment(old + prof.UnemploymentAdd*mag)
		if d := (w.Labor.Unemployment - old) * 100; d != 0 {
			effects = append(effects, ScenarioEffect{
				Metric: "unemployment_rate", Delta: scenarioRound2(d),
				Text: fmt.Sprintf("失业率 %+.2f 个百分点", d),
			})
		}
	}

	// ⑧ 政策利率 / 信贷窗口(stance 方向:tight=+1,loose=-1)。
	if w.CB != nil && (prof.PolicyRateDelta != 0 || prof.CreditWindowDelta != 0) {
		if prof.PolicyRateDelta != 0 {
			old := w.CB.PolicyRate
			next := clampF(old+prof.PolicyRateDelta*mag*policyDir, PolicyRateFloor, PolicyRateCap)
			w.CB.PolicyRate = next
			w.CB.RediscountRate = next + RediscountMarkup
			if w.CB.SLFRate < next {
				w.CB.SLFRate = next + SLFCorridorWidth
			}
			if d := (next - old) * 100; d != 0 {
				effects = append(effects, ScenarioEffect{
					Metric: "policy_rate", Delta: scenarioRound2(d),
					Text: fmt.Sprintf("政策利率 %+.2f 个百分点", d),
				})
			}
		}
		if prof.CreditWindowDelta != 0 {
			old := w.CB.CreditWindowFactor
			w.CB.CreditWindowFactor = clampF(old+prof.CreditWindowDelta*mag*policyDir, CreditWindowMin, CreditWindowMax)
			if d := w.CB.CreditWindowFactor - old; d != 0 {
				effects = append(effects, ScenarioEffect{
					Metric: "credit_window_factor", Delta: scenarioRound2(d),
					Text: fmt.Sprintf("信贷窗口乘数 %+.2f", d),
				})
			}
		}
	}

	// ⑨ 选举满意度偏置(线性衰减基数同时落到活动态)。
	biasBase := prof.ElectionBias * mag
	if biasBase != 0 {
		el := w.scenarioEnsureElection()
		old := el.SentimentBias
		el.SentimentBias = clampF(old+biasBase, -scenarioElectionBiasCap, scenarioElectionBiasCap)
		if d := el.SentimentBias - old; d != 0 {
			effects = append(effects, ScenarioEffect{
				Metric: "election_sentiment", Delta: scenarioRound2(d),
				Text: fmt.Sprintf("选举满意度偏置 %+.2f", d),
			})
		}
	}

	// 记录(环形缓冲 cap 20)。
	w.ScenarioSeq++
	// 批次71 §4.5:注入留痕四字段(target / duration / seed / snapshot_id)。
	// SnapshotID = 当前预测 run id(无 active run = 空串)。
	snapshotID := ""
	if run := w.predictionActiveRun(); run != nil {
		snapshotID = run.ID
	}
	rec := &ScenarioAppliedRecord{
		ID:             fmt.Sprintf("SC%d", w.ScenarioSeq),
		Kind:           def.spec.Kind,
		Magnitude:      magnitude,
		AppliedMonth:   w.Month,
		Note:           note,
		At:             at,
		Target:         target,
		DurationMonths: durationMonths,
		Seed:           seed,
		SnapshotID:     snapshotID,
	}
	w.ScenarioApplied = append(w.ScenarioApplied, *rec)
	if len(w.ScenarioApplied) > scenarioRecordCap {
		w.ScenarioApplied = w.ScenarioApplied[len(w.ScenarioApplied)-scenarioRecordCap:]
	}

	active := &ScenarioActive{
		ID:               rec.ID,
		Kind:             def.spec.Kind,
		Group:            def.spec.Group,
		Magnitude:        magnitude,
		TotalMonths:      durationMonths,
		RemainingMonths:  durationMonths,
		AppliedMonth:     w.Month,
		Target:           target,
		Note:             note,
		At:               at,
		electionBiasBase: biasBase,
	}
	w.Scenarios = append(w.Scenarios, active)

	// 批次68 §预测运行:注入记录并入 active run(方案 §3;零 rand)。
	w.predictionRecordInjection(rec, def.spec.Name, def.spec.Group, durationMonths, target)

	w.emitEvent(EventScenario, -1, fmt.Sprintf(
		"场景注入『%s』L%d(持续 %d 个月,目标:%s)%s",
		def.spec.Name, magnitude, durationMonths, scenarioTargetLabel(target), scenarioEventEffectSummary(effects)))
	return rec, effects, active, nil
}

// scenarioValidateTarget 按 kind 校验 target 并返回:
//   - goodsTargets 商品品类列表(goods_price_shock 用;其他 kind 忽略);
//   - policyDir   政策方向(only policy_shock:tight=+1 / loose=-1)。
func scenarioValidateTarget(kind, target string) ([]string, float64, *errcode.Error) {
	switch kind {
	case ScenarioGoodsPriceShock:
		if target == "" {
			return goodsOrder, 0, nil
		}
		for _, id := range goodsOrder {
			if id == target {
				return []string{id}, 0, nil
			}
		}
		return nil, 0, errcode.CodeMsg(errcode.ErrVirtualCityScenarioParamsInvalid,
			fmt.Sprintf("target 须为空或八大类之一(%s)", strings.Join(goodsOrder, "/")))
	case ScenarioPolicyShock:
		switch target {
		case "", "tight":
			return nil, 1, nil
		case "loose":
			return nil, -1, nil
		default:
			return nil, 0, errcode.CodeMsg(errcode.ErrVirtualCityScenarioParamsInvalid,
				"target 须为 tight 或 loose(缺省 tight)")
		}
	default:
		// 其余场景不消费 target:宽松忽略(旧/新客户端互通),但商品供给/
		// 价格冲击(灾害场景)恒作用于全品类。
		return goodsOrder, 0, nil
	}
}

// ─────────────────────────── 月度衰减 ───────────────────────────

// scenarioMonthStep 场景月度衰减(SettleMonth ②.0 劳动力月步之后调用;
// 调用方持房间锁)。
//
// 每个月对每个活动场景施加「剩余/总时长」比例的残余冲击:
//   - 股指漂移加成(叠加在泡沫月步重置后的 Market.DriftExtra 上);
//   - 商品价格指数续涨;
//   - 失业率月度上行;
//   - 国库月度重建/军费支出(按余额封顶);
//   - 选举满意度偏置按基数线性衰减(多场景取和)。
//
// 到期场景移除并播报「影响结束」;零 rand,固定种子模拟零回归。
func (w *World) scenarioMonthStep() {
	if w == nil || len(w.Scenarios) == 0 {
		return
	}
	// 选举偏置按「仍活动」的场景重算(循环内累计,循环后一次落盘):
	// 到期场景自然不再计入,不会残留,也不受多场景互相覆盖影响。
	biasSum := 0.0
	kept := w.Scenarios[:0]
	for _, sc := range w.Scenarios {
		if sc == nil || sc.RemainingMonths <= 0 {
			continue
		}
		def, ok := scenarioDefFor(sc.Kind)
		if !ok {
			continue
		}
		decay := float64(sc.RemainingMonths) / float64(max(1, sc.TotalMonths))
		mag := float64(sc.Magnitude)
		prof := def.profile

		if prof.StockDriftMonthly != 0 && w.Market != nil {
			w.Market.DriftExtra += prof.StockDriftMonthly * mag * decay
		}
		if prof.GoodsMonthlyDrift != 0 && w.Goods != nil {
			for _, id := range scenarioPriceTargets(sc) {
				it := w.Goods.Items[id]
				if it == nil {
					continue
				}
				it.PriceIdx = clampF(it.PriceIdx*(1+prof.GoodsMonthlyDrift*mag*decay),
					scenarioPriceIdxMin, scenarioPriceIdxMax)
			}
		}
		if prof.UnemploymentMonthly != 0 && w.Labor != nil {
			w.scenarioSetUnemployment(w.Labor.Unemployment + prof.UnemploymentMonthly*mag*decay)
		}
		if prof.TreasuryMonthlyCNY != 0 {
			w.scenarioTreasurySpend(int64(float64(prof.TreasuryMonthlyCNY) * mag * decay))
		}
		sc.RemainingMonths--
		if sc.RemainingMonths <= 0 {
			w.emitEvent(EventScenario, -1, fmt.Sprintf(
				"场景『%s』影响结束(共持续 %d 个月)", def.spec.Name, sc.TotalMonths))
			continue
		}
		// 选举偏置只统计「本月结束后仍活动」的场景 —— 到期当步即清零,
		// 避免最后一个月的残值永久留在 Election.SentimentBias。
		if sc.electionBiasBase != 0 {
			biasSum += sc.electionBiasBase * decay
		}
		kept = append(kept, sc)
	}
	w.Scenarios = kept
	if w.Election != nil {
		w.Election.SentimentBias = clampF(biasSum, -scenarioElectionBiasCap, scenarioElectionBiasCap)
	}
}

// ─────────────────────────── 内部工具 ───────────────────────────

// scenarioCashDamage 存活居民现金按比例损失(seat 升序,确定性)。
func (w *World) scenarioCashDamage(frac, mag float64) (int64, int) {
	var total int64
	n := 0
	for seat := 0; seat < MaxSeats; seat++ {
		p := w.Players[seat]
		if p == nil || !p.Alive || p.Cash <= 0 {
			continue
		}
		dmg := int64(float64(p.Cash) * frac * mag)
		if dmg <= 0 {
			continue
		}
		if dmg > p.Cash {
			dmg = p.Cash
		}
		p.Cash -= dmg
		// 批次71 §3.3 登记点 ③:场景冲击对居民现金的直接损失是**真账外**
		// (不经 Ledger)⇒ 必须登记,否则 I1 守恒立刻红。
		w.offLedgerRecord(w.Month, seat, OffLedgerScenarioDamage, -dmg, "场景冲击现金损失")
		total += dmg
		n++
	}
	return total, n
}

// scenarioTreasurySpend 国库支出(账内核算,不经 Ledger —— 与财政采购/
// 国债还本付息同口径,见 treasury.go 头注释)。不足时按余额封顶,不透支。
// 返回实际支出额(0 = 国库无余额)。
func (w *World) scenarioTreasurySpend(amount int64) int64 {
	if amount <= 0 || w.Treasury == nil || w.Treasury.Cash <= 0 {
		return 0
	}
	if amount > w.Treasury.Cash {
		amount = w.Treasury.Cash
	}
	w.Treasury.Cash -= amount
	// 批次71 §4.3:国库账外现金流出登记(供 I3 闭合恒等式)。
	// ⚠ 月结路径(②.0B)发生在国库月结快照之前、注入路径发生在月结之间
	// ⇒ 本项永不落在 [快照, 审计] 窗口内;登记只为「声明全部国库现金
	// 流出」完整性(若将来窗口口径变化,登记即生效)。
	w.Treasury.OtherExpense += amount
	return amount
}

// scenarioEnsureElection 惰性初始化选举状态(与 settlement ⑨G 同口径)。
func (w *World) scenarioEnsureElection() *CivicElection {
	if w.Election == nil {
		w.Election = NewCivicElection()
	}
	return w.Election
}

// scenarioSetUnemployment 写入失业率并同步就业率/裁员潮档位。
func (w *World) scenarioSetUnemployment(u float64) {
	if w.Labor == nil {
		return
	}
	w.Labor.Unemployment = clampF(u, UnemployFloor, UnemployCeil)
	w.Labor.Employment = 1 - w.Labor.Unemployment
	switch {
	case w.Labor.Unemployment > 0.12:
		w.Labor.LayoffWave = 2
	case w.Labor.Unemployment > 0.08:
		w.Labor.LayoffWave = 1
	default:
		w.Labor.LayoffWave = 0
	}
}

// scenarioPriceTargets 月度续涨作用范围 = goods_price_shock 的 target
// (空 = 全品类);其他场景恒全品类。
func scenarioPriceTargets(sc *ScenarioActive) []string {
	if sc == nil {
		return goodsOrder
	}
	if sc.Kind == ScenarioGoodsPriceShock && sc.Target != "" {
		if items, _, e := scenarioValidateTarget(sc.Kind, sc.Target); e == nil {
			return items
		}
	}
	return goodsOrder
}

// scenarioClampPrice 价格类字段安全区间(下限 0.01 与 Market.MonthStep 一致)。
func scenarioClampPrice(v float64) float64 {
	if v < 0.01 {
		return 0.01
	}
	return v
}

// scenarioPctChange 百分比变化(%,old=0 → 0)。
func scenarioPctChange(old, next float64) float64 {
	if old == 0 {
		return 0
	}
	return (next/old - 1) * 100
}

// scenarioRound2 保留两位小数(效果 delta 展示口径)。
func scenarioRound2(v float64) float64 {
	return math.Round(v*100) / 100
}

// scenarioGoodsLabel target 品类中文标签(全品类 → 「全品类」)。
func scenarioGoodsLabel(items []string) string {
	if len(items) == len(goodsOrder) {
		return "全品类"
	}
	labels := make([]string, 0, len(items))
	for _, id := range items {
		if cn, ok := goodsCN[id]; ok {
			labels = append(labels, cn)
			continue
		}
		labels = append(labels, id)
	}
	return strings.Join(labels, "/")
}

// scenarioTargetLabel 事件流里的目标标签。
func scenarioTargetLabel(target string) string {
	if target == "" {
		return "全城"
	}
	if cn, ok := goodsCN[target]; ok {
		return cn
	}
	return target
}

// scenarioEventEffectSummary 事件流效果摘要(取前 3 条,避免日志过长)。
func scenarioEventEffectSummary(effects []ScenarioEffect) string {
	if len(effects) == 0 {
		return ""
	}
	parts := make([]string, 0, 3)
	for i, e := range effects {
		if i >= 3 {
			break
		}
		parts = append(parts, e.Text)
	}
	return " — " + strings.Join(parts, "；")
}
