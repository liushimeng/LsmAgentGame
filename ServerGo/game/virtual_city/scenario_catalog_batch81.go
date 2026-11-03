// Package virtual_city — scenario_catalog_batch81.go: 批次81 场景目录扩展
// (10 → 40;2026-11-03 §批次81 City-System 主 Agent 与系统对话)。
//
// 契约: lag_docs/虚拟城市/已实现/81-CitySystem-Agent与系统对话/01-方案设计.md §5。
//
// 定位:既有 10 个场景(批次68)覆盖「市场与经济 3 / 社会与舆情 2 /
// 自然灾害 3 / 未知灾难 2」;本文件补齐 6 组共 40 个 —— 新增「政策与治理」
// 与「科技与新文明」两组,并把市场/社会/自然/未知四组补足。
//
// 纪律:
//   - **只新增,不改派发**:本文件只提供 scenarioDefsBatch81 切片;
//     applyScenarioToWorld 的 profile 字段 → 既有状态字段映射零改动
//     (契约 §5.2 zero-regression);
//   - **零 rand**:幅度全部 × magnitude 线性派生,不消耗 w.Rand;
//   - **默认强度**:日常冲击 L2–L3,罕见巨灾 L4–L5(与既有 10 个同口径)。
package virtual_city

// 批次81 新增场景 kind 常量(契约 §5.2;既有 10 个见 scenario.go)。
// 集中在本文件而非 scenario.go —— 保持 scenario.go 仅 2 处改动
// (目录 append + 分组常量),新增 kind 随新增目录同文件维护。
const (
	// 市场与经济(+8)
	ScenarioInterestRateHike     = "interest_rate_hike"
	ScenarioCurrencyDepreciation = "currency_depreciation"
	ScenarioTradeWar             = "trade_war"
	ScenarioCommodityBoom        = "commodity_boom"
	ScenarioInflationSpiral      = "inflation_spiral"
	ScenarioDeflation            = "deflation"
	ScenarioHousingBubbleBurst   = "housing_bubble_burst"
	ScenarioBankRun              = "bank_run"
	// 政策与治理(+5)
	ScenarioFiscalStimulus           = "fiscal_stimulus"
	ScenarioAusterity                = "austerity"
	ScenarioRegulatoryCrackdown      = "regulatory_crackdown"
	ScenarioInfrastructureInvestment = "infrastructure_investment"
	ScenarioSocialWelfareExpansion   = "social_welfare_expansion"
	// 社会与舆情(+5)
	ScenarioSocialUnrest               = "social_unrest"
	ScenarioPandemicOutbreak           = "pandemic_outbreak"
	ScenarioPopulationSurge            = "population_surge"
	ScenarioAgingCrisis                = "aging_crisis"
	ScenarioConsumerConfidenceCollapse = "consumer_confidence_collapse"
	// 自然灾害(+6)
	ScenarioFlood            = "flood"
	ScenarioTyphoon          = "typhoon"
	ScenarioDrought          = "drought"
	ScenarioWildfire         = "wildfire"
	ScenarioVolcanicEruption = "volcanic_eruption"
	ScenarioExtremeHeat      = "extreme_heat"
	// 未知灾难(+3)
	ScenarioNuclearAccident = "nuclear_accident"
	ScenarioSolarStorm      = "solar_storm"
	ScenarioBioweaponLeak   = "bioweapon_leak"
	// 科技与新文明(+3)
	ScenarioAIBoom                     = "ai_boom"
	ScenarioUnknownCivilizationContact = "unknown_civilization_contact"
	ScenarioCyberAttack                = "cyber_attack"
)

