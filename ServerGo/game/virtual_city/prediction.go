// Package virtual_city — prediction.go: 预测运行引擎(批次68,2026-10-21 §预测运行)。
//
// 闭环《虚拟城市-预测运行报告》§2-§4 五步流程中的 ①基线快照 / ④指标对比 /
// ⑤预测报告(②条件注入与 ③逐月演化已由 scenario.go 落地):
//
//	applyScenarioToWorld 注入时(冲击生效之前)拍 T0 基线 → SettleMonth 逐月
//	采样指标点 → BuildPredictionReport 纯函数出结构化预测报告(R1 观测对照)。
//
// 纪律:
//   - **零 rand**:所有指标纯算术取自既有状态字段(方案 §2 表),不消费
//     w.Rand、不引入平行账本(固定种子存量模拟回归零偏移,与 scenario.go
//     同款契约);
//   - **锁纪律(§92a)**:engine 级函数(predictionBeginOrJoin /
//     predictionRecordInjection / predictionMonthStep)全部由房间层持 r.mu
//     调用,不回调房间层;BuildPredictionReport 为纯函数,任意层可调;
//   - **nil 安全**:w.CB / w.Labor / w.Treasury / w.Election / w.Society /
//     w.SocietyHist 均可能为 nil,取数处按方案 §2 nil 回退值落 0/0.05;
//   - **复现层级 R1**(方案 §1):基线 = 注入时点(T0)观测快照(平直基线),
//     对比 = 实际轨迹 − 基线;R0 同种子孪生本批次不做,报告边界章节显式声明。
package virtual_city

import (
	"fmt"
	"hash/fnv"
	"math"
	"strings"
)

// 预测运行常量(方案 §3)。
const (
	// predictionHorizonMonths 预测运行观察窗:active 定义 =
	// w.Month − StartMonth < 24;超出后 run 结束,新注入开新 run。
	predictionHorizonMonths = 24
	// predictionTrajectoryCap 轨迹环形容量(逐月一个点,T0 也记一点;
	// 超出滚动丢最旧)。
	predictionTrajectoryCap = 36
	// predictionRunsCap 环形 run 容量(保留最近 3 个;新 run 挤出最旧)。
	predictionRunsCap = 3
	// PredictionTierR1 本批次复现层级标记(报告 meta.tier / boundaries)。
	PredictionTierR1 = "R1"
	// predictionRecoveryBandPct 峰值恢复判定带宽(方案 §4 规则3:偏离回落到
	// 基线 ±5% 且其后所有月保持在带内 → 视为恢复)。
	predictionRecoveryBandPct = 0.05
)

// ─────────────────────── 指标口径(方案 §2) ───────────────────────

// PredictionIndicators 单月指标点(方案 §2 表;全部既有字段取数,零平行账本)。
type PredictionIndicators struct {
	Month           int     `json:"month"`             // 主钟月份
	GoodsPriceIdx   float64 `json:"goods_price_idx"`   // 消费篮子价格(goodsWeights 加权均值)
	CPIAnn          float64 `json:"cpi_ann"`           // 内生通胀年化小数(CB nil → 0)
	Unemployment    float64 `json:"unemployment"`      // 失业率(Labor nil → 0.05)
	StockIndex      float64 `json:"stock_index"`       // 股指
	GoldPrice       float64 `json:"gold_price"`        // 黄金
	HouseIdx        float64 `json:"house_idx"`         // 房价指数(DistrictIdx 全区均值)
	TreasuryCash    int64   `json:"treasury_cash"`     // 国库现金(元;Treasury nil → 0)
	AvgResidentCash int64   `json:"avg_resident_cash"` // 存活玩家现金均值(元)
	GiniWealth      float64 `json:"gini_wealth"`       // 财富基尼(SocietySnapshot 最新)
	GiniIncome      float64 `json:"gini_income"`       // 收入基尼
	Top10Pct        float64 `json:"top10_pct"`         // 前 10% 财富占比
	Bottom50Pct     float64 `json:"bottom50_pct"`      // 后 50% 财富占比
	Pyramid         [4]int  `json:"pyramid"`           // 财富金字塔 4 层人数(分布影响用)
	Satisfaction    float64 `json:"satisfaction"`      // 社会满意度 0..100(Election nil → 0)
	BubbleLeverage  float64 `json:"bubble_leverage"`   // 市场杠杆率
}

// collectIndicators 纯算术取数(方案 §2;零 rand,零平行账本)。
// 调用方持房间锁(engine 级)。
func collectIndicators(w *World) PredictionIndicators {
	if w == nil {
		return PredictionIndicators{Unemployment: 0.05}
	}
	ind := PredictionIndicators{Month: w.Month, Unemployment: 0.05}
	// 消费篮子价格 = goodsWeights 加权均值(方案 §2)。
	if w.Goods != nil {
		sum, wsum := 0.0, 0.0
		for _, id := range goodsOrder {
			it := w.Goods.Items[id]
			if it == nil {
				continue
			}
			wt := goodsWeights[id]
			sum += it.PriceIdx * wt
			wsum += wt
		}
		if wsum > 0 {
			ind.GoodsPriceIdx = sum / wsum
		}
	}
	if w.CB != nil {
		ind.CPIAnn = w.CB.CPI
	}
	if w.Labor != nil {
		ind.Unemployment = w.Labor.Unemployment
	}
	if w.Market != nil {
		ind.StockIndex = w.Market.StockIndex
		ind.GoldPrice = w.Market.GoldPrice
		// 房价指数 = DistrictIdx 全区均值(方案 §2 「八区均值」的现行口径 ——
		// 城区表已扩到 32 区,与 scenario.go 房价冲击同口径遍历 DistrictDefs)。
		sum, n := 0.0, 0
		for _, d := range DistrictDefs {
			v := w.Market.DistrictIdx[d.ID]
			if v <= 0 {
				continue
			}
			sum += v
			n++
		}
		if n > 0 {
			ind.HouseIdx = sum / float64(n)
		}
	}
	if w.Treasury != nil {
		ind.TreasuryCash = w.Treasury.Cash
	}
	// 存活玩家现金均值(空座/死亡不计;整数元,方案 §4 金额口径)。
	cashSum, alive := int64(0), 0
	for _, p := range w.Players {
		if p == nil || !p.Alive {
			continue
		}
		cashSum += p.Cash
		alive++
	}
	if alive > 0 {
		ind.AvgResidentCash = cashSum / int64(alive)
	}
	// 分布指标 = SocietySnapshot 最新(SocietyHist.Latest nil 接收者安全)。
	if snap := w.SocietyHist.Latest(); snap != nil {
		ind.GiniWealth = snap.GiniWealth
		ind.GiniIncome = snap.GiniIncome
		ind.Top10Pct = snap.Top10Pct
		ind.Bottom50Pct = snap.Bottom50Pct
		ind.Pyramid = snap.Pyramid
	}
	// 社会满意度:Election nil → 0(方案 §2 nil 回退);非 nil 时复用
	// civic_election.ComputeVotes 的全城同值公式(100 − 基尼×100 + 舆情偏置),
	// 取自既有字段、不新增存储。
	if w.Election != nil {
		gini := 0.0
		if w.Society != nil {
			gini = clamp01(w.Society.Gini)
		}
		ind.Satisfaction = clampF(100-gini*100+w.Election.SentimentBias, 0, 100)
	}
	ind.BubbleLeverage = w.BubbleLeverage
	return ind
}

// ─────────────────────── 运行生命周期(方案 §3) ───────────────────────

// PredictionInjection run 内注入记录(报告 meta.injections / scenarios 复用;
// ScenarioAppliedRecord 子集 + 报告展示所需 name/group/duration/target)。
type PredictionInjection struct {
	ID             string `json:"id"`              // "SC1"…
	Kind           string `json:"kind"`            // 场景 kind
	Name           string `json:"name"`            // 目录中文名
	Group          string `json:"group"`           // 目录分组
	Magnitude      int    `json:"magnitude"`       // 强度 L1-L5
	DurationMonths int    `json:"duration_months"` // 持续月数
	Target         string `json:"target"`          // 目标参数
	Note           string `json:"note"`            // 条件触发者备注
	AppliedMonth   int    `json:"applied_month"`   // 注入月
}

// PredictionRun 单次预测运行(基线 + 逐月轨迹 + 注入集)。
type PredictionRun struct {
	ID         string                 `json:"id"`          // "PR"+seq(World.PredictionSeq 自增)
	StartMonth int                    `json:"start_month"` // T0 主钟月份
	Baseline   PredictionIndicators   `json:"baseline"`    // T0 基线(冲击生效之前)
	Trajectory []PredictionIndicators `json:"trajectory"`  // 逐月指标点(cap 36;[0]=T0)
	Injections []PredictionInjection  `json:"injections"`  // 并入的注入记录(旧→新)
}

// PredictionState 预测运行状态(挂 World.Prediction;惰性初始化)。
type PredictionState struct {
	Seq  int              // run id 自增序列(与 ScenarioSeq 同款)
	Runs []*PredictionRun // 最近 3 个 run(旧→新;环形挤出)
}

// PredictionRunSummary GET .../prediction 的 runs 元素(方案 §5)。
type PredictionRunSummary struct {
	ID             string   `json:"id"`
	StartMonth     int      `json:"start_month"`
	MonthsObserved int      `json:"months_observed"`
	Tier           string   `json:"tier"`
	Injections     []string `json:"injections"` // ["SC1","SC2"]
}

// predictionEnsure 惰性初始化(零值安全)。
func (w *World) predictionEnsure() *PredictionState {
	if w.Prediction == nil {
		w.Prediction = &PredictionState{}
	}
	return w.Prediction
}

// predictionActiveRun 当前 active run(nil = 无 run / 已超出 horizon)。
// active 定义(方案 §3):w.Month − StartMonth < predictionHorizonMonths。
func (w *World) predictionActiveRun() *PredictionRun {
	if w == nil || w.Prediction == nil || len(w.Prediction.Runs) == 0 {
		return nil
	}
	run := w.Prediction.Runs[len(w.Prediction.Runs)-1]
	if run == nil || w.Month-run.StartMonth >= predictionHorizonMonths {
		return nil
	}
	return run
}