// scenarioDefsBatch81 批次81 新增 30 个场景(分组/名称/默认强度/持续月数/
// 描述 → profile 数值)。字段结构与 scenario.go 的 scenarioDef 完全一致。
//
// 顺序 = 前端/工具分组展示顺序:市场与经济 → 政策与治理 → 社会与舆情 →
// 自然灾害 → 未知灾难 → 科技与新文明。
var scenarioDefsBatch81 = []scenarioDef{
	// ── 市场与经济(+8)──
	{
		spec: ScenarioSpec{
			Kind: ScenarioInterestRateHike, Name: "加息周期", DefaultMagnitude: 3,
			Group:       ScenarioGroupMarket,
			Description: "央行连续加息抑制通胀:政策利率与信贷窗口同步收紧,企业融资成本上行、股指漂移转负,居民满意度受挤压。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			PolicyRateDelta:   0.005,
			CreditWindowDelta: -0.15,
			StockDriftMonthly: -0.004,
			ElectionBias:      -3,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioCurrencyDepreciation, Name: "本币贬值", DefaultMagnitude: 3,
			Group:       ScenarioGroupMarket,
			Description: "汇率走低推高进口成本:消费品价格与 CPI 上行,黄金等避险资产受追捧。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsPriceShockPct: 0.04,
			GoodsMonthlyDrift:  0.015,
			GoldShockPct:       0.03,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioTradeWar, Name: "贸易战升级", DefaultMagnitude: 3,
			Group:       ScenarioGroupMarket,
			Description: "关税壁垒升级冲击出口企业:供给链受阻、失业率逐月上行,资本市场风险偏好下修。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsSupplyShockPct: -0.03,
			UnemploymentMonthly: 0.005,
			StockDriftMonthly:   -0.003,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioCommodityBoom, Name: "大宗商品繁荣", DefaultMagnitude: 2,
			Group:       ScenarioGroupMarket,
			Description: "全球大宗商品价格上行:商品价格指数与黄金同步走高,CPI 温和抬升,资源型行业景气改善。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsPriceShockPct: 0.05,
			GoldShockPct:       0.02,
			GoodsMonthlyDrift:  0.01,
			ElectionBias:       2,
		},
		defaultDuration: 4,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioInflationSpiral, Name: "通胀螺旋", DefaultMagnitude: 4,
			Group:       ScenarioGroupMarket,
			Description: "工资—价格螺旋自我强化:CPI 逐月加速上行,央行被迫加息,居民实际购买力与满意度持续受损。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsMonthlyDrift:  0.03,
			GoodsPriceShockPct: 0.02,
			PolicyRateDelta:    0.003,
			ElectionBias:       -6,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioDeflation, Name: "通缩螺旋", DefaultMagnitude: 3,
			Group:       ScenarioGroupMarket,
			Description: "需求收缩引发价格逐月下行:企业盈利受压、失业率攀升,央行降息对冲但信心修复缓慢。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsMonthlyDrift:   -0.02,
			UnemploymentMonthly: 0.008,
			PolicyRateDelta:     -0.002,
			StockDriftMonthly:   -0.002,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioHousingBubbleBurst, Name: "房地产泡沫破裂", DefaultMagnitude: 4,
			Group:       ScenarioGroupMarket,
			Description: "房价深度下修触发负财富效应:股指同步重挫、失业率快速上行,国库动用救市资金稳定市场。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			HouseShockPct:       -0.12,
			StockShockPct:       -0.08,
			TreasurySpendCNY:    2000000,
			UnemploymentMonthly: 0.010,
			ElectionBias:        -8,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioBankRun, Name: "银行挤兑", DefaultMagnitude: 4,
			Group:       ScenarioGroupMarket,
			Description: "居民挤提存款引发流动性危机:居民现金受损、信贷窗口大幅收紧,股指下挫,国库注入流动性。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			CashDamageFrac:    0.03,
			CreditWindowDelta: -0.4,
			StockShockPct:     -0.05,
			TreasurySpendCNY:  1500000,
		},
		defaultDuration: 6,
	},
	// ── 政策与治理(+5)──
	{
		spec: ScenarioSpec{
			Kind: ScenarioFiscalStimulus, Name: "财政刺激", DefaultMagnitude: 2,
			Group:       ScenarioGroupPolicy,
			Description: "政府扩大财政支出:国库一次性投入拉动就业,失业率下行、股指风险偏好回暖,满意度提升。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			TreasurySpendCNY:    1000000,
			UnemploymentMonthly: -0.005,
			StockShockPct:       0.03,
			ElectionBias:        5,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioAusterity, Name: "财政紧缩", DefaultMagnitude: 3,
			Group:       ScenarioGroupPolicy,
			Description: "政府压缩支出降低赤字:公共服务与转移支付收缩,失业率上行、物价温和回落,满意度下降。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			UnemploymentMonthly: 0.006,
			GoodsMonthlyDrift:   -0.005,
			ElectionBias:        -5,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioRegulatoryCrackdown, Name: "监管收紧", DefaultMagnitude: 3,
			Group:       ScenarioGroupPolicy,
			Description: "监管强度上升抬高合规成本:信贷窗口收紧、企业盈利预期下修,股指短期承压。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			CreditWindowDelta: -0.2,
			StockShockPct:     -0.02,
			StockDriftMonthly: -0.002,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioInfrastructureInvestment, Name: "基建投资", DefaultMagnitude: 2,
			Group:       ScenarioGroupPolicy,
			Description: "政府主导基建投资:国库支出创造就业岗位,失业率下行、供给链效率改善。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			TreasurySpendCNY:    1200000,
			UnemploymentMonthly: -0.008,
			GoodsSupplyShockPct: 0.02,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioSocialWelfareExpansion, Name: "社会保障扩面", DefaultMagnitude: 2,
			Group:       ScenarioGroupPolicy,
			Description: "社会保障覆盖面扩大:国库持续支出提升居民安全感,满意度与消费信心改善。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			TreasurySpendCNY: 600000,
			ElectionBias:     4,
		},
		defaultDuration: 6,
	},
	// ── 社会与舆情(+5)──
	{
		spec: ScenarioSpec{
			Kind: ScenarioSocialUnrest, Name: "社会动荡", DefaultMagnitude: 4,
			Group:       ScenarioGroupSociety,
			Description: "社会矛盾激化引发动荡:满意度大幅下挫、企业营收与股指受压,失业率上行,财政追加维稳支出。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			ElectionBias:        -10,
			StockShockPct:       -0.04,
			UnemploymentMonthly: 0.007,
			TreasurySpendCNY:    300000,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioPandemicOutbreak, Name: "疫情爆发", DefaultMagnitude: 4,
			Group:       ScenarioGroupSociety,
			Description: "传染病疫情冲击:供给链中断、失业率快速上行,国库投入公共卫生支出,满意度下降。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsSupplyShockPct: -0.06,
			UnemploymentMonthly: 0.012,
			TreasurySpendCNY:    1000000,
			ElectionBias:        -8,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioPopulationSurge, Name: "人口涌入", DefaultMagnitude: 2,
			Group:       ScenarioGroupSociety,
			Description: "外来人口快速流入:住房需求推高房价、消费品价格上行,劳动力供给增加使失业率小幅抬升。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			HouseShockPct:       0.04,
			GoodsPriceShockPct:  0.01,
			UnemploymentMonthly: 0.003,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioAgingCrisis, Name: "老龄化加剧", DefaultMagnitude: 3,
			Group:       ScenarioGroupSociety,
			Description: "人口结构老化:劳动力供给收缩、财政养老支出逐月增加,工资增长与物价温和上行。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			UnemploymentMonthly: -0.003,
			TreasuryMonthlyCNY:  200000,
			GoodsMonthlyDrift:   0.003,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioConsumerConfidenceCollapse, Name: "消费者信心崩塌", DefaultMagnitude: 3,
			Group:       ScenarioGroupSociety,
			Description: "居民预期转弱收缩消费:商品价格逐月下行、股指承压,失业率上行,满意度下降。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsMonthlyDrift:   -0.01,
			StockShockPct:       -0.03,
			UnemploymentMonthly: 0.005,
			ElectionBias:        -5,
		},
		defaultDuration: 6,
	},
	// ── 自然灾害(+6)──
	{
		spec: ScenarioSpec{
			Kind: ScenarioFlood, Name: "特大洪水", DefaultMagnitude: 3,
			Group:       ScenarioGroupNature,
			Description: "流域性洪水淹没城区:居民现金与住房受损、供给链中断,国库拨付重建资金。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			CashDamageFrac:      0.015,
			HouseShockPct:       -0.03,
			GoodsSupplyShockPct: -0.04,
			TreasurySpendCNY:    400000,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioTyphoon, Name: "强台风", DefaultMagnitude: 3,
			Group:       ScenarioGroupNature,
			Description: "强台风正面登陆:居民现金受损、供给与物流受阻,失业率短期上行,国库投入应急救灾。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			CashDamageFrac:      0.01,
			GoodsSupplyShockPct: -0.03,
			UnemploymentMonthly: 0.004,
			TreasurySpendCNY:    350000,
		},
		defaultDuration: 4,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioDrought, Name: "特大干旱", DefaultMagnitude: 3,
			Group:       ScenarioGroupNature,
			Description: "持续干旱打击农业生产:农产品价格上行、供给链承压,CPI 温和抬升。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsPriceShockPct:  0.06,
			GoodsSupplyShockPct: -0.05,
			GoodsMonthlyDrift:   0.02,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioWildfire, Name: "森林火灾", DefaultMagnitude: 2,
			Group:       ScenarioGroupNature,
			Description: "山林大火蔓延至城郊:居民现金与住房受损、局部供给中断,国库投入扑救与重建。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			CashDamageFrac:      0.008,
			GoodsSupplyShockPct: -0.02,
			HouseShockPct:       -0.01,
			TreasurySpendCNY:    250000,
		},
		defaultDuration: 4,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioVolcanicEruption, Name: "火山喷发", DefaultMagnitude: 4,
			Group:       ScenarioGroupNature,
			Description: "火山喷发掩埋部分城区:居民现金与住房严重受损、供给链大范围中断,国库启动长期重建。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			CashDamageFrac:      0.02,
			GoodsSupplyShockPct: -0.05,
			HouseShockPct:       -0.04,
			TreasurySpendCNY:    600000,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioExtremeHeat, Name: "极端高温", DefaultMagnitude: 2,
			Group:       ScenarioGroupNature,
			Description: "持续极端高温:用电紧张压制供给、部分商品价格上涨,户外劳动效率下降使失业率小幅上行。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsSupplyShockPct: -0.02,
			GoodsPriceShockPct:  0.015,
			UnemploymentMonthly: 0.002,
		},
		defaultDuration: 3,
	},
	// ── 未知灾难(+3)──
	{
		spec: ScenarioSpec{
			Kind: ScenarioNuclearAccident, Name: "核事故", DefaultMagnitude: 5,
			Group:       ScenarioGroupUnknown,
			Description: "核设施事故造成大面积污染:居民现金与住房严重受损、供给链长期中断,国库投入巨额处置与重建资金,满意度重挫。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			CashDamageFrac:      0.03,
			HouseShockPct:       -0.08,
			GoodsSupplyShockPct: -0.08,
			TreasurySpendCNY:    3000000,
			ElectionBias:        -15,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioSolarStorm, Name: "太阳风暴", DefaultMagnitude: 3,
			Group:       ScenarioGroupUnknown,
			Description: "强太阳风暴冲击电网与通信:供给链受阻、资本市场恐慌性下挫,国库投入设施抢修。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsSupplyShockPct: -0.06,
			StockShockPct:       -0.06,
			TreasurySpendCNY:    1200000,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioBioweaponLeak, Name: "生物武器泄漏", DefaultMagnitude: 5,
			Group:       ScenarioGroupUnknown,
			Description: "生物武器泄漏引发公共危机:居民现金受损、供给链长期中断,失业率快速上行,国库投入大规模处置资金。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			CashDamageFrac:      0.02,
			GoodsSupplyShockPct: -0.07,
			UnemploymentMonthly: 0.010,
			TreasurySpendCNY:    2000000,
		},
		defaultDuration: 12,
	},
	// ── 科技与新文明(+3)──
	{
		spec: ScenarioSpec{
			Kind: ScenarioAIBoom, Name: "AI 大爆发", DefaultMagnitude: 3,
			Group:       ScenarioGroupTech,
			Description: "AI 技术突破提升劳动生产率:工资增长与商品价格温和上行,资本市场风险偏好回暖,短期就业结构扰动。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			StockShockPct:       0.08,
			UnemploymentMonthly: 0.005,
			GoodsMonthlyDrift:   0.005,
			ElectionBias:        6,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioUnknownCivilizationContact, Name: "未知文明接触", DefaultMagnitude: 5,
			Group:       ScenarioGroupTech,
			Description: "与未知文明接触引发全社会不确定性:股指重挫、避险资产飙升,国库转入科研投入,满意度承压。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			StockShockPct:    -0.10,
			GoldShockPct:     0.10,
			TreasurySpendCNY: 2000000,
			ElectionBias:     -8,
		},
		defaultDuration: 18,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioCyberAttack, Name: "大规模网络攻击", DefaultMagnitude: 3,
			Group:       ScenarioGroupTech,
			Description: "针对金融与关键基础设施的网络攻击:股指下挫、供给链受阻,国库投入应急响应,信贷窗口收紧。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			StockShockPct:       -0.05,
			GoodsSupplyShockPct: -0.03,
			TreasurySpendCNY:    800000,
			CreditWindowDelta:   -0.2,
		},
		defaultDuration: 6,
	},
}