// predictionBeginOrJoin 注入入口(applyScenarioToWorld 校验通过后、冲击生效
// **之前**调用;调用方持房间锁):无 active run → 先拍 T0 基线开新 run
// ("PR"+seq);有 → 并入当前 run。零 rand。
func (w *World) predictionBeginOrJoin() *PredictionRun {
	if w == nil {
		return nil
	}
	if run := w.predictionActiveRun(); run != nil {
		return run
	}
	ps := w.predictionEnsure()
	ps.Seq++
	base := collectIndicators(w)
	run := &PredictionRun{
		ID:         fmt.Sprintf("PR%d", ps.Seq),
		StartMonth: w.Month,
		Baseline:   base,
		Trajectory: []PredictionIndicators{base}, // 方案 §2:T0 也记一点
	}
	ps.Runs = append(ps.Runs, run)
	if len(ps.Runs) > predictionRunsCap {
		ps.Runs = ps.Runs[len(ps.Runs)-predictionRunsCap:]
	}
	return run
}

// predictionRecordInjection 注入记录并入 active run(applyScenarioToWorld
// 收尾;调用方持房间锁)。方案 §3:horizon 期间新注入并入当前 run。
func (w *World) predictionRecordInjection(rec *ScenarioAppliedRecord, name, group string, durationMonths int, target string) {
	run := w.predictionActiveRun()
	if run == nil || rec == nil {
		return
	}
	run.Injections = append(run.Injections, PredictionInjection{
		ID:             rec.ID,
		Kind:           rec.Kind,
		Name:           name,
		Group:          group,
		Magnitude:      rec.Magnitude,
		DurationMonths: durationMonths,
		Target:         target,
		Note:           rec.Note,
		AppliedMonth:   rec.AppliedMonth,
	})
}

// predictionMonthStep 逐月采样(SettleMonth 内、scenarioMonthStep 与
// SocietyHist.Push 之后调用;调用方持房间锁):向 active run 追加当月指标点
// (cap 36,超出滚动丢最旧);无 active run → no-op。零 rand。
func (w *World) predictionMonthStep() {
	run := w.predictionActiveRun()
	if run == nil {
		return
	}
	run.Trajectory = append(run.Trajectory, collectIndicators(w))
	if len(run.Trajectory) > predictionTrajectoryCap {
		run.Trajectory = run.Trajectory[len(run.Trajectory)-predictionTrajectoryCap:]
	}
}

// ─────────────────────── 指标注册表(报告展示) ───────────────────────

// predictionMetricDef 报告指标定义(key 即方案 §2 字段名,unit/text 展示用)。
type predictionMetricDef struct {
	Key   string
	Unit  string // ratio | index | cny | score
	Label string
	Pick  func(PredictionIndicators) float64
}

// predictionMetrics 报告全指标表(顺序 = 展示顺序)。
var predictionMetrics = []predictionMetricDef{
	{"goods_price_idx", "index", "消费篮子价格", func(v PredictionIndicators) float64 { return v.GoodsPriceIdx }},
	{"cpi_ann", "ratio", "通胀(年化)", func(v PredictionIndicators) float64 { return v.CPIAnn }},
	{"unemployment", "ratio", "失业率", func(v PredictionIndicators) float64 { return v.Unemployment }},
	{"stock_index", "index", "股指", func(v PredictionIndicators) float64 { return v.StockIndex }},
	{"gold_price", "index", "黄金", func(v PredictionIndicators) float64 { return v.GoldPrice }},
	{"house_idx", "index", "房价指数", func(v PredictionIndicators) float64 { return v.HouseIdx }},
	{"treasury_cash", "cny", "国库现金", func(v PredictionIndicators) float64 { return float64(v.TreasuryCash) }},
	{"avg_resident_cash", "cny", "居民现金均值", func(v PredictionIndicators) float64 { return float64(v.AvgResidentCash) }},
	{"gini_wealth", "ratio", "财富基尼", func(v PredictionIndicators) float64 { return v.GiniWealth }},
	{"gini_income", "ratio", "收入基尼", func(v PredictionIndicators) float64 { return v.GiniIncome }},
	{"top10_pct", "ratio", "前10%财富占比", func(v PredictionIndicators) float64 { return v.Top10Pct }},
	{"bottom50_pct", "ratio", "后50%财富占比", func(v PredictionIndicators) float64 { return v.Bottom50Pct }},
	{"satisfaction", "score", "社会满意度", func(v PredictionIndicators) float64 { return v.Satisfaction }},
	{"bubble_leverage", "ratio", "杠杆率", func(v PredictionIndicators) float64 { return v.BubbleLeverage }},
}

// predictionMetricFor 按 key 查指标定义(未命中 → nil)。
func predictionMetricFor(key string) *predictionMetricDef {
	for i := range predictionMetrics {
		if predictionMetrics[i].Key == key {
			return &predictionMetrics[i]
		}
	}
	return nil
}

// predictionMetricByKey 内部快速取值(未命中 → 0)。
func predictionMetricByKey(v PredictionIndicators, key string) float64 {
	if def := predictionMetricFor(key); def != nil {
		return def.Pick(v)
	}
	return 0
}

// predictionDevPct 相对基线偏离百分比(基线 0 → 0;方案 §4 delta_pct 口径)。
func predictionDevPct(base, value float64) float64 {
	if base == 0 {
		return 0
	}
	return (value/base - 1) * 100
}

// predictionRound6 数值保留 6 位小数(方案 §4 展示口径)。
func predictionRound6(v float64) float64 {
	return math.Round(v*1e6) / 1e6
}

// predictionBaselineID 基线快照 id = fnv64(基线点字段序列化)
// (方案 §4 规则1;确定性,无 rand)。形如 "b-<hex16>"。
func predictionBaselineID(b PredictionIndicators) string {
	h := fnv.New64a()
	fmt.Fprintf(h, "%d|%.6f|%.6f|%.6f|%.6f|%.6f|%.6f|%d|%d|%.6f|%.6f|%.6f|%.6f|%d|%d|%d|%d|%.6f|%.6f",
		b.Month, b.GoodsPriceIdx, b.CPIAnn, b.Unemployment, b.StockIndex, b.GoldPrice,
		b.HouseIdx, b.TreasuryCash, b.AvgResidentCash, b.GiniWealth, b.GiniIncome,
		b.Top10Pct, b.Bottom50Pct, b.Pyramid[0], b.Pyramid[1], b.Pyramid[2], b.Pyramid[3],
		b.Satisfaction, b.BubbleLeverage)
	return fmt.Sprintf("b-%016x", h.Sum64())
}

// ─────────────────────── 预测报告(方案 §4 契约) ───────────────────────
//
// 全部 JSON snake_case;数值小数保留 6 位内,金额整数元。
// BuildPredictionReport 为纯函数(只读 run + 入参 seed/generatedAt),
// 确定性生成,任意层可调用(房间层持锁调用亦安全)。

// PredictionReportMeta 报告元数据(方案 §4 meta)。
type PredictionReportMeta struct {
	RunID              string                `json:"run_id"`
	Tier               string                `json:"tier"`
	BaselineSnapshotID string                `json:"baseline_snapshot_id"`
	Seed               int64                 `json:"seed"`
	StartMonth         int                   `json:"start_month"`
	MonthsObserved     int                   `json:"months_observed"`
	GeneratedAt        int64                 `json:"generated_at"`
	Injections         []PredictionInjection `json:"injections"`
}

// PredictionScenarioSection 场景描述章节(scenarios 元素)。
type PredictionScenarioSection struct {
	Kind           string `json:"kind"`
	Name           string `json:"name"`
	Group          string `json:"group"`
	Magnitude      int    `json:"magnitude"`
	DurationMonths int    `json:"duration_months"`
	Target         string `json:"target"`
	Note           string `json:"note"`
	AppliedMonth   int    `json:"applied_month"`
	Decay          string `json:"decay"`
}

// PredictionImpactSection 首月直接冲击元素(T0+1 点 vs 基线)。
type PredictionImpactSection struct {
	Metric   string  `json:"metric"`
	Baseline float64 `json:"baseline"`
	Value    float64 `json:"value"`
	DeltaPct float64 `json:"delta_pct"`
	Unit     string  `json:"unit"`
	Text     string  `json:"text"`
}

// PredictionTrajectoryPoint 逐月轨迹点(baseline 恒为 T0 值,前端可画双线)。
type PredictionTrajectoryPoint struct {
	Month    int                `json:"month"`
	Values   map[string]float64 `json:"values"`
	Baseline map[string]float64 `json:"baseline"`
}

// PredictionPeakSection 峰值与恢复元素(方案 §4 规则3)。
type PredictionPeakSection struct {
	Metric        string  `json:"metric"`
	PeakDevPct    float64 `json:"peak_dev_pct"`
	PeakMonth     int     `json:"peak_month"`
	TroughDevPct  float64 `json:"trough_dev_pct"`
	TroughMonth   int     `json:"trough_month"`
	RecoveryMonth *int    `json:"recovery_month"` // 未恢复 → null
	Recovered     bool    `json:"recovered"`
}

// PredictionTransmissionChain 传导归因链路(方案 §4 规则5)。
type PredictionTransmissionChain struct {
	Chain        string  `json:"chain"`
	Contribution float64 `json:"contribution"`
	Dominant     bool    `json:"dominant"`
	Evidence     string  `json:"evidence"`
}

// PredictionTransmission 传导归因章节。
type PredictionTransmission struct {
	Chains []PredictionTransmissionChain `json:"chains"`
	Note   string                        `json:"note"`
}

// PredictionDistribution 分布影响章节。
type PredictionDistribution struct {
	GiniWealthDelta  float64 `json:"gini_wealth_delta"`
	Top10DeltaPct    float64 `json:"top10_delta_pct"`
	Bottom50DeltaPct float64 `json:"bottom50_delta_pct"`
	PyramidShift     [4]int  `json:"pyramid_shift"`
	Text             string  `json:"text"`
}

// PredictionExtrapolation 外推 3/6/12 月元素(方案 §4 规则4)。
type PredictionExtrapolation struct {
	HorizonMonths int     `json:"horizon_months"`
	Metric        string  `json:"metric"`
	Point         float64 `json:"point"`
	Lo            float64 `json:"lo"`
	Hi            float64 `json:"hi"`
}

// PredictionResilience 韧性画像(仅灾害/未知灾难;方案 §4 规则6)。
type PredictionResilience struct {
	Applicable      bool     `json:"applicable"`
	RecoveryScore   int      `json:"recovery_score"`
	AdaptationScore int      `json:"adaptation_score"`
	TransformScore  int      `json:"transform_score"`
	Basis           []string `json:"basis"`
}

// PredictionReport 预测报告顶层(方案 §4;前后端共同遵守的契约)。
type PredictionReport struct {
	Meta             PredictionReportMeta        `json:"meta"`
	Scenarios        []PredictionScenarioSection `json:"scenarios"`
	FirstMonthImpact []PredictionImpactSection   `json:"first_month_impact"`
	Trajectory       []PredictionTrajectoryPoint `json:"trajectory"`
	PeakRecovery     []PredictionPeakSection     `json:"peak_recovery"`
	Transmission     PredictionTransmission      `json:"transmission"`
	Distribution     PredictionDistribution      `json:"distribution"`
	Extrapolation    []PredictionExtrapolation   `json:"extrapolation"`
	Resilience       PredictionResilience        `json:"resilience"`
	Boundaries       []string                    `json:"boundaries"`
}

// predictionChainDef 场景 → 传导链路静态表(设计文档 §4.2/§4.4;
// 代表指标用于贡献度归一)。
type predictionChainDef struct {
	Chain     string // 链路文本(①价格→②消费→…)
	RepMetric string // 代表指标 key(贡献度 = |dev_pct| 归一)
}

var predictionChainTable = map[string]predictionChainDef{
	ScenarioGoodsPriceShock: {"①价格→②消费→③就业→⑤信用→⑥资产→⑦社会", "goods_price_idx"},
	ScenarioPolicyShock:     {"④财政/⑤信用→②消费→③就业→⑤信用→⑥资产", "cpi_ann"},
	ScenarioMarketCrash:     {"⑥资产→⑤信用→②消费→③就业→④财政→⑦社会", "stock_index"},
	ScenarioSurveyPulse:     {"⑦社会(观测)→②消费→⑥资产", "stock_index"},
	ScenarioElectionMood:    {"⑦社会→④财政→①价格/⑤信用", "satisfaction"},
	ScenarioEarthquake:      {"⑥资产→③就业→④财政→⑤信用→⑦社会", "house_idx"},
	ScenarioTsunami:         {"⑥资产→③就业→④财政→⑤信用→⑦社会", "house_idx"},
	ScenarioExtremeDisaster: {"①价格→②消费→③就业→④财政→⑤信用→⑥资产→⑦社会(全链路)", "goods_price_idx"},
	ScenarioMeteorite:       {"⑥存量→①价格→②消费→③就业→④财政→⑦社会", "house_idx"},
	ScenarioAlienInvasion:   {"⑦信心→⑥资产→⑤信用→②消费→③就业→④财政", "stock_index"},
}

// predictionBoundaryR1 报告边界与限制(方案 §1/§4:必须显式声明 R1)。
var predictionBoundaryR1 = []string{
	"R1 观测对照:基线为注入时点观测值,非同种子反事实孪生(R0 未实现)",
	"含 LLM 居民决策噪声,复现层级 R1",
	"未知域压力测试(meteorite/alien_invasion):不承诺预测精度",
	"外推基于最近 3 月偏离斜率/几何半衰期拟合,置信带随视野线性放宽",
}

// BuildPredictionReport 由 run 纯函数生成预测报告(方案 §4 生成规则,全确定性)。
func BuildPredictionReport(run *PredictionRun, seed int64, generatedAt int64) *PredictionReport {
	if run == nil {
		return nil
	}
	monthsObserved := len(run.Trajectory) - 1
	if monthsObserved < 0 {
		monthsObserved = 0
	}
	injections := run.Injections
	if injections == nil {
		injections = []PredictionInjection{}
	}
	rep := &PredictionReport{
		Meta: PredictionReportMeta{
			RunID:              run.ID,
			Tier:               PredictionTierR1,
			BaselineSnapshotID: predictionBaselineID(run.Baseline),
			Seed:               seed,
			StartMonth:         run.StartMonth,
			MonthsObserved:     monthsObserved,
			GeneratedAt:        generatedAt,
			Injections:         injections,
		},
		Scenarios:        []PredictionScenarioSection{},
		FirstMonthImpact: []PredictionImpactSection{},
		Trajectory:       []PredictionTrajectoryPoint{},
		PeakRecovery:     []PredictionPeakSection{},
		Extrapolation:    []PredictionExtrapolation{},
		Boundaries:       predictionBoundaryR1,
		Transmission: PredictionTransmission{
			Chains: []PredictionTransmissionChain{},
			Note:   "贡献度按各链路代表指标的 |偏离| 归一启发式估计,非计量因果分解",
		},
		Resilience: PredictionResilience{Basis: []string{}},
	}

	// scenarios:注入记录 → 展示章节(衰减文案固定 —— scenario.go 月度按
	// remaining/total 线性衰减)。
	groups := map[string]bool{}
	for _, inj := range injections {
		rep.Scenarios = append(rep.Scenarios, PredictionScenarioSection{
			Kind: inj.Kind, Name: inj.Name, Group: inj.Group,
			Magnitude: inj.Magnitude, DurationMonths: inj.DurationMonths,
			Target: inj.Target, Note: inj.Note, AppliedMonth: inj.AppliedMonth,
			Decay: "线性衰减(remaining/total)",
		})
		groups[inj.Group] = true
	}

	// trajectory:全量逐月点(≤36 月;values 为当月点,baseline 恒 T0 值)。
	baseMap := map[string]float64{}
	for _, m := range predictionMetrics {
		baseMap[m.Key] = predictionRound6(m.Pick(run.Baseline))
	}
	for _, pt := range run.Trajectory {
		vals := map[string]float64{}
		for _, m := range predictionMetrics {
			vals[m.Key] = predictionRound6(m.Pick(pt))
		}
		rep.Trajectory = append(rep.Trajectory, PredictionTrajectoryPoint{
			Month: pt.Month, Values: vals, Baseline: baseMap,
		})
	}

	// first_month_impact:T0+1 点 vs 基线(方案 §4 规则2;T0 即基线 ⇒ 只报
	// T0+1。仅收录有偏离的指标,零漂移指标不占版面)。
	if len(run.Trajectory) >= 2 {
		first := run.Trajectory[1]
		for _, m := range predictionMetrics {
			b, v := m.Pick(run.Baseline), m.Pick(first)
			if b == v {
				continue
			}
			dp := predictionDevPct(b, v)
			rep.FirstMonthImpact = append(rep.FirstMonthImpact, PredictionImpactSection{
				Metric: m.Key, Baseline: predictionRound6(b), Value: predictionRound6(v),
				DeltaPct: predictionRound6(dp), Unit: m.Unit,
				Text: fmt.Sprintf("%s %+.1f%%", m.Label, dp),
			})
		}
	}

	// peak_recovery:逐指标找 |dev| 峰值月与反向谷值月;恢复 = 偏离回落到
	// 基线 ±5% 且其后所有月保持在带内(方案 §4 规则3)。
	peakByKey := map[string]PredictionPeakSection{}
	for _, m := range predictionMetrics {
		peak := predictionPeakFor(run, m)
		peakByKey[m.Key] = peak
		if peak.PeakDevPct != 0 || peak.TroughDevPct != 0 {
			rep.PeakRecovery = append(rep.PeakRecovery, peak)
		}
	}

	// transmission:静态表 kind→链路组,contribution = 代表指标 |dev_pct| 归一
	// (方案 §4 规则5);dominant = 最大者。
	rep.Transmission.Chains = predictionTransmissionChains(run, peakByKey)

	// distribution:分布影响(基尼/头部/底部/金字塔位移)。
	rep.Distribution = predictionDistribution(run)

	// extrapolation:3/6/12 月 × 核心 4 指标(方案 §4 规则4)。
	for _, h := range []int{3, 6, 12} {
		for _, key := range []string{"unemployment", "goods_price_idx", "stock_index", "gini_wealth"} {
			point, lo, hi := predictionExtrapolate(run, key, h)
			rep.Extrapolation = append(rep.Extrapolation, PredictionExtrapolation{
				HorizonMonths: h, Metric: key,
				Point: predictionRound6(point),
				Lo:    predictionRound6(lo),
				Hi:    predictionRound6(hi),
			})
		}
	}

	// resilience:仅灾害/未知灾难(方案 §4 规则6)。
	rep.Resilience = predictionResilience(run, peakByKey, groups)
	return rep
}

// predictionPeakFor 单指标的峰值/谷值/恢复月(方案 §4 规则3)。
func predictionPeakFor(run *PredictionRun, m predictionMetricDef) PredictionPeakSection {
	base := m.Pick(run.Baseline)
	peak := PredictionPeakSection{Metric: m.Key, Recovered: true}
	// 峰值 = |dev| 最大的观测点(并列取首现;T0 dev 恒 0)。
	peakDev, peakMonth := 0.0, run.StartMonth
	for _, pt := range run.Trajectory {
		dev := predictionDevPct(base, m.Pick(pt))
		if math.Abs(dev) > math.Abs(peakDev) {
			peakDev, peakMonth = dev, pt.Month
		}
	}
	// 谷值 = 与峰值反向的极值(峰值非负 → 最小偏离;峰值为负 → 最大偏离)。
	troughDev, troughMonth := 0.0, run.StartMonth
	first := true
	for _, pt := range run.Trajectory {
		dev := predictionDevPct(base, m.Pick(pt))
		if first {
			troughDev, troughMonth = dev, pt.Month
			first = false
			continue
		}
		if peakDev >= 0 {
			if dev < troughDev {
				troughDev, troughMonth = dev, pt.Month
			}
		} else if dev > troughDev {
			troughDev, troughMonth = dev, pt.Month
		}
	}
	peak.PeakDevPct = predictionRound6(peakDev)
	peak.PeakMonth = peakMonth
	peak.TroughDevPct = predictionRound6(troughDev)
	peak.TroughMonth = troughMonth

	// recovery_month:最后一个「带外」点之后、首个持续带内的月份;始终带内 →
	// T0 后首点月;末点仍带外 → 未恢复(null)。
	band := math.Abs(base) * predictionRecoveryBandPct
	if base == 0 {
		band = 0.005 // 基线 0 的绝对兜底带(百分比无定义)
	}
	lastOut := -1
	for i, pt := range run.Trajectory {
		if math.Abs(m.Pick(pt)-base) > band {
			lastOut = i
		}
	}
	if lastOut < len(run.Trajectory)-1 {
		recMonth := run.Trajectory[lastOut+1].Month
		peak.Recovered = true
		peak.RecoveryMonth = &recMonth
	} else {
		peak.Recovered = false
		peak.RecoveryMonth = nil
	}
	return peak
}

// predictionTransmissionChains 传导归因链路(贡献度归一,dominant 唯一)。
func predictionTransmissionChains(run *PredictionRun, peakByKey map[string]PredictionPeakSection) []PredictionTransmissionChain {
	// 按注入出现序去重 kind(确定性)。
	type chainRow struct {
		def     predictionChainDef
		weight  float64
		peakDev float64
		label   string
	}
	var rows []chainRow
	seen := map[string]bool{}
	for _, inj := range run.Injections {
		if seen[inj.Kind] {
			continue
		}
		seen[inj.Kind] = true
		def, ok := predictionChainTable[inj.Kind]
		if !ok {
			continue
		}
		peak := peakByKey[def.RepMetric]
		w := math.Abs(peak.PeakDevPct)
		label := "指标"
		if md := predictionMetricFor(def.RepMetric); md != nil {
			label = md.Label
		}
		rows = append(rows, chainRow{def: def, weight: w, peakDev: peak.PeakDevPct, label: label})
	}
	total := 0.0
	for _, r := range rows {
		total += r.weight
	}
	chains := make([]PredictionTransmissionChain, 0, len(rows))
	domIdx := 0
	for i, r := range rows {
		if r.weight > rows[domIdx].weight {
			domIdx = i
		}
	}
	for i, r := range rows {
		contrib := 0.0
		if total > 0 {
			contrib = r.weight / total
		} else if len(rows) > 0 {
			contrib = 1.0 / float64(len(rows)) // 全零偏离 → 等权
		}
		chains = append(chains, PredictionTransmissionChain{
			Chain:        r.def.Chain,
			Contribution: predictionRound6(contrib),
			Dominant:     i == domIdx && len(rows) > 0,
			Evidence:     fmt.Sprintf("%s偏离峰值 %+.1f%%", r.label, r.peakDev),
		})
	}
	return chains
}

// predictionDistribution 分布影响(末点 vs 基线;确定性文案)。
func predictionDistribution(run *PredictionRun) PredictionDistribution {
	d := PredictionDistribution{Text: "分布影响有限,财富结构基本稳定"}
	if len(run.Trajectory) == 0 {
		return d
	}
	last := run.Trajectory[len(run.Trajectory)-1]
	d.GiniWealthDelta = predictionRound6(last.GiniWealth - run.Baseline.GiniWealth)
	d.Top10DeltaPct = predictionRound6(predictionDevPct(run.Baseline.Top10Pct, last.Top10Pct))
	d.Bottom50DeltaPct = predictionRound6(predictionDevPct(run.Baseline.Bottom50Pct, last.Bottom50Pct))
	for i := 0; i < 4; i++ {
		d.PyramidShift[i] = last.Pyramid[i] - run.Baseline.Pyramid[i]
	}
	parts := make([]string, 0, 3)
	switch {
	case d.GiniWealthDelta > 1e-6:
		parts = append(parts, "财富集中度上升")
	case d.GiniWealthDelta < -1e-6:
		parts = append(parts, "财富集中度下降")
	}
	switch {
	case d.Bottom50DeltaPct < -0.5:
		parts = append(parts, "底层份额下降")
	case d.Bottom50DeltaPct > 0.5:
		parts = append(parts, "底层份额上升")
	}
	switch {
	case d.Top10DeltaPct > 0.5:
		parts = append(parts, "头部份额上升")
	case d.Top10DeltaPct < -0.5:
		parts = append(parts, "头部份额下降")
	}
	if len(parts) > 0 {
		d.Text = strings.Join(parts, ",")
	}
	return d
}

// predictionExtrapolate 外推(方案 §4 规则4):最近 ≤3 个观测点的偏离序列 ——
// |dev| 单调收敛 → 几何半衰期;否则线性斜率;置信带 =
// ±(1.5×近 3 月残差带 + 0.02×|point|×horizon/3)。
func predictionExtrapolate(run *PredictionRun, key string, horizonMonths int) (point, lo, hi float64) {
	base := predictionMetricByKey(run.Baseline, key)
	if len(run.Trajectory) == 0 {
		return 0, 0, 0
	}
	// 偏离序列(最近 ≤3 个观测点)。
	n := len(run.Trajectory)
	start := n - 3
	if start < 0 {
		start = 0
	}
	devs := make([]float64, 0, 3)
	for _, pt := range run.Trajectory[start:] {
		devs = append(devs, predictionMetricByKey(pt, key)-base)
	}
	var futureDev float64
	if len(devs) >= 2 {
		// 单调收敛(|dev| 非增且末段严格缩小)→ 几何半衰期;否则线性斜率。
		converging := true
		for i := 1; i < len(devs); i++ {
			if math.Abs(devs[i]) > math.Abs(devs[i-1]) {
				converging = false
				break
			}
		}
		if converging && math.Abs(devs[len(devs)-1]) < math.Abs(devs[0]) {
			ratio := math.Abs(devs[len(devs)-1]) / math.Max(math.Abs(devs[len(devs)-2]), 1e-12)
			if ratio > 1 {
				ratio = 1
			}
			futureDev = devs[len(devs)-1] * math.Pow(ratio, float64(horizonMonths))
		} else {
			slope := devs[len(devs)-1] - devs[len(devs)-2] // 月斜率
			futureDev = devs[len(devs)-1] + slope*float64(horizonMonths)
		}
	}
	point = base + futureDev
	// 残差带 = 近 3 月相邻偏离差的最大幅值(数据不足 → 0)。
	band := 0.0
	for i := 1; i < len(devs); i++ {
		if d := math.Abs(devs[i] - devs[i-1]); d > band {
			band = d
		}
	}
	margin := 1.5*band + 0.02*math.Abs(point)*float64(horizonMonths)/3.0
	return point, point - margin, point + margin
}

// predictionResilience 韧性画像(方案 §4 规则6):仅 group ∈ {自然灾害,
// 未知灾难} 适用;评分 = f(恢复速度、失业/物价脱钩恢复、长期水平位移) ∈
// [0,100]。确定性启发式,依据全部落 basis 文本。
func predictionResilience(run *PredictionRun, peakByKey map[string]PredictionPeakSection, groups map[string]bool) PredictionResilience {
	res := PredictionResilience{Basis: []string{}}
	if !groups[ScenarioGroupNature] && !groups[ScenarioGroupUnknown] {
		return res
	}
	res.Applicable = true
	core := []string{"unemployment", "goods_price_idx", "stock_index", "house_idx"}
	// 恢复力:核心指标恢复速度均值(未恢复计 25 分)。
	sum := 0.0
	for _, k := range core {
		p := peakByKey[k]
		score := 25.0
		if p.Recovered && p.RecoveryMonth != nil {
			rel := *p.RecoveryMonth - run.StartMonth
			score = clampF(100-float64(rel)*6, 0, 100)
		}
		sum += score
	}
	res.RecoveryScore = int(math.Round(sum / float64(len(core))))
	// 适应力:失业/物价是否脱钩恢复(恢复月差 ≥2 → 加分;失业未恢复 → 减分)。
	u, g := peakByKey["unemployment"], peakByKey["goods_price_idx"]
	adapt := 55.0
	if u.Recovered && g.Recovered && u.RecoveryMonth != nil && g.RecoveryMonth != nil {
		if diff := int(math.Abs(float64(*u.RecoveryMonth - *g.RecoveryMonth))); diff >= 2 {
			adapt += clampF(float64(diff), 0, 25) / 5 * 5 // 差距越大脱钩越明显
		}
	}
	if !u.Recovered {
		adapt -= 10
	}
	res.AdaptationScore = int(math.Round(clampF(adapt, 0, 100)))
	// 转型力:长期水平位移(基尼上行/国库受损 → 减分;房价恢复 → 加分)。
	last := run.Baseline
	if len(run.Trajectory) > 0 {
		last = run.Trajectory[len(run.Trajectory)-1]
	}
	transform := 40.0
	if last.GiniWealth <= run.Baseline.GiniWealth {
		transform += 10
	}
	if last.TreasuryCash >= run.Baseline.TreasuryCash {
		transform += 10
	}
	if peakByKey["house_idx"].Recovered {
		transform += 5
	}
	res.TransformScore = int(math.Round(clampF(transform, 0, 100)))
	res.Basis = []string{
		"恢复:GDP/失业回正速度",
		"适应:失业/物价脱钩恢复检验",
		"转型:长期水平位移(基尼/国库/房价)",
	}
	return res
}
